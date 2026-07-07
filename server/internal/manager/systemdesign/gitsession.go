package systemdesign

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

	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch, 2026-07-06): before ANY design job is
	// dispatched, converge the seated aiarch-design.yml onto the CURRENT template
	// rendering (drift → one refresh commit on the default branch; identical → no-op).
	// The birth seat runs ONCE under a constant idempotency key, so without this a
	// server release that moves the aiarch-state-mcp pin strands every live repo on a
	// binary the new validators reject (the gtdapp F81 incident). A sync failure BLOCKS
	// the dispatch — never run a design job against a scaffold we could not prove
	// current — and is CONTAINED by the caller at the failed gate like every other
	// dispatch-time rail fault.
	//
	// Temporal versioning guard (replay safety; mirrors construction-review-policy-
	// snapshot): this activity was ADDED to beginSession AFTER the CoAuthor workflow
	// first shipped, so a design session already in flight at deploy time has NO history
	// event for it — replaying such a history against unguarded new code fails the
	// workflow task with a non-determinism error (observed live: gtdapp:5 amendment
	// session — queries dead with "Workflow Task in failed state", the Retry signal
	// unprocessable). GetVersion pins pre-feature executions (DefaultVersion) to the OLD
	// command sequence: they skip the sync for their WHOLE run — including post-recovery
	// redrafts, because the version resolved at first replay is cached per execution —
	// while every execution STARTED after this deploy resolves v1 and syncs before each
	// dispatch. A pre-feature session that keeps failing on a stale scaffold heals via
	// Withdraw + a fresh amendment session (a new execution → v1 → sync).
	if workflow.GetVersion(ctx, "managed-scaffold-sync", workflow.DefaultVersion, 1) >= 1 {
		var scaffoldChanged bool
		if serr := wf.execRailActivityWithAuthRetry(ctx, wf.SyncManagedScaffoldActivity, syncScaffoldArgs{
			RepoRef: sourcecontrol.RepoRefString(repoRef), Cred: cred,
		}, &scaffoldChanged); serr != nil {
			return gitSession{}, fmt.Errorf("managed-scaffold sync failed — the seated %s could not be refreshed to this server's current template, so the design job was NOT dispatched (a stale scaffold pins an aiarch-state-mcp binary this server's validators reject); Retry re-runs the sync: %w", designWorkflowFileName, serr)
		}
		if scaffoldChanged {
			workflow.GetLogger(ctx).Info("managed scaffold drifted; refreshed the seated design workflow to the current template before dispatch",
				"file", designWorkflowFileName)
		}
	}

	// OpenBranch through the shared bounded Auth retry: a secondary-rate-limit 403 here no
	// longer kills the session (QA F35 twin). A genuine denial exhausts the budget and the
	// caller (runDraftRoundTrip) CONTAINS the fault at the failed gate.
	var branchRef string
	if err := wf.execRailActivityWithAuthRetry(ctx, wf.OpenBranchActivity, openBranchArgs{
		RepoRef: sourcecontrol.RepoRefString(repoRef), Branch: sessionBranch, Cred: cred,
	}, &branchRef); err != nil {
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
	if err := wf.execRailActivityWithAuthRetry(ctx, wf.OpenPullRequestActivity, openPullRequestArgs{
		RepoRef: sourcecontrol.RepoRefString(gf.repoRef),
		Head:    gf.branch,
		Base:    mainBranch,
		Title:   designPRTitle(kind),
		Body:    designPRBody(kind),
		Cred:    gf.cred,
	}, &prRef); err != nil {
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
func (wf *workflows) mergeOnApprove(ctx workflow.Context, projectID ProjectID, gf *gitSession, kind ArtifactKind) (bool, error) {
	if !gf.enabled {
		return true, nil
	}

	// Merge guard: the required CI check must be green before the App merges (the
	// "blocks merge" trust boundary). A non-green PR is NOT merged — the caller routes
	// to recovery. execRailActivityWithAuthRetry absorbs a transient (rate-limit) 403 within
	// a bounded WORKFLOW-SIDE budget (QA F35) so a single secondary-rate-limit blip no longer
	// kills the approve.
	var st pullRequestStatusView
	if err := wf.execRailActivityWithAuthRetry(ctx, wf.GetPullRequestStatusActivity, getPullRequestStatusArgs{
		RepoRef: sourcecontrol.RepoRefString(gf.repoRef), PRRef: gf.prRef, Cred: gf.cred,
	}, &st); err != nil {
		return false, err
	}
	if !st.CheckGreen {
		return false, nil
	}

	// F80c: the required check is green, but the PR may be MERGEABLE=false — main advanced
	// under the session branch (a staleness ack, a question seed) and their project.json
	// (a server-owned, single-writer-per-slot document) conflicts, so mergeable_state is
	// dirty. Attempting the merge here would fail and, worse, RE-APPROVING would loop
	// forever (the branch stays dirty). Instead RECONCILE the branch server-side — overlay
	// main's other slots onto the branch tip so it differs from main only in the in-flight
	// slot — which pushes a new commit that makes the PR mergeable. That push re-triggers
	// the required CI check, so we cannot merge in THIS pass; return the honest not-merged
	// path carrying an actionable reason (the caller re-awaits, and the next approve — once
	// CI is green again — merges cleanly). If the substrate cannot reconcile, the same
	// honest fallback applies.
	if !st.Mergeable {
		if rerr := wf.reconcileDivergedBranch(ctx, projectID, gf, kind); rerr != nil {
			return false, rerr
		}
		return false, temporal.NewNonRetryableApplicationError(
			"design PR was not mergeable (main advanced under the session branch); the branch was reconciled with main and CI is re-validating — re-approve once it is green",
			"DesignBranchReconciled", nil)
	}

	// Relay the architecture +1 (the counted approval + audit).
	if err := wf.execRailActivityWithAuthRetry(ctx, wf.PostReviewActivity, postReviewArgs{
		RepoRef: sourcecontrol.RepoRefString(gf.repoRef), PRRef: gf.prRef, Body: designArchApprovalBody(kind), Cred: gf.cred,
	}, nil); err != nil {
		return false, err
	}

	// App-mediated merge of sessionBranch → main.
	var merged bool
	if err := wf.execRailActivityWithAuthRetry(ctx, wf.MergePullRequestActivity, mergePullRequestArgs{
		RepoRef: sourcecontrol.RepoRefString(gf.repoRef), PRRef: gf.prRef, Cred: gf.cred,
	}, &merged); err != nil {
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

// reconcileDivergedBranch overlays main's slots (bar the in-flight one) onto the session
// branch tip so a MERGEABLE=false PR becomes mergeable again (F80c). It runs through
// applyRecovering so a stale-version Conflict re-reads the branch version and retries
// within bounded attempts; a substrate that lacks the reconcile extension surfaces the
// non-retryable ReconcileUnsupported the caller contains as an honest re-await. Seeding
// expectedVersion 0 is safe: an existing branch row trips the version guard → Conflict →
// applyRecovering re-reads the real branch version and retries.
func (wf *workflows) reconcileDivergedBranch(ctx workflow.Context, projectID ProjectID, gf *gitSession, kind ArtifactKind) error {
	branch := gf.readBackBranch()
	if branch == "" {
		return nil // dormant rail: no session branch to reconcile
	}
	_, err := wf.applyRecovering(ctx, projectID, branch, 0, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.ReconcileBranchActivity, reconcileBranchArgs{
			ProjectID: projectstate.ProjectID(projectID), ExpectedVersion: expected, Branch: branch, Kind: toPSKind(kind),
		}).Get(ctx, &v)
		return v, e
	})
	return err
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

// execRailActivityWithAuthRetry runs ANY rail Activity with a bounded WORKFLOW-SIDE retry
// on a transient-403-as-Auth fault (QA F35 + its draft-round-trip twin). The platform github
// ClassifyStatus conflates GitHub secondary rate-limit 403s with real permission denials —
// both become a NON-RETRYABLE Auth ApplicationError the Activity RetryPolicy cannot retry —
// so the workflow retries here: up to railAuthRetryMaxAttempts over ~30s (5s → 10s → cap 15s),
// with workflow.Sleep for deterministic backoff. A GENUINE permission denial exhausts the
// budget and the error propagates to the CALLER, which CONTAINS it (openPR/OpenBranch → the
// StageDraftFailed gate; the approve window → back to AwaitingReview for re-approve) — never a
// crash. Transport blips (Transient) are still retried INSIDE the Activity by railOpts.
// Cancellation propagates immediately. This is the ONE shared helper — the approve window and
// the draft round-trip do NOT duplicate the retry loop.
func (wf *workflows) execRailActivityWithAuthRetry(ctx workflow.Context, act interface{}, args interface{}, result interface{}) error {
	backoff := railAuthRetryBaseBackoff
	for attempt := 1; ; attempt++ {
		err := workflow.ExecuteActivity(railOpts(ctx), act, args).Get(ctx, result)
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

// mintCred runs MintRepoCredentialActivity → the short-lived credential threaded into
// every rail verb for this draft attempt's lifecycle.
func (wf *workflows) mintCred(ctx workflow.Context, repoRef sourcecontrol.RepoRef) (railCredEnvelope, error) {
	var cred railCredEnvelope
	err := workflow.ExecuteActivity(mintCredOpts(ctx), wf.MintRepoCredentialActivity, sourcecontrol.RepoRefString(repoRef)).Get(ctx, &cred)
	return cred, err
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
