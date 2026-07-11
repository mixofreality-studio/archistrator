package billing

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the billingManager impl.
// It supplies the genWorkerManifest (the workflow set codegen cannot know, the custom
// revenue-ledger Activities codegen has no contract for, the per-activity option
// presets, and the genActivities dep threading), the external RegisterManagerWorker
// entrypoint the composition root calls, and the startup Schedule registration.

import (
	"context"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
)

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

// signalApplyDelinquencyPolicy is the cross-Manager signal name delivered to
// operationsManager (matches operations.SignalApplyDelinquencyPolicy). Declared here as
// a string literal to avoid a Manager→Manager package import (the edge is queued via
// durableExecutionAccess, not a direct call).
const signalApplyDelinquencyPolicy = "applyDelinquencyPolicy"

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
	return workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	}
}

// recordHeadActivityOptions — billing head-state write transitions (10s; terminal
// NotFound/ContractMisuse; Conflict is surfaced for the workflow-level re-read loop, so
// it is NOT non-retryable here — the workflow body recovers it).
func recordHeadActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
				fwmgr.RAErrType(fwra.Conflict),
			},
		},
	}
}

// ledgerActivityOptions — revenueLedgerAccess (custom) / usageAccess appends + reads
// (30s; terminal ContractMisuse). Append-only ledgers: NO Conflict (gateway/runtime-
// event-id idempotent). Also consumed directly by the workflow for the custom revenue
// Activities (they are not on the generated invoker surface).
func ledgerActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	}
}

// gatewayActivityOptions — merchantGatewayAccess money movements (externalGateway; small
// budget; terminal Auth/NotFound/ContractMisuse → decideOnBillingFailure). Stripe-native
// dedup on the Manager-supplied Idempotency-Key.
func gatewayActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.Auth),
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	}
}

// durableActivityOptions — durableExecutionAccess deliverSignal / registerSchedule (30s;
// terminal NotFound/ContractMisuse).
func durableActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	}
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
// consumes: the four workflow bodies under their registered names, the three custom
// revenue-ledger Activities (no contract behind them — activities_custom.go), the
// per-activity option-preset hook, and the genActivities threaded from the impl's
// stored published deps.
//
// Unlike operations, billing's durableExecutionAccess IS wired into genActivities: the
// close-schedule registration (op 2.1) and the queued applyDelinquencyPolicy cross-
// Manager signal (ncuc5) are workflow-invoked generated activities. (The startup
// shortfallSweep Schedule is still registered directly via RegisterSchedules, not
// through a workflow.)
func (m *billingManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()
	custom := &customActivities{revenueLedger: noopRevenueLedger{}}
	wf := newWorkflows(wfDeps{
		Billing:      m.billing,
		Intervention: m.intervention,
		Acts:         genInvokers{Opts: optsHook},
		Custom:       custom,
	})

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindOnboard, Fn: wf.OnboardWorkflow},
			{Name: executionKindRegister, Fn: wf.RegisterCustomerWorkflow},
			{Name: executionKindClose, Fn: wf.CloseCycleWorkflow},
			{Name: executionKindShortfallSweep, Fn: wf.ShortfallSweepWorkflow},
		},
		// The custom Activities are registered under their stable names by the same
		// method value the workflow invokes (wf.Custom.XActivity) — Temporal maps the
		// function reference to the explicit Name, so invoke-by-reference resolves here.
		CustomActivities: []genRegisteredActivity{
			{Name: actRecordInboundRevenue, Fn: wf.Custom.RecordInboundRevenueActivity},
			{Name: actRecordReversal, Fn: wf.Custom.RecordReversalActivity},
			{Name: actReadRevenueRange, Fn: wf.Custom.ReadRevenueRangeActivity},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			BillingState:     m.billingState,
			Usage:            m.usage,
			MerchantGateway:  m.merchantGateway,
			DurableExecution: m.durableExecution,
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
