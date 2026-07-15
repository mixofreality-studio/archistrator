#!/usr/bin/env bash
# mcp-pilot-down.sh — stop everything mcp-pilot-up.sh started.
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN="$ROOT/.mcp-pilot"

if [[ -f "$RUN/pids" ]]; then
  while read -r name pid; do
    [[ -z "${pid:-}" ]] && continue
    if kill "$pid" 2>/dev/null; then echo "stopped $name ($pid)"; fi
  done < "$RUN/pids"
  : > "$RUN/pids"
fi

# Fallback: anything still listening on the pilot ports that we recognize.
for port in 8888 5173 8080; do
  pid=$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | head -1)
  [[ -z "${pid:-}" ]] && continue
  cmd=$(ps -o comm= -p "$pid" 2>/dev/null)
  case "$cmd" in
    *archistrator-server*|*node*|*vite*) kill "$pid" && echo "stopped :$port ($cmd, $pid)";;
    *) echo "left :$port alone (unrecognized process: $cmd, $pid)";;
  esac
done
echo "down."
