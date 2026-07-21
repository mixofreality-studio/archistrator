package projectdesign

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
	billing "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/stretchr/testify/mock"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
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
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, ProjectID(""), KindPlanningAssumptions, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_RequestArtifactDraft_Phase1Kind_FailedPrecondition(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	// A Phase-1 kind is a Client bug for the Phase-2 Manager.
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindMission, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for a Phase-1 kind, got %d", got)
	}
}

func Test_RequestArtifactDraft_SdpReviewKind_FailedPrecondition(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
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

	// F-R2 durable-slot-first: the abnormal-closed arm now consults main's slot before falling
	// back to the failed card. An UNCOMMITTED slot (this project has none) still renders the
	// honest StageDraftFailed — the case under test (a first-draft death).
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(id)}}
	m := &projectDesignManager{client: mc, projectState: ps}
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

// Plan-3 C2 (regression pin). isAbnormalClosedStatus → failedSessionView exists precisely so
// a session that dies mid-dispatch — e.g. the TRANSIENT dispatch/observe fault that exhausts
// its retry budget at coauthorphase2artifact.go:507-510 (the derr branch, closing the
// workflow while state.markActive had JUST stamped ActiveRoleArchitect/ActiveStepDrafting or
// Revising ahead of the dispatch) — can never leak that in-flight sub-step stamp through
// GetSessionState. failedSessionView is a PURE synthesis (it builds a fresh SessionStateView
// literal and never touches the replayed Query), so the guard holds for every abnormal
// terminal status Temporal can report for a dead workflow, not only FAILED.
func Test_GetSessionState_AbnormalClose_SubStepNeverLeaks(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

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

			m := &projectDesignManager{client: mc}
			view, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions)
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

// A Phase-2 draft whose immediate predecessor slot is uncommitted is refused with
// FailedPrecondition naming that predecessor — the wire enforces the Method's ordered
// Phase-2 spine, not only the SPA. Short-circuits before any Temporal client call.
func Test_RequestArtifactDraft_Phase2PredecessorUncommitted_FailedPrecondition(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	// activityList requested while its predecessor planningAssumptions is uncommitted.
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid)}}
	m := NewProjectDesignManager(nil, ps, nil, nil, nil, nil, nil, nil, nil)
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
	m := NewProjectDesignManager(nil, ps, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.RequestArtifactDraft(fwmanager.Context{Context: context.Background()}, pid, KindActivityList, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for missing project row, got %d", got)
	}
}

// The first Phase-2 kind (planningAssumptions) has NO predecessor — the gate passes
// without any head-state read (mirrors the SPA unlocking it without a sealed Phase 1),
// so a nil projectState is safe.
func Test_CheckPhase2Predecessor_FirstKind_NoRead(t *testing.T) {
	m := newProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err := m.checkPhase2Predecessor(context.Background(), ProjectID(uuid.NewString()), KindPlanningAssumptions); err != nil {
		t.Fatalf("planningAssumptions has no predecessor; gate must pass, got %v", err)
	}
}

// With the immediate predecessor Committed the gate passes (proceeds to dispatch).
func Test_CheckPhase2Predecessor_Committed_Proceeds(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: committedPhase2Project(pid, KindPlanningAssumptions)}
	m := newProjectDesignManager(nil, ps, nil, nil, nil, nil, nil, nil, nil)
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
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.RequestSDPCommit(fwmanager.Context{Context: context.Background()}, ProjectID(""))
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

// ---- SubmitSDPDecision ------------------------------------------------------

func Test_SubmitSDPDecision_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, ProjectID(""), SDPCommit, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_SubmitSDPDecision_CommitRequiresOptionID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitSDPDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), SDPDecisionUnknown, nil, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

// ---- SubmitReviewDecision (per-artifact OQ-3 gate) --------------------------

func Test_SubmitReviewDecision_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(""), KindPlanningAssumptions, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
	}
}

func Test_SubmitReviewDecision_RejectRequiresFeedback(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	// A Phase-1 kind is a Client bug for the Phase-2 Manager.
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindMission, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for a Phase-1 kind, got %d", got)
	}
}

func Test_SubmitReviewDecision_SdpReviewKind_FailedPrecondition(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	// The SDP review is not gated via the per-artifact reviewDecision signal.
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindSdpReview, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for KindSdpReview, got %d", got)
	}
}

func Test_SubmitReviewDecision_UnknownDecision(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindNetwork, ReviewDecisionUnknown, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for unknown decision, got %d", got)
	}
}

// ---- AdvanceToConstruction --------------------------------------------------

func Test_AdvanceToConstruction_EmptyProjectID(t *testing.T) {
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
	m := NewProjectDesignManager(nil, ps, nil, nil, nil, nil, nil, nil, nil)

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
	m := NewProjectDesignManager(nil, nil, nil, nil, nil, nil, nil, nil, nil)
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
	signalArg  any
	execCalled bool
}

type fakeWorkflowRun struct {
	client.WorkflowRun
	id string
}

func (r fakeWorkflowRun) GetID() string { return r.id }

func (c *recordingStartClient) SignalWithStartWorkflow(_ context.Context, workflowID, signalName string, signalArg any, _ client.StartWorkflowOptions, _ any, _ ...any) (client.WorkflowRun, error) {
	c.signalName = signalName
	c.signalArg = signalArg
	return fakeWorkflowRun{id: workflowID}, nil
}

func (c *recordingStartClient) ExecuteWorkflow(_ context.Context, _ client.StartWorkflowOptions, _ any, _ ...any) (client.WorkflowRun, error) {
	// The F47 regression: a bare ExecuteWorkflow against a RUNNING session (USE_EXISTING)
	// returns the existing handle WITHOUT delivering the new feedback. RequestArtifactDraft
	// must NOT use this path — record it so the test fails loudly if it regresses.
	c.execCalled = true
	return fakeWorkflowRun{id: "exec"}, nil
}

// DescribeWorkflowExecution answers the F-R2 receptive/revival probe RequestArtifactDraft now
// runs before + after the SignalWithStart. This delivery test has no prior run, so report the
// execution missing: prepareForDraftRequest treats NotFound as receptive (no query) and
// verifySessionRevived ignores a Describe error — leaving the signal-delivery assertion intact.
func (c *recordingStartClient) DescribeWorkflowExecution(_ context.Context, _, _ string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return nil, serviceerror.NewNotFound("no session")
}

func Test_RequestArtifactDraft_DeliversFeedbackViaRedraftSignal(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	// planningAssumptions is the first Phase-2 kind (no predecessor gate); notFound ⇒ amendment
	// index 0 (a normal/retry draft, not an amendment). The point under test is DELIVERY.
	ps := &fakeProjectState{notFound: true}
	fc := &recordingStartClient{}
	m := newProjectDesignManager(fc, ps, nil, nil, nil, nil, nil, nil, nil)

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

// F73 (part 1, Phase-2 twin). AskQuestions on a COMMITTED artifact whose co-author session is
// CLOSED must seed on MAIN (""), not the dead session's leftover amendment branch.
// resolveQuestionBranch keys off the P0-2 Describe-first honest view (GetSessionState), not the
// bare sessionState Query, which REPLAYS a closed run's stale mid-flight LIVE stage.
func Test_ResolveQuestionBranch_ClosedWorkflowLeftoverBranch_SeedsOnMain(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	resp := &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		},
	}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(resp, nil)
	// NO QueryWorkflow expectation: trusting the replayed query for a closed run would panic.

	proj := committedPhase2Project(id, KindPlanningAssumptions) // committed ⇒ amendmentIndexFor >= 1
	ps := &fakeProjectState{project: proj}

	m := &projectDesignManager{client: mc, projectState: ps}
	if branch := m.resolveQuestionBranch(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions); branch != "" {
		t.Fatalf("questions on a committed artifact whose session is closed must seed on main (\"\"), got leftover branch %q", branch)
	}
	mc.AssertExpectations(t)
}

// F73 (part 2, Phase-2 twin). The committed view must carry the slot's durable reviewThread so
// questions seeded on a COMMITTED Phase-2 artifact render on it.
func Test_CommittedSessionView_CarriesReviewThread(t *testing.T) {
	id := ProjectID(uuid.NewString())
	slot := projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		ReviewThread: []projectstate.ReviewComment{{
			ID:         "r1c1",
			Text:       "which resources are assumed?",
			Type:       projectstate.ReviewCommentTypeQuestion,
			Addressee:  projectstate.ReviewAddresseeArchitect,
			Status:     projectstate.ReviewCommentOpen,
			AuthorRole: reviewAuthorRole,
		}},
	}
	view, err := committedSessionView(id, KindPlanningAssumptions, slot)
	if err != nil {
		t.Fatalf("committedSessionView on a committed slot must not error: %v", err)
	}
	if view.Stage != StageCommitted {
		t.Fatalf("committed slot must render StageCommitted, got %d", view.Stage)
	}
	if len(view.ReviewThread) != 1 || view.ReviewThread[0].Text != "which resources are assumed?" {
		t.Fatalf("committed view must carry the slot's reviewThread question, got %+v", view.ReviewThread)
	}
}

// =============================================================================
// C-MPD-Δ regression spine — the Phase-2 AGENTIC-PIVOT dispatch → observe →
// read-back co-author gate (projectDesignManager.md §0.5), the TWIN of the C-MSD-Δ
// spine. Method product → NO BDD; regression-first, black-box at the WIRE SEAM. The
// LLM is stubbed at the EXTERNAL agentic-job boundary — a FAKE
// constructionPipelineAccess (submit/observe) + a FAKE projectStateAccess serving the
// read-back model the Action "committed". The Manager under test is NOT faked; the
// workflow drives the REAL dispatch → observe → read-back → human-gate sequence over
// the Temporal in-memory test environment (testsuite.WorkflowTestSuite — no Docker,
// no dev server, runs under -short).
//
// The SDP-review + Phase2-advance workflows use the REAL three estimate Engines
// (estimation.NewEstimationEngine() etc.) — they STAY server-side in-workflow (§0.5.5) and are NOT
// faked; the suite asserts the three-Engine join still runs in-process.
//
// Covers (the contract's required wire-level cases):
//   - happy plan-DRAFT round (dispatch → observe(Succeeded) → read-back → AwaitingReview)
//   - a REDRAFT gets a DISTINCT idempotency key (distinct ActivityID per dispatch)
//   - PhaseFailed → StageDraftFailed (NOT perpetual Drafting) → human gate (anti-wedge)
//   - the per-artifact reviewDecision suspend/resume gate unchanged (Reject loops / Withdraw)
//   - the SDP/human gate unchanged + the three estimation Engines still run in-process
// =============================================================================

// ---- Fake ProjectState ------------------------------------------------------

type fakeProjectState struct {
	mu sync.Mutex

	project  projectstate.Project
	notFound bool

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
	return f.bump(), nil
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
// branchAwareRejectFake, f29BranchFake, ledgerThreadFake) embed *fakeProjectState and
// override only the verbs they need branch/ledger-aware behavior for.

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

// ---- fakePipeline: the EXTERNAL agentic-job seam (constructionPipelineAccess) ---

// fakePipeline stands in for the claude-code-action DESIGN job at the WIRE seam. It
// records every submitted spec (so tests assert the ProjectID / artifact_kind / branch
// in DispatchInputs and the DISTINCT idempotency key per dispatch) and serves a
// scripted terminal phase per observe. By default a submitted job is observed
// pipelineSucceeded immediately (the job "ran, committed the JSON, CI went green").
type fakePipeline struct {
	mu sync.Mutex

	// phases is the scripted terminal phase per dispatch in order; once exhausted the
	// last entry repeats. Empty == always pipelineSucceeded.
	phases []pipelinePhase
	// diagnostic is attached to a failed/cancelled observation.
	diagnostic string

	submits []submitRecord
	// handlePhase tracks the phase to return for each issued handle.
	handlePhase map[string]pipelinePhase
	nextID      int
	// onObserve, when set, is invoked on each observe (used to snapshot/mutate state
	// mid-flight — the ONLY moment a dispatch is genuinely in flight; systemdesign twin).
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

// SubmitConstructionPipeline implements the GENERATED constructionpipeline contract seam
// (the submit invoker reaches it via the registered genActivities). The idempotency key is
// now stamped INSIDE the generated activity (genActivityIdempotencyKey) and arrives on the
// fwra call Context; the RepoRef→RepoTarget decode happens workflow-side (dispatchDesignJob),
// so spec.TargetRepo is the DECODED {Owner,Name} — recorded here as "owner/name".
func (p *fakePipeline) SubmitConstructionPipeline(rc fwra.Context, spec constructionpipeline.PipelineSpec) (constructionpipeline.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := len(p.submits)
	targetRepo := ""
	if !constructionpipeline.RepoTargetIsZero(spec.TargetRepo) {
		targetRepo = spec.TargetRepo.Owner + "/" + spec.TargetRepo.Name
	}
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
	return constructionpipeline.PipelineHandle(name), nil
}

// submitCount returns how many dispatches have been submitted so far (thread-safe), so a
// ledger fake can record the dispatch count at seed time and a test can assert ordering.
func (p *fakePipeline) submitCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.submits)
}

func (p *fakePipeline) ObserveConstructionPipeline(_ fwra.Context, handle constructionpipeline.PipelineHandle) (constructionpipeline.PipelineObservation, error) {
	p.mu.Lock()
	phase := p.handlePhase[constructionpipeline.PipelineHandleString(handle)]
	diag := p.diagnostic
	hook := p.onObserve
	p.mu.Unlock()
	if hook != nil {
		hook()
	}
	obs := constructionpipeline.PipelineObservation{Phase: neutralToRAPhase(phase)}
	if phase == pipelineFailed || phase == pipelineCancelled {
		obs.Diagnostic = diag
	}
	return obs, nil
}

// CancelConstructionPipeline satisfies the contract; the Phase-2 draft path never cancels.
func (p *fakePipeline) CancelConstructionPipeline(_ fwra.Context, _ constructionpipeline.PipelineHandle) error {
	return nil
}

// neutralToRAPhase maps this Manager's neutral scripted phase onto the RA phase the
// generated observe activity returns (the inverse of designPipelinePhase).
func neutralToRAPhase(p pipelinePhase) constructionpipeline.PipelinePhase {
	switch p {
	case pipelinePending:
		return constructionpipeline.PhasePending
	case pipelineRunning:
		return constructionpipeline.PhaseRunning
	case pipelineSucceeded:
		return constructionpipeline.PhaseSucceeded
	case pipelineFailed:
		return constructionpipeline.PhaseFailed
	case pipelineCancelled:
		return constructionpipeline.PhaseCancelled
	default:
		return constructionpipeline.PhasePending
	}
}

var _ constructionpipeline.ConstructionPipelineAccess = (*fakePipeline)(nil)

// ---- Test fixtures ----------------------------------------------------------

// committedSlot wraps a model as a committed slot.
func committedSlot(m projectstate.ArtifactModel) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: m}
}

