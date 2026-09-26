package main

// setreviewpolicy_handler_test.go — wire-level handler tests for the generated
// POST /api/v1/delivery/set-project-execution-policy/{projectID} binding
// (local-merge-and-policy Commit 2; re-pointed at stage 4a, when the construction
// Manager's SetReviewPolicy became the delivery Manager's
// SetProjectExecutionPolicy over an ExecutionPolicyInput). The generated handler
// layer carries zero hand-written Go and no in-package tests (gen-client
// prunes/diffs that tree), so its bindings are exercised HERE in the composition
// root — the same module that mounts them in production (main.gen.go) — over a
// FakeDeliveryManager:
// the tests prove the route, the request decode, the principal/authorize
// pre-steps, and the manager-error → HTTP status mapping, not the manager logic
// itself (that is manager_test.go's job).

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	deliveryweb "github.com/mixofreality-studio/archistrator/server/internal/client/web/delivery"
	delivery "github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	deliveryfake "github.com/mixofreality-studio/archistrator/server/internal/manager/delivery/fake"
)

// newSetReviewPolicyMux mounts the generated delivery handler over the given fake
// manager with the same interim PDP production uses (authz.go).
func newSetReviewPolicyMux(mgr delivery.DeliveryManager) *http.ServeMux {
	mux := http.NewServeMux()
	h := &deliveryweb.Handler{
		Manager:  mgr,
		Security: security.New(security.WithPolicyDecisionPoint(authenticatedOnlyPDP{})),
	}
	h.Register(mux)
	return mux
}

// postSetReviewPolicy performs the wire call, optionally carrying an
// authenticated principal (the middleware's context contract).
func postSetReviewPolicy(mux *http.ServeMux, projectID, body string, authenticated bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/set-project-execution-policy/"+projectID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authenticated {
		req = req.WithContext(security.WithPrincipal(req.Context(), security.Principal{Subject: "tester"}))
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestSetReviewPolicyHandler_HappyPath(t *testing.T) {
	var gotProject delivery.ProjectID
	var gotPreset string
	fake := &deliveryfake.FakeDeliveryManager{
		SetProjectExecutionPolicyFn: func(_ fwmanager.Context, projectID delivery.ProjectID, policy delivery.ExecutionPolicyInput) error {
			gotProject, gotPreset = projectID, policy.Preset
			return nil
		},
	}
	rr := postSetReviewPolicy(newSetReviewPolicyMux(fake), "proj-1", `{"policy":{"preset":"checkpoints"}}`, true)
	// error-only op → the generated binding answers 204 No Content on success.
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body: %s)", rr.Code, rr.Body.String())
	}
	if gotProject != "proj-1" || gotPreset != "checkpoints" {
		t.Fatalf("manager received (%q, %q), want (proj-1, checkpoints)", gotProject, gotPreset)
	}
}

func TestSetReviewPolicyHandler_UnknownPreset_MapsContractMisuse(t *testing.T) {
	fake := &deliveryfake.FakeDeliveryManager{
		SetProjectExecutionPolicyFn: func(_ fwmanager.Context, _ delivery.ProjectID, policy delivery.ExecutionPolicyInput) error {
			return &fwmanager.Error{Kind: fwmanager.ContractMisuse, Detail: "unknown review-policy preset " + policy.Preset}
		},
	}
	rr := postSetReviewPolicy(newSetReviewPolicyMux(fake), "proj-1", `{"policy":{"preset":"yolo"}}`, true)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a ContractMisuse preset reject (body: %s)", rr.Code, rr.Body.String())
	}
}

