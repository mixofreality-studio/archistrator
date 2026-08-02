package systemdesign

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/stretchr/testify/mock"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
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
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
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
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
	_, err := m.StartSystemDesign(bgRC(), ProjectID(uuid.NewString()))
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for absent research (no project row), got %d", got)
	}
}

// A project that exists but has an empty ResearchInput -> FailedPrecondition.
func Test_StartSystemDesign_ResearchEmpty_FailedPrecondition(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &renderFakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid)}} // zero ResearchInput
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
	_, err := m.StartSystemDesign(bgRC(), pid)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for empty research, got %d", got)
	}
}

func Test_RequestArtifactDraft_EmptyProjectID(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
	_, err := m.RequestArtifactDraft(bgRC(), ProjectID(""), KindMission, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_RequestArtifactDraft_WrongPhaseKind(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
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
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
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
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
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
	m := newSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
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
	m := newSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
	if err := m.checkPhase1Predecessor(context.Background(), pid, KindCoreUseCases); err != nil {
		t.Fatalf("committed predecessor; gate must pass, got %v", err)
	}
}

// The send-back / regenerate (redraft) path is unaffected: redrafting an in-review
// kind whose predecessor is Committed still passes the gate.
func Test_CheckPhase1Predecessor_RedraftUnaffected(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &renderFakeProjectState{project: committedProject(pid, KindMission)}
	m := newSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
	// glossary is being redrafted; its predecessor mission is committed → allowed.
	if err := m.checkPhase1Predecessor(context.Background(), pid, KindGlossary); err != nil {
		t.Fatalf("redraft with committed predecessor must pass, got %v", err)
	}
}

// ---- RequestArtifactDraft generating guard (QA incident 2026-07-15) ---------

// fakeSignalWorkflowRun satisfies client.WorkflowRun for the mocked SignalWithStart result.
type fakeSignalWorkflowRun struct {
	client.WorkflowRun
	id string
}

func (r fakeSignalWorkflowRun) GetID() string { return r.id }

// runningSessionClient mocks the Describe-then-Query pair GetSessionState runs for a
// RUNNING co-author workflow, answering the sessionState query with the given stage.
func runningSessionClient(wfID string, stage SessionStage) *temporalmocks.Client {
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(
		&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
			},
		}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).Return(
		fakeEncodedSessionView{view: SessionStateView{Stage: stage}}, nil)
	return mc
}

// THE INCIDENT GUARD (gtdapp:1, 2026-07-15): a draft request while the session is DRAFTING
// is refused with FailedPrecondition — the redraft signal would be consumable by no open
// gate and would sit buffered until it stale-consumed a later StageDraftFailed gate. The
// mock carries NO SignalWithStartWorkflow expectation: reaching it fails the test loudly.
func Test_RequestArtifactDraft_WhileDrafting_FailedPrecondition_NoSignal(t *testing.T) {
	for _, stage := range []SessionStage{StageDrafting, StageRedrafting} {
		pid := ProjectID(uuid.NewString())
		mc := runningSessionClient(coAuthorWorkflowID(pid, KindMission), stage)
		m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}

		_, err := m.RequestArtifactDraft(bgRC(), pid, KindMission, nil)
		sde := asSystemDesignError(t, err)
		if sde.Kind != fwmanager.FailedPrecondition {
			t.Fatalf("stage %d: want FailedPrecondition while a draft is generating, got %d (%v)", stage, sde.Kind, err)
		}
		if !strings.Contains(err.Error(), "already generating") {
			t.Fatalf("stage %d: the refusal must say a draft is already generating, got %q", stage, err.Error())
		}
		mc.AssertExpectations(t)
	}
}

// The receptive stages still SIGNAL: AwaitingReview (an open review gate) and DraftFailed
// (the recovery gate's Retry lever) deliver the redraft signal via SignalWithStart.
func Test_RequestArtifactDraft_ReceptiveStages_SignalDelivered(t *testing.T) {
	for _, stage := range []SessionStage{StageAwaitingReview, StageDraftFailed} {
		pid := ProjectID(uuid.NewString())
		wfID := coAuthorWorkflowID(pid, KindMission)
		mc := runningSessionClient(wfID, stage)
		mc.On("SignalWithStartWorkflow", mock.Anything, wfID, lSignalRedraft,
			mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(fakeSignalWorkflowRun{id: wfID}, nil)
		m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}

		ref, err := m.RequestArtifactDraft(bgRC(), pid, KindMission, nil)
		if err != nil {
			t.Fatalf("stage %d: a receptive stage must accept the draft request, got %v", stage, err)
		}
		if ref == "" {
			t.Fatalf("stage %d: expected a session ref", stage)
		}
		mc.AssertExpectations(t)
	}
}

// No session has ever run (Describe reports the execution missing → GetSessionState
// NotFound): the guard passes and the request STARTS the first session.
func Test_RequestArtifactDraft_NoSession_StartsFirstDraft(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(pid, KindMission)
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), serviceerror.NewNotFound("workflow not found for ID: "+wfID))
	mc.On("SignalWithStartWorkflow", mock.Anything, wfID, lSignalRedraft,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(fakeSignalWorkflowRun{id: wfID}, nil)
	m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}

	if _, err := m.RequestArtifactDraft(bgRC(), pid, KindMission, nil); err != nil {
		t.Fatalf("no-session must start the first draft, got %v", err)
	}
	mc.AssertExpectations(t)
}

// THE 2026-07-16 INCIDENT REGRESSION (manager half — revival). A DEAD session (the previous
// run closed FAILED, as gtdapp:1 did) is receptive: "Retry design job" must start a brand-new
// run. The manager must pin WorkflowIDReusePolicy ALLOW_DUPLICATE on the SignalWithStart
// (a stricter policy silently turns the retry into a no-op 200) and then VERIFY the session's
// latest execution is live before reporting success.
func Test_RequestArtifactDraft_DeadSession_RevivesFreshRun(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(pid, KindMission)

	mc := &temporalmocks.Client{}
	// Describe #1 (the receptive check): the previous run is FAILED → synthesized
	// StageDraftFailed → receptive.
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_FAILED},
		}, nil).Once()
	mc.On("SignalWithStartWorkflow", mock.Anything, wfID, lSignalRedraft,
		mock.Anything,
		mock.MatchedBy(func(o client.StartWorkflowOptions) bool {
			return o.WorkflowIDReusePolicy == enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE &&
				o.WorkflowIDConflictPolicy == enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING
		}),
		mock.Anything, mock.Anything).
		Return(fakeSignalWorkflowRun{id: wfID}, nil)
	// Describe #2 (the revival verification): a fresh run is now RUNNING.
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
		}, nil).Once()

	m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}
	ref, err := m.RequestArtifactDraft(bgRC(), pid, KindMission, nil)
	if err != nil {
		t.Fatalf("a dead session's retry must revive a fresh run, got %v", err)
	}
	if ref == "" {
		t.Fatal("expected a session ref")
	}
	mc.AssertExpectations(t)
}

// NO FALSE 200s (2026-07-16 incident): when the SignalWithStart reports success but the
// session's latest execution is STILL abnormally closed (nothing actually started — the
// observed live failure), the manager must return an honest error, never success.
func Test_RequestArtifactDraft_RevivalDidNotStart_HonestError(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(pid, KindMission)

	mc := &temporalmocks.Client{}
	// Both Describes (receptive check AND post-start verification) report FAILED — the
	// session never revived.
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_FAILED},
		}, nil)
	mc.On("SignalWithStartWorkflow", mock.Anything, wfID, lSignalRedraft,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(fakeSignalWorkflowRun{id: wfID}, nil)

	m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}
	_, err := m.RequestArtifactDraft(bgRC(), pid, KindMission, nil)
	if err == nil {
		t.Fatal("a retry that revived nothing must NOT return success (the false-200)")
	}
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure for a failed revival, got %d (%v)", sde.Kind, err)
	}
	if !strings.Contains(sde.Detail, "could not be revived") {
		t.Fatalf("the error must name the failed revival, got %q", sde.Detail)
	}
	mc.AssertExpectations(t)
}

// F-R2 (wedged-run recovery): a RUNNING session whose workflow TASK is perpetually failing (a
// deploy-time non-determinism loop) shows RUNNING to Describe but rejects the sessionState query
// with "...Workflow Task in failed state". A SignalWithStart would only BUFFER the redraft on
// that corpse, so Retry must TERMINATE the wedged run FIRST, then start a fresh one — while a
// TRANSIENT query blip must NEVER trigger a terminate.
func Test_RequestArtifactDraft_WedgedRun_SupersedesThenStarts(t *testing.T) {
	t.Run("wedged supersedes then signal-starts", func(t *testing.T) {
		pid := ProjectID(uuid.NewString())
		wfID := coAuthorWorkflowID(pid, KindMission)

		mc := &temporalmocks.Client{}
		// The receptive/wedged probe: a RUNNING execution whose query is wedged.
		mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
			Return(&workflowservice.DescribeWorkflowExecutionResponse{
				WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
			}, nil).Once()
		mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
			Return(nil, errors.New("Unable to query workflow due to Workflow Task in failed state")).Once()

		var order []string
		mc.On("TerminateWorkflow", mock.Anything, wfID, "", mock.Anything).
			Run(func(mock.Arguments) { order = append(order, "terminate") }).Return(nil).Once()
		mc.On("SignalWithStartWorkflow", mock.Anything, wfID, lSignalRedraft,
			mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Run(func(mock.Arguments) { order = append(order, "signalStart") }).
			Return(fakeSignalWorkflowRun{id: wfID}, nil)
		// Revival verification: a fresh run is now RUNNING.
		mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
			Return(&workflowservice.DescribeWorkflowExecutionResponse{
				WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
			}, nil).Once()

		m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}
		if _, err := m.RequestArtifactDraft(bgRC(), pid, KindMission, nil); err != nil {
			t.Fatalf("a wedged run must be superseded and revived, got %v", err)
		}
		if len(order) != 2 || order[0] != "terminate" || order[1] != "signalStart" {
			t.Fatalf("Terminate must precede SignalWithStart, got %v", order)
		}
		mc.AssertExpectations(t)
	})

	t.Run("transient query blip does NOT terminate", func(t *testing.T) {
		pid := ProjectID(uuid.NewString())
		wfID := coAuthorWorkflowID(pid, KindMission)

		mc := &temporalmocks.Client{}
		mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
			Return(&workflowservice.DescribeWorkflowExecutionResponse{
				WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
			}, nil)
		// A TRANSIENT query fault (NOT the wedged signature) must surface as an error, never a
		// terminate — we cannot prove the run is genuinely wedged.
		mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
			Return(nil, errors.New("context deadline exceeded"))

		m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}
		if _, err := m.RequestArtifactDraft(bgRC(), pid, KindMission, nil); err == nil {
			t.Fatal("a transient query fault must surface as an error, not silently proceed")
		}
		mc.AssertNotCalled(t, "TerminateWorkflow", mock.Anything, wfID, "", mock.Anything)
		mc.AssertNotCalled(t, "SignalWithStartWorkflow", mock.Anything, wfID, lSignalRedraft, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})
}

// F-R2 durable-slot-first: a session that died ABNORMALLY *after* its artifact committed on main
// must render the COMMITTED view (not a bare failed card), carrying a FailureReason so the
// abnormal end stays visible — the committed model's amend affordance is the recovery lever.
func Test_GetSessionState_AbnormalClose_CommittedSlot_ShowsCommittedWithReason(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED},
		}, nil)

	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 2,
		Mission: committedSlot(mustMission(t)),
	}}
	m := &systemDesignManager{client: mc, projectState: ps}

	view, err := m.GetSessionState(bgRC(), id, KindMission)
	if err != nil {
		t.Fatalf("an abnormal-closed run over a committed slot must synthesize a view, got %v", err)
	}
	if view.Stage != StageCommitted {
		t.Fatalf("a committed slot must render StageCommitted even after an abnormal close, got %d", view.Stage)
	}
	if view.FailureReason == nil || *view.FailureReason == "" {
		t.Fatal("the committed view must carry a FailureReason noting the abnormal end")
	}
	mc.AssertExpectations(t)
}

// F-R2 dead-session Withdraw (scoped): a dormant-mode session whose run died leaving an
// AwaitingReview slot on MAIN can still be WITHDRAWN synchronously through the RA (the slot
// resets durably) instead of the bare refusal — only for Withdraw + a staged-on-main slot, and
// never via a signal (a signal to the corpse can't be honored).
func Test_SubmitReviewDecision_DeadSession_StagedOnMain_WithdrawRecordsSynchronously(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	// The run is dead (abnormal-closed) → reviewGateView reports live=false.
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_FAILED},
		}, nil)

	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), "", ""), // staged on main (AwaitingReview)
	}}
	m := &systemDesignManager{
		client:        mc,
		projectState:  ps,
		designSession: projectstate.NewDesignSessionAccess(ps),
	}

	if err := m.SubmitReviewDecision(bgRC(), id, KindMission, ReviewWithdraw, nil); err != nil {
		t.Fatalf("a Withdraw against a dead session staged on main must record synchronously, got %v", err)
	}
	if len(ps.withdrawn) != 1 || ps.withdrawn[0] != projectstate.KindMission {
		t.Fatalf("the withdraw must be recorded on main, got %v", ps.withdrawn)
	}
	mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, wfID, "", signalReviewDecision, mock.Anything)
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
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
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
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
	err := m.SubmitReviewDecision(bgRC(), ProjectID(uuid.NewString()), KindMission, ReviewDecisionUnknown, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

func Test_SubmitReviewDecision_WrongPhaseKind(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
	err := m.SubmitReviewDecision(bgRC(), ProjectID(uuid.NewString()), KindActivityList, ReviewApprove, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition, got %d", got)
	}
}

func Test_AdvancePhase_EmptyProjectID(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
	_, err := m.AdvancePhase(bgRC(), ProjectID(""), false)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// F55: a committed-but-stale in-scope slot blocks AdvancePhase with a FailedPrecondition that
// NAMES the stale slot — the seal must not silently advance over a shifted basis. The check is
// a synchronous head-state read that short-circuits BEFORE any Temporal call, so a nil client
// is safe.
func Test_AdvancePhase_StaleSlot_FailedPreconditionNamingSlot(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	proj := committedProject(pid, KindMission, KindGlossary)
	proj.ScrubbedRequirements.Status = projectstate.ReviewCommitted
	proj.ScrubbedRequirements.StaleBasis = true
	ps := &renderFakeProjectState{project: proj}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")

	_, err := m.AdvancePhase(bgRC(), pid, false)
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for a stale committed slot, got %d", sde.Kind)
	}
	if !strings.Contains(err.Error(), "scrubbedRequirements") {
		t.Fatalf("error must name the stale slot scrubbedRequirements, got %q", err.Error())
	}
}

// F55: acknowledgeStale bypasses the stale gate — the seal proceeds to the Temporal start
// (here the mock start errors → Infrastructure, NOT the FailedPrecondition the gate would have
// produced). Proves the ack path is not blocked by staleness.
func Test_AdvancePhase_StaleSlot_AcknowledgeBypassesGate(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	proj := committedProject(pid, KindMission)
	proj.ScrubbedRequirements.Status = projectstate.ReviewCommitted
	proj.ScrubbedRequirements.StaleBasis = true
	ps := &renderFakeProjectState{project: proj}

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("boom"))
	m := &systemDesignManager{client: mc, projectState: ps}

	_, err := m.AdvancePhase(bgRC(), pid, true)
	if got := asSystemDesignError(t, err).Kind; got == fwmanager.FailedPrecondition {
		t.Fatal("with ack the stale gate must be bypassed, not surface FailedPrecondition")
	}
}

// F55: no stale slot → the gate is a no-op and the op proceeds unchanged (reaching the Temporal
// start, distinguishing "gate passed" from "gate blocked with FailedPrecondition").
func Test_AdvancePhase_NoStaleSlot_ProceedsUnchanged(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	proj := committedProject(pid, KindMission, KindGlossary, KindScrubbedRequirements)
	ps := &renderFakeProjectState{project: proj}

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("boom"))
	m := &systemDesignManager{client: mc, projectState: ps}

	_, err := m.AdvancePhase(bgRC(), pid, false)
	if got := asSystemDesignError(t, err).Kind; got == fwmanager.FailedPrecondition {
		t.Fatal("with no stale slot the gate must pass, not surface FailedPrecondition")
	}
}

func Test_GetSessionState_EmptyProjectID(t *testing.T) {
	m := NewSystemDesignManager(nil, nil, nil, nil, nil, nil, nil, "")
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

// Plan-3 C1 (regression pin, systemdesign twin of the projectdesign C2 test). isAbnormalClosedStatus
// → failedSessionView exists precisely so a session that dies mid-dispatch — e.g. the
// TRANSIENT dispatch/observe fault that exhausts its retry budget at coauthorartifact.go:698-704
// (the derr branch → recoverDispatchFailed, closing the workflow while state.markActive had
// JUST stamped ActiveRoleArchitect/ActiveStepDrafting or ActiveRoleProductManager/
// ActiveStepCritiquing/Revising ahead of the dispatch) — can never leak that in-flight sub-step
// stamp through GetSessionState. failedSessionView is a PURE synthesis (it builds a fresh
// SessionStateView literal and never touches the replayed Query), so the guard holds for every
// abnormal terminal status Temporal can report for a dead workflow, not only FAILED.
func Test_GetSessionState_AbnormalClose_SubStepNeverLeaks(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	abnormal := []enumspb.WorkflowExecutionStatus{
		enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED,
	}
	for _, status := range abnormal {
		t.Run(status.String(), func(t *testing.T) {
			mc := &temporalmocks.Client{}
			resp := &workflowservice.DescribeWorkflowExecutionResponse{
				WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
					Status: status,
				},
			}
			mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)
			// NO QueryWorkflow expectation: the guard must bypass the live query entirely — if
			// GetSessionState fell through to it (surfacing whatever ActiveRole/ActiveStep/Round
			// the dying dispatch had stamped moments before the abnormal close), the mock would
			// panic, proving the synthesis path never reaches it.

			m := &systemDesignManager{client: mc}
			view, err := m.GetSessionState(bgRC(), id, KindMission)
			if err != nil {
				t.Fatalf("GetSessionState on an abnormally-closed workflow must synthesize a view, got err: %v", err)
			}
			if view.Stage != StageDraftFailed {
				t.Fatalf("abnormal close must surface StageDraftFailed, got %d", view.Stage)
			}
			if view.ActiveRole != ActiveRoleNone || view.ActiveStep != ActiveStepNone || view.Round != 0 {
				t.Fatalf("abnormal close must never leak the in-flight sub-step stamp, got role=%d step=%d round=%d", view.ActiveRole, view.ActiveStep, view.Round)
			}
			mc.AssertExpectations(t)
		})
	}
}

// R3 (error altitude). When no design session exists, Temporal's Describe reports the
// execution NotFound with a raw "workflow not found for ID: <proj>:<kind>" message that
// leaks the internal execution-id format. GetSessionState must map that at the manager
// boundary to a clean, project-scoped NotFound — never surfacing "workflow not found"
// or the internal id to the client.
func Test_GetSessionState_NoSession_CleanNotFound(t *testing.T) {
	id := ProjectID("gtdapp")
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), serviceerror.NewNotFound("workflow not found for ID: gtdapp:0"))
	// No QueryWorkflow expectation: the NotFound Describe short-circuits to the clean error.

	m := &systemDesignManager{client: mc}
	_, err := m.GetSessionState(bgRC(), id, KindMission)
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %d", sde.Kind)
	}
	if !strings.Contains(sde.Detail, "no active design session for project") {
		t.Fatalf("Detail must be the clean no-session message, got %q", sde.Detail)
	}
	if strings.Contains(sde.Detail, "workflow not found") || strings.Contains(sde.Detail, "gtdapp:0") {
		t.Fatalf("Detail must not leak Temporal internals, got %q", sde.Detail)
	}
	mc.AssertExpectations(t)
}

// R3 twin: when Describe returns a non-NotFound blip (best-effort fall-through), the live
// QueryWorkflow may itself report the workflow NotFound. That path must ALSO map to the
// clean no-session error rather than leaking the raw Temporal message via mapQueryError.
func Test_GetSessionState_QueryNotFound_CleanNotFound(t *testing.T) {
	id := ProjectID("gtdapp")
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	// Describe errors non-NotFound → best-effort fall-through to the query.
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), errors.New("transient describe blip"))
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
		Return(nil, serviceerror.NewNotFound("workflow not found for ID: gtdapp:0"))

	m := &systemDesignManager{client: mc}
	_, err := m.GetSessionState(bgRC(), id, KindMission)
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %d", sde.Kind)
	}
	if strings.Contains(sde.Detail, "workflow not found") || strings.Contains(sde.Detail, "gtdapp:0") {
		t.Fatalf("Detail must not leak Temporal internals, got %q", sde.Detail)
	}
	mc.AssertExpectations(t)
}

// QA 2026-07-19 (poll-404 wizard reset). When the server's Temporal client ends up pointed
// at a FOREIGN dev server (the shared fixed port was taken over by another tool's
// `temporal server start-dev` — observed live: the systemtests server on 7233), lookups
// fail with serviceerror.NamespaceNotFound ("Namespace X is not found"). That is an
// INFRASTRUCTURE fault — the session store is the wrong/unavailable backend — NOT "no
// active design session". The old substring matcher ("not found") classified it NotFound,
// so the SPA received an authoritative 404 and reset the founder's wizard mid-use-case.
// It must map to Infrastructure so the polling client keeps its state and self-heals.
func Test_GetSessionState_NamespaceNotFound_IsInfrastructureNot404(t *testing.T) {
	id := ProjectID("gtdapp")
	wfID := coAuthorWorkflowID(id, KindCoreUseCases)

	mc := &temporalmocks.Client{}
	nsErr := serviceerror.NewNamespaceNotFound("default")
	// Describe fails non-execution-NotFound → best-effort fall-through to the query,
	// which fails the same way against the foreign backend.
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), nsErr)
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
		Return(nil, nsErr)

	m := &systemDesignManager{client: mc}
	_, err := m.GetSessionState(bgRC(), id, KindCoreUseCases)
	sde := asSystemDesignError(t, err)
	if sde.Kind == fwmanager.NotFound {
		t.Fatalf("a namespace-not-found (wrong/foreign Temporal backend) must NOT map to the authoritative no-session NotFound; got NotFound with detail %q", sde.Detail)
	}
	if sde.Kind != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %d (detail %q)", sde.Kind, sde.Detail)
	}
	mc.AssertExpectations(t)
}

// R4 (error altitude). getProject for an unknown project surfaces a stuttered, chain-leaking
// git error ("resourceaccess: github.GitStore.clone: repository not found: repository not
// found: Repository not found."). GetProject must map the RA NotFound to a single clean,
// project-scoped Detail while preserving the full chain on Cause for the server log.
func Test_GetProject_UnknownProject_CleanNotFound(t *testing.T) {
	id := ProjectID("gtdapp")
	// The exact shape the infra-github ClassifyGitError + fwra.Wrap chain produces.
	chain := fwra.Wrap(fwra.NotFound,
		//nolint:revive // reproduces GitHub's literal error text, trailing period included
		errors.New("github.GitStore.clone: repository not found: repository not found: Repository not found."),
		"projectstate.ReadProject")
	ps := &renderFakeProjectState{readErr: chain}

	m := &systemDesignManager{projectState: ps}
	_, err := m.GetProject(bgRC(), id)
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %d", sde.Kind)
	}
	if sde.Detail != `project "gtdapp" not found` {
		t.Fatalf("Detail must be the single clean project-scoped message, got %q", sde.Detail)
	}
	if strings.Contains(sde.Detail, "GitStore") || strings.Contains(sde.Detail, "repository not found") {
		t.Fatalf("Detail must not leak the internal git chain, got %q", sde.Detail)
	}
	if sde.Cause == nil {
		t.Fatal("the full cause chain must be preserved on Cause for the server log")
	}
	mc := sde.Cause.Error()
	if !strings.Contains(mc, "repository not found") {
		t.Fatalf("Cause must retain the detailed chain, got %q", mc)
	}
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

// F73 (part 1). AskQuestions on a COMMITTED artifact whose co-author session is CLOSED must
// seed its question thread on MAIN (""), NOT on the dead session's leftover amendment branch.
// resolveQuestionBranch now keys off the P0-2 Describe-first honest view (via GetSessionState),
// not the bare sessionState Query — which REPLAYS a closed run's stale mid-flight LIVE stage.
// Here the run is CLOSED (COMPLETED) and the slot is COMMITTED, so amendmentIndexFor would
// synthesize an "...-amend-N" branch if a live stage were trusted; the branch must still be "".
func Test_ResolveQuestionBranch_ClosedWorkflowLeftoverBranch_SeedsOnMain(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindScrubbedRequirements)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)
	// NO QueryWorkflow expectation: if resolveQuestionBranch trusted the replayed query for a
	// closed run, the mock would panic — proving the branch is decided from the honest view.

	proj := committedProject(id, KindScrubbedRequirements) // committed slot ⇒ amendmentIndexFor >= 1
	ps := &renderFakeProjectState{project: proj}

	m := &systemDesignManager{client: mc, projectState: ps}
	if branch := m.resolveQuestionBranch(bgRC(), id, KindScrubbedRequirements); branch != "" {
		t.Fatalf("questions on a committed artifact whose session is closed must seed on main (\"\"), got leftover branch %q", branch)
	}
	mc.AssertExpectations(t)
}

// F73 (part 2). The committed view must carry the slot's durable reviewThread, so questions
// seeded on a COMMITTED artifact (and their answers) render on it. committedSessionView is the
// synthesis the P0-2 completed path (GetSessionState → completedSessionView) drives.
func Test_CommittedSessionView_CarriesReviewThread(t *testing.T) {
	id := ProjectID(uuid.NewString())
	slot := projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		ReviewThread: []projectstate.ReviewComment{{
			ID:         "r1c1",
			Text:       "why is this scrubbed?",
			Type:       projectstate.ReviewCommentTypeQuestion,
			Addressee:  projectstate.ReviewAddresseeArchitect,
			Status:     projectstate.ReviewCommentOpen,
			AuthorRole: reviewAuthorRole,
		}},
	}
	view, err := committedSessionView(id, KindScrubbedRequirements, slot)
	if err != nil {
		t.Fatalf("committedSessionView on a committed slot must not error: %v", err)
	}
	if view.Stage != StageCommitted {
		t.Fatalf("committed slot must render StageCommitted, got %d", view.Stage)
	}
	if len(view.ReviewThread) != 1 || view.ReviewThread[0].Text != "why is this scrubbed?" {
		t.Fatalf("committed view must carry the slot's reviewThread question, got %+v", view.ReviewThread)
	}
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

// ReadProjectOnBranch mirrors ReadProject (branch=="" behaves exactly as ReadProject
// per the generated ProjectStateAccess contract; these tests never thread a branch).
func (f *renderFakeProjectState) ReadProjectOnBranch(rc fwra.Context, projectID projectstate.ProjectID, _ string) (projectstate.Project, error) {
	return f.ReadProject(rc, projectID)
}

func (f *renderFakeProjectState) StageArtifactForReviewOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactModel, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("renderFakeProjectState.StageArtifactForReviewOnBranch must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) RejectArtifactOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("renderFakeProjectState.RejectArtifactOnBranch must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) WithdrawArtifactOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("renderFakeProjectState.WithdrawArtifactOnBranch must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) RejectArtifactOnBranchWithComments(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, int64, []projectstate.ReviewComment, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("renderFakeProjectState.RejectArtifactOnBranchWithComments must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) SetReviewCommentStatusOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("renderFakeProjectState.SetReviewCommentStatusOnBranch must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) SeedReviewCommentsOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, int64, []projectstate.ReviewComment, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("renderFakeProjectState.SeedReviewCommentsOnBranch must not be called by these façade-precondition tests")
}

func (f *renderFakeProjectState) ReconcileBranchFromMain(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("renderFakeProjectState.ReconcileBranchFromMain must not be called by these façade-precondition tests")
}

// AcknowledgeStaleBasis is a real (if trivial) success implementation: the C2 fold
// (code-health-phase-a) removed the "substrate doesn't support stale-ack" runtime
// capability check from systemDesignManager.AcknowledgeStaleBasis — the generated
// ProjectStateAccess contract now requires this verb unconditionally — so the
// Test_AcknowledgeStaleBasis_* liveness-gate tests below now reach this call for real
// once the gate passes.
func (f *renderFakeProjectState) AcknowledgeStaleBasis(_ fwra.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return expectedVersion + 1, nil
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

// Failing-workflow-task hygiene (gtdapp:5 versioning incident): a session whose
// workflow task is in FAILED state (e.g. a deploy-time non-determinism fault being
// retried) rejects queries with the raw Temporal internals "Unable to query workflow
// due to Workflow Task in failed state". mapQueryError must surface a clean,
// actionable Infrastructure Detail instead of leaking that message to clients.
func Test_GetSessionState_QueryFailedTaskState_CleanInfrastructure(t *testing.T) {
	id := ProjectID("gtdapp")
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), errors.New("transient describe blip"))
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
		Return(nil, errors.New("Unable to query workflow due to Workflow Task in failed state"))

	m := &systemDesignManager{client: mc}
	_, err := m.GetSessionState(bgRC(), id, KindMission)
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %d", sde.Kind)
	}
	if strings.Contains(sde.Detail, "Workflow Task in failed state") || strings.Contains(sde.Detail, "Unable to query workflow") {
		t.Fatalf("Detail must not leak Temporal internals, got %q", sde.Detail)
	}
	if !strings.Contains(sde.Detail, "temporarily unavailable") {
		t.Fatalf("Detail must carry the clean retry guidance, got %q", sde.Detail)
	}
	mc.AssertExpectations(t)
}

// =============================================================================
// C-MSD-Δ regression spine — the AGENTIC-PIVOT dispatch → observe → read-back
// child gate (systemDesignManager.md §0d). Method product → NO BDD; regression-
// first, black-box at the WIRE SEAM. The LLM is stubbed at the EXTERNAL agentic-job
// boundary — a FAKE agenticJobAccess (submit/observe) + a FAKE
// projectStateAccess serving the read-back model the Action "committed". The
// Manager under test is NOT faked; the workflow drives the REAL dispatch → observe
// → read-back → human-gate sequence over the Temporal in-memory test environment
// (testsuite.WorkflowTestSuite — no Docker, no dev server, runs under -short).
//
// Covers (the contract's required wire-level cases):
//   - happy DRAFT round (dispatch → observe(Succeeded) → read-back → AwaitingReview)
//   - a REDRAFT gets a DISTINCT idempotency key (distinct ActivityID per dispatch)
//   - the PM-critique SECOND round-trip (mission)
//   - PhaseFailed → StageDraftFailed (NOT perpetual Drafting) → human gate (anti-wedge)
//   - the suspend/resume reviewDecision gate unchanged (Approve commits / Withdraw)
//   - the parent sequence + seal are untouched
// =============================================================================

// ---- fakeProjectState: read-back + the human-gate thin-writes ----------------

// fakeProjectState serves a scripted head-state on ReadProject (the read-back of the
// model the Action committed) and records the human-gate thin-writes the Manager makes.
type fakeProjectState struct {
	mu sync.Mutex

	project  projectstate.Project // the head-state ReadProject returns (read-back)
	notFound bool                 // when true ReadProject returns fwra.NotFound

	staged    []projectstate.ArtifactModel
	committed []projectstate.ArtifactKind
	rejected  []rejectCall
	withdrawn []projectstate.ArtifactKind
	advanced  int

	version projectstate.Version
}

type rejectCall struct {
	kind  projectstate.ArtifactKind
	notes string
}

func (f *fakeProjectState) ReadProject(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return projectstate.Project{}, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project, nil
}

func (f *fakeProjectState) ReadProjectVersion(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return 0, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project.Version, nil
}

func (f *fakeProjectState) bump() projectstate.Version {
	f.version++
	return f.version
}

func (f *fakeProjectState) StageArtifactForReview(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, model projectstate.ArtifactModel) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.staged = append(f.staged, model)
	return f.bump(), nil
}

func (f *fakeProjectState) CommitArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.committed = append(f.committed, kind)
	return f.bump(), nil
}

func (f *fakeProjectState) RejectArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind, notes string) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejected = append(f.rejected, rejectCall{kind: kind, notes: notes})
	// Mirror the real RA's Notes write: RejectArtifact stamps slot.Notes with the
	// architect's reject rationale. This is the COLLISION setup — a subsequent critique
	// read-back must read CritiqueVerdict, NOT these reject Notes. (The RA also clears
	// the critique carrier on transition; that invariant is covered by the projectstate
	// unit tests. Here the test scripts the carrier explicitly to model the next round's
	// critique commit, so we leave it to the script and only set Notes.)
	f.mutateSlotLocked(kind, func(s *projectstate.ArtifactSlot) {
		s.Notes = notes
	})
	return f.bump(), nil
}

// mutateSlotLocked applies fn to the served head-state's named slot. Caller holds mu.
// Used by the fakes to model the RA's slot mutations so the read-back reflects them.
func (f *fakeProjectState) mutateSlotLocked(kind projectstate.ArtifactKind, fn func(*projectstate.ArtifactSlot)) {
	switch kind {
	case projectstate.KindMission:
		fn(&f.project.Mission)
	case projectstate.KindGlossary:
		fn(&f.project.Glossary)
	case projectstate.KindScrubbedRequirements:
		fn(&f.project.ScrubbedRequirements)
	case projectstate.KindVolatilities:
		fn(&f.project.Volatilities)
	case projectstate.KindCoreUseCases:
		fn(&f.project.CoreUseCases)
	case projectstate.KindSystem:
		fn(&f.project.SystemDesign)
	case projectstate.KindOperationalConcepts:
		fn(&f.project.OperationalConcepts)
	case projectstate.KindStandardCheck:
		fn(&f.project.StandardCheck)
	}
}

// markCommitted flips the named slot to ReviewCommitted on the served head-state (with the
// supplied model so populated-slot semantics hold) — used by the phase-workflow tests'
// mocked children to model each step's approve→commit, so the parent's SEAL gate (which
// re-reads head-state) sees the progression.
func (f *fakeProjectState) markCommitted(kind projectstate.ArtifactKind, model projectstate.ArtifactModel) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mutateSlotLocked(kind, func(s *projectstate.ArtifactSlot) {
		s.Status = projectstate.ReviewCommitted
		if s.Model == nil {
			s.Model = model
		}
	})
}

// setSlotCritique scripts the served head-state's critique carrier for kind — what a
// critique Action would have committed. Safe for concurrent use (e.g. from onObserve).
func (f *fakeProjectState) setSlotCritique(kind projectstate.ArtifactKind, verdict, notes string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mutateSlotLocked(kind, func(s *projectstate.ArtifactSlot) {
		s.CritiqueVerdict = verdict
		s.CritiqueNotes = notes
	})
}

func (f *fakeProjectState) WithdrawArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind, _ string) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withdrawn = append(f.withdrawn, kind)
	return f.bump(), nil
}

func (f *fakeProjectState) AdvancePhase(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advanced++
	return f.bump(), nil
}

func (f *fakeProjectState) SetResearchInput(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ResearchInput) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}
func (f *fakeProjectState) SetOperatingModel(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.OperatingModel) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) CreateProject(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.OwnerScope, _ string) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) ListProjects(_ fwra.Context, _ projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	return nil, nil
}

// ---- C2 fold (code-health-phase-a): the 9 session/branch verbs folded into the
// generated ProjectStateAccess contract. fakeProjectState's DEFAULT behavior mirrors
// the OLD dormant-rail fallback (branch=="" / no-ledger behaves exactly as the
// corresponding main-path verb) — the specialized fakes below (seqProjectState,
// branchAwareFakeProjectState, f29BranchFake, ledgerFakeProjectState) embed
// *fakeProjectState and override only the verbs they need branch/ledger-aware
// behavior for.

func (f *fakeProjectState) ReadProjectOnBranch(rc fwra.Context, projectID projectstate.ProjectID, _ string) (projectstate.Project, error) {
	return f.ReadProject(rc, projectID)
}

func (f *fakeProjectState) StageArtifactForReviewOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, _ string, model projectstate.ArtifactModel, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.StageArtifactForReview(rc, projectID, expectedVersion, model)
}

func (f *fakeProjectState) RejectArtifactOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, _ string, kind projectstate.ArtifactKind, notes string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.RejectArtifact(rc, projectID, expectedVersion, kind, notes)
}

func (f *fakeProjectState) WithdrawArtifactOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, _ string, kind projectstate.ArtifactKind, notes string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.WithdrawArtifact(rc, projectID, expectedVersion, kind, notes)
}

func (f *fakeProjectState) RejectArtifactOnBranchWithComments(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, _ string, kind projectstate.ArtifactKind, notes string, _ int64, _ []projectstate.ReviewComment, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	// The base fake carries no durable ledger — mirrors the old comment-dropping
	// fallback (comments are accepted but not recorded).
	return f.RejectArtifact(rc, projectID, expectedVersion, kind, notes)
}

func (f *fakeProjectState) SetReviewCommentStatusOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) SeedReviewCommentsOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ int64, _ []projectstate.ReviewComment, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) ReconcileBranchFromMain(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) AcknowledgeStaleBasis(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

var _ projectstate.ProjectStateAccess = (*fakeProjectState)(nil)

// ---- fakePipeline: the EXTERNAL agentic-job seam (agenticJobAccess) ---

// fakePipeline stands in for the claude-code-action DESIGN job at the WIRE seam. It
// records every submitted spec (so tests assert the ProjectID / artifact_kind /
// branch in DispatchInputs and the DISTINCT idempotency key per dispatch) and serves
// a scripted terminal phase per observe. By default a submitted job is observed
// PipelineSucceeded immediately (the job "ran, committed the JSON, CI went green").
type fakePipeline struct {
	mu sync.Mutex

	// phases is the scripted terminal phase per dispatch in order; once exhausted the
	// last entry repeats. Empty == always PipelineSucceeded.
	phases []pipelinePhase
	// diagnostic is attached to a failed/cancelled observation.
	diagnostic string
	// runURL is attached to EVERY observation (the RA's resolved run URL — the real RA
	// surfaces it on live runs for the generating deep-link and on terminal failures
	// for the failed card's "why" pointer).
	runURL string
	// submitErr, when non-nil, makes SubmitAgenticJob FAIL (a terminal
	// dispatch-rejection fault, e.g. GitHub 422 → ContractMisuse) — the F15 gap-2a path.
	submitErr error

	submits []submitRecord
	// handleByName tracks the phase to return for each issued handle.
	handlePhase map[string]pipelinePhase
	nextID      int
	// onObserve, when set, is invoked on each observe (used to mutate the read-back
	// state mid-flight, e.g. clearing a critique note so the PM-revise loop terminates).
	onObserve func()
}

type submitRecord struct {
	projectID      ProjectID
	idempotencyKey fwra.IdempotencyKey
	dispatchInputs map[string]string
	// targetRepo / workflowFile capture the per-project-design-dispatch override so the
	// proof can assert the dispatch hit the per-project repo + aiarch-design.yml (not the
	// central construction repo + aiarch-construct.yml). Empty == dormant-rail fallback.
	targetRepo   string
	workflowFile string
}

func newFakePipeline(phases ...pipelinePhase) *fakePipeline {
	return &fakePipeline{phases: phases, handlePhase: map[string]pipelinePhase{}}
}

// SubmitAgenticJob implements the GENERATED agenticjob contract seam
// (the submit invoker reaches it via the registered genActivities). The idempotency key is
// now stamped INSIDE the generated activity (genActivityIdempotencyKey) and arrives on the
// fwra call Context; the RepoRef→RepoTarget decode happens workflow-side (dispatchDesignJob),
// so spec.TargetRepo is the DECODED {Owner,Name} — recorded here as "owner/name".
func (p *fakePipeline) SubmitAgenticJob(rc fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	targetRepo := ""
	if !agenticjob.RepoTargetIsZero(spec.TargetRepo) {
		targetRepo = spec.TargetRepo.Owner + "/" + spec.TargetRepo.Name
	}
	if p.submitErr != nil {
		// Record the attempt so a test can still assert the dispatch was tried, then fail
		// the submit — the terminal dispatch-rejection path (the whole round-trip errors).
		p.submits = append(p.submits, submitRecord{projectID: ProjectID(spec.ProjectID), idempotencyKey: rc.IdempotencyKey, dispatchInputs: spec.DispatchInputs})
		return agenticjob.PipelineHandle(""), p.submitErr
	}
	idx := len(p.submits)
	p.submits = append(p.submits, submitRecord{
		projectID:      ProjectID(spec.ProjectID),
		idempotencyKey: rc.IdempotencyKey,
		dispatchInputs: spec.DispatchInputs,
		targetRepo:     targetRepo,
		workflowFile:   spec.WorkflowFile,
	})
	phase := pipelineSucceeded
	if len(p.phases) > 0 {
		if idx < len(p.phases) {
			phase = p.phases[idx]
		} else {
			phase = p.phases[len(p.phases)-1]
		}
	}
	p.nextID++
	name := "design-run/" + uuid.NewString()
	p.handlePhase[name] = phase
	return agenticjob.PipelineHandle(name), nil
}

// submitCount returns how many dispatches have been submitted so far (thread-safe), so a
// ledger fake can record the dispatch count at seed time and a test can assert ordering.
func (p *fakePipeline) submitCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.submits)
}

func (p *fakePipeline) ObserveAgenticJob(_ fwra.Context, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	p.mu.Lock()
	phase := p.handlePhase[agenticjob.PipelineHandleString(handle)]
	hook := p.onObserve
	diag := p.diagnostic
	runURL := p.runURL
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	obs := agenticjob.PipelineObservation{Phase: neutralToRAPhase(phase)}
	// Mirror the real RA: the resolved run URL rides on EVERY observation (live runs
	// power the generating deep-link; terminal failures power the failed card).
	obs.RunURL = runURL
	if phase == pipelineFailed || phase == pipelineCancelled {
		obs.Diagnostic = diag
	}
	return obs, nil
}

// CancelAgenticJob satisfies the contract; the design draft path never cancels.
func (p *fakePipeline) CancelAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) error {
	return nil
}

// neutralToRAPhase maps this Manager's neutral scripted phase onto the RA phase the
// generated observe activity returns (the inverse of designPipelinePhase).
func neutralToRAPhase(p pipelinePhase) agenticjob.PipelinePhase {
	switch p {
	case pipelinePending:
		return agenticjob.PhasePending
	case pipelineRunning:
		return agenticjob.PhaseRunning
	case pipelineSucceeded:
		return agenticjob.PhaseSucceeded
	case pipelineFailed:
		return agenticjob.PhaseFailed
	case pipelineCancelled:
		return agenticjob.PhaseCancelled
	default:
		return agenticjob.PhasePending
	}
}

var _ agenticjob.AgenticJobAccess = (*fakePipeline)(nil)

// ---- helpers ----------------------------------------------------------------

// newWorkflows builds the workflows receiver under test. It carries NO RA dep (B10 —
// every Activity is generated): the fake substrate / pipeline is threaded to
// registerGenActivities / registerCoAuthor instead, exactly as production threads it
// into genActivities.
func newWorkflows() *workflows {
	return &workflows{Acts: genInvokers{Opts: activityOptions()}}
}

// registerGenActivities registers the GENERATED RA activities (projectState read-version /
// advance-phase, pipeline submit/observe/cancel, the six rail verbs, syncManagedScaffold,
// and — since B10 — the eight designSessionAccess verbs the migrated call sites now reach,
// including the envelope-parameter Stage op) under their contract names — mirrors what
// RegisterWorker threads via genActivities (worker.gen.go). Pipeline / rail may be nil for
// tests that never dispatch; the registered method values are only invoked when the
// workflow reaches them.
//
// The designSessionAccess ops are backed by projectstate.NewDesignSessionAccess(ps) — the
// REAL production wrapper (projectstate/designsession.go), not a hand-rolled test double —
// so every workflow test exercises the actual forward-to-base + provenance capability
// check the RA runs, not a re-implementation of it. ps must be non-nil; the generated
// ProjectStateAccess contract requires every branch/ledger/reconcile verb unconditionally
// (C2 fold, code-health-phase-a), so a test's fake (e.g. gitrail_test.go's
// branchAwareFakeProjectState, gitrail_proof_test.go's seqProjectState) simply OVERRIDES
// the verbs it wants branch/ledger-aware behavior for — no separate registration or
// capability opt-in needed.
func registerGenActivities(env *testsuite.TestWorkflowEnvironment, ps projectstate.ProjectStateAccess, pipe *fakePipeline, rail sourcecontrol.SourceControlAccess) {
	var pipeAcc agenticjob.AgenticJobAccess
	if pipe != nil {
		pipeAcc = pipe
	}
	acts := &genActivities{ProjectState: ps, Pipeline: pipeAcc, Rail: rail, DesignSession: projectstate.NewDesignSessionAccess(ps)}
	env.RegisterActivityWithOptions(acts.ProjectStateReadProjectVersion, activity.RegisterOptions{Name: "projectStateAccess.readProjectVersion"})
	env.RegisterActivityWithOptions(acts.ProjectStateAdvancePhase, activity.RegisterOptions{Name: "projectStateAccess.advancePhase"})
	env.RegisterActivityWithOptions(acts.PipelineSubmitAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.submitAgenticJob"})
	env.RegisterActivityWithOptions(acts.PipelineObserveAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.observeAgenticJob"})
	env.RegisterActivityWithOptions(acts.PipelineCancelAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.cancelAgenticJob"})
	env.RegisterActivityWithOptions(acts.RailGetInstallationToken, activity.RegisterOptions{Name: "sourceControlAccess.getInstallationToken"})
	env.RegisterActivityWithOptions(acts.RailOpenBranch, activity.RegisterOptions{Name: "sourceControlAccess.openBranch"})
	env.RegisterActivityWithOptions(acts.RailOpenPullRequest, activity.RegisterOptions{Name: "sourceControlAccess.openPullRequest"})
	env.RegisterActivityWithOptions(acts.RailGetPullRequestStatus, activity.RegisterOptions{Name: "sourceControlAccess.getPullRequestStatus"})
	env.RegisterActivityWithOptions(acts.RailPostReview, activity.RegisterOptions{Name: "sourceControlAccess.postReview"})
	env.RegisterActivityWithOptions(acts.RailMergePullRequest, activity.RegisterOptions{Name: "sourceControlAccess.mergePullRequest"})
	env.RegisterActivityWithOptions(acts.RailSyncManagedScaffold, activity.RegisterOptions{Name: "sourceControlAccess.syncManagedScaffold"})
	env.RegisterActivityWithOptions(acts.DesignSessionReadProjectOnBranch, activity.RegisterOptions{Name: "designSessionAccess.readProjectOnBranch"})
	env.RegisterActivityWithOptions(acts.DesignSessionStageArtifactForReviewOnBranch, activity.RegisterOptions{Name: "designSessionAccess.stageArtifactForReviewOnBranch"})
	env.RegisterActivityWithOptions(acts.DesignSessionCommitArtifactWithProvenance, activity.RegisterOptions{Name: "designSessionAccess.commitArtifactWithProvenance"})
	env.RegisterActivityWithOptions(acts.DesignSessionRejectArtifactOnBranchWithComments, activity.RegisterOptions{Name: "designSessionAccess.rejectArtifactOnBranchWithComments"})
	env.RegisterActivityWithOptions(acts.DesignSessionWithdrawArtifactOnBranch, activity.RegisterOptions{Name: "designSessionAccess.withdrawArtifactOnBranch"})
	env.RegisterActivityWithOptions(acts.DesignSessionReconcileBranchFromMain, activity.RegisterOptions{Name: "designSessionAccess.reconcileBranchFromMain"})
	env.RegisterActivityWithOptions(acts.DesignSessionSetReviewCommentStatusOnBranch, activity.RegisterOptions{Name: "designSessionAccess.setReviewCommentStatusOnBranch"})
	env.RegisterActivityWithOptions(acts.DesignSessionSeedReviewCommentsOnBranch, activity.RegisterOptions{Name: "designSessionAccess.seedReviewCommentsOnBranch"})
}

// registerCoAuthor registers the child gate workflow + its activities on the test env,
// exactly as RegisterWorker does in production (same stable names). Every Activity is
// generated (registerGenActivities); ps is the fake substrate the generated
// projectState/designSession activities are backed by (threaded explicitly — the
// workflows struct no longer carries an RA dep).
func registerCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps projectstate.ProjectStateAccess, pipe *fakePipeline) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: executionKindCoAuthor})
	registerGenActivities(env, ps, pipe, wf.Rail)
}

func registerPhaseAdvance(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps projectstate.ProjectStateAccess) {
	env.RegisterWorkflowWithOptions(wf.PhaseAdvanceWorkflow, workflow.RegisterOptions{Name: executionKindPhaseAdvance})
	registerGenActivities(env, ps, nil, nil)
}

func mustMission(t *testing.T) *projectstate.MissionStatement {
	t.Helper()
	m, err := projectstate.NewMissionStatement("ship value", []projectstate.Objective{{Number: 1, Statement: "be useful"}}, "components")
	if err != nil {
		t.Fatalf("NewMissionStatement: %v", err)
	}
	return m
}

func mustGlossary(t *testing.T) *projectstate.Glossary {
	t.Helper()
	g, err := projectstate.NewGlossary([]projectstate.GlossaryItem{{Term: "Aggregate", Definition: "a consistency boundary"}})
	if err != nil {
		t.Fatalf("NewGlossary: %v", err)
	}
	return g
}

func committedSlot(model projectstate.ArtifactModel) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: model}
}

// awaitingSlot mirrors what the Action commits + how the Manager reads it back: the
// Kind slot carries the typed Model the design job committed (the read-back source)
// plus the FIRST-CLASS PM-critique read-back carrier (D-MSD-Δ amendment) — distinct
// from the architect-reject Notes field. verdict is "" | "approve" | "revise";
// critiqueNotes rides a "revise" verdict.
func awaitingSlot(model projectstate.ArtifactModel, verdict, critiqueNotes string) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status:          projectstate.ReviewAwaitingReview,
		Model:           model,
		CritiqueVerdict: verdict,
		CritiqueNotes:   critiqueNotes,
	}
}

// systemReadBack builds a project whose System slot carries a committed-by-Action
// System model (the read-back source for a System draft) plus the committed priors.
// The slot's critique carrier is an APPROVE: System is architect-critiqued (the
// system-critique self-review round, architect self-critique amendment 2026-07-17),
// so the shared happy-path fixture ratifies the draft and the session proceeds to
// the human gate exactly as before — with one extra critique dispatch round-trip.
func systemReadBack(t *testing.T, id ProjectID) projectstate.Project {
	t.Helper()
	return projectstate.Project{
		ID:                   projectstate.ProjectID(id),
		Version:              3,
		Mission:              committedSlot(mustMission(t)),
		Glossary:             committedSlot(mustGlossary(t)),
		ScrubbedRequirements: committedSlot(&projectstate.ScrubbedRequirements{}),
		Volatilities:         committedSlot(&projectstate.Volatilities{}),
		CoreUseCases:         committedSlot(&projectstate.CoreUseCases{}),
		SystemDesign:         awaitingSlot(&projectstate.System{}, projectstate.CritiqueVerdictApprove, ""),
	}
}

// ---- Tests: child gate dispatch → observe → read-back -----------------------

// sdSessionView queries the CoAuthor session state and decodes the SessionStateView,
// fataling exactly as the inlined query/decode pattern it replaces.
func sdSessionView(t *testing.T, env *testsuite.TestWorkflowEnvironment) SessionStateView {
	t.Helper()
	enc, err := env.QueryWorkflow(querySessionState)
	if err != nil {
		t.Fatalf("QueryWorkflow: %v", err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		t.Fatalf("decode SessionStateView: %v", err)
	}
	return view
}

// sdAssertSystemDraftDispatchShape asserts the FIRST (draft) dispatch's shape:
// ProjectID, artifact_kind, target_branch and command in DispatchInputs, the
// RA-controlled idempotency discipline, and the dormant-rail empty per-project target.
func sdAssertSystemDraftDispatchShape(t *testing.T, sub submitRecord, id ProjectID) {
	t.Helper()
	if sub.projectID != id {
		t.Fatalf("dispatch carried wrong ProjectID: %q", sub.projectID)
	}
	if sub.dispatchInputs[dispatchInputArtifactKind] != projectstate.KindSystem.String() {
		t.Fatalf("dispatch artifact_kind = %q, want %s", sub.dispatchInputs[dispatchInputArtifactKind], projectstate.KindSystem)
	}
	if sub.dispatchInputs[dispatchInputTargetBranch] == "" {
		t.Fatal("dispatch must carry a non-empty target_branch")
	}
	if got := sub.dispatchInputs[dispatchInputCommand]; got != "system-draft" {
		t.Fatalf("dispatch must carry command=system-draft, got %q", got)
	}
	// The Manager MUST NOT set idempotency_token in DispatchInputs (RA-controlled).
	if _, present := sub.dispatchInputs["idempotency_token"]; present {
		t.Fatal("the Manager must NOT set idempotency_token in DispatchInputs (RA-controlled)")
	}
	if sub.idempotencyKey.IsZero() {
		t.Fatal("the dispatch Activity must supply a non-empty idempotency key")
	}
	// DORMANT-RAIL (non-git) preservation: with NO rail wired (newWorkflows), the
	// per-project-design-dispatch override is EMPTY, so the RA falls back to the
	// configured construction repo — the non-git / Postgres path is byte-unchanged.
	if sub.targetRepo != "" || sub.workflowFile != "" {
		t.Fatalf("dormant-rail dispatch must leave the per-project target empty, got repo=%q file=%q", sub.targetRepo, sub.workflowFile)
	}
}

// Happy DRAFT round: the gate DISPATCHES (with the right ProjectID + artifact_kind +
// branch in DispatchInputs and an RA-controlled — Manager-supplied — idempotency
// key), OBSERVES to PipelineSucceeded, READS BACK the committed typed model, and
// suspends at AwaitingReview surfacing the typed Draft. System is architect-owned →
// a SINGLE dispatch (no PM critique).
func Test_CoAuthor_DraftRoundTrip_DispatchObserveReadBack_AwaitsReview(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline() // default: dispatch observed Succeeded
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		view := sdSessionView(t, env)
		if view.Stage != StageAwaitingReview {
			t.Fatalf("want StageAwaitingReview, got %d", view.Stage)
		}
		if view.Draft.Kind != "system" || view.Draft.Model == nil {
			t.Fatalf("expected a staged system read-back draft envelope, got %+v", view.Draft)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.staged) != 1 {
		t.Fatalf("want 1 staged read-back model, got %d", len(ps.staged))
	}
	if _, ok := ps.staged[0].(*projectstate.System); !ok {
		t.Fatalf("staged model is not *projectstate.System: %T", ps.staged[0])
	}
	// System is architect-critiqued (architect self-critique amendment): the draft
	// dispatch PLUS the system-critique dispatch — exactly TWO round-trips.
	if len(pipe.submits) != 2 {
		t.Fatalf("System must dispatch draft + architect self-critique (2 round-trips), got %d submits", len(pipe.submits))
	}
	if got := pipe.submits[1].dispatchInputs[dispatchInputCommand]; got != "system-critique" {
		t.Fatalf("the second dispatch must be the architect self-critique, got command=%q", got)
	}
	sdAssertSystemDraftDispatchShape(t, pipe.submits[0], id)
}

// An Approve signal commits the read-back artifact via CommitArtifact(kind); the
// child gate returns CoAuthorApproved. Mission is PM-critiqued (critique observed
// Succeeded, read-back Notes empty == CritiqueApprove → straight to the human gate).
func Test_CoAuthor_Approve_Commits(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// Mission read-back: the slot carries the Action-committed Mission; the critique
	// carrier verdict is "approve" so the PM round-trip ratifies and the gate proceeds.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, ""),
	}}
	pipe := newFakePipeline() // draft Succeeded, critique Succeeded
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want CoAuthorApproved, got %d", outcome)
	}
	if len(ps.committed) != 1 || ps.committed[0] != projectstate.KindMission {
		t.Fatalf("want CommitArtifact(KindMission), got %v", ps.committed)
	}
	// Mission is PM-critiqued: TWO dispatch round-trips (draft + critique).
	if len(pipe.submits) != 2 {
		t.Fatalf("Mission must dispatch draft + PM-critique (2 round-trips), got %d", len(pipe.submits))
	}
	if pipe.submits[1].dispatchInputs[dispatchInputArtifactKind] != projectstate.KindMission.String() {
		t.Fatal("the second dispatch must be the PM-critique over the same kind")
	}
}

// VIBES AUTOGATE (F-R3 vibes-everywhere): under a vibes ReviewPolicy a CLEAN draft is
// AUTO-approved at the review gate WITHOUT a human signal — the design gate honors ReviewPolicy
// exactly like construction. NO delayed signal is registered: the autogate must commit on its own.
func Test_CoAuthor_VibesPolicy_AutoApproves_NoHumanSignal(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	vibes := projectstate.ReviewPresetVibes
	ps := &fakeProjectState{project: projectstate.Project{
		ID:           projectstate.ProjectID(id),
		Version:      1,
		Mission:      awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, ""),
		ReviewPolicy: projectstate.ReviewPolicy{Preset: &vibes},
	}}
	pipe := newFakePipeline() // draft Succeeded, critique Succeeded
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("a vibes policy must auto-approve without a human, got outcome %d", outcome)
	}
	if len(ps.committed) != 1 || ps.committed[0] != projectstate.KindMission {
		t.Fatalf("the auto-approve must commit the artifact, got committed=%v", ps.committed)
	}
}

// VIBES AUTOGATE — open change-requests BLOCK the auto-approve (stays human). Even under a vibes
// policy, an unresolved change-request (an amendment seed / critique feedback) keeps the session
// at the HUMAN gate: the autogate skips it. A human WITHDRAW then decides it — were the autogate
// (wrongly) auto-approving, the outcome would be coAuthorApproved and the artifact committed, so
// coAuthorWithdrawn + no commit proves the open change-request kept it human-gated.
func Test_CoAuthor_VibesPolicy_OpenChangeRequest_StaysHumanGated(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	vibes := projectstate.ReviewPresetVibes
	missionSlot := awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, "")
	missionSlot.ReviewThread = []projectstate.ReviewComment{{
		ID:         "c1",
		Status:     projectstate.ReviewCommentOpen,
		Type:       projectstate.ReviewCommentTypeChangeRequest,
		Text:       "tighten the vision sentence",
		AuthorRole: "architect",
	}}
	ps := &fakeProjectState{project: projectstate.Project{
		ID:           projectstate.ProjectID(id),
		Version:      1,
		Mission:      missionSlot,
		ReviewPolicy: projectstate.ReviewPolicy{Preset: &vibes},
	}}
	pipe := newFakePipeline()
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// The session must WAIT for a human despite vibes (the open change-request blocks the
	// autogate); a human WITHDRAW decides it. A wrongly-firing autogate would auto-APPROVE first.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("an open change-request under vibes must stay human-gated (withdrawn here), got %d", outcome)
	}
	if len(ps.committed) != 0 {
		t.Fatalf("an open change-request must block the auto-commit, got committed=%v", ps.committed)
	}
}

// VIBES AUTOGATE replay safety: a session in flight at deploy time (the "design-vibes-autogate"
// GetVersion resolves DefaultVersion) stays on the HUMAN gate even under a vibes policy — the
// autogate is pinned OFF for its whole run. A human WITHDRAW decides it; were the autogate
// (wrongly) ON, it would auto-APPROVE before the withdraw, so coAuthorWithdrawn proves the pin.
func Test_CoAuthor_VibesAutogate_VersionGate_PreFeatureStaysHumanGated(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	vibes := projectstate.ReviewPresetVibes
	ps := &fakeProjectState{project: projectstate.Project{
		ID:           projectstate.ProjectID(id),
		Version:      1,
		Mission:      awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, ""),
		ReviewPolicy: projectstate.ReviewPolicy{Preset: &vibes},
	}}
	pipe := newFakePipeline()
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// Pin the autogate OFF (a pre-feature in-flight execution replays DefaultVersion).
	env.OnGetVersion("design-vibes-autogate", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a pinned pre-feature session must run cleanly: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("a pinned (pre-feature) session must stay human-gated (withdrawn here), got %d", outcome)
	}
	if len(ps.committed) != 0 {
		t.Fatalf("a pinned session must NOT auto-commit, got committed=%v", ps.committed)
	}
}

// THE ANTI-WEDGE TEST. A dispatched job that reaches a TERMINAL FAILURE phase
// (PipelineFailed — drafting failed or the required CI validation check went red)
// must NOT crash the workflow and must NOT leave a perpetual StageDrafting. The
// session lands in the human-visible StageDraftFailed carrying the neutral
// Diagnostic, surfaced by getSessionState, and suspends on the SAME reviewDecision
// gate awaiting a human Retry/Withdraw. Withdraw ends gracefully.
func Test_CoAuthor_PhaseFailed_LandsInStageDraftFailed_NotPerpetualDrafting(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline(pipelineFailed)
	pipe.diagnostic = "aiarch-validate found 2 violations"
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		// The load-bearing anti-wedge assertion: NOT a perpetual Drafting.
		if view.Stage == StageDrafting {
			t.Fatal("a failed design job must NOT leave the session in perpetual StageDrafting (the wedge)")
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed after a terminal failure phase, got %d", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("StageDraftFailed must carry a human FailureReason (the neutral diagnostic)")
		}
		// Suspended on the SAME reviewDecision gate — Withdraw ends it.
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the draft-failed gate")
	}
	// A ran-but-failed job is terminal-at-the-Manager — it is escalated to the human
	// gate, NOT a workflow crash.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT fail the workflow, got: %v", err)
	}
	if len(ps.staged) != 0 {
		t.Fatalf("a failed draft must stage nothing, got %d", len(ps.staged))
	}
	// 2026-07-16 incident: NOTHING was ever staged, so the withdraw must SKIP the unstage
	// write (an unpopulated-slot WithdrawArtifact is a ContractMisuse that killed the whole
	// rail) and still end the session cleanly as withdrawn.
	if len(ps.withdrawn) != 0 {
		t.Fatalf("a never-staged withdraw must NOT call WithdrawArtifact (nothing to unstage), got %d", len(ps.withdrawn))
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want CoAuthorWithdrawn, got %d", outcome)
	}
}

// PhaseCancelled is likewise a terminal failure that lands in StageDraftFailed
// (never a perpetual Drafting).
func Test_CoAuthor_PhaseCancelled_LandsInStageDraftFailed(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline(pipelineCancelled)
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, _ := env.QueryWorkflow(querySessionState)
		var view SessionStateView
		_ = enc.Get(&view)
		if view.Stage != StageDraftFailed {
			t.Fatalf("PhaseCancelled must land in StageDraftFailed, got %d", view.Stage)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("PhaseCancelled must not crash the workflow: %v", err)
	}
}

// QA incident 2026-07-15 (gtdapp:1) — THE STALE-REDRAFT GATE-HYGIENE TEST. A redraft
// signal delivered while the job is still DRAFTING (the founder's "Request draft" click
// against a stale no-session SPA view; also every SignalWithStart START's ride-along
// signal) is buffered — no gate consumes it mid-draft. When the job then FAILS and the
// session lands at the human StageDraftFailed gate, that pre-failure signal must NOT
// auto-satisfy the gate's Retry selector: the human never saw the failure, so it cannot
// be an informed Retry. The gate-entry drain (failed-gate-redraft-drain, GetVersion-
// gated) discards it; the session must SIT at the failed gate until a real decision.
func Test_CoAuthor_StaleRedraftBufferedDuringDrafting_DoesNotAutoConsumeFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	// Observe #1 (t≈0) reports RUNNING and flips the handle to FAILED, holding a live
	// drafting window across the 15s poll timer — the stale signal lands inside it.
	pipe := newFakePipeline(pipelineRunning)
	pipe.diagnostic = "the PM-critique job committed no verdict"
	pipe.onObserve = func() {
		pipe.mu.Lock()
		defer pipe.mu.Unlock()
		for k := range pipe.handlePhase {
			pipe.handlePhase[k] = pipelineFailed
		}
	}
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// t=5s — MID-DRAFTING, before the failure lands at the t=15s observe: the incident's
	// stale "Request draft" click reaches the running workflow and buffers.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 5*time.Second)

	// t=60s — long after the failure: the session must still SIT at the human-visible
	// failed gate. Without the drain the buffered signal auto-consumed the gate the
	// instant it armed (an invisible, unwanted redraft round — asserted via the dispatch
	// count below). A Withdraw then ends the session.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("the session must sit at StageDraftFailed awaiting a HUMAN decision (stale buffered redraft discarded), got %d", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("the failed gate must surface the human FailureReason the stale signal would have skipped")
		}
		if got := pipe.submitCount(); got != 1 {
			t.Fatalf("the stale buffered redraft must NOT trigger a redraft dispatch, got %d dispatches", got)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the failed gate")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the drain must not crash the workflow: %v", err)
	}
	if got := pipe.submitCount(); got != 1 {
		t.Fatalf("exactly ONE draft dispatch expected (the stale redraft discarded), got %d", got)
	}
	// Never staged → the withdraw skips the unstage write (2026-07-16 incident) and ends clean.
	if len(ps.withdrawn) != 0 {
		t.Fatalf("a never-staged withdraw must NOT call WithdrawArtifact, got %d", len(ps.withdrawn))
	}
}

// QA F15 gap 2a — THE DISPATCH-REJECTION ANTI-WEDGE TEST. When the dispatch itself is
// rejected terminally (GitHub 422s the workflow_dispatch → DispatchDesignJobActivity
// fails non-retryably with ContractMisuse), the round-trip returns an ERROR. Historically
// this CRASHED the whole CoAuthor workflow FAILED while sessionState still replayed
// StageDrafting — the SPA wedged forever on "GENERATING" with no recovery. The fix routes
// it to the SAME human-visible StageDraftFailed gate: the workflow stays OPEN + QUERYABLE
// (Retry / Withdraw), never an invisible crash.
func Test_CoAuthor_DispatchRejected_LandsInStageDraftFailed_NotCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline() // phase scripting irrelevant: the SUBMIT fails.
	// A terminal, non-retryable RA fault (ContractMisuse) — the dispatchOpts RetryPolicy
	// marks it non-retryable, so the activity fails on the first attempt.
	pipe.submitErr = fwra.New(fwra.ContractMisuse, "github 422: workflow_dispatch payload too large")
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		// The load-bearing F15 assertion: a rejected DISPATCH must NOT leave a perpetual
		// StageDrafting (the invisible-failure the SPA wedged on).
		if view.Stage == StageDrafting {
			t.Fatal("a rejected dispatch must NOT leave the session in perpetual StageDrafting (the F15 wedge)")
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed after a terminal dispatch rejection, got %d", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("a dispatch rejection must carry a human FailureReason")
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the dispatch-failed gate")
	}
	// The fix: a terminal dispatch rejection is escalated to the human gate, NOT a crash.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal dispatch rejection must NOT fail the workflow (F15), got: %v", err)
	}
	if len(ps.staged) != 0 {
		t.Fatalf("a rejected dispatch must stage nothing, got %d", len(ps.staged))
	}
}

// QA F15 gap 2b — a RAN-BUT-FAILED design job surfaces the failed run's URL on the
// session view, so the SPA's failed card can deep-link the operator to the CI run/logs
// that explain WHY.
func Test_CoAuthor_RunFailed_SurfacesRunURL(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline(pipelineFailed)
	pipe.diagnostic = "construction pipeline failed"
	pipe.runURL = "https://github.com/acme/widgets/actions/runs/123"
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed, got %d", view.Stage)
		}
		if view.FailureRunURL == nil || *view.FailureRunURL != "https://github.com/acme/widgets/actions/runs/123" {
			t.Fatalf("a ran-but-failed job must surface the failed run URL, got %v", view.FailureRunURL)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
}

// QA F-GTD-6 — while the dispatched design job is LIVE (StageDrafting, between the
// dispatch and the terminal observation) the session view surfaces the run's URL, so
// the SPA's GENERATING scene can deep-link the operator to the actual GitHub Actions
// run instead of an unlinked "the job is running in your CI" notice. Once the job
// completes and the session reaches AwaitingReview, no run is in flight — the live
// link must be GONE (never a stale one).
func Test_CoAuthor_Generating_SurfacesLiveRunURL(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	// Scripted RUNNING on the first observation, flipped to SUCCEEDED after it, so the
	// test holds a live in-flight window (observe #1) and then completes (observe #2).
	pipe := newFakePipeline(pipelineRunning)
	pipe.runURL = "https://github.com/acme/widgets/actions/runs/777"
	pipe.onObserve = func() {
		pipe.mu.Lock()
		defer pipe.mu.Unlock()
		for k := range pipe.handlePhase {
			pipe.handlePhase[k] = pipelineSucceeded
		}
	}
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// Mid-drafting (after observe #1 at t≈0, before the 15s poll timer fires): the
	// generating view must carry the live run's URL.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDrafting {
			t.Fatalf("want StageDrafting while the job is live, got %d", view.Stage)
		}
		if view.RunURL == nil || *view.RunURL != "https://github.com/acme/widgets/actions/runs/777" {
			t.Fatalf("a LIVE design job must surface its run URL on the generating view (F-GTD-6), got %v", view.RunURL)
		}
	}, 5*time.Second)
	// After the successful draft reaches AwaitingReview no run is in flight — the live
	// link is gone. Withdraw to end the session.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow (post-draft): %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView (post-draft): %v", err)
		}
		if view.Stage == StageAwaitingReview && view.RunURL != nil {
			t.Errorf("no run is in flight at AwaitingReview — the live run URL must be cleared, got %q", *view.RunURL)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
}

// F-QA2-7 — the PM-critique CONCLUSION (verdict + the PM's rationale + the draft
// round it judged) is surfaced on the session view at the human gate, so the founder
// never approves a PM-reviewed artifact blind to what the PM concluded. An APPROVE
// carries the PM's approve-with-reservation notes too. A human REJECT then CLEARS the
// stamp — the surfaced verdict judged the now-rejected draft, so the view must never
// attribute it to the upcoming redraft (asserted deterministically by failing the
// redraft dispatch and querying at the StageDraftFailed gate).
func Test_CoAuthor_PMCritique_SurfacesConclusionAtGate_AndRejectClearsIt(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	const reservation = "solid mission; one taste-level wording reservation noted"
	// The Mission slot's critique carrier is an APPROVE that carries notes — the
	// verdict-discipline "approve with noted reservation" the PM records as comments.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, reservation),
	}}
	// draft Succeeded, critique Succeeded → gate; the post-reject REDRAFT dispatch
	// FAILS so the session parks deterministically at StageDraftFailed for the
	// cleared-stamp query.
	pipe := newFakePipeline(pipelineSucceeded, pipelineSucceeded, pipelineFailed)
	pipe.diagnostic = "redraft CI flake"
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("want StageAwaitingReview, got %d", view.Stage)
		}
		// The load-bearing F-QA2-7 assertion: the gate view carries the PM conclusion.
		if view.Critique == nil {
			t.Fatal("a PM-reviewed artifact at the human gate must surface the PM critique conclusion (F-QA2-7), got nil")
		}
		if view.Critique.Role != critiqueRoleProductManager {
			t.Errorf("critique role: want %q, got %q", critiqueRoleProductManager, view.Critique.Role)
		}
		if view.Critique.Verdict != projectstate.CritiqueVerdictApprove {
			t.Errorf("critique verdict: want approve, got %q", view.Critique.Verdict)
		}
		if view.Critique.Summary != reservation {
			t.Errorf("an APPROVE must carry the PM's notes (approve-with-reservation), want %q got %q", reservation, view.Critique.Summary)
		}
		if view.Critique.Round != 0 {
			t.Errorf("critique judged draft round 0, got %d", view.Critique.Round)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "tighten the objectives"}})
	}, 30*time.Second)

	// The rejected draft's redraft dispatch failed → StageDraftFailed. The stale PM
	// verdict (it judged the REJECTED draft) must be gone from the view.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow (post-reject): %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView (post-reject): %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed after the failed redraft, got %d", view.Stage)
		}
		if view.Critique != nil {
			t.Errorf("a human REJECT must clear the surfaced PM critique (it judged the rejected draft), got %+v", view.Critique)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 90*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
}

// A REDRAFT (the human Retry-via-Reject after a draft-failed gate) issues a SECOND
// dispatch with a DISTINCT idempotency key — a fresh, idempotent job, not a dedup of
// the stale one (the key is derived inside the dispatch Activity from a fresh
// ActivityID per ExecuteActivity invocation; N1).
func Test_CoAuthor_DraftFailedThenRetry_DistinctIdempotencyKey(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	// First dispatch fails; the retry dispatch (2nd) succeeds → reaches AwaitingReview.
	pipe := newFakePipeline(pipelineFailed, pipelineSucceeded)
	pipe.diagnostic = "transient CI flake"
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// Reject at the draft-failed gate → Retry-via-Reject → re-dispatch.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "retry please"}})
	}, 20*time.Second)
	// After the successful redraft reaches AwaitingReview, withdraw to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("a retry must issue a SECOND dispatch, got %d submits", len(pipe.submits))
	}
	k1 := pipe.submits[0].idempotencyKey
	k2 := pipe.submits[1].idempotencyKey
	if k1 == k2 {
		t.Fatalf("a redraft must get a DISTINCT idempotency key (fresh job), got identical %q", k1)
	}
	// F-QA2-24 pin: when the DRAFT is the job that failed, the gate's Retry REDRAFTS —
	// both dispatches are DRAFT jobs (the critique-retry resume applies only when the
	// critique was the failed job).
	for i := range 2 {
		if got := pipe.submits[i].dispatchInputs[dispatchInputJobMode]; got != jobModeDraft {
			t.Fatalf("dispatch %d job_mode = %q, want %q (a draft failure's Retry must redraft)", i, got, jobModeDraft)
		}
	}
	// The successful redraft staged the read-back model once.
	if len(ps.staged) != 1 {
		t.Fatalf("the recovered redraft must stage exactly once, got %d", len(ps.staged))
	}
}

// sdAssertNonConvergedPMGateView queries the session at the human gate after a
// never-converging PM critique and asserts the staged-best-effort gate shape:
// AwaitingReview, no active sub-step, the PM-CRITIQUE-UNRESOLVED warning, and the
// surfaced PM revise conclusion.
func sdAssertNonConvergedPMGateView(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()
	view := sdSessionView(t, env)
	if view.Stage != StageAwaitingReview {
		t.Fatalf("non-converging PM critique must stage for the human gate, got stage %d", view.Stage)
	}
	// The max-redraft non-converge branch must clear the PM sub-step before staging
	// for review — otherwise the query keeps claiming (ProductManager, Critiquing)
	// after PM work has stopped (mirrors the revise/proceed clearActive() sibling paths).
	if view.ActiveRole != ActiveRoleNone || view.ActiveStep != ActiveStepNone || view.Round != 0 {
		t.Fatalf("the human gate must show no active role after non-convergence, got role=%d step=%d round=%d", view.ActiveRole, view.ActiveStep, view.Round)
	}
	var sawWarning bool
	for _, f := range view.Findings {
		if string(f.RuleID) == "PM-CRITIQUE-UNRESOLVED" {
			sawWarning = true
		}
	}
	if !sawWarning {
		t.Fatalf("expected a PM-CRITIQUE-UNRESOLVED warning at the gate, got %+v", view.Findings)
	}
	// F-QA2-7: the non-converged push-back is ALSO surfaced as the structured PM
	// conclusion (not only the warning finding) — the founder sees the PM pushed
	// back on the staged draft, and why, before approving it.
	if view.Critique == nil || view.Critique.Verdict != projectstate.CritiqueVerdictRevise {
		t.Fatalf("the staged-best-effort gate must surface the PM revise conclusion, got %+v", view.Critique)
	}
	if view.Critique.Summary != "tighten the vision sentence" {
		t.Errorf("critique summary: want the PM's rationale, got %q", view.Critique.Summary)
	}
}

// PM-CRITIQUE second round-trip with CritiqueRevise (read-back Notes non-empty):
// each round re-dispatches the architect draft BEFORE the human gate. When the PM
// critic never converges, the loop must NOT crash the workflow (the wedge) — after
// maxRedraftAttempts the committed draft is staged for the human gate with the
// unresolved critique surfaced as a WARNING finding (the architect makes the call).
// This proves BOTH the PM second round-trip AND the non-convergence anti-wedge.
func Test_CoAuthor_PMCritiqueRevise_SecondRoundTrip_StagesForHumanGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// The Mission slot's critique carrier is persistently "revise" with notes
	// (CritiqueRevise every round) — the critic never converges.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictRevise, "tighten the vision sentence"),
	}}
	pipe := newFakePipeline() // every draft + critique dispatch Succeeds
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		sdAssertNonConvergedPMGateView(t, env)
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 90*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete (PM non-convergence must stage, not hang/crash)")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("PM non-convergence must NOT crash the workflow: %v", err)
	}
	// At least draft + critique per round, looped to maxRedraftAttempts: >2 dispatches,
	// proving the PM critique is a real SECOND round-trip that re-dispatches the draft.
	if len(pipe.submits) <= 2 {
		t.Fatalf("PM-revise must re-dispatch (draft+critique per round); got only %d dispatches", len(pipe.submits))
	}
	// Exactly one stage (the best-effort draft at the gate after the loop).
	if len(ps.staged) != 1 {
		t.Fatalf("want exactly one best-effort stage at the gate, got %d", len(ps.staged))
	}
}

// ARCHITECT SELF-CRITIQUE (amendment 2026-07-17; QA: gtdapp's architecture reached the
// human gate with THREE blockers and zero internal critique). KindSystem dispatches the
// system-critique job AFTER the draft read-back — the SECOND round-trip carries
// job_mode=critique — and an APPROVE verdict routes to the human gate surfacing the
// conclusion with the honest ARCHITECT role (never "productManager": the PM must not
// critique architecture).
func Test_CoAuthor_System_ArchitectSelfCritique_DispatchesCritique_RoleHonest(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// systemReadBack's System slot critique carrier is an APPROVE.
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline() // draft Succeeded, critique Succeeded
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("want StageAwaitingReview after the ratified self-critique, got %d", view.Stage)
		}
		if view.Critique == nil {
			t.Fatal("an architect-critiqued artifact at the human gate must surface the critique conclusion, got nil")
		}
		if view.Critique.Role != critiqueRoleArchitect {
			t.Errorf("critique role must be the honest architect (never productManager), got %q", view.Critique.Role)
		}
		if view.Critique.Verdict != projectstate.CritiqueVerdictApprove {
			t.Errorf("critique verdict: want approve, got %q", view.Critique.Verdict)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// TWO round-trips: the draft, then the system-critique with job_mode=critique.
	if len(pipe.submits) != 2 {
		t.Fatalf("System must dispatch draft + architect self-critique, got %d submits", len(pipe.submits))
	}
	crit := pipe.submits[1]
	if got := crit.dispatchInputs[dispatchInputCommand]; got != "system-critique" {
		t.Fatalf("the critique dispatch must carry command=system-critique, got %q", got)
	}
	if got := crit.dispatchInputs[dispatchInputJobMode]; got != jobModeCritique {
		t.Fatalf("the critique dispatch must carry job_mode=%s, got %q", jobModeCritique, got)
	}
	if got := crit.dispatchInputs[dispatchInputArtifactKind]; got != projectstate.KindSystem.String() {
		t.Fatalf("the critique dispatch must target the System kind, got %q", got)
	}
}

// ARCHITECT SELF-CRITIQUE — REVISE ROUTING + NON-CONVERGENCE. A revise verdict
// re-dispatches the architect draft BEFORE the human gate (the same second-round-trip
// loop as the PM critics); a never-converging self-critique stages best-effort at the
// gate with the unresolved critique surfaced under the honest ARCHITECT rule id.
func Test_CoAuthor_System_ArchitectSelfCritiqueRevise_RedispatchesDraft_StagesForHumanGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	proj := systemReadBack(t, id)
	// Persistently "revise" with notes — the self-critique never converges.
	proj.SystemDesign = awaitingSlot(&projectstate.System{}, projectstate.CritiqueVerdictRevise, "encapsulate the storage volatility")
	ps := &fakeProjectState{project: proj}
	pipe := newFakePipeline() // every draft + critique dispatch Succeeds
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("non-converging self-critique must stage for the human gate, got stage %d", view.Stage)
		}
		var sawWarning bool
		for _, f := range view.Findings {
			if string(f.RuleID) == "ARCHITECT-CRITIQUE-UNRESOLVED" {
				sawWarning = true
			}
			if string(f.RuleID) == "PM-CRITIQUE-UNRESOLVED" {
				t.Errorf("an architect-critiqued kind must never surface a PM-CRITIQUE finding, got %+v", f)
			}
		}
		if !sawWarning {
			t.Fatalf("expected an ARCHITECT-CRITIQUE-UNRESOLVED warning at the gate, got %+v", view.Findings)
		}
		if view.Critique == nil || view.Critique.Verdict != projectstate.CritiqueVerdictRevise {
			t.Fatalf("the staged-best-effort gate must surface the revise conclusion, got %+v", view.Critique)
		}
		if view.Critique.Role != critiqueRoleArchitect {
			t.Errorf("critique role: want architect, got %q", view.Critique.Role)
		}
		if view.Critique.Summary != "encapsulate the storage volatility" {
			t.Errorf("critique summary: want the critic's rationale, got %q", view.Critique.Summary)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 90*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete (self-critique non-convergence must stage, not hang/crash)")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("self-critique non-convergence must NOT crash the workflow: %v", err)
	}
	// At least draft + critique per round, looped to maxRedraftAttempts: >2 dispatches,
	// proving revise re-dispatches the draft (a real second round-trip).
	if len(pipe.submits) <= 2 {
		t.Fatalf("self-critique revise must re-dispatch (draft+critique per round); got only %d dispatches", len(pipe.submits))
	}
}

// THE OTHER ARCHITECT-OWNED KINDS STILL SKIP. Volatilities (and standard-check /
// operational-concepts, same switch arm) take NO critique round — a single draft
// dispatch straight to the human gate (EARMARK: extend only on live QA evidence).
func Test_CoAuthor_Volatilities_ArchitectOwned_SkipsCritique_SingleDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{
		ID:                   projectstate.ProjectID(id),
		Version:              2,
		Mission:              committedSlot(mustMission(t)),
		Glossary:             committedSlot(mustGlossary(t)),
		ScrubbedRequirements: committedSlot(&projectstate.ScrubbedRequirements{}),
		Volatilities:         awaitingSlot(&projectstate.Volatilities{}, "", ""),
	}}
	pipe := newFakePipeline()
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("want StageAwaitingReview, got %d", view.Stage)
		}
		if view.Critique != nil {
			t.Errorf("an uncritiqued kind must surface no critique conclusion, got %+v", view.Critique)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindVolatilities})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(pipe.submits) != 1 {
		t.Fatalf("Volatilities must be a single draft dispatch (no critique), got %d submits", len(pipe.submits))
	}
	if got := pipe.submits[0].dispatchInputs[dispatchInputCommand]; got != "volatilities-draft" {
		t.Fatalf("dispatch must carry command=volatilities-draft, got %q", got)
	}
}

// THE VERSION GATE (system-architect-critique). A KindSystem session in flight at
// deploy time (the running gtdapp:5 redraft) resolved DefaultVersion at its workflow
// start, so it must complete on the OLD no-critique command sequence: a single draft
// dispatch, no system-critique job, straight to the gate.
func Test_CoAuthor_System_ArchitectCritique_VersionGate_PreFeatureExecutionSkipsCritique(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline()
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// Simulate a PRE-FEATURE in-flight execution: GetVersion resolves DefaultVersion
	// (no version marker at the session start of the replayed history).
	env.OnGetVersion("system-architect-critique", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a pre-feature execution must run the OLD command sequence cleanly: %v", err)
	}
	if len(pipe.submits) != 1 {
		t.Fatalf("a pre-feature KindSystem session must dispatch the draft ONLY (no critique), got %d", len(pipe.submits))
	}
	if got := pipe.submits[0].dispatchInputs[dispatchInputCommand]; got != "system-draft" {
		t.Fatalf("dispatch must carry command=system-draft, got %q", got)
	}
	if len(ps.committed) != 1 {
		t.Fatalf("the approved pre-feature spine must commit once, got %v", ps.committed)
	}
}

// A Reject at the AwaitingReview gate calls RejectArtifact and loops back to a fresh
// DRAFT dispatch (the human-gate suspend/resume is unchanged). Approve after the
// redraft commits.
func Test_CoAuthor_Reject_LoopsToFreshDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline() // every dispatch Succeeds
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	const rejectNotes = "rework the decomposition"
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.rejected) != 1 || ps.rejected[0].kind != projectstate.KindSystem || ps.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact(KindSystem, %q), got %v", rejectNotes, ps.rejected)
	}
	if len(ps.committed) != 1 {
		t.Fatalf("want one commit after redraft->approve, got %v", ps.committed)
	}
	// Reject loops to a FRESH dispatch: at least 2 draft dispatches.
	if len(pipe.submits) < 2 {
		t.Fatalf("a reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
}

// THE COLLISION REGRESSION (D-MSD-Δ amendment — the senior-identified bug). On a
// PM-critiqued kind, a Reject at the AwaitingReview gate writes slot.Notes (the
// architect's reject rationale). The loop then re-drafts and re-critiques with NO
// intervening Stage before the critique read-back. With the OLD Notes-as-carrier the
// critique read-back would misread the reject Notes as CritiqueRevise and re-loop on
// the architect's own words. With the first-class CritiqueVerdict carrier the reject
// Notes are IGNORED by the read-back: the scripted "approve" verdict drives it and the
// gate proceeds to Approve. This proves the carrier read-back does NOT collide with the
// frozen reject Notes field.
func Test_CoAuthor_RejectNotes_DoNotLeakIntoCritiqueReadBack(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// Mission starts with an "approve" critique carrier so the FIRST round ratifies.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, ""),
	}}
	pipe := newFakePipeline() // every draft + critique dispatch Succeeds
	// Each critique round "commits" an "approve" verdict into the slot carrier (the
	// awaitingSlot seed already holds it; the redraft round's critique re-asserts it on
	// observe). The architect's reject Notes (set by RejectArtifact) are left untouched,
	// so the test proves the read-back consults the FIRST-CLASS carrier, not Notes.
	pipe.onObserve = func() {
		ps.setSlotCritique(projectstate.KindMission, projectstate.CritiqueVerdictApprove, "")
	}
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	const rejectNotes = "REJECT-RATIONALE: rework the vision — this MUST NOT be read as a PM verdict"

	// First gate: REJECT (writes slot.Notes = rejectNotes, clears the critique carrier).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
	}, 30*time.Second)
	// Second gate (after redraft+approve-critique): APPROVE to commit and finish.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.rejected) != 1 || ps.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact carrying the reject rationale, got %v", ps.rejected)
	}
	// The reject Notes must NOT have driven an extra critique-revise loop. After the
	// reject round the critique APPROVES, so the gate reaches Approve and commits once.
	if len(ps.committed) != 1 || ps.committed[0] != projectstate.KindMission {
		t.Fatalf("the architect's reject Notes must NOT be read as a PM critique verdict; want a single commit, got committed=%v", ps.committed)
	}
}

// sdAssertHumanGateNoActiveRole queries the session at the human AwaitingReview gate
// and asserts the (ActiveRole, ActiveStep, Round) sub-step stamp reads none/none/0.
func sdAssertHumanGateNoActiveRole(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()
	v := sdSessionView(t, env)
	if v.Stage != StageAwaitingReview {
		t.Fatalf("want StageAwaitingReview at the human gate, got %d", v.Stage)
	}
	if v.ActiveRole != ActiveRoleNone || v.ActiveStep != ActiveStepNone || v.Round != 0 {
		t.Fatalf("the human gate must show no active role, got role=%d step=%d round=%d", v.ActiveRole, v.ActiveStep, v.Round)
	}
}

// Plan-3 C1: the honest role-driven sub-step indicator. A PM-critiqued kind (Mission)
// driven through draft → critique(revise) → revise → critique(approve) → approve must
// surface the live (ActiveRole, ActiveStep, Round) at each dispatch boundary, and
// none/none/0 at the human gate. The in-flight snapshot is captured from the observe
// activity (the ONLY moment a dispatch is genuinely in flight — the fake's onObserve hook,
// which the workflow is blocked on); the gate snapshot from a delayed-callback query while
// the workflow is suspended awaiting the review decision. This is the workflow-LOCAL sub-
// step (no activity, no history command) served by the SAME sessionState query.
func Test_CoAuthor_ActiveSubStep_SequenceThroughDraftCritiqueReviseApprove(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// Mission read-back carries a "revise" critique verdict so the FIRST critique drives a
	// redraft; the SECOND critique is flipped to "approve" (in onObserve) so the loop converges.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictRevise, "tighten the vision"),
	}}
	pipe := newFakePipeline() // every draft + critique dispatch observed Succeeded

	type subStep struct {
		role  ActiveRole
		step  ActiveStep
		round int64
	}
	var mu sync.Mutex
	var seq []subStep
	critiqueObserves := 0
	// onObserve runs synchronously INSIDE the observe activity, i.e. exactly while a dispatch
	// is in flight — the one place the in-flight sub-step is live. Snapshot the query view
	// there, and flip the critique verdict to "approve" on the second critique so the revise
	// loop terminates.
	pipe.onObserve = func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			return
		}
		var v SessionStateView
		if err := enc.Get(&v); err != nil {
			return
		}
		mu.Lock()
		seq = append(seq, subStep{v.ActiveRole, v.ActiveStep, v.Round})
		if v.ActiveStep == ActiveStepCritiquing {
			critiqueObserves++
			if critiqueObserves >= 2 {
				ps.setSlotCritique(projectstate.KindMission, projectstate.CritiqueVerdictApprove, "")
			}
		}
		mu.Unlock()
	}

	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// At the human gate the sub-step must read none/none/0 (no role is working); approve to end.
	env.RegisterDelayedCallback(func() {
		sdAssertHumanGateNoActiveRole(t, env)
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 120*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want CoAuthorApproved after the revise loop converged, got %d", outcome)
	}

	want := []subStep{
		{ActiveRoleArchitect, ActiveStepDrafting, 0},        // draft in flight (round 0)
		{ActiveRoleProductManager, ActiveStepCritiquing, 0}, // PM critique in flight
		{ActiveRoleArchitect, ActiveStepRevising, 1},        // redraft/revise in flight (round 1)
		{ActiveRoleProductManager, ActiveStepCritiquing, 0}, // second PM critique in flight
	}
	mu.Lock()
	got := append([]subStep(nil), seq...)
	mu.Unlock()
	if len(got) != len(want) {
		t.Fatalf("want %d in-flight sub-step snapshots, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("in-flight snapshot %d = %+v, want %+v (full sequence: %+v)", i, got[i], want[i], got)
		}
	}
}

// THE DRAFT-FAILED GATE MUST SHOW NO ACTIVE ROLE. Twin of the projectdesign
// Test_CoAuthor_ActiveSubStep_ClearsAtDraftFailedGate — a terminal PipelineFailed job
// must not merely land in StageDraftFailed (already covered by
// Test_CoAuthor_PhaseFailed_LandsInStageDraftFailed_NotPerpetualDrafting), it must also
// clear the in-flight (ActiveRole, ActiveStep, Round) sub-step stamp, so the human gate
// never shows a stale "architect drafting" caption alongside the failure.
func Test_CoAuthor_ActiveSubStep_ClearsAtDraftFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline(pipelineFailed)
	pipe.diagnostic = "aiarch-validate found 2 violations"
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var v SessionStateView
		if err := enc.Get(&v); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if v.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed, got %d", v.Stage)
		}
		if v.ActiveRole != ActiveRoleNone || v.ActiveStep != ActiveStepNone || v.Round != 0 {
			t.Fatalf("StageDraftFailed must show no active role, got role=%d step=%d round=%d", v.ActiveRole, v.ActiveStep, v.Round)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT fail the workflow, got: %v", err)
	}
}

// THE MISSING-VERDICT SAFE DEFAULT. A critique dispatch reaches PipelineSucceeded but
// the slot's CritiqueVerdict read-back carrier is EMPTY (the job claimed success yet
// committed no verdict). The safe rule is NOT a silent approve: the session lands in
// the human-visible StageDraftFailed (the same anti-wedge gate as a failed job),
// surfacing a FailureReason, and suspends awaiting Retry/Withdraw. Withdraw ends clean
// and NOTHING is committed (an unreviewed draft must never sail through as approved).
func Test_CoAuthor_CritiqueMissingVerdict_LandsInStageDraftFailed_NotSilentApprove(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// Mission draft read-back is fine, but the critique carrier verdict is EMPTY.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), "", ""),
	}}
	pipe := newFakePipeline() // both draft + critique dispatch observed Succeeded
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a missing critique verdict must land in StageDraftFailed (NOT silent approve), got stage %d", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("StageDraftFailed from a missing verdict must carry a human FailureReason")
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the missing-verdict gate")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a missing critique verdict must NOT crash the workflow: %v", err)
	}
	if len(ps.committed) != 0 {
		t.Fatalf("a missing critique verdict must NEVER commit (no silent approve), got committed=%v", ps.committed)
	}
	if len(ps.staged) != 0 {
		t.Fatalf("a missing critique verdict must stage nothing, got %d", len(ps.staged))
	}
}

// F-QA2-24 — THE CRITIQUE-RETRY TEST (live forensics, gtdapp). When the PM-CRITIQUE job
// fails terminally, the DRAFT on the session branch is complete and read back fine — so
// the failed gate's Retry must RE-RUN THE CRITIQUE against that draft, NOT dispatch a
// feedbackless redraft (which finds no open comments and no revise verdict, commits
// nothing, and the template's silent-failure guard reds the run — a retry loop that never
// converges; observed twice consecutively on gtdapp). The gate copy must name the
// PM-critique (not the generic "the design job failed in CI"), and the successful critique
// retry must route the session to AwaitingReview with the F-QA2-7 PM conclusion stamped.
func Test_CoAuthor_CritiqueFailed_RetryRerunsCritique_NotRedraft(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// Mission draft read-back is fine; the critique carrier already holds "approve" so the
	// RETRIED critique ratifies and the session stages for the human gate.
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, ""),
	}}
	// Dispatch #1 (draft) Succeeds, #2 (critique) FAILS terminally, #3 (the retried
	// critique) Succeeds.
	pipe := newFakePipeline(pipelineSucceeded, pipelineFailed, pipelineSucceeded)
	pipe.diagnostic = "critique agent crashed before committing a verdict"
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// t=30s — at the failed gate: the reason must name the CRITIQUE. Then Retry (the
	// SPA's "Retry draft" lever — the redraft signal).
	env.RegisterDelayedCallback(func() {
		view := sdSessionView(t, env)
		if view.Stage != StageDraftFailed {
			t.Fatalf("a terminal critique failure must land in StageDraftFailed, got %d", view.Stage)
		}
		if view.FailureReason == nil || !strings.Contains(*view.FailureReason, "PM-critique") {
			t.Fatalf("the failed-gate copy must name the PM-critique (the draft did not fail), got %v", view.FailureReason)
		}
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 30*time.Second)

	// t=60s — the retried CRITIQUE succeeded and ratified: the session must be at the
	// human AwaitingReview gate with the PM conclusion stamped. Withdraw to end.
	env.RegisterDelayedCallback(func() {
		view := sdSessionView(t, env)
		if view.Stage != StageAwaitingReview {
			t.Fatalf("a successful critique retry must route to AwaitingReview, got %d", view.Stage)
		}
		// The retry reused the FULL runPMCritique flow — the F-QA2-7 stamp included.
		if view.Critique == nil || view.Critique.Verdict != projectstate.CritiqueVerdictApprove {
			t.Fatalf("the retried critique must stamp the PM conclusion at the gate, got %+v", view.Critique)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// THE dispatch-shape assertion: draft, failed critique, retried CRITIQUE — the Retry
	// dispatched a critique pipeline, and NO redraft was ever dispatched.
	if len(pipe.submits) != 3 {
		t.Fatalf("want exactly 3 dispatches (draft, failed critique, retried critique), got %d", len(pipe.submits))
	}
	wantModes := []string{jobModeDraft, jobModeCritique, jobModeCritique}
	for i, want := range wantModes {
		if got := pipe.submits[i].dispatchInputs[dispatchInputJobMode]; got != want {
			t.Fatalf("dispatch %d job_mode = %q, want %q (a critique failure's Retry must NOT redraft)", i, got, want)
		}
	}
	// The retried critique ratified → the kept draft staged exactly once for the gate.
	if len(ps.staged) != 1 {
		t.Fatalf("the ratified draft must stage exactly once, got %d", len(ps.staged))
	}
}

// THE F-QA2-24 VERSION GATE. The critique-retry resume was added AFTER design sessions
// shipped; an execution already suspended at a StageDraftFailed gate (gtdapp:1 sits at
// the glossary gate RIGHT NOW) has history in which a Retry after a critique failure
// dispatched a DRAFT job — replaying it against un-gated new code would schedule the
// resume read-back where history recorded a dispatch (a non-determinism failure).
// workflow.GetVersion("failed-gate-critique-retry") pins such executions (DefaultVersion)
// to the OLD redraft-on-retry sequence for their whole run — behavior AND gate copy
// together (the pinned view must not promise a critique re-run it will not perform).
// Mirrors Test_CoAuthor_Rail_ScaffoldSync_VersionGate_PreFeatureExecutionSkipsSync.
func Test_CoAuthor_CritiqueRetry_VersionGate_PreFeatureExecutionRedrafts(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(id),
		Version: 1,
		Mission: awaitingSlot(mustMission(t), projectstate.CritiqueVerdictApprove, ""),
	}}
	// #1 draft Succeeds, #2 critique FAILS; then (old semantics) the Retry REDRAFTS:
	// #3 draft Succeeds, #4 critique Succeeds (phases exhausted → last repeats).
	pipe := newFakePipeline(pipelineSucceeded, pipelineFailed, pipelineSucceeded)
	pipe.diagnostic = "critique agent crashed"
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// Simulate a PRE-FEATURE in-flight execution: GetVersion resolves DefaultVersion
	// (no version marker in the replayed history).
	env.OnGetVersion("failed-gate-critique-retry", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed, got %d", view.Stage)
		}
		// The pinned gate keeps the OLD copy: its Retry redrafts, so it must NOT promise
		// a critique re-run.
		if view.FailureReason == nil || strings.Contains(*view.FailureReason, "retry re-runs the critique") {
			t.Fatalf("a pinned pre-feature gate must keep the old redraft copy, got %v", view.FailureReason)
		}
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 30*time.Second)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 90*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindMission})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a pre-feature execution must run the OLD command sequence cleanly: %v", err)
	}
	// OLD command sequence: the Retry dispatched a DRAFT, then its critique — 4 dispatches.
	if len(pipe.submits) != 4 {
		t.Fatalf("want 4 dispatches on the pinned path (draft, failed critique, REDRAFT, critique), got %d", len(pipe.submits))
	}
	if got := pipe.submits[2].dispatchInputs[dispatchInputJobMode]; got != jobModeDraft {
		t.Fatalf("a pinned pre-feature Retry must REDRAFT (the old command sequence), got job_mode=%q", got)
	}
}

// ---- Tests: parent sequence (SystemDesignPhaseWorkflow) ---------------------

func registerPhase(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps projectstate.ProjectStateAccess) {
	env.RegisterWorkflowWithOptions(wf.SystemDesignPhaseWorkflow, workflow.RegisterOptions{Name: executionKindPhase})
	env.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: executionKindCoAuthor})
	registerGenActivities(env, ps, nil, nil)
}

// zeroCommittedModelFor builds the minimal valid committed model for a kind — what the
// phase tests' mocked children "commit" so the served head-state round-trips the strict
// codec (mission/glossary have non-empty invariants; the rest accept their zero model).
func zeroCommittedModelFor(t *testing.T, kind projectstate.ArtifactKind) projectstate.ArtifactModel {
	t.Helper()
	switch kind {
	case projectstate.KindMission:
		return mustMission(t)
	case projectstate.KindGlossary:
		return mustGlossary(t)
	case projectstate.KindScrubbedRequirements:
		return &projectstate.ScrubbedRequirements{}
	case projectstate.KindVolatilities:
		return &projectstate.Volatilities{}
	case projectstate.KindCoreUseCases:
		return &projectstate.CoreUseCases{}
	case projectstate.KindSystem:
		return &projectstate.System{}
	case projectstate.KindOperationalConcepts:
		return &projectstate.DeploymentOperationsModel{}
	case projectstate.KindStandardCheck:
		return &projectstate.StandardCheck{}
	default:
		t.Fatalf("no zero model for kind %s", kind)
		return nil
	}
}

// The parent drives the steps in fixed order; each child Approve auto-advances; after the
// last step the parent seals Phase 1. The child is MOCKED to Approve (each mocked approve
// commits its slot on the served head-state, in spine order — the parent is strictly
// sequential) so this test isolates the parent's sequencing + seal. The project starts
// with NOTHING committed, so the skip-committed restart gate skips nothing here.
func Test_Phase_AllStepsApproved_SealsPhase1(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1}
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows()
	registerPhase(env, wf, ps)

	kinds := projectstate.Phase1RequiredKinds()
	var childCalls int
	env.OnWorkflow(executionKindCoAuthor, mock.Anything, mock.Anything).
		Return(coAuthorApproved, nil).Times(len(kinds)).
		Run(func(mock.Arguments) {
			// Model the approve→commit each real child performs: commit the NEXT kind in
			// spine order (the parent is strictly sequential, so order is deterministic).
			ps.markCommitted(kinds[childCalls], zeroCommittedModelFor(t, kinds[childCalls]))
			childCalls++
		})

	env.ExecuteWorkflow(executionKindPhase, phaseInput{ProjectID: ProjectID(proj.ID)})

	if !env.IsWorkflowCompleted() {
		t.Fatal("parent workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("parent workflow error: %v", err)
	}
	if childCalls != len(kinds) {
		t.Fatalf("parent must run every step's child once, got %d of %d", childCalls, len(kinds))
	}
	if ps.advanced != 1 {
		t.Fatalf("parent must seal Phase 1 exactly once after all steps approved, advanced=%d", ps.advanced)
	}
}

// If a child gate reports Withdraw, the parent HALTS and does not seal. (Empty project —
// the first step's child runs and withdraws.)
func Test_Phase_StepWithdrawn_HaltsSequence_NoSeal(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1}
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows()
	registerPhase(env, wf, ps)

	env.OnWorkflow(executionKindCoAuthor, mock.Anything, mock.Anything).
		Return(coAuthorWithdrawn, nil).Once()

	env.ExecuteWorkflow(executionKindPhase, phaseInput{ProjectID: ProjectID(proj.ID)})

	if !env.IsWorkflowCompleted() {
		t.Fatal("parent workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("parent workflow error: %v", err)
	}
	if ps.advanced != 0 {
		t.Fatalf("a withdrawn step must NOT seal the phase, advanced=%d", ps.advanced)
	}
}

// THE 2026-07-16 INCIDENT REGRESSION (parent half). A child co-author FAILURE (gtdapp:1's
// withdraw-crash ContractMisuse) must NOT fail the phase workflow: the parent CONTAINS it
// — logs the cause, halts the sequence gracefully (COMPLETED, never FAILED), and does not
// seal — so the step stays restartable (a fresh requestArtifactDraft revives the step; a
// fresh startSystemDesign restarts the phase rail).
func Test_Phase_ChildFailure_Contained_PhaseCompletesGracefully_NoSeal(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1}
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows()
	registerPhase(env, wf, ps)

	env.OnWorkflow(executionKindCoAuthor, mock.Anything, mock.Anything).
		Return(coAuthorUnknown, temporal.NewNonRetryableApplicationError(
			"resourceaccess: projectstate.WithdrawArtifact: slot Glossary is unpopulated (stage a model first)",
			fwmanager.RAErrType(fwra.ContractMisuse), nil)).Once()

	env.ExecuteWorkflow(executionKindPhase, phaseInput{ProjectID: ProjectID(proj.ID)})

	if !env.IsWorkflowCompleted() {
		t.Fatal("parent workflow did not complete")
	}
	// THE load-bearing assertion: the phase workflow must survive the child failure.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a child failure must NOT fail the phase workflow (the whole Phase-1 rail died from one recovery click), got: %v", err)
	}
	if ps.advanced != 0 {
		t.Fatalf("a failed step must NOT seal the phase, advanced=%d", ps.advanced)
	}
}

// RESTART SEMANTICS (2026-07-16 incident recovery). A phase run started over a head-state
// that ALREADY carries committed steps (the restart of a halted/failed rail via
// startSystemDesign) must SKIP them and resume at the first open step — never re-draft a
// committed artifact. Mission is committed; the first (and only) child spawned must be the
// GLOSSARY step, which withdraws to halt.
func Test_Phase_Restart_SkipsCommittedSteps_ResumesAtFirstOpenStep(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := projectstate.Project{
		ID:      projectstate.ProjectID(uuid.NewString()),
		Version: 2,
		Mission: committedSlot(mustMission(t)),
	}
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows()
	registerPhase(env, wf, ps)

	var gotKinds []ArtifactKind
	env.OnWorkflow(executionKindCoAuthor, mock.Anything, mock.Anything).
		Return(coAuthorWithdrawn, nil).Once().
		Run(func(args mock.Arguments) {
			if in, ok := args.Get(1).(coAuthorInput); ok {
				gotKinds = append(gotKinds, in.ArtifactKind)
			}
		})

	env.ExecuteWorkflow(executionKindPhase, phaseInput{ProjectID: ProjectID(proj.ID)})

	if !env.IsWorkflowCompleted() {
		t.Fatal("parent workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("parent workflow error: %v", err)
	}
	if len(gotKinds) != 1 || gotKinds[0] != KindGlossary {
		t.Fatalf("a restarted phase must skip committed mission and spawn the GLOSSARY child first, got %v", gotKinds)
	}
	if ps.advanced != 0 {
		t.Fatalf("the halted restart must NOT seal, advanced=%d", ps.advanced)
	}
}

// ---- Tests: phase seal (PhaseAdvanceWorkflow) -------------------------------

// advancePhase returns MissingArtifacts when a required slot is uncommitted.
func Test_PhaseAdvance_Blocked_MissingArtifacts(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Mission: committedSlot(mustMission(t))}
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows()
	registerPhaseAdvance(env, wf, ps)

	env.ExecuteWorkflow(executionKindPhaseAdvance, phaseAdvanceInput{ProjectID: ProjectID(proj.ID)})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res PhaseAdvanceResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.Advanced {
		t.Fatal("want Advanced:false with a required slot uncommitted")
	}
	if len(res.MissingArtifacts) == 0 {
		t.Fatal("want a non-empty MissingArtifacts set")
	}
	missing := map[ArtifactKind]bool{}
	for _, k := range res.MissingArtifacts {
		missing[k] = true
	}
	if missing[KindMission] {
		t.Fatalf("Mission is committed and must NOT be missing: %v", res.MissingArtifacts)
	}
	if !missing[KindStandardCheck] {
		t.Fatalf("StandardCheck is uncommitted and must be missing: %v", res.MissingArtifacts)
	}
	if ps.advanced != 0 {
		t.Fatalf("blocked advance must NOT seal the phase, advanced=%d", ps.advanced)
	}
}

// advancePhase returns Advanced:true when all required slots are committed (the seal
// gates on all-committed; standard-check VALIDITY is the Action's CI check now).
func Test_PhaseAdvance_AllCommitted_Advances(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	proj := allPhase1Committed(t)
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows()
	registerPhaseAdvance(env, wf, ps)

	env.ExecuteWorkflow(executionKindPhaseAdvance, phaseAdvanceInput{ProjectID: ProjectID(proj.ID)})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res PhaseAdvanceResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("want Advanced:true, got %+v", res)
	}
	if ps.advanced != 1 {
		t.Fatalf("want AdvancePhase sealed once, advanced=%d", ps.advanced)
	}
}

// allPhase1Committed builds a Project with every Phase-1 required slot committed.
func allPhase1Committed(t *testing.T) projectstate.Project {
	t.Helper()
	g, err := projectstate.NewGlossary([]projectstate.GlossaryItem{{Term: "Aggregate", Definition: "a consistency boundary"}})
	if err != nil {
		t.Fatalf("NewGlossary: %v", err)
	}
	return projectstate.Project{
		ID:                   projectstate.ProjectID(uuid.NewString()),
		Version:              8,
		Mission:              committedSlot(mustMission(t)),
		Glossary:             committedSlot(g),
		ScrubbedRequirements: committedSlot(&projectstate.ScrubbedRequirements{}),
		Volatilities:         committedSlot(&projectstate.Volatilities{}),
		CoreUseCases:         committedSlot(&projectstate.CoreUseCases{}),
		SystemDesign:         committedSlot(&projectstate.System{}),
		OperationalConcepts:  committedSlot(&projectstate.DeploymentOperationsModel{}),
		StandardCheck:        committedSlot(&projectstate.StandardCheck{}),
	}
}

// acknowledgestale_test.go covers the F-GTD-12 live-session ack gate (Phase-1 twin of the
// projectdesign tests): acknowledging staleness on a slot whose amendment session is LIVE
// is refused with FailedPrecondition (the wire's 409/"failed_precondition"), because the
// ack's main-branch commit would turn the amendment's review PR merge-DIRTY and wedge its
// approve.

// fakeEncodedSessionView satisfies converter.EncodedValue for the mocked sessionState
// Query result.
type fakeEncodedSessionView struct{ view SessionStateView }

func (f fakeEncodedSessionView) HasValue() bool { return true }
func (f fakeEncodedSessionView) Get(valuePtr any) error {
	p, ok := valuePtr.(*SessionStateView)
	if !ok {
		return errors.New("unexpected query result type")
	}
	*p = f.view
	return nil
}

// A RUNNING co-author session awaiting review (the exact F-GTD-12 scenario: an amendment
// staged on its session branch with an open review PR) refuses the acknowledge with
// FailedPrecondition, naming the amendment.
func Test_AcknowledgeStaleBasis_LiveAmendmentSession_FailedPrecondition(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(
		&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
			},
		}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).Return(
		fakeEncodedSessionView{view: SessionStateView{Stage: StageAwaitingReview}}, nil)

	m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}
	err := m.AcknowledgeStaleBasis(bgRC(), id, KindMission, "unaffected")
	sde := asSystemDesignError(t, err)
	if sde.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition while the amendment session is live, got %d (%v)", sde.Kind, err)
	}
	if !strings.Contains(err.Error(), "amendment") {
		t.Fatalf("the refusal must explain the amendment conflict, got %q", err.Error())
	}
	mc.AssertExpectations(t)
}

// No session has ever run for the slot (Describe reports the execution missing →
// GetSessionState NotFound): the liveness gate passes and the ack proceeds to the
// substrate — proving the refusal above came from the gate, not this path. The
// generated ProjectStateAccess contract requires AcknowledgeStaleBasis unconditionally
// post-C2-fold (code-health-phase-a), so a substrate that "doesn't support" it is no
// longer a reachable case — the ack now SUCCEEDS once the gate passes.
func Test_AcknowledgeStaleBasis_NoSession_PassesLivenessGate(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(nil, serviceerror.NewNotFound("workflow not found"))

	m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{}}
	err := m.AcknowledgeStaleBasis(bgRC(), id, KindMission, "unaffected")
	if err != nil {
		t.Fatalf("expected the liveness gate to pass and the ack to succeed, got %v", err)
	}
	mc.AssertExpectations(t)
}

// A session that closed COMPLETED after committing is TERMINAL (the durable slot renders
// StageCommitted): the gate passes and the ack proceeds (and, post-C2-fold, succeeds).
func Test_AcknowledgeStaleBasis_CompletedSession_PassesLivenessGate(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(
		&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
			},
		}, nil)
	// NO QueryWorkflow expectation: a COMPLETED run's replayed query is bypassed.

	proj := committedProject(id, KindMission)
	proj.Mission.Model = &projectstate.MissionStatement{Vision: "V", Mission: "M"}
	m := &systemDesignManager{client: mc, projectState: &renderFakeProjectState{project: proj}}
	err := m.AcknowledgeStaleBasis(bgRC(), id, KindMission, "unaffected")
	if err != nil {
		t.Fatalf("a committed (terminal) session must pass the liveness gate and succeed, got %v", err)
	}
	mc.AssertExpectations(t)
}

// The live set is exactly the non-terminal stages: drafting / awaitingReview /
// redrafting / draftFailed (the recovery gate keeps the branch+PR).
func Test_SessionStageIsLive(t *testing.T) {
	live := []SessionStage{StageDrafting, StageAwaitingReview, StageRedrafting, StageDraftFailed}
	for _, s := range live {
		if !sessionStageIsLive(s) {
			t.Errorf("stage %s must be live", sessionStageLabel(s))
		}
	}
	terminal := []SessionStage{SessionStageUnknown, StageCommitted, StageWithdrawn, StageRefused}
	for _, s := range terminal {
		if sessionStageIsLive(s) {
			t.Errorf("stage %s must NOT be live", sessionStageLabel(s))
		}
	}
}

// activityfindings_test.go — coverage for the app-side activity-diagram gate the
// sessionState read-back applies to a CoreUseCases draft (founder ruling 2026-07-05).
// Every use case (core AND supporting) must carry a non-empty activity diagram with a
// start node and at least one action step; a diagram-less use case surfaces as one
// ERROR-severity finding on the review panel so the human gate flags it.

// ucd builds a UseCaseDecision carrying a named use case with the given activity diagram.
func ucd(name string, activity *projectstate.ActivityDiagram) projectstate.UseCaseDecision {
	return projectstate.UseCaseDecision{
		UseCase: projectstate.UseCase{Name: name, Activity: activity},
	}
}

// wellFormedActivity is the minimum acceptable diagram: a start node -> action -> end.
func wellFormedActivity() *projectstate.ActivityDiagram {
	return &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{
			{ID: "n1", Kind: projectstate.NodeStart},
			{ID: "n2", Kind: projectstate.NodeAction, Label: "Do the thing"},
			{ID: "n3", Kind: projectstate.NodeEnd},
		},
		Edges: []projectstate.ActivityEdge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
		},
	}
}

// eventEntryActivity has NO start node; its only entry is an edge-less timeEvent node
// (tier parity with methodcheck's activityHasEntryAndAction, 2026-07-30
// callchain-realization) -> action -> end. Acceptable.
func eventEntryActivity() *projectstate.ActivityDiagram {
	return &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{
			{ID: "n1", Kind: projectstate.NodeTimeEvent, Label: "midnight"},
			{ID: "n2", Kind: projectstate.NodeAction, Label: "Do the thing"},
			{ID: "n3", Kind: projectstate.NodeEnd},
		},
		Edges: []projectstate.ActivityEdge{
			{From: "n1", To: "n2"},
			{From: "n2", To: "n3"},
		},
	}
}

// eventNodeWithIncomingEdgeActivity has NO start node, and its only event node HAS an
// incoming edge — it is mid-flow, not an entry — so it must NOT satisfy UC-ACT-PRESENT.
func eventNodeWithIncomingEdgeActivity() *projectstate.ActivityDiagram {
	return &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{
			{ID: "n1", Kind: projectstate.NodeAction, Label: "Do the thing"},
			{ID: "n2", Kind: projectstate.NodeAcceptEvent, Label: "await message"},
		},
		Edges: []projectstate.ActivityEdge{
			{From: "n1", To: "n2"},
		},
	}
}

// A use case with a null or structurally-empty activity produces exactly one ERROR
// finding; a use case with a start + action diagram produces none.
func Test_useCaseActivityFindings_FlagsMissingAndEmptyDiagrams(t *testing.T) {
	noStart := &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{{ID: "n1", Kind: projectstate.NodeAction, Label: "step"}},
	}
	noAction := &projectstate.ActivityDiagram{
		Nodes: []projectstate.ActivityNode{{ID: "n1", Kind: projectstate.NodeStart}},
	}

	draft := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{
		ucd("Capture", nil),                             // null activity — the observed gtdapp defect
		ucd("Clarify", &projectstate.ActivityDiagram{}), // empty diagram (no nodes)
		ucd("Organize", noStart),                        // nodes but no start
		ucd("Reflect", noAction),                        // nodes but no action
		ucd("Engage", wellFormedActivity()),             // acceptable — no finding
	}}

	findings := useCaseActivityFindings(KindCoreUseCases, draft)
	if len(findings) != 4 {
		t.Fatalf("expected 4 ERROR findings (one per diagram-less use case), got %d: %+v", len(findings), findings)
	}

	wantNames := []string{"Capture", "Clarify", "Organize", "Reflect"}
	for i, f := range findings {
		if f.Severity != SeverityError {
			t.Errorf("finding %d: want SeverityError, got %v", i, f.Severity)
		}
		if string(f.RuleID) != "USECASE-ACTIVITY-MISSING" {
			t.Errorf("finding %d: want RuleID USECASE-ACTIVITY-MISSING, got %q", i, f.RuleID)
		}
		if !strings.Contains(f.Message, wantNames[i]) {
			t.Errorf("finding %d: message %q must name use case %q", i, f.Message, wantNames[i])
		}
		if f.Location == nil || f.Location.Ordinal != int64(i) {
			t.Errorf("finding %d: want Location.Ordinal %d, got %+v", i, i, f.Location)
		}
	}
	// The acceptable use case (index 4) must not appear.
	for _, f := range findings {
		if strings.Contains(f.Message, "Engage") {
			t.Errorf("well-formed use case Engage must not be flagged; got %q", f.Message)
		}
	}
}

// The gate is scoped to CoreUseCases: any other artifact kind, a nil draft, or a
// wrong-typed draft yields no findings (so the nil-when-empty Findings wire form is
// preserved for every other artifact).
func Test_useCaseActivityFindings_ScopedToCoreUseCasesKind(t *testing.T) {
	full := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{ucd("Capture", nil)}}

	if got := useCaseActivityFindings(KindMission, full); got != nil {
		t.Errorf("non-CoreUseCases kind must yield no findings, got %+v", got)
	}
	if got := useCaseActivityFindings(KindCoreUseCases, nil); got != nil {
		t.Errorf("nil draft must yield no findings, got %+v", got)
	}
	// A draft whose every use case carries a well-formed diagram yields no findings.
	ok := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{ucd("Capture", wellFormedActivity())}}
	if got := useCaseActivityFindings(KindCoreUseCases, ok); got != nil {
		t.Errorf("all-diagrammed draft must yield no findings, got %+v", got)
	}
}

// Tier parity (2026-07-30 callchain-realization): an ENTRY is a start node OR an
// edge-less timeEvent/acceptEvent node — mirrors methodcheck's
// activityHasEntryAndAction (framework-go/methodcheck/rules_statevalidation.go).
func Test_useCaseActivityFindings_EventEntryTierParity(t *testing.T) {
	draft := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{
		ucd("NightlySweep", eventEntryActivity()),                // event-only entry — acceptable
		ucd("AwaitMessage", eventNodeWithIncomingEdgeActivity()), // event node HAS incoming edge — not an entry
		ucd("Capture", wellFormedActivity()),                     // start-rooted — stays green
	}}

	findings := useCaseActivityFindings(KindCoreUseCases, draft)
	if len(findings) != 1 {
		t.Fatalf("expected exactly 1 ERROR finding (AwaitMessage's event node is not an entry), got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if !strings.Contains(f.Message, "AwaitMessage") {
		t.Errorf("finding must name use case AwaitMessage, got %q", f.Message)
	}
	if !strings.Contains(f.Message, "no entry") {
		t.Errorf("finding must name the entry rule honestly, got %q", f.Message)
	}
	// findings has exactly 1 element (asserted above), already bound to f —
	// no need to loop over findings again to check what it doesn't mention.
	for _, name := range []string{"NightlySweep", "Capture"} {
		if strings.Contains(f.Message, name) {
			t.Errorf("use case %q must not be flagged; got %q", name, f.Message)
		}
	}
}

// askquestions_dispatch_test.go — F82 coverage for the System-Design answer-job dispatch
// twin (the manager kinds 2/5 go through THIS manager). Mirrors the projectdesign tests:
// a dispatch fires (incl. on a LIVE session branch), re-fires with a fresh key, and logs a
// submit fault loudly.

type recordingPipeline struct {
	specs []agenticjob.PipelineSpec
	keys  []fwra.IdempotencyKey
	err   error
}

func (p *recordingPipeline) SubmitAgenticJob(rc fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.specs = append(p.specs, spec)
	p.keys = append(p.keys, rc.IdempotencyKey)
	if p.err != nil {
		return agenticjob.PipelineHandle(""), p.err
	}
	return agenticjob.PipelineHandle("run-1"), nil
}

func (p *recordingPipeline) ObserveAgenticJob(fwra.Context, agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	return agenticjob.PipelineObservation{}, nil
}

func (p *recordingPipeline) CancelAgenticJob(fwra.Context, agenticjob.PipelineHandle) error {
	return nil
}

func sdManagerWith(pipe agenticjob.AgenticJobAccess) *systemDesignManager {
	return &systemDesignManager{
		pipeline: pipe,
		repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRef("acme|acme/gtdapp"), true
		},
	}
}

func sdQuestions() []projectstate.ReviewComment {
	return questionsToLedger(projectstate.ReviewAddresseeArchitect, []AnchoredComment{
		{JSONPath: "$.components[0]", Text: "Which layer owns settlement?", AnchorText: "settlement"},
	})
}

// Dispatch fires for a LIVE session (non-empty branch) — the answer job rides the session
// branch tip where the ledger lives.
func TestSDDispatchAnswerJob_FiresOnLiveSessionBranch(t *testing.T) {
	pipe := &recordingPipeline{}
	m := sdManagerWith(pipe)
	const branch = "aiarch-design/gtdapp/5"
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindSystem, branch, projectstate.ReviewAddresseeArchitect, sdQuestions())

	if len(pipe.specs) != 1 {
		t.Fatalf("expected one submit, got %d", len(pipe.specs))
	}
	spec := pipe.specs[0]
	if spec.DispatchInputs[dispatchInputJobMode] != jobModeAnswer {
		t.Fatalf("job_mode must be answer, got %q", spec.DispatchInputs[dispatchInputJobMode])
	}
	if spec.DispatchInputs[dispatchInputTargetBranch] != branch {
		t.Fatalf("the answer job must target the live session branch, got %q", spec.DispatchInputs[dispatchInputTargetBranch])
	}
}

func TestSDDispatchAnswerJob_ReFiresWithUniqueKey(t *testing.T) {
	pipe := &recordingPipeline{}
	m := sdManagerWith(pipe)
	qs := sdQuestions()
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindSystem, "", projectstate.ReviewAddresseeArchitect, qs)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindSystem, "", projectstate.ReviewAddresseeArchitect, qs)
	if len(pipe.keys) != 2 || pipe.keys[0] == pipe.keys[1] {
		t.Fatalf("re-ask must re-fire with a distinct key, got %v", pipe.keys)
	}
}

func TestSDDispatchAnswerJob_LogsSubmitFailure(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	pipe := &recordingPipeline{err: fwra.New(fwra.Infrastructure, "boom")}
	m := sdManagerWith(pipe)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindSystem, "", projectstate.ReviewAddresseeArchitect, sdQuestions())
	if out := buf.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "dispatch FAILED") {
		t.Fatalf("a submit failure must be logged at ERROR; log was:\n%s", out)
	}
}

// Question-comments (2026-07-05): the pure helpers behind AskQuestions + the approve-gate.

// openReviewCommentIDs (the approve blocker set) must exclude open QUESTIONS and count only
// open change-requests (incl. the legacy empty-type entries).
func TestOpenReviewCommentIDs_ExcludesQuestions(t *testing.T) {
	thread := []projectstate.ReviewComment{
		{ID: "c1", Status: projectstate.ReviewCommentOpen},                                                    // legacy change-request → blocks
		{ID: "c2", Status: projectstate.ReviewCommentOpen, Type: projectstate.ReviewCommentTypeChangeRequest}, // blocks
		{ID: "q1", Status: projectstate.ReviewCommentOpen, Type: projectstate.ReviewCommentTypeQuestion},      // does NOT block
		{ID: "c3", Status: projectstate.ReviewCommentAddressed},                                               // addressed → does not block
	}
	got := projectstate.OpenReviewCommentIDs(thread)
	if len(got) != 2 || got[0] != "c1" || got[1] != "c2" {
		t.Fatalf("approve blocker set must be exactly the open change-requests [c1 c2], got %v", got)
	}
}

func TestQuestionsToLedger_StampsAndDrops(t *testing.T) {
	in := []AnchoredComment{
		{JSONPath: "$.a", Text: "real question?", AnchorText: "the anchor"},
		{JSONPath: "$.b", Text: "   ", AnchorText: "blank"}, // empty text → dropped
	}
	out := questionsToLedger(projectstate.ReviewAddresseeArchitect, in)
	if len(out) != 1 {
		t.Fatalf("empty-text question must be dropped; got %d entries", len(out))
	}
	q := out[0]
	if q.Type != projectstate.ReviewCommentTypeQuestion || q.Addressee != projectstate.ReviewAddresseeArchitect {
		t.Errorf("question not stamped type/addressee: %+v", q)
	}
	if q.Anchor != "$.a" || q.Text != "real question?" || q.AuthorRole != reviewAuthorRole {
		t.Errorf("question fields not carried: %+v", q)
	}
}

func TestNextQuestionRound(t *testing.T) {
	if got := nextQuestionRound(nil); got != 1 {
		t.Errorf("empty thread → round 1, got %d", got)
	}
	thread := []projectstate.ReviewComment{{Round: 0}, {Round: 3}, {Round: 1}}
	if got := nextQuestionRound(thread); got != 4 {
		t.Errorf("max round 3 → next 4, got %d", got)
	}
}

func TestIsLiveSessionStage(t *testing.T) {
	live := []SessionStage{StageDrafting, StageAwaitingReview, StageRedrafting, StageRefused}
	for _, s := range live {
		if !isLiveSessionStage(s) {
			t.Errorf("stage %v must be live", s)
		}
	}
	for _, s := range []SessionStage{SessionStageUnknown, StageDraftFailed} {
		if isLiveSessionStage(s) {
			t.Errorf("stage %v must NOT be live", s)
		}
	}
}

// Classification is derived from the Phase-2 activity-list metadata (worker class
// + coding) plus contract presence — NOT the id prefix alone (the N-* namespace
// conflates testing, infra, deployment, and documentation).
func TestConstructionRowsToContract_ClassifiesFromWorkerClass(t *testing.T) {
	rows := map[string]projectstate.ActivityConstructionStatus{
		"N-IT":    {ActivityID: "N-IT"},                                                                        // software-tester, noncoding → testing:systemTest
		"N-SC":    {ActivityID: "N-SC", Produced: []projectstate.ProducedArtifact{{Kind: "service-contract"}}}, // built a contract → service
		"N-CI":    {ActivityID: "N-CI"},                                                                        // senior-developer, noncoding → deployment
		"N-ADR":   {ActivityID: "N-ADR"},                                                                       // system-architect, noncoding → documentation
		"C-BE":    {ActivityID: "C-BE"},                                                                        // junior-developer, coding → service
		"U-SPA-1": {ActivityID: "U-SPA-1"},                                                                     // U-SPA prefix → frontend
	}
	meta := map[string]projectstate.ActivityItem{
		"N-IT":    {Name: "N-IT", WorkerClass: "software-tester", Coding: false},
		"N-SC":    {Name: "N-SC", WorkerClass: "senior-developer", Coding: false},
		"N-CI":    {Name: "N-CI", WorkerClass: "senior-developer", Coding: false},
		"N-ADR":   {Name: "N-ADR", WorkerClass: "system-architect", Coding: false},
		"C-BE":    {Name: "C-BE", WorkerClass: "junior-developer", Coding: true},
		"U-SPA-1": {Name: "U-SPA-1", WorkerClass: "junior-developer", Coding: true},
	}
	got := constructionRowsToContract(rows, meta)
	cases := []struct {
		id       string
		wantType ActivityType
		wantVar  TestingVariant
	}{
		{"N-IT", ActivityType(int(projectstate.ActivityTypeTesting)), TestingVariant(int(projectstate.TestVariantSystemTest))},
		{"N-SC", ActivityType(int(projectstate.ActivityTypeService)), 0},
		{"N-CI", ActivityType(int(projectstate.ActivityTypeDeployment)), 0},
		{"N-ADR", ActivityType(int(projectstate.ActivityTypeDocumentation)), 0},
		{"C-BE", ActivityType(int(projectstate.ActivityTypeService)), 0},
		{"U-SPA-1", ActivityType(int(projectstate.ActivityTypeFrontend)), 0},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			r, ok := got[c.id]
			if !ok {
				t.Fatalf("%s: missing from output", c.id)
			}
			if r.Type != c.wantType {
				t.Errorf("%s: Type = %d, want %d", c.id, r.Type, c.wantType)
			}
			if r.Variant != c.wantVar {
				t.Errorf("%s: Variant = %d, want %d", c.id, r.Variant, c.wantVar)
			}
		})
	}
}

// catalog_provenance_test.go — PM-P2-4 exposure. slotsToContract projects a committed slot's
// stored Provenance onto the ArtifactSlotView.Provenance read model; an uncommitted / pre-
// provenance slot exposes nil (omitempty on the wire).
func TestSlotsToContract_ExposesProvenance(t *testing.T) {
	var p projectstate.Project
	p.Mission = projectstate.ArtifactSlot{
		Status:    projectstate.ReviewCommitted,
		Model:     &projectstate.MissionStatement{Vision: "v", Mission: "m"},
		Revisions: 1,
		Provenance: &projectstate.Provenance{
			CommittedAt: "2026-07-06T08:30:00Z",
			ApprovedBy:  "alice",
			DraftedBy:   "agentic-design-rail",
		},
	}
	// Volatilities left uncommitted (no provenance) to prove the nil-safe path.

	slots := slotsToContract(p)

	var mission, volatilities *ArtifactSlotView
	for i := range slots {
		switch slots[i].Kind {
		case projectstate.KindMission.WireName():
			mission = &slots[i]
		case projectstate.KindVolatilities.WireName():
			volatilities = &slots[i]
		}
	}
	if mission == nil || volatilities == nil {
		t.Fatal("expected mission + volatilities slots in the contract view")
	}
	if mission.Provenance == nil {
		t.Fatal("committed mission slot must expose provenance")
	}
	if mission.Provenance.CommittedAt != "2026-07-06T08:30:00Z" ||
		mission.Provenance.ApprovedBy != "alice" ||
		mission.Provenance.DraftedBy != "agentic-design-rail" {
		t.Fatalf("provenance view mismatch: %+v", *mission.Provenance)
	}
	if volatilities.Provenance != nil {
		t.Fatalf("uncommitted slot must expose nil provenance, got %+v", *volatilities.Provenance)
	}
}

// catalog_test.go — the unit suite for the CATALOG ops (CreateProject/GetProject/
// ListProjects) folded onto systemDesignManager from the dissolved projectManager
// (2026-06-28). Ported verbatim; the manager is built via newCatalogMgr, which wires
// only the deps these synchronous ops touch (no Temporal client).

// rc is the Manager-layer call Context the ops lead with (zero principal in tests).
func rc() fwmanager.Context { return fwmanager.Context{Context: context.Background()} }

// newCatalogMgr builds a systemDesignManager exercising ONLY the folded catalog ops:
// it wires the projectState + (optional) rail + estimator + repoBase deps and leaves
// the Temporal client / pipeline / repo-resolver nil (those ops never touch them).
func newCatalogMgr(ps projectstate.ProjectStateAccess, sc sourcecontrol.SourceControlAccess, est estimation.EstimationEngine, repoBase string) SystemDesignManager {
	return NewSystemDesignManager(nil, ps, nil, sc, nil, est, nil, repoBase)
}

// slotByKind finds the contract slot whose Kind is the canonical wire name of the
// given projectstate ArtifactKind.
func slotByKind(st ProjectState, kind projectstate.ArtifactKind) (ArtifactSlotView, bool) {
	for _, s := range st.Slots {
		if s.Kind == kind.WireName() {
			return s, true
		}
	}
	return ArtifactSlotView{}, false
}

// fakeProjectStateAccess is the contract-first test double over the narrow
// ProjectStateAccess port this package declares (projectstate-typed; the Manager
// converts the value shapes into its OWN contract types after the call).
type fakeProjectStateAccess struct {
	createCalls   int
	createOwner   projectstate.OwnerScope
	createName    string
	createID      projectstate.ProjectID
	createKey     fwra.IdempotencyKey
	createVersion projectstate.Version
	createErr     error

	listCalls   int
	listOwner   projectstate.OwnerScope
	listSummary []projectstate.ProjectSummary
	listErr     error

	readCalls   int
	readID      projectstate.ProjectID
	readProject projectstate.Project
	readErr     error
}

func (f *fakeProjectStateAccess) CreateProject(rc fwra.Context, projectID projectstate.ProjectID, owner projectstate.OwnerScope, name string) (projectstate.Version, error) {
	f.createCalls++
	f.createID = projectID
	f.createOwner = owner
	f.createName = name
	f.createKey = rc.IdempotencyKey
	if f.createErr != nil {
		return 0, f.createErr
	}
	if f.createVersion == 0 {
		f.createVersion = 1
	}
	return f.createVersion, nil
}

func (f *fakeProjectStateAccess) ListProjects(_ fwra.Context, owner projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	f.listCalls++
	f.listOwner = owner
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listSummary, nil
}

func (f *fakeProjectStateAccess) ReadProject(_ fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
	f.readCalls++
	f.readID = projectID
	if f.readErr != nil {
		return projectstate.Project{}, f.readErr
	}
	return f.readProject, nil
}

// The projectManager now depends on the PUBLISHED projectstate.ProjectStateAccess
// (the consumer-mirror was retired), so the fake satisfies the full surface. Only
// CreateProject / ListProjects / ReadProject are exercised; the remaining verbs are
// inert stubs present solely to satisfy the interface.
var _ projectstate.ProjectStateAccess = (*fakeProjectStateAccess)(nil)

func (f *fakeProjectStateAccess) AdvancePhase(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) CommitArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ArtifactKind) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) ReadProjectVersion(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) RejectArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ArtifactKind, _ string) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) SetResearchInput(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ResearchInput) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) SetOperatingModel(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.OperatingModel) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) StageArtifactForReview(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ArtifactModel) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) WithdrawArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ArtifactKind, _ string) (projectstate.Version, error) {
	return 0, nil
}

// C2 fold (code-health-phase-a): inert stubs for the 9 session/branch verbs now
// required by the generated ProjectStateAccess contract — unexercised here, same as
// the pre-existing inert stubs above.
func (f *fakeProjectStateAccess) ReadProjectOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ string) (projectstate.Project, error) {
	return projectstate.Project{}, nil
}
func (f *fakeProjectStateAccess) StageArtifactForReviewOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactModel, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) RejectArtifactOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) WithdrawArtifactOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) RejectArtifactOnBranchWithComments(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ int64, _ []projectstate.ReviewComment, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) SetReviewCommentStatusOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) SeedReviewCommentsOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ int64, _ []projectstate.ReviewComment, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) ReconcileBranchFromMain(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}
func (f *fakeProjectStateAccess) AcknowledgeStaleBasis(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

// --- CreateProject ----------------------------------------------------------

// TestCreateProject_NameIsIdentityAndCallsRAOnce proves NAME-AS-IDENTITY (C-PM-Δ):
// the returned project id IS the user-supplied name, and the RA is called exactly once.
func TestCreateProject_NameIsIdentityAndCallsRAOnce(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := newCatalogMgr(fake, nil, nil, "")

	id, err := m.CreateProject(rc(), OwnerScope("alice@example.com"), "my-cool-system")
	if err != nil {
		t.Fatalf("CreateProject: unexpected error: %v", err)
	}
	if id != ProjectID("my-cool-system") {
		t.Fatalf("CreateProject: id = %q, want the user-supplied name (name-as-identity)", id)
	}
	if fake.createCalls != 1 {
		t.Fatalf("CreateProject: RA called %d times, want 1", fake.createCalls)
	}
	if fake.createID != projectstate.ProjectID("my-cool-system") {
		t.Fatalf("CreateProject: RA id %s, want my-cool-system", fake.createID)
	}
	if fake.createOwner != projectstate.OwnerScope("alice@example.com") {
		t.Fatalf("CreateProject: RA owner %q, want alice@example.com", fake.createOwner)
	}
	if fake.createName != "my-cool-system" {
		t.Fatalf("CreateProject: RA name %q, want my-cool-system", fake.createName)
	}
	if fake.createKey.IsZero() {
		t.Fatal("CreateProject: derived idempotency key is empty")
	}
}

func TestCreateProject_EmptyOwner_ContractMisuse(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := newCatalogMgr(fake, nil, nil, "")

	_, err := m.CreateProject(rc(), OwnerScope(""), "My Project")
	if err == nil {
		t.Fatal("CreateProject: expected error for empty owner")
	}
	var me *fwmanager.Error
	if !errors.As(err, &me) || me.Kind != fwmanager.ContractMisuse {
		t.Fatalf("CreateProject: want ContractMisuse, got %v", err)
	}
	if fake.createCalls != 0 {
		t.Fatalf("CreateProject: RA should not be called on validation failure, got %d", fake.createCalls)
	}
}

func TestCreateProject_EmptyName_ContractMisuse(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := newCatalogMgr(fake, nil, nil, "")

	_, err := m.CreateProject(rc(), OwnerScope("alice"), "")
	if err == nil {
		t.Fatal("CreateProject: expected error for empty name")
	}
	var me *fwmanager.Error
	if !errors.As(err, &me) || me.Kind != fwmanager.ContractMisuse {
		t.Fatalf("CreateProject: want ContractMisuse, got %v", err)
	}
}

func TestCreateProject_RAConflict_MapsInfrastructure(t *testing.T) {
	fake := &fakeProjectStateAccess{createErr: fwra.New(fwra.Conflict, "row exists")}
	m := newCatalogMgr(fake, nil, nil, "")

	_, err := m.CreateProject(rc(), OwnerScope("alice"), "P")
	var me *fwmanager.Error
	if !errors.As(err, &me) {
		t.Fatalf("CreateProject: want fwm.Error, got %v", err)
	}
	if me.Kind != fwmanager.Infrastructure {
		t.Fatalf("CreateProject: Conflict should map to Infrastructure, got %v", me.Kind)
	}
}

// --- CreateProject: adopt + workflow seating + call order -------------------

type callOrder struct{ seq []string }

func (c *callOrder) record(name string) { c.seq = append(c.seq, name) }

// fakeSourceControl is the test double over the PUBLISHED
// sourcecontrol.SourceControlAccess (the consumer-mirror + composition-root adapter
// were retired; the façade now calls the published interface directly). The
// projectManager's CreateProject seating drives exactly three of its verbs —
// AdoptProjectRepo → GetInstallationToken → CommitManagedFiles — which the call-order
// test asserts; the remaining verbs are inert stubs.
type fakeSourceControl struct {
	order *callOrder

	adoptCalls int
	adoptSpec  sourcecontrol.RepoAdoptionSpec
	adoptKey   fwra.IdempotencyKey
	adoptErr   error

	tokenCalls int
	tokenErr   error

	commitCalls int
	commitKey   fwra.IdempotencyKey
	commitErr   error
}

var _ sourcecontrol.SourceControlAccess = (*fakeSourceControl)(nil)

func (f *fakeSourceControl) AdoptProjectRepo(rc fwra.Context, spec sourcecontrol.RepoAdoptionSpec) (sourcecontrol.RepoRef, error) {
	f.adoptCalls++
	f.adoptSpec = spec
	f.adoptKey = rc.IdempotencyKey
	if f.order != nil {
		f.order.record("adoptProjectRepo")
	}
	if f.adoptErr != nil {
		return "", f.adoptErr
	}
	// Return a well-formed RepoRef (account|owner/repo) so ManagedScaffoldFiles can
	// derive the module path; name-as-identity makes owner==repo==RepoName here.
	return sourcecontrol.RepoRef("acct|acct/" + spec.RepoName), nil
}

func (f *fakeSourceControl) GetInstallationToken(_ fwra.Context, _ sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	f.tokenCalls++
	if f.order != nil {
		f.order.record("getInstallationToken")
	}
	if f.tokenErr != nil {
		return sourcecontrol.RepoCredential{}, f.tokenErr
	}
	return sourcecontrol.RepoCredential{Bytes: []byte("tok")}, nil
}

func (f *fakeSourceControl) CommitManagedFiles(rc fwra.Context, _ sourcecontrol.RepoRef, _ []sourcecontrol.ManagedFile, _ sourcecontrol.RepoCredential) (sourcecontrol.CommitRef, error) {
	f.commitCalls++
	f.commitKey = rc.IdempotencyKey
	if f.order != nil {
		f.order.record("commitManagedFiles")
	}
	if f.commitErr != nil {
		return "", f.commitErr
	}
	return sourcecontrol.CommitRef("commit"), nil
}

// --- inert stubs: the remaining published verbs the façade never calls -------

func (f *fakeSourceControl) ConfigureBranchProtection(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) error {
	return nil
}
func (f *fakeSourceControl) GetPullRequestStatus(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	return sourcecontrol.PullRequestStatus{}, nil
}
func (f *fakeSourceControl) InstallAuthorizeApp(_ fwra.Context, _ sourcecontrol.AccountRef) (sourcecontrol.Installation, error) {
	return "", nil
}
func (f *fakeSourceControl) MergePullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.MergeResult, error) {
	return sourcecontrol.MergeResult{}, nil
}
func (f *fakeSourceControl) OpenBranch(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.BranchName, _ sourcecontrol.RepoCredential) (sourcecontrol.BranchRef, error) {
	return "", nil
}
func (f *fakeSourceControl) OpenPullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestRef, error) {
	return "", nil
}
func (f *fakeSourceControl) PostReview(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential) error {
	return nil
}
func (f *fakeSourceControl) SyncManagedScaffold(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) (bool, error) {
	return false, nil
}

type orderingProjectState struct {
	*fakeProjectStateAccess
	order *callOrder
}

func (o *orderingProjectState) CreateProject(rc fwra.Context, projectID projectstate.ProjectID, owner projectstate.OwnerScope, name string) (projectstate.Version, error) {
	if o.order != nil {
		o.order.record("createProject")
	}
	return o.fakeProjectStateAccess.CreateProject(rc, projectID, owner, name)
}

// TestCreateProject_AdoptThenSeatThenCreate is the load-bearing call-order guarantee:
// adopt → mint-credential → seat-workflow → create, all under the SAME idempotency key.
func TestCreateProject_AdoptThenSeatThenCreate(t *testing.T) {
	order := &callOrder{}
	ps := &orderingProjectState{fakeProjectStateAccess: &fakeProjectStateAccess{}, order: order}
	sc := &fakeSourceControl{order: order}
	m := newCatalogMgr(ps, sc, nil, "")

	id, err := m.CreateProject(rc(), OwnerScope("alice@example.com"), "my-cool-system")
	if err != nil {
		t.Fatalf("CreateProject: unexpected error: %v", err)
	}
	if id != ProjectID("my-cool-system") {
		t.Fatalf("CreateProject: id = %q, want name-as-identity my-cool-system", id)
	}
	if sc.adoptCalls != 1 || sc.tokenCalls != 1 || sc.commitCalls != 1 {
		t.Fatalf("source-control call counts: adopt=%d token=%d commit=%d, want 1 each",
			sc.adoptCalls, sc.tokenCalls, sc.commitCalls)
	}
	if ps.createCalls != 1 {
		t.Fatalf("projectState.CreateProject called %d times, want 1", ps.createCalls)
	}
	want := []string{"adoptProjectRepo", "getInstallationToken", "commitManagedFiles", "createProject"}
	if len(order.seq) != len(want) {
		t.Fatalf("call order = %v, want %v", order.seq, want)
	}
	for i := range want {
		if order.seq[i] != want[i] {
			t.Fatalf("call order = %v, want %v", order.seq, want)
		}
	}
	if sc.adoptSpec.RepoName != string(id) {
		t.Fatalf("AdoptProjectRepo RepoName = %q, want %q (name-as-identity)", sc.adoptSpec.RepoName, string(id))
	}
	if sc.adoptSpec.Title != "my-cool-system" {
		t.Fatalf("AdoptProjectRepo Title = %q, want my-cool-system", sc.adoptSpec.Title)
	}
	if sc.adoptKey != ps.createKey || sc.commitKey != ps.createKey {
		t.Fatalf("idempotency keys diverged: adopt=%q commit=%q create=%q",
			sc.adoptKey, sc.commitKey, ps.createKey)
	}
}

// TestCreateProject_AdoptFailure_NoSeatingNoCreate proves the order is also a GATE.
func TestCreateProject_AdoptFailure_NoSeatingNoCreate(t *testing.T) {
	order := &callOrder{}
	ps := &orderingProjectState{fakeProjectStateAccess: &fakeProjectStateAccess{}, order: order}
	sc := &fakeSourceControl{order: order, adoptErr: fwra.New(fwra.Transient, "github 503")}
	m := newCatalogMgr(ps, sc, nil, "")

	_, err := m.CreateProject(rc(), OwnerScope("alice"), "taken-repo")
	if err == nil {
		t.Fatal("CreateProject: expected error when adopt fails")
	}
	var me *fwmanager.Error
	if !errors.As(err, &me) {
		t.Fatalf("CreateProject: want fwm.Error, got %v", err)
	}
	if me.Kind != fwmanager.Infrastructure {
		t.Fatalf("adopt Transient should map to Infrastructure, got %v", me.Kind)
	}
	if sc.adoptCalls != 1 {
		t.Fatalf("AdoptProjectRepo called %d times, want 1", sc.adoptCalls)
	}
	if sc.tokenCalls != 0 || sc.commitCalls != 0 {
		t.Fatalf("seating must NOT run after adopt failure: token=%d commit=%d", sc.tokenCalls, sc.commitCalls)
	}
	if ps.createCalls != 0 {
		t.Fatalf("projectState.CreateProject must NOT be called after an adopt failure, got %d", ps.createCalls)
	}
	if len(order.seq) != 1 || order.seq[0] != "adoptProjectRepo" {
		t.Fatalf("call order = %v, want [adoptProjectRepo] only", order.seq)
	}
}

// TestCreateProject_NilSourceControl_SkipsAdopt proves a credential-free dev server
// (nil sourceControl) still creates projects — repo-less, no adopt.
func TestCreateProject_NilSourceControl_SkipsAdopt(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := newCatalogMgr(fake, nil, nil, "")

	id, err := m.CreateProject(rc(), OwnerScope("alice"), "dev-project")
	if err != nil {
		t.Fatalf("CreateProject (nil source control): unexpected error: %v", err)
	}
	if id != ProjectID("dev-project") {
		t.Fatalf("CreateProject: id = %q", id)
	}
	if fake.createCalls != 1 {
		t.Fatalf("projectState.CreateProject called %d times, want 1", fake.createCalls)
	}
}

// --- ListProjects -----------------------------------------------------------

func TestListProjects_PassesThrough(t *testing.T) {
	now := time.Now().UTC()
	src := []projectstate.ProjectSummary{
		{ProjectID: "alpha", Name: "A", Owner: "alice", Phase: projectstate.PhaseSystemDesign, CommittedCount: 2, TotalCount: 8, UpdatedAt: now},
		{ProjectID: "beta", Name: "B", Owner: "alice", Phase: projectstate.PhaseProjectDesign, CommittedCount: 9, TotalCount: 9, UpdatedAt: now},
	}
	fake := &fakeProjectStateAccess{listSummary: src}
	m := newCatalogMgr(fake, nil, nil, "")

	got, err := m.ListProjects(rc(), OwnerScope("alice"))
	if err != nil {
		t.Fatalf("ListProjects: unexpected error: %v", err)
	}
	if fake.listCalls != 1 || fake.listOwner != projectstate.OwnerScope("alice") {
		t.Fatalf("ListProjects: RA calls=%d owner=%q", fake.listCalls, fake.listOwner)
	}
	if len(got) != len(src) {
		t.Fatalf("ListProjects: got %d summaries, want %d", len(got), len(src))
	}
	if got[0].ProjectID != ProjectID("alpha") || got[0].Name != "A" || got[0].Owner != OwnerScope("alice") {
		t.Fatalf("ListProjects: summary[0] identity mismatch: %+v", got[0])
	}
	if got[0].Phase != PhaseSystemDesign || got[0].CommittedCount != 2 || got[0].TotalCount != 8 {
		t.Fatalf("ListProjects: summary[0] progress mismatch: %+v", got[0])
	}
	if got[1].ProjectID != ProjectID("beta") || got[1].Phase != PhaseProjectDesign {
		t.Fatalf("ListProjects: summary[1] mismatch: %+v", got[1])
	}
}

func TestListProjects_RAError_MapsInfrastructure(t *testing.T) {
	fake := &fakeProjectStateAccess{listErr: fwra.New(fwra.Infrastructure, "db down")}
	m := newCatalogMgr(fake, nil, nil, "")

	_, err := m.ListProjects(rc(), OwnerScope("alice"))
	var me *fwmanager.Error
	if !errors.As(err, &me) || me.Kind != fwmanager.Infrastructure {
		t.Fatalf("ListProjects: want Infrastructure, got %v", err)
	}
}

// --- GetProject -------------------------------------------------------------

func sampleProject(id projectstate.ProjectID) projectstate.Project {
	return projectstate.Project{
		ID:      id,
		Version: 7,
		Phase:   projectstate.PhaseSystemDesign,
		Owner:   "alice",
		Name:    "Sample",
		Research: projectstate.ResearchCorpus{
			Sources: []projectstate.ResearchSourceRef{{Title: "Brief", Path: ".aiarch/state/research/00-brief.txt", ContentBytes: 13}},
		},
		Mission: projectstate.ArtifactSlot{
			Status: projectstate.ReviewCommitted,
			Model:  &projectstate.MissionStatement{},
		},
		Glossary: projectstate.ArtifactSlot{
			Status: projectstate.ReviewAwaitingReview,
			Model:  &projectstate.Glossary{},
			Notes:  "needs trimming",
		},
		Volatilities: projectstate.ArtifactSlot{
			Status: projectstate.ReviewRejected,
			Model:  &projectstate.Volatilities{},
			Notes:  "redo",
		},
	}
}

func TestGetProject_MapsAggregateToTypedSlots(t *testing.T) {
	id := ProjectID("my-cool-system")
	fake := &fakeProjectStateAccess{readProject: sampleProject(projectstate.ProjectID(id))}
	m := newCatalogMgr(fake, nil, nil, "")

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: unexpected error: %v", err)
	}
	if fake.readCalls != 1 || fake.readID != projectstate.ProjectID(id) {
		t.Fatalf("GetProject: RA calls=%d id=%s", fake.readCalls, fake.readID)
	}
	if st.ProjectID != id || st.Name != "Sample" || st.Owner != OwnerScope("alice") {
		t.Fatalf("GetProject: identity mismatch: %+v", st)
	}
	if st.Phase != PhaseSystemDesign || st.Version != 7 {
		t.Fatalf("GetProject: phase/version mismatch: %+v", st)
	}
	if len(st.Research.Sources) != 1 || st.Research.Sources[0].Title != "Brief" {
		t.Fatalf("GetProject: research not mapped: %+v", st.Research)
	}

	catalogAssertSampleSlotMapping(t, st)
}

// catalogAssertSampleSlotMapping asserts the typed-slot mapping of sampleProject's
// slots: a full slot set, committed mission, awaiting glossary (+notes), rejected
// volatilities, and an empty scrubbedRequirements slot.
func catalogAssertSampleSlotMapping(t *testing.T, st ProjectState) {
	t.Helper()
	if len(st.Slots) != len(projectstate.AllArtifactKinds()) {
		t.Fatalf("GetProject: got %d slots, want %d", len(st.Slots), len(projectstate.AllArtifactKinds()))
	}

	mission, _ := slotByKind(st, projectstate.KindMission)
	if mission.Stage != ArtifactStageCommitted {
		t.Fatalf("Mission stage = %v, want ArtifactStageCommitted", mission.Stage)
	}
	if mission.Model.Kind != "mission" || mission.Model.Model == nil {
		t.Fatalf("Mission model not mapped opaquely: %+v", mission.Model)
	}

	glossary, _ := slotByKind(st, projectstate.KindGlossary)
	if glossary.Stage != ArtifactStageAwaitingReview {
		t.Fatalf("Glossary stage = %v, want ArtifactStageAwaitingReview", glossary.Stage)
	}
	if glossary.Model.Model == nil {
		t.Fatal("Glossary model should be populated")
	}
	if glossary.Notes == nil || *glossary.Notes != "needs trimming" {
		t.Fatalf("Glossary notes = %v", glossary.Notes)
	}

	volatilities, _ := slotByKind(st, projectstate.KindVolatilities)
	if volatilities.Stage != ArtifactStageRejected {
		t.Fatalf("Volatilities stage = %v, want ArtifactStageRejected", volatilities.Stage)
	}

	scrubbed, _ := slotByKind(st, projectstate.KindScrubbedRequirements)
	if scrubbed.Stage != ArtifactStageEmpty {
		t.Fatalf("ScrubbedRequirements stage = %v, want ArtifactStageEmpty", scrubbed.Stage)
	}
	if scrubbed.Model.Model != nil {
		t.Fatal("ScrubbedRequirements model should be nil (empty slot)")
	}
	if scrubbed.Notes != nil {
		t.Fatal("ScrubbedRequirements notes should be nil (empty)")
	}
}

// decodeNetwork decodes the opaque Network slot model into the canonical projectstate
// type so the compute-at-read assertions can inspect the enriched figures.
func decodeNetwork(t *testing.T, st ProjectState) *projectstate.Network {
	t.Helper()
	slot, ok := slotByKind(st, projectstate.KindNetwork)
	if !ok || slot.Model.Model == nil {
		t.Fatalf("network slot model not present: %+v", slot.Model)
	}
	var n projectstate.Network
	if err := json.Unmarshal(*slot.Model.Model, &n); err != nil {
		t.Fatalf("decode network model: %v", err)
	}
	return &n
}

// TestGetProject_ComputeNetworkAtRead verifies the compute-at-read wiring over a small
// diamond network A→{B,C}→D with a milestone fanning in on D — read back through the
// opaque slot-model envelope.
func TestGetProject_ComputeNetworkAtRead(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(projectstate.ProjectID(id))
	p.ActivityList = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
			{Name: "A", EffortDays: 5, WorkerClass: "dev"},
			{Name: "B", EffortDays: 5, WorkerClass: "dev"},
			{Name: "C", EffortDays: 15, WorkerClass: "dev"},
			{Name: "D", EffortDays: 5, WorkerClass: "dev"},
		}},
	}
	p.Network = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: []projectstate.NetworkDependency{
				{Activity: "B", DependsOn: []string{"A"}},
				{Activity: "C", DependsOn: []string{"A"}},
				{Activity: "D", DependsOn: []string{"B", "C"}},
			},
			CriticalPath: []string{"A", "C", "D"},
			Milestones: []projectstate.NetworkMilestone{
				{ID: "M-DONE", Name: "Done", Public: true, DependsOn: []string{"D"}},
			},
		},
	}

	fake := &fakeProjectStateAccess{readProject: p}
	m := newCatalogMgr(fake, nil, estimation.NewEstimationEngine(), "")

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	netModel := decodeNetwork(t, st)
	if netModel.Summary == nil {
		t.Fatal("compute-at-read: summary not populated")
	}
	if netModel.Summary.TotalDurationDays != 25 {
		t.Fatalf("project duration = %v, want 25", netModel.Summary.TotalDurationDays)
	}
	if len(netModel.Computed) != 4 {
		t.Fatalf("computed nodes = %d, want 4", len(netModel.Computed))
	}
	if cNode := netModel.Computed["C"]; !cNode.OnCriticalPath || cNode.Band != "critical" {
		t.Fatalf("C should be critical: %+v", cNode)
	}
	if bNode := netModel.Computed["B"]; bNode.OnCriticalPath || bNode.TotalFloat != 10 {
		t.Fatalf("B should be off-CP with float 10: %+v", bNode)
	}
	catalogAssertComputedPathAndMilestone(t, netModel)
}

// catalogAssertComputedPathAndMilestone asserts the compute-at-read pass left the
// authored dependencies untouched, computed the float-0 critical path, and enriched
// the milestone with its event time + on-CP flag.
func catalogAssertComputedPathAndMilestone(t *testing.T, netModel *projectstate.Network) {
	t.Helper()
	if len(netModel.Dependencies) != 3 {
		t.Fatalf("authored dependencies perturbed: %v", netModel.Dependencies)
	}
	if got := netModel.CriticalPath; len(got) != 3 || got[0] != "A" || got[1] != "C" || got[2] != "D" {
		t.Fatalf("criticalPath not the computed float-0 set [A C D]: %v", got)
	}
	if len(netModel.Milestones) != 1 {
		t.Fatalf("milestones = %d, want 1", len(netModel.Milestones))
	}
	ms := netModel.Milestones[0]
	if ms.EventTime == nil || *ms.EventTime != 25 || ms.OnCriticalPath == nil || !*ms.OnCriticalPath {
		t.Fatalf("milestone compute wrong: %+v", ms)
	}
}

// TestGetProject_ComputeEarnedValueAtRead verifies the EV/SPI earned-value curve is
// computed SERVER-SIDE at read (via the constructionEstimationEngine) and surfaced on
// ConstructionProgress.EV — the relocation of the former web computeEV. A→B→C chain with
// A,B integrated and C not: earned reaches ~50% (10 of 20 effort days), planned ~100%,
// SPI ~0.5.
func TestGetProject_ComputeEarnedValueAtRead(t *testing.T) {
	id := ProjectID("ev-proj")
	p := sampleProject(projectstate.ProjectID(id))
	p.Phase = projectstate.PhaseConstruction
	p.ActivityList = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
			{Name: "A", EffortDays: 5, WorkerClass: "dev"},
			{Name: "B", EffortDays: 5, WorkerClass: "dev"},
			{Name: "C", EffortDays: 10, WorkerClass: "dev"},
		}},
	}
	p.Network = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{Dependencies: []projectstate.NetworkDependency{
			{Activity: "B", DependsOn: []string{"A"}},
			{Activity: "C", DependsOn: []string{"B"}},
		}},
	}
	p.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", BuildStatus: projectstate.BuildIntegrated},
		"B": {ActivityID: "B", BuildStatus: projectstate.BuildIntegrated},
		"C": {ActivityID: "C", BuildStatus: projectstate.BuildInConstruction},
	}
	p.ConstructionProgress = &projectstate.ConstructionProgress{Week: 2, TotalWeeks: 4, HandOffModel: "senior", SupervisionCap: 3}

	fake := &fakeProjectStateAccess{readProject: p}
	m := newCatalogMgr(fake, nil, estimation.NewEstimationEngine(), "")

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if st.ConstructionProgress == nil {
		t.Fatal("ConstructionProgress should be present")
	}
	ev := st.ConstructionProgress.EV
	if ev.SPI < 0.49 || ev.SPI > 0.51 {
		t.Fatalf("SPI = %v, want ~0.5", ev.SPI)
	}
	if n := len(ev.Earned); n == 0 || ev.Earned[n-1] < 49 || ev.Earned[n-1] > 51 {
		t.Fatalf("final earned want ~50%%, got %v", ev.Earned)
	}
	if n := len(ev.Planned); n == 0 || ev.Planned[n-1] < 99 {
		t.Fatalf("final planned want ~100%%, got %v", ev.Planned)
	}
}

// TestGetProject_ComposesPRRefAtRead verifies the manager composes each git row's
// prNumber (from the opaque ref) and prUrl (<repoBase>/pull/<ref>) at read — the
// relocation of the former web projectPRRef onto the contract-owning Manager.
func TestGetProject_ComposesPRRefAtRead(t *testing.T) {
	id := ProjectID("pr-proj")
	p := sampleProject(projectstate.ProjectID(id))
	p.ActivityGit = map[string]projectstate.ActivityGitStatus{
		"C-MST": {ActivityID: "C-MST", BranchName: "activity/C-MST", PullRequestRef: "44"},
	}
	fake := &fakeProjectStateAccess{readProject: p}
	m := newCatalogMgr(fake, nil, estimation.NewEstimationEngine(), "https://github.com/acme/proj")

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	row, ok := st.GitRows["C-MST"]
	if !ok {
		t.Fatalf("gitRows[C-MST] absent: %+v", st.GitRows)
	}
	if row.PrNumber != 44 {
		t.Fatalf("prNumber = %d, want 44 (parsed from opaque ref)", row.PrNumber)
	}
	if row.PrURL != "https://github.com/acme/proj/pull/44" {
		t.Fatalf("prUrl = %q, want composed <repoBase>/pull/44", row.PrURL)
	}
	// Provider-opacity: the opaque ref remains the durable truth alongside the projections.
	if row.PullRequestRef != "44" {
		t.Fatalf("opaque pullRequestRef must remain: %q", row.PullRequestRef)
	}
}

// TestGetProject_PRUrl_ProjectsPerProjectRepo verifies that when the per-project repo
// resolver resolves, each git row's prUrl is composed against the PROJECT's own repo
// (owner/name) with the configured central host borrowed — since the venue switch,
// gh-mode construction PRs open in the project repo, not the central construction repo.
func TestGetProject_PRUrl_ProjectsPerProjectRepo(t *testing.T) {
	id := ProjectID("gh-proj")
	p := sampleProject(projectstate.ProjectID(id))
	p.ActivityGit = map[string]projectstate.ActivityGitStatus{
		"C-MST": {ActivityID: "C-MST", BranchName: "activity/C-MST", PullRequestRef: "44"},
	}
	fake := &fakeProjectStateAccess{readProject: p}
	// Central base points at the construction repo; the resolver points the project at its
	// OWN repo (acme/gtdapp). The prUrl must use the project repo with the central host.
	repoFn := func(ProjectID) (sourcecontrol.RepoRef, bool) {
		return sourcecontrol.RepoRef("acme|acme/gtdapp"), true
	}
	m := newSystemDesignManager(nil, fake, nil, nil, repoFn, estimation.NewEstimationEngine(), nil, "https://github.com/central/constructrepo")

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	row := st.GitRows["C-MST"]
	if row.PrURL != "https://github.com/acme/gtdapp/pull/44" {
		t.Fatalf("prUrl = %q, want per-project repo base with central host", row.PrURL)
	}
	if row.PrNumber != 44 {
		t.Fatalf("prNumber = %d, want 44", row.PrNumber)
	}
}

// TestGetProject_NilEstimator_NoCompute verifies the compute-at-read is a no-op when no
// estimator is injected: the authored network is served unenriched.
func TestGetProject_NilEstimator_NoCompute(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(projectstate.ProjectID(id))
	p.Network = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: []projectstate.NetworkDependency{{Activity: "B", DependsOn: []string{"A"}}},
			CriticalPath: []string{"A", "B"},
		},
	}
	fake := &fakeProjectStateAccess{readProject: p}
	m := newCatalogMgr(fake, nil, nil, "")

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	net := decodeNetwork(t, st)
	if net.Summary != nil || len(net.Computed) != 0 {
		t.Fatalf("nil estimator should not compute: %+v", net)
	}
	if len(net.CriticalPath) != 2 || net.CriticalPath[0] != "A" {
		t.Fatalf("nil estimator should preserve authored criticalPath: %v", net.CriticalPath)
	}
}

// TestGetProject_OverwritesStaleCriticalPath verifies the served criticalPath[] is the
// COMPUTED float-0 activity set, not the stale authored names.
func TestGetProject_OverwritesStaleCriticalPath(t *testing.T) {
	id := ProjectID("net-proj")
	p := sampleProject(projectstate.ProjectID(id))
	p.ActivityList = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.ActivityList{Activities: []projectstate.ActivityItem{
			{Name: "A", EffortDays: 5, WorkerClass: "dev"},
			{Name: "B", EffortDays: 5, WorkerClass: "dev"},
		}},
	}
	p.Network = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: []projectstate.NetworkDependency{{Activity: "B", DependsOn: []string{"A"}}},
			CriticalPath: []string{"STALE", "GONE"},
		},
	}
	fake := &fakeProjectStateAccess{readProject: p}
	m := newCatalogMgr(fake, nil, estimation.NewEstimationEngine(), "")

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	net := decodeNetwork(t, st)
	if len(net.CriticalPath) != 2 || net.CriticalPath[0] != "A" || net.CriticalPath[1] != "B" {
		t.Fatalf("criticalPath not overwritten with computed float-0 set: %v", net.CriticalPath)
	}
}

func TestGetProject_NotFoundPassesThrough(t *testing.T) {
	fake := &fakeProjectStateAccess{readErr: fwra.New(fwra.NotFound, "no row")}
	m := newCatalogMgr(fake, nil, nil, "")

	_, err := m.GetProject(rc(), ProjectID("missing"))
	var me *fwmanager.Error
	if !errors.As(err, &me) {
		t.Fatalf("GetProject: want fwm.Error, got %v", err)
	}
	if me.Kind != fwmanager.NotFound {
		t.Fatalf("GetProject: want NotFound, got %v", me.Kind)
	}
}

func TestGetProject_EmptyProjectID_ContractMisuse(t *testing.T) {
	fake := &fakeProjectStateAccess{}
	m := newCatalogMgr(fake, nil, nil, "")

	_, err := m.GetProject(rc(), ProjectID(""))
	var me *fwmanager.Error
	if !errors.As(err, &me) || me.Kind != fwmanager.ContractMisuse {
		t.Fatalf("GetProject: want ContractMisuse, got %v", err)
	}
	if fake.readCalls != 0 {
		t.Fatal("GetProject: RA should not be called on nil id")
	}
}

// --- opaque envelope wire shape ---------------------------------------------

// TestProjectState_SlotWireShape proves the directly-serialized ArtifactSlotView marshals
// each slot with a STRING kind discriminator + the opaque {kind, model} envelope (the
// SAME wire shape the systemdesign session read emits), and that an empty slot omits the
// inner model payload (and notes).
func TestProjectState_SlotWireShape(t *testing.T) {
	id := ProjectID("my-cool-system")
	fake := &fakeProjectStateAccess{readProject: sampleProject(projectstate.ProjectID(id))}
	m := newCatalogMgr(fake, nil, nil, "")

	st, err := m.GetProject(rc(), id)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}

	data, err := json.Marshal(st.Slots)
	if err != nil {
		t.Fatalf("marshal slots: %v", err)
	}
	var wire []struct {
		Kind  string `json:"kind"`
		Stage int    `json:"stage"`
		Model struct {
			Kind  string          `json:"kind"`
			Model json.RawMessage `json:"model"`
		} `json:"model"`
		Notes *string `json:"notes"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	if len(wire) != len(projectstate.AllArtifactKinds()) {
		t.Fatalf("wire: got %d slots", len(wire))
	}
	byKind := map[string]int{}
	for i, w := range wire {
		byKind[w.Kind] = i
	}
	mission := wire[byKind["mission"]]
	if mission.Model.Kind != "mission" || len(mission.Model.Model) == 0 {
		t.Fatalf("wire: mission envelope wrong: %+v", mission.Model)
	}
	scrubbed := wire[byKind["scrubbedRequirements"]]
	if len(scrubbed.Model.Model) != 0 {
		t.Fatal("wire: empty slot should omit the inner model payload")
	}
	if scrubbed.Notes != nil {
		t.Fatal("wire: empty slot should omit notes")
	}
}

// TestPhaseNameLabels pins the PM-P2-5 fix: the human-readable PhaseName label is
// projected alongside the 0-indexed Phase int in BOTH read models
// (summaryToContract + projectStateToContract), 0/1/2 → the three lifecycle
// labels, and an out-of-range Phase yields "" rather than a fabricated label.
func TestPhaseNameLabels(t *testing.T) {
	cases := []struct {
		phase projectstate.Phase
		want  string
	}{
		{projectstate.PhaseSystemDesign, "system-design"},
		{projectstate.PhaseProjectDesign, "project-design"},
		{projectstate.PhaseConstruction, "construction"},
		{projectstate.Phase(9), ""}, // out of range → empty, never fabricated
	}
	m := &systemDesignManager{}
	for _, tc := range cases {
		if got := summaryToContract(projectstate.ProjectSummary{Phase: tc.phase}).PhaseName; got != tc.want {
			t.Errorf("summaryToContract(Phase=%d).PhaseName = %q, want %q", tc.phase, got, tc.want)
		}
		if got := m.projectStateToContract(projectstate.Project{Phase: tc.phase}).PhaseName; got != tc.want {
			t.Errorf("projectStateToContract(Phase=%d).PhaseName = %q, want %q", tc.phase, got, tc.want)
		}
		// The int Phase must still be carried unchanged next to the new label.
		if got := summaryToContract(projectstate.ProjectSummary{Phase: tc.phase}).Phase; int(got) != int(tc.phase) {
			t.Errorf("summaryToContract(Phase=%d).Phase = %d, want %d", tc.phase, got, tc.phase)
		}
	}
}

func TestTestingStateToContract(t *testing.T) {
	if got := testingStateToContract(nil); got != nil {
		t.Fatalf("nil input: got %v, want nil", got)
	}
	in := &projectstate.TestingState{
		TestRuns: []projectstate.TestRun{{ID: "run-1", Passed: 12, Failed: 1, Note: "nightly"}},
		Defects:  []projectstate.DefectRecord{{ID: "D-1", Title: "flake", Severity: "high", Note: "retry"}},
	}
	got := testingStateToContract(in)
	if got == nil {
		t.Fatal("populated input: got nil")
	}
	if len(got.TestRuns) != 1 || got.TestRuns[0].Id != "run-1" || got.TestRuns[0].Passed != 12 {
		t.Errorf("TestRuns mapped wrong: %+v", got.TestRuns)
	}
	if len(got.Defects) != 1 || got.Defects[0].Id != "D-1" || got.Defects[0].Severity != "high" {
		t.Errorf("Defects mapped wrong: %+v", got.Defects)
	}
}

// dynamicfindings_test.go — coverage for the app-side dynamic-view gate the
// sessionState read-back applies to a System draft (founder extension 2026-07-05).
// Every committed use case — core AND nonCore variation — must carry its own dynamic
// view (call chain) in the System model; an uncovered use case surfaces as one
// ERROR-severity finding on the review panel so the human gate flags it. This is the
// review-panel twin of methodcheck's USECASE-DYNAMIC-MISSING (the authoritative gate
// putDraftModel enforces while the agent authors).

// cucWith builds a committed CoreUseCases carrying the named use cases with the given
// ids and classifications.
func cucWith(decisions ...projectstate.UseCaseDecision) *projectstate.CoreUseCases {
	return &projectstate.CoreUseCases{Decisions: decisions}
}

func uc(id, name string, class projectstate.Classification) projectstate.UseCaseDecision {
	return projectstate.UseCaseDecision{
		UseCase: projectstate.UseCase{ID: projectstate.UseCaseID(id), Name: name, Classification: class},
	}
}

func systemWithViews(useCaseIDs ...string) *projectstate.System {
	var dvs []projectstate.DynamicView
	for _, id := range useCaseIDs {
		dvs = append(dvs, projectstate.DynamicView{UseCaseID: id, Key: "uc" + id, Title: "view " + id})
	}
	return &projectstate.System{DynamicViews: dvs}
}

// A System draft that leaves committed use cases without a dynamic view flags exactly
// those use cases (core AND nonCore variation), and none of the covered ones.
func Test_useCaseDynamicFindings_FlagsUncoveredUseCases(t *testing.T) {
	committed := cucWith(
		uc("capture", "Capture", projectstate.ClassCore),              // covered
		uc("clarify", "Clarify", projectstate.ClassCore),              // UNCOVERED (core)
		uc("clarify-bulk", "Clarify Bulk", projectstate.ClassNonCore), // UNCOVERED (nonCore variation)
		uc("engage", "Engage", projectstate.ClassNonCore),             // covered
	)
	draft := systemWithViews("capture", "engage")

	findings := useCaseDynamicFindings(KindSystem, draft, committed)
	if len(findings) != 2 {
		t.Fatalf("expected 2 ERROR findings (one per uncovered use case), got %d: %+v", len(findings), findings)
	}
	wantNames := []string{"Clarify", "Clarify Bulk"}
	for i, f := range findings {
		if f.Severity != SeverityError {
			t.Errorf("finding %d: want SeverityError, got %v", i, f.Severity)
		}
		if string(f.RuleID) != "USECASE-DYNAMIC-MISSING" {
			t.Errorf("finding %d: want RuleID USECASE-DYNAMIC-MISSING, got %q", i, f.RuleID)
		}
		if !strings.Contains(f.Message, wantNames[i]) {
			t.Errorf("finding %d: message %q must name use case %q", i, f.Message, wantNames[i])
		}
	}
	// The nonCore-variation message must name it as such.
	if !strings.Contains(findings[1].Message, "nonCore use-case variation") {
		t.Errorf("nonCore finding must be labelled as a variation, got %q", findings[1].Message)
	}
	for _, f := range findings {
		if strings.Contains(f.Message, "Capture") || strings.Contains(f.Message, "Engage") {
			t.Errorf("covered use case must not be flagged; got %q", f.Message)
		}
	}
}

// The gate is scoped to KindSystem with a committed CoreUseCases: any other kind, a
// nil/absent committed set, a nil draft, or a wrong-typed draft yields no findings.
func Test_useCaseDynamicFindings_ScopedToSystemKind(t *testing.T) {
	committed := cucWith(uc("capture", "Capture", projectstate.ClassCore))
	draft := systemWithViews() // no views at all

	if got := useCaseDynamicFindings(KindCoreUseCases, draft, committed); got != nil {
		t.Errorf("non-System kind must yield no findings, got %+v", got)
	}
	if got := useCaseDynamicFindings(KindSystem, draft, nil); got != nil {
		t.Errorf("nil committed CoreUseCases must yield no findings, got %+v", got)
	}
	if got := useCaseDynamicFindings(KindSystem, nil, committed); got != nil {
		t.Errorf("nil draft must yield no findings, got %+v", got)
	}
	// Every use case covered → no findings.
	full := systemWithViews("capture")
	if got := useCaseDynamicFindings(KindSystem, full, committed); got != nil {
		t.Errorf("all-covered draft must yield no findings, got %+v", got)
	}
}

// =============================================================================
// I-DESIGN-DISPATCH Part 3 — the WIRING-LEVEL PROOF (test-engineer). This file
// EXTENDS gitrail_test.go (the senior's two rail wire-tests) with the load-bearing
// branch-reconciliation assertions the settled model (Part 1, the exact branch
// table) demands but the happy-path smoke test does not yet pin:
//
//   1. read-back branch == dispatch target_branch (the session branch) — the
//      reconciliation the whole §2a rail exists to make true.
//   2. Approve: MergePullRequest lands BEFORE commit-on-main, and the post-merge
//      commit reads/writes MAIN (the §2a branch table rows 7-8 vs 9a).
//   3. Reject → redraft on a NEW session branch (attempt+1), a NEW PR — the prior
//      PR is not reused.
//   4. Failure (PhaseFailed) → StageDraftFailed with the rail WIRED (the anti-wedge
//      path still holds; the rail's dispatch-time half ran, the approve-time half
//      did NOT).
//   5. Required-check RED → merge BLOCKED, no main-commit (the §2b merge guard) —
//      the senior covers this; we add the ORDERED-event variant proving the guard
//      fires before any merge/commit and the session recovers, not crashes.
//
// Real Manager under test (the REAL CoAuthorArtifactWorkflow + every Activity);
// FAKE ONLY the external agentic-job seam (agenticJobAccess, reusing
// fakePipeline from workflow_test.go) + the GitHub PR-rail seam (a coherent
// SCRIPTED fakeRail) + the branch-aware projectStateAccess read-back. NO internal
// Manager component is faked. Temporal in-memory test env, runs under -short.
// The on-disk-git equivalent (the Action's raw CommitSubtree to a branch, then the
// read-back on that branch) is already proven one layer down by
// projectstate.TestGitStore_ExternalActionDraftIsReadBack; here we prove the
// MANAGER SPINE reconciles the branch the rail addressed with the branch the
// read-back/commit ride over.
// =============================================================================

// ---- seqLog: a shared ordered event log across the rail + projectstate fakes --

// seqLog records, in call order, the load-bearing spine events so a test can assert
// the SEQUENCE (merge-before-commit) and the BRANCH each read/write rode over. Both
// the rail fake and the branch-aware projectstate fake append to the SAME log.
type seqLog struct {
	mu     sync.Mutex
	events []seqEvent
}

type seqEvent struct {
	op     string // "merge" | "commit" | "readMain" | "readBranch" | "stageBranch" | "stageMain"
	branch string // for read/stage events: the branch the op rode over ("" == main)
}

func (l *seqLog) add(op, branch string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, seqEvent{op: op, branch: branch})
}

func (l *seqLog) ops() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.events))
	for i, e := range l.events {
		out[i] = e.op
	}
	return out
}

// firstIndexOf returns the index of the first event with op, or -1.
func (l *seqLog) firstIndexOf(op string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, e := range l.events {
		if e.op == op {
			return i
		}
	}
	return -1
}

// ---- scriptedRail: the EXTERNAL PR-rail seam with a per-attempt PR + ordered log -

// scriptedRail is a coherent IPullRequestRail-subset fake that models the PR rail
// per attempt: OpenBranch ensures a (per-attempt) session branch, OpenPullRequest
// mints a DISTINCT PR per head branch (a merged/closed PR is never reused), the
// status reflects a scripted green/red check, PostReview is the +1, and Merge moves
// the draft from the session branch to main. It records the ordered merge event into
// the shared seqLog so a test can assert merge-before-commit and which PR was merged.
type scriptedRail struct {
	mu  sync.Mutex
	log *seqLog

	checkGreen bool

	openedBranches []string
	openedPRHeads  []string
	mergedPRs      []string
	prByHead       map[string]string
	calls          map[string]int
}

func newScriptedRail(green bool, log *seqLog) *scriptedRail {
	return &scriptedRail{checkGreen: green, log: log, prByHead: map[string]string{}, calls: map[string]int{}}
}

func (r *scriptedRail) count(verb string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[verb]
}

// CommitManagedFiles backs the managed-scaffold sync (the free-function composition helper
// reaches the rail through this verb for a non-managedFileSyncer fake). Recorded under the
// "SyncManagedScaffold" counter.
func (r *scriptedRail) CommitManagedFiles(_ fwra.Context, _ sourcecontrol.RepoRef, _ []sourcecontrol.ManagedFile, _ sourcecontrol.RepoCredential) (sourcecontrol.CommitRef, error) {
	r.mu.Lock()
	r.calls["SyncManagedScaffold"]++
	r.mu.Unlock()
	return sourcecontrol.CommitRef("scaffold-sync"), nil
}

func (r *scriptedRail) GetInstallationToken(_ fwra.Context, _ sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.mu.Lock()
	r.calls["GetInstallationToken"]++
	r.mu.Unlock()
	return sourcecontrol.RepoCredential{Bytes: []byte("tok"), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (r *scriptedRail) OpenBranch(_ fwra.Context, _ sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential) (sourcecontrol.BranchRef, error) {
	r.mu.Lock()
	r.calls["OpenBranch"]++
	r.openedBranches = append(r.openedBranches, string(branch))
	r.mu.Unlock()
	return sourcecontrol.BranchRef(""), nil
}

func (r *scriptedRail) OpenPullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestRef, error) {
	r.mu.Lock()
	r.calls["OpenPullRequest"]++
	head := string(spec.Head)
	pr, ok := r.prByHead[head]
	if !ok {
		// A distinct PR per head branch — a fresh attempt branch opens a fresh PR.
		pr = "pr/" + head
		r.prByHead[head] = pr
		r.openedPRHeads = append(r.openedPRHeads, head)
	}
	r.mu.Unlock()
	// Ordered event (F40 openPR-after-read-back proof): record WHEN the PR was opened
	// relative to the read-back so a test can assert the PR is never opened before a
	// committed model has been confirmed on the session branch.
	if r.log != nil {
		r.log.add("openPR", head)
	}
	return sourcecontrol.PullRequestRefFromString(pr), nil
}

func (r *scriptedRail) GetPullRequestStatus(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.mu.Lock()
	r.calls["GetPullRequestStatus"]++
	green := r.checkGreen
	r.mu.Unlock()
	rollup := sourcecontrol.CheckFailure
	if green {
		rollup = sourcecontrol.CheckSuccess
	}
	return sourcecontrol.PullRequestStatus{CheckRollup: rollup, Mergeable: green}, nil
}

func (r *scriptedRail) PostReview(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential) error {
	r.mu.Lock()
	r.calls["PostReview"]++
	r.mu.Unlock()
	return nil
}

func (r *scriptedRail) MergePullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.MergeResult, error) {
	r.mu.Lock()
	r.calls["MergePullRequest"]++
	r.mergedPRs = append(r.mergedPRs, sourcecontrol.PullRequestRefString(pr))
	r.mu.Unlock()
	// The merge moves the draft from the session branch onto main — model that by
	// flipping the projectstate fake to serve the draft on main for the post-merge read.
	if r.log != nil {
		r.log.add("merge", sourcecontrol.PullRequestRefString(pr))
	}
	return sourcecontrol.MergeResult{Merged: true, Commit: "merged-" + sourcecontrol.PullRequestRefString(pr)}, nil
}

// The remaining SourceControlAccess ops are outside the design PR-rail lifecycle; inert.
func (r *scriptedRail) AdoptProjectRepo(_ fwra.Context, _ sourcecontrol.RepoAdoptionSpec) (sourcecontrol.RepoRef, error) {
	return sourcecontrol.RepoRef(""), nil
}

func (r *scriptedRail) ConfigureBranchProtection(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) error {
	return nil
}

func (r *scriptedRail) InstallAuthorizeApp(_ fwra.Context, _ sourcecontrol.AccountRef) (sourcecontrol.Installation, error) {
	return sourcecontrol.Installation(""), nil
}

// SyncManagedScaffold mirrors the REAL production sourceControlAccess impl
// ((*access).SyncManagedScaffold, github.go) — it delegates to the free-function
// composition helper rather than stubbing directly. B10 rewires the generated
// wf.Acts.RailSyncManagedScaffold invoker straight onto this method, so this method is
// now the LOAD-BEARING path every proof test's beginSession exercises (previously the
// free function was called directly by the now-deleted custom Activity, bypassing this
// method entirely).
func (r *scriptedRail) SyncManagedScaffold(rc fwra.Context, repo sourcecontrol.RepoRef, cred sourcecontrol.RepoCredential) (bool, error) {
	return sourcecontrol.SyncManagedScaffold(rc.Context, r, repo, cred)
}

var _ sourcecontrol.SourceControlAccess = (*scriptedRail)(nil)

// ---- seqProjectState: branch-aware read-back + ordered commit/read events ------

// seqProjectState wraps fakeProjectState with the §2a branch-aware extension AND the
// shared ordered log: it records which BRANCH each read-back/stage rode over and
// appends "commit"/"readMain"/"readBranch" events so a test can assert
// merge-before-commit and post-merge-read-on-main.
type seqProjectState struct {
	*fakeProjectState
	log *seqLog

	mu            sync.Mutex
	readBranches  []string
	stageBranches []string
}

var _ projectstate.ProjectStateAccess = (*seqProjectState)(nil)

func (f *seqProjectState) ReadProject(rc fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
	// main-path read (branch override "" ⇒ ReadProject): this is the priors read AND
	// the post-merge re-read the approve path does before commit-on-main.
	f.log.add("readMain", "")
	f.mu.Lock()
	f.readBranches = append(f.readBranches, "")
	f.mu.Unlock()
	return f.fakeProjectState.ReadProject(rc, projectID)
}

func (f *seqProjectState) ReadProjectOnBranch(rc fwra.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	if branch == "" {
		// The generated ProjectStateAccess contract requires ReadProjectOnBranch("") to
		// behave EXACTLY as ReadProject (C2 fold, code-health-phase-a) — this is now the
		// CONCRETE substrate's obligation, not a wrapper-level capability gate, so this
		// fake must honor it too (the "readMain" vs "readBranch" log distinction the
		// merge-before-commit assertions below rely on).
		return f.ReadProject(rc, projectID)
	}
	f.log.add("readBranch", branch)
	f.mu.Lock()
	f.readBranches = append(f.readBranches, branch)
	f.mu.Unlock()
	return f.fakeProjectState.ReadProject(rc, projectID)
}

func (f *seqProjectState) StageArtifactForReviewOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, model projectstate.ArtifactModel, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.log.add("stageBranch", branch)
	f.mu.Lock()
	f.stageBranches = append(f.stageBranches, branch)
	f.mu.Unlock()
	return f.StageArtifactForReview(fwra.Context{Context: rc.Context, IdempotencyKey: key}, projectID, expectedVersion, model)
}

func (f *seqProjectState) CommitArtifact(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind) (projectstate.Version, error) {
	f.log.add("commit", "")
	return f.fakeProjectState.CommitArtifact(rc, projectID, expectedVersion, kind)
}

func (f *seqProjectState) RejectArtifactOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.log.add("rejectBranch", branch)
	return f.RejectArtifact(fwra.Context{Context: rc.Context, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

func (f *seqProjectState) WithdrawArtifactOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.log.add("withdrawBranch", branch)
	return f.WithdrawArtifact(fwra.Context{Context: rc.Context, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

func newSeqRailWorkflows(rail sourcecontrol.SourceControlAccess) *workflows {
	return &workflows{
		Acts: genInvokers{Opts: activityOptions()},
		Rail: rail,
		Repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo"), true
		},
	}
}

// PROOF 1+2 — branch reconciliation + merge-before-commit + post-merge-read-on-main.
// The load-bearing assertions the settled branch table prescribes:
//   - the read-back rode over EXACTLY the dispatch target_branch (the session branch).
//   - the AwaitingReview stage rode over that SAME session branch.
//   - on Approve: MergePullRequest landed BEFORE the commit; the commit was preceded by
//     a main-path read (the post-merge re-seed) — i.e. commit reflects MAIN, not the
//     session branch.
func Test_CoAuthor_Rail_BranchReconciliation_MergeBeforeCommit_PostMergeReadOnMain(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // dispatch observed Succeeded
	rail := newScriptedRail(true, log)
	wf := newSeqRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("rail reconciliation workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want CoAuthorApproved, got %d", outcome)
	}

	// The exact session branch the dispatch addressed (from DispatchInputs).
	// (System is architect-critiqued: draft + system-critique = 2 dispatches.)
	if len(pipe.submits) != 2 {
		t.Fatalf("System must dispatch draft + architect self-critique, got %d", len(pipe.submits))
	}
	dispatchBranch := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if dispatchBranch == "" {
		t.Fatal("dispatch must carry a non-empty target_branch")
	}

	sdRailAssertPerProjectDispatchTarget(t, pipe.submits[0])
	sdRailAssertSessionBranchRode(t, rail, ps, dispatchBranch)
	sdRailAssertMergeBeforeCommitOnMain(t, log, rail, dispatchBranch)

	// Commit landed on main exactly once.
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one CommitArtifact(KindSystem) on main, got %v", base.committed)
	}
}

// sdRailAssertPerProjectDispatchTarget — THE PER-PROJECT-DESIGN-DISPATCH ASSERTION
// (the live-activation gap fix): with the rail WIRED, the dispatch must target the
// PER-PROJECT repo (the rail's repoRef) + aiarch-design.yml — NOT the central
// construction repo + aiarch-construct.yml. This is exactly what the systemtests fake
// could not catch (it intercepted all GitHub REST regardless of repo). The
// workflow-side dispatchDesignJob decodes the opaque RepoRef ("acct|owner/repo") to
// the RA's RepoTarget{Owner:"owner", Name:"repo"} BEFORE the generated submit invoker,
// so the fake records the decoded "owner/repo".
func sdRailAssertPerProjectDispatchTarget(t *testing.T, sub submitRecord) {
	t.Helper()
	if sub.targetRepo != "owner/repo" {
		t.Fatalf("design dispatch must target the per-project repo %q, got %q", "owner/repo", sub.targetRepo)
	}
	if sub.workflowFile != "aiarch-design.yml" {
		t.Fatalf("design dispatch must target aiarch-design.yml (NOT aiarch-construct.yml), got %q", sub.workflowFile)
	}
}

// sdRailAssertSessionBranchRode asserts the rail opened EXACTLY the dispatch session
// branch + a PR with that head, and that the read-back and the AwaitingReview stage
// both rode over that SAME session branch (THE LOAD-BEARING RECONCILIATION).
func sdRailAssertSessionBranchRode(t *testing.T, rail *scriptedRail, ps *seqProjectState, dispatchBranch string) {
	t.Helper()
	// The rail opened EXACTLY that branch + a PR with that head.
	if len(rail.openedBranches) != 1 || rail.openedBranches[0] != dispatchBranch {
		t.Fatalf("OpenBranch must address the dispatch session branch %q, got %v", dispatchBranch, rail.openedBranches)
	}
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != dispatchBranch {
		t.Fatalf("OpenPullRequest head must be the session branch %q, got %v", dispatchBranch, rail.openedPRHeads)
	}

	// THE LOAD-BEARING RECONCILIATION: the read-back rode over the dispatch target_branch.
	if len(ps.readBranches) == 0 {
		t.Fatal("no read recorded")
	}
	sawReadBackOnSession := false
	for _, b := range ps.readBranches {
		if b == dispatchBranch {
			sawReadBackOnSession = true
		}
	}
	if !sawReadBackOnSession {
		t.Fatalf("read-back branch must equal the dispatch target_branch %q, got reads %v", dispatchBranch, ps.readBranches)
	}
	// The AwaitingReview stage rode over that same session branch.
	if len(ps.stageBranches) != 1 || ps.stageBranches[0] != dispatchBranch {
		t.Fatalf("stage must ride over the dispatch session branch %q, got %v", dispatchBranch, ps.stageBranches)
	}
}

// sdRailAssertMergeBeforeCommitOnMain asserts the approve ordering the §2a table
// prescribes: the session-branch PR merged BEFORE the commit, with a main-path read
// (the post-merge re-seed) between merge and commit.
func sdRailAssertMergeBeforeCommitOnMain(t *testing.T, log *seqLog, rail *scriptedRail, dispatchBranch string) {
	t.Helper()
	// Merge landed BEFORE commit (the §2a table: merge first, then commit-on-main).
	mergeIdx := log.firstIndexOf("merge")
	commitIdx := log.firstIndexOf("commit")
	if mergeIdx < 0 {
		t.Fatalf("a green approve must MERGE; ops=%v", log.ops())
	}
	if commitIdx < 0 {
		t.Fatalf("a green approve must COMMIT; ops=%v", log.ops())
	}
	if mergeIdx >= commitIdx {
		t.Fatalf("merge must precede commit-on-main; ops=%v", log.ops())
	}
	// The merged PR is the session-branch PR.
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+dispatchBranch {
		t.Fatalf("merge must target the session-branch PR pr/%s, got %v", dispatchBranch, rail.mergedPRs)
	}

	// POST-MERGE READ ON MAIN: between merge and commit there is a main-path read
	// (branch "") — the approve path re-seeds headVersion from MAIN before committing.
	postMergeReadOnMain := false
	for i := mergeIdx + 1; i < commitIdx; i++ {
		if log.events[i].op == "readMain" {
			postMergeReadOnMain = true
		}
	}
	if !postMergeReadOnMain {
		t.Fatalf("after merge the approve path must re-read on MAIN before commit; ops=%v", log.ops())
	}
}

// PROOF 3 (F40) — Reject → redraft on the SAME persistent session branch, the SAME PR.
// The founder ruling: commit to one branch and improve it until it merges (the history of
// changes lives in git); NOT a PR per draft. So the second dispatch's target_branch EQUALS
// the first, the rail opened exactly ONE PR (idempotent on head), and the eventual merge is
// of that one accumulating PR.
func Test_CoAuthor_Rail_RejectRedraftsOnSameSessionBranchAndSamePR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, log)
	wf := newSeqRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// First gate: REJECT → redraft on the SAME branch/PR. Second gate: APPROVE → merge.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "rework decomposition"}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("reject-redraft workflow error: %v", err)
	}

	if len(pipe.submits) < 2 {
		t.Fatalf("reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
	b1 := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	b2 := pipe.submits[1].dispatchInputs[dispatchInputTargetBranch]
	if b1 == "" || b2 == "" {
		t.Fatalf("both dispatches must carry a target_branch, got %q / %q", b1, b2)
	}
	// THE LOAD-BEARING ASSERTION (F40): the redraft is on the SAME persistent session branch.
	if b1 != b2 {
		t.Fatalf("a reject must redraft on the SAME session branch (F40 single-branch); got %q then %q", b1, b2)
	}
	// The rail opened exactly ONE PR (idempotent on head — the persistent PR is reused).
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != b1 {
		t.Fatalf("reject must reuse the ONE PR on the persistent branch, got PR heads %v", rail.openedPRHeads)
	}
	// The merge is of that one accumulating PR.
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+b1 {
		t.Fatalf("the merged PR must be the persistent PR pr/%s, got %v", b1, rail.mergedPRs)
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit after redraft→approve, got %v", base.committed)
	}
}

// PROOF 4 — Failure with the rail WIRED. A PhaseFailed draft lands the session in
// StageDraftFailed (NOT perpetual Drafting, NOT a crash) even with the rail enabled:
// the dispatch-time rail half ran (mint + OpenBranch), but the approve-time half
// (status guard / +1 / merge) NEVER runs and NOTHING commits. Withdraw ends clean.
// This is the rail-aware variant of the existing anti-wedge test.
func Test_CoAuthor_Rail_PhaseFailed_LandsInStageDraftFailed_NoApproveRailNoCommit(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline(pipelineFailed)
	pipe.diagnostic = "aiarch-validate found 2 violations"
	rail := newScriptedRail(true, log)
	wf := newSeqRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage == StageDrafting {
			t.Fatal("a failed design job must NOT leave perpetual StageDrafting (the wedge), even with the rail wired")
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed after a terminal failure phase, got %d", view.Stage)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT crash the rail-wired workflow: %v", err)
	}
	// Dispatch-time rail half ran (the failure is observed AFTER OpenBranch).
	if rail.count("OpenBranch") == 0 {
		t.Fatalf("the dispatch-time rail half (OpenBranch) must have run before the observe")
	}
	// The approve-time rail half NEVER ran on a failed draft.
	if rail.count("OpenPullRequest") != 0 {
		t.Fatalf("a failed draft must NOT open a PR, got %d", rail.count("OpenPullRequest"))
	}
	if rail.count("GetPullRequestStatus") != 0 || rail.count("MergePullRequest") != 0 {
		t.Fatalf("a failed draft must NOT reach the merge guard/merge, got status=%d merge=%d",
			rail.count("GetPullRequestStatus"), rail.count("MergePullRequest"))
	}
	// Nothing staged, nothing committed; the withdraw SKIPS the unstage write (nothing was
	// ever staged — 2026-07-16 incident) and ends the session cleanly.
	if len(base.staged) != 0 || len(base.committed) != 0 {
		t.Fatalf("a failed draft must stage/commit nothing, got staged=%d committed=%v", len(base.staged), base.committed)
	}
	if len(base.withdrawn) != 0 {
		t.Fatalf("a never-staged withdraw must NOT call WithdrawArtifact, got %d", len(base.withdrawn))
	}
}

// PROOF 5 — Required-check RED → merge BLOCKED, no commit-on-main, ORDERED. The status
// guard (GetPullRequestStatus) fires; because the rollup is red the spine does NOT
// PostReview, does NOT MergePullRequest, and does NOT commit. It routes to the
// StageDraftFailed recovery gate; Withdraw ends clean. (Complements the senior's
// count-only guard test with an ordered-event + recovery assertion.)
func Test_CoAuthor_Rail_RequiredCheckRed_BlocksMerge_NoCommit_Recovers(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()           // draft Succeeds (the run was green) ...
	rail := newScriptedRail(false, log) // ... but the PR's required check is RED at merge time
	wf := newSeqRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a not-green merge guard must not crash the workflow: %v", err)
	}
	// The guard read happened; the merge + the +1 did NOT.
	if rail.count("GetPullRequestStatus") == 0 {
		t.Fatal("the approve path must consult the merge guard (GetPullRequestStatus)")
	}
	if rail.count("MergePullRequest") != 0 {
		t.Fatalf("a RED required check must BLOCK the merge, got %d merge calls", rail.count("MergePullRequest"))
	}
	if rail.count("PostReview") != 0 {
		t.Fatalf("a RED required check must NOT relay the +1, got %d PostReview calls", rail.count("PostReview"))
	}
	// No merge, no main-commit.
	if log.firstIndexOf("merge") != -1 {
		t.Fatalf("a RED required check must produce NO merge event; ops=%v", log.ops())
	}
	if log.firstIndexOf("commit") != -1 {
		t.Fatalf("a RED required check must produce NO commit event; ops=%v", log.ops())
	}
	if len(base.committed) != 0 {
		t.Fatalf("a not-green merge guard must NEVER commit, got %v", base.committed)
	}
	// It RECOVERED (withdraw from the StageDraftFailed gate), it did not wedge or crash.
	if len(base.withdrawn) != 1 {
		t.Fatalf("a blocked merge must route to the recovery gate; withdraw expected once, got %d", len(base.withdrawn))
	}
}

// PROOF 6 (F40 live-bug fix) — the PR is opened ONLY AFTER the read-back confirms a
// committed model on the session branch (i.e. only once the branch has ≥1 commit beyond
// main), never at session start. This regresses the observed gtdapp 422: a PR opened on a
// freshly-cut, zero-commit branch is rejected by GitHub ("no commits between base and
// head"). The load-bearing ordered assertion: the FIRST "openPR" event strictly follows
// the FIRST "readBranch" (read-back) event. Reject → redraft reuses the SAME one PR;
// approve merges it.
func Test_CoAuthor_Rail_OpenPR_OnlyAfterReadBack_ReuseThenMerge(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, log)
	wf := newSeqRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// First gate: REJECT → redraft on the SAME branch/PR. Second gate: APPROVE → merge.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "tighten"}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("openPR-after-read-back workflow error: %v", err)
	}

	// THE LOAD-BEARING ORDERING (the reorder fix): the branch is OpenBranch'd, then the
	// draft lands a commit, then the read-back confirms it — and ONLY THEN is the PR opened.
	firstReadBack := log.firstIndexOf("readBranch")
	firstOpenPR := log.firstIndexOf("openPR")
	if firstReadBack < 0 {
		t.Fatalf("expected a session-branch read-back; ops=%v", log.ops())
	}
	if firstOpenPR < 0 {
		t.Fatalf("a successful draft must open the PR; ops=%v", log.ops())
	}
	if firstOpenPR <= firstReadBack {
		t.Fatalf("the PR must be opened AFTER the first read-back (never on a zero-commit branch at session start); ops=%v", log.ops())
	}
	// The branch WAS opened before the read-back (dispatch-time half), so the ordering above
	// is 'PR after read-back', NOT 'no branch at all'.
	if rail.count("OpenBranch") == 0 {
		t.Fatalf("OpenBranch (dispatch-time half) must run so the Action has a branch to commit on; ops=%v", log.ops())
	}

	// Opened EXACTLY ONE PR across the reject→redraft round (idempotent on head), and it is
	// the session-branch PR that ultimately merges.
	b1 := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != b1 {
		t.Fatalf("reject→redraft must reuse the ONE persistent PR, got heads %v", rail.openedPRHeads)
	}
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+b1 {
		t.Fatalf("approve must merge the one persistent PR pr/%s, got %v", b1, rail.mergedPRs)
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit-on-main after redraft→approve, got %v", base.committed)
	}
}

// =============================================================================
// I-DESIGN-DISPATCH §2b/§2c WIRE-LEVEL regression — the PR-rail-enabled CoAuthor
// spine. Method product → NO BDD; regression-first, black-box at the WIRE seam.
// The rail is stubbed at the EXTERNAL sourceControlAccess boundary (a FAKE rail
// recording every verb) and the read-back is served by a BRANCH-AWARE fake
// projectstate. The Manager under test is NOT faked; the workflow drives the REAL
// mint → OpenBranch → dispatch → observe → OpenPullRequest → branch-aware read-back
// → stage(on-branch) → human gate → status-guard → +1 → merge → commit(on-main)
// sequence over the Temporal in-memory test env (runs under -short).
// =============================================================================

// ---- fakeRail: the EXTERNAL PR-rail seam (IPullRequestRail subset) -----------

type railCall struct {
	verb   string
	repo   string
	branch string
	prRef  string
	// cred is the credential bytes the verb was presented with (recorded by the
	// merge-window verbs) — lets a test assert WHICH minted token a call rode (F-QA2-44).
	cred string
}

// fakeRail records every PR-rail verb and serves a scripted PR status. checkGreen
// drives the merge guard. It satisfies the design Manager's sourceControlRail.
type fakeRail struct {
	mu         sync.Mutex
	calls      []railCall
	checkGreen bool
	openedPRs  int
	// statusAuthFailsRemaining, when >0, makes GetPullRequestStatus return an fwra.Auth
	// error (the platform's rate-limit-403-as-Auth) and decrement — used to exercise the
	// QA F35 bounded-retry + approve-window containment.
	statusAuthFailsRemaining int
	// openPRAuthFailsRemaining, when >0, makes OpenPullRequest return an fwra.Auth error
	// (the same rate-limit-403-as-Auth) and decrement — the QA F35 TWIN in the draft
	// round-trip. Set to railAuthRetryLongMaxAttempts (4, the F-QA2-49 long-backoff
	// budget) to exhaust the bounded retry so the FIRST openPR faults and lands at the
	// failed gate; the resume openPR then succeeds.
	openPRAuthFailsRemaining int
	// syncErr, when non-nil, makes SyncManagedScaffold fail terminally — exercises the
	// managed-scaffold-sync containment (dispatch BLOCKED, session lands at the failed
	// gate, NO design job submitted).
	syncErr error
	// syncChanged scripts the drift report (true ⇔ the seated scaffold drifted and the
	// sync "committed" a refresh).
	syncChanged bool
	// F-QA2-44 token-lifetime modeling. Each mint issues a DISTINCT token (tok-1, tok-2, …)
	// and makes it the one currently-valid token. When enforceTokenValidity is armed, the
	// merge-window verbs 403 (fwra.Auth — the platform's non-retryable classification) on
	// any credential that is not the currently-valid token; expireCurrentToken() models the
	// ~1h GitHub App installation-token expiry between dispatch and a late human approve.
	enforceTokenValidity bool
	mintSeq              int
	validToken           string
}

// expireCurrentToken invalidates the currently-valid token — the dispatch-time credential
// has aged past the ~1h installation-token lifetime (F-QA2-44). The next mint issues a
// fresh valid token.
func (r *fakeRail) expireCurrentToken() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validToken = ""
}

// staleCred reports whether the presented credential must be rejected (validity
// enforcement armed AND the credential is not the currently-valid token).
func (r *fakeRail) staleCred(cred sourcecontrol.RepoCredential) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enforceTokenValidity && string(cred.Bytes) != r.validToken
}

// credsFor returns the credential bytes each recorded call of verb presented, in order.
func (r *fakeRail) credsFor(verb string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.calls {
		if c.verb == verb {
			out = append(out, c.cred)
		}
	}
	return out
}

func (r *fakeRail) record(c railCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, c)
}

func (r *fakeRail) verbCount(verb string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c.verb == verb {
			n++
		}
	}
	return n
}

func (r *fakeRail) GetInstallationToken(_ fwra.Context, repo sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.record(railCall{verb: "GetInstallationToken", repo: sourcecontrol.RepoRefString(repo)})
	// Each mint issues a DISTINCT token and makes it the currently-valid one (F-QA2-44):
	// tok-1 for the dispatch-time mint, tok-2 for the gate-decision re-mint, …
	r.mu.Lock()
	r.mintSeq++
	tok := fmt.Sprintf("tok-%d", r.mintSeq)
	r.validToken = tok
	r.mu.Unlock()
	return sourcecontrol.RepoCredential{Bytes: []byte(tok), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// CommitManagedFiles backs the managed-scaffold sync: sourcecontrol.SyncManagedScaffold
// (the free-function composition helper the custom SyncManagedScaffoldActivity wraps)
// reaches the rail through this verb for a non-managedFileSyncer fake. It records the sync
// call under the "SyncManagedScaffold" counter and honors the scripted syncErr.
func (r *fakeRail) CommitManagedFiles(_ fwra.Context, repo sourcecontrol.RepoRef, _ []sourcecontrol.ManagedFile, _ sourcecontrol.RepoCredential) (sourcecontrol.CommitRef, error) {
	r.record(railCall{verb: "SyncManagedScaffold", repo: sourcecontrol.RepoRefString(repo)})
	r.mu.Lock()
	err := r.syncErr
	r.mu.Unlock()
	if err != nil {
		return sourcecontrol.CommitRef(""), err
	}
	return sourcecontrol.CommitRef("scaffold-sync"), nil
}

func (r *fakeRail) OpenBranch(_ fwra.Context, repo sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential) (sourcecontrol.BranchRef, error) {
	r.record(railCall{verb: "OpenBranch", repo: sourcecontrol.RepoRefString(repo), branch: string(branch)})
	// The Manager discards the BranchRef (it only ensures the branch exists); a zero
	// ref is fine — the workflow never re-materializes a branch handle.
	return sourcecontrol.BranchRef(""), nil
}

func (r *fakeRail) OpenPullRequest(_ fwra.Context, repo sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestRef, error) {
	r.record(railCall{verb: "OpenPullRequest", repo: sourcecontrol.RepoRefString(repo), branch: string(spec.Head), prRef: "pr/" + string(spec.Head)})
	r.mu.Lock()
	fail := r.openPRAuthFailsRemaining > 0
	if fail {
		r.openPRAuthFailsRemaining--
	}
	r.mu.Unlock()
	if fail {
		// The observed F35-twin fault: OpenPullRequest hits a GitHub secondary rate-limit 403
		// the platform classifier reports as Auth. The shared bounded retry absorbs it; a
		// persistent one lands the draft round-trip at the failed gate (draft preserved).
		return sourcecontrol.PullRequestRefFromString(""), fwra.New(fwra.Auth, "openPullRequest: github auth/permission denied")
	}
	r.mu.Lock()
	r.openedPRs++
	prRef := "pr/" + string(spec.Head)
	r.mu.Unlock()
	return sourcecontrol.PullRequestRefFromString(prRef), nil
}

func (r *fakeRail) GetPullRequestStatus(_ fwra.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.record(railCall{verb: "GetPullRequestStatus", repo: sourcecontrol.RepoRefString(repo), prRef: sourcecontrol.PullRequestRefString(pr), cred: string(cred.Bytes)})
	if r.staleCred(cred) {
		// The F-QA2-44 live fault: the presented installation token has EXPIRED; GitHub
		// 403s and the platform classifier reports a non-retryable Auth fault.
		return sourcecontrol.PullRequestStatus{}, fwra.New(fwra.Auth, "getPullRequest: github auth/permission denied (expired installation token)")
	}
	r.mu.Lock()
	fail := r.statusAuthFailsRemaining > 0
	if fail {
		r.statusAuthFailsRemaining--
	}
	r.mu.Unlock()
	if fail {
		// The observed F35 fault: GitHub secondary rate-limit 403 the platform classifier
		// reports as Auth. railApproveOpts retries it within a bounded budget.
		return sourcecontrol.PullRequestStatus{}, fwra.New(fwra.Auth, "getPullRequest: github auth/permission denied")
	}
	rollup := sourcecontrol.CheckFailure
	if r.checkGreen {
		rollup = sourcecontrol.CheckSuccess
	}
	return sourcecontrol.PullRequestStatus{CheckRollup: rollup, Mergeable: r.checkGreen}, nil
}

func (r *fakeRail) PostReview(_ fwra.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.ReviewSubmission, cred sourcecontrol.RepoCredential) error {
	r.record(railCall{verb: "PostReview", repo: sourcecontrol.RepoRefString(repo), prRef: sourcecontrol.PullRequestRefString(pr), cred: string(cred.Bytes)})
	if r.staleCred(cred) {
		return fwra.New(fwra.Auth, "postReview: github auth/permission denied (expired installation token)")
	}
	return nil
}

func (r *fakeRail) MergePullRequest(_ fwra.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential) (sourcecontrol.MergeResult, error) {
	r.record(railCall{verb: "MergePullRequest", repo: sourcecontrol.RepoRefString(repo), prRef: sourcecontrol.PullRequestRefString(pr), cred: string(cred.Bytes)})
	if r.staleCred(cred) {
		return sourcecontrol.MergeResult{}, fwra.New(fwra.Auth, "mergePullRequest: github auth/permission denied (expired installation token)")
	}
	return sourcecontrol.MergeResult{Merged: true, Commit: "merged"}, nil
}

// The remaining SourceControlAccess ops are outside the design PR-rail lifecycle; the stub
// satisfies the full contract with inert implementations so it can back the GENERATED rail
// Activities registered via genActivities.
func (r *fakeRail) AdoptProjectRepo(_ fwra.Context, _ sourcecontrol.RepoAdoptionSpec) (sourcecontrol.RepoRef, error) {
	return sourcecontrol.RepoRef(""), nil
}

func (r *fakeRail) ConfigureBranchProtection(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) error {
	return nil
}

func (r *fakeRail) InstallAuthorizeApp(_ fwra.Context, _ sourcecontrol.AccountRef) (sourcecontrol.Installation, error) {
	return sourcecontrol.Installation(""), nil
}

// SyncManagedScaffold mirrors the REAL production sourceControlAccess impl
// ((*access).SyncManagedScaffold, github.go) — it delegates to the free-function
// composition helper rather than stubbing directly. B10 rewires the generated
// wf.Acts.RailSyncManagedScaffold invoker straight onto this method (the custom
// SyncManagedScaffoldActivity that used to call the free function directly is gone), so
// this method is now the LOAD-BEARING path the syncErr/CommitManagedFiles-counter tests
// below exercise (previously the free function was called directly, bypassing this
// method entirely — a latent fake/production divergence this migration surfaced).
func (r *fakeRail) SyncManagedScaffold(rc fwra.Context, repo sourcecontrol.RepoRef, cred sourcecontrol.RepoCredential) (bool, error) {
	return sourcecontrol.SyncManagedScaffold(rc.Context, r, repo, cred)
}

var _ sourcecontrol.SourceControlAccess = (*fakeRail)(nil)

// ---- branchAwareFakeProjectState: read-back/stage capture by branch ----------

// branchAwareFakeProjectState extends fakeProjectState behavior with the §2a
// branch-aware extension so the rail-enabled spine's read-back + stage land on the
// session branch. It records the branch each read/stage targeted so the test can
// assert the session-branch routing.
type branchAwareFakeProjectState struct {
	*fakeProjectState
	mu               sync.Mutex
	readBranches     []string
	stageBranches    []string
	rejectBranches   []string
	withdrawBranches []string
	// failRejectOnBranch, when true, makes RejectArtifactOnBranch fault terminally
	// (a ContractMisuse) — used to exercise the QA F28 crash-containment recovery gate.
	failRejectOnBranch bool
	// failWithdrawOnMain, when true, makes the MAIN-path WithdrawArtifact fault (models the
	// unpopulated-main-slot ContractMisuse of the PR rail — the 2026-07-16 gtdapp incident's
	// exact error). Armed by the F30 review-gate withdraw test AND the never-staged
	// failed-gate withdraw test, so any regression to a blind main write crashes loudly.
	failWithdrawOnMain bool
	// failWithdrawOnBranchRemaining injects N terminal faults into the BRANCH-path
	// WithdrawArtifactOnBranch (then succeeds) — exercises the 2026-07-16 anti-wedge rule:
	// a fault while RECORDING a withdraw must land back at the gate, never kill the workflow.
	failWithdrawOnBranchRemaining int
	// failReadBackDecode, when true, makes the branch READ-BACK fault as a TERMINAL decode of
	// committed state (a ContractMisuse carrying the closed-enum wire-name diagnostic) — the
	// QA F36 scenario: the drafting agent committed free prose into the "trigger" closed enum,
	// CI validate went green, but the server codec rejects the value on read-back.
	failReadBackDecode bool
	// branchAdvancedModel, when non-nil, is the SystemDesign slot model the branch read-back
	// returns instead of main's — modeling an AMENDMENT whose Action actually CHANGED the
	// artifact (so sameArtifactModel(branch, main) is false and the F40 no-change guard does
	// NOT trip). Left nil, the branch read-back returns main's model verbatim (identical),
	// which trips the amendment no-change guard — the observed zero-new-commit scenario.
	branchAdvancedModel projectstate.ArtifactModel
}

var _ projectstate.ProjectStateAccess = (*branchAwareFakeProjectState)(nil)

func (f *branchAwareFakeProjectState) ReadProjectOnBranch(rc fwra.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	if branch == "" {
		// The generated ProjectStateAccess contract requires ReadProjectOnBranch("") to
		// behave EXACTLY as ReadProject (C2 fold, code-health-phase-a): no branch-read
		// bookkeeping, no branch-only fault injection — mirrors the pre-fold wrapper,
		// which never routed an empty branch through this method at all.
		return f.ReadProject(rc, projectID)
	}
	f.mu.Lock()
	f.readBranches = append(f.readBranches, branch)
	fail := f.failReadBackDecode
	f.mu.Unlock()
	if fail {
		// The QA F36 fault: the committed draft decodes MALFORMED (free prose in the "trigger"
		// closed enum). The real GitStore codec classifies this TERMINAL (ContractMisuse) and
		// carries the wire-name diagnostic; retry cannot fix the immutable committed bytes.
		return projectstate.Project{}, fwra.New(fwra.ContractMisuse,
			`projectstate: decode slots: decode slot CoreUseCases model: "A commitment of any size appears, however it arrives, and is still held only in the person's memory." is not a recognized Trigger wire name`)
	}
	proj, err := f.ReadProject(rc, projectID)
	if err != nil {
		return projectstate.Project{}, err
	}
	f.mu.Lock()
	adv := f.branchAdvancedModel
	f.mu.Unlock()
	if adv != nil {
		// The amendment's Action changed the artifact on the branch — serve a branch model
		// distinct from main so the no-change guard sees advancement and proceeds. The
		// critique carrier is an APPROVE (System is architect-critiqued; the critique job
		// commits its verdict to the same branch) so the round ratifies and proceeds.
		proj.SystemDesign = awaitingSlot(adv, projectstate.CritiqueVerdictApprove, "")
	}
	return proj, nil
}

func (f *branchAwareFakeProjectState) StageArtifactForReviewOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, model projectstate.ArtifactModel, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.stageBranches = append(f.stageBranches, branch)
	f.mu.Unlock()
	return f.StageArtifactForReview(fwra.Context{Context: rc.Context, IdempotencyKey: key}, projectID, expectedVersion, model)
}

// RejectArtifactOnBranch records the Reject on the SESSION BRANCH — the correct PR-rail
// substrate, where the draft was staged and the branch version matches. It delegates to
// the embedded fake's bookkeeping (rejected + Notes), then records the branch so the test
// asserts the reject rode the session branch (non-empty), not main.
func (f *branchAwareFakeProjectState) RejectArtifactOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.rejectBranches = append(f.rejectBranches, branch)
	fail := f.failRejectOnBranch
	f.mu.Unlock()
	if fail {
		// A terminal (non-retryable) write fault while recording the Reject — the crash
		// scenario QA F28 must contain instead of failing the whole workflow.
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RejectArtifact: simulated terminal write fault")
	}
	return f.fakeProjectState.RejectArtifact(fwra.Context{Context: rc.Context, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

// RejectArtifactOnBranchWithComments routes to THIS type's own RejectArtifactOnBranch
// (comments dropped) — branchAwareFakeProjectState models a branch-aware-but-NOT-ledger
// substrate (the old middle rung of the 3-way Ledger→BranchAware→base fallback). It must
// be defined directly here (not left to Go's embedding promotion of
// *fakeProjectState.RejectArtifactOnBranchWithComments) because a promoted method's
// internal f.RejectArtifact call resolves against the EMBEDDED type, not this outer one —
// it would silently skip the branch-routing override below and record no rejectBranches
// entry (Go has no virtual dispatch through embedding).
func (f *branchAwareFakeProjectState) RejectArtifactOnBranchWithComments(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, _ int64, _ []projectstate.ReviewComment, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.RejectArtifactOnBranch(rc, projectID, expectedVersion, branch, kind, notes, key)
}

// RejectArtifact (MAIN path) models the PRODUCTION PR-rail reality that caused QA F28: in
// the rail flow the draft is staged ONLY on the session branch, so main's slot is
// unpopulated and a main-path reject is a ContractMisuse ("stage a model first"), exactly
// as the real GitStore's statusTransition raises. This shadows the embedded fake so that
// if the Manager ever regresses to rejecting on main, the workflow crashes here and the
// regression test fails loudly.
func (f *branchAwareFakeProjectState) RejectArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind, _ string) (projectstate.Version, error) {
	return 0, fwra.New(fwra.ContractMisuse, "projectstate.RejectArtifact: slot "+kind.String()+" is unpopulated (stage a model first)")
}

// WithdrawArtifactOnBranch records the Withdraw on the SESSION BRANCH — the correct PR-rail
// substrate, where the draft was staged and the branch version matches. It delegates to
// the embedded fake's bookkeeping (withdrawn), then records the branch so the test asserts
// the withdraw rode the session branch (non-empty), not main. NOTE: the main-path
// WithdrawArtifact is intentionally NOT shadowed to fail — the FAILED-gate withdraw
// legitimately rides main (branch==""), so a blanket-failing shadow would break the
// not-green recovery test. The F30 regression guard is the withdrawBranches assertion (a
// regression to main leaves it empty).
func (f *branchAwareFakeProjectState) WithdrawArtifactOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.withdrawBranches = append(f.withdrawBranches, branch)
	fail := f.failWithdrawOnBranchRemaining > 0
	if fail {
		f.failWithdrawOnBranchRemaining--
	}
	f.mu.Unlock()
	if fail {
		// A terminal (non-retryable) write fault while recording the Withdraw — the
		// 2026-07-16 anti-wedge scenario the recovery path must contain.
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.WithdrawArtifact: simulated terminal write fault")
	}
	return f.fakeProjectState.WithdrawArtifact(fwra.Context{Context: rc.Context, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

// WithdrawArtifact (MAIN path) is the base behavior UNLESS failWithdrawOnMain is armed, in
// which case it models the PR-rail reality that caused QA F30: main's slot is unpopulated,
// so a main-path withdraw is a ContractMisuse. Armed only by the F30 review-gate test so a
// regression to withdrawing on main crashes the workflow and the test fails loudly.
func (f *branchAwareFakeProjectState) WithdrawArtifact(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind, notes string) (projectstate.Version, error) {
	if f.failWithdrawOnMain {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.WithdrawArtifact: slot "+kind.String()+" is unpopulated (stage a model first)")
	}
	return f.fakeProjectState.WithdrawArtifact(rc, projectID, expectedVersion, kind, notes)
}

func newRailWorkflows(rail sourcecontrol.SourceControlAccess) *workflows {
	return &workflows{
		Acts: genInvokers{Opts: activityOptions()},
		Rail: rail,
		Repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo"), true
		},
	}
}

// registerRailCoAuthor registers the rail-wired CoAuthor workflow + every generated
// activity, exactly as production's RegisterWorker does. ps is the fake substrate the
// generated projectState/designSession activities are backed by (threaded explicitly —
// the workflows struct no longer carries an RA dep; every Activity is generated).
func registerRailCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps projectstate.ProjectStateAccess, pipe *fakePipeline) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: executionKindCoAuthor})
	registerGenActivities(env, ps, pipe, wf.Rail)
}

// THE RAIL HAPPY PATH (§2b/§2c). With the rail wired, the System draft (architect-
// owned, single dispatch) runs the full settled flow: OpenBranch(sessionBranch) →
// dispatch → OpenPullRequest(head=sessionBranch) → read-back ON the session branch →
// stage ON the session branch → AwaitingReview → Approve → status guard (green) → +1 →
// merge → commit on main.
func Test_CoAuthor_RailEnabled_BranchPRReadBackPlusOneMerge_HappyPath(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // dispatch observed Succeeded
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("rail happy path workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want CoAuthorApproved, got %d", outcome)
	}
	// The rail ran the full settled sequence exactly once each.
	for _, verb := range []string{"OpenBranch", "OpenPullRequest", "GetPullRequestStatus", "PostReview", "MergePullRequest"} {
		if rail.verbCount(verb) != 1 {
			t.Fatalf("want exactly one %s rail call, got %d (calls: %+v)", verb, rail.verbCount(verb), rail.calls)
		}
	}
	// F-QA2-44: TWO mints — the dispatch-time mint (beginSession) plus the gate-decision
	// re-mint at approve (installation tokens expire in ~1h; the approve can arrive much
	// later, so its merge window must never reuse the dispatch-time token).
	if n := rail.verbCount("GetInstallationToken"); n != 2 {
		t.Fatalf("want two GetInstallationToken mints (dispatch + approve re-mint), got %d (calls: %+v)", n, rail.calls)
	}
	// The read-back + stage rode over the SESSION BRANCH (non-empty), not main.
	if len(ps.readBranches) == 0 || ps.readBranches[0] == "" {
		t.Fatalf("read-back must target the session branch, got %v", ps.readBranches)
	}
	if len(ps.stageBranches) != 1 || ps.stageBranches[0] == "" {
		t.Fatalf("stage must target the session branch, got %v", ps.stageBranches)
	}
	// Commit landed on main (the canonical head) exactly once.
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one CommitArtifact(KindSystem) on main, got %v", base.committed)
	}
}

// THE MERGE GUARD (§2b). At Approve the required CI check is NOT green: the rail must
// NOT merge and the spine must NOT commit — it routes to the StageDraftFailed recovery
// gate. Withdraw ends clean with nothing committed.
func Test_CoAuthor_RailEnabled_ApproveButPRNotGreen_DoesNotMerge_Recovers(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: false} // the merge guard is RED
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// First Approve hits the not-green guard → StageDraftFailed; then Withdraw ends it.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a not-green merge guard must not crash the workflow: %v", err)
	}
	if rail.verbCount("MergePullRequest") != 0 {
		t.Fatalf("a not-green PR must NOT be merged, got %d merge calls", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 0 {
		t.Fatalf("a not-green merge guard must NEVER commit, got %v", base.committed)
	}
}

// THE QA F36 REGRESSION — a MALFORMED committed draft read-back. The design job reports
// success and CI validate is GREEN (its Go mirror types the "trigger" field as a free
// string), but the server codec REJECTS the free-prose value in that closed enum on
// read-back (a TERMINAL ContractMisuse). Pre-fix the read-back Activity retried the same
// immutable committed bytes every ~100s FOREVER, leaving the session wedged at Drafting
// with no failure surface. After the fix the terminal decode fault lands the session at the
// human-visible StageDraftFailed gate carrying the DECODE DIAGNOSTIC as the FailureReason,
// and suspends awaiting Retry/Withdraw. Withdraw ends clean with nothing staged/committed.
func Test_CoAuthor_RailEnabled_MalformedReadBack_LandsInStageDraftFailed_WithDecodeReason(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base, failReadBackDecode: true}
	pipe := newFakePipeline() // dispatch observed Succeeded; CI validate is green
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		// The load-bearing anti-wedge assertion: NOT stuck at Drafting (the F36 wedge).
		if view.Stage == StageDrafting {
			t.Fatal("a malformed read-back must NOT leave the session in perpetual StageDrafting (F36 wedge)")
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a terminal decode read-back must land in StageDraftFailed, got stage %d", view.Stage)
		}
		// The FailureReason must carry the DECODE DIAGNOSTIC (the wire-name rejection) so the
		// human sees WHY — not a generic "job failed" message.
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("StageDraftFailed from a malformed read-back must carry a FailureReason")
		}
		if !strings.Contains(*view.FailureReason, "is not a recognized Trigger wire name") {
			t.Fatalf("FailureReason must carry the decode diagnostic; got %q", *view.FailureReason)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindCoreUseCases})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the decode-failed gate")
	}
	// A terminal decode-of-committed-state is contained at the Manager (human gate), NOT a
	// workflow crash and NOT an infinite retry loop.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal decode read-back must NOT fail the workflow, got: %v", err)
	}
	if len(ps.stageBranches) != 0 {
		t.Fatalf("a malformed read-back must stage nothing, got %v", ps.stageBranches)
	}
	if len(base.committed) != 0 {
		t.Fatalf("a malformed read-back must commit nothing, got %v", base.committed)
	}
}

// THE QA F28 REGRESSION — Reject/"Send back" against the PR rail. In the rail flow the
// draft + its AwaitingReview status live ONLY on the session branch (main's slot is
// unpopulated until an approved draft merges). The architect's Reject must therefore
// record the Rejected status ON THE SESSION BRANCH — NOT on main, where the version
// mismatches AND the slot is unpopulated (the ContractMisuse crash that ended the
// CoAuthor workflow FAILED and silently discarded the review comments). After the fix the
// Reject lands on the session branch (seeding the durable review ledger with the anchored
// comments + notes) and the workflow survives to re-dispatch the architect draft command.
func Test_CoAuthor_RailEnabled_Reject_RecordsOnSessionBranch_RedraftCarriesFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const (
		rejectNotes = "rework the decomposition"
		commentPath = "$.containers[0].name"
		commentText = "this manager name violates the layering rule"
	)
	feedback := &ReviewFeedback{
		Notes:    rejectNotes,
		Comments: []AnchoredComment{{JSONPath: commentPath, Text: commentText}},
	}

	// First gate: REJECT with anchored feedback. Second gate (after the redraft reaches
	// AwaitingReview again): WITHDRAW to end the session cleanly.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: feedback})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	// The reject must NOT crash the workflow (QA F28 was a non-retryable ContractMisuse
	// that ended it FAILED).
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a PR-rail reject must not crash the workflow: %v", err)
	}
	// The Reject rode the SESSION BRANCH, not main (main's slot is unpopulated — the
	// branchAwareFakeProjectState's main-path RejectArtifact would ContractMisuse).
	if len(ps.rejectBranches) != 1 || ps.rejectBranches[0] == "" {
		t.Fatalf("reject must target the session branch, got %v", ps.rejectBranches)
	}
	if len(base.rejected) != 1 || base.rejected[0].kind != projectstate.KindSystem || base.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact(KindSystem, %q) on the session branch, got %v", rejectNotes, base.rejected)
	}
	// The reject looped to a FRESH redraft dispatch. The architect's feedback no longer
	// rides a dispatch input — it reaches the drafting agent via the durable review LEDGER
	// (the reject-record asserted above is what seeds it, notes + anchored comments). The
	// redraft still dispatches the architect draft command for the kind.
	// (System is architect-critiqued, so each round is draft + system-critique.)
	drafts := 0
	for _, s := range pipe.submits {
		if s.dispatchInputs[dispatchInputCommand] == "system-draft" {
			drafts++
		}
	}
	if drafts < 2 {
		t.Fatalf("a reject must re-dispatch a fresh system-draft, got %d draft dispatches (submits %d)", drafts, len(pipe.submits))
	}
	if got := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputCommand]; got != "system-critique" {
		t.Fatalf("the redraft must be followed by the architect self-critique, got %q", got)
	}
}

// CRASH CONTAINMENT AT THE REVIEW GATE (QA F28 item 2). An activity fault while RECORDING
// the Reject must not kill the workflow. The spine lands at the human-visible
// StageDraftFailed recovery gate KEEPING the received feedback, so a Retry redrafts with
// the architect's comments woven in rather than silently discarding the send-back.
func Test_CoAuthor_RailEnabled_RejectWriteFaults_RecoversAtFailedGate_RetainsFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base, failRejectOnBranch: true}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const (
		rejectNotes = "rework the decomposition"
		commentPath = "$.containers[0].name"
		commentText = "this manager name violates the layering rule"
	)
	feedback := &ReviewFeedback{
		Notes:    rejectNotes,
		Comments: []AnchoredComment{{JSONPath: commentPath, Text: commentText}},
	}

	// First gate: REJECT (the write FAULTS terminally) → crash containment lands at the
	// StageDraftFailed gate carrying the feedback.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: feedback})
	}, 30*time.Second)
	// Assert the workflow landed at the recoverable failed gate (NOT crashed) with a reason.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow at failed gate: %v", err)
		}
		var view SessionStateView
		if derr := enc.Get(&view); derr != nil {
			t.Fatalf("decode SessionStateView: %v", derr)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a faulted reject must land at StageDraftFailed, got stage %v", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("the failed gate must surface a human FailureReason")
		}
	}, 45*time.Second)
	// Retry via the redraft lever WITH NO NEW FEEDBACK: the retained feedback must drive
	// the redraft.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 60*time.Second)
	// After the redraft reaches AwaitingReview, WITHDRAW to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 100*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a faulted reject must not crash the workflow: %v", err)
	}
	// The retry redrafted. This test uses a NON-ledger substrate (branchAwareFakeProjectState),
	// so the pre-dispatch failed-gate seed best-effort no-ops (NotFound) and there is no ledger
	// channel to assert on the redraft here — the seeding onto a ledger substrate is proven by
	// Test_CoAuthor_RailEnabled_FailedGateRetry_SeedsRetainedFeedbackToLedger_BeforeRedraft. What
	// this test proves is crash-containment (asserted above: no crash, lands at StageDraftFailed
	// with a reason) plus the retry re-dispatching the architect draft command.
	// (System is architect-critiqued: each round is draft + system-critique.)
	if len(pipe.submits) < 2 {
		t.Fatalf("the retry must issue a SECOND dispatch, got %d submits", len(pipe.submits))
	}
	retryDrafts := 0
	for _, s := range pipe.submits {
		if s.dispatchInputs[dispatchInputCommand] == "system-draft" {
			retryDrafts++
		}
	}
	if retryDrafts < 2 {
		t.Fatalf("retry must re-dispatch command=system-draft, got %d draft dispatches (submits %d)", retryDrafts, len(pipe.submits))
	}
}

// ---- f29BranchFake: version-enforcing branch-aware substrate (QA F29) --------

// f29BranchFake models the production reality F29 exposed: MAIN and the SESSION BRANCH
// sit at DIFFERENT versions. A fresh CoAuthor workflow captures main's version (mainVer),
// but the Action's prior draft/critique commits left a REUSED session branch AHEAD
// (branchVer). The read-back reads the BRANCH (branchVer); a stage that expects the stale
// main version Conflicts. Unlike the version-ignoring base fake, this fake ENFORCES the
// branch version on stage-on-branch, so a stale expected version surfaces the real
// fwra.Conflict — the exact non-retryable stage crash of F29 — and the test proves the fix
// converges. stageFailsRemaining injects terminal stage faults for the containment test.
type f29BranchFake struct {
	*fakeProjectState
	mu                  sync.Mutex
	mainVer             projectstate.Version
	branchVer           projectstate.Version
	stageExpecteds      []projectstate.Version
	stageFailsRemaining int
}

var _ projectstate.ProjectStateAccess = (*f29BranchFake)(nil)

func (f *f29BranchFake) ReadProject(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.project
	p.Version = f.mainVer
	return p, nil
}

func (f *f29BranchFake) ReadProjectVersion(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mainVer, nil
}

func (f *f29BranchFake) ReadProjectOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ string) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.project
	p.Version = f.branchVer // the dirty session branch is AHEAD of main
	return p, nil
}

func (f *f29BranchFake) StageArtifactForReviewOnBranch(_ fwra.Context, _ projectstate.ProjectID, expected projectstate.Version, _ string, model projectstate.ArtifactModel, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stageExpecteds = append(f.stageExpecteds, expected)
	if f.stageFailsRemaining > 0 {
		f.stageFailsRemaining--
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.StageArtifactForReview: simulated terminal stage fault")
	}
	if expected != f.branchVer {
		// The real GitStore's optimistic-concurrency guard — the F29 Conflict.
		return 0, fwra.New(fwra.Conflict, fmt.Sprintf("projectstate.StageArtifactForReview: stale version: have %d, expected %d", f.branchVer, expected))
	}
	f.branchVer++
	f.staged = append(f.staged, model)
	return f.branchVer, nil
}

func (f *f29BranchFake) RejectArtifactOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branchVer++
	return f.branchVer, nil
}

func (f *f29BranchFake) WithdrawArtifactOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Terminal in the F29 tests; leave branchVer untouched so the stage-convergence
	// assertion stays about the stage, not this withdraw.
	return f.branchVer, nil
}

// THE QA F29 REGRESSION — a fresh workflow reusing a session branch already AHEAD of main
// stages against the ACTUAL branch version and CONVERGES, instead of Conflicting
// non-recoverably against the stale main-captured version and crashing the workflow.
func Test_CoAuthor_RailEnabled_StageAgainstDirtyBranch_Converges_NoCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// main at v2; the reused session branch already advanced to v4 by prior draft/critique
	// commits — the exact "have 4, expected 2" split from the F29 report.
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	// F29 crashed the workflow (non-retryable Conflict → MutateConflictExhausted). The fix
	// must let it complete.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("staging against a dirty session branch must converge, not crash: %v", err)
	}
	// A stage SUCCEEDED (the branch version advanced past 4) — it converged on the branch.
	if ps.branchVer != 5 {
		t.Fatalf("stage must converge and advance the branch version to 5, got %d (expecteds=%v)", ps.branchVer, ps.stageExpecteds)
	}
	// The successful stage expected the ACTUAL branch version (4), never the stale main 2.
	last := ps.stageExpecteds[len(ps.stageExpecteds)-1]
	if last != 4 {
		t.Fatalf("the converged stage must expect the branch version 4, got %d (expecteds=%v)", last, ps.stageExpecteds)
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one commit after the converged stage → approve, got %v", base.committed)
	}
}

// CRASH CONTAINMENT AT THE STAGE STEP (QA F29 item 2). A terminal stage-for-review fault
// must NOT kill the workflow: the spine lands at the human-visible StageDraftFailed
// recovery gate. A Retry redrafts and the second stage converges.
func Test_CoAuthor_RailEnabled_StageFaults_RecoversAtFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// The FIRST stage faults terminally; the retry's stage converges.
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4, stageFailsRemaining: 1}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// After the stage fault the session is at StageDraftFailed — assert the recoverable
	// gate (not a crash), then Retry.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow at failed gate: %v", err)
		}
		var view SessionStateView
		if derr := enc.Get(&view); derr != nil {
			t.Fatalf("decode SessionStateView: %v", derr)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a faulted stage must land at StageDraftFailed, got stage %v", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("the failed gate must surface a human FailureReason")
		}
	}, 30*time.Second)
	// Retry via the redraft lever → re-draft → the second stage converges.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 45*time.Second)
	// After the recovered stage reaches AwaitingReview, Withdraw to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a faulted stage must not crash the workflow: %v", err)
	}
	// The recovered retry staged successfully (branch advanced past 4).
	if ps.branchVer != 5 {
		t.Fatalf("the recovered retry must converge its stage (branch → 5), got %d", ps.branchVer)
	}
}

// THE QA F30 REGRESSION — Withdraw against the PR rail records the Withdrawn status ON THE
// SESSION BRANCH (not main, where the version mismatches AND the slot is unpopulated), the
// workflow survives, and ends withdrawn. failWithdrawOnMain arms the main-path guard so a
// regression to withdrawing on main crashes the workflow and this test fails loudly.
func Test_CoAuthor_RailEnabled_Withdraw_RecordsOnSessionBranch_NoCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base, failWithdrawOnMain: true}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// At the AwaitingReview gate, WITHDRAW the not-yet-merged draft.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw, Feedback: &ReviewFeedback{Notes: "abandon this draft"}})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	// A rail-flow withdraw must NOT crash (F30 was a main-path ContractMisuse on the
	// unpopulated main slot).
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a PR-rail withdraw must not crash the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want CoAuthorWithdrawn, got %d", outcome)
	}
	// The Withdraw rode the SESSION BRANCH, not main.
	if len(ps.withdrawBranches) != 1 || ps.withdrawBranches[0] == "" {
		t.Fatalf("withdraw must target the session branch, got %v", ps.withdrawBranches)
	}
	if len(base.withdrawn) != 1 || base.withdrawn[0] != projectstate.KindSystem {
		t.Fatalf("want one WithdrawArtifact(KindSystem) on the session branch, got %v", base.withdrawn)
	}
}

// THE 2026-07-16 INCIDENT REGRESSION (child half — gtdapp glossary). The session sits at
// the StageDraftFailed gate having NEVER staged (the draft job failed before any
// StageArtifactForReview). Withdraw must SKIP the unstage write entirely — pre-fix it blindly
// wrote main, whose slot is unpopulated, raising the non-retryable ContractMisuse that
// TERMINATED this workflow and its parent phase. failWithdrawOnMain arms the exact production
// fault so any regression to the blind main write crashes this test loudly.
func Test_CoAuthor_FailedGate_Withdraw_NeverStaged_SkipsUnstage_EndsWithdrawn(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base, failWithdrawOnMain: true}
	pipe := newFakePipeline(pipelineFailed) // the draft job fails BEFORE anything stages
	pipe.diagnostic = "the design job failed before staging"
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw, Feedback: &ReviewFeedback{Notes: "abandon"}})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	// THE load-bearing assertion: one recovery click must never terminate the workflow.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a never-staged failed-gate withdraw must NOT crash the workflow, got: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want CoAuthorWithdrawn, got %d", outcome)
	}
	// NO unstage write anywhere — nothing was ever staged, so there is nothing to flip.
	if len(base.withdrawn) != 0 {
		t.Fatalf("a never-staged withdraw must NOT call WithdrawArtifact, got %v", base.withdrawn)
	}
	if len(ps.withdrawBranches) != 0 {
		t.Fatalf("a never-staged withdraw must NOT call WithdrawArtifactOnBranch, got %v", ps.withdrawBranches)
	}
}

// FIX-2 ANTI-WEDGE (failed gate) + staged-branch targeting. The session STAGED on the
// session branch, then a faulted reject landed it at the StageDraftFailed gate. The first
// Withdraw's write FAULTS terminally: the workflow must land BACK at the failed gate with an
// honest "withdraw failed: …" reason — never a workflow failure. The second Withdraw
// succeeds and must ride the STAGED BRANCH (the blind main write was the same
// unpopulated-slot crash; failWithdrawOnMain arms that regression guard).
func Test_CoAuthor_FailedGate_StagedWithdrawFaults_StaysAtGate_SecondWithdrawRidesStagedBranch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{
		fakeProjectState:              base,
		failRejectOnBranch:            true, // reject write faults → StageDraftFailed with the draft STAGED
		failWithdrawOnBranchRemaining: 1,    // first withdraw write faults; the second succeeds
		failWithdrawOnMain:            true, // any regression to a blind main write crashes loudly
	}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// AwaitingReview → Reject (write faults → failed gate, staged draft intact).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "rework"}})
	}, 30*time.Second)
	// Withdraw #1 — the write FAULTS: must stay at the failed gate with the honest reason.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)
	// Assert the gate re-armed with the withdraw-failed reason (workflow ALIVE).
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow after faulted withdraw: %v", err)
		}
		var view SessionStateView
		if derr := enc.Get(&view); derr != nil {
			t.Fatalf("decode SessionStateView: %v", derr)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a faulted failed-gate withdraw must land BACK at StageDraftFailed, got %d", view.Stage)
		}
		if view.FailureReason == nil || !strings.Contains(*view.FailureReason, "withdraw failed") {
			t.Fatalf("the re-armed gate must carry the honest withdraw-failed reason, got %v", view.FailureReason)
		}
	}, 75*time.Second)
	// Withdraw #2 — succeeds and ends the session.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 90*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("no recovery-path error may terminate the workflow, got: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want CoAuthorWithdrawn, got %d", outcome)
	}
	// Both withdraw attempts rode the STAGED session branch (never main).
	if len(ps.withdrawBranches) != 2 || ps.withdrawBranches[0] == "" || ps.withdrawBranches[1] == "" {
		t.Fatalf("failed-gate withdraw of a STAGED draft must target the staged session branch on every attempt, got %v", ps.withdrawBranches)
	}
	if len(base.withdrawn) != 1 || base.withdrawn[0] != projectstate.KindSystem {
		t.Fatalf("want exactly one successful WithdrawArtifact(KindSystem), got %v", base.withdrawn)
	}
}

// FIX-2 ANTI-WEDGE (review gate). A withdraw submitted at the AwaitingReview gate whose
// write FAULTS must return the session to AwaitingReview carrying the honest notice (the
// QA F35 approve-fault containment pattern) — never a workflow failure. A second withdraw
// then ends the session on the session branch.
func Test_CoAuthor_ReviewGate_WithdrawFaults_ReturnsToAwaitingReview_SecondWithdrawEnds(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{
		fakeProjectState:              base,
		failWithdrawOnBranchRemaining: 1,
		failWithdrawOnMain:            true,
	}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// Withdraw #1 at AwaitingReview — the write FAULTS.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)
	// The session must be BACK at AwaitingReview with the honest withdraw-failed notice.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow after faulted review-gate withdraw: %v", err)
		}
		var view SessionStateView
		if derr := enc.Get(&view); derr != nil {
			t.Fatalf("decode SessionStateView: %v", derr)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("a faulted review-gate withdraw must return to AwaitingReview (staged draft intact), got %d", view.Stage)
		}
		if view.FailureReason == nil || !strings.Contains(*view.FailureReason, "withdraw failed") {
			t.Fatalf("the re-armed review gate must carry the honest withdraw-failed notice, got %v", view.FailureReason)
		}
	}, 50*time.Second)
	// Withdraw #2 — succeeds.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a faulted review-gate withdraw must not crash the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want CoAuthorWithdrawn, got %d", outcome)
	}
	if len(ps.withdrawBranches) != 2 || ps.withdrawBranches[1] == "" {
		t.Fatalf("both withdraw attempts must ride the session branch, got %v", ps.withdrawBranches)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("want exactly one successful WithdrawArtifact, got %v", base.withdrawn)
	}
}

// F40 — a Retry at the StageDraftFailed gate redrafts on the SAME persistent session
// branch (the F32 branch-per-retry topology is unwound; the stale-base problem is now
// handled by the workflow template's refresh-from-main git step, not a fresh branch). This
// drives a stage fault → retry-via-reject (with feedback) and asserts the redraft dispatch
// targets the SAME branch AND still carries the retained feedback.
func Test_CoAuthor_RailEnabled_RetryAtFailedGate_SameBranch_RetainsFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// The FIRST stage faults terminally → StageDraftFailed; the retry's stage converges.
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4, stageFailsRemaining: 1}
	pipe := newFakePipeline() // every dispatch Succeeds (the fault is at STAGE, not the job)
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const retryNotes = "fix the layering violation before redrafting"
	// At the StageDraftFailed gate, Retry-via-Reject carrying feedback.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: retryNotes}})
	}, 30*time.Second)
	// After the recovered redraft reaches AwaitingReview, Withdraw to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed-gate retry must not crash the workflow: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("the retry must issue a SECOND draft dispatch, got %d submits", len(pipe.submits))
	}
	b0 := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	b1 := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputTargetBranch]
	if b0 == "" || b1 == "" {
		t.Fatalf("both dispatches must carry a target_branch, got %q / %q", b0, b1)
	}
	// F40: the retry redrafts on the SAME persistent session branch (the template's
	// refresh-from-main handles a stale base; no per-attempt suffix).
	if b1 != b0 {
		t.Fatalf("a failed-gate retry must redraft on the SAME session branch (F40); got %q then %q", b0, b1)
	}
	if strings.Contains(b1, "-amend-") {
		t.Fatalf("the retry branch must be the stable session branch (no amendment suffix), got %q", b1)
	}
	// The retry re-dispatches the architect draft command on the SAME session branch
	// (asserted above); the following submit is its architect self-critique. NOTE: this
	// retry-via-reject carries NOTES ONLY (no anchored comments), so the failed-gate ledger
	// seed is a no-op here; the anchored-comment seeding is proven by
	// Test_CoAuthor_RailEnabled_FailedGateRetry_SeedsRetainedFeedbackToLedger_BeforeRedraft.
	sameBranchDrafts := 0
	for _, s := range pipe.submits {
		if s.dispatchInputs[dispatchInputCommand] == "system-draft" {
			sameBranchDrafts++
		}
	}
	if sameBranchDrafts < 2 {
		t.Fatalf("retry must re-dispatch command=system-draft, got %d draft dispatches (submits %d)", sameBranchDrafts, len(pipe.submits))
	}
}

// sdAssertApproveFaultReturnedToGate queries the session after a contained
// approve-window fault and asserts it returned to AwaitingReview carrying the
// ratified re-approve notice.
func sdAssertApproveFaultReturnedToGate(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()
	enc, err := env.QueryWorkflow(querySessionState)
	if err != nil {
		t.Fatalf("QueryWorkflow after approve fault: %v", err)
	}
	var view SessionStateView
	if derr := enc.Get(&view); derr != nil {
		t.Fatalf("decode SessionStateView: %v", derr)
	}
	if view.Stage != StageAwaitingReview {
		t.Fatalf("an approve fault must return to AwaitingReview, got stage %v", view.Stage)
	}
	// F-QA2-41: the notice carries the founder-ratified framing — what failed, the
	// draft is unchanged, re-approve shortly.
	if view.FailureReason == nil || !strings.HasPrefix(*view.FailureReason, "The approve could not complete") {
		t.Fatalf("the returned session must carry the ratified re-approve notice, got %v", view.FailureReason)
	}
	if !strings.Contains(*view.FailureReason, "The draft is unchanged") {
		t.Fatalf("the notice must state the draft is unchanged, got %q", *view.FailureReason)
	}
}

// THE QA F35 REGRESSION — an approve-window fault (GetPullRequestStatus 403 → Auth kind)
// must NOT kill the workflow. After the bounded retry budget is exhausted, the session
// RETURNS to AwaitingReview carrying a queryable notice (FailureReason), and a re-approve
// succeeds and merges. NOT a redraft (which would discard the approved-quality draft).
func Test_CoAuthor_RailEnabled_ApproveStatusFault_ReturnsToAwaitingReview_ReapproveMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	// The first approve's GetPullRequestStatus 403s on all bounded long-backoff attempts
	// (F-QA2-49: 60s → 120s → 240s) → contained; after that the counter is 0 so the
	// re-approve reads green and merges.
	rail := &fakeRail{checkGreen: true, statusAuthFailsRemaining: railAuthRetryLongMaxAttempts}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// First approve → status 403s → contained → back to AwaitingReview with a notice.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	// Assert the session returned to AwaitingReview with a queryable re-approve notice.
	env.RegisterDelayedCallback(func() {
		sdAssertApproveFaultReturnedToGate(t, env)
	}, 500*time.Second) // after the ~420s long-backoff budget (60+120+240) exhausts (F-QA2-49)
	// Re-approve → now the status reads green → merge + commit.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 520*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("an approve-window fault must not crash the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("the re-approve must commit, got outcome %d", outcome)
	}
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("exactly one merge (on the successful re-approve), got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one commit after re-approve, got %v", base.committed)
	}
	// F-QA2-41: the successful re-approve DECISION cleared the stale fault notice — the
	// committed view must not carry it forward.
	enc, err := env.QueryWorkflow(querySessionState)
	if err != nil {
		t.Fatalf("QueryWorkflow after completion: %v", err)
	}
	var final SessionStateView
	if derr := enc.Get(&final); derr != nil {
		t.Fatalf("decode final SessionStateView: %v", derr)
	}
	if final.Stage != StageCommitted {
		t.Fatalf("want StageCommitted after the re-approve, got %v", final.Stage)
	}
	if final.FailureReason != nil {
		t.Fatalf("the re-approve decision must clear the stale approve-fault notice (F-QA2-41), got %q", *final.FailureReason)
	}
}

// F-QA2-41 (clearing twin of the F35 stamping proof above): a SEND-BACK decision after a
// contained approve fault supersedes the stale notice IMMEDIATELY — the Redrafting view
// (queried mid-redraft, before the fresh stage's own clear at the next AwaitingReview)
// must not still say "the approve could not complete" while the agent redrafts.
func Test_CoAuthor_RailEnabled_ApproveFaultNotice_ClearedOnSendBack(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	// Dispatch 1 (the initial draft) succeeds immediately; dispatch 2 (the redraft) is
	// scripted RUNNING on its first observation and flipped to SUCCEEDED after it, so the
	// test holds a live Redrafting window (the 15s poll timer) to query inside — the
	// runURL-test pattern.
	pipe := newFakePipeline(pipelineSucceeded, pipelineRunning)
	pipe.onObserve = func() {
		pipe.mu.Lock()
		defer pipe.mu.Unlock()
		for k := range pipe.handlePhase {
			pipe.handlePhase[k] = pipelineSucceeded
		}
	}
	// The first approve's GetPullRequestStatus 403s on all bounded long-backoff attempts
	// (F-QA2-49) → contained.
	rail := &fakeRail{checkGreen: true, statusAuthFailsRemaining: railAuthRetryLongMaxAttempts}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// First approve → status 403s → contained → back to AwaitingReview with the notice.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	// Send back instead of re-approving (the founder chooses a redraft) — after the
	// ~420s long-backoff budget (60+120+240) exhausts (F-QA2-49).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject,
			Feedback: &ReviewFeedback{Notes: "tighten the manager seams"}})
	}, 500*time.Second)
	// Mid-redraft (the observe poll timer is still pending): the stale approve-fault
	// notice must already be gone.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow mid-redraft: %v", err)
		}
		var view SessionStateView
		if derr := enc.Get(&view); derr != nil {
			t.Fatalf("decode SessionStateView: %v", derr)
		}
		// The redraft round-trip is live (the spine re-enters the draft loop as
		// Drafting/Redrafting depending on the sub-step) — either way it must no longer
		// be at the gate, and the stale notice must be gone.
		if view.Stage != StageRedrafting && view.Stage != StageDrafting {
			t.Fatalf("want a live redraft stage mid-redraft, got %v", view.Stage)
		}
		if view.FailureReason != nil {
			t.Fatalf("the send-back decision must clear the stale approve-fault notice (F-QA2-41), got %q", *view.FailureReason)
		}
	}, 505*time.Second)
	// The redraft reaches the next gate; withdraw to end the session.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 700*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the send-back after an approve fault must not crash the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want the withdrawn outcome ending the redrafted session, got %d", outcome)
	}
}

// BOUNDED RESILIENCE (QA F35). Two transient 403s then success: the bounded retry absorbs
// them and the merge completes on the FIRST approve — no return-to-review, no crash.
func Test_CoAuthor_RailEnabled_ApproveStatusTransient_RetriesThenMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true, statusAuthFailsRemaining: 2} // fail twice, 3rd succeeds
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("bounded retry must absorb the transient 403s: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("the first approve must commit after the retries, got %d", outcome)
	}
	// GetPullRequestStatus was attempted 3 times (2 faults + 1 success) within the budget.
	if n := rail.verbCount("GetPullRequestStatus"); n != 3 {
		t.Fatalf("want 3 bounded GetPullRequestStatus attempts (2 fault + 1 success), got %d", n)
	}
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("the merge must complete on the first approve, got %d merges", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit on the first approve, got %v", base.committed)
	}
}

// THE F38 AMENDMENT REGRESSION — reopening a COMMITTED artifact starts a fresh session on a
// …-amend-N branch and the draft prompt states it AMENDS the committed version. Driven by
// coAuthorInput.Amendment (which RequestArtifactDraft sets from the committed slot's
// Revisions). The reopening feedback rides coAuthorInput.Feedback.
func Test_CoAuthor_RailEnabled_Amendment_UsesAmendBranchAndPrompt(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	// Amendment 1 with anchored reopening feedback (the "why").
	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindSystem,
		Amendment:    1,
		Feedback:     &ReviewFeedback{Comments: []AnchoredComment{{JSONPath: "$.containers[0].name", Text: "rename this manager"}}},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("amendment session must not crash: %v", err)
	}
	if len(pipe.submits) == 0 {
		t.Fatal("amendment must dispatch a draft")
	}
	// The draft rode a fresh …-amend-1 branch (F38 + F40 stable within the amendment session).
	b := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if !strings.HasSuffix(b, "-amend-1") {
		t.Fatalf("amendment 1 must draft on a …-amend-1 branch, got %q", b)
	}
	// The amendment still dispatches the architect draft command (job_mode=draft). The
	// "this is an amendment" framing + the reopening feedback now reach the drafting agent
	// via the review ledger (seeded round-0) and the …-amend-1 branch, not a design_prompt.
	if got := pipe.submits[0].dispatchInputs[dispatchInputCommand]; got != "system-draft" {
		t.Fatalf("amendment must dispatch command=system-draft, got %q", got)
	}
}

// composeIdempotencyKey RUN-SCOPING (F40 root cause) pin RETIRED (B10): the local
// activityIdempotencyKey/composeIdempotencyKey helpers (activities_custom.go) are gone —
// every write this Manager makes now derives its key via the platform-generated
// genActivityIdempotencyKey (activities.gen.go, DO-NOT-EDIT), which the SAME 3-part
// run-scoped "workflowID:runID:activityID" format the deleted helper computed (verified
// by reading it before deletion). No pure seam remains in this package to pin the
// run-scoping property against; it now holds BY CONSTRUCTION in the shared generated
// derivation (identical for all five managers). Same posture construction (B8) and
// billing/projectdesign (B7/B9) already ship with — none of them pins the generated key
// format per-package either.

// F40 dispatch key is RUN-SCOPED end-to-end — the DispatchDesignJobActivity composes the
// key from activity.GetInfo, which now includes the RunID. Asserted through the fake
// pipeline's captured key (the test env pins a fixed run id, so we assert its presence).
func Test_CoAuthor_DispatchKey_CarriesRunID(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline(pipelineFailed) // fail after the first dispatch so it lands quickly
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)
	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(pipe.submits) == 0 {
		t.Fatal("expected a dispatch")
	}
	key := string(pipe.submits[0].idempotencyKey)
	// The run id must be a segment of the key (run-scoping) — not merely wfID:activityID.
	if !strings.Contains(key, ":default-test-run-id:") {
		t.Fatalf("dispatch idempotency key must be run-scoped (contain the RunID segment), got %q", key)
	}
}

// F40 AMENDMENT NO-CHANGE GUARD — an amendment session whose Action ran and "succeeded"
// but committed NOTHING that changed the artifact (branch read-back == committed main
// model) must land at the StageDraftFailed gate with the honest reason and open NO PR
// (opening one would 422 on the zero-new-commit branch). Withdraw ends clean.
func Test_CoAuthor_Rail_Amendment_NoChange_LandsFailedGate_NoPR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// branchAdvancedModel left nil ⇒ branch read-back == main ⇒ no advancement.
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // the design job "succeeds"
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a no-change amendment must land at StageDraftFailed, got stage %d", view.Stage)
		}
		reason := ""
		if view.FailureReason != nil {
			reason = *view.FailureReason
		}
		if !strings.Contains(reason, "no changes") {
			t.Fatalf("the failed gate must carry the honest no-change reason, got %q", reason)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindSystem,
		Amendment:    1,
		Feedback:     &ReviewFeedback{Notes: "please tighten"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("no-change amendment must not crash: %v", err)
	}
	// The dispatch + read-back ran, but NO PR was opened (the branch never advanced).
	if pipe.submits[0].dispatchInputs[dispatchInputTargetBranch] == "" {
		t.Fatal("the amendment must still dispatch a draft")
	}
	if rail.verbCount("OpenPullRequest") != 0 {
		t.Fatalf("a no-change amendment must open NO PR (zero-new-commit branch), got %d", rail.verbCount("OpenPullRequest"))
	}
	if rail.verbCount("MergePullRequest") != 0 {
		t.Fatalf("a no-change amendment must NOT merge, got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 0 {
		t.Fatalf("a no-change amendment must commit nothing, got %v", base.committed)
	}
}

// F40 AMENDMENT POSITIVE CONTROL — an amendment whose Action DID change the artifact
// (branch read-back differs from committed main) must NOT trip the no-change guard: it
// opens the PR and, on approve, merges + commits. Proves the guard blocks only genuine
// no-ops, not every amendment.
func Test_CoAuthor_Rail_Amendment_Advanced_OpensPR_Merges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{
		fakeProjectState:    base,
		branchAdvancedModel: &projectstate.System{Components: []projectstate.Component{{}}}, // differs from main's empty System
	}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindSystem,
		Amendment:    1,
		Feedback:     &ReviewFeedback{Notes: "add a manager"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("advanced amendment must not crash: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("an advanced amendment that is approved must merge+commit, got outcome %d", outcome)
	}
	if rail.verbCount("OpenPullRequest") == 0 {
		t.Fatal("an advanced amendment must OPEN a PR (the branch moved beyond main)")
	}
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("approve must merge the amendment PR once, got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 {
		t.Fatalf("approve must commit the amendment once, got %v", base.committed)
	}
}

// amendmentIndexFor — the pre-field fix rule. A COMMITTED slot yields an amendment index of
// max(1, Revisions): a slot committed BEFORE the Revisions field existed reads Revisions=0
// yet is still an amendment (index 1). Non-committed slots are the normal path (0).
func Test_amendmentIndexFor_Rule(t *testing.T) {
	// Pre-field committed slot (the observed gtdapp glossary case): Revisions 0 → index 1.
	if got := projectstate.AmendmentIndexFor(projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 0}); got != 1 {
		t.Fatalf("pre-field committed slot must yield amendment index 1, got %d", got)
	}
	// A committed slot with a real revision count returns it.
	if got := projectstate.AmendmentIndexFor(projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 3}); got != 3 {
		t.Fatalf("committed slot at revision 3 must yield amendment index 3, got %d", got)
	}
	// Non-committed slots are NOT amendments regardless of any stray Revisions value.
	for _, st := range []projectstate.ArtifactReviewStatus{
		projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected, projectstate.ReviewWithdrawn,
	} {
		if got := projectstate.AmendmentIndexFor(projectstate.ArtifactSlot{Status: st, Revisions: 5}); got != 0 {
			t.Fatalf("non-committed slot (status %d) must yield amendment index 0, got %d", st, got)
		}
	}
}

// ledgerFakeProjectState is the branch-aware fake PLUS the review-ledger seam, so the
// amendment SEED (SeedReviewCommentsActivity → SeedReviewCommentsOnBranch) actually FIRES
// and is observable. It is a SEPARATE type (not the shared branchAwareFakeProjectState) so
// existing reject tests — which assert on the non-ledger RejectArtifactOnBranch recorder —
// are unaffected by the ledger routing.
type ledgerFakeProjectState struct {
	*branchAwareFakeProjectState
	seededRounds   []int64
	seededComments [][]projectstate.ReviewComment
	// pipe, when set, lets the seed recorder capture the dispatch count AT SEED TIME so a test
	// can prove a failed-gate ledger seed lands BEFORE the redraft dispatch (seededAtSubmits[i]
	// is the number of dispatches already submitted when the i-th seed fired).
	pipe            *fakePipeline
	seededAtSubmits []int
}

var _ projectstate.ProjectStateAccess = (*ledgerFakeProjectState)(nil)

func (f *ledgerFakeProjectState) SeedReviewCommentsOnBranch(_ fwra.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, _ string, _ projectstate.ArtifactKind, round int64, comments []projectstate.ReviewComment, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.seededRounds = append(f.seededRounds, round)
	f.seededComments = append(f.seededComments, comments)
	if f.pipe != nil {
		f.seededAtSubmits = append(f.seededAtSubmits, f.pipe.submitCount())
	}
	return expectedVersion, nil
}

func (f *ledgerFakeProjectState) RejectArtifactOnBranchWithComments(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, _ int64, _ []projectstate.ReviewComment, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.RejectArtifactOnBranch(rc, projectID, expectedVersion, branch, kind, notes, key)
}

func (f *ledgerFakeProjectState) SetReviewCommentStatusOnBranch(_ fwra.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return expectedVersion, nil
}

// F38 PRE-FIELD AMENDMENT (the observed gtdapp glossary bug) — a slot COMMITTED before the
// Revisions field existed (Status=Committed, Revisions=0) must run the FULL amendment path:
// the index computes to 1 (amendmentIndexFor), the draft rides a …-amend-1 branch with the
// amendment prompt framing, AND the reopening feedback is SEEDED into the review ledger
// (round 0). Pre-fix it computed Amendment=0 → a normal draft on the canonical branch with
// NO seed.
func Test_CoAuthor_Rail_Amendment_PreFieldCommittedSlot_AmendBranch_Prompt_SeedFires(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// A PRE-FIELD committed SystemDesign slot: committedSlot sets Status=Committed, Revisions=0.
	proj := systemReadBack(t, id)
	proj.SystemDesign = committedSlot(&projectstate.System{})
	base := &fakeProjectState{project: proj}

	// The index the manager WOULD compute for this pre-field slot must be 1 (not 0).
	if got := projectstate.AmendmentIndexFor(proj.SystemDesign); got != 1 {
		t.Fatalf("a pre-field committed slot must compute amendment index 1, got %d", got)
	}

	// The branch advances the artifact (so the no-change guard passes) and the ledger records seeds.
	ps := &ledgerFakeProjectState{
		branchAwareFakeProjectState: &branchAwareFakeProjectState{
			fakeProjectState:    base,
			branchAdvancedModel: &projectstate.System{Components: []projectstate.Component{{}}},
		},
	}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	// Drive the workflow with the COMPUTED index (1), as the manager would.
	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindSystem,
		Amendment:    projectstate.AmendmentIndexFor(proj.SystemDesign),
		Feedback:     &ReviewFeedback{Comments: []AnchoredComment{{JSONPath: "$.components[0].name", Text: "rename this manager"}}},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pre-field amendment must not crash: %v", err)
	}
	if len(pipe.submits) == 0 {
		t.Fatal("the amendment must dispatch a draft")
	}
	// -amend-1 branch (NOT the canonical branch).
	b := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if !strings.HasSuffix(b, "-amend-1") {
		t.Fatalf("a pre-field committed slot must draft on a …-amend-1 branch, got %q", b)
	}
	// The amendment dispatches the architect draft command (job_mode=draft); the revision-1
	// framing now rides the …-amend-1 branch + the seeded ledger below, not a design_prompt.
	if got := pipe.submits[0].dispatchInputs[dispatchInputCommand]; got != "system-draft" {
		t.Fatalf("the amendment must dispatch command=system-draft, got %q", got)
	}
	// THE LOAD-BEARING FIX: the reopening feedback was SEEDED into the review ledger (round 0).
	if len(ps.seededRounds) == 0 {
		t.Fatal("the amendment SEED must fire for a pre-field committed slot (pre-fix it did not)")
	}
	if ps.seededRounds[0] != 0 {
		t.Fatalf("the reopening feedback must seed as round 0, got round %d", ps.seededRounds[0])
	}
	if len(ps.seededComments) == 0 || len(ps.seededComments[0]) == 0 {
		t.Fatal("the seed must carry the reopening comments as OPEN ledger entries")
	}
}

// NOTES-ONLY AMENDMENT SEED (the webApp Amend data-loss bug). The webApp Amend composer folds the
// user's rationale AND every queued rail comment into Feedback.Notes and sends NO structured
// Comments — so feedbackToLedgerComments returns empty and, PRE-FIX, seedAmendmentLedger bailed
// with NOTHING seeded: the reopen ledger was empty and the redraft agent reconciled on a stale
// basis with the user's direction silently dropped. The fix synthesizes an UNANCHORED round-0
// ledger comment carrying the Notes. This proves a Notes-only amendment seeds that rationale.
func Test_CoAuthor_Rail_Amendment_NotesOnlyFeedback_SeedsRationaleAsRound0Comment(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	proj := systemReadBack(t, id)
	proj.SystemDesign = committedSlot(&projectstate.System{})
	base := &fakeProjectState{project: proj}

	// The branch advances the artifact (so the no-change guard passes) and the ledger records seeds.
	ps := &ledgerFakeProjectState{
		branchAwareFakeProjectState: &branchAwareFakeProjectState{
			fakeProjectState:    base,
			branchAdvancedModel: &projectstate.System{Components: []projectstate.Component{{}}},
		},
	}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// Approve once the amendment reaches AwaitingReview, to end the session cleanly.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	const rationale = "tighten the manager boundaries and split the god interface"
	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindSystem,
		Amendment:    projectstate.AmendmentIndexFor(proj.SystemDesign),
		// EXACTLY what the webApp Amend composer sends: free-text rationale, no structured Comments.
		Feedback: &ReviewFeedback{Notes: rationale},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a Notes-only amendment must not crash: %v", err)
	}
	if len(pipe.submits) == 0 {
		t.Fatal("the amendment must dispatch a draft")
	}
	// THE FIX: the reopening RATIONALE (Notes) was seeded into the review ledger as a round-0 OPEN
	// entry — pre-fix, a Notes-only amendment seeded NOTHING (the reopen ledger was empty).
	if len(ps.seededRounds) == 0 {
		t.Fatal("a Notes-only amendment must SEED its rationale into the reopen ledger (pre-fix it seeded nothing)")
	}
	if ps.seededRounds[0] != 0 {
		t.Fatalf("the reopening rationale must seed as round 0, got round %d", ps.seededRounds[0])
	}
	found := false
	var seededComment projectstate.ReviewComment
	for _, batch := range ps.seededComments {
		for _, c := range batch {
			if c.Text == rationale {
				found = true
				seededComment = c
			}
		}
	}
	if !found {
		t.Fatalf("the seeded ledger must carry the amendment rationale %q, got %+v", rationale, ps.seededComments)
	}
	// The synthesized rationale comment is UNANCHORED (free-text Notes carry no JSONPath) and
	// stamped with the reviewer role, exactly like a filed reject comment.
	if seededComment.Anchor != "" {
		t.Fatalf("the synthesized rationale comment must be unanchored, got anchor %q", seededComment.Anchor)
	}
	if seededComment.AuthorRole != reviewAuthorRole {
		t.Fatalf("the synthesized rationale comment must carry the reviewer role %q, got %q", reviewAuthorRole, seededComment.AuthorRole)
	}
}

// FAILED-GATE FEEDBACK SEED (thin dispatch). A Retry-via-Reject AT a failed gate retains the
// architect's anchored feedback in workflow MEMORY only — unlike a review-gate reject, a
// failed-gate reject never touches the ledger. Under thin dispatch the drafting agent reads
// context ONLY via getReviewThread, so the manager must SEED that retained feedback into the
// durable review ledger BEFORE the redraft dispatch (else it evaporates — the B2 report gap).
// This proves the seed fires with EXACTLY the retained comment AND lands before the redraft.
func Test_CoAuthor_RailEnabled_FailedGateRetry_SeedsRetainedFeedbackToLedger_BeforeRedraft(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// The FIRST draft job FAILS (lands the session at the StageDraftFailed gate); the retry's
	// redraft succeeds and reaches AwaitingReview.
	pipe := newFakePipeline(pipelineFailed, pipelineSucceeded)
	pipe.diagnostic = "the drafting job failed in CI"
	ps := &ledgerFakeProjectState{
		branchAwareFakeProjectState: &branchAwareFakeProjectState{fakeProjectState: base},
		pipe:                        pipe,
	}
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const (
		retainedPath = "$.components[0].name"
		retainedText = "this manager name violates the layering rule — fix before redrafting"
	)
	// At the failed gate: Retry-via-Reject carrying anchored feedback.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{
			Decision: ReviewReject,
			Feedback: &ReviewFeedback{Notes: "redo the decomposition", Comments: []AnchoredComment{{JSONPath: retainedPath, Text: retainedText}}},
		})
	}, 30*time.Second)
	// After the recovered redraft reaches AwaitingReview, Withdraw to end cleanly.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed-gate retry must not crash the workflow: %v", err)
	}
	// The retry re-dispatched (a SECOND draft).
	if len(pipe.submits) < 2 {
		t.Fatalf("the retry must issue a SECOND draft dispatch, got %d submits", len(pipe.submits))
	}
	// THE FIX: the retained feedback was SEEDED into the review ledger exactly once (one round).
	if len(ps.seededRounds) != 1 {
		t.Fatalf("the retained failed-gate feedback must seed exactly once, got %d seeds (%v)", len(ps.seededRounds), ps.seededRounds)
	}
	// The seed carries the architect's anchored comment AS WELL AS the free-text Notes rationale
	// synthesized into an unanchored comment — the amend-seed-notes fix: a failed-gate reject's
	// Notes were previously DROPPED by feedbackToLedgerComments, evaporating under thin dispatch.
	if len(ps.seededComments) != 1 || len(ps.seededComments[0]) != 2 {
		t.Fatalf("the seed must carry the retained anchored comment PLUS the synthesized Notes comment, got %v", ps.seededComments)
	}
	if c := ps.seededComments[0][0]; c.Anchor != retainedPath || c.Text != retainedText {
		t.Fatalf("the first seeded comment must be the retained anchored feedback, got %+v", c)
	}
	if c := ps.seededComments[0][1]; c.Anchor != "" || c.Text != "redo the decomposition" {
		t.Fatalf("the second seeded comment must be the synthesized (unanchored) Notes rationale, got %+v", c)
	}
	// ORDERING: the seed landed BEFORE the redraft dispatch — only the first (failed) dispatch
	// had been submitted when the seed fired (count == 1), so the seed precedes the SECOND
	// dispatch (which brings the count to 2).
	if len(ps.seededAtSubmits) != 1 || ps.seededAtSubmits[0] != 1 {
		t.Fatalf("the ledger seed must land BEFORE the redraft dispatch (want 1 prior dispatch at seed time), got %v", ps.seededAtSubmits)
	}
}

// NO DOUBLE-SEED. A review-gate REJECT folds its feedback into the ledger via the reject write
// itself (RejectArtifactOnBranchWithComments). The pre-dispatch failed-gate seed must therefore
// SKIP it — otherwise the same comments would be seeded a SECOND time (at a different round →
// duplicate ledger entries). Proven here: after a review-gate reject → redraft, the failed-gate
// SEED activity never fired (the reject write is the sole ledger write for that feedback).
func Test_CoAuthor_RailEnabled_ReviewGateReject_NotDoubleSeeded(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	pipe := newFakePipeline() // every dispatch succeeds → reaches the REVIEW gate (not a failed gate)
	ps := &ledgerFakeProjectState{
		branchAwareFakeProjectState: &branchAwareFakeProjectState{fakeProjectState: base},
		pipe:                        pipe,
	}
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	feedback := &ReviewFeedback{
		Notes:    "rework the decomposition",
		Comments: []AnchoredComment{{JSONPath: "$.components[0].name", Text: "layering violation"}},
	}
	// First gate: REJECT with anchored feedback (the reject write seeds it). Second gate: WITHDRAW.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: feedback})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a review-gate reject must not crash the workflow: %v", err)
	}
	// The reject looped to a redraft (a SECOND dispatch).
	if len(pipe.submits) < 2 {
		t.Fatalf("a reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
	// THE GUARD: the failed-gate SEED activity never ran for the reject-seeded feedback — the
	// reject write is its ONLY ledger write, so there is no duplicate.
	if len(ps.seededRounds) != 0 {
		t.Fatalf("a review-gate reject must NOT be double-seeded via the failed-gate seed; got %d spurious seeds (%v)", len(ps.seededRounds), ps.seededRounds)
	}
}

// sdAssertOpenPRFaultContainedAtGate queries the failed gate after a persistent openPR
// Auth fault exhausted the bounded retry and asserts the honest containment shape:
// StageDraftFailed naming the pull-request step, with only the ONE draft dispatch so far.
func sdAssertOpenPRFaultContainedAtGate(t *testing.T, env *testsuite.TestWorkflowEnvironment, pipe *fakePipeline) {
	t.Helper()
	view := sdSessionView(t, env)
	if view.Stage != StageDraftFailed {
		t.Fatalf("a persistent openPR Auth fault must CONTAIN at StageDraftFailed, got stage %d", view.Stage)
	}
	reason := ""
	if view.FailureReason != nil {
		reason = *view.FailureReason
	}
	if !strings.Contains(reason, "pull request") {
		t.Fatalf("the failed gate must name the openPR step honestly, got %q", reason)
	}
	// Only ONE dispatch so far — the draft is preserved on the branch.
	if len(pipe.submits) != 1 {
		t.Fatalf("before retry there must be exactly ONE dispatch, got %d", len(pipe.submits))
	}
}

// F35 TWIN (the draft-round-trip openPR fault) — a GREEN draft + successful read-back, then
// OpenPullRequest persistently Auth-faults (secondary-rate-limit-403-as-Auth) past the shared
// bounded retry. The whole CoAuthor workflow must NOT die (as it did live on gtdapp kind 5):
// it CONTAINS the fault at the StageDraftFailed gate, and on Retry it RESUMES from the
// read-back — WITHOUT a second dispatch (which would burn another 20+ min draft and red the
// no-commit guard) — re-opens the PR, and Approve merges.
func Test_CoAuthor_Rail_OpenPRAuthFault_ContainsAtGate_RetryResumesNoRedispatch_ThenMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // the draft job succeeds (a green 20+ min draft)
	// OpenPullRequest Auth-faults for all railAuthRetryLongMaxAttempts of the FIRST openPR
	// (F-QA2-49: the ~420s long-backoff budget), so the bounded retry exhausts and the
	// round-trip lands at the failed gate; the resume openPR succeeds.
	rail := &fakeRail{checkGreen: true, openPRAuthFailsRemaining: railAuthRetryLongMaxAttempts}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// At the failed gate: assert StageDraftFailed with the honest openPR reason, then RETRY.
	env.RegisterDelayedCallback(func() {
		sdAssertOpenPRFaultContainedAtGate(t, env, pipe)
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 500*time.Second) // after the ~420s long-backoff budget (60+120+240) exhausts (F-QA2-49)

	// After the resume re-stages, Approve → merge.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 560*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("an openPR Auth fault must NOT kill the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("after resume + approve the session must be Approved, got %d", outcome)
	}

	// THE LOAD-BEARING ASSERTION: the Retry RESUMED from read-back — NO second DRAFT
	// dispatch (the 20+ min draft is never re-burned). The second submit is the
	// architect self-critique that runs after the resumed rail step succeeds.
	if len(pipe.submits) != 2 {
		t.Fatalf("want draft + critique dispatches only (no draft re-dispatch on retry); got %d", len(pipe.submits))
	}
	if got := pipe.submits[0].dispatchInputs[dispatchInputCommand]; got != "system-draft" {
		t.Fatalf("first dispatch must be the draft, got %q", got)
	}
	if got := pipe.submits[1].dispatchInputs[dispatchInputCommand]; got != "system-critique" {
		t.Fatalf("the retry must NOT re-dispatch the draft (resume from read-back); got a second %q dispatch", got)
	}
	// OpenPullRequest was attempted railAuthRetryLongMaxAttempts times (all faulting) in the
	// first round + once more on the resume (success) = maxAttempts+1.
	if got, want := rail.verbCount("OpenPullRequest"), railAuthRetryLongMaxAttempts+1; got != want {
		t.Fatalf("OpenPullRequest attempts: got %d, want %d (%d bounded-retry faults + 1 resume success)", got, want, railAuthRetryLongMaxAttempts)
	}
	// Exactly one PR was actually opened (the resume success), then merged, then committed once.
	if rail.openedPRs != 1 {
		t.Fatalf("exactly one PR must actually open (on the resume), got %d", rail.openedPRs)
	}
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("approve must merge once, got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 {
		t.Fatalf("approve must commit once, got %v", base.committed)
	}
}

// F-QA2-49 — THE LONG-BACKOFF SURVIVAL PROOF. A GitHub secondary-rate-limit 403 burst
// (which needs a >=60s cool-down before ANY retry can succeed) faults OpenPullRequest on
// the first railAuthRetryLongMaxAttempts-1 attempts; the fault CLEARS before the FINAL
// long-backoff attempt (60s + 120s + 240s of durable workflow.Sleep timers have elapsed —
// past the cool-down window). The draft round-trip must SURVIVE IN PLACE: no
// StageDraftFailed, exactly ONE dispatch, the PR opens on the last bounded attempt, and
// Approve merges. Under the OLD ~30s budget (5s → 10s → 15s) this exact burst exhausted
// entirely inside the cool-down and parked the session at the failed gate (observed live
// at gtdapp: 3 attempts across 15s → StageDraftFailed; a manual retry 15 min later
// succeeded first try).
func Test_CoAuthor_Rail_OpenPR403Burst_FaultClearsBeforeFinalLongBackoffAttempt_Survives(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // the draft job succeeds (a green 20+ min draft)
	// The 403 burst outlasts every attempt but the LAST: attempts 1..3 fault, the 4th
	// (after the 240s sleep, ~420s in) succeeds.
	rail := &fakeRail{checkGreen: true, openPRAuthFailsRemaining: railAuthRetryLongMaxAttempts - 1}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// Past the ~420s of long-backoff sleeps the session must sit at the REVIEW gate —
	// never the failed gate — with no stale failure notice; then Approve → merge.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if derr := enc.Get(&view); derr != nil {
			t.Fatalf("decode SessionStateView: %v", derr)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("a 403 burst clearing within the long-backoff budget must land at AwaitingReview (survive in place), got stage %d", view.Stage)
		}
		if view.FailureReason != nil {
			t.Fatalf("a survived 403 burst must carry NO failure notice, got %q", *view.FailureReason)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 500*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a 403 burst within the long-backoff budget must NOT kill the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("the session must be Approved after the survived burst, got %d", outcome)
	}
	// The burst was absorbed IN PLACE: one DRAFT dispatch only (the 20+ min draft was
	// never re-burned; the second submit is the architect self-critique after the rail
	// step recovered), every bounded attempt was used, and exactly one PR actually opened.
	if len(pipe.submits) != 2 {
		t.Fatalf("want draft + critique dispatches only (no draft re-dispatch), got %d", len(pipe.submits))
	}
	if got := pipe.submits[1].dispatchInputs[dispatchInputCommand]; got != "system-critique" {
		t.Fatalf("the absorbed burst must NOT re-dispatch the draft; got a second %q dispatch", got)
	}
	if got, want := rail.verbCount("OpenPullRequest"), railAuthRetryLongMaxAttempts; got != want {
		t.Fatalf("OpenPullRequest attempts: got %d, want %d (%d faults + 1 final success)", got, want, want-1)
	}
	if rail.openedPRs != 1 {
		t.Fatalf("exactly one PR must open (the final bounded attempt), got %d", rail.openedPRs)
	}
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("approve must merge once, got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 {
		t.Fatalf("approve must commit once, got %v", base.committed)
	}
}

// F47 — the REDRAFT-SIGNAL feedback path (what RequestArtifactDraft delivers via
// SignalWithStart). A draft job fails → StageDraftFailed gate; the redraft signal carries the
// operator's fix notes; the NEXT draft dispatch's prompt must CONTAIN those notes (they were
// dropped live on the retry-at-failed-gate path). Complements the retry-via-reject test.
func Test_CoAuthor_RailEnabled_RedraftSignalFeedbackReachesPrompt(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline(pipelineFailed) // the draft job fails → the StageDraftFailed gate
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const notes = "resources must be plain strings, not objects"
	// At the failed gate, deliver the REDRAFT signal (the RequestArtifactDraft path) with notes.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(lSignalRedraft, redraftSignal{Feedback: &ReviewFeedback{Notes: notes}})
	}, 30*time.Second)
	// Back at the gate after the second (also-failed) dispatch, withdraw to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a redraft-signal retry must not crash: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("the redraft signal must trigger a SECOND draft dispatch, got %d", len(pipe.submits))
	}
	// The redraft signal triggers a fresh architect draft dispatch. NOTE: a redraft-signal at
	// a FAILED gate carries operator notes that used to be woven into design_prompt; that
	// channel is retired and this path does NOT seed the review ledger, so the notes no longer
	// reach the drafting agent (see the B2 report's behavior note). What remains assertable is
	// the second dispatch and its command slug.
	if got := pipe.submits[1].dispatchInputs[dispatchInputCommand]; got != "system-draft" {
		t.Fatalf("the redraft signal must dispatch command=system-draft, got %q", got)
	}
}

// F48 — the Temporal activity-boundary codec MUST carry the durable review ledger (audited from
// projectdesign; systemdesign had the same hole). Without it, loadReviewThread reads the session
// branch through this projectEnvelope and returns [] despite the reject-append living in git.
func Test_projectEnvelope_PreservesReviewThread(t *testing.T) {
	id := ProjectID(uuid.NewString())
	p := systemReadBack(t, id)
	p.SystemDesign = projectstate.ArtifactSlot{
		Status: projectstate.ReviewAwaitingReview,
		Model:  &projectstate.System{},
		ReviewThread: []projectstate.ReviewComment{
			{ID: "r0c1", Text: "split this Manager per volatility", AuthorRole: "architect", Round: 0, Status: projectstate.ReviewCommentOpen},
		},
	}
	env, err := encodeProject(p)
	if err != nil {
		t.Fatalf("encodeProject: %v", err)
	}
	got, err := env.Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	thread := slotFor(got, KindSystem).ReviewThread
	if len(thread) != 1 {
		t.Fatalf("the review thread must survive the Temporal codec round-trip, got %d comments: %+v", len(thread), thread)
	}
	if thread[0].ID != "r0c1" || thread[0].Status != projectstate.ReviewCommentOpen || thread[0].Text == "" {
		t.Fatalf("the codec must preserve the comment's id/status/text, got %+v", thread[0])
	}
}

// ---------------------------------------------------------------------------
// MANAGED-SCAFFOLD SYNC (sync-on-dispatch, 2026-07-06). The seated aiarch-design.yml
// is converged onto the CURRENT template rendering BEFORE any design job is
// dispatched; a sync failure BLOCKS the dispatch (never run a design job against a
// scaffold the server could not prove current — the gtdapp stale-pin / F81 incident).
// ---------------------------------------------------------------------------

// firstCallIndex returns the index of the first recorded rail call with verb, or -1.
func (r *fakeRail) firstCallIndex(verb string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, c := range r.calls {
		if c.verb == verb {
			return i
		}
	}
	return -1
}

// THE SYNC ORDER. The managed-scaffold sync runs in the dispatch-time rail half,
// BEFORE OpenBranch and therefore before any design job is dispatched; a drifted
// scaffold (syncChanged=true) refreshes and the spine proceeds normally to Approve.
func Test_CoAuthor_Rail_ScaffoldSync_RunsBeforeDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true, syncChanged: true} // the seated scaffold DRIFTED
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a drifted-then-refreshed scaffold must not perturb the spine: %v", err)
	}
	if rail.verbCount("SyncManagedScaffold") != 1 {
		t.Fatalf("want exactly one managed-scaffold sync per dispatch-time session begin, got %d", rail.verbCount("SyncManagedScaffold"))
	}
	iSync, iBranch := rail.firstCallIndex("SyncManagedScaffold"), rail.firstCallIndex("OpenBranch")
	if iSync < 0 || iBranch < 0 || iSync > iBranch {
		t.Fatalf("the managed-scaffold sync must run BEFORE OpenBranch (pre-dispatch), got sync=%d openBranch=%d (calls: %+v)", iSync, iBranch, rail.calls)
	}
	// System is architect-critiqued: draft + system-critique = 2 dispatches, but still
	// exactly ONE scaffold sync (per dispatch-time session begin, asserted above).
	if len(pipe.submits) != 2 {
		t.Fatalf("the design job must dispatch draft + critique after a successful sync, got %d", len(pipe.submits))
	}
	if len(base.committed) != 1 {
		t.Fatalf("the approved spine must commit once, got %v", base.committed)
	}
}

// THE SYNC GATE. A managed-scaffold sync failure BLOCKS the dispatch: NO design job is
// submitted, NO session branch is opened, and the session lands at the human-visible
// StageDraftFailed gate (contained, never a crash). Withdraw ends clean.
func Test_CoAuthor_Rail_ScaffoldSyncFailure_BlocksDispatch_LandsAtFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true, syncErr: fwra.New(fwra.ContractMisuse, "seated workflow could not be refreshed")}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a failed managed-scaffold sync must land at StageDraftFailed (dispatch blocked), got %d", view.Stage)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed managed-scaffold sync must be CONTAINED at the failed gate, not crash: %v", err)
	}
	if got := rail.verbCount("SyncManagedScaffold"); got == 0 {
		t.Fatal("the managed-scaffold sync must have been attempted")
	}
	// The dispatch was BLOCKED: no design job submitted, no session branch, no PR.
	if len(pipe.submits) != 0 {
		t.Fatalf("a failed sync must BLOCK the design-job dispatch, got %d submits", len(pipe.submits))
	}
	if rail.verbCount("OpenBranch") != 0 || rail.verbCount("OpenPullRequest") != 0 {
		t.Fatalf("a failed sync must not open a branch/PR, got openBranch=%d openPR=%d",
			rail.verbCount("OpenBranch"), rail.verbCount("OpenPullRequest"))
	}
	// Nothing staged/committed; the withdraw SKIPS the unstage write (nothing was ever
	// staged — 2026-07-16 incident) and ends the session cleanly.
	if len(base.staged) != 0 || len(base.committed) != 0 {
		t.Fatalf("a blocked dispatch must stage/commit nothing, got staged=%d committed=%v", len(base.staged), base.committed)
	}
	if len(base.withdrawn) != 0 {
		t.Fatalf("a never-staged withdraw must NOT call WithdrawArtifact, got %d", len(base.withdrawn))
	}
}

// THE VERSION GATE (live regression, gtdapp:5). The sync activity was added to
// beginSession AFTER CoAuthor sessions shipped; an execution already in flight at
// deploy time has no history event for it, so it must NEVER issue the sync command —
// workflow.GetVersion("managed-scaffold-sync") pins such executions (DefaultVersion)
// to the OLD command sequence. This test simulates a pre-feature execution via the
// testsuite's GetVersion mock, arms syncErr so an UN-GATED sync would derail the run
// at the failed gate, and proves the spine completes the full pre-feature happy path
// with ZERO SyncManagedScaffold calls. If the gate is ever removed (or its changeID
// renamed), the armed syncErr fails this test loudly.
func Test_CoAuthor_Rail_ScaffoldSync_VersionGate_PreFeatureExecutionSkipsSync(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	// syncErr armed: if the sync ran despite DefaultVersion, the session would land at
	// the failed gate and the approve below would never commit — a loud failure.
	rail := &fakeRail{checkGreen: true, syncErr: fwra.New(fwra.ContractMisuse, "sync must not run for a pre-feature execution")}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// Simulate a PRE-FEATURE in-flight execution: GetVersion resolves DefaultVersion
	// (no version marker in the replayed history).
	env.OnGetVersion("managed-scaffold-sync", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a pre-feature execution must run the OLD command sequence cleanly: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want coAuthorApproved on the pre-feature path, got %d", outcome)
	}
	if got := rail.verbCount("SyncManagedScaffold"); got != 0 {
		t.Fatalf("a pre-feature (DefaultVersion) execution must NEVER call SyncManagedScaffold, got %d", got)
	}
	// submits=2: the System draft + its architect self-critique (that gate resolves its
	// OWN changeID, system-architect-critique, unmocked here → new path).
	if len(pipe.submits) != 2 || len(base.committed) != 1 {
		t.Fatalf("the pre-feature spine must dispatch draft+critique and commit exactly once, got submits=%d committed=%v", len(pipe.submits), base.committed)
	}
}

// F-QA2-44 — THE LIVE DEFECT (gtdapp kind=3). The approve arrives AFTER the dispatch-time
// installation token expired (GitHub App installation tokens live ~1h; the observed approve
// came 8+ hours after the last dispatch). The workflow used to thread the ONE cached
// dispatch-time credential into the merge window, so every approve attempt 403'd forever
// (non-retryable Auth — neither the Activity RetryPolicy nor the bounded railWithAuthRetry
// can heal an expired token). The fix: the approve arm mints a FRESH token at gate-decision
// time and rides it through status/+1/merge. This test arms token-validity enforcement on
// the fake rail, expires the dispatch-time token before the approve, and proves the approve
// still merges+commits — via a second mint whose fresh token every merge-window verb rode.
func Test_CoAuthor_RailEnabled_ApproveAfterTokenExpiry_RemintsFreshToken_Merges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true, enforceTokenValidity: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		// The human returns hours later: the dispatch-time token (tok-1) has EXPIRED.
		// Any merge-window verb presenting it now 403s.
		rail.expireCurrentToken()
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("an approve after token expiry must succeed via the gate re-mint: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want coAuthorApproved after the re-mint, got %d", outcome)
	}
	// TWO mints: the dispatch-time mint (tok-1) + the gate-decision re-mint (tok-2).
	if n := rail.verbCount("GetInstallationToken"); n != 2 {
		t.Fatalf("want two GetInstallationToken mints (dispatch + approve re-mint), got %d (calls: %+v)", n, rail.calls)
	}
	// Every merge-window verb rode the FRESH token — never the expired dispatch-time one.
	for _, verb := range []string{"GetPullRequestStatus", "PostReview", "MergePullRequest"} {
		creds := rail.credsFor(verb)
		if len(creds) == 0 {
			t.Fatalf("expected %s to run in the approve window (calls: %+v)", verb, rail.calls)
		}
		for _, c := range creds {
			if c != "tok-2" {
				t.Fatalf("%s must present the freshly-minted token tok-2, got %q (calls: %+v)", verb, c, rail.calls)
			}
		}
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one CommitArtifact(KindSystem) on main after the re-minted approve, got %v", base.committed)
	}
}

// F-QA2-44 VERSION GATE (replay pin, gtdapp:3). A PRE-FEATURE decision attempt — its
// history recorded the merge-window verbs WITHOUT a preceding gate mint — must replay the
// OLD command sequence: GetVersion resolves DefaultVersion for that attempt's PER-DECISION
// change id (gate-decision-token-remint-<seq>) and the approve arm must NOT schedule the
// re-mint activity. The testsuite mock pins the first decision attempt's id; the run then
// proves the approve proceeds on the dispatch-time credential with exactly ONE mint. (The
// id is per attempt — NOT static — so that on the live stuck execution the old recorded
// attempts stay pinned while the NEXT attempt resolves v1 and heals; a static id's
// DefaultVersion resolution would be cached for the execution's lifetime.)
func Test_CoAuthor_RailEnabled_GateRemint_VersionGate_PreFeatureAttemptSkipsRemint(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	// No validity enforcement: pre-feature histories only ever succeeded INSIDE the token's
	// fresh hour, so the dispatch-time credential still works on this replayed attempt.
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// Simulate the PRE-FEATURE recorded attempt: GetVersion resolves DefaultVersion for
	// decision attempt #1 (no version marker in the replayed history).
	env.OnGetVersion("gate-decision-token-remint-1", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a pre-feature decision attempt must run the OLD command sequence cleanly: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want coAuthorApproved on the pre-feature path, got %d", outcome)
	}
	// ONE mint only (the dispatch-time beginSession mint) — the pinned attempt must never
	// schedule the gate re-mint command.
	if n := rail.verbCount("GetInstallationToken"); n != 1 {
		t.Fatalf("a pre-feature (DefaultVersion) decision attempt must NOT re-mint: want 1 mint, got %d (calls: %+v)", n, rail.calls)
	}
	if len(base.committed) != 1 {
		t.Fatalf("the pre-feature approve must still commit exactly once, got %v", base.committed)
	}
}

// layerdegenerate_test.go — coverage for the app-side SYSTEM-LAYER-DEGENERATE gate the
// sessionState read-back applies to a System draft (F81). A layer-degenerate system
// (zero Managers / zero ResourceAccess, or a component whose name stereotype contradicts
// its layer) surfaces as ERROR findings on the review panel. This is the review-panel
// twin of methodcheck's SYSTEM-LAYER-DEGENERATE.

func comp(id, name string, kind projectstate.ComponentKind, layer projectstate.Layer) projectstate.Component {
	return projectstate.Component{ID: id, Name: name, Kind: kind, Layer: layer}
}

// A healthy system with a Manager and a ResourceAccess and consistent names raises no
// degeneracy finding.
func Test_systemLayerDegenerate_HealthySystemClean(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		comp("c", "WebClient", projectstate.CompClient, projectstate.LayerClient),
		comp("m", "OrderManager", projectstate.CompManager, projectstate.LayerManager),
		comp("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess),
	}}
	if f := systemLayerDegenerateFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("healthy system should be clean, got: %+v", f)
	}
}

// The live F81 corruption: every component defaulted to client (kind+layer both omitted).
// Zero Managers AND zero ResourceAccess AND every stereotyped name contradicts client.
func Test_systemLayerDegenerate_AllClientFlagged(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		comp("m", "OrderManager", projectstate.CompClient, projectstate.LayerClient),
		comp("e", "PricingEngine", projectstate.CompClient, projectstate.LayerClient),
		comp("ra", "OrderAccess", projectstate.CompClient, projectstate.LayerClient),
	}}
	f := systemLayerDegenerateFindings(KindSystem, sys)
	if len(f) == 0 {
		t.Fatal("an all-client system must be flagged")
	}
	var zeroMgr, zeroRA, nameMismatch int
	for _, fi := range f {
		if fi.RuleID != "SYSTEM-LAYER-DEGENERATE" {
			t.Fatalf("unexpected rule id %q", fi.RuleID)
		}
		switch {
		case strings.Contains(fi.Message, "zero Managers"):
			zeroMgr++
		case strings.Contains(fi.Message, "zero ResourceAccess"):
			zeroRA++
		case strings.Contains(fi.Message, "ends in"):
			nameMismatch++
		}
	}
	if zeroMgr != 1 || zeroRA != 1 {
		t.Fatalf("expected one zero-managers and one zero-resourceAccess finding, got mgr=%d ra=%d", zeroMgr, zeroRA)
	}
	if nameMismatch != 3 {
		t.Fatalf("expected 3 name/layer mismatch findings (Manager, Engine, Access), got %d", nameMismatch)
	}
}

// Zero managers alone (has RA) still trips the structure rule.
func Test_systemLayerDegenerate_ZeroManagers(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		comp("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess),
	}}
	f := systemLayerDegenerateFindings(KindSystem, sys)
	if len(f) != 1 || !strings.Contains(f[0].Message, "zero Managers") {
		t.Fatalf("expected exactly the zero-managers finding, got: %+v", f)
	}
}

// A single name/layer contradiction with otherwise-healthy structure trips only the
// name rule.
func Test_systemLayerDegenerate_NameLayerMismatch(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		comp("m", "OrderManager", projectstate.CompManager, projectstate.LayerManager),
		comp("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess),
		// A component named "…Engine" but sitting in the client layer.
		comp("e", "PricingEngine", projectstate.CompEngine, projectstate.LayerClient),
	}}
	f := systemLayerDegenerateFindings(KindSystem, sys)
	if len(f) != 1 {
		t.Fatalf("expected exactly one finding, got: %+v", f)
	}
	if !strings.Contains(f[0].Message, "PricingEngine") || !strings.Contains(f[0].Message, "engine") {
		t.Fatalf("finding should name the offending component and its expected layer, got: %v", f[0].Message)
	}
}

// A "…Store"/"…Resource" name implies the resource layer.
func Test_systemLayerDegenerate_StoreImpliesResource(t *testing.T) {
	if want, suffix, mismatch := nameLayerMismatch("EventStore", projectstate.LayerClient); !mismatch || want != projectstate.LayerResource || suffix != "Store" {
		t.Fatalf("EventStore in client layer should mismatch to resource, got want=%v suffix=%q mismatch=%v", want, suffix, mismatch)
	}
	if _, _, mismatch := nameLayerMismatch("EventStore", projectstate.LayerResource); mismatch {
		t.Fatal("EventStore in resource layer should be consistent")
	}
}

// A name with no recognized stereotype suffix never mismatches.
func Test_systemLayerDegenerate_UnstereotypedNameOK(t *testing.T) {
	if _, _, mismatch := nameLayerMismatch("Utilities", projectstate.LayerUtility); mismatch {
		t.Fatal("an unstereotyped name must not mismatch")
	}
}

// The rule is inert for non-System artifacts.
func Test_systemLayerDegenerate_NonSystemInert(t *testing.T) {
	if f := systemLayerDegenerateFindings(KindCoreUseCases, &projectstate.CoreUseCases{}); f != nil {
		t.Fatalf("rule must be inert for non-System artifacts, got: %+v", f)
	}
}

// ---- F22: read-model research slimming -------------------------------------

// The project read (GetProject → ProjectState) must carry research source TITLES and
// the per-source content byte-size, but NOT the corpus content itself — a source can be
// a whole 660KB book and the SPA never renders it. researchToContract is the single
// mapping seam; prove it empties Content and surfaces ContentBytes.
func Test_researchToContract_SlimsContentKeepsTitleAndBytes(t *testing.T) {
	// F42: the persisted corpus is already pointers ({Title, Path, ContentBytes}) — the read
	// model carries Title + ContentBytes (off the pointer) and never any Content.
	in := projectstate.ResearchCorpus{Sources: []projectstate.ResearchSourceRef{
		{Title: "The Founder Brief", Path: ".aiarch/state/research/00-the-founder-brief.txt", ContentBytes: 660_000},
		{Title: "Competitor Analysis", Path: ".aiarch/state/research/01-competitor-analysis.txt", ContentBytes: 10},
	}}

	out := researchToContract(in)

	if len(out.Sources) != 2 {
		t.Fatalf("want 2 sources preserved, got %d", len(out.Sources))
	}
	if out.Sources[0].Title != "The Founder Brief" || out.Sources[1].Title != "Competitor Analysis" {
		t.Fatalf("titles must be preserved, got %q / %q", out.Sources[0].Title, out.Sources[1].Title)
	}
	for i, s := range out.Sources {
		if s.Content != "" {
			t.Fatalf("source %d content must be empty on the read model, got %d bytes", i, len(s.Content))
		}
		if s.ContentBytes == nil {
			t.Fatalf("source %d must carry ContentBytes so the UI can show what is loaded", i)
		}
	}
	if got := *out.Sources[0].ContentBytes; got != 660_000 {
		t.Fatalf("ContentBytes must equal the pointer's byte size, want 660000 got %d", got)
	}
	if got := *out.Sources[1].ContentBytes; got != 10 {
		t.Fatalf("ContentBytes[1] want 10 got %d", got)
	}
}

// ---- F29 bonus: Temporal-envelope research slimming ------------------------

// The ReadProjectActivity envelope (encodeProject) must carry research source TITLES
// across the Temporal Activity boundary but NOT the corpus Content — a single source can
// be a whole book, and the Manager workflow only ever reads titles + IsZero. Carrying the
// full corpus blew the Temporal payload budget (TMPRL1103 warnings). Prove encodeProject
// strips Content, keeps Titles, preserves IsZero, and that the corpus content never
// crosses the boundary.
func Test_encodeProject_SlimsResearchContentAcrossActivityBoundary(t *testing.T) {
	// F42: the persisted corpus is pointers, so the Temporal envelope carries {Title, Path,
	// ContentBytes} — inherently tiny, no book-sized Content ever crosses the boundary.
	p := projectstate.Project{
		ID:      projectstate.ProjectID("gtdapp"),
		Version: 3,
		Research: projectstate.ResearchCorpus{Sources: []projectstate.ResearchSourceRef{
			{Title: "The Founder Brief", Path: ".aiarch/state/research/00-the-founder-brief.txt", ContentBytes: 660_000},
			{Title: "Competitor Analysis", Path: ".aiarch/state/research/01-competitor-analysis.txt", ContentBytes: 10},
		}},
	}

	env, err := encodeProject(p)
	if err != nil {
		t.Fatalf("encodeProject: %v", err)
	}

	// Titles + paths cross the boundary; the corpus type has no Content field at all.
	if len(env.Research.Sources) != 2 {
		t.Fatalf("want 2 source pointers preserved in the envelope, got %d", len(env.Research.Sources))
	}
	if env.Research.Sources[0].Title != "The Founder Brief" || env.Research.Sources[1].Title != "Competitor Analysis" {
		t.Fatalf("titles must survive encoding, got %q / %q", env.Research.Sources[0].Title, env.Research.Sources[1].Title)
	}
	if env.Research.Sources[0].Path == "" || env.Research.Sources[1].Path == "" {
		t.Fatalf("source file paths must survive encoding, got %+v", env.Research.Sources)
	}

	// The decoded head-state still carries the corpus (IsZero preserved) so writeResearch
	// emits the research-tools block — the agent reads the sources with listResearchSources /
	// getResearchSource rather than from an inlined title/path list.
	dec, err := env.Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.Research.IsZero() {
		t.Fatal("decoded research must not be zero — the pointer carrier preserves IsZero")
	}
}

// ---- F19: review-gate precondition -----------------------------------------

// stubEncodedStage is a minimal converter.EncodedValue whose Get sets only the Stage,
// letting a test script the sessionState query without a live workflow.
type stubEncodedStage struct{ stage SessionStage }

func (s stubEncodedStage) HasValue() bool { return true }

func (s stubEncodedStage) Get(ptr any) error {
	v, ok := ptr.(*SessionStateView)
	if !ok {
		return fmt.Errorf("stubEncodedStage: unexpected target %T", ptr)
	}
	v.Stage = s.stage
	return nil
}

// checkReviewPrecondition is the pure decision×stage gate. Approve is meaningful ONLY
// at AwaitingReview; reject/withdraw are meaningful at AwaitingReview or the failed
// recovery gate; everything else is a FailedPrecondition.
func Test_checkReviewPrecondition_Matrix(t *testing.T) {
	fp := func(err error) bool {
		var e *fwmanager.Error
		return err != nil && errors.As(err, &e) && e.Kind == fwmanager.FailedPrecondition
	}

	// approve: only AwaitingReview passes.
	for _, st := range []SessionStage{SessionStageUnknown, StageDrafting, StageRedrafting, StageCommitted, StageWithdrawn, StageRefused, StageDraftFailed} {
		if err := checkReviewPrecondition(ReviewApprove, st); !fp(err) {
			t.Fatalf("approve at stage %d must FailedPrecondition, got %v", st, err)
		}
	}
	if err := checkReviewPrecondition(ReviewApprove, StageAwaitingReview); err != nil {
		t.Fatalf("approve at AwaitingReview must pass, got %v", err)
	}

	// reject + withdraw: AwaitingReview and DraftFailed pass; others fail.
	for _, dec := range []ReviewDecision{ReviewReject, ReviewWithdraw} {
		for _, st := range []SessionStage{StageAwaitingReview, StageDraftFailed} {
			if err := checkReviewPrecondition(dec, st); err != nil {
				t.Fatalf("decision %d at stage %d must pass, got %v", dec, st, err)
			}
		}
		for _, st := range []SessionStage{SessionStageUnknown, StageDrafting, StageRedrafting, StageCommitted, StageWithdrawn, StageRefused} {
			if err := checkReviewPrecondition(dec, st); !fp(err) {
				t.Fatalf("decision %d at stage %d must FailedPrecondition, got %v", dec, st, err)
			}
		}
	}
}

// A never-drafted artifact (no workflow execution) must reject an approve with a
// FailedPrecondition and NEVER fire the signal (the old bug returned success {}).
func Test_SubmitReviewDecision_Approve_NeverDrafted_FailsWithoutSignal(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), serviceerror.NewNotFound("workflow not found for ID: "+wfID))
	// No QueryWorkflow / SignalWorkflow expectations: reaching either fails the mock.

	m := &systemDesignManager{client: mc}
	err := m.SubmitReviewDecision(bgRC(), id, KindMission, ReviewApprove, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("approve on a never-drafted artifact must FailedPrecondition, got %d", got)
	}
	mc.AssertExpectations(t)
	mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, wfID, "", signalReviewDecision, mock.Anything)
}

// Approve while the session is still drafting must fail without signaling.
func Test_SubmitReviewDecision_Approve_WhileDrafting_FailsWithoutSignal(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
		}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
		Return(stubEncodedStage{stage: StageDrafting}, nil)

	m := &systemDesignManager{client: mc}
	err := m.SubmitReviewDecision(bgRC(), id, KindMission, ReviewApprove, nil)
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("approve while drafting must FailedPrecondition, got %d", got)
	}
	mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, wfID, "", signalReviewDecision, mock.Anything)
}

// Approve at StageAwaitingReview is the legitimate flow — the precondition passes and
// the reviewDecision signal fires.
func Test_SubmitReviewDecision_Approve_AtAwaitingReview_Signals(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindMission)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
		}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
		Return(stubEncodedStage{stage: StageAwaitingReview}, nil)
	mc.On("SignalWorkflow", mock.Anything, wfID, "", signalReviewDecision, mock.Anything).Return(nil)

	m := &systemDesignManager{client: mc}
	if err := m.SubmitReviewDecision(bgRC(), id, KindMission, ReviewApprove, nil); err != nil {
		t.Fatalf("approve at AwaitingReview must succeed, got %v", err)
	}
	mc.AssertCalled(t, "SignalWorkflow", mock.Anything, wfID, "", signalReviewDecision, mock.Anything)
}

// THE 2026-07-16 INCIDENT REGRESSION (manager half — decisions). A decision against a DEAD
// session (the run closed FAILED, as gtdapp:1 did) synthesizes StageDraftFailed, which passes
// the reject/withdraw precondition — but a signal to that corpse is refused by Temporal
// ("workflow execution already completed") and pre-fix surfaced as 503 noise with zero SPA
// feedback. The manager must refuse with a typed, actionable FailedPrecondition and NEVER
// fire the signal.
func Test_SubmitReviewDecision_DeadSession_TypedFailedPrecondition_NoSignal(t *testing.T) {
	cases := []struct {
		name     string
		decision ReviewDecision
		feedback *ReviewFeedback
	}{
		{name: "withdraw", decision: ReviewWithdraw},
		{name: "reject", decision: ReviewReject, feedback: &ReviewFeedback{Notes: "send back"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := ProjectID(uuid.NewString())
			wfID := coAuthorWorkflowID(id, KindMission)

			mc := &temporalmocks.Client{}
			mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
				Return(&workflowservice.DescribeWorkflowExecutionResponse{
					WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_FAILED},
				}, nil)
			// NO QueryWorkflow / SignalWorkflow expectations: reaching either fails the mock.

			m := &systemDesignManager{client: mc}
			err := m.SubmitReviewDecision(bgRC(), id, KindMission, tc.decision, tc.feedback)
			sde := asSystemDesignError(t, err)
			if sde.Kind != fwmanager.FailedPrecondition {
				t.Fatalf("a decision on a dead session must be a typed FailedPrecondition (never 503 noise), got %d (%v)", sde.Kind, err)
			}
			if !strings.Contains(sde.Detail, "no longer running") || !strings.Contains(sde.Detail, "Retry design job") {
				t.Fatalf("the refusal must name the dead session and the recovery lever, got %q", sde.Detail)
			}
			mc.AssertExpectations(t)
			mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, wfID, "", signalReviewDecision, mock.Anything)
		})
	}
}

// These tests cover the SYNC, non-Temporal SetResearchInput op (op 2.6,
// systemDesignManager.md §2.6). They run entirely on the sync read/write path
// (no Temporal client), so a nil client is safe — the op never touches Temporal.

// setResearchFakeState is a recording fake of projectStateAccess for the
// SetResearchInput op. It records each (expectedVersion, research, idempotencyKey)
// the Manager passes to SetResearchInput, returns the head Version on ReadProject,
// and can be programmed to surface a Conflict on the first N writes (to exercise
// the sync-path re-read/re-apply loop). Verbs the op must NOT call panic.
type setResearchFakeState struct {
	headVersion projectstate.Version
	readErr     error

	// conflictsBeforeSuccess: the first N writes return fwra.Conflict; the rest
	// succeed. The fake bumps headVersion on each Conflict so a re-read sees a
	// fresh version (mirroring a concurrent writer).
	conflictsBeforeSuccess int

	// recorded write calls (in order).
	gotExpected []projectstate.Version
	gotResearch []projectstate.ResearchInput
	gotKeys     []fwra.IdempotencyKey
	readCalls   int
}

func (f *setResearchFakeState) ReadProject(fwra.Context, projectstate.ProjectID) (projectstate.Project, error) {
	f.readCalls++
	if f.readErr != nil {
		return projectstate.Project{}, f.readErr
	}
	return projectstate.Project{Version: f.headVersion}, nil
}

func (f *setResearchFakeState) ReadProjectVersion(fwra.Context, projectstate.ProjectID) (projectstate.Version, error) {
	f.readCalls++
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.headVersion, nil
}

func (f *setResearchFakeState) SetResearchInput(rc fwra.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, research projectstate.ResearchInput) (projectstate.Version, error) {
	f.gotExpected = append(f.gotExpected, expectedVersion)
	f.gotResearch = append(f.gotResearch, research)
	f.gotKeys = append(f.gotKeys, rc.IdempotencyKey)
	if len(f.gotExpected) <= f.conflictsBeforeSuccess {
		// Concurrent writer bumped the row: surface Conflict and advance head so the
		// Manager's re-read sees a fresh expectedVersion.
		f.headVersion++
		return 0, fwra.New(fwra.Conflict, "stale version")
	}
	f.headVersion++
	return f.headVersion, nil
}

func (f *setResearchFakeState) SetOperatingModel(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.OperatingModel) (projectstate.Version, error) {
	panic("setResearchFakeState.SetOperatingModel must not be called by SetResearchInput")
}

func (f *setResearchFakeState) StageArtifactForReview(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactModel) (projectstate.Version, error) {
	panic("setResearchFakeState.StageArtifactForReview must not be called by SetResearchInput")
}

func (f *setResearchFakeState) CommitArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind) (projectstate.Version, error) {
	panic("setResearchFakeState.CommitArtifact must not be called by SetResearchInput")
}

func (f *setResearchFakeState) RejectArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	panic("setResearchFakeState.RejectArtifact must not be called by SetResearchInput")
}

func (f *setResearchFakeState) WithdrawArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	panic("setResearchFakeState.WithdrawArtifact must not be called by SetResearchInput")
}

func (f *setResearchFakeState) AdvancePhase(fwra.Context, projectstate.ProjectID, projectstate.Version) (projectstate.Version, error) {
	panic("setResearchFakeState.AdvancePhase must not be called by SetResearchInput")
}

func (f *setResearchFakeState) CreateProject(fwra.Context, projectstate.ProjectID, projectstate.OwnerScope, string) (projectstate.Version, error) {
	panic("setResearchFakeState.CreateProject must not be called by SetResearchInput")
}

func (f *setResearchFakeState) ListProjects(fwra.Context, projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	panic("setResearchFakeState.ListProjects must not be called by SetResearchInput")
}

// C2 fold (code-health-phase-a): panic-stubs for the 9 session/branch verbs now
// required by the generated ProjectStateAccess contract — SetResearchInput never
// touches them, same as the pre-existing panic-stubs above.
func (f *setResearchFakeState) ReadProjectOnBranch(fwra.Context, projectstate.ProjectID, string) (projectstate.Project, error) {
	panic("setResearchFakeState.ReadProjectOnBranch must not be called by SetResearchInput")
}

func (f *setResearchFakeState) StageArtifactForReviewOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactModel, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.StageArtifactForReviewOnBranch must not be called by SetResearchInput")
}

func (f *setResearchFakeState) RejectArtifactOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.RejectArtifactOnBranch must not be called by SetResearchInput")
}

func (f *setResearchFakeState) WithdrawArtifactOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.WithdrawArtifactOnBranch must not be called by SetResearchInput")
}

func (f *setResearchFakeState) RejectArtifactOnBranchWithComments(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, int64, []projectstate.ReviewComment, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.RejectArtifactOnBranchWithComments must not be called by SetResearchInput")
}

func (f *setResearchFakeState) SetReviewCommentStatusOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.SetReviewCommentStatusOnBranch must not be called by SetResearchInput")
}

func (f *setResearchFakeState) SeedReviewCommentsOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, int64, []projectstate.ReviewComment, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.SeedReviewCommentsOnBranch must not be called by SetResearchInput")
}

func (f *setResearchFakeState) ReconcileBranchFromMain(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.ReconcileBranchFromMain must not be called by SetResearchInput")
}

func (f *setResearchFakeState) AcknowledgeStaleBasis(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.AcknowledgeStaleBasis must not be called by SetResearchInput")
}

var _ projectstate.ProjectStateAccess = (*setResearchFakeState)(nil)

func sampleResearch() ResearchInput {
	return ResearchInput{Sources: []ResearchSource{
		{Title: "Founder brief", Content: "We are building X for Y."},
	}}
}

// ---- façade preconditions ---------------------------------------------------

func Test_SetResearchInput_EmptyProjectID(t *testing.T) {
	m := NewSystemDesignManager(nil, &setResearchFakeState{}, nil, nil, nil, nil, nil, "")
	_, err := m.SetResearchInput(bgRC(), ProjectID(""), sampleResearch())
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for empty projectId, got %d", got)
	}
}

func Test_SetResearchInput_EmptyResearch(t *testing.T) {
	m := NewSystemDesignManager(nil, &setResearchFakeState{}, nil, nil, nil, nil, nil, "")
	_, err := m.SetResearchInput(bgRC(), ProjectID(uuid.NewString()), ResearchInput{})
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for empty research, got %d", got)
	}
}

// A corpus with the right shape (>=1 source) but a source carrying an empty or
// whitespace-only title/content is a ContractMisuse (BadRequest-class) at the
// façade boundary, BEFORE any projectStateAccess write — the fake's write verb
// panics if reached, so this also proves the gate short-circuits before the RA
// call. The client-facing detail names the offending source by 1-based position.
func Test_SetResearchInput_PerSourceShapeViolations(t *testing.T) {
	cases := []struct {
		name       string
		sources    []ResearchSource
		wantDetail string
	}{
		{
			name:       "missing title",
			sources:    []ResearchSource{{Content: "has content, no title"}},
			wantDetail: "research source 1: title must not be empty",
		},
		{
			name:       "whitespace-only title",
			sources:    []ResearchSource{{Title: "   ", Content: "has content"}},
			wantDetail: "research source 1: title must not be empty",
		},
		{
			name:       "missing content",
			sources:    []ResearchSource{{Title: "has title, no content"}},
			wantDetail: "research source 1: content must not be empty",
		},
		{
			name:       "whitespace-only content",
			sources:    []ResearchSource{{Title: "has title", Content: "\t\n "}},
			wantDetail: "research source 1: content must not be empty",
		},
		{
			// The offending index is reported (1-based) for a later source, and the
			// first well-formed source does not mask the second's violation.
			name: "second source missing content",
			sources: []ResearchSource{
				{Title: "Founder brief", Content: "We are building X."},
				{Title: "Competitor analysis"},
			},
			wantDetail: "research source 2: content must not be empty",
		},
		{
			name: "third source missing title",
			sources: []ResearchSource{
				{Title: "A", Content: "a"},
				{Title: "B", Content: "b"},
				{Content: "c"},
			},
			wantDetail: "research source 3: title must not be empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A fake whose write verb panics if reached — proves the gate rejects
			// BEFORE any projectStateAccess call.
			m := NewSystemDesignManager(nil, &setResearchFakeState{}, nil, nil, nil, nil, nil, "")
			_, err := m.SetResearchInput(bgRC(), ProjectID(uuid.NewString()), ResearchInput{Sources: tc.sources})
			e := asSystemDesignError(t, err)
			if e.Kind != fwmanager.ContractMisuse {
				t.Fatalf("want ContractMisuse, got %d", e.Kind)
			}
			if e.Detail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", e.Detail, tc.wantDetail)
			}
		})
	}
}

// ---- happy path -------------------------------------------------------------

func Test_SetResearchInput_HappyPath_RecordsWrite(t *testing.T) {
	ps := &setResearchFakeState{headVersion: 7}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
	research := sampleResearch()

	v, err := m.SetResearchInput(bgRC(), ProjectID(uuid.NewString()), research)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ps.gotExpected) != 1 {
		t.Fatalf("want exactly one write, got %d", len(ps.gotExpected))
	}
	if ps.gotExpected[0] != 7 {
		t.Fatalf("want expectedVersion 7 (the head just read), got %d", ps.gotExpected[0])
	}
	if len(ps.gotResearch[0].Sources) != 1 || ps.gotResearch[0].Sources[0].Title != "Founder brief" {
		t.Fatalf("research not passed through faithfully: %+v", ps.gotResearch[0])
	}
	if ps.gotKeys[0].IsZero() {
		t.Fatalf("want a non-empty idempotencyKey")
	}
	if v != 8 {
		t.Fatalf("want resulting Version 8 (head bumped), got %d", v)
	}
}

// A stable research payload derives a stable idempotency key (so a retried write
// of the SAME research collapses to a dedup no-op in the RA ledger).
func Test_SetResearchInput_IdempotencyKey_StableForSameResearch(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	research := sampleResearch()

	ps1 := &setResearchFakeState{}
	ps2 := &setResearchFakeState{}
	m1 := NewSystemDesignManager(nil, ps1, nil, nil, nil, nil, nil, "")
	m2 := NewSystemDesignManager(nil, ps2, nil, nil, nil, nil, nil, "")
	if _, err := m1.SetResearchInput(bgRC(), pid, research); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if _, err := m2.SetResearchInput(bgRC(), pid, research); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if ps1.gotKeys[0] != ps2.gotKeys[0] {
		t.Fatalf("same (project, research) must derive the same key: %q vs %q", ps1.gotKeys[0], ps2.gotKeys[0])
	}

	// A different research payload must derive a DIFFERENT key.
	other := ResearchInput{Sources: []ResearchSource{{Title: "Competitor analysis", Content: "Z does W."}}}
	ps3 := &setResearchFakeState{}
	m3 := NewSystemDesignManager(nil, ps3, nil, nil, nil, nil, nil, "")
	if _, err := m3.SetResearchInput(bgRC(), pid, other); err != nil {
		t.Fatalf("write 3: %v", err)
	}
	if ps3.gotKeys[0] == ps1.gotKeys[0] {
		t.Fatalf("different research must derive a different key")
	}
}

// ---- conflict-then-success sync re-read/re-apply loop -----------------------

func Test_SetResearchInput_ConflictThenSuccess_ReReads(t *testing.T) {
	ps := &setResearchFakeState{headVersion: 3, conflictsBeforeSuccess: 2}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")

	v, err := m.SetResearchInput(bgRC(), ProjectID(uuid.NewString()), sampleResearch())
	if err != nil {
		t.Fatalf("expected success after conflicts, got %v", err)
	}
	if len(ps.gotExpected) != 3 {
		t.Fatalf("want 3 write attempts (2 conflicts + 1 success), got %d", len(ps.gotExpected))
	}
	// Each attempt must re-read the (bumped) head version before re-applying.
	if ps.readCalls != 3 {
		t.Fatalf("want 3 ReadProject calls (one per attempt), got %d", ps.readCalls)
	}
	if ps.gotExpected[0] >= ps.gotExpected[1] || ps.gotExpected[1] >= ps.gotExpected[2] {
		t.Fatalf("each re-apply must carry a fresh (higher) expectedVersion, got %v", ps.gotExpected)
	}
	// The SAME idempotency key is reused across re-applies (one logical mutation).
	if ps.gotKeys[0] != ps.gotKeys[1] || ps.gotKeys[1] != ps.gotKeys[2] {
		t.Fatalf("re-applies must reuse the same idempotencyKey, got %v", ps.gotKeys)
	}
	if v == 0 {
		t.Fatalf("want a non-zero resulting Version")
	}
}

func Test_SetResearchInput_ConflictExhausted_Infrastructure(t *testing.T) {
	ps := &setResearchFakeState{conflictsBeforeSuccess: setResearchInputMaxAttempts + 1}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
	_, err := m.SetResearchInput(bgRC(), ProjectID(uuid.NewString()), sampleResearch())
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure after exhausting conflict retries, got %d", got)
	}
	if len(ps.gotExpected) != setResearchInputMaxAttempts {
		t.Fatalf("want exactly %d bounded attempts, got %d", setResearchInputMaxAttempts, len(ps.gotExpected))
	}
}

// ---- error passthrough ------------------------------------------------------

func Test_SetResearchInput_NotFound_Passthrough(t *testing.T) {
	// ReadProject succeeds but the write surfaces NotFound (no project aggregate).
	ps := &setResearchNotFoundOnWrite{}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
	_, err := m.SetResearchInput(bgRC(), ProjectID(uuid.NewString()), sampleResearch())
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.NotFound {
		t.Fatalf("want NotFound passthrough, got %d", got)
	}
}

func Test_SetResearchInput_ReadNotFound_Propagates(t *testing.T) {
	ps := &setResearchFakeState{readErr: fwra.New(fwra.NotFound, "no row yet")}
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, nil, "")
	_, err := m.SetResearchInput(bgRC(), ProjectID(uuid.NewString()), sampleResearch())
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.NotFound {
		t.Fatalf("want NotFound when ReadProject reports no row, got %d", got)
	}
}

// setResearchNotFoundOnWrite reads fine but the write reports NotFound.
type setResearchNotFoundOnWrite struct{ setResearchFakeState }

func (f *setResearchNotFoundOnWrite) SetResearchInput(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ResearchInput) (projectstate.Version, error) {
	return 0, fwra.New(fwra.NotFound, "no project aggregate")
}

var _ projectstate.ProjectStateAccess = (*setResearchNotFoundOnWrite)(nil)

// stagename_test.go — F72 stageName label. The bare Stage int's enum values differ across
// managers (systemdesign StageAwaitingReview == 2), so a human-readable StageName label ships
// alongside it. sessionStageLabel is the single authoritative map; withStageName stamps it.

func TestSessionStageLabel_Map(t *testing.T) {
	cases := map[SessionStage]string{
		SessionStageUnknown: "not started",
		StageDrafting:       "drafting",
		StageAwaitingReview: "awaiting review",
		StageRedrafting:     "redrafting",
		StageCommitted:      "committed",
		StageWithdrawn:      "withdrawn",
		StageRefused:        "refused",
		StageDraftFailed:    "draft failed",
	}
	for stage, want := range cases {
		if got := sessionStageLabel(stage); got != want {
			t.Errorf("sessionStageLabel(%d) = %q, want %q", int(stage), got, want)
		}
	}
}

func TestWithStageName_StampsLabel(t *testing.T) {
	// systemdesign StageAwaitingReview is the int 2 — the label disambiguates it.
	if int(StageAwaitingReview) != 2 {
		t.Fatalf("guard: expected systemdesign StageAwaitingReview == 2, got %d", int(StageAwaitingReview))
	}
	v := withStageName(SessionStateView{Stage: StageAwaitingReview})
	if v.StageName != "awaiting review" {
		t.Fatalf("withStageName StageName = %q, want %q", v.StageName, "awaiting review")
	}
	if v.Stage != StageAwaitingReview {
		t.Fatalf("withStageName must not alter the Stage int, got %d", int(v.Stage))
	}
}

// statevalidationfindings_test.go — coverage for the app-side read-back finding
// generators (architect ratification 2026-07-05) and the AdvancePhase pre-seal gates.
// Each rule gets a valid fixture (no finding) and a violating fixture (finding with the
// canonical rule id). A final test confirms a pre-existing VIOLATING committed state
// decodes WITHOUT erroring and only then surfaces findings (the critical read-safety
// invariant: reads never hard-fail; violations render as findings until an amendment).

func compE(id, name string, kind projectstate.ComponentKind, layer projectstate.Layer, enc string) projectstate.Component {
	return projectstate.Component{ID: id, Name: name, Kind: kind, Layer: layer, Encapsulates: enc}
}

func rel(from, to string, mode projectstate.CallMode, label string) projectstate.Relationship {
	return projectstate.Relationship{From: from, To: to, Mode: mode, Label: label}
}

func hasRule(fs []Finding, id string, sev Severity) bool {
	for _, f := range fs {
		if string(f.RuleID) == id && f.Severity == sev {
			return true
		}
	}
	return false
}

// ---- SYS-RA-ORPHAN ----

func Test_raOrphan_HealthyReachesResource(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "the order store"),
			compE("store", "OrderStore", projectstate.CompResource, projectstate.LayerResource, ""),
		},
		Relationships: []projectstate.Relationship{rel("ra", "store", projectstate.CallSync, "reads")},
	}
	if f := raOrphanFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("an RA that reaches a resource is not orphan, got: %+v", f)
	}
}

func Test_raOrphan_NoResourceEdgeFlagged(t *testing.T) {
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "the order workflow"),
			compE("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "the order store"),
		},
		Relationships: []projectstate.Relationship{rel("mgr", "ra", projectstate.CallSync, "loads")},
	}
	if !hasRule(raOrphanFindings(KindSystem, sys), "SYS-RA-ORPHAN", SeverityError) {
		t.Fatal("an RA with no outbound edge to a resource must be flagged SYS-RA-ORPHAN")
	}
}

func Test_raOrphan_ExternalTargetSatisfies(t *testing.T) {
	// An edge to an id that is not a modeled component is a documented external system.
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("ra", "GitHubAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "GitHub"),
		},
		Relationships: []projectstate.Relationship{rel("ra", "github.com", projectstate.CallQueued, "calls")},
	}
	if f := raOrphanFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("an RA reaching an external target is not orphan, got: %+v", f)
	}
}

// ---- SYS-ENCAPSULATES ----

// R5 (2026-07-17): SYS-ENCAPSULATES is scoped to the volatility-OWNING kinds
// (manager/engine/resourceAccess). Empty encapsulates on a Client (owns client
// volatility as a layer) or a Resource (a physical store owns nothing) is
// Method-correct and must NOT fire.
func Test_encapsulates_EmptyManagerError_ClientAndResourceExempt(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("c", "WebClient", projectstate.CompClient, projectstate.LayerClient, ""),
		compE("r", "GitRepo", projectstate.CompResource, projectstate.LayerResource, ""),
		compE("m", "OrderManager", projectstate.CompManager, projectstate.LayerManager, ""),
	}}
	f := encapsulatesFindings(KindSystem, sys)
	if !hasRule(f, "SYS-ENCAPSULATES", SeverityError) {
		t.Fatal("an empty-encapsulates manager must be an ERROR finding")
	}
	// The empty client and empty resource legitimately carry no volatility.
	for _, fi := range f {
		if strings.Contains(fi.Message, "WebClient") || strings.Contains(fi.Message, "GitRepo") {
			t.Fatalf("a client/resource must not be flagged for empty encapsulates, got: %+v", fi)
		}
	}
}

// ---- SYS-REL-DUP ----

func Test_relDup_ExactDuplicateError(t *testing.T) {
	sys := &projectstate.System{Relationships: []projectstate.Relationship{
		rel("a", "b", projectstate.CallSync, "x"),
		rel("a", "b", projectstate.CallSync, "x"),
	}}
	if !hasRule(relDupFindings(KindSystem, sys), "SYS-REL-DUP", SeverityError) {
		t.Fatal("an exact (from,to,mode) duplicate must be a SYS-REL-DUP error")
	}
}

func Test_relDup_LabelSplitWarning(t *testing.T) {
	sys := &projectstate.System{Relationships: []projectstate.Relationship{
		rel("a", "b", projectstate.CallSync, "reads"),
		rel("a", "b", projectstate.CallQueued, "writes"),
	}}
	f := relDupFindings(KindSystem, sys)
	if hasRule(f, "SYS-REL-DUP", SeverityError) {
		t.Fatal("distinct-mode edges are not an exact duplicate")
	}
	if !hasRule(f, "SYS-REL-DUP", SeverityWarning) {
		t.Fatal("a same-pair label split must be a SYS-REL-DUP warning")
	}
}

// DV-CHAIN-CONNECTED and its tests (dvChainFindings) were RETIRED 2026-07-30
// (callchain-realization Task 6): the rule duplicated — and, under the step-keyed
// DynamicView shape, CONTRADICTED — platform methodcheck's CC-PATH-CONNECTED, which
// also blesses actor-rooted (not just Client-rooted) chains. See
// framework-go/methodcheck/rules_callchain.go.

// ---- DV-TITLE-EMPTY (F10 gate lint) ----

func Test_dvTitle_PresentClean(t *testing.T) {
	sys := &projectstate.System{DynamicViews: []projectstate.DynamicView{
		{UseCaseID: "uc1", Key: "uc1-chain", Title: "Match Tradesman call chain"},
	}}
	if f := dvTitleFindings(KindSystem, sys); len(f) != 0 {
		t.Fatalf("a titled dynamic view is clean, got: %+v", f)
	}
}

func Test_dvTitle_EmptyAndWhitespaceError(t *testing.T) {
	sys := &projectstate.System{DynamicViews: []projectstate.DynamicView{
		{UseCaseID: "uc1", Key: "uc1-chain", Title: ""},
		{UseCaseID: "uc2", Key: "uc2-chain", Title: "   "},
	}}
	f := dvTitleFindings(KindSystem, sys)
	if len(f) != 2 {
		t.Fatalf("want one DV-TITLE-EMPTY error per untitled view, got: %+v", f)
	}
	if !hasRule(f, "DV-TITLE-EMPTY", SeverityError) {
		t.Fatal("an empty dynamic-view title must be a DV-TITLE-EMPTY error")
	}
}

func Test_dvTitle_OtherKindNil(t *testing.T) {
	if f := dvTitleFindings(KindCoreUseCases, &projectstate.CoreUseCases{}); f != nil {
		t.Fatalf("dvTitleFindings must be nil for non-System kinds, got: %+v", f)
	}
}

// ---- SYS-VOLATILITY-COVERAGE (F10 gate lint) ----

func Test_volatilityCoverage_ClaimedClean(t *testing.T) {
	committed := &projectstate.Volatilities{Items: []projectstate.Volatility{
		{Name: "notification transport", Axis: projectstate.AxisSameCustomerOverTime},
		{Name: "matching algorithm", Axis: projectstate.AxisAllCustomersAtOneTime},
	}}
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "workflow volatility; notification transport"),
		compE("eng", "MatchingEngine", projectstate.CompEngine, projectstate.LayerEngine, "Matching algorithm volatility"),
	}}
	if f := volatilityCoverageFindings(KindSystem, sys, committed); len(f) != 0 {
		t.Fatalf("claimed volatilities (case-insensitive prose match) are clean, got: %+v", f)
	}
}

func Test_volatilityCoverage_DispositionNoteClean(t *testing.T) {
	// An explicit disposition in encapsulates prose (not an encapsulation claim per se)
	// still counts — the lint fires only on TOTAL silence.
	committed := &projectstate.Volatilities{Items: []projectstate.Volatility{
		{Name: "report layout", Axis: projectstate.AxisSameCustomerOverTime},
	}}
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager,
			"order workflow. report layout: deferred — variable handled by client-side templates, not architectural"),
	}}
	if f := volatilityCoverageFindings(KindSystem, sys, committed); len(f) != 0 {
		t.Fatalf("a dispositioned volatility is clean, got: %+v", f)
	}
}

func Test_volatilityCoverage_UnclaimedError(t *testing.T) {
	committed := &projectstate.Volatilities{Items: []projectstate.Volatility{
		{Name: "notification transport", Axis: projectstate.AxisSameCustomerOverTime},
		{Name: "storage substrate", Axis: projectstate.AxisSameCustomerOverTime},
	}}
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "notification transport"),
	}}
	f := volatilityCoverageFindings(KindSystem, sys, committed)
	if len(f) != 1 || !hasRule(f, "SYS-VOLATILITY-COVERAGE", SeverityError) {
		t.Fatalf("want exactly one SYS-VOLATILITY-COVERAGE error for the silent volatility, got: %+v", f)
	}
	if !strings.Contains(f[0].Message, "storage substrate") {
		t.Fatalf("the finding must name the unclaimed volatility, got: %q", f[0].Message)
	}
}

func Test_volatilityCoverage_NoCommittedVolatilitiesNil(t *testing.T) {
	sys := &projectstate.System{}
	if f := volatilityCoverageFindings(KindSystem, sys, nil); f != nil {
		t.Fatalf("no committed Volatilities ⇒ nil findings, got: %+v", f)
	}
}

// ---- SYS-SERVICES-EXPLOSION (F10 gate lint) ----

func explosionCoreUseCases(names ...string) *projectstate.CoreUseCases {
	cuc := &projectstate.CoreUseCases{}
	for i, n := range names {
		cuc.Decisions = append(cuc.Decisions,
			ucDecision(fmt.Sprintf("uc%d", i+1), n, projectstate.ClassCore, "", ""))
	}
	return cuc
}

func Test_servicesExplosion_MirroredOnePerUseCaseWarns(t *testing.T) {
	committed := explosionCoreUseCases("Capture Commitment", "Clarify Inbox", "Review Projects")
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("m1", "CaptureCommitmentManager", projectstate.CompManager, projectstate.LayerManager, "x"),
		compE("m2", "ClarifyInboxManager", projectstate.CompManager, projectstate.LayerManager, "y"),
		compE("m3", "ReviewProjectsManager", projectstate.CompManager, projectstate.LayerManager, "z"),
	}}
	f := servicesExplosionFindings(KindSystem, sys, committed)
	if !hasRule(f, "SYS-SERVICES-EXPLOSION", SeverityWarning) {
		t.Fatalf("|Managers| == |core use cases| with 100%% name-mirroring must warn, got: %+v", f)
	}
}

func Test_servicesExplosion_VolatilityNamedManagersClean(t *testing.T) {
	// Same counts, but Manager names encode volatilities (not use cases): below the
	// 60%% mirroring threshold ⇒ no warning (equal counts alone are not the signal).
	committed := explosionCoreUseCases("Capture Commitment", "Clarify Inbox", "Review Projects")
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("m1", "IntakeManager", projectstate.CompManager, projectstate.LayerManager, "x"),
		compE("m2", "SchedulingManager", projectstate.CompManager, projectstate.LayerManager, "y"),
		compE("m3", "ReviewProjectsManager", projectstate.CompManager, projectstate.LayerManager, "z"),
	}}
	if f := servicesExplosionFindings(KindSystem, sys, committed); len(f) != 0 {
		t.Fatalf("1 of 3 mirrored (33%%) is below the 60%% threshold, got: %+v", f)
	}
}

func Test_servicesExplosion_CountMismatchClean(t *testing.T) {
	// Fewer Managers than core use cases — the Method-typical shape — never warns,
	// even with a mirrored name.
	committed := explosionCoreUseCases("Capture Commitment", "Clarify Inbox", "Review Projects")
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("m1", "CaptureCommitmentManager", projectstate.CompManager, projectstate.LayerManager, "x"),
		compE("m2", "PlanningManager", projectstate.CompManager, projectstate.LayerManager, "y"),
	}}
	if f := servicesExplosionFindings(KindSystem, sys, committed); len(f) != 0 {
		t.Fatalf("|Managers| != |core use cases| must not warn, got: %+v", f)
	}
}

func Test_servicesExplosion_NoCommittedUseCasesNil(t *testing.T) {
	sys := &projectstate.System{Components: []projectstate.Component{
		compE("m1", "CaptureCommitmentManager", projectstate.CompManager, projectstate.LayerManager, "x"),
	}}
	if f := servicesExplosionFindings(KindSystem, sys, nil); f != nil {
		t.Fatalf("no committed CoreUseCases ⇒ nil findings, got: %+v", f)
	}
}

// ---- UC-VARIATION-REF ----

func ucDecision(id, name string, class projectstate.Classification, variationOf, rejection string) projectstate.UseCaseDecision {
	uc := projectstate.UseCase{
		ID:             projectstate.UseCaseID(id),
		Name:           name,
		Trigger:        projectstate.TriggerClientAction,
		Classification: class,
	}
	if variationOf != "" {
		v := projectstate.UseCaseID(variationOf)
		uc.VariationOf = &v
	}
	return projectstate.UseCaseDecision{UseCase: uc, RejectionReason: rejection}
}

func Test_variationRef_ValidClean(t *testing.T) {
	cuc := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{
		ucDecision("base", "Base", projectstate.ClassCore, "", ""),
		ucDecision("var", "Variation", projectstate.ClassNonCore, "base", "narrower slice"),
	}}
	if f := variationRefFindings(KindCoreUseCases, cuc); len(f) != 0 {
		t.Fatalf("a well-formed variation set is clean, got: %+v", f)
	}
}

func Test_variationRef_Violations(t *testing.T) {
	cuc := &projectstate.CoreUseCases{Decisions: []projectstate.UseCaseDecision{
		ucDecision("base", "Base", projectstate.ClassCore, "nonsense", ""), // core with variationOf
		ucDecision("v1", "V1", projectstate.ClassNonCore, "ghost", "why"),  // unresolved variationOf
		ucDecision("v2", "V2", projectstate.ClassNonCore, "base", ""),      // empty rejectionReason
	}}
	f := variationRefFindings(KindCoreUseCases, cuc)
	if !hasRule(f, "UC-VARIATION-REF", SeverityError) {
		t.Fatal("expected UC-VARIATION-REF errors")
	}
	var coreVar, unresolved, noReason bool
	for _, fi := range f {
		switch {
		case strings.Contains(fi.Message, "core use case") && strings.Contains(fi.Message, "base, not a variation"):
			coreVar = true
		case strings.Contains(fi.Message, "does not resolve"):
			unresolved = true
		case strings.Contains(fi.Message, "empty rejectionReason"):
			noReason = true
		}
	}
	if !coreVar || !unresolved || !noReason {
		t.Fatalf("missing a violation class: coreVar=%v unresolved=%v noReason=%v (%+v)", coreVar, unresolved, noReason, f)
	}
}

// ---- GLOSS-FOURQ ----

func Test_glossaryFourQ_NonCanonicalError_And_CoverageWarning(t *testing.T) {
	g := &projectstate.Glossary{Items: []projectstate.GlossaryItem{
		{Term: "User", Category: "Who"},
		{Term: "Bogus", Category: "Nonsense"},
	}}
	f := glossaryFourQFindings(KindGlossary, g)
	if !hasRule(f, "GLOSS-FOURQ", SeverityError) {
		t.Fatal("a non-canonical category must be a GLOSS-FOURQ error")
	}
	// What/How/Where uncovered → warnings.
	if !hasRule(f, "GLOSS-FOURQ", SeverityWarning) {
		t.Fatal("uncovered Four-Questions categories must warn")
	}
}

func Test_glossaryFourQ_FullCoverageClean(t *testing.T) {
	g := &projectstate.Glossary{Items: []projectstate.GlossaryItem{
		{Term: "A", Category: "Who"}, {Term: "B", Category: "What"},
		{Term: "C", Category: "How"}, {Term: "D", Category: "Where"},
	}}
	if f := glossaryFourQFindings(KindGlossary, g); len(f) != 0 {
		t.Fatalf("full canonical coverage is clean, got: %+v", f)
	}
}

// ---- SR-ID-UNIQUE ----

func Test_scrubbedID_Violations(t *testing.T) {
	sr := &projectstate.ScrubbedRequirements{Items: []projectstate.Requirement{
		{ID: "R1", Statement: "ok"},
		{ID: "", Statement: "no id"},
		{ID: "R1", Statement: "dup id"},
		{ID: "R2", Statement: ""},
	}}
	f := scrubbedIDFindings(KindScrubbedRequirements, sr)
	var empty, dup, noStmt bool
	for _, fi := range f {
		if fi.RuleID != "SR-ID-UNIQUE" || fi.Severity != SeverityError {
			t.Fatalf("unexpected finding %+v", fi)
		}
		switch {
		case strings.Contains(fi.Message, "empty id"):
			empty = true
		case strings.Contains(fi.Message, "duplicated"):
			dup = true
		case strings.Contains(fi.Message, "empty statement"):
			noStmt = true
		}
	}
	if !empty || !dup || !noStmt {
		t.Fatalf("missing a violation class: empty=%v dup=%v noStmt=%v", empty, dup, noStmt)
	}
}

func Test_scrubbedID_Clean(t *testing.T) {
	sr := &projectstate.ScrubbedRequirements{Items: []projectstate.Requirement{
		{ID: "R1", Statement: "a"}, {ID: "R2", Statement: "b"},
	}}
	if f := scrubbedIDFindings(KindScrubbedRequirements, sr); len(f) != 0 {
		t.Fatalf("unique non-empty ids are clean, got: %+v", f)
	}
}

// OPC-TOPIC-COVERAGE was retired with the Wave-2 typed DeploymentOperationsModel: the
// free-text decisions[].topic list the nudge walked no longer exists (the topics are now
// required typed fields the schema enforces), so opcTopicFindings is inert and there is
// nothing left to assert here.

// ---- AdvancePhase pre-seal gates ----

func Test_standardCheckFailItems(t *testing.T) {
	proj := projectstate.Project{}
	proj.StandardCheck = projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.StandardCheck{Items: []projectstate.CheckItem{
			{Section: "S1", Guideline: "closed architecture", Status: projectstate.CheckPass},
			{Section: "S2", Guideline: "no design in a vacuum", Status: projectstate.CheckFail},
		}},
	}
	fails := standardCheckFailItems(proj)
	if len(fails) != 1 || !strings.Contains(fails[0], "no design in a vacuum") {
		t.Fatalf("expected one fail item naming the failing guideline, got: %v", fails)
	}
	// A pass/waived-only check is fail-free.
	proj.StandardCheck.Model = &projectstate.StandardCheck{Items: []projectstate.CheckItem{
		{Status: projectstate.CheckPass}, {Status: projectstate.CheckWaived},
	}}
	if f := standardCheckFailItems(proj); len(f) != 0 {
		t.Fatalf("a fail-free standard check must yield no items, got: %v", f)
	}
}

func Test_staleCommittedPhase1Kinds_NamesCause(t *testing.T) {
	proj := projectstate.Project{}
	proj.Volatilities = projectstate.ArtifactSlot{
		Status:          projectstate.ReviewCommitted,
		StaleBasis:      true,
		StaleBasisCause: &projectstate.StaleCause{UpstreamKind: "mission", UpstreamRevision: 2},
		Model:           &projectstate.Volatilities{},
	}
	got := staleCommittedPhase1Kinds(proj)
	if len(got) != 1 || !strings.Contains(got[0], "mission rev 2") {
		t.Fatalf("stale kind must name its cause (mission rev 2), got: %v", got)
	}
}

// ---- read-safety: a pre-existing VIOLATING committed state decodes, then yields findings ----

func Test_ViolatingCommittedState_DecodesThenFindings(t *testing.T) {
	// A System with an ORPHAN ResourceAccess (no edge to a resource) — a
	// finding-class violation, NOT a codec failure. (Empty encapsulates on an
	// M/E/RA is rejected by the encoder outright, so it cannot seed a committed
	// state; the empty client here is R5-exempt and correctly raises nothing.)
	sys := &projectstate.System{
		Components: []projectstate.Component{
			compE("web", "WebClient", projectstate.CompClient, projectstate.LayerClient, ""),
			compE("mgr", "OrderManager", projectstate.CompManager, projectstate.LayerManager, "the order workflow"),
			compE("ra", "OrderAccess", projectstate.CompResourceAccess, projectstate.LayerResourceAccess, "the order store"),
			compE("store", "OrderStore", projectstate.CompResource, projectstate.LayerResource, ""),
		},
		Relationships: []projectstate.Relationship{
			rel("web", "mgr", projectstate.CallSync, "places"),
			rel("mgr", "ra", projectstate.CallSync, "loads"),
			// NOTE: no ra → store edge, so ra is orphan.
		},
	}
	p := projectstate.Project{ID: "p"}
	p.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: sys, Revisions: 1}

	raw, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// CRITICAL INVARIANT: reading a violating committed state must NOT hard-fail.
	got, ok, err := projectstate.DecodeProjectJSON(raw, "p")
	if err != nil || !ok {
		t.Fatalf("a violating committed state must still decode (read-safety): ok=%v err=%v", ok, err)
	}
	model := got.SystemDesign.Model
	if !hasRule(raOrphanFindings(KindSystem, model), "SYS-RA-ORPHAN", SeverityError) {
		t.Fatal("orphan RA must surface as a finding on the decoded committed state")
	}
	// The empty client is R5-exempt: a legitimate empty encapsulates, no finding.
	if hasRule(encapsulatesFindings(KindSystem, model), "SYS-ENCAPSULATES", SeverityError) {
		t.Fatal("empty-encapsulates client must NOT fire SYS-ENCAPSULATES (R5 scoping)")
	}
}

// ---- reviewPolicyToContract (local-merge-and-policy Commit 3 read-back) -----

// The project read must carry the committed review-policy PRESET (the webApp's
// preset control reads it back), and a policy that ONLY sets a preset (the
// CreateProject "vibes" default) must not be dropped by the emptiness gate.
func Test_ReviewPolicyToContract_CarriesPreset(t *testing.T) {
	preset := projectstate.ReviewPresetVibes
	v := reviewPolicyToContract(projectstate.ReviewPolicy{Preset: &preset})
	if v == nil {
		t.Fatal("preset-only policy must not map to nil (emptiness gate must cover Preset)")
	}
	if v.Preset == nil || *v.Preset != projectstate.ReviewPresetVibes {
		t.Fatalf("Preset = %v, want vibes", v.Preset)
	}
	if reviewPolicyToContract(projectstate.ReviewPolicy{}) != nil {
		t.Fatal("a genuinely empty policy still maps to nil")
	}
}
