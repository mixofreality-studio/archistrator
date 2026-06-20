package systemdesign

import (
	"context"
	"testing"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
	fwmanager "github.com/davidmarne/archistrator-platform/framework-go/manager"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
	"github.com/google/uuid"
)

// These tests cover the SYNC, non-Temporal SetResearchInput op (op 2.6,
// systemDesignManager.md §2.6). They run entirely on the sync read/write path
// (no Temporal client), so a nil client is safe — the op never touches Temporal.

// setResearchFakeState is a recording fake of projectStateAccess for the
// SetResearchInput op. It records each (expectedVersion, research, idempotencyKey)
// the Manager passes to SetResearchInput, returns the head Version on ReadProject,
// and can be programmed to surface a Conflict on the first N writes (to exercise
// the sync-path re-read/re-apply loop). Verbs the op must NOT call panic.
type setResearchFakeState struct {
	headVersion projectstate.Version
	readErr     error

	// conflictsBeforeSuccess: the first N writes return fwra.Conflict; the rest
	// succeed. The fake bumps headVersion on each Conflict so a re-read sees a
	// fresh version (mirroring a concurrent writer).
	conflictsBeforeSuccess int

	// recorded write calls (in order).
	gotExpected []projectstate.Version
	gotResearch []projectstate.ResearchInput
	gotKeys     []fwra.IdempotencyKey
	readCalls   int
}

func (f *setResearchFakeState) ReadProject(context.Context, projectstate.ProjectID) (projectstate.Project, error) {
	f.readCalls++
	if f.readErr != nil {
		return projectstate.Project{}, f.readErr
	}
	return projectstate.Project{Version: f.headVersion}, nil
}

func (f *setResearchFakeState) SetResearchInput(_ context.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, research projectstate.ResearchInput, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.gotExpected = append(f.gotExpected, expectedVersion)
	f.gotResearch = append(f.gotResearch, research)
	f.gotKeys = append(f.gotKeys, key)
	if len(f.gotExpected) <= f.conflictsBeforeSuccess {
		// Concurrent writer bumped the row: surface Conflict and advance head so the
		// Manager's re-read sees a fresh expectedVersion.
		f.headVersion++
		return 0, fwra.New(fwra.Conflict, "stale version")
	}
	f.headVersion++
	return f.headVersion, nil
}

func (f *setResearchFakeState) StageArtifactForReview(context.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactModel, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.StageArtifactForReview must not be called by SetResearchInput")
}

func (f *setResearchFakeState) CommitArtifact(context.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.CommitArtifact must not be called by SetResearchInput")
}

func (f *setResearchFakeState) RejectArtifact(context.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.RejectArtifact must not be called by SetResearchInput")
}

func (f *setResearchFakeState) WithdrawArtifact(context.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.WithdrawArtifact must not be called by SetResearchInput")
}

func (f *setResearchFakeState) AdvancePhase(context.Context, projectstate.ProjectID, projectstate.Version, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.AdvancePhase must not be called by SetResearchInput")
}

func (f *setResearchFakeState) CreateProject(context.Context, projectstate.ProjectID, projectstate.OwnerScope, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	panic("setResearchFakeState.CreateProject must not be called by SetResearchInput")
}

func (f *setResearchFakeState) ListProjects(context.Context, projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	panic("setResearchFakeState.ListProjects must not be called by SetResearchInput")
}

var _ projectstate.ProjectStateAccess = (*setResearchFakeState)(nil)

func sampleResearch() projectstate.ResearchInput {
	return projectstate.ResearchInput{Sources: []projectstate.ResearchSource{
		{Title: "Founder brief", Content: "We are building X for Y."},
	}}
}

// ---- façade preconditions ---------------------------------------------------

func Test_SetResearchInput_EmptyProjectID(t *testing.T) {
	m := NewManager(nil, &setResearchFakeState{})
	_, err := m.SetResearchInput(context.Background(), ProjectID(""), sampleResearch())
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for empty projectId, got %d", got)
	}
}

func Test_SetResearchInput_EmptyResearch(t *testing.T) {
	m := NewManager(nil, &setResearchFakeState{})
	_, err := m.SetResearchInput(context.Background(), ProjectID(uuid.NewString()), projectstate.ResearchInput{})
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for empty research, got %d", got)
	}
}

// ---- happy path -------------------------------------------------------------

func Test_SetResearchInput_HappyPath_RecordsWrite(t *testing.T) {
	ps := &setResearchFakeState{headVersion: 7}
	m := NewManager(nil, ps)
	research := sampleResearch()

	v, err := m.SetResearchInput(context.Background(), ProjectID(uuid.NewString()), research)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ps.gotExpected) != 1 {
		t.Fatalf("want exactly one write, got %d", len(ps.gotExpected))
	}
	if ps.gotExpected[0] != 7 {
		t.Fatalf("want expectedVersion 7 (the head just read), got %d", ps.gotExpected[0])
	}
	if len(ps.gotResearch[0].Sources) != 1 || ps.gotResearch[0].Sources[0].Title != "Founder brief" {
		t.Fatalf("research not passed through faithfully: %+v", ps.gotResearch[0])
	}
	if ps.gotKeys[0].IsZero() {
		t.Fatalf("want a non-empty idempotencyKey")
	}
	if v != 8 {
		t.Fatalf("want resulting Version 8 (head bumped), got %d", v)
	}
}

