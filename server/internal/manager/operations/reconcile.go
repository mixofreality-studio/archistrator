package operations

import (
	"context"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

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
	workload, werr := wf.readWorkloadTelemetry(ctx, app.ID)
	if werr != nil {
		return false, false, werr
	}

	version := app.Version

	// Record observed compute as a Usage Log event (append-only; dedup-id idempotent),
	// attributed to the app's OWN customer: the in-flight summary carries no customer, so
	// the head-state row is read for it — only when there is compute to attribute.
	if attribution.RuntimeEventID != "" {
		op, oerr := wf.readOperatedSystem(ctx, app.ID)
		if oerr != nil {
			return false, false, oerr
		}
		if uerr := wf.recordComputeUsage(ctx, app.ID, op.CustomerID, attribution); uerr != nil {
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

		directive, derr := wf.Intervention.DecideOnHealth(fweng.Context{Context: context.Background()},
			intervention.HealthChange{
				OperatedAppID: intervention.OperatedAppID(app.ID.String()),
				// interventionEngine is reached through its published contract now;
				// healthStatusFromRuntimeStatus bridges the canonical
				// operatedsystemstate.RuntimeStatus straight onto intervention.HealthStatus
				// (collapsing the former two-hop path through this package's own
				// RuntimeStatusSeam — Task 5).
				FromHealth: healthStatusFromRuntimeStatus(app.Status),
				ToHealth:   healthStatusFromRuntimeStatus(health),
				SLOStatus:  sloStatusFromMet(slo.SloMet),
				Policy:     wf.InterventionPolicy,
			})
		if derr != nil {
			return false, false, fwmgr.MapError(derr)
		}
		switch directive {
		case intervention.HealthRetry:
			// EXECUTE Retry: INTENDED to re-publish prior desired state so the runtime
			// self-heals / re-converges. Deliberately does NOT call the runtime publish
			// (review finding 2): this Manager keeps no cached prior RuntimeDesiredState
			// to re-derive here, and — unlike the old opaque-bytes contract, where a
			// zero-value payload was inert — an empty RuntimeDesiredState is now exactly
			// the half-rendered deployment (empty AppName/Namespace/Image, Replicas:0)
			// the fail-loud constraint on assembleDesiredState exists to prevent. Sending
			// nothing is the honest choice over sending something wrong. A real self-heal
			// republish needs a "last published desired state" concept this task does not
			// add; earmarked as a follow-up. Observability-only for now, same posture as
			// HealthEscalate below.
			workflow.GetLogger(ctx).Info("health retry directive: no cached desired state to republish (follow-up)", "operatedAppId", app.ID.String())
		case intervention.HealthEscalate:
			// EXECUTE Escalate: surface to the operator (logged; the operator dashboard
			// reads head-state). No further mutation here.
			workflow.GetLogger(ctx).Warn("health escalated to operator", "operatedAppId", app.ID.String())
		default:
			// intervention.HealthDirective has no Unknown sentinel (HealthRetry is its
			// zero value) — any value outside {HealthRetry, HealthEscalate} is an
			// unrecognized engine decision, same non-retryable rejection as before.
			return false, false, temporal.NewNonRetryableApplicationError(
				"intervention returned an unknown health directive", "UnknownHealthDirective", nil)
		}
	}

	// --- Path C (autoscale) ---
	republished, err = wf.autoscaleOne(ctx, app, version, observation{health: health, sloMet: slo.SloMet, workload: workload})
	if err != nil {
		return transitioned, false, err
	}
	return transitioned, republished, nil
}

// observation is what one tick observed of an app: its health, whether its SLO holds,
// and its workload telemetry — the autoscaler's inputs beside the head-state scale.
type observation struct {
	health   operatedsystemstate.RuntimeStatus
	sloMet   bool
	workload operatedruntime.WorkloadTelemetry
}

// autoscaleOne runs Path C (autoscale) for one in-flight app: the autoscalerEngine weighs
// the tick's observation against the head-state desired scale (the replicas last
// published and the scaling policy they were published under) and, on a decision that
// changes the replicas, the app's desired state is re-rendered at the new replica count
// (render-manifests-c: the same bundle and deployment model a deploy renders from),
// published to the runtime and recorded with reason=autoscale. version is the head-state
// version as advanced by Path B's status transition (if any). An app whose head state
// records no scale (never deployed with one) is never autoscaled.
func (wf *workflows) autoscaleOne(ctx workflow.Context, app operatedsystemstate.OperatedSystemSummary, version operatedsystemstate.Version, obs observation) (republished bool, err error) {
	if app.Scale.MaxReplicas == 0 {
		return false, nil
	}
	decision, derr := wf.Autoscaler.ProposeDesiredState(
		fweng.Context{Context: context.Background()},
		autoscalerTelemetry(app, obs, workflow.Now(ctx)),
		autoscaler.DesiredState{
			OperatedAppID:      app.ID,
			InfrastructureKind: wf.InfrastructureKind,
			Replicas:           app.Scale.Replicas,
			LastDecisionAt:     app.Scale.ScaledAt,
		},
		autoscalerPolicyFor(wf.AutoscalerPolicy, app.Scale, obs.workload.Measured),
		wf.InfrastructureKind,
	)
	if derr != nil {
		return false, fwmgr.MapError(derr)
	}
	replicas := scaledReplicas(app.Scale, decision)
	if decision.Kind == autoscaler.DecisionNoChange || replicas == app.Scale.Replicas {
		return false, nil
	}

	// render-manifests-c: the revision basis is the app's own head state, bundle and
	// deployment model, re-rendered with the decision's replica count.
	op, err := wf.readOperatedSystem(ctx, app.ID)
	if err != nil {
		return false, err
	}
	bundle, err := wf.retrieveBundle(ctx, op.DeployableBundleRef)
	if err != nil {
		return false, err
	}
	proj, err := wf.readProject(ctx, op.ProjectRef)
	if err != nil {
		return false, err
	}
	desired, err := assembleDesiredState(proj, bundle, op)
	if err != nil {
		return false, temporal.NewNonRetryableApplicationError(
			"failed to assemble the desired state from the project's deployment model",
			fwmgr.ErrType(fwmgr.FailedPrecondition), err)
	}
	desired.Server.Replicas = replicas
	if err := wf.publishDesiredState(ctx, app.ID, desired); err != nil {
		return false, err
	}
	scale := app.Scale
	scale.Replicas = replicas
	scale.ScaledAt = workflow.Now(ctx)
	if _, err := wf.recordPublishDesiredState(ctx, app.ID, version, ReasonAutoscale, scale); err != nil {
		return false, err
	}
	return true, nil
}

// readWorkloadTelemetry invokes operatedRuntimeAccess.readWorkloadTelemetry (pure read):
// the workload's measured CPU utilization, the autoscaler's utilization signal.
func (wf *workflows) readWorkloadTelemetry(ctx workflow.Context, appID operatedAppID) (operatedruntime.WorkloadTelemetry, error) {
	return wf.Acts.OperatedRuntimeReadWorkloadTelemetry(ctx, appID)
}

// autoscalerTelemetry is the autoscalerEngine's telemetry snapshot for one tick: the
// workload's CPU utilization, the observed health, and the SLO posture (an SLO that does
// not hold is budget being burned), observed at the workflow clock.
func autoscalerTelemetry(app operatedsystemstate.OperatedSystemSummary, obs observation, at time.Time) autoscaler.Telemetry {
	slo := autoscaler.SLOWithinBudget
	if !obs.sloMet {
		slo = autoscaler.SLOBurningBudget
	}
	return autoscaler.Telemetry{
		OperatedAppID:   app.ID,
		ObservedAt:      at,
		CurrentReplicas: app.Scale.Replicas,
		CPUUtilization:  obs.workload.CPUUtilization,
		Health:          autoscalerHealth(obs.health),
		SLOStatus:       slo,
	}
}

// autoscalerPolicyFor is the autoscalerEngine's policy for an app, from the scaling
// policy its head state recorded at deploy. The floor is never below one replica: a
// scale to zero is an idle pause, which needs a traffic signal no runtime read reports,
// so idle-pause stays disabled (zero IdleThreshold). The CPU rules weigh the workload's
// utilization against the policy's target — up at the target, down below half of it
// once the last scale has stood for scaleDownGrace — and only when the runtime measured
// the workload: an unmeasured workload is never read as idle.
func autoscalerPolicyFor(configured autoscalerPolicy, scale operatedsystemstate.DesiredScale, measured bool) autoscaler.AutoscalerPolicy {
	p := autoscalerPolicyToEngine(configured)
	floor := max(scale.MinReplicas, 1)
	p.MinReplicas = floor
	p.MaxReplicas = scale.MaxReplicas
	p.BaselineReplicas = floor
	p.MaxStepDelta = 1
	if measured && scale.TargetUtilizationPct > 0 {
		p.ScaleUpCPU = float64(scale.TargetUtilizationPct) / 100
		p.ScaleDownCPU = p.ScaleUpCPU / 2
		p.ScaleDownGrace = scaleDownGrace
	}
	return p
}

// scaledReplicas is the replica count a decision leaves the server at, within the
// recorded policy's bounds and never below one.
func scaledReplicas(scale operatedsystemstate.DesiredScale, d autoscaler.Decision) int64 {
	n := scale.Replicas
	switch d.Kind {
	case autoscaler.DecisionScaleUp:
		n += d.Delta
	case autoscaler.DecisionScaleDown:
		n -= d.Delta
	case autoscaler.DecisionResume:
		n = d.ToBaseline
	case autoscaler.DecisionPause, autoscaler.DecisionNoChange:
		return scale.Replicas
	}
	return min(max(n, scale.MinReplicas, 1), scale.MaxReplicas)
}

// autoscalerHealth bridges the observed head-state status onto the autoscalerEngine's
// own HealthStatus enum (two generated enums; an explicit switch).
func autoscalerHealth(s operatedsystemstate.RuntimeStatus) autoscaler.HealthStatus {
	switch s {
	case operatedsystemstate.RuntimeStatusHealthy:
		return autoscaler.HealthHealthy
	case operatedsystemstate.RuntimeStatusDegraded:
		return autoscaler.HealthDegraded
	case operatedsystemstate.RuntimeStatusWithdrawn:
		return autoscaler.HealthUnhealthy
	case operatedsystemstate.RuntimeStatusUnknown, operatedsystemstate.RuntimeStatusPending:
		return autoscaler.HealthUnknown
	default:
		return autoscaler.HealthUnknown
	}
}

// readInFlightOperatedApps invokes operatedSystemStateAccess.readInFlightOperatedApps.
// Task 4: the former Manager-local operatedSystemSummary/inFlightScope mirrors are
// retired in favor of the invoker's contract types directly.
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) readInFlightOperatedApps(ctx workflow.Context, scope operatedsystemstate.InFlightScope) ([]operatedsystemstate.OperatedSystemSummary, error) {
	return wf.Acts.OperatedSystemStateReadInFlightOperatedApps(ctx, scope)
}

