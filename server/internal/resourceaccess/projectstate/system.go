package projectstate

import (
	"errors"
)

// Layer is the closed, ordered layer set per ch. 3 of The Method.
// Manager and Engine share the "Business Logic" rank: a Manager→Engine edge is
// DOWNWARD, not sideways. (projectStateAccess.md §3.3)

// rank 0
// rank 1  (Business Logic)
// rank 1  (Business Logic) — same rank as Manager
// rank 2
// rank 3
// utilities bar — spans all ranks, callable by anyone

// Rank collapses Manager+Engine so legality predicates treat M→E as downward.
// Returns -1 for Utility (rank-less, excluded from up/down legality checks).
// (projectStateAccess.md §3.3)
func (l Layer) Rank() int {
	switch l {
	case LayerClient:
		return 0
	case LayerManager, LayerEngine:
		return 1
	case LayerResourceAccess:
		return 2
	case LayerResource:
		return 3
	case LayerUtility:
		// Rank-less by design (spans all ranks) — same as the default below.
		return -1
	default:
		return -1 // Utility: rank-less, excluded from up/down legality
	}
}

// ComponentKind is the closed component taxonomy per ch. 3 of The Method.
// Naming conventions and distinguishing attributes are documented as invariants;
// the legality predicates enforcing them live in artifactValidationEngine.
// (projectStateAccess.md §3.3)

// <Noun>Client; a transport entry point
// <Noun>Manager; encapsulates a workflow volatility; "almost expendable"
// <Gerund>Engine; encapsulates an activity volatility; NO I/O
// <Noun>Access; encapsulates a Resource; ops are atomic business verbs
// a physical store / queue / external system
// passes the cappuccino-machine test

// Component is a node in the System static architecture model.
// (projectStateAccess.md §3.3)

// server-assigned Slug(Name); not LLM-authored
// e.g. "ProjectStateAccess"; naming rule per Kind (see below)

// must be the canonical Layer for Kind (checked by NewSystem)
// the volatility this component owns (Manager/Engine/RA); "" for Resource/Utility
// AtomicBusinessVerbs is an ATTRIBUTE OF A RESOURCEACCESS, not a component kind.
// Non-empty only when Kind == CompResourceAccess; lists the verb names.

// CanonicalLayer returns the canonical Layer for a ComponentKind. It is the
// single source of truth for the Kind→Layer derivation: NewSystem uses it to
// enforce the shape invariant, and the systemDesign finalize pass uses it to
// DERIVE Component.Layer server-side (the LLM never emits a layer — it is 100%
// derivable from Kind). (projectStateAccess.md §3.3)
func CanonicalLayer(k ComponentKind) Layer { return canonicalLayer(k) }

// canonicalLayer returns the canonical Layer for a ComponentKind.
// Used by NewSystem to enforce the shape invariant that a component's Layer
// matches its Kind. (projectStateAccess.md §3.3 "NewSystem validates … canonical layer")
func canonicalLayer(k ComponentKind) Layer {
	switch k {
	case CompClient:
		return LayerClient
	case CompManager:
		return LayerManager
	case CompEngine:
		return LayerEngine
	case CompResourceAccess:
		return LayerResourceAccess
	case CompResource:
		return LayerResource
	case CompUtility:
		// The zero value — same as the default below.
		return LayerUtility
	default:
		return LayerUtility
	}
}

// CallMode is the closed edge-mode set. (projectStateAccess.md §3.3)

// synchronous, in-process method call
// queued (the closed-layer M→M sideways exception)
// event / pub-sub (only Clients & Managers may publish/subscribe)

// Relationship is a directed edge between two Components in the System model.
// (projectStateAccess.md §3.3)

// destination-layer vocabulary (STRUCTURIZR-CONVENTIONS "Edge-label conventions")

// DynamicView is one call chain per use case (ch. 4): the participating components
// and the sync/queued edges among them. Maps 1:1 to a Structurizr dynamic view on
// render via artifactRenderingAccess. (projectStateAccess.md §3.3)

// links to a UseCase (Grammar B)
// stable view key, e.g. "uc1-coauthor-method-artifact"

// Mode ∈ {CallSync, CallQueued}; ordered

// System is the canonical typed static-architecture model (Grammar A, ch. 3/4).
// The .dsl/Structurizr text is a rendering produced by artifactRenderingAccess from
// this model — never stored separately, never the source of truth.
// (projectStateAccess.md §3.3)

// Kind implements ArtifactModel. (projectStateAccess.md §3.3)
func (s *System) Kind() ArtifactKind { return KindSystem }

// isArtifactModel seals the ArtifactModel sum to this package's models.
// (projectStateAccess.md §3.1)
func (s *System) isArtifactModel() {}

// NewSystem constructs a System after validating SHAPE only (not semantic legality).
// Shape checks (projectStateAccess.md §3.3):
//   - each Component.Name must be non-empty
//   - each Component.Layer must be the canonical Layer for its Kind
//     (Client→LayerClient, Manager→LayerManager, Engine→LayerEngine,
//     ResourceAccess→LayerResourceAccess, Resource→LayerResource, Utility→LayerUtility)
//
// Semantic legality (no calling up, no sideways except queued M→M, no layer-skipping,
// pub/sub origin/destination rules, the 12 Design Don'ts, cardinality) are predicates
// in artifactValidationEngine — NOT enforced here.
func NewSystem(components []Component, relationships []Relationship, dynamicViews []DynamicView) (*System, error) {
	for _, c := range components {
		if c.Name == "" {
			return nil, errors.New("projectstate.NewSystem: component Name must not be empty")
		}
		canonical := canonicalLayer(c.Kind)
		if c.Layer != canonical {
			return nil, errors.New("projectstate.NewSystem: component " + c.Name + " has Layer inconsistent with its Kind")
		}
	}
	return &System{
		Components:    components,
		Relationships: relationships,
		DynamicViews:  dynamicViews,
	}, nil
}
