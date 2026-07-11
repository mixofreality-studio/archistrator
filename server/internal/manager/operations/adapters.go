package operations

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// ResourceAccess interfaces (the dependencies the GENERATED constructor
// NewOperationsManager receives) to the Manager's unexported downstream seams
// (deps.go), plus the REAL Engine-contract divergence bridges (healthStatusFromRuntimeStatus,
// slaTierFromString, autoscalerPolicyToEngine, autoscaleActionToState,
// infrastructureKindForEstimation, moneyFromEstimation, whatIfCurveFromEstimation,
// scalePointsToEstimation). Per the founder DI model (2026-06-28) these were retired
// from cmd/server and live HERE, in the one package that knows both sides — the
// Manager depends on each dependency's PUBLISHED interface and adapts it internally
// (Option-B boundary mapping), exactly as construction/systemdesign/projectdesign fold
// their adapters.
//
// None of these imports Temporal (the Manager owns it); they are plain value-copy
// bridges run inside the Manager's Activities (RA seams). The three Engines
// (intervention.InterventionEngine / autoscaler.AutoscalerEngine /
// operationestimation.OperationEstimationEngine) have NO adapter — the workflow calls
// their published contracts directly (workflow.go). The mechanical enum/struct copies
// map by IDENTITY (an explicit switch), not by raw int, so a future re-order on either
// side is safe. Where the published shape is RICHER than the Manager-local config
// (extra telemetry/policy fields) the unset fields default to zero — the operations
// Worker carries no further policy config yet, and the stub RAs return
// not-implemented at runtime regardless.

import (
	"context"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
)

// ===========================================================================
// operatedSystemStateAccess contract converters. The former operatedSystemStateAdapter
// struct is retired — the workflow reaches the RA through the generated invokers
// (invokers.gen.go) and now speaks operatedsystemstate.* directly as its internal
// currency (Task 4: the operatedSystem / operatedSystemSummary / inFlightScope /
// delinquencyAction / version Manager-local mirrors that used to require folding here
// are deleted). The two converters below are NOT fold-boundary artifacts — each bridges
// operatedsystemstate.RuntimeStatus to a genuinely distinct GENERATED type this package
// does not own outright: DesiredStateReason (this package's own façade input type) and
// RuntimeStatusSeam (this package's own façade output type, the OperatedSystemView /
// HealthSnapshotView field type).
// ===========================================================================

// runtimeStatusFromState bridges operatedsystemstate.RuntimeStatus to this package's
// generated RuntimeStatusSeam (contract.gen.go) — used at the OperatedSystemView façade
// boundary (ViewWorkflow). Kept as an explicit switch (not a raw int cast) per the
// composition root's mapping convention: RuntimeStatusSeam is a legitimately separate
// generated enum from operatedsystemstate.RuntimeStatus, even though their values line
// up today. Task 5 retired its OTHER former caller (the interventionEngine healthChange
// boundary) — see healthStatusFromRuntimeStatus below, which goes straight from
// operatedsystemstate.RuntimeStatus to intervention.HealthStatus now that the Engine is
// reached through its published contract.
func runtimeStatusFromState(s operatedsystemstate.RuntimeStatus) RuntimeStatusSeam {
	switch s {
	case operatedsystemstate.RuntimeStatusUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return RuntimeStatusUnknown
	case operatedsystemstate.RuntimeStatusPending:
		return RuntimeStatusPending
	case operatedsystemstate.RuntimeStatusHealthy:
		return RuntimeStatusHealthy
	case operatedsystemstate.RuntimeStatusDegraded:
		return RuntimeStatusDegraded
	case operatedsystemstate.RuntimeStatusWithdrawn:
		return RuntimeStatusWithdrawn
	default:
		return RuntimeStatusUnknown
	}
}

func desiredStateReasonToState(r DesiredStateReason) operatedsystemstate.DesiredStateReason {
	switch r {
	case ReasonUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return operatedsystemstate.ReasonUnknown
	case ReasonDeployAfterConstruction:
		return operatedsystemstate.ReasonDeployAfterConstruction
	case ReasonOperator:
		return operatedsystemstate.ReasonOperator
	case ReasonAutoscale:
		return operatedsystemstate.ReasonAutoscale
	case ReasonDelinquency:
		return operatedsystemstate.ReasonDelinquency
	default:
		return operatedsystemstate.ReasonUnknown
	}
}

