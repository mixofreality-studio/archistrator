package projectstate

import (
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// provenance.go carries the ADDITIVE commit-provenance record for a committed artifact
// slot (PM-P2-4). It records WHO committed and WHEN, captured at the rail's approve→commit
// transition, so the read model can render "committed <date> · approved by X · drafted by Y"
// under the committed strip.
//
// It follows the staleBasisCause pattern (a19a25b + a9867cf) exactly:
//   - omitempty everywhere: an uncommitted slot carries no provenance, and every field is
//     independently optional so a commit with no acting identity still records committedAt.
//   - NO back-fill: a slot committed BEFORE this field existed reads back with a nil
//     Provenance. Absent provenance is allowed — the read model simply omits the line.
//   - The record is refreshed on every commit (a re-commit / amendment restamps it with the
//     new commit's committedAt + acting identity).
type Provenance struct {
	// CommittedAt is the RFC3339 wall-clock instant the commit landed, server-resolved from
	// the store's clock at commit time (RA code, time.Now() is fine). Always present on a
	// provenance-recorded commit.
	CommittedAt string `json:"committedAt,omitempty"`
	// ApprovedBy is a human-facing label for the acting identity that approved the commit
	// (the reviewer's username / email / subject, derived from the caller's SecurityPrincipal
	// at the manager boundary and threaded down). Empty when no identity reached the commit
	// path (e.g. a dev-mode zero principal) — absence is allowed.
	ApprovedBy string `json:"approvedBy,omitempty"`
	// DraftedBy is a human-facing label for the drafting agent/rail that produced the draft
	// (v1: the agentic design rail identity, plus the amendment-session marker when known).
	// Empty on a substrate that records no rail identity.
	DraftedBy string `json:"draftedBy,omitempty"`
}

// ProvenanceCommitProjectStateAccess is the OPTIONAL, dormant-when-unwired extension a
// substrate implements to record commit provenance ATOMICALLY with the commit (the same
// pattern as BranchAwareProjectStateAccess / LedgerProjectStateAccess). The design-manager
// commit Activity type-asserts it; a substrate WITHOUT it falls back to the plain
// CommitArtifact and simply records no provenance (absent provenance is allowed). Keeping it
// a separate extension leaves the generated ProjectStateAccess port + every existing fake
// untouched.
type ProvenanceCommitProjectStateAccess interface {
	CommitArtifactWithProvenance(rc fwra.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, approvedBy, draftedBy string) (Version, error)
}