// readBackSlot mirrors what the Action commits + how the Manager reads it back: the
// Kind slot carries the typed Model the design job committed (the read-back source).
func readBackSlot(m projectstate.ArtifactModel) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{Status: projectstate.ReviewAwaitingReview, Model: m}
}

func usd(minor int64) projectstate.Money {
	return projectstate.Money{MinorUnits: minor, Currency: "USD"}
}

// sdpReadyProject builds a head-state with committed PlanningAssumptions, ActivityList,
// Network, and the four Solution slots — enough to assemble the SDP review.
func sdpReadyProject(id projectstate.ProjectID) projectstate.Project {
	pa := &projectstate.PlanningAssumptions{
		Resources:           []string{"alice", "bob"},
		CalendarDaysPerWeek: 5,
		InfrastructureKind:  projectstate.InfrastructureKindGoTemporalPostgres,
		DeclaredUsage: projectstate.UsageAssumption{
			ExpectedDailyActiveUsers: 1000,
			RequestsPerMinute:        60,
			AvgPayloadBytes:          1024,
		},
		Terms: projectstate.SettlementTerms{
			RevenueShare:         projectstate.RevenueShareLaunchFlat10,
			RevenueSharePercent:  10,
			ComputeCost:          projectstate.ComputeCostFlatMarkup,
			ComputeMarkupPercent: 15,
			Schedule:             projectstate.ScheduleMonthly,
		},
	}
	al := &projectstate.ActivityList{
		Activities: []projectstate.ActivityItem{
			{Name: "design-core", EffortDays: 5, WorkerClass: "architect", Coding: false, RiskBucket: 3},
			{Name: "build-core", EffortDays: 10, WorkerClass: "senior", Coding: true, RiskBucket: 5},
			{Name: "build-ui", EffortDays: 5, WorkerClass: "junior", Coding: true, RiskBucket: 2},
		},
	}
	nw := &projectstate.Network{
		Dependencies: []projectstate.NetworkDependency{
			{Activity: "build-core", DependsOn: []string{"design-core"}},
			{Activity: "build-ui", DependsOn: []string{"build-core"}},
		},
		CriticalPath: []string{"design-core", "build-core"},
	}
	rates := map[string]projectstate.Money{
		"architect": usd(1000),
		"senior":    usd(800),
		"junior":    usd(500),
	}
	mkSol := func(kind projectstate.ArtifactKind, staffingCap int) *projectstate.Solution {
		return &projectstate.Solution{SlotKind: kind, StaffingCap: staffingCap, CalendarDaysPerWeek: 5, ClassRates: rates}
	}

	p := projectstate.Project{ID: id, Phase: projectstate.PhaseProjectDesign}
	p.PlanningAssumptions = committedSlot(pa)
	p.ActivityList = committedSlot(al)
	p.Network = committedSlot(nw)
	p.NormalSolution = committedSlot(mkSol(projectstate.KindNormalSolution, 2))
	p.DecompressedSolution = committedSlot(mkSol(projectstate.KindDecompressedSolution, 2))
	p.SubcriticalSolution = committedSlot(mkSol(projectstate.KindSubcriticalSolution, 1))
	p.CompressedSolution = committedSlot(mkSol(projectstate.KindCompressedSolution, 3))
	return p
}

// planningAssumptionsReadBack builds a project whose PlanningAssumptions slot carries
// a committed-by-Action PlanningAssumptions model (the read-back source for a
// PlanningAssumptions draft).
func planningAssumptionsReadBack(id projectstate.ProjectID) projectstate.Project {
	pa := &projectstate.PlanningAssumptions{
		Resources:           []string{"alice"},
		CalendarDaysPerWeek: 5,
		InfrastructureKind:  projectstate.InfrastructureKindGoTemporalPostgres,
	}
	p := projectstate.Project{ID: id, Phase: projectstate.PhaseProjectDesign, Version: 2}
	p.PlanningAssumptions = readBackSlot(pa)
	return p
}

// newWorkflows builds the workflows receiver under test. It carries NO RA dep (B9
// follow-up — every Activity is generated): the fake substrate / pipeline / rail are
// threaded to registerGenActivities / registerCoAuthor instead, exactly as production
// threads them into genActivities.
func newWorkflows() *workflows {
	return &workflows{
		Estimation:   estimation.NewEstimationEngine(),
		OperationEst: operationestimation.NewOperationEstimationEngine(),
		Settlement:   billing.NewBillingEngine(),
		// The generated invoker surface consults the manager's real preset hook; the
		// generated RA activities are registered on the env via registerGenActivities.
		Acts: genInvokers{Opts: activityOptions()},
	}
}

// registerGenActivities registers the GENERATED RA activities (projectState read-version /
// advance-phase, pipeline submit/observe/cancel, the seven rail verbs, and — since B9 + its
// follow-up — the seven designSessionAccess verbs the migrated call sites now reach,
// including the envelope-parameter Stage op) under their contract names — mirrors what
// RegisterWorker threads via genActivities (worker.gen.go).
// Pipeline / rail may be nil for tests that never dispatch; the registered method values are
// only invoked when the workflow reaches them.
//
// The designSessionAccess ops are backed by projectstate.NewDesignSessionAccess(ps) — the
// REAL production wrapper (projectstate/designsession.go), not a hand-rolled test double —
// so every workflow test exercises the actual forward-to-base + provenance capability
// check the RA runs, not a re-implementation of it. ps must be non-nil (every caller
// passes a concrete fake); the generated ProjectStateAccess contract requires every
// branch/ledger/reconcile verb unconditionally (C2 fold, code-health-phase-a), so a
// test's fake (e.g. gitrail_test.go's seqProjectState / ledgerThreadFake) simply
// OVERRIDES the verbs it wants branch/ledger-aware behavior for — no separate
// registration or capability opt-in needed.
func registerGenActivities(env *testsuite.TestWorkflowEnvironment, ps projectstate.ProjectStateAccess, pipe *fakePipeline, rail sourcecontrol.SourceControlAccess) {
	var pipeAcc constructionpipeline.ConstructionPipelineAccess
	if pipe != nil {
		pipeAcc = pipe
	}
	acts := &genActivities{ProjectState: ps, Pipeline: pipeAcc, Rail: rail, DesignSession: projectstate.NewDesignSessionAccess(ps)}
	env.RegisterActivityWithOptions(acts.ProjectStateReadProjectVersion, activity.RegisterOptions{Name: "projectStateAccess.readProjectVersion"})
	env.RegisterActivityWithOptions(acts.ProjectStateAdvancePhase, activity.RegisterOptions{Name: "projectStateAccess.advancePhase"})
	env.RegisterActivityWithOptions(acts.PipelineSubmitConstructionPipeline, activity.RegisterOptions{Name: "constructionPipelineAccess.submitConstructionPipeline"})
	env.RegisterActivityWithOptions(acts.PipelineObserveConstructionPipeline, activity.RegisterOptions{Name: "constructionPipelineAccess.observeConstructionPipeline"})
	env.RegisterActivityWithOptions(acts.PipelineCancelConstructionPipeline, activity.RegisterOptions{Name: "constructionPipelineAccess.cancelConstructionPipeline"})
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
	env.RegisterActivityWithOptions(acts.DesignSessionSetReviewCommentStatusOnBranch, activity.RegisterOptions{Name: "designSessionAccess.setReviewCommentStatusOnBranch"})
	env.RegisterActivityWithOptions(acts.DesignSessionSeedReviewCommentsOnBranch, activity.RegisterOptions{Name: "designSessionAccess.seedReviewCommentsOnBranch"})
}

// registerName builds the workflow.RegisterOptions naming a registered workflow.
func registerName(name string) workflow.RegisterOptions {
	return workflow.RegisterOptions{Name: name}
}

// registerCoAuthor registers the per-artifact gate workflow + its activities on the
// test env, exactly as RegisterWorker does in production (same stable names). Every
// Activity is generated (registerGenActivities); ps is the fake substrate the generated
// projectState/designSession activities are backed by (threaded explicitly — the
// workflows struct no longer carries an RA dep).
func registerCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps projectstate.ProjectStateAccess, pipe *fakePipeline) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorPhase2ArtifactWorkflow, registerName(executionKindCoAuthor))
	registerGenActivities(env, ps, pipe, nil)
}

// ---- Pure unit test of the deterministic assembly + engine-join helper ------

func Test_assembleSdpReview_FourRows_Deterministic(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wf := newWorkflows()
	proj := sdpReadyProject(projectstate.ProjectID(id))

	review, err := wf.assembleSdpReview(proj, "")
	if err != nil {
		t.Fatalf("assembleSdpReview: %v", err)
	}
	if len(review.Options) != 4 {
		t.Fatalf("want 4 option rows, got %d", len(review.Options))
	}
	if review.Recommendation == "" {
		t.Fatalf("want a non-empty recommendation")
	}
	if !optionInReview(review, review.Recommendation) {
		t.Fatalf("recommendation %s is not one of the assembled options", review.Recommendation)
	}
	for _, r := range review.Options {
		if r.BuildCost.Currency != "USD" {
			t.Fatalf("row %s: want USD build cost, got %q", r.OptionID, r.BuildCost.Currency)
		}
		if r.RevenueSharePercent != 10 {
			t.Fatalf("row %s: want 10%% revenue share, got %v", r.OptionID, r.RevenueSharePercent)
		}
	}

	again, err := wf.assembleSdpReview(proj, "")
	if err != nil {
		t.Fatalf("assembleSdpReview (2nd): %v", err)
	}
	if again.Recommendation != review.Recommendation {
		t.Fatalf("non-deterministic recommendation: %s vs %s", again.Recommendation, review.Recommendation)
	}
	for i := range review.Options {
		if again.Options[i] != review.Options[i] {
			t.Fatalf("non-deterministic row %d: %+v vs %+v", i, again.Options[i], review.Options[i])
		}
	}
}

func Test_assembleSdpReview_MissingPrerequisite_Errors(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wf := newWorkflows()
	proj := sdpReadyProject(projectstate.ProjectID(id))
	proj.Network = projectstate.ArtifactSlot{} // drop a prerequisite

	if _, err := wf.assembleSdpReview(proj, ""); err == nil {
		t.Fatalf("want an error for a missing Network prerequisite, got nil")
	}
}

// ---- CoAuthorPhase2ArtifactWorkflow: dispatch → observe → read-back ----------

// pdSessionView queries the CoAuthor session state and decodes the SessionStateView,
// fataling exactly as the inlined query/decode pattern it replaces.
func pdSessionView(t *testing.T, env *testsuite.TestWorkflowEnvironment) SessionStateView {
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

// pdAssertPlanDraftDispatchShape asserts the single Phase-2 draft dispatch's shape:
// ProjectID, artifact_kind, target_branch, thin command + job_mode in DispatchInputs,
// and the RA-controlled idempotency discipline.
func pdAssertPlanDraftDispatchShape(t *testing.T, sub submitRecord, id ProjectID) {
	t.Helper()
	if sub.projectID != id {
		t.Fatalf("dispatch carried wrong ProjectID: %q", sub.projectID)
	}
	if sub.dispatchInputs[dispatchInputArtifactKind] != projectstate.KindPlanningAssumptions.String() {
		t.Fatalf("dispatch artifact_kind = %q, want %s", sub.dispatchInputs[dispatchInputArtifactKind], projectstate.KindPlanningAssumptions)
	}
	if sub.dispatchInputs[dispatchInputTargetBranch] == "" {
		t.Fatal("dispatch must carry a non-empty target_branch")
	}
	// Thin dispatch: the Manager ships the .claude command NAME, not a composed prompt.
	if got := sub.dispatchInputs[dispatchInputCommand]; got != "planning-assumptions-draft" {
		t.Fatalf("dispatch command = %q, want planning-assumptions-draft", got)
	}
	if sub.dispatchInputs[dispatchInputJobMode] != jobModeDraft {
		t.Fatalf("draft dispatch job_mode = %q, want draft", sub.dispatchInputs[dispatchInputJobMode])
	}
	// The Manager MUST NOT set idempotency_token in DispatchInputs (RA-controlled).
	if _, present := sub.dispatchInputs["idempotency_token"]; present {
		t.Fatal("the Manager must NOT set idempotency_token in DispatchInputs (RA-controlled)")
	}
	if sub.idempotencyKey.IsZero() {
		t.Fatal("the dispatch Activity must supply a non-empty idempotency key")
	}
}

// Happy plan-DRAFT round: the gate DISPATCHES (with the right ProjectID +
// artifact_kind + branch in DispatchInputs and a Manager-supplied idempotency key),
// OBSERVES to pipelineSucceeded, READS BACK the committed typed Phase-2 model, and
// suspends at AwaitingReview surfacing the typed Draft. Phase 2 has NO PM critique →
// a SINGLE dispatch.
func Test_CoAuthor_PlanDraftRoundTrip_DispatchObserveReadBack_AwaitsReview(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	pipe := newFakePipeline() // default: dispatch observed Succeeded
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		view := pdSessionView(t, env)
		if view.Stage != StageAwaitingReview {
			t.Fatalf("want StageAwaitingReview, got %d", view.Stage)
		}
		if view.Draft.Kind != "planningAssumptions" || view.Draft.Model == nil {
			t.Fatalf("expected a staged planningAssumptions read-back draft envelope, got %+v", view.Draft)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.staged) != 1 {
		t.Fatalf("want 1 staged read-back model, got %d", len(ps.staged))
	}
	if _, ok := ps.staged[0].(*projectstate.PlanningAssumptions); !ok {
		t.Fatalf("staged model is not *projectstate.PlanningAssumptions: %T", ps.staged[0])
	}
	// Phase 2 has no PM critique: exactly ONE dispatch.
	if len(pipe.submits) != 1 {
		t.Fatalf("Phase-2 draft must be a single dispatch, got %d submits", len(pipe.submits))
	}
	pdAssertPlanDraftDispatchShape(t, pipe.submits[0], id)
}

// THE ANTI-WEDGE TEST. A dispatched Phase-2 job that reaches a TERMINAL FAILURE phase
// (pipelineFailed — drafting failed or the required CI validation check went red) must
// NOT crash the workflow and must NOT leave a perpetual StageDrafting. The session
// lands in the human-visible StageDraftFailed carrying the neutral Diagnostic,
// surfaced by getSessionState, and suspends on the SAME reviewDecision gate awaiting a
// human Retry/Withdraw. Withdraw ends gracefully.
func Test_CoAuthor_PhaseFailed_LandsInStageDraftFailed_NotPerpetualDrafting(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
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
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the draft-failed gate")
	}
	// A ran-but-failed job is terminal-at-the-Manager — escalated to the human gate, NOT
	// a workflow crash.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT fail the workflow, got: %v", err)
	}
	if len(ps.staged) != 0 {
		t.Fatalf("a failed draft must stage nothing, got %d", len(ps.staged))
	}
	if len(ps.withdrawn) != 1 {
		t.Fatalf("withdraw from the draft-failed gate must call WithdrawArtifact once, got %d", len(ps.withdrawn))
	}
}

