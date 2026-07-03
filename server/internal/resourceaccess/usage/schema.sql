-- usage_log — append-only ledger schema for the usageAccess ResourceAccess.
--
-- Provisioned by R-PG-US (2026-06-10). This file IS the migration: per the
-- server's established convention (see projectstate/postgres.go), each
-- Postgres-backed RA self-applies its deterministic, idempotent DDL in its
-- NewStore constructor over the shared pgx pool. C-UA's NewStore applies this
-- file (e.g. //go:embed schema.sql) at server boot — no external migration
-- tool, no operator step; the schema reconciles on every deploy.
--
-- Frozen contract: designs/aiarch/implementation/contracts/usageAccess.md
-- (APPROVED-AND-FROZEN 2026-05-30; §6 names this table `usage_log`).
-- 2026-06-09 repurpose (architecture.dsl L353/L398): the ledger meters BOTH
--   construction-TOKEN consumption (build phase) AND HOSTING/compute
--   consumption (operation phase) for the user's service bill; billingManager
--   reads the range at billing-period close (UC5) and billingEngine.PriceUsage
--   folds it into PeriodUsage{ConstructionTokens, ComputeUnitSeconds,
--   StorageBytesMonths, EgressBytes} (contracts/billingEngine.md §3).
--
-- Discipline (operational-concepts.md §13, ledger class — NOT head-state):
--   * INSERT-only. Rows are immutable metered facts: never UPDATEd, never
--     DELETEd (trigger-enforced below). No `version` column, no
--     `applied_mutation` ledger — those are the head-state discipline.
--   * Dedup/idempotency = UNIQUE(runtime_event_id): the caller-supplied,
--     globally-unique runtime/worker event id. A replayed tick/withdraw batch
--     collapses per-row to the existing entry (idempotent success, no
--     double-count). No optimistic-concurrency Conflict exists on this store.
--   * No stored counter, no priced/monetary column. Totals are derived at
--     read time by the Engines; pricing is billingEngine's ServicePricing
--     Strategy. This table stores raw, infrastructure-neutral meters only.
--
-- Column ↔ contract mapping (usageAccess.md §3.2 UsageEvent):
--   entry_id         → EntryRef (the ledger's own append position; returned by
--                      the write verbs, never a read key — there is no readEntry)
--   customer_id      → CustomerID (uuid; canonical billing aggregate key per
--                      D-BM §3.0 — shared with billingStateAccess/billingGatewayAccess)
--   operated_app_id  → OperatedAppID. NULLABLE — 2026-06-09 repurpose delta:
--                      hosting/compute facts carry the operated app they are
--                      attributed to; construction-token facts are build-phase
--                      and have NO operated app. (The frozen pre-repurpose §2.1
--                      precondition said non-zero; flagged to the D-UA contract
--                      refresh in implementation/log/R-PG-US.md.)
--   cycle_id         → CycleID (text). Post-repurpose the VALUE is the billing
--                      PeriodID (D-BM §3.0: "PeriodID replaces the settlement-era
--                      CycleID"; readRange is called with CycleID=periodId).
--                      Column keeps the frozen contract §6 name.
--   units_amount +
--   units_unit       → ComputeUnits{Amount, Unit} — one metered dimension per
--                      row, discriminated by the infrastructure-neutral unit
--                      name. OPEN SET (no CHECK enum — the dimension vocabulary
--                      is the metering volatility). Dimensions the UC5 close
--                      path folds today (billingEngine PeriodUsage):
--                        'construction-token'  → ConstructionTokens  (build)
--                        'compute-unit-second' → ComputeUnitSeconds  (hosting)
--                        'storage-byte-month'  → StorageBytesMonths  (hosting)
--                        'egress-byte'         → EgressBytes         (hosting)
--                      (GiB realizations live in the pricing Engine, never here.)
--   runtime_event_id → RuntimeEventID — the UNIQUE dedup token (runtime /
--                      metrics-pipeline / worker supplied; a domain value,
--                      not a Temporal key).
--   raw_meter        → RawMeter []byte (optional opaque source meter, audit only)
--   window_start/end → WindowStart / WindowEnd (the observed window)
--   occurred_at      → OccurredAt (runtime-supplied observation time)
--   recorded_at      → RecordedAt (set by this seam at append)
--
-- Op → query mapping (usageAccess.md §6):
--   recordComputeUsage / recordFinalUsage (+ token-usage appends post-repurpose)
--     → batch INSERT; ON CONFLICT (runtime_event_id) the row's existing
--       entry_id is selected and returned (idempotent no-op success per row).
--   readRange(UsageRangeQuery{CustomerID, CycleID, OperatedAppID?})
--     → SELECT … WHERE customer_id = $1 AND cycle_id = $2
--         [AND operated_app_id = $3]
--       ORDER BY recorded_at, entry_id   -- append order, served by the indexes below

CREATE TABLE IF NOT EXISTS usage_log (
    entry_id         bigint           GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id      uuid             NOT NULL,
    operated_app_id  uuid             NULL,
    cycle_id         text             NOT NULL CHECK (cycle_id <> ''),
    units_amount     double precision NOT NULL CHECK (units_amount >= 0),
    units_unit       text             NOT NULL CHECK (units_unit <> ''),
    runtime_event_id text             NOT NULL CHECK (runtime_event_id <> ''),
    raw_meter        bytea            NULL,
    window_start     timestamptz      NOT NULL,
    window_end       timestamptz      NOT NULL,
    occurred_at      timestamptz      NOT NULL,
    recorded_at      timestamptz      NOT NULL DEFAULT now(),
    CONSTRAINT usage_log_window_ck CHECK (window_end >= window_start),
    CONSTRAINT usage_log_runtime_event_id_key UNIQUE (runtime_event_id)
);

-- readRange, settlement/billing scope (whole period, append order):
-- WHERE customer_id AND cycle_id, ORDER BY recorded_at, entry_id.
CREATE INDEX IF NOT EXISTS usage_log_customer_cycle_idx
    ON usage_log (customer_id, cycle_id, recorded_at, entry_id);

-- readRange, projection scope (one operated app's facts — ncuc6 / optional
-- OperatedAppID predicate, frozen Q5). Partial: construction-token rows have
-- no operated app and never match this predicate.
CREATE INDEX IF NOT EXISTS usage_log_app_cycle_idx
    ON usage_log (operated_app_id, cycle_id)
    WHERE operated_app_id IS NOT NULL;

-- Append-only enforcement at the store (financial-class immutability,
-- operational-concepts.md §13 / Objective 7 audit trail). Facts are never
-- mutated or deleted; a metering correction is a NEW fact with its own
-- runtime_event_id. Idempotent via CREATE OR REPLACE (PG ≥ 14).
CREATE OR REPLACE FUNCTION usage_log_forbid_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'usage_log is append-only: % is forbidden (immutable metered facts)', TG_OP
        USING ERRCODE = 'raise_exception';
END;
$$;

CREATE OR REPLACE TRIGGER usage_log_immutable
    BEFORE UPDATE OR DELETE ON usage_log
    FOR EACH ROW EXECUTE FUNCTION usage_log_forbid_mutation();
