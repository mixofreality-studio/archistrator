package construction

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestHydrateConstructionActivity_ServicePhases(t *testing.T) {
	got := hydrateConstructionActivity("C-Orders", projectstate.ActivityItem{Coding: true, EffortDays: 5}, "comp-1")
	want := []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseDetailedDesign,
		projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	}
	if len(got.Phases) != len(want) {
		t.Fatalf("phases len = %d, want %d", len(got.Phases), len(want))
	}
	for i := range want {
		if got.Phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, got.Phases[i], want[i])
		}
	}
}

func TestHydrateConstructionActivity_TestingPlanIsThreePhases(t *testing.T) {
	got := hydrateConstructionActivity("N-STP", projectstate.ActivityItem{Coding: true}, "")
	want := []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	}
	if len(got.Phases) != len(want) {
		t.Fatalf("N-STP phases len = %d, want %d", len(got.Phases), len(want))
	}
	for i := range want {
		if got.Phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, got.Phases[i], want[i])
		}
	}
}

func TestDispatchInputsForIncludesCommand(t *testing.T) {
	// A service construction phase -> service-construction command.
	in := dispatchInputsFor(pipelineSpec{
		ActivityID:  "C-BM",
		ComponentID: "billingManager",
		Phase:       "construction",
	})
	if in["command"] != "service-construction" {
		t.Errorf("command = %q, want service-construction", in["command"])
	}
	if in["activity_id"] != "C-BM" || in["component_id"] != "billingManager" {
		t.Errorf("activity/component passthrough wrong: %+v", in)
	}
	if in["phase"] != "construction" {
		t.Errorf("phase = %q, want construction", in["phase"])
	}

	// A testing harness detailed-design phase -> testing-harness-detailed-design.
	in2 := dispatchInputsFor(pipelineSpec{
		ActivityID: "N-STH",
		Phase:      "detailed_design",
	})
	if in2["command"] != "testing-harness-detailed-design" {
		t.Errorf("command = %q, want testing-harness-detailed-design", in2["command"])
	}
}
