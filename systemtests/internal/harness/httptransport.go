package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// httpTransport drives the webClient HTTP surface. It is the reference black-box
// transport: it knows ONLY the published routes + JSON DTOs the generated Client
// layer serves (server/internal/client/web/{systemdesign,projectdesign}/*_handlers.gen.go),
// never a server internal type. When mcpClient is built, an mcpTransport
// implements the same Transport interface and the R4 equivalence test runs the
// same use-case steps through both.
//
// The generated Client surface is verb-scoped REST, NOT the old resource-scoped
// "/api/v1/projects/..." shape: every op is its own route
// "/api/v1/<component>/<op-kebab-name>[/{projectID}[/{optionID}]]", the request
// body carries exactly the op's remaining args (never projectID, which is always
// a path segment when the op takes one), and the response body is the op's bare
// return value — NOT a named wrapper struct. A single-value return (e.g.
// ProjectID, SessionRef, Version) is therefore a bare JSON scalar on the wire
// (e.g. `"proj-123"`), not `{"projectId":"proj-123"}`.
type httpTransport struct {
	baseURL string
	hc      *http.Client
}

// NewHTTPTransport binds a black-box transport to a running server's base URL.
func NewHTTPTransport(baseURL string) Transport {
	return &httpTransport{baseURL: baseURL, hc: &http.Client{}}
}

func (t *httpTransport) Name() string { return "http" }

func (t *httpTransport) Close() error { return nil }

// --- wire enum ordinal tables ------------------------------------------------
//
// Every generated enum is a bare Go int on the wire — "generated enums carry no
// behavior" (server/cmd/modelgen/main.go), so there is no MarshalJSON producing a
// friendly string. The ordinal↔name tables below mirror the iota order committed
// in server/internal/manager/systemdesign/contract.gen.go and
// server/internal/manager/projectdesign/contract.gen.go (the same mapping the SPA
// keeps client-side in webApp/src/api/enums.ts). This is reading the PUBLISHED
// wire contract, not importing server code — the harness stays black-box.

// artifactKindByOrdinal is shared by BOTH system-design and project-design
// ArtifactKind (one 0..16 ordering covers every Method artifact kind).
var artifactKindByOrdinal = []string{
	"mission", "glossary", "scrubbedRequirements", "volatilities",
	"coreUseCases", "system", "operationalConcepts", "standardCheck",
	"planningAssumptions", "activityList", "network", "normalSolution",
	"subcriticalSolution", "compressedSolution", "decompressedSolution",
	"riskModel", "sdpReview",
}

var artifactKindToOrdinal = func() map[string]int {
	m := make(map[string]int, len(artifactKindByOrdinal))
	for i, k := range artifactKindByOrdinal {
		m[k] = i
	}
	return m
}()

// artifactKindOrdinal maps a wire kind name to its ordinal. An unknown name maps
// to 0 (mission) — callers only ever pass names drawn from artifactKindByOrdinal.
func artifactKindOrdinal(kind string) int { return artifactKindToOrdinal[kind] }

// artifactKindName is the inverse of artifactKindOrdinal, used to decode
// PhaseAdvanceResult.missingArtifacts and SessionStateView.artifactKind.
func artifactKindName(ordinal int) string {
	if ordinal < 0 || ordinal >= len(artifactKindByOrdinal) {
		return "mission"
	}
	return artifactKindByOrdinal[ordinal]
}

// systemSessionStageByOrdinal is systemdesign.SessionStage's 0..7 ordering.
var systemSessionStageByOrdinal = []string{
	"unknown", "drafting", "awaitingReview", "redrafting",
	"committed", "withdrawn", "refused", "draftFailed",
}

// projectSessionStageByOrdinal is projectdesign.SessionStage's 0..8 ordering —
// ONE MORE stage than the system-design enum (assemblingSdp is inserted at
// ordinal 2), so it is a DISTINCT table, not a shared one.
var projectSessionStageByOrdinal = []string{
	"unknown", "drafting", "assemblingSdp", "awaitingReview", "redrafting",
	"committed", "withdrawn", "refused", "draftFailed",
}

func stageName(table []string, ordinal int) string {
	if ordinal < 0 || ordinal >= len(table) {
		return "unknown"
	}
	return table[ordinal]
}

