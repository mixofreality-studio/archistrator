package billing

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// This file holds the Workflows struct (the Manager's downstream dependency set),
// the three workflow bodies (the encapsulated BillingWorkflow volatility — C-BM §6.3),
// the workflow-level Conflict re-read→re-apply loop (§6.5), and the activity-option presets.
//
// How the two dependency kinds are reached differs by determinism class:
//   - The two Engines (billingEngine / interventionEngine) are PURE, deterministic,
//     called DIRECTLY in-workflow by value (no Activity wrapper — replay-safe).
//   - The ResourceAccess ports (billingStateAccess / usageAccess / billingGatewayAccess /
//     sourceControlAccess / durableExecutionAccess) are I/O and non-deterministic;
//     the workflow invokes the Activity methods on this same struct via workflow.ExecuteActivity.

// Deps bundles every downstream dependency the billingManager orchestrates,
// passed to RegisterWorker (worker.go) and held on the Workflows struct.
// Each field is a CONSUMER-DEFINED interface (deps.go).
type Deps struct {
	Billing      BillingEngine
	Intervention InterventionEngine

	BillingState BillingStateAccess
	Usage        UsageAccess
	Gateway      BillingGatewayAccess
	SourceCtrl   SourceControlAccess
	Durable      DurableExecutionAccess
}

// Workflows is the single billingManager component struct — BOTH the workflow
// receiver and the activity receiver (no separate Activities type, mirroring
// operations/settlement/construction).
type Workflows struct {
	Billing      BillingEngine
	Intervention InterventionEngine

	BillingState BillingStateAccess
	Usage        UsageAccess
	Gateway      BillingGatewayAccess
	SourceCtrl   SourceControlAccess
	Durable      DurableExecutionAccess
}

// newWorkflows builds the Workflows receiver from the injected Deps.
func newWorkflows(d Deps) *Workflows {
	return &Workflows{
		Billing:      d.Billing,
		Intervention: d.Intervention,
		BillingState: d.BillingState,
		Usage:        d.Usage,
		Gateway:      d.Gateway,
		SourceCtrl:   d.SourceCtrl,
		Durable:      d.Durable,
	}
}

// Bounds (in-workflow guards; NOT contract surface).
const (
	// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply loop (§6.5).
	maxMutateConflictAttempts = 20
)

// ---------------------------------------------------------------------------
// Activity option presets (C-BM §6.4). FU-BM-4 (named RetryPolicy library) is not
// yet landed; the inline §6.4 parameters are used.
// ---------------------------------------------------------------------------

// readHeadOpts — billing head-state pure reads (terminal NotFound/ContractMisuse).
func readHeadOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// recordHeadOpts — billing head-state write transitions (terminal NotFound/ContractMisuse;
// Conflict is surfaced for the workflow-level re-read loop, so NOT non-retryable here).
func recordHeadOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
				fwmgr.RAErrType(fwra.Conflict),
			},
		},
	})
}

// usageOpts — usageAccess period-fold read (~30s; terminal ContractMisuse/NotFound).
func usageOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.ContractMisuse),
				fwmgr.RAErrType(fwra.NotFound),
			},
		},
	})
}

// gatewayOpts — billingGatewayAccess charge + validate (~30s; small retry budget;
// terminal Auth/ContentPolicy/ContractMisuse — a hard decline is terminal ContentPolicy;
// the workflow catches it and runs DecideOnBillingFailure rather than retrying here).
func gatewayOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.Auth),
				fwmgr.RAErrType(fwra.ContentPolicy),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// sourceControlOpts — sourceControlAccess.confirmAppInstallation (~30s; terminal
// NotFound/Auth/ContractMisuse — NotFound = App not installed, surfaced as
// FailedPrecondition).
func sourceControlOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.Auth),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// durableOpts — durableExecutionAccess deliverSignal / registerSchedule (~30s;
// terminal NotFound/ContractMisuse).
func durableOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// raConflictErrType is the canonical Temporal Type() a head-state mutation Activity
// surfaces when expectedVersion is stale; the workflow recovers with the bounded
// re-read→re-apply loop (§6.5).
var raConflictErrType = fwmgr.RAErrType(fwra.Conflict)

// raNotFoundErrType is the canonical Temporal Type() ReadBillingAggregate surfaces
// for a missing aggregate.
var raNotFoundErrType = fwmgr.RAErrType(fwra.NotFound)

