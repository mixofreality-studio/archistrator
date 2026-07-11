package operations

import (
	"context"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
)

// This file declares the Manager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom). Per the senior hand-off, MOST of operationsManager's
// collaborators are not yet built as Go packages (their own C-* construction
// activities have not run), so this Manager is built against their FROZEN CONTRACTS
// as interfaces it declares here, and unit-tested with fakes:
//
//   - OperatedSystemStateAccess — operatedSystemStateAccess.md §2/§3 (FROZEN; not yet built)
//   - OperatedRuntimeAccess     — operatedRuntimeAccess.md §2/§3 (FROZEN; not yet built)
//   - UsageAccess               — usageAccess.md §2/§3 (FROZEN; not yet built)
//   - InterventionEngine        — interventionEngine.md §2.2 (FROZEN; not yet built)
//   - AutoscalerEngine          — autoscalerEngine.md §2.1 (FROZEN; not yet built)
//   - OperationEstimationEngine — operationEstimationEngine.md §2.2 (FROZEN; not yet built)
//
// The collaborators that DO exist as Go packages are consumed via narrow consumer
// interfaces declared here so the test fakes stay small:
//
//   - ArtifactAccess            — exists as internal/resourceaccess/artifact, BUT the
//     frozen retrieveDeployableBundle verb is NOT yet on the package (it currently
//     has RetrieveConstructionOutput). Consumed here via a NARROW seam interface
//     mirroring the frozen verb; the composition root adapts the concrete *artifact.Store
//     once that verb lands (escalation E-1 in C-MOP.md).
//   - DurableExecutionAccess    — exists as internal/resourceaccess/durableexecution;
//     only RegisterSchedule is a contract op this Manager calls (at startup). The
//     in-workflow primitives (awaitSignal / startTimer) are the Manager's OWN
//     workflow code (D-DA category A), NOT RA methods — they live in workflow.go /
//     operationsmanager.go.
//
// The data types each not-yet-built Engine/RA exchanges are declared here in the
// Manager-local SEAM form mirroring the frozen contract, suffixed "Seam" where the
// owning package will later own the canonical type. When the owner ships, these
// local mirrors are deleted and the import substituted; no public façade op changes
// (operationsManager.md OQ-3). This keeps the Method discipline "models live in
// their owning RA/Engine" intact.

// ===========================================================================
// operatedSystemStateAccess — reached ONLY through the generated typed invokers
// (invokers.gen.go), which carry the contract types (operatedsystemstate.OperatedSystem
// / OperatedSystemSummary / InFlightScope / Version / DelinquencyAction / RuntimeStatus)
// directly. Task 4 (seam-adapter cleanup) retired the Manager-local mirrors that used
// to duplicate these shapes 1:1 (operatedSystem, operatedSystemSummary, inFlightScope,
// delinquencyAction, version) — the workflow now speaks operatedsystemstate.* directly;
// no fold happens at the RA boundary. RuntimeStatusSeam (this package's OWN generated
// façade enum, contract.gen.go) is NOT one of those mirrors — it stays, because it is
// also the public OperatedSystemView/HealthSnapshotView field type and the
// interventionEngine healthChange field type (Task 5 scope); adapters.go keeps ONE
// surviving converter (runtimeStatusFromState) to bridge operatedsystemstate.RuntimeStatus
// into it at those two boundaries.
// ===========================================================================

// ===========================================================================
// operatedRuntimeAccess — reached ONLY through the generated typed invokers (see the
// operatedSystemStateAccess note above). Task 4 retired the Manager-local mirrors that
// duplicated its shapes 1:1 (runtimeDesiredState, sloStatusSeam, computeAttribution) —
// the workflow now speaks operatedruntime.RuntimeDesiredState / SloStatus /
// ComputeAttribution directly. computeUnitsSeam (below) is NOT retired: it is also the
// estimation Engine's input shape (usageEventSeam.Units, Task 5 scope).
// ===========================================================================

