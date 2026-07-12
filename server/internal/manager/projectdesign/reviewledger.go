package projectdesign

import (
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// reviewledger.go holds the durable review-ledger seam for the projectDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05) — the structural twin of the
// systemDesign Manager's reviewledger.go. Ledger STORAGE + transition rules live in
// projectstate (reviewthread.go); this is the Manager-side wiring: the ReviewComment ↔
// ReviewCommentView projection and the open-comment gate. The SetReviewCommentStatus /
// SeedReviewComments branch-mutation Activities MIGRATED (B9) onto the generated
// designSessionAccess.setReviewCommentStatusOnBranch / seedReviewCommentsOnBranch
// invokers (invokers.gen.go, reached via wf.Acts) — the ledger-extension fallback those
// custom bodies ran now lives inside the RA (projectstate/designsession.go).

// reviewAuthorRole is the role stamped on every comment the architect files at the
// Project-Design review gate.
const reviewAuthorRole = "architect"

// setCommentStatusSignal is the SetReviewCommentStatus signal payload — the waive/reopen
// transition delivered to the CoAuthor workflow suspended at the AwaitingReview gate.
type setCommentStatusSignal struct {
	CommentID string
	Status    string
}

// feedbackToLedgerComments converts the architect's inbound anchored comments (on a Reject's
// ReviewFeedback) into the projectstate.ReviewComment shape the append verb stamps into the
// durable thread. Only Anchor / AnchorText / Text / AuthorRole are filled — id / round / open
// status are server-minted in appendReviewComments. An anchored comment with empty Text is
// dropped (defensive); free-text Notes stay the reject notes, not ledger comments.
func feedbackToLedgerComments(feedback *ReviewFeedback) []projectstate.ReviewComment {
	if feedback == nil {
		return nil
	}
	out := make([]projectstate.ReviewComment, 0, len(feedback.Comments))
	for _, c := range feedback.Comments {
		if c.Text == "" {
			continue
		}
		out = append(out, projectstate.ReviewComment{
			Anchor:     c.JSONPath,
			AnchorText: c.AnchorText,
			Text:       c.Text,
			AuthorRole: reviewAuthorRole,
		})
	}
	return out
}

// toReviewCommentView projects one stored ledger entry onto its wire view.
func toReviewCommentView(c projectstate.ReviewComment) ReviewCommentView {
	return ReviewCommentView{
		ID:         c.ID,
		Anchor:     c.Anchor,
		AnchorText: c.AnchorText,
		Text:       c.Text,
		AuthorRole: c.AuthorRole,
		Round:      c.Round,
		Status:     c.Status,
		Response:   c.Response,
		Type:       c.Type,
		Addressee:  c.Addressee,
	}
}

// reviewThreadToView projects the durable ledger onto the wire thread the sessionState
// Query returns (nil stays nil so the omitempty wire shape is unchanged).
func reviewThreadToView(thread []projectstate.ReviewComment) []ReviewCommentView {
	if len(thread) == 0 {
		return nil
	}
	out := make([]ReviewCommentView, 0, len(thread))
	for _, c := range thread {
		out = append(out, toReviewCommentView(c))
	}
	return out
}

// openReviewCommentIDs returns the ids of every OPEN CHANGE-REQUEST — the comments that gate
// approve (review-ledger §4). Open QUESTIONS never gate (question-comments §approve). Empty
// ⇒ approve is unblocked.
func openReviewCommentIDs(thread []projectstate.ReviewComment) []string {
	var ids []string
	for _, c := range thread {
		if projectstate.ReviewCommentBlocksApprove(c) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// seedAmendmentLedger records the reopening feedback as round-0 OPEN ledger entries on the
// amendment session branch after the first stage, then reloads the in-memory thread.
// Best-effort; no-op with no anchored comments.
// maybeSeedAmendment seeds the amendment ledger exactly once when an amendment session first
// reaches AwaitingReview, returning the updated seeded flag (keeps the spine flat).
func (wf *workflows) maybeSeedAmendment(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, seeded bool, state *coAuthorState) bool {
	if in.Amendment > 0 && !seeded {
		wf.seedAmendmentLedger(ctx, in, gf, headVersion, state)
		return true
	}
	return seeded
}

func (wf *workflows) seedAmendmentLedger(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, state *coAuthorState) {
	comments := feedbackToLedgerComments(in.Feedback)
	if len(comments) == 0 {
		return
	}
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSeedReviewCommentsOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, gf.readBackBranch(), toPSKind(in.ArtifactKind), 0, comments)
	})
	if err != nil {
		return
	}
	*headVersion = newVersion
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
}

// loadReviewThread reads the artifact slot's durable ledger from the session branch ("" ⇒
// main). Called after every (re)stage and every waive/reopen so the query + approve gate see
// the live thread. A read fault is returned; the caller keeps the last-known thread.
func (wf *workflows) loadReviewThread(ctx workflow.Context, in coAuthorInput, gf gitSession) ([]projectstate.ReviewComment, error) {
	proj, err := wf.readProjectOnBranch(ctx, in.ProjectID, gf.readBackBranch())
	if err != nil {
		return nil, err
	}
	return slotFor(proj, toPSKind(in.ArtifactKind)).ReviewThread, nil
}

// applyCommentStatus applies one human review-ledger transition (waive / reopen) on the
// session branch during the AwaitingReview window, then refreshes the in-memory thread.
// Best-effort: an illegal transition / unknown id / transient fault leaves the review session
// at the gate with the unchanged thread (the manager pre-check already rejected most bad
// requests synchronously).
func (wf *workflows) applyCommentStatus(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, sig setCommentStatusSignal, state *coAuthorState) {
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSetReviewCommentStatusOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, gf.readBackBranch(), toPSKind(in.ArtifactKind), sig.CommentID, sig.Status)
	})
	if err != nil {
		return
	}
	*headVersion = newVersion
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
}
