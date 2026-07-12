package systemdesign

import (
	"go.temporal.io/sdk/workflow"
)

// ===========================================================================
// PhaseAdvanceWorkflow — seals Phase 1 (systemDesignManager.md §6.3). Retained as
// a public, standalone short-lived gating workflow (advancePhase op) AND invoked
// inline by the parent on Phase-1 seal (runPhaseAdvance).
// ===========================================================================

// phaseAdvanceInput is the start payload for PhaseAdvanceWorkflow.
type phaseAdvanceInput struct {
	ProjectID ProjectID
}

func (wf *workflows) PhaseAdvanceWorkflow(ctx workflow.Context, in phaseAdvanceInput) (PhaseAdvanceResult, error) {
	return wf.runPhaseAdvance(ctx, in.ProjectID)
}
