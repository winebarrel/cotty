---
name: cotty
description: Work in terminals the user shares through cotty, such as ssh logins, serial consoles and network devices. Use when the user asks you to run or check something on a remote host, a device, or a named cotty session, or mentions cotty.
---

# cotty

The user starts a command such as `ssh` or a serial console under `cotty wrap` in their own terminal, and the cotty MCP server lets you read its output and type into it. The user sees everything you type and can type at any time.

The MCP tools are `list_sessions`, `send`, `send_key` and `read`. Your client may show them with a prefix.

## Setup

If the tools are not available, check whether `cotty` is installed with `cotty --version`. If it is not, ask the user to install it:

```sh
go install github.com/winebarrel/cotty/cmd/cotty@latest
```

Binaries are also at https://github.com/winebarrel/cotty/releases. Then the MCP server, which is the command `cotty mcp`, has to be registered with your client. Leave installing and registering to the user.

## Find the session

1. Call `list_sessions`.
2. If the session you need is not there, ask the user to start it in their terminal and wait for them to say it is ready. You cannot start it yourself, because it runs in the user's terminal.

   ```sh
   cotty wrap -n web1 ssh web1.example.com
   cotty wrap -n board picocom -b 115200 /dev/tty.usbserial-0001
   ```

## Run a command

1. `read` the session first to see what state it is in: a shell prompt, a pager, a password prompt, a program still running.
2. `send` one command. Enter is pressed after it unless you pass `enter: false`.
3. `read` the result. Use `wait_for` with a regular expression for the prompt when you know it, or `idle_ms` (around 1000) to wait until the output stops. Raise `timeout_ms` for slow commands.
4. Check the result before sending the next command.

`read` returns only the output that arrived since your previous read of that session, so nothing is missed between calls. Pass `tail_lines` to look at the last lines again.

## Interactive programs

`read` gives you the output as a plain text stream with escape sequences removed, not a picture of the screen. Full-screen programs such as `vim`, `top`, `htop` and `less` are hard to follow that way, so prefer non-interactive forms:

- `top -b -n 1` instead of `top`
- `systemctl --no-pager`, `journalctl --no-pager`, `git --no-pager`
- `PAGER=cat`, or `cat` instead of `less`
- `terminal length 0` (or the device's equivalent) on network devices that page with `--More--`

If something is stuck or waiting, `send_key` `ctrl-c`, or `q` for a pager.

## Safety

- These are real machines, often in production. Before a command that changes anything (deleting files, restarting services, rebooting, changing configuration, writing to a device), tell the user what you will run and wait for their OK.
- Never type passwords, passphrases or other secrets. At a password prompt, ask the user to type it in their terminal, then `read` to continue.
- If `send` fails because agent input is denied, the user has blocked your input with Ctrl-] a. Stop and ask the user; do not retry.
- `[REDACTED]` in the output is text the user hid with `--redact`, such as a password or token; `list_sessions` shows in `redacts` how many strings a session hides. Do not try to reveal it, for example by printing it another way. If you need the value, ask the user.
- If `read` says the user paused the output, the user is hiding something from you with Ctrl-] p. Do not send anything, since you cannot see the result; wait for the user to resume it.
- If a session is not running, it has ended. Ask the user whether to start it again.
