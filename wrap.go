package cotty

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Defaults for what a caller leaves out. `cotty mcp` always fills them
// in from its own flags, so these apply only to other clients.
const (
	defaultBufferSize = 1 << 20
	defaultReadMax    = 32 << 10
	defaultTailLines  = 50
	defaultTimeout    = 30 * time.Second
)

// drainTimeout bounds the wait for the last output after the command
// exits. A process it left in the background can hold the terminal open
// indefinitely.
const drainTimeout = time.Second

// ExitError reports the exit status of the wrapped command, which cotty exits
// with in turn.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// WrapCmd runs a command in a terminal shared with the agent.
type WrapCmd struct {
	Name       string   `short:"n" required:"" help:"Session name: letters, digits, '.', '_' and '-', up to 32 characters."`
	BufferSize ByteSize `default:"1MiB" env:"COTTY_BUFFER_SIZE" help:"How much recent output to keep for the agent to read."`
	ReadOnly   bool     `short:"r" env:"COTTY_READ_ONLY" help:"Start with the agent's input denied. Ctrl-] a allows it."`
	Command    []string `arg:"" passthrough:"" help:"Command to run, with its arguments."`
}

func (w *WrapCmd) Run(c *Context) error {
	ln, err := c.listen(w.Name)

	if err != nil {
		return err
	}

	// The server closes the listener too; closing it again is harmless and
	// covers the returns before the server starts.
	defer ln.Close() //nolint:errcheck

	startedAt := time.Now()
	logFile, err := c.openLog(w.Name, startedAt)

	if err != nil {
		return err
	}

	defer logFile.Close() //nolint:errcheck

	cmd := exec.Command(w.Command[0], w.Command[1:]...)
	cmd.Env = append(os.Environ(), "COTTY_SESSION="+w.Name)

	stdinFd := int(c.Stdin.Fd())
	isTerm := term.IsTerminal(stdinFd)
	var size *pty.Winsize

	if isTerm {
		size, _ = pty.GetsizeFull(c.Stdin)
	}

	ptmx, err := pty.StartWithSize(cmd, size)

	if err != nil {
		return err
	}

	defer ptmx.Close() //nolint:errcheck

	bufferSize := int(w.BufferSize)

	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}

	s := &wrapSession{
		info: SessionInfo{
			Name:      w.Name,
			ID:        rand.Text(),
			Command:   w.Command,
			PID:       cmd.Process.Pid,
			StartedAt: startedAt,
		},
		ptmx: ptmx,
		out:  c.Stdout,
		buf:  newOutputBuffer(bufferSize),
		log:  logFile,
	}

	s.agentInput.Store(!w.ReadOnly)

	if isTerm {
		oldState, err := term.MakeRaw(stdinFd)

		if err != nil {
			return err
		}

		defer term.Restore(stdinFd, oldState) //nolint:errcheck

		winch := make(chan os.Signal, 1)
		signal.Notify(winch, syscall.SIGWINCH)
		stop := make(chan struct{})
		var wg sync.WaitGroup

		wg.Go(func() {
			for {
				select {
				case <-winch:
					copySize(c.Stdin, ptmx) //nolint:errcheck
				case <-stop:
					return
				}
			}
		})

		// Runs before ptmx is closed, so a resize never lands on a closed
		// terminal.
		defer func() {
			signal.Stop(winch)
			close(stop)
			wg.Wait()
		}()
	}

	s.writeOut(fmt.Appendf(nil, "[cotty] session %q started. agent input: %s. Ctrl-] ? for help\r\n", w.Name, allowedWord(!w.ReadOnly)))

	server := newSocketServer(ln, s.handle)
	go server.Serve()

	drained := make(chan struct{})

	go func() {
		s.pumpOutput()
		close(drained)
	}()

	go s.pumpInput(c.Stdin)

	waitErr := cmd.Wait()

	select {
	case <-drained:
	case <-time.After(drainTimeout):
	}

	// Closing the buffer first lets waiting readers see that the session
	// ended rather than being cut off by the server shutdown.
	s.buf.Close()
	server.Close() //nolint:errcheck
	s.notify(fmt.Sprintf("session %q closed", w.Name))

	var exitErr *exec.ExitError

	if errors.As(waitErr, &exitErr) {
		code := exitErr.ExitCode()

		if code < 0 {
			code = 1
		}

		return &ExitError{Code: code}
	}

	return waitErr
}

// copySize gives to the window size of from.
//
// The files are reached through SyscallConn rather than Fd, which would race
// with the reads that are going on at the same time and switch the files to
// blocking mode.
func copySize(from, to *os.File) error {
	var ws *unix.Winsize
	var err error

	if err := control(from, func(fd int) { ws, err = unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ) }); err != nil {
		return err
	}

	if err != nil {
		return err
	}

	if err := control(to, func(fd int) { err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, ws) }); err != nil {
		return err
	}

	return err
}

