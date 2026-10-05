// Hooks for interventionEngine scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package intervention_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// newSubject builds the component under test through its generated public
// constructor, exactly as cmd/server/main.gen.go wires it for its callers
// (operationsManager, billingManager, deliveryManager). interventionEngine is
// a pure Engine: it has no downstream dependency, so nothing on the host is
// wired into it.
func newSubject(t *testing.T, h *scenariohost.Host) intervention.InterventionEngine {
	t.Helper()
	_ = h
	return intervention.NewInterventionEngine()
}

// ---------------------------------------------------------------------------
// operate-a-delivered-system, node record-status.
//
// At record-status operationsManager's reconcile pass (manager/operations,
// reconcileOne) does three things for one in-flight app whose observed health
// differs from its last-known head-state: it appends the observed compute to
// usageAccess, records the status transition on operatedSystemStateAccess,
// and THEN calls DecideOnHealth with the transition. The scenario's expected
// states (operated-system-status-changed, compute-usage-recorded) attach to
// that DecideOnHealth stimulus (spec §11), but the pure Engine writes neither:
// the step hook therefore plays the caller's whole node against the REAL
// operatedSystemStateAccess and usageAccess (spec §12 B2 — public constructors
// over a real Postgres, the same stores both deploy profiles bind), and the
// probes read the state back through those components' read ops.
// ---------------------------------------------------------------------------

// The operated app's billing attribution: the customer and cycle the operations
// manager's reconcile pass is configured with (its usage events carry them),
// the values the compute-usage-recorded probe reads back.
const (
	operatedCustomerID = "6f1c2b9e-0c00-4c00-8000-0000000000c1"
	operatedCycleID    = "2026-10"
)

// operatedStores is one scenario's real operations-side ResourceAccess pair,
// over that scenario's own empty Postgres.
type operatedStores struct {
	systems operatedsystemstate.OperatedSystemStateAccess
	usage   usage.UsageAccess
}

// scenarioStores holds each running scenario's stores, keyed by its *testing.T:
// the step hook arranges through them and the probe hooks (which receive no
// state from the step) read back through the same ones. An entry is removed
// when its test ends.
var scenarioStores sync.Map

// storesFor returns t's stores, booting them on first use: a fresh Postgres
// (platform testinfra) and the two RAs built through their generated public
// constructors, which apply their own schemas — as main.gen.go does.
func storesFor(t *testing.T, h *scenariohost.Host) *operatedStores {
	t.Helper()
	if s, ok := scenarioStores.Load(t); ok {
		st, isStores := s.(*operatedStores)
		if !isStores {
			t.Fatalf("hooks: scenario stores of %s have type %T", t.Name(), s)
		}
		return st
	}
	pool := testinfra.StartPostgres(t)
	systems, err := operatedsystemstate.NewPostgresOperatedSystemStateAccess(h.Context(t), pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresOperatedSystemStateAccess: %v", err)
	}
	ledger, err := usage.NewPostgresUsageAccess(h.Context(t), pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresUsageAccess: %v", err)
	}
	st := &operatedStores{systems: systems, usage: ledger}
	scenarioStores.Store(t, st)
	t.Cleanup(func() { scenarioStores.Delete(t) })
	return st
}

// raContext builds the ResourceAccess call context an op of op's component
// takes, carrying ctx and idempotency key key — what operationsManager's
// generated activities hand these RAs (activities.gen.go). The context type C
// is inferred from op's first parameter, so this file never names the
// framework package that declares it (the hooks import rule admits component
// packages, not framework layer packages); the constraint spells that
// context's exact shape, so an op whose first parameter has any other shape
// does not compile. Every RA op of one module takes the same C, so the value
// is passed to the multi-parameter ops too.
func raContext[C ~struct {
	context.Context
	Principal      P
	IdempotencyKey K
}, P any, K ~string, A, R any](ctx context.Context, key string, _ func(C, A) (R, error)) C {
	var anonymous P // reconcile runs on the platform's own schedule: no acting principal
	return C(struct {
		context.Context
		Principal      P
		IdempotencyKey K
	}{ctx, anonymous, K(key)})
}

