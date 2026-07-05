package projectstate

import (
	"encoding/json"
	"fmt"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// slotcodec.go holds the SUBSTRATE-NEUTRAL slot codec + the shared write-path helpers
// the git store (gitstore.go) builds on. It was carved out of the retired Postgres
// store (postgres.go) so the git substrate keeps the one canonical slot encoding: a
// model written by either store round-trips identically.
//
// ArtifactModel is an INTERFACE, so round-tripping it needs a kind-discriminated
// envelope: each populated slot is stored as {status, notes, kind, model} where
// `model` is the concrete typed model's own JSON. On read we switch on the stored kind
// to unmarshal into the right concrete *Xxx and assign it to the named slot.

// slotJSON is the on-infrastructure JSON shape for one populated ArtifactSlot.
// The kind discriminator lets the read codec pick the concrete model type and
// the destination slot. model is the concrete model's own json.Marshal output.
type slotJSON struct {
	Status int             `json:"status"`
	Notes  string          `json:"notes,omitempty"`
	Kind   int             `json:"kind"`
	Model  json.RawMessage `json:"model,omitempty"`
	// CritiqueVerdict / CritiqueNotes are the additive, optional, defaulted-empty
	// PM-critique read-back carrier (D-MSD-Δ amendment, ArtifactSlot doc). omitempty
	// keeps the on-disk shape byte-identical for every slot a critique never touched.
	CritiqueVerdict string `json:"critiqueVerdict,omitempty"`
	CritiqueNotes   string `json:"critiqueNotes,omitempty"`
	// ReviewThread is the DURABLE review ledger for this slot (the review-ledger
	// feature, founder-ratified 2026-07-05): the ordered, server-minted, round-stamped
	// per-comment record that replaces the ephemeral client-only comment model. Unlike
	// the CritiqueVerdict/CritiqueNotes carrier (cleared on every stage / status
	// transition), the thread is DURABLE — it accumulates across redraft rounds and
	// survives Stage/Reject/Withdraw so a reviewer always sees the full comment history
	// (and each comment's response/status). omitempty keeps the on-disk shape
	// byte-identical for every slot the ledger never touched.
	ReviewThread []ReviewComment `json:"reviewThread,omitempty"`
	// Revisions counts how many times this slot has been COMMITTED (F38 amendments,
	// founder ruling 2026-07-05). Bumped on every CommitArtifact; it is the AMENDMENT
	// index a reopening seeds into the new session's branch (…-amend-N). omitempty keeps
	// the on-disk shape byte-identical for slots that were never committed.
	Revisions int64 `json:"revisions,omitempty"`
	// StaleBasis is set true on an already-COMMITTED slot when an UPSTREAM artifact it
	// depends on re-commits (an amendment shifted its basis, F38). It is a NON-blocking
	// UI signal — never an auto-invalidate/auto-redraft — cleared when THIS slot itself
	// re-commits (its own amendment is the reconcile). omitempty keeps the on-disk shape
	// byte-identical for slots that are not stale.
	StaleBasis bool `json:"staleBasis,omitempty"`
}

// slotEntry pairs a named-slot accessor with the kind that selects it. The
// ordered list is the single source of truth for the named-slot ↔ kind mapping
// used by both the read codec and the write-path slot routing.
type slotEntry struct {
	kind ArtifactKind
	ptr  func(p *Project) *ArtifactSlot
}

// slotTable enumerates every named slot on Project paired with its kind.
// Iterated to encode (all populated slots) and indexed by kind to route a write.
func slotTable() []slotEntry {
	return []slotEntry{
		{KindMission, func(p *Project) *ArtifactSlot { return &p.Mission }},
		{KindGlossary, func(p *Project) *ArtifactSlot { return &p.Glossary }},
		{KindScrubbedRequirements, func(p *Project) *ArtifactSlot { return &p.ScrubbedRequirements }},
		{KindVolatilities, func(p *Project) *ArtifactSlot { return &p.Volatilities }},
		{KindCoreUseCases, func(p *Project) *ArtifactSlot { return &p.CoreUseCases }},
		{KindSystem, func(p *Project) *ArtifactSlot { return &p.SystemDesign }},
		{KindOperationalConcepts, func(p *Project) *ArtifactSlot { return &p.OperationalConcepts }},
		{KindStandardCheck, func(p *Project) *ArtifactSlot { return &p.StandardCheck }},
		{KindPlanningAssumptions, func(p *Project) *ArtifactSlot { return &p.PlanningAssumptions }},
		{KindActivityList, func(p *Project) *ArtifactSlot { return &p.ActivityList }},
		{KindNetwork, func(p *Project) *ArtifactSlot { return &p.Network }},
		{KindNormalSolution, func(p *Project) *ArtifactSlot { return &p.NormalSolution }},
		{KindSubcriticalSolution, func(p *Project) *ArtifactSlot { return &p.SubcriticalSolution }},
		{KindCompressedSolution, func(p *Project) *ArtifactSlot { return &p.CompressedSolution }},
		{KindDecompressedSolution, func(p *Project) *ArtifactSlot { return &p.DecompressedSolution }},
		{KindRiskModel, func(p *Project) *ArtifactSlot { return &p.RiskModel }},
		{KindSdpReview, func(p *Project) *ArtifactSlot { return &p.SdpReview }},
	}
}

// slotPtr returns the named-slot accessor for kind, or false if kind names no
// known slot (an unrepresentable case given the closed enum, guarded anyway).
func slotPtr(p *Project, kind ArtifactKind) (*ArtifactSlot, bool) {
	for _, e := range slotTable() {
		if e.kind == kind {
			return e.ptr(p), true
		}
	}
	return nil, false
}

// encodeSlotsMap is the substrate-neutral slot codec: it returns the kind-keyed
// slotJSON map both substrates embed, so a slot serialises identically across either
// store (a model written by one substrate round-trips through the other).
func encodeSlotsMap(p *Project) (map[string]slotJSON, error) {
	out := map[string]slotJSON{}
	for _, e := range slotTable() {
		slot := e.ptr(p)
		if slot.Status == ReviewNone {
			continue
		}
		entry := slotJSON{
			Status:          int(slot.Status),
			Notes:           slot.Notes,
			Kind:            int(e.kind),
			CritiqueVerdict: slot.CritiqueVerdict,
			CritiqueNotes:   slot.CritiqueNotes,
			ReviewThread:    slot.ReviewThread,
			Revisions:       slot.Revisions,
			StaleBasis:      slot.StaleBasis,
		}
		if slot.Model != nil {
			mb, err := json.Marshal(slot.Model)
			if err != nil {
				return nil, fmt.Errorf("encode slot %s model: %w", e.kind, err)
			}
			entry.Model = mb
		}
		out[fmt.Sprintf("%d", int(e.kind))] = entry
	}
	return out, nil
}

// decodeSlotsMap is the substrate-neutral slot decoder (the inverse of encodeSlotsMap).
func decodeSlotsMap(w map[string]slotJSON, p *Project) error {
	for _, entry := range w {
		kind := ArtifactKind(entry.Kind)
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fmt.Errorf("decode slots: unknown kind ordinal %d", entry.Kind)
		}
		slot.Status = ArtifactReviewStatus(entry.Status)
		slot.Notes = entry.Notes
		slot.CritiqueVerdict = entry.CritiqueVerdict
		slot.CritiqueNotes = entry.CritiqueNotes
		slot.ReviewThread = entry.ReviewThread
		slot.Revisions = entry.Revisions
		slot.StaleBasis = entry.StaleBasis
		if len(entry.Model) > 0 {
			model, ok := NewModelForKind(kind)
			if !ok {
				return fmt.Errorf("decode slots: no model type for kind %s", kind)
			}
			if err := json.Unmarshal(entry.Model, model); err != nil {
				return fmt.Errorf("decode slot %s model: %w", kind, err)
			}
			// Restore Solution SlotKind: the four share one concrete type; the
			// destination slot's kind is authoritative.
			if sol, isSol := model.(*Solution); isSol {
				sol.SlotKind = kind
			}
			slot.Model = model
		}
	}
	return nil
}

