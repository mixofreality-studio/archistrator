package operations

import (
	"errors"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
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

// Deps bundles every downstream dependency the operationsManager orchestrates,
// passed to RegisterWorker (worker.go) and held on the Workflows struct. Each field
// is a CONSUMER-DEFINED interface (deps.go): the existing concrete RA types are
// adapted at the composition root; the not-yet-built Engines/RA are unit-tested with
// fakes.
type Deps struct {
	Intervention interventionEngine
	Autoscaler   autoscalerEngine
	Estimation   operationEstimationEngine

	OperatedSystemState operatedSystemStateAccess
	OperatedRuntime     operatedRuntimeAccess
	Usage               usageAccess
	Artifacts           artifactAccess

	// Policy snapshots fed to the Engines by value. In production the Manager reads
	// them from head-state; held here as the construction-time seam values.
	InterventionPolicy InterventionPolicy
	AutoscalerPolicy   AutoscalerPolicy
	InfrastructureKind InfrastructureKind

	// CurrentCycleID is the billing cycle the Manager attributes observed usage to
	// (carried onto the usage events). Held here as the construction-time seam value;
	// in production the Manager derives it from the operated app's billing context.
	CurrentCycleID string
	CustomerID     CustomerID
}

// Workflows is the single operationsManager component struct — BOTH the workflow
// receiver and the activity receiver (no separate Activities type, mirroring
// construction).
type Workflows struct {
	Intervention interventionEngine
	Autoscaler   autoscalerEngine
	Estimation   operationEstimationEngine

	OperatedSystemState operatedSystemStateAccess
	OperatedRuntime     operatedRuntimeAccess
	Usage               usageAccess
	Artifacts           artifactAccess

	InterventionPolicy InterventionPolicy
	AutoscalerPolicy   AutoscalerPolicy
	InfrastructureKind InfrastructureKind
	CurrentCycleID     string
	CustomerID         CustomerID
}

// newWorkflows builds the Workflows receiver from the injected Deps.
func newWorkflows(d Deps) *Workflows {
	return &Workflows{
		Intervention:        d.Intervention,
		Autoscaler:          d.Autoscaler,
		Estimation:          d.Estimation,
		OperatedSystemState: d.OperatedSystemState,
		OperatedRuntime:     d.OperatedRuntime,
		Usage:               d.Usage,
		Artifacts:           d.Artifacts,
		InterventionPolicy:  d.InterventionPolicy,
		AutoscalerPolicy:    d.AutoscalerPolicy,
		InfrastructureKind:  d.InfrastructureKind,
		CurrentCycleID:      d.CurrentCycleID,
		CustomerID:          d.CustomerID,
	}
}

// Bounds (in-workflow guards; NOT contract surface).
const (
	// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
	// loop (§6.5).
	maxMutateConflictAttempts = 20
)

// ---------------------------------------------------------------------------
// Activity option presets (operationsManager.md §6.4). Concrete RetryPolicy /
// timeout choices live here, in the Manager. FU-MOP-1 (named RetryPolicy library) is
// not yet landed; the inline §6.4 parameters are used (C-MOP-4).
// ---------------------------------------------------------------------------

// readHeadOpts — pure head-state reads (default policy; terminal NotFound).
func readHeadOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// recordHeadOpts — head-state write transitions (terminal NotFound; Conflict is
// surfaced for the workflow-level re-read loop, so it is NOT non-retryable here — the
// workflow body recovers it rather than the RetryPolicy).
func recordHeadOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
				fwmgr.RAErrType(fwra.Conflict),
			},
		},
	})
}

// publishOpts — operatedRuntimeAccess writes (git commit + push; externalGateway-
// style; terminal Auth/ContractMisuse). Git-content-idempotent — no version guard.
func publishOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.Auth),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// runtimeReadOpts — operatedRuntimeAccess pure reads (~30s; terminal Auth/NotFound).
func runtimeReadOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.Auth),
				fwmgr.RAErrType(fwra.NotFound),
			},
		},
	})
}

// artifactReadOpts — artifactAccess.retrieveDeployableBundle (~30s; terminal
// NotFound/Auth).
func artifactReadOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.Auth),
			},
		},
	})
}

// usageOpts — usageAccess appends + reads (~20s; terminal ContractMisuse/NotFound).
// Append-only ledger: NO Conflict (dedup-id idempotent).
func usageOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 20 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.ContractMisuse),
				fwmgr.RAErrType(fwra.NotFound),
			},
		},
	})
}

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

// DeployInput is the start payload for DeployWorkflow.
type DeployInput struct {
	OperatedAppID OperatedAppID
	Change        DesiredStateChange
}

