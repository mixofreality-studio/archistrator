package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestRunCommandCheckPassFailTimeout(t *testing.T) {
	r := checkRunner{root: t.TempDir(), outDir: t.TempDir(), now: time.Now,
		exec: func(ctx context.Context, _ string, _ []string, argv ...string) ([]byte, error) {
			switch argv[0] {
			case "ok":
				return []byte("fine"), nil
			case "bad":
				return []byte("boom"), errors.New("exit status 1")
			default:
				<-ctx.Done()
				return nil, ctx.Err()
			}
		}}
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "command:ok", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"ok"}}, TimeoutSec: 5},
		{ID: "command:bad", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"bad"}}, TimeoutSec: 5},
		{ID: "command:hang", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"hang"}}, TimeoutSec: 1},
	})
	got := []string{res[0].Status, res[1].Status, res[2].Status}
	if !slices.Equal(got, []string{"pass", "fail", "timeout"}) {
		t.Fatalf("statuses %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "command_bad.log")); !strings.Contains(string(b), "boom") {
		t.Fatal("log not written")
	}
}

// TestRunCommandCheckRealProcess runs real processes through the default exec: a
// nonzero exit fails with its output logged, and a hung process is killed at its cap.
func TestRunCommandCheckRealProcess(t *testing.T) {
	r := checkRunner{root: t.TempDir(), outDir: t.TempDir()}
	began := time.Now()
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "command:red", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"sh", "-c", "echo red-output; exit 3"}}, TimeoutSec: 10},
		{ID: "command:hang", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"sleep", "30"}}, TimeoutSec: 1},
	})
	if res[0].Status != "fail" || res[1].Status != "timeout" {
		t.Fatalf("got %s %s", res[0].Status, res[1].Status)
	}
	if elapsed := time.Since(began); elapsed > 15*time.Second {
		t.Fatalf("a hung command must be stopped at its cap, took %s", elapsed)
	}
	if b, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "command_red.log")); !strings.Contains(string(b), "red-output") || !strings.Contains(string(b), "exit status 3") {
		t.Fatalf("log %q", b)
	}
}

// TestRunCommandCheckDirAndEnv pins where a command runs and what it sees: the check's
// Dir under the repo root, the process env plus GOWORK=off for go and make only.
func TestRunCommandCheckDirAndEnv(t *testing.T) {
	root := t.TempDir()
	type call struct {
		dir string
		env []string
	}
	var calls []call
	r := checkRunner{root: root, outDir: filepath.Join(root, "check-results"), now: time.Now,
		exec: func(_ context.Context, dir string, env []string, _ ...string) ([]byte, error) {
			calls = append(calls, call{dir, env})
			return nil, nil
		}}
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "command:build", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"go", "build"}, Dir: "server"}, TimeoutSec: 5},
		{ID: "command:gen", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"make", "gen-tests-check"}}, TimeoutSec: 5},
		{ID: "command:typecheck", Kind: "command", Source: projectstate.SpecCheckSource{Argv: []string{"npm", "run", "typecheck"}, Dir: "webApp"}, TimeoutSec: 5},
	})
	if calls[0].dir != filepath.Join(root, "server") || calls[1].dir != root || calls[2].dir != filepath.Join(root, "webApp") {
		t.Fatalf("dirs %q %q %q", calls[0].dir, calls[1].dir, calls[2].dir)
	}
	if !slices.Contains(calls[0].env, "GOWORK=off") || !slices.Contains(calls[1].env, "GOWORK=off") || slices.Contains(calls[2].env, "GOWORK=off") {
		t.Fatal("GOWORK=off must be set for go and make only")
	}
	if res[0].OutputRef == nil || *res[0].OutputRef != "check-results/checks/command_build.log" {
		t.Fatalf("outputRef must be repo-root relative, got %v", res[0].OutputRef)
	}
}

