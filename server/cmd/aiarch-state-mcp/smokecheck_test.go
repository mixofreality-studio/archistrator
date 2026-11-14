package main

// smokecheck_test.go holds the boot smoke every integration carries (FPC plan 3, Task 3.2): the
// task spec names it for each type of integrating activity, and the check runner boots the
// composed app through its run contract and refuses to pass a launch that left anything behind.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/devstack"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// smokeActivityID is a deployment activity the test adds to the fixture's activity list: the
// fixture's four built components cover the other integrating types.
const smokeDeploymentActivity = "R-rollout"

// fixtureWithDeployment is the conformance fixture with one deployment activity (non-coding,
// junior-developer) appended to its committed activity list.
func fixtureWithDeployment(t *testing.T) []byte {
	t.Helper()
	var top map[string]any
	if err := json.Unmarshal(freshState(t), &top); err != nil {
		t.Fatal(err)
	}
	for _, slot := range top["slots"].(map[string]any) {
		model, _ := slot.(map[string]any)["model"].(map[string]any)
		acts, ok := model["activities"].([]any)
		if !ok {
			continue
		}
		clone := map[string]any{}
		for k, v := range acts[0].(map[string]any) {
			clone[k] = v
		}
		clone["name"] = smokeDeploymentActivity
		clone["workerClass"] = "junior-developer"
		clone["coding"] = false
		model["activities"] = append(acts, clone)
	}
	out, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Every integrating activity of the composed app boots it: the Manager and the machine clients
// compose the server, the browser surface's integration serves its page from it, and the
// deployment's rolls it out. The smoke follows every command check and precedes every scenario.
func TestIntegrationChecks_IncludeSmokeForEveryType(t *testing.T) {
	// Generation 5 added the smoke check to what a spec resolves to; a later generation (6: the job
	// task kind) keeps it.
	if projectstate.TaskSpecSchemaVersion < 5 {
		t.Errorf("TaskSpecSchemaVersion = %d, want at least 5: the smoke check changes what a spec resolves to", projectstate.TaskSpecSchemaVersion)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(fixtureWithDeployment(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, activity := range []string{freshManagerActivity, freshSurfaceActivity, "C-todo-mcp-server", freshMachineActivity, smokeDeploymentActivity} {
		t.Run(activity, func(t *testing.T) {
			spec, err := projectstate.TaskSpecFor(&proj, activity, "integration")
			if err != nil {
				t.Fatal(err)
			}
			requireSmokeBetweenCommandsAndScenarios(t, spec)
		})
	}
	// Construction boots nothing: only an integration is the unit of integration.
	spec, err := projectstate.TaskSpecFor(&proj, freshManagerActivity, "construction")
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(spec.Checks, func(c projectstate.SpecCheck) bool { return c.Kind == projectstate.CheckKindSmoke }) {
		t.Errorf("a construction task carries a smoke: %v", specCheckIDs(spec))
	}
}

// requireSmokeBetweenCommandsAndScenarios holds one integration spec to its smoke: present, the
// kind and cap the runner expects, after every command check and before every scenario, with the
// two builds it needs green first still there.
func requireSmokeBetweenCommandsAndScenarios(t *testing.T, spec projectstate.TaskSpec) {
	t.Helper()
	smoke := slices.IndexFunc(spec.Checks, func(c projectstate.SpecCheck) bool { return c.ID == "smoke:composition" })
	if smoke < 0 {
		t.Fatalf("%s's integration has no smoke:composition: %v", spec.Activity, specCheckIDs(spec))
	}
	if got := spec.Checks[smoke]; got.Kind != projectstate.CheckKindSmoke || got.TimeoutSec != 900 {
		t.Errorf("smoke check = %+v, want kind %q and a 900s cap", got, projectstate.CheckKindSmoke)
	}
	for i, c := range spec.Checks {
		if c.Kind == projectstate.CheckKindCommand && i > smoke {
			t.Errorf("command check %s runs after the smoke", c.ID)
		}
		if c.Kind == projectstate.CheckKindScenario && i < smoke {
			t.Errorf("scenario check %s runs before the smoke", c.ID)
		}
	}
	for _, build := range []string{"command:build-server", "command:build-devstack"} {
		if !slices.ContainsFunc(spec.Checks, func(c projectstate.SpecCheck) bool { return c.ID == build }) {
			t.Errorf("%s's integration lost %s, which the smoke needs green first: %v", spec.Activity, build, specCheckIDs(spec))
		}
	}
}

// withSmoke adds the full spec's smoke checks to a spec narrowed to its command checks: the
// composed app's integration gate is the commands and the boot.
func withSmoke(narrowed, full projectstate.TaskSpec) projectstate.TaskSpec {
	for _, c := range full.Checks {
		if c.Kind == projectstate.CheckKindSmoke {
			narrowed.Checks = append(narrowed.Checks, c)
		}
	}
	return narrowed
}

func specCheckIDs(s projectstate.TaskSpec) []string {
	ids := make([]string, len(s.Checks))
	for i, c := range s.Checks {
		ids[i] = c.ID
	}
	return ids
}

type noArgs struct{}
type noOut struct{}

// fakeBooted serves what the fixture's composed app serves: /readyz, an index page and every
// tool the model generates at /mcp.
func fakeBooted(t *testing.T, tools []string) *devstack.App {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "0"}, nil)
	for _, name := range tools {
		mcp.AddTool(srv, &mcp.Tool{Name: name, Description: name}, func(context.Context, *mcp.CallToolRequest, noArgs) (*mcp.CallToolResult, noOut, error) {
			return &mcp.CallToolResult{}, noOut{}, nil
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "<html></html>")
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &devstack.App{BaseURL: ts.URL, MCPURL: ts.URL + "/mcp", Log: func() string { return "launcher output" }}
}

// smokeRunner is a runner over a repo holding the fixture, whose launcher is a fake serving a
// complete composed app and whose docker inventory grows by leaked after Stop.
func smokeRunner(t *testing.T, leaked ...string) (checkRunner, *[]string) {
	t.Helper()
	root := t.TempDir()
	writeRepoFile(t, root, ".aiarch/state/project.json", freshState(t))
	m, err := projectmodel.Load(freshState(t))
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	stopped := false
	r := checkRunner{root: root, outDir: filepath.Join(root, "check-results"), now: time.Now,
		launch: func(_ context.Context, dir string, _ []string, _ io.Writer) (*devstack.App, error) {
			events = append(events, "launch "+dir)
			app := fakeBooted(t, devstack.ToolNames(m, nil))
			app.Stop = func() error { stopped = true; events = append(events, "stop"); return nil }
			return app, nil
		},
		inventory: func(context.Context) ([]string, error) {
			if stopped {
				return append([]string{"container:pre-existing"}, leaked...), nil
			}
			return []string{"container:pre-existing"}, nil
		},
	}
	return r, &events
}

func smokeSpecCheck() projectstate.SpecCheck {
	return projectstate.SpecCheck{ID: "smoke:composition", Kind: projectstate.CheckKindSmoke, TimeoutSec: 60}
}

func TestRunSmokePassesAfterALeakFreeBoot(t *testing.T) {
	r, events := smokeRunner(t)
	res := r.Run(context.Background(), []projectstate.SpecCheck{smokeSpecCheck()})
	if res[0].Status != checkPass {
		b, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "smoke_composition.log"))
		t.Fatalf("smoke = %s:\n%s", res[0].Status, b)
	}
	if res[0].Kind != projectstate.CheckKindSmoke {
		t.Errorf("result kind = %q", res[0].Kind)
	}
	if !slices.Contains(*events, "stop") {
		t.Errorf("the booted app was not stopped: %v", *events)
	}
}

// A launch whose teardown leaves a container behind is not a pass: teardown is part of the
// contract (a scenario run may not leave a process, container or volume).
func TestRunSmokeFailsOnLeftovers(t *testing.T) {
	r, _ := smokeRunner(t, "container:x")
	res := r.Run(context.Background(), []projectstate.SpecCheck{smokeSpecCheck()})
	if res[0].Status != checkFail {
		t.Fatalf("smoke over a leaking launch = %s, want fail", res[0].Status)
	}
	b, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "smoke_composition.log"))
	if !strings.Contains(string(b), "container:x") {
		t.Errorf("the log does not name the leftover:\n%s", b)
	}
}

