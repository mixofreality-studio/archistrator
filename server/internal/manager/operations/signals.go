package operations

import (
	"go.temporal.io/sdk/workflow"
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
// terms); it does NOT decide it. PauseNotWithdraw selects the enforcement shape the
// upstream BillingTerms prescribed (pause = replicas=0; withdraw = removed).

// PauseNotWithdraw is true when BillingTerms prescribe a pause (replicas=0) rather
// than a hard withdraw. The decision is upstream (Settlement); this Manager carries
// and executes it.

// ApplyDelinquencySignal is the applyDelinquencyPolicy payload (operationsManager.md
// §2.5). Delivered by settlementManager to {customerId}:delinquency.
type ApplyDelinquencySignal struct {
	CustomerID CustomerID
	Context    DelinquencyContext
}

// DelinquencyInput is the start payload for the delinquency-enforcement workflow. It
// is started (signal-with-start) by the applyDelinquencyPolicy Signal.
type DelinquencyInput struct {
	CustomerID CustomerID
}

// DelinquencyEnforcementWorkflow hosts the ncuc5 delinquency-enforcement branch. It is
// keyed {customerId}:delinquency; the applyDelinquencyPolicy Signal is signal-with-start
// against it. This is a Manager-owned WORKFLOW TYPE (implementation), not a public
// façade op — the five public ops are unchanged (operationsManager.md §6.2). The
// workflow resumes on awaitSignal, then EXECUTES the pause-or-withdraw per app.
func (wf *Workflows) DelinquencyEnforcementWorkflow(ctx workflow.Context, in DelinquencyInput) error {
	// awaitSignal("applyDelinquencyPolicy") — the Manager's own in-workflow primitive
	// (D-DA category A). Resumes the enforcement branch with the delivered context.
	sigCh := workflow.GetSignalChannel(ctx, SignalApplyDelinquencyPolicy)
	var sig ApplyDelinquencySignal
	sigCh.Receive(ctx, &sig)

	return wf.runDelinquencyBranch(ctx, in.CustomerID, sig.Context)
}

// runDelinquencyBranch runs the ncuc5 enforcement (operationsManager.md §6.3):
//  1. ReadInFlightOperatedAppsActivity (the delinquent customer's apps).
//  2. Per app, per BillingTerms: PublishDesiredStateActivity(pause-or-withdraw-patch)
//     (replicas=0 or removed).
//  3. RecordDelinquencyActionActivity (operatedSystemStateAccess.recordDelinquencyAction).
func (wf *Workflows) runDelinquencyBranch(ctx workflow.Context, customerID CustomerID, dctx DelinquencyContext) error {
	logger := workflow.GetLogger(ctx)
	cid := customerID
	apps, err := wf.readInFlightOperatedApps(ctx, InFlightScope{CustomerID: &cid})
	if err != nil {
		return err
	}

	action := DelinquencyActionWithdrawn
	if dctx.PauseNotWithdraw {
		action = DelinquencyActionPaused
	}

	for _, app := range apps {
		// EXECUTE the BillingTerms-derived enforcement: a pause publishes replicas=0
		// (via publishDesiredState); a hard withdraw removes the runtime.
		if dctx.PauseNotWithdraw {
			if perr := wf.publishDesiredState(ctx, app.ID, RuntimeDesiredState{ContentType: "application/desired-state"}); perr != nil {
				return perr
			}
		} else {
			if werr := wf.withdrawRuntime(ctx, app.ID); werr != nil {
				return werr
			}
		}
		// Record the delinquency action (head-state; Conflict loop).
		if _, rerr := wf.recordDelinquencyAction(ctx, app.ID, app.Version, action); rerr != nil {
			return rerr
		}
	}

	logger.Info("delinquency policy enforced", "customerId", customerID.String(), "apps", len(apps), "pause", dctx.PauseNotWithdraw)
	return nil
}