// getApplicationHealth invokes operatedRuntimeAccess.getApplicationHealth (pure read),
// canonicalizing the observed operatedruntime.RuntimeStatus into the
// operatedsystemstate.RuntimeStatus vocabulary via the surviving DIVERGENT converter
// (runtimeStatusFromRuntime — two RAs' independently generated enums).
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) getApplicationHealth(ctx workflow.Context, appID operatedAppID) (operatedsystemstate.RuntimeStatus, error) {
	s, err := wf.Acts.OperatedRuntimeGetApplicationHealth(ctx, appID)
	if err != nil {
		return operatedsystemstate.RuntimeStatusUnknown, err
	}
	return runtimeStatusFromRuntime(s), nil
}

// getSloStatus invokes operatedRuntimeAccess.getSloStatus (pure read). Task 4: the
// former Manager-local sloStatusSeam mirror is retired.
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) getSloStatus(ctx workflow.Context, appID operatedAppID) (operatedruntime.SloStatus, error) {
	return wf.Acts.OperatedRuntimeGetSloStatus(ctx, appID)
}

// readComputeAttribution invokes operatedRuntimeAccess.readComputeAttribution (pure
// read) over computeAttributionWindow. Task 4: the former Manager-local
// computeAttribution mirror is retired.
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) readComputeAttribution(ctx workflow.Context, appID operatedAppID) (operatedruntime.ComputeAttribution, error) {
	return wf.Acts.OperatedRuntimeReadComputeAttribution(ctx, appID, computeAttributionWindow(workflow.Now(ctx)))
}

