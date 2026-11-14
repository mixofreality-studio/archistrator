package internal_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// A Client's surface (FPC W6) picks its activity: web and mcpApp are hand-built under webApp/src
// (a U-SPA-* frontend activity), rest and mcpTools are generated machine clients (a C-* activity
// that walks the machineClient lifecycle), and a planned Client derives nothing.
func TestDerivedPlan_SurfacesSelectActivityType(t *testing.T) {
	system := estimation.SystemView{Components: []estimation.SystemComponent{
		{ID: "w", Name: "WebApp", Kind: "client", Surface: "web"},
		{ID: "a", Name: "TodoApp", Kind: "client", Surface: "mcpApp"},
		{ID: "r", Name: "RestApi", Kind: "client", Surface: "rest"},
		{ID: "m", Name: "McpServer", Kind: "client", Surface: "mcpTools"},
		{ID: "p", Name: "LaterApp", Kind: "client", Surface: "mcpApp", BuildStatus: "planned"},
		{ID: "list-manager", Name: "ListManager", Kind: "manager"},
	}}
	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, system, estimation.ActivityListDeltas{})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]estimation.DerivedActivity{}
	for _, a := range plan.Activities {
		byName[a.Name] = a
	}
	for name, wantSurface := range map[string]string{"U-SPA-w": "web", "U-SPA-a": "mcpApp", "C-r": "rest", "C-m": "mcpTools"} {
		a, ok := byName[name]
		if !ok {
			t.Fatalf("no %s in %+v", name, plan.Activities)
		}
		if a.Surface != wantSurface {
			t.Errorf("%s carries surface %q, want %q", name, a.Surface, wantSurface)
		}
	}
	if got := byName["U-SPA-a"].Title; !strings.HasSuffix(got, "(MCP App)") {
		t.Errorf("U-SPA-a title %q must end (MCP App)", got)
	}
	if got := byName["U-SPA-w"].Title; !strings.HasSuffix(got, "(SPA)") {
		t.Errorf("U-SPA-w title %q must end (SPA)", got)
	}
	for _, name := range []string{"U-SPA-p", "C-p", "C-w", "C-a"} {
		if _, ok := byName[name]; ok {
			t.Errorf("%s must not be derived", name)
		}
	}
	for name, want := range map[string]projectstate.ActivityType{
		"U-SPA-w": projectstate.ActivityTypeFrontend,
		"U-SPA-a": projectstate.ActivityTypeFrontend,
		"C-r":     projectstate.ActivityTypeMachineClient,
		"C-m":     projectstate.ActivityTypeMachineClient,
	} {
		a := byName[name]
		got, _, err := projectstate.ClassifyActivity(a.Name, a.WorkerClass, a.Coding, projectstate.Surface(a.Surface))
		if err != nil || got != want {
			t.Errorf("ClassifyActivity(%s) = %v, %v; want %v", name, got, err, want)
		}
	}
}

// A surface outside the closed set fails closed: the Engine reports it instead of quietly deriving a
// machine client activity that carries the bad value.
func TestDerivedPlan_UnknownSurfaceFailsClosed(t *testing.T) {
	for _, bad := range []string{"mcp", "Web"} {
		system := estimation.SystemView{Components: []estimation.SystemComponent{
			{ID: "x", Name: "OddClient", Kind: "client", Surface: bad},
		}}
		_, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, system, estimation.ActivityListDeltas{})
		if err == nil || !strings.Contains(err.Error(), bad) {
			t.Errorf("surface %q: want an error naming it, got %v", bad, err)
		}
	}
}

