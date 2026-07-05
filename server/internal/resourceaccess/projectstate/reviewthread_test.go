package projectstate

import (
	"errors"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// Pure review-ledger logic tests (internal package — they exercise the unexported
// helpers appendReviewComments / normalizeReviewThread / applyReviewCommentStatus that
// the GitStore verbs build on).

func TestAppendReviewComments_MintsDeterministicIDsAndStampsOpen(t *testing.T) {
	in := []ReviewComment{
		{Anchor: "$.vision", AnchorText: "the old vision", Text: "too vague", AuthorRole: "architect"},
		{Anchor: "", Text: "free-form nit", AuthorRole: "architect"},
	}
	got := appendReviewComments(nil, 2, in)
	if len(got) != 2 {
		t.Fatalf("appended %d, want 2", len(got))
	}
	if got[0].ID != "r2c1" || got[1].ID != "r2c2" {
		t.Fatalf("ids = %q,%q want r2c1,r2c2", got[0].ID, got[1].ID)
	}
	for i, c := range got {
		if c.Status != ReviewCommentOpen {
			t.Errorf("comment %d status = %q, want open", i, c.Status)
		}
		if c.Round != 2 {
			t.Errorf("comment %d round = %d, want 2", i, c.Round)
		}
		if c.Response != "" {
			t.Errorf("comment %d response = %q, want empty", i, c.Response)
		}
	}
	if got[0].AnchorText != "the old vision" {
		t.Errorf("anchorText not carried: %q", got[0].AnchorText)
	}
}

func TestAppendReviewComments_IdempotentOnRetry(t *testing.T) {
	in := []ReviewComment{
		{Anchor: "$.a", Text: "one", AuthorRole: "architect"},
		{Anchor: "$.b", Text: "two", AuthorRole: "architect"},
	}
	first := appendReviewComments(nil, 1, in)
	// A Temporal retry re-runs the SAME (round, comments) — the deterministic ids dedup,
	// so no duplicate entries appear (review-ledger §5).
	second := appendReviewComments(first, 1, in)
	if len(second) != 2 {
		t.Fatalf("re-append duplicated entries: len = %d, want 2", len(second))
	}
}

func TestAppendReviewComments_DistinctRoundsAccumulate(t *testing.T) {
	r1 := appendReviewComments(nil, 1, []ReviewComment{{Anchor: "$.a", Text: "one"}})
	r2 := appendReviewComments(r1, 2, []ReviewComment{{Anchor: "$.b", Text: "two"}})
	if len(r2) != 2 {
		t.Fatalf("distinct rounds should accumulate: len = %d, want 2", len(r2))
	}
	if r2[0].ID != "r1c1" || r2[1].ID != "r2c1" {
		t.Fatalf("ids = %q,%q want r1c1,r2c1", r2[0].ID, r2[1].ID)
	}
}

func TestNormalizeReviewThread_ResponsePresenceDecidesStatus(t *testing.T) {
	thread := []ReviewComment{
		{ID: "r1c1", Status: ReviewCommentOpen, Response: "fixed the vision"}, // agent responded
		{ID: "r1c2", Status: ReviewCommentAddressed, Response: ""},            // agent claimed addressed w/o response
		{ID: "r1c3", Status: ReviewCommentWaived, Response: ""},               // waived stays sticky
	}
	got := normalizeReviewThread(thread)
	if got[0].Status != ReviewCommentAddressed {
		t.Errorf("responded comment status = %q, want addressed", got[0].Status)
	}
	if got[1].Status != ReviewCommentOpen {
		t.Errorf("empty-response comment status = %q, want open (server overrides the agent's claim)", got[1].Status)
	}
	if got[2].Status != ReviewCommentWaived {
		t.Errorf("waived comment status = %q, want waived (sticky)", got[2].Status)
	}
}

func TestApplyReviewCommentStatus_LegalTransitions(t *testing.T) {
	thread := []ReviewComment{
		{ID: "r1c1", Status: ReviewCommentOpen},
		{ID: "r1c2", Status: ReviewCommentAddressed, Response: "done"},
	}
	// open -> waived (dismiss).
	got, err := applyReviewCommentStatus(thread, "r1c1", ReviewCommentWaived)
	if err != nil {
		t.Fatalf("open->waived: %v", err)
	}
	if got[0].Status != ReviewCommentWaived {
		t.Errorf("r1c1 status = %q, want waived", got[0].Status)
	}
	// addressed -> open (reopen) clears the response so the next normalize keeps it open.
	got, err = applyReviewCommentStatus(got, "r1c2", ReviewCommentOpen)
	if err != nil {
		t.Fatalf("addressed->open: %v", err)
	}
	if got[1].Status != ReviewCommentOpen {
		t.Errorf("r1c2 status = %q, want open", got[1].Status)
	}
	if got[1].Response != "" {
		t.Errorf("reopen must clear response, got %q", got[1].Response)
	}
}

func TestApplyReviewCommentStatus_IllegalTransitionAndUnknownID(t *testing.T) {
	thread := []ReviewComment{{ID: "r1c1", Status: ReviewCommentOpen}}
	// open -> open is not a legal human transition.
	if _, err := applyReviewCommentStatus(thread, "r1c1", ReviewCommentOpen); kindOfErr(err) != fwra.ContractMisuse {
		t.Errorf("open->open kind = %v, want ContractMisuse", kindOfErr(err))
	}
	// unknown id is NotFound.
	if _, err := applyReviewCommentStatus(thread, "nope", ReviewCommentWaived); kindOfErr(err) != fwra.NotFound {
		t.Errorf("unknown id kind = %v, want NotFound", kindOfErr(err))
	}
}

func kindOfErr(err error) fwra.Kind {
	var e *fwra.Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return fwra.Kind(0)
}
