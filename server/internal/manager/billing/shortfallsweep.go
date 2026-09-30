package billing

import (
	"encoding/json"
	"errors"
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

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
//
// TOLERANT TICK (fix round 1, Task 7c live-firing review): billingStateAccess is
// an arm-less REQUIRED binding today (no deployment perProfile arm in ANY
// profile) — the generated stub's every op returns fwra.New(fwra.Unknown, "not
// implemented"), which (Unknown.DefaultRetryable() == false) fails the
// Activity immediately, non-retryably, on the FIRST attempt of EVERY hourly
// firing. Rather than let that surface as a failed workflow execution (an
// ever-growing pile of failed hourly ticks with no operator action possible),
// an fwra.Unknown from readDelinquent is treated as a quiet no-op tick: WARN
// (naming the unimplemented RA) and complete cleanly with an empty result —
// the SAME "nothing to do this tick" shape a genuinely-empty delinquent set
// already produces. The sweep becomes real the moment billingStateAccess gains
// a real binding; no code here needs to change when that lands.
func (wf *workflows) ShortfallSweepWorkflow(ctx workflow.Context, in shortfallSweepInput) (ShortfallSweepResult, error) {
	logger := workflow.GetLogger(ctx)

	customers, err := wf.readDelinquent(ctx, billingstate.DelinquencyScope(in))
	if err != nil {
		if isRAUnimplemented(err) {
			logger.Warn("shortfall sweep: billingStateAccess is not implemented yet (arm-less required binding, no deployment arm) — quiet no-op tick; the sweep becomes real once the RA does")
			return ShortfallSweepResult{}, nil
		}
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

// readDelinquent invokes billingStateAccess.readPersistentlyDelinquentCustomers
// (cross-row read). The workflow speaks the generated billingstate.CustomerSummary /
// billingstate.DelinquencyScope contract types directly — no Manager-local mirror.
func (wf *workflows) readDelinquent(ctx workflow.Context, scope billingstate.DelinquencyScope) ([]billingstate.CustomerSummary, error) {
	return wf.Acts.BillingStateReadPersistentlyDelinquentCustomers(ctx, scope)
}

// raUnknownErrType is the canonical Temporal Type() an RA op surfaces for
// fwra.Unknown — the kind the arm-less stub constructors (contract.gen.go's
// stub<Interface>, e.g. stubBillingStateAccess) return for every op.
var raUnknownErrType = fwmgr.RAErrType(fwra.Unknown)

// isRAUnimplemented reports whether err is the arm-less-binding stub's
// fwra.Unknown ("not implemented"), surfaced through the Activity boundary as
// a non-retryable *temporal.ApplicationError (Unknown.DefaultRetryable() ==
// false, so it fails on the very first attempt, not after retries exhaust).
func isRAUnimplemented(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raUnknownErrType
	}
	return false
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
	targetWorkflowID := fmt.Sprintf("%s:delinquency", cid)
	return wf.Acts.MessageBusDeliverSignal(ctx,
		messagebus.ExecutionID(targetWorkflowID),
		messagebus.SignalName(signalApplyDelinquencyPolicy),
		messagebus.ExecutionPayload{Bytes: bytes})
}
