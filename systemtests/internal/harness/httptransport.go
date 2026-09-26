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

// --- STAGE 4a ---------------------------------------------------------------
//
// The three Managers this harness used to call are one (deliveryManager, twelve ops),
// so each Transport method below is re-pointed onto the op that now serves it. The
// Transport INTERFACE is unchanged — the scenarios still read CreateProject /
// GetSessionState / SubmitPhaseDecision — and so is everything each one proves; only
// the entry point moved. The map is Task 6 Step 9's dispatch table read backwards:
// CreateProject/SetResearchInput/StartDesign -> StartProject, every artifact draft ->
// DispatchActivityTask, every review verdict and phase seal -> SubmitReviewDecision,
// every session/catalog read -> QueryProjectView, and SetReviewPolicy ->
// SetProjectExecutionPolicy.
//
// The design ACTIVITY a task belongs to is derived from its artifact kind, exactly as
// the Manager derives it: the requirements activity owns mission/glossary/
// scrubbedRequirements/volatilities/coreUseCases, architecture owns system/
// operationalConcepts/standardCheck, and projectDesign owns every Phase-2 kind.

// --- UC1 (system-design / Phase-1) ------------------------------------------

func (t *httpTransport) CreateProject(ctx context.Context, name string) (string, error) {
	res, err := t.client.DeliveryStartProject(ctx, testOwner, name, nil, nil, nil, false)
	return string(res.ProjectID), sentinelError(err)
}

func (t *httpTransport) ListProjects(ctx context.Context, owner string) ([]ProjectSummary, error) {
	scope := sdk.OwnerScope(owner)
	view, err := t.client.DeliveryQueryProjectView(ctx, sdk.ProjectViewQuery{
		Kind: sdk.ProjectViewProjects, Owner: &scope,
	})
	if err != nil {
		return nil, sentinelError(err)
	}
	return toProjectSummaries(view.Projects), nil
}

func (t *httpTransport) SetResearchInput(ctx context.Context, projectID string, sources []ResearchSource) error {
	research := toResearchInput(sources)
	_, err := t.client.DeliveryStartProject(ctx, testOwner, "", &projectID, nil, &research, false)
	return sentinelError(err)
}

func (t *httpTransport) StartDesign(ctx context.Context, projectID string) (string, error) {
	res, err := t.client.DeliveryStartProject(ctx, testOwner, "", &projectID, nil, nil, true)
	if res.Session == nil {
		return "", sentinelError(err)
	}
	return string(*res.Session), sentinelError(err)
}

func (t *httpTransport) RequestArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	ref, err := t.client.DeliveryDispatchActivityTask(ctx, sdk.ProjectID(projectID),
		designActivityFor(kind), draftTaskFor(kind), nil)
	return string(ref), sentinelError(err)
}

func (t *httpTransport) GetSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
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

func (t *httpTransport) SubmitReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		designActivityFor(kind), reviewTaskFor(kind),
		sdk.ReviewDecisionInput{Decision: reviewDecision(decision)}, systemFeedback(feedback))
	return sentinelError(err)
}

func (t *httpTransport) AdvancePhase(ctx context.Context, projectID string) (bool, []string, error) {
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

func (t *httpTransport) RequestProjectArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	ref, err := t.client.DeliveryDispatchActivityTask(ctx, sdk.ProjectID(projectID),
		designActivityFor(kind), draftTaskFor(kind), nil)
	return string(ref), sentinelError(err)
}

func (t *httpTransport) GetProjectSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
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

func (t *httpTransport) SubmitProjectReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID),
		designActivityFor(kind), reviewTaskFor(kind),
		sdk.ReviewDecisionInput{Decision: reviewDecision(decision)}, projectFeedback(feedback))
	return sentinelError(err)
}

func (t *httpTransport) RequestSDPCommit(ctx context.Context, projectID string) (string, error) {
	ref, err := t.client.DeliveryDispatchActivityTask(ctx, sdk.ProjectID(projectID),
		"projectDesign", "sdpReview", nil)
	return string(ref), sentinelError(err)
}

