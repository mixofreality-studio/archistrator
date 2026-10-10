// Hooks for billingManager scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file.
//
// The subject is the REAL billingManager, built through its generated constructor
// billing.NewBillingManager over REAL collaborators, each through its own public
// constructor, the way cmd/server/main.gen.go wires them (DCT §12 B2):
// billingStateAccess, usageAccess and merchantGatewayAccess are their Postgres variants
// (§12 B5) over a Postgres of the scenario's own, so no state leaks between scenarios
// (the shortfall sweep reads every declined cycle and delinquent customer of the
// store); projectStateAccess is the local-profile git variant over a repository of the
// scenario's own; billingEngine and interventionEngine are the real pure engines;
// messageBus is the Temporal message bus on the host's dev server, and a host worker on
// billing.TaskQueue runs the manager's workflows through the composition root's own
// registration (RegisterManagerWorker).
//
// Arrangement goes through the collaborators' own contract write ops (a registered or
// gateway-bound customer, a validated instrument, metered usage, a settled or declined
// cycle, a project whose SDP option is committed) — never raw Resource bytes — and
// every probe calls the named component's read-only op on this scenario's stack. The
// hooks import no SDK and no framework layer package: the host serves the Temporal
// client, the worker and the layer call contexts.

package billing_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	ghtestinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	pgtestinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// replayWorkflows tells the scenario host which workflows this package's workers
// run: at every flush the host replays each workflow history a scenario
// started against them, and a non-determinism error fails that scenario.
// The registration is the composition root's own: billing.RegisterManagerWorker over a
// manager from the generated constructor. A replay executes workflow code only — the
// engines run in-workflow, so they are the real ones; the activities' collaborators are
// never called. The live worker runs with the SDK's default options, so the replayer
// does too.
func replayWorkflows() scenariohost.Workflows {
	return scenariohost.Workflows{Register: func(w scenariohost.Worker) {
		billing.RegisterManagerWorker(w, billing.NewBillingManager(nil, nil, nil, nil, nil,
			billingengine.NewBillingEngine(), intervention.NewInterventionEngine(), nil))
	}}
}

// newSubject builds the component under test against the host's real downstream
// stack: the generated billing.NewBillingManager over this scenario's own Postgres- and
// git-backed collaborators and the host's Temporal, with a host worker polling
// billing.TaskQueue.
//
// The pool comes from the platform's Postgres testinfra rather than the host's
// PostgresURL: opening a pool on a URL needs pgxpool or the postgres infrastructure
// module, neither of which a hooks file may import, while a …/testinfra package is.
func newSubject(t *testing.T, h *scenariohost.Host) billing.BillingManager {
	t.Helper()
	ctx := h.Context(t)
	pool := pgtestinfra.StartPostgres(t)
	billingState, err := billingstate.NewPostgresBillingStateAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresBillingStateAccess: %v", err)
	}
	usageLog, err := usage.NewPostgresUsageAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresUsageAccess: %v", err)
	}
	gateway, err := merchantgateway.NewPostgresMerchantGatewayAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresMerchantGatewayAccess: %v", err)
	}
	repo := ghtestinfra.StartLocalGitRepo(t, "main")
	projects := projectstate.NewGitLocalProjectStateAccess(repo.URL)

	subject := billing.NewBillingManager(h.TemporalClient(), billingState, usageLog, gateway,
		messagebus.NewTemporalMessageBus(h.TemporalClient(), billingKinds()),
		billingengine.NewBillingEngine(), intervention.NewInterventionEngine(), projects)
	h.StartWorker(t, billing.TaskQueue, func(w scenariohost.Worker) {
		billing.RegisterManagerWorker(w, subject)
	})
	stacks.Store(subject, &stack{
		billingState: billingState,
		usage:        usageLog,
		gateway:      gateway,
		projects:     projects,
		designs:      projectstate.NewGitLocalDesignSessionAccess(repo.URL),
	})
	t.Cleanup(func() { stacks.Delete(subject) })
	return subject
}

