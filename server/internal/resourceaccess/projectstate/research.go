package projectstate

// ResearchInput is the Phase-1 research corpus the system-design sequence STARTS
// from — the founder brief, competitor analysis, and customer interviews that
// conceptually populate designs/<product>/research/ (projectStateAccess.md §3.8,
// rework-2026-05-29 §2.6).
//
// It is a Method INPUT, deliberately distinguished from the seven co-authored,
// review-gated Method artifacts:
//   - It does NOT implement ArtifactModel (no Kind(), no isArtifactModel()) — it
//     is not part of the closed artifact sum.
//   - It is NOT held in an ArtifactSlot and carries NO ArtifactReviewStatus —
//     there is no AwaitingReview/Committed/Rejected/Withdrawn lifecycle. The
//     architect does not draft it, the PM does not ratify it, the human does not
//     commit it.
//   - It is a plain field on Project (§3.2), set via setResearchInput, read whole
//     via readProject.
//
// The shape is intentionally minimal and design-level — its exact internal layout
// is construction-refinable. The frozen surface is the field + the verb +
// read-whole; not the precise field set.
type ResearchInput struct {
	// Sources is the set of named research documents/sources feeding Phase-1.
	// Zero value (no Sources) == not yet provided.
	Sources []ResearchSource
}

// ResearchSource is one named research document/source feeding Phase-1 system
// design. Title is human-meaningful; Content is the corpus text the mission-draft
// prompt consumes (or a reference resolvable at construction time — refinable).
type ResearchSource struct {
	Title   string
	Content string
}

// IsZero reports whether the ResearchInput is unprovided (no Sources). The
// setResearchInput pre-condition rejects a zero value (projectStateAccess.md §2).
func (r ResearchInput) IsZero() bool { return len(r.Sources) == 0 }
