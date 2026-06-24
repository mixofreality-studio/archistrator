package billing

import (
	"context"
	"sync"
	"testing"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// =============================================================================
// billingManager workflow unit tests over the Temporal in-memory test environment
// (testsuite.WorkflowTestSuite). The two Engines (billingEngine, interventionEngine)
// and the five ResourceAccess ports are constructed as interface test doubles (fakes)
// against the frozen consumer interfaces in deps.go. Runs with no Docker or cluster.
//
// STP map in manager_test.go.
// =============================================================================

// ---- Fakes (interface test doubles) -----------------------------------------

// fakeBillingState records head-state transitions + serves scripted state.
// Satisfies BillingStateAccess (deps.go).
type fakeBillingState struct {
	mu sync.Mutex

	aggregate BillingAggregate
	notFound  bool

	// openConflictFirst, when >0, returns fwra.Conflict on the first N
	// OpenBillingAggregate calls — drives the §6.5 re-read→re-apply loop.
	openConflictFirst int

	openCalls   int
	recordCalls []RecordServiceInvoiceArgs
	version     Version
}

func (f *fakeBillingState) bump() Version {
	f.version++
	f.aggregate.Version = f.version
	return f.version
}

func (f *fakeBillingState) ReadBillingAggregate(_ context.Context, customerID CustomerID) (BillingAggregate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return BillingAggregate{}, fwra.New(fwra.NotFound, "no row")
	}
	out := f.aggregate
	out.ID = customerID
	return out, nil
}

func (f *fakeBillingState) ReadDelinquentCustomers(_ context.Context) ([]DelinquentCustomerSeam, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return nil, nil
}

func (f *fakeBillingState) OpenBillingAggregate(_ context.Context, _ CustomerID, _ Version, _ fwra.IdempotencyKey) (Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openCalls++
	if f.openConflictFirst > 0 {
		f.openConflictFirst--
		f.version++ // simulated racing mutation
		f.aggregate.Version = f.version
		return 0, fwra.New(fwra.Conflict, "stale version")
	}
	return f.bump(), nil
}

func (f *fakeBillingState) RecordServiceInvoice(_ context.Context, _ CustomerID, _ Version, periodID PeriodID, invoice ServiceInvoiceSeam, charged bool, _ fwra.IdempotencyKey) (Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordCalls = append(f.recordCalls, RecordServiceInvoiceArgs{
		PeriodID: periodID,
		Invoice:  invoice,
		Charged:  charged,
	})
	return f.bump(), nil
}

var _ BillingStateAccess = (*fakeBillingState)(nil)

// fakeDelinquentState wraps fakeBillingState with a configurable delinquent list —
// used by the retry-sweep tests only.
type fakeDelinquentState struct {
	*fakeBillingState
	delinquent []DelinquentCustomerSeam
}

func (f *fakeDelinquentState) ReadDelinquentCustomers(_ context.Context) ([]DelinquentCustomerSeam, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.delinquent, nil
}

var _ BillingStateAccess = (*fakeDelinquentState)(nil)

// fakeUsage serves a scripted PeriodUsageSeam.
type fakeUsage struct {
	usage PeriodUsageSeam
}

func (u *fakeUsage) ReadPeriodUsage(_ context.Context, _ CustomerID, _ PeriodID) (PeriodUsageSeam, error) {
	return u.usage, nil
}

var _ UsageAccess = (*fakeUsage)(nil)

// fakeBillingGateway records gateway calls; declineChargeFirst makes ChargeUser fail
// terminally (fwra.ContentPolicy) for the first declineChargeFirst calls.
type fakeBillingGateway struct {
	mu sync.Mutex

	declineChargeFirst int
	validateCalled     int
	charges            []ChargeUserArgs
}

func (g *fakeBillingGateway) ValidateStoredInstrument(_ context.Context, _ CustomerID, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.validateCalled++
	return nil
}

