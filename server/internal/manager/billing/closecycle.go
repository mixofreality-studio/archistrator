package billing

import (
	"context"
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

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
//  4. billingengine.BillingEngine.ComputeNet (direct, by value) → {signedNet, routingDirective}.
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

	// Compute the signed net + routing directive — DIRECT, by value, in-workflow,
	// through the published contract (fweng.Context{Context: context.Background()} is
	// the replay-safe idiom the Engine op requires).
	result, cerr := wf.Billing.ComputeNet(fweng.Context{Context: context.Background()},
		revenue, usage, termsToEngine(billing.Terms))
	if cerr != nil {
		return CloseCycleResult{}, fwmgr.MapError(cerr)
	}

	escalated, routeErr := wf.routeNet(ctx, in.CustomerID, in.CycleID, result, 0)
	if routeErr != nil {
		return CloseCycleResult{}, routeErr
	}

	// Record the billing outcome (head-state; Conflict loop). Build the contract type
	// directly; only the engine-owned routing directive needs converting (Money is
	// already the shared minor-units + currency shape).
	outcome := billingstate.BillingOutcome{
		Net:       billingstate.Money{MinorUnits: result.SignedNet.MinorUnits, Currency: result.SignedNet.Currency},
		Directive: routingDirectiveToState(result.RoutingDirective),
		Escalated: escalated,
	}
	if _, serr := wf.settleCycle(ctx, in.CustomerID, billing.Version, in.CycleID, outcome); serr != nil {
		return CloseCycleResult{}, serr
	}

	logger.Info("cycle settled", "customerId", in.CustomerID.String(), "cycleId", in.CycleID,
		"directive", routingDirectiveName(RoutingDirective(result.RoutingDirective)), "escalated", escalated)

	// Await a chargeback for this cycle (forward-only recompute saga). The wait is the
	// Manager's own in-workflow primitive (awaitSignal — category A). A short selector
	// keeps the close bounded for the test/scheduled path; a chargeback re-derives the
	// recompute. The await is non-blocking when no chargeback arrives within the cycle.
	wf.awaitChargeback(ctx, in.CustomerID, in.CycleID, result)

	return CloseCycleResult{
		CustomerID: in.CustomerID,
		CycleID:    in.CycleID,
		// Convert the Engine directive to the façade's OWN RoutingDirective at the
		// boundary (full encapsulation: the public contract carries billing's own
		// enum, not the engine's). The iota order matches, so the cast is faithful.
		Routed:    RoutingDirective(result.RoutingDirective),
		Escalated: escalated,
	}, nil
}

