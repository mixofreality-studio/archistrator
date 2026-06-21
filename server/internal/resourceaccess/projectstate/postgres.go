package projectstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	fwpg "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// Store is the concrete, Postgres-backed implementation of ProjectStateAccess
// (projectStateAccess.md §6 infrastructure mapping). The project's head-state lives
// in ONE row of project_state, mutated in place by the atomic verbs under
// optimistic concurrency. There is no event log and no projection: the row IS
// the truth. A second table, applied_mutation, is the idempotency dedup ledger
// (keys only — NOT an audit trail).
//
// The struct imports NO Temporal (layer rule, projectStateAccess.md §2): the
// idempotency key arrives as an ordinary parameter and is never read from
// ambient context.
type Store struct {
	pool *pgxpool.Pool
}

// Compile-time proof the concrete Store satisfies the port. If the port ever
// drifts, this line breaks the build — exactly the guard The Method wants
// between a contract and its construction.
var _ ProjectStateAccess = (*Store)(nil)

// NewStore builds a Store over an existing pgx pool and applies the schema (DDL)
// deterministically via an embedded, idempotent migration. Applying the
// migration in the constructor keeps schema setup co-located with the only
// component allowed to touch the infrastructure and makes the Store self-sufficient
// for both production wiring and the integration tests.
func NewStore(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, "projectstate.NewStore: nil pool")
	}
	if _, err := pool.Exec(ctx, schemaDDL); err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "projectstate.NewStore: apply schema")
	}
	return &Store{pool: pool}, nil
}

// schemaDDL is the deterministic, idempotent migration for the head-state
// aggregate and the idempotency dedup ledger.
//
//   - project_state: one row per project. version is the optimistic-concurrency
//     token (bumped by every verb). slots is the per-kind typed-model review state,
//     held as a JSONB object {kindOrdinal: {status, notes, model}} —
//     infrastructure-opaque to callers. The typed Method models ARE stored here
//     (this re-cut's centerpiece), not refs into another store.
//   - applied_mutation: the dedup ledger. PRIMARY KEY(project_id, idempotency_key)
//     enforces activity-retry idempotency; result_version records the version the
//     first attempt committed so a replay returns it verbatim.
//
// project_id is a uuid column. NOTE (C-PM-Δ 2026-06-15): ProjectID is now a string
// newtype (name-as-identity), NOT uuid.UUID — so at RUNTIME this `uuid` column only
// round-trips uuid-FORMAT identity strings. The Postgres store is the DEV FALLBACK
// substrate (the live UC1/UC2 path is the git store); re-typing this column to text
// is owned by the pg-retirement activity, OUT of the name-as-identity scope.
const schemaDDL = `
CREATE TABLE IF NOT EXISTS project_state (
    project_id  uuid        NOT NULL PRIMARY KEY,
    version     bigint      NOT NULL,
    phase       int         NOT NULL DEFAULT 0,
    owner       text        NOT NULL DEFAULT '',
    name        text        NOT NULL DEFAULT '',
    slots       jsonb       NOT NULL DEFAULT '{}'::jsonb,
    research    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- research column added ➕ 2026-05-29 (ResearchInput Method input); additive,
-- idempotent for existing deployments that predate the column.
ALTER TABLE project_state ADD COLUMN IF NOT EXISTS research jsonb NOT NULL DEFAULT '{}'::jsonb;

-- owner/name columns added (Task 2.3): explicit project creation + the landing
-- catalog. Additive + idempotent; existing dev rows backfill to '' (no prod data).
ALTER TABLE project_state ADD COLUMN IF NOT EXISTS owner text NOT NULL DEFAULT '';
ALTER TABLE project_state ADD COLUMN IF NOT EXISTS name  text NOT NULL DEFAULT '';

-- catalog lookup: ListProjects filters by owner, newest-first.
CREATE INDEX IF NOT EXISTS project_state_owner_idx ON project_state (owner, updated_at DESC);

CREATE TABLE IF NOT EXISTS applied_mutation (
    project_id      uuid        NOT NULL,
    idempotency_key text        NOT NULL,
    result_version  bigint      NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, idempotency_key)
);`

