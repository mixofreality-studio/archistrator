package systemdesign

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the systemDesignManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the three workflow
// bodies under their registered names, the CUSTOM Activities the generated layer has no
// contract for (the projectEnvelope-codec reads, the head-state mutation writes carrying
// the BranchAware/Ledger/Provenance/Reconciling capability type-assertions, the
// review-ledger branch mutations, and the free-function managed-scaffold sync —
// activities_custom.go / reviewledger.go / gitrail.go), the per-activity option-preset
// hook, and the genActivities dep threading. It also hosts the external
// RegisterManagerWorker entrypoint the composition root calls (cmd/server/main.go).
//
// The Engine dependencies are called DIRECTLY in-workflow (deterministic, by value) and
// are NOT Activities; the durable-execution in-workflow primitives (awaitSignal /
// startTimer) are the Manager's own code.

import (
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// activityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities (projectState / pipeline / rail). A name with no entry
// falls back to the generated default (invokers.gen.go). Keyed by the generated registered
// activity name (<componentKey>.<opName>); the concrete presets reproduce the pre-migration
// per-call-site choices exactly. The CUSTOM Activities (codec reads / mutations / the
// scaffold sync) are NOT on this surface — they apply their presets (readProjectOpts /
// mutateOpts / railOpts) at the workflow call site directly.
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
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the three workflow bodies under their registered names, the CUSTOM Activities (method
// values on the workflows receiver, registered under their stable names), the per-activity
// option-preset hook, and the genActivities threaded from the impl's stored published deps.
//
// The workflows receiver holds the generated invoker surface (Acts) plus the deps the
// custom Activities still reach directly: ProjectState (codec reads / mutations / ledger /
// reconcile) and the published Rail (the free-function SyncManagedScaffold + the gitEnabled
// gate). The migration routes the contract-backed RA ops (readProjectVersion / advancePhase
// / submit / observe / the six rail verbs) through the generated invoker surface.
func (m *systemDesignManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()

	wf := &workflows{
		ProjectState: m.projectState,
		Acts:         genInvokers{Opts: optsHook},
		// Rail is the PUBLISHED sourceControlAccess: nil ⇒ the PR rail is dormant and the
		// CoAuthor spine runs the original main-path behavior. Held directly for the
		// gitEnabled gate + the custom SyncManagedScaffold Activity; the six rail verbs go
		// through the generated invoker surface (wf.Acts.Rail*).
		Rail: m.rail,
		Repo: m.repo,
	}

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindPhase, Fn: wf.SystemDesignPhaseWorkflow},
			{Name: executionKindCoAuthor, Fn: wf.CoAuthorArtifactWorkflow},
			{Name: executionKindPhaseAdvance, Fn: wf.PhaseAdvanceWorkflow},
		},
		CustomActivities: []genRegisteredActivity{
			{Name: actReadProject, Fn: wf.ReadProjectActivity},
			{Name: actReadProjectOnBranch, Fn: wf.ReadProjectOnBranchActivity},
			{Name: actStageForReview, Fn: wf.StageArtifactForReviewActivity},
			{Name: actCommitArtifact, Fn: wf.CommitArtifactActivity},
			{Name: actRejectArtifact, Fn: wf.RejectArtifactActivity},
			{Name: actWithdrawArtifact, Fn: wf.WithdrawArtifactActivity},
			{Name: actReconcileBranch, Fn: wf.ReconcileBranchActivity},
			{Name: actSetReviewCommentStatus, Fn: wf.SetReviewCommentStatusActivity},
			{Name: actSeedReviewComments, Fn: wf.SeedReviewCommentsActivity},
			{Name: actSyncManagedScaffold, Fn: wf.SyncManagedScaffoldActivity},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState: m.projectState,
			Pipeline:     m.pipeline,
			Rail:         m.rail,
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
