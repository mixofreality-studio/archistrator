package operations

import (
	"context"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
)

// This file declares the Manager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom) for the collaborators that are NOT yet built as Go
// packages, plus the narrow consumer seams for the two that exist but whose frozen
// verb isn't on them yet:
//
//   - ArtifactAccess         — exists as internal/resourceaccess/artifact, BUT the
//     frozen retrieveDeployableBundle verb is NOT yet on the package (it currently
//     has RetrieveConstructionOutput). Consumed here via a NARROW seam interface
//     mirroring the frozen verb; the composition root adapts the concrete *artifact.Store
//     once that verb lands (escalation E-1 in C-MOP.md).
//   - DurableExecutionAccess — exists as internal/resourceaccess/durableexecution;
//     only RegisterSchedule is a contract op this Manager calls (at startup). The
//     in-workflow primitives (awaitSignal / startTimer) are the Manager's OWN
//     workflow code (D-DA category A), NOT RA methods — they live in workflow.go /
//     operationsmanager.go.
//
// operatedSystemStateAccess, operatedRuntimeAccess, and usageAccess are reached ONLY
// through the generated typed invokers (invokers.gen.go); interventionEngine,
// autoscalerEngine, and operationEstimationEngine are reached through their PUBLISHED
// contracts directly (workflow.go) — see the retirement note at the end of this file.
// No Manager-local mirror remains for any of the six.

// ===========================================================================
// operatedSystemStateAccess — reached ONLY through the generated typed invokers
// (invokers.gen.go), which carry the contract types (operatedsystemstate.OperatedSystem
// / OperatedSystemSummary / InFlightScope / Version / DelinquencyAction / RuntimeStatus)
// directly. Task 4 (seam-adapter cleanup) retired the Manager-local mirrors that used
// to duplicate these shapes 1:1 (operatedSystem, operatedSystemSummary, inFlightScope,
// delinquencyAction, version) — the workflow now speaks operatedsystemstate.* directly;
// no fold happens at the RA boundary. RuntimeStatusSeam (this package's OWN generated
// façade enum, contract.gen.go) is NOT one of those mirrors — it stays, because it is
// also the public OperatedSystemView/HealthSnapshotView field type; adapters.go keeps
// runtimeStatusFromState to bridge operatedsystemstate.RuntimeStatus into it at that
// façade boundary. Task 5 retired the OTHER caller of runtimeStatusFromState (the
// interventionEngine healthChange boundary) — that hop now goes straight from
// operatedsystemstate.RuntimeStatus to intervention.HealthStatus (healthStatusFromRuntimeStatus,
// adapters.go), since the engine is reached through its published contract now.
// ===========================================================================

// ===========================================================================
// operatedRuntimeAccess — reached ONLY through the generated typed invokers (see the
// operatedSystemStateAccess note above). Task 4 retired the Manager-local mirrors that
// duplicated its shapes 1:1 (runtimeDesiredState, sloStatusSeam, computeAttribution) —
// the workflow now speaks operatedruntime.RuntimeDesiredState / SloStatus /
// ComputeAttribution directly. Task 5 retired computeUnitsSeam (it was kept alive only
// as operationEstimationEngine's seam input shape via usageEventSeam below; the engine
// is now reached through its published contract, whose ObservedUsage the workflow
// builds directly from usage.UsageEvent — see observedUsageFromEvents, workflow.go).
// ===========================================================================

// ===========================================================================
// usageAccess — reached ONLY through the generated typed invokers (see the
// operatedSystemStateAccess note above). Task 4 retired usageRangeQuerySeam (an exact
// 1:1 mirror of usage.UsageRangeQuery with no other consumer) — the workflow now builds
// usage.UsageRangeQuery directly. Task 5 retired usageEventSeam — its only remaining
// role was operationEstimationEngine's (seam) input shape; readUsageRange (workflow.go)
// now returns []usage.UsageEvent straight from the generated invoker with no fold, and
// observedUsageFromEvents (workflow.go) aggregates it directly into the published
// operationestimation.ObservedUsage the Engine consumes.
// ===========================================================================

// ===========================================================================
// artifactAccess — EXISTS as a Go package (internal/resourceaccess/artifact) but the
// frozen retrieveDeployableBundle verb is NOT yet on it (it has
// RetrieveConstructionOutput). Consumed here via a NARROW seam interface mirroring
// the frozen verb; the composition root adapts the concrete *artifact.Store once the
// verb lands (escalation E-1 in C-MOP.md). The bundle ref is a plain content
// address (a string), matching the package's content-address discipline.
// ===========================================================================

// NOTE: the artifactAccess consumer-seam interface is retired (see the
// operatedSystemStateAccess note above) — reached through the generated invoker
// ArtifactRetrieveConstructionOutput (escalation E-1: the deployable bundle IS a
// construction output until the frozen retrieveDeployableBundle verb lands). The
// deployableBundle mirror below remains as the workflow's retrieve-bundle result.

