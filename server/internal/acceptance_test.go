package internal_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The acceptance residual (FPC plan 4, Amendment C) over the platform's todomvc conformance
// fixture. The use cases two actors originate (work-the-list, resume-the-list) leave outcomes no
// single client projection asserts, and reword-a-todo has the fixture's one cross-client path; the
// paths one actor walks through one client are covered by their client's plan and are not residual.

func todomvcProject(t *testing.T) *projectstate.Project {
	t.Helper()
	raw, err := conformance.Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil || !ok {
		t.Fatalf("decode the todomvc fixture: ok=%v err=%v", ok, err)
	}
	return &p
}

// checkResidualUseCase holds one residual entry to the use cases the fixture's residual is made of.
func checkResidualUseCase(t *testing.T, r scenario.Residual) {
	t.Helper()
	uc, _, _ := strings.Cut(r.Scenario, "-P")
	switch uc {
	case "work-the-list", "resume-the-list": // two actors, so a later outcome
	case "reword-a-todo":
		// the member rewords in the browser, then through MCP
		if _, ok := r.Reason.(scenario.CrossClient); !ok {
			t.Errorf("residual %s is residual as %#v, want a CrossClient path", r.Scenario, r.Reason)
		}
	default:
		t.Errorf("residual %s: a path one actor walks through one client is its client plan's", r.Scenario)
	}
}

