package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/davidmarne/archistrator/server/internal/manager/operations"
	fwmgr "github.com/davidmarne/archistrator-platform/framework-go/manager"
	"github.com/davidmarne/archistrator-platform/framework-go/utilities/security"
	"github.com/google/uuid"
)

// These are BLACK-BOX HTTP-level tests of the UC4 operateDeliveredSystem facet.
// They drive the REAL Routes() handler end-to-end (auth middleware → security gate →
// handler → narrow OperationsEntry port → wire DTO), using a hand-written fake that
// satisfies the narrow OperationsEntry port (NOT a mock of the Manager's Temporal
// internals) and a permit/deny fake security Utility. Each operate route is covered
// for the success path, the authorize-deny path, and a bad-input path.

// --- fakes -----------------------------------------------------------------

// fakeOperations is a hand-written fake satisfying the narrow OperationsEntry port.
// It records the last call and returns canned results / errors, so a test can assert
// the handler relayed EXACTLY the right typed input to the right op.
type fakeOperations struct {
	deployCalls   int
	lastDeployApp operations.OperatedAppID
	lastChange    operations.DesiredStateChange
	deployResult  operations.DeployResult
	deployErr     error

	withdrawCalls   int
	lastWithdrawApp operations.OperatedAppID
	lastWithdrawCID string
	lastWithdrawRsn operations.WithdrawReason
	withdrawResult  operations.WithdrawResult
	withdrawErr     error

	costCalls      int
	lastCostApp    operations.OperatedAppID
	lastCostReqID  string
	lastCostPoints *operations.ScaleWhatIfPoints
	costResult     operations.CostProjection
	costErr        error

	viewCalls     int
	lastViewApp   operations.OperatedAppID
	lastViewReqID string
	viewResult    operations.OperatedSystemView
	viewErr       error
}

func (f *fakeOperations) DeployAfterConstruction(_ context.Context, appID operations.OperatedAppID, change operations.DesiredStateChange) (operations.DeployResult, error) {
	f.deployCalls++
	f.lastDeployApp = appID
	f.lastChange = change
	return f.deployResult, f.deployErr
}

func (f *fakeOperations) WithdrawSystem(_ context.Context, appID operations.OperatedAppID, changeID string, reason operations.WithdrawReason) (operations.WithdrawResult, error) {
	f.withdrawCalls++
	f.lastWithdrawApp = appID
	f.lastWithdrawCID = changeID
	f.lastWithdrawRsn = reason
	return f.withdrawResult, f.withdrawErr
}

func (f *fakeOperations) QueryCostProjection(_ context.Context, appID operations.OperatedAppID, requestID string, points *operations.ScaleWhatIfPoints) (operations.CostProjection, error) {
	f.costCalls++
	f.lastCostApp = appID
	f.lastCostReqID = requestID
	f.lastCostPoints = points
	return f.costResult, f.costErr
}

func (f *fakeOperations) QueryOperatedSystemView(_ context.Context, appID operations.OperatedAppID, requestID string) (operations.OperatedSystemView, error) {
	f.viewCalls++
	f.lastViewApp = appID
	f.lastViewReqID = requestID
	return f.viewResult, f.viewErr
}

// compile-time proof the fake satisfies the narrow port the handlers depend on.
var _ OperationsEntry = (*fakeOperations)(nil)

// fakeSecurity is a permit/deny fake of the security Utility. permit toggles the
// Authorize outcome; the other two ops are unused on this surface.
type fakeSecurity struct{ permit bool }

func (s fakeSecurity) Authorize(context.Context, security.SecurityPrincipal, security.Action, security.ResourceRef) (security.Decision, error) {
	if s.permit {
		return security.Decision{Permit: true, Reason: security.ReasonPermitted}, nil
	}
	return security.Decision{Permit: false, Reason: security.ReasonNotPermitted}, nil
}

func (fakeSecurity) VerifyWebhookSignature(context.Context, security.WebhookChannel, []byte, security.SignatureMaterial) error {
	return errors.New("unused")
}

func (fakeSecurity) ObtainServiceIdentity(context.Context, security.ServiceAudience) (security.ServiceCredential, error) {
	return security.ServiceCredential{}, errors.New("unused")
}

var _ security.Security = fakeSecurity{}

