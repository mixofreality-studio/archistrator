package web

import (
	"encoding/json"
	"errors"
	"net/http"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
)

// This file is the HTTP binding for the UC1 driveDesignPhase facet. Each handler
// is a THIN routing facet (webClient.md §0): decode HTTP/JSON → resolve principal
// + authorize (security Utility) → translate to the Manager's typed input →
// invoke EXACTLY ONE systemDesignManager op → shape the reply. No business logic,
// no state, no cross-Manager sequencing.
//
// The workflow ops route through the Manager's typed surface (which owns
// Temporal). All of driveDesignPhase's intents route to systemDesignManager only
// — the one Manager per request rule (webClient.md "Op → Manager fan-out map",
// UC1 row).

// Routes returns the http.Handler for the UC1 webClient surface. authMW (built by
// AuthMiddleware in the composition root) wraps ONLY the authenticated API
// subtree; the liveness/readiness probes are mounted on the outer mux OUTSIDE the
// auth boundary so they answer without a token (the middleware now REJECTS
// unauthenticated requests, so health must not sit behind it). The mux is stdlib
// net/http (Go 1.22+ method+path patterns) — no third-party router (house style).
func (c *Client) Routes(authMW func(http.Handler) http.Handler) http.Handler {
	api := http.NewServeMux()

	// project CATALOG facet — projectManager (flat; projectId minted by the server).
	api.HandleFunc("POST /api/v1/projects", c.handleCreateProject)
	api.HandleFunc("GET /api/v1/projects", c.handleListProjects)
	api.HandleFunc("GET /api/v1/projects/{projectId}", c.handleGetProject)

	// driveDesignPhase facet — Phase-1 (systemDesignManager) intents. Every route is
	// PROJECT-SCOPED: projectId is the path param, never a body field.
	api.HandleFunc("POST /api/v1/projects/{projectId}/system-design/research-input", c.handleSetResearchInput)
	api.HandleFunc("POST /api/v1/projects/{projectId}/system-design/start", c.handleStartSystemDesign)
	api.HandleFunc("POST /api/v1/projects/{projectId}/system-design/artifacts/draft", c.handleRequestArtifactDraft)
	api.HandleFunc("POST /api/v1/projects/{projectId}/system-design/artifacts/review", c.handleSubmitReviewDecision)
	api.HandleFunc("POST /api/v1/projects/{projectId}/system-design/advance", c.handleAdvancePhase)
	api.HandleFunc("GET /api/v1/projects/{projectId}/system-design/sessions/{artifactKind}", c.handleGetSessionState)

	// driveDesignPhase facet — Phase-2 (projectDesignManager) intents (UC2). Same
	// project-scoped shape: projectId in the path.
	api.HandleFunc("POST /api/v1/projects/{projectId}/project-design/artifacts/draft", c.handleRequestProjectArtifactDraft)
	api.HandleFunc("POST /api/v1/projects/{projectId}/project-design/artifacts/review", c.handleSubmitProjectReviewDecision)
	api.HandleFunc("POST /api/v1/projects/{projectId}/project-design/sdp/assemble", c.handleRequestSDPCommit)
	api.HandleFunc("POST /api/v1/projects/{projectId}/project-design/sdp/decision", c.handleSubmitSDPDecision)
	api.HandleFunc("POST /api/v1/projects/{projectId}/project-design/advance", c.handleAdvanceToConstruction)
	api.HandleFunc("GET /api/v1/projects/{projectId}/project-design/sessions/{artifactKind}", c.handleGetProjectSessionState)

	// superviseConstruction facet — Phase-3 (constructionManager) intents (UC3 — the
	// operations console). Project-scoped; the optional activityId is a query param
	// on the session read and a path segment on the override.
	api.HandleFunc("GET /api/v1/projects/{projectId}/construction/session", c.handleGetConstructionSessionState)
	api.HandleFunc("POST /api/v1/projects/{projectId}/construction/begin", c.handleBeginConstruction)
	api.HandleFunc("POST /api/v1/projects/{projectId}/construction/pause", c.handlePauseProject)
	api.HandleFunc("POST /api/v1/projects/{projectId}/construction/activities/{activityId}/override", c.handleOverrideActivity)

	// operateDeliveredSystem facet — UC4 (operationsManager) intents (the operations
	// console). Operated-app-scoped: operatedAppId is the path param. Deploy / Scale /
	// UpdateAutoscalerPolicy all relay to DeployAfterConstruction (a desired-state
	// republish parameterized by patch); Withdraw → WithdrawSystem; cost-projection is
	// a read-only GET → QueryCostProjection.
	api.HandleFunc("POST /api/v1/operations/{operatedAppId}/deploy", c.handleDeploy)
	api.HandleFunc("POST /api/v1/operations/{operatedAppId}/scale", c.handleScale)
	api.HandleFunc("POST /api/v1/operations/{operatedAppId}/autoscaler-policy", c.handleUpdateAutoscalerPolicy)
	api.HandleFunc("POST /api/v1/operations/{operatedAppId}/withdraw", c.handleWithdraw)
	api.HandleFunc("GET /api/v1/operations/{operatedAppId}/cost-projection", c.handleQueryCostProjection)
	// readOperatedSystemView facet — Contract 2 (IWebReadModel; operationsRead-ruling.md
	// §C). The composing operator read view that backs U-SPA-4. Read-only GET →
	// QueryOperatedSystemView.
	api.HandleFunc("GET /api/v1/operations/{operatedAppId}/view", c.handleGetOperatedSystemView)

	root := http.NewServeMux()
	// Liveness/readiness — unauthenticated probes, OUTSIDE the auth boundary.
	root.HandleFunc("GET /healthz", c.handleHealth)
	root.HandleFunc("GET /readyz", c.handleHealth)
	// GET /api/userinfo — the SPA's session probe (GTD parity). Behind the auth
	// boundary so the edge-forwarded bearer token is validated into a principal;
	// the shared framework handler then returns that principal's identity claims.
	// A 200 tells the SPA it has a live session; a 401 (Middleware, for an
	// absent/invalid token) makes the SPA reload to trigger the edge OIDC redirect.
	root.Handle("GET /api/userinfo", authMW(http.HandlerFunc(security.UserInfoHandler)))
	// Everything under /api/v1/ is authenticated. The api mux's patterns carry the
	// full /api/v1/... path, so no prefix stripping is needed.
	root.Handle("/api/v1/", authMW(api))
	return root
}

