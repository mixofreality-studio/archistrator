package projectstate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	postgresinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// These integration tests exercise the concrete Postgres Store against a real
// Postgres testcontainer (framework-go-infrastructure-postgres/testinfra). They are
// skipped under -short (postgresinfra.StartPostgres handles the skip), so
// `go test -short ./...` spins no container.
//
// This is the developer-owned regression harness for the typed-model head-state
// Store: every contract behaviour that can demonstrate the Store does NOT work has
// a case here (typed-model round-trip through JSONB, optimistic concurrency,
// idempotent replay, per-slot status transitions, the seal, the not-found read,
// caller misuse, and the concurrent-writer race).

// newStore spins a fresh Postgres + applies the schema, returning a ready Store.
func newStore(t *testing.T) (*projectstate.Store, context.Context) {
	t.Helper()
	pool := postgresinfra.StartPostgres(t)
	ctx := context.Background()
	store, err := projectstate.NewStore(ctx, pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store, ctx
}

// assertKind asserts err is an *fwra.Error of the given kind.
func assertKind(t *testing.T, err error, want fwra.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %s, got nil", want)
	}
	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
	if e.Kind != want {
		t.Fatalf("expected kind %s, got %s (detail: %s)", want, e.Kind, e.Detail)
	}
}

func mustMission(t *testing.T, vision string) *projectstate.MissionStatement {
	t.Helper()
	m, err := projectstate.NewMissionStatement(vision, []projectstate.Objective{{Number: 1, Statement: "objective one"}}, "mission in components")
	if err != nil {
		t.Fatalf("NewMissionStatement: %v", err)
	}
	return m
}

