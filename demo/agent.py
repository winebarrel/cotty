"""Demo timeline. Runs in the left tmux pane as the "agent".

It drives cotty mcp over stdio like an MCP client would, types into the
right panes with tmux send-keys to play the human, and sets the caption in
the tmux status line.
"""

import json
import os
import subprocess
import sys
import time

SOCK = sys.argv[1]
WEB, DB = sys.argv[2], sys.argv[3]

CYAN = "\033[36m"
GREEN = "\033[32m"
RED = "\033[31m"
DIM = "\033[2m"
BOLD = "\033[1m"
RESET = "\033[0m"


def tmux(*args):
    subprocess.run(["tmux", "-L", SOCK, *args], check=True)


def caption(text):
    tmux("set", "-g", "status-left", f" {text} ")


def human_type(pane, text, enter=True):
    for ch in text:
        tmux("send-keys", "-t", pane, "-l", ch)
        time.sleep(0.05)
    if enter:
        time.sleep(0.2)
        tmux("send-keys", "-t", pane, "Enter")


class MCP:
    def __init__(self):
        self.p = subprocess.Popen(["cotty", "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
        self.id = 0
        self.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                "clientInfo": {"name": "demo", "version": "0"}})
        self.p.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
        self.p.stdin.flush()

    def rpc(self, method, params):
        self.id += 1
        self.p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": self.id, "method": method, "params": params}) + "\n")
        self.p.stdin.flush()
        while True:
            msg = json.loads(self.p.stdout.readline())
            if msg.get("id") == self.id:
                return msg["result"]

    def tool(self, name, quiet=False, **args):
        if not quiet:
            shown = ", ".join(f"{k}={json.dumps(v, ensure_ascii=False)}" for k, v in args.items())
            print(f"{CYAN}{BOLD}> {name}{RESET}{CYAN}({shown}){RESET}", flush=True)
            time.sleep(0.6)
        res = self.rpc("tools/call", {"name": name, "arguments": args})
        text = res["content"][0]["text"]
        color = RED if res.get("isError") else DIM
        return text, color


def show(text, color):
    for line in text.rstrip("\n").split("\n"):
        print(f"  {color}{line}{RESET}", flush=True)
    print(flush=True)


def main():
    # Wait for vhs to attach so the timeline starts on camera.
    while subprocess.run(["tmux", "-L", SOCK, "list-clients"], capture_output=True, text=True).stdout.strip() == "":
        time.sleep(0.2)

    print(f"{BOLD}Agent{RESET} {DIM}(an MCP client talking to `cotty mcp`){RESET}\n", flush=True)

    caption("cotty: share your terminals with an AI agent")
    time.sleep(3)

    caption("1. Start each terminal under `cotty wrap -n <name>`")
    time.sleep(1)
    human_type(WEB, "cotty wrap -n web1 bash")
    time.sleep(0.8)
    human_type(DB, "cotty wrap -n db1 bash")
    time.sleep(2.5)

    mcp = MCP()

    caption("2. The agent finds them with list_sessions")
    time.sleep(1)
    text, color = mcp.tool("list_sessions")
    names = ", ".join(s["name"] for s in json.loads(text)["sessions"])
    show(f"sessions: {names}", color)
    time.sleep(2)

    caption("3. The agent types into each terminal (send) and reads the output (read)")
    time.sleep(1)
    # Start the cursors at what is already on screen.
    mcp.tool("read", quiet=True, session="web1")
    mcp.tool("read", quiet=True, session="db1")

    mcp.tool("send", session="web1", text="uname -sm")
    show(*mcp.tool("read", session="web1", idle_ms=500))
    time.sleep(1)

    mcp.tool("send", session="db1", text="echo $COTTY_SESSION; date +%F")
    show(*mcp.tool("read", session="db1", idle_ms=500))
    time.sleep(2)

    caption("4. You can type too, and the agent sees it")
    time.sleep(1)
    human_type(WEB, "echo hello from the human")
    time.sleep(1)
    show(*mcp.tool("read", session="web1", idle_ms=500))
    time.sleep(2)

    caption("5. Ctrl-] a denies the agent's input in that terminal")
    time.sleep(1.5)
    tmux("send-keys", "-t", DB, "C-]")
    time.sleep(0.3)
    tmux("send-keys", "-t", DB, "a")
    time.sleep(2)
    show(*mcp.tool("send", session="db1", text="rm -rf /tmp/data"))
    time.sleep(3)

    caption("6. Ctrl-] a again allows it")
    time.sleep(1.5)
    tmux("send-keys", "-t", DB, "C-]")
    time.sleep(0.3)
    tmux("send-keys", "-t", DB, "a")
    time.sleep(2)
    mcp.tool("send", session="db1", text="echo allowed again")
    show(*mcp.tool("read", session="db1", idle_ms=500))
    time.sleep(2)

    caption("Every session is also logged as plain text under ~/.cotty/log")
    time.sleep(4)
    # vhs stops recording on its own; clean up after it has.
    time.sleep(15)
    tmux("kill-server")


main()