// billingKinds is the slice of the composition root's messageBus kind table
// (cmd/server/hooks.go MessageBusTemporalArgs) billingManager reaches: the shortfall
// sweep and the per-customer cycle close on the billing task queue, and operations'
// delinquency enforcement, which the sweep's applyDelinquencyPolicy signal starts.
func billingKinds() map[messagebus.ExecutionKind]messagebus.KindBinding {
	return map[messagebus.ExecutionKind]messagebus.KindBinding{
		"billingShortfallSweep":            {WorkflowType: "billingShortfallSweep", TaskQueue: billing.TaskQueue},
		"billingCloseCycle":                {WorkflowType: "billingCloseCycle", TaskQueue: billing.TaskQueue},
		"operationsDelinquencyEnforcement": {WorkflowType: "operationsDelinquencyEnforcement", TaskQueue: operations.TaskQueue},
	}
}

// stack is one scenario's real collaborators, kept so the step and probe hooks can
// arrange and observe the state the subject reads and writes. designs is the
// designSessionAccess facet over the scenario's repository: the delivery Manager's
// commit of a project option, which a registration reads back.
type stack struct {
	billingState billingstate.BillingStateAccess
	usage        usage.UsageAccess
	gateway      merchantgateway.MerchantGatewayAccess
	projects     projectstate.ProjectStateAccess
	designs      projectstate.DesignSessionAccess
}

// stacks maps a subject newSubject built to its stack.
var stacks sync.Map

func stackOf(t *testing.T, subject billing.BillingManager) *stack {
	t.Helper()
	st, ok := stacks.Load(subject)
	if !ok {
		t.Fatal("hooks: no stack for this subject (newSubject did not build it)")
	}
	s, ok := st.(*stack)
	if !ok {
		t.Fatalf("hooks: stack of the wrong type %T", st)
	}
	return s
}

// stepTimeout bounds one call into the subject, so a workflow that cannot finish fails
// its scenario instead of hanging the package.
const stepTimeout = 90 * time.Second

// bounded puts stepTimeout on a step's call context; the scenario's cleanup cancels it.
func bounded(ctx context.Context, t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	t.Cleanup(cancel)
	return ctx
}

// unarrangeable fails a step whose guards no collaborator's contract can arrange, so the
// scenario cannot pass on a state its path does not hold.
func unarrangeable(t *testing.T, why string) {
	t.Helper()
	t.Fatalf("unarrangeable: %s", why)
}

// noDeclineThenAccept is why a path that needs one charge declined and the next accepted
// cannot be arranged: merchantGatewayAccess declines exactly when the customer holds no
// validated instrument, which holds alike for every attempt of one charge sequence (the
// close's in-close retry, one sweep's re-charges). Escalated in plan-5 round 3 (which
// retry semantics the product has is not settled by a principle). Arranged with an
// instrument, the first charge lands and the scenario would pass on the [yes] path's
// state; arranged without one, every charge declines.
const noDeclineThenAccept = "the path needs a charge declined and the next accepted within one charge sequence; " +
	"merchantGatewayAccess declines exactly when no validated instrument is held, which no op changes between " +
	"two attempts of one sequence (plan-5 r3 escalation: the product's retry semantics)"

// ---------------------------------------------------------------------------
// Arrangement through the collaborators' own contract ops.
// ---------------------------------------------------------------------------

// cycleRef names one customer's cycle. It is the generated step input of
// GetBillingCycle, so the ids decode into the contract's own types from their JSON.
type cycleRef = inputViewABillingCycleP1S1

func refOf(t *testing.T, customerID, cycle string) cycleRef {
	t.Helper()
	var r cycleRef
	mustUnmarshal(t, fmt.Sprintf(`{"customerID":%q,"cycleID":%q}`, customerID, cycle), &r)
	return r
}

// The projects customers register under: the delinquency probes scope by them, and the
// onboarding registration reads its committed option.
const (
	billP2Project    = "stp-billing-bill-P2-project"
	retryP3Project   = "stp-billing-retry-P3-project"
	billingProject   = "stp-billing-project"
	chosenOptionID   = "opt-normal"
	committedByActor = "founder@stp.test"
)

const (
	closedCycle   = "2026-09" // the cycle the bill and view scenarios close or read
	openCycle     = "2026-10" // the period view P1 reads before it closes
	declinedCycle = "2026-08" // the cycle whose charge stands declined in the retry scenarios
)

// meteredUnitSeconds is the compute a customer's cycle meters: a positive usage, so the
// cycle's net is a charge.
const meteredUnitSeconds = 4200

