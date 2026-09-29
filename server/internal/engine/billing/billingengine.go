// Package billing is the billingEngine — the Engine that encapsulates
// billing-terms volatility (compute-cost pricing, schedule, billing): how the signed
// net for a customer's cycle is computed from inbound revenue and compute usage, and
// whether it routes a shortfall charge (charge-only: a non-negative net routes
// NoAction — the platform never pays out). REVENUE SHARE IS NOT ONE OF ITS AXES any
// more (stage 4b2, founder ruling): the platform bills a usage-based hosting fee for
// operating a delivered system, so there is no cut to vary and nothing to encapsulate
// about one — see computeNet below.
//
// Contract: designs/aiarch/implementation/contracts/billingEngine.md (FROZEN
// 2026-05-29). Layer rules: [[the-method-layers]] / Löwy ch. 5 — the Engine layer.
//
// PURE & DETERMINISTIC. This package does NO I/O, reads NO clock (no time.Now()),
// uses NO RNG (no math/rand), starts no goroutines, and makes NO outbound calls to
// any ResourceAccess, Manager, or other Engine. It STATES a routing directive as a
// VALUE; it never moves money. The two calling Managers (billingManager,
// projectDesignManager) read all inputs from the ledgers / head-state, pass value
// snapshots in, and execute the returned directive themselves. This is what makes
// the Managers' direct in-workflow calls replay-safe.
//
// Money safety (billingEngine.md §3, §6): money is NEVER a float — all money math
// is exact int64 minor units. Settling real money under an unregistered COMPUTE-COST
// regime is a financial-correctness hazard, so an unknown-terms input returns an error
// (fweng.InvalidInput, "unknown terms"); the Engine NEVER silently falls back to a
// default regime. That is now ONE regime and not two — the revenue-share disjunct went
// with the concept, and termsKnown below says why the narrowing was load-bearing rather
// than cosmetic.
//
// A FAILING COMPUTATION IS A DOMAIN RESULT, not an error: a zero-net cycle yields a
// zero net + RoutingNoAction, a normal return value. The error channel is reserved
// for programmer / contract misuse (ContractMisuse), unknown terms (InvalidInput),
// and broken internal invariants (InternalInvariant) only.
//
// Imports ONLY framework-go/engine (the shared Engine error model, aliased fweng).
// Per Option B full encapsulation the contract redefines every domain type it uses
// as its OWN generated def (contract.gen.go: Money, BillingTerms, the
// billing-terms enums, ProjectOption, OptionID), so this package imports NO
// projectstate — the projectDesignManager converts the canonical projectstate option
// to billing.ProjectOption at the call boundary.
package billing

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// computeCostCentsPerComputeUnitSecond is the deterministic base price (in minor
// units, i.e. cents) applied per metered compute-unit-second before markup. It is a
// fixed Strategy constant of the launch FlatMarkup regime — NOT a clock/RNG/config
// read. Exact integer arithmetic only; no float money.
const computeCostCentsPerComputeUnitSecond int64 = 1

// BillingEngineImpl and NewBillingEngine are generated (contract.gen.go); the
// behaviour below is hand-written on that generated struct.

// termsKnown reports whether the pivot regime is registered. An unknown regime is a
// deploy/config hazard — settling real money under an unregistered compute-cost regime is
// forbidden, so callers turn a false here into an InvalidInput "unknown terms" error rather
// than a silent default.
//
// IT USED TO CHECK TWO. The revenue-share disjunct went with the concept (stage 4b2, founder
// ruling): a guard cannot refuse a vocabulary that no longer exists. What it was refusing on
// this repo was `terms.revenueShare == 0` on a committed slot 8 that meant "no revenue share"
// — the merchant-of-record reversal of 2026-06-09 — so every option was rejected and slots
// 11-16 carried staleBasis for months. The compute-cost half is UNCHANGED and still
// load-bearing: that regime is a real choice, its zero value is a real absence, and a
// silently-defaulted markup is real money.
func termsKnown(terms BillingTerms) bool {
	return terms.ComputeCost != ComputeCostUnknown
}

// ProjectCommitTimeComputeCost echoes the committed option's compute-cost regime kind and
// markup as a projection (no actuals) — NOT the operation-side cost forecast (that is
// operationEstimationEngine). Unknown terms ⇒ InvalidInput "unknown terms" — never a silent
// default (money safety).
//
// It was projectCommitTimeRevenueShareAndComputeCost until stage 4b2. The name named two
// things and it now does one.
func (BillingEngineImpl) ProjectCommitTimeComputeCost(_ fweng.Context, option ProjectOption) (Projection, error) {
	terms := option.Terms
	if !termsKnown(terms) {
		return Projection{}, fweng.New(fweng.InvalidInput, "unknown terms")
	}
	return Projection{
		ComputeCostKind:      terms.ComputeCost,
		ComputeMarkupPercent: terms.ComputeMarkupPercent,
	}, nil
}

