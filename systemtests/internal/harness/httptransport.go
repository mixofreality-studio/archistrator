package harness

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/sdk"
)

// httpTransport drives the webClient HTTP surface. It is now a THIN delegate
// over the generated, self-contained client SDK (internal/sdk, emitted by
// server/cmd/appgen → transportgen): every method forwards to the matching
// sdk.HTTPClient op, encodes/decodes enum names via enums.go, and maps the
// SDK's structured *sdk.APIError onto the transport-agnostic sentinels the
// use-case steps assert on. The hand-rolled route/body/decode logic (and the 11
// wire enum ordinal tables) it used to carry now live in the SDK — the harness
// no longer restates the wire contract, it consumes the generated mirror of it.
//
// Black-box discipline is unchanged: the SDK is stdlib-only, zero-import, and
// carries only the published contract (routes + DTOs), never a server internal
// type. The MCP twin (mcptransport.go) delegates to sdk.MCPClient over the same
// per-op Go signatures, so the R4 cross-surface equivalence property holds by
// construction.
type httpTransport struct {
	client *sdk.HTTPClient
}

// NewHTTPTransport binds a black-box transport to a running server's base URL.
// Bearer is empty — the systemtests server runs dev-auth (any authenticated
// principal), so no Authorization header is sent.
func NewHTTPTransport(baseURL string) Transport {
	return &httpTransport{client: &sdk.HTTPClient{BaseURL: baseURL, HTTP: &http.Client{}}}
}

func (t *httpTransport) Name() string { return "http" }

func (t *httpTransport) Close() error { return nil }

// --- error mapping (shared with mcptransport.go) -----------------------------

// statusError maps an HTTP status to a transport-agnostic sentinel so tests
// assert outcomes the same way regardless of surface.
func statusError(code int) error {
	switch code {
	case http.StatusBadRequest:
		return ErrBadRequest
	case http.StatusUnauthorized:
		return ErrUnauthenticated
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	case http.StatusServiceUnavailable:
		return ErrUnavailable
	default:
		return fmt.Errorf("unexpected status %d", code)
	}
}

// kindToSentinel maps a Manager error Kind (the "<Kind>: <Detail>" MCP tool
// error text / the {error,code} envelope's semantic class) onto a sentinel.
// Returns nil for an unrecognized kind so the caller surfaces the raw error.
func kindToSentinel(kind string) error {
	switch kind {
	case "ContractMisuse":
		return ErrBadRequest
	case "NotFound":
		return ErrNotFound
	case "Unauthorized":
		return ErrForbidden
	case "FailedPrecondition":
		return ErrConflict
	case "Infrastructure":
		return ErrUnavailable
	default:
		return nil
	}
}

// sentinelError maps an SDK wire error (*sdk.APIError from HTTP, *sdk.MCPToolError
// from MCP) onto a transport-agnostic sentinel wrapped with the clean Detail, so
// a step written once asserts identically over both surfaces. Any other error
// (a plain transport failure, a protocol-level MCP error) surfaces unchanged.
func sentinelError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *sdk.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%w: %s", statusError(apiErr.Status), apiErr.Detail)
	}
	var toolErr *sdk.MCPToolError
	if errors.As(err, &toolErr) {
		if s := kindToSentinel(toolErr.Kind); s != nil {
			return fmt.Errorf("%w: %s", s, toolErr.Detail)
		}
	}
	return err
}

// --- UC1 (system-design / Phase-1) ------------------------------------------

func (t *httpTransport) CreateProject(ctx context.Context, name string) (string, error) {
	id, err := t.client.SystemDesignCreateProject(ctx, testOwner, name)
	return string(id), sentinelError(err)
}

func (t *httpTransport) ListProjects(ctx context.Context, owner string) ([]ProjectSummary, error) {
	rows, err := t.client.SystemDesignListProjects(ctx, sdk.OwnerScope(owner))
	if err != nil {
		return nil, sentinelError(err)
	}
	return toProjectSummaries(rows), nil
}

func (t *httpTransport) SetResearchInput(ctx context.Context, projectID string, sources []ResearchSource) error {
	_, err := t.client.SystemDesignSetResearchInput(ctx, sdk.ProjectID(projectID), toResearchInput(sources))
	return sentinelError(err)
}

func (t *httpTransport) StartDesign(ctx context.Context, projectID string) (string, error) {
	ref, err := t.client.SystemDesignStartSystemDesign(ctx, sdk.ProjectID(projectID))
	return string(ref), sentinelError(err)
}

