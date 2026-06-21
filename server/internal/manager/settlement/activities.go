package settlement

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// This file holds the Manager-owned Temporal Activity wrappers — one per ResourceAccess
// call the workflows make (settlementManager.md §6.4). They are METHODS ON THE Workflows
// STRUCT: there is no separate Activities type. The RA dependencies live as fields on
// Workflows (workflow.go) and are reached on the struct, but the calls run inside
// Temporal Activities because those RA operations are I/O / non-deterministic and would
// break replay determinism on the workflow goroutine. The two Engines (settlementEngine,
// interventionEngine) are deliberately NOT Activities: they are pure deterministic
// functions the workflow body calls directly (settlementManager.md §6.4 "NOT Activities").
//
// Each settlement head-state WRITE Activity derives idempotencyKey
// "${workflowId}:${activityId}" from the Temporal activity context (so the RA layer
// never reads Temporal context). The append-only ledger writes dedup on the gateway
// event id carried in the entry (revenueLedgerAccess.md §3). The gateway money moves
// dedup on the Manager-supplied Stripe Idempotency-Key settle:{customerId}:{cycleId}
// (settlementManager.md §6.4). Every result runs through fwmgr.MapError so terminal
// port failures surface to Temporal as terminal errors of their canonical Type().

// activityIdempotencyKey derives "${workflowId}:${activityId}" from the running
// Activity's info — the stable, distinct key each logical settlement head-state write
// needs (settlementManager.md §6.4/§6.5).
func activityIdempotencyKey(ctx context.Context) fwra.IdempotencyKey {
	info := activity.GetInfo(ctx)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%s", info.WorkflowExecution.ID, info.ActivityID))
}

// gatewayIdempotencyKey derives the Stripe Idempotency-Key settle:{customerId}:{cycleId}
// for the money-moving gateway Activities (settlementManager.md §6.4 line 264/706).
func gatewayIdempotencyKey(customerID CustomerID, cycleID CycleID) string {
	return fmt.Sprintf("settle:%s:%s", customerID, cycleID)
}

// =============================================================================
// settlementStateAccess (head-state) Activities.
// =============================================================================

// ReadSettlementActivity wraps settlementStateAccess.readSettlement. Pure whole-aggregate
// read; no idempotency key.
func (wf *Workflows) ReadSettlementActivity(ctx context.Context, customerID CustomerID) (Settlement, error) {
	return mapErr(wf.SettlementState.ReadSettlement(ctx, customerID))
}

// ReadDelinquentActivity wraps settlementStateAccess.readPersistentlyDelinquentCustomers.
// Pure cross-row read; no idempotency key.
func (wf *Workflows) ReadDelinquentActivity(ctx context.Context, scope DelinquencyScope) ([]CustomerSummary, error) {
	return mapErr(wf.SettlementState.ReadPersistentlyDelinquentCustomers(ctx, scope))
}

// RegisterCustomerArgs bundles the register head-state write inputs.
type RegisterCustomerArgs struct {
	CustomerID      CustomerID
	ExpectedVersion Version
}

// RegisterCustomerActivity wraps settlementStateAccess.registerCustomer. idempotencyKey =
// "${workflowId}:${activityId}"; a stale expectedVersion surfaces fwra.Conflict for the
// §6.5 re-read loop.
func (wf *Workflows) RegisterCustomerActivity(ctx context.Context, a RegisterCustomerArgs) (Version, error) {
	return mapErr(wf.SettlementState.RegisterCustomer(ctx, a.CustomerID, a.ExpectedVersion, CustomerProfileSeam{}, activityIdempotencyKey(ctx)))
}

// BindGatewayLiveArgs bundles the bind head-state write inputs.
type BindGatewayLiveArgs struct {
	CustomerID      CustomerID
	ExpectedVersion Version
	Binding         GatewayBindingSeam
}

// BindGatewayLiveActivity wraps settlementStateAccess.bindGatewayLive.
func (wf *Workflows) BindGatewayLiveActivity(ctx context.Context, a BindGatewayLiveArgs) (Version, error) {
	return mapErr(wf.SettlementState.BindGatewayLive(ctx, a.CustomerID, a.ExpectedVersion, a.Binding, activityIdempotencyKey(ctx)))
}

