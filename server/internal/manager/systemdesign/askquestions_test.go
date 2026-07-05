package systemdesign

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// Question-comments (2026-07-05): the pure helpers behind AskQuestions + the approve-gate.

// openReviewCommentIDs (the approve blocker set) must exclude open QUESTIONS and count only
// open change-requests (incl. the legacy empty-type entries).
func TestOpenReviewCommentIDs_ExcludesQuestions(t *testing.T) {
	thread := []projectstate.ReviewComment{
		{ID: "c1", Status: projectstate.ReviewCommentOpen},                                                    // legacy change-request → blocks
		{ID: "c2", Status: projectstate.ReviewCommentOpen, Type: projectstate.ReviewCommentTypeChangeRequest}, // blocks
		{ID: "q1", Status: projectstate.ReviewCommentOpen, Type: projectstate.ReviewCommentTypeQuestion},      // does NOT block
		{ID: "c3", Status: projectstate.ReviewCommentAddressed},                                               // addressed → does not block
	}
	got := openReviewCommentIDs(thread)
	if len(got) != 2 || got[0] != "c1" || got[1] != "c2" {
		t.Fatalf("approve blocker set must be exactly the open change-requests [c1 c2], got %v", got)
	}
}

func TestQuestionsToLedger_StampsAndDrops(t *testing.T) {
	in := []AnchoredComment{
		{JSONPath: "$.a", Text: "real question?", AnchorText: "the anchor"},
		{JSONPath: "$.b", Text: "   ", AnchorText: "blank"}, // empty text → dropped
	}
	out := questionsToLedger(projectstate.ReviewAddresseeArchitect, in)
	if len(out) != 1 {
		t.Fatalf("empty-text question must be dropped; got %d entries", len(out))
	}
	q := out[0]
	if q.Type != projectstate.ReviewCommentTypeQuestion || q.Addressee != projectstate.ReviewAddresseeArchitect {
		t.Errorf("question not stamped type/addressee: %+v", q)
	}
	if q.Anchor != "$.a" || q.Text != "real question?" || q.AuthorRole != reviewAuthorRole {
		t.Errorf("question fields not carried: %+v", q)
	}
}

func TestNextQuestionRound(t *testing.T) {
	if got := nextQuestionRound(nil); got != 1 {
		t.Errorf("empty thread → round 1, got %d", got)
	}
	thread := []projectstate.ReviewComment{{Round: 0}, {Round: 3}, {Round: 1}}
	if got := nextQuestionRound(thread); got != 4 {
		t.Errorf("max round 3 → next 4, got %d", got)
	}
}

func TestAnswerPrompt_RoleAndIDs(t *testing.T) {
	qs := []projectstate.ReviewComment{
		{ID: "r5c1", Text: "clarify the mission?", AnchorText: "Vision"},
		{ID: "r5c2", Text: "why this objective?"},
	}
	// PM addressee → Product Manager role.
	pm := answerPrompt(projectstate.KindMission, projectstate.ReviewAddresseePM, qs)
	if !strings.Contains(pm, "Product Manager") {
		t.Errorf("pm answer prompt must put the agent in the Product Manager role")
	}
	// Architect addressee → System Architect role.
	arch := answerPrompt(projectstate.KindMission, projectstate.ReviewAddresseeArchitect, qs)
	if !strings.Contains(arch, "System Architect") {
		t.Errorf("architect answer prompt must put the agent in the System Architect role")
	}
	for _, want := range []string{"r5c1", "r5c2", "respondToReviewComment", "publishDraft", "getReviewThread"} {
		if !strings.Contains(pm, want) {
			t.Errorf("answer prompt missing %q", want)
		}
	}
	// It must NOT instruct a model rewrite (answer mode has no putDraftModel).
	if strings.Contains(pm, "putDraftModel") {
		t.Errorf("answer prompt must not mention putDraftModel")
	}
}

func TestIsLiveSessionStage(t *testing.T) {
	live := []SessionStage{StageDrafting, StageAwaitingReview, StageRedrafting, StageRefused}
	for _, s := range live {
		if !isLiveSessionStage(s) {
			t.Errorf("stage %v must be live", s)
		}
	}
	for _, s := range []SessionStage{SessionStageUnknown, StageDraftFailed} {
		if isLiveSessionStage(s) {
			t.Errorf("stage %v must NOT be live", s)
		}
	}
}
