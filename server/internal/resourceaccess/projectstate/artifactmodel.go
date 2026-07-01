package projectstate

import "time"

// ProjectSummary is the catalog row for the landing grid (Task 2.3). It is a
// derived projection of the head-state — NOT a stored shape — returned by
// ListProjects: identity + display fields plus the current-phase progress
// (committed vs total artifact slots) so the grid can render a progress badge
// without loading every project's full slot set.
type ProjectSummary struct {
	ProjectID      ProjectID
	Name           string
	Owner          OwnerScope
	Phase          Phase
	CommittedCount int // committed artifact slots in the current phase
	TotalCount     int // total required artifact slots in the current phase
	UpdatedAt      time.Time
}

// Phase identifies the lifecycle phase the project currently sits in.
// Additive as later Managers come online. (projectStateAccess.md §3.1)
type Phase int

const (
	// PhaseSystemDesign is Phase 1 — driven by systemDesignManager.
	PhaseSystemDesign Phase = iota
	// PhaseProjectDesign is Phase 2 — reachable once Phase 1 is sealed (advancePhase).
	PhaseProjectDesign
	// PhaseConstruction is Phase 3 — reachable once Phase 2 is sealed by
	// projectDesignManager.advanceToConstruction (which seals the SDP-review option
	// gate). The Phase-3 work itself is owned by the constructionManager; this
	// constant only gives AdvancePhase a clean target beyond PhaseProjectDesign so
	// the Phase-2 seal increments into a named phase rather than an unnamed ordinal.
	// (projectDesignManager.md §2.4 / PHASE NOTE — additive.)
	PhaseConstruction
	// additive as later phases come online (Operations)
)

// ArtifactReviewStatus is the per-slot review state in the Project head-state
// aggregate. (projectStateAccess.md §3.1)
type ArtifactReviewStatus int

const (
	// ReviewNone — the slot has never been staged (zero value).
	ReviewNone ArtifactReviewStatus = iota
	// ReviewAwaitingReview — staged, suspended at the review gate.
	ReviewAwaitingReview
	// ReviewCommitted — architect approved.
	ReviewCommitted
	// ReviewRejected — architect rejected (will redraft); model retained for the redraft baseline.
	ReviewRejected
	// ReviewWithdrawn — architect abandoned at the gate.
	ReviewWithdrawn
)

// ArtifactModel is the closed interface every typed Method model implements.
// Kind() lets stageArtifactForReview route a model to its named slot by concrete
// Go type; isArtifactModel() is unexported and seals the sum — only the models
// enumerated in this package (System, and the models in Task 3) satisfy it.
//
// This is NOT an open extension point — extending it would resurrect the retired
// ArtifactSchema volatility. The Method is a stable book; the artifact set is closed.
// (projectStateAccess.md §3.1)
type ArtifactModel interface {
	Kind() ArtifactKind
	isArtifactModel() // unexported: closes the sum to this package's models
}

