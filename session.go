package cotty

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxNameLen = 32

	// maxSockPath is the longest socket path macOS accepts. Linux allows a few
	// more bytes, but a limit that holds on both keeps behavior the same.
	maxSockPath = 103

	infoTimeout = time.Second
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validateName(name string) error {
	switch {
	case name == "":
		return errors.New("session name is empty")
	case len(name) > maxNameLen:
		return fmt.Errorf("session name %q is longer than %d characters", name, maxNameLen)
	case !namePattern.MatchString(name):
		return fmt.Errorf("session name %q may contain only letters, digits, '.', '_' and '-', and must start with a letter or digit", name)
	}

	return nil
}

func (c *Context) sockPath(name string) string {
	return filepath.Join(c.sockDir(), name+".sock")
}

// listen claims the socket for a new session.
//
// A socket already there is taken over only when nothing answers on it, which
// means the session that made it ended without cleaning up.
func (c *Context) listen(name string) (net.Listener, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}

	path := c.sockPath(name)

	if len(path) > maxSockPath {
		return nil, fmt.Errorf("socket path %s is longer than %d bytes; use a shorter session name or COTTY_HOME", path, maxSockPath)
	}

	if err := os.MkdirAll(c.sockDir(), 0o700); err != nil {
		return nil, err
	}

	if _, err := os.Lstat(path); err == nil {
		conn, err := net.DialTimeout("unix", path, infoTimeout)

		if err == nil {
			conn.Close() //nolint:errcheck
			return nil, fmt.Errorf("session %q already exists", name)
		}

		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}

	ln, err := net.Listen("unix", path)

	if err != nil {
		return nil, err
	}

	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close() //nolint:errcheck
		return nil, err
	}

	return ln, nil
}

// openLog creates the log file for a session started at t.
func (c *Context) openLog(name string, t time.Time) (*os.File, error) {
	if err := os.MkdirAll(c.logDir(), 0o700); err != nil {
		return nil, err
	}

	path := filepath.Join(c.logDir(), name+"-"+t.Format("20060102-150405")+".log")

	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
}

// sessions returns the running sessions sorted by name. A socket that does not
// answer belongs to a session that is gone and is left out.
func (c *Context) sessions(ctx context.Context) []SessionInfo {
	paths, _ := filepath.Glob(filepath.Join(c.sockDir(), "*.sock"))
	sort.Strings(paths)
	infos := []SessionInfo{}

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".sock")

		if validateName(name) != nil {
			continue
		}

		ctx, cancel := context.WithTimeout(ctx, infoTimeout)
		resp, err := roundTrip(ctx, path, &request{Op: opInfo})
		cancel()

		if err != nil || resp.Info == nil {
			continue
		}

		infos = append(infos, *resp.Info)
	}

	return infos
}
