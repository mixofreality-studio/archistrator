// Hooks for billingStateAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package billingstate_test

import (
	"fmt"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/billingstate"
)

// newSubject builds the component under test the way the composition root does on
// both profiles: cmd/server/main.gen.go constructs billingStateAccess with the
// generated billingstate.NewPostgresBillingStateAccess over a pgx pool, which applies
// the package's own schema. Every scenario gets its own empty Postgres, so the
// delinquent set a scenario reads back holds exactly the customers its own
// arrangement and stimuli wrote (five scenarios read the same project scope).
//
// The pool comes from the platform's Postgres testinfra rather than the host's
// PostgresURL: opening a pool on a URL needs pgxpool or the postgres infrastructure
// module, neither of which a hooks file may import, while a …/testinfra package is
// admitted.
func newSubject(t *testing.T, h *scenariohost.Host) billingstate.BillingStateAccess {
	t.Helper()
	pool := testinfra.StartPostgres(t)
	subject, err := billingstate.NewPostgresBillingStateAccess(h.Context(t), pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresBillingStateAccess: %v", err)
	}
	return subject
}

// billingProject is the project every arranged customer is registered under: the
// scope the retry and delinquency scenarios read the persistently-delinquent set by.
const billingProject = "stp-billing-project-01"

// The retry scenarios read the persistently-delinquent set; the customer each one
// expects to find is fixed by its binding, so the arrangement names it here.
const (
	retryP2Customer = "2e710000-0000-4000-8000-000000000001"
	retryP3Customer = "2e720000-0000-4000-8000-000000000002"
	retryP4Customer = "2e730000-0000-4000-8000-000000000003"
	retryP1Customer = "2e740000-0000-4000-8000-000000000004"
)

// The arrangement calls the contract's own write ops with arguments decoded from
// JSON into the generated step-input types of those ops, so a hook names no type
// outside this package and the generated scenarios file.
type (
	registerArgs = inputOnboardANewCustomerP2S2 // RegisterCustomer
	bindArgs     = inputOnboardANewCustomerP1S2 // BindGatewayLive
	settleArgs   = inputBillTheUserForUsageP1S2 // SettleCycle
)

// arrangedTerms are the terms every arranged customer registers under: the committed
// option's tiered-floors regime, no markup, settled monthly in USD.
const arrangedTerms = `{"ComputeCostKind":2,"ComputeMarkupPercent":0,"ScheduleKind":1,"Currency":"USD"}`

// arrangeRegistered opens the customer's billing aggregate (version 0 -> 1) under
// billingProject and arrangedTerms, as onboarding's RegisterCustomer does.
func arrangeRegistered(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, customerID string) {
	t.Helper()
	var a registerArgs
	mustUnmarshal(t, fmt.Sprintf(`{"customerID":%q,"expectedVersion":0,"profile":{"ProjectID":%q,"Terms":%s},"idempotencyKey":"arrange-register:%s"}`, customerID, billingProject, arrangedTerms, customerID), &a)
	v, err := subject.RegisterCustomer(callContext(t, h, "arrange-register"), a.CustomerID, a.ExpectedVersion, a.Profile, a.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: RegisterCustomer %s: %v", customerID, err)
	}
	if v != 1 {
		t.Fatalf("arrange: RegisterCustomer %s: version %d, want 1", customerID, v)
	}
}

// arrangeBound registers the customer and binds its gateway live (version 2): the
// state every billing cycle starts from.
func arrangeBound(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, customerID string) {
	t.Helper()
	arrangeRegistered(t, h, subject, customerID)
	var a bindArgs
	mustUnmarshal(t, fmt.Sprintf(`{"customerID":%q,"expectedVersion":1,"binding":{"GatewayCustomerRef":"cus_%s"},"idempotencyKey":"arrange-bind:%s"}`, customerID, customerID, customerID), &a)
	v, err := subject.BindGatewayLive(callContext(t, h, "arrange-bind"), a.CustomerID, a.ExpectedVersion, a.Binding, a.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: BindGatewayLive %s: %v", customerID, err)
	}
	if v != 2 {
		t.Fatalf("arrange: BindGatewayLive %s: version %d, want 2", customerID, v)
	}
}

