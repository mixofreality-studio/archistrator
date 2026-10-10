// Hooks for mcpClientBilling scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file.
//
// The subject is the REAL billing tool surface: the generated MCP tools Handler over
// the REAL billingManager, built through its generated public constructor
// (billing.NewBillingManager) and served by a real worker (billing.RegisterManagerWorker)
// on the scenario host's Temporal dev server — the wiring cmd/server/main.gen.go
// performs. Every collaborator is real and built through its own public constructor
// (DCT §12 B2); nothing is doubled:
//
//   - billingStateAccess, merchantGatewayAccess and usageAccess are the Postgres
//     implementations, each scenario on its OWN Postgres (bindings of different
//     scenarios never share a customer, but a re-run of one scenario must start from
//     the empty aggregate its guards name);
//   - projectStateAccess is the git-local implementation over the scenario's own bare
//     repository, holding the fixture project committed as the store's own document
//     (projectstate.EncodeProjectJSON): its SDP review is committed with a chosen
//     option and its planning assumptions carry the terms billing registers a customer
//     under (DCT §12 B13);
//   - messageBus is the Temporal implementation over the host's client with the
//     billing half of the composition root's kind table;
//   - billingEngine and interventionEngine are the real pure engines.
//
// Arrangement goes through the subject's own tools wherever the use case has one
// (billingRegisterCustomer, billingOnboardPaymentIntegration), and otherwise through
// the actor that owns the state: usage through usageAccess's own write verb (the
// operations reconcile records it), a closed period through billingManager's
// CloseBillingCycle (what the closeBillingCycle:<customer> schedule fires). Every probe
// calls the named component's read-only op on this scenario's stack.

package billing_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	ghtestinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	pgtestinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	mcpbilling "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/billing"
	billingengine "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// Identities and periods the test plan's bindings and probes name.
const (
	onboardP1Customer = "0b1a0000-0000-4000-8000-000000000301"
	onboardP2Customer = "0b1a0000-0000-4000-8000-000000000302"
	viewP1Customer    = "0b1a0000-0000-4000-8000-000000000501"
	viewP2Customer    = "0b1a0000-0000-4000-8000-000000000502"

	openCycle   = "2026-10" // a period nothing has closed
	closedCycle = "2026-09" // a period whose close charged the customer

	// fixtureProject is the project every customer here registers to be billed for:
	// the one onboard-a-new-customer-P2 names. The git-local store's identity is the
	// repository, so each scenario's repository holds exactly this project.
	fixtureProject = "stp-mcpbilling-onboard-P2-project"

	// stepTimeout bounds one tool or manager call, so a workflow that cannot finish
	// fails its scenario instead of hanging the package.
	stepTimeout = 90 * time.Second
)

// messageBusKinds is the billing half of the composition root's messageBus kind table
// (cmd/server/hooks.go MessageBusTemporalArgs): the ExecutionKinds billingManager
// registers Schedules for, bound to its workflow types and task queue.
var messageBusKinds = map[messagebus.ExecutionKind]messagebus.KindBinding{
	"billingShortfallSweep": {WorkflowType: "billingShortfallSweep", TaskQueue: billing.TaskQueue},
	"billingCloseCycle":     {WorkflowType: "billingCloseCycle", TaskQueue: billing.TaskQueue},
}

// stack is one scenario's downstream wiring, kept so step and probe hooks reach the
// same instances the subject's manager and worker run against.
type stack struct {
	manager         billing.BillingManager
	billingState    billingstate.BillingStateAccess
	merchantGateway merchantgateway.MerchantGatewayAccess
	usage           usage.UsageAccess
	messageBus      messagebus.MessageBus
}

var (
	stacksMu sync.Mutex
	stacks   = map[*testing.T]*stack{}
)

// stackOf returns the stack newSubject built for this scenario's test.
func stackOf(t *testing.T) *stack {
	t.Helper()
	stacksMu.Lock()
	defer stacksMu.Unlock()
	s, ok := stacks[t]
	if !ok {
		t.Fatal("hooks: no stack: newSubject did not run for this scenario")
	}
	return s
}