// deployableBundle mirrors the constructed-output bundle retrieved for a first
// deploy. Re-uses the existing artifact.ConstructionOutput shape as the bundle body
// (the deployable bundle IS a construction output — artifactAccess.md), kept as a
// thin Manager-local wrapper so the seam stays narrow.
type deployableBundle struct {
	Output artifact.ConstructionOutput
}

// ===========================================================================
// durableExecutionAccess — EXISTS (internal/resourceaccess/durableexecution). Only
// RegisterSchedule is a contract op this Manager calls (at startup). Consumed via a
// narrow seam interface so the composition root adapts the concrete
// *durableexecution.Runtime (whose RegisterSchedule signature is
// RegisterSchedule(ctx, ScheduleID, ScheduleSpec)). The in-workflow primitives
// (awaitSignal / startTimer) are the Manager's OWN workflow code (D-DA category A),
// NOT RA methods.
// ===========================================================================

// durableExecutionAccess is the Manager's consumer view: the one startup op.
// UNEXPORTED seam; the folded adapter bridges the published
// durableexecution.DurableExecutionAccess to it.
type durableExecutionAccess interface {
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// scheduleSpec mirrors durableexecution.ScheduleSpec for the one startup op (the
// operatedStateReconcile Schedule). The composition root adapts the concrete RA.
type scheduleSpec struct {
	ID           string
	WorkflowType string
	TaskQueue    string
	IntervalSecs int
}

// ===========================================================================
// interventionEngine / autoscalerEngine / operationEstimationEngine — RETIRED. The
// consumer-seam interfaces AND their local data mirrors (healthChange,
// interventionPolicy, healthDirective + its consts, telemetry, autoscalerDesiredState,
// infrastructureKind + its const, autoscaleDecisionSeam, computeUnitsSeam,
// usageEventSeam, observedUsage) are retired — the workflow reaches all three Engines
// through their PUBLISHED contracts (intervention.InterventionEngine /
// autoscaler.AutoscalerEngine / operationestimation.OperationEstimationEngine, each
// component's contract.gen.go), called DIRECTLY in-workflow by value (no Activity, no
// idempotency key, imports no Temporal), with
// fweng.Context{Context: context.Background()} supplied inline at each call site
// (workflow.go).
//
// autoscalerPolicy (workflow.go) is NOT one of the retired seam mirrors above, despite
// the name: it is the Manager's OWN config-currency type for the autoscaler policy,
// kept distinct from the Engine's published AutoscalerPolicy because the two Mode
// enums disagree on zero value (façade Unknown=0 vs the Engine's own zero value, which
// IS Auto) — a config → contract builder input (same allowed-survivor class as
// slaTierFromString below), not an identity seam mirror to a not-yet-built collaborator.
//
// adapters.go keeps the REAL divergence bridges that remain:
//   - healthStatusFromRuntimeStatus — operatedsystemstate.RuntimeStatus (5 values) to
//     intervention.HealthStatus (4 values); genuinely different enums, one hop
//     (collapsing the former two-hop path through this package's own RuntimeStatusSeam).
//   - slaTierFromString — the Manager's raw string SLA-tier config (no typed config
//     source is wired yet) to intervention.SLATier.
//   - autoscalerPolicyToEngine (+ autoscalerModeToEngine) — bridges this package's own
//     façade autoscalerPolicy (Mode: AutoscalerMode, Unknown=0/Auto=1/Manual=2) onto the
//     autoscaler Engine's own published AutoscalerPolicy (Mode: Auto=0/Manual=1, no
//     Unknown) at the ProposeDesiredState call site; genuinely divergent VALUES, not
//     just names. The OperatedSystemView façade output reads wf.AutoscalerPolicy.Mode
//     straight through (no bridge needed on that side).
//   - autoscaleActionToState — autoscaler.DecisionKind straight to
//     operatedsystemstate.AutoscaleAction (collapsing the former two-hop path through
//     this package's own façade AutoscaleAction).
//   - infrastructureKindForEstimation — autoscaler.InfrastructureKind (this Manager's
//     canonical config currency, since autoscalerPolicy/DesiredState carry it too) to
//     operationestimation.InfrastructureKind; two independently generated enums that
//     happen to share values today.
//   - moneyFromEstimation / whatIfCurveFromEstimation / scalePointsToEstimation —
//     the façade's OWN generated Money/WhatIfCurve/WhatIfPoint/ScalePoint
//     (contract.gen.go) are genuinely distinct types from operationestimation's (the
//     façade's WhatIfPoint/ScalePoint carry Replicas int64; the engine's carry
//     LoadMultiplier float64 — a real unit divergence, not a rename).
//
// observedUsageFromEvents (workflow.go) is the real aggregation (Σ Units.Amount, count)
// that folds the usage RA's read range into the Engine's ObservedUsage input — kept
// alongside billing's foldRevenue/foldUsage precedent, not an identity mirror.
// ===========================================================================
