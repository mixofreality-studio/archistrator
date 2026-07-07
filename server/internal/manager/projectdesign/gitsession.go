package projectdesign

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// gitsession.go is the WORKFLOW-LEVEL wiring of the settled branch→PR→read-back→+1→merge
// design model (I-DESIGN-DISPATCH §2b) into the CoAuthorArtifactWorkflow spine. It
// MIRRORS the construction Manager's gitforward.go: the rail OWNS the git provider
// interaction (ensure branch, open PR, read CI rollup, relay +1, perform merge) and
// RETURNS opaque handles; the Manager threads a once-minted credential into every verb;
// the branch-aware read-back/stage (§2a) rides over the session branch while the human
// reviews, then commit/advance land on main AFTER the merge.
//
// DORMANT-WHEN-UNWIRED: every helper checks gf.enabled. When the rail/repo is not wired
// the session is disabled and each helper is a no-op that leaves the spine on the
// original main-path behavior (the read-back branch is "" ⇒ main).

// gitSession is the per-draft-attempt git-lifecycle state the spine carries. It is
// workflow-local (rebuilt deterministically on replay) and holds the opaque handles the
// rail returned + the once-minted credential. branch is the session branch the Action
// drafts/commits + opens its PR on; readBackBranch returns "" (main) when disabled so
// the branch-aware read-back/stage collapse to the original behavior.
type gitSession struct {
	enabled bool
	repoRef sourcecontrol.RepoRef
	cred    railCredEnvelope
	branch  string
	prRef   string
}

// readBackBranch is the branch the read-back + AwaitingReview-stage ride over. The
// session branch while a draft is staged for review (so the human sees the not-yet-
// merged draft); "" (main) when the rail is dormant (the original behavior).
func (gf gitSession) readBackBranch() string {
	if gf.enabled {
		return gf.branch
	}
	return ""
}

// dispatchRepo is the opaque per-project RepoRef the agentic design job dispatches to
// (per-project-design-dispatch): the user's per-project repo where aiarch-design.yml
// was committed at project birth. "" when the rail is dormant ⇒ the RA falls back to
// the configured construction repo (the non-git / Postgres path is unchanged).
func (gf gitSession) dispatchRepo() string {
	if gf.enabled {
		return sourcecontrol.RepoRefString(gf.repoRef)
	}
	return ""
}

// gitEnabled reports whether the PR rail is wired AND a repo resolves for this project.
// When false the spine runs unchanged (read-back/stage on main, no branch/PR ops).
func (wf *workflows) gitEnabled(projectID ProjectID) (sourcecontrol.RepoRef, bool) {
	if wf.Rail == nil || wf.Repo == nil {
		return sourcecontrol.RepoRef(""), false
	}
	return wf.Repo(projectID)
}

// beginSession runs the dispatch-time half of the rail lifecycle for one draft attempt:
// mint the credential, then OpenBranch(sessionBranch) (ensure the branch exists before
// the Action drafts on it). A dormant slice returns a disabled session and touches
// nothing. The session branch is per-attempt (designBranch threads the attempt suffix).
func (wf *workflows) beginSession(ctx workflow.Context, projectID ProjectID, sessionBranch string) (gitSession, error) {
	repoRef, ok := wf.gitEnabled(projectID)
	if !ok {
		return gitSession{enabled: false}, nil
	}
	gf := gitSession{enabled: true, repoRef: repoRef, branch: sessionBranch}

	cred, err := wf.mintCred(ctx, repoRef)
	if err != nil {
		return gitSession{}, err
	}
	gf.cred = cred

	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch, 2026-07-06; mirrors systemdesign): before
	// ANY design job is dispatched, converge the seated aiarch-design.yml onto the CURRENT
	// template rendering (drift → one refresh commit on the default branch; identical →
	// no-op). A sync failure BLOCKS the dispatch — never run a design job against a
	// scaffold we could not prove current — and is CONTAINED by the caller at the failed
	// gate like every other dispatch-time rail fault.
	//
	// Temporal versioning guard (replay safety; mirrors construction-review-policy-
	// snapshot and the systemdesign twin): this activity was ADDED to beginSession AFTER
	// the CoAuthor workflow first shipped, so a Phase-2 design session already in flight
	// at deploy time has NO history event for it — replaying such a history against
	// unguarded new code fails the workflow task with a non-determinism error. GetVersion
	// pins pre-feature executions (DefaultVersion) to the OLD command sequence: they skip
	// the sync for their WHOLE run — including post-recovery redrafts, because the
	// version resolved at first replay is cached per execution — while every execution
	// STARTED after this deploy resolves v1 and syncs before each dispatch. A pre-feature
	// session that keeps failing on a stale scaffold heals via Withdraw + a fresh
	// session (a new execution → v1 → sync).
	if workflow.GetVersion(ctx, "managed-scaffold-sync", workflow.DefaultVersion, 1) >= 1 {
		var scaffoldChanged bool
		// SyncManagedScaffold stays a CUSTOM Activity (free-function composition helper), so
		// it is still invoked via ExecuteActivity by method value, wrapped in the shared
		// bounded Auth retry with its railOpts preset applied at this call site.
		if serr := wf.railWithAuthRetry(ctx, func() error {
			return workflow.ExecuteActivity(railOpts(ctx), wf.SyncManagedScaffoldActivity, syncScaffoldArgs{
				RepoRef: sourcecontrol.RepoRefString(repoRef), Cred: cred,
			}).Get(ctx, &scaffoldChanged)
		}); serr != nil {
			return gitSession{}, fmt.Errorf("managed-scaffold sync failed — the seated %s could not be refreshed to this server's current template, so the design job was NOT dispatched (a stale scaffold pins an aiarch-state-mcp binary this server's validators reject); Retry re-runs the sync: %w", designWorkflowFileName, serr)
		}
		if scaffoldChanged {
			workflow.GetLogger(ctx).Info("managed scaffold drifted; refreshed the seated design workflow to the current template before dispatch",
				"file", designWorkflowFileName)
		}
	}

	// OpenBranch through the shared bounded Auth retry: a secondary-rate-limit 403 here no
	// longer kills the session (QA F35 twin). A genuine denial exhausts the budget and the
	// caller (coAuthorDraftRound) CONTAINS the fault at the failed gate. The opened BranchRef
	// is not retained (the deterministic session-branch name is the addressing key).
	if err := wf.railWithAuthRetry(ctx, func() error {
		_, e := wf.Acts.RailOpenBranch(ctx, repoRef, sourcecontrol.BranchName(sessionBranch), cred.toRail())
		return e
	}); err != nil {
		return gitSession{}, err
	}
	return gf, nil
}

