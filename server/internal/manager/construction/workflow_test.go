package construction

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// =============================================================================
// constructionManager workflow unit tests over the Temporal in-memory test
// environment (testsuite.WorkflowTestSuite). The three Engines (handOffEngine,
// interventionEngine, reviewEngine) and the four ResourceAccess ports
// (projectStateAccess, constructionPipelineAccess, artifactAccess, workerAccess)
// are constructed as interface test doubles (fakes) — the not-yet-built deps are
// driven against their FROZEN CONTRACTS as the Manager-declared consumer interfaces
// (deps.go). These run with no Docker and no dev server (the real-infrastructure
// exercise is a later integration activity).
//
// They assert the UC3 spine (cast → dispatch → submit/observe → stage → review →
// recordChangeReviewed → recordActivityExited), the no-eligible-activity quiet
// tick, the pause branch (NCUC2), the operator-override branch, and the key
// error/variance/conflict paths — per [[the-method-testing]] (black-box where the
// observable is the workflow result/recorded side effects).
// =============================================================================

// ---- Fakes (interface test doubles for the downstream deps) -----------------

// fakeProjectState records the additive Phase-3 transition calls + serves a
// scripted head-state. It satisfies the Manager's ProjectStateAccess consumer
// interface (deps.go) — the read + the three additive transition verbs.
type fakeProjectState struct {
	mu sync.Mutex

	project  projectstate.Project
	notFound bool

	// conflictFirst, when >0, returns fwra.Conflict on the first N transition
	// calls (across all transition verbs) before succeeding — drives the §6.5
	// re-read→re-apply loop.
	conflictFirst int

	reviewed []string
	exited   []exitCall
	paused   []string

	version projectstate.Version
}

type exitCall struct {
	activityID string
	outcome    projectstate.ActivityOutcome
}

func (f *fakeProjectState) ReadProject(_ context.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return projectstate.Project{}, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project, nil
}

func (f *fakeProjectState) bump() projectstate.Version {
	f.version++
	f.project.Version = f.version
	return f.version
}

func (f *fakeProjectState) maybeConflict() error {
	if f.conflictFirst > 0 {
		f.conflictFirst--
		// Advance the served head version so the re-read sees a newer value.
		f.version++
		f.project.Version = f.version
		return fwra.New(fwra.Conflict, "stale version")
	}
	return nil
}

func (f *fakeProjectState) RecordChangeReviewed(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.reviewed = append(f.reviewed, activityID)
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityExited(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, outcome projectstate.ActivityOutcome, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.exited = append(f.exited, exitCall{activityID: activityID, outcome: outcome})
	return f.bump(), nil
}

func (f *fakeProjectState) RecordOperatorPaused(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, reason string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.paused = append(f.paused, reason)
	return f.bump(), nil
}

var _ ProjectStateAccess = (*fakeProjectState)(nil)

// fakeWorker is the generic typed worker double. It returns a scripted raw-JSON
// body per Generate call (the last is repeated), or genErr verbatim. badJSON, when
// true, returns un-unmarshallable bytes (drives the WorkerRefused terminal).
type fakeWorker struct {
	mu sync.Mutex

	genErr    error
	badJSON   bool
	prompts   []string
	classes   []string
	cancelled []fwra.IdempotencyKey
}

func (w *fakeWorker) Generate(_ context.Context, spec workerGenerateSpec, _ fwra.IdempotencyKey) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prompts = append(w.prompts, spec.Prompt)
	w.classes = append(w.classes, spec.WorkerClass)
	if w.genErr != nil {
		return nil, w.genErr
	}
	if w.badJSON {
		return []byte("not json"), nil
	}
	b, _ := json.Marshal(artifact.ConstructionOutput{Bytes: []byte("built"), MIMEType: "text/plain"})
	return b, nil
}

func (w *fakeWorker) Cancel(_ context.Context, key fwra.IdempotencyKey) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cancelled = append(w.cancelled, key)
	return nil
}

var _ WorkerAccess = (*fakeWorker)(nil)

