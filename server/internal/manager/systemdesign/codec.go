package systemdesign

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// This file used to OWN the Manager's serialization of the sealed
// projectstate.ArtifactModel sum across the Temporal Activity boundary. That wire
// codec (modelEnvelope/slotEnvelope/projectEnvelope + EncodeModel/EncodeProject/Decode)
// is now PROMOTED DOWN into projectstate (envelope.go) — designSessionAccess absorbed
// the branch/ledger/provenance/reconcile capability chains this Manager's custom
// activities (activities_custom.go) used to run over optional ProjectStateAccess
// extensions, and the envelope moved with them so ReadProjectOnBranch can return it
// directly (a concrete, Temporal-serializable projection).
//
// The three type names below are ALIASES to the projectstate types, so every existing
// declaration/field/call site in this package keeps compiling unchanged EXCEPT the
// Decode method call sites: aliasing preserves type identity but not method-name
// casing, and the promoted methods are EXPORTED (Decode, not decode) — those call
// sites were updated in lockstep with this move.
type (
	modelEnvelope   = projectstate.ModelEnvelope
	slotEnvelope    = projectstate.SlotEnvelope
	projectEnvelope = projectstate.ProjectEnvelope
)

// draftModelFor builds the OPAQUE public DraftModel envelope ({kind, model}) the
// session read carries the staged typed draft as. Kind is the artifactKind's canonical
// camelCase wire name (always set, so the SPA gets {"kind":"mission"} even before a
// draft is staged); Model is the concrete model's own JSON, omitted when nil. This is
// the public-surface twin of modelEnvelope (the Temporal/Activity carrier) — the same
// {kind, model} wire shape the SPA decodes, with Kind as a plain string so the
// generated contract carries no projectstate ArtifactKind.
func draftModelFor(kind ArtifactKind, model projectstate.ArtifactModel) (DraftModel, error) {
	env := DraftModel{Kind: artifactKindWireName(kind)}
	if model != nil {
		raw, err := json.Marshal(model)
		if err != nil {
			return DraftModel{}, fmt.Errorf("encode draft model %s: %w", model.Kind(), err)
		}
		rm := json.RawMessage(raw)
		env.Model = &rm
	}
	return env, nil
}

// sameArtifactModel reports whether two typed models are byte-identical in their
// canonical JSON form. Go marshals a given concrete struct deterministically (field
// order is declaration order; map keys are sorted), so this is a stable, replay-safe
// value comparison the workflow goroutine may call directly (no I/O). Used by the
// amendment no-change guard: when an amendment session's branch read-back is identical
// to the committed main model, the draft advanced the branch by nothing, so there is
// no change to review or merge and the session must land at the failed gate rather than
// 422 on an effectively-empty PR.
func sameArtifactModel(a, b projectstate.ArtifactModel) (bool, error) {
	ea, err := encodeModel(a)
	if err != nil {
		return false, err
	}
	eb, err := encodeModel(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ea.Model, eb.Model), nil
}

// encodeModel delegates to the promoted projectstate.EncodeModel. Kept as a
// package-level wrapper (rather than rewriting every call site to the qualified name)
// so this move stays a minimal, mechanical diff.
func encodeModel(model projectstate.ArtifactModel) (modelEnvelope, error) {
	return projectstate.EncodeModel(model)
}

// encodeProject wraps the head-state aggregate for the Temporal boundary, delegating
// the shared slot/model codec to projectstate.EncodeProject and then OPTING IN to
// carrying the Research corpus pointer (projectstate/envelope.go doc: EncodeProject
// leaves Research nil by default — a plain struct field's `omitempty` would not
// suppress the key, so the promoted type uses a pointer and requires an explicit
// opt-in). The persisted corpus (F42) is a set of {Title, Path, ContentBytes}
// POINTERS — the book-sized Content lives as files at .aiarch/state/research/, NOT in
// this envelope — so it round-trips whole and stays inherently tiny (the QA F29
// titles-only slimming is now structural, not a special case). The mission-draft step
// reads Title + Path off it.
func encodeProject(p projectstate.Project) (projectEnvelope, error) {
	env, err := projectstate.EncodeProject(p)
	if err != nil {
		return projectEnvelope{}, err
	}
	env.Research = &p.Research
	return env, nil
}