// PhaseCancelled is likewise a terminal failure that lands in StageDraftFailed (never
// a perpetual Drafting).
func Test_CoAuthor_PhaseCancelled_LandsInStageDraftFailed(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
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

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("PhaseCancelled must not crash the workflow: %v", err)
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
	ps := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
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

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if len(ps.staged) != 1 {
		t.Fatalf("the recovered redraft must stage exactly once, got %d", len(ps.staged))
	}
}

// A Reject at the AwaitingReview gate calls RejectArtifact and loops back to a fresh
// DRAFT dispatch (the per-artifact human-gate suspend/resume is unchanged). Approve
// after the redraft commits.
func Test_CoAuthor_Reject_LoopsToFreshDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	pipe := newFakePipeline() // every dispatch Succeeds
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	const rejectNotes = "rework the staffing assumptions"
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.rejected) != 1 || ps.rejected[0].kind != projectstate.KindPlanningAssumptions || ps.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact(KindPlanningAssumptions, %q), got %v", rejectNotes, ps.rejected)
	}
	if len(ps.committed) != 1 {
		t.Fatalf("want one commit after redraft->approve, got %v", ps.committed)
	}
	// Reject loops to a FRESH dispatch: at least 2 draft dispatches.
	if len(pipe.submits) < 2 {
		t.Fatalf("a reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
}

// VIBES AUTOGATE (F-R3 vibes-everywhere, Phase-2 twin): under a vibes ReviewPolicy a CLEAN draft
// is AUTO-approved at the review gate WITHOUT a human signal — the design gate honors ReviewPolicy
// exactly like construction. NO delayed signal is registered: the autogate must commit on its own.
func Test_CoAuthor_VibesPolicy_AutoApproves_NoHumanSignal(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	proj := planningAssumptionsReadBack(projectstate.ProjectID(id))
	vibes := projectstate.ReviewPresetVibes
	proj.ReviewPolicy.Preset = &vibes
	ps := &fakeProjectState{project: proj}
	pipe := newFakePipeline() // every dispatch Succeeds
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if len(ps.committed) != 1 || ps.committed[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("the auto-approve must commit the artifact, got committed=%v", ps.committed)
	}
}

// VIBES AUTOGATE replay safety (Phase-2 twin): a pre-feature in-flight session (the
// "design-vibes-autogate" GetVersion resolves DefaultVersion) stays on the HUMAN gate even under a
// vibes policy. A human WITHDRAW decides it; were the autogate (wrongly) ON, it would auto-APPROVE
// first, so coAuthorWithdrawn proves the pin.
func Test_CoAuthor_VibesAutogate_VersionGate_PreFeatureStaysHumanGated(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	proj := planningAssumptionsReadBack(projectstate.ProjectID(id))
	vibes := projectstate.ReviewPresetVibes
	proj.ReviewPolicy.Preset = &vibes
	ps := &fakeProjectState{project: proj}
	pipe := newFakePipeline()
	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// Pin the autogate OFF (a pre-feature in-flight execution replays DefaultVersion).
	env.OnGetVersion("design-vibes-autogate", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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

// pdAssertSubStepFailedGateClear queries the session at the StageDraftFailed gate and
// asserts the in-flight (ActiveRole, ActiveStep, Round) stamp reads none/none/0.
func pdAssertSubStepFailedGateClear(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()
	v := pdSessionView(t, env)
	if v.Stage != StageDraftFailed {
		t.Fatalf("want StageDraftFailed after the terminal job failure, got %d", v.Stage)
	}
	if v.ActiveRole != ActiveRoleNone || v.ActiveStep != ActiveStepNone || v.Round != 0 {
		t.Fatalf("StageDraftFailed must show no active role, got role=%d step=%d round=%d", v.ActiveRole, v.ActiveStep, v.Round)
	}
}

// pdAssertSubStepReviewGateClear queries the session at the human AwaitingReview gate
// and asserts the in-flight (ActiveRole, ActiveStep, Round) stamp reads none/none/0.
func pdAssertSubStepReviewGateClear(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()
	v := pdSessionView(t, env)
	if v.Stage != StageAwaitingReview {
		t.Fatalf("want StageAwaitingReview after the retry, got %d", v.Stage)
	}
	if v.ActiveRole != ActiveRoleNone || v.ActiveStep != ActiveStepNone || v.Round != 0 {
		t.Fatalf("the human gate must show no active role, got role=%d step=%d round=%d", v.ActiveRole, v.ActiveStep, v.Round)
	}
}

// Plan-3 C2: the honest role-driven sub-step indicator (projectdesign twin of the
// systemdesign C1 test). Phase 2 has NO PM critique — every kind is Architect-only
// (drafting/revising) — so a draft → retry-via-reject (after a terminal job failure) →
// approve sequence must surface the live (ActiveRole, ActiveStep, Round) at each dispatch
// boundary, and none/none/0 at the human gate. The redraft rides the StageDraftFailed
// Retry-via-Reject lever (NOT a plain AwaitingReview-gate reject) because redraftCount —
// the round the Manager's OWN stageForAttempt/markActive both key off — is bumped ONLY on
// a failed-gate retry in this workflow (F40: a plain AwaitingReview reject stays on the
// SAME persistent branch/PR and advances only the review-ledger round, not the attempt
// counter); this is the scenario that genuinely reaches ActiveStepRevising round 1. The
// in-flight snapshot is captured from the observe activity (the ONLY moment a dispatch is
// genuinely in flight — the fake's onObserve hook, which the workflow is blocked on); the
// gate snapshot from a delayed-callback query while the workflow is suspended.
func Test_CoAuthor_ActiveSubStep_SequenceThroughDraftReviseApprove(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	// First dispatch fails (terminal); the retry dispatch (2nd) succeeds → AwaitingReview.
	pipe := newFakePipeline(pipelineFailed, pipelineSucceeded)
	pipe.diagnostic = "aiarch-validate found 2 violations"

	type subStep struct {
		role  ActiveRole
		step  ActiveStep
		round int64
	}
	var mu sync.Mutex
	var seq []subStep
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
		mu.Unlock()
	}

	wf := newWorkflows()
	registerCoAuthor(env, wf, ps, pipe)

	// The first dispatch fails terminally, landing at StageDraftFailed. Retry-via-Reject
	// (with feedback) re-dispatches — this is the lever that bumps redraftCount to 1.
	env.RegisterDelayedCallback(func() {
		pdAssertSubStepFailedGateClear(t, env)
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "rework it"}})
	}, 30*time.Second)

	// After the successful retry reaches AwaitingReview, the sub-step must read
	// none/none/0; approve to end.
	env.RegisterDelayedCallback(func() {
		pdAssertSubStepReviewGateClear(t, env)
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.committed) != 1 {
		t.Fatalf("want one commit after retry->approve, got %v", ps.committed)
	}

	want := []subStep{
		{ActiveRoleArchitect, ActiveStepDrafting, 0}, // first draft in flight (round 0, will fail)
		{ActiveRoleArchitect, ActiveStepRevising, 1}, // retry-via-reject redraft in flight (round 1)
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

// Plan-3 C2: a design job that reaches a terminal FAILURE phase must clear the in-flight
// sub-step stamp — the belt-and-braces clear in awaitDraftFailedRecovery — so the
// StageDraftFailed gate never shows a stale "architect drafting" pill while the human
// decides Retry/Withdraw.
func Test_CoAuthor_ActiveSubStep_ClearsAtDraftFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
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

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT fail the workflow, got: %v", err)
	}
}

// ---- AssembleSDPReviewWorkflow: the SDP/human gate + in-workflow Engine join -

// The SDP-review workflow still ASSEMBLES the four options and runs the three
// estimation Engine joins IN-WORKFLOW (those Engines did NOT move to the Action), then
// stages the assembled SdpReview and suspends on the sdpDecision gate. Commit binds
// the chosen option. This proves the human gate is UNCHANGED and the estimation
// Engines still run in-process.
func Test_AssembleSDPReviewWorkflow_Commit_HappyPath_EnginesRunInProcess(t *testing.T) {
	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: sdpReadyProject(projectstate.ProjectID(id))}
	wf := newWorkflows() // REAL three estimate Engines; NO pipeline needed (no dispatch)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.AssembleSDPReviewWorkflow, registerName(executionKindSDPReview))
	registerGenActivities(env, ps, nil, nil)

	pre, err := wf.assembleSdpReview(ps.project, "")
	if err != nil {
		t.Fatalf("pre-assembly: %v", err)
	}
	chosen := OptionID(pre.Recommendation)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalSDPDecision, sdpDecisionSignal{Decision: SDPCommit, OptionID: &chosen})
	}, time.Second)

	env.ExecuteWorkflow(executionKindSDPReview, sdpReviewInput{ProjectID: id})

	if !env.IsWorkflowCompleted() {
		t.Fatalf("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	if len(ps.staged) == 0 {
		t.Fatalf("expected at least one staged SdpReview")
	}
	rev, ok := ps.staged[0].(*projectstate.SdpReview)
	if !ok {
		t.Fatalf("staged model is %T, want *SdpReview", ps.staged[0])
	}
	// The in-workflow three-Engine join produced four rows carrying the joined outputs.
	if len(rev.Options) != 4 {
		t.Fatalf("staged SdpReview has %d rows, want 4 (the in-workflow Engine join)", len(rev.Options))
	}
	for _, r := range rev.Options {
		if r.BuildCost.Currency == "" {
			t.Fatalf("row %s missing constructionEstimationEngine BuildCost (Engine join did not run)", r.OptionID)
		}
	}
	if len(ps.committed) != 1 || ps.committed[0] != projectstate.KindSdpReview {
		t.Fatalf("want exactly one KindSdpReview commit, got %v", ps.committed)
	}
	last := ps.staged[len(ps.staged)-1].(*projectstate.SdpReview)
	if last.Recommendation != projectstate.OptionID(chosen) {
		t.Fatalf("committed review recommendation = %s, want chosen %s", last.Recommendation, chosen)
	}
}

// Plan-3 C2: the SDP assembly is server-side (assembleSdpReview — a deterministic join,
// not an agentic dispatch), so its sub-step indicator must NEVER stamp a role — the query
// view must read none/none/0 at every observable point (the AwaitingReview human gate here;
// StageAssemblingSDP itself is never externally observable within one workflow task since
// no activity/timer separates it from the immediately-following stage, so the gate is the
// meaningful assertion point).
func Test_AssembleSDPReviewWorkflow_ActiveSubStep_AlwaysNone(t *testing.T) {
	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: sdpReadyProject(projectstate.ProjectID(id))}
	wf := newWorkflows()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.AssembleSDPReviewWorkflow, registerName(executionKindSDPReview))
	registerGenActivities(env, ps, nil, nil)

	pre, err := wf.assembleSdpReview(ps.project, "")
	if err != nil {
		t.Fatalf("pre-assembly: %v", err)
	}
	chosen := OptionID(pre.Recommendation)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var v SessionStateView
		if err := enc.Get(&v); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if v.Stage != StageAwaitingReview {
			t.Fatalf("want StageAwaitingReview at the human gate, got %d", v.Stage)
		}
		if v.ActiveRole != ActiveRoleNone || v.ActiveStep != ActiveStepNone || v.Round != 0 {
			t.Fatalf("SDP assembly must never stamp a role, got role=%d step=%d round=%d", v.ActiveRole, v.ActiveStep, v.Round)
		}
		env.SignalWorkflow(signalSDPDecision, sdpDecisionSignal{Decision: SDPCommit, OptionID: &chosen})
	}, time.Second)

	env.ExecuteWorkflow(executionKindSDPReview, sdpReviewInput{ProjectID: id})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
}

func Test_AssembleSDPReviewWorkflow_RejectAll_ReassemblesThenCommits(t *testing.T) {
	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: sdpReadyProject(projectstate.ProjectID(id))}
	wf := newWorkflows()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.AssembleSDPReviewWorkflow, registerName(executionKindSDPReview))
	registerGenActivities(env, ps, nil, nil)

	pre, _ := wf.assembleSdpReview(ps.project, "")
	chosen := OptionID(pre.Recommendation)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalSDPDecision, sdpDecisionSignal{Decision: SDPRejectAll, Feedback: &ReviewFeedback{Notes: "cut cost"}})
	}, time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalSDPDecision, sdpDecisionSignal{Decision: SDPCommit, OptionID: &chosen})
	}, 2*time.Second)

	env.ExecuteWorkflow(executionKindSDPReview, sdpReviewInput{ProjectID: id})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.rejected) != 1 || ps.rejected[0].kind != projectstate.KindSdpReview {
		t.Fatalf("want one KindSdpReview reject, got %v", ps.rejected)
	}
	if len(ps.committed) != 1 || ps.committed[0] != projectstate.KindSdpReview {
		t.Fatalf("want one KindSdpReview commit after re-assembly, got %v", ps.committed)
	}
}

// ---- Phase2AdvanceWorkflow --------------------------------------------------

func Test_Phase2AdvanceWorkflow_MissingArtifacts_NotAdvanced(t *testing.T) {
	id := ProjectID(uuid.NewString())
	proj := projectstate.Project{ID: projectstate.ProjectID(id), Phase: projectstate.PhaseProjectDesign}
	proj.PlanningAssumptions = committedSlot(&projectstate.PlanningAssumptions{CalendarDaysPerWeek: 5})
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.Phase2AdvanceWorkflow, registerName(executionKindPhaseAdvance))
	registerGenActivities(env, ps, nil, nil)

	env.ExecuteWorkflow(executionKindPhaseAdvance, phaseAdvanceInput{ProjectID: id})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res PhaseAdvanceResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("result: %v", err)
	}
	if res.Advanced {
		t.Fatalf("want Advanced=false with missing artifacts")
	}
	if len(res.MissingArtifacts) == 0 {
		t.Fatalf("want a non-empty MissingArtifacts set")
	}
	if ps.advanced != 0 {
		t.Fatalf("AdvancePhase must NOT be called when gating fails (called %d times)", ps.advanced)
	}
}

func Test_Phase2AdvanceWorkflow_AllCommittedWithOption_Advances(t *testing.T) {
	id := ProjectID(uuid.NewString())
	proj := sdpReadyProject(projectstate.ProjectID(id))
	proj.RiskModel = committedSlot(&projectstate.RiskModel{})
	proj.SdpReview = committedSlot(&projectstate.SdpReview{
		Options:        []projectstate.SdpOptionRow{{OptionID: "NormalSolution"}},
		Recommendation: "NormalSolution",
	})
	ps := &fakeProjectState{project: proj}
	wf := newWorkflows()

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.Phase2AdvanceWorkflow, registerName(executionKindPhaseAdvance))
	registerGenActivities(env, ps, nil, nil)

	env.ExecuteWorkflow(executionKindPhaseAdvance, phaseAdvanceInput{ProjectID: id})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res PhaseAdvanceResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("result: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("want Advanced=true, got missing=%v", res.MissingArtifacts)
	}
	if ps.advanced != 1 {
		t.Fatalf("want exactly one AdvancePhase seal, got %d", ps.advanced)
	}
}

