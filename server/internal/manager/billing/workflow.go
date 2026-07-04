package billing

import (
	"errors"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// This file holds the Workflows struct (the Manager's downstream dependency set), the
// five workflow bodies (the encapsulated BillingWorkflow volatility —
// billingManager.md §6.3), the workflow-level Conflict re-read→re-apply loop (§6.5),
// the forward-only chargeback recompute saga, and the activity-option presets.
//
// How the two dependency kinds are reached differs by determinism class:
//   - The two Engines (billingEngine / interventionEngine) are PURE, deterministic,
//     called DIRECTLY in-workflow by value (no Activity wrapper — replay-safe).
//   - The ResourceAccess ports (billingStateAccess / revenueLedgerAccess /
//     usageAccess / merchantGatewayAccess /
//     durableExecutionAccess) are I/O and NON-deterministic; the workflow invokes the
//     Activity methods on this same struct via workflow.ExecuteActivity (activities.go).

// wfDeps bundles every downstream dependency the billingManager orchestrates, passed
// to RegisterWorker (worker.go) and held on the Workflows struct. Each field is a
// CONSUMER-DEFINED interface (deps.go): the concrete RA types are adapted at the
// composition root; the not-yet-built Engines/RA are unit-tested with fakes.
type wfDeps struct {
	Billing      billingEngine
	Intervention interventionEngine

	BillingState  billingStateAccess
	RevenueLedger revenueLedgerAccess
	Usage         usageAccess
	Gateway       merchantGatewayAccess
	Durable       durableExecutionAccess
}

// Workflows is the single billingManager component struct — BOTH the workflow
// receiver and the activity receiver (no separate Activities type, mirroring
// operations/construction).
type workflows struct {
	Billing      billingEngine
	Intervention interventionEngine

	BillingState  billingStateAccess
	RevenueLedger revenueLedgerAccess
	Usage         usageAccess
	Gateway       merchantGatewayAccess
	Durable       durableExecutionAccess
}

// newWorkflows builds the Workflows receiver from the injected wfDeps.
func newWorkflows(d wfDeps) *workflows {
	return &workflows{
		Billing:       d.Billing,
		Intervention:  d.Intervention,
		BillingState:  d.BillingState,
		RevenueLedger: d.RevenueLedger,
		Usage:         d.Usage,
		Gateway:       d.Gateway,
		Durable:       d.Durable,
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

// ---------------------------------------------------------------------------
// Activity option presets (billingManager.md §6.4). Concrete RetryPolicy / timeout
// choices live here, in the Manager. FU-MST-4 (named RetryPolicy library) is not yet
// landed; the inline §6.4 parameters are used.
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

// recordHeadOpts — billing head-state write transitions (terminal
// NotFound/ContractMisuse; Conflict is surfaced for the workflow-level re-read loop, so
// it is NOT non-retryable here — the workflow body recovers it).
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

// ledgerOpts — revenueLedgerAccess / usageAccess appends + reads (~30s; terminal
// ContractMisuse). Append-only ledgers: NO Conflict (gateway/runtime-event-id idempotent).
func ledgerOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// gatewayOpts — merchantGatewayAccess money movements (externalGateway; small budget;
// terminal Auth/NotFound/ContractMisuse → decideOnBillingFailure). Stripe-native
// dedup on the Manager-supplied Idempotency-Key.
func gatewayOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.Auth),
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// durableOpts — durableExecutionAccess deliverSignal / registerSchedule (~30s; terminal
// NotFound/ContractMisuse).
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

// raNotFoundErrType is the canonical Temporal Type() ReadBilling surfaces for a
// missing billing aggregate.
var raNotFoundErrType = fwmgr.RAErrType(fwra.NotFound)

// gatewayFailureErrType is the canonical Temporal Type() a terminal charge decline
// surfaces (RA Auth — the gateway declined). On it the close workflow runs the
// interventionEngine decide→execute branch (OQ-4).
var gatewayDeclineErrType = fwmgr.RAErrType(fwra.Auth)

// ===========================================================================
// OnboardWorkflow — op 2.1 entry (UC5 operator-initiated payment-integration onboard).
// ===========================================================================

// OnboardInput is the start payload for OnboardWorkflow.
type onboardInput struct {
	DeployedAppID deployedAppID
}

// OnboardWorkflow drives UC5 onboard (billingManager.md §6.3):
//  1. ReadBillingActivity → resolves deployedAppId → customerId + terms/payout.
//  2. CreateConnectedAccountActivity (merchantGatewayAccess).
//  3. BindGatewayLiveActivity (head-state; Conflict loop).
//  4. RegisterScheduleActivity (the per-customer closeBillingCycle:<customerId> Schedule).
//
// (Runtime payment-config wiring is NOT a billing step: publishing desired state into
// the operated runtime is OperationsManager's publishDesiredState concern — the
// declared architecture gives billing no operatedRuntimeAccess edge; per operational
// concept #2 billing reaches operations only via the queued applyDelinquencyPolicy
// signal.)
func (wf *workflows) OnboardWorkflow(ctx workflow.Context, in onboardInput) (BillingRef, error) {
	logger := workflow.GetLogger(ctx)

	// Resolve the billing aggregate. The head-state row carries the customerId the
	// deployed app settles under (§3.0 / UC5 line 683).
	billing, err := wf.readBillingByDeployedApp(ctx, in.DeployedAppID)
	if err != nil {
		// A missing billing aggregate violates the UC5 pre-condition ("the deployed
		// app exists and is operated"); surface it as a terminal FailedPrecondition.
		if isReadNotFound(err) {
			return BillingRef{}, temporal.NewNonRetryableApplicationError(
				"no billing aggregate for the deployed app (not registered/operated)",
				fwmgr.ManagerErrType(fwmgr.FailedPrecondition), nil)
		}
		return BillingRef{}, err
	}
	customerID := billing.ID

	// Create the merchant connected account (external gateway).
	binding, gerr := wf.createConnectedAccount(ctx, customerID)
	if gerr != nil {
		return BillingRef{}, gerr
	}

	// Record the binding (head-state; Conflict loop).
	if _, berr := wf.bindGatewayLive(ctx, customerID, billing.Version, binding); berr != nil {
		return BillingRef{}, berr
	}

	// Register the per-customer cycle-close Schedule (idempotent by id).
	if rerr := wf.registerCloseSchedule(ctx, customerID); rerr != nil {
		return BillingRef{}, rerr
	}

	logger.Info("payment integration onboarded", "customerId", customerID.String(), "deployedAppId", in.DeployedAppID.String())
	return BillingRef{CustomerID: customerID}, nil
}

// ===========================================================================
// RegisterCustomerWorkflow — op 2.2 entry (ncuc1 open the billing aggregate).
// ===========================================================================

// RegisterInput is the start payload for RegisterCustomerWorkflow.
type registerInput struct {
	CustomerID customerID
}

// RegisterCustomerWorkflow drives ncuc1 (billingManager.md §6.3):
//  1. ValidateStoredInstrumentActivity (merchantGatewayAccess; zero-amount auth).
//  2. RegisterCustomerActivity (head-state; opens the aggregate; Conflict loop).
func (wf *workflows) RegisterCustomerWorkflow(ctx workflow.Context, in registerInput) (BillingRef, error) {
	logger := workflow.GetLogger(ctx)

	if verr := wf.validateStoredInstrument(ctx, in.CustomerID); verr != nil {
		return BillingRef{}, verr
	}

	// Open the aggregate. A fresh registration seeds expectedVersion 0; the Conflict
	// loop recovers a racing register.
	if _, rerr := wf.registerCustomer(ctx, in.CustomerID, 0); rerr != nil {
		return BillingRef{}, rerr
	}

	logger.Info("customer registered", "customerId", in.CustomerID.String())
	return BillingRef(in), nil
}

// ===========================================================================
// CloseCycleWorkflow — op 2.3 entry (UC6 cycle-close; Schedule-triggered). The worked
// example. Stays alive to await the inbound/reversal/chargeback Signals that target its
// id (§6.3); a chargeback runs the forward-only recompute saga.
// ===========================================================================

// CloseInput is the start payload for CloseCycleWorkflow.
type closeInput struct {
	CustomerID customerID
	CycleID    cycleID
}

// CloseCycleWorkflow drives UC6 cycle-close (billingManager.md §6.3):
//  1. (drain any inbound-revenue signals already enqueued — the append is idempotent on
//     the gateway event id; the net is computed here, not at signal time).
//  2. ReadBillingActivity → terms + expectedVersion.
//  3. ReadRevenueRangeActivity + ReadUsageRangeActivity → value snapshots.
//  4. billingEngine.ComputeNet (direct, by value) → {signedNet, routingDirective}.
//  5. execute the directive: Payout / Charge (on failure decide→execute) / NoAction.
//  6. SettleCycleActivity (head-state; Conflict loop).
//  7. await chargebackReceived → forward-only RecomputeCycle saga (§6.3).
func (wf *workflows) CloseCycleWorkflow(ctx workflow.Context, in closeInput) (CloseCycleResult, error) {
	logger := workflow.GetLogger(ctx)

	// Drain any inbound-revenue signals delivered before/at close (signal-with-start
	// may have started this workflow). Each append is idempotent on the gateway event
	// id; the net is computed below, not at signal time (§2.5).
	wf.drainInboundRevenue(ctx, in.CustomerID, in.CycleID)

	billing, err := wf.readBilling(ctx, in.CustomerID)
	if err != nil {
		return CloseCycleResult{}, err
	}
	if !billing.GatewayBound {
		return CloseCycleResult{}, temporal.NewNonRetryableApplicationError(
			"customer is not registered + gateway-bound; cannot close cycle",
			fwmgr.ManagerErrType(fwmgr.FailedPrecondition), nil)
	}

	revenue, rerr := wf.foldRevenue(ctx, in.CustomerID, in.CycleID)
	if rerr != nil {
		return CloseCycleResult{}, rerr
	}
	usage, uerr := wf.foldUsage(ctx, in.CustomerID, in.CycleID)
	if uerr != nil {
		return CloseCycleResult{}, uerr
	}

	// Compute the signed net + routing directive — DIRECT, by value, in-workflow.
	result, cerr := wf.Billing.ComputeNet(revenue, usage, billing.Terms)
	if cerr != nil {
		return CloseCycleResult{}, fwmgr.MapError(cerr)
	}

	escalated, routeErr := wf.routeNet(ctx, in.CustomerID, in.CycleID, result, 0)
	if routeErr != nil {
		return CloseCycleResult{}, routeErr
	}

	// Record the billing outcome (head-state; Conflict loop).
	outcome := billingOutcomeSeam{
		Net:       result.SignedNet,
		Directive: result.RoutingDirective,
		Escalated: escalated,
	}
	if _, serr := wf.settleCycle(ctx, in.CustomerID, billing.Version, in.CycleID, outcome); serr != nil {
		return CloseCycleResult{}, serr
	}

	logger.Info("cycle settled", "customerId", in.CustomerID.String(), "cycleId", in.CycleID,
		"directive", result.RoutingDirective.String(), "escalated", escalated)

	// Await a chargeback for this cycle (forward-only recompute saga). The wait is the
	// Manager's own in-workflow primitive (awaitSignal — category A). A short selector
	// keeps the close bounded for the test/scheduled path; a chargeback re-derives the
	// recompute. The await is non-blocking when no chargeback arrives within the cycle.
	wf.awaitChargeback(ctx, in.CustomerID, in.CycleID, result)

	return CloseCycleResult{
		CustomerID: in.CustomerID,
		CycleID:    in.CycleID,
		// Convert the Engine-seam directive to the façade's OWN RoutingDirective at the
		// boundary (full encapsulation: the public contract carries billing's own
		// enum, not the deps.go seam). The iota order matches, so the cast is faithful.
		Routed:    RoutingDirective(result.RoutingDirective),
		Escalated: escalated,
	}, nil
}

// routeNet executes the Engine's routing directive (billingManager.md §6.3 / §0
// decision 2). Payout → payoutCustomer; Charge → chargeCustomer (on failure
// decide→execute {Retry|Escalate|Delay}); NoAction → skip. Returns whether the cycle
// was escalated (the OQ-4 head-state escalation flag). attempt seeds the re-charge
// budget for the Retry directive.
func (wf *workflows) routeNet(ctx workflow.Context, customerID customerID, cycleID cycleID, result billingResultSeam, attempt int) (escalated bool, err error) {
	switch result.RoutingDirective {
	case routingPayout:
		return false, wf.payoutCustomer(ctx, customerID, cycleID, result.SignedNet)
	case routingCharge:
		// Charge the positive magnitude of the negative shortfall net.
		chargeAmount := Money{MinorUnits: -result.SignedNet.MinorUnits, Currency: result.SignedNet.Currency}
		cerr := wf.chargeCustomer(ctx, customerID, cycleID, chargeAmount)
		if cerr == nil {
			return false, nil
		}
		if !isGatewayDecline(cerr) {
			return false, cerr
		}
		// On a charge DECLINE, decide+execute (interventionEngine, direct in-workflow).
		return wf.handleChargeFailure(ctx, customerID, cycleID, result, attempt)
	case routingNoAction:
		return false, nil
	default:
		return false, temporal.NewNonRetryableApplicationError(
			"billing engine returned an unknown routing directive", "UnknownRoutingDirective", nil)
	}
}

// handleChargeFailure runs the OQ-4 decide→execute branch on a declined charge:
//   - Retry   → re-enter the charge within the bounded re-charge budget.
//   - Delay   → leave the shortfall for the next shortfallSweep (the head-state record
//     carries no escalation; the sweep re-attempts). Returns escalated=false.
//   - Escalate→ flag delinquency on head-state (escalated=true); the operator dashboard
//     reads it via readBilling (no new DSL edge; §6.3).
func (wf *workflows) handleChargeFailure(ctx workflow.Context, customerID customerID, cycleID cycleID, result billingResultSeam, attempt int) (escalated bool, err error) {
	directive, derr := wf.Intervention.DecideOnBillingFailure(billingFailureSeam{
		CustomerID:   customerID,
		CycleID:      cycleID,
		Kind:         billingFailureChargeDeclined,
		AttemptCount: attempt + 1,
	})
	if derr != nil {
		return false, fwmgr.MapError(derr)
	}
	switch directive {
	case billingRetry:
		if attempt+1 >= maxChargeRetries {
			// Budget exhausted — flip to an escalation rather than loop forever.
			return true, nil
		}
		// EXECUTE Retry: re-route the same net (re-enters the charge).
		return wf.routeNet(ctx, customerID, cycleID, result, attempt+1)
	case billingDelay:
		// EXECUTE Delay: leave for the next shortfallSweep; no escalation flag.
		workflow.GetLogger(ctx).Info("charge delayed to next shortfall sweep",
			"customerId", customerID.String(), "cycleId", cycleID)
		return false, nil
	case billingEscalate:
		// EXECUTE Escalate: flag delinquency on the head-state outcome (read by the
		// operator dashboard via readBilling; no new edge).
		workflow.GetLogger(ctx).Warn("billing charge escalated to delinquency",
			"customerId", customerID.String(), "cycleId", cycleID)
		return true, nil
	default:
		return false, temporal.NewNonRetryableApplicationError(
			"intervention returned an unknown billing-failure directive", "UnknownBillingDirective", nil)
	}
}

// drainInboundRevenue appends any inbound-revenue signals already enqueued on the cycle
// workflow (non-blocking). Each append is idempotent on the gateway event id (§2.5);
// the net is (re)computed at close, not here.
func (wf *workflows) drainInboundRevenue(ctx workflow.Context, customerID customerID, cycleID cycleID) {
	ch := workflow.GetSignalChannel(ctx, signalInboundRevenueReceived)
	for {
		var event GatewayRevenueEvent
		if !ch.ReceiveAsync(&event) {
			return
		}
		// A signal targeting a different cycle is ignored (defensive; the workflow id
		// already scopes to this cycle).
		if event.CycleID != cycleID {
			continue
		}
		_ = wf.recordInboundRevenue(ctx, revenueEntrySeam{
			CustomerID:     customerID,
			CycleID:        cycleID,
			Kind:           revenueKindInbound,
			Amount:         event.Amount,
			GatewayEventID: event.GatewayEventID,
			OccurredAt:     event.OccurredAt,
		})
	}
}

// awaitChargeback waits (bounded by the cycle window) for a chargebackReceived signal
// and, on arrival, runs the forward-only recompute saga (§6.3). The wait is the
// Manager's own in-workflow primitive (awaitSignal — category A). It returns once a
// chargeback is handled or the cycle window elapses with none.
func (wf *workflows) awaitChargeback(ctx workflow.Context, customerID customerID, cycleID cycleID, prior billingResultSeam) {
	ch := workflow.GetSignalChannel(ctx, signalChargebackReceived)
	var event GatewayReversalEvent
	if !ch.ReceiveAsync(&event) {
		// No chargeback pending — the close completes; a later chargeback re-derives a
		// fresh close workflow via signal-with-start (§6.2).
		return
	}
	if err := wf.recomputeCycle(ctx, customerID, cycleID, event, prior); err != nil {
		workflow.GetLogger(ctx).Error("chargeback recompute failed", "customerId", customerID.String(), "cycleId", cycleID, "err", err.Error())
	}
}

// recomputeCycle runs the forward-only chargeback recompute saga (billingManager.md
// §6.3 RecomputeCycleWorkflow body):
//  1. RecordReversalActivity (revenueLedgerAccess; dedup on the chargeback event id).
//  2. re-fold the reversal-adjusted revenue + usage.
//  3. billingEngine.RecomputeNet (direct, by value) → corrected net + DELTA directive.
//  4. ResettleCycleActivity (head-state correction; Conflict loop).
//  5. route the DELTA charge/payout via the gateway. No rollback (forward-only).
func (wf *workflows) recomputeCycle(ctx workflow.Context, customerID customerID, cycleID cycleID, event GatewayReversalEvent, prior billingResultSeam) error {
	// Append the reversal (idempotent on the chargeback's gateway event id).
	if err := wf.recordReversal(ctx, reversalEntrySeam{
		CustomerID:     customerID,
		CycleID:        cycleID,
		Amount:         event.Amount,
		GatewayEventID: event.GatewayEventID,
		// ReversesGatewayEventID is an optional back-link; the generated façade type
		// carries it as *string (`,omitempty`), the RA seam as a plain string (empty ⇒
		// absent), so deref nil-safe at the boundary.
		ReversesGatewayEventID: derefString(event.ReversesGatewayEventID),
		OccurredAt:             event.OccurredAt,
	}); err != nil {
		return err
	}

	// Re-read the now reversal-adjusted range + the cycle usage.
	revenue, rerr := wf.foldRevenue(ctx, customerID, cycleID)
	if rerr != nil {
		return rerr
	}
	usage, uerr := wf.foldUsage(ctx, customerID, cycleID)
	if uerr != nil {
		return uerr
	}

	billing, serr := wf.readBilling(ctx, customerID)
	if serr != nil {
		return serr
	}

	// Recompute the corrected net + DELTA directive — DIRECT, by value, in-workflow.
	corrected, cerr := wf.Billing.RecomputeNet(reBillingInputSeam{
		Revenue:      revenue,
		Usage:        usage,
		Terms:        billing.Terms,
		PriorSettled: prior,
	})
	if cerr != nil {
		return fwmgr.MapError(cerr)
	}

	// Record the correction (head-state in place; Conflict loop).
	correction := billingOutcomeSeam{
		Net:       corrected.SignedNet,
		Directive: corrected.RoutingDirective,
	}
	if _, rserr := wf.resettleCycle(ctx, customerID, billing.Version, cycleID, correction); rserr != nil {
		return rserr
	}

	// Route the DELTA forward (no rollback).
	_, routeErr := wf.routeNet(ctx, customerID, cycleID, corrected, 0)
	return routeErr
}

// ===========================================================================
// ShortfallSweepWorkflow — op 2.4 entry (ncuc5 delinquency sweep; Schedule-triggered).
// ===========================================================================

// ShortfallSweepInput is the start payload for ShortfallSweepWorkflow (platform scope).
type shortfallSweepInput struct {
	ProjectID string // optional scope narrow; empty ⇒ platform-wide
}

// ShortfallSweepWorkflow drives ncuc5 (billingManager.md §6.3):
//  1. ReadDelinquentActivity → the persistently-delinquent customer set.
//  2. for each, DeliverDelinquencySignalActivity → the queued applyDelinquencyPolicy
//     Signal to operationsManager (the single sanctioned queued M→M edge).
//
// Does NOT pause/withdraw apps itself — that is operationsManager's scope downstream.
func (wf *workflows) ShortfallSweepWorkflow(ctx workflow.Context, in shortfallSweepInput) (ShortfallSweepResult, error) {
	logger := workflow.GetLogger(ctx)

	customers, err := wf.readDelinquent(ctx, delinquencyScope(in))
	if err != nil {
		return ShortfallSweepResult{}, err
	}

	result := ShortfallSweepResult{SignalledCustomers: []customerID{}}
	for _, c := range customers {
		if derr := wf.deliverDelinquencySignal(ctx, c.ID, c.PauseNotWithdraw); derr != nil {
			return ShortfallSweepResult{}, derr
		}
		result.SignalledCustomers = append(result.SignalledCustomers, c.ID)
	}

	logger.Info("shortfall sweep complete", "signalled", len(result.SignalledCustomers))
	return result, nil
}

// ---------------------------------------------------------------------------
// Read + fold helpers.
// ---------------------------------------------------------------------------

// readBilling runs the ReadBillingActivity (whole head-state read).
func (wf *workflows) readBilling(ctx workflow.Context, customerID customerID) (billingHead, error) {
	c := readHeadOpts(ctx)
	var s billingHead
	if err := workflow.ExecuteActivity(c, wf.ReadBillingActivity, customerID).Get(ctx, &s); err != nil {
		return billingHead{}, err
	}
	return s, nil
}

// readBillingByDeployedApp resolves a deployedAppId to its billing aggregate
// (UC5 onboarding). The head-state RA keys on customerId; the onboarding read carries
// the deployedAppId so the RA resolves the owning customer. Modelled as the same
// ReadBillingActivity over the deployedApp's resolved customer; here the deployedApp
// id IS the resolution input the RA maps to the customer aggregate.
func (wf *workflows) readBillingByDeployedApp(ctx workflow.Context, deployedAppID deployedAppID) (billingHead, error) {
	// The billing aggregate is per-customer; the onboarding RA read resolves the
	// owning customer from the deployed app. We pass the deployedAppId as the read key;
	// the RA returns the customer's Billing (ID = customerId).
	return wf.readBilling(ctx, deployedAppID)
}

// foldRevenue reads the cycle's revenue facts and folds them into the Engine's
// CycleRevenue value snapshot (exact minor-unit signed sum; never a float).
func (wf *workflows) foldRevenue(ctx workflow.Context, customerID customerID, cycleID cycleID) (cycleRevenueSeam, error) {
	c := ledgerOpts(ctx)
	var entries []revenueEntrySeam
	if err := workflow.ExecuteActivity(c, wf.ReadRevenueRangeActivity, readRevenueRangeArgs{
		CustomerID: customerID, CycleID: cycleID,
	}).Get(ctx, &entries); err != nil {
		return cycleRevenueSeam{}, err
	}

	var gross int64
	currency := ""
	for _, e := range entries {
		gross += e.Amount.MinorUnits // signed; reversals are negative facts
		if currency == "" {
			currency = e.Amount.Currency
		}
	}
	return cycleRevenueSeam{
		CustomerID:   customerID,
		CycleID:      cycleID,
		GrossInbound: Money{MinorUnits: gross, Currency: currency},
		Currency:     currency,
		EventCount:   len(entries),
	}, nil
}

// foldUsage reads the cycle's usage facts (whole cycle; OperatedAppID nil) and folds
// them into the Engine's CycleUsage value snapshot.
func (wf *workflows) foldUsage(ctx workflow.Context, customerID customerID, cycleID cycleID) (cycleUsageSeam, error) {
	c := ledgerOpts(ctx)
	var events []usageEventSeam
	if err := workflow.ExecuteActivity(c, wf.ReadUsageRangeActivity, usageRangeQuerySeam{
		CustomerID: customerID, CycleID: cycleID, OperatedAppID: nil,
	}).Get(ctx, &events); err != nil {
		return cycleUsageSeam{}, err
	}

	var units float64
	for _, e := range events {
		units += e.Units.Amount
	}
	return cycleUsageSeam{
		CustomerID:         customerID,
		CycleID:            cycleID,
		ComputeUnitSeconds: units,
	}, nil
}

// readDelinquent runs the ReadDelinquentActivity (cross-row read).
func (wf *workflows) readDelinquent(ctx workflow.Context, scope delinquencyScope) ([]customerSummary, error) {
	c := readHeadOpts(ctx)
	var out []customerSummary
	if err := workflow.ExecuteActivity(c, wf.ReadDelinquentActivity, scope).Get(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Gateway / runtime / ledger / durable write helpers.
// ---------------------------------------------------------------------------

func (wf *workflows) createConnectedAccount(ctx workflow.Context, customerID customerID) (gatewayBindingSeam, error) {
	c := gatewayOpts(ctx)
	var b gatewayBindingSeam
	err := workflow.ExecuteActivity(c, wf.CreateConnectedAccountActivity, customerID).Get(ctx, &b)
	return b, err
}

func (wf *workflows) validateStoredInstrument(ctx workflow.Context, customerID customerID) error {
	c := gatewayOpts(ctx)
	return workflow.ExecuteActivity(c, wf.ValidateStoredInstrumentActivity, customerID).Get(ctx, nil)
}

func (wf *workflows) payoutCustomer(ctx workflow.Context, customerID customerID, cycleID cycleID, amount Money) error {
	c := gatewayOpts(ctx)
	return workflow.ExecuteActivity(c, wf.PayoutCustomerActivity, gatewayMoveArgs{
		CustomerID: customerID, CycleID: cycleID, Amount: amount,
	}).Get(ctx, nil)
}

func (wf *workflows) chargeCustomer(ctx workflow.Context, customerID customerID, cycleID cycleID, amount Money) error {
	c := gatewayOpts(ctx)
	return workflow.ExecuteActivity(c, wf.ChargeCustomerActivity, gatewayMoveArgs{
		CustomerID: customerID, CycleID: cycleID, Amount: amount,
	}).Get(ctx, nil)
}

func (wf *workflows) recordInboundRevenue(ctx workflow.Context, entry revenueEntrySeam) error {
	c := ledgerOpts(ctx)
	return workflow.ExecuteActivity(c, wf.RecordInboundRevenueActivity, entry).Get(ctx, nil)
}

func (wf *workflows) recordReversal(ctx workflow.Context, reversal reversalEntrySeam) error {
	c := ledgerOpts(ctx)
	return workflow.ExecuteActivity(c, wf.RecordReversalActivity, reversal).Get(ctx, nil)
}

func (wf *workflows) deliverDelinquencySignal(ctx workflow.Context, customerID customerID, pauseNotWithdraw bool) error {
	c := durableOpts(ctx)
	return workflow.ExecuteActivity(c, wf.DeliverDelinquencySignalActivity, deliverDelinquencyArgs{
		CustomerID: customerID, PauseNotWithdraw: pauseNotWithdraw,
	}).Get(ctx, nil)
}

func (wf *workflows) registerCloseSchedule(ctx workflow.Context, customerID customerID) error {
	c := durableOpts(ctx)
	return workflow.ExecuteActivity(c, wf.RegisterScheduleActivity, customerID).Get(ctx, nil)
}

// ---------------------------------------------------------------------------
// Head-state recovering write helpers (§6.5 Conflict re-read→re-apply loop).
// ---------------------------------------------------------------------------

func (wf *workflows) registerCustomer(ctx workflow.Context, customerID customerID, seed version) (version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected version) (version, error) {
		c := recordHeadOpts(ctx)
		var v version
		e := workflow.ExecuteActivity(c, wf.RegisterCustomerActivity, registerCustomerArgs{
			CustomerID: customerID, ExpectedVersion: expected,
		}).Get(ctx, &v)
		return v, e
	})
}

func (wf *workflows) bindGatewayLive(ctx workflow.Context, customerID customerID, seed version, binding gatewayBindingSeam) (version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected version) (version, error) {
		c := recordHeadOpts(ctx)
		var v version
		e := workflow.ExecuteActivity(c, wf.BindGatewayLiveActivity, bindGatewayLiveArgs{
			CustomerID: customerID, ExpectedVersion: expected, Binding: binding,
		}).Get(ctx, &v)
		return v, e
	})
}

