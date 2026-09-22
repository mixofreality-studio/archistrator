package review

import (
	"errors"
	"slices"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

func asEngineError(t *testing.T, err error) *fweng.Error {
	t.Helper()
	var e *fweng.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
	}
	return e
}

func validChange() ReviewChange {
	return ReviewChange{ActivityID: "C-1", ComponentID: "handOffEngine", ContentAddress: "addr-1"}
}

// vibesPolicy is the preset the roster tests run under: it holds the GATE verdict
// still (auto on every non-floor row) so a roster assertion reads the roster alone.
func vibesPolicy() ReviewPolicy { return ReviewPolicy{Preset: "vibes"} }

// The five construction lifecycle phases, in profile order — the ActivityMethodPhase
// wire names the Manager passes. The engine takes them as bare strings (a design
// activity's phases are not members of that closed five), so the vocabulary is
// restated here rather than imported.
var constructionPhases = []string{"requirements", "detailed_design", "test_plan", "construction", "integration"}

// The (activityType, lifecyclePhase) → ArtifactKind table, moved out of the
// constructionManager (reviewArtifactKindFor) and asserted through the engine's own
// reported ArtifactKind. Every row was pinned in manager_test.go before the move; a
// diff here is a behaviour change, not a refactor.
func Test_ProposeReviews_PerKind(t *testing.T) {
	e := NewReviewEngine()
	cases := []struct {
		name      string
		typ       ActivityType
		phase     string
		component string
		wantKind  ReviewArtifactKind
		wantRole  string
		wantAmend bool
	}{
		{"service requirements", ActivityTypeService, "requirements", "c", ReviewKindNoncoding, roleArchitect, false},
		{"service detailed design", ActivityTypeService, "detailed_design", "c", ReviewKindDetailedDesign, roleArchitect, true},
		{"service test plan", ActivityTypeService, "test_plan", "c", ReviewKindNoncoding, roleArchitect, false},
		{"service construction", ActivityTypeService, "construction", "c", ReviewKindConstruction, roleSeniorReviewer, false},
		{"service integration", ActivityTypeService, "integration", "c", ReviewKindIntegration, roleSeniorReviewer, false},
		{"frontend design", ActivityTypeFrontend, "detailed_design", "web-client", ReviewKindUIDesign, roleUIDesigner, true},
		{"frontend construction", ActivityTypeFrontend, "construction", "web-client", ReviewKindUICode, roleSeniorReviewer, false},
		{"uiDesign concept", ActivityTypeUIDesign, "detailed_design", "web-client", ReviewKindUIDesign, roleUIDesigner, true},
		{"deployment spec", ActivityTypeDeployment, "detailed_design", "github", ReviewKindDetailedDesign, roleArchitect, true},
		{"deployment convergence", ActivityTypeDeployment, "integration", "github", ReviewKindIntegration, roleSeniorReviewer, false},
		{"N-STP plan review", ActivityTypeTesting, "integration", "", ReviewKindNoncoding, roleArchitect, false},
		{"documentation construction", ActivityTypeDocumentation, "construction", "", ReviewKindNoncoding, roleArchitect, false},
		{"integration activity", ActivityTypeIntegration, "integration", "", ReviewKindIntegration, roleSeniorReviewer, false},
		{"integration detailed design", ActivityTypeIntegration, "detailed_design", "", ReviewKindNoncoding, roleArchitect, false},
	}
	for _, c := range cases {
		set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "A", ComponentID: c.component},
			c.typ, c.phase, c.component, vibesPolicy(), false, nil)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if set.ArtifactKind != c.wantKind {
			t.Errorf("%s: artifactKind = %s, want %s", c.name, set.ArtifactKind, c.wantKind)
		}
		if len(set.Reviewers) == 0 {
			t.Fatalf("%s: empty reviewer set", c.name)
		}
		if got := set.Reviewers[0].Role; got != c.wantRole {
			t.Errorf("%s: role = %q, want %q", c.name, got, c.wantRole)
		}
		if got := set.Reviewers[0].MayAmend; got != c.wantAmend {
			t.Errorf("%s: mayAmend = %v, want %v", c.name, got, c.wantAmend)
		}
	}
}

