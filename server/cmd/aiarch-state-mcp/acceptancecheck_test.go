package main

// acceptancecheck_test.go runs the M-ACCEPT milestone's acceptance gate for real (FPC plan 4, Task
// 4.5): the whole fixture built the way construction leaves it (every component, both machine
// clients, the browser surface with the RUN-021 harvest's hand-written SPA), the acceptance scenarios
// of the fixture's acceptance plan generated from the recorded plan and run by the venue's job over
// the launched app. The red proof puts one defect in the SPA, in memory, and shows the gate holding
// and naming where the failure routes.

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	// redVariant is the acceptance variant whose probe asserts the member's browser shows the todo the
	// agent marked completed as checked.
	redVariant = "work-the-list-P1@todoApi"
	// spaItem is the harvested SPA's todo item, the file the red proof mutates, and the rendering of
	// a todo's state it breaks.
	spaItem    = "webApp/src/todo-web-app/components/TodoItem.tsx"
	spaChecked = "checked={todo.completed}"
	// uitestsPackage seats the Playwright project the generated specs run in (the app's own uitests
	// directory; the scaffold carries no Playwright).
	uitestsPackage = `{"name":"conformance-uitests","private":true,"version":"0.0.0","type":"module","devDependencies":{"@playwright/test":"1.60.0"}}` + "\n"
)

// seatHarvestedSPA writes the fixture's hand-written SPA (every webApp file of its implementation
// tree) into the repo, each through edit.
func seatHarvestedSPA(t *testing.T, repo string, edit func(path, body string) string) {
	t.Helper()
	impl, err := conformance.FixtureImpl("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	written := 0
	err = fs.WalkDir(impl, "webApp", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := fs.ReadFile(impl, path)
		if err != nil {
			return err
		}
		writeRepoFile(t, repo, path, []byte(edit(path, string(body))))
		written++
		return nil
	})
	if err != nil || written == 0 {
		t.Fatalf("the fixture's SPA: %d file(s), err %v", written, err)
	}
}

// acceptanceRepo is the fixture built whole and committed, the way the constructions before M-ACCEPT
// leave it: the server-side components and the composition, both machine clients, the browser surface
// with the harvested SPA (edited by edit), every generated test, and Playwright seated for the
// acceptance specs.
func acceptanceRepo(t *testing.T, edit func(path, body string) string) string {
	t.Helper()
	for _, tool := range []string{"npm", "npx"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required: the acceptance specs cannot run without it", tool)
		}
	}
	repo := integratedRepo(t)
	commit := func(msg string) {
		inRepo(t, repo, "git", "add", "-A")
		inRepo(t, repo, "git", "commit", "-q", "--allow-empty", "-m", msg)
	}
	for _, c := range freshMachineClients {
		runGenerators(t, repo, activitySpec(t, repo, c.activity, "integration"))
		commit("integrate " + c.activity)
	}

	surface := activitySpec(t, repo, freshSurfaceActivity, "construction")
	runGenerators(t, repo, surface)
	seatHarvestedSPA(t, repo, edit)
	s := &Session{Mode: jobModeConstruct, StateRoot: repo, ActivityID: freshSurfaceActivity, TaskID: "construction", ComponentID: "todo-web-app", git: runGit}
	res, err := s.runChecks(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Results {
		t.Fatalf("the surface's %s failed:\n%s", f.ID, f.Tail)
	}
	commit("build the surface")

	// Generated tests last: the acceptance specs locate through the surface's elements and the Go
	// ones compile against the composition.
	inRepo(t, repo, "make", "gen-tests")
	commit("generate the tests")

	ui := filepath.Join(repo, "uitests")
	if err := os.WriteFile(filepath.Join(ui, "package.json"), []byte(uitestsPackage), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{{"npm", "install", "--no-audit", "--no-fund"}, {"npx", "playwright", "install", "chromium"}} {
		if out, err := runIn(ui, argv...); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(argv, " "), err, out)
		}
	}
	commit("seat the acceptance project")
	return repo
}

// An acceptance run is red when a write one client made is not shown on another, and the gate says
// where it routes. The fixture is built whole with the harvested SPA changed so that a todo is never
// rendered as checked (in memory: the committed SPA is untouched). The M-ACCEPT gate (run-checks
// --gate over the milestone's spec, as the venue's job runs it) runs every acceptance scenario of the
// plan; the variant whose probe asserts the member's browser shows the todo the agent marked completed
// as checked fails, and the route the hold comment carries names todo-web-app on the variant's path
// and the REST client's activity to re-open. The job's own recording of the CheckRun (no agent) is
// TestPump_JobTaskRunsWithoutAgent's: its checkout is a fresh clone with no installed browser
// project, which a venue provisions and this test does not.
func TestAcceptanceJob_RedRunHoldsAndNamesTheRoute(t *testing.T) {
	repo := acceptanceRepo(t, func(path, body string) string {
		if path != spaItem {
			return body
		}
		if !strings.Contains(body, spaChecked) {
			t.Fatalf("%s does not contain %q: the harvested SPA changed; update the red proof", spaItem, spaChecked)
		}
		return strings.Replace(body, spaChecked, "checked={false}", 1)
	})
	statuses, logs := gate(t, repo, activitySpec(t, repo, mAcceptActivityID, "integration"))

	failed := map[string]bool{}
	passed, skipped := 0, 0
	for id, status := range statuses {
		variant, ok := strings.CutPrefix(id, "scenario:acceptance:")
		if !ok {
			if status != checkPass {
				t.Errorf("%s: %s\n%s", id, status, logs[id])
			}
			continue
		}
		switch status {
		case checkPass:
			passed++
		case "skip":
			skipped++
		default:
			failed[variant] = true
		}
	}
	if !failed[redVariant] {
		t.Errorf("the gate does not name %s as failed: failed = %v (%d passed, %d skipped)\n%s", redVariant, failed, passed, skipped, logs["scenario:acceptance:"+redVariant])
	}
	// The defect is the SPA's one rendering of a completed todo: exactly the variants that assert a
	// completed todo on the member's browser fail, and the rest hold.
	if passed == 0 {
		t.Errorf("no acceptance variant passed (failed %v): the defect is one rendering, so the others must hold", failed)
	}

	raw, err := os.ReadFile(filepath.Join(repo, ".aiarch/state/project.json"))
	if err != nil {
		t.Fatal(err)
	}
	proj, _, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	route, err := projectstate.AcceptanceRoute(&proj, redVariant)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(route.Components, "todo-web-app") {
		t.Errorf("the route's components %v do not name todo-web-app, where the failure is", route.Components)
	}
	if route.ClientActivity != "C-todo-api" {
		t.Errorf("the route re-opens %q, want C-todo-api (the client the agent's stimulus enters through)", route.ClientActivity)
	}
	if line := projectstate.AcceptanceRouteLine(route); !strings.Contains(line, redVariant) || !strings.Contains(line, "C-todo-api") {
		t.Errorf("the hold comment's line does not carry the variant and its route: %q", line)
	}
}