// --- driveDesignPhase handlers ---------------------------------------------

// handleSetResearchInput — driveDesignPhase{SetResearchInput}. Routes to the
// Manager's SYNC, non-Temporal SetResearchInput op (a new intent WITHIN the
// driveDesignPhase facet — webClient.md Amendment A2; IWebEntry op count
// unchanged). This is the step BEFORE start: it records the ResearchInput Method
// INPUT so the project can satisfy StartSystemDesign's precondition. 204 No
// Content on success (a pure precondition write — no resource ref to return,
// consistent with submitReviewDecision's 204; the frozen OpenAPI contract for
// this path is 204). The Manager's resulting Version is discarded on the wire;
// the SPA re-reads head-state to reflect the saved input.
func (c *Client) handleSetResearchInput(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req setResearchInputRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	research, err := req.toResearchInput()
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", projectID.String()) {
		return
	}
	if _, err := c.systemDesign.SetResearchInput(r.Context(), projectID, research); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleStartSystemDesign — driveDesignPhase{StartSystemDesign}. Routes to
// systemDesignManager.StartSystemDesign (Workflow).
func (c *Client) handleStartSystemDesign(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", projectID.String()) {
		return
	}
	ref, err := c.systemDesign.StartSystemDesign(r.Context(), projectID)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sessionRefResponse{SessionRef: ref.String()})
}

// handleRequestArtifactDraft — driveDesignPhase{RequestArtifactDraft}. Routes to
// systemDesignManager.RequestArtifactDraft (Workflow).
func (c *Client) handleRequestArtifactDraft(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req requestArtifactDraftRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	kind, err := parseArtifactKind(req.ArtifactKind)
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", projectID.String()) {
		return
	}
	var feedback *systemdesign.ReviewFeedback
	if req.Feedback != nil {
		feedback = &systemdesign.ReviewFeedback{Notes: *req.Feedback}
	}
	ref, err := c.systemDesign.RequestArtifactDraft(r.Context(), projectID, kind, feedback)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sessionRefResponse{SessionRef: ref.String()})
}

// handleSubmitReviewDecision — driveDesignPhase{SubmitReviewDecision}. Routes to
// systemDesignManager.SubmitReviewDecision (Signal).
func (c *Client) handleSubmitReviewDecision(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	var req submitReviewDecisionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	kind, err := parseArtifactKind(req.ArtifactKind)
	if err != nil {
		writeClientError(w, err)
		return
	}
	decision, err := parseReviewDecision(req.Decision)
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "approve-artifact", projectID.String()) {
		return
	}
	feedback := req.toReviewFeedback()
	if err := c.systemDesign.SubmitReviewDecision(r.Context(), projectID, kind, decision, feedback); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdvancePhase — driveDesignPhase{AdvancePhase}. Routes to
