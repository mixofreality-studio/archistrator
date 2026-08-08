// Package operatedsystemstate is the operatedSystemStateAccess component of the
// ResourceAccess layer — the Postgres-backed store for OPERATED apps' runtime
// system state (the operations rail), distinct from the git-backed design-time
// project state in the projectstate package.
package operatedsystemstate

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

// postgresOperatedSystemStateAccess is the concrete, Postgres-backed implementation of
// OperatedSystemStateAccess (operatedSystemStateAccess §6 infrastructure mapping). It is
// UNEXPORTED — the package's only public surface is the generated
// OperatedSystemStateAccess interface + models + the generated
// NewPostgresOperatedSystemStateAccess constructor (option-1 delegated DI). The head
// state lives in the mutable operated_system row (one per operated app) carrying an
// optimistic-concurrency `version`; idempotency is the companion
// operated_system_mutation ledger. The struct imports NO Temporal (layer rule): the
// idempotency key arrives as an ordinary parameter on each write and is never read from
// ambient context.
type postgresOperatedSystemStateAccess struct {
	pool *pgxpool.Pool
}

// Compile-time proof the concrete impl satisfies the generated port. If the port ever
// drifts, this line breaks the build — the guard The Method wants between a contract and
// its construction.
var _ OperatedSystemStateAccess = (*postgresOperatedSystemStateAccess)(nil)

// schemaDDL is the deterministic, idempotent head-state migration (schema.sql in this
// package IS the migration). Applying it in the constructor keeps schema setup
// co-located with the only component allowed to touch the operated_system Resource and
// makes the Store self-sufficient for both production wiring and the integration tests
// — the usageAccess / projectstate.NewStore convention.
//
//go:embed schema.sql
var schemaDDL string

// newPostgresOperatedSystemStateAccess is the hand-written, unexported builder behind the
// generated NewPostgresOperatedSystemStateAccess constructor (option-1 delegated DI). It
// builds the impl over an existing pgx pool and applies the embedded, idempotent schema —
// the stateful setup the builder owns — returning the interface so the concrete struct
// stays unexported. Safe to run on every boot/redeploy.
func newPostgresOperatedSystemStateAccess(ctx context.Context, pool *pgxpool.Pool) (OperatedSystemStateAccess, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, "operatedsystemstate.NewPostgresOperatedSystemStateAccess: nil pool")
	}
	if _, err := pool.Exec(ctx, schemaDDL); err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "operatedsystemstate.NewPostgresOperatedSystemStateAccess: apply schema")
	}
	return &postgresOperatedSystemStateAccess{pool: pool}, nil
}

// ---------------------------------------------------------------------------
// Reads (pure).
// ---------------------------------------------------------------------------

const selectOperatedSystemSQL = `
SELECT operated_app_id, version, status, in_flight, deployable_bundle_ref
FROM operated_system
WHERE operated_app_id = $1`

// ReadOperatedSystem returns the whole head-state for one operated app (§2). A missing
// row is fwra.NotFound (via the pg error mapper).
func (s *postgresOperatedSystemStateAccess) ReadOperatedSystem(rc fwra.Context, operatedAppID uuid.UUID) (OperatedSystem, error) {
	const op = "operatedsystemstate.ReadOperatedSystem"
	if operatedAppID == uuid.Nil {
		return OperatedSystem{}, fwra.New(fwra.ContractMisuse, op+": zero operatedAppID")
	}
	var (
		out    OperatedSystem
		status int16
	)
	err := s.pool.QueryRow(rc.Context, selectOperatedSystemSQL, operatedAppID).
		Scan(&out.ID, &out.Version, &status, &out.InFlight, &out.DeployableBundleRef)
	if err != nil {
		return OperatedSystem{}, fwpg.MapError(err, op)
	}
	out.Status = RuntimeStatus(status)
	return out, nil
}

