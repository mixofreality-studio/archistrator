package operations

import (
	"errors"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// This file holds the Workflows struct (the Manager's downstream dependency set),
// the four workflow bodies + the delinquency-enforcement branch (the encapsulated
// OperationsWorkflow volatility — operationsManager.md §6.3), the workflow-level
// Conflict re-read→re-apply loop (§6.5), and the activity-option presets.
//
// How the two dependency kinds are reached differs by determinism class:
//   - The three Engines (Intervention / Autoscaler / Estimation) are PURE,
//     deterministic, called DIRECTLY in-workflow (no Activity wrapper — replay-safe).
//   - The ResourceAccess ports (OperatedSystemState / OperatedRuntime / Usage /
//     Artifacts) are I/O and NON-deterministic; the workflow invokes the Activity
//     methods on this same struct via workflow.ExecuteActivity (activities.go).

// wfDeps bundles every downstream dependency the operationsManager orchestrates,
// passed to newWorkflows (from WorkerManifest, workermanifest.go) and held on the
// Workflows struct. The three Engines are consumer-defined seam interfaces (deps.go),
// called DIRECTLY in-workflow. The ResourceAccess layer is reached ONLY through the
// generated typed invoker surface (Acts, invokers.gen.go) — the former RA consumer
// seams + composition-root adapters are retired.
type wfDeps struct {
	Intervention interventionEngine
	Autoscaler   autoscalerEngine
	Estimation   operationEstimationEngine

	// Acts is the generated workflow-side invoker surface (invokers.gen.go): one
	// method per ResourceAccess activity, carrying contract types. Its Opts hook
	// supplies the per-activity option presets (workermanifest.go).
	Acts genInvokers

	// Policy snapshots fed to the Engines by value. In production the Manager reads
	// them from head-state; held here as the construction-time seam values.
	InterventionPolicy interventionPolicy
	AutoscalerPolicy   autoscalerPolicy
	InfrastructureKind infrastructureKind

	// CurrentCycleID is the billing cycle the Manager attributes observed usage to
	// (carried onto the usage events). Held here as the construction-time seam value;
	// in production the Manager derives it from the operated app's billing context.
	CurrentCycleID string
	CustomerID     customerID
}

// workflows is the single operationsManager component struct — the workflow receiver.
// The RA activities are the generated genActivities (activities.gen.go); this struct
// reaches them through the typed invokers (Acts).
type workflows struct {
	Intervention interventionEngine
	Autoscaler   autoscalerEngine
	Estimation   operationEstimationEngine

	Acts genInvokers

	InterventionPolicy interventionPolicy
	AutoscalerPolicy   autoscalerPolicy
	InfrastructureKind infrastructureKind
	CurrentCycleID     string
	CustomerID         customerID
}

// newWorkflows builds the Workflows receiver from the injected wfDeps.
func newWorkflows(d wfDeps) *workflows {
	return &workflows{
		Intervention:       d.Intervention,
		Autoscaler:         d.Autoscaler,
		Estimation:         d.Estimation,
		Acts:               d.Acts,
		InterventionPolicy: d.InterventionPolicy,
		AutoscalerPolicy:   d.AutoscalerPolicy,
		InfrastructureKind: d.InfrastructureKind,
		CurrentCycleID:     d.CurrentCycleID,
		CustomerID:         d.CustomerID,
	}
}

// Bounds (in-workflow guards; NOT contract surface).
const (
	// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
	// loop (§6.5).
	maxMutateConflictAttempts = 20
)

// raConflictErrType is the canonical Temporal Type() a head-state mutation Activity
// surfaces when expectedVersion is stale; the workflow recovers with the bounded
// re-read→re-apply loop (§6.5).
var raConflictErrType = fwmgr.RAErrType(fwra.Conflict)

// raNotFoundErrType is the canonical Temporal Type() ReadOperatedSystem surfaces for a
// missing operated app.
var raNotFoundErrType = fwmgr.RAErrType(fwra.NotFound)

// ===========================================================================
// DeployWorkflow — op 2.1 entry (operator deploy / scale / policy republish).
// ===========================================================================

// deployInput is the start payload for DeployWorkflow.
type deployInput struct {
	OperatedAppID operatedAppID
	Change        DesiredStateChange
}

// DeployWorkflow drives UC4 deploy (operationsManager.md §6.3):
//  1. ReadOperatedSystemActivity → head-state (desiredState, deployableBundleRef).
//  2. (first deploy, full bundle) RetrieveDeployableBundleActivity.
//  3. PublishDesiredStateActivity (the git commit).
//  4. RecordPublishDesiredStateActivity (head-state transition, reason=operator|deploy).
func (wf *workflows) DeployWorkflow(ctx workflow.Context, in deployInput) (DeployResult, error) {
	logger := workflow.GetLogger(ctx)

	op, err := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if err != nil {
		return DeployResult{}, err
	}

	// Deploy pre-condition (§2.1): the operated system has a deployableBundleRef for a
	// first take-live (full bundle). FailedPrecondition is a terminal façade-class
	// error surfaced from the workflow.
	if in.Change.Reason == ReasonDeployAfterConstruction && in.Change.PatchKind == PatchFullBundle {
		if op.DeployableBundleRef == "" {
			return DeployResult{}, temporal.NewNonRetryableApplicationError(
				"operated system has no deployableBundleRef (no constructed output to deploy)",
				fwmgr.ManagerErrType(fwmgr.FailedPrecondition), nil)
		}
		// Retrieve the deployable bundle the publish renders from.
		if _, berr := wf.retrieveBundle(ctx, op.DeployableBundleRef); berr != nil {
			return DeployResult{}, berr
		}
	}

	// Publish the rendered desired state (git commit; content-idempotent).
	revision := publishRevision(in.OperatedAppID, in.Change.ChangeID)
	if perr := wf.publishDesiredState(ctx, in.OperatedAppID, operatedruntime.RuntimeDesiredState{
		Bytes:       in.Change.RenderedDesiredState,
		ContentType: "application/desired-state",
	}); perr != nil {
		return DeployResult{}, perr
	}

	// Record the head-state desired-state transition (additive; Conflict loop).
	if _, rerr := wf.recordPublishDesiredState(ctx, in.OperatedAppID, op.Version, in.Change.Reason, nil); rerr != nil {
		return DeployResult{}, rerr
	}

	logger.Info("deploy published desired state", "operatedAppId", in.OperatedAppID.String(), "reason", desiredStateReasonName(in.Change.Reason))
	return DeployResult{Published: true, Revision: &revision}, nil
}

// ===========================================================================
// ReconcileWorkflow — op 2.2 (Schedule-triggered, 30s). Path B observe + Path C
// autoscale in ONE execution per firing.
// ===========================================================================

// reconcileInput is the start payload for ReconcileWorkflow (empty Scope ⇒ all).
type reconcileInput struct {
	Scope []operatedAppID
}

func (wf *workflows) ReconcileWorkflow(ctx workflow.Context, in reconcileInput) (ReconcileResult, error) {
	logger := workflow.GetLogger(ctx)

	apps, err := wf.readInFlightOperatedApps(ctx, operatedsystemstate.InFlightScope{AppIDs: in.Scope})
	if err != nil {
		return ReconcileResult{}, err
	}

	result := ReconcileResult{Observed: int64(len(apps))}
	for _, app := range apps {
		transitioned, republished, perr := wf.reconcileOne(ctx, app)
		if perr != nil {
			return ReconcileResult{}, perr
		}
		if transitioned {
			result.Transitions++
		}
		if republished {
			result.Republished++
		}
	}
	logger.Info("reconcile tick complete", "observed", result.Observed, "transitions", result.Transitions, "republished", result.Republished)
	return result, nil
}

// reconcileOne runs Path B (observe) + Path C (autoscale) for one in-flight app.
// Returns whether a head-state transition was recorded (Path B) and whether an
// autoscaler-driven republish happened (Path C, non-NoChange).
func (wf *workflows) reconcileOne(ctx workflow.Context, app operatedsystemstate.OperatedSystemSummary) (transitioned bool, republished bool, err error) {
	// --- Path B (observe) ---
	health, herr := wf.getApplicationHealth(ctx, app.ID)
	if herr != nil {
		return false, false, herr
	}
	slo, serr := wf.getSloStatus(ctx, app.ID)
	if serr != nil {
		return false, false, serr
	}
	attribution, aerr := wf.readComputeAttribution(ctx, app.ID)
	if aerr != nil {
		return false, false, aerr
	}

	version := app.Version

	// Record observed compute as a Usage Log event (append-only; dedup-id idempotent).
	if attribution.RuntimeEventID != "" {
		if uerr := wf.recordComputeUsage(ctx, app.ID, attribution); uerr != nil {
			return false, false, uerr
		}
	}

	// On a health transition vs last-known head-state, record the status change AND
	// run the intervention decision (DECIDE → EXECUTE).
	if health != app.Status {
		v, rerr := wf.recordRuntimeStatusChange(ctx, app.ID, version, health)
		if rerr != nil {
			return false, false, rerr
		}
		version = v
		transitioned = true

		directive, derr := wf.Intervention.DecideOnHealth(healthChange{
			AppID: app.ID,
			// interventionEngine's healthChange keeps the generated RuntimeStatusSeam
			// field type (Task 5 scope, untouched); bridge from the canonical
			// operatedsystemstate.RuntimeStatus via the surviving runtimeStatusFromState.
			FromStatus: runtimeStatusFromState(app.Status),
			ToStatus:   runtimeStatusFromState(health),
			SloMet:     slo.SloMet,
		}, wf.InterventionPolicy)
		if derr != nil {
			return false, false, fwmgr.MapError(derr)
		}
		switch directive {
		case healthDirectiveUnknown:
			// zero-value sentinel, also returned by the intervention adapter on any
			// engine error or unrecognized engine decision — same "unknown directive"
			// non-retryable rejection as any other unrecognized value.
			return false, false, temporal.NewNonRetryableApplicationError(
				"intervention returned an unknown health directive", "UnknownHealthDirective", nil)
		case healthDirectiveRetry:
			// EXECUTE Retry: re-publish prior desired state so the runtime self-heals /
			// re-converges (content-idempotent — a no-op if unchanged).
			if perr := wf.publishDesiredState(ctx, app.ID, operatedruntime.RuntimeDesiredState{ContentType: "application/desired-state"}); perr != nil {
				return false, false, perr
			}
		case healthDirectiveEscalate:
			// EXECUTE Escalate: surface to the operator (logged; the operator dashboard
			// reads head-state). No further mutation here.
			workflow.GetLogger(ctx).Warn("health escalated to operator", "operatedAppId", app.ID.String())
		default:
			return false, false, temporal.NewNonRetryableApplicationError(
				"intervention returned an unknown health directive", "UnknownHealthDirective", nil)
		}
	}

	// --- Path C (autoscale) ---
	decision, aerr2 := wf.Autoscaler.ProposeDesiredState(
		telemetry{CurrentReplicas: 0},
		autoscalerDesiredState{InfrastructureKind: wf.InfrastructureKind},
		wf.AutoscalerPolicy,
		wf.InfrastructureKind,
	)
	if aerr2 != nil {
		return transitioned, false, fwmgr.MapError(aerr2)
	}
	if decision.Action == AutoscaleNoChange {
		return transitioned, false, nil
	}

	// Non-NoChange ⇒ render revised manifests → publish → record (reason=autoscale).
	// Idle-pause (AutoscalePause) renders replicas=0 inside the opaque bytes.
	if perr := wf.publishDesiredState(ctx, app.ID, operatedruntime.RuntimeDesiredState{ContentType: "application/desired-state"}); perr != nil {
		return transitioned, false, perr
	}
	dec := decision
	if _, rerr := wf.recordPublishDesiredState(ctx, app.ID, version, ReasonAutoscale, &dec); rerr != nil {
		return transitioned, false, rerr
	}
	return transitioned, true, nil
}

// ===========================================================================
// WithdrawWorkflow — op 2.3 (ncuc3 withdraw).
// ===========================================================================

// withdrawInput is the start payload for WithdrawWorkflow.
type withdrawInput struct {
	OperatedAppID operatedAppID
	Reason        WithdrawReason
}

// WithdrawWorkflow drives ncuc3 (operationsManager.md §6.3):
//  1. WithdrawRuntimeActivity (operatedRuntimeAccess.withdraw; NotFound ⇒ success).
//  2. RecordFinalUsageActivity (usageAccess.recordFinalUsage).
//  3. WithdrawHeadStateActivity (operatedSystemStateAccess.withdrawSystem).
//
// Idempotent on the id; an already-withdrawn app collapses to a no-op success
// (NotFound on the runtime withdraw maps to success in the RA; a withdrawn head-state
// is recorded idempotently on its dedup key).
func (wf *workflows) WithdrawWorkflow(ctx workflow.Context, in withdrawInput) (WithdrawResult, error) {
	logger := workflow.GetLogger(ctx)

	op, err := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if err != nil {
		// A missing operated app is treated as an already-withdrawn no-op success
		// (the desired post-condition — "no running runtime" — already holds).
		if isReadNotFound(err) {
			return WithdrawResult{Withdrawn: true}, nil
		}
		return WithdrawResult{}, err
	}
	if op.Status == operatedsystemstate.RuntimeStatusWithdrawn {
		return WithdrawResult{Withdrawn: true}, nil
	}

	if werr := wf.withdrawRuntime(ctx, in.OperatedAppID); werr != nil {
		return WithdrawResult{}, werr
	}

	// Capture the final usage before the runtime is pruned. A best-effort final read of
	// compute attribution drives the recordFinalUsage append (dedup-id idempotent).
	attribution, aerr := wf.readComputeAttribution(ctx, in.OperatedAppID)
	if aerr != nil {
		return WithdrawResult{}, aerr
	}
	if attribution.RuntimeEventID != "" {
		if uerr := wf.recordFinalUsage(ctx, in.OperatedAppID, attribution); uerr != nil {
			return WithdrawResult{}, uerr
		}
	}

	if _, herr := wf.withdrawHeadState(ctx, in.OperatedAppID, op.Version); herr != nil {
		return WithdrawResult{}, herr
	}

	logger.Info("withdrawn", "operatedAppId", in.OperatedAppID.String())
	return WithdrawResult{Withdrawn: true}, nil
}

// ===========================================================================
// CostProjectionWorkflow — op 2.4 (ncuc6, short-lived read-only). NO mutation.
// ===========================================================================

// costProjectionInput is the start payload for CostProjectionWorkflow.
type costProjectionInput struct {
	OperatedAppID     operatedAppID
	ScaleWhatIfPoints []ScalePoint
}

// CostProjectionWorkflow drives ncuc6 (operationsManager.md §6.3):
//  1. ReadUsageRangeActivity (usageAccess.readRange) + ReadOperatedSystemActivity.
//  2. operationEstimationEngine.ProjectForOperatedApp (direct in-workflow). NO mutation.
func (wf *workflows) CostProjectionWorkflow(ctx workflow.Context, in costProjectionInput) (costProjection, error) {
	// Read recent desired-state history (head-state read) — establishes the app exists.
	if _, err := wf.readOperatedSystem(ctx, in.OperatedAppID); err != nil {
		return costProjection{}, err
	}

	appID := in.OperatedAppID
	events, uerr := wf.readUsageRange(ctx, usage.UsageRangeQuery{
		CustomerID:    wf.CustomerID,
		CycleID:       usage.CycleID(wf.CurrentCycleID),
		OperatedAppID: &appID,
	})
	if uerr != nil {
		return costProjection{}, uerr
	}

	projection, perr := wf.Estimation.ProjectForOperatedApp(
		observedUsage{Events: events},
		wf.InfrastructureKind,
		in.ScaleWhatIfPoints,
	)
	if perr != nil {
		return costProjection{}, fwmgr.MapError(perr)
	}
	return projection, nil
}

// ===========================================================================
// ViewWorkflow — op 2.7 (short-lived read-only operator view). NO mutation.
// Composes the EXISTING read Activities into one OperatedSystemView
// (operationsRead-ruling.md §A). No new Activities, no new RA verbs.
// ===========================================================================

// viewInput is the start payload for ViewWorkflow.
type viewInput struct {
	OperatedAppID operatedAppID
}

// ViewWorkflow drives the U-SPA-4 operator read view (operationsRead-ruling.md §A):
//  1. ReadOperatedSystemActivity  → head-state phase (RuntimePhase) + inFlight.
//  2. GetApplicationHealthActivity → observed health snapshot phase.
//  3. GetSloStatusActivity         → SLO posture (rolled into the health snapshot + one row).
//  4. ReadUsageRangeActivity + operationEstimationEngine.ProjectForOperatedApp (nil
//     what-if) → CurrentRunRate (run-rate only).
//
// The autoscaler mode is sourced from the committed policy snapshot the Manager
// carries (wf.AutoscalerPolicy.Mode). The autoscaler DECISION history and the
// per-phase RecentEvents are NOT exposed by an existing frozen RA read verb (head-state
// exposes Status/Version/InFlight only); per the ruling's Construction note they are
// surfaced empty here and a one-line follow-up is flagged to the architect — NO new RA
// verb is invented. ALL reads, NO write Activity, NO version bump.
func (wf *workflows) ViewWorkflow(ctx workflow.Context, in viewInput) (OperatedSystemView, error) {
	op, err := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if err != nil {
		return OperatedSystemView{}, err
	}

	health, herr := wf.getApplicationHealth(ctx, in.OperatedAppID)
	if herr != nil {
		return OperatedSystemView{}, herr
	}

	slo, serr := wf.getSloStatus(ctx, in.OperatedAppID)
	if serr != nil {
		return OperatedSystemView{}, serr
	}

	// Run-rate only (no what-if points) — same usage read the cost-projection path uses.
	appID := in.OperatedAppID
	events, uerr := wf.readUsageRange(ctx, usage.UsageRangeQuery{
		CustomerID:    wf.CustomerID,
		CycleID:       usage.CycleID(wf.CurrentCycleID),
		OperatedAppID: &appID,
	})
	if uerr != nil {
		return OperatedSystemView{}, uerr
	}
	projection, perr := wf.Estimation.ProjectForOperatedApp(
		observedUsage{Events: events},
		wf.InfrastructureKind,
		nil, // run-rate only
	)
	if perr != nil {
		return OperatedSystemView{}, fwmgr.MapError(perr)
	}

	// OperatedSystemView / HealthSnapshotView keep the generated RuntimeStatusSeam field
	// type (this package's own façade output contract); bridge from the canonical
	// operatedsystemstate.RuntimeStatus via the surviving runtimeStatusFromState.
	view := OperatedSystemView{
		OperatedAppID: in.OperatedAppID,
		Phase:         runtimeStatusFromState(op.Status),
		InFlight:      op.InFlight,
		Health: HealthSnapshotView{
			SloMet: slo.SloMet,
			Detail: slo.Detail,
			Phase:  runtimeStatusFromState(health),
		},
		// One SLO row from the observed SLO posture. The frozen operatedRuntimeAccess SLO
		// read collapses to one posture (getSloStatus); per-component rows beyond this are
		// behind a not-yet-exposed read and are surfaced as the single rollup row.
		Slos: []SloRowView{{
			Component: "app",
			Objective: slo.Detail,
			SloMet:    slo.SloMet,
			Healthy:   health == operatedsystemstate.RuntimeStatusHealthy,
		}},
		// RecentEvents: bounded, newest-first. The head-state status history is not a
		// single RA read today (Construction-note follow-up); surfaced empty.
		RecentEvents: nil,
		Autoscaler: AutoscalerView{
			Mode: wf.AutoscalerPolicy.Mode,
			// Decisions: not retrievable from a single frozen RA read today
			// (Construction-note follow-up); surfaced empty.
			Decisions: nil,
		},
		CurrentRunRate: projection.CurrentRunRate,
	}
	return view, nil
}

// ---------------------------------------------------------------------------
// Head-state read + recovering write helpers (§6.5).
// ---------------------------------------------------------------------------

// readOperatedSystem invokes operatedSystemStateAccess.readOperatedSystem. Task 4: the
// former Manager-local operatedSystem mirror is retired — the invoker's contract type
// IS the workflow's internal currency now, so no fold happens here.
func (wf *workflows) readOperatedSystem(ctx workflow.Context, operatedAppID operatedAppID) (operatedsystemstate.OperatedSystem, error) {
	return wf.Acts.OperatedSystemStateReadOperatedSystem(ctx, operatedAppID)
}

// readInFlightOperatedApps invokes operatedSystemStateAccess.readInFlightOperatedApps.
// Task 4: the former Manager-local operatedSystemSummary/inFlightScope mirrors are
// retired in favor of the invoker's contract types directly.
func (wf *workflows) readInFlightOperatedApps(ctx workflow.Context, scope operatedsystemstate.InFlightScope) ([]operatedsystemstate.OperatedSystemSummary, error) {
	return wf.Acts.OperatedSystemStateReadInFlightOperatedApps(ctx, scope)
}

// retrieveBundle invokes artifactAccess.retrieveConstructionOutput (escalation E-1:
// the deployable bundle IS a construction output until the frozen
// retrieveDeployableBundle verb lands).
func (wf *workflows) retrieveBundle(ctx workflow.Context, ref string) (deployableBundle, error) {
	out, err := wf.Acts.ArtifactRetrieveConstructionOutput(ctx, ref)
	if err != nil {
		return deployableBundle{}, err
	}
	return deployableBundle{Output: out}, nil
}

// publishDesiredState invokes operatedRuntimeAccess.publishDesiredState (git commit;
// content-idempotent). Task 4: the former Manager-local runtimeDesiredState mirror is
// retired — desired IS the contract type now.
func (wf *workflows) publishDesiredState(ctx workflow.Context, appID operatedAppID, desired operatedruntime.RuntimeDesiredState) error {
	return wf.Acts.OperatedRuntimePublishDesiredState(ctx, appID, desired)
}

// withdrawRuntime invokes operatedRuntimeAccess.withdraw (NotFound ⇒ success in the RA).
func (wf *workflows) withdrawRuntime(ctx workflow.Context, appID operatedAppID) error {
	return wf.Acts.OperatedRuntimeWithdraw(ctx, appID)
}

// getApplicationHealth invokes operatedRuntimeAccess.getApplicationHealth (pure read),
// canonicalizing the observed operatedruntime.RuntimeStatus into the
// operatedsystemstate.RuntimeStatus vocabulary via the surviving DIVERGENT converter
// (runtimeStatusFromRuntime — two RAs' independently generated enums).
func (wf *workflows) getApplicationHealth(ctx workflow.Context, appID operatedAppID) (operatedsystemstate.RuntimeStatus, error) {
	s, err := wf.Acts.OperatedRuntimeGetApplicationHealth(ctx, appID)
	if err != nil {
		return operatedsystemstate.RuntimeStatusUnknown, err
	}
	return runtimeStatusFromRuntime(s), nil
}

// getSloStatus invokes operatedRuntimeAccess.getSloStatus (pure read). Task 4: the
// former Manager-local sloStatusSeam mirror is retired.
func (wf *workflows) getSloStatus(ctx workflow.Context, appID operatedAppID) (operatedruntime.SloStatus, error) {
	return wf.Acts.OperatedRuntimeGetSloStatus(ctx, appID)
}

// readComputeAttribution invokes operatedRuntimeAccess.readComputeAttribution (pure
// read). The Manager pins the window to a default (open) window here; the RA attributes
// since last observation. Task 4: the former Manager-local computeAttribution mirror is
// retired.
func (wf *workflows) readComputeAttribution(ctx workflow.Context, appID operatedAppID) (operatedruntime.ComputeAttribution, error) {
	return wf.Acts.OperatedRuntimeReadComputeAttribution(ctx, appID, operatedruntime.AttributionWindow{})
}

// recordComputeUsage invokes usageAccess.recordComputeUsage (append; dedup-id
// idempotent). The single-event slice-wrap lives here now (was in the retired activity).
func (wf *workflows) recordComputeUsage(ctx workflow.Context, appID operatedAppID, attribution operatedruntime.ComputeAttribution) error {
	_, err := wf.Acts.UsageRecordComputeUsage(ctx, []usage.UsageEvent{wf.usageEvent(ctx, appID, attribution)})
	return err
}

// recordFinalUsage invokes usageAccess.recordFinalUsage (append; dedup-id idempotent).
func (wf *workflows) recordFinalUsage(ctx workflow.Context, appID operatedAppID, attribution operatedruntime.ComputeAttribution) error {
	_, err := wf.Acts.UsageRecordFinalUsage(ctx, []usage.UsageEvent{wf.usageEvent(ctx, appID, attribution)})
	return err
}

// readUsageRange invokes usageAccess.readRange (pure read) and folds the contract
// events into the Manager-local seam the estimation Engine consumes. Task 4: the former
// Manager-local usageRangeQuerySeam mirror is retired (query IS the contract type now);
// usageEventSeam stays — it is operationEstimationEngine's input shape (Task 5 scope).
func (wf *workflows) readUsageRange(ctx workflow.Context, query usage.UsageRangeQuery) ([]usageEventSeam, error) {
	events, err := wf.Acts.UsageReadRange(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]usageEventSeam, 0, len(events))
	for _, e := range events {
		out = append(out, usageEventSeam{
			OperatedAppID:  e.OperatedAppID,
			CustomerID:     e.CustomerID,
			CycleID:        string(e.CycleID),
			Units:          computeUnitsSeam{Amount: e.Units.Amount, Unit: e.Units.Unit},
			RuntimeEventID: string(e.RuntimeEventID),
			ObservedAt:     e.OccurredAt,
		})
	}
	return out, nil
}

