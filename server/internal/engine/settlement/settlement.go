// Package settlement is the settlementEngine — the Engine that encapsulates
// settlement-terms volatility (revenue share, compute-cost pricing, schedule,
// billing): how the signed net for a customer's cycle is computed from inbound
// revenue and compute usage, and which way (payout vs shortfall charge) it routes.
//
// Contract: designs/aiarch/implementation/contracts/settlementEngine.md (FROZEN
// 2026-05-29). Layer rules: [[the-method-layers]] / Löwy ch. 5 — the Engine layer.
//
// PURE & DETERMINISTIC. This package does NO I/O, reads NO clock (no time.Now()),
// uses NO RNG (no math/rand), starts no goroutines, and makes NO outbound calls to
// any ResourceAccess, Manager, or other Engine. It STATES a routing directive as a
// VALUE; it never moves money. The two calling Managers (settlementManager,
// projectDesignManager) read all inputs from the ledgers / head-state, pass value
// snapshots in, and execute the returned directive themselves. This is what makes
// the Managers' direct in-workflow calls replay-safe.
//
// Money safety (settlementEngine.md §3, §6): money is NEVER a float — all money math
// is exact int64 minor units. Settling real money under an unregistered
// revenue-share / compute-cost regime is a financial-correctness hazard, so an
// unknown-terms input returns an error (fweng.InvalidInput, "unknown terms"); the
// Engine NEVER silently falls back to a default regime.
//
// A FAILING COMPUTATION IS A DOMAIN RESULT, not an error: a zero-net cycle yields a
// zero net + RoutingNoAction, a normal return value. The error channel is reserved
// for programmer / contract misuse (ContractMisuse), unknown terms (InvalidInput),
// and broken internal invariants (InternalInvariant) only.
//
// Imports ONLY framework-go/engine (the shared Engine error model, aliased fweng).
// Per Option B full encapsulation the contract redefines every domain type it uses
// as its OWN generated def (contract.gen.go: Money, SettlementTerms, the
// settlement-terms enums, ProjectOption, OptionID), so this package imports NO
// projectstate — the projectDesignManager converts the canonical projectstate option
// to settlement.ProjectOption at the call boundary.
package settlement

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// computeCostCentsPerComputeUnitSecond is the deterministic base price (in minor
// units, i.e. cents) applied per metered compute-unit-second before markup. It is a
// fixed Strategy constant of the launch FlatMarkup regime — NOT a clock/RNG/config
// read. Exact integer arithmetic only; no float money.
const computeCostCentsPerComputeUnitSecond int64 = 1

// RoutingDirective is which way the signed net routes. It is a VALUE the Engine
// returns; the Manager executes it against merchantGatewayAccess (it is never
// performed here). For RecomputeNet the directive still reflects the corrected net's
// sign; the Manager computes the re-charge / re-payout delta vs the prior settlement.

// RoutingNoAction — net == 0; nothing routes.

// RoutingPayout — net > 0; Manager calls merchantGatewayAccess.payoutCustomer.

// RoutingCharge — net < 0; Manager calls merchantGatewayAccess.chargeCustomer.

// CycleRevenue is a value snapshot of a cycle's inbound revenue, read by the Manager
// from the Revenue Ledger and passed in by value. For RecomputeNet, GrossInbound is
// the REVERSAL-ADJUSTED total (the Manager has already recorded the chargeback
// reversal and re-read the range). Canonical home: revenueLedgerAccess.

// Σ inbound revenue over the cycle window (exact minor units)
// for audit/labeling; not load-bearing in the net

// CycleUsage is a value snapshot of a cycle's metered compute usage, read by the
// Manager from the Usage Log and passed in by value. Canonical home: usageAccess.

// metered compute consumed over the cycle window

// SettlementResult is the shared output of ComputeNet AND RecomputeNet (the
// factor-up, settlementEngine.md §2.4): the signed net plus the routing directive,
// plus the decomposition the Manager renders into the settlement statement. It is
// NOT the executed payout/charge — the Manager executes that.

// SignedNet = (inbound revenue − revenue share − compute cost). Signed:
// positive == payout to customer; negative == shortfall charge. Exact minor units.

// STATED, not performed
// the cut taken (for the statement)
// the pass-through applied (for the statement)

// ReSettlementInput is the input to RecomputeNet (the DSL label recomputeNet(affectedCycle)).
// The affected-cycle value carries everything needed to recompute by value: the
// reversal-adjusted revenue, the usage, the terms, and the prior settled result (so
// the Manager can compute the re-charge / re-payout delta). The Engine never re-reads
// any ledger.

