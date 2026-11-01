package main

// freshcomposition_test.go carries a fresh project past its first Manager: the composition root
// the job generates must really compile against a hand-written Manager, a machine client must
// land without a design package of its own, and a browser surface must pass its gate on the real
// npm toolchain. Each generated file comes from the task's own spec, through the same `go tool`
// run the job makes, and every changed path is held to the task's own edit scope.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	freshManagerActivity = "C-list-manager"
	freshMachineActivity = "C-machine-api"
	freshSurfaceActivity = "U-SPA-web-app"
)

const listManagerContract = `{
 "component": "listManager", "layer": "Manager", "goPackage": "internal/manager/list", "title": "list",
 "deps": [{"name": "todos", "component": "todoListAccess"}, {"name": "caller", "component": "callerAccess"}],
 "$defs": {"OwnerName": {"type": "string"}},
 "interface": {"name": "ListManager", "layer": "manager", "operations": [
   {"name": "TakeStock", "params": [{"name": "owner", "schema": {"$ref": "#/$defs/OwnerName"}}], "result": {"$ref": "#/$defs/OwnerName"}, "error": true}
 ]}
}`

const machineAPIContract = `{
 "component": "machineApi", "layer": "Client", "goPackage": "internal/client/machineapi", "title": "machine api",
 "interface": {"name": "MachineApi", "layer": "client", "operations": []}
}`

const callerImpl = `// Package caller is the caller resource access.
package caller

import (
	"net/http"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

type httpAccess struct {
	baseURL string
	client  *http.Client
}

func newExternalHTTPCallerAccess(baseURL string, client *http.Client) (CallerAccess, error) {
	return &httpAccess{baseURL: baseURL, client: client}, nil
}

func (*httpAccess) EstablishListOwner(fwra.Context, CallerProof) (ListOwner, error) {
	return "", nil
}
`

const listManagerImpl = `// Package list is the list manager.
package list

import (
	"github.com/mixofreality-studio/archistrator-platform/framework-go/manager"

	"example/internal/resourceaccess/caller"
	"example/internal/resourceaccess/todolist"
)

type listManager struct {
	todos  todolist.TodoListAccess
	caller caller.CallerAccess
}

func newListManager(todos todolist.TodoListAccess, caller caller.CallerAccess) *listManager {
	return &listManager{todos: todos, caller: caller}
}

func (*listManager) TakeStock(_ manager.Context, owner OwnerName) (OwnerName, error) {
	return owner, nil
}

// WorkerManifest supplies this Manager's workflows to the generated RegisterManagerWorker.
func (m *listManager) WorkerManifest() genWorkerManifest {
	return genWorkerManifest{Activities: genActivities{Todos: m.todos, Caller: m.caller}}
}
`

