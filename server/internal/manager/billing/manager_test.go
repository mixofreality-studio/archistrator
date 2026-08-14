package billing

// =============================================================================
// SERVICE TEST PLAN (STP) — billingManager (C-MST)
// (the-method-testing: STP-first — the list of all the ways to demonstrate the
//  component does NOT work. NO BDD/Gherkin. White-box test client + black-box tests
//  with hand-written fakes for the frozen-collaborator seams. This Manager handles
//  REAL MONEY — correctness, idempotency, and exact-money invariants are paramount.)
//
// A. Façade pre-condition / contract-misuse (this file; no Temporal client needed —
//    the checks short-circuit before any client call, a nil client is safe):
//   A1  OnboardPaymentIntegration rejects empty deployedAppId      → ContractMisuse
//   A2  RegisterCustomer rejects empty customerId                  → ContractMisuse
//   A3  CloseBillingCycle rejects empty customerId              → ContractMisuse
//   A4  CloseBillingCycle rejects empty cycleId                 → ContractMisuse
//   A5  RunShortfallSweep rejects empty tickId                     → ContractMisuse
//   A6  RecordInboundRevenue rejects empty customerId/cycleId/gatewayEventId → ContractMisuse
//   A7  RecordRevenueReversal rejects empty customerId/cycleId/gatewayEventId → ContractMisuse
//   A8  Workflow-id derivation tokens are the §6.1 shapes
//   A9  RoutingDirective String() coverage
//   A10 gatewayIdempotencyKey is settle:{customerId}:{cycleId}
//
// B. OnboardWorkflow (workflow_test.go):
//   B1  happy path: read → bindGatewayLive → registerSchedule (charge-only: no
//                   connected-account creation); returns the resolved customerId
//   B2  a missing billing aggregate (read NotFound) → FailedPrecondition; no gateway bind
//
// C. RegisterCustomerWorkflow (workflow_test.go):
//   C1  happy path: validateStoredInstrument → registerCustomer; returns the customerId
//
// D. CloseCycleWorkflow (workflow_test.go) — the money spine:
//   D1  charge-only: net > 0 moves no money (no payout) + records settleCycle(NoAction)
//   D2  Charge: net < 0 routes chargeCustomer (positive magnitude) + records settleCycle(Charge)
//   D3  NoAction: net == 0 routes NOTHING + records settleCycle(NoAction)
//   D4  exact money: the charge amount is the EXACT positive magnitude of the signed net
//   D5  not registered/gateway-bound → FailedPrecondition; nothing routed
//   D6  inbound-revenue signals drained before close are appended (idempotent on event id)
//
// E. CloseCycleWorkflow charge-failure branch (OQ-4, workflow_test.go):
//   E1  decline → Retry → re-charge succeeds; not escalated
//   E2  decline → Escalate → settleCycle(Escalated=true); CloseCycleResult.Escalated true
//   E3  decline → Delay → not escalated, no further charge (left for the sweep)
//
// F. RecomputeCycle / chargeback saga (workflow_test.go):
//   F1  chargebackReceived → recordReversal → RecomputeNet → resettleCycle → route delta
//
// G. ShortfallSweepWorkflow (workflow_test.go):
//   G1  delinquent customers → one queued applyDelinquencyPolicy signal per customer
//   G2  empty sweep → no signals, empty SignalledCustomers (a normal quiet sweep)
//
// H. §6.5 Conflict discipline (workflow_test.go) — money-affecting idempotent replay:
//   H1  settleCycle returns Conflict twice → re-read→re-apply converges to ONE record
// =============================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// These tests cover the façade-boundary pre-condition checks the contract puts on the
// six public ops (billingManager.md §2/§3.1). They run BEFORE any Temporal client
// call, so they need no cluster and no client — a nil client is safe because the checks
// short-circuit first.

func asBillingError(t *testing.T, err error) *fwmgr.Error {
	t.Helper()
	var se *fwmgr.Error
	if !errors.As(err, &se) {
		t.Fatalf("expected *BillingError, got %T: %v", err, err)
	}
	return se
}

// testCtx builds a Manager-layer call Context for the façade pre-condition tests (the
// zero Principal is fine — these checks short-circuit before any Temporal call).
func testCtx() fwmgr.Context {
	return fwmgr.Context{Context: context.Background()}
}

// ---- A1: OnboardPaymentIntegration ------------------------------------------

func Test_Onboard_EmptyDeployedAppID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.OnboardPaymentIntegration(testCtx(), uuid.Nil)
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A2: RegisterCustomer ----------------------------------------------------