// gatewayDeclineErrType is the canonical Temporal Type() a terminal hard decline
// surfaces (billingGatewayAccess ContentPolicy — the contract's declined-charge kind).
// On this error the close workflow runs the interventionEngine decide→execute branch.
var gatewayDeclineErrType = fwmgr.RAErrType(fwra.ContentPolicy)

// ===========================================================================
// RegisterCustomerWorkflow — op 1 (registerCustomer; C-BM §2.1).
// ===========================================================================

// RegisterCustomerInput is the start payload for RegisterCustomerWorkflow.
type RegisterCustomerInput struct {
	CustomerID CustomerID `json:"customerId"`
}

// RegisterCustomerWorkflow drives registerCustomer (C-BM §6.3):
//  1. ValidateStoredInstrumentActivity (billingGatewayAccess; zero-amount auth).
//  2. ConfirmAppInstallationActivity (sourceControlAccess; GitHub App standing — amendment A-1).
//  3. OpenBillingAggregateActivity (billingStateAccess; opens the aggregate; Conflict loop).
//  4. RegisterClosePeriodScheduleActivity (durableExecutionAccess; per-customer Schedule).
//
// Registration-failure on hard decline (ContentPolicy from gateway) or App-not-installed
// (NotFound from sourceControlAccess) surfaces as FailedPrecondition.
func (wf *Workflows) RegisterCustomerWorkflow(ctx workflow.Context, in RegisterCustomerInput) (BillingRef, error) {
	logger := workflow.GetLogger(ctx)

	// Validate the stored charge instrument (zero-amount auth).
	if err := wf.validateStoredInstrument(ctx, in.CustomerID); err != nil {
		return BillingRef{}, err
	}

	// Confirm the GitHub App is installed on the customer's account (amendment A-1).
	if err := wf.confirmAppInstallation(ctx, in.CustomerID); err != nil {
		// NotFound = App not installed: registration-failure path.
		if isSourceControlNotFound(err) {
			return BillingRef{}, temporal.NewNonRetryableApplicationError(
				"GitHub App is not installed on the customer's account; registration cannot proceed",
				fwmgr.ManagerErrType(fwmgr.FailedPrecondition), nil)
		}
		return BillingRef{}, err
	}

	// Open the billing aggregate (head-state; Conflict loop; fresh open = expectedVersion 0).
	if _, err := wf.openBillingAggregate(ctx, in.CustomerID, 0); err != nil {
		return BillingRef{}, err
	}

	// Register the per-customer closeBillingPeriod:<customerId> Temporal Schedule.
	if err := wf.registerClosePeriodSchedule(ctx, in.CustomerID); err != nil {
		return BillingRef{}, err
	}

	logger.Info("billing customer registered", "customerId", in.CustomerID.String())
	return BillingRef{CustomerID: in.CustomerID}, nil
}

// ===========================================================================
// CloseBillingPeriodWorkflow — op 2 (closeBillingPeriod; C-BM §2.2).
// ===========================================================================

// CloseBillingPeriodInput is the start payload for CloseBillingPeriodWorkflow.
type CloseBillingPeriodInput struct {
	CustomerID CustomerID `json:"customerId"`
	PeriodID   PeriodID   `json:"periodId"`
}

