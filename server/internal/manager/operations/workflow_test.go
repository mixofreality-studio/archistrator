package operations

import (
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// =============================================================================
// operationsManager workflow unit tests over the Temporal in-memory test environment
// (testsuite.WorkflowTestSuite). Post-temporalgen migration: the ResourceAccess layer
// is reached through the GENERATED activities (activities.gen.go) + invokers
// (invokers.gen.go). The four RA ports are constructed as CONTRACT-interface test
// doubles (fakes over operatedsystemstate/operatedruntime/usage/artifact), wired into
// genActivities and registered under the generated activity names; the workflow's Acts
// invoker surface calls them by name. The three Engines stay direct-in-workflow seam
// fakes. No Docker, no dev server.
//
// They assert the four workflow bodies + the delinquency signal branch, the §6.5
// Conflict re-read loop, idle-pause (replicas=0 via Pause), withdraw idempotency,
// cost-projection no-mutation, and the queued delinquency branch — per
// [[the-method-testing]]. STP map in manager_test.go.
// =============================================================================

// ---- Fakes: CONTRACT-interface test doubles for the RA ports ----------------

// fakeOperatedState records the head-state transition calls + serves scripted state.
// Satisfies operatedsystemstate.OperatedSystemStateAccess.
type fakeOperatedState struct {
	mu sync.Mutex

	system   operatedsystemstate.OperatedSystem
	inFlight []operatedsystemstate.OperatedSystemSummary
	notFound bool

	// conflictFirst, when >0, returns fwra.Conflict on the first N publishDesiredState
	// calls before succeeding — drives the §6.5 re-read→re-apply loop.
	conflictFirst int

	published   []operatedsystemstate.DesiredStateReason
	statusChges []operatedsystemstate.RuntimeStatus
	withdrawn   []uuid.UUID
	delinquency []operatedsystemstate.DelinquencyAction
	version     operatedsystemstate.Version
}

func (f *fakeOperatedState) ReadOperatedSystem(_ fwra.Context, _ uuid.UUID) (operatedsystemstate.OperatedSystem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return operatedsystemstate.OperatedSystem{}, fwra.New(fwra.NotFound, "no row")
	}
	return f.system, nil
}

func (f *fakeOperatedState) ReadInFlightOperatedApps(_ fwra.Context, _ operatedsystemstate.InFlightScope) ([]operatedsystemstate.OperatedSystemSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inFlight, nil
}

func (f *fakeOperatedState) bump() operatedsystemstate.Version {
	f.version++
	f.system.Version = f.version
	return f.version
}

func (f *fakeOperatedState) maybeConflict() error {
	if f.conflictFirst > 0 {
		f.conflictFirst--
		f.version++
		f.system.Version = f.version
		return fwra.New(fwra.Conflict, "stale version")
	}
	return nil
}

func (f *fakeOperatedState) PublishDesiredState(_ fwra.Context, _ uuid.UUID, _ operatedsystemstate.Version, reason operatedsystemstate.DesiredStateReason, _ *operatedsystemstate.AutoscaleDecision, _ fwra.IdempotencyKey) (operatedsystemstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.published = append(f.published, reason)
	return f.bump(), nil
}

func (f *fakeOperatedState) RecordRuntimeStatusChange(_ fwra.Context, _ uuid.UUID, _ operatedsystemstate.Version, status operatedsystemstate.RuntimeStatus, _ fwra.IdempotencyKey) (operatedsystemstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusChges = append(f.statusChges, status)
	return f.bump(), nil
}

func (f *fakeOperatedState) WithdrawSystem(_ fwra.Context, appID uuid.UUID, _ operatedsystemstate.Version, _ fwra.IdempotencyKey) (operatedsystemstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.withdrawn = append(f.withdrawn, appID)
	return f.bump(), nil
}

func (f *fakeOperatedState) RecordDelinquencyAction(_ fwra.Context, _ uuid.UUID, _ operatedsystemstate.Version, action operatedsystemstate.DelinquencyAction, _ fwra.IdempotencyKey) (operatedsystemstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delinquency = append(f.delinquency, action)
	return f.bump(), nil
}

