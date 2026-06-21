package usagelog_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	postgresinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres/testinfra"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usagelog"
)

// These integration tests exercise the concrete Postgres Store against a real
// Postgres testcontainer (framework-go-infrastructure-postgres/testinfra),
// skipped under -short exactly like the sibling projectstate suite.
//
// This is the developer-owned regression harness for the append-only
// usage_log ledger — the C-UA Service Test Plan made executable. Every way to
// demonstrate the Store does NOT work has a case here:
//   - the no-double-count invariant (duplicate runtime event id → same
//     EntryRef, ONE row, no public Duplicate error) — C-UA #1,
//   - mixed and in-batch duplicates deduped per-row,
//   - append-only enforced AT THE STORE (trigger rejects UPDATE/DELETE),
//   - append-order replay across both write verbs (one unified log) — C-UA #3,
//   - the frozen UsageRangeQuery scopes (whole period vs one operated app),
//   - the 2026-06-09 repurpose delta (construction-token fact, NULL operated
//     app) round-tripping,
//   - empty period → empty slice (NOT NotFound),
//   - caller misuse (ContractMisuse on every violated pre-condition) — C-UA #2,
//   - idempotent constructor-applied DDL.

// newStore spins a fresh Postgres, applies the schema via NewStore, and hands
// back the Store plus the raw pool (for row-count probes and the trigger test).
func newStore(t *testing.T) (*usagelog.Store, *pgxpool.Pool, context.Context) {
	t.Helper()
	pool := postgresinfra.StartPostgres(t)
	ctx := context.Background()
	store, err := usagelog.NewStore(ctx, pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store, pool, ctx
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

// rowCount counts the rows recorded for one runtime event id.
func rowCount(t *testing.T, pool *pgxpool.Pool, ctx context.Context, id usagelog.RuntimeEventID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM usage_log WHERE runtime_event_id = $1`, string(id)).Scan(&n); err != nil {
		t.Fatalf("rowCount: %v", err)
	}
	return n
}

// event builds a well-formed hosting/compute fact.
func event(customer usagelog.CustomerID, app usagelog.OperatedAppID, cycle usagelog.CycleID, runtimeEventID, unit string, amount float64) usagelog.UsageEvent {
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	return usagelog.UsageEvent{
		CustomerID:     customer,
		OperatedAppID:  app,
		CycleID:        cycle,
		Units:          usagelog.ComputeUnits{Amount: amount, Unit: unit},
		RuntimeEventID: usagelog.RuntimeEventID(runtimeEventID),
		WindowStart:    start,
		WindowEnd:      start.Add(time.Minute),
		OccurredAt:     start.Add(time.Minute),
	}
}

// tokenEvent builds a construction-token fact — the 2026-06-09 repurpose
// shape: build-phase consumption with NO operated app (zero OperatedAppID).
func tokenEvent(customer usagelog.CustomerID, cycle usagelog.CycleID, runtimeEventID string, amount float64) usagelog.UsageEvent {
	return event(customer, uuid.Nil, cycle, runtimeEventID, "construction-token", amount)
}

// TestRecordThenReadRange_AppendOrder: facts recorded by BOTH write verbs land
// in one unified log and replay in append order with seam-set Ref/RecordedAt.
// Covers C-UA #3's "a recordFinalUsage batch appended later still appears in a
// re-read range".
func TestRecordThenReadRange_AppendOrder(t *testing.T) {
	store, _, ctx := newStore(t)
	customer := uuid.New()
	app := uuid.New()
	cycle := usagelog.CycleID("2026-06")

	first := []usagelog.UsageEvent{
		tokenEvent(customer, cycle, "ev-token-1", 1200),
		event(customer, app, cycle, "ev-compute-1", "compute-unit-second", 37.5),
	}
	refs1, err := store.RecordComputeUsage(ctx, first)
	if err != nil {
		t.Fatalf("RecordComputeUsage: %v", err)
	}
	if len(refs1) != 2 || refs1[0] == "" || refs1[1] == "" {
		t.Fatalf("expected 2 non-empty refs, got %v", refs1)
	}

	refs2, err := store.RecordFinalUsage(ctx, []usagelog.UsageEvent{
		event(customer, app, cycle, "ev-final-1", "egress-byte", 4096),
	})
	if err != nil {
		t.Fatalf("RecordFinalUsage: %v", err)
	}
	if len(refs2) != 1 || refs2[0] == "" {
		t.Fatalf("expected 1 non-empty ref, got %v", refs2)
	}

	got, err := store.ReadRange(ctx, usagelog.UsageRangeQuery{CustomerID: customer, CycleID: cycle})
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 facts, got %d", len(got))
	}
	wantOrder := []usagelog.RuntimeEventID{"ev-token-1", "ev-compute-1", "ev-final-1"}
	for i, want := range wantOrder {
		if got[i].RuntimeEventID != want {
			t.Fatalf("append order broken at %d: want %s, got %s", i, want, got[i].RuntimeEventID)
		}
		if got[i].Ref == "" || got[i].RecordedAt.IsZero() {
			t.Fatalf("fact %d missing seam-set Ref/RecordedAt: %+v", i, got[i])
		}
	}
	// Write refs correlate with replayed refs.
	if got[0].Ref != refs1[0] || got[1].Ref != refs1[1] || got[2].Ref != refs2[0] {
		t.Fatalf("refs do not correlate: write %v/%v vs read %v", refs1, refs2, []usagelog.EntryRef{got[0].Ref, got[1].Ref, got[2].Ref})
	}
	// The token fact round-trips with an ABSENT operated app (zero uuid).
	if got[0].OperatedAppID != uuid.Nil {
		t.Fatalf("token fact should have zero OperatedAppID, got %s", got[0].OperatedAppID)
	}
	if got[0].Units != (usagelog.ComputeUnits{Amount: 1200, Unit: "construction-token"}) {
		t.Fatalf("token units did not round-trip: %+v", got[0].Units)
	}
	if got[1].OperatedAppID != app {
		t.Fatalf("hosting fact lost its operated app: %s", got[1].OperatedAppID)
	}
}

// TestDuplicateReplay_SameRefNoSecondRow: the no-double-count invariant
// (C-UA #1). A replayed runtime event id — even through the OTHER write verb,
// since both append to the one unified log — returns the PRIOR EntryRef as
// idempotent success and appends no second row.
func TestDuplicateReplay_SameRefNoSecondRow(t *testing.T) {
	store, pool, ctx := newStore(t)
	customer := uuid.New()
	app := uuid.New()
	cycle := usagelog.CycleID("2026-06")
	ev := event(customer, app, cycle, "ev-dup", "compute-unit-second", 10)

	refs1, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{ev})
	if err != nil {
		t.Fatalf("first record: %v", err)
	}

	// Replay via the same verb.
	refs2, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{ev})
	if err != nil {
		t.Fatalf("replay must be idempotent success, got: %v", err)
	}
	if refs2[0] != refs1[0] {
		t.Fatalf("replay must return the prior ref: %s != %s", refs2[0], refs1[0])
	}

	// Replay via the other verb (one unified log, same UNIQUE constraint).
	refs3, err := store.RecordFinalUsage(ctx, []usagelog.UsageEvent{ev})
	if err != nil {
		t.Fatalf("cross-verb replay must be idempotent success, got: %v", err)
	}
	if refs3[0] != refs1[0] {
		t.Fatalf("cross-verb replay must return the prior ref: %s != %s", refs3[0], refs1[0])
	}

	if n := rowCount(t, pool, ctx, "ev-dup"); n != 1 {
		t.Fatalf("no-double-count violated: %d rows for ev-dup", n)
	}
}

// TestMixedBatch_PerEventDedup: a batch mixing new and already-recorded events
// succeeds per-row (contract §2.1 batch semantics): the duplicate position
// carries the prior ref, the new position a fresh ref.
func TestMixedBatch_PerEventDedup(t *testing.T) {
	store, pool, ctx := newStore(t)
	customer := uuid.New()
	app := uuid.New()
	cycle := usagelog.CycleID("2026-06")

	dup := event(customer, app, cycle, "ev-mixed-dup", "compute-unit-second", 1)
	refs1, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{dup})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	fresh := event(customer, app, cycle, "ev-mixed-new", "storage-byte-month", 2)
	refs2, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{dup, fresh})
	if err != nil {
		t.Fatalf("mixed batch must succeed, got: %v", err)
	}
	if refs2[0] != refs1[0] {
		t.Fatalf("duplicate position must carry the prior ref: %s != %s", refs2[0], refs1[0])
	}
	if refs2[1] == "" || refs2[1] == refs1[0] {
		t.Fatalf("new position must carry a fresh ref, got %s", refs2[1])
	}
	if n := rowCount(t, pool, ctx, "ev-mixed-dup"); n != 1 {
		t.Fatalf("duplicate double-counted: %d rows", n)
	}
	if n := rowCount(t, pool, ctx, "ev-mixed-new"); n != 1 {
		t.Fatalf("fresh event not recorded exactly once: %d rows", n)
	}
}

// TestInBatchDuplicate: the same runtime event id twice WITHIN one batch
// collapses to one row, both positions returning the same ref.
func TestInBatchDuplicate(t *testing.T) {
	store, pool, ctx := newStore(t)
	customer := uuid.New()
	cycle := usagelog.CycleID("2026-06")
	ev := tokenEvent(customer, cycle, "ev-inbatch", 5)

	refs, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{ev, ev})
	if err != nil {
		t.Fatalf("in-batch duplicate must succeed, got: %v", err)
	}
	if refs[0] == "" || refs[0] != refs[1] {
		t.Fatalf("both positions must carry the same ref, got %v", refs)
	}
	if n := rowCount(t, pool, ctx, "ev-inbatch"); n != 1 {
		t.Fatalf("in-batch duplicate double-counted: %d rows", n)
	}
}

// TestAppendOnly_TriggerRejectsMutation: the store itself (not just RA
// discipline) rejects UPDATE and DELETE — financial-class immutability.
func TestAppendOnly_TriggerRejectsMutation(t *testing.T) {
	store, pool, ctx := newStore(t)
	customer := uuid.New()
	cycle := usagelog.CycleID("2026-06")
	if _, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{tokenEvent(customer, cycle, "ev-immutable", 9)}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE usage_log SET units_amount = 0 WHERE runtime_event_id = 'ev-immutable'`); err == nil {
		t.Fatal("UPDATE must be rejected by the append-only trigger")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM usage_log WHERE runtime_event_id = 'ev-immutable'`); err == nil {
		t.Fatal("DELETE must be rejected by the append-only trigger")
	}
	if n := rowCount(t, pool, ctx, "ev-immutable"); n != 1 {
		t.Fatalf("fact lost: %d rows", n)
	}
}

// TestReadRange_OperatedAppScope: the frozen Q5 query shapes. The app-scoped
// read returns only that app's facts; the whole-period read returns every
// fact including construction-token rows (NULL operated app); a different
// app's scope excludes them all.
func TestReadRange_OperatedAppScope(t *testing.T) {
	store, _, ctx := newStore(t)
	customer := uuid.New()
	appA := uuid.New()
	appB := uuid.New()
	cycle := usagelog.CycleID("2026-06")

	if _, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{
		event(customer, appA, cycle, "ev-a-1", "compute-unit-second", 1),
		event(customer, appB, cycle, "ev-b-1", "compute-unit-second", 2),
		tokenEvent(customer, cycle, "ev-tok-1", 3),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	whole, err := store.ReadRange(ctx, usagelog.UsageRangeQuery{CustomerID: customer, CycleID: cycle})
	if err != nil {
		t.Fatalf("whole-period read: %v", err)
	}
	if len(whole) != 3 {
		t.Fatalf("whole period must return 3 facts, got %d", len(whole))
	}

	scopedA, err := store.ReadRange(ctx, usagelog.UsageRangeQuery{CustomerID: customer, CycleID: cycle, OperatedAppID: &appA})
	if err != nil {
		t.Fatalf("app-scoped read: %v", err)
	}
	if len(scopedA) != 1 || scopedA[0].RuntimeEventID != "ev-a-1" {
		t.Fatalf("app A scope wrong: %+v", scopedA)
	}

	// Token rows have no operated app and never match an app predicate.
	other := uuid.New()
	scopedNone, err := store.ReadRange(ctx, usagelog.UsageRangeQuery{CustomerID: customer, CycleID: cycle, OperatedAppID: &other})
	if err != nil {
		t.Fatalf("unmatched app scope: %v", err)
	}
	if len(scopedNone) != 0 {
		t.Fatalf("unmatched app scope must be empty, got %+v", scopedNone)
	}
}

// TestReadRange_EmptyPeriod: an empty period replays as an empty (non-nil)
// slice — NOT fwra.NotFound (contract §2.3).
func TestReadRange_EmptyPeriod(t *testing.T) {
	store, _, ctx := newStore(t)
	got, err := store.ReadRange(ctx, usagelog.UsageRangeQuery{CustomerID: uuid.New(), CycleID: "2026-06"})
	if err != nil {
		t.Fatalf("empty period must not error, got: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil slice, got %#v", got)
	}
}

// TestEmptyBatch_NoOpSuccess: an empty observation batch records nothing and
// succeeds with an empty ref slice.
func TestEmptyBatch_NoOpSuccess(t *testing.T) {
	store, pool, ctx := newStore(t)
	refs, err := store.RecordComputeUsage(ctx, nil)
	if err != nil {
		t.Fatalf("empty batch must succeed, got: %v", err)
	}
	if refs == nil || len(refs) != 0 {
		t.Fatalf("expected empty non-nil refs, got %#v", refs)
	}
	var n int
	if qErr := pool.QueryRow(ctx, `SELECT count(*) FROM usage_log`).Scan(&n); qErr != nil {
		t.Fatalf("count: %v", qErr)
	}
	if n != 0 {
		t.Fatalf("empty batch must record nothing, got %d rows", n)
	}
}

// TestContractMisuse covers every violated write/read pre-condition
// (contract §2.1/§2.3 + the repurpose delta; C-UA #2's negative-units case).
func TestContractMisuse(t *testing.T) {
	store, pool, ctx := newStore(t)
	customer := uuid.New()
	app := uuid.New()
	cycle := usagelog.CycleID("2026-06")

	bad := []struct {
		name string
		ev   usagelog.UsageEvent
	}{
		{"empty RuntimeEventID", event(customer, app, cycle, "", "compute-unit-second", 1)},
		{"zero CustomerID", event(uuid.Nil, app, cycle, "ev-x1", "compute-unit-second", 1)},
		{"empty CycleID", event(customer, app, "", "ev-x2", "compute-unit-second", 1)},
		{"negative amount", event(customer, app, cycle, "ev-x3", "compute-unit-second", -1)},
		{"NaN amount", event(customer, app, cycle, "ev-x4", "compute-unit-second", math.NaN())},
		{"empty unit", event(customer, app, cycle, "ev-x5", "", 1)},
	}
	for _, tc := range bad {
		_, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{tc.ev})
		assertKind(t, err, fwra.ContractMisuse)
		_, err = store.RecordFinalUsage(ctx, []usagelog.UsageEvent{tc.ev})
		assertKind(t, err, fwra.ContractMisuse)
	}

	// Inverted window.
	inv := event(customer, app, cycle, "ev-x6", "compute-unit-second", 1)
	inv.WindowEnd = inv.WindowStart.Add(-time.Second)
	_, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{inv})
	assertKind(t, err, fwra.ContractMisuse)

	// A bad event ANYWHERE in the batch rejects the whole batch before any append.
	good := event(customer, app, cycle, "ev-good", "compute-unit-second", 1)
	_, err = store.RecordComputeUsage(ctx, []usagelog.UsageEvent{good, bad[0].ev})
	assertKind(t, err, fwra.ContractMisuse)
	if n := rowCount(t, pool, ctx, "ev-good"); n != 0 {
		t.Fatalf("rejected batch must append nothing, got %d rows for ev-good", n)
	}

	// Read pre-conditions.
	_, err = store.ReadRange(ctx, usagelog.UsageRangeQuery{CustomerID: uuid.Nil, CycleID: cycle})
	assertKind(t, err, fwra.ContractMisuse)
	_, err = store.ReadRange(ctx, usagelog.UsageRangeQuery{CustomerID: customer, CycleID: ""})
	assertKind(t, err, fwra.ContractMisuse)
	zero := uuid.Nil
	_, err = store.ReadRange(ctx, usagelog.UsageRangeQuery{CustomerID: customer, CycleID: cycle, OperatedAppID: &zero})
	assertKind(t, err, fwra.ContractMisuse)

	// Constructor misuse.
	_, err = usagelog.NewStore(ctx, nil)
	assertKind(t, err, fwra.ContractMisuse)
}

// TestSchemaIdempotent: the constructor-applied DDL is safe on every boot —
// a second NewStore over the same pool succeeds and existing facts survive.
func TestSchemaIdempotent(t *testing.T) {
	store, pool, ctx := newStore(t)
	customer := uuid.New()
	if _, err := store.RecordComputeUsage(ctx, []usagelog.UsageEvent{tokenEvent(customer, "2026-06", "ev-boot", 1)}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := usagelog.NewStore(ctx, pool); err != nil {
		t.Fatalf("second NewStore (redeploy) must succeed: %v", err)
	}
	if n := rowCount(t, pool, ctx, "ev-boot"); n != 1 {
		t.Fatalf("redeploy must not disturb the ledger: %d rows", n)
	}
}
