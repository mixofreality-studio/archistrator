package projectdesign

import (
	"fmt"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// RegisterWorker wires the projectDesignManager onto a Temporal Worker polling the
// project-design task queue (projectDesignManager.md §6.1). The Manager's workflow
// dependencies live on a single Workflows struct (there is no separate Activities
// type).
//
// 2026-06-15 agentic-pivot re-cut (projectDesignManager.md §0.5 / D-MPD-Δ):
//   - The Phase-2 plan-DRAFTING mechanism flips to dispatch → observe → read-back.
//     The retired GenerateTypedDataActivity (the synchronous workerAccess path) is
//     GONE; the new DispatchDesignJobActivity + ObserveDesignJobActivity (over
//     constructionPipelineAccess) replace it.
//   - workerAccess and artifactValidationEngine are DROPPED from the Manager's deps
//     (no synchronous LLM call survives; Phase-2 validation is the required CI check
//     inside the Action, surfaced via the observed terminal phase).
//   - The three estimate Engines (estimation + operationestimation + settlement)
//     STAY — they are pure, deterministic, by-value joins called DIRECTLY from
//     workflow code, NOT Activities (§0.5.5 "RETAINED, unchanged").
//   - The CoAuthorPhase2ArtifactWorkflow gate, the AssembleSDPReviewWorkflow (UC2
//     spine with the in-workflow three-Engine join), and the Phase2AdvanceWorkflow
//     seal are unchanged in shape.
//   - The head-state mutation Activities (stage/commit/reject/withdraw/advancePhase)
//     STAY — the human-gate decision writes remain server-side thin-writes (§0.5.3).
//   - 2026-06-16 I-DESIGN-DISPATCH §2b: the OPTIONAL PR rail (rail + repo resolver) is
//     threaded in. When both are non-nil the per-artifact CoAuthorPhase2 draft path wraps
//     each draft in the settled branch→PR→read-back→+1→merge model + the branch-aware
//     read-back/stage; when nil that path is UNCHANGED. The AssembleSDPReviewWorkflow gets
//     NO rail (only the per-artifact draft path does). The rail Activities + the
//     branch-aware ReadProjectOnBranch are registered regardless (an unwired rail never
//     invokes them).
// It takes the constructed ProjectDesignManager and reads the Worker-side deps off
// its (unexported) impl, wrapping the published constructionpipeline / sourcecontrol
// deps in the package's folded adapters before handing them to the Workflows struct.
// The published rail is OPTIONAL: a nil rail leaves the Workflows.Rail seam nil so the
// branch→PR→merge path stays dormant (a dev server with no source-control creds).
func RegisterWorker(w worker.Worker, m ProjectDesignManager) {
	impl, ok := m.(*projectDesignManager)
	if !ok {
		panic(fmt.Sprintf("projectdesign.RegisterWorker: unexpected ProjectDesignManager impl %T", m))
	}
	var rail sourceControlRail
	if impl.rail != nil {
		rail = railAdapterImpl{inner: impl.rail}
	}
	wf := &Workflows{
		Estimation:   impl.estimator,
		OperationEst: impl.opEstimator,
		Settlement:   impl.settlement,
		ProjectState: impl.projectState,
		Pipeline:     pipelineDispatchAdapter{inner: impl.pipeline},
		Rail:         rail,
		Repo:         impl.repo,
	}
	w.RegisterWorkflowWithOptions(wf.CoAuthorPhase2ArtifactWorkflow, workflow.RegisterOptions{Name: ExecutionKindCoAuthor})
	w.RegisterWorkflowWithOptions(wf.AssembleSDPReviewWorkflow, workflow.RegisterOptions{Name: ExecutionKindSDPReview})
	w.RegisterWorkflowWithOptions(wf.Phase2AdvanceWorkflow, workflow.RegisterOptions{Name: ExecutionKindPhaseAdvance})

	w.RegisterActivityWithOptions(wf.ReadProjectActivity, activity.RegisterOptions{Name: actReadProject})
	w.RegisterActivityWithOptions(wf.ReadProjectVersionActivity, activity.RegisterOptions{Name: actReadProjectVersion})
	w.RegisterActivityWithOptions(wf.ReadProjectOnBranchActivity, activity.RegisterOptions{Name: actReadProjectOnBranch})
	w.RegisterActivityWithOptions(wf.DispatchDesignJobActivity, activity.RegisterOptions{Name: actDispatchDesignJob})
	w.RegisterActivityWithOptions(wf.ObserveDesignJobActivity, activity.RegisterOptions{Name: actObserveDesignJob})
	w.RegisterActivityWithOptions(wf.StageArtifactForReviewActivity, activity.RegisterOptions{Name: actStageForReview})
	w.RegisterActivityWithOptions(wf.CommitArtifactActivity, activity.RegisterOptions{Name: actCommitArtifact})
	w.RegisterActivityWithOptions(wf.RejectArtifactActivity, activity.RegisterOptions{Name: actRejectArtifact})
	w.RegisterActivityWithOptions(wf.WithdrawArtifactActivity, activity.RegisterOptions{Name: actWithdrawArtifact})
	w.RegisterActivityWithOptions(wf.AdvancePhaseActivity, activity.RegisterOptions{Name: actAdvancePhase})

	// PR-rail Activities (I-DESIGN-DISPATCH §2b). Registered unconditionally; an unwired
	// rail (rail/repo nil) never invokes them.
	w.RegisterActivityWithOptions(wf.MintRepoCredentialActivity, activity.RegisterOptions{Name: actMintRepoCredential})
	w.RegisterActivityWithOptions(wf.OpenBranchActivity, activity.RegisterOptions{Name: actOpenBranch})
	w.RegisterActivityWithOptions(wf.OpenPullRequestActivity, activity.RegisterOptions{Name: actOpenPullRequest})
	w.RegisterActivityWithOptions(wf.GetPullRequestStatusActivity, activity.RegisterOptions{Name: actGetPullRequestStatus})
	w.RegisterActivityWithOptions(wf.PostReviewActivity, activity.RegisterOptions{Name: actPostReview})
	w.RegisterActivityWithOptions(wf.MergePullRequestActivity, activity.RegisterOptions{Name: actMergePullRequest})
}
