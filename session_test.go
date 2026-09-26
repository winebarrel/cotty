package cotty

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateName(t *testing.T) {
	for _, name := range []string{"web1", "a", "db.prod_1-a", strings.Repeat("x", maxNameLen)} {
		assert.NoError(t, validateName(name), name)
	}

	for _, name := range []string{"", "-a", ".a", "a/b", "a b", "あ", strings.Repeat("x", maxNameLen+1)} {
		assert.Error(t, validateName(name), name)
	}
}

func TestListenTakesOverStaleSocket(t *testing.T) {
	c := &Context{Home: shortTempDir(t)}
	require.NoError(t, os.MkdirAll(c.sockDir(), 0o700))

	// A socket file with nobody listening, as a crashed session leaves.
	ln, err := net.Listen("unix", c.sockPath("stale"))
	require.NoError(t, err)
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()

	ln, err = c.listen("stale")
	require.NoError(t, err)
	defer ln.Close()

	info, err := os.Stat(c.sockPath("stale"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestListenPathTooLong(t *testing.T) {
	c := &Context{Home: "/" + strings.Repeat("x", maxSockPath)}

	_, err := c.listen("a")
	assert.ErrorContains(t, err, "is longer than")
}

func TestSessionsSkipsDeadSockets(t *testing.T) {
	home := shortTempDir(t)
	ts := startSession(t, home, "alive", "cat")
	c := &Context{Home: home}

	ln, err := net.Listen("unix", c.sockPath("dead"))
	require.NoError(t, err)
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()

	// Not a session name, so not even tried.
	require.NoError(t, os.WriteFile(c.sockPath("-x"), nil, 0o600))

	infos := c.sessions(context.Background())
	require.Len(t, infos, 1)
	assert.Equal(t, "alive", infos[0].Name)

	_, err = ts.call(&request{Op: opSend, Data: "\x04"})
	require.NoError(t, err)
	require.NoError(t, ts.wait(t))
}

func TestListenCannotCreateDir(t *testing.T) {
	home := shortTempDir(t)
	require.NoError(t, os.WriteFile(home+"/sock", nil, 0o600))

	_, err := (&Context{Home: home}).listen("a")
	assert.Error(t, err)
}

func TestOpenLogCannotCreateDir(t *testing.T) {
	home := shortTempDir(t)
	require.NoError(t, os.WriteFile(home+"/log", nil, 0o600))

	err := (&WrapCmd{Name: "a", Command: []string{"true"}}).Run(&Context{Home: home, Stdin: os.Stdin, Stdout: &syncBuffer{}})
	assert.Error(t, err)
}
