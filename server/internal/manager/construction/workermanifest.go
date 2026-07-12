package construction

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the constructionManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the four workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go).
//
// B8 (custom activities → generated, clean cut) migrated 13 of the former 14 CUSTOM
// Activities (activities_custom.go / gitactivities.go) onto the GENERATED invoker
// surface — their registration is now automatic via the generated RegisterWorker
// (worker.gen.go), which registers every genActivities op unconditionally. ONE Activity
// stays CUSTOM: ReadProjectActivity (see activities_custom.go for why). Since B6 dropped
// the CustomActivities manifest surface, RegisterManagerWorker below registers it
// explicitly — this is a REAL FIX, not cosmetic: before this task NONE of the 14 custom
// Activities were registered in production (the old CustomActivities field this file
// used to populate had already been deleted from genWorkerManifest by a prior regen
// commit), so ReadProjectActivity (and every other custom Activity, until migrated) was
// silently broken — any workflow.ExecuteActivity call reaching it would have failed with
// "unable to find activity type" the first time a real worker tried to run it. The 13
// migrated ops are fixed automatically (they ride the generated RegisterWorker); this
// ONE explicit registration fixes the 14th (see the task-B8 report for the precedent —
// billing's B7 rewire hit the identical systemic gap for its 3 revenue-ledger ops).
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

// activityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities. A name with no entry falls back to the generated
// default (invokers.gen.go). Keyed by the generated registered activity name
// (<componentKey>.<opName>); the concrete presets reproduce the pre-migration
// per-call-site choices exactly, including the 13 head-state Record*/version-read
// presets B8 moved here from the retired workflow.ExecuteActivity call sites (recordOpts
// / readProjectOpts's VALUE forms — recordActivityOptions/readProjectActivityOptions,
// workflow.go). The ONE remaining CUSTOM Activity (ReadProjectActivity) is NOT on this
// surface — it applies its preset (readProjectOpts) at its workflow.ExecuteActivity call
// site directly (workflow.go: readProject).
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
		// B8: re-keyed from the retired custom-Activity call sites onto their generated
		// registered names, preserving the identical timeout/retry scope.
		"projectStateAccess.readProjectVersion":              readProjectActivityOptions(),
		"constructionTransitionAccess.recordChangeReviewed":  recordActivityOptions(),
		"constructionTransitionAccess.recordActivityExited":  recordActivityOptions(),
		"constructionTransitionAccess.recordActivityFailed":  recordActivityOptions(),
		"constructionTransitionAccess.recordOperatorPaused":  recordActivityOptions(),
		"constructionTransitionAccess.recordPhaseStarted":    recordActivityOptions(),
		"constructionTransitionAccess.recordPhaseCompleted":  recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityBranchOpened": recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityCIObserved":   recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityArchApproved": recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityMerged":       recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityStarted":      recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityCompleted":    recordActivityOptions(),
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// newWorkflowsForWorker builds the workflows receiver from the manager's stored
// published deps — shared by WorkerManifest() (the genWorkerManifest.Workflows entries)
// and RegisterManagerWorker (the ONE surviving custom Activity registration below).
func (m *constructionManager) newWorkflowsForWorker(optsHook func(activityName string) (workflow.ActivityOptions, bool)) *workflows {
	engPolicy := constructionInterventionPolicy(m.interventionMode)
	return newWorkflows(wfDeps{
		HandOff:      m.handOff,
		Intervention: m.intervention,
		Review:       m.review,
		ProjectState: m.projectState,
		// GitStatus's ONLY remaining role is the "is the mirror wired" nil-check feature
		// flag (gitforward.go) — its writes are reached through Acts.GitStatus* (B8), so
		// no type-assertion onto a local seam is needed; m.gitActivityStatus already
		// speaks projectstate.GitActivityStatusAccess directly (constructionmanager.go).
		GitStatus: m.gitActivityStatus,
		Acts:      genInvokers{Opts: optsHook},
		// RailEnabled gates the PR-rail lifecycle (gitEnabled) alongside GitStatus + Repo.
		// The per-project Repo resolver is not wired, so the PR-rail slice stays dormant
		// (the started/completed construction records still fire when GitStatus is wired).
		RailEnabled:           m.rail != nil,
		NextEligibleActivity:  nextEligibleActivity,
		HandOffPolicy:         handoff.HandOffPolicy{},
		InterventionPolicy:    engPolicy,
		EscalationWaitTimeout: m.escalationWaitTimeout,
	})
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the four workflow bodies under their registered names, the per-activity option-preset
// hook, and the genActivities threaded from the impl's stored published deps.
func (m *constructionManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()
	wf := m.newWorkflowsForWorker(optsHook)

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindPump, Fn: wf.PumpNextActivityWorkflow},
			{Name: executionKindConstructActivity, Fn: wf.ConstructActivityWorkflow},
			{Name: executionKindReplanSweep, Fn: wf.ReplanSweepWorkflow},
			{Name: executionKindProjectSupervision, Fn: wf.ProjectSupervisionWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:           m.projectState,
			Artifact:               m.artifact,
			Pipeline:               m.pipeline,
			Rail:                   m.rail,
			ConstructionTransition: m.constructionTransition,
			GitStatus:              m.gitActivityStatus,
			DesignSession:          m.designSession,
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
	mf := impl.WorkerManifest()
	RegisterWorker(w, mf)
	// ReadProjectActivity is the ONE surviving CUSTOM Activity (B8; activities_custom.go).
	// The generated RegisterWorker above only knows genActivities' ops (worker.gen.go);
	// the former CustomActivities manifest surface was dropped (B6), so this hand-written
	// Activity must be registered explicitly here — see the package doc's "REAL FIX" note.
	w.RegisterActivity(impl.newWorkflowsForWorker(mf.ActivityOptions).ReadProjectActivity)
}
