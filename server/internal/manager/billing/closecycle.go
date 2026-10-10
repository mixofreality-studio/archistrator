package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

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
// example.
// ===========================================================================

// CloseInput is the start payload for CloseCycleWorkflow. The façade
// (CloseBillingCycle) names both; the customer's cycle-close Schedule names the customer
// alone, and the firing closes the cycle that has just elapsed (elapsedCycle).
type closeInput struct {
	CustomerID customerID
	CycleID    cycleID
}

// cycleIDLayout is a cycle id's layout: a calendar month.
const cycleIDLayout = "2006-01"

// elapsedCycle is the cycle a scheduled close firing at now closes: the calendar month
// before now's, in UTC — the Schedule fires at the start of every month (closeCycleCron).
func elapsedCycle(now time.Time) cycleID {
	start := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	return start.AddDate(0, -1, 0).Format(cycleIDLayout)
}

// CloseCycleWorkflow drives UC6 cycle-close (billingManager.md §6.3; use case
// bill-the-user-for-usage):
//  1. ReadBillingActivity → terms + expectedVersion.
//  2. ReadUsageRangeActivity → the usage value snapshot (revenue is the zero snapshot).
//  3. billingengine.BillingEngine.ComputeNet (direct, by value) → {signedNet, routingDirective}.
//  4. execute the directive: Charge (each decline decided by the interventionEngine:
//     retry under the next attempt's key, or record the persistent decline) / NoAction.
//  5. SettleCycleActivity (head-state; Conflict loop) — the outcome with its charge's
//     retry history (Declined, Escalated, Attempts), which the shortfall sweep reads.
func (wf *workflows) CloseCycleWorkflow(ctx workflow.Context, in closeInput) (CloseCycleResult, error) {
	logger := workflow.GetLogger(ctx)

	if in.CycleID == "" {
		// A scheduled firing: workflow.Now is the firing's recorded time, so the derived
		// cycle replays identically.
		in.CycleID = elapsedCycle(workflow.Now(ctx))
	}

	billing, err := wf.readBilling(ctx, in.CustomerID)
	if err != nil {
		return CloseCycleResult{}, err
	}
	if !billing.GatewayBound {
		return CloseCycleResult{}, temporal.NewNonRetryableApplicationError(
			"customer is not registered + gateway-bound; cannot close cycle",
			fwmgr.ErrType(fwmgr.FailedPrecondition), nil)
	}

	usage, uerr := wf.foldUsage(ctx, in.CustomerID, in.CycleID)
	if uerr != nil {
		return CloseCycleResult{}, uerr
	}

	// Compute the signed net + routing directive — DIRECT, by value, in-workflow,
	// through the published contract (fweng.Context{Context: context.Background()} is
	// the replay-safe idiom the Engine op requires).
	result, cerr := wf.Billing.ComputeNet(fweng.Context{Context: context.Background()},
		chargeOnlyRevenue(billing.Terms.Currency), usage, termsToEngine(billing.Terms))
	if cerr != nil {
		return CloseCycleResult{}, fwmgr.MapError(cerr)
	}

	charge, routeErr := wf.routeNet(ctx, in.CustomerID, in.CycleID, result)
	if routeErr != nil {
		return CloseCycleResult{}, routeErr
	}

	// Record the billing outcome (head-state; Conflict loop). Build the contract type
	// directly; only the engine-owned routing directive needs converting (Money is
	// already the shared minor-units + currency shape).
	outcome := billingstate.BillingOutcome{
		Net:       billingstate.Money{MinorUnits: result.SignedNet.MinorUnits, Currency: result.SignedNet.Currency},
		Directive: routingDirectiveToState(result.RoutingDirective),
		Declined:  charge.declined,
		Escalated: charge.escalated,
		Attempts:  charge.attempts,
	}
	if _, serr := wf.settleCycle(ctx, in.CustomerID, billing.Version, in.CycleID, outcome); serr != nil {
		return CloseCycleResult{}, serr
	}

	logger.Info("cycle settled", "customerId", in.CustomerID.String(), "cycleId", in.CycleID,
		"directive", routingDirectiveName(RoutingDirective(result.RoutingDirective)),
		"attempts", charge.attempts, "declined", charge.declined, "escalated", charge.escalated)

	return CloseCycleResult{
		CustomerID: in.CustomerID,
		CycleID:    in.CycleID,
		// Convert the Engine directive to the façade's OWN RoutingDirective at the
		// boundary (full encapsulation: the public contract carries billing's own
		// enum, not the engine's). The iota order matches, so the cast is faithful.
		Routed:    RoutingDirective(result.RoutingDirective),
		Escalated: charge.escalated,
	}, nil
}

