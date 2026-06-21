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

	Close() error
}
