#!/usr/bin/env bash
# build-local.sh — the local-first single-binary build (local-first-init-funnel
# Task 4, docs/superpowers/plans/2026-07-19-local-first-init-funnel.md).
#
# Builds the webApp SPA, stages its dist/ under server/cmd/server/webappdist/
# (go:embed can only name files within its own package directory — it cannot
# reach ../../../webApp/dist directly, see cmd/server/spa_embed.go), then
# builds the server with `-tags localdist`, which embeds that staged tree.
# Produces ONE artifact at the repo root: ./archistrator — the server + the
# embedded SPA + /api + /mcp, all one process (Serena pattern: `archistrator
# mcp`/`archistrator init`, Task 5, will run out of this same binary).
#
# Cloud images are UNAFFECTED: they build without -tags localdist (see
# server/Dockerfile) and never touch webappdist/ (see cmd/server/spa_stub.go,
# the tag's no-op arm).
#
# Runs the server build WORKSPACE-ACTIVE (does not set GOWORK=off): cmd/server
# currently depends on an unreleased platform commit (hooks.go's "REVIEWED
# RESIDUALS" #4/#5 — the composegen profile-gated-pool + claude-local provider
# patches), so `go build ./cmd/server` only succeeds with the local
# archistrator-platform checkout in go.work scope (the repo's default dev/CI
# mode, per go.work at the repo root). This is a pre-existing constraint, not
# something this script introduces.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_APP_DIR="$ROOT/webApp"
SERVER_DIR="$ROOT/server"
EMBED_DIR="$SERVER_DIR/cmd/server/webappdist"
OUT_BIN="$ROOT/archistrator"

note() { printf '\n== %s\n' "$*"; }

note "building webApp SPA (npm run build)"
(cd "$WEB_APP_DIR" && npm run build)

note "staging webApp/dist -> ${EMBED_DIR#"$ROOT"/}"
rm -rf "$EMBED_DIR"
cp -R "$WEB_APP_DIR/dist" "$EMBED_DIR"

note "building server (go build -tags localdist)"
(cd "$SERVER_DIR" && go build -tags localdist -o "$OUT_BIN" ./cmd/server)

note "built ${OUT_BIN#"$ROOT"/}"
