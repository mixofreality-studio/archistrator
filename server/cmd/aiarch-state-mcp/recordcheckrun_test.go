package main

// recordcheckrun_test.go — the `record-check-run` ONE-SHOT subcommand the venue runs
// after the check gate: it turns check-results/results.json into a projectstate.CheckRun
// on .testingState.checkRuns, red or green (deviation 5: an evidence write, no methodcheck).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// recordSpec is a two-check spec for activity C-BG's integration task.
func recordSpec() projectstate.TaskSpec {
	s := projectstate.TaskSpec{
		Activity: "C-BG", Task: "integration",
		Checks: []projectstate.SpecCheck{
			{ID: "rule:project", Kind: projectstate.CheckKindRule, Title: "Method rules"},
			{ID: "scenario:billingManager:s1", Kind: projectstate.CheckKindScenario, Title: "s1"},
		},
	}
	s.Hash = s.ComputeHash()
	return s
}

// writeJSONFile writes v as JSON to a fresh temp file and returns its path.
func writeJSONFile(t *testing.T, name string, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func recordResults(ruleStatus string) []CheckResult {
	return []CheckResult{
		{CheckId: "rule:project", Kind: "rule", Status: ruleStatus, DurationMs: 40, OutputRef: ptr("check-results/checks/rule_project.log")},
		{CheckId: "scenario:billingManager:s1", Kind: "scenario", Status: "pass", DurationMs: 60},
	}
}

// recordFlags is the full flag set the workflow passes.
func recordFlags(spec, results string) []string {
	return []string{"--spec", spec, "--results", results, "--run-id", "gh-7-1", "--artifact", "check-results-C-BG-7-1", "--revision", "C-BG:integration:1"}
}

// seedRecordCheckout seats p as the construct checkout's project (no project id in env)
// and returns the session that reads it back.
func seedRecordCheckout(t *testing.T, p projectstate.Project) *Session {
	t.Helper()
	s, _ := seedProject(t, p, jobModeConstruct, 0)
	t.Setenv(envJobMode, "")
	t.Setenv(envArtifactKind, "")
	t.Setenv(envStateRoot, s.StateRoot)
	t.Setenv(envProjectID, "")
	t.Setenv(envActivityID, "")
	t.Setenv(envRevision, "")
	return s
}

// TestRecordCheckRun_RedRuleCheckStillRecords pins review focus 4: a project whose
// glossary carries an Error finding (the methodcheck gate would refuse any validated
// write) still records its red CheckRun — evidence is written red or green.
func TestRecordCheckRun_RedRuleCheckStillRecords(t *testing.T) {
	s := seedRecordCheckout(t, glossaryWithAnErrorFinding(t))
	spec := writeJSONFile(t, "spec.json", recordSpec())
	results := writeJSONFile(t, "results.json", recordResults("fail"))
	if err := runRecordCheckRun(recordFlags(spec, results)); err != nil {
		t.Fatalf("a red run must still record: %v", err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if proj.TestingState == nil || len(proj.TestingState.CheckRuns) != 1 {
		t.Fatalf("want one CheckRun, got %+v", proj.TestingState)
	}
	assertRecordedAsGiven(t, proj.TestingState.CheckRuns[0])
	if proj.ID != "testproj" {
		t.Fatalf("the document's id must survive the write: %q", proj.ID)
	}
}

// assertRecordedAsGiven holds the recorded run to recordFlags, recordSpec and a red
// recordResults.
func assertRecordedAsGiven(t *testing.T, run projectstate.CheckRun) {
	t.Helper()
	want := projectstate.CheckRun{ID: "gh-7-1", Activity: "C-BG", Task: "integration", Revision: "C-BG:integration:1",
		SpecHash: recordSpec().Hash, Artifact: "check-results-C-BG-7-1"}
	got := run
	got.StartedAt, got.EndedAt, got.Results = nil, nil, nil
	if !reflect.DeepEqual(got, want) || len(run.Results) != 2 || run.Results[0].Status != "fail" {
		t.Fatalf("run not recorded as given: %+v", run)
	}
	if run.StartedAt == nil || run.EndedAt == nil || run.EndedAt.Before(*run.StartedAt) {
		t.Fatalf("started/ended: %+v", run)
	}
}

// TestRecordCheckRun_ReRecordReplaces: the same run id recorded twice (a re-run, or the
// push-race retry) leaves one CheckRun, the latest.
func TestRecordCheckRun_ReRecordReplaces(t *testing.T) {
	s := seedRecordCheckout(t, minimalProject())
	spec := writeJSONFile(t, "spec.json", recordSpec())
	for _, status := range []string{"fail", "pass"} {
		if err := runRecordCheckRun(recordFlags(spec, writeJSONFile(t, "results.json", recordResults(status)))); err != nil {
			t.Fatal(err)
		}
	}
	proj, _, _ := s.readProject()
	if n := len(proj.TestingState.CheckRuns); n != 1 || proj.TestingState.CheckRuns[0].Results[0].Status != "pass" {
		t.Fatalf("want one replaced run, got %+v", proj.TestingState.CheckRuns)
	}
}

// TestRecordCheckRun_RevisionFromEnv: --revision falls back to AIARCH_REVISION, stored
// verbatim; with neither the run is refused (R15: the revision is the attempt id).
func TestRecordCheckRun_RevisionFromEnv(t *testing.T) {
	s := seedRecordCheckout(t, minimalProject())
	flags := recordFlags(writeJSONFile(t, "spec.json", recordSpec()), writeJSONFile(t, "results.json", recordResults("pass")))[:8]
	if err := runRecordCheckRun(flags); err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("want a revision error, got %v", err)
	}
	t.Setenv(envRevision, "C-BG:integration:2")
	if err := runRecordCheckRun(flags); err != nil {
		t.Fatal(err)
	}
	proj, _, _ := s.readProject()
	if got := proj.TestingState.CheckRuns[0].Revision; got != "C-BG:integration:2" {
		t.Fatalf("revision %q", got)
	}
}

// TestRecordCheckRun_RefusesMalformedResults: results that are empty, carry an unknown
// status, name a check the spec does not hold, or miss one it does are refused before
// anything is written — a silently partial run would read as "nothing failed".
func TestRecordCheckRun_RefusesMalformedResults(t *testing.T) {
	s := seedRecordCheckout(t, minimalProject())
	spec := writeJSONFile(t, "spec.json", recordSpec())
	bogus := recordResults("pass")
	bogus[1].Status = "green"
	stranger := append(recordResults("pass"), CheckResult{CheckId: "command:who", Kind: "command", Status: "pass"})
	for name, c := range map[string]struct {
		results any
		want    string
	}{
		"empty":   {[]CheckResult{}, "no results"},
		"status":  {bogus, `status "green"`},
		"unknown": {stranger, `"command:who"`},
		"missing": {recordResults("pass")[:1], `"scenario:billingManager:s1"`},
		"shape":   {map[string]any{"x": 1}, "results"},
	} {
		t.Run(name, func(t *testing.T) {
			err := runRecordCheckRun(recordFlags(spec, writeJSONFile(t, "results.json", c.results)))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error naming %s, got %v", c.want, err)
			}
		})
	}
	proj, _, _ := s.readProject()
	if proj.TestingState != nil {
		t.Fatalf("nothing may be written: %+v", proj.TestingState)
	}
}

// TestRecordCheckRun_RefusesATamperedSpec: the spec's hash must be its content's, so the
// run never carries a hash the server would not compute.
func TestRecordCheckRun_RefusesATamperedSpec(t *testing.T) {
	seedRecordCheckout(t, minimalProject())
	sp := recordSpec()
	sp.Task = "construction"
	err := runRecordCheckRun(recordFlags(writeJSONFile(t, "spec.json", sp), writeJSONFile(t, "results.json", recordResults("pass"))))
	if err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatalf("want a hash error, got %v", err)
	}
}

