package systemdesign

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
	fwmanager "github.com/davidmarne/archistrator-platform/framework-go/manager"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// This file holds the Manager-owned Temporal Activity wrappers — one per
// ResourceAccess call the workflow makes (systemDesignManager.md §6.4). They are
// METHODS ON THE Workflows STRUCT: there is no separate Activities type. The RA
// interface dependencies (ProjectState / Workers) live as fields on Workflows
// (see workflow.go) and are reached "on the struct", but the calls run inside
// Temporal Activities because those RA operations are I/O / non-deterministic and
// would break replay determinism if invoked on the workflow goroutine. The Engine
// (artifactValidationEngine) is deliberately NOT an Activity: it is a pure
// deterministic function the workflow body calls directly via wf.Validator
// (systemDesignManager.md §6.4 "Not Activities").
//
// 2026-05-29 re-cut (systemDesignManager.md §0b / rework §6): the engine-dispatch
// GenerateMethodArtifactActivity is GONE (systemDesignEngine deleted). Drafting
// AND PM-critique are the single generic GenerateTypedDataActivity wrapping
// workerAccess.GenerateTypedData[T] — the Manager's SEQUENCE assembles the prompt
// (prompts.go) and chooses the typed T. The render Activities are also gone
// (rendering is a sync read-path op on the Manager, off the replayed path).
//
// Each WRITE Activity body derives the idempotency key "${workflowId}:${activityId}"
// from the Temporal activity context (so the RA layer never reads Temporal
// context — D-PA §3, D-WA §3) and runs the port result through the generic error
// mapper mapErr (errors.go) to tag terminal port failures with their stable
// Temporal error Type().

// activityIdempotencyKey derives "${workflowId}:${activityId}" from the running
// Activity's info. The ActivityID is unique per activity invocation within a
// workflow, giving the stable, distinct key each logical write needs
// (systemDesignManager.md §6.4; D-PA §3).
func activityIdempotencyKey(ctx context.Context) fwra.IdempotencyKey {
	info := activity.GetInfo(ctx)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%s", info.WorkflowExecution.ID, info.ActivityID))
}

// ---- ReadProjectActivity (wraps projectStateAccess.readProject) -------------
// Pure whole-aggregate read; no idempotency key (systemDesignManager.md §6.4).
// Returns the head-state as a Temporal-serializable projectEnvelope (the typed
// slot Models are interfaces the default JSON converter cannot decode — codec.go).

func (wf *Workflows) ReadProjectActivity(ctx context.Context, projectID projectstate.ProjectID) (projectEnvelope, error) {
	proj, err := wf.ProjectState.ReadProject(ctx, projectID)
	if err != nil {
		return projectEnvelope{}, fwmanager.MapError(err)
	}
	return encodeProject(proj)
}

// ---- ReadProjectOnBranchActivity (branch-aware read-back, I-DESIGN-DISPATCH §2a) ----
// The agentic design rail reads back the not-yet-merged draft on the SESSION BRANCH
// during the AwaitingReview window. This Activity routes to the branch-aware extension
// when the ProjectState substrate supports it; otherwise it falls back to the main-path
// ReadProject (branch override ignored) so a non-git/Postgres substrate is unperturbed.
// A pure read; no idempotency key.

// ReadProjectOnBranchArgs bundles the branch-aware read inputs.
type ReadProjectOnBranchArgs struct {
	ProjectID projectstate.ProjectID
	Branch    string
}

func (wf *Workflows) ReadProjectOnBranchActivity(ctx context.Context, a ReadProjectOnBranchArgs) (projectEnvelope, error) {
	var (
		proj projectstate.Project
		err  error
	)
	if ba, ok := wf.ProjectState.(projectstate.BranchAwareProjectStateAccess); ok && a.Branch != "" {
		proj, err = ba.ReadProjectOnBranch(ctx, a.ProjectID, a.Branch)
	} else {
		proj, err = wf.ProjectState.ReadProject(ctx, a.ProjectID)
	}
	if err != nil {
		return projectEnvelope{}, fwmanager.MapError(err)
	}
	return encodeProject(proj)
}