func TestSetReviewPolicyHandler_Unauthenticated_401(t *testing.T) {
	fake := &deliveryfake.FakeDeliveryManager{
		SetProjectExecutionPolicyFn: func(_ fwmanager.Context, _ delivery.ProjectID, _ delivery.ExecutionPolicyInput) error {
			t.Error("manager must not be reached without a principal")
			return nil
		},
	}
	rr := postSetReviewPolicy(newSetReviewPolicyMux(fake), "proj-1", `{"policy":{"preset":"vibes"}}`, false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestSetReviewPolicyHandler_UnknownProject_404(t *testing.T) {
	fake := &deliveryfake.FakeDeliveryManager{
		SetProjectExecutionPolicyFn: func(_ fwmanager.Context, _ delivery.ProjectID, _ delivery.ExecutionPolicyInput) error {
			return &fwmanager.Error{Kind: fwmanager.NotFound, Detail: "no such project"}
		},
	}
	rr := postSetReviewPolicy(newSetReviewPolicyMux(fake), "ghost", `{"policy":{"preset":"vibes"}}`, true)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// STAGE 4a fix round 3 — CREATING A PROJECT IS REACHABLE OVER REST.
//
// StartProject folds create/adopt into one op and signals "create" by the ABSENCE of
// projectID. While the contract declared that param as `$ref: ProjectID`, the http
// generator routed it into a path segment (httpgen/plan.go isIDPathParam claims any
// scalar-string $def ending in "ID" and ignores `pointer`), and net/http's mux cannot
// match an empty segment — so POST .../start-project/ never routed and the create arm
// was reachable only over MCP, whose tool marks the field omitempty. The param now
// rides the BODY, the route has no path segment, and these two tests are the pin: one
// per arm, at the wire, over the generated handler.
// ---------------------------------------------------------------------------

func postStartProject(mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/start-project", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(security.WithPrincipal(req.Context(), security.Principal{Subject: "tester"}))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// Test_StartProjectHandler_NoProjectID_Creates is the arm the path form made
// unreachable: a body with no projectID must ROUTE and reach the manager with nil.
func Test_StartProjectHandler_NoProjectID_Creates(t *testing.T) {
	var gotID *string
	called := false
	fake := &deliveryfake.FakeDeliveryManager{
		StartProjectFn: func(_ fwmanager.Context, _ delivery.OwnerScope, name string, projectID *string, _ *delivery.OperatingModel, _ *delivery.ResearchInput, _ bool) (delivery.StartProjectResult, error) {
			called, gotID = true, projectID
			return delivery.StartProjectResult{ProjectID: delivery.ProjectID(name), Version: 1}, nil
		},
	}
	rr := postStartProject(newSetReviewPolicyMux(fake), `{"owner":"usr-1","name":"aiarch-demo","start":false}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a create must ROUTE without a path id (body: %s)", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatal("the manager was never reached; the route did not match")
	}
	if gotID != nil {
		t.Fatalf("projectID = %v, want nil — an absent id is what MEANS create", *gotID)
	}
}

// Test_StartProjectHandler_WithProjectID_Adopts is the other arm: the id rides the body
// and arrives verbatim. There is no route id to contradict it — the mismatch the path
// form made possible cannot be expressed any more.
func Test_StartProjectHandler_WithProjectID_Adopts(t *testing.T) {
	var gotID *string
	fake := &deliveryfake.FakeDeliveryManager{
		StartProjectFn: func(_ fwmanager.Context, _ delivery.OwnerScope, _ string, projectID *string, _ *delivery.OperatingModel, _ *delivery.ResearchInput, _ bool) (delivery.StartProjectResult, error) {
			gotID = projectID
			return delivery.StartProjectResult{ProjectID: "proj-1", Version: 4}, nil
		},
	}
	rr := postStartProject(newSetReviewPolicyMux(fake), `{"owner":"usr-1","name":"","projectID":"proj-1","start":true}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	if gotID == nil || *gotID != "proj-1" {
		t.Fatalf("projectID = %v, want \"proj-1\" carried through the body", gotID)
	}
}

// ---------------------------------------------------------------------------
// STAGE 4a PRE-FINAL — THE ADOPT ARM IS AUTHORIZED AGAINST THE PROJECT IT NAMES.
//
// Moving projectID off the path (above) also moved StartProject off the ONE mechanism
// that binds an authorization decision to a project: the generator turns a leading ID
// path param into Authorize(verb, {Kind:"project", ID:<id>}) and, with no path param,
// falls back to the owner-scoped {Kind:"deliveryCatalog", ID:principal.Subject}. So an
// adopt named a project the catalog decision never sees.
// projectScopedDeliveryManager (hooks.go) re-asks that same question at the composition
// root, over BOTH transports. These tests drive it through the generated handler with a
// PDP that denies ONE project, which is what proves the resource ref is really bound —
// a guard reading the wrong ref would pass every one of them but the first.
// ---------------------------------------------------------------------------

// oneProjectDenyingPDP permits everything except the named project resource, and records
// every ref it was asked about so a test can assert WHICH decisions were made.
type oneProjectDenyingPDP struct {
	deniedProject string
	asked         []security.ResourceRef
}

func (p *oneProjectDenyingPDP) Decide(_ context.Context, _ security.Principal, _ security.Action, resource security.ResourceRef) (bool, error) {
	p.asked = append(p.asked, resource)
	return resource.Kind != "project" || resource.ID != p.deniedProject, nil
}

// newGuardedStartProjectMux mounts the generated handler over the SAME guarded manager
// production mounts (the WrapManagers composition, minus the logging seam's logger
// noise), with one PDP answering both the handler's catalog decision and the guard's
// project decision.
func newGuardedStartProjectMux(mgr delivery.DeliveryManager, pdp security.PolicyDecisionPoint) *http.ServeMux {
	sec := security.New(security.WithPolicyDecisionPoint(pdp))
	mux := http.NewServeMux()
	h := &deliveryweb.Handler{
		Manager:  projectScopedDeliveryManager{DeliveryManager: mgr, security: sec},
		Security: sec,
	}
	h.Register(mux)
	return mux
}

func Test_StartProjectAdopt_DeniedProject_403_AndNeverReachesTheManager(t *testing.T) {
	fake := &deliveryfake.FakeDeliveryManager{
		StartProjectFn: func(_ fwmanager.Context, _ delivery.OwnerScope, _ string, _ *string, _ *delivery.OperatingModel, _ *delivery.ResearchInput, _ bool) (delivery.StartProjectResult, error) {
			t.Error("the manager must not be reached for a project the principal may not act on")
			return delivery.StartProjectResult{}, nil
		},
	}
	pdp := &oneProjectDenyingPDP{deniedProject: "proj-secret"}

	rr := postStartProject(newGuardedStartProjectMux(fake, pdp), `{"owner":"usr-1","name":"","projectID":"proj-secret","start":true}`)

	// 403 is what the path route gave (fwmanager.Unauthorized → StatusForbidden,
	// statusForKind in the generated handler).
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for an adopt of a project the principal may not act on (body: %s)", rr.Code, rr.Body.String())
	}
	var sawProject bool
	for _, ref := range pdp.asked {
		if ref.Kind == "project" && ref.ID == "proj-secret" {
			sawProject = true
		}
	}
	if !sawProject {
		t.Fatalf("no {project, proj-secret} decision was ever asked; refs asked = %+v", pdp.asked)
	}
}

func Test_StartProjectAdopt_PermittedProject_Adopts(t *testing.T) {
	var gotID *string
	fake := &deliveryfake.FakeDeliveryManager{
		StartProjectFn: func(_ fwmanager.Context, _ delivery.OwnerScope, _ string, projectID *string, _ *delivery.OperatingModel, _ *delivery.ResearchInput, _ bool) (delivery.StartProjectResult, error) {
			gotID = projectID
			return delivery.StartProjectResult{ProjectID: "proj-1", Version: 4}, nil
		},
	}
	pdp := &oneProjectDenyingPDP{deniedProject: "proj-secret"}

	rr := postStartProject(newGuardedStartProjectMux(fake, pdp), `{"owner":"usr-1","name":"","projectID":"proj-1","start":true}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — this principal may adopt proj-1 (body: %s)", rr.Code, rr.Body.String())
	}
	if gotID == nil || *gotID != "proj-1" {
		t.Fatalf("projectID = %v, want \"proj-1\" carried through to the manager", gotID)
	}
}

func Test_StartProjectCreate_AsksNoProjectDecision(t *testing.T) {
	fake := &deliveryfake.FakeDeliveryManager{
		StartProjectFn: func(_ fwmanager.Context, _ delivery.OwnerScope, name string, _ *string, _ *delivery.OperatingModel, _ *delivery.ResearchInput, _ bool) (delivery.StartProjectResult, error) {
			return delivery.StartProjectResult{ProjectID: delivery.ProjectID(name), Version: 1}, nil
		},
	}
	pdp := &oneProjectDenyingPDP{deniedProject: "proj-secret"}

	rr := postStartProject(newGuardedStartProjectMux(fake, pdp), `{"owner":"usr-1","name":"aiarch-demo","start":false}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	// A create names no project, so the catalog decision is the only one there is.
	for _, ref := range pdp.asked {
		if ref.Kind == "project" {
			t.Fatalf("a create asked a project-scoped decision (%+v) — there is no project yet to name", ref)
		}
	}
	if len(pdp.asked) != 1 || pdp.asked[0].Kind != "deliveryCatalog" {
		t.Fatalf("refs asked = %+v, want exactly the one deliveryCatalog decision", pdp.asked)
	}
}

// The guard is only real if the composition root INSTALLS it: WrapManagers is the one
// place that does, and it is what both the REST handler and the MCP tools are handed.
func Test_WrapManagers_InstallsTheDeliveryAdoptGuard(t *testing.T) {
	h := &appHooks{logger: slog.New(slog.DiscardHandler)}
	wrapped := h.WrapManagers(WebManagers{DeliveryManager: &deliveryfake.FakeDeliveryManager{}})
	if _, ok := wrapped.DeliveryManager.(projectScopedDeliveryManager); !ok {
		t.Fatalf("WrapManagers returned %T; the delivery adopt guard is not installed", wrapped.DeliveryManager)
	}
}
