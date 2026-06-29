package settlement

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// =============================================================================
// settlementManager workflow unit tests over the Temporal in-memory test environment
// (testsuite.WorkflowTestSuite). The two Engines (settlementEngine, interventionEngine)
// and the six ResourceAccess ports (settlementStateAccess, revenueLedgerAccess,
// usageAccess, merchantGatewayAccess, operatedRuntimeAccess, durableExecutionAccess) are
// constructed as interface test doubles (fakes) — the not-yet-built deps are driven
// against their FROZEN CONTRACTS as the Manager-declared consumer interfaces (deps.go).
// These run with no Docker and no dev server.
//
// They assert the money spine (compute → route → record), exact-money invariants, the
// OQ-4 charge-failure decide→execute branch, the forward-only chargeback recompute saga
// and its idempotent ledger appends, the queued delinquency signal to operations, and
// the §6.5 Conflict re-read loop on the money-affecting settleCycle write — per
// [[the-method-testing]] (black-box where the observable is the workflow result /
// recorded side effects). STP map in manager_test.go.
// =============================================================================

// ---- Fakes (interface test doubles for the downstream deps) -----------------

// fakeSettlementState records the head-state transition calls + serves scripted state.
// Satisfies SettlementStateAccess (deps.go).
type fakeSettlementState struct {
	mu sync.Mutex

	settlement settlementHead
	delinquent []customerSummary
	notFound   bool

	// settleConflictFirst, when >0, returns fwra.Conflict on the first N settleCycle
	// calls before succeeding — drives the §6.5 re-read→re-apply loop on the
	// money-affecting write.
	settleConflictFirst int

	registered []customerID
	bound      []customerID
	settled    []settlementOutcomeSeam
	resettled  []settlementOutcomeSeam
	readN      int
	version    version
}

func (f *fakeSettlementState) ReadSettlement(_ context.Context, _ customerID) (settlementHead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readN++
	if f.notFound {
		return settlementHead{}, fwra.New(fwra.NotFound, "no row")
	}
	return f.settlement, nil
}

func (f *fakeSettlementState) ReadPersistentlyDelinquentCustomers(_ context.Context, _ delinquencyScope) ([]customerSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.delinquent, nil
}

func (f *fakeSettlementState) bump() version {
	f.version++
	f.settlement.Version = f.version
	return f.version
}

func (f *fakeSettlementState) RegisterCustomer(_ context.Context, c customerID, _ version, _ customerProfileSeam, _ fwra.IdempotencyKey) (version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = append(f.registered, c)
	return f.bump(), nil
}

func (f *fakeSettlementState) BindGatewayLive(_ context.Context, c customerID, _ version, _ gatewayBindingSeam, _ fwra.IdempotencyKey) (version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bound = append(f.bound, c)
	return f.bump(), nil
}

func (f *fakeSettlementState) SettleCycle(_ context.Context, _ customerID, _ version, _ cycleID, outcome settlementOutcomeSeam, _ fwra.IdempotencyKey) (version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.settleConflictFirst > 0 {
		f.settleConflictFirst--
		f.version++ // a racing mutation advanced the version
		f.settlement.Version = f.version
		return 0, fwra.New(fwra.Conflict, "stale version")
	}
	f.settled = append(f.settled, outcome)
	return f.bump(), nil
}

func (f *fakeSettlementState) ResettleCycle(_ context.Context, _ customerID, _ version, _ cycleID, correction settlementOutcomeSeam, _ fwra.IdempotencyKey) (version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resettled = append(f.resettled, correction)
	return f.bump(), nil
}

var _ settlementStateAccess = (*fakeSettlementState)(nil)

// fakeRevenueLedger records appends + serves a scripted range.
type fakeRevenueLedger struct {
	mu sync.Mutex

	rangeEntries []revenueEntrySeam
	inbound      []revenueEntrySeam
	reversals    []reversalEntrySeam
}

func (r *fakeRevenueLedger) RecordInboundRevenue(_ context.Context, entry revenueEntrySeam) (entryRefSeam, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inbound = append(r.inbound, entry)
	r.rangeEntries = append(r.rangeEntries, entry)
	return entryRefSeam("ref"), nil
}

