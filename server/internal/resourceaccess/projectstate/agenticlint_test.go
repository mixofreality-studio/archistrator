package projectstate

import (
	"strings"
	"testing"
)

// impl is a test helper that returns a pointer to an Implementation (the schema field
// is optional → *Implementation).
func impl(v Implementation) *Implementation { return &v }

// agenticBool returns a pointer to a bool for the optional Relationship.Agentic flag.
func agenticBool(v bool) *bool { return &v }

// agenticTestSystem builds a System with two components — an agentic manager and a
// plain (coded) engine — and one dynamic view whose single step is configurable.
func agenticTestSystem(ownerImpl Implementation, step Relationship) System {
	return System{
		Components: []Component{
			{ID: "construction-manager", Name: "ConstructionManager", Kind: CompManager, Layer: LayerManager, Implementation: impl(ownerImpl)},
			{ID: "review-engine", Name: "ReviewEngine", Kind: CompEngine, Layer: LayerEngine},
		},
		Relationships: []Relationship{
			{From: "construction-manager", To: "review-engine", Mode: CallSync, Label: "review"},
		},
		DynamicViews: []DynamicView{
			{UseCaseID: "uc-build", Key: "uc-build-view", Edges: []Relationship{step}},
		},
	}
}

func hasRule(v []AgenticViolation, rule string) *AgenticViolation {
	for i := range v {
		if v[i].RuleID == rule {
			return &v[i]
		}
	}
	return nil
}

// TestLintAgenticWorkflows_CleanAgenticStep proves a well-formed agentic sub-workflow
// step (agentic flag, non-empty palette, agentic owner) produces NO violations.
func TestLintAgenticWorkflows_CleanAgenticStep(t *testing.T) {
	step := Relationship{From: "construction-manager", To: "review-engine", Mode: CallSync, Label: "orchestrate", Agentic: agenticBool(true), Palette: []string{"reviewProposeReviews"}}
	if v := LintAgenticWorkflows(agenticTestSystem(ImplAgentic, step)); len(v) != 0 {
		t.Fatalf("a clean agentic step must not lint: %+v", v)
	}
	// A hybrid owner is equally valid.
	if v := LintAgenticWorkflows(agenticTestSystem(ImplHybrid, step)); len(v) != 0 {
		t.Fatalf("a hybrid owner must be accepted: %+v", v)
	}
}

// TestLintAgenticWorkflows_OwnerNotAgentic proves an agentic step whose owner is coded
// fires DV-AGENTIC-STEP-OWNER-NOT-AGENTIC as an ERROR.
func TestLintAgenticWorkflows_OwnerNotAgentic(t *testing.T) {
	step := Relationship{From: "construction-manager", To: "review-engine", Mode: CallSync, Label: "orchestrate", Agentic: agenticBool(true), Palette: []string{"reviewProposeReviews"}}
	v := LintAgenticWorkflows(agenticTestSystem(ImplCoded, step))
	got := hasRule(v, RuleAgenticStepOwnerNotAgentic)
	if got == nil {
		t.Fatalf("expected %s, got %+v", RuleAgenticStepOwnerNotAgentic, v)
	}
	if got.Warning {
		t.Fatal("owner-not-agentic must be an ERROR, not a warning")
	}
	if !strings.Contains(got.Reason, "coded") {
		t.Fatalf("reason should name the owner's implementation: %q", got.Reason)
	}
}

// TestLintAgenticWorkflows_PaletteRequiresAgentic proves a step with a palette but no
// agentic flag fires DV-PALETTE-REQUIRES-AGENTIC as an ERROR.
func TestLintAgenticWorkflows_PaletteRequiresAgentic(t *testing.T) {
	step := Relationship{From: "construction-manager", To: "review-engine", Mode: CallSync, Label: "call", Palette: []string{"reviewProposeReviews"}}
	v := LintAgenticWorkflows(agenticTestSystem(ImplAgentic, step))
	got := hasRule(v, RulePaletteRequiresAgentic)
	if got == nil || got.Warning {
		t.Fatalf("expected an ERROR %s, got %+v", RulePaletteRequiresAgentic, v)
	}
}

// TestLintAgenticWorkflows_ComponentNoPalette proves an agentic component that owns a
// step but documents no palette anywhere fires COMPONENT-AGENTIC-NO-PALETTE as a warning.
func TestLintAgenticWorkflows_ComponentNoPalette(t *testing.T) {
	// The owner is agentic and owns the step, but the step is not agentic and carries no
	// palette → the component's palette is undocumented.
	step := Relationship{From: "construction-manager", To: "review-engine", Mode: CallSync, Label: "call"}
	v := LintAgenticWorkflows(agenticTestSystem(ImplAgentic, step))
	got := hasRule(v, RuleAgenticComponentNoPalette)
	if got == nil {
		t.Fatalf("expected %s, got %+v", RuleAgenticComponentNoPalette, v)
	}
	if !got.Warning {
		t.Fatal("component-no-palette must be a WARNING")
	}
	if got.Component != "construction-manager" {
		t.Fatalf("warning should name the component, got %q", got.Component)
	}
}

// TestImplementationOf_DefaultsCoded proves the absent (nil) implementation field maps
// to the safe default ImplCoded (the "default openly" decision).
func TestImplementationOf_DefaultsCoded(t *testing.T) {
	if got := implementationOf(Component{ID: "x"}); got != ImplCoded {
		t.Fatalf("absent implementation must default to ImplCoded, got %v", got)
	}
	if got := implementationOf(Component{ID: "x", Implementation: impl(ImplAgentic)}); got != ImplAgentic {
		t.Fatalf("declared implementation must be honored, got %v", got)
	}
}

// TestImplementationWireRoundTrip proves the Implementation enum encodes/decodes via its
// camelCase wire names and rejects an unrecognized value.
func TestImplementationWireRoundTrip(t *testing.T) {
	for name, want := range map[string]Implementation{"coded": ImplCoded, "hybrid": ImplHybrid, "agentic": ImplAgentic} {
		var got Implementation
		if err := got.UnmarshalJSON([]byte(`"` + name + `"`)); err != nil || got != want {
			t.Fatalf("decode %q = (%v,%v), want %v", name, got, err, want)
		}
		b, err := want.MarshalJSON()
		if err != nil || string(b) != `"`+name+`"` {
			t.Fatalf("encode %v = (%s,%v), want %q", want, b, err, name)
		}
	}
	var bad Implementation
	if err := bad.UnmarshalJSON([]byte(`"quantum"`)); err == nil {
		t.Fatal("an unrecognized implementation wire name must be rejected")
	}
}
