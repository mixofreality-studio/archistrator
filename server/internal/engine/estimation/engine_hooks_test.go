// Hooks for estimationEngine scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package estimation_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
)

// newSubject builds the component under test through its generated public
// constructor. estimationEngine is a pure Engine (no outbound dependencies), so
// the host contributes nothing beyond the shared lifecycle.
func newSubject(t *testing.T, _ *scenariohost.Host) estimation.EstimationEngine {
	t.Helper()
	return estimation.NewEstimationEngine()
}