// computeUnitsSeam mirrors operatedRuntimeAccess.md / usageAccess.md §3 ComputeUnits
// — an infrastructure-neutral metered quantity (never priced, never a cloud lexeme).
// Kept (not retired in Task 4): it is also operationEstimationEngine's input shape via
// usageEventSeam.Units below (Task 5 scope — Engine mirror types are untouched here).
type computeUnitsSeam struct {
	Amount float64
	Unit   string
}

// ===========================================================================
// usageAccess — reached ONLY through the generated typed invokers (see the
// operatedSystemStateAccess note above). Task 4 retired usageRangeQuerySeam (an exact
// 1:1 mirror of usage.UsageRangeQuery with no other consumer) — the workflow now builds
// usage.UsageRangeQuery directly. usageEventSeam is NOT retired: despite its doc comment
// naming usageAccess.UsageEvent as the type it mirrors, its ACTUAL role today is
// operationEstimationEngine's input shape (observedUsage.Events below, Task 5 scope) —
// deliberately narrower than usage.UsageEvent (no RawMeter/window fields), so it is left
// untouched per the Task 4 brief's "if one is engine-side, leave for Task 5" rule.
// ===========================================================================

// usageEventSeam mirrors usageAccess.md §3 UsageEvent — one observed compute-usage
// fact carrying its runtime-supplied dedup id.
type usageEventSeam struct {
	OperatedAppID  operatedAppID
	CustomerID     customerID
	CycleID        string
	Units          computeUnitsSeam
	RuntimeEventID string
	ObservedAt     time.Time
}

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
// interventionEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors of
// the operate-time verb (interventionEngine.md §2.2 decideOnHealth). DECIDE →
// the Manager EXECUTES. Pure, deterministic, called DIRECTLY in-workflow (no
// Activity, no idempotency key, imports no Temporal).
// ===========================================================================

// interventionEngine mirrors interventionEngine.md §2.2 — the operate-time health
// intervention decision. The Engine DECIDES; the Manager EXECUTES. UNEXPORTED seam;
// the folded adapter bridges the published intervention.InterventionEngine to it
// (folding the policy into the published HealthChange.Policy).
type interventionEngine interface {
	DecideOnHealth(change healthChange, policy interventionPolicy) (healthDirective, error)
}

// healthChange mirrors interventionEngine.md §3 — the observed health/SLO transition.
type healthChange struct {
	AppID      operatedAppID
	FromStatus RuntimeStatusSeam
	ToStatus   RuntimeStatusSeam
	SloMet     bool
}

// interventionPolicy mirrors interventionEngine.md §3 — the committed intervention
// policy snapshot, fed BY VALUE. The casting RULE is package-internal to the Engine.
type interventionPolicy struct {
	RetryBudget int
	SLATier     string
}

// healthDirective mirrors interventionEngine.md §2.2/§3 — the Engine's decision
// {Retry | Escalate}.
type healthDirective int

const (
	// healthDirectiveUnknown is the zero value.
	healthDirectiveUnknown healthDirective = iota
	// healthDirectiveRetry: no human action — re-observe / let the runtime self-heal;
	// the Manager records the status change and re-publishes prior desired state.
	healthDirectiveRetry
	// healthDirectiveEscalate: page the operator — the Manager surfaces it.
	healthDirectiveEscalate
)

// ===========================================================================
// autoscalerEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors of
// proposeDesiredState (autoscalerEngine.md §2.1). Pure, deterministic, direct
// in-workflow. DECIDE → the Manager EXECUTES (renders revised manifests, publishes).
// ===========================================================================

// autoscalerEngine mirrors autoscalerEngine.md §2.1 — the autoscale decision. The
// Engine DECIDES; the Manager EXECUTES a republish on a non-NoChange decision.
// UNEXPORTED seam; the folded adapter bridges the published autoscaler.AutoscalerEngine.
type autoscalerEngine interface {
	ProposeDesiredState(telemetry telemetry, currentDesired autoscalerDesiredState, policy autoscalerPolicy, infrastructureKind infrastructureKind) (autoscaleDecisionSeam, error)
}

