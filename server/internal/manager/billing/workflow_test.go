package billing

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// =============================================================================
// billingManager workflow unit tests over the Temporal in-memory test environment
// (testsuite.WorkflowTestSuite). Post-temporalgen migration: the ResourceAccess layer is
// reached through the GENERATED activities (activities.gen.go) + invokers
// (invokers.gen.go). ALL FIVE contract-backed RA ports (billingState/usage/
// merchantGateway/durableExecution/revenueLedger — B7 folded the revenue-ledger fake in
// alongside the rest) are constructed as CONTRACT-interface test doubles, wired into
// genActivities and registered under the generated activity names. The two Engines are
// direct-in-workflow fakes over their PUBLISHED contracts (billingengine.BillingEngine /
// intervention.InterventionEngine — no Manager-local seam). No Docker, no dev server.
//
// They assert the money spine (compute → route → record), exact-money invariants, the
// OQ-4 charge-failure decide→execute branch, the forward-only chargeback recompute saga
// and its idempotent ledger appends, the queued delinquency signal to operations, and
// the §6.5 Conflict re-read loop on the money-affecting settleCycle write — per
// [[the-method-testing]] (black-box where the observable is the workflow result /
// recorded side effects). STP map in manager_test.go.
// =============================================================================

// ---- Fakes: CONTRACT-interface test doubles for the RA ports ----------------

// fakeBillingState records the head-state transition calls + serves scripted state.
// Satisfies billingstate.BillingStateAccess.
type fakeBillingState struct {
	mu sync.Mutex

	billing    billingstate.Billing
	delinquent []billingstate.CustomerSummary
	notFound   bool

	// settleConflictFirst, when >0, returns fwra.Conflict on the first N settleCycle
	// calls before succeeding — drives the §6.5 re-read→re-apply loop on the
	// money-affecting write.
	settleConflictFirst int

	registered []uuid.UUID
	bound      []uuid.UUID
	settled    []billingstate.BillingOutcome
	resettled  []billingstate.BillingOutcome
	readN      int
	version    billingstate.Version
}

func (f *fakeBillingState) ReadBilling(_ fwra.Context, _ uuid.UUID) (billingstate.Billing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readN++
	if f.notFound {
		return billingstate.Billing{}, fwra.New(fwra.NotFound, "no row")
	}
	return f.billing, nil
}

func (f *fakeBillingState) ReadPersistentlyDelinquentCustomers(_ fwra.Context, _ billingstate.DelinquencyScope) ([]billingstate.CustomerSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.delinquent, nil
}

func (f *fakeBillingState) bump() billingstate.Version {
	f.version++
	f.billing.Version = f.version
	return f.version
}

func (f *fakeBillingState) RegisterCustomer(_ fwra.Context, c uuid.UUID, _ billingstate.Version, _ billingstate.CustomerProfile, _ fwra.IdempotencyKey) (billingstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = append(f.registered, c)
	return f.bump(), nil
}

func (f *fakeBillingState) BindGatewayLive(_ fwra.Context, c uuid.UUID, _ billingstate.Version, _ billingstate.GatewayBinding, _ fwra.IdempotencyKey) (billingstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bound = append(f.bound, c)
	return f.bump(), nil
}

func (f *fakeBillingState) SettleCycle(_ fwra.Context, _ uuid.UUID, _ billingstate.Version, _ string, outcome billingstate.BillingOutcome, _ fwra.IdempotencyKey) (billingstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.settleConflictFirst > 0 {
		f.settleConflictFirst--
		f.version++ // a racing mutation advanced the version
		f.billing.Version = f.version
		return 0, fwra.New(fwra.Conflict, "stale version")
	}
	f.settled = append(f.settled, outcome)
	return f.bump(), nil
}

func (f *fakeBillingState) ResettleCycle(_ fwra.Context, _ uuid.UUID, _ billingstate.Version, _ string, correction billingstate.BillingOutcome, _ fwra.IdempotencyKey) (billingstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resettled = append(f.resettled, correction)
	return f.bump(), nil
}

var _ billingstate.BillingStateAccess = (*fakeBillingState)(nil)