// fakePipeline serves a scripted terminal observation after one running poll.
type fakePipeline struct {
	mu sync.Mutex

	phase     PipelinePhase // terminal phase to serve
	diag      string
	submitted []PipelineSpec
	cancelled []PipelineHandle
	polls     int
}

func (p *fakePipeline) SubmitConstructionPipeline(_ context.Context, spec PipelineSpec, _ fwra.IdempotencyKey) (PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	return PipelineHandle{Name: "wf-" + spec.ActivityID}, nil
}

func (p *fakePipeline) ObserveConstructionPipeline(_ context.Context, _ PipelineHandle) (PipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	ph := p.phase
	if ph == PipelinePhaseUnknown {
		ph = PipelineSucceeded
	}
	return PipelineObservation{Phase: ph, Diagnostic: p.diag}, nil
}

func (p *fakePipeline) CancelConstructionPipeline(_ context.Context, handle PipelineHandle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, handle)
	return nil
}

var _ ConstructionPipelineAccess = (*fakePipeline)(nil)

// fakeArtifacts records stored outputs and returns a deterministic address.
type fakeArtifacts struct {
	mu     sync.Mutex
	stored []artifact.ConstructionOutput
}

func (a *fakeArtifacts) StoreConstructionOutput(_ context.Context, output artifact.ConstructionOutput, _ fwra.IdempotencyKey) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stored = append(a.stored, output)
	return "addr-1", nil
}

func (a *fakeArtifacts) RetrieveConstructionOutput(_ context.Context, _ string) (artifact.ConstructionOutput, error) {
	return artifact.ConstructionOutput{}, nil
}

var _ ArtifactAccess = (*fakeArtifacts)(nil)

// fakeHandOff returns a scripted worker class.
type fakeHandOff struct {
	class WorkerClass
	err   error
}

func (h *fakeHandOff) PickWorkerClass(_ ConstructionActivity, _ HandOffPolicy) (WorkerClass, error) {
	if h.err != nil {
		return WorkerClassUnknown, h.err
	}
	if h.class == WorkerClassUnknown {
		return AIWorker, nil
	}
	return h.class, nil
}

var _ HandOffEngine = (*fakeHandOff)(nil)

// fakeIntervention returns scripted directives/plans.
type fakeIntervention struct {
	directive VarianceDirective
	plan      PausePlan
}

func (i *fakeIntervention) DecideOnVariance(_ ConstructionVariance) (VarianceDirective, error) {
	if i.directive == DirectiveUnknown {
		return DirectiveEscalate, nil
	}
	return i.directive, nil
}

func (i *fakeIntervention) ApplyPausePolicy(_ string, _ PauseRequestContext) (PausePlan, error) {
	return i.plan, nil
}

var _ InterventionEngine = (*fakeIntervention)(nil)

// fakeReview returns a scripted reviewer set.
type fakeReview struct {
	set ReviewSet
}

func (r *fakeReview) ProposeReviews(_ ReviewChange, _ string, _ string, _ string, _ []string) (ReviewSet, error) {
	return r.set, nil
}

var _ ReviewEngine = (*fakeReview)(nil)

// ---- helpers ----------------------------------------------------------------

// registerConstruct registers the per-activity child workflow + its activities.
func registerConstruct(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: ExecutionKindConstructActivity})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.GenerateWorkActivity)
	env.RegisterActivity(wf.CancelWorkerActivity)
	env.RegisterActivity(wf.SubmitPipelineActivity)
	env.RegisterActivity(wf.ObservePipelineActivity)
	env.RegisterActivity(wf.CancelPipelineActivity)
	env.RegisterActivity(wf.StoreConstructionOutputActivity)
	env.RegisterActivity(wf.RecordChangeReviewedActivity)
	env.RegisterActivity(wf.RecordActivityExitedActivity)
	env.RegisterActivity(wf.RecordOperatorPausedActivity)
}