// ---------------------------------------------------------------------------
// JSONB codec — INTERNAL to this package (projectStateAccess.md §6: "JSONB-or-
// columns is the implementer's call"). ArtifactModel is an INTERFACE, so
// round-tripping it needs a kind-discriminated envelope: each populated slot is
// stored as {status, notes, kind, model} where `model` is the concrete typed
// model's own JSON. On read we switch on the stored kind to unmarshal into the
// right concrete *Xxx and assign it to the named slot. No schemaVersion
// envelope (§9 Q3 — additive-only evolution).
//
// WIRE-CONTRACT MIGRATION (Task 2.7): the inner `model` payload is the concrete
// model's own JSON, which now uses camelCase field names and STRING enum names
// (axis, severity, component kind, layer, …) rather than the prior PascalCase +
// integer ordinals. The model UnmarshalJSON paths accept BOTH the new string
// enums and legacy integer ordinals, so a slot persisted before this migration
// still decodes — but any pre-migration JSONB written with PascalCase field names
// is matched case-insensitively by Go on read. There is no production data: dev
// `project_state` rows should be reset after this change so freshly-written rows
// carry the canonical camelCase + string-enum shape. The slotJSON envelope itself
// (status/notes/kind/model) keeps its integer `kind` ordinal — it is the INTERNAL
// storage discriminator, not the public SPA wire contract.
// ---------------------------------------------------------------------------

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
	// keeps the on-disk shape byte-identical for every slot a critique never touched,
	// so the aiarch-validate decode and every legacy row are unaffected. The single
	// writer is the PM-critique Action (committing these keys into project.json); the
	// single reader is systemDesignManager.readBackCritique.
	CritiqueVerdict string `json:"critiqueVerdict,omitempty"`
	CritiqueNotes   string `json:"critiqueNotes,omitempty"`
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

// encodeSlots serialises every populated slot of the aggregate to the JSONB
// object shape (keys are decimal kind ordinals). A slot is "populated" when its
// status is not ReviewNone; an unpopulated slot is omitted entirely.
func encodeSlots(p *Project) ([]byte, error) {
	out, err := encodeSlotsMap(p)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// encodeSlotsMap is the substrate-neutral slot codec: it returns the kind-keyed
// slotJSON map both the Postgres JSONB column and the git project.json document
// embed, so a slot serialises identically across either store (a model written by
// one substrate round-trips through the other).
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

// decodeSlots parses the JSONB object back into the aggregate's named typed
// slots, switching on each stored kind to unmarshal into the right concrete
// model. Mutates p in place.
func decodeSlots(raw []byte, p *Project) error {
	var w map[string]slotJSON
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	return decodeSlotsMap(w, p)
}

// decodeSlotsMap is the substrate-neutral slot decoder shared by the Postgres and
// git stores (the inverse of encodeSlotsMap).
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
		if len(entry.Model) > 0 {
			model, ok := NewModelForKind(kind)
			if !ok {
				return fmt.Errorf("decode slots: no model type for kind %s", kind)
			}
			if err := json.Unmarshal(entry.Model, model); err != nil {
				return fmt.Errorf("decode slot %s model: %w", kind, err)
			}
			// Restore Solution SlotKind: the four share one concrete type; the
			// destination slot's kind is authoritative. NewModelForKind pre-sets
			// SlotKind, but re-apply after unmarshal as belt-and-suspenders in case
			// the persisted JSON had a stale or differing value.
			if sol, isSol := model.(*Solution); isSol {
				sol.SlotKind = kind
			}
			slot.Model = model
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Atomic business verbs — each is a one-liner over applyMutation supplying a
// pure, in-memory slot transition closure. The verb records the fact; it does
// not re-decide whether the transition is allowed (the Manager's / Engine's gate).
// ---------------------------------------------------------------------------

func (s *Store) StageArtifactForReview(ctx context.Context, projectID ProjectID, expectedVersion Version, model ArtifactModel, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if model == nil {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.StageArtifactForReview: nil staged model")
	}
	kind := model.Kind()
	return s.applyMutation(ctx, "StageArtifactForReview", projectID, expectedVersion, idempotencyKey, func(p *Project) error {
		slot, ok := slotPtr(p, kind)
		if !ok {
			return fwra.New(fwra.ContractMisuse, fmt.Sprintf("projectstate.StageArtifactForReview: unknown kind %s", kind))
		}
		slot.Status = ReviewAwaitingReview
		slot.Model = model
		slot.Notes = ""
		// A fresh stage supersedes any prior-round critique read-back on this slot.
		slot.CritiqueVerdict = ""
		slot.CritiqueNotes = ""
		return nil
	})
}

func (s *Store) CommitArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "CommitArtifact", projectID, expectedVersion, idempotencyKey, statusTransition("CommitArtifact", kind, ReviewCommitted, ""))
}

