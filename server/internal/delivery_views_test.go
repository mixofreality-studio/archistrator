package internal_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	artifactfake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	episodefake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	projectstatefake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate/fake"
)

// The delivery Manager's `taskSpec` and `testRun` project views over archistrator's own
// committed project.json, read through the public QueryProjectView with the project and
// artifact reads faked (agent task spec, Task B5). These tests live here, not beside the
// Manager, for the reason taskspec_test.go gives: a component package holds only its
// generated scenarios file and its hooks file (TestFileLayout).

const (
	viewActivity = "C-agentic-job-access"
	viewContract = "agenticJobAccess"
)

// viewManager is a delivery Manager whose project read answers proj and whose artifact
// index answers files; every other collaborator is absent (the views read neither).
func viewManager(proj projectstate.Project, files []string) delivery.DeliveryManager {
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(fwra.Context, projectstate.ProjectID) (projectstate.Project, error) { return proj, nil },
	}
	art := &artifactfake.FakeArtifactAccess{
		GetTestRunIndexFn: func(fwra.Context, artifact.TestRunRef) (artifact.TestRunIndex, error) {
			return artifact.TestRunIndex{Files: files}, nil
		},
	}
	// The episode ledger is empty: the activity view (which a taskSpec query with a revision
	// reads its attempt ids from) folds episodes into revisions, and none are needed here.
	eps := &episodefake.FakeEpisodeAccess{
		ListEpisodesFn: func(fwra.Context, episode.EpisodeQuery) ([]episode.EpisodeRecord, error) { return nil, nil },
	}
	return delivery.NewDeliveryManager(noSessions{}, ps, art, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, eps, 0, "", nil, "")
}

// noSessions is a Temporal client with no workflow running: the activity view asks a
// running row's live session and reads "not dispatched" (NotFound) as no session. Every
// other client call is absent (nil embedded interface) and would panic.
type noSessions struct{ client.Client }

func (noSessions) QueryWorkflow(context.Context, string, string, string, ...any) (converter.EncodedValue, error) {
	return nil, serviceerror.NewNotFound("workflow not found")
}

func viewCtx() fwmanager.Context { return fwmanager.Context{Context: context.Background()} }

func taskSpecQuery(activity, task string) delivery.ProjectViewQuery {
	pid := "archistrator"
	return delivery.ProjectViewQuery{Kind: delivery.ProjectViewTaskSpec, ProjectID: &pid, ActivityID: &activity, TaskID: &task}
}

// withCheckRun appends run to the project's recorded check runs.
func withCheckRun(p projectstate.Project, run projectstate.CheckRun) projectstate.Project {
	ts := projectstate.TestingState{}
	if p.TestingState != nil {
		ts = *p.TestingState
	}
	ts.CheckRuns = append(slices.Clone(ts.CheckRuns), run)
	p.TestingState = &ts
	return p
}