// A stable research payload derives a stable idempotency key (so a retried write
// of the SAME research collapses to a dedup no-op in the RA ledger).
func Test_SetResearchInput_IdempotencyKey_StableForSameResearch(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	research := sampleResearch()

	ps1 := &setResearchFakeState{}
	ps2 := &setResearchFakeState{}
	m1 := NewManager(nil, ps1)
	m2 := NewManager(nil, ps2)
	if _, err := m1.SetResearchInput(context.Background(), pid, research); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if _, err := m2.SetResearchInput(context.Background(), pid, research); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if ps1.gotKeys[0] != ps2.gotKeys[0] {
		t.Fatalf("same (project, research) must derive the same key: %q vs %q", ps1.gotKeys[0], ps2.gotKeys[0])
	}

	// A different research payload must derive a DIFFERENT key.
	other := projectstate.ResearchInput{Sources: []projectstate.ResearchSource{{Title: "Competitor analysis", Content: "Z does W."}}}
	ps3 := &setResearchFakeState{}
	m3 := NewManager(nil, ps3)
	if _, err := m3.SetResearchInput(context.Background(), pid, other); err != nil {
		t.Fatalf("write 3: %v", err)
	}
	if ps3.gotKeys[0] == ps1.gotKeys[0] {
		t.Fatalf("different research must derive a different key")
	}
}

// ---- conflict-then-success sync re-read/re-apply loop -----------------------

func Test_SetResearchInput_ConflictThenSuccess_ReReads(t *testing.T) {
	ps := &setResearchFakeState{headVersion: 3, conflictsBeforeSuccess: 2}
	m := NewManager(nil, ps)

	v, err := m.SetResearchInput(context.Background(), ProjectID(uuid.NewString()), sampleResearch())
	if err != nil {
		t.Fatalf("expected success after conflicts, got %v", err)
	}
	if len(ps.gotExpected) != 3 {
		t.Fatalf("want 3 write attempts (2 conflicts + 1 success), got %d", len(ps.gotExpected))
	}
	// Each attempt must re-read the (bumped) head version before re-applying.
	if ps.readCalls != 3 {
		t.Fatalf("want 3 ReadProject calls (one per attempt), got %d", ps.readCalls)
	}
	if !(ps.gotExpected[0] < ps.gotExpected[1] && ps.gotExpected[1] < ps.gotExpected[2]) {
		t.Fatalf("each re-apply must carry a fresh (higher) expectedVersion, got %v", ps.gotExpected)
	}
	// The SAME idempotency key is reused across re-applies (one logical mutation).
	if ps.gotKeys[0] != ps.gotKeys[1] || ps.gotKeys[1] != ps.gotKeys[2] {
		t.Fatalf("re-applies must reuse the same idempotencyKey, got %v", ps.gotKeys)
	}
	if v == 0 {
		t.Fatalf("want a non-zero resulting Version")
	}
}

func Test_SetResearchInput_ConflictExhausted_Infrastructure(t *testing.T) {
	ps := &setResearchFakeState{conflictsBeforeSuccess: setResearchInputMaxAttempts + 1}
	m := NewManager(nil, ps)
	_, err := m.SetResearchInput(context.Background(), ProjectID(uuid.NewString()), sampleResearch())
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure after exhausting conflict retries, got %d", got)
	}
	if len(ps.gotExpected) != setResearchInputMaxAttempts {
		t.Fatalf("want exactly %d bounded attempts, got %d", setResearchInputMaxAttempts, len(ps.gotExpected))
	}
}

// ---- error passthrough ------------------------------------------------------

func Test_SetResearchInput_NotFound_Passthrough(t *testing.T) {
	// ReadProject succeeds but the write surfaces NotFound (no project aggregate).
	ps := &setResearchNotFoundOnWrite{}
	m := NewManager(nil, ps)
	_, err := m.SetResearchInput(context.Background(), ProjectID(uuid.NewString()), sampleResearch())
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.NotFound {
		t.Fatalf("want NotFound passthrough, got %d", got)
	}
}

func Test_SetResearchInput_ReadNotFound_Propagates(t *testing.T) {
	ps := &setResearchFakeState{readErr: fwra.New(fwra.NotFound, "no row yet")}
	m := NewManager(nil, ps)
	_, err := m.SetResearchInput(context.Background(), ProjectID(uuid.NewString()), sampleResearch())
	if got := asSystemDesignError(t, err).Kind; got != fwmanager.NotFound {
		t.Fatalf("want NotFound when ReadProject reports no row, got %d", got)
	}
}

// setResearchNotFoundOnWrite reads fine but the write reports NotFound.
type setResearchNotFoundOnWrite struct{ setResearchFakeState }

func (f *setResearchNotFoundOnWrite) SetResearchInput(context.Context, projectstate.ProjectID, projectstate.Version, projectstate.ResearchInput, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, fwra.New(fwra.NotFound, "no project aggregate")
}

var _ projectstate.ProjectStateAccess = (*setResearchNotFoundOnWrite)(nil)
