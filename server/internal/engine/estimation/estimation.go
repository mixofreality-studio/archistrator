// Package estimation implements the estimationEngine component — the Engine that
// encapsulates the construction-side EstimationModel volatility: HOW construction
// duration, cost, and risk are computed for one project option.
//
// Contract: methodpoc/designs/aiarch/implementation/contracts/estimationEngine.md
// (APPROVED — FROZEN 2026-05-28). Layer doctrine: [[the-method-layers]] (Engine
// layer) — Engines are PURE, DETERMINISTIC, in-workflow computation:
//
//   - NO I/O, NO time.Now(), NO math/rand, NO goroutines, NO global mutable state.
//   - NO outbound calls — no ResourceAccess, no other Engine, no Manager.
//   - Imports ONLY the framework-go Engine error model (fweng) and its OWN
//     generated contract types (Option B full encapsulation — it imports NO
//     projectstate; the projectDesignManager / projectManager convert the canonical
//     projectstate option/network to estimation.* at the call boundary). It imports
//     NO Temporal — its determinism is what makes the projectDesignManager's direct
//     in-workflow call replay-safe (contract §6).
//
// A failing computation is a DOMAIN RESULT (a normal return value — e.g. a
// zero/edge estimate for a degenerate option), NOT an error. The *fweng.Error
// channel is reserved for programmer / contract misuse ONLY (nil/structurally
// invalid input — fweng.ContractMisuse) and broken engine invariants
// (fweng.InternalInvariant). See contract §3 "Error model".
//
// Single operation EstimateForOption (contract §2.1), named verbatim from the
// architecture.dsl edge label. The single output ConstructionEstimate carries
// three mutually-consistent facets (DurationDays, BuildCost, Risk) produced from
// one pass over the option's activity network (contract §8 Variant B: NOT split
// into three property-style ops). The EstimationModel method (5-day quanta /
// arithmetic risk today; cone-of-uncertainty / geometric risk tomorrow) is a
// package-internal concern behind this surface — FU-EE-D.
package estimation

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// ---------------------------------------------------------------------------
// Domain types redefined as this component's OWN defs (Option B full
// encapsulation). They MIRROR projectstate (the canonical home owned by
// projectStateAccess); the projectDesignManager / projectManager convert at the
// call boundary so this Engine imports NO projectstate. Per the
// settlement/billing/operationestimation precedent the slim ProjectOption /
// Network carry only the fields THIS Engine actually reads.
// ---------------------------------------------------------------------------

// Money is an exact integer-minor-units amount plus an ISO-4217 currency. NEVER a
// float. Mirrors projectstate.Money. The build cost facet of ConstructionEstimate.

// signed minor units, e.g. 1299 == 12.99
// ISO-4217, e.g. "USD"

// OptionID identifies one assembled ProjectOption within an SDP review. Mirrors
// projectstate.OptionID (carried for audit/labeling only — the math ignores it).

// OptionActivity is one activity in an option's CPM network — effort in 5-day
// quanta, its worker class, whether it sits on the critical path, and its
// Fibonacci risk bucket. Mirrors projectstate.OptionActivity.

// 1,2,3,5,8,13 (Fibonacci) — higher == riskier

// ActivityNetwork is the option's activity graph as the Engine needs it: the flat
// activity set with effort, worker class, critical-path membership and risk bucket.
// Mirrors projectstate.ActivityNetwork.

// WorkerMix is the option's worker-class build-cost rates (per person-day) plus
// the staffing cap that bounds parallelism. Mirrors projectstate.WorkerMix.

// build cost per person-day, by worker class
// max concurrent staff (parallelism bound)

// ProjectOption is the SLIM input to EstimateForOption: the committed project
// option as THIS Engine needs it. The canonical projectstate.ProjectOption carries
// the settlement Terms, declared usage, infrastructure kind, and solution kind too —
// none of which the construction estimate reads — so per the settlement/billing
// precedent only the construction-side slice crosses. The projectDesignManager
// converts the canonical option at the call boundary.

// ActivityItem is one activity in the Phase-2 activity list as ComputeNetwork needs
// it — its network id (Name) and effort in 5-day quanta. The canonical
// projectstate.ActivityItem carries worker class / coding / risk-bucket / title too,
// none of which the CPM solve reads, so the slim mirror carries only Name + Effort.

// ActivityList is the Phase-2 activity list as ComputeNetwork needs it. Mirrors
// projectstate.ActivityList (slim — see ActivityItem).

// NetworkDependency declares that one activity depends on a set of predecessors.
// Mirrors projectstate.NetworkDependency.

// NetworkMilestone is one authored zero-duration event node on the project network
// as ComputeNetwork needs it — its id and its predecessor fan-in. The canonical
// projectstate.NetworkMilestone carries name/public + the computed on-CP/event-time
// pointers too; the CPM solve reads only ID + DependsOn (it COMPUTES the rest), so
// the slim mirror carries only those. Milestones are EXCLUDED from the risk
// decomposition (they carry no effort and no risk bucket).

