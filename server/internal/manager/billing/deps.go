package billing

import (
	"context"
	"time"
)

// This file declares billingManager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom). Per the senior hand-off, NONE of billingManager's
// collaborators is yet built as a Go package in this module, so this Manager is built
// against their FROZEN CONTRACTS as interfaces it declares here, and unit-tested with
// hand-written fakes:
//
//   - BillingStateAccess  — billingStateAccess.md §2/§3 (design-only; FU-MST-1 id migration)
//   - RevenueLedgerAccess    — revenueLedgerAccess.md §2/§3 (FROZEN; not yet built)
//   - UsageAccess            — usageAccess.md §2/§3 (FROZEN; not yet built)
//   - MerchantGatewayAccess  — merchantGatewayAccess (D-MA — NOT YET CONTRACTED; FU-MST-2/OQ-2)
//   - BillingEngine       — billingEngine.md §2.1/§2.2 (FROZEN; not yet built)
//   - InterventionEngine     — interventionEngine.md §2.3 (FROZEN; not yet built)
//   - DurableExecutionAccess — exists as internal/resourceaccess/durableexecution, but
//     consumed via a NARROW seam interface (deliverSignal + registerSchedule) so the
//     composition root adapts the concrete *durableexecution.Runtime. The in-workflow
//     awaitSignal primitive (the inbound/reversal/chargeback waits) is the Manager's
//     OWN workflow code (D-DA category A), NOT an RA method.
//
// The data types each not-yet-built Engine/RA exchanges are declared here in the
// Manager-local SEAM form mirroring the frozen contract, suffixed "Seam" where the
// owning package will later own the canonical type. When the owner ships, these local
// mirrors are deleted and the import substituted; no public façade op changes
// (billingManager.md OQ-7). This keeps the Method discipline "models live in their
// owning RA/Engine" intact.
//
// §3.0 IDENTITY: every collaborator below keys on CustomerID = uuid.UUID. We do NOT
// reintroduce BillingID(string) (the §3.0 ruling); billingStateAccess is
// consumed here ALREADY MIGRATED (the FU-MST-1 shape), which the composition root will
// satisfy once that RA is built.

// ===========================================================================
// billingStateAccess — DESIGN-ONLY (FU-MST-1: id-type migrated to CustomerID).
// The billing/customer head-state RA. Each WRITE carries expectedVersion +
// idempotencyKey; a stale-version fwra.Conflict drives the §6.5 re-read→re-apply loop.
// Keyed on CustomerID per §3.0.
// ===========================================================================

// NOTE: the billingStateAccess consumer-seam interface is retired — the workflow reaches
// this RA through the generated typed invokers (invokers.gen.go), which carry the
// contract types directly. The Manager-local data mirrors below remain: the workflow
// folds the invokers' contract values into them (adapters.go converters) so the workflow
// body + Engines keep speaking one unified vocabulary. The empty CustomerProfile opened
// at registration is built inline in the workflow (billingstate.CustomerProfile{}).

// Version is the billing head-state optimistic-concurrency version
// (billingStateAccess.md §3). Mirrors the owning RA's Version type.
type version uint64

// GatewayBindingSeam mirrors billingStateAccess.md §3 GatewayBinding — the
// connected-account / gateway identifiers recorded at onboarding.
type gatewayBindingSeam struct {
	ConnectedAccountID string
}

// BillingOutcomeSeam mirrors billingStateAccess.md §3 BillingOutcome — the
// per-cycle business record of "cycle settled with this net". The money movement is a
// separate ledger step; this is the head-state outcome. Money is exact minor units.
type billingOutcomeSeam struct {
	Net       Money                // the signed settled net (exact minor units; never a float)
	Directive routingDirectiveSeam // the routed directive the Manager executed
	// Escalated flags the OQ-4 charge-failure escalation surfaced to the operator
	// dashboard via readBilling (no new DSL edge; §6.3).
	Escalated bool
}

// Billing mirrors billingStateAccess.md §3 — the head-state aggregate the
// workflow reads to carry expectedVersion forward and resolve the customer's terms +
// gateway binding. Keyed on CustomerID per §3.0.
type billingHead struct {
	ID            customerID
	Version       version
	GatewayBound  bool             // a GatewayBinding is present (registered + onboarded)
	Registered    bool             // the aggregate is open (registerCustomer ran)
	Terms         billingTermsSeam // the customer's billing terms (fed to the Engine by value)
	PayoutAccount string           // opaque payout-account ref (resolved deployedAppIdentity)
}

// CustomerSummary mirrors billingStateAccess.md §3 CustomerSummary (post FU-MST-1
// id migration: ID is CustomerID, not BillingID) — one persistently-delinquent
// customer in the sweep's cross-row read. PauseNotWithdraw carries the BillingTerms-
// derived enforcement shape the downstream operationsManager executes.
type customerSummary struct {
	ID               customerID
	PauseNotWithdraw bool // BillingTerms: pause (replicas=0) vs hard withdraw
}

// DelinquencyScope is the consumer-side platform/project scope for the sweep's
// cross-row read (billingManager.md §2.4 — platform scope). Empty ⇒ all customers.
type delinquencyScope struct {
	// ProjectID optionally narrows the scope; empty ⇒ platform-wide.
	ProjectID string
}

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
// billingEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors of the
// two billing-compute verbs (billingEngine.md §2.1/§2.2). DECIDE → the Manager
// EXECUTES the routing. Pure, deterministic, called DIRECTLY in-workflow (no Activity,
// no idempotency key, imports no Temporal). The Manager passes VALUE snapshots.
// ===========================================================================