// arrangeSettled closes the customer's cycle (version 2 -> 3) with the period's
// priced net, 42.00 USD owed and routed to a charge, on its first attempt: declined
// leaves the charge standing declined for the shortfall sweep, sweepsAged sweeps
// later; escalated is a decline with no retry left.
func arrangeSettled(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, customerID, cycle string, declined, escalated bool, sweepsAged int) {
	t.Helper()
	var a settleArgs
	mustUnmarshal(t, fmt.Sprintf(`{"customerID":%q,"expectedVersion":2,"cycle":%q,"outcome":{"Net":{"MinorUnits":-4200,"Currency":"USD"},"Directive":2,"Declined":%t,"Escalated":%t,"Attempts":1,"SweepsAged":%d},"idempotencyKey":"arrange-settle:%s:%s"}`, customerID, cycle, declined, escalated, sweepsAged, customerID, cycle), &a)
	v, err := subject.SettleCycle(callContext(t, h, "arrange-settle"), a.CustomerID, a.ExpectedVersion, a.Cycle, a.Outcome, a.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: SettleCycle %s %s: %v", customerID, cycle, err)
	}
	if v != 3 {
		t.Fatalf("arrange: SettleCycle %s %s: version %d, want 3", customerID, cycle, v)
	}
}

// arrangeDeclined leaves the customer with its 2026-09 cycle closed as a charge that
// stands declined after one attempt, sweepsAged sweeps ago, not escalated (version
// 3): the retry candidate the shortfall sweep reads.
func arrangeDeclined(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, customerID string, sweepsAged int) {
	t.Helper()
	arrangeBound(t, h, subject, customerID)
	arrangeSettled(t, h, subject, customerID, "2026-09", true, false, sweepsAged)
}

// stepBillTheUserForUsageP1S1 — Bill the User for Usage — [yes], step 1: ReadBilling
// Projected stimulus (JSON): {"seq":1,"node":"read-hosting-usage","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadBilling","call":"bill-the-user-for-usage/read-hosting-usage/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadBilling","inputs":[{"name":"customerID","value":"b1110000-0000-4000-8000-000000000001"}],"expect":{"result":"{\"ID\":\"b1110000-0000-4000-8000-000000000001\",\"Version\":2,\"Registered\":true,\"GatewayBound\":true}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards charge-succeeded=[yes] hold; then calls the operation exactly as billingManager does.
func stepBillTheUserForUsageP1S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputBillTheUserForUsageP1S1) (any, error) {
	t.Helper()
	arrangeBound(t, h, subject, in.CustomerID.String())
	return subject.ReadBilling(callContext(t, h, "1"), in.CustomerID)
}

// stepBillTheUserForUsageP2S1 — Bill the User for Usage — [no] · [no], step 1: ReadBilling
// Projected stimulus (JSON): {"seq":1,"node":"read-hosting-usage","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadBilling","call":"bill-the-user-for-usage/read-hosting-usage/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadBilling","inputs":[{"name":"customerID","value":"b1120000-0000-4000-8000-000000000002"}],"expect":{"result":"{\"ID\":\"b1120000-0000-4000-8000-000000000002\",\"Version\":2,\"Registered\":true,\"GatewayBound\":true}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards charge-succeeded=[no], retries-left=[no] hold; then calls the operation exactly as billingManager does.
func stepBillTheUserForUsageP2S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputBillTheUserForUsageP2S1) (any, error) {
	t.Helper()
	arrangeBound(t, h, subject, in.CustomerID.String())
	return subject.ReadBilling(callContext(t, h, "1"), in.CustomerID)
}

// stepOnboardANewCustomerP1S1 — Onboard a New Customer — [yes], step 1: ReadBilling
// Projected stimulus (JSON): {"seq":1,"node":"resolve-app","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadBilling","call":"onboard-a-new-customer/resolve-app/4"},"expectedOutputs":[{"node":"resolve-app","kind":"inferred","to":"","label":"Resolve the deployed app to its billing customer"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadBilling","inputs":[{"name":"customerID","value":"0b010000-0000-4000-8000-000000000001"}],"expect":{"result":"{\"ID\":\"0b010000-0000-4000-8000-000000000001\",\"Version\":1,\"Registered\":true,\"GatewayBound\":false}","errorExpected":false},"probes":[{"for":"resolve-app","component":"billingStateAccess","operation":"ReadBilling","inputs":[{"name":"customerID","value":"0b010000-0000-4000-8000-000000000001"}],"expect":{"result":"{\"ID\":\"0b010000-0000-4000-8000-000000000001\",\"Registered\":true}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards registered=[yes] hold; then calls the operation exactly as billingManager does.
func stepOnboardANewCustomerP1S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputOnboardANewCustomerP1S1) (any, error) {
	t.Helper()
	// registered=[yes]: the customer aggregate exists, but no gateway is bound yet.
	arrangeRegistered(t, h, subject, in.CustomerID.String())
	return subject.ReadBilling(callContext(t, h, "1"), in.CustomerID)
}

