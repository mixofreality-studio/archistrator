package projectdesign

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
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on
// the public ops (projectDesignManager.md §2/§3). They run BEFORE any Temporal
// client call, so they need no cluster and no client — a nil client is safe
// because the checks short-circuit first.

func asProjectDesignError(t *testing.T, err error) *fwmanager.Error {
	t.Helper()
	var pde *fwmanager.Error
	if !errors.As(err, &pde) {
		t.Fatalf("expected *projectDesignError, got %T: %v", err, err)
	}
	return pde
}

// ---- RequestArtifactDraft ---------------------------------------------------

func Test_RequestArtifactDraft_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, ProjectID(""), KindPlanningAssumptions, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_RequestArtifactDraft_Phase1Kind_FailedPrecondition(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	// A Phase-1 kind is a Client bug for the Phase-2 Manager.
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindMission, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for a Phase-1 kind, got %d", got)
	}
}

func Test_RequestArtifactDraft_SdpReviewKind_FailedPrecondition(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	// The SDP review is assembled, not co-authored.
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindSdpReview, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for KindSdpReview, got %d", got)
	}
}

// ---- RequestArtifactDraft spine-ordering gate (Phase-2 twin of STP-UC1-B1) --

// committedPhase2Project builds a head-state Project whose named slot for each given
// Phase-2 kind is Committed (Status only — the gate reads Status, not the model).
func committedPhase2Project(pid ProjectID, committed ...ArtifactKind) projectstate.Project {
	p := projectstate.Project{ID: projectstate.ProjectID(pid)}
	for _, k := range committed {
		switch k {
		case KindPlanningAssumptions:
			p.PlanningAssumptions.Status = projectstate.ReviewCommitted
		case KindActivityList:
			p.ActivityList.Status = projectstate.ReviewCommitted
		case KindNetwork:
			p.Network.Status = projectstate.ReviewCommitted
		case KindNormalSolution:
			p.NormalSolution.Status = projectstate.ReviewCommitted
		case KindDecompressedSolution:
			p.DecompressedSolution.Status = projectstate.ReviewCommitted
		case KindSubcriticalSolution:
			p.SubcriticalSolution.Status = projectstate.ReviewCommitted
		case KindCompressedSolution:
			p.CompressedSolution.Status = projectstate.ReviewCommitted
		case KindRiskModel:
			p.RiskModel.Status = projectstate.ReviewCommitted
		}
	}
	return p
}

// P0-2 (closed-COMPLETED, committed — Phase-2 twin). A CoAuthor run that committed its
// Phase-2 artifact and then completed still answers the replayed sessionState Query with a
// STALE mid-flight stage. GetSessionState must Describe the run, see COMPLETED, and rebuild the
// COMMITTED view from the durable slot on main — StageCommitted carrying the committed model —
// WITHOUT trusting (or calling) the replayed Query.
func Test_GetSessionState_CompletedCommitted_ReturnsCommittedView(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)
	// NO QueryWorkflow expectation: a COMPLETED run's replayed query is stale and must be
	// bypassed. If GetSessionState fell through to it, the mock would panic.

	proj := committedPhase2Project(id, KindPlanningAssumptions)
	proj.PlanningAssumptions.Model = &projectstate.PlanningAssumptions{Notes: "the-notes"}
	ps := &fakeProjectState{project: proj}

	m := &projectDesignManager{client: mc, projectState: ps}
	view, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions)
	if err != nil {
		t.Fatalf("GetSessionState on a completed+committed session must return the committed view, got err: %v", err)
	}
	if view.Stage != StageCommitted {
		t.Fatalf("completed+committed must surface StageCommitted (not the replayed StageDrafting), got %d", view.Stage)
	}
	if view.Draft.Model == nil || !strings.Contains(string(*view.Draft.Model), "the-notes") {
		t.Fatalf("committed view model must be the committed slot content, got %v", view.Draft.Model)
	}
	mc.AssertExpectations(t)
}

