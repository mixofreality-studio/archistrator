// Package billingstate is the billingStateAccess component of the ResourceAccess
// layer: the Postgres-backed billing head-state aggregate, one per customer — its
// registration, its gateway binding and the settlement of each billing cycle.
//
// Both deployment profiles bind the same Postgres variant (deterministic component
// testing, spec §12 ruling B5: the local profile uses the same Postgres).
//
// It used to carry a second contract facet, revenueLedgerAccess — an append-only ledger
// of inbound end-user revenue — as a permanent no-op: under the charge-only model
// (billingManager.md §3.0, R-013) the platform never invoices for, or shares in,
// end-user revenue. dct Plan 4 Task 3 deleted the facet (spec §11 ruling A5: deleted,
// not deprecated). A future real revenue ledger is a new contract designed against a
// use case, not that stub revived.
package billingstate

import (
	"context"
	_ "embed"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	fwpg "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-postgres"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// postgresBillingStateAccess is the Postgres-backed BillingStateAccess. It is
// UNEXPORTED — the package's public surface is the generated BillingStateAccess
// interface, its models and the generated NewPostgresBillingStateAccess constructor.
// The head state lives in the mutable billing_customer row carrying an
// optimistic-concurrency `version`; each settled cycle is a billing_cycle row;
// idempotency is the billing_mutation ledger. It imports no Temporal (layer rule): the
// idempotency key arrives as an ordinary parameter on each write.
type postgresBillingStateAccess struct {
	pool *pgxpool.Pool
}

var _ BillingStateAccess = (*postgresBillingStateAccess)(nil)

// schemaDDL is the idempotent head-state migration (schema.sql in this package IS the
// migration), applied by the constructor on every boot.
//
//go:embed schema.sql
var schemaDDL string

// newPostgresBillingStateAccess is the hand-written builder behind the generated
// NewPostgresBillingStateAccess: it applies the embedded, idempotent schema and returns
// the store over the pool.
func newPostgresBillingStateAccess(ctx context.Context, pool *pgxpool.Pool) (BillingStateAccess, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, "billingstate.NewPostgresBillingStateAccess: nil pool")
	}
	if _, err := pool.Exec(ctx, schemaDDL); err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "billingstate.NewPostgresBillingStateAccess: apply schema")
	}
	return &postgresBillingStateAccess{pool: pool}, nil
}

// delinquentPauses is the delinquency enforcement every persistently delinquent
// customer is reported with: PAUSE, the reversible action. The contract names it a
// customer billing term, but no op writes a withdraw term yet, and a withdraw removes
// the runtime of every operated app of the customer irreversibly — so the store never
// reports one it was not told to.
const delinquentPauses = true

// ---------------------------------------------------------------------------
// Reads (pure).
// ---------------------------------------------------------------------------

const selectCustomerSQL = `
SELECT version, gateway_bound,
       terms_compute_cost_kind, terms_compute_markup_pct, terms_schedule_kind, terms_currency
FROM billing_customer
WHERE customer_id = $1`

// ReadBilling returns the customer's billing aggregate, with the terms it registered
// under. A customer that was never registered is not an error: it reads as
// Registered=false at Version 0 — the version RegisterCustomer expects to open it from —
// with the zero (unknown) terms.
func (s *postgresBillingStateAccess) ReadBilling(rc fwra.Context, customerID uuid.UUID) (Billing, error) {
	const op = "billingstate.ReadBilling"
	if customerID == uuid.Nil {
		return Billing{}, fwra.New(fwra.ContractMisuse, op+": zero customerID")
	}
	out := Billing{ID: customerID}
	var version uint64
	err := s.pool.QueryRow(rc.Context, selectCustomerSQL, customerID).Scan(&version, &out.GatewayBound,
		&out.Terms.ComputeCostKind, &out.Terms.ComputeMarkupPercent, &out.Terms.ScheduleKind, &out.Terms.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return Billing{}, fwpg.MapError(err, op)
	}
	out.Version = Version(version)
	out.Registered = true
	return out, nil
}

const selectCycleSQL = `
SELECT ` + outcomeColumns + `
FROM billing_cycle
WHERE customer_id = $1 AND cycle = $2`

