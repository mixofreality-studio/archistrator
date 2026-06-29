// Package operations is the operationsManager component of the archistrator
// server's Manager layer — the use-case façade that drives a delivered system
// through its operational life (UC4 "Operate a delivered system"), per the
// senior-frozen contract
// designs/aiarch/implementation/contracts/operationsManager.md (C-MOP).
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Schedule), it registers the operatedStateReconcile
// (30s) Schedule at startup, defines one Activity per ResourceAccess call, owns the
// Signal handler + the in-workflow primitives (awaitSignal / startTimer /
// registerSchedule), and derives the idempotency key "${workflowId}:${activityId}"
// passed down to each head-state RA write. Temporal lives ONLY in this component;
// the downstream Engines (interventionEngine, autoscalerEngine,
// operationEstimationEngine — pure, in-workflow, by value) and ResourceAccess ports
// (operatedSystemStateAccess, operatedRuntimeAccess, usageAccess, artifactAccess,
// durableExecutionAccess) import no Temporal.
//
// The FIVE frozen public ops (operationsManager.md §2):
//   - DeployAfterConstruction — Workflow (entry; operator deploy / scale / policy)
//   - ReconcileOperatedState  — Workflow (entry; Schedule-triggered observe+autoscale)
//   - WithdrawSystem          — Workflow (entry; terminal withdraw)
//   - QueryCostProjection     — Workflow (entry; short-lived read-only, no mutation)
//   - ApplyDelinquencyPolicy  — Signal (queued, cross-Manager from settlementManager)
//
// File layout (mirrors internal/manager/construction):
//   - operationsmanager.go : the Manager that translates public ops into Temporal client calls (§6.2)
//   - contract.go          : the public façade types (§3)
//   - deps.go              : the consumer-side dep interfaces + frozen-collaborator seams (§5)
//   - workflow.go          : the Workflows deps struct + workflow bodies + the Conflict loop (§6.3, §6.5)
//   - activities.go        : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - signals.go           : the queued delinquency Signal payload + enforcement branch (§6.3)
//   - errors.go            : the port-error -> Temporal-error mapping helper (§6.4)
//   - worker.go            : worker registration of workflows + activities + the Schedule (§6.1)
package operations

