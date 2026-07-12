package operations

import (
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
)

// ===========================================================================
// DeployWorkflow — op 2.1 entry (operator deploy / scale / policy republish).
// ===========================================================================

// deployInput is the start payload for DeployWorkflow.
type deployInput struct {
	OperatedAppID operatedAppID
	Change        DesiredStateChange
}

// DeployWorkflow drives UC4 deploy (operationsManager.md §6.3):
//  1. ReadOperatedSystemActivity → head-state (desiredState, deployableBundleRef).
//  2. (first deploy, full bundle) RetrieveDeployableBundleActivity.
//  3. PublishDesiredStateActivity (the git commit).
//  4. RecordPublishDesiredStateActivity (head-state transition, reason=operator|deploy).
func (wf *workflows) DeployWorkflow(ctx workflow.Context, in deployInput) (DeployResult, error) {
	logger := workflow.GetLogger(ctx)

	op, err := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if err != nil {
		return DeployResult{}, err
	}

	// Deploy pre-condition (§2.1): the operated system has a deployableBundleRef for a
	// first take-live (full bundle). FailedPrecondition is a terminal façade-class
	// error surfaced from the workflow.
	if in.Change.Reason == ReasonDeployAfterConstruction && in.Change.PatchKind == PatchFullBundle {
		if op.DeployableBundleRef == "" {
			return DeployResult{}, temporal.NewNonRetryableApplicationError(
				"operated system has no deployableBundleRef (no constructed output to deploy)",
				fwmgr.ManagerErrType(fwmgr.FailedPrecondition), nil)
		}
		// Retrieve the deployable bundle the publish renders from.
		if _, berr := wf.retrieveBundle(ctx, op.DeployableBundleRef); berr != nil {
			return DeployResult{}, berr
		}
	}

	// Publish the rendered desired state (git commit; content-idempotent).
	revision := publishRevision(in.OperatedAppID, in.Change.ChangeID)
	if perr := wf.publishDesiredState(ctx, in.OperatedAppID, operatedruntime.RuntimeDesiredState{
		Bytes:       in.Change.RenderedDesiredState,
		ContentType: "application/desired-state",
	}); perr != nil {
		return DeployResult{}, perr
	}

	// Record the head-state desired-state transition (additive; Conflict loop).
	if _, rerr := wf.recordPublishDesiredState(ctx, in.OperatedAppID, op.Version, in.Change.Reason, nil); rerr != nil {
		return DeployResult{}, rerr
	}

	logger.Info("deploy published desired state", "operatedAppId", in.OperatedAppID.String(), "reason", desiredStateReasonName(in.Change.Reason))
	return DeployResult{Published: true, Revision: &revision}, nil
}

// ===========================================================================
// artifactAccess — EXISTS as a Go package (internal/resourceaccess/artifact) but the
// frozen retrieveDeployableBundle verb is NOT yet on it (it has
// RetrieveConstructionOutput). Consumed here via a NARROW seam interface mirroring
// the frozen verb; the composition root adapts the concrete *artifact.Store once the
// verb lands (escalation E-1 in C-MOP.md). The bundle ref is a plain content
// address (a string), matching the package's content-address discipline.
// ===========================================================================

// NOTE: the artifactAccess consumer-seam interface is retired (see the
// operatedSystemStateAccess note above) — reached through the generated invoker
// ArtifactRetrieveConstructionOutput (escalation E-1: the deployable bundle IS a
// construction output until the frozen retrieveDeployableBundle verb lands). The
// deployableBundle mirror below remains as the workflow's retrieve-bundle result.

// deployableBundle mirrors the constructed-output bundle retrieved for a first
// deploy. Re-uses the existing artifact.ConstructionOutput shape as the bundle body
// (the deployable bundle IS a construction output — artifactAccess.md), kept as a
// thin Manager-local wrapper so the seam stays narrow.
type deployableBundle struct {
	Output artifact.ConstructionOutput
}

// ---------------------------------------------------------------------------
// Head-state read + recovering write helpers (§6.5).
// ---------------------------------------------------------------------------

// readOperatedSystem invokes operatedSystemStateAccess.readOperatedSystem. Task 4: the
// former Manager-local operatedSystem mirror is retired — the invoker's contract type
// IS the workflow's internal currency now, so no fold happens here.
// Shared workflow-context helper (used by 4 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) readOperatedSystem(ctx workflow.Context, operatedAppID operatedAppID) (operatedsystemstate.OperatedSystem, error) {
	return wf.Acts.OperatedSystemStateReadOperatedSystem(ctx, operatedAppID)
}

// retrieveBundle invokes artifactAccess.retrieveConstructionOutput (escalation E-1:
// the deployable bundle IS a construction output until the frozen
// retrieveDeployableBundle verb lands).
func (wf *workflows) retrieveBundle(ctx workflow.Context, ref string) (deployableBundle, error) {
	out, err := wf.Acts.ArtifactRetrieveConstructionOutput(ctx, ref)
	if err != nil {
		return deployableBundle{}, err
	}
	return deployableBundle{Output: out}, nil
}

// publishDesiredState invokes operatedRuntimeAccess.publishDesiredState (git commit;
// content-idempotent). Task 4: the former Manager-local runtimeDesiredState mirror is
// retired — desired IS the contract type now.
// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) publishDesiredState(ctx workflow.Context, appID operatedAppID, desired operatedruntime.RuntimeDesiredState) error {
	return wf.Acts.OperatedRuntimePublishDesiredState(ctx, appID, desired)
}

// recordPublishDesiredState applies the head-state desired-state transition with the
// Conflict loop (§6.5). decision is carried only for reason=autoscale. Task 4: seed/
// return now speak operatedsystemstate.Version directly (the former Manager-local
// version mirror is retired). Task 5: decision is now the published *autoscaler.Decision
// (the seam autoscaleDecisionSeam is retired) — autoscaleDecisionToState (adapters.go)
// bridges it straight to operatedsystemstate.AutoscaleDecision.
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) recordPublishDesiredState(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version, reason DesiredStateReason, decision *autoscaler.Decision) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStatePublishDesiredState(ctx, appID,
			expected,
			desiredStateReasonToState(reason),
			autoscaleDecisionToState(decision))
	})
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to construction). On a
// stale-version fwra.Conflict it re-reads the true head Version and re-applies with
// the SAME idempotency key (dedup-first ordering preserves idempotent replay).
// Shared workflow-context helper (used by 4 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) applyRecovering(
	ctx workflow.Context,
	appID operatedAppID,
	seed operatedsystemstate.Version,
	apply func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error),
) (operatedsystemstate.Version, error) {
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		op, rerr := wf.readOperatedSystem(ctx, appID)
		if rerr != nil {
			return 0, rerr
		}
		expected = op.Version
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
}

// publishRevision derives a deterministic published-revision token for UI correlation
// (opaque; not a Temporal id).
func publishRevision(appID operatedAppID, changeID string) string {
	return appID.String() + ":" + changeID
}