// outcomeColumns are a cycle's BillingOutcome columns, in scanOutcome's order.
const outcomeColumns = `net_minor_units, net_currency, directive, declined, escalated, attempts, sweeps_aged`

// outcomeDest is the Scan destination list for outcomeColumns; directive lands in d.
func outcomeDest(o *BillingOutcome, d *int16) []any {
	return []any{&o.Net.MinorUnits, &o.Net.Currency, d, &o.Declined, &o.Escalated, &o.Attempts, &o.SweepsAged}
}

// ReadCycle returns one billing cycle of a registered customer. A cycle nothing has
// settled yet is an open period — Settled=false, not an error; a customer that was
// never registered is NotFound.
func (s *postgresBillingStateAccess) ReadCycle(rc fwra.Context, customerID uuid.UUID, cycle string) (BillingCycle, error) {
	const op = "billingstate.ReadCycle"
	if customerID == uuid.Nil {
		return BillingCycle{}, fwra.New(fwra.ContractMisuse, op+": zero customerID")
	}
	if cycle == "" {
		return BillingCycle{}, fwra.New(fwra.ContractMisuse, op+": empty cycle")
	}
	out := BillingCycle{Cycle: cycle}
	var directive int16
	err := s.pool.QueryRow(rc.Context, selectCycleSQL, customerID, cycle).Scan(outcomeDest(&out.Outcome, &directive)...)
	if err == nil {
		out.Settled = true
		out.Outcome.Directive = RoutingDirective(directive)
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return BillingCycle{}, fwpg.MapError(err, op)
	}
	var registered bool
	if perr := s.pool.QueryRow(rc.Context, existsSQL, customerID).Scan(&registered); perr != nil {
		return BillingCycle{}, fwpg.MapError(perr, op+": no billing aggregate for customer")
	}
	return BillingCycle{Cycle: cycle}, nil
}

// selectDelinquentSQL is the cross-row delinquency scan: every customer with at least
// one escalated cycle, narrowed to one project when $1 is non-empty. Ordered by id so
// a sweep visits customers deterministically.
const selectDelinquentSQL = `
SELECT c.customer_id
FROM billing_customer c
WHERE ($1 = '' OR c.project_id = $1)
  AND EXISTS (
      SELECT 1 FROM billing_cycle y
      WHERE y.customer_id = c.customer_id AND y.escalated)
ORDER BY c.customer_id`

// ReadPersistentlyDelinquentCustomers returns the customers with an unpaid
// (escalated) cycle. scope.ProjectID narrows the set to the customers registered under
// that project; an empty ProjectID is the platform-wide sweep. An empty result is an
// empty (non-nil) slice, never NotFound.
func (s *postgresBillingStateAccess) ReadPersistentlyDelinquentCustomers(rc fwra.Context, scope DelinquencyScope) ([]CustomerSummary, error) {
	const op = "billingstate.ReadPersistentlyDelinquentCustomers"
	rows, err := s.pool.Query(rc.Context, selectDelinquentSQL, scope.ProjectID)
	if err != nil {
		return nil, fwpg.MapError(err, op)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CustomerSummary, error) {
		c := CustomerSummary{PauseNotWithdraw: delinquentPauses}
		return c, row.Scan(&c.ID)
	})
	if err != nil {
		return nil, fwpg.MapError(err, op)
	}
	if out == nil {
		out = []CustomerSummary{}
	}
	return out, nil
}

// selectDeclinedSQL is the shortfall sweep's scan: every settled cycle whose charge
// stands declined and is not escalated, narrowed to one project's customers when $1 is
// non-empty. Ordered by customer then cycle so a sweep visits them deterministically.
const selectDeclinedSQL = `
SELECT y.customer_id, y.cycle, ` + qualifiedOutcomeColumns + `
FROM billing_cycle y
JOIN billing_customer c ON c.customer_id = y.customer_id
WHERE y.declined AND NOT y.escalated
  AND ($1 = '' OR c.project_id = $1)
ORDER BY y.customer_id, y.cycle`

// qualifiedOutcomeColumns are outcomeColumns of the billing_cycle alias y.
const qualifiedOutcomeColumns = `y.net_minor_units, y.net_currency, y.directive, y.declined, y.escalated, y.attempts, y.sweeps_aged`