func endedAt(year int) *time.Time {
	t := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestTaskSpecViewOfANeverRunTaskHasNoCheckRun(t *testing.T) {
	proj := *ownProject(t)
	for _, task := range []string{"construction", "codeReview"} {
		want, err := projectstate.TaskSpecFor(&proj, viewActivity, task)
		if err != nil {
			t.Fatal(err)
		}
		v, err := viewManager(proj, nil).QueryProjectView(viewCtx(), taskSpecQuery(viewActivity, task))
		if err != nil {
			t.Fatalf("%s: %v", task, err)
		}
		if v.Kind != delivery.ProjectViewTaskSpec || v.TaskSpec == nil {
			t.Fatalf("%s: view %+v carries no task spec", task, v)
		}
		assertSpecView(t, task, v.TaskSpec, want)
		if v.TaskSpec.CheckRun != nil || v.TaskSpec.SpecChanged {
			t.Fatalf("%s: a never-run task shows run %+v (specChanged %v)", task, v.TaskSpec.CheckRun, v.TaskSpec.SpecChanged)
		}
	}
}

// assertSpecView holds the wire view to the resolved spec: same task, same hash, the same
// identity, model and checks, and no nil list on the wire.
func assertSpecView(t *testing.T, task string, s *delivery.DeliveryTaskSpecView, want projectstate.TaskSpec) {
	t.Helper()
	if s.Hash != want.Hash || len(s.Hash) != 64 || s.Activity != viewActivity || s.Task != task {
		t.Fatalf("%s: spec %s/%s hash %q, want %q", task, s.Activity, s.Task, s.Hash, want.Hash)
	}
	if len(s.Checks) != len(want.Checks) || s.Identity.Command != want.Identity.Command || s.Model.ID != want.Model.ID {
		t.Fatalf("%s: view %+v does not carry the resolved spec %+v", task, s, want)
	}
	if s.Tools.Mcp == nil || s.Tools.Builtin == nil || s.Prompt.Skills == nil || s.EditScope.Extra == nil {
		t.Fatalf("%s: a nil list reaches the wire: %+v", task, s)
	}
}

func TestTaskSpecViewFlagsARunRecordedAgainstAnotherSpec(t *testing.T) {
	proj := *ownProject(t)
	spec, err := projectstate.TaskSpecFor(&proj, viewActivity, "integration")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, hash  string
		wantChanged bool
	}{
		{"same spec", spec.Hash, false},
		{"another spec", "0000000000000000000000000000000000000000000000000000000000000000", true},
	} {
		run := projectstate.CheckRun{ID: "b5-" + tc.name, Activity: viewActivity, Task: "integration",
			Revision: viewActivity + ":integration:9", SpecHash: tc.hash, EndedAt: endedAt(2100), Artifact: "a",
			Results: []projectstate.CheckResult{{CheckId: "rule:project", Kind: projectstate.CheckKindRule, Status: "pass"}}}
		v, err := viewManager(withCheckRun(proj, run), nil).QueryProjectView(viewCtx(), taskSpecQuery(viewActivity, "integration"))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := v.TaskSpec
		if got.CheckRun == nil || got.CheckRun.ID != run.ID {
			t.Fatalf("%s: the latest run is not the one shown: %+v", tc.name, got.CheckRun)
		}
		if got.SpecChanged != tc.wantChanged {
			t.Fatalf("%s: specChanged %v, want %v", tc.name, got.SpecChanged, tc.wantChanged)
		}
		if len(got.CheckRun.Results) != 1 || got.CheckRun.Results[0].CheckId != "rule:project" {
			t.Fatalf("%s: run results %+v", tc.name, got.CheckRun.Results)
		}
	}
}

func TestTaskSpecViewNeedsItsSelectors(t *testing.T) {
	proj := *ownProject(t)
	q := taskSpecQuery(viewActivity, "")
	if _, err := viewManager(proj, nil).QueryProjectView(viewCtx(), q); err == nil {
		t.Fatal("a taskSpec query with no taskId answered")
	}
	q = taskSpecQuery(viewActivity, "noSuchTask")
	if _, err := viewManager(proj, nil).QueryProjectView(viewCtx(), q); err == nil {
		t.Fatal("a taskSpec query naming no task answered")
	}
}

