package projectstate_test

import (
	"testing"

	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// Additive (➕ 2026-05-29) regression coverage for the ResearchInput Method-input
// field + setResearchInput verb (projectStateAccess.md §0b/§2/§3.8). Purely
// additive — these reuse the existing newStore/assertKind harness and do not
// touch any existing slot/verb test. Skipped under -short (newStore via
// postgresinfra.StartPostgres).

func sampleResearch() projectstate.ResearchInput {
	return projectstate.ResearchInput{Sources: []projectstate.ResearchSource{
		{Title: "Founder brief", Content: "we want to automate The Method"},
		{Title: "Competitor analysis", Content: "no one applies Lowy end-to-end"},
	}}
}

// TestSetResearchInput_RoundTrip: set the research corpus on a fresh aggregate,
// then read it back whole via ReadProject. Version advances 0 -> 1.
func TestSetResearchInput_RoundTrip(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	// A project is born explicitly now (Task 2.3); research is set on the existing row.
	vc, err := store.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: "wf:create:0"}, pid, "owner@example.com", "Order System")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	research := sampleResearch()
	v1, err := store.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: "wf:research:0"}, pid, vc, research)
	if err != nil {
		t.Fatalf("SetResearchInput: %v", err)
	}
	if v1 != 2 {
		t.Fatalf("expected version 2 after create+set, got %d", v1)
	}

	proj, err := store.ReadProject(fwra.Context{Context: ctx}, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.ResearchInput.IsZero() {
		t.Fatal("ResearchInput must round-trip non-empty")
	}
	if len(proj.ResearchInput.Sources) != 2 {
		t.Fatalf("expected 2 research sources, got %d", len(proj.ResearchInput.Sources))
	}
	if proj.ResearchInput.Sources[0].Title != "Founder brief" || proj.ResearchInput.Sources[0].Content != "we want to automate The Method" {
		t.Fatalf("research source did not round-trip: %+v", proj.ResearchInput.Sources[0])
	}
}

// TestSetResearchInput_ReplaceInPlace: a second SetResearchInput replaces the
// corpus (set once, replaceable). ResearchInput is NOT review-gated.
func TestSetResearchInput_ReplaceInPlace(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	vc, err := store.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: "wf:create:0"}, pid, "owner@example.com", "Order System")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	v1, err := store.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: "wf:research:0"}, pid, vc, sampleResearch())
	if err != nil {
		t.Fatalf("first SetResearchInput: %v", err)
	}

	replacement := projectstate.ResearchInput{Sources: []projectstate.ResearchSource{
		{Title: "Customer interview #3", Content: "they want full audit trails"},
	}}
	v2, err := store.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: "wf:research:1"}, pid, v1, replacement)
	if err != nil {
		t.Fatalf("second SetResearchInput: %v", err)
	}
	if v2 != 3 {
		t.Fatalf("expected version 3 after create+set+replace, got %d", v2)
	}

	proj, err := store.ReadProject(fwra.Context{Context: ctx}, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if len(proj.ResearchInput.Sources) != 1 || proj.ResearchInput.Sources[0].Title != "Customer interview #3" {
		t.Fatalf("replace-in-place did not take: %+v", proj.ResearchInput.Sources)
	}
}

// TestSetResearchInput_EmptyResearch_ContractMisuse: a zero ResearchInput is
// rejected before any infrastructure write.
func TestSetResearchInput_EmptyResearch_ContractMisuse(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	_, err := store.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: "wf:research:empty"}, pid, 0, projectstate.ResearchInput{})
	assertKind(t, err, fwra.ContractMisuse)
}

// TestSetResearchInput_DedupReplay: a retry carrying the SAME idempotency key
// collapses to a no-op success returning the version the first attempt committed
// (the dedup ledger discipline shared with the slot verbs).
func TestSetResearchInput_DedupReplay(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	vc, err := store.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: "wf:create:0"}, pid, "owner@example.com", "Order System")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	v1, err := store.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: "wf:research:dedup"}, pid, vc, sampleResearch())
	if err != nil {
		t.Fatalf("first SetResearchInput: %v", err)
	}

	// Same key, a now-stale expectedVersion: the dedup probe wins and returns v1.
	v1again, err := store.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: "wf:research:dedup"}, pid, vc, sampleResearch())
	if err != nil {
		t.Fatalf("dedup replay must succeed, got: %v", err)
	}
	if v1again != v1 {
		t.Fatalf("dedup replay must return the first committed version %d, got %d", v1, v1again)
	}

	// And the head version did not advance past the first commit.
	proj, err := store.ReadProject(fwra.Context{Context: ctx}, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v1 {
		t.Fatalf("dedup replay must not bump version: have %d, want %d", proj.Version, v1)
	}
}

// TestSetResearchInput_CoexistsWithSlots: ResearchInput and the artifact slots
// are independent — setting research does not disturb a staged Mission slot, and
// staging a Mission does not clear ResearchInput.
func TestSetResearchInput_CoexistsWithSlots(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	vc, err := store.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: "wf:create:0"}, pid, "owner@example.com", "Order System")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	v1, err := store.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: "wf:research:0"}, pid, vc, sampleResearch())
	if err != nil {
		t.Fatalf("SetResearchInput: %v", err)
	}

	v2, err := store.StageArtifactForReview(fwra.Context{Context: ctx, IdempotencyKey: "wf:stage:0"}, pid, v1, mustMission(t, "a vision"))
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}

	proj, err := store.ReadProject(fwra.Context{Context: ctx}, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v2 {
		t.Fatalf("expected head version %d, got %d", v2, proj.Version)
	}
	if proj.ResearchInput.IsZero() {
		t.Fatal("staging a slot must not clear ResearchInput")
	}
	if proj.Mission.Status != projectstate.ReviewAwaitingReview {
		t.Fatalf("Mission slot must be AwaitingReview, got %d", proj.Mission.Status)
	}
}
