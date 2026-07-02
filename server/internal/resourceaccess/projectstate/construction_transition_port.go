package projectstate

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ConstructionTransitionAccess is the Port interface for the Phase-3 construction
// transition verbs (App-C §6 adjudicated: 8 ops ≤ 12 cap, per conformance gate
// lifecycle-2 T3 analysis). Previously only 3 ops were on the port; the gate
// confirmed that the constructionManager consumer needs all 8.
//
// Op count: 10 (≤12 per App-C §6.2c; >12 "avoid", ≥20 "reject").
type ConstructionTransitionAccess interface {
	RecordChangeReviewed(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordActivityExited(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, outcome ActivityOutcome, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordActivityFailed(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, reason FailureReason, detail string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordOperatorPaused(ctx context.Context, projectID ProjectID, expectedVersion Version, reason string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordReviewPolicy(ctx context.Context, projectID ProjectID, expectedVersion Version, policy ReviewPolicy, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordPhaseStarted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, phase ActivityMethodPhase, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordPhaseCompleted(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, phase ActivityMethodPhase, artifactRef string, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordServiceContractProduced(ctx context.Context, projectID ProjectID, expectedVersion Version, component string, contract ServiceContract, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	RecordPhaseArtifactProduced(ctx context.Context, projectID ProjectID, expectedVersion Version, activityID string, mapKey string, payload PhaseArtifactPayload, cred RepoCredential, idempotencyKey fwra.IdempotencyKey) (Version, error)
	ReadProject(ctx context.Context, projectID ProjectID, cred RepoCredential) (Project, error)
}

// Compile-time assertion: GitStore must satisfy the full 8-op port.
var _ ConstructionTransitionAccess = (*GitStore)(nil)