// stepRetryADeclinedServiceInvoiceP2S1 — Retry a Declined Service Invoice — [yes: retry] · [yes], step 1: ReadDeclinedCycles
// Projected stimulus (JSON): {"seq":1,"node":"charge-declined","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadDeclinedCycles","call":"retry-a-declined-service-invoice/charge-declined/1.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadDeclinedCycles","inputs":[{"name":"scope","value":"{\"ProjectID\":\"stp-billing-project-01\"}"}],"expect":{"result":"[{\"CustomerID\":\"2e710000-0000-4000-8000-000000000001\",\"Cycle\":\"2026-09\",\"Outcome\":{\"Net\":{\"MinorUnits\":-4200,\"Currency\":\"USD\"},\"Directive\":2,\"Declined\":true,\"Escalated\":false,\"Attempts\":1,\"SweepsAged\":0}}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards retry-budget=[yes: retry], charge-ok=[yes] hold; then calls the operation exactly as billingManager does.
func stepRetryADeclinedServiceInvoiceP2S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputRetryADeclinedServiceInvoiceP2S1) (any, error) {
	t.Helper()
	arrangeDeclined(t, h, subject, retryP2Customer, 0)
	return subject.ReadDeclinedCycles(callContext(t, h, "1"), in.Scope)
}

// stepRetryADeclinedServiceInvoiceP3S1 — Retry a Declined Service Invoice — [no: exhausted] · [yes: delay], step 1: ReadDeclinedCycles
// Projected stimulus (JSON): {"seq":1,"node":"charge-declined","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadDeclinedCycles","call":"retry-a-declined-service-invoice/charge-declined/1.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadDeclinedCycles","inputs":[{"name":"scope","value":"{\"ProjectID\":\"stp-billing-project-01\"}"}],"expect":{"result":"[{\"CustomerID\":\"2e720000-0000-4000-8000-000000000002\",\"Cycle\":\"2026-09\",\"Outcome\":{\"Net\":{\"MinorUnits\":-4200,\"Currency\":\"USD\"},\"Directive\":2,\"Declined\":true,\"Escalated\":false,\"Attempts\":1,\"SweepsAged\":0}}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards retry-budget=[no: exhausted], tolerance=[yes: delay] hold; then calls the operation exactly as billingManager does.
func stepRetryADeclinedServiceInvoiceP3S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputRetryADeclinedServiceInvoiceP3S1) (any, error) {
	t.Helper()
	arrangeDeclined(t, h, subject, retryP3Customer, 0)
	return subject.ReadDeclinedCycles(callContext(t, h, "1"), in.Scope)
}

// stepRetryADeclinedServiceInvoiceP3S3 — Retry a Declined Service Invoice — [no: exhausted] · [yes: delay], step 3: ResettleCycle
// Projected stimulus (JSON): {"seq":4,"node":"delay-sweep","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ResettleCycle","call":"retry-a-declined-service-invoice/delay-sweep/0"},"expectedOutputs":null,"expectedStates":[{"node":"cycle-awaiting-sweep","object":"SettlementCycle","inState":"[declined, shortfall awaiting next sweep]"}],"hints":null}
// Binding (JSON): {"seq":4,"operation":"ResettleCycle","inputs":[{"name":"customerID","value":"2e720000-0000-4000-8000-000000000002"},{"name":"expectedVersion","value":"3"},{"name":"cycle","value":"2026-09"},{"name":"correction","value":"{\"Net\":{\"MinorUnits\":-4200,\"Currency\":\"USD\"},\"Directive\":2,\"Declined\":true,\"Escalated\":false,\"Attempts\":1,\"SweepsAged\":1}"},{"name":"idempotencyKey","value":"sweep-state:2e720000-0000-4000-8000-000000000002:2026-09:1"}],"expect":{"result":"4","errorExpected":false},"probes":[{"for":"cycle-awaiting-sweep","component":"billingStateAccess","operation":"ReadCycle","inputs":[{"name":"customerID","value":"2e720000-0000-4000-8000-000000000002"},{"name":"cycle","value":"2026-09"}],"expect":{"result":"{\"Cycle\":\"2026-09\",\"Settled\":true,\"Outcome\":{\"Escalated\":false,\"Declined\":true,\"Attempts\":1,\"SweepsAged\":1}}","errorExpected":false}},{"for":"cycle-awaiting-sweep","component":"billingStateAccess","operation":"ReadPersistentlyDelinquentCustomers","inputs":[{"name":"scope","value":"{\"ProjectID\":\"stp-billing-project-01\"}"}],"expect":{"result":"[]","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards retry-budget=[no: exhausted], tolerance=[yes: delay] hold; then calls the operation exactly as billingManager does.
func stepRetryADeclinedServiceInvoiceP3S4(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputRetryADeclinedServiceInvoiceP3S4) (any, error) {
	t.Helper()
	// retry budget exhausted, still inside tolerance: the cycle is re-settled still
	// declined, one sweep older, leaving the shortfall for the next sweep.
	return subject.ResettleCycle(callContext(t, h, "4"), in.CustomerID, in.ExpectedVersion, in.Cycle, in.Correction, in.IdempotencyKey)
}

