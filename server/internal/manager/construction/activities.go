package construction

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// This file holds the Manager-owned Temporal Activity wrappers — one per
// ResourceAccess call the workflow makes (constructionManager.md §6.4). They are
// METHODS ON THE workflows STRUCT: there is no separate Activities type. The RA
// dependencies live as fields on workflows (workflow.go) and are reached on the
// struct, but the calls run inside Temporal Activities because those RA operations
// are I/O / non-deterministic and would break replay determinism if invoked on the
// workflow goroutine. The three Engines (handOffEngine, interventionEngine,
// reviewEngine) are deliberately NOT Activities: they are pure deterministic
// functions the workflow body calls directly (constructionManager.md §6.4 "Not
// Activities").
//
// Each WRITE Activity body derives the idempotency key "${workflowId}:${activityId}"
// from the Temporal activity context (so the RA layer never reads Temporal context —
// D-PA §3, D-WA §3) and runs the port result through the generic error mapper
// mapErr (errors.go) to tag terminal port failures with their stable Temporal
// error Type().

// activityIdempotencyKey derives "${workflowId}:${activityId}" from the running
// Activity's info — the stable, distinct key each logical write needs
// (constructionManager.md §6.4; D-PA §3).
func activityIdempotencyKey(ctx context.Context) fwra.IdempotencyKey {
	info := activity.GetInfo(ctx)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%s", info.WorkflowExecution.ID, info.ActivityID))
}

// ---- ReadProjectActivity (wraps projectStateAccess.readProject) -------------
// Pure whole-aggregate read; no idempotency key (constructionManager.md §6.4).
func (wf *workflows) ReadProjectActivity(ctx context.Context, projectID projectstate.ProjectID) (projectEnvelope, error) {
	proj, err := wf.ProjectState.ReadProject(fwra.Context{Context: ctx}, projectID)
	if err != nil {
		return projectEnvelope{}, fwmanager.MapError(err)
	}
	return encodeProject(proj), nil
}

// ---- ReadProjectVersionActivity (wraps projectStateAccess.ReadProjectVersion) ----
// Cheap version-only read; no idempotency key. Returns just the head-state Version
// across the Temporal boundary instead of the whole encoded aggregate — the
// read-your-writes seed and the applyRecovering Conflict loop need only the token.
func (wf *workflows) ReadProjectVersionActivity(ctx context.Context, projectID projectstate.ProjectID) (projectstate.Version, error) {
	v, err := wf.ProjectState.ReadProjectVersion(fwra.Context{Context: ctx}, projectID)
	if err != nil {
		return 0, fwmanager.MapError(err)
	}
	return v, nil
}

// ---- GenerateWorkActivity (wraps the generic typed worker) ------------------
// The work-dispatch step (constructionManager.md §6.3 step 2 / step 5 review
// fan-out). The Manager's SEQUENCE assembled the prompt (prompts.go) and chose the
// WorkerClass; this Activity asks the worker for a typed ConstructionOutput.
//
// idempotencyKey = "${workflowId}:${activityId}" -> forwarded VERBATIM into
// worker.Generate (long StartToClose, small retry budget; §6.4). A worker
// UnmarshalError (the worker ran but produced a non-ConstructionOutput) becomes a
// non-retryable WorkerRefused terminal routed into intervention; transport/auth/
// quota errors bubble up via the canonical mapping for the RetryPolicy to act.

// generateWorkArgs bundles the generic worker dispatch inputs for the Activity
// boundary.
type generateWorkArgs struct {
	WorkerClass string
	Prompt      string
}

// workerRefusedErrType is the Temporal error Type() for the unconstructable /
// refused terminal (the worker ran but produced a non-ConstructionOutput).
const workerRefusedErrType = "WorkerRefused"

func (wf *workflows) GenerateWorkActivity(ctx context.Context, a generateWorkArgs) (artifact.ConstructionOutput, error) {
	key := activityIdempotencyKey(ctx)
	out, err := generateConstructionOutput(ctx, wf.Workers, workerGenerateSpec{
		WorkerClass: a.WorkerClass,
		Prompt:      a.Prompt,
	}, key)
	if err != nil {
		return artifact.ConstructionOutput{}, mapWorkerError(err)
	}
	return out, nil
}

