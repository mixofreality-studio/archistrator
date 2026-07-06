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
// MCP server on the SAME manifest-scoping channel the design workflow uses (the twin of
// sourcecontrol.TestDesignWorkflowWiresStateMcp): it obtains the binary, bakes the
// construction ambient session context (incl. AIARCH_TOOL_ALLOWLIST) as env on the MCP
// process, and passes --mcp-config to claude-code-action.
func TestConstructWorkflowWiresStateMcp(t *testing.T) {
	body := constructWorkflowBody(t)

	// Obtains the MCP binary (built from the in-repo source — the construct workflow runs
	// inside the server checkout).
	if !strings.Contains(body, "go build -o") || !strings.Contains(body, "./cmd/aiarch-state-mcp") {
		t.Errorf("construct workflow must build the aiarch-state MCP server from ./cmd/aiarch-state-mcp; got:\n%s", body)
	}

	// The MCP config bakes the CONSTRUCTION ambient context, including the manifest-scoping
	// AIARCH_TOOL_ALLOWLIST channel (agentic-managers item 5).
	for _, key := range []string{
		"AIARCH_PROJECT_ID", "AIARCH_JOB_MODE", "AIARCH_COMPONENT_ID",
		"AIARCH_ACTIVITY_ID", "AIARCH_TARGET_BRANCH", "AIARCH_TOOL_ALLOWLIST", "AIARCH_STATE_ROOT",
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

	// The allowlist is sourced from the tool_allowlist dispatch input — the same channel the
	// construction Manager resolves through resolvedToolAllowlist / dispatchInputsFor.
	if !strings.Contains(body, "${{ inputs.tool_allowlist }}") {
		t.Error("MCP config must source AIARCH_TOOL_ALLOWLIST from the tool_allowlist dispatch input")
	}

	// The workflow declares the tool_allowlist workflow_dispatch input (the binding contract
	// with dispatchInputsFor's dispatchInputToolAllowlist key).
	if !strings.Contains(body, "tool_allowlist:") {
		t.Error("construct workflow must declare the tool_allowlist workflow_dispatch input")
	}
	if dispatchInputToolAllowlist != "tool_allowlist" {
		t.Errorf("dispatchInputToolAllowlist = %q; the workflow input is named tool_allowlist", dispatchInputToolAllowlist)
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
