package operations

// =============================================================================
// SERVICE TEST PLAN (STP) — operationsManager (C-MOP)
// (the-method-testing: STP-first — the list of all the ways to demonstrate the
//  component does NOT work. NO BDD/Gherkin. White-box test client + black-box tests
//  with hand-written fakes for the frozen-collaborator seams.)
//
// A. Façade pre-condition / contract-misuse (this file; no Temporal client needed —
//    the checks short-circuit before any client call):
//   A1  DeployAfterConstruction rejects empty operatedAppId            → ContractMisuse
//   A2  DeployAfterConstruction rejects empty changeId                 → ContractMisuse
//   A3  DeployAfterConstruction rejects Reason=autoscale (reserved)    → ContractMisuse  (OQ-5)
//   A4  DeployAfterConstruction rejects Reason=delinquency (reserved)  → ContractMisuse  (OQ-5)
//   A5  DeployAfterConstruction rejects Reason=unknown                 → ContractMisuse
//   A6  ReconcileOperatedState rejects empty tickId                    → ContractMisuse
//   A7  WithdrawSystem rejects empty operatedAppId / empty changeId    → ContractMisuse
//   A8  QueryCostProjection rejects empty operatedAppId / empty requestId → ContractMisuse
//   A9  ApplyDelinquencyPolicy rejects empty customerId                → ContractMisuse
//   A10 Workflow-id derivation tokens are the §6.1 shapes
//   A11 DesiredStateReason / AutoscaleAction String() coverage
//
// B. DeployWorkflow (workflow_test.go):
//   B1  happy path: read → (bundle) → publish runtime → record head-state(deploy)
//   B2  missing deployableBundleRef on a full-bundle first deploy → FailedPrecondition
//   B3  operator scale (PatchScale) republish: no bundle retrieve, publish+record
//
// C. ReconcileWorkflow (workflow_test.go):
//   C1  Path B: health transition → recordRuntimeStatusChange + DecideOnHealth(Retry)
//                                   → re-publish; usage recorded
//   C2  Path C: autoscaler Pause (idle) → record(autoscale); no runtime publish yet
//       (no replica-override input to assembleDesiredState — follow-up)
//   C3  quiet tick: no transition + NoChange → no transitions, no republishes
//   C4  multiple in-flight apps counted in ReconcileResult.Observed
//
// D. WithdrawWorkflow (workflow_test.go):
//   D1  happy path: withdraw runtime → record final usage → withdraw head-state
//   D2  already-withdrawn head-state → no-op success (no runtime call)
//   D3  read NotFound (unknown app) → no-op success
//
// E. CostProjectionWorkflow (workflow_test.go):
//   E1  reads usage range + head-state, returns the Engine projection, NO mutation
//       (asserted by zero head-state writes on the fake)
//
// F. DelinquencyEnforcementWorkflow (workflow_test.go):
//   F1  queued signal resumes branch → pause recordDelinquencyAction; no runtime
//       publish yet (no replica-override input to assembleDesiredState — follow-up)
//   F2  withdraw-terms branch → withdraw runtime + recordDelinquencyAction
//
// G. §6.5 Conflict discipline (workflow_test.go):
//   G1  recordPublishDesiredState returns Conflict twice → re-read→re-apply converges
// =============================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	projectstatefake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on the
// five public ops (operationsManager.md §2/§3.4). They run BEFORE any Temporal client
// call, so they need no cluster and no client — a nil client is safe because the
// checks short-circuit first.

// bgCtx is the Manager-layer call Context the façade pre-condition tests pass (the
// Principal is zero — these checks short-circuit before any Temporal/authz path).
func bgCtx() fwmgr.Context {
	return fwmgr.Context{Context: context.Background()}
}

func asOperationsError(t *testing.T, err error) *fwmgr.Error {
	t.Helper()
	var oe *fwmgr.Error
	if !errors.As(err, &oe) {
		t.Fatalf("expected *OperationsError, got %T: %v", err, err)
	}
	return oe
}

// ---- A1/A2: DeployAfterConstruction id checks -------------------------------

func Test_Deploy_EmptyOperatedAppID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.Nil,
		DesiredStateChange{Reason: ReasonDeployAfterConstruction, ChangeID: "c1"})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_Deploy_EmptyChangeID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.New(),
		DesiredStateChange{Reason: ReasonOperator, ChangeID: ""})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A3/A4/A5: the reason discriminator rejection (OQ-5) --------------------

func Test_Deploy_RejectsReservedAutoscaleReason(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.New(),
		DesiredStateChange{Reason: ReasonAutoscale, ChangeID: "c1"})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("autoscale reason must be ContractMisuse on deploy, got %s", got)
	}
}

func Test_Deploy_RejectsReservedDelinquencyReason(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.New(),
		DesiredStateChange{Reason: ReasonDelinquency, ChangeID: "c1"})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("delinquency reason must be ContractMisuse on deploy, got %s", got)
	}
}