// BillingEngine mirrors billingEngine.md §2.1/§2.2 — the signed-net + routing
// compute. The Engine STATES the directive; the Manager EXECUTES it.
type billingEngine interface {
	// ComputeNet computes the cycle's signed net + routing directive (UC6 close).
	ComputeNet(revenue cycleRevenueSeam, usage cycleUsageSeam, terms billingTermsSeam) (billingResultSeam, error)
	// RecomputeNet recomputes the corrected net + DELTA directive after a reversal
	// (ncuc4 chargeback; forward-only).
	RecomputeNet(affected reBillingInputSeam) (billingResultSeam, error)
}

// RoutingDirectiveSeam mirrors billingEngine.md §3 RoutingDirective — the closed
// routing decision set. The iota order matches the frozen contract (NoAction, Payout,
// Charge).
type routingDirectiveSeam int

const (
	// RoutingNoAction is net == 0 (or a recompute delta == 0) — skip.
	routingNoAction routingDirectiveSeam = iota
	// RoutingPayout is net > 0 — the Manager calls merchantGatewayAccess.payoutCustomer.
	routingPayout
	// RoutingCharge is net < 0 — the Manager calls merchantGatewayAccess.chargeCustomer.
	routingCharge
)

// String returns the canonical name for a routing directive.
func (d routingDirectiveSeam) String() string {
	switch d {
	case routingNoAction:
		return "NoAction"
	case routingPayout:
		return "Payout"
	case routingCharge:
		return "Charge"
	default:
		return "Unknown"
	}
}

// CycleRevenueSeam mirrors billingEngine.md §3 CycleRevenue — the value snapshot of
// the cycle's inbound revenue the Manager folds from revenueLedgerAccess.readRange. For
// recompute this is the REVERSAL-ADJUSTED total (the Manager appended the reversal and
// re-read the range). Exact minor units.
type cycleRevenueSeam struct {
	CustomerID   customerID
	CycleID      cycleID
	GrossInbound Money // Σ inbound (already reversal-adjusted for recompute), exact minor units
	Currency     string
	EventCount   int
}

// CycleUsageSeam mirrors billingEngine.md §3 CycleUsage — the value snapshot of the
// cycle's compute usage the Manager folds from usageAccess.readRange.
type cycleUsageSeam struct {
	CustomerID         customerID
	CycleID            cycleID
	ComputeUnitSeconds float64
}

// BillingTermsSeam mirrors billingEngine.md §3 BillingTerms — the customer's
// terms snapshot, read from billing head-state and fed to the Engine by value. The
// Strategy discriminators are package-internal to the Engine.
type billingTermsSeam struct {
	RevenueShareKind int // opaque discriminator; the Engine pivots on it
	ComputeCostKind  int
	ScheduleKind     int
	BillingKind      int
}

// BillingResultSeam mirrors billingEngine.md §3 BillingResult — the shared
// output of ComputeNet/RecomputeNet. SignedNet is exact minor units; the Manager routes
// the directive. RevenueShareApplied/ComputeCostApplied are the statement decomposition.
type billingResultSeam struct {
	SignedNet           Money
	RoutingDirective    routingDirectiveSeam
	RevenueShareApplied Money
	ComputeCostApplied  Money
}

// ReBillingInputSeam mirrors billingEngine.md §3 ReBillingInput — the
// reversal-adjusted recompute input carrying the prior settled result so the DELTA can
// be computed (forward-only).
type reBillingInputSeam struct {
	Revenue      cycleRevenueSeam
	Usage        cycleUsageSeam
	Terms        billingTermsSeam
	PriorSettled billingResultSeam
}

// ===========================================================================
// interventionEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors of
// the billing-failure verb (interventionEngine.md §2.3 decideOnBillingFailure).
// DECIDE → the Manager EXECUTES. Pure, deterministic, direct in-workflow.
// ===========================================================================

// InterventionEngine mirrors interventionEngine.md §2.3 — the billing-failure
// decision. The Engine DECIDES {Retry | Delay | Escalate}; the Manager EXECUTES.
type interventionEngine interface {
	DecideOnBillingFailure(failure billingFailureSeam) (billingFailureDirectiveSeam, error)
}

// BillingFailureKindSeam mirrors interventionEngine.md §3 BillingFailureKind.
type billingFailureKindSeam int

const (
	// BillingFailureChargeDeclined is a declined shortfall charge.
	billingFailureChargeDeclined billingFailureKindSeam = iota
	// BillingFailureDisputed is a disputed cycle.
	billingFailureDisputed
	// BillingFailureChargedBack is a charged-back cycle.
	billingFailureChargedBack
)

// BillingFailureSeam mirrors interventionEngine.md §3 BillingFailure — the
// failed-action context fed to the decision by value.
type billingFailureSeam struct {
	CustomerID   customerID
	CycleID      cycleID
	Kind         billingFailureKindSeam
	AttemptCount int
	ShortfallAge int // sweeps elapsed; NOT a clock read
}

// BillingFailureDirectiveSeam mirrors interventionEngine.md §3 — the closed
// decision set. The directive IDENTITY (not the numeric value) is load-bearing
// (interventionEngine.md §3 senior note); the order mirrors the frozen contract.
type billingFailureDirectiveSeam int

const (
	// BillingRetry re-attempts the charge now (within budget).
	billingRetry billingFailureDirectiveSeam = iota
	// BillingDelay backs off; re-attempts on the next shortfallSweep (grace).
	billingDelay
	// BillingEscalate flags delinquency (tolerance exhausted).
	billingEscalate
)
