package main

// machineclient_types_test.go drives every shape a Manager's contract can hand a machine client
// through REST and through MCP separately: the RUN-009 fixture's Manager takes and returns one
// named string, which proves routing and nothing about how a real contract's types cross a
// transport. An inline integer, an object body, an enum, an id in the path, a query of a GET, a
// void result and a refusal each take a different generated path.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const typedManagerContract = `{"component": "listKeepingManager", "layer": "Manager", "goPackage": "internal/manager/listkeeping", "title": "list keeping",
 "deps": [{"name": "recognizer", "component": "recognizingEngine"}, {"name": "todos", "component": "todoListAccess"}, {"name": "bus", "component": "pubSub"}],
 "$defs": {
  "Proof": {"type": "string"},
  "TodoID": {"type": "string"},
  "Filter": {"type": "string", "enum": ["all", "active", "completed"]},
  "Options": {"type": "object", "properties": {"priority": {"type": "integer"}, "tags": {"type": "array", "items": {"type": "string"}}}, "required": ["priority", "tags"], "additionalProperties": false},
  "Todo": {"type": "object", "properties": {"id": {"$ref": "#/$defs/TodoID"}, "title": {"type": "string"}, "completed": {"type": "boolean"}}, "required": ["id", "title", "completed"], "additionalProperties": false},
  "TodoList": {"type": "object", "properties": {"todos": {"type": "array", "items": {"$ref": "#/$defs/Todo"}}, "itemsLeft": {"type": "integer"}}, "required": ["todos", "itemsLeft"], "additionalProperties": false}
 },
 "interface": {"name": "ListKeepingManager", "layer": "manager", "operations": [
  {"name": "AddTodo", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}, {"name": "title", "schema": {"type": "string"}}, {"name": "options", "schema": {"$ref": "#/$defs/Options"}}], "result": {"$ref": "#/$defs/TodoList"}, "error": true},
  {"name": "RewordTodo", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}, {"name": "todoId", "schema": {"$ref": "#/$defs/TodoID"}}, {"name": "title", "schema": {"type": "string"}}], "result": {"$ref": "#/$defs/TodoList"}, "error": true},
  {"name": "RemoveTodo", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}, {"name": "todoId", "schema": {"$ref": "#/$defs/TodoID"}}], "error": true},
  {"name": "ToggleTodo", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}, {"name": "todoId", "schema": {"$ref": "#/$defs/TodoID"}}, {"name": "completed", "schema": {"type": "boolean"}}], "result": {"$ref": "#/$defs/TodoList"}, "error": true},
  {"name": "ToggleAll", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}], "result": {"$ref": "#/$defs/TodoList"}, "error": true},
  {"name": "ClearCompleted", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}], "result": {"type": "integer", "minimum": 0}, "error": true},
  {"name": "ReviewList", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}, {"name": "filter", "schema": {"$ref": "#/$defs/Filter"}}], "result": {"$ref": "#/$defs/TodoList"}, "error": true},
  {"name": "SwitchFilter", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}, {"name": "filter", "schema": {"$ref": "#/$defs/Filter"}}], "result": {"$ref": "#/$defs/TodoList"}, "error": true},
  {"name": "GetItemsLeft", "params": [{"name": "proof", "schema": {"$ref": "#/$defs/Proof"}}, {"name": "limit", "schema": {"type": "integer"}}], "result": {"type": "integer", "minimum": 0}, "error": true}
 ]}}`

const typedManagerMethods = `func one(title string) *TodoList {
	return &TodoList{Todos: []Todo{{Id: "t1", Title: title}}, ItemsLeft: 1}
}

func (*listKeepingManager) AddTodo(_ manager.Context, _ Proof, title string, options Options) (TodoList, error) {
	if options.Priority != 3 || len(options.Tags) != 1 {
		return TodoList{}, manager.New(manager.ContractMisuse, "the object did not cross")
	}
	return *one(title), nil
}

func (*listKeepingManager) RewordTodo(_ manager.Context, _ Proof, todoId TodoID, title string) (TodoList, error) {
	return *one(string(todoId) + ":" + title), nil
}

func (*listKeepingManager) RemoveTodo(_ manager.Context, _ Proof, todoId TodoID) error {
	if todoId == "missing" {
		return manager.New(manager.NotFound, "no such todo")
	}
	return nil
}

func (*listKeepingManager) ToggleTodo(_ manager.Context, _ Proof, todoId TodoID, completed bool) (TodoList, error) {
	return TodoList{Todos: []Todo{{Id: todoId, Title: "x", Completed: completed}}, ItemsLeft: 0}, nil
}

func (*listKeepingManager) ToggleAll(manager.Context, Proof) (TodoList, error) {
	return *one("all"), nil
}

func (*listKeepingManager) ClearCompleted(manager.Context, Proof) (int64, error) {
	return 7, nil
}

func (*listKeepingManager) ReviewList(_ manager.Context, _ Proof, filter Filter) (TodoList, error) {
	return *one(string(filter)), nil
}

func (*listKeepingManager) SwitchFilter(_ manager.Context, _ Proof, filter Filter) (TodoList, error) {
	return *one(string(filter)), nil
}

