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
	"path/filepath"
	"strings"
	"testing"

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
	methods := make([]string, 0, len(run009ManagerOps))
	for _, op := range run009ManagerOps {
		methods = append(methods, fmt.Sprintf("func (*listKeepingManager) %s(_ manager.Context, proof Proof) (Proof, error) {\n\treturn proof, nil\n}\n", op))
	}
	impl := strings.NewReplacer("METHODS", strings.TrimRight(strings.Join(methods, "\n"), "\n"), "MODULE", module).Replace(run009ManagerImpl)
	writeRepoFile(t, repo, "internal/manager/listkeeping/listkeepingmanager.go", []byte(impl))
	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "go", "mod", "tidy")
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "build the components")
	return repo
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
	requireInScope(t, repo, integrate)
	inRepo(t, repo, "git", "commit", "-q", "-m", "integrate the manager")
	statuses, logs = gate(t, repo, commandChecks(t, integrate))
	requirePassed(t, statuses, logs)
	for _, want := range []string{"command:build-server", "command:gen-compose-check", "command:gen-clients-check", "command:clients-wire"} {
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

	for _, activity := range []string{"C-web-api", "C-mcp-server"} {
		spec := activitySpec(t, repo, activity, "integration")
		runGenerators(t, repo, spec)
		inRepo(t, repo, "go", "mod", "tidy")
		requireInScope(t, repo, spec)
		inRepo(t, repo, "git", "commit", "-q", "--allow-empty", "-m", "integrate "+activity)
		requireMachineClientGate(t, repo, spec)
	}
	for _, want := range []string{"internal/client/web/listkeeping/client_scenarios.gen_test.go", "internal/client/mcpserver/listkeeping/client_scenarios.gen_test.go"} {
		if _, err := os.Stat(filepath.Join(repo, want)); err != nil {
			t.Errorf("no proof at %s: %v", want, err)
		}
	}
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