// P0-2 (closed-COMPLETED, uncommitted — Phase-2 twin). A run that completed WITHOUT landing a
// commit must NOT surface the stale replayed StageDrafting either — it renders an honest
// terminal derived from the slot (here a withdrawn slot → StageWithdrawn), never Drafting.
func Test_GetSessionState_CompletedUncommitted_ReturnsHonestTerminal(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)

	proj := projectstate.Project{ID: projectstate.ProjectID(id)}
	proj.PlanningAssumptions.Status = projectstate.ReviewWithdrawn
	ps := &fakeProjectState{project: proj}

	m := &projectDesignManager{client: mc, projectState: ps}
	view, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions)
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

// An ABNORMALLY-closed run (FAILED) still synthesizes the honest StageDraftFailed view (F15/F28
// parity with systemdesign), bypassing the lying replayed Query.
func Test_GetSessionState_DeadWorkflow_SynthesizesFailedView(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)

	m := &projectDesignManager{client: mc}
	view, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions)
	if err != nil {
		t.Fatalf("GetSessionState on a dead workflow must synthesize a view, got err: %v", err)
	}
	if view.Stage != StageDraftFailed {
		t.Fatalf("a dead workflow must surface StageDraftFailed, got %d", view.Stage)
	}
	if view.FailureReason == nil || *view.FailureReason == "" {
		t.Fatal("a synthesized dead-workflow view must carry a human FailureReason")
	}
	mc.AssertExpectations(t)
}

// A Phase-2 draft whose immediate predecessor slot is uncommitted is refused with
// FailedPrecondition naming that predecessor — the wire enforces the Method's ordered
// Phase-2 spine, not only the SPA. Short-circuits before any Temporal client call.
func Test_RequestArtifactDraft_Phase2PredecessorUncommitted_FailedPrecondition(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	// activityList requested while its predecessor planningAssumptions is uncommitted.
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid)}}
	m := NewProjectDesignManager(nil, ps, nil, nil, nil, nil, nil, nil)
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, pid, KindActivityList, nil)
	pde := asProjectDesignError(t, err)
	if pde.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for uncommitted predecessor, got %d", pde.Kind)
	}
	if !strings.Contains(err.Error(), "planningAssumptions") {
		t.Fatalf("error should name the uncommitted predecessor planningAssumptions, got %q", err.Error())
	}
}

// A brand-new project (no head-state row → NotFound) refuses a non-first Phase-2 draft.
func Test_RequestArtifactDraft_NoProjectRow_FailedPrecondition(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{notFound: true}
	m := NewProjectDesignManager(nil, ps, nil, nil, nil, nil, nil, nil)
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, pid, KindActivityList, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for missing project row, got %d", got)
	}
}

// The first Phase-2 kind (planningAssumptions) has NO predecessor — the gate passes
// without any head-state read (mirrors the SPA unlocking it without a sealed Phase 1),
// so a nil projectState is safe.
func Test_CheckPhase2Predecessor_FirstKind_NoRead(t *testing.T) {
	m := newProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	if err := m.checkPhase2Predecessor(context.Background(), ProjectID(uuid.NewString()), KindPlanningAssumptions); err != nil {
		t.Fatalf("planningAssumptions has no predecessor; gate must pass, got %v", err)
	}
}

// With the immediate predecessor Committed the gate passes (proceeds to dispatch).
func Test_CheckPhase2Predecessor_Committed_Proceeds(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: committedPhase2Project(pid, KindPlanningAssumptions)}
	m := newProjectDesignManager(nil, ps, nil, nil, nil, nil, nil, nil)
	if err := m.checkPhase2Predecessor(context.Background(), pid, KindActivityList); err != nil {
		t.Fatalf("committed predecessor; gate must pass, got %v", err)
	}
}

// phase2PredecessorKind returns the immediate predecessor for each Phase-2 kind, and
// no predecessor for the first (planningAssumptions).
func Test_Phase2PredecessorKind(t *testing.T) {
	if _, ok := phase2PredecessorKind(KindPlanningAssumptions); ok {
		t.Fatal("planningAssumptions (first) must have no predecessor")
	}
	cases := map[ArtifactKind]ArtifactKind{
		KindActivityList:         KindPlanningAssumptions,
		KindNetwork:              KindActivityList,
		KindNormalSolution:       KindNetwork,
		KindDecompressedSolution: KindNormalSolution,
		KindSubcriticalSolution:  KindDecompressedSolution,
		KindCompressedSolution:   KindSubcriticalSolution,
		KindRiskModel:            KindCompressedSolution,
	}
	for kind, want := range cases {
		got, ok := phase2PredecessorKind(kind)
		if !ok || got != want {
			t.Fatalf("predecessor(%s) = (%s,%v), want (%s,true)", artifactKindString(kind), artifactKindString(got), ok, artifactKindString(want))
		}
	}
}

