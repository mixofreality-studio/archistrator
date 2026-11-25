package internal_test

import (
	"slices"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
)

// The derived plan carries one noncoding activity no component owns: the use-case acceptance test
// plan (Amendment C). It binds the residual paths of the whole system, so it exists for every system
// that has a component. It no longer runs beside construction: it runs after the client and manager
// work whose contracts its residual is projected over, because a residual read before any client
// contract exists is empty by construction (ISS-028-3).

func TestDerivedPlan_EmitsNATP(t *testing.T) {
	system := estimation.SystemView{Components: []estimation.SystemComponent{
		{ID: "list-manager", Name: "ListManager", Kind: "manager"},
		{ID: "todo-web-app", Name: "TodoWebApp", Kind: "client", Surface: "web"},
	}}
	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, system, estimation.ActivityListDeltas{})
	if err != nil {
		t.Fatal(err)
	}
	var got []estimation.DerivedActivity
	for _, a := range plan.Activities {
		if a.Name == "N-ATP" {
			got = append(got, a)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want exactly one N-ATP, got %+v", got)
	}
	a := got[0]
	if a.WorkerClass != "test-engineer" || a.EffortDays != 5 || a.Coding || !a.Derived || a.ComponentID != "" || a.Title == "" {
		t.Errorf("N-ATP = %+v, want a derived noncoding 5-day test-engineer activity that owns no component", a)
	}
	requireNATPRow(t, plan, []string{"C-list-manager", "U-SPA-todo-web-app"})
}

// A client app that already depends on its manager reduces the manager away.
func TestDerivedPlan_NATPReducesManagerBehindClientApp(t *testing.T) {
	system := estimation.SystemView{
		Components: []estimation.SystemComponent{
			{ID: "list-manager", Name: "ListManager", Kind: "manager"},
			{ID: "todo-web-app", Name: "TodoWebApp", Kind: "client", Surface: "web"},
		},
		Relationships: []estimation.SystemRelationship{{From: "todo-web-app", To: "list-manager"}},
	}
	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, system, estimation.ActivityListDeltas{})
	if err != nil {
		t.Fatal(err)
	}
	requireNATPRow(t, plan, []string{"U-SPA-todo-web-app"})
}

// With no client and no manager the residual has nothing to read: the source rule applies.
func TestDerivedPlan_NATPWithoutClientOrManagerStaysOnM0(t *testing.T) {
	system := estimation.SystemView{Components: []estimation.SystemComponent{
		{ID: "ruling-engine", Name: "RulingEngine", Kind: "engine"},
	}}
	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, system, estimation.ActivityListDeltas{})
	if err != nil {
		t.Fatal(err)
	}
	requireNATPRow(t, plan, []string{"M0"})
}

func requireNATPRow(t *testing.T, plan estimation.DerivedPlan, want []string) {
	t.Helper()
	for _, d := range plan.Dependencies {
		if d.Activity != "N-ATP" {
			continue
		}
		if !slices.Equal(d.DependsOn, want) {
			t.Errorf("N-ATP depends on %v, want %v", d.DependsOn, want)
		}
		return
	}
	t.Error("N-ATP has no dependency row: an activity with no incident edge gets no CPM node")
}

func TestDerivedPlan_EmptySystemDerivesNoNATP(t *testing.T) {
	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, estimation.SystemView{}, estimation.ActivityListDeltas{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range plan.Activities {
		if a.Name == "N-ATP" {
			t.Fatalf("an empty system has nothing to accept, got %+v", a)
		}
	}
}

// M-ACCEPT is the node that runs the milestone's job (the pump opens activities, never milestones),
// so it is a zero-effort derived activity. It waits for every terminal of the plan and for the
// acceptance test plan, and the project ends only after it: the one sink depends on it alone.
func TestDerivedPlan_MAcceptPrecedesMEND(t *testing.T) {
	system := estimation.SystemView{Components: []estimation.SystemComponent{
		{ID: "list-manager", Name: "ListManager", Kind: "manager"},
		{ID: "ruling-engine", Name: "RulingEngine", Kind: "engine"},
		{ID: "todo-web-app", Name: "TodoWebApp", Kind: "client", Surface: "web"},
		{ID: "stripe", Name: "Stripe", Kind: "resource", Provisioning: "vendor"},
	}}
	additive := []estimation.AdditiveActivity{{
		Name: "N-DOC-guide", Title: "Operator guide", EffortDays: 5, RiskBucket: 2, WorkerClass: "product-manager",
		Justification: "nothing in the derivation writes the operator's guide",
	}}
	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, system, estimation.ActivityListDeltas{Additive: additive})
	if err != nil {
		t.Fatal(err)
	}
	requireMAcceptActivity(t, plan)
	// N-ATP follows the client app and the manager (ISS-028-3), so neither is terminal.
	wantTerminals := []string{"C-ruling-engine", "N-ATP", "N-DOC-guide", "R-stripe"}
	var terminals []string
	for _, d := range plan.Dependencies {
		if d.Activity == "M-ACCEPT" {
			terminals = d.DependsOn
		}
		if slices.Contains(d.DependsOn, "M-ACCEPT") {
			t.Errorf("%s depends on M-ACCEPT: nothing but the project end comes after it", d.Activity)
		}
	}
	if !slices.Equal(terminals, wantTerminals) {
		t.Errorf("M-ACCEPT depends on %v, want every other terminal and N-ATP: %v", terminals, wantTerminals)
	}
	for _, m := range plan.Milestones {
		if m.Id == "MEND" && slices.Equal(m.DependsOn, []string{"M-ACCEPT"}) {
			return
		}
	}
	t.Fatalf("no MEND depending on M-ACCEPT alone in %+v", plan.Milestones)
}

// requireMAcceptActivity holds the plan to one derived zero-effort noncoding system-architect M-ACCEPT.
func requireMAcceptActivity(t *testing.T, plan estimation.DerivedPlan) {
	t.Helper()
	for _, a := range plan.Activities {
		if a.Name != "M-ACCEPT" {
			continue
		}
		if a.EffortDays != 0 || !a.Derived || a.Coding || a.WorkerClass != "system-architect" || a.Title == "" {
			t.Errorf("M-ACCEPT = %+v, want a derived noncoding zero-effort system-architect activity", a)
		}
		return
	}
	t.Fatalf("no M-ACCEPT in %+v", plan.Activities)
}
