package main

// facetcontract_test.go pins recordServiceContract over a component that owns several contract
// facets (projectStateAccess files projectStateAccess, activityExecutionAccess, designSessionAccess
// and gitActivityStatusAccess), and over a committed contract whose pre-existing findings an
// amendment does not introduce (BLK-ISS0282-FACET-CONTRACT-UNRECORDABLE). The pieces are driven
// directly: the verb's whole-state Method gate is held by its own tests, and the fixtures here
// predate rules it now enforces.

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// facetProject is the RUN-011 project with a second facet "todoListViewAccess" committed beside the
// todo list access's own contract, owned by owner.
func facetProject(t *testing.T, owner string) (projectstate.Project, projectstate.Component) {
	t.Helper()
	proj := run011Project(t)
	facet := proj.ServiceContracts["todoListAccess"]
	facet.Interface.Name = "TodoListViewAccess"
	facet.Component = owner
	proj.ServiceContracts["todoListViewAccess"] = facet
	for _, c := range proj.SystemDesign.Model.(*projectstate.System).Components {
		if c.ID == "todo-list-access" {
			return proj, c
		}
	}
	t.Fatal("fixture lost todo-list-access")
	return proj, projectstate.Component{}
}

// A contract whose interface names a committed facet of the ambient component is filed under that
// facet's key; the primary interface and an unnamed one keep the component's own key.
func TestFacetKey_AmendsAFacetUnderItsOwnKey(t *testing.T) {
	for _, owner := range []string{"todoListAccess", "todo-list-access"} {
		proj, comp := facetProject(t, owner)
		for _, tc := range []struct{ iface, want string }{
			{"TodoListViewAccess", "todoListViewAccess"},
			{"TodoListAccess", "todoListAccess"},
			{"", "todoListAccess"},
			{"BrandNewFacetAccess", "todoListAccess"},
		} {
			sc := projectstate.ServiceContract{}
			sc.Interface.Name = tc.iface
			got, err := facetKey(proj, comp, "todoListAccess", sc)
			if err != nil || got != tc.want {
				t.Errorf("owner %q interface %q: key = %q, %v; want %q", owner, tc.iface, got, err, tc.want)
			}
		}
	}
}

// A key another component owns is refused, never filed under the ambient key.
func TestFacetKey_RefusesAKeyAnotherComponentOwns(t *testing.T) {
	proj, comp := facetProject(t, "someOtherComponent")
	sc := projectstate.ServiceContract{}
	sc.Interface.Name = "TodoListViewAccess"
	_, err := facetKey(proj, comp, "todoListAccess", sc)
	if err == nil || !strings.Contains(err.Error(), "todoListViewAccess") || !strings.Contains(err.Error(), "someOtherComponent") {
		t.Fatalf("a facet owned by another component must be refused, naming key and owner: %v", err)
	}
}

// An amendment that omits goPackage keeps the committed facet's package, which its sibling facets
// share (the derivation alone would call it held).
func TestGoPackageFromSystem_AmendmentKeepsTheCommittedPackage(t *testing.T) {
	proj, comp := facetProject(t, "todoListAccess")
	want := proj.ServiceContracts["todoListViewAccess"].GoPackage
	sc := projectstate.ServiceContract{Layer: "ResourceAccess"}
	if err := goPackageFromSystem(proj, comp, "todoListViewAccess", &sc); err != nil || sc.GoPackage != want {
		t.Fatalf("goPackage = %q, %v; want the committed %q", sc.GoPackage, err, want)
	}
}

// Re-recording a ResourceAccess committed with a goPackage and no infra is not refused for the
// infra the amendment did not drop; a first record of an infra-less one still is.
func TestCanonicalizeAmendment_AllowsPreExistingInfraLessResourceAccess(t *testing.T) {
	proj := run011Project(t)
	committed := proj.ServiceContracts["todoListAccess"]
	committed.Infra = nil
	amend := committed
	if err := canonicalizeAmendment("todoListAccess", &amend, committed, true); err != nil {
		t.Fatalf("re-recording a committed infra-less contract: %v", err)
	}
	if len(amend.Infra) != 0 {
		t.Fatalf("the validation placeholder leaked into the recorded contract: %v", amend.Infra)
	}
	first := committed
	if err := canonicalizeAmendment("todoListAccess", &first, projectstate.ServiceContract{}, false); err == nil || !strings.Contains(err.Error(), "infra") {
		t.Fatalf("a first infra-less ResourceAccess must still be refused: %v", err)
	}
	withInfra := proj.ServiceContracts["todoListAccess"]
	if err := canonicalizeAmendment("todoListAccess", &withInfra, committed, true); err != nil {
		t.Fatalf("an amendment that supplies infra: %v", err)
	}
}