// usageEvent assembles one contract UsageEvent from an observed attribution. The
// RuntimeEventID is the append-only ledger's dedup token (usageAccess.md §2/§3).
// OccurredAt is read from the deterministic workflow clock (replay-safe).
func (wf *workflows) usageEvent(ctx workflow.Context, appID operatedAppID, attribution operatedruntime.ComputeAttribution) usage.UsageEvent {
	return usage.UsageEvent{
		OperatedAppID:  appID,
		CustomerID:     wf.CustomerID,
		CycleID:        usage.CycleID(wf.CurrentCycleID),
		Units:          usage.ComputeUnits{Amount: attribution.Units.Amount, Unit: attribution.Units.Unit},
		RuntimeEventID: usage.RuntimeEventID(attribution.RuntimeEventID),
		OccurredAt:     workflow.Now(ctx),
	}
}

// recordPublishDesiredState applies the head-state desired-state transition with the
// Conflict loop (§6.5). decision is carried only for reason=autoscale. Task 4: seed/
// return now speak operatedsystemstate.Version directly (the former Manager-local
// version mirror is retired); decision stays *autoscaleDecisionSeam (autoscalerEngine's
// output shape, Task 5 scope) — autoscaleDecisionToState is left untouched (engine-side).
func (wf *workflows) recordPublishDesiredState(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version, reason DesiredStateReason, decision *autoscaleDecisionSeam) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStatePublishDesiredState(ctx, appID,
			expected,
			desiredStateReasonToState(reason),
			autoscaleDecisionToState(decision))
	})
}

