package projectstate

import (
	"context"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// This file holds the ADDITIVE Phase-3 construction-transition write verbs the
// constructionManager (C-MCN) drives. Per constructionManager.md §5.3 / §6.4 and
// projectStateAccess.md §5, these are head-state status transitions modelled as
// future-additive slots: they were "design-only" on the frozen D-PA surface and
// are fillable during C-MCN WITHOUT a D-PA contract change. They are kept on a
// SEPARATE additive interface (ConstructionTransitionAccess) so existing
// ProjectStateAccess consumers/fakes (systemdesign) are unaffected — the concrete
// Store satisfies both.
//
// Each verb is idempotent on a caller-supplied idempotencyKey and guarded by
// optimistic concurrency on expectedVersion, exactly like the Phase-1/2 verbs —
// it goes through the same `applyMutation` helper + idempotency ledger
// (projectStateAccess.md §6). The construction-activity head-state status model
// itself (per-activity Reviewed/Exited/Paused records) is the larger D-PA
// follow-up; v1 records the transition into the idempotency ledger so the
// transition is durable and replay-idempotent, leaving the richer per-activity
// status aggregate to that follow-up.

// ActivityOutcome is the closed terminal outcome recorded for a construction
// activity's binary exit (constructionManager.md §2.1 / §6.3 step 8).
type ActivityOutcome int

const (
	// ActivityOutcomeUnknown is the zero value.
	ActivityOutcomeUnknown ActivityOutcome = iota
	// ActivityOutcomeCompleted is a normal, reviewed binary exit.
	ActivityOutcomeCompleted
	// ActivityOutcomeSkipped is an operator-skip exit (overrideActivity Skip).
	ActivityOutcomeSkipped
	// ActivityOutcomeTakenOver is an exit after an operator/automatic takeover.
	ActivityOutcomeTakenOver
)

// String returns the canonical name for the outcome.
func (o ActivityOutcome) String() string {
	switch o {
	case ActivityOutcomeCompleted:
		return "Completed"
	case ActivityOutcomeSkipped:
		return "Skipped"
	case ActivityOutcomeTakenOver:
		return "TakenOver"
	default:
		return "Unknown"
	}
}

// ConstructionTransitionAccess is the ADDITIVE Phase-3 head-state transition port
// (constructionManager.md §5.3). Kept separate from ProjectStateAccess so the
// Phase-1/2 consumers stay source-stable; the concrete Store satisfies both.
type ConstructionTransitionAccess interface {
	// RecordChangeReviewed records that a produced change for activityID has been
	// routed through review and recorded reviewed (constructionManager.md §6.3
	// step 6). Optimistic on expectedVersion; idempotent on idempotencyKey.
	RecordChangeReviewed(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, idempotencyKey fwra.IdempotencyKey) (Version, error)

	// RecordActivityExited records the binary activity exit for activityID with the
	// given terminal outcome (constructionManager.md §6.3 step 8).
	RecordActivityExited(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, idempotencyKey fwra.IdempotencyKey) (Version, error)

	// RecordOperatorPaused records the operator-paused head-state transition for the
	// project (constructionManager.md §6.3 PauseProjectBranch; NCUC2 658).
	RecordOperatorPaused(ctx context.Context, projectID ProjectID, expectedVersion Version, reason string, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

// RecordChangeReviewed — additive verb. v1 records the transition through the
// shared optimistic-concurrency + idempotency-ledger applyMutation helper (the
// head-state aggregate carries no per-activity status field yet — D-PA follow-up),
// so the transition is durable and replay-idempotent on the caller's key.
func (s *Store) RecordChangeReviewed(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RecordChangeReviewed", projectID, expectedVersion, idempotencyKey, func(p *Project) error {
		return nil // head-state status-aggregate fill is the D-PA follow-up; the ledger records the transition
	})
}

// RecordActivityExited — additive verb (the binary activity exit).
func (s *Store) RecordActivityExited(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RecordActivityExited", projectID, expectedVersion, idempotencyKey, func(p *Project) error {
		return nil
	})
}

// RecordOperatorPaused — additive verb (operator-paused transition).
func (s *Store) RecordOperatorPaused(ctx context.Context, projectID ProjectID, expectedVersion Version, reason string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RecordOperatorPaused", projectID, expectedVersion, idempotencyKey, func(p *Project) error {
		return nil
	})
}

// compile-time conformance: the Store impl satisfies the additive port.
var _ ConstructionTransitionAccess = (*Store)(nil)
