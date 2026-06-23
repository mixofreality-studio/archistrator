package billinggateway

// stripe.go is the concrete Stripe-backed Gateway — the ONLY place this RA
// speaks Stripe. All Stripe wire vocabulary (PaymentIntent, SetupIntent,
// pi_…, seti_…, /v1/payment_intents, /v1/setup_intents, card_declined,
// Stripe customer id) is confined here; none of it leaks back across the
// BillingGatewayAccess port.
//
// AUTH: the Gateway holds the Stripe secret key and presents it as
// "Authorization: Bearer <key>" on every call. The key is NEVER surfaced on
// the port and is NEVER forwarded to any other seam.
//
// IDEMPOTENCY: the caller-supplied GatewayRequestKey is forwarded verbatim
// as the HTTP "Idempotency-Key" header on all write calls. Stripe's 24 h dedup
// window absorbs replays; a same-key-different-payload conflict returns 400
// with error.type "idempotency_error" → mapped to fwra.Conflict.
//
// ERROR MAPPING (contract ErrorModel):
//   - network / DNS / TLS failure             → fwra.Transient (retryable)
//   - HTTP 429 (rate_limit_error)             → fwra.RateLimited (retryable)
//   - HTTP 5xx                                → fwra.Transient (retryable)
//   - HTTP 401 (authentication_error)         → fwra.Auth (terminal)
//   - HTTP 400 idempotency_error              → fwra.Conflict (terminal)
//   - card_error (declined)                   → fwra.ContentPolicy (terminal)
//   - invalid_request_error no such customer  → fwra.NotFound (terminal)
//   - other invalid_request_error             → fwra.ContractMisuse (terminal)

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

const stripeAPIBase = "https://api.stripe.com"

// StripeConfig holds the Stripe binding the composition root supplies.
type StripeConfig struct {
	// SecretKey is the Stripe secret API key (sk_live_… or sk_test_…).
	SecretKey string
	// APIBaseURL overrides the Stripe API root (empty == stripeAPIBase; a test
	// fake or staging endpoint overrides it).
	APIBaseURL string
}

// stripeGatewayClient is the internal seam over the Stripe REST API — declared
// as an interface so the concrete HTTP transport can be swapped for a test fake
// without changing the Gateway's logic.
type stripeGatewayClient interface {
	// post sends a form-encoded POST to the given Stripe endpoint, forwarding
	// idempotencyKey as the Idempotency-Key header. It returns the decoded
	// Stripe JSON body on 2xx, or a stripeError on any failure.
	post(ctx context.Context, path string, params url.Values, idempotencyKey string) (json.RawMessage, error)
}

// stripeHTTPClient is the concrete stripeGatewayClient over net/http.
type stripeHTTPClient struct {
	secretKey string
	baseURL   string
	http      *http.Client
}

func (c *stripeHTTPClient) post(ctx context.Context, path string, params url.Values, idempotencyKey string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+path, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fwra.Wrap(fwra.Transient, err, "billinggateway: build request")
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Stripe-Version", "2024-06-20")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fwra.Wrap(fwra.Transient, err, "billinggateway: stripe request failed")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fwra.Wrap(fwra.Transient, err, "billinggateway: read response body")
	}

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return json.RawMessage(body), nil
	}
	return nil, mapStripeHTTPError(resp.StatusCode, body)
}

// stripeError is the Stripe error envelope returned on non-2xx responses.
type stripeError struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Param   string `json:"param"`
	} `json:"error"`
}

