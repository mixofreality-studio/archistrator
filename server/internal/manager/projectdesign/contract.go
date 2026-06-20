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
// 2026-06-16: the in-process artifactValidation Engine was REMOVED — the Method gate
// moved to framework-go/methodcheck (the seated `go test` in the user repo). This
// Manager surfaces the small Finding value type (relocated locally in findings.go) on
// its session read to explain redrafts; it runs no validation Engine in-process.
//
// The 5 public ops (contract §2):
//   - RequestArtifactDraft   — Workflow (entry, per-artifact CoAuthorPhase2ArtifactWorkflow gate)
//   - RequestSDPCommit       — Workflow (entry, AssembleSDPReviewWorkflow — the UC2 spine)
//   - SubmitSDPDecision      — Signal (sdpDecision, to the SDP-review workflow)
//   - AdvanceToConstruction  — Workflow (entry, short-lived Phase-2 seal)
//   - GetSessionState        — Query (sessionState, read-only)
//
// plus SubmitReviewDecision — Signal (reviewDecision, to the per-artifact gate;
// the OQ-3 per-artifact architect gate, mirroring D-MSD's per-artifact gate).
//
// File layout within the package:
//   - projectdesignmanager.go : the Manager that translates public ops into Temporal client calls (§6.2)
//   - contract.go             : the public façade types + the public ops surface (§2, §3)
//   - workflow.go             : the Workflows deps struct + the three workflow bodies + signal/query handlers (§6.3)
//   - activities.go           : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - errors.go               : the port-error -> Temporal-error translation (§6.4)
//   - prompts.go              : the Phase-2 architect-role draft prompt corpus (the sequence owns the prompts)
//   - worker.go               : worker registration of workflows + activities (§6.1)
package projectdesign

import (
	"encoding/json"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
	fwmanager "github.com/davidmarne/archistrator-platform/framework-go/manager"
)

// ---------------------------------------------------------------------------
// Public data contracts (projectDesignManager.md §3) — the Client surface.
// Infrastructure-opaque: no Temporal id is exposed here. The typed Method models
// (ArtifactModel) and the OptionID are the shared aiarch domain types, referenced
// not redefined.
// ---------------------------------------------------------------------------

// ProjectID / ArtifactKind / Version / OptionID are the shared domain vocabulary;
// re-exported as aliases so the public façade reads as its own surface while
// staying one-source-of-truth with the projectstate package (contract §3.0).
type (
	ProjectID    = projectstate.ProjectID
	ArtifactKind = projectstate.ArtifactKind
	Version      = projectstate.Version
	OptionID     = projectstate.OptionID
)

// SessionRef is an opaque, infrastructure-opaque reference to a running Phase-2
// session (an artifact-co-authoring session or the SDP-review session — contract
// §3.1). It wraps the underlying durable-execution identity without exposing it
// as a Temporal id; the Client persists/echoes it and never parses it. Identical
// shape to systemDesignManager.md §3.1.
type SessionRef struct {
	opaque string
}

// NewSessionRef constructs a SessionRef from an infrastructure identity. Internal
// to the Manager; Clients only ever receive and echo SessionRefs.
func NewSessionRef(opaque string) SessionRef { return SessionRef{opaque: opaque} }

// String returns the canonical printable form (for logs, UI correlation).
func (s SessionRef) String() string { return s.opaque }

// Equal reports value equality.
func (s SessionRef) Equal(other SessionRef) bool { return s.opaque == other.opaque }

// ReviewDecision is the architect's commit-authority decision at the per-artifact
// Phase-2 review gate (contract §10 OQ-3 — Phase-2 artifacts ARE individually
// gated, mirroring Phase 1). Identical to systemDesignManager.md §3.2.
type ReviewDecision int

const (
	ReviewDecisionUnknown ReviewDecision = iota
	ReviewApprove                        // commit the typed model in its slot (projectStateAccess.commitArtifact)
	ReviewReject                         // loop back to draft with feedback (projectStateAccess.rejectArtifact)
	ReviewWithdraw                       // abandon the draft (projectStateAccess.withdrawArtifact)
)

// ReviewFeedback is the architect's free-text rejection/withdraw rationale
// (contract §3.2). Required on Reject and on an SDP RejectAll; optional on
// Withdraw; ignored on Approve.
type ReviewFeedback struct {
	Notes string
}

// SDPDecision is the architect's decision at the option-commitment gate
// (contract §3.2). Commit binds the named option; RejectAll re-enters Phase 2
// with feedback to produce a fresh SDP review.
type SDPDecision int

const (
	SDPDecisionUnknown SDPDecision = iota
	SDPCommit                      // bind the named option, run the commit-time confirmation set, commit the review
	SDPRejectAll                   // record the rejected outcome; re-assemble with feedback woven in
)

// PhaseAdvanceResult is the gating outcome of advanceToConstruction
// (contract §3.3). A non-Advanced result is the NORMAL "you still owe artifacts
// X, Y / no option bound" answer (not an error).
type PhaseAdvanceResult struct {
	Advanced         bool
	MissingArtifacts []ArtifactKind
}

