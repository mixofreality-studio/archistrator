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

	reviewed  []string
	exited    []exitCall
	failed    []failCall
	paused    []string
	phaseDone []phaseCompletedCall

	version projectstate.Version
}

// phaseCompletedCall records one RecordPhaseCompleted transition (the gate's durable
// per-phase completion record). The gate tests assert on it via phaseCompleted.
type phaseCompletedCall struct {
	activityID string
	phase      string
}

// phaseCompleted reports whether RecordPhaseCompleted landed for (activityID, phase).
func (f *fakeProjectState) phaseCompleted(activityID, phase string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.phaseDone {
		if c.activityID == activityID && c.phase == phase {
			return true
		}
	}
	return false
}

type exitCall struct {
	activityID string
	outcome    projectstate.ActivityOutcome
}

type failCall struct {
	activityID string
	reason     projectstate.FailureReason
	detail     string
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

func (f *fakeProjectState) RecordChangeReviewed(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.reviewed = append(f.reviewed, activityID)
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityExited(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, outcome projectstate.ActivityOutcome, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.exited = append(f.exited, exitCall{activityID: activityID, outcome: outcome})
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityFailed(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, reason projectstate.FailureReason, detail string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.failed = append(f.failed, failCall{activityID: activityID, reason: reason, detail: detail})
	return f.bump(), nil
}

func (f *fakeProjectState) RecordOperatorPaused(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, reason string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.paused = append(f.paused, reason)
	return f.bump(), nil
}

func (f *fakeProjectState) RecordReviewPolicy(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ReviewPolicy, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseStarted(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ActivityMethodPhase, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseCompleted(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.phaseDone = append(f.phaseDone, phaseCompletedCall{activityID: activityID, phase: phase.String()})
	return f.bump(), nil
}

// ---- gitActivityStatusAccess seam (per-activity construction head-state) ----
// The gate tests wire GitStatus so gitOn is true (the LOCAL/dry-run profile — no PR
// rail), which is what drives the phase-started/completed head-state records. These
// are no-op bumps; only RecordPhaseCompleted (a construction-transition verb, above)
// carries the assertion the gate tests read.

func (f *fakeProjectState) RecordActivityBranchOpened(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _, _, _, _, _ string, _ bool, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityCIObserved(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.CICheckState, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityArchApproved(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityMerged(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityStarted(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityCompleted(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordServiceContractProduced(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ServiceContract, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseArtifactProduced(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ string, _ projectstate.PhaseArtifactPayload, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

var _ projectStateReader = (*fakeProjectState)(nil)
var _ constructionTransitionAccess = (*fakeProjectState)(nil)
var _ gitActivityStatusAccess = (*fakeProjectState)(nil)

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

var _ workerAccess = (*fakeWorker)(nil)

// fakePipeline serves a scripted terminal observation after one running poll.
type fakePipeline struct {
	mu sync.Mutex

	phase     PipelinePhase // terminal phase to serve
	diag      string
	submitted []pipelineSpec
	cancelled []pipelineHandle
	polls     int
}

func (p *fakePipeline) SubmitConstructionPipeline(_ context.Context, spec pipelineSpec, _ fwra.IdempotencyKey) (pipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	return pipelineHandle{Name: "wf-" + spec.ActivityID}, nil
}

func (p *fakePipeline) ObserveConstructionPipeline(_ context.Context, _ pipelineHandle) (pipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	ph := p.phase
	if ph == PipelinePhaseUnknown {
		ph = PipelineSucceeded
	}
	return pipelineObservation{Phase: ph, Diagnostic: p.diag}, nil
}

func (p *fakePipeline) CancelConstructionPipeline(_ context.Context, handle pipelineHandle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, handle)
	return nil
}

var _ constructionPipelineAccess = (*fakePipeline)(nil)

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

var _ artifactAccess = (*fakeArtifacts)(nil)

// fakeHandOff returns a scripted worker class.
type fakeHandOff struct {
	class workerClass
	err   error
}

func (h *fakeHandOff) PickWorkerClass(_ constructionActivity, _ handOffPolicy) (workerClass, error) {
	if h.err != nil {
		return workerClassUnknown, h.err
	}
	if h.class == workerClassUnknown {
		return aiWorker, nil
	}
	return h.class, nil
}

var _ handOffEngine = (*fakeHandOff)(nil)

// fakeIntervention returns scripted directives/plans.
type fakeIntervention struct {
	directive varianceDirective
	plan      pausePlan
}

func (i *fakeIntervention) DecideOnVariance(_ constructionVariance) (varianceDirective, error) {
	if i.directive == directiveUnknown {
		return directiveEscalate, nil
	}
	return i.directive, nil
}

func (i *fakeIntervention) ApplyPausePolicy(_ string, _ pauseRequestContext) (pausePlan, error) {
	return i.plan, nil
}

var _ interventionEngine = (*fakeIntervention)(nil)

// fakeReview returns a scripted reviewer set.
type fakeReview struct {
	set ReviewSet
}

func (r *fakeReview) ProposeReviews(_ reviewChange, _ string, _ string, _ string, _ []string) (ReviewSet, error) {
	return r.set, nil
}

var _ reviewEngine = (*fakeReview)(nil)

// ---- helpers ----------------------------------------------------------------

// registerConstruct registers the per-activity child workflow + its activities.
func registerConstruct(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.GenerateWorkActivity)
	env.RegisterActivity(wf.CancelWorkerActivity)
	env.RegisterActivity(wf.SubmitPipelineActivity)
	env.RegisterActivity(wf.ObservePipelineActivity)
	env.RegisterActivity(wf.CancelPipelineActivity)
	env.RegisterActivity(wf.StoreConstructionOutputActivity)
	env.RegisterActivity(wf.RecordChangeReviewedActivity)
	env.RegisterActivity(wf.RecordActivityExitedActivity)
	env.RegisterActivity(wf.RecordActivityFailedActivity)
	env.RegisterActivity(wf.RecordOperatorPausedActivity)
	// Phase-gate + per-activity construction-status records (fire only when gitOn;
	// the gate tests wire GitStatus so these must be registered).
	env.RegisterActivity(wf.RecordActivityStartedActivity)
	env.RegisterActivity(wf.RecordActivityCompletedActivity)
	env.RegisterActivity(wf.RecordPhaseStartedActivity)
	env.RegisterActivity(wf.RecordPhaseCompletedActivity)
}

func registerPump(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.PumpNextActivityWorkflow, workflow.RegisterOptions{Name: executionKindPump})
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	// The pump now waits for child COMPLETION (self-cascade), so the per-activity
	// child runs end-to-end and ALL its activities must be registered.
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.GenerateWorkActivity)
	env.RegisterActivity(wf.CancelWorkerActivity)
	env.RegisterActivity(wf.SubmitPipelineActivity)
	env.RegisterActivity(wf.ObservePipelineActivity)
	env.RegisterActivity(wf.CancelPipelineActivity)
	env.RegisterActivity(wf.StoreConstructionOutputActivity)
	env.RegisterActivity(wf.RecordChangeReviewedActivity)
	env.RegisterActivity(wf.RecordActivityExitedActivity)
	env.RegisterActivity(wf.RecordActivityFailedActivity)
	env.RegisterActivity(wf.RecordOperatorPausedActivity)
}

func registerSupervision(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.ProjectSupervisionWorkflow, workflow.RegisterOptions{Name: executionKindProjectSupervision})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.CancelWorkerActivity)
	env.RegisterActivity(wf.CancelPipelineActivity)
	env.RegisterActivity(wf.RecordOperatorPausedActivity)
}

func registerReplanSweep(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.ReplanSweepWorkflow, workflow.RegisterOptions{Name: executionKindReplanSweep})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
}

func sampleActivity() constructionActivity {
	return constructionActivity{
		ActivityID:  "C-XYZ",
		Kind:        activityKindConstruction,
		ComponentID: "comp-1",
		Layer:       "engine",
		Phases:      projectstate.ProfileFor(projectstate.ActivityTypeService, 0).PhaseIDs(),
	}
}

// ---- Tests: per-activity spine (ConstructActivityWorkflow) ------------------

// The happy-path UC3 spine: cast → dispatch → submit/observe(succeeded) → stage →
// review(empty set) → recordChangeReviewed → recordActivityExited(Completed).
func Test_Construct_HappyPath_RecordsReviewedAndExited(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	art := &fakeArtifacts{}
	w := &fakeWorker{}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: aiWorker}, Intervention: &fakeIntervention{directive: directiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: pipe, Artifacts: art, Workers: w,
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
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
	// The App-A phase-walk dispatches one pipeline per phase (Requirements →
	// Detailed Design → Test Plan → Construction → Integration).
	if len(pipe.submitted) != len(sampleActivity().Phases) {
		t.Fatalf("want %d pipeline submits (one per App-A phase), got %d", len(sampleActivity().Phases), len(pipe.submitted))
	}
}

// runPumpWith builds the fakePipeline-backed Temporal test environment, executes
// ConstructActivityWorkflow with the supplied activity, and returns the pipeline
// double so the caller can inspect pipe.submitted.
func runPumpWith(t *testing.T, act constructionActivity) *fakePipeline {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	art := &fakeArtifacts{}
	w := &fakeWorker{}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: aiWorker}, Intervention: &fakeIntervention{directive: directiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: pipe, Artifacts: art, Workers: w,
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: ActivityID(act.ActivityID), Activity: act,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	return pipe
}

// Test_Construct_TestingPlanWalksThreePhases proves that a testing-plan activity
// (3 canonical phases) drives exactly 3 pipeline submissions, not the service 5.
func Test_Construct_TestingPlanWalksThreePhases(t *testing.T) {
	act := constructionActivity{
		ActivityID:  "N-STP",
		Kind:        activityKindConstruction,
		ComponentID: "system",
		Phases:      projectstate.ProfileFor(projectstate.ActivityTypeTesting, projectstate.TestVariantPlan).PhaseIDs(),
	}
	if len(act.Phases) != 3 {
		t.Fatalf("precondition: testing-plan phases = %d, want 3", len(act.Phases))
	}
	pipe := runPumpWith(t, act)
	if len(pipe.submitted) != 3 {
		t.Fatalf("submitted %d pipelines, want 3", len(pipe.submitted))
	}
}

// architectOnly skips dispatch + pipeline and awaits an operator override; a Skip
// override exits the activity with the operator-skip outcome and no worker dispatch.
func Test_Construct_ArchitectOnly_AwaitsOverride_SkipExits(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	w := &fakeWorker{}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: architectOnly}, Intervention: &fakeIntervention{directive: directiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: w,
	})
	registerConstruct(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorOverride, operatorOverrideSignal{Override: ActivityOverride{Kind: OverrideSkip}})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-ARCH", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(w.prompts) != 0 {
		t.Fatalf("architectOnly must NOT dispatch a worker, got %d dispatches", len(w.prompts))
	}
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeSkipped {
		t.Fatalf("want one recordActivityExited(Skipped), got %v", ps.exited)
	}
}

// A failed pipeline → variance → DecideOnVariance(Takeover): the takeover cancels
// the in-flight worker, then re-dispatches; with the pipeline now succeeding the
// activity completes normally on the next loop.
func Test_Construct_PipelineFailed_Takeover_CancelsWorker_ThenCompletes(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	// The pipeline fails on the first run, then a flippable fake makes it succeed.
	pipe := &flippablePipeline{first: PipelineFailed, rest: PipelineSucceeded}
	w := &fakeWorker{}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: aiWorker}, Intervention: &fakeIntervention{directive: directiveTakeover},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: pipe, Artifacts: &fakeArtifacts{}, Workers: w,
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-PF", Activity: sampleActivity(),
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
	cancelled []pipelineHandle
}

func (p *flippablePipeline) SubmitConstructionPipeline(_ context.Context, spec pipelineSpec, _ fwra.IdempotencyKey) (pipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submits++
	return pipelineHandle{Name: "wf"}, nil
}

func (p *flippablePipeline) ObserveConstructionPipeline(_ context.Context, _ pipelineHandle) (pipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.submits <= 1 {
		return pipelineObservation{Phase: p.first, Diagnostic: "boom"}, nil
	}
	return pipelineObservation{Phase: p.rest}, nil
}

func (p *flippablePipeline) CancelConstructionPipeline(_ context.Context, handle pipelineHandle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, handle)
	return nil
}

var _ constructionPipelineAccess = (*flippablePipeline)(nil)

// The §6.5 Conflict discipline: a recordChangeReviewed that returns fwra.Conflict
// twice before succeeding drives the workflow-level re-read→re-apply loop; the
// activity still completes (reviewed + exited recorded).
func Test_Construct_ConflictOnRecord_ReReadReApply_Succeeds(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}, conflictFirst: 2}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: aiWorker}, Intervention: &fakeIntervention{directive: directiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded},
		Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-CONF", Activity: sampleActivity(),
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

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{}, Intervention: &fakeIntervention{}, Review: &fakeReview{},
		ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		NextEligibleActivity: nil,
	})
	registerPump(env, wf)

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: ProjectID(ps.project.ID)})

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
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{}, Intervention: &fakeIntervention{}, Review: &fakeReview{},
		ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
	})
	registerPump(env, wf)

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: ProjectID(uuid.NewString())})

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
// *workflow.ContinueAsNewError carrying the next pumpInput. The child's spine ran
// end-to-end (one reviewed + one completed exit recorded).
func Test_Pump_EligibleActivity_RunsChild_ThenContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: aiWorker}, Intervention: &fakeIntervention{directive: directiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded},
		Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		NextEligibleActivity: func(_ projectstate.Project) (constructionActivity, bool) {
			return sampleActivity(), true
		},
	})
	registerPump(env, wf)

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if !env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	// A successful eligible dispatch self-cascades: the terminal "error" is a
	// ContinueAsNewError carrying the next tick's pumpInput (NOT a real failure).
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
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: aiWorker}, Intervention: &fakeIntervention{}, Review: &fakeReview{},
		ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		NextEligibleActivity: func(_ projectstate.Project) (constructionActivity, bool) {
			return constructionActivity{}, false // network drained
		},
	})
	registerPump(env, wf)

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

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
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: aiWorker}, Intervention: &fakeIntervention{directive: directiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded},
		Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		NextEligibleActivity: func(_ projectstate.Project) (constructionActivity, bool) {
			return sampleActivity(), true // an activity IS eligible — but the pause wins
		},
	})
	registerPump(env, wf)

	// Deliver the pause Signal so it is already queued when the pump checks (the pump's
	// non-blocking ReceiveAsync observes it at the top, before any dispatch).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, 0)

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

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
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	w := &fakeWorker{}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{}, Review: &fakeReview{},
		Intervention: &fakeIntervention{plan: pausePlan{PipelinesToCancel: []string{"wf-C-1"}, RecordPaused: true}},
		ProjectState: ps, Pipeline: pipe, Artifacts: &fakeArtifacts{}, Workers: w,
	})
	registerSupervision(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})

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
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{}, Intervention: &fakeIntervention{}, Review: &fakeReview{},
		ProjectState: ps, Pipeline: &fakePipeline{}, Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
	})
	registerReplanSweep(env, wf)

	env.ExecuteWorkflow(executionKindReplanSweep, replanSweepInput{ProjectID: &pid})

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