// ReadInFlightOperatedApps returns the in-flight operated apps for a scope (§2): empty
// scope ⇒ every in-flight app (the default reconcile tick); AppIDs set ⇒ that subset;
// CustomerID set ⇒ the delinquent customer's in-flight apps (the delinquency sweep). An
// empty result is an empty (non-nil) slice, never NotFound.
func (s *postgresOperatedSystemStateAccess) ReadInFlightOperatedApps(rc fwra.Context, scope InFlightScope) ([]OperatedSystemSummary, error) {
	const op = "operatedsystemstate.ReadInFlightOperatedApps"

	// Fixed-placeholder predicates: a NULL bind no-ops its clause, so the whole scope
	// (all in-flight / AppIDs subset / CustomerID sweep) is one static, injection-free
	// query. AppIDs is bound as NULL when empty; CustomerID as NULL when nil.
	var appIDs any
	if len(scope.AppIDs) > 0 {
		appIDs = scope.AppIDs
	}
	var customerID any
	if scope.CustomerID != nil {
		if *scope.CustomerID == uuid.Nil {
			return nil, fwra.New(fwra.ContractMisuse, op+": CustomerID set but zero (use nil for all in-flight apps)")
		}
		customerID = *scope.CustomerID
	}

	const q = `
SELECT operated_app_id, version, status
FROM operated_system
WHERE in_flight
  AND ($1::uuid[] IS NULL OR operated_app_id = ANY($1))
  AND ($2::uuid IS NULL OR customer_id = $2)
ORDER BY operated_app_id`

	rows, err := s.pool.Query(rc.Context, q, appIDs, customerID)
	if err != nil {
		return nil, fwpg.MapError(err, op)
	}
	defer rows.Close()

	out := []OperatedSystemSummary{}
	for rows.Next() {
		var (
			sum    OperatedSystemSummary
			status int16
		)
		if scanErr := rows.Scan(&sum.ID, &sum.Version, &status); scanErr != nil {
			return nil, fwpg.MapError(scanErr, op+": scan row")
		}
		sum.Status = RuntimeStatus(status)
		out = append(out, sum)
	}
	if rErr := rows.Err(); rErr != nil {
		return nil, fwpg.MapError(rErr, op+": iterate rows")
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Writes (version-guarded, dedup-first idempotent).
// ---------------------------------------------------------------------------

const dedupSelectSQL = `SELECT resulting_version FROM operated_system_mutation WHERE idempotency_key = $1`

const ledgerInsertSQL = `
INSERT INTO operated_system_mutation (idempotency_key, operated_app_id, resulting_version)
VALUES ($1, $2, $3)`

const existsSQL = `SELECT true FROM operated_system WHERE operated_app_id = $1`

const insertOperatedSystemSQL = `
INSERT INTO operated_system
    (operated_app_id, version, status, in_flight, last_reason,
     decision_action, decision_delta, decision_to_baseline)
VALUES ($1, 1, $2, true, $3, $4, $5, $6)
ON CONFLICT (operated_app_id) DO NOTHING
RETURNING version`

const insertRegisterOperatedSystemSQL = `
INSERT INTO operated_system
    (operated_app_id, version, status, in_flight, deployable_bundle_ref, customer_id, project_ref)
VALUES ($1, 1, $2, false, $3, $4, $5)
ON CONFLICT (operated_app_id) DO NOTHING
RETURNING version`

// RegisterOperatedSystem seeds the head-state row for a newly operated app (§2). It is
// the writer for deployable_bundle_ref, customer_id, and project_ref — the onboarding
// columns the frozen desired-state verbs above have no way to set. The row is created at
// version 1, status Unknown, not yet in-flight (PublishDesiredState is what puts it
// in-flight). Dedup-first, like every other write verb: a replayed key collapses to the
// recorded resulting version. A second registration under a DIFFERENT key against an
// already-registered app is not an overwrite — it is a terminal fwra.Conflict, matching
// the head-state discipline the other verbs follow.
func (s *postgresOperatedSystemStateAccess) RegisterOperatedSystem(
	rc fwra.Context,
	operatedAppID uuid.UUID,
	customerID uuid.UUID,
	projectRef string,
	deployableBundleRef string,
	idempotencyKey fwra.IdempotencyKey,
) (Version, error) {
	const op = "operatedsystemstate.RegisterOperatedSystem"
	return s.mutate(rc, op, operatedAppID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		var v uint64
		err := tx.QueryRow(ctx, insertRegisterOperatedSystemSQL,
			operatedAppID, int(RuntimeStatusUnknown), deployableBundleRef, customerID, projectRef).Scan(&v)
		if errors.Is(err, pgx.ErrNoRows) {
			// A row already exists: not an overwrite.
			return 0, fwra.New(fwra.Conflict, op+": operated app already registered")
		}
		if err != nil {
			return 0, fwpg.MapError(err, op+": insert")
		}
		return Version(v), nil
	})
}

// PublishDesiredState records the head-state desired-state transition (§2). With
// expectedVersion 0 it CREATES the head-state row (version 1, status Pending, in-flight)
// — the create seam a version-0 caller uses; with a positive expectedVersion it applies a
// version-guarded republish (in-flight, reason, optional autoscale decision) WITHOUT
// touching the observed status. Stale expectation ⇒ Conflict; missing app on a positive
// expectation ⇒ NotFound.
func (s *postgresOperatedSystemStateAccess) PublishDesiredState(rc fwra.Context, operatedAppID uuid.UUID, expectedVersion Version, reason DesiredStateReason, decision *AutoscaleDecision, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	const op = "operatedsystemstate.PublishDesiredState"
	var da, dd, db any // NULL unless an autoscale decision is carried
	if decision != nil {
		da, dd, db = int(decision.Action), decision.Delta, decision.ToBaseline
	}
	return s.mutate(rc, op, operatedAppID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		if expectedVersion == 0 {
			var v uint64
			err := tx.QueryRow(ctx, insertOperatedSystemSQL,
				operatedAppID, int(RuntimeStatusPending), int(reason), da, dd, db).Scan(&v)
			if errors.Is(err, pgx.ErrNoRows) {
				// A row already exists: a version-0 create expectation is stale.
				return 0, fwra.New(fwra.Conflict, op+": operated system already exists (expectedVersion 0)")
			}
			if err != nil {
				return 0, fwpg.MapError(err, op+": insert")
			}
			return Version(v), nil
		}
		return s.versionedUpdate(ctx, tx, op, operatedAppID, expectedVersion, `
UPDATE operated_system
SET version = version + 1, in_flight = true, last_reason = $3,
    decision_action = $4, decision_delta = $5, decision_to_baseline = $6, updated_at = now()
WHERE operated_app_id = $1 AND version = $2
RETURNING version`, operatedAppID, uint64(expectedVersion), int(reason), da, dd, db)
	})
}

