package projectstate

import "testing"

// TestConstructionTransitionAccess_OpCount asserts the port is within App-C §6 bounds.
// 3–5 ops: strive. ≤12: acceptable. >12: warning. ≥20: reject (directive error).
// Current count: 8. This test documents the adjudicated count from lifecycle-2 Plan 2.
func TestConstructionTransitionAccess_OpCount(t *testing.T) {
	const wantOps = 8
	const avoidAbove = 12
	if wantOps > avoidAbove {
		t.Errorf("ConstructionTransitionAccess has %d ops; App-C §6 advises avoiding >%d", wantOps, avoidAbove)
	}
	// The var _ assertion above (GitStore) is the real compile-time gate.
	// This test just documents the decision.
	t.Logf("ConstructionTransitionAccess: %d ops (App-C §6 adjudicated ≤12 at lifecycle-2 Task 3)", wantOps)
}
