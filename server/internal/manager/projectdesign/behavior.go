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
	"fmt"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// newSessionRef constructs a SessionRef from an infrastructure identity. Internal to
// the Manager; Clients only ever receive and echo SessionRefs.
func newSessionRef(opaque string) SessionRef { return SessionRef(opaque) }

// toPSKind converts projectdesign's OWN ArtifactKind to the canonical
// projectstate.ArtifactKind (ordinal-preserving) for behavior + RA-boundary calls.
func toPSKind(k ArtifactKind) projectstate.ArtifactKind { return projectstate.ArtifactKind(k) }

// fromPSKind converts a canonical projectstate.ArtifactKind to projectdesign's OWN
// ArtifactKind (ordinal-preserving) at the read boundary.
func fromPSKind(k projectstate.ArtifactKind) ArtifactKind { return ArtifactKind(k) }

// artifactKindString returns the PascalCase Go-identifier name for an ArtifactKind
// (the dispatch-input + PR-title + diagnostic form). Mirrors projectstate String().
func artifactKindString(k ArtifactKind) string { return toPSKind(k).String() }

// artifactKindWireName returns the canonical camelCase wire name for an ArtifactKind.
func artifactKindWireName(k ArtifactKind) string { return toPSKind(k).WireName() }

// artifactKindIsPhase2 reports whether the kind belongs to The Method's Phase 2.
func artifactKindIsPhase2(k ArtifactKind) bool { return toPSKind(k).IsPhase2() }

// phase2RequiredKinds returns the ordered set of Phase-2 artifact kinds (projectdesign's
// OWN type), mirroring projectstate.Phase2RequiredKinds() — the same order the SPA's
// PHASE2_ORDER locks steps by.
func phase2RequiredKinds() []ArtifactKind {
	ps := projectstate.Phase2RequiredKinds()
	out := make([]ArtifactKind, 0, len(ps))
	for _, k := range ps {
		out = append(out, fromPSKind(k))
	}
	return out
}

// phase2PredecessorKind returns the Phase-2 kind that must be Committed immediately
// before `kind` may be drafted — the wire-side mirror of the SPA's Phase-2 buildSpine
// step lock. The first required kind (planningAssumptions) has no predecessor and
// returns (_, false); a kind not in the Phase-2 set likewise returns (_, false).
func phase2PredecessorKind(kind ArtifactKind) (ArtifactKind, bool) {
	req := phase2RequiredKinds()
	for i, k := range req {
		if k == kind {
			if i == 0 {
				return 0, false
			}
			return req[i-1], true
		}
	}
	return 0, false
}

// predecessorNotCommittedMsg is the FailedPrecondition detail naming the uncommitted
// predecessor that blocks the requested draft (by its canonical camelCase wire name).
func predecessorNotCommittedMsg(pred ArtifactKind) string {
	return fmt.Sprintf("predecessor artifact %q must be committed before this kind can be drafted", artifactKindWireName(pred))
}

// strPtrOrNil maps a failure-reason string to the optional contract field: nil for
// the empty string (omitted on the wire), &s otherwise (the project notesPtr pattern).
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