func (r *fakeRevenueLedger) RecordReversal(_ context.Context, reversal reversalEntrySeam) (entryRefSeam, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reversals = append(r.reversals, reversal)
	// A reversal is a new negative fact appended to the same log readRange replays.
	r.rangeEntries = append(r.rangeEntries, revenueEntrySeam{
		CustomerID: reversal.CustomerID, CycleID: reversal.CycleID,
		Kind: revenueKindReversal, Amount: reversal.Amount, GatewayEventID: reversal.GatewayEventID,
	})
	return entryRefSeam("revref"), nil
}

func (r *fakeRevenueLedger) ReadRange(_ context.Context, _ customerID, _ cycleID) ([]revenueEntrySeam, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]revenueEntrySeam, len(r.rangeEntries))
	copy(out, r.rangeEntries)
	return out, nil
}

var _ revenueLedgerAccess = (*fakeRevenueLedger)(nil)

// fakeUsage serves a scripted usage range.
type fakeUsage struct {
	rangeEvents []usageEventSeam
}

func (u *fakeUsage) ReadRange(_ context.Context, _ usageRangeQuerySeam) ([]usageEventSeam, error) {
	return u.rangeEvents, nil
}

var _ usageAccess = (*fakeUsage)(nil)

// fakeGateway records money moves; declineCharge makes ChargeCustomer fail terminally
// (RA Auth) the first declineChargeFirst times.
type fakeGateway struct {
	mu sync.Mutex

	declineChargeFirst int

	payouts   []Money
	charges   []Money
	created   int
	validated int
}

func (g *fakeGateway) PayoutCustomer(_ context.Context, _ customerID, amount Money, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.payouts = append(g.payouts, amount)
	return nil
}

func (g *fakeGateway) ChargeCustomer(_ context.Context, _ customerID, amount Money, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.declineChargeFirst > 0 {
		g.declineChargeFirst--
		return fwra.New(fwra.Auth, "card declined")
	}
	g.charges = append(g.charges, amount)
	return nil
}

func (g *fakeGateway) CreateConnectedAccount(_ context.Context, _ customerID, _ string) (gatewayBindingSeam, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.created++
	return gatewayBindingSeam{ConnectedAccountID: "acct-1"}, nil
}

func (g *fakeGateway) ValidateStoredInstrument(_ context.Context, _ customerID, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.validated++
	return nil
}

var _ merchantGatewayAccess = (*fakeGateway)(nil)

// fakeRuntime records wirePaymentConfig calls.
type fakeRuntime struct {
	wired int
}

func (r *fakeRuntime) WirePaymentConfig(_ context.Context, _ deployedAppID, _ gatewayBindingSeam, _ fwra.IdempotencyKey) error {
	r.wired++
	return nil
}

var _ operatedRuntimeAccess = (*fakeRuntime)(nil)

