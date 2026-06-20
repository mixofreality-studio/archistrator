package projectstate

import "time"

// gitactivitystatus.go holds the per-activity git-forward head-state types
// (projectStateAccess.md §GIT-HEAD-STATE, D-PA-GIT, FROZEN 2026-06-12). It is the
// durable mirror of what IPullRequestRail (sourceControlAccess) returns as the
// branch→PR→CI→+1→merge lifecycle advances per construction-network activity.
//
// PROVIDER-OPACITY is the load-bearing constraint: this RA stores the rail's
// opaque String() handles + a typed CI enum. It names no provider, parses no
// handle, and calls no other RA — the constructionManager (the only both-seam
// toucher) receives the opaque rail returns and threads them into the Record*
// verbs (gitactivity.go), exactly as it threads the resolved typed model into
// StageArtifactForReview. RA-never-calls-RA holds: there is no
// projectStateAccess → sourceControlAccess edge.

// CICheckState is the provider-neutral CI rollup the SPA renders (3 states),
// mirroring sourcecontrol.CheckState + the ux-mock CiStatus. A DUMB reflection of
// the Actions run — it NEVER gates any Approve control. (GIT.1)
type CICheckState int

const (
	// CICheckPending — at least one check still running, none failed (ux-mock 'in_progress').
	CICheckPending CICheckState = iota
	// CICheckSuccess — all checks concluded successfully (ux-mock 'success').
	CICheckSuccess
	// CICheckFailure — at least one check failed (ux-mock 'failed').
	CICheckFailure
)

// String returns the canonical name for the CI rollup state.
func (c CICheckState) String() string {
	switch c {
	case CICheckSuccess:
		return "Success"
	case CICheckFailure:
		return "Failure"
	default:
		return "Pending"
	}
}

// ActivityGitStatus is the per-activity git-forward head-state record (D-PA-GIT).
// One per construction-network activity, keyed by ActivityID — the durable mirror
// of what IPullRequestRail returns. PROVIDER-OPAQUE: every handle is the rail's
// opaque String() form; CICheck mirrors the rail's CheckState; NO provider lexeme
// is stored. (GIT.1)
//
// feedback_agent_friendly_typed_schemas: the key is ActivityID (a stable NAME, not
// a minted UUID); BranchRef/PullRequestRef are resolved by the rail and threaded
// through the Manager (this RA stores, never derives); the SPA's display #N AND
// clickable prUrl are derived/constructed AT READ from the opaque PullRequestRef +
// the per-project repo base the webClient holds (neither is a stored duplicate, and
// the repo-base construction keeps the head-state free of any provider host);
// UpdatedAt is server-resolved. prNumber-as-int and prUrl-as-string are
// deliberately NOT stored (derivable; the rail returns no url — OQ-3 RULED).
type ActivityGitStatus struct {
	// ActivityID is the network activity id (D-CW, C-MST, I-UC1, cr-021-export…) —
	// the map key (NAME-as-identity).
	ActivityID string
	// BranchName is the per-activity branch (Manager-derived; provider-neutral,
	// e.g. "activity/C-MST").
	BranchName string
	// BranchRef is the opaque BranchRef.String() (today a git ref; never parsed).
	BranchRef string
	// PullRequestRef is the opaque PullRequestRef.String() (today a PR number; never
	// parsed). The SPA constructs the clickable prUrl from THIS + the per-project repo
	// base (OQ-3 RULED: no url stored — the rail returns none, and storing a
	// github.com/owner/repo url would leak a provider host). Empty until the PR opens
	// (branch-only first touch).
	PullRequestRef string
	// CICheck is the last-observed CI rollup reflection (mirrors rail CheckState); a
	// DUMB reflection, never a gate.
	CICheck CICheckState
	// ArchApproved is set once the human's architecture +1 was relayed (postReview
	// Approve) — the ArchApprovedTag.
	ArchApproved bool
	// Merged is set once the gated merge to main completed (MergeResult.Merged).
	Merged bool
	// CRLabel is the cr-NN change-request group label, "" when not a CR activity
	// (GitRowMeta crLabel).
	CRLabel string
	// IsRevert marks a PR that carries inverse commits (a revert PR) — op-concepts §15.
	IsRevert bool
	// UpdatedAt is the last Record* touch — SERVER-RESOLVED at commit, never
	// caller-minted.
	UpdatedAt time.Time
}