// newTestHandler builds the REAL Routes() handler over the fakes, with dev-mode auth
// injecting a principal (so the security gate runs against fakeSecurity, not the token
// validator).
func newTestHandler(ops OperationsEntry, permit bool) http.Handler {
	c := NewClient(nil, nil, nil, nil, ops, fakeSecurity{permit: permit}, "")
	dev := DevConfig{Enabled: true, Principal: security.SecurityPrincipal{
		Kind:    security.PrincipalUser,
		Subject: "test-operator",
	}}
	return c.Routes(AuthMiddleware(dev, nil))
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --- Deploy ----------------------------------------------------------------

func TestDeploy_Success(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{deployResult: operations.DeployResult{Published: true, Revision: "rev-1"}}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/deploy",
		deployRequest{ChangeID: "deploy-1"})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if ops.deployCalls != 1 {
		t.Fatalf("deploy called %d times, want 1", ops.deployCalls)
	}
	if ops.lastDeployApp != appID {
		t.Fatalf("relayed appID %v, want %v", ops.lastDeployApp, appID)
	}
	if ops.lastChange.Reason != operations.ReasonDeployAfterConstruction {
		t.Fatalf("reason = %v, want deployAfterConstruction", ops.lastChange.Reason)
	}
	if ops.lastChange.PatchKind != operations.PatchFullBundle {
		t.Fatalf("patchKind = %v, want PatchFullBundle", ops.lastChange.PatchKind)
	}
	if ops.lastChange.ChangeID != "deploy-1" {
		t.Fatalf("changeID = %q, want deploy-1", ops.lastChange.ChangeID)
	}
	var resp deployResultResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if !resp.Published || resp.Revision != "rev-1" || resp.OperatedAppID != appID.String() {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestDeploy_AuthDeny(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, false) // deny

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/deploy",
		deployRequest{ChangeID: "deploy-1"})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ops.deployCalls != 0 {
		t.Fatalf("deploy called %d times on deny, want 0 (authorize BEFORE Manager call)", ops.deployCalls)
	}
}

func TestDeploy_BadInput_NonUUID(t *testing.T) {
	ops := &fakeOperations{}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/not-a-uuid/deploy",
		deployRequest{ChangeID: "deploy-1"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ops.deployCalls != 0 {
		t.Fatalf("deploy called %d times on bad input, want 0", ops.deployCalls)
	}
}

// --- Scale -----------------------------------------------------------------

func TestScale_Success(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{deployResult: operations.DeployResult{Published: true}}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/scale",
		scaleRequest{ChangeID: "scale-1"})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if ops.deployCalls != 1 {
		t.Fatalf("deploy called %d times, want 1 (scale folds onto DeployAfterConstruction)", ops.deployCalls)
	}
	if ops.lastChange.Reason != operations.ReasonOperator {
		t.Fatalf("reason = %v, want operator", ops.lastChange.Reason)
	}
	if ops.lastChange.PatchKind != operations.PatchScale {
		t.Fatalf("patchKind = %v, want PatchScale", ops.lastChange.PatchKind)
	}
}

func TestScale_AuthDeny(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, false)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/scale",
		scaleRequest{ChangeID: "scale-1"})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ops.deployCalls != 0 {
		t.Fatalf("deploy called %d times on deny, want 0", ops.deployCalls)
	}
}

func TestScale_BadInput_UnknownField(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, true)

	// DisallowUnknownFields → a stray field is a 400 before any Manager call.
	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/scale",
		map[string]any{"changeId": "scale-1", "bogus": true})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ops.deployCalls != 0 {
		t.Fatalf("deploy called %d times on bad input, want 0", ops.deployCalls)
	}
}

// --- UpdateAutoscalerPolicy ------------------------------------------------

func TestUpdateAutoscalerPolicy_Success(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{deployResult: operations.DeployResult{Published: true}}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/autoscaler-policy",
		updateAutoscalerPolicyRequest{ChangeID: "policy-1"})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if ops.lastChange.Reason != operations.ReasonOperator {
		t.Fatalf("reason = %v, want operator", ops.lastChange.Reason)
	}
	if ops.lastChange.PatchKind != operations.PatchPolicy {
		t.Fatalf("patchKind = %v, want PatchPolicy", ops.lastChange.PatchKind)
	}
}

func TestUpdateAutoscalerPolicy_AuthDeny(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, false)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/autoscaler-policy",
		updateAutoscalerPolicyRequest{ChangeID: "policy-1"})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ops.deployCalls != 0 {
		t.Fatalf("deploy called %d times on deny, want 0", ops.deployCalls)
	}
}