// openPR opens the PR (head=sessionBranch, base=main) AFTER the draft observe succeeds.
// Idempotent on head — if the Action already opened a PR the rail returns the existing
// handle (the server's open is the authoritative handle for the merge step). A dormant
// session is a no-op.
func (wf *workflows) openPR(ctx workflow.Context, gf *gitSession, kind ArtifactKind) error {
	if !gf.enabled {
		return nil
	}
	// OpenPullRequest through the shared bounded Auth retry (QA F35 twin): openPR runs in the
	// draft round-trip AFTER a 20+ minute draft, so a single secondary-rate-limit 403 must not
	// discard that work. A genuine permission denial exhausts the budget and the caller CONTAINS
	// the fault at the failed gate (the committed draft is preserved; Retry resumes).
	var prRef string
	if err := wf.railWithAuthRetry(ctx, func() error {
		pr, e := wf.Acts.RailOpenPullRequest(ctx, gf.repoRef, sourcecontrol.PullRequestSpec{
			Head:  sourcecontrol.BranchName(gf.branch),
			Base:  sourcecontrol.BranchName(mainBranch),
			Title: designPRTitle(kind),
			Body:  designPRBody(kind),
		}, gf.cred.toRail())
		if e != nil {
			return e
		}
		prRef = sourcecontrol.PullRequestRefString(pr)
		return nil
	}); err != nil {
		return err
	}
	gf.prRef = prRef
	return nil
}

// mergeOnApprove runs the approve-time half of the rail lifecycle: the merge GUARD
// (GetPullRequestStatus — CheckRollup must be green), the architecture +1 relay
// (PostReview Approve), and the App-mediated merge (MergePullRequest sessionBranch →
// main). It returns ok=true only when the merge landed; ok=false means the merge guard
// was not green (the caller routes that to the StageDraftFailed recovery gate — the PR
// is not green, do NOT merge, never wedge). A dormant session returns ok=true (the
// non-git spine commits on main with no rail).
func (wf *workflows) mergeOnApprove(ctx workflow.Context, gf *gitSession, kind ArtifactKind) (bool, error) {
	if !gf.enabled {
		return true, nil
	}

	// Merge guard: the required CI check must be green before the App merges (the
	// "blocks merge" trust boundary). A non-green PR is NOT merged — the caller routes
	// to recovery. execRailActivityWithAuthRetry absorbs a transient (rate-limit) 403 within
	// a bounded WORKFLOW-SIDE budget (QA F35) so a single secondary-rate-limit blip no longer
	// kills the approve.
	var st pullRequestStatusView
	if err := wf.railWithAuthRetry(ctx, func() error {
		prStatus, e := wf.Acts.RailGetPullRequestStatus(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef), gf.cred.toRail())
		if e != nil {
			return e
		}
		st = pullRequestStatusView{
			CheckGreen:    prStatus.CheckRollup == sourcecontrol.CheckSuccess,
			ApprovalCount: int(prStatus.ApprovalCount),
			Mergeable:     prStatus.Mergeable,
		}
		return nil
	}); err != nil {
		return false, err
	}
	if !st.CheckGreen {
		return false, nil
	}

	// Relay the architecture +1 (the counted approval + audit). The ReviewApprove verdict is
	// supplied here at the workflow call site (the generated PostReview invoker is verdict-
	// neutral — design only ever approves).
	if err := wf.railWithAuthRetry(ctx, func() error {
		return wf.Acts.RailPostReview(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef),
			sourcecontrol.ReviewSubmission{Verdict: sourcecontrol.ReviewApprove, Body: designArchApprovalBody(kind)}, gf.cred.toRail())
	}); err != nil {
		return false, err
	}

	// App-mediated merge of sessionBranch → main.
	var merged bool
	if err := wf.railWithAuthRetry(ctx, func() error {
		mr, e := wf.Acts.RailMergePullRequest(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef), gf.cred.toRail())
		if e != nil {
			return e
		}
		merged = mr.Merged
		return nil
	}); err != nil {
		return false, err
	}
	if !merged {
		// The guard was green but the merge did not complete (a race / not-mergeable):
		// surface as terminal so the spine does not commit a false merge.
		return false, temporal.NewNonRetryableApplicationError(
			"design PR merge did not complete (not mergeable)", "DesignMergeNotCompleted", nil)
	}
	return true, nil
}

