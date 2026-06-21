package projectstate_test

// Black-box regression tests for the per-activity construction status head-state
// (Task 1: seed-archistrator-design-state). Mirrors the gitactivity_test.go
// discipline: real throwaway on-disk git store, no mocks, test-authoring
// constitution §7 anti-cheat. Covers:
//   - RecordActivityStarted births the row (Phase=Running, StartedAt set)
//   - RecordActivityCompleted advances to Done, CompletedAt set
//   - idempotent re-record (same key, stale version → ledger wins)
//   - EncodeProjectJSON → DecodeProjectJSON round-trip preserves ActivityConstruction

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	ps "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// newConstructionStore spins a real local git store and seeds a project so
// modeRequireExisting Record* verbs have a row.
func newConstructionStore(t *testing.T) (*ps.GitStore, ps.ProjectID, ps.Version, ps.RepoCredential) {
	t.Helper()
	store, cred, ctx := newLocalGitStore(t)
	id := ps.ProjectID(uuid.NewString())
	v, err := store.CreateProject(ctx, id, "alice", "ConstructionDemo", cred, fwra.IdempotencyKey("wf:create-con"))
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return store, id, v, cred
}

// readConstruction reads the ActivityConstruction row for activityID.
func readConstruction(t *testing.T, store *ps.GitStore, id ps.ProjectID, cred ps.RepoCredential, activityID string) ps.ActivityConstructionStatus {
	t.Helper()
	proj, err := store.ReadProject(context.Background(), id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	s, ok := proj.ActivityConstruction[activityID]
	if !ok {
		t.Fatalf("ActivityConstruction[%s] absent; have keys %v", activityID, constructionKeys(proj))
	}
	return s
}

func constructionKeys(p ps.Project) []string {
	out := make([]string, 0, len(p.ActivityConstruction))
	for k := range p.ActivityConstruction {
		out = append(out, k)
	}
	return out
}

// TestRecordActivityStarted_BirthsRow — RecordActivityStarted births the row with
// Phase=Running and a non-nil StartedAt; CompletedAt must be nil.
func TestRecordActivityStarted_BirthsRow(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordActivityStarted(ctx, id, v, "X001", cred, fwra.IdempotencyKey("wf:started"))
	if err != nil {
		t.Fatalf("RecordActivityStarted: %v", err)
	}
	if v2 != v+1 {
		t.Fatalf("version = %d, want %d", v2, v+1)
	}
	s := readConstruction(t, store, id, cred, "X001")
	if s.ActivityID != "X001" {
		t.Fatalf("ActivityID = %q, want X001", s.ActivityID)
	}
	if s.Phase != ps.ActivityConstructionRunning {
		t.Fatalf("Phase = %v, want Running", s.Phase)
	}
	if s.StartedAt == nil {
		t.Fatal("StartedAt must be set after RecordActivityStarted")
	}
	if s.CompletedAt != nil {
		t.Fatalf("CompletedAt must be nil after started, got %v", s.CompletedAt)
	}
}

// TestRecordActivityCompleted_AdvancesToDone — after a Started, RecordActivityCompleted
// flips Phase to Done and sets CompletedAt.
func TestRecordActivityCompleted_AdvancesToDone(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordActivityStarted(ctx, id, v, "X001", cred, fwra.IdempotencyKey("wf:started-done"))
	if err != nil {
		t.Fatalf("RecordActivityStarted: %v", err)
	}
	v3, err := store.RecordActivityCompleted(ctx, id, v2, "X001", cred, fwra.IdempotencyKey("wf:completed"))
	if err != nil {
		t.Fatalf("RecordActivityCompleted: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("version = %d, want %d", v3, v2+1)
	}
	s := readConstruction(t, store, id, cred, "X001")
	if s.Phase != ps.ActivityConstructionDone {
		t.Fatalf("Phase = %v, want Done", s.Phase)
	}
	if s.CompletedAt == nil {
		t.Fatal("CompletedAt must be set after RecordActivityCompleted")
	}
	if s.StartedAt == nil {
		t.Fatal("StartedAt must still be set after CompletedAt")
	}
}

// TestRecordActivityStarted_Idempotent — retrying the same key with a stale version
// returns the prior Version via the dedup ledger, no double-apply.
func TestRecordActivityStarted_Idempotent(t *testing.T) {
	store, id, v, cred := newConstructionStore(t)
	ctx := context.Background()

	v2, err := store.RecordActivityStarted(ctx, id, v, "X001", cred, fwra.IdempotencyKey("wf:started-idem"))
	if err != nil {
		t.Fatalf("RecordActivityStarted: %v", err)
	}
	before, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}

	// Retry with the SAME key but stale expectedVersion=0; dedup must win.
	v2again, err := store.RecordActivityStarted(ctx, id, 0, "X001", cred, fwra.IdempotencyKey("wf:started-idem"))
	if err != nil {
		t.Fatalf("idempotent retry should succeed via ledger, got: %v", err)
	}
	if v2again != v2 {
		t.Fatalf("idempotent retry version = %d, want original %d", v2again, v2)
	}
	after, err := store.ReadProject(ctx, id, cred)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if after.Version != before.Version {
		t.Fatalf("retry produced a NEW state commit %d → %d (DOUBLE APPLY)", before.Version, after.Version)
	}
}

// TestActivityConstruction_RoundTrip — EncodeProjectJSON → DecodeProjectJSON
// preserves the ActivityConstruction map (phase, timestamps).
func TestActivityConstruction_RoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	comp := now.Add(5 * time.Minute)

	p := ps.Project{}
	p.ActivityConstruction = map[string]ps.ActivityConstructionStatus{
		"X001": {
			ActivityID:  "X001",
			Phase:       ps.ActivityConstructionDone,
			StartedAt:   &now,
			CompletedAt: &comp,
		},
		"X002": {
			ActivityID: "X002",
			Phase:      ps.ActivityConstructionRunning,
			StartedAt:  &now,
		},
	}

	raw, err := ps.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("EncodeProjectJSON: %v", err)
	}
	got, ok, err := ps.DecodeProjectJSON(raw, "")
	if err != nil {
		t.Fatalf("DecodeProjectJSON: %v", err)
	}
	if !ok {
		t.Fatal("DecodeProjectJSON: ok=false, want true")
	}

	x001, found := got.ActivityConstruction["X001"]
	if !found {
		t.Fatal("X001 absent after round-trip")
	}
	if x001.Phase != ps.ActivityConstructionDone {
		t.Fatalf("X001 Phase = %v, want Done", x001.Phase)
	}
	if x001.StartedAt == nil || !x001.StartedAt.Equal(now) {
		t.Fatalf("X001 StartedAt = %v, want %v", x001.StartedAt, now)
	}
	if x001.CompletedAt == nil || !x001.CompletedAt.Equal(comp) {
		t.Fatalf("X001 CompletedAt = %v, want %v", x001.CompletedAt, comp)
	}

	x002, found := got.ActivityConstruction["X002"]
	if !found {
		t.Fatal("X002 absent after round-trip")
	}
	if x002.Phase != ps.ActivityConstructionRunning {
		t.Fatalf("X002 Phase = %v, want Running", x002.Phase)
	}
	if x002.CompletedAt != nil {
		t.Fatalf("X002 CompletedAt should be nil, got %v", x002.CompletedAt)
	}
}

// TestActivityConstructionPhase_String — the phase String() returns wire names.
func TestActivityConstructionPhase_String(t *testing.T) {
	cases := []struct {
		phase ps.ActivityConstructionPhase
		want  string
	}{
		{ps.ActivityConstructionNotStarted, "notStarted"},
		{ps.ActivityConstructionRunning, "running"},
		{ps.ActivityConstructionDone, "done"},
	}
	for _, c := range cases {
		if got := c.phase.String(); got != c.want {
			t.Errorf("Phase(%d).String() = %q, want %q", c.phase, got, c.want)
		}
	}
}
