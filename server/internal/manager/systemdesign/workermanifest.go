package systemdesign

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the systemDesignManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the three workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go). This Manager has ZERO
// custom Temporal Activities (B10: the last ones — the projectEnvelope-codec reads, the
// head-state mutation writes carrying the BranchAware/Ledger/Provenance/Reconciling
// capability type-assertions, the review-ledger branch mutations, and the free-function
// managed-scaffold sync — were deleted when their call sites migrated onto the generated
// designSessionAccess / sourceControlAccess.syncManagedScaffold invokers); every Activity
// is generated and registered by the generated RegisterWorker.
//
// The Engine dependencies are called DIRECTLY in-workflow (deterministic, by value) and
// are NOT Activities; the durable-execution in-workflow primitives (awaitSignal /
// startTimer) are the Manager's own code.

import (
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// activityOptions returns the option-preset hook the generated invokers consult for
// EVERY Activity this Manager executes (projectState / pipeline / rail / designSession —
// B10 completes the migration, so this is now the complete set). A name with no entry
// falls back to the generated default (invokers.gen.go). Keyed by the generated registered
// activity name (<componentKey>.<opName>); the concrete presets reproduce the pre-migration
// per-call-site choices exactly (each designSessionAccess.* entry reproduces the retired
// custom Activity's readProjectOpts/mutateOpts preset byte-for-byte; syncManagedScaffold
// reproduces the retired custom Activity's railOpts preset byte-for-byte).
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
		"designSessionAccess.reconcileBranchFromMain":            mutateActivityOptions(),
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
// The workflows receiver holds the generated invoker surface (Acts) — every contract-
// backed RA op (readProjectVersion / advancePhase / submit / observe / the seven rail
// verbs / the eight designSession verbs) is reached through it — plus the published Rail
// (held directly ONLY for the nil/dormant gitEnabled gate) and Repo. The receiver carries
// no RA dep of its own; every Activity it executes is generated (B10).
func (m *systemDesignManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()

	wf := &workflows{
		Acts: genInvokers{Opts: optsHook},
		// Rail is the PUBLISHED sourceControlAccess: nil ⇒ the PR rail is dormant and the
		// CoAuthor spine runs the original main-path behavior. Held directly ONLY for the
		// gitEnabled gate; every rail verb (including syncManagedScaffold) goes through the
		// generated invoker surface (wf.Acts.Rail*).
		Rail: m.rail,
		Repo: m.repo,
	}

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindPhase, Fn: wf.SystemDesignPhaseWorkflow},
			{Name: executionKindCoAuthor, Fn: wf.CoAuthorArtifactWorkflow},
			{Name: executionKindPhaseAdvance, Fn: wf.PhaseAdvanceWorkflow},
		},
		// Every Activity this Manager's workflows execute is generated, so the generated
		// RegisterWorker registers the complete set — no explicit custom-Activity
		// registration remains (B10).
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:  m.projectState,
			Pipeline:      m.pipeline,
			Rail:          m.rail,
			DesignSession: m.designSession,
		},
	}
}

// RegisterManagerWorker wires the systemDesignManager onto a Temporal Worker polling the
// system-design task queue (systemDesignManager.md §6.1). It preserves the external call
// shape the composition root used before the generated-layer migration, asserting to the
// concrete *systemDesignManager the generated constructor returns and delegating to the
// generated RegisterWorker with the impl's WorkerManifest.
func RegisterManagerWorker(w worker.Worker, m SystemDesignManager) {
	impl, ok := m.(*systemDesignManager)
	if !ok {
		panic("systemdesign: RegisterManagerWorker requires a *systemDesignManager from NewSystemDesignManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}
