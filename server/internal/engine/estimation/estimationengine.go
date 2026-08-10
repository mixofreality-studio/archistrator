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
	milestones := solveMilestonesOnCP(network.Milestones, milestoneIDs, earlyFinish, activityOnCP, projectDuration)

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
	if err := validateOptionActivities(activities, rates); err != nil {
		return ConstructionEstimate{}, err
	}

	// --- Cost currency: the single shared currency of the participating rates; a mixed
	// or empty currency is a ContractMisuse (the Manager mis-assembled the mix). ---
	currency, err := sharedRateCurrency(activities, rates)
	if err != nil {
		return ConstructionEstimate{}, err
	}

	deps, milestones := option.Network.Dependencies, option.Network.Milestones
	staffCap := option.WorkerMix.StaffingCap

	// --- Top-resource compression (Löwy ch.9 §2/§3; Phase-2 rework F5e). CriticalSpeedup
	// s>1 assigns faster "top resources" to the RESOURCE-CRITICAL activities (found by an
	// unbuffered base solve): their effort is divided by s, so the project finishes sooner
	// AND off-critical float shrinks (riskier). The book's SECOND compression lever, after
	// parallelism (a higher StaffingCap). s==1 (every non-compressed option) is a no-op. ---
	speedup := option.CriticalSpeedup
	if speedup < 1 {
		speedup = 1
	}
	onCP := criticalSet(activities, deps, milestones, staffCap)
	solveActs := speedUpCritical(activities, onCP, speedup)

	// --- Duration: resource-constrained (resource-leveled) schedule length in sim-days
	// PLUS the decompression buffer, then calendar-stretched. The engine runs its OWN CPM
	// + resource solve over the option's dependency graph honoring the staffing cap; it no
	// longer sums authored on-critical-path efforts (Phase-2 rework F1/F3/F4). ---
	sched := resourceLevelSchedule(solveActs, deps, milestones, staffCap, option.BufferDays)
	stretch := calendarStretch(option.CalendarDaysPerWeek)
	durationDays := sched.projectDuration * stretch

	directCost := Money{MinorUnits: directCostMinorUnits(activities, rates, onCP, speedup), Currency: currency}

	// --- Risk decomposition from the RESOURCE-CONSTRAINED floats (Löwy ch.10 §3). ---
	criticalityRisk := criticalityRiskOf(sched.activities)
	activityRisk := activityRiskOf(sched.activities)
	composite := clamp01(0.5*criticalityRisk + 0.5*activityRisk)

	indirectCost, err := indirectCostOf(option.IndirectDailyRate, durationDays, directCost)
	if err != nil {
		return ConstructionEstimate{}, err
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

// validateOptionActivities enforces the per-activity ContractMisuse pre-conditions
// (a projectDesignManager bug, not a domain result): non-negative effort and a
// rate present for every referenced worker class.
func validateOptionActivities(activities []OptionActivity, rates map[string]Money) error {
	for i, a := range activities {
		if a.EffortDays < 0 {
			return fweng.New(fweng.ContractMisuse,
				"EstimateForOption: activity "+activityRef(a, i)+" has negative EffortDays")
		}
		if _, ok := rates[a.WorkerClass]; !ok {
			return fweng.New(fweng.ContractMisuse,
				"EstimateForOption: activity "+activityRef(a, i)+
					" references WorkerClass "+quote(a.WorkerClass)+" with no rate in WorkerMix.ClassRates")
		}
	}
	return nil
}

// sharedRateCurrency returns the single currency shared by every participating
// rate; a mixed or empty currency is a ContractMisuse (the Manager mis-assembled
// the mix).
func sharedRateCurrency(activities []OptionActivity, rates map[string]Money) (string, error) {
	currency := ""
	for i, a := range activities {
		rate := rates[a.WorkerClass]
		if rate.Currency == "" {
			return "", fweng.New(fweng.ContractMisuse,
				"EstimateForOption: rate for WorkerClass "+quote(a.WorkerClass)+" has empty currency")
		}
		if currency == "" {
			currency = rate.Currency
		} else if rate.Currency != currency {
			return "", fweng.New(fweng.ContractMisuse,
				"EstimateForOption: mixed rate currencies ("+quote(currency)+" vs "+
					quote(rate.Currency)+") at activity "+activityRef(a, i))
		}
	}
	return currency, nil
}

// indirectCostOf computes the indirect cost: duration (calendar days) × indirect
// daily rate (Phase-2 rework F6). This is what makes a longer option (subcritical)
// COSTLIER even when its direct cost is similar, and gives the time-cost curve its
// minimum. Zero-value rate ⇒ no indirect term (back-compat). A mismatched
// non-empty currency is a ContractMisuse.
func indirectCostOf(r Money, durationDays float64, directCost Money) (Money, error) {
	indirectCost := Money{Currency: directCost.Currency}
	if r.MinorUnits != 0 || r.Currency != "" {
		if r.Currency != "" && directCost.Currency != "" && r.Currency != directCost.Currency {
			return Money{}, fweng.New(fweng.ContractMisuse,
				"EstimateForOption: IndirectDailyRate currency "+quote(r.Currency)+
					" != direct cost currency "+quote(directCost.Currency))
		}
		indirectCost = Money{MinorUnits: int64(durationDays * float64(r.MinorUnits)), Currency: directCost.Currency}
	}
	return indirectCost, nil
}

// criticalSet runs an unbuffered resource-leveled base solve and returns the set of
// resource-critical activity ids — the path top resources are applied to under compression.
func criticalSet(activities []OptionActivity, deps []NetworkDependency, milestones []NetworkMilestone, staffingCap int64) map[string]bool {
	base := resourceLevelSchedule(activities, deps, milestones, staffingCap, 0)
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
	workers := max(staffingCap, 1)
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

	fwd := ssgsForward(idSet, predecessors, successors, earlyStart, isMilestone, dur, int(workers))
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
func ssgsForward(idSet map[string]struct{}, predecessors, successors map[string][]string, earlyStart map[string]float64, isMilestone map[string]struct{}, dur func(string) float64, workers int) ssgsResult {
	freeAt := make([]float64, workers)      // per-worker next-free time
	lastOnWorker := make([]string, workers) // activity that last used each worker (resource edges)
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

// --- Derivation: DerivePlan ---
//
// DerivePlan is the deterministic Phase-2 derivation of the activity list and network
// from the committed System (Löwy ch. 11; Fig 11-4 → Fig 11-5 is literally a transitive
// reduction over the component dependency chart). The activity inventory is NOT authored:
// it falls out of the architecture, and the only authored input is the ActivityListDeltas
// document (justified effort/risk overrides plus genuinely componentless additive
// activities).
//
// Purity, as for every op on this Engine: no I/O, no clock, no randomness, no globals.
// Identical inputs → identical DerivedPlan, always. All iteration over maps is sorted.

// DerivePlan derives the full activity list and network from the committed System and
// applies the authored deltas.
//
// An EMPTY system is a normal DOMAIN result (an empty plan) — a project may be read
// before its architecture is committed. The *fweng.Error channel is reserved for
// contract misuse: a delta that the vocabulary forbids (an override naming no derived
// activity, an additive carrying a componentId, a missing justification).
func (EstimationEngineImpl) DerivePlan(_ fweng.Context, system SystemView, deltas ActivityListDeltas) (DerivedPlan, error) {
	if len(system.Components) == 0 {
		return DerivedPlan{Activities: nil, Dependencies: nil, Milestones: nil}, nil
	}
	acts := deriveActivities(system)
	deps := deriveDependencies(system, acts)
	ms := deriveMilestones(system, acts)
	return applyDeltas(acts, deps, ms, deltas)
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

// alwaysEmitNoncoding is the standard testing / QA inventory emitted for EVERY project
// (the-method-activity-list Step 2b). Unit testing alone is "borderline useless" (Löwy
// ch. 2); the load-bearing verification is full regression of the integrated system, so
// the harnesses are planned work, not an afterthought. Fixed efforts — these do not
// scale with the architecture.
var alwaysEmitNoncoding = []struct {
	Name   string
	Title  string
	Effort float64
}{
	{"N-QA", "Quality-assurance process and gates", 10},
	{"N-PERF", "Performance testing", 15},
	{"N-RTH", "Regression test harness", 15},
	{"N-SMOKE", "Daily build and smoke", 5},
	{"N-STH", "System test harness", 20},
	{"N-STP", "System test plan (all core use cases)", 15},
	{"N-IT", "System testing (terminal gate)", 30},
}

// isCodeLayer reports whether a component kind gets a coding activity at all. Resources
// are provisioned, never coded by us.
func isCodeLayer(kind string) bool {
	switch kind {
	case "client", "manager", "engine", "resourceAccess", "utility":
		return true
	}
	return false
}

// codingActivityFor emits the C-* coding activity for a handwritten code-layer
// component, or false when the component doesn't qualify: generated transport (the
// generator does that work), a non-code-layer kind such as resource, or a "provided"
// component — platform/third-party supplied, no coding activity either.
//
// Löwy's Table 11-1 DOES give Logging and Security their own coding activities (6 and
// 7) — because in that worked example they are BUILT. The rule here is about who builds
// it, not about the layer: this project's security/diagnostics/logging/message-bus
// utilities are CONFIGURED against off-the-shelf platform pieces (Keycloak, OTel, a
// structured sink, the Workflow Execution Substrate), so planning construction work for
// them would be the same defect as planning work the generator does. An unauthored
// constructionProfile still defaults to "handwritten" upstream (toEstimationSystemView) —
// only an explicit "generated" or "provided" value skips the activity.
func codingActivityFor(c SystemComponent) (DerivedActivity, bool) {
	if !isCodeLayer(c.Kind) || c.ConstructionProfile == "generated" || c.ConstructionProfile == "provided" {
		return DerivedActivity{}, false
	}
	effort := defaultEffortFor(c.Kind)
	return DerivedActivity{
		Name:        "C-" + c.ID,
		Title:       "Build " + c.Name,
		EffortDays:  effort,
		RiskBucket:  defaultRiskFor(effort),
		WorkerClass: workerClassFor("C"),
		Coding:      true,
		ComponentID: c.ID,
		Derived:     true,
	}, true
}

// provisioningActivityFor emits the R-* provisioning activity for a vendor resource.
// Owned stores get none — their schema/deploy work arrives as additive noncoding.
func provisioningActivityFor(c SystemComponent) (DerivedActivity, bool) {
	if c.Kind != "resource" || c.Provisioning != "vendor" {
		return DerivedActivity{}, false
	}
	effort := defaultEffortFor(c.Kind)
	return DerivedActivity{
		Name:        "R-" + c.ID,
		Title:       "Provision " + c.Name,
		EffortDays:  effort,
		RiskBucket:  defaultRiskFor(effort),
		WorkerClass: workerClassFor("R"),
		Coding:      false,
		ComponentID: c.ID,
		Derived:     true,
	}, true
}

// managerSPAActivityFor emits the U-SPA-<manager> construction activity. A screen that
// crosses managers is the exception (it arrives as an additive delta), not a reason to
// weaken this one-per-manager rule.
func managerSPAActivityFor(c SystemComponent) DerivedActivity {
	return DerivedActivity{
		Name:        "U-SPA-" + c.ID,
		Title:       "SPA screens for " + c.Name,
		EffortDays:  20,
		RiskBucket:  defaultRiskFor(20),
		WorkerClass: workerClassFor("U"),
		Coding:      true,
		ComponentID: c.ID,
		Derived:     true,
	}
}

// spaScaffoldActivities emits the always-paired SPA scaffold and UI-design-concept
// activities, present exactly when the system declares a UI surface.
func spaScaffoldActivities() []DerivedActivity {
	return []DerivedActivity{
		{
			Name: "U-SPA-S", Title: "SPA scaffold, auth wiring and design system",
			EffortDays: 10, RiskBucket: defaultRiskFor(10),
			WorkerClass: workerClassFor("U"), Coding: true, Derived: true,
		},
		{
			Name: "G-SPA", Title: "UI design concepts for the SPA",
			EffortDays: 15, RiskBucket: defaultRiskFor(15),
			WorkerClass: workerClassFor("G"), Coding: false, Derived: true,
		},
	}
}

// noncodingInventoryActivities emits the always-emit testing / QA inventory.
func noncodingInventoryActivities() []DerivedActivity {
	out := make([]DerivedActivity, 0, len(alwaysEmitNoncoding))
	for _, n := range alwaysEmitNoncoding {
		out = append(out, DerivedActivity{
			Name: n.Name, Title: n.Title,
			EffortDays: n.Effort, RiskBucket: defaultRiskFor(n.Effort),
			WorkerClass: noncodingInventoryClass(n.Name), Coding: false, Derived: true,
		})
	}
	return out
}

// systemHasUISurface reports whether any component in the system declares a UI surface.
func systemHasUISurface(system SystemView) bool {
	for _, c := range system.Components {
		if c.UiSurface {
			return true
		}
	}
	return false
}

// deriveActivities emits the full derived activity set for the System, sorted by name.
//
// Emission rules (each one a mechanical consequence of the architecture):
//
//	C-<id>          one per code-layer component with constructionProfile == "handwritten"
//	(none)          "generated" components — the generator does that work
//	(none)          "provided" components — platform/third-party supplied, nothing to build
//	R-<id>          one per Resource with provisioning == "vendor"
//	(none)          owned stores — schema/deploy work arrives as additive noncoding
//	U-SPA-<manager> one per Manager, when any component declares a UI surface
//	U-SPA-S         the SPA scaffold, when any component declares a UI surface
//	G-SPA           the UI-design concept, when any component declares a UI surface
//	(none)          no I-* integration activity — App A makes integration a PHASE of
//	                every activity's own lifecycle, so a separate I-* would charge the
//	                same work twice; System Testing (N-IT) is the one activity Table
//	                11-1 gives integration, and it depends on the U-SPA-* construction
//	                activities (see addFixedPatternEdges), not on a per-use-case I-*.
//	N-*             the always-emit testing inventory
func deriveActivities(system SystemView) []DerivedActivity {
	out := make([]DerivedActivity, 0, len(system.Components)*2)

	uiSurface := systemHasUISurface(system)

	for _, c := range system.Components {
		if a, ok := codingActivityFor(c); ok {
			out = append(out, a)
		}
		if a, ok := provisioningActivityFor(c); ok {
			out = append(out, a)
		}
		if uiSurface && c.Kind == "manager" {
			out = append(out, managerSPAActivityFor(c))
		}
	}

	if uiSurface {
		out = append(out, spaScaffoldActivities()...)
	}

	out = append(out, noncodingInventoryActivities()...)

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// --- Network derivation: dependency edges and milestones ---
//
// This turns the derived activity set plus the System's relationships into the project
// network: dependency edges (transitively reduced, exactly as Löwy Fig 11-4 → Fig 11-5)
// and the derived milestones.
//
// "Even with a simple system having only two use cases, the dependency chart is
// cluttered and hard to analyze. A simple technique you can leverage to reduce the
// complexity is to eliminate dependencies that duplicate inherited dependencies."
// (ch. 11 §1.2) — that technique is transitive reduction, and it is code, not judgment.

// transitiveReduction removes every edge that is implied by a longer path. An edge
// u→v is redundant when v is reachable from u through some OTHER direct successor of u.
//
// Cycle-safe: reachability is a visited-set BFS, so a malformed (cyclic) committed
// System yields a defensible result instead of hanging. Output predecessor lists are
// sorted, so the derivation stays deterministic.
func transitiveReduction(edges map[string][]string) map[string][]string {
	// reachable reports whether dst is reachable from src WITHOUT using the direct
	// src→skip edge.
	reachable := func(src, dst, skip string) bool {
		visited := map[string]bool{src: true}
		queue := make([]string, 0, len(edges[src]))
		for _, n := range edges[src] {
			if n != skip {
				queue = append(queue, n)
			}
		}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur == dst {
				return true
			}
			if visited[cur] {
				continue
			}
			visited[cur] = true
			queue = append(queue, edges[cur]...)
		}
		return false
	}

	out := make(map[string][]string, len(edges))
	for node, preds := range edges {
		kept := make([]string, 0, len(preds))
		for _, p := range preds {
			if !reachable(node, p, p) {
				kept = append(kept, p)
			}
		}
		if len(kept) == 0 {
			continue
		}
		sort.Strings(kept)
		out[node] = kept
	}
	return out
}

// activityForComponent indexes the CODING/PROVISIONING activity per component, so an
// architecture edge can be rewritten as an activity edge. Components with no derived
// activity (owned stores, generated transport) are simply absent, and edges into them
// are dropped rather than emitted as dangling references.
func activityForComponent(acts []DerivedActivity) map[string]string {
	out := map[string]string{}
	for _, a := range acts {
		if a.ComponentID == "" {
			continue
		}
		switch a.Name[:2] {
		case "C-", "R-":
			out[a.ComponentID] = a.Name
		}
	}
	return out
}

// architectureEdges rewrites the System's relationships into raw activity edges
// (unreduced), dropping self-edges and edges touching a component with no derived
// activity.
func architectureEdges(system SystemView, byComponent map[string]string) map[string][]string {
	raw := map[string][]string{}
	for _, r := range system.Relationships {
		from, okFrom := byComponent[r.From]
		to, okTo := byComponent[r.To]
		if !okFrom || !okTo || from == to {
			continue
		}
		raw[from] = append(raw[from], to)
	}
	return raw
}

// spaScreenNames extracts the sorted U-SPA-<manager>-* activity names present in the
// derived set (the scaffold U-SPA-S is excluded — it is a predecessor of these, not a
// peer), for the fixed pattern edges below.
func spaScreenNames(acts []DerivedActivity) (spaScreens []string) {
	for _, a := range acts {
		if len(a.Name) > 6 && a.Name[:6] == "U-SPA-" && a.Name != "U-SPA-S" {
			spaScreens = append(spaScreens, a.Name)
		}
	}
	sort.Strings(spaScreens)
	return spaScreens
}

// addFixedPatternEdges applies the mechanical sequencing the architecture graph cannot
// state:
//
//   - the UI design gates SPA construction, the scaffold gates the per-manager screens;
//   - EACH per-manager SPA construction activity ALSO depends on the manager's own C-*
//     coding activity — structurally what Table 11-1's activity 19 (Client App1) does by
//     depending on the managers it calls. architectureEdges cannot express this itself:
//     every client component's constructionProfile is "generated" (the platform emits
//     the transport tier), so activityForComponent indexes no C-* activity for it, and
//     the client→manager architecture relationships have no C-* source to route
//     through — they are silently dropped rather than misrouted (C1, 2026-08-10).
//   - the test plan gates the harnesses;
//   - every per-manager SPA construction activity gates the terminal system-testing
//     activity — structurally what Table 11-1's activity 21 (System Testing) does by
//     depending on the client activities (5, 19, 20), now that there is no separate I-*
//     to depend on instead (ruling 2: integration is a PHASE of each activity's own
//     lifecycle, not a standalone activity). When the system declares NO UI surface at
//     all (no U-SPA-* screens exist), N-IT's fan-in falls back to every manager's C-*
//     coding activity directly instead — the same Table-11-1 activity-21 relationship,
//     minus the client layer that a headless project does not have (C1 minor, same root
//     cause: without this fallback a headless project's N-IT gets NO predecessors at
//     all);
//   - the always-emit noncoding inventory (N-QA, N-SMOKE, N-PERF) each get a defensible
//     predecessor so every activity the derivation always emits is reachable in the CPM
//     graph (C2, 2026-08-10): buildNodeUniverse walks EDGES, not the activity list, so
//     an activity with no incident edge in either direction silently gets no node at all
//     and drops out of the CPM solve with no ES/EF/float and no critical-path
//     membership, however large its effort. N-PERF follows N-STH — performance testing
//     runs the harness N-STH builds. N-SMOKE follows N-STP for the same reason N-STH and
//     N-RTH do — a daily smoke check needs the test plan's definition of what "smoke"
//     covers before it can run one. N-QA is a process/gate activity with no natural
//     upstream work product of its own (Löwy: QA reviews and tunes the development
//     PROCESS, "what will it take to assure quality" — it is not gated BY the work), so
//     it gets no predecessor of its own, but it must still gate N-IT (system testing)
//     rather than sit as an island off the schedule the CPM solve can't reach.
//
// Edges into an activity absent from this derivation (e.g. no UI surface) are simply
// skipped.
func addFixedPatternEdges(reduced map[string][]string, acts []DerivedActivity, system SystemView) {
	present := map[string]bool{}
	for _, a := range acts {
		present[a.Name] = true
	}
	spaScreens := spaScreenNames(acts)

	addEdge := func(activity, pred string) {
		if !present[activity] || !present[pred] {
			return
		}
		reduced[activity] = append(reduced[activity], pred)
	}
	for _, s := range spaScreens {
		mgrActivity := "C-" + s[len("U-SPA-"):] // s is "U-SPA-<manager-component-id>"
		addEdge(s, "G-SPA")
		addEdge(s, "U-SPA-S")
		addEdge(s, mgrActivity)
		addEdge("N-IT", s)
	}
	if len(spaScreens) == 0 {
		// Headless fallback (C1 minor): no U-SPA-* screens exist, so route N-IT's fan-in
		// straight to every manager's coding activity instead of through a client layer
		// that does not exist.
		for _, c := range system.Components {
			if c.Kind == "manager" {
				addEdge("N-IT", "C-"+c.ID)
			}
		}
	}
	addEdge("N-STH", "N-STP")
	addEdge("N-RTH", "N-STP")

	// C2: give the always-emit inventory its own pattern edges so every one of these
	// activities is reachable in the CPM graph (see the function doc above).
	addEdge("N-PERF", "N-STH")
	addEdge("N-SMOKE", "N-STP")
	addEdge("N-IT", "N-QA")
}

// deriveDependencies builds the network edges: the transitively reduced architecture
// edges, plus the fixed pattern edges that no relationship expresses.
func deriveDependencies(system SystemView, acts []DerivedActivity) []NetworkDependency {
	raw := architectureEdges(system, activityForComponent(acts))
	reduced := transitiveReduction(raw)
	addFixedPatternEdges(reduced, acts, system)

	out := make([]NetworkDependency, 0, len(reduced))
	for activity, preds := range reduced {
		sort.Strings(preds)
		out = append(out, NetworkDependency{Activity: activity, DependsOn: preds})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Activity < out[j].Activity })
	return out
}

// deriveMilestones emits M0-M3. M0 is the SDP-review forced dependency (ch. 11 "About
// Milestones": "none of the construction activities should start before the SDP
// review"). M1-M3 are layer completions.
//
// M4 (Use Cases Demonstrable) is deliberately NOT derived: it depended entirely on the
// now-removed I-* integration activities (ruling 2) and had no other fan-in of its own,
// so it is removed rather than emitted with an empty DependsOn.
//
// M5 (v1 Production Live) is deliberately NOT derived: it depends entirely on additive
// noncoding activities, so it arrives as an additive delta.
func deriveMilestones(system SystemView, acts []DerivedActivity) []NetworkMilestone {
	kindByComponent := map[string]string{}
	for _, c := range system.Components {
		kindByComponent[c.ID] = c.Kind
	}

	var provisioning, engines, managers []string
	for _, a := range acts {
		switch {
		case len(a.Name) > 2 && a.Name[:2] == "R-":
			provisioning = append(provisioning, a.Name)
		case len(a.Name) > 2 && a.Name[:2] == "C-":
			switch kindByComponent[a.ComponentID] {
			case "engine":
				engines = append(engines, a.Name)
			case "manager":
				managers = append(managers, a.Name)
			}
		}
	}
	for _, s := range [][]string{provisioning, engines, managers} {
		sort.Strings(s)
	}

	return []NetworkMilestone{
		{Id: "M0"}, // SDP Review Approved — the forced dependency, no fan-in
		{Id: "M1", DependsOn: provisioning},
		{Id: "M2", DependsOn: engines},
		{Id: "M3", DependsOn: managers},
	}
}

// --- Delta application: the authored delta vocabulary ---
//
// This enforces and applies the authored delta vocabulary.
//
// The vocabulary is CLOSED and deliberately narrow — numbers plus additive activities:
//
//   - an OVERRIDE may replace effortDays / riskBucket on a DERIVED activity, and must
//     carry a written justification;
//   - an ADDITIVE may append an activity that maps to NO single component, declaring its
//     own incident edges.
//
// There is no exclusion and no derived-to-derived edge override, on purpose. An
// exclusion asserts that a committed component requires no work — which is either false
// or an admission that it should not be a component. A wrong exclusion is SILENT where a
// wrong derivation is LOUD, and the silent form is exactly how C-HE, C-WIA and R-WIT
// survived in the committed plan against components that no longer exist. If a derived
// edge is wrong, the System relationship is wrong: fix the architecture.

// workerRoster is the fixed Method team. An unknown class silently rides default token
// rates in the cost engines and misclassifies in every downstream view, so it is
// rejected rather than defaulted.
var workerRoster = map[string]bool{
	"system-architect": true, "product-manager": true, "project-manager": true,
	"senior-developer": true, "junior-developer": true, "ui-designer": true,
	"ux-reviewer": true, "qa-engineer": true, "test-engineer": true, "software-tester": true,
}

var fibonacciBuckets = map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}

// legalEffort enforces App C §4.4: a 5-day quantum, no god activity.
func legalEffort(d float64) bool {
	return d > 0 && d <= 35 && float64(int(d)) == d && int(d)%5 == 0
}

// applyOverrides applies the ActivityOverride deltas onto acts (indexed by index) in
// place. An override may replace effortDays/riskBucket on a DERIVED activity only, and
// must carry a written justification.
func applyOverrides(acts []DerivedActivity, index map[string]int, overrides []ActivityOverride) error {
	for _, o := range overrides {
		i, ok := index[o.Activity]
		if !ok {
			return fweng.New(fweng.ContractMisuse,
				"DerivePlan: override names activity "+o.Activity+" which the System does not derive; "+
					"if the work is real the architecture is missing a component, and if the component is gone the override is a zombie")
		}
		if o.Justification == "" {
			return fweng.New(fweng.ContractMisuse,
				"DerivePlan: override of "+o.Activity+" carries no justification; the delta document is the entire human-review surface and every line must defend itself")
		}
		if o.EffortDays != nil {
			if !legalEffort(*o.EffortDays) {
				return fweng.New(fweng.ContractMisuse,
					"DerivePlan: override of "+o.Activity+" breaks the 5-day quantum or the 35-day god-activity cap")
			}
			acts[i].EffortDays = *o.EffortDays
		}
		if o.RiskBucket != nil {
			if !fibonacciBuckets[*o.RiskBucket] {
				return fweng.New(fweng.ContractMisuse,
					"DerivePlan: override of "+o.Activity+" sets a non-Fibonacci risk bucket")
			}
			acts[i].RiskBucket = *o.RiskBucket
		}
	}
	return nil
}

// validateAdditive checks one AdditiveActivity against the closed vocabulary. It does
// NOT check incident edges — those are validated only after every additive has been
// added to the index (see appendAdditives / validateAdditiveEdges), so two additives
// may legally depend on each other.
func validateAdditive(a AdditiveActivity, index map[string]int) error {
	if _, clash := index[a.Name]; clash {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" shadows a derived activity; that is an exclusion in disguise")
	}
	if a.ComponentID != nil {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" carries a componentId; additive is for genuinely componentless work, "+
				"and a component-bound additive is a covert exclusion/replacement channel")
	}
	if a.Justification == "" {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" carries no justification")
	}
	if !legalEffort(a.EffortDays) {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" breaks the 5-day quantum or the 35-day god-activity cap")
	}
	if !fibonacciBuckets[a.RiskBucket] {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" sets a non-Fibonacci risk bucket")
	}
	if !workerRoster[a.WorkerClass] {
		return fweng.New(fweng.ContractMisuse,
			"DerivePlan: additive activity "+a.Name+" names worker class "+a.WorkerClass+
				", which is not on the fixed Method roster; an unknown class silently rides default token rates")
	}
	return nil
}

