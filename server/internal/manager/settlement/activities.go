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
func gatewayIdempotencyKey(customerID customerID, cycleID cycleID) string {
	return fmt.Sprintf("settle:%s:%s", customerID, cycleID)
}

// =============================================================================
// settlementStateAccess (head-state) Activities.
// =============================================================================

// ReadSettlementActivity wraps settlementStateAccess.readSettlement. Pure whole-aggregate
// read; no idempotency key.
func (wf *workflows) ReadSettlementActivity(ctx context.Context, customerID customerID) (settlementHead, error) {
	return mapErr(wf.SettlementState.ReadSettlement(ctx, customerID))
}

// ReadDelinquentActivity wraps settlementStateAccess.readPersistentlyDelinquentCustomers.
// Pure cross-row read; no idempotency key.
func (wf *workflows) ReadDelinquentActivity(ctx context.Context, scope delinquencyScope) ([]customerSummary, error) {
	return mapErr(wf.SettlementState.ReadPersistentlyDelinquentCustomers(ctx, scope))
}

// RegisterCustomerArgs bundles the register head-state write inputs.
type registerCustomerArgs struct {
	CustomerID      customerID
	ExpectedVersion version
}

// RegisterCustomerActivity wraps settlementStateAccess.registerCustomer. idempotencyKey =
// "${workflowId}:${activityId}"; a stale expectedVersion surfaces fwra.Conflict for the
// §6.5 re-read loop.
func (wf *workflows) RegisterCustomerActivity(ctx context.Context, a registerCustomerArgs) (version, error) {
	return mapErr(wf.SettlementState.RegisterCustomer(ctx, a.CustomerID, a.ExpectedVersion, customerProfileSeam{}, activityIdempotencyKey(ctx)))
}

// BindGatewayLiveArgs bundles the bind head-state write inputs.
type bindGatewayLiveArgs struct {
	CustomerID      customerID
	ExpectedVersion version
	Binding         gatewayBindingSeam
}

// BindGatewayLiveActivity wraps settlementStateAccess.bindGatewayLive.
func (wf *workflows) BindGatewayLiveActivity(ctx context.Context, a bindGatewayLiveArgs) (version, error) {
	return mapErr(wf.SettlementState.BindGatewayLive(ctx, a.CustomerID, a.ExpectedVersion, a.Binding, activityIdempotencyKey(ctx)))
}

// SettleCycleArgs bundles the settle head-state write inputs.
type settleCycleArgs struct {
	CustomerID      customerID
	ExpectedVersion version
	CycleID         cycleID
	Outcome         settlementOutcomeSeam
}

// SettleCycleActivity wraps settlementStateAccess.settleCycle (the money-affecting
// head-state outcome record). idempotencyKey = "${workflowId}:${activityId}"; the
// dedup-first ledger makes an idempotent replay a no-op success (§6.5).
func (wf *workflows) SettleCycleActivity(ctx context.Context, a settleCycleArgs) (version, error) {
	return mapErr(wf.SettlementState.SettleCycle(ctx, a.CustomerID, a.ExpectedVersion, a.CycleID, a.Outcome, activityIdempotencyKey(ctx)))
}

// ResettleCycleArgs bundles the resettle head-state write inputs.
type resettleCycleArgs struct {
	CustomerID      customerID
	ExpectedVersion version
	CycleID         cycleID
	Correction      settlementOutcomeSeam
}

// ResettleCycleActivity wraps settlementStateAccess.resettleCycle (the chargeback
// correction record).
func (wf *workflows) ResettleCycleActivity(ctx context.Context, a resettleCycleArgs) (version, error) {
	return mapErr(wf.SettlementState.ResettleCycle(ctx, a.CustomerID, a.ExpectedVersion, a.CycleID, a.Correction, activityIdempotencyKey(ctx)))
}

// =============================================================================
// revenueLedgerAccess + usageAccess (append-only ledger) Activities.
// =============================================================================

// RecordInboundRevenueActivity wraps revenueLedgerAccess.recordInboundRevenue. Dedup-id
// idempotent on entry.GatewayEventID (NO Conflict on this append-only ledger).
func (wf *workflows) RecordInboundRevenueActivity(ctx context.Context, entry revenueEntrySeam) (entryRefSeam, error) {
	return mapErr(wf.RevenueLedger.RecordInboundRevenue(ctx, entry))
}

// RecordReversalActivity wraps revenueLedgerAccess.recordReversal. Dedup-id idempotent
// on the chargeback's GatewayEventID.
func (wf *workflows) RecordReversalActivity(ctx context.Context, reversal reversalEntrySeam) (entryRefSeam, error) {
	return mapErr(wf.RevenueLedger.RecordReversal(ctx, reversal))
}

// ReadRevenueRangeArgs bundles the revenue range-read inputs.
type readRevenueRangeArgs struct {
	CustomerID customerID
	CycleID    cycleID
}

// ReadRevenueRangeActivity wraps revenueLedgerAccess.readRange. Pure read; no key.
func (wf *workflows) ReadRevenueRangeActivity(ctx context.Context, a readRevenueRangeArgs) ([]revenueEntrySeam, error) {
	return mapErr(wf.RevenueLedger.ReadRange(ctx, a.CustomerID, a.CycleID))
}

