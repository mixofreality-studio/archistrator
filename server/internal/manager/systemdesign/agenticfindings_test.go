package systemdesign

// agenticfindings_test.go — coverage for the app-side agentic/implementation
// consistency gate the sessionState read-back applies to a System draft
// (agentic-managers doctrine). Review-panel twin of projectstate.LintAgenticWorkflows.

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func agAgentic(v bool) *bool                                            { return &v }
func agImpl(v projectstate.Implementation) *projectstate.Implementation { return &v }

// A well-formed agentic sub-workflow raises no finding.
func Test_agenticWorkflowFindings_Clean(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			{ID: "construction-manager", Name: "ConstructionManager", Kind: projectstate.CompManager, Layer: projectstate.LayerManager, Implementation: agImpl(projectstate.ImplAgentic)},
			{ID: "review-engine", Name: "ReviewEngine", Kind: projectstate.CompEngine, Layer: projectstate.LayerEngine},
		},
		Relationships: []projectstate.Relationship{
			{From: "construction-manager", To: "review-engine", Mode: projectstate.CallSync, Label: "review"},
		},
		DynamicViews: []projectstate.DynamicView{{
			UseCaseID: "uc-build",
			Edges: []projectstate.Relationship{
				{From: "construction-manager", To: "review-engine", Mode: projectstate.CallSync, Label: "orchestrate", Agentic: agAgentic(true), Palette: []string{"reviewProposeReviews"}},
			},
		}},
	}
	if f := agenticWorkflowFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("clean agentic workflow should raise no finding, got: %+v", f)
	}
	// Non-System kinds never produce this finding.
	if f := agenticWorkflowFindings(KindVolatilities, sys); f != nil {
		t.Fatalf("non-System kind must produce no agentic finding, got: %+v", f)
	}
}

// An agentic step whose owner is coded surfaces an ERROR finding.
func Test_agenticWorkflowFindings_OwnerNotAgenticFlagged(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			{ID: "construction-manager", Name: "ConstructionManager", Kind: projectstate.CompManager, Layer: projectstate.LayerManager}, // implementation absent → coded
			{ID: "review-engine", Name: "ReviewEngine", Kind: projectstate.CompEngine, Layer: projectstate.LayerEngine},
		},
		Relationships: []projectstate.Relationship{
			{From: "construction-manager", To: "review-engine", Mode: projectstate.CallSync, Label: "review"},
		},
		DynamicViews: []projectstate.DynamicView{{
			UseCaseID: "uc-build",
			Edges: []projectstate.Relationship{
				{From: "construction-manager", To: "review-engine", Mode: projectstate.CallSync, Label: "orchestrate", Agentic: agAgentic(true), Palette: []string{"reviewProposeReviews"}},
			},
		}},
	}
	f := agenticWorkflowFindings(KindSystem, sys)
	if len(f) == 0 {
		t.Fatal("an agentic step with a coded owner must raise a finding")
	}
	if f[0].RuleID != projectstate.RuleAgenticStepOwnerNotAgentic || f[0].Severity != SeverityError {
		t.Fatalf("wrong finding shape: %+v", f[0])
	}
	if !strings.Contains(f[0].Message, "uc-build") {
		t.Fatalf("finding should name the dynamic view: %q", f[0].Message)
	}
}
