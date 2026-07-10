package operations

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// engine / ResourceAccess interfaces (the dependencies the GENERATED constructor
// NewOperationsManager receives) to the Manager's unexported downstream seams
// (deps.go). Per the founder DI model (2026-06-28) these were retired from cmd/server
// and live HERE, in the one package that knows both sides — the Manager depends on
// each dependency's PUBLISHED interface and adapts it internally (Option-B boundary
// mapping), exactly as construction/systemdesign/projectdesign fold their adapters.
//
// None of these imports Temporal (the Manager owns it); they are plain value-copy
// bridges run inside the Manager's Activities (RA seams) or directly in-workflow
// (Engine seams). The mechanical enum/struct copies map by IDENTITY (an explicit
// switch), not by raw int, so a future re-order on either side is safe. The published
// op-state types are RICHER than the Manager-local seams (extra telemetry/policy
// fields); the unset fields default to zero — the operations Worker carries no policy
// config yet, and the stub RAs return not-implemented at runtime regardless.

import (
	"context"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
)

// ===========================================================================
// operatedSystemStateAccess contract <-> Manager-local seam converters. The former
// operatedSystemStateAdapter struct is retired — the workflow reaches the RA through
// the generated invokers (invokers.gen.go); these pure converters fold the contract
// types the invokers exchange into the Manager-local seams the workflow + Engines use.
// ===========================================================================

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

