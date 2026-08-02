package main

import (
	"context"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// toolByComponent returns the first catalog tool for the given component, failing the
// test if there is none — so a test names a component, not a brittle tool name.
func toolByComponent(t *testing.T, component string) projectstate.InternalTool {
	t.Helper()
	for _, tl := range projectstate.InternalToolCatalog() {
		if tl.Component == component {
			return tl
		}
	}
	t.Fatalf("no catalog tool for component %q", component)
	return projectstate.InternalTool{}
}

// TestExecuteEngineTool_LiveDispatch proves the generic reflection invoker runs a
// real Engine operation in process: interventionEngine.DecideOnVariance with a valid
// variance + policy returns a directive and no error. This is the end-to-end proof the
// execution rail binds named args to the live Go method's positional params.
func TestExecuteEngineTool_LiveDispatch(t *testing.T) {
	tool, ok := projectstate.InternalToolByName("interventionDecideOnVariance")
	if !ok {
		t.Fatal("interventionDecideOnVariance not in catalog")
	}
	res, err := executeRawTool(context.Background(), nil, tool, map[string]any{
		"variance": map[string]any{
			"ProjectID":    "P1",
			"ActivityID":   "C-PE",
			"Kind":         2, // WorkerMiss
			"AttemptCount": 0,
			"Severity":     0,
			"Policy":       map[string]any{"Mode": 2, "RetryBudget": 2}, // Tiered
		},
	})
	if err != nil {
		t.Fatalf("live engine dispatch failed: %v", err)
	}
	if res == nil {
		t.Fatal("expected a VarianceDirective result, got nil")
	}
}

// TestExecuteEngineTool_SurfacesDomainError proves a domain-level engine error (a
// ContractMisuse from empty input) is surfaced through the invoker rather than
// swallowed — the method really ran.
func TestExecuteEngineTool_SurfacesDomainError(t *testing.T) {
	tool, _ := projectstate.InternalToolByName("interventionDecideOnVariance")
	_, err := executeRawTool(context.Background(), nil, tool, map[string]any{
		"variance": map[string]any{}, // empty ProjectID/ActivityID → ContractMisuse
	})
	if err == nil {
		t.Fatal("expected the engine's ContractMisuse error to surface")
	}
	if !strings.Contains(err.Error(), "ProjectID") {
		t.Fatalf("expected the engine's own error, got: %v", err)
	}
}

// TestExecuteRawTool_UnavailableInSubstrate proves an external-RA op returns a typed,
// documented unavailable-in-substrate error (naming the missing dependency) instead
// of an I/O failure or a stub success.
func TestExecuteRawTool_UnavailableInSubstrate(t *testing.T) {
	for _, component := range []string{"sourceControlAccess", "artifactAccess", "merchantGatewayAccess", "usageAccess"} {
		tool := toolByComponent(t, component)
		_, err := executeRawTool(context.Background(), nil, tool, nil)
		if err == nil {
			t.Fatalf("%s: expected unavailable-in-substrate error", component)
		}
		if !strings.Contains(err.Error(), "unavailable in this substrate") {
			t.Fatalf("%s: error must say unavailable in this substrate, got: %v", component, err)
		}
	}
}

// TestExecuteRawTool_AgentHiddenRefused proves a hidden op never executes even if it
// somehow reaches the rail.
func TestExecuteRawTool_AgentHiddenRefused(t *testing.T) {
	tool, ok := projectstate.InternalToolByName("projectStateCommitArtifact")
	if !ok {
		t.Fatal("projectStateCommitArtifact not in catalog")
	}
	if !tool.AgentHidden {
		t.Fatal("fixture precondition: projectStateCommitArtifact must be agent-hidden")
	}
	_, err := executeRawTool(context.Background(), nil, tool, nil)
	if err == nil || !strings.Contains(err.Error(), "agent-hidden") {
		t.Fatalf("expected agent-hidden refusal, got: %v", err)
	}
}

// TestExecuteProjectStateRead_ReadProject proves a projectStateAccess read serves the
// checked-out project directly from the session (the in-substrate RA read).
func TestExecuteProjectStateRead_ReadProject(t *testing.T) {
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	tool, ok := projectstate.InternalToolByName("projectStateReadProject")
	if !ok {
		t.Fatal("projectStateReadProject not in catalog")
	}
	res, err := executeRawTool(context.Background(), s, tool, map[string]any{"projectID": "testproj"})
	if err != nil {
		t.Fatalf("projectStateReadProject failed: %v", err)
	}
	proj, ok := res.(projectstate.Project)
	if !ok {
		t.Fatalf("expected a projectstate.Project result, got %T", res)
	}
	if proj.ID != "testproj" {
		t.Fatalf("read the wrong project: %q", proj.ID)
	}
}

// TestInSubstrateLedger pins the executes-vs-unavailable split: exactly the 6 Engines
// + projectStateAccess execute in-substrate, and every other RA component is on the
// documented unavailable ledger. A new component must consciously land on one side.
func TestInSubstrateLedger(t *testing.T) {
	inSub := map[string]bool{}
	for _, c := range inSubstrateComponents() {
		inSub[c] = true
	}
	for _, tl := range projectstate.InternalToolCatalog() {
		if inSub[tl.Component] {
			continue
		}
		if _, ok := unavailableDeps[tl.Component]; !ok {
			t.Errorf("component %q is neither in-substrate nor on the unavailable ledger — classify it", tl.Component)
		}
	}
	// The in-substrate set is exactly projectStateAccess + the 6 engines.
	if got := len(inSubstrateComponents()); got != 7 {
		t.Errorf("expected 7 in-substrate components (projectStateAccess + 6 engines), got %d: %v", got, inSubstrateComponents())
	}
}
