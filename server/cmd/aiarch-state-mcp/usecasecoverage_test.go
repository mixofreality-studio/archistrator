package main

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// TestCritiquePrompt_RendersCoverageTable pins the PM's coverage read (FPC Task 2.14): the
// getUseCaseCoverage verb renders, per use case, one table row per derived path with its door
// (the Client the actor's first stimulus enters through), the outcome nodes the actor is shown,
// and the derivation's defect codes. It reads the DRAFT core use cases the critique is judging,
// not only the committed slot, and is available to the critique job.
func TestCritiquePrompt_RendersCoverageTable(t *testing.T) {
	s := newSessionFromEditedFixture(t, "../../../.aiarch/state/project.json", func(p *projectstate.Project) {
		// The critique judges a draft: the slot is awaiting review, not committed.
		p.CoreUseCases.Status = projectstate.ReviewAwaitingReview
	})
	out, err := s.getUseCaseCoverage()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("coverage table:\n%s", out)
	for _, col := range []string{"| Use case |", "| Path |", "| Guards |", "| Door |", "| Outcomes the actor sees |", "| Problems |"} {
		if !strings.Contains(out, col) {
			t.Errorf("coverage table has no %q column:\n%s", col, out)
		}
	}
	if !strings.Contains(out, "execute-a-project-activity") {
		t.Errorf("the draft's use cases are not in the table:\n%s", out)
	}
	found := false
	for _, v := range composedVerbs(&Session{Mode: jobModeCritique}) {
		if v.name == "getUseCaseCoverage" && containsStr(v.modes, jobModeCritique) {
			found = true
		}
	}
	if !found {
		t.Fatal("getUseCaseCoverage is not registered for the critique job")
	}
}
