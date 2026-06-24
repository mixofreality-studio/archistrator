package billing

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/activity"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// This file holds the Manager-owned Temporal Activity wrappers — one per
// ResourceAccess call the workflows make (C-BM §6.4 — 10 Activities).
// They are METHODS ON THE Workflows STRUCT (no separate Activities type).
// The RA dependencies live as fields on Workflows (workflow.go) and are reached
// on the struct, but the calls run inside Temporal Activities because those RA
// operations are I/O / non-deterministic and would break replay determinism.
//
// The two Engines (billingEngine, interventionEngine) are deliberately NOT Activities:
// they are pure deterministic functions the workflow body calls directly (C-BM §6.4).
//
// Each WRITE Activity derives the idempotencyKey "${workflowId}:${activityId}" from
// the Temporal activity context (so the RA layer never reads Temporal context) via
// activityIdempotencyKey. The billingGatewayAccess calls derive the GatewayRequestKey
// the same way (a string cast of the same key).

// activityIdempotencyKey derives "${workflowId}:${activityId}" from the running
// Activity's info — the stable, distinct key each logical write needs (C-BM §6.4/§6.5).
func activityIdempotencyKey(ctx context.Context) fwra.IdempotencyKey {
	info := activity.GetInfo(ctx)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%s", info.WorkflowExecution.ID, info.ActivityID))
}

// gatewayRequestKey derives the GatewayRequestKey string for gateway calls.
// Same derivation as activityIdempotencyKey (C-BM Idempotency: GatewayRequestKey =
// ${workflowId}:${activityId}).
func gatewayRequestKey(ctx context.Context) string {
	return string(activityIdempotencyKey(ctx))
}

// =============================================================================
// billingStateAccess Activities — 4 of 10.
// =============================================================================

// ReadBillingAggregateActivity wraps billingStateAccess.readBillingAggregate. Pure
// read; no idempotency key.
func (wf *Workflows) ReadBillingAggregateActivity(ctx context.Context, customerID CustomerID) (BillingAggregate, error) {
	return mapErr(wf.BillingState.ReadBillingAggregate(ctx, customerID))
}

// ReadDelinquentCustomersActivity wraps billingStateAccess.readDelinquentCustomers.
// Pure cross-row read; no idempotency key.
func (wf *Workflows) ReadDelinquentCustomersActivity(ctx context.Context) ([]DelinquentCustomerSeam, error) {
	return mapErr(wf.BillingState.ReadDelinquentCustomers(ctx))
}

// OpenBillingAggregateArgs bundles the open-aggregate write inputs.
type OpenBillingAggregateArgs struct {
	CustomerID      CustomerID `json:"customerId"`
	ExpectedVersion Version    `json:"expectedVersion"`
}

// OpenBillingAggregateActivity wraps billingStateAccess.openBillingAggregate (additive
// write). idempotencyKey = "${workflowId}:${activityId}"; a stale expectedVersion
// surfaces fwra.Conflict for the §6.5 re-read loop.
func (wf *Workflows) OpenBillingAggregateActivity(ctx context.Context, a OpenBillingAggregateArgs) (Version, error) {
	return mapErr(wf.BillingState.OpenBillingAggregate(ctx, a.CustomerID, a.ExpectedVersion, activityIdempotencyKey(ctx)))
}

// RecordServiceInvoiceArgs bundles the record-invoice write inputs.
type RecordServiceInvoiceArgs struct {
	CustomerID      CustomerID         `json:"customerId"`
	ExpectedVersion Version            `json:"expectedVersion"`
	PeriodID        PeriodID           `json:"periodId"`
	Invoice         ServiceInvoiceSeam `json:"invoice"`
	Charged         bool               `json:"charged"`
}

// RecordServiceInvoiceActivity wraps billingStateAccess.recordServiceInvoice (additive
// write). idempotencyKey = "${workflowId}:${activityId}"; Conflict drives the §6.5 loop.
func (wf *Workflows) RecordServiceInvoiceActivity(ctx context.Context, a RecordServiceInvoiceArgs) (Version, error) {
	return mapErr(wf.BillingState.RecordServiceInvoice(ctx, a.CustomerID, a.ExpectedVersion, a.PeriodID, a.Invoice, a.Charged, activityIdempotencyKey(ctx)))
}

// =============================================================================
// usageAccess Activity — 1 of 10.
// =============================================================================

// ReadPeriodUsageArgs bundles the period-usage read inputs.
type ReadPeriodUsageArgs struct {
	CustomerID CustomerID `json:"customerId"`
	PeriodID   PeriodID   `json:"periodId"`
}