func TestAcceptanceScenarios_TodomvcResidual(t *testing.T) {
	p := todomvcProject(t)
	residual, err := projectstate.AcceptanceResidual(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(residual) == 0 {
		t.Fatal("the todomvc fixture has a residual: two actors reach the list through three clients")
	}
	for _, r := range residual {
		checkResidualUseCase(t, r)
	}
	if !scenario.IsResidual(residual, "work-the-list-P1@todoApi") {
		t.Errorf("work-the-list-P1@todoApi must be residual: %+v", residual)
	}
	for _, covered := range []string{"come-back-to-an-intact-list-P1@todoWebApp", "drive-the-list-from-agent-tooling-P1@todoMcpServer", "mark-a-todo-completed-or-active-P1@todoWebApp"} {
		if scenario.IsResidual(residual, covered) {
			t.Errorf("%s is covered by one client projection and is not residual", covered)
		}
	}
	// AcceptanceScenarios is exactly the residual variants, in variant order.
	scenarios, err := projectstate.AcceptanceScenarios(p)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range scenarios {
		ids = append(ids, s.ID)
		if !scenario.IsResidual(residual, s.ID) {
			t.Errorf("AcceptanceScenarios returned %s, which is not residual", s.ID)
		}
	}
	for _, r := range residual {
		if !slices.Contains(ids, r.Scenario) {
			t.Errorf("residual %s is missing from AcceptanceScenarios %v", r.Scenario, ids)
		}
	}
	if !slices.IsSorted(ids) {
		t.Errorf("AcceptanceScenarios must be sorted by variant id: %v", ids)
	}
}

func TestAcceptanceRoute_NamesPathComponentsAndClientActivity(t *testing.T) {
	p := todomvcProject(t)
	r, err := projectstate.AcceptanceRoute(p, "work-the-list-P1@todoApi")
	if err != nil {
		t.Fatal(err)
	}
	if r.Scenario != "work-the-list-P1@todoApi" || r.ClientActivity != "C-todo-api" {
		t.Fatalf("route = %+v, want the todoApi client activity C-todo-api", r)
	}
	// Every component a call of the steps on the variant's path nodes names, in path order: n1a's
	// calls enter through the three clients (the surface is where the member's outcome is shown and
	// so where a failure of it is: the route names it though the stimulus enters through the REST
	// client), n2 the manager and its engine, n4 the access and its store and the ruling engine.
	// message-bus is called at n7, which this variant's path skips.
	want := []string{"todo-web-app", "todo-mcp-server", "todo-api", "list-manager", "access-granting-engine", "todo-list-access", "todo-list-store", "list-ruling-engine"}
	if !slices.Equal(r.Components, want) {
		t.Errorf("components = %v, want %v (as the dynamic view's steps order them)", r.Components, want)
	}
	if _, err := projectstate.AcceptanceRoute(p, "no-such-variant"); err == nil {
		t.Error("a variant that is not an acceptance variant must be refused")
	}
}

// withATPActivity commits N-ATP to the own project's activity list, in memory.
func withATPActivity(t *testing.T) *projectstate.Project {
	t.Helper()
	p := ownProject(t)
	list, ok := p.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || list == nil {
		t.Fatal("the committed activity list did not decode")
	}
	list.Activities = append(list.Activities, projectstate.ActivityItem{
		Name: "N-ATP", Title: "Use-case acceptance test plan", EffortDays: 5, WorkerClass: "test-engineer", RiskBucket: 2,
	})
	return p
}

func TestATPActivityClassifiesAsTheAcceptancePlanLifecycle(t *testing.T) {
	typ, variant, err := projectstate.ClassifyActivity("N-ATP", "test-engineer", false, false, false)
	if err != nil || typ != projectstate.ActivityTypeTesting || variant != projectstate.TestVariantAcceptancePlan {
		t.Fatalf("N-ATP classifies as (%v, %v, %v), want testing/acceptancePlan", typ, variant, err)
	}
	if key := projectstate.LifecycleKeyFor(typ, variant); key != "testing:acceptancePlan" {
		t.Fatalf("lifecycle key = %q", key)
	}
	if _, ok := methodassets.ManifestFor("acceptance-test-plan"); !ok {
		t.Error("acceptance-test-plan has no step manifest")
	}
}

func TestTaskSpecForATPIsStateOnlyWithATPRules(t *testing.T) {
	s, err := projectstate.TaskSpecFor(withATPActivity(t), "N-ATP", "stp")
	if err != nil {
		t.Fatal(err)
	}
	if s.Identity.Command != "acceptance-test-plan" || s.Identity.WorkerClass != "test-engineer" {
		t.Fatalf("identity = %+v", s.Identity)
	}
	if !s.EditScope.StateOnly {
		t.Errorf("the acceptance plan is a record: its scope is state only, got %+v", s.EditScope)
	}
	if got := checkIDs(s); !slices.Equal(got, []string{"rule:atp"}) {
		t.Fatalf("checks = %v, want [rule:atp]", got)
	}
	for _, c := range s.Checks {
		if !slices.Equal(c.Source.RulePrefixes, []string{"ATP-", "TP-"}) {
			t.Errorf("rule prefixes = %v, want ATP- and TP-", c.Source.RulePrefixes)
		}
	}
	if !strings.Contains(s.Prompt.Text, "N-ATP") {
		t.Errorf("the prompt must be rendered with the activity id as its one argument, got %.200q", s.Prompt.Text)
	}
}

// ---- the acceptance gate's evidence (Task 4.4) ----

// acceptanceRun is an M-ACCEPT integration run at revision, with a verdict for every residual variant
// of p: pass, except that failing names one variant (and "" none).
func acceptanceRun(t *testing.T, p *projectstate.Project, id, revision, failing string, year int) projectstate.CheckRun {
	t.Helper()
	scenarios, err := projectstate.AcceptanceScenarios(p)
	if err != nil || len(scenarios) == 0 {
		t.Fatalf("no residual variant to run: %d, %v", len(scenarios), err)
	}
	run := projectstate.CheckRun{ID: id, Activity: mAcceptActivity, Task: "integration", Revision: revision,
		EndedAt: endedAt(year), Artifact: "acceptance-" + id,
		Results: []projectstate.CheckResult{{CheckId: "rule:atp", Kind: projectstate.CheckKindRule, Status: "pass"}}}
	for _, sc := range scenarios {
		status := "pass"
		if sc.ID == failing {
			status = "fail"
		}
		run.Results = append(run.Results, projectstate.CheckResult{
			CheckId: "scenario:" + projectstate.AcceptanceContract + ":" + sc.ID, Kind: projectstate.CheckKindScenario, Status: status, DurationMs: 9})
	}
	return run
}

func acceptanceView(t *testing.T, p projectstate.Project) *delivery.ProjectView {
	t.Helper()
	pid, activity := "todomvc", mAcceptActivity
	v, err := viewManager(p, nil).QueryProjectView(viewCtx(), delivery.ProjectViewQuery{
		Kind: delivery.ProjectViewTestRun, ProjectID: &pid, ActivityID: &activity})
	if err != nil {
		t.Fatal(err)
	}
	return &v
}

// The testRun view of the milestone is the acceptance contract's join: the run the venue recorded at
// the composed revision, every residual variant with a verdict and none the run never named. The
// view carries the run's revision, which the gate holds to the attempt it judges, so a run of an
// earlier composition is never the one a later round passes on.
func TestAcceptanceGate_RequiresRunAtComposedRevision(t *testing.T) {
	p := withMAccept(t)
	first := acceptanceRun(t, p, "acc-1", mAcceptActivity+":integration:1", "", 2090)
	v := acceptanceView(t, withCheckRun(*p, first))
	if len(v.TestRuns) != 1 || v.TestRun == nil {
		t.Fatalf("the view must carry the acceptance contract's join alone: %+v", v.TestRuns)
	}
	tr := v.TestRuns[0]
	if tr.Run.Component != projectstate.AcceptanceContract || tr.Run.Revision != first.Revision || tr.Artifact != first.Artifact {
		t.Fatalf("run %+v, want %s viewed for the acceptance contract at %s", tr.Run, first.ID, first.Revision)
	}
	scenarios, _ := projectstate.AcceptanceScenarios(p)
	if len(tr.Missing) != 0 || len(tr.Unknown) != 0 || len(tr.Scenarios) != len(scenarios) {
		t.Errorf("a run naming every residual variant is complete: missing %v unknown %v scenarios %d (want %d)", tr.Missing, tr.Unknown, len(tr.Scenarios), len(scenarios))
	}
	for _, sc := range tr.Scenarios {
		if sc.Status != "pass" {
			t.Errorf("scenario %s status %q, want pass", sc.Id, sc.Status)
		}
	}

	// The composition changed: the milestone ran again at its next attempt. The run the view shows,
	// and the gate judges, is that later one, whose revision is no longer the first attempt's.
	second := acceptanceRun(t, p, "acc-2", mAcceptActivity+":integration:2", "", 2100)
	v = acceptanceView(t, withCheckRun(withCheckRun(*p, first), second))
	if got := v.TestRuns[0].Run.Revision; got != second.Revision || got == first.Revision {
		t.Fatalf("the latest run is at revision %q, want %q (not the earlier composition's %q)", got, second.Revision, first.Revision)
	}

	// A run that never named a variant is incomplete, not vacuously green.
	partial := acceptanceRun(t, p, "acc-3", mAcceptActivity+":integration:3", "", 2110)
	partial.Results = partial.Results[:len(partial.Results)-1]
	v = acceptanceView(t, withCheckRun(*p, partial))
	if len(v.TestRuns[0].Missing) != 1 {
		t.Errorf("a run missing one residual variant must name it: %+v", v.TestRuns[0].Missing)
	}
}

// A red variant is named with the components on its path and the activity to re-open. The view marks
// the scenario failed (what the gate's hold text carries); the route is the platform's, derived from
// the dynamic view's steps on the variant's path nodes and the client activity the first actor
// stimulus enters through.
func TestAcceptanceGate_HoldNamesPathComponents(t *testing.T) {
	p := withMAccept(t)
	const variant = "work-the-list-P1@todoApi"
	run := acceptanceRun(t, p, "acc-red", mAcceptActivity+":integration:1", variant, 2100)
	v := acceptanceView(t, withCheckRun(*p, run))
	var status string
	for _, sc := range v.TestRuns[0].Scenarios {
		if sc.Id == variant {
			status = sc.Status
		}
	}
	if status != "fail" {
		t.Fatalf("%s status %q in the view, want fail", variant, status)
	}

	route, err := projectstate.AcceptanceRoute(p, variant)
	if err != nil {
		t.Fatal(err)
	}
	if route.ClientActivity != "C-todo-api" || !slices.Contains(route.Components, "todo-api") || !slices.Contains(route.Components, "list-manager") {
		t.Fatalf("route = %+v", route)
	}
	line := projectstate.AcceptanceRouteLine(route)
	for _, want := range []string{
		variant + ": components on path: " + strings.Join(route.Components, ", "),
		"OverrideActivity(C-todo-api, retry)",
		"a new attempt on its integration task from a fresh branch off main",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the hold line must carry %q:\n%s", want, line)
		}
	}
	noClient := projectstate.AcceptanceRouteLine(projectstate.AcceptanceRouteInfo{Scenario: variant, Components: []string{"todo-api"}})
	if !strings.Contains(noClient, "no client activity derives for todoApi") || strings.Contains(noClient, "OverrideActivity") {
		t.Errorf("a variant whose client no activity builds must say so, not name an activity:\n%s", noClient)
	}
}