// appendAdditives validates and appends deltas.Additive onto acts, registering each new
// name in index as it goes, and returns the extra dependency edges the additives declare
// for themselves (not yet validated against the post-additive index).
func appendAdditives(acts []DerivedActivity, index map[string]int, additive []AdditiveActivity) ([]DerivedActivity, []NetworkDependency, error) {
	extraDeps := make([]NetworkDependency, 0, len(additive))
	for _, a := range additive {
		if err := validateAdditive(a, index); err != nil {
			return acts, nil, err
		}
		acts = append(acts, DerivedActivity{
			Name: a.Name, Title: a.Title, EffortDays: a.EffortDays, RiskBucket: a.RiskBucket,
			WorkerClass: a.WorkerClass, Coding: a.Coding, Derived: false,
		})
		index[a.Name] = len(acts) - 1
		if len(a.DependsOn) > 0 {
			preds := make([]string, len(a.DependsOn))
			copy(preds, a.DependsOn)
			sort.Strings(preds)
			extraDeps = append(extraDeps, NetworkDependency{Activity: a.Name, DependsOn: preds})
		}
	}
	return acts, extraDeps, nil
}

// validateAdditiveEdges checks additive incident edges only AFTER every additive has
// been added to index (C3: an additive declares its OWN incident edges only — the
// target must exist in the plan, or it would inject a dangling node into the CPM
// solve).
func validateAdditiveEdges(extraDeps []NetworkDependency, index map[string]int) error {
	for _, d := range extraDeps {
		for _, p := range d.DependsOn {
			if _, ok := index[p]; !ok {
				return fweng.New(fweng.ContractMisuse,
					"DerivePlan: additive activity "+d.Activity+" depends on "+p+
						", which is not an activity in the plan; an additive declares its OWN incident edges only")
			}
		}
	}
	return nil
}