import (
	"github.com/google/uuid"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// ---------------------------------------------------------------------------
// Public data contracts (operationsManager.md §3) — the Client surface.
// Infrastructure-opaque: no Temporal id is exposed here. The operated-system
// head-state model and the Engine directive/projection types are referenced from
// their owning ResourceAccess / Engine packages (deps.go seams), not redefined
// (memory: Method data models live in their owning RA).
// ---------------------------------------------------------------------------

// operatedAppID is the operated-system aggregate identifier; a plain uuid.UUID,
// canonical in operatedSystemStateAccess (operationsManager.md §3.0 / OQ-3 →
// standardised on OperatedAppId, not deployedAppId).
type operatedAppID = uuid.UUID

// customerID is the billing-customer aggregate identifier; a plain uuid.UUID,
// canonical in settlementStateAccess (operationsManager.md §3.0).
type customerID = uuid.UUID

// DesiredStateReason discriminates the operator-chosen desired-state mutation
// (operationsManager.md §3.1). The reason is LOAD-BEARING: DeployAfterConstruction
// (2.1) accepts only {ReasonDeployAfterConstruction, ReasonOperator}; ReasonAutoscale
// is reserved for ReconcileOperatedState (2.2) Path C and ReasonDelinquency for
// ApplyDelinquencyPolicy (2.5). The reserved reasons are rejected on 2.1 as a
// ContractMisuse (§2.6 / OQ-5).

// ReasonUnknown is the zero value (rejected as ContractMisuse on 2.1).

// ReasonDeployAfterConstruction is the first take-live of a constructed bundle.

// ReasonOperator is a manual scale / autoscaler-policy change by the operator.

// ReasonAutoscale is INTERNAL — set by 2.2 Path C; rejected on 2.1.

// ReasonDelinquency is INTERNAL — set by 2.5; rejected on 2.1.

// PatchKind discriminates the shape of the operator-chosen desired-state patch
// (operationsManager.md §2.6 / §3.1). The one "republish desired state" facet
// carries a full bundle (deploy), a scale patch, or a policy patch — the factor-up
// of deploy / scale / updateAutoscalerPolicy.

// PatchKindUnknown is the zero value.

// PatchFullBundle is the first deploy of a constructed bundle (retrieved from
// artifactAccess).

// PatchScale is a manual replica/resource scale patch.

// PatchPolicy is an autoscaler-policy change patch.

// DesiredStateChange is the operator-chosen desired-state mutation passed to
// DeployAfterConstruction (2.1). A discriminated union (Reason + PatchKind) over the
// patch shapes the one "republish desired state" facet must carry (§3.1). The
// rendered desired-state bytes are infrastructure-opaque at this boundary — the
// Manager renders them into the operatedRuntimeAccess.DesiredState inside the
// workflow; the canonical shapes live in operatedSystemStateAccess / operatedRuntimeAccess.

// ChangeID is the operator-supplied continuity token; it keys the deploy
// workflow id {operatedAppId}:deploy:{changeId} (§6.1).

// RenderedDesiredState is the infrastructure-neutral rendered desired-state the
// Manager publishes (a full bundle, a scale patch, or a policy patch). Opaque at
// the contract boundary; the bytes are committed by operatedRuntimeAccess.

// WithdrawReason carries the operator's free-text withdrawal rationale
// (operationsManager.md §3.2). Opaque to the contract.

// ReconcileScope narrows which in-flight apps a reconcile tick observes
// (operationsManager.md §3.2). Empty AppIDs ⇒ all in-flight operated apps (the
// default schedule firing).

// ScalePoint is one hypothetical scale level for the what-if cost curve
// (operationsManager.md §3.2). Opaque to the Engine call.

// ScaleWhatIfPoints carries the hypothetical scale levels for the cost what-if
// (operationsManager.md §3.2).

// DeployResult is the result of DeployAfterConstruction (operationsManager.md §3.3).
// Published is true iff the desired state was durably published to the manifests
// repo. Revision is the published desired-state revision (for UI correlation; opaque).

// WithdrawResult is the result of WithdrawSystem (operationsManager.md §3.3).
// Withdrawn is true iff the withdrawal was durably recorded; an already-withdrawn
// app is an idempotent no-op success.

// ReconcileResult is the result of one ReconcileOperatedState tick
// (operationsManager.md §3.3). Observed counts the in-flight apps observed,
// Transitions the head-state transitions recorded (Path B), and Republished the
// autoscaler-driven republishes (Path C, non-NoChange).

// costProjection is the read-only op-time cost projection returned by
// QueryCostProjection (operationsManager.md §3.3 — CANONICAL in
// operationEstimationEngine.md §3). Mirrored as the Manager-local seam shape (deps.go
// costProjection); re-exported here as the façade result. NO state mutation produces
// it.
type costProjection = CostProjectionSeam

// ---------------------------------------------------------------------------
// OperatedSystemView — op 2.7 façade type (operationsRead-ruling.md §B). The
// composed, infrastructure-opaque operator display view returned by
// QueryOperatedSystemView. It REUSES the existing seam enums (RuntimeStatusSeam,
// AutoscalerMode, AutoscaleAction, Money) — no new domain types. The sub-types are
// Manager-local VIEW projections (like CostProjectionSeam): SloRowView /
// HealthSnapshotView mirror operatedRuntimeAccess's read outputs;
// AutoscaleDecisionView mirrors the autoscalerEngine decision shape. No Temporal id,
// no PromQL, no cluster lexeme appears here.
// ---------------------------------------------------------------------------

// OperatedSystemView is the composed operator display view (operationsRead-ruling.md
// §B). It fans the existing internal reads (head-state + observed health/SLO +
// autoscaler mode/decisions + cost run-rate) into one denormalized read view that
// exists nowhere as a stored row. MUTATING NO STATE produces it.

// the operated-system aggregate id
// RuntimePhase rollup (head-state Status)
// head-state InFlight
// observed health snapshot
// per-component SLO rows
// health/phase transition events (bounded, newest-first)
// decision history + mode
// operationEstimationEngine run-rate (no what-if)

// HealthSnapshotView mirrors the observed-health portion of operatedRuntimeAccess's
// read outputs (getApplicationHealth + getSloStatus).

// SloRowView mirrors one per-component SLO row (operatedRuntimeAccess SLO posture).

// e.g. "api", "worker"
// the SLO objective text ("99.9% availability")

// RuntimeStatusEventView mirrors one health/phase transition event (bounded,
// newest-first) the view surfaces from the head-state status history.

// AutoscalerView mirrors the autoscaler mode + decision history.

// Auto | Manual (existing seam)
// newest-first, bounded

// AutoscaleDecisionView mirrors one autoscaler decision (autoscalerEngine decision
// shape) for the operator history.

// NoChange | ScaleUp | ScaleDown | Pause | Resume (existing)
// the telemetry signal that drove it (why)
// did it publish a revised desired state, or no-op

// ---------------------------------------------------------------------------
// Façade error model (operationsManager.md §3.4).
// CALLER/PROGRAMMER errors at the façade boundary — distinct from the workflow's
// own failure handling (Temporal RetryPolicy + the intervention/autoscaler
// alternative paths inside the workflow body). Kinds used: ContractMisuse,
// FailedPrecondition, NotFound, Unauthorized, Infrastructure.
// ---------------------------------------------------------------------------

// operationsError is the typed façade error (operationsManager.md §3.4). It is an
// alias for fwmgr.Error so errors.As(&operationsError) call sites work.
type operationsError = fwmgr.Error

func newError(kind fwmgr.Kind, detail string) *fwmgr.Error {
	return fwmgr.New(kind, detail)
}
