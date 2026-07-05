package systemdesign

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	enumspb "go.temporal.io/api/enums/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	temporalmocks "go.temporal.io/sdk/mocks"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on
// the four public ops (systemDesignManager.md §2/§3). They run BEFORE any Temporal
// client call, so they need no cluster and no client — a nil client is safe
// because the checks short-circuit first.

func asSystemDesignError(t *testing.T, err error) *fwmanager.Error {
	t.Helper()
	var sde *fwmanager.Error
	if !errors.As(err, &sde) {
		t.Fatalf("expected *SystemDesignError, got %T: %v", err, err)
	}
	return sde
}

// bgRC is the Manager-layer call Context the façade ops now lead with (fwm.Context
// embedding a background context.Context; the zero Principal is a safe test stopgap).
func bgRC() fwmanager.Context { return fwmanager.Context{Context: context.Background()} }

// ---- StartSystemDesign (op 2.0, 2026-05-29) façade preconditions ------------

func Test_StartSystemDesign_EmptyProjectID(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	_, err := m.StartSystemDesign(bgRC(), ProjectID(""))
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// ResearchInput absent (a project with no row) -> FailedPrecondition. The
// precondition check short-circuits before any Temporal client call, so a nil
// client is safe.
func Test_StartSystemDesign_ResearchAbsent_FailedPrecondition(t *testing.T) {
	ps := &renderFakeProjectState{readErr: fwra.New(fwra.NotFound, "no row yet")}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, "")
	_, err := m.StartSystemDesign(bgRC(), ProjectID(uuid.NewString()))
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for absent research (no project row), got %d", got)
	}
}

// A project that exists but has an empty ResearchInput -> FailedPrecondition.
func Test_StartSystemDesign_ResearchEmpty_FailedPrecondition(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &renderFakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid)}} // zero ResearchInput
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, "")
	_, err := m.StartSystemDesign(bgRC(), pid)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for empty research, got %d", got)
	}
}

func Test_RequestArtifactDraft_EmptyProjectID(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	_, err := m.RequestArtifactDraft(bgRC(), ProjectID(""), KindMission, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_RequestArtifactDraft_WrongPhaseKind(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	// A Phase-2 kind is a Client bug for the Phase-1 Manager.
	_, err := m.RequestArtifactDraft(bgRC(), ProjectID(uuid.NewString()), KindSdpReview, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition, got %d", got)
	}
}

// ---- RequestArtifactDraft spine-ordering gate (STP-UC1-B1) ------------------

// committedProject builds a head-state Project whose named slot for each given kind
// is Committed (Status only — the gate reads Status, not the model). Mirrors the
// wire/on-disk committed-slot shape the SPA's buildSpine reads.
func committedProject(pid ProjectID, committed ...ArtifactKind) projectstate.Project {
	p := projectstate.Project{ID: projectstate.ProjectID(pid)}
	for _, k := range committed {
		if slot, ok := slotPtrForTest(&p, k); ok {
			slot.Status = projectstate.ReviewCommitted
		}
	}
	return p
}

// slotPtrForTest routes a Phase-1 kind to its named Project slot (the test-side
// mirror of the codec's slot routing; the production slotFor reads by value).
func slotPtrForTest(p *projectstate.Project, k ArtifactKind) (*projectstate.ArtifactSlot, bool) {
	switch k {
	case KindMission:
		return &p.Mission, true
	case KindGlossary:
		return &p.Glossary, true
	case KindScrubbedRequirements:
		return &p.ScrubbedRequirements, true
	case KindVolatilities:
		return &p.Volatilities, true
	case KindCoreUseCases:
		return &p.CoreUseCases, true
	case KindSystem:
		return &p.SystemDesign, true
	case KindOperationalConcepts:
		return &p.OperationalConcepts, true
	case KindStandardCheck:
		return &p.StandardCheck, true
	default:
		return nil, false
	}
}

// A draft whose immediate predecessor slot is uncommitted is refused with
// FailedPrecondition naming that predecessor — the wire enforces the Method's ordered
// Phase-1 spine, not only the SPA (STP-UC1-B1). The check short-circuits before any
// Temporal client call, so a nil client is safe.
func Test_RequestArtifactDraft_PredecessorUncommitted_FailedPrecondition(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	// coreUseCases (kind 4) requested while its predecessor volatilities (kind 3) is
	// uncommitted — exactly STP-UC1-B1.
	ps := &renderFakeProjectState{project: committedProject(pid, KindMission, KindGlossary, KindScrubbedRequirements)}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, "")
	_, err := m.RequestArtifactDraft(bgRC(), pid, KindCoreUseCases, nil)
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for uncommitted predecessor, got %d", sde.Kind)
	}
	if !strings.Contains(err.Error(), "volatilities") {
		t.Fatalf("error should name the uncommitted predecessor volatilities, got %q", err.Error())
	}
}