// RecordRuntimeStatusChange records an observed runtime-status transition (§2),
// version-guarded. Does not change in-flight (an app stays in-flight through health
// transitions; only withdraw ends operation).
func (s *postgresOperatedSystemStateAccess) RecordRuntimeStatusChange(rc fwra.Context, operatedAppID uuid.UUID, expectedVersion Version, status RuntimeStatus, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	const op = "operatedsystemstate.RecordRuntimeStatusChange"
	return s.mutate(rc, op, operatedAppID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		return s.versionedUpdate(ctx, tx, op, operatedAppID, expectedVersion, `
UPDATE operated_system
SET version = version + 1, status = $3, updated_at = now()
WHERE operated_app_id = $1 AND version = $2
RETURNING version`, operatedAppID, uint64(expectedVersion), int(status))
	})
}

// WithdrawSystem marks the operated system withdrawn (§2) — the head-state terminal:
// status Withdrawn, in-flight cleared. Version-guarded.
func (s *postgresOperatedSystemStateAccess) WithdrawSystem(rc fwra.Context, operatedAppID uuid.UUID, expectedVersion Version, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	const op = "operatedsystemstate.WithdrawSystem"
	return s.mutate(rc, op, operatedAppID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		return s.versionedUpdate(ctx, tx, op, operatedAppID, expectedVersion, `
UPDATE operated_system
SET version = version + 1, status = $3, in_flight = false, updated_at = now()
WHERE operated_app_id = $1 AND version = $2
RETURNING version`, operatedAppID, uint64(expectedVersion), int(RuntimeStatusWithdrawn))
	})
}