func registerPump(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.PumpNextActivityWorkflow, workflow.RegisterOptions{Name: ExecutionKindPump})
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: ExecutionKindConstructActivity})
	// The pump now waits for child COMPLETION (self-cascade), so the per-activity
	// child runs end-to-end and ALL its activities must be registered.
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.GenerateWorkActivity)
	env.RegisterActivity(wf.CancelWorkerActivity)
	env.RegisterActivity(wf.SubmitPipelineActivity)
	env.RegisterActivity(wf.ObservePipelineActivity)
	env.RegisterActivity(wf.CancelPipelineActivity)
	env.RegisterActivity(wf.StoreConstructionOutputActivity)
	env.RegisterActivity(wf.RecordChangeReviewedActivity)
	env.RegisterActivity(wf.RecordActivityExitedActivity)
	env.RegisterActivity(wf.RecordOperatorPausedActivity)
}

func registerSupervision(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.ProjectSupervisionWorkflow, workflow.RegisterOptions{Name: ExecutionKindProjectSupervision})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.CancelWorkerActivity)
	env.RegisterActivity(wf.CancelPipelineActivity)
	env.RegisterActivity(wf.RecordOperatorPausedActivity)
}

func registerReplanSweep(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.ReplanSweepWorkflow, workflow.RegisterOptions{Name: ExecutionKindReplanSweep})
	env.RegisterActivity(wf.ReadProjectActivity)
}

func sampleActivity() ConstructionActivity {
	return ConstructionActivity{ActivityID: "C-XYZ", Kind: ActivityKindConstruction, ComponentID: "comp-1", Layer: "engine"}
}

// ---- Tests: per-activity spine (ConstructActivityWorkflow) ------------------

// The happy-path UC3 spine: cast → dispatch → submit/observe(succeeded) → stage →
// review(empty set) → recordChangeReviewed → recordActivityExited(Completed).
func Test_Construct_HappyPath_RecordsReviewedAndExited(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	art := &fakeArtifacts{}
	w := &fakeWorker{}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: pipe, Artifacts: art, Workers: w,
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: ps.project.ID, ActivityID: "C-XYZ", Activity: sampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.reviewed) != 1 || ps.reviewed[0] != "C-XYZ" {
		t.Fatalf("want one recordChangeReviewed(C-XYZ), got %v", ps.reviewed)
	}
	if len(ps.exited) != 1 || ps.exited[0].activityID != "C-XYZ" || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want one recordActivityExited(C-XYZ, Completed), got %v", ps.exited)
	}
	if len(pipe.submitted) != 1 {
		t.Fatalf("want one pipeline submit, got %d", len(pipe.submitted))
	}
	if len(art.stored) != 1 {
		t.Fatalf("want one staged output, got %d", len(art.stored))
	}
}

// The review fan-out: a reviewer set of two reviewers triggers two extra worker
// dispatches (one per reviewer) on top of the construction dispatch; a mayAmend
// reviewer re-stages an amended output.
func Test_Construct_ReviewFanOut_DispatchesPerReviewer_AndReStagesMayAmend(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	art := &fakeArtifacts{}
	w := &fakeWorker{}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review: &fakeReview{set: ReviewSet{Reviewers: []Reviewer{
			{Role: "senior", Perspective: "contract", MayAmend: true},
			{Role: "security", Perspective: "owasp", MayAmend: false},
		}}},
		ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded}, Artifacts: art, Workers: w,
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: ps.project.ID, ActivityID: "C-XYZ", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// 1 construction dispatch + 2 reviewer dispatches = 3 Generate calls.
	if len(w.prompts) != 3 {
		t.Fatalf("want 3 worker dispatches (1 construct + 2 reviewers), got %d", len(w.prompts))
	}
	// 1 construction stage + 1 mayAmend re-stage = 2 stored outputs.
	if len(art.stored) != 2 {
		t.Fatalf("want 2 staged outputs (construct + mayAmend re-stage), got %d", len(art.stored))
	}
}

// ArchitectOnly skips dispatch + pipeline and awaits an operator override; a Skip
// override exits the activity with the operator-skip outcome and no worker dispatch.
func Test_Construct_ArchitectOnly_AwaitsOverride_SkipExits(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	w := &fakeWorker{}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: ArchitectOnly}, Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: w,
	})
	registerConstruct(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalOperatorOverride, OperatorOverrideSignal{Override: ActivityOverride{Kind: OverrideSkip}})
	}, time.Millisecond)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: ps.project.ID, ActivityID: "C-ARCH", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(w.prompts) != 0 {
		t.Fatalf("ArchitectOnly must NOT dispatch a worker, got %d dispatches", len(w.prompts))
	}
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeSkipped {
		t.Fatalf("want one recordActivityExited(Skipped), got %v", ps.exited)
	}
}

