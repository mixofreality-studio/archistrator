// Hooks for merchantGatewayAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package merchantgateway_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/merchantgateway"
)

// newSubject builds the component under test the way the composition root does on
// both profiles: cmd/server/main.gen.go constructs merchantGatewayAccess with the
// generated merchantgateway.NewPostgresMerchantGatewayAccess over a pgx pool, which
// applies the package's own gateway-ledger schema. Every scenario gets its own empty
// Postgres, so a customer holds a validated instrument only when its own arrangement
// or stimuli validated one, and an idempotency key is recorded only when its own
// arrangement or stimuli used it.
//
// The pool comes from the platform's Postgres testinfra rather than the host's
// PostgresURL: opening a pool on a URL needs pgxpool or the postgres infrastructure
// module, neither of which a hooks file may import, while a …/testinfra package is
// admitted.
func newSubject(t *testing.T, h *scenariohost.Host) merchantgateway.MerchantGatewayAccess {
	t.Helper()
	pool := testinfra.StartPostgres(t)
	subject, err := merchantgateway.NewPostgresMerchantGatewayAccess(h.Context(t), pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresMerchantGatewayAccess: %v", err)
	}
	return subject
}

// The arrangement and the replay checks call the contract's own ops with arguments
// decoded from JSON into the generated step-input types of those ops, so a hook
// names no type outside this package and the generated scenarios file.
type (
	validateArgs = inputOnboardANewCustomerP2S1 // ValidateStoredInstrument
	chargeArgs   = inputBillTheUserForUsageP1S1 // ChargeCustomer
)

// arrangeValidated runs the zero-amount validation of the customer's stored
// instrument, as onboarding's ValidateStoredInstrument does, and reads it back: the
// state in which a charge against the customer goes through. The customer is the
// one the step's charge names.
func arrangeValidated(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, charge chargeArgs) {
	t.Helper()
	a := validateArgs{CustomerID: charge.CustomerID, IdempotencyKey: "arrange-validate:" + charge.CustomerID.String()}
	if err := subject.ValidateStoredInstrument(callContext(t, h, "arrange-validate"), a.CustomerID, a.IdempotencyKey); err != nil {
		t.Fatalf("arrange: ValidateStoredInstrument %s: %v", a.CustomerID, err)
	}
	got, err := subject.ReadStoredInstrument(callContext(t, h, "arrange-read"), a.CustomerID)
	if err != nil {
		t.Fatalf("arrange: ReadStoredInstrument %s: %v", a.CustomerID, err)
	}
	if !got.Validated {
		t.Fatalf("arrange: ReadStoredInstrument %s: Validated=false after a validation", a.CustomerID)
	}
}

// requireDeclined fails unless err is the gateway's decline: a terminal Auth-kind
// layer error, the kind the billing rail reads as a declined charge. It reads the
// kind through the generated scenarios file's errorHasCode, the same check a
// binding's errorCode "Auth" expectation makes.
func requireDeclined(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil || !errorHasCode(err, "Auth") {
		t.Fatalf("%s: want the gateway's decline (Auth), got %v", what, err)
	}
}

// arrangeDeclinedAttempt charges the customer under attempt's settlement key while
// it holds no validated instrument, so the gateway declines it and records the
// decline under that key: the earlier attempt a retry follows.
func arrangeDeclinedAttempt(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, in chargeArgs, key string) {
	t.Helper()
	err := subject.ChargeCustomer(callContext(t, h, "arrange-decline"), in.CustomerID, in.Amount, key)
	requireDeclined(t, "arrange: ChargeCustomer "+key, err)
}

// requireReplayed replays one attempt under its own key and requires the outcome the
// gateway recorded for it: nil for a charge that went through, the decline for one
// that was declined. A replay is what a retried activity sends, so this is the
// "at most one charge per (customer, cycle, attempt)" the hints ask the binding to
// assert: the gateway answers the attempt from its ledger and does not charge again.
func requireReplayed(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, in chargeArgs, key string, declined bool) {
	t.Helper()
	err := subject.ChargeCustomer(callContext(t, h, "replay"), in.CustomerID, in.Amount, key)
	if declined {
		requireDeclined(t, "replay: ChargeCustomer "+key, err)
		return
	}
	if err != nil {
		t.Fatalf("replay: ChargeCustomer %s: want the recorded success replayed, got %v", key, err)
	}
}