// applyAdditiveMilestones validates and appends the AdditiveMilestone deltas (C4: M5 "v1
// Production Live" depends entirely on additive noncoding and therefore cannot derive)
// onto ms, and returns the combined, sorted milestone list. A derived milestone may
// still ACQUIRE predecessors from additive activities, which is why dependsOn is
// validated against the full post-additive activity set in index.
func applyAdditiveMilestones(ms []NetworkMilestone, index map[string]int, additive []AdditiveMilestone) ([]NetworkMilestone, error) {
	milestones := make([]NetworkMilestone, len(ms))
	copy(milestones, ms)
	derivedMilestone := make(map[string]bool, len(ms))
	for _, m := range ms {
		derivedMilestone[m.Id] = true
	}
	for _, am := range additive {
		if derivedMilestone[am.Id] {
			return nil, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive milestone "+am.Id+" shadows a derived milestone")
		}
		if am.Justification == "" {
			return nil, fweng.New(fweng.ContractMisuse,
				"DerivePlan: additive milestone "+am.Id+" carries no justification")
		}
		for _, p := range am.DependsOn {
			if _, ok := index[p]; !ok {
				return nil, fweng.New(fweng.ContractMisuse,
					"DerivePlan: additive milestone "+am.Id+" depends on "+p+", which is not an activity in the plan")
			}
		}
		preds := make([]string, len(am.DependsOn))
		copy(preds, am.DependsOn)
		sort.Strings(preds)
		milestones = append(milestones, NetworkMilestone{Id: am.Id, DependsOn: preds})
		derivedMilestone[am.Id] = true
	}
	sort.Slice(milestones, func(i, j int) bool { return milestones[i].Id < milestones[j].Id })
	return milestones, nil
}

