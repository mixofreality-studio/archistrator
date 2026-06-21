package projectstate

// gitconstruction.go is the git-substrate realization of the additive Phase-3
// construction-transition verbs (constructionManager.md §5.3; see construction.go
// for the Postgres-era port + rationale). The git GitStore satisfies the SAME
// ConstructionTransitionAccess facet — re-cut with the Manager-threaded
// `cred RepoCredential` (REWORK.4) the substrate swap forces, exactly as the
// Phase-1/2 verbs are. v1 records the transition through the shared ref-CAS +
// in-repo-dedup applyMutation path so it is durable and replay-idempotent; the
// richer per-activity head-state status aggregate is the same D-PA follow-up the
// Postgres impl deferred.

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// GitConstructionTransitionAccess is the cred-threaded construction-transition
// facet of the git store (the §REWORK.4 re-cut of ConstructionTransitionAccess).
type GitConstructionTransitionAccess interface {
	RecordChangeReviewed(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordActivityExited(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordOperatorPaused(ctx context.Context, projectID ProjectID, expectedVersion Version, reason string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

var _ GitConstructionTransitionAccess = (*GitStore)(nil)

// RecordChangeReviewed records the review transition for activityID (v1: the
// ledger records it; the per-activity status field is the D-PA follow-up).
func (s *GitStore) RecordChangeReviewed(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RecordChangeReviewed", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, func(p *Project) error { return nil })
}

// RecordActivityExited records the binary activity exit for activityID.
func (s *GitStore) RecordActivityExited(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RecordActivityExited", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, func(p *Project) error { return nil })
}

// RecordOperatorPaused records the operator-paused head-state transition.
func (s *GitStore) RecordOperatorPaused(ctx context.Context, projectID ProjectID, expectedVersion Version, reason string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RecordOperatorPaused", projectID, expectedVersion, cred, idempotencyKey, modeUpsert, func(p *Project) error { return nil })
}