// CloseBillingPeriodWorkflow drives closeBillingPeriod (C-BM §6.3):
//  1. ReadBillingAggregateActivity (billingStateAccess; pricing policy + P3 guard).
//  2. ReadPeriodUsageActivity (usageAccess; period usage fold).
//  3. billingEngine.PriceUsage (direct, by value) → ServiceInvoice.
//  4. ChargeUserActivity if amount>0 (billingGatewayAccess; key=${workflowId}:${actId}).
//     On ContentPolicy decline: interventionEngine.DecideOnBillingFailure (direct).
//  5. RecordServiceInvoiceActivity (billingStateAccess; Conflict loop).
//
// WorkflowIDReusePolicy:RejectDuplicate guards against double-billing at the Temporal
// level; the P3 guard handles the in-workflow idempotency (period already closed).
func (wf *Workflows) CloseBillingPeriodWorkflow(ctx workflow.Context, in CloseBillingPeriodInput) (CloseBillingPeriodResult, error) {
	logger := workflow.GetLogger(ctx)

	// Read the billing aggregate (pricing policy + version + closed-period guard P3).
	aggregate, err := wf.readBillingAggregate(ctx, in.CustomerID)
	if err != nil {
		return CloseBillingPeriodResult{}, err
	}

	// P3: period-already-closed guard — idempotent in-workflow (WorkflowIDReusePolicy
	// guards at the Temporal start level; this guards against a partial-replay that
	// already recorded the invoice in a previous attempt).
	for _, closed := range aggregate.ClosedPeriods {
		if closed.PeriodID == in.PeriodID {
			logger.Info("billing period already closed (idempotent)", "customerId", in.CustomerID.String(), "periodId", in.PeriodID)
			return CloseBillingPeriodResult{
				CustomerID: in.CustomerID,
				PeriodID:   in.PeriodID,
				Charged:    closed.Charged,
			}, nil
		}
	}

	// Read and fold period usage (pure read; no key).
	usage, err := wf.readPeriodUsage(ctx, in.CustomerID, in.PeriodID)
	if err != nil {
		return CloseBillingPeriodResult{}, err
	}

	// Price usage in-workflow via billingEngine.PriceUsage (pure, deterministic, no Activity).
	invoice, perr := wf.Billing.PriceUsage(usage, aggregate.ServicePricing)
	if perr != nil {
		return CloseBillingPeriodResult{}, fwmgr.MapError(perr)
	}

	// Charge if amount > 0; a zero invoice is a normal outcome (Charged=false), not an error.
	charged := false
	if invoice.ServiceInvoiceAmount.MinorUnits > 0 {
		cerr := wf.chargeUser(ctx, in.CustomerID, in.PeriodID, invoice.ServiceInvoiceAmount)
		if cerr == nil {
			charged = true
		} else if isGatewayDecline(cerr) {
			// A hard decline is ContentPolicy from billingGatewayAccess — NOT a BillingError.
			// Route to interventionEngine.DecideOnBillingFailure (direct in-workflow).
			directive, derr := wf.Intervention.DecideOnBillingFailure(BillingFailureSeam{
				CustomerID:   in.CustomerID,
				PeriodID:     in.PeriodID,
				AttemptCount: 1,
			})
			if derr != nil {
				return CloseBillingPeriodResult{}, fwmgr.MapError(derr)
			}
			switch directive {
			case BillingFailureEscalate:
				// Escalate: leave for retry sweep + delinquency signal; Charged remains false.
				logger.Warn("billing charge declined and escalated to retry sweep",
					"customerId", in.CustomerID.String(), "periodId", in.PeriodID)
			case BillingFailureTransient:
				// Transient decline: leave for retry sweep without signal; Charged remains false.
				logger.Info("billing charge declined (transient); deferred to retry sweep",
					"customerId", in.CustomerID.String(), "periodId", in.PeriodID)
			default:
				return CloseBillingPeriodResult{}, temporal.NewNonRetryableApplicationError(
					"intervention returned an unknown billing-failure directive",
					"UnknownBillingFailureDirective", nil)
			}
		} else {
			return CloseBillingPeriodResult{}, cerr
		}
	}

	// Record the invoice outcome (head-state; Conflict loop).
	if _, rerr := wf.recordServiceInvoice(ctx, in.CustomerID, aggregate.Version, in.PeriodID, invoice, charged); rerr != nil {
		return CloseBillingPeriodResult{}, rerr
	}

	logger.Info("billing period closed", "customerId", in.CustomerID.String(), "periodId", in.PeriodID, "charged", charged)
	return CloseBillingPeriodResult{CustomerID: in.CustomerID, PeriodID: in.PeriodID, Charged: charged}, nil
}

// ===========================================================================
// RunBillingRetrySweepWorkflow — op 3 (runBillingRetrySweep; C-BM §2.3).
// ===========================================================================

// RunBillingRetrySweepInput is the start payload for RunBillingRetrySweepWorkflow
// (no persistent fields — the tickId is used only for the workflow id derivation in
// billingmanager.go; the Schedule carries no business payload).
type RunBillingRetrySweepInput struct{}