// ---- CancelWorkerActivity (wraps workerAccess.Cancel) -----------------------
// The operator-pause / takeover abandon path (DSL-static Cancel(key) edge,
// constructionManager.md §6.3). Idempotent: an unknown key is success in the RA.
func (wf *workflows) CancelWorkerActivity(ctx context.Context, _ struct{}) (struct{}, error) {
	return struct{}{}, fwmanager.MapError(wf.Workers.Cancel(ctx, activityIdempotencyKey(ctx)))
}

// ---- constructionPipelineAccess Activities ----------------------------------

// SubmitPipelineActivity wraps submitConstructionPipeline (UC3 543). Deterministic
// Argo name from the caller-supplied key.
func (wf *workflows) SubmitPipelineActivity(ctx context.Context, spec pipelineSpec) (pipelineHandle, error) {
	return mapErr(wf.Pipeline.SubmitConstructionPipeline(ctx, spec, activityIdempotencyKey(ctx)))
}

// ObservePipelineActivity wraps observeConstructionPipeline (UC3 545). Pure read.
func (wf *workflows) ObservePipelineActivity(ctx context.Context, handle pipelineHandle) (pipelineObservation, error) {
	return mapErr(wf.Pipeline.ObserveConstructionPipeline(ctx, handle))
}

// CancelPipelineActivity wraps cancelConstructionPipeline (NCUC2 656). Idempotent
// on intent (ErrNotFound ⇒ success in the RA).
func (wf *workflows) CancelPipelineActivity(ctx context.Context, handle pipelineHandle) (struct{}, error) {
	return struct{}{}, fwmanager.MapError(wf.Pipeline.CancelConstructionPipeline(ctx, handle))
}

// ---- artifactAccess Activity ------------------------------------------------

// StoreConstructionOutputActivity wraps storeConstructionOutput (UC3 546/549).
// Content-addressable; caller-supplied key.
func (wf *workflows) StoreConstructionOutputActivity(ctx context.Context, output artifact.ConstructionOutput) (string, error) {
	return mapErr(wf.Artifacts.StoreConstructionOutput(ctx, output, activityIdempotencyKey(ctx)))
}

// ---- projectStateAccess construction-transition Activities ------------------
// Each wraps one additive head-state transition verb. The idempotencyKey is
// derived per Activity invocation; a stale-version fwra.Conflict surfaces as the
// canonical Temporal Type() and the workflow-level applyRecovering loop re-reads
// the head version and re-applies with the SAME key (constructionManager.md §6.5).

// recordChangeReviewedArgs bundles the inputs for recordChangeReviewed. Cred is the
// Manager-threaded credential (empty/zero in the dev/dry-run profile).
type recordChangeReviewedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordChangeReviewedActivity(ctx context.Context, a recordChangeReviewedArgs) (projectstate.Version, error) {
	return mapErr(wf.ConstructionTransition.RecordChangeReviewed(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordActivityExitedArgs bundles the inputs for recordActivityExited.
type recordActivityExitedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Outcome         projectstate.ActivityOutcome
	Cred            railCredEnvelope
}

func (wf *workflows) RecordActivityExitedActivity(ctx context.Context, a recordActivityExitedArgs) (projectstate.Version, error) {
	return mapErr(wf.ConstructionTransition.RecordActivityExited(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID, a.Outcome, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordActivityFailedArgs bundles the inputs for recordActivityFailed (the
// terminal-FAILURE head-state transition — bounded-wait / autonomous-retry fix).
type recordActivityFailedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Reason          projectstate.FailureReason
	Detail          string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordActivityFailedActivity(ctx context.Context, a recordActivityFailedArgs) (projectstate.Version, error) {
	return mapErr(wf.ConstructionTransition.RecordActivityFailed(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID, a.Reason, a.Detail, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordOperatorPausedArgs bundles the inputs for recordOperatorPaused.
type recordOperatorPausedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Reason          string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordOperatorPausedActivity(ctx context.Context, a recordOperatorPausedArgs) (projectstate.Version, error) {
	return mapErr(wf.ConstructionTransition.RecordOperatorPaused(ctx, a.ProjectID, a.ExpectedVersion, a.Reason, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}
