// Package estimation implements the estimationEngine component — the Engine that
// encapsulates the construction-side EstimationModel volatility (HOW construction
// duration, cost, and risk are computed for one project option — EstimateForOption)
// and the read-side CPM network solve used to render the project's critical path
// (ComputeNetwork, architecture.dsl:695) via a criticality-BAND classification
// Strategy (defaultBandPolicy). ComputeNetwork is the server-side home of the math
// the webApp formerly ran client-side (api/projectAdapters.ts::toNetworkView), moved
// onto the Engine per the founder gate (2026-06-19) so the SPA renders authoritative
// server-computed figures rather than re-deriving them.
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
//     NO Temporal — its determinism is what makes the projectDesignManager's and
//     projectManager's direct in-workflow calls replay-safe (contract §6).
//
// A failing computation is a DOMAIN RESULT (a normal return value — e.g. a
// zero/edge estimate for a degenerate option), NOT an error. The *fweng.Error
// channel is reserved for programmer / contract misuse ONLY (nil/structurally
// invalid input — fweng.ContractMisuse) and broken engine invariants
// (fweng.InternalInvariant). See contract §3 "Error model".
package estimation

import (
	"sort"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// floatEpsilon absorbs float64 rounding on the total-float (LS−ES) subtraction so the
// standard on-critical-path test (totalFloat ≈ 0) is robust. Activity efforts are whole
// 5-day quanta so exact-zero is the norm; the epsilon is a guard, not a tolerance band.
const floatEpsilon = 1e-9

// bandPolicy is the float-criticality band classification Strategy (Löwy ch.8 §2). The
// thresholds are ABSOLUTE day-counts kept on the policy so they are TUNABLE without
// touching the solve: a node ON the critical path is `critical`; otherwise `red` at
// float ≤ RedMaxDays, `yellow` at RedMaxDays < float ≤ YellowMaxDays, `green` above.
// The near-critical flag (off-CP but tight) keys off the SAME RedMaxDays boundary so
// the band colour-coding and the near-critical roll-up never drift apart.
type bandPolicy struct {
	// RedMaxDays is the inclusive upper bound of the red (near-critical) band, in days.
	RedMaxDays float64
	// YellowMaxDays is the inclusive upper bound of the yellow band, in days; above it
	// is green.
	YellowMaxDays float64
}

// defaultBandPolicy is the launch band Strategy — the absolute thresholds the retired
// client-side toNetworkView used (red ≤ 5d, yellow 6–25d, green ≥ 26d), now the single
// server-side source of truth.
var defaultBandPolicy = bandPolicy{RedMaxDays: 5, YellowMaxDays: 25}

// Band band names (the wire-stable strings the SPA colour-codes on).
const (
	bandCritical = "critical"
	bandRed      = "red"
	bandYellow   = "yellow"
	bandGreen    = "green"
)

// classify returns the float-criticality band for a node. onCriticalPath wins; else the
// absolute float thresholds apply.
func (p bandPolicy) classify(onCriticalPath bool, totalFloat float64) string {
	if onCriticalPath {
		return bandCritical
	}
	if totalFloat <= p.RedMaxDays {
		return bandRed
	}
	if totalFloat <= p.YellowMaxDays {
		return bandYellow
	}
	return bandGreen
}

// nearCritical reports whether an off-CP node falls inside the near-critical band
// (float ≤ RedMaxDays). On-CP nodes are never near-critical (they ARE critical).
func (p bandPolicy) nearCritical(onCriticalPath bool, totalFloat float64) bool {
	return !onCriticalPath && totalFloat <= p.RedMaxDays
}

// ComputeNetwork is the read-side CPM op the projectManager calls per read. It runs a
// forward/backward pass over (activities × dependencies), classifies each node into its
// criticality band via policy, computes the milestone event nodes (zero-duration; event
// time = max predecessor earliest-finish; EXCLUDED from risk), and rolls up the project
// summary. It is the server-side replacement for the client's toNetworkView.
//
// Pure and deterministic: identical inputs → identical NetworkSolution, always (the
// activity universe and topological order are deterministically sorted, never input-
// ordering-dependent). The authored CriticalPath names seed the on-CP membership; a node
// is on the CP if it is named there OR its total float is ≤ 0 (a zero-float node the
// authored list omitted is still surfaced as critical).
//
// The error is *fweng.Error and signals contract misuse ONLY (a nil-ish/degenerate
// input the Manager should never assemble): an InternalInvariant guard catches a
// computed negative duration. An empty network is a normal DOMAIN result (an empty
// solution), NOT an error — a project may be read before its network is authored.
func (EstimationEngineImpl) ComputeNetwork(_ fweng.Context, activities ActivityList, network Network) (NetworkSolution, error) {
	effortByName := make(map[string]ActivityItem, len(activities.Activities))
	for _, a := range activities.Activities {
		effortByName[a.Name] = a
	}

	deps := network.Dependencies

	// Milestones are FIRST-CLASS zero-duration nodes in the SAME CPM graph as activities
	// (standard rule, 2026-06-19): with the fan-out topology a milestone has real
	// predecessors (its fan-in) AND real successors (downstream nodes that dependOn it),
	// so it flows through one forward/backward pass and gets ES/EF/LS/LF/float NATURALLY —
	// its on-CP is then the SAME textbook test as any node (totalFloat ≈ 0). There is no
	// bespoke milestone on-CP rule. A milestone's duration is 0, so its eventTime ==
	// earliestStart == earliestFinish.
	milestoneIDs := map[string]struct{}{}
	for _, m := range network.Milestones {
		milestoneIDs[m.Id] = struct{}{}
	}

	idSet := buildNodeUniverse(deps, network.Milestones)
	if len(idSet) == 0 {
		// No network authored yet — a normal empty result, not an error.
		return NetworkSolution{Nodes: map[string]NetworkNode{}, Milestones: nil, Summary: NetworkSummary{}}, nil
	}

	predecessors, successors := buildAdjacency(idSet, deps, network.Milestones)

	dur := func(id string) float64 {
		if _, isMilestone := milestoneIDs[id]; isMilestone {
			return 0 // milestones are zero-duration events
		}
		return effortByName[id].EffortDays // zero value when the activity is unknown
	}

	order := topoOrder(idSet, predecessors)

	earlyStart, earlyFinish, projectDuration := forwardPass(order, predecessors, dur)
	lateFinish, lateStart := backwardPass(order, successors, dur, projectDuration)
	col := computeColumns(order, predecessors)

	// Build the per-activity CPM facets (float test + free float + band), which also yields
	// the activityOnCP map the milestone solver needs. Milestones are excluded here; their
	// facets come from solveMilestonesOnCP (determining-predecessor rule) below.
	nodes, activityOnCP := buildActivityNodes(order, milestoneIDs, earlyStart, earlyFinish, lateStart, lateFinish, col, successors, projectDuration)

	// Milestone solutions: eventTime == the milestone node's earliestFinish (duration 0,
	// already milestone-aware via the unified pass — chaining works). onCriticalPath uses
	// the DETERMINING-PREDECESSOR rule (architect + team-lead, 2026-06-19), NOT the float
	// test — a milestone MARKS reaching a point, so its criticality is the criticality of
	// the achievement that gates it, not the slack of a dead-end sink.
	milestones := solveMilestonesOnCP(network.Milestones, milestoneIDs, predecessors, earlyFinish, activityOnCP, projectDuration)

	summary := summarizeActivityNodes(nodes, projectDuration)

	if projectDuration < 0 {
		return NetworkSolution{}, fweng.New(fweng.InternalInvariant,
			"ComputeNetwork: computed negative project duration")
	}

	return NetworkSolution{Nodes: nodes, Milestones: milestones, Summary: summary}, nil
}

// buildNodeUniverse collects the node universe = everything named in activity dependencies
// (activities + their predecessors) PLUS every milestone id and its fan-in, so a node with
// no declared row — or a milestone with no fan-out — still appears.
func buildNodeUniverse(deps []NetworkDependency, milestones []NetworkMilestone) map[string]struct{} {
	idSet := map[string]struct{}{}
	for _, d := range deps {
		idSet[d.Activity] = struct{}{}
		for _, p := range d.DependsOn {
			idSet[p] = struct{}{}
		}
	}
	for _, m := range milestones {
		idSet[m.Id] = struct{}{}
		for _, p := range m.DependsOn {
			idSet[p] = struct{}{}
		}
	}
	return idSet
}

// buildAdjacency builds the predecessor + successor edge maps over the node universe:
// activity dependency edges plus milestone fan-IN edges (a milestone's dependsOn).
// Milestone fan-OUT edges are already captured as ordinary activity deps that name the
// milestone as a predecessor (e.g. a design root dependsOn M0).
func buildAdjacency(idSet map[string]struct{}, deps []NetworkDependency, milestones []NetworkMilestone) (predecessors, successors map[string][]string) {
	predecessors = map[string][]string{}
	successors = map[string][]string{}
	for id := range idSet {
		predecessors[id] = nil
		successors[id] = nil
	}
	for _, d := range deps {
		for _, p := range d.DependsOn {
			predecessors[d.Activity] = append(predecessors[d.Activity], p)
			successors[p] = append(successors[p], d.Activity)
		}
	}
	for _, m := range milestones {
		for _, p := range m.DependsOn {
			predecessors[m.Id] = append(predecessors[m.Id], p)
			successors[p] = append(successors[p], m.Id)
		}
	}
	return predecessors, successors
}

// forwardPass computes earliest start / finish per node and the project duration (the
// longest earliestFinish over all nodes).
func forwardPass(order []string, predecessors map[string][]string, dur func(string) float64) (earlyStart, earlyFinish map[string]float64, projectDuration float64) {
	earlyStart = map[string]float64{}
	earlyFinish = map[string]float64{}
	for _, id := range order {
		es := 0.0
		for _, p := range predecessors[id] {
			if earlyFinish[p] > es {
				es = earlyFinish[p]
			}
		}
		earlyStart[id] = es
		earlyFinish[id] = es + dur(id)
	}
	for _, ef := range earlyFinish {
		if ef > projectDuration {
			projectDuration = ef
		}
	}
	return earlyStart, earlyFinish, projectDuration
}

// backwardPass computes latest finish / start per node given the project duration.
func backwardPass(order []string, successors map[string][]string, dur func(string) float64, projectDuration float64) (lateFinish, lateStart map[string]float64) {
	lateFinish = map[string]float64{}
	lateStart = map[string]float64{}
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		succ := successors[id]
		var lf float64
		if len(succ) == 0 {
			lf = projectDuration
		} else {
			lf = projectDuration
			first := true
			for _, s := range succ {
				if first || lateStart[s] < lf {
					lf = lateStart[s]
					first = false
				}
			}
		}
		lateFinish[id] = lf
		lateStart[id] = lf - dur(id)
	}
	return lateFinish, lateStart
}

