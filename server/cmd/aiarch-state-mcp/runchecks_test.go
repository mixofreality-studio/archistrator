package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// shCheck is a command check running script under sh.
func shCheck(id, title, script string) projectstate.SpecCheck {
	return projectstate.SpecCheck{ID: id, Kind: projectstate.CheckKindCommand, Title: title,
		Source: projectstate.SpecCheckSource{Argv: []string{"sh", "-c", script}}, TimeoutSec: 30}
}

// writeChecksSpec writes a spec holding checks and returns its path.
func writeChecksSpec(t *testing.T, checks ...projectstate.SpecCheck) string {
	t.Helper()
	s := projectstate.TaskSpec{Activity: "C-x", Task: "construction", Checks: checks}
	s.Hash = s.ComputeHash()
	return writeJSONFile(t, "spec.json", s)
}

func readResultsFile(t *testing.T, dir string) []CheckResult {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out []CheckResult
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func statuses(rs []CheckResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.CheckId + "=" + r.Status
	}
	return out
}

// TestRunChecksCmd_ExitCodesAndResults: all green exits 0; any red exits 1 with every
// result (in spec order) in results.json and each check's log beside it.
func TestRunChecksCmd_ExitCodesAndResults(t *testing.T) {
	t.Chdir(t.TempDir())
	out := filepath.Join(t.TempDir(), "check-results")
	green := writeChecksSpec(t, shCheck("command:a", "A", "echo a"))
	if err := runRunChecks([]string{"--gate", "--spec", green, "--out", out}, &bytes.Buffer{}); err != nil {
		t.Fatalf("all green must exit 0: %v", err)
	}

	red := writeChecksSpec(t, shCheck("command:a", "A", "echo a"), shCheck("command:b", "B", "echo broke; exit 3"))
	err := runRunChecks([]string{"--gate", "--spec", red, "--out", out}, &bytes.Buffer{})
	var ee exitError
	if !errors.As(err, &ee) || ee.code != 1 {
		t.Fatalf("a red check must exit 1, got %v", err)
	}
	got := readResultsFile(t, out)
	if !slices.Equal(statuses(got), []string{"command:a=pass", "command:b=fail"}) {
		t.Fatalf("results %v", statuses(got))
	}
	if b, _ := os.ReadFile(filepath.Join(out, "checks", "command_b.log")); !strings.Contains(string(b), "broke") {
		t.Fatalf("log of command:b: %q", b)
	}
}

// TestRunChecksCmd_GateStartsClean: --gate deletes the results dir first, so nothing an
// earlier run left (a stale log or results file) survives into the gate.
func TestRunChecksCmd_GateStartsClean(t *testing.T) {
	t.Chdir(t.TempDir())
	out := filepath.Join(t.TempDir(), "check-results")
	if err := os.MkdirAll(filepath.Join(out, "checks"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(out, "checks", "stale.log")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runRunChecks([]string{"--gate", "--spec", writeChecksSpec(t, shCheck("command:a", "A", "true")), "--out", out}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("the gate must start from a clean results dir")
	}
}

// TestRunChecksCmd_FailedOnlyRerunsOnlyTheRed: --failed-only re-runs the checks the
// previous results.json did not pass and keeps the others' verdicts; with no previous
// results it runs everything.
func TestRunChecksCmd_FailedOnlyRerunsOnlyTheRed(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	out := filepath.Join(t.TempDir(), "check-results")
	marker := filepath.Join(work, "ran")
	spec := writeChecksSpec(t,
		shCheck("command:a", "A", "echo a >> "+marker),
		shCheck("command:b", "B", "echo b >> "+marker+"; test -f "+filepath.Join(work, "fixed")),
	)
	if err := runRunChecks([]string{"--failed-only", "--spec", spec, "--out", out}, &bytes.Buffer{}); err == nil {
		t.Fatal("b is red on the first run")
	}
	if b, _ := os.ReadFile(marker); string(b) != "a\nb\n" {
		t.Fatalf("with no previous results everything runs, ran %q", b)
	}
	if err := os.WriteFile(filepath.Join(work, "fixed"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runRunChecks([]string{"--failed-only", "--spec", spec, "--out", out}, &bytes.Buffer{}); err != nil {
		t.Fatalf("b fixed, a kept: %v", err)
	}
	if b, _ := os.ReadFile(marker); string(b) != "a\nb\nb\n" {
		t.Fatalf("only b may re-run, ran %q", b)
	}
	if got := statuses(readResultsFile(t, out)); !slices.Equal(got, []string{"command:a=pass", "command:b=pass"}) {
		t.Fatalf("merged results %v", got)
	}
}

// TestRunChecksCmd_RunnerErrorExits2: a spec that cannot be read is the runner's own
// failure, exit 2, distinct from a red check.
func TestRunChecksCmd_RunnerErrorExits2(t *testing.T) {
	err := runRunChecks([]string{"--spec", filepath.Join(t.TempDir(), "missing.json"), "--out", t.TempDir()}, &bytes.Buffer{})
	var ee exitError
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Fatalf("want exit 2, got %v", err)
	}
}

// fakeCheckExec fails every command whose argv contains "lint" and records each call.
type fakeCheckExec struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeCheckExec) exec(_ context.Context, _ string, _ []string, argv ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := strings.Join(argv, " ")
	f.calls = append(f.calls, line)
	if strings.Contains(line, "lint") {
		var log strings.Builder
		for i := range 60 {
			log.WriteString("lint line " + string(rune('A'+i%26)) + "\n")
		}
		log.WriteString("LAST LINE\n")
		return []byte(log.String()), errors.New("exit status 1")
	}
	return []byte("ok\n"), nil
}

func (f *fakeCheckExec) reset() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls
	f.calls = nil
	return c
}

func callRunChecks(t *testing.T, cs *mcp.ClientSession, args map[string]any) (*mcp.CallToolResult, runChecksOutput) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: runChecksTool, Arguments: args})
	if err != nil {
		t.Fatalf("run_checks: %v", err)
	}
	var out runChecksOutput
	if !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("structured output: %v", err)
		}
	}
	return res, out
}