// ComputeNet computes the signed net for an actual closed cycle and the routing
// directive. All money math is exact int64 minor units. (billingEngine.md §2.1)
func (BillingEngineImpl) ComputeNet(_ fweng.Context, revenue CycleRevenue, usage CycleUsage, terms BillingTerms) (BillingResult, error) {
	return computeNet(revenue, usage, terms)
}

// RecomputeNet computes the corrected signed net for a reversal-adjusted cycle. The
// computation is identical to ComputeNet over the reversal-adjusted revenue total —
// the Manager has already recorded the chargeback reversal and re-read the range
// before calling; the Engine never re-reads any ledger. The Manager computes the
// delta vs affectedCycle.PriorSettled. (billingEngine.md §2.2)
func (BillingEngineImpl) RecomputeNet(_ fweng.Context, affectedCycle ReBillingInput) (BillingResult, error) {
	return computeNet(affectedCycle.Revenue, affectedCycle.Usage, affectedCycle.Terms)
}

// computeNet is the shared, pure net computation behind ComputeNet and RecomputeNet.
//
// Money math is exact integer minor units throughout:
//
//	computeCostApplied  = computeUnitSeconds × centsPerUnit, then ×(1 + markup/100)
//	      base and markup folded into one integer ×/÷ to keep it exact
//	signedNet           = GrossInbound − computeCostApplied
//
// THE REVENUE-SHARE TERM IS GONE, not zeroed (stage 4b2, founder ruling): the platform bills
// a usage-based hosting fee for operating a delivered system and nothing else, so there is no
// cut to subtract and no RevenueShareApplied to report. GrossInbound stays exactly what it
// was — it is a TOTAL that happened to have a share taken out of it, not the share.
//
// RoutingDirective follows the sign of signedNet (charge-only: <0 Charge, >=0 NoAction).
func computeNet(revenue CycleRevenue, usage CycleUsage, terms BillingTerms) (BillingResult, error) {
	// Pre-conditions — Manager wiring bugs, not "no-net-possible" outcomes.
	if revenue.GrossInbound.Currency == "" {
		return BillingResult{}, fweng.New(fweng.ContractMisuse,
			"computeNet: revenue currency is empty (Manager failed to assemble a valid CycleRevenue)")
	}
	if revenue.GrossInbound.MinorUnits < 0 {
		return BillingResult{}, fweng.New(fweng.ContractMisuse,
			"computeNet: gross inbound revenue is negative (Manager failed to assemble a valid CycleRevenue)")
	}
	if !termsKnown(terms) {
		// Money safety: never settle real money under an unregistered regime.
		return BillingResult{}, fweng.New(fweng.InvalidInput, "unknown terms")
	}

	currency := revenue.GrossInbound.Currency
	gross := revenue.GrossInbound.MinorUnits

	// Compute cost: base = computeUnitSeconds × centsPerUnit, then × (1 + markup/100).
	// computeUnitSeconds is a usage quantity (not money); it is converted to integer
	// minor units exactly once, here, and never carried as float money thereafter.
	baseComputeUnits := roundToInt64(usage.ComputeUnitSeconds) * computeCostCentsPerComputeUnitSecond
	markupTimes100 := roundToInt64(terms.ComputeMarkupPercent * 100)
	// base × (1 + markup/100) == base × (10000 + markupTimes100) / 10000, exact.
	computeCostUnits := baseComputeUnits * (10000 + markupTimes100) / 10000

	signedNetUnits := gross - computeCostUnits

	result := BillingResult{
		SignedNet:          Money{MinorUnits: signedNetUnits, Currency: currency},
		RoutingDirective:   directiveFor(signedNetUnits),
		ComputeCostApplied: Money{MinorUnits: computeCostUnits, Currency: currency},
	}

	// Internal-invariant guards (Engine bugs, not domain outcomes).
	if computeCostUnits < 0 {
		return BillingResult{}, fweng.New(fweng.InternalInvariant,
			"computeNet: compute cost is negative")
	}
	if !directiveConsistent(signedNetUnits, result.RoutingDirective) {
		return BillingResult{}, fweng.New(fweng.InternalInvariant,
			"computeNet: routing directive inconsistent with the sign of the net")
	}

	return result, nil
}

// directiveFor maps a signed net (in minor units) to its routing directive. Under the
// charge-only model the platform never pays out, so a positive net (platform-owes-customer)
// collapses to NoAction — only a negative net (customer-owes-platform) routes a Charge.
func directiveFor(signedNetUnits int64) RoutingDirective {
	if signedNetUnits < 0 {
		return RoutingCharge
	}
	return RoutingNoAction
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