// computeAttributionWindow is the window one observation attributes compute over: the
// reconcile interval ending at now, in whole seconds. The Manager names the window — the
// ResourceAccess measures it and refuses one shorter than a second (ContractMisuse) — and
// the window used to be the zero value, so the real runtime refused every reconcile tick
// and every withdraw's final capture. now is the workflow clock, so a replayed tick asks
// for the same window, and the RuntimeEventID the RA derives from it keeps the usage
// append idempotent.
func computeAttributionWindow(now time.Time) operatedruntime.AttributionWindow {
	to := now.UTC().Truncate(time.Second)
	return operatedruntime.AttributionWindow{From: to.Add(-reconcileInterval), To: to}
}

// recordComputeUsage invokes usageAccess.recordComputeUsage (append; dedup-id
// idempotent). The single-event slice-wrap lives here now (was in the retired activity).
func (wf *workflows) recordComputeUsage(ctx workflow.Context, appID operatedAppID, customer customerID, attribution operatedruntime.ComputeAttribution) error {
	_, err := wf.Acts.UsageRecordComputeUsage(ctx, []usage.UsageEvent{usageEvent(ctx, appID, customer, attribution)})
	return err
}

// usageEvent assembles one contract UsageEvent from an observed attribution, attributed
// to the operated app's own customer (its head-state row's CustomerID) and to the
// billing cycle the observation falls in. The RuntimeEventID is the append-only
// ledger's dedup token (usageAccess.md §2/§3). OccurredAt — and so the cycle — is read
// from the deterministic workflow clock (replay-safe).
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func usageEvent(ctx workflow.Context, appID operatedAppID, customer customerID, attribution operatedruntime.ComputeAttribution) usage.UsageEvent {
	now := workflow.Now(ctx)
	return usage.UsageEvent{
		OperatedAppID:  appID,
		CustomerID:     customer,
		CycleID:        usageCycleID(now),
		Units:          usage.ComputeUnits{Amount: attribution.Units.Amount, Unit: attribution.Units.Unit},
		RuntimeEventID: usage.RuntimeEventID(attribution.RuntimeEventID),
		OccurredAt:     now,
	}
}

