package projectdesign

import (
	"encoding/json"
	"fmt"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// This file owns the Manager's serialization of the sealed projectstate.ArtifactModel
// sum across the Temporal Activity boundary. The Temporal default JSON payload
// converter cannot decode into an interface field (it does not know which
// concrete type to construct), so the typed models the workflow threads —
// returned by ReadProjectActivity / GenerateTypedDataActivity and carried into
// StageArtifactForReviewActivity — are wrapped in a discriminated envelope
// (Kind + the concrete model's own JSON) at the Activity boundary, then
// reconstructed into the concrete type by Kind. This keeps the downstream
// RA/worker contract shapes (which carry the bare interface) unchanged while
// making the Manager's Temporal payloads round-trip-safe.
//
// Copied verbatim from systemdesign/codec.go: the envelope scheme operates over
// the same projectstate types (which already cover Phase 2) and is phase-agnostic.

// modelEnvelope is the wire form of one typed model: the STRING kind discriminator
// + the concrete model's own JSON under "model" ({"kind":"normalSolution","model":{…}}).
// A nil model encodes as the zero envelope (Model empty), which decodes back to a nil
// model. The Kind field is a projectstate.ArtifactKind, which marshals as its
// camelCase wire name. Byte-identical to systemdesign.modelEnvelope by construction.
type modelEnvelope struct {
	Kind  projectstate.ArtifactKind `json:"kind"`
	Model json.RawMessage           `json:"model,omitempty"`
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

// decode reconstructs the concrete typed model from its envelope. An empty Model
// payload decodes to a nil model.
func (e modelEnvelope) decode() (projectstate.ArtifactModel, error) {
	if len(e.Model) == 0 {
		return nil, nil
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
}

// projectEnvelope is the wire form of the head-state Project across the
// ReadProjectActivity boundary: the identity/version/phase plus every populated
// slot keyed by kind ordinal. Empty slots are omitted.
type projectEnvelope struct {
	ID       projectstate.ProjectID                     `json:"id"`
	Version  projectstate.Version                       `json:"version"`
	Phase    projectstate.Phase                         `json:"phase"`
	Research projectstate.ResearchInput                 `json:"research,omitempty"`
	Slots    map[projectstate.ArtifactKind]slotEnvelope `json:"slots,omitempty"`
}

// encodeProject wraps the head-state aggregate for the Temporal boundary.
func encodeProject(p projectstate.Project) (projectEnvelope, error) {
	out := projectEnvelope{ID: p.ID, Version: p.Version, Phase: p.Phase, Research: p.ResearchInput, Slots: map[projectstate.ArtifactKind]slotEnvelope{}}
	for _, kind := range allSlotKinds() {
		slot := slotFor(p, kind)
		if slot.Status == projectstate.ReviewNone && slot.Model == nil {
			continue
		}
		me, err := encodeModel(slot.Model)
		if err != nil {
			return projectEnvelope{}, err
		}
		out.Slots[kind] = slotEnvelope{Status: slot.Status, Notes: slot.Notes, Model: me}
	}
	return out, nil
}

// decode reconstructs the head-state aggregate from its envelope.
func (e projectEnvelope) decode() (projectstate.Project, error) {
	p := projectstate.Project{ID: e.ID, Version: e.Version, Phase: e.Phase, ResearchInput: e.Research}
	for kind, se := range e.Slots {
		model, err := se.Model.decode()
		if err != nil {
			return projectstate.Project{}, err
		}
		if err := setSlot(&p, kind, projectstate.ArtifactSlot{Status: se.Status, Model: model, Notes: se.Notes}); err != nil {
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