// customerTerms are the terms a customer registers under: what a registration records
// from the committed option (tiered floors, no markup, monthly, in USD).
var customerTerms = billingstate.BillingTerms{
	ComputeCostKind: int64(billingengine.ComputeCostTieredFloors),
	ScheduleKind:    int64(billingengine.ScheduleMonthly),
	Currency:        "USD",
}

// arrangeRegistered opens the customer's billing record (version 1) under project and
// customerTerms, as a registration does.
func arrangeRegistered(t *testing.T, h *scenariohost.Host, st *stack, r cycleRef, project string) billingstate.Version {
	t.Helper()
	rc := h.ResourceAccessContext(t, "arrange-register:"+r.CustomerID.String())
	v, err := st.billingState.RegisterCustomer(rc, r.CustomerID, 0,
		billingstate.CustomerProfile{ProjectID: project, Terms: customerTerms}, rc.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: RegisterCustomer %s: %v", r.CustomerID, err)
	}
	return v
}

// arrangeOperated registers the customer and binds its gateway live: an onboarded
// customer, the state every billing cycle starts from.
func arrangeOperated(t *testing.T, h *scenariohost.Host, st *stack, r cycleRef, project string) billingstate.Version {
	t.Helper()
	v := arrangeRegistered(t, h, st, r, project)
	rc := h.ResourceAccessContext(t, "arrange-bind:"+r.CustomerID.String())
	v, err := st.billingState.BindGatewayLive(rc, r.CustomerID, v,
		billingstate.GatewayBinding{GatewayCustomerRef: r.CustomerID.String()}, rc.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: BindGatewayLive %s: %v", r.CustomerID, err)
	}
	return v
}

// arrangeInstrument has the gateway hold a validated stored instrument for the customer:
// its charges go through.
func arrangeInstrument(t *testing.T, h *scenariohost.Host, st *stack, r cycleRef) {
	t.Helper()
	rc := h.ResourceAccessContext(t, "arrange-validate:"+r.CustomerID.String())
	if err := st.gateway.ValidateStoredInstrument(rc, r.CustomerID, string(rc.IdempotencyKey)); err != nil {
		t.Fatalf("arrange: ValidateStoredInstrument %s: %v", r.CustomerID, err)
	}
}

// arrangeUsage meters the cycle's compute through usageAccess's own append, as the
// operations side records it.
func arrangeUsage(t *testing.T, h *scenariohost.Host, st *stack, r cycleRef) {
	t.Helper()
	windowStart := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	events := []usage.UsageEvent{{
		CustomerID:     r.CustomerID,
		OperatedAppID:  r.CustomerID,
		CycleID:        usage.CycleID(r.CycleID),
		Units:          usage.ComputeUnits{Amount: meteredUnitSeconds, Unit: "compute-unit-seconds"},
		RuntimeEventID: usage.RuntimeEventID(fmt.Sprintf("meter:%s:%s", r.CustomerID, r.CycleID)),
		WindowStart:    windowStart,
		WindowEnd:      windowStart.AddDate(0, 1, 0).Add(-time.Second),
	}}
	if _, err := st.usage.RecordComputeUsage(h.ResourceAccessContext(t, "arrange-usage:"+r.CustomerID.String()), events); err != nil {
		t.Fatalf("arrange: RecordComputeUsage %s %s: %v", r.CustomerID, r.CycleID, err)
	}
}

// chargeNet is the net a metered cycle settles: its compute cost, charged.
var chargeNet = billingstate.Money{MinorUnits: -meteredUnitSeconds, Currency: "USD"}

// arrangeSettled records the cycle's close with outcome, as the close records it.
func arrangeSettled(t *testing.T, h *scenariohost.Host, st *stack, r cycleRef, v billingstate.Version, outcome billingstate.BillingOutcome) {
	t.Helper()
	rc := h.ResourceAccessContext(t, fmt.Sprintf("arrange-settle:%s:%s", r.CustomerID, r.CycleID))
	if _, err := st.billingState.SettleCycle(rc, r.CustomerID, v, r.CycleID, outcome, rc.IdempotencyKey); err != nil {
		t.Fatalf("arrange: SettleCycle %s %s: %v", r.CustomerID, r.CycleID, err)
	}
}