// fakeDurable records delivered signals + registered schedules.
type fakeDurable struct {
	mu sync.Mutex

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

var _ durableExecutionAccess = (*fakeDurable)(nil)

// fakeSettlementEngine returns a scripted SettlementResult for compute + recompute.
type fakeSettlementEngine struct {
	computeResult   settlementResultSeam
	recomputeResult settlementResultSeam
	computeN        int
	recomputeN      int
}

func (e *fakeSettlementEngine) ComputeNet(_ cycleRevenueSeam, _ cycleUsageSeam, _ settlementTermsSeam) (settlementResultSeam, error) {
	e.computeN++
	return e.computeResult, nil
}

func (e *fakeSettlementEngine) RecomputeNet(_ reSettlementInputSeam) (settlementResultSeam, error) {
	e.recomputeN++
	return e.recomputeResult, nil
}

var _ settlementEngine = (*fakeSettlementEngine)(nil)

// fakeIntervention returns a scripted settlement-failure directive.
type fakeIntervention struct {
	directive settlementFailureDirectiveSeam
}

func (i *fakeIntervention) DecideOnSettlementFailure(_ settlementFailureSeam) (settlementFailureDirectiveSeam, error) {
	return i.directive, nil
}

var _ interventionEngine = (*fakeIntervention)(nil)

// ---- helpers ----------------------------------------------------------------

type fakes struct {
	state   *fakeSettlementState
	ledger  *fakeRevenueLedger
	usage   *fakeUsage
	gateway *fakeGateway
	runtime *fakeRuntime
	durable *fakeDurable
	engine  *fakeSettlementEngine
	interv  *fakeIntervention
}

func baseDeps() (wfDeps, *fakes) {
	f := &fakes{
		state:   &fakeSettlementState{},
		ledger:  &fakeRevenueLedger{},
		usage:   &fakeUsage{},
		gateway: &fakeGateway{},
		runtime: &fakeRuntime{},
		durable: &fakeDurable{},
		engine:  &fakeSettlementEngine{},
		interv:  &fakeIntervention{directive: settlementRetry},
	}
	return wfDeps{
		Settlement:      f.engine,
		Intervention:    f.interv,
		SettlementState: f.state,
		RevenueLedger:   f.ledger,
		Usage:           f.usage,
		Gateway:         f.gateway,
		OperatedRuntime: f.runtime,
		Durable:         f.durable,
	}, f
}

func registerOnboard(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.OnboardWorkflow, workflow.RegisterOptions{Name: executionKindOnboard})
	env.RegisterActivity(wf.ReadSettlementActivity)
	env.RegisterActivity(wf.CreateConnectedAccountActivity)
	env.RegisterActivity(wf.WirePaymentConfigActivity)
	env.RegisterActivity(wf.BindGatewayLiveActivity)
	env.RegisterActivity(wf.RegisterScheduleActivity)
}

func registerRegister(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.RegisterCustomerWorkflow, workflow.RegisterOptions{Name: executionKindRegister})
	env.RegisterActivity(wf.ValidateStoredInstrumentActivity)
	env.RegisterActivity(wf.RegisterCustomerActivity)
}

func registerClose(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.CloseCycleWorkflow, workflow.RegisterOptions{Name: executionKindClose})
	env.RegisterActivity(wf.ReadSettlementActivity)
	env.RegisterActivity(wf.ReadRevenueRangeActivity)
	env.RegisterActivity(wf.ReadUsageRangeActivity)
	env.RegisterActivity(wf.PayoutCustomerActivity)
	env.RegisterActivity(wf.ChargeCustomerActivity)
	env.RegisterActivity(wf.SettleCycleActivity)
	env.RegisterActivity(wf.ResettleCycleActivity)
	env.RegisterActivity(wf.RecordInboundRevenueActivity)
	env.RegisterActivity(wf.RecordReversalActivity)
}

func registerSweep(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.ShortfallSweepWorkflow, workflow.RegisterOptions{Name: executionKindShortfallSweep})
	env.RegisterActivity(wf.ReadDelinquentActivity)
	env.RegisterActivity(wf.DeliverDelinquencySignalActivity)
}

// boundSettlement returns a registered + gateway-bound settlement at the given version.
func boundSettlement(id customerID, version version) settlementHead {
	return settlementHead{ID: id, Version: version, Registered: true, GatewayBound: true}
}

func usd(minor int64) Money { return Money{MinorUnits: minor, Currency: "USD"} }

// ============================ B. OnboardWorkflow =============================

// B1: happy path resolves the customer, creates the connected account, wires the
// runtime, binds the gateway, and registers the per-customer cycle Schedule.
func Test_Onboard_HappyPath(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = settlementHead{ID: cid, Version: 2}
	wf := newWorkflows(deps)
	registerOnboard(env, wf)

	env.ExecuteWorkflow(executionKindOnboard, onboardInput{DeployedAppID: uuid.New()})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var ref SettlementRef
	if err := env.GetWorkflowResult(&ref); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ref.CustomerID != cid {
		t.Fatalf("want resolved customerId %s, got %s", cid, ref.CustomerID)
	}
	if f.gateway.created != 1 {
		t.Fatalf("want one connected account, got %d", f.gateway.created)
	}
	if f.runtime.wired != 1 {
		t.Fatalf("want one runtime wire, got %d", f.runtime.wired)
	}
	if len(f.state.bound) != 1 {
		t.Fatalf("want one bindGatewayLive, got %d", len(f.state.bound))
	}
	if len(f.durable.schedules) != 1 {
		t.Fatalf("want one registered cycle Schedule, got %d", len(f.durable.schedules))
	}
}