// ---- RequestSDPCommit -------------------------------------------------------

func Test_RequestSDPCommit_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.RequestSDPCommit(fwmanager.Context{Context: context.Background()}, ProjectID(""))
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// ---- SubmitSDPDecision ------------------------------------------------------

func Test_SubmitSDPDecision_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, ProjectID(""), SDPCommit, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_SubmitSDPDecision_CommitRequiresOptionID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	pid := ProjectID(uuid.NewString())

	// nil optionId.
	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, pid, SDPCommit, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Commit without optionId, got %d", got)
	}

	// empty optionId.
	empty := OptionID("")
	err = m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, pid, SDPCommit, &empty, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Commit with empty optionId, got %d", got)
	}
}

func Test_SubmitSDPDecision_RejectAllRequiresFeedback(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	pid := ProjectID(uuid.NewString())

	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, pid, SDPRejectAll, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for RejectAll without feedback, got %d", got)
	}

	err = m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, pid, SDPRejectAll, nil, &ReviewFeedback{Notes: ""})
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for RejectAll with empty notes, got %d", got)
	}
}

func Test_SubmitSDPDecision_UnknownDecision(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), SDPDecisionUnknown, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

// ---- SubmitReviewDecision (per-artifact OQ-3 gate) --------------------------

func Test_SubmitReviewDecision_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(""), KindPlanningAssumptions, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_SubmitReviewDecision_RejectRequiresFeedback(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	pid := ProjectID(uuid.NewString())
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, pid, KindActivityList, ReviewReject, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Reject without feedback, got %d", got)
	}

	err = m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, pid, KindActivityList, ReviewReject, &ReviewFeedback{Notes: ""})
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for Reject with empty notes, got %d", got)
	}
}

func Test_SubmitReviewDecision_WrongPhaseKind(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	// A Phase-1 kind is a Client bug for the Phase-2 Manager.
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindMission, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for a Phase-1 kind, got %d", got)
	}
}

func Test_SubmitReviewDecision_SdpReviewKind_FailedPrecondition(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	// The SDP review is not gated via the per-artifact reviewDecision signal.
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindSdpReview, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for KindSdpReview, got %d", got)
	}
}

func Test_SubmitReviewDecision_UnknownDecision(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindNetwork, ReviewDecisionUnknown, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

// ---- AdvanceToConstruction --------------------------------------------------

func Test_AdvanceToConstruction_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.AdvanceToConstruction(fwmanager.Context{Context: context.Background()}, ProjectID(""), false)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// F55 (Phase-2 twin): a committed-but-stale in-scope Phase-2 slot blocks AdvanceToConstruction
// with a FailedPrecondition that NAMES the stale slot. Synchronous head-state read that
// short-circuits before any Temporal call.
func Test_AdvanceToConstruction_StaleSlot_FailedPreconditionNamingSlot(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	proj := committedPhase2Project(pid, KindPlanningAssumptions, KindActivityList, KindNetwork)
	proj.Network.StaleBasis = true
	ps := &fakeProjectState{project: proj}
	m := NewProjectDesignManager(nil, ps, nil, nil, nil, nil, nil, nil)

	_, err := m.AdvanceToConstruction(fwmanager.Context{Context: context.Background()}, pid, false)
	pde := asProjectDesignError(t, err)
	if pde.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for a stale committed slot, got %d", pde.Kind)
	}
	if !strings.Contains(err.Error(), "network") {
		t.Fatalf("error must name the stale slot network, got %q", err.Error())
	}
}

