package construction

import (
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// This file holds the operator-supervision Signal payloads + the project-level
// supervision workflow that hosts the operator-pause branch (constructionManager.md
// §6.3 PauseProjectBranch + §6.2). The pause Signal (operatorPauseRequested) is a
// signal-with-start against {projectId}:construction; the supervision workflow
// resumes on awaitSignal, runs interventionEngine.applyPausePolicy → PausePlan
// (DECIDE), then the Manager EXECUTES the plan (cancelConstructionPipeline per
// pipeline + recordOperatorPaused).

// OperatorPauseSignal is the operatorPauseRequested payload (constructionManager.md
// §2.3). The Reason rides on the signal and is safe to log.
type OperatorPauseSignal struct {
	ProjectID ProjectID
	Reason    string
}

// OperatorOverrideSignal is the operatorOverride payload (constructionManager.md
// §2.4). Delivered to the per-activity child {projectId}:{activityId}.
type OperatorOverrideSignal struct {
	Override ActivityOverride
}

// ProjectSupervisionInput is the start payload for the project-level supervision
// workflow. It is started (signal-with-start) by the pause Signal.
type ProjectSupervisionInput struct {
	ProjectID ProjectID
}

// ProjectSupervisionWorkflow hosts the project-level operator-pause branch and the
// project-level sessionState Query. It is a long-lived workflow keyed
// {projectId}:construction; the pause Signal is signal-with-start against it. This
// is a Manager-owned WORKFLOW TYPE (implementation), not a public façade op — the
// five public ops are unchanged (constructionManager.md §6.2).
func (wf *Workflows) ProjectSupervisionWorkflow(ctx workflow.Context, in ProjectSupervisionInput) error {
	state := &constructState{projectID: in.ProjectID, stage: StageDispatching}
	if err := workflow.SetQueryHandler(ctx, QuerySessionState, func() (ConstructionSessionView, error) {
		return ConstructionSessionView{ProjectID: in.ProjectID, Stage: state.stage}, nil
	}); err != nil {
		return err
	}

	pauseCh := workflow.GetSignalChannel(ctx, SignalOperatorPauseRequested)
	var sig OperatorPauseSignal
	pauseCh.Receive(ctx, &sig)

	return wf.runPauseBranch(ctx, in.ProjectID, sig.Reason, state)
}

// runPauseBranch runs the NCUC2 operator-pause branch: applyPausePolicy (DECIDE)
// then EXECUTE the plan (constructionManager.md §6.3).
func (wf *Workflows) runPauseBranch(ctx workflow.Context, projectID ProjectID, reason string, state *constructState) error {
	plan, perr := wf.Intervention.ApplyPausePolicy(projectID.String(), PauseRequestContext{Reason: reason})
	if perr != nil {
		return fwmanager.MapError(perr)
	}

	// EXECUTE: cancel each in-flight pipeline the plan names.
	for _, pid := range plan.PipelinesToCancel {
		cc := observePipelineOpts(ctx)
		if err := workflow.ExecuteActivity(cc, wf.CancelPipelineActivity, PipelineHandle{Name: pid}).Get(ctx, nil); err != nil {
			return err
		}
	}

	// EXECUTE: abandon any in-flight worker dispatch on the pause path.
	if err := wf.cancelWorker(ctx); err != nil {
		return err
	}

	// EXECUTE: record the operator-paused head-state transition.
	if plan.RecordPaused {
		headVersion := wf.readVersion(ctx, projectID)
		if _, err := wf.applyRecovering(ctx, projectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			c := recordOpts(ctx)
			var v projectstate.Version
			e := workflow.ExecuteActivity(c, wf.RecordOperatorPausedActivity, RecordOperatorPausedArgs{
				ProjectID: projectID, ExpectedVersion: expected, Reason: reason,
			}).Get(ctx, &v)
			return v, e
		}); err != nil {
			return err
		}
	}

	state.stage = StagePaused
	return nil
}