func TestRuleCheckFiltersByPrefixAndFailsOnError(t *testing.T) {
	root := repoWithProject(t, glossaryWithAnErrorFinding(t))
	r := checkRunner{root: root, outDir: t.TempDir(), now: time.Now}
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "rule:glossary", Kind: "rule", Source: projectstate.SpecCheckSource{RuleSlot: "Glossary", RulePrefixes: []string{"GLOSS-"}}, TimeoutSec: 60},
		{ID: "rule:volatilities", Kind: "rule", Source: projectstate.SpecCheckSource{RuleSlot: "Volatilities", RulePrefixes: []string{"VOL-"}}, TimeoutSec: 60},
	})
	if res[0].Status != "fail" || res[1].Status != "pass" {
		t.Fatalf("got %s %s", res[0].Status, res[1].Status)
	}
	b, err := os.ReadFile(filepath.Join(r.outDir, "checks", "rule_glossary.log"))
	if err != nil || !strings.Contains(string(b), "GLOSS-FOURQ") || !strings.Contains(string(b), "ERROR") {
		t.Fatalf("the rule log must list the kept Error finding, got %q (%v)", b, err)
	}
}

func TestScenarioCheckMissingVerdictFails(t *testing.T) {
	root := repoWithProject(t, goContractProject())
	var argvSeen []string
	var envSeen []string
	r := checkRunner{root: root, outDir: filepath.Join(root, "check-results"), now: time.Now,
		exec: func(_ context.Context, dir string, env []string, argv ...string) ([]byte, error) {
			argvSeen, envSeen = argv, env
			if dir != filepath.Join(root, "server") {
				t.Errorf("go scenarios must run in the module dir, got %s", dir)
			}
			writeIndex(t, filepath.Join(dir, "test-results", "billingStateAccess"), []scenarioVerdict{
				{Scenario: "bill-P1", Status: "pass", DurationMs: 12},
			})
			stream := `{"Action":"output","Test":"TestScenario_bill_P1","Output":"ran A\n"}` + "\n" +
				`{"Action":"output","Test":"TestScenario_bill_P1/bill-P1","Output":"sub A\n"}` + "\n" +
				`{"Action":"output","Test":"TestScenario_bill_P2","Output":"ran B\n"}` + "\n"
			return []byte(stream), errors.New("exit status 1")
		}}
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "scenario:billingStateAccess:bill-P1", Kind: "scenario", Source: projectstate.SpecCheckSource{Contract: "billingStateAccess", Scenario: "bill-P1"}, TimeoutSec: 30},
		{ID: "scenario:billingStateAccess:bill-P2", Kind: "scenario", Source: projectstate.SpecCheckSource{Contract: "billingStateAccess", Scenario: "bill-P2"}, TimeoutSec: 30},
	})
	if res[0].Status != "pass" || res[1].Status != "fail" {
		t.Fatalf("got %s %s", res[0].Status, res[1].Status)
	}
	if res[0].DurationMs != 12 {
		t.Fatalf("duration must come from the index, got %d", res[0].DurationMs)
	}
	logA, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "scenario_billingStateAccess_bill-P1.log"))
	if string(logA) != "ran A\nsub A\n" {
		t.Fatalf("scenario log must be its slice of the -json stream, got %q", logA)
	}
	logB, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "scenario_billingStateAccess_bill-P2.log"))
	if !strings.Contains(string(logB), "no verdict recorded") {
		t.Fatalf("missing verdict must say so, got %q", logB)
	}
	want := []string{"go", "test", "-p", "1", "-count=1", "-timeout", "120s", "./internal/resourceaccess/billingstate/",
		"-run", "^TestScenario(_[A-Za-z0-9]+)?_(bill_P1|bill_P2)$", "-json"}
	if !slices.Equal(argvSeen, want) {
		t.Fatalf("argv\n got %q\nwant %q", argvSeen, want)
	}
	if !slices.Contains(envSeen, "GOWORK=off") || !slices.Contains(envSeen, "AIARCH_SCENARIO_TIMEOUT=30s") {
		t.Fatalf("env must carry GOWORK=off and the per-scenario cap, got %v", envSeen)
	}
}