var _ operatedsystemstate.OperatedSystemStateAccess = (*fakeOperatedState)(nil)

// fakeRuntime records publish/withdraw + serves scripted reads. Satisfies
// operatedruntime.OperatedRuntimeAccess.
type fakeRuntime struct {
	mu sync.Mutex

	health      operatedruntime.RuntimeStatus
	slo         operatedruntime.SloStatus
	attribution operatedruntime.ComputeAttribution

	publishes []uuid.UUID
	withdraws []uuid.UUID
}

func (r *fakeRuntime) PublishDesiredState(_ fwra.Context, appID uuid.UUID, _ operatedruntime.RuntimeDesiredState, _ fwra.IdempotencyKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publishes = append(r.publishes, appID)
	return nil
}

func (r *fakeRuntime) Withdraw(_ fwra.Context, appID uuid.UUID, _ fwra.IdempotencyKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.withdraws = append(r.withdraws, appID)
	return nil
}

func (r *fakeRuntime) GetApplicationHealth(_ fwra.Context, _ uuid.UUID) (operatedruntime.RuntimeStatus, error) {
	return r.health, nil
}

func (r *fakeRuntime) GetSloStatus(_ fwra.Context, _ uuid.UUID) (operatedruntime.SloStatus, error) {
	return r.slo, nil
}

func (r *fakeRuntime) ReadComputeAttribution(_ fwra.Context, _ uuid.UUID, _ operatedruntime.AttributionWindow) (operatedruntime.ComputeAttribution, error) {
	return r.attribution, nil
}

func (r *fakeRuntime) WirePaymentConfig(_ fwra.Context, _ uuid.UUID, _ operatedruntime.GatewayBinding, _ fwra.IdempotencyKey) error {
	return nil
}

var _ operatedruntime.OperatedRuntimeAccess = (*fakeRuntime)(nil)

// fakeUsage records appends + serves a scripted range. Satisfies usage.UsageAccess.
type fakeUsage struct {
	mu sync.Mutex

	rangeEvents []usage.UsageEvent
	computeN    int
	finalN      int
}

func (u *fakeUsage) RecordComputeUsage(_ fwra.Context, events []usage.UsageEvent) ([]usage.EntryRef, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.computeN += len(events)
	return nil, nil
}

func (u *fakeUsage) RecordFinalUsage(_ fwra.Context, events []usage.UsageEvent) ([]usage.EntryRef, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.finalN += len(events)
	return nil, nil
}

func (u *fakeUsage) ReadRange(_ fwra.Context, _ usage.UsageRangeQuery) ([]usage.UsageEvent, error) {
	return u.rangeEvents, nil
}

var _ usage.UsageAccess = (*fakeUsage)(nil)

// fakeArtifacts serves a scripted construction output (the deploy bundle path,
// escalation E-1). Satisfies artifact.ArtifactAccess.
type fakeArtifacts struct {
	retrieveN int
	mu        sync.Mutex
}

func (a *fakeArtifacts) RetrieveConstructionOutput(_ fwra.Context, _ string) (artifact.ConstructionOutput, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retrieveN++
	return artifact.ConstructionOutput{}, nil
}

func (a *fakeArtifacts) RetrieveOutputTree(_ fwra.Context, _ string) (artifact.OutputTree, error) {
	return artifact.OutputTree{}, nil
}

func (a *fakeArtifacts) StoreConstructionOutput(_ fwra.Context, _ artifact.ConstructionOutput) (string, error) {
	return "", nil
}

var _ artifact.ArtifactAccess = (*fakeArtifacts)(nil)

// ---- Engine fakes (direct-in-workflow seam doubles; unchanged) --------------

// fakeIntervention returns a scripted health directive.
type fakeIntervention struct {
	directive healthDirective
}

func (i *fakeIntervention) DecideOnHealth(_ healthChange, _ interventionPolicy) (healthDirective, error) {
	if i.directive == healthDirectiveUnknown {
		return healthDirectiveRetry, nil
	}
	return i.directive, nil
}

var _ interventionEngine = (*fakeIntervention)(nil)

