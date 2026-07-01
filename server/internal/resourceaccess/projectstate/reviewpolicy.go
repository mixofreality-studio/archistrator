package projectstate

// ReviewPolicy is the per-project, committed configuration of WHICH phases require a
// human approval gate during construction. It composes with the reviewEngine (which
// computes WHO reviews): the engine gives the reviewer set; this policy says whether a
// human must sign off before the phase advances. The zero value gates nothing — the
// construction loop then behaves exactly as before this feature ("pure vibes").
type ReviewPolicy struct {
	// GatedPhasesByType maps an ActivityType wire name ("service"/"frontend"/"testing"/...)
	// to the canonical phases that require human approval for that type.
	GatedPhasesByType map[string][]ActivityMethodPhase `json:"gatedPhasesByType,omitempty"`
}

// RequiresHuman reports whether a phase of the given activity type requires human approval.
func (p ReviewPolicy) RequiresHuman(activityType string, phase ActivityMethodPhase) bool {
	for _, gated := range p.GatedPhasesByType[activityType] {
		if gated == phase {
			return true
		}
	}
	return false
}

// gateIDToPhase maps the webApp PolicyPanel's ad-hoc gate ids to canonical phases, so the
// mock vocabulary never reaches head-state. Canonical ids pass through in ReviewPolicyFromGateIDs.
var gateIDToPhase = map[string]ActivityMethodPhase{
	"svc-contract": MethodPhaseDetailedDesign,
	"svc-review":   MethodPhaseIntegration,
	"fe-approve":   MethodPhaseDetailedDesign,
	"test-plan":    MethodPhaseTestPlan,
}

// ReviewPolicyFromGateIDs builds a policy from per-type gate-id lists (canonical or ad-hoc).
func ReviewPolicyFromGateIDs(byType map[string][]string) ReviewPolicy {
	out := ReviewPolicy{GatedPhasesByType: map[string][]ActivityMethodPhase{}}
	for typ, ids := range byType {
		for _, id := range ids {
			ph, ok := gateIDToPhase[id]
			if !ok {
				ph = ActivityMethodPhase(id)
			}
			switch ph {
			case MethodPhaseRequirements, MethodPhaseDetailedDesign, MethodPhaseTestPlan,
				MethodPhaseConstruction, MethodPhaseIntegration:
				out.GatedPhasesByType[typ] = append(out.GatedPhasesByType[typ], ph)
			}
		}
	}
	return out
}
