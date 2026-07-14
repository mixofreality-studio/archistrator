package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestListAndGetResearchSource(t *testing.T) {
	p := minimalProject()
	p.Research = projectstate.ResearchCorpus{Sources: []projectstate.ResearchSourceRef{
		{Title: "Founder brief", Path: ".aiarch/state/research/00-founder-brief.txt", ContentBytes: 5},
	}}
	s, _ := seedProject(t, p, jobModeDraft, projectstate.KindMission)
	// Materialize the corpus file the pointer references.
	if err := os.MkdirAll(filepath.Join(s.StateRoot, statePathPrefix, "research"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.StateRoot, ".aiarch/state/research/00-founder-brief.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := s.listResearchSources()
	if err != nil || !strings.Contains(list, "Founder brief") {
		t.Fatalf("listResearchSources = %q, %v", list, err)
	}
	body, err := s.getResearchSource(".aiarch/state/research/00-founder-brief.txt")
	if err != nil || body != "hello" {
		t.Fatalf("getResearchSource = %q, %v", body, err)
	}
	// Traversal is refused.
	if _, err := s.getResearchSource("../../etc/passwd"); err == nil {
		t.Fatalf("expected traversal refusal")
	}
}

func TestGetCommittedSlot(t *testing.T) {
	p := minimalProject()
	p.Volatilities = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model:  &projectstate.Volatilities{Items: []projectstate.Volatility{{Name: "X", Rationale: "y", Axis: projectstate.AxisSameCustomerOverTime}}},
	}
	s, _ := seedProject(t, p, jobModeDraft, projectstate.KindSystem)

	got, err := s.getCommittedSlot("volatilities")
	if err != nil || !strings.Contains(got, "sameCustomerOverTime") {
		t.Fatalf("getCommittedSlot(volatilities) = %q, %v", got, err)
	}
	// Not-committed kind reports plainly.
	msg, err := s.getCommittedSlot("mission")
	if err != nil || !strings.Contains(msg, "not committed") {
		t.Fatalf("expected not-committed message, got %q, %v", msg, err)
	}
}

func TestGetDraftSlotAndReviewThread(t *testing.T) {
	p := minimalProject()
	p.Volatilities = projectstate.ArtifactSlot{
		Status:       projectstate.ReviewAwaitingReview,
		Model:        &projectstate.Volatilities{Items: []projectstate.Volatility{{Name: "N", Rationale: "r", Axis: projectstate.AxisAllCustomersAtOneTime}}},
		ReviewThread: []projectstate.ReviewComment{{ID: "r1c1", Text: "why?", Status: projectstate.ReviewCommentOpen}},
	}
	s, _ := seedProject(t, p, jobModeDraft, projectstate.KindVolatilities)

	draft, err := s.getDraftSlot()
	if err != nil || !strings.Contains(draft, "allCustomersAtOneTime") {
		t.Fatalf("getDraftSlot = %q, %v", draft, err)
	}
	thread, err := s.getReviewThread()
	if err != nil || !strings.Contains(thread, "r1c1") {
		t.Fatalf("getReviewThread = %q, %v", thread, err)
	}

	// A pristine ambient slot reports no draft.
	s2, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	msg, err := s2.getDraftSlot()
	if err != nil || !strings.Contains(msg, "from scratch") {
		t.Fatalf("expected from-scratch message, got %q, %v", msg, err)
	}
}

func TestGetCritique(t *testing.T) {
	// No critique recorded yet reports plainly.
	s, _ := seedProject(t, minimalProject(), jobModeDraft, projectstate.KindVolatilities)
	msg, err := s.getCritique()
	if err != nil || !strings.Contains(msg, "No critique has been recorded") {
		t.Fatalf("expected no-critique message, got %q, %v", msg, err)
	}

	// A revise verdict surfaces its notes.
	p := minimalProject()
	p.Volatilities = projectstate.ArtifactSlot{
		Status:          projectstate.ReviewAwaitingReview,
		Model:           &projectstate.Volatilities{Items: []projectstate.Volatility{{Name: "N", Rationale: "r", Axis: projectstate.AxisAllCustomersAtOneTime}}},
		CritiqueVerdict: projectstate.CritiqueVerdictRevise,
		CritiqueNotes:   "tighten the rationale for N",
	}
	s2, _ := seedProject(t, p, jobModeDraft, projectstate.KindVolatilities)
	got, err := s2.getCritique()
	if err != nil || !strings.Contains(got, "revise") || !strings.Contains(got, "tighten the rationale for N") {
		t.Fatalf("getCritique(revise) = %q, %v", got, err)
	}

	// An approve verdict clears notes.
	p2 := minimalProject()
	p2.Volatilities = projectstate.ArtifactSlot{
		Status:          projectstate.ReviewAwaitingReview,
		Model:           &projectstate.Volatilities{Items: []projectstate.Volatility{{Name: "N", Rationale: "r", Axis: projectstate.AxisAllCustomersAtOneTime}}},
		CritiqueVerdict: projectstate.CritiqueVerdictApprove,
		CritiqueNotes:   "",
	}
	s3, _ := seedProject(t, p2, jobModeDraft, projectstate.KindVolatilities)
	got2, err := s3.getCritique()
	if err != nil || !strings.Contains(got2, "approve") || strings.Contains(got2, "tighten") {
		t.Fatalf("getCritique(approve) = %q, %v", got2, err)
	}
}