// TestScenarioCheckTimeoutAndStaleVerdict: a per-scenario timeout in the index maps to
// timeout, and a verdict left in the index by an earlier run of a scenario this run
// selected never counts — the runner prunes it before the run.
func TestScenarioCheckTimeoutAndStaleVerdict(t *testing.T) {
	root := repoWithProject(t, goContractProject())
	idx := filepath.Join(root, "server", "test-results", "billingStateAccess")
	writeIndex(t, idx, []scenarioVerdict{
		{Scenario: "bill-P2", Status: "pass"},  // stale: selected but not re-recorded
		{Scenario: "other-P9", Status: "pass"}, // not selected: survives
	})
	r := checkRunner{root: root, outDir: filepath.Join(root, "check-results"), now: time.Now,
		exec: func(_ context.Context, dir string, _ []string, _ ...string) ([]byte, error) {
			got := readIndex(t, filepath.Join(dir, "test-results", "billingStateAccess"))
			if len(got) != 1 || got[0].Scenario != "other-P9" {
				t.Errorf("the selected scenarios must be pruned before the run, index %v", got)
			}
			writeIndex(t, filepath.Join(dir, "test-results", "billingStateAccess"), append(got, scenarioVerdict{Scenario: "bill-P1", Status: "timeout"}))
			return nil, errors.New("exit status 1")
		}}
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "scenario:billingStateAccess:bill-P1", Kind: "scenario", Source: projectstate.SpecCheckSource{Contract: "billingStateAccess", Scenario: "bill-P1"}},
		{ID: "scenario:billingStateAccess:bill-P2", Kind: "scenario", Source: projectstate.SpecCheckSource{Contract: "billingStateAccess", Scenario: "bill-P2"}},
	})
	if res[0].Status != "timeout" || res[1].Status != "fail" {
		t.Fatalf("got %s %s", res[0].Status, res[1].Status)
	}
}

// TestScenarioCheckGoTimeoutFlags pins R8: TimeoutSec defaults to 900 and go test's own
// -timeout is sum(TimeoutSec)+60s, so the package default never panics a long batch.
func TestScenarioCheckGoTimeoutFlags(t *testing.T) {
	root := repoWithProject(t, goContractProject())
	var argvSeen, envSeen []string
	r := checkRunner{root: root, outDir: t.TempDir(), now: time.Now,
		exec: func(_ context.Context, _ string, env []string, argv ...string) ([]byte, error) {
			argvSeen, envSeen = argv, env
			return nil, nil
		}}
	r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "scenario:billingStateAccess:a", Kind: "scenario", Source: projectstate.SpecCheckSource{Contract: "billingStateAccess", Scenario: "a"}},
		{ID: "scenario:billingStateAccess:b", Kind: "scenario", Source: projectstate.SpecCheckSource{Contract: "billingStateAccess", Scenario: "b"}},
	})
	if i := slices.Index(argvSeen, "-timeout"); i < 0 || argvSeen[i+1] != "1860s" {
		t.Fatalf("go test -timeout must be 2×900+60s, argv %q", argvSeen)
	}
	if !slices.Contains(envSeen, "AIARCH_SCENARIO_TIMEOUT=900s") {
		t.Fatalf("default per-scenario cap must be 900s, env %v", envSeen)
	}
}