// arrangeDeclined is an onboarded customer whose cycle closed with its charge declined
// and left for the shortfall sweep, after attempts charge attempts and sweepsAged sweeps
// — the retry history the sweep's policy reads (note-retry-history).
func arrangeDeclined(t *testing.T, h *scenariohost.Host, st *stack, r cycleRef, project string, attempts, sweepsAged int64) {
	t.Helper()
	arrangeSettled(t, h, st, r, arrangeOperated(t, h, st, r, project), billingstate.BillingOutcome{
		Net:        chargeNet,
		Directive:  billingstate.RoutingCharge,
		Declined:   true,
		Attempts:   attempts,
		SweepsAged: sweepsAged,
	})
}

// arrangeCommittedOption is a project whose SDP option is committed under planning
// assumptions carrying its billing terms and cost currency, written the way the
// commit-to-a-project-option use case writes it: the project created through
// projectStateAccess, each artifact staged and committed through designSessionAccess.
func arrangeCommittedOption(t *testing.T, h *scenariohost.Host, st *stack, project string) {
	t.Helper()
	pid := projectstate.ProjectID(project)
	v, err := st.projects.CreateProject(h.ResourceAccessContext(t, "arrange-create-project"), pid,
		projectstate.OwnerScope("stp-billing-owner"), "STP Billing Onboarding")
	if err != nil {
		t.Fatalf("arrange: CreateProject %s: %v", project, err)
	}
	v = stageAndCommit(t, h, st, pid, v, &projectstate.PlanningAssumptions{
		Terms:             projectstate.SettlementTerms{ComputeCost: projectstate.ComputeCostTieredFloors, Schedule: projectstate.ScheduleMonthly},
		IndirectDailyRate: projectstate.Money{MinorUnits: 50000, Currency: "USD"},
	})
	chosen := projectstate.OptionID(chosenOptionID)
	stageAndCommit(t, h, st, pid, v, &projectstate.SdpReview{
		Options: []projectstate.SdpOptionRow{{
			OptionID: chosen, SolutionKind: projectstate.KindNormalSolution, DurationDays: 100,
			BuildCost: projectstate.Money{MinorUnits: 1000000, Currency: "USD"},
		}},
		Recommendation: chosen,
		ChosenOption:   &chosen,
	})
}

// stageAndCommit stages model on main and commits it with its provenance, returning the
// project's version after the commit.
func stageAndCommit(t *testing.T, h *scenariohost.Host, st *stack, pid projectstate.ProjectID, v projectstate.Version, model projectstate.ArtifactModel) projectstate.Version {
	t.Helper()
	env, err := projectstate.EncodeModel(model)
	if err != nil {
		t.Fatalf("arrange: encode %v: %v", model.Kind(), err)
	}
	rc := h.ResourceAccessContext(t, fmt.Sprintf("arrange-stage-%d", model.Kind()))
	v, err = st.designs.StageArtifactForReviewOnBranch(rc, pid, v, "", env, rc.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: stage %v: %v", model.Kind(), err)
	}
	v, err = st.designs.CommitArtifactWithProvenance(h.ResourceAccessContext(t, fmt.Sprintf("arrange-commit-%d", model.Kind())),
		pid, v, model.Kind(), committedByActor, "project-manager")
	if err != nil {
		t.Fatalf("arrange: commit %v: %v", model.Kind(), err)
	}
	return v
}

// ---------------------------------------------------------------------------
// Probes: the named collaborator's read-only op on this scenario's stack.
// ---------------------------------------------------------------------------

func readCycle(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, customerID, cycle string) (any, error) {
	t.Helper()
	r := refOf(t, customerID, cycle)
	return stackOf(t, subject).billingState.ReadCycle(h.ResourceAccessContext(t, "probe-read-cycle"), r.CustomerID, r.CycleID)
}

func readBilling(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, customerID string) (any, error) {
	t.Helper()
	r := refOf(t, customerID, "")
	return stackOf(t, subject).billingState.ReadBilling(h.ResourceAccessContext(t, "probe-read-billing"), r.CustomerID)
}

func readDelinquent(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, project string) (any, error) {
	t.Helper()
	return stackOf(t, subject).billingState.ReadPersistentlyDelinquentCustomers(
		h.ResourceAccessContext(t, "probe-read-delinquent"), billingstate.DelinquencyScope{ProjectID: project})
}

// ---------------------------------------------------------------------------
// Bill the User for Usage.
// ---------------------------------------------------------------------------

