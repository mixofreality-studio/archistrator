package main

// run009_test.go replays RUN-009's real contracts and the components its agents wrote. That run
// walked into three walls in a row at its first Manager, none of which a hand-trimmed fixture
// reached: a lowercase Utility operation, an inline integer result, and a Utility dependency with
// no constructed value. Its System is conformed to the Client design rules, and nothing else in
// its contracts is touched: testdata/fresh/run009.project.json.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The Manager RUN-009 never reached: the ops its dynamic views call, over its three kinds of dep.
var run009ManagerOps = []string{"AddTodo", "RewordTodo", "RemoveTodo", "ToggleTodo", "ToggleAll", "ClearCompleted", "ReviewList", "SwitchFilter", "GetItemsLeft"}

const run009Activity = "C-list-keeping-manager"

func run009ManagerContract() string {
	ops := make([]string, 0, len(run009ManagerOps))
	for _, op := range run009ManagerOps {
		ops = append(ops, fmt.Sprintf(`{"name": %q, "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}], "result": {"$ref": "#/$defs/Proof"}, "error": true}`, op))
	}
	return `{"component": "listKeepingManager", "layer": "Manager", "goPackage": "internal/manager/listkeeping", "title": "list keeping",
	 "deps": [{"name": "recognizer", "component": "recognizingEngine"}, {"name": "todos", "component": "todoListAccess"}, {"name": "bus", "component": "pubSub"}],
	 "$defs": {"Proof": {"type": "string"}},
	 "interface": {"name": "ListKeepingManager", "layer": "manager", "operations": [` + strings.Join(ops, ",") + `]}}`
}

const run009ManagerImpl = `// Package listkeeping is the list keeping manager.
package listkeeping

import (
	"github.com/mixofreality-studio/archistrator-platform/framework-go/manager"

	"MODULE/internal/engine/recognizing"
	"MODULE/internal/resourceaccess/todolist"
	"MODULE/internal/utility/pubsub"
)

type listKeepingManager struct {
	recognizer recognizing.RecognizingEngine
	todos      todolist.TodoListAccess
	bus        pubsub.PubSub
}

func newListKeepingManager(recognizer recognizing.RecognizingEngine, todos todolist.TodoListAccess, bus pubsub.PubSub) *listKeepingManager {
	return &listKeepingManager{recognizer: recognizer, todos: todos, bus: bus}
}

METHODS

// WorkerManifest supplies this Manager's workflows to the generated RegisterManagerWorker.
func (m *listKeepingManager) WorkerManifest() genWorkerManifest {
	return genWorkerManifest{Activities: genActivities{Todos: m.todos, Bus: m.bus}}
}
`

func run009State(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run009.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		t.Fatal(err)
	}
	contracts["listKeepingManager"] = json.RawMessage(run009ManagerContract())
	merged, err := json.Marshal(contracts)
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = merged
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return withBrowserContracts(t, out)
}

// run009Repo has RUN-009's three components and the Manager built, as a passing construction
// leaves them.
func run009Repo(t *testing.T) string {
	t.Helper()
	repo := bornRepo(t, run009State(t))
	methods := make([]string, 0, len(run009ManagerOps))
	for _, op := range run009ManagerOps {
		methods = append(methods, fmt.Sprintf("func (*listKeepingManager) %s(_ manager.Context, proof Proof) (Proof, error) {\n\treturn proof, nil\n}\n", op))
	}
	buildRun009Components(t, repo, strings.TrimRight(strings.Join(methods, "\n"), "\n"))
	return repo
}

// buildRun009Components commits RUN-009's three components and a Manager with these methods.
func buildRun009Components(t *testing.T, repo, methods string) {
	t.Helper()
	module := strings.TrimSpace(inRepo(t, repo, "go", "list", "-m"))
	inRepo(t, repo, "make", "gen-models", "CONTRACTS=pubSub recognizingEngine todoListAccess listKeepingManager")
	for file, dest := range map[string]string{
		"pubsubutility.go":     "internal/utility/pubsub/pubsubutility.go",
		"recognizingengine.go": "internal/engine/recognizing/recognizingengine.go",
		"todolistaccess.go":    "internal/resourceaccess/todolist/todolistaccess.go",
	} {
		src, err := os.ReadFile(filepath.Join("testdata", "fresh", "run009", file))
		if err != nil {
			t.Fatal(err)
		}
		writeRepoFile(t, repo, dest, src)
	}
	runGenerators(t, repo, activitySpec(t, repo, run009Activity, "construction"))
	impl := strings.NewReplacer("METHODS", methods, "MODULE", module).Replace(run009ManagerImpl)
	writeRepoFile(t, repo, "internal/manager/listkeeping/listkeepingmanager.go", []byte(impl))
	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "go", "mod", "tidy")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "build the components")
}