// runtimeStatusFor maps the intervention vocabulary's health back onto the
// head-state RuntimeStatus operationsManager records (the inverse of its
// healthStatusFromRuntimeStatus bridge, for the two observed states a reconcile
// transition moves between).
func runtimeStatusFor(t *testing.T, health intervention.HealthStatus) operatedsystemstate.RuntimeStatus {
	t.Helper()
	switch health {
	case intervention.HealthHealthy:
		return operatedsystemstate.RuntimeStatusHealthy
	case intervention.HealthDegraded:
		return operatedsystemstate.RuntimeStatusDegraded
	case intervention.HealthUnknown, intervention.HealthUnhealthy:
		t.Fatalf("hooks: health %d is not an observed reconcile state this arrangement records", health)
	}
	t.Fatalf("hooks: unknown health %d", health)
	return operatedsystemstate.RuntimeStatusUnknown
}

// recordStatusAsOperationsManager runs node record-status as reconcileOne does
// for the app the binding's HealthChange names, then calls DecideOnHealth:
//
//  1. arrange the app as in-flight-observed at change.FromHealth (registered,
//     status recorded) — the head-state the reconcile tick read;
//  2. append the tick's observed compute to the usage ledger;
//  3. record the transition to change.ToHealth (version-guarded);
//  4. call DecideOnHealth exactly as the manager does: in-workflow, bare
//     context (the generated callContext), the binding's HealthChange.
//
// Every write carries a fixed key derived from the app, so the arrangement is
// idempotent within a scenario. The guard state-transition=[yes] is the
// input's own FromHealth != ToHealth; decision-nochange is autoscalerEngine's
// guard, downstream of this call, and needs no arrangement here.
func recordStatusAsOperationsManager(t *testing.T, h *scenariohost.Host, subject intervention.InterventionEngine, change intervention.HealthChange) (any, error) {
	t.Helper()
	if change.FromHealth == change.ToHealth {
		t.Fatalf("binding violates guard state-transition=[yes]: FromHealth == ToHealth == %d", change.FromHealth)
	}
	st := storesFor(t, h)
	ctx := h.Context(t)
	app := string(change.OperatedAppID)

	var sys operatedsystemstate.OperatedSystem
	mustUnmarshal(t, fmt.Sprintf(`{"ID":%q,"CustomerID":%q}`, app, operatedCustomerID), &sys)

	keyed := func(step string) string { return "reconcile:" + app + ":" + step }
	rc := raContext(ctx, keyed("register"), st.systems.ReadOperatedSystem)
	v, err := st.systems.RegisterOperatedSystem(rc, sys.ID, sys.CustomerID, "stp-ie-project-0001", "stp-ie-bundle", rc.IdempotencyKey)
	if err != nil {
		t.Fatalf("arrange: RegisterOperatedSystem %s: %v", app, err)
	}
	rc = raContext(ctx, keyed("observed"), st.systems.ReadOperatedSystem)
	if v, err = st.systems.RecordRuntimeStatusChange(rc, sys.ID, v, runtimeStatusFor(t, change.FromHealth), rc.IdempotencyKey); err != nil {
		t.Fatalf("arrange: RecordRuntimeStatusChange %s -> %d: %v", app, change.FromHealth, err)
	}

	var events []usage.UsageEvent
	mustUnmarshal(t, fmt.Sprintf(`[{"CustomerID":%q,"OperatedAppID":%q,"CycleID":%q,"Units":{"Amount":1.5,"Unit":"vcpu-hour"},"RuntimeEventID":%q}]`,
		operatedCustomerID, app, operatedCycleID, keyed("compute")), &events)
	if _, err = st.usage.RecordComputeUsage(raContext(ctx, keyed("compute"), st.usage.ReadRange), events); err != nil {
		t.Fatalf("record-status: RecordComputeUsage %s: %v", app, err)
	}

	rc = raContext(ctx, keyed("transition"), st.systems.ReadOperatedSystem)
	if _, err = st.systems.RecordRuntimeStatusChange(rc, sys.ID, v, runtimeStatusFor(t, change.ToHealth), rc.IdempotencyKey); err != nil {
		t.Fatalf("record-status: RecordRuntimeStatusChange %s -> %d: %v", app, change.ToHealth, err)
	}

	return subject.DecideOnHealth(callContext(t, h, "1"), change)
}

