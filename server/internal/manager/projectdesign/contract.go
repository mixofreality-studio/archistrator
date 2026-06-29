// Package projectdesign is the projectDesignManager component of the aiarch
// server's Manager layer — the use-case façade that drives a project through
// Phase 2 of The Method (Project Design), per the senior-passed contract
// designs/aiarch/implementation/contracts/projectDesignManager.md (D-MPD,
// APPROVED — FROZEN 2026-05-29). It is the Phase-2 TWIN of the systemdesign
// package (D-MSD) and mirrors it file-by-file.
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it defines and registers one Activity
// per ResourceAccess call, owns the Signal/Query handlers, and derives the
// idempotency key "${workflowId}:${activityId}" passed down to each RA verb.
// Temporal lives ONLY in this component; the downstream Engines
// (estimation, operationestimation, settlement) and ResourceAccess (projectstate,
// worker) ports are Temporal-free, and the three estimate Engines are PURE — called
// DIRECTLY from workflow code, never wrapped in an Activity (contract §6.3/§6.4).
//
// SCHEMA-FIRST (full encapsulation): this component OWNS its contract I/O types.
// The public surface (ProjectDesignManager port + the I/O value types) is GENERATED
// into contract.gen.go from contract.schema.json (edit the schema + `make gen`; do
// NOT hand-edit the generated surface). The generated contract imports NEITHER the
// projectstate ResourceAccess NOR Temporal: projectdesign mirrors the head-state
// value shapes (ProjectID / ArtifactKind / OptionID) as its OWN named types and
// field-maps from projectstate at the Manager boundary (the systemdesign precedent).
// The staged typed DRAFT (and the assembled SDP review) is carried OPAQUELY — a
// {kind, model} envelope (DraftModel) — so projectdesign never regenerates or shares
// projectstate's sealed ArtifactModel sum or its 17 variants.
//
// The consumer-side dependency interfaces (ConstructionPipelineAccess /
// SourceControlRail), the Temporal workflows struct + workflow inputs/signals, and
// the internal SDP-assembly (assembleSdpReview over projectstate.Project) stay
// HAND-WRITTEN and are NOT part of the generated contract.
//
// File layout within the package:
//   - projectdesignmanager.go : the Manager + the ProjectDesignManager port (§6.2)
//   - contract.go             : the public façade types (§2, §3) — generated surface
//   - behavior.go             : free functions over the contract value types
//   - workflow.go             : the workflows deps struct + workflow bodies + signal/query handlers (§6.3)
//   - activities.go           : the Manager-owned Activity wrappers, as methods on workflows (§6.4)
//   - errors.go               : the port-error -> Temporal-error translation (§6.4)
//   - prompts.go              : the Phase-2 architect-role draft prompt corpus
//   - worker.go               : worker registration of workflows + activities (§6.1)
package projectdesign