// stepBillTheUserForUsageP1S1 — Bill the User for Usage — [yes], step 1: CloseBillingCycle
// An onboarded customer with a validated instrument and metered usage: the charge goes
// through on the first attempt.
func stepBillTheUserForUsageP1S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputBillTheUserForUsageP1S1) (any, error) {
	t.Helper()
	st := stackOf(t, subject)
	r := cycleRef(in)
	arrangeOperated(t, h, st, r, billingProject)
	arrangeInstrument(t, h, st, r)
	arrangeUsage(t, h, st, r)
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.CloseBillingCycle(rc, in.CustomerID, in.CycleID)
}

// probeBillTheUserForUsageP1S1N1 — Bill the User for Usage — [yes], step 1, probe 1: billingStateAccess.ReadCycle proves period-closed-paid.
func probeBillTheUserForUsageP1S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readCycle(t, h, subject, "0b1a0000-0000-4000-8000-000000000101", closedCycle)
}

// stepBillTheUserForUsageP2S1 — Bill the User for Usage — [no] · [no], step 1: CloseBillingCycle
// An onboarded customer whose gateway holds no validated instrument: every charge
// attempt declines, the retry budget runs out, and the close records the persistent
// decline.
func stepBillTheUserForUsageP2S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputBillTheUserForUsageP2S1) (any, error) {
	t.Helper()
	st := stackOf(t, subject)
	r := cycleRef(in)
	arrangeOperated(t, h, st, r, billP2Project)
	arrangeUsage(t, h, st, r)
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.CloseBillingCycle(rc, in.CustomerID, in.CycleID)
}

// probeBillTheUserForUsageP2S1N1 — Bill the User for Usage — [no] · [no], step 1, probe 1: billingStateAccess.ReadCycle proves period-closed-unpaid.
func probeBillTheUserForUsageP2S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readCycle(t, h, subject, "0b1a0000-0000-4000-8000-000000000102", closedCycle)
}

// probeBillTheUserForUsageP2S1N3 — Bill the User for Usage — [no] · [no], step 1, probe 3: billingStateAccess.ReadPersistentlyDelinquentCustomers proves customer-delinquent.
func probeBillTheUserForUsageP2S1N3(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readDelinquent(t, h, subject, billP2Project)
}

// stepBillTheUserForUsageP3S1 — Bill the User for Usage — [no] · [yes] · [yes], step 1: CloseBillingCycle
// The close's first charge must decline and its in-close retry land (noDeclineThenAccept).
func stepBillTheUserForUsageP3S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputBillTheUserForUsageP3S1) (any, error) {
	t.Helper()
	st := stackOf(t, subject)
	r := cycleRef(in)
	arrangeOperated(t, h, st, r, billingProject)
	arrangeUsage(t, h, st, r)
	unarrangeable(t, noDeclineThenAccept)
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.CloseBillingCycle(rc, in.CustomerID, in.CycleID)
}

// probeBillTheUserForUsageP3S1N1 — Bill the User for Usage — [no] · [yes] · [yes], step 1, probe 1: billingStateAccess.ReadCycle proves period-closed-paid.
func probeBillTheUserForUsageP3S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readCycle(t, h, subject, "0b1a0000-0000-4000-8000-000000000103", closedCycle)
}

// ---------------------------------------------------------------------------
// Onboard a New Customer.
// ---------------------------------------------------------------------------

// stepOnboardANewCustomerP1S1 — Onboard a New Customer — [yes], step 1: OnboardPaymentIntegration
// The deployed app's customer is registered and not yet gateway-bound: onboarding binds
// the gateway and registers the cycle-close schedule.
func stepOnboardANewCustomerP1S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputOnboardANewCustomerP1S1) (any, error) {
	t.Helper()
	arrangeRegistered(t, h, stackOf(t, subject), refOf(t, in.DeployedAppID.String(), ""), billingProject)
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.OnboardPaymentIntegration(rc, in.DeployedAppID)
}

// probeOnboardANewCustomerP1S1N1 — Onboard a New Customer — [yes], step 1, probe 1: billingStateAccess.ReadBilling proves obj-gateway-bound.
func probeOnboardANewCustomerP1S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readBilling(t, h, subject, "0b1a0000-0000-4000-8000-000000000201")
}