func (s *Store) RejectArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "RejectArtifact", projectID, expectedVersion, idempotencyKey, statusTransition("RejectArtifact", kind, ReviewRejected, notes))
}

func (s *Store) WithdrawArtifact(ctx context.Context, projectID ProjectID, expectedVersion Version, kind ArtifactKind, notes string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "WithdrawArtifact", projectID, expectedVersion, idempotencyKey, statusTransition("WithdrawArtifact", kind, ReviewWithdrawn, notes))
}

func (s *Store) AdvancePhase(ctx context.Context, projectID ProjectID, expectedVersion Version, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	return s.applyMutation(ctx, "AdvancePhase", projectID, expectedVersion, idempotencyKey, func(p *Project) error {
		p.Phase++
		return nil
	})
}

// SetResearchInput records the Phase-1 research corpus on the head-state
// (➕ 2026-05-29). It is a Method INPUT replace-in-place — no slot, no review
// lifecycle. ContractMisuse if research is the zero value. Runs through the same
// applyMutation dual discipline (optimistic concurrency + dedup) as the slot verbs.
//
// The project row must already exist (Task 2.3): a project is born explicitly via
// CreateProject, NOT implicitly on first research write. An absent row surfaces
// fwra.NotFound.
func (s *Store) SetResearchInput(ctx context.Context, projectID ProjectID, expectedVersion Version, research ResearchInput, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if research.IsZero() {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.SetResearchInput: empty research (no sources)")
	}
	return s.applyMutationMode(ctx, "SetResearchInput", projectID, expectedVersion, idempotencyKey, modeRequireExisting, func(p *Project) error {
		p.ResearchInput = research
		return nil
	})
}

// CreateProject is the EXPLICIT birth of a project (Task 2.3): it inserts a new
// head-state row at version 1 owned by owner with the given display name, in
// PhaseSystemDesign. It rides the shared applyMutation dual discipline in
// modeCreateOnly — idempotent on idempotencyKey (a retry returns the version the
// first attempt committed) and fwra.Conflict if the id already exists under a
// DIFFERENT key. The expectedVersion is always 0 (a brand-new row).
func (s *Store) CreateProject(ctx context.Context, projectID ProjectID, owner OwnerScope, name string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if owner == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.CreateProject: empty owner")
	}
	return s.applyMutationMode(ctx, "CreateProject", projectID, 0, idempotencyKey, modeCreateOnly, func(p *Project) error {
		p.Owner = owner
		p.Name = name
		p.Phase = PhaseSystemDesign
		return nil
	})
}

// ListProjects returns the catalog summaries for every project owned by owner,
// newest-first (Task 2.3). Each summary derives the current-phase progress
// (committed vs total artifact slots) from the stored slot set. An owner with no
// projects yields an empty, non-nil slice.
func (s *Store) ListProjects(ctx context.Context, owner OwnerScope) ([]ProjectSummary, error) {
	if owner == "" {
		return nil, fwra.New(fwra.ContractMisuse, "projectstate.ListProjects: empty owner")
	}
	const q = `
SELECT project_id, name, phase, slots, updated_at
FROM project_state
WHERE owner = $1
ORDER BY updated_at DESC, project_id DESC`
	rows, err := s.pool.Query(ctx, q, string(owner))
	if err != nil {
		return nil, fwpg.MapError(err, "projectstate.ListProjects")
	}
	defer rows.Close()

	summaries := []ProjectSummary{}
	for rows.Next() {
		var pid ProjectID
		var name string
		var phase int
		var slotsRaw []byte
		var updatedAt time.Time
		if scanErr := rows.Scan(&pid, &name, &phase, &slotsRaw, &updatedAt); scanErr != nil {
			return nil, fwpg.MapError(scanErr, "projectstate.ListProjects: scan row")
		}
		committed, total, sErr := phaseSlotCounts(Phase(phase), slotsRaw)
		if sErr != nil {
			return nil, fwra.Wrap(fwra.Infrastructure, sErr, "projectstate.ListProjects: count slots")
		}
		summaries = append(summaries, ProjectSummary{
			ProjectID:      pid,
			Name:           name,
			Owner:          owner,
			Phase:          Phase(phase),
			CommittedCount: committed,
			TotalCount:     total,
			UpdatedAt:      updatedAt,
		})
	}
	if rErr := rows.Err(); rErr != nil {
		return nil, fwpg.MapError(rErr, "projectstate.ListProjects: iterate rows")
	}
	return summaries, nil
}

