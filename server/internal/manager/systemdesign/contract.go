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
// 2026-06-16: the in-process artifactValidation Engine was REMOVED — the Method gate
// moved to framework-go/methodcheck (the seated `go test` in the user repo). This
// Manager no longer runs validation in-process; it surfaces the small Finding value
// type (relocated locally in findings.go) on its session read to explain redrafts.
//
// 2026-05-26 typed-models re-cut (systemDesignManager.md §0): the workflow spine
// now threads TYPED projectstate.ArtifactModels end to end — the worker returns a typed
// model (no bytes, no parse in the Manager), artifactAccess is GONE from this
// Manager, and artifactRenderingAccess is the new RA dep that produces the
// human-facing view the architect reviews. The four public ops and their
// Workflow/Signal/Query classification are preserved.
//
// File layout within the package (the systemDesignManager component):
//   - systemdesignmanager.go : the Manager that translates public ops into Temporal client calls (§6.2)
//   - contract.go            : the public façade types + the four public ops (§2, §3)
//   - workflow.go            : the Workflows deps struct + workflow bodies + signal/query handlers (§6.3, §6.6)
//   - activities.go          : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - errors.go              : the port-error -> Temporal-error translation (§6.4)
//   - worker.go              : worker registration of workflows + activities (§6.1)
package systemdesign

