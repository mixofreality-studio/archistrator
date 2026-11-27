package main

// readonlychain_test.go — ISS-033-1: a Manager's operation, and a surface's, is read-only by its
// designed call chain, never by its name. The project is todomvc-shaped: a Manager whose view and
// count operations only ask an Engine for permission and recall the list, and whose AddTodo also
// records a todo and publishes.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	roManager = "list-manager"
	roWeb     = "todo-web-app"
	roRest    = "todo-api"
)

// roCall is one call of a dynamic view step.
type roCall struct{ from, to, label string }

// roUseCase is one use case: an actor asks a client for op, the client calls the Manager, and the
// Manager makes managerCalls.
type roUseCase struct {
	id           string
	op           string // the actor's label into the clients, e.g. "ViewList"
	managerLabel string // the client's call into the Manager
	managerCalls []roCall
	clients      []string
	// clientCalls are calls each client also makes into something else in the same step as its
	// call into the Manager (RUN-033: subscribe(ListChanged) into the message bus), drawn between
	// the client's call and the Manager's own.
	clientCalls []roCall
}

func roUseCases() []roUseCase {
	both := []string{roWeb, roRest}
	permit := roCall{roManager, "access-granting-engine", "Permit(principal, owner, intent)"}
	retrieve := roCall{roManager, "todo-list-access", "retrieveList(owner) → todos"}
	return []roUseCase{
		{"view-list", "ViewList", "viewList(view) → todos", []roCall{permit, retrieve}, both,
			[]roCall{{"", "message-bus", "subscribe(ListChanged)"}}},
		{"count-items-left", "CountItemsLeft", "countItemsLeft() → count", []roCall{permit, retrieve}, both, nil},
		{"add-todo", "AddTodo", "addTodo(wording) → todo", []roCall{permit,
			{roManager, "todo-list-access", "recordTodos(owner, todos)"},
			{roManager, "message-bus", "publish(ListChanged)"}}, both, nil},
		{"get-stale", "GetStale", "getStale() → todos", []roCall{permit,
			{roManager, "todo-list-access", "discardTodos(owner, ids)"}}, nil, nil},
		// The Manager only asks the Engine, but the Engine records: a write through an Engine.
		{"sync-todos", "SyncTodos", "syncTodos() → todos", []roCall{permit,
			{"access-granting-engine", "todo-list-access", "recordTodos(owner, todos)"}}, both, nil},
		{"peek-list", "PeekList", "peekList() → todos", []roCall{permit,
			{roManager, "todo-list-access", "frobnicate(owner)"}}, nil, nil},
	}
}

func roUseCaseJSON(uc roUseCase) map[string]any {
	node := func(id, kind, label, role string, actor any) map[string]any {
		return map[string]any{"id": id, "kind": kind, "label": label, "roleName": role, "linkedActorId": actor}
	}
	edge := func(from, to string) map[string]any {
		return map[string]any{"from": from, "to": to, "kind": "controlFlow", "guard": ""}
	}
	return map[string]any{
		"id": uc.id, "name": uc.id, "trigger": "clientAction", "classification": "core", "variationOf": nil,
		"actors": []any{map[string]any{"id": "team-member", "role": "Team Member"}},
		"activity": map[string]any{
			"nodes": []any{
				node("n1", "start", "", "Team Member", "team-member"),
				node("n1a", "action", "Ask", "Team Member", "team-member"),
				node("n2", "action", "Answer", "List Manager", nil),
				node("n3", "end", "", "", nil),
			},
			"edges": []any{edge("n1", "n1a"), edge("n1a", "n2"), edge("n2", "n3")},
		},
	}
}

func roViewJSON(uc roUseCase) map[string]any {
	call := func(c roCall) map[string]any {
		return map[string]any{"from": c.from, "to": c.to, "mode": "sync", "label": c.label}
	}
	var actor, managerStep []any
	for _, client := range uc.clients {
		actor = append(actor, call(roCall{"team-member", client, uc.op + " — asks"}))
		managerStep = append(managerStep, call(roCall{client, roManager, uc.managerLabel}))
		for _, c := range uc.clientCalls {
			managerStep = append(managerStep, call(roCall{client, c.to, c.label}))
		}
	}
	for _, c := range uc.managerCalls {
		managerStep = append(managerStep, call(c))
	}
	steps := []any{}
	if len(actor) > 0 {
		steps = append(steps, map[string]any{"activityNodeId": "n1a", "calls": actor})
	}
	if len(uc.clients) == 0 {
		// No client in the design reaches this op: the actor asks the Manager directly.
		managerStep = append([]any{call(roCall{"team-member", roManager, uc.managerLabel})}, managerStep...)
	}
	steps = append(steps, map[string]any{"activityNodeId": "n2", "calls": managerStep})
	return map[string]any{"useCaseId": uc.id, "key": uc.id, "title": uc.id, "steps": steps}
}

