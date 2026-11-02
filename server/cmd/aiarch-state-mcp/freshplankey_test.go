package main

// freshplankey_test.go is the regression for the wall RUN-008 hit: a fresh project's component
// id is kebab-case and its contract key camelCase, and a plan filed under the id made testgen
// write verdicts to test-results/<id> while run_checks read test-results/<contract>, so every
// scenario reported "no verdict recorded". It uses the plan and the contract an agent really
// recorded in that run (testdata/fresh/run008.*), recorded through the verb under the kebab id.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	run008Activity  = "C-change-notice-access"
	run008Component = "change-notice-access"
	run008Contract  = "changeNoticeAccess"
	run008Package   = "internal/resourceaccess/changenotice"
)

// changeNoticeImpl implements what the contract promises without the Postgres channel the real
// component uses, so the scenarios run on any machine: a repeated change id is a no-op, and an
// empty argument is the contract's terminal error.
const changeNoticeImpl = `// Package changenotice makes it known that a named List has changed.
package changenotice

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

type postgresAccess struct {
	seen map[ChangeID]bool
}

func newPostgresChangeNoticeAccess(_ context.Context, _ *pgxpool.Pool) (ChangeNoticeAccess, error) {
	return &postgresAccess{seen: map[ChangeID]bool{}}, nil
}

func (a *postgresAccess) AnnounceListChanged(_ fwra.Context, owner ListOwner, change ChangeID) error {
	if owner == "" || change == "" {
		return fwra.New(fwra.ContractMisuse, "listOwner and changeID must be non-empty")
	}
	a.seen[change] = true
	return nil
}
`

const changeNoticeHooks = `package changenotice_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"

	"MODULE/internal/resourceaccess/changenotice"
)

func newSubject(t *testing.T, h *scenariohost.Host) changenotice.ChangeNoticeAccess {
	t.Helper()
	subject, err := changenotice.NewPostgresChangeNoticeAccess(h.Context(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}
`

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
// integration's real scenario checks then find every verdict, green.
func TestFreshPlanRecordedUnderItsIDRunsGreen(t *testing.T) {
	repo := bornRepo(t, run008State(t))
	s := &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: run008Activity, TaskID: "stp", ComponentID: run008Component, git: runGit}
	if err := s.recordPhaseArtifact(run008Component, map[string]any{"testPlan": run008Plan(t, run008Component)}); err != nil {
		t.Fatal(err)
	}
	plans := recordedPlans(t, repo)
	if p, ok := plans[run008Contract]; !ok || p.Component != run008Contract || len(plans) != 1 {
		t.Fatalf("the plan must be filed under the contract key, got %+v", plans)
	}

	inRepo(t, repo, "make", "gen-models", "CONTRACTS="+run008Contract)
	writeRepoFile(t, repo, run008Package+"/changenoticeaccess.go", []byte(changeNoticeImpl))
	inRepo(t, repo, "make", "gen-tests")
	mod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	module := regexp.MustCompile(`(?m)^module (\S+)$`).FindSubmatch(mod)
	if module == nil {
		t.Fatalf("no module line:\n%s", mod)
	}
	writeRepoFile(t, repo, run008Package+"/access_hooks_test.go", []byte(strings.ReplaceAll(changeNoticeHooks, "MODULE", string(module[1]))))
	inRepo(t, repo, "go", "mod", "tidy")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "build and bind the component")

	spec := activitySpec(t, repo, run008Activity, "integration")
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
	if _, err := os.Stat(filepath.Join(repo, "test-results", run008Contract, "results.json")); err != nil {
		t.Errorf("the verdicts must land under the contract key: %v", err)
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
	if err := s.recordTestingState(map[string]any{"PlanDefect": map[string]any{"id": "forged"}}); err == nil || !strings.Contains(err.Error(), "reportPlanDefect") {
		t.Errorf("the verb must be the only writer, got %v", err)
	}
	assertDigestsCrossTheBoundary(t, proj, digests)
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
