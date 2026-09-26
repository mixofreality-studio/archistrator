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

// --- STAGE 4a ---------------------------------------------------------------
//
// Re-pointed onto the twelve-op deliveryManager exactly as httptransport.go is, and
// deliberately body-for-body identical to it: the two surfaces must speak the same
// twelve ops or R4's cross-surface equivalence is not being tested.

// --- UC1 (system-design / Phase-1) ------------------------------------------

func (t *mcpTransport) CreateProject(ctx context.Context, name string) (string, error) {
	res, err := t.client.DeliveryStartProject(ctx, testOwner, name, nil, nil, nil, false)
	return string(res.ProjectID), sentinelError(err)
}

func (t *mcpTransport) ListProjects(ctx context.Context, owner string) ([]ProjectSummary, error) {
	scope := sdk.OwnerScope(owner)
	view, err := t.client.DeliveryQueryProjectView(ctx, sdk.ProjectViewQuery{
		Kind: sdk.ProjectViewProjects, Owner: &scope,
	})
	if err != nil {
		return nil, sentinelError(err)
	}
	return toProjectSummaries(view.Projects), nil
}

func (t *mcpTransport) SetResearchInput(ctx context.Context, projectID string, sources []ResearchSource) error {
	research := toResearchInput(sources)
	_, err := t.client.DeliveryStartProject(ctx, testOwner, "", &projectID, nil, &research, false)
	return sentinelError(err)
}

func (t *mcpTransport) StartDesign(ctx context.Context, projectID string) (string, error) {
	res, err := t.client.DeliveryStartProject(ctx, testOwner, "", &projectID, nil, nil, true)
	if res.Session == nil {
		return "", sentinelError(err)
	}
	return string(*res.Session), sentinelError(err)
}

func (t *mcpTransport) RequestArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	ref, err := t.client.DeliveryDispatchActivityTask(ctx, sdk.ProjectID(projectID),
		designActivityFor(kind), draftTaskFor(kind), nil)
	return string(ref), sentinelError(err)
}

func (t *mcpTransport) GetSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	view, err := t.querySession(ctx, projectID, kind)
	if err != nil || view.Session == nil {
		// Any non-200 (404 not-yet-started, transient 503, ...) means "not
		// observable yet" to a poller — never fatal here.
		return SessionState{}, false, sentinelError(err)
	}
	s := view.Session
	return SessionState{
		ProjectID:     string(s.ProjectID),
		ArtifactKind:  artifactKindNameOf(s.ArtifactKind),
		Stage:         systemStageName(s.Stage),
		FailureReason: strPtrVal(s.FailureReason),
	}, true, nil
}

func (t *mcpTransport) SubmitReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		designActivityFor(kind), reviewTaskFor(kind),
		sdk.ReviewDecisionInput{Decision: reviewDecision(decision)}, systemFeedback(feedback))
	return sentinelError(err)
}

func (t *mcpTransport) AdvancePhase(ctx context.Context, projectID string) (bool, []string, error) {
	// The phase seal is the ReviewAdvance decision on the architecture activity's gate;
	// the outcome is read back through the summary view, not returned by the write.
	ack := false
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		"architecture", "architectureReview",
		sdk.ReviewDecisionInput{Decision: sdk.ReviewAdvance, AcknowledgeStale: &ack}, nil)
	if err != nil {
		return false, nil, sentinelError(err)
	}
	return t.phaseAdvanced(ctx, projectID, sdk.PhaseProjectDesign)
}

// --- UC2 (project-design / Phase-2) -----------------------------------------

func (t *mcpTransport) RequestProjectArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	ref, err := t.client.DeliveryDispatchActivityTask(ctx, sdk.ProjectID(projectID),
		designActivityFor(kind), draftTaskFor(kind), nil)
	return string(ref), sentinelError(err)
}

func (t *mcpTransport) GetProjectSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	view, err := t.querySession(ctx, projectID, kind)
	if err != nil || view.ProjectSession == nil {
		return SessionState{}, false, sentinelError(err)
	}
	s := view.ProjectSession
	return SessionState{
		ProjectID:     string(s.ProjectID),
		ArtifactKind:  artifactKindNameOf(s.ArtifactKind),
		Stage:         projectStageName(s.Stage),
		FailureReason: strPtrVal(s.FailureReason),
	}, true, nil
}

func (t *mcpTransport) SubmitProjectReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		designActivityFor(kind), reviewTaskFor(kind),
		sdk.ReviewDecisionInput{Decision: reviewDecision(decision)}, projectFeedback(feedback))
	return sentinelError(err)
}

