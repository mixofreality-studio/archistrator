package main

// admission_test.go holds the tiers of artifact admission (FPC Task 6.3): Tier 1 runs on every
// record verb, in process, and refuses what the generators cannot build; Tier 2 is the venue's
// admission check at commit, never a record verb's.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// admissionFixture is the todomvc conformance fixture and the state a project is born with.
func admissionFixture(t *testing.T) (conformanceFixtureDoc, projectstate.Project) {
	t.Helper()
	raw, err := conformance.Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	var fx conformanceFixtureDoc
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	return fx, conformanceBirth(t, raw)
}

// deepCopy clones a JSON-shaped map.
func deepCopy(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func stateBytes(t *testing.T, s *Session) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.StateRoot, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// recordContract records the contract filed under key through the verb as the component's own job.
func recordContract(t *testing.T, s *Session, birth projectstate.Project, key string, contract map[string]any) error {
	t.Helper()
	c := conformanceComponentOf(t, birth, key)
	s.ComponentID, s.ActivityID = c.ID, "C-"+c.ID
	return s.recordServiceContract(contract)
}

// spyAdmission replaces the Engine's Tier 1 op for the test with fn, and returns the kinds asked.
func spyAdmission(t *testing.T, fn func(kind string) []review.AdmissionProblem) *[]string {
	t.Helper()
	var kinds []string
	prev := admitArtifact
	admitArtifact = func(_, kind string) ([]review.AdmissionProblem, error) {
		kinds = append(kinds, kind)
		return fn(kind), nil
	}
	t.Cleanup(func() { admitArtifact = prev })
	return &kinds
}

// A def named like the constructor modelgen writes for the contract's interface is a contract the
// recorder accepts and modelgen cannot declare (a type and a function of one name): the verb refuses
// it with every problem, and writes nothing.
func TestRecordServiceContract_RefusesTier1Local(t *testing.T) {
	fx, birth := admissionFixture(t)
	s, _ := seedProject(t, birth, jobModeConstruct, 0)
	key := conformanceRecordOrder(t, birth, fx.ServiceContracts)[0]
	contract := deepCopy(t, fx.ServiceContracts[key])
	iface := contract["interface"].(map[string]any)["name"].(string)
	defs, _ := contract["$defs"].(map[string]any)
	if defs == nil {
		defs = map[string]any{}
		contract["$defs"] = defs
	}
	defs["New"+iface] = map[string]any{"type": "object", "properties": map[string]any{"A": map[string]any{"type": "string"}}, "required": []any{"A"}}

	before := stateBytes(t, s)
	err := recordContract(t, s, birth, key, contract)
	if err == nil {
		t.Fatal("a contract whose def collides with its interface must be refused by Tier 1")
	}
	for _, want := range []string{"Tier 1", "modelgen", "contract.gen.go", "redeclared"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal lacks %q:\n%v", want, err)
		}
	}
	if !bytes.Equal(before, stateBytes(t, s)) {
		t.Error("a refused record must leave project.json byte-unchanged")
	}
	if len(s.admissionNotes) != 0 {
		t.Errorf("a refusal carries no warnings: %v", s.admissionNotes)
	}
}