// autoscaleActionToState bridges the autoscaler Engine's own DecisionKind straight to
// operatedsystemstate's persisted AutoscaleAction — two independently generated enums
// that happen to share order/values today (an explicit switch, not a raw cast, so a
// future re-order on either side stays safe). Task 5 collapsed the former two-hop path
// (autoscaler.DecisionKind -> this package's own façade AutoscaleAction ->
// operatedsystemstate.AutoscaleAction) into this one direct converter, now that the
// Engine is reached through its published contract and no longer folds its decision
// into the façade's own enum along the way.
func autoscaleActionToState(k autoscaler.DecisionKind) operatedsystemstate.AutoscaleAction {
	switch k {
	case autoscaler.DecisionNoChange:
		// no-op decision, nothing to do — explicit mapping to the state package's own
		// no-op constant.
		return operatedsystemstate.AutoscaleNoChange
	case autoscaler.DecisionScaleUp:
		return operatedsystemstate.AutoscaleScaleUp
	case autoscaler.DecisionScaleDown:
		return operatedsystemstate.AutoscaleScaleDown
	case autoscaler.DecisionPause:
		return operatedsystemstate.AutoscalePause
	case autoscaler.DecisionResume:
		return operatedsystemstate.AutoscaleResume
	default:
		return operatedsystemstate.AutoscaleNoChange
	}
}

func autoscaleDecisionToState(d *autoscaler.Decision) *operatedsystemstate.AutoscaleDecision {
	if d == nil {
		return nil
	}
	return &operatedsystemstate.AutoscaleDecision{
		Action:     autoscaleActionToState(d.Kind),
		Delta:      d.Delta,
		ToBaseline: d.ToBaseline,
	}
}

// ===========================================================================
// operatedRuntimeAccess -> operatedsystemstate contract converter (the
// operatedRuntimeAdapter struct is retired; see the operatedSystemStateAccess note).
// DIVERGENT survivor (Task 4): operatedruntime.RuntimeStatus and
// operatedsystemstate.RuntimeStatus are two RAs' independently generated enums that
// happen to share values today; the workflow canonicalizes observed health into the
// operatedsystemstate vocabulary at this ONE boundary (generated <-> generated, never
// seam <-> generated) so a future re-order on either side stays safe.
// ===========================================================================

func runtimeStatusFromRuntime(s operatedruntime.RuntimeStatus) operatedsystemstate.RuntimeStatus {
	switch s {
	case operatedruntime.RuntimeStatusUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return operatedsystemstate.RuntimeStatusUnknown
	case operatedruntime.RuntimeStatusPending:
		return operatedsystemstate.RuntimeStatusPending
	case operatedruntime.RuntimeStatusHealthy:
		return operatedsystemstate.RuntimeStatusHealthy
	case operatedruntime.RuntimeStatusDegraded:
		return operatedsystemstate.RuntimeStatusDegraded
	case operatedruntime.RuntimeStatusWithdrawn:
		return operatedsystemstate.RuntimeStatusWithdrawn
	default:
		return operatedsystemstate.RuntimeStatusUnknown
	}
}

// ===========================================================================
// durableExecutionAccess adapter — over durableexecution.DurableExecutionAccess. Only
// the startup RegisterSchedule verb is consumed (the published ScheduleSpec resolves
// the task queue via its KindBinding table, so the seam's TaskQueue is not threaded).
// ===========================================================================

type durableAdapter struct {
	inner durableexecution.DurableExecutionAccess
}

var _ durableExecutionAccess = durableAdapter{}

