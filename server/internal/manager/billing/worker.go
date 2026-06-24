package billing

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (C-BM §6.1/§6.2).
// ---------------------------------------------------------------------------

// TaskQueue is the one queue per Manager that the in-process Temporal Worker polls.
// Contract: C-BM Stereotype "task queue: billing".
const TaskQueue = "billing"

// ExecutionKinds — the registered workflow names (C-BM §6.2).
const (
	// ExecutionKindRegister is the registerCustomer workflow (§2.1).
	ExecutionKindRegister = "billingRegisterCustomer"
	// ExecutionKindClosePeriod is the closeBillingPeriod workflow (§2.2).
	ExecutionKindClosePeriod = "billingClosePeriod"
	// ExecutionKindRetrySweep is the runBillingRetrySweep workflow (§2.3).
	ExecutionKindRetrySweep = "billingRetrySweep"
)

// Schedule ids + cadence (C-BM §6.1; operational-concepts.md §4).
const (
	// scheduleIDRetrySweep is the hourly billingRetrySweep Schedule id.
	scheduleIDRetrySweep = "billing:billingRetrySweep"

	// retrySweepInterval is the retry-sweep cadence (hourly per C-BM §2.3).
	retrySweepInterval = 1 * time.Hour
)

// closePeriodScheduleID returns the per-customer closeBillingPeriod:<customerId>
// Schedule id (C-BM §2.2 / §6.1).
func closePeriodScheduleID(customerID CustomerID) string {
	return "billing:closeBillingPeriod:" + customerID.String()
}

// Activity name constants. Registered under these stable names; the workflow
// bodies call by method value — name and call stay in lockstep (C-BM §6.4).
const (
	// billingStateAccess Activities.
	actReadBillingAggregate    = "ReadBillingAggregateActivity"
	actReadDelinquentCustomers = "ReadDelinquentCustomersActivity"
	actOpenBillingAggregate    = "OpenBillingAggregateActivity"
	actRecordServiceInvoice    = "RecordServiceInvoiceActivity"

	// usageAccess Activity.
	actReadPeriodUsage = "ReadPeriodUsageActivity"

	// billingGatewayAccess Activities.
	actValidateStoredInstrument = "ValidateStoredInstrumentActivity"
	actChargeUser               = "ChargeUserActivity"

	// sourceControlAccess Activity.
	actConfirmAppInstallation = "ConfirmAppInstallationActivity"

	// durableExecutionAccess Activities.
	actRegisterClosePeriodSchedule = "RegisterClosePeriodScheduleActivity"
	actDeliverDelinquencySignal    = "DeliverDelinquencySignalActivity"
)

// RegisterWorker wires the billingManager onto a Temporal Worker polling the
// billing task queue (C-BM §6.1). The two Engines (billingEngine,
// interventionEngine — called DIRECTLY in-workflow, not via Activity) and the
// five ResourceAccess ports live on the Workflows struct (workflow.go).
func RegisterWorker(w worker.Worker, deps Deps) {
	wf := newWorkflows(deps)

	w.RegisterWorkflowWithOptions(wf.RegisterCustomerWorkflow, workflow.RegisterOptions{Name: ExecutionKindRegister})
	w.RegisterWorkflowWithOptions(wf.CloseBillingPeriodWorkflow, workflow.RegisterOptions{Name: ExecutionKindClosePeriod})
	w.RegisterWorkflowWithOptions(wf.RunBillingRetrySweepWorkflow, workflow.RegisterOptions{Name: ExecutionKindRetrySweep})

	// billingStateAccess Activities (4).
	w.RegisterActivityWithOptions(wf.ReadBillingAggregateActivity, activity.RegisterOptions{Name: actReadBillingAggregate})
	w.RegisterActivityWithOptions(wf.ReadDelinquentCustomersActivity, activity.RegisterOptions{Name: actReadDelinquentCustomers})
	w.RegisterActivityWithOptions(wf.OpenBillingAggregateActivity, activity.RegisterOptions{Name: actOpenBillingAggregate})
	w.RegisterActivityWithOptions(wf.RecordServiceInvoiceActivity, activity.RegisterOptions{Name: actRecordServiceInvoice})

	// usageAccess Activity (1).
	w.RegisterActivityWithOptions(wf.ReadPeriodUsageActivity, activity.RegisterOptions{Name: actReadPeriodUsage})

	// billingGatewayAccess Activities (2).
	w.RegisterActivityWithOptions(wf.ValidateStoredInstrumentActivity, activity.RegisterOptions{Name: actValidateStoredInstrument})
	w.RegisterActivityWithOptions(wf.ChargeUserActivity, activity.RegisterOptions{Name: actChargeUser})

	// sourceControlAccess Activity (1).
	w.RegisterActivityWithOptions(wf.ConfirmAppInstallationActivity, activity.RegisterOptions{Name: actConfirmAppInstallation})

	// durableExecutionAccess Activities (2).
	w.RegisterActivityWithOptions(wf.RegisterClosePeriodScheduleActivity, activity.RegisterOptions{Name: actRegisterClosePeriodSchedule})
	w.RegisterActivityWithOptions(wf.DeliverDelinquencySignalActivity, activity.RegisterOptions{Name: actDeliverDelinquencySignal})
}

// RegisterSchedules registers (idempotently) the hourly billingRetrySweep Temporal
// Schedule at startup via durableExecutionAccess (C-BM §6.1). Per-customer
// closeBillingPeriod Schedules are registered inside RegisterCustomerWorkflow.
func RegisterSchedules(ctx context.Context, durable DurableExecutionAccess) error {
	return durable.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDRetrySweep,
		WorkflowType: ExecutionKindRetrySweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: int(retrySweepInterval / time.Second),
	})
}
