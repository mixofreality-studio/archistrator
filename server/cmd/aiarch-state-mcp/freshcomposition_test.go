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

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	freshManagerActivity = "C-list-manager"
	freshMachineActivity = "C-machine-api"
	freshSurfaceActivity = "U-SPA-web-app"
)

// listManagerContract is the Manager's contract as a designer writes it: no deps. Recording it
// through the verb derives them from the System's edges.
const listManagerContract = `{
 "component": "listManager", "layer": "Manager", "goPackage": "internal/manager/list", "title": "list",
 "$defs": {"OwnerName": {"type": "string"}},
 "interface": {"name": "ListManager", "layer": "manager", "operations": [
   {"name": "TakeStock", "params": [{"name": "owner", "schema": {"$ref": "#/$defs/OwnerName"}}], "result": {"$ref": "#/$defs/OwnerName"}, "error": true}
 ]}
}`

// The Manager's other two kinds of dep: an Engine and a Utility that binds no infrastructure.
const rulesEngineContract = `{
 "component": "rulesEngine", "layer": "Engine", "goPackage": "internal/engine/rules", "title": "rules",
 "$defs": {"Verdict": {"type": "string"}},
 "interface": {"name": "RulesEngine", "layer": "engine", "operations": [{"name": "Judge", "params": [], "result": {"$ref": "#/$defs/Verdict"}, "error": true}]}
}`

const clockContract = `{
 "component": "clock", "layer": "Utility", "goPackage": "internal/utility/clock", "title": "clock",
 "$defs": {"Instant": {"type": "string"}},
 "interface": {"name": "Clock", "layer": "utility", "operations": [{"name": "Now", "params": [], "result": {"type": "integer"}, "error": false}]}
}`

const rulesEngineImpl = `// Package rules is the rules engine.
package rules

import fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

type engine struct{}

func newRulesEngine() RulesEngine { return engine{} }

func (engine) Judge(fweng.Context) (Verdict, error) { return "", nil }
`

const clockImpl = `// Package clock is the clock utility.
package clock

import fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

type clock struct{}

func newClock() Clock { return clock{} }

func (clock) Now(fwra.Context) int64 { return 0 }
`

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
	"go.temporal.io/sdk/client"

	"example/internal/engine/rules"
	"example/internal/resourceaccess/caller"
	"example/internal/resourceaccess/todolist"
	"example/internal/utility/clock"
)

type listManager struct {
	client client.Client
	caller caller.CallerAccess
	todos  todolist.TodoListAccess
	rules  rules.RulesEngine
	clock  clock.Clock
}

func newListManager(c client.Client, caller caller.CallerAccess, todos todolist.TodoListAccess, rules rules.RulesEngine, clock clock.Clock) *listManager {
	return &listManager{client: c, caller: caller, todos: todos, rules: rules, clock: clock}
}

func (*listManager) TakeStock(_ manager.Context, owner OwnerName) (OwnerName, error) {
	return owner, nil
}