// computeColumns assigns each node its longest-path depth from a source (topological
// layering for the swimlanes).
func computeColumns(order []string, predecessors map[string][]string) map[string]int64 {
	col := map[string]int64{}
	for _, id := range order {
		var c int64
		for _, p := range predecessors[id] {
			if col[p]+1 > c {
				c = col[p] + 1
			}
		}
		col[id] = c
	}
	return col
}

// freeFloatOf computes a node's free float: min over successors of
// (succ.earliestStart - this.earliestFinish); a sink's free float = projectDuration -
// this.earliestFinish. Clamped to [0, total] so it never exceeds total float (a guard the
// SPA relies on for the band colouring).
func freeFloatOf(ef, total float64, successors []string, earlyStart map[string]float64, projectDuration float64) float64 {
	free := projectDuration - ef
	if len(successors) > 0 {
		free = projectDuration
		first := true
		for _, s := range successors {
			slack := earlyStart[s] - ef
			if first || slack < free {
				free = slack
				first = false
			}
		}
	}
	if free < 0 {
		free = 0
	}
	if free > total {
		free = total
	}
	return free
}

// buildActivityNodes builds the per-activity CPM node facets and the activityOnCP map. Both
// activities and milestones flow through the same forward/backward pass, but milestones are
// EXCLUDED from the Nodes map (they carry no effort/bucket and risk never sees them);
// milestone on-CP is computed separately by solveMilestonesOnCP. Activity on-CP is the
// standard float test — total float ≈ 0 (textbook CPM); floatEpsilon absorbs float64
// rounding on the LS−ES subtraction.
func buildActivityNodes(order []string, milestoneIDs map[string]struct{}, earlyStart, earlyFinish, lateStart, lateFinish map[string]float64, col map[string]int64, successors map[string][]string, projectDuration float64) (map[string]NetworkNode, map[string]bool) {
	nodes := make(map[string]NetworkNode, len(order))
	activityOnCP := map[string]bool{}
	for _, id := range order {
		es := earlyStart[id]
		ls := lateStart[id]
		total := ls - es
		onCP := total <= floatEpsilon

		if _, isMilestone := milestoneIDs[id]; isMilestone {
			continue // milestones are not activity nodes; their facets come from solveMilestonesOnCP
		}
		activityOnCP[id] = onCP

		ef := earlyFinish[id]
		free := freeFloatOf(ef, total, successors[id], earlyStart, projectDuration)

		nodes[id] = NetworkNode{
			EarliestStart:  es,
			EarliestFinish: ef,
			LatestStart:    ls,
			LatestFinish:   lateFinish[id],
			TotalFloat:     total,
			FreeFloat:      free,
			OnCriticalPath: onCP,
			NearCritical:   defaultBandPolicy.nearCritical(onCP, total),
			Band:           defaultBandPolicy.classify(onCP, total),
			Column:         col[id],
		}
	}
	return nodes, activityOnCP
}

