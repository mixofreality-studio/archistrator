#!/usr/bin/env bash
#
# temporal-dev-stop.sh — stop the local Temporal dev server started by
# temporal-dev.sh. The persistent SQLite DB (.temporal/aiarch-test.db) is KEPT,
# so workflow history survives a stop/start for later `make temporal-ui` browsing.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
PID_FILE="${MODULE_ROOT}/.temporal/dev.pid"

if [[ ! -f "${PID_FILE}" ]]; then
  echo "No Temporal dev server pid file; nothing to stop."
  exit 0
fi

PID="$(cat "${PID_FILE}")"
if [[ -n "${PID}" ]] && kill -0 "${PID}" 2>/dev/null; then
  echo "Stopping Temporal dev server (pid ${PID})."
  kill "${PID}" 2>/dev/null || true
fi
rm -f "${PID_FILE}"
