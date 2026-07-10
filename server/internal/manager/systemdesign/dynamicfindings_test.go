package systemdesign

// dynamicfindings_test.go — coverage for the app-side dynamic-view gate the
// sessionState read-back applies to a System draft (founder extension 2026-07-05).
// Every committed use case — core AND nonCore variation — must carry its own dynamic
// view (call chain) in the System model; an uncovered use case surfaces as one
// ERROR-severity finding on the review panel so the human gate flags it. This is the
// review-panel twin of methodcheck's USECASE-DYNAMIC-MISSING (the authoritative gate
// putDraftModel enforces while the agent authors).

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// cucWith builds a committed CoreUseCases carrying the named use cases with the given
// ids and classifications.
func cucWith(decisions ...projectstate.UseCaseDecision) *projectstate.CoreUseCases {
	return &projectstate.CoreUseCases{Decisions: decisions}
}

func uc(id, name string, class projectstate.Classification) projectstate.UseCaseDecision {
	return projectstate.UseCaseDecision{
		UseCase: projectstate.UseCase{ID: projectstate.UseCaseID(id), Name: name, Classification: class},
	}
}

func systemWithViews(useCaseIDs ...string) *projectstate.System {
	var dvs []projectstate.DynamicView
	for _, id := range useCaseIDs {
		dvs = append(dvs, projectstate.DynamicView{UseCaseID: id, Key: "uc" + id, Title: "view " + id})
	}
	return &projectstate.System{DynamicViews: dvs}
}

// A System draft that leaves committed use cases without a dynamic view flags exactly
// those use cases (core AND nonCore variation), and none of the covered ones.
func Test_useCaseDynamicFindings_FlagsUncoveredUseCases(t *testing.T) {
	committed := cucWith(
		uc("capture", "Capture", projectstate.ClassCore),              // covered
		uc("clarify", "Clarify", projectstate.ClassCore),              // UNCOVERED (core)
		uc("clarify-bulk", "Clarify Bulk", projectstate.ClassNonCore), // UNCOVERED (nonCore variation)
		uc("engage", "Engage", projectstate.ClassNonCore),             // covered
	)
	draft := systemWithViews("capture", "engage")

	findings := useCaseDynamicFindings(KindSystem, draft, committed)
	if len(findings) != 2 {
		t.Fatalf("expected 2 ERROR findings (one per uncovered use case), got %d: %+v", len(findings), findings)
	}
	wantNames := []string{"Clarify", "Clarify Bulk"}
	for i, f := range findings {
		if f.Severity != SeverityError {
			t.Errorf("finding %d: want SeverityError, got %v", i, f.Severity)
		}
		if string(f.RuleID) != "USECASE-DYNAMIC-MISSING" {
			t.Errorf("finding %d: want RuleID USECASE-DYNAMIC-MISSING, got %q", i, f.RuleID)
		}
		if !strings.Contains(f.Message, wantNames[i]) {
			t.Errorf("finding %d: message %q must name use case %q", i, f.Message, wantNames[i])
		}
	}
	// The nonCore-variation message must name it as such.
	if !strings.Contains(findings[1].Message, "nonCore use-case variation") {
		t.Errorf("nonCore finding must be labelled as a variation, got %q", findings[1].Message)
	}
	for _, f := range findings {
		if strings.Contains(f.Message, "Capture") || strings.Contains(f.Message, "Engage") {
			t.Errorf("covered use case must not be flagged; got %q", f.Message)
		}
	}
}

// The gate is scoped to KindSystem with a committed CoreUseCases: any other kind, a
// nil/absent committed set, a nil draft, or a wrong-typed draft yields no findings.
func Test_useCaseDynamicFindings_ScopedToSystemKind(t *testing.T) {
	committed := cucWith(uc("capture", "Capture", projectstate.ClassCore))
	draft := systemWithViews() // no views at all

	if got := useCaseDynamicFindings(KindCoreUseCases, draft, committed); got != nil {
		t.Errorf("non-System kind must yield no findings, got %+v", got)
	}
	if got := useCaseDynamicFindings(KindSystem, draft, nil); got != nil {
		t.Errorf("nil committed CoreUseCases must yield no findings, got %+v", got)
	}
	if got := useCaseDynamicFindings(KindSystem, nil, committed); got != nil {
		t.Errorf("nil draft must yield no findings, got %+v", got)
	}
	// Every use case covered → no findings.
	full := systemWithViews("capture")
	if got := useCaseDynamicFindings(KindSystem, full, committed); got != nil {
		t.Errorf("all-covered draft must yield no findings, got %+v", got)
	}
}