import (
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// ---------------------------------------------------------------------------
// Identity / domain scalars (projectdesign's OWN named types — value-identical to
// projectstate; the Manager converts at the projectStateAccess boundary). They are
// PURE DATA on the generated surface; behavior lives in behavior.go as free
// functions so contract.gen.go imports no projectstate.
// ---------------------------------------------------------------------------

// ProjectID is the project aggregate identifier — its value IS the user-supplied
// adopted repo name (name-as-identity). Mirrors projectstate.ProjectID.

// OptionID names one project-design option in the SDP review (the architect commits
// one at the option-commitment gate). Mirrors projectstate.OptionID.

// ArtifactKind is the closed artifact-slot enum. The ordinals MIRROR
// projectstate.ArtifactKind so int(...) conversion at the boundary is
// meaning-preserving; behavior (WireName/IsPhase2/...) lives in behavior.go as free
// functions over a projectstate conversion so the generated type stays pure data.

// ---- Phase 1 (carried for ordinal parity with projectstate; not driven here) ----

// ---- Phase 2 ----

// ---------------------------------------------------------------------------
// Session reference + review surface.
// ---------------------------------------------------------------------------

// SessionRef is an opaque, infrastructure-opaque reference to a running Phase-2
// session (an artifact-co-authoring session or the SDP-review session — contract
// §3.1). It wraps the underlying durable-execution identity as an opaque string the
// Client persists/echoes and never parses. Construction is via the newSessionRef
// free function (behavior.go).

// ReviewDecision is the architect's commit-authority decision at the per-artifact
// Phase-2 review gate (contract §10 OQ-3 — Phase-2 artifacts ARE individually
// gated, mirroring Phase 1).

// commit the typed model in its slot
// loop back to draft with feedback
// abandon the draft

// ReviewFeedback is the architect's free-text rejection/withdraw rationale
// (contract §3.2). Required on Reject and on an SDP RejectAll; optional on
// Withdraw; ignored on Approve.

// SDPDecision is the architect's decision at the option-commitment gate
// (contract §3.2). Commit binds the named option; RejectAll re-enters Phase 2
// with feedback to produce a fresh SDP review.

// bind the named option, commit the review
// record the rejected outcome; re-assemble with feedback

// PhaseAdvanceResult is the gating outcome of advanceToConstruction
// (contract §3.3). A non-Advanced result is the NORMAL "you still owe artifacts
// X, Y / no option bound" answer (not an error).

// ---------------------------------------------------------------------------
// Session read view (getSessionState) + the OPAQUE staged-draft envelope.
//
// DraftModel is the discriminated {kind, model} envelope the staged typed draft /
// assembled SdpReview is carried as — IDENTICAL on the wire to the systemdesign
// DraftModel envelope. The model is carried OPAQUELY as raw JSON: projectdesign
// never names the concrete projectstate model types or the sealed ArtifactModel sum
// here.
// ---------------------------------------------------------------------------

// DraftModel is the opaque {kind, model} envelope carrying the staged typed draft (or
// the assembled SdpReview) as raw JSON. Model is omitted when no draft is staged.
// Kind is the canonical camelCase wire name (e.g. "planningAssumptions").

// SessionStage collapses the technical workflow state into the handful of stages
// the UI needs (contract §3.4). StageAssemblingSDP sits between drafting and
// awaiting-review for the SDP-review session.

// worker dispatched; typed model not yet produced
// SDP-review workflow: assembling options + joining Engine outputs
// model staged (status AwaitingReview); suspended on the review signal
// architect rejected; looping back with feedback
// commitArtifact applied; terminal for this kind/option
// withdrawArtifact applied; terminal
// worker refused/cancelled and could not produce a model; terminal
// StageDraftFailed (agentic-pivot D-MPD-Δ, §3.4 — the twin of systemDesignManager
// StageDraftFailed) is the human-visible, human-actionable stage the session lands
// in when the dispatched agentic Phase-2 DESIGN job reaches a TYPED terminal failure
// phase. It carries the job's neutral Diagnostic in FailureReason. Surfaced by
// getSessionState so the SPA renders an actionable failure and NEVER a perpetual
// StageDrafting / StageAssemblingSDP spinner (the anti-wedge requirement).

// SessionStateView is a point-in-time, read-only view of one Phase-2 session's
// TECHNICAL progress (contract §3.4) — the answer to getSessionState (a Temporal
// Query), NOT the business-state read. The staged TYPED draft / assembled SdpReview
// is carried OPAQUELY via DraftModel; Findings explain "why it's being redrafted".

// Draft is the staged typed draft / SdpReview awaiting review, carried as the
// opaque {kind, model} envelope (model nil before the first stage).

// FailureReason is a short, human, non-leaking explanation set ONLY when Stage is
// StageDraftFailed (a terminal Phase-2 design-job failure). It gives the SPA a
// message + recovery affordance instead of a wedged "generating" screen. Empty
// (nil) otherwise.

// ---------------------------------------------------------------------------
// Façade error model (projectDesignManager.md §3.5).
// These are CALLER/PROGRAMMER errors at the façade boundary — distinct from the
// workflow's own failure handling. Kinds follow the framework-go standard set.
// ---------------------------------------------------------------------------

func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}
