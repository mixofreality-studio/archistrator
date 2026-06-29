package systemdesign

// behavior.go holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract value (the canonical-name
// lookups that used to be methods on the projectstate enums, the opaque SessionRef
// constructor) lives here as a free function.
//
// systemdesign's OWN ArtifactKind mirrors projectstate.ArtifactKind ordinal-for-
// ordinal, so its behavior is derived by a meaning-preserving int conversion to the
// canonical projectstate type rather than re-implemented here.

import (
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// newSessionRef constructs a SessionRef from an infrastructure identity. Internal to
// the Manager; Clients only ever receive and echo SessionRefs.
func newSessionRef(opaque string) SessionRef { return SessionRef(opaque) }

// toPSKind converts systemdesign's OWN ArtifactKind to the canonical
// projectstate.ArtifactKind (ordinal-preserving) for behavior + RA-boundary calls.
func toPSKind(k ArtifactKind) projectstate.ArtifactKind { return projectstate.ArtifactKind(k) }

// artifactKindString returns the PascalCase Go-identifier name for an ArtifactKind
// (the dispatch-input + PR-title + diagnostic form). Mirrors projectstate String().
func artifactKindString(k ArtifactKind) string { return toPSKind(k).String() }

// artifactKindWireName returns the canonical camelCase wire name for an ArtifactKind.
func artifactKindWireName(k ArtifactKind) string { return toPSKind(k).WireName() }

// artifactKindIsPhase1 reports whether the kind belongs to The Method's Phase 1.
func artifactKindIsPhase1(k ArtifactKind) bool { return toPSKind(k).IsPhase1() }

// phase1RequiredKinds returns the ordered set of Phase-1 artifact kinds (systemdesign's
// OWN type), mirroring projectstate.Phase1RequiredKinds().
func phase1RequiredKinds() []ArtifactKind {
	ps := projectstate.Phase1RequiredKinds()
	out := make([]ArtifactKind, 0, len(ps))
	for _, k := range ps {
		out = append(out, ArtifactKind(k))
	}
	return out
}

// strPtrOrNil maps a failure-reason string to the optional contract field: nil for
// the empty string (omitted on the wire), &s otherwise (the project notesPtr pattern).
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// researchIsZero reports whether the ResearchInput is unprovided (no Sources). The
// SetResearchInput pre-condition rejects a zero value.
func researchIsZero(r ResearchInput) bool { return len(r.Sources) == 0 }

// toPSResearch converts the contract ResearchInput to projectstate.ResearchInput at
// the projectStateAccess boundary.
func toPSResearch(r ResearchInput) projectstate.ResearchInput {
	sources := make([]projectstate.ResearchSource, 0, len(r.Sources))
	for _, s := range r.Sources {
		sources = append(sources, projectstate.ResearchSource{Title: s.Title, Content: s.Content})
	}
	return projectstate.ResearchInput{Sources: sources}
}