func Test_Register_EmptyCustomerID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.RegisterCustomer(testCtx(), uuid.Nil)
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A3/A4: CloseBillingCycle --------------------------------------------

func Test_Close_EmptyCustomerID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.CloseBillingCycle(testCtx(), uuid.Nil, "cycle-1")
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_Close_EmptyCycleID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.CloseBillingCycle(testCtx(), uuid.New(), "")
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A5: RunShortfallSweep --------------------------------------------------

func Test_Sweep_EmptyTickID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	_, err := m.RunShortfallSweep(testCtx(), "")
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A6: RecordInboundRevenue -----------------------------------------------

func Test_RecordInbound_EmptyCustomerID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.RecordInboundRevenue(testCtx(), GatewayRevenueEvent{CycleID: "c1", GatewayEventID: "g1"})
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RecordInbound_EmptyCycleID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.RecordInboundRevenue(testCtx(), GatewayRevenueEvent{CustomerID: uuid.New(), GatewayEventID: "g1"})
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RecordInbound_EmptyGatewayEventID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.RecordInboundRevenue(testCtx(), GatewayRevenueEvent{CustomerID: uuid.New(), CycleID: "c1"})
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A7: RecordRevenueReversal ----------------------------------------------

func Test_RecordReversal_EmptyGatewayEventID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.RecordRevenueReversal(testCtx(), GatewayReversalEvent{CustomerID: uuid.New(), CycleID: "c1"})
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RecordReversal_EmptyCustomerID(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.RecordRevenueReversal(testCtx(), GatewayReversalEvent{CycleID: "c1", GatewayEventID: "g1"})
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// ---- A8: workflow id derivation (§6.1) --------------------------------------

func Test_WorkflowIDDerivation(t *testing.T) {
	cid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	dapp := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	if got := onboardWorkflowID(dapp); got != dapp.String()+":onboard" {
		t.Fatalf("onboard id: %q", got)
	}
	if got := registerWorkflowID(cid); got != cid.String()+":register" {
		t.Fatalf("register id: %q", got)
	}
	if got := closeWorkflowID(cid, "cycle-7"); got != cid.String()+":cycle-7:close" {
		t.Fatalf("close id: %q", got)
	}
	if got := shortfallSweepWorkflowID("t9"); got != ":all:shortfallSweep:t9" {
		t.Fatalf("sweep id: %q", got)
	}
}

// ---- A9: RoutingDirective name coverage -------------------------------------

func Test_RoutingDirectiveName(t *testing.T) {
	cases := map[RoutingDirective]string{
		RoutingDirectiveNoAction: "NoAction",
		RoutingDirectiveCharge:   "Charge",
	}
	for d, want := range cases {
		if got := routingDirectiveName(d); got != want {
			t.Fatalf("routingDirectiveName(%d) = %q, want %q", int(d), got, want)
		}
	}
}

// ---- A10: gateway idempotency key shape -------------------------------------

func Test_GatewayIdempotencyKey(t *testing.T) {
	cid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	if got := gatewayIdempotencyKey(cid, "cycle-3"); got != "settle:"+cid.String()+":cycle-3" {
		t.Fatalf("gateway idempotency key: %q", got)
	}
}

// =============================================================================
// billingManager workflow unit tests over the Temporal in-memory test environment
// (testsuite.WorkflowTestSuite). Post-temporalgen migration: the ResourceAccess layer is
// reached through the GENERATED activities (activities.gen.go) + invokers
// (invokers.gen.go). ALL FIVE contract-backed RA ports (billingState/usage/
// merchantGateway/messageBus/revenueLedger — B7 folded the revenue-ledger fake in
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
// (RA Auth) the first declineChargeFirst times. Satisfies merchantgateway.MerchantGatewayAccess
// (charge-only: ChargeCustomer + ValidateStoredInstrument).
type fakeGateway struct {
	mu sync.Mutex

	declineChargeFirst int

	charges   []merchantgateway.Money
	validated int
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

func (g *fakeGateway) ValidateStoredInstrument(_ fwra.Context, _ uuid.UUID, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.validated++
	return nil
}

var _ merchantgateway.MerchantGatewayAccess = (*fakeGateway)(nil)

// fakeMessageBus records delivered signals (decoded from the JSON payload) + registered
// schedules. Satisfies messagebus.MessageBus.
type fakeMessageBus struct {
	mu sync.Mutex

	signals   []deliverSignalPayload
	schedules []string
}

func (d *fakeMessageBus) DeliverSignal(_ fwra.Context, _ messagebus.ExecutionID, _ messagebus.SignalName, payload messagebus.ExecutionPayload) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var p deliverSignalPayload
	_ = json.Unmarshal(payload.Bytes, &p)
	d.signals = append(d.signals, p)
	return nil
}

func (d *fakeMessageBus) RegisterSchedule(_ fwra.Context, scheduleID messagebus.ScheduleID, _ messagebus.ScheduleSpec) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.schedules = append(d.schedules, string(scheduleID))
	return nil
}