// A first contract of a fresh project cannot be built into the composed app yet: the composition
// needs the Manager the mcpApp fronts. That is the design in progress: the write lands, and the
// tool result says Tier 2 enforces what Tier 1 could not yet build.
func TestRecordServiceContract_Tier1IncompleteIsAWarning(t *testing.T) {
	fx, birth := admissionFixture(t)
	s, _ := seedProject(t, birth, jobModeConstruct, 0)
	key := conformanceRecordOrder(t, birth, fx.ServiceContracts)[0]
	if err := recordContract(t, s, birth, key, fx.ServiceContracts[key]); err != nil {
		t.Fatalf("an incomplete design must not refuse the first contract: %v", err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := proj.ServiceContracts[key]; !ok {
		t.Fatal("the write must land")
	}
	text := s.withAdmissionNotes("recorded.")
	if !strings.Contains(text, "Tier 1 (incomplete, enforced at Tier 2)") {
		t.Errorf("the tool result lacks the incomplete warning:\n%s", text)
	}
	if again := s.withAdmissionNotes("again."); again != "again." {
		t.Errorf("the warnings are delivered once, got %q", again)
	}
}

// The System slot is a consumer kind: a Local problem refuses the draft, and the System is the kind asked.
func TestPutDraftModel_RunsTier1ForTheSystem(t *testing.T) {
	web := `{"id":"c1","name":"WebClient","kind":"client","layer":"client","surface":"web","encapsulates":"presentation volatility"},`
	api := `{"id":"api","name":"TodoApi","kind":"client","layer":"client","surface":"rest","encapsulates":"machine API volatility"},`
	draft := []byte(`{"components":[` + web + api + surfaceDraftTail + `],
		"relationships":[{"from":"c1","to":"m1","mode":"sync","label":"capture"},{"from":"api","to":"m1","mode":"sync","label":"capture"},` + surfaceDraftRels + `],
		"dynamicViews":[]}`)
	kinds := spyAdmission(t, func(string) []review.AdmissionProblem {
		return []review.AdmissionProblem{{Consumer: "appgen compose", Path: "server/cmd/server/main.gen.go", Message: "1:1: boom", Severity: review.AdmissionLocal, Tier: review.AdmissionTier1}}
	})
	s, _ := seedProject(t, surfaceMissionProject(), jobModeDraft, projectstate.KindSystem)
	before := stateBytes(t, s)
	err := s.putDraftModel(draft)
	if err == nil || !strings.Contains(err.Error(), "appgen compose") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("a Local Tier 1 problem must refuse the System draft, got %v", err)
	}
	if !slices.Equal(*kinds, []string{"system"}) {
		t.Errorf("the System draft admits kind %v, want [system]", *kinds)
	}
	if !bytes.Equal(before, stateBytes(t, s)) {
		t.Error("a refused draft must leave project.json byte-unchanged")
	}
}

// The test plan's consumer is testgen: the plan record admits kind testPlan, and a UI design admits uiDesign.
func TestRecordPhaseArtifact_TestPlanRunsTier1(t *testing.T) {
	fx, birth := admissionFixture(t)
	s, _ := seedProject(t, birth, jobModeConstruct, 0)
	for _, key := range conformanceRecordOrder(t, birth, fx.ServiceContracts) {
		c := conformanceComponentOf(t, birth, key)
		if c.Kind == projectstate.CompClient {
			continue
		}
		if err := recordContract(t, s, birth, key, fx.ServiceContracts[key]); err != nil {
			t.Fatalf("recordServiceContract(%s): %v", key, err)
		}
	}
	kinds := spyAdmission(t, func(kind string) []review.AdmissionProblem {
		if kind != "testPlan" {
			return nil
		}
		return []review.AdmissionProblem{{Consumer: "testgen", Path: "server/internal/x/x_scenarios.gen_test.go", Message: "9:9: boom", Severity: review.AdmissionLocal, Tier: review.AdmissionTier1}}
	})
	for _, mapKey := range slices.Sorted(maps.Keys(fx.PhaseArtifacts.UIDesign)) {
		s.ComponentID, s.ActivityID = mapKey, "C-"+mapKey
		if err := s.recordPhaseArtifact(mapKey, map[string]any{"uiDesign": fx.PhaseArtifacts.UIDesign[mapKey]}); err != nil {
			t.Fatalf("recordPhaseArtifact(uiDesign %s): %v", mapKey, err)
		}
	}
	key := slices.Sorted(maps.Keys(fx.PhaseArtifacts.TestPlan))[0]
	c := conformanceComponentOf(t, birth, key)
	s.ComponentID, s.ActivityID = c.ID, "C-"+c.ID
	before := stateBytes(t, s)
	err := s.recordPhaseArtifact(key, map[string]any{"testPlan": fx.PhaseArtifacts.TestPlan[key]})
	if err == nil || !strings.Contains(err.Error(), "testgen") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("a Local Tier 1 problem must refuse the test plan, got %v", err)
	}
	if !bytes.Equal(before, stateBytes(t, s)) {
		t.Error("a refused plan must leave project.json byte-unchanged")
	}
	if len(*kinds) == 0 || (*kinds)[len(*kinds)-1] != "testPlan" {
		t.Errorf("the plan record admits kind testPlan, asked %v", *kinds)
	}
	if !slices.Contains(*kinds, "uiDesign") {
		t.Errorf("the UI design record admits kind uiDesign, asked %v", *kinds)
	}
}

// Tier 2 is the venue's, at commit: the tasks whose merge commits a generated-from artifact carry
// the check, no construction or integration task does, no record verb runs it, and the check runs
// the conformance chain over the checkout's project.json and reports the failing step's stderr.
func TestAdmitTier2_RunsAtCommitOnly(t *testing.T) {
	requireTier2OnCommittingTasksOnly(t)

	var chains int
	prev := runChainOn
	runChainOn = func(context.Context, string, []byte, string, ...conformance.ChainOption) (conformance.Report, error) {
		chains++
		return conformance.Report{}, nil
	}
	t.Cleanup(func() { runChainOn = prev })
	recordWholeFixture(t)
	if chains != 0 {
		t.Fatalf("the record verbs ran the conformance chain %d times: Tier 2 is the venue's check at commit", chains)
	}

	requireAdmissionCheckRunsTheChain(t, &chains)
}

