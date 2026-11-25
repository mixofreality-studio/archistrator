package main

// contractswindow_test.go — the record verb and the gate every built app runs must judge a design
// alike (RUN-029). SCN-UNADDRESSED, SCN-ACTOR-PATH-CLIENT and TP-CLIENT-CONTRACTED read the whole
// contract set, and contracts are recorded one component at a time, so they are advisory while a
// component's contract may still arrive. That window used to live in this package only: the record
// verb accepted the first contract and the construction task's command:arch check (the generated
// TestMethod, methodcheck.Check with no FindingPolicy) refused the same state, for good.

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// contractPrefixes is every prefix of the fixture's contracts recorded one at a time, in the order
// a construction agent records them (record order, then reverse), as project.json bytes: the
// fixture's design and activity state with only that prefix of its contracts.
func contractPrefixes(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	var fx conformanceFixtureDoc
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(doc["serviceContracts"], &contracts); err != nil {
		t.Fatal(err)
	}
	birth := conformanceBirth(t, raw)
	order := conformanceRecordOrder(t, birth, fx.ServiceContracts)
	reversed := slices.Clone(order)
	slices.Reverse(reversed)
	var out [][]byte
	for _, keys := range [][]string{order, reversed} {
		for k := 0; k <= len(keys); k++ {
			d := maps.Clone(doc)
			delete(d, "phaseArtifacts")
			have := map[string]json.RawMessage{}
			for _, key := range keys[:k] {
				have[key] = contracts[key]
			}
			b, err := json.Marshal(have)
			if err != nil {
				t.Fatal(err)
			}
			d["serviceContracts"] = b
			prefix, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, prefix)
		}
	}
	return out
}

// errorSet is the sorted "RULE: message" of every Error finding.
func errorSet(fs []methodcheck.Finding) []string {
	var out []string
	for _, f := range fs {
		if f.Severity == methodcheck.SeverityError {
			out = append(out, string(f.RuleID)+": "+f.Message)
		}
	}
	slices.Sort(out)
	return out
}

// verdicts is the Errors the record verb's own severity pipeline leaves on the project bytes b and
// the Errors the built app's gate reports, plus how many contract-set findings the gate holds as
// Warnings.
func verdicts(t *testing.T, b []byte) (verb, gate []string, advisory int) {
	t.Helper()
	contractRules := map[methodcheck.RuleID]bool{"SCN-UNADDRESSED": true, "SCN-ACTOR-PATH-CLIENT": true, "TP-CLIENT-CONTRACTED": true}
	proj, ok, err := projectstate.DecodeProjectJSON(b, "")
	if err != nil || !ok {
		t.Fatalf("does not decode: %v (ok=%v)", err, ok)
	}
	verbErrs, err := writeGateErrors(b, proj)
	if err != nil {
		t.Fatalf("record verb: %v", err)
	}
	mp, ok, err := methodcheck.DecodeProject(b)
	if err != nil || !ok {
		t.Fatalf("methodcheck decode: %v (ok=%v)", err, ok)
	}
	gateAll, err := methodcheck.GateFindings(mp, methodcheck.ProjectSpec{SupportedSurfaces: methodcheckOptions().SupportedSurfaces})
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	for _, f := range gateAll {
		if f.Severity == methodcheck.SeverityWarning && contractRules[f.RuleID] {
			advisory++
		}
	}
	return errorSet(verbErrs), errorSet(gateAll), advisory
}

// At every contract prefix of every conformance fixture, the Errors the record verb's own severity
// pipeline (writeGateErrors: methodcheck, then every policy of this package) leaves are exactly the
// Errors the built app's gate reports (methodcheck.GateFindings with the scaffold template's spec,
// no FindingPolicy). The test fails whenever a policy here reclassifies a methodcheck finding the
// gate would not: it failed on the state this fix replaces, where the window lived in this package.
// The window is deliberately not compared under an ambient design slot: slot scoping is the design
// rail's grandfathering of committed defects it cannot write, and no built app's gate runs there.
func TestRecordVerbVerdictIsTheGateVerdict_EveryContractPrefix(t *testing.T) {
	for _, name := range conformance.FixtureNames() {
		t.Run(name, func(t *testing.T) {
			raw, err := conformance.Fixture(name)
			if err != nil {
				t.Fatal(err)
			}
			advisory := 0
			for i, b := range contractPrefixes(t, raw) {
				verb, gate, n := verdicts(t, b)
				advisory += n
				if !slices.Equal(verb, gate) {
					t.Errorf("prefix %d: the record verb leaves Errors %v, the gate reports %v", i, verb, gate)
				}
			}
			if advisory == 0 && name == "todomvc" {
				t.Errorf("no contract prefix of %s held a contract-set rule as a Warning: the comparison never meets the window", name)
			}
		})
	}
}

// This package's policies never restate the window: a contract-set finding that methodcheck
// reports as an Error stays one through applyGateSeverityPolicies, whatever the contract set holds.
func TestGateSeverityPolicies_LeaveTheContractsWindowToMethodcheck(t *testing.T) {
	proj := deadlockFixtureProject(false)
	in := []methodcheck.Finding{
		{RuleID: "SCN-UNADDRESSED", Severity: methodcheck.SeverityError, Message: "unaddressed"},
		{RuleID: "SCN-ACTOR-PATH-CLIENT", Severity: methodcheck.SeverityError, Message: "no client"},
		{RuleID: "TP-CLIENT-CONTRACTED", Severity: methodcheck.SeverityError, Message: "no client contract"},
	}
	kept := map[methodcheck.RuleID]methodcheck.Finding{}
	for _, f := range applyGateSeverityPolicies(proj, 0, false, in) {
		kept[f.RuleID] = f
	}
	for _, want := range in {
		if got, ok := kept[want.RuleID]; !ok || got.Severity != methodcheck.SeverityError || got.Message != want.Message {
			t.Errorf("%s: policies changed an Error from methodcheck to %+v", want.RuleID, got)
		}
	}
}
