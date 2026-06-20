package systemdesign

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/davidmarne/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// RegisterWorker wires the systemDesignManager onto a Temporal Worker polling the
// system-design task queue (systemDesignManager.md §6.1). The Manager's workflow
// dependencies live on a single Workflows struct (there is no separate Activities
// type).
//
// 2026-06-15 agentic-pivot re-cut (systemDesignManager.md §0d / D-MSD-Δ):
//   - The draft / PM-critique mechanism flips to dispatch → observe → read-back. The
//     retired GenerateTypedDataActivity / GenerateToolTurnActivity (the synchronous
//     workerAccess path) are GONE; the new DispatchDesignJobActivity +
//     ObserveDesignJobActivity (over constructionPipelineAccess) replace them.
//   - workerAccess and artifactValidationEngine are DROPPED from the Manager's deps
//     (no synchronous LLM call survives; validation is the required CI check inside
//     the Action, surfaced via the observed terminal phase).
//   - The PARENT SystemDesignPhaseWorkflow, the per-step CHILD CoAuthorArtifactWorkflow
//     gate, and the standalone PhaseAdvanceWorkflow are unchanged in shape.
//   - The head-state mutation Activities (stage/commit/reject/withdraw/advancePhase)
//     STAY — the human-gate decision writes remain server-side thin-writes (§0d.3).
//   - 2026-06-16 I-DESIGN-DISPATCH §2b: the OPTIONAL PR rail (rail + repo resolver) is
//     threaded in. When both are non-nil the CoAuthor spine wraps each draft in the
//     settled branch→PR→read-back→+1→merge model + the branch-aware read-back/stage; when
//     nil the spine is UNCHANGED (read-back/stage on main, no branch/PR ops). The rail
//     Activities (Mint/OpenBranch/OpenPullRequest/GetPullRequestStatus/PostReview/
//     MergePullRequest) + the branch-aware ReadProjectOnBranch are registered regardless
//     (an unwired rail simply never invokes them).
func RegisterWorker(
	w worker.Worker,
	projectState projectstate.ProjectStateAccess,
	pipeline ConstructionPipelineAccess,
	rail SourceControlRail,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
) {
	wf := &Workflows{
		ProjectState: projectState,
		Pipeline:     pipeline,
		Rail:         rail,
		Repo:         repo,
	}
	w.RegisterWorkflowWithOptions(wf.SystemDesignPhaseWorkflow, workflow.RegisterOptions{Name: ExecutionKindPhase})
	w.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: ExecutionKindCoAuthor})
	w.RegisterWorkflowWithOptions(wf.PhaseAdvanceWorkflow, workflow.RegisterOptions{Name: ExecutionKindPhaseAdvance})

	w.RegisterActivityWithOptions(wf.ReadProjectActivity, activity.RegisterOptions{Name: actReadProject})
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