// A Manager's ambient edge to an external Utility nobody builds (no contract, buildStatus external)
// is no constructor dep and no reason to refuse the record.
func TestManagerDepsFromSystem_SkipsAnExternalUtilityWithNoContract(t *testing.T) {
	proj := run011Project(t)
	ext := "external"
	sys := proj.SystemDesign.Model.(*projectstate.System)
	for i := range sys.Components {
		if sys.Components[i].ID == "message-bus" {
			sys.Components[i].BuildStatus = &ext
		}
	}
	delete(proj.ServiceContracts, "messageBus")
	deps, err := projectstate.ManagerDepsFromSystem(proj, run011Manager)
	if err != nil {
		t.Fatalf("an external Utility with no contract must not block the Manager: %v", err)
	}
	for _, d := range deps {
		if d.Component == "messageBus" {
			t.Fatalf("an external Utility is no constructor dep: %+v", d)
		}
	}
}

// A committed contract that carries a finding ValidateContract now raises (a reserved param name)
// can be re-recorded; an amendment that introduces a different finding, and a first record, cannot.
func TestCanonicalizeAmendment_AllowsAnyFindingTheCommittedContractCarries(t *testing.T) {
	proj := run011Project(t)
	committed := proj.ServiceContracts["todoListAccess"]
	committed.Interface.Operations = append([]projectstate.ContractOperation(nil), committed.Interface.Operations...)
	committed.Interface.Operations = append(committed.Interface.Operations, projectstate.ContractOperation{
		Name:   "Decide",
		Params: []projectstate.ContractParam{{Name: "decision", Schema: []byte(`{"type":"string"}`)}},
		Error:  true,
	})

	same := committed
	if err := canonicalizeAmendment("todoListAccess", &same, committed, true); err != nil {
		t.Fatalf("re-recording a committed contract's own finding: %v", err)
	}
	first := committed
	if err := canonicalizeAmendment("todoListAccess", &first, projectstate.ServiceContract{}, false); err == nil {
		t.Fatal("a first record carrying the finding must be refused")
	}
	other := committed
	other.Interface.Operations = append(append([]projectstate.ContractOperation(nil), committed.Interface.Operations...), projectstate.ContractOperation{
		Name:   "Other",
		Params: []projectstate.ContractParam{{Name: "ctx", Schema: []byte(`{"type":"string"}`)}},
		Error:  true,
	})
	if err := canonicalizeAmendment("todoListAccess", &other, committed, true); err == nil {
		t.Fatal("an amendment introducing a different finding must be refused")
	}
}

// An amended facet keeps its committed owner; a supplied owner naming another component is refused.
func TestKeepCommittedOwner(t *testing.T) {
	committed := projectstate.ServiceContract{Component: "projectStateAccess"}
	sc := projectstate.ServiceContract{}
	if err := keepCommittedOwner("activityExecutionAccess", committed, true, &sc); err != nil || sc.Component != "projectStateAccess" {
		t.Fatalf("owner = %q, %v; want the committed one", sc.Component, err)
	}
	sc = projectstate.ServiceContract{Component: "projectStateAccess"}
	if err := keepCommittedOwner("activityExecutionAccess", committed, true, &sc); err != nil {
		t.Fatalf("the committed owner supplied verbatim: %v", err)
	}
	sc = projectstate.ServiceContract{Component: "other"}
	if err := keepCommittedOwner("activityExecutionAccess", committed, true, &sc); err == nil || !strings.Contains(err.Error(), "projectStateAccess") {
		t.Fatalf("another owner must be refused: %v", err)
	}
	sc = projectstate.ServiceContract{}
	if err := keepCommittedOwner("x", projectstate.ServiceContract{}, false, &sc); err != nil || sc.Component != "" {
		t.Fatalf("a first record gets no owner invented: %q %v", sc.Component, err)
	}
}
