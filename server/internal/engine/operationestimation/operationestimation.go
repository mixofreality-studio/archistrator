// Package operationestimation is the operationEstimationEngine — the Engine that
// encapsulates the OperationEstimationModel volatility: how operating cost is
// forecasted as a function of usage.
//
// Contract: methodpoc/designs/aiarch/implementation/contracts/operationEstimationEngine.md
// (APPROVED — frozen 2026-05-28). Layer rules: [[the-method-layers]] (Engine layer).
//
// THE METHOD — ENGINE LAYER (Löwy, Righting Software ch. 5):
//
//	This package is PURE and DETERMINISTIC. It performs no I/O, reads no clock
//	(no time.Now()), draws no randomness (no math/rand), makes no outbound calls
//	(no ResourceAccess, no other Engine, no Manager), publishes/subscribes to no
//	events, and holds no global mutable state. Every time- and usage-shaped input
//	arrives as a VALUE from the calling Manager. Same inputs → same output, always.
//	This byte-determinism is what makes both calling Managers' direct in-workflow
//	calls (projectDesignManager, operationsManager) replay-safe with no Temporal
//	Activity wrapper — the Engine imports no Temporal.
//
//	A FAILING COMPUTATION IS A DOMAIN RESULT, not an error. A degenerate forecast
//	(e.g. all-zero cost for a paused app) is a normal return value. The error
//	channel (*fweng.Error) is reserved for PROGRAMMER / CONTRACT MISUSE only:
//	an unregistered infrastructureKind, a semantically-unusable input, an all-zero
//	declared usage with no forecast basis, or a broken internal invariant.
//
// Imports: ONLY projectstate (input value types, the downward engine→projectstate
// edge) and framework-go/engine (the shared Engine error model, aliased fweng).
//
// Strategy axis (CustomerAppInfrastructure): the Engine pivots internally on the
// opaque projectstate.InfrastructureKind discriminator. Adding a new infrastructure
// constituent is a package-internal cost-model addition + a new enum constant — NOT
// a contract amendment. Unknown infrastructure ⇒ fweng.InvalidInput, never a silent
// default to the wrong cost rules.
package operationestimation

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ---------------------------------------------------------------------------
// Output value objects (owned by this Engine — they are computation results, not
// persisted head-state). Field names are load-bearing: projectDesignManager and
// operationsManager depend on them.
// ---------------------------------------------------------------------------

// CostCurvePoint is the projected monthly operating cost at one load multiple of
// the declared usage.
type CostCurvePoint struct {
	LoadMultiplier       float64
	ProjectedMonthlyCost projectstate.Money
}

// UsageCostCurve is projected operating cost as a function of load level, plotted
// at discrete multipliers. Monotonic non-decreasing in LoadMultiplier; always
// includes the 1.0 (declared-usage) point.
type UsageCostCurve struct {
	Points []CostCurvePoint
}

// PayoutShortfallForecast is the expected payout-or-shortfall per settlement cycle
// with a ± sensitivity band around the declared assumption.
type PayoutShortfallForecast struct {
	// ExpectedPerCycleNet is signed: positive == payout to the customer; negative
	// == shortfall charge to the customer.
	ExpectedPerCycleNet projectstate.Money
	// SensitivityLow is the net at the low edge of the ± usage band (cheaper, so a
	// larger payout / smaller shortfall).
	SensitivityLow projectstate.Money
	// SensitivityHigh is the net at the high edge of the ± usage band (costlier, so
	// a smaller payout / larger shortfall).
	SensitivityHigh projectstate.Money
}

// OperationForecast is the design-time output of EstimateForOption.
type OperationForecast struct {
	UsageCostCurve            UsageCostCurve
	PayoutVsShortfallForecast PayoutShortfallForecast
}

// ObservedUsage is a snapshot of what an operated app is ACTUALLY using, read by
// the Manager (operationsManager via usageAccess.readRange) and passed in by value.
// The Engine treats it as an infrastructure-agnostic value; it reads no clock.
type ObservedUsage struct {
	ComputeUnitSeconds float64 // metered compute consumed over the window
	RequestCount       int64
	StorageBytesMonths float64 // metered storage over the window
	EgressBytes        float64
	ObservedReplicas   int // representative capacity over the window
}

