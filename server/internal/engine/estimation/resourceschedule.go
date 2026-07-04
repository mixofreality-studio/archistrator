package estimation

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
