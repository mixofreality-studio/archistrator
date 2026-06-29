package construction

import (
	"testing"
)

// TestPipelineAdapter_DispatchInputs asserts that dispatchInputsFor maps
// ActivityID → "activity_id" and ComponentID → "component_id".
// pipelineAdapter.inner is the constructionpipeline.ConstructionPipelineAccess interface (not a concrete struct,
// interface), so we test the pure mapping helper directly — no fake adapter needed.
func TestPipelineAdapter_DispatchInputs(t *testing.T) {
	inputs := dispatchInputsFor(pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
	})
	if inputs["activity_id"] != "C-PE" {
		t.Fatalf("expected activity_id=C-PE, got %q", inputs["activity_id"])
	}
	if inputs["component_id"] != "projectExport" {
		t.Fatalf("expected component_id=projectExport, got %q", inputs["component_id"])
	}
}

// TestDispatchInputsFor_WithPhaseAndRole asserts that non-empty Phase and Role
// fields on pipelineSpec are emitted as "phase" and "role" keys in the dispatch
// inputs map (REQ-2 + Plan 1 Task 6).
func TestDispatchInputsFor_WithPhaseAndRole(t *testing.T) {
	spec := pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
		Phase:       "requirements",
		Role:        "senior-developer",
	}
	got := dispatchInputsFor(spec)
	cases := map[string]string{
		"activity_id":  "C-PE",
		"component_id": "projectExport",
		"phase":        "requirements",
		"role":         "senior-developer",
	}
	for k, want := range cases {
		if got[k] != want {
			t.Errorf("dispatchInputsFor[%q] = %q, want %q", k, got[k], want)
		}
	}
}

// TestDispatchInputsFor_EmptyPhaseOmitted asserts that empty Phase and Role values
// are NOT emitted — callers that do not set them get only activity_id/component_id,
// so existing workflow dispatches that rely on workflow-declared defaults are unaffected.
func TestDispatchInputsFor_EmptyPhaseOmitted(t *testing.T) {
	spec := pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
		// Phase and Role intentionally empty
	}
	got := dispatchInputsFor(spec)
	if _, ok := got["phase"]; ok {
		t.Error("phase key should not be present when Phase is empty")
	}
	if _, ok := got["role"]; ok {
		t.Error("role key should not be present when Role is empty")
	}
}
