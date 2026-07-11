package projectstate

import (
	"encoding/json"
	"fmt"
)

// envelope.go is the ONE Manager-Temporal-boundary wire codec for the sealed
// ArtifactModel sum + the head-state Project aggregate, promoted down from the
// near-duplicate codec.go the projectdesign and systemdesign Managers each carried
// (designSessionAccess promotion — the branch/ledger/provenance/reconcile capability
// chains those two Managers' custom activities ran over optional ProjectStateAccess
// extensions move into this package's own IMPL; ReadProjectOnBranch returns
// ProjectEnvelope directly so the Temporal payload stays a concrete projection and the
// Manager layer no longer owns this decode).
//
// Reconciled from projectdesign/codec.go + systemdesign/codec.go: modelEnvelope was
// byte-identical between the two; projectEnvelope/slotEnvelope were near-identical
// with two deliberate systemdesign-only additions, both preserved here:
//
//   - Research (F16 payload-slimming): Phase-2 project design deliberately does NOT
//     carry the research corpus across the Activity boundary — a single source can be
//     a whole 660KB book, and Phase-2 never reads it. projectdesign's OWN codec.go
//     never declared the field at all, and its own test
//     (Test_encodeProject_DropsResearchCorpus) asserts the wire payload never even
//     contains a "research" KEY, not merely an empty one — a plain (non-pointer)
//     struct field's `omitempty` does NOT suppress the key in encoding/json, so the
//     field here is a POINTER: nil unless a caller opts in. EncodeProject leaves it
//     nil; systemdesign's codec.go assigns `env.Research = &p.Research` itself after
//     calling EncodeProject (its mission-draft step legitimately weaves the corpus
//     titles in).
//   - CritiqueVerdict / CritiqueNotes (D-MSD-Δ PM-critique carrier): systemdesign-only
//     feature, but carried HERE unconditionally (unlike Research) because they are
//     small strings, always empty for a Manager that never sets them (projectdesign),
//     and cleared on every status transition regardless (statusTransition,
//     slotcodec.go) — no payload-size or wire-shape hazard in sharing them.
//
// construction/codec.go's projectEnvelope/encodeProject is DELIBERATELY NOT part of
// this move, despite the shared symbol names: it is a structurally different, far
// narrower wire type (no modelEnvelope discriminated-union discipline at all, no Slots
// map — a flat projection of exactly the few concrete fields the construction pump's
// eligibility selection reads across ReadProjectActivity) serving a Manager that never
// consumes the BranchAware / Ledger / ProvenanceCommit / Reconciling capability chains
// this component (designSessionAccess) promotes — confirmed by grep: none of those four
// interfaces are referenced anywhere under internal/manager/construction. It stays
// local to construction, untouched by this change.

// ModelEnvelope is the wire form of one typed ArtifactModel: the STRING kind
// discriminator + the concrete model's own JSON under "model"
// ({"kind":"mission","model":{…}}). A nil model encodes as the zero envelope (Model
// empty), which decodes back to a nil model.
type ModelEnvelope struct {
	Kind  ArtifactKind    `json:"kind"`
	Model json.RawMessage `json:"model,omitempty"`
}

// EncodeModel wraps a (possibly nil) typed model into its envelope.
func EncodeModel(model ArtifactModel) (ModelEnvelope, error) {
	if model == nil {
		return ModelEnvelope{}, nil
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return ModelEnvelope{}, fmt.Errorf("encode model %s: %w", model.Kind(), err)
	}
	return ModelEnvelope{Kind: model.Kind(), Model: raw}, nil
}

// Decode reconstructs the concrete typed model from its envelope.
func (e ModelEnvelope) Decode() (ArtifactModel, error) {
	if len(e.Model) == 0 {
		// Not an error: an empty payload IS the documented "no model yet" state
		// (e.g. a slot that has never been drafted). Every call site checks err
		// first, then uses a nil model as a legitimate value — a typed sentinel
		// error would force every caller to unwrap-and-ignore it, which is exactly
		// what returning a plain nil model already achieves.
		return nil, nil //nolint:nilnil // (nil model, nil err) is the documented "no model yet" value; see the block comment above.
	}
	model, ok := NewModelForKind(e.Kind)
	if !ok {
		return nil, fmt.Errorf("decode model: no concrete type for kind %s", e.Kind)
	}
	if err := json.Unmarshal(e.Model, model); err != nil {
		return nil, fmt.Errorf("decode model %s: %w", e.Kind, err)
	}
	if sol, isSol := model.(*Solution); isSol {
		// The four Solution slots share one concrete type distinguished by SlotKind;
		// the envelope Kind is authoritative. NewModelForKind pre-sets SlotKind, but
		// belt-and-suspenders: re-apply it after unmarshal in case the JSON had a
		// stale or differing value.
		sol.SlotKind = e.Kind
	}
	return model, nil
}

