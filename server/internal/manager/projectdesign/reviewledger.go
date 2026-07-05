package projectdesign

import (
	"context"

	"go.temporal.io/sdk/workflow"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// reviewledger.go holds the durable review-ledger seam for the projectDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05) — the structural twin of the
// systemDesign Manager's reviewledger.go. Ledger STORAGE + transition rules live in
// projectstate (reviewthread.go); this is the Manager-side wiring: the ReviewComment ↔
// ReviewCommentView projection, the open-comment gate, and the SetReviewCommentStatus
// branch-mutation Activity.

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

// openReviewCommentIDs returns the ids of every OPEN ledger entry — the comments that gate
// approve (review-ledger §4). Empty ⇒ approve is unblocked.
func openReviewCommentIDs(thread []projectstate.ReviewComment) []string {
	var ids []string
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentOpen {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// setCommentStatusArgs bundles the SetReviewCommentStatus branch mutation inputs across the
// Activity boundary. Branch is the session branch the ledger lives on ("" ⇒ main).
type setCommentStatusArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Kind            projectstate.ArtifactKind
	CommentID       string
	Status          string
	Branch          string
}

// SetReviewCommentStatusActivity wraps the review-ledger SetReviewCommentStatus verb — the
// human waive/reopen transition on the session branch during the AwaitingReview window.
func (wf *workflows) SetReviewCommentStatusActivity(ctx context.Context, a setCommentStatusArgs) (projectstate.Version, error) {
	if led, ok := wf.ProjectState.(projectstate.LedgerProjectStateAccess); ok {
		return mapErr(led.SetReviewCommentStatusOnBranch(ctx, a.ProjectID, a.ExpectedVersion, a.Branch, a.Kind, a.CommentID, a.Status, activityIdempotencyKey(ctx)))
	}
	return mapErr(projectstate.Version(0), fwra.New(fwra.NotFound, "review ledger not supported by this substrate"))
}

// seedReviewCommentsArgs bundles the F38 amendment ledger-seed inputs across the Activity
// boundary (Phase-2 twin).
type seedReviewCommentsArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Kind            projectstate.ArtifactKind
	Round           int64
	Comments        []projectstate.ReviewComment
	Branch          string
}

// SeedReviewCommentsActivity wraps the review-ledger seed verb (F38): appends the reopening
// feedback as OPEN entries (no status change) to the amendment session's freshly-staged slot.
func (wf *workflows) SeedReviewCommentsActivity(ctx context.Context, a seedReviewCommentsArgs) (projectstate.Version, error) {
	if led, ok := wf.ProjectState.(projectstate.LedgerProjectStateAccess); ok {
		return mapErr(led.SeedReviewCommentsOnBranch(ctx, a.ProjectID, a.ExpectedVersion, a.Branch, a.Kind, a.Round, a.Comments, activityIdempotencyKey(ctx)))
	}
	return mapErr(projectstate.Version(0), fwra.New(fwra.NotFound, "review ledger not supported by this substrate"))
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
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.SeedReviewCommentsActivity, seedReviewCommentsArgs{
			ProjectID:       projectstate.ProjectID(in.ProjectID),
			ExpectedVersion: expected,
			Kind:            toPSKind(in.ArtifactKind),
			Round:           0,
			Comments:        comments,
			Branch:          gf.readBackBranch(),
		}).Get(ctx, &v)
		return v, e
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
	c := readProjectOpts(ctx)
	var pe projectEnvelope
	if err := workflow.ExecuteActivity(c, wf.ReadProjectOnBranchActivity, readProjectOnBranchArgs{
		ProjectID: projectstate.ProjectID(in.ProjectID),
		Branch:    gf.readBackBranch(),
	}).Get(ctx, &pe); err != nil {
		return nil, err
	}
	proj, err := pe.decode()
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
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.SetReviewCommentStatusActivity, setCommentStatusArgs{
			ProjectID:       projectstate.ProjectID(in.ProjectID),
			ExpectedVersion: expected,
			Kind:            toPSKind(in.ArtifactKind),
			CommentID:       sig.CommentID,
			Status:          sig.Status,
			Branch:          gf.readBackBranch(),
		}).Get(ctx, &v)
		return v, e
	})
	if err != nil {
		return
	}
	*headVersion = newVersion
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
}
