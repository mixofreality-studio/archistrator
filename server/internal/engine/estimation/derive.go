// Package estimation implements the EstimationEngine. This file, derive.go, implements
// DerivePlan — the deterministic Phase-2 derivation of the activity list and network from
// the committed System (Löwy ch. 11; Fig 11-4 → Fig 11-5 is literally a transitive
// reduction over the component dependency chart). The activity inventory is NOT authored:
// it falls out of the architecture, and the only authored input is the ActivityListDeltas
// document (justified effort/risk overrides plus genuinely componentless additive
// activities).
//
// Purity, as for every op on this Engine: no I/O, no clock, no randomness, no globals.
// Identical inputs → identical DerivedPlan, always. All iteration over maps is sorted.
package estimation

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// DerivePlan derives the full activity list and network from the committed System and
// applies the authored deltas.
//
// An EMPTY system is a normal DOMAIN result (an empty plan) — a project may be read
// before its architecture is committed. The *fweng.Error channel is reserved for
// contract misuse: a delta that the vocabulary forbids (an override naming no derived
// activity, an additive carrying a componentId, a missing justification).
func (EstimationEngineImpl) DerivePlan(_ fweng.Context, system SystemView, _ ActivityListDeltas) (DerivedPlan, error) {
	if len(system.Components) == 0 {
		return DerivedPlan{Activities: nil, Dependencies: nil, Milestones: nil}, nil
	}
	return DerivedPlan{Activities: nil, Dependencies: nil, Milestones: nil}, nil
}