func (*listKeepingManager) GetItemsLeft(_ manager.Context, _ Proof, limit int64) (int64, error) {
	return limit + 1, nil
}`

// typedStep is what each operation is called with and what the actor sees. The result is the
// bare value: the MCP case wraps it, because a tool's result sits under "result".
var typedSteps = map[string]struct {
	inputs [][2]string
	result string
	code   string
}{
	"AddTodo":        {[][2]string{{"title", `"buy milk"`}, {"options", `{"priority":3,"tags":["home"]}`}}, `{"todos":[{"title":"buy milk"}],"itemsLeft":1}`, ""},
	"RewordTodo":     {[][2]string{{"todoId", `"t 1/x"`}, {"title", `"new"`}}, `{"todos":[{"title":"t 1/x:new"}]}`, ""},
	"RemoveTodo":     {[][2]string{{"todoId", `"missing"`}}, "", "NotFound"},
	"ToggleTodo":     {[][2]string{{"todoId", `"t1"`}, {"completed", `true`}}, `{"todos":[{"id":"t1","completed":true}]}`, ""},
	"ToggleAll":      {nil, `{"itemsLeft":1}`, ""},
	"ClearCompleted": {nil, `7`, ""},
	"ReviewList":     {[][2]string{{"filter", `"active"`}}, `{"todos":[{"title":"active"}]}`, ""},
	"SwitchFilter":   {[][2]string{{"filter", `"completed"`}}, `{"todos":[{"title":"completed"}]}`, ""},
	"GetItemsLeft":   {[][2]string{{"limit", `41`}}, `42`, ""},
}

func typedState(t *testing.T) []byte {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(run009State(t), &top); err != nil {
		t.Fatal(err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		t.Fatal(err)
	}
	contracts["listKeepingManager"] = json.RawMessage(typedManagerContract)
	delete(contracts, "webApi")
	delete(contracts, "mcpServer")
	raw, err := json.Marshal(contracts)
	if err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = raw
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return withBrowserContracts(t, out)
}

// recordTypedPlan binds every projected scenario of a client to typedSteps.
func recordTypedPlan(t *testing.T, repo, contract string) {
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
	var bindings []map[string]any
	for _, s := range scenarios {
		var steps []map[string]any
		for _, st := range s.Stimuli {
			want, ok := typedSteps[strings.TrimPrefix(st.Input.Op, "ListKeeping")]
			if !ok {
				t.Fatalf("%s step %d: no typed step for operation %q", s.ID, st.Seq, st.Input.Op)
			}
			inputs := []map[string]any{{"name": "proof", "value": `"proof-value"`}}
			for _, in := range want.inputs {
				inputs = append(inputs, map[string]any{"name": in[0], "value": in[1]})
			}
			expect := map[string]any{}
			switch {
			case want.code != "":
				expect["errorExpected"], expect["errorCode"] = true, want.code
			case contract == "mcpServer":
				expect["result"] = `{"result":` + want.result + `}`
			default:
				expect["result"] = want.result
			}
			steps = append(steps, map[string]any{"seq": st.Seq, "operation": st.Input.Op, "inputs": inputs, "expect": expect, "unobservable": run009Unobservable(st)})
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

// Every shape crosses each transport on its own, and the actor sees the result or the refusal the
// plan says, spelled the way the Manager's error model spells it.
func TestMachineClientsCarryEveryContractShape(t *testing.T) {
	repo := bornRepo(t, typedState(t))
	buildRun009Components(t, repo, typedManagerMethods)
	integrate := activitySpec(t, repo, run009Activity, "integration")
	runGenerators(t, repo, integrate)
	inRepo(t, repo, "make", "gen-tests")
	inRepo(t, repo, "go", "mod", "tidy")
	fillPolicy(t, repo, ownScopePolicy)
	inRepo(t, repo, "git", "add", "-A")
	inRepo(t, repo, "git", "commit", "-q", "-m", "integrate the manager")

	for _, tc := range []struct{ activity, contract, pkg, ctor string }{
		{"C-web-api", "webApi", "internal/client/web/listkeeping", run009RESTSubject},
		{"C-mcp-server", "mcpServer", "internal/client/mcpserver/listkeeping", run009MCPSubject},
	} {
		t.Run(tc.contract, func(t *testing.T) {
			recordTypedPlan(t, repo, tc.contract)
			spec := activitySpec(t, repo, tc.activity, "integration")
			runGenerators(t, repo, spec)
			inRepo(t, repo, "make", "gen-tests")
			module := strings.TrimSpace(inRepo(t, repo, "go", "list", "-m"))
			hooks := strings.NewReplacer("MODULE", module, "CLIENTPKG", tc.pkg, "SUBJECT", tc.ctor).Replace(run009ClientHooks)
			writeRepoFile(t, repo, tc.pkg+"/client_hooks_test.go", []byte(hooks))
			inRepo(t, repo, "go", "mod", "tidy")
			inRepo(t, repo, "git", "add", "-A")
			inRepo(t, repo, "git", "commit", "-q", "-m", "bind "+tc.contract)
			statuses, logs := gate(t, repo, activitySpec(t, repo, tc.activity, "integration"))
			requirePassed(t, statuses, logs)
		})
	}
}
