package estimation

// network.go is the read-side CPM facet of the constructionEstimationEngine: a PURE,
// deterministic forward/backward critical-path solve over the project network plus a
// criticality-BAND classification Strategy. It is the server-side home of the math the
// webApp formerly ran client-side (api/projectAdapters.ts::toNetworkView), moved onto
// the Engine per the founder gate (2026-06-19) so the SPA renders authoritative
// server-computed figures rather than re-deriving them.
//
// CONTRACT (architecture.dsl:695): ComputeNetwork(activities, dependencies) →
// {per-node ES/EF/LS/LF, total/free float, onCriticalPath, nearCritical, criticality
// band, column, network summary}. Band classification is a Policy Strategy ON this
// Engine (DefaultBandPolicy) — NOT a new component.
//
// LAYER DISCIPLINE (unchanged from estimation.go): Engine layer — pure, deterministic,
// in-workflow. NO I/O, NO time, NO rand, NO goroutines, NO outbound calls. Imports ONLY
// the input value types (projectstate) and the framework-go Engine error model. The
// projectManager calls this directly at read time; its determinism is what makes that
// safe (identical network → identical solution, always).

import (
	"sort"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// floatEpsilon absorbs float64 rounding on the total-float (LS−ES) subtraction so the
// standard on-critical-path test (totalFloat ≈ 0) is robust. Activity efforts are whole
// 5-day quanta so exact-zero is the norm; the epsilon is a guard, not a tolerance band.
const floatEpsilon = 1e-9

// BandPolicy is the float-criticality band classification Strategy (Löwy ch.8 §2). The
// thresholds are ABSOLUTE day-counts kept on the policy so they are TUNABLE without
// touching the solve: a node ON the critical path is `critical`; otherwise `red` at
// float ≤ RedMaxDays, `yellow` at RedMaxDays < float ≤ YellowMaxDays, `green` above.
// The near-critical flag (off-CP but tight) keys off the SAME RedMaxDays boundary so
// the band colour-coding and the near-critical roll-up never drift apart.
type BandPolicy struct {
	// RedMaxDays is the inclusive upper bound of the red (near-critical) band, in days.
	RedMaxDays float64
	// YellowMaxDays is the inclusive upper bound of the yellow band, in days; above it
	// is green.
	YellowMaxDays float64
}

// DefaultBandPolicy is the launch band Strategy — the absolute thresholds the retired
// client-side toNetworkView used (red ≤ 5d, yellow 6–25d, green ≥ 26d), now the single
// server-side source of truth.
var DefaultBandPolicy = BandPolicy{RedMaxDays: 5, YellowMaxDays: 25}

// Band band names (the wire-stable strings the SPA colour-codes on).
const (
	BandCritical = "critical"
	BandRed      = "red"
	BandYellow   = "yellow"
	BandGreen    = "green"
)

// classify returns the float-criticality band for a node. onCriticalPath wins; else the
// absolute float thresholds apply.
func (p BandPolicy) classify(onCriticalPath bool, totalFloat float64) string {
	if onCriticalPath {
		return BandCritical
	}
	if totalFloat <= p.RedMaxDays {
		return BandRed
	}
	if totalFloat <= p.YellowMaxDays {
		return BandYellow
	}
	return BandGreen
}

// nearCritical reports whether an off-CP node falls inside the near-critical band
// (float ≤ RedMaxDays). On-CP nodes are never near-critical (they ARE critical).
func (p BandPolicy) nearCritical(onCriticalPath bool, totalFloat float64) bool {
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
func (engine) ComputeNetwork(activities projectstate.ActivityList, network projectstate.Network) (NetworkSolution, error) {
	effortByName := make(map[string]projectstate.ActivityItem, len(activities.Activities))
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
		milestoneIDs[m.ID] = struct{}{}
	}

	// The node universe = everything named in activity dependencies (activities + their
	// predecessors) PLUS every milestone id and its fan-in, so a node with no declared
	// row — or a milestone with no fan-out — still appears.
	idSet := map[string]struct{}{}
	for _, d := range deps {
		idSet[d.Activity] = struct{}{}
		for _, p := range d.DependsOn {
			idSet[p] = struct{}{}
		}
	}
	for _, m := range network.Milestones {
		idSet[m.ID] = struct{}{}
		for _, p := range m.DependsOn {
			idSet[p] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		// No network authored yet — a normal empty result, not an error.
		return NetworkSolution{Nodes: map[string]NetworkNode{}, Milestones: nil, Summary: NetworkSummary{}}, nil
	}

	predecessors := map[string][]string{}
	successors := map[string][]string{}
	for id := range idSet {
		predecessors[id] = nil
		successors[id] = nil
	}
	// Activity dependency edges.
	for _, d := range deps {
		for _, p := range d.DependsOn {
			predecessors[d.Activity] = append(predecessors[d.Activity], p)
			successors[p] = append(successors[p], d.Activity)
		}
	}
	// Milestone fan-IN edges (a milestone's dependsOn). Milestone fan-OUT edges are
	// already captured above as ordinary activity deps that name the milestone as a
	// predecessor (e.g. a design root dependsOn M0).
	for _, m := range network.Milestones {
		for _, p := range m.DependsOn {
			predecessors[m.ID] = append(predecessors[m.ID], p)
			successors[p] = append(successors[p], m.ID)
		}
	}

	dur := func(id string) float64 {
		if _, isMilestone := milestoneIDs[id]; isMilestone {
			return 0 // milestones are zero-duration events
		}
		return effortByName[id].EffortDays // zero value when the activity is unknown
	}

	order := topoOrder(idSet, predecessors)

	// Forward pass: earliest start / finish.
	earlyStart := map[string]float64{}
	earlyFinish := map[string]float64{}
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
	projectDuration := 0.0
	for _, ef := range earlyFinish {
		if ef > projectDuration {
			projectDuration = ef
		}
	}

	// Backward pass: latest finish / start.
	lateFinish := map[string]float64{}
	lateStart := map[string]float64{}
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

	// Column = longest-path depth from a source (topological layering for the swimlanes).
	col := map[string]int{}
	for _, id := range order {
		c := 0
		for _, p := range predecessors[id] {
			if col[p]+1 > c {
				c = col[p] + 1
			}
		}
		col[id] = c
	}

	// onCriticalPath for ACTIVITIES — standard float test: total float ≈ 0 (textbook CPM).
	// (floatEpsilon absorbs float64 rounding on the LS−ES subtraction.) Milestones do NOT
	// use this — see solveMilestonesOnCP for the determining-predecessor rule.
	onCriticalPath := func(totalFloat float64) bool { return totalFloat <= floatEpsilon }

	// Free float = min over successors of (succ.earliestStart - this.earliestFinish);
	// a sink's free float = projectDuration - this.earliestFinish. Free float never
	// exceeds total float (a guard the SPA relies on for the band colouring).
	//
	// Build per-node CPM facets for EVERY node (activities + milestones), then split:
	// activity nodes → the Nodes map; milestone nodes → the milestone solutions. Both flow
	// through the SAME forward/backward pass (so milestone eventTime + milestone→milestone
	// chaining are correct), but a milestone is EXCLUDED from the Nodes map, the summary CP
	// count, and risk (it carries no effort/bucket — EstimateForOption never sees it).
	// Activity on-CP comes from the float test here; MILESTONE on-CP is computed separately
	// by solveMilestonesOnCP (determining-predecessor rule), not from this float value.
	nodes := make(map[string]NetworkNode, len(order))
	activityOnCP := map[string]bool{}
	for _, id := range order {
		es := earlyStart[id]
		ls := lateStart[id]
		total := ls - es
		onCP := onCriticalPath(total)

		if _, isMilestone := milestoneIDs[id]; isMilestone {
			continue // milestones are not activity nodes; their facets come below
		}
		activityOnCP[id] = onCP

		ef := earlyFinish[id]
		free := projectDuration - ef
		if succ := successors[id]; len(succ) > 0 {
			free = projectDuration
			first := true
			for _, s := range succ {
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

		nodes[id] = NetworkNode{
			EarliestStart:  es,
			EarliestFinish: ef,
			LatestStart:    ls,
			LatestFinish:   lateFinish[id],
			TotalFloat:     total,
			FreeFloat:      free,
			OnCriticalPath: onCP,
			NearCritical:   DefaultBandPolicy.nearCritical(onCP, total),
			Band:           DefaultBandPolicy.classify(onCP, total),
			Column:         col[id],
		}
	}

	// Milestone solutions: eventTime == the milestone node's earliestFinish (duration 0,
	// already milestone-aware via the unified pass — chaining works). onCriticalPath uses
	// the DETERMINING-PREDECESSOR rule (architect + team-lead, 2026-06-19), NOT the float
	// test — a milestone MARKS reaching a point, so its criticality is the criticality of
	// the achievement that gates it, not the slack of a dead-end sink.
	milestones := solveMilestonesOnCP(network.Milestones, milestoneIDs, predecessors, earlyFinish, activityOnCP, projectDuration)

	// Summary roll-up over the ACTIVITY nodes (milestones excluded — they are events,
	// not work). criticalPathDays == projectDuration (the longest path IS the CP length;
	// it is NOT the sum of CP-activity durations, which double-counts parallel branches).
	cpCount := 0
	nearCount := 0
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

	summary := NetworkSummary{
		TotalDurationDays:         projectDuration,
		CriticalPathActivityCount: cpCount,
		CriticalPathDays:          projectDuration,
		MaxFloat:                  maxFloat,
		NearCriticalCount:         nearCount,
	}

	if projectDuration < 0 {
		return NetworkSolution{}, fweng.New(fweng.InternalInvariant,
			"ComputeNetwork: computed negative project duration")
	}

	return NetworkSolution{Nodes: nodes, Milestones: milestones, Summary: summary}, nil
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
	authored []projectstate.NetworkMilestone,
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

	// onCPOf returns a predecessor's on-CP: an activity's float-based on-CP, or a
	// milestone-predecessor's already-solved on-CP (false if not yet solved — the
	// dependency-order pass below ensures it is, except across an authoring cycle).
	onCPOf := func(id string) bool {
		if _, isMilestone := milestoneIDs[id]; isMilestone {
			return solved[id].OnCriticalPath
		}
		return activityOnCP[id]
	}

	solveOne := func(m projectstate.NetworkMilestone) NetworkMilestoneSolution {
		event := earlyFinish[m.ID] // milestone-aware EF (chaining already folded in)

		// ROOT convention: no predecessors ⇒ the project-start gate, on-CP.
		if len(m.DependsOn) == 0 {
			return NetworkMilestoneSolution{ID: m.ID, OnCriticalPath: true, EventTime: event}
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
			pOnCP := onCPOf(p)
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

		return NetworkMilestoneSolution{ID: m.ID, OnCriticalPath: onCP, EventTime: event}
	}

	// Resolve in dependency order (fixpoint): a milestone whose milestone-predecessors are
	// all solved is resolvable; iterate until no progress, then force the remainder (breaks
	// any authoring cycle deterministically).
	pending := make([]projectstate.NetworkMilestone, len(authored))
	copy(pending, authored)
	for len(pending) > 0 {
		progressed := false
		var still []projectstate.NetworkMilestone
		for _, m := range pending {
			ready := true
			for _, p := range m.DependsOn {
				if _, isMilestone := milestoneIDs[p]; isMilestone {
					if _, done := solved[p]; !done {
						ready = false
						break
					}
				}
			}
			if ready {
				solved[m.ID] = solveOne(m)
				progressed = true
			} else {
				still = append(still, m)
			}
		}
		if !progressed {
			for _, m := range still {
				if _, done := solved[m.ID]; !done {
					solved[m.ID] = solveOne(m)
				}
			}
			break
		}
		pending = still
	}

	out := make([]NetworkMilestoneSolution, 0, len(authored))
	for _, m := range authored {
		out = append(out, solved[m.ID])
	}
	return out
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

// --- NetworkSolution: the Engine's read-side output value object ---------------

// NetworkSolution is the sole output of ComputeNetwork: the per-node CPM result, the
// computed milestone event nodes, and the project summary. The projectManager maps this
// onto the projectstate.Network computed block at read time. Construction-side / read-
// side ONLY — no cost, no payout (those are EstimateForOption's facets).
type NetworkSolution struct {
	Nodes      map[string]NetworkNode     // keyed by activity id
	Milestones []NetworkMilestoneSolution // computed event nodes, authored order preserved
	Summary    NetworkSummary
}

// NetworkNode is the per-activity CPM result for one dependency-graph node.
type NetworkNode struct {
	EarliestStart  float64
	EarliestFinish float64
	LatestStart    float64
	LatestFinish   float64
	TotalFloat     float64
	FreeFloat      float64
	OnCriticalPath bool
	NearCritical   bool
	Band           string // critical|red|yellow|green
	Column         int
}

// NetworkMilestoneSolution is the computed facet of one milestone event node (the
// authored id + the computed on-CP/event-time). Excluded from risk.
type NetworkMilestoneSolution struct {
	ID             string
	OnCriticalPath bool
	EventTime      float64
}

// NetworkSummary is the project-level CPM roll-up.
type NetworkSummary struct {
	TotalDurationDays         float64
	CriticalPathActivityCount int
	CriticalPathDays          float64
	MaxFloat                  float64
	NearCriticalCount         int
}