// B2: a missing settlement aggregate (read NotFound) fails the pre-condition; no money
// move happens.
func Test_Onboard_NoAggregate_FailedPrecondition(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	f.state.notFound = true
	wf := newWorkflows(deps)
	registerOnboard(env, wf)

	env.ExecuteWorkflow(executionKindOnboard, onboardInput{DeployedAppID: uuid.New()})

	if env.GetWorkflowError() == nil {
		t.Fatal("want a FailedPrecondition error for a missing settlement aggregate")
	}
	if f.gateway.created != 0 {
		t.Fatalf("nothing must be created on a failed pre-condition, got %d", f.gateway.created)
	}
}

// ============================ C. RegisterCustomerWorkflow ===================

// C1: happy path validates the instrument and opens the aggregate.
func Test_Register_HappyPath(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	wf := newWorkflows(deps)
	registerRegister(env, wf)

	env.ExecuteWorkflow(executionKindRegister, registerInput{CustomerID: cid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if f.gateway.validated != 1 {
		t.Fatalf("want one validateStoredInstrument, got %d", f.gateway.validated)
	}
	if len(f.state.registered) != 1 || f.state.registered[0] != cid {
		t.Fatalf("want one registerCustomer(%s), got %v", cid, f.state.registered)
	}
}

// ============================ D. CloseCycleWorkflow (money spine) ============

// D1: a positive net routes a payout and records settleCycle(Payout).
func Test_Close_Payout(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 3)
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(5000), RoutingDirective: routingPayout}
	wf := newWorkflows(deps)
	registerClose(env, wf)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res CloseCycleResult
	_ = env.GetWorkflowResult(&res)
	if res.Routed != RoutingDirectivePayout {
		t.Fatalf("want Routed=Payout, got %s", routingDirectiveName(res.Routed))
	}
	if len(f.gateway.payouts) != 1 || f.gateway.payouts[0].MinorUnits != 5000 {
		t.Fatalf("want one payout of 5000, got %v", f.gateway.payouts)
	}
	if len(f.gateway.charges) != 0 {
		t.Fatalf("payout must not charge, got %v", f.gateway.charges)
	}
	if len(f.state.settled) != 1 || f.state.settled[0].Directive != routingPayout {
		t.Fatalf("want one settleCycle(Payout), got %v", f.state.settled)
	}
}

// D2/D4: a negative net routes a charge of the EXACT positive magnitude and records
// settleCycle(Charge).
func Test_Close_Charge_ExactMagnitude(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 1)
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(-1299), RoutingDirective: routingCharge}
	wf := newWorkflows(deps)
	registerClose(env, wf)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(f.gateway.charges) != 1 {
		t.Fatalf("want one charge, got %v", f.gateway.charges)
	}
	// EXACT money: the charge is the positive magnitude of the -1299 signed net.
	if f.gateway.charges[0].MinorUnits != 1299 {
		t.Fatalf("want an exact charge of 1299 (magnitude of -1299), got %d", f.gateway.charges[0].MinorUnits)
	}
	if f.gateway.charges[0].Currency != "USD" {
		t.Fatalf("want USD currency preserved, got %q", f.gateway.charges[0].Currency)
	}
	if len(f.state.settled) != 1 || f.state.settled[0].Net.MinorUnits != -1299 {
		t.Fatalf("want settleCycle with the signed net -1299, got %v", f.state.settled)
	}
}

// D3: a zero net routes NOTHING and records settleCycle(NoAction).
func Test_Close_NoAction(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 1)
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(0), RoutingDirective: routingNoAction}
	wf := newWorkflows(deps)
	registerClose(env, wf)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(f.gateway.payouts) != 0 || len(f.gateway.charges) != 0 {
		t.Fatalf("NoAction must move no money; payouts=%v charges=%v", f.gateway.payouts, f.gateway.charges)
	}
	if len(f.state.settled) != 1 || f.state.settled[0].Directive != routingNoAction {
		t.Fatalf("want one settleCycle(NoAction), got %v", f.state.settled)
	}
}