func TestRunSmokeFailsWhenTheToolTableIsShort(t *testing.T) {
	r, _ := smokeRunner(t)
	r.launch = func(context.Context, string, []string, io.Writer) (*devstack.App, error) {
		app := fakeBooted(t, []string{"listAddTodo"})
		app.Stop = func() error { return nil }
		return app, nil
	}
	res := r.Run(context.Background(), []projectstate.SpecCheck{smokeSpecCheck()})
	b, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "smoke_composition.log"))
	if res[0].Status != checkFail || !strings.Contains(string(b), "listOpenList") {
		t.Fatalf("smoke over a short tool table = %s:\n%s", res[0].Status, b)
	}
}

// The smoke over the real thing: the conformance fixture, with its component bodies, scaffolded
// and generated by the platform tree this module is built against, is booted through its own run
// contract by the real runner (devstack.Launch and the docker inventory), answers every probe,
// and leaves nothing behind.
func TestRunSmokeBootsTheComposedFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a composed app: needs docker, the temporal CLI and npm")
	}
	for _, tool := range []string{"docker", "temporal", "npm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required: the boot smoke cannot be proven without it", tool)
		}
	}
	impl, err := conformance.FixtureImpl("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	if _, err := conformance.RunChain(ctx, "todomvc", dir, conformance.WithImpl(impl)); err != nil {
		t.Fatalf("RunChain: %v", err)
	}
	r := checkRunner{root: dir, outDir: filepath.Join(dir, "check-results"), now: time.Now}
	res := r.Run(ctx, []projectstate.SpecCheck{{ID: "smoke:composition", Kind: projectstate.CheckKindSmoke, TimeoutSec: 900}})
	log, _ := os.ReadFile(filepath.Join(r.outDir, "checks", "smoke_composition.log"))
	if res[0].Status != checkPass {
		t.Fatalf("smoke over the composed fixture = %s:\n%s", res[0].Status, log)
	}
}
