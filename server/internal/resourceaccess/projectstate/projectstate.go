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

// ProjectStateAccess — the Temporal-free port over the project head-state
// aggregate (projectStateAccess.md §2) — is now GENERATED into contract.gen.go
// from contract.schema.json (schema-first, interface-only mode: the PORT is
// regenerated, but the domain types + persistence codec stay HAND-WRITTEN here,
// the canonical source of truth). Its 8 atomic verbs take rc fwra.Context first
// (carrying ctx + idempotency key). Every write verb honours optimistic
// concurrency (expectedVersion → fwra.Conflict on a stale value) AND idempotency
// (rc.IdempotencyKey deduped in a ledger; a retry collapses to the committed
// version). The verbs record facts; they do NOT re-decide whether a transition is
// allowed (the Manager's gate) nor whether the typed model is semantically valid
// (artifactValidationEngine).

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
