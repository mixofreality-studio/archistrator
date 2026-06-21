// Package project is the projectManager component of the aiarch server's Manager
// layer — a THIN use-case façade over the project HEAD-STATE aggregate
// (architecture.dsl `projectManager`). Unlike systemDesignManager /
// projectDesignManager it owns NO durable workflow of its own: it has no Temporal
// dependency, no Activities, no Signal/Query handlers. It is the project CATALOG +
// cross-phase typed read the Client (webClient) talks to for the three
// non-co-authoring operations:
//
//   - CreateProject — birth a project (generate the ProjectID, derive an
//     idempotency key, delegate to projectStateAccess.CreateProject).
//   - ListProjects  — the landing-grid catalog for an owner (pass-through).
//   - GetProject    — the full typed head-state for one project, mapped from the
//     Project aggregate into a transport-friendly typed ProjectState.
//
// This is the MANAGER layer but a degenerate one: it imports only the
// projectStateAccess ResourceAccess port (a downward edge) and the framework-go
// manager error model. It deliberately does NOT import another Manager
// (systemDesignManager) — that would be a SIDEWAYS import the layer model forbids
// and TestMethodLayering's NoSideways rule rejects. The UI stage enum the
// architect's review surface needs is therefore defined LOCALLY here as
// ArtifactStage (mirroring systemdesign.SessionStage's committed/awaitingReview/…
// stages) rather than imported.
//
// File layout within the component:
//   - contract.go        : the public façade types (ProjectState/ArtifactSlotView/
//     ArtifactStage) + the model-envelope JSON codec + the
//     narrow ProjectStateAccess port this package declares.
//   - projectmanager.go  : the Manager and its three ops.
//   - errors.go          : the RA-error → Manager-error translation.
package project

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ---------------------------------------------------------------------------
// Shared domain vocabulary, re-exported as aliases so the public façade reads in
// its own surface terms while staying one-source-of-truth with projectstate. A
// Client (webClient) may depend on these value types via the Manager surface — a
// legal Client→Manager value-type dependency on a shared domain value, NOT a
// Client→ResourceAccess call.
// ---------------------------------------------------------------------------

type (
	// ProjectID is the project aggregate identifier — a string newtype whose value
	// IS the user-supplied adopted repo name (name-as-identity, C-PM-Δ 2026-06-15).
	ProjectID = projectstate.ProjectID
	// OwnerScope is the owning-principal catalog scope.
	OwnerScope = projectstate.OwnerScope
	// Version is the head-state optimistic-concurrency token.
	Version = projectstate.Version
	// Phase is the lifecycle phase the project currently sits in.
	Phase = projectstate.Phase
	// ArtifactKind discriminates the named typed-model slots.
	ArtifactKind = projectstate.ArtifactKind
)

// ProjectSummary is the catalog row for the landing grid. It is the projectstate
// projection re-exported verbatim (stable field names) so ListProjects reads on
// the Manager surface without a redundant mapping layer.
type ProjectSummary = projectstate.ProjectSummary

// ActivityGitStatus is the per-activity git-forward head-state record
// (projectStateAccess §GIT-HEAD-STATE, D-PA-GIT). Re-exported verbatim from the
// owning ResourceAccess so the head-state read surfaces it on the Manager surface
// without a redundant mapping layer — exactly the ProjectSummary re-export pattern.
// A Client (webClient) may depend on this value type via the Manager surface (a
// legal Client→Manager value-type dependency, NOT a Client→ResourceAccess call).
// It is a PURE read projection: the Manager neither writes nor derives it (the git
// Record* verbs on projectStateAccess own all writes).
type ActivityGitStatus = projectstate.ActivityGitStatus

// ActivityConstructionStatus is the per-activity construction head-state record
// (Task 1: seed-archistrator-design-state). Re-exported verbatim from the owning
// ResourceAccess exactly as ActivityGitStatus — a pure read projection on the
// Manager surface. The Client (webClient) may depend on this value type via the
// Manager surface (a legal Client→Manager value-type dependency).
type ActivityConstructionStatus = projectstate.ActivityConstructionStatus

// ConstructionProgress is the project-level Phase-3 tracking framing scalars.
// Re-exported verbatim from the owning ResourceAccess so the Manager surface
// exposes the seeded framing without a redundant mapping layer.
type ConstructionProgress = projectstate.ConstructionProgress

