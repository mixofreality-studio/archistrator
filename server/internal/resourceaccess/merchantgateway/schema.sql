-- merchantGatewayAccess schema: the platform's own gateway ledger.
--
-- This file IS the migration: it is embedded into the package and applied by
-- NewPostgresMerchantGatewayAccess on every boot, so every statement is idempotent
-- (CREATE … IF NOT EXISTS). Both deployment profiles run it against the same
-- Postgres the billing, operations and usage stores use.
--
-- What it records:
--   * merchant_instrument — one row per customer whose stored payment instrument
--     passed the zero-amount validation (ValidateStoredInstrument). A charge against
--     a customer with no validated instrument is DECLINED.
--   * merchant_gateway_request — every gateway request, keyed on the caller's
--     idempotency key, with the outcome the gateway gave it. A request replayed
--     under the same key returns the RECORDED outcome — a declined charge stays
--     declined under its key, exactly as a card gateway replays a cached response —
--     so a retry after a decline needs a new key. Reusing a key for a different
--     request (another kind, customer or amount) is a caller error.

CREATE TABLE IF NOT EXISTS merchant_instrument (
    customer_id  uuid        PRIMARY KEY,
    validated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS merchant_gateway_request (
    idempotency_key    text        PRIMARY KEY,
    kind               smallint    NOT NULL, -- 1 validate stored instrument, 2 charge
    customer_id        uuid        NOT NULL,
    amount_minor_units bigint      NOT NULL DEFAULT 0,
    currency           text        NOT NULL DEFAULT '',
    declined           boolean     NOT NULL,
    requested_at       timestamptz NOT NULL DEFAULT now()
);