// RUN-009's Manager path: it constructs, it composes the server, and both machine clients over it
// land on their generated code with their proofs passing.
func TestRun009ManagerPath(t *testing.T) {
	repo := run009Repo(t)
	construct := activitySpec(t, repo, run009Activity, "construction")
	statuses, logs := gate(t, repo, commandChecks(t, construct))
	requirePassed(t, statuses, logs)

	integrate := activitySpec(t, repo, run009Activity, "integration")
	runGenerators(t, repo, integrate)
	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "go", "mod", "tidy")
	fillPolicy(t, repo, ownScopePolicy)
	requireInScope(t, repo, integrate)
	inRepo(t, repo, "git", "commit", "-q", "-m", "integrate the manager")
	statuses, logs = gate(t, repo, commandChecks(t, integrate))
	requirePassed(t, statuses, logs)
	for _, want := range []string{"command:build-server", "command:gen-compose-check", "command:gen-clients-check"} {
		if _, ok := statuses[want]; !ok {
			t.Errorf("the integration gate has no %s", want)
		}
	}
	main, err := os.ReadFile(filepath.Join(repo, "cmd/server/main.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), "pubsub.NewPubSub()") {
		t.Error("the composition root must build the infra-free Utility by its pure constructor")
	}

	// A machine client's contract came with its Manager's, one op per route or tool its views reach.
	sc, ok := stateContract(t, repo, "webApi")
	if !ok || sc.GoPackage != "internal/client/web/listkeeping" || !slices.ContainsFunc(sc.Interface.Operations, func(op projectstate.ContractOperation) bool { return op.Name == "ListKeepingAddTodo" }) {
		t.Fatalf("webApi's derived contract: %+v", sc)
	}
	if sc, ok := stateContract(t, repo, "mcpServer"); !ok || sc.GoPackage != "internal/client/mcpserver/listkeeping" {
		t.Fatalf("mcpServer's derived contract: %+v", sc)
	}

	for _, tc := range []struct{ activity, contract, pkg, ctor string }{
		{"C-web-api", "webApi", "internal/client/web/listkeeping", run009RESTSubject},
		{"C-mcp-server", "mcpServer", "internal/client/mcpserver/listkeeping", run009MCPSubject},
	} {
		t.Run(tc.contract, func(t *testing.T) { requireClientScenarios(t, repo, tc.activity, tc.contract, tc.pkg, tc.ctor) })
	}
}

func stateContract(t *testing.T, repo, key string) (projectstate.ServiceContract, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, ".aiarch/state/project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ServiceContracts map[string]projectstate.ServiceContract `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	sc, ok := doc.ServiceContracts[key]
	return sc, ok
}

// run009Subject builds the client's generated Handler over the real Manager. The fixture's Manager
// answers without its dependencies, so no worker has to serve a workflow for it here.
const (
	run009RESTSubject = `Manager: listkeeping.NewListKeepingManager(recognizing.NewRecognizingEngine(), nil, pubsub.NewPubSub()), Security: permitAll{}`
	run009MCPSubject  = `Manager: listkeeping.NewListKeepingManager(recognizing.NewRecognizingEngine(), nil, pubsub.NewPubSub()), Security: permitAll{}`
)

const run009ClientHooks = `// Package hooks for the client's scenario tests.
package listkeeping_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"

	listkeepingclient "MODULE/CLIENTPKG"
	"MODULE/internal/engine/recognizing"
	"MODULE/internal/manager/listkeeping"
	"MODULE/internal/utility/pubsub"
)

func newSubject(t *testing.T, h *scenariohost.Host) toolServer {
	t.Helper()
	_ = h
	return toolServer{Handlers: []toolRegistrar{&listkeepingclient.Handler{SUBJECT}}}
}
`

