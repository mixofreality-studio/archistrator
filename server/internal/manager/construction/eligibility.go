package construction

// eligibility.go holds the pump's PURE eligibility selection over committed head-state
// (constructionManager.md §6.3 step 1) — the Manager's own workflow-side selection logic,
// deterministic and replay-safe (called directly in-workflow via the injected
// NextEligibleActivity helper). It was folded out of adapters.go so adapters.go carries
// only the engine boundary adapters; none of this touches Temporal or any RA seam.

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// nextEligibleActivity resolves the next eligible construction activity for a project
// from its head-state. An activity is eligible iff it is NotStarted and every dep is
// Done. Iteration is ActivityList declaration order with a name tie-break.
func nextEligibleActivity(proj projectstate.Project) (constructionActivity, bool) {
	if proj.Network.Status != projectstate.ReviewCommitted {
		return constructionActivity{}, false
	}
	network, ok := proj.Network.Model.(*projectstate.Network)
	if !ok || network == nil {
		return constructionActivity{}, false
	}
	if proj.ActivityList.Status != projectstate.ReviewCommitted {
		return constructionActivity{}, false
	}
	activityList, ok := proj.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || activityList == nil {
		return constructionActivity{}, false
	}

	itemByName := make(map[string]projectstate.ActivityItem, len(activityList.Activities))
	for _, item := range activityList.Activities {
		itemByName[item.Name] = item
	}

	depsByActivity := make(map[string][]string, len(network.Dependencies))
	for _, dep := range network.Dependencies {
		depsByActivity[dep.Activity] = dep.DependsOn
	}

	type candidate struct {
		declIdx  int
		activity string
	}
	var candidates []candidate
	for i, item := range activityList.Activities {
		name := item.Name
		if !isActivityNotStarted(name, proj.ActivityConstruction) {
			continue
		}
		if !allDepsDone(depsByActivity[name], proj.ActivityConstruction) {
			continue
		}
		candidates = append(candidates, candidate{declIdx: i, activity: name})
	}
	if len(candidates) == 0 {
		return constructionActivity{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].declIdx != candidates[j].declIdx {
			return candidates[i].declIdx < candidates[j].declIdx
		}
		return candidates[i].activity < candidates[j].activity
	})

	chosen := candidates[0].activity
	item := itemByName[chosen]
	var produced []projectstate.ProducedArtifact
	if proj.ActivityConstruction != nil {
		produced = proj.ActivityConstruction[chosen].Produced
	}
	component, ok := resolveComponentID(item.Title, produced, proj.ServiceContracts)
	if !ok {
		slog.Warn("construction pump: no service-contract key resolves for activity — skipping dispatch",
			"activityId", chosen, "title", item.Title)
		return constructionActivity{}, false
	}
	return hydrateConstructionActivity(chosen, item, component), true
}

// resolveComponentID maps an activity to its service-contract component KEY.
func resolveComponentID(title string, produced []projectstate.ProducedArtifact, contracts map[string]projectstate.ServiceContract) (string, bool) {
	for _, art := range produced {
		if art.Kind != "service-contract" {
			continue
		}
		if key, ok := matchContractKey(art.Title, contracts); ok {
			return key, true
		}
	}

	base := title
	if i := strings.IndexByte(base, '('); i >= 0 {
		base = base[:i]
	}
	n := normalizeIdent(base)
	best, bestLen := "", 0
	for comp := range contracts {
		cn := normalizeIdent(comp)
		if cn != "" && len(cn) > bestLen && strings.Contains(n, cn) {
			best, bestLen = comp, len(cn)
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// matchContractKey resolves a produced service-contract artifact title to a real key.
func matchContractKey(title string, contracts map[string]projectstate.ServiceContract) (string, bool) {
	n := normalizeIdent(title)
	if n == "" {
		return "", false
	}
	best, bestLen := "", 0
	for comp := range contracts {
		cn := normalizeIdent(comp)
		if cn == "" {
			continue
		}
		if cn == n {
			return comp, true
		}
		if len(cn) > bestLen && strings.Contains(n, cn) {
			best, bestLen = comp, len(cn)
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// normalizeIdent lowercases s and keeps only [a-z0-9].
func normalizeIdent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isActivityNotStarted reports whether the activity is in the NotStarted phase.
func isActivityNotStarted(activityID string, status map[string]projectstate.ActivityConstructionStatus) bool {
	if status == nil {
		return true
	}
	s, exists := status[activityID]
	if !exists {
		return true
	}
	return s.Phase == projectstate.ActivityConstructionNotStarted
}

// allDepsDone reports whether every dependency has Phase == Done.
func allDepsDone(deps []string, status map[string]projectstate.ActivityConstructionStatus) bool {
	for _, dep := range deps {
		if status == nil {
			return false
		}
		s, exists := status[dep]
		if !exists || s.Phase != projectstate.ActivityConstructionDone {
			return false
		}
	}
	return true
}

// hydrateConstructionActivity populates a constructionActivity from the activity id +
// its ActivityList item. Coding=true → Construction; Coding=false → Noncoding.
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, componentID string) constructionActivity {
	kind := activityKindNoncoding
	if item.Coding {
		kind = activityKindConstruction
	}
	typ := projectstate.DeriveType(activityID)
	variant := projectstate.DeriveVariant(activityID)
	return constructionActivity{
		ActivityID:   activityID,
		Kind:         kind,
		ComponentID:  componentID,
		EstimateDays: item.EffortDays,
		Phases:       projectstate.ProfileFor(typ, variant).PhaseIDs(),
	}
}