// A brand-new project (no head-state row → NotFound) refuses a non-first draft: no
// slot is committed, so the predecessor is uncommitted.
func Test_RequestArtifactDraft_NoProjectRow_FailedPrecondition(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &renderFakeProjectState{readErr: fwra.New(fwra.NotFound, "no row yet")}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, "")
	_, err := m.RequestArtifactDraft(bgRC(), pid, KindGlossary, nil)
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for missing project row, got %d", sde.Kind)
	}
	if !strings.Contains(err.Error(), "mission") {
		t.Fatalf("error should name the uncommitted predecessor mission, got %q", err.Error())
	}
}

// The first kind (mission) has NO predecessor — the gate passes without any head-state
// read, so a nil projectState is safe (the gate never reads).
func Test_CheckPhase1Predecessor_FirstKind_NoRead(t *testing.T) {
	m := newSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	if err := m.checkPhase1Predecessor(context.Background(), ProjectID(uuid.NewString()), KindMission); err != nil {
		t.Fatalf("mission has no predecessor; gate must pass, got %v", err)
	}
}

// With the immediate predecessor Committed the gate passes (proceeds to dispatch).
func Test_CheckPhase1Predecessor_Committed_Proceeds(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	// coreUseCases proceeds once its predecessor volatilities is committed (the only
	// slot the immediate-predecessor gate consults).
	ps := &renderFakeProjectState{project: committedProject(pid, KindVolatilities)}
	m := newSystemDesignManager(nil, ps, nil, nil, nil, nil, "")
	if err := m.checkPhase1Predecessor(context.Background(), pid, KindCoreUseCases); err != nil {
		t.Fatalf("committed predecessor; gate must pass, got %v", err)
	}
}

// The send-back / regenerate (redraft) path is unaffected: redrafting an in-review
// kind whose predecessor is Committed still passes the gate.
func Test_CheckPhase1Predecessor_RedraftUnaffected(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &renderFakeProjectState{project: committedProject(pid, KindMission)}
	m := newSystemDesignManager(nil, ps, nil, nil, nil, nil, "")
	// glossary is being redrafted; its predecessor mission is committed → allowed.
	if err := m.checkPhase1Predecessor(context.Background(), pid, KindGlossary); err != nil {
		t.Fatalf("redraft with committed predecessor must pass, got %v", err)
	}
}

// phase1PredecessorKind returns the immediate predecessor for each Phase-1 kind, and
// no predecessor for the first (mission).
func Test_Phase1PredecessorKind(t *testing.T) {
	if _, ok := phase1PredecessorKind(KindMission); ok {
		t.Fatal("mission (first) must have no predecessor")
	}
	cases := map[ArtifactKind]ArtifactKind{
		KindGlossary:             KindMission,
		KindScrubbedRequirements: KindGlossary,
		KindVolatilities:         KindScrubbedRequirements,
		KindCoreUseCases:         KindVolatilities,
		KindSystem:               KindCoreUseCases,
		KindOperationalConcepts:  KindSystem,
		KindStandardCheck:        KindOperationalConcepts,
	}
	for kind, want := range cases {
		got, ok := phase1PredecessorKind(kind)
		if !ok || got != want {
			t.Fatalf("predecessor(%s) = (%s,%v), want (%s,true)", artifactKindString(kind), artifactKindString(got), ok, artifactKindString(want))
		}
	}
}

func Test_SubmitReviewDecision_RejectRequiresFeedback(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	pid := ProjectID(uuid.NewString())
	err := m.SubmitReviewDecision(bgRC(), pid, KindMission, ReviewReject, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Reject without feedback, got %d", got)
	}

	// Reject with empty notes is also misuse.
	err = m.SubmitReviewDecision(bgRC(), pid, KindMission, ReviewReject, &ReviewFeedback{Notes: ""})
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Reject with empty notes, got %d", got)
	}
}

