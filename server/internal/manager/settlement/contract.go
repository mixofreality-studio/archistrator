// Package settlement is the settlementManager component of the archistrator
// server's Manager layer — the use-case façade for the platform's money lifecycle
// on operated customer apps (Objective 3 — revenue share + compute-cost recovery),
// per the senior-frozen contract
// designs/aiarch/implementation/contracts/settlementManager.md (C-MST).
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal), it registers the per-customer
// closeSettlementCycle:<customerId> Schedule (at onboarding) and the hourly
// shortfallSweep Schedule (at startup), defines one Activity per ResourceAccess
// call, owns the Signal handlers (inboundRevenueReceived / chargebackReceived) and
// the in-workflow primitives (awaitSignal — category A — and the saga
// compensation), and derives the idempotency keys passed down to each settlement
// head-state RA write. Temporal lives ONLY in this component; the downstream
// Engines (settlementEngine, interventionEngine — pure, in-workflow, by value) and
// ResourceAccess ports (settlementStateAccess, revenueLedgerAccess, usageAccess,
// merchantGatewayAccess, operatedRuntimeAccess, durableExecutionAccess) import no
// Temporal.
//
// The SIX frozen public ops (settlementManager.md §2):
//   - OnboardPaymentIntegration — Workflow (entry; operator-initiated UC5 onboard)
//   - RegisterCustomer          — Workflow (entry; ncuc1 open the aggregate)
//   - CloseSettlementCycle      — Workflow (entry; Schedule-triggered cycle close)
//   - RunShortfallSweep         — Workflow (entry; Schedule-triggered delinquency sweep)
//   - RecordInboundRevenue      — Signal (webhook-fed inbound revenue fact)
//   - RecordRevenueReversal     — Signal (webhook-fed chargeback reversal fact)
//
// File layout (mirrors internal/manager/operations):
//   - contract.go            : the public façade types (§3) + the façade error model (§3.1)
//   - settlementmanager.go   : the Manager that translates public ops into Temporal client calls (§6.2)
//   - deps.go                : the consumer-side dep interfaces + frozen-collaborator seams (§5)
//   - workflow.go            : the Workflows deps struct + workflow bodies + the Conflict loop (§6.3, §6.5)
//   - activities.go          : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - signals.go             : the inbound/reversal Signal payloads handled by the cycle workflow (§6.3)
//   - errors.go              : the port-error -> Temporal-error mapping helper (§6.4)
//   - worker.go              : worker registration of workflows + activities + the Schedules (§6.1)
package settlement

