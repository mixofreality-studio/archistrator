package main

// activityrecords_test.go — REC-BUILT-UNRECORDED / REC-TESTING-UNQUALIFIED
// (activityrecords.go): the activity execution ledger against the checkout and the
// recorded runs, their attribution, and their wiring into `validate`.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	recActivity  = "C-task-access"
	recComponent = "ra1"
	recContract  = "taskAccess"
	recPackage   = "internal/resourceaccess/task"
)

// recProject is the drift fixture's System with TaskAccess (ra1) carrying a contract in
// recPackage and a committed plan holding its coding activity, rows and runs empty.
func recProject() projectstate.Project {
	p := driftFixtureProject(false)
	sys := amendedSystemModel()
	key := recContract
	for i := range sys.Components {
		if sys.Components[i].ID == recComponent {
			sys.Components[i].ContractKey = &key
		}
	}
	p.SystemDesign.Model = sys
	list := coveringActivityList()
	list.Activities = append(list.Activities, projectstate.ActivityItem{
		Name: recActivity, Title: "Build TaskAccess", Coding: true, WorkerClass: "junior-developer",
		EffortDays: 5, RiskBucket: 2, ComponentID: recComponent,
	})
	p.ActivityList.Model = list
	p.ServiceContracts = map[string]projectstate.ServiceContract{
		recContract: {
			Component: recContract, Layer: "ResourceAccess", GoPackage: recPackage,
			Interface: projectstate.ContractInterface{Name: "TaskAccess"},
		},
	}
	return p
}

// recCheckout is a checkout whose Go module sits under server/ (archistrator's layout),
// holding the given files of recPackage.
func recCheckout(files ...string) checkout {
	fsys := fstest.MapFS{"server/go.mod": {Data: []byte("module example\n")}}
	for _, f := range files {
		fsys["server/"+recPackage+"/"+f] = &fstest.MapFile{Data: []byte("package task\n")}
	}
	return newCheckout(fsys)
}

// builtTaskAccess is the checkout where TaskAccess is built: its implementation file and
// a generated scenarios file.
func builtTaskAccess() checkout {
	return recCheckout("taskaccess.go", "contract.gen.go", "access_scenarios.gen_test.go")
}

// attempt is one ledger entry of recActivity.
func attempt(task projectstate.MethodTask, n int, outcome projectstate.TaskOutcome, ev projectstate.EvidenceRef) projectstate.TaskAttempt {
	return projectstate.TaskAttempt{
		AttemptID: projectstate.AttemptID(recActivity, task, n),
		Task:      task, Phase: projectstate.PhaseForTask(task), Attempt: n,
		Actor: projectstate.ActorAgent, Outcome: outcome, Evidence: ev,
	}
}

// throughIntegration is a service ledger whose every task before `testing` passed, with
// one integration attempt (recActivity:integration:1).
func throughIntegration() []projectstate.TaskAttempt {
	var out []projectstate.TaskAttempt
	for _, task := range []projectstate.MethodTask{
		projectstate.TaskSRS, projectstate.TaskSRSReview, projectstate.TaskDetailedDesign, projectstate.TaskDesignReview,
		projectstate.TaskSTP, projectstate.TaskSTPReview, projectstate.TaskConstruction, projectstate.TaskCodeReview,
		projectstate.TaskIntegration,
	} {
		out = append(out, attempt(task, 1, projectstate.OutcomePassed, projectstate.EvidenceRef{}))
	}
	return out
}

// citedPass is a testing pass citing run id.
func citedPass(n int, run string) projectstate.TaskAttempt {
	return attempt(projectstate.TaskTesting, n, projectstate.OutcomePassed, projectstate.EvidenceRef{Kind: projectstate.EvidenceTestRun, Ref: run})
}

// qualifyingRun is the run a testing pass of recActivity stands on: its activity, at the
// judged integration attempt, every projected scenario green.
func qualifyingRun() projectstate.TestRun {
	return projectstate.TestRun{
		ID: "run-1", Activity: recActivity, Component: recContract,
		Revision: projectstate.AttemptID(recActivity, projectstate.TaskIntegration, 1), Artifact: "test-results-run-1",
		Scenarios: []projectstate.ScenarioResult{{Scenario: "uc1.P1", Status: "pass"}, {Scenario: "uc1.P2", Status: "pass"}},
	}
}

// twoScenarios projects uc1.P1 and uc1.P2 onto recComponent and nothing elsewhere.
func twoScenarios(_ *projectstate.Project, component string) ([]scenario.Scenario, error) {
	if component != recComponent {
		return nil, nil
	}
	return []scenario.Scenario{{ID: "uc1.P1"}, {ID: "uc1.P2"}}, nil
}

