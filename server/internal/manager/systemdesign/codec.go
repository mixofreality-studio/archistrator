package systemdesign

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// This file owns the Manager's serialization of the sealed projectstate.ArtifactModel
// sum across the Temporal Activity boundary. The Temporal default JSON payload
// converter cannot decode into an interface field (it does not know which
// concrete type to construct), so the typed models the workflow threads —
// returned by ReadProjectActivity / DraftMethodArtifactActivity and carried into
// StageArtifactForReviewActivity / the render Activities — are wrapped in a
// discriminated envelope (Kind + the concrete model's own JSON) at the Activity
// boundary, then reconstructed into the concrete type by Kind. This keeps the
// downstream RA/worker contract shapes (which carry the bare interface) unchanged
// while making the Manager's Temporal payloads round-trip-safe.
//
// The Kind discriminator + per-kind concrete-type construction mirrors the
// scheme projectStateAccess already uses to persist the sum to JSONB — the same
// closed set, owned here for the Temporal seam.

// modelEnvelope is the wire form of one typed model: the STRING kind discriminator
// + the concrete model's own JSON under "model". The public typed contract the SPA
// consumes reads {"kind":"mission","model":{…}}. A nil model encodes as the zero
// envelope (Model empty), which decodes back to a nil model. The Kind field is a
// projectstate.ArtifactKind, which marshals as its camelCase wire name.
type modelEnvelope struct {
	Kind  projectstate.ArtifactKind `json:"kind"`
	Model json.RawMessage           `json:"model,omitempty"`
}

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

// encodeModel wraps a (possibly nil) typed model into its envelope.
func encodeModel(model projectstate.ArtifactModel) (modelEnvelope, error) {
	if model == nil {
		return modelEnvelope{}, nil
	}
	raw, err := json.Marshal(model)
	if err != nil {
		return modelEnvelope{}, fmt.Errorf("encode model %s: %w", model.Kind(), err)
	}
	return modelEnvelope{Kind: model.Kind(), Model: raw}, nil
}

// decodeModel reconstructs the concrete typed model from its envelope. An empty
// Model payload decodes to a nil model.
func (e modelEnvelope) decode() (projectstate.ArtifactModel, error) {
	if len(e.Model) == 0 {
		// Not an error: an empty payload IS the documented "no model yet" state
		// (e.g. a slot that has never been drafted). Every call site checks err
		// first, then uses a nil model as a legitimate value (see activities.go
		// StageArtifactForReviewActivity, codec.go projectEnvelope.decode) — a
		// typed sentinel error would force every caller to unwrap-and-ignore it,
		// which is exactly what returning a plain nil model already achieves.
		return nil, nil //nolint:nilnil // (nil model, nil err) is the documented "no model yet" value; see the block comment above.
	}
	model, ok := projectstate.NewModelForKind(e.Kind)
	if !ok {
		return nil, fmt.Errorf("decode model: no concrete type for kind %s", e.Kind)
	}
	if err := json.Unmarshal(e.Model, model); err != nil {
		return nil, fmt.Errorf("decode model %s: %w", e.Kind, err)
	}
	if sol, isSol := model.(*projectstate.Solution); isSol {
		// The four Solution slots share one concrete type distinguished by SlotKind;
		// the envelope Kind is authoritative. projectstate.NewModelForKind pre-sets SlotKind,
		// but belt-and-suspenders: re-apply it after unmarshal in case the JSON had a
		// stale or differing value.
		sol.SlotKind = e.Kind
	}
	return model, nil
}

// slotEnvelope is the wire form of one Project slot across a Temporal boundary:
// the review status + the model envelope.
type slotEnvelope struct {
	Status projectstate.ArtifactReviewStatus `json:"status"`
	Notes  string                            `json:"notes,omitempty"`
	Model  modelEnvelope                     `json:"model"`
	// CritiqueVerdict / CritiqueNotes carry the first-class PM-critique read-back
	// carrier (D-MSD-Δ amendment) across the ReadProjectActivity Temporal boundary so
	// readBackCritique can consult it. Defaulted-empty (omitempty) — unaffected for any
	// slot a critique never touched.
	CritiqueVerdict string `json:"critiqueVerdict,omitempty"`
	CritiqueNotes   string `json:"critiqueNotes,omitempty"`
	// ReviewThread carries the DURABLE review ledger across the ReadProjectOnBranchActivity
	// Temporal boundary (F48). Without it, loadReviewThread — which reads the session branch
	// through this envelope — silently returned [] even though the reject-with-comments append
	// lives in the branch git, so the redraft prompt lost its writeReviewLedger block, the
	// session-state query showed no comments, and the approve gate did not block. omitempty
	// keeps the payload byte-identical for any slot the ledger never touched.
	ReviewThread []projectstate.ReviewComment `json:"reviewThread,omitempty"`
}

