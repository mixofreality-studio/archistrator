package projectdesign

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/settlement"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

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

func (f *fakeProjectState) CreateProject(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.OwnerScope, _ string) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) ListProjects(_ fwra.Context, _ projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	return nil, nil
}

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

func (p *fakePipeline) SubmitConstructionPipeline(_ context.Context, spec pipelineSpec, key fwra.IdempotencyKey) (pipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := len(p.submits)
	p.submits = append(p.submits, submitRecord{
		projectID:      spec.ProjectID,
		idempotencyKey: key,
		dispatchInputs: spec.DispatchInputs,
		targetRepo:     spec.TargetRepo,
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
	return pipelineHandle{Name: name}, nil
}

func (p *fakePipeline) ObserveConstructionPipeline(_ context.Context, handle pipelineHandle) (pipelineObservation, error) {
	p.mu.Lock()
	phase := p.handlePhase[handle.Name]
	diag := p.diagnostic
	p.mu.Unlock()
	obs := pipelineObservation{Phase: phase}
	if phase == pipelineFailed || phase == pipelineCancelled {
		obs.Diagnostic = diag
	}
	return obs, nil
}

var _ constructionPipelineAccess = (*fakePipeline)(nil)

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
	mkSol := func(kind projectstate.ArtifactKind, cap int) *projectstate.Solution {
		return &projectstate.Solution{SlotKind: kind, StaffingCap: cap, CalendarDaysPerWeek: 5, ClassRates: rates}
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

func newWorkflows(ps projectstate.ProjectStateAccess) *workflows {
	return &workflows{
		Estimation:   estimation.NewEstimationEngine(),
		OperationEst: operationestimation.NewOperationEstimationEngine(),
		Settlement:   settlement.NewSettlementEngine(),
		ProjectState: ps,
	}
}

// newCoAuthorWorkflows builds a workflows wired with a fake pipeline for the agentic
// co-author draft path.
func newCoAuthorWorkflows(ps projectstate.ProjectStateAccess, pipe *fakePipeline) *workflows {
	wf := newWorkflows(ps)
	wf.Pipeline = pipe
	return wf
}

// registerName builds the workflow.RegisterOptions naming a registered workflow.
func registerName(name string) workflow.RegisterOptions {
	return workflow.RegisterOptions{Name: name}
}

// registerCoAuthor registers the per-artifact gate workflow + its activities on the
// test env, exactly as RegisterWorker does in production (same stable names).
func registerCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorPhase2ArtifactWorkflow, registerName(executionKindCoAuthor))
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.DispatchDesignJobActivity)
	env.RegisterActivity(wf.ObserveDesignJobActivity)
	env.RegisterActivity(wf.StageArtifactForReviewActivity)
	env.RegisterActivity(wf.CommitArtifactActivity)
	env.RegisterActivity(wf.RejectArtifactActivity)
	env.RegisterActivity(wf.WithdrawArtifactActivity)
}

// ---- Pure unit test of the deterministic assembly + engine-join helper ------

func Test_assembleSdpReview_FourRows_Deterministic(t *testing.T) {
	id := ProjectID(uuid.NewString())
	wf := newWorkflows(nil)
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
	wf := newWorkflows(nil)
	proj := sdpReadyProject(projectstate.ProjectID(id))
	proj.Network = projectstate.ArtifactSlot{} // drop a prerequisite

	if _, err := wf.assembleSdpReview(proj, ""); err == nil {
		t.Fatalf("want an error for a missing Network prerequisite, got nil")
	}
}

// ---- CoAuthorPhase2ArtifactWorkflow: dispatch → observe → read-back ----------

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
	wf := newCoAuthorWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

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
	sub := pipe.submits[0]
	if sub.projectID != id {
		t.Fatalf("dispatch carried wrong ProjectID: %q", sub.projectID)
	}
	if sub.dispatchInputs[dispatchInputArtifactKind] != projectstate.KindPlanningAssumptions.String() {
		t.Fatalf("dispatch artifact_kind = %q, want %s", sub.dispatchInputs[dispatchInputArtifactKind], projectstate.KindPlanningAssumptions)
	}
	if sub.dispatchInputs[dispatchInputTargetBranch] == "" {
		t.Fatal("dispatch must carry a non-empty target_branch")
	}
	if sub.dispatchInputs[dispatchInputDesignPrompt] == "" {
		t.Fatal("dispatch must carry the composed design_prompt")
	}
	// The Manager MUST NOT set idempotency_token in DispatchInputs (RA-controlled).
	if _, present := sub.dispatchInputs["idempotency_token"]; present {
		t.Fatal("the Manager must NOT set idempotency_token in DispatchInputs (RA-controlled)")
	}
	if sub.idempotencyKey.IsZero() {
		t.Fatal("the dispatch Activity must supply a non-empty idempotency key")
	}
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
	wf := newCoAuthorWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

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
	wf := newCoAuthorWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

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
	wf := newCoAuthorWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

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
	wf := newCoAuthorWorkflows(ps, pipe)
	registerCoAuthor(env, wf)

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

// ---- AssembleSDPReviewWorkflow: the SDP/human gate + in-workflow Engine join -

// The SDP-review workflow still ASSEMBLES the four options and runs the three
// estimation Engine joins IN-WORKFLOW (those Engines did NOT move to the Action), then
// stages the assembled SdpReview and suspends on the sdpDecision gate. Commit binds
// the chosen option. This proves the human gate is UNCHANGED and the estimation
// Engines still run in-process.
func Test_AssembleSDPReviewWorkflow_Commit_HappyPath_EnginesRunInProcess(t *testing.T) {
	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: sdpReadyProject(projectstate.ProjectID(id))}
	wf := newWorkflows(ps) // REAL three estimate Engines; NO pipeline needed (no dispatch)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.AssembleSDPReviewWorkflow, registerName(executionKindSDPReview))
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.StageArtifactForReviewActivity)
	env.RegisterActivity(wf.CommitArtifactActivity)
	env.RegisterActivity(wf.RejectArtifactActivity)

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

func Test_AssembleSDPReviewWorkflow_RejectAll_ReassemblesThenCommits(t *testing.T) {
	id := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: sdpReadyProject(projectstate.ProjectID(id))}
	wf := newWorkflows(ps)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.AssembleSDPReviewWorkflow, registerName(executionKindSDPReview))
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.StageArtifactForReviewActivity)
	env.RegisterActivity(wf.CommitArtifactActivity)
	env.RegisterActivity(wf.RejectArtifactActivity)

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
	wf := newWorkflows(ps)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.Phase2AdvanceWorkflow, registerName(executionKindPhaseAdvance))
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.AdvancePhaseActivity)

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
	wf := newWorkflows(ps)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(wf.Phase2AdvanceWorkflow, registerName(executionKindPhaseAdvance))
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.AdvancePhaseActivity)

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
