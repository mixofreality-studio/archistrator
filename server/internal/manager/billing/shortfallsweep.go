package billing

import (
	"context"
	"encoding/json"
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// ===========================================================================
// ShortfallSweepWorkflow — op 2.4 entry (ncuc5 delinquency sweep; Schedule-triggered).
// ===========================================================================

// ShortfallSweepInput is the start payload for ShortfallSweepWorkflow (platform scope).
type shortfallSweepInput struct {
	ProjectID string // optional scope narrow; empty ⇒ platform-wide
}

// ShortfallSweepWorkflow drives ncuc5 (billingManager.md §6.3; use case
// retry-a-declined-service-invoice):
//  1. ReadDeclinedCycles → every settled cycle whose charge stands declined and is not
//     escalated, each with its retry history (attempts made, sweeps aged).
//  2. for each, retryDeclined: the interventionEngine decides under settlementPolicy —
//     one re-charge under the next attempt's key (B18: it lands once the customer has
//     replaced the payment instrument), leave it for the next sweep, or escalate it —
//     and ResettleCycle records the result.
//  3. ReadDelinquentActivity → the persistently-delinquent customer set; for each,
//     DeliverDelinquencySignalActivity → the queued applyDelinquencyPolicy Signal to
//     operationsManager (the single sanctioned queued M→M edge).
//
// Does NOT pause/withdraw apps itself — that is operationsManager's scope downstream.
func (wf *workflows) ShortfallSweepWorkflow(ctx workflow.Context, in shortfallSweepInput) (ShortfallSweepResult, error) {
	logger := workflow.GetLogger(ctx)
	scope := billingstate.DelinquencyScope(in)

	declined, err := wf.Acts.BillingStateReadDeclinedCycles(ctx, scope)
	if err != nil {
		return ShortfallSweepResult{}, err
	}
	for _, c := range declined {
		if rerr := wf.retryDeclined(ctx, c); rerr != nil {
			return ShortfallSweepResult{}, rerr
		}
	}

	customers, err := wf.readDelinquent(ctx, scope)
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

	logger.Info("shortfall sweep complete", "declined", len(declined), "signalled", len(result.SignalledCustomers))
	return result, nil
}

// retryDeclined runs one declined cycle through the retry use case and records the result
// on the cycle. consult-policy: the interventionEngine (direct, in-workflow) decides on the
// cycle's recorded history under settlementPolicy. Retry → ONE re-charge under the next
// attempt's key settle:{customerId}:{cycleId}:{attempt} (B6, B18): it lands only when the
// customer has replaced the payment instrument since the decline, and a decline again is
// left for the next sweep (delay-sweep). Delay leaves the shortfall for the next sweep;
// Escalate marks it persistently delinquent. A cycle the sweep leaves declined or escalates
// has aged one more sweep; one whose charge lands is paid.
func (wf *workflows) retryDeclined(ctx workflow.Context, c billingstate.DeclinedCycle) error {
	directive, derr := wf.Intervention.DecideOnSettlementFailure(fweng.Context{Context: context.Background()},
		intervention.SettlementFailure{
			CustomerID:   intervention.CustomerID(c.CustomerID.String()),
			CycleID:      intervention.CycleID(c.Cycle),
			Kind:         intervention.ChargeDeclined,
			AttemptCount: c.Outcome.Attempts,
			ShortfallAge: c.Outcome.SweepsAged,
			Policy:       settlementPolicy(),
		})
	if derr != nil {
		return fwmgr.MapError(derr)
	}
	correction := c.Outcome
	switch directive {
	case intervention.SettlementRetry:
		amount := merchantgateway.Money{MinorUnits: -c.Outcome.Net.MinorUnits, Currency: c.Outcome.Net.Currency}
		rec, err := wf.chargeAttempt(ctx, c.CustomerID, c.Cycle, amount, chargeRecord{attempts: c.Outcome.Attempts, declined: true})
		if err != nil {
			return err
		}
		correction.Attempts = rec.attempts
		correction.Declined = rec.declined
	case intervention.SettlementDelay:
		workflow.GetLogger(ctx).Info("declined charge left for the next shortfall sweep",
			"customerId", c.CustomerID.String(), "cycleId", c.Cycle, "attempts", c.Outcome.Attempts)
	case intervention.SettlementEscalate:
		workflow.GetLogger(ctx).Warn("declined charge escalated to persistent delinquency",
			"customerId", c.CustomerID.String(), "cycleId", c.Cycle, "attempts", c.Outcome.Attempts)
		correction.Escalated = true
	default:
		return temporal.NewNonRetryableApplicationError(
			"intervention returned an unknown billing-failure directive", "UnknownBillingDirective", nil)
	}
	if correction.Declined {
		correction.SweepsAged++
	}
	billing, err := wf.readBilling(ctx, c.CustomerID)
	if err != nil {
		return err
	}
	_, err = wf.applyRecovering(ctx, c.CustomerID, billing.Version, func(expected billingstate.Version) (billingstate.Version, error) {
		return wf.Acts.BillingStateResettleCycle(ctx, c.CustomerID, expected, c.Cycle, correction)
	})
	return err
}

// readDelinquent invokes billingStateAccess.readPersistentlyDelinquentCustomers
// (cross-row read). The workflow speaks the generated billingstate.CustomerSummary /
// billingstate.DelinquencyScope contract types directly — no Manager-local mirror.
func (wf *workflows) readDelinquent(ctx workflow.Context, scope billingstate.DelinquencyScope) ([]billingstate.CustomerSummary, error) {
	return wf.Acts.BillingStateReadPersistentlyDelinquentCustomers(ctx, scope)
}

// The two wire values billing may send on applyDelinquencyPolicy. They mirror
// operations.delinquencyAction's ordinals and are pinned against them by
// Test_Delinquency_TheTwoPackagesAgreeOnTheWire (which lives on the operations side,
// because that is where the vocabulary is owned). There is deliberately no wire value for
// the vocabulary's UNKNOWN member: a sweep that cannot say pause-or-withdraw must not
// send, and the receiver refuses the zero anyway.
const (
	delinquencyActionPauseWire    = 1
	delinquencyActionWithdrawWire = 2
)

// signalApplyDelinquencyPolicy is the cross-Manager signal name delivered to
// operationsManager (matches operations.SignalApplyDelinquencyPolicy). Declared here as
// a string literal to avoid a Manager→Manager package import (the edge is queued via
// the messageBus utility, not a direct call).
const signalApplyDelinquencyPolicy = "applyDelinquencyPolicy"

// executionKindDelinquency is the messageBus ExecutionKind of operations' delinquency
// enforcement workflow — a string literal for the same reason as the signal name
// (billing may not import operations); the composition root's kind table pairs it
// with operations' registered workflow type and task queue.
const executionKindDelinquency = "operationsDelinquencyEnforcement"

// deliverDelinquencySignal invokes messageBus.deliverSignal — the one
// sanctioned queued M→M edge (applyDelinquencyPolicy → operationsManager). Fire-and-
// forget; dedup is the receiving handler's concern (D-DA §9 OQ3). The target is the
// customer's operations delinquency workflow ({customerId}:delinquency). The payload is
// JSON-encoded workflow-side (deterministic; replay-safe).
//
// THE PAYLOAD IS NOT MIRRORED ANY MORE: billing may not import operations (the signal
// name is a string literal here for exactly that reason), so the shape is written inline
// and PINNED by a cross-package json-key test rather than by a struct nobody
// compiler-links. Its keys are the operations-side applyDelinquencySignal's, FLAT, and the
// action is a small integer whose ZERO value the receiver refuses — so a producer that
// forgets to set it gets a loud refusal, where the retired bool's zero meant WITHDRAW.
//
// NO ContentType. messagebus.ExecutionPayload.ContentType is declared once and read by
// NOTHING (contract.gen.go:23; DeliverSignal passes payload.Bytes alone, messagebus.go:162,
// and the utility's own header says it is a transport, not a serialiser). A producer that
// sets it is telling its reader the transport serialises for them, which is the belief
// that dropped every lease message for a whole wave.
//
// The parameter is `cid`, not `customerID`, because the inline payload shape names the
// TYPE customerID and a same-named parameter shadows it.
func (wf *workflows) deliverDelinquencySignal(ctx workflow.Context, cid customerID, pauseNotWithdraw bool) error {
	action := delinquencyActionWithdrawWire
	if pauseNotWithdraw {
		action = delinquencyActionPauseWire
	}
	bytes, err := json.Marshal(struct {
		CustomerID customerID `json:"CustomerID"`
		Action     int        `json:"Action"`
	}{CustomerID: cid, Action: action})
	if err != nil {
		return err
	}
	// The receiver is born by the message: no operations execution runs for a customer
	// until its first delinquency, so the signal starts {customerId}:delinquency when
	// none is running (signal-with-start, the model's queued edge). Its start argument
	// is the operations workflow's input, {CustomerID}.
	start, err := json.Marshal(struct {
		CustomerID customerID `json:"CustomerID"`
	}{CustomerID: cid})
	if err != nil {
		return err
	}
	targetWorkflowID := fmt.Sprintf("%s:delinquency", cid)
	return wf.Acts.MessageBusDeliverSignal(ctx,
		messagebus.ExecutionID(targetWorkflowID),
		messagebus.SignalName(signalApplyDelinquencyPolicy),
		messagebus.ExecutionPayload{Bytes: bytes},
		&messagebus.ExecutionStart{Kind: executionKindDelinquency, Payload: messagebus.ExecutionPayload{Bytes: start}})
}