// reviewDecisionToOrdinal mirrors ReviewDecision (0 unknown,1 approve,2 reject,3 withdraw).
var reviewDecisionToOrdinal = map[string]int{
	"approve":  1,
	"reject":   2,
	"withdraw": 3,
}

// sdpDecisionToOrdinal mirrors SDPDecision (0 unknown,1 commit,2 rejectAll).
var sdpDecisionToOrdinal = map[string]int{
	"commit":    1,
	"rejectAll": 2,
}

// --- UC3 (construction) wire enum ordinal tables ----------------------------
// Mirror the iota order committed in server/internal/manager/construction/contract.gen.go.

// constructionStageByOrdinal is ConstructionStage's 0..7 ordering.
var constructionStageByOrdinal = []string{
	"unknown", "dispatching", "pipelineRunning", "reviewing",
	"awaitingTakeover", "paused", "exited", "awaitingApproval",
}

// pipelinePhaseByOrdinal is PipelinePhase's 0..5 ordering.
var pipelinePhaseByOrdinal = []string{
	"unknown", "pending", "running", "succeeded", "failed", "cancelled",
}

// phaseDecisionToOrdinal mirrors PhaseDecision (0 unknown,1 approve,2 sendBack).
var phaseDecisionToOrdinal = map[string]int{
	"approve":  1,
	"sendBack": 2,
}

// constructionSessionViewWire is the wire shape of ConstructionSessionView
// (construction/contract.gen.go) — only the fields the UC3 wire tests assert on.
type constructionSessionViewWire struct {
	ProjectID     string `json:"projectId"`
	ActivityID    string `json:"activityId,omitempty"`
	Stage         int    `json:"stage"`
	PipelinePhase *int   `json:"pipelinePhase,omitempty"`
}

// pumpResultWire is the wire shape of PumpResult (ExecuteNextActivity's response).
type pumpResultWire struct {
	Dispatched bool    `json:"dispatched"`
	ActivityID *string `json:"activityId,omitempty"`
}

// --- UC4 (operations) wire enum ordinal tables -------------------------------
// Mirror the iota order committed in server/internal/manager/operations/contract.gen.go.

// desiredStateReasonToOrdinal mirrors DesiredStateReason (0 unknown,1
// deployAfterConstruction,2 operator,3 autoscale,4 delinquency).
var desiredStateReasonToOrdinal = map[string]int{
	"deployAfterConstruction": 1,
	"operator":                2,
	"autoscale":               3,
	"delinquency":             4,
}

// patchKindToOrdinal mirrors PatchKind (0 unknown,1 fullBundle,2 scale,3 policy).
var patchKindToOrdinal = map[string]int{
	"fullBundle": 1,
	"scale":      2,
	"policy":     3,
}

// runtimeStatusByOrdinal is RuntimeStatusSeam's 0..4 ordering.
var runtimeStatusByOrdinal = []string{
	"unknown", "pending", "healthy", "degraded", "withdrawn",
}

// operatedSystemViewWire is the wire shape of OperatedSystemView (operations/
// contract.gen.go) — PascalCase JSON tags per the published contract (NOT
// camelCase like the design/construction DTOs); only the fields the UC4 wire
// tests assert on.
type operatedSystemViewWire struct {
	OperatedAppID string `json:"OperatedAppID"`
	Phase         int    `json:"Phase"`
	InFlight      bool   `json:"InFlight"`
}

// testOwner is the fixed OwnerScope the harness mints every project under. Owner
// is a required, non-empty CreateProject arg but is NOT consulted by
// authenticatedOnlyPDP (any authenticated principal may act on any resource — see
// server/cmd/server/authz.go) and the Transport interface's CreateProject has no
// owner parameter, so a single constant value is sufficient for a black-box test.
const testOwner = "systemtest"

// reviewFeedbackBody is the wire shape of ReviewFeedback for a request body
// (systemdesign.ReviewFeedback additionally carries anchored Comments, which the
// harness never populates — Notes alone round-trips through both the
// systemdesign and projectdesign Feedback structs, which both accept an object
// with an extra unused field absent).
type reviewFeedbackBody struct {
	Notes string `json:"notes"`
}