// Identical inputs yield identical answers (determinism — the property that makes the
// in-workflow call replay-safe).
func Test_ProposeReviews_Deterministic(t *testing.T) {
	e := NewReviewEngine()
	call := func() (ReviewSet, error) {
		return e.ProposeReviews(fweng.Context{}, validChange(), ActivityTypeService, "construction", "c",
			ReviewPolicy{Preset: "checkpoints"}, false, []string{"x"})
	}
	a, err := call()
	if err != nil {
		t.Fatal(err)
	}
	b, err := call()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Reviewers) != len(b.Reviewers) || a.Reviewers[0] != b.Reviewers[0] ||
		a.RequiresHuman != b.RequiresHuman || a.Reason != b.Reason || a.ArtifactKind != b.ArtifactKind {
		t.Fatalf("non-deterministic: %+v vs %+v", a, b)
	}
}

func Test_ProposeReviews_EmptyActivityID_ContractMisuse(t *testing.T) {
	e := NewReviewEngine()
	_, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ComponentID: "c"},
		ActivityTypeService, "construction", "c", vibesPolicy(), false, nil)
	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// A component-scoped kind reviews ONE component's artifact (its contract, its code, its
// UI design, its UI code). An activity with NO component has none of those to hold the
// work against, so the kind DEGRADES to Noncoding — the architect signs it off — rather
// than the gate losing its roster. The two system-level kinds are unaffected: the system
// test plan and system testing have no component by construction.
//
// This rule moved into the engine WITH the kind table (stage 2). It used to live in the
// constructionManager (reviewArtifactKindFor's trailing guard), which is why the engine's
// own component pre-condition could refuse a call the Manager by then never made. Keeping
// the DEGRADE — not the refusal — is what preserves behaviour: after Task 8 an engine
// refusal OPENS the gate, so refusing here would silently un-gate a componentless
// component-typed activity that is gated today.
func Test_ProposeReviews_AComponentlessActivityDegradesToASignOff(t *testing.T) {
	e := NewReviewEngine()
	noComponent := ReviewChange{ActivityID: "N-STP"}
	for _, c := range []struct {
		typ   ActivityType
		phase string
	}{
		{ActivityTypeService, "detailed_design"},
		{ActivityTypeService, "construction"},
		{ActivityTypeFrontend, "detailed_design"},
		{ActivityTypeFrontend, "construction"},
		{ActivityTypeUIDesign, "detailed_design"},
		{ActivityTypeDeployment, "detailed_design"},
	} {
		set, err := e.ProposeReviews(fweng.Context{}, noComponent, c.typ, c.phase, "", vibesPolicy(), false, nil)
		if err != nil {
			t.Fatalf("%s/%s with no component: %v", c.typ, c.phase, err)
		}
		if set.ArtifactKind != ReviewKindNoncoding {
			t.Errorf("%s/%s with no component: artifactKind = %s, want %s", c.typ, c.phase, set.ArtifactKind, ReviewKindNoncoding)
		}
		if len(set.Reviewers) != 1 || set.Reviewers[0].Role != roleArchitect {
			t.Errorf("%s/%s with no component: reviewers = %+v, want a single architect sign-off", c.typ, c.phase, set.Reviewers)
		}
	}
	for _, c := range []struct {
		typ   ActivityType
		phase string
	}{
		{ActivityTypeIntegration, "integration"},
		{ActivityTypeTesting, "integration"},
		{ActivityTypeTesting, "construction"},
	} {
		set, err := e.ProposeReviews(fweng.Context{}, noComponent, c.typ, c.phase, "", vibesPolicy(), false, nil)
		if err != nil || len(set.Reviewers) == 0 {
			t.Fatalf("%s/%s with no component: want reviewers, got %+v, %v", c.typ, c.phase, set, err)
		}
	}
}