func TestUpdateAutoscalerPolicy_BadInput_NonUUID(t *testing.T) {
	ops := &fakeOperations{}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/xyz/autoscaler-policy",
		updateAutoscalerPolicyRequest{ChangeID: "policy-1"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// --- Withdraw --------------------------------------------------------------

func TestWithdraw_Success(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{withdrawResult: operations.WithdrawResult{Withdrawn: true}}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/withdraw",
		withdrawRequest{ChangeID: "wd-1", Reason: "cost overrun"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ops.withdrawCalls != 1 {
		t.Fatalf("withdraw called %d times, want 1", ops.withdrawCalls)
	}
	if ops.lastWithdrawApp != appID {
		t.Fatalf("relayed appID %v, want %v", ops.lastWithdrawApp, appID)
	}
	if ops.lastWithdrawCID != "wd-1" {
		t.Fatalf("changeID = %q, want wd-1", ops.lastWithdrawCID)
	}
	if ops.lastWithdrawRsn.Notes != "cost overrun" {
		t.Fatalf("reason notes = %q, want 'cost overrun'", ops.lastWithdrawRsn.Notes)
	}
	var resp withdrawResultResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if !resp.Withdrawn || resp.OperatedAppID != appID.String() {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestWithdraw_AuthDeny(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, false)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/withdraw",
		withdrawRequest{ChangeID: "wd-1"})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ops.withdrawCalls != 0 {
		t.Fatalf("withdraw called %d times on deny, want 0", ops.withdrawCalls)
	}
}

func TestWithdraw_BadInput_NonUUID(t *testing.T) {
	ops := &fakeOperations{}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/nope/withdraw",
		withdrawRequest{ChangeID: "wd-1"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ops.withdrawCalls != 0 {
		t.Fatalf("withdraw called %d times on bad input, want 0", ops.withdrawCalls)
	}
}

// handleWithdraw maps the Manager's NotFound error to 404 via writeManagerError.
func TestWithdraw_ManagerError_NotFound(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{withdrawErr: fwmgr.New(fwmgr.NotFound, "no such app")}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/operations/"+appID.String()+"/withdraw",
		withdrawRequest{ChangeID: "wd-1"})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// --- QueryCostProjection ---------------------------------------------------

func TestQueryCostProjection_Success(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{costResult: operations.CostProjection{
		CurrentRunRate:       operations.Money{MinorUnits: 1000, Currency: "USD"},
		ProjectedMonthlyCost: operations.Money{MinorUnits: 720000, Currency: "USD"},
		ScaleWhatIfCurve: operations.WhatIfCurve{Points: []operations.WhatIfPoint{
			{Replicas: 1, ProjectedMonthlyCost: operations.Money{MinorUnits: 360000, Currency: "USD"}},
			{Replicas: 3, ProjectedMonthlyCost: operations.Money{MinorUnits: 1080000, Currency: "USD"}},
		}},
	}}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/operations/"+appID.String()+"/cost-projection?requestId=req-1&scaleWhatIfPoints=1,3", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ops.costCalls != 1 {
		t.Fatalf("cost called %d times, want 1", ops.costCalls)
	}
	if ops.lastCostReqID != "req-1" {
		t.Fatalf("requestID = %q, want req-1", ops.lastCostReqID)
	}
	if ops.lastCostPoints == nil || len(ops.lastCostPoints.Points) != 2 ||
		ops.lastCostPoints.Points[0].Replicas != 1 || ops.lastCostPoints.Points[1].Replicas != 3 {
		t.Fatalf("scaleWhatIfPoints = %+v, want [1 3]", ops.lastCostPoints)
	}
	var resp costProjectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.OperatedAppID != appID.String() {
		t.Fatalf("operatedAppId = %q, want %q", resp.OperatedAppID, appID.String())
	}
	if resp.CurrentRunRate.MinorUnits != 1000 || resp.CurrentRunRate.Currency != "USD" {
		t.Fatalf("currentRunRate = %+v", resp.CurrentRunRate)
	}
	if len(resp.ScaleWhatIfCurve) != 2 || resp.ScaleWhatIfCurve[1].Replicas != 3 {
		t.Fatalf("curve = %+v", resp.ScaleWhatIfCurve)
	}
}

func TestQueryCostProjection_NoWhatIf(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/operations/"+appID.String()+"/cost-projection?requestId=req-1", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ops.lastCostPoints != nil {
		t.Fatalf("scaleWhatIfPoints = %+v, want nil (none requested)", ops.lastCostPoints)
	}
}

func TestQueryCostProjection_AuthDeny(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, false)

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/operations/"+appID.String()+"/cost-projection?requestId=req-1", nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ops.costCalls != 0 {
		t.Fatalf("cost called %d times on deny, want 0", ops.costCalls)
	}
}

func TestQueryCostProjection_BadInput_MissingRequestID(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/operations/"+appID.String()+"/cost-projection", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (requestId required)", rec.Code)
	}
	if ops.costCalls != 0 {
		t.Fatalf("cost called %d times on bad input, want 0", ops.costCalls)
	}
}

func TestQueryCostProjection_BadInput_NonIntWhatIf(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/operations/"+appID.String()+"/cost-projection?requestId=req-1&scaleWhatIfPoints=1,x", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (non-int what-if)", rec.Code)
	}
	if ops.costCalls != 0 {
		t.Fatalf("cost called %d times on bad input, want 0", ops.costCalls)
	}
}

