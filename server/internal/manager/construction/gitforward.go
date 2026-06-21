package construction

import (
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// gitforward.go is the WORKFLOW-LEVEL wiring of the git-forward (branch→PR→CI→+1→
// merge) lifecycle into the per-activity construction spine (C-MCN-GIT; D-PA-GIT §5).
// It is the ONLY place that composes the two seams the constructionManager alone
// touches: the PR rail (sourceControlAccess / IPullRequestRail) and the per-activity
// git head-state mirror (projectStateAccess §GIT-HEAD-STATE). The division of labor
// (D-PA-GIT §5):
//
//   - the rail OWNS the git provider interaction (cut branch, open PR, read CI,
//     relay +1, perform merge) and RETURNS opaque handles + a status reflection;
//   - this Manager receives the opaque returns and MIRRORS them onto the head-state
//     via the additive Record* verbs;
//   - projectStateAccess stores the opaque strings + typed CI enum — it never calls
//     the rail (RA-never-calls-RA).
//
// The merge AUTHORITY split is preserved: interventionEngine DECIDES when to merge
// (the existing variance machinery), the Manager PERFORMS it here. The +1 is the
// architect's in-app approval; the existing reviewEngine fan-out is the technical
// review and is unchanged — the git +1 relay is the SEPARATE, audit-worthy human
// architecture sign-off the head-state records.
//
// CRASH-SAFETY / IDEMPOTENCY: every rail call is on a deterministic name (idempotent
// in the rail) and every Record* goes through applyRecovering — the workflow-level
// Conflict re-read→re-apply loop (§6.5) — with the per-Activity idempotency key, so a
// workflow retry re-running any step is a no-op (the rail's deterministic-name
// idempotency + the git store's dedup-first ledger). The cred is minted ONCE per
// activity lifecycle and threaded into every rail + record verb.

// gitForward is the per-activity git-lifecycle state the spine carries across its
// steps. It is workflow-local (rebuilt deterministically on replay) and holds the
// opaque handles the rail returned + the credential the Manager minted. headVersion
// is shared with the non-git transition records (read-your-writes; §6.5) — the caller
// passes a pointer to the spine's headVersion so both record families advance one
// monotonic token.
type gitForward struct {
	enabled   bool
	repoRef   sourcecontrol.RepoRef
	cred      railCredEnvelope
	branch    string
	branchRef string
	prRef     string
	crLabel   string
	isRevert  bool
}

// gitEnabled reports whether the git-forward slice is wired AND a repo resolves for
// this project. When false the spine runs unchanged (the live Postgres-store
// composition that predates the GitStore).
func (wf *Workflows) gitEnabled(projectID ProjectID) (sourcecontrol.RepoRef, bool) {
	if wf.Rail == nil || wf.GitStatus == nil || wf.Repo == nil {
		return sourcecontrol.RepoRef{}, false
	}
	return wf.Repo(projectID)
}

// openActivityBranchAndPR runs the dispatch-time half of the lifecycle: mint the
// credential, OpenBranch + OpenPullRequest on the rail, then RecordActivityBranchOpened
// (the PR-tolerant fused upsert — births the row with branch+PR and CICheck=Pending).
// It returns the populated gitForward and advances *headVersion. A nil/dormant slice
// returns a disabled gitForward and touches nothing.
func (wf *Workflows) openActivityBranchAndPR(
	ctx workflow.Context,
	in ConstructActivityInput,
	preMintedCred railCredEnvelope,
	headVersion *projectstate.Version,
) (gitForward, error) {
	repoRef, ok := wf.gitEnabled(in.ProjectID)
	if !ok {
		return gitForward{enabled: false}, nil
	}

	gf := gitForward{
		enabled:  true,
		repoRef:  repoRef,
		branch:   activityBranchName(in.ActivityID),
		crLabel:  in.Activity.CRLabel,
		isRevert: in.Activity.IsRevert,
	}

	// REUSE the credential minted ONCE at the top of the spine for the started
	// record (Task 3) — one mint per activity git lifecycle, threaded into every
	// rail + record verb. (Empty when no started cred was minted, which only happens
	// if the slice is dormant — and then gitEnabled is false above and we never get
	// here.)
	gf.cred = preMintedCred
	cred := preMintedCred

	// Rail: cut the per-activity branch.
	c := railOpts(ctx)
	var branchRef string
	if err := workflow.ExecuteActivity(c, wf.OpenBranchActivity, OpenBranchArgs{
		RepoRef: repoRef.String(), Branch: gf.branch, Cred: cred,
	}).Get(ctx, &branchRef); err != nil {
		return gitForward{}, err
	}
	gf.branchRef = branchRef

	// Rail: open the PR (base = main; cr-NN label rides in Hints).
	var prRef string
	if err := workflow.ExecuteActivity(c, wf.OpenPullRequestActivity, OpenPullRequestArgs{
		RepoRef: repoRef.String(),
		Head:    gf.branch,
		Base:    mainBranch,
		Title:   prTitle(in.ActivityID),
		Body:    prBody(in.Activity),
		Hints:   crLabelHints(gf.crLabel),
		Cred:    cred,
	}).Get(ctx, &prRef); err != nil {
		return gitForward{}, err
	}
	gf.prRef = prRef

	// Mirror: birth the per-activity git head-state row (PR-tolerant fused upsert).
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		rc := recordOpts(ctx)
		var out projectstate.Version
		e := workflow.ExecuteActivity(rc, wf.RecordActivityBranchOpenedActivity, RecordActivityBranchOpenedArgs{
			ProjectID: in.ProjectID, ExpectedVersion: expected, ActivityID: string(in.ActivityID),
			Branch: gf.branch, BranchRef: gf.branchRef, PRRef: gf.prRef,
			CRLabel: gf.crLabel, IsRevert: gf.isRevert, Cred: cred,
		}).Get(ctx, &out)
		return out, e
	})
	if err != nil {
		return gitForward{}, err
	}
	*headVersion = v
	return gf, nil
}

