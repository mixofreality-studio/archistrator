package operations

import (
	"context"
	"time"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/artifact"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
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

// OperatedSystemStateAccess mirrors operatedSystemStateAccess.md §2 — the
// operated-system head-state RA. Reads are pure; writes carry the version guard +
// dedup-first idempotency key.
type OperatedSystemStateAccess interface {
	// ReadOperatedSystem returns the whole head-state (NotFound if no row).
	ReadOperatedSystem(ctx context.Context, operatedAppID OperatedAppID) (OperatedSystem, error)
	// ReadInFlightOperatedApps returns the in-flight operated apps for a scope
	// (empty AppIDs ⇒ all; a customer scope for the delinquency sweep).
	ReadInFlightOperatedApps(ctx context.Context, scope InFlightScope) ([]OperatedSystemSummary, error)
	// PublishDesiredState records the head-state desired-state transition (additive).
	PublishDesiredState(ctx context.Context, operatedAppID OperatedAppID, expectedVersion Version, reason DesiredStateReason, decision *AutoscaleDecisionSeam, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// RecordRuntimeStatusChange records an observed runtime-status transition.
	RecordRuntimeStatusChange(ctx context.Context, operatedAppID OperatedAppID, expectedVersion Version, status RuntimeStatusSeam, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// WithdrawSystem marks the operated system withdrawn (head-state terminal).
	WithdrawSystem(ctx context.Context, operatedAppID OperatedAppID, expectedVersion Version, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// RecordDelinquencyAction records a delinquency-handling action.
	RecordDelinquencyAction(ctx context.Context, operatedAppID OperatedAppID, expectedVersion Version, action DelinquencyAction, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

// Version is the operated-system optimistic-concurrency version
// (operatedSystemStateAccess.md §3). Mirrors the owning RA's Version type.
type Version uint64

// RuntimeStatusSeam mirrors operatedSystemStateAccess.md §3 RuntimeStatus enum
// (e.g. Pending | Healthy | Degraded | Withdrawn). Manager-local seam.
type RuntimeStatusSeam int

const (
	// RuntimeStatusUnknown is the zero value.
	RuntimeStatusUnknown RuntimeStatusSeam = iota
	// RuntimeStatusPending is a freshly-published, not-yet-converged app.
	RuntimeStatusPending
	// RuntimeStatusHealthy is a healthy app.
	RuntimeStatusHealthy
	// RuntimeStatusDegraded is an unhealthy app.
	RuntimeStatusDegraded
	// RuntimeStatusWithdrawn is a withdrawn app.
	RuntimeStatusWithdrawn
)

// DelinquencyAction mirrors operatedSystemStateAccess.md §3 — the recorded
// delinquency-handling action.
type DelinquencyAction int

const (
	// DelinquencyActionUnknown is the zero value.
	DelinquencyActionUnknown DelinquencyAction = iota
	// DelinquencyActionPaused records a pause (replicas=0) enforcement.
	DelinquencyActionPaused
	// DelinquencyActionWithdrawn records a withdraw enforcement.
	DelinquencyActionWithdrawn
)

// OperatedSystem mirrors operatedSystemStateAccess.md §3 — the head-state aggregate
// the workflow reads to carry expectedVersion forward and resolve the published
// desired state for a republish.
type OperatedSystem struct {
	ID                  OperatedAppID
	Version             Version
	Status              RuntimeStatusSeam
	InFlight            bool
	DeployableBundleRef string // empty ⇒ no constructed output present (deploy pre-condition fails)
}

// OperatedSystemSummary mirrors operatedSystemStateAccess.md §3 — one in-flight app
// in the reconcile-tick / delinquency-sweep cross-row read.
type OperatedSystemSummary struct {
	ID      OperatedAppID
	Version Version
	Status  RuntimeStatusSeam
}

// InFlightScope is the consumer-side scope for ReadInFlightOperatedApps. Empty ⇒ all
// in-flight apps (the default reconcile tick); CustomerID set ⇒ the delinquent
// customer's apps (the delinquency sweep, operationsManager.md §2.5).
type InFlightScope struct {
	AppIDs     []OperatedAppID
	CustomerID *CustomerID
}

// ===========================================================================
// operatedRuntimeAccess — FROZEN, NOT YET BUILT. Narrow consumer interface
// (operatedRuntimeAccess.md §2). Two writes (publishDesiredState / withdraw,
// git-content-idempotent — no version guard) + the observe reads (collapsed
// readRuntimeStatus + readComputeAttribution per the frozen §2.5 factoring; this
// Manager consumes them under the contract's per-verb names the contract §2.2 / §6.4
// table commits to: getApplicationHealth / getSloStatus / readComputeAttribution).
// ===========================================================================

// OperatedRuntimeAccess mirrors operatedRuntimeAccess.md §2 — the GitOps/cluster/
// observability fronting. Writes return at durable acceptance (NOT convergence);
// reads observe infrastructure-driven convergence.
type OperatedRuntimeAccess interface {
	// PublishDesiredState commits the rendered desired state (git commit). Idempotent
	// on content; the caller-supplied key lands in the commit message.
	PublishDesiredState(ctx context.Context, appID OperatedAppID, desired RuntimeDesiredState, idempotencyKey fwra.IdempotencyKey) error
	// Withdraw removes the desired state (ArgoCD prunes). NotFound ⇒ success.
	Withdraw(ctx context.Context, appID OperatedAppID, idempotencyKey fwra.IdempotencyKey) error
	// GetApplicationHealth reads the observed health snapshot (pure).
	GetApplicationHealth(ctx context.Context, appID OperatedAppID) (RuntimeStatusSeam, error)
	// GetSloStatus reads the observed SLO posture (pure).
	GetSloStatus(ctx context.Context, appID OperatedAppID) (SloStatusSeam, error)
	// ReadComputeAttribution reads observed compute consumption over a window (pure).
	ReadComputeAttribution(ctx context.Context, appID OperatedAppID, window AttributionWindow) (ComputeAttribution, error)
}

// RuntimeDesiredState mirrors operatedRuntimeAccess.md §3 DesiredState — the
// infrastructure-neutral rendered desired-state the Manager publishes. The bytes are
// opaque (replicas=0 etc. live inside them, not as contract fields).
type RuntimeDesiredState struct {
	Bytes       []byte
	ContentType string
}

// SloStatusSeam mirrors the SLO-posture portion of operatedRuntimeAccess.md §3
// RuntimeStatus (the frozen contract collapses health + SLO into one RuntimeStatus;
// this Manager keeps the §6.4-table per-verb seam name for the SLO read).
type SloStatusSeam struct {
	SloMet bool
	Detail string
}

// AttributionWindow mirrors operatedRuntimeAccess.md §3 — the closed time range to
// attribute consumption over (the reconcile-tick interval for Path B).
type AttributionWindow struct {
	From time.Time
	To   time.Time
}

// ComputeAttribution mirrors operatedRuntimeAccess.md §3 — per-app infrastructure-
// neutral observed consumption (ComputeUnits + an opaque source meter id). The
// Manager forwards it to usageAccess.recordComputeUsage on the tick.
type ComputeAttribution struct {
	Units          ComputeUnitsSeam
	RuntimeEventID string // the runtime-supplied globally-unique dedup token for the usage append
}

// ComputeUnitsSeam mirrors operatedRuntimeAccess.md / usageAccess.md §3 ComputeUnits
// — an infrastructure-neutral metered quantity (never priced, never a cloud lexeme).
type ComputeUnitsSeam struct {
	Amount float64
	Unit   string
}

// ===========================================================================
// usageAccess — FROZEN, NOT YET BUILT. Narrow consumer interface (usageAccess.md
// §2). Two append-writes (recordComputeUsage / recordFinalUsage, dedup-id
// idempotent — NO Conflict, NO version guard) + one range-read.
// ===========================================================================

// UsageAccess mirrors usageAccess.md §2 — the append-only compute-usage ledger.
// Writes are idempotent on event.RuntimeEventID (a duplicate is success, not an
// error); reads are pure.
type UsageAccess interface {
	// RecordComputeUsage appends observed compute-usage facts (per reconcile tick).
	RecordComputeUsage(ctx context.Context, events []UsageEventSeam) error
	// RecordFinalUsage appends the final usage batch at withdraw.
	RecordFinalUsage(ctx context.Context, events []UsageEventSeam) error
	// ReadRange replays the cycle's usage facts (the cost-projection read keys on
	// OperatedAppID + lastCycle).
	ReadRange(ctx context.Context, query UsageRangeQuerySeam) ([]UsageEventSeam, error)
}

// UsageEventSeam mirrors usageAccess.md §3 UsageEvent — one observed compute-usage
// fact carrying its runtime-supplied dedup id.
type UsageEventSeam struct {
	OperatedAppID  OperatedAppID
	CustomerID     CustomerID
	CycleID        string
	Units          ComputeUnitsSeam
	RuntimeEventID string
	ObservedAt     time.Time
}

// UsageRangeQuerySeam mirrors usageAccess.md §3 UsageRangeQuery — the cycle-scope read
// query (OperatedAppID set ⇒ one app's facts, for the ncuc6 cost projection).
type UsageRangeQuerySeam struct {
	CustomerID    CustomerID
	CycleID       string
	OperatedAppID *OperatedAppID
}

// ===========================================================================
// artifactAccess — EXISTS as a Go package (internal/resourceaccess/artifact) but the
// frozen retrieveDeployableBundle verb is NOT yet on it (it has
// RetrieveConstructionOutput). Consumed here via a NARROW seam interface mirroring
// the frozen verb; the composition root adapts the concrete *artifact.Store once the
// verb lands (escalation E-1 in C-MOP.md). The bundle ref is a plain content
// address (a string), matching the package's content-address discipline.
// ===========================================================================

// ArtifactAccess is the Manager's consumer view of artifactAccess for the deploy
// path: retrieve the deployable bundle for a constructed app
// (operationsManager.md §6.4 RetrieveDeployableBundleActivity).
type ArtifactAccess interface {
	RetrieveDeployableBundle(ctx context.Context, deployableBundleRef string) (DeployableBundle, error)
}

// DeployableBundle mirrors the constructed-output bundle retrieved for a first
// deploy. Re-uses the existing artifact.ConstructionOutput shape as the bundle body
// (the deployable bundle IS a construction output — artifactAccess.md), kept as a
// thin Manager-local wrapper so the seam stays narrow.
type DeployableBundle struct {
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

// DurableExecutionAccess is the Manager's consumer view: the one startup op.
type DurableExecutionAccess interface {
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

// InterventionEngine mirrors interventionEngine.md §2.2 — the operate-time health
// intervention decision. The Engine DECIDES; the Manager EXECUTES.
type InterventionEngine interface {
	DecideOnHealth(change HealthChange, policy InterventionPolicy) (HealthDirective, error)
}

// HealthChange mirrors interventionEngine.md §3 — the observed health/SLO transition.
type HealthChange struct {
	AppID      OperatedAppID
	FromStatus RuntimeStatusSeam
	ToStatus   RuntimeStatusSeam
	SloMet     bool
}

// InterventionPolicy mirrors interventionEngine.md §3 — the committed intervention
// policy snapshot, fed BY VALUE. The casting RULE is package-internal to the Engine.
type InterventionPolicy struct {
	RetryBudget int
	SLATier     string
}

// HealthDirective mirrors interventionEngine.md §2.2/§3 — the Engine's decision
// {Retry | Escalate}.
type HealthDirective int

const (
	// HealthDirectiveUnknown is the zero value.
	HealthDirectiveUnknown HealthDirective = iota
	// HealthDirectiveRetry: no human action — re-observe / let the runtime self-heal;
	// the Manager records the status change and re-publishes prior desired state.
	HealthDirectiveRetry
	// HealthDirectiveEscalate: page the operator — the Manager surfaces it.
	HealthDirectiveEscalate
)

// ===========================================================================
// autoscalerEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors of
// proposeDesiredState (autoscalerEngine.md §2.1). Pure, deterministic, direct
// in-workflow. DECIDE → the Manager EXECUTES (renders revised manifests, publishes).
// ===========================================================================

// AutoscalerEngine mirrors autoscalerEngine.md §2.1 — the autoscale decision. The
// Engine DECIDES; the Manager EXECUTES a republish on a non-NoChange decision.
type AutoscalerEngine interface {
	ProposeDesiredState(telemetry Telemetry, currentDesired AutoscalerDesiredState, policy AutoscalerPolicy, infrastructureKind InfrastructureKind) (AutoscaleDecisionSeam, error)
}

// Telemetry mirrors autoscalerEngine.md §3 — the observed load snapshot the Manager
// assembles from the Path B reads.
type Telemetry struct {
	RequestsPerSecond float64
	P95LatencyMs      float64
	CurrentReplicas   int
	CPUUtilization    float64
}

// AutoscalerDesiredState mirrors autoscalerEngine.md §3 DesiredState — the current
// desired state the autoscaler compares against (Replicas=0 ⇒ paused).
type AutoscalerDesiredState struct {
	InfrastructureKind InfrastructureKind
	Replicas           int
}

// AutoscalerPolicy mirrors autoscalerEngine.md §3 — the customer-tunable autoscaler
// policy (fed by value; the casting RULE is package-internal).
type AutoscalerPolicy struct {
	Kind             InfrastructureKind
	Mode             AutoscalerMode
	MinReplicas      int
	BaselineReplicas int
}

// AutoscalerMode mirrors autoscalerEngine.md §3 — Auto | Manual (manual ⇒ NoChange).
type AutoscalerMode int

const (
	// AutoscalerModeUnknown is the zero value.
	AutoscalerModeUnknown AutoscalerMode = iota
	// AutoscalerModeAuto enables the decision.
	AutoscalerModeAuto
	// AutoscalerModeManual ⇒ the Engine always returns NoChange.
	AutoscalerModeManual
)

// InfrastructureKind mirrors autoscalerEngine.md / operationEstimationEngine.md §3 —
// the opaque infrastructure discriminator (CustomerAppInfrastructure volatility).
type InfrastructureKind int

const (
	// InfrastructureKindUnknown is the zero value.
	InfrastructureKindUnknown InfrastructureKind = iota
	// InfrastructureKindGoTemporalPostgres is the launch infrastructure.
	InfrastructureKindGoTemporalPostgres
)

// AutoscaleAction mirrors autoscalerEngine.md §3 Decision — the closed decision set.
type AutoscaleAction int

const (
	// AutoscaleNoChange is the no-op decision (the common quiet-tick outcome).
	AutoscaleNoChange AutoscaleAction = iota
	// AutoscaleScaleUp increments replicas by Delta.
	AutoscaleScaleUp
	// AutoscaleScaleDown decrements replicas by Delta.
	AutoscaleScaleDown
	// AutoscalePause idle-pauses (publish replicas=0).
	AutoscalePause
	// AutoscaleResume resumes from zero to ToBaseline.
	AutoscaleResume
)

// String returns the canonical name for an autoscale action.
func (a AutoscaleAction) String() string {
	switch a {
	case AutoscaleNoChange:
		return "NoChange"
	case AutoscaleScaleUp:
		return "ScaleUp"
	case AutoscaleScaleDown:
		return "ScaleDown"
	case AutoscalePause:
		return "Pause"
	case AutoscaleResume:
		return "Resume"
	default:
		return "Unknown"
	}
}

// AutoscaleDecisionSeam mirrors autoscalerEngine.md §3 Decision — the sum-type the
// Engine returns. Delta is bounded by the policy on ScaleUp/ScaleDown; ToBaseline is
// the resume-from-zero target.
type AutoscaleDecisionSeam struct {
	Action     AutoscaleAction
	Delta      int
	ToBaseline int
}

// ===========================================================================
// operationEstimationEngine — FROZEN, NOT YET BUILT. Consumer interface + local
// mirrors of projectForOperatedApp (operationEstimationEngine.md §2.2). Pure,
// deterministic, direct in-workflow (read-only path). DECIDE / project, no mutation.
// ===========================================================================

// OperationEstimationEngine mirrors operationEstimationEngine.md §2.2 — the op-time
// read-side cost projection. Pure; no mutation.
type OperationEstimationEngine interface {
	ProjectForOperatedApp(observedUsage ObservedUsage, infrastructureKind InfrastructureKind, scaleWhatIfPoints []ScalePoint) (CostProjectionSeam, error)
}

// ObservedUsage mirrors operationEstimationEngine.md §3 — the observed-usage snapshot
// the Manager populates from usageAccess.readRange(operatedAppId, lastCycle).
type ObservedUsage struct {
	Events []UsageEventSeam
}

// Money mirrors operationEstimationEngine.md §3 Money — an infrastructure-neutral
// monetary amount (minor units + currency).
type Money struct {
	MinorUnits int64
	Currency   string
}

// WhatIfPoint mirrors operationEstimationEngine.md §3 — one projected cost point.
type WhatIfPoint struct {
	Replicas             int
	ProjectedMonthlyCost Money
}

// WhatIfCurve mirrors operationEstimationEngine.md §3 — the projected-cost curve.
type WhatIfCurve struct {
	Points []WhatIfPoint
}

// CostProjectionSeam mirrors operationEstimationEngine.md §3 CostProjection — the
// op-time projection returned by QueryCostProjection (re-exported as the façade
// CostProjection in contract.go).
type CostProjectionSeam struct {
	CurrentRunRate       Money
	ProjectedMonthlyCost Money
	ScaleWhatIfCurve     WhatIfCurve
}