// ---- Tests: conditional per-phase approval gate (Task 6) --------------------

// newFakeProjectStateWithPolicy builds a fakeProjectState whose served project
// carries the given committed ReviewPolicy (the gate's start-snapshot source).
func newFakeProjectStateWithPolicy(policy projectstate.ReviewPolicy) *fakeProjectState {
	return &fakeProjectState{project: projectstate.Project{
		ID:           projectstate.ProjectID(uuid.NewString()),
		Version:      1,
		Phase:        2,
		ReviewPolicy: policy,
	}}
}

// gateDeps builds a wfDeps for the gate tests: the fake project-state serves BOTH the
// read/transition seams AND the git-status seam (wired so gitOn is true → the phase
// records fire; the PR rail stays dormant, so branch/PR/merge are no-ops).
func gateDeps(ps *fakeProjectState, pipe constructionPipelineAccess) wfDeps {
	return wfDeps{
		HandOff:      &fakeHandOff{class: aiWorker},
		Intervention: &fakeIntervention{directive: directiveRetry},
		Review:       &fakeReview{},
		ProjectState: ps,
		GitStatus:    ps,
		Pipeline:     pipe,
		Artifacts:    &fakeArtifacts{},
		Workers:      &fakeWorker{},
	}
}

// newFakePipeline is the default all-phases-succeed pipeline double.
func newFakePipeline() *fakePipeline { return &fakePipeline{phase: PipelineSucceeded} }