// predecessor activity ids (the fan-in)

// Network is the Phase-2 project network as ComputeNetwork needs it: the AUTHORED
// activity dependencies and milestone fan-in. The canonical projectstate.Network
// also carries the authored criticalPath[] and the compute-at-read Computed/Summary
// block; the CPM solve reads only Dependencies + Milestones (it COMPUTES the rest),
// so the slim mirror carries only those.

// topFibonacciBucket is the highest Fibonacci risk bucket (contract activity-risk
// normalization divides the per-activity bucket sum by count × 13.0).
const topFibonacciBucket = 13.0

// maxCalendarStretch caps the calendar stretch factor so a pathological
// CalendarDaysPerWeek (e.g. fractional days/week) cannot inflate the duration
// without bound. 5 d/wk → 1.0; 1 d/wk → 5.0; capped at 7.0.
const maxCalendarStretch = 7.0

// RiskScore is the composite construction-risk decomposition for one option.
// The decomposition is exposed (not folded away) because the SDP review renders
// criticality and activity risk separately (contract §3; risk.md 81-83).

// overall composite risk in [0,1]
// fraction of activities on the critical path, in [0,1]
// normalized weighted Fibonacci-bucket activity risk in [0,1]

// ConstructionEstimate is the sole output of EstimateForOption — the
// construction-side SDP row. Construction-side ONLY: no operating cost, no
// payout, no settled net (contract §4 Non-goals 1-2). The three facets are
// mutually consistent because they are produced from one pass over one network.

// CPM critical-path length in sim-days at the option's worker mix
// Σ activity effort person-days × worker-class build rate

// EstimationEngine is the construction-estimation facet over the EstimationModel
// volatility. One behavioural operation (contract §2 — 1-op count investigated &
// waived; matches the autoscalerEngine precedent).

// EstimateForOption computes the construction duration, build cost, and risk
// for one project option. Pure and deterministic: identical option ->
// identical ConstructionEstimate, always.
//
// The error is *fweng.Error and signals programmer/contract misuse ONLY:
//   - ContractMisuse: the option has no activities, an activity has negative
//     effort, references a worker class with no rate, or the rates have a
//     mixed/empty currency. (A projectDesignManager bug — it failed to
//     assemble a valid option before calling.)
//   - InternalInvariant: a computed risk component fell outside [0,1] or the
//     duration came out negative (an engine bug — a guard).

// ComputeNetwork runs the read-side CPM solve over the project network: per-node
// ES/EF/LS/LF, total/free float, on-critical-path, near-critical, criticality band
// (a Policy Strategy ON this Engine), topological column, the computed milestone
// event nodes, and the project summary. Pure and deterministic. It is the server-
// side home of the math the webApp formerly ran client-side (toNetworkView), called
// at read time by the projectManager. See network.go for the contract + the band
// Strategy. An empty (unauthored) network is a normal empty result, not an error.

// The concrete, stateless EstimationEngine — EstimationEngineImpl — and its
// constructor NewEstimationEngine() are GENERATED into contract.gen.go. No fields =>
// no mutable state => trivially deterministic and reentrant. The behaviour below is
// hand-written on the generated struct (across estimation.go, network.go,
// earnedvalue.go).