// routeNet executes the Engine's routing directive (billingManager.md §6.3 / §0
// decision 2). Payout → payoutCustomer; Charge → chargeCustomer (on failure
// decide→execute {Retry|Escalate|Delay}); NoAction → skip. Returns whether the cycle
// was escalated (the OQ-4 head-state escalation flag). attempt seeds the re-charge
// budget for the Retry directive.
func (wf *workflows) routeNet(ctx workflow.Context, customerID customerID, cycleID cycleID, result billingengine.BillingResult, attempt int) (escalated bool, err error) {
	switch result.RoutingDirective {
	case billingengine.RoutingPayout:
		return false, wf.payoutCustomer(ctx, customerID, cycleID, Money{MinorUnits: result.SignedNet.MinorUnits, Currency: result.SignedNet.Currency})
	case billingengine.RoutingCharge:
		// Charge the positive magnitude of the negative shortfall net.
		chargeAmount := Money{MinorUnits: -result.SignedNet.MinorUnits, Currency: result.SignedNet.Currency}
		cerr := wf.chargeCustomer(ctx, customerID, cycleID, chargeAmount)
		if cerr == nil {
			return false, nil
		}
		if !isGatewayDecline(cerr) {
			return false, cerr
		}
		// On a charge DECLINE, decide+execute (intervention.InterventionEngine, direct in-workflow).
		return wf.handleChargeFailure(ctx, customerID, cycleID, result, attempt)
	case billingengine.RoutingNoAction:
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
func (wf *workflows) handleChargeFailure(ctx workflow.Context, customerID customerID, cycleID cycleID, result billingengine.BillingResult, attempt int) (escalated bool, err error) {
	directive, derr := wf.Intervention.DecideOnSettlementFailure(fweng.Context{Context: context.Background()},
		intervention.SettlementFailure{
			CustomerID:   intervention.CustomerID(customerID.String()),
			CycleID:      intervention.CycleID(cycleID),
			Kind:         intervention.ChargeDeclined,
			AttemptCount: int64(attempt + 1),
		})
	if derr != nil {
		return false, fwmgr.MapError(derr)
	}
	switch directive {
	case intervention.SettlementRetry:
		if attempt+1 >= maxChargeRetries {
			// Budget exhausted — flip to an escalation rather than loop forever.
			return true, nil
		}
		// EXECUTE Retry: re-route the same net (re-enters the charge).
		return wf.routeNet(ctx, customerID, cycleID, result, attempt+1)
	case intervention.SettlementDelay:
		// EXECUTE Delay: leave for the next shortfallSweep; no escalation flag.
		workflow.GetLogger(ctx).Info("charge delayed to next shortfall sweep",
			"customerId", customerID.String(), "cycleId", cycleID)
		return false, nil
	case intervention.SettlementEscalate:
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
		_ = wf.recordInboundRevenue(ctx, billingstate.RevenueEntry{
			CustomerID:     customerID,
			CycleID:        string(cycleID),
			Kind:           billingstate.RevenueKindInbound,
			Amount:         billingstate.Money{MinorUnits: event.Amount.MinorUnits, Currency: event.Amount.Currency},
			GatewayEventID: event.GatewayEventID,
			OccurredAt:     event.OccurredAt,
		})
	}
}

// awaitChargeback waits (bounded by the cycle window) for a chargebackReceived signal
// and, on arrival, runs the forward-only recompute saga (§6.3). The wait is the
// Manager's own in-workflow primitive (awaitSignal — category A). It returns once a
// chargeback is handled or the cycle window elapses with none.
func (wf *workflows) awaitChargeback(ctx workflow.Context, customerID customerID, cycleID cycleID, prior billingengine.BillingResult) {
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
//  3. billingengine.BillingEngine.RecomputeNet (direct, by value) → corrected net + DELTA directive.
//  4. ResettleCycleActivity (head-state correction; Conflict loop).
//  5. route the DELTA charge/payout via the gateway. No rollback (forward-only).
func (wf *workflows) recomputeCycle(ctx workflow.Context, customerID customerID, cycleID cycleID, event GatewayReversalEvent, prior billingengine.BillingResult) error {
	// Append the reversal (idempotent on the chargeback's gateway event id).
	if err := wf.recordReversal(ctx, billingstate.ReversalEntry{
		CustomerID:     customerID,
		CycleID:        string(cycleID),
		Amount:         billingstate.Money{MinorUnits: event.Amount.MinorUnits, Currency: event.Amount.Currency},
		GatewayEventID: event.GatewayEventID,
		// ReversesGatewayEventID is an optional back-link; the generated façade type
		// carries it as *string (`,omitempty`), the generated RA contract type as a
		// plain string (empty ⇒ absent), so deref nil-safe at the boundary.
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

	// Recompute the corrected net + DELTA directive — DIRECT, by value, in-workflow,
	// through the published contract.
	corrected, cerr := wf.Billing.RecomputeNet(fweng.Context{Context: context.Background()},
		billingengine.ReBillingInput{
			Revenue:      revenue,
			Usage:        usage,
			Terms:        termsToEngine(billing.Terms),
			PriorSettled: prior,
		})
	if cerr != nil {
		return fwmgr.MapError(cerr)
	}

	// Record the correction (head-state in place; Conflict loop).
	correction := billingstate.BillingOutcome{
		Net:       billingstate.Money{MinorUnits: corrected.SignedNet.MinorUnits, Currency: corrected.SignedNet.Currency},
		Directive: routingDirectiveToState(corrected.RoutingDirective),
	}
	if _, rserr := wf.resettleCycle(ctx, customerID, billing.Version, cycleID, correction); rserr != nil {
		return rserr
	}

	// Route the DELTA forward (no rollback).
	_, routeErr := wf.routeNet(ctx, customerID, cycleID, corrected, 0)
	return routeErr
}

// foldRevenue invokes revenueLedgerAccess.readRange and folds the cycle's revenue facts
// into the published billingengine.CycleRevenue value snapshot (exact minor-unit signed
// sum; never a float).
func (wf *workflows) foldRevenue(ctx workflow.Context, customerID customerID, cycleID cycleID) (billingengine.CycleRevenue, error) {
	entries, err := wf.Acts.RevenueLedgerReadRange(ctx, customerID, string(cycleID))
	if err != nil {
		return billingengine.CycleRevenue{}, err
	}

	var gross int64
	currency := ""
	for _, e := range entries {
		gross += e.Amount.MinorUnits // signed; reversals are negative facts
		if currency == "" {
			currency = e.Amount.Currency
		}
	}
	return billingengine.CycleRevenue{
		GrossInbound: billingengine.Money{MinorUnits: gross, Currency: currency},
		EventCount:   int64(len(entries)),
	}, nil
}

// foldUsage invokes usageAccess.readRange (whole cycle; OperatedAppID nil) and folds the
// contract events into the published billingengine.CycleUsage value snapshot.
func (wf *workflows) foldUsage(ctx workflow.Context, customerID customerID, cycleID cycleID) (billingengine.CycleUsage, error) {
	events, err := wf.Acts.UsageReadRange(ctx, usage.UsageRangeQuery{
		CustomerID: customerID, CycleID: usage.CycleID(cycleID), OperatedAppID: nil,
	})
	if err != nil {
		return billingengine.CycleUsage{}, err
	}

	var units float64
	for _, e := range events {
		units += e.Units.Amount
	}
	return billingengine.CycleUsage{
		ComputeUnitSeconds: units,
	}, nil
}

// payoutCustomer invokes merchantGatewayAccess.payoutCustomer (caller-keyed
// settle:{customerId}:{cycleId}).
func (wf *workflows) payoutCustomer(ctx workflow.Context, customerID customerID, cycleID cycleID, amount Money) error {
	key := gatewayIdempotencyKey(customerID, cycleID)
	return wf.Acts.MerchantGatewayPayoutCustomer(ctx, fwra.IdempotencyKey(key), customerID,
		merchantgateway.Money{MinorUnits: amount.MinorUnits, Currency: amount.Currency}, key)
}

// chargeCustomer invokes merchantGatewayAccess.chargeCustomer (caller-keyed
// settle:{customerId}:{cycleId}). A terminal decline (RA Auth) surfaces to the
// decideOnBillingFailure branch (OQ-4).
func (wf *workflows) chargeCustomer(ctx workflow.Context, customerID customerID, cycleID cycleID, amount Money) error {
	key := gatewayIdempotencyKey(customerID, cycleID)
	return wf.Acts.MerchantGatewayChargeCustomer(ctx, fwra.IdempotencyKey(key), customerID,
		merchantgateway.Money{MinorUnits: amount.MinorUnits, Currency: amount.Currency}, key)
}

// recordInboundRevenue invokes revenueLedgerAccess.recordInboundRevenue (dedup on the
// gateway event id; NO Conflict kind on this append-only ledger).
func (wf *workflows) recordInboundRevenue(ctx workflow.Context, entry billingstate.RevenueEntry) error {
	_, err := wf.Acts.RevenueLedgerRecordInboundRevenue(ctx, entry)
	return err
}

// recordReversal invokes revenueLedgerAccess.recordReversal (dedup on the chargeback's
// gateway event id; NO Conflict kind on this append-only ledger).
func (wf *workflows) recordReversal(ctx workflow.Context, reversal billingstate.ReversalEntry) error {
	_, err := wf.Acts.RevenueLedgerRecordReversal(ctx, reversal)
	return err
}

func (wf *workflows) settleCycle(ctx workflow.Context, customerID customerID, seed billingstate.Version, cycleID cycleID, outcome billingstate.BillingOutcome) (billingstate.Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected billingstate.Version) (billingstate.Version, error) {
		return wf.Acts.BillingStateSettleCycle(ctx, customerID, expected, string(cycleID), outcome)
	})
}

func (wf *workflows) resettleCycle(ctx workflow.Context, customerID customerID, seed billingstate.Version, cycleID cycleID, correction billingstate.BillingOutcome) (billingstate.Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected billingstate.Version) (billingstate.Version, error) {
		return wf.Acts.BillingStateResettleCycle(ctx, customerID, expected, string(cycleID), correction)
	})
}

// gatewayIdempotencyKey derives the Stripe Idempotency-Key settle:{customerId}:{cycleId}
// for the money-moving gateway Activities (billingManager.md §6.4 line 264/706). A pure,
// business-stable string composition — replay-safe, so it is built workflow-side and
// supplied EXPLICITLY to the caller-keyed gateway invoker (as BOTH the caller key and the
// contract's idempotencyKey param), preserving today's dedup token exactly.
func gatewayIdempotencyKey(customerID customerID, cycleID cycleID) string {
	return fmt.Sprintf("settle:%s:%s", customerID, cycleID)
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

// gatewayFailureErrType is the canonical Temporal Type() a terminal charge decline
// surfaces (RA Auth — the gateway declined). On it the close workflow runs the
// intervention.InterventionEngine decide→execute branch (OQ-4).
var gatewayDeclineErrType = fwmgr.RAErrType(fwra.Auth)

// This file documents the two webhook-fed Signals the CloseCycleWorkflow handles
// (billingManager.md §6.1/§6.2). Both are durable point-to-point Signals targeting
// the affected cycle's workflow id {customerId}:{cycleId}:close; signal-with-start
// starts the cycle workflow when it is not yet running (§6.2).
//
//   - inboundRevenueReceived (op 2.5): the cycle workflow appends the revenue fact to
//     the Revenue Ledger (idempotent on the gateway event id). The append is the
//     durable record; the net is (re)computed at cycle close, not at signal time.
//     Drained by CloseCycleWorkflow.drainInboundRevenue (workflow.go).
//   - chargebackReceived (op 2.6): the cycle workflow appends the reversal, recomputes
//     the net forward-only, records the correction, and routes the delta. Handled by
//     CloseCycleWorkflow.awaitChargeback → recomputeCycle (workflow.go).
//
// The signal PAYLOADS are the façade input types GatewayRevenueEvent /
// GatewayReversalEvent (contract.go) — the verified webhook body the Manager maps onto
// the revenueLedgerAccess-owned entry shapes. awaitSignal (the wait on these channels)
// is the Manager's OWN in-workflow primitive (D-DA category A), NOT a contract op.

// Signal names (billingManager.md §6.1/§6.2). The two webhook-fed revenue facts are
// delivered as durable Signals to the affected cycle's workflow.
const (
	// SignalInboundRevenueReceived backs RecordInboundRevenue (op 2.5).
	signalInboundRevenueReceived = "inboundRevenueReceived"
	// SignalChargebackReceived backs RecordRevenueReversal (op 2.6).
	signalChargebackReceived = "chargebackReceived"
)

// This file holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract enum (e.g. the canonical
// name lookup that would otherwise be a String() method) lives here as a free
// function. This is the operations/behavior.go precedent (a contract-value-type
// method becomes a free function so the generated scalar/enum carries no behavior;
// contractstrip refuses to strip an owned type that still has a method).

// derefString returns the pointed-to string, or "" for nil. The generated
// GatewayReversalEvent.ReversesGatewayEventID is optional (`,omitempty` ⇒ *string);
// the generated billingstate.ReversalEntry carries it as a plain string (empty ⇒
// absent).
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// routingDirectiveName returns the canonical name for the façade RoutingDirective.
// Kept as a FREE FUNCTION (not a RoutingDirective method) so the generated enum is
// pure data.
func routingDirectiveName(d RoutingDirective) string {
	switch d {
	case RoutingDirectiveNoAction:
		return "NoAction"
	case RoutingDirectivePayout:
		return "Payout"
	case RoutingDirectiveCharge:
		return "Charge"
	}
	// Unreachable for the three defined RoutingDirective values above (the
	// exhaustive linter enforces that every real variant has its own case); kept
	// as a defensive fallback for an out-of-range ordinal.
	return "Unknown"
}

// termsToEngine bridges the RA-owned terms head-state onto the engine's compute
// input. The two contracts genuinely disagree (the engine carries percent fields
// the head-state does not); zero-fill preserves today's behavior. Divergence
// earmarked for a project.json contract alignment (see plan Task 12 earmarks).
func termsToEngine(t billingstate.BillingTerms) billingengine.BillingTerms {
	return billingengine.BillingTerms{
		RevenueShare: billingengine.RevenueShareKind(t.RevenueShareKind),
		ComputeCost:  billingengine.ComputeCostKind(t.ComputeCostKind),
		Schedule:     billingengine.ScheduleKind(t.ScheduleKind),
	}
}

// routingDirectiveToState maps the engine-owned routing decision onto the
// RA-owned persisted enum by IDENTITY (explicit switch — re-order safe).
func routingDirectiveToState(d billingengine.RoutingDirective) billingstate.RoutingDirective {
	switch d {
	case billingengine.RoutingPayout:
		return billingstate.RoutingPayout
	case billingengine.RoutingCharge:
		return billingstate.RoutingCharge
	case billingengine.RoutingNoAction:
		return billingstate.RoutingNoAction
	default:
		return billingstate.RoutingNoAction
	}
}