// stepOnboardANewCustomerP2S1 — Onboard a New Customer — [no], step 1: OnboardPaymentIntegration
// Nothing is arranged: the deployed app's customer has no billing record yet.
func stepOnboardANewCustomerP2S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputOnboardANewCustomerP2S1) (any, error) {
	t.Helper()
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.OnboardPaymentIntegration(rc, in.DeployedAppID)
}

// stepOnboardANewCustomerP2S2 — Onboard a New Customer — [no], step 2: RegisterCustomer
// The customer registers for a project whose SDP option is committed: the registration
// validates the instrument, opens the record under the committed option's terms and
// completes the onboarding step 1 left waiting.
func stepOnboardANewCustomerP2S2(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputOnboardANewCustomerP2S2) (any, error) {
	t.Helper()
	arrangeCommittedOption(t, h, stackOf(t, subject), in.ProjectID)
	rc := callContext(t, h, "2")
	rc.Context = bounded(rc.Context, t)
	return subject.RegisterCustomer(rc, in.CustomerID, in.ProjectID)
}

// probeOnboardANewCustomerP2S2N1 — Onboard a New Customer — [no], step 2, probe 1: billingStateAccess.ReadBilling proves obj-customer-registered.
func probeOnboardANewCustomerP2S2N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readBilling(t, h, subject, "0b1a0000-0000-4000-8000-000000000202")
}

// probeOnboardANewCustomerP2S2N2 — Onboard a New Customer — [no], step 2, probe 2: billingStateAccess.ReadBilling proves obj-gateway-bound.
func probeOnboardANewCustomerP2S2N2(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readBilling(t, h, subject, "0b1a0000-0000-4000-8000-000000000202")
}

// ---------------------------------------------------------------------------
// Retry a Declined Service Invoice.
//
// The sweep's policy (billingManager's own Tiered configuration) retries a declined
// charge while fewer than 2 attempts have been made, then leaves it for the next sweep
// while it has aged fewer than 3 sweeps, then escalates it. Each scenario arranges the
// declined cycle's retry history to steer it (note-retry-history).
// ---------------------------------------------------------------------------

// stepRetryADeclinedServiceInvoiceP1S1 — Retry a Declined Service Invoice — [yes: retry] · [yes], step 1: RunShortfallSweep
// The customer's August charge declined once and the customer has since fixed its
// instrument: the sweep's re-charge, under the second attempt's key, goes through.
func stepRetryADeclinedServiceInvoiceP1S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputRetryADeclinedServiceInvoiceP1S1) (any, error) {
	t.Helper()
	st := stackOf(t, subject)
	r := refOf(t, "0b1a0000-0000-4000-8000-000000000301", declinedCycle)
	arrangeDeclined(t, h, st, r, billingProject, 1, 0)
	arrangeInstrument(t, h, st, r)
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.RunShortfallSweep(rc, in.TickID)
}

// probeRetryADeclinedServiceInvoiceP1S1N1 — Retry a Declined Service Invoice — [yes: retry] · [yes], step 1, probe 1: billingStateAccess.ReadCycle proves cycle-settled.
func probeRetryADeclinedServiceInvoiceP1S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readCycle(t, h, subject, "0b1a0000-0000-4000-8000-000000000301", declinedCycle)
}

// stepRetryADeclinedServiceInvoiceP2S1 — Retry a Declined Service Invoice — [no: exhausted] · [yes: delay], step 1: RunShortfallSweep
// The customer's August charge declined on both attempts its budget allows and has aged
// no sweep: the sweep leaves it declined for the next one, a sweep older.
func stepRetryADeclinedServiceInvoiceP2S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputRetryADeclinedServiceInvoiceP2S1) (any, error) {
	t.Helper()
	arrangeDeclined(t, h, stackOf(t, subject), refOf(t, "0b1a0000-0000-4000-8000-000000000302", declinedCycle), billingProject, 2, 0)
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.RunShortfallSweep(rc, in.TickID)
}

// probeRetryADeclinedServiceInvoiceP2S1N1 — Retry a Declined Service Invoice — [no: exhausted] · [yes: delay], step 1, probe 1: billingStateAccess.ReadCycle proves cycle-awaiting-sweep.
func probeRetryADeclinedServiceInvoiceP2S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readCycle(t, h, subject, "0b1a0000-0000-4000-8000-000000000302", declinedCycle)
}