// WorkerManifest supplies this Manager's workflows to the generated RegisterManagerWorker.
func (m *listManager) WorkerManifest() genWorkerManifest {
	return genWorkerManifest{Activities: genActivities{CallerAccess: m.caller, TodoListAccess: m.todos, Clock: m.clock}}
}
`

// composedState is the fresh state with the Manager and machine client contracts recorded. The
// Manager's goes through the verb, which derives its constructor deps from the System.
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
	contracts["rulesEngine"] = json.RawMessage(rulesEngineContract)
	contracts["clock"] = json.RawMessage(clockContract)
	raw, err := json.Marshal(contracts)
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = raw
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return withBrowserContracts(t, withRecordedManager(t, withManagerDeps(t, out)))
}

// withRecordedManager records listManagerContract through the verb, as the Manager's detailed
// design does.
func withRecordedManager(t *testing.T, state []byte) []byte {
	t.Helper()
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(state, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := seedProject(t, proj, jobModeConstruct, 0)
	s.ActivityID, s.ComponentID = freshManagerActivity, "list-manager"
	var contract map[string]any
	if err := json.Unmarshal([]byte(listManagerContract), &contract); err != nil {
		t.Fatal(err)
	}
	if err := s.recordServiceContract(contract); err != nil {
		t.Fatalf("the Manager's contract: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(s.StateRoot, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// recordClientContracts derives every Client's contract into contracts.
func recordClientContracts(t *testing.T, proj *projectstate.Project, sys *projectstate.System, contracts map[string]json.RawMessage) {
	t.Helper()
	for _, c := range sys.Components {
		if c.Kind != projectstate.CompClient || c.UiSurface == nil || !*c.UiSurface {
			continue
		}
		if err := projectstate.RecordBrowserContract(proj, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := projectstate.RecordMachineClientContracts(proj); err != nil {
		t.Fatal(err)
	}
	for key, sc := range proj.ServiceContracts {
		if strings.EqualFold(sc.Layer, "client") {
			raw, err := json.Marshal(sc)
			if err != nil {
				t.Fatal(err)
			}
			contracts[key] = raw
		}
	}
}

// withManagerDeps draws the Manager's Engine and Utility into the System: the components, their
// edges, and a call to each in a step the Manager already works in, so every operation is reached.
func withManagerDeps(t *testing.T, state []byte) []byte {
	t.Helper()
	var top map[string]any
	if err := json.Unmarshal(state, &top); err != nil {
		t.Fatal(err)
	}
	system := top["slots"].(map[string]any)["5"].(map[string]any)["model"].(map[string]any)
	system["components"] = append(system["components"].([]any),
		map[string]any{"id": "rules-engine", "name": "RulesEngine", "kind": "engine", "layer": "engine", "encapsulates": "list rules", "encapsulatesVolatilities": []any{}, "atomicBusinessVerbs": []any{"judge"}, "contractKey": "rulesEngine"},
		map[string]any{"id": "clock", "name": "Clock", "kind": "utility", "layer": "utility", "encapsulates": "the time", "atomicBusinessVerbs": []any{}, "contractKey": "clock"})
	system["relationships"] = append(system["relationships"].([]any),
		map[string]any{"from": "list-manager", "to": "rules-engine", "mode": "sync", "label": "Judge"},
		map[string]any{"from": "list-manager", "to": "clock", "mode": "sync", "label": "Now"})
	for _, v := range system["dynamicViews"].([]any) {
		for _, st := range v.(map[string]any)["steps"].([]any) {
			step := st.(map[string]any)
			for _, c := range step["calls"].([]any) {
				if c.(map[string]any)["to"] == "list-manager" {
					step["calls"] = append(step["calls"].([]any),
						map[string]any{"from": "list-manager", "to": "rules-engine", "mode": "sync", "label": "Judge"},
						map[string]any{"from": "list-manager", "to": "clock", "mode": "sync", "label": "Now"})
					out, err := json.Marshal(top)
					if err != nil {
						t.Fatal(err)
					}
					return out
				}
			}
		}
	}
	t.Fatal("no view calls the Manager")
	return nil
}

// conformClientDesign states on a recorded project what the design rules now ask of a Client: an
// actor's call into a browser surface leads with its operation, a built machine client names its
// transport and is planned as one, and no relationship goes uncalled. The corpus stays verbatim;
// a recorded run predates these rules.
func conformClientDesign(t *testing.T, state []byte) []byte {
	t.Helper()
	var top map[string]any
	if err := json.Unmarshal(state, &top); err != nil {
		t.Fatal(err)
	}
	slots, _ := top["slots"].(map[string]any)
	system, _ := slots["5"].(map[string]any)["model"].(map[string]any)
	conformClients(system)
	if plan, ok := slots["9"].(map[string]any); ok {
		for _, a := range plan["model"].(map[string]any)["activities"].([]any) {
			item := a.(map[string]any)
			if strings.HasPrefix(item["name"].(string), "C-") && machineClientID(system, item["componentId"]) {
				item["machineClient"] = true
			}
		}
	}
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// conformClients states each machine client's transport, leads each actor call into a browser
// surface with its operation, and drops the relationships no view calls.
func conformClients(system map[string]any) {
	surface := map[string]bool{}
	for _, c := range system["components"].([]any) {
		comp := c.(map[string]any)
		if comp["kind"] != "client" {
			continue
		}
		if ui, _ := comp["uiSurface"].(bool); ui {
			surface[comp["id"].(string)] = true
		} else if comp["buildStatus"] == nil {
			comp["machineProtocol"] = "mcp"
		}
	}
	called := map[string]bool{}
	for _, v := range system["dynamicViews"].([]any) {
		view := v.(map[string]any)
		for _, st := range view["steps"].([]any) {
			for _, c := range st.(map[string]any)["calls"].([]any) {
				call := c.(map[string]any)
				called[call["from"].(string)+">"+call["to"].(string)] = true
				if surface[call["to"].(string)] {
					call["label"] = useCaseOp(view["useCaseId"].(string)) + " — " + call["label"].(string)
				}
			}
		}
	}
	var kept []any
	for _, r := range system["relationships"].([]any) {
		rel := r.(map[string]any)
		if called[rel["from"].(string)+">"+rel["to"].(string)] {
			kept = append(kept, rel)
		}
	}
	system["relationships"] = kept
}

func machineClientID(system map[string]any, id any) bool {
	for _, c := range system["components"].([]any) {
		comp := c.(map[string]any)
		if comp["id"] == id && comp["kind"] == "client" {
			ui, _ := comp["uiSurface"].(bool)
			return !ui
		}
	}
	return false
}

// useCaseOp is a use case's id as an exported operation name.
func useCaseOp(id string) string {
	var b strings.Builder
	for word := range strings.SplitSeq(id, "-") {
		if word != "" {
			b.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
	}
	return b.String()
}

// withBrowserContracts records every Client's derived contract, as recording a surface's UI design
// and a Manager's contract do, so the fixture's plan reaches an integration with scenarios.
func withBrowserContracts(t *testing.T, state []byte) []byte {
	t.Helper()
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(state, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	sys, ok := proj.SystemDesign.Model.(*projectstate.System)
	if !ok {
		t.Fatal("no System")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(state, &top); err != nil {
		t.Fatal(err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		t.Fatal(err)
	}
	recordClientContracts(t, &proj, sys, contracts)
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

// requireNothingLeftBehind holds a gate to leaving a clean checkout. Agents commit with
// `git add -A`, so anything a check drops in the tree lands in their commit and fails their own
// edit scope: a built server binary, a compiler's scratch.
func requireNothingLeftBehind(t *testing.T, repo string) {
	t.Helper()
	if left := inRepo(t, repo, "git", "status", "--porcelain"); strings.TrimSpace(left) != "" {
		t.Errorf("the gate left files in the checkout:\n%s", left)
	}
}

// composedRepo has both resource accesses and the Manager built, committed as a passing
// construction leaves them: the job generated each contract, the verb tidied.
func composedRepo(t *testing.T) string {
	t.Helper()
	repo := bornRepo(t, composedState(t))
	module := strings.TrimSpace(inRepo(t, repo, "go", "list", "-m"))
	inRepo(t, repo, "make", "gen-models", "CONTRACTS="+freshContract+" callerAccess listManager rulesEngine clock")
	writeRepoFile(t, repo, freshPackage+"/todolistaccess.go", []byte(todoListImpl))
	writeRepoFile(t, repo, "internal/engine/rules/rulesengine.go", []byte(rulesEngineImpl))
	writeRepoFile(t, repo, "internal/utility/clock/clockutility.go", []byte(clockImpl))
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

	requireNothingLeftBehind(t, repo)

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

// A machine client takes the machineClient lifecycle and has a Go package; a browser surface takes the
// frontend lifecycle. Classification reads the design, not the activity's name.
func TestFreshClientsTakeTheirDesignedLifecycle(t *testing.T) {
	repo := bornRepo(t, composedState(t))
	if got := activitySpec(t, repo, freshMachineActivity, "stp").Identity.Command; got != "service-test-plan" {
		t.Errorf("machine-api's plan takes %q", got)
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

	requireNothingLeftBehind(t, repo)

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

// A browser surface's contract comes from the design: its operations are the names the actor
// calls lead with, it lands where the plan reads it, and a re-record follows a changed design.
func TestFreshBrowserContractIsDerivedFromItsLabels(t *testing.T) {
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(composedState(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	const surface = "web-app"
	requireDerivedShape(t, proj.ServiceContracts["webApp"])
	scenarios, err := projectstate.ComponentScenarios(&proj, "webApp")
	if err != nil || len(scenarios) == 0 {
		t.Fatalf("the surface's scenarios: %d, %v", len(scenarios), err)
	}
	if _, err := projectstate.TaskSpecFor(&proj, freshSurfaceActivity, "integration"); err != nil {
		t.Errorf("a surface with a derived contract reaches its integration: %v", err)
	}

	sys, _ := proj.SystemDesign.Model.(*projectstate.System)
	rewriteActorLabels(sys, surface, "OnlyOne | AndAnother — prose")
	if err := projectstate.RecordBrowserContract(&proj, surface); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, op := range proj.ServiceContracts["webApp"].Interface.Operations {
		got = append(got, op.Name)
	}
	if !slices.Equal(got, []string{"OnlyOne", "AndAnother"}) {
		t.Errorf("a re-record must rewrite the contract, got %v", got)
	}

	rewriteActorLabels(sys, surface, "a prose label")
	if err := projectstate.RecordBrowserContract(&proj, surface); err == nil || !strings.Contains(err.Error(), "must lead with exported operation names") {
		t.Errorf("a prose label names no operation: %v", err)
	}
	if err := projectstate.RecordBrowserContract(&proj, "machine-api"); err == nil {
		t.Error("only a browser surface has a derived contract")
	}
}

func requireDerivedShape(t *testing.T, contract projectstate.ServiceContract) {
	t.Helper()
	if contract.Layer != "Client" || contract.GoPackage != "" || len(contract.Interface.Operations) == 0 {
		t.Fatalf("the derived contract is a Client's with no Go package and some operations: %+v", contract)
	}
	for _, op := range contract.Interface.Operations {
		if len(op.Params) != 1 || op.Params[0].Name != "action" {
			t.Errorf("%s must take the one web-dialect action param: %+v", op.Name, op.Params)
		}
	}
}

func rewriteActorLabels(sys *projectstate.System, surface, label string) {
	for i := range sys.DynamicViews {
		for j := range sys.DynamicViews[i].Steps {
			for k := range sys.DynamicViews[i].Steps[j].Calls {
				if call := &sys.DynamicViews[i].Steps[j].Calls[k]; call.To == surface {
					call.Label = label
				}
			}
		}
	}
}

// With no contract, a browser surface's integration has nothing to prove, and an empty gate must
// not pass: its spec is refused, naming the way out.
func TestFreshBrowserSurfaceWithNoScenariosHasNoSpec(t *testing.T) {
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(composedState(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	delete(proj.ServiceContracts, "webApp")
	if _, err := projectstate.TaskSpecFor(&proj, freshSurfaceActivity, "integration"); err == nil || !strings.Contains(err.Error(), "no scenario to integrate against") {
		t.Errorf("got %v", err)
	}
	if _, err := projectstate.TaskSpecFor(&proj, freshSurfaceActivity, "construction"); err != nil {
		t.Errorf("only integration owes scenarios: %v", err)
	}
}

// A Client authors no contract, whatever layer the agent claims for it.
func TestRecordServiceContractRefusesAClient(t *testing.T) {
	repo := bornRepo(t, composedState(t))
	raw, err := os.ReadFile(filepath.Join(repo, ".aiarch/state/project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(raw, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		component string
		contract  projectstate.ServiceContract
	}{
		{"machine-api", projectstate.ServiceContract{Layer: "Client"}},
		{"machine-api", projectstate.ServiceContract{Layer: "Manager"}},
		{"web-app", projectstate.ServiceContract{Layer: "Utility"}},
		{"list-manager", projectstate.ServiceContract{Layer: "client"}},
	} {
		if err := refuseClientContract(proj, tc.component, tc.contract); err == nil || !strings.Contains(err.Error(), "authors no contract") {
			t.Errorf("%s as %s: %v", tc.component, tc.contract.Layer, err)
		}
	}
	if err := refuseClientContract(proj, "list-manager", projectstate.ServiceContract{Layer: "Manager"}); err != nil {
		t.Error(err)
	}
}

// Every lifecycle task of every planned activity resolves on a fresh project, including the ones
// no other test here reaches.
func TestFreshEveryPlannedTaskResolves(t *testing.T) {
	repo := bornRepo(t, composedState(t))
	raw, err := os.ReadFile(filepath.Join(repo, ".aiarch/state/project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(raw, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Slots map[string]struct {
			Model struct {
				Activities []projectstate.ActivityItem `json:"activities"`
			} `json:"model"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	planned := doc.Slots["9"].Model.Activities
	if len(planned) == 0 {
		t.Fatal("the fixture plans no activity")
	}
	for _, a := range planned {
		typ, variant, err := projectstate.ClassifyActivity(a.Name, a.WorkerClass, a.Coding, a.UISurface, a.MachineClient)
		if err != nil {
			t.Errorf("%s is unclassified: %v", a.Name, err)
			continue
		}
		lc, ok := methodassets.LifecycleFor(projectstate.LifecycleKeyFor(typ, variant))
		if !ok {
			t.Errorf("%s has no lifecycle", a.Name)
			continue
		}
		for _, task := range lc.Tasks {
			if _, err := projectstate.TaskSpecFor(&proj, a.Name, task.ID); err != nil {
				t.Errorf("%s/%s: %v", a.Name, task.ID, err)
			}
		}
	}
}

