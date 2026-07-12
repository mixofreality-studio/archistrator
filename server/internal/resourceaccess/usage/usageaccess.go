package usage

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	fwpg "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// postgresUsageAccess is the concrete, Postgres-backed implementation of
// UsageAccess (usageAccess.md §6 infrastructure mapping; schema authored by
// R-PG-US). It is UNEXPORTED — the package's only public surface is the generated
// UsageAccess interface + models + the generated NewPostgresUsageAccess
// constructor (option-1 generated-DI). The ledger lives in the append-only
// usage_log table: INSERT-only rows deduped by UNIQUE(runtime_event_id), with a
// store-level trigger rejecting every UPDATE/DELETE. The struct imports NO
// Temporal (layer rule): the runtime event id arrives as an ordinary field on
// each event and is never read from ambient context.
type postgresUsageAccess struct {
	pool *pgxpool.Pool
}

// Compile-time proof the concrete impl satisfies the port. If the port ever
// drifts, this line breaks the build — exactly the guard The Method wants
// between a contract and its construction.
var _ UsageAccess = (*postgresUsageAccess)(nil)

// schemaDDL is the deterministic, idempotent migration for the append-only
// ledger, authored by R-PG-US (schema.sql in this package IS the migration).
// Applying it in the constructor keeps schema setup co-located with the only
// component allowed to touch the usage_log Resource and makes the Store
// self-sufficient for both production wiring and the integration tests —
// the exact projectstate.NewStore convention.
//
//go:embed schema.sql
var schemaDDL string

// newPostgresUsageAccess is the hand-written, unexported builder behind the
// generated NewPostgresUsageAccess constructor (option-1 delegated DI). It builds
// the impl over an existing pgx pool and applies the embedded, idempotent schema
// (DDL) — the stateful setup the builder owns — returning the UsageAccess
// interface so the concrete struct stays unexported. Safe to run on every
// boot/redeploy.
func newPostgresUsageAccess(ctx context.Context, pool *pgxpool.Pool) (UsageAccess, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, "usage.NewPostgresUsageAccess: nil pool")
	}
	if _, err := pool.Exec(ctx, schemaDDL); err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "usage.NewPostgresUsageAccess: apply schema")
	}
	return &postgresUsageAccess{pool: pool}, nil
}

// RecordComputeUsage appends a batch of observed usage facts (contract §2.1).
// The cross-cutting ctx now rides the ResourceAccess call Context (fwra.Context
// embeds context.Context); this port carries NO fwra.IdempotencyKey — dedup is
// the domain RuntimeEventID field on each event (DB UNIQUE constraint), so the
// component stays Temporal-free and the behaviour is byte-identical.
func (s *postgresUsageAccess) RecordComputeUsage(rc fwra.Context, events []UsageEvent) ([]EntryRef, error) {
	return s.appendBatch(rc.Context, "usage.RecordComputeUsage", events)
}

// RecordFinalUsage appends the final usage batch captured at withdraw
// (contract §2.2). Same table, same transaction shape, same idempotency as
// RecordComputeUsage — the "final" distinction is the business moment, not a
// column this seam exposes (contract §6).
func (s *postgresUsageAccess) RecordFinalUsage(rc fwra.Context, events []UsageEvent) ([]EntryRef, error) {
	return s.appendBatch(rc.Context, "usage.RecordFinalUsage", events)
}

// insertSQL appends one immutable fact. ON CONFLICT (runtime_event_id)
// DO NOTHING — NOT DO UPDATE, which the append-only trigger would (rightly)
// reject. A conflicting row returns no RETURNING row; appendBatch then
// resolves the prior entry's ref in a second pass (idempotent success).
const insertSQL = `
INSERT INTO usage_log
    (customer_id, operated_app_id, cycle_id, units_amount, units_unit,
     runtime_event_id, raw_meter, window_start, window_end, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (runtime_event_id) DO NOTHING
RETURNING entry_id`

const selectExistingSQL = `SELECT entry_id FROM usage_log WHERE runtime_event_id = $1`

