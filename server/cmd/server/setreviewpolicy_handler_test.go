package main

// setreviewpolicy_handler_test.go — wire-level handler tests for the generated
// POST /api/v1/construction/set-review-policy/{projectID} binding
// (local-merge-and-policy Commit 2). The generated handler layer carries zero
// hand-written Go and no in-package tests (gen-client prunes/diffs that tree),
// so its bindings are exercised HERE in the composition root — the same module
// that mounts them in production (main.gen.go) — over a FakeConstructionManager:
// the tests prove the route, the request decode, the principal/authorize
// pre-steps, and the manager-error → HTTP status mapping, not the manager logic
// itself (that is manager_test.go's job).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	constructionweb "github.com/mixofreality-studio/archistrator/server/internal/client/web/construction"
	construction "github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	constructionfake "github.com/mixofreality-studio/archistrator/server/internal/manager/construction/fake"
)

// newSetReviewPolicyMux mounts the generated construction handler over the
// given fake manager with the same interim PDP production uses (authz.go).
func newSetReviewPolicyMux(mgr construction.ConstructionManager) *http.ServeMux {
	mux := http.NewServeMux()
	h := &constructionweb.Handler{
		Manager:  mgr,
		Security: security.New(security.WithPolicyDecisionPoint(authenticatedOnlyPDP{})),
	}
	h.Register(mux)
	return mux
}

// postSetReviewPolicy performs the wire call, optionally carrying an
// authenticated principal (the middleware's context contract).
func postSetReviewPolicy(mux *http.ServeMux, projectID, body string, authenticated bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/construction/set-review-policy/"+projectID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authenticated {
		req = req.WithContext(security.WithPrincipal(req.Context(), security.Principal{Subject: "tester"}))
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestSetReviewPolicyHandler_HappyPath(t *testing.T) {
	var gotProject construction.ProjectID
	var gotPreset string
	fake := &constructionfake.FakeConstructionManager{
		SetReviewPolicyFn: func(_ fwmanager.Context, projectID construction.ProjectID, preset string) error {
			gotProject, gotPreset = projectID, preset
			return nil
		},
	}
	rr := postSetReviewPolicy(newSetReviewPolicyMux(fake), "proj-1", `{"preset":"checkpoints"}`, true)
	// error-only op → the generated binding answers 204 No Content on success.
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body: %s)", rr.Code, rr.Body.String())
	}
	if gotProject != "proj-1" || gotPreset != "checkpoints" {
		t.Fatalf("manager received (%q, %q), want (proj-1, checkpoints)", gotProject, gotPreset)
	}
}

func TestSetReviewPolicyHandler_UnknownPreset_MapsContractMisuse(t *testing.T) {
	fake := &constructionfake.FakeConstructionManager{
		SetReviewPolicyFn: func(_ fwmanager.Context, _ construction.ProjectID, preset string) error {
			return &fwmanager.Error{Kind: fwmanager.ContractMisuse, Detail: "unknown review-policy preset " + preset}
		},
	}
	rr := postSetReviewPolicy(newSetReviewPolicyMux(fake), "proj-1", `{"preset":"yolo"}`, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a ContractMisuse preset reject (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestSetReviewPolicyHandler_Unauthenticated_401(t *testing.T) {
	fake := &constructionfake.FakeConstructionManager{
		SetReviewPolicyFn: func(_ fwmanager.Context, _ construction.ProjectID, _ string) error {
			t.Error("manager must not be reached without a principal")
			return nil
		},
	}
	rr := postSetReviewPolicy(newSetReviewPolicyMux(fake), "proj-1", `{"preset":"vibes"}`, false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestSetReviewPolicyHandler_UnknownProject_404(t *testing.T) {
	fake := &constructionfake.FakeConstructionManager{
		SetReviewPolicyFn: func(_ fwmanager.Context, _ construction.ProjectID, _ string) error {
			return &fwmanager.Error{Kind: fwmanager.NotFound, Detail: "no such project"}
		},
	}
	rr := postSetReviewPolicy(newSetReviewPolicyMux(fake), "ghost", `{"preset":"vibes"}`, true)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", rr.Code, rr.Body.String())
	}
}
