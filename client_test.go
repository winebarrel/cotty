package cotty

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listenOnce accepts one connection, reads the request line and hands the
// connection to fn.
func listenOnce(t *testing.T, fn func(net.Conn)) string {
	t.Helper()

	path := filepath.Join(shortTempDir(t), "s.sock")
	ln, err := net.Listen("unix", path)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()

		if err != nil {
			return
		}

		defer conn.Close()
		bufio.NewReader(conn).ReadBytes('\n')
		fn(conn)
	}()

	return path
}

func TestRoundTripNoReply(t *testing.T) {
	path := listenOnce(t, func(net.Conn) {})

	_, err := roundTrip(context.Background(), path, &request{Op: opInfo})
	assert.Error(t, err)
	assert.False(t, isDialError(err))
}

func TestRoundTripCanceled(t *testing.T) {
	path := listenOnce(t, func(net.Conn) { time.Sleep(time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := roundTrip(ctx, path, &request{Op: opInfo})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRoundTripNotListening(t *testing.T) {
	_, err := roundTrip(context.Background(), filepath.Join(shortTempDir(t), "none.sock"), &request{Op: opInfo})
	assert.True(t, isDialError(err))
}

func TestServerMalformedRequest(t *testing.T) {
	require := require.New(t)
	ts := startSession(t, shortTempDir(t), "malformed", "cat")

	conn, err := net.Dial("unix", ts.ctx.sockPath("malformed"))
	require.NoError(err)
	defer conn.Close()

	_, err = conn.Write([]byte("{\n"))
	require.NoError(err)

	line, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(err)
	assert.Contains(t, line, "malformed request")

	_, err = ts.call(&request{Op: opSend, Data: "\x04"})
	require.NoError(err)
	require.NoError(ts.wait(t))
}
