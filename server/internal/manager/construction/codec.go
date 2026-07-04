package construction

import (
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// This file owns the Manager's Temporal-boundary serialization helpers
// (constructionManager.md §6.4).

// projectEnvelope is the Temporal-serializable projection of the head-state
// Project the pump/sweep needs across the ReadProjectActivity boundary. The full
// Project carries a sealed projectstate.ArtifactModel interface in each slot, which
// the default JSON converter cannot decode; so this envelope carries the identity,
// the optimistic-concurrency Version, the lifecycle Phase — AND the three CONCRETE,
// JSON-serializable projections the pump's eligibility selection
// (nextEligibleActivity) reads: the committed Network (dependencies + critical path),
// the committed ActivityList (the per-activity effort/coding facts), and the
// per-activity ActivityConstruction status map. These three are plain structs (no
// sealed ArtifactModel interface), so they cross the Temporal converter cleanly.
// readProject re-inflates a projectstate.Project from them with the two slots marked
// ReviewCommitted so nextEligibleActivity's committed-slot guards pass.
type projectEnvelope struct {
	ID      projectstate.ProjectID `json:"id"`
	Version projectstate.Version   `json:"version"`
	Phase   projectstate.Phase     `json:"phase"`

	// Network / ActivityList are the COMMITTED Phase-2 slot models the pump needs to
	// walk the dependency graph + hydrate the chosen activity. Non-nil only when the
	// corresponding slot is ReviewCommitted (the *Committed flags carry that fact so
	// readProject can restore the slot Status without re-deriving it).
	Network               *projectstate.Network      `json:"network,omitempty"`
	NetworkCommitted      bool                       `json:"networkCommitted,omitempty"`
	ActivityList          *projectstate.ActivityList `json:"activityList,omitempty"`
	ActivityListCommitted bool                       `json:"activityListCommitted,omitempty"`

	// ActivityConstruction is the per-activity construction head-state the eligibility
	// selection reads (NotStarted/Running/Done). nil until the first RecordActivityStarted.
	ActivityConstruction map[string]projectstate.ActivityConstructionStatus `json:"activityConstruction,omitempty"`

	// ServiceContracts is the per-component contract corpus, keyed by component name.
	// The pump's hydrate step resolves an activity → its component contract from this
	// map (resolveComponentID), so it must cross the Activity boundary. Plain structs,
	// JSON-serializable.
	ServiceContracts map[string]projectstate.ServiceContract `json:"serviceContracts,omitempty"`

	// ReviewPolicy is the committed per-project human-approval-gate configuration the
	// per-activity spine's phase gate reads at workflow start (Task 6). Plain struct
	// (map of string→[]string phase ids), JSON-serializable — crosses the Activity
	// boundary cleanly. Zero value gates nothing (the pre-feature behavior).
	ReviewPolicy projectstate.ReviewPolicy `json:"reviewPolicy,omitempty"`
}

// encodeProject projects the head-state aggregate onto the envelope, carrying the
// committed Network + ActivityList slot models and the construction status map so the
// pump's eligibility selection has everything it needs across the Activity boundary.
func encodeProject(p projectstate.Project) projectEnvelope {
	e := projectEnvelope{
		ID:                   p.ID,
		Version:              p.Version,
		Phase:                p.Phase,
		ActivityConstruction: p.ActivityConstruction,
		ServiceContracts:     p.ServiceContracts,
		ReviewPolicy:         p.ReviewPolicy,
	}
	if p.Network.Status == projectstate.ReviewCommitted {
		if n, ok := p.Network.Model.(*projectstate.Network); ok {
			e.Network = n
			e.NetworkCommitted = true
		}
	}
	if p.ActivityList.Status == projectstate.ReviewCommitted {
		if al, ok := p.ActivityList.Model.(*projectstate.ActivityList); ok {
			e.ActivityList = al
			e.ActivityListCommitted = true
		}
	}
	return e
}

// decodeProject re-inflates a projectstate.Project from the envelope for the pump's
// pure eligibility selection. The two Phase-2 slots are restored to ReviewCommitted
// (their Model set to the carried struct) so nextEligibleActivity's committed-slot
// guards pass; every other slot stays zero (the pump does not read them).
func decodeProject(e projectEnvelope) projectstate.Project {
	p := projectstate.Project{
		ID:                   e.ID,
		Version:              e.Version,
		Phase:                e.Phase,
		ActivityConstruction: e.ActivityConstruction,
		ServiceContracts:     e.ServiceContracts,
		ReviewPolicy:         e.ReviewPolicy,
	}
	if e.NetworkCommitted && e.Network != nil {
		p.Network = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: e.Network}
	}
	if e.ActivityListCommitted && e.ActivityList != nil {
		p.ActivityList = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: e.ActivityList}
	}
	return p
}
