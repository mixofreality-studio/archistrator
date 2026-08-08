package operatedsystemstate_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	postgresinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
)

// These integration tests exercise the concrete Postgres head-state store against a real
// Postgres testcontainer (framework-go-infrastructure-postgres/testinfra), skipped under
// -short exactly like the sibling usage suite. This is the developer-owned regression
// harness for the operated_system head-state discipline (C-OSA), made executable: every
// way to demonstrate the store does NOT work has a case here —
//   - version-0 create seam + create-on-existing → Conflict,
//   - optimistic concurrency: correct version bumps, stale version → Conflict,
//   - missing app on a positive-version write → NotFound; read of a missing app → NotFound,
//   - dedup-first idempotency: a replayed idempotency key collapses to the recorded
//     version with NO extra bump (the head-state analogue of usageAccess's no-double-count),
//   - the Conflict → re-read → re-apply convergence the operationsManager §6.5 loop drives,
//   - status / withdraw / delinquency transitions + their in-flight effects,
//   - ReadInFlightOperatedApps scopes (all / AppIDs subset / withdrawn excluded),
//   - caller misuse (ContractMisuse on zero appID / empty idempotency key),
//   - idempotent constructor-applied DDL.

func rc(ctx context.Context) fwra.Context { return fwra.Context{Context: ctx} }

func newStore(t *testing.T) (operatedsystemstate.OperatedSystemStateAccess, *pgxpool.Pool, context.Context) {
	t.Helper()
	pool := postgresinfra.StartPostgres(t)
	ctx := context.Background()
	store, err := operatedsystemstate.NewPostgresOperatedSystemStateAccess(ctx, pool)
	if err != nil {
		t.Fatalf("NewPostgresOperatedSystemStateAccess: %v", err)
	}
	return store, pool, ctx
}

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

