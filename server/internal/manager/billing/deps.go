package billing

import (
	"context"
	"time"
)

// This file declares billingManager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom) for the two collaborators still reached through a
// Manager-local seam, plus the seam data types they carry:
//
//   - RevenueLedgerAccess — not a Go package to build against; charge-only removed the
//     revenue-ledger component from the design entirely (slot 5 has no revenue-ledger
//     RA). The interface and its data mirrors (entryRefSeam, revenueEntrySeam,
//     reversalEntrySeam) stay only so the close/recompute workflow spine keeps
//     compiling; the composition root wires them to a permanent no-op adapter
//     (adapters.go noopRevenueLedger — see its TODO for the follow-up to excise the
//     spine outright).
//   - DurableExecutionAccess — exists as internal/resourceaccess/durableexecution;
//     consumed here via a NARROW seam interface (RegisterSchedule only, for the
//     startup shortfallSweep registration). The composition root adapts the concrete
//     *durableexecution.Runtime (durableAdapter, adapters.go).
//
// Every other collaborator — billingStateAccess, usageAccess, merchantGatewayAccess,
// billingengine.BillingEngine, intervention.InterventionEngine — is reached directly
// through the generated typed invokers/Activities and their published contracts
// (workflow.go); no Manager-local seam or data mirror remains for any of them (see the
// per-collaborator notes below). The in-workflow awaitSignal primitive (the
// inbound/reversal/chargeback waits) is the Manager's OWN workflow code (D-DA category
// A), NOT an RA method.

// ===========================================================================
// billingStateAccess — the billing/customer head-state RA. Each WRITE carries
// expectedVersion + idempotencyKey; a stale-version fwra.Conflict drives the §6.5
// re-read→re-apply loop. Keyed on CustomerID per §3.0.
//
// The consumer-seam interface AND its local data mirrors are retired — the workflow
// reaches this RA through the generated typed invokers (invokers.gen.go) and speaks
// the generated billingstate contract types (Billing, BillingTerms, BillingOutcome,
// RoutingDirective, CustomerSummary, DelinquencyScope, GatewayBinding, Version)
// directly, with no Manager-local wrapper. The empty CustomerProfile opened at
// registration is built inline in the workflow (billingstate.CustomerProfile{}).
// ===========================================================================

// ===========================================================================
// revenueLedgerAccess — FROZEN, NOT YET BUILT. Narrow consumer interface
// (revenueLedgerAccess.md §2). Two append-writes (recordInboundRevenue /
// recordReversal, dedup on the GATEWAY EVENT ID — NO Conflict, NO version guard) +
// one range-read. Keyed on CustomerID per §3.0.
// ===========================================================================

// RevenueLedgerAccess mirrors revenueLedgerAccess.md §2 — the append-only Revenue
// Ledger. Writes are idempotent on entry.GatewayEventID (a duplicate is success, not
// an error); reads are pure. There is NO Conflict kind on this contract.
type revenueLedgerAccess interface {
	// RecordInboundRevenue appends an inbound revenue fact (dedup on GatewayEventID).
	RecordInboundRevenue(ctx context.Context, entry revenueEntrySeam) (entryRefSeam, error)
	// RecordReversal appends a reversal/chargeback fact (dedup on GatewayEventID).
	RecordReversal(ctx context.Context, reversal reversalEntrySeam) (entryRefSeam, error)
	// ReadRange replays the cycle's revenue facts (inbound + reversals, append order).
	ReadRange(ctx context.Context, customerID customerID, cycleID cycleID) ([]revenueEntrySeam, error)
}

// EntryRefSeam mirrors revenueLedgerAccess.md §3 EntryRef — an opaque ref to a
// recorded ledger entry.
type entryRefSeam string

// RevenueKindSeam mirrors revenueLedgerAccess.md §3 RevenueKind.
type revenueKindSeam int

const (
	// RevenueKindInbound is an end-user payment collected via the gateway.
	revenueKindInbound revenueKindSeam = iota
	// RevenueKindReversal is a chargeback/dispute reversal of a prior inbound fact.
	revenueKindReversal
)

// RevenueEntrySeam mirrors revenueLedgerAccess.md §3 RevenueEntry — one immutable
// revenue fact (the recordInboundRevenue payload and the readRange element type).
type revenueEntrySeam struct {
	CustomerID     customerID
	CycleID        cycleID
	Kind           revenueKindSeam
	Amount         Money // signed minor units + currency (exact; never a float)
	GatewayEventID string
	OccurredAt     time.Time
}

