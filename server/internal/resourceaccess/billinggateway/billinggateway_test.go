package billinggateway

// Internal package tests — access to unexported stripeGatewayClient interface
// and newGatewayWithClient constructor. These are unit tests of the Gateway's
// pre-condition validation and response-mapping logic; no network access, no
// containers, no -short guard.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// ---------------------------------------------------------------------------
// ContractMisuse — ChargeUser pre-conditions.
// ---------------------------------------------------------------------------

func TestChargeUser_ContractMisuse_ZeroCustomer(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.Nil,
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 100, Currency: "USD"},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestChargeUser_ContractMisuse_EmptyKey(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 100, Currency: "USD"},
		Key:        "",
	})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestChargeUser_ContractMisuse_ZeroAmount(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 0, Currency: "USD"},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestChargeUser_ContractMisuse_NegativeAmount(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: -50, Currency: "USD"},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestChargeUser_ContractMisuse_EmptyCurrency(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 100, Currency: ""},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---------------------------------------------------------------------------
// ContractMisuse — ValidateStoredInstrument pre-conditions.
// ---------------------------------------------------------------------------

func TestValidateStoredInstrument_ContractMisuse_ZeroCustomer(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{})
	_, err := gw.ValidateStoredInstrument(ctx(), ValidateStoredInstrumentRequest{
		CustomerID: uuid.Nil,
		Key:        "validate:abc:1",
	})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestValidateStoredInstrument_ContractMisuse_EmptyKey(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{})
	_, err := gw.ValidateStoredInstrument(ctx(), ValidateStoredInstrumentRequest{
		CustomerID: uuid.New(),
		Key:        "",
	})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---------------------------------------------------------------------------
// Happy path — ChargeUser returns GatewayCharge on succeeded PaymentIntent.
// ---------------------------------------------------------------------------

func TestChargeUser_HappyPath(t *testing.T) {
	pi := jsonPaymentIntent("pi_test_123", "succeeded", 1299, "usd")
	gw := newGatewayWithClient(fakeClient{response: pi})

	charge, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 1299, Currency: "USD"},
		Key:        "wf1:act1",
	})
	if err != nil {
		t.Fatalf("ChargeUser: unexpected error: %v", err)
	}
	if charge.Status != ChargeSucceeded {
		t.Fatalf("expected ChargeSucceeded, got %v", charge.Status)
	}
	if charge.ChargeID == "" {
		t.Fatal("ChargeID must not be empty")
	}
	if charge.Amount.MinorUnits != 1299 {
		t.Fatalf("Amount.MinorUnits: want 1299, got %d", charge.Amount.MinorUnits)
	}
	if charge.Amount.Currency != "USD" {
		t.Fatalf("Amount.Currency: want USD, got %s", charge.Amount.Currency)
	}
}

// ---------------------------------------------------------------------------
// Happy path — ValidateStoredInstrument returns InstrumentValidation on success.
// ---------------------------------------------------------------------------

func TestValidateStoredInstrument_HappyPath(t *testing.T) {
	si := jsonSetupIntent("seti_test_456", "succeeded")
	gw := newGatewayWithClient(fakeClient{response: si})

	iv, err := gw.ValidateStoredInstrument(ctx(), ValidateStoredInstrumentRequest{
		CustomerID: uuid.New(),
		Key:        "validate:cust-abc:1",
	})
	if err != nil {
		t.Fatalf("ValidateStoredInstrument: unexpected error: %v", err)
	}
	if iv.Status != ChargeSucceeded {
		t.Fatalf("expected ChargeSucceeded, got %v", iv.Status)
	}
	if iv.AuthorizationID == "" {
		t.Fatal("AuthorizationID must not be empty")
	}
}

// ---------------------------------------------------------------------------
// non-succeeded status on a PaymentIntent maps to ContentPolicy.
// ---------------------------------------------------------------------------

func TestChargeUser_RequiresAction_ContentPolicy(t *testing.T) {
	pi := jsonPaymentIntent("pi_test_999", "requires_action", 500, "usd")
	gw := newGatewayWithClient(fakeClient{response: pi})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 500, Currency: "USD"},
		Key:        "wf1:act2",
	})
	assertKind(t, err, fwra.ContentPolicy)
}

// ---------------------------------------------------------------------------
// Error mapping — card decline maps to ContentPolicy (terminal).
// ---------------------------------------------------------------------------

func TestChargeUser_CardDecline_ContentPolicy(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{err: fwra.New(fwra.ContentPolicy, "billinggateway: card declined")})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 500, Currency: "USD"},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.ContentPolicy)
	if isRetryable(err) {
		t.Fatal("ContentPolicy must be terminal (Retryable=false)")
	}
}

// ---------------------------------------------------------------------------
// Error mapping — no payment method maps to NotFound.
// ---------------------------------------------------------------------------

func TestValidateStoredInstrument_NoInstrument_NotFound(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{err: fwra.New(fwra.NotFound, "billinggateway: no such customer")})
	_, err := gw.ValidateStoredInstrument(ctx(), ValidateStoredInstrumentRequest{
		CustomerID: uuid.New(),
		Key:        "validate:cust-abc:1",
	})
	assertKind(t, err, fwra.NotFound)
}

// ---------------------------------------------------------------------------
// Error mapping — Auth error (401).
// ---------------------------------------------------------------------------

