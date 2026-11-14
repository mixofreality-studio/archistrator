package internal_test

import (
	"slices"
	"testing"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The M-ACCEPT milestone (FPC plan 4, Amendment C) is a zero-effort derived activity whose one work
// task is a venue JOB: the venue runs the task spec's checks and records the CheckRun, and no agent
// runs. These tests hold what the Manager and the job read of it: its classification, its lifecycle
// key and its task spec.

const mAcceptActivity = "M-ACCEPT"

// withMAccept is the todomvc project with M-ACCEPT committed to its activity list, in memory: the
// fixture predates the milestone, which the derivation now emits (slots 9/10 are re-materialized by
// the plan's release step).
func withMAccept(t *testing.T) *projectstate.Project {
	t.Helper()
	p := todomvcProject(t)
	list, ok := p.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || list == nil {
		t.Fatal("the committed activity list did not decode")
	}
	list.Activities = append(list.Activities, projectstate.ActivityItem{
		Name: mAcceptActivity, Title: "Use-case acceptance (M-ACCEPT)", EffortDays: 0, WorkerClass: "system-architect", RiskBucket: 1,
	})
	return p
}

// A job has a command (the venue's routing slug) and an artifact kind and nothing an agent needs:
// no prompt, no model, no tools. It edits no repo file (its scope is the state document alone) and
// its checks are the boot, the generated tests' currency and one acceptance scenario per residual
// variant, filed under the pseudo-contract "acceptance".
func TestTaskSpecForJobHasNoAgent(t *testing.T) {
	p := withMAccept(t)
	s, err := projectstate.TaskSpecFor(p, mAcceptActivity, "integration")
	if err != nil {
		t.Fatal(err)
	}
	if projectstate.TaskSpecSchemaVersion != 6 {
		t.Errorf("TaskSpecSchemaVersion = %d, want 6: the job kind changes the spec's shape", projectstate.TaskSpecSchemaVersion)
	}
	requireNoAgent(t, s)
	requireAcceptanceChecks(t, p, s)

	// The milestone's review is a human gate: nothing to run, nothing to edit.
	review, err := projectstate.TaskSpecFor(p, mAcceptActivity, "testing")
	if err != nil {
		t.Fatal(err)
	}
	if review.Identity.Kind != methodassets.LifecycleTaskReview || !review.EditScope.StateOnly || len(review.Checks) != 0 {
		t.Errorf("the testing review = %+v, want a command-less review of the run", review)
	}
}

// requireNoAgent holds a job's spec to naming a venue command and configuring no agent.
func requireNoAgent(t *testing.T, s projectstate.TaskSpec) {
	t.Helper()
	if s.Identity.Kind != methodassets.LifecycleTaskJob || s.Identity.Command != "acceptance-run" || s.Identity.ArtifactKind != "AcceptanceRun" {
		t.Fatalf("identity = %+v, want a job of command acceptance-run producing an AcceptanceRun", s.Identity)
	}
	if s.Prompt.Text != "" || s.Model.ID != "" || len(s.Tools.MCP) != 0 || len(s.Tools.Builtin) != 0 || s.Identity.Agent != "" || s.Identity.WorkerClass != "" {
		t.Errorf("a job runs no agent: prompt %q, model %q, tools %+v, agent %q", s.Prompt.Text, s.Model.ID, s.Tools, s.Identity.Agent)
	}
	if !s.EditScope.StateOnly {
		t.Errorf("a job edits no repo file: its scope is state only, got %+v", s.EditScope)
	}
}

// requireAcceptanceChecks holds a job's checks to the rule, the boot, the generated tests' currency and
// one scenario per residual variant, in the order sortChecks gives (rule, command, smoke, scenario, each
// kind by id).
func requireAcceptanceChecks(t *testing.T, p *projectstate.Project, s projectstate.TaskSpec) {
	t.Helper()
	scenarios, err := projectstate.AcceptanceScenarios(p)
	if err != nil || len(scenarios) == 0 {
		t.Fatalf("the todomvc fixture has a residual to run: %d scenarios, %v", len(scenarios), err)
	}
	want := []string{"rule:atp", "command:build-devstack", "command:build-server", "command:gen-tests-check", "smoke:composition"}
	for _, v := range scenarios {
		want = append(want, "scenario:acceptance:"+v.ID)
	}
	var got []string
	for _, c := range s.Checks {
		got = append(got, c.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("checks = %v\nwant     %v", got, want)
	}
	for _, c := range s.Checks[len(s.Checks)-len(scenarios):] {
		if c.Kind != projectstate.CheckKindScenario || c.Source.Contract != projectstate.AcceptanceContract || c.Source.Scenario == "" || c.Source.UseCase == "" {
			t.Errorf("scenario check %+v must name its variant, its use case and the acceptance contract", c)
		}
	}
}

// M-ACCEPT classifies by its exact id, as the three design ids do, to the milestone type; N-ATP by
// its id prefix to the acceptance-plan testing variant. Both keys name a lifecycle.
func TestClassifyActivity_MilestoneAndATP(t *testing.T) {
	typ, variant, err := projectstate.ClassifyActivity(mAcceptActivity, "system-architect", false, false, false)
	if err != nil || typ != projectstate.ActivityTypeMilestone || variant != projectstate.TestVariantAcceptance {
		t.Fatalf("M-ACCEPT classifies as (%v, %v, %v), want milestone/acceptance", typ, variant, err)
	}
	if key := projectstate.LifecycleKeyFor(typ, variant); key != "milestone:acceptance" {
		t.Errorf("lifecycle key = %q, want milestone:acceptance", key)
	}
	typ, variant, err = projectstate.ClassifyActivity("N-ATP", "test-engineer", false, false, false)
	if err != nil || typ != projectstate.ActivityTypeTesting || variant != projectstate.TestVariantAcceptancePlan {
		t.Fatalf("N-ATP classifies as (%v, %v, %v), want testing/acceptancePlan", typ, variant, err)
	}
	if key := projectstate.LifecycleKeyFor(typ, variant); key != "testing:acceptancePlan" {
		t.Errorf("lifecycle key = %q, want testing:acceptancePlan", key)
	}
	for _, key := range []string{"milestone:acceptance", "testing:acceptancePlan"} {
		if _, ok := methodassets.LifecycleFor(key); !ok {
			t.Errorf("the method assets carry no lifecycle %q", key)
		}
	}
	if projectstate.ActivityTypeMilestone.String() != "milestone" || projectstate.TestVariantAcceptance.String() != "acceptance" {
		t.Errorf("wire names = %q / %q, want milestone / acceptance", projectstate.ActivityTypeMilestone, projectstate.TestVariantAcceptance)
	}
}