// RecordDelinquencyAction records a delinquency-handling action (§2). A pause keeps the
// app in-flight (replicas=0 is still an operated app); a withdraw is terminal (status
// Withdrawn, in-flight cleared). Version-guarded.
func (s *postgresOperatedSystemStateAccess) RecordDelinquencyAction(rc fwra.Context, operatedAppID uuid.UUID, expectedVersion Version, action DelinquencyAction, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	const op = "operatedsystemstate.RecordDelinquencyAction"
	return s.mutate(rc, op, operatedAppID, idempotencyKey, func(ctx context.Context, tx pgx.Tx) (Version, error) {
		switch action {
		case DelinquencyActionWithdrawn:
			return s.versionedUpdate(ctx, tx, op, operatedAppID, expectedVersion, `
UPDATE operated_system
SET version = version + 1, status = $3, in_flight = false, last_delinquency = $4, updated_at = now()
WHERE operated_app_id = $1 AND version = $2
RETURNING version`, operatedAppID, uint64(expectedVersion), int(RuntimeStatusWithdrawn), int(action))
		case DelinquencyActionPaused:
			return s.versionedUpdate(ctx, tx, op, operatedAppID, expectedVersion, `
UPDATE operated_system
SET version = version + 1, in_flight = true, last_delinquency = $3, updated_at = now()
WHERE operated_app_id = $1 AND version = $2
RETURNING version`, operatedAppID, uint64(expectedVersion), int(action))
		case DelinquencyActionUnknown:
			fallthrough
		default:
			return 0, fwra.New(fwra.ContractMisuse, op+": unknown delinquency action")
		}
	})
}

// ---------------------------------------------------------------------------
// Shared write machinery.
// ---------------------------------------------------------------------------

