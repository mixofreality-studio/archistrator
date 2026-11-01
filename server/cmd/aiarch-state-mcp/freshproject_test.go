package main

// freshproject_test.go proves a project born through the real seat path can pass its own
// construction gate, and that the gate is not vacuous. Every wall the todomvc bench hit came
// from code that had only ever seen archistrator's own hand-built project.json and layout, so
// this test starts from nothing else: sourcecontrol's rendered scaffold, the contracts an
// agent really recorded (RUN-007, testdata/fresh), the real make targets, the real task spec
// and the real run-checks binary. It resolves the seated module's pins through GOPROXY, which
// is why the platform tags are released before the app consumes them.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

const (
	freshActivity  = "C-todo-list-access"
	freshComponent = "todo-list-access"
	freshContract  = "todoListAccess"
	freshPackage   = "internal/resourceaccess/todolist"
)

// todoListImpl is a component the way a junior developer is told to write it: one file named
// for the leaf and stereotype, an unexported builder behind the generated constructor, and
// nothing hand-written exported.
const todoListImpl = `// Package todolist is the TodoList resource access.
package todolist

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

type postgresAccess struct {
	pool *pgxpool.Pool
}

func newPostgresTodoListAccess(_ context.Context, pool *pgxpool.Pool) (TodoListAccess, error) {
	return &postgresAccess{pool: pool}, nil
}

func (*postgresAccess) RecordNewTodo(fwra.Context, ListOwner, Wording, ChangeID) (TodoID, error) {
	return "", nil
}

func (*postgresAccess) RecordRewording(fwra.Context, ListOwner, TodoID, Wording, ChangeID) (ChangeOutcome, error) {
	return ChangeOutcome{}, nil
}

func (*postgresAccess) RecordMarking(fwra.Context, ListOwner, *TodoID, TodoState, ChangeID) (ChangeOutcome, error) {
	return ChangeOutcome{}, nil
}

func (*postgresAccess) DiscardTodo(fwra.Context, ListOwner, TodoID, ChangeID) (ChangeOutcome, error) {
	return ChangeOutcome{}, nil
}

func (*postgresAccess) SweepCompleted(fwra.Context, ListOwner, ChangeID) error { return nil }

func (*postgresAccess) RecallList(fwra.Context, ListOwner) (TodoList, error) {
	return TodoList{}, nil
}
`

var (
	stateBinOnce sync.Once
	stateBinPath string
	stateBinErr  error
)

// stateBinary builds the real aiarch-state-mcp once per test binary, against this module.
func stateBinary(t *testing.T) string {
	t.Helper()
	stateBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fresh-state-bin")
		if err != nil {
			stateBinErr = err
			return
		}
		stateBinPath = filepath.Join(dir, "aiarch-state-mcp")
		cmd := exec.Command("go", "build", "-o", stateBinPath, ".")
		cmd.Env = append(os.Environ(), "GOWORK=off")
		if out, err := cmd.CombinedOutput(); err != nil {
			stateBinErr = &buildError{out: string(out), err: err}
		}
	})
	if stateBinErr != nil {
		t.Fatal(stateBinErr)
	}
	return stateBinPath
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return e.err.Error() + ": " + e.out }

func writeRepoFile(t *testing.T, repo, rel string, body []byte) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func inRepo(t *testing.T, repo string, argv ...string) string {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(argv, " "), err, out)
	}
	return string(out)
}