// F55 (Phase-2 twin): acknowledgeStale bypasses the stale gate — the seal proceeds to the
// Temporal start (mock errors → Infrastructure, not FailedPrecondition).
func Test_AdvanceToConstruction_StaleSlot_AcknowledgeBypassesGate(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	proj := committedPhase2Project(pid, KindNetwork)
	proj.Network.StaleBasis = true
	ps := &fakeProjectState{project: proj}

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("boom"))
	m := &projectDesignManager{client: mc, projectState: ps}

	_, err := m.AdvanceToConstruction(fwmanager.Context{Context: context.Background()}, pid, true)
	if got := asProjectDesignError(t, err).Kind; got == fwmanager.FailedPrecondition {
		t.Fatal("with ack the stale gate must be bypassed, not surface FailedPrecondition")
	}
}

// F55 (Phase-2 twin): no stale slot → the gate is a no-op and the op proceeds unchanged.
func Test_AdvanceToConstruction_NoStaleSlot_ProceedsUnchanged(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	proj := committedPhase2Project(pid, KindPlanningAssumptions, KindNetwork)
	ps := &fakeProjectState{project: proj}

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("boom"))
	m := &projectDesignManager{client: mc, projectState: ps}

	_, err := m.AdvanceToConstruction(fwmanager.Context{Context: context.Background()}, pid, false)
	if got := asProjectDesignError(t, err).Kind; got == fwmanager.FailedPrecondition {
		t.Fatal("with no stale slot the gate must pass, not surface FailedPrecondition")
	}
}

// ---- GetSessionState --------------------------------------------------------

func Test_GetSessionState_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID(""), KindPlanningAssumptions)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// ---- F47: RequestArtifactDraft DELIVERS the feedback via the redraft signal --------

// recordingStartClient captures the SignalWithStartWorkflow call so a test can assert
// RequestArtifactDraft delivers the redraft signal (with feedback) rather than dropping it
// via a bare ExecuteWorkflow. Embeds client.Client so any other method panics if reached.
type recordingStartClient struct {
	client.Client
	signalName string
	signalArg  interface{}
	execCalled bool
}

type fakeWorkflowRun struct {
	client.WorkflowRun
	id string
}

func (r fakeWorkflowRun) GetID() string { return r.id }

func (c *recordingStartClient) SignalWithStartWorkflow(_ context.Context, workflowID, signalName string, signalArg interface{}, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
	c.signalName = signalName
	c.signalArg = signalArg
	return fakeWorkflowRun{id: workflowID}, nil
}

func (c *recordingStartClient) ExecuteWorkflow(_ context.Context, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
	// The F47 regression: a bare ExecuteWorkflow against a RUNNING session (USE_EXISTING)
	// returns the existing handle WITHOUT delivering the new feedback. RequestArtifactDraft
	// must NOT use this path — record it so the test fails loudly if it regresses.
	c.execCalled = true
	return fakeWorkflowRun{id: "exec"}, nil
}

func Test_RequestArtifactDraft_DeliversFeedbackViaRedraftSignal(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	// planningAssumptions is the first Phase-2 kind (no predecessor gate); notFound ⇒ amendment
	// index 0 (a normal/retry draft, not an amendment). The point under test is DELIVERY.
	ps := &fakeProjectState{notFound: true}
	fc := &recordingStartClient{}
	m := newProjectDesignManager(fc, ps, nil, nil, nil, nil, nil, nil)

	const notes = "resources must be plain strings, not objects"
	if _, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, pid, KindPlanningAssumptions, &ReviewFeedback{Notes: notes}); err != nil {
		t.Fatalf("RequestArtifactDraft: %v", err)
	}

	// THE FIX: the request must DELIVER the feedback via the redraft signal (so a running
	// session at the failed gate receives it), NOT drop it via a bare ExecuteWorkflow.
	if fc.execCalled {
		t.Fatal("RequestArtifactDraft must NOT use bare ExecuteWorkflow (drops feedback on a running session)")
	}
	if fc.signalName != signalRedraft {
		t.Fatalf("RequestArtifactDraft must signal %q (redraft), got %q", signalRedraft, fc.signalName)
	}
	sig, ok := fc.signalArg.(redraftSignal)
	if !ok {
		t.Fatalf("the redraft signal payload must be redraftSignal, got %T", fc.signalArg)
	}
	if sig.Feedback == nil || sig.Feedback.Notes != notes {
		t.Fatalf("the redraft signal must carry the request feedback %q, got %+v", notes, sig.Feedback)
	}
}
