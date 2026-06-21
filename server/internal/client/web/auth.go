package web

import (
	"context"
	"net/http"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// This file is the auth MIDDLEWARE selection for the Client edge
// (operational-concepts.md §1 topology: External → Envoy Gateway [TLS] →
// in-process Client).
//
// TRUST MODEL: Envoy forwards the Authorization header unchanged and the server
// validates the bearer ACCESS token itself (GTD-parity), via the framework
// security [security.Validator] + [security.Middleware]. A valid token yields a
// typed [security.SecurityPrincipal] on the request context; an invalid/absent
// token is rejected with 401 before any handler runs. (This REPLACES the former
// "trust the edge, read x-aiarch-claim-* headers" model.)
//
// DEV MODE: when running WITHOUT an IdP in front (local `go run`, systemtests),
// there is no token to validate. A clearly-gated dev flag injects a dev principal
// directly so the server is locally runnable end-to-end. It is OFF by default and
// MUST be off in any real deployment.

// DevConfig configures the clearly-gated dev-mode principal injection.
type DevConfig struct {
	// Enabled turns on dev-mode principal injection. MUST be false in any
	// IdP-fronted deployment.
	Enabled bool
	// Principal is the dev identity injected on every request when Enabled.
	Principal security.SecurityPrincipal
}

// AuthMiddleware returns the middleware mounted in front of the authenticated API
// surface:
//
//   - dev.Enabled: inject dev.Principal on every request (no token required) — the
//     local/systemtest happy path.
//   - otherwise: validate the bearer access token with validator via
//     [security.Middleware]. A nil validator denies every request (401) — used
//     where no IdP is configured (e.g. the systemtests auth-boundary check), so
//     the unauthenticated surface still rejects rather than failing to boot.
func AuthMiddleware(dev DevConfig, validator security.Validator) func(http.Handler) http.Handler {
	if dev.Enabled {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(security.WithPrincipal(r.Context(), dev.Principal)))
			})
		}
	}
	if validator == nil {
		validator = denyAllValidator{}
	}
	return security.Middleware(validator)
}

// denyAllValidator rejects every token. It backs the no-IdP-configured,
// non-dev deployment so the auth boundary returns 401 instead of the server
// failing to construct a validator at boot.
type denyAllValidator struct{}

func (denyAllValidator) ValidateAccessToken(context.Context, string) (security.SecurityPrincipal, error) {
	return security.SecurityPrincipal{}, security.NewError(security.ErrUnauthenticated)
}
