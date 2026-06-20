package usagelog

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

	fwpg "github.com/davidmarne/archistrator-platform/framework-go-infrastructure-postgres"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// Store is the concrete, Postgres-backed implementation of UsageAccess
// (usageAccess.md §6 infrastructure mapping; schema authored by R-PG-US).
// The ledger lives in the append-only usage_log table: INSERT-only rows
// deduped by UNIQUE(runtime_event_id), with a store-level trigger rejecting
// every UPDATE/DELETE. The struct imports NO Temporal (layer rule): the
// runtime event id arrives as an ordinary field on each event and is never
// read from ambient context.
type Store struct {
	pool *pgxpool.Pool
}

// Compile-time proof the concrete Store satisfies the port. If the port ever
// drifts, this line breaks the build — exactly the guard The Method wants
// between a contract and its construction.
var _ UsageAccess = (*Store)(nil)

// schemaDDL is the deterministic, idempotent migration for the append-only
// ledger, authored by R-PG-US (schema.sql in this package IS the migration).
// Applying it in the constructor keeps schema setup co-located with the only
// component allowed to touch the usage_log Resource and makes the Store
// self-sufficient for both production wiring and the integration tests —
// the exact projectstate.NewStore convention.
//
//go:embed schema.sql
var schemaDDL string

// NewStore builds a Store over an existing pgx pool and applies the embedded,
// idempotent schema (DDL). Safe to run on every boot/redeploy.
func NewStore(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, "usagelog.NewStore: nil pool")
	}
	if _, err := pool.Exec(ctx, schemaDDL); err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "usagelog.NewStore: apply schema")
	}
	return &Store{pool: pool}, nil
}

// RecordComputeUsage appends a batch of observed usage facts (contract §2.1).
func (s *Store) RecordComputeUsage(ctx context.Context, events []UsageEvent) ([]EntryRef, error) {
	return s.appendBatch(ctx, "usagelog.RecordComputeUsage", events)
}

// RecordFinalUsage appends the final usage batch captured at withdraw
// (contract §2.2). Same table, same transaction shape, same idempotency as
// RecordComputeUsage — the "final" distinction is the business moment, not a
// column this seam exposes (contract §6).
func (s *Store) RecordFinalUsage(ctx context.Context, events []UsageEvent) ([]EntryRef, error) {
	return s.appendBatch(ctx, "usagelog.RecordFinalUsage", events)
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
func (s *Store) appendBatch(ctx context.Context, op string, events []UsageEvent) ([]EntryRef, error) {
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
	defer tx.Rollback(ctx)

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
		defer br.Close()
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
			defer br.Close()
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
func (s *Store) ReadRange(ctx context.Context, query UsageRangeQuery) ([]UsageEvent, error) {
	const op = "usagelog.ReadRange"
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