func Test_Deploy_RejectsUnknownReason(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.DeployAfterConstruction(bgCtx(), uuid.New(),
		DesiredStateChange{Reason: ReasonUnknown, ChangeID: "c1"})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("unknown reason must be ContractMisuse on deploy, got %s", got)
	}
}

// ---- A6: ReconcileOperatedState ---------------------------------------------

func Test_Reconcile_EmptyTickID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.ReconcileOperatedState(bgCtx(), "", nil)
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A7: WithdrawSystem ------------------------------------------------------

func Test_Withdraw_EmptyOperatedAppID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.WithdrawSystem(bgCtx(), uuid.Nil, "c1", WithdrawReason{})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_Withdraw_EmptyChangeID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.WithdrawSystem(bgCtx(), uuid.New(), "", WithdrawReason{})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A8: QueryCostProjection ------------------------------------------------

func Test_CostProjection_EmptyOperatedAppID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.QueryCostProjection(bgCtx(), uuid.Nil, "r1", nil)
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_CostProjection_EmptyRequestID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.QueryCostProjection(bgCtx(), uuid.New(), "", nil)
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A8b: QueryOperatedSystemView (op 2.7) ----------------------------------

func Test_View_EmptyOperatedAppID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.QueryOperatedSystemView(bgCtx(), uuid.Nil, "r1")
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_View_EmptyRequestID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.QueryOperatedSystemView(bgCtx(), uuid.New(), "")
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A9: ApplyDelinquencyPolicy ---------------------------------------------