// stepRetryADeclinedServiceInvoiceP4S1 — Retry a Declined Service Invoice — [no: exhausted] · [no: aged out], step 1: ReadDeclinedCycles
// Projected stimulus (JSON): {"seq":1,"node":"charge-declined","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadDeclinedCycles","call":"retry-a-declined-service-invoice/charge-declined/1.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadDeclinedCycles","inputs":[{"name":"scope","value":"{\"ProjectID\":\"stp-billing-project-01\"}"}],"expect":{"result":"[{\"CustomerID\":\"2e730000-0000-4000-8000-000000000003\",\"Cycle\":\"2026-09\",\"Outcome\":{\"Net\":{\"MinorUnits\":-4200,\"Currency\":\"USD\"},\"Directive\":2,\"Declined\":true,\"Escalated\":false,\"Attempts\":1,\"SweepsAged\":2}}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards retry-budget=[no: exhausted], tolerance=[no: aged out] hold; then calls the operation exactly as billingManager does.
func stepRetryADeclinedServiceInvoiceP4S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputRetryADeclinedServiceInvoiceP4S1) (any, error) {
	t.Helper()
	arrangeDeclined(t, h, subject, retryP4Customer, 2)
	return subject.ReadDeclinedCycles(callContext(t, h, "1"), in.Scope)
}

// stepRetryADeclinedServiceInvoiceP4S3 — Retry a Declined Service Invoice — [no: exhausted] · [no: aged out], step 3: ResettleCycle
// Projected stimulus (JSON): {"seq":4,"node":"escalate-delinquent","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ResettleCycle","call":"retry-a-declined-service-invoice/escalate-delinquent/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"ResettleCycle","inputs":[{"name":"customerID","value":"2e730000-0000-4000-8000-000000000003"},{"name":"expectedVersion","value":"3"},{"name":"cycle","value":"2026-09"},{"name":"correction","value":"{\"Net\":{\"MinorUnits\":-4200,\"Currency\":\"USD\"},\"Directive\":2,\"Declined\":true,\"Escalated\":true,\"Attempts\":1,\"SweepsAged\":3}"},{"name":"idempotencyKey","value":"escalate-state:2e730000-0000-4000-8000-000000000003:2026-09"}],"expect":{"result":"4","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards retry-budget=[no: exhausted], tolerance=[no: aged out] hold; then calls the operation exactly as billingManager does.
func stepRetryADeclinedServiceInvoiceP4S4(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputRetryADeclinedServiceInvoiceP4S4) (any, error) {
	t.Helper()
	// retry budget exhausted and aged out of tolerance: the cycle is re-settled as an
	// escalated outcome (the escalation to a human is the persistent-delinquency mark).
	return subject.ResettleCycle(callContext(t, h, "4"), in.CustomerID, in.ExpectedVersion, in.Cycle, in.Correction, in.IdempotencyKey)
}

// stepRetryADeclinedServiceInvoiceP1S1 — Retry a Declined Service Invoice — [yes: retry] · [no: declined again], step 1: ReadDeclinedCycles
// Projected stimulus (JSON): {"seq":1,"node":"charge-declined","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadDeclinedCycles","call":"retry-a-declined-service-invoice/charge-declined/1.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadDeclinedCycles","inputs":[{"name":"scope","value":"{\"ProjectID\":\"stp-billing-project-01\"}"}],"expect":{"result":"[{\"CustomerID\":\"2e740000-0000-4000-8000-000000000004\",\"Cycle\":\"2026-09\",\"Outcome\":{\"Net\":{\"MinorUnits\":-4200,\"Currency\":\"USD\"},\"Directive\":2,\"Declined\":true,\"Escalated\":false,\"Attempts\":1,\"SweepsAged\":0}}]","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards retry-budget=[yes: retry], charge-ok=[no: declined again] hold; then calls the operation exactly as billingManager does.
func stepRetryADeclinedServiceInvoiceP1S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputRetryADeclinedServiceInvoiceP1S1) (any, error) {
	t.Helper()
	arrangeDeclined(t, h, subject, retryP1Customer, 0)
	return subject.ReadDeclinedCycles(callContext(t, h, "1"), in.Scope)
}