// usageCycleID is the billing cycle an instant falls in: its calendar month in UTC,
// "YYYY-MM". Usage is written to, and read back from, the cycle of the workflow clock
// — the same clock on both sides, so an observation and the read that follows it in
// the same month meet in the same cycle.
// Shared workflow-context helper (used by 4 workflows); lives in its first caller's file per the file-layout standard.
func usageCycleID(at time.Time) usage.CycleID {
	return usage.CycleID(at.UTC().Format("2006-01"))
}

// recordRuntimeStatusChange applies the observed-status head-state transition. Task 4:
// status is now operatedsystemstate.RuntimeStatus directly (runtimeStatusToState, the
// former reverse converter, has no remaining caller and is retired).
func (wf *workflows) recordRuntimeStatusChange(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version, status operatedsystemstate.RuntimeStatus) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStateRecordRuntimeStatusChange(ctx, appID, expected, status)
	})
}

// ===========================================================================
// interventionEngine — REAL divergence bridges. The workflow calls the published
// intervention.InterventionEngine.DecideOnHealth DIRECTLY (workflow.go), with
// fweng.Context{Context: context.Background()} supplied inline at the call site.
// ===========================================================================

// healthStatusFromRuntimeStatus bridges operatedsystemstate.RuntimeStatus (5 values)
// straight to intervention.HealthStatus (4 values, no Withdrawn/Pending split) — two
// genuinely different generated enums, one hop. Task 5 collapsed the former two-hop
// path (operatedsystemstate.RuntimeStatus -> this package's own RuntimeStatusSeam ->
// intervention.HealthStatus) now that the Engine is reached through its published
// contract and no longer folds through the façade's own enum along the way.
func healthStatusFromRuntimeStatus(s operatedsystemstate.RuntimeStatus) intervention.HealthStatus {
	switch s {
	case operatedsystemstate.RuntimeStatusUnknown:
		// zero-value sentinel — health not yet known, same bucket as intervention's own
		// HealthUnknown.
		return intervention.HealthUnknown
	case operatedsystemstate.RuntimeStatusPending:
		// not yet observed as healthy/degraded/withdrawn — health not yet known, same
		// bucket as intervention's own HealthUnknown.
		return intervention.HealthUnknown
	case operatedsystemstate.RuntimeStatusHealthy:
		return intervention.HealthHealthy
	case operatedsystemstate.RuntimeStatusDegraded:
		return intervention.HealthDegraded
	case operatedsystemstate.RuntimeStatusWithdrawn:
		return intervention.HealthUnhealthy
	default:
		return intervention.HealthUnknown
	}
}