// fakeAutoscaler returns a scripted decision.
type fakeAutoscaler struct {
	decision autoscaleDecisionSeam
}

func (a *fakeAutoscaler) ProposeDesiredState(_ telemetry, _ autoscalerDesiredState, _ autoscalerPolicy, _ infrastructureKind) (autoscaleDecisionSeam, error) {
	return a.decision, nil
}

var _ autoscalerEngine = (*fakeAutoscaler)(nil)

// fakeEstimation returns a scripted projection.
type fakeEstimation struct {
	projection CostProjectionSeam
	calls      int
}

func (e *fakeEstimation) ProjectForOperatedApp(_ observedUsage, _ infrastructureKind, _ []ScalePoint) (CostProjectionSeam, error) {
	e.calls++
	return e.projection, nil
}

var _ operationEstimationEngine = (*fakeEstimation)(nil)

// ---- helpers ----------------------------------------------------------------

func baseDeps() (wfDeps, *fakeOperatedState, *fakeRuntime, *fakeUsage, *fakeArtifacts) {
	os := &fakeOperatedState{}
	rt := &fakeRuntime{}
	us := &fakeUsage{}
	ar := &fakeArtifacts{}
	return wfDeps{
		Intervention:       &fakeIntervention{directive: healthDirectiveRetry},
		Autoscaler:         &fakeAutoscaler{decision: autoscaleDecisionSeam{Action: AutoscaleNoChange}},
		Estimation:         &fakeEstimation{},
		Acts:               genInvokers{Opts: activityOptions()},
		InfrastructureKind: infrastructureKindGoTemporalPostgres,
		CurrentCycleID:     "cycle-1",
		CustomerID:         uuid.New(),
	}, os, rt, us, ar
}