// fakeRevenueLedger records appends + serves a scripted range. Satisfies the generated
// billingstate.RevenueLedgerAccess contract.
type fakeRevenueLedger struct {
	mu sync.Mutex

	rangeEntries []billingstate.RevenueEntry
	inbound      []billingstate.RevenueEntry
	reversals    []billingstate.ReversalEntry
}

func (r *fakeRevenueLedger) RecordInboundRevenue(_ fwra.Context, entry billingstate.RevenueEntry) (billingstate.EntryRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inbound = append(r.inbound, entry)
	r.rangeEntries = append(r.rangeEntries, entry)
	return billingstate.EntryRef("ref"), nil
}

func (r *fakeRevenueLedger) RecordReversal(_ fwra.Context, reversal billingstate.ReversalEntry) (billingstate.EntryRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reversals = append(r.reversals, reversal)
	// A reversal is a new negative fact appended to the same log readRange replays.
	r.rangeEntries = append(r.rangeEntries, billingstate.RevenueEntry{
		CustomerID: reversal.CustomerID, CycleID: reversal.CycleID,
		Kind: billingstate.RevenueKindReversal, Amount: reversal.Amount, GatewayEventID: reversal.GatewayEventID,
	})
	return billingstate.EntryRef("revref"), nil
}

func (r *fakeRevenueLedger) ReadRange(_ fwra.Context, _ uuid.UUID, _ string) ([]billingstate.RevenueEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]billingstate.RevenueEntry, len(r.rangeEntries))
	copy(out, r.rangeEntries)
	return out, nil
}

var _ billingstate.RevenueLedgerAccess = (*fakeRevenueLedger)(nil)

// fakeUsage serves a scripted usage range. Satisfies usage.UsageAccess.
type fakeUsage struct {
	rangeEvents []usage.UsageEvent
}

func (u *fakeUsage) ReadRange(_ fwra.Context, _ usage.UsageRangeQuery) ([]usage.UsageEvent, error) {
	return u.rangeEvents, nil
}

func (u *fakeUsage) RecordComputeUsage(_ fwra.Context, _ []usage.UsageEvent) ([]usage.EntryRef, error) {
	return nil, nil
}

func (u *fakeUsage) RecordFinalUsage(_ fwra.Context, _ []usage.UsageEvent) ([]usage.EntryRef, error) {
	return nil, nil
}

var _ usage.UsageAccess = (*fakeUsage)(nil)

// fakeGateway records money moves; declineCharge makes ChargeCustomer fail terminally
// (RA Auth) the first declineChargeFirst times. Satisfies merchantgateway.MerchantGatewayAccess.
type fakeGateway struct {
	mu sync.Mutex

	declineChargeFirst int

	payouts   []merchantgateway.Money
	charges   []merchantgateway.Money
	created   int
	validated int
}

func (g *fakeGateway) PayoutCustomer(_ fwra.Context, _ uuid.UUID, amount merchantgateway.Money, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.payouts = append(g.payouts, amount)
	return nil
}

func (g *fakeGateway) ChargeCustomer(_ fwra.Context, _ uuid.UUID, amount merchantgateway.Money, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.declineChargeFirst > 0 {
		g.declineChargeFirst--
		return fwra.New(fwra.Auth, "card declined")
	}
	g.charges = append(g.charges, amount)
	return nil
}

func (g *fakeGateway) CreateConnectedAccount(_ fwra.Context, _ uuid.UUID, _ string) (merchantgateway.GatewayBinding, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.created++
	return merchantgateway.GatewayBinding{ConnectedAccountID: "acct-1"}, nil
}

func (g *fakeGateway) ValidateStoredInstrument(_ fwra.Context, _ uuid.UUID, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.validated++
	return nil
}

var _ merchantgateway.MerchantGatewayAccess = (*fakeGateway)(nil)

// fakeDurable records delivered signals (decoded from the JSON payload) + registered
// schedules. Satisfies durableexecution.DurableExecutionAccess.
type fakeDurable struct {
	mu sync.Mutex

	signals   []deliverSignalPayload
	schedules []string
}

func (d *fakeDurable) DeliverSignal(_ fwra.Context, _ durableexecution.ExecutionID, _ durableexecution.SignalName, payload durableexecution.ExecutionPayload) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var p deliverSignalPayload
	_ = json.Unmarshal(payload.Bytes, &p)
	d.signals = append(d.signals, p)
	return nil
}

