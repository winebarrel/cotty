package main

import (
	"errors"
	"os"

	"github.com/alecthomas/kong"
	"github.com/winebarrel/cotty"
)

// version is stamped in by GoReleaser at release time.
var version string

var cli struct {
	Version kong.VersionFlag

	Home string `env:"COTTY_HOME" default:"~/.cotty" type:"path" help:"Directory for session sockets and logs."`

	Wrap cotty.WrapCmd `cmd:"" help:"Run a command in a terminal shared with the agent."`
	MCP  cotty.MCPCmd  `cmd:"" name:"mcp" help:"Serve the sessions to an agent over MCP on stdio."`
}

func main() {
	v := resolveVersion(version)

	kctx := kong.Parse(&cli,
		kong.Name("cotty"),
		kong.Description("Share a terminal session with an AI agent."),
		kong.Vars{"version": v},
	)

	err := kctx.Run(&cotty.Context{
		Home:      cli.Home,
		Version:   v,
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		ErrOutput: os.Stderr,
	})

	// The wrapped command's exit status is passed on without a message; the
	// command has already said what went wrong.
	var exitErr *cotty.ExitError

	if errors.As(err, &exitErr) {
		os.Exit(exitErr.Code)
	}

	kctx.FatalIfErrorf(err)
}