// D5: a customer that is not registered + gateway-bound fails the pre-condition; no
// money move and no settle.
func Test_Close_NotBound_FailedPrecondition(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = settlementHead{ID: cid, Version: 1, Registered: true, GatewayBound: false}
	wf := newWorkflows(deps)
	registerClose(env, wf)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if env.GetWorkflowError() == nil {
		t.Fatal("want a FailedPrecondition for a not-gateway-bound customer")
	}
	if len(f.gateway.payouts) != 0 || len(f.gateway.charges) != 0 || len(f.state.settled) != 0 {
		t.Fatalf("nothing must settle/move on the failed pre-condition")
	}
}

// D6: inbound-revenue signals delivered before close are drained and appended
// (idempotent on the gateway event id); the close still settles.
func Test_Close_DrainsInboundRevenueSignals(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 1)
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(100), RoutingDirective: routingPayout}
	wf := newWorkflows(deps)
	registerClose(env, wf)

	// Deliver an inbound-revenue signal at start (drained before close).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalInboundRevenueReceived, GatewayRevenueEvent{
			GatewayEventID: "g1", CustomerID: cid, CycleID: "cycle-1", Amount: usd(100),
		})
	}, 0)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(f.ledger.inbound) != 1 || f.ledger.inbound[0].GatewayEventID != "g1" {
		t.Fatalf("want the inbound-revenue signal appended (dedup g1), got %v", f.ledger.inbound)
	}
}

// ============================ E. charge-failure branch (OQ-4) ===============

// E1: a decline → Retry → re-charge succeeds; the cycle is NOT escalated.
func Test_Close_ChargeDecline_Retry_Recharges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 1)
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(-2000), RoutingDirective: routingCharge}
	f.gateway.declineChargeFirst = 1 // first charge declines, retry succeeds
	f.interv.directive = settlementRetry
	wf := newWorkflows(deps)
	registerClose(env, wf)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res CloseCycleResult
	_ = env.GetWorkflowResult(&res)
	if res.Escalated {
		t.Fatal("a successful retry must NOT escalate")
	}
	if len(f.gateway.charges) != 1 || f.gateway.charges[0].MinorUnits != 2000 {
		t.Fatalf("want one successful re-charge of 2000, got %v", f.gateway.charges)
	}
	if len(f.state.settled) != 1 || f.state.settled[0].Escalated {
		t.Fatalf("want settleCycle not escalated, got %v", f.state.settled)
	}
}

// E2: a decline → Escalate → settleCycle(Escalated=true); the result flags escalation.
func Test_Close_ChargeDecline_Escalate_FlagsDelinquency(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 1)
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(-2000), RoutingDirective: routingCharge}
	f.gateway.declineChargeFirst = 99 // never succeeds
	f.interv.directive = settlementEscalate
	wf := newWorkflows(deps)
	registerClose(env, wf)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res CloseCycleResult
	_ = env.GetWorkflowResult(&res)
	if !res.Escalated {
		t.Fatal("an Escalate directive must flag the result escalated")
	}
	if len(f.state.settled) != 1 || !f.state.settled[0].Escalated {
		t.Fatalf("want settleCycle(Escalated=true), got %v", f.state.settled)
	}
	if len(f.gateway.charges) != 0 {
		t.Fatalf("an escalated decline records NO successful charge, got %v", f.gateway.charges)
	}
}

// E3: a decline → Delay → not escalated, no successful charge (left for the sweep).
func Test_Close_ChargeDecline_Delay_NoEscalation(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 1)
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(-2000), RoutingDirective: routingCharge}
	f.gateway.declineChargeFirst = 99
	f.interv.directive = settlementDelay
	wf := newWorkflows(deps)
	registerClose(env, wf)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res CloseCycleResult
	_ = env.GetWorkflowResult(&res)
	if res.Escalated {
		t.Fatal("a Delay directive must NOT escalate")
	}
	if len(f.gateway.charges) != 0 {
		t.Fatalf("a delayed decline records no successful charge, got %v", f.gateway.charges)
	}
}

// ============================ F. chargeback recompute saga ==================

