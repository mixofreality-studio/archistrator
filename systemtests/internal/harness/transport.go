package harness

import (
	"context"
	"errors"
)

// Sentinel outcomes the wire transports map their protocol-level failures onto,
// so use-case steps assert on SEMANTIC results and ONE step body runs unchanged
// against ANY transport (HTTP today, MCP once mcpClient is built — R4).
var (
	ErrBadRequest      = errors.New("bad request")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	// ErrUnavailable is the sentinel for HTTP 503 (framework-go manager.Infrastructure).
	// The UC4 operations façade maps EVERY workflow-execution failure to this kind at
	// the façade boundary (operationsManager.go we.Get() error branches) regardless of
	// the underlying cause, so a wire test asserts on this ONE sentinel rather than a
	// generic "unexpected status" error.
	ErrUnavailable = errors.New("service unavailable")
)

// ResearchSource is the wire form of one named Phase-1 research document. It
// mirrors the published research-input DTO (api/openapi.yaml) — a value carried
// on the contract, NOT a server internal type.
type ResearchSource struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// SessionState is the transport-agnostic projection of GetSessionState.
type SessionState struct {
	ProjectID    string
	ArtifactKind string
	Stage        string
	// FailureReason is set only when Stage == "draftFailed" — the human-readable
	// "why" (e.g. a read-back state-validation rejection, a terminal job failure).
	// "" otherwise.
	FailureReason string
}

// ProjectSummary is the transport-agnostic projection of one ListProjects row —
// only the fields the wire tests assert on (stored owner + human-readable phase
// label, PM-P2-5/PM-P2-6).
type ProjectSummary struct {
	ProjectID string
	Name      string
	Owner     string
	PhaseName string
}

// ConstructionSessionState is the transport-agnostic projection of constructionManager's
// GetSessionState (ConstructionSessionView) — the per-activity UC3 supervision view.
type ConstructionSessionState struct {
	ProjectID     string
	ActivityID    string
	Stage         string // decoded ConstructionStage ordinal (e.g. "dispatching", "exited")
	PipelinePhase string // decoded PipelinePhase ordinal, "" when absent
}

// DesiredStateChange is the wire form of operationsManager's DesiredStateChange —
// the deploy/republish payload DeployAfterConstruction accepts. Reason and PatchKind
// are the published ordinal enums (operations_handlers.gen.go / operations contract).
type DesiredStateChange struct {
	Reason    string // "deployAfterConstruction" | "operator" | "autoscale" | "delinquency"
	PatchKind string // "fullBundle" | "scale" | "policy"
	ChangeID  string
}

// OperatedSystemView is the transport-agnostic projection of QueryOperatedSystemView —
// only the fields the UC4 wire tests assert on (RuntimeStatusSeam decoded to name).
type OperatedSystemView struct {
	OperatedAppID string
	Phase         string // decoded RuntimeStatusSeam ordinal ("pending"|"healthy"|"degraded"|"withdrawn")
	InFlight      bool
}

