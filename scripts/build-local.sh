#!/usr/bin/env bash
# build-local.sh — the local-first three-binary build (local-first-init-funnel
# Task 4/5/6, docs/superpowers/plans/2026-07-19-local-first-init-funnel.md;
# final-review I1).
#
# Produces THREE artifacts side by side at the repo root — the exact layout
# discovery expects:
#
#   ./archistrator         (cmd/archistrator) — the standalone-daemon CLI
#                           (`archistrator init` scaffolds a .mcp.json HTTP
#                           entry pointing at it; `archistrator serve` then
#                           runs it as a manually-started, long-lived
#                           process — no more Claude-Code auto-spawn).
#                           `locateServerBinary`
#                           (cmd/archistrator/serverchild.go) looks for
#                           archistrator-server as a SIBLING of whichever
#                           binary is currently running, so this file's name
#                           and its directory relative to the other two matter.
#   ./archistrator-server   (cmd/server, -tags localdist) — the SAME
#                           composition root the cloud image runs, built with
#                           the embedded SPA (staged below) so `archistrator
#                           serve` can spawn it as a loopback-bound child.
#   ./aiarch-state-mcp      (cmd/aiarch-state-mcp) — the construct-verb rig
#                           the local construction executor (Task 6) attaches
#                           to headless `claude` via --mcp-config.
#                           `locateStateMCPBinary` (cmd/server/hooks.go) looks
#                           for this as a sibling of the RUNNING
#                           archistrator-server binary — same reason it must
#                           land in this same directory.
#
# Builds the webApp SPA first, stages its dist/ under
# server/cmd/server/webappdist/ (go:embed can only name files within its own
# package directory — it cannot reach ../../../webApp/dist directly, see
# cmd/server/spa_embed.go), then builds archistrator-server with
# `-tags localdist`, which embeds that staged tree.
#
# Cloud images are UNAFFECTED: they build cmd/server without -tags localdist
# (see server/Dockerfile) and never touch webappdist/ (see
# cmd/server/spa_stub.go, the tag's no-op arm) — and never touch this script.
#
# Runs every build WORKSPACE-ACTIVE (does not set GOWORK=off): both
# cmd/server AND cmd/archistrator currently depend on an unreleased platform
# commit (hooks.go's "REVIEWED RESIDUALS" #5 — the claude-local Worker
# Provider preflight, llm.PreflightClaudeCLI, that cmd/archistrator/
# preflight.go ALSO calls), so `go build` on either only succeeds with the
# local archistrator-platform checkout in go.work scope (the repo's default
# dev/CI mode, per go.work at the repo root). cmd/aiarch-state-mcp has no such
# dependency (builds clean GOWORK=off too) but is built the same way here for
# one consistent build mode across all three binaries. This is a pre-existing
# constraint, not something this script introduces.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_APP_DIR="$ROOT/webApp"
SERVER_DIR="$ROOT/server"
EMBED_DIR="$SERVER_DIR/cmd/server/webappdist"

OUT_CLI="$ROOT/archistrator"
OUT_SERVER="$ROOT/archistrator-server"
OUT_STATE_MCP="$ROOT/aiarch-state-mcp"

note() { printf '\n== %s\n' "$*"; }

note "building webApp SPA (npm run build)"
(cd "$WEB_APP_DIR" && npm run build)

note "staging webApp/dist -> ${EMBED_DIR#"$ROOT"/}"
rm -rf "$EMBED_DIR"
cp -R "$WEB_APP_DIR/dist" "$EMBED_DIR"

note "building archistrator-server (go build -tags localdist ./cmd/server)"
(cd "$SERVER_DIR" && go build -tags localdist -o "$OUT_SERVER" ./cmd/server)

note "building archistrator (go build ./cmd/archistrator)"
(cd "$SERVER_DIR" && go build -o "$OUT_CLI" ./cmd/archistrator)

note "building aiarch-state-mcp (go build ./cmd/aiarch-state-mcp)"
(cd "$SERVER_DIR" && go build -o "$OUT_STATE_MCP" ./cmd/aiarch-state-mcp)

note "built ${OUT_CLI#"$ROOT"/}, ${OUT_SERVER#"$ROOT"/}, ${OUT_STATE_MCP#"$ROOT"/}"