// stepBillTheUserForUsageP1S1 — Bill the User for Usage — [yes], step 1: ChargeCustomer
// Projected stimulus (JSON): {"seq":1,"node":"charge-user","input":{"kind":"call","from":"billing-manager","to":"merchantgatewayaccess","op":"ChargeCustomer","call":"bill-the-user-for-usage/charge-user/0"},"expectedOutputs":[{"node":"charge-user","kind":"inferred","to":"","label":"Charge the customer for the period"}],"expectedStates":null,"hints":["Charge idempotency key is settle:{customerId}:{cycleId}:{attempt}; a retry through retries-left [yes] is the next attempt under a new key (the gateway replays a declined key's decline), and a replay of one attempt reuses its key, so the binding asserts at most one charge per (customer, cycle, attempt)"]}
// Binding (JSON): {"seq":1,"operation":"ChargeCustomer","inputs":[{"name":"customerID","value":"d1110000-0000-4000-8000-000000000001"},{"name":"amount","value":"{\"MinorUnits\":4200,\"Currency\":\"USD\"}"},{"name":"idempotencyKey","value":"settle:d1110000-0000-4000-8000-000000000001:2026-09:1"}],"expect":{"errorExpected":false},"probes":[],"unobservable":["charge-user"],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards charge-succeeded=[yes] hold; then calls the operation exactly as billingManager does, and replays the attempt under its key.
func stepBillTheUserForUsageP1S1(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, in inputBillTheUserForUsageP1S1) (any, error) {
	t.Helper()
	// charge-succeeded=[yes]: the customer holds a validated stored instrument.
	arrangeValidated(t, h, subject, chargeArgs(in))
	err := subject.ChargeCustomer(callContext(t, h, "1"), in.CustomerID, in.Amount, in.IdempotencyKey)
	if err == nil {
		requireReplayed(t, h, subject, in, in.IdempotencyKey, false)
	}
	return nil, err
}

// stepBillTheUserForUsageP3S2 — Bill the User for Usage — [no] · [yes] · [yes], step 2: ChargeCustomer
// Projected stimulus (JSON): {"seq":2,"node":"charge-user","input":{"kind":"call","from":"billing-manager","to":"merchantgatewayaccess","op":"ChargeCustomer","call":"bill-the-user-for-usage/charge-user/0"},"expectedOutputs":[{"node":"charge-user","kind":"inferred","to":"","label":"Charge the customer for the period"}],"expectedStates":null,"hints":["Charge idempotency key is settle:{customerId}:{cycleId}:{attempt}; a retry through retries-left [yes] is the next attempt under a new key (the gateway replays a declined key's decline), and a replay of one attempt reuses its key, so the binding asserts at most one charge per (customer, cycle, attempt)"]}
// Binding (JSON): {"seq":2,"operation":"ChargeCustomer","inputs":[{"name":"customerID","value":"d1130000-0000-4000-8000-000000000003"},{"name":"amount","value":"{\"MinorUnits\":4200,\"Currency\":\"USD\"}"},{"name":"idempotencyKey","value":"settle:d1130000-0000-4000-8000-000000000003:2026-09:2"}],"expect":{"errorExpected":false},"probes":[],"unobservable":["charge-user"],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards charge-succeeded=[no], retries-left=[yes], charge-succeeded=[yes] hold; then calls the operation exactly as billingManager does, and replays both attempts under their keys.
func stepBillTheUserForUsageP3S2(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, in inputBillTheUserForUsageP3S2) (any, error) {
	t.Helper()
	// Step 1 declined attempt 1 (no validated instrument) and recorded the decline
	// under its key. retries-left=[yes], charge-succeeded=[yes]: the customer's
	// instrument is validated before the retry, and the retry is attempt 2 under its
	// own key.
	arrangeValidated(t, h, subject, chargeArgs(in))
	err := subject.ChargeCustomer(callContext(t, h, "2"), in.CustomerID, in.Amount, in.IdempotencyKey)
	if err == nil {
		requireReplayed(t, h, subject, chargeArgs(in), in.IdempotencyKey, false)
		// The gateway replays a declined key's decline: attempt 1 stays declined.
		requireReplayed(t, h, subject, chargeArgs(in), "settle:"+in.CustomerID.String()+":2026-09:1", true)
	}
	return nil, err
}