// mustRunChecks calls run_checks and fails the test on a tool error.
func mustRunChecks(t *testing.T, cs *mcp.ClientSession, args map[string]any) (string, runChecksOutput) {
	t.Helper()
	res, out := callRunChecks(t, cs, args)
	if res.IsError {
		t.Fatalf("run_checks %v: %s", args, contentText(res))
	}
	return contentText(res), out
}

// assertFailuresOnly holds a run_checks result to the failures-only shape: the lint
// failure carries its title and its log's capped tail, and no pass is listed.
func assertFailuresOnly(t *testing.T, text string, out runChecksOutput) {
	t.Helper()
	if out.Failed < 1 || len(out.Results) != out.Failed {
		t.Fatalf("results must be the failures only: %+v", out)
	}
	i := slices.IndexFunc(out.Results, func(f runChecksFailure) bool { return f.ID == "command:lint" })
	if i < 0 {
		t.Fatalf("no lint failure in %+v", out.Results)
	}
	lint := out.Results[i]
	if lint.Title == "" || !strings.Contains(lint.Tail, "LAST LINE") || strings.Count(lint.Tail, "\n") > tailLines+1 {
		t.Fatalf("lint failure: %+v", lint)
	}
	if !strings.Contains(text, "✗ "+lint.Title) || !strings.Contains(text, "Full logs: ") || strings.Contains(text, "[pass]") {
		t.Fatalf("text must list failures only:\n%s", text)
	}
	if !strings.HasPrefix(out.LogRef, filepath.Join(".aiarch", "tmp", "checks")) {
		t.Fatalf("logRef %q", out.LogRef)
	}
}

func readStateFile(t *testing.T, root string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestRunChecksVerb_ReportsOnlyFailuresAndHonoursFailedOnly: the verb resolves the
// spec from the session's activity and task alone, returns only the failures (each with
// its log's tail), records nothing, and with failedOnly re-runs only those.
func TestRunChecksVerb_ReportsOnlyFailuresAndHonoursFailedOnly(t *testing.T) {
	root := seedOwnCheckout(t)
	before := readStateFile(t, root)
	fake := &fakeCheckExec{}
	s := &Session{Mode: jobModeConstruct, StateRoot: root, ActivityID: "C-agentic-job-access", TaskID: "construction",
		ComponentID: "agentic-job-access", git: runGit, checkExec: fake.exec}
	cs := connectInMemory(t, s)

	text, out := mustRunChecks(t, cs, map[string]any{})
	first := fake.reset()
	if len(first) < 2 {
		t.Fatalf("construction runs several commands, ran %v", first)
	}
	assertFailuresOnly(t, text, out)

	_, out2 := mustRunChecks(t, cs, map[string]any{"failedOnly": true})
	second := fake.reset()
	if !slices.ContainsFunc(second, func(c string) bool { return strings.Contains(c, "lint") }) || len(second) >= len(first) {
		t.Fatalf("failedOnly must re-run only the red commands: first %v, second %v", first, second)
	}
	if out2.Passed+out2.Failed != out.Passed+out.Failed {
		t.Fatalf("failedOnly must report over the whole spec: %+v vs %+v", out2, out)
	}

	if !bytes.Equal(before, readStateFile(t, root)) || s.wroteState {
		t.Fatal("run_checks must record nothing")
	}
	if ig, err := os.ReadFile(filepath.Join(root, ".aiarch", "tmp", ".gitignore")); err != nil || string(ig) != "*\n" {
		t.Fatalf("the scratch dir must ignore itself: %q %v", ig, err)
	}
}

// TestRunChecksVerb_NeedsActivityAndTask: without the ambient activity or task the verb
// fails naming the env var the dispatch should have stamped.
func TestRunChecksVerb_NeedsActivityAndTask(t *testing.T) {
	for _, c := range []struct {
		activity, task, want string
	}{
		{"", "construction", envActivityID},
		{"C-agentic-job-access", "", envTaskID},
	} {
		s := &Session{Mode: jobModeDraft, StateRoot: t.TempDir(), ActivityID: c.activity, TaskID: c.task, checkExec: (&fakeCheckExec{}).exec}
		res, _ := callRunChecks(t, connectInMemory(t, s), map[string]any{})
		if !res.IsError || !strings.Contains(contentText(res), c.want) {
			t.Fatalf("want an error naming %s, got %s", c.want, contentText(res))
		}
	}
}

func TestLogTailCapsLinesAndBytes(t *testing.T) {
	var b strings.Builder
	for range 100 {
		b.WriteString(strings.Repeat("x", 200) + "\n")
	}
	got := checkLogTail(b.String())
	if len(got) > tailBytes || strings.Count(got, "\n") > tailLines {
		t.Fatalf("tail %d bytes, %d lines", len(got), strings.Count(got, "\n"))
	}
	if short := checkLogTail("a\nb\n"); short != "a\nb\n" {
		t.Fatalf("a short log is kept whole: %q", short)
	}
}
