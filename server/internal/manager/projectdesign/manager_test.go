package projectdesign

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"go.temporal.io/sdk/client"
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
	_, err := m.AdvanceToConstruction(fwmanager.Context{Context: context.Background()}, ProjectID(""))
	if got := asProjectDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %d", got)
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