// findingsOf runs the rules and returns the findings of rule.
func findingsOf(t *testing.T, p projectstate.Project, repo checkout, rule methodcheck.RuleID) []methodcheck.Finding {
	t.Helper()
	all, err := activityRecordFindings(p, repo, twoScenarios)
	if err != nil {
		t.Fatalf("activityRecordFindings: %v", err)
	}
	var out []methodcheck.Finding
	for _, f := range all {
		if f.RuleID == rule {
			out = append(out, f)
		}
	}
	return out
}

// onlyFinding asserts exactly one Error finding of rule on recActivity whose message
// contains every want, and returns it.
func onlyFinding(t *testing.T, p projectstate.Project, repo checkout, rule methodcheck.RuleID, want ...string) {
	t.Helper()
	got := findingsOf(t, p, repo, rule)
	if len(got) != 1 {
		t.Fatalf("%s findings = %d, want 1: %+v", rule, len(got), got)
	}
	f := got[0]
	if f.Severity != methodcheck.SeverityError {
		t.Errorf("%s severity = %v, want Error", rule, f.Severity)
	}
	if f.Location == nil || f.Location.Section != "activityExecution."+recActivity {
		t.Errorf("%s location = %+v, want section activityExecution.%s", rule, f.Location, recActivity)
	}
	for _, w := range want {
		if !strings.Contains(f.Message, w) {
			t.Errorf("%s message lacks %q:\n%s", rule, w, f.Message)
		}
	}
}

// noFinding asserts rule is silent.
func noFinding(t *testing.T, p projectstate.Project, repo checkout, rule methodcheck.RuleID) {
	t.Helper()
	if got := findingsOf(t, p, repo, rule); len(got) != 0 {
		t.Fatalf("%s must be silent, got %+v", rule, got)
	}
}

// ---- REC-BUILT-UNRECORDED -------------------------------------------------------------

func TestRECBuiltUnrecorded_BuiltComponentWithNoRow(t *testing.T) {
	onlyFinding(t, recProject(), builtTaskAccess(), ruleBuiltUnrecorded,
		recActivity, `"ra1"`, "server/"+recPackage+"/taskaccess.go", "server/"+recPackage+"/access_scenarios.gen_test.go",
		"has no .activityExecution row")
}

func TestRECBuiltUnrecorded_RowWithoutAPassedCodeReview(t *testing.T) {
	p := recProject()
	ledger := throughIntegration()[:7] // through construction; codeReview never ran
	p.ActivityExecution = map[string]projectstate.ActivityExecution{recActivity: {ActivityID: recActivity, Attempts: ledger}}
	onlyFinding(t, p, builtTaskAccess(), ruleBuiltUnrecorded, "records neither a binary exit")

	// A codeReview that was REJECTED is no completion either: the gate's latest attempt decides.
	ledger = append(ledger, attempt(projectstate.TaskCodeReview, 1, projectstate.OutcomeRejected, projectstate.EvidenceRef{}))
	p.ActivityExecution[recActivity] = projectstate.ActivityExecution{ActivityID: recActivity, Attempts: ledger}
	onlyFinding(t, p, builtTaskAccess(), ruleBuiltUnrecorded, "records neither a binary exit")
}

func TestRECBuiltUnrecorded_ACompletionRecordSatisfiesIt(t *testing.T) {
	p := recProject()
	p.ActivityExecution = map[string]projectstate.ActivityExecution{recActivity: {ActivityID: recActivity, Attempts: throughIntegration()[:8]}}
	noFinding(t, p, builtTaskAccess(), ruleBuiltUnrecorded)

	done := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	p.ActivityExecution[recActivity] = projectstate.ActivityExecution{ActivityID: recActivity, CompletedAt: &done}
	noFinding(t, p, builtTaskAccess(), ruleBuiltUnrecorded)
}

func TestRECBuiltUnrecorded_BuiltNeedsBothFiles(t *testing.T) {
	p := recProject()
	noFinding(t, p, recCheckout("taskaccess.go", "contract.gen.go"), ruleBuiltUnrecorded)                // no generated scenarios
	noFinding(t, p, recCheckout("contract.gen.go", "access_scenarios.gen_test.go"), ruleBuiltUnrecorded) // no implementation
	noFinding(t, p, recCheckout(), ruleBuiltUnrecorded)
}

func TestRECBuiltUnrecorded_ModuleAtTheRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"go.mod":                      {Data: []byte("module example\n")},
		recPackage + "/taskaccess.go": {Data: []byte("package task\n")},
		recPackage + "/access_scenarios.gen_test.go":   {Data: []byte("package task_test\n")},
		"server/" + recPackage + "/unrelated_file.txt": {Data: []byte("x")},
	}
	onlyFinding(t, recProject(), newCheckout(fsys), ruleBuiltUnrecorded, recPackage+"/taskaccess.go")
}