func (t *httpTransport) RequestArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	ref, err := t.client.SystemDesignRequestArtifactDraft(ctx, sdk.ProjectID(projectID), artifactKind(kind), nil)
	return string(ref), sentinelError(err)
}

func (t *httpTransport) GetSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	view, err := t.client.SystemDesignGetSessionState(ctx, sdk.ProjectID(projectID), artifactKind(kind))
	if err != nil {
		// Any non-200 (404 not-yet-started, transient 503, ...) means "not
		// observable yet" to a poller — never fatal here.
		return SessionState{}, false, sentinelError(err)
	}
	return SessionState{
		ProjectID:     string(view.ProjectID),
		ArtifactKind:  artifactKindNameOf(view.ArtifactKind),
		Stage:         systemStageName(view.Stage),
		FailureReason: strPtrVal(view.FailureReason),
	}, true, nil
}

func (t *httpTransport) SubmitReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	err := t.client.SystemDesignSubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		artifactKind(kind), reviewDecision(decision), systemFeedback(feedback))
	return sentinelError(err)
}

func (t *httpTransport) AdvancePhase(ctx context.Context, projectID string) (bool, []string, error) {
	res, err := t.client.SystemDesignAdvancePhase(ctx, sdk.ProjectID(projectID), false)
	return res.Advanced, decodeMissingArtifacts(res.MissingArtifacts), sentinelError(err)
}

// --- UC2 (project-design / Phase-2) -----------------------------------------

func (t *httpTransport) RequestProjectArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	ref, err := t.client.ProjectDesignRequestArtifactDraft(ctx, sdk.ProjectID(projectID), artifactKind(kind), nil)
	return string(ref), sentinelError(err)
}

func (t *httpTransport) GetProjectSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
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

func (t *httpTransport) SubmitProjectReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	err := t.client.ProjectDesignSubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		artifactKind(kind), reviewDecision(decision), projectFeedback(feedback))
	return sentinelError(err)
}

func (t *httpTransport) RequestSDPCommit(ctx context.Context, projectID string) (string, error) {
	ref, err := t.client.ProjectDesignRequestSDPCommit(ctx, sdk.ProjectID(projectID))
	return string(ref), sentinelError(err)
}

func (t *httpTransport) SubmitSDPDecision(ctx context.Context, projectID, decision, optionID, feedback string) error {
	// optionID is a PATH segment on this route; the ServeMux pattern requires it
	// even for rejectAll (which carries no option) — "-" is the harness's
	// placeholder for "no option". The SDK takes a VALUE sdk.OptionID.
	seg := optionID
	if seg == "" {
		seg = "-"
	}
	err := t.client.ProjectDesignSubmitSDPDecision(ctx, sdk.ProjectID(projectID),
		sdpDecision(decision), sdk.OptionID(seg), projectFeedback(feedback))
	return sentinelError(err)
}

func (t *httpTransport) AdvanceToConstruction(ctx context.Context, projectID string) (bool, []string, error) {
	res, err := t.client.ProjectDesignAdvanceToConstruction(ctx, sdk.ProjectID(projectID), false)
	return res.Advanced, decodeMissingArtifacts(res.MissingArtifacts), sentinelError(err)
}

// --- UC3 (construction / Phase-3) -------------------------------------------

func (t *httpTransport) ExecuteNextActivity(ctx context.Context, projectID, tickID string) (bool, string, error) {
	res, err := t.client.ConstructionExecuteNextActivity(ctx, sdk.ProjectID(projectID), tickID)
	return res.Dispatched, activityIDPtrVal(res.ActivityID), sentinelError(err)
}

func (t *httpTransport) GetConstructionSessionState(ctx context.Context, projectID, activityID string) (ConstructionSessionState, error) {
	view, err := t.client.ConstructionGetSessionState(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID))
	if err != nil {
		return ConstructionSessionState{}, sentinelError(err)
	}
	return toConstructionSessionState(view), nil
}

func (t *httpTransport) SubmitPhaseDecision(ctx context.Context, projectID, activityID, phase, decision, feedback string) error {
	err := t.client.ConstructionSubmitPhaseDecision(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID),
		phase, phaseDecision(decision), constructionFeedback(feedback))
	return sentinelError(err)
}

func (t *httpTransport) UpdateReviewPolicy(ctx context.Context, projectID string, gatedPhasesByType map[string][]string) error {
	err := t.client.ConstructionUpdateReviewPolicy(ctx, sdk.ProjectID(projectID),
		sdk.ReviewPolicyInput{GatedPhasesByType: gatedPhasesByType})
	return sentinelError(err)
}