// telemetry mirrors autoscalerEngine.md §3 — the observed load snapshot the Manager
// assembles from the Path B reads.
type telemetry struct {
	RequestsPerSecond float64
	P95LatencyMs      float64
	CurrentReplicas   int
	CPUUtilization    float64
}

// autoscalerDesiredState mirrors autoscalerEngine.md §3 DesiredState — the current
// desired state the autoscaler compares against (Replicas=0 ⇒ paused).
type autoscalerDesiredState struct {
	InfrastructureKind infrastructureKind
	Replicas           int
}

// autoscalerPolicy mirrors autoscalerEngine.md §3 — the customer-tunable autoscaler
// policy (fed by value; the casting RULE is package-internal).
type autoscalerPolicy struct {
	Kind             infrastructureKind
	Mode             AutoscalerMode
	MinReplicas      int
	BaselineReplicas int
}

// AutoscalerMode mirrors autoscalerEngine.md §3 — Auto | Manual (manual ⇒ NoChange).

// AutoscalerModeUnknown is the zero value.

// AutoscalerModeAuto enables the decision.

// AutoscalerModeManual ⇒ the Engine always returns NoChange.

// infrastructureKind mirrors autoscalerEngine.md / operationEstimationEngine.md §3 —
// the opaque infrastructure discriminator (CustomerAppInfrastructure volatility).
type infrastructureKind int

const (
	// the zero value.
	_ infrastructureKind = iota
	// infrastructureKindGoTemporalPostgres is the launch infrastructure.
	infrastructureKindGoTemporalPostgres
)

// AutoscaleAction mirrors autoscalerEngine.md §3 Decision — the closed decision set.

// AutoscaleNoChange is the no-op decision (the common quiet-tick outcome).

// AutoscaleScaleUp increments replicas by Delta.

// AutoscaleScaleDown decrements replicas by Delta.

// AutoscalePause idle-pauses (publish replicas=0).

// AutoscaleResume resumes from zero to ToBaseline.

// autoscaleDecisionSeam mirrors autoscalerEngine.md §3 Decision — the sum-type the
// Engine returns. Delta is bounded by the policy on ScaleUp/ScaleDown; ToBaseline is
// the resume-from-zero target.
type autoscaleDecisionSeam struct {
	Action     AutoscaleAction
	Delta      int
	ToBaseline int
}

// ===========================================================================
// operationEstimationEngine — FROZEN, NOT YET BUILT. Consumer interface + local
// mirrors of projectForOperatedApp (operationEstimationEngine.md §2.2). Pure,
// deterministic, direct in-workflow (read-only path). DECIDE / project, no mutation.
// ===========================================================================

// operationEstimationEngine mirrors operationEstimationEngine.md §2.2 — the op-time
// read-side cost projection. Pure; no mutation. UNEXPORTED seam; the folded adapter
// bridges the published operationestimation.OperationEstimationEngine to it
// (aggregating the seam's usage events into the published ObservedUsage shape).
type operationEstimationEngine interface {
	ProjectForOperatedApp(observedUsage observedUsage, infrastructureKind infrastructureKind, scaleWhatIfPoints []ScalePoint) (CostProjectionSeam, error)
}

// observedUsage mirrors operationEstimationEngine.md §3 — the observed-usage snapshot
// the Manager populates from usageAccess.readRange(operatedAppId, lastCycle).
type observedUsage struct {
	Events []usageEventSeam
}

// Money mirrors operationEstimationEngine.md §3 Money — an infrastructure-neutral
// monetary amount (minor units + currency).

// WhatIfPoint mirrors operationEstimationEngine.md §3 — one projected cost point.

// WhatIfCurve mirrors operationEstimationEngine.md §3 — the projected-cost curve.

// CostProjectionSeam mirrors operationEstimationEngine.md §3 CostProjection — the
// op-time projection returned by QueryCostProjection (re-exported as the façade
// CostProjection in contract.go).
