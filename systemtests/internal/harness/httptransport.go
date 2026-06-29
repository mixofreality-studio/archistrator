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
// transport: it knows ONLY the published routes + JSON DTOs (api/openapi.yaml),
// never a server internal type. When mcpClient is built, an mcpTransport
// implements the same Transport interface and the R4 equivalence test runs the
// same use-case steps through both.
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

type createProjectResp struct {
	ProjectID string `json:"projectId"`
}

type sessionRefResp struct {
	SessionRef string `json:"sessionRef"`
}

type sessionStateResp struct {
	ProjectID    string `json:"projectId"`
	ArtifactKind string `json:"artifactKind"`
	Stage        string `json:"stage"`
}

type advanceResp struct {
	Advanced         bool     `json:"advanced"`
	MissingArtifacts []string `json:"missingArtifacts"`
}

func (t *httpTransport) CreateProject(ctx context.Context, name string) (string, error) {
	var out createProjectResp
	_, err := t.do(ctx, http.MethodPost, "/api/v1/projects",
		map[string]any{"name": name}, http.StatusCreated, &out)
	return out.ProjectID, err
}

// All phase routes are PROJECT-SCOPED: projectId is a path segment, never a body
// field (api/openapi.yaml — project-scoped URLs). The body carries only the
// remaining intent payload (research corpus, artifactKind, decision).

func (t *httpTransport) SetResearchInput(ctx context.Context, projectID string, sources []ResearchSource) error {
	body := map[string]any{"research": map[string]any{"sources": sources}}
	path := fmt.Sprintf("/api/v1/projects/%s/system-design/research-input", projectID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) StartDesign(ctx context.Context, projectID string) (string, error) {
	var out sessionRefResp
	path := fmt.Sprintf("/api/v1/projects/%s/system-design/start", projectID)
	_, err := t.do(ctx, http.MethodPost, path, nil, http.StatusAccepted, &out)
	return out.SessionRef, err
}

func (t *httpTransport) RequestArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	var out sessionRefResp
	path := fmt.Sprintf("/api/v1/projects/%s/system-design/artifacts/draft", projectID)
	_, err := t.do(ctx, http.MethodPost, path,
		map[string]any{"artifactKind": kind}, http.StatusAccepted, &out)
	return out.SessionRef, err
}

func (t *httpTransport) GetSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/system-design/sessions/%s", projectID, kind)
	var out sessionStateResp
	status, err := t.do(ctx, http.MethodGet, path, nil, http.StatusOK, &out)
	if err != nil {
		// Any non-200 (404 not-yet-started, transient 503, ...) means "not
		// observable yet" to a poller — never fatal here.
		_ = status
		return SessionState{}, false, err
	}
	return SessionState(out), true, nil
}

func (t *httpTransport) SubmitReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	body := map[string]any{"artifactKind": kind, "decision": decision}
	if feedback != "" {
		body["feedback"] = feedback
	}
	path := fmt.Sprintf("/api/v1/projects/%s/system-design/artifacts/review", projectID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) AdvancePhase(ctx context.Context, projectID string) (bool, []string, error) {
	var out advanceResp
	path := fmt.Sprintf("/api/v1/projects/%s/system-design/advance", projectID)
	_, err := t.do(ctx, http.MethodPost, path, nil, http.StatusOK, &out)
	return out.Advanced, out.MissingArtifacts, err
}

// --- UC2 (project-design / Phase-2) -----------------------------------------
// Each method speaks ONLY the published project-design routes + DTOs
// (internal/client/web/projectdesign.go). projectId is a path segment; the body
// carries the remaining intent payload — the same project-scoped shape as Phase 1.

func (t *httpTransport) RequestProjectArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	var out sessionRefResp
	path := fmt.Sprintf("/api/v1/projects/%s/project-design/artifacts/draft", projectID)
	_, err := t.do(ctx, http.MethodPost, path,
		map[string]any{"artifactKind": kind}, http.StatusAccepted, &out)
	return out.SessionRef, err
}

// projectSessionStateResp mirrors projectSessionStateResponse: the technical
// header fields (projectId/artifactKind/stage) plus a nested typed "view" the
// wiring test does not assert on (so it is intentionally left undecoded here).
type projectSessionStateResp struct {
	ProjectID    string `json:"projectId"`
	ArtifactKind string `json:"artifactKind"`
	Stage        string `json:"stage"`
}

func (t *httpTransport) GetProjectSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/project-design/sessions/%s", projectID, kind)
	var out projectSessionStateResp
	if _, err := t.do(ctx, http.MethodGet, path, nil, http.StatusOK, &out); err != nil {
		// Any non-200 (404 not-yet-started, transient 503, ...) means "not
		// observable yet" to a poller — never fatal here.
		return SessionState{}, false, err
	}
	return SessionState(out), true, nil
}

func (t *httpTransport) SubmitProjectReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	body := map[string]any{"artifactKind": kind, "decision": decision}
	if feedback != "" {
		body["feedback"] = feedback
	}
	path := fmt.Sprintf("/api/v1/projects/%s/project-design/artifacts/review", projectID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) RequestSDPCommit(ctx context.Context, projectID string) (string, error) {
	var out sessionRefResp
	path := fmt.Sprintf("/api/v1/projects/%s/project-design/sdp/assemble", projectID)
	_, err := t.do(ctx, http.MethodPost, path, nil, http.StatusAccepted, &out)
	return out.SessionRef, err
}

func (t *httpTransport) SubmitSDPDecision(ctx context.Context, projectID, decision, optionID, feedback string) error {
	body := map[string]any{"decision": decision}
	if optionID != "" {
		body["optionId"] = optionID
	}
	if feedback != "" {
		body["feedback"] = feedback
	}
	path := fmt.Sprintf("/api/v1/projects/%s/project-design/sdp/decision", projectID)
	_, err := t.do(ctx, http.MethodPost, path, body, http.StatusNoContent, nil)
	return err
}

func (t *httpTransport) AdvanceToConstruction(ctx context.Context, projectID string) (bool, []string, error) {
	var out advanceResp
	path := fmt.Sprintf("/api/v1/projects/%s/project-design/advance", projectID)
	_, err := t.do(ctx, http.MethodPost, path, nil, http.StatusOK, &out)
	return out.Advanced, out.MissingArtifacts, err
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
	default:
		return fmt.Errorf("unexpected status %d", code)
	}
}
