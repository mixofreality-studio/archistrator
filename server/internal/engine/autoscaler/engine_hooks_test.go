// Hooks for autoscalerEngine scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package autoscaler_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
)

// newSubject builds the component under test exactly as its caller
// (operationsManager) wires it: through the generated public constructor. The
// autoscaler is a pure Engine with no downstream dependencies, so nothing on
// the host needs arranging.
func newSubject(t *testing.T, h *scenariohost.Host) autoscaler.AutoscalerEngine {
	t.Helper()
	_ = h
	return autoscaler.NewAutoscalerEngine()
}
