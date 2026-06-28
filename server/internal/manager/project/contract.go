// Package project is the projectManager component of the aiarch server's Manager
// layer — a THIN use-case façade over the project HEAD-STATE aggregate
// (architecture.dsl `projectManager`). Unlike systemDesignManager /
// projectDesignManager it owns NO durable workflow of its own: it has no Temporal
// dependency, no Activities, no Signal/Query handlers. It is the project CATALOG +
// cross-phase typed read the Client (webClient) talks to for the three
// non-co-authoring operations:
//
//   - CreateProject — birth a project (name-as-identity; adopt repo + seat the
//     agentic-design workflow file, then create the head-state row).
//   - ListProjects  — the landing-grid catalog for an owner (pass-through).
//   - GetProject    — the full typed head-state for one project, mapped from the
//     projectstate.Project aggregate into a transport-friendly typed ProjectState.
//
// SCHEMA-FIRST (full encapsulation): this component OWNS its contract I/O types.
// The public surface (ProjectManager port + the I/O value types below) is GENERATED
// into contract.gen.go from contract.schema.json (edit the schema + `make gen`; do
// NOT hand-edit the generated surface). The generated contract imports NEITHER the
// projectstate ResourceAccess NOR Temporal: project mirrors the head-state value
// shapes as its OWN types and field-maps from projectstate at the Manager boundary
// (the construction/operations/settlement Manager precedent). In particular the
// per-slot artifact MODEL is carried OPAQUELY — an {kind, raw-json} envelope
// (ArtifactSlotModel) — so project never regenerates or shares projectstate's sealed
// ArtifactModel sum or its 17 variants.
//
// The consumer-side dependency ports (ProjectStateAccess / SourceControlAccess in
// ports.go) and the behavior over the contract value types (behavior.go) stay
// HAND-WRITTEN and are NOT part of the generated contract.
package project

// ---------------------------------------------------------------------------
// Identity scalars (project's OWN named types — name-as-identity, C-PM-Δ). They
// MIRROR projectstate's identity newtypes; the Manager converts at the RA boundary.
// ---------------------------------------------------------------------------

// ProjectID is the project aggregate identifier — its value IS the user-supplied
// adopted repo name (project name == repo name).

// OwnerScope is the owning-principal catalog scope (the authenticated subject/email).

// ---------------------------------------------------------------------------
// Lifecycle / review / status enums (project's OWN, value-identical to projectstate;
// behavior lives in behavior.go as free functions so the generated types are pure
// data). The ordinals match projectstate so int(...) conversion is meaning-preserving.
// ---------------------------------------------------------------------------

// Phase identifies the lifecycle phase the project currently sits in.

// PhaseSystemDesign is Phase 1 — driven by systemDesignManager.

// PhaseProjectDesign is Phase 2 — driven by projectDesignManager.

// PhaseConstruction is Phase 3 — driven by constructionManager.

// ArtifactStage collapses a slot's stored review status into the handful of stages
// the catalog read needs. Derived purely from the head-state ArtifactReviewStatus
// (no session-transient Drafting/Redrafting/Refused — those live in a live session
// read, not the head-state).

// StageEmpty — the slot has never been staged (maps ReviewNone).

// StageAwaitingReview — staged, suspended at the review gate (maps ReviewAwaitingReview).

// StageCommitted — architect approved (maps ReviewCommitted).

// StageRejected — architect rejected; model retained for the redraft baseline (maps ReviewRejected).

// StageWithdrawn — architect abandoned the draft at the gate (maps ReviewWithdrawn).

// CICheckState is the provider-neutral CI rollup the SPA renders (3 states). A DUMB
// reflection of the Actions run — it NEVER gates any Approve control.

// CICheckPending — at least one check still running, none failed.

// CICheckSuccess — all checks concluded successfully.

// CICheckFailure — at least one check failed.

// ActivityType is the canonical per-activity construction type axis.

// ActivityTypeService — a Manager/Engine/ResourceAccess/Client component build.

// ActivityTypeFrontend — a SPA / web UI surface build.

// ActivityTypeTesting — a system-test / CI activity (variant selected by TestingVariant).

// ActivityTypeDeployment — a devops / provisioning activity.

// ActivityTypeDocumentation — a tech-writing / ADR / runbook activity.

// TestingVariant discriminates the five N-* testing activity sub-types (meaningful
// only when ActivityType == ActivityTypeTesting).

// TestVariantPlan — N-STP: system test plan.

// TestVariantHarness — N-STH: test harness construction.

// TestVariantPerf — N-PERF: performance rig.

// TestVariantSystemTest — N-IT: system test execution.

// TestVariantQAProcess — N-QA: QA process definition.

// ActivityConstructionPhase is the coarse per-activity construction lifecycle.

// ActivityConstructionNotStarted is the zero value — not yet dispatched.

