package main

// run014_test.go replays RUN-014's wall. testdata/fresh/run014.project.json is that run's main
// when the architecture draft stopped: the committed core use cases carried an action that only
// returns what the read, match and count nodes before it produced (review-the-todo-list/n11) and
// three refusals on a verdict an earlier check already returned. CC-COVERAGE requires a call for
// every action node, so the draft could not honestly draw them, and its only instruction ("say
// so in your publishDraft message") could not work. reportUpstreamDefect is how that stop is
// told to the system.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	run014Activity = "architecture"
	run014Task     = "architectureDraft"
	run014Gate     = "coreUseCasesReview"
)

func run014Session(t *testing.T) *Session {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run014.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Session{Mode: jobModeDraft, Kind: projectstate.KindSystem, StateRoot: bornRepo(t, raw),
		ActivityID: run014Activity, TaskID: run014Task, git: runGit}
}

// run014Nodes are the four nodes RUN-014's draft could not realize, the first by its use case's
// name and the rest by id, as an agent may write either.
func run014Nodes() []projectstate.UpstreamNodeRef {
	return []projectstate.UpstreamNodeRef{
		{UseCase: "Review the Todo List", Node: "n11"},
		{UseCase: "change-the-todo-list", Node: "n12"},
		{UseCase: "review-the-todo-list", Node: "n12"},
		{UseCase: "drive-the-list-from-agent-tooling", Node: "n6"},
	}
}

func TestRun014ReportUpstreamDefectIsStampedOnTheAttempt(t *testing.T) {
	s := run014Session(t)
	valid := reportUpstreamDefectInput{Gate: run014Gate, NodeRefs: run014Nodes(),
		Rationale: "n11 only hands back what n6, n8 and n9 produced; each refusal is n5's verdict, already returned"}
	if _, err := s.reportUpstreamDefect(valid); err == nil || !strings.Contains(err.Error(), "attempt") {
		t.Errorf("with no ambient attempt the verb must refuse, got %v", err)
	}
	revision := projectstate.AttemptID(run014Activity, run014Task, 1)
	t.Setenv(envRevision, revision)
	assertUpstreamRefusals(t, s)
	if _, err := s.publishDraft("nothing"); err == nil {
		t.Fatal("a refused report recorded nothing, so the publish must be refused too")
	}
	if _, err := s.reportUpstreamDefect(valid); err != nil {
		t.Fatal(err)
	}
	// A repeat under one attempt replaces rather than adds.
	if _, err := s.reportUpstreamDefect(valid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.publishDraft("upstream defect"); err != nil {
		t.Fatalf("a recorded report must publish, or the stop is an empty exit again: %v", err)
	}
	assertUpstreamStamped(t, s, revision, valid.Rationale)
}

// assertUpstreamStamped checks the one report on the document: the verb's stamps, the nodes
// filed by id, the upstream slot untouched, and the report surviving the workflow boundary.
func assertUpstreamStamped(t *testing.T, s *Session, revision, rationale string) {
	t.Helper()
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.UpstreamDefects) != 1 {
		t.Fatalf("%d reports on the document, want 1: %+v", len(proj.UpstreamDefects), proj.UpstreamDefects)
	}
	r := proj.UpstreamDefects[0]
	if r.ID != revision+":requirements:"+run014Gate || r.Revision != revision || r.Activity != run014Activity ||
		r.Task != run014Task || r.ActivityType != "requirements" || r.Gate != run014Gate || r.Rationale != rationale {
		t.Errorf("the report is not stamped by the verb: %+v", r)
	}
	if got := r.NodeRefs[0]; got.UseCase != "review-the-todo-list" || got.Node != "n11" {
		t.Errorf("a node named by its use case's name must be filed under the id, got %+v", got)
	}
	if proj.CoreUseCases.Status != projectstate.ReviewCommitted {
		t.Error("the report must not touch the upstream slot")
	}
	assertUpstreamCrossesTheEnvelope(t, proj, r.ID)
}