// =============================================================================
// I-DESIGN-DISPATCH Part 3 — the projectdesign (Phase-2) TWIN of the wiring-level
// PROOF. Method product → NO BDD; regression-first, black-box at the WIRE seam. The
// rail is stubbed at the EXTERNAL sourceControlAccess boundary (a coherent SCRIPTED
// rail) + the read-back is served by a BRANCH-AWARE fake projectstate; the external
// agentic-job seam is the existing fakePipeline (workflow_test.go). The Manager under
// test is the REAL CoAuthorPhase2ArtifactWorkflow — no internal component is faked.
//
// Phase-2 has NO PM-critique round-trip (a single draft dispatch), and the SDP-assemble
// path keeps its three estimate Engines IN-WORKFLOW and gets NO rail — so this file
// drives ONLY the per-artifact draft path (the only path the rail rides). The
// AssembleSDPReviewWorkflow is deliberately untouched here (its in-process Engine join
// is proven in workflow_test.go).
//
// Proven here, mirroring the systemdesign twin:
//   1. happy round-trip + branch reconciliation (read-back branch == dispatch target_branch)
//   2. Approve: merge BEFORE commit-on-main + post-merge read on MAIN
//   3. Reject → redraft on a NEW session branch (attempt+1) + a NEW PR
//   4. PhaseFailed → StageDraftFailed with the rail wired (no approve-rail, no commit)
//   5. required-check RED → merge BLOCKED, no commit, recovers
// =============================================================================

// ---- seqLog: a shared ordered event log across the rail + projectstate fakes --

type seqLog struct {
	mu     sync.Mutex
	events []seqEvent
}

type seqEvent struct {
	op     string
	branch string
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

// ---- scriptedRail: the EXTERNAL PR-rail seam (per-attempt PR + ordered log) ----

type scriptedRail struct {
	mu  sync.Mutex
	log *seqLog

	checkGreen bool
	// statusAuthFailsRemaining, when >0, makes GetPullRequestStatus return an fwra.Auth
	// error (the platform's rate-limit-403-as-Auth) and decrement — exercises QA F35.
	statusAuthFailsRemaining int
	// syncErr, when non-nil, makes SyncManagedScaffold fail terminally — exercises the
	// managed-scaffold-sync containment (dispatch BLOCKED, session at the failed gate).
	syncErr error
	// F-QA2-44 token-lifetime modeling (systemdesign twin parity). Each mint issues a
	// DISTINCT token (tok-1, tok-2, …) and makes it the currently-valid one. When
	// enforceTokenValidity is armed, the merge-window verbs 403 (fwra.Auth — the platform's
	// non-retryable classification) on any credential that is not the currently-valid
	// token; expireCurrentToken() models the ~1h GitHub App installation-token expiry
	// between dispatch and a late human approve.
	enforceTokenValidity bool
	mintSeq              int
	validToken           string

	openedBranches []string
	openedPRHeads  []string
	mergedPRs      []string
	prByHead       map[string]string
	calls          map[string]int
	// creds records, per merge-window verb, the credential bytes each call presented —
	// lets a test assert WHICH minted token a call rode (F-QA2-44).
	creds map[string][]string
}

// expireCurrentToken invalidates the currently-valid token — the dispatch-time credential
// has aged past the ~1h installation-token lifetime (F-QA2-44). The next mint issues a
// fresh valid token.
func (r *scriptedRail) expireCurrentToken() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validToken = ""
}

// staleCred reports whether the presented credential must be rejected (validity
// enforcement armed AND the credential is not the currently-valid token). Records the
// presented credential under verb first.
func (r *scriptedRail) staleCred(verb string, cred sourcecontrol.RepoCredential) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.creds == nil {
		r.creds = map[string][]string{}
	}
	r.creds[verb] = append(r.creds[verb], string(cred.Bytes))
	return r.enforceTokenValidity && string(cred.Bytes) != r.validToken
}

func newScriptedRail(green bool, log *seqLog) *scriptedRail {
	return &scriptedRail{checkGreen: green, log: log, prByHead: map[string]string{}, calls: map[string]int{}}
}

func (r *scriptedRail) count(verb string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[verb]
}

// CommitManagedFiles backs the managed-scaffold sync: sourcecontrol.SyncManagedScaffold
// (the free-function composition helper this fake's own SyncManagedScaffold method below
// delegates to, mirroring the real production RA) reaches the rail through this verb for
// a non-managedFileSyncer fake. It records the sync call under the "SyncManagedScaffold"
// counter and honors the scripted syncErr.
func (r *scriptedRail) CommitManagedFiles(_ fwra.Context, _ sourcecontrol.RepoRef, _ []sourcecontrol.ManagedFile, _ sourcecontrol.RepoCredential) (sourcecontrol.CommitRef, error) {
	r.mu.Lock()
	r.calls["SyncManagedScaffold"]++
	err := r.syncErr
	r.mu.Unlock()
	if err != nil {
		return sourcecontrol.CommitRef(""), err
	}
	return sourcecontrol.CommitRef("scaffold-sync"), nil
}

func (r *scriptedRail) GetInstallationToken(_ fwra.Context, _ sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.mu.Lock()
	r.calls["GetInstallationToken"]++
	// Each mint issues a DISTINCT token and makes it the currently-valid one (F-QA2-44):
	// tok-1 for the dispatch-time mint, tok-2 for the gate-decision re-mint, …
	r.mintSeq++
	tok := fmt.Sprintf("tok-%d", r.mintSeq)
	r.validToken = tok
	r.mu.Unlock()
	return sourcecontrol.RepoCredential{Bytes: []byte(tok), ExpiresAt: time.Now().Add(time.Hour)}, nil
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

func (r *scriptedRail) GetPullRequestStatus(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.mu.Lock()
	r.calls["GetPullRequestStatus"]++
	r.mu.Unlock()
	if r.staleCred("GetPullRequestStatus", cred) {
		// The F-QA2-44 live fault: the presented installation token has EXPIRED; GitHub
		// 403s and the platform classifier reports a non-retryable Auth fault.
		return sourcecontrol.PullRequestStatus{}, fwra.New(fwra.Auth, "getPullRequest: github auth/permission denied (expired installation token)")
	}
	r.mu.Lock()
	green := r.checkGreen
	fail := r.statusAuthFailsRemaining > 0
	if fail {
		r.statusAuthFailsRemaining--
	}
	r.mu.Unlock()
	if fail {
		// The observed F35 fault: GitHub secondary rate-limit 403 the platform classifier
		// reports as Auth. railWithAuthRetry retries it within a bounded budget.
		return sourcecontrol.PullRequestStatus{}, fwra.New(fwra.Auth, "getPullRequest: github auth/permission denied")
	}
	rollup := sourcecontrol.CheckFailure
	if green {
		rollup = sourcecontrol.CheckSuccess
	}
	return sourcecontrol.PullRequestStatus{CheckRollup: rollup, Mergeable: green}, nil
}

func (r *scriptedRail) PostReview(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.ReviewSubmission, cred sourcecontrol.RepoCredential) error {
	r.mu.Lock()
	r.calls["PostReview"]++
	r.mu.Unlock()
	if r.staleCred("PostReview", cred) {
		return fwra.New(fwra.Auth, "postReview: github auth/permission denied (expired installation token)")
	}
	return nil
}

func (r *scriptedRail) MergePullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential) (sourcecontrol.MergeResult, error) {
	if r.staleCred("MergePullRequest", cred) {
		return sourcecontrol.MergeResult{}, fwra.New(fwra.Auth, "mergePullRequest: github auth/permission denied (expired installation token)")
	}
	r.mu.Lock()
	r.calls["MergePullRequest"]++
	r.mergedPRs = append(r.mergedPRs, sourcecontrol.PullRequestRefString(pr))
	r.mu.Unlock()
	if r.log != nil {
		r.log.add("merge", sourcecontrol.PullRequestRefString(pr))
	}
	return sourcecontrol.MergeResult{Merged: true, Commit: "merged-" + sourcecontrol.PullRequestRefString(pr)}, nil
}

// The remaining SourceControlAccess ops are outside the design PR-rail lifecycle; the stub
// satisfies the full contract with inert implementations so it can back the GENERATED rail
// Activities registered via genActivities.
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
// composition helper rather than stubbing directly. B9 rewires the generated
// wf.Acts.RailSyncManagedScaffold invoker straight onto this method (the custom
// SyncManagedScaffoldActivity that used to call the free function directly is gone), so
// this method is now the LOAD-BEARING path the syncErr/CommitManagedFiles-counter tests
// below exercise (previously the free function was called directly, bypassing this
// method entirely — a latent fake/production divergence this migration surfaced).
func (r *scriptedRail) SyncManagedScaffold(rc fwra.Context, repo sourcecontrol.RepoRef, cred sourcecontrol.RepoCredential) (bool, error) {
	return sourcecontrol.SyncManagedScaffold(rc.Context, r, repo, cred)
}

var _ sourcecontrol.SourceControlAccess = (*scriptedRail)(nil)

// ---- seqProjectState: branch-aware read-back + ordered commit/read events ------

type seqProjectState struct {
	*fakeProjectState
	log *seqLog

	mu            sync.Mutex
	readBranches  []string
	stageBranches []string
}

var _ projectstate.ProjectStateAccess = (*seqProjectState)(nil)

func (f *seqProjectState) ReadProject(rc fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
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
		// CONCRETE substrate's obligation, not a wrapper-level capability gate.
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

func newRailWorkflows(rail sourcecontrol.SourceControlAccess) *workflows {
	return &workflows{
		Estimation:   estimation.NewEstimationEngine(),
		OperationEst: operationestimation.NewOperationEstimationEngine(),
		Settlement:   billing.NewBillingEngine(),
		Acts:         genInvokers{Opts: activityOptions()},
		Rail:         rail,
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
	env.RegisterWorkflowWithOptions(wf.CoAuthorPhase2ArtifactWorkflow, workflow.RegisterOptions{Name: executionKindCoAuthor})
	registerGenActivities(env, ps, pipe, wf.Rail)
}

// PROOF 1+2 — branch reconciliation + merge-before-commit + post-merge-read-on-main
// for the Phase-2 per-artifact draft path.
func Test_CoAuthorPhase2_Rail_BranchReconciliation_MergeBeforeCommit_PostMergeReadOnMain(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // dispatch observed Succeeded
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("rail reconciliation workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want coAuthorApproved, got %d", outcome)
	}

	if len(pipe.submits) != 1 {
		t.Fatalf("Phase-2 draft must be a single dispatch, got %d", len(pipe.submits))
	}
	dispatchBranch := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if dispatchBranch == "" {
		t.Fatal("dispatch must carry a non-empty target_branch")
	}

	pdRailAssertPerProjectDispatchTarget(t, pipe.submits[0])
	pdRailAssertSessionBranchRode(t, rail, ps, dispatchBranch)
	pdRailAssertMergeBeforeCommitOnMain(t, log, rail, dispatchBranch)

	if len(base.committed) != 1 || base.committed[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one CommitArtifact(KindPlanningAssumptions) on main, got %v", base.committed)
	}
}

// pdRailAssertPerProjectDispatchTarget — THE PER-PROJECT-DESIGN-DISPATCH ASSERTION
// (UC2 twin of the live-activation gap fix): with the rail WIRED, the Phase-2 design
// dispatch must target the PER-PROJECT repo (the rail's repoRef) + aiarch-design.yml —
// NOT the central construction repo + aiarch-construct.yml.
// The workflow-side dispatchDesignJob decodes the opaque RepoRef ("acct|owner/repo") to
// the RA's RepoTarget{Owner:"owner", Name:"repo"} BEFORE the generated submit invoker, so
// the fake records the decoded "owner/repo".
func pdRailAssertPerProjectDispatchTarget(t *testing.T, sub submitRecord) {
	t.Helper()
	if sub.targetRepo != "owner/repo" {
		t.Fatalf("design dispatch must target the per-project repo %q, got %q", "owner/repo", sub.targetRepo)
	}
	if sub.workflowFile != "aiarch-design.yml" {
		t.Fatalf("design dispatch must target aiarch-design.yml (NOT aiarch-construct.yml), got %q", sub.workflowFile)
	}
}

// pdRailAssertSessionBranchRode asserts the rail opened exactly the dispatch session
// branch + its PR, and that the read-back and the AwaitingReview stage both rode over
// that SAME session branch (THE LOAD-BEARING RECONCILIATION).
func pdRailAssertSessionBranchRode(t *testing.T, rail *scriptedRail, ps *seqProjectState, dispatchBranch string) {
	t.Helper()
	if len(rail.openedBranches) != 1 || rail.openedBranches[0] != dispatchBranch {
		t.Fatalf("OpenBranch must address the dispatch session branch %q, got %v", dispatchBranch, rail.openedBranches)
	}
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != dispatchBranch {
		t.Fatalf("OpenPullRequest head must be the session branch %q, got %v", dispatchBranch, rail.openedPRHeads)
	}

	// THE LOAD-BEARING RECONCILIATION: read-back rode over the dispatch target_branch.
	sawReadBackOnSession := false
	for _, b := range ps.readBranches {
		if b == dispatchBranch {
			sawReadBackOnSession = true
		}
	}
	if !sawReadBackOnSession {
		t.Fatalf("read-back branch must equal the dispatch target_branch %q, got reads %v", dispatchBranch, ps.readBranches)
	}
	if len(ps.stageBranches) != 1 || ps.stageBranches[0] != dispatchBranch {
		t.Fatalf("stage must ride over the dispatch session branch %q, got %v", dispatchBranch, ps.stageBranches)
	}
}

// pdRailAssertMergeBeforeCommitOnMain asserts the approve ordering: the session-branch
// PR merged BEFORE the commit, with a main-path read (the post-merge re-seed) between.
func pdRailAssertMergeBeforeCommitOnMain(t *testing.T, log *seqLog, rail *scriptedRail, dispatchBranch string) {
	t.Helper()
	mergeIdx := log.firstIndexOf("merge")
	commitIdx := log.firstIndexOf("commit")
	if mergeIdx < 0 || commitIdx < 0 {
		t.Fatalf("a green approve must MERGE then COMMIT; ops=%v", log.ops())
	}
	if mergeIdx >= commitIdx {
		t.Fatalf("merge must precede commit-on-main; ops=%v", log.ops())
	}
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+dispatchBranch {
		t.Fatalf("merge must target the session-branch PR pr/%s, got %v", dispatchBranch, rail.mergedPRs)
	}

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

// PROOF 3 (F40) — Reject → redraft on the SAME persistent session branch + the SAME PR.
func Test_CoAuthorPhase2_Rail_RejectRedraftsOnSameSessionBranchAndSamePR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "rework the staffing assumptions"}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if b1 != b2 {
		t.Fatalf("a reject must redraft on the SAME session branch (F40 single-branch); got %q then %q", b1, b2)
	}
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != b1 {
		t.Fatalf("reject must reuse the ONE PR on the persistent branch, got PR heads %v", rail.openedPRHeads)
	}
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+b1 {
		t.Fatalf("the merged PR must be the persistent PR pr/%s, got %v", b1, rail.mergedPRs)
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit after redraft→approve, got %v", base.committed)
	}
}

