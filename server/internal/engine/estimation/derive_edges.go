// derive_edges.go turns the derived activity set plus the System's relationships into
// the project network: dependency edges (transitively reduced, exactly as Löwy Fig 11-4
// → Fig 11-5) and the derived milestones.
//
// "Even with a simple system having only two use cases, the dependency chart is
// cluttered and hard to analyze. A simple technique you can leverage to reduce the
// complexity is to eliminate dependencies that duplicate inherited dependencies."
// (ch. 11 §1.2) — that technique is transitive reduction, and it is code, not judgment.

package estimation

import "sort"

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

// useCaseAndSPANames extracts the sorted I-* and U-SPA-<manager>-* activity names
// present in the derived set, for the fixed pattern edges below.
func useCaseAndSPANames(acts []DerivedActivity) (useCases, spaScreens []string) {
	for _, a := range acts {
		if len(a.Name) > 2 && a.Name[:2] == "I-" {
			useCases = append(useCases, a.Name)
		}
		if len(a.Name) > 6 && a.Name[:6] == "U-SPA-" && a.Name != "U-SPA-S" {
			spaScreens = append(spaScreens, a.Name)
		}
	}
	sort.Strings(useCases)
	sort.Strings(spaScreens)
	return useCases, spaScreens
}

// addFixedPatternEdges applies the mechanical sequencing the architecture graph cannot
// state: the UI design gates SPA construction, the scaffold gates the per-manager
// screens, the test plan gates the harnesses, and every integration gates the terminal
// system-testing activity. Edges into an activity absent from this derivation (e.g. no
// UI surface) are simply skipped.
func addFixedPatternEdges(reduced map[string][]string, acts []DerivedActivity) {
	present := map[string]bool{}
	for _, a := range acts {
		present[a.Name] = true
	}
	useCases, spaScreens := useCaseAndSPANames(acts)

	addEdge := func(activity, pred string) {
		if !present[activity] || !present[pred] {
			return
		}
		reduced[activity] = append(reduced[activity], pred)
	}
	for _, s := range spaScreens {
		addEdge(s, "G-SPA")
		addEdge(s, "U-SPA-S")
	}
	addEdge("N-STH", "N-STP")
	addEdge("N-RTH", "N-STP")
	for _, uc := range useCases {
		addEdge("N-IT", uc)
	}
}

// deriveDependencies builds the network edges: the transitively reduced architecture
// edges, plus the fixed pattern edges that no relationship expresses.
func deriveDependencies(system SystemView, acts []DerivedActivity) []NetworkDependency {
	raw := architectureEdges(system, activityForComponent(acts))
	reduced := transitiveReduction(raw)
	addFixedPatternEdges(reduced, acts)

	out := make([]NetworkDependency, 0, len(reduced))
	for activity, preds := range reduced {
		sort.Strings(preds)
		out = append(out, NetworkDependency{Activity: activity, DependsOn: preds})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Activity < out[j].Activity })
	return out
}

// deriveMilestones emits M0-M4. M0 is the SDP-review forced dependency (ch. 11 "About
// Milestones": "none of the construction activities should start before the SDP
// review"). M1-M3 are layer completions, M4 is use-cases-demonstrable.
//
// M5 (v1 Production Live) is deliberately NOT derived: it depends entirely on additive
// noncoding activities, so it arrives as an additive delta.
func deriveMilestones(system SystemView, acts []DerivedActivity) []NetworkMilestone {
	kindByComponent := map[string]string{}
	for _, c := range system.Components {
		kindByComponent[c.ID] = c.Kind
	}

	var provisioning, engines, managers, integrations []string
	for _, a := range acts {
		switch {
		case len(a.Name) > 2 && a.Name[:2] == "R-":
			provisioning = append(provisioning, a.Name)
		case len(a.Name) > 2 && a.Name[:2] == "I-":
			integrations = append(integrations, a.Name)
		case len(a.Name) > 2 && a.Name[:2] == "C-":
			switch kindByComponent[a.ComponentID] {
			case "engine":
				engines = append(engines, a.Name)
			case "manager":
				managers = append(managers, a.Name)
			}
		}
	}
	for _, s := range [][]string{provisioning, engines, managers, integrations} {
		sort.Strings(s)
	}

	return []NetworkMilestone{
		{Id: "M0"}, // SDP Review Approved — the forced dependency, no fan-in
		{Id: "M1", DependsOn: provisioning},
		{Id: "M2", DependsOn: engines},
		{Id: "M3", DependsOn: managers},
		{Id: "M4", DependsOn: integrations},
	}
}
