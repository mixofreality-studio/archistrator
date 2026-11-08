package main

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/testgen"

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
	// The fixture holds the contract RUN-021 recorded; the dialect under test is what the verb
	// derives now.
	delete(proj.ServiceContracts, "todoWebApp")
	if err := projectstate.RecordBrowserContract(&proj, "todo-web-app"); err != nil {
		t.Fatal(err)
	}
	contract := proj.ServiceContracts["todoWebApp"]
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

// FPC plan 2: the browser dialect is the platform's sum-typed UIAction / UIObservable
// ({"kind","model"} envelopes over a design element target), and the contract's $defs speak
// exactly the kinds the platform decodes and testgen emits (methodcheck.UIActionKinds /
// UIObservableKinds, exported by testgen). A step observes an ARRAY of observables with at least
// one: an empty array would assert nothing.
func TestBrowserContractDialect(t *testing.T) {
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(composedState(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture holds the contract RUN-021 recorded; the dialect under test is what the verb
	// derives now.
	delete(proj.ServiceContracts, "todoWebApp")
	if err := projectstate.RecordBrowserContract(&proj, "todo-web-app"); err != nil {
		t.Fatal(err)
	}
	kindsOf := func(def string) []string {
		var d struct {
			OneOf []struct {
				Properties struct {
					Kind struct {
						Const string `json:"const"`
					} `json:"kind"`
				} `json:"properties"`
			} `json:"oneOf"`
		}
		if err := json.Unmarshal(proj.ServiceContracts["todoWebApp"].Defs[def], &d); err != nil {
			t.Fatal(err)
		}
		var kinds []string
		for _, v := range d.OneOf {
			kinds = append(kinds, v.Properties.Kind.Const)
		}
		slices.Sort(kinds)
		return kinds
	}
	if got, want := kindsOf("UIAction"), testgen.UIActionKinds(); !slices.Equal(got, want) {
		t.Errorf("UIAction kinds %v, the platform emits %v", got, want)
	}
	if got, want := kindsOf("UIObservable"), testgen.UIObservableKinds(); !slices.Equal(got, want) {
		t.Errorf("UIObservable kinds %v, the platform emits %v", got, want)
	}

	actions := browserDialectSchema(t, "UIAction")
	for _, ok := range []string{
		`{"kind":"click","model":{"target":{"element":"add"}}}`,
		`{"kind":"fill","model":{"target":{"element":"new"},"value":"milk"}}`,
		`{"kind":"fill","model":{"target":{"element":"edit"},"value":""}}`,
		`{"kind":"navigate","model":{"route":"#/active"}}`,
		`{"kind":"check","model":{"target":{"element":"toggle","nth":1}}}`,
		`{"kind":"uncheck","model":{"target":{"element":"toggle"}}}`,
		`{"kind":"dblclick","model":{"target":{"element":"title"}}}`,
		`{"kind":"hover","model":{"target":{"element":"row"}}}`,
		`{"kind":"press","model":{"target":{"element":"edit"},"key":"Enter"}}`,
		`{"kind":"select","model":{"target":{"element":"filter"},"option":"Active"}}`,
	} {
		if !validates(t, actions, ok) {
			t.Errorf("the dialect refuses %s", ok)
		}
	}
	for _, bad := range []string{
		`{"kind":"tap","model":{"target":{"element":"x"}}}`,                  // outside the closed set
		`{"kind":"click","target":"x"}`,                                      // the v1 flat form
		`{"kind":"click","model":{"target":"x"}}`,                            // a target is an element reference
		`{"kind":"click","model":{"target":{"element":"x","nth":-1}}}`,       // nth is an index
		`{"kind":"press","model":{"target":{"element":"edit"}}}`,             // a key press needs its key
		`{"kind":"check","model":{}}`,                                        // a check needs its box
		`{"kind":"navigate","model":{}}`,                                     // a route
		`{"kind":"navigate","model":{"route":"/","target":{"element":"x"}}}`, // navigate acts on no element
		`{"kind":"click","model":{"target":{"element":"x"},"value":"y"}}`,    // a click carries no value
		`{"kind":"fill","model":{"target":{"element":"x"}}}`,                 // a fill states its text, even ""
	} {
		if validates(t, actions, bad) {
			t.Errorf("the dialect accepts %s", bad)
		}
	}

	observables := browserDialectSchema(t, "UIObservation")
	for _, ok := range []string{
		`[{"kind":"visible","model":{"target":{"element":"list"}}}]`,
		`[{"kind":"checked","model":{"target":{"element":"toggle","nth":0}}}]`,
		`[{"kind":"unchecked","model":{"target":{"element":"toggle"}}}]`,
		`[{"kind":"text","model":{"target":{"element":"count"},"text":"1 item left"}}]`,
		`[{"kind":"text","model":{"target":{"element":"new-todo"},"text":""}}]`, // a cleared field
		`[{"kind":"value","model":{"target":{"element":"new-todo"},"value":""}}]`,
		`[{"kind":"count","model":{"target":{"element":"todo-item"},"n":0}}]`,
		`[{"kind":"url","model":{"route":"#/active"}}]`,
		`[{"kind":"checked","model":{"target":{"element":"toggle"}}},{"kind":"absent","model":{"target":{"element":"banner"}}}]`,
	} {
		if !validates(t, observables, ok) {
			t.Errorf("the dialect refuses %s", ok)
		}
	}
	for _, bad := range []string{
		`[]`, // asserts nothing
		`{"kind":"visible","model":{"target":{"element":"list"}}}`, // one observable is a one-element array
		`[{"kind":"enabled","model":{"target":{"element":"a"}},"extra":1}]`,
		`[{"kind":"visible","model":{"target":{"element":"a"},"text":"b"}}]`,
		`[{"kind":"text","model":{"target":{"element":"a"}}}]`,  // text states its text
		`[{"kind":"count","model":{"target":{"element":"a"}}}]`, // a count states n
		`[{"kind":"count","model":{"target":{"element":"a"},"n":-1}}]`,
		`[{"kind":"blinking","model":{"target":{"element":"a"}}}]`,
		`[{"visible":"a"}]`, // the v1 form
	} {
		if validates(t, observables, bad) {
			t.Errorf("the dialect accepts %s", bad)
		}
	}
}
