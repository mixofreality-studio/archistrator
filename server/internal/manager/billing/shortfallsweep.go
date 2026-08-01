package billing

import (
	"encoding/json"
	"fmt"

	"go.temporal.io/sdk/workflow"

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
func (wf *workflows) ShortfallSweepWorkflow(ctx workflow.Context, in shortfallSweepInput) (ShortfallSweepResult, error) {
	logger := workflow.GetLogger(ctx)

	customers, err := wf.readDelinquent(ctx, billingstate.DelinquencyScope(in))
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

// readDelinquent invokes billingStateAccess.readPersistentlyDelinquentCustomers
// (cross-row read). The workflow speaks the generated billingstate.CustomerSummary /
// billingstate.DelinquencyScope contract types directly — no Manager-local mirror.
func (wf *workflows) readDelinquent(ctx workflow.Context, scope billingstate.DelinquencyScope) ([]billingstate.CustomerSummary, error) {
	return wf.Acts.BillingStateReadPersistentlyDelinquentCustomers(ctx, scope)
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
