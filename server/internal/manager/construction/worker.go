package construction

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (constructionManager.md §6.1/§6.2).
// ---------------------------------------------------------------------------

// TaskQueue is the one queue per Manager that the in-process Temporal Worker in
// the server polls (constructionManager.md §6.1; operational-concepts.md §6 —
// "one Worker per task queue: ... construction ...").
const TaskQueue = "construction"

// Signal and query names (constructionManager.md §6.1/§6.2).
const (
	// SignalOperatorPauseRequested resumes a suspended construction execution at
	// its awaitSignal; backs PauseProject (NCUC2).
	SignalOperatorPauseRequested = "operatorPauseRequested"
	// SignalOperatorOverride resumes a per-activity child workflow; backs
	// OverrideActivity.
	SignalOperatorOverride = "operatorOverride"
	// QuerySessionState returns a ConstructionSessionView; backs GetSessionState.
	QuerySessionState = "sessionState"
)

// ExecutionKinds — the registered workflow names (constructionManager.md §6.2).
const (
	// ExecutionKindPump is the per-tick PumpNextActivityWorkflow (the 30s pump).
	ExecutionKindPump = "constructionPumpNextActivity"
	// ExecutionKindConstructActivity is the per-activity child workflow.
	ExecutionKindConstructActivity = "constructionConstructActivity"
	// ExecutionKindReplanSweep is the per-tick ReplanSweepWorkflow (the 5m sweep).
	ExecutionKindReplanSweep = "constructionReplanSweep"
	// ExecutionKindProjectSupervision is the long-lived project-level supervision
	// workflow that hosts the operator-pause branch + project-level session Query.
	ExecutionKindProjectSupervision = "constructionProjectSupervision"
)

// Schedule ids + cadences (constructionManager.md §6.1; operational-concepts.md §4).
const (
	scheduleIDNextActivity = "construction:nextActivity"
	scheduleIDReplanSweep  = "construction:replanSweep"

	// nextActivityInterval is the pump cadence (30s).
	nextActivityInterval = 30 * time.Second
	// replanSweepInterval is the variance-sweep cadence (5m).
	replanSweepInterval = 5 * time.Minute
)

// Activity name constants. The Activity methods are registered under these stable
// names and the workflow bodies invoke them by the method value on wf, so the
// registered name and the call stay in lockstep (constructionManager.md §6.4).
const (
	actReadProject             = "ReadProjectActivity"
	actGenerateWork            = "GenerateWorkActivity"
	actCancelWorker            = "CancelWorkerActivity"
	actSubmitPipeline          = "SubmitPipelineActivity"
	actObservePipeline         = "ObservePipelineActivity"
	actCancelPipeline          = "CancelPipelineActivity"
	actStoreConstructionOutput = "StoreConstructionOutputActivity"
	actRecordChangeReviewed    = "RecordChangeReviewedActivity"
	actRecordActivityExited    = "RecordActivityExitedActivity"
	actRecordActivityFailed    = "RecordActivityFailedActivity"
	actRecordOperatorPaused    = "RecordOperatorPausedActivity"

	// git-forward slice (C-MCN-GIT): the PR-rail + git head-state Record activities.
	actMintRepoCredential         = "MintRepoCredentialActivity"
	actOpenBranch                 = "OpenBranchActivity"
	actOpenPullRequest            = "OpenPullRequestActivity"
	actGetPullRequestStatus       = "GetPullRequestStatusActivity"
	actPostReview                 = "PostReviewActivity"
	actMergePullRequest           = "MergePullRequestActivity"
	actRecordActivityBranchOpened = "RecordActivityBranchOpenedActivity"
	actRecordActivityCIObserved   = "RecordActivityCIObservedActivity"
	actRecordActivityArchApproved = "RecordActivityArchApprovedActivity"
	actRecordActivityMerged       = "RecordActivityMergedActivity"
	// per-activity construction head-state (Task 3): started/completed lifecycle.
	actRecordActivityStarted   = "RecordActivityStartedActivity"
	actRecordActivityCompleted = "RecordActivityCompletedActivity"
)

