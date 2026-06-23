package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/client/mcp"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/settlement"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
)

// These tests exercise the three mcpClient ops (ListTools, InvokeTool,
// ReadProjectState) using hand-written fakes that satisfy the narrow Manager
// entry ports. They confirm: the static catalog is non-empty, authorization is
// gated before every Manager call, unknown tool names are rejected, and Manager
// errors are mapped to ToolError.

// ---------------------------------------------------------------------------
// fakes
// ---------------------------------------------------------------------------

type fakeProject struct {
	createResult project.ProjectID
	createErr    error
	listResult   []project.ProjectSummary
	listErr      error
	getResult    project.ProjectState
	getErr       error
}

func (f *fakeProject) CreateProject(_ context.Context, _ project.OwnerScope, _ string) (project.ProjectID, error) {
	return f.createResult, f.createErr
}
func (f *fakeProject) ListProjects(_ context.Context, _ project.OwnerScope) ([]project.ProjectSummary, error) {
	return f.listResult, f.listErr
}
func (f *fakeProject) GetProject(_ context.Context, _ project.ProjectID) (project.ProjectState, error) {
	return f.getResult, f.getErr
}

type fakeSystemDesign struct {
	startResult     systemdesign.SessionRef
	startErr        error
	setResearchErr  error
	requestDraftRef systemdesign.SessionRef
	requestDraftErr error
	reviewErr       error
	advanceResult   systemdesign.PhaseAdvanceResult
	advanceErr      error
	stateResult     systemdesign.SessionStateView
	stateErr        error
}

func (f *fakeSystemDesign) StartSystemDesign(_ context.Context, _ systemdesign.ProjectID) (systemdesign.SessionRef, error) {
	return f.startResult, f.startErr
}
func (f *fakeSystemDesign) SetResearchInput(_ context.Context, _ systemdesign.ProjectID, _ systemdesign.ResearchInput) (systemdesign.Version, error) {
	return 0, f.setResearchErr
}
func (f *fakeSystemDesign) RequestArtifactDraft(_ context.Context, _ systemdesign.ProjectID, _ systemdesign.ArtifactKind, _ *systemdesign.ReviewFeedback) (systemdesign.SessionRef, error) {
	return f.requestDraftRef, f.requestDraftErr
}
func (f *fakeSystemDesign) SubmitReviewDecision(_ context.Context, _ systemdesign.ProjectID, _ systemdesign.ArtifactKind, _ systemdesign.ReviewDecision, _ *systemdesign.ReviewFeedback) error {
	return f.reviewErr
}
func (f *fakeSystemDesign) AdvancePhase(_ context.Context, _ systemdesign.ProjectID) (systemdesign.PhaseAdvanceResult, error) {
	return f.advanceResult, f.advanceErr
}
func (f *fakeSystemDesign) GetSessionState(_ context.Context, _ systemdesign.ProjectID, _ systemdesign.ArtifactKind) (systemdesign.SessionStateView, error) {
	return f.stateResult, f.stateErr
}

type fakeProjectDesign struct {
	requestDraftRef projectdesign.SessionRef
	requestDraftErr error
	reviewErr       error
	sdpRef          projectdesign.SessionRef
	sdpRefErr       error
	sdpDecisionErr  error
	advanceResult   projectdesign.PhaseAdvanceResult
	advanceErr      error
	stateResult     projectdesign.SessionStateView
	stateErr        error
}

func (f *fakeProjectDesign) RequestArtifactDraft(_ context.Context, _ projectdesign.ProjectID, _ projectdesign.ArtifactKind, _ *projectdesign.ReviewFeedback) (projectdesign.SessionRef, error) {
	return f.requestDraftRef, f.requestDraftErr
}
func (f *fakeProjectDesign) SubmitReviewDecision(_ context.Context, _ projectdesign.ProjectID, _ projectdesign.ArtifactKind, _ projectdesign.ReviewDecision, _ *projectdesign.ReviewFeedback) error {
	return f.reviewErr
}
func (f *fakeProjectDesign) RequestSDPCommit(_ context.Context, _ projectdesign.ProjectID) (projectdesign.SessionRef, error) {
	return f.sdpRef, f.sdpRefErr
}
func (f *fakeProjectDesign) SubmitSDPDecision(_ context.Context, _ projectdesign.ProjectID, _ projectdesign.SDPDecision, _ *projectdesign.OptionID, _ *projectdesign.ReviewFeedback) error {
	return f.sdpDecisionErr
}
func (f *fakeProjectDesign) AdvanceToConstruction(_ context.Context, _ projectdesign.ProjectID) (projectdesign.PhaseAdvanceResult, error) {
	return f.advanceResult, f.advanceErr
}
func (f *fakeProjectDesign) GetSessionState(_ context.Context, _ projectdesign.ProjectID, _ projectdesign.ArtifactKind) (projectdesign.SessionStateView, error) {
	return f.stateResult, f.stateErr
}