// OptionID identifies one assembled ProjectOption within an SDP review. Mirrors
// projectstate.OptionID; the contract redefines it as its own type so the Engine
// imports nothing (Option B full encapsulation). The caller converts at the boundary.

// ProjectOption is the input to ProjectCommitTimeRevenueShareAndComputeCost: the
// committed project option as this Engine needs it — it reads ONLY the customer's
// settlement Terms (and carries OptionID for audit/labeling). The canonical Phase-2
// option model is owned by projectStateAccess (projectstate.ProjectOption) and
// carries many more option-shaping fields (network, worker mix, usage assumption)
// read by the two estimation Engines; this Engine ignores them, so — per the
// billingEngine precedent (its own slim ProjectOption) — the contract carries only
// the slice it reads. The projectDesignManager converts the canonical option to
// this snapshot at the call boundary.

// the customer's settlement terms, carried on the option by value

// Projection is the output of ProjectCommitTimeRevenueShareAndComputeCost: the
// terms-side projection bound to the committed option for the SDP confirmation row
// (UC2). It is a PROJECTION OF TERMS — no actuals exist at commit time — and is NOT
// the operation-side cost forecast (that is operationEstimationEngine).

// SettlementEngine is the pure, deterministic settlement-terms-application port
// (settlementEngine.md §2). Three ops, two callers, ZERO outbound edges.

// ComputeNet — UC6 cycle-close signed net + routing for an actual closed cycle.
// Called by settlementManager. (settlementEngine.md §2.1)

// RecomputeNet — ncuc4 chargeback re-settlement: the corrected net for a cycle
// whose revenue was reversal-adjusted by a chargeback. Called by settlementManager.
// (settlementEngine.md §2.2)

// ProjectCommitTimeRevenueShareAndComputeCost — UC2 commit-time terms projection.
// Takes only the option (which carries the customer's terms by value). Called by
// projectDesignManager. (settlementEngine.md §2.3)

// engine is the stateless implementation of SettlementEngine. It holds no fields,
// does no I/O, reads no clock/RNG, and starts no goroutines — safe to call directly
// from workflow code.
type engine struct{}

// New returns the stateless settlement Engine.
func New() SettlementEngine { return engine{} }

// Compile-time assertion that engine satisfies the port.
var _ SettlementEngine = engine{}

// termsKnown reports whether both pivot regimes are registered. An unknown regime is
// a deploy/config hazard — settling real money under an unregistered revenue-share or
// compute-cost regime is forbidden, so callers turn a false here into an
// InvalidInput "unknown terms" error rather than a silent default.
func termsKnown(terms SettlementTerms) bool {
	return terms.RevenueShare != RevenueShareUnknown &&
		terms.ComputeCost != ComputeCostUnknown
}

// ProjectCommitTimeRevenueShareAndComputeCost echoes the committed option's
// settlement-terms regime kinds and percents as a projection (no actuals). Unknown
// terms ⇒ InvalidInput "unknown terms" — never a silent default (money safety).
func (engine) ProjectCommitTimeRevenueShareAndComputeCost(_ fweng.Context, option ProjectOption) (Projection, error) {
	terms := option.Terms
	if !termsKnown(terms) {
		return Projection{}, fweng.New(fweng.InvalidInput, "unknown terms")
	}
	return Projection{
		RevenueShareKind:     terms.RevenueShare,
		RevenueSharePercent:  terms.RevenueSharePercent,
		ComputeCostKind:      terms.ComputeCost,
		ComputeMarkupPercent: terms.ComputeMarkupPercent,
	}, nil
}

// ComputeNet computes the signed net for an actual closed cycle and the routing
// directive. All money math is exact int64 minor units. (settlementEngine.md §2.1)
func (engine) ComputeNet(_ fweng.Context, revenue CycleRevenue, usage CycleUsage, terms SettlementTerms) (SettlementResult, error) {
	return computeNet(revenue, usage, terms)
}

// RecomputeNet computes the corrected signed net for a reversal-adjusted cycle. The
// computation is identical to ComputeNet over the reversal-adjusted revenue total;
// the Manager computes the delta vs affectedCycle.PriorSettled. (settlementEngine.md §2.2)
func (engine) RecomputeNet(_ fweng.Context, affectedCycle ReSettlementInput) (SettlementResult, error) {
	return computeNet(affectedCycle.Revenue, affectedCycle.Usage, affectedCycle.Terms)
}