// ServiceContract is the typed model for one component's service contract corpus
// entry. Re-exported verbatim from the owning ResourceAccess exactly as
// ActivityConstructionStatus and ConstructionProgress — a pure read projection on
// the Manager surface. The Client (webClient) may depend on this value type via the
// Manager surface (a legal Client→Manager value-type dependency, NOT a
// Client→ResourceAccess call).
type ServiceContract = projectstate.ServiceContract

// CICheckState is the provider-neutral 3-state CI rollup the SPA renders, re-exported
// from the owning ResourceAccess. A DUMB reflection of the GitHub-Actions run — it
// NEVER gates any Approve control (GIT.1).
type CICheckState = projectstate.CICheckState

// ArtifactStage collapses a slot's review status into the handful of stages the UI
// needs at the project-catalog read. It is the head-state counterpart of
// systemdesign.SessionStage (which describes a LIVE session's technical progress):
// this enum is derived purely from the stored ArtifactReviewStatus and carries no
// session-transient state (no Drafting/Redrafting/Refused — those exist only while
// a co-authoring workflow runs and are read via systemDesignManager.GetSessionState,
// not the head-state).
//
// Defined LOCALLY rather than imported from systemdesign: a Manager importing
// another Manager is a sideways edge the layer model forbids (TestMethodLayering
// NoSideways). The ordinals are independent of systemdesign.SessionStage.
type ArtifactStage int

const (
	// StageEmpty — the slot has never been staged (maps ReviewNone).
	StageEmpty ArtifactStage = iota
	// StageAwaitingReview — staged, suspended at the review gate (maps ReviewAwaitingReview).
	StageAwaitingReview
	// StageCommitted — architect approved (maps ReviewCommitted).
	StageCommitted
	// StageRejected — architect rejected; model retained for the redraft baseline (maps ReviewRejected).
	StageRejected
	// StageWithdrawn — architect abandoned the draft at the gate (maps ReviewWithdrawn).
	StageWithdrawn
)

// stageForStatus maps the stored per-slot ArtifactReviewStatus to the UI stage.
func stageForStatus(s projectstate.ArtifactReviewStatus) ArtifactStage {
	switch s {
	case projectstate.ReviewAwaitingReview:
		return StageAwaitingReview
	case projectstate.ReviewCommitted:
		return StageCommitted
	case projectstate.ReviewRejected:
		return StageRejected
	case projectstate.ReviewWithdrawn:
		return StageWithdrawn
	case projectstate.ReviewNone:
		return StageEmpty
	default:
		return StageEmpty
	}
}

// ArtifactSlotView is one artifact slot of the head-state, typed. It carries ONLY
// what ReadProject's Project aggregate exposes per slot: the slot kind, its review
// stage, the typed model (nil while the slot is empty), and the architect's notes
// (populated by the RA on Reject/Withdraw). Findings/Critique are SESSION-TRANSIENT
// (they live in systemDesignManager.GetSessionState, not the head-state aggregate),
// so they are deliberately OMITTED here — the head-state has no such fields to map.
type ArtifactSlotView struct {
	Kind  ArtifactKind               `json:"kind"`
	Stage ArtifactStage              `json:"stage"`
	Model projectstate.ArtifactModel `json:"model"`           // typed model; nil if the slot is empty
	Notes string                     `json:"notes,omitempty"` // architect rationale on Reject/Withdraw
}

// ProjectState is the full typed head-state for one project — the answer to
// GetProject. Slots holds one ArtifactSlotView per defined ArtifactKind in the
// stable slot order (projectstate.AllArtifactKinds()).
//
// GitRows carries the per-activity git-forward head-state (D-PA-GIT), keyed by
// ActivityID — the durable mirror of the branch→PR→CI→+1→merge lifecycle the
// construction-session view and the change-request views render. It is the SAME
// CQRS head-state read (off Project.ActivityGit), NOT the construction-session
// Temporal Query (which is technical progress only). nil until the first git
// Record* verb populates it in Phase 3 — the honest-empty convention (an activity
// with no git row is simply absent from the map, never a fabricated empty row).
//
// ActivityConstruction carries the per-activity construction head-state keyed by
// ActivityID (Task 1: seed-archistrator-design-state). Carried whole from
// Project.ActivityConstruction — the same posture as GitRows. nil until the first
// RecordActivityStarted verb in Phase 3.
//
// ConstructionProgress carries the project-level Phase-3 framing scalars. nil
// until seeded by the bootstrap generator.
type ProjectState struct {
	ProjectID            ProjectID
	Name                 string
	Owner                OwnerScope
	Phase                Phase
	Version              Version
	Research             projectstate.ResearchInput
	Slots                []ArtifactSlotView
	GitRows              map[string]ActivityGitStatus
	ActivityConstruction map[string]ActivityConstructionStatus
	ConstructionProgress *ConstructionProgress
	ServiceContracts     map[string]ServiceContract
}