func (a durableAdapter) RegisterSchedule(ctx context.Context, spec scheduleSpec) error {
	return a.inner.RegisterSchedule(
		fwra.Context{Context: ctx},
		durableexecution.ScheduleID(spec.ID),
		durableexecution.ScheduleSpec{
			ExecutionKind: durableexecution.ExecutionKind(spec.WorkflowType),
			Cadence:       durableexecution.Cadence{Every: time.Duration(spec.IntervalSecs) * time.Second},
		},
	)
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

// slaTierFromString resolves the Manager's raw string SLA-tier config (no typed config
// source is wired yet — the operations Worker carries no policy config today) onto
// intervention.SLATier. Kept as a genuine config -> engine-input builder (not an
// identity seam mirror): unlike the retired local interventionPolicy struct, there is
// no shadow SLATier enum on this side to eliminate — the source really is a string.
func slaTierFromString(s string) intervention.SLATier {
	switch s {
	case "paid":
		return intervention.SLATierPaid
	case "enterprise":
		return intervention.SLATierEnterprise
	default:
		return intervention.SLATierFree
	}
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

// ===========================================================================
// operationEstimationEngine — REAL divergence bridges. The workflow calls the
// published operationestimation.OperationEstimationEngine.ProjectForOperatedApp
// DIRECTLY (workflow.go). observedUsageFromEvents (the real Σ-Units.Amount aggregation
// off the usage RA's read range) lives in workflow.go, alongside billing's
// foldRevenue/foldUsage precedent — it is workflow-owned folding, not a boundary
// adapter.
// ===========================================================================

// infrastructureKindForEstimation bridges this Manager's canonical InfrastructureKind
// currency (autoscaler.InfrastructureKind — also autoscalerPolicy/DesiredState's type)
// onto operationestimation's OWN InfrastructureKind. Two independently generated enums
// that happen to share values today; an explicit switch, not a raw cast.
func infrastructureKindForEstimation(k autoscaler.InfrastructureKind) operationestimation.InfrastructureKind {
	switch k {
	case autoscaler.InfrastructureKindUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return operationestimation.InfrastructureKindUnknown
	case autoscaler.InfrastructureKindGoTemporalPostgres:
		return operationestimation.InfrastructureKindGoTemporalPostgres
	default:
		return operationestimation.InfrastructureKindUnknown
	}
}

// scalePointsToEstimation converts the façade's own ScalePoint (Replicas int64) into
// operationestimation's ScalePoint (LoadMultiplier float64) — a real unit divergence
// (an integer replica count vs. a float load multiplier), not a rename.
func scalePointsToEstimation(points []ScalePoint) []operationestimation.ScalePoint {
	out := make([]operationestimation.ScalePoint, 0, len(points))
	for _, p := range points {
		out = append(out, operationestimation.ScalePoint{LoadMultiplier: float64(p.Replicas)})
	}
	return out
}

// moneyFromEstimation bridges operationestimation's own Money onto this package's
// generated façade Money (contract.gen.go) — same shape today, but genuinely distinct
// generated types (their JSON tags already diverge: MinorUnits/Currency here vs.
// minorUnits/currency on the engine's side), so a field-by-field copy, not a cast.
func moneyFromEstimation(m operationestimation.Money) Money {
	return Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// whatIfCurveFromEstimation bridges operationestimation's WhatIfCurve/WhatIfPoint onto
// this package's own façade WhatIfCurve/WhatIfPoint — the engine's WhatIfPoint carries
// LoadMultiplier float64, the façade's carries Replicas int64 (the same real unit
// divergence as scalePointsToEstimation, in reverse).
func whatIfCurveFromEstimation(c operationestimation.WhatIfCurve) WhatIfCurve {
	points := make([]WhatIfPoint, 0, len(c.Points))
	for _, p := range c.Points {
		points = append(points, WhatIfPoint{
			Replicas:             int64(p.LoadMultiplier),
			ProjectedMonthlyCost: moneyFromEstimation(p.ProjectedMonthlyCost),
		})
	}
	return WhatIfCurve{Points: points}
}

// costProjectionFromEstimation bridges operationestimation's own CostProjection onto
// this package's generated façade CostProjectionSeam (contract.gen.go) — the façade's
// re-exported QueryCostProjection/QueryOperatedSystemView result type.
func costProjectionFromEstimation(p operationestimation.CostProjection) CostProjectionSeam {
	return CostProjectionSeam{
		CurrentRunRate:       moneyFromEstimation(p.CurrentRunRate),
		ProjectedMonthlyCost: moneyFromEstimation(p.ProjectedMonthlyCost),
		ScaleWhatIfCurve:     whatIfCurveFromEstimation(p.ScaleWhatIfCurve),
	}
}