// PROOF 4 — Failure with the rail WIRED: PhaseFailed lands in StageDraftFailed, the
// dispatch-time rail half ran but the approve-time half never does, nothing commits.
func Test_CoAuthorPhase2_Rail_PhaseFailed_LandsInStageDraftFailed_NoApproveRailNoCommit(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline(pipelineFailed)
	pipe.diagnostic = "aiarch-validate found 2 violations"
	rail := newScriptedRail(true, log)
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
		if view.Stage == StageDrafting {
			t.Fatal("a failed design job must NOT leave perpetual StageDrafting (the wedge), even with the rail wired")
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed after a terminal failure phase, got %d", view.Stage)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT crash the rail-wired workflow: %v", err)
	}
	if rail.count("OpenBranch") == 0 {
		t.Fatal("the dispatch-time rail half (OpenBranch) must have run before the observe")
	}
	if rail.count("OpenPullRequest") != 0 {
		t.Fatalf("a failed draft must NOT open a PR, got %d", rail.count("OpenPullRequest"))
	}
	if rail.count("GetPullRequestStatus") != 0 || rail.count("MergePullRequest") != 0 {
		t.Fatalf("a failed draft must NOT reach the merge guard/merge, got status=%d merge=%d",
			rail.count("GetPullRequestStatus"), rail.count("MergePullRequest"))
	}
	if len(base.staged) != 0 || len(base.committed) != 0 {
		t.Fatalf("a failed draft must stage/commit nothing, got staged=%d committed=%v", len(base.staged), base.committed)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("withdraw from the draft-failed gate must call WithdrawArtifact once, got %d", len(base.withdrawn))
	}
}

// PROOF 5 — Required-check RED → merge BLOCKED, no commit, recovers (ordered).
func Test_CoAuthorPhase2_Rail_RequiredCheckRed_BlocksMerge_NoCommit_Recovers(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()           // draft Succeeds (the run was green) ...
	rail := newScriptedRail(false, log) // ... but the PR's required check is RED at merge time
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a not-green merge guard must not crash the workflow: %v", err)
	}
	if rail.count("GetPullRequestStatus") == 0 {
		t.Fatal("the approve path must consult the merge guard (GetPullRequestStatus)")
	}
	if rail.count("MergePullRequest") != 0 {
		t.Fatalf("a RED required check must BLOCK the merge, got %d merge calls", rail.count("MergePullRequest"))
	}
	if rail.count("PostReview") != 0 {
		t.Fatalf("a RED required check must NOT relay the +1, got %d PostReview calls", rail.count("PostReview"))
	}
	if log.firstIndexOf("merge") != -1 {
		t.Fatalf("a RED required check must produce NO merge event; ops=%v", log.ops())
	}
	if log.firstIndexOf("commit") != -1 {
		t.Fatalf("a RED required check must produce NO commit event; ops=%v", log.ops())
	}
	if len(base.committed) != 0 {
		t.Fatalf("a not-green merge guard must NEVER commit, got %v", base.committed)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("a blocked merge must route to the recovery gate; withdraw expected once, got %d", len(base.withdrawn))
	}
}

// ---- branchAwareRejectFake: faithful PR-rail reject substrate (QA F28) ---------

// branchAwareRejectFake models the PRODUCTION PR-rail reality that caused QA F28 for the
// Phase-2 CoAuthor spine: the draft is staged ONLY on the session branch, so main's slot
// is unpopulated and a MAIN-path reject is a ContractMisuse. Its RejectArtifactOnBranch
// records the session branch (and can fault-inject); its shadowing main-path RejectArtifact
// fails loudly, so a regression to rejecting on main crashes the workflow and the test.
type branchAwareRejectFake struct {
	*fakeProjectState
	mu               sync.Mutex
	rejectBranches   []string
	withdrawBranches []string
	// failRejectOnBranch makes RejectArtifactOnBranch fault terminally (ContractMisuse) —
	// exercises the crash-containment recovery gate.
	failRejectOnBranch bool
	// failWithdrawOnMain, when true, makes the MAIN-path WithdrawArtifact fault (models the
	// unpopulated-main-slot ContractMisuse of the PR rail). Opt-in so ONLY the F30 review-gate
	// withdraw test arms it.
	failWithdrawOnMain bool
}

var _ projectstate.ProjectStateAccess = (*branchAwareRejectFake)(nil)

func (f *branchAwareRejectFake) ReadProjectOnBranch(rc fwra.Context, projectID projectstate.ProjectID, _ string) (projectstate.Project, error) {
	return f.ReadProject(rc, projectID)
}

func (f *branchAwareRejectFake) StageArtifactForReviewOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, _ string, model projectstate.ArtifactModel, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.StageArtifactForReview(fwra.Context{Context: rc.Context, IdempotencyKey: key}, projectID, expectedVersion, model)
}

func (f *branchAwareRejectFake) RejectArtifactOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
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
// (comments dropped) — branchAwareRejectFake models a branch-aware-but-NOT-ledger
// substrate (the old middle rung of the 3-way Ledger→BranchAware→base fallback). Defined
// directly here (not left to embedding promotion of *fakeProjectState's version) because a
// promoted method's internal f.RejectArtifact call resolves against the EMBEDDED type, not
// this outer one — Go has no virtual dispatch through embedding.
func (f *branchAwareRejectFake) RejectArtifactOnBranchWithComments(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, _ int64, _ []projectstate.ReviewComment, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.RejectArtifactOnBranch(rc, projectID, expectedVersion, branch, kind, notes, key)
}

// RejectArtifact (MAIN path) models the PR-rail reality: main's slot is unpopulated, so a
// main-path reject is a ContractMisuse. Shadows the embedded fake so a regression to
// rejecting on main crashes the workflow and this test fails loudly.
func (f *branchAwareRejectFake) RejectArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind, _ string) (projectstate.Version, error) {
	return 0, fwra.New(fwra.ContractMisuse, "projectstate.RejectArtifact: slot "+kind.String()+" is unpopulated (stage a model first)")
}

// WithdrawArtifactOnBranch records the Withdraw on the SESSION BRANCH and delegates to the
// embedded fake's bookkeeping (withdrawn). The main-path WithdrawArtifact is intentionally
// NOT shadowed to fail (the FAILED-gate withdraw legitimately rides main); the F30
// regression guard is the withdrawBranches assertion.
func (f *branchAwareRejectFake) WithdrawArtifactOnBranch(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.withdrawBranches = append(f.withdrawBranches, branch)
	f.mu.Unlock()
	return f.fakeProjectState.WithdrawArtifact(fwra.Context{Context: rc.Context, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

// WithdrawArtifact (MAIN path) is the base behavior UNLESS failWithdrawOnMain is armed, in
// which case it models the PR-rail reality that caused QA F30: main's slot is unpopulated,
// so a main-path withdraw is a ContractMisuse. Armed only by the F30 review-gate test so a
// regression to withdrawing on main crashes the workflow and the test fails loudly.
func (f *branchAwareRejectFake) WithdrawArtifact(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind, notes string) (projectstate.Version, error) {
	if f.failWithdrawOnMain {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.WithdrawArtifact: slot "+kind.String()+" is unpopulated (stage a model first)")
	}
	return f.fakeProjectState.WithdrawArtifact(rc, projectID, expectedVersion, kind, notes)
}

// THE QA F28 REGRESSION (Phase-2 twin) — Reject/"Send back" against the PR rail records
// the Rejected status ON THE SESSION BRANCH (not main, where the version mismatches AND
// the slot is unpopulated), the workflow survives, and a fresh redraft is re-dispatched.
// (Under thin dispatch the reject's feedback reaches the agent via the review ledger, not a
// composed prompt — the anchored-comment seed is exercised by the dedicated seed tests below;
// this Notes-only reject seeds nothing.)
func Test_CoAuthorPhase2_Rail_Reject_RecordsOnSessionBranch_RedraftCarriesFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &branchAwareRejectFake{fakeProjectState: base}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const rejectNotes = "rework the staffing assumptions"
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a PR-rail reject must not crash the workflow: %v", err)
	}
	if len(ps.rejectBranches) != 1 || ps.rejectBranches[0] == "" {
		t.Fatalf("reject must target the session branch, got %v", ps.rejectBranches)
	}
	if len(base.rejected) != 1 || base.rejected[0].kind != projectstate.KindPlanningAssumptions || base.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact(KindPlanningAssumptions, %q) on the session branch, got %v", rejectNotes, base.rejected)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("a reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
	// Thin dispatch: the redraft ships the command NAME, not a feedback-laden prompt.
	if got := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputCommand]; got != "planning-assumptions-draft" {
		t.Fatalf("redraft command = %q, want planning-assumptions-draft", got)
	}
}

// CRASH CONTAINMENT AT THE REVIEW GATE (Phase-2 twin, QA F28 item 2). An activity fault
// while RECORDING the Reject must not kill the workflow: the spine lands at the
// human-visible StageDraftFailed recovery gate KEEPING the feedback, so a Retry redrafts
// with the architect's notes rather than silently discarding the send-back.
func Test_CoAuthorPhase2_Rail_RejectWriteFaults_RecoversAtFailedGate_RetainsFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &branchAwareRejectFake{fakeProjectState: base, failRejectOnBranch: true}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const rejectNotes = "rework the staffing assumptions"
	// First gate: REJECT (the write FAULTS terminally) → crash containment lands at the
	// StageDraftFailed gate carrying the feedback.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
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
		env.SignalWorkflow(signalRedraft, redraftSignal{})
	}, 60*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 100*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a faulted reject must not crash the workflow: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("the retry must issue a SECOND dispatch, got %d submits", len(pipe.submits))
	}
	// Thin dispatch: the retry ships the command NAME. The retained feedback's survival across
	// the fault is what matters here (a Notes-only reject seeds no ledger comments); the
	// anchored-comment seed-before-redraft is proven by the dedicated seed tests below.
	if got := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputCommand]; got != "planning-assumptions-draft" {
		t.Fatalf("retry command = %q, want planning-assumptions-draft", got)
	}
}

// ---- f29BranchFake (Phase-2 twin): version-enforcing branch-aware substrate --

// f29BranchFake models the F29 reality for the Phase-2 spine: MAIN and the reused SESSION
// BRANCH sit at DIFFERENT versions. A fresh workflow captures main's version, but the
// Action's prior commits left the branch AHEAD. The read-back reads the branch version; a
// stage expecting the stale main version Conflicts. This fake ENFORCES the branch version
// on stage-on-branch (unlike the version-ignoring base fake), so the fix's convergence is
// proven.
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
	p.Version = f.branchVer
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
	// Terminal in the F29 tests; leave branchVer untouched.
	return f.branchVer, nil
}

// THE QA F29 REGRESSION (Phase-2 twin) — a fresh workflow reusing a session branch already
// AHEAD of main stages against the ACTUAL branch version and CONVERGES, instead of
// Conflicting non-recoverably against the stale main-captured version and crashing.
func Test_CoAuthorPhase2_Rail_StageAgainstDirtyBranch_Converges_NoCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("staging against a dirty session branch must converge, not crash: %v", err)
	}
	if ps.branchVer != 5 {
		t.Fatalf("stage must converge and advance the branch version to 5, got %d (expecteds=%v)", ps.branchVer, ps.stageExpecteds)
	}
	if last := ps.stageExpecteds[len(ps.stageExpecteds)-1]; last != 4 {
		t.Fatalf("the converged stage must expect the branch version 4, got %d (expecteds=%v)", last, ps.stageExpecteds)
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one commit after the converged stage → approve, got %v", base.committed)
	}
}

// THE QA F30 REGRESSION (Phase-2 twin) — Withdraw against the PR rail records the Withdrawn
// status ON THE SESSION BRANCH (not main, where the version mismatches AND the slot is
// unpopulated), the workflow survives, and ends withdrawn. failWithdrawOnMain arms the
// main-path guard so a regression to withdrawing on main crashes and this test fails loudly.
func Test_CoAuthorPhase2_Rail_Withdraw_RecordsOnSessionBranch_NoCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &branchAwareRejectFake{fakeProjectState: base, failWithdrawOnMain: true}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw, Feedback: &ReviewFeedback{Notes: "abandon this draft"}})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a PR-rail withdraw must not crash the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want coAuthorWithdrawn, got %d", outcome)
	}
	if len(ps.withdrawBranches) != 1 || ps.withdrawBranches[0] == "" {
		t.Fatalf("withdraw must target the session branch, got %v", ps.withdrawBranches)
	}
	if len(base.withdrawn) != 1 || base.withdrawn[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one WithdrawArtifact(KindPlanningAssumptions) on the session branch, got %v", base.withdrawn)
	}
}

// F40 (Phase-2 twin) — a Retry at the StageDraftFailed gate redrafts on the SAME persistent
// session branch (the F32 branch-per-retry topology is unwound; the template's refresh-from-
// main handles a stale base). Drives a stage fault → retry-via-reject (with feedback) and
// asserts the redraft dispatch targets the SAME branch. (Under thin dispatch the retained
// feedback reaches the agent via the ledger, not the prompt; this Notes-only reject seeds none.)
func Test_CoAuthorPhase2_Rail_RetryAtFailedGate_SameBranch_RetainsFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4, stageFailsRemaining: 1}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const retryNotes = "fix the staffing assumptions before redrafting"
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: retryNotes}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if b1 != b0 {
		t.Fatalf("a failed-gate retry must redraft on the SAME session branch (F40); got %q then %q", b0, b1)
	}
	if strings.Contains(b1, "-amend-") {
		t.Fatalf("the retry branch must be the stable session branch (no amendment suffix), got %q", b1)
	}
}

// THE QA F35 REGRESSION (Phase-2 twin) — an approve-window fault (GetPullRequestStatus 403
// → Auth kind) must NOT kill the workflow. After the bounded retry is exhausted the session
// RETURNS to AwaitingReview with a queryable notice, and a re-approve merges. Not a redraft.
func Test_CoAuthorPhase2_Rail_ApproveStatusFault_ReturnsToAwaitingReview_ReapproveMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: &seqLog{}}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	// The first approve exhausts the bounded long-backoff budget (F-QA2-49: 60s → 120s → 240s).
	rail.statusAuthFailsRemaining = railAuthRetryLongMaxAttempts

	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
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
		if view.FailureReason == nil || !strings.Contains(*view.FailureReason, "approve") {
			t.Fatalf("the returned session must carry a re-approve notice, got %v", view.FailureReason)
		}
	}, 500*time.Second) // after the ~420s long-backoff budget (60+120+240) exhausts (F-QA2-49)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 520*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if len(rail.mergedPRs) != 1 {
		t.Fatalf("exactly one merge (on the successful re-approve), got %v", rail.mergedPRs)
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one commit after re-approve, got %v", base.committed)
	}
}