// ---- Project head-state mutation Activities ---------------------------------
// Each wraps one atomic verb on projectStateAccess. The idempotencyKey is
// derived per Activity invocation and recorded in the RA's dedup ledger under
// UNIQUE(project_id, idempotency_key) (projectStateAccess.md §2.1, §6). A
// stale-version fwra.Conflict surfaces as the canonical Temporal Type(); the
// Manager's workflow-level applyRecovering loop re-reads the head version and
// re-applies with the SAME key (an idempotent no-op if the prior attempt already
// committed). Terminal on ContractMisuse.

// StageArtifactForReviewArgs carries the TYPED model into its slot (D-PA: stage
// carries the model, routed to its slot by model.Kind()). The model is carried as
// an envelope across the Temporal boundary (codec.go).
type StageArtifactForReviewArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Model           modelEnvelope
	// Branch is the OPTIONAL session-branch override (I-DESIGN-DISPATCH §2a). Empty ⇒
	// the AwaitingReview thin-write lands on main exactly as today (every existing
	// caller/test leaves it empty and is unperturbed). Non-empty ⇒ the staged-slot
	// status flip rides over the session branch the draft lives on, routed to the
	// branch-aware extension when the substrate supports it.
	Branch string
}

func (wf *Workflows) StageArtifactForReviewActivity(ctx context.Context, a StageArtifactForReviewArgs) (projectstate.Version, error) {
	model, err := a.Model.decode()
	if err != nil {
		return 0, fwmanager.MapError(err)
	}
	if ba, ok := wf.ProjectState.(projectstate.BranchAwareProjectStateAccess); ok && a.Branch != "" {
		return mapErr(ba.StageArtifactForReviewOnBranch(ctx, a.ProjectID, a.ExpectedVersion, a.Branch, model, activityIdempotencyKey(ctx)))
	}
	return mapErr(wf.ProjectState.StageArtifactForReview(ctx, a.ProjectID, a.ExpectedVersion, model, activityIdempotencyKey(ctx)))
}

// MutateArtifactArgs bundles the inputs for the per-artifact review verbs that
// key by Kind only (the model already lives in the slot from staging — D-PA §2).
// Commit ignores Notes; Reject/Withdraw carry the architect's notes.
type MutateArtifactArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Kind            projectstate.ArtifactKind
	Notes           string
}

func (wf *Workflows) CommitArtifactActivity(ctx context.Context, a MutateArtifactArgs) (projectstate.Version, error) {
	return mapErr(wf.ProjectState.CommitArtifact(ctx, a.ProjectID, a.ExpectedVersion, a.Kind, activityIdempotencyKey(ctx)))
}

func (wf *Workflows) RejectArtifactActivity(ctx context.Context, a MutateArtifactArgs) (projectstate.Version, error) {
	return mapErr(wf.ProjectState.RejectArtifact(ctx, a.ProjectID, a.ExpectedVersion, a.Kind, a.Notes, activityIdempotencyKey(ctx)))
}

func (wf *Workflows) WithdrawArtifactActivity(ctx context.Context, a MutateArtifactArgs) (projectstate.Version, error) {
	return mapErr(wf.ProjectState.WithdrawArtifact(ctx, a.ProjectID, a.ExpectedVersion, a.Kind, a.Notes, activityIdempotencyKey(ctx)))
}

// AdvancePhaseArgs bundles the seal verb's inputs for the Activity boundary.
type AdvancePhaseArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
}

func (wf *Workflows) AdvancePhaseActivity(ctx context.Context, a AdvancePhaseArgs) (projectstate.Version, error) {
	return mapErr(wf.ProjectState.AdvancePhase(ctx, a.ProjectID, a.ExpectedVersion, activityIdempotencyKey(ctx)))
}
