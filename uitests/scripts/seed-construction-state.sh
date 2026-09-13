#!/usr/bin/env bash
# seed-construction-state.sh — build the DOGFOOD-SEEDED project-state repo the
# construction specs run against (the `uitests-construction` CI job, and the same
# thing locally).
#
#   usage: seed-construction-state.sh [--episodes] <bare-repo-path>
#
# It creates a NEW bare git repo at <bare-repo-path> whose one commit on `main`
# holds this checkout's own .aiarch/state/project.json, with `id` forced to
# "archistrator" (the id the specs' content gates read — see
# uitests/tests/support/gating.ts). The file is taken LIVE from the working tree,
# never from a frozen fixture, so the specs always run against what the commit
# under test says the dogfood construction phase looks like.
#
# --episodes also seeds the episode ledger episodes-panel.spec.ts reads, via
# server/cmd/gen-uitests-episodes (run from server/: it resolves its default trace
# fixture relative to that directory). The ledger lives on disk under
# <bare-repo-path>/.aiarch/traces, where the server's episodeAccess looks for it.
#
# Point the server at the result with
#   ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true
#   ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL=file://<bare-repo-path>
#
# THIS REPO IS NOT FOR THE PROJECT-CREATION SPECS. Under the LOCAL profile every
# project id resolves to this one repo, so CreateProject against it hard-fails
# (guardProjectIdentity: the repo already holds "archistrator"). Those specs need
# the fresh empty repo the `uitests` job builds. The two configurations are
# mutually exclusive: see uitests/README.md and .github/workflows/uitests.yml.
#
# Override the source file with SEED_PROJECT_JSON=<path> (default: this checkout's).
set -euo pipefail

episodes=0
if [[ "${1:-}" == "--episodes" ]]; then
  episodes=1
  shift
fi
if [[ $# -ne 1 ]]; then
  echo "usage: $0 [--episodes] <bare-repo-path>" >&2
  exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
bare="$1"
source_json="${SEED_PROJECT_JSON:-$root/.aiarch/state/project.json}"

if [[ ! -f "$source_json" ]]; then
  echo "seed-construction-state: no project.json at $source_json" >&2
  exit 1
fi
# Refuse to reuse an existing repo: a stale seed would test old state and still pass.
if [[ -e "$bare" ]]; then
  echo "seed-construction-state: $bare already exists; remove it first (a stale seed tests stale state)" >&2
  exit 1
fi

bare="$(mkdir -p "$bare" && cd "$bare" && pwd)"
git init -q --bare --initial-branch=main "$bare"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
git clone -q "$bare" "$work/seed" 2>/dev/null
git -C "$work/seed" checkout -q -B main
mkdir -p "$work/seed/.aiarch/state"
jq '.id = "archistrator"' "$source_json" >"$work/seed/.aiarch/state/project.json"

# The content gates need a non-empty system-test plan; fail here, where the cause
# is obvious, rather than in 80 specs.
scenarios="$(jq '.testingState.systemTestPlan.scenarios // [] | length' "$work/seed/.aiarch/state/project.json")"
if [[ "$scenarios" -eq 0 ]]; then
  echo "seed-construction-state: $source_json has no testingState.systemTestPlan.scenarios; the construction specs would have nothing to assert" >&2
  exit 1
fi

src_rev="$(git -C "$root" rev-parse --short HEAD 2>/dev/null || echo unknown)"
git -C "$work/seed" add -A
git -C "$work/seed" -c user.name=uitests -c user.email=uitests@aiarch.local \
  commit -q -m "seed: dogfood project state from $src_rev"
git -C "$work/seed" push -q origin main

echo "seeded $bare from $source_json ($src_rev, id=archistrator, $scenarios system-test scenarios)"

if [[ $episodes -eq 1 ]]; then
  (cd "$root/server" && GOWORK=off go run ./cmd/gen-uitests-episodes -repo "$bare")
  echo "seeded the episode ledger under $bare/.aiarch/traces"
fi