func Test_SubmitReviewDecision_UnknownDecision(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	err := m.SubmitReviewDecision(bgRC(), ProjectID(uuid.NewString()), KindMission, ReviewDecisionUnknown, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

func Test_SubmitReviewDecision_WrongPhaseKind(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	err := m.SubmitReviewDecision(bgRC(), ProjectID(uuid.NewString()), KindActivityList, ReviewApprove, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition, got %d", got)
	}
}

func Test_AdvancePhase_EmptyProjectID(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	_, err := m.AdvancePhase(bgRC(), ProjectID(""))
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_GetSessionState_EmptyProjectID(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, "")
	_, err := m.GetSessionState(bgRC(), ProjectID(""), KindMission)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// QA F15 gap 2a (query-side defense). A CoAuthor workflow that died ABNORMALLY (FAILED)
// still answers the sessionState Query by history-replay with its last stage (StageDrafting),
// which LIES that drafting is in progress. GetSessionState must Describe the execution,
// detect the abnormal-closed status, and synthesize an explicit StageDraftFailed view —
// WITHOUT trusting (or even calling) the replayed Query.
func Test_GetSessionState_DeadWorkflow_SynthesizesFailedView(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)
	// Deliberately set NO QueryWorkflow expectation: if GetSessionState fell through to the
	// (lying) replayed query for an abnormal-closed workflow, the mock would panic — proving
	// the synthesis path bypasses it.

	m := &systemDesignManager{client: mc}
	view, err := m.GetSessionState(bgRC(), id, KindMission)
	if err != nil {
		t.Fatalf("GetSessionState on a dead workflow must synthesize a view, got err: %v", err)
	}
	if view.Stage != StageDraftFailed {
		t.Fatalf("a dead workflow must surface StageDraftFailed (not the replayed StageDrafting), got %d", view.Stage)
	}
	if view.FailureReason == nil || *view.FailureReason == "" {
		t.Fatal("a synthesized dead-workflow view must carry a human FailureReason")
	}
	mc.AssertExpectations(t)
}

// A RUNNING execution falls through to the live Query (Describe reports RUNNING) — the
// synthesis path must NOT hijack a healthy session. Here the Query is left unmocked and
// the mock is asserted to have been asked to Describe; a RUNNING status must not short-
// circuit to a synthesized failure.
func Test_GetSessionState_RunningWorkflow_DoesNotSynthesize(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)
	// QueryWorkflow returns an error so we don't have to synthesize a real EncodedValue;
	// the point is only that the RUNNING path REACHES the query rather than synthesizing.
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
		Return(nil, errors.New("boom"))

	m := &systemDesignManager{client: mc}
	if _, err := m.GetSessionState(bgRC(), id, KindMission); err == nil {
		t.Fatal("a RUNNING session must fall through to the live Query (which here errors), not synthesize success")
	}
	mc.AssertExpectations(t)
}

// P0-2 (closed-COMPLETED, committed). A CoAuthor run that committed its artifact and then
// completed still answers the replayed sessionState Query with a STALE mid-flight stage
// (StageDrafting → "GENERATING · MISSION" forever). GetSessionState must Describe the run,
// see COMPLETED, and rebuild the COMMITTED view from the durable slot on main — StageCommitted
// carrying the committed model — WITHOUT trusting (or calling) the replayed Query.
func Test_GetSessionState_CompletedCommitted_ReturnsCommittedView(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)
	// NO QueryWorkflow expectation: a COMPLETED run's replayed query is stale and must be
	// bypassed. If GetSessionState fell through to it, the mock would panic.

	proj := committedProject(id, KindMission)
	proj.Mission.Model = &projectstate.MissionStatement{Vision: "V", Mission: "M"}
	ps := &renderFakeProjectState{project: proj}

	m := &systemDesignManager{client: mc, projectState: ps}
	view, err := m.GetSessionState(bgRC(), id, KindMission)
	if err != nil {
		t.Fatalf("GetSessionState on a completed+committed session must return the committed view, got err: %v", err)
	}
	if view.Stage != StageCommitted {
		t.Fatalf("completed+committed must surface StageCommitted (not the replayed StageDrafting), got %d", view.Stage)
	}
	if view.Draft.Model == nil {
		t.Fatal("committed view must carry the committed model")
	}
	if !strings.Contains(string(*view.Draft.Model), "\"vision\":\"V\"") {
		t.Fatalf("committed view model must be the committed slot content, got %s", string(*view.Draft.Model))
	}
	mc.AssertExpectations(t)
}

