package harness

// mcptransport.go is the MCP twin of httptransport.go — it drives the SAME
// published Client surface over the streamable-HTTP MCP mount (/mcp) instead of
// the REST routes, and is the R4 cross-surface-equivalence transport: runUC1
// (and any other transport-agnostic flow) runs unchanged against either.
//
// Like its HTTP sibling it is now a THIN delegate over the generated client SDK
// (internal/sdk). sdk.MCPClient owns the whole streamable-HTTP JSON-RPC/SSE
// machinery (initialize → notifications/initialized → tools/call, one SSE
// "message" event per response) the hand transport used to carry inline; each
// method here forwards to the matching sdk.MCPClient op — whose Go signature is
// byte-identical to the sdk.HTTPClient op — and maps the SDK's structured
// *sdk.MCPToolError (parsed from the isError "<Kind>: <Detail>" text) onto the
// SAME transport sentinels httptransport maps HTTP status codes onto (see
// sentinelError). The generated MCP client speaks only the published tool
// names + JSON argument/result shapes, never a server internal type.

import (
	"context"
	"net/http"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/sdk"
)

// mcpTransport drives the generated MCP tool surface over streamable HTTP.
type mcpTransport struct {
	client *sdk.MCPClient
}

// NewMCPTransport binds a black-box MCP transport to a running server's base
// URL. The session is established lazily on the first call (inside the SDK:
// initialize + notifications/initialized), mirroring NewHTTPTransport's
// zero-argument construction. Bearer is empty (dev-auth).
func NewMCPTransport(baseURL string) Transport {
	return &mcpTransport{client: &sdk.MCPClient{BaseURL: baseURL, HTTP: &http.Client{}}}
}

func (t *mcpTransport) Name() string { return "mcp" }

func (t *mcpTransport) Close() error { return nil }

// --- UC1 (system-design / Phase-1) ------------------------------------------

func (t *mcpTransport) CreateProject(ctx context.Context, name string) (string, error) {
	id, err := t.client.SystemDesignCreateProject(ctx, testOwner, name)
	return string(id), sentinelError(err)
}

func (t *mcpTransport) ListProjects(ctx context.Context, owner string) ([]ProjectSummary, error) {
	rows, err := t.client.SystemDesignListProjects(ctx, sdk.OwnerScope(owner))
	if err != nil {
		return nil, sentinelError(err)
	}
	return toProjectSummaries(rows), nil
}

func (t *mcpTransport) SetResearchInput(ctx context.Context, projectID string, sources []ResearchSource) error {
	_, err := t.client.SystemDesignSetResearchInput(ctx, sdk.ProjectID(projectID), toResearchInput(sources))
	return sentinelError(err)
}

func (t *mcpTransport) StartDesign(ctx context.Context, projectID string) (string, error) {
	ref, err := t.client.SystemDesignStartSystemDesign(ctx, sdk.ProjectID(projectID))
	return string(ref), sentinelError(err)
}

func (t *mcpTransport) RequestArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	ref, err := t.client.SystemDesignRequestArtifactDraft(ctx, sdk.ProjectID(projectID), artifactKind(kind), nil)
	return string(ref), sentinelError(err)
}

func (t *mcpTransport) GetSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	view, err := t.client.SystemDesignGetSessionState(ctx, sdk.ProjectID(projectID), artifactKind(kind))
	if err != nil {
		return SessionState{}, false, sentinelError(err)
	}
	return SessionState{
		ProjectID:     string(view.ProjectID),
		ArtifactKind:  artifactKindNameOf(view.ArtifactKind),
		Stage:         systemStageName(view.Stage),
		FailureReason: strPtrVal(view.FailureReason),
	}, true, nil
}

func (t *mcpTransport) SubmitReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	err := t.client.SystemDesignSubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		artifactKind(kind), reviewDecision(decision), systemFeedback(feedback))
	return sentinelError(err)
}

func (t *mcpTransport) AdvancePhase(ctx context.Context, projectID string) (bool, []string, error) {
	res, err := t.client.SystemDesignAdvancePhase(ctx, sdk.ProjectID(projectID), false)
	return res.Advanced, decodeMissingArtifacts(res.MissingArtifacts), sentinelError(err)
}

// --- UC2 (project-design / Phase-2) -----------------------------------------

func (t *mcpTransport) RequestProjectArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	ref, err := t.client.ProjectDesignRequestArtifactDraft(ctx, sdk.ProjectID(projectID), artifactKind(kind), nil)
	return string(ref), sentinelError(err)
}

func (t *mcpTransport) GetProjectSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	view, err := t.client.ProjectDesignGetSessionState(ctx, sdk.ProjectID(projectID), artifactKind(kind))
	if err != nil {
		return SessionState{}, false, sentinelError(err)
	}
	return SessionState{
		ProjectID:     string(view.ProjectID),
		ArtifactKind:  artifactKindNameOf(view.ArtifactKind),
		Stage:         projectStageName(view.Stage),
		FailureReason: strPtrVal(view.FailureReason),
	}, true, nil
}

