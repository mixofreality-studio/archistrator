package operations

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// This file holds the Manager-owned Temporal Activity wrappers — one per
// ResourceAccess call the workflows make (operationsManager.md §6.4 — 15 Activities).
// They are METHODS ON THE Workflows STRUCT: there is no separate Activities type. The
// RA dependencies live as fields on Workflows (workflow.go) and are reached on the
// struct, but the calls run inside Temporal Activities because those RA operations are
// I/O / non-deterministic and would break replay determinism if invoked on the
// workflow goroutine. The three Engines (interventionEngine, autoscalerEngine,
// operationEstimationEngine) are deliberately NOT Activities: they are pure
// deterministic functions the workflow body calls directly (operationsManager.md §6.4
// "Not Activities").
//
// Each WRITE Activity body derives the idempotencyKey "${workflowId}:${activityId}"
// from the Temporal activity context (so the RA layer never reads Temporal context)
// and runs the port result through the generic error mapper mapErr (errors.go) to tag
// terminal port failures with their stable Temporal error Type(). The append-only
// usage writes ALSO derive the key but the RA dedups on the runtime event id carried
// in the event (usageAccess.md §3) — the key is supplied for traceability.

// activityIdempotencyKey derives "${workflowId}:${activityId}" from the running
// Activity's info — the stable, distinct key each logical write needs
// (operationsManager.md §6.4; §6.5).
func activityIdempotencyKey(ctx context.Context) fwra.IdempotencyKey {
	info := activity.GetInfo(ctx)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%s", info.WorkflowExecution.ID, info.ActivityID))
}

// =============================================================================
// operatedSystemStateAccess (head-state) Activities — 6 of 15.
// =============================================================================

// ReadOperatedSystemActivity wraps operatedSystemStateAccess.readOperatedSystem. Pure
// whole-aggregate read; no idempotency key (operationsManager.md §6.4).
func (wf *workflows) ReadOperatedSystemActivity(ctx context.Context, operatedAppID operatedAppID) (operatedSystem, error) {
	return mapErr(wf.OperatedSystemState.ReadOperatedSystem(ctx, operatedAppID))
}

// ReadInFlightOperatedAppsActivity wraps operatedSystemStateAccess.readInFlightOperatedApps.
// Pure cross-row read; no idempotency key.
func (wf *workflows) ReadInFlightOperatedAppsActivity(ctx context.Context, scope inFlightScope) ([]operatedSystemSummary, error) {
	return mapErr(wf.OperatedSystemState.ReadInFlightOperatedApps(ctx, scope))
}

// recordPublishDesiredStateArgs bundles the head-state desired-state transition inputs.
type recordPublishDesiredStateArgs struct {
	AppID           operatedAppID
	ExpectedVersion version
	Reason          DesiredStateReason
	Decision        *autoscaleDecisionSeam // carried only for reason=autoscale
}

// RecordPublishDesiredStateActivity wraps operatedSystemStateAccess.publishDesiredState
// (head-state, additive). idempotencyKey = "${workflowId}:${activityId}"; a stale
// expectedVersion surfaces fwra.Conflict for the §6.5 re-read loop.
func (wf *workflows) RecordPublishDesiredStateActivity(ctx context.Context, a recordPublishDesiredStateArgs) (version, error) {
	return mapErr(wf.OperatedSystemState.PublishDesiredState(ctx, a.AppID, a.ExpectedVersion, a.Reason, a.Decision, activityIdempotencyKey(ctx)))
}

// recordRuntimeStatusChangeArgs bundles the observed-status transition inputs.
type recordRuntimeStatusChangeArgs struct {
	AppID           operatedAppID
	ExpectedVersion version
	Status          RuntimeStatusSeam
}

// RecordRuntimeStatusChangeActivity wraps operatedSystemStateAccess.recordRuntimeStatusChange.
func (wf *workflows) RecordRuntimeStatusChangeActivity(ctx context.Context, a recordRuntimeStatusChangeArgs) (version, error) {
	return mapErr(wf.OperatedSystemState.RecordRuntimeStatusChange(ctx, a.AppID, a.ExpectedVersion, a.Status, activityIdempotencyKey(ctx)))
}

// withdrawHeadStateArgs bundles the withdraw head-state transition inputs.
type withdrawHeadStateArgs struct {
	AppID           operatedAppID
	ExpectedVersion version
}

// WithdrawHeadStateActivity wraps operatedSystemStateAccess.withdrawSystem (head-state
// → withdrawn).
func (wf *workflows) WithdrawHeadStateActivity(ctx context.Context, a withdrawHeadStateArgs) (version, error) {
	return mapErr(wf.OperatedSystemState.WithdrawSystem(ctx, a.AppID, a.ExpectedVersion, activityIdempotencyKey(ctx)))
}