func (g *fakeBillingGateway) ChargeUser(_ context.Context, customerID CustomerID, amount Money, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.declineChargeFirst > 0 {
		g.declineChargeFirst--
		return fwra.New(fwra.ContentPolicy, "card declined")
	}
	g.charges = append(g.charges, ChargeUserArgs{CustomerID: customerID, Amount: amount})
	return nil
}

var _ BillingGatewayAccess = (*fakeBillingGateway)(nil)

// fakeSourceCtrl records app-installation confirmations; notInstalled surfaces NotFound.
type fakeSourceCtrl struct {
	mu           sync.Mutex
	notInstalled bool
	confirmed    int
}

func (s *fakeSourceCtrl) ConfirmAppInstallation(_ context.Context, _ CustomerID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notInstalled {
		return fwra.New(fwra.NotFound, "app not installed")
	}
	s.confirmed++
	return nil
}

var _ SourceControlAccess = (*fakeSourceCtrl)(nil)

// fakeDurable records delivered signals + registered schedules.
type fakeDurable struct {
	mu        sync.Mutex
	signals   []deliverSignalPayload
	schedules []string
}

func (d *fakeDurable) DeliverSignal(_ context.Context, _ string, _ string, payload deliverSignalPayload) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.signals = append(d.signals, payload)
	return nil
}

func (d *fakeDurable) RegisterSchedule(_ context.Context, spec scheduleSpec) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.schedules = append(d.schedules, spec.ID)
	return nil
}

var _ DurableExecutionAccess = (*fakeDurable)(nil)

// fakeBillingEngine returns a scripted ServiceInvoiceSeam from PriceUsage.
type fakeBillingEngine struct {
	invoice ServiceInvoiceSeam
	callN   int
}

func (e *fakeBillingEngine) PriceUsage(_ PeriodUsageSeam, _ ServicePricingSeam) (ServiceInvoiceSeam, error) {
	e.callN++
	return e.invoice, nil
}

var _ BillingEngine = (*fakeBillingEngine)(nil)

// fakeIntervention returns a scripted billing-failure directive.
type fakeIntervention struct {
	directive BillingFailureDirectiveSeam
}

func (i *fakeIntervention) DecideOnBillingFailure(_ BillingFailureSeam) (BillingFailureDirectiveSeam, error) {
	return i.directive, nil
}

var _ InterventionEngine = (*fakeIntervention)(nil)

// ---- helpers ----------------------------------------------------------------

type billingFakes struct {
	state   *fakeBillingState
	usage   *fakeUsage
	gateway *fakeBillingGateway
	sc      *fakeSourceCtrl
	durable *fakeDurable
	engine  *fakeBillingEngine
	interv  *fakeIntervention
}

func baseBillingDeps() (Deps, *billingFakes) {
	f := &billingFakes{
		state:   &fakeBillingState{},
		usage:   &fakeUsage{},
		gateway: &fakeBillingGateway{},
		sc:      &fakeSourceCtrl{},
		durable: &fakeDurable{},
		engine:  &fakeBillingEngine{},
		interv:  &fakeIntervention{directive: BillingFailureEscalate},
	}
	return Deps{
		Billing:      f.engine,
		Intervention: f.interv,
		BillingState: f.state,
		Usage:        f.usage,
		Gateway:      f.gateway,
		SourceCtrl:   f.sc,
		Durable:      f.durable,
	}, f
}

func registerBillingRegister(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.RegisterCustomerWorkflow, workflow.RegisterOptions{Name: ExecutionKindRegister})
	env.RegisterActivity(wf.ValidateStoredInstrumentActivity)
	env.RegisterActivity(wf.ConfirmAppInstallationActivity)
	env.RegisterActivity(wf.ReadBillingAggregateActivity)
	env.RegisterActivity(wf.OpenBillingAggregateActivity)
	env.RegisterActivity(wf.RegisterClosePeriodScheduleActivity)
}