// A worker that produces unconstructable output → WorkerRefused → variance →
// interventionEngine.DecideOnVariance. With directive Retry the supervision loops;
// the bad worker keeps failing until maxVarianceAttempts, ending in a terminal
// VarianceExhausted error (nothing recorded reviewed/exited).
func Test_Construct_WorkerRefused_RoutesToIntervention_RetryLoopExhausts(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded},
		Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{badJSON: true},
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: ps.project.ID, ActivityID: "C-BAD", Activity: sampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if env.GetWorkflowError() == nil {
		t.Fatal("expected a terminal error after the retry loop exhausts")
	}
	if len(ps.reviewed) != 0 || len(ps.exited) != 0 {
		t.Fatalf("a refused-worker activity must not record reviewed/exited, got reviewed=%v exited=%v", ps.reviewed, ps.exited)
	}
}

// A failed pipeline → variance → DecideOnVariance(Takeover): the takeover cancels
// the in-flight worker, then re-dispatches; with the pipeline now succeeding the
// activity completes normally on the next loop.
func Test_Construct_PipelineFailed_Takeover_CancelsWorker_ThenCompletes(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	// The pipeline fails on the first run, then a flippable fake makes it succeed.
	pipe := &flippablePipeline{first: PipelineFailed, rest: PipelineSucceeded}
	w := &fakeWorker{}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{directive: DirectiveTakeover},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: pipe, Artifacts: &fakeArtifacts{}, Workers: w,
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: ps.project.ID, ActivityID: "C-PF", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(w.cancelled) == 0 {
		t.Fatal("takeover must cancel the in-flight worker")
	}
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want a completed exit after takeover+re-dispatch, got %v", ps.exited)
	}
}

// flippablePipeline serves `first` on the first terminal observation, then `rest`.
type flippablePipeline struct {
	mu        sync.Mutex
	first     PipelinePhase
	rest      PipelinePhase
	submits   int
	cancelled []PipelineHandle
}

func (p *flippablePipeline) SubmitConstructionPipeline(_ context.Context, spec PipelineSpec, _ fwra.IdempotencyKey) (PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submits++
	return PipelineHandle{Name: "wf"}, nil
}

func (p *flippablePipeline) ObserveConstructionPipeline(_ context.Context, _ PipelineHandle) (PipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.submits <= 1 {
		return PipelineObservation{Phase: p.first, Diagnostic: "boom"}, nil
	}
	return PipelineObservation{Phase: p.rest}, nil
}

func (p *flippablePipeline) CancelConstructionPipeline(_ context.Context, handle PipelineHandle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, handle)
	return nil
}

var _ ConstructionPipelineAccess = (*flippablePipeline)(nil)

// The §6.5 Conflict discipline: a recordChangeReviewed that returns fwra.Conflict
// twice before succeeding drives the workflow-level re-read→re-apply loop; the
// activity still completes (reviewed + exited recorded).
func Test_Construct_ConflictOnRecord_ReReadReApply_Succeeds(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: ProjectID(uuid.NewString()), Version: 1, Phase: 2}, conflictFirst: 2}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded},
		Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: ps.project.ID, ActivityID: "C-CONF", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.reviewed) != 1 {
		t.Fatalf("conflict loop must converge to exactly one recorded reviewed, got %v", ps.reviewed)
	}
	if len(ps.exited) != 1 {
		t.Fatalf("want one recorded exit after the conflict loop, got %v", ps.exited)
	}
}

// ---- Tests: pump (PumpNextActivityWorkflow) ---------------------------------

