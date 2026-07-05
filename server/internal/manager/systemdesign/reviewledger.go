package systemdesign

import (
	"context"

	"go.temporal.io/sdk/workflow"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// reviewledger.go holds the durable review-ledger seam for the systemDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05): the projectstate.ReviewComment
// ↔ ReviewCommentView projection the sessionState Query surfaces, the open-comment gate
// the approve precondition reads, and the SetReviewCommentStatus branch-mutation Activity.
// The ledger STORAGE + transition rules live in projectstate (reviewthread.go); this file
// is only the Manager-side wiring.

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

// openReviewCommentIDs returns the ids of every OPEN ledger entry — the comments that
// gate approve (review-ledger §4). Empty ⇒ approve is unblocked.
func openReviewCommentIDs(thread []projectstate.ReviewComment) []string {
	var ids []string
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentOpen {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// setCommentStatusArgs bundles the SetReviewCommentStatus branch mutation inputs across
// the Activity boundary. Branch is the session branch the ledger lives on ("" ⇒ main).
type setCommentStatusArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Kind            projectstate.ArtifactKind
	CommentID       string
	Status          string
	Branch          string
}

// SetReviewCommentStatusActivity wraps the review-ledger SetReviewCommentStatus verb: it
// applies the human status transition to one ledger entry on the session branch during the
// AwaitingReview window (same substrate routing as Reject). Falls back to nothing when the
// substrate lacks the ledger extension — a non-ledger substrate has no thread to mutate, so
// the op is a NotFound the manager surfaces as FailedPrecondition. Terminal on ContractMisuse.
func (wf *workflows) SetReviewCommentStatusActivity(ctx context.Context, a setCommentStatusArgs) (projectstate.Version, error) {
	if led, ok := wf.ProjectState.(projectstate.LedgerProjectStateAccess); ok {
		return mapErr(led.SetReviewCommentStatusOnBranch(ctx, a.ProjectID, a.ExpectedVersion, a.Branch, a.Kind, a.CommentID, a.Status, activityIdempotencyKey(ctx)))
	}
	return mapErr(projectstate.Version(0), fwra.New(fwra.NotFound, "review ledger not supported by this substrate"))
}

// seedReviewCommentsArgs bundles the F38 amendment ledger-seed inputs across the Activity
// boundary. Branch is the amendment session branch the freshly-staged draft lives on.
type seedReviewCommentsArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Kind            projectstate.ArtifactKind
	Round           int64
	Comments        []projectstate.ReviewComment
	Branch          string
}

// SeedReviewCommentsActivity wraps the review-ledger seed verb: it appends the reopening
// feedback as OPEN entries (no status change) to the amendment session's freshly-staged
// slot. A non-ledger substrate has no thread to seed → NotFound (surfaced up; the amendment
// still proceeds with the feedback woven into the prompt).
func (wf *workflows) SeedReviewCommentsActivity(ctx context.Context, a seedReviewCommentsArgs) (projectstate.Version, error) {
	if led, ok := wf.ProjectState.(projectstate.LedgerProjectStateAccess); ok {
		return mapErr(led.SeedReviewCommentsOnBranch(ctx, a.ProjectID, a.ExpectedVersion, a.Branch, a.Kind, a.Round, a.Comments, activityIdempotencyKey(ctx)))
	}
	return mapErr(projectstate.Version(0), fwra.New(fwra.NotFound, "review ledger not supported by this substrate"))
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

// loadReviewThread reads the artifact slot's durable ledger from the session branch (the
// same branch the draft is staged on; "" ⇒ main). Called on the workflow goroutine after
// every (re)stage and after every waive/reopen so the sessionState Query + the approve gate
// see the live thread. A read fault is returned to the caller, which keeps the last-known
// thread (the ledger is auxiliary display/gate state — a transient read miss must not derail
// the review session).
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
	return slotFor(proj, in.ArtifactKind).ReviewThread, nil
}

// applyCommentStatus applies one human review-ledger transition (waive / reopen) to the
// session branch during the AwaitingReview window, then refreshes the in-memory thread so
// the query + approve gate reflect it. Best-effort: an illegal transition / unknown id /
// transient fault leaves the review session at the gate with the unchanged thread (the
// manager's SetReviewCommentStatus pre-check already rejects most bad requests
// synchronously; this is the durable apply, not the validation point).
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