// ScalePoint is one op-time "what-if" load level. LoadMultiplier must be > 0;
// 1.0 == current observed load.
type ScalePoint struct {
	LoadMultiplier float64
}

// WhatIfPoint is the projected monthly cost at one ScalePoint.
type WhatIfPoint struct {
	LoadMultiplier       float64
	ProjectedMonthlyCost projectstate.Money
}

// WhatIfCurve is the projected cost at each requested ScalePoint plus the
// current-load (1.0) point. Monotonic non-decreasing in LoadMultiplier.
type WhatIfCurve struct {
	Points []WhatIfPoint
}

// CostProjection is the op-time output of ProjectForOperatedApp.
type CostProjection struct {
	CurrentRunRate       projectstate.Money // extrapolated cost-per-cycle at current observed load
	ProjectedMonthlyCost projectstate.Money // run-rate normalized to a calendar month
	ScaleWhatIfCurve     WhatIfCurve        // projected cost at each requested ScalePoint (+ current-load point)
}

// ---------------------------------------------------------------------------
// Contract surface.
// ---------------------------------------------------------------------------

// OperationEstimationEngine is the frozen two-operation Engine surface. Both ops
// are pure deterministic functions; both return *fweng.Error on programmer/contract
// misuse only.
type OperationEstimationEngine interface {
	// EstimateForOption is the design-time SDP-review forecast: given a project
	// option, the customer's declared usage assumptions, and the chosen
	// infrastructure kind, produce the usage→operating-cost curve and the
	// payout-vs-shortfall forecast. Called by projectDesignManager.
	EstimateForOption(
		option projectstate.ProjectOption,
		declaredUsage projectstate.UsageAssumption,
		infrastructureKind projectstate.InfrastructureKind,
	) (OperationForecast, error)

	// ProjectForOperatedApp is the op-time read-side projection: given observed
	// usage on an already-operated app and a set of scale what-if points, produce
	// the current run-rate, projected monthly cost, and the what-if curve. Called
	// by operationsManager.
	ProjectForOperatedApp(
		observedUsage ObservedUsage,
		infrastructureKind projectstate.InfrastructureKind,
		scaleWhatIfPoints []ScalePoint,
	) (CostProjection, error)
}

// New returns the default OperationEstimationEngine. It is stateless and safe to
// share/reuse across calls and Managers.
func New() OperationEstimationEngine { return engine{} }

// engine is the stateless implementation. It holds no fields — all behaviour is a
// pure function of the inputs, pivoting on the package-internal cost-Strategy table.
type engine struct{}

// ---------------------------------------------------------------------------
// Internal cost-Strategy axis (CustomerAppInfrastructure). NOT on the contract.
// Adding an infrastructure is a new entry here + a new InfrastructureKind constant.
// ---------------------------------------------------------------------------

// infrastructureCostModel is the per-infrastructure deterministic cost Strategy.
type infrastructureCostModel interface {
	// monthlyComputeCostMinorUnits returns the projected monthly operating cost, in
	// USD minor units (cents), implied by a per-month request volume scaled by load.
	// Monotonic non-decreasing in load for load >= 0.
	monthlyComputeCostMinorUnits(requestsPerMonth float64, load float64) int64
	// monthlyComputeCostFromObserved returns the projected monthly operating cost,
	// in USD minor units, implied by an observed-usage snapshot scaled by load.
	// Monotonic non-decreasing in load for load >= 0.
	monthlyComputeCostFromObserved(observed ObservedUsage, load float64) int64
}

// costModelFor resolves the cost Strategy for an infrastructure kind, or reports an
// UnknownInfrastructure-style InvalidInput when none is registered. The Engine never
// falls back to a default Strategy (that would forecast against the wrong cost rules).
func costModelFor(kind projectstate.InfrastructureKind) (infrastructureCostModel, *fweng.Error) {
	switch kind {
	case projectstate.InfrastructureKindGoTemporalPostgres:
		return goTemporalPostgresCostModel{}, nil
	case projectstate.InfrastructureKindUnknown:
		return nil, fweng.New(fweng.InvalidInput, "unknown infrastructure kind")
	default:
		return nil, fweng.New(fweng.InvalidInput, "unknown infrastructure kind")
	}
}