// failOncePipeline fails the pipeline exactly once for the named phase, then serves
// success for it (and every other phase). It correlates the observed phase via the
// last-submitted spec (runPipeline submits then immediately observes, sequentially).
type failOncePipeline struct {
	mu        sync.Mutex
	failPhase string
	failed    map[string]bool
	lastPhase string
	submitted []pipelineSpec
}

func newFakePipelineFailingOnce(phase string) *failOncePipeline {
	return &failOncePipeline{failPhase: phase, failed: map[string]bool{}}
}

func (p *failOncePipeline) SubmitConstructionPipeline(_ context.Context, spec pipelineSpec, _ fwra.IdempotencyKey) (pipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	p.lastPhase = spec.Phase
	return pipelineHandle{Name: "wf-" + spec.ActivityID}, nil
}

func (p *failOncePipeline) ObserveConstructionPipeline(_ context.Context, _ pipelineHandle) (pipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ph := p.lastPhase
	if ph == p.failPhase && !p.failed[ph] {
		p.failed[ph] = true
		return pipelineObservation{Phase: PipelineFailed, Diagnostic: "forced one-time failure"}, nil
	}
	return pipelineObservation{Phase: PipelineSucceeded}, nil
}

func (p *failOncePipeline) CancelConstructionPipeline(_ context.Context, _ pipelineHandle) error {
	return nil
}

