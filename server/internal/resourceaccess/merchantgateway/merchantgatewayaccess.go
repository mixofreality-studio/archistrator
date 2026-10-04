// Package merchantgateway is the merchantGatewayAccess component of the
// ResourceAccess layer: the gateway the billing rail validates a customer's stored
// payment instrument against and charges a cycle's shortfall through.
//
// Its one variant is Postgres-backed on both deployment profiles (deterministic
// component testing, spec §12 ruling B5: the local profile uses the same Postgres).
// It is the platform's own gateway ledger: it records which customers hold a validated
// instrument and every request with the outcome it was given, and it moves no money.
// A card-network variant (docs/billing-setup.md) is a further variant of this contract
// selected per profile, not a change to this one.
package merchantgateway

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

// postgresMerchantGatewayAccess is the Postgres-backed MerchantGatewayAccess. It is
// UNEXPORTED — the package's public surface is the generated MerchantGatewayAccess
// interface, its models and the generated NewPostgresMerchantGatewayAccess
// constructor. It imports no Temporal (layer rule): the idempotency key arrives as an
// ordinary parameter on each request.
type postgresMerchantGatewayAccess struct {
	pool *pgxpool.Pool
}

var _ MerchantGatewayAccess = (*postgresMerchantGatewayAccess)(nil)

// schemaDDL is the idempotent gateway-ledger migration (schema.sql in this package IS
// the migration), applied by the constructor on every boot.
//
//go:embed schema.sql
var schemaDDL string

// newPostgresMerchantGatewayAccess is the hand-written builder behind the generated
// NewPostgresMerchantGatewayAccess: it applies the embedded, idempotent schema and
// returns the gateway over the pool.
func newPostgresMerchantGatewayAccess(ctx context.Context, pool *pgxpool.Pool) (MerchantGatewayAccess, error) {
	if pool == nil {
		return nil, fwra.New(fwra.ContractMisuse, "merchantgateway.NewPostgresMerchantGatewayAccess: nil pool")
	}
	if _, err := pool.Exec(ctx, schemaDDL); err != nil {
		return nil, fwra.Wrap(fwra.Infrastructure, err, "merchantgateway.NewPostgresMerchantGatewayAccess: apply schema")
	}
	return &postgresMerchantGatewayAccess{pool: pool}, nil
}

// requestKind is the merchant_gateway_request.kind discriminator.
type requestKind int16

const (
	requestValidate requestKind = 1
	requestCharge   requestKind = 2
)

// gatewayRequest is one request as the ledger records it: what was asked, of whom,
// and for how much.
type gatewayRequest struct {
	kind       requestKind
	customerID uuid.UUID
	amount     Money
}

const insertInstrumentSQL = `
INSERT INTO merchant_instrument (customer_id)
VALUES ($1)
ON CONFLICT (customer_id) DO NOTHING`

// ValidateStoredInstrument runs the zero-amount validation of the customer's stored
// instrument and records the customer as holding a validated one. Idempotent on the
// key.
func (s *postgresMerchantGatewayAccess) ValidateStoredInstrument(rc fwra.Context, customerID uuid.UUID, idempotencyKey string) error {
	const op = "merchantgateway.ValidateStoredInstrument"
	req := gatewayRequest{kind: requestValidate, customerID: customerID}
	return s.request(rc, op, idempotencyKey, req, func(ctx context.Context, tx pgx.Tx) (bool, error) {
		if _, err := tx.Exec(ctx, insertInstrumentSQL, customerID); err != nil {
			return false, fwpg.MapError(err, op+": record instrument")
		}
		return false, nil
	})
}

const instrumentExistsSQL = `SELECT true FROM merchant_instrument WHERE customer_id = $1`

// ChargeCustomer charges amount to the customer's validated stored instrument. A
// customer with no validated instrument is declined — a terminal fwra.Auth, the kind
// the billing rail reads as a decline. The outcome is recorded under the key: a replay
// of the key returns it again, a decline included.
func (s *postgresMerchantGatewayAccess) ChargeCustomer(rc fwra.Context, customerID uuid.UUID, amount Money, idempotencyKey string) error {
	const op = "merchantgateway.ChargeCustomer"
	if amount.MinorUnits <= 0 {
		return fwra.New(fwra.ContractMisuse, op+": amount must be positive")
	}
	if amount.Currency == "" {
		return fwra.New(fwra.ContractMisuse, op+": empty currency")
	}
	req := gatewayRequest{kind: requestCharge, customerID: customerID, amount: amount}
	return s.request(rc, op, idempotencyKey, req, func(ctx context.Context, tx pgx.Tx) (bool, error) {
		var validated bool
		err := tx.QueryRow(ctx, instrumentExistsSQL, customerID).Scan(&validated)
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		if err != nil {
			return false, fwpg.MapError(err, op+": instrument")
		}
		return false, nil
	})
}

const selectRequestSQL = `
SELECT kind, customer_id, amount_minor_units, currency, declined
FROM merchant_gateway_request
WHERE idempotency_key = $1`

const insertRequestSQL = `
INSERT INTO merchant_gateway_request (idempotency_key, kind, customer_id, amount_minor_units, currency, declined)
VALUES ($1, $2, $3, $4, $5, $6)`

// request runs one gateway request in a single transaction, dedup-first: a key
// already recorded replays its recorded outcome (or is ContractMisuse when it was
// recorded for a different request); otherwise decide runs, its outcome is recorded
// under the key and the transaction commits. A declined outcome is returned as
// fwra.Auth AFTER the commit, so the decline itself is what a replay finds.
func (s *postgresMerchantGatewayAccess) request(
	rc fwra.Context,
	op string,
	key string,
	req gatewayRequest,
	decide func(ctx context.Context, tx pgx.Tx) (declined bool, err error),
) error {
	ctx := rc.Context
	if req.customerID == uuid.Nil {
		return fwra.New(fwra.ContractMisuse, op+": zero customerID")
	}
	if key == "" {
		return fwra.New(fwra.ContractMisuse, op+": empty idempotencyKey")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fwpg.MapError(err, op)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	declined, replayed, err := recordedOutcome(ctx, tx, op, key, req)
	if err != nil {
		return err
	}
	if !replayed {
		if declined, err = decide(ctx, tx); err != nil {
			return err
		}
		if _, ierr := tx.Exec(ctx, insertRequestSQL, key, int16(req.kind), req.customerID,
			req.amount.MinorUnits, req.amount.Currency, declined); ierr != nil {
			return fwpg.MapError(ierr, op+": record request")
		}
	}
	if cerr := tx.Commit(ctx); cerr != nil {
		return fwpg.MapError(cerr, op+": commit")
	}
	if declined {
		return fwra.New(fwra.Auth, op+": declined: the customer holds no validated stored instrument")
	}
	return nil
}

// recordedOutcome looks the key up in the request ledger: replayed=false when it was
// never used; otherwise the recorded decline, provided the key was recorded for this
// very request.
func recordedOutcome(ctx context.Context, tx pgx.Tx, op, key string, req gatewayRequest) (declined, replayed bool, err error) {
	var (
		kind int16
		prev gatewayRequest
	)
	err = tx.QueryRow(ctx, selectRequestSQL, key).
		Scan(&kind, &prev.customerID, &prev.amount.MinorUnits, &prev.amount.Currency, &declined)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fwpg.MapError(err, op+": dedup")
	}
	prev.kind = requestKind(kind)
	if prev != req {
		return false, false, fwra.New(fwra.ContractMisuse, op+": idempotencyKey already used for a different request")
	}
	return declined, true, nil
}