// RunBillingRetrySweepWorkflow drives runBillingRetrySweep (C-BM §6.3):
//  1. ReadDelinquentCustomersActivity (billingStateAccess; cross-row read).
//  2. Per delinquent customer: ChargeUserActivity (billingGatewayAccess; new key =
//     ${sweepWorkflowId}:${activityId} for genuinely-new-attempt semantics — R-1).
//  3. On persistent decline: DeliverDelinquencySignalActivity (durableExecutionAccess;
//     applyDelinquencyPolicy → operationsManager).
//
// Genuinely-new-attempt semantics (R-1): the sweep uses this workflow's own
// workflowId (the :all:billingRetrySweep:{tickId} id) as the key prefix, so each
// sweep firing produces a fresh Stripe idempotency key — a NEW charge attempt that
// Stripe does not dedup against the original period-close attempt.
func (wf *Workflows) RunBillingRetrySweepWorkflow(ctx workflow.Context, _ RunBillingRetrySweepInput) (BillingRetrySweepResult, error) {
	logger := workflow.GetLogger(ctx)

	// Read persistently-delinquent customers (cross-row; pure read; no key).
	delinquents, err := wf.readDelinquentCustomers(ctx)
	if err != nil {
		return BillingRetrySweepResult{}, err
	}

	var signalled []CustomerID
	for i, d := range delinquents {
		// Re-charge with new execution-scoped key (genuinely new attempt; R-1).
		// activityIdempotencyKey in ChargeUserActivity derives ${sweepWorkflowId}:${actId},
		// which is per-delinquent-customer unique within this sweep.
		cerr := wf.chargeDelinquentUser(ctx, d, i)
		if cerr == nil {
			// Charge succeeded — customer is no longer delinquent.
			continue
		}
		if !isGatewayDecline(cerr) {
			return BillingRetrySweepResult{}, cerr
		}
		// Persistent decline: deliver applyDelinquencyPolicy signal to operationsManager.
		if serr := wf.deliverDelinquencySignal(ctx, d.CustomerID); serr != nil {
			return BillingRetrySweepResult{}, serr
		}
		signalled = append(signalled, d.CustomerID)
	}

	if signalled == nil {
		signalled = []CustomerID{}
	}
	logger.Info("billing retry sweep complete", "delinquents", len(delinquents), "signalled", len(signalled))
	return BillingRetrySweepResult{SignalledCustomers: signalled}, nil
}

// ---------------------------------------------------------------------------
// Head-state read + recovering write helpers (C-BM §6.5).
// ---------------------------------------------------------------------------

// readBillingAggregate runs the ReadBillingAggregateActivity.
func (wf *Workflows) readBillingAggregate(ctx workflow.Context, customerID CustomerID) (BillingAggregate, error) {
	c := readHeadOpts(ctx)
	var agg BillingAggregate
	if err := workflow.ExecuteActivity(c, wf.ReadBillingAggregateActivity, customerID).Get(ctx, &agg); err != nil {
		return BillingAggregate{}, err
	}
	return agg, nil
}

// openBillingAggregate applies the OpenBillingAggregateActivity with the Conflict loop (§6.5).
func (wf *Workflows) openBillingAggregate(ctx workflow.Context, customerID CustomerID, seed Version) (Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected Version) (Version, error) {
		c := recordHeadOpts(ctx)
		var v Version
		e := workflow.ExecuteActivity(c, wf.OpenBillingAggregateActivity, OpenBillingAggregateArgs{
			CustomerID:      customerID,
			ExpectedVersion: expected,
		}).Get(ctx, &v)
		return v, e
	})
}

// recordServiceInvoice applies the RecordServiceInvoiceActivity with the Conflict loop (§6.5).
func (wf *Workflows) recordServiceInvoice(ctx workflow.Context, customerID CustomerID, seed Version, periodID PeriodID, invoice ServiceInvoiceSeam, charged bool) (Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected Version) (Version, error) {
		c := recordHeadOpts(ctx)
		var v Version
		e := workflow.ExecuteActivity(c, wf.RecordServiceInvoiceActivity, RecordServiceInvoiceArgs{
			CustomerID:      customerID,
			ExpectedVersion: expected,
			PeriodID:        periodID,
			Invoice:         invoice,
			Charged:         charged,
		}).Get(ctx, &v)
		return v, e
	})
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to operations/settlement).
// On a stale-version fwra.Conflict it re-reads the true head Version and re-applies
// with the SAME idempotency key (dedup-first ordering preserves idempotent replay).
func (wf *Workflows) applyRecovering(
	ctx workflow.Context,
	customerID CustomerID,
	seed Version,
	apply func(expected Version) (Version, error),
) (Version, error) {
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		agg, rerr := wf.readBillingAggregate(ctx, customerID)
		if rerr != nil {
			return 0, rerr
		}
		expected = agg.Version
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
}

