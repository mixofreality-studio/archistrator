package projectdesign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

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

func (s stubEncodedStage) Get(ptr interface{}) error {
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

func (f *fakeQueryClient) QueryWorkflow(_ context.Context, _ string, _ string, _ string, _ ...interface{}) (converter.EncodedValue, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return stubEncodedStage{stage: f.stage}, nil
}

func (f *fakeQueryClient) SignalWorkflow(_ context.Context, _ string, _ string, _ string, _ interface{}) error {
	f.signalCalled = true
	return nil
}

// DescribeWorkflowExecution reports the session workflow as ABSENT so GetSessionState's
// Describe-first defense (F15/F28 + P0-2) falls through: a not-found execution maps to the
// clean "project design has not started" NotFound (F20), and the SubmitReviewDecision tests
// (which drive reviewGateView, not GetSessionState) never reach this method.
func (f *fakeQueryClient) DescribeWorkflowExecution(_ context.Context, _ string, _ string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return nil, fmt.Errorf("workflow not found")
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
	m := newProjectDesignManager(fc, nil, nil, nil, nil, nil, nil, nil)
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
	m := newProjectDesignManager(fc, nil, nil, nil, nil, nil, nil, nil)
	if err := m.SubmitReviewDecision(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), KindActivityList, ReviewApprove, nil); err != nil {
		t.Fatalf("approve at AwaitingReview must succeed, got %v", err)
	}
	if !fc.signalCalled {
		t.Fatal("approve at AwaitingReview must fire the reviewDecision signal")
	}
}

// ---- F20: clean not-found altitude on the pre-phase session read -----------

func Test_GetSessionState_BeforePhase2_CleanNotFound(t *testing.T) {
	fc := &fakeQueryClient{queryErr: fmt.Errorf("workflow not found for ID: gtdapp:8")}
	m := newProjectDesignManager(fc, nil, nil, nil, nil, nil, nil, nil)
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