// statusTransition builds the pure in-memory transition for commit/reject/withdraw:
// a status flip on the slot named by kind, keeping the model already staged there.
// ContractMisuse if the slot is unpopulated (no model was ever staged).
func statusTransition(op string, kind ArtifactKind, to ArtifactReviewStatus, notes string) func(*Project) error {
	return func(p *Project) error {
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.%s: unknown kind %s", op, kind))
		}
		if slot.Status == ReviewNone || slot.Model == nil {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.%s: slot %s is unpopulated (stage a model first)", op, kind))
		}
		slot.Status = to
		slot.Notes = notes
		// Clear the PM-critique read-back carrier on every status transition.
		slot.CritiqueVerdict = ""
		slot.CritiqueNotes = ""
		return nil
	}
}

// commitTransition is the CommitArtifact-specific transition (F38 amendments/staleness). It
// flips the slot to ReviewCommitted (the statusTransition contract) and then, in the SAME
// atomic mutation over the whole Project:
//   - bumps the committed slot's Revisions (the count of commits; the amendment index a
//     reopening seeds into the next session's …-amend-N branch),
//   - CLEARS the committed slot's own StaleBasis (re-committing IS the reconcile), and
//   - sets StaleBasis=true on every ALREADY-committed DOWNSTREAM slot (its basis shifted).
//
// On a FIRST commit no downstream slot is committed yet, so the downstream marking is a
// no-op; only a re-commit (amendment) actually flags anything.
func commitTransition(kind ArtifactKind) func(*Project) error {
	flip := statusTransition("CommitArtifact", kind, ReviewCommitted, "")
	return func(p *Project) error {
		if err := flip(p); err != nil {
			return err
		}
		slot, _ := slotPtr(p, kind)
		slot.Revisions++
		slot.StaleBasis = false
		for _, dk := range downstreamKinds(kind) {
			if ds, ok := slotPtr(p, dk); ok && ds.Status == ReviewCommitted {
				ds.StaleBasis = true
			}
		}
		return nil
	}
}

// mutationMode tunes how a store's applyMutation treats the brand-new-row case.
type mutationMode int

const (
	// modeUpsert is the legacy default: an absent row at expectedVersion 0 is
	// created (the slot verbs are tolerant of being the first write).
	modeUpsert mutationMode = iota
	// modeRequireExisting fails with fwra.NotFound when no row exists. Used by
	// verbs that may only run on an already-created project (SetResearchInput).
	modeRequireExisting
	// modeCreateOnly fails with fwra.Conflict when a row already exists. Used by
	// CreateProject so a project is born exactly once.
	modeCreateOnly
)
