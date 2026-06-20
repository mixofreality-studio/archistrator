package projectstate_test

import (
	"testing"

	"github.com/google/uuid"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// Integration coverage for the explicit project-registry verbs CreateProject and
// ListProjects (Task 2.3). A project is now born EXPLICITLY (no longer implicitly
// on first SetResearchInput) and carries an owner + name so the landing grid can
// list a principal's projects. These reuse the existing newStore/assertKind
// testcontainer harness and are skipped under -short.

const (
	ownerAlice projectstate.OwnerScope = "alice@example.com"
	ownerBob   projectstate.OwnerScope = "bob@example.com"
)

// TestCreateProject_InsertsAtVersionOne: a fresh CreateProject inserts a row at
// version 1, readable via ReadProject with the owner + name persisted.
func TestCreateProject_InsertsAtVersionOne(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	v, err := store.CreateProject(ctx, pid, ownerAlice, "Order System", "wf:create:0")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected version 1 after create, got %d", v)
	}

	proj, err := store.ReadProject(ctx, pid)
	if err != nil {
		t.Fatalf("ReadProject after create: %v", err)
	}
	if proj.Version != 1 {
		t.Fatalf("expected head version 1, got %d", proj.Version)
	}
	if proj.Phase != projectstate.PhaseSystemDesign {
		t.Fatalf("expected fresh project in PhaseSystemDesign, got %d", proj.Phase)
	}
	if proj.Owner != ownerAlice {
		t.Fatalf("expected owner %q, got %q", ownerAlice, proj.Owner)
	}
	if proj.Name != "Order System" {
		t.Fatalf("expected name %q, got %q", "Order System", proj.Name)
	}
}

// TestCreateProject_DuplicateConflict: creating a project whose id already exists
// is a Conflict (the row is born once). A DIFFERENT idempotency key proves this is
// a real id collision, not a dedup replay.
func TestCreateProject_DuplicateConflict(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	if _, err := store.CreateProject(ctx, pid, ownerAlice, "First", "wf:create:0"); err != nil {
		t.Fatalf("first CreateProject: %v", err)
	}
	_, err := store.CreateProject(ctx, pid, ownerAlice, "Second", "wf:create:1")
	assertKind(t, err, fwra.Conflict)
}

// TestCreateProject_DedupReplay: a retry carrying the SAME idempotency key
// collapses to a no-op success returning the version the first attempt committed
// — NOT a Conflict (the dedup ledger discipline shared with the other verbs).
func TestCreateProject_DedupReplay(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())
	const key fwra.IdempotencyKey = "wf:create:dedup"

	v1, err := store.CreateProject(ctx, pid, ownerAlice, "Order System", key)
	if err != nil {
		t.Fatalf("first CreateProject: %v", err)
	}
	v1again, err := store.CreateProject(ctx, pid, ownerAlice, "Order System", key)
	if err != nil {
		t.Fatalf("dedup replay must succeed, got: %v", err)
	}
	if v1again != v1 {
		t.Fatalf("dedup replay must return the first committed version %d, got %d", v1, v1again)
	}

	// The head version did not advance.
	proj, err := store.ReadProject(ctx, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != v1 {
		t.Fatalf("dedup replay must not bump version: have %d, want %d", proj.Version, v1)
	}
}

// TestCreateProject_ContractMisuse covers the caller-input pre-conditions.
func TestCreateProject_ContractMisuse(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	// Zero projectID.
	_, err := store.CreateProject(ctx, projectstate.ProjectID(""), ownerAlice, "n", "k")
	assertKind(t, err, fwra.ContractMisuse)

	// Empty idempotencyKey.
	_, err = store.CreateProject(ctx, pid, ownerAlice, "n", "")
	assertKind(t, err, fwra.ContractMisuse)

	// Empty owner.
	_, err = store.CreateProject(ctx, pid, "", "n", "k")
	assertKind(t, err, fwra.ContractMisuse)
}

// TestSetResearchInput_RequiresExistingProject: SetResearchInput no longer creates
// the project implicitly. Against an id with no row it returns NotFound; once the
// project exists (via CreateProject) the same call succeeds.
func TestSetResearchInput_RequiresExistingProject(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	// No row yet → NotFound (no implicit creation).
	_, err := store.SetResearchInput(ctx, pid, 0, sampleResearch(), "wf:research:0")
	assertKind(t, err, fwra.NotFound)

	// Create the project, then SetResearchInput at the post-create version.
	v1, err := store.CreateProject(ctx, pid, ownerAlice, "Order System", "wf:create:0")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	v2, err := store.SetResearchInput(ctx, pid, v1, sampleResearch(), "wf:research:0")
	if err != nil {
		t.Fatalf("SetResearchInput after create: %v", err)
	}
	if v2 != v1+1 {
		t.Fatalf("expected version %d after research, got %d", v1+1, v2)
	}
}