// summarizeActivityNodes rolls up the project-level CPM summary over the ACTIVITY nodes
// (milestones excluded — they are events, not work). criticalPathDays == projectDuration
// (the longest path IS the CP length; it is NOT the sum of CP-activity durations, which
// double-counts parallel branches).
func summarizeActivityNodes(nodes map[string]NetworkNode, projectDuration float64) NetworkSummary {
	var cpCount int64
	var nearCount int64
	maxFloat := 0.0
	for _, n := range nodes {
		if n.OnCriticalPath {
			cpCount++
		}
		if n.NearCritical {
			nearCount++
		}
		if n.TotalFloat > maxFloat {
			maxFloat = n.TotalFloat
		}
	}
	return NetworkSummary{
		TotalDurationDays:         projectDuration,
		CriticalPathActivityCount: cpCount,
		CriticalPathDays:          projectDuration,
		MaxFloat:                  maxFloat,
		NearCriticalCount:         nearCount,
	}
}

// solveMilestonesOnCP computes each milestone's eventTime + onCriticalPath via the
// DETERMINING-PREDECESSOR rule (architect + team-lead ruling, 2026-06-19). A milestone
// MARKS reaching a point; its criticality is the criticality of the achievement that
// gates it, NOT the slack of a dead-end sink — so the float test (which treats a fan-in
// sink as off-CP) is wrong for markers. The rule:
//
//   - eventTime = the milestone node's earliestFinish (already milestone-aware from the
//     unified forward pass: a milestone-predecessor contributes its OWN eventTime, so
//     chains like N-DOGFOOD → M5 resolve to 155, not 0).
//   - DETERMINING predecessor = the predecessor whose finish time SETS the eventTime (the
//     max-earliestFinish fan-in node); on a tie, prefer an on-CP predecessor so the
//     marker reflects the critical achievement.
//   - onCriticalPath = the determining predecessor is on-CP (an activity's float-based
//     on-CP, or a milestone-predecessor's already-computed on-CP), PLUS two conventions:
//     (a) ROOT: a milestone with no predecessors (M0, eventTime 0) is on-CP — the
//     project-start gate sits at the CP origin.
//     (b) POST-TERMINAL: a milestone at the project frontier (eventTime ≥ projectDuration)
//     whose determining predecessor is ANOTHER MILESTONE is a post-v1 marker
//     (N-DOGFOOD → M5) and is forced OFF-CP — it is not part of the v1 critical path.
//     The terminal RELEASE milestone (M5) is distinguished because its determining
//     predecessor is an ACTIVITY at the frontier, so it stays on-CP.
//
// Milestones are resolved in dependency order (a milestone depending on another milestone
// is solved after it). Authored list order is preserved in the returned slice.
func solveMilestonesOnCP(
	authored []NetworkMilestone,
	milestoneIDs map[string]struct{},
	predecessors map[string][]string,
	earlyFinish map[string]float64,
	activityOnCP map[string]bool,
	projectDuration float64,
) []NetworkMilestoneSolution {
	if len(authored) == 0 {
		return nil
	}

	solved := map[string]NetworkMilestoneSolution{}

	// Resolve in dependency order (fixpoint): a milestone whose milestone-predecessors are
	// all solved is resolvable; iterate until no progress, then force the remainder (breaks
	// any authoring cycle deterministically).
	pending := make([]NetworkMilestone, len(authored))
	copy(pending, authored)
	for len(pending) > 0 {
		progressed := false
		var still []NetworkMilestone
		for _, m := range pending {
			if milestoneReady(m, milestoneIDs, solved) {
				solved[m.Id] = solveMilestone(m, milestoneIDs, earlyFinish, activityOnCP, solved, projectDuration)
				progressed = true
			} else {
				still = append(still, m)
			}
		}
		if !progressed {
			for _, m := range still {
				if _, done := solved[m.Id]; !done {
					solved[m.Id] = solveMilestone(m, milestoneIDs, earlyFinish, activityOnCP, solved, projectDuration)
				}
			}
			break
		}
		pending = still
	}

	out := make([]NetworkMilestoneSolution, 0, len(authored))
	for _, m := range authored {
		out = append(out, solved[m.Id])
	}
	return out
}

