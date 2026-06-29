package settlement

import (
	"context"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// This file declares settlementManager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom). Per the senior hand-off, NONE of settlementManager's
// collaborators is yet built as a Go package in this module, so this Manager is built
// against their FROZEN CONTRACTS as interfaces it declares here, and unit-tested with
// hand-written fakes:
//
//   - SettlementStateAccess  — settlementStateAccess.md §2/§3 (design-only; FU-MST-1 id migration)
//   - RevenueLedgerAccess    — revenueLedgerAccess.md §2/§3 (FROZEN; not yet built)
//   - UsageAccess            — usageAccess.md §2/§3 (FROZEN; not yet built)
//   - MerchantGatewayAccess  — merchantGatewayAccess (D-MA — NOT YET CONTRACTED; FU-MST-2/OQ-2)
//   - OperatedRuntimeAccess  — operatedRuntimeAccess.md §2/§3 (FROZEN; not yet built)
//   - SettlementEngine       — settlementEngine.md §2.1/§2.2 (FROZEN; not yet built)
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
// (settlementManager.md OQ-7). This keeps the Method discipline "models live in their
// owning RA/Engine" intact.
//
// §3.0 IDENTITY: every collaborator below keys on CustomerID = uuid.UUID. We do NOT
// reintroduce SettlementID(string) (the §3.0 ruling); settlementStateAccess is
// consumed here ALREADY MIGRATED (the FU-MST-1 shape), which the composition root will
// satisfy once that RA is built.

// ===========================================================================
// settlementStateAccess — DESIGN-ONLY (FU-MST-1: id-type migrated to CustomerID).
// Narrow consumer interface: the head-state reads + the additive write verbs. Each
// WRITE carries expectedVersion + idempotencyKey; a stale-version fwra.Conflict drives
// the §6.5 re-read→re-apply loop. Keyed on CustomerID per §3.0.
// ===========================================================================

// SettlementStateAccess mirrors settlementStateAccess.md §2 (post FU-MST-1) — the
// settlement/customer head-state RA. Reads are pure; writes carry the version guard +
// dedup-first idempotency key.
type settlementStateAccess interface {
	// ReadSettlement returns the whole head-state aggregate (NotFound if no row).
	ReadSettlement(ctx context.Context, customerID CustomerID) (Settlement, error)
	// ReadPersistentlyDelinquentCustomers returns the persistently-delinquent customer
	// set (drives the shortfall sweep). Platform/scope input; a cross-row read.
	ReadPersistentlyDelinquentCustomers(ctx context.Context, scope DelinquencyScope) ([]CustomerSummary, error)
	// RegisterCustomer opens the settlement aggregate (additive write).
	RegisterCustomer(ctx context.Context, customerID CustomerID, expectedVersion Version, profile CustomerProfileSeam, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// BindGatewayLive records that the merchant-gateway binding is live (additive write).
	BindGatewayLive(ctx context.Context, customerID CustomerID, expectedVersion Version, binding GatewayBindingSeam, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// SettleCycle records the settlement outcome for a cycle (additive write).
	SettleCycle(ctx context.Context, customerID CustomerID, expectedVersion Version, cycle CycleID, outcome SettlementOutcomeSeam, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// ResettleCycle records a correction to a previously-settled cycle (additive write).
	ResettleCycle(ctx context.Context, customerID CustomerID, expectedVersion Version, cycle CycleID, correction SettlementOutcomeSeam, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

// Version is the settlement head-state optimistic-concurrency version
// (settlementStateAccess.md §3). Mirrors the owning RA's Version type.
type Version uint64

// CustomerProfileSeam mirrors settlementStateAccess.md §3 CustomerProfile — the
// infrastructure-opaque customer identity/payout snapshot opened at registration.
type CustomerProfileSeam struct {
	// PayoutAccountRef is an opaque gateway/payout-account reference (the contract is
	// "identity, payout account, …"); kept narrow at this seam.
	PayoutAccountRef string
}

// GatewayBindingSeam mirrors settlementStateAccess.md §3 GatewayBinding — the
// connected-account / gateway identifiers recorded at onboarding.
type GatewayBindingSeam struct {
	ConnectedAccountID string
}

// SettlementOutcomeSeam mirrors settlementStateAccess.md §3 SettlementOutcome — the
// per-cycle business record of "cycle settled with this net". The money movement is a
// separate ledger step; this is the head-state outcome. Money is exact minor units.
type SettlementOutcomeSeam struct {
	Net       Money                // the signed settled net (exact minor units; never a float)
	Directive RoutingDirectiveSeam // the routed directive the Manager executed
	// Escalated flags the OQ-4 charge-failure escalation surfaced to the operator
	// dashboard via readSettlement (no new DSL edge; §6.3).
	Escalated bool
}

// Settlement mirrors settlementStateAccess.md §3 — the head-state aggregate the
// workflow reads to carry expectedVersion forward and resolve the customer's terms +
// gateway binding. Keyed on CustomerID per §3.0.
type Settlement struct {
	ID            CustomerID
	Version       Version
	GatewayBound  bool                // a GatewayBinding is present (registered + onboarded)
	Registered    bool                // the aggregate is open (registerCustomer ran)
	Terms         SettlementTermsSeam // the customer's settlement terms (fed to the Engine by value)
	PayoutAccount string              // opaque payout-account ref (resolved deployedAppIdentity)
}

// CustomerSummary mirrors settlementStateAccess.md §3 CustomerSummary (post FU-MST-1
// id migration: ID is CustomerID, not SettlementID) — one persistently-delinquent
// customer in the sweep's cross-row read. PauseNotWithdraw carries the BillingTerms-
// derived enforcement shape the downstream operationsManager executes.
type CustomerSummary struct {
	ID               CustomerID
	PauseNotWithdraw bool // BillingTerms: pause (replicas=0) vs hard withdraw
}

// DelinquencyScope is the consumer-side platform/project scope for the sweep's
// cross-row read (settlementManager.md §2.4 — platform scope). Empty ⇒ all customers.
type DelinquencyScope struct {
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
	RecordInboundRevenue(ctx context.Context, entry RevenueEntrySeam) (EntryRefSeam, error)
	// RecordReversal appends a reversal/chargeback fact (dedup on GatewayEventID).
	RecordReversal(ctx context.Context, reversal ReversalEntrySeam) (EntryRefSeam, error)
	// ReadRange replays the cycle's revenue facts (inbound + reversals, append order).
	ReadRange(ctx context.Context, customerID CustomerID, cycleID CycleID) ([]RevenueEntrySeam, error)
}

// EntryRefSeam mirrors revenueLedgerAccess.md §3 EntryRef — an opaque ref to a
// recorded ledger entry.
type EntryRefSeam string

// RevenueKindSeam mirrors revenueLedgerAccess.md §3 RevenueKind.
type RevenueKindSeam int

const (
	// RevenueKindInbound is an end-user payment collected via the gateway.
	RevenueKindInbound RevenueKindSeam = iota
	// RevenueKindReversal is a chargeback/dispute reversal of a prior inbound fact.
	RevenueKindReversal
)

// RevenueEntrySeam mirrors revenueLedgerAccess.md §3 RevenueEntry — one immutable
// revenue fact (the recordInboundRevenue payload and the readRange element type).
type RevenueEntrySeam struct {
	CustomerID     CustomerID
	CycleID        CycleID
	Kind           RevenueKindSeam
	Amount         Money // signed minor units + currency (exact; never a float)
	GatewayEventID string
	OccurredAt     time.Time
}

// ReversalEntrySeam mirrors revenueLedgerAccess.md §3 ReversalEntry — the
// recordReversal payload (negative Amount + optional back-link).
type ReversalEntrySeam struct {
	CustomerID             CustomerID
	CycleID                CycleID
	Amount                 Money // negative minor units + currency
	GatewayEventID         string
	ReversesGatewayEventID string // optional back-link; empty if absent
	OccurredAt             time.Time
}

// ===========================================================================
// usageAccess — FROZEN, NOT YET BUILT. Narrow consumer interface (usageAccess.md §2).
// This Manager only READS (the cycle fold at close; OperatedAppID nil = whole cycle).
// The append-writes (recordComputeUsage / recordFinalUsage) belong to operationsManager,
// NOT settlementManager — they are not on this seam.
// ===========================================================================

// UsageAccess mirrors usageAccess.md §2.3 — the cycle-scope read this Manager uses to
// fold a whole cycle's usage at close. Pure read; no key.
type usageAccess interface {
	// ReadRange replays the cycle's usage facts (OperatedAppID nil ⇒ whole cycle).
	ReadRange(ctx context.Context, query UsageRangeQuerySeam) ([]UsageEventSeam, error)
}

// UsageRangeQuerySeam mirrors usageAccess.md §3 UsageRangeQuery — the cycle-scope read
// query. settlementManager folds the WHOLE cycle, so OperatedAppID is nil (§5.2 / D-UA §2.3).
type UsageRangeQuerySeam struct {
	CustomerID    CustomerID
	CycleID       CycleID
	OperatedAppID *DeployedAppID // nil for settlement's whole-cycle fold
}

// ComputeUnitsSeam mirrors usageAccess.md §3 ComputeUnits — an infrastructure-neutral
// metered quantity (never priced, never a cloud lexeme).
type ComputeUnitsSeam struct {
	Amount float64
	Unit   string
}

// UsageEventSeam mirrors usageAccess.md §3 UsageEvent — one metered usage fact (the
// readRange element type the Manager folds into the Engine's CycleUsage snapshot).
type UsageEventSeam struct {
	CustomerID    CustomerID
	OperatedAppID DeployedAppID
	CycleID       CycleID
	Units         ComputeUnitsSeam
	OccurredAt    time.Time
}

// ===========================================================================
// merchantGatewayAccess — D-MA NOT YET CONTRACTED (FU-MST-2 / OQ-2). The seam is
// defined by the DSL labels (component description line 211 + caller edges) + the §6.4
// Activity wrappers (externalGateway RetryPolicy; Stripe Idempotency-Key =
// settle:{customerId}:{cycleId}). The narrow consumer interface below mirrors those
// four verbs; REPLACE with the owner import when the D-MA contract lands and is built.
// ===========================================================================

// MerchantGatewayAccess mirrors the four merchantGatewayAccess verbs this Manager
// calls (settlementManager.md §5.2/§6.4). The Manager moves money here by VALUE; the
// gateway dedups on the Manager-supplied idempotency key. SEAM — D-MA is unbuilt
// (FU-MST-2); replace with the owner import when it lands.
type merchantGatewayAccess interface {
	// PayoutCustomer pays the (positive) net to the customer. idempotencyKey =
	// settle:{customerId}:{cycleId} (Stripe-native dedup).
	PayoutCustomer(ctx context.Context, customerID CustomerID, amount Money, idempotencyKey string) error
	// ChargeCustomer charges the (positive magnitude of the negative) shortfall net.
	// A decline/auth/contract-misuse is terminal and drives decideOnSettlementFailure.
	ChargeCustomer(ctx context.Context, customerID CustomerID, amount Money, idempotencyKey string) error
	// CreateConnectedAccount creates the merchant connected account (onboarding).
	CreateConnectedAccount(ctx context.Context, customerID CustomerID, idempotencyKey string) (GatewayBindingSeam, error)
	// ValidateStoredInstrument validates the stored instrument via a zero-amount auth
	// (customer registration; ncuc1).
	ValidateStoredInstrument(ctx context.Context, customerID CustomerID, idempotencyKey string) error
}

// ===========================================================================
// operatedRuntimeAccess — FROZEN, NOT YET BUILT. Narrow consumer interface — only the
// onboarding write this Manager uses: wirePaymentConfig (folds into publishDesiredState
// per D-OR §2.5). Git-content-idempotent — no version guard.
// ===========================================================================

// OperatedRuntimeAccess mirrors the one operatedRuntimeAccess verb this Manager uses at
// onboarding (settlementManager.md §5.2). Idempotent on the caller-supplied key (git
// content-address).
type operatedRuntimeAccess interface {
	// WirePaymentConfig wires the gateway binding into the deployed app's runtime
	// (folds into publishDesiredState; D-OR §2.5).
	WirePaymentConfig(ctx context.Context, deployedAppID DeployedAppID, binding GatewayBindingSeam, idempotencyKey fwra.IdempotencyKey) error
}

// ===========================================================================
// durableExecutionAccess — EXISTS (internal/resourceaccess/durableexecution). The two
// category-B control-plane verbs this Manager calls: deliverSignal (the one queued
// cross-Manager applyDelinquencyPolicy edge) + registerSchedule (×2). Consumed via a
// narrow seam interface so the composition root adapts the concrete *durableexecution.
// Runtime (whose RegisterSchedule / DeliverSignal signatures differ). awaitSignal (the
// inbound/reversal/chargeback waits) is the Manager's OWN workflow code (D-DA category
// A), NOT an RA method.
// ===========================================================================

// DurableExecutionAccess is the Manager's consumer view: the two category-B verbs.
type durableExecutionAccess interface {
	// DeliverSignal delivers a queued signal to another Manager's workflow (the one
	// sanctioned M→M edge: applyDelinquencyPolicy → operationsManager).
	DeliverSignal(ctx context.Context, targetWorkflowID string, signalName string, payload deliverSignalPayload) error
	// RegisterSchedule registers (idempotently, by id) a recurring Schedule.
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// deliverSignalPayload mirrors the applyDelinquencyPolicy payload delivered to
// operationsManager (the receiving handler dedups; D-DA §9 OQ3). The composition root
// adapts it onto durableexecution.ExecutionPayload.
type deliverSignalPayload struct {
	CustomerID       CustomerID
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
// settlementEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors of the
// two settlement-compute verbs (settlementEngine.md §2.1/§2.2). DECIDE → the Manager
// EXECUTES the routing. Pure, deterministic, called DIRECTLY in-workflow (no Activity,
// no idempotency key, imports no Temporal). The Manager passes VALUE snapshots.
// ===========================================================================

// SettlementEngine mirrors settlementEngine.md §2.1/§2.2 — the signed-net + routing
// compute. The Engine STATES the directive; the Manager EXECUTES it.
type settlementEngine interface {
	// ComputeNet computes the cycle's signed net + routing directive (UC6 close).
	ComputeNet(revenue CycleRevenueSeam, usage CycleUsageSeam, terms SettlementTermsSeam) (SettlementResultSeam, error)
	// RecomputeNet recomputes the corrected net + DELTA directive after a reversal
	// (ncuc4 chargeback; forward-only).
	RecomputeNet(affected ReSettlementInputSeam) (SettlementResultSeam, error)
}

// RoutingDirectiveSeam mirrors settlementEngine.md §3 RoutingDirective — the closed
// routing decision set. The iota order matches the frozen contract (NoAction, Payout,
// Charge).
type RoutingDirectiveSeam int

const (
	// RoutingNoAction is net == 0 (or a recompute delta == 0) — skip.
	RoutingNoAction RoutingDirectiveSeam = iota
	// RoutingPayout is net > 0 — the Manager calls merchantGatewayAccess.payoutCustomer.
	RoutingPayout
	// RoutingCharge is net < 0 — the Manager calls merchantGatewayAccess.chargeCustomer.
	RoutingCharge
)

// String returns the canonical name for a routing directive.
func (d RoutingDirectiveSeam) String() string {
	switch d {
	case RoutingNoAction:
		return "NoAction"
	case RoutingPayout:
		return "Payout"
	case RoutingCharge:
		return "Charge"
	default:
		return "Unknown"
	}
}

// CycleRevenueSeam mirrors settlementEngine.md §3 CycleRevenue — the value snapshot of
// the cycle's inbound revenue the Manager folds from revenueLedgerAccess.readRange. For
// recompute this is the REVERSAL-ADJUSTED total (the Manager appended the reversal and
// re-read the range). Exact minor units.
type CycleRevenueSeam struct {
	CustomerID   CustomerID
	CycleID      CycleID
	GrossInbound Money // Σ inbound (already reversal-adjusted for recompute), exact minor units
	Currency     string
	EventCount   int
}

// CycleUsageSeam mirrors settlementEngine.md §3 CycleUsage — the value snapshot of the
// cycle's compute usage the Manager folds from usageAccess.readRange.
type CycleUsageSeam struct {
	CustomerID         CustomerID
	CycleID            CycleID
	ComputeUnitSeconds float64
}

// SettlementTermsSeam mirrors settlementEngine.md §3 SettlementTerms — the customer's
// terms snapshot, read from settlement head-state and fed to the Engine by value. The
// Strategy discriminators are package-internal to the Engine.
type SettlementTermsSeam struct {
	RevenueShareKind int // opaque discriminator; the Engine pivots on it
	ComputeCostKind  int
	ScheduleKind     int
	BillingKind      int
}

// SettlementResultSeam mirrors settlementEngine.md §3 SettlementResult — the shared
// output of ComputeNet/RecomputeNet. SignedNet is exact minor units; the Manager routes
// the directive. RevenueShareApplied/ComputeCostApplied are the statement decomposition.
type SettlementResultSeam struct {
	SignedNet           Money
	RoutingDirective    RoutingDirectiveSeam
	RevenueShareApplied Money
	ComputeCostApplied  Money
}

// ReSettlementInputSeam mirrors settlementEngine.md §3 ReSettlementInput — the
// reversal-adjusted recompute input carrying the prior settled result so the DELTA can
// be computed (forward-only).
type ReSettlementInputSeam struct {
	Revenue      CycleRevenueSeam
	Usage        CycleUsageSeam
	Terms        SettlementTermsSeam
	PriorSettled SettlementResultSeam
}

// ===========================================================================
// interventionEngine — FROZEN, NOT YET BUILT. Consumer interface + local mirrors of
// the settlement-failure verb (interventionEngine.md §2.3 decideOnSettlementFailure).
// DECIDE → the Manager EXECUTES. Pure, deterministic, direct in-workflow.
// ===========================================================================

// InterventionEngine mirrors interventionEngine.md §2.3 — the settlement-failure
// decision. The Engine DECIDES {Retry | Delay | Escalate}; the Manager EXECUTES.
type interventionEngine interface {
	DecideOnSettlementFailure(failure SettlementFailureSeam) (SettlementFailureDirectiveSeam, error)
}

// SettlementFailureKindSeam mirrors interventionEngine.md §3 SettlementFailureKind.
type SettlementFailureKindSeam int

const (
	// SettlementFailureChargeDeclined is a declined shortfall charge.
	SettlementFailureChargeDeclined SettlementFailureKindSeam = iota
	// SettlementFailureDisputed is a disputed cycle.
	SettlementFailureDisputed
	// SettlementFailureChargedBack is a charged-back cycle.
	SettlementFailureChargedBack
)

// SettlementFailureSeam mirrors interventionEngine.md §3 SettlementFailure — the
// failed-action context fed to the decision by value.
type SettlementFailureSeam struct {
	CustomerID   CustomerID
	CycleID      CycleID
	Kind         SettlementFailureKindSeam
	AttemptCount int
	ShortfallAge int // sweeps elapsed; NOT a clock read
}

// SettlementFailureDirectiveSeam mirrors interventionEngine.md §3 — the closed
// decision set. The directive IDENTITY (not the numeric value) is load-bearing
// (interventionEngine.md §3 senior note); the order mirrors the frozen contract.
type SettlementFailureDirectiveSeam int

const (
	// SettlementRetry re-attempts the charge now (within budget).
	SettlementRetry SettlementFailureDirectiveSeam = iota
	// SettlementDelay backs off; re-attempts on the next shortfallSweep (grace).
	SettlementDelay
	// SettlementEscalate flags delinquency (tolerance exhausted).
	SettlementEscalate
)