// sessionStateWire is the wire shape common to systemdesign.SessionStateView and
// projectdesign.SessionStateView: the header fields the wiring tests assert on,
// decoded generically (the nested "draft"/"findings" are intentionally left
// undecoded — the wiring test does not assert on them).
type sessionStateWire struct {
	ProjectID    string `json:"projectId"`
	ArtifactKind int    `json:"artifactKind"`
	Stage        int    `json:"stage"`
}

// phaseAdvanceWire is the wire shape of PhaseAdvanceResult (shared shape between
// systemdesign and projectdesign): missingArtifacts is a bare []int on the wire.
type phaseAdvanceWire struct {
	Advanced         bool  `json:"advanced"`
	MissingArtifacts []int `json:"missingArtifacts"`
}

func decodeMissingArtifacts(ords []int) []string {
	out := make([]string, 0, len(ords))
	for _, o := range ords {
		out = append(out, artifactKindName(o))
	}
	return out
}

func (t *httpTransport) CreateProject(ctx context.Context, name string) (string, error) {
	var out string
	_, err := t.do(ctx, http.MethodPost, "/api/v1/system-design/create-project",
		map[string]any{"owner": testOwner, "name": name}, http.StatusOK, &out)
	return out, err
}

// All phase routes are PROJECT-SCOPED: projectId is a path segment, never a body
// field. The body carries only the remaining intent payload (research corpus,
// artifactKind ordinal, decision ordinal).

func (t *httpTransport) SetResearchInput(ctx context.Context, projectID string, sources []ResearchSource) error {
	body := map[string]any{"research": map[string]any{"sources": sources}}
	path := fmt.Sprintf("/api/v1/system-design/set-research-input/%s", projectID)
	// SetResearchInput returns the resulting head Version (a bare int64) on 200;
	// the harness callers only care about success/failure, so the body is
	// discarded (out == nil skips decode in do()).
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusOK, nil)
	return err
}

func (t *httpTransport) StartDesign(ctx context.Context, projectID string) (string, error) {
	var out string
	path := fmt.Sprintf("/api/v1/system-design/start-system-design/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path, nil, http.StatusOK, &out)
	return out, err
}

func (t *httpTransport) RequestArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	var out string
	path := fmt.Sprintf("/api/v1/system-design/request-artifact-draft/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path,
		map[string]any{"kind": artifactKindOrdinal(kind)}, http.StatusOK, &out)
	return out, err
}

func (t *httpTransport) GetSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	path := fmt.Sprintf("/api/v1/system-design/get-session-state/%s?kind=%d", projectID, artifactKindOrdinal(kind))
	var out sessionStateWire
	status, err := t.do(ctx, http.MethodGet, path, nil, http.StatusOK, &out)
	if err != nil {
		// Any non-200 (404 not-yet-started, transient 503, ...) means "not
		// observable yet" to a poller — never fatal here.
		_ = status
		return SessionState{}, false, err
	}
	return SessionState{
		ProjectID:    out.ProjectID,
		ArtifactKind: artifactKindName(out.ArtifactKind),
		Stage:        stageName(systemSessionStageByOrdinal, out.Stage),
	}, true, nil
}

func (t *httpTransport) SubmitReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	body := map[string]any{
		"kind":     artifactKindOrdinal(kind),
		"decision": reviewDecisionToOrdinal[decision],
	}
	if feedback != "" {
		body["feedback"] = reviewFeedbackBody{Notes: feedback}
	}
	path := fmt.Sprintf("/api/v1/system-design/submit-review-decision/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) AdvancePhase(ctx context.Context, projectID string) (bool, []string, error) {
	var out phaseAdvanceWire
	path := fmt.Sprintf("/api/v1/system-design/advance-phase/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path, nil, http.StatusOK, &out)
	return out.Advanced, decodeMissingArtifacts(out.MissingArtifacts), err
}

// --- UC2 (project-design / Phase-2) -----------------------------------------
// Each method speaks ONLY the published project-design routes + DTOs
// (server/internal/client/web/projectdesign/project-design_handlers.gen.go).
// projectId is a path segment; the body carries the remaining intent payload —
// the same project-scoped shape as Phase 1.

func (t *httpTransport) RequestProjectArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	var out string
	path := fmt.Sprintf("/api/v1/project-design/request-artifact-draft/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path,
		map[string]any{"kind": artifactKindOrdinal(kind)}, http.StatusOK, &out)
	return out, err
}