// goTemporalPostgresCostModel is the launch infrastructure's cost Strategy:
// Go + Temporal + Postgres + Git + S3. A simple, deterministic linear unit-cost
// model. Constants chosen to be reasonable, not authoritative — the volatility is
// the SHAPE of the model, not these numbers.
type goTemporalPostgresCostModel struct{}

// Unit-cost constants for the launch infrastructure (all in USD cents).
const (
	// computeCentsPerThousandRequests is the marginal compute+DB cost per 1,000
	// served requests per month on the Go/Temporal/Postgres stack.
	computeCentsPerThousandRequests = 8.0
	// baselineMonthlyCents is the fixed monthly floor (always-on Postgres + Temporal
	// + a minimum replica) before any request load.
	baselineMonthlyCents = 5000.0
	// computeCentsPerComputeUnitSecond is the op-time marginal cost per metered
	// compute-unit-second.
	computeCentsPerComputeUnitSecond = 0.02
	// computeCentsPerMillionRequests is the op-time marginal cost per 1M requests.
	computeCentsPerMillionRequests = 40.0
	// storageCentsPerGiBMonth is the op-time marginal storage cost per GiB-month.
	storageCentsPerGiBMonth = 10.0
	// egressCentsPerGiB is the op-time marginal egress cost per GiB.
	egressCentsPerGiB = 9.0
	// replicaCentsPerMonth is the op-time per-replica monthly cost.
	replicaCentsPerMonth = 2000.0
)

const bytesPerGiB = 1024.0 * 1024.0 * 1024.0

// monthlyComputeCostMinorUnits: baseline floor + marginal request cost, scaled by
// load. Load only scales the marginal (request-driven) term — the baseline floor is
// fixed — keeping the result monotonic non-decreasing in load.
func (goTemporalPostgresCostModel) monthlyComputeCostMinorUnits(requestsPerMonth float64, load float64) int64 {
	marginal := (requestsPerMonth / 1000.0) * computeCentsPerThousandRequests * load
	return roundToMinorUnits(baselineMonthlyCents + marginal)
}

// monthlyComputeCostFromObserved: sum the metered op-time dimensions, scale the
// load-sensitive ones by load, add the fixed replica floor. Storage is treated as
// load-insensitive (data at rest does not grow with request load in this simple
// model); compute, requests, and egress scale with load.
func (goTemporalPostgresCostModel) monthlyComputeCostFromObserved(o ObservedUsage, load float64) int64 {
	computeCents := o.ComputeUnitSeconds * computeCentsPerComputeUnitSecond * load
	requestCents := (float64(o.RequestCount) / 1_000_000.0) * computeCentsPerMillionRequests * load
	egressCents := (o.EgressBytes / bytesPerGiB) * egressCentsPerGiB * load
	storageCents := o.StorageBytesMonths / bytesPerGiB * storageCentsPerGiBMonth
	replicaCents := float64(o.ObservedReplicas) * replicaCentsPerMonth
	return roundToMinorUnits(computeCents + requestCents + egressCents + storageCents + replicaCents)
}

// ---------------------------------------------------------------------------
// Design-time forecast.
// ---------------------------------------------------------------------------

// designLoadMultipliers are the fixed load levels of the design-time cost curve.
// Sorted ascending; includes the 1.0 declared-usage point.
var designLoadMultipliers = []float64{0.5, 1.0, 2.0, 5.0, 10.0}

// arpuCentsPerDAUPerMonth is the notional average monthly revenue per daily-active
// user used to derive the customer's gross revenue for the payout-vs-shortfall
// forecast. Deterministic constant — no external lookup.
const arpuCentsPerDAUPerMonth = 300.0

// defaultCurrency is used unless the option's settlement terms imply otherwise.
const defaultCurrency = "USD"

