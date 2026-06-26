package settlement

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// SettlementManager is the settlementManager port — the public use-case surface of the
// façade (settlementManager.md §2). Each op leads with the Manager-layer call Context
// (fwmgr.Context, embedding context.Context + the Principal); the *Manager derives
// ctx := rc.Context inside.
//
// SCHEMA-FIRST: this interface (and the port I/O types) are GENERATED into
// contract.gen.go from contract.schema.json (edit the schema + `make gen`; do NOT
// hand-edit the generated surface). The concrete *Manager below satisfies it; the
// consumer-side dependency interfaces (deps.go) and the Temporal Workflows struct
// (workflow.go) stay hand-written and are NOT part of this contract.

// Compile-time proof the concrete Manager satisfies the SettlementManager port. Each
// op leads with the Manager-layer call Context (fwmgr.Context); the *Manager derives
// ctx := rc.Context inside.
var _ SettlementManager = (*Manager)(nil)

// Manager is the settlementManager façade. It exposes the six public use-case ops
// (settlementManager.md §2) and OWNS Temporal. The Temporal-backed ops:
//   - OnboardPaymentIntegration — Workflow (entry; StartWorkflow, id {customerId}:onboard)
//   - RegisterCustomer          — Workflow (entry; StartWorkflow, id {customerId}:register)
//   - CloseSettlementCycle      — Workflow (entry; StartWorkflow, id {customerId}:{cycleId}:close)
//   - RunShortfallSweep         — Workflow (entry; StartWorkflow, id :all:shortfallSweep:{tickId})
//   - RecordInboundRevenue      — Signal (SignalWithStart inboundRevenueReceived → close id)
//   - RecordRevenueReversal     — Signal (SignalWithStart chargebackReceived → close id)
//
// The Manager holds only a Temporal client; the façade pre-condition checks (non-empty
// ids) are enforced here before any Temporal call (settlementManager.md §2/§3.1). The
// downstream Engines/RA are held on the Workflows struct (workflow.go), reached only
// from workflow code.
type Manager struct {
	client client.Client
}

// NewManager constructs a Manager over an existing Temporal client.
func NewManager(c client.Client) *Manager {
	return &Manager{client: c}
}

// OnboardPaymentIntegration — op 2.1. Temporal Workflow (entry; StartWorkflow, id
// {customerId}:onboard). Resolves the settlement aggregate (deployedAppId → customerId
// via readSettlement) → creates the connected account → wires runtime payment config →
// records the binding → registers the per-customer closeSettlementCycle Schedule.
// Idempotent on the id (a redundant start returns the running SettlementRef). SYNC:
// returns once the onboarding workflow is durably accepted.
func (m *Manager) OnboardPaymentIntegration(rc fwmgr.Context, deployedAppID DeployedAppID) (SettlementRef, error) {
	ctx := rc.Context
	if deployedAppID == uuid.Nil {
		return SettlementRef{}, newError(fwmgr.ContractMisuse, "empty deployedAppId")
	}

	// The onboarding workflow resolves deployedAppId → customerId via readSettlement
	// (§3.0 / §2.1); the workflow id is derived from the resolved customerId inside the
	// workflow's start. The Manager seeds the workflow id family on the deployedAppId
	// until the customer is resolved (a deterministic start token).
	wfID := onboardWorkflowID(deployedAppID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindOnboard, OnboardInput{
		DeployedAppID: deployedAppID,
	})
	if err != nil {
		return SettlementRef{}, mapStartError(err)
	}
	var result SettlementRef
	if err := we.Get(ctx, &result); err != nil {
		return SettlementRef{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// RegisterCustomer — op 2.2. Temporal Workflow (entry; StartWorkflow, id
// {customerId}:register). Validates the stored instrument (zero-amount auth) → opens
// the settlement aggregate. Idempotent on the id. SYNC.
func (m *Manager) RegisterCustomer(rc fwmgr.Context, customerID CustomerID) (SettlementRef, error) {
	ctx := rc.Context
	if customerID == uuid.Nil {
		return SettlementRef{}, newError(fwmgr.ContractMisuse, "empty customerId")
	}

	wfID := registerWorkflowID(customerID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindRegister, RegisterInput{
		CustomerID: customerID,
	})
	if err != nil {
		return SettlementRef{}, mapStartError(err)
	}
	var result SettlementRef
	if err := we.Get(ctx, &result); err != nil {
		return SettlementRef{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// CloseSettlementCycle — op 2.3. Temporal Workflow (entry; scheduler-triggered via the
// per-customer closeSettlementCycle:<customerId> Schedule; id {customerId}:{cycleId}:close
// — the continuity token chargeback Signals target). Reads revenue + usage → computes
// the signed net + routing directive in-workflow by value → executes the directive
// (payout/charge/skip; on charge failure decides+executes {Retry|Escalate|Delay}) →
// records the outcome. Idempotent on the id (a redundant firing collapses to the
// running close). SYNC from the scheduler's POV.
func (m *Manager) CloseSettlementCycle(rc fwmgr.Context, customerID CustomerID, cycleID CycleID) (CloseCycleResult, error) {
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
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindClose, CloseInput{
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
func (m *Manager) RunShortfallSweep(rc fwmgr.Context, tickID string) (ShortfallSweepResult, error) {
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
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindShortfallSweep, ShortfallSweepInput{})
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
func (m *Manager) RecordInboundRevenue(rc fwmgr.Context, event GatewayRevenueEvent) error {
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
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, SignalInboundRevenueReceived, event,
		opts, ExecutionKindClose, CloseInput{CustomerID: event.CustomerID, CycleID: event.CycleID})
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
func (m *Manager) RecordRevenueReversal(rc fwmgr.Context, event GatewayReversalEvent) error {
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
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, SignalChargebackReceived, event,
		opts, ExecutionKindClose, CloseInput{CustomerID: event.CustomerID, CycleID: event.CycleID})
	if err != nil {
		return mapSignalError(err)
	}
	return nil
}

// --- workflow id derivation (continuity tokens; settlementManager.md §6.1) ----

// onboardWorkflowID derives the onboarding workflow id. The contract names it
// {customerId}:onboard once resolved; the Manager seeds the start token on the
// deployedAppId (resolved to the customer inside the workflow). The id family is
// deterministic so a redundant start collapses (§6.1 / §2.1).
func onboardWorkflowID(deployedAppID DeployedAppID) string {
	return fmt.Sprintf("%s:onboard", deployedAppID)
}

// registerWorkflowID derives {customerId}:register.
func registerWorkflowID(customerID CustomerID) string {
	return fmt.Sprintf("%s:register", customerID)
}

// closeWorkflowID derives {customerId}:{cycleId}:close — the continuity token the
// inbound/reversal/chargeback Signals target (§6.1).
func closeWorkflowID(customerID CustomerID, cycleID CycleID) string {
	return fmt.Sprintf("%s:%s:close", customerID, cycleID)
}

// shortfallSweepWorkflowID derives :all:shortfallSweep:{tickId} (schedule firing id =
// workflow id, Temporal-native firing idempotency; §6.1).
func shortfallSweepWorkflowID(tickID string) string {
	return fmt.Sprintf(":all:shortfallSweep:%s", tickID)
}

// --- error mapping at the façade boundary (settlementManager.md §3.1) ---------

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