// The activity type is a string on the wire (the internal MCP tool decodes JSON into
// it), so an out-of-vocabulary value is still reachable and still refused.
func Test_ProposeReviews_UnknownActivityType_ContractMisuse(t *testing.T) {
	e := NewReviewEngine()
	_, err := e.ProposeReviews(fweng.Context{}, validChange(), "notAType", "construction", "c", vibesPolicy(), false, nil)
	if got := asEngineError(t, err).Kind; got != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// vibes/checkpoints/full/legacy, construction side — transcribed from the nine
// TestReviewPolicy_* cases that used to live in projectstate's access_test.go. Every
// row here passed there before the policy moved; a diff in this table is a behaviour
// change, not a refactor.
func Test_ProposeReviews_ConstructionGate_MatchesTheRetiredEffectiveGate(t *testing.T) {
	cases := []struct {
		name   string
		policy ReviewPolicy
		floor  bool
		gated  map[string]bool // phase -> requiresHuman
	}{
		{"vibes gates nothing", ReviewPolicy{Preset: "vibes"}, false,
			map[string]bool{}},
		{"vibes still cannot bypass the floor", ReviewPolicy{Preset: "vibes"}, true,
			map[string]bool{"construction": true}},
		{"the floor guards only the construction dispatch", ReviewPolicy{Preset: "full"}, true,
			map[string]bool{"requirements": true, "detailed_design": true, "test_plan": true, "construction": true, "integration": true}},
		{"checkpoints", ReviewPolicy{Preset: "checkpoints"}, false,
			map[string]bool{"detailed_design": true, "construction": true, "integration": true}},
		{"full", ReviewPolicy{Preset: "full"}, false,
			map[string]bool{"requirements": true, "detailed_design": true, "test_plan": true, "construction": true, "integration": true}},
		{"legacy falls back to the explicit map", ReviewPolicy{GatedPhasesByType: map[string][]string{"frontend": {"detailed_design"}}}, false,
			map[string]bool{}},
	}
	e := NewReviewEngine()
	for _, c := range cases {
		for _, p := range constructionPhases {
			set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "C-x", ComponentID: "x"},
				ActivityTypeService, p, "x", c.policy, c.floor, nil)
			if err != nil {
				t.Fatalf("%s/%s: %v", c.name, p, err)
			}
			if set.RequiresHuman != c.gated[p] {
				t.Errorf("%s/%s: requiresHuman=%v, want %v (reason %q)", c.name, p, set.RequiresHuman, c.gated[p], set.Reason)
			}
			if set.Reason == "" {
				t.Errorf("%s/%s: every verdict must carry its one-line reason", c.name, p)
			}
		}
	}
	// The legacy map is consulted per TYPE, so the frontend row it holds does gate.
	set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "U-SPA-w", ComponentID: "w"},
		ActivityTypeFrontend, "detailed_design", "w",
		ReviewPolicy{GatedPhasesByType: map[string][]string{"frontend": {"detailed_design"}}}, false, nil)
	if err != nil || !set.RequiresHuman {
		t.Fatalf("the legacy explicit map must still gate frontend/detailed_design: %+v %v", set, err)
	}
}

// The design rail's rule, verbatim: vibes auto-approves, everything else (including the
// unset/legacy preset) holds for a human. This is what coauthorartifact.go and
// coauthorphase2artifact.go computed inline before the engine owned it.
func Test_ProposeReviews_DesignGate_KeepsTheVibesAutogateRule(t *testing.T) {
	e := NewReviewEngine()
	for _, tc := range []struct {
		preset string
		human  bool
	}{{"vibes", false}, {"checkpoints", true}, {"full", true}, {"", true}} {
		for _, cell := range []struct {
			typ   ActivityType
			phase string
		}{
			{ActivityTypeRequirements, "mission"},
			{ActivityTypeRequirements, "glossary"},
			{ActivityTypeRequirements, "volatilities"},
			{ActivityTypeRequirements, "coreUseCases"},
			{ActivityTypeArchitecture, "architecture"},
			{ActivityTypeProjectDesign, "planningAssumptions"},
		} {
			set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: string(cell.typ)},
				cell.typ, cell.phase, "", ReviewPolicy{Preset: tc.preset}, false, nil)
			if err != nil {
				t.Fatalf("%s/%s preset=%q: %v", cell.typ, cell.phase, tc.preset, err)
			}
			if set.RequiresHuman != tc.human {
				t.Errorf("%s/%s preset=%q: requiresHuman=%v, want %v", cell.typ, cell.phase, tc.preset, set.RequiresHuman, tc.human)
			}
		}
	}
}

// The new non-overridable floor: M0 commits spend, so the projectDesign gate always
// holds for a human — under vibes, and with no contract and no component in sight.
func Test_ProposeReviews_ProjectDesignGateAlwaysRequiresAHuman(t *testing.T) {
	for _, preset := range []string{"vibes", "checkpoints", "full", ""} {
		set, err := NewReviewEngine().ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "projectDesign"},
			ActivityTypeProjectDesign, "sdp", "", ReviewPolicy{Preset: preset}, false, nil)
		if err != nil {
			t.Fatalf("preset %q: %v", preset, err)
		}
		if !set.RequiresHuman {
			t.Fatalf("preset %q: the SDP review approves the plan AND its cost; no preset may bypass it", preset)
		}
		if len(set.Reviewers) != 0 {
			t.Errorf("preset %q: the plan is computed, so no agent reviews it; got %+v", preset, set.Reviewers)
		}
	}
}

