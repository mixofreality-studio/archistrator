package projectstate

// ProfilePhase pairs a canonical ActivityMethodPhase with its per-profile weight
// and human-facing display label. The phase id is ALWAYS one of the five canonical
// ids (Requirements/DetailedDesign/TestPlan/Construction/Integration) so the shared
// earned-value/progress formula (Appendix A) stays uniform across all activity
// types; only the label and weight vary per profile.
type ProfilePhase struct {
	Phase  ActivityMethodPhase
	Weight int
	Label  string
}

// Profile is the per-activity-type preset over the ONE canonical lifecycle: an
// ordered subset of the five canonical phases with weights and display labels.
// It is NOT a distinct lifecycle — it is weights + labels + a phase subset over
// the single shared phase vocabulary (Righting Software, Appendix A / Table A-1).
type Profile struct {
	Phases []ProfilePhase
}

// PhaseIDs returns the ordered canonical phase ids for this profile — the sequence
// the construction pump dispatches.
func (pr Profile) PhaseIDs() []ActivityMethodPhase {
	ids := make([]ActivityMethodPhase, len(pr.Phases))
	for i, p := range pr.Phases {
		ids[i] = p.Phase
	}
	return ids
}

// toPhaseCompletions materializes the profile into the store's PhaseCompletion
// slice (seeded, all Completed=false).
func (pr Profile) toPhaseCompletions() []PhaseCompletion {
	out := make([]PhaseCompletion, len(pr.Phases))
	for i, p := range pr.Phases {
		out[i] = PhaseCompletion{Phase: p.Phase, Weight: p.Weight, Label: p.Label}
	}
	return out
}

// ProfileFor returns the canonical-phase profile for an activity type (and testing
// variant, meaningful only when t == ActivityTypeTesting). All ids are canonical;
// bespoke phase ids are gone. Weights sum to 100 within each profile.
func ProfileFor(t ActivityType, v TestingVariant) Profile {
	switch t {
	case ActivityTypeFrontend:
		// Code-as-design: design-heavy, construction is data-wiring.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 15, "UX Requirements"},
			{MethodPhaseDetailedDesign, 25, "Design"},
			{MethodPhaseTestPlan, 10, "Flows"},
			{MethodPhaseConstruction, 35, "Construction"},
			{MethodPhaseIntegration, 15, "Integration"},
		}}
	case ActivityTypeTesting:
		return profileForTestingVariant(v)
	case ActivityTypeDeployment:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 25, "Provisioning Spec"},
			{MethodPhaseConstruction, 50, "Construction"},
			{MethodPhaseIntegration, 25, "Convergence Verification"},
		}}
	case ActivityTypeDocumentation:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 20, "Outline"},
			{MethodPhaseConstruction, 60, "Authoring"},
			{MethodPhaseIntegration, 20, "Doc Review"},
		}}
	default: // ActivityTypeService — the canonical five.
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 15, "Requirements"},
			{MethodPhaseDetailedDesign, 20, "Detailed Design"},
			{MethodPhaseTestPlan, 10, "Test Plan"},
			{MethodPhaseConstruction, 40, "Construction"},
			{MethodPhaseIntegration, 15, "Integration"},
		}}
	}
}

func profileForTestingVariant(v TestingVariant) Profile {
	switch v {
	case TestVariantHarness:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 15, "Harness Design"},
			{MethodPhaseConstruction, 70, "Harness Construction"},
			{MethodPhaseIntegration, 15, "Harness Review"},
		}}
	case TestVariantPerf:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 25, "Perf Scenario Design"},
			{MethodPhaseConstruction, 50, "Rig Construction"},
			{MethodPhaseIntegration, 25, "Rig Review"},
		}}
	case TestVariantSystemTest:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 10, "Smoke Pass"},
			{MethodPhaseConstruction, 45, "Use-Case Execution"},
			{MethodPhaseIntegration, 45, "Regression & Sign-off"},
		}}
	case TestVariantQAProcess:
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseDetailedDesign, 40, "Gate Definition"},
			{MethodPhaseConstruction, 60, "Process Audit"},
		}}
	default: // TestVariantPlan (N-STP)
		return Profile{Phases: []ProfilePhase{
			{MethodPhaseRequirements, 20, "Use-Case Trace"},
			{MethodPhaseConstruction, 45, "Plan Authoring"},
			{MethodPhaseIntegration, 35, "Plan Review"},
		}}
	}
}