// ArtifactSlot pairs a review status with the typed model and the architect's
// notes for a single named artifact slot in the Project aggregate.
// (projectStateAccess.md §3.1)
//
// CRITIQUE READ-BACK CARRIER (additive, D-MSD-Δ amendment ratified 2026-06-15).
// CritiqueVerdict + CritiqueNotes are a FIRST-CLASS, optional, defaulted-empty
// read-back location for the PM-critique round-trip the systemDesignManager runs
// before the human gate. They are DISTINCT from Notes (whose frozen meaning —
// architect reject/withdraw rationale — is 100% preserved): the senior review of
// C-MSD-Δ found that overloading Notes as the critique carrier produces a concrete
// misread on the PM-kind reject loop (a RejectArtifact writes slot.Notes, then a
// critique read-back with no intervening Stage would misclassify the architect's
// reject notes as the PM verdict) and that "empty Notes = approve" is ambiguous.
// These dedicated fields remove that overload.
//
//   - SINGLE WRITER: the PM-critique agentic Action, via the committed
//     .aiarch/state/project.json (the slot's critiqueVerdict / critiqueNotes JSON
//     keys). aiarch's server-side thin-write verbs NEVER set them — and
//     StageArtifactForReview / the status-transition verbs CLEAR them (so a stale
//     critique from a prior round can never leak across a redraft/reject loop).
//   - SINGLE READER: the systemDesignManager's readBackCritique (after a critique
//     dispatch reaches PhaseSucceeded). CritiqueVerdict drives the verdict; the
//     "empty Notes = approve" ambiguity is gone — see readBackCritique for the
//     missing-verdict safe-default rule (a critique-expected read-back with an
//     empty verdict is a draft failure, NOT a silent approve).
//
// Defaulted-empty (omitempty in the slot codec) so every existing reader/writer —
// and the out-of-process aiarch-validate CLI decode — is unaffected.
type ArtifactSlot struct {
	Status ArtifactReviewStatus
	Model  ArtifactModel // the canonical typed model; nil only while ReviewNone
	Notes  string        // architect rationale; populated by the ResourceAccess layer on Reject/Withdraw

	// CritiqueVerdict is the PM-critique read-back verdict for this slot
	// ("" | CritiqueVerdictApprove | CritiqueVerdictRevise). Empty == no critique
	// committed for the current draft. Written ONLY by the PM-critique Action;
	// cleared by StageArtifactForReview / the status-transition verbs.
	CritiqueVerdict string
	// CritiqueNotes is the PM-critique read-back revision guidance, carried on a
	// Revise verdict. Distinct from Notes (the architect's reject/withdraw rationale).
	CritiqueNotes string
}

// Canonical CritiqueVerdict carrier values written into ArtifactSlot.CritiqueVerdict
// by the PM-critique Action and read back by the systemDesignManager. They are the
// projectStateAccess-layer string encoding of the Manager's CritiqueVerdict enum;
// the Manager maps between the two so the typed enum stays the Manager's own surface.
const (
	// CritiqueVerdictApprove ratifies the just-committed draft unchanged.
	CritiqueVerdictApprove = "approve"
	// CritiqueVerdictRevise asks for a redraft; CritiqueNotes carries the guidance.
	CritiqueVerdictRevise = "revise"
)