// SessionStage collapses the technical workflow state into the handful of stages
// the UI needs (contract §3.4). StageAssemblingSDP sits between drafting and
// awaiting-review for the SDP-review session.
type SessionStage int

const (
	SessionStageUnknown SessionStage = iota
	StageDrafting                    // worker dispatched; typed model not yet produced
	StageAssemblingSDP               // SDP-review workflow: assembling options + joining the three Engine outputs
	StageAwaitingReview              // model staged (status AwaitingReview); suspended on the review/decision signal
	StageRedrafting                  // architect rejected; looping back with feedback
	StageCommitted                   // commitArtifact applied; terminal for this kind/option
	StageWithdrawn                   // withdrawArtifact applied; terminal
	StageRefused                     // worker refused/cancelled and could not produce a model; terminal
	// StageDraftFailed (agentic-pivot D-MPD-Δ, §3.4 — the twin of systemDesignManager
	// StageDraftFailed) is the human-visible, human-actionable stage the session lands
	// in when the dispatched agentic Phase-2 DESIGN job reaches a TYPED terminal failure
	// phase (PhaseFailed / PhaseCancelled) — drafting failed in the user's CI or the
	// required CI validation check went red. It carries the job's neutral Diagnostic in
	// FailureReason. It is surfaced by getSessionState so the SPA renders an actionable
	// failure and NEVER a perpetual StageDrafting / StageAssemblingSDP spinner (the
	// anti-wedge requirement — memory/project_aiarch_draft_failure_wedge.md). The
	// session suspends on the per-artifact reviewDecision gate awaiting a human
	// Retry (via Reject)/Withdraw. A ran-but-failed job is terminal-at-the-Manager;
	// only transient dispatch errors auto-retry.
	StageDraftFailed
)

// SessionStateView is a point-in-time, read-only view of one Phase-2 session's
// TECHNICAL progress (contract §3.4). It is the answer to getSessionState (a
// Temporal Query), NOT the business-state read. It surfaces the staged TYPED
// draft / assembled SdpReview + structured validation Findings.
type SessionStateView struct {
	ProjectID    ProjectID
	ArtifactKind ArtifactKind
	Stage        SessionStage
	Draft        projectstate.ArtifactModel // the staged typed draft / SdpReview awaiting review; nil before first stage
	Findings     []Finding                  // latest validation findings (so the UI can show "why it's being redrafted")
	// FailureReason is a short, human, non-leaking explanation set ONLY when Stage is
	// StageDraftFailed (a terminal Phase-2 design-job failure — drafting failed in the
	// user's CI or the required CI validation check went red). It gives the SPA a
	// message + recovery affordance instead of a wedged "generating" screen (the
	// draft-failure-wedge incident). Empty otherwise.
	FailureReason string
}

// sessionStateViewWire is the JSON wire form of SessionStateView. Draft is the
// sealed projectstate.ArtifactModel interface, which the default JSON converter
// cannot decode (it does not know the concrete type) — neither over the Temporal
// Query payload nor over the REST transport. It is carried as a discriminated
// envelope (codec.go), keeping the public field the typed interface while
// round-tripping. Identical scheme to systemDesignManager.md §3.4.
type sessionStateViewWire struct {
	ProjectID     ProjectID     `json:"projectId"`
	ArtifactKind  ArtifactKind  `json:"artifactKind"`
	Stage         SessionStage  `json:"stage"`
	Draft         modelEnvelope `json:"draft"`
	Findings      []Finding     `json:"findings,omitempty"`
	FailureReason string        `json:"failureReason,omitempty"`
}

// MarshalJSON encodes the staged typed Draft via the model envelope codec.
func (v SessionStateView) MarshalJSON() ([]byte, error) {
	me, err := encodeModel(v.Draft)
	if err != nil {
		return nil, err
	}
	return json.Marshal(sessionStateViewWire{
		ProjectID:     v.ProjectID,
		ArtifactKind:  v.ArtifactKind,
		Stage:         v.Stage,
		Draft:         me,
		Findings:      v.Findings,
		FailureReason: v.FailureReason,
	})
}

// UnmarshalJSON reconstructs the concrete typed Draft from its envelope.
func (v *SessionStateView) UnmarshalJSON(data []byte) error {
	var w sessionStateViewWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	model, err := w.Draft.decode()
	if err != nil {
		return err
	}
	v.ProjectID = w.ProjectID
	v.ArtifactKind = w.ArtifactKind
	v.Stage = w.Stage
	v.Draft = model
	v.Findings = w.Findings
	v.FailureReason = w.FailureReason
	return nil
}

// ---------------------------------------------------------------------------
// Façade error model (projectDesignManager.md §3.5).
// These are CALLER/PROGRAMMER errors at the façade boundary — distinct from the
// workflow's own failure handling. Kinds follow the framework-go standard set.
// ---------------------------------------------------------------------------

// ProjectDesignError is the typed façade error (contract §3.5). It is an alias
// for fwmanager.Error so errors.As(&ProjectDesignError) call sites and test
// helpers work without change.
type ProjectDesignError = fwmanager.Error

func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}