// stepOnboardANewCustomerP2S1 — Onboard a New Customer — [no], step 1: ValidateStoredInstrument
// Projected stimulus (JSON): {"seq":1,"node":"validate-instrument","input":{"kind":"call","from":"billing-manager","to":"merchantgatewayaccess","op":"ValidateStoredInstrument","call":"onboard-a-new-customer/validate-instrument/4"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ValidateStoredInstrument","inputs":[{"name":"customerID","value":"d2210000-0000-4000-8000-000000000001"},{"name":"idempotencyKey","value":"validate:d2210000-0000-4000-8000-000000000001"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Checks, through the contract's own read op, the state in which guards registered=[no] hold; then calls the operation exactly as billingManager does.
func stepOnboardANewCustomerP2S1(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, in inputOnboardANewCustomerP2S1) (any, error) {
	t.Helper()
	// registered=[no]: a new customer the gateway has never validated.
	before, err := subject.ReadStoredInstrument(callContext(t, h, "arrange-read"), in.CustomerID)
	if err != nil {
		t.Fatalf("arrange: ReadStoredInstrument %s: %v", in.CustomerID, err)
	}
	if before.Validated {
		t.Fatalf("arrange: ReadStoredInstrument %s: a new customer already holds a validated instrument", in.CustomerID)
	}
	return nil, subject.ValidateStoredInstrument(callContext(t, h, "1"), in.CustomerID, in.IdempotencyKey)
}

// stepRetryADeclinedServiceInvoiceP1S1 — Retry a Declined Service Invoice — [yes: retry] · [yes], step 1: ChargeCustomer
// Projected stimulus (JSON): {"seq":1,"node":"recharge","input":{"kind":"call","from":"billing-manager","to":"merchantgatewayaccess","op":"ChargeCustomer","call":"retry-a-declined-service-invoice/recharge/0"},"expectedOutputs":[{"node":"recharge","kind":"inferred","to":"","label":"Retry the charge under the next attempt's settlement idempotency key"}],"expectedStates":null,"hints":["Each charge attempt carries its attempt number in the settlement idempotency key settle:{customerId}:{cycleId}:{attempt}: the gateway replays a key's recorded outcome, a decline included, so a retry under the same key could never succeed. A replay of one attempt reuses that attempt's key, so the binding asserts at most one charge per (customer, cycle, attempt)."]}
// Binding (JSON): {"seq":1,"operation":"ChargeCustomer","inputs":[{"name":"customerID","value":"d3310000-0000-4000-8000-000000000001"},{"name":"amount","value":"{\"MinorUnits\":4200,\"Currency\":\"USD\"}"},{"name":"idempotencyKey","value":"settle:d3310000-0000-4000-8000-000000000001:2026-09:2"}],"expect":{"errorExpected":false},"probes":[],"unobservable":["recharge"],"hook":true}
// Arranges, through the contract's own ops, the state in which guards retry-budget=[yes: retry], charge-ok=[yes] hold; then calls the operation exactly as billingManager does, and replays the attempt under its key.
func stepRetryADeclinedServiceInvoiceP1S1(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, in inputRetryADeclinedServiceInvoiceP1S1) (any, error) {
	t.Helper()
	// charge-declined: attempt 1 was declined and recorded under its key.
	// retry-budget=[yes: retry], charge-ok=[yes]: the instrument is validated before
	// the retry, which is attempt 2 under its own key.
	arrangeDeclinedAttempt(t, h, subject, chargeArgs(in), "settle:"+in.CustomerID.String()+":2026-09:1")
	arrangeValidated(t, h, subject, chargeArgs(in))
	err := subject.ChargeCustomer(callContext(t, h, "1"), in.CustomerID, in.Amount, in.IdempotencyKey)
	if err == nil {
		requireReplayed(t, h, subject, chargeArgs(in), in.IdempotencyKey, false)
	}
	return nil, err
}