// ReadDeclinedCycles returns the settled cycles whose charge stands declined and is not
// escalated — the shortfall sweep's retry candidates — each with its retry history
// (attempts made, sweeps aged). scope.ProjectID narrows them to one project's
// customers; an empty ProjectID is the platform-wide sweep. An empty result is an empty
// (non-nil) slice, never NotFound.
func (s *postgresBillingStateAccess) ReadDeclinedCycles(rc fwra.Context, scope DelinquencyScope) ([]DeclinedCycle, error) {
	const op = "billingstate.ReadDeclinedCycles"
	rows, err := s.pool.Query(rc.Context, selectDeclinedSQL, scope.ProjectID)
	if err != nil {
		return nil, fwpg.MapError(err, op)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeclinedCycle, error) {
		var (
			d         DeclinedCycle
			directive int16
		)
		err := row.Scan(append([]any{&d.CustomerID, &d.Cycle}, outcomeDest(&d.Outcome, &directive)...)...)
		d.Outcome.Directive = RoutingDirective(directive)
		return d, err
	})
	if err != nil {
		return nil, fwpg.MapError(err, op)
	}
	if out == nil {
		out = []DeclinedCycle{}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Writes (version-guarded, dedup-first).
// ---------------------------------------------------------------------------

const insertCustomerSQL = `
INSERT INTO billing_customer (customer_id, version, project_id,
    terms_compute_cost_kind, terms_compute_markup_pct, terms_schedule_kind, terms_currency)
VALUES ($1, 1, $2, $3, $4, $5, $6)
ON CONFLICT (customer_id) DO NOTHING
RETURNING version`

// RegisterCustomer opens the customer's billing aggregate at version 1 under the
// profile's project and terms (the committed option's regime, markup, schedule and
// currency, DCT §12 B13). Only expectedVersion 0 opens it: registering a customer that
// already exists, under a different key, is a Conflict (a replay of the same key is
// the recorded version), and a positive expectation for a customer that does not exist
// is NotFound. A negative markup is ContractMisuse.
func (s *postgresBillingStateAccess) RegisterCustomer(rc fwra.Context, customerID uuid.UUID, expectedVersion Version, profile CustomerProfile, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	const op = "billingstate.RegisterCustomer"
	if profile.Terms.ComputeMarkupPercent < 0 {
		return 0, fwra.New(fwra.ContractMisuse, op+": negative compute markup")
	}
	t := profile.Terms
	return s.mutate(rc, op, customerID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		if expectedVersion != 0 {
			return 0, missOrConflict(ctx, tx, op, customerID, "registration opens the aggregate from version 0")
		}
		var v uint64
		err := tx.QueryRow(ctx, insertCustomerSQL, customerID, profile.ProjectID,
			t.ComputeCostKind, t.ComputeMarkupPercent, t.ScheduleKind, t.Currency).Scan(&v)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fwra.New(fwra.Conflict, op+": customer already registered")
		}
		if err != nil {
			return 0, fwpg.MapError(err, op+": insert")
		}
		return Version(v), nil
	})
}

// BindGatewayLive binds the customer's gateway customer reference and marks the
// aggregate gateway-bound. Version-guarded.
func (s *postgresBillingStateAccess) BindGatewayLive(rc fwra.Context, customerID uuid.UUID, expectedVersion Version, binding GatewayBinding, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	const op = "billingstate.BindGatewayLive"
	if binding.GatewayCustomerRef == "" {
		return 0, fwra.New(fwra.ContractMisuse, op+": empty GatewayCustomerRef")
	}
	return s.mutate(rc, op, customerID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		return versionedUpdate(ctx, tx, op, customerID, expectedVersion, `
UPDATE billing_customer
SET version = version + 1, gateway_bound = true, gateway_customer_ref = $3, updated_at = now()
WHERE customer_id = $1 AND version = $2
RETURNING version`, customerID, uint64(expectedVersion), binding.GatewayCustomerRef)
	})
}

