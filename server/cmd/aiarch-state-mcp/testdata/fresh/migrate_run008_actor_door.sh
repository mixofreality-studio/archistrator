#!/usr/bin/env bash
# Reproduces the run008 fixture migration: ./migrate_run008_actor_door.sh run008.project.json
# The output keeps the fixture's form (2-space indent, Go's HTML-escaping of & < >, no
# trailing newline), so the diff against the pre-migration file is the migration alone.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
f=${1:?path to run008.project.json}
tmp=$(mktemp)
jq -f "$here/migrate_run008_actor_door.jq" "$f" | sed 's/&/\\u0026/g; s/</\\u003c/g; s/>/\\u003e/g' | perl -0pe 's/\n\z//' > "$tmp"
mv "$tmp" "$f"