func TestRECBuiltUnrecorded_OutOfReach(t *testing.T) {
	// No contract key on the component, an uncommitted plan, and a contract with no goPackage
	// each leave nothing to read.
	p := recProject()
	p.SystemDesign.Model = amendedSystemModel()
	noFinding(t, p, builtTaskAccess(), ruleBuiltUnrecorded)

	p = recProject()
	p.ActivityList.Status = projectstate.ReviewAwaitingReview
	noFinding(t, p, builtTaskAccess(), ruleBuiltUnrecorded)

	p = recProject()
	sc := p.ServiceContracts[recContract]
	sc.GoPackage = ""
	p.ServiceContracts[recContract] = sc
	noFinding(t, p, builtTaskAccess(), ruleBuiltUnrecorded)
}

// ---- REC-TESTING-UNQUALIFIED ----------------------------------------------------------

// testedProject is recProject with recActivity's row holding ledger and the given runs.
func testedProject(ledger []projectstate.TaskAttempt, runs ...projectstate.TestRun) projectstate.Project {
	p := recProject()
	p.ActivityExecution = map[string]projectstate.ActivityExecution{recActivity: {ActivityID: recActivity, Attempts: ledger}}
	if len(runs) > 0 {
		p.TestingState = &projectstate.TestingState{TestRuns: runs}
	}
	return p
}

func TestRECTestingUnqualified_AQualifyingRunSatisfiesIt(t *testing.T) {
	p := testedProject(append(throughIntegration(), citedPass(1, "run-1")), qualifyingRun())
	noFinding(t, p, builtTaskAccess(), ruleTestingUnqualified)
	noFinding(t, p, builtTaskAccess(), ruleBuiltUnrecorded)
}

func TestRECTestingUnqualified_UncitedPass(t *testing.T) {
	uncited := attempt(projectstate.TaskTesting, 1, projectstate.OutcomePassed, projectstate.EvidenceRef{Kind: projectstate.EvidenceGit, Ref: "abc123"})
	p := testedProject(append(throughIntegration(), uncited), qualifyingRun())
	onlyFinding(t, p, recCheckout(), ruleTestingUnqualified, recActivity+":testing:1", "cites no TestRun", `"git"`)
}

func TestRECTestingUnqualified_CitedRunNotRecorded(t *testing.T) {
	p := testedProject(append(throughIntegration(), citedPass(1, "run-gone")))
	onlyFinding(t, p, recCheckout(), ruleTestingUnqualified, `"run-gone"`, ".testingState.testRuns does not hold")
}

func TestRECTestingUnqualified_RunOfAnotherActivity(t *testing.T) {
	run := qualifyingRun()
	run.Activity = "C-other"
	p := testedProject(append(throughIntegration(), citedPass(1, "run-1")), run)
	onlyFinding(t, p, recCheckout(), ruleTestingUnqualified, `belongs to activity "C-other"`)
}

func TestRECTestingUnqualified_RunAtAnotherRevision(t *testing.T) {
	// A second integration attempt was judged by the passing round; the run is the first's.
	ledger := append(throughIntegration(),
		attempt(projectstate.TaskTesting, 1, projectstate.OutcomeRejected, projectstate.EvidenceRef{}),
		attempt(projectstate.TaskIntegration, 2, projectstate.OutcomePassed, projectstate.EvidenceRef{}),
		citedPass(2, "run-1"))
	p := testedProject(ledger, qualifyingRun())
	onlyFinding(t, p, recCheckout(), ruleTestingUnqualified,
		`revision "`+recActivity+`:integration:1"`, `judged integration attempt "`+recActivity+`:integration:2"`)
}

func TestRECTestingUnqualified_ScenarioSetDiffers(t *testing.T) {
	run := qualifyingRun()
	run.Scenarios = []projectstate.ScenarioResult{{Scenario: "uc1.P1", Status: "pass"}, {Scenario: "uc9.P1", Status: "pass"}}
	p := testedProject(append(throughIntegration(), citedPass(1, "run-1")), run)
	onlyFinding(t, p, recCheckout(), ruleTestingUnqualified, "never ran projected scenario(s) uc1.P2", "no projection holds: uc9.P1")
}

func TestRECTestingUnqualified_RunNotGreen(t *testing.T) {
	run := qualifyingRun()
	run.Scenarios = []projectstate.ScenarioResult{{Scenario: "uc1.P1", Status: "fail"}, {Scenario: "uc1.P2", Status: "skip"}}
	p := testedProject(append(throughIntegration(), citedPass(1, "run-1")), run)
	onlyFinding(t, p, recCheckout(), ruleTestingUnqualified, "did not pass: uc1.P1=fail, uc1.P2=skip")
}