// chargeRecord is what executing a cycle's routing directive leaves on its outcome: the
// charge attempts made, and whether the charge stands declined and, if so, escalated to
// persistent delinquency.
type chargeRecord struct {
	attempts  int64
	declined  bool
	escalated bool
}

// routeNet executes the Engine's routing directive (billingManager.md §6.3 / §0
// decision 2). Charge → chargeCustomer, each decline decided by the interventionEngine
// under closeSettlementPolicy (retries-left [yes] re-charges under the next attempt's
// key; [no] records the persistent decline); NoAction → skip. Charge-only: there is no
// Payout directive — a non-negative net routes NoAction.
func (wf *workflows) routeNet(ctx workflow.Context, customerID customerID, cycleID cycleID, result billingengine.BillingResult) (chargeRecord, error) {
	switch result.RoutingDirective {
	case billingengine.RoutingCharge:
		// Charge the positive magnitude of the negative shortfall net.
		amount := merchantgateway.Money{MinorUnits: -result.SignedNet.MinorUnits, Currency: result.SignedNet.Currency}
		return wf.chargeWithRetries(ctx, customerID, cycleID, amount, chargeRecord{}, 0, closeSettlementPolicy())
	case billingengine.RoutingNoAction:
		return chargeRecord{}, nil
	default:
		return chargeRecord{}, temporal.NewNonRetryableApplicationError(
			"billing engine returned an unknown routing directive", "UnknownRoutingDirective", nil)
	}
}

// chargeWithRetries is the charge loop the close and the shortfall sweep share (use
// cases bill-the-user-for-usage charge-user → charge-succeeded → retries-left, and
// retry-a-declined-service-invoice consult-policy → retry-budget → recharge → charge-ok).
// rec is the charge's history so far; shortfallAge the sweeps the shortfall has aged.
// Before every charge it asks the interventionEngine (direct, in-workflow) what the
// history allows, under policy: Retry charges under the next attempt's key — a success
// clears the decline, a decline is decided again; Delay leaves the shortfall declined for
// the next sweep; Escalate marks it persistently delinquent. A first charge (a close) is
// made unasked: there is no failure to decide on yet.
func (wf *workflows) chargeWithRetries(ctx workflow.Context, customerID customerID, cycleID cycleID, amount merchantgateway.Money, rec chargeRecord, shortfallAge int64, policy intervention.InterventionPolicy) (chargeRecord, error) {
	for {
		if rec.declined {
			directive, derr := wf.Intervention.DecideOnSettlementFailure(fweng.Context{Context: context.Background()},
				intervention.SettlementFailure{
					CustomerID:   intervention.CustomerID(customerID.String()),
					CycleID:      intervention.CycleID(cycleID),
					Kind:         intervention.ChargeDeclined,
					AttemptCount: rec.attempts,
					ShortfallAge: shortfallAge,
					Policy:       policy,
				})
			if derr != nil {
				return chargeRecord{}, fwmgr.MapError(derr)
			}
			switch directive {
			case intervention.SettlementRetry:
				// retries-left [yes]: the next attempt, below.
			case intervention.SettlementDelay:
				workflow.GetLogger(ctx).Info("declined charge left for the next shortfall sweep",
					"customerId", customerID.String(), "cycleId", cycleID, "attempts", rec.attempts)
				return rec, nil
			case intervention.SettlementEscalate:
				workflow.GetLogger(ctx).Warn("declined charge escalated to persistent delinquency",
					"customerId", customerID.String(), "cycleId", cycleID, "attempts", rec.attempts)
				rec.escalated = true
				return rec, nil
			default:
				return chargeRecord{}, temporal.NewNonRetryableApplicationError(
					"intervention returned an unknown billing-failure directive", "UnknownBillingDirective", nil)
			}
		}
		cerr := wf.chargeCustomer(ctx, customerID, cycleID, rec.attempts, amount)
		rec.attempts++
		if cerr == nil {
			rec.declined = false
			return rec, nil
		}
		if !isGatewayDecline(cerr) {
			return chargeRecord{}, cerr
		}
		rec.declined = true
	}
}

// settlementPolicy is billingManager's own intervention policy for a declined charge
// (the Manager's Tiered configuration, as operations' is for health): a declined charge
// is retried while fewer than settlementRetryBudget attempts have been made, then left
// for the shortfall sweep while it has aged fewer than settlementToleranceSweeps sweeps,
// then escalated to persistent delinquency.
func settlementPolicy() intervention.InterventionPolicy {
	return intervention.InterventionPolicy{
		Mode:                     intervention.Tiered,
		RetryBudget:              settlementRetryBudget,
		ShortfallToleranceSweeps: settlementToleranceSweeps,
	}
}