// registerActs wires the four RA fakes into genActivities and registers each generated
// activity under its stable registered name — the names the workflow's Acts invokers
// call by. Registering the full set each test is harmless (unused ones are never
// dispatched) and keeps the per-workflow register helpers uniform.
func registerActs(env *testsuite.TestWorkflowEnvironment, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage, ar *fakeArtifacts) {
	acts := &genActivities{OperatedSystemState: os, OperatedRuntime: rt, Usage: us, Artifact: ar}
	reg := func(fn any, name string) {
		env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	reg(acts.OperatedSystemStateReadOperatedSystem, "operatedSystemStateAccess.readOperatedSystem")
	reg(acts.OperatedSystemStateReadInFlightOperatedApps, "operatedSystemStateAccess.readInFlightOperatedApps")
	reg(acts.OperatedSystemStatePublishDesiredState, "operatedSystemStateAccess.publishDesiredState")
	reg(acts.OperatedSystemStateRecordRuntimeStatusChange, "operatedSystemStateAccess.recordRuntimeStatusChange")
	reg(acts.OperatedSystemStateWithdrawSystem, "operatedSystemStateAccess.withdrawSystem")
	reg(acts.OperatedSystemStateRecordDelinquencyAction, "operatedSystemStateAccess.recordDelinquencyAction")
	reg(acts.OperatedRuntimePublishDesiredState, "operatedRuntimeAccess.publishDesiredState")
	reg(acts.OperatedRuntimeWithdraw, "operatedRuntimeAccess.withdraw")
	reg(acts.OperatedRuntimeGetApplicationHealth, "operatedRuntimeAccess.getApplicationHealth")
	reg(acts.OperatedRuntimeGetSloStatus, "operatedRuntimeAccess.getSloStatus")
	reg(acts.OperatedRuntimeReadComputeAttribution, "operatedRuntimeAccess.readComputeAttribution")
	reg(acts.UsageRecordComputeUsage, "usageAccess.recordComputeUsage")
	reg(acts.UsageRecordFinalUsage, "usageAccess.recordFinalUsage")
	reg(acts.UsageReadRange, "usageAccess.readRange")
	reg(acts.ArtifactRetrieveConstructionOutput, "artifactAccess.retrieveConstructionOutput")
}

func registerDeploy(env *testsuite.TestWorkflowEnvironment, wf *workflows, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage, ar *fakeArtifacts) {
	env.RegisterWorkflowWithOptions(wf.DeployWorkflow, workflow.RegisterOptions{Name: executionKindDeploy})
	registerActs(env, os, rt, us, ar)
}

func registerReconcile(env *testsuite.TestWorkflowEnvironment, wf *workflows, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage, ar *fakeArtifacts) {
	env.RegisterWorkflowWithOptions(wf.ReconcileWorkflow, workflow.RegisterOptions{Name: executionKindReconcile})
	registerActs(env, os, rt, us, ar)
}

func registerWithdraw(env *testsuite.TestWorkflowEnvironment, wf *workflows, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage, ar *fakeArtifacts) {
	env.RegisterWorkflowWithOptions(wf.WithdrawWorkflow, workflow.RegisterOptions{Name: executionKindWithdraw})
	registerActs(env, os, rt, us, ar)
}

func registerCostProjection(env *testsuite.TestWorkflowEnvironment, wf *workflows, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage, ar *fakeArtifacts) {
	env.RegisterWorkflowWithOptions(wf.CostProjectionWorkflow, workflow.RegisterOptions{Name: executionKindCostProjection})
	registerActs(env, os, rt, us, ar)
}

func registerView(env *testsuite.TestWorkflowEnvironment, wf *workflows, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage, ar *fakeArtifacts) {
	env.RegisterWorkflowWithOptions(wf.ViewWorkflow, workflow.RegisterOptions{Name: executionKindOperatedSystemView})
	registerActs(env, os, rt, us, ar)
}

func registerDelinquency(env *testsuite.TestWorkflowEnvironment, wf *workflows, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage, ar *fakeArtifacts) {
	env.RegisterWorkflowWithOptions(wf.DelinquencyEnforcementWorkflow, workflow.RegisterOptions{Name: executionKindDelinquency})
	registerActs(env, os, rt, us, ar)
}

// ============================ B. DeployWorkflow ==============================

// B1: full-bundle first deploy with a bundle ref retrieves the bundle, publishes the
// desired state, and records the head-state transition (reason=deployAfterConstruction).
func Test_Deploy_HappyPath_RetrievesBundle_PublishesAndRecords(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 4, DeployableBundleRef: "addr-1"}
	wf := newWorkflows(deps)
	registerDeploy(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindDeploy, deployInput{
		OperatedAppID: appID,
		Change:        DesiredStateChange{Reason: ReasonDeployAfterConstruction, PatchKind: PatchFullBundle, ChangeID: "c1"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res DeployResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.Published {
		t.Fatal("want Published:true")
	}
	if ar.retrieveN != 1 {
		t.Fatalf("want one bundle retrieve, got %d", ar.retrieveN)
	}
	if len(rt.publishes) != 1 {
		t.Fatalf("want one runtime publish, got %d", len(rt.publishes))
	}
	if len(os.published) != 1 || os.published[0] != operatedsystemstate.ReasonDeployAfterConstruction {
		t.Fatalf("want one head-state publish(deployAfterConstruction), got %v", os.published)
	}
}

// B2: a full-bundle first deploy with NO deployableBundleRef fails the pre-condition
// (FailedPrecondition); nothing is published.
func Test_Deploy_NoBundleRef_FailedPrecondition(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 1, DeployableBundleRef: ""}
	wf := newWorkflows(deps)
	registerDeploy(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindDeploy, deployInput{
		OperatedAppID: appID,
		Change:        DesiredStateChange{Reason: ReasonDeployAfterConstruction, PatchKind: PatchFullBundle, ChangeID: "c1"},
	})

	if env.GetWorkflowError() == nil {
		t.Fatal("want a FailedPrecondition error for a missing deployableBundleRef")
	}
	if len(rt.publishes) != 0 || len(os.published) != 0 {
		t.Fatalf("nothing must be published on a failed pre-condition; publishes=%d head=%d", len(rt.publishes), len(os.published))
	}
}

// B3: an operator scale republish (PatchScale) does NOT retrieve a bundle but still
// publishes + records (reason=operator).
func Test_Deploy_OperatorScale_NoBundleRetrieve(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 2}
	wf := newWorkflows(deps)
	registerDeploy(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindDeploy, deployInput{
		OperatedAppID: appID,
		Change:        DesiredStateChange{Reason: ReasonOperator, PatchKind: PatchScale, ChangeID: "c2"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if ar.retrieveN != 0 {
		t.Fatalf("operator scale must NOT retrieve a bundle, got %d", ar.retrieveN)
	}
	if len(rt.publishes) != 1 || len(os.published) != 1 || os.published[0] != operatedsystemstate.ReasonOperator {
		t.Fatalf("want one publish + head-state record(operator), got publishes=%d head=%v", len(rt.publishes), os.published)
	}
}

// ============================ C. ReconcileWorkflow ==========================

// C1: a health transition records the status change, runs DecideOnHealth(Retry) which
// re-publishes desired state, and records observed usage.
func Test_Reconcile_HealthTransition_RecordsStatus_AndRepublishes(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.inFlight = []operatedsystemstate.OperatedSystemSummary{{ID: appID, Version: 1, Status: operatedsystemstate.RuntimeStatusHealthy}}
	rt.health = operatedruntime.RuntimeStatusDegraded // transition healthy → degraded
	rt.attribution = operatedruntime.ComputeAttribution{Units: operatedruntime.ComputeUnits{Amount: 2, Unit: "cpu-second"}, RuntimeEventID: "evt-1"}
	deps.Intervention = &fakeIntervention{directive: healthDirectiveRetry}
	wf := newWorkflows(deps)
	registerReconcile(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindReconcile, reconcileInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res ReconcileResult
	_ = env.GetWorkflowResult(&res)
	if res.Observed != 1 || res.Transitions != 1 {
		t.Fatalf("want Observed=1 Transitions=1, got %+v", res)
	}
	if len(os.statusChges) != 1 {
		t.Fatalf("want one recordRuntimeStatusChange, got %d", len(os.statusChges))
	}
	if us.computeN != 1 {
		t.Fatalf("want one recordComputeUsage, got %d", us.computeN)
	}
	// Retry directive re-publishes prior desired state (runtime publish, no head-state
	// autoscale record).
	if len(rt.publishes) != 1 {
		t.Fatalf("want one re-publish from the Retry directive, got %d", len(rt.publishes))
	}
}

// C2: autoscaler Pause (idle) publishes replicas=0 and records the head-state
// transition (reason=autoscale).
func Test_Reconcile_AutoscalePause_PublishesAndRecordsAutoscale(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.inFlight = []operatedsystemstate.OperatedSystemSummary{{ID: appID, Version: 1, Status: operatedsystemstate.RuntimeStatusHealthy}}
	rt.health = operatedruntime.RuntimeStatusHealthy // no health transition
	deps.Autoscaler = &fakeAutoscaler{decision: autoscaleDecisionSeam{Action: AutoscalePause}}
	wf := newWorkflows(deps)
	registerReconcile(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindReconcile, reconcileInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res ReconcileResult
	_ = env.GetWorkflowResult(&res)
	if res.Transitions != 0 {
		t.Fatalf("no health transition expected, got Transitions=%d", res.Transitions)
	}
	if res.Republished != 1 {
		t.Fatalf("want one autoscaler republish (Pause), got %d", res.Republished)
	}
	if len(os.published) != 1 || os.published[0] != operatedsystemstate.ReasonAutoscale {
		t.Fatalf("want one head-state publish(autoscale), got %v", os.published)
	}
	if len(rt.publishes) != 1 {
		t.Fatalf("want one runtime publish for the idle-pause, got %d", len(rt.publishes))
	}
}

// C3: a quiet tick (no health transition + NoChange) records nothing.
func Test_Reconcile_QuietTick_NoTransitions_NoRepublishes(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.inFlight = []operatedsystemstate.OperatedSystemSummary{{ID: appID, Version: 1, Status: operatedsystemstate.RuntimeStatusHealthy}}
	rt.health = operatedruntime.RuntimeStatusHealthy
	rt.attribution = operatedruntime.ComputeAttribution{} // empty event id ⇒ no usage append
	wf := newWorkflows(deps)
	registerReconcile(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindReconcile, reconcileInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res ReconcileResult
	_ = env.GetWorkflowResult(&res)
	if res.Transitions != 0 || res.Republished != 0 {
		t.Fatalf("want a quiet tick, got %+v", res)
	}
	if len(os.published) != 0 || len(os.statusChges) != 0 || len(rt.publishes) != 0 {
		t.Fatalf("quiet tick must not write; published=%v status=%v publishes=%d", os.published, os.statusChges, len(rt.publishes))
	}
}

// C4: multiple in-flight apps are all observed (counted).
func Test_Reconcile_MultipleApps_AllObserved(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	os.inFlight = []operatedsystemstate.OperatedSystemSummary{
		{ID: uuid.New(), Version: 1, Status: operatedsystemstate.RuntimeStatusHealthy},
		{ID: uuid.New(), Version: 1, Status: operatedsystemstate.RuntimeStatusHealthy},
		{ID: uuid.New(), Version: 1, Status: operatedsystemstate.RuntimeStatusHealthy},
	}
	rt.health = operatedruntime.RuntimeStatusHealthy
	wf := newWorkflows(deps)
	registerReconcile(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindReconcile, reconcileInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res ReconcileResult
	_ = env.GetWorkflowResult(&res)
	if res.Observed != 3 {
		t.Fatalf("want Observed=3, got %d", res.Observed)
	}
}

// ============================ D. WithdrawWorkflow ===========================

// D1: happy path withdraws the runtime, records final usage, and withdraws the
// head-state.
func Test_Withdraw_HappyPath(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 2, Status: operatedsystemstate.RuntimeStatusHealthy}
	rt.attribution = operatedruntime.ComputeAttribution{Units: operatedruntime.ComputeUnits{Amount: 1, Unit: "cpu-second"}, RuntimeEventID: "final-1"}
	wf := newWorkflows(deps)
	registerWithdraw(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindWithdraw, withdrawInput{OperatedAppID: appID})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res WithdrawResult
	_ = env.GetWorkflowResult(&res)
	if !res.Withdrawn {
		t.Fatal("want Withdrawn:true")
	}
	if len(rt.withdraws) != 1 {
		t.Fatalf("want one runtime withdraw, got %d", len(rt.withdraws))
	}
	if us.finalN != 1 {
		t.Fatalf("want one recordFinalUsage, got %d", us.finalN)
	}
	if len(os.withdrawn) != 1 {
		t.Fatalf("want one head-state withdraw, got %d", len(os.withdrawn))
	}
}

// D2: an already-withdrawn head-state is a no-op success (no runtime call).
func Test_Withdraw_AlreadyWithdrawn_NoOpSuccess(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 5, Status: operatedsystemstate.RuntimeStatusWithdrawn}
	wf := newWorkflows(deps)
	registerWithdraw(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindWithdraw, withdrawInput{OperatedAppID: appID})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res WithdrawResult
	_ = env.GetWorkflowResult(&res)
	if !res.Withdrawn {
		t.Fatal("already-withdrawn must be a no-op success")
	}
	if len(rt.withdraws) != 0 || len(os.withdrawn) != 0 {
		t.Fatalf("already-withdrawn must not re-call; runtime=%d head=%d", len(rt.withdraws), len(os.withdrawn))
	}
}

// D3: a read NotFound (unknown app) is a no-op success.
func Test_Withdraw_NotFound_NoOpSuccess(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	os.notFound = true
	wf := newWorkflows(deps)
	registerWithdraw(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindWithdraw, withdrawInput{OperatedAppID: uuid.New()})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res WithdrawResult
	_ = env.GetWorkflowResult(&res)
	if !res.Withdrawn {
		t.Fatal("not-found app must be a no-op withdraw success")
	}
	if len(rt.withdraws) != 0 {
		t.Fatalf("not-found must not call runtime withdraw, got %d", len(rt.withdraws))
	}
}

// ============================ E. CostProjectionWorkflow =====================

// E1: cost projection reads usage + head-state, returns the Engine projection, and
// MUTATES NO STATE (no head-state writes, no usage appends).
func Test_CostProjection_ReturnsProjection_NoMutation(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 3}
	us.rangeEvents = []usage.UsageEvent{{OperatedAppID: appID, RuntimeEventID: "e1"}}
	est := &fakeEstimation{projection: CostProjectionSeam{
		CurrentRunRate:       Money{MinorUnits: 1200, Currency: "USD"},
		ProjectedMonthlyCost: Money{MinorUnits: 36000, Currency: "USD"},
	}}
	deps.Estimation = est
	wf := newWorkflows(deps)
	registerCostProjection(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindCostProjection, costProjectionInput{OperatedAppID: appID})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res costProjection
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.ProjectedMonthlyCost.MinorUnits != 36000 {
		t.Fatalf("want the Engine projection passed through, got %+v", res)
	}
	if est.calls != 1 {
		t.Fatalf("want one Engine projection call, got %d", est.calls)
	}
	// NO MUTATION assertion: no head-state writes, no usage appends, no runtime publish.
	if len(os.published) != 0 || len(os.statusChges) != 0 || len(os.withdrawn) != 0 || len(os.delinquency) != 0 {
		t.Fatalf("cost projection must not mutate head-state; %+v", os)
	}
	if us.computeN != 0 || us.finalN != 0 {
		t.Fatalf("cost projection must not append usage; compute=%d final=%d", us.computeN, us.finalN)
	}
	if len(rt.publishes) != 0 || len(rt.withdraws) != 0 {
		t.Fatalf("cost projection must not write runtime; publishes=%d withdraws=%d", len(rt.publishes), len(rt.withdraws))
	}
}

// ============================ H. ViewWorkflow (read-only) ===================

// H1: the operator view composes the existing reads (head-state + health + SLO +
// run-rate) into one OperatedSystemView and MUTATES NO STATE — zero write Activities,
// no version bump. This is the U-SPA-4 read path (operationsRead-ruling.md §A).
func Test_View_ComposesReads_NoMutation(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 7, Status: operatedsystemstate.RuntimeStatusHealthy, InFlight: true}
	os.version = 7
	rt.health = operatedruntime.RuntimeStatusHealthy
	rt.slo = operatedruntime.SloStatus{SloMet: true, Detail: "99.9% / 30d"}
	us.rangeEvents = []usage.UsageEvent{{OperatedAppID: appID, RuntimeEventID: "e1"}}
	deps.AutoscalerPolicy = autoscalerPolicy{Mode: AutoscalerModeAuto}
	deps.Estimation = &fakeEstimation{projection: CostProjectionSeam{
		CurrentRunRate: Money{MinorUnits: 4120, Currency: "USD"},
	}}
	wf := newWorkflows(deps)
	registerView(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindOperatedSystemView, viewInput{OperatedAppID: appID})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var view OperatedSystemView
	if err := env.GetWorkflowResult(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Composed read content.
	if view.OperatedAppID != appID {
		t.Fatalf("operatedAppID = %v, want %v", view.OperatedAppID, appID)
	}
	if view.Phase != RuntimeStatusHealthy || !view.InFlight {
		t.Fatalf("phase/inFlight = %v/%v, want Healthy/true", view.Phase, view.InFlight)
	}
	if !view.Health.SloMet || view.Health.Phase != RuntimeStatusHealthy {
		t.Fatalf("health snapshot = %+v", view.Health)
	}
	if len(view.Slos) != 1 || !view.Slos[0].SloMet || !view.Slos[0].Healthy {
		t.Fatalf("slos = %+v", view.Slos)
	}
	if view.Autoscaler.Mode != AutoscalerModeAuto {
		t.Fatalf("autoscaler mode = %v, want Auto", view.Autoscaler.Mode)
	}
	if view.CurrentRunRate.MinorUnits != 4120 || view.CurrentRunRate.Currency != "USD" {
		t.Fatalf("currentRunRate = %+v", view.CurrentRunRate)
	}

	// NO MUTATION assertion: the view path performs no write Activity and no version bump.
	if os.version != 7 {
		t.Fatalf("view must NOT bump head-state version; want 7, got %d", os.version)
	}
	if len(os.published) != 0 || len(os.statusChges) != 0 || len(os.withdrawn) != 0 || len(os.delinquency) != 0 {
		t.Fatalf("view must not write head-state; %+v", os)
	}
	if us.computeN != 0 || us.finalN != 0 {
		t.Fatalf("view must not append usage; compute=%d final=%d", us.computeN, us.finalN)
	}
	if len(rt.publishes) != 0 || len(rt.withdraws) != 0 {
		t.Fatalf("view must not write runtime; publishes=%d withdraws=%d", len(rt.publishes), len(rt.withdraws))
	}
}

// ============================ F. DelinquencyEnforcementWorkflow =============

// F1: the queued signal resumes the branch; pause terms publish replicas=0 + record
// the delinquency action (Paused).
func Test_Delinquency_PauseTerms_PublishesAndRecordsPaused(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	cid := uuid.New()
	os.inFlight = []operatedsystemstate.OperatedSystemSummary{{ID: uuid.New(), Version: 1}, {ID: uuid.New(), Version: 1}}
	wf := newWorkflows(deps)
	registerDelinquency(env, wf, os, rt, us, ar)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalApplyDelinquencyPolicy, applyDelinquencySignal{
			CustomerID: cid, Context: DelinquencyContext{PauseNotWithdraw: true},
		})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindDelinquency, delinquencyInput{CustomerID: cid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(rt.publishes) != 2 {
		t.Fatalf("want two pause publishes (one per app), got %d", len(rt.publishes))
	}
	if len(rt.withdraws) != 0 {
		t.Fatalf("pause terms must NOT withdraw, got %d", len(rt.withdraws))
	}
	if len(os.delinquency) != 2 || os.delinquency[0] != operatedsystemstate.DelinquencyActionPaused {
		t.Fatalf("want two recordDelinquencyAction(Paused), got %v", os.delinquency)
	}
}

// F2: withdraw terms withdraw the runtime + record the delinquency action (Withdrawn).
func Test_Delinquency_WithdrawTerms_WithdrawsAndRecordsWithdrawn(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	cid := uuid.New()
	os.inFlight = []operatedsystemstate.OperatedSystemSummary{{ID: uuid.New(), Version: 1}}
	wf := newWorkflows(deps)
	registerDelinquency(env, wf, os, rt, us, ar)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalApplyDelinquencyPolicy, applyDelinquencySignal{
			CustomerID: cid, Context: DelinquencyContext{PauseNotWithdraw: false},
		})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindDelinquency, delinquencyInput{CustomerID: cid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(rt.withdraws) != 1 {
		t.Fatalf("want one withdraw on withdraw terms, got %d", len(rt.withdraws))
	}
	if len(os.delinquency) != 1 || os.delinquency[0] != operatedsystemstate.DelinquencyActionWithdrawn {
		t.Fatalf("want one recordDelinquencyAction(Withdrawn), got %v", os.delinquency)
	}
}

// ============================ G. §6.5 Conflict discipline ===================

// G1: a recordPublishDesiredState that returns fwra.Conflict twice before succeeding
// drives the workflow-level re-read→re-apply loop; the deploy still completes.
func Test_Deploy_ConflictOnRecord_ReReadReApply_Succeeds(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 1}
	os.conflictFirst = 2 // first two head-state publishes Conflict, then succeed
	wf := newWorkflows(deps)
	registerDeploy(env, wf, os, rt, us, ar)

	env.ExecuteWorkflow(executionKindDeploy, deployInput{
		OperatedAppID: appID,
		Change:        DesiredStateChange{Reason: ReasonOperator, PatchKind: PatchScale, ChangeID: "c-conf"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res DeployResult
	_ = env.GetWorkflowResult(&res)
	if !res.Published {
		t.Fatal("deploy must converge after the Conflict re-read loop")
	}
	if len(os.published) != 1 {
		t.Fatalf("conflict loop must converge to exactly one recorded head-state publish, got %v", os.published)
	}
}
