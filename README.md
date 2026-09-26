# cotty

[![CI](https://github.com/winebarrel/cotty/actions/workflows/ci.yml/badge.svg)](https://github.com/winebarrel/cotty/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/winebarrel/cotty/graph/badge.svg?token=0LCVFJMFeC)](https://codecov.io/gh/winebarrel/cotty)

cotty shares a terminal session between you and an AI agent.

You start a command such as `ssh` or a serial console under `cotty wrap` and use it as usual. The agent reads the same output and types into the same session through an MCP server, `cotty mcp`. Both of you can type at any time, and you can deny the agent's input with a key.

```
[your terminal]                          [agent, e.g. Claude Code]
 cotty wrap -n web1 ssh web1                    | stdio
   runs ssh in a pty                          cotty mcp
   listens on ~/.cotty/sock/web1.sock <-------'
```

## Installation

```sh
go install github.com/winebarrel/cotty/cmd/cotty@latest
```

Or download a binary from the [releases](https://github.com/winebarrel/cotty/releases).

## Usage

Start a session with a name:

```sh
cotty wrap -n web1 ssh web1.example.com
cotty wrap -n board picocom -b 115200 /dev/tty.usbserial-0001
```

Register the MCP server with the agent, for Claude Code:

```sh
claude mcp add cotty -- cotty mcp
```

Then ask the agent to work in the session, for example "check the disk usage on web1".

Flags of `cotty wrap` go before the command; everything from the command on is passed to it as is.

### Options

| Flag | Environment variable | Default | Description |
|------|----------------------|---------|-------------|
| `--home` | `COTTY_HOME` | `~/.cotty` | Directory for session sockets and logs. |
| `wrap --buffer-size` | `COTTY_BUFFER_SIZE` | `1MiB` | How much recent output to keep for the agent to read. |
| `mcp --read-max` | `COTTY_READ_MAX` | `32KiB` | Most output one read returns. Older output beyond it is dropped. |
| `mcp --tail-lines` | `COTTY_TAIL_LINES` | `50` | Lines a read returns when it has no previous read to continue from. |
| `mcp --timeout` | `COTTY_TIMEOUT` | `30s` | How long a read waits when the agent does not say. |
| `mcp --max-timeout` | `COTTY_MAX_TIMEOUT` | `10m` | Longest wait the agent may ask a read for. |

Sizes take a unit of B, K, KB, KiB, M, MB or MiB, all powers of 1024. To change the `mcp` options, pass them when registering the server:

```sh
claude mcp add cotty -- cotty mcp --read-max 64KiB --max-timeout 30m
```

```
Usage: cotty <command> [flags]

Share a terminal session with an AI agent.

Flags:
  -h, --help               Show context-sensitive help.
      --version
      --home="~/.cotty"    Directory for session sockets and logs ($COTTY_HOME).

Commands:
  wrap --name=STRING <command> ... [flags]
    Run a command in a terminal shared with the agent.

  mcp [flags]
    Serve the sessions to an agent over MCP on stdio.
```

### Key bindings

| Keys | Action |
|------|--------|
| `Ctrl-]` `a` | Allow or deny the agent's input (allowed at start) |
| `Ctrl-]` `?` | Show help |
| `Ctrl-]` `Ctrl-]` | Send `Ctrl-]` to the command |

### MCP tools

| Tool | Description |
|------|-------------|
| `list_sessions` | List the running sessions. |
| `send` | Type text into a session, followed by Enter unless `enter` is false. |
| `send_key` | Press a special key such as `ctrl-c`, `up` or `tab`. |
| `read` | Read the output that arrived since the previous read, as plain text. `wait_for` waits for a regular expression, `idle_ms` waits for the output to settle, and `tail_lines` returns the last lines instead. |

To confirm each input while letting the agent read freely, allow only the read-only tools in Claude Code's permissions:

```json
{
  "permissions": {
    "allow": ["mcp__cotty__list_sessions", "mcp__cotty__read"]
  }
}
```

### Files

- `~/.cotty/sock/<name>.sock`: the session socket, removed when the session ends. Only you can connect to it.
- `~/.cotty/log/<name>-<YYYYMMDD-HHMMSS>.log`: the session output as plain text, with escape sequences removed.

The command runs with `COTTY_SESSION=<name>` in its environment.

## Notes

- The agent sees the output from the start of the session, up to `--buffer-size`. The log keeps all of it.
- Passwords you type at a prompt that turns echo off, as `ssh` and `sudo` do, never appear in the output, so the agent does not see them.
