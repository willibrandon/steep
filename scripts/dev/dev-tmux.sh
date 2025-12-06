#!/bin/bash
# Steep Development tmux Session
#
# Usage: ./scripts/dev-tmux.sh [--debug]
#
# Layout:
# ┌───────────┬───────────┬───────────┐
# │ source    │ target    │ agent     │
# └───────────┴───────────┴───────────┘
#
# Ctrl+b d = detach (keeps running)
# tmux attach -t steep = reattach
# tmux kill-session -t steep = stop everything

cd /Users/brandon/src/steep

DEBUG_FLAG=""
if [[ "$1" == "--debug" || "$DEBUG" == "1" ]]; then
  DEBUG_FLAG="--debug"
fi

tmux kill-session -t steep 2>/dev/null

tmux new-session -d -s steep -n dev \
  "PGPASSWORD=test ./bin/steep-repl run --config ./configs/test/source.yaml $DEBUG_FLAG; read"

tmux split-window -h -t steep:dev \
  "PGPASSWORD=test ./bin/steep-repl run --config ./configs/test/target.yaml $DEBUG_FLAG; read"

tmux split-window -h -t steep:dev \
  "make run-agent-dev ARGS=\"$DEBUG_FLAG\"; read"

tmux select-layout -t steep:dev even-horizontal

tmux attach -t steep