const insertCycleSQL = `
INSERT INTO billing_cycle (customer_id, cycle, ` + outcomeColumns + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (customer_id, cycle) DO NOTHING
RETURNING customer_id`

// SettleCycle records the close of one billing cycle with its outcome (version
// guarded). A cycle is settled once: settling it again under another key is a
// Conflict — ResettleCycle is the correction verb.
func (s *postgresBillingStateAccess) SettleCycle(rc fwra.Context, customerID uuid.UUID, expectedVersion Version, cycle string, outcome BillingOutcome, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	const op = "billingstate.SettleCycle"
	if err := checkCycleWrite(op, cycle, outcome); err != nil {
		return 0, err
	}
	return s.mutate(rc, op, customerID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		return writeCycle(ctx, tx, op, customerID, expectedVersion, insertCycleSQL, cycle, outcome,
			fwra.New(fwra.Conflict, op+": cycle already settled (ResettleCycle corrects it)"))
	})
}

const updateCycleSQL = `
UPDATE billing_cycle
SET net_minor_units = $3, net_currency = $4, directive = $5, declined = $6, escalated = $7,
    attempts = $8, sweeps_aged = $9, updated_at = now()
WHERE customer_id = $1 AND cycle = $2
RETURNING customer_id`

// ResettleCycle replaces the outcome of an already settled cycle with its correction
// (a retried charge that went through, a shortfall left for the next sweep, or one
// escalated). Version-guarded; a cycle that was never settled has nothing to correct
// (NotFound).
func (s *postgresBillingStateAccess) ResettleCycle(rc fwra.Context, customerID uuid.UUID, expectedVersion Version, cycle string, correction BillingOutcome, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	const op = "billingstate.ResettleCycle"
	if err := checkCycleWrite(op, cycle, correction); err != nil {
		return 0, err
	}
	return s.mutate(rc, op, customerID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		return writeCycle(ctx, tx, op, customerID, expectedVersion, updateCycleSQL, cycle, correction,
			fwra.New(fwra.NotFound, op+": cycle was never settled"))
	})
}

// checkCycleWrite enforces the cycle-write preconditions: a named cycle, a retry
// history that is not negative, a declined or escalated outcome only on a charge, and
// a routing directive the contract defines.
func checkCycleWrite(op, cycle string, outcome BillingOutcome) error {
	if cycle == "" {
		return fwra.New(fwra.ContractMisuse, op+": empty cycle")
	}
	if outcome.Attempts < 0 || outcome.SweepsAged < 0 {
		return fwra.New(fwra.ContractMisuse, op+": negative retry history")
	}
	if (outcome.Declined || outcome.Escalated) && outcome.Directive != RoutingCharge {
		return fwra.New(fwra.ContractMisuse, op+": only a charge can be declined or escalated")
	}
	switch outcome.Directive {
	case RoutingNoAction, RoutingCharge:
		return nil
	default:
		return fwra.New(fwra.ContractMisuse, op+": unknown routing directive")
	}
}

// writeCycle bumps the customer's version (the guard) and writes one cycle row with
// sql — an insert that skips an existing row, or an update of an existing one; either
// RETURNs the row it wrote. A statement that wrote no row fails with noRow.
func writeCycle(ctx context.Context, tx pgx.Tx, op string, customerID uuid.UUID, expectedVersion Version, sql, cycle string, outcome BillingOutcome, noRow error) (Version, error) {
	v, err := versionedUpdate(ctx, tx, op, customerID, expectedVersion, `
