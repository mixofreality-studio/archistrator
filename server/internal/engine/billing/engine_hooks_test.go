// Hooks for billingEngine scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package billing_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
)

// newSubject builds the component under test exactly as its callers
// (billingManager, deliveryManager) wire it: through the generated public
// constructor. The billingEngine is a pure Engine with no downstream
// dependencies, so nothing on the host needs arranging.
func newSubject(t *testing.T, h *scenariohost.Host) billing.BillingEngine {
	t.Helper()
	_ = h
	return billing.NewBillingEngine()
}