func Test_Delinquency_EmptyCustomerID(t *testing.T) {
	m := newOperationsManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.ApplyDelinquencyPolicy(bgCtx(), uuid.Nil, DelinquencyContext{})
	if got := asOperationsError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A10: workflow id derivation (§6.1) -------------------------------------

func Test_WorkflowIDDerivation(t *testing.T) {
	pid := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	if got := deployWorkflowID(pid, "c1"); got != pid.String()+":deploy:c1" {
		t.Fatalf("deploy id: %q", got)
	}
	if got := reconcileWorkflowID("t1"); got != "operatedStateReconcile:t1" {
		t.Fatalf("reconcile id: %q", got)
	}
	if got := withdrawWorkflowID(pid, "c2"); got != pid.String()+":withdraw:c2" {
		t.Fatalf("withdraw id: %q", got)
	}
	if got := costProjectionWorkflowID(pid, "r1"); got != pid.String()+":costProjection:r1" {
		t.Fatalf("cost projection id: %q", got)
	}
	if got := viewWorkflowID(pid, "r1"); got != pid.String()+":view:r1" {
		t.Fatalf("view id: %q", got)
	}
	if got := delinquencyWorkflowID(pid); got != pid.String()+":delinquency" {
		t.Fatalf("delinquency id: %q", got)
	}
}

// ---- A11: String coverage ---------------------------------------------------

func Test_DesiredStateReason_String(t *testing.T) {
	cases := map[DesiredStateReason]string{
		ReasonDeployAfterConstruction: "deployAfterConstruction",
		ReasonOperator:                "operator",
		ReasonAutoscale:               "autoscale",
		ReasonDelinquency:             "delinquency",
		ReasonUnknown:                 "unknown",
	}
	for r, want := range cases {
		if got := desiredStateReasonName(r); got != want {
			t.Fatalf("desiredStateReasonName(%d) = %q, want %q", int(r), got, want)
		}
	}
}

func Test_AutoscaleAction_String(t *testing.T) {
	cases := map[AutoscaleAction]string{
		AutoscaleNoChange: "NoChange", AutoscaleScaleUp: "ScaleUp",
		AutoscaleScaleDown: "ScaleDown", AutoscalePause: "Pause", AutoscaleResume: "Resume",
	}
	for a, want := range cases {
		if got := autoscaleActionName(a); got != want {
			t.Fatalf("autoscaleActionName(%d) = %q, want %q", int(a), got, want)
		}
	}
}

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

func (f *fakeOperatedState) RegisterOperatedSystem(_ fwra.Context, _ uuid.UUID, _ uuid.UUID, _ string, _ string, _ fwra.IdempotencyKey) (operatedsystemstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
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
	published []operatedruntime.RuntimeDesiredState // review finding 1: the PAYLOAD each publish carried, not just the count
	withdraws []uuid.UUID
}

func (r *fakeRuntime) PublishDesiredState(_ fwra.Context, appID uuid.UUID, desired operatedruntime.RuntimeDesiredState, _ fwra.IdempotencyKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publishes = append(r.publishes, appID)
	r.published = append(r.published, desired)
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

// fakeBundleManifestJSON is a valid bundleManifest (assemble.go) payload — the
// deploy bundle path (B1) now folds this through assembleDesiredState, which
// requires parseable bundle bytes; content doesn't matter to these tests, only
// that it parses.
const fakeBundleManifestJSON = `{"serverImage":"fake/server:test","webAppImage":"fake/webapp:test"}`

func (a *fakeArtifacts) RetrieveConstructionOutput(_ fwra.Context, _ string) (artifact.ConstructionOutput, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.retrieveN++
	return artifact.ConstructionOutput{Bytes: []byte(fakeBundleManifestJSON), MIMEType: "application/json"}, nil
}

func (a *fakeArtifacts) RetrieveOutputTree(_ fwra.Context, _ string) (artifact.OutputTree, error) {
	return artifact.OutputTree{}, nil
}

func (a *fakeArtifacts) StoreConstructionOutput(_ fwra.Context, _ artifact.ConstructionOutput) (string, error) {
	return "", nil
}

var _ artifact.ArtifactAccess = (*fakeArtifacts)(nil)

// ---- Engine fakes (direct-in-workflow published-contract doubles) ----------

// fakeIntervention returns a scripted health directive. Satisfies
// intervention.InterventionEngine (only DecideOnHealth is exercised by these tests;
// the other three verbs are unused stubs).
type fakeIntervention struct {
	directive intervention.HealthDirective
}

func (i *fakeIntervention) DecideOnHealth(_ fweng.Context, _ intervention.HealthChange) (intervention.HealthDirective, error) {
	return i.directive, nil
}

func (i *fakeIntervention) ApplyPausePolicy(_ fweng.Context, _ intervention.PauseRequestContext) (intervention.PausePlan, error) {
	return intervention.PausePlan{}, nil
}

func (i *fakeIntervention) DecideOnSettlementFailure(_ fweng.Context, _ intervention.SettlementFailure) (intervention.SettlementFailureDirective, error) {
	return intervention.SettlementRetry, nil
}

func (i *fakeIntervention) DecideOnVariance(_ fweng.Context, _ intervention.ConstructionVariance) (intervention.VarianceDirective, error) {
	return intervention.VarianceRetry, nil
}

var _ intervention.InterventionEngine = (*fakeIntervention)(nil)

// fakeAutoscaler returns a scripted decision. Satisfies autoscaler.AutoscalerEngine.
type fakeAutoscaler struct {
	decision autoscaler.Decision
}

func (a *fakeAutoscaler) ProposeDesiredState(_ fweng.Context, _ autoscaler.Telemetry, _ autoscaler.DesiredState, _ autoscaler.AutoscalerPolicy, _ autoscaler.InfrastructureKind) (autoscaler.Decision, error) {
	return a.decision, nil
}

var _ autoscaler.AutoscalerEngine = (*fakeAutoscaler)(nil)

// fakeEstimation returns a scripted projection. Satisfies
// operationestimation.OperationEstimationEngine (only ProjectForOperatedApp is
// exercised; EstimateForOption is an unused stub).
type fakeEstimation struct {
	projection operationestimation.CostProjection
	calls      int
}

func (e *fakeEstimation) ProjectForOperatedApp(_ fweng.Context, _ operationestimation.ObservedUsage, _ operationestimation.InfrastructureKind, _ []operationestimation.ScalePoint) (operationestimation.CostProjection, error) {
	e.calls++
	return e.projection, nil
}

func (e *fakeEstimation) EstimateForOption(_ fweng.Context, _ operationestimation.ProjectOption, _ operationestimation.UsageAssumption, _ operationestimation.InfrastructureKind) (operationestimation.OperationForecast, error) {
	return operationestimation.OperationForecast{}, nil
}

var _ operationestimation.OperationEstimationEngine = (*fakeEstimation)(nil)

// ---- helpers ----------------------------------------------------------------

func baseDeps() (wfDeps, *fakeOperatedState, *fakeRuntime, *fakeUsage, *fakeArtifacts) {
	os := &fakeOperatedState{}
	rt := &fakeRuntime{}
	us := &fakeUsage{}
	ar := &fakeArtifacts{}
	return wfDeps{
		Intervention:       &fakeIntervention{directive: intervention.HealthRetry},
		Autoscaler:         &fakeAutoscaler{decision: autoscaler.Decision{Kind: autoscaler.DecisionNoChange}},
		Estimation:         &fakeEstimation{},
		Acts:               genInvokers{Opts: activityOptions()},
		InfrastructureKind: autoscaler.InfrastructureKindGoTemporalPostgres,
		CurrentCycleID:     "cycle-1",
		CustomerID:         uuid.New(),
	}, os, rt, us, ar
}

// registerActs wires the four RA fakes into genActivities and registers each generated
// activity under its stable registered name — the names the workflow's Acts invokers
// call by. Registering the full set each test is harmless (unused ones are never
// dispatched) and keeps the per-workflow register helpers uniform.
func registerActs(env *testsuite.TestWorkflowEnvironment, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage, ar *fakeArtifacts) {
	// ps serves the SAME complete "archistrator" project fixture assemble_test.go
	// exercises directly (projectFixture) — so DeployWorkflow's happy path (B1),
	// which now folds the deployment model through assembleDesiredState, keeps
	// succeeding without every test needing to author its own model.
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
			return projectFixture(), nil
		},
	}
	acts := &genActivities{OperatedSystemState: os, OperatedRuntime: rt, Usage: us, Artifact: ar, ProjectState: ps}
	reg := func(fn any, name string) {
		env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	reg(acts.ProjectStateReadProject, "projectStateAccess.readProject")
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
	// Review finding 1: assert WHAT was published, not just that a publish
	// happened — a left-zero-valued `desired` in deploy.go would previously pass
	// this test undetected.
	if len(rt.published) != 1 {
		t.Fatalf("want one recorded published payload, got %d", len(rt.published))
	}
	if got := rt.published[0]; got.Namespace != "archistrator" || got.Server.Image != "fake/server:test" {
		t.Fatalf("published desired state not assembled: got Namespace=%q Server.Image=%q, want Namespace=%q Server.Image=%q",
			got.Namespace, got.Server.Image, "archistrator", "fake/server:test")
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
	deps.Intervention = &fakeIntervention{directive: intervention.HealthRetry}
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
	// Review finding 2: the Retry directive no longer calls the runtime publish —
	// this Manager has no cached prior desired state to re-derive, and publishing
	// an empty RuntimeDesiredState is a real hazard under the typed contract, not
	// an inert no-op. Retry is observability-only until a real "last published
	// state" concept lands (see reconcile.go's HealthRetry comment).
	if len(rt.publishes) != 0 {
		t.Fatalf("want zero runtime publishes from the Retry directive (no cached state to republish), got %d", len(rt.publishes))
	}
}

// C2: autoscaler Pause (idle) records the head-state transition (reason=autoscale).
// Does NOT call the runtime publish (review finding 2): assembleDesiredState has
// no replica-override input yet, so there is no correct RuntimeDesiredState this
// path could construct today — see autoscaleOne's comment (reconcile.go).
func Test_Reconcile_AutoscalePause_RecordsAutoscaleDecision(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.inFlight = []operatedsystemstate.OperatedSystemSummary{{ID: appID, Version: 1, Status: operatedsystemstate.RuntimeStatusHealthy}}
	rt.health = operatedruntime.RuntimeStatusHealthy // no health transition
	deps.Autoscaler = &fakeAutoscaler{decision: autoscaler.Decision{Kind: autoscaler.DecisionPause}}
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
	if len(rt.publishes) != 0 {
		t.Fatalf("want zero runtime publishes for the idle-pause (no replica-override path yet), got %d", len(rt.publishes))
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
	est := &fakeEstimation{projection: operationestimation.CostProjection{
		CurrentRunRate:       operationestimation.Money{MinorUnits: 1200, Currency: "USD"},
		ProjectedMonthlyCost: operationestimation.Money{MinorUnits: 36000, Currency: "USD"},
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
	deps.Estimation = &fakeEstimation{projection: operationestimation.CostProjection{
		CurrentRunRate: operationestimation.Money{MinorUnits: 4120, Currency: "USD"},
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
	viewAssertComposedContent(t, view, appID)

	// NO MUTATION assertion: the view path performs no write Activity and no version bump.
	viewAssertNoMutation(t, os, rt, us)
}

// viewAssertComposedContent asserts the H1 composed read content: identity, phase +
// in-flight, health snapshot, SLO row, autoscaler mode, and the Engine run-rate.
func viewAssertComposedContent(t *testing.T, view OperatedSystemView, appID uuid.UUID) {
	t.Helper()
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
}

// viewAssertNoMutation asserts the H1 view path performed no write Activity anywhere
// (head-state at version 7, no usage appends, no runtime writes).
func viewAssertNoMutation(t *testing.T, os *fakeOperatedState, rt *fakeRuntime, us *fakeUsage) {
	t.Helper()
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

// H2: an operated app with NO configured autoscaler policy (baseDeps' zero-value
// AutoscalerPolicy — operationsManager has no setter for it; workermanifest.go folds
// m.autoscalerPolicy through unmodified) must report AutoscalerModeUnknown on the
// view, not Auto. Regression for the Task 5 engine-contract retype that flipped the
// façade's own zero-value semantics: this package's façade AutoscalerMode has
// Unknown=0/Auto=1/Manual=2, but the autoscaler engine's own AutoscalerMode has NO
// Unknown value (its zero value IS Auto) — retyping the Manager-held policy straight
// to autoscaler.AutoscalerPolicy silently turned "unconfigured" into "Auto".
func Test_View_UnconfiguredAutoscaler_ReportsUnknown(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, os, rt, us, ar := baseDeps()
	appID := uuid.New()
	os.system = operatedsystemstate.OperatedSystem{ID: appID, Version: 1, Status: operatedsystemstate.RuntimeStatusHealthy}
	os.version = 1
	rt.health = operatedruntime.RuntimeStatusHealthy
	rt.slo = operatedruntime.SloStatus{SloMet: true, Detail: "n/a"}
	// deps.AutoscalerPolicy intentionally left at its zero value (unconfigured).
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
	if view.Autoscaler.Mode != AutoscalerModeUnknown {
		t.Fatalf("autoscaler mode = %v, want Unknown for an unconfigured policy", view.Autoscaler.Mode)
	}
}

// ============================ F. DelinquencyEnforcementWorkflow =============

// F1: the queued signal resumes the branch; pause terms record the delinquency
// action (Paused). No runtime publish yet (review finding 2): a pause needs a
// real scale-to-zero, which assembleDesiredState cannot construct without a
// replica-override input — see runDelinquencyBranch's comment
// (delinquencyenforcement.go).
func Test_Delinquency_PauseTerms_RecordsPaused(t *testing.T) {
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
	if len(rt.publishes) != 0 {
		t.Fatalf("want zero runtime publishes for pause terms (no replica-override path yet), got %d", len(rt.publishes))
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

// ============================ F. assembleDesiredState (deploy.go) ============

// mustJSON marshals v into a bundleManifest-shaped payload for a deployableBundle
// fixture, failing the test on a marshal error (never expected for these literals).
func mustJSON(t *testing.T, v bundleManifest) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return b
}

// projectFixture builds a complete, self-consistent "archistrator" project whose
// cloud deployment environment MIRRORS THE REAL COMMITTED STRUCTURE (verified
// against .aiarch/state/project.json, founder ruling 2026-08-08 correcting the
// original round-1 brief): a namespace declaring ONE workload node (the server)
// plus role-carrying infrastructure nodes — gateway, identityProvider, the
// webapp's static-asset server (role "other", disambiguated from the decoy
// cloud-infra-temporal-alike node below by its relationship to the webapp's
// containerInstance, not by role — D11 trap #1), and three database nodes
// collapsed to one PostgresSpec (D11 trap #2). The webapp's own containerInstance
// legitimately sits on the architect's browser (D11 trap #3) — that subtree is
// NOT a decoy, it is the correct place for it.
//
// testProject (below) is this fixture under the exact name the task brief's given
// test calls; registerActs (above) shares this SAME builder so the pre-existing
// DeployWorkflow tests — which now also invoke assembleDesiredState — keep
// passing without hand-authoring a second fixture.
func projectFixture() projectstate.Project {
	browser := projectstate.DeploymentNode{
		Key:        "cloud-node-browser",
		Name:       "Web browser",
		Technology: "Chrome, Firefox, Safari or Edge",
		ContainerInstances: []projectstate.ContainerInstance{
			{Key: "cloud-ci-spa-browser", ContainerKey: "archistrator-webapp"},
		},
	}
	architectMachine := projectstate.DeploymentNode{
		Key:        "cloud-node-architect-machine",
		Name:       "Architect's computer",
		Technology: "macOS, Windows or Linux",
		Children:   []projectstate.DeploymentNode{browser},
	}

	serverDeployment := projectstate.DeploymentNode{
		Key:        "cloud-node-server-deployment",
		Name:       "server Deployment",
		Technology: "Kubernetes Deployment",
		Instances:  2,
		ContainerInstances: []projectstate.ContainerInstance{
			{Key: "cloud-ci-server", ContainerKey: "archistrator-server"},
		},
	}
	namespace := projectstate.DeploymentNode{
		Key:        "cloud-node-ns-archistrator",
		Name:       "archistrator namespace",
		Technology: "k8s-namespace",
		Children:   []projectstate.DeploymentNode{serverDeployment},
		InfrastructureNodes: []projectstate.InfrastructureNode{
			{Key: "cloud-infra-gateway", Name: "Envoy Gateway", Role: projectstate.RoleGateway},
			{Key: "cloud-infra-keycloak", Name: "Keycloak", Role: projectstate.RoleIdentityProvider},
			{Key: "cloud-infra-static-assets", Name: "Static asset server", Role: projectstate.RoleOther},
			{Key: "cloud-infra-operatedsystemstate", Name: "OperatedSystemState", Role: projectstate.RoleDatabase},
			{Key: "cloud-infra-billingstate", Name: "BillingState", Role: projectstate.RoleDatabase},
			{Key: "cloud-infra-usagelog", Name: "UsageLog", Role: projectstate.RoleDatabase},
		},
	}
	// decoyOtherRoleNode: a SECOND role:"other" node in the SAME namespace as the
	// real static-asset server, with no relationship delivering the webapp
	// container — proves the D11 trap #1 disambiguation is real (relationship,
	// not "the only role:other node I happened to find").
	namespace.InfrastructureNodes = append(namespace.InfrastructureNodes,
		projectstate.InfrastructureNode{Key: "cloud-infra-temporal", Name: "DurableExecutionRuntime", Role: projectstate.RoleOther})
	cluster := projectstate.DeploymentNode{
		Key:        "cloud-node-cluster",
		Name:       "Mixofreality Kubernetes Cluster",
		Technology: "k8s",
		Children:   []projectstate.DeploymentNode{namespace},
	}

	model := &projectstate.DeploymentOperationsModel{
		Deployment: projectstate.DeploymentTopology{
			Environments: []projectstate.DeploymentEnvironment{
				{
					Profile: projectstate.ProfileCloud,
					Title:   "Cloud",
					Nodes:   []projectstate.DeploymentNode{architectMachine, cluster},
					// The relationship is what identifies the static-asset node as the
					// webapp's serving node (D11 trap #1) — role alone is ambiguous.
					Relationships: []projectstate.DeploymentRelationship{
						{From: "cloud-infra-static-assets", To: "cloud-ci-spa-browser", Label: "Delivers the SPA to the architect's browser", Technology: "HTTPS", Mode: projectstate.CallSync},
					},
				},
				{
					Profile: projectstate.ProfileTest,
					Title:   "Test",
				},
			},
		},
	}

	return projectstate.Project{
		ID: "archistrator",
		OperationalConcepts: projectstate.ArtifactSlot{
			Model: model,
		},
	}
}

// testProject is the task brief's named fixture: "a trimmed project with a cloud
// environment". See projectFixture for the node tree it returns.
func testProject(t *testing.T) projectstate.Project {
	t.Helper()
	return projectFixture()
}

// testProjectLocalOnly is a project whose deployment model carries no cloud
// environment (only local) — the model assembleDesiredState must reject.
func testProjectLocalOnly(t *testing.T) projectstate.Project {
	t.Helper()
	model := &projectstate.DeploymentOperationsModel{
		Deployment: projectstate.DeploymentTopology{
			Environments: []projectstate.DeploymentEnvironment{
				{Profile: projectstate.ProfileLocal, Title: "Local"},
				{Profile: projectstate.ProfileTest, Title: "Test"},
			},
		},
	}
	return projectstate.Project{
		ID: "archistrator",
		OperationalConcepts: projectstate.ArtifactSlot{
			Model: model,
		},
	}
}

func TestAssembleDesiredState_MapsCloudEnvironmentAndBundleImages(t *testing.T) {
	proj := testProject(t) // fixture: a trimmed project with a cloud environment
	bundle := deployableBundle{Output: artifact.ConstructionOutput{
		Bytes:    mustJSON(t, bundleManifest{ServerImage: "ghcr.io/mixofreality-studio/archistrator-server:0.8.16", WebAppImage: "ghcr.io/mixofreality-studio/archistrator-webapp:0.6.61"}),
		MIMEType: "application/json",
	}}
	op := operatedsystemstate.OperatedSystem{ID: uuid.New(), Version: 3}

	got, err := assembleDesiredState(proj, bundle, op)
	if err != nil {
		t.Fatalf("assembleDesiredState: %v", err)
	}
	if got.Namespace != "archistrator" {
		t.Errorf("Namespace = %q, want archistrator", got.Namespace)
	}
	if got.Host != "archistrator.capture-gtd.com" {
		t.Errorf("Host = %q, want archistrator.capture-gtd.com", got.Host)
	}
	if got.Server.ModelKey != "cloud-node-server-deployment" {
		t.Errorf("Server.ModelKey = %q, want cloud-node-server-deployment", got.Server.ModelKey)
	}
	if !got.SelfManaged {
		t.Error("archistrator must assemble as SelfManaged")
	}

	// Additional coverage beyond the brief's given assertions, exercising the rest
	// of the fold: image threading, replicas-from-model, webapp/postgres node
	// resolution (D11 role-based selection), and OIDC/ModelKey derivation.
	if got.Server.Image != "ghcr.io/mixofreality-studio/archistrator-server:0.8.16" {
		t.Errorf("Server.Image = %q", got.Server.Image)
	}
	if got.Server.Replicas != 2 {
		t.Errorf("Server.Replicas = %d, want 2 (from the model's declared instances)", got.Server.Replicas)
	}
	// WebApp.ModelKey is the STATIC-ASSET node's key (D11 trap #3), never the
	// browser node's — the browser is where archistrator-webapp's containerInstance
	// legitimately lives, but it must never be colored by cluster health.
	if got.WebApp.ModelKey != "cloud-infra-static-assets" {
		t.Errorf("WebApp.ModelKey = %q, want cloud-infra-static-assets (the serving node, not the browser)", got.WebApp.ModelKey)
	}
	if got.WebApp.Image != "ghcr.io/mixofreality-studio/archistrator-webapp:0.6.61" {
		t.Errorf("WebApp.Image = %q", got.WebApp.Image)
	}
	// Postgres.ModelKey collapses the three database-role nodes to one,
	// deterministically the alphabetically-first key (D11 trap #2).
	if got.Postgres.ModelKey != "cloud-infra-billingstate" || !got.Postgres.Enabled {
		t.Errorf("Postgres = %+v, want ModelKey=cloud-infra-billingstate (alphabetically first of the 3 database nodes) Enabled=true", got.Postgres)
	}
	if got.ModelKey != "cloud-node-ns-archistrator" {
		t.Errorf("ModelKey = %q, want cloud-node-ns-archistrator", got.ModelKey)
	}
	if got.OIDC.ClientID != "archistrator-webapp" {
		t.Errorf("OIDC.ClientID = %q, want archistrator-webapp", got.OIDC.ClientID)
	}
}

func TestAssembleDesiredState_RejectsAModelWithNoCloudEnvironment(t *testing.T) {
	proj := testProjectLocalOnly(t) // fixture with only the local environment
	_, err := assembleDesiredState(proj, deployableBundle{}, operatedsystemstate.OperatedSystem{})
	if err == nil {
		t.Fatal("expected an error when the deployment model has no cloud environment")
	}
}

// namespaceFor is a test helper: the fixture's resolved cluster -> namespace
// path is the same two hops in every mutation test below.
func namespaceFor(model *projectstate.DeploymentOperationsModel) *projectstate.DeploymentNode {
	cluster := &model.Deployment.Environments[0].Nodes[1]
	return &cluster.Children[0]
}

func fixtureBundle(t *testing.T) deployableBundle {
	return deployableBundle{Output: artifact.ConstructionOutput{
		Bytes:    mustJSON(t, bundleManifest{ServerImage: "s:1", WebAppImage: "w:1"}),
		MIMEType: "application/json",
	}}
}

// TestAssembleDesiredState_RejectsAWorkloadNodeWithNoMatchingContainerInstance
// covers the "fail loudly rather than defaulting" requirement directly: a cloud
// environment whose namespace carries no matching server container instance is a
// real misconfiguration, not a silently half-rendered deployment.
func TestAssembleDesiredState_RejectsAWorkloadNodeWithNoMatchingContainerInstance(t *testing.T) {
	proj := testProject(t)
	model := proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	// Drop the server workload node entirely, leaving the namespace with only
	// infrastructure nodes.
	namespaceFor(model).Children = nil

	_, err := assembleDesiredState(proj, fixtureBundle(t), operatedsystemstate.OperatedSystem{})
	if err == nil {
		t.Fatal("expected an error when the model has no server workload node")
	}
}

// TestAssembleDesiredState_RejectsANamespaceThatDisagreesWithTheAppID covers
// review finding 3: the fold must not silently use appName as Namespace while
// ignoring what the resolved namespace node's own key actually says.
func TestAssembleDesiredState_RejectsANamespaceThatDisagreesWithTheAppID(t *testing.T) {
	proj := testProject(t)
	model := proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	// Rename the namespace node so it no longer follows the ns-<appID> convention
	// (e.g. an architect declared a differently-named namespace) — must fail
	// loudly, not silently deploy to "archistrator" anyway.
	namespaceFor(model).Key = "cloud-node-ns-foo"

	_, err := assembleDesiredState(proj, fixtureBundle(t), operatedsystemstate.OperatedSystem{})
	if err == nil {
		t.Fatal("expected an error when the resolved namespace node disagrees with the app id")
	}
}

// TestAssembleDesiredState_RejectsAZeroInstanceWorkload covers review finding 3:
// a workload node declaring 0 instances is a real misconfiguration (fail loudly),
// never a silent scale-to-zero.
func TestAssembleDesiredState_RejectsAZeroInstanceWorkload(t *testing.T) {
	proj := testProject(t)
	model := proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	namespaceFor(model).Children[0].Instances = 0 // server workload

	_, err := assembleDesiredState(proj, fixtureBundle(t), operatedsystemstate.OperatedSystem{})
	if err == nil {
		t.Fatal("expected an error when a workload node declares 0 instances")
	}
}

// TestAssembleDesiredState_RejectsMissingIdentityProvider covers D11: an
// identityProvider-role node is required (an app with no OIDC provider cannot be
// assembled); its absence must fail loudly, not silently omit OIDC.
func TestAssembleDesiredState_RejectsMissingIdentityProvider(t *testing.T) {
	proj := testProject(t)
	model := proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	ns := namespaceFor(model)
	kept := ns.InfrastructureNodes[:0]
	for _, in := range ns.InfrastructureNodes {
		if in.Role != projectstate.RoleIdentityProvider {
			kept = append(kept, in)
		}
	}
	ns.InfrastructureNodes = kept

	_, err := assembleDesiredState(proj, fixtureBundle(t), operatedsystemstate.OperatedSystem{})
	if err == nil {
		t.Fatal("expected an error when the namespace has no identityProvider node")
	}
}

// TestAssembleDesiredState_RejectsMissingDatabaseNodes covers D11: zero
// database-role nodes is a real misconfiguration (which database backs this
// app?), not an omitted-but-tolerated Postgres.
func TestAssembleDesiredState_RejectsMissingDatabaseNodes(t *testing.T) {
	proj := testProject(t)
	model := proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	ns := namespaceFor(model)
	kept := ns.InfrastructureNodes[:0]
	for _, in := range ns.InfrastructureNodes {
		if in.Role != projectstate.RoleDatabase {
			kept = append(kept, in)
		}
	}
	ns.InfrastructureNodes = kept

	_, err := assembleDesiredState(proj, fixtureBundle(t), operatedsystemstate.OperatedSystem{})
	if err == nil {
		t.Fatal("expected an error when the namespace has no database node")
	}
}

// TestAssembleDesiredState_RejectsAWebAppWithNoDeliveringRelationship covers
// D11 trap #1 directly: role:"other" alone is ambiguous (the fixture's decoy
// cloud-infra-temporal-alike node shares it) — with the relationship removed,
// nothing identifies a serving node, and the fold must fail loudly rather than
// guess between the two role:"other" candidates.
func TestAssembleDesiredState_RejectsAWebAppWithNoDeliveringRelationship(t *testing.T) {
	proj := testProject(t)
	model := proj.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	model.Deployment.Environments[0].Relationships = nil

	_, err := assembleDesiredState(proj, fixtureBundle(t), operatedsystemstate.OperatedSystem{})
	if err == nil {
		t.Fatal("expected an error when no relationship identifies the webapp's serving node")
	}
}

// TestAssembleDesiredState_AgainstTheRealCommittedModel is the round-2 check:
// assembleDesiredState must SUCCEED against archistrator's own real, committed
// .aiarch/state/project.json cloud environment — not a hand-authored fixture.
// The founder's round-1 framing ("the model lacks a webapp/postgres node") was
// wrong; the model DOES describe the deployment, via infrastructureNodes +
// role, not containerInstances. This test is the proof.
func TestAssembleDesiredState_AgainstTheRealCommittedModel(t *testing.T) {
	proj := realArchistratorProject(t)
	bundle := deployableBundle{Output: artifact.ConstructionOutput{
		Bytes:    mustJSON(t, bundleManifest{ServerImage: "ghcr.io/mixofreality-studio/archistrator-server:0.8.16", WebAppImage: "ghcr.io/mixofreality-studio/archistrator-webapp:0.6.61"}),
		MIMEType: "application/json",
	}}
	op := operatedsystemstate.OperatedSystem{ID: uuid.New(), Version: 1}

	got, err := assembleDesiredState(proj, bundle, op)
	if err != nil {
		t.Fatalf("assembleDesiredState against the real committed model: %v", err)
	}

	if got.AppName != "archistrator" || got.Namespace != "archistrator" {
		t.Errorf("AppName/Namespace = %q/%q, want archistrator/archistrator", got.AppName, got.Namespace)
	}
	if got.ModelKey != "cloud-node-ns-archistrator" {
		t.Errorf("ModelKey = %q, want cloud-node-ns-archistrator", got.ModelKey)
	}
	if !got.SelfManaged {
		t.Error("archistrator's own project must assemble as SelfManaged")
	}
	if got.Server.ModelKey != "cloud-node-server-deployment" || got.Server.Replicas != 2 {
		t.Errorf("Server = %+v, want ModelKey=cloud-node-server-deployment Replicas=2", got.Server)
	}
	if got.WebApp.ModelKey != "cloud-infra-static-assets" {
		t.Errorf("WebApp.ModelKey = %q, want cloud-infra-static-assets", got.WebApp.ModelKey)
	}
	wantDBKeys := map[string]bool{"cloud-infra-operatedsystemstate": true, "cloud-infra-billingstate": true, "cloud-infra-usagelog": true}
	if !wantDBKeys[got.Postgres.ModelKey] || !got.Postgres.Enabled {
		t.Errorf("Postgres = %+v, want ModelKey in %v Enabled=true", got.Postgres, wantDBKeys)
	}
	if got.OIDC.Issuer == "" || got.OIDC.ClientID == "" {
		t.Errorf("OIDC = %+v, want both Issuer and ClientID populated", got.OIDC)
	}
}

// realArchistratorProject reads the real, committed .aiarch/state/project.json
// straight off disk and decodes just enough of it (the operational-concepts
// slot) to exercise assembleDesiredState against reality, without pulling in
// the full projectstate.GitStore machinery (an on-disk git worktree read this
// test doesn't need). Deliberately narrow: only proj.ID and
// proj.OperationalConcepts.Model are populated — the two fields
// assembleDesiredState reads.
func realArchistratorProject(t *testing.T) projectstate.Project {
	t.Helper()
	// server/internal/manager/operations -> server -> repo root -> .aiarch/state/project.json
	path := filepath.Join("..", "..", "..", "..", ".aiarch", "state", "project.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc struct {
		ID    string `json:"id"`
		Slots map[string]struct {
			Model struct {
				Deployment json.RawMessage `json:"deployment"`
			} `json:"model"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	slot6, ok := doc.Slots["6"]
	if !ok {
		t.Fatalf("%s has no slot 6 (operational concepts)", path)
	}
	var topology projectstate.DeploymentTopology
	if err := json.Unmarshal(slot6.Model.Deployment, &topology); err != nil {
		t.Fatalf("unmarshal slot 6 deployment topology: %v", err)
	}
	return projectstate.Project{
		ID: projectstate.ProjectID(doc.ID),
		OperationalConcepts: projectstate.ArtifactSlot{
			Model: &projectstate.DeploymentOperationsModel{Deployment: topology},
		},
	}
}
