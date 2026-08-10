package estimation

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Löwy Fig 11-4 → Fig 11-5: Client A depends on Manager A AND Security; Manager A also
// depends on Security. The Client→Security edge is INHERITED through Manager A and must
// be eliminated. This is the canonical worked example in ch. 11 §1.2.
func TestTransitiveReductionEliminatesInheritedDependencies(t *testing.T) {
	in := map[string][]string{
		"client-a":  {"manager-a", "security"},
		"manager-a": {"security"},
	}
	got := transitiveReduction(in)
	want := map[string][]string{
		"client-a":  {"manager-a"},
		"manager-a": {"security"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("transitiveReduction = %v, want %v", got, want)
	}
}

// A longer inherited chain: A→B→C→D plus the direct A→D and A→C shortcuts. Only the
// immediate edge survives at each hop.
func TestTransitiveReductionEliminatesMultiHopInheritance(t *testing.T) {
	in := map[string][]string{
		"a": {"b", "c", "d"},
		"b": {"c"},
		"c": {"d"},
	}
	got := transitiveReduction(in)
	want := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"d"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("transitiveReduction = %v, want %v", got, want)
	}
}

// A diamond has no redundant edge — both paths are load-bearing and must survive.
func TestTransitiveReductionKeepsDiamondEdges(t *testing.T) {
	in := map[string][]string{
		"top":   {"left", "right"},
		"left":  {"bottom"},
		"right": {"bottom"},
	}
	got := transitiveReduction(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("transitiveReduction dropped a load-bearing diamond edge: %v", got)
	}
}

// Determinism: predecessor lists must come back sorted, never in map order.
func TestTransitiveReductionIsSorted(t *testing.T) {
	in := map[string][]string{"x": {"c", "a", "b"}}
	got := transitiveReduction(in)
	if !sort.StringsAreSorted(got["x"]) {
		t.Errorf("predecessors not sorted: %v", got["x"])
	}
}

// A cycle is bad input, but the reduction must TERMINATE rather than hang or overflow —
// a malformed committed System must never wedge the derivation.
func TestTransitiveReductionTerminatesOnCycles(_ *testing.T) {
	in := map[string][]string{"a": {"b"}, "b": {"a"}}
	_ = transitiveReduction(in) // must simply return
}

func edgeSystem() SystemView {
	sys := sampleSystem()
	sys.Relationships = []SystemRelationship{
		{From: "order-manager", To: "pricing-engine"},
		{From: "order-manager", To: "order-access"},
		{From: "pricing-engine", To: "order-access"},
		{From: "order-access", To: "order-db"},
	}
	return sys
}

func depsByActivity(deps []NetworkDependency) map[string][]string {
	m := make(map[string][]string, len(deps))
	for _, d := range deps {
		m[d.Activity] = d.DependsOn
	}
	return m
}

// Architecture edges become activity edges, reduced. order-manager→order-access is
// inherited via pricing-engine and must not survive.
func TestDeriveDependenciesMapsRelationshipsAndReduces(t *testing.T) {
	got := depsByActivity(deriveDependencies(edgeSystem(), deriveActivities(edgeSystem())))
	if !reflect.DeepEqual(got["C-order-manager"], []string{"C-pricing-engine"}) {
		t.Errorf("C-order-manager dependsOn = %v, want [C-pricing-engine] after reduction", got["C-order-manager"])
	}
	if !reflect.DeepEqual(got["C-pricing-engine"], []string{"C-order-access"}) {
		t.Errorf("C-pricing-engine dependsOn = %v, want [C-order-access]", got["C-pricing-engine"])
	}
}

// An edge pointing at a component with NO derived activity (an owned store, a generated
// client) must be dropped, not emitted as a dangling reference into the CPM solve.
// The ComponentID contract this whole file depends on: deriveDependencies indexes
// activities by ComponentID to rewrite architecture edges as activity edges, so a
// componentless activity that carried a stray ComponentID would silently capture edges
// meant for a real component. Asserted here, at the consumer, rather than in Task 3.
func TestComponentIDIsSetOnlyOnComponentBoundActivities(t *testing.T) {
	for _, a := range deriveActivities(edgeSystem()) {
		componentBound := strings.HasPrefix(a.Name, "C-") || strings.HasPrefix(a.Name, "R-") ||
			(strings.HasPrefix(a.Name, "U-SPA-") && a.Name != "U-SPA-S")
		switch {
		case componentBound && a.ComponentID == "":
			t.Errorf("%s is component-bound but carries no ComponentID", a.Name)
		case !componentBound && a.ComponentID != "":
			t.Errorf("%s is componentless but carries ComponentID %q; it would capture edges meant for that component",
				a.Name, a.ComponentID)
		}
	}
}

func TestDeriveDependenciesDropsEdgesToComponentsWithNoActivity(t *testing.T) {
	got := depsByActivity(deriveDependencies(edgeSystem(), deriveActivities(edgeSystem())))
	for _, pred := range got["C-order-access"] {
		if pred == "C-order-db" || pred == "R-order-db" {
			t.Errorf("emitted an edge to the owned store order-db: %v", got["C-order-access"])
		}
	}
}

// Fixed pattern edges: the UI design gates SPA construction, the scaffold gates the
// per-manager screens, the test plan gates the harness, and every integration gates the
// terminal system-testing activity.
func TestDeriveDependenciesEmitsFixedPatternEdges(t *testing.T) {
	sys := edgeSystem()
	got := depsByActivity(deriveDependencies(sys, deriveActivities(sys)))
	assertContains := func(activity, want string) {
		t.Helper()
		for _, p := range got[activity] {
			if p == want {
				return
			}
		}
		t.Errorf("%s dependsOn %v, missing %q", activity, got[activity], want)
	}
	assertContains("U-SPA-order-manager", "G-SPA")
	assertContains("U-SPA-order-manager", "U-SPA-S")
	assertContains("N-STH", "N-STP")
	assertContains("N-IT", "I-UC1")
	assertContains("N-IT", "I-UC2")
}

// M0 is the SDP-review milestone: Löwy makes it an explicit forced dependency so that
// no construction activity starts before the review. M1-M3 are layer-completion
// milestones, M4 is use-cases-demonstrable.
func TestDeriveMilestones(t *testing.T) {
	sys := edgeSystem()
	ms := deriveMilestones(sys, deriveActivities(sys))
	byID := make(map[string]NetworkMilestone, len(ms))
	for _, m := range ms {
		byID[m.Id] = m
	}
	for _, want := range []string{"M0", "M1", "M2", "M3", "M4"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("missing derived milestone %q", want)
		}
	}
	if got := byID["M4"].DependsOn; !reflect.DeepEqual(got, []string{"I-UC1", "I-UC2"}) {
		t.Errorf("M4 dependsOn = %v, want the integration set", got)
	}
	if got := byID["M3"].DependsOn; !reflect.DeepEqual(got, []string{"C-order-manager"}) {
		t.Errorf("M3 (managers complete) dependsOn = %v, want [C-order-manager]", got)
	}
	// M5 (v1 Production Live) depends entirely on additive noncoding, so it is NOT
	// derived — it arrives as an additive delta.
	if _, ok := byID["M5"]; ok {
		t.Error("M5 must not be derived; it depends entirely on additive noncoding")
	}
}