// --- GetOperatedSystemView (Contract 2 — IWebReadModel; the U-SPA-4 read path) ----

func TestGetOperatedSystemView_Success(t *testing.T) {
	appID := uuid.New()
	at := time.Date(2026, 6, 11, 8, 30, 0, 0, time.UTC)
	ops := &fakeOperations{viewResult: operations.OperatedSystemView{
		OperatedAppID: appID,
		Phase:         operations.RuntimeStatusHealthy,
		InFlight:      true,
		Health: operations.HealthSnapshotView{
			SloMet: true, Detail: "99.9% / 30d", Phase: operations.RuntimeStatusHealthy,
		},
		Slos: []operations.SloRowView{
			{Component: "api", Objective: "99.9% / 30d", SloMet: true, Healthy: true},
			{Component: "worker", Objective: "lag < 60s", SloMet: false, Healthy: true},
		},
		RecentEvents: []operations.RuntimeStatusEventView{
			{At: at, From: operations.RuntimeStatusPending, To: operations.RuntimeStatusHealthy, Note: "converged"},
		},
		Autoscaler: operations.AutoscalerView{
			Mode: operations.AutoscalerModeAuto,
			Decisions: []operations.AutoscaleDecisionView{
				{At: at, Action: operations.AutoscaleScaleUp, Reason: "rps spike", Published: true},
				{At: at, Action: operations.AutoscaleNoChange, Reason: "quiet", Published: false},
			},
		},
		CurrentRunRate: operations.Money{MinorUnits: 4120, Currency: "USD"},
	}}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/operations/"+appID.String()+"/view?requestId=view-1", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ops.viewCalls != 1 {
		t.Fatalf("view called %d times, want 1", ops.viewCalls)
	}
	if ops.lastViewApp != appID {
		t.Fatalf("relayed appID %v, want %v", ops.lastViewApp, appID)
	}
	if ops.lastViewReqID != "view-1" {
		t.Fatalf("requestID = %q, want view-1", ops.lastViewReqID)
	}

	var resp operatedSystemViewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.OperatedAppID != appID.String() {
		t.Fatalf("operatedAppId = %q, want %q", resp.OperatedAppID, appID.String())
	}
	if resp.Phase != "healthy" || !resp.InFlight {
		t.Fatalf("phase/inFlight = %q/%v, want healthy/true", resp.Phase, resp.InFlight)
	}
	if !resp.Health.SloMet || resp.Health.Detail != "99.9% / 30d" || resp.Health.Phase != "healthy" {
		t.Fatalf("health = %+v", resp.Health)
	}
	if len(resp.Slos) != 2 || resp.Slos[0].Component != "api" || resp.Slos[1].SloMet {
		t.Fatalf("slos = %+v", resp.Slos)
	}
	if len(resp.RecentEvents) != 1 || resp.RecentEvents[0].From != "pending" || resp.RecentEvents[0].To != "healthy" {
		t.Fatalf("recentEvents = %+v", resp.RecentEvents)
	}
	if resp.RecentEvents[0].At != "2026-06-11T08:30:00Z" {
		t.Fatalf("event At = %q, want RFC3339", resp.RecentEvents[0].At)
	}
	if resp.Autoscaler.Mode != "auto" || len(resp.Autoscaler.Decisions) != 2 {
		t.Fatalf("autoscaler = %+v", resp.Autoscaler)
	}
	if resp.Autoscaler.Decisions[0].Action != "scaleUp" || !resp.Autoscaler.Decisions[0].Published {
		t.Fatalf("decision[0] = %+v", resp.Autoscaler.Decisions[0])
	}
	if resp.Autoscaler.Decisions[1].Action != "noChange" {
		t.Fatalf("decision[1] action = %q, want noChange", resp.Autoscaler.Decisions[1].Action)
	}
	if resp.CurrentRunRate.MinorUnits != 4120 || resp.CurrentRunRate.Currency != "USD" {
		t.Fatalf("currentRunRate = %+v", resp.CurrentRunRate)
	}
}

func TestGetOperatedSystemView_BadInput_MissingRequestID(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, true)

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/operations/"+appID.String()+"/view", nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (requestId required)", rec.Code)
	}
	if ops.viewCalls != 0 {
		t.Fatalf("view called %d times on bad input, want 0", ops.viewCalls)
	}
}

func TestGetOperatedSystemView_AuthDeny(t *testing.T) {
	appID := uuid.New()
	ops := &fakeOperations{}
	h := newTestHandler(ops, false) // deny

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/operations/"+appID.String()+"/view?requestId=view-1", nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ops.viewCalls != 0 {
		t.Fatalf("view called %d times on deny, want 0 (authorize BEFORE Manager call)", ops.viewCalls)
	}
}
