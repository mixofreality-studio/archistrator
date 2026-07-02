package systemdesign

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestTestingStateToContract(t *testing.T) {
	if got := testingStateToContract(nil); got != nil {
		t.Fatalf("nil input: got %v, want nil", got)
	}
	in := &projectstate.TestingState{
		TestRuns: []projectstate.TestRun{{ID: "run-1", Passed: 12, Failed: 1, Note: "nightly"}},
		Defects:  []projectstate.DefectRecord{{ID: "D-1", Title: "flake", Severity: "high", Note: "retry"}},
	}
	got := testingStateToContract(in)
	if got == nil {
		t.Fatal("populated input: got nil")
	}
	if len(got.TestRuns) != 1 || got.TestRuns[0].Id != "run-1" || got.TestRuns[0].Passed != 12 {
		t.Errorf("TestRuns mapped wrong: %+v", got.TestRuns)
	}
	if len(got.Defects) != 1 || got.Defects[0].Id != "D-1" || got.Defects[0].Severity != "high" {
		t.Errorf("Defects mapped wrong: %+v", got.Defects)
	}
}