// TestScenarioCheckWebContract: a contract with no goPackage runs Playwright from
// uitests with the contract's generated config, and its index is copied to the
// repo-root test-results/<contract>/ with refs resolved there.
func TestScenarioCheckWebContract(t *testing.T) {
	root := repoWithProject(t, minimalProject())
	var argvSeen, envSeen []string
	var dirSeen string
	r := checkRunner{root: root, outDir: filepath.Join(root, "check-results"), now: time.Now,
		exec: func(_ context.Context, dir string, env []string, argv ...string) ([]byte, error) {
			argvSeen, envSeen, dirSeen = argv, env, dir
			writeIndex(t, filepath.Join(dir, "test-results", "webClient"), []scenarioVerdict{
				{Scenario: "s-P1", Status: "fail", Stdout: "clicked\n", Stderr: "expected visible\n", VideoRef: "s-P1/video.webm", TraceRef: "s-P1/trace.zip"},
			})
			return []byte("1 failed"), errors.New("exit status 1")
		}}
	res := r.Run(context.Background(), []projectstate.SpecCheck{
		{ID: "scenario:webClient:s-P1", Kind: "scenario", Source: projectstate.SpecCheckSource{Contract: "webClient", Scenario: "s-P1"}, TimeoutSec: 60},
	})
	want := []string{"npx", "playwright", "test", "-c", "generated/webClient/playwright.config.gen.ts", "--grep", `(s-P1) — `}
	if !slices.Equal(argvSeen, want) || dirSeen != filepath.Join(root, "uitests") {
		t.Fatalf("argv %q in %s", argvSeen, dirSeen)
	}
	if !slices.Contains(envSeen, "AIARCH_SCENARIO_TIMEOUT_MS=60000") {
		t.Fatalf("env %v", envSeen)
	}
	if res[0].Status != "fail" || res[0].VideoRef == nil || *res[0].VideoRef != "test-results/webClient/s-P1/video.webm" ||
		res[0].TraceRef == nil || *res[0].TraceRef != "test-results/webClient/s-P1/trace.zip" {
		t.Fatalf("result %+v", res[0])
	}
	if _, err := os.Stat(filepath.Join(root, "test-results", "webClient", "results.json")); err != nil {
		t.Fatalf("the web index must be copied to test-results/<contract>/: %v", err)
	}
	log, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "scenario_webClient_s-P1.log"))
	if string(log) != "clicked\nexpected visible\n" {
		t.Fatalf("web log is the index stdout+stderr, got %q", log)
	}
}

func TestFailedOnlySelectsFailuresAndTimeouts(t *testing.T) {
	all := []projectstate.SpecCheck{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	prev := []CheckResult{{CheckId: "a", Status: "pass"}, {CheckId: "b", Status: "fail"}, {CheckId: "c", Status: "timeout"}}
	got := failedOnly(prev, all)
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("%v", got)
	}
}

// TestScenarioIdentMatchesTestgen pins the replicated testgen ident against test names
// the generator actually emitted: a single-contract package (plain names) and a
// multi-contract one (interface infix).
func TestScenarioIdentMatchesTestgen(t *testing.T) {
	cases := []struct{ file, iface, id string }{
		{"../../internal/resourceaccess/billingstate/access_scenarios.gen_test.go", "BillingStateAccess", "bill-the-user-for-usage-P1"},
		{"../../internal/resourceaccess/projectstate/access_scenarios.gen_test.go", "ActivityExecutionAccess", "add-a-use-case-to-an-in-flight-project-P1"},
	}
	for _, c := range cases {
		src, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, name := range goScenarioTestNames(c.iface, c.id) {
			if strings.Contains(string(src), "func "+name+"(t *testing.T)") {
				found = true
			}
		}
		if !found {
			t.Fatalf("no generated test in %s is named any of %v", c.file, goScenarioTestNames(c.iface, c.id))
		}
		re := regexp.MustCompile(goScenarioRunRegex([]string{c.id}))
		if !re.MatchString(goScenarioTestNames(c.iface, c.id)[0]) || !re.MatchString(goScenarioTestNames(c.iface, c.id)[1]) {
			t.Fatalf("-run regex %s must select both spellings", re)
		}
	}
}

// --- fixtures ---

func repoWithProject(t *testing.T, p projectstate.Project) string {
	t.Helper()
	return seedValidateRoot(t, p)
}

// glossaryWithAnErrorFinding is a project whose committed glossary carries a
// non-canonical category, which GLOSS-FOURQ reports as an Error.
func glossaryWithAnErrorFinding(t *testing.T) projectstate.Project {
	t.Helper()
	return withGlossaryDefect(minimalProject())
}

func goContractProject() projectstate.Project {
	p := minimalProject()
	p.ModuleDir = "server"
	p.ServiceContracts = map[string]projectstate.ServiceContract{
		"billingStateAccess": {
			Component: "billingStateAccess", Layer: "ResourceAccess",
			GoPackage: "internal/resourceaccess/billingstate",
			Interface: projectstate.ContractInterface{Name: "BillingStateAccess"},
		},
	}
	return p
}

func writeIndex(t *testing.T, dir string, v []scenarioVerdict) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readIndex(t *testing.T, dir string) []scenarioVerdict {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v []scenarioVerdict
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