// assertUpstreamCrossesTheEnvelope checks the walk reads the report through the workflow boundary.
func assertUpstreamCrossesTheEnvelope(t *testing.T, proj projectstate.Project, id string) {
	t.Helper()
	env, err := projectstate.EncodeProject(proj)
	if err != nil {
		t.Fatal(err)
	}
	back, err := env.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if len(back.UpstreamDefects) != 1 || back.UpstreamDefects[0].ID != id {
		t.Errorf("the report does not survive the envelope: %+v", back.UpstreamDefects)
	}
}

// assertUpstreamRefusals checks what the verb refuses on this document.
func assertUpstreamRefusals(t *testing.T, s *Session) {
	t.Helper()
	nodes := run014Nodes()
	for name, tc := range map[string]struct {
		task string
		in   reportUpstreamDefectInput
		want string
	}{
		"no rationale":        {run014Task, reportUpstreamDefectInput{Gate: run014Gate, NodeRefs: nodes}, "rationale"},
		"no nodes":            {run014Task, reportUpstreamDefectInput{Gate: run014Gate, Rationale: "x"}, "at least one node"},
		"an undeclared gate":  {run014Task, reportUpstreamDefectInput{Gate: "glossaryReview", NodeRefs: nodes, Rationale: "x"}, "may report only to requirements.coreUseCasesReview"},
		"a task with none":    {"architectureReview", reportUpstreamDefectInput{Gate: run014Gate, NodeRefs: nodes, Rationale: "x"}, "declares no upstream route"},
		"an unknown use case": {run014Task, reportUpstreamDefectInput{Gate: run014Gate, NodeRefs: []projectstate.UpstreamNodeRef{{UseCase: "ghost", Node: "n1"}}, Rationale: "x"}, "no committed use case"},
		"an unknown node":     {run014Task, reportUpstreamDefectInput{Gate: run014Gate, NodeRefs: []projectstate.UpstreamNodeRef{{UseCase: "capture-a-todo", Node: "n99"}}, Rationale: "x"}, "has no node"},
		"a note":              {run014Task, reportUpstreamDefectInput{Gate: run014Gate, NodeRefs: []projectstate.UpstreamNodeRef{{UseCase: "review-the-todo-list", Node: "n14"}}, Rationale: "x"}, "no realization obligation"},
		"a decision":          {run014Task, reportUpstreamDefectInput{Gate: run014Gate, NodeRefs: []projectstate.UpstreamNodeRef{{UseCase: "review-the-todo-list", Node: "n5"}}, Rationale: "x"}, "no realization obligation"},
		"a node twice":        {run014Task, reportUpstreamDefectInput{Gate: run014Gate, NodeRefs: []projectstate.UpstreamNodeRef{nodes[0], {UseCase: "review-the-todo-list", Node: "n11"}}, Rationale: "x"}, "named twice"},
	} {
		s.TaskID = tc.task
		if _, err := s.reportUpstreamDefect(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want a refusal naming %q", name, err, tc.want)
		}
	}
	s.TaskID = run014Task
}

// The verb is the draft job's alone: a critic, an answer job and a construction job never see it,
// and the step manifest grants it to the system draft.
func TestReportUpstreamDefectIsTheSystemDraftsVerb(t *testing.T) {
	for _, v := range composedVerbs(&Session{}) {
		if v.name == "reportUpstreamDefect" && (len(v.modes) != 1 || v.modes[0] != jobModeDraft) {
			t.Errorf("reportUpstreamDefect registers in %v, want draft mode alone", v.modes)
		}
	}
	if !grantedTools(&Session{Command: "system-draft"}).allows("reportUpstreamDefect") {
		t.Error("the system-draft step is not granted reportUpstreamDefect")
	}
	if grantedTools(&Session{Command: "core-use-cases-draft"}).allows("reportUpstreamDefect") {
		t.Error("a draft whose task declares no upstream route is granted reportUpstreamDefect")
	}
}
