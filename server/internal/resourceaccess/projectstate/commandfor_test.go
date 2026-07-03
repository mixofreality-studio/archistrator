package projectstate

import "testing"

func TestProfileSlug(t *testing.T) {
	cases := []struct {
		t    ActivityType
		v    TestingVariant
		want string
	}{
		{ActivityTypeService, 0, "service"},
		{ActivityTypeFrontend, 0, "frontend"},
		{ActivityTypeDeployment, 0, "deployment"},
		{ActivityTypeDocumentation, 0, "documentation"},
		{ActivityTypeTesting, TestVariantPlan, "testing-plan"},
		{ActivityTypeTesting, TestVariantHarness, "testing-harness"},
		{ActivityTypeTesting, TestVariantPerf, "testing-perf"},
		{ActivityTypeTesting, TestVariantSystemTest, "testing-systemtest"},
		{ActivityTypeTesting, TestVariantQAProcess, "testing-qa"},
	}
	for _, c := range cases {
		if got := ProfileSlug(c.t, c.v); got != c.want {
			t.Errorf("ProfileSlug(%v,%v) = %q, want %q", c.t, c.v, got, c.want)
		}
	}
}

func TestCommandFor(t *testing.T) {
	if got := CommandFor(ActivityTypeService, 0, MethodPhaseDetailedDesign); got != "service-detailed-design" {
		t.Errorf("got %q, want service-detailed-design", got)
	}
	if got := CommandFor(ActivityTypeTesting, TestVariantHarness, MethodPhaseConstruction); got != "testing-harness-construction" {
		t.Errorf("got %q, want testing-harness-construction", got)
	}
}

// TestCommandForTotalOverProfiles asserts CommandFor returns a non-empty,
// well-formed slug for every phase that ProfileFor actually emits — the command
// matrix is exactly the flattening of ProfileFor.
func TestCommandForTotalOverProfiles(t *testing.T) {
	for _, combo := range allProfileCombos() {
		for _, p := range ProfileFor(combo.t, combo.v).PhaseIDs() {
			got := CommandFor(combo.t, combo.v, p)
			if got == "" {
				t.Errorf("CommandFor(%v,%v,%q) empty", combo.t, combo.v, p)
			}
			if want := ProfileSlug(combo.t, combo.v) + "-" + kebabPhase(p); got != want {
				t.Errorf("CommandFor = %q, want %q", got, want)
			}
		}
	}
}

type profileCombo struct {
	t ActivityType
	v TestingVariant
}

// allProfileCombos enumerates every distinct (type, variant) profile in the domain.
func allProfileCombos() []profileCombo {
	return []profileCombo{
		{ActivityTypeService, 0},
		{ActivityTypeFrontend, 0},
		{ActivityTypeDeployment, 0},
		{ActivityTypeDocumentation, 0},
		{ActivityTypeTesting, TestVariantPlan},
		{ActivityTypeTesting, TestVariantHarness},
		{ActivityTypeTesting, TestVariantPerf},
		{ActivityTypeTesting, TestVariantSystemTest},
		{ActivityTypeTesting, TestVariantQAProcess},
	}
}