// systemDesignManager.AdvancePhase (Workflow). A non-Advanced result is the
// NORMAL gating answer (HTTP 200), not an error.
func (c *Client) handleAdvancePhase(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", projectID.String()) {
		return
	}
	result, err := c.systemDesign.AdvancePhase(r.Context(), projectID)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	missing := make([]string, 0, len(result.MissingArtifacts))
	for _, k := range result.MissingArtifacts {
		missing = append(missing, artifactKindName(k))
	}
	writeJSON(w, http.StatusOK, phaseAdvanceResponse{Advanced: result.Advanced, MissingArtifacts: missing})
}

// handleGetSessionState — driveDesignPhase polling relayed to
// systemDesignManager.GetSessionState (Query). webClient.md folds polling into
// the design-phase activity rather than a separate facet (no property-like op).
func (c *Client) handleGetSessionState(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	kind, err := parseArtifactKind(r.PathValue("artifactKind"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "drive-phase", projectID.String()) {
		return
	}
	view, err := c.systemDesign.GetSessionState(r.Context(), projectID, kind)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessionStateResponse{
		ProjectID:    projectID.String(),
		ArtifactKind: artifactKindName(kind),
		Stage:        sessionStageName(view.Stage),
		View:         view,
	})
}

// --- project catalog handlers ----------------------------------------------

// handleCreateProject — project catalog{CreateProject}. Routes to
// projectManager.CreateProject. NAME-AS-IDENTITY (C-PM-Δ): the projectId IS the
// user-supplied repo name in the request body (the user creates the empty repo
// first; aiarch adopts it), so the body carries {name}; the owner is derived from
// the authenticated principal, NOT supplied by the caller — a project belongs to its
// creator. Returns 201 with the {projectId} (== the repo name) the SPA then scopes
// its subsequent phase routes under. (2026-06-15 correction: no agenticToken field —
// the CLAUDE_CODE_OAUTH_TOKEN is user-provisioned via the Claude Code GitHub App.)
func (c *Client) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	principal, ok := security.PrincipalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !c.authorizeCatalog(w, r, "create-project", principal) {
		return
	}
	var req createProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id, err := c.project.CreateProject(projectCtx(r), ownerScopeFor(principal), req.Name)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createProjectResponse{ProjectID: string(id)})
}

// handleListProjects — project catalog{ListProjects}. Routes to
// projectManager.ListProjects scoped to the authenticated principal's owner key, so
// the landing grid shows only the caller's own projects.
func (c *Client) handleListProjects(w http.ResponseWriter, r *http.Request) {
	principal, ok := security.PrincipalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return
	}
	if !c.authorizeCatalog(w, r, "list-projects", principal) {
		return
	}
	summaries, err := c.project.ListProjects(projectCtx(r), ownerScopeFor(principal))
	if err != nil {
		writeManagerError(w, err)
		return
	}
	rows := make([]projectSummaryResponse, 0, len(summaries))
	for _, s := range summaries {
		rows = append(rows, projectSummaryFromManager(s))
	}
	writeJSON(w, http.StatusOK, rows)
}

// handleGetProject — project catalog{GetProject}. Routes to
// projectManager.GetProject for the path-scoped projectId, returning the full typed
// head-state (every artifact slot's stage + typed model via the shared envelope).
func (c *Client) handleGetProject(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r.PathValue("projectId"))
	if err != nil {
		writeClientError(w, err)
		return
	}
	if !c.authorizeProject(w, r, "read-project", projectID.String()) {
		return
	}
	state, err := c.project.GetProject(projectCtx(r), project.ProjectID(projectID.String()))
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c.projectStateFromManager(state))
}

// projectCtx builds the Manager-layer call Context for a projectManager op from an
// HTTP request: the request context plus the authenticated principal (if the auth
// middleware put one on the context; the zero principal is a safe stopgap). Mirrors
// constructionCtx (construction.go) / operationsCtx (operations.go).
func projectCtx(r *http.Request) fwmanager.Context {
	rc := fwmanager.Context{Context: r.Context()}
	if p, ok := security.PrincipalFrom(r.Context()); ok {
		rc.Principal = p
	}
	return rc
}

func (c *Client) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// --- security gate ---------------------------------------------------------