// milestoneReady reports whether all of a milestone's MILESTONE-predecessors are already
// solved (activity predecessors are always available from the forward pass), so it can be
// resolved in the dependency-order fixpoint.
func milestoneReady(m NetworkMilestone, milestoneIDs map[string]struct{}, solved map[string]NetworkMilestoneSolution) bool {
	for _, p := range m.DependsOn {
		if _, isMilestone := milestoneIDs[p]; isMilestone {
			if _, done := solved[p]; !done {
				return false
			}
		}
	}
	return true
}

// milestonePredecessorOnCP returns a predecessor's on-CP: an activity's float-based on-CP,
// or a milestone-predecessor's already-solved on-CP (false if not yet solved — the
// dependency-order pass ensures it is, except across an authoring cycle).
func milestonePredecessorOnCP(id string, milestoneIDs map[string]struct{}, solved map[string]NetworkMilestoneSolution, activityOnCP map[string]bool) bool {
	if _, isMilestone := milestoneIDs[id]; isMilestone {
		return solved[id].OnCriticalPath
	}
	return activityOnCP[id]
}

// solveMilestone computes one milestone's event time + on-CP via the determining-predecessor
// rule. eventTime == the milestone node's earliestFinish (milestone-aware EF, chaining
// already folded in). ROOT (no predecessors) ⇒ on-CP.
func solveMilestone(m NetworkMilestone, milestoneIDs map[string]struct{}, earlyFinish map[string]float64, activityOnCP map[string]bool, solved map[string]NetworkMilestoneSolution, projectDuration float64) NetworkMilestoneSolution {
	event := earlyFinish[m.Id] // milestone-aware EF (chaining already folded in)

	// ROOT convention: no predecessors ⇒ the project-start gate, on-CP.
	if len(m.DependsOn) == 0 {
		return NetworkMilestoneSolution{ID: m.Id, OnCriticalPath: true, EventTime: event}
	}

	// Find the DETERMINING predecessor: max finish time; on a tie prefer an on-CP one.
	var detID string
	detFinish := -1.0
	detOnCP := false
	for _, p := range m.DependsOn {
		var finish float64
		if _, isMilestone := milestoneIDs[p]; isMilestone {
			finish = solved[p].EventTime
		} else {
			finish = earlyFinish[p]
		}
		pOnCP := milestonePredecessorOnCP(p, milestoneIDs, solved, activityOnCP)
		if finish > detFinish || (finish == detFinish && pOnCP && !detOnCP) {
			detID = p
			detFinish = finish
			detOnCP = pOnCP
		}
	}

	onCP := detOnCP

	// POST-TERMINAL convention: a frontier milestone whose determining predecessor is
	// itself a MILESTONE is a post-v1 marker (e.g. N-DOGFOOD → M5) ⇒ force off-CP. The
	// terminal release milestone is gated by an ACTIVITY at the frontier, so it is
	// unaffected and stays on-CP.
	if event >= projectDuration-floatEpsilon {
		if _, detIsMilestone := milestoneIDs[detID]; detIsMilestone {
			onCP = false
		}
	}

	return NetworkMilestoneSolution{ID: m.Id, OnCriticalPath: onCP, EventTime: event}
}

