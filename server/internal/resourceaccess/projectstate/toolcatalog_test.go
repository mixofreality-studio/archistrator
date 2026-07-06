package projectstate

import (
	"encoding/json"
	"testing"
)

// TestInternalCatalog_RepresentativeRAAndEngine proves the generated internal
// tool surface (toolcatalog.gen.go, from .serviceContracts) carries a correct
// descriptor for a representative ResourceAccess AND Engine operation: right
// name, layer, readOnlyHint, and a self-contained (parseable) input/output schema.
func TestInternalCatalog_RepresentativeRAAndEngine(t *testing.T) {
	// ResourceAccess read: projectStateAccess.ReadProject → read-only, exposable,
	// input schema names the projectId param.
	rp, ok := InternalToolByName("projectStateReadProject")
	if !ok {
		t.Fatal("expected a generated tool for projectStateAccess.ReadProject")
	}
	if rp.Layer != "ResourceAccess" || rp.Operation != "ReadProject" || rp.Component != "projectStateAccess" {
		t.Fatalf("wrong descriptor metadata: %+v", rp)
	}
	if !rp.ReadOnly {
		t.Fatal("ReadProject must be marked read-only (a Read* op)")
	}
	if rp.AgentHidden {
		t.Fatal("a projectStateAccess READ must be agent-exposable")
	}
	assertObjectSchemaHasProp(t, rp.InputSchema, "projectID")
	assertParseable(t, rp.OutputSchema)

	// Engine op: reviewEngine.ProposeReviews → read-only (Engines are pure), exposable.
	pr, ok := InternalToolByName("reviewProposeReviews")
	if !ok {
		t.Fatal("expected a generated tool for reviewEngine.ProposeReviews")
	}
	if pr.Layer != "Engine" || !pr.ReadOnly {
		t.Fatalf("every Engine op must be read-only (pure): %+v", pr)
	}
	if pr.AgentHidden {
		t.Fatal("an Engine op must be agent-exposable")
	}
	assertParseable(t, pr.InputSchema)
	assertParseable(t, pr.OutputSchema)

	// ResourceAccess with a payload result → schema carries the reachable $defs.
	rt, ok := InternalToolByName("artifactRetrieveOutputTree")
	if !ok {
		t.Fatal("expected a generated tool for artifactAccess.RetrieveOutputTree")
	}
	if !assertParseableHasDefs(t, rt.OutputSchema) {
		t.Fatal("a payload result schema must inline its reachable $defs to be self-contained")
	}
}

// TestInternalCatalog_AgentHiddenRawOpsAbsentFromExposable proves the merge-
// authority raw ops (e.g. CommitArtifact) are GENERATED (present in the full
// catalog, flagged AgentHidden) but ABSENT from the agent-exposable set — the
// composed verbs / server rail replace them.
func TestInternalCatalog_AgentHiddenRawOpsAbsentFromExposable(t *testing.T) {
	commit, ok := InternalToolByName("projectStateCommitArtifact")
	if !ok {
		t.Fatal("CommitArtifact must still be GENERATED into the full catalog")
	}
	if !commit.AgentHidden {
		t.Fatal("raw CommitArtifact must be AgentHidden — merge authority stays with the server rail")
	}
	for _, tl := range AgentExposableTools() {
		if tl.Component == "projectStateAccess" && !tl.ReadOnly {
			t.Fatalf("a projectStateAccess write leaked into the agent-exposable set: %s", tl.Name)
		}
		if tl.AgentHidden {
			t.Fatalf("AgentExposableTools returned an AgentHidden tool: %s", tl.Name)
		}
	}

	// Every RA/Engine contract operation is tool-eligible: the catalog is non-empty
	// and every descriptor carries parseable schemas.
	all := InternalToolCatalog()
	if len(all) < 40 {
		t.Fatalf("expected the full RA/Engine surface, got only %d tools", len(all))
	}
	for _, tl := range all {
		assertParseable(t, tl.InputSchema)
		assertParseable(t, tl.OutputSchema)
	}
}

func assertParseable(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("schema does not parse: %v\n%s", err, raw)
	}
}

func assertObjectSchemaHasProp(t *testing.T, raw json.RawMessage, prop string) {
	t.Helper()
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("input schema does not parse: %v", err)
	}
	if _, ok := s.Properties[prop]; !ok {
		t.Fatalf("input schema missing property %q; have %v", prop, keysOf(s.Properties))
	}
}

func assertParseableHasDefs(t *testing.T, raw json.RawMessage) bool {
	t.Helper()
	assertParseable(t, raw)
	var s struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	_ = json.Unmarshal(raw, &s)
	return len(s.Defs) > 0
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
