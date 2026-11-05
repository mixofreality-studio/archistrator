package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// browserDialectSchema resolves one of the derived browser contract's $defs (on a fresh,
// non-archistrator project) so instances can be validated against it.
func browserDialectSchema(t *testing.T, def string) *jsonschema.Resolved {
	t.Helper()
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(composedState(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	contract := proj.ServiceContracts["webApp"]
	requireDerivedShape(t, contract)
	defs := map[string]*jsonschema.Schema{}
	for name, raw := range contract.Defs {
		var s jsonschema.Schema
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("$defs.%s is not a JSON Schema: %v", name, err)
		}
		defs[name] = &s
	}
	root := &jsonschema.Schema{Ref: "#/$defs/" + def, Defs: defs}
	resolved, err := root.Resolve(nil)
	if err != nil {
		t.Fatalf("resolving %s: %v", def, err)
	}
	return resolved
}

func validates(t *testing.T, r *jsonschema.Resolved, raw string) bool {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return r.Validate(v) == nil
}

// FIX-062: the browser dialect states what a user does and sees. The derived contract's action
// kind enum is closed (check, uncheck, dblclick, hover and press join click, fill and navigate),
// press needs a target, and each observable states exactly one thing, checked and unchecked
// among them. The kinds are the platform's one list (methodcheck.UIActionKinds /
// UIObservableKinds, exported by testgen as what it emits).
//
// EARMARK (FIX-062 condition 3): this test spells the kinds because the pinned platform
// (framework-go v0.20.0, app-generator v0.20.2) predates the exported set. With the release
// that carries it, compare against testgen.UIActionKinds()/UIObservableKinds() instead.
func TestBrowserContractDialect(t *testing.T) {
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(composedState(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Properties struct {
			Kind struct {
				Enum []string `json:"enum"`
			} `json:"kind"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(proj.ServiceContracts["webApp"].Defs["UIAction"], &action); err != nil {
		t.Fatal(err)
	}
	if want := []string{"check", "click", "dblclick", "fill", "hover", "navigate", "press", "uncheck"}; !slices.Equal(action.Properties.Kind.Enum, want) {
		t.Errorf("UIAction kinds %v, the dialect is %v", action.Properties.Kind.Enum, want)
	}
	var observable struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(proj.ServiceContracts["webApp"].Defs["UIObservable"], &observable); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"checked", "text", "unchecked", "url", "visible"} {
		if _, ok := observable.Properties[kind]; !ok {
			t.Errorf("UIObservable has no %q", kind)
		}
	}

	actions := browserDialectSchema(t, "UIAction")
	for _, ok := range []string{
		`{"kind":"click","target":"add"}`,
		`{"kind":"fill","target":"new","value":"milk"}`,
		`{"kind":"fill","target":"edit"}`,
		`{"kind":"navigate","value":"/active"}`,
		`{"kind":"check","target":"toggle"}`,
		`{"kind":"uncheck","target":"toggle"}`,
		`{"kind":"dblclick","target":"title"}`,
		`{"kind":"hover","target":"row"}`,
		`{"kind":"press","target":"edit","value":"Enter"}`,
	} {
		if !validates(t, actions, ok) {
			t.Errorf("the dialect refuses %s", ok)
		}
	}
	for _, bad := range []string{
		`{"kind":"tap","target":"x"}`,                  // outside the closed enum
		`{"kind":"press","value":"Enter"}`,             // a key press needs its element
		`{"kind":"press","target":"edit"}`,             // and its key
		`{"kind":"check"}`,                             // a check needs its box
		`{"kind":"navigate"}`,                          // a route
		`{"kind":"navigate","target":"x","value":"/"}`, // navigate acts on no element
		`{"kind":"click","target":"x","value":"y"}`,    // a click carries no value
		`{"kind":"click","target":"x","key":"y"}`,      // no other field
	} {
		if validates(t, actions, bad) {
			t.Errorf("the dialect accepts %s", bad)
		}
	}

	observables := browserDialectSchema(t, "UIObservation")
	for _, ok := range []string{
		`{"visible":"list"}`,
		`{"checked":"toggle"}`,
		`{"unchecked":"toggle"}`,
		`{"text":"1 item left","target":"count"}`,
		`{"text":"","target":"new-todo"}`, // a cleared field: presence states the kind, as methodcheck reads it
		`{"url":"/active"}`,
		`[{"checked":"toggle"},{"text":"0 items left","target":"count"}]`,
	} {
		if !validates(t, observables, ok) {
			t.Errorf("the dialect refuses %s", ok)
		}
	}
	for _, bad := range []string{
		`{}`,
		`{"visible":"a","checked":"a"}`,
		`{"visible":"a","target":"b"}`,
		`{"text":"x"}`,
		`{"enabled":"a"}`,
		`[{"visible":"a"},{"url":"/","checked":"b"}]`,
	} {
		if validates(t, observables, bad) {
			t.Errorf("the dialect accepts %s", bad)
		}
	}
}