// ReversalEntrySeam mirrors revenueLedgerAccess.md §3 ReversalEntry — the
// recordReversal payload (negative Amount + optional back-link).
type reversalEntrySeam struct {
	CustomerID             customerID
	CycleID                cycleID
	Amount                 Money // negative minor units + currency
	GatewayEventID         string
	ReversesGatewayEventID string // optional back-link; empty if absent
	OccurredAt             time.Time
}

// ===========================================================================
// usageAccess — FROZEN. This Manager only READS (the cycle fold at close; OperatedAppID
// nil = whole cycle). Reached through the generated invoker (UsageReadRange); the
// workflow builds usage.UsageRangeQuery directly and sums the returned events. The
// former consumer-seam interface + its usageEventSeam/computeUnitsSeam/usageRangeQuerySeam
// mirrors are retired (nothing else spoke them). The append-writes
// (recordComputeUsage / recordFinalUsage) belong to operationsManager, NOT billing.
// ===========================================================================

// ===========================================================================
// merchantGatewayAccess — D-MA. Reached through the generated CALLER-KEYED invokers
// (merchantGatewayAccess.*): the workflow supplies the business-stable Stripe
// Idempotency-Key EXPLICITLY (gatewayIdempotencyKey settle:{customerId}:{cycleId} for
// money-moves; onboard:{id} / validate:{id} for the ad-hoc auths) as BOTH the caller key
// (fwra.Context.IdempotencyKey) and the contract's own idempotencyKey param. The former
// consumer-seam interface is retired; the Money value type is converted inline at the
// invoker boundary (merchantgateway.Money).
// ===========================================================================

// ===========================================================================
// durableExecutionAccess — EXISTS (internal/resourceaccess/durableexecution). The two
// category-B control-plane verbs this Manager calls: deliverSignal (the one queued
// cross-Manager applyDelinquencyPolicy edge) + registerSchedule (×2). Consumed via a
// narrow seam interface so the composition root adapts the concrete *durableexecution.
// Runtime (whose RegisterSchedule / DeliverSignal signatures differ). awaitSignal (the
// inbound/reversal/chargeback waits) is the Manager's OWN workflow code (D-DA category
// A), NOT an RA method.
// ===========================================================================

// DurableExecutionAccess is the Manager's consumer view for the STARTUP Schedule
// registration only. The workflow-invoked category-B verbs — deliverSignal (the queued
// applyDelinquencyPolicy → operationsManager edge) and the per-customer registerSchedule
// (op 2.1) — are reached through the generated invokers now; only the startup
// shortfallSweep registration (RegisterSchedules) still goes through this seam +
// durableAdapter (adapters.go).
type durableExecutionAccess interface {
	// RegisterSchedule registers (idempotently, by id) a recurring Schedule.
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// deliverSignalPayload mirrors the applyDelinquencyPolicy payload delivered to
// operationsManager (the receiving handler dedups; D-DA §9 OQ3). The composition root
// adapts it onto durableexecution.ExecutionPayload.
type deliverSignalPayload struct {
	CustomerID       customerID
	PauseNotWithdraw bool
}

// scheduleSpec mirrors durableexecution.ScheduleSpec for the two Schedules this Manager
// registers. The composition root adapts the concrete RA.
type scheduleSpec struct {
	ID           string
	WorkflowType string
	TaskQueue    string
	IntervalSecs int
}

// ===========================================================================
// billingEngine / interventionEngine — RETIRED. The consumer-seam interfaces AND
// their local data mirrors (RoutingDirectiveSeam, CycleRevenueSeam, CycleUsageSeam,
// BillingResultSeam, ReBillingInputSeam, BillingFailureSeam, BillingFailureKindSeam,
// BillingFailureDirectiveSeam) are retired — the workflow reaches both Engines through
// their PUBLISHED contracts (billingengine.BillingEngine / intervention.
// InterventionEngine, each component's contract.gen.go), called DIRECTLY in-workflow
// by value (no Activity, no idempotency key, imports no Temporal), with
// fweng.Context{Context: context.Background()} supplied inline at each call site
// (workflow.go). adapters.go keeps the two REAL divergence bridges (termsToEngine,
// routingDirectiveToState) — the engine-owned BillingTerms/RoutingDirective are
// distinct named types from their billingstate counterparts.
// ===========================================================================