// A machine client is classified by the plan's own field, and only then.
func TestClassifyMachineClient(t *testing.T) {
	typ, _, err := projectstate.ClassifyActivity("C-web-api", "junior-developer", true, false, true)
	if err != nil || typ != projectstate.ActivityTypeMachineClient {
		t.Errorf("got %v, %v", typ, err)
	}
	typ, _, err = projectstate.ClassifyActivity("C-web-api", "junior-developer", true, false, false)
	if err != nil || typ != projectstate.ActivityTypeService {
		t.Errorf("with no flag it is a service: %v, %v", typ, err)
	}
	lc, ok := methodassets.LifecycleFor(projectstate.LifecycleKeyFor(projectstate.ActivityTypeMachineClient, 0))
	if !ok || lc.Type != "machineClient" {
		t.Errorf("no machineClient lifecycle: %+v", lc)
	}
}

// A machine client binds a plan, so its testing review judges a scenario run through it like any
// component's, and nothing is proved without one. It designs and constructs nothing: both are generated.
func TestMachineClientLifecycleBindsAPlan(t *testing.T) {
	lc, ok := methodassets.LifecycleFor("machineClient")
	if !ok {
		t.Fatal("no machineClient lifecycle")
	}
	var ids []string
	for _, task := range lc.Tasks {
		ids = append(ids, task.ID)
	}
	if !slices.Equal(ids, []string{"stp", "stpReview", "integration", "testing"}) {
		t.Errorf("tasks = %v", ids)
	}
	if !slices.ContainsFunc(lc.Tasks, func(task methodassets.LifecycleTask) bool { return task.ArtifactKind == "STP" }) {
		t.Error("a machine client binds a test plan")
	}
}

// Recording a surface's UI design through the verb writes its contract in the same mutation, so
// the plan after it has operations to bind.
func TestRecordingAUIDesignDerivesTheSurfaceContract(t *testing.T) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(composedState(t), &top); err != nil {
		t.Fatal(err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		t.Fatal(err)
	}
	delete(contracts, "webApp")
	raw, err := json.Marshal(contracts)
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = raw
	state, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(state, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := seedProject(t, proj, jobModeConstruct, 0)
	s.ActivityID, s.ComponentID = freshSurfaceActivity, "web-app"
	if err := s.recordPhaseArtifact("web-app", map[string]any{"UIDesign": map[string]any{"component": "web-app", "content": "the concept"}}); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	requireDerivedShape(t, got.ServiceContracts["webApp"])
}