// SlotEnvelope is the wire form of one Project slot across a Temporal boundary: the
// review status + the model envelope.
type SlotEnvelope struct {
	Status ArtifactReviewStatus `json:"status"`
	Notes  string               `json:"notes,omitempty"`
	Model  ModelEnvelope        `json:"model"`
	// CritiqueVerdict / CritiqueNotes carry the first-class PM-critique read-back
	// carrier (D-MSD-Δ amendment) across the boundary — see the package doc above for
	// why these are shared unconditionally while Research is not. omitempty keeps the
	// payload byte-identical for every slot a critique never touched.
	CritiqueVerdict string `json:"critiqueVerdict,omitempty"`
	CritiqueNotes   string `json:"critiqueNotes,omitempty"`
	// ReviewThread carries the DURABLE review ledger across the Temporal boundary
	// (F48): without it, a branch-aware read silently drops the reject-with-comments
	// append even though it lives in the branch git. omitempty keeps the payload
	// byte-identical for every slot the ledger never touched.
	ReviewThread []ReviewComment `json:"reviewThread,omitempty"`
}

// ProjectEnvelope is the wire form of the head-state Project across the
// ReadProjectActivity / DesignSessionAccess.ReadProjectOnBranch boundary: the
// identity/version/phase plus every populated slot keyed by kind ordinal. Empty slots
// are omitted. See the package doc above for why Research is a pointer.
type ProjectEnvelope struct {
	ID      ProjectID `json:"id"`
	Version Version   `json:"version"`
	Phase   Phase     `json:"phase"`
	// Research is nil unless a caller opts in (systemdesign's codec.go does, right
	// after calling EncodeProject); nil ⇒ omitempty drops the "research" key from the
	// wire payload entirely, matching projectdesign's own never-declared-the-field
	// behavior byte-for-byte.
	Research *ResearchCorpus               `json:"research,omitempty"`
	Slots    map[ArtifactKind]SlotEnvelope `json:"slots,omitempty"`
}

// EncodeProject wraps the head-state aggregate for the Temporal boundary, using the
// SAME canonical kind↔field slot table the substrate persistence codec uses
// (slotTable, slotcodec.go) so a Manager's envelope and the on-disk persistence never
// drift apart. Does NOT populate Research (see ProjectEnvelope doc) — a caller that
// needs the corpus in its envelope assigns `env.Research = &p.Research` itself after
// calling this.
func EncodeProject(p Project) (ProjectEnvelope, error) {
	out := ProjectEnvelope{ID: p.ID, Version: p.Version, Phase: p.Phase, Slots: map[ArtifactKind]SlotEnvelope{}}
	for _, e := range slotTable() {
		slot := *e.ptr(&p)
		if slot.Status == ReviewNone && slot.Model == nil {
			continue
		}
		me, err := EncodeModel(slot.Model)
		if err != nil {
			return ProjectEnvelope{}, err
		}
		out.Slots[e.kind] = SlotEnvelope{
			Status:          slot.Status,
			Notes:           slot.Notes,
			Model:           me,
			CritiqueVerdict: slot.CritiqueVerdict,
			CritiqueNotes:   slot.CritiqueNotes,
			ReviewThread:    slot.ReviewThread,
		}
	}
	return out, nil
}

// Decode reconstructs the head-state aggregate from its envelope.
func (e ProjectEnvelope) Decode() (Project, error) {
	p := Project{ID: e.ID, Version: e.Version, Phase: e.Phase}
	if e.Research != nil {
		p.Research = *e.Research
	}
	for kind, se := range e.Slots {
		model, err := se.Model.Decode()
		if err != nil {
			return Project{}, err
		}
		slot, ok := slotPtr(&p, kind)
		if !ok {
			return Project{}, fmt.Errorf("decode project envelope: unknown kind ordinal %d", int(kind))
		}
		*slot = ArtifactSlot{
			Status:          se.Status,
			Model:           model,
			Notes:           se.Notes,
			CritiqueVerdict: se.CritiqueVerdict,
			CritiqueNotes:   se.CritiqueNotes,
			ReviewThread:    se.ReviewThread,
		}
	}
	return p, nil
}