// topoOrder is Kahn's topological order over the predecessor map, deterministic via a
// sorted ready-queue; any cycle remnant is appended in sorted order so no node is
// dropped (mirrors the retired client topoOrder's resilience).
func topoOrder(idSet map[string]struct{}, predecessors map[string][]string) []string {
	indeg := map[string]int{}
	for id := range idSet {
		indeg[id] = len(predecessors[id])
	}
	succ := map[string][]string{}
	for id, preds := range predecessors {
		for _, p := range preds {
			succ[p] = append(succ[p], id)
		}
	}

	var queue []string
	for id := range idSet {
		if indeg[id] == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	var out []string
	seen := map[string]struct{}{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		out = append(out, id)
		seen[id] = struct{}{}
		var ready []string
		for _, s := range succ[id] {
			indeg[s]--
			if indeg[s] == 0 {
				ready = append(ready, s)
			}
		}
		if len(ready) > 0 {
			queue = append(queue, ready...)
			sort.Strings(queue)
		}
	}
	// Any nodes left (a cycle) appended deterministically so nothing is dropped.
	var remnant []string
	for id := range idSet {
		if _, ok := seen[id]; !ok {
			remnant = append(remnant, id)
		}
	}
	sort.Strings(remnant)
	return append(out, remnant...)
}

// maxCalendarStretch caps the calendar stretch factor so a pathological
// CalendarDaysPerWeek (e.g. fractional days/week) cannot inflate the duration
// without bound. 5 d/wk → 1.0; 1 d/wk → 5.0; capped at 7.0.
const maxCalendarStretch = 7.0

// RiskScore is exposed as a decomposition (Criticality + ActivityRisk, not folded
// into one number) because the SDP review renders them separately (risk.md 81-83).

// EstimationEngineImpl and NewEstimationEngine are generated (contract.gen.go); the
// behaviour below is hand-written on that generated struct.

// EstimateForOption implements EstimationEngine. It runs in one pass over the
// option's activity network so the three returned facets stay mutually
// consistent (contract §2.2, §8 Variant B).
//
// The error is *fweng.Error and signals programmer/contract misuse ONLY:
//   - ContractMisuse: the option has no activities, an activity has negative
//     effort, references a worker class with no rate, or the rates have a
//     mixed/empty currency. (A projectDesignManager bug — it failed to
//     assemble a valid option before calling.)
//   - InternalInvariant: a computed risk component fell outside [0,1] or the
//     duration came out negative (an engine bug — a guard).
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

	// --- Cost currency: the single shared currency of the participating rates; a mixed
	// or empty currency is a ContractMisuse (the Manager mis-assembled the mix). ---
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
	}

	deps, milestones := option.Network.Dependencies, option.Network.Milestones
	cap := option.WorkerMix.StaffingCap

	// --- Top-resource compression (Löwy ch.9 §2/§3; Phase-2 rework F5e). CriticalSpeedup
	// s>1 assigns faster "top resources" to the RESOURCE-CRITICAL activities (found by an
	// unbuffered base solve): their effort is divided by s, so the project finishes sooner
	// AND off-critical float shrinks (riskier). The book's SECOND compression lever, after
	// parallelism (a higher StaffingCap). s==1 (every non-compressed option) is a no-op. ---
	speedup := option.CriticalSpeedup
	if speedup < 1 {
		speedup = 1
	}
	onCP := criticalSet(activities, deps, milestones, cap)
	solveActs := speedUpCritical(activities, onCP, speedup)

	// --- Duration: resource-constrained (resource-leveled) schedule length in sim-days
	// PLUS the decompression buffer, then calendar-stretched. The engine runs its OWN CPM
	// + resource solve over the option's dependency graph honoring the staffing cap; it no
	// longer sums authored on-critical-path efforts (Phase-2 rework F1/F3/F4). ---
	sched := resourceLevelSchedule(solveActs, deps, milestones, cap, option.BufferDays)
	stretch := calendarStretch(option.CalendarDaysPerWeek)
	durationDays := sched.projectDuration * stretch

	directCost := Money{MinorUnits: directCostMinorUnits(activities, rates, onCP, speedup), Currency: currency}

	// --- Risk decomposition from the RESOURCE-CONSTRAINED floats (Löwy ch.10 §3). ---
	criticalityRisk := criticalityRiskOf(sched.activities)
	activityRisk := activityRiskOf(sched.activities)
	composite := clamp01(0.5*criticalityRisk + 0.5*activityRisk)

	// --- Indirect cost: duration (calendar days) × indirect daily rate (Phase-2 rework
	// F6). This is what makes a longer option (subcritical) COSTLIER even when its direct
	// cost is similar, and gives the time-cost curve its minimum. Zero-value rate ⇒ no
	// indirect term (back-compat). A mismatched non-empty currency is a ContractMisuse. ---
	indirectCost := Money{Currency: directCost.Currency}
	if r := option.IndirectDailyRate; r.MinorUnits != 0 || r.Currency != "" {
		if r.Currency != "" && directCost.Currency != "" && r.Currency != directCost.Currency {
			return ConstructionEstimate{}, fweng.New(fweng.ContractMisuse,
				"EstimateForOption: IndirectDailyRate currency "+quote(r.Currency)+
					" != direct cost currency "+quote(directCost.Currency))
		}
		indirectCost = Money{MinorUnits: int64(durationDays * float64(r.MinorUnits)), Currency: directCost.Currency}
	}
	buildCost := Money{MinorUnits: directCost.MinorUnits + indirectCost.MinorUnits, Currency: directCost.Currency}

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
		DirectCost:   directCost,
		IndirectCost: indirectCost,
		Risk: RiskScore{
			Composite:       composite,
			CriticalityRisk: criticalityRisk,
			ActivityRisk:    activityRisk,
		},
	}, nil
}

// criticalSet runs an unbuffered resource-leveled base solve and returns the set of
// resource-critical activity ids — the path top resources are applied to under compression.
func criticalSet(activities []OptionActivity, deps []NetworkDependency, milestones []NetworkMilestone, cap int64) map[string]bool {
	base := resourceLevelSchedule(activities, deps, milestones, cap, 0)
	onCP := make(map[string]bool, len(base.activities))
	for _, a := range base.activities {
		if a.onCP {
			onCP[a.id] = true
		}
	}
	return onCP
}