func (t *httpTransport) GetProjectSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	path := fmt.Sprintf("/api/v1/project-design/get-session-state/%s?kind=%d", projectID, artifactKindOrdinal(kind))
	var out sessionStateWire
	if _, err := t.do(ctx, http.MethodGet, path, nil, http.StatusOK, &out); err != nil {
		// Any non-200 (404 not-yet-started, transient 503, ...) means "not
		// observable yet" to a poller — never fatal here.
		return SessionState{}, false, err
	}
	return SessionState{
		ProjectID:    out.ProjectID,
		ArtifactKind: artifactKindName(out.ArtifactKind),
		Stage:        stageName(projectSessionStageByOrdinal, out.Stage),
	}, true, nil
}

func (t *httpTransport) SubmitProjectReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	body := map[string]any{
		"kind":     artifactKindOrdinal(kind),
		"decision": reviewDecisionToOrdinal[decision],
	}
	if feedback != "" {
		body["feedback"] = reviewFeedbackBody{Notes: feedback}
	}
	path := fmt.Sprintf("/api/v1/project-design/submit-review-decision/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) RequestSDPCommit(ctx context.Context, projectID string) (string, error) {
	var out string
	path := fmt.Sprintf("/api/v1/project-design/request-sdp-commit/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path, nil, http.StatusOK, &out)
	return out, err
}

func (t *httpTransport) SubmitSDPDecision(ctx context.Context, projectID, decision, optionID, feedback string) error {
	body := map[string]any{"decision": sdpDecisionToOrdinal[decision]}
	if feedback != "" {
		body["feedback"] = reviewFeedbackBody{Notes: feedback}
	}
	// optionID is a PATH segment on this route (POST .../submit-sdp-decision/
	// {projectID}/{optionID}), not a body field — required by the net/http
	// ServeMux pattern even when the SDP decision is rejectAll (which carries no
	// option); "-" is the harness's placeholder for "no option".
	seg := optionID
	if seg == "" {
		seg = "-"
	}
	path := fmt.Sprintf("/api/v1/project-design/submit-sdp-decision/%s/%s", projectID, seg)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) AdvanceToConstruction(ctx context.Context, projectID string) (bool, []string, error) {
	var out phaseAdvanceWire
	path := fmt.Sprintf("/api/v1/project-design/advance-to-construction/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path, nil, http.StatusOK, &out)
	return out.Advanced, decodeMissingArtifacts(out.MissingArtifacts), err
}

// --- UC3 (construction / Phase-3) -------------------------------------------
// Each method speaks ONLY the published construction routes + DTOs
// (server/internal/client/web/construction/construction_handlers.gen.go).

func (t *httpTransport) ExecuteNextActivity(ctx context.Context, projectID, tickID string) (bool, string, error) {
	var out pumpResultWire
	path := fmt.Sprintf("/api/v1/construction/execute-next-activity/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path, map[string]any{"tickID": tickID}, http.StatusOK, &out)
	activityID := ""
	if out.ActivityID != nil {
		activityID = *out.ActivityID
	}
	return out.Dispatched, activityID, err
}

func (t *httpTransport) GetConstructionSessionState(ctx context.Context, projectID, activityID string) (ConstructionSessionState, error) {
	var out constructionSessionViewWire
	path := fmt.Sprintf("/api/v1/construction/get-session-state/%s/%s", projectID, activityID)
	if _, err := t.do(ctx, http.MethodGet, path, nil, http.StatusOK, &out); err != nil {
		return ConstructionSessionState{}, err
	}
	state := ConstructionSessionState{
		ProjectID:  out.ProjectID,
		ActivityID: out.ActivityID,
		Stage:      stageName(constructionStageByOrdinal, out.Stage),
	}
	if out.PipelinePhase != nil {
		state.PipelinePhase = stageName(pipelinePhaseByOrdinal, *out.PipelinePhase)
	}
	return state, nil
}

func (t *httpTransport) SubmitPhaseDecision(ctx context.Context, projectID, activityID, phase, decision, feedback string) error {
	body := map[string]any{
		"phase":    phase,
		"decision": phaseDecisionToOrdinal[decision],
	}
	if feedback != "" {
		body["feedback"] = reviewFeedbackBody{Notes: feedback}
	}
	path := fmt.Sprintf("/api/v1/construction/submit-phase-decision/%s/%s", projectID, activityID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) UpdateReviewPolicy(ctx context.Context, projectID string, gatedPhasesByType map[string][]string) error {
	body := map[string]any{"policy": map[string]any{"gatedPhasesByType": gatedPhasesByType}}
	path := fmt.Sprintf("/api/v1/construction/update-review-policy/%s", projectID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

// --- UC4 (operations / Phase-4) ---------------------------------------------
// Each method speaks ONLY the published operations routes + DTOs
// (server/internal/client/web/operations/operations_handlers.gen.go).

func (t *httpTransport) DeployAfterConstruction(ctx context.Context, operatedAppID string, change DesiredStateChange) (bool, string, error) {
	body := map[string]any{"change": map[string]any{
		"reason":               desiredStateReasonToOrdinal[change.Reason],
		"patchKind":            patchKindToOrdinal[change.PatchKind],
		"changeId":             change.ChangeID,
		"renderedDesiredState": change.RenderedDesiredState,
	}}
	var out struct {
		Published bool    `json:"published"`
		Revision  *string `json:"revision,omitempty"`
	}
	path := fmt.Sprintf("/api/v1/operations/deploy-after-construction/%s", operatedAppID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusOK, &out)
	revision := ""
	if out.Revision != nil {
		revision = *out.Revision
	}
	return out.Published, revision, err
}

func (t *httpTransport) ReconcileOperatedState(ctx context.Context, tickID string, appIDs []string) (int64, int64, int64, error) {
	body := map[string]any{"tickID": tickID}
	if appIDs != nil {
		body["scope"] = map[string]any{"appIds": appIDs}
	}
	var out struct {
		Observed    int64 `json:"observed"`
		Transitions int64 `json:"transitions"`
		Republished int64 `json:"republished"`
	}
	_, err := t.do(ctx, http.MethodPost, "/api/v1/operations/reconcile-operated-state", body, http.StatusOK, &out)
	return out.Observed, out.Transitions, out.Republished, err
}

func (t *httpTransport) QueryOperatedSystemView(ctx context.Context, operatedAppID, requestID string) (OperatedSystemView, error) {
	var out operatedSystemViewWire
	path := fmt.Sprintf("/api/v1/operations/query-operated-system-view/%s?requestID=%s", operatedAppID, requestID)
	if _, err := t.do(ctx, http.MethodGet, path, nil, http.StatusOK, &out); err != nil {
		return OperatedSystemView{}, err
	}
	return OperatedSystemView{
		OperatedAppID: out.OperatedAppID,
		Phase:         stageName(runtimeStatusByOrdinal, out.Phase),
		InFlight:      out.InFlight,
	}, nil
}

func (t *httpTransport) ApplyDelinquencyPolicy(ctx context.Context, customerID string, pauseNotWithdraw bool) error {
	body := map[string]any{"delinquencyContext": map[string]any{"pauseNotWithdraw": pauseNotWithdraw}}
	path := fmt.Sprintf("/api/v1/operations/apply-delinquency-policy/%s", customerID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) WithdrawSystem(ctx context.Context, operatedAppID, changeID, notes string) (bool, error) {
	body := map[string]any{"changeID": changeID, "reason": map[string]any{"notes": notes}}
	var out struct {
		Withdrawn bool `json:"withdrawn"`
	}
	path := fmt.Sprintf("/api/v1/operations/withdraw-system/%s", operatedAppID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusOK, &out)
	return out.Withdrawn, err
}

// do issues one request, maps a non-expected status onto a sentinel error, and
// decodes the body into out on success. It returns the status code so callers
// can distinguish transient/absent (404/503) from a hard failure.
func (t *httpTransport) do(ctx context.Context, method, path string, body any, want int, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("marshal %s %s: %w", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, rdr)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode != want {
		return resp.StatusCode, statusError(resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

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