// P0-2 (closed-COMPLETED, uncommitted). A run that completed WITHOUT landing a commit (e.g.
// withdrawn) must NOT surface the stale replayed StageDrafting either — it renders an honest
// terminal derived from the slot (here a withdrawn slot → StageWithdrawn), never Drafting.
func Test_GetSessionState_CompletedUncommitted_ReturnsHonestTerminal(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)

	proj := projectstate.Project{ID: projectstate.ProjectID(id)}
	proj.Mission.Status = projectstate.ReviewWithdrawn
	ps := &renderFakeProjectState{project: proj}

	m := &systemDesignManager{client: mc, projectState: ps}
	view, err := m.GetSessionState(bgRC(), id, KindMission)
	if err != nil {
		t.Fatalf("GetSessionState on a completed+uncommitted session must synthesize a terminal view, got err: %v", err)
	}
	if view.Stage == StageDrafting {
		t.Fatal("a completed+uncommitted session must NOT surface the stale StageDrafting")
	}
	if view.Stage != StageWithdrawn {
		t.Fatalf("a withdrawn completed slot must surface StageWithdrawn, got %d", view.Stage)
	}
	mc.AssertExpectations(t)
}

// SessionRef is opaque: it round-trips and compares by value, never parsed.
func Test_SessionRef_OpaqueValueSemantics(t *testing.T) {
	a := newSessionRef("proj-1:1")
	b := newSessionRef("proj-1:1")
	c := newSessionRef("proj-1:2")
	if a != b {
		t.Fatal("equal refs should compare equal")
	}
	if a == c {
		t.Fatal("different refs should not compare equal")
	}
	if string(a) != "proj-1:1" {
		t.Fatalf("unexpected ref string: %q", string(a))
	}
}

// ---- minimal test doubles for the façade-precondition tests ----------------

// renderFakeProjectState serves a scripted ReadProject result. Other verbs panic
// — these façade-precondition tests only ever exercise the read path.
type renderFakeProjectState struct {
	project projectstate.Project
	readErr error
}

func (f *renderFakeProjectState) ReadProject(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	if f.readErr != nil {
		return projectstate.Project{}, f.readErr
	}
	return f.project, nil
}

func (f *renderFakeProjectState) ReadProjectVersion(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Version, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.project.Version, nil
}

func (f *renderFakeProjectState) StageArtifactForReview(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactModel) (projectstate.Version, error) {
	panic("renderFakeProjectState.StageArtifactForReview must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) CommitArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind) (projectstate.Version, error) {
	panic("renderFakeProjectState.CommitArtifact must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) RejectArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	panic("renderFakeProjectState.RejectArtifact must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) WithdrawArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	panic("renderFakeProjectState.WithdrawArtifact must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) AdvancePhase(fwra.Context, projectstate.ProjectID, projectstate.Version) (projectstate.Version, error) {
	panic("renderFakeProjectState.AdvancePhase must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) SetResearchInput(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ResearchInput) (projectstate.Version, error) {
	panic("renderFakeProjectState.SetResearchInput must not be called by these façade-precondition tests")
}
func (f *renderFakeProjectState) SetOperatingModel(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.OperatingModel) (projectstate.Version, error) {
	panic("renderFakeProjectState.SetOperatingModel must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) CreateProject(fwra.Context, projectstate.ProjectID, projectstate.OwnerScope, string) (projectstate.Version, error) {
	panic("renderFakeProjectState.CreateProject must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) ListProjects(fwra.Context, projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	panic("renderFakeProjectState.ListProjects must not be called by these façade-precondition tests")
}

// compile-time conformance.
var _ projectstate.ProjectStateAccess = (*renderFakeProjectState)(nil)

// IsPhase1 covers the Phase-1 subset gate the contract uses.
func Test_ArtifactKind_IsPhase1(t *testing.T) {
	phase1 := []ArtifactKind{
		KindMission, KindGlossary, KindScrubbedRequirements,
		KindVolatilities, KindCoreUseCases, KindSystem,
		KindOperationalConcepts, KindStandardCheck,
	}
	for _, k := range phase1 {
		if !artifactKindIsPhase1(k) {
			t.Fatalf("kind %s should be Phase 1", artifactKindString(k))
		}
	}
	notPhase1 := []ArtifactKind{
		KindSdpReview, KindActivityList,
		KindNetwork, KindRiskModel,
	}
	for _, k := range notPhase1 {
		if artifactKindIsPhase1(k) {
			t.Fatalf("kind %s should NOT be Phase 1", artifactKindString(k))
		}
	}
}
