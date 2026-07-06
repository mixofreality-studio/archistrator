package construction

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// constructWorkflowBody reads archistrator's OWN aiarch-construct.yml, located relative
// to this test file (robust to the test's working directory). It is the static workflow
// the construction dispatch (dispatchInputsFor) drives.
func constructWorkflowBody(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// server/internal/manager/construction → repo root is four levels up.
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", ".github", "workflows", "aiarch-construct.yml")
	b, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative test fixture path
	if err != nil {
		t.Fatalf("read aiarch-construct.yml: %v", err)
	}
	return string(b)
}

// TestConstructWorkflowWiresStateMcp asserts aiarch-construct.yml wires the aiarch-state
// MCP server (the twin of sourcecontrol.TestDesignWorkflowWiresStateMcp): it obtains the
// binary, bakes the construction ambient session context as env on the MCP process, and
// passes --mcp-config to claude-code-action.
func TestConstructWorkflowWiresStateMcp(t *testing.T) {
	body := constructWorkflowBody(t)

	// Obtains the MCP binary (built from the in-repo source — the construct workflow runs
	// inside the server checkout).
	if !strings.Contains(body, "go build -o") || !strings.Contains(body, "./cmd/aiarch-state-mcp") {
		t.Errorf("construct workflow must build the aiarch-state MCP server from ./cmd/aiarch-state-mcp; got:\n%s", body)
	}

	// The MCP config bakes the CONSTRUCTION ambient context.
	for _, key := range []string{
		"AIARCH_PROJECT_ID", "AIARCH_JOB_MODE", "AIARCH_COMPONENT_ID",
		"AIARCH_ACTIVITY_ID", "AIARCH_TARGET_BRANCH", "AIARCH_STATE_ROOT",
	} {
		if !strings.Contains(body, key) {
			t.Errorf("MCP config must set %s on the aiarch-state server process", key)
		}
	}

	// Construct job mode (the construction session context is keyed by component/activity,
	// not an artifact kind).
	if !strings.Contains(body, `"AIARCH_JOB_MODE": "construct"`) {
		t.Error("MCP config must set AIARCH_JOB_MODE to construct")
	}

	// --mcp-config wires the server into the Claude CLI.
	if !strings.Contains(body, "--mcp-config") {
		t.Error("claude-code-action must wire the aiarch-state MCP server via --mcp-config")
	}
}

// TestConstructWorkflowKeepsLoadBearingAnchors guards the dispatch contract the MCP wiring
// must not have disturbed: the idempotency run-name anchor and the additive dispatch
// inputs.
func TestConstructWorkflowKeepsLoadBearingAnchors(t *testing.T) {
	body := constructWorkflowBody(t)
	for _, anchor := range []string{
		"run-name: aiarch-cp-${{ inputs.idempotency_token }}",
		"idempotency_token:",
		"activity_id:",
		"component_id:",
		"/${{ inputs.command }} ${{ inputs.component_id }} ${{ inputs.activity_id }}",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("construct workflow lost a load-bearing anchor: %q", anchor)
		}
	}
}
