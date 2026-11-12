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
	"regexp"
	"slices"
	"strings"
	"testing"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	freshManagerActivity = "C-list-manager"
	freshMachineActivity = "C-todo-api"
	freshSurfaceActivity = "U-SPA-todo-web-app"
)

// composedState is the fresh state: the conformance fixture already holds every component's
// contract (Manager, Engines, Utility, ResourceAccess and the Clients' derived ones) and the
// test plans and UI design that bind them.
func composedState(t *testing.T) []byte {
	t.Helper()
	return freshState(t)
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

// serverContracts are the fixture's contracts the server-side activities construct, in the order a
// construction agent finishes them: a Manager's constructor takes the contracts it depends on.
const serverContracts = "todoListAccess messageBus accessGrantingEngine listRulingEngine listManager"

// composedRepo has every server-side component built (the fixture's own bodies) and committed
// as a passing construction leaves them: the job generated each contract, the verb tidied.
func composedRepo(t *testing.T) string {
	t.Helper()
	return composedRepoFrom(t, composedState(t))
}

// composedRepoFrom is composedRepo over a variant of the fixture's state.
func composedRepoFrom(t *testing.T, state []byte) string {
	t.Helper()
	repo := bornRepo(t, state)
	module := strings.TrimSpace(inRepo(t, repo, "go", "list", "-m"))
	inRepo(t, repo, "make", "gen-models", "CONTRACTS="+serverContracts)
	for _, path := range []string{
		"internal/resourceaccess/todolist/todolistaccess.go",
		"internal/utility/messagebus/messagebusutility.go",
		"internal/engine/accessgranting/accessgrantingengine.go",
		"internal/engine/listruling/listrulingengine.go",
	} {
		writeRepoFile(t, repo, path, []byte(fixtureImpl(t, path, module)))
	}
	runGenerators(t, repo, activitySpec(t, repo, freshManagerActivity, "construction"))
	for _, name := range []string{"listmanager", "add", "clear", "discard", "mark", "open", "reword", "toggle"} {
		path := "internal/manager/list/" + name + ".go"
		writeRepoFile(t, repo, path, []byte(fixtureImpl(t, path, module)))
	}
	// No generated scenario test yet: they import the composition's scenariocompose package, which
	// the Manager's integration generates, so every component's tests land after it.
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
	requireInScope(t, repo, spec)
	inRepo(t, repo, "git", "commit", "-q", "-m", "generate the composition")
	// The scenario tests are the job's, generated from the reviewed plans once the composition
	// exists; the agent's own changes (hooks.go, the module graph) are what the scope holds it to.
	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "generate the scenario tests")
	for _, want := range []string{
		"cmd/server/main.gen.go", "cmd/server/config.gen.go", "cmd/server/hooks.go",
		"cmd/server/authz.gen.go", "cmd/server/authz.gen_test.go", "cmd/server/policy.gen.go",
		"internal/client/web/server.gen.go", "internal/client/web/list/list_handlers.gen.go",
		"internal/client/todomcpserver/list/list_tools.gen.go", "api/openapi.yaml",
		"cmd/devstack/main.gen.go",
	} {
		if _, err := os.Stat(filepath.Join(repo, want)); err != nil {
			t.Errorf("the job did not write %s: %v", want, err)
		}
	}
	inRepo(t, repo, "go", "mod", "tidy")
	requireInScope(t, repo, spec)
	inRepo(t, repo, "git", "commit", "-q", "-m", "integrate the manager")
	statuses, logs := gate(t, repo, withSmoke(commandChecks(t, spec), spec))
	requirePassed(t, statuses, logs)
	for _, want := range []string{"command:build-server", "command:gen-clients-check", "command:gen-config-check", "command:gen-compose-check", "command:arch", "command:tidy", "command:build-devstack", freshPolicyCheck, "smoke:composition"} {
		if _, ok := statuses[want]; !ok {
			t.Errorf("the integration gate has no %s: %v", want, statuses)
		}
	}

	requireNothingLeftBehind(t, repo)
	requirePolicyRedOnAMismatch(t, repo, spec)
	// The machine client composes the server too, so it cannot pass a policy that fails only on
	// the web transport.
	if machine := activitySpec(t, repo, freshMachineActivity, "integration"); !slices.ContainsFunc(machine.Checks, func(c projectstate.SpecCheck) bool { return c.ID == freshPolicyCheck }) {
		t.Errorf("the machine client's integration does not hold the policy to the generated requests: %+v", machine.Checks)
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

// policyRefusal is the generated policy test's report of a request the policy refuses.
var policyRefusal = regexp.MustCompile(`ListManager\.\w+: the policy refuses`)

// freshPolicyCheck is the composition check that runs the framework's generated
// TestPolicyAdmitsEveryGeneratedRequest (FIX-046), now against the policy generated from the model.
const freshPolicyCheck = "command:policy-admits-generated-requests"

// requirePolicyRedOnAMismatch: the composition root's policy is generated from the same table as
// the transports (cmd/server/policy.gen.go), so nobody writes one that disagrees with them. The
// check that holds them together is still real: a policy missing a verb the transports send (what
// RUN-015's hand-written one did) is red, naming the refused request, and the generated one is green.
func requirePolicyRedOnAMismatch(t *testing.T, repo string, spec projectstate.TaskSpec) {
	t.Helper()
	statuses, logs := gate(t, repo, commandChecks(t, spec, freshPolicyCheck))
	if statuses[freshPolicyCheck] != checkPass {
		t.Fatalf("the generated policy refuses a request the generated transports send:\n%s", logs[freshPolicyCheck])
	}
	path := filepath.Join(repo, "cmd/server/policy.gen.go")
	generated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(generated), "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, `verb: "reword-todo"`) })
	if at < 0 {
		t.Fatalf("the generated policy has no reword-todo row to drop:\n%s", generated)
	}
	writeRepoFile(t, repo, "cmd/server/policy.gen.go", []byte(strings.Join(slices.Delete(slices.Clone(lines), at, at+1), "\n")))
	statuses, logs = gate(t, repo, commandChecks(t, spec, freshPolicyCheck))
	if statuses[freshPolicyCheck] == checkPass || !policyRefusal.MatchString(logs[freshPolicyCheck]) {
		t.Errorf("a policy missing a verb the transports send stayed green: %s\n%s", statuses[freshPolicyCheck], logs[freshPolicyCheck])
	}
	if err := os.WriteFile(path, generated, 0o600); err != nil {
		t.Fatal(err)
	}
	statuses, logs = gate(t, repo, commandChecks(t, spec, freshPolicyCheck))
	if statuses[freshPolicyCheck] != checkPass {
		t.Errorf("the generated policy, restored, went red:\n%s", logs[freshPolicyCheck])
	}
}

// integratedRepo is composedRepo with the Manager's integration landed: the composition root,
// the generated transports and the generated policy, as a passing integration leaves them.
func integratedRepo(t *testing.T) string {
	t.Helper()
	return integratedRepoFrom(t, composedState(t))
}

// integratedRepoFrom is integratedRepo over a variant of the fixture's state.
func integratedRepoFrom(t *testing.T, state []byte) string {
	t.Helper()
	repo := composedRepoFrom(t, state)
	runGenerators(t, repo, activitySpec(t, repo, freshManagerActivity, "integration"))
	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "go", "mod", "tidy")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "integrate the manager")
	return repo
}

