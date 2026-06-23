#!/usr/bin/env bash
# PostToolUse hook for Edit/Write/MultiEdit.
#
# When Claude edits a Structurizr DSL file under designs/<project>/system/,
# this hook:
#   1. If architecture.dsl was edited, syncs the change to workspace.dsl
#      (workspace.dsl is the parser entry point; architecture.dsl is the
#      canonical source).
#   2. Runs `structurizr-validate` on the edited file.
#   3. On validation failure, exits 2 with the parser output on stderr,
#      which surfaces the error back to Claude as a tool-result error.
#
# Exits 0 (silently) for any tool input that is NOT a *.dsl file under
# methodpoc/designs/<project>/system/. Other Edits/Writes are unaffected.

set -euo pipefail

PAYLOAD="$(cat)"

# Extract the file_path from the tool input. jq returns an empty string if
# the key is missing (e.g., for tools that don't take a file_path).
FILE_PATH="$(jq -r '.tool_input.file_path // ""' <<<"$PAYLOAD")"

if [[ -z "$FILE_PATH" ]]; then
    exit 0
fi

# Only fire on DSL files under methodpoc/designs/<project>/system/.
if [[ ! "$FILE_PATH" =~ /methodpoc/designs/[^/]+/system/.+\.dsl$ ]]; then
    exit 0
fi

DESIGN_DIR="$(dirname "$FILE_PATH")"
BASENAME="$(basename "$FILE_PATH")"

# Find the validation script. It lives at methodpoc/structurizr-validate.
# We climb up from designs/<project>/system to methodpoc/.
METHODPOC_DIR="$(dirname "$(dirname "$DESIGN_DIR")")"
VALIDATE="$METHODPOC_DIR/../structurizr-validate"
# Resolve to absolute path. The hook may be invoked from anywhere.
VALIDATE="$(cd "$(dirname "$VALIDATE")" && pwd)/$(basename "$VALIDATE")"

if [[ ! -x "$VALIDATE" ]]; then
    echo "structurizr-validate not found or not executable at $VALIDATE" >&2
    exit 2
fi

# Sync architecture.dsl → workspace.dsl when architecture.dsl is edited.
# workspace.dsl is what the parser loads; architecture.dsl is the canonical
# source. Keeping them in lockstep avoids stale renders.
if [[ "$BASENAME" == "architecture.dsl" ]] && [[ -f "$DESIGN_DIR/workspace.dsl" ]]; then
    cp "$FILE_PATH" "$DESIGN_DIR/workspace.dsl"
fi

# Validate the file that was just edited. If architecture.dsl was synced,
# workspace.dsl now matches; either is sufficient to validate.
if ! "$VALIDATE" --file "$FILE_PATH" 1>&2; then
    echo "" >&2
    echo "Structurizr DSL validation failed. Fix the parser errors above before continuing." >&2
    echo "Run manually with: ./methodpoc/structurizr-validate <project>" >&2
    exit 2
fi

exit 0