func (t *mcpTransport) SubmitProjectReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	err := t.client.ProjectDesignSubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		artifactKind(kind), reviewDecision(decision), projectFeedback(feedback))
	return sentinelError(err)
}

func (t *mcpTransport) RequestSDPCommit(ctx context.Context, projectID string) (string, error) {
	ref, err := t.client.ProjectDesignRequestSDPCommit(ctx, sdk.ProjectID(projectID))
	return string(ref), sentinelError(err)
}

func (t *mcpTransport) SubmitSDPDecision(ctx context.Context, projectID, decision, optionID, feedback string) error {
	// Same "-" no-option placeholder policy as the HTTP transport: the tool's
	// optionID input is a required path-mirror on the server; pass the VALUE
	// sdk.OptionID placeholder when the SDP decision carries no option.
	seg := optionID
	if seg == "" {
		seg = "-"
	}
	err := t.client.ProjectDesignSubmitSDPDecision(ctx, sdk.ProjectID(projectID),
		sdpDecision(decision), sdk.OptionID(seg), projectFeedback(feedback))
	return sentinelError(err)
}

func (t *mcpTransport) AdvanceToConstruction(ctx context.Context, projectID string) (bool, []string, error) {
	res, err := t.client.ProjectDesignAdvanceToConstruction(ctx, sdk.ProjectID(projectID), false)
	return res.Advanced, decodeMissingArtifacts(res.MissingArtifacts), sentinelError(err)
}

// --- UC3 (construction / Phase-3) -------------------------------------------

func (t *mcpTransport) ExecuteNextActivity(ctx context.Context, projectID, tickID string) (bool, string, error) {
	res, err := t.client.ConstructionExecuteNextActivity(ctx, sdk.ProjectID(projectID), tickID)
	return res.Dispatched, activityIDPtrVal(res.ActivityID), sentinelError(err)
}

func (t *mcpTransport) GetConstructionSessionState(ctx context.Context, projectID, activityID string) (ConstructionSessionState, error) {
	view, err := t.client.ConstructionGetSessionState(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID))
	if err != nil {
		return ConstructionSessionState{}, sentinelError(err)
	}
	return toConstructionSessionState(view), nil
}

func (t *mcpTransport) SubmitPhaseDecision(ctx context.Context, projectID, activityID, phase, decision, feedback string) error {
	err := t.client.ConstructionSubmitPhaseDecision(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID),
		phase, phaseDecision(decision), constructionFeedback(feedback))
	return sentinelError(err)
}

func (t *mcpTransport) UpdateReviewPolicy(ctx context.Context, projectID string, gatedPhasesByType map[string][]string) error {
	err := t.client.ConstructionUpdateReviewPolicy(ctx, sdk.ProjectID(projectID),
		sdk.ReviewPolicyInput{GatedPhasesByType: gatedPhasesByType})
	return sentinelError(err)
}

// --- UC4 (operations / Phase-4) ---------------------------------------------

func (t *mcpTransport) DeployAfterConstruction(ctx context.Context, operatedAppID string, change DesiredStateChange) (bool, string, error) {
	res, err := t.client.OperationsDeployAfterConstruction(ctx, operatedAppID, toDesiredStateChange(change))
	return res.Published, strPtrVal(res.Revision), sentinelError(err)
}

func (t *mcpTransport) ReconcileOperatedState(ctx context.Context, tickID string, appIDs []string) (int64, int64, int64, error) {
	res, err := t.client.OperationsReconcileOperatedState(ctx, tickID, reconcileScope(appIDs))
	return res.Observed, res.Transitions, res.Republished, sentinelError(err)
}

func (t *mcpTransport) QueryOperatedSystemView(ctx context.Context, operatedAppID, requestID string) (OperatedSystemView, error) {
	view, err := t.client.OperationsQueryOperatedSystemView(ctx, operatedAppID, requestID)
	if err != nil {
		return OperatedSystemView{}, sentinelError(err)
	}
	return OperatedSystemView{
		OperatedAppID: view.OperatedAppID,
		Phase:         runtimeStatusName(view.Phase),
		InFlight:      view.InFlight,
	}, nil
}

func (t *mcpTransport) ApplyDelinquencyPolicy(ctx context.Context, customerID string, pauseNotWithdraw bool) error {
	err := t.client.OperationsApplyDelinquencyPolicy(ctx, customerID,
		sdk.DelinquencyContext{PauseNotWithdraw: pauseNotWithdraw})
	return sentinelError(err)
}

func (t *mcpTransport) WithdrawSystem(ctx context.Context, operatedAppID, changeID, notes string) (bool, error) {
	res, err := t.client.OperationsWithdrawSystem(ctx, operatedAppID, changeID, sdk.WithdrawReason{Notes: notes})
	return res.Withdrawn, sentinelError(err)
}