// ReadUsageRangeActivity wraps usageAccess.readRange (whole cycle; OperatedAppID nil).
// Pure read; no key.
func (wf *workflows) ReadUsageRangeActivity(ctx context.Context, query usageRangeQuerySeam) ([]usageEventSeam, error) {
	return mapErr(wf.Usage.ReadRange(ctx, query))
}

// =============================================================================
// merchantGatewayAccess (external gateway) Activities. The money moves; dedup on the
// Manager-supplied Stripe Idempotency-Key settle:{customerId}:{cycleId}.
// =============================================================================

// GatewayMoveArgs bundles a payout/charge money-move inputs.
type gatewayMoveArgs struct {
	CustomerID customerID
	CycleID    cycleID
	Amount     Money
}

// PayoutCustomerActivity wraps merchantGatewayAccess.payoutCustomer.
func (wf *workflows) PayoutCustomerActivity(ctx context.Context, a gatewayMoveArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Gateway.PayoutCustomer(ctx, a.CustomerID, a.Amount, gatewayIdempotencyKey(a.CustomerID, a.CycleID)))
}

// ChargeCustomerActivity wraps merchantGatewayAccess.chargeCustomer. A terminal decline
// (RA Auth) surfaces to the workflow's decideOnSettlementFailure branch (OQ-4).
func (wf *workflows) ChargeCustomerActivity(ctx context.Context, a gatewayMoveArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Gateway.ChargeCustomer(ctx, a.CustomerID, a.Amount, gatewayIdempotencyKey(a.CustomerID, a.CycleID)))
}

// CreateConnectedAccountActivity wraps merchantGatewayAccess.createConnectedAccount.
func (wf *workflows) CreateConnectedAccountActivity(ctx context.Context, customerID customerID) (gatewayBindingSeam, error) {
	return mapErr(wf.Gateway.CreateConnectedAccount(ctx, customerID, fmt.Sprintf("onboard:%s", customerID)))
}

// ValidateStoredInstrumentActivity wraps merchantGatewayAccess.validateStoredInstrument
// (zero-amount auth at registration).
func (wf *workflows) ValidateStoredInstrumentActivity(ctx context.Context, customerID customerID) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Gateway.ValidateStoredInstrument(ctx, customerID, fmt.Sprintf("validate:%s", customerID)))
}

// =============================================================================
// operatedRuntimeAccess Activity (onboarding runtime wiring).
// =============================================================================

// WirePaymentConfigArgs bundles the runtime-wiring inputs.
type wirePaymentConfigArgs struct {
	DeployedAppID deployedAppID
	Binding       gatewayBindingSeam
}

// WirePaymentConfigActivity wraps operatedRuntimeAccess.wirePaymentConfig (git
// commit; content-idempotent on the "${workflowId}:${activityId}" key).
func (wf *workflows) WirePaymentConfigActivity(ctx context.Context, a wirePaymentConfigArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.OperatedRuntime.WirePaymentConfig(ctx, a.DeployedAppID, a.Binding, activityIdempotencyKey(ctx)))
}

// =============================================================================
// durableExecutionAccess (category-B control-plane) Activities.
// =============================================================================

// DeliverDelinquencyArgs bundles the queued cross-Manager signal inputs.
type deliverDelinquencyArgs struct {
	CustomerID       customerID
	PauseNotWithdraw bool
}

// DeliverDelinquencySignalActivity wraps durableExecutionAccess.deliverSignal — the one
// sanctioned queued M→M edge (applyDelinquencyPolicy → operationsManager). Fire-and-
// forget; dedup is the receiving handler's concern (D-DA §9 OQ3). The target is the
// customer's operations delinquency workflow ({customerId}:delinquency — operationsManager
// signal-with-start continuity token).
func (wf *workflows) DeliverDelinquencySignalActivity(ctx context.Context, a deliverDelinquencyArgs) (struct{}, error) {
	targetWorkflowID := fmt.Sprintf("%s:delinquency", a.CustomerID)
	return struct{}{}, fwmgr.MapError(wf.Durable.DeliverSignal(ctx, targetWorkflowID, signalApplyDelinquencyPolicy, deliverSignalPayload{
		CustomerID:       a.CustomerID,
		PauseNotWithdraw: a.PauseNotWithdraw,
	}))
}

// RegisterScheduleActivity wraps durableExecutionAccess.registerSchedule for the
// per-customer closeSettlementCycle:<customerId> Schedule (idempotent by id). Registered
// at onboarding (op 2.1).
func (wf *workflows) RegisterScheduleActivity(ctx context.Context, customerID customerID) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Durable.RegisterSchedule(ctx, scheduleSpec{
		ID:           fmt.Sprintf("%s:%s", scheduleIDCloseCyclePrefix, customerID),
		WorkflowType: executionKindClose,
		TaskQueue:    TaskQueue,
		IntervalSecs: closeCycleDefaultIntervalSecs,
	}))
}

// signalApplyDelinquencyPolicy is the cross-Manager signal name delivered to
// operationsManager (matches operations.SignalApplyDelinquencyPolicy). Declared here as
// a string literal to avoid a Manager→Manager package import (the edge is queued via
// durableExecutionAccess, not a direct call).
const signalApplyDelinquencyPolicy = "applyDelinquencyPolicy"
