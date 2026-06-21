package main

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// authenticatedOnlyPDP is the INTERIM authorization policy: every request that
// reached a handler has already passed JWT validation (the auth middleware
// rejects unauthenticated API calls with 401 before any handler runs), so this
// PDP permits any authenticated principal to take any action on any resource.
//
// It deliberately keeps the [security.PolicyDecisionPoint] seam in place so the
// authorizeProject() pre-step in the webClient stays wired. Authentication is
// the only gate for now; fine-grained authorization is a no-op until Cedar.
//
// TODO(cedar): replace this whole seam with the Cedar PDP (per
// designs/aiarch/project/planning-assumptions.md §Security — "Cedar-style
// authorization"). Drop-in via security.WithPolicyDecisionPoint; no handler or
// Manager change required.
type authenticatedOnlyPDP struct{}

// Decide permits unconditionally. The principal is already authenticated (the
// middleware validated its bearer token), and a nil error means "engine
// reachable" so the caller does not fail closed.
func (authenticatedOnlyPDP) Decide(_ context.Context, _ security.SecurityPrincipal, _ security.Action, _ security.ResourceRef) (bool, error) {
	return true, nil
}