func (d *fakeDurable) RegisterSchedule(_ fwra.Context, scheduleID durableexecution.ScheduleID, _ durableexecution.ScheduleSpec) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.schedules = append(d.schedules, string(scheduleID))
	return nil
}

func (d *fakeDurable) QueryExecutionState(_ fwra.Context, _ durableexecution.ExecutionID, _ durableexecution.QueryName, _ durableexecution.ExecutionPayload) (durableexecution.ExecutionStateView, error) {
	return durableexecution.ExecutionStateView{}, nil
}

func (d *fakeDurable) StartOrSignalExecution(_ fwra.Context, _ durableexecution.ExecutionKind, _ durableexecution.ExecutionID, _ durableexecution.SignalName, _ durableexecution.ExecutionPayload) (durableexecution.ExecutionHandle, error) {
	return durableexecution.ExecutionHandle(""), nil
}

var _ durableexecution.DurableExecutionAccess = (*fakeDurable)(nil)

// fakeBillingEngine returns a scripted BillingResult for compute + recompute.
// Satisfies the published billingengine.BillingEngine contract directly (no seam).
type fakeBillingEngine struct {
	computeResult   billingengine.BillingResult
	recomputeResult billingengine.BillingResult
	computeN        int
	recomputeN      int
}

func (e *fakeBillingEngine) ComputeNet(_ fweng.Context, _ billingengine.CycleRevenue, _ billingengine.CycleUsage, _ billingengine.BillingTerms) (billingengine.BillingResult, error) {
	e.computeN++
	return e.computeResult, nil
}

func (e *fakeBillingEngine) ProjectCommitTimeRevenueShareAndComputeCost(_ fweng.Context, _ billingengine.ProjectOption) (billingengine.Projection, error) {
	return billingengine.Projection{}, nil
}

func (e *fakeBillingEngine) RecomputeNet(_ fweng.Context, _ billingengine.ReBillingInput) (billingengine.BillingResult, error) {
	e.recomputeN++
	return e.recomputeResult, nil
}

var _ billingengine.BillingEngine = (*fakeBillingEngine)(nil)

// fakeIntervention returns a scripted billing-failure directive. Satisfies the
// published intervention.InterventionEngine contract directly (no seam).
type fakeIntervention struct {
	directive intervention.SettlementFailureDirective
}

func (i *fakeIntervention) ApplyPausePolicy(_ fweng.Context, _ intervention.PauseRequestContext) (intervention.PausePlan, error) {
	return intervention.PausePlan{}, nil
}

func (i *fakeIntervention) DecideOnHealth(_ fweng.Context, _ intervention.HealthChange) (intervention.HealthDirective, error) {
	return intervention.HealthRetry, nil
}

func (i *fakeIntervention) DecideOnSettlementFailure(_ fweng.Context, _ intervention.SettlementFailure) (intervention.SettlementFailureDirective, error) {
	return i.directive, nil
}

func (i *fakeIntervention) DecideOnVariance(_ fweng.Context, _ intervention.ConstructionVariance) (intervention.VarianceDirective, error) {
	return intervention.VarianceRetry, nil
}

var _ intervention.InterventionEngine = (*fakeIntervention)(nil)

// ---- helpers ----------------------------------------------------------------

type fakes struct {
	state   *fakeBillingState
	ledger  *fakeRevenueLedger
	usage   *fakeUsage
	gateway *fakeGateway
	durable *fakeDurable
	engine  *fakeBillingEngine
	interv  *fakeIntervention
}

func baseDeps() (wfDeps, *fakes) {
	f := &fakes{
		state:   &fakeBillingState{},
		ledger:  &fakeRevenueLedger{},
		usage:   &fakeUsage{},
		gateway: &fakeGateway{},
		durable: &fakeDurable{},
		engine:  &fakeBillingEngine{},
		interv:  &fakeIntervention{directive: intervention.SettlementRetry},
	}
	return wfDeps{
		Billing:      f.engine,
		Intervention: f.interv,
		Acts:         genInvokers{Opts: activityOptions()},
	}, f
}