// mutate runs one version-guarded head-state write inside a single transaction with the
// dedup-first idempotency discipline: (1) if the caller key was already applied, return
// the recorded resulting version (no-op success); (2) otherwise run the supplied write,
// record the key→version in the ledger, and commit. A write error rolls the whole
// transaction back so the ledger is written ONLY on success — a retried key never
// double-applies.
func (s *postgresOperatedSystemStateAccess) mutate(
	rc fwra.Context,
	op string,
	operatedAppID uuid.UUID,
	key fwra.IdempotencyKey,
	do func(ctx context.Context, tx pgx.Tx) (Version, error),
) (Version, error) {
	ctx := rc.Context
	if operatedAppID == uuid.Nil {
		return 0, fwra.New(fwra.ContractMisuse, op+": zero operatedAppID")
	}
	if key == "" {
		return 0, fwra.New(fwra.ContractMisuse, op+": empty idempotencyKey")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fwpg.MapError(err, op)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	// (1) dedup-first: a replayed key collapses to the recorded resulting version.
	var prior uint64
	switch derr := tx.QueryRow(ctx, dedupSelectSQL, string(key)).Scan(&prior); {
	case derr == nil:
		if cerr := tx.Commit(ctx); cerr != nil {
			return 0, fwpg.MapError(cerr, op+": commit (dedup)")
		}
		return Version(prior), nil
	case errors.Is(derr, pgx.ErrNoRows):
		// first application — fall through
	default:
		return 0, fwpg.MapError(derr, op+": dedup")
	}

	// (2) apply the version-guarded write.
	newV, err := do(ctx, tx)
	if err != nil {
		return 0, err
	}

	// Record the key→version so a replay is a no-op success.
	if _, lerr := tx.Exec(ctx, ledgerInsertSQL, string(key), operatedAppID, uint64(newV)); lerr != nil {
		return 0, fwpg.MapError(lerr, op+": ledger")
	}
	if cerr := tx.Commit(ctx); cerr != nil {
		return 0, fwpg.MapError(cerr, op+": commit")
	}
	return newV, nil
}

// noopOperatedSystemStateAccess is the permanent LOCAL-PROFILE no-op
// OperatedSystemStateAccess (operational-concepts.md's local environment:
// "operatedSystemState=no-op" — local-first-init-funnel Task 2b; the init-funnel
// target has no Postgres, and operations/deploy is a cloud-only, paid-tier concern
// per the canonical deployment-scenario decision: "Local-first is the free
// acquisition funnel (design + construction on the user's own subscription); hosted
// deploy/operate is the paid tier"). It mirrors usage.noopUsageAccess's documented
// stance: NO persistence. Every read behaves as though no operated app has ever
// existed (ReadOperatedSystem -> NotFound, matching the real store's own "missing row
// is NotFound" contract; ReadInFlightOperatedApps -> empty). Every write trivially
// "succeeds" (a fixed placeholder Version(1), echoing the real store's fresh-create
// shape) without persisting anything — a write here is not a promise of durability,
// exactly like usageAccess's dropped RecordComputeUsage batch. Caller-misuse
// preconditions (zero operatedAppID, empty idempotencyKey) are still enforced
// identically to the Postgres impl, so callers observe the SAME contract-level errors
// in both profiles. Do not add any behind this type; a future real local head-state
// store is a new, separate implementation, not this one grown up (mirrors
// usage.noopUsageAccess's and revenueledger.noopRevenueLedgerAccess's documented
// stance).
type noopOperatedSystemStateAccess struct{}

// NewNoOpOperatedSystemStateAccess returns the permanent no-op OperatedSystemStateAccess
// for the local profile. It takes no arguments — there is no infrastructure binding
// (the local deployment binding declares infra: [] for this variant).
func NewNoOpOperatedSystemStateAccess() OperatedSystemStateAccess {
	return noopOperatedSystemStateAccess{}
}

var _ OperatedSystemStateAccess = noopOperatedSystemStateAccess{}

// noopPlaceholderVersion is the fixed Version every no-op write "succeeds" with. It is
// NOT a real, incrementing head-state version — nothing is persisted to increment — and
// is chosen to echo the real store's fresh-create shape (a first PublishDesiredState
// creates version 1) rather than the zero value, which this component's real impl
// reserves as a caller-misuse marker ("zero expectedVersion for an update").
const noopPlaceholderVersion Version = 1

// ReadOperatedSystem always reports NotFound — nothing is ever persisted locally, so
// every operated app "doesn't exist" (mirrors the real store's own missing-row
// contract, just unconditionally).
func (noopOperatedSystemStateAccess) ReadOperatedSystem(_ fwra.Context, operatedAppID uuid.UUID) (OperatedSystem, error) {
	const op = "operatedsystemstate.ReadOperatedSystem"
	if operatedAppID == uuid.Nil {
		return OperatedSystem{}, fwra.New(fwra.ContractMisuse, op+": zero operatedAppID")
	}
	return OperatedSystem{}, fwra.New(fwra.NotFound, op+": no operated system (local profile: operatedSystemStateAccess is no-op)")
}

// ReadInFlightOperatedApps always reports no in-flight apps — an empty (non-nil)
// slice, never NotFound, matching the real store's own empty-result contract.
func (noopOperatedSystemStateAccess) ReadInFlightOperatedApps(_ fwra.Context, scope InFlightScope) ([]OperatedSystemSummary, error) {
	const op = "operatedsystemstate.ReadInFlightOperatedApps"
	if scope.CustomerID != nil && *scope.CustomerID == uuid.Nil {
		return nil, fwra.New(fwra.ContractMisuse, op+": CustomerID set but zero (use nil for all in-flight apps)")
	}
	return []OperatedSystemSummary{}, nil
}

// RegisterOperatedSystem trivially "succeeds" (local no-op); nothing is persisted, so a
// subsequent ReadOperatedSystem for the same app still reports NotFound.
func (noopOperatedSystemStateAccess) RegisterOperatedSystem(_ fwra.Context, operatedAppID uuid.UUID, _ uuid.UUID, _ string, _ string, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if err := checkMutateMisuse("operatedsystemstate.RegisterOperatedSystem", operatedAppID, idempotencyKey); err != nil {
		return 0, err
	}
	return noopPlaceholderVersion, nil
}

// PublishDesiredState trivially "succeeds" (local no-op): nothing is persisted, so a
// subsequent ReadOperatedSystem for the same app still reports NotFound.
func (noopOperatedSystemStateAccess) PublishDesiredState(_ fwra.Context, operatedAppID uuid.UUID, _ Version, _ DesiredStateReason, _ *AutoscaleDecision, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if err := checkMutateMisuse("operatedsystemstate.PublishDesiredState", operatedAppID, idempotencyKey); err != nil {
		return 0, err
	}
	return noopPlaceholderVersion, nil
}

// RecordRuntimeStatusChange trivially "succeeds" (local no-op); nothing is persisted.
func (noopOperatedSystemStateAccess) RecordRuntimeStatusChange(_ fwra.Context, operatedAppID uuid.UUID, _ Version, _ RuntimeStatus, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if err := checkMutateMisuse("operatedsystemstate.RecordRuntimeStatusChange", operatedAppID, idempotencyKey); err != nil {
		return 0, err
	}
	return noopPlaceholderVersion, nil
}

// WithdrawSystem trivially "succeeds" (local no-op); nothing is persisted.
func (noopOperatedSystemStateAccess) WithdrawSystem(_ fwra.Context, operatedAppID uuid.UUID, _ Version, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if err := checkMutateMisuse("operatedsystemstate.WithdrawSystem", operatedAppID, idempotencyKey); err != nil {
		return 0, err
	}
	return noopPlaceholderVersion, nil
}

// RecordDelinquencyAction trivially "succeeds" (local no-op); nothing is persisted.
// Unlike the Postgres impl, an unknown DelinquencyAction is NOT distinguished — there
// is no write to route, so every action (including DelinquencyActionUnknown) shares
// the same trivial-success reply; the real store's action-routing switch has nothing
// to bridge to here.
func (noopOperatedSystemStateAccess) RecordDelinquencyAction(_ fwra.Context, operatedAppID uuid.UUID, _ Version, _ DelinquencyAction, idempotencyKey fwra.IdempotencyKey) (Version, error) {
	if err := checkMutateMisuse("operatedsystemstate.RecordDelinquencyAction", operatedAppID, idempotencyKey); err != nil {
		return 0, err
	}
	return noopPlaceholderVersion, nil
}

// checkMutateMisuse enforces the SAME caller-misuse preconditions the Postgres impl's
// mutate() applies (zero operatedAppID, empty idempotencyKey) so callers observe
// identical contract-level errors in both profiles.
func checkMutateMisuse(op string, operatedAppID uuid.UUID, key fwra.IdempotencyKey) error {
	if operatedAppID == uuid.Nil {
		return fwra.New(fwra.ContractMisuse, op+": zero operatedAppID")
	}
	if key == "" {
		return fwra.New(fwra.ContractMisuse, op+": empty idempotencyKey")
	}
	return nil
}

// versionedUpdate runs an UPDATE … WHERE operated_app_id = $1 AND version = $2 …
// RETURNING version. A matched row returns its bumped version. No matched row is
// disambiguated with an existence probe: the row exists ⇒ the expectation was stale
// (fwra.Conflict, driving the Manager re-read loop); the row is absent ⇒ fwra.NotFound.
func (s *postgresOperatedSystemStateAccess) versionedUpdate(
	ctx context.Context,
	tx pgx.Tx,
	op string,
	operatedAppID uuid.UUID,
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
	var exists bool
	if perr := tx.QueryRow(ctx, existsSQL, operatedAppID).Scan(&exists); perr != nil {
		if errors.Is(perr, pgx.ErrNoRows) {
			return 0, fwra.New(fwra.NotFound, op+": no operated system for app")
		}
		return 0, fwpg.MapError(perr, op+": existence probe")
	}
	return 0, fwra.New(fwra.Conflict, op+": stale expectedVersion")
}
