// Package projectstate is the projectStateAccess component of the aiarch
// server's ResourceAccess layer — the Temporal-free port over the project's
// HEAD-STATE aggregate (projectStateAccess.md). The Project aggregate is a
// single stored row holding current state, mutated in place by atomic business
// verbs under optimistic concurrency + idempotency. There is NO event log, NO
// projection, NO fold: the stored row IS the truth.
//
// Re-cut 2026-05-26 to typed Method models (projectStateAccess.md §0): each
// Phase-1/2 artifact is a NAMED TYPED SLOT on Project holding the canonical
// typed ArtifactModel plus its review status — not an opaque ArtifactRef.
// stageArtifactForReview carries the typed model (routed to its slot by Kind());
// commit/reject/withdraw key by ArtifactKind (the model is already in the slot).
// ArtifactRef is gone.
//
// Per The Method's layer model ([[the-method-layers]]): ResourceAccess
// components import NO Temporal. The typed Method models and the head-state
// aggregate are OWNED HERE — this is the RA that fronts them — and reached by
// every downstream layer (Manager, Engine) as a downward import. The component
// also imports framework-go/resourceaccess for the shared error model and
// IdempotencyKey, both acyclic, layer-internal edges.
//
// The component records facts; it does NOT author business decisions (Non-goal:
// no business-decision logic). The systemDesignManager reads the head-state,
// applies its Phase-1 transition gate, asks artifactValidationEngine to validate
// content, and only then calls the atomic verb here to persist the outcome.
package projectstate

import (
	"context"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ProjectStateAccess is the Temporal-free port over the project head-state
// aggregate (projectStateAccess.md §2). Six atomic operations: five business
// verbs that mutate the head-state in place, and one whole-aggregate read.
//
// Every write verb honours the dual discipline:
//   - Optimistic concurrency: expectedVersion is checked against the row's
//     current version; a stale value surfaces fwra.Conflict (non-retryable at the
//     Activity level — the Manager re-reads, recomputes, re-applies in its
//     workflow loop).
//   - Idempotency: idempotencyKey is recorded in a dedup ledger; a retried verb
//     carrying the same key collapses to a no-op that returns the version the
//     prior attempt committed (the duplicate case is handled internally and
//     surfaces as success — there is deliberately no public duplicate error).
//
// The verbs record facts; they do NOT re-decide whether a transition is allowed
// (the Manager's gate) nor whether the typed model is semantically valid
// (artifactValidationEngine). This RA stores the typed model it is handed.
type ProjectStateAccess interface {
	// CreateProject is the EXPLICIT birth of a project (Task 2.3). It inserts a new
	// head-state row at version 1 owned by owner with the given display name and the
	// project in PhaseSystemDesign. A project is no longer born implicitly on first
	// SetResearchInput — it must be created first. fwra.Conflict if a row already
	// exists for projectID; idempotent on idempotencyKey via the shared dedup ledger
	// (a retry with the same key returns the version the first attempt committed).
	CreateProject(ctx context.Context, projectID ProjectID, owner OwnerScope, name string, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// ListProjects returns the catalog summaries for every project owned by owner,
	// newest-first. Each summary carries identity + display fields and the current-
	// phase progress (committed vs total artifact slots). An owner with no projects
	// yields an empty (non-nil) slice, not an error.
	ListProjects(ctx context.Context, owner OwnerScope) ([]ProjectSummary, error)
	// StageArtifactForReview stores model in the slot its concrete type selects
	// (via model.Kind()) and sets that slot's status to ReviewAwaitingReview. The
	// typed model is the staged content — no ref. model must be non-nil.
	StageArtifactForReview(ctx context.Context, projectID ProjectID, expectedVersion Version, model ArtifactModel, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// CommitArtifact sets the slot named by kind to ReviewCommitted (architect
	// approved). The model already lives in the slot from staging; commit does not
	// re-carry it. The slot must be populated.
	CommitArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// RejectArtifact sets the slot named by kind to ReviewRejected with the
	// architect's notes. The model is retained for the redraft baseline.
	RejectArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// WithdrawArtifact sets the slot named by kind to ReviewWithdrawn with the
	// architect's notes.
	WithdrawArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// AdvancePhase seals the current phase and moves Phase forward. The Manager has
	// already gated on all required slots being committed; this verb records the
	// fact, it does not re-decide it.
	AdvancePhase(ctx context.Context, projectID ProjectID, expectedVersion Version, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// SetResearchInput sets the project's ResearchInput field (replace-in-place;
	// set once, replaceable). It records a Method INPUT, not a co-authored
	// artifact — there is NO review status and NO slot transition. research must be
	// non-zero. systemDesignManager reads it back via ReadProject to seed the
	// mission-draft worker call (➕ 2026-05-29, projectStateAccess.md §2, §3.8).
	// The project row must already exist (created via CreateProject) — this verb no
	// longer creates it implicitly and returns fwra.NotFound if the row is absent
	// (Task 2.3).
	SetResearchInput(ctx context.Context, projectID ProjectID, expectedVersion Version, research ResearchInput, idempotencyKey fwra.IdempotencyKey) (Version, error)
	// ReadProject returns the current head-state aggregate, including every
	// populated typed-model slot. fwra.NotFound if the project has no row yet (the
	// caller branches on absence).
	ReadProject(ctx context.Context, projectID ProjectID) (Project, error)
}

// BranchAwareProjectStateAccess is the OPTIONAL branch-aware extension of the no-cred
// projectStateAccess port the design Managers consume during the AwaitingReview window
// (I-DESIGN-DISPATCH §2a). The agentic design rail commits the draft on a per-session
// branch (the Action's commit), so the Manager READS BACK and STAGES on that branch
// while the human reviews, then reads/writes the default (main) after the PR merges.
//
// It is a SEPARATE interface, not added to ProjectStateAccess, so every existing
// caller, adapter, and test compiles unchanged: a design Manager type-asserts its
// ProjectStateAccess field to this extension and uses the *OnBranch verbs ONLY when
// (a) the substrate supports it AND (b) a non-empty branch is in play. An EMPTY branch
// is defined to behave EXACTLY as the corresponding non-branch verb (the default/main),
// so threading "" is always safe and identical to today.
type BranchAwareProjectStateAccess interface {
	// ReadProjectOnBranch reads the head-state aggregate from branch (a provider-neutral
	// session-branch name). branch=="" reads the default/main exactly as ReadProject.
	ReadProjectOnBranch(ctx context.Context, projectID ProjectID, branch string) (Project, error)
	// StageArtifactForReviewOnBranch lands the AwaitingReview thin-write on branch (the
	// session branch the draft lives on). branch=="" behaves exactly as
	// StageArtifactForReview (the default/main).
	StageArtifactForReviewOnBranch(ctx context.Context, projectID ProjectID, expectedVersion Version, branch string, model ArtifactModel, idempotencyKey fwra.IdempotencyKey) (Version, error)
}

// Error is the shared ResourceAccess error model (framework-go), re-exported as
// an alias so this component's contract reads in its own terms while every RA
// component shares one fixed enum. Construct with fwra.New / fwra.Wrap using the
// shared kinds (fwra.NotFound, fwra.Conflict, fwra.Transient, fwra.Infrastructure,
// fwra.ContractMisuse).
type Error = fwra.Error