// The testRun view reads the integration CheckRun's `scenario:<contract>:*` results and
// joins them with the contract's projection: what the testing gate holds on (missing,
// unknown, a red status) is exactly what the reviewer sees.
func TestTestRunViewJoinsTheIntegrationCheckRun(t *testing.T) {
	proj := *ownProject(t)
	projected, err := projectstate.ComponentScenarios(&proj, viewContract)
	if err != nil || len(projected) < 2 {
		t.Fatalf("projection of %s: %d scenarios, %v", viewContract, len(projected), err)
	}
	dropped, timedOut := projected[0].ID, projected[1].ID
	log := "check-results/checks/one.log"
	run := projectstate.CheckRun{ID: "b5-integration", Activity: viewActivity, Task: "integration",
		Revision: viewActivity + ":integration:3", EndedAt: endedAt(2100), Artifact: "artifact-b5",
		Results: integrationResults(projected, log)}
	pid, activity, contract := "archistrator", viewActivity, viewContract
	v, err := viewManager(withCheckRun(proj, run), []string{log}).QueryProjectView(viewCtx(), delivery.ProjectViewQuery{
		Kind: delivery.ProjectViewTestRun, ProjectID: &pid, ActivityID: &activity, ComponentID: &contract})
	if err != nil {
		t.Fatal(err)
	}
	tr := v.TestRun
	assertRunHeader(t, tr, run)
	if !slices.Equal(tr.Missing, []string{dropped}) {
		t.Fatalf("missing %v, want [%s] (another contract's verdict must not count)", tr.Missing, dropped)
	}
	if !slices.Equal(tr.Unknown, []string{"not-a-projected-scenario"}) {
		t.Fatalf("unknown %v", tr.Unknown)
	}
	if len(tr.Scenarios) != len(projected) || len(tr.Run.Scenarios) != len(projected) {
		t.Fatalf("scenarios %d and run results %d, want %d each", len(tr.Scenarios), len(tr.Run.Scenarios), len(projected))
	}
	byID := map[string]delivery.TestRunScenarioView{}
	for _, sc := range tr.Scenarios {
		byID[sc.Id] = sc
	}
	if got := byID[dropped].Status; got != "missing" {
		t.Fatalf("dropped scenario status %q", got)
	}
	if got := byID[timedOut]; got.Status != "timeout" || got.DurationMs != 7 || got.OutputRef != log {
		t.Fatalf("scenario %s: %+v", timedOut, got)
	}
}

// assertRunHeader holds the view's run to the CheckRun it was read from, viewed for
// viewContract.
func assertRunHeader(t *testing.T, tr *delivery.DeliveryTestRunView, run projectstate.CheckRun) {
	t.Helper()
	if tr == nil || tr.Run.Id != run.ID || tr.Run.Component != viewContract || tr.Run.Revision != run.Revision || tr.Artifact != run.Artifact {
		t.Fatalf("run %+v, want %s viewed for %s", tr, run.ID, viewContract)
	}
}

// integrationResults is an integration run over projected: a build check, a verdict for
// projected[0] under ANOTHER contract only (so it is missing here), one result naming no
// projected scenario, and a verdict for every other projected scenario — the first of
// them a timeout — each with log as its output.
func integrationResults(projected []scenario.Scenario, log string) []projectstate.CheckResult {
	prefix := projectstate.CheckKindScenario + ":" + viewContract + ":"
	results := []projectstate.CheckResult{
		{CheckId: "command:build", Kind: projectstate.CheckKindCommand, Status: "pass"},
		{CheckId: projectstate.CheckKindScenario + ":otherContract:" + projected[0].ID, Kind: projectstate.CheckKindScenario, Status: "pass"},
		{CheckId: prefix + "not-a-projected-scenario", Kind: projectstate.CheckKindScenario, Status: "pass"},
	}
	for i, sc := range projected[1:] {
		status := "pass"
		if i == 0 {
			status = "timeout"
		}
		results = append(results, projectstate.CheckResult{CheckId: prefix + sc.ID, Kind: projectstate.CheckKindScenario,
			Status: status, DurationMs: 7, OutputRef: &log})
	}
	return results
}

// With no componentId the view shows the first contract the run carries verdicts for, and
// a legacy run (revision a commit sha, no spec hash) is still the latest run shown.
func TestTestRunViewDefaultsToTheRunsContract(t *testing.T) {
	proj := *ownProject(t)
	pid, activity := "archistrator", viewActivity
	v, err := viewManager(proj, nil).QueryProjectView(viewCtx(), delivery.ProjectViewQuery{
		Kind: delivery.ProjectViewTestRun, ProjectID: &pid, ActivityID: &activity})
	if err != nil {
		t.Fatal(err)
	}
	if v.TestRun == nil || v.TestRun.Run.Component != viewContract || len(v.TestRun.Run.Scenarios) == 0 {
		t.Fatalf("legacy run view %+v", v.TestRun)
	}
}