func control(f *os.File, fn func(fd int)) error {
	rc, err := f.SyscallConn()

	if err != nil {
		return err
	}

	return rc.Control(func(fd uintptr) { fn(int(fd)) })
}

// wrapSession is the state of a running session.
type wrapSession struct {
	info       SessionInfo
	agentInput atomic.Bool

	ptmx  *os.File
	ptyMu sync.Mutex

	// out is the user's terminal. Output from the command and cotty's own
	// messages are written to it from different goroutines, so writes are
	// serialized to keep an escape sequence from being cut in two.
	out   io.Writer
	outMu sync.Mutex

	buf *outputBuffer
	log io.Writer
}

func (s *wrapSession) writePTY(p []byte) error {
	s.ptyMu.Lock()
	defer s.ptyMu.Unlock()

	_, err := s.ptmx.Write(p)

	return err
}

func (s *wrapSession) writeOut(p []byte) {
	s.outMu.Lock()
	defer s.outMu.Unlock()

	s.out.Write(p) //nolint:errcheck
}

// notify shows a message from cotty on the user's terminal. It is not part of
// the session's output, so the agent and the log never see it.
func (s *wrapSession) notify(msg string) {
	s.writeOut([]byte("\r\n[cotty] " + msg + "\r\n"))
}

func (s *wrapSession) pumpOutput() {
	var stripper ansiStripper
	buf := make([]byte, 32<<10)

	for {
		n, err := s.ptmx.Read(buf)

		if n > 0 {
			s.writeOut(buf[:n])
			text := stripper.Strip(buf[:n])
			s.buf.Write(text) //nolint:errcheck
			s.log.Write(text) //nolint:errcheck
		}

		if err != nil {
			return
		}
	}
}

func (s *wrapSession) pumpInput(r io.Reader) {
	var filter prefixFilter
	buf := make([]byte, 1024)

	for {
		n, err := r.Read(buf)

		if n > 0 {
			forward, commands := filter.Filter(buf[:n])

			if len(forward) > 0 {
				if s.writePTY(forward) != nil {
					return
				}
			}

			for _, cmd := range commands {
				s.runPrefixCommand(cmd)
			}
		}

		if err != nil {
			return
		}
	}
}

func (s *wrapSession) runPrefixCommand(cmd byte) {
	switch cmd {
	case prefixToggleAgent:
		allowed := !s.agentInput.Load()
		s.agentInput.Store(allowed)
		s.notify("agent input: " + allowedWord(allowed))
	case prefixHelp:
		s.notify(fmt.Sprintf("Ctrl-] a: allow/deny agent input (now %s), Ctrl-] Ctrl-]: send Ctrl-]", allowedWord(s.agentInput.Load())))
	}
}

func allowedWord(allowed bool) string {
	if allowed {
		return "allowed"
	}

	return "denied"
}

func (s *wrapSession) handle(ctx context.Context, req *request) *response {
	switch req.Op {
	case opInfo:
		info := s.info
		info.AgentInput = s.agentInput.Load()

		return &response{Info: &info}
	case opSend:
		if !s.agentInput.Load() {
			return &response{Error: fmt.Sprintf("agent input is denied in session %q; the user can allow it with Ctrl-] a", s.info.Name)}
		}

		if err := s.writePTY([]byte(req.Data)); err != nil {
			return &response{Error: err.Error()}
		}

		return &response{}
	case opRead:
		return s.read(ctx, req)
	default:
		return &response{Error: fmt.Sprintf("unknown op: %q", req.Op)}
	}
}

func (s *wrapSession) read(ctx context.Context, req *request) *response {
	opts := readOptions{
		Timeout: defaultTimeout,
		Idle:    time.Duration(req.IdleMs) * time.Millisecond,
		Max:     defaultReadMax,
	}

	if req.TimeoutMs > 0 {
		opts.Timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}

	if req.Max > 0 {
		opts.Max = req.Max
	}

	if req.WaitFor != "" {
		re, err := regexp.Compile(req.WaitFor)

		if err != nil {
			return &response{Error: fmt.Sprintf("invalid wait_for: %s", err)}
		}

		opts.WaitFor = re
	}

	if req.From != nil && req.ID == s.info.ID {
		opts.From = *req.From
	} else {
		lines := req.TailLines

		if lines <= 0 {
			lines = defaultTailLines
		}

		opts.From = s.buf.TailOffset(lines)
	}

	res := s.buf.Read(ctx, opts)

	return &response{Read: &readReply{
		ID:        s.info.ID,
		Output:    string(res.Output),
		Next:      res.Next,
		Truncated: res.Truncated,
		Matched:   res.Matched,
		TimedOut:  res.TimedOut,
		Closed:    res.Closed,
	}}
}