// TestListProjects_FiltersByOwner_NewestFirst: ListProjects returns only the given
// owner's projects, newest-first, with name/owner/phase carried in the summary.
func TestListProjects_FiltersByOwner_NewestFirst(t *testing.T) {
	store, ctx := newStore(t)

	aliceOld := projectstate.ProjectID(uuid.NewString())
	aliceNew := projectstate.ProjectID(uuid.NewString())
	bobOne := projectstate.ProjectID(uuid.NewString())

	if _, err := store.CreateProject(ctx, aliceOld, ownerAlice, "Alpha", "wf:c:1"); err != nil {
		t.Fatalf("create aliceOld: %v", err)
	}
	if _, err := store.CreateProject(ctx, bobOne, ownerBob, "Bobs", "wf:c:2"); err != nil {
		t.Fatalf("create bobOne: %v", err)
	}
	if _, err := store.CreateProject(ctx, aliceNew, ownerAlice, "Beta", "wf:c:3"); err != nil {
		t.Fatalf("create aliceNew: %v", err)
	}

	summaries, err := store.ListProjects(ctx, ownerAlice)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 projects for alice, got %d: %+v", len(summaries), summaries)
	}
	// Newest-first: aliceNew was created last.
	if summaries[0].ProjectID != aliceNew {
		t.Fatalf("expected newest-first ordering (aliceNew first), got %v", summaries[0].ProjectID)
	}
	if summaries[1].ProjectID != aliceOld {
		t.Fatalf("expected aliceOld second, got %v", summaries[1].ProjectID)
	}
	if summaries[0].Name != "Beta" || summaries[0].Owner != ownerAlice {
		t.Fatalf("summary fields not carried: %+v", summaries[0])
	}
	if summaries[0].Phase != projectstate.PhaseSystemDesign {
		t.Fatalf("expected PhaseSystemDesign in summary, got %d", summaries[0].Phase)
	}
}

// TestListProjects_CountsCommittedSlots: the summary reports committed vs total
// artifact slots for the current phase. A fresh project has 0 committed; committing
// a Phase-1 slot bumps CommittedCount; TotalCount is the count of required Phase-1
// slots.
func TestListProjects_CountsCommittedSlots(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	v1, err := store.CreateProject(ctx, pid, ownerAlice, "Order System", "wf:c:0")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Fresh project: 0 committed, TotalCount == number of required Phase-1 slots.
	summaries, err := store.ListProjects(ctx, ownerAlice)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 project, got %d", len(summaries))
	}
	wantTotal := len(projectstate.Phase1RequiredKinds())
	if summaries[0].TotalCount != wantTotal {
		t.Fatalf("expected TotalCount %d for fresh Phase-1 project, got %d", wantTotal, summaries[0].TotalCount)
	}
	if summaries[0].CommittedCount != 0 {
		t.Fatalf("expected 0 committed on a fresh project, got %d", summaries[0].CommittedCount)
	}

	// Stage + commit the Mission slot → CommittedCount becomes 1.
	v2, err := store.StageArtifactForReview(ctx, pid, v1, mustMission(t, "a vision"), "wf:stage:0")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	if _, err := store.CommitArtifact(ctx, pid, v2, projectstate.KindMission, "wf:commit:0"); err != nil {
		t.Fatalf("CommitArtifact: %v", err)
	}

	summaries, err = store.ListProjects(ctx, ownerAlice)
	if err != nil {
		t.Fatalf("ListProjects after commit: %v", err)
	}
	if summaries[0].CommittedCount != 1 {
		t.Fatalf("expected CommittedCount 1 after committing Mission, got %d", summaries[0].CommittedCount)
	}
	if summaries[0].TotalCount != wantTotal {
		t.Fatalf("TotalCount must remain %d, got %d", wantTotal, summaries[0].TotalCount)
	}
}

// TestListProjects_EmptyForUnknownOwner: an owner with no projects gets an empty,
// non-nil slice (not an error).
func TestListProjects_EmptyForUnknownOwner(t *testing.T) {
	store, ctx := newStore(t)

	summaries, err := store.ListProjects(ctx, "nobody@example.com")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected no projects, got %d", len(summaries))
	}
}

// TestListProjects_ContractMisuse: an empty owner is caller misuse.
func TestListProjects_ContractMisuse(t *testing.T) {
	store, ctx := newStore(t)
	_, err := store.ListProjects(ctx, "")
	assertKind(t, err, fwra.ContractMisuse)
}