func runtimeStatusToState(s RuntimeStatusSeam) operatedsystemstate.RuntimeStatus {
	switch s {
	case RuntimeStatusUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return operatedsystemstate.RuntimeStatusUnknown
	case RuntimeStatusPending:
		return operatedsystemstate.RuntimeStatusPending
	case RuntimeStatusHealthy:
		return operatedsystemstate.RuntimeStatusHealthy
	case RuntimeStatusDegraded:
		return operatedsystemstate.RuntimeStatusDegraded
	case RuntimeStatusWithdrawn:
		return operatedsystemstate.RuntimeStatusWithdrawn
	default:
		return operatedsystemstate.RuntimeStatusUnknown
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

func delinquencyActionToState(a delinquencyAction) operatedsystemstate.DelinquencyAction {
	switch a {
	case delinquencyActionPaused:
		return operatedsystemstate.DelinquencyActionPaused
	case delinquencyActionWithdrawn:
		return operatedsystemstate.DelinquencyActionWithdrawn
	default:
		return operatedsystemstate.DelinquencyActionUnknown
	}
}

func autoscaleActionToState(a AutoscaleAction) operatedsystemstate.AutoscaleAction {
	switch a {
	case AutoscaleNoChange:
		// no-op action, nothing to do — explicit mapping to the state package's own
		// no-op constant.
		return operatedsystemstate.AutoscaleNoChange
	case AutoscaleScaleUp:
		return operatedsystemstate.AutoscaleScaleUp
	case AutoscaleScaleDown:
		return operatedsystemstate.AutoscaleScaleDown
	case AutoscalePause:
		return operatedsystemstate.AutoscalePause
	case AutoscaleResume:
		return operatedsystemstate.AutoscaleResume
	default:
		return operatedsystemstate.AutoscaleNoChange
	}
}

func autoscaleDecisionToState(d *autoscaleDecisionSeam) *operatedsystemstate.AutoscaleDecision {
	if d == nil {
		return nil
	}
	return &operatedsystemstate.AutoscaleDecision{
		Action:     autoscaleActionToState(d.Action),
		Delta:      int64(d.Delta),
		ToBaseline: int64(d.ToBaseline),
	}
}

// ===========================================================================
// operatedRuntimeAccess contract -> Manager-local seam converter (the
// operatedRuntimeAdapter struct is retired; see the operatedSystemStateAccess note).
// ===========================================================================

func runtimeStatusFromRuntime(s operatedruntime.RuntimeStatus) RuntimeStatusSeam {
	switch s {
	case operatedruntime.RuntimeStatusUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return RuntimeStatusUnknown
	case operatedruntime.RuntimeStatusPending:
		return RuntimeStatusPending
	case operatedruntime.RuntimeStatusHealthy:
		return RuntimeStatusHealthy
	case operatedruntime.RuntimeStatusDegraded:
		return RuntimeStatusDegraded
	case operatedruntime.RuntimeStatusWithdrawn:
		return RuntimeStatusWithdrawn
	default:
		return RuntimeStatusUnknown
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
// interventionEngine adapter — over intervention.InterventionEngine (operate-time
// DecideOnHealth). The seam's policy is folded into the published HealthChange.Policy.
// ===========================================================================

type interventionAdapter struct {
	inner intervention.InterventionEngine
}

var _ interventionEngine = interventionAdapter{}

func (a interventionAdapter) DecideOnHealth(change healthChange, policy interventionPolicy) (healthDirective, error) {
	d, err := a.inner.DecideOnHealth(fweng.Context{Context: context.Background()}, intervention.HealthChange{
		OperatedAppID: intervention.OperatedAppID(change.AppID.String()),
		FromHealth:    healthStatusFromSeam(change.FromStatus),
		ToHealth:      healthStatusFromSeam(change.ToStatus),
		SLOStatus:     sloStatusFromMet(change.SloMet),
		Policy:        interventionPolicyToEngine(policy),
	})
	if err != nil {
		return healthDirectiveUnknown, err
	}
	switch d {
	case intervention.HealthRetry:
		return healthDirectiveRetry, nil
	case intervention.HealthEscalate:
		return healthDirectiveEscalate, nil
	default:
		return healthDirectiveUnknown, nil
	}
}

func healthStatusFromSeam(s RuntimeStatusSeam) intervention.HealthStatus {
	switch s {
	case RuntimeStatusUnknown:
		// zero-value sentinel — health not yet known, same bucket as intervention's own
		// HealthUnknown.
		return intervention.HealthUnknown
	case RuntimeStatusPending:
		// not yet observed as healthy/degraded/withdrawn — health not yet known, same
		// bucket as intervention's own HealthUnknown.
		return intervention.HealthUnknown
	case RuntimeStatusHealthy:
		return intervention.HealthHealthy
	case RuntimeStatusDegraded:
		return intervention.HealthDegraded
	case RuntimeStatusWithdrawn:
		return intervention.HealthUnhealthy
	default:
		return intervention.HealthUnknown
	}
}

func sloStatusFromMet(met bool) intervention.SLOStatus {
	if met {
		return intervention.SLOWithinBudget
	}
	return intervention.SLOOutOfBudget
}

func interventionPolicyToEngine(p interventionPolicy) intervention.InterventionPolicy {
	return intervention.InterventionPolicy{
		RetryBudget: int64(p.RetryBudget),
		SLATier:     slaTierFromString(p.SLATier),
	}
}

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
// autoscalerEngine adapter — over autoscaler.AutoscalerEngine.
// ===========================================================================

type autoscalerAdapter struct {
	inner autoscaler.AutoscalerEngine
}

var _ autoscalerEngine = autoscalerAdapter{}

func (a autoscalerAdapter) ProposeDesiredState(telemetry telemetry, currentDesired autoscalerDesiredState, policy autoscalerPolicy, infrastructureKind infrastructureKind) (autoscaleDecisionSeam, error) {
	d, err := a.inner.ProposeDesiredState(
		fweng.Context{Context: context.Background()},
		autoscaler.Telemetry{
			RequestsPerSecond: telemetry.RequestsPerSecond,
			P95LatencyMs:      telemetry.P95LatencyMs,
			CurrentReplicas:   int64(telemetry.CurrentReplicas),
			CPUUtilization:    telemetry.CPUUtilization,
		},
		autoscaler.DesiredState{
			InfrastructureKind: infraKindToAutoscaler(currentDesired.InfrastructureKind),
			Replicas:           int64(currentDesired.Replicas),
		},
		autoscaler.AutoscalerPolicy{
			Kind:             infraKindToAutoscaler(policy.Kind),
			Mode:             autoscalerModeToEngine(policy.Mode),
			MinReplicas:      int64(policy.MinReplicas),
			BaselineReplicas: int64(policy.BaselineReplicas),
		},
		infraKindToAutoscaler(infrastructureKind),
	)
	if err != nil {
		return autoscaleDecisionSeam{}, err
	}
	return autoscaleDecisionSeam{
		Action:     autoscaleActionFromDecision(d.Kind),
		Delta:      int(d.Delta),
		ToBaseline: int(d.ToBaseline),
	}, nil
}

func infraKindToAutoscaler(k infrastructureKind) autoscaler.InfrastructureKind {
	switch k {
	case infrastructureKindGoTemporalPostgres:
		return autoscaler.InfrastructureKindGoTemporalPostgres
	default:
		return autoscaler.InfrastructureKindUnknown
	}
}

func autoscalerModeToEngine(m AutoscalerMode) autoscaler.AutoscalerMode {
	switch m {
	case AutoscalerModeAuto:
		// literal auto mapping.
		return autoscaler.AutoscalerModeAuto
	case AutoscalerModeUnknown:
		// zero-value sentinel — the autoscaler engine's own AutoscalerMode has no
		// Unknown value (its zero value IS Auto), so an unset mode defaults to auto,
		// same as AutoscalerModeAuto above.
		return autoscaler.AutoscalerModeAuto
	case AutoscalerModeManual:
		return autoscaler.AutoscalerModeManual
	default:
		return autoscaler.AutoscalerModeAuto
	}
}

func autoscaleActionFromDecision(k autoscaler.DecisionKind) AutoscaleAction {
	switch k {
	case autoscaler.DecisionNoChange:
		// no-op decision, nothing to do — explicit mapping to the local seam's own
		// no-op constant.
		return AutoscaleNoChange
	case autoscaler.DecisionScaleUp:
		return AutoscaleScaleUp
	case autoscaler.DecisionScaleDown:
		return AutoscaleScaleDown
	case autoscaler.DecisionPause:
		return AutoscalePause
	case autoscaler.DecisionResume:
		return AutoscaleResume
	default:
		return AutoscaleNoChange
	}
}

// ===========================================================================
// operationEstimationEngine adapter — over operationestimation.OperationEstimationEngine.
// The seam carries raw usage EVENTS; the published ProjectForOperatedApp consumes an
// aggregated ObservedUsage, so the adapter rolls the events up (sum of metered units).
// ===========================================================================

type estimationAdapter struct {
	inner operationestimation.OperationEstimationEngine
}

var _ operationEstimationEngine = estimationAdapter{}

func (a estimationAdapter) ProjectForOperatedApp(observedUsage observedUsage, infrastructureKind infrastructureKind, scaleWhatIfPoints []ScalePoint) (CostProjectionSeam, error) {
	var computeUnitSeconds float64
	for _, e := range observedUsage.Events {
		computeUnitSeconds += e.Units.Amount
	}
	points := make([]operationestimation.ScalePoint, 0, len(scaleWhatIfPoints))
	for _, p := range scaleWhatIfPoints {
		points = append(points, operationestimation.ScalePoint{LoadMultiplier: float64(p.Replicas)})
	}
	proj, err := a.inner.ProjectForOperatedApp(
		fweng.Context{Context: context.Background()},
		operationestimation.ObservedUsage{
			ComputeUnitSeconds: computeUnitSeconds,
			RequestCount:       int64(len(observedUsage.Events)),
		},
		infraKindToEstimation(infrastructureKind),
		points,
	)
	if err != nil {
		return CostProjectionSeam{}, err
	}
	return CostProjectionSeam{
		CurrentRunRate:       moneyFromEstimation(proj.CurrentRunRate),
		ProjectedMonthlyCost: moneyFromEstimation(proj.ProjectedMonthlyCost),
		ScaleWhatIfCurve:     whatIfCurveFromEstimation(proj.ScaleWhatIfCurve),
	}, nil
}

func infraKindToEstimation(k infrastructureKind) operationestimation.InfrastructureKind {
	switch k {
	case infrastructureKindGoTemporalPostgres:
		return operationestimation.InfrastructureKindGoTemporalPostgres
	default:
		return operationestimation.InfrastructureKindUnknown
	}
}

func moneyFromEstimation(m operationestimation.Money) Money {
	return Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

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