// newSubject builds the in-process MCP server the scenarios call: the Handler
// of every generated MCP tools package serving the tools below, bound to real
// managers on the host's stack, and Context when the tools need an identity
// (the principal they read from it).
// Tools: billingGetBillingCycle, billingOnboardPaymentIntegration, billingRegisterCustomer.
//
// The billing tools read no principal, so Context stays nil (the host's).
func newSubject(t *testing.T, h *scenariohost.Host) toolServer {
	t.Helper()
	ctx := h.Context(t)

	pool := pgtestinfra.StartPostgres(t)
	billingState, err := billingstate.NewPostgresBillingStateAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: billingStateAccess: %v", err)
	}
	merchantGateway, err := merchantgateway.NewPostgresMerchantGatewayAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: merchantGatewayAccess: %v", err)
	}
	usageAccess, err := usage.NewPostgresUsageAccess(ctx, pool)
	if err != nil {
		t.Fatalf("hooks: usageAccess: %v", err)
	}
	projectRepo := ghtestinfra.StartLocalGitRepo(t, "main")
	commitProject(t, projectRepo, committedProject())
	projects := projectstate.NewGitLocalProjectStateAccess(projectRepo.URL)
	bus := messagebus.NewTemporalMessageBus(h.TemporalClient(), messageBusKinds)

	manager := billing.NewBillingManager(h.TemporalClient(), billingState, usageAccess, merchantGateway, bus,
		billingengine.NewBillingEngine(), intervention.NewInterventionEngine(), projects)
	h.StartWorker(t, billing.TaskQueue, func(w scenariohost.Worker) { billing.RegisterManagerWorker(w, manager) })

	stacksMu.Lock()
	stacks[t] = &stack{manager: manager, billingState: billingState, merchantGateway: merchantGateway, usage: usageAccess, messageBus: bus}
	stacksMu.Unlock()
	t.Cleanup(func() {
		stacksMu.Lock()
		delete(stacks, t)
		stacksMu.Unlock()
	})
	return toolServer{Handlers: []toolRegistrar{&mcpbilling.Handler{Manager: manager}}}
}

// committedProject is the fixture project as its store holds it once its SDP review
// is committed: option "normal" chosen, priced under flat-markup monthly terms in USD.
func committedProject() projectstate.Project {
	chosen := projectstate.OptionID("normal")
	return projectstate.Project{
		ID:      projectstate.ProjectID(fixtureProject),
		Name:    "STP billing fixture",
		Version: 1,
		Phase:   projectstate.PhaseConstruction,
		PlanningAssumptions: projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 1, Model: &projectstate.PlanningAssumptions{
			Terms: projectstate.SettlementTerms{
				ComputeCost:          projectstate.ComputeCostFlatMarkup,
				ComputeMarkupPercent: 20,
				Schedule:             projectstate.ScheduleMonthly,
			},
			IndirectDailyRate: projectstate.Money{MinorUnits: 100000, Currency: "USD"},
		}},
		SdpReview: projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 1, Model: &projectstate.SdpReview{
			Options:        []projectstate.SdpOptionRow{{OptionID: chosen, SolutionKind: projectstate.KindNormalSolution}},
			Recommendation: chosen,
			Rationale:      "the normal solution meets the deadline at the lowest risk",
			ChosenOption:   &chosen,
		}},
	}
}

// commitProject commits p to repo's main branch as the store's own document
// (.aiarch/state/project.json): the Resource, not the ResourceAccess, is what the
// arrangement writes.
func commitProject(t *testing.T, repo ghtestinfra.LocalGitRepo, p projectstate.Project) {
	t.Helper()
	raw, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("hooks: encode project %s: %v", p.ID, err)
	}
	work := filepath.Join(t.TempDir(), "arrange")
	git(t, "", "clone", "--quiet", repo.Dir, work)
	stateDir := filepath.Join(work, ".aiarch", "state")
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "project.json"), raw, 0o600); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	git(t, work, "add", ".aiarch/state/project.json")
	git(t, work, "-c", "user.name=scenario-arrange", "-c", "user.email=arrange@aiarch.local",
		"-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "arrange "+string(p.ID))
	git(t, work, "push", "--quiet", "origin", "main")
}

