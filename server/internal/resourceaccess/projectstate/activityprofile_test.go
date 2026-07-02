package projectstate

import "testing"

func canonicalIDsAllowed(p ActivityMethodPhase) bool {
	switch p {
	case MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration:
		return true
	}
	return false
}

func TestProfileFor_AllCanonicalIDsAndSum100(t *testing.T) {
	cases := []struct {
		name    string
		typ     ActivityType
		variant TestingVariant
		wantLen int
	}{
		{"service", ActivityTypeService, 0, 5},
		{"frontend", ActivityTypeFrontend, 0, 5},
		{"deployment", ActivityTypeDeployment, 0, 3},
		{"documentation", ActivityTypeDocumentation, 0, 3},
		{"testing_plan", ActivityTypeTesting, TestVariantPlan, 3},
		{"testing_harness", ActivityTypeTesting, TestVariantHarness, 3},
		{"testing_perf", ActivityTypeTesting, TestVariantPerf, 3},
		{"testing_systemtest", ActivityTypeTesting, TestVariantSystemTest, 3},
		{"testing_qa", ActivityTypeTesting, TestVariantQAProcess, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pr := ProfileFor(c.typ, c.variant)
			if len(pr.Phases) != c.wantLen {
				t.Fatalf("%s: len = %d, want %d", c.name, len(pr.Phases), c.wantLen)
			}
			total := 0
			for _, p := range pr.Phases {
				if !canonicalIDsAllowed(p.Phase) {
					t.Errorf("%s: non-canonical phase id %q", c.name, p.Phase)
				}
				if p.Label == "" {
					t.Errorf("%s: phase %q has empty label", c.name, p.Phase)
				}
				total += p.Weight
			}
			if total != 100 {
				t.Errorf("%s: weight sum = %d, want 100", c.name, total)
			}
		})
	}
}

func TestProfileFor_ServiceIsCanonicalFive(t *testing.T) {
	got := ProfileFor(ActivityTypeService, 0).PhaseIDs()
	want := []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
		MethodPhaseConstruction, MethodPhaseIntegration,
	}
	if len(got) != len(want) {
		t.Fatalf("service PhaseIDs len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("service PhaseIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestProfileFor_TestingPlanRelabelsCanonicalIDs(t *testing.T) {
	pr := ProfileFor(ActivityTypeTesting, TestVariantPlan)
	want := []ProfilePhase{
		{MethodPhaseRequirements, 20, "Use-Case Trace"},
		{MethodPhaseConstruction, 45, "Plan Authoring"},
		{MethodPhaseIntegration, 35, "Plan Review"},
	}
	if len(pr.Phases) != len(want) {
		t.Fatalf("plan profile len = %d, want %d", len(pr.Phases), len(want))
	}
	for i, w := range want {
		if pr.Phases[i] != w {
			t.Errorf("plan phase[%d] = %+v, want %+v", i, pr.Phases[i], w)
		}
	}
}
