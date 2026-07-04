package billing

import (
	"context"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
)

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (billingManager.md §6.1/§6.2).
// ---------------------------------------------------------------------------

// TaskQueue is the one queue per Manager that the in-process Temporal Worker in the
// server polls (billingManager.md §6.1; operational-concepts.md §6 — "one Worker per
// task queue: ... billing").
const TaskQueue = "billing"

// ExecutionKinds — the registered workflow names (billingManager.md §6.2).
const (
	// ExecutionKindOnboard is the UC5 payment-integration onboarding workflow.
	executionKindOnboard = "billingOnboardPayment"
	// ExecutionKindRegister is the ncuc1 customer-registration workflow.
	executionKindRegister = "billingRegisterCustomer"
	// ExecutionKindClose is the UC6 cycle-close workflow (also hosts the inbound/
	// chargeback Signals and the forward-only recompute saga).
	executionKindClose = "billingCloseCycle"
	// ExecutionKindShortfallSweep is the ncuc5 shortfall-sweep workflow.
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

// Activity name constants. The Activity methods are registered under these stable names
// and the workflow bodies invoke them by the method value on wf, so the registered name
// and the call stay in lockstep (billingManager.md §6.4 — one per RA call).
const (
	// billingStateAccess (head-state) Activities.
	actReadBilling      = "ReadBillingActivity"
	actReadDelinquent   = "ReadDelinquentActivity"
	actRegisterCustomer = "RegisterCustomerActivity"
	actBindGatewayLive  = "BindGatewayLiveActivity"
	actSettleCycle      = "SettleCycleActivity"
	actResettleCycle    = "ResettleCycleActivity"

	// revenueLedgerAccess + usageAccess (append-only ledger) Activities.
	actRecordInboundRevenue = "RecordInboundRevenueActivity"
	actRecordReversal       = "RecordReversalActivity"
	actReadRevenueRange     = "ReadRevenueRangeActivity"
	actReadUsageRange       = "ReadUsageRangeActivity"

	// merchantGatewayAccess (external gateway) Activities.
	actPayoutCustomer         = "PayoutCustomerActivity"
	actChargeCustomer         = "ChargeCustomerActivity"
	actCreateConnectedAccount = "CreateConnectedAccountActivity"
	actValidateStoredInstr    = "ValidateStoredInstrumentActivity"

	// operatedRuntimeAccess Activity.
	actWirePaymentConfig = "WirePaymentConfigActivity"

	// durableExecutionAccess (category-B) Activities.
	actDeliverDelinquency = "DeliverDelinquencySignalActivity"
	actRegisterSchedule   = "RegisterScheduleActivity"
)

// RegisterWorker wires the billingManager onto a Temporal Worker polling the
// billing task queue (billingManager.md §6.1). The Manager's workflow dependencies
// — the two Engines (billingEngine, interventionEngine, called DIRECTLY in-workflow)
// and the ResourceAccess ports (billingStateAccess, revenueLedgerAccess, usageAccess,
// merchantGatewayAccess, operatedRuntimeAccess, durableExecutionAccess) — live on a
// single Workflows struct (there is no separate Activities type).
//
// The two Engines' verbs are called DIRECTLY from workflow code (deterministic, by
// value) and are NOT Activities. The durableExecutionAccess in-workflow primitive
// (awaitSignal) is the Manager's own code (category A) and is NOT an Activity either.
func RegisterWorker(w worker.Worker, m BillingManager) {
	impl, ok := m.(*billingManager)
	if !ok {
		panic("billing: RegisterWorker requires a *billingManager from NewBillingManager")
	}

	// Fold the published deps the constructor stored into the unexported seams the
	// Workflows struct holds (adapters.go) — the Option-B boundary mapping that replaces
	// the former composition-root wfDeps wiring.
	deps := wfDeps{
		Billing:         billingEngineAdapter{inner: impl.billing},
		Intervention:    interventionAdapter{inner: impl.intervention},
		BillingState:    billingStateAdapter{inner: impl.billingState},
		RevenueLedger:   noopRevenueLedger{},
		Usage:           usageAdapter{inner: impl.usage},
		Gateway:         merchantGatewayAdapter{inner: impl.merchantGateway},
		OperatedRuntime: operatedRuntimeAdapter{inner: impl.operatedRuntime},
		Durable:         durableAdapter{inner: impl.durableExecution},
	}
	wf := newWorkflows(deps)

	w.RegisterWorkflowWithOptions(wf.OnboardWorkflow, workflow.RegisterOptions{Name: executionKindOnboard})
	w.RegisterWorkflowWithOptions(wf.RegisterCustomerWorkflow, workflow.RegisterOptions{Name: executionKindRegister})
	w.RegisterWorkflowWithOptions(wf.CloseCycleWorkflow, workflow.RegisterOptions{Name: executionKindClose})
	w.RegisterWorkflowWithOptions(wf.ShortfallSweepWorkflow, workflow.RegisterOptions{Name: executionKindShortfallSweep})

	// The Activities (billingManager.md §6.4), one per RA call.
	w.RegisterActivityWithOptions(wf.ReadBillingActivity, activity.RegisterOptions{Name: actReadBilling})
	w.RegisterActivityWithOptions(wf.ReadDelinquentActivity, activity.RegisterOptions{Name: actReadDelinquent})
	w.RegisterActivityWithOptions(wf.RegisterCustomerActivity, activity.RegisterOptions{Name: actRegisterCustomer})
	w.RegisterActivityWithOptions(wf.BindGatewayLiveActivity, activity.RegisterOptions{Name: actBindGatewayLive})
	w.RegisterActivityWithOptions(wf.SettleCycleActivity, activity.RegisterOptions{Name: actSettleCycle})
	w.RegisterActivityWithOptions(wf.ResettleCycleActivity, activity.RegisterOptions{Name: actResettleCycle})

	w.RegisterActivityWithOptions(wf.RecordInboundRevenueActivity, activity.RegisterOptions{Name: actRecordInboundRevenue})
	w.RegisterActivityWithOptions(wf.RecordReversalActivity, activity.RegisterOptions{Name: actRecordReversal})
	w.RegisterActivityWithOptions(wf.ReadRevenueRangeActivity, activity.RegisterOptions{Name: actReadRevenueRange})
	w.RegisterActivityWithOptions(wf.ReadUsageRangeActivity, activity.RegisterOptions{Name: actReadUsageRange})

	w.RegisterActivityWithOptions(wf.PayoutCustomerActivity, activity.RegisterOptions{Name: actPayoutCustomer})
	w.RegisterActivityWithOptions(wf.ChargeCustomerActivity, activity.RegisterOptions{Name: actChargeCustomer})
	w.RegisterActivityWithOptions(wf.CreateConnectedAccountActivity, activity.RegisterOptions{Name: actCreateConnectedAccount})
	w.RegisterActivityWithOptions(wf.ValidateStoredInstrumentActivity, activity.RegisterOptions{Name: actValidateStoredInstr})

	w.RegisterActivityWithOptions(wf.WirePaymentConfigActivity, activity.RegisterOptions{Name: actWirePaymentConfig})

	w.RegisterActivityWithOptions(wf.DeliverDelinquencySignalActivity, activity.RegisterOptions{Name: actDeliverDelinquency})
	w.RegisterActivityWithOptions(wf.RegisterScheduleActivity, activity.RegisterOptions{Name: actRegisterSchedule})
}

// RegisterSchedules registers (idempotently) the platform-wide shortfallSweep (hourly)
// Temporal Schedule at startup via durableExecutionAccess (billingManager.md §6.1;
// FU-MST-3). Called once at process start. The per-customer closeBillingCycle:<customerId>
// Schedule is NOT registered here — it is registered per-customer at onboarding (op 2.1,
// RegisterScheduleActivity).
func RegisterSchedules(ctx context.Context, durable durableexecution.DurableExecutionAccess) error {
	return durableAdapter{inner: durable}.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDShortfallSweep,
		WorkflowType: executionKindShortfallSweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: shortfallSweepIntervalSecs,
	})
}
