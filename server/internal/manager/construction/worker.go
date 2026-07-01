package construction

import (
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
	// signalOperatorPauseRequested resumes a suspended construction execution at
	// its awaitSignal; backs PauseProject (NCUC2).
	signalOperatorPauseRequested = "operatorPauseRequested"
	// signalOperatorOverride resumes a per-activity child workflow; backs
	// OverrideActivity.
	signalOperatorOverride = "operatorOverride"
	// signalPhaseDecision delivers a phase-gated approval/send-back decision to a
	// per-activity child workflow; backs SubmitPhaseDecision.
	signalPhaseDecision = "phaseDecision"
	// querySessionState returns a ConstructionSessionView; backs GetSessionState.
	querySessionState = "sessionState"
)

// ExecutionKinds — the registered workflow names (constructionManager.md §6.2).
const (
	// executionKindPump is the per-tick PumpNextActivityWorkflow (the 30s pump).
	executionKindPump = "constructionPumpNextActivity"
	// executionKindConstructActivity is the per-activity child workflow.
	executionKindConstructActivity = "constructionConstructActivity"
	// executionKindReplanSweep is the per-tick ReplanSweepWorkflow (the 5m sweep).
	executionKindReplanSweep = "constructionReplanSweep"
	// executionKindProjectSupervision is the long-lived project-level supervision
	// workflow that hosts the operator-pause branch + project-level session Query.
	executionKindProjectSupervision = "constructionProjectSupervision"
)

// Activity name constants. The Activity methods are registered under these stable
// names and the workflow bodies invoke them by the method value on wf, so the
// registered name and the call stay in lockstep (constructionManager.md §6.4).
const (
	actReadProject             = "ReadProjectActivity"
	actReadProjectVersion      = "ReadProjectVersionActivity"
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
	actMintRepoCredential         = "MintRepoCredentialActivity" // #nosec G101 -- Temporal activity NAME constant, not a credential
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

	// phase-record head-state (Task 5): phase started/completed lifecycle.
	actRecordPhaseStarted   = "RecordPhaseStartedActivity"
	actRecordPhaseCompleted = "RecordPhaseCompletedActivity"
)

// RegisterWorker wires the constructionManager onto a Temporal Worker polling the
// construction task queue (constructionManager.md §6.1). The Manager's workflow
// dependencies — the three Engines (handOffEngine, interventionEngine,
// reviewEngine, called DIRECTLY in-workflow) and the ResourceAccess ports
// (projectStateAccess, artifactAccess, workerAccess, constructionPipelineAccess)
// — live on a single workflows struct (there is no separate Activities type).
//
// The three Engines' verbs are called DIRECTLY from workflow code (deterministic,
// by value) and are NOT Activities. The durableExecutionAccess in-workflow
// primitives (awaitSignal / startTimer / executeChild) are the Manager's own code
// (category A) and are NOT Activities either.
func RegisterWorker(w worker.Worker, m ConstructionManager) {
	impl, ok := m.(*constructionManager)
	if !ok {
		panic("construction: RegisterWorker requires a *constructionManager from NewConstructionManager")
	}

	// Fold the published deps the constructor stored into the unexported seams the
	// workflows struct holds (adapters.go) — the Option-B boundary mapping that
	// replaces the former composition-root WireDeps/WithGitForward.
	engPolicy, mgrPolicy := constructionInterventionPolicy(impl.interventionMode)

	deps := wfDeps{
		HandOff:                handoffAdapter{inner: impl.handOff},
		Intervention:           interventionAdapter{inner: impl.intervention, policy: engPolicy},
		Review:                 reviewAdapter{inner: impl.review},
		ProjectState:           impl.projectState,
		ConstructionTransition: impl.constructionTransition,
		Pipeline:               pipelineAdapter{inner: impl.pipeline},
		Artifacts:              artifactAdapter{inner: impl.artifact},
		Workers:                workerAdapter{inner: impl.worker},
		NextEligibleActivity:   nextEligibleActivity,
		HandOffPolicy:          handOffPolicy{},
		InterventionPolicy:     mgrPolicy,
		EscalationWaitTimeout:  impl.escalationWaitTimeout,
	}
	// OPTIONAL PR rail (dormant until a per-project Repo resolver is also wired —
	// gitEnabled gates on Rail+GitStatus+Repo). nil rail leaves it unset.
	if impl.rail != nil {
		deps.Rail = railAdapter{inner: impl.rail}
	}
	// OPTIONAL per-activity git head-state slice. The gitActivityStatus dep (the
	// published 4-verb facet) must ALSO satisfy the started/completed construction
	// facet for the pump's eligibility cascade; the concrete git store/adapter do, so
	// type-assert onto the combined seam. nil/unsatisfied ⇒ dormant.
	if gs, gok := impl.gitActivityStatus.(gitActivityStatusAccess); gok {
		deps.GitStatus = gs
	}

	wf := newWorkflows(deps)

	w.RegisterWorkflowWithOptions(wf.PumpNextActivityWorkflow, workflow.RegisterOptions{Name: executionKindPump})
	w.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	w.RegisterWorkflowWithOptions(wf.ReplanSweepWorkflow, workflow.RegisterOptions{Name: executionKindReplanSweep})
	w.RegisterWorkflowWithOptions(wf.ProjectSupervisionWorkflow, workflow.RegisterOptions{Name: executionKindProjectSupervision})

	w.RegisterActivityWithOptions(wf.ReadProjectActivity, activity.RegisterOptions{Name: actReadProject})
	w.RegisterActivityWithOptions(wf.ReadProjectVersionActivity, activity.RegisterOptions{Name: actReadProjectVersion})
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
	w.RegisterActivityWithOptions(wf.RecordPhaseStartedActivity, activity.RegisterOptions{Name: actRecordPhaseStarted})
	w.RegisterActivityWithOptions(wf.RecordPhaseCompletedActivity, activity.RegisterOptions{Name: actRecordPhaseCompleted})
}