// requireClientScenarios binds every scenario the projection gives one client, generates its tests
// through that client, fills the hooks, and runs them. A broken surface then goes red: this is the
// founder's rule that each surface is tested on its own, against what is below it.
func requireClientScenarios(t *testing.T, repo, activity, contract, pkg, subject string) {
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
	recordClientPlan(t, repo, contract)
	runGenerators(t, repo, spec)
	inRepo(t, repo, "make", "gen-tests")
	module := strings.TrimSpace(inRepo(t, repo, "go", "list", "-m"))
	hooks := strings.NewReplacer("MODULE", module, "CLIENTPKG", pkg, "SUBJECT", subject).Replace(run009ClientHooks)
	writeRepoFile(t, repo, pkg+"/client_hooks_test.go", []byte(hooks))
	inRepo(t, repo, "go", "mod", "tidy")
	// The plan is the test engineer's commit; what is left is this task's own.
	inRepo(t, repo, "git", "add", ".aiarch/state/project.json")
	inRepo(t, repo, "git", "commit", "-q", "-m", "bind "+contract)
	requireInScope(t, repo, activitySpec(t, repo, activity, "integration"))
	inRepo(t, repo, "git", "commit", "-q", "--allow-empty", "-m", "integrate "+activity)

	// The whole gate, as the job runs it: every command check and each scenario's own verdict.
	bound := activitySpec(t, repo, activity, "integration")
	statuses, logs := gate(t, repo, bound)
	requirePassed(t, statuses, logs)
	for _, c := range bound.Checks {
		if _, ok := statuses[c.ID]; !ok {
			t.Errorf("the gate gave %s no verdict", c.ID)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "test-results", contract, "results.json")); err != nil {
		t.Errorf("the verdicts must land under the contract key: %v", err)
	}

	// A wrong expectation goes red, so the run really crosses the transport to the Manager.
	gen := filepath.Join(repo, pkg, "client_scenarios.gen_test.go")
	generated, err := os.ReadFile(gen)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(generated), `"proof-value"`, `"other-value"`, 1)
	if broken == string(generated) {
		t.Fatalf("the generated scenarios carry no bound value:\n%s", generated)
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

func runIn(repo string, argv ...string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GOWORK=off")
	raw, err := cmd.CombinedOutput()
	return string(raw), err
}

// recordClientPlan binds every projected scenario of a client: each stimulus calls its operation
// with the proof value and expects it back, in that transport's own shape.
func recordClientPlan(t *testing.T, repo, contract string) {
	t.Helper()
	statePath := filepath.Join(repo, ".aiarch/state/project.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(raw, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	scenarios, err := projectstate.ComponentScenarios(&proj, contract)
	if err != nil {
		t.Fatal(err)
	}
	result := `"proof-value"`
	if contract == "mcpServer" {
		result = `{"result":"proof-value"}`
	}
	var bindings []map[string]any
	for _, s := range scenarios {
		var steps []map[string]any
		for _, st := range s.Stimuli {
			if st.Input.Op == "" {
				t.Fatalf("%s step %d resolved no operation", s.ID, st.Seq)
			}
			steps = append(steps, map[string]any{
				"seq": st.Seq, "operation": st.Input.Op,
				"inputs": []map[string]any{{"name": "proof", "value": "proof-value", "schemaRef": "Proof"}},
				"expect": map[string]any{"result": result}, "unobservable": run009Unobservable(st),
			})
		}
		bindings = append(bindings, map[string]any{"scenario": s.ID, "steps": steps})
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	artifacts, _ := top["phaseArtifacts"].(map[string]any)
	plans, _ := artifacts["testPlan"].(map[string]any)
	plans[contract] = map[string]any{"component": contract, "bindings": bindings}
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// run009Unobservable names what a stimulus expects that a client cannot see.
func run009Unobservable(st scenario.Stimulus) []string {
	var out []string
	for _, o := range st.ExpectedOutputs {
		out = append(out, o.Node)
	}
	for _, s := range st.ExpectedStates {
		out = append(out, s.Node)
	}
	return out
}

// Every one of RUN-009's contracts records, and the spelling it wrote itself is refused, naming
// why a view's label still resolves.
func TestRun009ContractsRecord(t *testing.T) {
	var doc struct {
		ServiceContracts map[string]projectstate.ServiceContract `json:"serviceContracts"`
	}
	if err := json.Unmarshal(run009State(t), &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"pubSub", "recognizingEngine", "todoListAccess", "listKeepingManager"} {
		sc := doc.ServiceContracts[key]
		if err := canonicalizeContract(key, &sc); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
	lowercase := doc.ServiceContracts["pubSub"]
	lowercase.Interface.Operations = append(lowercase.Interface.Operations[:0:0], lowercase.Interface.Operations...)
	lowercase.Interface.Operations[0].Name = "publish"
	if err := canonicalizeContract("pubSub", &lowercase); err == nil || !strings.Contains(err.Error(), "exported Go identifier") {
		t.Errorf("RUN-009's lowercase operation must be refused at record time: %v", err)
	}
}

// The contract comes with its Manager's: nothing exists before it, a re-record follows it, and a
// client over a second Manager gets a facet, because one package serves one surface.
func TestMachineClientContractFollowsItsManagers(t *testing.T) {
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(run009State(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	requireDerivedMachineShape(t, proj)

	sys, _ := proj.SystemDesign.Model.(*projectstate.System)
	rewriteClientCalls(sys, "mcp-server", "list-keeping-manager", "addTodo(proof)")
	if err := projectstate.RecordMachineClientContracts(&proj); err != nil {
		t.Fatal(err)
	}
	if got := proj.ServiceContracts["mcpServer"].Interface.Operations; len(got) != 1 || got[0].Name != "ListKeepingAddTodo" {
		t.Errorf("a re-record follows the design, got %+v", got)
	}

	requireDerivedContractFollowsRevisions(t, &proj)

	second := proj.ServiceContracts["listKeepingManager"]
	second.Interface.Name, second.GoPackage = "AuditManager", "internal/manager/audit"
	proj.ServiceContracts["auditManager"] = second
	sys.DynamicViews[0].Steps[0].Calls = append(sys.DynamicViews[0].Steps[0].Calls,
		projectstate.TraceCall{From: "agent", To: "mcp-server", Label: "audits"},
		projectstate.TraceCall{From: "mcp-server", To: "audit-manager", Label: "reviewList(proof)"})
	if err := projectstate.RecordMachineClientContracts(&proj); err != nil {
		t.Fatal(err)
	}
	facet, ok := proj.ServiceContracts["mcpServerListKeeping"]
	audit := proj.ServiceContracts["mcpServer"]
	if !ok || facet.Component != "mcpServer" || facet.GoPackage != "internal/client/mcpserver/listkeeping" {
		t.Errorf("the second Manager's facet: %+v", facet)
	}
	if audit.GoPackage != "internal/client/mcpserver/audit" || audit.Interface.Operations[0].Name != "AuditReviewList" {
		t.Errorf("the first Manager's: %+v", audit)
	}
}

// requireDerivedContractFollowsRevisions: a Manager re-recorded under the same operation names, as
// a design review sends back, carries its new params to the client, whose tests would call the old
// ones otherwise; a hand-written family holding those names is not this derivation's to rewrite.
func requireDerivedContractFollowsRevisions(t *testing.T, proj *projectstate.Project) {
	t.Helper()
	revised := proj.ServiceContracts["listKeepingManager"]
	revised.Interface.Operations = slices.Clone(revised.Interface.Operations)
	revised.Interface.Operations[0].Params = append(slices.Clone(revised.Interface.Operations[0].Params), revised.Interface.Operations[0].Params[0])
	revised.Interface.Operations[0].Params[1].Name = "title"
	proj.ServiceContracts["listKeepingManager"] = revised
	if err := projectstate.RecordMachineClientContracts(proj); err != nil {
		t.Fatal(err)
	}
	if got := proj.ServiceContracts["mcpServer"].Interface.Operations[0].Params; len(got) != 2 || got[1].Name != "title" {
		t.Errorf("a re-recorded Manager's params must reach its client, got %+v", got)
	}

	// A hand-written family with exactly those names stays: nothing here wrote it.
	handWritten := proj.ServiceContracts["mcpServer"]
	handWritten.Title = "written by hand"
	proj.ServiceContracts["mcpServer"] = handWritten
	if err := projectstate.RecordMachineClientContracts(proj); err != nil {
		t.Fatal(err)
	}
	if got := proj.ServiceContracts["mcpServer"].Title; got != "written by hand" {
		t.Errorf("a hand-written family holding the same names was rewritten: %q", got)
	}
	handWritten.Title = "mcpServer contract, derived from listKeepingManager"
	proj.ServiceContracts["mcpServer"] = handWritten

}

// requireDerivedMachineShape holds RUN-009's two machine contracts to what derivation promises.
func requireDerivedMachineShape(t *testing.T, proj projectstate.Project) {
	t.Helper()
	web := proj.ServiceContracts["webApi"]
	if len(web.Interface.Operations) != len(run009ManagerOps) {
		t.Errorf("webApi has %d operations, want the %d its views reach", len(web.Interface.Operations), len(run009ManagerOps))
	}
	if web.Layer != "Client" || web.Deps != nil || web.Infra != nil {
		t.Errorf("a derived contract is a Client's and depends on nothing: %+v", web)
	}
	for _, op := range web.Interface.Operations {
		if len(op.Params) != 1 || op.Params[0].Name != "proof" {
			t.Errorf("%s keeps the Manager's params: %+v", op.Name, op.Params)
		}
	}
	mcp := proj.ServiceContracts["mcpServer"]
	if !strings.Contains(string(mcp.Interface.Operations[0].Result), `"result"`) || strings.Contains(string(web.Interface.Operations[0].Result), `"result"`) {
		t.Errorf("an MCP tool's result is its structured output and a route's is bare: %s, %s", mcp.Interface.Operations[0].Result, web.Interface.Operations[0].Result)
	}
}

func rewriteClientCalls(sys *projectstate.System, from, to, label string) {
	for i := range sys.DynamicViews {
		for j := range sys.DynamicViews[i].Steps {
			for k := range sys.DynamicViews[i].Steps[j].Calls {
				if call := &sys.DynamicViews[i].Steps[j].Calls[k]; call.From == from && call.To == to {
					call.Label = label
				}
			}
		}
	}
}

// A project holding only its Managers' contracts gets its machine clients' from them.
func TestRecordingAManagerDerivesItsClients(t *testing.T) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(run009State(t), &top); err != nil {
		t.Fatal(err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		t.Fatal(err)
	}
	delete(contracts, "webApi")
	delete(contracts, "mcpServer")
	delete(contracts, "webApp")
	delete(contracts, "mcpAppsSurface")
	var anyProject projectstate.ProjectID
	raw, err := json.Marshal(contracts)
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = raw
	state, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	proj, _, err := projectstate.DecodeProjectJSON(state, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := proj.ServiceContracts["webApi"]; ok {
		t.Fatal("the fixture must start with no client contract")
	}
	if err := projectstate.RecordMachineClientContracts(&proj); err != nil {
		t.Fatal(err)
	}
	if _, ok := proj.ServiceContracts["webApi"]; !ok {
		t.Error("the REST client's contract must be derived")
	}
	if _, ok := proj.ServiceContracts["mcpServer"]; !ok {
		t.Error("the MCP client's contract must be derived")
	}
}

// A machine client reaches no task spec without its derived contract or with no scenario through
// it: either would integrate a generated surface that nothing ever tests.
func TestMachineClientWithNothingToTestHasNoSpec(t *testing.T) {
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(run009State(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projectstate.TaskSpecFor(&proj, "C-web-api", "integration"); err != nil {
		t.Fatalf("with its contract and its views it has a spec: %v", err)
	}
	contract := proj.ServiceContracts["webApi"]
	delete(proj.ServiceContracts, "webApi")
	if _, err := projectstate.TaskSpecFor(&proj, "C-web-api", "integration"); err == nil || !strings.Contains(err.Error(), "has no contract") {
		t.Errorf("no contract: %v", err)
	}
	proj.ServiceContracts["webApi"] = contract
	sys, _ := proj.SystemDesign.Model.(*projectstate.System)
	for i := range sys.DynamicViews {
		for j := range sys.DynamicViews[i].Steps {
			st := &sys.DynamicViews[i].Steps[j]
			st.Calls = slices.DeleteFunc(st.Calls, func(c projectstate.TraceCall) bool { return c.To == "web-api" })
		}
	}
	if _, err := projectstate.TaskSpecFor(&proj, "C-web-api", "integration"); err == nil || !strings.Contains(err.Error(), "no scenario to integrate against") {
		t.Errorf("no scenario: %v", err)
	}
}

// A machine client's generated scenarios are in its integration's edit scope, beside its other
// generated files.
func TestGeneratedScenariosAreInTheGeneratedScope(t *testing.T) {
	glob := "internal/client/web/listkeeping/**/*.gen.*"
	for _, p := range []string{"internal/client/web/listkeeping/client_scenarios.gen_test.go", "internal/client/web/listkeeping/x_handlers.gen.go"} {
		if !projectstate.EditScopeGlobMatch(glob, p) {
			t.Errorf("%s is not in %s", p, glob)
		}
	}
	if projectstate.EditScopeGlobMatch(glob, "internal/client/web/listkeeping/client_hooks_test.go") {
		t.Error("a hand-filled hooks file is not generated")
	}
}