// stepRetryADeclinedServiceInvoiceP3S1 — Retry a Declined Service Invoice — [no: exhausted] · [no: aged out], step 1: RunShortfallSweep
// The customer's August charge declined on both attempts its budget allows and has aged
// its three sweeps of tolerance, under the project the delinquency probe scopes by: the
// sweep escalates the cycle and signals the now delinquent customer to operations.
func stepRetryADeclinedServiceInvoiceP3S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputRetryADeclinedServiceInvoiceP3S1) (any, error) {
	t.Helper()
	arrangeDeclined(t, h, stackOf(t, subject), refOf(t, "0b1a0000-0000-4000-8000-000000000303", declinedCycle), retryP3Project, 2, 3)
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.RunShortfallSweep(rc, in.TickID)
}

// probeRetryADeclinedServiceInvoiceP3S1N1 — Retry a Declined Service Invoice — [no: exhausted] · [no: aged out], step 1, probe 1: billingStateAccess.ReadPersistentlyDelinquentCustomers proves customer-delinquent.
func probeRetryADeclinedServiceInvoiceP3S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readDelinquent(t, h, subject, retryP3Project)
}

// stepRetryADeclinedServiceInvoiceP4S1 — Retry a Declined Service Invoice — [yes: retry] · [no: declined again] · [yes: retry] · [yes], step 1: RunShortfallSweep
// The sweep's first re-charge must decline and its second land (noDeclineThenAccept).
func stepRetryADeclinedServiceInvoiceP4S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputRetryADeclinedServiceInvoiceP4S1) (any, error) {
	t.Helper()
	arrangeDeclined(t, h, stackOf(t, subject), refOf(t, "0b1a0000-0000-4000-8000-000000000304", declinedCycle), billingProject, 0, 0)
	unarrangeable(t, noDeclineThenAccept)
	rc := callContext(t, h, "1")
	rc.Context = bounded(rc.Context, t)
	return subject.RunShortfallSweep(rc, in.TickID)
}

// probeRetryADeclinedServiceInvoiceP4S1N1 — Retry a Declined Service Invoice — [yes: retry] · [no: declined again] · [yes: retry] · [yes], step 1, probe 1: billingStateAccess.ReadCycle proves cycle-settled.
func probeRetryADeclinedServiceInvoiceP4S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readCycle(t, h, subject, "0b1a0000-0000-4000-8000-000000000304", declinedCycle)
}

// ---------------------------------------------------------------------------
// View a Billing Cycle.
// ---------------------------------------------------------------------------

// stepViewABillingCycleP1S1 — View a Billing Cycle — [no: period open], step 1: GetBillingCycle
// An onboarded customer whose October period nothing has closed yet.
func stepViewABillingCycleP1S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputViewABillingCycleP1S1) (any, error) {
	t.Helper()
	arrangeOperated(t, h, stackOf(t, subject), cycleRef(in), billingProject)
	return subject.GetBillingCycle(callContext(t, h, "1"), in.CustomerID, in.CycleID)
}

// probeViewABillingCycleP1S1N1 — View a Billing Cycle — [no: period open], step 1, probe 1: billingStateAccess.ReadCycle proves send-cycle-open.
func probeViewABillingCycleP1S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readCycle(t, h, subject, "0b1a0000-0000-4000-8000-000000000401", openCycle)
}

// stepViewABillingCycleP2S1 — View a Billing Cycle — [yes], step 1: GetBillingCycle
// An onboarded customer whose September cycle settled paid, as a charge on its first
// attempt.
func stepViewABillingCycleP2S1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager, in inputViewABillingCycleP2S1) (any, error) {
	t.Helper()
	st := stackOf(t, subject)
	r := cycleRef(in)
	arrangeSettled(t, h, st, r, arrangeOperated(t, h, st, r, billingProject), billingstate.BillingOutcome{
		Net: chargeNet, Directive: billingstate.RoutingCharge, Attempts: 1,
	})
	return subject.GetBillingCycle(callContext(t, h, "1"), in.CustomerID, in.CycleID)
}

// probeViewABillingCycleP2S1N1 — View a Billing Cycle — [yes], step 1, probe 1: billingStateAccess.ReadCycle proves send-cycle-settlement.
func probeViewABillingCycleP2S1N1(t *testing.T, h *scenariohost.Host, subject billing.BillingManager) (any, error) {
	t.Helper()
	return readCycle(t, h, subject, "0b1a0000-0000-4000-8000-000000000402", closedCycle)
}