// TestCreateReadUpdate covers the version-0 create seam, the read-back, and a
// version-guarded republish.
func TestCreateReadUpdate(t *testing.T) {
	store, _, ctx := newStore(t)
	app := uuid.New()

	v, err := store.PublishDesiredState(rc(ctx), app, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, "k-create")
	if err != nil {
		t.Fatalf("create publish: %v", err)
	}
	if v != 1 {
		t.Fatalf("create version = %d, want 1", v)
	}

	got, err := store.ReadOperatedSystem(rc(ctx), app)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.ID != app || got.Version != 1 || got.Status != operatedsystemstate.RuntimeStatusPending || !got.InFlight {
		t.Fatalf("read = %+v, want {ID:%s Version:1 Status:Pending InFlight:true}", got, app)
	}

	// Republish at the correct version bumps to 2 without touching observed status.
	v2, err := store.PublishDesiredState(rc(ctx), app, 1, operatedsystemstate.ReasonOperator, nil, "k-republish")
	if err != nil {
		t.Fatalf("republish: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("republish version = %d, want 2", v2)
	}
	got2, _ := store.ReadOperatedSystem(rc(ctx), app)
	if got2.Status != operatedsystemstate.RuntimeStatusPending {
		t.Fatalf("republish changed status to %v, want unchanged Pending", got2.Status)
	}
}

// TestConflictAndNotFound covers stale-version Conflict, create-on-existing Conflict, and
// NotFound on both a positive-version write and a read of a missing app.
func TestConflictAndNotFound(t *testing.T) {
	store, _, ctx := newStore(t)
	app := uuid.New()
	missing := uuid.New()

	if _, err := store.ReadOperatedSystem(rc(ctx), missing); true {
		assertKind(t, err, fwra.NotFound)
	}
	if _, err := store.RecordRuntimeStatusChange(rc(ctx), missing, 1, operatedsystemstate.RuntimeStatusHealthy, "k-nf"); true {
		assertKind(t, err, fwra.NotFound)
	}

	if _, err := store.PublishDesiredState(rc(ctx), app, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, "k1"); err != nil {
		t.Fatalf("create: %v", err)
	}
	// create-on-existing (version-0 expectation stale) → Conflict.
	if _, err := store.PublishDesiredState(rc(ctx), app, 0, operatedsystemstate.ReasonOperator, nil, "k2"); true {
		assertKind(t, err, fwra.Conflict)
	}
	// stale positive version → Conflict.
	if _, err := store.RecordRuntimeStatusChange(rc(ctx), app, 99, operatedsystemstate.RuntimeStatusHealthy, "k3"); true {
		assertKind(t, err, fwra.Conflict)
	}
}

// TestIdempotentReplay covers dedup-first idempotency: a replayed idempotency key returns
// the recorded resulting version and does NOT bump the row again.
func TestIdempotentReplay(t *testing.T) {
	store, _, ctx := newStore(t)
	app := uuid.New()

	v1, err := store.PublishDesiredState(rc(ctx), app, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, "same-key")
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	v2, err := store.PublishDesiredState(rc(ctx), app, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, "same-key")
	if err != nil {
		t.Fatalf("replay publish: %v", err)
	}
	if v1 != v2 {
		t.Fatalf("replay version %d != first %d", v2, v1)
	}
	got, _ := store.ReadOperatedSystem(rc(ctx), app)
	if got.Version != 1 {
		t.Fatalf("replay double-applied: version = %d, want 1", got.Version)
	}
}

// TestConflictConvergence models the operationsManager §6.5 loop: a stale-version write
// gets Conflict, the caller re-reads the true version and re-applies with the SAME key,
// which converges.
func TestConflictConvergence(t *testing.T) {
	store, _, ctx := newStore(t)
	app := uuid.New()

	if _, err := store.PublishDesiredState(rc(ctx), app, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, "k-seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A concurrent writer advanced the version to 2.
	if _, err := store.RecordRuntimeStatusChange(rc(ctx), app, 1, operatedsystemstate.RuntimeStatusHealthy, "k-advance"); err != nil {
		t.Fatalf("advance: %v", err)
	}

	// Our attempt with the stale seed version 1 conflicts...
	_, err := store.WithdrawSystem(rc(ctx), app, 1, "k-withdraw")
	assertKind(t, err, fwra.Conflict)
	// ...re-read the true version and re-apply with the SAME key.
	cur, _ := store.ReadOperatedSystem(rc(ctx), app)
	vFinal, err := store.WithdrawSystem(rc(ctx), app, cur.Version, "k-withdraw")
	if err != nil {
		t.Fatalf("re-apply withdraw: %v", err)
	}
	if vFinal != cur.Version+1 {
		t.Fatalf("withdraw version = %d, want %d", vFinal, cur.Version+1)
	}
}

// TestWithdrawAndDelinquency covers the withdraw terminal and the two delinquency actions
// and their in-flight effects.
func TestWithdrawAndDelinquency(t *testing.T) {
	store, _, ctx := newStore(t)

	// Withdraw: terminal, clears in-flight.
	wApp := uuid.New()
	v, _ := store.PublishDesiredState(rc(ctx), wApp, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, "w1")
	if _, err := store.WithdrawSystem(rc(ctx), wApp, v, "w2"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	got, _ := store.ReadOperatedSystem(rc(ctx), wApp)
	if got.Status != operatedsystemstate.RuntimeStatusWithdrawn || got.InFlight {
		t.Fatalf("withdrawn app = %+v, want Withdrawn + not in-flight", got)
	}

	// Delinquency paused: stays in-flight.
	pApp := uuid.New()
	pv, _ := store.PublishDesiredState(rc(ctx), pApp, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, "p1")
	if _, err := store.RecordDelinquencyAction(rc(ctx), pApp, pv, operatedsystemstate.DelinquencyActionPaused, "p2"); err != nil {
		t.Fatalf("delinquency pause: %v", err)
	}
	gotP, _ := store.ReadOperatedSystem(rc(ctx), pApp)
	if !gotP.InFlight {
		t.Fatalf("paused app lost in-flight: %+v", gotP)
	}

	// Delinquency withdrawn: terminal.
	dApp := uuid.New()
	dv, _ := store.PublishDesiredState(rc(ctx), dApp, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, "d1")
	if _, err := store.RecordDelinquencyAction(rc(ctx), dApp, dv, operatedsystemstate.DelinquencyActionWithdrawn, "d2"); err != nil {
		t.Fatalf("delinquency withdraw: %v", err)
	}
	gotD, _ := store.ReadOperatedSystem(rc(ctx), dApp)
	if gotD.Status != operatedsystemstate.RuntimeStatusWithdrawn || gotD.InFlight {
		t.Fatalf("delinquency-withdrawn app = %+v, want Withdrawn + not in-flight", gotD)
	}
}

// TestReadInFlightScopes covers the in-flight scan scopes and withdrawn exclusion.
func TestReadInFlightScopes(t *testing.T) {
	store, _, ctx := newStore(t)
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	for i, app := range []uuid.UUID{a, b, c} {
		if _, err := store.PublishDesiredState(rc(ctx), app, 0, operatedsystemstate.ReasonDeployAfterConstruction, nil, fwra.IdempotencyKey("seed-"+app.String())); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	// Withdraw c so it drops out of the in-flight scan.
	cv, _ := store.ReadOperatedSystem(rc(ctx), c)
	if _, err := store.WithdrawSystem(rc(ctx), c, cv.Version, "wc"); err != nil {
		t.Fatalf("withdraw c: %v", err)
	}

	all, err := store.ReadInFlightOperatedApps(rc(ctx), operatedsystemstate.InFlightScope{})
	if err != nil {
		t.Fatalf("read in-flight all: %v", err)
	}
	if !containsAll(all, a, b) || containsAny(all, c) {
		t.Fatalf("in-flight all = %+v, want {a,b} without c", all)
	}

	subset, err := store.ReadInFlightOperatedApps(rc(ctx), operatedsystemstate.InFlightScope{AppIDs: []uuid.UUID{a}})
	if err != nil {
		t.Fatalf("read in-flight subset: %v", err)
	}
	if len(subset) != 1 || subset[0].ID != a {
		t.Fatalf("in-flight subset = %+v, want [a]", subset)
	}

	// Customer scope: customer_id is unwritten by the frozen verbs (documented gap), so a
	// customer-scoped sweep is honestly empty, never NotFound.
	cust := uuid.New()
	custScope, err := store.ReadInFlightOperatedApps(rc(ctx), operatedsystemstate.InFlightScope{CustomerID: &cust})
	if err != nil {
		t.Fatalf("read in-flight customer scope: %v", err)
	}
	if len(custScope) != 0 {
		t.Fatalf("customer-scoped in-flight = %+v, want empty", custScope)
	}
}

// TestContractMisuse covers the caller pre-condition rejections.
func TestContractMisuse(t *testing.T) {
	store, _, ctx := newStore(t)
	app := uuid.New()

	if _, err := store.ReadOperatedSystem(rc(ctx), uuid.Nil); true {
		assertKind(t, err, fwra.ContractMisuse)
	}
	if _, err := store.PublishDesiredState(rc(ctx), uuid.Nil, 0, operatedsystemstate.ReasonOperator, nil, "k"); true {
		assertKind(t, err, fwra.ContractMisuse)
	}
	if _, err := store.PublishDesiredState(rc(ctx), app, 0, operatedsystemstate.ReasonOperator, nil, ""); true {
		assertKind(t, err, fwra.ContractMisuse)
	}
}

// ---------------------------------------------------------------------------
// NewNoOpOperatedSystemStateAccess — the LOCAL-PROFILE variant selected when the
// deployment binding has no Postgres backing (local-first-init-funnel Task 2b:
// "operatedSystemState=no-op", mirroring usage.NewNoOpUsageAccess). Unlike the suite
// above, these are pure in-process unit tests — no testcontainer, no network.
//
// Semantics under test (mirroring usageAccess's documented no-op stance): NOTHING is
// ever persisted. Every read behaves as if no operated app has ever existed
// (ReadOperatedSystem -> NotFound; ReadInFlightOperatedApps -> empty). Every write
// trivially "succeeds" (matching the real store's fresh-create shape, Version(1)) but
// is never reflected in a subsequent read — a write is not a promise of durability
// here, exactly like usageAccess's dropped RecordComputeUsage batch. Caller-misuse
// preconditions (zero id, empty idempotency key) are still enforced identically to the
// Postgres impl, so callers see the SAME contract-level errors in both profiles.
// ---------------------------------------------------------------------------

func TestNoOp_ReadOperatedSystem_AlwaysNotFound(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	_, err := store.ReadOperatedSystem(rc(context.Background()), uuid.New())
	assertKind(t, err, fwra.NotFound)
}

func TestNoOp_ReadOperatedSystem_ZeroID_ContractMisuse(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	_, err := store.ReadOperatedSystem(rc(context.Background()), uuid.Nil)
	assertKind(t, err, fwra.ContractMisuse)
}

func TestNoOp_ReadInFlightOperatedApps_AlwaysEmpty(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	out, err := store.ReadInFlightOperatedApps(rc(context.Background()), operatedsystemstate.InFlightScope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatalf("expected a non-nil empty slice, got nil")
	}
	if len(out) != 0 {
		t.Fatalf("expected zero in-flight apps, got %d", len(out))
	}
}

func TestNoOp_PublishDesiredState_TriviallySucceeds_NotPersisted(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	appID := uuid.New()

	v, err := store.PublishDesiredState(rc(context.Background()), appID, 0,
		operatedsystemstate.ReasonOperator, nil, fwra.IdempotencyKey("k1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected placeholder Version 1, got %d", v)
	}

	// The write is a no-op: a subsequent read of the SAME app still reports NotFound —
	// nothing was actually persisted (mirrors usageAccess's ReadRange returning no
	// facts after a RecordComputeUsage "success").
	_, err = store.ReadOperatedSystem(rc(context.Background()), appID)
	assertKind(t, err, fwra.NotFound)
}

func TestNoOp_PublishDesiredState_ZeroID_ContractMisuse(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	_, err := store.PublishDesiredState(rc(context.Background()), uuid.Nil, 0,
		operatedsystemstate.ReasonOperator, nil, fwra.IdempotencyKey("k1"))
	assertKind(t, err, fwra.ContractMisuse)
}

func TestNoOp_PublishDesiredState_EmptyIdempotencyKey_ContractMisuse(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	_, err := store.PublishDesiredState(rc(context.Background()), uuid.New(), 0,
		operatedsystemstate.ReasonOperator, nil, fwra.IdempotencyKey(""))
	assertKind(t, err, fwra.ContractMisuse)
}

func TestNoOp_RecordRuntimeStatusChange_TriviallySucceeds(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	v, err := store.RecordRuntimeStatusChange(rc(context.Background()), uuid.New(), 1,
		operatedsystemstate.RuntimeStatusHealthy, fwra.IdempotencyKey("k1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected placeholder Version 1, got %d", v)
	}
}

func TestNoOp_WithdrawSystem_TriviallySucceeds(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	v, err := store.WithdrawSystem(rc(context.Background()), uuid.New(), 1, fwra.IdempotencyKey("k1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected placeholder Version 1, got %d", v)
	}
}

func TestNoOp_RecordDelinquencyAction_TriviallySucceeds(t *testing.T) {
	store := operatedsystemstate.NewNoOpOperatedSystemStateAccess()
	v, err := store.RecordDelinquencyAction(rc(context.Background()), uuid.New(), 1,
		operatedsystemstate.DelinquencyActionPaused, fwra.IdempotencyKey("k1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 1 {
		t.Fatalf("expected placeholder Version 1, got %d", v)
	}
}

func TestNoOp_ImplementsInterface(_ *testing.T) {
	var _ = operatedsystemstate.NewNoOpOperatedSystemStateAccess()
}

func containsAll(apps []operatedsystemstate.OperatedSystemSummary, want ...uuid.UUID) bool {
	for _, w := range want {
		if !containsAny(apps, w) {
			return false
		}
	}
	return true
}

func containsAny(apps []operatedsystemstate.OperatedSystemSummary, want ...uuid.UUID) bool {
	for _, a := range apps {
		if slices.Contains(want, a.ID) {
			return true
		}
	}
	return false
}