func roComponent(id, name, kind, key, surface string) map[string]any {
	c := map[string]any{"id": id, "name": name, "kind": kind, "layer": kind, "encapsulates": "x", "atomicBusinessVerbs": nil}
	if key != "" {
		c["contractKey"] = key
	}
	if surface != "" {
		c["surface"] = surface
	}
	return c
}

// roProject is the todomvc-shaped project: the conformance fixture's frame, with use cases and a
// System of this test's own, and the leaf contracts recorded as the verb records them.
func roProject(t *testing.T, useCases []roUseCase) projectstate.Project {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(freshState(t), &top); err != nil {
		t.Fatal(err)
	}
	var slots map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["slots"], &slots); err != nil {
		t.Fatal(err)
	}
	var decisions, views []any
	for _, uc := range useCases {
		decisions = append(decisions, map[string]any{"rejectionReason": "", "essenceRationale": "x", "useCase": roUseCaseJSON(uc)})
		views = append(views, roViewJSON(uc))
	}
	system := map[string]any{
		"components": []any{
			roComponent(roWeb, "TodoWebApp", "client", "todoWebApp", "web"),
			roComponent(roRest, "TodoApi", "client", "todoApi", "rest"),
			roComponent(roManager, "ListManager", "manager", "listManager", ""),
			roComponent("access-granting-engine", "AccessGrantingEngine", "engine", "accessGrantingEngine", ""),
			roComponent("todo-list-access", "TodoListAccess", "resourceAccess", "todoListAccess", ""),
			roComponent("todo-list-store", "TodoListStore", "resource", "", ""),
			roComponent("message-bus", "MessageBus", "utility", "messageBus", ""),
		},
		"relationships": []any{},
		"dynamicViews":  views,
	}
	var err error
	if slots["4"]["model"], err = json.Marshal(map[string]any{"decisions": decisions}); err != nil {
		t.Fatal(err)
	}
	if slots["5"]["model"], err = json.Marshal(system); err != nil {
		t.Fatal(err)
	}
	if top["slots"], err = json.Marshal(slots); err != nil {
		t.Fatal(err)
	}
	top["serviceContracts"] = json.RawMessage(`{}`)
	state, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	proj, _, err := projectstate.DecodeProjectJSON(state, "")
	if err != nil {
		t.Fatal(err)
	}
	proj.ServiceContracts = map[string]projectstate.ServiceContract{}
	return proj
}

func roOps(layer, iface string, names ...string) projectstate.ServiceContract {
	sc := projectstate.ServiceContract{Layer: layer}
	sc.Interface.Name = iface
	for _, n := range names {
		sc.Interface.Operations = append(sc.Interface.Operations, projectstate.ContractOperation{
			Name:   n,
			Params: []projectstate.ContractParam{{Name: "owner", Schema: json.RawMessage(`{"type":"string"}`)}},
			Result: json.RawMessage(`{"type":"string"}`),
			Error:  true,
		})
	}
	return sc
}

// roRecordLeaves records the Engine, ResourceAccess and Utility the way the verb does: their flags
// from their names.
func roRecordLeaves(p *projectstate.Project) {
	for key, sc := range map[string]projectstate.ServiceContract{
		"accessGrantingEngine": roOps("Engine", "AccessGrantingEngine", "Permit"),
		"todoListAccess":       roOps("ResourceAccess", "TodoListAccess", "RetrieveList", "RecordTodos", "DiscardTodos"),
		"messageBus":           roOps("Utility", "MessageBus", "Publish", "Subscribe"),
	} {
		sc.Component = key
		markReadOnlyOperations(&sc)
		p.ServiceContracts[key] = sc
	}
}

func roManagerContract() projectstate.ServiceContract {
	return roOps("Manager", "TodoListManager", "ViewList", "CountItemsLeft", "AddTodo", "GetStale", "ListOrphans", "PeekList", "SyncTodos")
}

// roFlags is name → readOnly of a recorded contract's operations.
func roFlags(p projectstate.Project, key string) map[string]bool {
	out := map[string]bool{}
	for _, o := range p.ServiceContracts[key].Interface.Operations {
		out[o.Name] = o.ReadOnly
	}
	return out
}

func roRequire(t *testing.T, what string, got map[string]bool, want map[string]bool) {
	t.Helper()
	for op, w := range want {
		g, ok := got[op]
		if !ok {
			t.Errorf("%s: no operation %s (have %v)", what, op, got)
			continue
		}
		if g != w {
			t.Errorf("%s: %s readOnly = %v, want %v", what, op, g, w)
		}
	}
}