type fakeConstruction struct {
	stateResult   construction.ConstructionSessionView
	stateErr      error
	pauseErr      error
	overrideErr   error
	executeResult construction.PumpResult
	executeErr    error
}

func (f *fakeConstruction) GetSessionState(_ context.Context, _ construction.ProjectID, _ *construction.ActivityID) (construction.ConstructionSessionView, error) {
	return f.stateResult, f.stateErr
}
func (f *fakeConstruction) PauseProject(_ context.Context, _ construction.ProjectID, _ string) error {
	return f.pauseErr
}
func (f *fakeConstruction) OverrideActivity(_ context.Context, _ construction.ProjectID, _ construction.ActivityID, _ construction.ActivityOverride) error {
	return f.overrideErr
}
func (f *fakeConstruction) ExecuteNextActivity(_ context.Context, _ construction.ProjectID, _ string) (construction.PumpResult, error) {
	return f.executeResult, f.executeErr
}

type fakeOperations struct {
	deployResult   operations.DeployResult
	deployErr      error
	withdrawResult operations.WithdrawResult
	withdrawErr    error
	costResult     operations.CostProjection
	costErr        error
	viewResult     operations.OperatedSystemView
	viewErr        error
}

func (f *fakeOperations) DeployAfterConstruction(_ context.Context, _ operations.OperatedAppID, _ operations.DesiredStateChange) (operations.DeployResult, error) {
	return f.deployResult, f.deployErr
}
func (f *fakeOperations) WithdrawSystem(_ context.Context, _ operations.OperatedAppID, _ string, _ operations.WithdrawReason) (operations.WithdrawResult, error) {
	return f.withdrawResult, f.withdrawErr
}
func (f *fakeOperations) QueryCostProjection(_ context.Context, _ operations.OperatedAppID, _ string, _ *operations.ScaleWhatIfPoints) (operations.CostProjection, error) {
	return f.costResult, f.costErr
}
func (f *fakeOperations) QueryOperatedSystemView(_ context.Context, _ operations.OperatedAppID, _ string) (operations.OperatedSystemView, error) {
	return f.viewResult, f.viewErr
}

type fakeBilling struct {
	onboardResult settlement.SettlementRef
	onboardErr    error
}

func (f *fakeBilling) OnboardPaymentIntegration(_ context.Context, _ settlement.DeployedAppID) (settlement.SettlementRef, error) {
	return f.onboardResult, f.onboardErr
}

// permitSecurity always permits.
type permitSecurity struct{}

func (permitSecurity) Authorize(_ context.Context, _ security.SecurityPrincipal, _ security.Action, _ security.ResourceRef) (security.Decision, error) {
	return security.Decision{Permit: true}, nil
}
func (permitSecurity) VerifyWebhookSignature(_ context.Context, _ security.WebhookChannel, _ []byte, _ security.SignatureMaterial) error {
	return nil
}
func (permitSecurity) ObtainServiceIdentity(_ context.Context, _ security.ServiceAudience) (security.ServiceCredential, error) {
	return security.ServiceCredential{}, nil
}

// denySecurity always denies.
type denySecurity struct{}

func (denySecurity) Authorize(_ context.Context, _ security.SecurityPrincipal, _ security.Action, _ security.ResourceRef) (security.Decision, error) {
	return security.Decision{Permit: false}, nil
}
func (denySecurity) VerifyWebhookSignature(_ context.Context, _ security.WebhookChannel, _ []byte, _ security.SignatureMaterial) error {
	return errors.New("deny")
}
func (denySecurity) ObtainServiceIdentity(_ context.Context, _ security.ServiceAudience) (security.ServiceCredential, error) {
	return security.ServiceCredential{}, errors.New("deny")
}

// newTestClient builds a Client wired to the supplied fakes.
func newTestClient(proj *fakeProject, sd *fakeSystemDesign, pd *fakeProjectDesign, con *fakeConstruction, ops *fakeOperations, bill *fakeBilling, sec security.Security) *mcp.Client {
	return mcp.NewClient(proj, sd, pd, con, ops, bill, sec)
}

var testPrincipal = security.SecurityPrincipal{
	Kind:    security.PrincipalUser,
	Subject: "test-user",
	Email:   "test@example.com",
}

// ---------------------------------------------------------------------------
// ListTools tests
// ---------------------------------------------------------------------------

