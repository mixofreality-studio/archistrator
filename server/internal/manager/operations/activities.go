package operations

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	fwmgr "github.com/davidmarne/archistrator-platform/framework-go/manager"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
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
func (wf *Workflows) ReadOperatedSystemActivity(ctx context.Context, operatedAppID OperatedAppID) (OperatedSystem, error) {
	return mapErr(wf.OperatedSystemState.ReadOperatedSystem(ctx, operatedAppID))
}

// ReadInFlightOperatedAppsActivity wraps operatedSystemStateAccess.readInFlightOperatedApps.
// Pure cross-row read; no idempotency key.
func (wf *Workflows) ReadInFlightOperatedAppsActivity(ctx context.Context, scope InFlightScope) ([]OperatedSystemSummary, error) {
	return mapErr(wf.OperatedSystemState.ReadInFlightOperatedApps(ctx, scope))
}

// RecordPublishDesiredStateArgs bundles the head-state desired-state transition inputs.
type RecordPublishDesiredStateArgs struct {
	AppID           OperatedAppID
	ExpectedVersion Version
	Reason          DesiredStateReason
	Decision        *AutoscaleDecisionSeam // carried only for reason=autoscale
}

// RecordPublishDesiredStateActivity wraps operatedSystemStateAccess.publishDesiredState
// (head-state, additive). idempotencyKey = "${workflowId}:${activityId}"; a stale
// expectedVersion surfaces fwra.Conflict for the §6.5 re-read loop.
func (wf *Workflows) RecordPublishDesiredStateActivity(ctx context.Context, a RecordPublishDesiredStateArgs) (Version, error) {
	return mapErr(wf.OperatedSystemState.PublishDesiredState(ctx, a.AppID, a.ExpectedVersion, a.Reason, a.Decision, activityIdempotencyKey(ctx)))
}

// RecordRuntimeStatusChangeArgs bundles the observed-status transition inputs.
type RecordRuntimeStatusChangeArgs struct {
	AppID           OperatedAppID
	ExpectedVersion Version
	Status          RuntimeStatusSeam
}

// RecordRuntimeStatusChangeActivity wraps operatedSystemStateAccess.recordRuntimeStatusChange.
func (wf *Workflows) RecordRuntimeStatusChangeActivity(ctx context.Context, a RecordRuntimeStatusChangeArgs) (Version, error) {
	return mapErr(wf.OperatedSystemState.RecordRuntimeStatusChange(ctx, a.AppID, a.ExpectedVersion, a.Status, activityIdempotencyKey(ctx)))
}

// WithdrawHeadStateArgs bundles the withdraw head-state transition inputs.
type WithdrawHeadStateArgs struct {
	AppID           OperatedAppID
	ExpectedVersion Version
}

// WithdrawHeadStateActivity wraps operatedSystemStateAccess.withdrawSystem (head-state
// → withdrawn).
func (wf *Workflows) WithdrawHeadStateActivity(ctx context.Context, a WithdrawHeadStateArgs) (Version, error) {
	return mapErr(wf.OperatedSystemState.WithdrawSystem(ctx, a.AppID, a.ExpectedVersion, activityIdempotencyKey(ctx)))
}

// RecordDelinquencyActionArgs bundles the delinquency-action transition inputs.
type RecordDelinquencyActionArgs struct {
	AppID           OperatedAppID
	ExpectedVersion Version
	Action          DelinquencyAction
}

// RecordDelinquencyActionActivity wraps operatedSystemStateAccess.recordDelinquencyAction.
func (wf *Workflows) RecordDelinquencyActionActivity(ctx context.Context, a RecordDelinquencyActionArgs) (Version, error) {
	return mapErr(wf.OperatedSystemState.RecordDelinquencyAction(ctx, a.AppID, a.ExpectedVersion, a.Action, activityIdempotencyKey(ctx)))
}

// =============================================================================
// operatedRuntimeAccess (GitOps/cluster/observability) Activities — 5 of 15.
// =============================================================================

// PublishDesiredStateArgs bundles the runtime publish inputs.
type PublishDesiredStateArgs struct {
	AppID   OperatedAppID
	Desired RuntimeDesiredState
}

// PublishDesiredStateActivity wraps operatedRuntimeAccess.publishDesiredState (git
// commit). idempotencyKey → deterministic commit message (git content-idempotent — no
// version guard).
func (wf *Workflows) PublishDesiredStateActivity(ctx context.Context, a PublishDesiredStateArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.OperatedRuntime.PublishDesiredState(ctx, a.AppID, a.Desired, activityIdempotencyKey(ctx)))
}

// WithdrawRuntimeActivity wraps operatedRuntimeAccess.withdraw. Idempotent on intent;
// the RA maps NotFound (already-gone) to success.
func (wf *Workflows) WithdrawRuntimeActivity(ctx context.Context, appID OperatedAppID) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.OperatedRuntime.Withdraw(ctx, appID, activityIdempotencyKey(ctx)))
}

// GetApplicationHealthActivity wraps operatedRuntimeAccess.getApplicationHealth. Pure read.
func (wf *Workflows) GetApplicationHealthActivity(ctx context.Context, appID OperatedAppID) (RuntimeStatusSeam, error) {
	return mapErr(wf.OperatedRuntime.GetApplicationHealth(ctx, appID))
}

// GetSloStatusActivity wraps operatedRuntimeAccess.getSloStatus. Pure read.
func (wf *Workflows) GetSloStatusActivity(ctx context.Context, appID OperatedAppID) (SloStatusSeam, error) {
	return mapErr(wf.OperatedRuntime.GetSloStatus(ctx, appID))
}

// ReadComputeAttributionActivity wraps operatedRuntimeAccess.readComputeAttribution.
// Pure read. The Manager pins the window to the reconcile-tick interval; here a default
// (open) window is passed and the RA attributes since last observation.
func (wf *Workflows) ReadComputeAttributionActivity(ctx context.Context, appID OperatedAppID) (ComputeAttribution, error) {
	return mapErr(wf.OperatedRuntime.ReadComputeAttribution(ctx, appID, AttributionWindow{}))
}

// =============================================================================
// artifactAccess Activity — 1 of 15.
// =============================================================================

// RetrieveDeployableBundleActivity wraps artifactAccess.retrieveDeployableBundle. Pure
// read; no idempotency key.
func (wf *Workflows) RetrieveDeployableBundleActivity(ctx context.Context, deployableBundleRef string) (DeployableBundle, error) {
	return mapErr(wf.Artifacts.RetrieveDeployableBundle(ctx, deployableBundleRef))
}

// =============================================================================
// usageAccess (append-only ledger) Activities — 3 of 15.
// =============================================================================

// RecordComputeUsageActivity wraps usageAccess.recordComputeUsage (append). Dedup-id
// idempotent on the event's RuntimeEventID (NO Conflict on this append-only ledger).
func (wf *Workflows) RecordComputeUsageActivity(ctx context.Context, event UsageEventSeam) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Usage.RecordComputeUsage(ctx, []UsageEventSeam{event}))
}

// RecordFinalUsageActivity wraps usageAccess.recordFinalUsage (append at withdraw).
func (wf *Workflows) RecordFinalUsageActivity(ctx context.Context, event UsageEventSeam) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Usage.RecordFinalUsage(ctx, []UsageEventSeam{event}))
}

// ReadUsageRangeActivity wraps usageAccess.readRange. Pure read; no idempotency key.
func (wf *Workflows) ReadUsageRangeActivity(ctx context.Context, query UsageRangeQuerySeam) ([]UsageEventSeam, error) {
	return mapErr(wf.Usage.ReadRange(ctx, query))
}