// computeNet is the shared, pure net computation behind ComputeNet and RecomputeNet.
//
// Money math is exact integer minor units throughout:
//
//	revenueShareApplied = GrossInbound × RevenueSharePercent / 100
//	      computed as int64(GrossInbound × round(pct×100)) / 10000 to avoid float drift
//	computeCostApplied  = computeUnitSeconds × centsPerUnit, then ×(1 + markup/100)
//	      base and markup folded into one integer ×/÷ to keep it exact
//	signedNet           = GrossInbound − revenueShareApplied − computeCostApplied
//
// RoutingDirective follows the sign of signedNet (>0 Payout, <0 Charge, ==0 NoAction).
func computeNet(revenue CycleRevenue, usage CycleUsage, terms SettlementTerms) (SettlementResult, error) {
	// Pre-conditions — Manager wiring bugs, not "no-net-possible" outcomes.
	if revenue.GrossInbound.Currency == "" {
		return SettlementResult{}, fweng.New(fweng.ContractMisuse,
			"computeNet: revenue currency is empty (Manager failed to assemble a valid CycleRevenue)")
	}
	if revenue.GrossInbound.MinorUnits < 0 {
		return SettlementResult{}, fweng.New(fweng.ContractMisuse,
			"computeNet: gross inbound revenue is negative (Manager failed to assemble a valid CycleRevenue)")
	}
	if !termsKnown(terms) {
		// Money safety: never settle real money under an unregistered regime.
		return SettlementResult{}, fweng.New(fweng.InvalidInput, "unknown terms")
	}

	currency := revenue.GrossInbound.Currency
	gross := revenue.GrossInbound.MinorUnits

	// Revenue share: GrossInbound × pct/100, exact integer arithmetic. pctTimes100
	// is the percent scaled by 100 (so 10.0% → 1000), giving a /10000 divisor and
	// keeping two decimal places of percent precision without float money.
	pctTimes100 := roundToInt64(terms.RevenueSharePercent * 100)
	revenueShareUnits := gross * pctTimes100 / 10000

	// Compute cost: base = computeUnitSeconds × centsPerUnit, then × (1 + markup/100).
	// computeUnitSeconds is a usage quantity (not money); it is converted to integer
	// minor units exactly once, here, and never carried as float money thereafter.
	baseComputeUnits := roundToInt64(usage.ComputeUnitSeconds) * computeCostCentsPerComputeUnitSecond
	markupTimes100 := roundToInt64(terms.ComputeMarkupPercent * 100)
	// base × (1 + markup/100) == base × (10000 + markupTimes100) / 10000, exact.
	computeCostUnits := baseComputeUnits * (10000 + markupTimes100) / 10000

	signedNetUnits := gross - revenueShareUnits - computeCostUnits

	result := SettlementResult{
		SignedNet:           Money{MinorUnits: signedNetUnits, Currency: currency},
		RoutingDirective:    directiveFor(signedNetUnits),
		RevenueShareApplied: Money{MinorUnits: revenueShareUnits, Currency: currency},
		ComputeCostApplied:  Money{MinorUnits: computeCostUnits, Currency: currency},
	}

	// Internal-invariant guards (Engine bugs, not domain outcomes).
	if revenueShareUnits > gross {
		return SettlementResult{}, fweng.New(fweng.InternalInvariant,
			"computeNet: revenue share exceeds gross inbound")
	}
	if computeCostUnits < 0 {
		return SettlementResult{}, fweng.New(fweng.InternalInvariant,
			"computeNet: compute cost is negative")
	}
	if !directiveConsistent(signedNetUnits, result.RoutingDirective) {
		return SettlementResult{}, fweng.New(fweng.InternalInvariant,
			"computeNet: routing directive inconsistent with the sign of the net")
	}

	return result, nil
}

// directiveFor maps a signed net (in minor units) to its routing directive.
func directiveFor(signedNetUnits int64) RoutingDirective {
	switch {
	case signedNetUnits > 0:
		return RoutingPayout
	case signedNetUnits < 0:
		return RoutingCharge
	default:
		return RoutingNoAction
	}
}

// directiveConsistent verifies a directive matches the sign of the net.
func directiveConsistent(signedNetUnits int64, d RoutingDirective) bool {
	return d == directiveFor(signedNetUnits)
}

// roundToInt64 rounds a float quantity to the nearest int64 (round half away from
// zero). It is the SINGLE controlled crossing from a float usage/percent quantity to
// exact integer arithmetic; money itself is never a float. No math.Round import — the
// rounding is done with deterministic integer truncation so the Engine stays minimal.
func roundToInt64(f float64) int64 {
	if f >= 0 {
		return int64(f + 0.5)
	}
	return int64(f - 0.5)
}
