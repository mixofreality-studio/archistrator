#!/usr/bin/env bash
#
# temporal-ui.sh — browse every wire-test workflow execution + full event
# history. The systemtests Temporal is a LOCAL dev server (temporal-dev.sh)
# persisting to .temporal/aiarch-test.db with the web UI on :8233. This ensures
# it is up and opens the UI — the systemtests port of the server module's
# `make temporal-ui`.
#
# Workflow:
#   1. make test-integration     # populates .temporal/aiarch-test.db
#   2. make temporal-ui          # opens http://localhost:8233 (namespace aiarch-test)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UI_PORT="${UI_PORT:-8233}"
URL="http://localhost:${UI_PORT}"

# Ensure the dev server (persistent DB + UI) is running.
"${SCRIPT_DIR}/temporal-dev.sh"

echo "Temporal UI:  ${URL}  (namespace aiarch-test)"

# Open the browser (best-effort, cross-platform).
if command -v open >/dev/null 2>&1; then
  exec open "${URL}"
elif command -v xdg-open >/dev/null 2>&1; then
  exec xdg-open "${URL}"
else
  echo "Open ${URL} in your browser."
fi
