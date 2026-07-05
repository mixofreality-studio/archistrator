package systemdesign

import (
	"errors"
	"fmt"
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

// ---- F22: read-model research slimming -------------------------------------

// The project read (GetProject → ProjectState) must carry research source TITLES and
// the per-source content byte-size, but NOT the corpus content itself — a source can be
// a whole 660KB book and the SPA never renders it. researchToContract is the single
// mapping seam; prove it empties Content and surfaces ContentBytes.
func Test_researchToContract_SlimsContentKeepsTitleAndBytes(t *testing.T) {
	book := strings.Repeat("x", 660_000)
	in := projectstate.ResearchInput{Sources: []projectstate.ResearchSource{
		{Title: "The Founder Brief", Content: book},
		{Title: "Competitor Analysis", Content: "short note"},
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
			t.Fatalf("source %d content must be emptied on the read model, got %d bytes", i, len(s.Content))
		}
		if s.ContentBytes == nil {
			t.Fatalf("source %d must carry ContentBytes so the UI can show what is loaded", i)
		}
	}
	if got := *out.Sources[0].ContentBytes; got != int64(len(book)) {
		t.Fatalf("ContentBytes must equal the source content size, want %d got %d", len(book), got)
	}
	if got := *out.Sources[1].ContentBytes; got != int64(len("short note")) {
		t.Fatalf("ContentBytes[1] want %d got %d", len("short note"), got)
	}
}

// ---- F19: review-gate precondition -----------------------------------------

// stubEncodedStage is a minimal converter.EncodedValue whose Get sets only the Stage,
// letting a test script the sessionState query without a live workflow.
type stubEncodedStage struct{ stage SessionStage }

func (s stubEncodedStage) HasValue() bool { return true }

func (s stubEncodedStage) Get(ptr interface{}) error {
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
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), fmt.Errorf("workflow not found for ID: %s", wfID))
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