// RegisterWorker wires the constructionManager onto a Temporal Worker polling the
// construction task queue (constructionManager.md §6.1). The Manager's workflow
// dependencies — the three Engines (handOffEngine, interventionEngine,
// reviewEngine, called DIRECTLY in-workflow) and the ResourceAccess ports
// (projectStateAccess, artifactAccess, workerAccess, constructionPipelineAccess)
// — live on a single Workflows struct (there is no separate Activities type).
//
// The three Engines' verbs are called DIRECTLY from workflow code (deterministic,
// by value) and are NOT Activities. The durableExecutionAccess in-workflow
// primitives (awaitSignal / startTimer / executeChild) are the Manager's own code
// (category A) and are NOT Activities either.
func RegisterWorker(w worker.Worker, deps Deps) {
	wf := newWorkflows(deps)

	w.RegisterWorkflowWithOptions(wf.PumpNextActivityWorkflow, workflow.RegisterOptions{Name: ExecutionKindPump})
	w.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: ExecutionKindConstructActivity})
	w.RegisterWorkflowWithOptions(wf.ReplanSweepWorkflow, workflow.RegisterOptions{Name: ExecutionKindReplanSweep})
	w.RegisterWorkflowWithOptions(wf.ProjectSupervisionWorkflow, workflow.RegisterOptions{Name: ExecutionKindProjectSupervision})

	w.RegisterActivityWithOptions(wf.ReadProjectActivity, activity.RegisterOptions{Name: actReadProject})
	w.RegisterActivityWithOptions(wf.GenerateWorkActivity, activity.RegisterOptions{Name: actGenerateWork})
	w.RegisterActivityWithOptions(wf.CancelWorkerActivity, activity.RegisterOptions{Name: actCancelWorker})
	w.RegisterActivityWithOptions(wf.SubmitPipelineActivity, activity.RegisterOptions{Name: actSubmitPipeline})
	w.RegisterActivityWithOptions(wf.ObservePipelineActivity, activity.RegisterOptions{Name: actObservePipeline})
	w.RegisterActivityWithOptions(wf.CancelPipelineActivity, activity.RegisterOptions{Name: actCancelPipeline})
	w.RegisterActivityWithOptions(wf.StoreConstructionOutputActivity, activity.RegisterOptions{Name: actStoreConstructionOutput})
	w.RegisterActivityWithOptions(wf.RecordChangeReviewedActivity, activity.RegisterOptions{Name: actRecordChangeReviewed})
	w.RegisterActivityWithOptions(wf.RecordActivityExitedActivity, activity.RegisterOptions{Name: actRecordActivityExited})
	w.RegisterActivityWithOptions(wf.RecordActivityFailedActivity, activity.RegisterOptions{Name: actRecordActivityFailed})
	w.RegisterActivityWithOptions(wf.RecordOperatorPausedActivity, activity.RegisterOptions{Name: actRecordOperatorPaused})

	// git-forward slice (C-MCN-GIT). Registered unconditionally; the workflow only
	// invokes them when the git slice is wired (Rail+GitStatus+Repo non-nil) — a
	// dormant slice never reaches these, so registering them is inert otherwise.
	w.RegisterActivityWithOptions(wf.MintRepoCredentialActivity, activity.RegisterOptions{Name: actMintRepoCredential})
	w.RegisterActivityWithOptions(wf.OpenBranchActivity, activity.RegisterOptions{Name: actOpenBranch})
	w.RegisterActivityWithOptions(wf.OpenPullRequestActivity, activity.RegisterOptions{Name: actOpenPullRequest})
	w.RegisterActivityWithOptions(wf.GetPullRequestStatusActivity, activity.RegisterOptions{Name: actGetPullRequestStatus})
	w.RegisterActivityWithOptions(wf.PostReviewActivity, activity.RegisterOptions{Name: actPostReview})
	w.RegisterActivityWithOptions(wf.MergePullRequestActivity, activity.RegisterOptions{Name: actMergePullRequest})
	w.RegisterActivityWithOptions(wf.RecordActivityBranchOpenedActivity, activity.RegisterOptions{Name: actRecordActivityBranchOpened})
	w.RegisterActivityWithOptions(wf.RecordActivityCIObservedActivity, activity.RegisterOptions{Name: actRecordActivityCIObserved})
	w.RegisterActivityWithOptions(wf.RecordActivityArchApprovedActivity, activity.RegisterOptions{Name: actRecordActivityArchApproved})
	w.RegisterActivityWithOptions(wf.RecordActivityMergedActivity, activity.RegisterOptions{Name: actRecordActivityMerged})
	w.RegisterActivityWithOptions(wf.RecordActivityStartedActivity, activity.RegisterOptions{Name: actRecordActivityStarted})
	w.RegisterActivityWithOptions(wf.RecordActivityCompletedActivity, activity.RegisterOptions{Name: actRecordActivityCompleted})
}

// RegisterSchedules registers (idempotently) the nextActivity (30s) +
// replanSweep (5m) Temporal Schedules at startup via durableExecutionAccess
// (constructionManager.md §6.1; FU-MCN-1). Called once at process start.
func RegisterSchedules(ctx context.Context, durable DurableExecutionAccess) error {
	if err := durable.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDNextActivity,
		WorkflowType: ExecutionKindPump,
		TaskQueue:    TaskQueue,
		IntervalSecs: int(nextActivityInterval / time.Second),
	}); err != nil {
		return err
	}
	return durable.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDReplanSweep,
		WorkflowType: ExecutionKindReplanSweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: int(replanSweepInterval / time.Second),
	})
}