// EstimateForOption implements EstimationEngine. It runs in one pass over the
// option's activity network so the three returned facets stay mutually
// consistent (contract §2.2, §8 Variant B).
func (EstimationEngineImpl) EstimateForOption(_ fweng.Context, option ProjectOption) (ConstructionEstimate, error) {
	activities := option.Network.Activities

	// --- ContractMisuse pre-conditions (programmer error, not a domain result) ---
	if len(activities) == 0 {
		return ConstructionEstimate{}, fweng.New(fweng.ContractMisuse,
			"EstimateForOption: option network has zero activities")
	}
	rates := option.WorkerMix.ClassRates
	for i, a := range activities {
		if a.EffortDays < 0 {
			return ConstructionEstimate{}, fweng.New(fweng.ContractMisuse,
				"EstimateForOption: activity "+activityRef(a, i)+" has negative EffortDays")
		}
		if _, ok := rates[a.WorkerClass]; !ok {
			return ConstructionEstimate{}, fweng.New(fweng.ContractMisuse,
				"EstimateForOption: activity "+activityRef(a, i)+
					" references WorkerClass "+quote(a.WorkerClass)+" with no rate in WorkerMix.ClassRates")
		}
	}

	// --- Build cost: Σ (effort person-days × worker-class build rate). The cost
	// currency is the single shared currency of the participating rates; a mixed
	// or empty currency is a ContractMisuse (the Manager mis-assembled the mix). ---
	var totalMinorUnits int64
	currency := ""
	for i, a := range activities {
		rate := rates[a.WorkerClass]
		if rate.Currency == "" {
			return ConstructionEstimate{}, fweng.New(fweng.ContractMisuse,
				"EstimateForOption: rate for WorkerClass "+quote(a.WorkerClass)+" has empty currency")
		}
		if currency == "" {
			currency = rate.Currency
		} else if rate.Currency != currency {
			return ConstructionEstimate{}, fweng.New(fweng.ContractMisuse,
				"EstimateForOption: mixed rate currencies ("+quote(currency)+" vs "+
					quote(rate.Currency)+") at activity "+activityRef(a, i))
		}
		// Integer minor-units cost: effort person-days × per-person-day rate.
		// Deterministic float→int truncation (no rounding mode ambiguity).
		totalMinorUnits += int64(a.EffortDays * float64(rate.MinorUnits))
	}
	buildCost := Money{MinorUnits: totalMinorUnits, Currency: currency}

	// --- Duration: CPM critical-path length in sim-days. ---
	durationDays := criticalPathDays(activities, option.WorkerMix.StaffingCap)
	// Calendar stretch: a 5 d/wk team progresses faster than a 2 d/wk team, so a
	// lower CalendarDaysPerWeek stretches the sim-day duration. 5 d/wk => 1.0.
	durationDays *= calendarStretch(option.CalendarDaysPerWeek)

	// --- Risk decomposition. ---
	count := float64(len(activities))
	criticalCount := 0
	var bucketSum int64
	for _, a := range activities {
		if a.OnCriticalPath {
			criticalCount++
		}
		bucketSum += a.RiskBucket
	}
	criticalityRisk := float64(criticalCount) / count
	activityRisk := clamp01(float64(bucketSum) / (count * topFibonacciBucket))
	composite := clamp01(0.5*criticalityRisk + 0.5*activityRisk)

	// --- InternalInvariant guards: a bug if any of these holds post-computation. ---
	if durationDays < 0 {
		return ConstructionEstimate{}, fweng.New(fweng.InternalInvariant,
			"EstimateForOption: computed negative DurationDays")
	}
	if out01(criticalityRisk) || out01(activityRisk) || out01(composite) {
		return ConstructionEstimate{}, fweng.New(fweng.InternalInvariant,
			"EstimateForOption: a computed risk component fell outside [0,1]")
	}

	return ConstructionEstimate{
		DurationDays: durationDays,
		BuildCost:    buildCost,
		Risk: RiskScore{
			Composite:       composite,
			CriticalityRisk: criticalityRisk,
			ActivityRisk:    activityRisk,
		},
	}, nil
}

// criticalPathDays returns the construction critical-path length in sim-days.
//
// The option's ActivityNetwork is a flat activity set whose critical-path
// membership is already flagged (OptionActivity.OnCriticalPath), so the CPM
// critical-path length is the sum of effort over flagged activities. If NO
// activity is flagged, fall back to a parallelism estimate: total effort spread
// across the staffing cap (max(StaffingCap, 1) workers). Deterministic — no
// tie-breaking ambiguity because the input ordering is not consulted.
func criticalPathDays(activities []OptionActivity, staffingCap int64) float64 {
	var criticalDays, totalDays float64
	anyCritical := false
	for _, a := range activities {
		totalDays += a.EffortDays
		if a.OnCriticalPath {
			anyCritical = true
			criticalDays += a.EffortDays
		}
	}
	if anyCritical {
		return criticalDays
	}
	cap := staffingCap
	if cap < 1 {
		cap = 1
	}
	return totalDays / float64(cap)
}

// calendarStretch maps the option's working days/week to a duration multiplier:
// 5.0 / max(CalendarDaysPerWeek, 1), capped at maxCalendarStretch. A 5 d/wk team
// is the 1.0 baseline; a 2 d/wk team stretches by 2.5×.
func calendarStretch(calendarDaysPerWeek float64) float64 {
	d := calendarDaysPerWeek
	if d < 1 {
		d = 1
	}
	s := 5.0 / d
	if s > maxCalendarStretch {
		s = maxCalendarStretch
	}
	return s
}

// clamp01 clamps x into [0,1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// out01 reports whether x lies outside [0,1] (NaN counts as outside).
func out01(x float64) bool { return !(x >= 0 && x <= 1) }

// activityRef renders a stable human reference to an activity for error detail.
func activityRef(a OptionActivity, idx int) string {
	if a.ActivityId != "" {
		return quote(a.ActivityId)
	}
	return "#" + itoa(idx)
}

// quote wraps s in double quotes for readable error detail (no fmt dependency
// needed for this single use, keeping the import set minimal).
func quote(s string) string { return "\"" + s + "\"" }

// itoa renders a small non-negative int without importing strconv/fmt.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
