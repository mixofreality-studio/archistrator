package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// opNames is the interface.operations[].name list of a contract entry.
func opNames(t *testing.T, entry json.RawMessage) []string {
	t.Helper()
	var doc struct {
		Interface struct {
			Operations []struct {
				Name string `json:"name"`
			} `json:"operations"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(entry, &doc); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, op := range doc.Interface.Operations {
		out = append(out, op.Name)
	}
	return out
}

// A manager exposed with an op list carries only those ops onto the client layer,
// in contract order; every other member of the entry is kept verbatim.
func TestExposedContract_KeepsOnlyListedOps(t *testing.T) {
	entry := json.RawMessage(`{"component":"billingManager","goPackage":"internal/manager/billing","$defs":{"X":{"type":"string"}},` +
		`"interface":{"name":"BillingManager","operations":[{"name":"CloseBillingCycle","params":[]},{"name":"GetBillingCycle","params":[{"name":"cycleID"}]},{"name":"RunShortfallSweep"}]}}`)
	got, err := exposedContract(entry, []string{"GetBillingCycle"})
	if err != nil {
		t.Fatal(err)
	}
	if names := opNames(t, got); !slices.Equal(names, []string{"GetBillingCycle"}) {
		t.Fatalf("exposed ops = %v, want [GetBillingCycle]", names)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"component", "goPackage"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("member %q dropped", k)
		}
	}
	if !strings.Contains(string(got), `"params":[{"name":"cycleID"}]`) {
		t.Errorf("the kept op is not verbatim: %s", got)
	}
}

// The $defs an exposed contract keeps are those its kept ops reach, transitively:
// a type only a hidden op uses never reaches the OpenAPI document.
func TestExposedContract_PrunesDefsToTheKeptOps(t *testing.T) {
	entry := json.RawMessage(`{"$defs":{` +
		`"View":{"type":"object","properties":{"r":{"$ref":"#/$defs/Routing"}}},` +
		`"Routing":{"type":"integer"},` +
		`"CloseResult":{"type":"object","properties":{"r":{"$ref":"#/$defs/Routing"}}},` +
		`"Id":{"type":"string"}},` +
		`"interface":{"name":"M","operations":[` +
		`{"name":"Close","result":{"$ref":"#/$defs/CloseResult"}},` +
		`{"name":"Get","params":[{"name":"id","schema":{"$ref":"#/$defs/Id"}}],"result":{"$ref":"#/$defs/View"}}]}}`)
	got, err := exposedContract(entry, []string{"Get"})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range doc.Defs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"Id", "Routing", "View"}) {
		t.Fatalf("kept $defs = %v, want [Id Routing View]", keys)
	}
}

// nil exposes the whole interface, unchanged.
func TestExposedContract_NilKeepsEveryOp(t *testing.T) {
	entry := json.RawMessage(`{"interface":{"name":"M","operations":[{"name":"A"},{"name":"B"}]}}`)
	got, err := exposedContract(entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(entry) {
		t.Fatalf("nil op list rewrote the entry: %s", got)
	}
}

// A listed op the contract does not declare is an error, not a silently smaller
// surface.
func TestExposedContract_UnknownOpFails(t *testing.T) {
	entry := json.RawMessage(`{"interface":{"name":"M","operations":[{"name":"A"}]}}`)
	if _, err := exposedContract(entry, []string{"A", "Nope"}); err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("want an error naming Nope, got %v", err)
	}
}

// The committed model: billingManager reaches the client layer with its read-only
// GetBillingCycle only — the money-moving ops (close, sweep, onboarding) stay off
// the web and MCP surfaces.
func TestExposedManagers_BillingIsReadOnly(t *testing.T) {
	path := filepath.Join("..", "..", "..", ".aiarch", "state", "project.json") // the test runs in cmd/clientgen
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contracts := parseContracts(path, raw)
	ops, ok := exposedManagers["billingManager"]
	if !ok {
		t.Fatal("billingManager is not exposed")
	}
	got, err := exposedContract(contracts["billingManager"], ops)
	if err != nil {
		t.Fatal(err)
	}
	if names := opNames(t, got); !slices.Equal(names, []string{"GetBillingCycle"}) {
		t.Fatalf("billingManager exposes %v, want [GetBillingCycle]", names)
	}
	for _, key := range []string{"deliveryManager", "operationsManager"} {
		if ops, ok := exposedManagers[key]; !ok || ops != nil {
			t.Errorf("%s: want its whole interface exposed (nil op list), got %v (exposed=%v)", key, ops, ok)
		}
	}
}
