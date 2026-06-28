// Package systemdesign is the systemDesignManager component of the aiarch
// server's Manager layer — the use-case façade that drives a project through
// Phase 1 of The Method (System Design), per the senior-passed contract
// designs/aiarch/implementation/contracts/systemDesignManager.md.
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it defines and registers one Activity
// per ResourceAccess call, owns the Signal/Query handlers, and derives the
// idempotency key "${workflowId}:${activityId}" passed down to each RA verb.
// Temporal lives ONLY in this component; the downstream ResourceAccess
// (resourceaccess/*) ports are Temporal-free.
//
// SCHEMA-FIRST (full encapsulation): this component OWNS its contract I/O types.
// The public surface (SystemDesignManager port + the I/O value types) is GENERATED
// into contract.gen.go from contract.schema.json (edit the schema + `make gen`; do
// NOT hand-edit the generated surface). The generated contract imports NEITHER the
// projectstate ResourceAccess NOR Temporal: systemdesign mirrors the head-state
// value shapes (ProjectID / ArtifactKind / ResearchInput / Version) as its OWN
// named types and field-maps from projectstate at the Manager boundary (the
// project/construction/operations Manager precedent). The staged typed DRAFT is
// carried OPAQUELY — a {kind, model} envelope (DraftModel) — so systemdesign never
// regenerates or shares projectstate's sealed ArtifactModel sum or its 17 variants.
//
// The consumer-side dependency interfaces (ConstructionPipelineAccess /
// SourceControlRail), the Temporal Workflows struct + workflow inputs/signals, the
// PM-critique value types (Critique / CritiqueVerdict), and the behavior over the
// contract value types (behavior.go) stay HAND-WRITTEN and are NOT part of the
// generated contract.
//
// File layout within the package (the systemDesignManager component):
//   - systemdesignmanager.go : the Manager + the SystemDesignManager port (§6.2)
//   - contract.go            : the public façade types (§2, §3) — generated surface
//   - behavior.go            : free functions over the contract value types
//   - workflow.go            : the Workflows deps struct + workflow bodies + signal/query handlers (§6.3, §6.6)
//   - activities.go          : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - errors.go              : the port-error -> Temporal-error translation (§6.4)
//   - worker.go              : worker registration of workflows + activities (§6.1)
package systemdesign

