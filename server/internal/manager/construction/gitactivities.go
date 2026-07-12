package construction

import (
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// gitactivities.go held the CUSTOM per-activity git head-state Record Activities
// (branch-open / CI-observed / arch-approved / merged / started / completed). B8
// (custom activities → generated, clean cut) migrated all six onto the GENERATED
// invoker surface (invokers.gen.go: genInvokers.GitStatus*), called directly from
// gitforward.go — the projectStateAccess §GIT-HEAD-STATE facet is now a real generated
// contract (projectstate.GitActivityStatusAccess), not a plain-goType dep temporalgen
// has no op for. This file now holds only the git-forward VALUE CARRIERS (Phase C
// folding candidates, per the task brief): the credential envelope, the PR-status
// projection, and the CI-state mapper.
//
// The PR-rail verbs (mint / OpenBranch / OpenPullRequest / GetPullRequestStatus /
// PostReview / MergePullRequest) are likewise GENERATED (activities.gen.go) and reached
// through the generated invoker surface (genInvokers.Rail*); the workflow-side value
// mapping (opaque-handle *FromString/*String marshalling, CheckState→CICheckState,
// cr-label→Hints) lives in gitforward.go.
//
// CRED OPACITY ACROSS THE RA SEAM: the rail returns a sourcecontrol.RepoCredential; the
// git head-state verbs take a projectstate.RepoCredential. These are
// structurally-identical-but-distinct opaque carriers (the NoSideways layer rule keeps
// projectstate from importing sourcecontrol — projectstate/credential.go). The Manager is
// the one seam allowed to touch both, so it converts (railCredEnvelope.toRail /
// toProjectState).

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
