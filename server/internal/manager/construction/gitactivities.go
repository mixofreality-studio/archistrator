package construction

import (
	"context"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// gitactivities.go holds the CUSTOM per-activity git head-state Record Activities the
// generated temporalgen layer has no contract for (projectStateAccess §GIT-HEAD-STATE is
// a plain-goType dep, not a schema-first contract) plus the git-forward value carriers.
// The Record verbs are METHODS ON THE workflows STRUCT (like the rest of the custom
// activities — activities_custom.go); they are registered via the manifest's
// CustomActivities under their stable names (workermanifest.go) and invoked by method
// value from the workflow body (gitforward.go).
//
// The PR-rail verbs (mint / OpenBranch / OpenPullRequest / GetPullRequestStatus /
// PostReview / MergePullRequest) are RETIRED from this file: those ops are GENERATED
// (activities.gen.go) and reached through the generated invoker surface (genInvokers.Rail*);
// the workflow-side value mapping (opaque-handle *FromString/*String marshalling,
// CheckState→CICheckState, cr-label→Hints) lives in gitforward.go.
//
// LAYER RULE THIS FILE OBEYS: idempotencyKey "${workflowId}:${activityId}" is derived from
// the Activity context HERE (activityIdempotencyKey, activities_custom.go) — the git
// store's dedup-first ledger makes a retried Activity a no-op.
//
// CRED OPACITY ACROSS THE RA SEAM: the rail returns a sourcecontrol.RepoCredential; the git
// head-state verbs take a projectstate.RepoCredential. These are
// structurally-identical-but-distinct opaque carriers (the NoSideways layer rule keeps
// projectstate from importing sourcecontrol — projectstate/credential.go). The Manager is
// the one seam allowed to touch both, so it converts (railCredEnvelope.toRail /
// toProjectState).

// ===========================================================================
// git head-state Record Activities (projectStateAccess §GIT-HEAD-STATE).
// Each wraps one additive Record verb. A stale-version fwra.Conflict surfaces as the
// canonical Temporal Type() and the workflow-level applyRecovering loop re-reads HEAD
// and re-applies with the SAME idempotency key (no double-record).
// ===========================================================================

// recordActivityBranchOpenedArgs bundles the branch-opened record (PR-tolerant upsert).
type recordActivityBranchOpenedArgs struct {
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

func (wf *workflows) RecordActivityBranchOpenedActivity(ctx context.Context, a recordActivityBranchOpenedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityBranchOpened(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Branch, a.BranchRef, a.PRRef, a.CRLabel, a.IsRevert, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordActivityCIObservedArgs bundles the CI-observed record (the poll-loop verb).
type recordActivityCIObservedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	CICheck         projectstate.CICheckState
	Cred            railCredEnvelope
}

func (wf *workflows) RecordActivityCIObservedActivity(ctx context.Context, a recordActivityCIObservedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityCIObserved(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.CICheck, a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordActivityArchApprovedArgs bundles the arch-+1 record.
type recordActivityArchApprovedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordActivityArchApprovedActivity(ctx context.Context, a recordActivityArchApprovedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityArchApproved(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordActivityMergedArgs bundles the terminal merged record.
type recordActivityMergedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordActivityMergedActivity(ctx context.Context, a recordActivityMergedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityMerged(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordActivityStartedArgs bundles the per-activity construction-started record
// (Phase → Running). It powers the pump's eligibility gating (Task 3).
type recordActivityStartedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordActivityStartedActivity(ctx context.Context, a recordActivityStartedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityStarted(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID,
		a.Cred.toProjectState(), activityIdempotencyKey(ctx)))
}

// recordActivityCompletedArgs bundles the per-activity construction-completed record
// (Phase → Done). It unblocks dependents in the pump's eligibility selection (Task 3).
type recordActivityCompletedArgs struct {
	ProjectID       projectstate.ProjectID
	ExpectedVersion projectstate.Version
	ActivityID      string
	Cred            railCredEnvelope
}

func (wf *workflows) RecordActivityCompletedActivity(ctx context.Context, a recordActivityCompletedArgs) (projectstate.Version, error) {
	return mapErr(wf.GitStatus.RecordActivityCompleted(fwra.Context{Context: ctx}, a.ProjectID, a.ExpectedVersion, a.ActivityID,
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

// pullRequestStatusView is the Manager-local Activity-boundary projection of the
// rail's PullRequestStatus (a reflection the Manager feeds interventionEngine — NOT a
// gate). CheckRollup is the provider-neutral CI rollup the git head-state mirrors.
type pullRequestStatusView struct {
	CheckRollup   projectstate.CICheckState
	ApprovalCount int
	Mergeable     bool
}

// mapCheckState maps the rail's CheckState onto the git head-state's provider-neutral
// CICheckState (the two enums are aligned-by-identity, mapped here so a future re-order
// is safe). A DUMB reflection — it never gates any Approve control.
func mapCheckState(s sourcecontrol.CheckState) projectstate.CICheckState {
	switch s {
	case sourcecontrol.CheckPending:
		// explicit: pending check state maps directly, same as any unmapped value.
		return projectstate.CICheckPending
	case sourcecontrol.CheckSuccess:
		return projectstate.CICheckSuccess
	case sourcecontrol.CheckFailure:
		return projectstate.CICheckFailure
	default:
		return projectstate.CICheckPending
	}
}
