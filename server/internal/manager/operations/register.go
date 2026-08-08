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
// A second registration under a DIFFERENT key against an already-registered app
// surfaces as a terminal Conflict (registerOpts, operationsmanager.go); the workflow
// does NOT retry or paper over it — registration is a one-time event, not a
// re-read→re-apply race.
func (wf *workflows) RegisterWorkflow(ctx workflow.Context, in registerInput) (Version, error) {
	v, err := wf.Acts.OperatedSystemStateRegisterOperatedSystem(ctx, in.OperatedAppID, in.CustomerID, in.ProjectRef, in.DeployableBundleRef)
	if err != nil {
		return 0, err
	}
	return Version(v), nil
}