// railAuthRetry* bound the workflow-side rail retry on a transient-403-as-Auth fault
// (QA F35 + its draft-round-trip twin). Shared by BOTH halves of the rail lifecycle:
// the dispatch-time half (OpenBranch / OpenPullRequest) and the approve-time half
// (GetPullRequestStatus / PostReview / MergePullRequest).
const (
	railAuthRetryMaxAttempts = 3
	railAuthRetryBaseBackoff = 5 * time.Second
	railAuthRetryMaxBackoff  = 15 * time.Second
)

// railWithAuthRetry runs ANY rail call (a closure over a generated invoker or the custom
// SyncManagedScaffold Activity) with a bounded WORKFLOW-SIDE retry on a transient-403-as-Auth
// fault (QA F35 + its draft-round-trip twin). The platform github ClassifyStatus conflates
// GitHub secondary rate-limit 403s with real permission denials — both become a NON-RETRYABLE
// Auth ApplicationError the Activity RetryPolicy cannot retry — so the workflow retries here:
// up to railAuthRetryMaxAttempts over ~30s (5s → 10s → cap 15s), with workflow.Sleep for
// deterministic backoff. A GENUINE permission denial exhausts the budget and the error
// propagates to the CALLER, which CONTAINS it (openPR/OpenBranch → the StageDraftFailed gate;
// the approve window → back to AwaitingReview for re-approve) — never a crash. Transport blips
// (Transient) are still retried INSIDE the Activity by railActivityOptions. Cancellation
// propagates immediately. This is the ONE shared helper — the approve window and the draft
// round-trip do NOT duplicate the retry loop.
func (wf *workflows) railWithAuthRetry(ctx workflow.Context, call func() error) error {
	backoff := railAuthRetryBaseBackoff
	for attempt := 1; ; attempt++ {
		err := call()
		if err == nil {
			return nil
		}
		if temporal.IsCanceledError(err) || !isRailAuthFault(err) || attempt >= railAuthRetryMaxAttempts {
			return err
		}
		workflow.GetLogger(ctx).Warn("rail 403 (auth/rate-limit); bounded workflow-side retry", "attempt", attempt)
		_ = workflow.Sleep(ctx, backoff)
		if backoff *= 2; backoff > railAuthRetryMaxBackoff {
			backoff = railAuthRetryMaxBackoff
		}
	}
}

// mintCred runs the generated sourceControlAccess.getInstallationToken invoker → the
// short-lived credential threaded into every rail verb for this draft attempt's lifecycle.
func (wf *workflows) mintCred(ctx workflow.Context, repoRef sourcecontrol.RepoRef) (railCredEnvelope, error) {
	cred, err := wf.Acts.RailGetInstallationToken(ctx, repoRef)
	if err != nil {
		return railCredEnvelope{}, err
	}
	return railCredEnvelope{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

// readProjectOnBranch reads the head-state on an OPTIONAL branch override (§2a). When
// branch=="" or the ProjectState substrate does not support the branch-aware extension,
// it falls back to the original main-path ReadProject — so the branch-aware read-back is
// purely additive and the default path is unchanged. The read runs in an Activity
// (I/O), reusing the ReadProjectActivity for branch=="" and ReadProjectOnBranchActivity
// otherwise.
func (wf *workflows) readProjectOnBranch(ctx workflow.Context, projectID ProjectID, branch string) (projectstate.Project, error) {
	if branch == "" {
		return wf.readProject(ctx, projectID)
	}
	c := readProjectOpts(ctx)
	var pe projectEnvelope
	if err := workflow.ExecuteActivity(c, wf.ReadProjectOnBranchActivity, readProjectOnBranchArgs{
		ProjectID: projectstate.ProjectID(projectID), Branch: branch,
	}).Get(ctx, &pe); err != nil {
		return projectstate.Project{}, err
	}
	return pe.decode()
}