// ---------------------------------------------------------------------------
// Wire codec — the typed ArtifactModel is the sealed projectstate.ArtifactModel
// interface, which the default JSON converter cannot decode (it does not know the
// concrete type). It is carried as the SAME discriminated {kind:int, raw:json}
// envelope systemdesign uses across its Temporal/REST seam, so the SPA's generated
// client decodes a slot's model identically whether it arrives via the
// systemDesignManager session read or the projectManager head-state read. The key
// names ("kind"/"raw") and the kind ordinals are projectstate.ArtifactKind's, i.e.
// IDENTICAL to systemdesign.modelEnvelope by construction.
// ---------------------------------------------------------------------------

// modelEnvelope mirrors systemdesign.modelEnvelope exactly: the STRING kind
// discriminator + the concrete model's own JSON under "model"
// ({"kind":"mission","model":{…}}). A nil model encodes as the zero envelope (Model
// empty), which decodes back to a nil model.
type modelEnvelope struct {
	Kind  ArtifactKind    `json:"kind"`
	Model json.RawMessage `json:"model,omitempty"`
}

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
		// the envelope Kind is authoritative.
		sol.SlotKind = e.Kind
	}
	return model, nil
}

// artifactSlotViewWire is the JSON wire form of ArtifactSlotView: the typed Model
// is carried as the discriminated envelope.
type artifactSlotViewWire struct {
	Kind  ArtifactKind  `json:"kind"`
	Stage ArtifactStage `json:"stage"`
	Model modelEnvelope `json:"model"`
	Notes string        `json:"notes,omitempty"`
}

// MarshalJSON encodes the typed Model via the model envelope codec.
func (v ArtifactSlotView) MarshalJSON() ([]byte, error) {
	me, err := encodeModel(v.Model)
	if err != nil {
		return nil, err
	}
	return json.Marshal(artifactSlotViewWire{Kind: v.Kind, Stage: v.Stage, Model: me, Notes: v.Notes})
}

// UnmarshalJSON reconstructs the concrete typed Model from its envelope.
func (v *ArtifactSlotView) UnmarshalJSON(data []byte) error {
	var w artifactSlotViewWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	model, err := w.Model.decode()
	if err != nil {
		return err
	}
	v.Kind = w.Kind
	v.Stage = w.Stage
	v.Model = model
	v.Notes = w.Notes
	return nil
}

// ---------------------------------------------------------------------------
// Narrow ResourceAccess port — the verbs this Manager needs, a subset of
// projectstate.ProjectStateAccess. Declared here so the Manager depends on the
// minimum surface and is trivially fakeable in tests (contract-first test double).
// ---------------------------------------------------------------------------

// ProjectStateAccess is the narrow head-state port the projectManager consumes.
// projectstate.ProjectStateAccess satisfies it structurally.
type ProjectStateAccess interface {
	CreateProject(ctx context.Context, projectID ProjectID, owner OwnerScope, name string, idempotencyKey fwra.IdempotencyKey) (Version, error)
	ListProjects(ctx context.Context, owner OwnerScope) ([]ProjectSummary, error)
	ReadProject(ctx context.Context, projectID ProjectID) (projectstate.Project, error)
}

// ---------------------------------------------------------------------------
// Narrow sourceControlAccess port — the lifecycle verbs project birth needs.
// Declared here (not imported from the concrete sourcecontrol package) so this
// Manager depends only on the verbs it calls, per dependency-inversion / the layer
// model: a Manager depends on an interface IT declares, which the concrete
// ResourceAccess satisfies structurally in the composition root. The opaque value
// types this port names (RepoSpec / RepoRef / RepoCredential) are the SAME
// provider-neutral types the RA exposes, threaded through a tiny composition-root
// adapter (sourcecontrol_adapter.go) so this package imports no GitHub vocabulary
// and no sibling-RA concrete. NAME-AS-IDENTITY (C-PM-Δ, 2026-06-15): the user
// supplies the repo NAME, which IS the project identity, so RepoSpec.RepoName
// (not a server-minted id) is the load-bearing field.
// ---------------------------------------------------------------------------