// authorizeProject runs the cross-cutting pre-step every facet shares
// (webClient.md): read the principal the auth middleware validated onto the
// context, then authorize the intent on the project resource. Returns true to
// proceed; on failure it writes the response (401 authN-absent / 403 deny) and
// returns false. FAIL-CLOSED: a security error is treated as deny (Authorize
// contract). The 401 branch is defense in depth — the middleware already rejects
// unauthenticated API requests before any handler runs.
func (c *Client) authorizeProject(w http.ResponseWriter, r *http.Request, verb, projectID string) bool {
	principal, ok := security.PrincipalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return false
	}
	decision, err := c.security.Authorize(r.Context(), principal,
		security.Action{Verb: verb},
		security.ResourceRef{Kind: "project", ID: projectID})
	if err != nil || !decision.Permit {
		// Fail-closed: an Authorize error means DENY, never permit.
		writeError(w, http.StatusForbidden, "forbidden", "not permitted")
		return false
	}
	return true
}

// authorizeCatalog authorizes a catalog-scoped intent (createProject / listProjects)
// that targets no single project yet. It authorizes on the per-owner catalog
// resource (Kind "projectCatalog", ID the owner key) so a Cedar policy can later
// scope catalog actions per owner. The principal is resolved by the caller and
// passed in (the catalog handlers also need it to derive the OwnerScope). Returns
// true to proceed; on Deny/error it writes 403 (fail-closed) and returns false.
func (c *Client) authorizeCatalog(w http.ResponseWriter, r *http.Request, verb string, principal security.SecurityPrincipal) bool {
	decision, err := c.security.Authorize(r.Context(), principal,
		security.Action{Verb: verb},
		security.ResourceRef{Kind: "projectCatalog", ID: string(ownerScopeFor(principal))})
	if err != nil || !decision.Permit {
		writeError(w, http.StatusForbidden, "forbidden", "not permitted")
		return false
	}
	return true
}

// ownerScopeFor derives the project catalog OwnerScope from the authenticated
// principal. The stable opaque Subject ("sub") is the owner key — it never changes
// across token refreshes or email/username edits, so a project stays bound to its
// creator's identity. The Email is the fallback only when a token carried no sub
// (defensive; the validator normally guarantees a subject). This is the single
// place the owner key is derived; the catalog handlers never trust a body field.
func ownerScopeFor(p security.SecurityPrincipal) project.OwnerScope {
	if p.Subject != "" {
		return project.OwnerScope(p.Subject)
	}
	return project.OwnerScope(p.Email)
}

// --- response helpers ------------------------------------------------------

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, errorResponse{Error: detail, Code: code})
}

// writeClientError maps a transport-level parse error to HTTP 400.
func writeClientError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadRequest, "bad_request", err.Error())
}

// writeManagerError maps a systemDesignManager.Error (framework-go manager.Error)
// to its HTTP status. The Manager error model is already non-leaking; the detail
// is caller-safe.
func writeManagerError(w http.ResponseWriter, err error) {
	var me *fwmanager.Error
	if errors.As(err, &me) {
		writeError(w, statusForKind(me.Kind), codeForKind(me.Kind), me.Detail)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", "internal error")
}

func statusForKind(kind fwmanager.Kind) int {
	switch kind {
	case fwmanager.ContractMisuse:
		return http.StatusBadRequest
	case fwmanager.NotFound:
		return http.StatusNotFound
	case fwmanager.Unauthorized:
		return http.StatusForbidden
	case fwmanager.FailedPrecondition:
		return http.StatusConflict
	case fwmanager.Infrastructure:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// codeForKind maps a framework-go manager.Kind to the stable, snake_case error
// code the OpenAPI ErrorResponse.code enum documents (api/openapi.yaml). The raw
// fwmanager.Kind.String() is Pascal-case ("ContractMisuse") and is NOT in the
// documented enum, and would clash with the snake_case transport-error codes
// (bad_request/unauthenticated/forbidden/internal) emitted elsewhere — so the
// wire code is normalized here. The generated TS client sees only the documented
// codes.
func codeForKind(kind fwmanager.Kind) string {
	switch kind {
	case fwmanager.ContractMisuse:
		return "contract_misuse"
	case fwmanager.NotFound:
		return "not_found"
	case fwmanager.Unauthorized:
		return "forbidden"
	case fwmanager.FailedPrecondition:
		return "failed_precondition"
	case fwmanager.Infrastructure:
		return "infrastructure"
	default:
		return "internal"
	}
}