// No eligible activity ⇒ PumpResult{Dispatched:false} — a normal quiet tick, not
// an error (no NextEligibleActivity helper wired).
func Test_Pump_NoEligibleActivity_QuietTick(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{}, Intervention: &fakeIntervention{}, Review: &fakeReview{},
		ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		NextEligibleActivity: nil,
	})
	registerPump(env, wf)

	env.ExecuteWorkflow(ExecutionKindPump, PumpInput{ProjectID: ps.project.ID})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump error: %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("want Dispatched:false on a quiet tick, got %+v", res)
	}
}

// A brand-new project (ReadProject NotFound) is also a quiet tick, not an error.
func Test_Pump_ProjectNotFound_QuietTick(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{notFound: true}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{}, Intervention: &fakeIntervention{}, Review: &fakeReview{},
		ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
	})
	registerPump(env, wf)

	env.ExecuteWorkflow(ExecutionKindPump, PumpInput{ProjectID: ProjectID(uuid.NewString())})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump error: %v", err)
	}
	var res PumpResult
	_ = env.GetWorkflowResult(&res)
	if res.Dispatched {
		t.Fatal("a not-found project must be a quiet tick")
	}
}

// An eligible activity ⇒ the pump runs the per-activity child to COMPLETION, then
// SELF-CASCADES via ContinueAsNew (Task 3). The test env surfaces ContinueAsNew as a
// *workflow.ContinueAsNewError carrying the next PumpInput. The child's spine ran
// end-to-end (one reviewed + one completed exit recorded).
func Test_Pump_EligibleActivity_RunsChild_ThenContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded},
		Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		NextEligibleActivity: func(_ projectstate.Project) (ConstructionActivity, bool) {
			return sampleActivity(), true
		},
	})
	registerPump(env, wf)

	env.ExecuteWorkflow(ExecutionKindPump, PumpInput{ProjectID: pid})

	if !env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	// A successful eligible dispatch self-cascades: the terminal "error" is a
	// ContinueAsNewError carrying the next tick's PumpInput (NOT a real failure).
	err := env.GetWorkflowError()
	var canErr *workflow.ContinueAsNewError
	if !errors.As(err, &canErr) {
		t.Fatalf("want a ContinueAsNewError (self-cascade), got %v", err)
	}
	// The child ran end-to-end exactly once.
	if len(ps.exited) != 1 || ps.exited[0].activityID != "C-XYZ" {
		t.Fatalf("want the child to have recorded one exit for C-XYZ, got %v", ps.exited)
	}
}

// A drained network (nextEligible returns false) ⇒ the pump goes QUIET WITHOUT
// ContinueAsNew (the cascade ends) — Dispatched:false, no error.
func Test_Pump_DrainedNetwork_QuietNoContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{}, Review: &fakeReview{},
		ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		NextEligibleActivity: func(_ projectstate.Project) (ConstructionActivity, bool) {
			return ConstructionActivity{}, false // network drained
		},
	})
	registerPump(env, wf)

	env.ExecuteWorkflow(ExecutionKindPump, PumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("drained pump must be a clean quiet tick, got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("a drained network must go quiet (Dispatched:false), got %+v", res)
	}
}

// A pause Signal delivered to the (cascading) pump halts it BEFORE any dispatch: the
// pump goes quiet WITHOUT ContinueAsNew and WITHOUT starting a child, even though an
// activity is eligible. The resume path re-triggers the pump.
func Test_Pump_PauseSignal_HaltsCascade_NoDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded},
		Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		NextEligibleActivity: func(_ projectstate.Project) (ConstructionActivity, bool) {
			return sampleActivity(), true // an activity IS eligible — but the pause wins
		},
	})
	registerPump(env, wf)

	// Deliver the pause Signal so it is already queued when the pump checks (the pump's
	// non-blocking ReceiveAsync observes it at the top, before any dispatch).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalOperatorPauseRequested, OperatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, 0)

	env.ExecuteWorkflow(ExecutionKindPump, PumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("paused pump must be a clean quiet tick (no ContinueAsNew), got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("a paused pump must NOT dispatch, got %+v", res)
	}
	// The child's spine never ran: nothing recorded exited.
	if len(ps.exited) != 0 {
		t.Fatalf("a paused pump must not run any activity, got exits %v", ps.exited)
	}
}