// recordRuntimeStatusChange applies the observed-status head-state transition. Task 4:
// status is now operatedsystemstate.RuntimeStatus directly (runtimeStatusToState, the
// former reverse converter, has no remaining caller and is retired).
func (wf *workflows) recordRuntimeStatusChange(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version, status operatedsystemstate.RuntimeStatus) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStateRecordRuntimeStatusChange(ctx, appID, expected, status)
	})
}

// withdrawHeadState applies the withdraw head-state transition.
func (wf *workflows) withdrawHeadState(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStateWithdrawSystem(ctx, appID, expected)
	})
}

// recordDelinquencyAction applies the delinquency-action head-state transition. Task 4:
// action is now operatedsystemstate.DelinquencyAction directly (delinquencyActionToState,
// the former identity converter, has no remaining caller and is retired).
func (wf *workflows) recordDelinquencyAction(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version, action operatedsystemstate.DelinquencyAction) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStateRecordDelinquencyAction(ctx, appID, expected, action)
	})
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to construction). On a
// stale-version fwra.Conflict it re-reads the true head Version and re-applies with
// the SAME idempotency key (dedup-first ordering preserves idempotent replay).
func (wf *workflows) applyRecovering(
	ctx workflow.Context,
	appID operatedAppID,
	seed operatedsystemstate.Version,
	apply func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error),
) (operatedsystemstate.Version, error) {
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		op, rerr := wf.readOperatedSystem(ctx, appID)
		if rerr != nil {
			return 0, rerr
		}
		expected = op.Version
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
}

// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

// isReadNotFound reports whether err is a head-state read's NotFound.
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// publishRevision derives a deterministic published-revision token for UI correlation
// (opaque; not a Temporal id).
func publishRevision(appID operatedAppID, changeID string) string {
	return appID.String() + ":" + changeID
}
