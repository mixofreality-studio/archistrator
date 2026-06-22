package main

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
)

// TestPipelineAdapter_DispatchInputs asserts that dispatchInputsFor maps
// ActivityID → "activity_id" and ComponentID → "component_id".
// pipelineAdapter.inner is *constructionpipeline.Access (a concrete struct, not an
// interface), so we test the pure mapping helper directly — no fake adapter needed.
func TestPipelineAdapter_DispatchInputs(t *testing.T) {
	inputs := dispatchInputsFor(construction.PipelineSpec{
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