// TestStageCommitThenRead: stage a typed *MissionStatement, then commit by kind,
// then read the head-state. The model round-trips through JSONB and lands in the
// Mission slot. Version advances 0 -> 1 -> 2.
func TestStageCommitThenRead(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	mission := mustMission(t, "a terse vision")

	v1, err := store.StageArtifactForReview(ctx, pid, 0, mission, "wf:stage:0")
	if err != nil {
		t.Fatalf("StageArtifactForReview: %v", err)
	}
	if v1 != 1 {
		t.Fatalf("expected version 1 after stage, got %d", v1)
	}

	// After staging, ReadProject shows the Mission slot AwaitingReview with the model.
	staged, err := store.ReadProject(ctx, pid)
	if err != nil {
		t.Fatalf("ReadProject after stage: %v", err)
	}
	if staged.Mission.Status != projectstate.ReviewAwaitingReview {
		t.Fatalf("expected Mission AwaitingReview, got status %d", staged.Mission.Status)
	}
	gotMission, ok := staged.Mission.Model.(*projectstate.MissionStatement)
	if !ok {
		t.Fatalf("expected Mission model to be *projectstate.MissionStatement, got %T", staged.Mission.Model)
	}
	if gotMission.Vision != "a terse vision" {
		t.Fatalf("model did not round-trip: vision %q", gotMission.Vision)
	}
	if len(gotMission.Objectives) != 1 || gotMission.Objectives[0].Statement != "objective one" {
		t.Fatalf("model did not round-trip objectives: %+v", gotMission.Objectives)
	}

	v2, err := store.CommitArtifact(ctx, pid, v1, projectstate.KindMission, "wf:commit:0")
	if err != nil {
		t.Fatalf("CommitArtifact: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("expected version 2 after commit, got %d", v2)
	}

	proj, err := store.ReadProject(ctx, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Version != 2 {
		t.Fatalf("expected head version 2, got %d", proj.Version)
	}
	if proj.Mission.Status != projectstate.ReviewCommitted {
		t.Fatalf("expected Mission Committed, got status %d", proj.Mission.Status)
	}
	// Commit does NOT re-carry the model; it stays in the slot from staging.
	if _, ok := proj.Mission.Model.(*projectstate.MissionStatement); !ok {
		t.Fatalf("expected Mission model retained after commit, got %T", proj.Mission.Model)
	}
}

// TestOptimisticConcurrencyConflict: a stale expectedVersion (two stages both
// targeting version 0) loses with fwra.Conflict.
func TestOptimisticConcurrencyConflict(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	if _, err := store.StageArtifactForReview(ctx, pid, 0, mustMission(t, "v"), "wf:a:0"); err != nil {
		t.Fatalf("first stage: %v", err)
	}
	// Second caller still believes head is 0 -> stale -> loses.
	g, gErr := projectstate.NewGlossary([]projectstate.GlossaryItem{{Term: "t", Definition: "d"}})
	if gErr != nil {
		t.Fatalf("NewGlossary: %v", gErr)
	}
	_, err := store.StageArtifactForReview(ctx, pid, 0, g, "wf:b:0")
	assertKind(t, err, fwra.Conflict)
}

// TestIdempotentRetry: re-applying with the SAME idempotencyKey collapses to a
// no-op that returns the already-committed version (dedup-first), even when the
// caller re-passes a now-stale expectedVersion, and the head is NOT advanced.
func TestIdempotentRetry(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())
	const key fwra.IdempotencyKey = "wf:stage:0"

	v1, err := store.StageArtifactForReview(ctx, pid, 0, mustMission(t, "v"), key)
	if err != nil {
		t.Fatalf("first stage: %v", err)
	}

	// Retry with the same key. Even though the caller re-passes expectedVersion 0
	// (as a retried activity would), the dedup ledger short-circuits to the
	// already-committed version with no error and no second mutation.
	v2, err := store.StageArtifactForReview(ctx, pid, 0, mustMission(t, "v"), key)
	if err != nil {
		t.Fatalf("idempotent retry should succeed, got: %v", err)
	}
	if v1 != v2 {
		t.Fatalf("idempotent retry should return the same version: %d != %d", v1, v2)
	}

	// The head did not advance (proved by a fresh mutation at expectedVersion 1
	// succeeding and landing at version 2).
	v3, err := store.CommitArtifact(ctx, pid, 1, projectstate.KindMission, "wf:commit:1")
	if err != nil {
		t.Fatalf("mutation at version 1 should succeed (head not advanced by replay): %v", err)
	}
	if v3 != 2 {
		t.Fatalf("expected next head version 2, got %d", v3)
	}
}

// TestPerSlotStatusTransitions: drive Mission Stage->Commit, Glossary
// Stage->Reject (with notes), Volatilities Stage->Withdraw (with notes), and
// confirm the head-state shows each slot in its terminal status.
func TestPerSlotStatusTransitions(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	glossary, _ := projectstate.NewGlossary([]projectstate.GlossaryItem{{Term: "t", Definition: "d"}})
	vol := &projectstate.Volatilities{Items: []projectstate.Volatility{{Name: "v", Rationale: "r", Axis: projectstate.AxisSameCustomerOverTime}}}

	type step struct {
		do  func(exp projectstate.Version) (projectstate.Version, error)
		key string
	}
	steps := []step{
		{func(e projectstate.Version) (projectstate.Version, error) {
			return store.StageArtifactForReview(ctx, pid, e, mustMission(t, "m"), "k0")
		}, "k0"},
		{func(e projectstate.Version) (projectstate.Version, error) {
			return store.CommitArtifact(ctx, pid, e, projectstate.KindMission, "k1")
		}, "k1"},
		{func(e projectstate.Version) (projectstate.Version, error) {
			return store.StageArtifactForReview(ctx, pid, e, glossary, "k2")
		}, "k2"},
		{func(e projectstate.Version) (projectstate.Version, error) {
			return store.RejectArtifact(ctx, pid, e, projectstate.KindGlossary, "needs more terms", "k3")
		}, "k3"},
		{func(e projectstate.Version) (projectstate.Version, error) {
			return store.StageArtifactForReview(ctx, pid, e, vol, "k4")
		}, "k4"},
		{func(e projectstate.Version) (projectstate.Version, error) {
			return store.WithdrawArtifact(ctx, pid, e, projectstate.KindVolatilities, "abandoned", "k5")
		}, "k5"},
	}

	var v projectstate.Version
	for i, s := range steps {
		next, err := s.do(v)
		if err != nil {
			t.Fatalf("step %d (%s): %v", i, s.key, err)
		}
		v = next
	}

	proj, err := store.ReadProject(ctx, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Mission.Status != projectstate.ReviewCommitted {
		t.Fatalf("Mission should be Committed, got status %d", proj.Mission.Status)
	}
	if proj.Glossary.Status != projectstate.ReviewRejected {
		t.Fatalf("Glossary should be Rejected, got status %d", proj.Glossary.Status)
	}
	if proj.Glossary.Notes != "needs more terms" {
		t.Fatalf("Glossary reject notes did not round-trip, got %q", proj.Glossary.Notes)
	}
	if proj.Volatilities.Status != projectstate.ReviewWithdrawn {
		t.Fatalf("Volatilities should be Withdrawn, got status %d", proj.Volatilities.Status)
	}
	if proj.Volatilities.Notes != "abandoned" {
		t.Fatalf("Volatilities withdraw notes did not round-trip, got %q", proj.Volatilities.Notes)
	}
	// The Glossary model is retained for the redraft baseline.
	if _, ok := proj.Glossary.Model.(*projectstate.Glossary); !ok {
		t.Fatalf("expected Glossary model retained after reject, got %T", proj.Glossary.Model)
	}
}

// TestAdvancePhaseSeals: AdvancePhase advances the phase and the version while
// leaving the committed slot unchanged.
func TestAdvancePhaseSeals(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	v1, err := store.StageArtifactForReview(ctx, pid, 0, mustMission(t, "m"), "k0")
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	v2, err := store.CommitArtifact(ctx, pid, v1, projectstate.KindMission, "k1")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	v3, err := store.AdvancePhase(ctx, pid, v2, "k2")
	if err != nil {
		t.Fatalf("AdvancePhase: %v", err)
	}
	if v3 != v2+1 {
		t.Fatalf("expected seal to advance version to %d, got %d", v2+1, v3)
	}

	proj, err := store.ReadProject(ctx, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.Phase != projectstate.PhaseProjectDesign {
		t.Fatalf("expected phase advanced to PhaseProjectDesign, got %d", proj.Phase)
	}
	if proj.Mission.Status != projectstate.ReviewCommitted {
		t.Fatal("seal must leave the committed Mission slot unchanged")
	}
}

// TestReadProject_NotFound: an unknown project has no row -> fwra.NotFound.
func TestReadProject_NotFound(t *testing.T) {
	store, ctx := newStore(t)
	_, err := store.ReadProject(ctx, projectstate.ProjectID(uuid.NewString()))
	assertKind(t, err, fwra.NotFound)
}

// TestContractMisuse covers the caller-input pre-conditions.
func TestContractMisuse(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	// Zero projectID.
	_, err := store.CommitArtifact(ctx, projectstate.ProjectID(""), 0, projectstate.KindMission, "k")
	assertKind(t, err, fwra.ContractMisuse)

	// Empty idempotencyKey.
	_, err = store.CommitArtifact(ctx, pid, 0, projectstate.KindMission, "")
	assertKind(t, err, fwra.ContractMisuse)

	// Read with zero projectID.
	_, err = store.ReadProject(ctx, projectstate.ProjectID(""))
	assertKind(t, err, fwra.ContractMisuse)

	// Nil staged model.
	_, err = store.StageArtifactForReview(ctx, pid, 0, nil, "k")
	assertKind(t, err, fwra.ContractMisuse)

	// Commit on an unpopulated slot (no model ever staged for Mission).
	_, err = store.CommitArtifact(ctx, pid, 0, projectstate.KindMission, "commit-unpopulated")
	assertKind(t, err, fwra.ContractMisuse)
}

// TestSolutionSlotRoundTrip: the four Solution slots share *projectstate.Solution and
// are distinguished by SlotKind. Staging a NormalSolution must round-trip and
// restore SlotKind so the read codec assigns it to the right slot.
func TestSolutionSlotRoundTrip(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	sol := projectstate.NewSolution(projectstate.KindNormalSolution)
	v, err := store.StageArtifactForReview(ctx, pid, 0, sol, "k0")
	if err != nil {
		t.Fatalf("stage solution: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected version 1, got %d", v)
	}

	proj, err := store.ReadProject(ctx, pid)
	if err != nil {
		t.Fatalf("ReadProject: %v", err)
	}
	if proj.NormalSolution.Status != projectstate.ReviewAwaitingReview {
		t.Fatalf("expected NormalSolution AwaitingReview, got %d", proj.NormalSolution.Status)
	}
	got, ok := proj.NormalSolution.Model.(*projectstate.Solution)
	if !ok {
		t.Fatalf("expected NormalSolution model *projectstate.Solution, got %T", proj.NormalSolution.Model)
	}
	if got.SlotKind != projectstate.KindNormalSolution {
		t.Fatalf("expected SlotKind KindNormalSolution restored, got %v", got.SlotKind)
	}
}

// TestTickVsOperatorRace: two concurrent mutations on the SAME aggregate at the
// same version contend, and exactly one wins while the other gets fwra.Conflict.
func TestTickVsOperatorRace(t *testing.T) {
	store, ctx := newStore(t)
	pid := projectstate.ProjectID(uuid.NewString())

	// Seed head = 1.
	if _, err := store.StageArtifactForReview(ctx, pid, 0, mustMission(t, "m"), "seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	type outcome struct {
		v   projectstate.Version
		err error
	}
	results := make(chan outcome, 2)
	g, _ := projectstate.NewGlossary([]projectstate.GlossaryItem{{Term: "t", Definition: "d"}})
	vol := &projectstate.Volatilities{Items: []projectstate.Volatility{{Name: "v", Rationale: "r"}}}
	racers := []struct {
		model projectstate.ArtifactModel
		key   fwra.IdempotencyKey
	}{
		{g, "tick"},
		{vol, "operator"},
	}
	for _, r := range racers {
		go func(model projectstate.ArtifactModel, key fwra.IdempotencyKey) {
			v, err := store.StageArtifactForReview(ctx, pid, 1, model, key)
			results <- outcome{v, err}
		}(r.model, r.key)
	}

	var wins, conflicts int
	for i := 0; i < 2; i++ {
		o := <-results
		switch {
		case o.err == nil && o.v == 2:
			wins++
		case o.err != nil:
			var e *fwra.Error
			if errors.As(o.err, &e) && e.Kind == fwra.Conflict {
				conflicts++
			} else {
				t.Fatalf("unexpected error: %v", o.err)
			}
		default:
			t.Fatalf("unexpected outcome: v=%d err=%v", o.v, o.err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("expected exactly 1 winner and 1 conflict, got wins=%d conflicts=%d", wins, conflicts)
	}
}