func TestChargeUser_Auth(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{err: fwra.New(fwra.Auth, "billinggateway: No API key")})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 100, Currency: "USD"},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.Auth)
}

// ---------------------------------------------------------------------------
// Error mapping — idempotency conflict maps to Conflict (terminal).
// ---------------------------------------------------------------------------

func TestChargeUser_IdempotencyConflict(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{err: fwra.New(fwra.Conflict, "billinggateway: idempotency conflict")})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 100, Currency: "USD"},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.Conflict)
}

// ---------------------------------------------------------------------------
// Error mapping — RateLimited (retryable).
// ---------------------------------------------------------------------------

func TestChargeUser_RateLimited(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{err: fwra.New(fwra.RateLimited, "billinggateway: rate limited")})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 100, Currency: "USD"},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.RateLimited)
	if !isRetryable(err) {
		t.Fatal("RateLimited must be retryable")
	}
}

// ---------------------------------------------------------------------------
// Error mapping — Transient (retryable).
// ---------------------------------------------------------------------------

func TestChargeUser_Transient(t *testing.T) {
	gw := newGatewayWithClient(fakeClient{err: fwra.New(fwra.Transient, "billinggateway: stripe HTTP 500")})
	_, err := gw.ChargeUser(ctx(), ChargeRequest{
		CustomerID: uuid.New(),
		PeriodID:   "2026-06",
		Amount:     Money{MinorUnits: 100, Currency: "USD"},
		Key:        "wf1:act1",
	})
	assertKind(t, err, fwra.Transient)
	if !isRetryable(err) {
		t.Fatal("Transient must be retryable")
	}
}

// ---------------------------------------------------------------------------
// NewGateway config validation.
// ---------------------------------------------------------------------------

func TestNewGateway_EmptySecretKey_ContractMisuse(t *testing.T) {
	_, err := NewGateway(StripeConfig{SecretKey: ""})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestNewGateway_ValidConfig_OK(t *testing.T) {
	gw, err := NewGateway(StripeConfig{SecretKey: "sk_test_abc"})
	if err != nil {
		t.Fatalf("NewGateway: unexpected error: %v", err)
	}
	if gw == nil {
		t.Fatal("NewGateway must return non-nil Gateway")
	}
}

// ---------------------------------------------------------------------------
// mapStripeHTTPError unit tests — verify the HTTP-to-fwra mapping directly.
// ---------------------------------------------------------------------------

func TestMapStripeHTTPError_RateLimit(t *testing.T) {
	err := mapStripeHTTPError(429, stripeErrBody("rate_limit_error", "", "Too many requests"))
	assertKind(t, err, fwra.RateLimited)
	if !err.Retryable {
		t.Fatal("RateLimited must be retryable")
	}
}

func TestMapStripeHTTPError_ServerError(t *testing.T) {
	err := mapStripeHTTPError(500, stripeErrBody("api_error", "", "Internal server error"))
	assertKind(t, err, fwra.Transient)
	if !err.Retryable {
		t.Fatal("5xx must be retryable")
	}
}

func TestMapStripeHTTPError_AuthError(t *testing.T) {
	err := mapStripeHTTPError(401, stripeErrBody("authentication_error", "", "No API key"))
	assertKind(t, err, fwra.Auth)
}

func TestMapStripeHTTPError_IdempotencyConflict(t *testing.T) {
	err := mapStripeHTTPError(400, stripeErrBody("idempotency_error", "", "Keys in conflict"))
	assertKind(t, err, fwra.Conflict)
}

func TestMapStripeHTTPError_CardError(t *testing.T) {
	err := mapStripeHTTPError(402, stripeErrBody("card_error", "card_declined", "Your card was declined."))
	assertKind(t, err, fwra.ContentPolicy)
	if err.Retryable {
		t.Fatal("ContentPolicy must be terminal")
	}
}

func TestMapStripeHTTPError_NoSuchCustomer(t *testing.T) {
	err := mapStripeHTTPError(400, stripeErrBody("invalid_request_error", "resource_missing", "No such customer"))
	assertKind(t, err, fwra.NotFound)
}

func TestMapStripeHTTPError_InvalidParam(t *testing.T) {
	err := mapStripeHTTPError(400, stripeErrBody("invalid_request_error", "parameter_missing", "amount is required"))
	assertKind(t, err, fwra.ContractMisuse)
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

func ctx() context.Context { return context.Background() }

// fakeClient is a stripeGatewayClient that returns a fixed response or error.
type fakeClient struct {
	response json.RawMessage
	err      error
}

func (f fakeClient) post(_ context.Context, _ string, _ url.Values, _ string) (json.RawMessage, error) {
	return f.response, f.err
}

func assertKind(t *testing.T, err error, want fwra.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %s, got nil", want)
	}
	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
	if e.Kind != want {
		t.Fatalf("kind: want %s, got %s (detail: %s)", want, e.Kind, e.Detail)
	}
}

func isRetryable(err error) bool {
	var e *fwra.Error
	if errors.As(err, &e) {
		return e.Retryable
	}
	return false
}

func jsonPaymentIntent(id, status string, amount int64, currency string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{
		"id": id, "status": status, "amount": amount, "currency": currency,
	})
	return json.RawMessage(b)
}

func jsonSetupIntent(id, status string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{"id": id, "status": status})
	return json.RawMessage(b)
}

func stripeErrBody(errType, code, message string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"type":    errType,
			"code":    code,
			"message": message,
		},
	})
	return b
}