// composedState is the fresh state with the Manager and machine client contracts recorded.
func composedState(t *testing.T) []byte {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(freshState(t), &top); err != nil {
		t.Fatal(err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		t.Fatal(err)
	}
	contracts["listManager"] = json.RawMessage(listManagerContract)
	contracts["machineApi"] = json.RawMessage(machineAPIContract)
	raw, err := json.Marshal(contracts)
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = raw
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// runGenerators runs the spec's generators exactly as the job does: `go tool` in the module.
func runGenerators(t *testing.T, repo string, spec projectstate.TaskSpec) {
	t.Helper()
	if len(spec.Generate) == 0 {
		t.Fatalf("%s/%s generates nothing", spec.Activity, spec.Task)
	}
	for _, g := range spec.Generate {
		inRepo(t, repo, g.Argv...)
	}
}

// requireInScope holds every path the task changed to the task's edit scope.
func requireInScope(t *testing.T, repo string, spec projectstate.TaskSpec) {
	t.Helper()
	globs := slices.Clone(spec.EditScope.Extra)
	for _, c := range spec.EditScope.Components {
		globs = append(globs, c.Paths...)
	}
	inRepo(t, repo, "git", "add", "-A")
	for p := range strings.FieldsSeq(inRepo(t, repo, "git", "diff", "--cached", "--name-only")) {
		if !slices.ContainsFunc(globs, func(g string) bool { return projectstate.EditScopeGlobMatch(g, p) }) {
			t.Errorf("%s/%s wrote %s, which its edit scope does not admit: %v", spec.Activity, spec.Task, p, globs)
		}
	}
}

// composedRepo has both resource accesses and the Manager built, committed as a passing
// construction leaves them: the job generated each contract, the verb tidied.
func composedRepo(t *testing.T) string {
	t.Helper()
	repo := bornRepo(t, composedState(t))
	module := strings.TrimSpace(inRepo(t, repo, "go", "list", "-m"))
	inRepo(t, repo, "make", "gen-models", "CONTRACTS="+freshContract+" callerAccess listManager")
	writeRepoFile(t, repo, freshPackage+"/todolistaccess.go", []byte(todoListImpl))
	writeRepoFile(t, repo, "internal/resourceaccess/caller/calleraccess.go", []byte(callerImpl))
	runGenerators(t, repo, activitySpec(t, repo, freshManagerActivity, "construction"))
	writeRepoFile(t, repo, "internal/manager/list/listmanager.go", []byte(strings.ReplaceAll(listManagerImpl, `"example/`, `"`+module+`/`)))
	// The resource accesses' own integration tasks have already landed their tests.
	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "build the components")
	return repo
}

// The composition root a Manager's integration generates compiles against the real Manager, its
// checks pass, and everything it wrote is in scope. This is what had never been compiled.
func TestFreshManagerIntegrationComposesTheServer(t *testing.T) {
	repo := composedRepo(t)
	spec := activitySpec(t, repo, freshManagerActivity, "integration")
	runGenerators(t, repo, spec)
	inRepo(t, repo, "make", "gen-tests")
	for _, want := range []string{
		"cmd/server/main.gen.go", "cmd/server/config.gen.go", "cmd/server/hooks.go",
		"internal/client/web/server.gen.go", "internal/client/web/list/list_handlers.gen.go",
		"internal/client/machineapi/list/list_tools.gen.go", "api/openapi.yaml",
	} {
		if _, err := os.Stat(filepath.Join(repo, want)); err != nil {
			t.Errorf("the job did not write %s: %v", want, err)
		}
	}
	inRepo(t, repo, "go", "mod", "tidy")
	requireInScope(t, repo, spec)
	inRepo(t, repo, "git", "commit", "-q", "-m", "integrate the manager")
	statuses, logs := gate(t, repo, commandChecks(t, spec))
	requirePassed(t, statuses, logs)
	for _, want := range []string{"command:build-server", "command:gen-clients-check", "command:gen-config-check", "command:gen-compose-check", "command:arch", "command:tidy"} {
		if _, ok := statuses[want]; !ok {
			t.Errorf("the integration gate has no %s: %v", want, statuses)
		}
	}

	// The drift checks are real: a stale generated file is red.
	writeRepoFile(t, repo, "cmd/server/main.gen.go", []byte("package main\n"))
	statuses, _ = gate(t, repo, commandChecks(t, spec, "command:gen-compose-check", "command:build-server"))
	for _, id := range []string{"command:gen-compose-check", "command:build-server"} {
		if statuses[id] == checkPass {
			t.Errorf("%s stayed green over a stale main.gen.go", id)
		}
	}
}

// A resource access's integration leaves the server alone.
func TestFreshResourceIntegrationGeneratesNoServer(t *testing.T) {
	repo := bornRepo(t, composedState(t))
	spec := activitySpec(t, repo, freshActivity, "integration")
	if len(spec.Generate) != 0 {
		t.Errorf("a resource access must not generate the server: %+v", spec.Generate)
	}
	if slices.ContainsFunc(spec.Checks, func(c projectstate.SpecCheck) bool { return c.ID == "command:build-server" }) {
		t.Error("a resource access must not be held to a server that does not exist yet")
	}
}

// A machine client takes the service lifecycle and has a Go package; a browser surface takes the
// frontend lifecycle. Classification reads the design, not the activity's name.
func TestFreshClientsTakeTheirDesignedLifecycle(t *testing.T) {
	repo := bornRepo(t, composedState(t))
	if got := activitySpec(t, repo, freshMachineActivity, "construction").Identity.Command; got != "service-construction" {
		t.Errorf("machine-api takes %q", got)
	}
	spec := activitySpec(t, repo, freshSurfaceActivity, "construction")
	if spec.Identity.Command != "frontend-construction" {
		t.Errorf("web-app takes %q", spec.Identity.Command)
	}
	if !slices.Contains(spec.EditScope.Components[0].Paths, "webApp/src/web-app/**") || slices.Contains(spec.EditScope.Components[0].Paths, "webApp/src/workspace-embed/**") {
		t.Errorf("a surface owns its own directory only: %v", spec.EditScope.Components[0].Paths)
	}
}

// Finishing a browser surface must not turn every later arch gate red: its code is not a Go
// package. A machine client that finished with no package still is red.
func TestFreshFinishedBrowserSurfaceOwesNoPackage(t *testing.T) {
	repo := composedRepo(t)
	state := filepath.Join(repo, ".aiarch/state/project.json")
	raw, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	rows, _ := top["activityExecution"].(map[string]any)
	if rows == nil {
		rows = map[string]any{}
	}
	done := map[string]any{"completedAt": "2026-11-01T00:00:00Z"}
	finish := func(activities ...string) {
		for _, a := range activities {
			rows[a] = done
		}
		top["activityExecution"] = rows
		out, err := json.Marshal(top)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(state, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	finish(freshSurfaceActivity, "U-SPA-workspace-embed")
	arch := commandChecks(t, taskSpec(t, repo, "construction"), "command:arch")
	statuses, logs := gate(t, repo, arch)
	if statuses["command:arch"] != checkPass {
		t.Errorf("a finished browser surface reddened the gate:\n%s", logs["command:arch"])
	}
	finish(freshMachineActivity)
	statuses, logs = gate(t, repo, arch)
	if statuses["command:arch"] == checkPass || !strings.Contains(logs["command:arch"], "ALIGN-MISSING-PKG") {
		t.Errorf("a finished machine client with no package must be red: %s\n%s", statuses["command:arch"], logs["command:arch"])
	}
}

// A browser surface's construction on the real toolchain: the job bootstraps the web project,
// the verb installs it, the agent commits, and the gate's own npm ci, typecheck and lint pass.
// A layer violation then goes red, and a second surface adds only its own files.
func TestFreshBrowserSurfacePassesItsGate(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Fatal("npm is required: a browser surface's gate cannot be proven without it")
	}
	repo := composedRepo(t)
	integrate := activitySpec(t, repo, freshManagerActivity, "integration")
	runGenerators(t, repo, integrate)
	inRepo(t, repo, "go", "mod", "tidy")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "integrate the manager")

	spec := activitySpec(t, repo, freshSurfaceActivity, "construction")
	runGenerators(t, repo, spec)
	s := &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: freshSurfaceActivity, TaskID: "construction", ComponentID: "web-app", git: runGit}
	res, err := s.runChecks(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Note, "web project was installed") {
		t.Errorf("the verb must install and tell the agent to commit, note = %q", res.Note)
	}
	for _, f := range res.Results {
		t.Errorf("the verb's %s failed:\n%s", f.ID, f.Tail)
	}
	for _, want := range []string{"webApp/package-lock.json", "webApp/src/contracts/schema.ts", "webApp/src/api/ops.gen.ts", "webApp/web-app.html", "uitests/package.json"} {
		if _, err := os.Stat(filepath.Join(repo, want)); err != nil {
			t.Errorf("no %s: %v", want, err)
		}
	}
	requireInScope(t, repo, spec)
	if strings.Contains(inRepo(t, repo, "git", "diff", "--cached", "--name-only"), "node_modules") {
		t.Fatal("node_modules must be ignored by the seated .gitignore")
	}
	inRepo(t, repo, "git", "commit", "-q", "-m", "build the surface")
	statuses, logs := gate(t, repo, commandChecks(t, spec))
	requirePassed(t, statuses, logs)
	for _, want := range []string{"command:install", "command:typecheck", "command:lint", "command:gen-ops-check"} {
		if _, ok := statuses[want]; !ok {
			t.Errorf("the surface's gate has no %s: %v", want, statuses)
		}
	}

	// A route reaching the transport directly breaks the layering.
	writeRepoFile(t, repo, "webApp/src/web-app/routes/Bad.tsx", []byte("import type { JSX } from 'react';\nimport { useOpsClient } from '../../api/opsContext';\n\nexport function Bad(): JSX.Element {\n  useOpsClient();\n  return <main />;\n}\n"))
	statuses, _ = gate(t, repo, commandChecks(t, spec, "command:lint"))
	if statuses["command:lint"] == checkPass {
		t.Error("eslint stayed green over a route that imports the transport")
	}
	if err := os.Remove(filepath.Join(repo, "webApp/src/web-app/routes/Bad.tsx")); err != nil {
		t.Fatal(err)
	}

	second := activitySpec(t, repo, "U-SPA-workspace-embed", "construction")
	runGenerators(t, repo, second)
	requireInScope(t, repo, second)
	if strings.Contains(inRepo(t, repo, "git", "diff", "--cached", "--name-only"), "package.json") {
		t.Error("a second surface must keep the project's own files")
	}
}

// A machine client's code is all generated: once a Manager is integrated, its own construction
// and integration have nothing left to write, and both gates pass on what is committed. Finished,
// it owes the package that generation wrote.
func TestFreshMachineClientLandsOnGeneratedCode(t *testing.T) {
	repo := composedRepo(t)
	runGenerators(t, repo, activitySpec(t, repo, freshManagerActivity, "integration"))
	inRepo(t, repo, "go", "mod", "tidy")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "integrate the manager")

	construct := activitySpec(t, repo, freshMachineActivity, "construction")
	statuses, logs := gate(t, repo, commandChecks(t, construct))
	requirePassed(t, statuses, logs)

	integrate := activitySpec(t, repo, freshMachineActivity, "integration")
	runGenerators(t, repo, integrate)
	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "go", "mod", "tidy")
	requireInScope(t, repo, integrate)
	inRepo(t, repo, "git", "commit", "-q", "--allow-empty", "-m", "integrate the machine client")
	statuses, logs = gate(t, repo, commandChecks(t, integrate))
	requirePassed(t, statuses, logs)
}
