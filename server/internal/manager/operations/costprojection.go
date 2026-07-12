package operations

import (
	"context"

	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

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
		fweng.Context{Context: context.Background()},
		observedUsageFromEvents(events),
		infrastructureKindForEstimation(wf.InfrastructureKind),
		scalePointsToEstimation(in.ScaleWhatIfPoints),
	)
	if perr != nil {
		return costProjection{}, fwmgr.MapError(perr)
	}
	return costProjectionFromEstimation(projection), nil
}

// readUsageRange invokes usageAccess.readRange (pure read). Task 4 retired the former
// Manager-local usageRangeQuerySeam mirror (query IS the contract type now); Task 5
// retired usageEventSeam (its only remaining role was operationEstimationEngine's seam
// input shape) — the result now passes straight through with no fold;
// observedUsageFromEvents aggregates it directly into the Engine's published
// ObservedUsage input at the call site.
func (wf *workflows) readUsageRange(ctx workflow.Context, query usage.UsageRangeQuery) ([]usage.UsageEvent, error) {
	return wf.Acts.UsageReadRange(ctx, query)
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