// observeCIAndRecord reads the PR's CI rollup once and mirrors it onto the head-state
// (the poll-loop verb — D-PA-GIT §5). Called between the spine's durable waits while
// the pipeline runs. Returns the observed reflection so the caller can feed it into the
// variance machinery. A dormant slice is a no-op returning Pending.
func (wf *Workflows) observeCIAndRecord(
	ctx workflow.Context,
	in ConstructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
) (PullRequestStatusView, error) {
	if !gf.enabled {
		return PullRequestStatusView{CheckRollup: projectstate.CICheckPending}, nil
	}

	c := railOpts(ctx)
	var st PullRequestStatusView
	if err := workflow.ExecuteActivity(c, wf.GetPullRequestStatusActivity, GetPullRequestStatusArgs{
		RepoRef: gf.repoRef.String(), PRRef: gf.prRef, Cred: gf.cred,
	}).Get(ctx, &st); err != nil {
		return PullRequestStatusView{}, err
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		rc := recordOpts(ctx)
		var out projectstate.Version
		e := workflow.ExecuteActivity(rc, wf.RecordActivityCIObservedActivity, RecordActivityCIObservedArgs{
			ProjectID: in.ProjectID, ExpectedVersion: expected, ActivityID: string(in.ActivityID),
			CICheck: st.CheckRollup, Cred: gf.cred,
		}).Get(ctx, &out)
		return out, e
	})
	if err != nil {
		return PullRequestStatusView{}, err
	}
	*headVersion = v
	return st, nil
}