func (engine) EstimateForOption(
	option projectstate.ProjectOption,
	declaredUsage projectstate.UsageAssumption,
	infrastructureKind projectstate.InfrastructureKind,
) (OperationForecast, error) {
	model, ierr := costModelFor(infrastructureKind)
	if ierr != nil {
		return OperationForecast{}, ierr
	}

	// ContractMisuse: all-zero declared usage gives no basis to forecast from. The
	// Manager guarantees a usable value (a category default when the customer can't
	// give a number — glossary "Usage Assumption"), so an all-zero arrival is a
	// Manager bug, not a "no-forecast-possible" domain outcome.
	if declaredUsage.ExpectedDailyActiveUsers <= 0 &&
		declaredUsage.RequestsPerMinute <= 0 &&
		declaredUsage.AvgPayloadBytes <= 0 {
		return OperationForecast{}, fweng.New(fweng.ContractMisuse,
			"EstimateForOption: declaredUsage is all-zero — no forecast basis")
	}

	currency := currencyFor(option.Terms)

	// Monthly request volume implied by the declared assumption. RequestsPerMinute
	// is the steady-state rate; DAU contributes a per-user monthly request floor so
	// a DAU-only assumption (no rate given) still produces a non-trivial curve.
	const minutesPerMonth = 60.0 * 24.0 * 30.0
	const requestsPerDAUPerMonth = 30.0
	requestsPerMonth := declaredUsage.RequestsPerMinute*minutesPerMonth +
		float64(declaredUsage.ExpectedDailyActiveUsers)*requestsPerDAUPerMonth

	// Usage→cost curve at the fixed design load multipliers.
	points := make([]CostCurvePoint, 0, len(designLoadMultipliers))
	for _, load := range designLoadMultipliers {
		points = append(points, CostCurvePoint{
			LoadMultiplier:       load,
			ProjectedMonthlyCost: minorUnits(model.monthlyComputeCostMinorUnits(requestsPerMonth, load), currency),
		})
	}
	curve := UsageCostCurve{Points: points}
	if !curveMonotonic(load1Costs(points)) {
		return OperationForecast{}, fweng.New(fweng.InternalInvariant,
			"EstimateForOption: usage cost curve is non-monotonic")
	}

	// Notional monthly gross revenue from the declared DAU.
	grossRevenueCents := float64(declaredUsage.ExpectedDailyActiveUsers) * arpuCentsPerDAUPerMonth
	// aiarch's revenue-share cut (the platform's take) per the option's settlement terms.
	aiarchCutCents := grossRevenueCents * (option.Terms.RevenueSharePercent / 100.0)

	// Net = aiarch's cut minus the projected compute cost. Computed at three load
	// points so the band is a deterministic ± around the declared assumption.
	costAt := func(load float64) float64 {
		return float64(model.monthlyComputeCostMinorUnits(requestsPerMonth, load))
	}
	expectedNetCents := aiarchCutCents - costAt(1.0)
	// Lower cost edge (0.5×) → costs less → larger net; higher cost edge (2×) →
	// costs more → smaller net.
	netLowCostCents := aiarchCutCents - costAt(0.5)
	netHighCostCents := aiarchCutCents - costAt(2.0)

	forecast := OperationForecast{
		UsageCostCurve: curve,
		PayoutVsShortfallForecast: PayoutShortfallForecast{
			ExpectedPerCycleNet: minorUnits(roundToMinorUnits(expectedNetCents), currency),
			SensitivityLow:      minorUnits(roundToMinorUnits(netLowCostCents), currency),
			SensitivityHigh:     minorUnits(roundToMinorUnits(netHighCostCents), currency),
		},
	}
	return forecast, nil
}

// ---------------------------------------------------------------------------
// Op-time projection.
// ---------------------------------------------------------------------------

