package projectdesign

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// This file holds the Manager-owned Temporal Activity wrappers — one per
// ResourceAccess call the workflow makes (projectDesignManager.md §6.4). They are
// METHODS ON THE workflows STRUCT: there is no separate Activities type. The RA
// interface dependencies (ProjectState + Pipeline) live as fields on workflows
// (see workflow.go) and are reached "on the struct", but the calls run inside
// Temporal Activities because those RA operations are I/O / non-deterministic and
// would break replay determinism if invoked on the workflow goroutine.
//
// 2026-06-15 agentic-pivot re-cut (projectDesignManager.md §0.5 / D-MPD-Δ): the
// Phase-2 plan-DRAFTING mechanism flips to dispatch → observe → read-back. The
// retired GenerateTypedDataActivity (the synchronous workerAccess path) is GONE; the
// new DispatchDesignJobActivity + ObserveDesignJobActivity (over
// constructionPipelineAccess, in dispatch.go) replace it. workerAccess and
// artifactValidationEngine are DROPPED from the draft path. The three estimate
// Engines (estimation, operationestimation, settlement) STAY — they are pure,
// deterministic, by-value joins the workflow body calls directly (contract §6.3/§6.4
// "Not Activities"; §0.5.5 "RETAINED, unchanged"). This file is the Phase-2 twin of
// systemdesign/activities.go.
//
// Each WRITE Activity body derives the idempotency key "${workflowId}:${activityId}"
// from the Temporal activity context (so the RA layer never reads Temporal
// context) and runs the port result through the generic error mapper mapErr.

// activityIdempotencyKey derives "${workflowId}:${activityId}" from the running
// Activity's info. The ActivityID is unique per activity invocation within a
// workflow, giving the stable, distinct key each logical write needs.
func activityIdempotencyKey(ctx context.Context) fwra.IdempotencyKey {
	info := activity.GetInfo(ctx)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%s", info.WorkflowExecution.ID, info.ActivityID))
}

// ---- ReadProjectActivity (wraps projectStateAccess.readProject) -------------
// Pure whole-aggregate read; no idempotency key. Returns the head-state as a
// Temporal-serializable projectEnvelope (the typed slot Models are interfaces the
// default JSON converter cannot decode — codec.go).

func (wf *workflows) ReadProjectActivity(ctx context.Context, projectID projectstate.ProjectID) (projectEnvelope, error) {
	proj, err := wf.ProjectState.ReadProject(fwra.Context{Context: ctx}, projectID)
	if err != nil {
		return projectEnvelope{}, fwmanager.MapError(err)
	}
	return encodeProject(proj)
}

// ---- ReadProjectVersionActivity (wraps projectStateAccess.ReadProjectVersion) ----
// Cheap version-only read; no idempotency key. Returns just the head-state Version
// across the Temporal boundary instead of the whole encoded aggregate — the
// applyRecovering Conflict loop needs only the token to re-seed its next attempt.
func (wf *workflows) ReadProjectVersionActivity(ctx context.Context, projectID projectstate.ProjectID) (projectstate.Version, error) {
	v, err := wf.ProjectState.ReadProjectVersion(fwra.Context{Context: ctx}, projectID)
	if err != nil {
		return 0, fwmanager.MapError(err)
	}
	return v, nil
}

// ReadProjectOnBranchActivity is the branch-aware read-back (I-DESIGN-DISPATCH §2a):
// the agentic design rail reads back the not-yet-merged draft on the SESSION BRANCH
// during the AwaitingReview window. Routes to the branch-aware extension when the
// substrate supports it AND a branch is supplied; otherwise falls back to the main-path
// ReadProject (branch ignored) so a non-git/Postgres substrate is unperturbed. Pure
// read; no idempotency key.

// readProjectOnBranchArgs bundles the branch-aware read inputs.
type readProjectOnBranchArgs struct {
	ProjectID projectstate.ProjectID
	Branch    string
}