// closeSettlementPolicy is settlementPolicy at a cycle's close. The close grants the
// charge its retry budget but no sweeps of tolerance: tolerance is what the shortfall
// sweep extends to a shortfall it finds declined, while a charge the close cannot land
// within its retries is a persistent decline (bill-the-user-for-usage retries-left [no]
// → persistent-decline → customer-delinquent).
func closeSettlementPolicy() intervention.InterventionPolicy {
	p := settlementPolicy()
	p.ShortfallToleranceSweeps = 0
	return p
}

// chargeOnlyRevenue is the cycle's revenue snapshot under the charge-only model: zero,
// from no events, in the currency the customer's terms settle in (DCT §12 B13). The net
// is the hosting-cost charge alone.
func chargeOnlyRevenue(currency string) billingengine.CycleRevenue {
	return billingengine.CycleRevenue{GrossInbound: billingengine.Money{Currency: currency}}
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

// chargeCustomer invokes merchantGatewayAccess.chargeCustomer (caller-keyed
// settle:{customerId}:{cycleId}:{attempt}). A terminal decline (RA Auth) surfaces to the
// decideOnBillingFailure branch (OQ-4).
func (wf *workflows) chargeCustomer(ctx workflow.Context, customerID customerID, cycleID cycleID, attempt int64, amount merchantgateway.Money) error {
	key := gatewayIdempotencyKey(customerID, cycleID, attempt)
	return wf.Acts.MerchantGatewayChargeCustomer(ctx, fwra.IdempotencyKey(key), customerID, amount, key)
}

func (wf *workflows) settleCycle(ctx workflow.Context, customerID customerID, seed billingstate.Version, cycleID cycleID, outcome billingstate.BillingOutcome) (billingstate.Version, error) {
	return wf.applyRecovering(ctx, customerID, seed, func(expected billingstate.Version) (billingstate.Version, error) {
		return wf.Acts.BillingStateSettleCycle(ctx, customerID, expected, string(cycleID), outcome)
	})
}

// gatewayIdempotencyKey derives the Stripe Idempotency-Key
// settle:{customerId}:{cycleId}:{attempt} for the money-moving gateway Activities
// (spec §12 B6). attempt is the 0-based charge attempt of this close; the key carries it
// 1-based, the AttemptCount the intervention policy reads. A gateway replays a key's
// recorded outcome, a decline included, so a retry under the first attempt's key could
// never succeed: every retry is a new attempt under a new key, while a replay of one
// attempt (a Temporal activity retry) reuses that attempt's key and charges at most once.
// A pure, business-stable string composition — replay-safe, so it is built workflow-side
// and supplied EXPLICITLY to the caller-keyed gateway invoker (as BOTH the caller key and
// the contract's idempotencyKey param).
func gatewayIdempotencyKey(customerID customerID, cycleID cycleID, attempt int64) string {
	return fmt.Sprintf("settle:%s:%s:%d", customerID, cycleID, attempt+1)
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

// This file holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract enum (e.g. the canonical
// name lookup that would otherwise be a String() method) lives here as a free
// function. This is the operations/behavior.go precedent (a contract-value-type
// method becomes a free function so the generated scalar/enum carries no behavior;
// contractstrip refuses to strip an owned type that still has a method).

// routingDirectiveName returns the canonical name for the façade RoutingDirective.
// Kept as a FREE FUNCTION (not a RoutingDirective method) so the generated enum is
// pure data.
func routingDirectiveName(d RoutingDirective) string {
	switch d {
	case RoutingDirectiveNoAction:
		return "NoAction"
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
		ComputeCost:          billingengine.ComputeCostKind(t.ComputeCostKind),
		ComputeMarkupPercent: t.ComputeMarkupPercent,
		Schedule:             billingengine.ScheduleKind(t.ScheduleKind),
	}
}

// routingDirectiveToState maps the engine-owned routing decision onto the
// RA-owned persisted enum by IDENTITY (explicit switch — re-order safe).
func routingDirectiveToState(d billingengine.RoutingDirective) billingstate.RoutingDirective {
	switch d {
	case billingengine.RoutingCharge:
		return billingstate.RoutingCharge
	case billingengine.RoutingNoAction:
		return billingstate.RoutingNoAction
	default:
		return billingstate.RoutingNoAction
	}
}
