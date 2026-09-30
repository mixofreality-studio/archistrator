package operations

import (
	"encoding/json"
	"fmt"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
)

// This file holds the queued delinquency Signal payload + the delinquency-enforcement
// workflow that hosts the resumed branch (operationsManager.md §6.3
// DelinquencyEnforcementBranch + §2.5). The applyDelinquencyPolicy Signal is a
// signal-with-start against {customerId}:delinquency; the enforcement workflow resumes
// on awaitSignal("applyDelinquencyPolicy"), reads the delinquent customer's in-flight
// apps, and EXECUTES the BillingTerms-derived pause-or-withdraw the upstream Settlement
// decided. This is the ONE inbound queued cross-Manager edge (settlementManager →
// operationsManager) the layer rules permit; it is inbound (this Manager never calls
// Settlement).

// DelinquencyContext is the BillingTerms-derived enforcement directive Settlement
// decided (operationsManager.md §3.2). The Manager EXECUTES it (pause vs withdraw per
// terms); it does NOT decide it. It is the CONTRACT type carried on the public façade op,
// and since stage 4b3 its one member is DelinquencyAction rather than a bool — see below.

// DelinquencyAction's String, hand-written on the GENERATED enum (contract.gen.go). The
// vocabulary has three members because a bool has two and the missing one is the important
// one: DelinquencyActionUnknown is what a request body that named no action decodes to, and
// the branch REFUSES it. Until stage 4b3 the wire carried a non-pointer `pauseNotWithdraw`
// bool whose zero value was WITHDRAW — the only arm that acts, and irreversible — on a
// generated REST/MCP handler that does a plain decodeJSON with no required-presence check.
// So "nobody said" and "remove every in-flight app of this customer" were the same eight
// bytes. This is the rule 4b1 already wrote at the planning-assumptions site: a field whose
// value is its vocabulary's UNKNOWN member is absent in the only sense that matters.
//
// The varnames are deliberately NOT operatedsystemstate.DelinquencyAction's
// (Paused/Withdrawn): that enum records what was DONE and this one names what was ASKED, and
// a reader who confuses the directive with the recorded head state has confused the two ends
// of this branch.
func (a DelinquencyAction) String() string {
	switch a {
	case DelinquencyActionPause:
		return "pause"
	case DelinquencyActionWithdraw:
		return "withdraw"
	case DelinquencyActionUnknown:
		return "unknown"
	}
	return "unknown"
}

// applyDelinquencySignal is the applyDelinquencyPolicy payload (operationsManager.md
// §2.5), delivered to {customerId}:delinquency by TWO producers — the façade op
// (SignalWithStartWorkflow) and the billing shortfall sweep (messageBus.deliverSignal).
//
// It is FLAT because both producers are flat: billing has always sent
// {CustomerID, <the action>} and the façade nested the same single fact one level deeper.
// The nesting was the whole of the mismatch (the contract type DelinquencyContext has
// exactly one member), so flattening cost no contract change and made the two producers
// converge on ONE payload type — which is what the missing gate would have required anyway.
// This is a Manager-internal type, NOT a contract type; no generated surface moves with it.
// Its Action is the GENERATED contract enum since stage 4b3: the façade now passes the
// caller's directive straight through, so a second local vocabulary would be a mirror with
// nothing on the other side of it.
type applyDelinquencySignal struct {
	CustomerID customerID        `json:"CustomerID"`
	Action     DelinquencyAction `json:"Action"`
}

// decodeDelinquencySignal normalises the TWO wire forms this channel really carries, and
// the reason is transport, not taste: the façade sends a STRUCT through
// SignalWithStartWorkflow (json/plain) while the billing sweep sends raw []byte through
// messageBus.deliverSignal (binary/plain), and a concrete-struct receive target can hold
// the second one not at all — the SDK logs "Corrupted signal received on channel …" and
// DROPS it. That silence is what hid the whole stage-4b2 main-write lease for a wave.
//
// The shape is delivery's pumpDecodeSignal (pumpnextactivity.go:229), reproduced rather
// than shared: the two packages are different Managers and may not import each other, so
// the duplication is deliberate. The one difference is the answer to a payload it cannot
// read — the pump DROPS one (its channel names a lease holder, and acting on a message it
// cannot attribute is the hazard there), while this one REFUSES, because here the only
// action a zero value could select is the destructive one.
func decodeDelinquencySignal(raw any) (applyDelinquencySignal, error) {
	var b []byte
	switch v := raw.(type) {
	case []byte:
		b = v
	case map[string]any:
		m, err := json.Marshal(v)
		if err != nil {
			return applyDelinquencySignal{}, fmt.Errorf(
				"applyDelinquencyPolicy: payload could not be re-encoded: %w", err)
		}
		b = m
	default:
		return applyDelinquencySignal{}, fmt.Errorf(
			"applyDelinquencyPolicy: unreadable payload of type %T", raw)
	}
	var sig applyDelinquencySignal
	if err := json.Unmarshal(b, &sig); err != nil {
		return applyDelinquencySignal{}, fmt.Errorf(
			"applyDelinquencyPolicy: payload could not be decoded: %w", err)
	}
	return sig, nil
}