// projectEnvelope is the wire form of the head-state Project across the
// ReadProjectActivity boundary: the identity/version/phase plus every populated
// slot keyed by kind ordinal. Empty slots are omitted.
type projectEnvelope struct {
	ID       projectstate.ProjectID                     `json:"id"`
	Version  projectstate.Version                       `json:"version"`
	Phase    projectstate.Phase                         `json:"phase"`
	Research projectstate.ResearchCorpus                `json:"research,omitempty"`
	Slots    map[projectstate.ArtifactKind]slotEnvelope `json:"slots,omitempty"`
}

// encodeProject wraps the head-state aggregate for the Temporal boundary. The persisted
// research corpus (F42) is now a set of {Title, Path, ContentBytes} POINTERS — the
// book-sized Content lives as files at .aiarch/state/research/, NOT in this envelope — so
// it round-trips whole and stays inherently tiny (the QA F29 titles-only slimming is now
// structural, not a special case). The mission-draft step reads Title + Path off it.
func encodeProject(p projectstate.Project) (projectEnvelope, error) {
	out := projectEnvelope{ID: p.ID, Version: p.Version, Phase: p.Phase, Research: p.Research, Slots: map[projectstate.ArtifactKind]slotEnvelope{}}
	for _, kind := range allSlotKinds() {
		slot := slotFor(p, ArtifactKind(kind))
		if slot.Status == projectstate.ReviewNone && slot.Model == nil {
			continue
		}
		me, err := encodeModel(slot.Model)
		if err != nil {
			return projectEnvelope{}, err
		}
		out.Slots[kind] = slotEnvelope{
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

// decode reconstructs the head-state aggregate from its envelope.
func (e projectEnvelope) decode() (projectstate.Project, error) {
	p := projectstate.Project{ID: e.ID, Version: e.Version, Phase: e.Phase, Research: e.Research}
	for kind, se := range e.Slots {
		model, err := se.Model.decode()
		if err != nil {
			return projectstate.Project{}, err
		}
		if err := setSlot(&p, kind, projectstate.ArtifactSlot{
			Status:          se.Status,
			Model:           model,
			Notes:           se.Notes,
			CritiqueVerdict: se.CritiqueVerdict,
			CritiqueNotes:   se.CritiqueNotes,
			ReviewThread:    se.ReviewThread,
		}); err != nil {
			return projectstate.Project{}, err
		}
	}
	return p, nil
}

// allSlotKinds returns every Project slot kind (Phase 1 + Phase 2) in a stable
// order, for deterministic envelope encoding. Delegates to projectstate.AllArtifactKinds()
// so that adding a new kind to the domain automatically includes it here.
func allSlotKinds() []projectstate.ArtifactKind {
	return projectstate.AllArtifactKinds()
}

// setSlot writes the named slot for kind on p.
func setSlot(p *projectstate.Project, kind projectstate.ArtifactKind, slot projectstate.ArtifactSlot) error {
	switch kind {
	case projectstate.KindMission:
		p.Mission = slot
	case projectstate.KindGlossary:
		p.Glossary = slot
	case projectstate.KindScrubbedRequirements:
		p.ScrubbedRequirements = slot
	case projectstate.KindVolatilities:
		p.Volatilities = slot
	case projectstate.KindCoreUseCases:
		p.CoreUseCases = slot
	case projectstate.KindSystem:
		p.SystemDesign = slot
	case projectstate.KindOperationalConcepts:
		p.OperationalConcepts = slot
	case projectstate.KindStandardCheck:
		p.StandardCheck = slot
	case projectstate.KindPlanningAssumptions:
		p.PlanningAssumptions = slot
	case projectstate.KindActivityList:
		p.ActivityList = slot
	case projectstate.KindNetwork:
		p.Network = slot
	case projectstate.KindNormalSolution:
		p.NormalSolution = slot
	case projectstate.KindSubcriticalSolution:
		p.SubcriticalSolution = slot
	case projectstate.KindCompressedSolution:
		p.CompressedSolution = slot
	case projectstate.KindDecompressedSolution:
		p.DecompressedSolution = slot
	case projectstate.KindRiskModel:
		p.RiskModel = slot
	case projectstate.KindSdpReview:
		p.SdpReview = slot
	default:
		return fmt.Errorf("setSlot: unknown kind ordinal %d", int(kind))
	}
	return nil
}
