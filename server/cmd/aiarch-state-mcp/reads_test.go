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

// COMMENT-MARGIN task 5b — THE ANSWER JOB REACHES A REOPENED QUESTION THREAD.
// Routing a reviewer reply into a question thread is only half the path: the answer job
// must then answer it. The job's /design-answer command selects "the OPEN questions
// addressed to you" from getReviewThread and answers each with respondToReviewComment.
// This drives that exact pair against a thread whose LAST utterance is the reviewer's
// (the shape ApplyReviewBatch leaves behind after a routed reply — status derived back to
// open, type/addressee intact): the answer session must SEE it, and must be able to answer
// it by id with no status gate in the way.
func TestAnswerJobSeesAndAnswersAReopenedQuestionThread(t *testing.T) {
	// THE FIXTURE IS DERIVED, NOT ASSERTED. Hardcoding Status: open here would make this test
	// blind to exactly the mutations it exists to rule out — a filter or gate keyed on the
	// status. So the reopened state is produced the way production produces it: an ANSWERED
	// question thread, plus one reviewer utterance, run through the real
	// projectstate.ApplyReviewBatch (the very call AskQuestions makes via the seed verb),
	// whose closing normalizeReviewThread derives the status.
	answered := []projectstate.ReviewComment{{
		ID: "r1c0", Round: 1, Text: "Why only three objectives?",
		AuthorRole: "architect",
		Type:       projectstate.ReviewCommentTypeQuestion,
		Addressee:  projectstate.ReviewAddresseeArchitect,
		Status:     projectstate.ReviewCommentAnswered,
		Replies: []projectstate.ReviewCommentReply{
			{ID: "r1c0-u1", AuthorRole: "architect", Text: "Three is the abstraction ceiling.", At: "2026-09-19T00:00:00Z"},
		},
	}}
	reopened, err := projectstate.ApplyReviewBatch(answered, 2, nil, []projectstate.ReviewReply{{
		CommentID: "r1c0", AuthorRole: "architect-user",
		Text: "That does not answer the cost objective", At: "2026-09-19T02:00:00Z",
	}})
	if err != nil {
		t.Fatalf("ApplyReviewBatch: %v", err)
	}
	// Precondition, stated so a change in the derive rule fails HERE with a clear message
	// rather than silently defusing every assertion below.
	if reopened[0].Status != projectstate.ReviewCommentOpen {
		t.Fatalf("fixture precondition: a reviewer utterance must DERIVE the thread back to open, got %q", reopened[0].Status)
	}

	p := minimalProject()
	p.Mission = projectstate.ArtifactSlot{
		Status:       projectstate.ReviewAwaitingReview,
		Model:        &projectstate.MissionStatement{},
		ReviewThread: reopened,
	}
	s, _ := seedProject(t, p, jobModeAnswer, projectstate.KindMission)

	// STEP 1 of /design-answer: collect the OPEN questions addressed to you.
	thread, err := s.getReviewThread()
	if err != nil {
		t.Fatalf("getReviewThread: %v", err)
	}
	for _, want := range []string{"r1c0", projectstate.ReviewCommentOpen, projectstate.ReviewCommentTypeQuestion, projectstate.ReviewAddresseeArchitect} {
		if !strings.Contains(thread, want) {
			t.Fatalf("the answer job must see the reopened question as %q; thread was:\n%s", want, thread)
		}
	}
	// The reviewer's follow-up is the context the answer must address.
	if !strings.Contains(thread, "does not answer the cost objective") {
		t.Fatalf("the reviewer's follow-up utterance must be visible to the answer job:\n%s", thread)
	}

	// STEP 3 of /design-answer: answer it in place, by id.
	if err := s.respondToReviewComment("r1c0", "Cost is covered by objective 2."); err != nil {
		t.Fatalf("respondToReviewComment on a reopened question: %v", err)
	}
	slot := readBackSlot(t, s, projectstate.KindMission)
	if len(slot.ReviewThread) != 1 {
		t.Fatalf("answering must not open a thread, got %d entries", len(slot.ReviewThread))
	}
	entry := slot.ReviewThread[0]
	if len(entry.Replies) != 3 || entry.Replies[2].AuthorRole != "architect" {
		t.Fatalf("the agent answer must append a third, AGENT-authored utterance: %+v", entry.Replies)
	}
	if entry.Status != projectstate.ReviewCommentAnswered {
		t.Fatalf("an answered question settles back to answered, got %q", entry.Status)
	}
}
