package systemdesign

// palettefindings_test.go — coverage for the app-side DV-PALETTE-NOT-IN-EDGES gate
// the sessionState read-back applies to a System draft (agentic-managers spec item
// 5). A dynamic-view step whose tool palette names a tool the static architecture
// forbids surfaces as an ERROR finding on the review panel — the review-panel twin
// of the resolver's dispatch-time lint (projectstate.LintPalettesWithinEdges).

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// A System whose dynamic-view step palette stays within the owner's static edges
// raises no finding.
func Test_paletteWithinEdges_Clean(t *testing.T) {
	sys := &projectstate.System{
		Relationships: []projectstate.Relationship{
			{From: "construction-manager", To: "project-state-access", Mode: projectstate.CallSync, Label: "read"},
		},
		DynamicViews: []projectstate.DynamicView{{
			UseCaseID: "uc-build",
			Edges: []projectstate.Relationship{
				{From: "construction-manager", To: "project-state-access", Mode: projectstate.CallSync, Label: "read", Palette: []string{"projectStateReadProject"}},
			},
		}},
	}
	if f := paletteWithinEdgesFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("in-edges palette should be clean, got: %+v", f)
	}
	// Non-System kinds never produce this finding.
	if f := paletteWithinEdgesFindings(KindVolatilities, sys); f != nil {
		t.Fatalf("non-System kind must produce no palette finding, got: %+v", f)
	}
}

// A palette naming a tool outside the owner's static edges surfaces an ERROR.
func Test_paletteWithinEdges_OutOfEdgesFlagged(t *testing.T) {
	sys := &projectstate.System{
		Relationships: []projectstate.Relationship{
			{From: "construction-manager", To: "project-state-access", Mode: projectstate.CallSync, Label: "read"},
		},
		DynamicViews: []projectstate.DynamicView{{
			UseCaseID: "uc-build",
			Edges: []projectstate.Relationship{
				{From: "construction-manager", To: "project-state-access", Mode: projectstate.CallSync, Label: "read", Palette: []string{"billingStateReadBilling"}},
			},
		}},
	}
	f := paletteWithinEdgesFindings(KindSystem, sys)
	if len(f) == 0 {
		t.Fatal("out-of-edges palette must raise a finding")
	}
	if f[0].RuleID != "DV-PALETTE-NOT-IN-EDGES" || f[0].Severity != SeverityError {
		t.Fatalf("wrong finding shape: %+v", f[0])
	}
	if !strings.Contains(f[0].Message, "billingStateReadBilling") {
		t.Fatalf("finding should name the offending tool: %q", f[0].Message)
	}
}
