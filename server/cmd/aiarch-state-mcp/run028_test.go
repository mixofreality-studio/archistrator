package main

// run028_test.go pins ISS-028-1. RUN-028 (todomvc): methodcheck reports UC-OUTCOME-AT-ASKING-SURFACE
// (and UC-LANE-RESOLVES, UC-IO-KINDS, UC-NO-IO) only once the System is recorded, but the prefix
// table attributed every UC- rule to CoreUseCases. The System draft check dropped them, the System
// write downgraded them, and the first construction write refused them where no one could fix them.
// A rule is now owned by the slot that records its LAST prerequisite.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// run028Order is the order the Method records the artifacts a rule can name as a prerequisite.
var run028Order = []string{
	"mission", "glossary", "scrubbedRequirements", "volatilities", "coreUseCases", "system",
	"operationalConcepts", "standardCheck", "serviceContracts",
	"phaseArtifacts.testPlan", "phaseArtifacts.uiDesign",
}

var run028NonSlot = []string{"serviceContracts", "phaseArtifacts.testPlan", "phaseArtifacts.uiDesign"}

func TestRegistryEveryPrerequisiteMapsToAKindOrANonSlotRecord(t *testing.T) {
	for _, id := range methodcheck.EmittedRuleIDs() {
		prs, ok := methodcheck.RulePrerequisites(id)
		if !ok {
			continue
		}
		for _, pr := range prs {
			name := methodcheck.PrerequisiteName(pr)
			if !slices.Contains(run028Order, name) {
				t.Errorf("rule %s: prerequisite %q is not in the recording order", id, name)
				continue
			}
			if slices.Contains(run028NonSlot, name) {
				continue
			}
			if k, ok := projectstate.ArtifactKindFromWireName(name); !ok || k < projectstate.KindMission || k > projectstate.KindStandardCheck {
				t.Errorf("rule %s: prerequisite %q maps to no design slot kind", id, name)
			}
		}
	}
}

func TestRegistryRuleIsOwnedByItsLatestPrerequisite(t *testing.T) {
	checked := 0
	for _, id := range methodcheck.EmittedRuleIDs() {
		prs, ok := methodcheck.RulePrerequisites(id)
		if !ok || len(prs) == 0 {
			continue
		}
		latest, rank, allSlots := "", -1, true
		for _, pr := range prs {
			name := methodcheck.PrerequisiteName(pr)
			if slices.Contains(run028NonSlot, name) {
				allSlots = false
			}
			if r := slices.Index(run028Order, name); r > rank {
				latest, rank = name, r
			}
		}
		if !allSlots {
			continue
		}
		want, found := projectstate.ArtifactKindFromWireName(latest)
		if !found {
			t.Errorf("rule %s: latest prerequisite %q has no kind", id, latest)
			continue
		}
		checked++
		if kind, class := projectstate.AttributeRule(string(id)); class != projectstate.AttribSlot || kind != want {
			t.Errorf("rule %s: AttributeRule = (%v, %v), want (%v, AttribSlot): owned by its latest prerequisite %q", id, kind, class, want, latest)
		}
		if !ruleInScope(string(id), want, true, projectstate.RulePrefixesFor(want)) {
			t.Errorf("rule %s: a rule check with RuleSlot %s drops it", id, want.WireName())
		}
		f := []methodcheck.Finding{{RuleID: id, Severity: methodcheck.SeverityError}}
		if got := applySlotScopeDowngrades(want, f)[0].Severity; got != methodcheck.SeverityError {
			t.Errorf("rule %s: the %s write downgrades it to %v, want full severity", id, want.WireName(), got)
		}
	}
	if checked == 0 {
		t.Fatal("no emitted rule has design-slot prerequisites: the registry gate checked nothing")
	}
}

func TestRegistryUCRulesEmittedByTheArchitectureValidatorBelongToTheSystem(t *testing.T) {
	for _, id := range []string{"UC-OUTCOME-AT-ASKING-SURFACE", "UC-LANE-RESOLVES", "UC-IO-KINDS", "UC-NO-IO"} {
		if kind, class := projectstate.AttributeRule(id); kind != projectstate.KindSystem || class != projectstate.AttribSlot {
			t.Errorf("AttributeRule(%s) = (%v, %v), want (System, AttribSlot)", id, kind, class)
		}
	}
}

// run028Root is the checked-in RUN-028 architecture commit as a repo root.
const run028Root = "testdata/run028-architecture"

func run028Errors(t *testing.T) []methodcheck.Finding {
	t.Helper()
	fs, err := validateFindings(run028Root, projectstate.KindSystem, true)
	if err != nil {
		t.Fatal(err)
	}
	var errs []methodcheck.Finding
	for _, f := range fs {
		if f.Severity == methodcheck.SeverityError {
			errs = append(errs, f)
		}
	}
	return errs
}

func TestRun028ValidateReportsTheEightUCErrorsWithSystemAmbient(t *testing.T) {
	errs := run028Errors(t)
	n := 0
	for _, f := range errs {
		if !strings.HasPrefix(string(f.RuleID), "UC-") {
			t.Errorf("unexpected Error %s: %s", f.RuleID, f.Message)
			continue
		}
		n++
	}
	if n != 8 {
		t.Fatalf("want the 8 UC-* Errors of RUN-028 at full severity under an ambient System, got %d", n)
	}
}

func TestRun028RuleSystemFailsNamingThem(t *testing.T) {
	root := t.TempDir()
	raw, err := os.ReadFile(filepath.Join(run028Root, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, statePathPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, statePathPrefix, projectFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	r := checkRunner{root: root, outDir: t.TempDir(), now: time.Now}
	res := r.Run(context.Background(), []review.SpecCheck{{
		ID: "rule:system", Kind: "rule", TimeoutSec: 60,
		Source: review.SpecCheckSource{RuleSlot: ptr("System"), RulePrefixes: projectstate.RulePrefixesFor(projectstate.KindSystem)},
	}})
	log, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "rule_system.log"))
	if res[0].Status != "fail" {
		t.Fatalf("rule:system passed over the RUN-028 architecture; log:\n%s", log)
	}
	if got := strings.Count(string(log), "UC-OUTCOME-AT-ASKING-SURFACE"); got != 8 {
		t.Fatalf("the log must name UC-OUTCOME-AT-ASKING-SURFACE 8 times, got %d:\n%s", got, log)
	}
}

func TestRun028PutDraftModelOfTheSystemIsRefused(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(run028Root, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	proj, _, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	slot, ok := slotFor(&proj, projectstate.KindSystem)
	if !ok || slot.Model == nil {
		t.Fatal("the fixture carries no System slot")
	}
	draft, err := json.Marshal(slot.Model)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := seedProject(t, proj, jobModeDraft, projectstate.KindSystem)
	err = s.putDraftModel(draft)
	if err == nil {
		t.Fatal("putDraftModel of the System must be refused: the UC-* Errors are the System's to fix")
	}
	if !strings.Contains(err.Error(), "UC-OUTCOME-AT-ASKING-SURFACE") {
		t.Fatalf("the refusal must name the rule, got: %v", err)
	}
	if s.wroteState {
		t.Fatal("wroteState set despite a refusal")
	}
}