// git runs one git command in dir.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hooks: git %v: %v\n%s", args, err, out)
	}
}

// callTool calls one tool on the subject, bounded by stepTimeout.
func callTool(t *testing.T, h *scenariohost.Host, subject *toolSession, name, args string) (any, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.Context(t), stepTimeout)
	defer cancel()
	return subject.Call(ctx, name, args)
}

// customer is the billing aggregate identity a binding names, decoded the way the
// contract carries it (a JSON uuid).
func customer(t *testing.T, id string) billingstate.Billing {
	t.Helper()
	var b billingstate.Billing
	mustUnmarshal(t, `{"ID":"`+id+`"}`, &b)
	return b
}

// arrangeTool calls one of the subject's own tools as an arrangement: it must succeed.
func arrangeTool(t *testing.T, h *scenariohost.Host, subject *toolSession, name, args string) {
	t.Helper()
	if _, err := callTool(t, h, subject, name, args); err != nil {
		t.Fatalf("hooks: arrange %s %s: %v", name, args, err)
	}
}

// arrangeRegistered registers the customer for the fixture project through the
// subject's own billingRegisterCustomer tool: its instrument validated and its billing
// aggregate opened under the project's committed terms.
func arrangeRegistered(t *testing.T, h *scenariohost.Host, subject *toolSession, id string) {
	t.Helper()
	arrangeTool(t, h, subject, "billingRegisterCustomer", `{"customerID":"`+id+`","projectID":"`+fixtureProject+`"}`)
}

// arrangeCharged takes the customer through a paid close of cycle: registered and
// onboarded through the subject's own tools (gateway bound, close schedule
// registered), an hour of compute recorded for the period through usageAccess's own
// write verb, and the period closed by billingManager.CloseBillingCycle — what the
// customer's closeBillingCycle schedule fires. The shortfall routes to a charge on the
// validated instrument, which the gateway accepts.
func arrangeCharged(t *testing.T, h *scenariohost.Host, subject *toolSession, id, cycle string) {
	t.Helper()
	s := stackOf(t)
	arrangeRegistered(t, h, subject, id)
	arrangeTool(t, h, subject, "billingOnboardPaymentIntegration", `{"deployedAppID":"`+id+`"}`)

	start := time.Date(2026, time.September, 15, 9, 0, 0, 0, time.UTC)
	if _, err := s.usage.RecordComputeUsage(h.ResourceAccessContext(t, "arrange-usage"), []usage.UsageEvent{{
		CustomerID:     customer(t, id).ID,
		OperatedAppID:  customer(t, id).ID,
		CycleID:        usage.CycleID(cycle),
		Units:          usage.ComputeUnits{Amount: 3600, Unit: "cpu-seconds"},
		RuntimeEventID: usage.RuntimeEventID("stp-mcpbilling-usage-" + id + "-" + cycle),
		WindowStart:    start,
		WindowEnd:      start.Add(time.Hour),
		OccurredAt:     start.Add(time.Hour),
	}}); err != nil {
		t.Fatalf("hooks: arrange usage %s %s: %v", id, cycle, err)
	}

	ctx, cancel := context.WithTimeout(h.Context(t), stepTimeout)
	defer cancel()
	rc := h.ManagerContext(t)
	rc.Context = ctx
	res, err := s.manager.CloseBillingCycle(rc, customer(t, id).ID, cycle)
	if err != nil {
		t.Fatalf("hooks: arrange close %s %s: %v", id, cycle, err)
	}
	if res.Routed != billing.RoutingDirectiveCharge || res.Escalated {
		t.Fatalf("hooks: arrange close %s %s: routed %v escalated %v, want a charge that went through", id, cycle, res.Routed, res.Escalated)
	}
}

// readBilling is the billingStateAccess.ReadBilling probe on this scenario's stack.
func readBilling(t *testing.T, h *scenariohost.Host, id string) (any, error) {
	t.Helper()
	return stackOf(t).billingState.ReadBilling(h.ResourceAccessContext(t, "probe-read-billing"), customer(t, id).ID)
}