// BOUNDED RESILIENCE (QA F35, Phase-2 twin) + THE F-QA2-49 LONG-BACKOFF SURVIVAL PROOF.
// A secondary-rate-limit 403 burst faults every attempt but the LAST: the fault clears
// before the FINAL long-backoff attempt (60s + 120s + 240s of durable timers — past the
// >=60s cool-down GitHub demands), the bounded retry absorbs the whole burst, and the
// merge completes on the FIRST approve. Under the OLD ~30s/3-attempt budget this exact
// burst would have exhausted (there WAS no 4th attempt).
func Test_CoAuthorPhase2_Rail_ApproveStatusTransient_RetriesThenMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: &seqLog{}}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	rail.statusAuthFailsRemaining = railAuthRetryLongMaxAttempts - 1 // fault clears before the FINAL long-backoff attempt

	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if n := rail.calls["GetPullRequestStatus"]; n != railAuthRetryLongMaxAttempts {
		t.Fatalf("want %d bounded GetPullRequestStatus attempts (%d faults + 1 final success), got %d", railAuthRetryLongMaxAttempts, railAuthRetryLongMaxAttempts-1, n)
	}
	if len(rail.mergedPRs) != 1 {
		t.Fatalf("the merge must complete on the first approve, got %v", rail.mergedPRs)
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit on the first approve, got %v", base.committed)
	}
}

// F-QA2-44 (Phase-2 twin of the live gtdapp kind=3 defect). The approve arrives AFTER the
// dispatch-time installation token expired (~1h lifetime; the observed approve came 8+
// hours later). The workflow used to thread the ONE cached dispatch-time credential into
// the merge window, so every approve 403'd forever (non-retryable Auth). The fix: the
// approve arm mints a FRESH token at gate-decision time. This test arms token-validity
// enforcement, expires the dispatch-time token before the approve, and proves the approve
// still merges+commits via a second mint whose fresh token every merge-window verb rode.
func Test_CoAuthorPhase2_Rail_ApproveAfterTokenExpiry_RemintsFreshToken_Merges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: &seqLog{}}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	rail.enforceTokenValidity = true

	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		// The human returns hours later: the dispatch-time token (tok-1) has EXPIRED.
		// Any merge-window verb presenting it now 403s.
		rail.expireCurrentToken()
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if n := rail.count("GetInstallationToken"); n != 2 {
		t.Fatalf("want two GetInstallationToken mints (dispatch + approve re-mint), got %d", n)
	}
	// Every merge-window verb rode the FRESH token — never the expired dispatch-time one.
	for _, verb := range []string{"GetPullRequestStatus", "PostReview", "MergePullRequest"} {
		creds := rail.creds[verb]
		if len(creds) == 0 {
			t.Fatalf("expected %s to run in the approve window (creds: %+v)", verb, rail.creds)
		}
		for _, c := range creds {
			if c != "tok-2" {
				t.Fatalf("%s must present the freshly-minted token tok-2, got %q (creds: %+v)", verb, c, rail.creds)
			}
		}
	}
	if len(rail.mergedPRs) != 1 {
		t.Fatalf("exactly one merge on the re-minted approve, got %v", rail.mergedPRs)
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one CommitArtifact(KindPlanningAssumptions) on main, got %v", base.committed)
	}
}

// F-QA2-44 VERSION GATE (replay pin — Phase-2 twin). A PRE-FEATURE decision attempt (its
// history recorded the merge-window verbs WITHOUT a preceding gate mint) must replay the
// OLD command sequence: GetVersion resolves DefaultVersion for that attempt's PER-DECISION
// change id (gate-decision-token-remint-p2-<seq>) and the approve arm must NOT schedule
// the re-mint activity. (The id is per attempt — NOT static — so a live stuck execution's
// old recorded attempts stay pinned while its NEXT attempt resolves v1 and heals.)
func Test_CoAuthorPhase2_Rail_GateRemint_VersionGate_PreFeatureAttemptSkipsRemint(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: &seqLog{}}
	pipe := newFakePipeline()
	// No validity enforcement: pre-feature histories only ever succeeded INSIDE the token's
	// fresh hour, so the dispatch-time credential still works on this replayed attempt.
	rail := newScriptedRail(true, &seqLog{})

	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// Simulate the PRE-FEATURE recorded attempt: GetVersion resolves DefaultVersion for
	// decision attempt #1 (no version marker in the replayed history).
	env.OnGetVersion("gate-decision-token-remint-p2-1", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if n := rail.count("GetInstallationToken"); n != 1 {
		t.Fatalf("a pre-feature (DefaultVersion) decision attempt must NOT re-mint: want 1 mint, got %d", n)
	}
	if len(base.committed) != 1 {
		t.Fatalf("the pre-feature approve must still commit exactly once, got %v", base.committed)
	}
}

// PROOF 6 (F40 live-bug fix) — the PR is opened ONLY AFTER the read-back confirms a
// committed model on the session branch (branch has ≥1 commit beyond main), never at
// session start on a freshly-cut zero-commit branch (the observed gtdapp 422 "no commits
// between base and head"). Ordered assertion: the FIRST "openPR" strictly follows the
// FIRST "readBranch" (read-back). Reject → redraft reuses the SAME one PR; approve merges.
func Test_CoAuthorPhase2_Rail_OpenPR_OnlyAfterReadBack_ReuseThenMerge(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "tighten"}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("openPR-after-read-back workflow error: %v", err)
	}

	// THE LOAD-BEARING ORDERING (the reorder fix): OpenBranch, then a committed draft, then
	// the read-back confirms it — and ONLY THEN is the PR opened.
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
	if rail.count("OpenBranch") == 0 {
		t.Fatalf("OpenBranch (dispatch-time half) must run so the Action has a branch to commit on; ops=%v", log.ops())
	}

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

// F40 RUN-SCOPING NOTE (former Test_composeIdempotencyKey_RunScoped_DistinctPerRun):
// the Manager-local composeIdempotencyKey helper is DELETED with the last custom
// Activity (B9 follow-up) — every idempotency key is now derived by the platform-
// generated genActivityIdempotencyKey (activities.gen.go), which threads the RunID as
// the middle segment of the 3-part "${workflowId}:${runId}:${activityId}" key BY
// CONSTRUCTION, so a fresh session (new run) of the same workflow ID can never dedup
// onto a predecessor session's completed GitHub run (the observed amendment 422). The
// derivation has no pure seam left to pin in this package (it reads the live Activity
// context inside DO-NOT-EDIT generated code); the format is the platform emitter's
// contract, shared by all five managers.

// F40 AMENDMENT NO-CHANGE GUARD — a Phase-2 amendment session whose Action "succeeded"
// but committed NOTHING that changed the artifact (branch read-back == committed main
// model) must land at StageDraftFailed with the honest reason and open NO PR. Withdraw
// ends clean.
func Test_CoAuthorPhase2_Rail_Amendment_NoChange_LandsFailedGate_NoPR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	// seqProjectState.ReadProjectOnBranch returns the SAME project as main ⇒ no advancement.
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // the design job "succeeds"
	rail := newScriptedRail(true, log)
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
		ArtifactKind: KindPlanningAssumptions,
		Amendment:    1,
		Feedback:     &ReviewFeedback{Notes: "please tighten"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("no-change amendment must not crash: %v", err)
	}
	if rail.count("OpenPullRequest") != 0 {
		t.Fatalf("a no-change amendment must open NO PR (zero-new-commit branch), got %d", rail.count("OpenPullRequest"))
	}
	if rail.count("MergePullRequest") != 0 {
		t.Fatalf("a no-change amendment must NOT merge, got %d", rail.count("MergePullRequest"))
	}
	if len(base.committed) != 0 {
		t.Fatalf("a no-change amendment must commit nothing, got %v", base.committed)
	}
}

// amendmentIndexFor — the pre-field fix rule (Phase-2 twin). A COMMITTED slot yields
// max(1, Revisions): a slot committed BEFORE the Revisions field existed reads Revisions=0
// yet is still an amendment (index 1). Non-committed slots are the normal path (0).
func Test_amendmentIndexFor_Rule(t *testing.T) {
	if got := projectstate.AmendmentIndexFor(projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 0}); got != 1 {
		t.Fatalf("pre-field committed slot must yield amendment index 1, got %d", got)
	}
	if got := projectstate.AmendmentIndexFor(projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 4}); got != 4 {
		t.Fatalf("committed slot at revision 4 must yield amendment index 4, got %d", got)
	}
	for _, st := range []projectstate.ArtifactReviewStatus{
		projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected, projectstate.ReviewWithdrawn,
	} {
		if got := projectstate.AmendmentIndexFor(projectstate.ArtifactSlot{Status: st, Revisions: 5}); got != 0 {
			t.Fatalf("non-committed slot (status %d) must yield amendment index 0, got %d", st, got)
		}
	}
}

// F47 (thin dispatch) — the REDRAFT-SIGNAL feedback path (what RequestArtifactDraft delivers via
// SignalWithStart, replacing the bare ExecuteWorkflow that DROPPED the feedback). A draft job
// fails → StageDraftFailed gate; the redraft signal carries the operator's anchored fix comment.
// Under thin dispatch the drafting agent reads context ONLY via getReviewThread, so that memory-
// only redraft-signal feedback must be SEEDED into the durable review ledger BEFORE the next draft
// dispatch — else it evaporates (the redraft-signal sibling of the failed-gate-reject seed gap).
func Test_CoAuthorPhase2_RedraftSignal_SeedsAnchoredFeedbackToLedger_BeforeRedraft(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	// First draft FAILS (→ StageDraftFailed gate); the redraft (after the signal) succeeds.
	pipe := newFakePipeline(pipelineFailed, pipelineSucceeded)
	ps := &ledgerThreadFake{seqProjectState: &seqProjectState{fakeProjectState: base, log: &seqLog{}}, pipe: pipe}
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	const (
		fixPath = "$.resources"
		fixText = "resources must be plain strings, not objects"
	)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalRedraft, redraftSignal{Feedback: &ReviewFeedback{Comments: []AnchoredComment{{JSONPath: fixPath, Text: fixText}}}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a redraft-signal retry must not crash: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("the redraft signal must trigger a SECOND draft dispatch, got %d", len(pipe.submits))
	}
	// THE FIX: the redraft-signal feedback was seeded into the review ledger exactly once, carrying
	// the operator's anchored comment as a durable OPEN entry the agent reads with getReviewThread.
	if len(ps.seededRounds) != 1 || len(ps.seededComments) != 1 || len(ps.seededComments[0]) != 1 {
		t.Fatalf("the redraft-signal feedback must seed exactly one anchored comment, got rounds=%v comments=%v", ps.seededRounds, ps.seededComments)
	}
	if c := ps.seededComments[0][0]; c.Anchor != fixPath || c.Text != fixText {
		t.Fatalf("the seeded comment must be the redraft-signal feedback, got %+v", c)
	}
	// ORDERING: the seed landed BEFORE the redraft dispatch — only the first (failed) dispatch had
	// been submitted at seed time (count == 1), so the seed precedes the SECOND dispatch.
	if len(ps.seededAtSubmits) != 1 || ps.seededAtSubmits[0] != 1 {
		t.Fatalf("the ledger seed must land BEFORE the redraft dispatch (want 1 prior dispatch at seed time), got %v", ps.seededAtSubmits)
	}
}

