package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
)

// constructionCtx builds the Manager-layer call Context for a constructionManager op
// from an HTTP request: the request context plus the authenticated principal (if the
// auth middleware put one on the context; the zero principal is a safe stopgap for the
// scheduler-style paths).
func constructionCtx(r *http.Request) fwm.Context {
	rc := fwm.Context{Context: r.Context()}
	if p, ok := security.PrincipalFrom(r.Context()); ok {
		rc.Principal = p
	}
	return rc
}

// This file is the HTTP binding for the UC3 superviseConstruction facet (the
// operations console). Each handler is a THIN routing facet (webClient.md §0):
// decode HTTP/JSON → resolve principal + authorize (security Utility) → translate
// to the Manager's typed input → invoke EXACTLY ONE constructionManager op → shape
// the reply. No business logic, no state, no cross-Manager sequencing. Mirrors
// projectdesign.go (Phase 2) and handlers.go (Phase 1).
//
// The three console intents route to constructionManager only (the one-Manager-
// per-request rule): GetSessionState (Query), PauseProject (Signal), OverrideActivity
// (Signal). The scheduler-driven pump/sweep Workflows are NOT console-driven and
// have no route here.

// --- request DTOs ----------------------------------------------------------

// pauseProjectRequest delivers an operator pause (Signal operatorPauseRequested).
// The project is the {projectId} path segment, not a body field. Reason is
// required (the Manager rejects an empty reason as ContractMisuse).
type pauseProjectRequest struct {
	Reason string `json:"reason"`
}

// overrideActivityRequest delivers the operator's manual steer on one activity
// (Signal operatorOverride). The project + activity are the {projectId}/{activityId}
// path segments. Kind is one of takeover|retry|skip|reassign; notes is free-text.
type overrideActivityRequest struct {
	Kind  string `json:"kind"`
	Notes string `json:"notes,omitempty"`
}

// --- response DTOs ---------------------------------------------------------

// constructionSessionStateResponse is the read-only technical view of one
// construction session (GetSessionState Query). It carries the typed
// ConstructionSessionView so the SPA can render the tracker (stage), the
// interventions tab (variance), and the artifacts tab (reviewSet / pipeline phase).
// The Stage / PipelinePhase enums are rendered onto stable wire strings; the typed
// view is also embedded verbatim for the SPA's discriminated rendering.
type constructionSessionStateResponse struct {
	ProjectID     string                               `json:"projectId"`
	ActivityID    *string                              `json:"activityId,omitempty"`
	Stage         string                               `json:"stage"`
	PipelinePhase *string                              `json:"pipelinePhase,omitempty"`
	View          construction.ConstructionSessionView `json:"view"`
}

// --- handlers --------------------------------------------------------------