import (
	"encoding/json"
	"fmt"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ---------------------------------------------------------------------------
// Public data contracts (systemDesignManager.md §3) — the Client surface.
// Infrastructure-opaque: no Temporal id is exposed here. The typed Method models
// (ArtifactModel) are the shared aiarch domain types, referenced not redefined.
// ---------------------------------------------------------------------------

// ProjectID and ArtifactKind are the shared domain vocabulary; re-exported as
// aliases so the public façade reads as its own surface while staying
// one-source-of-truth with the domain package (systemDesignManager.md §3.0).
// ProjectID is a string newtype (name-as-identity, the adopted repo name);
// ArtifactKind is the closed slot enum.
type (
	ProjectID    = projectstate.ProjectID
	ArtifactKind = projectstate.ArtifactKind
)

// ResearchInput / ResearchSource are the Phase-1 research-corpus Method INPUT
// (systemDesignManager.md §2.6, canonical: projectStateAccess.md §3.8). Re-exported
// as aliases so the SetResearchInput op reads in the Manager's own surface terms
// AND so a Client (webClient) can depend on the value type via the Manager surface
// — a legal Client→Manager value-type dependency on a shared domain value, NOT a
// Client→ResourceAccess call. One source of truth: the canonical definition stays
// in projectstate.
type (
	ResearchInput  = projectstate.ResearchInput
	ResearchSource = projectstate.ResearchSource
)

// Version is the head-state optimistic-concurrency token (projectStateAccess.md
// §3.0), re-exported so the SetResearchInput op's resulting-Version return reads on
// the Manager surface and a Client can echo it without importing projectstate.
type Version = projectstate.Version

// SessionRef is an opaque, infrastructure-opaque reference to a running Phase-1
// co-authoring session (systemDesignManager.md §3.1). It wraps the underlying
// durable-execution identity without exposing it as a Temporal id; the Client
// persists/echoes it and never parses it.
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

// ReviewDecision is the architect's commit-authority decision at the review gate
// (systemDesignManager.md §3.2).
type ReviewDecision int

const (
	ReviewDecisionUnknown ReviewDecision = iota
	ReviewApprove                        // commit the typed model in its slot (projectStateAccess.commitArtifact)
	ReviewReject                         // loop to draftMethodArtifact with feedback (projectStateAccess.rejectArtifact)
	ReviewWithdraw                       // abandon the draft; project stays at prior phase (projectStateAccess.withdrawArtifact)
)

// AnchoredComment is one piece of "send back" guidance the architect anchors to a
// location in the typed artifact model (systemDesignManager.md §3.2). JSONPath is
// carried as OPAQUE guidance text this round — the server does NOT evaluate or
// validate it; it references a path into one of the Phase-1 typed models
// (projectstate.models_phase1) and is woven verbatim into the redraft prompt so
// the architect-role worker knows WHERE the comment applies.
type AnchoredComment struct {
	// JSONPath is a path into the typed artifact model the comment refers to,
	// e.g. "$.vision" or "$.objectives[0].statement". Opaque to the server.
	JSONPath string
	// Text is the architect's comment at that location.
	Text string
}

// ReviewFeedback is the architect's rejection/withdraw rationale
// (systemDesignManager.md §3.2). Required on Reject; optional on Withdraw;
// ignored on Approve. Notes is the free-text rationale; Comments is the optional
// set of JSONPath-anchored comments, consulted ONLY on Reject and woven into the
// architect-role redraft prompt beneath the Notes.
type ReviewFeedback struct {
	Notes    string
	Comments []AnchoredComment
}

// PhaseAdvanceResult is the gating outcome of advancePhase
// (systemDesignManager.md §3.3). A non-Advanced result is the NORMAL "you still
// owe artifacts X, Y" answer (not an error).
type PhaseAdvanceResult struct {
	Advanced         bool
	MissingArtifacts []ArtifactKind
}

// Critique is the PM-critique result (systemDesignManager.md §3.6, 2026-05-29).
// It is OWNED by this Manager — the generic worker is merely parameterized over
// it via workerAccess.GenerateTypedData[Critique] (the worker owns no Method /
// Phase-1 types). On Revise the Manager's sequence loops back to the architect-
// role draft step with Notes woven in, BEFORE the human gate; the PM never blocks
// the human.
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
// after unmarshal (workerAccess.md §3f). A Revise verdict must carry Notes; an
// out-of-range verdict is unconstructable. A shape failure surfaces as a
// worker.UnmarshalError (the worker produced a non-Critique), routed through
// intervention — NOT a semantic verdict.
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

// SessionStage collapses the technical workflow state into the handful of stages
// the UI needs (systemDesignManager.md §3.4).
type SessionStage int

const (
	SessionStageUnknown SessionStage = iota
	StageDrafting                    // worker dispatched; typed model not yet produced/validated
	StageAwaitingReview              // model staged (status AwaitingReview); suspended on the review signal
	StageRedrafting                  // architect rejected; looping back to draftMethodArtifact with feedback
	StageCommitted                   // commitArtifact applied; terminal for this artifactKind
	StageWithdrawn                   // withdrawArtifact applied; terminal
	StageRefused                     // worker refused/cancelled and could not produce a model; terminal (§6.3)
	// StageDraftFailed (agentic-pivot D-MSD-Δ, §3.4) is the human-visible,
	// human-actionable stage the session lands in when the dispatched agentic DESIGN
	// job reaches a TYPED terminal failure phase (PhaseFailed / PhaseCancelled) —
	// drafting failed in the user's CI or the required CI validation check went red.
	// It carries the job's neutral Diagnostic in FailureReason. It is surfaced by
	// getSessionState so the SPA renders "your design job failed: <diagnostic> —
	// retry or withdraw" and NEVER a perpetual StageDrafting spinner (the anti-wedge
	// requirement — the draft-failure-wedge incident). The session suspends on the
	// existing reviewDecision gate awaiting a human Retry (via Reject/redraft) or
	// Withdraw. A ran-but-failed job is terminal-at-the-Manager; only transient
	// dispatch errors auto-retry.
	StageDraftFailed
)

// SessionStateView is a point-in-time, read-only view of one co-authoring
// session's TECHNICAL progress (systemDesignManager.md §3.4). It is the answer to
// getSessionState (a Temporal Query), NOT the business-state read. The re-cut
// surfaces the staged TYPED draft + structured validation Findings (replacing the
// old DraftRef *ArtifactRef).
type SessionStateView struct {
	ProjectID    ProjectID
	ArtifactKind ArtifactKind
	Stage        SessionStage
	Draft        projectstate.ArtifactModel // the staged typed draft awaiting review; nil before first stage
	Findings     []Finding                  // latest validation findings (so the UI can show "why it's being redrafted")
	// FailureReason is a short, human, non-leaking explanation set ONLY when Stage
	// is StageRefused (a terminal worker fault — e.g. the AI worker is out of
	// credits). It gives the SPA a message + recovery affordance instead of a wedged
	// "generating" screen (Bug B, prod incident 2026-06-01). Empty otherwise.
	FailureReason string
}

// sessionStateViewWire is the JSON wire form of SessionStateView. Draft is the
// sealed projectstate.ArtifactModel interface, which the default JSON converter cannot
// decode (it does not know the concrete type) — neither over the Temporal Query
// payload nor over the REST transport. It is carried as a discriminated envelope
// (codec.go), keeping the public field the typed interface while round-tripping.
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
// Façade error model (systemDesignManager.md §3.5).
// These are CALLER/PROGRAMMER errors at the façade boundary — distinct from the
// workflow's own failure handling (Temporal RetryPolicy + the validation/refusal
// alternative paths inside the workflow body). Kinds follow the framework-go
// standard set; see fwmanager.Kind for the enumeration.
// ---------------------------------------------------------------------------

// SystemDesignError is the typed façade error (systemDesignManager.md §3.5).
// It is an alias for fwmanager.Error so existing errors.As(&SystemDesignError)
// call sites and test helpers keep working without change.
type SystemDesignError = fwmanager.Error

func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}
