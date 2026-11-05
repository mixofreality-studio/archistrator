package main

// run016_test.go replays RUN-016's wall. testdata/fresh/run016.project.json is its
// activity/C-message-bus state with the bus's contract taken out, and
// run016.contract.message-bus.json is that contract exactly as its detailed-design agent passed it
// to the verb (trace cc387de97719638d16e5137b273d0cb5): the shape the verb advertises, with no
// goPackage. The verb stored it without one, every generator skipped it without saying so, and
// construction started with no contract.gen.go and no fake, ten times in a row.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	run016Component = "message-bus"
	run016Activity  = "C-message-bus"
)

func run016Session(t *testing.T) *Session {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run016.project.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := run010Session(t, raw, run016Component)
	s.ActivityID = run016Activity
	return s
}

// run016Contract is the bus's contract as RUN-016's agent passed it.
func run016Contract(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "fresh", "run016.contract.message-bus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if _, ok := c["goPackage"]; ok {
		t.Fatal("the fixture must carry RUN-016's contract verbatim, with no goPackage")
	}
	return c
}

func TestRun016ContractGoPackageIsDerived(t *testing.T) {
	s := run016Session(t)
	if err := s.recordServiceContract(run016Contract(t)); err != nil {
		t.Fatalf("the verbatim contract: %v", err)
	}
	proj, _, err := s.readProject()
	if err != nil {
		t.Fatal(err)
	}
	sc := proj.ServiceContracts["messageBus"]
	// (a) the Go package is derived from the component.
	if sc.GoPackage != "internal/utility/messagebus" {
		t.Fatalf("stored goPackage = %q, want internal/utility/messagebus", sc.GoPackage)
	}
	// (b) modelgen builds it, and the construction job generates it.
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if err := modelgen.ValidateContract("messageBus", raw); err != nil {
		t.Fatalf("modelgen refuses the stored contract: %v", err)
	}
	spec, err := projectstate.TaskSpecFor(&proj, run016Activity, "construction")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(spec.Contracts, ","); got != "messageBus" {
		t.Fatalf("the construction job generates [%s], want [messageBus]", got)
	}
	// (c) the next activity's Manager takes the bus as a dep. RUN-016's main had already merged
	// these three contracts, with these packages, when kept-list-manager came up.
	for key, pkg := range map[string]string{
		"callerAccess":     "internal/resourceaccess/caller",
		"listRulingEngine": "internal/engine/listruling",
		"todoListAccess":   "internal/resourceaccess/todolist",
	} {
		proj.ServiceContracts[key] = projectstate.ServiceContract{Component: key, GoPackage: pkg}
	}
	deps, err := projectstate.ManagerDepsFromSystem(proj, "kept-list-manager")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range deps {
		names = append(names, d.Component)
	}
	if !strings.Contains(strings.Join(names, ","), "messageBus") {
		t.Fatalf("kept-list-manager's deps %v leave out messageBus", names)
	}
}

// (d) a supplied goPackage is held to one Go identifier segment under the layer's directory.
func TestRun016SuppliedGoPackageIsOneSegment(t *testing.T) {
	for _, bad := range []string{"internal/utility/message-bus", "internal/utility/a/b", "internal/utility/type", "internal/utility/", "internal/engine/messagebus"} {
		t.Run(bad, func(t *testing.T) {
			s := run016Session(t)
			c := run016Contract(t)
			c["goPackage"] = bad
			err := s.recordServiceContract(c)
			if err == nil || !strings.Contains(err.Error(), "goPackage") {
				t.Fatalf("goPackage %q must be refused naming goPackage, got: %v", bad, err)
			}
			if s.wroteState {
				t.Fatal("a refused contract must write nothing")
			}
		})
	}
	t.Run("a valid package other than the derived one is kept", func(t *testing.T) {
		s := run016Session(t)
		c := run016Contract(t)
		c["goPackage"] = "internal/utility/bus"
		if err := s.recordServiceContract(c); err != nil {
			t.Fatal(err)
		}
		proj, _, err := s.readProject()
		if err != nil {
			t.Fatal(err)
		}
		if got := proj.ServiceContracts["messageBus"].GoPackage; got != "internal/utility/bus" {
			t.Fatalf("stored goPackage = %q, want the supplied internal/utility/bus", got)
		}
	})
}

// (e) the leaf falls back to the whole joined id when stripping leaves nothing, a keyword, or a
// package another contract holds.
func TestRun016DerivedGoPackageLeaf(t *testing.T) {
	held := map[string]projectstate.ServiceContract{
		"callerAccess": {GoPackage: "internal/resourceaccess/caller"},
	}
	for _, tc := range []struct {
		id   string
		kind projectstate.ComponentKind
		want string
	}{
		{"message-bus", projectstate.CompUtility, "internal/utility/messagebus"},
		{"caller-access", projectstate.CompResourceAccess, "internal/resourceaccess/caller"},
		{"list-ruling-engine", projectstate.CompEngine, "internal/engine/listruling"},
		{"todo-list-access", projectstate.CompResourceAccess, "internal/resourceaccess/todolist"},
		{"kept-list-manager", projectstate.CompManager, "internal/manager/keptlist"},
		{"type-access", projectstate.CompResourceAccess, "internal/resourceaccess/typeaccess"},
		{"access", projectstate.CompResourceAccess, "internal/resourceaccess/access"},
		{"caller-engine", projectstate.CompEngine, "internal/engine/caller"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			p := projectstate.Project{ServiceContracts: held}
			key, _ := projectstate.ContractKeyFor(projectstate.Component{ID: tc.id, Kind: tc.kind})
			got, err := derivedGoPackage(p, projectstate.Component{ID: tc.id, Kind: tc.kind}, key)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("derived %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("a leaf another contract holds falls back to the joined id", func(t *testing.T) {
		p := projectstate.Project{ServiceContracts: held}
		p.ServiceContracts = map[string]projectstate.ServiceContract{"other": {GoPackage: "internal/engine/caller"}}
		got, err := derivedGoPackage(p, projectstate.Component{ID: "caller-engine", Kind: projectstate.CompEngine}, "callerEngine")
		if err != nil || got != "internal/engine/callerengine" {
			t.Fatalf("got %q, %v", got, err)
		}
		p.ServiceContracts = map[string]projectstate.ServiceContract{"other": {GoPackage: "internal/utility/messagebus"}}
		if got, err := derivedGoPackage(p, projectstate.Component{ID: "message-bus", Kind: projectstate.CompUtility}, "messageBus"); err == nil || !strings.Contains(err.Error(), "goPackage") {
			t.Fatalf("a Utility strips nothing, so when its joined id is held too it must be refused naming goPackage: got %q, %v", got, err)
		}
		p.ServiceContracts = map[string]projectstate.ServiceContract{"other": {GoPackage: "internal/resourceaccess/caller"}}
		got, err = derivedGoPackage(p, projectstate.Component{ID: "caller-access", Kind: projectstate.CompResourceAccess}, "callerAccess")
		if err != nil || got != "internal/resourceaccess/calleraccess" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("the component's own recorded package is no collision", func(t *testing.T) {
		got, err := derivedGoPackage(projectstate.Project{ServiceContracts: held}, projectstate.Component{ID: "caller-access", Kind: projectstate.CompResourceAccess}, "callerAccess")
		if err != nil || got != "internal/resourceaccess/caller" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
}
