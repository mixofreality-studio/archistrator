package operations

import (
	"context"

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
			// EXECUTE Retry: re-publish prior desired state so the runtime self-heals /
			// re-converges (content-idempotent — a no-op if unchanged).
			if perr := wf.publishDesiredState(ctx, app.ID, operatedruntime.RuntimeDesiredState{ContentType: "application/desired-state"}); perr != nil {
				return false, false, perr
			}
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
	republished, err = wf.autoscaleOne(ctx, app, version)
	if err != nil {
		return transitioned, false, err
	}
	return transitioned, republished, nil
}

// autoscaleOne runs Path C (autoscale) for one in-flight app: propose a desired state
// and, on a non-NoChange decision, publish + record the republish. version is the
// head-state version as advanced by Path B's status transition (if any). Extracted from
// reconcileOne to satisfy the gocyclo gate — the workflow-command order is identical to
// the pre-extraction inline body.
func (wf *workflows) autoscaleOne(ctx workflow.Context, app operatedsystemstate.OperatedSystemSummary, version operatedsystemstate.Version) (republished bool, err error) {
	// autoscalerPolicyToEngine bridges the Manager's own façade AutoscalerPolicy onto
	// the Engine's published AutoscalerPolicy (adapters.go) — the one call site that
	// needs the Engine's own Mode shape.
	decision, aerr2 := wf.Autoscaler.ProposeDesiredState(
		fweng.Context{Context: context.Background()},
		autoscaler.Telemetry{CurrentReplicas: 0},
		autoscaler.DesiredState{InfrastructureKind: wf.InfrastructureKind},
		autoscalerPolicyToEngine(wf.AutoscalerPolicy),
		wf.InfrastructureKind,
	)
	if aerr2 != nil {
		return false, fwmgr.MapError(aerr2)
	}
	if decision.Kind == autoscaler.DecisionNoChange {
		return false, nil
	}

	// Non-NoChange ⇒ render revised manifests → publish → record (reason=autoscale).
	// Idle-pause (AutoscalePause) renders replicas=0 inside the opaque bytes.
	if perr := wf.publishDesiredState(ctx, app.ID, operatedruntime.RuntimeDesiredState{ContentType: "application/desired-state"}); perr != nil {
		return false, perr
	}
	dec := decision
	if _, rerr := wf.recordPublishDesiredState(ctx, app.ID, version, ReasonAutoscale, &dec); rerr != nil {
		return false, rerr
	}
	return true, nil
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
// read). The Manager pins the window to a default (open) window here; the RA attributes
// since last observation. Task 4: the former Manager-local computeAttribution mirror is
// retired.
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) readComputeAttribution(ctx workflow.Context, appID operatedAppID) (operatedruntime.ComputeAttribution, error) {
	return wf.Acts.OperatedRuntimeReadComputeAttribution(ctx, appID, operatedruntime.AttributionWindow{})
}

// recordComputeUsage invokes usageAccess.recordComputeUsage (append; dedup-id
// idempotent). The single-event slice-wrap lives here now (was in the retired activity).
func (wf *workflows) recordComputeUsage(ctx workflow.Context, appID operatedAppID, attribution operatedruntime.ComputeAttribution) error {
	_, err := wf.Acts.UsageRecordComputeUsage(ctx, []usage.UsageEvent{wf.usageEvent(ctx, appID, attribution)})
	return err
}

// usageEvent assembles one contract UsageEvent from an observed attribution. The
// RuntimeEventID is the append-only ledger's dedup token (usageAccess.md §2/§3).
// OccurredAt is read from the deterministic workflow clock (replay-safe).
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
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