// freshMachineClients are the fixture's two machine clients over its Manager: one REST, one MCP.
var freshMachineClients = []struct{ activity, contract, pkg string }{
	{"C-todo-api", "todoApi", "internal/client/web/list"},
	{"C-todo-mcp-server", "todoMcpServer", "internal/client/todomcpserver/list"},
}

// A machine client is held to the fixture's own plan for it, bound by the test engineer: its
// integration generates the client's transport scenarios from that plan (the generated
// scenariocompose package supplies the subject; an agent writes no hooks file, FPC GC-1), and
// every command check of the gate passes, test-discriminate included. The scenarios' own
// verdicts are held by requireClientScenarioVerdicts: the scenarios run over the wire against the
// booted app, as the plan's principals.
func requireMachineClientGate(t *testing.T, repo, activity, contract, pkg string) {
	t.Helper()
	spec := activitySpec(t, repo, activity, "integration")
	scenarios := 0
	for _, c := range spec.Checks {
		if c.Kind == projectstate.CheckKindScenario {
			scenarios++
		}
	}
	if scenarios == 0 {
		t.Fatalf("%s's integration carries no scenario check", activity)
	}
	runGenerators(t, repo, spec)
	requireInScope(t, repo, spec)
	inRepo(t, repo, "git", "commit", "-q", "--allow-empty", "-m", "integrate "+activity)
	if _, err := os.Stat(filepath.Join(repo, pkg, "client_scenarios.gen_test.go")); err != nil {
		t.Errorf("the client's scenarios were not generated: %v", err)
	}

	bound := commandChecks(t, activitySpec(t, repo, activity, "integration"))
	statuses, logs := gate(t, repo, bound)
	requirePassed(t, statuses, logs)
	for _, c := range bound.Checks {
		if _, ok := statuses[c.ID]; !ok {
			t.Errorf("the gate gave %s no verdict", c.ID)
		}
	}
	requireClientScenarioVerdicts(t, repo, activity, contract, pkg)
}

