// Package durableexecution is the durableExecutionAccess component of the aiarch
// server's ResourceAccess layer — the INFRASTRUCTURE-OPAQUE port over the
// cross-execution CONTROL PLANE of the durable workflow-execution runtime
// (durableExecutionAccess.md). It is the only component permitted to call the
// durableExecutionRuntime Resource (architecture.dsl line 289).
//
// THE LOAD-BEARING LAYER RULE (durableExecutionAccess.md §1, §4 Non-goal #1;
// [[the-method-layers]] "Temporal mapping"): even though this RA fronts Temporal
// ITSELF, its PUBLIC surface imports NO Temporal and carries ZERO Temporal
// lexemes (Workflow, Activity, Signal-type, WorkflowID, RunID, TaskQueue, Worker,
// Namespace). The contract distinguishes two categories of Manager↔runtime
// interaction:
//
//   - CATEGORY A — in-workflow primitives (startTimer / awaitSignal / executeChild
//     / continueAsNew / query-self). These are deterministic Temporal Workflow SDK
//     calls the Manager makes from INSIDE its own workflow body. They are NOT ops
//     on this contract: routing them through an RA would force the RA to import the
//     Workflow SDK and run inside the replay context — the exact coupling the
//     layering directive forbids. They live in the Manager packages.
//   - CATEGORY B — cross-execution control-plane I/O (start-or-signal /
//     deliver-signal / register-schedule / query-another-execution). These are
//     ordinary RPC against the runtime's control plane, performed from OUTSIDE the
//     target execution's deterministic context. THESE FOUR are the contract ops
//     below, and the only thing this package exposes.
//
// Idempotency on the start verb is carried by a CALLER-SUPPLIED ExecutionID (the
// deterministic continuity token), never read from ambient Temporal context — the
// same move artifactAccess/projectStateAccess use with their caller-supplied
// idempotencyKey. The runtime is natively idempotent on that id.
//
// The concrete Temporal-backed implementation lives in temporal.go; it is the
// SOLE file in the corpus where this RA touches the Temporal SDK, and it never
// leaks a Temporal type back across the port.
package durableexecution