// Project is the head-state aggregate — the "sane state object" the contract is
// built around. Read whole; never folded. Each Phase-1/2 artifact is a NAMED
// TYPED SLOT, not a map[ArtifactKind]ArtifactRef. Named slots over a map: the
// set of Method artifacts is closed and known (the book defines exactly these),
// so a struct of named fields is the faithful, self-documenting encoding and
// prevents an unknown ArtifactKind ever appearing.
//
// The ArtifactKind enum is retained for the generic write verbs and internal
// slot-by-kind lookup used by the RA implementation. (projectStateAccess.md §3.2)
type Project struct {
	ID      ProjectID
	Version Version
	Phase   Phase

	// Owner is the principal that owns this project — the catalog scope used by
	// ListProjects. Set once at CreateProject; the project is born explicitly with
	// an owner rather than implicitly on first write. (Task 2.3)
	Owner OwnerScope
	// Name is the human-readable project name shown in the landing grid. Set at
	// CreateProject. (Task 2.3)
	Name string

	// ResearchInput is the Phase-1 research corpus the system-design sequence
	// STARTS from (➕ 2026-05-29, projectStateAccess.md §3.2/§3.8). A Method INPUT,
	// NOT an ArtifactModel and NOT review-gated: set whole via setResearchInput,
	// read back by systemDesignManager to seed the mission-draft worker call. Zero
	// value (no Sources) == not yet provided.
	ResearchInput ResearchInput

	// ActivityGit is the per-activity git-forward head-state, keyed by ActivityID
	// (➕ 2026-06-12, D-PA-GIT, projectStateAccess.md §GIT-HEAD-STATE). Additive,
	// populated only in Phase 3 (nil until the first Record* git verb) — the same
	// posture as the §2-DELTA Construction facet and ResearchInput. The durable
	// mirror of what IPullRequestRail returns; PROVIDER-OPAQUE (opaque String()
	// handles + a typed CI enum, no provider lexeme). Read whole via readProject so
	// the webClient (C-CW-GIT) can project each row onto the ux-mock GitRef.
	ActivityGit map[string]ActivityGitStatus

	// ActivityConstruction is the per-activity construction head-state, keyed by
	// ActivityID (➕ 2026-06-17, Task 1: seed-archistrator-design-state). Additive,
	// populated only in Phase 3 (nil until the first RecordActivityStarted call) —
	// same posture as ActivityGit. Tracks the coarse lifecycle (NotStarted/Running/Done)
	// and server-resolved timestamps for the dry-run construction pump.
	ActivityConstruction map[string]ActivityConstructionStatus

	// ConstructionProgress is the project-level Phase-3 tracking snapshot (ux-mock
	// Tracker framing scalars). Additive, nil until seeded by the bootstrap generator.
	// EV curves are NOT stored here — they are derived at read time from the per-activity
	// status + the network. Only the framing scalars are seeded.
	ConstructionProgress *ConstructionProgress

	// ServiceContracts is the per-component typed service-contract corpus (extracted
	// from the real contract markdown). Additive, keyed by component name, nil until seeded.
	ServiceContracts map[string]ServiceContract

	// PhaseArtifacts holds the typed phase-scoped artifacts produced during Phase-3
	// construction (SRS, test plans, integration notes, UI designs, etc.). Additive,
	// nil until the first RecordPhaseArtifactProduced call.
	PhaseArtifacts *PhaseArtifacts `json:"phaseArtifacts,omitempty"`

	// TestingState holds the project-level testing artifacts produced by N-* activities
	// (system test plan, harness, perf rig, quality gates, test runs, defects). Additive,
	// nil until the first testing activity produces output.
	TestingState *TestingState `json:"testingState,omitempty"`

	// OperatorPaused is set when an operator pauses the project's construction
	// (RecordOperatorPaused). Cleared when construction resumes (not yet a verb in
	// the v1 contract; the field is additive and defaults false).
	OperatorPaused bool
	// PauseReason is the operator-supplied reason for the pause. Empty when not paused.
	PauseReason string

	// ReviewPolicy is the per-project committed configuration of which (activity-type,
	// phase) pairs require human approval during construction. The zero value gates
	// nothing — the construction loop behaves as before this feature was introduced.
	ReviewPolicy ReviewPolicy `json:"reviewPolicy,omitempty"`

	// ---- Phase 1 slots ----
	Mission              ArtifactSlot // Model is *MissionStatement when populated
	Glossary             ArtifactSlot // Model is *Glossary
	ScrubbedRequirements ArtifactSlot // Model is *ScrubbedRequirements (OQ-2)
	Volatilities         ArtifactSlot // Model is *Volatilities
	CoreUseCases         ArtifactSlot // Model is *CoreUseCases
	SystemDesign         ArtifactSlot // Model is *System (Grammar A)
	OperationalConcepts  ArtifactSlot // Model is *OperationalConcepts
	StandardCheck        ArtifactSlot // Model is *StandardCheck

	// ---- Phase 2 slots (additive; design-only until projectDesignManager is built) ----
	PlanningAssumptions  ArtifactSlot // Model is *PlanningAssumptions
	ActivityList         ArtifactSlot // Model is *ActivityList
	Network              ArtifactSlot // Model is *Network
	NormalSolution       ArtifactSlot // Model is *Solution
	SubcriticalSolution  ArtifactSlot // Model is *Solution
	CompressedSolution   ArtifactSlot // Model is *Solution
	DecompressedSolution ArtifactSlot // Model is *Solution
	RiskModel            ArtifactSlot // Model is *RiskModel
	SdpReview            ArtifactSlot // Model is *SdpReview
}
