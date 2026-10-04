// Hooks for reviewEngine scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.

package review_test

import (
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
)

// newSubject builds the component under test exactly as its callers wire it: the
// generated constructor in internal/engine/review. reviewEngine is a pure Engine (no
// I/O, no outbound calls), so it takes no downstream doubles from the host.
func newSubject(t *testing.T, _ *scenariohost.Host) review.ReviewEngine {
	t.Helper()
	return review.NewReviewEngine()
}
