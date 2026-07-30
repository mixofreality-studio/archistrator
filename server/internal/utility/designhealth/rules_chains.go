package designhealth

import (
	"fmt"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// rules_chains.go — the §6 per-use-case dynamic-view rules. A dynamic view is one
// use-case call chain; The Method constrains how Managers appear in a chain:
//
//	§6a  exactly ONE Manager is the client's entry orchestrator per chain
//	§6b  at most ONE queued Manager→Manager hop per chain
//
// Both are SeverityWarning: a dynamic view is a modeling aid and an odd chain is
// worth a reviewer's eye, not an authoring-gate block. Endpoints that name no
// System component (external actors like a git repo or a human role) are ignored —
// only component participants carry a layer.
func chainFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	idx := in.componentIndex()

	var out []methodcheck.Finding
	for i, dv := range in.Slots.DynamicViews {
		section := "dynamicView " + dvLabel(dv)

		// §6a — count DISTINCT Managers a client directly calls in this chain. The
		// entry orchestrator is the Manager reached from a client edge; downstream
		// queued Managers (permitted by §6b) are not entry points.
		entryManagers := map[string]bool{}
		queuedManagerHops := 0
		for _, e := range dv.Edges {
			from, okF := idx[e.From]
			to, okT := idx[e.To]
			if okF && okT && from.Kind == "client" && to.Kind == "manager" {
				entryManagers[to.ID] = true
			}
			if okF && from.Kind == "manager" && e.Mode == "queued" {
				queuedManagerHops++
			}
		}
		if len(entryManagers) > 1 {
			out = append(out, finding(RuleChainEntryManager, methodcheck.SeverityWarning, i, section,
				fmt.Sprintf("chain %q has %d distinct client-entry Managers (%s) — a use-case chain should be driven by exactly one entry Manager; downstream Managers must be reached by a queued hop, not a second client entry",
					dvLabel(dv), len(entryManagers), sortedKeys(entryManagers))))
		}
		// §6b — at most one queued Manager→Manager hop per chain.
		if queuedManagerHops > 1 {
			out = append(out, finding(RuleChainQueuedManager, methodcheck.SeverityWarning, i, section,
				fmt.Sprintf("chain %q has %d queued Manager hops — The Method permits at most one queued Manager→Manager handoff per use-case chain",
					dvLabel(dv), queuedManagerHops)))
		}
	}
	return out
}

// dvLabel picks the most human-readable identifier for a dynamic view.
func dvLabel(dv dynamicView) string {
	if dv.UseCaseID != "" {
		return dv.UseCaseID
	}
	return dv.Key
}