var _ constructionPipelineAccess = (*failOncePipeline)(nil)

// Empty ReviewPolicy → no suspend, all phases dispatch. Byte-for-byte today's behavior.
func Test_Construct_EmptyPolicy_NoGate_WalksAllPhases(t *testing.T) {
	pipe := runPumpWith(t, sampleActivity()) // fakeProjectState default policy = empty
	if len(pipe.submitted) != 5 {
		t.Fatalf("empty policy submitted %d, want 5", len(pipe.submitted))
	}
}

// A gated phase suspends until the matching-phase Approve arrives, which records the
// phase completion to head-state.
func Test_Construct_GatedPhase_ApproveRecordsCompleted(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}})
	wf := newWorkflows(gateDeps(ps, newFakePipeline()))
	registerConstruct(env, wf)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 30*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("expected RecordPhaseCompleted(detailed_design) after approval")
	}
}

// The gate is phase-multiplexed: a decision for a DIFFERENT phase is ignored; only the
// matching-phase decision releases the gate.
func Test_Construct_GatedPhase_StaleSignalIgnored(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}})
	wf := newWorkflows(gateDeps(ps, newFakePipeline()))
	registerConstruct(env, wf)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove}) // wrong phase
	}, 10*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 40*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("gate must release only on the matching-phase decision")
	}
}

