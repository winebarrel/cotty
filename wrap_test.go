package cotty

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// shortTempDir returns a temporary directory with a short path. t.TempDir
// includes the test name, which can push a socket path past its limit.
func shortTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "cotty")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	return dir
}

type testSession struct {
	ctx   *Context
	name  string
	stdin *os.File
	out   *syncBuffer
	done  chan error
}

func startSession(t *testing.T, home, name string, command ...string) *testSession {
	t.Helper()

	return startWrap(t, home, &WrapCmd{Name: name, Command: command})
}

func startWrap(t *testing.T, home string, wrap *WrapCmd) *testSession {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	ts := &testSession{
		ctx:   &Context{Home: home, Stdin: r, Stdout: &syncBuffer{}, ErrOutput: &syncBuffer{}},
		name:  wrap.Name,
		stdin: w,
		done:  make(chan error, 1),
	}
	ts.out = ts.ctx.Stdout.(*syncBuffer)

	go func() {
		ts.done <- wrap.Run(ts.ctx)
	}()

	t.Cleanup(func() {
		w.Close()
		r.Close()
	})

	require.Eventually(t, func() bool {
		_, err := ts.call(&request{Op: opInfo})
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)

	return ts
}

func (ts *testSession) call(req *request) (*response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return roundTrip(ctx, ts.ctx.sockPath(ts.name), req)
}

func (ts *testSession) wait(t *testing.T) error {
	t.Helper()

	select {
	case err := <-ts.done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end")
		return nil
	}
}

func TestWrapSendAndRead(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	ts := startSession(t, shortTempDir(t), "cat", "cat")

	resp, err := ts.call(&request{Op: opInfo})
	require.NoError(err)
	assert.Equal("cat", resp.Info.Name)
	assert.Equal([]string{"cat"}, resp.Info.Command)
	assert.True(resp.Info.AgentInput)
	assert.NotZero(resp.Info.PID)

	_, err = ts.call(&request{Op: opSend, Data: "hello\r"})
	require.NoError(err)

	// The terminal echoes the line and cat prints it back.
	resp, err = ts.call(&request{Op: opRead, WaitFor: `hello\n.*hello\n`, TimeoutMs: 5000})
	require.NoError(err)
	assert.True(resp.Read.Matched)
	assert.Equal("hello\nhello\n", resp.Read.Output)
	assert.Equal(resp.Info, (*SessionInfo)(nil))

	// A cursor picks up after what was already read.
	from := resp.Read.Next
	_, err = ts.call(&request{Op: opSend, Data: "world\r"})
	require.NoError(err)

	resp2, err := ts.call(&request{Op: opRead, ID: resp.Read.ID, From: &from, WaitFor: `world\n.*world\n`, TimeoutMs: 5000})
	require.NoError(err)
	assert.Equal("world\nworld\n", resp2.Read.Output)

	// A cursor from another session is ignored in favor of the tail.
	zero := int64(0)
	resp3, err := ts.call(&request{Op: opRead, ID: "other", From: &zero, TailLines: 1})
	require.NoError(err)
	assert.Equal("world\n", resp3.Read.Output)

	// What the user types is output too, and cotty's own messages are not.
	ts.stdin.Write([]byte("typed\r"))
	resp4, err := ts.call(&request{Op: opRead, ID: resp2.Read.ID, From: &resp2.Read.Next, WaitFor: `typed\n.*typed\n`, TimeoutMs: 5000})
	require.NoError(err)
	assert.Equal("typed\ntyped\n", resp4.Read.Output)

	assert.Contains(ts.out.String(), `[cotty] session "cat" started.`)
	assert.Contains(ts.out.String(), "hello\r\n")

	_, err = ts.call(&request{Op: opSend, Data: "\x04"})
	require.NoError(err)
	require.NoError(ts.wait(t))
	assert.Contains(ts.out.String(), `[cotty] session "cat" closed`)

	// The socket goes away with the session.
	_, err = os.Stat(ts.ctx.sockPath("cat"))
	assert.ErrorIs(err, os.ErrNotExist)
}

func TestWrapLog(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	home := shortTempDir(t)
	out := &syncBuffer{}
	err := (&WrapCmd{Name: "log", Command: []string{"printf", `\033[1mbold\033[0m\r\n`}}).Run(&Context{Home: home, Stdin: os.Stdin, Stdout: out})
	require.NoError(err)

	// The user's terminal gets the output as it is.
	assert.Contains(out.String(), "\x1b[1mbold\x1b[0m")

	logs, err := filepath.Glob(filepath.Join(home, "log", "log-*.log"))
	require.NoError(err)
	require.Len(logs, 1)

	data, err := os.ReadFile(logs[0])
	require.NoError(err)
	assert.Equal("bold\n", string(data))
}

func TestWrapEnv(t *testing.T) {
	require := require.New(t)
	ts := startSession(t, shortTempDir(t), "env", "sh", "-c", `echo "session=$COTTY_SESSION"; read x`)

	resp, err := ts.call(&request{Op: opRead, WaitFor: `session=env\n`, TimeoutMs: 5000})
	require.NoError(err)
	require.True(resp.Read.Matched)

	_, err = ts.call(&request{Op: opSend, Data: "\r"})
	require.NoError(err)
	require.NoError(ts.wait(t))
}

func TestWrapExitCode(t *testing.T) {
	ts := startSession(t, shortTempDir(t), "exit", "sh", "-c", "sleep 0.2; exit 3")

	err := ts.wait(t)
	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 3, exitErr.Code)
}