// sloStatusFromMet folds the workflow's observed SLO-met bool onto intervention's own
// SLOStatus enum (a real, non-identity conversion — bool has no "shape" to mirror).
func sloStatusFromMet(met bool) intervention.SLOStatus {
	if met {
		return intervention.SLOWithinBudget
	}
	return intervention.SLOOutOfBudget
}

// ===========================================================================
// autoscalerEngine — REAL divergence bridge, now on the way IN. The Manager holds its
// autoscaler policy config in its OWN façade currency (autoscalerPolicy, workflow.go —
// Mode: this package's generated AutoscalerMode, whose zero value is
// AutoscalerModeUnknown, matching "no policy configured"). The autoscaler Engine's own
// AutoscalerMode has NO Unknown value (its zero value IS Auto), so the two enums
// genuinely disagree on VALUE — autoscalerPolicyToEngine bridges the façade policy onto
// the Engine's own autoscaler.AutoscalerPolicy at the ProposeDesiredState call site
// (workflow.go), an explicit switch rather than a raw cast. This is a config → contract
// builder (the same allowed-survivor class as slaTierFromString above), not an identity
// seam mirror. Because the Manager's OWN façade Mode already carries the
// Unknown/Auto/Manual vocabulary, the OperatedSystemView.Autoscaler.Mode field reads it
// straight off wf.AutoscalerPolicy.Mode with NO bridge needed on the way OUT
// (workflow.go, ViewWorkflow) — autoscalerModeFromEngine is retired.
// ===========================================================================

// autoscalerPolicyToEngine bridges the Manager's façade-shaped autoscaler policy
// (autoscalerPolicy, workflow.go) onto the autoscaler Engine's own published
// AutoscalerPolicy. The Mode conversion mirrors the retired autoscalerModeToEngine's
// documented behavior: the façade's zero-value AutoscalerModeUnknown maps to the
// Engine's AutoscalerModeAuto, matching the fact that the Engine's own zero value
// already IS Auto (an unconfigured policy behaves as auto-scaling either way — only the
// façade's OWN Unknown/Auto/Manual vocabulary needed preserving for the view). Fields
// with no façade-side counterpart (MaxReplicas, MaxStepDelta, IdleThreshold,
// ScaleUpCPU/ScaleDownCPU/ScaleDownGrace, SLATier, Pinned, MaxBurstCap) default to
// zero — the operations Worker carries no further policy config yet (same as before
// this bridge existed).
func autoscalerPolicyToEngine(p autoscalerPolicy) autoscaler.AutoscalerPolicy {
	return autoscaler.AutoscalerPolicy{
		Kind:             p.Kind,
		Mode:             autoscalerModeToEngine(p.Mode),
		MinReplicas:      p.MinReplicas,
		BaselineReplicas: p.BaselineReplicas,
	}
}

// autoscalerModeToEngine bridges this package's OWN façade AutoscalerMode
// (Unknown=0/Auto=1/Manual=2) onto the autoscaler Engine's own AutoscalerMode
// (Auto=0/Manual=1, no Unknown) — the two enums genuinely disagree on VALUE (not just
// on name), so an explicit switch is required, not merely convention.
func autoscalerModeToEngine(m AutoscalerMode) autoscaler.AutoscalerMode {
	switch m {
	case AutoscalerModeAuto:
		return autoscaler.AutoscalerModeAuto
	case AutoscalerModeUnknown:
		// zero-value sentinel — the autoscaler engine's own AutoscalerMode has no
		// Unknown value (its zero value IS Auto), so an unset façade policy defaults
		// to auto, same as AutoscalerModeAuto above.
		return autoscaler.AutoscalerModeAuto
	case AutoscalerModeManual:
		return autoscaler.AutoscalerModeManual
	default:
		return autoscaler.AutoscalerModeAuto
	}
}