// stepViewABillingCycleP1S1 — View a Billing Cycle — [no: period open], step 1: ReadCycle
// Projected stimulus (JSON): {"seq":1,"node":"read-billing-cycle","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadCycle","call":"view-a-billing-cycle/read-billing-cycle/2"},"expectedOutputs":[{"node":"read-billing-cycle","kind":"inferred","to":"","label":"Read the billing period's recorded settlement"},{"node":"send-cycle-open","kind":"sendSignal","to":"architect-user","label":"Report that the period is still open: nothing has been charged for it yet"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadCycle","inputs":[{"name":"customerID","value":"5ee10000-0000-4000-8000-000000000001"},{"name":"cycle","value":"2026-10"}],"expect":{"result":"{\"Cycle\":\"2026-10\",\"Settled\":false}","errorExpected":false},"probes":[{"for":"read-billing-cycle","component":"billingStateAccess","operation":"ReadCycle","inputs":[{"name":"customerID","value":"5ee10000-0000-4000-8000-000000000001"},{"name":"cycle","value":"2026-10"}],"expect":{"result":"{\"Cycle\":\"2026-10\",\"Settled\":false}","errorExpected":false}}],"unobservable":["send-cycle-open"],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards cycle-closed=[no: period open] hold; then calls the operation exactly as billingManager does.
func stepViewABillingCycleP1S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputViewABillingCycleP1S1) (any, error) {
	t.Helper()
	// cycle-closed=[no: period open]: the customer exists, but nothing settled the cycle.
	arrangeBound(t, h, subject, in.CustomerID.String())
	return subject.ReadCycle(callContext(t, h, "1"), in.CustomerID, in.Cycle)
}

// stepViewABillingCycleP2S1 — View a Billing Cycle — [yes], step 1: ReadCycle
// Projected stimulus (JSON): {"seq":1,"node":"read-billing-cycle","input":{"kind":"call","from":"billing-manager","to":"billingstateaccess","op":"ReadCycle","call":"view-a-billing-cycle/read-billing-cycle/2"},"expectedOutputs":[{"node":"read-billing-cycle","kind":"inferred","to":"","label":"Read the billing period's recorded settlement"},{"node":"send-cycle-settlement","kind":"sendSignal","to":"architect-user","label":"Return the period's settlement: how the net routed (charged or nothing owed) and whether a declined charge escalated the customer to delinquency (invoice unpaid)"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadCycle","inputs":[{"name":"customerID","value":"5ee20000-0000-4000-8000-000000000002"},{"name":"cycle","value":"2026-09"}],"expect":{"result":"{\"Cycle\":\"2026-09\",\"Settled\":true,\"Outcome\":{\"Net\":{\"MinorUnits\":-4200,\"Currency\":\"USD\"},\"Directive\":2,\"Escalated\":false}}","errorExpected":false},"probes":[{"for":"read-billing-cycle","component":"billingStateAccess","operation":"ReadCycle","inputs":[{"name":"customerID","value":"5ee20000-0000-4000-8000-000000000002"},{"name":"cycle","value":"2026-09"}],"expect":{"result":"{\"Cycle\":\"2026-09\",\"Settled\":true}","errorExpected":false}}],"unobservable":["send-cycle-settlement"],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards cycle-closed=[yes] hold; then calls the operation exactly as billingManager does.
func stepViewABillingCycleP2S1(t *testing.T, h *scenariohost.Host, subject billingstate.BillingStateAccess, in inputViewABillingCycleP2S1) (any, error) {
	t.Helper()
	// cycle-closed=[yes]: the cycle was settled as a charge that went through.
	arrangeBound(t, h, subject, in.CustomerID.String())
	arrangeSettled(t, h, subject, in.CustomerID.String(), in.Cycle, false, false, 0)
	return subject.ReadCycle(callContext(t, h, "1"), in.CustomerID, in.Cycle)
}