// readCycle is the billingStateAccess.ReadCycle probe on this scenario's stack.
func readCycle(t *testing.T, h *scenariohost.Host, id, cycle string) (any, error) {
	t.Helper()
	return stackOf(t).billingState.ReadCycle(h.ResourceAccessContext(t, "probe-read-cycle"), customer(t, id).ID, cycle)
}

// stepOnboardANewCustomerP1S1 — Onboard a New Customer — [yes], step 1: BillingOnboardPaymentIntegration (tool billingOnboardPaymentIntegration)
// Projected stimulus (JSON): {"seq":1,"node":"resolve-app","input":{"kind":"action","from":"architect-user","to":"mcpclientbilling","op":"BillingOnboardPaymentIntegration","call":"onboard-a-new-customer/resolve-app/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"BillingOnboardPaymentIntegration","inputs":[{"name":"deployedAppID","value":"0b1a0000-0000-4000-8000-000000000301"}],"expect":{"result":"{\"result\":{\"customerId\":\"0b1a0000-0000-4000-8000-000000000301\"}}","errorExpected":false},"probes":[{"for":"obj-gateway-bound","component":"billingStateAccess","operation":"ReadBilling","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000301"}],"expect":{"result":"{\"ID\":\"0b1a0000-0000-4000-8000-000000000301\",\"Registered\":true,\"GatewayBound\":true}","errorExpected":false}},{"for":"obj-schedule-registered","component":"messageBus","operation":"DescribeSchedule","inputs":[{"name":"scheduleID","value":"closeBillingCycle:0b1a0000-0000-4000-8000-000000000301","schemaRef":"#/$defs/ScheduleID"}],"expect":{"result":"{\"Registered\":true}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranged: registered=[yes] — the deployed app's customer registered for the fixture
// project, so onboarding resolves it, binds its gateway and registers its schedule.
func stepOnboardANewCustomerP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	arrangeRegistered(t, h, subject, onboardP1Customer)
	return callTool(t, h, subject, "billingOnboardPaymentIntegration", args)
}

// probeOnboardANewCustomerP1S1N1 — Onboard a New Customer — [yes], step 1, probe 1: billingStateAccess.ReadBilling proves obj-gateway-bound.
// Probe (JSON): {"for":"obj-gateway-bound","component":"billingStateAccess","operation":"ReadBilling","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000301"}],"expect":{"result":"{\"ID\":\"0b1a0000-0000-4000-8000-000000000301\",\"Registered\":true,\"GatewayBound\":true}","errorExpected":false}}
func probeOnboardANewCustomerP1S1N1(t *testing.T, h *scenariohost.Host, _ *toolSession) (any, error) {
	t.Helper()
	return readBilling(t, h, onboardP1Customer)
}

// probeOnboardANewCustomerP1S1N2 — Onboard a New Customer — [yes], step 1, probe 2: messageBus.DescribeSchedule proves obj-schedule-registered.
// Probe (JSON): {"for":"obj-schedule-registered","component":"messageBus","operation":"DescribeSchedule","inputs":[{"name":"scheduleID","value":"closeBillingCycle:0b1a0000-0000-4000-8000-000000000301","schemaRef":"#/$defs/ScheduleID"}],"expect":{"result":"{\"Registered\":true}","errorExpected":false}}
func probeOnboardANewCustomerP1S1N2(t *testing.T, h *scenariohost.Host, _ *toolSession) (any, error) {
	t.Helper()
	return stackOf(t).messageBus.DescribeSchedule(h.ResourceAccessContext(t, "probe-describe-schedule"),
		messagebus.ScheduleID("closeBillingCycle:"+onboardP1Customer))
}