func (t *httpTransport) SubmitSDPDecision(ctx context.Context, projectID, decision, optionID, feedback string) error {
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

func (t *httpTransport) AdvanceToConstruction(ctx context.Context, projectID string) (bool, []string, error) {
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

func (t *httpTransport) ExecuteNextActivity(ctx context.Context, projectID, tickID string) (bool, string, error) {
	res, err := t.client.DeliveryExecuteNextActivity(ctx, sdk.ProjectID(projectID), tickID)
	return res.Dispatched, activityIDPtrVal(res.ActivityID), sentinelError(err)
}

func (t *httpTransport) GetConstructionSessionState(ctx context.Context, projectID, activityID string) (ConstructionSessionState, error) {
	id := activityID
	view, err := t.client.DeliveryQueryProjectView(ctx, sdk.ProjectViewQuery{
		Kind: sdk.ProjectViewSession, ProjectID: &projectID, ActivityID: &id,
	})
	if err != nil || view.ConstructionSession == nil {
		return ConstructionSessionState{}, sentinelError(err)
	}
	return toConstructionSessionState(*view.ConstructionSession), nil
}

func (t *httpTransport) SubmitPhaseDecision(ctx context.Context, projectID, activityID, phase, decision, feedback string) error {
	err := t.client.DeliverySubmitReviewDecision(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID),
		phase, sdk.ReviewDecisionInput{Decision: phaseReviewDecision(decision)}, constructionFeedback(feedback))
	return sentinelError(err)
}

func (t *httpTransport) UpdateReviewPolicy(ctx context.Context, projectID string, gatedPhasesByType map[string][]string) error {
	policy := sdk.ReviewPolicyInput{GatedPhasesByType: gatedPhasesByType}
	err := t.client.DeliverySetProjectExecutionPolicy(ctx, sdk.ProjectID(projectID),
		sdk.ExecutionPolicyInput{Policy: &policy})
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
func systemFeedback(notes string) *sdk.ReviewFeedback {
	if notes == "" {
		return nil
	}
	return &sdk.ReviewFeedback{Notes: notes}
}

func projectFeedback(notes string) *sdk.ReviewFeedback {
	if notes == "" {
		return nil
	}
	return &sdk.ReviewFeedback{Notes: notes}
}

func constructionFeedback(notes string) *sdk.ReviewFeedback {
	if notes == "" {
		return nil
	}
	return &sdk.ReviewFeedback{Notes: notes}
}

// querySession is the one session read the twelve-op surface exposes; which arm of the
// ProjectView it fills (Session for a Phase-1 kind, ProjectSession for a Phase-2 one) is
// the Manager's decision, made from the artifact kind, so both callers pass the kind and
// read their own arm.
func (t *httpTransport) querySession(ctx context.Context, projectID, kind string) (sdk.ProjectView, error) {
	ak := artifactKind(kind)
	return t.client.DeliveryQueryProjectView(ctx, sdk.ProjectViewQuery{
		Kind: sdk.ProjectViewSession, ProjectID: &projectID, ArtifactKind: &ak,
	})
}

// phaseAdvanced reads the phase seal's OUTCOME back off the summary view. The old
// AdvancePhase/AdvanceToConstruction ops returned {advanced, missingArtifacts}; the
// merged SubmitReviewDecision is void, so the harness reads the same two facts from
// state — the project's phase, and (when it has not moved) the uncommitted slots that
// are holding it.
func (t *httpTransport) phaseAdvanced(ctx context.Context, projectID string, want sdk.Phase) (bool, []string, error) {
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
func (t *httpTransport) QueryActivityView(ctx context.Context, projectID, activityID string) (string, error) {
	view, err := t.client.DeliveryQueryActivityView(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID))
	if err != nil {
		return "", sentinelError(err)
	}
	return activityViewStateName(view.State), nil
}

// OverrideActivity delivers the operator's steer through the twelve-op surface.
func (t *httpTransport) OverrideActivity(ctx context.Context, projectID, activityID string, kind int, notes string) error {
	err := t.client.DeliveryOverrideActivity(ctx, sdk.ProjectID(projectID), sdk.ActivityID(activityID),
		sdk.ActivityOverride{Kind: sdk.OverrideKind(kind), Notes: notes})
	return sentinelError(err)
}
