package construction

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// activities_custom.go holds the CUSTOM Manager-owned Temporal Activity wrappers the
// generated temporalgen layer cannot emit — the two projectEnvelope-codec reads and the
// six constructionTransitionAccess head-state transition writes. (The six git head-state
// RecordActivity* writes are the other CUSTOM Activities; they live with their value
// carriers in gitactivities.go.) The contract-backed RA ops (pipeline / artifact / rail)
// are GENERATED (activities.gen.go) and reached through the generated invoker surface
// (genInvokers); these have no frozen contract behind them (the projectEnvelope
// concrete-projection codec and the plain-goType constructionTransition / gitActivityStatus
// deps), so temporalgen has nothing to generate and they are registered via the manifest's
// CustomActivities under their existing stable names (workermanifest.go).
//
// They are METHODS ON THE workflows STRUCT (construction's Activity receiver has always
// been the workflows struct). The RA dependencies live as fields on workflows (workflow.go)
// and are reached on the struct, but the calls run inside Temporal Activities because those
// RA operations are I/O / non-deterministic and would break replay determinism if invoked
// on the workflow goroutine. The three Engines (handOffEngine, interventionEngine,
// reviewEngine) are deliberately NOT Activities: they are pure deterministic functions the
// workflow body calls directly (constructionManager.md §6.4 "Not Activities").
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
	return mapErr(wf.ConstructionTransition.RecordChangeReviewed(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
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
	return mapErr(wf.ConstructionTransition.RecordActivityExited(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID, a.Outcome, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
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
	return mapErr(wf.ConstructionTransition.RecordActivityFailed(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID, a.Reason, a.Detail, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordOperatorPausedArgs bundles the inputs for recordOperatorPaused.
type recordOperatorPausedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	Reason          string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordOperatorPausedActivity(ctx context.Context, a recordOperatorPausedArgs) (projectstate.Version, error) {
	return mapErr(wf.ConstructionTransition.RecordOperatorPaused(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.Reason, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordPhaseStartedArgs bundles the inputs for RecordPhaseStarted.
type recordPhaseStartedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Phase           projectstate.ActivityMethodPhase
	Cred            railCredEnvelope
}

func (wf *workflows) RecordPhaseStartedActivity(ctx context.Context, a recordPhaseStartedArgs) (projectstate.Version, error) {
	return mapErr(wf.ConstructionTransition.RecordPhaseStarted(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID, a.Phase, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordPhaseCompletedArgs bundles the inputs for RecordPhaseCompleted.
// ArtifactRef is the content-address of any phase artifact (empty string if none).
type recordPhaseCompletedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Phase           projectstate.ActivityMethodPhase
	ArtifactRef     string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordPhaseCompletedActivity(ctx context.Context, a recordPhaseCompletedArgs) (projectstate.Version, error) {
	return mapErr(wf.ConstructionTransition.RecordPhaseCompleted(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID, a.Phase, a.ArtifactRef, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}