import (
	"context"
	"time"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// DurableExecutionAccess is the infrastructure-opaque control-plane port over the
// durable workflow-execution runtime (durableExecutionAccess.md §2). Four atomic
// cross-execution verbs, every one importing no Temporal:
//
//   - StartOrSignalExecution — the Client entry verb. Start a fresh execution by
//     deterministic ExecutionID, or (signal-with-start) deliver a signal into a
//     possibly-suspended execution of the same id. Returns once the start/signal
//     is DURABLY ACCEPTED, not once the execution completes (executions run
//     minutes-to-months). Native idempotency on ExecutionID: re-issuing with the
//     same id converges on the same handle — no duplicate execution.
//   - DeliverSignal — fire-and-forget cross-execution signal to another running
//     execution's signal channel (the one queued Manager→Manager edge). void
//     return; at-least-once to the channel (dedup is the caller's / receiving
//     handler's concern, NOT this contract's).
//   - RegisterSchedule — register a recurring schedule, idempotent on ScheduleID
//     (safe to run on every server boot / replica). Last-writer-wins on a changed
//     spec.
//   - QueryExecutionState — read-only, side-effect-free query of a running (or
//     recently-closed) execution's TECHNICAL progress. Never a business-state read
//     (that is projectStateAccess head-state).
type DurableExecutionAccess interface {
	StartOrSignalExecution(ctx context.Context, executionKind ExecutionKind, executionID ExecutionID, signalName SignalName, payload ExecutionPayload) (ExecutionHandle, error)
	DeliverSignal(ctx context.Context, targetExecutionID ExecutionID, signalName SignalName, payload ExecutionPayload) error
	RegisterSchedule(ctx context.Context, scheduleID ScheduleID, spec ScheduleSpec) error
	QueryExecutionState(ctx context.Context, executionID ExecutionID, queryName QueryName, args ExecutionPayload) (ExecutionStateView, error)
}

// ExecutionKind is a LOGICAL name for a kind of durable execution
// (durableExecutionAccess.md §3), e.g. "systemDesignPhase1",
// "settlementCycleClose", "operatedStateReconcile". The ExecutionKind → (infra
// workflow-type, task queue) mapping is owned INSIDE this package (registry.go);
// callers never name a Temporal workflow type or task queue.
type ExecutionKind string

// ExecutionID is the CALLER-SUPPLIED, deterministic continuity token
// (durableExecutionAccess.md §3; operational-concepts.md §5 "workflow id is the
// continuity token"), e.g. "{projectId}:{artifactKind}", "{projectId}:sdpReview",
// "closeSettlementCycle:{customerId}". The runtime is natively idempotent on it.
// Passing it in — rather than reading the runtime's ambient id — is what keeps
// this component Temporal-free.
type ExecutionID string

// SignalName is a LOGICAL signal-channel name (durableExecutionAccess.md §3),
// e.g. "reviewDecision", "applyDelinquencyPolicy".
type SignalName string

// QueryName is a LOGICAL query-handler name (durableExecutionAccess.md §3), e.g.
// "costProjection", "currentPhase".
type QueryName string

// ScheduleID is a STABLE recurring-schedule id (durableExecutionAccess.md §3),
// e.g. "shortfallSweep", "operatedStateReconcile". Stable across boots so
// startup-time registration is idempotent.
type ScheduleID string

// ExecutionPayload is an opaque serialised payload (durableExecutionAccess.md
// §3). The contract is a TRANSPORT, not a validator: payload semantics are the
// caller's and the receiving handler's responsibility.
type ExecutionPayload struct {
	// Bytes is the serialised payload; treated as opaque by this contract.
	Bytes []byte
	// ContentType is a best-effort serialisation hint (e.g. "application/json");
	// the contract does not validate it.
	ContentType string
}

// ExecutionHandle is an OPAQUE, immutable identity for one started/running
// execution (durableExecutionAccess.md §3). Callers compare by value and never
// parse it; a Manager that records an execution reference in a business event
// persists String(), never an infrastructure id.
//
// Infrastructure-opaque: today it wraps the runtime's (workflow-id, run-id) pair
// internally, never exposed as such.
type ExecutionHandle struct {
	opaque string
}

// String returns the canonical printable form (for logs, audit events).
func (h ExecutionHandle) String() string { return h.opaque }

// Equal reports value equality of two handles.
func (h ExecutionHandle) Equal(other ExecutionHandle) bool { return h.opaque == other.opaque }

// IsZero reports whether the handle is the zero value (no execution addressed).
func (h ExecutionHandle) IsZero() bool { return h.opaque == "" }

// ScheduleSpec describes a recurring schedule (durableExecutionAccess.md §3).
// Infrastructure-neutral: Cadence is a duration or a cron string, never a
// Temporal ScheduleSpec.
type ScheduleSpec struct {
	// ExecutionKind is what each firing starts.
	ExecutionKind ExecutionKind
	// Cadence is the infrastructure-neutral recurrence.
	Cadence Cadence
	// TargetIDTemplate is how each firing's ExecutionID is derived (advisory; the
	// runtime derives the firing-level id natively for exactly-once firing).
	TargetIDTemplate string
	// StartPayload is the opaque payload each firing's execution starts with.
	StartPayload ExecutionPayload
}

// Cadence is an infrastructure-neutral recurrence: a fixed interval OR a cron
// expression (durableExecutionAccess.md §3). Exactly one of Every / CronExpr is
// set; setting neither is a contract misuse.
type Cadence struct {
	// Every is a fixed-interval cadence; zero if CronExpr is used.
	Every time.Duration
	// CronExpr is an infrastructure-neutral cron expression; empty if Every is used.
	CronExpr string
}

// ExecutionStatus is the infrastructure-neutral TECHNICAL status of an execution
// (durableExecutionAccess.md §3). "running" and "suspended awaiting a signal" are
// deliberately collapsed into StatusRunning — suspend is a technical sub-state
// with no business consumer (the architect-review-gate UI reads the BUSINESS
// AwaitingReview head-state, not this technical view).
type ExecutionStatus int

const (
	// StatusUnknown is the zero value (status not determinable).
	StatusUnknown ExecutionStatus = iota
	// StatusRunning — in flight (possibly suspended awaiting a signal).
	StatusRunning
	// StatusCompleted — ran to completion (terminal).
	StatusCompleted
	// StatusFailed — terminated with failure (terminal).
	StatusFailed
	// StatusCancelled — cancelled/terminated by operator (terminal).
	StatusCancelled
	// StatusTimedOut — execution-level timeout (terminal).
	StatusTimedOut
)

var statusNames = map[ExecutionStatus]string{
	StatusUnknown: "Unknown", StatusRunning: "Running", StatusCompleted: "Completed",
	StatusFailed: "Failed", StatusCancelled: "Cancelled", StatusTimedOut: "TimedOut",
}

// String returns the stable name (logs, audit).
func (s ExecutionStatus) String() string {
	if n, ok := statusNames[s]; ok {
		return n
	}
	return "Unknown"
}

// ExecutionStateView is a point-in-time, infrastructure-neutral view of a running
// execution's TECHNICAL progress (durableExecutionAccess.md §3). It carries
// technical execution status, NOT business state (business current-state lives in
// projectStateAccess head-state).
type ExecutionStateView struct {
	// Handle is the execution this view describes.
	Handle ExecutionHandle
	// Status is the technical execution status.
	Status ExecutionStatus
	// QueryResult is the named query handler's serialised result; empty if the
	// query was not run (e.g. the execution is closed and the runtime returned no
	// query value). The caller deserialises it per the handler's contract.
	QueryResult []byte
	// StartedAt is when the execution started.
	StartedAt time.Time
	// ClosedAt is when the execution closed; nil while running.
	ClosedAt *time.Time
}

// Error is the shared ResourceAccess error model (framework-go), re-exported as
// an alias so this component's contract reads in its own terms while every RA
// component shares one fixed enum. Construct with fwra.New / fwra.Wrap using the
// shared kinds. The contract's logical error vocabulary maps onto the shared
// kinds as follows (durableExecutionAccess.md §3 DurableExecutionError):
//
//   - ErrTransient        → fwra.Transient        (retryable: gRPC blip / 5xx)
//   - ErrNotFound         → fwra.NotFound         (terminal: no execution with id)
//   - ErrUnknownKind      → fwra.ContractMisuse   (terminal: kind/signal not registered)
//   - ErrQueryRejected    → fwra.ContentPolicy    (terminal: handler rejected query)
//   - ErrInfrastructure   → fwra.Infrastructure   (escalate)
//   - ErrContractMisuse   → fwra.ContractMisuse   (terminal: caller pre-condition)
//
// ErrAlreadyExists is informational only: StartOrSignalExecution maps the
// already-exists case to SUCCESS (returns the existing handle) and never surfaces
// it.
type Error = fwra.Error
