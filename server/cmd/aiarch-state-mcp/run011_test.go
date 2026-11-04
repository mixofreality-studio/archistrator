package main

// run011_test.go replays RUN-011's first Manager. testdata/fresh/run011.project.json is its
// activity/C-todo-keeping-manager state with the Manager's contract taken out, and
// run011.contract.todo-keeping-manager.json is that contract exactly as its agent passed it to
// the verb: no layer and no deps, because a designer on a fresh project never writes deps and no
// prompt asks for them. With no deps, modelgen emitted a zero-argument NewTodoKeepingManager(),
// temporalgen an empty genActivities whose worker.gen.go did not compile, and composegen would
// have skipped the Manager entirely. testdata/fresh/run011 holds the three components its agents
// built.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	run011Manager  = "todo-keeping-manager"
	run011Activity = "C-todo-keeping-manager"
)

func run011Raw(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run011.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// run011ManagerContract is the Manager's contract as RUN-011's agent passed it.
func run011ManagerContract(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run011.contract.todo-keeping-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if _, ok := c["deps"]; ok {
		t.Fatal("the fixture must keep the contract RUN-011's agent recorded, which names no deps")
	}
	return c
}

// run011Session is a construct session over proj, ambient on the Manager.
func run011Session(t *testing.T, proj projectstate.Project) *Session {
	t.Helper()
	s, _ := seedProject(t, proj, jobModeConstruct, 0)
	s.ComponentID, s.ActivityID = run011Manager, run011Activity
	return s
}

func run011Project(t *testing.T) projectstate.Project {
	t.Helper()
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(run011Raw(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := proj.ServiceContracts["todoKeepingManager"]; ok {
		t.Fatal("the fixture must not carry the Manager's contract")
	}
	return proj
}

// run011Recorded records the agent's contract through the verb and returns the stored state.
func run011Recorded(t *testing.T) []byte {
	t.Helper()
	s := run011Session(t, run011Project(t))
	if err := s.recordServiceContract(run011ManagerContract(t)); err != nil {
		t.Fatalf("RUN-011's Manager contract: %v", err)
	}
	stored, err := os.ReadFile(filepath.Join(s.StateRoot, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

// The Manager's constructor deps come from the System: the Temporal client, then the
// ResourceAccess and Utility components the Manager draws an edge to, by contract key, in the
// order the relationships draw them. The Resources stay out.
func TestRun011ManagerRecordDerivesDepsFromTheSystem(t *testing.T) {
	proj := run011Project(t)
	// The precondition of a refusal-free record: every Utility the Manager calls has a contract.
	sys := proj.SystemDesign.Model.(*projectstate.System)
	for _, r := range sys.Relationships {
		if r.From != run011Manager {
			continue
		}
		for _, c := range sys.Components {
			if key, ok := projectstate.ContractKeyFor(c); ok && c.ID == r.To && c.Kind == projectstate.CompUtility {
				if _, recorded := proj.ServiceContracts[key]; !recorded {
					t.Fatalf("the fixture's Utility %s has no contract", c.ID)
				}
			}
		}
	}

	stored := run011Recorded(t)
	model, err := projectmodel.Load(stored)
	if err != nil {
		t.Fatalf("what the verb records, every generator loads: %v", err)
	}
	var doc struct {
		ServiceContracts map[string]projectstate.ServiceContract `json:"serviceContracts"`
	}
	if err := json.Unmarshal(stored, &doc); err != nil {
		t.Fatal(err)
	}
	want := []projectstate.ContractDep{
		{Name: "client", GoType: "client.Client", GoImport: "go.temporal.io/sdk/client"},
		{Name: "actorIdentityAccess", Component: "actorIdentityAccess"},
		{Name: "todoListAccess", Component: "todoListAccess"},
		{Name: "messageBus", Component: "messageBus"},
	}
	if got := doc.ServiceContracts["todoKeepingManager"].Deps; !slices.Equal(got, want) {
		t.Fatalf("deps = %+v\nwant %+v", got, want)
	}
	if got := len(model.Contracts["todoKeepingManager"].Deps); got != len(want) {
		t.Errorf("the loaded model carries %d deps, want %d", got, len(want))
	}
}

// The deps follow the design, as the layer does: a component dep the agent supplies that the
// System does not draw, or draws under another name, is refused with nothing written. A plain dep
// is the agent's to add and is kept after the client, and the client is kept once.
func TestRecordServiceContractRefusesADepTheSystemDoesNotDraw(t *testing.T) {
	for name, deps := range map[string][]any{
		"no edge":        {map[string]any{"name": "rules", "component": "rulesEngine"}},
		"a Resource":     {map[string]any{"name": "todoStore", "component": "todoStore"}},
		"another name":   {map[string]any{"name": "lists", "component": "todoListAccess"}},
		"another client": {map[string]any{"name": "client", "goType": "*client.Client", "goImport": "go.temporal.io/sdk/client"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := run011Session(t, run011Project(t))
			err := s.recordServiceContract(mergedContract(t, run011ManagerContract(t), map[string]any{"deps": deps}))
			if err == nil || !strings.Contains(err.Error(), "leave") {
				t.Fatalf("%v must be refused, saying what to do: %v", deps, err)
			}
			if s.wroteState {
				t.Fatal("a refused record writes nothing")
			}
		})
	}

	s := run011Session(t, run011Project(t))
	supplied := []any{
		map[string]any{"name": "maxTitle", "goType": "int"},
		map[string]any{"name": "client", "goType": "client.Client", "goImport": "go.temporal.io/sdk/client"},
		map[string]any{"name": "todoListAccess", "component": "todoListAccess"},
	}
	if err := s.recordServiceContract(mergedContract(t, run011ManagerContract(t), map[string]any{"deps": supplied})); err != nil {
		t.Fatal(err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range proj.ServiceContracts["todoKeepingManager"].Deps {
		names = append(names, d.Name)
	}
	if want := []string{"client", "maxTitle", "actorIdentityAccess", "todoListAccess", "messageBus"}; !slices.Equal(names, want) {
		t.Errorf("deps = %v, want %v", names, want)
	}
}

// A Manager recorded before a component it calls has a contract is refused, naming the component
// and the activity that builds it: its constructor takes that contract's interface.
func TestRecordServiceContractRefusesAManagerBeforeItsDeps(t *testing.T) {
	proj := run011Project(t)
	delete(proj.ServiceContracts, "messageBus")
	s := run011Session(t, proj)
	err := s.recordServiceContract(run011ManagerContract(t))
	if err == nil || !strings.Contains(err.Error(), `"message-bus"`) || !strings.Contains(err.Error(), "C-message-bus") {
		t.Fatalf("the record must be refused, naming the Utility and its activity: %v", err)
	}
	if s.wroteState {
		t.Fatal("a refused record writes nothing")
	}
}

// run011ManagerImpl is the Manager the way a junior developer writes it against the generated
// constructor: the client first, then the contract-key-named deps.
const run011ManagerImpl = `// Package todokeeping is the todo keeping manager.
package todokeeping

import (
	"github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"go.temporal.io/sdk/client"

	"MODULE/internal/resourceaccess/actoridentity"
	"MODULE/internal/resourceaccess/todolist"
	"MODULE/internal/utility/messagebus"
)

type todoKeepingManager struct {
	client     client.Client
	identities actoridentity.ActorIdentityAccess
	todos      todolist.TodoListAccess
	bus        messagebus.MessageBus
}

func newTodoKeepingManager(c client.Client, identities actoridentity.ActorIdentityAccess, todos todolist.TodoListAccess, bus messagebus.MessageBus) *todoKeepingManager {
	return &todoKeepingManager{client: c, identities: identities, todos: todos, bus: bus}
}

func (*todoKeepingManager) AddTodo(manager.Context, ActorProof, string) (AddOutcome, error) {
	return AddOutcome{}, nil
}

func (*todoKeepingManager) RewordTodo(manager.Context, ActorProof, TodoID, string) (ChangeOutcome, error) {
	return ChangeOutcome{}, nil
}

func (*todoKeepingManager) RemoveTodo(manager.Context, ActorProof, TodoID) (ChangeOutcome, error) {
	return ChangeOutcome{}, nil
}

func (*todoKeepingManager) MarkTodo(manager.Context, ActorProof, TodoID, bool) (ChangeOutcome, error) {
	return ChangeOutcome{}, nil
}

func (*todoKeepingManager) MarkEveryTodo(manager.Context, ActorProof) (ChangeOutcome, error) {
	return ChangeOutcome{}, nil
}

func (*todoKeepingManager) ClearCompleted(manager.Context, ActorProof) (ChangeOutcome, error) {
	return ChangeOutcome{}, nil
}

func (*todoKeepingManager) ReviewList(manager.Context, ActorProof, View) (ListReview, error) {
	return ListReview{}, nil
}

// WorkerManifest supplies this Manager's workflows to the generated RegisterManagerWorker.
func (m *todoKeepingManager) WorkerManifest() genWorkerManifest {
	return genWorkerManifest{Activities: genActivities{ActorIdentityAccess: m.identities, TodoListAccess: m.todos, MessageBus: m.bus}}
}
`

// RUN-011's wall, at $0: the Manager recorded through the verb generates a constructor that hands
// it its dependencies, and the generated Temporal layer and a hand-written Manager over it vet
// clean.
func TestRun011ManagerGeneratesAndCompiles(t *testing.T) {
	repo := bornRepo(t, run011Recorded(t))
	module := strings.TrimSpace(inRepo(t, repo, "go", "list", "-m"))
	inRepo(t, repo, "make", "gen-models", "CONTRACTS=actorIdentityAccess todoListAccess messageBus todoKeepingManager")
	for file, dest := range map[string]string{
		"actoridentityaccess.go": "internal/resourceaccess/actoridentity/actoridentityaccess.go",
		"todolistaccess.go":      "internal/resourceaccess/todolist/todolistaccess.go",
		"messagebusutility.go":   "internal/utility/messagebus/messagebusutility.go",
	} {
		src, err := os.ReadFile(filepath.Join("testdata", "fresh", "run011", file))
		if err != nil {
			t.Fatal(err)
		}
		writeRepoFile(t, repo, dest, src)
	}
	runGenerators(t, repo, activitySpec(t, repo, run011Activity, "construction"))
	ctor, err := os.ReadFile(filepath.Join(repo, "internal/manager/todokeeping/contract.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "func NewTodoKeepingManager(client client.Client, actorIdentityAccess actoridentity.ActorIdentityAccess, todoListAccess todolist.TodoListAccess, messageBus messagebus.MessageBus) TodoKeepingManager"; !strings.Contains(string(ctor), want) {
		t.Errorf("the generated constructor must take the derived deps:\nwant %s\nin\n%s", want, ctor)
	}
	writeRepoFile(t, repo, "internal/manager/todokeeping/todokeepingmanager.go", []byte(strings.ReplaceAll(run011ManagerImpl, "MODULE", module)))
	inRepo(t, repo, "go", "mod", "tidy")
	if out := inRepo(t, repo, "go", "vet", "./internal/manager/..."); strings.TrimSpace(out) != "" {
		t.Errorf("go vet: %s", out)
	}
	worker, err := os.ReadFile(filepath.Join(repo, "internal/manager/todokeeping/worker.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"actorIdentityAccess.recognizeActor", "todoListAccess.recordTodo", "messageBus.publish"} {
		if !strings.Contains(string(worker), fmt.Sprintf("Name: %q", op)) {
			t.Errorf("worker.gen.go registers no %s activity", op)
		}
	}
}