// The design reviewer rows, against today's critiqueCriticFor.
func Test_ProposeReviews_DesignReviewerRows(t *testing.T) {
	e := NewReviewEngine()
	cases := []struct {
		typ   ActivityType
		phase string
		roles []string
		amend bool
	}{
		{ActivityTypeRequirements, "mission", []string{"productManager"}, false},
		{ActivityTypeRequirements, "glossary", []string{"productManager"}, false},
		{ActivityTypeRequirements, "volatilities", nil, false},
		{ActivityTypeRequirements, "coreUseCases", []string{"productManager"}, false},
		{ActivityTypeArchitecture, "architecture", []string{"architect"}, true},
		{ActivityTypeProjectDesign, "planningAssumptions", nil, false},
		{ActivityTypeProjectDesign, "sdp", nil, false},
	}
	for _, c := range cases {
		set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "a"}, c.typ, c.phase, "",
			vibesPolicy(), false, nil)
		if err != nil {
			t.Fatalf("%s/%s: %v", c.typ, c.phase, err)
		}
		var got []string
		for _, r := range set.Reviewers {
			got = append(got, r.Role)
		}
		if !slices.Equal(got, c.roles) {
			t.Errorf("%s/%s reviewers = %v, want %v", c.typ, c.phase, got, c.roles)
		}
		if len(set.Reviewers) == 1 && set.Reviewers[0].MayAmend != c.amend {
			t.Errorf("%s/%s mayAmend = %v, want %v", c.typ, c.phase, set.Reviewers[0].MayAmend, c.amend)
		}
	}
	// The PM reviews business alignment against the mission — the design rail's own
	// wire labels, so both surfaces name the role and the lens identically.
	set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "a"},
		ActivityTypeRequirements, "mission", "", vibesPolicy(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := set.Reviewers[0]; r.Perspective != perspectiveBusinessAlignment || r.ReferenceArtifact != "mission" {
		t.Errorf("the PM critique round reviews business alignment against the mission, got %+v", r)
	}
}

// TOTALITY. Every (activityType, lifecyclePhase) the platform can reach — the five
// construction phases, the six design lifecycle phases and the nine Phase-2 artifact
// drafts — answers without an error and carries a reason. This is the proof that
// replaced the Manager-side totality test: no gate can lose its verdict to what the
// Manager passed. A roster may be empty ONLY on the design rows that have no reviewer
// by design (volatilities; every projectDesign phase).
func Test_ProposeReviews_IsTotalOverEveryTypeAndPhase(t *testing.T) {
	e := NewReviewEngine()
	types := []ActivityType{
		ActivityTypeService, ActivityTypeFrontend, ActivityTypeTesting, ActivityTypeDeployment,
		ActivityTypeDocumentation, ActivityTypeUIDesign, ActivityTypeIntegration,
		ActivityTypeRequirements, ActivityTypeArchitecture, ActivityTypeProjectDesign,
	}
	phases := slices.Concat(constructionPhases,
		[]string{"mission", "glossary", "scrubbedRequirements", "volatilities", "coreUseCases", "architecture", "sdp"},
		[]string{"planningAssumptions", "activityList", "network", "normalSolution", "subcriticalSolution",
			"compressedSolution", "decompressedSolution", "riskModel", "sdpReview"})
	reviewerless := map[ActivityType]bool{ActivityTypeProjectDesign: true}
	for _, typ := range types {
		for _, p := range phases {
			for _, component := range []string{"comp-1", ""} {
				set, err := e.ProposeReviews(fweng.Context{}, ReviewChange{ActivityID: "A", ComponentID: component},
					typ, p, component, ReviewPolicy{Preset: "checkpoints"}, false, nil)
				if err != nil {
					t.Fatalf("%s/%s component=%q: %v", typ, p, component, err)
				}
				if set.Reason == "" {
					t.Errorf("%s/%s component=%q: no reason on the verdict", typ, p, component)
				}
				emptyOK := reviewerless[typ] || (typ == ActivityTypeRequirements && p == "volatilities")
				if len(set.Reviewers) == 0 && !emptyOK {
					t.Errorf("%s/%s component=%q: empty reviewer set", typ, p, component)
				}
			}
		}
	}
}
