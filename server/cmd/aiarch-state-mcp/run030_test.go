package main

// run030_test.go replays RUN-030's wall. testdata/fresh/run030.project.json is that run's main at
// the halt; run030.system-draft.json is the System its draft gave putDraftModel. The refusal named
// UC-OUTCOME-AT-ASKING-SURFACE at shape-the-list: a defect of the use case's own shape, which no
// System can discharge. reportUpstreamDefect refused its decision/sendSignal nodes, so nothing was
// filed. The second report class, a core-use-case rule finding, is how that stop is told.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	run030Rule     = "UC-OUTCOME-AT-ASKING-SURFACE"
	run030UseCase  = "shape-the-list"
	run030Revision = "run030-attempt"
)

func run030Session(t *testing.T) *Session {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run030.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(envRevision, projectstate.AttemptID(run014Activity, run014Task, 1))
	return &Session{Mode: jobModeDraft, Kind: projectstate.KindSystem, StateRoot: bornRepo(t, raw),
		ActivityID: run014Activity, TaskID: run014Task, git: runGit}
}

func run030Report(rules ...upstreamRuleInput) reportUpstreamDefectInput {
	return reportUpstreamDefectInput{Gate: run014Gate, RuleFindings: rules, Rationale: "the use case's outcome is not at the asking surface"}
}

func run030Ref() upstreamRuleInput {
	return upstreamRuleInput{RuleID: run030Rule, UseCase: run030UseCase}
}

func TestRun030RuleFindingIsReportedAfterTheRefusal(t *testing.T) {
	s := run030Session(t)
	draft, err := os.ReadFile(filepath.Join("testdata", "fresh", "run030.system-draft.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.putDraftModel(draft); err == nil || !strings.Contains(err.Error(), run030Rule) {
		t.Fatalf("the System draft must be refused naming %s, got %v", run030Rule, err)
	}
	if _, err := s.reportUpstreamDefect(run030Report(run030Ref())); err != nil {
		t.Fatal(err)
	}
	if _, err := s.publishDraft("upstream defect"); err != nil {
		t.Fatalf("a recorded report must publish: %v", err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.UpstreamDefects) != 1 || len(proj.UpstreamDefects[0].RuleFindings) != 1 || len(proj.UpstreamDefects[0].NodeRefs) != 0 {
		t.Fatalf("want one report with one rule finding: %+v", proj.UpstreamDefects)
	}
	got := proj.UpstreamDefects[0].RuleFindings[0]
	if got.RuleID != run030Rule || got.UseCase != run030UseCase || strings.TrimSpace(got.Finding) == "" {
		t.Errorf("the finding must be filed by id with the refusal's message: %+v", got)
	}
}

func TestRun030RuleFindingNotReportedByTheRefusalIsRefused(t *testing.T) {
	s := run030Session(t)
	raw0, err := os.ReadFile(s.projectFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.reportUpstreamDefect(run030Report(run030Ref())); err == nil || !strings.Contains(err.Error(), "reports no Error") {
		t.Fatalf("with no refusal the ref must be refused, got %v", err)
	}
	raw1, err := os.ReadFile(s.projectFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw0) != string(raw1) || s.wroteState {
		t.Error("a refused report must write nothing")
	}
}

func TestRun030RuleFindingRefusals(t *testing.T) {
	s := run030Session(t)
	draft, err := os.ReadFile(filepath.Join("testdata", "fresh", "run030.system-draft.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.putDraftModel(draft); err == nil {
		t.Fatal("the draft must be refused")
	}
	for name, tc := range map[string]struct {
		in   reportUpstreamDefectInput
		want string
	}{
		"a rule outside the family":      {run030Report(upstreamRuleInput{RuleID: "CC-COVERAGE", UseCase: run030UseCase}), "core-use-case rule"},
		"an unknown use case":            {run030Report(upstreamRuleInput{RuleID: run030Rule, UseCase: "ghost"}), "no committed use case"},
		"a scenario it does not fire at": {run030Report(upstreamRuleInput{RuleID: run030Rule, UseCase: run030UseCase, Scenario: "no-such-scenario"}), "reports no Error"},
		"no refs at all":                 {run030Report(), "at least one"},
		"the same ref twice":             {run030Report(run030Ref(), run030Ref()), "twice"},
		"a decision as a node ref": {reportUpstreamDefectInput{Gate: run014Gate, Rationale: "x",
			NodeRefs: []projectstate.UpstreamNodeRef{{UseCase: run030UseCase, Node: "which-surface"}}}, "no realization obligation"},
	} {
		_, err := s.reportUpstreamDefect(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want a refusal naming %q", name, err, tc.want)
		}
	}
}
