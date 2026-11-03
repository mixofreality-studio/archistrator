package main

// run010_test.go replays RUN-010's real fresh project: testdata/fresh/run010.project.json is its
// main after C-list-access merged, with the two sibling contracts its other branches recorded
// (identityAccess, messageBus), exactly as its agents wrote them. Its listAccess carries the layer
// "resourceaccess": every gate of the task that recorded it matched layers ignoring case, and
// projectmodel.Load, the first thing every generator does, refused it, so C-list-manager's
// construction could generate nothing and no agent of it could re-record another component's
// contract.

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// archGateRun is the seated gate's -run pattern, as the arch check runs it (projectstate.archCheck).
const archGateRun = "^(TestFileLayout|TestGeneratedOnlyPublic|TestMethodLayering|TestMethod|TestProjectModel)$"

// run010Components is each RUN-010 contract by the component that recorded it.
var run010Components = map[string]string{"listAccess": "list-access", "identityAccess": "identity-access", "messageBus": "message-bus"}

func run010Raw(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run010.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// run010Contracts is the fixture's contracts as RUN-010's agents passed them to the verb.
func run010Contracts(t *testing.T) map[string]map[string]any {
	t.Helper()
	var doc struct {
		ServiceContracts map[string]map[string]any `json:"serviceContracts"`
	}
	if err := json.Unmarshal(run010Raw(t), &doc); err != nil {
		t.Fatal(err)
	}
	for key := range run010Components {
		if _, ok := doc.ServiceContracts[key]; !ok {
			t.Fatalf("the fixture lost RUN-010's %s", key)
		}
	}
	return doc.ServiceContracts
}

// run010Session is a construct session over the fixture, ambient on component.
func run010Session(t *testing.T, raw []byte, component string) *Session {
	t.Helper()
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(raw, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := seedProject(t, proj, jobModeConstruct, 0)
	s.ComponentID = component
	return s
}

// run010Recorded is a construct session over the fixture with each of its contracts recorded
// again through the verb, as its agents passed them.
func run010Recorded(t *testing.T) *Session {
	t.Helper()
	contracts := run010Contracts(t)
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(run010Raw(t), anyProject)
	if err != nil {
		t.Fatal(err)
	}
	delete(proj.ServiceContracts, "listAccess")
	s, _ := seedProject(t, proj, jobModeConstruct, 0)
	for _, key := range []string{"listAccess", "identityAccess", "messageBus"} {
		s.ComponentID = run010Components[key]
		if err := s.recordServiceContract(contracts[key]); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	return s
}

// RUN-010's state as it stood is one no generator can load; recorded through the verb, each of its
// contracts takes the layer its component's kind names, spelled as projectmodel spells it, and the
// whole state loads.
func TestRun010ContractsLoadInEveryGenerator(t *testing.T) {
	raw := run010Raw(t)
	if _, err := projectmodel.Load(raw); err == nil || !strings.Contains(err.Error(), `unknown layer "resourceaccess"`) {
		t.Fatalf("the fixture must keep the layer RUN-010's agent recorded: %v", err)
	}
	if got := run010Contracts(t)["listAccess"]["layer"]; got != "resourceaccess" {
		t.Fatalf("the fixture's listAccess layer is %v", got)
	}
	s := run010Recorded(t)
	want := map[string]string{"listAccess": "ResourceAccess", "identityAccess": "ResourceAccess", "messageBus": "Utility"}
	stored, err := os.ReadFile(filepath.Join(s.StateRoot, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	model, err := projectmodel.Load(stored)
	if err != nil {
		t.Fatalf("what the verb records, every generator loads: %v", err)
	}
	for key, layer := range want {
		if got := model.Contracts[key].Layer; got != layer {
			t.Errorf("%s's layer is %q, want %q", key, got, layer)
		}
	}
}

// The layer is the design's: a contract that names another layer than its component's kind is
// refused, and leaving it out records the right one.
func TestRecordServiceContractTakesTheLayerFromTheSystem(t *testing.T) {
	s := run010Recorded(t)
	s.ComponentID = "message-bus"
	bus := run010Contracts(t)["messageBus"]
	for _, wrong := range []map[string]any{{"layer": "ResourceAccess"}, {"interface": map[string]any{"layer": "engine"}}} {
		c := mergedContract(t, bus, wrong)
		err := s.recordServiceContract(c)
		if err == nil || !strings.Contains(err.Error(), `layer is "Utility"`) {
			t.Errorf("%v must be refused, naming the design's layer: %v", wrong, err)
		}
	}
	delete(bus, "layer")
	if err := s.recordServiceContract(bus); err != nil {
		t.Fatal(err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	if got := proj.ServiceContracts["messageBus"]; got.Layer != "Utility" || got.Interface.Layer != "utility" {
		t.Errorf("layers %q / %q, want the System's", got.Layer, got.Interface.Layer)
	}
}

// Whatever else made the state unloadable, a record that would leave it so is refused, naming the
// reason, with nothing written.
func TestConstructionRecordRefusesAStateNoGeneratorLoads(t *testing.T) {
	s := run010Session(t, run010Raw(t), "message-bus")
	err := s.recordServiceContract(run010Contracts(t)["messageBus"])
	if err == nil || !strings.Contains(err.Error(), "no generator can load") || !strings.Contains(err.Error(), "listAccess") {
		t.Fatalf("RUN-010's unloadable listAccess must stop the record: %v", err)
	}
	if s.wroteState {
		t.Fatal("a refused record writes nothing")
	}
}

// The derived machine clients over RUN-010's Manager, recorded through the verb with operations its
// views' labels name, leave TP-LABEL-OP with nothing to say: the labels resolve through the Manager
// the clients front onto their derived operations.
func TestRun010ManagerRecordKeepsTheLabelsResolved(t *testing.T) {
	s := run010Recorded(t)
	s.ComponentID = "list-manager"
	if err := s.recordServiceContract(run010ManagerContract(t)); err != nil {
		t.Fatalf("the Manager's contract: %v", err)
	}
	stored, err := os.ReadFile(filepath.Join(s.StateRoot, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	after, _, err := projectstate.DecodeProjectJSON(stored, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []string{"listApi", "listMcpServer"} {
		ops := after.ServiceContracts[client].Interface.Operations
		if len(ops) != len(run010ManagerOps) || !strings.HasPrefix(ops[0].Name, "List") {
			t.Errorf("%s's derived operations: %+v", client, ops)
		}
	}
	findings, err := methodcheck.ValidateProjectJSON(stored)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if f.RuleID == "TP-LABEL-OP" {
			t.Errorf("%s: %s", f.RuleID, f.Message)
		}
	}
}

// RUN-010's Manager, with the operations the actor calls into its clients name.
var run010ManagerOps = []string{"AddTodo", "RewordTodo", "DiscardTodo", "SetTodoDone", "ToggleAll", "ClearCompleted", "ReviewList"}

func run010ManagerContract(t *testing.T) map[string]any {
	t.Helper()
	ops := make([]any, 0, len(run010ManagerOps))
	for _, op := range run010ManagerOps {
		ops = append(ops, map[string]any{"name": op, "params": []any{map[string]any{"name": "proof", "schema": map[string]any{"$ref": "#/$defs/Proof"}}}, "result": map[string]any{"$ref": "#/$defs/Proof"}, "error": true})
	}
	return map[string]any{
		"component": "listManager", "goPackage": "internal/manager/list", "title": "list manager",
		"deps":      []any{map[string]any{"name": "identity", "component": "identityAccess"}, map[string]any{"name": "lists", "component": "listAccess"}, map[string]any{"name": "bus", "component": "messageBus"}},
		"$defs":     map[string]any{"Proof": map[string]any{"type": "string"}},
		"interface": map[string]any{"name": "ListManager", "operations": ops},
	}
}

func mergedContract(t *testing.T, base, extra map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for k, v := range extra {
		if inner, ok := v.(map[string]any); ok {
			if into, ok := out[k].(map[string]any); ok {
				maps.Copy(into, inner)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// The seated gate loads the state as every generator does, so RUN-010's state is red in the task
// that recorded it, where its agent could still fix it, and green once recorded through the verb.
func TestRun010SeatedGateLoadsTheState(t *testing.T) {
	repo := bornRepo(t, run010Raw(t))
	cmd := exec.Command("go", "test", "-count=1", "-v", "-run", archGateRun, "./internal/")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "--- FAIL: TestProjectModel") || !strings.Contains(string(out), `unknown layer "resourceaccess"`) {
		t.Fatalf("the seated gate must refuse RUN-010's state: %v\n%s", err, out)
	}
	stored, err := os.ReadFile(filepath.Join(run010Recorded(t).StateRoot, statePathPrefix, projectFile))
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, ".aiarch/state/project.json", stored)
	out = []byte(inRepo(t, repo, "go", "test", "-count=1", "-v", "-run", "^TestProjectModel$", "./internal/"))
	if !strings.Contains(string(out), "--- PASS: TestProjectModel") {
		t.Fatalf("recorded through the verb, the state loads:\n%s", out)
	}
}
