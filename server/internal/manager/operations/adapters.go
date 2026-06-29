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
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usagelog"
)

// ===========================================================================
// operatedSystemStateAccess adapter — over operatedsystemstate.OperatedSystemStateAccess.
// ===========================================================================

type operatedSystemStateAdapter struct {
	inner operatedsystemstate.OperatedSystemStateAccess
}

var _ operatedSystemStateAccess = operatedSystemStateAdapter{}

func (a operatedSystemStateAdapter) ReadOperatedSystem(ctx context.Context, operatedAppID operatedAppID) (operatedSystem, error) {
	op, err := a.inner.ReadOperatedSystem(fwra.Context{Context: ctx}, operatedAppID)
	if err != nil {
		return operatedSystem{}, err
	}
	return operatedSystem{
		ID:                  op.ID,
		Version:             version(op.Version),
		Status:              runtimeStatusFromState(op.Status),
		InFlight:            op.InFlight,
		DeployableBundleRef: op.DeployableBundleRef,
	}, nil
}

func (a operatedSystemStateAdapter) ReadInFlightOperatedApps(ctx context.Context, scope inFlightScope) ([]operatedSystemSummary, error) {
	apps, err := a.inner.ReadInFlightOperatedApps(fwra.Context{Context: ctx}, operatedsystemstate.InFlightScope{
		AppIDs:     scope.AppIDs,
		CustomerID: scope.CustomerID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]operatedSystemSummary, 0, len(apps))
	for _, s := range apps {
		out = append(out, operatedSystemSummary{
			ID:      s.ID,
			Version: version(s.Version),
			Status:  runtimeStatusFromState(s.Status),
		})
	}
	return out, nil
}

func (a operatedSystemStateAdapter) PublishDesiredState(ctx context.Context, operatedAppID operatedAppID, expectedVersion version, reason DesiredStateReason, decision *autoscaleDecisionSeam, idempotencyKey fwra.IdempotencyKey) (version, error) {
	v, err := a.inner.PublishDesiredState(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		operatedAppID,
		operatedsystemstate.Version(expectedVersion),
		desiredStateReasonToState(reason),
		autoscaleDecisionToState(decision),
		idempotencyKey,
	)
	return version(v), err
}

func (a operatedSystemStateAdapter) RecordRuntimeStatusChange(ctx context.Context, operatedAppID operatedAppID, expectedVersion version, status RuntimeStatusSeam, idempotencyKey fwra.IdempotencyKey) (version, error) {
	v, err := a.inner.RecordRuntimeStatusChange(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		operatedAppID,
		operatedsystemstate.Version(expectedVersion),
		runtimeStatusToState(status),
		idempotencyKey,
	)
	return version(v), err
}

func (a operatedSystemStateAdapter) WithdrawSystem(ctx context.Context, operatedAppID operatedAppID, expectedVersion version, idempotencyKey fwra.IdempotencyKey) (version, error) {
	v, err := a.inner.WithdrawSystem(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		operatedAppID,
		operatedsystemstate.Version(expectedVersion),
		idempotencyKey,
	)
	return version(v), err
}

func (a operatedSystemStateAdapter) RecordDelinquencyAction(ctx context.Context, operatedAppID operatedAppID, expectedVersion version, action delinquencyAction, idempotencyKey fwra.IdempotencyKey) (version, error) {
	v, err := a.inner.RecordDelinquencyAction(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		operatedAppID,
		operatedsystemstate.Version(expectedVersion),
		delinquencyActionToState(action),
		idempotencyKey,
	)
	return version(v), err
}

func runtimeStatusFromState(s operatedsystemstate.RuntimeStatus) RuntimeStatusSeam {
	switch s {
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
// operatedRuntimeAccess adapter — over operatedruntime.OperatedRuntimeAccess.
// ===========================================================================

type operatedRuntimeAdapter struct {
	inner operatedruntime.OperatedRuntimeAccess
}

var _ operatedRuntimeAccess = operatedRuntimeAdapter{}

func (a operatedRuntimeAdapter) PublishDesiredState(ctx context.Context, appID operatedAppID, desired runtimeDesiredState, idempotencyKey fwra.IdempotencyKey) error {
	return a.inner.PublishDesiredState(
		fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey},
		appID,
		operatedruntime.RuntimeDesiredState{Bytes: desired.Bytes, ContentType: desired.ContentType},
		idempotencyKey,
	)
}

func (a operatedRuntimeAdapter) Withdraw(ctx context.Context, appID operatedAppID, idempotencyKey fwra.IdempotencyKey) error {
	return a.inner.Withdraw(fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey}, appID, idempotencyKey)
}