// mapStripeHTTPError translates a Stripe non-2xx HTTP response into an fwra.Error.
func mapStripeHTTPError(statusCode int, body []byte) *fwra.Error {
	var se stripeError
	_ = json.Unmarshal(body, &se)
	detail := se.Error.Message
	if detail == "" {
		detail = fmt.Sprintf("stripe HTTP %d", statusCode)
	}

	switch {
	case statusCode == http.StatusTooManyRequests:
		return fwra.New(fwra.RateLimited, "billinggateway: "+detail)

	case statusCode >= 500:
		return fwra.New(fwra.Transient, "billinggateway: "+detail)

	case statusCode == http.StatusUnauthorized:
		return fwra.New(fwra.Auth, "billinggateway: "+detail)

	case se.Error.Type == "idempotency_error":
		return fwra.New(fwra.Conflict, "billinggateway: idempotency conflict — same key, different payload: "+detail)

	case se.Error.Type == "card_error":
		return fwra.New(fwra.ContentPolicy, "billinggateway: card declined: "+detail)

	case se.Error.Type == "invalid_request_error":
		if isNoSuchCustomer(se.Error.Code, se.Error.Param, detail) {
			return fwra.New(fwra.NotFound, "billinggateway: customer or payment method not found: "+detail)
		}
		return fwra.New(fwra.ContractMisuse, "billinggateway: invalid request: "+detail)

	case se.Error.Type == "authentication_error":
		return fwra.New(fwra.Auth, "billinggateway: "+detail)

	default:
		return fwra.New(fwra.Transient, "billinggateway: "+detail)
	}
}

