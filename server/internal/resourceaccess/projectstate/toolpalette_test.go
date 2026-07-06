package projectstate

import (
	"strings"
	"testing"
)

// paletteTestSystem builds a minimal System whose static edges let
// "construction-manager" reach "project-state-access" and "review-engine" (via
// canonical matching to the projectStateAccess / reviewEngine contract keys) but
// NOT "billing-state-access". It carries one dynamic view for use case "uc-build".
func paletteTestSystem(edgePalette []string) System {
	return System{
		Relationships: []Relationship{
			{From: "construction-manager", To: "project-state-access", Mode: CallSync, Label: "read"},
			{From: "construction-manager", To: "review-engine", Mode: CallSync, Label: "review"},
			// deliberately NO edge to billing-state-access.
		},
		DynamicViews: []DynamicView{
			{
				UseCaseID: "uc-build",
				Key:       "uc-build-view",
				Edges: []Relationship{
					{From: "construction-manager", To: "project-state-access", Mode: CallSync, Label: "read", Palette: edgePalette},
				},
			},
		},
	}
}

// TestResolveToolPalette_Found proves a documented palette resolves to EXACTLY its
// tool set (no fallback, no lint error) when every tool is ⊆ the step owner's
// static edges.
func TestResolveToolPalette_Found(t *testing.T) {
	sys := paletteTestSystem([]string{"projectStateReadProject", "reviewProposeReviews"})
	res := ResolveToolPalette(sys, "uc-build", "draft", []string{"fallbackTool"})

	if res.UsedFallback {
		t.Fatal("a documented palette must NOT fall back")
	}
	if len(res.Errors) != 0 {
		t.Fatalf("valid palette produced lint errors: %v", res.Errors)
	}
	want := []string{"projectStateReadProject", "reviewProposeReviews"}
	if strings.Join(res.Tools, ",") != strings.Join(want, ",") {
		t.Fatalf("resolved tools = %v, want %v", res.Tools, want)
	}
}

// TestResolveToolPalette_MissingFallsBackWithWarn proves that with no documented
// palette the resolver returns the caller's fallback set, flags UsedFallback, and
// emits a WARN naming the missing palette.
func TestResolveToolPalette_MissingFallsBackWithWarn(t *testing.T) {
	sys := paletteTestSystem(nil) // edge carries no palette
	fallback := []string{"putDraftModel", "publishDraft"}
	res := ResolveToolPalette(sys, "uc-build", "draft", fallback)

	if !res.UsedFallback {
		t.Fatal("a use case with no palette must use the fallback")
	}
	if strings.Join(res.Tools, ",") != strings.Join(fallback, ",") {
		t.Fatalf("fallback tools = %v, want %v", res.Tools, fallback)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "no tool palette documented") {
		t.Fatalf("expected a missing-palette WARN, got %v", res.Warnings)
	}

	// An unknown use case is likewise a fallback (no dynamic view at all).
	if r2 := ResolveToolPalette(sys, "does-not-exist", "critique", fallback); !r2.UsedFallback {
		t.Fatal("an unknown use case must fall back")
	}
}

// TestResolveToolPalette_NotInEdgesIsError proves the palette-⊄-edges lint fires an
// ERROR when a palette names a tool whose target component is not a static
// dependency of the step owner, and when it names an agent-hidden op.
func TestResolveToolPalette_NotInEdgesIsError(t *testing.T) {
	// billingStateReadBilling targets billing-state-access, which construction-manager
	// has NO static edge to.
	sys := paletteTestSystem([]string{"projectStateReadProject", "billingStateReadBilling"})
	res := ResolveToolPalette(sys, "uc-build", "draft", nil)
	if len(res.Errors) == 0 {
		t.Fatal("a palette naming a tool outside the owner's static edges must ERROR")
	}
	if !strings.Contains(strings.Join(res.Errors, "|"), "billingStateReadBilling") {
		t.Fatalf("error should name the offending tool: %v", res.Errors)
	}

	// LintPalettesWithinEdges (the shared authoritative check) sees the same violation.
	viol := LintPalettesWithinEdges(sys)
	if len(viol) == 0 || viol[0].Tool != "billingStateReadBilling" {
		t.Fatalf("LintPalettesWithinEdges missed the violation: %+v", viol)
	}

	// An agent-hidden op in a palette is also a violation, even if the owner CAN reach
	// projectStateAccess statically.
	hidden := paletteTestSystem([]string{"projectStateCommitArtifact"})
	hv := LintPalettesWithinEdges(hidden)
	if len(hv) == 0 || !strings.Contains(hv[0].Reason, "agent-hidden") {
		t.Fatalf("expected an agent-hidden violation, got %+v", hv)
	}
}