// applyDeltas overlays the authored deltas on the derived baseline.
func applyDeltas(base []DerivedActivity, deps []NetworkDependency, ms []NetworkMilestone, deltas ActivityListDeltas) (DerivedPlan, error) {
	index := make(map[string]int, len(base))
	for i, a := range base {
		index[a.Name] = i
	}
	acts := make([]DerivedActivity, len(base))
	copy(acts, base)

	if err := applyOverrides(acts, index, deltas.Overrides); err != nil {
		return DerivedPlan{}, err
	}

	acts, extraDeps, err := appendAdditives(acts, index, deltas.Additive)
	if err != nil {
		return DerivedPlan{}, err
	}

	// Additive edges are validated only AFTER every additive exists, so two additives may
	// legally depend on each other.
	if err := validateAdditiveEdges(extraDeps, index); err != nil {
		return DerivedPlan{}, err
	}

	milestones, err := applyAdditiveMilestones(ms, index, deltas.AdditiveMilestones)
	if err != nil {
		return DerivedPlan{}, err
	}

	allDeps := make([]NetworkDependency, 0, len(deps)+len(extraDeps))
	allDeps = append(allDeps, deps...)
	allDeps = append(allDeps, extraDeps...)
	sort.Slice(allDeps, func(i, j int) bool { return allDeps[i].Activity < allDeps[j].Activity })
	sort.Slice(acts, func(i, j int) bool { return acts[i].Name < acts[j].Name })

	return DerivedPlan{Activities: acts, Dependencies: allDeps, Milestones: milestones}, nil
}