// handleGetConstructionSessionState — superviseConstruction{GetSessionState}.
// Routes to constructionManager.GetSessionState (Query). The optional activityId
// query parameter selects the per-activity child session; absent it returns the
// project-level supervision view.
func (c *Client) handleGetConstructionSessionState(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var activityID *construction.ActivityID
	if a := r.URL.Query().Get("activityId"); a != "" {
		aid := construction.ActivityID(a)
		activityID = &aid
	}
	if !c.authorizeProject(w, r, "read-project", string(projectID)) {
		return
	}
	view, err := c.construction.GetSessionState(constructionCtx(r), construction.ProjectID(projectID), activityID)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	resp := constructionSessionStateResponse{
		ProjectID: string(projectID),
		Stage:     constructionStageName(view.Stage),
		View:      view,
	}
	if view.ActivityID != nil {
		aid := string(*view.ActivityID)
		resp.ActivityID = &aid
	}
	if view.PipelinePhase != nil {
		phase := constructionPipelinePhaseName(*view.PipelinePhase)
		resp.PipelinePhase = &phase
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePauseProject — superviseConstruction{PauseProject}. Routes to
// constructionManager.PauseProject (Signal). 204 No Content once the signal is
// durably enqueued (the pause branch runs asynchronously inside the workflow).
func (c *Client) handlePauseProject(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req pauseProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", string(projectID)) {
		return
	}
	if err := c.construction.PauseProject(constructionCtx(r), construction.ProjectID(projectID), req.Reason); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleOverrideActivity — superviseConstruction{OverrideActivity}. Routes to
// constructionManager.OverrideActivity (Signal). 204 No Content once the signal is
// durably enqueued; the operator's steer feeds the SAME decide→execute machinery as
// the automatic variance path.
func (c *Client) handleOverrideActivity(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	activityID := r.PathValue("activityId")
	if activityID == "" {
		writeClientError(w, fmt.Errorf("activityId is required"))
		return
	}
	var req overrideActivityRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	kind, err := parseOverrideKind(req.Kind)
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", string(projectID)) {
		return
	}
	override := construction.ActivityOverride{Kind: kind, Notes: req.Notes}
	if err := c.construction.OverrideActivity(constructionCtx(r), construction.ProjectID(projectID), construction.ActivityID(activityID), override); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// beginConstructionResponse acknowledges that the pump tick was accepted. The cascade
// runs ASYNCHRONOUSLY (the pump self-cascades over the committed network via
// ContinueAsNew); the SPA polls the project read for the per-activity construction
// status as activities flip eligible→in-construction→integrated.
type beginConstructionResponse struct {
	Accepted bool   `json:"accepted"`
	TickID   string `json:"tickId"`
}

// handleBeginConstruction — the console's manual pump trigger ("Begin construction").
// Routes to constructionManager.ExecuteNextActivity (Workflow), which starts the pump
// and self-cascades. Because the cascade drains the WHOLE network before
// ExecuteNextActivity returns (the pump follows ContinueAsNew), the relay is
// FIRE-AND-FORGET: a detached goroutine drives the cascade while this handler returns
// 202 Accepted immediately, so the SPA can poll the project read and watch the tracker
// animate. (The pump is normally scheduler-triggered with no request principal, so the
// detached context mirrors that path.)
func (c *Client) handleBeginConstruction(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", string(projectID)) {
		return
	}
	// A time-bucketed tick id collapses a double-click WITHIN THE SAME MINUTE onto the
	// in-flight pump (the Manager starts the pump with WORKFLOW_ID_CONFLICT_POLICY
	// USE_EXISTING), while a deliberate re-begin in a later minute gets a fresh id and
	// starts a new pump run (the old fixed "console-begin" id would forever collide with
	// a drained/closed run of the same id, blocking re-begin).
	tickID := fmt.Sprintf("console-begin-%d", time.Now().Unix()/60)
	go func() {
		res, err := c.construction.ExecuteNextActivity(fwm.Context{Context: context.Background()}, construction.ProjectID(projectID), tickID)
		if err != nil {
			slog.Error("construction begin pump returned an error", "projectId", string(projectID), "err", err)
			return
		}
		slog.Info("construction begin pump cascade quiesced", "projectId", string(projectID), "dispatched", res.Dispatched)
	}()
	writeJSON(w, http.StatusAccepted, beginConstructionResponse{Accepted: true, TickID: tickID})
}

// --- transport mappings ----------------------------------------------------

// parseOverrideKind maps the wire kind string to the typed OverrideKind. An unknown
// kind is a client error (the Manager would also reject it as ContractMisuse).
func parseOverrideKind(s string) (construction.OverrideKind, error) {
	switch s {
	case "takeover":
		return construction.OverrideTakeover, nil
	case "retry":
		return construction.OverrideRetry, nil
	case "skip":
		return construction.OverrideSkip, nil
	case "reassign":
		return construction.OverrideReassign, nil
	default:
		return construction.OverrideUnknown, fmt.Errorf("kind %q must be one of takeover|retry|skip|reassign", s)
	}
}

// constructionStageName renders the technical ConstructionStage onto the wire.
func constructionStageName(stage construction.ConstructionStage) string {
	switch stage {
	case construction.StageDispatching:
		return "dispatching"
	case construction.StagePipelineRunning:
		return "pipelineRunning"
	case construction.StageReviewing:
		return "reviewing"
	case construction.StageAwaitingTakeover:
		return "awaitingTakeover"
	case construction.StagePaused:
		return "paused"
	case construction.StageExited:
		return "exited"
	default:
		return "unknown"
	}
}

// constructionPipelinePhaseName renders the technical PipelinePhase onto the wire.
func constructionPipelinePhaseName(phase construction.PipelinePhase) string {
	switch phase {
	case construction.PipelinePending:
		return "pending"
	case construction.PipelineRunning:
		return "running"
	case construction.PipelineSucceeded:
		return "succeeded"
	case construction.PipelineFailed:
		return "failed"
	case construction.PipelineCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}
