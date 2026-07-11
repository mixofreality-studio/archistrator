package projectdesign

// acknowledgestale_test.go covers the F-GTD-12 live-session ack gate: acknowledging
// staleness on a slot whose amendment session is LIVE is refused with FailedPrecondition
// (the wire's 409/"failed_precondition"), because the ack's main-branch commit would turn
// the amendment's review PR merge-DIRTY and wedge its approve.

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
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// fakeEncodedSessionView satisfies converter.EncodedValue for the mocked sessionState
// Query result.
type fakeEncodedSessionView struct{ view SessionStateView }

func (f fakeEncodedSessionView) HasValue() bool { return true }
func (f fakeEncodedSessionView) Get(valuePtr interface{}) error {
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
// substrate-support check — proving the refusal above came from the gate, not this path.
func Test_AcknowledgeStaleBasis_NoSession_PassesLivenessGate(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wfID := coAuthorWorkflowID(id, KindPlanningAssumptions)

	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "").Return(nil, errors.New("workflow not found"))

	// fakeProjectState does NOT implement StaleAckProjectStateAccess, so passing the
	// gate lands on the substrate FailedPrecondition with its distinct message.
	m := &projectDesignManager{client: mc, projectState: &fakeProjectState{}}
	err := m.AcknowledgeStaleBasis(fwmanager.Context{Context: context.Background()}, id, KindPlanningAssumptions, "unaffected")
	pde := asProjectDesignError(t, err)
	if pde.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("want the substrate FailedPrecondition, got %d (%v)", pde.Kind, err)
	}
	if !strings.Contains(err.Error(), "not supported by this substrate") {
		t.Fatalf("expected to pass the liveness gate and hit the substrate check, got %q", err.Error())
	}
	mc.AssertExpectations(t)
}

// A session that closed COMPLETED after committing is TERMINAL (the durable slot renders
// StageCommitted): the gate passes and the ack proceeds.
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
	pde := asProjectDesignError(t, err)
	if !strings.Contains(err.Error(), "not supported by this substrate") {
		t.Fatalf("a committed (terminal) session must pass the liveness gate, got %d %q", pde.Kind, err.Error())
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