func TestWrapReadSeesClose(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	ts := startSession(t, shortTempDir(t), "close", "sh", "-c", "read x; echo bye")

	sent := make(chan struct{})

	go func() {
		time.Sleep(100 * time.Millisecond)
		ts.call(&request{Op: opSend, Data: "\r"})
		close(sent)
	}()

	resp, err := ts.call(&request{Op: opRead, WaitFor: `never`, TimeoutMs: 5000})
	require.NoError(err)
	assert.True(resp.Read.Closed)
	assert.False(resp.Read.TimedOut)
	assert.Contains(resp.Read.Output, "bye\n")

	<-sent
	require.NoError(ts.wait(t))
}

func TestWrapDenyAgentInput(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	ts := startSession(t, shortTempDir(t), "deny", "cat")

	ts.stdin.Write([]byte{prefixKey, prefixToggleAgent})

	require.Eventually(func() bool {
		resp, err := ts.call(&request{Op: opInfo})
		return err == nil && !resp.Info.AgentInput
	}, 5*time.Second, 10*time.Millisecond)

	_, err := ts.call(&request{Op: opSend, Data: "x"})
	assert.ErrorContains(err, `agent input is denied in session "deny"`)
	assert.Contains(ts.out.String(), "[cotty] agent input: denied")

	ts.stdin.Write([]byte{prefixKey, prefixHelp})
	require.Eventually(func() bool {
		return bytes.Contains([]byte(ts.out.String()), []byte("Ctrl-] a: allow/deny agent input (now denied)"))
	}, 5*time.Second, 10*time.Millisecond)

	ts.stdin.Write([]byte{prefixKey, prefixToggleAgent})

	require.Eventually(func() bool {
		_, err := ts.call(&request{Op: opSend, Data: "\x04"})
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(ts.wait(t))
}

func TestWrapPause(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	ts := startSession(t, shortTempDir(t), "pause", "cat")

	_, err := ts.call(&request{Op: opSend, Data: "before\r"})
	require.NoError(err)

	resp, err := ts.call(&request{Op: opRead, WaitFor: `before\n`, TimeoutMs: 5000})
	require.NoError(err)
	require.True(resp.Read.Matched)
	assert.False(resp.Read.Paused)
	next := resp.Read.Next

	ts.stdin.Write([]byte{prefixKey, prefixTogglePause})

	require.Eventually(func() bool {
		resp, err := ts.call(&request{Op: opInfo})
		return err == nil && resp.Info.Paused
	}, 5*time.Second, 10*time.Millisecond)

	assert.Contains(ts.out.String(), "[cotty] agent output: paused")

	ts.stdin.Write([]byte("secret\r"))

	// The user still sees the output.
	require.Eventually(func() bool {
		return bytes.Count([]byte(ts.out.String()), []byte("secret")) == 2
	}, 5*time.Second, 10*time.Millisecond)

	ts.stdin.Write([]byte{prefixKey, prefixHelp})
	require.Eventually(func() bool {
		return bytes.Contains([]byte(ts.out.String()), []byte("Ctrl-] p: pause/resume agent output (now paused)"))
	}, 5*time.Second, 10*time.Millisecond)

	ts.stdin.Write([]byte{prefixKey, prefixTogglePause})

	require.Eventually(func() bool {
		resp, err := ts.call(&request{Op: opInfo})
		return err == nil && !resp.Info.Paused
	}, 5*time.Second, 10*time.Millisecond)

	_, err = ts.call(&request{Op: opSend, Data: "after\r"})
	require.NoError(err)

	resp, err = ts.call(&request{Op: opRead, ID: resp.Read.ID, From: &next, WaitFor: `after\n`, TimeoutMs: 5000})
	require.NoError(err)
	require.True(resp.Read.Matched)
	assert.Equal("\n[cotty: the user paused the output here]\nafter\nafter\n", resp.Read.Output)
	assert.NotContains(resp.Read.Output, "secret")

	_, err = ts.call(&request{Op: opSend, Data: "\x04"})
	require.NoError(err)
	require.NoError(ts.wait(t))

	logs, err := filepath.Glob(filepath.Join(ts.ctx.Home, "log", "pause-*.log"))
	require.NoError(err)
	require.Len(logs, 1)

	data, err := os.ReadFile(logs[0])
	require.NoError(err)
	assert.NotContains(string(data), "secret")
	assert.Contains(string(data), "[cotty: the user paused the output here]")
}

func TestWrapBadRequests(t *testing.T) {
	ts := startSession(t, shortTempDir(t), "bad", "cat")

	_, err := ts.call(&request{Op: "nope"})
	assert.ErrorContains(t, err, `unknown op: "nope"`)

	_, err = ts.call(&request{Op: opRead, WaitFor: "("})
	assert.ErrorContains(t, err, "invalid wait_for")

	_, err = ts.call(&request{Op: opSend, Data: "\x04"})
	require.NoError(t, err)
	require.NoError(t, ts.wait(t))
}

func TestWrapDuplicateName(t *testing.T) {
	home := shortTempDir(t)
	ts := startSession(t, home, "dup", "cat")

	err := (&WrapCmd{Name: "dup", Command: []string{"cat"}}).Run(&Context{Home: home, Stdin: os.Stdin, Stdout: &syncBuffer{}})
	assert.EqualError(t, err, `session "dup" already exists`)

	_, err = ts.call(&request{Op: opSend, Data: "\x04"})
	require.NoError(t, err)
	require.NoError(t, ts.wait(t))
}

func TestWrapCommandNotFound(t *testing.T) {
	home := shortTempDir(t)
	c := &Context{Home: home, Stdin: os.Stdin, Stdout: &syncBuffer{}}

	err := (&WrapCmd{Name: "nf", Command: []string{"/nonexistent/command"}}).Run(c)
	assert.Error(t, err)

	// The socket claimed for the session is released.
	_, err = os.Stat(c.sockPath("nf"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestWrapInvalidName(t *testing.T) {
	err := (&WrapCmd{Name: "a/b", Command: []string{"cat"}}).Run(&Context{Home: shortTempDir(t), Stdin: os.Stdin, Stdout: &syncBuffer{}})
	assert.ErrorContains(t, err, "may contain only")
}

func TestWrapOnTerminal(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Give the session a real terminal as its stdin, as a user's shell would.
	ptmx, tty, err := pty.Open()
	require.NoError(err)
	defer ptmx.Close()
	defer tty.Close()
	require.NoError(pty.Setsize(ptmx, &pty.Winsize{Rows: 30, Cols: 100}))

	home := shortTempDir(t)
	c := &Context{Home: home, Stdin: tty, Stdout: &syncBuffer{}}
	done := make(chan error, 1)

	go func() {
		done <- (&WrapCmd{Name: "tty", Command: []string{"sh", "-c", "stty size; read x; stty size; read x"}}).Run(c)
	}()

	ts := &testSession{ctx: c, name: "tty", done: done}

	var resp *response

	require.Eventually(func() bool {
		r, err := ts.call(&request{Op: opRead, WaitFor: `30 100\n`, TimeoutMs: 1000})

		if err != nil || !r.Read.Matched {
			return false
		}

		resp = r

		return true
	}, 5*time.Second, 50*time.Millisecond)

	// A resize of the user's terminal reaches the command.
	require.NoError(pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120}))
	require.NoError(syscall.Kill(os.Getpid(), syscall.SIGWINCH))
	time.Sleep(100 * time.Millisecond)

	// In raw mode a carriage return from the user reaches the command as is.
	ptmx.Write([]byte("\r"))

	resp, err = ts.call(&request{Op: opRead, ID: resp.Read.ID, From: &resp.Read.Next, WaitFor: `40 120\n`, TimeoutMs: 5000})
	require.NoError(err)
	assert.True(resp.Read.Matched)

	ptmx.Write([]byte("\r"))
	require.NoError(ts.wait(t))
}

func TestExitError(t *testing.T) {
	assert.EqualError(t, &ExitError{Code: 2}, "exit status 2")
}

func TestWrapReadOnly(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	ts := startWrap(t, shortTempDir(t), &WrapCmd{Name: "ro", ReadOnly: true, Command: []string{"cat"}})

	resp, err := ts.call(&request{Op: opInfo})
	require.NoError(err)
	assert.False(resp.Info.AgentInput)
	assert.Contains(ts.out.String(), `[cotty] session "ro" started. agent input: denied.`)

	_, err = ts.call(&request{Op: opSend, Data: "x"})
	assert.ErrorContains(err, `agent input is denied in session "ro"`)

	ts.stdin.Write([]byte{prefixKey, prefixToggleAgent})

	require.Eventually(func() bool {
		_, err := ts.call(&request{Op: opSend, Data: "\x04"})
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(ts.wait(t))
}

func TestWrapBufferSize(t *testing.T) {
	home := shortTempDir(t)
	out := &syncBuffer{}
	c := &Context{Home: home, Stdin: os.Stdin, Stdout: out}
	done := make(chan error, 1)

	go func() {
		done <- (&WrapCmd{Name: "small", BufferSize: 8, Command: []string{"sh", "-c", "echo 0123456789; read x"}}).Run(c)
	}()

	ts := &testSession{ctx: c, name: "small", done: done}
	var resp *response

	require.Eventually(t, func() bool {
		r, err := ts.call(&request{Op: opRead, WaitFor: `9\n`, TimeoutMs: 1000})

		if err != nil || !r.Read.Matched {
			return false
		}

		resp = r

		return true
	}, 5*time.Second, 50*time.Millisecond)

	// Only the last 8 bytes are kept.
	assert.Equal(t, "3456789\n", resp.Read.Output)

	_, err := ts.call(&request{Op: opSend, Data: "\r"})
	require.NoError(t, err)
	require.NoError(t, ts.wait(t))
}

func TestCopySizeNotTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer r.Close()
	defer w.Close()

	ptmx, tty, err := pty.Open()
	require.NoError(t, err)
	defer ptmx.Close()
	defer tty.Close()

	assert.Error(t, copySize(r, ptmx))
	assert.Error(t, copySize(tty, w))
}