// phaseSlotCounts decodes the stored slot set and reports, for the project's
// current phase, how many of that phase's required artifact slots are committed
// and how many there are in total. Phases beyond the two design phases have no
// required slot set and report (0, 0).
func phaseSlotCounts(phase Phase, slotsRaw []byte) (committed, total int, err error) {
	var required []ArtifactKind
	switch phase {
	case PhaseSystemDesign:
		required = Phase1RequiredKinds()
	case PhaseProjectDesign:
		required = Phase2RequiredKinds()
	default:
		return 0, 0, nil
	}

	p := Project{}
	if decErr := decodeSlots(slotsRaw, &p); decErr != nil {
		return 0, 0, decErr
	}
	total = len(required)
	for _, kind := range required {
		slot, ok := slotPtr(&p, kind)
		if ok && slot.Status == ReviewCommitted {
			committed++
		}
	}
	return committed, total, nil
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
		// Clear the PM-critique read-back carrier on every status transition: the
		// critique pertains to the in-flight draft being reviewed, not to the
		// committed/rejected/withdrawn outcome. Clearing here guarantees a stale
		// critique can never be read back across a reject/redraft loop (the collision
		// the senior review identified). The architect's reject/withdraw rationale
		// rides Notes (above), never the critique carrier.
		slot.CritiqueVerdict = ""
		slot.CritiqueNotes = ""
		return nil
	}
}

// applyMutation is the one shared write path. It records ONE head-state mutation
// under the dual discipline (projectStateAccess.md §2, §6). The transaction
// ordering is load-bearing — dedup FIRST, version guard SECOND:
//
//  1. Probe applied_mutation. A HIT means a prior attempt with this key already
//     committed: return its result_version as an idempotent no-op success,
//     IGNORING expectedVersion (a retried activity may re-pass a now-stale
//     version; the dedup must win — there is no public duplicate error).
//  2. Load the head-state FOR UPDATE (the row lock serialises racers on this
//     aggregate). No row + expectedVersion != 0 -> fwra.Conflict.
//  3. Version guard: row.version != expectedVersion -> fwra.Conflict (optimistic-
//     concurrency loser; the Manager re-reads, recomputes, re-applies).
//  4. Apply the pure in-memory slot transition and bump version. A transition
//     that violates a structural pre-condition returns ContractMisuse.
//  5. Upsert the head row guarded by the version (belt-and-suspenders for the
//     brand-new-row race the FOR UPDATE can't cover); 0 rows -> fwra.Conflict.
//     Then record the dedup ledger row with the committed result_version. Commit.
//
// mutationMode tunes how applyMutation treats the brand-new-row case.
type mutationMode int

const (
	// modeUpsert is the legacy default: an absent row at expectedVersion 0 is
	// created (the slot verbs are tolerant of being the first write).
	modeUpsert mutationMode = iota
	// modeRequireExisting fails with fwra.NotFound when no row exists. Used by
	// verbs that may only run on an already-created project (Task 2.3:
	// SetResearchInput).
	modeRequireExisting
	// modeCreateOnly fails with fwra.Conflict when a row already exists. Used by
	// CreateProject so a project is born exactly once.
	modeCreateOnly
)

func (s *Store) applyMutation(
	ctx context.Context,
	op string,
	projectID ProjectID,
	expectedVersion Version,
	idempotencyKey fwra.IdempotencyKey,
	mutate func(p *Project) error,
) (Version, error) {
	return s.applyMutationMode(ctx, op, projectID, expectedVersion, idempotencyKey, modeUpsert, mutate)
}