func TestListTools_ReturnsCatalog(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	catalog, toolErr := c.ListTools(context.Background(), testPrincipal)
	if toolErr != nil {
		t.Fatalf("ListTools returned unexpected error: %v", toolErr)
	}
	if len(catalog.Tools) == 0 {
		t.Error("ListTools returned an empty catalog; want at least one tool")
	}
}

func TestListTools_ContainsExpectedTools(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	catalog, _ := c.ListTools(context.Background(), testPrincipal)

	want := map[mcp.ToolName]bool{
		"project__create":               true,
		"project__list":                 true,
		"project__get":                  true,
		"system_design__start":          true,
		"system_design__request_draft":  true,
		"system_design__submit_review":  true,
		"system_design__advance":        true,
		"project_design__request_draft": true,
		"construction__execute_next":    true,
		"operations__deploy":            true,
		"billing__onboard":              true,
	}
	got := make(map[mcp.ToolName]bool, len(catalog.Tools))
	for _, t := range catalog.Tools {
		got[t.ToolName] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("ListTools: want tool %q in catalog", name)
		}
	}
}

func TestListTools_EachToolHasTitleAndInputSchema(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	catalog, _ := c.ListTools(context.Background(), testPrincipal)
	for _, td := range catalog.Tools {
		if td.Title == "" {
			t.Errorf("tool %q has empty Title", td.ToolName)
		}
		if td.InputSchema == "" {
			t.Errorf("tool %q has empty InputSchema", td.ToolName)
		}
	}
}

// ---------------------------------------------------------------------------
// InvokeTool tests
// ---------------------------------------------------------------------------

func TestInvokeTool_UnknownTool_ReturnsInvalidArgument(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	_, err := c.InvokeTool(context.Background(), testPrincipal, "nonexistent__tool", mcp.ToolArgs{})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrInvalidArgument {
		t.Errorf("want kind %q, got %q", mcp.ToolErrInvalidArgument, err.Kind)
	}
}

func TestInvokeTool_ProjectCreate_AuthorizeDeny(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, denySecurity{})
	_, err := c.InvokeTool(context.Background(), testPrincipal, "project__create", mcp.ToolArgs{"name": "my-repo"})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrPermissionDenied {
		t.Errorf("want kind %q, got %q", mcp.ToolErrPermissionDenied, err.Kind)
	}
}

func TestInvokeTool_ProjectCreate_MissingName_ReturnsInvalidArgument(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	_, err := c.InvokeTool(context.Background(), testPrincipal, "project__create", mcp.ToolArgs{})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrInvalidArgument {
		t.Errorf("want kind %q, got %q", mcp.ToolErrInvalidArgument, err.Kind)
	}
}