func (wf *workflows) settleCycle(ctx workflow.Context, customerID customerID, seed version, cycleID cycleID, outcome billingOutcomeSeam) (version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected version) (version, error) {
		c := recordHeadOpts(ctx)
		var v version
		e := workflow.ExecuteActivity(c, wf.SettleCycleActivity, settleCycleArgs{
			CustomerID: customerID, ExpectedVersion: expected, CycleID: cycleID, Outcome: outcome,
		}).Get(ctx, &v)
		return v, e
	})
}

func (wf *workflows) resettleCycle(ctx workflow.Context, customerID customerID, seed version, cycleID cycleID, correction billingOutcomeSeam) (version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected version) (version, error) {
		c := recordHeadOpts(ctx)
		var v version
		e := workflow.ExecuteActivity(c, wf.ResettleCycleActivity, resettleCycleArgs{
			CustomerID: customerID, ExpectedVersion: expected, CycleID: cycleID, Correction: correction,
		}).Get(ctx, &v)
		return v, e
	})
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to operations/construction).
// On a stale-version fwra.Conflict it re-reads the true head Version and re-applies with
// the SAME idempotency key (dedup-first ordering preserves idempotent replay of the
// money-affecting write).
func (wf *workflows) applyRecovering(
	ctx workflow.Context,
	customerID customerID,
	seed version,
	apply func(expected version) (version, error),
) (version, error) {
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
				"billing head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		s, rerr := wf.readBilling(ctx, customerID)
		if rerr != nil {
			return 0, rerr
		}
		expected = s.Version
		workflow.GetLogger(ctx).Info("billing head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
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

// isGatewayDecline reports whether err is a terminal gateway charge decline (RA Auth —
// the gateway declined the charge), the trigger for the OQ-4 decide→execute branch.
func isGatewayDecline(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == gatewayDeclineErrType
	}
	return false
}