func (s *Store) applyMutationMode(
	ctx context.Context,
	op string,
	projectID ProjectID,
	expectedVersion Version,
	idempotencyKey fwra.IdempotencyKey,
	mode mutationMode,
	mutate func(p *Project) error,
) (Version, error) {
	if projectID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate."+op+": zero projectID")
	}
	if idempotencyKey.IsZero() {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate."+op+": empty idempotencyKey")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fwpg.MapError(err, "projectstate."+op)
	}
	defer tx.Rollback(ctx)

	// STEP 1 — dedup probe (dedup-first; preserves idempotent replay at a stale version).
	if v, found, e := lookupAppliedTx(ctx, tx, projectID, idempotencyKey, op); e != nil {
		return 0, e
	} else if found {
		return v, nil
	}

	// STEP 2 — load head-state FOR UPDATE.
	p, exists, e := loadProjectTx(ctx, tx, projectID, op)
	if e != nil {
		return 0, e
	}
	if exists && mode == modeCreateOnly {
		// The row already exists and this is a create-only verb (and the dedup probe
		// above already cleared this idempotency key): a genuine id collision.
		return 0, fwra.New(fwra.Conflict, fmt.Sprintf(
			"projectstate.%s: project %s already exists", op, projectID))
	}
	if !exists {
		if mode == modeRequireExisting {
			return 0, fwra.New(fwra.NotFound, fmt.Sprintf(
				"projectstate.%s: no aggregate for project %s (create it first)", op, projectID))
		}
		if expectedVersion != 0 {
			return 0, fwra.New(fwra.Conflict, fmt.Sprintf(
				"projectstate.%s: no aggregate for project %s but expectedVersion %d != 0",
				op, projectID, expectedVersion))
		}
		p = Project{ID: projectID, Version: 0}
	}

	// STEP 3 — version guard.
	if p.Version != expectedVersion {
		return 0, fwra.New(fwra.Conflict, fmt.Sprintf(
			"projectstate.%s: stale version for project %s: have %d, expected %d",
			op, projectID, p.Version, expectedVersion))
	}

	// STEP 4 — apply the pure transition + bump version. A structural pre-condition
	// violation (e.g. commit on an unpopulated slot) surfaces as ContractMisuse.
	if mErr := mutate(&p); mErr != nil {
		return 0, mErr
	}
	p.Version = expectedVersion + 1

	// STEP 5 — guarded upsert, then dedup ledger insert, then commit.
	if e := upsertProjectTx(ctx, tx, &p, expectedVersion, op); e != nil {
		return 0, e
	}
	const insKey = `INSERT INTO applied_mutation (project_id, idempotency_key, result_version) VALUES ($1,$2,$3)`
	if _, e := tx.Exec(ctx, insKey, projectID, string(idempotencyKey), int64(p.Version)); e != nil {
		return 0, fwpg.MapError(e, "projectstate."+op+": record idempotency key")
	}
	if e := tx.Commit(ctx); e != nil {
		return 0, fwpg.MapError(e, "projectstate."+op+": commit")
	}
	return p.Version, nil
}

// lookupAppliedTx resolves the committed result_version for an idempotencyKey,
// reporting whether a row exists. A present row means this is an idempotent retry
// (return its version, found=true); absent means proceed with the mutation.
func lookupAppliedTx(ctx context.Context, tx pgx.Tx, projectID ProjectID, idempotencyKey fwra.IdempotencyKey, op string) (Version, bool, error) {
	const q = `SELECT result_version FROM applied_mutation WHERE project_id = $1 AND idempotency_key = $2`
	var v int64
	err := tx.QueryRow(ctx, q, projectID, string(idempotencyKey)).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fwpg.MapError(err, "projectstate."+op+": probe idempotency ledger")
	}
	return Version(v), true, nil
}

// loadProjectTx loads the head-state row FOR UPDATE (row lock). exists=false with
// no error means the aggregate has never been written.
func loadProjectTx(ctx context.Context, tx pgx.Tx, projectID ProjectID, op string) (Project, bool, error) {
	const q = `SELECT version, phase, owner, name, slots, research FROM project_state WHERE project_id = $1 FOR UPDATE`
	var version int64
	var phase int
	var owner string
	var name string
	var raw []byte
	var researchRaw []byte
	err := tx.QueryRow(ctx, q, projectID).Scan(&version, &phase, &owner, &name, &raw, &researchRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, fwpg.MapError(err, "projectstate."+op+": load head-state")
	}
	p := Project{ID: projectID, Version: Version(version), Phase: Phase(phase), Owner: OwnerScope(owner), Name: name}
	if decErr := decodeSlots(raw, &p); decErr != nil {
		return Project{}, false, fwra.Wrap(fwra.Infrastructure, decErr, "projectstate."+op+": decode slots")
	}
	if decErr := decodeResearch(researchRaw, &p); decErr != nil {
		return Project{}, false, fwra.Wrap(fwra.Infrastructure, decErr, "projectstate."+op+": decode research")
	}
	return p, true, nil
}

