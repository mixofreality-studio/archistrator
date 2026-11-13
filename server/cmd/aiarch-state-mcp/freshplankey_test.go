package main

// freshplankey_test.go is the regression for the wall RUN-008 hit: a fresh project's component
// id is kebab-case and its contract key camelCase, and a plan filed under the id made testgen
// write verdicts to test-results/<id> while run_checks read test-results/<contract>, so every
// scenario reported "no verdict recorded". It uses the plan and the contract an agent really
// recorded in that run (testdata/fresh/run008.*), recorded through the verb under the kebab id.

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	run008Activity  = "C-change-notice-access"
	run008Component = "change-notice-access"
	run008Contract  = "changeNoticeAccess"
)

// run008State is the state document as the plan's task found it: the contract recorded, no plan
// yet, and none of the run's own check evidence.
func run008State(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run008.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	top["phaseArtifacts"].(map[string]any)["testPlan"] = map[string]any{}
	delete(top, "testingState")
	var anyProject projectstate.ProjectID
	mid, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	mid = conformClientDesign(t, mid)
	proj, _, err := projectstate.DecodeProjectJSON(mid, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	out, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func run008Plan(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run008.plan."+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if plan["component"] != name {
		t.Fatalf("the fixture must keep the kebab id RUN-008's agent recorded, got %v", plan["component"])
	}
	return plan
}

func recordedPlans(t *testing.T, repo string) map[string]struct {
	Component string `json:"component"`
} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, ".aiarch/state/project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		PhaseArtifacts struct {
			TestPlan map[string]struct {
				Component string `json:"component"`
			} `json:"testPlan"`
		} `json:"phaseArtifacts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.PhaseArtifacts.TestPlan
}

// A plan recorded through the verb under the kebab id lands under the contract key, and
// integration's real scenario checks then find every verdict, green. The component is the
// conformance fixture's TodoListAccess, its plan the fixture's own (binding model v2), recorded
// under the kebab id the way RUN-008's agent filed its plan.
func TestFreshPlanRecordedUnderItsIDRunsGreen(t *testing.T) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(freshState(t), &top); err != nil {
		t.Fatal(err)
	}
	var artifacts struct {
		TestPlan map[string]map[string]any `json:"testPlan"`
		UIDesign json.RawMessage           `json:"uiDesign"`
	}
	if err := json.Unmarshal(top["phaseArtifacts"], &artifacts); err != nil {
		t.Fatal(err)
	}
	plan := artifacts.TestPlan[freshContract]
	if plan == nil {
		t.Fatalf("the fixture has no plan for %s", freshContract)
	}
	planned := len(artifacts.TestPlan)
	delete(artifacts.TestPlan, freshContract)
	var err error
	if top["phaseArtifacts"], err = json.Marshal(artifacts); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	plan["component"] = freshComponent // the kebab id; the verb files it under the contract key

	// Every other component is built and integrated; this one's plan is the stp task's to record.
	repo := integratedRepoFrom(t, state)
	s := &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: freshActivity, TaskID: "stp", ComponentID: freshComponent, git: runGit}
	if err := s.recordPhaseArtifact(freshComponent, map[string]any{"testPlan": plan}); err != nil {
		t.Fatal(err)
	}
	plans := recordedPlans(t, repo)
	if p, ok := plans[freshContract]; !ok || p.Component != freshContract || len(plans) != planned {
		t.Fatalf("the plan must be filed under the contract key, got %+v", plans)
	}

	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "go", "mod", "tidy")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "bind the component")

	runIntegrationScenarios(t, repo)
	if _, err := os.Stat(filepath.Join(repo, "test-results", freshContract, "results.json")); err != nil {
		t.Errorf("the verdicts must land under the contract key: %v", err)
	}
}

// runIntegrationScenarios gates the integration's scenario checks (plus the gen-tests drift
// check) in repo and requires every verdict green.
func runIntegrationScenarios(t *testing.T, repo string) {
	t.Helper()
	spec := activitySpec(t, repo, freshActivity, "integration")
	kept := spec.Checks[:0:0]
	for _, c := range spec.Checks {
		if c.Kind == projectstate.CheckKindScenario || c.ID == "command:gen-tests-check" {
			kept = append(kept, c)
		}
	}
	scenarios := len(kept) - 1
	if scenarios < 1 {
		t.Fatalf("integration has no scenario checks: %+v", spec.Checks)
	}
	spec.Checks = kept
	statuses, logs := gate(t, repo, spec)
	requirePassed(t, statuses, logs)
	if len(statuses) != scenarios+1 {
		t.Errorf("got %d results, want %d", len(statuses), scenarios+1)
	}
}

// The archived list-record-access plan, under its own kebab id, is filed the same way once its
// contract exists, and refused before: a plan with no contract has nothing to be filed under.
func TestFreshPlanWithoutItsContractIsRefused(t *testing.T) {
	repo := bornRepo(t, run008State(t))
	s := &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: "C-list-record-access", TaskID: "stp", ComponentID: "list-record-access", git: runGit}
	plan := run008Plan(t, "list-record-access")
	err := s.recordPhaseArtifact("list-record-access", map[string]any{"testPlan": plan})
	if err == nil || !strings.Contains(err.Error(), "record the contract first") {
		t.Fatalf("got %v, want a refusal", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run008.contract.list-record-access.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	s.ActivityID = "C-list-record-access"
	if err := s.recordServiceContract(contract); err != nil {
		t.Fatal(err)
	}
	if err := s.recordPhaseArtifact("list-record-access", map[string]any{"testPlan": plan}); err != nil {
		t.Fatal(err)
	}
	if p, ok := recordedPlans(t, repo)["listRecordAccess"]; !ok || p.Component != "listRecordAccess" {
		t.Fatalf("the plan must be filed under the contract key, got %+v", recordedPlans(t, repo))
	}
}

// Integration's allegation carries what the verb stamps, so the agent cannot report for another
// attempt or set the baseline it is judged against, and it refuses what no run could make good.
func TestFreshReportPlanDefect(t *testing.T) {
	repo := bornRepo(t, run008State(t))
	s := &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: run008Activity, TaskID: "stp", ComponentID: run008Component, git: runGit}
	if err := s.recordPhaseArtifact(run008Component, map[string]any{"testPlan": run008Plan(t, run008Component)}); err != nil {
		t.Fatal(err)
	}
	const scenarioID = "add-a-todo-P1"
	valid := reportPlanDefectInput{Contract: run008Contract, Scenarios: []string{scenarioID}, Reason: "clause 2 refuses an empty owner"}

	s.TaskID = "integration"
	if err := s.reportPlanDefect(valid); err == nil || !strings.Contains(err.Error(), "attempt") {
		t.Errorf("with no ambient attempt the verb must refuse, got %v", err)
	}
	const revision = run008Activity + ":integration:3"
	t.Setenv(envRevision, revision)
	assertReportRefusals(t, s, scenarioID)
	s.TaskID = "construction"
	if err := s.reportPlanDefect(valid); err == nil || !strings.Contains(err.Error(), "no test plan") {
		t.Errorf("a task with no route must be refused, got %v", err)
	}
	s.TaskID = "integration"
	if err := s.reportPlanDefect(valid); err != nil {
		t.Fatal(err)
	}
	if err := s.reportPlanDefect(valid); err != nil {
		t.Fatal(err)
	}
	assertReportStamped(t, s, revision, scenarioID)
}

// "Binding stands" is a legal answer to a report, and the job fails a run that leaves the branch
// where it was, so an unchanged re-record must still advance it, with the plan's digests unmoved:
// they are how the upheld binding is recognised and the re-plan judged.
func TestFreshPlanReRecordedUnchangedAdvancesTheBranch(t *testing.T) {
	repo := bornRepo(t, run008State(t))
	session := func() *Session {
		return &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: run008Activity, TaskID: "stp", ComponentID: run008Component, git: runGit}
	}
	plan := run008Plan(t, run008Component)
	first := session()
	if err := first.recordPhaseArtifact(run008Component, map[string]any{"testPlan": plan}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.publishDraft("the plan"); err != nil {
		t.Fatal(err)
	}
	digests := func() map[string]string {
		proj, _, err := session().readProject()
		if err != nil {
			t.Fatal(err)
		}
		d, err := projectstate.PlanBindingDigests(&proj, []string{run008Contract})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	before, tip := digests(), inRepo(t, repo, "git", "rev-parse", "HEAD")

	redraft := session()
	if _, err := redraft.publishDraft("nothing recorded"); err == nil {
		t.Fatal("a redraft that records nothing must be refused, which is why the prompt says to re-record")
	}
	if err := redraft.recordPhaseArtifact(run008Contract, map[string]any{"testPlan": run008Plan(t, run008Component)}); err != nil {
		t.Fatal(err)
	}
	if _, err := redraft.publishDraft("binding stands"); err != nil {
		t.Fatal(err)
	}
	if got := inRepo(t, repo, "git", "rev-parse", "HEAD"); got == tip {
		t.Error("the branch did not advance, so the job would fail the redraft")
	}
	if !maps.Equal(before, digests()) {
		t.Error("re-recording an unchanged plan moved its digests, so the binding would read as changed")
	}
}

// assertReportRefusals checks what the verb refuses whatever the run says.
func assertReportRefusals(t *testing.T, s *Session, scenarioID string) {
	t.Helper()
	for name, tc := range map[string]struct {
		in   reportPlanDefectInput
		want string
	}{
		"no reason":           {reportPlanDefectInput{Contract: run008Contract, Scenarios: []string{scenarioID}}, "reason"},
		"no scenarios":        {reportPlanDefectInput{Contract: run008Contract, Reason: "x"}, "scenarios"},
		"another contract":    {reportPlanDefectInput{Contract: "ghostAccess", Scenarios: []string{scenarioID}, Reason: "x"}, "not one of this activity's contracts"},
		"an unbound scenario": {reportPlanDefectInput{Contract: run008Contract, Scenarios: []string{"ghost-P1"}, Reason: "x"}, "binds no scenario"},
		"a scenario twice":    {reportPlanDefectInput{Contract: run008Contract, Scenarios: []string{scenarioID, scenarioID}, Reason: "x"}, "named twice"},
	} {
		if err := s.reportPlanDefect(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want a refusal naming %q", name, err, tc.want)
		}
	}
}

// assertReportStamped checks the one report on the document: the verb's stamps, a baseline over
// every binding, and that it alone may write one and that its digests survive the workflow boundary.
func assertReportStamped(t *testing.T, s *Session, revision, scenarioID string) {
	t.Helper()
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if proj.TestingState == nil || len(proj.TestingState.PlanDefects) != 1 {
		t.Fatalf("a repeat under one attempt must replace: %+v", proj.TestingState)
	}
	got := proj.TestingState.PlanDefects[0]
	if got.ID != revision+":"+run008Contract || got.Revision != revision || got.Activity != run008Activity || got.Task != "integration" {
		t.Errorf("the verb's stamps are wrong: %+v", got)
	}
	digests, err := projectstate.PlanBindingDigests(&proj, []string{run008Contract})
	if err != nil {
		t.Fatal(err)
	}
	if len(digests) == 0 || len(got.Baseline) != len(digests) || got.Baseline[run008Contract+":"+scenarioID] != digests[run008Contract+":"+scenarioID] {
		t.Errorf("the baseline must digest every binding: %v vs %v", got.Baseline, digests)
	}
	assertOnlyTheSystemWritesEvidence(t, s)
	assertDigestsCrossTheBoundary(t, proj, digests)
}

// assertOnlyTheSystemWritesEvidence: both agent-facing verbs reach the routing that stores a report
// and a run, so both must refuse them, or an agent could forge the attempt or the baseline.
func assertOnlyTheSystemWritesEvidence(t *testing.T, s *Session) {
	t.Helper()
	for field, want := range map[string]string{"PlanDefect": "reportPlanDefect", "CheckRun": "record-check-run"} {
		forged := map[string]any{field: map[string]any{"id": "forged"}}
		if err := s.recordTestingState(forged); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("recordTestingState must refuse %s, got %v", field, err)
		}
		if err := s.recordPhaseArtifact("k", forged); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("recordPhaseArtifact must refuse %s, got %v", field, err)
		}
	}
}

// assertDigestsCrossTheBoundary: a workflow reads the plan's digests through the envelope, which
// does not carry the plans, and they must read as what the verb took as the baseline.
func assertDigestsCrossTheBoundary(t *testing.T, proj projectstate.Project, digests map[string]string) {
	t.Helper()
	env, err := projectstate.EncodeProject(proj)
	if err != nil {
		t.Fatal(err)
	}
	through, err := env.Decode()
	if err != nil {
		t.Fatal(err)
	}
	same, err := projectstate.PlanBindingDigests(&through, []string{run008Contract})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range digests {
		if same[k] != v {
			t.Errorf("the digest of %s changed across the workflow boundary", k)
		}
	}
}
