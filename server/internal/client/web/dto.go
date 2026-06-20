package web

import (
	"fmt"

	"github.com/davidmarne/archistrator/server/internal/manager/systemdesign"
	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// This file holds the HTTP/JSON wire DTOs for the UC1 driveDesignPhase facet and
// the transport-level enum<->string mappings. These are the Client's transport
// binding — deliberately NOT on the systemDesignManager surface (which speaks
// typed Go values). The OpenAPI spec (openapi.yaml) is the source of truth for
// these shapes; they are kept faithful to it.

// --- request DTOs ----------------------------------------------------------

// requestArtifactDraftRequest starts/continues a co-authoring gate for one
// Method artifact. The project is identified by the {projectId} path segment, not a
// body field. Feedback is optional (woven into the next draft on a re-entry).
type requestArtifactDraftRequest struct {
	ArtifactKind string  `json:"artifactKind"`
	Feedback     *string `json:"feedback,omitempty"`
}

// submitReviewDecisionRequest delivers the architect's gate decision (Signal). The
// project is the {projectId} path segment, not a body field. Comments are the
// optional JSONPath-anchored "send back" comments, consulted by the Manager ONLY
// on a "reject" decision and woven into the redraft prompt beneath the feedback.
type submitReviewDecisionRequest struct {
	ArtifactKind string               `json:"artifactKind"`
	Decision     string               `json:"decision"` // "approve" | "reject" | "withdraw"
	Feedback     *string              `json:"feedback,omitempty"`
	Comments     []anchoredCommentDTO `json:"comments,omitempty"`
}

// anchoredCommentDTO is the wire form of systemdesign.AnchoredComment — one comment
// anchored to a JSONPath location in the typed artifact model. The jsonPath is
// opaque guidance text this round (the server does not evaluate it).
type anchoredCommentDTO struct {
	JSONPath string `json:"jsonPath"`
	Text     string `json:"text"`
}

// toReviewFeedback maps the request's feedback + comments onto the typed
// systemdesign.ReviewFeedback, or returns nil when neither is present (so the
// Manager sees "no feedback" rather than an empty struct).
func (req submitReviewDecisionRequest) toReviewFeedback() *systemdesign.ReviewFeedback {
	if req.Feedback == nil && len(req.Comments) == 0 {
		return nil
	}
	fb := &systemdesign.ReviewFeedback{}
	if req.Feedback != nil {
		fb.Notes = *req.Feedback
	}
	for _, c := range req.Comments {
		fb.Comments = append(fb.Comments, systemdesign.AnchoredComment{
			JSONPath: c.JSONPath,
			Text:     c.Text,
		})
	}
	return fb
}

// setResearchInputRequest records the Phase-1 ResearchInput Method INPUT so the
// project can satisfy StartSystemDesign's ResearchInput-present precondition
// (systemDesignManager.md §2.6; openapi.yaml SetResearchInputRequest). The project
// is the {projectId} path segment; the body is the research corpus (≥1 named source).
type setResearchInputRequest struct {
	Research researchInputDTO `json:"research"`
}

// researchInputDTO is the wire form of systemdesign.ResearchInput.
type researchInputDTO struct {
	Sources []researchSourceDTO `json:"sources"`
}

// researchSourceDTO is the wire form of systemdesign.ResearchSource — one named
// research document feeding Phase-1 (title + corpus content).
type researchSourceDTO struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// toResearchInput maps the wire DTO to the typed systemdesign.ResearchInput value
// (a Manager-surface alias for projectstate.ResearchInput — a value-type dependency
// on the Manager surface, not a Client→ResourceAccess call). It validates the same
// shape the OpenAPI schema requires: ≥1 source, each with non-empty title+content.
// A shape violation is a transport-level client error (clean 400) that the Manager
// would also reject as ContractMisuse.
func (req setResearchInputRequest) toResearchInput() (systemdesign.ResearchInput, error) {
	if len(req.Research.Sources) == 0 {
		return systemdesign.ResearchInput{}, fmt.Errorf("research.sources must contain at least one source")
	}
	sources := make([]systemdesign.ResearchSource, 0, len(req.Research.Sources))
	for i, s := range req.Research.Sources {
		if s.Title == "" {
			return systemdesign.ResearchInput{}, fmt.Errorf("research.sources[%d].title is required", i)
		}
		if s.Content == "" {
			return systemdesign.ResearchInput{}, fmt.Errorf("research.sources[%d].content is required", i)
		}
		sources = append(sources, systemdesign.ResearchSource{Title: s.Title, Content: s.Content})
	}
	return systemdesign.ResearchInput{Sources: sources}, nil
}

// --- response DTOs ---------------------------------------------------------

// sessionRefResponse echoes the opaque SessionRef the Manager returned. The
// Client never parses it; it is a continuity token the SPA persists/echoes.
type sessionRefResponse struct {
	SessionRef string `json:"sessionRef"`
}

// phaseAdvanceResponse is the gating outcome of advancePhase. A non-Advanced
// result is the NORMAL "you still owe artifacts X, Y" answer (HTTP 200).
type phaseAdvanceResponse struct {
	Advanced         bool     `json:"advanced"`
	MissingArtifacts []string `json:"missingArtifacts"`
}

// sessionStateResponse is the read-only technical view of one co-authoring
// session (getSessionState Query). Draft is carried as raw JSON via the
// SessionStateView's own discriminated codec (the Manager's MarshalJSON).
type sessionStateResponse struct {
	ProjectID    string                        `json:"projectId"`
	ArtifactKind string                        `json:"artifactKind"`
	Stage        string                        `json:"stage"`
	View         systemdesign.SessionStateView `json:"view"`
}

// errorResponse is the uniform JSON error envelope. Detail is a non-leaking,
// caller-safe message (the Manager error model already keeps policy/PII detail
// out of the surface).
type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// --- transport mappings ----------------------------------------------------

// parseProjectID reads the wire projectId. NAME-AS-IDENTITY (C-PM-Δ, 2026-06-15):
// the projectId IS the adopted repo name — a plain string identity, carried
// verbatim, no longer a UUID. An empty value is a client error.
func parseProjectID(s string) (systemdesign.ProjectID, error) {
	if s == "" {
		return "", fmt.Errorf("projectId is required")
	}
	return systemdesign.ProjectID(s), nil
}

// phase1KindByName maps the wire artifactKind string to the typed Phase-1
// ArtifactKind. Only the seven Phase-1 kinds are accepted on this UC1 surface;
// an unknown or non-Phase-1 name is a client error (the Manager would also
// reject it, but rejecting at the transport edge gives a clean 400).
var phase1KindByName = map[string]systemdesign.ArtifactKind{
	"mission":              projectstate.KindMission,
	"glossary":             projectstate.KindGlossary,
	"scrubbedRequirements": projectstate.KindScrubbedRequirements,
	"volatilities":         projectstate.KindVolatilities,
	"coreUseCases":         projectstate.KindCoreUseCases,
	"system":               projectstate.KindSystem,
	"operationalConcepts":  projectstate.KindOperationalConcepts,
	"standardCheck":        projectstate.KindStandardCheck,
}

// kindWireName is the inverse of phase1KindByName for response shaping.
var kindWireName = func() map[systemdesign.ArtifactKind]string {
	m := make(map[systemdesign.ArtifactKind]string, len(phase1KindByName))
	for name, kind := range phase1KindByName {
		m[kind] = name
	}
	return m
}()

// parseArtifactKind maps a wire artifactKind to the typed Phase-1 kind.
func parseArtifactKind(s string) (systemdesign.ArtifactKind, error) {
	kind, ok := phase1KindByName[s]
	if !ok {
		return 0, fmt.Errorf("artifactKind %q is not a recognized Phase-1 kind", s)
	}
	return kind, nil
}

// artifactKindName renders an ArtifactKind onto the wire (falls back to the
// kind's String() for non-Phase-1 kinds, which never appear on this surface).
func artifactKindName(kind systemdesign.ArtifactKind) string {
	if name, ok := kindWireName[kind]; ok {
		return name
	}
	return kind.String()
}

// parseReviewDecision maps the wire decision string to the typed ReviewDecision.
func parseReviewDecision(s string) (systemdesign.ReviewDecision, error) {
	switch s {
	case "approve":
		return systemdesign.ReviewApprove, nil
	case "reject":
		return systemdesign.ReviewReject, nil
	case "withdraw":
		return systemdesign.ReviewWithdraw, nil
	default:
		return systemdesign.ReviewDecisionUnknown, fmt.Errorf("decision %q must be one of approve|reject|withdraw", s)
	}
}

// sessionStageName renders the technical SessionStage onto the wire.
func sessionStageName(stage systemdesign.SessionStage) string {
	switch stage {
	case systemdesign.StageDrafting:
		return "drafting"
	case systemdesign.StageAwaitingReview:
		return "awaitingReview"
	case systemdesign.StageRedrafting:
		return "redrafting"
	case systemdesign.StageCommitted:
		return "committed"
	case systemdesign.StageWithdrawn:
		return "withdrawn"
	case systemdesign.StageRefused:
		return "refused"
	case systemdesign.StageDraftFailed:
		// The human-visible, human-actionable terminal-failure stage the session lands
		// in when the dispatched agentic DESIGN job reaches a TYPED terminal failure
		// phase (PhaseFailed/PhaseCancelled — drafting failed in the user's CI or the
		// required CI validation check went red). Surfaced as a DISTINCT wire stage (NOT
		// "unknown") so the SPA renders "your design job failed — retry or withdraw" and
		// never a perpetual "drafting" spinner (the anti-wedge requirement, D-MSD-Δ §3.4 /
		// the draft-failure-wedge incident).
		return "draftFailed"
	default:
		return "unknown"
	}
}