// SettleCycleArgs bundles the settle head-state write inputs.
type SettleCycleArgs struct {
	CustomerID      CustomerID
	ExpectedVersion Version
	CycleID         CycleID
	Outcome         SettlementOutcomeSeam
}

// SettleCycleActivity wraps settlementStateAccess.settleCycle (the money-affecting
// head-state outcome record). idempotencyKey = "${workflowId}:${activityId}"; the
// dedup-first ledger makes an idempotent replay a no-op success (§6.5).
func (wf *Workflows) SettleCycleActivity(ctx context.Context, a SettleCycleArgs) (Version, error) {
	return mapErr(wf.SettlementState.SettleCycle(ctx, a.CustomerID, a.ExpectedVersion, a.CycleID, a.Outcome, activityIdempotencyKey(ctx)))
}

// ResettleCycleArgs bundles the resettle head-state write inputs.
type ResettleCycleArgs struct {
	CustomerID      CustomerID
	ExpectedVersion Version
	CycleID         CycleID
	Correction      SettlementOutcomeSeam
}

// ResettleCycleActivity wraps settlementStateAccess.resettleCycle (the chargeback
// correction record).
func (wf *Workflows) ResettleCycleActivity(ctx context.Context, a ResettleCycleArgs) (Version, error) {
	return mapErr(wf.SettlementState.ResettleCycle(ctx, a.CustomerID, a.ExpectedVersion, a.CycleID, a.Correction, activityIdempotencyKey(ctx)))
}

// =============================================================================
// revenueLedgerAccess + usageAccess (append-only ledger) Activities.
// =============================================================================

// RecordInboundRevenueActivity wraps revenueLedgerAccess.recordInboundRevenue. Dedup-id
// idempotent on entry.GatewayEventID (NO Conflict on this append-only ledger).
func (wf *Workflows) RecordInboundRevenueActivity(ctx context.Context, entry RevenueEntrySeam) (EntryRefSeam, error) {
	return mapErr(wf.RevenueLedger.RecordInboundRevenue(ctx, entry))
}

// RecordReversalActivity wraps revenueLedgerAccess.recordReversal. Dedup-id idempotent
// on the chargeback's GatewayEventID.
func (wf *Workflows) RecordReversalActivity(ctx context.Context, reversal ReversalEntrySeam) (EntryRefSeam, error) {
	return mapErr(wf.RevenueLedger.RecordReversal(ctx, reversal))
}

// ReadRevenueRangeArgs bundles the revenue range-read inputs.
type ReadRevenueRangeArgs struct {
	CustomerID CustomerID
	CycleID    CycleID
}

// ReadRevenueRangeActivity wraps revenueLedgerAccess.readRange. Pure read; no key.
func (wf *Workflows) ReadRevenueRangeActivity(ctx context.Context, a ReadRevenueRangeArgs) ([]RevenueEntrySeam, error) {
	return mapErr(wf.RevenueLedger.ReadRange(ctx, a.CustomerID, a.CycleID))
}

// ReadUsageRangeActivity wraps usageAccess.readRange (whole cycle; OperatedAppID nil).
// Pure read; no key.
func (wf *Workflows) ReadUsageRangeActivity(ctx context.Context, query UsageRangeQuerySeam) ([]UsageEventSeam, error) {
	return mapErr(wf.Usage.ReadRange(ctx, query))
}

// =============================================================================
// merchantGatewayAccess (external gateway) Activities. The money moves; dedup on the
// Manager-supplied Stripe Idempotency-Key settle:{customerId}:{cycleId}.
// =============================================================================

// GatewayMoveArgs bundles a payout/charge money-move inputs.
type GatewayMoveArgs struct {
	CustomerID CustomerID
	CycleID    CycleID
	Amount     Money
}

// PayoutCustomerActivity wraps merchantGatewayAccess.payoutCustomer.
func (wf *Workflows) PayoutCustomerActivity(ctx context.Context, a GatewayMoveArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Gateway.PayoutCustomer(ctx, a.CustomerID, a.Amount, gatewayIdempotencyKey(a.CustomerID, a.CycleID)))
}