// F48 — the Temporal activity-boundary codec MUST carry the durable review ledger. Without it,
// loadReviewThread (which reads the session branch through this projectEnvelope) returned [] even
// though the reject-with-comments append lives in the branch git — so the session-state query, the
// ledger the drafting agent reads with getReviewThread, and the approve gate all saw an empty thread.
func Test_projectEnvelope_PreservesReviewThread(t *testing.T) {
	var p projectstate.Project
	p.PlanningAssumptions = projectstate.ArtifactSlot{
		Status: projectstate.ReviewAwaitingReview,
		Model:  &projectstate.PlanningAssumptions{CalendarDaysPerWeek: 5},
		ReviewThread: []projectstate.ReviewComment{
			{ID: "r0c1", Text: "resources must be plain strings, not objects", AuthorRole: "architect", Round: 0, Status: projectstate.ReviewCommentOpen},
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
	thread := slotFor(got, projectstate.KindPlanningAssumptions).ReviewThread
	if len(thread) != 1 {
		t.Fatalf("the review thread must survive the Temporal codec round-trip, got %d comments: %+v", len(thread), thread)
	}
	if thread[0].ID != "r0c1" || thread[0].Status != projectstate.ReviewCommentOpen || thread[0].Text == "" {
		t.Fatalf("the codec must preserve the comment's id/status/text, got %+v", thread[0])
	}
}

// ledgerThreadFake stores a MUTABLE review thread and implements the review-ledger seam so a
// workflow-level test can drive reject-with-comments and read the thread back THROUGH the real
// Temporal codec (F48). ReadProjectOnBranch injects the live thread into the slot the read-back
// returns, modelling the git branch state the reject-append produced.
type ledgerThreadFake struct {
	*seqProjectState
	tmu    sync.Mutex
	thread []projectstate.ReviewComment
	// The following record the failed-gate ledger seed (thin dispatch) so a test can assert the
	// seed fired with exactly the retained comments and landed BEFORE the redraft dispatch.
	seededRounds   []int64
	seededComments [][]projectstate.ReviewComment
	// pipe, when set, lets the seed recorder capture the dispatch count AT SEED TIME
	// (seededAtSubmits[i] = dispatches already submitted when the i-th seed fired).
	pipe            *fakePipeline
	seededAtSubmits []int
}

var _ projectstate.ProjectStateAccess = (*ledgerThreadFake)(nil)

func (f *ledgerThreadFake) snapshot() []projectstate.ReviewComment {
	f.tmu.Lock()
	defer f.tmu.Unlock()
	return append([]projectstate.ReviewComment(nil), f.thread...)
}

func (f *ledgerThreadFake) ReadProjectOnBranch(rc fwra.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	proj, err := f.seqProjectState.ReadProjectOnBranch(rc, projectID, branch)
	if err != nil {
		return projectstate.Project{}, err
	}
	slot := proj.PlanningAssumptions
	slot.ReviewThread = f.snapshot()
	proj.PlanningAssumptions = slot
	return proj, nil
}

func (f *ledgerThreadFake) RejectArtifactOnBranchWithComments(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, round int64, comments []projectstate.ReviewComment, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.tmu.Lock()
	for i, c := range comments {
		f.thread = append(f.thread, projectstate.ReviewComment{
			ID: fmt.Sprintf("r%dc%d", round, i+1), Anchor: c.Anchor, AnchorText: c.AnchorText,
			Text: c.Text, AuthorRole: c.AuthorRole, Round: round, Status: projectstate.ReviewCommentOpen,
		})
	}
	f.tmu.Unlock()
	return f.RejectArtifactOnBranch(rc, projectID, expectedVersion, branch, kind, notes, key)
}

func (f *ledgerThreadFake) SetReviewCommentStatusOnBranch(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, commentID string, status string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.tmu.Lock()
	for i := range f.thread {
		if f.thread[i].ID == commentID {
			f.thread[i].Status = status
		}
	}
	f.tmu.Unlock()
	return f.bump(), nil
}

func (f *ledgerThreadFake) SeedReviewCommentsOnBranch(_ fwra.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, _ string, _ projectstate.ArtifactKind, round int64, comments []projectstate.ReviewComment, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.tmu.Lock()
	f.seededRounds = append(f.seededRounds, round)
	f.seededComments = append(f.seededComments, comments)
	if f.pipe != nil {
		f.seededAtSubmits = append(f.seededAtSubmits, f.pipe.submitCount())
	}
	for i, c := range comments {
		f.thread = append(f.thread, projectstate.ReviewComment{ID: fmt.Sprintf("r%dc%d", round, i+1), Anchor: c.Anchor, AnchorText: c.AnchorText, Text: c.Text, AuthorRole: c.AuthorRole, Round: round, Status: projectstate.ReviewCommentOpen})
	}
	f.tmu.Unlock()
	return expectedVersion, nil
}

// F48 END-TO-END — reject-with-comments must flow through the whole review-ledger loop now that
// the Temporal codec carries the thread: (a) the session-state query shows the open entry, (b)
// the reject loops to a redraft dispatch (the comment now reaches the agent via getReviewThread),
// and (c) Approve is BLOCKED until the open comment is waived. All three read the workflow's
// in-memory reviewThread, which is
// reloaded from the branch read-back — the read that silently dropped the thread pre-F48.
func Test_CoAuthorPhase2_RejectWithComments_ThreadRefreshes_QueryPromptAndApproveGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &ledgerThreadFake{seqProjectState: &seqProjectState{fakeProjectState: base, log: &seqLog{}}}
	pipe := newFakePipeline() // every dispatch succeeds
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)
	// The review-ledger designSessionAccess ops (Set/Seed) are registered by
	// registerGenActivities (inside registerRailCoAuthor), backed by
	// NewDesignSessionAccess(wf.ProjectState) — the generated ProjectStateAccess contract
	// requires the ledger verbs unconditionally post-C2-fold, and ps (ledgerThreadFake)
	// overrides them with real thread-tracking behavior, so the wrapper's direct forward
	// routes to it for real.

	const commentText = "resources must be plain strings, not objects"

	// t=30s: at the first AwaitingReview, REJECT with an anchored comment.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{
			Decision: ReviewReject,
			Feedback: &ReviewFeedback{Comments: []AnchoredComment{{JSONPath: "$.resources", Text: commentText}}},
		})
	}, 30*time.Second)

	// t=80s: at the second AwaitingReview, the query must SHOW the open comment; then Approve
	// (which must be BLOCKED because the comment is still open).
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if len(view.ReviewThread) != 1 || view.ReviewThread[0].Status != projectstate.ReviewCommentOpen {
			t.Fatalf("(a) the session-state query must show the OPEN reject comment, got %+v", view.ReviewThread)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 80*time.Second)

	// t=130s: Approve must have been BLOCKED (still AwaitingReview, nothing committed). Then waive.
	env.RegisterDelayedCallback(func() {
		enc, _ := env.QueryWorkflow(querySessionState)
		var view SessionStateView
		_ = enc.Get(&view)
		if view.Stage != StageAwaitingReview {
			t.Fatalf("(c) Approve must be BLOCKED while a comment is open — want AwaitingReview, got stage %d", view.Stage)
		}
		if len(base.committed) != 0 {
			t.Fatalf("(c) nothing may commit while a comment is open, got %v", base.committed)
		}
		env.SignalWorkflow(signalSetCommentStatus, setCommentStatusSignal{CommentID: "r0c1", Status: projectstate.ReviewCommentWaived})
	}, 130*time.Second)

	// t=170s: with the comment waived, Approve now merges.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 170*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the review-ledger loop must not crash: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("after waive + approve the session must be Approved, got %d", outcome)
	}
	if len(base.committed) != 1 {
		t.Fatalf("approve-after-waive must commit once, got %v", base.committed)
	}
	// (b) the reject looped to a REDRAFT dispatch (the second submit). Under thin dispatch the
	// open review-ledger comment reaches the drafting agent via getReviewThread (proven by (a)
	// showing it in the live thread), not a composed prompt — the redraft ships only the command.
	if len(pipe.submits) < 2 {
		t.Fatalf("the reject must trigger a redraft dispatch, got %d", len(pipe.submits))
	}
	if got := pipe.submits[1].dispatchInputs[dispatchInputCommand]; got != "planning-assumptions-draft" {
		t.Fatalf("(b) the redraft must dispatch command=planning-assumptions-draft, got %q", got)
	}
}

// NO DOUBLE-SEED. A review-gate REJECT folds its feedback into the ledger via the reject write
// itself (RejectArtifactOnBranchWithComments). The pre-dispatch failed-gate seed must therefore
// SKIP it — otherwise the same comments would be seeded a SECOND time (at a different round →
// duplicate ledger entries). Proven here: after a review-gate reject → redraft, the failed-gate
// SEED activity never fired (the reject write is the sole ledger write for that feedback).
func Test_CoAuthorPhase2_ReviewGateReject_NotDoubleSeeded(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	pipe := newFakePipeline() // every dispatch succeeds → reaches the REVIEW gate (not a failed gate)
	ps := &ledgerThreadFake{seqProjectState: &seqProjectState{fakeProjectState: base, log: &seqLog{}}, pipe: pipe}
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// First gate: REJECT with anchored feedback (the reject write seeds it). Second gate: WITHDRAW.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{
			Decision: ReviewReject,
			Feedback: &ReviewFeedback{Comments: []AnchoredComment{{JSONPath: "$.resources", Text: "layering violation"}}},
		})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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

// MANAGED-SCAFFOLD SYNC GATE (sync-on-dispatch, 2026-07-06; UC2 twin of the
// systemdesign proof). A managed-scaffold sync failure BLOCKS the Phase-2 design
// dispatch: NO design job is submitted, NO session branch is opened, and the session
// lands at the human-visible StageDraftFailed gate (contained, never a crash).
func Test_CoAuthorPhase2_Rail_ScaffoldSyncFailure_BlocksDispatch_LandsAtFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, log)
	rail.syncErr = fwra.New(fwra.ContractMisuse, "seated workflow could not be refreshed")
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

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed managed-scaffold sync must be CONTAINED at the failed gate, not crash: %v", err)
	}
	if rail.count("SyncManagedScaffold") == 0 {
		t.Fatal("the managed-scaffold sync must have been attempted")
	}
	if len(pipe.submits) != 0 {
		t.Fatalf("a failed sync must BLOCK the design-job dispatch, got %d submits", len(pipe.submits))
	}
	if rail.count("OpenBranch") != 0 || rail.count("OpenPullRequest") != 0 {
		t.Fatalf("a failed sync must not open a branch/PR, got openBranch=%d openPR=%d",
			rail.count("OpenBranch"), rail.count("OpenPullRequest"))
	}
	if len(base.staged) != 0 || len(base.committed) != 0 {
		t.Fatalf("a blocked dispatch must stage/commit nothing, got staged=%d committed=%v", len(base.staged), base.committed)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("withdraw from the failed gate must call WithdrawArtifact once, got %d", len(base.withdrawn))
	}
}

// THE VERSION GATE (UC2 twin of the systemdesign proof; live regression gtdapp:5).
// A Phase-2 design execution already in flight when the managed-scaffold sync
// deployed has no history event for the sync activity — GetVersion pins it
// (DefaultVersion) to the OLD command sequence, so it must complete its full happy
// path with ZERO SyncManagedScaffold calls even with syncErr armed.
func Test_CoAuthorPhase2_Rail_ScaffoldSync_VersionGate_PreFeatureExecutionSkipsSync(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, log)
	// syncErr armed: an UN-GATED sync would derail the pre-feature run at the failed gate.
	rail.syncErr = fwra.New(fwra.ContractMisuse, "sync must not run for a pre-feature execution")
	wf := newRailWorkflows(rail)
	registerRailCoAuthor(env, wf, ps, pipe)

	// Simulate a PRE-FEATURE in-flight execution: GetVersion resolves DefaultVersion.
	env.OnGetVersion("managed-scaffold-sync", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

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
	if got := rail.count("SyncManagedScaffold"); got != 0 {
		t.Fatalf("a pre-feature (DefaultVersion) execution must NEVER call SyncManagedScaffold, got %d", got)
	}
	if len(pipe.submits) != 1 || len(base.committed) != 1 {
		t.Fatalf("the pre-feature spine must dispatch and commit exactly once, got submits=%d committed=%v", len(pipe.submits), base.committed)
	}
}

// acknowledgestale_test.go covers the F-GTD-12 live-session ack gate: acknowledging
// staleness on a slot whose amendment session is LIVE is refused with FailedPrecondition
// (the wire's 409/"failed_precondition"), because the ack's main-branch commit would turn
// the amendment's review PR merge-DIRTY and wedge its approve.

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
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(
		&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
			},
		}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).Return(
		fakeEncodedSessionView{view: SessionStateView{Stage: StageAwaitingReview}}, nil)

	m := &projectDesignManager{client: mc, projectState: &fakeProjectState{}}
	err := m.AcknowledgeStaleBasis(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions, "unaffected")
	pde := asProjectDesignError(t, err)
	if pde.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition while the amendment session is live, got %d (%v)", pde.Kind, err)
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
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(nil, serviceerror.NewNotFound("workflow not found"))

	m := &projectDesignManager{client: mc, projectState: &fakeProjectState{}}
	err := m.AcknowledgeStaleBasis(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions, "unaffected")
	if err != nil {
		t.Fatalf("expected the liveness gate to pass and the ack to succeed, got %v", err)
	}
	mc.AssertExpectations(t)
}

// A session that closed COMPLETED after committing is TERMINAL (the durable slot renders
// StageCommitted): the gate passes and the ack proceeds (and, post-C2-fold, succeeds).
func Test_AcknowledgeStaleBasis_CompletedSession_PassesLivenessGate(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(
		&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
				Status: enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
			},
		}, nil)
	// NO QueryWorkflow expectation: a COMPLETED run's replayed query is bypassed.

	proj := committedPhase2Project(id, KindPlanningAssumptions)
	proj.PlanningAssumptions.Model = &projectstate.PlanningAssumptions{Notes: "n"}
	m := &projectDesignManager{client: mc, projectState: &fakeProjectState{project: proj}}
	err := m.AcknowledgeStaleBasis(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions, "unaffected")
	if err != nil {
		t.Fatalf("a committed (terminal) session must pass the liveness gate and succeed, got %v", err)
	}
	mc.AssertExpectations(t)
}

// The live set is exactly the non-terminal stages: drafting / assemblingSdp /
// awaitingReview / redrafting / draftFailed (the recovery gate keeps the branch+PR).
func Test_SessionStageIsLive(t *testing.T) {
	live := []SessionStage{StageDrafting, StageAssemblingSDP, StageAwaitingReview, StageRedrafting, StageDraftFailed}
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

// TestDeriveClassRates_FromModelTier checks the AI $/day derivation (F11b) against the
// hand-computed price list × the default throughput (2 MTok in / 0.5 MTok out per day):
//
//	fable  = 2×$10 + 0.5×$50 = $45.00 → 4500¢
//	opus   = 2×$5  + 0.5×$25 = $22.50 → 2250¢
//	sonnet = 2×$3  + 0.5×$15 = $13.50 → 1350¢
func TestDeriveClassRates_FromModelTier(t *testing.T) {
	var pa projectstate.PlanningAssumptions // empty rate card ⇒ documented defaults
	rates := deriveClassRates(pa, []string{"system-architect", "senior-developer", "junior-developer"})

	want := map[string]int64{
		"system-architect": 4500, // fable
		"senior-developer": 2250, // opus
		"junior-developer": 1350, // sonnet
	}
	for class, cents := range want {
		got := rates[class]
		if got.Currency != "USD" || got.MinorUnits != cents {
			t.Errorf("%s rate = %+v, want %d¢ USD", class, got, cents)
		}
	}
}

// TestDeriveClassRates_UnknownClassDefaultsSonnet: a stale/unknown class (e.g. the
// phantom "architect") must still resolve so the option assembles — it falls back to the
// sonnet tier (F11d: phantom classes map to no agent).
func TestDeriveClassRates_UnknownClassDefaultsSonnet(t *testing.T) {
	rates := deriveClassRates(projectstate.PlanningAssumptions{}, []string{"architect"})
	if got := rates["architect"]; got.MinorUnits != 1350 || got.Currency != "USD" {
		t.Errorf("unknown class rate = %+v, want 1350¢ USD (sonnet fallback)", got)
	}
}

// TestDeriveClassRates_AuthoredCardOverridesDefault: an authored RateCard entry wins over
// the default (F11a), including a higher throughput or a top-tier model.
func TestDeriveClassRates_AuthoredCardOverridesDefault(t *testing.T) {
	pa := projectstate.PlanningAssumptions{
		RateCard: map[string]projectstate.WorkerRateSpec{
			"junior-developer": {ModelID: "opus", MegatokensInPerDay: 2, MegatokensOutPerDay: 0.5},
		},
	}
	rates := deriveClassRates(pa, []string{"junior-developer"})
	if got := rates["junior-developer"]; got.MinorUnits != 2250 {
		t.Errorf("authored opus rate = %+v, want 2250¢ (opus, not sonnet default)", got)
	}
}

// TestIndirectDailyRate_DefaultWhenUnset covers the F6 fallback.
func TestIndirectDailyRate_DefaultWhenUnset(t *testing.T) {
	if got := indirectDailyRateOf(projectstate.PlanningAssumptions{}); got != defaultIndirectDailyRate {
		t.Errorf("unset indirect rate = %+v, want default %+v", got, defaultIndirectDailyRate)
	}
	authored := projectstate.Money{MinorUnits: 999, Currency: "USD"}
	if got := indirectDailyRateOf(projectstate.PlanningAssumptions{IndirectDailyRate: authored}); got != authored {
		t.Errorf("authored indirect rate = %+v, want %+v", got, authored)
	}
}

// TestRateForSpec_FullModelIds covers the priceFamily normalization: rate-card modelIds
// are authored as FULL API ids while apiPricing is keyed by family — the exact-key
// lookup silently priced every full id as sonnet (gtdapp 2026-07-11).
func TestRateForSpec_FullModelIds(t *testing.T) {
	cases := []struct {
		id      string
		in, out float64
		want    int64
	}{
		{"claude-opus-4-8", 5, 1.5, 5*500 + 1.5*2500},        // opus, NOT sonnet fallback
		{"claude-sonnet-5", 8, 2, 8*300 + 2*1500},            // sonnet
		{"claude-haiku-4-5-20251001", 10, 3, 10*100 + 3*500}, // haiku
		{"claude-fable-5", 6, 1.5, 6*1000 + 1.5*5000},        // fable
		{"totally-unknown-model", 2, 0.5, 2*300 + 0.5*1500},  // unknown → sonnet fallback
	}
	for _, c := range cases {
		got := rateForSpec(projectstate.WorkerRateSpec{ModelID: c.id, MegatokensInPerDay: c.in, MegatokensOutPerDay: c.out})
		if got.MinorUnits != c.want {
			t.Errorf("rateForSpec(%q) = %d¢/day, want %d¢/day", c.id, got.MinorUnits, c.want)
		}
	}
}

// askquestions_test.go — F82 coverage for the Project-Design answer-job dispatch path
// (the manager kind=8/PlanningAssumptions goes through THIS manager). Focus: a
// pm-addressed dispatch actually fires, a re-ask re-fires with a fresh key, and a submit
// fault is LOGGED LOUDLY rather than vanishing.

// recordingPipeline is a fake ConstructionPipelineAccess that records every submit and can
// be told to fail — the seam the swallowed error hid.
type recordingPipeline struct {
	specs []constructionpipeline.PipelineSpec
	keys  []fwra.IdempotencyKey
	err   error
}

func (p *recordingPipeline) SubmitConstructionPipeline(rc fwra.Context, spec constructionpipeline.PipelineSpec) (constructionpipeline.PipelineHandle, error) {
	p.specs = append(p.specs, spec)
	p.keys = append(p.keys, rc.IdempotencyKey)
	if p.err != nil {
		return constructionpipeline.PipelineHandle(""), p.err
	}
	return constructionpipeline.PipelineHandle("run-1"), nil
}

func (p *recordingPipeline) ObserveConstructionPipeline(fwra.Context, constructionpipeline.PipelineHandle) (constructionpipeline.PipelineObservation, error) {
	return constructionpipeline.PipelineObservation{}, nil
}

func (p *recordingPipeline) CancelConstructionPipeline(fwra.Context, constructionpipeline.PipelineHandle) error {
	return nil
}

func managerWith(pipe constructionpipeline.ConstructionPipelineAccess, repoOK bool) *projectDesignManager {
	return &projectDesignManager{
		pipeline: pipe,
		repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRef("acme|acme/gtdapp"), repoOK
		},
	}
}

