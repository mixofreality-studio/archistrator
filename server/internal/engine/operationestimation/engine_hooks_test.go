// Hooks for operationEstimationEngine scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package operationestimation_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
)

// newSubject builds the component under test through its public generated
// constructor. operationEstimationEngine is a pure Engine with no outbound
// dependencies, so the host contributes nothing to its wiring.
func newSubject(t *testing.T, h *scenariohost.Host) operationestimation.OperationEstimationEngine {
	t.Helper()
	_ = h
	return operationestimation.NewOperationEstimationEngine()
}