// speedUpCritical returns the activity set with critical-path effort divided by speedup
// (top-resource compression, Löwy ch.9 §2/§3; F5e). speedup==1 is a no-op returning the
// input unchanged. Off-critical effort is untouched, so their float shrinks (riskier).
func speedUpCritical(activities []OptionActivity, onCP map[string]bool, speedup float64) []OptionActivity {
	if speedup <= 1 {
		return activities
	}
	out := make([]OptionActivity, len(activities))
	for i, a := range activities {
		out[i] = a
		if onCP[a.ActivityId] {
			out[i].EffortDays = a.EffortDays / speedup
		}
	}
	return out
}

// directCostMinorUnits sums effort × rate, with a CONVEX premium (rate × speedup²) on the
// sped-up critical activities. The QUADRATIC premium reproduces the book's convex time-cost
// curve (ch.9 §3): buying schedule with top resources gets disproportionately expensive, so
// the compressed option sits ABOVE normal on cost even though its shorter duration saves
// indirect — the time-cost minimum lands near normal, not at the shortest schedule.
func directCostMinorUnits(activities []OptionActivity, rates map[string]Money, onCP map[string]bool, speedup float64) int64 {
	var total int64
	for _, a := range activities {
		perDay := float64(rates[a.WorkerClass].MinorUnits)
		if speedup > 1 && onCP[a.ActivityId] {
			total += int64(a.EffortDays * perDay * speedup * speedup)
		} else {
			total += int64(a.EffortDays * perDay)
		}
	}
	return total
}

// --- Float-based risk (Löwy ch.10 §3.4 "Criticality Risk" + §3.6 "Activity Risk") -----

// Float-criticality band weights. criticality risk weights each activity by how much
// float it has: a zero-float (critical) activity is the riskiest (weight 4); a generous-
// float activity is the safest (weight 1). The bands themselves reuse the network.go
// defaultBandPolicy thresholds (critical=0, red≤5d, yellow≤25d, green>25d) so the risk
// decomposition and the SPA's band colouring never drift apart.
const (
	weightCritical = 4.0 // 0 float           (defaultBandPolicy: critical)
	weightHigh     = 3.0 // 0 < float ≤ 5d     (red / near-critical)
	weightMedium   = 2.0 // 5 < float ≤ 25d    (yellow)
	weightLow      = 1.0 // float > 25d        (green)
)

// criticalityRiskOf implements the book's WEIGHTED criticality risk (ch.10 §3.4):
//
//	criticality = (Wc·Nc + Wh·Nh + Wm·Nm + Wl·Nl) / (Wc·N)
//
// with Wc=4, Wh=3, Wm=2, Wl=1. The denominator Wc·N is the all-critical maximum, so the
// score is 1.0 when every activity is critical and floors at Wl/Wc = 0.25 when every
// activity has generous float. Replaces the old "fraction of activities on the CP" (F2).
func criticalityRiskOf(acts []leveledActivity) float64 {
	n := len(acts)
	if n == 0 {
		return 0
	}
	weighted := 0.0
	for _, a := range acts {
		switch defaultBandPolicy.classify(a.onCP, a.totalFloat) {
		case bandCritical:
			weighted += weightCritical
		case bandRed:
			weighted += weightHigh
		case bandYellow:
			weighted += weightMedium
		default: // bandGreen
			weighted += weightLow
		}
	}
	return clamp01(weighted / (weightCritical * float64(n)))
}