func registerBillingClose(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.CloseBillingPeriodWorkflow, workflow.RegisterOptions{Name: ExecutionKindClosePeriod})
	env.RegisterActivity(wf.ReadBillingAggregateActivity)
	env.RegisterActivity(wf.ReadPeriodUsageActivity)
	env.RegisterActivity(wf.ChargeUserActivity)
	env.RegisterActivity(wf.RecordServiceInvoiceActivity)
}

func registerBillingSweep(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.RunBillingRetrySweepWorkflow, workflow.RegisterOptions{Name: ExecutionKindRetrySweep})
	env.RegisterActivity(wf.ReadDelinquentCustomersActivity)
	env.RegisterActivity(wf.ChargeUserActivity)
	env.RegisterActivity(wf.DeliverDelinquencySignalActivity)
}

func usdBilling(minor int64) Money { return Money{MinorUnits: minor, Currency: "USD"} }

// ============================ B. RegisterCustomerWorkflow ====================

// B1: happy path: validate instrument → confirm GitHub App → open aggregate →
// register per-customer close-period Schedule.
func Test_BillingRegister_HappyPath(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseBillingDeps()
	cid := uuid.New()
	wf := newWorkflows(deps)
	registerBillingRegister(env, wf)

	env.ExecuteWorkflow(ExecutionKindRegister, RegisterCustomerInput{CustomerID: cid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var ref BillingRef
	if err := env.GetWorkflowResult(&ref); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if ref.CustomerID != cid {
		t.Fatalf("want customerID %s in result, got %s", cid, ref.CustomerID)
	}
	if f.gateway.validateCalled != 1 {
		t.Fatalf("want one validateStoredInstrument, got %d", f.gateway.validateCalled)
	}
	if f.sc.confirmed != 1 {
		t.Fatalf("want one confirmAppInstallation, got %d", f.sc.confirmed)
	}
	if f.state.openCalls != 1 {
		t.Fatalf("want one openBillingAggregate, got %d", f.state.openCalls)
	}
	if len(f.durable.schedules) != 1 {
		t.Fatalf("want one registered close-period Schedule, got %d", len(f.durable.schedules))
	}
}

// B2: GitHub App not installed → FailedPrecondition; no aggregate opened.
func Test_BillingRegister_AppNotInstalled_FailedPrecondition(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseBillingDeps()
	f.sc.notInstalled = true
	wf := newWorkflows(deps)
	registerBillingRegister(env, wf)

	env.ExecuteWorkflow(ExecutionKindRegister, RegisterCustomerInput{CustomerID: uuid.New()})

	if env.GetWorkflowError() == nil {
		t.Fatal("want FailedPrecondition error when GitHub App is not installed")
	}
	if f.state.openCalls != 0 {
		t.Fatalf("no aggregate must be opened on failed pre-condition, got %d opens", f.state.openCalls)
	}
}

// ============================ C. CloseBillingPeriodWorkflow ==================

// C1: happy path: read aggregate → read usage → price → charge → record (Charged=true).
func Test_BillingClose_HappyPath_Charged(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseBillingDeps()
	cid := uuid.New()
	f.state.aggregate = BillingAggregate{ID: cid, Version: 1, Registered: true}
	f.engine.invoice = ServiceInvoiceSeam{ServiceInvoiceAmount: usdBilling(1000)}
	wf := newWorkflows(deps)
	registerBillingClose(env, wf)

	env.ExecuteWorkflow(ExecutionKindClosePeriod, CloseBillingPeriodInput{
		CustomerID: cid, PeriodID: "2026-05",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result CloseBillingPeriodResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.Charged {
		t.Fatal("want Charged=true for a non-zero invoice")
	}
	if result.PeriodID != "2026-05" {
		t.Fatalf("want periodId 2026-05, got %s", result.PeriodID)
	}
	if len(f.gateway.charges) != 1 || f.gateway.charges[0].Amount.MinorUnits != 1000 {
		t.Fatalf("want one charge of 1000, got %v", f.gateway.charges)
	}
	if len(f.state.recordCalls) != 1 || !f.state.recordCalls[0].Charged {
		t.Fatalf("want one recordServiceInvoice(Charged=true), got %v", f.state.recordCalls)
	}
}

// C2: zero invoice (no usage) → Charged=false, no charge call.
func Test_BillingClose_ZeroInvoice_NotCharged(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseBillingDeps()
	cid := uuid.New()
	f.state.aggregate = BillingAggregate{ID: cid, Version: 1, Registered: true}
	f.engine.invoice = ServiceInvoiceSeam{ServiceInvoiceAmount: usdBilling(0)}
	wf := newWorkflows(deps)
	registerBillingClose(env, wf)

	env.ExecuteWorkflow(ExecutionKindClosePeriod, CloseBillingPeriodInput{
		CustomerID: cid, PeriodID: "2026-05",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result CloseBillingPeriodResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Charged {
		t.Fatal("want Charged=false for zero invoice")
	}
	if len(f.gateway.charges) != 0 {
		t.Fatalf("want no charge for zero invoice, got %v", f.gateway.charges)
	}
	if len(f.state.recordCalls) != 1 || f.state.recordCalls[0].Charged {
		t.Fatalf("want one recordServiceInvoice(Charged=false), got %v", f.state.recordCalls)
	}
}

// C3: period-already-closed guard (P3) — idempotent close returns the recorded result
// without re-pricing or re-charging.
func Test_BillingClose_PeriodAlreadyClosed_IdempotentReturn(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseBillingDeps()
	cid := uuid.New()
	f.state.aggregate = BillingAggregate{
		ID: cid, Version: 2, Registered: true,
		ClosedPeriods: []ClosedPeriodSeam{{PeriodID: "2026-05", Charged: true}},
	}
	wf := newWorkflows(deps)
	registerBillingClose(env, wf)

	env.ExecuteWorkflow(ExecutionKindClosePeriod, CloseBillingPeriodInput{
		CustomerID: cid, PeriodID: "2026-05",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result CloseBillingPeriodResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.Charged {
		t.Fatal("P3 must reflect the already-recorded Charged=true")
	}
	// no pricing, no charge, no second record
	if f.engine.callN != 0 {
		t.Fatalf("P3 must not call PriceUsage, called %d times", f.engine.callN)
	}
	if len(f.gateway.charges) != 0 {
		t.Fatalf("P3 must not charge, got %v", f.gateway.charges)
	}
	if len(f.state.recordCalls) != 0 {
		t.Fatalf("P3 must not record again, got %v", f.state.recordCalls)
	}
}

// C4: charge declines (ContentPolicy) → DecideOnBillingFailure(Escalate) →
// Charged=false, invoice recorded (left for retry sweep).
func Test_BillingClose_ChargeDecline_Escalate_NotCharged(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseBillingDeps()
	cid := uuid.New()
	f.state.aggregate = BillingAggregate{ID: cid, Version: 1, Registered: true}
	f.engine.invoice = ServiceInvoiceSeam{ServiceInvoiceAmount: usdBilling(800)}
	f.gateway.declineChargeFirst = 99 // always declines
	f.interv.directive = BillingFailureEscalate
	wf := newWorkflows(deps)
	registerBillingClose(env, wf)

	env.ExecuteWorkflow(ExecutionKindClosePeriod, CloseBillingPeriodInput{
		CustomerID: cid, PeriodID: "2026-06",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result CloseBillingPeriodResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Charged {
		t.Fatal("want Charged=false when all charge attempts decline")
	}
	if len(f.gateway.charges) != 0 {
		t.Fatalf("a declined charge must not succeed, got %v", f.gateway.charges)
	}
	if len(f.state.recordCalls) != 1 || f.state.recordCalls[0].Charged {
		t.Fatalf("want one recordServiceInvoice(Charged=false), got %v", f.state.recordCalls)
	}
}

// ============================ D. RunBillingRetrySweepWorkflow ================

// D1: delinquent customers → re-charge declines → deliver applyDelinquencyPolicy signal.
func Test_BillingSweep_DelinquentCustomer_SignalDelivered(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	cid := uuid.New()
	base := &fakeBillingState{}
	delinqState := &fakeDelinquentState{
		fakeBillingState: base,
		delinquent: []DelinquentCustomerSeam{
			{CustomerID: cid, PeriodID: "2026-05", Amount: usdBilling(500)},
		},
	}
	f := &billingFakes{
		state:   base,
		usage:   &fakeUsage{},
		gateway: &fakeBillingGateway{declineChargeFirst: 99},
		sc:      &fakeSourceCtrl{},
		durable: &fakeDurable{},
		engine:  &fakeBillingEngine{},
		interv:  &fakeIntervention{directive: BillingFailureEscalate},
	}
	deps := Deps{
		Billing:      f.engine,
		Intervention: f.interv,
		BillingState: delinqState,
		Usage:        f.usage,
		Gateway:      f.gateway,
		SourceCtrl:   f.sc,
		Durable:      f.durable,
	}
	wf := newWorkflows(deps)
	registerBillingSweep(env, wf)

	env.ExecuteWorkflow(ExecutionKindRetrySweep, RunBillingRetrySweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result BillingRetrySweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.SignalledCustomers) != 1 || result.SignalledCustomers[0] != cid {
		t.Fatalf("want one signalled customer %s, got %v", cid, result.SignalledCustomers)
	}
	if len(f.durable.signals) != 1 || !f.durable.signals[0].PauseNotWithdraw {
		t.Fatalf("want one applyDelinquencyPolicy signal with PauseNotWithdraw=true, got %v", f.durable.signals)
	}
}

// D2: quiet sweep (no delinquent customers) → empty SignalledCustomers, no signals.
func Test_BillingSweep_NoDelinquents_Quiet(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	base := &fakeBillingState{}
	delinqState := &fakeDelinquentState{fakeBillingState: base, delinquent: nil}
	f := &billingFakes{
		state:   base,
		usage:   &fakeUsage{},
		gateway: &fakeBillingGateway{},
		sc:      &fakeSourceCtrl{},
		durable: &fakeDurable{},
		engine:  &fakeBillingEngine{},
		interv:  &fakeIntervention{},
	}
	deps := Deps{
		Billing:      f.engine,
		Intervention: f.interv,
		BillingState: delinqState,
		Usage:        f.usage,
		Gateway:      f.gateway,
		SourceCtrl:   f.sc,
		Durable:      f.durable,
	}
	wf := newWorkflows(deps)
	registerBillingSweep(env, wf)

	env.ExecuteWorkflow(ExecutionKindRetrySweep, RunBillingRetrySweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result BillingRetrySweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.SignalledCustomers) != 0 {
		t.Fatalf("quiet sweep must signal nobody, got %v", result.SignalledCustomers)
	}
	if len(f.durable.signals) != 0 {
		t.Fatalf("quiet sweep must deliver no signals, got %d", len(f.durable.signals))
	}
}

// ============================ E. §6.5 Conflict loop ==========================

// E1: OpenBillingAggregateActivity returns Conflict twice → applyRecovering
// re-reads the version and re-applies; converges to exactly one opened aggregate.
func Test_BillingRegister_OpenConflict_ConvergesOnce(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseBillingDeps()
	f.state.openConflictFirst = 2 // first two calls Conflict; third succeeds
	wf := newWorkflows(deps)
	registerBillingRegister(env, wf)

	env.ExecuteWorkflow(ExecutionKindRegister, RegisterCustomerInput{CustomerID: uuid.New()})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// The aggregate must be opened exactly once in aggregate terms (one domain state
	// change) despite the intermediate conflicts.
	if f.state.openCalls != 3 {
		// 2 conflicts + 1 success = 3 raw calls, but the domain state is "opened once"
		t.Fatalf("want 3 raw OpenBillingAggregate calls (2 conflicts + 1 success), got %d", f.state.openCalls)
	}
	if len(f.durable.schedules) != 1 {
		t.Fatalf("want one Schedule registered after conflict convergence, got %d", len(f.durable.schedules))
	}
}