func TestRECTestingUnqualified_ProjectionRefuses(t *testing.T) {
	p := testedProject(append(throughIntegration(), citedPass(1, "run-1")), qualifyingRun())
	all, err := activityRecordFindings(p, recCheckout(), func(*projectstate.Project, string) ([]scenario.Scenario, error) {
		return nil, errors.New("derive: boom")
	})
	if err != nil {
		t.Fatalf("activityRecordFindings: %v", err)
	}
	if len(all) != 1 || all[0].RuleID != ruleTestingUnqualified || !strings.Contains(all[0].Message, "cannot be projected: derive: boom") {
		t.Fatalf("a projection failure must read as an unqualified run, got %+v", all)
	}
}

func TestRECTestingUnqualified_OnlyTheLatestPassedAttemptIsJudged(t *testing.T) {
	// The latest testing attempt was rejected: the task is not passed, so nothing to judge.
	ledger := append(throughIntegration(),
		attempt(projectstate.TaskTesting, 1, projectstate.OutcomePassed, projectstate.EvidenceRef{}),
		attempt(projectstate.TaskTesting, 2, projectstate.OutcomeRejected, projectstate.EvidenceRef{}))
	noFinding(t, testedProject(ledger), recCheckout(), ruleTestingUnqualified)
}

func TestRECTestingUnqualified_LifecycleWithoutATestPlan(t *testing.T) {
	// A deployment activity's testing review judges no scenario run (T4 applies only to a
	// lifecycle that binds a test plan): its uncited pass stands.
	p := recProject()
	p.ActivityExecution = map[string]projectstate.ActivityExecution{
		"provision-task-db": {ActivityID: "provision-task-db", Attempts: []projectstate.TaskAttempt{{
			AttemptID: "provision-task-db:testing:1", Task: projectstate.TaskTesting,
			Phase: projectstate.PhaseForTask(projectstate.TaskTesting), Attempt: 1, Outcome: projectstate.OutcomePassed,
		}}},
	}
	noFinding(t, p, recCheckout(), ruleTestingUnqualified)
}

// ---- attribution and the validate seam --------------------------------------------------

func TestAttributeRule_ActivityRecordRules(t *testing.T) {
	for _, rule := range []methodcheck.RuleID{ruleBuiltUnrecorded, ruleTestingUnqualified} {
		if _, class := projectstate.AttributeRule(string(rule)); class != projectstate.AttribActivityExecution {
			t.Errorf("AttributeRule(%s) class = %v, want AttribActivityExecution", rule, class)
		}
	}
}

// seedRecRoot writes p as the checkout's state and TaskAccess as built in its server/
// module.
func seedRecRoot(t *testing.T, p projectstate.Project) string {
	t.Helper()
	root := seedValidateRoot(t, p)
	dir := filepath.Join(root, "server", filepath.FromSlash(recPackage))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		filepath.Join(root, "server", "go.mod"):            "module example\n",
		filepath.Join(dir, "taskaccess.go"):                "package task\n",
		filepath.Join(dir, "access_scenarios.gen_test.go"): "package task_test\n",
	} {
		if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestValidate_ActivityRecordRulesGateTheWholeDocument(t *testing.T) {
	// Recorded: the gate passes.
	p := recProject()
	p.ActivityExecution = map[string]projectstate.ActivityExecution{recActivity: {ActivityID: recActivity, Attempts: throughIntegration()}}
	var out bytes.Buffer
	if err := runValidate([]string{"--root", seedRecRoot(t, p)}, &out); err != nil {
		t.Fatalf("a built component with its completion record must pass, got: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "REC-") {
		t.Fatalf("no REC-* finding expected, got:\n%s", out.String())
	}

	// Unrecorded: the whole-document gate fails on it.
	root := seedRecRoot(t, recProject())
	out.Reset()
	err := runValidate([]string{"--root", root}, &out)
	if err == nil || !strings.Contains(out.String(), "ERROR    [REC-BUILT-UNRECORDED]") {
		t.Fatalf("an unrecorded built component must fail the whole-document gate, got err=%v\n%s", err, out.String())
	}

	// A design session cannot write an activity row: for it the finding is other-owner
	// data and downgrades (the slot-scoped policy), naming the owner.
	out.Reset()
	if err := runValidate([]string{"--root", root, "--slot", "System"}, &out); err != nil {
		t.Fatalf("a design session must not be blocked by an activity record, got: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "WARNING  [REC-BUILT-UNRECORDED]") || !strings.Contains(out.String(), "activityExecution slot") {
		t.Fatalf("the REC finding must downgrade with its owner named, got:\n%s", out.String())
	}
}