// delinquencyInput is the start payload for the delinquency-enforcement workflow. It
// is started (signal-with-start) by the applyDelinquencyPolicy Signal.
type delinquencyInput struct {
	CustomerID customerID
}

// DelinquencyEnforcementWorkflow hosts the ncuc5 delinquency-enforcement branch. It is
// keyed {customerId}:delinquency; the applyDelinquencyPolicy Signal is signal-with-start
// against it. This is a Manager-owned WORKFLOW TYPE (implementation), not a public
// façade op — the five public ops are unchanged (operationsManager.md §6.2). The
// workflow resumes on awaitSignal, then EXECUTES the pause-or-withdraw per app.
func (wf *workflows) DelinquencyEnforcementWorkflow(ctx workflow.Context, in delinquencyInput) error {
	// awaitSignal("applyDelinquencyPolicy") — the Manager's own in-workflow primitive
	// (D-DA category A). Resumes the enforcement branch with the delivered context.
	sigCh := workflow.GetSignalChannel(ctx, signalApplyDelinquencyPolicy)
	var raw any
	sigCh.Receive(ctx, &raw)
	sig, derr := decodeDelinquencySignal(raw)
	if derr != nil {
		// AN UNREADABLE DELINQUENCY MESSAGE IS A REFUSAL, NOT A DEFAULT. The channel name
		// carries no intent here (unlike a pause, whose NAME is the operator's intent), and
		// the only action a zero value could select is the destructive one.
		return derr
	}

	return wf.runDelinquencyBranch(ctx, in.CustomerID, sig.Action)
}

// runDelinquencyBranch runs the ncuc5 enforcement (operationsManager.md §6.3):
//  1. ReadInFlightOperatedAppsActivity (the delinquent customer's apps).
//  2. Per app, per BillingTerms: PublishDesiredStateActivity(pause-or-withdraw-patch)
//     (replicas=0 or removed).
//  3. RecordDelinquencyActionActivity (operatedSystemStateAccess.recordDelinquencyAction).
func (wf *workflows) runDelinquencyBranch(ctx workflow.Context, customerID customerID, action DelinquencyAction) error {
	logger := workflow.GetLogger(ctx)
	switch action {
	case DelinquencyActionPause, DelinquencyActionWithdraw:
	case DelinquencyActionUnknown:
		// REFUSED, LOUDLY. The retired bool's zero value selected WITHDRAW, so a payload that
		// named no action removed the runtime of every in-flight app of this customer. There
		// is no safe default here: pause publishes nothing today and withdraw is irreversible,
		// so the only honest answer to "nobody said" is to stop and say so.
		return temporal.NewNonRetryableApplicationError(
			"applyDelinquencyPolicy for customer "+customerID.String()+" named no action; "+
				"pause and withdraw are the two this branch enforces and neither may be assumed",
			"DelinquencyActionUnsaid", nil)
	}

	cid := customerID
	apps, err := wf.readInFlightOperatedApps(ctx, operatedsystemstate.InFlightScope{CustomerID: &cid})
	if err != nil {
		return err
	}

	state := operatedsystemstate.DelinquencyActionWithdrawn
	if action == DelinquencyActionPause {
		state = operatedsystemstate.DelinquencyActionPaused
	}

	for _, app := range apps {
		// EXECUTE the BillingTerms-derived enforcement: a pause is INTENDED to publish
		// replicas=0; a hard withdraw removes the runtime.
		if action == DelinquencyActionPause {
			// Deliberately does NOT call the runtime publish (review finding 2, same
			// reasoning as reconcile.go's HealthRetry/autoscale comments): the only
			// state this path could construct today is either the static model-declared
			// replica count (WRONG — a pause must scale to zero, not republish the
			// normal count) or an empty struct (the exact half-rendered-deployment
			// hazard the fail-loud fold exists to prevent). recordDelinquencyAction
			// below still records that the pause was DECIDED and applied at the
			// head-state level; the runtime-level scale-to-zero needs a replica-override
			// extension to assembleDesiredState, earmarked as a follow-up (same one
			// reconcile.go's autoscale path needs).
			logger.Info("delinquency pause: no replica-override path to publish a real scale-to-zero yet (follow-up)", "operatedAppId", app.ID.String())
		} else {
			if werr := wf.withdrawRuntime(ctx, app.ID); werr != nil {
				return werr
			}
		}
		// Record the delinquency action (head-state; Conflict loop).
		if _, rerr := wf.recordDelinquencyAction(ctx, app.ID, app.Version, state); rerr != nil {
			return rerr
		}
	}

	logger.Info("delinquency policy enforced", "customerId", customerID.String(), "apps", len(apps), "action", action.String())
	return nil
}

// recordDelinquencyAction applies the delinquency-action head-state transition. Task 4:
// action is now operatedsystemstate.DelinquencyAction directly (delinquencyActionToState,
// the former identity converter, has no remaining caller and is retired).
func (wf *workflows) recordDelinquencyAction(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version, action operatedsystemstate.DelinquencyAction) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStateRecordDelinquencyAction(ctx, appID, expected, action)
	})
}
