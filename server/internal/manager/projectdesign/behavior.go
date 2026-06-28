package projectdesign

// behavior.go holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract value (the canonical-name
// lookups that used to be methods on the projectstate enums, the opaque SessionRef
// constructor) lives here as a free function.
//
// projectdesign's OWN ArtifactKind mirrors projectstate.ArtifactKind ordinal-for-
// ordinal, so its behavior is derived by a meaning-preserving int conversion to the
// canonical projectstate type rather than re-implemented here. This is the Phase-2
// twin of systemdesign/behavior.go.

import (
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// NewSessionRef constructs a SessionRef from an infrastructure identity. Internal to
// the Manager; Clients only ever receive and echo SessionRefs.
func NewSessionRef(opaque string) SessionRef { return SessionRef(opaque) }

// toPSKind converts projectdesign's OWN ArtifactKind to the canonical
// projectstate.ArtifactKind (ordinal-preserving) for behavior + RA-boundary calls.
func toPSKind(k ArtifactKind) projectstate.ArtifactKind { return projectstate.ArtifactKind(k) }

// fromPSKind converts a canonical projectstate.ArtifactKind to projectdesign's OWN
// ArtifactKind (ordinal-preserving) at the read boundary.
func fromPSKind(k projectstate.ArtifactKind) ArtifactKind { return ArtifactKind(k) }

// ArtifactKindString returns the PascalCase Go-identifier name for an ArtifactKind
// (the dispatch-input + PR-title + diagnostic form). Mirrors projectstate String().
func ArtifactKindString(k ArtifactKind) string { return toPSKind(k).String() }

// ArtifactKindWireName returns the canonical camelCase wire name for an ArtifactKind.
func ArtifactKindWireName(k ArtifactKind) string { return toPSKind(k).WireName() }

// ArtifactKindIsPhase2 reports whether the kind belongs to The Method's Phase 2.
func ArtifactKindIsPhase2(k ArtifactKind) bool { return toPSKind(k).IsPhase2() }

// phase2RequiredKinds returns the ordered set of Phase-2 required artifact kinds
// (projectdesign's OWN type), mirroring projectstate.Phase2RequiredKinds().
func phase2RequiredKinds() []ArtifactKind {
	ps := projectstate.Phase2RequiredKinds()
	out := make([]ArtifactKind, 0, len(ps))
	for _, k := range ps {
		out = append(out, fromPSKind(k))
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
