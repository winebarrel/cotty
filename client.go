package cotty

import (
	"context"
	"encoding/json"
	"errors"
	"net"
)

// roundTrip sends one request to the session socket at path.
func roundTrip(ctx context.Context, path string, req *request) (*response, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)

	if err != nil {
		return nil, err
	}

	defer conn.Close() //nolint:errcheck

	// Closing the connection is what unblocks a read when ctx ends.
	stop := context.AfterFunc(ctx, func() { conn.Close() }) //nolint:errcheck
	defer stop()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}

	var resp response

	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		return nil, err
	}

	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	return &resp, nil
}

// isDialError reports whether err came from failing to connect, as opposed to
// something going wrong once connected.
func isDialError(err error) bool {
	var opErr *net.OpError

	return errors.As(err, &opErr) && opErr.Op == "dial"
}
