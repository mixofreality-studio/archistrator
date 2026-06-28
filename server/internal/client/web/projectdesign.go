package web

import (
	"fmt"
	"net/http"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// This file is the HTTP binding for the UC2 driveDesignPhase (Phase-2) facet. Each
// handler is a THIN routing facet (webClient.md §0): decode HTTP/JSON → resolve
// principal + authorize (security Utility) → translate to the Manager's typed input
// → invoke EXACTLY ONE projectDesignManager op → shape the reply. No business
// logic, no state, no cross-Manager sequencing. Mirrors handlers.go (Phase 1).

// --- request DTOs ----------------------------------------------------------

// requestProjectArtifactDraftRequest starts/continues a Phase-2 artifact
// co-authoring gate. artifactKind is one of the eight DRAFTABLE Phase-2 kinds
// (NOT sdpReview — that is assembled via /sdp/assemble).
type requestProjectArtifactDraftRequest struct {
	ArtifactKind string  `json:"artifactKind"`
	Feedback     *string `json:"feedback,omitempty"`
}

// submitProjectReviewDecisionRequest delivers the architect's per-artifact gate
// decision for a Phase-2 artifact (Signal reviewDecision, OQ-3). The project is the
// {projectId} path segment, not a body field.
type submitProjectReviewDecisionRequest struct {
	ArtifactKind string  `json:"artifactKind"`
	Decision     string  `json:"decision"` // "approve" | "reject" | "withdraw"
	Feedback     *string `json:"feedback,omitempty"`
}

// submitSDPDecisionRequest delivers the architect's SDP decision (Signal
// sdpDecision). optionId is required on commit; feedback is required on rejectAll.
// The project is the {projectId} path segment, not a body field.
type submitSDPDecisionRequest struct {
	Decision string  `json:"decision"` // "commit" | "rejectAll"
	OptionID *string `json:"optionId,omitempty"`
	Feedback *string `json:"feedback,omitempty"`
}

// projectSessionStateResponse is the read-only technical view of one Phase-2
// session (getSessionState Query).
type projectSessionStateResponse struct {
	ProjectID    string                         `json:"projectId"`
	ArtifactKind string                         `json:"artifactKind"`
	Stage        string                         `json:"stage"`
	View         projectdesign.SessionStateView `json:"view"`
}

// --- handlers --------------------------------------------------------------

// handleRequestProjectArtifactDraft — driveDesignPhase{Phase2 RequestArtifactDraft}.
// Routes to projectDesignManager.RequestArtifactDraft (Workflow).
func (c *Client) handleRequestProjectArtifactDraft(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req requestProjectArtifactDraftRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	kind, err := parseProjectDesignKind(req.ArtifactKind, false)
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", string(projectID)) {
		return
	}
	var feedback *projectdesign.ReviewFeedback
	if req.Feedback != nil {
		feedback = &projectdesign.ReviewFeedback{Notes: *req.Feedback}
	}
	ref, err := c.projectDesign.RequestArtifactDraft(r.Context(), projectdesign.ProjectID(projectID), kind, feedback)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sessionRefResponse{SessionRef: ref.String()})
}

// handleSubmitProjectReviewDecision — driveDesignPhase{Phase2 SubmitReviewDecision}.
// Routes to projectDesignManager.SubmitReviewDecision (per-artifact Signal).
func (c *Client) handleSubmitProjectReviewDecision(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req submitProjectReviewDecisionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	kind, err := parseProjectDesignKind(req.ArtifactKind, false)
	if err != nil {
		writeClientError(w, err)
		return
	}
	decision, err := parseProjectReviewDecision(req.Decision)
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "approve-artifact", string(projectID)) {
		return
	}
	var feedback *projectdesign.ReviewFeedback
	if req.Feedback != nil {
		feedback = &projectdesign.ReviewFeedback{Notes: *req.Feedback}
	}
	if err := c.projectDesign.SubmitReviewDecision(r.Context(), projectdesign.ProjectID(projectID), kind, decision, feedback); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRequestSDPCommit — driveDesignPhase{RequestSDPCommit}. Routes to
// projectDesignManager.RequestSDPCommit (Workflow).
func (c *Client) handleRequestSDPCommit(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", string(projectID)) {
		return
	}
	ref, err := c.projectDesign.RequestSDPCommit(r.Context(), projectdesign.ProjectID(projectID))
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sessionRefResponse{SessionRef: ref.String()})
}

// handleSubmitSDPDecision — driveDesignPhase{SubmitSDPDecision}. Routes to
// projectDesignManager.SubmitSDPDecision (Signal). Commit binds the named option;
// RejectAll re-enters Phase 2 with feedback.
func (c *Client) handleSubmitSDPDecision(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req submitSDPDecisionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	decision, err := parseSDPDecision(req.Decision)
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "approve-artifact", string(projectID)) {
		return
	}
	var optionID *projectdesign.OptionID
	if req.OptionID != nil {
		oid := projectdesign.OptionID(*req.OptionID)
		optionID = &oid
	}
	var feedback *projectdesign.ReviewFeedback
	if req.Feedback != nil {
		feedback = &projectdesign.ReviewFeedback{Notes: *req.Feedback}
	}
	if err := c.projectDesign.SubmitSDPDecision(r.Context(), projectdesign.ProjectID(projectID), decision, optionID, feedback); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdvanceToConstruction — driveDesignPhase{AdvanceToConstruction}. Routes to
