package operations

import (
	"context"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
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
// operatedSystemStateAccess — FROZEN, NOT YET BUILT. Narrow consumer interface
// (the head-state read + the additive operate-transition write verbs) +
// Manager-local mirrors of its frozen types (operatedSystemStateAccess.md §2/§3).
// Each WRITE carries expectedVersion + idempotencyKey; a stale-version fwra.Conflict
// drives the §6.5 re-read→re-apply loop.
// ===========================================================================

// operatedSystemStateAccess mirrors operatedSystemStateAccess.md §2 — the
// operated-system head-state RA. Reads are pure; writes carry the version guard +
// dedup-first idempotency key. UNEXPORTED downstream seam (founder DI model
// 2026-06-28): the GENERATED NewOperationsManager takes the dep's PUBLISHED
// operatedsystemstate.OperatedSystemStateAccess; the folded adapter (adapters.go)
// bridges it to this seam.
type operatedSystemStateAccess interface {
	// ReadOperatedSystem returns the whole head-state (NotFound if no row).
	ReadOperatedSystem(ctx context.Context, operatedAppID operatedAppID) (operatedSystem, error)
	// ReadInFlightOperatedApps returns the in-flight operated apps for a scope
	// (empty AppIDs ⇒ all; a customer scope for the delinquency sweep).
	ReadInFlightOperatedApps(ctx context.Context, scope inFlightScope) ([]operatedSystemSummary, error)
	// PublishDesiredState records the head-state desired-state transition (additive).
	PublishDesiredState(ctx context.Context, operatedAppID operatedAppID, expectedVersion version, reason DesiredStateReason, decision *autoscaleDecisionSeam, idempotencyKey fwra.IdempotencyKey) (version, error)
	// RecordRuntimeStatusChange records an observed runtime-status transition.
	RecordRuntimeStatusChange(ctx context.Context, operatedAppID operatedAppID, expectedVersion version, status RuntimeStatusSeam, idempotencyKey fwra.IdempotencyKey) (version, error)
	// WithdrawSystem marks the operated system withdrawn (head-state terminal).
	WithdrawSystem(ctx context.Context, operatedAppID operatedAppID, expectedVersion version, idempotencyKey fwra.IdempotencyKey) (version, error)
	// RecordDelinquencyAction records a delinquency-handling action.
	RecordDelinquencyAction(ctx context.Context, operatedAppID operatedAppID, expectedVersion version, action delinquencyAction, idempotencyKey fwra.IdempotencyKey) (version, error)
}

// version is the operated-system optimistic-concurrency version
// (operatedSystemStateAccess.md §3). Mirrors the owning RA's version type.
type version uint64

// RuntimeStatusSeam mirrors operatedSystemStateAccess.md §3 RuntimeStatus enum
// (e.g. Pending | Healthy | Degraded | Withdrawn). Manager-local seam.

// RuntimeStatusUnknown is the zero value.

// RuntimeStatusPending is a freshly-published, not-yet-converged app.

// RuntimeStatusHealthy is a healthy app.

// RuntimeStatusDegraded is an unhealthy app.

// RuntimeStatusWithdrawn is a withdrawn app.

// delinquencyAction mirrors operatedSystemStateAccess.md §3 — the recorded
// delinquency-handling action.
type delinquencyAction int

const (
	// delinquencyActionUnknown is the zero value.
	delinquencyActionUnknown delinquencyAction = iota
	// delinquencyActionPaused records a pause (replicas=0) enforcement.
	delinquencyActionPaused
	// delinquencyActionWithdrawn records a withdraw enforcement.
	delinquencyActionWithdrawn
)

// operatedSystem mirrors operatedSystemStateAccess.md §3 — the head-state aggregate
// the workflow reads to carry expectedVersion forward and resolve the published
// desired state for a republish.
type operatedSystem struct {
	ID                  operatedAppID
	Version             version
	Status              RuntimeStatusSeam
	InFlight            bool
	DeployableBundleRef string // empty ⇒ no constructed output present (deploy pre-condition fails)
}

// operatedSystemSummary mirrors operatedSystemStateAccess.md §3 — one in-flight app
// in the reconcile-tick / delinquency-sweep cross-row read.
type operatedSystemSummary struct {
	ID      operatedAppID
	Version version
	Status  RuntimeStatusSeam
}

// inFlightScope is the consumer-side scope for ReadInFlightOperatedApps. Empty ⇒ all
// in-flight apps (the default reconcile tick); CustomerID set ⇒ the delinquent
// customer's apps (the delinquency sweep, operationsManager.md §2.5).
type inFlightScope struct {
	AppIDs     []operatedAppID
	CustomerID *customerID
}

// ===========================================================================
// operatedRuntimeAccess — FROZEN, NOT YET BUILT. Narrow consumer interface
// (operatedRuntimeAccess.md §2). Two writes (publishDesiredState / withdraw,
// git-content-idempotent — no version guard) + the observe reads (collapsed
// readRuntimeStatus + readComputeAttribution per the frozen §2.5 factoring; this
// Manager consumes them under the contract's per-verb names the contract §2.2 / §6.4
// table commits to: getApplicationHealth / getSloStatus / readComputeAttribution).
// ===========================================================================

// operatedRuntimeAccess mirrors operatedRuntimeAccess.md §2 — the GitOps/cluster/
// observability fronting. Writes return at durable acceptance (NOT convergence);
// reads observe infrastructure-driven convergence. UNEXPORTED seam; the folded
// adapter bridges the published operatedruntime.OperatedRuntimeAccess to it.
type operatedRuntimeAccess interface {
	// PublishDesiredState commits the rendered desired state (git commit). Idempotent
	// on content; the caller-supplied key lands in the commit message.
	PublishDesiredState(ctx context.Context, appID operatedAppID, desired runtimeDesiredState, idempotencyKey fwra.IdempotencyKey) error
	// Withdraw removes the desired state (ArgoCD prunes). NotFound ⇒ success.
	Withdraw(ctx context.Context, appID operatedAppID, idempotencyKey fwra.IdempotencyKey) error
	// GetApplicationHealth reads the observed health snapshot (pure).
	GetApplicationHealth(ctx context.Context, appID operatedAppID) (RuntimeStatusSeam, error)
	// GetSloStatus reads the observed SLO posture (pure).
	GetSloStatus(ctx context.Context, appID operatedAppID) (sloStatusSeam, error)
	// ReadComputeAttribution reads observed compute consumption over a window (pure).
	ReadComputeAttribution(ctx context.Context, appID operatedAppID, window attributionWindow) (computeAttribution, error)
}

// runtimeDesiredState mirrors operatedRuntimeAccess.md §3 DesiredState — the
// infrastructure-neutral rendered desired-state the Manager publishes. The bytes are
// opaque (replicas=0 etc. live inside them, not as contract fields).
type runtimeDesiredState struct {
	Bytes       []byte
	ContentType string
}

// sloStatusSeam mirrors the SLO-posture portion of operatedRuntimeAccess.md §3
// RuntimeStatus (the frozen contract collapses health + SLO into one RuntimeStatus;
// this Manager keeps the §6.4-table per-verb seam name for the SLO read).
type sloStatusSeam struct {
	SloMet bool
	Detail string
}

// attributionWindow mirrors operatedRuntimeAccess.md §3 — the closed time range to
// attribute consumption over (the reconcile-tick interval for Path B).
type attributionWindow struct {
	From time.Time
	To   time.Time
}

// computeAttribution mirrors operatedRuntimeAccess.md §3 — per-app infrastructure-
// neutral observed consumption (ComputeUnits + an opaque source meter id). The
// Manager forwards it to usageAccess.recordComputeUsage on the tick.
type computeAttribution struct {
	Units          computeUnitsSeam
	RuntimeEventID string // the runtime-supplied globally-unique dedup token for the usage append
}

// computeUnitsSeam mirrors operatedRuntimeAccess.md / usageAccess.md §3 ComputeUnits
// — an infrastructure-neutral metered quantity (never priced, never a cloud lexeme).
type computeUnitsSeam struct {
	Amount float64
	Unit   string
}

// ===========================================================================
// usageAccess — FROZEN, NOT YET BUILT. Narrow consumer interface (usageAccess.md
// §2). Two append-writes (recordComputeUsage / recordFinalUsage, dedup-id
// idempotent — NO Conflict, NO version guard) + one range-read.
// ===========================================================================

// usageAccess mirrors usageAccess.md §2 — the append-only compute-usage ledger.
// Writes are idempotent on event.RuntimeEventID (a duplicate is success, not an
// error); reads are pure. UNEXPORTED seam; the folded adapter bridges the published
// usagelog.UsageAccess to it (dropping the published []EntryRef return).
type usageAccess interface {
	// RecordComputeUsage appends observed compute-usage facts (per reconcile tick).
	RecordComputeUsage(ctx context.Context, events []usageEventSeam) error
	// RecordFinalUsage appends the final usage batch at withdraw.
	RecordFinalUsage(ctx context.Context, events []usageEventSeam) error
	// ReadRange replays the cycle's usage facts (the cost-projection read keys on
	// OperatedAppID + lastCycle).
	ReadRange(ctx context.Context, query usageRangeQuerySeam) ([]usageEventSeam, error)
}

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

// usageRangeQuerySeam mirrors usageAccess.md §3 UsageRangeQuery — the cycle-scope read
// query (OperatedAppID set ⇒ one app's facts, for the ncuc6 cost projection).
type usageRangeQuerySeam struct {
	CustomerID    customerID
	CycleID       string
	OperatedAppID *operatedAppID
}

// ===========================================================================
// artifactAccess — EXISTS as a Go package (internal/resourceaccess/artifact) but the
// frozen retrieveDeployableBundle verb is NOT yet on it (it has
// RetrieveConstructionOutput). Consumed here via a NARROW seam interface mirroring
// the frozen verb; the composition root adapts the concrete *artifact.Store once the
// verb lands (escalation E-1 in C-MOP.md). The bundle ref is a plain content
// address (a string), matching the package's content-address discipline.
// ===========================================================================

// artifactAccess is the Manager's consumer view of artifactAccess for the deploy
// path: retrieve the deployable bundle for a constructed app
// (operationsManager.md §6.4 RetrieveDeployableBundleActivity). UNEXPORTED seam; the
// folded adapter bridges the published artifact.ArtifactAccess (escalation E-1: over
// RetrieveConstructionOutput until the frozen retrieveDeployableBundle verb lands).
type artifactAccess interface {
	RetrieveDeployableBundle(ctx context.Context, deployableBundleRef string) (deployableBundle, error)
}

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
	// infrastructureKindUnknown is the zero value.
	infrastructureKindUnknown infrastructureKind = iota
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
