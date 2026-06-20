package projectstate

import (
	"errors"
)

// Layer is the closed, ordered layer set per ch. 3 of The Method.
// Manager and Engine share the "Business Logic" rank: a Manager→Engine edge is
// DOWNWARD, not sideways. (projectStateAccess.md §3.3)
type Layer int

const (
	LayerClient         Layer = iota // rank 0
	LayerManager                     // rank 1  (Business Logic)
	LayerEngine                      // rank 1  (Business Logic) — same rank as Manager
	LayerResourceAccess              // rank 2
	LayerResource                    // rank 3
	LayerUtility                     // utilities bar — spans all ranks, callable by anyone
)

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
	default:
		return -1 // Utility: rank-less, excluded from up/down legality
	}
}

// ComponentKind is the closed component taxonomy per ch. 3 of The Method.
// Naming conventions and distinguishing attributes are documented as invariants;
// the legality predicates enforcing them live in artifactValidationEngine.
// (projectStateAccess.md §3.3)
type ComponentKind int

const (
	CompClient         ComponentKind = iota // <Noun>Client; a transport entry point
	CompManager                             // <Noun>Manager; encapsulates a workflow volatility; "almost expendable"
	CompEngine                              // <Gerund>Engine; encapsulates an activity volatility; NO I/O
	CompResourceAccess                      // <Noun>Access; encapsulates a Resource; ops are atomic business verbs
	CompResource                            // a physical store / queue / external system
	CompUtility                             // passes the cappuccino-machine test
)

// Component is a node in the System static architecture model.
// (projectStateAccess.md §3.3)
type Component struct {
	ID           ComponentID   `json:"id"`   // server-assigned Slug(Name); not LLM-authored
	Name         string        `json:"name"` // e.g. "ProjectStateAccess"; naming rule per Kind (see below)
	Kind         ComponentKind `json:"kind"`
	Layer        Layer         `json:"layer"`        // must be the canonical Layer for Kind (checked by NewSystem)
	Encapsulates string        `json:"encapsulates"` // the volatility this component owns (Manager/Engine/RA); "" for Resource/Utility
	// AtomicBusinessVerbs is an ATTRIBUTE OF A RESOURCEACCESS, not a component kind.
	// Non-empty only when Kind == CompResourceAccess; lists the verb names.
	AtomicBusinessVerbs []string `json:"atomicBusinessVerbs"`
}

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
	default:
		return LayerUtility
	}
}

// CallMode is the closed edge-mode set. (projectStateAccess.md §3.3)
type CallMode int

const (
	CallSync        CallMode = iota // synchronous, in-process method call
	CallQueued                      // queued (the closed-layer M→M sideways exception)
	CallEventPubSub                 // event / pub-sub (only Clients & Managers may publish/subscribe)
)

// Relationship is a directed edge between two Components in the System model.
// (projectStateAccess.md §3.3)
type Relationship struct {
	From  ComponentID `json:"from"`
	To    ComponentID `json:"to"`
	Mode  CallMode    `json:"mode"`
	Label string      `json:"label"` // destination-layer vocabulary (STRUCTURIZR-CONVENTIONS "Edge-label conventions")
}

// DynamicView is one call chain per use case (ch. 4): the participating components
// and the sync/queued edges among them. Maps 1:1 to a Structurizr dynamic view on
// render via artifactRenderingAccess. (projectStateAccess.md §3.3)
type DynamicView struct {
	UseCaseID    UseCaseID      `json:"useCaseId"` // links to a UseCase (Grammar B)
	Key          string         `json:"key"`       // stable view key, e.g. "uc1-coauthor-method-artifact"
	Title        string         `json:"title"`
	Participants []ComponentID  `json:"participants"`
	Edges        []Relationship `json:"edges"` // Mode ∈ {CallSync, CallQueued}; ordered
}

// System is the canonical typed static-architecture model (Grammar A, ch. 3/4).
// The .dsl/Structurizr text is a rendering produced by artifactRenderingAccess from
// this model — never stored separately, never the source of truth.
// (projectStateAccess.md §3.3)
type System struct {
	Components    []Component    `json:"components"`
	Relationships []Relationship `json:"relationships"`
	DynamicViews  []DynamicView  `json:"dynamicViews"`
}

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
