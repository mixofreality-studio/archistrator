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
	m := NewSystemDesignManager(nil, ps, nil, nil, nil, nil, "")

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
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), errors.New("workflow not found for ID: gtdapp:0"))
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
		Return(nil, errors.New("workflow not found for ID: gtdapp:0"))

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

// R4 (error altitude). getProject for an unknown project surfaces a stuttered, chain-leaking
// git error ("resourceaccess: github.GitStore.clone: repository not found: repository not
// found: Repository not found."). GetProject must map the RA NotFound to a single clean,
// project-scoped Detail while preserving the full chain on Cause for the server log.
func Test_GetProject_UnknownProject_CleanNotFound(t *testing.T) {
	id := ProjectID("gtdapp")
	// The exact shape the infra-github ClassifyGitError + fwra.Wrap chain produces.
	chain := fwra.Wrap(fwra.NotFound,
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