// withRevision is q naming task revision n.
func withRevision(q delivery.ProjectViewQuery, n int) delivery.ProjectViewQuery {
	q.Revision = &n
	return q
}

// R15: a taskSpec query naming revision N shows the run recorded under one of that
// revision's attempt ids even when a later run exists. R35: a revision none of whose
// attempt ids recorded a run shows NO run — never another revision's.
func TestTaskSpecViewSelectsTheRunOfTheNamedRevision(t *testing.T) {
	// No recorded run but the two below (the own project's runs include one at revision 1).
	proj := *ownProject(t)
	proj.TestingState = &projectstate.TestingState{}
	// The own project's integration revision 1 is attempt C-agentic-job-access:integration:1.
	atRev1 := projectstate.CheckRun{ID: "b5-rev1", Activity: viewActivity, Task: "integration",
		Revision: viewActivity + ":integration:1", EndedAt: endedAt(2090), Artifact: "a"}
	later := projectstate.CheckRun{ID: "b5-later", Activity: viewActivity, Task: "integration",
		Revision: viewActivity + ":integration:7", EndedAt: endedAt(2100), Artifact: "a"}
	q := taskSpecQuery(viewActivity, "integration")

	v, err := viewManager(withCheckRun(withCheckRun(proj, atRev1), later), nil).QueryProjectView(viewCtx(), withRevision(q, 1))
	if err != nil {
		t.Fatal(err)
	}
	if v.TaskSpec.CheckRun == nil || v.TaskSpec.CheckRun.ID != atRev1.ID {
		t.Fatalf("revision 1 shows run %+v, want %s (its own attempt's run, not the later one)", v.TaskSpec.CheckRun, atRev1.ID)
	}
	v, err = viewManager(withCheckRun(withCheckRun(proj, atRev1), later), nil).QueryProjectView(viewCtx(), q)
	if err != nil {
		t.Fatal(err)
	}
	if v.TaskSpec.CheckRun == nil || v.TaskSpec.CheckRun.ID != later.ID {
		t.Fatalf("no revision shows run %+v, want the latest %s", v.TaskSpec.CheckRun, later.ID)
	}
	v, err = viewManager(withCheckRun(proj, later), nil).QueryProjectView(viewCtx(), withRevision(q, 1))
	if err != nil {
		t.Fatal(err)
	}
	if v.TaskSpec.CheckRun != nil {
		t.Fatalf("revision 1, which recorded no run of its own, shows run %s (R35: none)", v.TaskSpec.CheckRun.ID)
	}
	if _, err := viewManager(proj, nil).QueryProjectView(viewCtx(), withRevision(q, 99)); err == nil {
		t.Fatal("a revision the task never had answered")
	}
}

// withoutCommittedPlan is p with no committed activity list (a fresh project, or one whose
// list is being redrafted in project design).
func withoutCommittedPlan(p projectstate.Project) projectstate.Project {
	p.ActivityList = projectstate.ArtifactSlot{}
	return p
}