func (t *mcpTransport) RequestSDPCommit(ctx context.Context, projectID string) (string, error) {
	ref, err := t.client.DeliveryDispatchActivityTask(ctx, sdk.ProjectID(projectID),
		"projectDesign", "sdpReview", nil)
	return string(ref), sentinelError(err)
}

func (t *mcpTransport) SubmitSDPDecision(ctx context.Context, projectID, decision, optionID, feedback string) error {
	// optionID is a PATH segment on this route; the ServeMux pattern requires it
	// even for rejectAll (which carries no option) — "-" is the harness's
	// placeholder for "no option". The SDK takes a VALUE sdk.OptionID.
	in := sdk.ReviewDecisionInput{Decision: sdpReviewDecision(decision)}
	if optionID != "" {
		in.OptionID = &optionID
	}
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		"projectDesign", "sdpReview", in, projectFeedback(feedback))
	return sentinelError(err)
}

func (t *mcpTransport) AdvanceToConstruction(ctx context.Context, projectID string) (bool, []string, error) {
	ack := false
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		"projectDesign", "sdpReview",
		sdk.ReviewDecisionInput{Decision: sdk.ReviewAdvance, AcknowledgeStale: &ack}, nil)
	if err != nil {
		return false, nil, sentinelError(err)
	}
	return t.phaseAdvanced(ctx, projectID, sdk.PhaseConstruction)
}

// --- UC3 (construction / Phase-3) -------------------------------------------

func (t *mcpTransport) ExecuteNextActivity(ctx context.Context, projectID, tickID string) (bool, string, error) {
	res, err := t.client.DeliveryExecuteNextActivity(ctx, sdk.ProjectID(projectID), tickID)
	return res.Dispatched, activityIDPtrVal(res.ActivityID), sentinelError(err)
}

func (t *mcpTransport) GetConstructionSessionState(ctx context.Context, projectID, activityID string) (ConstructionSessionState, error) {
	id := activityID
	view, err := t.client.DeliveryQueryProjectView(ctx, sdk.ProjectViewQuery{
		Kind: sdk.ProjectViewSession, ProjectID: &projectID, ActivityID: &id,
	})
	if err != nil || view.ConstructionSession == nil {
		return ConstructionSessionState{}, sentinelError(err)
	}
	return toConstructionSessionState(*view.ConstructionSession), nil
}

func (t *mcpTransport) SubmitPhaseDecision(ctx context.Context, projectID, activityID, phase, decision, feedback string) error {
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID),
		phase, sdk.ReviewDecisionInput{Decision: phaseReviewDecision(decision)}, constructionFeedback(feedback))
	return sentinelError(err)
}

func (t *mcpTransport) UpdateReviewPolicy(ctx context.Context, projectID string, gatedPhasesByType map[string][]string) error {
	policy := sdk.ReviewPolicyInput{GatedPhasesByType: gatedPhasesByType}
	err := t.client.DeliverySetProjectExecutionPolicy(ctx, sdk.ProjectID(projectID),
		sdk.ExecutionPolicyInput{Policy: &policy})
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

func (t *mcpTransport) querySession(ctx context.Context, projectID, kind string) (sdk.ProjectView, error) {
	ak := artifactKind(kind)
	return t.client.DeliveryQueryProjectView(ctx, sdk.ProjectViewQuery{
		Kind: sdk.ProjectViewSession, ProjectID: &projectID, ArtifactKind: &ak,
	})
}

func (t *mcpTransport) phaseAdvanced(ctx context.Context, projectID string, want sdk.Phase) (bool, []string, error) {
	view, err := t.client.DeliveryQueryProjectView(ctx, sdk.ProjectViewQuery{
		Kind: sdk.ProjectViewSummary, ProjectID: &projectID,
	})
	if err != nil || view.Summary == nil {
		return false, nil, sentinelError(err)
	}
	if view.Summary.Phase == want {
		return true, nil, nil
	}
	var missing []string
	for _, slot := range view.Summary.Slots {
		if slot.Stage != sdk.ArtifactStageCommitted {
			missing = append(missing, slot.Kind)
		}
	}
	return false, missing, nil
}

// QueryActivityView reads one activity's whole lifecycle through the twelve-op surface.
func (t *mcpTransport) QueryActivityView(ctx context.Context, projectID, activityID string) (string, error) {
	view, err := t.client.DeliveryQueryActivityView(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID))
	if err != nil {
		return "", sentinelError(err)
	}
	return activityViewStateName(view.State), nil
}

// OverrideActivity delivers the operator's steer through the twelve-op surface.
func (t *mcpTransport) OverrideActivity(ctx context.Context, projectID, activityID string, kind int, notes string) error {
	err := t.client.DeliveryOverrideActivity(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID),
		sdk.ActivityOverride{Kind: sdk.OverrideKind(kind), Notes: notes})
	return sentinelError(err)
}
