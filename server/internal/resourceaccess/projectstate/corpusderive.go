package projectstate

import "strings"

// corpusderive.go holds the PURE corpus→typed-state derivation rules (Task 2). No
// filesystem access — Task 3 (cmd/seed-construction) does the IO and feeds these the
// observed CorpusPresence. Reproducible, deterministic, unit-testable.

// CorpusPresence is what the corpus scanner observed for one activity id.
type CorpusPresence struct {
	HasLog           bool   // a log/<id>.md exists
	HasPassingReview bool   // a matching *-review.md / -R log exists (passing)
	HasContract      bool   // a contracts/<component>.md exists
	ContractFile     string // corpus-relative path to the contract, when HasContract
}

// DeriveKind maps an activity to its kind from the activity-id family.
// Only U-SPA* activities are "frontend" — SPA UI-design activities are the sole
// frontend kind. N-* activities are testing. Everything else (including all
// *Client / *Manager / *Engine / *Access components and infra/integration) is
// service, because a Client component exposes a service contract just like any
// other service-layer component.
func DeriveKind(activityID, componentName string) ActivityKind {
	_ = componentName // caller passes it; classification is id-based only
	id := strings.ToUpper(activityID)
	switch {
	case strings.HasPrefix(id, "U-SPA"):
		return ActivityKindFrontend
	case strings.HasPrefix(id, "N-"):
		return ActivityKindTesting
	default:
		return ActivityKindService
	}
}

// DeriveBuildStatus maps corpus presence to the finer build-status lens. integrated is
// true only when a log AND a passing review both exist.
func DeriveBuildStatus(p CorpusPresence) (ActivityBuildStatus, bool) {
	switch {
	case p.HasLog && p.HasPassingReview:
		return BuildIntegrated, true
	case p.HasLog:
		return BuildInReview, false
	default:
		return BuildInConstruction, false
	}
}

// DeriveProduced builds the produced-artifact list: a frozen contract (when a contract
// file exists) and the built code (when a construction log exists).
func DeriveProduced(p CorpusPresence, componentName string) []ProducedArtifact {
	var out []ProducedArtifact
	if p.HasContract {
		out = append(out, ProducedArtifact{
			Kind:     "service-contract",
			Title:    componentName + " — service contract",
			Source:   p.ContractFile,
			Produced: true,
			Note:     "Frozen App-B service contract.",
		})
	}
	if p.HasLog {
		out = append(out, ProducedArtifact{
			Kind:     "code",
			Title:    componentName + " — built component",
			Source:   "implementation/log",
			Produced: true,
			Note:     "Construction output recorded in the implementation log.",
		})
	}
	return out
}
