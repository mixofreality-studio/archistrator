package operations

import (
	"errors"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// ===========================================================================
// WithdrawWorkflow — op 2.3 (ncuc3 withdraw).
// ===========================================================================

// withdrawInput is the start payload for WithdrawWorkflow.
type withdrawInput struct {
	OperatedAppID operatedAppID
	Reason        WithdrawReason
}

// WithdrawWorkflow drives ncuc3 (operationsManager.md §6.3):
//  1. WithdrawRuntimeActivity (operatedRuntimeAccess.withdraw; NotFound ⇒ success).
//  2. RecordFinalUsageActivity (usageAccess.recordFinalUsage).
//  3. WithdrawHeadStateActivity (operatedSystemStateAccess.withdrawSystem).
//
// Idempotent on the id; an already-withdrawn app collapses to a no-op success
// (NotFound on the runtime withdraw maps to success in the RA; a withdrawn head-state
// is recorded idempotently on its dedup key).
func (wf *workflows) WithdrawWorkflow(ctx workflow.Context, in withdrawInput) (WithdrawResult, error) {
	logger := workflow.GetLogger(ctx)

	op, err := wf.readOperatedSystem(ctx, in.OperatedAppID)
	if err != nil {
		// A missing operated app is treated as an already-withdrawn no-op success
		// (the desired post-condition — "no running runtime" — already holds).
		if isReadNotFound(err) {
			return WithdrawResult{Withdrawn: true}, nil
		}
		return WithdrawResult{}, err
	}
	if op.Status == operatedsystemstate.RuntimeStatusWithdrawn {
		return WithdrawResult{Withdrawn: true}, nil
	}

	if werr := wf.withdrawRuntime(ctx, in.OperatedAppID); werr != nil {
		return WithdrawResult{}, werr
	}

	// Capture the final usage before the runtime is pruned. A best-effort final read of
	// compute attribution drives the recordFinalUsage append (dedup-id idempotent).
	attribution, aerr := wf.readComputeAttribution(ctx, in.OperatedAppID)
	if aerr != nil {
		return WithdrawResult{}, aerr
	}
	if attribution.RuntimeEventID != "" {
		if uerr := wf.recordFinalUsage(ctx, in.OperatedAppID, attribution); uerr != nil {
			return WithdrawResult{}, uerr
		}
	}

	if _, herr := wf.withdrawHeadState(ctx, in.OperatedAppID, op.Version); herr != nil {
		return WithdrawResult{}, herr
	}

	logger.Info("withdrawn", "operatedAppId", in.OperatedAppID.String())
	return WithdrawResult{Withdrawn: true}, nil
}

// withdrawRuntime invokes operatedRuntimeAccess.withdraw (NotFound ⇒ success in the RA).
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) withdrawRuntime(ctx workflow.Context, appID operatedAppID) error {
	return wf.Acts.OperatedRuntimeWithdraw(ctx, appID)
}

// recordFinalUsage invokes usageAccess.recordFinalUsage (append; dedup-id idempotent).
func (wf *workflows) recordFinalUsage(ctx workflow.Context, appID operatedAppID, attribution operatedruntime.ComputeAttribution) error {
	_, err := wf.Acts.UsageRecordFinalUsage(ctx, []usage.UsageEvent{wf.usageEvent(ctx, appID, attribution)})
	return err
}

// withdrawHeadState applies the withdraw head-state transition.
func (wf *workflows) withdrawHeadState(ctx workflow.Context, appID operatedAppID, seed operatedsystemstate.Version) (operatedsystemstate.Version, error) {
	return wf.applyRecovering(ctx, appID, seed, func(expected operatedsystemstate.Version) (operatedsystemstate.Version, error) {
		return wf.Acts.OperatedSystemStateWithdrawSystem(ctx, appID, expected)
	})
}

// isReadNotFound reports whether err is a head-state read's NotFound.
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// raNotFoundErrType is the canonical Temporal Type() ReadOperatedSystem surfaces for a
// missing operated app.
var raNotFoundErrType = fwmgr.RAErrType(fwra.NotFound)