import (
	"time"

	"github.com/google/uuid"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// ---------------------------------------------------------------------------
// Identity & canonical types (settlementManager.md §3.0 — THE MATERIAL RULING).
//
// CANONICAL settlement aggregate identity = CustomerID, typed uuid.UUID. This
// ratifies the two frozen ledgers (revenueLedgerAccess / usageAccess, already
// CustomerID(uuid)) in place and forces the design-only settlementStateAccess to
// migrate SettlementID(string) → CustomerID(uuid) additively (FU-MST-1). We do NOT
// reintroduce SettlementID(string). CycleID stays string (all three ledgers agree).
// DeployedAppID is the operations-side operated-app identity (NOT the settlement
// key); the onboarding workflow resolves DeployedAppID → CustomerID via
// settlementStateAccess.readSettlement.
// ---------------------------------------------------------------------------

// CustomerID is the canonical settlement aggregate identity (settlementManager.md
// §3.0). One settlement aggregate per customer; shared by revenueLedgerAccess,
// usageAccess, and (post FU-MST-1) settlementStateAccess.
type CustomerID = uuid.UUID

// CycleID is the billing cycle a settlement folds at close. Agreed string across
// revenueLedgerAccess / usageAccess / settlementStateAccess (settlementManager.md §3.0).
type CycleID = string

// DeployedAppID is the operated-app identity owned by the operations side; it is
// NOT the settlement aggregate key. op 2.1 resolves it to a CustomerID
// (settlementManager.md §3.0 / §2.1).
type DeployedAppID = uuid.UUID

// ---------------------------------------------------------------------------
// Money — exact integer minor units + currency. NEVER a float (settlementManager.md
// §3 / revenueLedgerAccess §3.1 / settlementEngine §3). Signed: positive == payout to
// the customer, negative == shortfall charge. This Manager moves money via
// merchantGatewayAccess by VALUE; the math is the Engine's, never re-derived here.
// ---------------------------------------------------------------------------

// Money is a signed amount in minor units (cents) plus an ISO-4217 currency. The
// shared money value type the Engine produces and this Manager routes.
type Money struct {
	MinorUnits int64  `json:"minorUnits"` // signed; e.g. 1299 == 12.99; reversals carry a negative value
	Currency   string `json:"currency"`   // ISO-4217, e.g. "USD"
}

// ---------------------------------------------------------------------------
// RoutingDirective — settlementEngine-owned (settlementEngine.md §3); MIRRORED here
// as the Manager-local seam (deps.go RoutingDirectiveSeam) and re-exported as the
// façade RoutingDirective for the CloseCycleResult. The Engine STATES the directive;
// this Manager EXECUTES it (settlementManager.md §0 decision 2).
// ---------------------------------------------------------------------------

// RoutingDirective is which way the signed net routes (settlementManager.md §3 /
// settlementEngine.md §3). A VALUE the Engine returns; the Manager executes it
// against merchantGatewayAccess.
type RoutingDirective = RoutingDirectiveSeam

// ---------------------------------------------------------------------------
// Public façade return values (settlementManager.md §3). These are this Manager's
// own view types — NOT persisted head-state. The persisted shapes (SettlementOutcome,
// RevenueEntry, ...) are owned by their RA/Engine and referenced via deps.go seams,
// never redefined here (memory: feedback_method_models_owned_by_ra.md).
// ---------------------------------------------------------------------------

// SettlementRef is the continuity token returned by onboarding / registration
// (settlementManager.md §3).
type SettlementRef struct {
	CustomerID CustomerID `json:"customerId"`
}

// CloseCycleResult is the result of CloseSettlementCycle (settlementManager.md §3).
// SignedNet is NOT surfaced raw — it is recorded in settlementStateAccess; the read
// path is settlementStateAccess.readSettlement (the CQRS split, §6.6). Routed states
// which directive the Manager executed.
type CloseCycleResult struct {
	CustomerID CustomerID       `json:"customerId"`
	CycleID    CycleID          `json:"cycleId"`
	Routed     RoutingDirective `json:"routed"`
	// Escalated is true when the charge failed and interventionEngine returned
	// Escalate (the customer is flagged delinquent on head-state; OQ-4 / §6.3). The
	// operator dashboard reads it via settlementStateAccess.readSettlement.
	Escalated bool `json:"escalated"`
}

// ShortfallSweepResult is the result of RunShortfallSweep (settlementManager.md §3).
// SignalledCustomers may be empty — a quiet sweep is a normal outcome.
type ShortfallSweepResult struct {
	SignalledCustomers []CustomerID `json:"signalledCustomers"`
}

// ---------------------------------------------------------------------------
// Webhook payload inputs (settlementManager.md §3). These façade input types carry
// the (upstream-signature-verified) webhook body the Manager maps onto the
// revenueLedgerAccess-owned RevenueEntry / ReversalEntry at append time. The
// persisted shapes are owned by revenueLedgerAccess (deps.go seams), not redefined.
// ---------------------------------------------------------------------------

// GatewayRevenueEvent is the verified inbound-revenue webhook body (op 2.5). The
// gateway event id is the append's dedup token (revenueLedgerAccess dedups on it).
type GatewayRevenueEvent struct {
	GatewayEventID string     `json:"gatewayEventId"` // globally-unique dedup token
	CustomerID     CustomerID `json:"customerId"`
	CycleID        CycleID    `json:"cycleId"`
	Amount         Money      `json:"amount"`     // signed minor units + currency (inbound: positive)
	OccurredAt     time.Time  `json:"occurredAt"` // gateway-supplied
}

// GatewayReversalEvent is the verified chargeback/reversal webhook body (op 2.6). The
// chargeback's own gateway event id is the dedup token; ReversesGatewayEventID is an
// optional back-link to the inbound entry it reverses.
type GatewayReversalEvent struct {
	GatewayEventID         string     `json:"gatewayEventId"` // the chargeback's own dedup token
	CustomerID             CustomerID `json:"customerId"`
	CycleID                CycleID    `json:"cycleId"`
	Amount                 Money      `json:"amount"` // negative minor units + currency
	ReversesGatewayEventID string     `json:"reversesGatewayEventId,omitempty"`
	OccurredAt             time.Time  `json:"occurredAt"`
}

// ---------------------------------------------------------------------------
// Façade error model (settlementManager.md §3.1).
// CALLER/PROGRAMMER errors at the façade boundary — distinct from the workflow's own
// failure handling (Temporal RetryPolicy + the interventionEngine decide→execute
// alternative paths + the forward-only chargeback compensation inside the workflow
// body). Kinds used: ContractMisuse, FailedPrecondition, NotFound, Unauthorized,
// Infrastructure.
// ---------------------------------------------------------------------------

// SettlementError is the typed façade error (settlementManager.md §3.1). It is an
// alias for fwmgr.Error so errors.As(&SettlementError) call sites work — the SAME
// shared Manager error model the peer Managers use.
type SettlementError = fwmgr.Error

func newError(kind fwmgr.Kind, detail string) *fwmgr.Error {
	return fwmgr.New(kind, detail)
}