func Test_Construct_VarianceRetry_DoesNotReGateApprovedPhase(t *testing.T) {
	// THE resumability guarantee: approve an early gated phase, then force a LATER phase's
	// pipeline to fail once (→ variance retry re-walks from index 0). The already-approved
	// phase must NOT re-suspend — the in-memory completedPhases set skips it.
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseRequirements}, // gate phase 0
	}})
	pipe := newFakePipelineFailingOnce("test_plan") // phase 2 fails once, then succeeds
	wf := newWorkflows(gateDeps(ps, pipe))
	registerConstruct(env, wf)
	approvals := 0
	env.RegisterDelayedCallback(func() {
		approvals++
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove})
	}, 20*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// If the retry re-gated phase 0, the workflow would block waiting for a 2nd approval that
	// never comes (test times out) — reaching completion with a single approval proves it did not.
	if approvals != 1 {
		t.Fatalf("expected exactly 1 approval (phase 0 not re-gated on retry), got %d", approvals)
	}
}

// cancelledWorker returns nil bytes + nil error (the Cancel-then-Generate replay).
type cancelledWorker struct{}

func (cancelledWorker) Generate(context.Context, workerGenerateSpec, fwra.IdempotencyKey) ([]byte, error) {
	return nil, nil
}
func (cancelledWorker) Cancel(context.Context, fwra.IdempotencyKey) error { return nil }

var _ workerAccess = cancelledWorker{}
