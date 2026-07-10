package projectstate

import (
	"fmt"
	"strings"
)

// ResearchCorpus is the PERSISTED Phase-1 research corpus on Project (F42 files-not-JSON,
// founder ruling 2026-07-05). Unlike the ResearchInput verb INPUT (which carries the raw
// {Title, Content}), the persisted corpus stores only a POINTER per source — the CONTENT
// lives as a file at .aiarch/state/research/<slug>.txt in the project repo, NOT inside
// project.json. SetResearchInput writes the file + this pointer in ONE atomic commit.
type ResearchCorpus struct {
	Sources []ResearchSourceRef `json:"Sources"`
}

// ResearchSourceRef is one persisted research pointer: the human Title, the repo-relative
// Path of the corpus file (e.g. ".aiarch/state/research/00-founder-brief.txt" — the
// drafting Action reads it straight off the checked-out repo), and ContentBytes (the byte
// size, so the read model can show "N KB loaded" without shipping the corpus). The raw
// content is deliberately ABSENT — it is structurally gone from project.json (F42/F22).
type ResearchSourceRef struct {
	Title        string `json:"Title"`
	Path         string `json:"Path"`
	ContentBytes int64  `json:"ContentBytes"`
}

// IsZero reports whether the persisted corpus is unprovided (no sources).
func (r ResearchCorpus) IsZero() bool { return len(r.Sources) == 0 }

// researchDir is the corpus-file directory, RELATIVE to statePathPrefix (.aiarch/state).
// Files ride the projectstate substrate — one atomic CommitSubtree, the same idempotency
// ledger — so no CommitManagedFiles allowlist applies (F42).
const researchDir = "research"

// researchFileRel returns the corpus file key RELATIVE to statePathPrefix for source
// index i with the given title: "research/<NN>-<slug>.txt". The zero-padded index makes
// it deterministic + collision-free even when two sources share a title/slug.
func researchFileRel(i int, title string) string {
	return fmt.Sprintf("%s/%02d-%s.txt", researchDir, i, researchSlug(title))
}

// researchPath returns the REPO-RELATIVE path stored in a ResearchSourceRef (prefixed with
// statePathPrefix), so the drafting Action can open it directly from the repo root.
func researchPath(i int, title string) string {
	return statePathPrefix + "/" + researchFileRel(i, title)
}

// researchSlug lowercases a title and collapses every run of non-alphanumeric characters to
// a single "-", trimming leading/trailing dashes. An empty/symbol-only title yields "source".
func researchSlug(title string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "source"
	}
	return s
}

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

// Sources is the set of named research documents/sources feeding Phase-1.
// Zero value (no Sources) == not yet provided.

// ResearchSource is one named research document/source feeding Phase-1 system
// design. Title is human-meaningful; Content is the corpus text the mission-draft
// prompt consumes (or a reference resolvable at construction time — refinable).

// IsZero reports whether the ResearchInput is unprovided (no Sources). The
// setResearchInput pre-condition rejects a zero value (projectStateAccess.md §2).
func (r ResearchInput) IsZero() bool { return len(r.Sources) == 0 }