UPDATE billing_customer
SET version = version + 1, updated_at = now()
WHERE customer_id = $1 AND version = $2
RETURNING version`, customerID, uint64(expectedVersion))
	if err != nil {
		return 0, err
	}
	var written uuid.UUID
	err = tx.QueryRow(ctx, sql, customerID, cycle,
		outcome.Net.MinorUnits, outcome.Net.Currency, int(outcome.Directive), outcome.Declined, outcome.Escalated,
		outcome.Attempts, outcome.SweepsAged).Scan(&written)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, noRow
	}
	if err != nil {
		return 0, fwpg.MapError(err, op+": write cycle")
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Shared write machinery.
// ---------------------------------------------------------------------------

const dedupSelectSQL = `SELECT op, customer_id, resulting_version FROM billing_mutation WHERE idempotency_key = $1`

const ledgerInsertSQL = `
INSERT INTO billing_mutation (idempotency_key, op, customer_id, resulting_version)
VALUES ($1, $2, $3, $4)`

const existsSQL = `SELECT true FROM billing_customer WHERE customer_id = $1`

// mutate runs one head-state write in a single transaction with the dedup-first
// discipline: (1) a key already applied returns its recorded resulting version (no-op
// success) — or ContractMisuse when it was applied by another op or to another
// customer (the merchantGatewayAccess rule: a key names one request); (2) otherwise
// the write runs, the key -> version is recorded and the transaction commits. A write
// error rolls everything back, so the ledger holds only applied writes.
func (s *postgresBillingStateAccess) mutate(
	rc fwra.Context,
	op string,
	customerID uuid.UUID,
	key fwra.IdempotencyKey,
	do func(ctx context.Context, tx pgx.Tx) (Version, error),
) (Version, error) {
	ctx := rc.Context
	if customerID == uuid.Nil {
		return 0, fwra.New(fwra.ContractMisuse, op+": zero customerID")
	}
	if key == "" {
		return 0, fwra.New(fwra.ContractMisuse, op+": empty idempotencyKey")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fwpg.MapError(err, op)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	prior, replayed, err := replayedVersion(ctx, tx, op, customerID, key)
	if err != nil {
		return 0, err
	}
	if replayed {
		if cerr := tx.Commit(ctx); cerr != nil {
			return 0, fwpg.MapError(cerr, op+": commit (dedup)")
		}
		return prior, nil
	}

	newV, err := do(ctx, tx)
	if err != nil {
		return 0, err
	}
	if _, lerr := tx.Exec(ctx, ledgerInsertSQL, string(key), op, customerID, uint64(newV)); lerr != nil {
		return 0, fwpg.MapError(lerr, op+": ledger")
	}
	if cerr := tx.Commit(ctx); cerr != nil {
		return 0, fwpg.MapError(cerr, op+": commit")
	}
	return newV, nil
}

// replayedVersion reports the version a key already applied resulted in. A key
// recorded by another op or against another customer is a caller error, not a replay:
// a replay is the same write retried, so it carries the same op and customer.
func replayedVersion(ctx context.Context, tx pgx.Tx, op string, customerID uuid.UUID, key fwra.IdempotencyKey) (Version, bool, error) {
	var (
		appliedBy string
		owner     uuid.UUID
		prior     uint64
	)
	err := tx.QueryRow(ctx, dedupSelectSQL, string(key)).Scan(&appliedBy, &owner, &prior)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, fwpg.MapError(err, op+": dedup")
	case appliedBy != op:
		return 0, false, fwra.New(fwra.ContractMisuse, op+": idempotencyKey already applied by "+appliedBy)
	case owner != customerID:
		return 0, false, fwra.New(fwra.ContractMisuse, op+": idempotencyKey already applied to another customer")
	default:
		return Version(prior), true, nil
	}
}

// versionedUpdate runs an UPDATE … WHERE customer_id = $1 AND version = $2 …
// RETURNING version. A matched row returns its bumped version; no matched row is a
// Conflict when the customer exists (stale expectation) and NotFound when it does not.
func versionedUpdate(
	ctx context.Context,
	tx pgx.Tx,
	op string,
	customerID uuid.UUID,
	expectedVersion Version,
	sql string,
	args ...any,
) (Version, error) {
	if expectedVersion == 0 {
		return 0, fwra.New(fwra.ContractMisuse, op+": zero expectedVersion for an update")
	}
	var v uint64
	err := tx.QueryRow(ctx, sql, args...).Scan(&v)
	if err == nil {
		return Version(v), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fwpg.MapError(err, op)
	}
	return 0, missOrConflict(ctx, tx, op, customerID, "stale expectedVersion")
}

// missOrConflict disambiguates a write that matched no row: the customer exists ⇒
// Conflict (carrying why), it does not ⇒ NotFound.
func missOrConflict(ctx context.Context, tx pgx.Tx, op string, customerID uuid.UUID, why string) error {
	var exists bool
	if err := tx.QueryRow(ctx, existsSQL, customerID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fwra.New(fwra.NotFound, op+": no billing aggregate for customer")
		}
		return fwpg.MapError(err, op+": existence probe")
	}
	return fwra.New(fwra.Conflict, op+": "+why)
}