// activityRiskOf implements the book's activity risk (ch.10 §3.6):
//
//	activity = 1 − Σ Fi / (N · Fmax)
//
// High when most activities have low float; low when many have generous float. Edge
// cases the book notes (F2): an empty set is 0 risk; a UNIFORM-float set (every activity
// on the resource-critical path with Fmax≈0) is maximum risk 1.0 rather than a 0/0 NaN.
func activityRiskOf(acts []leveledActivity) float64 {
	n := len(acts)
	if n == 0 {
		return 0
	}
	sumFloat := 0.0
	maxFloat := 0.0
	for _, a := range acts {
		sumFloat += a.totalFloat
		if a.totalFloat > maxFloat {
			maxFloat = a.totalFloat
		}
	}
	if maxFloat <= floatEpsilon {
		// All activities are critical (zero float) — maximal, brittle. Avoid 0/0.
		return 1.0
	}
	return clamp01(1.0 - sumFloat/(float64(n)*maxFloat))
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
func (EstimationEngineImpl) ComputeEarnedValue(_ fweng.Context, activities ActivityList, network Network, integrated []string, totalWeeks int64, calendarDaysPerWeek int64) (EVCurve, error) {
	calDaysPerWeek := int(calendarDaysPerWeek)
	if calDaysPerWeek <= 0 {
		calDaysPerWeek = defaultCalendarDaysPerWeek
	}

	integratedSet := make(map[string]bool, len(integrated))
	for _, id := range integrated {
		integratedSet[id] = true
	}

	effort, finish, total := scheduleActivityFinishes(activities, network)

	tw := int(totalWeeks)
	if tw <= 0 {
		tw = deriveTotalWeeks(activities, finish, calDaysPerWeek)
	}

	weeks, planned, earned := accumulateEVCurves(activities, effort, finish, integratedSet, total, calDaysPerWeek, tw)

	spi := 0.0
	if planned[tw] > 0 {
		spi = earned[tw] / planned[tw]
	}

	return EVCurve{Weeks: weeks, Earned: earned, Planned: planned, SPI: spi}, nil
}

// scheduleActivityFinishes runs the memoized longest-path forward pass over the network
// dependencies, returning each activity's per-name effort, its earliest-finish (in days),
// and the total effort. Pure: identical inputs → identical maps.
func scheduleActivityFinishes(activities ActivityList, network Network) (effort, finish map[string]float64, total float64) {
	effort = map[string]float64{}
	depsOf := map[string][]string{}
	for _, a := range activities.Activities {
		effort[a.Name] = a.EffortDays
		total += a.EffortDays
	}
	for _, d := range network.Dependencies {
		depsOf[d.Activity] = d.DependsOn
	}

	// memoized earliest-finish (in days) via longest path.
	finish = map[string]float64{}
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
	return effort, finish, total
}

// deriveTotalWeeks bounds the curve length from the latest scheduled finish when the
// caller passes a non-positive totalWeeks.
func deriveTotalWeeks(activities ActivityList, finish map[string]float64, calDaysPerWeek int) int {
	tw := 1
	for _, a := range activities.Activities {
		if w := int(finish[a.Name])/calDaysPerWeek + 1; w > tw {
			tw = w
		}
	}
	return tw
}

// accumulateEVCurves builds the cumulative planned (all activities) and earned (integrated
// only) curves as a percentage of total effort, keyed by week index.
func accumulateEVCurves(activities ActivityList, effort, finish map[string]float64, integratedSet map[string]bool, total float64, calDaysPerWeek, tw int) (weeks []int64, planned, earned []float64) {
	weeks = make([]int64, tw+1)
	planned = make([]float64, tw+1)
	earned = make([]float64, tw+1)
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
	return weeks, planned, earned
}

// resourceschedule.go is the resource-constrained (resource-leveled) scheduling facet
// of the estimationEngine — the by-the-book replacement for the old "sum the authored
// critical-path efforts" duration heuristic (Phase-2 estimation rework F1/F3/F4).
//
// BOOK BASIS (Löwy, Righting Software):
//   - ch.7 §8 / ch.11 §3.3 "Going Subcritical": with FEWER resources than the network's
//     inherent parallelism, activities that used to run in parallel QUEUE through the
//     scarce resource. Queued float is consumed, near-critical chains become critical,
//     the project duration EXTENDS, and risk rises. That is exactly what a lower
//     StaffingCap produces here.
//   - ch.10 §5 "Risk Decompression": appending a buffer to the schedule tail widens the
//     float on every path uniformly WITHOUT reducing staff — modeled as BufferDays,
//     which pushes the terminal late-finish out.
//
// The solve is the classic serial schedule-generation scheme (SSGS): walk the activities
// in earliest-start priority order (respecting precedence) and greedily assign each to the
// earliest-free worker among StaffingCap workers. Where a worker (not a real predecessor)
// gates an activity's start, a synthetic RESOURCE EDGE is recorded so the backward pass
// can compute the correct resource-constrained float. Pure and deterministic (ties break
// by id / worker index) — no clock, no RNG, replay-safe.

// leveledActivity is one real activity's resource-constrained result: its effort, its
// resource-constrained total float, and whether it lands on the resource-critical path.
type leveledActivity struct {
	id         string
	effort     float64
	totalFloat float64
	onCP       bool
}

// leveledSchedule is the resource-constrained solve for one option: the per-activity
// results (milestones excluded — they are events, not work) and the constrained project
// duration in sim-days INCLUDING the decompression buffer.
type leveledSchedule struct {
	activities      []leveledActivity
	projectDuration float64
}

// ssgsResult is the forward SSGS solve: constrained early start/finish per node, the
// synthetic resource edges, and the constrained (pre-buffer) project duration.
type ssgsResult struct {
	cs, cf          map[string]float64
	resSucc         map[string][]string
	projectDuration float64
}

// resourceLevelSchedule runs the SSGS over (activities × dependencies × milestones)
// honoring staffingCap, then derives each activity's total float against the resource-
// augmented precedence graph with the terminal late-finish pushed out by bufferDays.
func resourceLevelSchedule(activities []OptionActivity, deps []NetworkDependency, milestones []NetworkMilestone, staffingCap int64, bufferDays float64) leveledSchedule {
	cap := staffingCap
	if cap < 1 {
		cap = 1
	}
	if bufferDays < 0 {
		bufferDays = 0
	}

	effortByID := make(map[string]float64, len(activities))
	for _, a := range activities {
		effortByID[a.ActivityId] = a.EffortDays
	}
	isMilestone := make(map[string]struct{}, len(milestones))
	for _, m := range milestones {
		isMilestone[m.Id] = struct{}{}
	}
	// dur returns a node's duration: an activity's effort, 0 for a milestone or an
	// undeclared predecessor id.
	dur := func(id string) float64 { return effortByID[id] }

	idSet := buildNodeUniverse(deps, milestones)
	for _, a := range activities {
		idSet[a.ActivityId] = struct{}{} // an isolated activity is still scheduled work
	}
	if len(idSet) == 0 {
		return leveledSchedule{}
	}

	predecessors, successors := buildAdjacency(idSet, deps, milestones)
	order := topoOrder(idSet, predecessors)
	// Unconstrained earliest-start per node — the SSGS priority: among activities whose
	// predecessors are all scheduled, the one that COULD start earliest goes first, so
	// genuinely parallel chains are not accidentally serialized behind a later activity.
	earlyStart, _, _ := forwardPass(order, predecessors, dur)

	fwd := ssgsForward(idSet, predecessors, successors, earlyStart, isMilestone, dur, int(cap))
	lateStart := backwardFloat(idSet, deps, milestones, fwd.resSucc, dur, fwd.projectDuration+bufferDays)

	out := make([]leveledActivity, 0, len(activities))
	for _, a := range activities {
		total := lateStart[a.ActivityId] - fwd.cs[a.ActivityId]
		if total < 0 {
			total = 0
		}
		out = append(out, leveledActivity{
			id:         a.ActivityId,
			effort:     a.EffortDays,
			totalFloat: total,
			onCP:       total <= floatEpsilon,
		})
	}
	return leveledSchedule{activities: out, projectDuration: fwd.projectDuration + bufferDays}
}

// ssgsForward runs the priority-list forward solve: repeatedly schedule the ready node
// with the smallest (earlyStart, id), assigning it to the earliest-free worker.
func ssgsForward(idSet map[string]struct{}, predecessors, successors map[string][]string, earlyStart map[string]float64, isMilestone map[string]struct{}, dur func(string) float64, cap int) ssgsResult {
	freeAt := make([]float64, cap)      // per-worker next-free time
	lastOnWorker := make([]string, cap) // activity that last used each worker (resource edges)
	cs := make(map[string]float64, len(idSet))
	cf := make(map[string]float64, len(idSet))
	resSucc := map[string][]string{}

	indeg := make(map[string]int, len(idSet))
	for id := range idSet {
		indeg[id] = len(predecessors[id])
	}
	scheduled := make(map[string]bool, len(idSet))

	for remaining := len(idSet); remaining > 0; remaining-- {
		id := pickReady(idSet, scheduled, indeg, earlyStart)
		scheduled[id] = true
		for _, s := range successors[id] {
			indeg[s]--
		}

		predFinish := maxPredFinish(predecessors[id], cf)
		if _, ok := isMilestone[id]; ok {
			cs[id], cf[id] = predFinish, predFinish // zero-duration event, no worker
			continue
		}
		w := earliestFreeWorker(freeAt)
		start := predFinish
		if freeAt[w] > start {
			// The worker (not a real predecessor) gates the start: record the resource
			// edge from the activity that last held this worker.
			start = freeAt[w]
			if lastOnWorker[w] != "" {
				resSucc[lastOnWorker[w]] = append(resSucc[lastOnWorker[w]], id)
			}
		}
		finish := start + dur(id)
		cs[id], cf[id] = start, finish
		freeAt[w], lastOnWorker[w] = finish, id
	}

	projectDuration := 0.0
	for _, f := range cf {
		if f > projectDuration {
			projectDuration = f
		}
	}
	return ssgsResult{cs: cs, cf: cf, resSucc: resSucc, projectDuration: projectDuration}
}

// pickReady returns the unscheduled node with all predecessors scheduled (indeg 0) that
// has the smallest (earlyStart, id). If none is ready (a dependency cycle remnant) it
// returns the smallest-id unscheduled node so nothing is dropped (topoOrder resilience).
func pickReady(idSet map[string]struct{}, scheduled map[string]bool, indeg map[string]int, earlyStart map[string]float64) string {
	id := ""
	for cand := range idSet {
		if scheduled[cand] || indeg[cand] > 0 {
			continue
		}
		if id == "" || earlyStart[cand] < earlyStart[id] ||
			(earlyStart[cand] == earlyStart[id] && cand < id) {
			id = cand
		}
	}
	if id != "" {
		return id
	}
	for cand := range idSet {
		if !scheduled[cand] && (id == "" || cand < id) {
			id = cand
		}
	}
	return id
}

// maxPredFinish returns the latest constrained finish over a node's predecessors (0 for a
// source node).
func maxPredFinish(preds []string, cf map[string]float64) float64 {
	m := 0.0
	for _, p := range preds {
		if cf[p] > m {
			m = cf[p]
		}
	}
	return m
}

// earliestFreeWorker returns the index of the worker that frees soonest (lowest index on
// a tie — deterministic).
func earliestFreeWorker(freeAt []float64) int {
	w := 0
	for i := 1; i < len(freeAt); i++ {
		if freeAt[i] < freeAt[w] {
			w = i
		}
	}
	return w
}

// backwardFloat runs the backward pass over the resource-AUGMENTED graph (real
// dependencies ∪ synthetic resource edges), returning each node's late start. The
// terminal late-finish is terminalLF (= constrained duration + decompression buffer), so
// a buffer widens float uniformly (ch.10 §5).
func backwardFloat(idSet map[string]struct{}, deps []NetworkDependency, milestones []NetworkMilestone, resSucc map[string][]string, dur func(string) float64, terminalLF float64) map[string]float64 {
	combinedSucc := map[string][]string{}
	combinedPred := map[string][]string{}
	for id := range idSet {
		combinedSucc[id] = nil
		combinedPred[id] = nil
	}
	addEdge := func(pred, succ string) {
		combinedSucc[pred] = append(combinedSucc[pred], succ)
		combinedPred[succ] = append(combinedPred[succ], pred)
	}
	for _, d := range deps {
		for _, p := range d.DependsOn {
			addEdge(p, d.Activity)
		}
	}
	for _, m := range milestones {
		for _, p := range m.DependsOn {
			addEdge(p, m.Id)
		}
	}
	for pred, succs := range resSucc {
		for _, s := range succs {
			addEdge(pred, s)
		}
	}

	order := topoOrder(idSet, combinedPred)
	lateStart := make(map[string]float64, len(idSet))
	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		lf := terminalLF
		if succ := combinedSucc[id]; len(succ) > 0 {
			first := true
			for _, s := range succ {
				if first || lateStart[s] < lf {
					lf = lateStart[s]
					first = false
				}
			}
		}
		lateStart[id] = lf - dur(id)
	}
	return lateStart
}
