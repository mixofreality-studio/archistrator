package construction

import (
	"context"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// gitactivities.go holds the Manager-owned Temporal Activity wrappers for the
// git-forward slice (C-MCN-GIT): the PR rail verbs (sourceControlAccess /
// IPullRequestRail) and the per-activity git head-state Record verbs
// (projectStateAccess §GIT-HEAD-STATE). They are METHODS ON THE Workflows STRUCT,
// like the rest (activities.go). Each runs the port call inside an Activity because
// rail/RA operations are I/O and would break replay determinism on the workflow
// goroutine.
//
// THE TWO LAYER RULES THIS FILE OBEYS:
//   - cred is MINTED by the Manager (MintRepoCredentialActivity → GetInstallationToken,
//     a call DOWN) and threaded INTO every other rail/record verb as a parameter — the
//     RA layer never reads Temporal context and never fetches the credential itself
//     ([[feedback_temporal_manager_layer_only]]; sourceControlAccess §1.1; D-PA-R
//     REWORK.4).
//   - idempotencyKey "${workflowId}:${activityId}" is derived from the Activity
//     context HERE (activityIdempotencyKey, activities.go) — the rail's deterministic
//     names + the git store's dedup-first ledger make a retried Activity a no-op.
//
// CRED OPACITY ACROSS THE RA SEAM: the rail returns a sourcecontrol.RepoCredential;
// the git head-state verbs take a projectstate.RepoCredential. These are
// structurally-identical-but-distinct opaque carriers (the NoSideways layer rule keeps
// projectstate from importing sourcecontrol — projectstate/credential.go). The Manager
// is the one seam allowed to touch both, so it converts (convertCred). The opaque
// rail handles (BranchRef/PullRequestRef.String()) cross the Activity boundary as plain
// strings and are reconstructed with the rail's *FromString constructors.

// ===========================================================================
// PR-rail Activities (IPullRequestRail).
// ===========================================================================

// MintRepoCredentialActivity wraps GetInstallationToken — the Manager mints the
// short-lived credential it threads into every other rail/record verb. Read-shaped
// (no idempotency key); a rejected/expired identity surfaces fwra.Auth (terminal).
func (wf *Workflows) MintRepoCredentialActivity(ctx context.Context, repoRef string) (railCredEnvelope, error) {
	cred, err := wf.Rail.GetInstallationToken(ctx, sourcecontrol.RepoRefFromString(repoRef))
	if err != nil {
		return railCredEnvelope{}, fwmanager.MapError(err)
	}
	return railCredEnvelope{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

// OpenBranchArgs bundles the OpenBranch inputs across the Activity boundary.
type OpenBranchArgs struct {
	RepoRef string
	Branch  string
	Cred    railCredEnvelope
}

// OpenBranchActivity wraps IPullRequestRail.OpenBranch → the opaque BranchRef
// (its String() form). Idempotent on the deterministic branch name.
func (wf *Workflows) OpenBranchActivity(ctx context.Context, a OpenBranchArgs) (string, error) {
	br, err := wf.Rail.OpenBranch(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.BranchName(a.Branch),
		a.Cred.toRail(),
		activityIdempotencyKey(ctx),
	)
	if err != nil {
		return "", fwmanager.MapError(err)
	}
	return br.String(), nil
}

// OpenPullRequestArgs bundles the OpenPullRequest inputs across the Activity
// boundary. The cr-NN change-request label rides in Hints (PullRequestSpec.Hints) —
// not a first-class field (sourcecontrol.go §3).
type OpenPullRequestArgs struct {
	RepoRef string
	Head    string
	Base    string
	Title   string
	Body    string
	Hints   []byte
	Cred    railCredEnvelope
}

// OpenPullRequestActivity wraps IPullRequestRail.OpenPullRequest → the opaque
// PullRequestRef (its String() form). Idempotent on the head branch.
func (wf *Workflows) OpenPullRequestActivity(ctx context.Context, a OpenPullRequestArgs) (string, error) {
	pr, err := wf.Rail.OpenPullRequest(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.PullRequestSpec{
			Head:  sourcecontrol.BranchName(a.Head),
			Base:  sourcecontrol.BranchName(a.Base),
			Title: a.Title,
			Body:  a.Body,
			Hints: a.Hints,
		},
		a.Cred.toRail(),
		activityIdempotencyKey(ctx),
	)
	if err != nil {
		return "", fwmanager.MapError(err)
	}
	return pr.String(), nil
}

// GetPullRequestStatusArgs bundles the status read inputs.
type GetPullRequestStatusArgs struct {
	RepoRef string
	PRRef   string
	Cred    railCredEnvelope
}

// GetPullRequestStatusActivity wraps GetPullRequestStatus → the provider-neutral
// CI rollup the Manager mirrors. Pure read.
func (wf *Workflows) GetPullRequestStatusActivity(ctx context.Context, a GetPullRequestStatusArgs) (PullRequestStatusView, error) {
	st, err := wf.Rail.GetPullRequestStatus(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.PullRequestRefFromString(a.PRRef),
		a.Cred.toRail(),
	)
	if err != nil {
		return PullRequestStatusView{}, fwmanager.MapError(err)
	}
	return PullRequestStatusView{
		CheckRollup:   mapCheckState(st.CheckRollup),
		ApprovalCount: st.ApprovalCount,
		Mergeable:     st.Mergeable,
	}, nil
}

// PostReviewArgs bundles the +1-relay inputs. Verdict is the rail's ReviewVerdict
// (carried as int across the boundary; the spine only relays Approve).
type PostReviewArgs struct {
	RepoRef string
	PRRef   string
	Verdict int
	Body    string
	Cred    railCredEnvelope
}

// PostReviewActivity wraps PostReview — relays the architecture +1 (Approve) to the
// PR. Idempotent on re-post.
func (wf *Workflows) PostReviewActivity(ctx context.Context, a PostReviewArgs) (struct{}, error) {
	err := wf.Rail.PostReview(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.PullRequestRefFromString(a.PRRef),
		sourcecontrol.ReviewSubmission{Verdict: sourcecontrol.ReviewVerdict(a.Verdict), Body: a.Body},
		a.Cred.toRail(),
		activityIdempotencyKey(ctx),
	)
	if err != nil {
		return struct{}{}, fwmanager.MapError(err)
	}
	return struct{}{}, nil
}

// MergePullRequestArgs bundles the gated-merge inputs.
type MergePullRequestArgs struct {
	RepoRef string
	PRRef   string
	Cred    railCredEnvelope
}

// MergePullRequestActivity wraps MergePullRequest → whether the merge to main
// landed (MergeResult.Merged). The Manager PERFORMS the merge; interventionEngine is
// the AUTHORITY for when (the gate decision happens in workflow code before this).
// Idempotent (already-merged maps to Merged=true inside the rail).
func (wf *Workflows) MergePullRequestActivity(ctx context.Context, a MergePullRequestArgs) (bool, error) {
	mr, err := wf.Rail.MergePullRequest(ctx,
		sourcecontrol.RepoRefFromString(a.RepoRef),
		sourcecontrol.PullRequestRefFromString(a.PRRef),
		a.Cred.toRail(),
		activityIdempotencyKey(ctx),
	)
	if err != nil {
		return false, fwmanager.MapError(err)
	}
	return mr.Merged, nil
}

// ===========================================================================
// git head-state Record Activities (projectStateAccess §GIT-HEAD-STATE).
// Each wraps one additive Record verb. A stale-version fwra.Conflict surfaces as the
// canonical Temporal Type() and the workflow-level applyRecovering loop re-reads HEAD
// and re-applies with the SAME idempotency key (no double-record).
// ===========================================================================

// RecordActivityBranchOpenedArgs bundles the branch-opened record (PR-tolerant upsert).
type RecordActivityBranchOpenedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Branch          string
	BranchRef       string
	PRRef           string
	CRLabel         string
	IsRevert        bool
	Cred            railCredEnvelope
}

func (wf *Workflows) RecordActivityBranchOpenedActivity(ctx context.Context, a RecordActivityBranchOpenedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityBranchOpened(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Branch, a.BranchRef, a.PRRef, a.CRLabel, a.IsRevert, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// RecordActivityCIObservedArgs bundles the CI-observed record (the poll-loop verb).
type RecordActivityCIObservedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	CICheck         projectstate.CICheckState
	Cred            railCredEnvelope
}

func (wf *Workflows) RecordActivityCIObservedActivity(ctx context.Context, a RecordActivityCIObservedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityCIObserved(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.CICheck, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// RecordActivityArchApprovedArgs bundles the arch-+1 record.
type RecordActivityArchApprovedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *Workflows) RecordActivityArchApprovedActivity(ctx context.Context, a RecordActivityArchApprovedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityArchApproved(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// RecordActivityMergedArgs bundles the terminal merged record.
type RecordActivityMergedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *Workflows) RecordActivityMergedActivity(ctx context.Context, a RecordActivityMergedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityMerged(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// RecordActivityStartedArgs bundles the per-activity construction-started record
// (Phase → Running). It powers the pump's eligibility gating (Task 3).
type RecordActivityStartedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *Workflows) RecordActivityStartedActivity(ctx context.Context, a RecordActivityStartedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityStarted(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// RecordActivityCompletedArgs bundles the per-activity construction-completed record
// (Phase → Done). It unblocks dependents in the pump's eligibility selection (Task 3).
type RecordActivityCompletedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *Workflows) RecordActivityCompletedActivity(ctx context.Context, a RecordActivityCompletedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityCompleted(ctx, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// ===========================================================================
// Activity-boundary value carriers.
// ===========================================================================

// railCredEnvelope carries the opaque short-lived credential across the Activity
// boundary (and back into the workflow, where it is held for the activity's git
// lifecycle). It is the Manager's OWN transport carrier — it converts to either RA's
// credential type at the call site (the Manager is the seam allowed to touch both).
// The Bytes are write-only at every consumer (never logged); they ride the Temporal
// payload exactly as the rail itself returns them.
type railCredEnvelope struct {
	Bytes     []byte
	ExpiresAt time.Time
}

func (c railCredEnvelope) toRail() sourcecontrol.RepoCredential {
	return sourcecontrol.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

func (c railCredEnvelope) toProjectState() projectstate.RepoCredential {
	return projectstate.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

// PullRequestStatusView is the Manager-local Activity-boundary projection of the
// rail's PullRequestStatus (a reflection the Manager feeds interventionEngine — NOT a
// gate). CheckRollup is the provider-neutral CI rollup the git head-state mirrors.
type PullRequestStatusView struct {
	CheckRollup   projectstate.CICheckState
	ApprovalCount int
	Mergeable     bool
}

// mapCheckState maps the rail's CheckState onto the git head-state's provider-neutral
// CICheckState (the two enums are aligned-by-identity, mapped here so a future re-order
// is safe). A DUMB reflection — it never gates any Approve control.
func mapCheckState(s sourcecontrol.CheckState) projectstate.CICheckState {
	switch s {
	case sourcecontrol.CheckSuccess:
		return projectstate.CICheckSuccess
	case sourcecontrol.CheckFailure:
		return projectstate.CICheckFailure
	default:
		return projectstate.CICheckPending
	}
}