func sampleQuestions() []projectstate.ReviewComment {
	return questionsToLedger(projectstate.ReviewAddresseePM, []AnchoredComment{
		{JSONPath: "$.assumptions[0]", Text: "Is the calendar 5 days/week?", AnchorText: "calendar"},
	})
}

// A pm-addressed dispatch actually submits a job_mode=answer run to the project repo.
func TestDispatchAnswerJob_FiresForPM(t *testing.T) {
	pipe := &recordingPipeline{}
	m := managerWith(pipe, true)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, sampleQuestions())

	if len(pipe.specs) != 1 {
		t.Fatalf("expected exactly one answer-job submit, got %d", len(pipe.specs))
	}
	spec := pipe.specs[0]
	if spec.DispatchInputs[dispatchInputJobMode] != jobModeAnswer {
		t.Fatalf("answer job must dispatch with job_mode=answer, got %q", spec.DispatchInputs[dispatchInputJobMode])
	}
	if spec.TargetRepo.Owner != "acme" || spec.TargetRepo.Name != "gtdapp" {
		t.Fatalf("answer job must target the project repo, got %+v", spec.TargetRepo)
	}
	// Thin dispatch: the pm addressee rides the command NAME (design-answer-pm), not a prompt role.
	if got := spec.DispatchInputs[dispatchInputCommand]; got != "design-answer-pm" {
		t.Fatalf("a pm-addressed answer job must dispatch command=design-answer-pm, got %q", got)
	}
}

// Re-asking re-fires the answer job with a DIFFERENT idempotency key (F82 recovery), so the
// RA does not dedup the re-dispatch away.
func TestDispatchAnswerJob_ReFiresWithUniqueKey(t *testing.T) {
	pipe := &recordingPipeline{}
	m := managerWith(pipe, true)
	qs := sampleQuestions()
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, qs)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, qs)

	if len(pipe.keys) != 2 {
		t.Fatalf("expected two submits, got %d", len(pipe.keys))
	}
	if pipe.keys[0] == pipe.keys[1] {
		t.Fatalf("re-ask must re-fire with a DIFFERENT key (else the RA dedups it away); both were %q", pipe.keys[0])
	}
}

// A submit FAULT is logged loudly (ERROR) instead of vanishing — the F82 root-cause fix.
func TestDispatchAnswerJob_LogsSubmitFailure(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	pipe := &recordingPipeline{err: fwra.New(fwra.Infrastructure, "boom")}
	m := managerWith(pipe, true)
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, sampleQuestions())

	out := buf.String()
	if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "dispatch FAILED") {
		t.Fatalf("a submit failure must be logged at ERROR; log was:\n%s", out)
	}
	if !strings.Contains(out, "boom") {
		t.Fatalf("the failure log must carry the underlying error; log was:\n%s", out)
	}
}

// A rail-less (nil pipeline/repo) server logs a WARN and does not attempt a submit.
func TestDispatchAnswerJob_RailDormantWarns(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	m := &projectDesignManager{} // no pipeline, no repo
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, sampleQuestions())
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("a dormant rail must WARN that the question will not be auto-answered; log was:\n%s", buf.String())
	}
}

// An unresolved repo logs an ERROR and does not submit.
func TestDispatchAnswerJob_RepoUnresolvedErrors(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	pipe := &recordingPipeline{}
	m := managerWith(pipe, false) // repo resolver returns ok=false
	m.dispatchAnswerJob(context.Background(), "gtdapp", KindPlanningAssumptions, "", projectstate.ReviewAddresseePM, sampleQuestions())
	if len(pipe.specs) != 0 {
		t.Fatalf("no submit must be attempted when the repo does not resolve")
	}
	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("an unresolved repo must be logged at ERROR; log was:\n%s", buf.String())
	}
}

func TestExistingQuestionRound(t *testing.T) {
	qs := sampleQuestions()
	// Seeded at round 3 in the thread.
	thread := []projectstate.ReviewComment{{
		Type: projectstate.ReviewCommentTypeQuestion, Round: 3,
		Addressee: qs[0].Addressee, Anchor: qs[0].Anchor, Text: qs[0].Text,
	}}
	if r, ok := existingQuestionRound(thread, qs); !ok || r != 3 {
		t.Fatalf("existingQuestionRound must find the prior seeding at round 3, got r=%d ok=%v", r, ok)
	}
	// A never-seeded question is not found.
	if _, ok := existingQuestionRound(nil, qs); ok {
		t.Fatal("existingQuestionRound must report not-found for an empty thread")
	}
}

func TestAnswerJobDispatchKey_Unique(t *testing.T) {
	qs := sampleQuestions()
	k1 := answerJobDispatchKey("gtdapp", KindPlanningAssumptions, "", qs)
	k2 := answerJobDispatchKey("gtdapp", KindPlanningAssumptions, "", qs)
	if k1 == k2 {
		t.Fatalf("answerJobDispatchKey must be unique per call, both were %q", k1)
	}
}

// ---- F16: envelope slimming (drop the research corpus) ----------------------

// The projectEnvelope crosses the Temporal Activity boundary on every projectdesign
// read. Phase-2 project design never reads the corpus, so a 660KB research source must
// NOT ride along toward Temporal's 2MB kill threshold. Prove the corpus neither appears
// in the encoded payload nor survives a round-trip.
func Test_encodeProject_DropsResearchCorpus(t *testing.T) {
	p := projectstate.Project{
		ID:      "gtdapp",
		Version: 3,
		Phase:   1,
		Research: projectstate.ResearchCorpus{Sources: []projectstate.ResearchSourceRef{
			{Title: "RESEARCH-SENTINEL Founder Brief", Path: ".aiarch/state/research/00-founder-brief.txt", ContentBytes: 660_000},
		}},
	}

	env, err := encodeProject(p)
	if err != nil {
		t.Fatalf("encodeProject: %v", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if strings.Contains(string(raw), "RESEARCH-SENTINEL") {
		t.Fatalf("research corpus leaked into the Temporal envelope (%d bytes)", len(raw))
	}
	if strings.Contains(string(raw), "\"research\"") {
		t.Fatal("envelope must not carry a research field at all")
	}

	back, err := env.Decode()
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !back.Research.IsZero() {
		t.Fatal("research must not survive the projectdesign envelope round-trip")
	}
}

// ---- F19: review-gate precondition -----------------------------------------

// stubEncodedStage is a minimal converter.EncodedValue whose Get sets only the Stage.
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

// fakeQueryClient scripts QueryWorkflow (stage or error) and records whether the
// reviewDecision signal fired. It embeds client.Client so unimplemented methods panic.
type fakeQueryClient struct {
	client.Client
	stage        SessionStage
	queryErr     error
	signalCalled bool
}

func (f *fakeQueryClient) QueryWorkflow(_ context.Context, _ string, _ string, _ string, _ ...any) (converter.EncodedValue, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return stubEncodedStage{stage: f.stage}, nil
}

func (f *fakeQueryClient) SignalWorkflow(_ context.Context, _ string, _ string, _ string, _ any) error {
	f.signalCalled = true
	return nil
}

// DescribeWorkflowExecution reports the session workflow as RUNNING so both GetSessionState
// and (F-R2) reviewGateView — now BOTH Describe-first — fall through to the sessionState
// query, which returns the configured stage (or queryErr). A live RUNNING run is the right
// fixture for these SubmitReviewDecision/GetSessionState tests: the NotFound cases drive it via
// queryErr, and the abnormal/completed synthesis paths have their own dedicated tests.
func (f *fakeQueryClient) DescribeWorkflowExecution(_ context.Context, _ string, _ string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING},
	}, nil
}

func Test_checkReviewPrecondition_Matrix(t *testing.T) {
	fp := func(err error) bool {
		var e *fwmanager.Error
		return err != nil && errors.As(err, &e) && e.Kind == fwmanager.FailedPrecondition
	}

	for _, st := range []SessionStage{SessionStageUnknown, StageDrafting, StageAssemblingSDP, StageRedrafting, StageCommitted, StageWithdrawn, StageRefused, StageDraftFailed} {
		if err := checkReviewPrecondition(ReviewApprove, st); !fp(err) {
			t.Fatalf("approve at stage %d must FailedPrecondition, got %v", st, err)
		}
	}
	if err := checkReviewPrecondition(ReviewApprove, StageAwaitingReview); err != nil {
		t.Fatalf("approve at AwaitingReview must pass, got %v", err)
	}
	for _, dec := range []ReviewDecision{ReviewReject, ReviewWithdraw} {
		for _, st := range []SessionStage{StageAwaitingReview, StageDraftFailed} {
			if err := checkReviewPrecondition(dec, st); err != nil {
				t.Fatalf("decision %d at stage %d must pass, got %v", dec, st, err)
			}
		}
		for _, st := range []SessionStage{SessionStageUnknown, StageDrafting, StageAssemblingSDP, StageRedrafting, StageCommitted, StageWithdrawn, StageRefused} {
			if err := checkReviewPrecondition(dec, st); !fp(err) {
				t.Fatalf("decision %d at stage %d must FailedPrecondition, got %v", dec, st, err)
			}
		}
	}
}

func Test_SubmitReviewDecision_Approve_WhileDrafting_FailsWithoutSignal(t *testing.T) {
	fc := &fakeQueryClient{stage: StageDrafting}
	m := newProjectDesignManager(fc, nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindActivityList, ReviewApprove, nil)
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.FailedPrecondition {
		t.Fatalf("approve while drafting must FailedPrecondition, got %d", got)
	}
	if fc.signalCalled {
		t.Fatal("no reviewDecision signal must fire when the precondition fails")
	}
}

func Test_SubmitReviewDecision_Approve_AtAwaitingReview_Signals(t *testing.T) {
	fc := &fakeQueryClient{stage: StageAwaitingReview}
	m := newProjectDesignManager(fc, nil, nil, nil, nil, nil, nil, nil, nil)
	if err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindActivityList, ReviewApprove, nil); err != nil {
		t.Fatalf("approve at AwaitingReview must succeed, got %v", err)
	}
	if !fc.signalCalled {
		t.Fatal("approve at AwaitingReview must fire the reviewDecision signal")
	}
}

// ---- F20: clean not-found altitude on the pre-phase session read -----------

func Test_GetSessionState_BeforePhase2_CleanNotFound(t *testing.T) {
	fc := &fakeQueryClient{queryErr: serviceerror.NewNotFound("workflow not found for ID: gtdapp:8")}
	m := newProjectDesignManager(fc, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID("gtdapp"), KindPlanningAssumptions)
	e := asProjectDesignError(t, err)
	if e.Kind != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %d", e.Kind)
	}
	if strings.Contains(e.Detail, "workflow not found") || strings.Contains(e.Detail, "gtdapp:8") {
		t.Fatalf("Temporal internals leaked to the client: %q", e.Detail)
	}
	if !strings.Contains(e.Detail, "project design has not started") {
		t.Fatalf("want a user-altitude message, got %q", e.Detail)
	}
}

// QA 2026-07-19 (poll-404 wizard reset twin): a namespace-not-found from a wrong/foreign
// Temporal backend must NOT map to the authoritative "project design has not started"
// NotFound — the polled SPA trusts that 404 and drops its session view. It stays an
// Infrastructure fault the client tolerates.
func Test_GetSessionState_NamespaceNotFound_IsInfrastructureNot404(t *testing.T) {
	id := ProjectID("gtdapp")
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	nsErr := serviceerror.NewNamespaceNotFound("default")
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), nsErr)
	mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).
		Return(nil, nsErr)

	m := &projectDesignManager{client: mc}
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions)
	e := asProjectDesignError(t, err)
	if e.Kind == fwmanager.NotFound {
		t.Fatalf("namespace-not-found (wrong Temporal backend) must not claim session absence, got NotFound %q", e.Detail)
	}
	if e.Kind != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %d (detail %q)", e.Kind, e.Detail)
	}
	mc.AssertExpectations(t)
}

// stagename_test.go — F72 stageName label for the Phase-2 manager. Phase-2's Stage enum
// values DIFFER from Phase-1's (projectdesign StageAwaitingReview == 3, systemdesign == 2), so
// the human-readable StageName label removes the cross-manager ambiguity. sessionStageLabel is
// the single authoritative map; withStageName stamps it.

func TestSessionStageLabel_Map(t *testing.T) {
	cases := map[SessionStage]string{
		SessionStageUnknown: "not started",
		StageDrafting:       "drafting",
		StageAssemblingSDP:  "assembling SDP",
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
	// projectdesign StageAwaitingReview is the int 3 (vs systemdesign's 2) — the label is the
	// portable disambiguator across the two managers' divergent enums.
	if int(StageAwaitingReview) != 3 {
		t.Fatalf("guard: expected projectdesign StageAwaitingReview == 3, got %d", int(StageAwaitingReview))
	}
	v := withStageName(SessionStateView{Stage: StageAwaitingReview})
	if v.StageName != "awaiting review" {
		t.Fatalf("withStageName StageName = %q, want %q", v.StageName, "awaiting review")
	}
	if v.Stage != StageAwaitingReview {
		t.Fatalf("withStageName must not alter the Stage int, got %d", int(v.Stage))
	}
}
