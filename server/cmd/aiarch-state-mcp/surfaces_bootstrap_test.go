package main

// surfaces_bootstrap_test.go — a browser surface's web project is bootstrapped for the surface it
// serves (FPC Plan 5, Task 5.5): the generator the job runs before the agent carries the Client's
// stated kind, so an MCP Apps Client is seated as a host-bridge entry and a web Client as a page.

import (
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func TestFrontendGenerators_BootstrapCarriesTheClientsSurface(t *testing.T) {
	proj, _, err := projectstate.DecodeProjectJSON(freshState(t), "")
	if err != nil {
		t.Fatal(err)
	}
	for activity, want := range map[string]string{"U-SPA-todo-web-app": "web", "U-SPA-todo-mcp-app": "mcpApp"} {
		spec, err := deriveTaskSpec(&proj, activity, "construction")
		if err != nil {
			t.Fatalf("%s: %v", activity, err)
		}
		var argv []string
		for _, g := range spec.Generate {
			if g.Title == "webgen bootstrap" {
				argv = g.Argv
			}
		}
		if got := strings.Join(argv, " "); !strings.HasSuffix(got, "-kind "+want) {
			t.Errorf("%s: webgen bootstrap argv %q does not end in -kind %s", activity, got, want)
		}
	}
}

// A browser surface's edit scope admits every file its bootstrap seats: the entry file the surface
// kind gets (<id>.html for a page, <id>.mcpapp.html for an MCP Apps shell) and the shared MCP Apps
// build seat every web project carries.
func TestFrontendScope_AdmitsTheBootstrapSeats(t *testing.T) {
	proj, _, err := projectstate.DecodeProjectJSON(freshState(t), "")
	if err != nil {
		t.Fatal(err)
	}
	for activity, entry := range map[string]string{
		"U-SPA-todo-web-app": "webApp/todo-web-app.html",
		"U-SPA-todo-mcp-app": "webApp/todo-mcp-app.mcpapp.html",
	} {
		spec, err := deriveTaskSpec(&proj, activity, "construction")
		if err != nil {
			t.Fatalf("%s: %v", activity, err)
		}
		for _, want := range []string{entry, "webApp/scripts/build-mcpapps.mjs"} {
			if !slices.ContainsFunc(spec.EditScope.Extra, func(g string) bool { return projectstate.EditScopeGlobMatch(g, want) }) {
				t.Errorf("%s: edit scope %v does not admit %s", activity, spec.EditScope.Extra, want)
			}
		}
	}
}