// stepOnboardANewCustomerP2S1 — Onboard a New Customer — [no], step 1: BillingOnboardPaymentIntegration (tool billingOnboardPaymentIntegration)
// Projected stimulus (JSON): {"seq":1,"node":"resolve-app","input":{"kind":"action","from":"architect-user","to":"mcpclientbilling","op":"BillingOnboardPaymentIntegration","call":"onboard-a-new-customer/resolve-app/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"BillingOnboardPaymentIntegration","inputs":[{"name":"deployedAppID","value":"0b1a0000-0000-4000-8000-000000000302"}],"expect":{"errorExpected":true,"errorCode":"FailedPrecondition"},"probes":[{"for":"registered","component":"billingStateAccess","operation":"ReadBilling","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000302"}],"expect":{"result":"{\"Registered\":false,\"GatewayBound\":false}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranged: registered=[no] — nothing is arranged: the deployed app's customer has no
// billing aggregate on this scenario's empty stack.
func stepOnboardANewCustomerP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return callTool(t, h, subject, "billingOnboardPaymentIntegration", args)
}

// probeOnboardANewCustomerP2S1N1 — Onboard a New Customer — [no], step 1, probe 1: billingStateAccess.ReadBilling proves registered.
// Probe (JSON): {"for":"registered","component":"billingStateAccess","operation":"ReadBilling","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000302"}],"expect":{"result":"{\"Registered\":false,\"GatewayBound\":false}","errorExpected":false}}
func probeOnboardANewCustomerP2S1N1(t *testing.T, h *scenariohost.Host, _ *toolSession) (any, error) {
	t.Helper()
	return readBilling(t, h, onboardP2Customer)
}

// stepOnboardANewCustomerP2S2 — Onboard a New Customer — [no], step 2: BillingRegisterCustomer (tool billingRegisterCustomer)
// Projected stimulus (JSON): {"seq":2,"node":"validate-instrument","input":{"kind":"action","from":"architect-user","to":"mcpclientbilling","op":"BillingRegisterCustomer","call":"onboard-a-new-customer/validate-instrument/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"BillingRegisterCustomer","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000302"},{"name":"projectID","value":"stp-mcpbilling-onboard-P2-project"}],"expect":{"result":"{\"result\":{\"customerId\":\"0b1a0000-0000-4000-8000-000000000302\"}}","errorExpected":false},"probes":[{"for":"obj-instrument-validated","component":"merchantGatewayAccess","operation":"ReadStoredInstrument","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000302"}],"expect":{"result":"{\"Validated\":true}","errorExpected":false}},{"for":"obj-customer-registered","component":"billingStateAccess","operation":"ReadBilling","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000302"}],"expect":{"result":"{\"ID\":\"0b1a0000-0000-4000-8000-000000000302\",\"Registered\":true}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranged: registered=[no] still holds — step 1 left the customer unregistered.
func stepOnboardANewCustomerP2S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return callTool(t, h, subject, "billingRegisterCustomer", args)
}

// probeOnboardANewCustomerP2S2N1 — Onboard a New Customer — [no], step 2, probe 1: merchantGatewayAccess.ReadStoredInstrument proves obj-instrument-validated.
// Probe (JSON): {"for":"obj-instrument-validated","component":"merchantGatewayAccess","operation":"ReadStoredInstrument","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000302"}],"expect":{"result":"{\"Validated\":true}","errorExpected":false}}
func probeOnboardANewCustomerP2S2N1(t *testing.T, h *scenariohost.Host, _ *toolSession) (any, error) {
	t.Helper()
	return stackOf(t).merchantGateway.ReadStoredInstrument(h.ResourceAccessContext(t, "probe-read-instrument"), customer(t, onboardP2Customer).ID)
}

// probeOnboardANewCustomerP2S2N2 — Onboard a New Customer — [no], step 2, probe 2: billingStateAccess.ReadBilling proves obj-customer-registered.
// Probe (JSON): {"for":"obj-customer-registered","component":"billingStateAccess","operation":"ReadBilling","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000302"}],"expect":{"result":"{\"ID\":\"0b1a0000-0000-4000-8000-000000000302\",\"Registered\":true}","errorExpected":false}}
func probeOnboardANewCustomerP2S2N2(t *testing.T, h *scenariohost.Host, _ *toolSession) (any, error) {
	t.Helper()
	return readBilling(t, h, onboardP2Customer)
}

