package systemdesign

import (
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"go.temporal.io/sdk/temporal"
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

	// SKIP-COMMITTED (2026-07-16 incident; restartability). A phase run RESTARTED via
	// startSystemDesign after an earlier run halted (a withdrawn step, or a contained
	// child failure — below) must NOT re-draft steps that are already Committed on main:
	// pre-fix, a restart re-spawned the mission child over the committed mission. Read
	// the head-state once at start and skip every already-committed step, so the restart
	// resumes at the first open step. Steps committed DURING this run are never in this
	// snapshot, so the live sequence is unchanged.
	//
	// Temporal versioning guard (replay safety; mirrors failed-gate-ledger-seed): this
	// adds a read Activity a pre-deploy phase execution's history does not carry.
	// GetVersion pins in-flight executions (DefaultVersion) to the old no-read sequence;
	// every execution started after this deploy resolves v1.
	skipCommitted := workflow.GetVersion(ctx, "phase-skip-committed-steps", workflow.DefaultVersion, 1) >= 1
	var startProj projectstate.Project
	if skipCommitted {
		if p, err := wf.readProject(ctx, in.ProjectID); err != nil {
			if !isReadNotFound(err) {
				return err
			}
			startProj = projectstate.Project{ID: projectstate.ProjectID(in.ProjectID)}
		} else {
			startProj = p
		}
	}

	// Drive the seven steps in fixed Method order. For each step, spawn the child
	// gate and auto-advance only on the child's Approve outcome; a Withdraw holds
	// the phase at that step (the operator re-enters via requestArtifactDraft).
	for _, kind := range phase1RequiredKinds() {
		if skipCommitted && slotFor(startProj, kind).Status == projectstate.ReviewCommitted {
			logger.Info("co-author step already committed at phase start; skipping", "kind", artifactKindString(kind))
			continue
		}
		childID := coAuthorWorkflowID(in.ProjectID, kind)
		cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: childID,
		})
		var outcome coAuthorOutcome
		if err := workflow.ExecuteChildWorkflow(cctx, executionKindCoAuthor, coAuthorInput{
			ProjectID:    in.ProjectID,
			ArtifactKind: kind,
		}).Get(ctx, &outcome); err != nil {
			// CHILD-FAILURE CONTAINMENT (2026-07-16 incident): a child co-author failure
			// must NOT fail the phase — pre-fix, one ContractMisuse in the glossary child
			// terminated the ENTIRE Phase-1 rail (gtdapp:systemDesign FAILED). Contain it
			// exactly like a Withdraw: log the cause and halt the sequence GRACEFULLY, so
			// the step is restartable — a fresh requestArtifactDraft revives the step as a
			// standalone session (the established withdraw re-entry path), or a fresh
			// startSystemDesign restarts this phase rail (which now skips committed steps
			// and resumes here). Only a workflow-cancellation (teardown) propagates.
			// Pure error-handling change (no new commands) — replay-safe unversioned.
			if temporal.IsCanceledError(err) {
				return err
			}
			logger.Error("co-author step FAILED; containing — phase halts gracefully (restart the step via requestArtifactDraft, or the phase via startSystemDesign)",
				"kind", artifactKindString(kind), "error", err.Error())
			return nil
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