// registerActs wires the contract-backed RA fakes into genActivities and registers each
// generated activity — including the three revenueLedgerAccess ops (B7: no longer custom
// Activities) — under its stable registered name, the name the workflow's Acts invokers
// call by. Registering the full set each test is harmless (unused ones are never
// dispatched) and keeps the per-workflow register helpers uniform.
func registerActs(env *testsuite.TestWorkflowEnvironment, f *fakes) {
	acts := &genActivities{
		BillingState:     f.state,
		Usage:            f.usage,
		MerchantGateway:  f.gateway,
		DurableExecution: f.durable,
		RevenueLedger:    f.ledger,
	}
	reg := func(fn any, name string) {
		env.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	reg(acts.BillingStateReadBilling, "billingStateAccess.readBilling")
	reg(acts.BillingStateReadPersistentlyDelinquentCustomers, "billingStateAccess.readPersistentlyDelinquentCustomers")
	reg(acts.BillingStateRegisterCustomer, "billingStateAccess.registerCustomer")
	reg(acts.BillingStateBindGatewayLive, "billingStateAccess.bindGatewayLive")
	reg(acts.BillingStateSettleCycle, "billingStateAccess.settleCycle")
	reg(acts.BillingStateResettleCycle, "billingStateAccess.resettleCycle")
	reg(acts.UsageReadRange, "usageAccess.readRange")
	reg(acts.MerchantGatewayPayoutCustomer, "merchantGatewayAccess.payoutCustomer")
	reg(acts.MerchantGatewayChargeCustomer, "merchantGatewayAccess.chargeCustomer")
	reg(acts.MerchantGatewayCreateConnectedAccount, "merchantGatewayAccess.createConnectedAccount")
	reg(acts.MerchantGatewayValidateStoredInstrument, "merchantGatewayAccess.validateStoredInstrument")
	reg(acts.DurableExecutionDeliverSignal, "durableExecutionAccess.deliverSignal")
	reg(acts.DurableExecutionRegisterSchedule, "durableExecutionAccess.registerSchedule")
	reg(acts.RevenueLedgerReadRange, "revenueLedgerAccess.readRange")
	reg(acts.RevenueLedgerRecordInboundRevenue, "revenueLedgerAccess.recordInboundRevenue")
	reg(acts.RevenueLedgerRecordReversal, "revenueLedgerAccess.recordReversal")
}

func registerOnboard(env *testsuite.TestWorkflowEnvironment, wf *workflows, f *fakes) {
	env.RegisterWorkflowWithOptions(wf.OnboardWorkflow, workflow.RegisterOptions{Name: executionKindOnboard})
	registerActs(env, f)
}

func registerRegister(env *testsuite.TestWorkflowEnvironment, wf *workflows, f *fakes) {
	env.RegisterWorkflowWithOptions(wf.RegisterCustomerWorkflow, workflow.RegisterOptions{Name: executionKindRegister})
	registerActs(env, f)
}

func registerClose(env *testsuite.TestWorkflowEnvironment, wf *workflows, f *fakes) {
	env.RegisterWorkflowWithOptions(wf.CloseCycleWorkflow, workflow.RegisterOptions{Name: executionKindClose})
	registerActs(env, f)
}

func registerSweep(env *testsuite.TestWorkflowEnvironment, wf *workflows, f *fakes) {
	env.RegisterWorkflowWithOptions(wf.ShortfallSweepWorkflow, workflow.RegisterOptions{Name: executionKindShortfallSweep})
	registerActs(env, f)
}

// boundBilling returns a registered + gateway-bound billing at the given version.
func boundBilling(id uuid.UUID, version billingstate.Version) billingstate.Billing {
	return billingstate.Billing{ID: id, Version: version, Registered: true, GatewayBound: true}
}

func usd(minor int64) Money { return Money{MinorUnits: minor, Currency: "USD"} }

// engineUSD builds the published billingengine.Money the fakeBillingEngine scripts its
// results with (distinct named type from the façade's own Money, same shape).
func engineUSD(minor int64) billingengine.Money {
	return billingengine.Money{MinorUnits: minor, Currency: "USD"}
}

// ============================ B. OnboardWorkflow =============================

// B1: happy path resolves the customer, creates the connected account, binds the
// gateway, and registers the per-customer cycle Schedule.
func Test_Onboard_HappyPath(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.billing = billingstate.Billing{ID: cid, Version: 2}
	wf := newWorkflows(deps)
	registerOnboard(env, wf, f)

	env.ExecuteWorkflow(executionKindOnboard, onboardInput{DeployedAppID: uuid.New()})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var ref BillingRef
	if err := env.GetWorkflowResult(&ref); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ref.CustomerID != cid {
		t.Fatalf("want resolved customerId %s, got %s", cid, ref.CustomerID)
	}
	if f.gateway.created != 1 {
		t.Fatalf("want one connected account, got %d", f.gateway.created)
	}
	if len(f.state.bound) != 1 {
		t.Fatalf("want one bindGatewayLive, got %d", len(f.state.bound))
	}
	if len(f.durable.schedules) != 1 {
		t.Fatalf("want one registered cycle Schedule, got %d", len(f.durable.schedules))
	}
}

// B2: a missing billing aggregate (read NotFound) fails the pre-condition; no money
// move happens.
func Test_Onboard_NoAggregate_FailedPrecondition(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	f.state.notFound = true
	wf := newWorkflows(deps)
	registerOnboard(env, wf, f)

	env.ExecuteWorkflow(executionKindOnboard, onboardInput{DeployedAppID: uuid.New()})

	if env.GetWorkflowError() == nil {
		t.Fatal("want a FailedPrecondition error for a missing billing aggregate")
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
	registerRegister(env, wf, f)

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
	f.state.billing = boundBilling(cid, 3)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(5000), RoutingDirective: billingengine.RoutingPayout}
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
	if len(f.state.settled) != 1 || f.state.settled[0].Directive != billingstate.RoutingPayout {
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
	f.state.billing = boundBilling(cid, 1)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(-1299), RoutingDirective: billingengine.RoutingCharge}
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
	f.state.billing = boundBilling(cid, 1)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(0), RoutingDirective: billingengine.RoutingNoAction}
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(f.gateway.payouts) != 0 || len(f.gateway.charges) != 0 {
		t.Fatalf("NoAction must move no money; payouts=%v charges=%v", f.gateway.payouts, f.gateway.charges)
	}
	if len(f.state.settled) != 1 || f.state.settled[0].Directive != billingstate.RoutingNoAction {
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
	f.state.billing = billingstate.Billing{ID: cid, Version: 1, Registered: true, GatewayBound: false}
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
	f.state.billing = boundBilling(cid, 1)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(100), RoutingDirective: billingengine.RoutingPayout}
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
	f.state.billing = boundBilling(cid, 1)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(-2000), RoutingDirective: billingengine.RoutingCharge}
	f.gateway.declineChargeFirst = 1 // first charge declines, retry succeeds
	f.interv.directive = intervention.SettlementRetry
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
	f.state.billing = boundBilling(cid, 1)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(-2000), RoutingDirective: billingengine.RoutingCharge}
	f.gateway.declineChargeFirst = 99 // never succeeds
	f.interv.directive = intervention.SettlementEscalate
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
	f.state.billing = boundBilling(cid, 1)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(-2000), RoutingDirective: billingengine.RoutingCharge}
	f.gateway.declineChargeFirst = 99
	f.interv.directive = intervention.SettlementDelay
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
	f.state.billing = boundBilling(cid, 1)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(5000), RoutingDirective: billingengine.RoutingPayout}
	// After the reversal, the corrected net is a charge of 1500 (delta to claw back).
	f.engine.recomputeResult = billingengine.BillingResult{SignedNet: engineUSD(-1500), RoutingDirective: billingengine.RoutingCharge}
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
	f.state.delinquent = []billingstate.CustomerSummary{
		{ID: c1, PauseNotWithdraw: true},
		{ID: c2, PauseNotWithdraw: false},
	}
	wf := newWorkflows(deps)
	registerSweep(env, wf, f)

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
	registerSweep(env, wf, f)

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
	f.state.billing = boundBilling(cid, 1)
	f.state.settleConflictFirst = 2 // first two settleCycle calls Conflict, then succeed
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(0), RoutingDirective: billingengine.RoutingNoAction}
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

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
