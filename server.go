package cotty

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const (
	maxRequestSize   = 1 << 20
	requestTimeout   = 10 * time.Second
	shutdownTimeout  = 2 * time.Second
	responseDeadline = 10 * time.Second
)

type requestHandler func(ctx context.Context, req *request) *response

// socketServer answers requests on a session socket.
type socketServer struct {
	ln     net.Listener
	handle requestHandler
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newSocketServer(ln net.Listener, handle requestHandler) *socketServer {
	ctx, cancel := context.WithCancel(context.Background())

	return &socketServer{
		ln:     ln,
		handle: handle,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Serve accepts connections until the listener is closed.
func (s *socketServer) Serve() {
	for {
		conn, err := s.ln.Accept()

		if err != nil {
			return
		}

		s.wg.Go(func() {
			defer conn.Close() //nolint:errcheck
			s.serveConn(conn)
		})
	}
}

func (s *socketServer) serveConn(conn net.Conn) {
	var resp *response

	conn.SetReadDeadline(time.Now().Add(requestTimeout)) //nolint:errcheck
	line, err := bufio.NewReader(io.LimitReader(conn, maxRequestSize)).ReadBytes('\n')

	if err != nil {
		return
	}

	var req request

	if err := json.Unmarshal(line, &req); err != nil {
		resp = &response{Error: "malformed request: " + err.Error()}
	} else {
		resp = s.handle(s.ctx, &req)
	}

	conn.SetWriteDeadline(time.Now().Add(responseDeadline)) //nolint:errcheck
	json.NewEncoder(conn).Encode(resp)                      //nolint:errcheck
}

// Close stops accepting connections, which also removes the socket file, and
// gives requests in flight a moment to finish.
func (s *socketServer) Close() error {
	err := s.ln.Close()
	s.cancel()

	done := make(chan struct{})

	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(shutdownTimeout):
	}

	if errors.Is(err, net.ErrClosed) {
		return nil
	}

	return err
}
