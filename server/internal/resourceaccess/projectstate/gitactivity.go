package projectstate

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// gitactivity.go holds the ADDITIVE per-activity git-forward Record* verbs
// (projectStateAccess.md §GIT-HEAD-STATE / GIT.2, D-PA-GIT, FROZEN 2026-06-12).
// They JOIN the existing additive construction-transition verbs
// (RecordChangeReviewed/RecordActivityExited/RecordOperatorPaused, gitconstruction.go)
// on the SAME additive facet — NOT the core ProjectStateAccess (Phase-1/2)
// interface, which is unchanged. Each carries the Manager-threaded
// `cred RepoCredential` (REWORK.4), `expectedVersion` + `idempotencyKey`, and
// `activityID`, and routes through the identical applyMutation ref-CAS + dedup-first
// path with modeRequireExisting (OQ-mode RULED: the project row exists by Phase 3 —
// same mode as SetResearchInput; do NOT inherit the v1 no-ops' modeUpsert).
//
// The ONLY change vs the v1 no-op mutators is that the closure now UPSERTS
// p.ActivityGit[activityID] — a PARTIAL map update (mutate one key, leave the rest
// byte-identical), the load-bearing convergence invariant (GIT.4): two records for
// two DIFFERENT activities converge under ref-CAS rather than clobber.
//
// PROVIDER-OPACITY: the Manager threads the rail's opaque String() handles + a
// typed CICheckState in; this RA stores them verbatim. No provider lexeme; no edge
// to sourceControlAccess.

// GitActivityStatusAccess is the additive per-activity git-forward head-state facet
// of the git store (the §GIT-HEAD-STATE verbs). Kept on the same additive surface
// as GitConstructionTransitionAccess so the core ProjectStateAccess port stays
// source-stable; the concrete GitStore satisfies all three facets.
type GitActivityStatusAccess interface {
	// RecordActivityBranchOpened births (or upserts) the per-activity git row at
	// activity dispatch. PR-TOLERANT UPSERT (OQ-2 RULED): a branch-only first touch
	// (empty prRef) converges to branch+PR on a later touch, so a stage between the
	// rail's OpenBranch and OpenPullRequest is acceptable. Sets CICheck = Pending on
	// first birth. NO prURL parameter (the rail returns none — OQ-3).
	RecordActivityBranchOpened(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID, branch, branchRef, prRef, crLabel string, isRevert bool, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)

	// RecordActivityCIObserved records the last-observed CI rollup for activityID
	// (the poll-loop verb — highest churn; its own verb so the poll touches nothing
	// else). A DUMB reflection, never a gate.
	RecordActivityCIObserved(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, ci CICheckState, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)

	// RecordActivityArchApproved records that the human's architecture +1 was relayed
	// (after PostReview(Approve)) — a distinct, audit-worthy human-gated fact.
	RecordActivityArchApproved(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)

	// RecordActivityMerged records the terminal git fact — the interventionEngine-gated
	// merge to main completed (MergeResult.Merged).
	RecordActivityMerged(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

// Compile-time proof the concrete GitStore satisfies the additive git-status facet.
var _ GitActivityStatusAccess = (*GitStore)(nil)

// upsertActivity fetches (or initialises) the per-activity row, applies the supplied
// in-place mutation, server-resolves UpdatedAt, and writes the SINGLE map key back.
// The map is lazily allocated. This is a PARTIAL map-key update (GIT.4): only the
// named key is touched; every other ActivityGit entry is left byte-identical, so two
// records on DIFFERENT activityIds converge under ref-CAS instead of clobbering.
func (s *GitStore) upsertActivity(p *Project, activityID string, mutate func(g *ActivityGitStatus)) {
	if p.ActivityGit == nil {
		p.ActivityGit = map[string]ActivityGitStatus{}
	}
	g := p.ActivityGit[activityID] // zero value on first touch — births the row
	g.ActivityID = activityID
	mutate(&g)
	g.UpdatedAt = s.now()
	p.ActivityGit[activityID] = g
}

func (s *GitStore) RecordActivityBranchOpened(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID, branch, branchRef, prRef, crLabel string, isRevert bool, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityBranchOpened: empty activityID")
	}
	return s.applyMutation(ctx, "RecordActivityBranchOpened", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		first := true
		if p.ActivityGit != nil {
			_, first = p.ActivityGit[activityID]
			first = !first
		}
		s.upsertActivity(p, activityID, func(g *ActivityGitStatus) {
			g.BranchName = branch
			g.BranchRef = branchRef
			// PR-TOLERANT UPSERT: only overwrite the PR-side fields when the caller
			// carries them (the OpenPullRequest touch). A branch-only first touch leaves
			// a transient empty prRef that converges on the second call; never clobber a
			// previously-recorded prRef back to empty.
			if prRef != "" {
				g.PullRequestRef = prRef
			}
			if crLabel != "" {
				g.CRLabel = crLabel
			}
			if isRevert {
				g.IsRevert = true
			}
			if first {
				g.CICheck = CICheckPending
			}
		})
		return nil
	})
}

func (s *GitStore) RecordActivityCIObserved(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, ci CICheckState, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityCIObserved: empty activityID")
	}
	return s.applyMutation(ctx, "RecordActivityCIObserved", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		s.upsertActivity(p, activityID, func(g *ActivityGitStatus) {
			g.CICheck = ci
		})
		return nil
	})
}

func (s *GitStore) RecordActivityArchApproved(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityArchApproved: empty activityID")
	}
	return s.applyMutation(ctx, "RecordActivityArchApproved", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		s.upsertActivity(p, activityID, func(g *ActivityGitStatus) {
			g.ArchApproved = true
		})
		return nil
	})
}

func (s *GitStore) RecordActivityMerged(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RecordActivityMerged: empty activityID")
	}
	return s.applyMutation(ctx, "RecordActivityMerged", projectID, expectedVersion, cred, idempotencyKey, modeRequireExisting, func(p *Project) error {
		s.upsertActivity(p, activityID, func(g *ActivityGitStatus) {
			g.Merged = true
		})
		return nil
	})
}
