// Package billing is the billingManager component of the archistrator
// server's Manager layer — the use-case façade for the platform's money lifecycle
// on operated customer apps (Objective 3 — revenue share + compute-cost recovery),
// per the senior-frozen contract
// designs/aiarch/implementation/contracts/billingManager.md (C-MST).
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal), it registers the per-customer
// closeBillingCycle:<customerId> Schedule (at onboarding) and the hourly
// shortfallSweep Schedule (at startup), defines one Activity per ResourceAccess
// call, owns the Signal handlers (inboundRevenueReceived / chargebackReceived) and
// the in-workflow primitives (awaitSignal — category A — and the saga
// compensation), and derives the idempotency keys passed down to each billing
// head-state RA write. Temporal lives ONLY in this component; the downstream
// Engines (billingEngine, interventionEngine — pure, in-workflow, by value) and
// ResourceAccess ports (billingStateAccess, revenueLedgerAccess, usageAccess,
// merchantGatewayAccess, operatedRuntimeAccess, durableExecutionAccess) import no
// Temporal.
//
// The SIX frozen public ops (billingManager.md §2):
//   - OnboardPaymentIntegration — Workflow (entry; operator-initiated UC5 onboard)
//   - RegisterCustomer          — Workflow (entry; ncuc1 open the aggregate)
//   - CloseBillingCycle      — Workflow (entry; Schedule-triggered cycle close)
//   - RunShortfallSweep         — Workflow (entry; Schedule-triggered delinquency sweep)
//   - RecordInboundRevenue      — Signal (webhook-fed inbound revenue fact)
//   - RecordRevenueReversal     — Signal (webhook-fed chargeback reversal fact)
//
// File layout (mirrors internal/manager/operations):
//   - contract.go            : the public façade types (§3) + the façade error model (§3.1)
//   - billingmanager.go   : the Manager that translates public ops into Temporal client calls (§6.2)
//   - deps.go                : the consumer-side dep interfaces + frozen-collaborator seams (§5)
//   - workflow.go            : the Workflows deps struct + workflow bodies + the Conflict loop (§6.3, §6.5)
//   - activities.go          : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - signals.go             : the inbound/reversal Signal payloads handled by the cycle workflow (§6.3)
//   - errors.go              : the port-error -> Temporal-error mapping helper (§6.4)
//   - worker.go              : worker registration of workflows + activities + the Schedules (§6.1)
package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/revenueledger"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// BillingManager is the billingManager port — the public use-case surface of the
// façade (billingManager.md §2). Each op leads with the Manager-layer call Context
// (fwmgr.Context, embedding context.Context + the Principal); the *Manager derives
// ctx := rc.Context inside.
//
// SCHEMA-FIRST: this interface (and the port I/O types) are GENERATED into
// contract.gen.go from this component's `.serviceContracts` entry in
// .aiarch/state/project.json (edit that entry + `make gen`; do NOT
// hand-edit the generated surface). The concrete *Manager below satisfies it; the
// consumer-side dependency interfaces (deps.go) and the Temporal Workflows struct
// (workflow.go) stay hand-written and are NOT part of this contract.

// Compile-time proof the concrete billingManager satisfies the generated
// BillingManager port. Each op leads with the Manager-layer call Context
// (fwmgr.Context); the impl derives ctx := rc.Context inside.
var _ BillingManager = (*billingManager)(nil)

// billingManager is the billingManager façade — the concrete implementation of the
// GENERATED BillingManager interface (contract.gen.go). It exposes the six public
// use-case ops (billingManager.md §2) and OWNS Temporal. The Temporal-backed ops:
//   - OnboardPaymentIntegration — Workflow (entry; StartWorkflow, id {customerId}:onboard)
//   - RegisterCustomer          — Workflow (entry; StartWorkflow, id {customerId}:register)
//   - CloseBillingCycle      — Workflow (entry; StartWorkflow, id {customerId}:{cycleId}:close)
//   - RunShortfallSweep         — Workflow (entry; StartWorkflow, id :all:shortfallSweep:{tickId})
//   - RecordInboundRevenue      — Signal (SignalWithStart inboundRevenueReceived → close id)
//   - RecordRevenueReversal     — Signal (SignalWithStart chargebackReceived → close id)
//
// The façade methods use only the Temporal client; the pre-condition checks (non-empty
// ids) are enforced here before any Temporal call (billingManager.md §2/§3.1). It ALSO
// stores the PUBLISHED downstream deps the GENERATED constructor was given so
// RegisterWorker can fold them (adapters.go) into the hand-written Temporal Workflows.
// The former exported consumer-mirror interfaces are RETIRED; the Manager depends on the
// deps' PUBLISHED interfaces and adapts them internally.
type billingManager struct {
	client client.Client

	billingState     billingstate.BillingStateAccess
	usage            usage.UsageAccess
	merchantGateway  merchantgateway.MerchantGatewayAccess
	durableExecution durableexecution.DurableExecutionAccess
	billing          billingengine.BillingEngine
	intervention     intervention.InterventionEngine

	// revenueLedger (B6/B7) is the generated revenueLedgerAccess dep, threaded into
	// genActivities (workermanifest.go) exactly like billingState/usage/merchantGateway
	// /durableExecution: the workflow reaches it through the generated invoker surface
	// (invokers.gen.go/activities.gen.go) — no Manager-local seam or custom Activity.
	revenueLedger revenueledger.RevenueLedgerAccess
}