// relayArchApprovalAndRecord relays the architecture +1 (PostReview Approve) to the PR
// and records the audit-worthy ArchApproved fact (D-PA-GIT §5). Called once the
// activity's review has passed (the architect's in-app sign-off). A dormant slice is a
// no-op.
func (wf *Workflows) relayArchApprovalAndRecord(
	ctx workflow.Context,
	in ConstructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
) error {
	if !gf.enabled {
		return nil
	}

	c := railOpts(ctx)
	if err := workflow.ExecuteActivity(c, wf.PostReviewActivity, PostReviewArgs{
		RepoRef: gf.repoRef.String(), PRRef: gf.prRef,
		Verdict: int(sourcecontrol.ReviewApprove), Body: archApprovalBody(in.ActivityID),
		Cred: gf.cred,
	}).Get(ctx, nil); err != nil {
		return err
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		rc := recordOpts(ctx)
		var out projectstate.Version
		e := workflow.ExecuteActivity(rc, wf.RecordActivityArchApprovedActivity, RecordActivityArchApprovedArgs{
			ProjectID: in.ProjectID, ExpectedVersion: expected, ActivityID: string(in.ActivityID), Cred: gf.cred,
		}).Get(ctx, &out)
		return out, e
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// mergeAndRecord PERFORMS the gated merge (the interventionEngine gate already
// cleared in workflow code) and, on a Merged result, records the terminal git fact
// (D-PA-GIT §5). A dormant slice is a no-op. A non-Merged result (e.g. not yet
// mergeable) is surfaced as a non-retryable terminal so the spine does NOT record a
// false merge — the activity's variance machinery handles the not-yet-mergeable case.
func (wf *Workflows) mergeAndRecord(
	ctx workflow.Context,
	in ConstructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
) error {
	if !gf.enabled {
		return nil
	}

	c := railOpts(ctx)
	var merged bool
	if err := workflow.ExecuteActivity(c, wf.MergePullRequestActivity, MergePullRequestArgs{
		RepoRef: gf.repoRef.String(), PRRef: gf.prRef, Cred: gf.cred,
	}).Get(ctx, &merged); err != nil {
		return err
	}
	if !merged {
		return temporal.NewNonRetryableApplicationError(
			"gated merge did not complete (PR not mergeable)", "MergeNotCompleted", nil)
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		rc := recordOpts(ctx)
		var out projectstate.Version
		e := workflow.ExecuteActivity(rc, wf.RecordActivityMergedActivity, RecordActivityMergedArgs{
			ProjectID: in.ProjectID, ExpectedVersion: expected, ActivityID: string(in.ActivityID), Cred: gf.cred,
		}).Get(ctx, &out)
		return out, e
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// recordActivityStarted marks the activity Running in the per-activity construction
// head-state at the TOP of the spine (Task 3), BEFORE any dispatch. This is what
// flips the activity out of NotStarted so the pump's eligibility selection
// (nextEligibleActivity over proj.ActivityConstruction) does not re-dispatch it on a
// concurrent/redundant tick. Cred-threaded like the four git head-state records; a
// dormant slice (git unwired) is a no-op (the live Postgres composition has no
// per-activity construction head-state, so the gate degrades to the child-workflow-id
// idempotency the pump already relies on). It mints a credential ONCE for the
// started+completed pair via the supplied gitForward.cred when the branch lifecycle
// has already minted one, else mints its own.
func (wf *Workflows) recordActivityStarted(
	ctx workflow.Context,
	in ConstructActivityInput,
	cred railCredEnvelope,
	headVersion *projectstate.Version,
) error {
	if wf.GitStatus == nil {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		rc := recordOpts(ctx)
		var out projectstate.Version
		e := workflow.ExecuteActivity(rc, wf.RecordActivityStartedActivity, RecordActivityStartedArgs{
			ProjectID: in.ProjectID, ExpectedVersion: expected, ActivityID: string(in.ActivityID), Cred: cred,
		}).Get(ctx, &out)
		return out, e
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// recordActivityCompleted marks the activity Done in the per-activity construction
// head-state at the END of the spine (Task 3), alongside RecordActivityExited. This
// is what unblocks dependents in the pump's eligibility selection (allDepsDone). A
// dormant slice is a no-op.
func (wf *Workflows) recordActivityCompleted(
	ctx workflow.Context,
	in ConstructActivityInput,
	cred railCredEnvelope,
	headVersion *projectstate.Version,
) error {
	if wf.GitStatus == nil {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		rc := recordOpts(ctx)
		var out projectstate.Version
		e := workflow.ExecuteActivity(rc, wf.RecordActivityCompletedActivity, RecordActivityCompletedArgs{
			ProjectID: in.ProjectID, ExpectedVersion: expected, ActivityID: string(in.ActivityID), Cred: cred,
		}).Get(ctx, &out)
		return out, e
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// startedCred resolves the credential the construction started/completed records
// thread, and reports whether those records fire at all. It deliberately gates on the
// CONSTRUCTION-STATUS slice (GitStatus), NOT the full PR-rail slice — the per-activity
// Running/Done head-state is what drives the pump's eligibility cascade and is
// independent of the branch→PR→merge lifecycle:
//
//   - PR-rail wired (gitEnabled — Rail+GitStatus+Repo, the CLOUD GitHub profile): mint
//     the short-lived installation token via the rail; it is reused by the branch/PR
//     lifecycle AND the started/completed records.
//   - GitStatus wired but no PR rail (the LOCAL/dry-run profile — file:// repo, no
//     GitHub): the status records still fire so the cascade advances, threading a ZERO
//     credential. The local git store's gitAuth ignores the credential entirely
//     (GitAuth{Local:true}), so no token is needed; the PR-rail lifecycle stays dormant
//     (gitEnabled is false, so gf.enabled is false on every branch/PR/CI/merge step).
//   - GitStatus unwired (the legacy Postgres-store composition): false — the
//     started/completed records are no-ops and the pump degrades to child-workflow-id
//     idempotency.
//
// Minted/resolved ONCE at the top of the spine and reused for the completed record.
func (wf *Workflows) startedCred(ctx workflow.Context, projectID ProjectID) (railCredEnvelope, bool, error) {
	if wf.GitStatus == nil {
		return railCredEnvelope{}, false, nil
	}
	// CLOUD profile: a PR rail + repo resolve ⇒ mint the real installation token.
	if repoRef, ok := wf.gitEnabled(projectID); ok {
		cred, err := wf.mintCred(ctx, repoRef)
		if err != nil {
			return railCredEnvelope{}, false, err
		}
		return cred, true, nil
	}
	// LOCAL/dry-run profile: status records fire with a zero (ignored) credential.
	return railCredEnvelope{}, true, nil
}

// mintCred runs MintRepoCredentialActivity → the short-lived credential the Manager
// threads into every rail + record verb for this activity's lifecycle.
func (wf *Workflows) mintCred(ctx workflow.Context, repoRef sourcecontrol.RepoRef) (railCredEnvelope, error) {
	c := mintCredOpts(ctx)
	var cred railCredEnvelope
	err := workflow.ExecuteActivity(c, wf.MintRepoCredentialActivity, repoRef.String()).Get(ctx, &cred)
	return cred, err
}
