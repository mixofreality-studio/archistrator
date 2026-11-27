package main

// runtpcontract_test.go pins ISS-032-2: a contract-scoped TP check (rule:tp:<contractKey>)
// requires that contract's phaseArtifacts.testPlan record. methodcheck runs TP-BOUND only
// over components that HAVE a plan, so without the requirement a plan-less contract passed
// vacuously (RUN-032: a testingState Defect stood in for the plan).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
)

const tpContract = "autoscalerEngine"

func tpCheck(contract string) review.SpecCheck {
	c := review.SpecCheck{
		ID: "rule:tp", Kind: "rule", TimeoutSec: 60,
		Source: review.SpecCheckSource{RulePrefixes: []string{"TP-"}},
	}
	if contract != "" {
		c.ID = "rule:tp:" + contract
		c.Source.Contract = &contract
	}
	return c
}

// runTP runs check over a checkout whose state document is state, returning status and log.
func runTP(t *testing.T, state []byte, check review.SpecCheck) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, statePathPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, statePathPrefix, projectFile), state, 0o644); err != nil {
		t.Fatal(err)
	}
	r := checkRunner{root: root, outDir: t.TempDir(), now: time.Now}
	res := r.Run(context.Background(), []review.SpecCheck{check})
	logs, _ := filepath.Glob(filepath.Join(r.outDir, "checks", "*.log"))
	if len(logs) != 1 {
		t.Fatalf("want one check log, got %v", logs)
	}
	log, _ := os.ReadFile(logs[0])
	return res[0].Status, string(log)
}

// withoutPlan is the committed document with contract's plan entry removed, and, when
// defect is set, a testingState Defect recorded in its place.
func withoutPlan(t *testing.T, raw []byte, contract string, defect bool) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	pa, _ := doc["phaseArtifacts"].(map[string]any)
	plans, _ := pa["testPlan"].(map[string]any)
	if _, ok := plans[contract]; !ok {
		t.Fatalf("the committed document has no plan for %s", contract)
	}
	delete(plans, contract)
	if defect {
		ts, _ := doc["testingState"].(map[string]any)
		if ts == nil {
			ts = map[string]any{}
			doc["testingState"] = ts
		}
		ts["defects"] = []any{map[string]any{"id": "D-1", "title": "no plan", "severity": "high"}}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestContractScopedTPFailsWithoutAPlan(t *testing.T) {
	raw, err := os.ReadFile(committedProject)
	if err != nil {
		t.Fatal(err)
	}
	for name, defect := range map[string]bool{"no record": false, "only a testingState Defect": true} {
		t.Run(name, func(t *testing.T) {
			state := withoutPlan(t, raw, tpContract, defect)
			status, log := runTP(t, state, tpCheck(tpContract))
			if status != "fail" {
				t.Fatalf("a contract with no plan must fail; status %s, log:\n%s", status, log)
			}
			if !strings.Contains(log, "no test plan recorded for "+tpContract) {
				t.Fatalf("the log must name the contract and the missing plan:\n%s", log)
			}
			// The unscoped check is unchanged: it passes vacuously over the same document.
			if status, log := runTP(t, state, tpCheck("")); status != "pass" {
				t.Fatalf("the unscoped rule:tp must behave as before; status %s, log:\n%s", status, log)
			}
		})
	}
}

func TestContractScopedTPFailsForAnUnknownContract(t *testing.T) {
	raw, err := os.ReadFile(committedProject)
	if err != nil {
		t.Fatal(err)
	}
	status, log := runTP(t, raw, tpCheck("noSuchContract"))
	if status != "fail" || !strings.Contains(log, "no test plan recorded for noSuchContract") {
		t.Fatalf("a non-contract key must fail naming it; status %s, log:\n%s", status, log)
	}
}

func TestContractScopedTPPassesOnceAPlanIsRecorded(t *testing.T) {
	raw, err := os.ReadFile(committedProject)
	if err != nil {
		t.Fatal(err)
	}
	want, wlog := runTP(t, raw, tpCheck(""))
	got, glog := runTP(t, raw, tpCheck(tpContract))
	if got != want || got != "pass" {
		t.Fatalf("scoped %s (log:\n%s)\nunscoped %s (log:\n%s)", got, glog, want, wlog)
	}
	if strings.Contains(glog, "no test plan recorded") {
		t.Fatalf("a recorded plan must not be reported missing:\n%s", glog)
	}
}