func (engine) ProjectForOperatedApp(
	observedUsage ObservedUsage,
	infrastructureKind projectstate.InfrastructureKind,
	scaleWhatIfPoints []ScalePoint,
) (CostProjection, error) {
	model, ierr := costModelFor(infrastructureKind)
	if ierr != nil {
		return CostProjection{}, ierr
	}

	// InvalidInput: a what-if point at or below zero load is semantically unusable.
	for _, p := range scaleWhatIfPoints {
		if p.LoadMultiplier <= 0 {
			return CostProjection{}, fweng.New(fweng.InvalidInput,
				"ProjectForOperatedApp: ScalePoint.LoadMultiplier must be > 0")
		}
	}

	// Current run-rate and monthly cost are the same monthly projection at load 1.0
	// in this model (run-rate IS the monthly extrapolation of observed usage).
	currentMonthly := model.monthlyComputeCostFromObserved(observedUsage, 1.0)

	// What-if curve: always include the current-load (1.0) point, then one per
	// requested ScalePoint, sorted ascending by load for a monotonic curve.
	loads := make([]float64, 0, len(scaleWhatIfPoints)+1)
	loads = append(loads, 1.0)
	for _, p := range scaleWhatIfPoints {
		loads = append(loads, p.LoadMultiplier)
	}
	sortFloatsAsc(loads)
	loads = dedupeFloats(loads)

	whatIfPoints := make([]WhatIfPoint, 0, len(loads))
	for _, load := range loads {
		whatIfPoints = append(whatIfPoints, WhatIfPoint{
			LoadMultiplier:       load,
			ProjectedMonthlyCost: minorUnits(model.monthlyComputeCostFromObserved(observedUsage, load), defaultCurrency),
		})
	}
	if !curveMonotonic(whatIfCosts(whatIfPoints)) {
		return CostProjection{}, fweng.New(fweng.InternalInvariant,
			"ProjectForOperatedApp: scale what-if curve is non-monotonic")
	}

	return CostProjection{
		CurrentRunRate:       minorUnits(currentMonthly, defaultCurrency),
		ProjectedMonthlyCost: minorUnits(currentMonthly, defaultCurrency),
		ScaleWhatIfCurve:     WhatIfCurve{Points: whatIfPoints},
	}, nil
}

// ---------------------------------------------------------------------------
// Pure helpers (no I/O, no clock, no RNG).
// ---------------------------------------------------------------------------

// minorUnits builds a projectstate.Money from a minor-units amount and currency.
func minorUnits(amount int64, currency string) projectstate.Money {
	return projectstate.Money{MinorUnits: amount, Currency: currency}
}

// roundToMinorUnits deterministically rounds cents (a float) to the nearest integer
// minor unit, half away from zero. No math.Round dependency to keep the import set
// to the two allowed packages.
func roundToMinorUnits(cents float64) int64 {
	if cents >= 0 {
		return int64(cents + 0.5)
	}
	return int64(cents - 0.5)
}

// currencyFor derives the forecast currency from the option's settlement terms.
// The launch terms imply USD; the field exists so a future negotiated-currency
// regime is a model change, not a signature change.
func currencyFor(_ projectstate.SettlementTerms) string {
	return defaultCurrency
}

// load1Costs extracts the minor-unit costs from a design curve, in point order.
func load1Costs(points []CostCurvePoint) []int64 {
	out := make([]int64, len(points))
	for i, p := range points {
		out[i] = p.ProjectedMonthlyCost.MinorUnits
	}
	return out
}

// whatIfCosts extracts the minor-unit costs from a what-if curve, in point order.
func whatIfCosts(points []WhatIfPoint) []int64 {
	out := make([]int64, len(points))
	for i, p := range points {
		out[i] = p.ProjectedMonthlyCost.MinorUnits
	}
	return out
}

// curveMonotonic reports whether a cost sequence is non-decreasing. A curve sorted
// ascending by load must yield non-decreasing cost; a violation is an internal
// invariant break (an Engine bug), surfaced as InternalInvariant by the callers.
func curveMonotonic(costs []int64) bool {
	for i := 1; i < len(costs); i++ {
		if costs[i] < costs[i-1] {
			return false
		}
	}
	return true
}

// sortFloatsAsc sorts in place, ascending. Insertion sort — deterministic, no
// sort-package dependency needed and the slices are tiny.
func sortFloatsAsc(xs []float64) {
	for i := 1; i < len(xs); i++ {
		v := xs[i]
		j := i - 1
		for j >= 0 && xs[j] > v {
			xs[j+1] = xs[j]
			j--
		}
		xs[j+1] = v
	}
}

// dedupeFloats removes adjacent duplicates from an ascending-sorted slice,
// preserving order. Keeps the what-if curve from carrying a duplicate of the 1.0
// current-load point when a caller also requests a 1.0 ScalePoint.
func dedupeFloats(xs []float64) []float64 {
	if len(xs) == 0 {
		return xs
	}
	out := xs[:1]
	for i := 1; i < len(xs); i++ {
		if xs[i] != out[len(out)-1] {
			out = append(out, xs[i])
		}
	}
	return out
}