// ChargeCustomerActivity wraps merchantGatewayAccess.chargeCustomer. A terminal decline
// (RA Auth) surfaces to the workflow's decideOnSettlementFailure branch (OQ-4).
func (wf *Workflows) ChargeCustomerActivity(ctx context.Context, a GatewayMoveArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Gateway.ChargeCustomer(ctx, a.CustomerID, a.Amount, gatewayIdempotencyKey(a.CustomerID, a.CycleID)))
}

// CreateConnectedAccountActivity wraps merchantGatewayAccess.createConnectedAccount.
func (wf *Workflows) CreateConnectedAccountActivity(ctx context.Context, customerID CustomerID) (GatewayBindingSeam, error) {
	return mapErr(wf.Gateway.CreateConnectedAccount(ctx, customerID, fmt.Sprintf("onboard:%s", customerID)))
}

// ValidateStoredInstrumentActivity wraps merchantGatewayAccess.validateStoredInstrument
// (zero-amount auth at registration).
func (wf *Workflows) ValidateStoredInstrumentActivity(ctx context.Context, customerID CustomerID) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Gateway.ValidateStoredInstrument(ctx, customerID, fmt.Sprintf("validate:%s", customerID)))
}

// =============================================================================
// operatedRuntimeAccess Activity (onboarding runtime wiring).
// =============================================================================

// WirePaymentConfigArgs bundles the runtime-wiring inputs.
type WirePaymentConfigArgs struct {
	DeployedAppID DeployedAppID
	Binding       GatewayBindingSeam
}

// WirePaymentConfigActivity wraps operatedRuntimeAccess.wirePaymentConfig (git
// commit; content-idempotent on the "${workflowId}:${activityId}" key).
func (wf *Workflows) WirePaymentConfigActivity(ctx context.Context, a WirePaymentConfigArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.OperatedRuntime.WirePaymentConfig(ctx, a.DeployedAppID, a.Binding, activityIdempotencyKey(ctx)))
}

// =============================================================================
// durableExecutionAccess (category-B control-plane) Activities.
// =============================================================================

// DeliverDelinquencyArgs bundles the queued cross-Manager signal inputs.
type DeliverDelinquencyArgs struct {
	CustomerID       CustomerID
	PauseNotWithdraw bool
}

// DeliverDelinquencySignalActivity wraps durableExecutionAccess.deliverSignal — the one
// sanctioned queued M→M edge (applyDelinquencyPolicy → operationsManager). Fire-and-
// forget; dedup is the receiving handler's concern (D-DA §9 OQ3). The target is the
// customer's operations delinquency workflow ({customerId}:delinquency — operationsManager
// signal-with-start continuity token).
func (wf *Workflows) DeliverDelinquencySignalActivity(ctx context.Context, a DeliverDelinquencyArgs) (struct{}, error) {
	targetWorkflowID := fmt.Sprintf("%s:delinquency", a.CustomerID)
	return struct{}{}, fwmgr.MapError(wf.Durable.DeliverSignal(ctx, targetWorkflowID, signalApplyDelinquencyPolicy, deliverSignalPayload{
		CustomerID:       a.CustomerID,
		PauseNotWithdraw: a.PauseNotWithdraw,
	}))
}

// RegisterScheduleActivity wraps durableExecutionAccess.registerSchedule for the
// per-customer closeSettlementCycle:<customerId> Schedule (idempotent by id). Registered
// at onboarding (op 2.1).
func (wf *Workflows) RegisterScheduleActivity(ctx context.Context, customerID CustomerID) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Durable.RegisterSchedule(ctx, scheduleSpec{
		ID:           fmt.Sprintf("%s:%s", scheduleIDCloseCyclePrefix, customerID),
		WorkflowType: ExecutionKindClose,
		TaskQueue:    TaskQueue,
		IntervalSecs: closeCycleDefaultIntervalSecs,
	}))
}

// signalApplyDelinquencyPolicy is the cross-Manager signal name delivered to
// operationsManager (matches operations.SignalApplyDelinquencyPolicy). Declared here as
// a string literal to avoid a Manager→Manager package import (the edge is queued via
// durableExecutionAccess, not a direct call).
const signalApplyDelinquencyPolicy = "applyDelinquencyPolicy"
