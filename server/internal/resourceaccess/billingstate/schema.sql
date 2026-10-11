-- billingStateAccess head-state schema (the billing aggregate, one per customer).
--
-- This file IS the migration: it is embedded into the package and applied by
-- NewPostgresBillingStateAccess on every boot, so every statement is idempotent
-- (CREATE … IF NOT EXISTS). Both deployment profiles run it against the same
-- Postgres the operations and usage stores use.
--
-- Discipline (the operatedSystemStateAccess convention):
--   * Head-state = ONE mutable billing_customer row per customer carrying an
--     optimistic-concurrency `version`. Every write verb carries expectedVersion; a
--     mismatch is a terminal fwra.Conflict the billingManager recovers with its
--     re-read -> re-apply loop. A successful write bumps version by exactly one and
--     RETURNs the new value. RegisterCustomer is the one write that creates the row
--     (version 1).
--   * Dedup-first idempotency = the companion billing_mutation ledger keyed on the
--     caller-supplied idempotency key. A replayed write collapses to the recorded
--     resulting version (no-op success), so a Temporal activity retry after a lost
--     result never double-applies a transition.
--
-- Column <-> contract mapping (billingStateAccess):
--   billing_customer.customer_id          -> Billing.ID
--   billing_customer.version              -> Billing.Version
--   billing_customer (row exists)         -> Billing.Registered
--   billing_customer.gateway_bound        -> Billing.GatewayBound (BindGatewayLive)
--   billing_customer.gateway_customer_ref -> GatewayBinding.GatewayCustomerRef
--   billing_customer.project_id           -> CustomerProfile.ProjectID: the
--                                            customer -> project link the
--                                            DelinquencyScope narrows by
--   billing_customer.terms_*              -> CustomerProfile.Terms / Billing.Terms:
--                                            the committed option's terms the
--                                            customer registered under (B13)
--   billing_cycle (row exists)            -> BillingCycle.Settled (SettleCycle
--                                            creates it, ResettleCycle corrects it)
--   billing_cycle.net_* / directive /
--   declined / escalated / attempts /
--   sweeps_aged                           -> BillingCycle.Outcome (the settlement
--                                            and its charge's retry history)
--
-- A customer is PERSISTENTLY DELINQUENT while any of its settled cycles carries an
-- escalated outcome (a declined charge with no retry left — the invoice unpaid);
-- a ResettleCycle that records the cycle paid takes it out of the set. A cycle whose
-- charge stands DECLINED and is not escalated awaits the shortfall sweep's retry.

CREATE TABLE IF NOT EXISTS billing_customer (
    customer_id          uuid        PRIMARY KEY,
    version              bigint      NOT NULL CHECK (version > 0),
    project_id           text        NOT NULL DEFAULT '',
    gateway_bound        boolean     NOT NULL DEFAULT false,
    gateway_customer_ref text        NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- ReadPersistentlyDelinquentCustomers with a project scope.
CREATE INDEX IF NOT EXISTS billing_customer_project_idx
    ON billing_customer (project_id);

CREATE TABLE IF NOT EXISTS billing_cycle (
    customer_id     uuid        NOT NULL REFERENCES billing_customer (customer_id),
    cycle           text        NOT NULL,
    net_minor_units bigint      NOT NULL,
    net_currency    text        NOT NULL,
    directive       smallint    NOT NULL,
    escalated       boolean     NOT NULL,
    settled_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (customer_id, cycle)
);

-- ReadPersistentlyDelinquentCustomers: only escalated cycles participate.
-- Columns added since the first schema (2026-10-09, DCT §12 B13 and the retry
-- history): reconcile a database the earlier schema created.
ALTER TABLE billing_customer ADD COLUMN IF NOT EXISTS terms_compute_cost_kind    smallint         NOT NULL DEFAULT 0;
ALTER TABLE billing_customer ADD COLUMN IF NOT EXISTS terms_compute_markup_pct   double precision NOT NULL DEFAULT 0;
ALTER TABLE billing_customer ADD COLUMN IF NOT EXISTS terms_schedule_kind        smallint         NOT NULL DEFAULT 0;
ALTER TABLE billing_customer ADD COLUMN IF NOT EXISTS terms_currency             text             NOT NULL DEFAULT '';
ALTER TABLE billing_cycle    ADD COLUMN IF NOT EXISTS declined                   boolean          NOT NULL DEFAULT false;
ALTER TABLE billing_cycle    ADD COLUMN IF NOT EXISTS attempts                   bigint           NOT NULL DEFAULT 0;
ALTER TABLE billing_cycle    ADD COLUMN IF NOT EXISTS sweeps_aged                bigint           NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS billing_cycle_declined_idx
    ON billing_cycle (customer_id)
    WHERE declined AND NOT escalated;

CREATE INDEX IF NOT EXISTS billing_cycle_escalated_idx
    ON billing_cycle (customer_id)
    WHERE escalated;

-- Dedup-first idempotency ledger: each successful write records its caller key with
-- the op that applied it, the customer it wrote and the resulting version; a replayed
-- write (same key, same op, same customer) reads it back, and a key reused by another
-- op or for another customer is refused as ContractMisuse.
CREATE TABLE IF NOT EXISTS billing_mutation (
    idempotency_key   text        PRIMARY KEY,
    op                text        NOT NULL,
    customer_id       uuid        NOT NULL,
    resulting_version bigint      NOT NULL,
    applied_at        timestamptz NOT NULL DEFAULT now()
);