// --- UC4 (operations / Phase-4) ---------------------------------------------

func (t *httpTransport) DeployAfterConstruction(ctx context.Context, operatedAppID string, change DesiredStateChange) (bool, string, error) {
	res, err := t.client.OperationsDeployAfterConstruction(ctx, operatedAppID, toDesiredStateChange(change))
	return res.Published, strPtrVal(res.Revision), sentinelError(err)
}

func (t *httpTransport) ReconcileOperatedState(ctx context.Context, tickID string, appIDs []string) (int64, int64, int64, error) {
	res, err := t.client.OperationsReconcileOperatedState(ctx, tickID, reconcileScope(appIDs))
	return res.Observed, res.Transitions, res.Republished, sentinelError(err)
}

func (t *httpTransport) QueryOperatedSystemView(ctx context.Context, operatedAppID, requestID string) (OperatedSystemView, error) {
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

func (t *httpTransport) ApplyDelinquencyPolicy(ctx context.Context, customerID string, pauseNotWithdraw bool) error {
	err := t.client.OperationsApplyDelinquencyPolicy(ctx, customerID,
		sdk.DelinquencyContext{PauseNotWithdraw: pauseNotWithdraw})
	return sentinelError(err)
}

func (t *httpTransport) WithdrawSystem(ctx context.Context, operatedAppID, changeID, notes string) (bool, error) {
	res, err := t.client.OperationsWithdrawSystem(ctx, operatedAppID, changeID, sdk.WithdrawReason{Notes: notes})
	return res.Withdrawn, sentinelError(err)
}

// --- shared SDK<->harness projections (used by BOTH transports) --------------

func toProjectSummaries(rows []sdk.ProjectSummary) []ProjectSummary {
	out := make([]ProjectSummary, 0, len(rows))
	for _, s := range rows {
		out = append(out, ProjectSummary{
			ProjectID: string(s.ProjectID),
			Name:      s.Name,
			Owner:     string(s.Owner),
			PhaseName: s.PhaseName,
		})
	}
	return out
}

func toResearchInput(sources []ResearchSource) sdk.ResearchInput {
	if sources == nil {
		return sdk.ResearchInput{Sources: nil}
	}
	out := make([]sdk.ResearchSource, 0, len(sources))
	for _, s := range sources {
		out = append(out, sdk.ResearchSource{Title: s.Title, Content: s.Content})
	}
	return sdk.ResearchInput{Sources: out}
}

func toConstructionSessionState(view sdk.ConstructionSessionView) ConstructionSessionState {
	state := ConstructionSessionState{
		ProjectID:  string(view.ProjectID),
		ActivityID: activityIDPtrVal(view.ActivityID),
		Stage:      constructionStageName(view.Stage),
	}
	if view.PipelinePhase != nil {
		state.PipelinePhase = pipelinePhaseName(*view.PipelinePhase)
	}
	return state
}

func toDesiredStateChange(change DesiredStateChange) sdk.DesiredStateChange {
	return sdk.DesiredStateChange{
		Reason:    desiredStateReason(change.Reason),
		PatchKind: patchKind(change.PatchKind),
		ChangeID:  change.ChangeID,
	}
}

// reconcileScope builds the optional *sdk.ReconcileScope — nil (all in-flight
// apps) when appIDs is nil, matching the hand transport's omit-when-nil body.
func reconcileScope(appIDs []string) *sdk.ReconcileScope {
	if appIDs == nil {
		return nil
	}
	return &sdk.ReconcileScope{AppIDs: appIDs}
}

func activityIDPtrVal(id *sdk.ActivityID) string {
	if id == nil {
		return ""
	}
	return string(*id)
}

// feedback builders — the Manager requires a non-empty feedback object only on
// reject/sendBack; the harness passes "" otherwise, which becomes a nil pointer
// (the omitempty field is dropped from the request body, exactly as before).
func systemFeedback(notes string) *sdk.SystemDesignReviewFeedback {
	if notes == "" {
		return nil
	}
	return &sdk.SystemDesignReviewFeedback{Notes: notes}
}

func projectFeedback(notes string) *sdk.ProjectDesignReviewFeedback {
	if notes == "" {
		return nil
	}
	return &sdk.ProjectDesignReviewFeedback{Notes: notes}
}

func constructionFeedback(notes string) *sdk.ConstructionReviewFeedback {
	if notes == "" {
		return nil
	}
	return &sdk.ConstructionReviewFeedback{Notes: notes}
}