// DeployWorkflow drives UC4 deploy (operationsManager.md §6.3):
//  1. ReadOperatedSystemActivity → head-state (desiredState, deployableBundleRef).
//  2. (first deploy, full bundle) RetrieveDeployableBundleActivity.
//  3. PublishDesiredStateActivity (the git commit).
//  4. RecordPublishDesiredStateActivity (head-state transition, reason=operator|deploy).
func (wf *Workflows) DeployWorkflow(ctx workflow.Context, in DeployInput) (DeployResult, error) {
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
	if perr := wf.publishDesiredState(ctx, in.OperatedAppID, RuntimeDesiredState{
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

// ReconcileInput is the start payload for ReconcileWorkflow (empty Scope ⇒ all).
type ReconcileInput struct {
	Scope []OperatedAppID
}

func (wf *Workflows) ReconcileWorkflow(ctx workflow.Context, in ReconcileInput) (ReconcileResult, error) {
	logger := workflow.GetLogger(ctx)

	apps, err := wf.readInFlightOperatedApps(ctx, InFlightScope{AppIDs: in.Scope})
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
func (wf *Workflows) reconcileOne(ctx workflow.Context, app OperatedSystemSummary) (transitioned bool, republished bool, err error) {
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

		directive, derr := wf.Intervention.DecideOnHealth(HealthChange{
			AppID:      app.ID,
			FromStatus: app.Status,
			ToStatus:   health,
			SloMet:     slo.SloMet,
		}, wf.InterventionPolicy)
		if derr != nil {
			return false, false, fwmgr.MapError(derr)
		}
		switch directive {
		case HealthDirectiveRetry:
			// EXECUTE Retry: re-publish prior desired state so the runtime self-heals /
			// re-converges (content-idempotent — a no-op if unchanged).
			if perr := wf.publishDesiredState(ctx, app.ID, RuntimeDesiredState{ContentType: "application/desired-state"}); perr != nil {
				return false, false, perr
			}
		case HealthDirectiveEscalate:
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
		Telemetry{CurrentReplicas: 0},
		AutoscalerDesiredState{InfrastructureKind: wf.InfrastructureKind},
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
	if perr := wf.publishDesiredState(ctx, app.ID, RuntimeDesiredState{ContentType: "application/desired-state"}); perr != nil {
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

// WithdrawInput is the start payload for WithdrawWorkflow.
type WithdrawInput struct {
	OperatedAppID OperatedAppID
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
func (wf *Workflows) WithdrawWorkflow(ctx workflow.Context, in WithdrawInput) (WithdrawResult, error) {
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
	if op.Status == RuntimeStatusWithdrawn {
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

// CostProjectionInput is the start payload for CostProjectionWorkflow.
type CostProjectionInput struct {
	OperatedAppID     OperatedAppID
	ScaleWhatIfPoints []ScalePoint
}

// CostProjectionWorkflow drives ncuc6 (operationsManager.md §6.3):
//  1. ReadUsageRangeActivity (usageAccess.readRange) + ReadOperatedSystemActivity.
//  2. operationEstimationEngine.ProjectForOperatedApp (direct in-workflow). NO mutation.
func (wf *Workflows) CostProjectionWorkflow(ctx workflow.Context, in CostProjectionInput) (CostProjection, error) {
	// Read recent desired-state history (head-state read) — establishes the app exists.
	if _, err := wf.readOperatedSystem(ctx, in.OperatedAppID); err != nil {
		return CostProjection{}, err
	}

	appID := in.OperatedAppID
	events, uerr := wf.readUsageRange(ctx, UsageRangeQuerySeam{
		CustomerID:    wf.CustomerID,
		CycleID:       wf.CurrentCycleID,
		OperatedAppID: &appID,
	})
	if uerr != nil {
		return CostProjection{}, uerr
	}

	projection, perr := wf.Estimation.ProjectForOperatedApp(
		ObservedUsage{Events: events},
		wf.InfrastructureKind,
		in.ScaleWhatIfPoints,
	)
	if perr != nil {
		return CostProjection{}, fwmgr.MapError(perr)
	}
	return projection, nil
}

// ===========================================================================
// ViewWorkflow — op 2.7 (short-lived read-only operator view). NO mutation.
// Composes the EXISTING read Activities into one OperatedSystemView
// (operationsRead-ruling.md §A). No new Activities, no new RA verbs.
// ===========================================================================

// ViewInput is the start payload for ViewWorkflow.
type ViewInput struct {
	OperatedAppID OperatedAppID
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
func (wf *Workflows) ViewWorkflow(ctx workflow.Context, in ViewInput) (OperatedSystemView, error) {
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
	events, uerr := wf.readUsageRange(ctx, UsageRangeQuerySeam{
		CustomerID:    wf.CustomerID,
		CycleID:       wf.CurrentCycleID,
		OperatedAppID: &appID,
	})
	if uerr != nil {
		return OperatedSystemView{}, uerr
	}
	projection, perr := wf.Estimation.ProjectForOperatedApp(
		ObservedUsage{Events: events},
		wf.InfrastructureKind,
		nil, // run-rate only
	)
	if perr != nil {
		return OperatedSystemView{}, fwmgr.MapError(perr)
	}

	view := OperatedSystemView{
		OperatedAppID: in.OperatedAppID,
		Phase:         op.Status,
		InFlight:      op.InFlight,
		Health: HealthSnapshotView{
			SloMet: slo.SloMet,
			Detail: slo.Detail,
			Phase:  health,
		},
		// One SLO row from the observed SLO posture. The frozen operatedRuntimeAccess SLO
		// read collapses to one posture (getSloStatus); per-component rows beyond this are
		// behind a not-yet-exposed read and are surfaced as the single rollup row.
		Slos: []SloRowView{{
			Component: "app",
			Objective: slo.Detail,
			SloMet:    slo.SloMet,
			Healthy:   health == RuntimeStatusHealthy,
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

// readOperatedSystem runs the ReadOperatedSystemActivity.
func (wf *Workflows) readOperatedSystem(ctx workflow.Context, operatedAppID OperatedAppID) (OperatedSystem, error) {
	c := readHeadOpts(ctx)
	var op OperatedSystem
	if err := workflow.ExecuteActivity(c, wf.ReadOperatedSystemActivity, operatedAppID).Get(ctx, &op); err != nil {
		return OperatedSystem{}, err
	}
	return op, nil
}

// readInFlightOperatedApps runs the ReadInFlightOperatedAppsActivity.
func (wf *Workflows) readInFlightOperatedApps(ctx workflow.Context, scope InFlightScope) ([]OperatedSystemSummary, error) {
	c := readHeadOpts(ctx)
	var apps []OperatedSystemSummary
	if err := workflow.ExecuteActivity(c, wf.ReadInFlightOperatedAppsActivity, scope).Get(ctx, &apps); err != nil {
		return nil, err
	}
	return apps, nil
}

// retrieveBundle runs the RetrieveDeployableBundleActivity.
func (wf *Workflows) retrieveBundle(ctx workflow.Context, ref string) (DeployableBundle, error) {
	c := artifactReadOpts(ctx)
	var b DeployableBundle
	err := workflow.ExecuteActivity(c, wf.RetrieveDeployableBundleActivity, ref).Get(ctx, &b)
	return b, err
}

// publishDesiredState runs the PublishDesiredStateActivity (git commit; content-idempotent).
func (wf *Workflows) publishDesiredState(ctx workflow.Context, appID OperatedAppID, desired RuntimeDesiredState) error {
	c := publishOpts(ctx)
	return workflow.ExecuteActivity(c, wf.PublishDesiredStateActivity, PublishDesiredStateArgs{
		AppID: appID, Desired: desired,
	}).Get(ctx, nil)
}

// withdrawRuntime runs the WithdrawRuntimeActivity (NotFound ⇒ success in the RA).
func (wf *Workflows) withdrawRuntime(ctx workflow.Context, appID OperatedAppID) error {
	c := publishOpts(ctx)
	return workflow.ExecuteActivity(c, wf.WithdrawRuntimeActivity, appID).Get(ctx, nil)
}

// getApplicationHealth runs the GetApplicationHealthActivity (pure read).
func (wf *Workflows) getApplicationHealth(ctx workflow.Context, appID OperatedAppID) (RuntimeStatusSeam, error) {
	c := runtimeReadOpts(ctx)
	var s RuntimeStatusSeam
	err := workflow.ExecuteActivity(c, wf.GetApplicationHealthActivity, appID).Get(ctx, &s)
	return s, err
}

// getSloStatus runs the GetSloStatusActivity (pure read).
func (wf *Workflows) getSloStatus(ctx workflow.Context, appID OperatedAppID) (SloStatusSeam, error) {
	c := runtimeReadOpts(ctx)
	var s SloStatusSeam
	err := workflow.ExecuteActivity(c, wf.GetSloStatusActivity, appID).Get(ctx, &s)
	return s, err
}

// readComputeAttribution runs the ReadComputeAttributionActivity (pure read).
func (wf *Workflows) readComputeAttribution(ctx workflow.Context, appID OperatedAppID) (ComputeAttribution, error) {
	c := runtimeReadOpts(ctx)
	var a ComputeAttribution
	err := workflow.ExecuteActivity(c, wf.ReadComputeAttributionActivity, appID).Get(ctx, &a)
	return a, err
}

// recordComputeUsage runs the RecordComputeUsageActivity (append; dedup-id idempotent).
func (wf *Workflows) recordComputeUsage(ctx workflow.Context, appID OperatedAppID, attribution ComputeAttribution) error {
	c := usageOpts(ctx)
	return workflow.ExecuteActivity(c, wf.RecordComputeUsageActivity, wf.usageEvent(ctx, appID, attribution)).Get(ctx, nil)
}

// recordFinalUsage runs the RecordFinalUsageActivity (append; dedup-id idempotent).
func (wf *Workflows) recordFinalUsage(ctx workflow.Context, appID OperatedAppID, attribution ComputeAttribution) error {
	c := usageOpts(ctx)
	return workflow.ExecuteActivity(c, wf.RecordFinalUsageActivity, wf.usageEvent(ctx, appID, attribution)).Get(ctx, nil)
}

// readUsageRange runs the ReadUsageRangeActivity (pure read).
func (wf *Workflows) readUsageRange(ctx workflow.Context, query UsageRangeQuerySeam) ([]UsageEventSeam, error) {
	c := usageOpts(ctx)
	var events []UsageEventSeam
	err := workflow.ExecuteActivity(c, wf.ReadUsageRangeActivity, query).Get(ctx, &events)
	return events, err
}

// usageEvent assembles one UsageEvent from an observed attribution. The
// RuntimeEventID is the append-only ledger's dedup token (usageAccess.md §2/§3).
// ObservedAt is read from the deterministic workflow clock (replay-safe).
func (wf *Workflows) usageEvent(ctx workflow.Context, appID OperatedAppID, attribution ComputeAttribution) UsageEventSeam {
	return UsageEventSeam{
		OperatedAppID:  appID,
		CustomerID:     wf.CustomerID,
		CycleID:        wf.CurrentCycleID,
		Units:          attribution.Units,
		RuntimeEventID: attribution.RuntimeEventID,
		ObservedAt:     workflow.Now(ctx),
	}
}

// recordPublishDesiredState applies the head-state desired-state transition with the
// Conflict loop (§6.5). decision is carried only for reason=autoscale.
func (wf *Workflows) recordPublishDesiredState(ctx workflow.Context, appID OperatedAppID, seed Version, reason DesiredStateReason, decision *AutoscaleDecisionSeam) (Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected Version) (Version, error) {
		c := recordHeadOpts(ctx)
		var v Version
		e := workflow.ExecuteActivity(c, wf.RecordPublishDesiredStateActivity, RecordPublishDesiredStateArgs{
			AppID: appID, ExpectedVersion: expected, Reason: reason, Decision: decision,
		}).Get(ctx, &v)
		return v, e
	})
}

// recordRuntimeStatusChange applies the observed-status head-state transition.
func (wf *Workflows) recordRuntimeStatusChange(ctx workflow.Context, appID OperatedAppID, seed Version, status RuntimeStatusSeam) (Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected Version) (Version, error) {
		c := recordHeadOpts(ctx)
		var v Version
		e := workflow.ExecuteActivity(c, wf.RecordRuntimeStatusChangeActivity, RecordRuntimeStatusChangeArgs{
			AppID: appID, ExpectedVersion: expected, Status: status,
		}).Get(ctx, &v)
		return v, e
	})
}

// withdrawHeadState applies the withdraw head-state transition.
func (wf *Workflows) withdrawHeadState(ctx workflow.Context, appID OperatedAppID, seed Version) (Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected Version) (Version, error) {
		c := recordHeadOpts(ctx)
		var v Version
		e := workflow.ExecuteActivity(c, wf.WithdrawHeadStateActivity, WithdrawHeadStateArgs{
			AppID: appID, ExpectedVersion: expected,
		}).Get(ctx, &v)
		return v, e
	})
}

// recordDelinquencyAction applies the delinquency-action head-state transition.
func (wf *Workflows) recordDelinquencyAction(ctx workflow.Context, appID OperatedAppID, seed Version, action DelinquencyAction) (Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected Version) (Version, error) {
		c := recordHeadOpts(ctx)
		var v Version
		e := workflow.ExecuteActivity(c, wf.RecordDelinquencyActionActivity, RecordDelinquencyActionArgs{
			AppID: appID, ExpectedVersion: expected, Action: action,
		}).Get(ctx, &v)
		return v, e
	})
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to construction). On a
// stale-version fwra.Conflict it re-reads the true head Version and re-applies with
// the SAME idempotency key (dedup-first ordering preserves idempotent replay).
func (wf *Workflows) applyRecovering(
	ctx workflow.Context,
	appID OperatedAppID,
	seed Version,
	apply func(expected Version) (Version, error),
) (Version, error) {
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
func publishRevision(appID OperatedAppID, changeID string) string {
	return appID.String() + ":" + changeID
}
