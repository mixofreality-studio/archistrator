package projectstate

import "testing"

// Question-comments (2026-07-05): type/addressee defaulting + the approve-gate classifier.

func TestAppendReviewComments_CarriesTypeAndAddressee(t *testing.T) {
	in := []ReviewComment{
		{Text: "why this volatility?", Type: ReviewCommentTypeQuestion, Addressee: ReviewAddresseePM},
		{Text: "rename this" /* no Type → change-request */},
	}
	out := appendReviewComments(nil, 1, in)
	if len(out) != 2 {
		t.Fatalf("want 2 entries, got %d", len(out))
	}
	if out[0].Type != ReviewCommentTypeQuestion || out[0].Addressee != ReviewAddresseePM {
		t.Errorf("question entry lost its type/addressee: %+v", out[0])
	}
	if out[1].Type != "" || out[1].Addressee != "" {
		t.Errorf("change-request entry must have empty type/addressee (legacy default): %+v", out[1])
	}
	// Ids are minted deterministically and predictable via ReviewCommentID.
	if out[0].ID != ReviewCommentID(1, 0) || out[1].ID != ReviewCommentID(1, 1) {
		t.Errorf("minted ids not deterministic: %q %q", out[0].ID, out[1].ID)
	}
}

func TestReviewCommentClassifiers(t *testing.T) {
	cases := []struct {
		name       string
		c          ReviewComment
		isQuestion bool
		blocksAppr bool
	}{
		{"legacy open (empty type) blocks", ReviewComment{Status: ReviewCommentOpen}, false, true},
		{"open change-request blocks", ReviewComment{Status: ReviewCommentOpen, Type: ReviewCommentTypeChangeRequest}, false, true},
		{"open question does NOT block", ReviewComment{Status: ReviewCommentOpen, Type: ReviewCommentTypeQuestion}, true, false},
		{"addressed question does NOT block", ReviewComment{Status: ReviewCommentAddressed, Type: ReviewCommentTypeQuestion}, true, false},
		{"addressed change-request does NOT block", ReviewComment{Status: ReviewCommentAddressed}, false, false},
		{"waived change-request does NOT block", ReviewComment{Status: ReviewCommentWaived}, false, false},
	}
	for _, tc := range cases {
		if got := ReviewCommentIsQuestion(tc.c); got != tc.isQuestion {
			t.Errorf("%s: IsQuestion=%v want %v", tc.name, got, tc.isQuestion)
		}
		if got := ReviewCommentBlocksApprove(tc.c); got != tc.blocksAppr {
			t.Errorf("%s: BlocksApprove=%v want %v", tc.name, got, tc.blocksAppr)
		}
	}
}

// An answered question normalizes to addressed (Response non-empty), an unanswered one
// stays open — and either way a question never blocks approve.
func TestNormalize_QuestionStatusAndGate(t *testing.T) {
	thread := []ReviewComment{
		{ID: "q1", Status: ReviewCommentOpen, Type: ReviewCommentTypeQuestion, Response: ""},
		{ID: "q2", Status: ReviewCommentOpen, Type: ReviewCommentTypeQuestion, Response: "because X"},
	}
	out := normalizeReviewThread(thread)
	if out[0].Status != ReviewCommentOpen {
		t.Errorf("unanswered question must stay open, got %q", out[0].Status)
	}
	if out[1].Status != ReviewCommentAddressed {
		t.Errorf("answered question must be addressed, got %q", out[1].Status)
	}
	for _, c := range out {
		if ReviewCommentBlocksApprove(c) {
			t.Errorf("a question must never block approve: %+v", c)
		}
	}
}

// A staleAck audit entry is sticky: normalization never reopens it (no Response, but it must
// stay addressed), and appendStaleAck stamps it addressed + staleAck with a fresh id.
func TestStaleAck_AppendAndNormalizeSticky(t *testing.T) {
	out := appendStaleAck(nil, "architect", "diagrams only")
	if len(out) != 1 {
		t.Fatalf("want 1 entry, got %d", len(out))
	}
	e := out[0]
	if e.Type != ReviewCommentTypeStaleAck || e.Status != ReviewCommentAddressed || e.AuthorRole != "architect" {
		t.Fatalf("staleAck shape wrong: %+v", e)
	}
	// Normalize must NOT flip the staleAck (empty response) to open.
	normalized := normalizeReviewThread(out)
	if normalized[0].Status != ReviewCommentAddressed {
		t.Errorf("staleAck must stay addressed after normalize, got %q", normalized[0].Status)
	}
	if ReviewCommentBlocksApprove(normalized[0]) {
		t.Errorf("staleAck must never block approve")
	}
}