func TestRecordCheckRun_FlagsRequired(t *testing.T) {
	err := runRecordCheckRun([]string{"--results", "r.json"})
	if err == nil || !strings.Contains(err.Error(), "--spec") {
		t.Fatalf("want a missing --spec error, got %v", err)
	}
}

// TestRecordCheckRun_KeepsTheCommittedDocument: recording onto archistrator's own
// committed project.json, with no AIARCH_PROJECT_ID, changes nothing but .testingState
// (DCT plan 5 r2: a write once renamed the project after the checkout directory).
func TestRecordCheckRun_KeepsTheCommittedDocument(t *testing.T) {
	_, raw := copyCommittedProject(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, statePathPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, statePathPrefix, projectFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envJobMode, "")
	t.Setenv(envArtifactKind, "")
	t.Setenv(envStateRoot, root)
	t.Setenv(envProjectID, "")
	t.Setenv(envActivityID, "")
	if err := runRecordCheckRun(recordFlags(writeJSONFile(t, "spec.json", recordSpec()), writeJSONFile(t, "results.json", recordResults("pass")))); err != nil {
		t.Fatalf("record-check-run: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(root, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	var before, got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatal(err)
	}
	for k, v := range before {
		if k != "testingState" && !bytes.Equal(v, got[k]) {
			t.Errorf("member %q changed by recording a run", k)
		}
	}
	if len(got) != len(before) {
		t.Errorf("members: %d before, %d after", len(before), len(got))
	}
}

// TestOneShotDispatch_CheckSubcommands: the four venue subcommands are one-shots (never
// fall through to the MCP server) whose failures carry the subcommand name, and the
// retired record-test-run is no longer one.
func TestOneShotDispatch_CheckSubcommands(t *testing.T) {
	for _, name := range []string{"task-spec", "run-checks", "edit-scope", "record-check-run"} {
		handled, err := runOneShotCommand([]string{name, "--bogus-flag"})
		if !handled {
			t.Fatalf("%s must be a one-shot subcommand", name)
		}
		if err == nil || !strings.HasPrefix(err.Error(), name+":") {
			t.Fatalf("%s: want a prefixed failure, got %v", name, err)
		}
	}
	if handled, _ := runOneShotCommand([]string{"record-test-run"}); handled {
		t.Fatal("record-test-run is retired; it must not be handled")
	}
}
