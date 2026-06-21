#!/usr/bin/env bash
#
# temporal-dev.sh — ensure a LOCAL Temporal dev server is running with a
# persistent SQLite DB + the web UI. This is the systemtests Temporal provider:
# `temporal server start-dev` from the temporal CLI you already have, mirroring
# the server module's persistent .temporal/aiarch-test.db model (no container).
#
# Idempotent: if a frontend is already answering on the port it is REUSED;
# otherwise the dev server is started in the background (pid in .temporal/dev.pid,
# logs in .temporal/dev.log) and we block until it is healthy.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

NAMESPACE="aiarch-test"
FRONTEND_PORT="${FRONTEND_PORT:-7233}"
UI_PORT="${UI_PORT:-8233}"
ADDR="127.0.0.1:${FRONTEND_PORT}"

DB_DIR="${MODULE_ROOT}/.temporal"
DB_FILE="${DB_DIR}/aiarch-test.db"
PID_FILE="${DB_DIR}/dev.pid"
LOG_FILE="${DB_DIR}/dev.log"

if ! command -v temporal >/dev/null 2>&1; then
  echo "error: 'temporal' CLI not found on PATH — install it: https://docs.temporal.io/cli#install" >&2
  exit 1
fi

# Already serving? Reuse it (the persistent DB means prior history is intact).
if temporal operator cluster health --address "${ADDR}" >/dev/null 2>&1; then
  echo "Temporal dev server already running at ${ADDR} (namespace ${NAMESPACE}); reusing."
  exit 0
fi

mkdir -p "${DB_DIR}"
echo "Starting Temporal dev server — DB ${DB_FILE}, frontend ${ADDR}, UI http://localhost:${UI_PORT} ..."
nohup temporal server start-dev \
  --db-filename "${DB_FILE}" \
  --namespace "${NAMESPACE}" \
  --ip 127.0.0.1 \
  --port "${FRONTEND_PORT}" \
  --ui-port "${UI_PORT}" \
  --log-level error \
  >"${LOG_FILE}" 2>&1 &
echo $! >"${PID_FILE}"

# Block until the frontend answers (or give up and surface the log).
for _ in $(seq 1 60); do
  if temporal operator cluster health --address "${ADDR}" >/dev/null 2>&1; then
    echo "Temporal dev server ready (namespace ${NAMESPACE}); UI http://localhost:${UI_PORT}."
    exit 0
  fi
  sleep 1
done

echo "error: Temporal dev server did not become healthy within 60s; see ${LOG_FILE}" >&2
exit 1