func (wf *workflows) ReadProjectOnBranchActivity(ctx context.Context, a readProjectOnBranchArgs) (projectEnvelope, error) {
	var (
		proj projectstate.Project
		err  error
	)
	if ba, ok := wf.ProjectState.(projectstate.BranchAwareProjectStateAccess); ok && a.Branch != "" {
		proj, err = ba.ReadProjectOnBranch(ctx, a.ProjectID, a.Branch)
	} else {
		proj, err = wf.ProjectState.ReadProject(fwra.Context{Context: ctx}, a.ProjectID)
	}
	if err != nil {
		return projectEnvelope{}, fwmanager.MapError(err)
	}
	return encodeProject(proj)
}

// ---- Project head-state mutation Activities ---------------------------------
// Each wraps one atomic verb on projectStateAccess. The idempotencyKey is derived
// per Activity invocation; a stale-version fwra.Conflict surfaces as the canonical
// Temporal Type() and the workflow-level applyRecovering loop re-reads and
// re-applies with the SAME key. Terminal on ContractMisuse.

// stageArtifactForReviewArgs carries the TYPED model into its slot (the model is
// carried as an envelope across the Temporal boundary — codec.go).
type stageArtifactForReviewArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Model           modelEnvelope
	// Branch is the OPTIONAL session-branch override (I-DESIGN-DISPATCH §2a). Empty ⇒
	// the AwaitingReview thin-write lands on main exactly as today (every existing
	// caller/test leaves it empty and is unperturbed). Non-empty ⇒ the staged-slot
	// status flip rides over the session branch the draft lives on.
	Branch string
}

func (wf *workflows) StageArtifactForReviewActivity(ctx context.Context, a stageArtifactForReviewArgs) (projectstate.Version, error) {
	model, err := a.Model.decode()
	if err != nil {
		return 0, fwmanager.MapError(err)
	}
	if ba, ok := wf.ProjectState.(projectstate.BranchAwareProjectStateAccess); ok && a.Branch != "" {
		return mapErr(ba.StageArtifactForReviewOnBranch(ctx, a.ProjectID, a.ExpectedVersion, a.Branch, model, activityIdempotencyKey(ctx)))
	}
	return mapErr(wf.ProjectState.StageArtifactForReview(fwra.Context{Context: ctx, IdempotencyKey: activityIdempotencyKey(ctx)}, a.ProjectID, a.ExpectedVersion, model))
}

// mutateArtifactArgs bundles the inputs for the per-artifact review verbs that
// key by Kind only (the model already lives in the slot from staging). Commit
// ignores Notes; Reject/Withdraw carry the architect's notes.
type mutateArtifactArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Kind            projectstate.ArtifactKind
	Notes           string
}

func (wf *workflows) CommitArtifactActivity(ctx context.Context, a mutateArtifactArgs) (projectstate.Version, error) {
	return mapErr(wf.ProjectState.CommitArtifact(fwra.Context{Context: ctx, IdempotencyKey: activityIdempotencyKey(ctx)}, a.ProjectID, a.ExpectedVersion, a.Kind))
}

func (wf *workflows) RejectArtifactActivity(ctx context.Context, a mutateArtifactArgs) (projectstate.Version, error) {
	return mapErr(wf.ProjectState.RejectArtifact(fwra.Context{Context: ctx, IdempotencyKey: activityIdempotencyKey(ctx)}, a.ProjectID, a.ExpectedVersion, a.Kind, a.Notes))
}

func (wf *workflows) WithdrawArtifactActivity(ctx context.Context, a mutateArtifactArgs) (projectstate.Version, error) {
	return mapErr(wf.ProjectState.WithdrawArtifact(fwra.Context{Context: ctx, IdempotencyKey: activityIdempotencyKey(ctx)}, a.ProjectID, a.ExpectedVersion, a.Kind, a.Notes))
}

// advancePhaseArgs bundles the seal verb's inputs for the Activity boundary.
type advancePhaseArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
}

func (wf *workflows) AdvancePhaseActivity(ctx context.Context, a advancePhaseArgs) (projectstate.Version, error) {
	return mapErr(wf.ProjectState.AdvancePhase(fwra.Context{Context: ctx, IdempotencyKey: activityIdempotencyKey(ctx)}, a.ProjectID, a.ExpectedVersion))
}