// appendBatch is the one shared write path (contract §6): ONE transaction,
// batched INSERTs (pgx batch — the documented bulk-op justification), per-row
// dedup on UNIQUE(runtime_event_id). For each duplicate the PRIOR entry's ref
// is selected and returned in that row's position — idempotent no-op success,
// never a public Duplicate error. Refs are returned in input order.
func (s *postgresUsageAccess) appendBatch(ctx context.Context, op string, events []UsageEvent) ([]EntryRef, error) {
	for i := range events {
		if err := validateEvent(op, i, &events[i]); err != nil {
			return nil, err
		}
	}
	refs := make([]EntryRef, len(events))
	if len(events) == 0 {
		// An empty observation batch (e.g. a tick with nothing in flight) is a
		// no-op success — there is no fact to record and nothing to dedup.
		return refs, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fwpg.MapError(err, op)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit; error intentionally ignored

	// PASS 1 — batched appends. A row that came back carries the new entry_id;
	// a conflict (duplicate runtime event id) yields no row and is resolved in
	// pass 2. An in-batch duplicate behaves identically: the first occurrence
	// inserts, the second conflicts against it inside the same transaction.
	ins := &pgx.Batch{}
	for i := range events {
		ev := &events[i]
		var appID any // NULL for construction-token facts (zero OperatedAppID)
		if ev.OperatedAppID != uuid.Nil {
			appID = ev.OperatedAppID
		}
		ins.Queue(insertSQL,
			ev.CustomerID, appID, string(ev.CycleID),
			ev.Units.Amount, ev.Units.Unit,
			string(ev.RuntimeEventID), ev.RawMeter,
			ev.WindowStart, ev.WindowEnd, ev.OccurredAt,
		)
	}
	duplicates, err := func() ([]int, error) {
		br := tx.SendBatch(ctx, ins)
		defer func() { _ = br.Close() }()
		var dups []int
		for i := range events {
			var id int64
			scanErr := br.QueryRow().Scan(&id)
			switch {
			case scanErr == nil:
				refs[i] = entryRef(id)
			case errors.Is(scanErr, pgx.ErrNoRows):
				dups = append(dups, i)
			default:
				return nil, fwpg.MapError(scanErr, fmt.Sprintf("%s: append event %d", op, i))
			}
		}
		return dups, nil
	}()
	if err != nil {
		return nil, err
	}

	// PASS 2 — resolve each duplicate to the already-recorded entry's ref.
	if len(duplicates) > 0 {
		sel := &pgx.Batch{}
		for _, i := range duplicates {
			sel.Queue(selectExistingSQL, string(events[i].RuntimeEventID))
		}
		err = func() error {
			br := tx.SendBatch(ctx, sel)
			defer func() { _ = br.Close() }()
			for _, i := range duplicates {
				var id int64
				if scanErr := br.QueryRow().Scan(&id); scanErr != nil {
					// The UNIQUE conflict proved the row exists and committed
					// (DO NOTHING waits out an in-flight writer), so absence here
					// is a store fault, not a caller condition.
					return fwpg.MapError(scanErr, fmt.Sprintf(
						"%s: resolve prior entry for duplicate runtime event id %q", op, events[i].RuntimeEventID))
				}
				refs[i] = entryRef(id)
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fwpg.MapError(err, op+": commit")
	}
	return refs, nil
}

// ReadRange replays the immutable usage facts in scope, in append order
// (contract §2.3): whole period when query.OperatedAppID is nil, one operated
// app's facts when set. Pure read; an empty period returns an empty
// (non-nil) slice, never NotFound.
func (s *postgresUsageAccess) ReadRange(rc fwra.Context, query UsageRangeQuery) ([]UsageEvent, error) {
	ctx := rc.Context
	const op = "usage.ReadRange"
	if query.CustomerID == uuid.Nil {
		return nil, fwra.New(fwra.ContractMisuse, op+": zero CustomerID")
	}
	if query.CycleID == "" {
		return nil, fwra.New(fwra.ContractMisuse, op+": empty CycleID")
	}
	if query.OperatedAppID != nil && *query.OperatedAppID == uuid.Nil {
		return nil, fwra.New(fwra.ContractMisuse, op+": OperatedAppID set but zero (use nil for the whole period)")
	}

	q := `
SELECT entry_id, customer_id, operated_app_id, cycle_id, units_amount, units_unit,
       runtime_event_id, raw_meter, window_start, window_end, occurred_at, recorded_at
FROM usage_log
WHERE customer_id = $1 AND cycle_id = $2`
	args := []any{query.CustomerID, string(query.CycleID)}
	if query.OperatedAppID != nil {
		q += ` AND operated_app_id = $3`
		args = append(args, *query.OperatedAppID)
	}
	q += `
ORDER BY recorded_at, entry_id`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fwpg.MapError(err, op)
	}
	defer rows.Close()

	out := []UsageEvent{}
	for rows.Next() {
		var (
			id             int64
			customerID     uuid.UUID
			appID          *uuid.UUID
			cycleID        string
			amount         float64
			unit           string
			runtimeEventID string
			rawMeter       []byte
			ev             UsageEvent
		)
		if scanErr := rows.Scan(&id, &customerID, &appID, &cycleID, &amount, &unit,
			&runtimeEventID, &rawMeter, &ev.WindowStart, &ev.WindowEnd, &ev.OccurredAt, &ev.RecordedAt); scanErr != nil {
			return nil, fwpg.MapError(scanErr, op+": scan row")
		}
		ev.CustomerID = customerID
		if appID != nil {
			ev.OperatedAppID = *appID
		}
		ev.CycleID = CycleID(cycleID)
		ev.Units = ComputeUnits{Amount: amount, Unit: unit}
		ev.RuntimeEventID = RuntimeEventID(runtimeEventID)
		ev.RawMeter = rawMeter
		ev.Ref = entryRef(id)
		out = append(out, ev)
	}
	if rErr := rows.Err(); rErr != nil {
		return nil, fwpg.MapError(rErr, op+": iterate rows")
	}
	return out, nil
}

// validateEvent enforces the write pre-conditions (contract §2.1/§2.2, with
// the 2026-06-09 repurpose delta): RuntimeEventID non-empty, CustomerID
// non-zero, CycleID non-empty, Units well-formed (finite, non-negative,
// named unit), a non-inverted window. OperatedAppID is deliberately NOT
// required — construction-token facts have no operated app (zero → NULL);
// the frozen pre-repurpose "non-zero OperatedAppID" precondition is the
// recorded additive delta, not silently re-imposed here.
func validateEvent(op string, i int, ev *UsageEvent) error {
	misuse := func(msg string) error {
		return fwra.New(fwra.ContractMisuse, fmt.Sprintf("%s: event %d: %s", op, i, msg))
	}
	if ev.RuntimeEventID == "" {
		return misuse("empty RuntimeEventID")
	}
	if ev.CustomerID == uuid.Nil {
		return misuse("zero CustomerID")
	}
	if ev.CycleID == "" {
		return misuse("empty CycleID")
	}
	if math.IsNaN(ev.Units.Amount) || math.IsInf(ev.Units.Amount, 0) {
		return misuse("malformed Units.Amount (not finite)")
	}
	if ev.Units.Amount < 0 {
		return misuse("negative Units.Amount")
	}
	if ev.Units.Unit == "" {
		return misuse("empty Units.Unit")
	}
	if ev.WindowEnd.Before(ev.WindowStart) {
		return misuse("inverted window (WindowEnd before WindowStart)")
	}
	return nil
}

// entryRef renders the ledger's append position as the opaque EntryRef token.
func entryRef(id int64) EntryRef {
	return EntryRef(strconv.FormatInt(id, 10))
}

// ---- from usage.go ----
// Package usage is the usageAccess component of the archistrator server's
// ResourceAccess layer — the Temporal-free port over the Usage Log, the
// APPEND-ONLY ledger of metered usage facts (contracts/usageAccess.md, FROZEN
// 2026-05-30; schema provisioned by R-PG-US 2026-06-10).
//
// This is the LEDGER discipline, not the head-state discipline
// (operational-concepts.md §13): rows are immutable metered facts — INSERTed
// once, never UPDATEd, never DELETEd (trigger-enforced at the store). There is
// no `version` token, no `applied_mutation` dedup ledger, no fwra.Conflict and
// no fwra.IdempotencyKey on this surface. Idempotency is the caller-supplied,
// globally-unique RUNTIME EVENT ID — an ordinary domain parameter — enforced
// by the DB UNIQUE constraint: a replayed batch collapses per-row to the
// already-recorded entry and returns its EntryRef as success (there is
// deliberately NO public duplicate error).
//
// 2026-06-09 repurpose (semantic, no new verb): the one unified log meters
// BOTH construction-token consumption (build phase — units_unit
// 'construction-token', no operated app) AND hosting/compute consumption
// (operation phase — 'compute-unit-second' / 'storage-byte-month' /
// 'egress-byte', attributed to an operated app) for the user's service bill.
// billingManager reads the range at billing-period close (UC5) and
// billingEngine.PriceUsage folds it; the CycleID VALUE is the billing
// PeriodID. The dimension vocabulary is an OPEN SET (the metering
// volatility) — this component never interprets Unit.
//
// Per The Method's layer model ([[the-method-layers]]): imports NO Temporal;
// no RA→RA, no RA→Engine; atomic business verbs, not CRUD; the total is
// DERIVED by the pricing Engine at read time, never aggregated or stored here.

// CustomerID is the billing counterparty the usage facts are scoped to
// (canonical billing aggregate key, shared with the billing stores).
// PROVISIONAL per the frozen contract §9 Q3 (escalated to D-MST); an id-type
// realignment there is an additive swap here.
type CustomerID = uuid.UUID

// CycleID is the billing period a usage fact belongs to. Post-repurpose the
// VALUE is the billing PeriodID ("PeriodID replaces the settlement-era
// CycleID"); the name keeps the frozen contract §3.1 spelling.

// OperatedAppID is the operated app a hosting/compute fact is attributed to.
// 2026-06-09 repurpose delta (recorded, not silently absorbed): construction-
// token facts are build-phase and have NO operated app — the zero uuid here
// means "absent" and persists as NULL. Hosting facts carry a non-zero id.
type OperatedAppID = uuid.UUID

// RuntimeEventID is the caller-supplied, globally-unique event identifier
// (runtime / metrics-pipeline / worker supplied) — the natural dedup token for
// an append-only ledger. A domain value, NOT a Temporal key and NOT an
// fwra.IdempotencyKey; the DB UNIQUE constraint on it collapses a replayed
// append to an idempotent success.

// ComputeUnits is one infrastructure-neutral metered quantity — never a
// priced/monetary amount (pricing is the billing Engine's Strategy) and never
// a raw cloud billing lexeme. Unit is the open-set dimension discriminator
// (e.g. "construction-token", "compute-unit-second", "storage-byte-month",
// "egress-byte"); this component stores it opaquely.

// non-negative metered quantity
// infrastructure-neutral unit name (open set)

// EntryRef is an opaque reference to one recorded ledger entry (the ledger's
// own append position). Returned by the write verbs so a caller can correlate
// an append — including the duplicate-replay case, which returns the PRIOR
// entry's ref. It is never a read key (there is no readEntry — contract §2.5).

// UsageRangeQuery is the ReadRange input (frozen Q5 shape). One read scope
// value serves both caller edges: a whole billing period (OperatedAppID nil —
// the period-close fold) and one operated app's facts (OperatedAppID set —
// the cost-projection read).

// OperatedAppID is the OPTIONAL read scope: nil = whole period (the
// period-close fold), set = one operated app's facts (the cost-projection
// read). The `,omitempty` tag is load-bearing for schema-first codegen: it
// captures this field as optional so the generated contract.gen.go preserves
// the POINTER (nil-distinguishable) shape rather than a plain value.

// UsageEvent is one immutable metered usage fact — the element of the write
// batches AND the element type ReadRange replays, in append order. There is
// ONE unified log: tick-recorded, final-recorded, and construction-token facts
// share this shape and the same table (no kind discriminator — contract §3.2).
//
// Ref and RecordedAt are SET BY THIS SEAM: they are outputs of the append
// (and populated on replay); any caller-supplied value is ignored on write.

// zero = absent (construction-token fact) → NULL

// metered, non-negative; never a priced amount
// the globally-unique dedup token (UNIQUE constraint)
// OPTIONAL opaque source-meter payload, audit only; nil if absent
// start of the observed window the fact covers
// end of the observed window (>= WindowStart)
// when the source recorded the observation (caller-supplied)
// when this ledger appended it (set by the seam)
// the entry's own append position (set by the seam)

// UsageAccess is the Temporal-free port over the Usage Log (contract §2).
// Three atomic operations: two append-writes and one range-read.
//
// Write idempotency: each event is deduped INDEPENDENTLY on its own
// RuntimeEventID via the DB UNIQUE constraint. A duplicate id is an idempotent
// no-op SUCCESS returning the prior entry's EntryRef — no second row, no
// double-count, no public Duplicate/Conflict error. A mixed batch (some new,
// some already recorded) succeeds, returning each event's ref in input order.
//
// Error kinds on this port: fwra.Transient / fwra.Infrastructure (retryable)
// and fwra.ContractMisuse (terminal — violated pre-condition). NotFound is
// NOT used: an empty period replays as an empty slice. There is NO Conflict —
// append-only means nothing contends.

// RecordComputeUsage appends a batch of observed usage facts (the periodic
// reconcile-tick record; post-repurpose also the construction-token append).
// Returns the entries' refs in input order; duplicates collapse per-row to
// the prior ref. An empty batch is a no-op success returning an empty slice.

// RecordFinalUsage appends the final usage batch captured at withdraw — the
// same fact shape into the same unified log; the distinction is the business
// moment, not a stored kind (contract §2.2/§2.4). Same idempotency contract.

// ReadRange replays the immutable usage facts in scope (whole period, or one
// operated app's facts when query.OperatedAppID is set) in append order. A
// pure, side-effect-free read: no aggregation, no stored total. An empty
// period returns an empty (non-nil) slice, not NotFound.

// Error is the shared ResourceAccess error model (framework-go), re-exported
// as an alias so this component's contract reads in its own terms while every
// RA component shares one fixed enum. Construct with fwra.New / fwra.Wrap.
type Error = fwra.Error
