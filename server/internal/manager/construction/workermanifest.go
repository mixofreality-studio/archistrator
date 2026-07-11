package construction

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the constructionManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the four workflow
// bodies under their registered names, the 14 CUSTOM Activities the generated layer has
// no contract for (the projectEnvelope-codec reads, the constructionTransition Record*
// writes, and the git head-state RecordActivity* writes — activities_custom.go /
// gitactivities.go), the per-activity option-preset hook, and the genActivities dep
// threading. It also hosts the external RegisterManagerWorker entrypoint the composition
// root calls (cmd/server/main.go).
//
// The three Engines (handOff / intervention / review) are called DIRECTLY in-workflow
// (deterministic, by value) and are NOT Activities; the durableExecutionAccess in-workflow
// primitives (awaitSignal / startTimer / executeChild) are the Manager's own code.

import (
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
)

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
	// queryPumpDispatch returns THIS pump run's pumpDispatch decision; backs the
	// synchronous dispatch outcome ExecuteNextActivity returns WITHOUT awaiting the
	// background self-cascade drain (constructionManager.md §2.1).
	queryPumpDispatch = "pumpDispatchDecision"
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

// Custom Activity registered names — stable across the temporalgen migration. The
// workflow invokes these Activities by METHOD VALUE on the workflows receiver
// (activities_custom.go / gitactivities.go); the manifest registers them under these
// same names via CustomActivities. They cover the ops the generated layer has no
// contract for: the projectEnvelope-codec reads, the constructionTransition head-state
// transition writes, and the git head-state RecordActivity* writes.
const (
	actReadProject          = "ReadProjectActivity"
	actReadProjectVersion   = "ReadProjectVersionActivity"
	actRecordChangeReviewed = "RecordChangeReviewedActivity"
	actRecordActivityExited = "RecordActivityExitedActivity"
	actRecordActivityFailed = "RecordActivityFailedActivity"
	actRecordOperatorPaused = "RecordOperatorPausedActivity"
	actRecordPhaseStarted   = "RecordPhaseStartedActivity"
	actRecordPhaseCompleted = "RecordPhaseCompletedActivity"

	actRecordActivityBranchOpened = "RecordActivityBranchOpenedActivity"
	actRecordActivityCIObserved   = "RecordActivityCIObservedActivity"
	actRecordActivityArchApproved = "RecordActivityArchApprovedActivity"
	actRecordActivityMerged       = "RecordActivityMergedActivity"
	actRecordActivityStarted      = "RecordActivityStartedActivity"
	actRecordActivityCompleted    = "RecordActivityCompletedActivity"
)

// activityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities (pipeline / artifact / rail). A name with no entry falls
// back to the generated default (invokers.gen.go). Keyed by the generated registered
// activity name (<componentKey>.<opName>); the concrete presets reproduce the pre-migration
// per-call-site choices exactly. The CUSTOM Activities (read / record) are NOT on this
// surface — they apply their presets (readProjectOpts / recordOpts) at the workflow call
// site directly.
func activityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"constructionPipelineAccess.submitConstructionPipeline":  submitPipelineActivityOptions(),
		"constructionPipelineAccess.observeConstructionPipeline": observePipelineActivityOptions(),
		"constructionPipelineAccess.cancelConstructionPipeline":  observePipelineActivityOptions(),
		"sourceControlAccess.getInstallationToken":               mintCredActivityOptions(),
		"sourceControlAccess.openBranch":                         railActivityOptions(),
		"sourceControlAccess.openPullRequest":                    railActivityOptions(),
		"sourceControlAccess.getPullRequestStatus":               railActivityOptions(),
		"sourceControlAccess.postReview":                         railActivityOptions(),
		"sourceControlAccess.mergePullRequest":                   railActivityOptions(),
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the four workflow bodies under their registered names, the 14 custom Activities (method
// values on the workflows receiver, registered under their stable names), the per-activity
// option-preset hook, and the genActivities threaded from the impl's stored published deps.
//
// The custom Activities remain methods on the workflows receiver (construction's Activity
// receiver has always been the workflows struct, unlike billing's separate customActivities
// type) — the migration routes only the contract-backed RA ops (pipeline / artifact / rail)
// through the generated invoker surface.
func (m *constructionManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()
	engPolicy := constructionInterventionPolicy(m.interventionMode)

	// OPTIONAL per-activity git head-state slice. The gitActivityStatus dep (the published
	// 4-verb facet) must ALSO satisfy the started/completed construction facet for the
	// pump's eligibility cascade; the concrete git store/adapter do, so type-assert onto the
	// combined seam. nil/unsatisfied ⇒ dormant.
	var gitStatus gitActivityStatusAccess
	if gs, ok := m.gitActivityStatus.(gitActivityStatusAccess); ok {
		gitStatus = gs
	}

	wf := newWorkflows(wfDeps{
		HandOff:                m.handOff,
		Intervention:           m.intervention,
		Review:                 m.review,
		ProjectState:           m.projectState,
		ConstructionTransition: m.constructionTransition,
		GitStatus:              gitStatus,
		Acts:                   genInvokers{Opts: optsHook},
		// RailEnabled gates the PR-rail lifecycle (gitEnabled) alongside GitStatus + Repo.
		// The per-project Repo resolver is not wired, so the PR-rail slice stays dormant
		// (the started/completed construction records still fire when GitStatus is wired).
		RailEnabled:           m.rail != nil,
		NextEligibleActivity:  nextEligibleActivity,
		HandOffPolicy:         handoff.HandOffPolicy{},
		InterventionPolicy:    engPolicy,
		EscalationWaitTimeout: m.escalationWaitTimeout,
	})

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindPump, Fn: wf.PumpNextActivityWorkflow},
			{Name: executionKindConstructActivity, Fn: wf.ConstructActivityWorkflow},
			{Name: executionKindReplanSweep, Fn: wf.ReplanSweepWorkflow},
			{Name: executionKindProjectSupervision, Fn: wf.ProjectSupervisionWorkflow},
		},
		CustomActivities: []genRegisteredActivity{
			{Name: actReadProject, Fn: wf.ReadProjectActivity},
			{Name: actReadProjectVersion, Fn: wf.ReadProjectVersionActivity},
			{Name: actRecordChangeReviewed, Fn: wf.RecordChangeReviewedActivity},
			{Name: actRecordActivityExited, Fn: wf.RecordActivityExitedActivity},
			{Name: actRecordActivityFailed, Fn: wf.RecordActivityFailedActivity},
			{Name: actRecordOperatorPaused, Fn: wf.RecordOperatorPausedActivity},
			{Name: actRecordPhaseStarted, Fn: wf.RecordPhaseStartedActivity},
			{Name: actRecordPhaseCompleted, Fn: wf.RecordPhaseCompletedActivity},
			{Name: actRecordActivityBranchOpened, Fn: wf.RecordActivityBranchOpenedActivity},
			{Name: actRecordActivityCIObserved, Fn: wf.RecordActivityCIObservedActivity},
			{Name: actRecordActivityArchApproved, Fn: wf.RecordActivityArchApprovedActivity},
			{Name: actRecordActivityMerged, Fn: wf.RecordActivityMergedActivity},
			{Name: actRecordActivityStarted, Fn: wf.RecordActivityStartedActivity},
			{Name: actRecordActivityCompleted, Fn: wf.RecordActivityCompletedActivity},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState: m.projectState,
			Artifact:     m.artifact,
			Pipeline:     m.pipeline,
			Rail:         m.rail,
		},
	}
}

// RegisterManagerWorker wires the constructionManager onto a Temporal Worker polling the
// construction task queue (constructionManager.md §6.1). It preserves the external call
// shape the composition root used before the generated-layer migration, asserting to the
// concrete *constructionManager the generated constructor returns and delegating to the
// generated RegisterWorker with the impl's WorkerManifest.
func RegisterManagerWorker(w worker.Worker, m ConstructionManager) {
	impl, ok := m.(*constructionManager)
	if !ok {
		panic("construction: RegisterManagerWorker requires a *constructionManager from NewConstructionManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}