// R33: the three reserved design-prefix activities resolve every task's spec with no
// committed activity list — over the project as given, and over its Temporal envelope,
// which is what the dispatch (dispatchSpecHash) resolves over — and the taskSpec view
// serves them; the estimate is zero (unestimated), not a guess.
func TestTaskSpecForDesignPrefixWithoutACommittedPlan(t *testing.T) {
	proj := withoutCommittedPlan(*ownProject(t))
	env, err := projectstate.EncodeProject(proj)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := env.Decode()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"requirements", "architecture", "projectDesign"} {
		tasks := designPrefixTasks(t, id)
		for _, task := range tasks {
			s, err := projectstate.TaskSpecFor(&proj, id, task)
			if err != nil {
				t.Fatalf("%s/%s with no committed plan: %v", id, task, err)
			}
			if s.Model.Estimate != (projectstate.SpecEstimate{}) {
				t.Fatalf("%s/%s estimate %+v, want unestimated (zero)", id, task, s.Model.Estimate)
			}
			viaEnvelope, err := projectstate.TaskSpecFor(&decoded, id, task)
			if err != nil || viaEnvelope.Hash != s.Hash {
				t.Fatalf("%s/%s over the envelope: hash %q (%v), want %q", id, task, viaEnvelope.Hash, err, s.Hash)
			}
		}
		v, err := viewManager(proj, nil).QueryProjectView(viewCtx(), taskSpecQuery(id, tasks[0]))
		if err != nil || v.TaskSpec == nil {
			t.Fatalf("taskSpec view of %s/%s with no committed plan: %v", id, tasks[0], err)
		}
	}
	if _, err := projectstate.TaskSpecFor(&proj, viewActivity, "construction"); err == nil {
		t.Fatal("a non-design activity resolved with no committed plan")
	}
}

// designPrefixTasks is the task ids of a design-prefix activity's lifecycle (the three
// reserved ids name their own lifecycles).
func designPrefixTasks(t *testing.T, id string) []string {
	t.Helper()
	lc, ok := methodassets.LifecycleFor(id)
	if !ok || len(lc.Tasks) == 0 {
		t.Fatalf("no lifecycle %q", id)
	}
	out := make([]string, 0, len(lc.Tasks))
	for _, task := range lc.Tasks {
		out = append(out, task.ID)
	}
	return out
}

// Envelope parity: a spec resolved over the Temporal envelope of a project whose module
// is not at the repo root hashes like one resolved over the project itself, so the
// dispatched spec_hash matches what the job resolves over its checkout.
func TestTaskSpecHashSurvivesTheProjectEnvelope(t *testing.T) {
	proj := *ownProject(t)
	if proj.ModuleDir != "server" {
		t.Fatalf("own moduleDir %q, want server", proj.ModuleDir)
	}
	env, err := projectstate.EncodeProject(proj)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := env.Decode()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"construction", "integration", "codeReview"} {
		want, err := projectstate.TaskSpecFor(&proj, viewActivity, task)
		if err != nil {
			t.Fatal(err)
		}
		got, err := projectstate.TaskSpecFor(&decoded, viewActivity, task)
		if err != nil || got.Hash != want.Hash {
			t.Fatalf("%s over the envelope: %q (%v), want %q", task, got.Hash, err, want.Hash)
		}
	}
}

// R36: the testRun view carries one view per contract the testing gate judges — every
// contract the activity's component owns — not only the first.
func TestTestRunViewCarriesEveryJudgedContract(t *testing.T) {
	proj := *ownProject(t)
	const activity, component = "C-project-state-access", "project-state-access"
	contracts := ownedContracts(&proj, component)
	if len(contracts) < 2 {
		t.Fatalf("%s owns %v, want a multi-contract component", component, contracts)
	}
	pid, act := "archistrator", activity
	v, err := viewManager(proj, nil).QueryProjectView(viewCtx(), delivery.ProjectViewQuery{
		Kind: delivery.ProjectViewTestRun, ProjectID: &pid, ActivityID: &act})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tr := range v.TestRuns {
		got = append(got, tr.Run.Component)
	}
	if !slices.Equal(got, contracts) {
		t.Fatalf("testRuns for %v, want every judged contract %v", got, contracts)
	}
	if v.TestRun == nil || v.TestRun.Run.Component != contracts[0] {
		t.Fatalf("testRun %+v, want the first judged contract %s", v.TestRun, contracts[0])
	}
}

// ownedContracts is the contracts the System gives component, as the gate names them.
func ownedContracts(p *projectstate.Project, component string) []string {
	key, ok := projectstate.ContractKeyOf(p, component)
	if !ok {
		return nil
	}
	var out []string
	for name, sc := range p.ServiceContracts {
		if name == key || sc.Component == key {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}