// stepRetryADeclinedServiceInvoiceP4S1 — Retry a Declined Service Invoice — [yes: retry] · [no: declined again] · [yes: retry] · [yes], step 1: ChargeCustomer
// Projected stimulus (JSON): {"seq":1,"node":"recharge","input":{"kind":"call","from":"billing-manager","to":"merchantgatewayaccess","op":"ChargeCustomer","call":"retry-a-declined-service-invoice/recharge/0"},"expectedOutputs":[{"node":"recharge","kind":"inferred","to":"","label":"Retry the charge under the next attempt's settlement idempotency key"}],"expectedStates":null,"hints":["Each charge attempt carries its attempt number in the settlement idempotency key settle:{customerId}:{cycleId}:{attempt}: the gateway replays a key's recorded outcome, a decline included, so a retry under the same key could never succeed. A replay of one attempt reuses that attempt's key, so the binding asserts at most one charge per (customer, cycle, attempt)."]}
// Binding (JSON): {"seq":1,"operation":"ChargeCustomer","inputs":[{"name":"customerID","value":"d3340000-0000-4000-8000-000000000004"},{"name":"amount","value":"{\"MinorUnits\":4200,\"Currency\":\"USD\"}"},{"name":"idempotencyKey","value":"settle:d3340000-0000-4000-8000-000000000004:2026-09:2"}],"expect":{"errorExpected":true,"errorCode":"Auth"},"probes":[],"unobservable":["recharge"],"hook":true}
// Arranges, through the contract's own ops, the state in which guards retry-budget=[yes: retry], charge-ok=[no: declined again] hold; then calls the operation exactly as billingManager does, and replays the declined attempt under its key.
func stepRetryADeclinedServiceInvoiceP4S1(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, in inputRetryADeclinedServiceInvoiceP4S1) (any, error) {
	t.Helper()
	// charge-declined: attempt 1 was declined and recorded under its key.
	// retry-budget=[yes: retry], charge-ok=[no: declined again]: the customer still
	// holds no validated instrument, so attempt 2 is declined too.
	arrangeDeclinedAttempt(t, h, subject, chargeArgs(in), "settle:"+in.CustomerID.String()+":2026-09:1")
	err := subject.ChargeCustomer(callContext(t, h, "1"), in.CustomerID, in.Amount, in.IdempotencyKey)
	if err != nil {
		requireReplayed(t, h, subject, chargeArgs(in), in.IdempotencyKey, true)
	}
	return nil, err
}

// stepRetryADeclinedServiceInvoiceP4S2 — Retry a Declined Service Invoice — [yes: retry] · [no: declined again] · [yes: retry] · [yes], step 2: ChargeCustomer
// Projected stimulus (JSON): {"seq":2,"node":"recharge","input":{"kind":"call","from":"billing-manager","to":"merchantgatewayaccess","op":"ChargeCustomer","call":"retry-a-declined-service-invoice/recharge/0"},"expectedOutputs":[{"node":"recharge","kind":"inferred","to":"","label":"Retry the charge under the next attempt's settlement idempotency key"}],"expectedStates":null,"hints":["Each charge attempt carries its attempt number in the settlement idempotency key settle:{customerId}:{cycleId}:{attempt}: the gateway replays a key's recorded outcome, a decline included, so a retry under the same key could never succeed. A replay of one attempt reuses that attempt's key, so the binding asserts at most one charge per (customer, cycle, attempt)."]}
// Binding (JSON): {"seq":2,"operation":"ChargeCustomer","inputs":[{"name":"customerID","value":"d3340000-0000-4000-8000-000000000004"},{"name":"amount","value":"{\"MinorUnits\":4200,\"Currency\":\"USD\"}"},{"name":"idempotencyKey","value":"settle:d3340000-0000-4000-8000-000000000004:2026-09:3"}],"expect":{"errorExpected":false},"probes":[],"unobservable":["recharge"],"hook":true}
// Arranges, through the contract's own write ops, the state in which guards retry-budget=[yes: retry], charge-ok=[yes] hold after a second decline; then calls the operation exactly as billingManager does, and replays both attempts under their keys.
func stepRetryADeclinedServiceInvoiceP4S2(t *testing.T, h *scenariohost.Host, subject merchantgateway.MerchantGatewayAccess, in inputRetryADeclinedServiceInvoiceP4S2) (any, error) {
	t.Helper()
	// Step 1 declined attempt 2. retry-budget=[yes: retry], charge-ok=[yes]: the
	// instrument is validated before the next retry, which is attempt 3 under its own
	// key.
	arrangeValidated(t, h, subject, chargeArgs(in))
	err := subject.ChargeCustomer(callContext(t, h, "2"), in.CustomerID, in.Amount, in.IdempotencyKey)
	if err == nil {
		requireReplayed(t, h, subject, chargeArgs(in), in.IdempotencyKey, false)
		// The gateway replays a declined key's decline: attempt 2 stays declined
		// even though the instrument is now validated.
		requireReplayed(t, h, subject, chargeArgs(in), "settle:"+in.CustomerID.String()+":2026-09:2", true)
	}
	return nil, err
}
