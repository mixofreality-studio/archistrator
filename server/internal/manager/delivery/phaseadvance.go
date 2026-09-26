package delivery

import (
	"go.temporal.io/sdk/workflow"
)

// ===========================================================================
// PhaseAdvanceWorkflow — seals Phase 1 (systemDesignManager.md §6.3). Retained as
// a public, standalone short-lived gating workflow (advancePhase op) AND invoked
// inline by the parent on Phase-1 seal (runPhaseAdvance).
// ===========================================================================

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// phaseAdvanceInput is the start payload for PhaseAdvanceWorkflow.
type phaseAdvanceInput struct {
	ProjectID ProjectID
}

func (wf *workflows) PhaseAdvanceWorkflow(ctx workflow.Context, in phaseAdvanceInput) (PhaseAdvanceResult, error) {
	return wf.runPhaseAdvance(ctx, in.ProjectID)
}