// requireTier2OnCommittingTasksOnly is (a): which specs carry the check.
func requireTier2OnCommittingTasksOnly(t *testing.T) {
	t.Helper()
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(freshState(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		activity, task string
		want           bool
	}{
		{freshManagerActivity, "detailedDesign", true},
		{freshManagerActivity, "stp", true},
		{freshSurfaceActivity, "detailedDesign", true}, // the UI design
		{freshManagerActivity, "construction", false},
		{freshManagerActivity, "integration", false},
		{freshSurfaceActivity, "integration", false},
	} {
		spec, err := deriveTaskSpec(&proj, c.activity, c.task)
		if err != nil {
			t.Fatalf("%s/%s: %v", c.activity, c.task, err)
		}
		i := slices.IndexFunc(spec.Checks, func(k review.SpecCheck) bool { return k.Kind == projectstate.CheckKindAdmission })
		if (i >= 0) != c.want {
			t.Errorf("%s/%s: admission check present = %v, want %v (checks %v)", c.activity, c.task, i >= 0, c.want, specCheckIDs(spec))
			continue
		}
		if c.want && (spec.Checks[i].ID != "admission:tier2" || spec.Checks[i].TimeoutSec != 1800) {
			t.Errorf("%s/%s: admission check = %+v, want admission:tier2 with an 1800 s cap", c.activity, c.task, spec.Checks[i])
		}
	}
}

// recordWholeFixture is (b)'s workload: every contract, UI design and test plan of the todomvc
// fixture recorded through the verbs.
func recordWholeFixture(t *testing.T) {
	t.Helper()
	fx, birth := admissionFixture(t)
	s, _ := seedProject(t, birth, jobModeConstruct, 0)
	for _, key := range conformanceRecordOrder(t, birth, fx.ServiceContracts) {
		if conformanceComponentOf(t, birth, key).Kind == projectstate.CompClient {
			continue
		}
		if err := recordContract(t, s, birth, key, fx.ServiceContracts[key]); err != nil {
			t.Fatalf("recordServiceContract(%s): %v", key, err)
		}
	}
	for _, mapKey := range slices.Sorted(maps.Keys(fx.PhaseArtifacts.UIDesign)) {
		s.ComponentID, s.ActivityID = mapKey, "C-"+mapKey
		if err := s.recordPhaseArtifact(mapKey, map[string]any{"uiDesign": fx.PhaseArtifacts.UIDesign[mapKey]}); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(fx.PhaseArtifacts.TestPlan)) {
		c := conformanceComponentOf(t, birth, key)
		s.ComponentID, s.ActivityID = c.ID, "C-"+c.ID
		if err := s.recordPhaseArtifact(key, map[string]any{"testPlan": fx.PhaseArtifacts.TestPlan[key]}); err != nil {
			t.Fatal(err)
		}
	}
}

// requireAdmissionCheckRunsTheChain is (c): run-checks over a spec holding the check runs the chain
// once over the checkout's document, and a failing chain's stderr is the log.
func requireAdmissionCheckRunsTheChain(t *testing.T, chains *int) {
	t.Helper()
	root := t.TempDir()
	writeRepoFile(t, root, ".aiarch/state/project.json", freshState(t))
	var gotRaw []byte
	var gotFixture string
	failing := true
	runChainOn = func(_ context.Context, fixture string, raw []byte, _ string, _ ...conformance.ChainOption) (conformance.Report, error) {
		*chains++
		gotRaw, gotFixture = raw, fixture
		if !failing {
			return conformance.Report{Steps: []conformance.StepResult{{Name: "scaffold", Duration: time.Second}}}, nil
		}
		return conformance.Report{Steps: []conformance.StepResult{
			{Name: "scaffold", Duration: time.Second},
			{Name: "go build ./...", ExitCode: 1, Stderr: "internal/x/x.go:3:1: undefined: Y", Duration: time.Second},
		}}, errors.New("go build ./...: exit status 1")
	}
	r := checkRunner{root: root, outDir: filepath.Join(root, "check-results"), now: time.Now}
	check := review.SpecCheck{ID: "admission:tier2", Kind: projectstate.CheckKindAdmission, Title: "Tier 2", TimeoutSec: 60}
	*chains = 0
	res := r.Run(context.Background(), []review.SpecCheck{check})
	if *chains != 1 {
		t.Fatalf("run-checks ran the chain %d times, want once", *chains)
	}
	if !bytes.Equal(gotRaw, freshState(t)) || gotFixture != "project" {
		t.Errorf("the chain ran over fixture %q and %d bytes, want %q over the checkout's project.json", gotFixture, len(gotRaw), "project")
	}
	log, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "admission_tier2.log"))
	if res[0].Status != checkFail || res[0].Kind != projectstate.CheckKindAdmission || !strings.Contains(string(log), "undefined: Y") {
		t.Fatalf("a failing chain = %s %s, log:\n%s", res[0].Status, res[0].Kind, log)
	}
	failing = false
	if res := r.Run(context.Background(), []review.SpecCheck{check}); res[0].Status != checkPass {
		t.Errorf("a green chain = %s, want pass", res[0].Status)
	}
}
