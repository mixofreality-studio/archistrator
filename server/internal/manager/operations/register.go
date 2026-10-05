package operations

import (
	"go.temporal.io/sdk/workflow"
)

// ===========================================================================
// RegisterWorkflow — op 2.8 entry (onboarding seed).
// ===========================================================================

// registerInput is the start payload for RegisterWorkflow.
type registerInput struct {
	OperatedAppID       operatedAppID
	CustomerID          customerID
	ProjectRef          string
	DeployableBundleRef string
}

// RegisterWorkflow drives the onboarding seed (operationsManager.md §6.3): a single
// call to operatedSystemStateAccess.RegisterOperatedSystem, which creates the
// head-state row at version 1 (customer, project, and deployable-bundle ref) or —
// replayed under the SAME idempotency key — returns the already-recorded version.
//
// A REPLAYED REGISTRATION ANSWERS THE VERSION ALREADY RECORDED
// (register-a-delivered-system-for-operation, note-register-once). The activity's
// idempotency key is run-scoped, so the caller replaying RegisterOperatedApp starts a
// NEW run whose write carries a new key and meets the RA's already-registered Conflict.
// That Conflict is a replay, not a second registration, exactly when the recorded row
// IS this registration — the same customer, project and deployable bundle — and the
// answer is then the version on record, as the RA itself answers a same-key replay.
// A registration that differs in any of the three stays the terminal Conflict
// (registerOpts, operationsmanager.go): the workflow does not retry or paper over it —
// registration is a one-time event, not a re-read→re-apply race.
func (wf *workflows) RegisterWorkflow(ctx workflow.Context, in registerInput) (Version, error) {
	v, err := wf.Acts.OperatedSystemStateRegisterOperatedSystem(ctx, in.OperatedAppID, in.CustomerID, in.ProjectRef, in.DeployableBundleRef)
	if err == nil {
		return Version(v), nil
	}
	if !isConflict(err) {
		return 0, err
	}
	op, rerr := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if rerr != nil {
		return 0, rerr
	}
	if op.CustomerID != in.CustomerID || op.ProjectRef != in.ProjectRef || op.DeployableBundleRef != in.DeployableBundleRef {
		return 0, err
	}
	return Version(op.Version), nil
}
