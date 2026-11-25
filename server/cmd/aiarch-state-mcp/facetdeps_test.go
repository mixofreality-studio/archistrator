package main

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// An amendment of a committed Manager contract keeps the committed deps: their names and order,
// the facets of a drawn component it takes, and its plain deps. A committed dep whose edge the
// System no longer draws is dropped; a drawn edge the committed deps do not cover is appended.
func TestDepsFromSystem_AmendmentKeepsTheCommittedDeps(t *testing.T) {
	proj := run011Project(t)
	var mgr projectstate.Component
	for _, c := range proj.SystemDesign.Model.(*projectstate.System).Components {
		if c.ID == run011Manager {
			mgr = c
		}
	}
	derived, err := projectstate.ManagerDepsFromSystem(proj, run011Manager)
	if err != nil || len(derived) < 3 {
		t.Fatalf("fixture needs two drawn deps: %v %v", derived, err)
	}
	first := derived[1]
	// A facet of the first drawn component, a custom name for it, a plain dep, and a dep on an
	// edge the System does not draw.
	facet := proj.ServiceContracts[first.Component]
	facet.Component = first.Component
	proj.ServiceContracts["firstFacetAccess"] = facet
	committed := projectstate.ServiceContract{Deps: []projectstate.ContractDep{
		derived[0],
		{Name: "custom", Component: first.Component},
		{Name: "facet", Component: "firstFacetAccess"},
		{Name: "timeout", GoType: "time.Duration", GoImport: "time"},
		{Name: "gone", Component: "notDrawnAnymore"},
	}}
	var sc projectstate.ServiceContract
	if err := depsFromSystem(proj, mgr, committed, &sc); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range sc.Deps {
		got = append(got, d.Name)
	}
	want := []string{derived[0].Name, "custom", "facet", "timeout"}
	for _, d := range derived[2:] {
		want = append(want, d.Name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("deps = %v, want %v", got, want)
	}
	// With nothing committed the deps are derived exactly as before.
	var fresh projectstate.ServiceContract
	if err := depsFromSystem(proj, mgr, projectstate.ServiceContract{}, &fresh); err != nil || len(fresh.Deps) != len(derived) {
		t.Fatalf("first record: %v %v", fresh.Deps, err)
	}
	// A component dep the System does not draw and the committed contract does not carry is refused.
	bad := projectstate.ServiceContract{Deps: []projectstate.ContractDep{{Name: "x", Component: "nowhere"}}}
	if err := depsFromSystem(proj, mgr, committed, &bad); err == nil || !strings.Contains(err.Error(), "not one the System draws") {
		t.Fatalf("undrawn dep must be refused: %v", err)
	}
}
