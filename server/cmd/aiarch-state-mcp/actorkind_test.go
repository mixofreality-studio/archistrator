package main

// actorkind_test.go — the app-side schema mirror of methodcheck's Actor.Kind and
// TestPrincipal.OnBehalfOf (FPC plan 2, Task 2.13c decision 3, GC-5). The $defs the app
// carries for projectStateAccess are what an agent's recordTestPlan / use-case drafts are
// validated against, so they must say what methodcheck decodes: a closed actor kind and an
// optional delegation subject on a test principal.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ownDefSchema resolves one of projectStateAccess's $defs from archistrator's own project.json.
func ownDefSchema(t *testing.T, def string) *jsonschema.Resolved {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(raw, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	defs := map[string]*jsonschema.Schema{}
	for name, d := range proj.ServiceContracts["projectStateAccess"].Defs {
		var s jsonschema.Schema
		if err := json.Unmarshal(d, &s); err != nil {
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

func TestTestPrincipal_OnBehalfOfRoundTrips(t *testing.T) {
	var pr projectstate.TestPrincipal
	if err := json.Unmarshal([]byte(`{"subject":"agent-1","roles":["architect"],"onBehalfOf":"alice"}`), &pr); err != nil {
		t.Fatal(err)
	}
	if pr.OnBehalfOf == nil || *pr.OnBehalfOf != "alice" {
		t.Fatalf("onBehalfOf = %v, want alice", pr.OnBehalfOf)
	}
	out, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back["onBehalfOf"] != "alice" {
		t.Fatalf("marshalled principal = %s, want onBehalfOf alice", out)
	}
	// Absent stays absent: a human principal carries no delegation claim.
	out, _ = json.Marshal(projectstate.TestPrincipal{Subject: "alice", Roles: []string{}})
	if json.Valid(out) && string(out) != `{"subject":"alice","roles":[]}` {
		t.Fatalf("a principal with no delegation marshals %s", out)
	}

	schema := ownDefSchema(t, "TestPrincipal")
	if !validates(t, schema, `{"subject":"agent-1","roles":[],"onBehalfOf":"alice"}`) {
		t.Error("TestPrincipal schema refuses onBehalfOf")
	}
	if !validates(t, schema, `{"subject":"alice","roles":[]}`) {
		t.Error("TestPrincipal schema must keep onBehalfOf optional")
	}
	if validates(t, schema, `{"subject":"agent-1","roles":[],"onBehalfOf":7}`) {
		t.Error("TestPrincipal schema accepts a non-string onBehalfOf")
	}
}

// A delegated agent's intents are what the delegating human lets it do (FPC plan 3, Task 3.0): the
// scenario host writes them into the agent's Claims["delegations"] grant, and a grant with no
// intents has no reach. The app's $defs mirror must carry them, or the codec drops the field.
func TestTestPrincipal_IntentsRoundTrips(t *testing.T) {
	var pr projectstate.TestPrincipal
	if err := json.Unmarshal([]byte(`{"subject":"agent-1","roles":["agent"],"onBehalfOf":"alice","intents":["read","add"]}`), &pr); err != nil {
		t.Fatal(err)
	}
	if len(pr.Intents) != 2 || pr.Intents[0] != "read" || pr.Intents[1] != "add" {
		t.Fatalf("intents = %v, want [read add]", pr.Intents)
	}
	out, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Intents []string `json:"intents"`
	}
	if err := json.Unmarshal(out, &back); err != nil || len(back.Intents) != 2 {
		t.Fatalf("marshalled principal = %s, want both intents (%v)", out, err)
	}

	schema := ownDefSchema(t, "TestPrincipal")
	if !validates(t, schema, `{"subject":"agent-1","roles":[],"onBehalfOf":"alice","intents":["read"]}`) {
		t.Error("TestPrincipal schema refuses intents")
	}
	if !validates(t, schema, `{"subject":"agent-1","roles":[],"onBehalfOf":"alice"}`) {
		t.Error("TestPrincipal schema must keep intents optional")
	}
	if validates(t, schema, `{"subject":"agent-1","roles":[],"onBehalfOf":"alice","intents":"read"}`) {
		t.Error("TestPrincipal schema accepts intents that are not a list")
	}
	if validates(t, schema, `{"subject":"agent-1","roles":[],"onBehalfOf":"alice","intents":[7]}`) {
		t.Error("TestPrincipal schema accepts a non-string intent")
	}
}

func TestActorKind_RefusesUnknown(t *testing.T) {
	schema := ownDefSchema(t, "Actor")
	for _, kind := range []string{"human", "agent", "system"} {
		if !validates(t, schema, `{"id":"a","role":"Architect","kind":"`+kind+`"}`) {
			t.Errorf("Actor schema refuses kind %q", kind)
		}
	}
	for _, kind := range []string{"robot", "", "Agent", "bot"} {
		if validates(t, schema, `{"id":"a","role":"Architect","kind":"`+kind+`"}`) {
			t.Errorf("Actor schema accepts the unknown kind %q", kind)
		}
	}
	if validates(t, schema, `{"id":"a","role":"Architect","kind":"human","extra":1}`) {
		t.Error("Actor schema must stay closed (additionalProperties false)")
	}
	var a projectstate.Actor
	if err := json.Unmarshal([]byte(`{"id":"a","role":"R","kind":"agent"}`), &a); err != nil || a.Kind == nil || *a.Kind != projectstate.ActorKindAgent {
		t.Errorf("projectstate.Actor.Kind = %v (err %v), want agent", a.Kind, err)
	}
}
