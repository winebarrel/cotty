#!/bin/bash
# Sets up the tmux session that demo.tape records: the agent on the left and
# two terminals on the right. agent.py in the left pane plays both the agent
# and the user once vhs attaches.
set -e
D=$(cd "$(dirname "$0")" && pwd)
SOCK=cottydemo

go build -o "$D/cotty" "$D/../cmd/cotty"

tmux -L $SOCK kill-server 2>/dev/null || true
rm -rf /tmp/cottydemo.*

# A short COTTY_HOME keeps the socket paths within their length limit, and a
# HOME of its own keeps your shell settings out of the recording.
CH=$(mktemp -d /tmp/cottydemo.XXXX)
export HOME="$D/home" COTTY_HOME="$CH" PATH="$D:$PATH" BASH_SILENCE_DEPRECATION_WARNING=1

T="tmux -L $SOCK -f /dev/null"
$T new-session -d -s demo -x 150 -y 40 bash --norc --noprofile
AGENT=$($T display -p -t demo '#{pane_id}')
WEB=$($T split-window -h -l 55% -t "$AGENT" -P -F '#{pane_id}' bash)
DB=$($T split-window -v -t "$WEB" -P -F '#{pane_id}' bash)

$T set -g status-position top
$T set -g status-style 'bg=colour24,fg=colour231,bold'
$T set -g status-left-length 150
$T set -g status-left ' '
$T set -g status-right ''
$T set -g window-status-format ''
$T set -g window-status-current-format ''
$T set -g pane-border-status top
$T set -g pane-border-format ' #{pane_title} '
$T set -g pane-active-border-style 'fg=colour245'
$T set -g allow-set-title off
$T select-pane -t "$AGENT" -T 'agent'
$T select-pane -t "$WEB" -T 'your terminal 1'
$T select-pane -t "$DB" -T 'your terminal 2'

$T send-keys -t "$AGENT" "clear; python3 $D/agent.py $SOCK $WEB $DB" Enter
$T send-keys -t "$WEB" clear Enter
$T send-keys -t "$DB" clear Enter