// ---- Tests: pause branch (ProjectSupervisionWorkflow / NCUC2) ---------------

// The operator-pause branch: applyPausePolicy returns a plan naming a pipeline to
// cancel + RecordPaused; the Manager EXECUTES the cancel + worker-cancel +
// recordOperatorPaused.
func Test_Pause_AppliesPolicy_CancelsPipeline_RecordsPaused(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	w := &fakeWorker{}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{}, Review: &fakeReview{},
		Intervention: &fakeIntervention{plan: PausePlan{PipelinesToCancel: []string{"wf-C-1"}, RecordPaused: true}},
		ProjectState: ps, Pipeline: pipe, Artifacts: &fakeArtifacts{}, Workers: w,
	})
	registerSupervision(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalOperatorPauseRequested, OperatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)

	env.ExecuteWorkflow(ExecutionKindProjectSupervision, ProjectSupervisionInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("supervision error: %v", err)
	}
	if len(pipe.cancelled) != 1 {
		t.Fatalf("want one pipeline cancel from the pause plan, got %d", len(pipe.cancelled))
	}
	if len(w.cancelled) != 1 {
		t.Fatalf("want one worker cancel on the pause abandon path, got %d", len(w.cancelled))
	}
	if len(ps.paused) != 1 || ps.paused[0] != "operator halt" {
		t.Fatalf("want one recordOperatorPaused(operator halt), got %v", ps.paused)
	}
}

// ---- Tests: replan sweep (ReplanSweepWorkflow) ------------------------------

// A quiet sweep returns an empty result (no auto-replan).
func Test_ReplanSweep_QuietSweep_EmptyResult(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	wf := newWorkflows(Deps{
		HandOff: &fakeHandOff{}, Intervention: &fakeIntervention{}, Review: &fakeReview{},
		ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
	})
	registerReplanSweep(env, wf)

	env.ExecuteWorkflow(ExecutionKindReplanSweep, ReplanSweepInput{ProjectID: &pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("sweep error: %v", err)
	}
	var res ReplanSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode sweep result: %v", err)
	}
	if len(res.FlaggedVariances) != 0 {
		t.Fatalf("want an empty quiet sweep, got %v", res.FlaggedVariances)
	}
}

// ---- worker-output adapter unit test (no Temporal) --------------------------

// generateConstructionOutput round-trips a JSON ConstructionOutput; a bad body is
// a *workerUnmarshalError (distinct from a transport error); a nil (cancelled)
// body is the zero value with nil error.
func Test_GenerateConstructionOutput_Cases(t *testing.T) {
	good := &fakeWorker{}
	out, err := generateConstructionOutput(context.Background(), good, workerGenerateSpec{}, "k")
	if err != nil {
		t.Fatalf("good worker: %v", err)
	}
	if string(out.Bytes) != "built" {
		t.Fatalf("want built bytes, got %q", out.Bytes)
	}

	bad := &fakeWorker{badJSON: true}
	_, err = generateConstructionOutput(context.Background(), bad, workerGenerateSpec{}, "k")
	var ue *workerUnmarshalError
	if !errors.As(err, &ue) {
		t.Fatalf("want *workerUnmarshalError for bad JSON, got %T: %v", err, err)
	}

	cancelled := &cancelledWorker{}
	out, err = generateConstructionOutput(context.Background(), cancelled, workerGenerateSpec{}, "k")
	if err != nil {
		t.Fatalf("cancelled worker: %v", err)
	}
	if len(out.Bytes) != 0 {
		t.Fatalf("cancelled (nil) response must be the zero ConstructionOutput, got %q", out.Bytes)
	}
}

// cancelledWorker returns nil bytes + nil error (the Cancel-then-Generate replay).
type cancelledWorker struct{}

func (cancelledWorker) Generate(context.Context, workerGenerateSpec, fwra.IdempotencyKey) ([]byte, error) {
	return nil, nil
}
func (cancelledWorker) Cancel(context.Context, fwra.IdempotencyKey) error { return nil }

var _ WorkerAccess = cancelledWorker{}
