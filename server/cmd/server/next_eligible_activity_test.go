package main

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// makeCommittedNetwork builds a minimal committed ArtifactSlot holding a *projectstate.Network.
func makeCommittedNetwork(deps []projectstate.NetworkDependency) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: deps,
		},
	}
}

// makeCommittedActivityList builds a minimal committed ArtifactSlot holding a *projectstate.ActivityList.
func makeCommittedActivityList(items []projectstate.ActivityItem) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.ActivityList{
			Activities: items,
		},
	}
}

// TestNextEligibleActivity_Chain exercises the A→B→C network with progressively
// committed construction status entries.
func TestNextEligibleActivity_Chain(t *testing.T) {
	// Network: A has no deps; B dependsOn A; C dependsOn B.
	network := []projectstate.NetworkDependency{
		{Activity: "A", DependsOn: []string{}},
		{Activity: "B", DependsOn: []string{"A"}},
		{Activity: "C", DependsOn: []string{"B"}},
	}
	activities := []projectstate.ActivityItem{
		{Name: "A", EffortDays: 5, WorkerClass: "AI", Coding: true, RiskBucket: 2},
		{Name: "B", EffortDays: 3, WorkerClass: "AI", Coding: true, RiskBucket: 1},
		{Name: "C", EffortDays: 8, WorkerClass: "Human", Coding: false, RiskBucket: 3},
	}

	base := projectstate.Project{
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
	}

	// ---- Case 1: empty ActivityConstruction → A is eligible (no deps). ----
	proj := base
	got, ok := nextEligibleActivity(proj)
	if !ok {
		t.Fatal("case 1: expected eligible activity, got false")
	}
	if got.ActivityID != "A" {
		t.Fatalf("case 1: expected A, got %q", got.ActivityID)
	}
	if got.EstimateDays != 5 {
		t.Fatalf("case 1: expected EstimateDays=5, got %f", got.EstimateDays)
	}

	// ---- Case 2: A Done → B is eligible. ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
	}
	got, ok = nextEligibleActivity(proj)
	if !ok {
		t.Fatal("case 2: expected eligible activity, got false")
	}
	if got.ActivityID != "B" {
		t.Fatalf("case 2: expected B, got %q", got.ActivityID)
	}
	if got.EstimateDays != 3 {
		t.Fatalf("case 2: expected EstimateDays=3, got %f", got.EstimateDays)
	}

	// ---- Case 3: A Done, B Running → nothing eligible (C blocked; B running). ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
		"B": {ActivityID: "B", Phase: projectstate.ActivityConstructionRunning},
	}
	_, ok = nextEligibleActivity(proj)
	if ok {
		t.Fatal("case 3: expected no eligible activity, got true")
	}

	// ---- Case 4: A Done, B Done → C is eligible. ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
		"B": {ActivityID: "B", Phase: projectstate.ActivityConstructionDone},
	}
	got, ok = nextEligibleActivity(proj)
	if !ok {
		t.Fatal("case 4: expected eligible activity, got false")
	}
	if got.ActivityID != "C" {
		t.Fatalf("case 4: expected C, got %q", got.ActivityID)
	}
	if got.EstimateDays != 8 {
		t.Fatalf("case 4: expected EstimateDays=8, got %f", got.EstimateDays)
	}
}

// TestNextEligibleActivity_UncommittedSlots exercises the nil/uncommitted guard.
func TestNextEligibleActivity_UncommittedSlots(t *testing.T) {
	activities := []projectstate.ActivityItem{
		{Name: "A", EffortDays: 5, WorkerClass: "AI", Coding: true, RiskBucket: 2},
	}

	// Uncommitted Network slot (zero value ArtifactSlot).
	t.Run("uncommitted_network", func(t *testing.T) {
		proj := projectstate.Project{
			ActivityList: makeCommittedActivityList(activities),
		}
		_, ok := nextEligibleActivity(proj)
		if ok {
			t.Fatal("expected false for uncommitted network, got true")
		}
	})

	// Uncommitted ActivityList slot.
	t.Run("uncommitted_activity_list", func(t *testing.T) {
		proj := projectstate.Project{
			Network: makeCommittedNetwork([]projectstate.NetworkDependency{
				{Activity: "A", DependsOn: []string{}},
			}),
		}
		_, ok := nextEligibleActivity(proj)
		if ok {
			t.Fatal("expected false for uncommitted activity list, got true")
		}
	})

	// Both uncommitted (zero-value project).
	t.Run("both_uncommitted", func(t *testing.T) {
		_, ok := nextEligibleActivity(projectstate.Project{})
		if ok {
			t.Fatal("expected false for zero-value project, got true")
		}
	})
}

// TestNextEligibleActivity_HydratedFields checks that the returned ConstructionActivity
// is fully hydrated from the ActivityList item (Kind, ComponentID stay zero/empty since
// the ActivityList has no component/kind — only the fields that map cleanly are set).
func TestNextEligibleActivity_HydratedFields(t *testing.T) {
	network := []projectstate.NetworkDependency{
		{Activity: "X", DependsOn: []string{}},
	}
	activities := []projectstate.ActivityItem{
		{Name: "X", EffortDays: 13, WorkerClass: "HumanSenior", Coding: true, RiskBucket: 5},
	}
	proj := projectstate.Project{
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
	}
	got, ok := nextEligibleActivity(proj)
	if !ok {
		t.Fatal("expected eligible activity")
	}
	if got.ActivityID != "X" {
		t.Fatalf("expected ActivityID=X, got %q", got.ActivityID)
	}
	if got.EstimateDays != 13 {
		t.Fatalf("expected EstimateDays=13, got %f", got.EstimateDays)
	}
	// Kind is determined by Coding flag: Coding=true → ActivityKindConstruction.
	if got.Kind != construction.ActivityKindConstruction {
		t.Fatalf("expected Kind=ActivityKindConstruction, got %v", got.Kind)
	}
}
