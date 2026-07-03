-- operated_system — head-state store for the operatedSystemStateAccess ResourceAccess.
--
-- This file IS the migration: per the server's established convention (see
-- usage/schema.sql and the retired projectstate/postgres.go), each Postgres-backed
-- RA self-applies its deterministic, idempotent DDL in its New* constructor over the
-- shared pgx pool. C-OSA's builder applies this file (//go:embed schema.sql) at server
-- boot — no external migration tool, no operator step; the schema reconciles on every
-- deploy.
--
-- Discipline (operational-concepts.md §13, HEAD-STATE class — NOT the append-only
-- ledger the sibling usageAccess is):
--   * Each operated app has ONE mutable row carrying an optimistic-concurrency
--     `version`. Every write verb carries expectedVersion; a mismatch is a terminal
--     fwra.Conflict the operationsManager recovers with its §6.5 re-read→re-apply loop.
--     A successful write bumps version by exactly one and RETURNs the new value.
--   * Dedup-first idempotency = the companion operated_system_mutation ledger keyed on
--     the caller-supplied fwra.IdempotencyKey. A replayed write collapses to the
--     already-recorded resulting version (idempotent no-op success), so a Temporal
--     activity retry after a lost result never double-applies a transition.
--
-- Column ↔ contract mapping (operatedSystemStateAccess §3 OperatedSystem):
--   operated_app_id        → OperatedSystem.ID (uuid PK; the operated app identity).
--   version                → OperatedSystem.Version (optimistic-concurrency guard).
--   status                 → OperatedSystem.Status (RuntimeStatus enum: 0 Unknown /
--                            1 Pending / 2 Healthy / 3 Degraded / 4 Withdrawn). Set by
--                            RecordRuntimeStatusChange (observed) and the withdraw/
--                            delinquency-withdraw terminals; a republish does NOT touch it.
--   in_flight              → OperatedSystem.InFlight (true ⇒ actively operated; the
--                            ReadInFlightOperatedApps scan predicate). Set true on
--                            publish, false on withdraw / delinquency-withdraw.
--   deployable_bundle_ref  → OperatedSystem.DeployableBundleRef. GAP (documented in
--                            postgres.go + the C-OSA follow-up): the frozen §2 contract
--                            has NO write verb carrying a bundle ref, so this column has
--                            no writer here and stays '' — the deploy-after-construction
--                            seeding handoff (a cross-Manager write or an added verb) is
--                            a follow-up. Read back faithfully so a seeded row works.
--   customer_id            → the delinquency-sweep scope key for ReadInFlightOperatedApps
--                            (InFlightScope.CustomerID). Same GAP: no write verb carries
--                            a customer id, so it stays NULL and a customer-scoped sweep
--                            returns empty until the seeding handoff lands. Nullable.
--   last_reason            → the last DesiredStateReason a publish recorded (audit).
--   last_delinquency       → the last DelinquencyAction recorded (audit).
--   decision_*             → the last AutoscaleDecision a reason=autoscale publish
--                            carried (audit; nullable — most publishes carry none).

CREATE TABLE IF NOT EXISTS operated_system (
    operated_app_id       uuid        PRIMARY KEY,
    version               bigint      NOT NULL CHECK (version > 0),
    status                smallint    NOT NULL DEFAULT 0,
    in_flight             boolean     NOT NULL DEFAULT false,
    deployable_bundle_ref text        NOT NULL DEFAULT '',
    customer_id           uuid        NULL,
    last_reason           smallint    NOT NULL DEFAULT 0,
    last_delinquency      smallint    NOT NULL DEFAULT 0,
    decision_action       smallint    NULL,
    decision_delta        bigint      NULL,
    decision_to_baseline  bigint      NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- ReadInFlightOperatedApps: the reconcile-tick / delinquency-sweep cross-row scan is
-- always scoped to in_flight rows. Partial index — withdrawn/terminal rows never match.
CREATE INDEX IF NOT EXISTS operated_system_inflight_idx
    ON operated_system (in_flight)
    WHERE in_flight;

-- ReadInFlightOperatedApps customer scope (delinquency sweep). Partial: only in-flight,
-- customer-tagged rows participate (customer_id NULL until the seeding handoff lands).
CREATE INDEX IF NOT EXISTS operated_system_customer_inflight_idx
    ON operated_system (customer_id)
    WHERE in_flight AND customer_id IS NOT NULL;

-- Dedup-first idempotency ledger. Each successful write records its caller idempotency
-- key with the resulting version; a replayed write reads it back (no-op success). This
-- is the HEAD-STATE analogue of usageAccess's UNIQUE(runtime_event_id) dedup.
CREATE TABLE IF NOT EXISTS operated_system_mutation (
    idempotency_key   text        PRIMARY KEY,
    operated_app_id   uuid        NOT NULL,
    resulting_version bigint      NOT NULL,
    applied_at        timestamptz NOT NULL DEFAULT now()
);