// readOperatedSystem is the operated-system-status-changed probe: the probe's
// operatedAppID, read through operatedSystemStateAccess.ReadOperatedSystem.
func readOperatedSystem(t *testing.T, h *scenariohost.Host, operatedAppID string) (any, error) {
	t.Helper()
	st := storesFor(t, h)
	var sys operatedsystemstate.OperatedSystem
	mustUnmarshal(t, fmt.Sprintf(`{"ID":%q}`, operatedAppID), &sys)
	return st.systems.ReadOperatedSystem(raContext(h.Context(t), "", st.systems.ReadOperatedSystem), sys.ID)
}

// readUsageRange is the compute-usage-recorded probe: the probe's query,
// read through usageAccess.ReadRange.
func readUsageRange(t *testing.T, h *scenariohost.Host, queryJSON string) (any, error) {
	t.Helper()
	st := storesFor(t, h)
	var query usage.UsageRangeQuery
	mustUnmarshal(t, queryJSON, &query)
	return st.usage.ReadRange(raContext(h.Context(t), "", st.usage.ReadRange), query)
}

// The probes' inputs, verbatim from the binding.
const (
	probeOperatedAppID = "6f1c2b9e-0a11-4c00-8000-0000000000a1"
	probeUsageQuery    = `{"CustomerID":"6f1c2b9e-0c00-4c00-8000-0000000000c1","CycleID":"2026-10","OperatedAppID":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}`
)

// stepOperateADeliveredSystemP4S1 — Operate a Delivered System — [yes] · [yes], step 1: DecideOnHealth
// Projected stimulus (JSON): {"seq":1,"node":"record-status","input":{"kind":"call","from":"operations-manager","to":"interventionengine","op":"DecideOnHealth","call":"operate-a-delivered-system/record-status/3"},"expectedOutputs":[{"node":"record-status","kind":"inferred","to":"","label":"Record status change and compute usage; decide on intervention"}],"expectedStates":[{"node":"operated-system-status-changed","object":"Operated System","inState":"[status changed]"},{"node":"compute-usage-recorded","object":"Compute Usage","inState":"[recorded]"}],"hints":["Intervention decision ∈ {Retry, Escalate}; status is written only on a transition. Escalate has no realized output to the Operator yet (see rationale)"]}
// Binding (JSON): {"seq":1,"operation":"DecideOnHealth","inputs":[{"name":"healthChange","value":"{\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"FromHealth\":1,\"ToHealth\":2,\"SLOStatus\":3,\"Severity\":0,\"Policy\":{\"Mode\":2,\"SLATier\":0,\"RetryBudget\":2,\"ShortfallToleranceSweeps\":0}}"}],"expect":{"result":"1","errorExpected":false},"probes":[{"for":"operated-system-status-changed","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":3}","errorExpected":false}},{"for":"compute-usage-recorded","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"2026-10\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-10\"}]","errorExpected":false}}],"unobservable":["record-status"],"hook":true}
// Hook: arrange state so that guards state-transition=[yes], decision-nochange=[yes] hold, call the operation, return its result.
func stepOperateADeliveredSystemP4S1(t *testing.T, h *scenariohost.Host, subject intervention.InterventionEngine, in inputOperateADeliveredSystemP4S1) (any, error) {
	t.Helper()
	return recordStatusAsOperationsManager(t, h, subject, in.HealthChange)
}

// probeOperateADeliveredSystemP4S1N1 — Operate a Delivered System — [yes] · [yes], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves operated-system-status-changed.
// Probe (JSON): {"for":"operated-system-status-changed","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":3}","errorExpected":false}}
// Hook: call operatedSystemStateAccess.ReadOperatedSystem with the probe's inputs on the host's stack and return its result.
func probeOperateADeliveredSystemP4S1N1(t *testing.T, h *scenariohost.Host, subject intervention.InterventionEngine) (any, error) {
	t.Helper()
	_ = subject
	return readOperatedSystem(t, h, probeOperatedAppID)
}