// ReadPeriodUsageActivity wraps usageAccess.readPeriodUsage. Pure read; no key.
func (wf *Workflows) ReadPeriodUsageActivity(ctx context.Context, a ReadPeriodUsageArgs) (PeriodUsageSeam, error) {
	return mapErr(wf.Usage.ReadPeriodUsage(ctx, a.CustomerID, a.PeriodID))
}

// =============================================================================
// billingGatewayAccess Activities — 2 of 10.
// =============================================================================

// ValidateStoredInstrumentActivity wraps billingGatewayAccess.validateStoredInstrument
// (zero-amount auth). GatewayRequestKey = "${workflowId}:${activityId}" (Stripe-native
// dedup). A hard decline surfaces as ContentPolicy (terminal; workflow handles it).
func (wf *Workflows) ValidateStoredInstrumentActivity(ctx context.Context, customerID CustomerID) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Gateway.ValidateStoredInstrument(ctx, customerID, gatewayRequestKey(ctx)))
}

// ChargeUserArgs bundles the charge inputs.
type ChargeUserArgs struct {
	CustomerID CustomerID `json:"customerId"`
	PeriodID   PeriodID   `json:"periodId"`
	Amount     Money      `json:"amount"`
}

// ChargeUserActivity wraps billingGatewayAccess.chargeUser. GatewayRequestKey =
// "${workflowId}:${activityId}" — Stripe-native dedup. The same function is called
// from both closeBillingPeriodWorkflow (key = close-wf-id:act-id) and
// runBillingRetrySweepWorkflow (key = sweep-wf-id:act-id, the genuinely-new-attempt
// key per R-1). A hard decline surfaces as ContentPolicy (terminal; workflow handles).
func (wf *Workflows) ChargeUserActivity(ctx context.Context, a ChargeUserArgs) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.Gateway.ChargeUser(ctx, a.CustomerID, a.Amount, gatewayRequestKey(ctx)))
}

// =============================================================================
// sourceControlAccess Activity — 1 of 10.
// =============================================================================

// ConfirmAppInstallationActivity wraps sourceControlAccess.confirmAppInstallation
// (GitHub App standing authorization — amendment A-1; D-SC Q2/Q3 co-location ruling).
// Idempotent: discover/confirm is a no-op if already installed. NotFound = not installed.
func (wf *Workflows) ConfirmAppInstallationActivity(ctx context.Context, customerID CustomerID) (struct{}, error) {
	return struct{}{}, fwmgr.MapError(wf.SourceCtrl.ConfirmAppInstallation(ctx, customerID))
}

// =============================================================================
// durableExecutionAccess Activities — 2 of 10.
// =============================================================================

// RegisterClosePeriodScheduleActivity registers (idempotently) the per-customer
// closeBillingPeriod:<customerId> Temporal Schedule via durableExecutionAccess
// (C-BM §2.1 / §6.1). The Schedule fires monthly (or at the configured billing cadence)
// and triggers CloseBillingPeriodWorkflow with the period id derived by the scheduler.
func (wf *Workflows) RegisterClosePeriodScheduleActivity(ctx context.Context, customerID CustomerID) (struct{}, error) {
	spec := scheduleSpec{
		ID:           closePeriodScheduleID(customerID),
		WorkflowType: ExecutionKindClosePeriod,
		TaskQueue:    TaskQueue,
		IntervalSecs: 30 * 24 * 60 * 60, // monthly (30-day approximation; tunable)
	}
	return struct{}{}, fwmgr.MapError(wf.Durable.RegisterSchedule(ctx, spec))
}

// DeliverDelinquencySignalActivity delivers the queued applyDelinquencyPolicy signal
// to the operationsManager's {customerId}:delinquency workflow (the ONE sanctioned
// queued M→M edge; C-BM §5 operationsManager outbound). The signal is fire-and-forget
// (at-least-once to the channel); dedup is the receiving handler's concern.
func (wf *Workflows) DeliverDelinquencySignalActivity(ctx context.Context, customerID CustomerID) (struct{}, error) {
	targetWfID := delinquencyTargetWorkflowID(customerID)
	payload := deliverSignalPayload{
		CustomerID:       customerID,
		PauseNotWithdraw: true, // default: pause (replicas=0); billing terms may override at composition root
	}
	return struct{}{}, fwmgr.MapError(wf.Durable.DeliverSignal(ctx, targetWfID, "applyDelinquencyPolicy", payload))
}