func TestManagerReadOnlyComesFromItsCallChain(t *testing.T) {
	p := roProject(t, roUseCases())
	roRecordLeaves(&p)
	if err := recordManagerContract(&p, roManager, "listManager", roManagerContract()); err != nil {
		t.Fatal(err)
	}
	roRequire(t, "Manager", roFlags(p, "listManager"), map[string]bool{
		"ViewList":       true,
		"CountItemsLeft": true,
		"AddTodo":        false, // records a todo and publishes
		"GetStale":       false, // its name says read; its chain discards
		"ListOrphans":    false, // on no path: nothing says it only observes
		"PeekList":       false, // a segment with a label no operation resolves
		"SyncTodos":      false, // the Manager only asks the Engine, which records the todos
	})
	// The machine client copies the flags the Manager now carries; none is set by hand.
	roRequire(t, "rest client", roFlags(p, "todoApi"), map[string]bool{
		"TodoListViewList":       true,
		"TodoListCountItemsLeft": true,
		"TodoListAddTodo":        false,
	})
}

func TestBrowserReadOnlyComesFromTheManagerOpsItFansInto(t *testing.T) {
	want := map[string]bool{"ViewList": true, "CountItemsLeft": true, "AddTodo": false}

	t.Run("surface recorded after the Manager", func(t *testing.T) {
		p := roProject(t, roUseCases())
		roRecordLeaves(&p)
		if err := recordManagerContract(&p, roManager, "listManager", roManagerContract()); err != nil {
			t.Fatal(err)
		}
		if err := projectstate.RecordBrowserContract(&p, roWeb); err != nil {
			t.Fatal(err)
		}
		roRequire(t, "web", roFlags(p, "todoWebApp"), want)
	})

	t.Run("surface recorded before the Manager", func(t *testing.T) {
		p := roProject(t, roUseCases())
		roRecordLeaves(&p)
		if err := projectstate.RecordBrowserContract(&p, roWeb); err != nil {
			t.Fatal(err)
		}
		roRequire(t, "web before the Manager", roFlags(p, "todoWebApp"),
			map[string]bool{"ViewList": false, "CountItemsLeft": false, "AddTodo": false})
		if err := recordManagerContract(&p, roManager, "listManager", roManagerContract()); err != nil {
			t.Fatal(err)
		}
		roRequire(t, "web after the Manager", roFlags(p, "todoWebApp"), want)
	})
}

func TestManagerReadOnlyFailsClosed(t *testing.T) {
	t.Run("a callee with no recorded contract", func(t *testing.T) {
		p := roProject(t, roUseCases())
		roRecordLeaves(&p)
		delete(p.ServiceContracts, "todoListAccess") // the derivation drops a call into it
		if err := recordManagerContract(&p, roManager, "listManager", roManagerContract()); err != nil {
			t.Fatal(err)
		}
		roRequire(t, "Manager", roFlags(p, "listManager"), map[string]bool{"ViewList": false, "CountItemsLeft": false})
	})

	t.Run("a call into another Manager", func(t *testing.T) {
		cases := roUseCases()
		cases[0].managerCalls = append(cases[0].managerCalls, roCall{roManager, "access-granting-engine", "Permit(a, b, c)"})
		p := roProject(t, cases)
		roRecordLeaves(&p)
		// Rebind the Engine id to a Manager contract: the callee is a Manager, so a write.
		sys := p.SystemDesign.Model.(*projectstate.System)
		for i := range sys.Components {
			if sys.Components[i].ID == "access-granting-engine" {
				sys.Components[i].Kind = projectstate.CompManager
			}
		}
		if err := recordManagerContract(&p, roManager, "listManager", roManagerContract()); err != nil {
			t.Fatal(err)
		}
		roRequire(t, "Manager", roFlags(p, "listManager"), map[string]bool{"ViewList": false})
	})

	t.Run("a derivation that fails refuses, naming the Manager", func(t *testing.T) {
		cases := roUseCases()
		p := roProject(t, cases)
		cu := p.CoreUseCases.Model.(*projectstate.CoreUseCases)
		cu.Decisions[0].UseCase.Activity.Edges = append(cu.Decisions[0].UseCase.Activity.Edges,
			projectstate.ActivityEdge{From: "n2", To: "nowhere"})
		err := recordManagerContract(&p, roManager, "listManager", roManagerContract())
		if err == nil || !strings.Contains(err.Error(), roManager) {
			t.Fatalf("want a refusal naming %q, got %v", roManager, err)
		}
	})
}

func TestMarkReadOnlyOperationsLeavesAManagerToItsCallChain(t *testing.T) {
	sc := roManagerContract()
	for i := range sc.Interface.Operations {
		sc.Interface.Operations[i].ReadOnly = true
	}
	markReadOnlyOperations(&sc)
	for _, o := range sc.Interface.Operations {
		if o.ReadOnly {
			t.Errorf("%s: the name classifier must not mark a Manager's operation", o.Name)
		}
	}
}