func (a operatedRuntimeAdapter) GetApplicationHealth(ctx context.Context, appID operatedAppID) (RuntimeStatusSeam, error) {
	s, err := a.inner.GetApplicationHealth(fwra.Context{Context: ctx}, appID)
	if err != nil {
		return RuntimeStatusUnknown, err
	}
	return runtimeStatusFromRuntime(s), nil
}

func (a operatedRuntimeAdapter) GetSloStatus(ctx context.Context, appID operatedAppID) (sloStatusSeam, error) {
	s, err := a.inner.GetSloStatus(fwra.Context{Context: ctx}, appID)
	if err != nil {
		return sloStatusSeam{}, err
	}
	return sloStatusSeam{SloMet: s.SloMet, Detail: s.Detail}, nil
}

func (a operatedRuntimeAdapter) ReadComputeAttribution(ctx context.Context, appID operatedAppID, window attributionWindow) (computeAttribution, error) {
	att, err := a.inner.ReadComputeAttribution(fwra.Context{Context: ctx}, appID, operatedruntime.AttributionWindow{
		From: window.From,
		To:   window.To,
	})
	if err != nil {
		return computeAttribution{}, err
	}
	return computeAttribution{
		Units:          computeUnitsSeam{Amount: att.Units.Amount, Unit: att.Units.Unit},
		RuntimeEventID: att.RuntimeEventID,
	}, nil
}

func runtimeStatusFromRuntime(s operatedruntime.RuntimeStatus) RuntimeStatusSeam {
	switch s {
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
// usageAccess adapter — over usagelog.UsageAccess (dropping the published []EntryRef).
// ===========================================================================

type usageAdapter struct {
	inner usagelog.UsageAccess
}

var _ usageAccess = usageAdapter{}

func (a usageAdapter) RecordComputeUsage(ctx context.Context, events []usageEventSeam) error {
	_, err := a.inner.RecordComputeUsage(fwra.Context{Context: ctx}, usageEventsToLog(events))
	return err
}

func (a usageAdapter) RecordFinalUsage(ctx context.Context, events []usageEventSeam) error {
	_, err := a.inner.RecordFinalUsage(fwra.Context{Context: ctx}, usageEventsToLog(events))
	return err
}

func (a usageAdapter) ReadRange(ctx context.Context, query usageRangeQuerySeam) ([]usageEventSeam, error) {
	events, err := a.inner.ReadRange(fwra.Context{Context: ctx}, usagelog.UsageRangeQuery{
		CustomerID:    query.CustomerID,
		CycleID:       usagelog.CycleID(query.CycleID),
		OperatedAppID: query.OperatedAppID,
	})
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

func usageEventsToLog(events []usageEventSeam) []usagelog.UsageEvent {
	out := make([]usagelog.UsageEvent, 0, len(events))
	for _, e := range events {
		out = append(out, usagelog.UsageEvent{
			CustomerID:     e.CustomerID,
			OperatedAppID:  e.OperatedAppID,
			CycleID:        usagelog.CycleID(e.CycleID),
			Units:          usagelog.ComputeUnits{Amount: e.Units.Amount, Unit: e.Units.Unit},
			RuntimeEventID: usagelog.RuntimeEventID(e.RuntimeEventID),
			OccurredAt:     e.ObservedAt,
		})
	}
	return out
}

// ===========================================================================
// artifactAccess adapter — over artifact.ArtifactAccess. Escalation E-1: the frozen
// retrieveDeployableBundle verb is not yet on the package, so the deployable bundle is
// served by the existing RetrieveConstructionOutput (the deployable bundle IS a
// construction output — artifactAccess.md).
// ===========================================================================

type artifactAdapter struct {
	inner artifact.ArtifactAccess
}

var _ artifactAccess = artifactAdapter{}

func (a artifactAdapter) RetrieveDeployableBundle(ctx context.Context, deployableBundleRef string) (deployableBundle, error) {
	out, err := a.inner.RetrieveConstructionOutput(fwra.Context{Context: ctx}, deployableBundleRef)
	if err != nil {
		return deployableBundle{}, err
	}
	return deployableBundle{Output: out}, nil
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
	case AutoscalerModeManual:
		return autoscaler.AutoscalerModeManual
	default:
		return autoscaler.AutoscalerModeAuto
	}
}

func autoscaleActionFromDecision(k autoscaler.DecisionKind) AutoscaleAction {
	switch k {
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