// recordDelinquencyActionArgs bundles the delinquency-action transition inputs.
type recordDelinquencyActionArgs struct {
	AppID           operatedAppID
	ExpectedVersion version
	Action          delinquencyAction
}

// RecordDelinquencyActionActivity wraps operatedSystemStateAccess.recordDelinquencyAction.
func (wf *workflows) RecordDelinquencyActionActivity(ctx context.Context, a recordDelinquencyActionArgs) (version, error) {
	return mapErr(wf.OperatedSystemState.RecordDelinquencyAction(ctx, a.AppID, a.ExpectedVersion, a.Action, activityIdempotencyKey(ctx)))
}

// =============================================================================
// operatedRuntimeAccess (GitOps/cluster/observability) Activities — 5 of 15.
// =============================================================================

// publishDesiredStateArgs bundles the runtime publish inputs.
type publishDesiredStateArgs struct {
	AppID   operatedAppID
	Desired runtimeDesiredState
}

// PublishDesiredStateActivity wraps operatedRuntimeAccess.publishDesiredState (git
// commit). idempotencyKey → deterministic commit message (git content-idempotent — no
// version guard).
func (wf *workflows) PublishDesiredStateActivity(ctx context.Context, a publishDesiredStateArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.OperatedRuntime.PublishDesiredState(ctx, a.AppID, a.Desired, activityIdempotencyKey(ctx)))
}

// WithdrawRuntimeActivity wraps operatedRuntimeAccess.withdraw. Idempotent on intent;
// the RA maps NotFound (already-gone) to success.
func (wf *workflows) WithdrawRuntimeActivity(ctx context.Context, appID operatedAppID) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.OperatedRuntime.Withdraw(ctx, appID, activityIdempotencyKey(ctx)))
}

// GetApplicationHealthActivity wraps operatedRuntimeAccess.getApplicationHealth. Pure read.
func (wf *workflows) GetApplicationHealthActivity(ctx context.Context, appID operatedAppID) (RuntimeStatusSeam, error) {
	return mapErr(wf.OperatedRuntime.GetApplicationHealth(ctx, appID))
}

// GetSloStatusActivity wraps operatedRuntimeAccess.getSloStatus. Pure read.
func (wf *workflows) GetSloStatusActivity(ctx context.Context, appID operatedAppID) (sloStatusSeam, error) {
	return mapErr(wf.OperatedRuntime.GetSloStatus(ctx, appID))
}

// ReadComputeAttributionActivity wraps operatedRuntimeAccess.readComputeAttribution.
// Pure read. The Manager pins the window to the reconcile-tick interval; here a default
// (open) window is passed and the RA attributes since last observation.
func (wf *workflows) ReadComputeAttributionActivity(ctx context.Context, appID operatedAppID) (computeAttribution, error) {
	return mapErr(wf.OperatedRuntime.ReadComputeAttribution(ctx, appID, attributionWindow{}))
}

// =============================================================================
// artifactAccess Activity — 1 of 15.
// =============================================================================

// RetrieveDeployableBundleActivity wraps artifactAccess.retrieveDeployableBundle. Pure
// read; no idempotency key.
func (wf *workflows) RetrieveDeployableBundleActivity(ctx context.Context, deployableBundleRef string) (deployableBundle, error) {
	return mapErr(wf.Artifacts.RetrieveDeployableBundle(ctx, deployableBundleRef))
}

// =============================================================================
// usageAccess (append-only ledger) Activities — 3 of 15.
// =============================================================================

// RecordComputeUsageActivity wraps usageAccess.recordComputeUsage (append). Dedup-id
// idempotent on the event's RuntimeEventID (NO Conflict on this append-only ledger).
func (wf *workflows) RecordComputeUsageActivity(ctx context.Context, event usageEventSeam) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Usage.RecordComputeUsage(ctx, []usageEventSeam{event}))
}

// RecordFinalUsageActivity wraps usageAccess.recordFinalUsage (append at withdraw).
func (wf *workflows) RecordFinalUsageActivity(ctx context.Context, event usageEventSeam) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Usage.RecordFinalUsage(ctx, []usageEventSeam{event}))
}

// ReadUsageRangeActivity wraps usageAccess.readRange. Pure read; no idempotency key.
func (wf *workflows) ReadUsageRangeActivity(ctx context.Context, query usageRangeQuerySeam) ([]usageEventSeam, error) {
	return mapErr(wf.Usage.ReadRange(ctx, query))
}
