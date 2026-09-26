// Package cotty shares a terminal session between a person and an AI agent.
//
// The person starts a program under `cotty wrap` and uses it as usual. The
// wrapper listens on a Unix socket, through which `cotty mcp` lets the agent
// read the program's output and type into it. There is no daemon: each
// wrapper owns its session, and the socket directory is the list of sessions.
package cotty

import (
	"io"
	"os"
	"path/filepath"
)

// Context carries what commands need from the process they run in.
type Context struct {
	// Home is the directory holding the sockets and logs.
	Home      string
	Version   string
	Stdin     *os.File
	Stdout    io.Writer
	ErrOutput io.Writer
}

func (c *Context) sockDir() string {
	return filepath.Join(c.Home, "sock")
}

func (c *Context) logDir() string {
	return filepath.Join(c.Home, "log")
}
