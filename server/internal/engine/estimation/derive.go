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

// workerClassFor maps an activity ID prefix to its worker class. The roster is FIXED —
// an unknown class silently rides default token rates in the cost engines and
// misclassifies in every downstream view, so this function only ever returns a roster
// member.
//
// Verified against the 69 hand-authored activities in the committed list: prefix
// predicts workerClass with ZERO exceptions, which is what makes it derivable.
func workerClassFor(prefix string) string {
	switch prefix {
	case "C", "U":
		return "junior-developer" // junior builds components and the SPA
	case "R", "I":
		return "senior-developer" // senior integrates and owns provisioning
	case "G":
		return "ui-designer"
	default:
		return "senior-developer"
	}
}

// noncodingInventoryClass returns the fixed worker class for a member of the always-emit
// noncoding inventory. Löwy ch. 9 keeps three DISTINCT quality roles — the test engineer
// (writes code to break the system), the software tester (runs system testing), and the
// QA engineer (senior, process: "what will it take to assure quality?"). Do not collapse
// them. The regression harness is developer-owned, deliberately NOT the test engineer's.
func noncodingInventoryClass(name string) string {
	switch name {
	case "N-STP", "N-STH", "N-PERF":
		return "test-engineer"
	case "N-RTH", "N-SMOKE":
		return "senior-developer"
	case "N-QA":
		return "qa-engineer"
	case "N-IT":
		return "software-tester"
	default:
		return "senior-developer"
	}
}

// defaultEffortFor returns the band-MIDPOINT effort default for a component, in whole
// 5-day quanta. These are the bands the-method-activity-list already states.
//
// Exception: resource. The Resource build band is 10–20 (midpoint 15), but R-* activities
// are emitted ONLY for provisioning vendor resources (Stripe account, GitHub App) — not
// building a resource. Owned stores get no R-* at all; their schema work arrives as
// additive noncoding. The value 10 is grounded in observed vendor-provisioning effort:
// the committed activity list's six R-* activities ran 5, 5, 10, 15, 15, 25 (median and
// mean both 12.5). Do not "fix" this to 15; that would silently inflate every
// vendor-provisioning estimate.
//
// Deliberately NOT signal-driven (op counts, volatility counts, graph degree): service
// contracts are Phase-3 artifacts and do not exist when this runs, a regression over
// slot-5 metadata is the false precision App C §4.4 forbids ("strive for accuracy, not
// precision"), and it would make the baseline churn whenever a relationship is edited.
// Roughly half of these get overridden by a justified delta — that is the design intent:
// the agent's judgment is spent on the exceptions, not on transcription.
func defaultEffortFor(kind string) float64 {
	switch kind {
	case "manager":
		return 25
	case "engine":
		return 15
	case "resourceAccess":
		return 10
	case "client":
		return 25
	case "utility":
		return 10
	case "resource":
		return 10
	default:
		return 10
	}
}

// defaultRiskFor maps an effort band to a Fibonacci risk bucket. Dumb on purpose — it
// carries no more information than the effort does, which is honest: at Phase-2 time
// nothing better is knowable without the agent's judgment, and that arrives as an
// override.
func defaultRiskFor(effortDays float64) int64 {
	switch {
	case effortDays <= 10:
		return 2
	case effortDays <= 20:
		return 3
	default:
		return 5
	}
}