// requireClientScenarioVerdicts is the whole of a machine client's gate: each scenario check's own
// verdict passes and lands under the contract key, and a wrong expectation goes red, so the run
// really crosses the transport to the Manager. The generated file drives the launched app over
// the wire (MCPSession or RESTClient), never an in-process handler.
func requireClientScenarioVerdicts(t *testing.T, repo, activity, contract, pkg string) {
	t.Helper()
	gen := filepath.Join(repo, pkg, "client_scenarios.gen_test.go")
	generated, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	const bound = `"errorExpected":false}`
	lines := strings.Split(string(generated), "\n")
	at := slices.IndexFunc(lines, func(l string) bool {
		return strings.HasPrefix(strings.TrimLeft(l, "\t"), "expect(t, ") && strings.Contains(l, bound)
	})
	if at < 0 {
		t.Fatalf("the generated scenarios carry no expectation to get wrong:\n%s", generated)
	}
	lines[at] = strings.Replace(lines[at], bound, `"errorExpected":true}`, 1)
	broken := strings.Join(lines, "\n")
	if !strings.Contains(string(generated), "scenariohost.MCPSession(") && !strings.Contains(string(generated), "scenariohost.RESTClient(") {
		t.Fatalf("the generated scenarios do not drive the launched app over the wire:\n%s", generated)
	}

	spec := activitySpec(t, repo, activity, "integration")
	spec.Checks = slices.DeleteFunc(slices.Clone(spec.Checks), func(c projectstate.SpecCheck) bool {
		return c.Kind != projectstate.CheckKindScenario
	})
	statuses, logs := gate(t, repo, spec)
	requirePassed(t, statuses, logs)
	for _, c := range spec.Checks {
		if _, ok := statuses[c.ID]; !ok {
			t.Errorf("the gate gave %s no verdict", c.ID)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "test-results", contract, "results.json")); err != nil {
		t.Errorf("the verdicts must land under the contract key: %v", err)
	}

	if err := os.WriteFile(gen, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runIn(repo, "go", "test", "-count=1", "./"+pkg+"/"); err == nil {
		t.Errorf("a wrong expectation stayed green:\n%s", out)
	}
	if err := os.WriteFile(gen, generated, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The fixture's Manager path: it constructs on the gate, it composes the server, and its two
// machine clients' contracts came with the Manager's, one operation per route or tool its views reach.
func TestFreshManagerPathComposesBothMachineClients(t *testing.T) {
	repo := integratedRepo(t)
	construct := activitySpec(t, repo, freshManagerActivity, "construction")
	statuses, logs := gate(t, repo, commandChecks(t, construct))
	requirePassed(t, statuses, logs)

	main, err := os.ReadFile(filepath.Join(repo, "cmd/server/main.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), "messagebus.NewMessageBus()") {
		t.Error("the composition root must build the infra-free Utility by its pure constructor")
	}
	for _, c := range freshMachineClients {
		sc, ok := stateContract(t, repo, c.contract)
		if !ok || sc.GoPackage != c.pkg || !slices.ContainsFunc(sc.Interface.Operations, func(op projectstate.ContractOperation) bool { return op.Name == "ListAddTodo" }) {
			t.Errorf("%s's derived contract: %+v", c.contract, sc)
		}
	}
}

// Each machine client over the fixture's real Manager contract (strings, a closed enum, booleans,
// object results, a read-only operation, an owner scope) integrates on its own: its transport and
// scenarios are generated, in scope, and its command gate is green.
func TestMachineClientsIntegrateOverTheManagersRealShapes(t *testing.T) {
	repo := integratedRepo(t)
	for _, c := range freshMachineClients {
		t.Run(c.contract, func(t *testing.T) { requireMachineClientGate(t, repo, c.activity, c.contract, c.pkg) })
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
		t.Errorf("todo-api's plan takes %q", got)
	}
	spec := activitySpec(t, repo, freshSurfaceActivity, "construction")
	if spec.Identity.Command != "frontend-construction" {
		t.Errorf("todo-web-app takes %q", spec.Identity.Command)
	}
	if !slices.Contains(spec.EditScope.Components[0].Paths, "webApp/src/todo-web-app/**") || slices.Contains(spec.EditScope.Components[0].Paths, "webApp/src/todo-api/**") {
		t.Errorf("a surface owns its own directory only: %v", spec.EditScope.Components[0].Paths)
	}
}

// Finishing a browser surface must not turn every later arch gate red: its code is not a Go
// package, and neither is a REST client's. An MCP server that finished with no package still is red.
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
	finish(freshSurfaceActivity, freshMachineActivity)
	arch := commandChecks(t, taskSpec(t, repo, "construction"), "command:arch")
	statuses, logs := gate(t, repo, arch)
	if statuses["command:arch"] != checkPass {
		t.Errorf("a finished browser surface or REST client reddened the gate:\n%s", logs["command:arch"])
	}
	finish("C-todo-mcp-server")
	statuses, logs = gate(t, repo, arch)
	if statuses["command:arch"] == checkPass || !strings.Contains(logs["command:arch"], "ALIGN-MISSING-PKG") {
		t.Errorf("a finished MCP server with no package must be red: %s\n%s", statuses["command:arch"], logs["command:arch"])
	}
}

// A browser surface's construction on the real toolchain: the job bootstraps the web project,
// the verb installs it, the agent commits, and the gate's own npm ci, typecheck and lint pass.
// A layer violation then goes red.
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
	s := &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: freshSurfaceActivity, TaskID: "construction", ComponentID: "todo-web-app", git: runGit}
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
	for _, want := range []string{"webApp/package-lock.json", "webApp/src/contracts/schema.ts", "webApp/src/api/ops.gen.ts", "webApp/todo-web-app.html", "uitests/package.json"} {
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
	writeRepoFile(t, repo, "webApp/src/todo-web-app/routes/Bad.tsx", []byte("import type { JSX } from 'react';\nimport { useOpsClient } from '../../api/opsContext';\n\nexport function Bad(): JSX.Element {\n  useOpsClient();\n  return <main />;\n}\n"))
	statuses, _ = gate(t, repo, commandChecks(t, spec, "command:lint"))
	if statuses["command:lint"] == checkPass {
		t.Error("eslint stayed green over a route that imports the transport")
	}
	if err := os.Remove(filepath.Join(repo, "webApp/src/todo-web-app/routes/Bad.tsx")); err != nil {
		t.Fatal(err)
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
	const surface = "todo-web-app"
	requireDerivedShape(t, proj.ServiceContracts["todoWebApp"])
	scenarios, err := projectstate.ComponentScenarios(&proj, "todoWebApp")
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
	for _, op := range proj.ServiceContracts["todoWebApp"].Interface.Operations {
		got = append(got, op.Name)
	}
	if !slices.Equal(got, []string{"OnlyOne", "AndAnother"}) {
		t.Errorf("a re-record must rewrite the contract, got %v", got)
	}

	rewriteActorLabels(sys, surface, "a prose label")
	if err := projectstate.RecordBrowserContract(&proj, surface); err == nil || !strings.Contains(err.Error(), "must lead with exported operation names") {
		t.Errorf("a prose label names no operation: %v", err)
	}
	if err := projectstate.RecordBrowserContract(&proj, "todo-api"); err == nil {
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
	delete(proj.ServiceContracts, "todoWebApp")
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
		{"todo-api", projectstate.ServiceContract{Layer: "Client"}},
		{"todo-api", projectstate.ServiceContract{Layer: "Manager"}},
		{"todo-web-app", projectstate.ServiceContract{Layer: "Utility"}},
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
	delete(contracts, "todoWebApp")
	raw, err := json.Marshal(contracts)
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = raw
	// The surface's design is recorded below, so the project starts without it.
	var artifacts map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["phaseArtifacts"], &artifacts); err != nil {
		t.Fatal(err)
	}
	delete(artifacts, "uiDesign")
	if top["phaseArtifacts"], err = json.Marshal(artifacts); err != nil {
		t.Fatal(err)
	}
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
	s.ActivityID, s.ComponentID = freshSurfaceActivity, "todo-web-app"
	// The fixture's own UI design for the surface, recorded again: the verb writes the contract.
	var fx struct {
		PhaseArtifacts struct {
			UIDesign map[string]map[string]any `json:"uiDesign"`
		} `json:"phaseArtifacts"`
	}
	if err := json.Unmarshal(composedState(t), &fx); err != nil {
		t.Fatal(err)
	}
	if err := s.recordPhaseArtifact("todo-web-app", map[string]any{"UIDesign": fx.PhaseArtifacts.UIDesign["todo-web-app"]}); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	requireDerivedShape(t, got.ServiceContracts["todoWebApp"])
}
