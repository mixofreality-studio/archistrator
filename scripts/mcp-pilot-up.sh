#!/usr/bin/env bash
# mcp-pilot-up.sh — boot the full MCP Apps pilot stack in the background.
# Task 11 tooling (docs/superpowers/plans/2026-07-14-mcp-apps-pilot.md).
#
# Starts (detached, logs + pids under .mcp-pilot/):
#   1. archistrator Go server, dev mode, :8888  (WORKSPACE-ACTIVE build — the
#      pinned projectmodel lacks Operation.UI until the Task-12 release)
#   2. webApp production build served via `vite preview` on :5173 (the ui://
#      stub references the BUILT dist/mcp-app.js — the vite dev server cannot
#      serve it; DX earmark in Task 12)
#   3. ext-apps basic-host on :8080, pointed at http://localhost:8888/mcp
#
# Idempotent: reuses any component already healthy on its port.
# Stop everything with scripts/mcp-pilot-down.sh.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN="$ROOT/.mcp-pilot"
EXT_APPS_DIR="${EXT_APPS_DIR:-/tmp/ext-apps}"
mkdir -p "$RUN"
: > "$RUN/pids"

note() { printf '\n== %s\n' "$*"; }
port_busy() { lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1; }
wait_http() { # url, tries, sleep
  local url=$1 tries=$2 pause=${3:-1}
  for _ in $(seq 1 "$tries"); do
    if curl -sf -o /dev/null "$url"; then return 0; fi
    sleep "$pause"
  done
  return 1
}

# --- env: local config + pilot overrides -------------------------------------
if [[ -f "$ROOT/server/.env" ]]; then
  set -a; # shellcheck disable=SC1091
  source "$ROOT/server/.env"; set +a
fi
export ARCHISTRATOR_AUTH_DEV_MODE=true            # dev principal + /mcp dev CORS
export ARCHISTRATOR_WEBAPP_ORIGIN=http://localhost:5173
export ARCHISTRATOR_WEBAPP_ASSET_VERSION=dev
unset GOWORK 2>/dev/null || true                  # workspace must stay ACTIVE

# --- 1. Go server on :8888 ----------------------------------------------------
if curl -sf -o /dev/null http://localhost:8888/healthz; then
  note "server: already healthy on :8888 — reusing"
elif port_busy 8888; then
  echo "FATAL: :8888 is occupied but /healthz does not answer — free it first (scripts/mcp-pilot-down.sh?)" >&2
  exit 1
else
  note "server: building (workspace-active)…"
  (cd "$ROOT/server" && go build -o "$RUN/archistrator-server" ./cmd/server)
  note "server: starting on :8888 (log: .mcp-pilot/server.log)"
  nohup "$RUN/archistrator-server" > "$RUN/server.log" 2>&1 &
  echo "server $!" >> "$RUN/pids"; disown
  wait_http http://localhost:8888/healthz 30 || { echo "FATAL: server never became healthy — see .mcp-pilot/server.log" >&2; exit 1; }
fi

# --- 2. webApp dist (SPA + MCP shell) on :5173 --------------------------------
if curl -sf -o /dev/null http://localhost:5173/mcp-app.js; then
  note "webApp: :5173 already serves mcp-app.js — reusing"
elif port_busy 5173; then
  echo "FATAL: :5173 is busy but does not serve /mcp-app.js (vite DEV server? it can't serve the built bundle) — stop it, then re-run" >&2
  exit 1
else
  note "webApp: building SPA + MCP shell…"
  (cd "$ROOT/webApp" && npm run build) > "$RUN/webapp-build.log" 2>&1 || { echo "FATAL: webApp build failed — see .mcp-pilot/webapp-build.log" >&2; exit 1; }
  note "webApp: serving dist/ on :5173 (log: .mcp-pilot/webapp.log)"
  (cd "$ROOT/webApp" && nohup npx vite preview --port 5173 --strictPort > "$RUN/webapp.log" 2>&1 &
   echo "webapp $!" >> "$RUN/pids")
  wait_http http://localhost:5173/mcp-app.js 20 || { echo "FATAL: :5173/mcp-app.js not served — see .mcp-pilot/webapp.log" >&2; exit 1; }
fi

# --- 3. ext-apps basic-host on :8080 -------------------------------------------
if port_busy 8080; then
  note "basic-host: :8080 already occupied — assuming it's a running basic-host, reusing"
else
  if [[ ! -d "$EXT_APPS_DIR/.git" ]]; then
    note "basic-host: cloning ext-apps into $EXT_APPS_DIR…"
    git clone --depth 1 https://github.com/modelcontextprotocol/ext-apps.git "$EXT_APPS_DIR" > "$RUN/basic-host.log" 2>&1
  fi
  if [[ ! -d "$EXT_APPS_DIR/examples/basic-host/node_modules" ]]; then
    note "basic-host: npm install…"
    (cd "$EXT_APPS_DIR/examples/basic-host" && npm install) >> "$RUN/basic-host.log" 2>&1
  fi
  note "basic-host: starting on :8080 (log: .mcp-pilot/basic-host.log)"
  (cd "$EXT_APPS_DIR/examples/basic-host" && SERVERS='["http://localhost:8888/mcp"]' nohup npm start >> "$RUN/basic-host.log" 2>&1 &
   echo "basichost $!" >> "$RUN/pids")
  wait_http http://localhost:8080 60 || { echo "FATAL: basic-host never answered on :8080 — see .mcp-pilot/basic-host.log" >&2; exit 1; }
fi

note "ALL UP"
cat <<'EOT'
  MCP endpoint   http://localhost:8888/mcp   (dev principal, dev CORS active)
  shell assets   http://localhost:5173/mcp-app.js
  basic-host     http://localhost:8080

Next: open http://localhost:8080, pick systemDesignGetSessionState,
args projectID="archistrator" kind=0 (mission) → expect the committed-artifact
view (no live session on the dogfood project). Stop: scripts/mcp-pilot-down.sh
EOT
