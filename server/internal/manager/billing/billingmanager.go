package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// Manager is the billingManager façade. It exposes the three public ops (C-BM §2)
// and OWNS Temporal. The Temporal-backed ops:
//   - RegisterCustomer      — Workflow (entry; id {customerId}:register)
//   - CloseBillingPeriod    — Workflow (entry; scheduler-triggered; id {customerId}:{periodId}:close)
//   - RunBillingRetrySweep  — Workflow (entry; scheduler-triggered; id :all:billingRetrySweep:{tickId})
//
// The Manager holds only a Temporal client; the façade pre-condition checks
// (non-empty ids) are enforced here before any Temporal call (C-BM §2/§3.4). The
// downstream Engines/RA are held on the Workflows struct (workflow.go), reached
// only from workflow code.
type Manager struct {
	client client.Client
}

// NewManager constructs a Manager over an existing Temporal client.
func NewManager(c client.Client) *Manager {
	return &Manager{client: c}
}

// RegisterCustomer — op 1 (C-BM §2.1). Temporal Workflow (entry; StartWorkflow, id
// {customerId}:register; WorkflowIDConflictPolicy: USE_EXISTING — idempotent on workflow id).
// Validates the stored charge instrument (zero-amount auth), confirms GitHub App standing
// authorization, opens the billing aggregate, and registers the per-customer
// closeBillingPeriod:<customerId> Schedule. SYNC: returns once the registration is
// durably complete.
func (m *Manager) RegisterCustomer(ctx context.Context, customerID CustomerID) (BillingRef, error) {
	if customerID == uuid.Nil {
		return BillingRef{}, newError(fwmgr.ContractMisuse, "empty customerId")
	}

	wfID := registerWorkflowID(customerID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindRegister, RegisterCustomerInput{
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

// CloseBillingPeriod — op 2 (C-BM §2.2). Temporal Workflow (entry; scheduler-triggered
// via per-customer closeBillingPeriod:<customerId> Schedule; id {customerId}:{periodId}:close;
// WorkflowIDReusePolicy: RejectDuplicate + period-already-closed guard P3). Meters
// usage, prices via billingEngine.PriceUsage, charges if amount>0, consults
// interventionEngine.DecideOnBillingFailure on decline, records outcome. SYNC.
func (m *Manager) CloseBillingPeriod(ctx context.Context, customerID CustomerID, periodID PeriodID) (CloseBillingPeriodResult, error) {
	if customerID == uuid.Nil {
		return CloseBillingPeriodResult{}, newError(fwmgr.ContractMisuse, "empty customerId")
	}
	if periodID == "" {
		return CloseBillingPeriodResult{}, newError(fwmgr.ContractMisuse, "empty periodId")
	}

	wfID := closePeriodWorkflowID(customerID, periodID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindClosePeriod, CloseBillingPeriodInput{
		CustomerID: customerID,
		PeriodID:   periodID,
	})
	if err != nil {
		return CloseBillingPeriodResult{}, mapStartError(err)
	}
	var result CloseBillingPeriodResult
	if err := we.Get(ctx, &result); err != nil {
		return CloseBillingPeriodResult{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// RunBillingRetrySweep — op 3 (C-BM §2.3). Temporal Workflow (entry; scheduler-triggered
// via hourly billingRetrySweep Schedule; id :all:billingRetrySweep:{tickId}). Reads
// persistently-delinquent customers, re-charges each declined invoice with a new
// execution-scoped key (genuinely new attempt), and delivers a queued
// applyDelinquencyPolicy signal to operationsManager on persistent decline. SYNC.
func (m *Manager) RunBillingRetrySweep(ctx context.Context, tickID string) (BillingRetrySweepResult, error) {
	if tickID == "" {
		return BillingRetrySweepResult{}, newError(fwmgr.ContractMisuse, "empty tickId")
	}

	wfID := billingRetrySweepWorkflowID(tickID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindRetrySweep, RunBillingRetrySweepInput{})
	if err != nil {
		return BillingRetrySweepResult{}, mapStartError(err)
	}
	var result BillingRetrySweepResult
	if err := we.Get(ctx, &result); err != nil {
		return BillingRetrySweepResult{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// --- workflow id derivation (continuity tokens; C-BM §6.1) ---

// registerWorkflowID derives {customerId}:register.
func registerWorkflowID(customerID CustomerID) string {
	return fmt.Sprintf("%s:register", customerID)
}

// closePeriodWorkflowID derives {customerId}:{periodId}:close.
func closePeriodWorkflowID(customerID CustomerID, periodID PeriodID) string {
	return fmt.Sprintf("%s:%s:close", customerID, periodID)
}

// billingRetrySweepWorkflowID derives :all:billingRetrySweep:{tickId} (schedule
// firing id = workflow id; Temporal-native firing idempotency; C-BM §6.1 / R-1).
func billingRetrySweepWorkflowID(tickID string) string {
	return fmt.Sprintf(":all:billingRetrySweep:%s", tickID)
}

// --- error mapping at the façade boundary (C-BM §3.4) ---

func mapStartError(err error) error {
	return newError(fwmgr.Infrastructure, err.Error())
}

// isNotFound reports whether the Temporal error indicates the addressed execution
// does not exist (mirrors operations/construction/settlement matchers).
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errNotFoundSentinel) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "NotFound")
}

var errNotFoundSentinel = errors.New("not found")
