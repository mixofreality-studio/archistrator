package systemdesign

import (
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// reviewledger.go holds the durable review-ledger seam for the systemDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05): the projectstate.ReviewComment
// ↔ ReviewCommentView projection the sessionState Query surfaces, the open-comment gate
// the approve precondition reads, and the SetReviewCommentStatus branch mutation. The
// ledger STORAGE + transition rules live in projectstate (reviewthread.go); the branch
// mutation itself is the GENERATED designSessionAccess.setReviewCommentStatusOnBranch /
// seedReviewCommentsOnBranch invoker (B10) — this file is only the Manager-side wiring
// (the wire-view projections, the reject/seed comment shaping, and the workflow-side
// apply/reload helpers).

// reviewAuthorRole is the role stamped on every comment the architect files at the
// System-Design review gate. The ledger records WHO filed each comment; in the design
// phase the reviewer at the gate is always the architect.
const reviewAuthorRole = "architect"

// setCommentStatusSignal is the SetReviewCommentStatus signal payload. It rides the
// signalSetCommentStatus channel to the CoAuthorArtifactWorkflow suspended at the
// AwaitingReview gate, which applies the branch mutation (open->waived / addressed->open).
type setCommentStatusSignal struct {
	CommentID string
	Status    string
}

// feedbackToLedgerComments converts the architect's inbound anchored comments (the wire
// AnchoredComment carried on a Reject's ReviewFeedback) into the projectstate.ReviewComment
// shape the append verb stamps into the durable thread. Only Anchor / AnchorText / Text /
// AuthorRole are filled — the id / round / open status / empty response are server-minted
// in appendReviewComments. Free-text-only Notes are NOT comments (they stay the reject
// notes); an anchored comment with empty Text is dropped (defensive).
func feedbackToLedgerComments(feedback ReviewFeedback) []projectstate.ReviewComment {
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
// Query returns (nil stays nil so the omitempty wire shape is unchanged for slots that
// never carried a comment).
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

// openReviewCommentIDs returns the ids of every OPEN CHANGE-REQUEST — the comments that
// gate approve (review-ledger §4). Open QUESTIONS never gate (question-comments §approve),
// so they are excluded. Empty ⇒ approve is unblocked.
func openReviewCommentIDs(thread []projectstate.ReviewComment) []string {
	var ids []string
	for _, c := range thread {
		if projectstate.ReviewCommentBlocksApprove(c) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// seedAmendmentLedger records the reopening feedback (coAuthorInput.Feedback) as round-0 OPEN
// ledger entries on the amendment session branch, right after the first stage, then reloads
// the in-memory thread so the query + prompt surface them. Best-effort: a seed miss (e.g. a
// non-ledger substrate) leaves the feedback in the prompt only. No-op when there are no
// anchored comments to seed.
// maybeSeedAmendment seeds the amendment ledger exactly once, the first time an amendment
// session reaches AwaitingReview, returning the (possibly-updated) seeded flag. Keeps the
// spine flat (the F38 guard lives here, not inline in the workflow body).
func (wf *workflows) maybeSeedAmendment(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, seeded bool, state *coAuthorState) bool {
	if in.Amendment > 0 && !seeded {
		wf.seedAmendmentLedger(ctx, in, gf, headVersion, state)
		return true
	}
	return seeded
}

func (wf *workflows) seedAmendmentLedger(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, state *coAuthorState) {
	if in.Feedback == nil {
		return
	}
	comments := feedbackToLedgerComments(*in.Feedback)
	if len(comments) == 0 {
		return
	}
	branch := gf.readBackBranch()
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, branch, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSeedReviewCommentsOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, branch, toPSKind(in.ArtifactKind), 0, comments)
	})
	if err != nil {
		return
	}
	*headVersion = newVersion
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
}

// loadReviewThread reads the artifact slot's durable ledger from the session branch (the
// same branch the draft is staged on; "" ⇒ main). Called on the workflow goroutine after
// every (re)stage and after every waive/reopen so the sessionState Query + the approve gate
// see the live thread. A read fault is returned to the caller, which keeps the last-known
// thread (the ledger is auxiliary display/gate state — a transient read miss must not derail
// the review session). Delegates to the shared readProjectOnBranch helper (gitsession.go)
// rather than duplicating the read-and-decode inline.
func (wf *workflows) loadReviewThread(ctx workflow.Context, in coAuthorInput, gf gitSession) ([]projectstate.ReviewComment, error) {
	proj, err := wf.readProjectOnBranch(ctx, in.ProjectID, gf.readBackBranch())
	if err != nil {
		return nil, err
	}
	return slotFor(proj, in.ArtifactKind).ReviewThread, nil
}

// applyCommentStatus applies one human review-ledger transition (waive / reopen) to the
// session branch during the AwaitingReview window, then refreshes the in-memory thread so
// the query + approve gate reflect it. Best-effort: an illegal transition / unknown id /
// transient fault leaves the review session at the gate with the unchanged thread (the
// manager's SetReviewCommentStatus pre-check already rejects most bad requests
// synchronously; this is the durable apply, not the validation point).
func (wf *workflows) applyCommentStatus(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, sig setCommentStatusSignal, state *coAuthorState) {
	branch := gf.readBackBranch()
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, branch, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSetReviewCommentStatusOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, branch, toPSKind(in.ArtifactKind), sig.CommentID, sig.Status)
	})
	if err != nil {
		return
	}
	*headVersion = newVersion
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
}