import (
	"fmt"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// ---------------------------------------------------------------------------
// Identity / domain scalars (systemdesign's OWN named types — value-identical to
// projectstate; the Manager converts at the projectStateAccess boundary). They are
// PURE DATA on the generated surface; behavior lives in behavior.go as free
// functions so contract.gen.go imports no projectstate.
// ---------------------------------------------------------------------------

// ProjectID is the project aggregate identifier — its value IS the user-supplied
// adopted repo name (name-as-identity). Mirrors projectstate.ProjectID.

// Version is the head-state optimistic-concurrency token returned by SetResearchInput
// (mirrors projectstate.Version; carried so the Client can echo it).

// ArtifactKind is the closed artifact-slot enum. The ordinals MIRROR
// projectstate.ArtifactKind so int(...) conversion at the boundary is
// meaning-preserving; behavior (WireName/IsPhase1/...) lives in behavior.go as free
// functions over a projectstate conversion so the generated type stays pure data.

// ---- Phase 1 ----

// ---- Phase 2 (carried for ordinal parity with projectstate; not driven here) ----

// ---------------------------------------------------------------------------
// Phase-1 research input (a Method INPUT, not a co-authored artifact). Mirrors
// projectstate.ResearchInput / ResearchSource; converted at the boundary.
// ---------------------------------------------------------------------------

// ResearchSource is one named research document/source feeding Phase-1 system design.

// ResearchInput is the Phase-1 research corpus the system-design sequence starts from.

// ---------------------------------------------------------------------------
// Session reference + review surface.
// ---------------------------------------------------------------------------

// SessionRef is an opaque, infrastructure-opaque reference to a running Phase-1
// co-authoring session (systemDesignManager.md §3.1). It wraps the underlying
// durable-execution identity as an opaque string the Client persists/echoes and
// never parses. Construction is via the NewSessionRef free function (behavior.go).

// ReviewDecision is the architect's commit-authority decision at the review gate
// (systemDesignManager.md §3.2).

// commit the typed model in its slot
// loop to draft with feedback
// abandon the draft; project stays at prior phase

// AnchoredComment is one piece of "send back" guidance the architect anchors to a
// JSONPath location in the typed artifact model (systemDesignManager.md §3.2). The
// JSONPath is OPAQUE guidance text — the server does not evaluate it.

// ReviewFeedback is the architect's rejection/withdraw rationale
// (systemDesignManager.md §3.2). Required on Reject; optional on Withdraw; ignored
// on Approve. Comments are consulted ONLY on Reject and woven into the redraft prompt.

// PhaseAdvanceResult is the gating outcome of advancePhase
// (systemDesignManager.md §3.3). A non-Advanced result is the NORMAL "you still owe
// artifacts X, Y" answer (not an error).

// ---------------------------------------------------------------------------
// Session read view (getSessionState) + the OPAQUE staged-draft envelope.
//
// DraftModel is the discriminated {kind, model} envelope the staged typed draft is
// carried as — IDENTICAL on the wire to the project ArtifactSlotModel envelope (so
// the SPA decodes the draft the same way regardless of which read produced it). The
// model is carried OPAQUELY as raw JSON: systemdesign never names the concrete
// projectstate model types or the sealed ArtifactModel sum here.
// ---------------------------------------------------------------------------

// DraftModel is the opaque {kind, model} envelope carrying the staged typed draft as
// raw JSON. Model is omitted when no draft is staged. Kind is the canonical camelCase
// wire name (e.g. "mission").

// SessionStage collapses the technical workflow state into the handful of stages
// the UI needs (systemDesignManager.md §3.4).

// worker dispatched; typed model not yet produced/validated
// model staged (status AwaitingReview); suspended on the review signal
// architect rejected; looping back to draft with feedback
// commitArtifact applied; terminal for this artifactKind
// withdrawArtifact applied; terminal
// worker refused/cancelled and could not produce a model; terminal (§6.3)
// StageDraftFailed is the human-visible, human-actionable stage the session lands
// in when the dispatched agentic DESIGN job reaches a TYPED terminal failure phase
// (PhaseFailed / PhaseCancelled). It carries the job's neutral Diagnostic in
// FailureReason. Surfaced by getSessionState so the SPA renders "your design job
// failed: <diagnostic> — retry or withdraw" and NEVER a perpetual StageDrafting
// spinner (the anti-wedge requirement — the draft-failure-wedge incident).

// SessionStateView is a point-in-time, read-only view of one co-authoring session's
// TECHNICAL progress (systemDesignManager.md §3.4) — the answer to getSessionState (a
// Temporal Query), NOT the business-state read. The staged TYPED draft is carried
// OPAQUELY via DraftModel; Findings explain "why it's being redrafted".

// Draft is the staged typed draft awaiting review, carried as the opaque {kind,
// model} envelope (model nil before the first stage).

// FailureReason is a short, human, non-leaking explanation set ONLY when Stage is
// StageRefused / StageDraftFailed. Empty otherwise.

// ---------------------------------------------------------------------------
// PM-critique value types (systemDesignManager.md §3.6). OWNED by this Manager and
// used ONLY internally (the workflow / readBackCritique) — NOT part of the public
// port surface, so they stay hand-written and are NOT in the generated contract.
// ---------------------------------------------------------------------------

// Critique is the PM-critique result. On Revise the Manager's sequence loops back to
// the architect-role draft step with Notes woven in, BEFORE the human gate.
type Critique struct {
	Verdict CritiqueVerdict `json:"verdict"`
	Notes   string          `json:"notes"`
}

// CritiqueVerdict is the closed PM verdict set.
type CritiqueVerdict int

const (
	CritiqueUnknown CritiqueVerdict = iota
	CritiqueApprove                 // PM ratifies the draft; proceed to the human gate
	CritiqueRevise                  // PM asks for revision; loop back to the draft step with Notes
)

// Validate is the optional mechanical shape hook GenerateTypedData[Critique] runs
// after unmarshal. A Revise verdict must carry Notes; an out-of-range verdict is
// unconstructable.
func (c *Critique) Validate() error {
	switch c.Verdict {
	case CritiqueApprove:
		return nil
	case CritiqueRevise:
		if c.Notes == "" {
			return fmt.Errorf("Critique: Revise verdict requires Notes")
		}
		return nil
	default:
		return fmt.Errorf("Critique: unknown verdict ordinal %d", int(c.Verdict))
	}
}

// ---------------------------------------------------------------------------
// Façade error model (systemDesignManager.md §3.5). CALLER/PROGRAMMER errors at the
// façade boundary — distinct from the workflow's own failure handling.
// ---------------------------------------------------------------------------

// SystemDesignError is the typed façade error — an alias for fwmanager.Error so
// existing errors.As(&SystemDesignError) call sites keep working without change.
type SystemDesignError = fwmanager.Error

func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}