// isNoSuchCustomer returns true for Stripe errors that indicate the customer
// or their payment method does not exist — mapped to fwra.NotFound.
func isNoSuchCustomer(code, param, detail string) bool {
	switch {
	case code == "resource_missing":
		return true
	case strings.Contains(param, "customer"):
		return true
	case strings.Contains(detail, "No such customer"):
		return true
	case strings.Contains(detail, "no attached payment method"):
		return true
	case strings.Contains(detail, "does not have a default payment method"):
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Gateway — the BillingGatewayAccess implementation.
// ---------------------------------------------------------------------------

// Gateway is the concrete BillingGatewayAccess. It holds a stripeGatewayClient
// seam (the real Stripe HTTP transport in production; a fake in tests).
type Gateway struct {
	client stripeGatewayClient
}

// Compile-time assertion.
var _ BillingGatewayAccess = (*Gateway)(nil)

// NewGateway constructs a production Gateway from the supplied StripeConfig.
// Config is validated eagerly; no network I/O occurs here.
func NewGateway(cfg StripeConfig) (*Gateway, error) {
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fwra.New(fwra.ContractMisuse, "NewGateway: empty SecretKey")
	}
	base := strings.TrimRight(cfg.APIBaseURL, "/")
	if base == "" {
		base = stripeAPIBase
	}
	return &Gateway{
		client: &stripeHTTPClient{
			secretKey: cfg.SecretKey,
			baseURL:   base,
			http:      &http.Client{},
		},
	}, nil
}

// newGatewayWithClient builds a Gateway using the supplied client seam (tests).
func newGatewayWithClient(client stripeGatewayClient) *Gateway {
	return &Gateway{client: client}
}

// ChargeUser pulls aiarch's service-invoice amount from the customer's stored
// payment instrument. Idempotent on req.Key via Stripe's Idempotency-Key header.
// (billingGatewayAccess §2 chargeUser)
func (g *Gateway) ChargeUser(ctx context.Context, req ChargeRequest) (GatewayCharge, error) {
	if err := validateChargeRequest(req); err != nil {
		return GatewayCharge{}, err
	}

	params := url.Values{}
	params.Set("customer", req.CustomerID.String())
	params.Set("amount", fmt.Sprintf("%d", req.Amount.MinorUnits))
	params.Set("currency", strings.ToLower(req.Amount.Currency))
	params.Set("confirm", "true")
	params.Set("off_session", "true")

	body, err := g.client.post(ctx, "/v1/payment_intents", params, string(req.Key))
	if err != nil {
		return GatewayCharge{}, err
	}

	var pi stripePaymentIntent
	if jsonErr := json.Unmarshal(body, &pi); jsonErr != nil {
		return GatewayCharge{}, fwra.Wrap(fwra.Transient, jsonErr, "billinggateway: decode payment_intent response")
	}
	if pi.Status != "succeeded" {
		// A non-declined, non-succeeded status (e.g. "requires_action") is a
		// payment that cannot be collected off-session — treat as a decline.
		return GatewayCharge{}, fwra.New(fwra.ContentPolicy,
			"billinggateway: payment intent not succeeded: status="+pi.Status)
	}

	return GatewayCharge{
		ChargeID: opaqueChargeID(pi.ID),
		Status:   ChargeSucceeded,
		Amount:   Money{MinorUnits: pi.Amount, Currency: strings.ToUpper(pi.Currency)},
	}, nil
}

// ValidateStoredInstrument zero-amount-authorizes the customer's stored payment
// instrument using a Stripe SetupIntent. Idempotent on req.Key.
// (billingGatewayAccess §2 validateStoredInstrument)
func (g *Gateway) ValidateStoredInstrument(ctx context.Context, req ValidateStoredInstrumentRequest) (InstrumentValidation, error) {
	if err := validateStoredInstrumentRequest(req); err != nil {
		return InstrumentValidation{}, err
	}

	params := url.Values{}
	params.Set("customer", req.CustomerID.String())
	params.Set("confirm", "true")
	params.Add("payment_method_types[]", "card")

	body, err := g.client.post(ctx, "/v1/setup_intents", params, string(req.Key))
	if err != nil {
		return InstrumentValidation{}, err
	}

	var si stripeSetupIntent
	if jsonErr := json.Unmarshal(body, &si); jsonErr != nil {
		return InstrumentValidation{}, fwra.Wrap(fwra.Transient, jsonErr, "billinggateway: decode setup_intent response")
	}
	if si.Status != "succeeded" {
		return InstrumentValidation{}, fwra.New(fwra.ContentPolicy,
			"billinggateway: setup intent not succeeded: status="+si.Status)
	}

	return InstrumentValidation{
		AuthorizationID: opaqueAuthorizationID(si.ID),
		Status:          ChargeSucceeded,
	}, nil
}

// ---------------------------------------------------------------------------
// Pre-condition validators (ContractMisuse on invalid inputs).
// ---------------------------------------------------------------------------

func validateChargeRequest(req ChargeRequest) *fwra.Error {
	if req.CustomerID == [16]byte{} {
		return fwra.New(fwra.ContractMisuse, "billinggateway: ChargeRequest.CustomerID is zero")
	}
	if req.Key.IsZero() {
		return fwra.New(fwra.ContractMisuse, "billinggateway: ChargeRequest.Key is empty")
	}
	if req.Amount.MinorUnits <= 0 {
		return fwra.New(fwra.ContractMisuse, "billinggateway: ChargeRequest.Amount.MinorUnits must be positive")
	}
	if strings.TrimSpace(req.Amount.Currency) == "" {
		return fwra.New(fwra.ContractMisuse, "billinggateway: ChargeRequest.Amount.Currency is empty")
	}
	return nil
}

func validateStoredInstrumentRequest(req ValidateStoredInstrumentRequest) *fwra.Error {
	if req.CustomerID == [16]byte{} {
		return fwra.New(fwra.ContractMisuse, "billinggateway: ValidateStoredInstrumentRequest.CustomerID is zero")
	}
	if req.Key.IsZero() {
		return fwra.New(fwra.ContractMisuse, "billinggateway: ValidateStoredInstrumentRequest.Key is empty")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal Stripe response shapes (provider vocabulary, confined here).
// ---------------------------------------------------------------------------

// stripePaymentIntent is the minimal Stripe PaymentIntent shape we decode from
// /v1/payment_intents responses. Only the fields this seam reads are listed;
// the rest of the Stripe response is intentionally discarded.
type stripePaymentIntent struct {
	ID       string `json:"id"`       // pi_… — opaque within this seam; never exposed
	Status   string `json:"status"`   // "succeeded", "requires_payment_method", …
	Amount   int64  `json:"amount"`   // minor units
	Currency string `json:"currency"` // lower-case ISO-4217
}

// stripeSetupIntent is the minimal Stripe SetupIntent shape we decode from
// /v1/setup_intents responses.
type stripeSetupIntent struct {
	ID     string `json:"id"`     // seti_… — opaque within this seam
	Status string `json:"status"` // "succeeded", "requires_confirmation", …
}

// ---------------------------------------------------------------------------
// Opaque correlation token constructors — provider ids stay inside this seam.
// The opaque prefix ensures no caller can interpret the value as a Stripe id.
// ---------------------------------------------------------------------------

// opaqueChargeID wraps a Stripe PaymentIntent id as an opaque correlation token.
func opaqueChargeID(piID string) string {
	return "charge:" + piID
}

// opaqueAuthorizationID wraps a Stripe SetupIntent id as an opaque correlation
// token.
func opaqueAuthorizationID(siID string) string {
	return "auth:" + siID
}