// freshState is RUN-007's state document with its contracts recorded the way the verb now
// records them: the capitalised layer canonicalised, the Resource named as infra refused.
func freshState(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run007.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ServiceContracts map[string]projectstate.ServiceContract `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ServiceContracts[freshContract].Interface.Layer != "ResourceAccess" {
		t.Fatalf("the fixture must keep the capitalised layer RUN-007's agent recorded")
	}
	caller := doc.ServiceContracts["callerAccess"]
	err = canonicalizeContract("callerAccess", &caller)
	if err == nil || !strings.Contains(err.Error(), "ExternalHTTP") {
		t.Fatalf("recording RUN-007's IdentityProvider infra must be refused, naming the menu: %v", err)
	}
	caller.Infra = []string{"ExternalHTTP"}
	if err := canonicalizeContract("callerAccess", &caller); err != nil {
		t.Fatal(err)
	}
	todo := doc.ServiceContracts[freshContract]
	if err := canonicalizeContract(freshContract, &todo); err != nil {
		t.Fatal(err)
	}
	if todo.Interface.Layer != "resourceaccess" {
		t.Fatalf("the layer must be canonical, got %q", todo.Interface.Layer)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	contracts, err := json.Marshal(map[string]projectstate.ServiceContract{freshContract: todo, "callerAccess": caller})
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = contracts
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// bornRepo seats a repo through the real scaffold render and commits it.
func bornRepo(t *testing.T, state []byte) string {
	t.Helper()
	repo := t.TempDir()
	files, err := sourcecontrol.ManagedScaffoldFiles(sourcecontrol.RepoRefFromString("acme|acme/todomvc"), "aiarch-app")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		writeRepoFile(t, repo, f.Path, f.Content)
	}
	writeRepoFile(t, repo, ".aiarch/state/project.json", state)
	inRepo(t, repo, "git", "init", "-q", "-b", "main")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "birth")
	return repo
}

func taskSpec(t *testing.T, repo, task string) projectstate.TaskSpec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, ".aiarch/state/project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(raw, anyProject)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	spec, err := projectstate.TaskSpecFor(&proj, freshActivity, task)
	if err != nil {
		t.Fatalf("TaskSpecFor(%s): %v", task, err)
	}
	return spec
}

// gate runs the real binary's --gate over the checks, returning id → status and the logs.
func gate(t *testing.T, repo string, spec projectstate.TaskSpec) (map[string]string, map[string]string) {
	t.Helper()
	out := t.TempDir()
	spec.Hash = spec.ComputeHash()
	body, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(out, "spec.json")
	if err := os.WriteFile(specPath, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(stateBinary(t), "run-checks", "--gate", "--spec", specPath, "--out", filepath.Join(out, "res"))
	cmd.Dir = repo
	_, _ = cmd.CombinedOutput() // exit 1 is a red check; the results file says which
	results, err := readPrevResults(filepath.Join(out, "res"))
	if err != nil || results == nil {
		t.Fatalf("no results: %v", err)
	}
	statuses, logs := map[string]string{}, map[string]string{}
	for _, r := range results {
		statuses[r.CheckId] = r.Status
		if r.OutputRef != nil {
			p := *r.OutputRef
			if !filepath.IsAbs(p) {
				p = filepath.Join(repo, p)
			}
			if b, err := os.ReadFile(p); err == nil {
				logs[r.CheckId] = string(b)
			}
		}
	}
	return statuses, logs
}

// commandChecks keeps the spec's command checks with the given ids (all when none), dropping
// golangci-lint when this machine has none: its absence says nothing about the project.
func commandChecks(t *testing.T, spec projectstate.TaskSpec, ids ...string) projectstate.TaskSpec {
	t.Helper()
	_, lintErr := exec.LookPath("golangci-lint")
	kept := spec.Checks[:0:0]
	for _, c := range spec.Checks {
		switch {
		case c.Kind != projectstate.CheckKindCommand:
		case c.ID == "command:lint" && lintErr != nil:
			t.Log("golangci-lint is not installed; command:lint not exercised")
		case len(ids) == 0 || slices.Contains(ids, c.ID):
			kept = append(kept, c)
		}
	}
	spec.Checks = kept
	return spec
}

func requirePassed(t *testing.T, statuses, logs map[string]string) {
	t.Helper()
	if len(statuses) == 0 {
		t.Fatal("no check ran")
	}
	for id, st := range statuses {
		if st != checkPass {
			t.Errorf("%s: %s\n%s", id, st, logs[id])
		}
	}
}

// builtRepo is a born repo with the conventional component committed, the way a passing
// construction task leaves it: job-generated contract, the verb's tidy, the agent's commit.
func builtRepo(t *testing.T) string {
	t.Helper()
	repo := bornRepo(t, freshState(t))
	inRepo(t, repo, "make", "gen-models", "CONTRACTS="+freshContract)
	writeRepoFile(t, repo, freshPackage+"/todolistaccess.go", []byte(todoListImpl))
	s := &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: freshActivity, TaskID: "construction", ComponentID: freshComponent, git: runGit}
	res, err := s.runChecks(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Note, "module graph was tidied") {
		t.Errorf("the verb must tidy and tell the agent to commit, note = %q", res.Note)
	}
	if !strings.Contains(inRepo(t, repo, "git", "status", "--short"), "go.mod") {
		t.Error("a pgx import must have moved go.mod: the verb did not tidy")
	}
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "build the component")
	return repo
}

// The birth state itself: every seat renders, the module is tidy, and the gate is green before
// any component exists. An untidy birth would redden every project on its first commit.
func TestFreshBirthIsGreen(t *testing.T) {
	repo := bornRepo(t, freshState(t))
	for _, seat := range []string{"go.mod", "go.sum", "Makefile", "Makefile.scenarios.mk", ".gitattributes", ".golangci.yml", "internal/aiarch_arch_test.go"} {
		if _, err := os.Stat(filepath.Join(repo, seat)); err != nil {
			t.Errorf("birth did not seat %s: %v", seat, err)
		}
	}
	inRepo(t, repo, "go", "mod", "tidy", "-diff")
	out := inRepo(t, repo, "go", "test", "-count=1", "-v", "-run", "^(TestFileLayout|TestGeneratedOnlyPublic|TestMethodLayering|TestMethod)$", "./internal/")
	for _, name := range []string{"TestFileLayout", "TestGeneratedOnlyPublic", "TestMethodLayering", "TestMethod"} {
		if !strings.Contains(out, "--- PASS: "+name) {
			t.Errorf("%s did not pass at birth:\n%s", name, out)
		}
	}
	for _, target := range []string{"gen-models-check", "gen-tests"} {
		inRepo(t, repo, "make", target)
	}
}

// A conventional component passes every command check of its construction task, and its
// integration task's generators run. Then each way of breaking the convention turns a gate red.
func TestFreshComponentPassesItsGate(t *testing.T) {
	repo := builtRepo(t)
	construct := taskSpec(t, repo, "construction")
	if !slices.Contains(construct.Contracts, freshContract) || slices.Contains(construct.Contracts, "callerAccess") {
		t.Fatalf("the job must generate this component's contract and no sibling's: %v", construct.Contracts)
	}
	statuses, logs := gate(t, repo, commandChecks(t, construct))
	requirePassed(t, statuses, logs)
	for _, want := range []string{"command:arch", "command:tidy", "command:gen-models-check", "command:build", "command:vet", "command:gofmt"} {
		if _, ok := statuses[want]; !ok {
			t.Errorf("the construction gate has no %s: %v", want, statuses)
		}
	}

	integration := taskSpec(t, repo, "integration")
	inRepo(t, repo, "make", "gen-tests")
	statuses, logs = gate(t, repo, commandChecks(t, integration, "command:gen-tests-check"))
	requirePassed(t, statuses, logs)
}

// The gate must be able to go red: nothing else detects it going vacuous again. Each case
// breaks one convention in a copy of the built tree.
func TestFreshGateCatchesBrokenConventions(t *testing.T) {
	built := builtRepo(t)
	const arch = "command:arch"
	for _, tc := range []struct {
		name  string
		check string
		wants *regexp.Regexp
		make  func(t *testing.T, repo string)
	}{
		{"a rogue package outside the layers", arch, regexp.MustCompile(`domain/rogue`), func(t *testing.T, repo string) {
			writeRepoFile(t, repo, "internal/domain/rogue/rogue.go", []byte("// Package rogue is outside the layer model.\npackage rogue\n"))
		}},
		{"a rogue package and no other code", arch, regexp.MustCompile(`domain/rogue`), func(t *testing.T, repo string) {
			inRepo(t, repo, "git", "rm", "-rqf", "internal/resourceaccess")
			writeRepoFile(t, repo, "internal/domain/rogue/rogue.go", []byte("// Package rogue is the only code here.\npackage rogue\n"))
		}},
		{"a misnamed file", arch, regexp.MustCompile(`helpers\.go`), func(t *testing.T, repo string) {
			writeRepoFile(t, repo, freshPackage+"/helpers.go", []byte("package todolist\n\nfunc helper() {}\n\nvar _ = helper\n"))
		}},
		{"a hand-exported symbol", arch, regexp.MustCompile(`Open`), func(t *testing.T, repo string) {
			writeRepoFile(t, repo, freshPackage+"/todolistaccess.go", []byte(todoListImpl+"\n// Open is hand-exported.\nfunc Open() {}\n"))
		}},
		{"an import off the menu", arch, regexp.MustCompile(`golang\.org/x/sync`), func(t *testing.T, repo string) {
			src := strings.Replace(todoListImpl, `"context"`, "\"context\"\n\n\t_ \"golang.org/x/sync/errgroup\"", 1)
			writeRepoFile(t, repo, freshPackage+"/todolistaccess.go", []byte(src))
			inRepo(t, repo, "go", "mod", "tidy")
		}},
		{"the seated gate deleted", arch, regexp.MustCompile(`no Go files`), func(t *testing.T, repo string) {
			inRepo(t, repo, "git", "rm", "-qf", "internal/aiarch_arch_test.go")
		}},
		{"the seated gate gutted", arch, regexp.MustCompile(`no test ran`), func(t *testing.T, repo string) {
			writeRepoFile(t, repo, "internal/aiarch_arch_test.go", []byte("package internal_test\n"))
		}},
		{"a local replace", arch, regexp.MustCompile(`local path`), func(t *testing.T, repo string) {
			f, err := os.OpenFile(filepath.Join(repo, "go.mod"), os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			if _, err := f.WriteString("\nreplace example.com/nothing => ../nothing\n"); err != nil {
				t.Fatal(err)
			}
		}},
		{"an untidy module", "command:tidy", regexp.MustCompile(`.`), func(t *testing.T, repo string) {
			inRepo(t, repo, "sh", "-c", "grep -v 'pgx/v5' go.mod > go.mod.new && mv go.mod.new go.mod")
		}},
		{"a hand-edited generated file", "command:gen-models-check", regexp.MustCompile(`contract\.gen\.go`), func(t *testing.T, repo string) {
			p := filepath.Join(repo, freshPackage, "contract.gen.go")
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append(b, []byte("\n// edited\n")...), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a completed activity whose package is missing", arch, regexp.MustCompile(`ALIGN-MISSING-PKG`), func(t *testing.T, repo string) {
			// The real row shape: the sibling finished, and the design still names its package,
			// so its code is owed. Until the row says so the same absence is tolerated.
			p := filepath.Join(repo, ".aiarch/state/project.json")
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			doc["activityExecution"].(map[string]any)["C-caller-access"].(map[string]any)["completedAt"] = "2026-10-31T17:34:48Z"
			out, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, out, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"a component that skipped generation", "command:gen-models-check", regexp.MustCompile(`missing:`), func(t *testing.T, repo string) {
			inRepo(t, repo, "git", "rm", "-qf", freshPackage+"/contract.gen.go")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			inRepo(t, built, "git", "clone", "-q", built, repo)
			spec := taskSpec(t, repo, "construction")
			tc.make(t, repo)
			statuses, logs := gate(t, repo, commandChecks(t, spec, tc.check))
			if statuses[tc.check] == checkPass {
				t.Fatalf("%s stayed green:\n%s", tc.check, logs[tc.check])
			}
			if !tc.wants.MatchString(logs[tc.check]) {
				t.Errorf("%s went red for the wrong reason, want %s:\n%s", tc.check, tc.wants, logs[tc.check])
			}
		})
	}
}

// Sibling activities each add a require. The seated .gitattributes keeps that from being a
// text conflict; the tidy afterwards is what the merge tail folds into the merge commit.
func TestFreshSiblingModuleMergeSettles(t *testing.T) {
	repo := bornRepo(t, freshState(t))
	addImport := func(branch, file, importPath string) {
		inRepo(t, repo, "git", "checkout", "-q", "-b", branch, "main")
		writeRepoFile(t, repo, file, []byte("// Package x imports a sanctioned driver.\npackage x\n\nimport _ \""+importPath+"\"\n"))
		inRepo(t, repo, "go", "mod", "tidy")
		inRepo(t, repo, "git", "add", "-A")
		inRepo(t, repo, "git", "commit", "-q", "-m", branch)
	}
	addImport("activity/a", "internal/resourceaccess/a/aaccess.go", "github.com/jackc/pgx/v5/pgxpool")
	addImport("activity/b", "internal/resourceaccess/b/baccess.go", "github.com/google/uuid")
	inRepo(t, repo, "git", "checkout", "-q", "main")
	inRepo(t, repo, "git", "merge", "-q", "--no-ff", "-m", "a", "activity/a")
	inRepo(t, repo, "git", "merge", "-q", "--no-ff", "-m", "b", "activity/b")
	// The merge tail's sequence (agenticjob tidyMergedModule). A plain tidy is not enough: it
	// keeps a union merge's unsorted go.sum when the set of lines is already right.
	if err := os.Remove(filepath.Join(repo, "go.sum")); err != nil {
		t.Fatal(err)
	}
	inRepo(t, repo, "go", "mod", "tidy")
	inRepo(t, repo, "go", "mod", "tidy", "-diff")
	inRepo(t, repo, "go", "build", "./internal/...")
}

// The prompt names the approved infra. The menu has one source, so the prompt cannot drift.
func TestDetailedDesignPromptNamesTheApprovedMenu(t *testing.T) {
	rendered, err := modelgenMenuInPrompt()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range modelgen.ApprovedInfra() {
		if !strings.Contains(rendered, "`"+name+"`") {
			t.Errorf("service-detailed-design does not name approved infra %s", name)
		}
	}
}

func modelgenMenuInPrompt() (string, error) {
	return methodassets.RenderCommand("service-detailed-design", []string{freshComponent, freshActivity})
}

// RUN-007's attempts 7 and 8 of one task failed identically in two different temp worktrees.
// The fingerprint exists to see that, so the digest must not be fooled by the temp path; and a
// different failure must still read as different.
func TestFailureDigestSeesThroughRunNoise(t *testing.T) {
	logsOf := func(dir string) ([]CheckResult, func(CheckResult) string) {
		entries, err := os.ReadDir(filepath.Join("testdata", "fresh", "run007-logs", dir))
		if err != nil {
			t.Fatal(err)
		}
		logs := map[string]string{}
		var results []CheckResult
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join("testdata", "fresh", "run007-logs", dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			id := strings.TrimSuffix(e.Name(), ".log")
			logs[id] = string(b)
			results = append(results, CheckResult{CheckId: id, Status: checkFail})
		}
		return results, func(r CheckResult) string { return logs[r.CheckId] }
	}
	r7, log7 := logsOf("attempt7")
	r8, log8 := logsOf("attempt8")
	d7, d8 := failureDigest(r7, log7), failureDigest(r8, log8)
	if d7 == "" || d7 != d8 {
		t.Fatalf("RUN-007's identical failures must digest alike: %q vs %q", d7, d8)
	}
	different := func(r CheckResult) string { return log7(r) + "\nundefined: somethingNew\n" }
	if failureDigest(r7, different) == d7 {
		t.Error("a different failure must digest differently")
	}
	if got := failureDigest([]CheckResult{{CheckId: "a", Status: checkPass}}, log7); got != "" {
		t.Errorf("a green run has no digest, got %q", got)
	}
}

func TestScrubLogRemovesRunNoise(t *testing.T) {
	a := scrubLog("FAIL pkg (1.234s) at 2026-10-31T17:34:48Z /tmp/x/wt/a.go:3 0xc000123 127.0.0.1:5432 took 12ms")
	b := scrubLog("FAIL pkg (9.1s) at 2026-11-01T01:02:03Z /var/y/wt/a.go:3 0xc000abc 127.0.0.1:60000 took 3ms")
	if a != b {
		t.Fatalf("scrubbed logs differ:\n%s\n%s", a, b)
	}
}