// Transport is the transport-agnostic seam over ONE Client surface. The HTTP
// transport drives webClient; a future MCP transport drives mcpClient. The
// cross-surface equivalence test (R4) runs identical steps through both and
// asserts identical committed state.
//
// Every method is BLACK-BOX: it speaks only the published wire contract (routes
// + JSON DTOs, or MCP tools). No method reaches into server internals — the
// harness module cannot import them (Go internal/ seal + depguard + the
// constitution test).
type Transport interface {
	// Name identifies the surface ("http", "mcp") for test diagnostics.
	Name() string

	// CreateProject mints a new project via the catalog and returns its server-
	// assigned projectId. Projects are no longer born implicitly on first phase
	// touch — a UC must create one before driving its phases.
	CreateProject(ctx context.Context, name string) (projectID string, err error)

	// ListProjects lists every project visible to owner, most-recently-updated
	// first — each summary carries the project's CANONICAL STORED owner (not
	// necessarily the enumeration scope echoed back — PM-P2-6) and its
	// human-readable PhaseName ("system-design"|"project-design"|"construction").
	ListProjects(ctx context.Context, owner string) ([]ProjectSummary, error)

	SetResearchInput(ctx context.Context, projectID string, sources []ResearchSource) error
	StartDesign(ctx context.Context, projectID string) (sessionRef string, err error)
	RequestArtifactDraft(ctx context.Context, projectID, artifactKind string) (sessionRef string, err error)
	GetSessionState(ctx context.Context, projectID, artifactKind string) (state SessionState, found bool, err error)
	// SubmitReview delivers a gate decision. feedback is the reject NOTES — the
	// Manager requires it non-empty on "reject" (ignored on approve/withdraw); pass
	// "" for approve/withdraw.
	SubmitReview(ctx context.Context, projectID, artifactKind, decision, feedback string) error
	AdvancePhase(ctx context.Context, projectID string) (advanced bool, missing []string, err error)

	// --- UC2 (project-design / Phase-2) driveDesignPhase intents -------------
	// These drive the projectDesignManager facet (POST .../project-design/...).
	// Same project-scoped shape as the Phase-1 verbs above; identical black-box
	// discipline (published routes + DTOs only).

	// RequestProjectArtifactDraft starts/continues a Phase-2 artifact co-authoring
	// gate (planningAssumptions, activityList, network, the four solutions,
	// riskModel — NOT sdpReview, which is assembled). Returns the session ref.
	RequestProjectArtifactDraft(ctx context.Context, projectID, artifactKind string) (sessionRef string, err error)
	// GetProjectSessionState reads one Phase-2 session's technical view. The kind
	// may be any Phase-2 kind, including "sdpReview".
	GetProjectSessionState(ctx context.Context, projectID, artifactKind string) (state SessionState, found bool, err error)
	// SubmitProjectReview delivers a per-artifact Phase-2 gate decision
	// (approve|reject|withdraw). feedback is the reject NOTES (required on reject).
	SubmitProjectReview(ctx context.Context, projectID, artifactKind, decision, feedback string) error
	// RequestSDPCommit assembles the SDP-review session (the option set + curves).
	RequestSDPCommit(ctx context.Context, projectID string) (sessionRef string, err error)
	// SubmitSDPDecision delivers the architect's SDP gate decision. optionID is
	// required on "commit"; feedback is required on "rejectAll". Pass "" to omit.
	SubmitSDPDecision(ctx context.Context, projectID, decision, optionID, feedback string) error
	// AdvanceToConstruction is the Phase-2 → Phase-3 gate. A non-advanced result
	// (with the missing artifact list) is the NORMAL gating answer, not an error.
	AdvanceToConstruction(ctx context.Context, projectID string) (advanced bool, missing []string, err error)

	// --- UC3 (construction / Phase-3) superviseConstruction intents -----------
	// These drive the constructionManager facet (POST .../construction/...). Only
	// the ops STP-UC3's cases drive (plus UpdateReviewPolicy, the minimal staging
	// op that makes the detailed_design phase gate actually suspend for a human
	// decision) are exposed — constructionManager.OverrideActivity/PauseProject/
	// RunReplanSweep are read (construction_handlers.gen.go) but not wire-driven by
	// any STP-UC3 case, so they are NOT added here.

	// ExecuteNextActivity pumps ONE construction tick: dispatches the next eligible
	// activity (the dependency-network frontier, no unmet predecessors) or reports a
	// quiet tick (dispatched=false) when nothing is eligible. tickID is the caller's
	// firing id — a duplicate tickID must not double-dispatch (RA-idempotency
	// promoted to the client surface, STP-UC3-B1).
	ExecuteNextActivity(ctx context.Context, projectID, tickID string) (dispatched bool, activityID string, err error)
	// GetConstructionSessionState reads the per-activity UC3 supervision view
	// (constructionManager.md ConstructionSessionView). activityID is REQUIRED — the
	// published route has no project-level (nil-activityID) query form.
	GetConstructionSessionState(ctx context.Context, projectID, activityID string) (state ConstructionSessionState, err error)
	// SubmitPhaseDecision delivers the operator's phase-gated approve/send-back
	// decision to the named activity's current phase. feedback is the send-back
	// NOTES — required (non-empty) on "sendBack", ignored on "approve" (pass "").
	SubmitPhaseDecision(ctx context.Context, projectID, activityID, phase, decision, feedback string) error
	// UpdateReviewPolicy stages the per-project ReviewPolicy (which (activityType,
	// phase) pairs require human approval during construction). gatedPhasesByType
	// maps an ActivityType wire name ("service"/"frontend"/"testing"/...) to the
	// canonical phase ids ("detailed_design", "construction", ...) that gate.
	UpdateReviewPolicy(ctx context.Context, projectID string, gatedPhasesByType map[string][]string) error

	// --- UC4 (operations / Phase-4) operateDeliveredSystem + readOperatedSystemView --
	// Only the ops STP-UC4's cases drive; QueryCostProjection (read handlers.gen.go
	// for its shape) is not exercised by any STP-UC4 case, so it is NOT added here.

	// DeployAfterConstruction publishes a desired-state change for an operated app
	// (a full-bundle deploy or a policy/scale republish). operatedAppID is a UUID
	// string; a duplicate changeID must return the SAME revision (STP-UC4-B1).
	DeployAfterConstruction(ctx context.Context, operatedAppID string, change DesiredStateChange) (published bool, revision string, err error)
	// ReconcileOperatedState runs one observe(+autoscale) tick, scoped to appIDs
	// (empty/nil = all in-flight apps). Returns the tick's observed/transitions/
	// republished counts.
	ReconcileOperatedState(ctx context.Context, tickID string, appIDs []string) (observed, transitions, republished int64, err error)
	// QueryOperatedSystemView reads the operator display view for one operated app
	// (phase, in-flight, health, SLOs, autoscaler, run-rate). Side-effect-free.
	QueryOperatedSystemView(ctx context.Context, operatedAppID, requestID string) (view OperatedSystemView, err error)
	// ApplyDelinquencyPolicy delivers the queued cross-Manager delinquency signal
	// (normally settlementManager → operationsManager). pauseNotWithdraw=true pauses
	// (reversible); false withdraws. QUEUED: returns once durably enqueued, not once
	// the enforcement workflow has run.
	ApplyDelinquencyPolicy(ctx context.Context, customerID string, pauseNotWithdraw bool) error
	// WithdrawSystem terminally withdraws an operated app's desired state. Idempotent
	// on the id; a withdrawn app is never resurrected by reconcile (STP-UC4-N2).
	WithdrawSystem(ctx context.Context, operatedAppID, changeID, notes string) (withdrawn bool, err error)

	Close() error
}