// ActivityConstructionRunning — the construction agent is in progress.

// ActivityConstructionDone — construction completed (agent finished).

// ActivityConstructionFailed — construction reached a terminal failure.

// ActivityBuildStatus is the finer per-activity build-status lens.

// BuildInConstruction — a construction log exists, work in progress (zero value).

// BuildInReview — a construction log exists without a passing review.

// BuildIntegrated — construction log + a passing review exist.

// BuildFailed — the build reached a terminal failure.

// FailureReason is the closed enum of terminal-failure causes recorded on an
// activity's construction head-state when it reaches ActivityConstructionFailed.

// FailureReasonUnknown is the zero value (no failure recorded).

// PipelineFailed — the construction pipeline reached a terminal FAILURE conclusion.

// PipelineCancelled — the construction pipeline run was cancelled.

// PipelineTimedOut — the construction pipeline timed out / poll budget exhausted.

// VarianceExhausted — the supervision loop exhausted its variance/retry budget.

// EscalationTimedOut — an escalation waited for an override that never came.

// ActivityMethodPhase is one App-A internal phase within a construction activity —
// a canonical lowercase phase-id string (the wire encoding is the value itself). The
// known phase-id constants live in projectstate; project carries the value opaquely.

// ---------------------------------------------------------------------------
// Phase-1 research input (a Method INPUT, not a co-authored artifact).
// ---------------------------------------------------------------------------

// ResearchSource is one named research document/source feeding Phase-1 system design.

// ResearchInput is the Phase-1 research corpus the system-design sequence starts from.

// ---------------------------------------------------------------------------
// Catalog row (ListProjects).
// ---------------------------------------------------------------------------

// ProjectSummary is the catalog row for the landing grid — identity + display fields
// plus the current-phase progress (committed vs total artifact slots).

// ---------------------------------------------------------------------------
// Per-activity git-forward head-state (D-PA-GIT). Provider-opaque: every handle is
// the rail's opaque String() form; CICheck mirrors the rail's CheckState.
// ---------------------------------------------------------------------------

// ActivityGitStatus is the per-activity git-forward head-state record, keyed by
// ActivityID in ProjectState.GitRows.

// ---------------------------------------------------------------------------
// Per-activity construction head-state.
// ---------------------------------------------------------------------------

// ProducedArtifact is one artifact a construction activity produced.

// PhaseCompletion is one App-A internal phase record within an activity's Phases slice.

// ActivityConstructionStatus is the per-activity construction head-state record, keyed
// by ActivityID in ProjectState.ActivityConstruction.

// ConstructionProgress holds the project-level construction tracking framing scalars.

// ---------------------------------------------------------------------------
// Typed service-contract corpus (one per component, keyed by component name).
// ---------------------------------------------------------------------------

// GoField is one field in a Go struct shown in the "Code/interface" view.

// ContractStruct is one Go struct (request or response) carried by an op.

// ContractOp is one operation/method on the service contract.

// ContractParty is one caller or callee in the service contract.

// ContractRevision records one re-cut of the service contract.

// ServiceContract is the typed model for one component's service contract corpus entry.

// ---------------------------------------------------------------------------
// Artifact slot view + the OPAQUE per-slot model envelope.
//
// ArtifactSlotModel is the discriminated {kind, model} envelope the slot's typed
// model is carried as — IDENTICAL on the wire to the systemdesign model envelope
// (so the SPA decodes a slot's model the same way regardless of which read produced
// it). The model is carried OPAQUELY as raw JSON: project never names the concrete
// projectstate model types or the sealed ArtifactModel sum here. Kind is the
// canonical camelCase wire name (e.g. "mission").
// ---------------------------------------------------------------------------

// ArtifactSlotModel is the opaque {kind, model} envelope carrying a slot's typed
// model as raw JSON. Model is omitted when the slot is empty.

// ArtifactSlotView is one artifact slot of the head-state, typed. It carries the
// slot kind (camelCase wire name), its review stage, the opaque model envelope (model
// nil while the slot is empty), and the architect's notes (omitted when empty).

// ---------------------------------------------------------------------------
// Full typed head-state for one project (GetProject).
// ---------------------------------------------------------------------------

// ProjectState is the full typed head-state for one project. Slots holds one
// ArtifactSlotView per defined artifact kind in the stable slot order. GitRows /
// ActivityConstruction / ConstructionProgress / ServiceContracts are the additive
// Phase-3 head-state projections, honest-empty (nil until first populated).

// ---------------------------------------------------------------------------
// The projectManager port — the public use-case surface of the façade. Each op
// leads with the Manager-layer call Context (fwm.Context, embedding context.Context
// + the Principal); the *Manager derives ctx := rc.Context inside.
// ---------------------------------------------------------------------------

// ProjectManager is the projectManager port the webClient depends on. The concrete
// *Manager satisfies it.
