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
--   deployable_bundle_ref  → OperatedSystem.DeployableBundleRef. Written by
--                            RegisterOperatedSystem at onboarding.
--   customer_id            → OperatedSystem.CustomerID (zero UUID when NULL) and the
--                            delinquency-sweep scope key for ReadInFlightOperatedApps
--                            (InFlightScope.CustomerID). Written by RegisterOperatedSystem.
--   project_ref            → OperatedSystem.ProjectRef: the project whose project.json
--                            supplies the deployment model the operationsManager renders
--                            from. A pointer, never a copy.
--   last_reason            → the last DesiredStateReason a publish recorded (audit).
--   last_delinquency       → the last DelinquencyAction recorded (audit).
--   scale_*                → OperatedSystem.Scale: the desired scale the last publish
--                            recorded — the server replicas it published and the
--                            project's scaling policy (min/max replicas, target CPU
--                            utilization, scale-to-zero) — and when it was published
--                            (scaled_at; NULL until the first publish). The autoscaler
--                            reads it as the current desired state and its policy.

CREATE TABLE IF NOT EXISTS operated_system (
    operated_app_id       uuid        PRIMARY KEY,
    version               bigint      NOT NULL CHECK (version > 0),
    status                smallint    NOT NULL DEFAULT 0,
    in_flight             boolean     NOT NULL DEFAULT false,
    deployable_bundle_ref text        NOT NULL DEFAULT '',
    customer_id           uuid        NULL,
    project_ref           text        NOT NULL DEFAULT '',
    last_reason           smallint    NOT NULL DEFAULT 0,
    last_delinquency      smallint    NOT NULL DEFAULT 0,
    scale_replicas        bigint      NOT NULL DEFAULT 0,
    scale_min_replicas    bigint      NOT NULL DEFAULT 0,
    scale_max_replicas    bigint      NOT NULL DEFAULT 0,
    scale_target_util_pct bigint      NOT NULL DEFAULT 0,
    scale_to_zero         boolean     NOT NULL DEFAULT false,
    scaled_at             timestamptz NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now()
);

-- A row created before the desired scale replaced the recorded autoscale decision
-- (2026-10-09) carries the decision_* columns and no scale_* ones: reconcile it in place.
ALTER TABLE operated_system DROP COLUMN IF EXISTS decision_action;
ALTER TABLE operated_system DROP COLUMN IF EXISTS decision_delta;
ALTER TABLE operated_system DROP COLUMN IF EXISTS decision_to_baseline;
ALTER TABLE operated_system ADD COLUMN IF NOT EXISTS scale_replicas        bigint      NOT NULL DEFAULT 0;
ALTER TABLE operated_system ADD COLUMN IF NOT EXISTS scale_min_replicas    bigint      NOT NULL DEFAULT 0;
ALTER TABLE operated_system ADD COLUMN IF NOT EXISTS scale_max_replicas    bigint      NOT NULL DEFAULT 0;
ALTER TABLE operated_system ADD COLUMN IF NOT EXISTS scale_target_util_pct bigint      NOT NULL DEFAULT 0;
ALTER TABLE operated_system ADD COLUMN IF NOT EXISTS scale_to_zero         boolean     NOT NULL DEFAULT false;
ALTER TABLE operated_system ADD COLUMN IF NOT EXISTS scaled_at             timestamptz NULL;

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