var _ messagebus.MessageBus = (*fakeMessageBus)(nil)

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
	state      *fakeBillingState
	ledger     *fakeRevenueLedger
	usage      *fakeUsage
	gateway    *fakeGateway
	messageBus *fakeMessageBus
	engine     *fakeBillingEngine
	interv     *fakeIntervention
}

func baseDeps() (wfDeps, *fakes) {
	f := &fakes{
		state:      &fakeBillingState{},
		ledger:     &fakeRevenueLedger{},
		usage:      &fakeUsage{},
		gateway:    &fakeGateway{},
		messageBus: &fakeMessageBus{},
		engine:     &fakeBillingEngine{},
		interv:     &fakeIntervention{directive: intervention.SettlementRetry},
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
		BillingState:    f.state,
		Usage:           f.usage,
		MerchantGateway: f.gateway,
		MessageBus:      f.messageBus,
		RevenueLedger:   f.ledger,
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
	reg(acts.MerchantGatewayChargeCustomer, "merchantGatewayAccess.chargeCustomer")
	reg(acts.MerchantGatewayValidateStoredInstrument, "merchantGatewayAccess.validateStoredInstrument")
	reg(acts.MessageBusDeliverSignal, "messageBus.deliverSignal")
	reg(acts.MessageBusRegisterSchedule, "messageBus.registerSchedule")
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

// B1: happy path resolves the customer, binds the gateway customer reference
// (charge-only: no connected-account creation), and registers the per-customer cycle Schedule.
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
	if len(f.state.bound) != 1 {
		t.Fatalf("want one bindGatewayLive, got %d", len(f.state.bound))
	}
	if len(f.messageBus.schedules) != 1 {
		t.Fatalf("want one registered cycle Schedule, got %d", len(f.messageBus.schedules))
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
	if len(f.state.bound) != 0 {
		t.Fatalf("nothing must be bound on a failed pre-condition, got %d", len(f.state.bound))
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

// D1: charge-only — a positive net moves no money (no payout) and records
// settleCycle(NoAction).
func Test_Close_PositiveNet_NoAction(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	deps, f := baseDeps()
	cid := uuid.New()
	f.state.billing = boundBilling(cid, 3)
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(5000), RoutingDirective: billingengine.RoutingNoAction}
	wf := newWorkflows(deps)
	registerClose(env, wf, f)

	env.ExecuteWorkflow(executionKindClose, closeInput{CustomerID: cid, CycleID: "cycle-1"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var res CloseCycleResult
	_ = env.GetWorkflowResult(&res)
	if res.Routed != RoutingDirectiveNoAction {
		t.Fatalf("want Routed=NoAction, got %s", routingDirectiveName(res.Routed))
	}
	if len(f.gateway.charges) != 0 {
		t.Fatalf("a positive net must move no money (charge-only, no payout), got charges=%v", f.gateway.charges)
	}
	if len(f.state.settled) != 1 || f.state.settled[0].Directive != billingstate.RoutingNoAction {
		t.Fatalf("want one settleCycle(NoAction), got %v", f.state.settled)
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
	if len(f.gateway.charges) != 0 {
		t.Fatalf("NoAction must move no money; charges=%v", f.gateway.charges)
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
	if len(f.gateway.charges) != 0 || len(f.state.settled) != 0 {
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
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(100), RoutingDirective: billingengine.RoutingNoAction}
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
	f.engine.computeResult = billingengine.BillingResult{SignedNet: engineUSD(5000), RoutingDirective: billingengine.RoutingNoAction}
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
	if len(f.messageBus.signals) != 2 {
		t.Fatalf("want two queued delinquency signals, got %d", len(f.messageBus.signals))
	}
	// The BillingTerms-derived enforcement shape is carried on the signal.
	if !f.messageBus.signals[0].PauseNotWithdraw || f.messageBus.signals[1].PauseNotWithdraw {
		t.Fatalf("want pause-vs-withdraw carried per customer, got %+v", f.messageBus.signals)
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
	if len(f.messageBus.signals) != 0 {
		t.Fatalf("a quiet sweep must deliver no signals, got %d", len(f.messageBus.signals))
	}
}

// G3 (fix round 1, Task 7c live-firing review): billingStateAccess is an
// arm-less REQUIRED binding today (no deployment perProfile arm) — this test
// backs the Activity with the REAL generated stub (billingstate.
// NewBillingStateAccess(), NOT a scripted fake) to reproduce exactly what fired
// live: every op returns fwra.Unknown("not implemented"). The sweep must
// complete CLEANLY (no workflow error, an empty result) instead of failing.
func Test_Sweep_UnimplementedBillingState_QuietNoOpTick(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	wf := newWorkflows(wfDeps{Acts: genInvokers{Opts: activityOptions()}})
	env.RegisterWorkflowWithOptions(wf.ShortfallSweepWorkflow, workflow.RegisterOptions{Name: executionKindShortfallSweep})
	stubActs := &genActivities{BillingState: billingstate.NewBillingStateAccess()}
	env.RegisterActivityWithOptions(stubActs.BillingStateReadPersistentlyDelinquentCustomers,
		activity.RegisterOptions{Name: "billingStateAccess.readPersistentlyDelinquentCustomers"})

	env.ExecuteWorkflow(executionKindShortfallSweep, shortfallSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("want a quiet no-op tick against the unimplemented stub RA, got workflow error: %v", err)
	}
	var res ShortfallSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(res.SignalledCustomers) != 0 {
		t.Fatalf("want an empty result on the tolerant tick, got %v", res.SignalledCustomers)
	}
}

// G4: isRAUnimplemented is the EXACT condition gating the tolerant tick's WARN
// log (shortfallsweep.go) — the TestWorkflowEnvironment exposes no hook to
// assert on log TEXT, so this proves the gate itself fires correctly against
// the REAL stub's REAL (fwmgr-mapped) error, run outside any workflow: the
// same stubBillingStateAccess.ReadPersistentlyDelinquentCustomers call
// Test_Sweep_UnimplementedBillingState_QuietNoOpTick exercises through the
// Activity boundary, mapped exactly as that boundary maps it.
func Test_IsRAUnimplemented_RealStubError(t *testing.T) {
	stub := billingstate.NewBillingStateAccess()
	_, err := stub.ReadPersistentlyDelinquentCustomers(fwra.Context{Context: t.Context()}, billingstate.DelinquencyScope{})
	if err == nil {
		t.Fatal("want the arm-less stub to return an error")
	}
	mapped := fwmgr.MapError(err)
	if !isRAUnimplemented(mapped) {
		t.Fatalf("want isRAUnimplemented(true) for the stub's mapped error, got false (mapped: %v)", mapped)
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

// Test_RecordRevenue_RequiresCurrency — Money.currency is schema-required, and
// required is presence-only: a money value with no currency is not a money value
// (2026-08-13 contract-strictness audit).
func Test_RecordRevenue_RequiresCurrency(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	err := m.RecordInboundRevenue(testCtx(), GatewayRevenueEvent{
		CustomerID: uuid.New(), CycleID: "cycle-1", GatewayEventID: "evt-1",
		Amount: Money{MinorUnits: 100, Currency: ""},
	})
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse for a currency-less amount, got %s", got)
	}
	rerr := m.RecordRevenueReversal(testCtx(), GatewayReversalEvent{
		CustomerID: uuid.New(), CycleID: "cycle-1", GatewayEventID: "evt-2",
		Amount: Money{MinorUnits: 100, Currency: "   "},
	})
	if got := asBillingError(t, rerr).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse for a currency-less reversal amount, got %s", got)
	}
}

// Test_RecordRevenueReversal_RejectsEmptyBackLink — the reverses-link is optional
// (nil = reverses no single event), but a pointer to "" persisted into the ledger
// through derefString as an empty id indistinguishable from absent.
func Test_RecordRevenueReversal_RejectsEmptyBackLink(t *testing.T) {
	m := newBillingManager(nil, nil, nil, nil, nil, nil, nil, nil)
	blank := "  "
	err := m.RecordRevenueReversal(testCtx(), GatewayReversalEvent{
		CustomerID: uuid.New(), CycleID: "cycle-1", GatewayEventID: "evt-3",
		Amount: Money{MinorUnits: 100, Currency: "usd"}, ReversesGatewayEventID: &blank,
	})
	if got := asBillingError(t, err).Kind; got != fwmgr.ContractMisuse {
		t.Fatalf("want ContractMisuse for a present-but-empty reverses link, got %s", got)
	}
}
