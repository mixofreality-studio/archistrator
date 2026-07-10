package systemdesign

// activityfindings_test.go — coverage for the app-side activity-diagram gate the
// sessionState read-back applies to a CoreUseCases draft (founder ruling 2026-07-05).
// Every use case (core AND supporting) must carry a non-empty activity diagram with a
// start node and at least one action step; a diagram-less use case surfaces as one
// ERROR-severity finding on the review panel so the human gate flags it.

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ucd builds a UseCaseDecision carrying a named use case with the given activity diagram.
func ucd(name string, activity *projectstate.ActivityDiagram) projectstate.UseCaseDecision {
	return projectstate.UseCaseDecision{
		UseCase: projectstate.UseCase{Name: name, Activity: activity},
	}
}

// wellFormedActivity is the minimum acceptable diagram: a start node -> action -> end.
func wellFormedActivity() *projectstate.ActivityDiagram {
	return &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{
			{ID: "n1", Kind: projectstate.NodeStart},
			{ID: "n2", Kind: projectstate.NodeAction, Label: "Do the thing"},
			{ID: "n3", Kind: projectstate.NodeEnd},
		},
		Edges: []projectstate.ActivityEdge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
		},
	}
}

// A use case with a null or structurally-empty activity produces exactly one ERROR
// finding; a use case with a start + action diagram produces none.
func Test_useCaseActivityFindings_FlagsMissingAndEmptyDiagrams(t *testing.T) {
	noStart := &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{{ID: "n1", Kind: projectstate.NodeAction, Label: "step"}},
	}
	noAction := &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{{ID: "n1", Kind: projectstate.NodeStart}},
	}

	draft := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{
		ucd("Capture", nil),                             // null activity — the observed gtdapp defect
		ucd("Clarify", &projectstate.ActivityDiagram{}), // empty diagram (no nodes)
		ucd("Organize", noStart),                        // nodes but no start
		ucd("Reflect", noAction),                        // nodes but no action
		ucd("Engage", wellFormedActivity()),             // acceptable — no finding
	}}

	findings := useCaseActivityFindings(KindCoreUseCases, draft)
	if len(findings) != 4 {
		t.Fatalf("expected 4 ERROR findings (one per diagram-less use case), got %d: %+v", len(findings), findings)
	}

	wantNames := []string{"Capture", "Clarify", "Organize", "Reflect"}
	for i, f := range findings {
		if f.Severity != SeverityError {
			t.Errorf("finding %d: want SeverityError, got %v", i, f.Severity)
		}
		if string(f.RuleID) != "USECASE-ACTIVITY-MISSING" {
			t.Errorf("finding %d: want RuleID USECASE-ACTIVITY-MISSING, got %q", i, f.RuleID)
		}
		if !strings.Contains(f.Message, wantNames[i]) {
			t.Errorf("finding %d: message %q must name use case %q", i, f.Message, wantNames[i])
		}
		if f.Location == nil || f.Location.Ordinal != int64(i) {
			t.Errorf("finding %d: want Location.Ordinal %d, got %+v", i, i, f.Location)
		}
	}
	// The acceptable use case (index 4) must not appear.
	for _, f := range findings {
		if strings.Contains(f.Message, "Engage") {
			t.Errorf("well-formed use case Engage must not be flagged; got %q", f.Message)
		}
	}
}

// The gate is scoped to CoreUseCases: any other artifact kind, a nil draft, or a
// wrong-typed draft yields no findings (so the nil-when-empty Findings wire form is
// preserved for every other artifact).
func Test_useCaseActivityFindings_ScopedToCoreUseCasesKind(t *testing.T) {
	full := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{ucd("Capture", nil)}}

	if got := useCaseActivityFindings(KindMission, full); got != nil {
		t.Errorf("non-CoreUseCases kind must yield no findings, got %+v", got)
	}
	if got := useCaseActivityFindings(KindCoreUseCases, nil); got != nil {
		t.Errorf("nil draft must yield no findings, got %+v", got)
	}
	// A draft whose every use case carries a well-formed diagram yields no findings.
	ok := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{ucd("Capture", wellFormedActivity())}}
	if got := useCaseActivityFindings(KindCoreUseCases, ok); got != nil {
		t.Errorf("all-diagrammed draft must yield no findings, got %+v", got)
	}
}