// readPeriodUsage runs the ReadPeriodUsageActivity.
func (wf *Workflows) readPeriodUsage(ctx workflow.Context, customerID CustomerID, periodID PeriodID) (PeriodUsageSeam, error) {
	c := usageOpts(ctx)
	var usage PeriodUsageSeam
	err := workflow.ExecuteActivity(c, wf.ReadPeriodUsageActivity, ReadPeriodUsageArgs{
		CustomerID: customerID,
		PeriodID:   periodID,
	}).Get(ctx, &usage)
	return usage, err
}

// validateStoredInstrument runs the ValidateStoredInstrumentActivity.
func (wf *Workflows) validateStoredInstrument(ctx workflow.Context, customerID CustomerID) error {
	c := gatewayOpts(ctx)
	return workflow.ExecuteActivity(c, wf.ValidateStoredInstrumentActivity, customerID).Get(ctx, nil)
}

// confirmAppInstallation runs the ConfirmAppInstallationActivity.
func (wf *Workflows) confirmAppInstallation(ctx workflow.Context, customerID CustomerID) error {
	c := sourceControlOpts(ctx)
	return workflow.ExecuteActivity(c, wf.ConfirmAppInstallationActivity, customerID).Get(ctx, nil)
}

// chargeUser runs the ChargeUserActivity for the period-close path. The key is
// derived from the closeBillingPeriod workflow's context: ${closePeriodWfId}:${actId}.
func (wf *Workflows) chargeUser(ctx workflow.Context, customerID CustomerID, periodID PeriodID, amount Money) error {
	c := gatewayOpts(ctx)
	return workflow.ExecuteActivity(c, wf.ChargeUserActivity, ChargeUserArgs{
		CustomerID: customerID,
		PeriodID:   periodID,
		Amount:     amount,
	}).Get(ctx, nil)
}

// chargeDelinquentUser runs the ChargeUserActivity for the retry-sweep path.
// The key is derived from the sweep workflow's context: ${sweepWfId}:${actId},
// achieving genuinely-new-attempt semantics (R-1). idx is used to label the activity
// but does NOT affect the key (Temporal's activityId is deterministic from position).
func (wf *Workflows) chargeDelinquentUser(ctx workflow.Context, d DelinquentCustomerSeam, _ int) error {
	c := gatewayOpts(ctx)
	return workflow.ExecuteActivity(c, wf.ChargeUserActivity, ChargeUserArgs{
		CustomerID: d.CustomerID,
		PeriodID:   d.PeriodID,
		Amount:     d.Amount,
	}).Get(ctx, nil)
}

// readDelinquentCustomers runs the ReadDelinquentCustomersActivity.
func (wf *Workflows) readDelinquentCustomers(ctx workflow.Context) ([]DelinquentCustomerSeam, error) {
	c := readHeadOpts(ctx)
	var delinquents []DelinquentCustomerSeam
	err := workflow.ExecuteActivity(c, wf.ReadDelinquentCustomersActivity).Get(ctx, &delinquents)
	return delinquents, err
}

// registerClosePeriodSchedule runs the RegisterClosePeriodScheduleActivity.
func (wf *Workflows) registerClosePeriodSchedule(ctx workflow.Context, customerID CustomerID) error {
	c := durableOpts(ctx)
	return workflow.ExecuteActivity(c, wf.RegisterClosePeriodScheduleActivity, customerID).Get(ctx, nil)
}

// deliverDelinquencySignal runs the DeliverDelinquencySignalActivity (queued M→M;
// applyDelinquencyPolicy → operationsManager's {customerId}:delinquency workflow).
func (wf *Workflows) deliverDelinquencySignal(ctx workflow.Context, customerID CustomerID) error {
	c := durableOpts(ctx)
	return workflow.ExecuteActivity(c, wf.DeliverDelinquencySignalActivity, customerID).Get(ctx, nil)
}

// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

// isReadNotFound reports whether err is a head-state read's NotFound.
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// isGatewayDecline reports whether err is a terminal hard decline from the gateway
// (ContentPolicy — the contract's declined-charge kind).
func isGatewayDecline(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == gatewayDeclineErrType
	}
	return false
}

// isSourceControlNotFound reports whether err is the GitHub App not-installed error
// (sourceControlAccess NotFound = App not installed).
func isSourceControlNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// delinquencyTargetWorkflowID derives the operationsManager's delinquency workflow id
// ({customerId}:delinquency) that the applyDelinquencyPolicy signal targets (C-BM §5
// operationsManager outbound: "deliverSignal (queued M→M via durableExecutionAccess)").
func delinquencyTargetWorkflowID(customerID CustomerID) string {
	return fmt.Sprintf("%s:delinquency", customerID)
}
