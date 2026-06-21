package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// fixture1 and fixture2 are minimal service contract JSON objects written to a
// temp data dir. Keys are camelCase to verify Go's case-insensitive unmarshal.
var fixture1 = map[string]any{
	"component":  "billingEngine",
	"layer":      "Engine",
	"stereotype": "«Engine» — pure business computation",
	"volatility": "BillingRules",
	"status":     "FROZEN",
	"ops": []map[string]any{
		{"signature": "ComputeInvoice(usage Usage) (Invoice, error)", "stereotype": "Engine pure", "note": "no side-effects"},
		{"signature": "ValidatePayment(payment Payment) error", "stereotype": "Engine pure", "note": "idempotent"},
		{"signature": "ApplyDiscount(invoice Invoice, code string) (Invoice, error)", "stereotype": "Engine pure", "note": ""},
	},
	"inbound":       []map[string]any{},
	"outbound":      []map[string]any{},
	"dataContracts": []string{"Invoice", "Usage"},
	"errorModel":    "fwra.Error kind-discriminated",
	"idempotency":   "none — pure function",
	"revisions":     []map[string]any{},
}

var fixture2 = map[string]any{
	"component":  "settlementManager",
	"layer":      "Manager",
	"stereotype": "«Manager» — orchestrates settlement workflows",
	"volatility": "SettlementChannel",
	"status":     "IN-DESIGN",
	"ops": []map[string]any{
		{"signature": "OnboardPaymentIntegration(intent OnboardIntent) (SettlementRef, error)", "stereotype": "Manager Temporal workflow", "note": "UC5"},
	},
	"inbound": []map[string]any{
		{"name": "webClient", "layer": "Client"},
	},
	"outbound": []map[string]any{
		{"name": "billingEngine", "layer": "Engine", "how": "direct call"},
	},
	"dataContracts": []string{"SettlementRef"},
	"errorModel":    "fwra.Error kind-discriminated",
	"idempotency":   "caller-minted UUID key",
	"revisions": []map[string]any{
		{"rev": "r1", "at": "2026-06-17", "by": "architect", "byActivity": "D-CW", "summary": "initial draft"},
	},
}

// TestLoadContracts verifies that loadContracts reads two fixture JSON files from
// a temp dir and returns a map with both contracts keyed by their Component field.
func TestLoadContracts(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "billingEngine.json", fixture1)
	writeFixture(t, dir, "settlementManager.json", fixture2)

	contracts, err := loadContracts(dir)
	if err != nil {
		t.Fatalf("loadContracts: %v", err)
	}

	if len(contracts) != 2 {
		t.Fatalf("len(contracts) = %d, want 2", len(contracts))
	}

	be, ok := contracts["billingEngine"]
	if !ok {
		t.Fatal("contracts[billingEngine] absent")
	}
	if be.Component != "billingEngine" {
		t.Errorf("billingEngine.Component = %q, want billingEngine", be.Component)
	}
	if len(be.Ops) != 3 {
		t.Errorf("billingEngine.Ops len = %d, want 3", len(be.Ops))
	}
	if be.Layer != "Engine" {
		t.Errorf("billingEngine.Layer = %q, want Engine", be.Layer)
	}

	sm, ok := contracts["settlementManager"]
	if !ok {
		t.Fatal("contracts[settlementManager] absent")
	}
	if sm.Component != "settlementManager" {
		t.Errorf("settlementManager.Component = %q, want settlementManager", sm.Component)
	}
	if len(sm.Ops) != 1 {
		t.Errorf("settlementManager.Ops len = %d, want 1", len(sm.Ops))
	}
	if len(sm.Revisions) != 1 {
		t.Errorf("settlementManager.Revisions len = %d, want 1", len(sm.Revisions))
	}
	if sm.Revisions[0].Rev != "r1" {
		t.Errorf("settlementManager.Revisions[0].Rev = %q, want r1", sm.Revisions[0].Rev)
	}
	if len(sm.Inbound) != 1 {
		t.Errorf("settlementManager.Inbound len = %d, want 1", len(sm.Inbound))
	}
	if len(sm.Outbound) != 1 {
		t.Errorf("settlementManager.Outbound len = %d, want 1", len(sm.Outbound))
	}
}

// TestLoadContracts_FallbackStem verifies that when Component is empty the
// filename stem is used as the map key.
func TestLoadContracts_FallbackStem(t *testing.T) {
	dir := t.TempDir()
	noComponent := map[string]any{
		"component": "",
		"layer":     "Engine",
		"ops":       []map[string]any{},
	}
	writeFixture(t, dir, "myComponent.json", noComponent)

	contracts, err := loadContracts(dir)
	if err != nil {
		t.Fatalf("loadContracts: %v", err)
	}
	if _, ok := contracts["myComponent"]; !ok {
		t.Fatal("expected key myComponent (fallback to filename stem)")
	}
}

// TestLoadContracts_SkipsBadJSON verifies that a malformed JSON file emits a
// WARNING and is skipped, while valid files are still returned.
func TestLoadContracts_SkipsBadJSON(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "billingEngine.json", fixture1)
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	contracts, err := loadContracts(dir)
	if err != nil {
		t.Fatalf("loadContracts: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("len(contracts) = %d, want 1 (corrupt file skipped)", len(contracts))
	}
	if _, ok := contracts["billingEngine"]; !ok {
		t.Fatal("billingEngine absent")
	}
}

// TestIngest performs a full round-trip: writes two fixture contract files to a
// temp data dir, writes a minimal project.json to a temp file, calls ingest,
// then re-reads the file and asserts both contracts are present with correct keys
// and op counts. All other Project fields (slots, phase, etc.) must be untouched.
func TestIngest(t *testing.T) {
	dataDir := t.TempDir()
	writeFixture(t, dataDir, "billingEngine.json", fixture1)
	writeFixture(t, dataDir, "settlementManager.json", fixture2)

	// Build a minimal project.json using the public codec.
	proj := ps.Project{ID: "archistrator"}
	raw, err := ps.EncodeProjectJSON(proj)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	projFile := filepath.Join(t.TempDir(), "project.json")
	if err := os.WriteFile(projFile, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := ingest(dataDir, projFile, "archistrator")
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if n != 2 {
		t.Fatalf("ingest returned %d, want 2", n)
	}

	// Re-read and decode the rewritten file.
	got, err := os.ReadFile(projFile)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	p, ok, err := ps.DecodeProjectJSON(got, "archistrator")
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON ok=false")
	}

	if len(p.ServiceContracts) != 2 {
		t.Fatalf("ServiceContracts len = %d, want 2", len(p.ServiceContracts))
	}

	be := p.ServiceContracts["billingEngine"]
	if be.Component != "billingEngine" {
		t.Errorf("billingEngine.Component = %q", be.Component)
	}
	if len(be.Ops) != 3 {
		t.Errorf("billingEngine Ops = %d, want 3", len(be.Ops))
	}

	sm := p.ServiceContracts["settlementManager"]
	if sm.Component != "settlementManager" {
		t.Errorf("settlementManager.Component = %q", sm.Component)
	}
	if len(sm.Ops) != 1 {
		t.Errorf("settlementManager Ops = %d, want 1", len(sm.Ops))
	}
	if len(sm.Revisions) != 1 || sm.Revisions[0].Rev != "r1" {
		t.Errorf("settlementManager.Revisions unexpected: %+v", sm.Revisions)
	}
}

// writeFixture marshals v as JSON and writes it to dir/name.
func writeFixture(t *testing.T, dir, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal fixture %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}
