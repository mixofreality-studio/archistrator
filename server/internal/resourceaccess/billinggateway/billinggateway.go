// Package billinggateway is the billingGatewayAccess component of the aiarch
// server's ResourceAccess layer — the GATEWAY-OPAQUE port over the billing
// gateway (Stripe as an ordinary vendor-to-customer charge;
// billingGatewayAccess FROZEN contract, r2 APPROVE-AND-FREEZE 2026-06-10).
//
// Two atomic operations:
//
//   - ChargeUser — pull aiarch's own service-invoice amount from the user's
//     stored charge instrument. Activity-wrapped write; idempotent on
//     req.Key via the gateway-native Idempotency-Key header. Hard decline
//     surfaces as terminal fwra.ContentPolicy.
//
//   - ValidateStoredInstrument — zero-amount-authorize the user's stored charge
//     instrument at registration (before any billing period exists). Activity-
//     wrapped write; idempotent on req.Key via the gateway-native
//     Idempotency-Key header. Failed validation surfaces as terminal
//     fwra.ContentPolicy; absent instrument as fwra.NotFound.
//
// THE LOAD-BEARING LAYER RULES ([[the-method-layers]]):
//
//   - GATEWAY OPACITY. The public surface carries ZERO Stripe wire/data lexemes
//     (PaymentIntent, SetupIntent, charge id, pi_…, seti_…, payment_method,
//     Stripe customer id, /v1/payment_intents, error.code). The opaque value
//     types (ChargeID, AuthorizationID) wrap the gateway correlation tokens;
//     callers never parse them. ALL Stripe vocabulary lives inside stripe.go —
//     never on the port.
//
//   - NO RA→RA CALL. This component imports and calls no other ResourceAccess.
//
//   - NO TEMPORAL. Every method is plain Go; the calling Manager wraps each
//     call in a Temporal Activity it owns and chooses retry/timeout there.
//     Errors carry an accurate fwra.Retryable flag (seeded from kind); this
//     component never reads Temporal context.
//
//   - IDEMPOTENCY via gateway-native Idempotency-Key header: the
//     caller-supplied GatewayRequestKey is forwarded verbatim to the gateway.
//     Same key → gateway dedups → exactly-once charge. Different payload on
//     the same key → fwra.Conflict (derivation bug, never retried).
package billinggateway