// probeOperateADeliveredSystemP4S1N2 — Operate a Delivered System — [yes] · [yes], step 1, probe 2: usageAccess.ReadRange proves compute-usage-recorded.
// Probe (JSON): {"for":"compute-usage-recorded","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"2026-10\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-10\"}]","errorExpected":false}}
// Hook: call usageAccess.ReadRange with the probe's inputs on the host's stack and return its result.
func probeOperateADeliveredSystemP4S1N2(t *testing.T, h *scenariohost.Host, subject intervention.InterventionEngine) (any, error) {
	t.Helper()
	_ = subject
	return readUsageRange(t, h, probeUsageQuery)
}

// stepOperateADeliveredSystemP5S1 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1: DecideOnHealth
// Projected stimulus (JSON): {"seq":1,"node":"record-status","input":{"kind":"call","from":"operations-manager","to":"interventionengine","op":"DecideOnHealth","call":"operate-a-delivered-system/record-status/3"},"expectedOutputs":[{"node":"record-status","kind":"inferred","to":"","label":"Record status change and compute usage; decide on intervention"}],"expectedStates":[{"node":"operated-system-status-changed","object":"Operated System","inState":"[status changed]"},{"node":"compute-usage-recorded","object":"Compute Usage","inState":"[recorded]"}],"hints":["Intervention decision ∈ {Retry, Escalate}; status is written only on a transition. Escalate has no realized output to the Operator yet (see rationale)"]}
// Binding (JSON): {"seq":1,"operation":"DecideOnHealth","inputs":[{"name":"healthChange","value":"{\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"FromHealth\":1,\"ToHealth\":2,\"SLOStatus\":1,\"Severity\":0,\"Policy\":{\"Mode\":2,\"SLATier\":0,\"RetryBudget\":2,\"ShortfallToleranceSweeps\":0}}"}],"expect":{"result":"0","errorExpected":false},"probes":[{"for":"operated-system-status-changed","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":3}","errorExpected":false}},{"for":"compute-usage-recorded","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"2026-10\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-10\"}]","errorExpected":false}}],"unobservable":["record-status"],"hook":true}
// Hook: arrange state so that guards state-transition=[yes], decision-nochange=[no: scale/pause/resume] hold, call the operation, return its result.
func stepOperateADeliveredSystemP5S1(t *testing.T, h *scenariohost.Host, subject intervention.InterventionEngine, in inputOperateADeliveredSystemP5S1) (any, error) {
	t.Helper()
	return recordStatusAsOperationsManager(t, h, subject, in.HealthChange)
}

// probeOperateADeliveredSystemP5S1N1 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1, probe 1: operatedSystemStateAccess.ReadOperatedSystem proves operated-system-status-changed.
// Probe (JSON): {"for":"operated-system-status-changed","component":"operatedSystemStateAccess","operation":"ReadOperatedSystem","inputs":[{"name":"operatedAppID","value":"6f1c2b9e-0a11-4c00-8000-0000000000a1"}],"expect":{"result":"{\"ID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"Status\":3}","errorExpected":false}}
// Hook: call operatedSystemStateAccess.ReadOperatedSystem with the probe's inputs on the host's stack and return its result.
func probeOperateADeliveredSystemP5S1N1(t *testing.T, h *scenariohost.Host, subject intervention.InterventionEngine) (any, error) {
	t.Helper()
	_ = subject
	return readOperatedSystem(t, h, probeOperatedAppID)
}

// probeOperateADeliveredSystemP5S1N2 — Operate a Delivered System — [yes] · [no: scale/pause/resume], step 1, probe 2: usageAccess.ReadRange proves compute-usage-recorded.
// Probe (JSON): {"for":"compute-usage-recorded","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"CycleID\":\"2026-10\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}"}],"expect":{"result":"[{\"CustomerID\":\"6f1c2b9e-0c00-4c00-8000-0000000000c1\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-10\"}]","errorExpected":false}}
// Hook: call usageAccess.ReadRange with the probe's inputs on the host's stack and return its result.
func probeOperateADeliveredSystemP5S1N2(t *testing.T, h *scenariohost.Host, subject intervention.InterventionEngine) (any, error) {
	t.Helper()
	_ = subject
	return readUsageRange(t, h, probeUsageQuery)
}
