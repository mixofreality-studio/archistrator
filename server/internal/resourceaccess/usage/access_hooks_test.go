// Hooks for usageAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package usage_test

import (
	"fmt"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// newSubject builds the component under test the way the composition root does
// on the cloud profile: cmd/server/main.gen.go constructs usageAccess with the
// generated usage.NewPostgresUsageAccess over a pgx pool, which applies the
// package's own append-only usage_log schema. Every scenario gets its own empty
// Postgres, so the facts a scenario reads back are exactly the ones its own
// arrangement and stimuli appended.
//
// The pool comes from the platform's Postgres testinfra rather than the host's
// PostgresURL: opening a pool on a URL needs pgxpool or the postgres
// infrastructure module, neither of which a hooks file may import
// (hooks-import-not-allowed), while a …/testinfra package is admitted.
func newSubject(t *testing.T, h *scenariohost.Host) usage.UsageAccess {
	t.Helper()
	pool := testinfra.StartPostgres(t)
	subject, err := usage.NewPostgresUsageAccess(h.Context(t), pool)
	if err != nil {
		t.Fatalf("hooks: NewPostgresUsageAccess: %v", err)
	}
	return subject
}

// fact is one metered usage fact an arrangement appends: the customer comes from
// the read's own query, the rest is named per fact.
type fact struct {
	app, cycle, runtimeEventID string
	amount                     float64
}

// arrangeRecorded appends facts for the query's customer through the contract's
// own write op, RecordComputeUsage, exactly as the operations manager's tick
// does (one batch, so the ledger's append order is the order given). The events
// are decoded from JSON into the contract's UsageEvent so the hook names no type
// outside the component package.
func arrangeRecorded(t *testing.T, h *scenariohost.Host, subject usage.UsageAccess, query usage.UsageRangeQuery, facts ...fact) {
	t.Helper()
	events := make([]usage.UsageEvent, len(facts))
	for i, f := range facts {
		mustUnmarshal(t, fmt.Sprintf(`{"CustomerID":%q,"OperatedAppID":%q,"CycleID":%q,"Units":{"Amount":%v,"Unit":"vCPU-hour"},"RuntimeEventID":%q,"RawMeter":"eyJjcHUiOjEyLjV9","WindowStart":"2026-09-30T23:00:00Z","WindowEnd":"2026-09-30T23:15:00Z","OccurredAt":"2026-09-30T23:15:00Z"}`,
			query.CustomerID.String(), f.app, f.cycle, f.amount, f.runtimeEventID), &events[i])
	}
	refs, err := subject.RecordComputeUsage(callContext(t, h, "arrange-record"), events)
	if err != nil {
		t.Fatalf("arrange: RecordComputeUsage: %v", err)
	}
	if len(refs) != len(facts) {
		t.Fatalf("arrange: RecordComputeUsage: %d refs for %d facts", len(refs), len(facts))
	}
}

// The bindings attribute every hosting fact to operated app …a1 or …a2.
const (
	appA1 = "6f1c2b9e-0a11-4c00-8000-0000000000a1"
	appA2 = "6f1c2b9e-0a11-4c00-8000-0000000000a2"
)

// stepBillTheUserForUsageP1S1 — Bill the User for Usage — [yes], step 1: ReadRange
// Projected stimulus (JSON): {"seq":1,"node":"read-hosting-usage","input":{"kind":"call","from":"billing-manager","to":"usageaccess","op":"ReadRange","call":"bill-the-user-for-usage/read-hosting-usage/2"},"expectedOutputs":[{"node":"read-hosting-usage","kind":"inferred","to":"","label":"Read hosting usage for the period"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0001\",\"CycleID\":\"2026-09\",\"OperatedAppID\":null}","schemaRef":"#/$defs/UsageRangeQuery"}],"expect":{"result":"[{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0001\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-09\",\"RuntimeEventID\":\"rt-bill-p1-0001\",\"Units\":{\"Amount\":12.5,\"Unit\":\"vCPU-hour\"}},{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0001\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-09\",\"RuntimeEventID\":\"rt-bill-p1-0002\",\"Units\":{\"Amount\":7.25,\"Unit\":\"vCPU-hour\"}}]","errorExpected":false},"probes":[{"for":"read-hosting-usage","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0001\",\"CycleID\":\"2026-09\",\"OperatedAppID\":null}","schemaRef":"#/$defs/UsageRangeQuery"}],"expect":{"result":"[{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0001\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-09\",\"RuntimeEventID\":\"rt-bill-p1-0001\",\"Units\":{\"Amount\":12.5,\"Unit\":\"vCPU-hour\"}},{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0001\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-09\",\"RuntimeEventID\":\"rt-bill-p1-0002\",\"Units\":{\"Amount\":7.25,\"Unit\":\"vCPU-hour\"}}]","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the ledger for the path that takes guards charge-succeeded=[yes], then calls the operation as its caller does.
func stepBillTheUserForUsageP1S1(t *testing.T, h *scenariohost.Host, subject usage.UsageAccess, in inputBillTheUserForUsageP1S1) (any, error) {
	t.Helper()
	// The guard charge-succeeded=[yes] is decided downstream of this read
	// (billingManager → merchant gateway); the ledger's part of the path is the
	// period's metered facts, so the arrangement is those facts — plus one fact
	// of the previous period the period-close read must not fold in.
	arrangeRecorded(t, h, subject, in.Query,
		fact{app: appA1, cycle: "2026-08", runtimeEventID: "rt-bill-p1-prev-0001", amount: 99},
		fact{app: appA1, cycle: "2026-09", runtimeEventID: "rt-bill-p1-0001", amount: 12.5},
		fact{app: appA1, cycle: "2026-09", runtimeEventID: "rt-bill-p1-0002", amount: 7.25},
	)
	return subject.ReadRange(callContext(t, h, "1"), in.Query)
}

// stepBillTheUserForUsageP2S1 — Bill the User for Usage — [no] · [no], step 1: ReadRange
// Projected stimulus (JSON): {"seq":1,"node":"read-hosting-usage","input":{"kind":"call","from":"billing-manager","to":"usageaccess","op":"ReadRange","call":"bill-the-user-for-usage/read-hosting-usage/2"},"expectedOutputs":[{"node":"read-hosting-usage","kind":"inferred","to":"","label":"Read hosting usage for the period"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0002\",\"CycleID\":\"2026-09\",\"OperatedAppID\":null}","schemaRef":"#/$defs/UsageRangeQuery"}],"expect":{"result":"[{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0002\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-09\",\"RuntimeEventID\":\"rt-bill-p2-0001\",\"Units\":{\"Amount\":40.0,\"Unit\":\"vCPU-hour\"}},{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0002\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a2\",\"CycleID\":\"2026-09\",\"RuntimeEventID\":\"rt-bill-p2-0002\",\"Units\":{\"Amount\":3.5,\"Unit\":\"vCPU-hour\"}}]","errorExpected":false},"probes":[{"for":"read-hosting-usage","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0002\",\"CycleID\":\"2026-09\",\"OperatedAppID\":null}","schemaRef":"#/$defs/UsageRangeQuery"}],"expect":{"result":"[{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0002\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-09\",\"RuntimeEventID\":\"rt-bill-p2-0001\",\"Units\":{\"Amount\":40.0,\"Unit\":\"vCPU-hour\"}},{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0002\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a2\",\"CycleID\":\"2026-09\",\"RuntimeEventID\":\"rt-bill-p2-0002\",\"Units\":{\"Amount\":3.5,\"Unit\":\"vCPU-hour\"}}]","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the ledger for the path that takes guards charge-succeeded=[no], retries-left=[no], then calls the operation as its caller does.
func stepBillTheUserForUsageP2S1(t *testing.T, h *scenariohost.Host, subject usage.UsageAccess, in inputBillTheUserForUsageP2S1) (any, error) {
	t.Helper()
	// The guards charge-succeeded=[no], retries-left=[no] are decided downstream
	// of this read; the ledger holds the period's facts across two operated apps,
	// and the whole-period read (OperatedAppID nil) must return both apps' facts.
	// A replayed batch for the first fact must not double-count it.
	arrangeRecorded(t, h, subject, in.Query,
		fact{app: appA1, cycle: "2026-09", runtimeEventID: "rt-bill-p2-0001", amount: 40},
		fact{app: appA2, cycle: "2026-09", runtimeEventID: "rt-bill-p2-0002", amount: 3.5},
	)
	arrangeRecorded(t, h, subject, in.Query,
		fact{app: appA1, cycle: "2026-09", runtimeEventID: "rt-bill-p2-0001", amount: 40},
	)
	return subject.ReadRange(callContext(t, h, "1"), in.Query)
}

// stepViewOperatingCostProjectionP1S1 — View Operating-Cost Projection — [yes], step 1: ReadRange
// Projected stimulus (JSON): {"seq":1,"node":"read-usage","input":{"kind":"call","from":"operations-manager","to":"usageaccess","op":"ReadRange","call":"view-operating-cost-projection/read-usage/0"},"expectedOutputs":[{"node":"read-usage","kind":"inferred","to":"","label":"Read observed compute usage for the current cycle"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0006\",\"CycleID\":\"2026-10\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}","schemaRef":"#/$defs/UsageRangeQuery"}],"expect":{"result":"[{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0006\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-10\",\"RuntimeEventID\":\"rt-view-p1-0001\",\"Units\":{\"Amount\":6.0,\"Unit\":\"vCPU-hour\"}}]","errorExpected":false},"probes":[{"for":"read-usage","component":"usageAccess","operation":"ReadRange","inputs":[{"name":"query","value":"{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0006\",\"CycleID\":\"2026-10\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\"}","schemaRef":"#/$defs/UsageRangeQuery"}],"expect":{"result":"[{\"CustomerID\":\"3b0e6c1a-7d2f-4e5b-9a10-0000000c0006\",\"OperatedAppID\":\"6f1c2b9e-0a11-4c00-8000-0000000000a1\",\"CycleID\":\"2026-10\",\"RuntimeEventID\":\"rt-view-p1-0001\",\"Units\":{\"Amount\":6.0,\"Unit\":\"vCPU-hour\"}}]","errorExpected":false}}],"unobservable":[],"hook":true}
// Arranges the ledger for the path that takes guards app-known=[yes], then calls the operation as its caller does.
func stepViewOperatingCostProjectionP1S1(t *testing.T, h *scenariohost.Host, subject usage.UsageAccess, in inputViewOperatingCostProjectionP1S1) (any, error) {
	t.Helper()
	// app-known=[yes]: the operated app has metered facts this cycle. The read is
	// scoped to that one app, so a fact of another app of the same customer and
	// cycle is arranged too and must not be returned.
	arrangeRecorded(t, h, subject, in.Query,
		fact{app: appA1, cycle: "2026-10", runtimeEventID: "rt-view-p1-0001", amount: 6},
		fact{app: appA2, cycle: "2026-10", runtimeEventID: "rt-view-p1-other-0001", amount: 2},
	)
	return subject.ReadRange(callContext(t, h, "1"), in.Query)
}