import (
	"context"

	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// BillingGatewayAccess is the gateway-opaque port over the billing gateway
// (billingGatewayAccess FROZEN contract §2). Two atomic write verbs, each
// importing no Temporal; each a facet of "aiarch collects money from the user
// for its own service invoice".
type BillingGatewayAccess interface {
	// ChargeUser pulls aiarch's service-invoice amount from the user's stored
	// charge instrument. Idempotent on req.Key. Hard decline (card_declined /
	// equivalent) returns terminal fwra.ContentPolicy for the Manager to route
	// to interventionEngine.DecideOnBillingFailure.
	ChargeUser(ctx context.Context, req ChargeRequest) (GatewayCharge, error)

	// ValidateStoredInstrument zero-amount-authorizes the user's stored charge
	// instrument at registration. Idempotent on req.Key. Failed validation
	// returns terminal fwra.ContentPolicy; absent instrument fwra.NotFound.
	ValidateStoredInstrument(ctx context.Context, req ValidateStoredInstrumentRequest) (InstrumentValidation, error)
}

// ---------------------------------------------------------------------------
// Data contracts (billingGatewayAccess FROZEN contract §3).
// ---------------------------------------------------------------------------

// CustomerID is the uuid.UUID identity of a registered customer with a
// validated stored charge instrument. Provider-opaque at this boundary: maps
// to a Stripe Customer id INSIDE the gateway seam.
type CustomerID = uuid.UUID

// PeriodID is the billing-period context for ChargeUser (audit + Manager key
// derivation). Provider-opaque string.
type PeriodID = string

// Money is a positive integer minor-units amount with an ISO-4217 currency
// code. Direction is the verb (ChargeUser always pulls, never pays out);
// negative or zero MinorUnits on a ChargeRequest is fwra.ContractMisuse.
type Money struct {
	MinorUnits int64  // positive minor units (e.g. 1299 == USD $12.99)
	Currency   string // ISO-4217 (e.g. "USD")
}

// GatewayRequestKey is the caller-supplied deterministic dedup token forwarded
// to the gateway as the native Idempotency-Key header. Alias of
// fwra.IdempotencyKey; the Manager derives it as "${workflowId}:${activityId}".
// An empty Key is fwra.ContractMisuse.
type GatewayRequestKey = fwra.IdempotencyKey

// ChargeRequest is the input to ChargeUser.
type ChargeRequest struct {
	// CustomerID identifies the customer whose stored instrument is charged.
	CustomerID CustomerID
	// PeriodID is the billing-period context for audit and Manager key derivation.
	PeriodID PeriodID
	// Amount is the positive service-invoice magnitude to charge (minor units +
	// ISO-4217 currency). Non-positive MinorUnits or empty Currency is
	// fwra.ContractMisuse.
	Amount Money
	// Key is the caller-supplied deterministic gateway request key placed into
	// the gateway-native Idempotency-Key header.
	Key GatewayRequestKey
}

// ValidateStoredInstrumentRequest is the input to ValidateStoredInstrument.
type ValidateStoredInstrumentRequest struct {
	// CustomerID identifies the customer whose stored instrument is validated.
	CustomerID CustomerID
	// Key is the caller-supplied deterministic gateway request key placed into
	// the gateway-native Idempotency-Key header.
	Key GatewayRequestKey
}

// ChargeStatus is the outcome enum for both gateway operations. Declines are
// terminal fwra.Error (ContentPolicy), not a status value; this enum contains
// only the success state.
type ChargeStatus int

const (
	// ChargeSucceeded is the only status value: the charge / zero-amount
	// authorization succeeded.
	ChargeSucceeded ChargeStatus = iota
)

// GatewayCharge is the output of ChargeUser. An opaque gateway correlation
// token (ChargeID) plus the echoed amount; carries ZERO Stripe lexemes.
type GatewayCharge struct {
	// ChargeID is an opaque gateway correlation token (not a Stripe id).
	ChargeID string
	// Status is ChargeSucceeded; declines are terminal fwra.Error.
	Status ChargeStatus
	// Amount is the magnitude actually charged; echoes the request (or the
	// original amount on a deduped replay).
	Amount Money
}

// InstrumentValidation is the output of ValidateStoredInstrument. An opaque
// zero-amount authorization id; carries ZERO Stripe lexemes.
type InstrumentValidation struct {
	// AuthorizationID is an opaque gateway zero-amount authorization id (not a
	// Stripe id).
	AuthorizationID string
	// Status is ChargeSucceeded (zero-amount auth succeeded); failed validation
	// is terminal fwra.Error.
	Status ChargeStatus
}

// Error is the shared ResourceAccess error model (framework-go), re-exported
// as an alias so this component reads in its own terms while every RA shares
// one fixed enum. Construct with fwra.New / fwra.Wrap.
//
// Kinds used by this component:
//
//   - Transient      (retryable): network blip / gateway 5xx
//   - RateLimited    (retryable): 429
//   - Infrastructure (retryable): persistent gateway infrastructure error
//   - ContentPolicy  (terminal): hard card decline — Manager routes to
//     interventionEngine.DecideOnBillingFailure or registration-failure path
//   - Auth           (terminal): aiarch credential invalid
//   - NotFound       (terminal): no addressable instrument for the customer
//   - ContractMisuse (terminal): non-positive Amount, empty Currency, empty Key,
//     zero CustomerID
//   - Conflict       (terminal): same Key with different payload (derivation bug)
type Error = fwra.Error
