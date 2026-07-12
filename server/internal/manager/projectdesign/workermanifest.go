package projectdesign

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the projectDesignManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the three workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go). This Manager has ZERO
// custom Temporal Activities (B9 + its follow-up ruling: the last one,
// StageArtifactForReviewActivity, was deleted when the designSessionAccess Stage op's
// model param became the codable ModelEnvelope at the schema) — every Activity is
// generated and registered by the generated RegisterWorker.
//
// The three estimate Engines (Estimation / OperationEst / Settlement) are called DIRECTLY
// in-workflow (deterministic, by value) and are NOT Activities; the durable-execution
// in-workflow primitives (awaitSignal / startTimer) are the Manager's own code.

import (
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// activityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities (projectState / pipeline / rail / designSession). A name
// with no entry falls back to the generated default (invokers.gen.go). Keyed by the
// generated registered activity name (<componentKey>.<opName>); the concrete presets
// reproduce the pre-migration per-call-site choices exactly (B9 disclosure: every
// designSessionAccess.* entry below reproduces the retired custom Activity's
// readProjectOpts/mutateOpts preset byte-for-byte).
func activityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"projectStateAccess.readProjectVersion":                  readProjectActivityOptions(),
		"projectStateAccess.advancePhase":                        mutateActivityOptions(),
		"constructionPipelineAccess.submitConstructionPipeline":  dispatchActivityOptions(),
		"constructionPipelineAccess.observeConstructionPipeline": observeActivityOptions(),
		"sourceControlAccess.getInstallationToken":               mintCredActivityOptions(),
		"sourceControlAccess.openBranch":                         railActivityOptions(),
		"sourceControlAccess.openPullRequest":                    railActivityOptions(),
		"sourceControlAccess.getPullRequestStatus":               railActivityOptions(),
		"sourceControlAccess.postReview":                         railActivityOptions(),
		"sourceControlAccess.mergePullRequest":                   railActivityOptions(),
		"sourceControlAccess.syncManagedScaffold":                railActivityOptions(),
		"designSessionAccess.readProjectOnBranch":                readProjectActivityOptions(),
		"designSessionAccess.stageArtifactForReviewOnBranch":     mutateActivityOptions(),
		"designSessionAccess.commitArtifactWithProvenance":       mutateActivityOptions(),
		"designSessionAccess.rejectArtifactOnBranchWithComments": mutateActivityOptions(),
		"designSessionAccess.withdrawArtifactOnBranch":           mutateActivityOptions(),
		"designSessionAccess.setReviewCommentStatusOnBranch":     mutateActivityOptions(),
		"designSessionAccess.seedReviewCommentsOnBranch":         mutateActivityOptions(),
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the three workflow bodies under their registered names, the per-activity option-preset
// hook, and the genActivities threaded from the impl's stored published deps.
//
// The workflows receiver holds the generated invoker surface (Acts) — every
// contract-backed RA op (readProjectVersion / advancePhase / submit / observe / the
// seven rail verbs / the eight designSession verbs) is reached through it; the receiver
// carries no RA dep of its own.
func (m *projectDesignManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()

	wf := &workflows{
		Estimation:   m.estimator,
		OperationEst: m.opEstimator,
		Settlement:   m.settlement,
		Acts:         genInvokers{Opts: optsHook},
		// Rail is the PUBLISHED sourceControlAccess: nil ⇒ the PR rail is dormant and the
		// CoAuthor draft path runs the original main-path behavior. Held directly for the
		// gitEnabled gate; the seven rail verbs (including syncManagedScaffold, since B9) go
		// through the generated invoker surface (wf.Acts.Rail*).
		Rail: m.rail,
		Repo: m.repo,
	}

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindCoAuthor, Fn: wf.CoAuthorPhase2ArtifactWorkflow},
			{Name: executionKindSDPReview, Fn: wf.AssembleSDPReviewWorkflow},
			{Name: executionKindPhaseAdvance, Fn: wf.Phase2AdvanceWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:  m.projectState,
			Pipeline:      m.pipeline,
			Rail:          m.rail,
			DesignSession: m.designSession,
		},
	}
}

// RegisterManagerWorker wires the projectDesignManager onto a Temporal Worker polling the
// project-design task queue (projectDesignManager.md §6.1). It preserves the external call
// shape the composition root used before the generated-layer migration, asserting to the
// concrete *projectDesignManager the generated constructor returns and delegating to the
// generated RegisterWorker with the impl's WorkerManifest. Every Activity this Manager's
// workflows execute is generated, so the generated RegisterWorker registers the complete
// set — no explicit custom-Activity registration remains (B9 follow-up).
func RegisterManagerWorker(w worker.Worker, m ProjectDesignManager) {
	impl, ok := m.(*projectDesignManager)
	if !ok {
		panic("projectdesign: RegisterManagerWorker requires a *projectDesignManager from NewProjectDesignManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}