// upsertProjectTx writes the head-state row guarded by the expected version. On
// an existing row whose version no longer matches expectedVersion the DO UPDATE
// WHERE clause filters the row out (0 rows affected) -> fwra.Conflict. On a
// brand-new project the INSERT path runs (no conflict). This is the belt-and-
// suspenders guard for the brand-new-row race the FOR UPDATE lock cannot cover.
func upsertProjectTx(ctx context.Context, tx pgx.Tx, p *Project, expectedVersion Version, op string) error {
	slotsJSON, err := encodeSlots(p)
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "projectstate."+op+": encode slots")
	}
	researchJSON, err := json.Marshal(p.ResearchInput)
	if err != nil {
		return fwra.Wrap(fwra.Infrastructure, err, "projectstate."+op+": encode research")
	}
	const q = `
INSERT INTO project_state (project_id, version, phase, owner, name, slots, research)
VALUES ($1, $2, $3, $7, $8, $4, $6)
ON CONFLICT (project_id) DO UPDATE
    SET version = EXCLUDED.version, phase = EXCLUDED.phase, owner = EXCLUDED.owner, name = EXCLUDED.name, slots = EXCLUDED.slots, research = EXCLUDED.research, updated_at = now()
    WHERE project_state.version = $5`
	ct, err := tx.Exec(ctx, q, p.ID, int64(p.Version), int(p.Phase), slotsJSON, int64(expectedVersion), researchJSON, string(p.Owner), p.Name)
	if err != nil {
		// A unique-violation on the PK during the INSERT arm means a concurrent
		// brand-new writer beat us to the row; classify as Conflict, not Infrastructure.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == fwpg.UniqueViolationCode {
			return fwra.Wrap(fwra.Conflict, err, "projectstate."+op+": concurrent create lost")
		}
		return fwpg.MapError(err, "projectstate."+op+": upsert head-state")
	}
	if ct.RowsAffected() == 0 {
		return fwra.New(fwra.Conflict, fmt.Sprintf(
			"projectstate.%s: version guard lost for project %s (expected %d)", op, p.ID, expectedVersion))
	}
	return nil
}

// ReadProject serves the read side: the whole head-state aggregate, including
// every populated typed-model slot. An absent row is fwra.NotFound — the caller
// branches on absence (projectStateAccess.md §2).
func (s *Store) ReadProject(ctx context.Context, projectID ProjectID) (Project, error) {
	if projectID == "" {
		return Project{}, fwra.New(fwra.ContractMisuse, "projectstate.ReadProject: zero projectID")
	}
	const q = `SELECT version, phase, owner, name, slots, research FROM project_state WHERE project_id = $1`
	var version int64
	var phase int
	var owner string
	var name string
	var raw []byte
	var researchRaw []byte
	err := s.pool.QueryRow(ctx, q, projectID).Scan(&version, &phase, &owner, &name, &raw, &researchRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, fwra.New(fwra.NotFound, fmt.Sprintf("projectstate.ReadProject: no state for project %s", projectID))
	}
	if err != nil {
		return Project{}, fwpg.MapError(err, "projectstate.ReadProject")
	}
	p := Project{ID: projectID, Version: Version(version), Phase: Phase(phase), Owner: OwnerScope(owner), Name: name}
	if decErr := decodeSlots(raw, &p); decErr != nil {
		return Project{}, fwra.Wrap(fwra.Infrastructure, decErr, "projectstate.ReadProject: decode slots")
	}
	if decErr := decodeResearch(researchRaw, &p); decErr != nil {
		return Project{}, fwra.Wrap(fwra.Infrastructure, decErr, "projectstate.ReadProject: decode research")
	}
	return p, nil
}

// decodeResearch parses the research JSONB column into p.ResearchInput. An empty
// or NULL-ish column ('{}' default) decodes to the zero ResearchInput (no sources).
func decodeResearch(raw []byte, p *Project) error {
	if len(raw) == 0 {
		return nil
	}
	var ri ResearchInput
	if err := json.Unmarshal(raw, &ri); err != nil {
		return err
	}
	p.ResearchInput = ri
	return nil
}