func TestInvokeTool_ProjectCreate_ManagerError_MapsToToolError(t *testing.T) {
	proj := &fakeProject{
		createErr: fwmgr.New(fwmgr.FailedPrecondition, "project already exists"),
	}
	c := newTestClient(proj, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	_, err := c.InvokeTool(context.Background(), testPrincipal, "project__create", mcp.ToolArgs{"name": "my-repo"})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrFailedPrecondition {
		t.Errorf("want kind %q, got %q", mcp.ToolErrFailedPrecondition, err.Kind)
	}
}

func TestInvokeTool_ProjectCreate_Success(t *testing.T) {
	proj := &fakeProject{createResult: project.ProjectID("my-repo")}
	c := newTestClient(proj, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	result, toolErr := c.InvokeTool(context.Background(), testPrincipal, "project__create", mcp.ToolArgs{"name": "my-repo"})
	if toolErr != nil {
		t.Fatalf("InvokeTool returned error: %v", toolErr)
	}
	var body map[string]string
	if err := json.Unmarshal(result.Content, &body); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if body["projectId"] != "my-repo" {
		t.Errorf("want projectId %q, got %q", "my-repo", body["projectId"])
	}
}

func TestInvokeTool_SystemDesignStart_Success(t *testing.T) {
	sd := &fakeSystemDesign{startResult: systemdesign.NewSessionRef("sess-123")}
	c := newTestClient(&fakeProject{}, sd, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	result, toolErr := c.InvokeTool(context.Background(), testPrincipal, "system_design__start", mcp.ToolArgs{"projectId": "my-repo"})
	if toolErr != nil {
		t.Fatalf("InvokeTool returned error: %v", toolErr)
	}
	var body map[string]string
	if err := json.Unmarshal(result.Content, &body); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if body["sessionRef"] != "sess-123" {
		t.Errorf("want sessionRef %q, got %q", "sess-123", body["sessionRef"])
	}
}

func TestInvokeTool_SystemDesignRequestDraft_InvalidKind(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	_, err := c.InvokeTool(context.Background(), testPrincipal, "system_design__request_draft", mcp.ToolArgs{
		"projectId":    "my-repo",
		"artifactKind": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrInvalidArgument {
		t.Errorf("want kind %q, got %q", mcp.ToolErrInvalidArgument, err.Kind)
	}
}

func TestInvokeTool_ConstructionPause_Success(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	result, toolErr := c.InvokeTool(context.Background(), testPrincipal, "construction__pause", mcp.ToolArgs{
		"projectId": "my-repo",
		"reason":    "operator pause",
	})
	if toolErr != nil {
		t.Fatalf("InvokeTool returned error: %v", toolErr)
	}
	if len(result.Content) == 0 {
		t.Error("InvokeTool returned empty content")
	}
}

func TestInvokeTool_ConstructionOverride_InvalidKind(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	_, err := c.InvokeTool(context.Background(), testPrincipal, "construction__override", mcp.ToolArgs{
		"projectId":  "my-repo",
		"activityId": "G001",
		"kind":       "invalid_kind",
	})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrInvalidArgument {
		t.Errorf("want kind %q, got %q", mcp.ToolErrInvalidArgument, err.Kind)
	}
}

func TestInvokeTool_InfrastructureError_MapsToUnavailable(t *testing.T) {
	proj := &fakeProject{
		listErr: fwmgr.New(fwmgr.Infrastructure, "temporal unavailable"),
	}
	c := newTestClient(proj, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	_, err := c.InvokeTool(context.Background(), testPrincipal, "project__list", mcp.ToolArgs{})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrUnavailable {
		t.Errorf("want kind %q, got %q", mcp.ToolErrUnavailable, err.Kind)
	}
	if !err.Retryable {
		t.Error("want Retryable=true for unavailable error")
	}
}

// ---------------------------------------------------------------------------
// ReadProjectState tests
// ---------------------------------------------------------------------------

func TestReadProjectState_DefaultView_CallsGetProject(t *testing.T) {
	proj := &fakeProject{
		getResult: project.ProjectState{ProjectID: "my-repo", Name: "my-repo"},
	}
	c := newTestClient(proj, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})

	view, toolErr := c.ReadProjectState(context.Background(), testPrincipal, mcp.ProjectID("my-repo"), nil)
	if toolErr != nil {
		t.Fatalf("ReadProjectState returned error: %v", toolErr)
	}
	if len(view.State) == 0 {
		t.Error("ReadProjectState returned empty State")
	}
}

func TestReadProjectState_AuthorizeDeny(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, denySecurity{})
	_, err := c.ReadProjectState(context.Background(), testPrincipal, mcp.ProjectID("my-repo"), nil)
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrPermissionDenied {
		t.Errorf("want kind %q, got %q", mcp.ToolErrPermissionDenied, err.Kind)
	}
}

func TestReadProjectState_SystemDesignView_RequiresArtifactKind(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	_, err := c.ReadProjectState(context.Background(), testPrincipal, mcp.ProjectID("my-repo"), &mcp.StateView{
		Kind: mcp.StateViewKindSystemDesign,
		// ArtifactKind intentionally empty
	})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrInvalidArgument {
		t.Errorf("want kind %q, got %q", mcp.ToolErrInvalidArgument, err.Kind)
	}
}

func TestReadProjectState_SystemDesignView_Success(t *testing.T) {
	sd := &fakeSystemDesign{stateResult: systemdesign.SessionStateView{Stage: systemdesign.StageCommitted}}
	c := newTestClient(&fakeProject{}, sd, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})

	view, toolErr := c.ReadProjectState(context.Background(), testPrincipal, mcp.ProjectID("my-repo"), &mcp.StateView{
		Kind:         mcp.StateViewKindSystemDesign,
		ArtifactKind: "mission",
	})
	if toolErr != nil {
		t.Fatalf("ReadProjectState returned error: %v", toolErr)
	}
	if len(view.State) == 0 {
		t.Error("ReadProjectState returned empty State")
	}
}

func TestReadProjectState_UnknownView_ReturnsInvalidArgument(t *testing.T) {
	c := newTestClient(&fakeProject{}, &fakeSystemDesign{}, &fakeProjectDesign{}, &fakeConstruction{}, &fakeOperations{}, &fakeBilling{}, permitSecurity{})
	_, err := c.ReadProjectState(context.Background(), testPrincipal, mcp.ProjectID("my-repo"), &mcp.StateView{
		Kind: "unknownView",
	})
	if err == nil {
		t.Fatal("expected a ToolError, got nil")
	}
	if err.Kind != mcp.ToolErrInvalidArgument {
		t.Errorf("want kind %q, got %q", mcp.ToolErrInvalidArgument, err.Kind)
	}
}