// stepViewABillingCycleP1S1 — View a Billing Cycle — [no: period open], step 1: BillingGetBillingCycle (tool billingGetBillingCycle)
// Projected stimulus (JSON): {"seq":1,"node":"open-billing-cycle","input":{"kind":"action","from":"architect-user","to":"mcpclientbilling","op":"BillingGetBillingCycle","call":"view-a-billing-cycle/open-billing-cycle/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"BillingGetBillingCycle","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000501"},{"name":"cycleID","value":"2026-10"}],"expect":{"result":"{\"result\":{\"customerId\":\"0b1a0000-0000-4000-8000-000000000501\",\"cycleId\":\"2026-10\",\"closed\":false,\"routed\":0,\"escalated\":false}}","errorExpected":false},"probes":[{"for":"send-cycle-open","component":"billingStateAccess","operation":"ReadCycle","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000501"},{"name":"cycle","value":"2026-10"}],"expect":{"result":"{\"Cycle\":\"2026-10\",\"Settled\":false}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranged: cycle-closed=[no: period open] — the customer is registered and nothing has
// closed its 2026-10 period.
func stepViewABillingCycleP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	arrangeRegistered(t, h, subject, viewP1Customer)
	return callTool(t, h, subject, "billingGetBillingCycle", args)
}

// probeViewABillingCycleP1S1N1 — View a Billing Cycle — [no: period open], step 1, probe 1: billingStateAccess.ReadCycle proves send-cycle-open.
// Probe (JSON): {"for":"send-cycle-open","component":"billingStateAccess","operation":"ReadCycle","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000501"},{"name":"cycle","value":"2026-10"}],"expect":{"result":"{\"Cycle\":\"2026-10\",\"Settled\":false}","errorExpected":false}}
func probeViewABillingCycleP1S1N1(t *testing.T, h *scenariohost.Host, _ *toolSession) (any, error) {
	t.Helper()
	return readCycle(t, h, viewP1Customer, openCycle)
}

// stepViewABillingCycleP2S1 — View a Billing Cycle — [yes], step 1: BillingGetBillingCycle (tool billingGetBillingCycle)
// Projected stimulus (JSON): {"seq":1,"node":"open-billing-cycle","input":{"kind":"action","from":"architect-user","to":"mcpclientbilling","op":"BillingGetBillingCycle","call":"view-a-billing-cycle/open-billing-cycle/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"BillingGetBillingCycle","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000502"},{"name":"cycleID","value":"2026-09"}],"expect":{"result":"{\"result\":{\"customerId\":\"0b1a0000-0000-4000-8000-000000000502\",\"cycleId\":\"2026-09\",\"closed\":true,\"routed\":2,\"escalated\":false}}","errorExpected":false},"probes":[{"for":"send-cycle-settlement","component":"billingStateAccess","operation":"ReadCycle","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000502"},{"name":"cycle","value":"2026-09"}],"expect":{"result":"{\"Cycle\":\"2026-09\",\"Settled\":true,\"Outcome\":{\"Directive\":2,\"Escalated\":false}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranged: cycle-closed=[yes] — the customer's 2026-09 period closed with a charge
// that went through.
func stepViewABillingCycleP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	arrangeCharged(t, h, subject, viewP2Customer, closedCycle)
	return callTool(t, h, subject, "billingGetBillingCycle", args)
}

// probeViewABillingCycleP2S1N1 — View a Billing Cycle — [yes], step 1, probe 1: billingStateAccess.ReadCycle proves send-cycle-settlement.
// Probe (JSON): {"for":"send-cycle-settlement","component":"billingStateAccess","operation":"ReadCycle","inputs":[{"name":"customerID","value":"0b1a0000-0000-4000-8000-000000000502"},{"name":"cycle","value":"2026-09"}],"expect":{"result":"{\"Cycle\":\"2026-09\",\"Settled\":true,\"Outcome\":{\"Directive\":2,\"Escalated\":false}}","errorExpected":false}}
func probeViewABillingCycleP2S1N1(t *testing.T, h *scenariohost.Host, _ *toolSession) (any, error) {
	t.Helper()
	return readCycle(t, h, viewP2Customer, closedCycle)
}
