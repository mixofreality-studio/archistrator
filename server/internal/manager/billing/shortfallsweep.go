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

// deliverSignalPayload mirrors the applyDelinquencyPolicy payload delivered to
// operationsManager (the receiving handler dedups; D-DA §9 OQ3). The composition root
// adapts it onto messagebus.ExecutionPayload.
type deliverSignalPayload struct {
	CustomerID       customerID
	PauseNotWithdraw bool
}

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
func (wf *workflows) deliverDelinquencySignal(ctx workflow.Context, customerID customerID, pauseNotWithdraw bool) error {
	bytes, err := json.Marshal(deliverSignalPayload{CustomerID: customerID, PauseNotWithdraw: pauseNotWithdraw})
	if err != nil {
		return err
	}
	targetWorkflowID := fmt.Sprintf("%s:delinquency", customerID)
	return wf.Acts.MessageBusDeliverSignal(ctx,
		messagebus.ExecutionID(targetWorkflowID),
		messagebus.SignalName(signalApplyDelinquencyPolicy),
		messagebus.ExecutionPayload{Bytes: bytes, ContentType: "application/json"})
}
