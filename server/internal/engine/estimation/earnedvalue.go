package estimation

// earnedvalue.go is the read-side EARNED-VALUE facet of the constructionEstimationEngine:
// a PURE, deterministic CPM forward pass that schedules each activity's finish week, then
// builds the cumulative planned + earned curves and the schedule-performance index (SPI).
// It is the server-side home of the math the webApp formerly ran client-side (web
// constructionrows.go::computeEV), moved onto the Engine per the founder gate so the SPA
// renders authoritative server-computed figures rather than re-deriving them.
//
// CONTRACT (project.json .serviceContracts.constructionEstimationEngine): ComputeEarnedValue(
// activities, network, integrated, totalWeeks, calendarDaysPerWeek) → {weeks, earned,
// planned, spi}.
//
// LAYER DISCIPLINE (unchanged from estimation.go / network.go): Engine layer — pure,
// deterministic, in-workflow. NO I/O, NO time, NO rand, NO goroutines, NO outbound calls.
// Imports ONLY its OWN generated contract types and the framework-go Engine error model.
// The projectManager calls this directly at read time; its determinism is what makes that
// safe (identical inputs → identical EVCurve, always).

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// defaultCalendarDaysPerWeek is the standard 5-day workweek fallback when the caller
// passes a non-positive CalendarDaysPerWeek (mirrors the retired web computeEV default).
const defaultCalendarDaysPerWeek = 5

// ComputeEarnedValue runs a CPM forward pass to schedule each activity's earliest finish
// (in days, via longest path over the network dependencies), buckets each finish into its
// scheduled week, then builds the cumulative PLANNED curve (all activities, by scheduled
// finish) and EARNED curve (only the integrated activities, by scheduled finish), both as
// a percentage of total effort. SPI = earned/planned at the current (last) week.
//
// integrated is the set of activity ids (network names) that are integrated/credited; an
// activity not in the set contributes to planned but not earned. totalWeeks bounds the
// curve length; when non-positive it is derived from the latest scheduled finish.
//
// Pure and deterministic: identical inputs → identical EVCurve, always (the curve is keyed
// by week index, never by input ordering). An empty activity list yields zero-valued
// curves with a single week-0 sample — a normal DOMAIN result, not an error. The error is
// *fweng.Error and is reserved for contract misuse; this computation never raises one.
func (engine) ComputeEarnedValue(_ fweng.Context, activities ActivityList, network Network, integrated []string, totalWeeks int64, calendarDaysPerWeek int64) (EVCurve, error) {
	calDaysPerWeek := int(calendarDaysPerWeek)
	if calDaysPerWeek <= 0 {
		calDaysPerWeek = defaultCalendarDaysPerWeek
	}

	integratedSet := make(map[string]bool, len(integrated))
	for _, id := range integrated {
		integratedSet[id] = true
	}

	effort := map[string]float64{}
	depsOf := map[string][]string{}
	var total float64
	for _, a := range activities.Activities {
		effort[a.Name] = a.EffortDays
		total += a.EffortDays
	}
	for _, d := range network.Dependencies {
		depsOf[d.Activity] = d.DependsOn
	}

	// memoized earliest-finish (in days) via longest path.
	finish := map[string]float64{}
	var ef func(string) float64
	ef = func(n string) float64 {
		if v, ok := finish[n]; ok {
			return v
		}
		finish[n] = 0 // cycle guard
		start := 0.0
		for _, p := range depsOf[n] {
			if pf := ef(p); pf > start {
				start = pf
			}
		}
		v := start + effort[n]
		finish[n] = v
		return v
	}
	for _, a := range activities.Activities {
		ef(a.Name)
	}

	tw := int(totalWeeks)
	if tw <= 0 {
		tw = 1
		for _, a := range activities.Activities {
			if w := int(finish[a.Name])/calDaysPerWeek + 1; w > tw {
				tw = w
			}
		}
	}

	weeks := make([]int64, tw+1)
	planned := make([]float64, tw+1)
	earned := make([]float64, tw+1)
	for w := 0; w <= tw; w++ {
		weeks[w] = int64(w)
		var p, e float64
		for _, a := range activities.Activities {
			fw := int(finish[a.Name]) / calDaysPerWeek
			if fw <= w {
				p += effort[a.Name]
				if integratedSet[a.Name] {
					e += effort[a.Name]
				}
			}
		}
		if total > 0 {
			planned[w] = p / total * 100
			earned[w] = e / total * 100
		}
	}

	spi := 0.0
	if planned[tw] > 0 {
		spi = earned[tw] / planned[tw]
	}

	return EVCurve{Weeks: weeks, Earned: earned, Planned: planned, SPI: spi}, nil
}