// newBillingManager is the hand-written, unexported builder the generated
// NewBillingManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only the client; the deps are
// stored for RegisterWorker (worker.go), which folds them into the Temporal Workflows.
func newBillingManager(
	c client.Client,
	billingState billingstate.BillingStateAccess,
	usage usage.UsageAccess,
	merchantGateway merchantgateway.MerchantGatewayAccess,
	durableExecution durableexecution.DurableExecutionAccess,
	billing billingengine.BillingEngine,
	interventionEng intervention.InterventionEngine,
	revenueLedger revenueledger.RevenueLedgerAccess,
) *billingManager {
	return &billingManager{
		client:           c,
		revenueLedger:    revenueLedger,
		billingState:     billingState,
		usage:            usage,
		merchantGateway:  merchantGateway,
		durableExecution: durableExecution,
		billing:          billing,
		intervention:     interventionEng,
	}
}

// OnboardPaymentIntegration — op 2.1. Temporal Workflow (entry; StartWorkflow, id
// {customerId}:onboard). Resolves the billing aggregate (deployedAppId → customerId
// via readBilling) → creates the connected account → records the binding → registers
// the per-customer closeBillingCycle Schedule. (Runtime payment-config wiring is an
// operations desired-state concern — OperationsManager's publishDesiredState — not a
// billing edge; see operational concept #2.)
// Idempotent on the id (a redundant start returns the running BillingRef). SYNC:
// returns once the onboarding workflow is durably accepted.
func (m *billingManager) OnboardPaymentIntegration(rc fwmgr.Context, deployedAppID deployedAppID) (BillingRef, error) {
	ctx := rc.Context
	if deployedAppID == uuid.Nil {
		return BillingRef{}, newError(fwmgr.ContractMisuse, "empty deployedAppId")
	}

	// The onboarding workflow resolves deployedAppId → customerId via readBilling
	// (§3.0 / §2.1); the workflow id is derived from the resolved customerId inside the
	// workflow's start. The Manager seeds the workflow id family on the deployedAppId
	// until the customer is resolved (a deterministic start token).
	wfID := onboardWorkflowID(deployedAppID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindOnboard, onboardInput{
		DeployedAppID: deployedAppID,
	})
	if err != nil {
		return BillingRef{}, mapStartError(err)
	}
	var result BillingRef
	if err := we.Get(ctx, &result); err != nil {
		return BillingRef{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// RegisterCustomer — op 2.2. Temporal Workflow (entry; StartWorkflow, id
// {customerId}:register). Validates the stored instrument (zero-amount auth) → opens
// the billing aggregate. Idempotent on the id. SYNC.
func (m *billingManager) RegisterCustomer(rc fwmgr.Context, customerID customerID) (BillingRef, error) {
	ctx := rc.Context
	if customerID == uuid.Nil {
		return BillingRef{}, newError(fwmgr.ContractMisuse, "empty customerId")
	}

	wfID := registerWorkflowID(customerID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindRegister, registerInput{
		CustomerID: customerID,
	})
	if err != nil {
		return BillingRef{}, mapStartError(err)
	}
	var result BillingRef
	if err := we.Get(ctx, &result); err != nil {
		return BillingRef{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// CloseBillingCycle — op 2.3. Temporal Workflow (entry; scheduler-triggered via the
// per-customer closeBillingCycle:<customerId> Schedule; id {customerId}:{cycleId}:close
// — the continuity token chargeback Signals target). Reads revenue + usage → computes
// the signed net + routing directive in-workflow by value → executes the directive
// (payout/charge/skip; on charge failure decides+executes {Retry|Escalate|Delay}) →
// records the outcome. Idempotent on the id (a redundant firing collapses to the
// running close). SYNC from the scheduler's POV.
func (m *billingManager) CloseBillingCycle(rc fwmgr.Context, customerID customerID, cycleID cycleID) (CloseCycleResult, error) {
	ctx := rc.Context
	if customerID == uuid.Nil {
		return CloseCycleResult{}, newError(fwmgr.ContractMisuse, "empty customerId")
	}
	if cycleID == "" {
		return CloseCycleResult{}, newError(fwmgr.ContractMisuse, "empty cycleId")
	}

	wfID := closeWorkflowID(customerID, cycleID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindClose, closeInput{
		CustomerID: customerID,
		CycleID:    cycleID,
	})
	if err != nil {
		return CloseCycleResult{}, mapStartError(err)
	}
	var result CloseCycleResult
	if err := we.Get(ctx, &result); err != nil {
		return CloseCycleResult{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// RunShortfallSweep — op 2.4. Temporal Workflow (entry; scheduler-triggered via the
// hourly shortfallSweep Schedule; id :all:shortfallSweep:{tickId}). Reads the
// persistently-delinquent customer set and, for each, delivers a queued
// applyDelinquencyPolicy Signal to operationsManager (the single sanctioned queued M→M
// edge). Does NOT pause/withdraw apps itself. SYNC.
func (m *billingManager) RunShortfallSweep(rc fwmgr.Context, tickID string) (ShortfallSweepResult, error) {
	ctx := rc.Context
	if tickID == "" {
		return ShortfallSweepResult{}, newError(fwmgr.ContractMisuse, "empty tickId")
	}

	wfID := shortfallSweepWorkflowID(tickID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindShortfallSweep, shortfallSweepInput{})
	if err != nil {
		return ShortfallSweepResult{}, mapStartError(err)
	}
	var result ShortfallSweepResult
	if err := we.Get(ctx, &result); err != nil {
		return ShortfallSweepResult{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// RecordInboundRevenue — op 2.5. Temporal Signal (inboundRevenueReceived, to the
// affected cycle's workflow id {customerId}:{cycleId}:close; signal-with-start when the
// cycle workflow is not yet running). The targeted cycle workflow appends the revenue
// fact to the Revenue Ledger (idempotent on the gateway event id). SYNC from the
// Client's POV: returns once the signal is durably enqueued. Signature verified
// upstream — this façade does not re-verify.
func (m *billingManager) RecordInboundRevenue(rc fwmgr.Context, event GatewayRevenueEvent) error {
	ctx := rc.Context
	if event.CustomerID == uuid.Nil {
		return newError(fwmgr.ContractMisuse, "empty customerId")
	}
	if event.CycleID == "" {
		return newError(fwmgr.ContractMisuse, "empty cycleId")
	}
	if event.GatewayEventID == "" {
		return newError(fwmgr.ContractMisuse, "empty gatewayEventId")
	}

	wfID := closeWorkflowID(event.CustomerID, event.CycleID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, signalInboundRevenueReceived, event,
		opts, executionKindClose, closeInput{CustomerID: event.CustomerID, CycleID: event.CycleID})
	if err != nil {
		return mapSignalError(err)
	}
	return nil
}

// RecordRevenueReversal — op 2.6. Temporal Signal (chargebackReceived, to the affected
// cycle's workflow id {customerId}:{cycleId}:close; signal-with-start → the cycle
// workflow re-derives the forward-only recompute when the original close has
// completed). The cycle workflow appends the reversal (idempotent on the chargeback's
// gateway event id), recomputes the net forward-only, records the correction, and
// routes the delta. Compensation is forward-only; no rollback. SYNC.
func (m *billingManager) RecordRevenueReversal(rc fwmgr.Context, event GatewayReversalEvent) error {
	ctx := rc.Context
	if event.CustomerID == uuid.Nil {
		return newError(fwmgr.ContractMisuse, "empty customerId")
	}
	if event.CycleID == "" {
		return newError(fwmgr.ContractMisuse, "empty cycleId")
	}
	if event.GatewayEventID == "" {
		return newError(fwmgr.ContractMisuse, "empty gatewayEventId")
	}

	wfID := closeWorkflowID(event.CustomerID, event.CycleID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, signalChargebackReceived, event,
		opts, executionKindClose, closeInput{CustomerID: event.CustomerID, CycleID: event.CycleID})
	if err != nil {
		return mapSignalError(err)
	}
	return nil
}

// --- workflow id derivation (continuity tokens; billingManager.md §6.1) ----

// onboardWorkflowID derives the onboarding workflow id. The contract names it
// {customerId}:onboard once resolved; the Manager seeds the start token on the
// deployedAppId (resolved to the customer inside the workflow). The id family is
// deterministic so a redundant start collapses (§6.1 / §2.1).
func onboardWorkflowID(deployedAppID deployedAppID) string {
	return fmt.Sprintf("%s:onboard", deployedAppID)
}

// registerWorkflowID derives {customerId}:register.
func registerWorkflowID(customerID customerID) string {
	return fmt.Sprintf("%s:register", customerID)
}

// closeWorkflowID derives {customerId}:{cycleId}:close — the continuity token the
// inbound/reversal/chargeback Signals target (§6.1).
func closeWorkflowID(customerID customerID, cycleID cycleID) string {
	return fmt.Sprintf("%s:%s:close", customerID, cycleID)
}

// shortfallSweepWorkflowID derives :all:shortfallSweep:{tickId} (schedule firing id =
// workflow id, Temporal-native firing idempotency; §6.1).
func shortfallSweepWorkflowID(tickID string) string {
	return fmt.Sprintf(":all:shortfallSweep:%s", tickID)
}

// --- error mapping at the façade boundary (billingManager.md §3.1) ---------

func mapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; any other
	// error is treated as an infrastructure fault at the transport layer.
	return newError(fwmgr.Infrastructure, err.Error())
}

func mapSignalError(err error) error {
	if isNotFound(err) {
		return newError(fwmgr.NotFound, err.Error())
	}
	return newError(fwmgr.Infrastructure, err.Error())
}

// isNotFound reports whether the Temporal error indicates the addressed execution does
// not exist (mirrors the operations/construction matcher).
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errNotFoundSentinel) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "NotFound")
}

var errNotFoundSentinel = errors.New("not found")

// ---------------------------------------------------------------------------
// Identity & canonical types (billingManager.md §3.0 — THE MATERIAL RULING).
//
// CANONICAL billing aggregate identity = CustomerID, typed uuid.UUID. This
// ratifies the two frozen ledgers (revenueLedgerAccess / usageAccess, already
// CustomerID(uuid)) in place and forces the design-only billingStateAccess to
// migrate BillingID(string) → CustomerID(uuid) additively (FU-MST-1). We do NOT
// reintroduce BillingID(string). CycleID stays string (all three ledgers agree).
// DeployedAppID is the operations-side operated-app identity (NOT the billing
// key); the onboarding workflow resolves DeployedAppID → CustomerID via
// billingStateAccess.readBilling.
// ---------------------------------------------------------------------------

// CustomerID is the canonical billing aggregate identity (billingManager.md
// §3.0). One billing aggregate per customer; shared by revenueLedgerAccess,
// usageAccess, and (post FU-MST-1) billingStateAccess.
type customerID = uuid.UUID

// CycleID is the billing cycle a billing folds at close. Agreed string across
// revenueLedgerAccess / usageAccess / billingStateAccess (billingManager.md §3.0).
type cycleID = string

// DeployedAppID is the operated-app identity owned by the operations side; it is
// NOT the billing aggregate key. op 2.1 resolves it to a CustomerID
// (billingManager.md §3.0 / §2.1).
type deployedAppID = uuid.UUID

// ---------------------------------------------------------------------------
// Money — exact integer minor units + currency. NEVER a float (billingManager.md
// §3 / revenueLedgerAccess §3.1 / billingEngine §3). Signed: positive == payout to
// the customer, negative == shortfall charge. This Manager moves money via
// merchantGatewayAccess by VALUE; the math is the Engine's, never re-derived here.
// ---------------------------------------------------------------------------

// Money is a signed amount in minor units (cents) plus an ISO-4217 currency. The
// shared money value type the Engine produces and this Manager routes.

// signed; e.g. 1299 == 12.99; reversals carry a negative value
// ISO-4217, e.g. "USD"

// ---------------------------------------------------------------------------
// RoutingDirective — the façade's OWN copy of which way the signed net routed
// (billingManager.md §3). The canonical decision is billingEngine-owned
// (billingEngine.md §3), MIRRORED at the Manager-local seam (deps.go
// RoutingDirectiveSeam) the Engine returns; this OWN enum is what the public
// CloseCycleResult exposes. FULL ENCAPSULATION: the generated contract must carry
// billing's OWN type, not the deps.go seam, so the workflow converts at the
// boundary (RoutingDirective(seam)). The Engine STATES the directive; this Manager
// EXECUTES it (billingManager.md §0 decision 2). The iota order matches the seam.
// ---------------------------------------------------------------------------

// RoutingDirective is which way the signed net routed, on the public façade result
// (billingManager.md §3). A VALUE the Manager records after executing the Engine's
// routing decision against merchantGatewayAccess. The canonical-name lookup lives in
// behavior.go as the free function routingDirectiveName (so the generated enum carries
// no behavior).

// RoutingDirectiveNoAction is net == 0 (or a recompute delta == 0) — skipped.

// RoutingDirectivePayout is net > 0 — payoutCustomer was called.

// RoutingDirectiveCharge is net < 0 — chargeCustomer was called.

// ---------------------------------------------------------------------------
// Public façade return values (billingManager.md §3). These are this Manager's
// own view types — NOT persisted head-state. The persisted shapes (BillingOutcome,
// RevenueEntry, ...) are owned by their RA/Engine and referenced via deps.go seams,
// never redefined here.
// ---------------------------------------------------------------------------

// BillingRef is the continuity token returned by onboarding / registration
// (billingManager.md §3).

// CloseCycleResult is the result of CloseBillingCycle (billingManager.md §3).
// SignedNet is NOT surfaced raw — it is recorded in billingStateAccess; the read
// path is billingStateAccess.readBilling (the CQRS split, §6.6). Routed states
// which directive the Manager executed.

// Escalated is true when the charge failed and interventionEngine returned
// Escalate (the customer is flagged delinquent on head-state; OQ-4 / §6.3). The
// operator dashboard reads it via billingStateAccess.readBilling.

// ShortfallSweepResult is the result of RunShortfallSweep (billingManager.md §3).
// SignalledCustomers may be empty — a quiet sweep is a normal outcome.

// ---------------------------------------------------------------------------
// Webhook payload inputs (billingManager.md §3). These façade input types carry
// the (upstream-signature-verified) webhook body the Manager maps onto the
// revenueLedgerAccess-owned RevenueEntry / ReversalEntry at append time. The
// persisted shapes are owned by revenueLedgerAccess (deps.go seams), not redefined.
// ---------------------------------------------------------------------------

// GatewayRevenueEvent is the verified inbound-revenue webhook body (op 2.5). The
// gateway event id is the append's dedup token (revenueLedgerAccess dedups on it).

// globally-unique dedup token

// signed minor units + currency (inbound: positive)
// gateway-supplied

// GatewayReversalEvent is the verified chargeback/reversal webhook body (op 2.6). The
// chargeback's own gateway event id is the dedup token; ReversesGatewayEventID is an
// optional back-link to the inbound entry it reverses.

// the chargeback's own dedup token

// negative minor units + currency

// ---------------------------------------------------------------------------
// Façade error model (billingManager.md §3.1).
// CALLER/PROGRAMMER errors at the façade boundary — distinct from the workflow's own
// failure handling (Temporal RetryPolicy + the interventionEngine decide→execute
// alternative paths + the forward-only chargeback compensation inside the workflow
// body). Kinds used: ContractMisuse, FailedPrecondition, NotFound, Unauthorized,
// Infrastructure.
// ---------------------------------------------------------------------------

func newError(kind fwmgr.Kind, detail string) *fwmgr.Error {
	return fwmgr.New(kind, detail)
}

// This file declares billingManager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom) for the one collaborator still reached through a
// Manager-local seam, plus the seam data types it carries:
//
//   - DurableExecutionAccess — exists as internal/resourceaccess/durableexecution;
//     consumed here via a NARROW seam interface (RegisterSchedule only, for the
//     startup shortfallSweep registration). The composition root adapts the concrete
//     *durableexecution.Runtime (durableAdapter, adapters.go).
//
// Every other collaborator — billingStateAccess, usageAccess, merchantGatewayAccess,
// revenueLedgerAccess, billingengine.BillingEngine, intervention.InterventionEngine —
// is reached directly through the generated typed invokers/Activities and their
// published contracts (workflow.go); no Manager-local seam or data mirror remains for
// any of them (see the per-collaborator notes below). The in-workflow awaitSignal
// primitive (the inbound/reversal/chargeback waits) is the Manager's OWN workflow code
// (D-DA category A), NOT an RA method.

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
// revenueLedgerAccess — B7: the former Manager-local seam (interface +
// entryRefSeam/revenueEntrySeam/reversalEntrySeam mirrors) and the three custom
// Temporal Activities that wrapped it (activities_custom.go) are RETIRED. The
// close/recompute workflow spine now reaches this RA through the generated typed
// invokers (invokers.gen.go) and speaks the generated revenueledger contract types
// (RevenueEntry, ReversalEntry, EntryRef, RevenueKind) directly, with no Manager-local
// wrapper — the same discipline billingStateAccess already followed. The append-only
// dedup semantics (idempotent on entry.GatewayEventID; NO Conflict kind) are unchanged.
// ===========================================================================

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

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// ResourceAccess interfaces (the dependencies the GENERATED constructor
// NewBillingManager receives) to the Manager's unexported downstream seams (deps.go),
// plus the two REAL Engine-contract divergence bridges (termsToEngine,
// routingDirectiveToState). Per the founder DI model (2026-06-28) these were retired
// from cmd/server and live HERE, in the one package that knows both sides — the
// Manager depends on each dependency's PUBLISHED interface and adapts it internally
// (Option-B boundary mapping), exactly as operations/construction fold their adapters.

// revenueLedgerAccess (B7): the former Manager-local seam + noopRevenueLedger stub
// adapter are RETIRED. The workflow now reaches this RA through the generated typed
// invokers (invokers.gen.go), speaking revenueledger.RevenueEntry/ReversalEntry/EntryRef
// directly — no adapter needed (see workermanifest.go WorkerManifest, which threads
// m.revenueLedger straight into genActivities.RevenueLedger).

// ===========================================================================
// billingStateAccess contract converters. The former billingStateAdapter struct AND
// the Manager-local billingHead/billingOutcomeSeam/gatewayBindingSeam/customerSummary/
// delinquencyScope/version mirrors are retired — the workflow reaches the RA through
// the generated invokers (invokers.gen.go) and speaks the billingstate contract types
// (Billing, BillingTerms, BillingOutcome, CustomerSummary, DelinquencyScope,
// GatewayBinding, Version) directly. The two converters below are the REAL
// divergences that remain: the engine-owned BillingTerms/RoutingDirective are
// distinct named types from their billingstate counterparts.
// ===========================================================================

// ===========================================================================
// durableExecutionAccess adapter — over durableexecution.DurableExecutionAccess. Only
// the startup RegisterSchedule verb is consumed (the platform-wide shortfallSweep; the
// workflow-invoked deliverSignal + per-customer registerSchedule now go through the
// generated invokers). The published ScheduleSpec resolves the task queue via its
// KindBinding table, so the seam's TaskQueue is not threaded.
// ===========================================================================

type durableAdapter struct {
	inner durableexecution.DurableExecutionAccess
}

var _ durableExecutionAccess = durableAdapter{}

func (a durableAdapter) RegisterSchedule(ctx context.Context, spec scheduleSpec) error {
	return a.inner.RegisterSchedule(
		fwra.Context{Context: ctx},
		durableexecution.ScheduleID(spec.ID),
		durableexecution.ScheduleSpec{
			ExecutionKind: durableexecution.ExecutionKind(spec.WorkflowType),
			Cadence:       durableexecution.Cadence{Every: time.Duration(spec.IntervalSecs) * time.Second},
		},
	)
}

// ===========================================================================
// billingEngine / interventionEngine adapters — RETIRED. The workflow calls the
// published billingengine.BillingEngine / intervention.InterventionEngine contracts
// DIRECTLY (workflow.go), passing fweng.Context{Context: context.Background()} inline
// at each call site. termsToEngine + routingDirectiveToState above remain the two REAL
// divergence bridges.
// ===========================================================================

// This file holds the Workflows struct (the Manager's downstream dependency set), the
// four workflow bodies (the encapsulated BillingWorkflow volatility — billingManager.md
// §6.3), the workflow-level Conflict re-read→re-apply loop (§6.5), the forward-only
// chargeback recompute saga, and the RA-boundary fold helpers.
//
// How the two dependency kinds are reached differs by determinism class:
//   - The two Engines (billingengine.BillingEngine / intervention.InterventionEngine —
//     the PUBLISHED contracts, no Manager-local seam) are PURE, deterministic, called
//     DIRECTLY in-workflow by value (no Activity wrapper — replay-safe), with
//     fweng.Context{Context: context.Background()} supplied inline at each call site.
//   - The ResourceAccess layer is I/O and NON-deterministic; the workflow reaches it
//     ONLY through the generated typed invoker surface (Acts, invokers.gen.go),
//     including the three revenueLedgerAccess ops — so no hand file in this package
//     names an activity by string (arch_activitynames_test.go proves it).

// wfDeps bundles every downstream dependency the billingManager orchestrates, passed to
// newWorkflows (from WorkerManifest, workermanifest.go) and held on the Workflows struct.
// The two Engines are typed as their PUBLISHED contract interfaces (no Manager-local
// seam), called DIRECTLY in-workflow. The ResourceAccess layer is reached through the
// generated typed invokers (Acts).
type wfDeps struct {
	Billing      billingengine.BillingEngine
	Intervention intervention.InterventionEngine

	// Acts is the generated workflow-side invoker surface (invokers.gen.go): one method
	// per ResourceAccess activity, carrying contract types. Its Opts hook supplies the
	// per-activity option presets (workermanifest.go).
	Acts genInvokers
}

// workflows is the single billingManager component struct — the workflow receiver. The
// RA activities are the generated genActivities (activities.gen.go); this struct reaches
// them through the typed invokers (Acts).
type workflows struct {
	Billing      billingengine.BillingEngine
	Intervention intervention.InterventionEngine

	Acts genInvokers
}

// newWorkflows builds the Workflows receiver from the injected wfDeps.
func newWorkflows(d wfDeps) *workflows {
	return &workflows{
		Billing:      d.Billing,
		Intervention: d.Intervention,
		Acts:         d.Acts,
	}
}

// Bounds (in-workflow guards; NOT contract surface).
const (
	// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply loop
	// (§6.5).
	maxMutateConflictAttempts = 20

	// maxChargeRetries bounds the in-workflow Retry directive re-charge budget (OQ-4;
	// the external-gateway retry budget). The Activity RetryPolicy handles transport
	// retries; this bounds the intervention-decided re-charges.
	maxChargeRetries = 5
)

// raConflictErrType is the canonical Temporal Type() a head-state mutation Activity
// surfaces when expectedVersion is stale; the workflow recovers with the bounded
// re-read→re-apply loop (§6.5).
var raConflictErrType = fwmgr.RAErrType(fwra.Conflict)

// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the billingManager impl.
// It supplies the genWorkerManifest (the workflow set codegen cannot know, the
// per-activity option presets, and the genActivities dep threading), the external
// RegisterManagerWorker entrypoint the composition root calls, and the startup
// Schedule registration.

// ---------------------------------------------------------------------------
// Registered workflow names (billingManager.md §6.2). Stable — the continuity
// tokens the façade (billingmanager.go) starts workflows under.
// ---------------------------------------------------------------------------

const (
	// executionKindOnboard is the UC5 payment-integration onboarding workflow.
	executionKindOnboard = "billingOnboardPayment"
	// executionKindRegister is the ncuc1 customer-registration workflow.
	executionKindRegister = "billingRegisterCustomer"
	// executionKindClose is the UC6 cycle-close workflow (also hosts the inbound/
	// chargeback Signals and the forward-only recompute saga).
	executionKindClose = "billingCloseCycle"
	// executionKindShortfallSweep is the ncuc5 shortfall-sweep workflow.
	executionKindShortfallSweep = "billingShortfallSweep"
)

// Schedule ids + cadence (billingManager.md §6.1; operational-concepts.md §4).
const (
	// scheduleIDCloseCyclePrefix is the per-customer cycle-close Schedule id prefix; the
	// full id is "closeBillingCycle:<customerId>" (registered at onboarding, op 2.1).
	scheduleIDCloseCyclePrefix = "closeBillingCycle"

	// scheduleIDShortfallSweep is the platform-wide shortfall-sweep Schedule id
	// (registered at startup).
	scheduleIDShortfallSweep = "shortfallSweep"

	// closeCycleDefaultIntervalSecs is the default per-customer cycle cadence (daily) the
	// onboarding Schedule registers; the real cadence is derived from the customer's
	// BillingSchedule (operational-concepts.md §4 line 113). Default = 24h.
	closeCycleDefaultIntervalSecs = 24 * 60 * 60

	// shortfallSweepIntervalSecs is the hourly shortfall-sweep cadence (1h;
	// operational-concepts.md §4).
	shortfallSweepIntervalSecs = 60 * 60
)

// ---------------------------------------------------------------------------
// Per-activity option presets (billingManager.md §6.4). Concrete RetryPolicy /
// timeout choices live here, in the Manager, keyed by the generated registered
// activity name; the generated invoker's Opts hook applies them per call. FU-MST-4
// (named RetryPolicy library) is not yet landed; the inline §6.4 parameters are used.
// ---------------------------------------------------------------------------

// readHeadActivityOptions — billing head-state pure reads (10s; terminal
// NotFound/ContractMisuse).
func readHeadActivityOptions() workflow.ActivityOptions {
	return fwmgr.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// recordHeadActivityOptions — billing head-state write transitions (10s; terminal
// NotFound/ContractMisuse; Conflict is surfaced for the workflow-level re-read loop, so
// it is NOT non-retryable here — the workflow body recovers it).
func recordHeadActivityOptions() workflow.ActivityOptions {
	return fwmgr.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse, fwra.Conflict},
	}.Options()
}

// ledgerActivityOptions — revenueLedgerAccess / usageAccess appends + reads (30s;
// terminal ContractMisuse). Append-only ledgers: NO Conflict (gateway/runtime-event-id
// idempotent).
func ledgerActivityOptions() workflow.ActivityOptions {
	return fwmgr.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
}

// gatewayActivityOptions — merchantGatewayAccess money movements (externalGateway; small
// budget; terminal Auth/NotFound/ContractMisuse → decideOnBillingFailure). Stripe-native
// dedup on the Manager-supplied Idempotency-Key.
func gatewayActivityOptions() workflow.ActivityOptions {
	return fwmgr.ActivityPreset{
		Timeout:     30 * time.Second,
		MaxAttempts: 3,
		TerminalRA:  []fwra.Kind{fwra.Auth, fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// durableActivityOptions — durableExecutionAccess deliverSignal / registerSchedule (30s;
// terminal NotFound/ContractMisuse).
func durableActivityOptions() workflow.ActivityOptions {
	return fwmgr.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// activityOptions returns the option-preset hook the generated invokers consult. A
// name with no entry falls back to the generated default (invokers.gen.go). Keyed by the
// generated registered activity name (<componentKey>.<opName>).
func activityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"billingStateAccess.readBilling":                         readHeadActivityOptions(),
		"billingStateAccess.readPersistentlyDelinquentCustomers": readHeadActivityOptions(),
		"billingStateAccess.registerCustomer":                    recordHeadActivityOptions(),
		"billingStateAccess.bindGatewayLive":                     recordHeadActivityOptions(),
		"billingStateAccess.settleCycle":                         recordHeadActivityOptions(),
		"billingStateAccess.resettleCycle":                       recordHeadActivityOptions(),
		"usageAccess.readRange":                                  ledgerActivityOptions(),
		"revenueLedgerAccess.readRange":                          ledgerActivityOptions(),
		"revenueLedgerAccess.recordInboundRevenue":               ledgerActivityOptions(),
		"revenueLedgerAccess.recordReversal":                     ledgerActivityOptions(),
		"merchantGatewayAccess.payoutCustomer":                   gatewayActivityOptions(),
		"merchantGatewayAccess.chargeCustomer":                   gatewayActivityOptions(),
		"merchantGatewayAccess.createConnectedAccount":           gatewayActivityOptions(),
		"merchantGatewayAccess.validateStoredInstrument":         gatewayActivityOptions(),
		"durableExecutionAccess.deliverSignal":                   durableActivityOptions(),
		"durableExecutionAccess.registerSchedule":                durableActivityOptions(),
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go)
// consumes: the four workflow bodies under their registered names, the per-activity
// option-preset hook, and the genActivities threaded from the impl's stored published
// deps. The three revenue-ledger Activities are the GENERATED revenueLedgerAccess.*
// activities (activities.gen.go/worker.gen.go), reached workflow-side through
// wf.Acts (invokers.gen.go) exactly like every other RA.
//
// Unlike operations, billing's durableExecutionAccess IS wired into genActivities: the
// close-schedule registration (op 2.1) and the queued applyDelinquencyPolicy cross-
// Manager signal (ncuc5) are workflow-invoked generated activities. (The startup
// shortfallSweep Schedule is still registered directly via RegisterSchedules, not
// through a workflow.)
func (m *billingManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()
	wf := newWorkflows(wfDeps{
		Billing:      m.billing,
		Intervention: m.intervention,
		Acts:         genInvokers{Opts: optsHook},
	})

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindOnboard, Fn: wf.OnboardWorkflow},
			{Name: executionKindRegister, Fn: wf.RegisterCustomerWorkflow},
			{Name: executionKindClose, Fn: wf.CloseCycleWorkflow},
			{Name: executionKindShortfallSweep, Fn: wf.ShortfallSweepWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			BillingState:     m.billingState,
			Usage:            m.usage,
			MerchantGateway:  m.merchantGateway,
			DurableExecution: m.durableExecution,
			RevenueLedger:    m.revenueLedger,
		},
	}
}

// RegisterManagerWorker wires the billingManager onto a Temporal Worker polling the
// billing task queue (billingManager.md §6.1). It preserves the external call shape the
// composition root used before the generated-layer migration, asserting to the concrete
// *billingManager the generated constructor returns and delegating to the generated
// RegisterWorker with the impl's WorkerManifest.
func RegisterManagerWorker(w worker.Worker, m BillingManager) {
	impl, ok := m.(*billingManager)
	if !ok {
		panic("billing: RegisterManagerWorker requires a *billingManager from NewBillingManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}

// RegisterSchedules registers (idempotently) the platform-wide shortfallSweep (hourly)
// Temporal Schedule at startup via durableExecutionAccess (billingManager.md §6.1;
// FU-MST-3). Called once at process start. The per-customer closeBillingCycle:<customerId>
// Schedule is NOT registered here — it is registered per-customer at onboarding (op 2.1,
// via the generated durableExecutionAccess.registerSchedule activity).
func RegisterSchedules(ctx context.Context, durable durableexecution.DurableExecutionAccess) error {
	return durableAdapter{inner: durable}.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDShortfallSweep,
		WorkflowType: executionKindShortfallSweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: shortfallSweepIntervalSecs,
	})
}