// RepoSpec is the provider-NEUTRAL description of the user's EXISTING repo to ADOPT
// at project birth (C-PM-Δ: REPLACES the old provision-by-id shape). RepoName is the
// user-supplied identity (project name == repo name, name-as-identity). Mirrors
// sourcecontrol.RepoAdoptionSpec's load-bearing surface; declared locally so the
// Manager owns the port shape and imports no GitHub vocabulary.
type RepoSpec struct {
	// RepoName is the user-supplied repo name == the project identity (the adopt
	// idempotency anchor). The repo MUST already exist; AdoptProjectRepo never creates it.
	RepoName string
	// Account is the provider-neutral source-control account/org the repo lives under
	// (the App installation's org). Empty means "the RA's composition-root default account".
	Account string
	// Title is the human project name. The RA applies it as the repo description so
	// the discover-by-enumeration catalog can render a title without a per-repo read.
	Title string
}

// RepoRef is an opaque, provider-neutral handle to the adopted per-project repo.
// The projectManager threads it from AdoptProjectRepo into the seating verbs
// (credential mint + secret + workflow file), then discards it (the repo name IS
// the identity, so the handle is re-derivable later). It treats the ref as fully opaque.
type RepoRef interface {
	// IsZero reports whether the ref addresses no repo.
	IsZero() bool
	// String returns the canonical printable form (logging/audit only).
	String() string
}

// RepoCredential is an opaque, short-lived bearer credential the Manager MINTS from
// the adopted repo (MintRepoCredential) and THREADS into the seating verb
// (SeatAgenticWorkflow) as a caller-supplied parameter — exactly the "returned,
// never recorded" discipline of sourceControlAccess §1.1. Fully opaque to the
// Manager: it neither parses, logs, nor persists it.
type RepoCredential interface {
	// IsZero reports whether the credential addresses no repo / is unset.
	IsZero() bool
}

// SourceControlAccess is the narrow source-control lifecycle port the projectManager
// consumes at project birth. It is the subset of sourcecontrol.SourceControlLifecycle
// this Manager needs to ADOPT the user's repo and SEAT it for agentic dispatch
// (caller-home ratified == project birth) BEFORE the project head-state row is
// created, so a project is never born without an adopted, workflow-seated repo. Every
// write verb is idempotent (adopt re-converges on the repo name; workflow-file is
// overwrite-if-changed), so a retry after a partial failure re-converges (the I-RA
// call-order guarantee, preserved).
//
// CORRECTION (2026-06-15, founder ruling): WriteAgenticToken was REMOVED. aiarch does
// NO secret management. The CLAUDE_CODE_OAUTH_TOKEN the agentic-design workflow reads
// is provisioned by the Claude Code GitHub App when the USER runs /install-github-app
// on their repo — a user onboarding prerequisite, never seated by aiarch. Seating now
// means just committing the workflow file (which still needs an installation token via
// MintRepoCredential).
type SourceControlAccess interface {
	// AdoptProjectRepo adopts the user's existing repo under the App installation and
	// tags it. Permissive (2026-06-16): succeeds regardless of content; only
	// NotUnderInstallation is terminal (the strict-empty RepoNotEmpty hard-fail is gone).
	AdoptProjectRepo(ctx context.Context, spec RepoSpec, key fwra.IdempotencyKey) (RepoRef, error)
	// MintRepoCredential mints the short-lived credential SeatAgenticWorkflow needs to
	// commit the workflow file. The Manager threads the result into SeatAgenticWorkflow.
	MintRepoCredential(ctx context.Context, repo RepoRef) (RepoCredential, error)
	// SeatAgenticWorkflow commits the claude-code-action DESIGN workflow file into the
	// repo's .github/workflows/. WHICH file (the embedded design template) is the
	// adapter's concern; the Manager only requests the repo be seated for dispatch.
	SeatAgenticWorkflow(ctx context.Context, repo RepoRef, cred RepoCredential, key fwra.IdempotencyKey) error
}
