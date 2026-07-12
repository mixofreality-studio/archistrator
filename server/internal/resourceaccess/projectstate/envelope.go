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
// construction/codec.go's projectEnvelope/encodeProject was DELIBERATELY NOT part of
// the original move (a structurally different, far narrower flat projection of exactly
// the concrete fields the construction pump's eligibility selection reads) — but the
// B8 follow-up FOLDED it in rather than keep a purpose-specific parallel envelope
// forking the codec: ProjectEnvelope now also carries the three Phase-3
// construction-fidelity sections construction's pump needs and the Slots map could not
// express (they are top-level Project fields OUTSIDE slotTable, not ArtifactSlots):
//
//   - ActivityConstruction / ServiceContracts (maps, omitempty): the per-activity
//     construction head-state (NotStarted/Running/Done + phase completions) the pump's
//     eligibility selection walks, and the per-component contract corpus its hydrate
//     step resolves against. nil for every project construction never touched, so the
//     keys are structurally ABSENT from every pd/sd wire payload (byte-identical to the
//     pre-B8 envelope — pinned by TestProjectEnvelope_NoConstructionState_OmitsConstructionKeys).
//   - ReviewPolicy (*ReviewPolicy, omitempty): the committed human-approval-gate
//     configuration the per-activity spine's phase gate snapshots at workflow start. A
//     POINTER for the same reason Research is (a plain struct field's omitempty does
//     NOT suppress the key in encoding/json); nil unless the policy is non-zero.
//
// Unlike Research (caller-opt-in, F16 payload-slimming — a single corpus source can be
// a whole 660KB book), these three are SMALL and EncodeProject populates them
// unconditionally when present: the construction Manager reads through the generated
// designSessionAccess.readProjectOnBranch invoker and has no seam to opt in after the
// fact, and every non-construction project carries none of them anyway.
// construction/codec.go is deleted; its former decode semantics (committed-slot
// restore for Network/ActivityList) are subsumed by the Slots map's own
// status-faithful round-trip.

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

	// ActivityConstruction / ServiceContracts / ReviewPolicy are the Phase-3
	// construction-fidelity sections (B8 follow-up — see the package doc above): the
	// top-level Project fields outside slotTable() that construction's pump reads
	// across the designSessionAccess.readProjectOnBranch boundary. All three are
	// structurally absent from the wire payload (omitempty) for every project
	// construction never touched, keeping the pd/sd payloads byte-identical to the
	// pre-B8 envelope.
	ActivityConstruction map[string]ActivityConstructionStatus `json:"activityConstruction,omitempty"`
	ServiceContracts     map[string]ServiceContract            `json:"serviceContracts,omitempty"`
	ReviewPolicy         *ReviewPolicy                         `json:"reviewPolicy,omitempty"`
}

// EncodeProject wraps the head-state aggregate for the Temporal boundary, using the
// SAME canonical kind↔field slot table the substrate persistence codec uses
// (slotTable, slotcodec.go) so a Manager's envelope and the on-disk persistence never
// drift apart. Does NOT populate Research (see ProjectEnvelope doc) — a caller that
// needs the corpus in its envelope assigns `env.Research = &p.Research` itself after
// calling this.
func EncodeProject(p Project) (ProjectEnvelope, error) {
	out := ProjectEnvelope{ID: p.ID, Version: p.Version, Phase: p.Phase, Slots: map[ArtifactKind]SlotEnvelope{}}
	// Construction-fidelity sections (B8 follow-up): carried unconditionally when
	// present — nil maps / a zero policy stay structurally absent from the wire
	// (omitempty), so non-construction payloads are byte-identical to before.
	out.ActivityConstruction = p.ActivityConstruction
	out.ServiceContracts = p.ServiceContracts
	if len(p.ReviewPolicy.GatedPhasesByType) != 0 {
		rp := p.ReviewPolicy
		out.ReviewPolicy = &rp
	}
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
	// Construction-fidelity sections (B8 follow-up): restored verbatim; absent keys
	// decode to nil/zero, exactly the pre-construction Project state.
	p.ActivityConstruction = e.ActivityConstruction
	p.ServiceContracts = e.ServiceContracts
	if e.ReviewPolicy != nil {
		p.ReviewPolicy = *e.ReviewPolicy
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