// F1: a chargebackReceived signal runs the forward-only recompute: append reversal →
// RecomputeNet → resettleCycle → route the delta.
func Test_Close_Chargeback_ForwardOnlyRecompute(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 1)
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(5000), RoutingDirective: routingPayout}
	// After the reversal, the corrected net is a charge of 1500 (delta to claw back).
	f.engine.recomputeResult = settlementResultSeam{SignedNet: usd(-1500), RoutingDirective: routingCharge}
	wf := newWorkflows(deps)
	registerClose(env, wf)

	// Deliver a chargeback after the initial settle (drained by awaitChargeback).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalChargebackReceived, GatewayReversalEvent{
			GatewayEventID: "cb1", CustomerID: cid, CycleID: "cycle-1", Amount: usd(-6500),
		})
	}, 0)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(f.ledger.reversals) != 1 || f.ledger.reversals[0].GatewayEventID != "cb1" {
		t.Fatalf("want one reversal appended (dedup cb1), got %v", f.ledger.reversals)
	}
	if f.engine.recomputeN != 1 {
		t.Fatalf("want one RecomputeNet, got %d", f.engine.recomputeN)
	}
	if len(f.state.resettled) != 1 {
		t.Fatalf("want one resettleCycle correction, got %v", f.state.resettled)
	}
	// The delta is a charge of the exact magnitude of -1500.
	if len(f.gateway.charges) != 1 || f.gateway.charges[0].MinorUnits != 1500 {
		t.Fatalf("want one delta charge of 1500, got %v", f.gateway.charges)
	}
}

// ============================ G. ShortfallSweepWorkflow =====================

// G1: delinquent customers produce one queued applyDelinquencyPolicy signal each.
func Test_Sweep_SignalsEachDelinquentCustomer(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	c1, c2 := uuid.New(), uuid.New()
	f.state.delinquent = []customerSummary{
		{ID: c1, PauseNotWithdraw: true},
		{ID: c2, PauseNotWithdraw: false},
	}
	wf := newWorkflows(deps)
	registerSweep(env, wf)

	env.ExecuteWorkflow(executionKindShortfallSweep, shortfallSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res ShortfallSweepResult
	_ = env.GetWorkflowResult(&res)
	if len(res.SignalledCustomers) != 2 {
		t.Fatalf("want two signalled customers, got %v", res.SignalledCustomers)
	}
	if len(f.durable.signals) != 2 {
		t.Fatalf("want two queued delinquency signals, got %d", len(f.durable.signals))
	}
	// The BillingTerms-derived enforcement shape is carried on the signal.
	if !f.durable.signals[0].PauseNotWithdraw || f.durable.signals[1].PauseNotWithdraw {
		t.Fatalf("want pause-vs-withdraw carried per customer, got %+v", f.durable.signals)
	}
}

// G2: an empty sweep signals nobody — a normal quiet outcome.
func Test_Sweep_QuietSweep_NoSignals(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	f.state.delinquent = nil
	wf := newWorkflows(deps)
	registerSweep(env, wf)

	env.ExecuteWorkflow(executionKindShortfallSweep, shortfallSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res ShortfallSweepResult
	_ = env.GetWorkflowResult(&res)
	if len(res.SignalledCustomers) != 0 {
		t.Fatalf("a quiet sweep must signal nobody, got %v", res.SignalledCustomers)
	}
	if len(f.durable.signals) != 0 {
		t.Fatalf("a quiet sweep must deliver no signals, got %d", len(f.durable.signals))
	}
}

// ============================ H. §6.5 Conflict (money write) ================

// H1: settleCycle returns Conflict twice → the workflow re-reads the version and
// re-applies with the same key, converging to EXACTLY ONE recorded outcome (no
// double-record of the money-affecting write).
func Test_Close_SettleConflict_ReReadReApply_ConvergesToOne(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.settlement = boundSettlement(cid, 1)
	f.state.settleConflictFirst = 2 // first two settleCycle calls Conflict, then succeed
	f.engine.computeResult = settlementResultSeam{SignedNet: usd(0), RoutingDirective: routingNoAction}
	wf := newWorkflows(deps)
	registerClose(env, wf)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(f.state.settled) != 1 {
		t.Fatalf("the Conflict loop must converge to EXACTLY one settleCycle record, got %d", len(f.state.settled))
	}
}

// silence unused-import guard for time in case all delayed callbacks are removed.
var _ = time.Millisecond