// projectDesignManager.AdvanceToConstruction (Workflow). A non-Advanced result is
// the NORMAL gating answer (HTTP 200), not an error.
func (c *Client) handleAdvanceToConstruction(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", string(projectID)) {
		return
	}
	result, err := c.projectDesign.AdvanceToConstruction(r.Context(), projectdesign.ProjectID(projectID))
	if err != nil {
		writeManagerError(w, err)
		return
	}
	missing := make([]string, 0, len(result.MissingArtifacts))
	for _, k := range result.MissingArtifacts {
		missing = append(missing, projectDesignKindName(k))
	}
	writeJSON(w, http.StatusOK, phaseAdvanceResponse{Advanced: result.Advanced, MissingArtifacts: missing})
}

// handleGetProjectSessionState — Phase-2 polling relayed to
// projectDesignManager.GetSessionState (Query). The artifactKind path segment may
// be any Phase-2 kind, including "sdpReview" (the SDP-review session).
func (c *Client) handleGetProjectSessionState(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	kind, err := parseProjectDesignKind(r.PathValue("artifactKind"), true)
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", string(projectID)) {
		return
	}
	view, err := c.projectDesign.GetSessionState(r.Context(), projectdesign.ProjectID(projectID), kind)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectSessionStateResponse{
		ProjectID:    string(projectID),
		ArtifactKind: projectDesignKindName(kind),
		Stage:        projectSessionStageName(view.Stage),
		View:         view,
	})
}

// --- transport mappings ----------------------------------------------------

// phase2KindByName maps the wire artifactKind string to the typed Phase-2
// ArtifactKind. The nine Phase-2 kinds are accepted on this surface; an unknown or
// non-Phase-2 name is a client error.
var phase2KindByName = map[string]projectdesign.ArtifactKind{
	"planningAssumptions":  projectstate.KindPlanningAssumptions,
	"activityList":         projectstate.KindActivityList,
	"network":              projectstate.KindNetwork,
	"normalSolution":       projectstate.KindNormalSolution,
	"decompressedSolution": projectstate.KindDecompressedSolution,
	"subcriticalSolution":  projectstate.KindSubcriticalSolution,
	"compressedSolution":   projectstate.KindCompressedSolution,
	"riskModel":            projectstate.KindRiskModel,
	"sdpReview":            projectstate.KindSdpReview,
}

// projectDesignKindWireName is the inverse of phase2KindByName for response shaping.
var projectDesignKindWireName = func() map[projectdesign.ArtifactKind]string {
	m := make(map[projectdesign.ArtifactKind]string, len(phase2KindByName))
	for name, kind := range phase2KindByName {
		m[kind] = name
	}
	return m
}()

// parseProjectDesignKind maps a wire artifactKind to the typed Phase-2 kind. When
// allowSDPReview is false the "sdpReview" kind is rejected (it is assembled via
// /sdp/assemble, not co-authored as a draftable artifact).
func parseProjectDesignKind(s string, allowSDPReview bool) (projectdesign.ArtifactKind, error) {
	kind, ok := phase2KindByName[s]
	if !ok {
		return 0, fmt.Errorf("artifactKind %q is not a recognized Phase-2 kind", s)
	}
	if !allowSDPReview && kind == projectstate.KindSdpReview {
		return 0, fmt.Errorf("artifactKind %q is assembled via /sdp/assemble, not co-authored as a draft", s)
	}
	return kind, nil
}

// projectDesignKindName renders a Phase-2 ArtifactKind onto the wire.
func projectDesignKindName(kind projectdesign.ArtifactKind) string {
	if name, ok := projectDesignKindWireName[kind]; ok {
		return name
	}
	return kind.String()
}

// parseProjectReviewDecision maps the wire decision string to the typed Phase-2
// ReviewDecision (the per-artifact co-authoring gate).
func parseProjectReviewDecision(s string) (projectdesign.ReviewDecision, error) {
	switch s {
	case "approve":
		return projectdesign.ReviewApprove, nil
	case "reject":
		return projectdesign.ReviewReject, nil
	case "withdraw":
		return projectdesign.ReviewWithdraw, nil
	default:
		return projectdesign.ReviewDecisionUnknown, fmt.Errorf("decision %q must be one of approve|reject|withdraw", s)
	}
}

// parseSDPDecision maps the wire decision string to the typed SDPDecision (the
// option-commitment gate).
func parseSDPDecision(s string) (projectdesign.SDPDecision, error) {
	switch s {
	case "commit":
		return projectdesign.SDPCommit, nil
	case "rejectAll":
		return projectdesign.SDPRejectAll, nil
	default:
		return projectdesign.SDPDecisionUnknown, fmt.Errorf("decision %q must be one of commit|rejectAll", s)
	}
}

// projectSessionStageName renders the technical Phase-2 SessionStage onto the wire.
func projectSessionStageName(stage projectdesign.SessionStage) string {
	switch stage {
	case projectdesign.StageDrafting:
		return "drafting"
	case projectdesign.StageAssemblingSDP:
		return "assemblingSdp"
	case projectdesign.StageAwaitingReview:
		return "awaitingReview"
	case projectdesign.StageRedrafting:
		return "redrafting"
	case projectdesign.StageCommitted:
		return "committed"
	case projectdesign.StageWithdrawn:
		return "withdrawn"
	case projectdesign.StageRefused:
		return "refused"
	case projectdesign.StageDraftFailed:
		// The human-visible, human-actionable terminal-failure stage (the Phase-2 twin
		// of systemdesign's StageDraftFailed): the dispatched agentic Phase-2 design job
		// reached a TYPED terminal failure phase. Surfaced as a DISTINCT wire stage (NOT
		// "unknown") so the SPA shows a retry/withdraw affordance, never a perpetual
		// "drafting" spinner (D-MPD-Δ §3.4, the anti-wedge requirement).
		return "draftFailed"
	default:
		return "unknown"
	}
}