// ownProjectDoc reads the own project.json as a generic document.
func ownProjectDoc(t *testing.T) (map[string]any, []byte) {
	t.Helper()
	raw, err := os.ReadFile("../../.aiarch/state/project.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc, raw
}

// ownComponent is the own System's component id, as a generic object.
func ownComponent(t *testing.T, doc map[string]any, id string) map[string]any {
	t.Helper()
	model := doc["slots"].(map[string]any)["5"].(map[string]any)["model"].(map[string]any)
	for _, c := range model["components"].([]any) {
		if m := c.(map[string]any); m["id"] == id {
			return m
		}
	}
	t.Fatalf("no component %s", id)
	return nil
}

func decodeDoc(t *testing.T, doc map[string]any) error {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = projectstate.DecodeProjectJSON(b, "")
	return err
}

// The schema is strict: the retired booleans and an unknown surface are refused, and the own
// project, as committed, states its Clients' surfaces.
func TestSurfaceSchema_DecodesStrictly(t *testing.T) {
	doc, raw := ownProjectDoc(t)
	ownComponent(t, doc, "web-client")["surface"] = "mcp"
	if err := decodeDoc(t, doc); err == nil {
		t.Error("surface \"mcp\" must be refused")
	}
	doc, _ = ownProjectDoc(t)
	ownComponent(t, doc, "web-client")["uiSurface"] = true
	if err := decodeDoc(t, doc); err == nil || !strings.Contains(err.Error(), "uiSurface") {
		t.Errorf("the retired uiSurface must be refused naming it, got %v", err)
	}

	p, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil || !ok {
		t.Fatalf("the own project must decode: ok=%v err=%v", ok, err)
	}
	sys, ok := p.SystemDesign.Model.(*projectstate.System)
	if !ok {
		t.Fatalf("the own system slot decoded to %T", p.SystemDesign.Model)
	}
	want := map[string]projectstate.Surface{"web-client": projectstate.SurfaceWeb, "mcp-client": projectstate.SurfaceMCPTools, "scheduler-client": ""}
	for _, c := range sys.Components {
		w, ok := want[c.ID]
		if !ok {
			continue
		}
		got := projectstate.Surface("")
		if c.Surface != nil {
			got = *c.Surface
		}
		if got != w {
			t.Errorf("%s surface = %q, want %q", c.ID, got, w)
		}
		delete(want, c.ID)
	}
	if len(want) != 0 {
		t.Errorf("components not found: %v", want)
	}
}

// The record verb holds the mission's deliverySurfaces to what the brief committed to: non-emptiness
// lives in Go, not in the schema (GC-1).
func TestMissionDeliverySurfaces_RecordVerb(t *testing.T) {
	objectives := []projectstate.Objective{{Number: 1, Statement: "one"}, {Number: 2, Statement: "two"}}
	valid := func() *projectstate.MissionStatement {
		return &projectstate.MissionStatement{
			Vision: "v", Mission: "m", Objectives: objectives,
			DeliverySurfaces: []projectstate.DeliverySurface{
				{Kind: projectstate.SurfaceWeb, Audience: projectstate.AudienceHuman, Source: "host a UI portal", Objective: 1},
				{Kind: projectstate.SurfaceMCPTools, Audience: projectstate.AudienceMachine, Source: "over the remote MCP channel", Objective: 2},
			},
		}
	}
	if err := projectstate.ValidateModelIdentities(valid()); err != nil {
		t.Fatalf("a valid two-entry list must pass: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*projectstate.MissionStatement)
		want string
	}{
		{"no entries", func(m *projectstate.MissionStatement) { m.DeliverySurfaces = nil }, "deliverySurfaces"},
		{"a blank source", func(m *projectstate.MissionStatement) { m.DeliverySurfaces[0].Source = "  " }, "source"},
		{"an unknown objective", func(m *projectstate.MissionStatement) { m.DeliverySurfaces[0].Objective = 99 }, "objective 99"},
		{"an objective number held by two objectives", func(m *projectstate.MissionStatement) {
			m.Objectives = []projectstate.Objective{{Number: 1, Statement: "one"}, {Number: 1, Statement: "uno"}, {Number: 2, Statement: "two"}}
		}, "objective 1"},
		{"a kind listed twice", func(m *projectstate.MissionStatement) {
			m.DeliverySurfaces[1].Kind = projectstate.SurfaceWeb
			m.DeliverySurfaces[1].Audience = projectstate.AudienceHuman
		}, "listed twice"},
		{"an audience that disagrees with the kind", func(m *projectstate.MissionStatement) { m.DeliverySurfaces[1].Audience = projectstate.AudienceHuman }, "audience"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := valid()
			tc.edit(m)
			err := projectstate.ValidateModelIdentities(m)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a problem naming %q, got %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), "1 identity problem(s)") {
				t.Errorf("want exactly one problem, got %v", err)
			}
		})
	}
}
