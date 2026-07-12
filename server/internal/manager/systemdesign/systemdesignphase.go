package systemdesign

import (
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"go.temporal.io/sdk/workflow"
)

// ===========================================================================
// SystemDesignPhaseWorkflow — the PARENT (2026-05-29; systemDesignManager.md
// §0b / §6, rework §2.2). Drives the seven Phase-1 steps in fixed Method order,
// spawning the per-step child gate via executeChild, auto-advancing on each
// human Approve, and sealing Phase 1 after step 7.
//
//   mission → glossary → scrubbed-requirements → volatilities → core-use-cases
//   → system(architecture) → operational-concepts → standard-check → SEAL
//
// (Phase1RequiredKinds() is the fixed ordered sequence — the single source of
// truth shared with the seal gate.)
// ===========================================================================

// phaseInput is the start payload for SystemDesignPhaseWorkflow.
type phaseInput struct {
	ProjectID ProjectID
}

func (wf *workflows) SystemDesignPhaseWorkflow(ctx workflow.Context, in phaseInput) error {
	logger := workflow.GetLogger(ctx)

	// Drive the seven steps in fixed Method order. For each step, spawn the child
	// gate and auto-advance only on the child's Approve outcome; a Withdraw holds
	// the phase at that step (the operator re-enters via requestArtifactDraft).
	for _, kind := range phase1RequiredKinds() {
		childID := coAuthorWorkflowID(in.ProjectID, kind)
		cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: childID,
		})
		var outcome coAuthorOutcome
		if err := workflow.ExecuteChildWorkflow(cctx, executionKindCoAuthor, coAuthorInput{
			ProjectID:    in.ProjectID,
			ArtifactKind: kind,
		}).Get(ctx, &outcome); err != nil {
			return err
		}
		if outcome != coAuthorApproved {
			// The human withdrew this step; the phase does not advance. The parent
			// stops here — re-entry is via a fresh requestArtifactDraft on the step.
			logger.Info("co-author step not approved; halting phase sequence", "kind", artifactKindString(kind), "outcome", int(outcome))
			return nil
		}
		logger.Info("co-author step approved; advancing phase sequence", "kind", artifactKindString(kind))
	}

	// All seven steps approved → seal Phase 1 (advancePhase). The parent runs the
	// same gate as the standalone PhaseAdvanceWorkflow inline.
	res, err := wf.runPhaseAdvance(ctx, in.ProjectID)
	if err != nil {
		return err
	}
	if !res.Advanced {
		logger.Warn("phase seal blocked despite all steps approved", "missing", res.MissingArtifacts)
	}
	return nil
}

// runPhaseAdvance is the shared seal gate body, called by both the standalone
// PhaseAdvanceWorkflow and the parent SystemDesignPhaseWorkflow.
// Shared workflow-context helper (used by 2 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) runPhaseAdvance(ctx workflow.Context, projectID ProjectID) (PhaseAdvanceResult, error) {
	var proj projectstate.Project
	if p, err := wf.readProject(ctx, projectID); err != nil {
		if !isReadNotFound(err) {
			return PhaseAdvanceResult{}, err
		}
		proj = projectstate.Project{ID: projectstate.ProjectID(projectID)}
	} else {
		proj = p
	}

	// Gate: every required Phase-1 kind must be Committed.
	var missing []ArtifactKind
	for _, kind := range phase1RequiredKinds() {
		if slotFor(proj, kind).Status != projectstate.ReviewCommitted {
			missing = append(missing, kind)
		}
	}
	if len(missing) > 0 {
		return PhaseAdvanceResult{Advanced: false, MissingArtifacts: missing}, nil
	}

	// All required slots committed → seal. Per the agentic pivot (§0d.5) the
	// artifactValidationEngine is DROPPED from this Manager: validity is the required
	// CI check inside the Action (a slot only reaches ReviewCommitted after its design
	// job's CI validation went green AND the architect Approved), so an in-workflow
	// re-validation of the standard-check here would re-implement the CI gate the
	// Action already enforces. The all-committed gate is the seal condition.

	// Seal Phase 1. AdvancePhase is a MAIN write (Conflict re-read targets main, branch=="").
	if _, err := wf.applyRecovering(ctx, projectID, "", proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ProjectStateAdvancePhase(ctx, projectstate.ProjectID(projectID), expected)
	}); err != nil {
		return PhaseAdvanceResult{}, err
	}
	return PhaseAdvanceResult{Advanced: true}, nil
}
