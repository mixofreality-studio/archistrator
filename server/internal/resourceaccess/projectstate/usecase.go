package projectstate

import (
	"errors"
)

// Trigger is the closed 3-set of use-case trigger kinds per ch. 4.
// (projectStateAccess.md §3.4)
type Trigger int

const (
	TriggerClientAction Trigger = iota // a Client-initiated request
	TriggerTimer                       // a scheduled/timer tick
	TriggerBusMessage                  // an inbound bus/queue message
)

// Classification distinguishes Core from NonCore use cases per ch. 4.
// NO UML include/extend/generalize — the book does not use them.
// (projectStateAccess.md §3.4)
type Classification int

const (
	ClassCore    Classification = iota
	ClassNonCore                // use VariationOf to link to the core UC it permutes
)

// ActivityNodeKind is the closed node set the book enumerates plus UML-general
// nodes admitted for repo use and tagged as such. (projectStateAccess.md §3.4)
type ActivityNodeKind int

const (
	// ---- book-enumerated (ch. 4 / App C 1c) ----
	NodeStart    ActivityNodeKind = iota
	NodeAction                    // a step / action
	NodeDecision                  // guarded outgoing edges
	NodeMerge
	NodeFork
	NodeJoin
	NodeEnd
	NodeSwimLane // a.k.a. Partition; Name = role, optional link to actor/component
	NodeNote
	// ---- UML-general, NOT book-enumerated (admitted for repo use, TAGGED) ----
	NodeLoop          // UML-general
	NodeSwitch        // UML-general
	NodeGoto          // UML-general
	NodeInterruptEdge // UML-general
)

// BookEnumerated reports whether a node kind is in the book's closed set (vs UML-general).
// WARNING: the iota ordering is load-bearing — book-enumerated node kinds must be
// declared before NodeNote; do not insert non-book nodes before it.
// (projectStateAccess.md §3.4)
func (k ActivityNodeKind) BookEnumerated() bool { return k <= NodeNote }

// ActivityNode is a node in an ActivityDiagram.
//
// NAME-AS-IDENTITY (2026-06-04): ID is a server-assigned SLUG of the node Label
// (or a positional fallback for unlabeled structural nodes), NOT a UUID and NOT
// LLM-authored. ActivityEdge.From/To carry this same slug. LinkedActorID /
// LinkedCompID are NAME-slug references (the linked Actor's role-slug / the linked
// Component's id) resolved server-side from the names the LLM emitted.
// (projectStateAccess.md §3.4)
type ActivityNode struct {
	ID    string           `json:"id"`
	Kind  ActivityNodeKind `json:"kind"`
	Label string           `json:"label"`
	// For NodeSwimLane: the role name and an optional link to an actor or Component.
	RoleName      string       `json:"roleName"`
	LinkedActorID *string      `json:"linkedActorId"`
	LinkedCompID  *ComponentID `json:"linkedCompId"`
}

// EdgeKind is the closed set of activity-edge kinds. (projectStateAccess.md §3.4)
type EdgeKind int

const (
	EdgeControlFlow EdgeKind = iota // plain control flow
	EdgeGuardedFlow                 // outgoing edge of a Decision; Guard non-empty
)

// ActivityEdge is a directed edge in an ActivityDiagram.
// (projectStateAccess.md §3.4)
type ActivityEdge struct {
	From  string   `json:"from"` // an ActivityNode.ID (node-label slug)
	To    string   `json:"to"`   // an ActivityNode.ID (node-label slug)
	Kind  EdgeKind `json:"kind"`
	Guard string   `json:"guard"` // non-empty only for EdgeGuardedFlow
}

// ActivityDiagram is the activity-diagram model for a UseCase that has nested
// conditions (App C 1c). Required when the use case's flow contains a NodeDecision;
// that rule is enforced by artifactValidationEngine, not here.
// (projectStateAccess.md §3.4)
type ActivityDiagram struct {
	Nodes []ActivityNode `json:"nodes"`
	Edges []ActivityEdge `json:"edges"`
}

// Actor is a participant role in a UseCase per ch. 4 (the "Who" of the Four Questions).
// (projectStateAccess.md §3.4)
type Actor struct {
	ID   string `json:"id"`   // server-assigned Slug(Role); not LLM-authored
	Role string `json:"role"` // the role/actor name
}

// UseCase is the canonical typed use-case model (Grammar B, ch. 4).
// UseCase and ActivityDiagram are PLAIN VALUE TYPES — they do NOT implement
// ArtifactModel. The slot-level model CoreUseCases (defined in Task 3) holds a
// collection of UseCaseDecision values and implements ArtifactModel with KindCoreUseCases.
//
// Constraints (enforced by artifactValidationEngine, not here):
//   - a CoreUseCases collection must hold 2–6 UseCase values with Classification==ClassCore
//   - any UseCase whose flow contains a NodeDecision must have a non-nil Activity
//
// (projectStateAccess.md §3.4)
type UseCase struct {
	ID             UseCaseID        `json:"id"`   // server-assigned Slug(Name); not LLM-authored
	Name           string           `json:"name"` // the human-readable identity
	Actors         []Actor          `json:"actors"`
	Trigger        Trigger          `json:"trigger"`
	Classification Classification   `json:"classification"`
	VariationOf    *UseCaseID       `json:"variationOf"` // the core UC's id (Slug of its name); set when ClassNonCore
	Activity       *ActivityDiagram `json:"activity"`    // required when the use case has nested conditions (App C 1c)
}

// NewUseCase validates SHAPE only and returns a copy of the UseCase if valid.
// Shape check: Name must be non-empty.
// The "NodeDecision ⇒ non-nil Activity" rule is a SEMANTIC constraint enforced by
// artifactValidationEngine, NOT here.
// (projectStateAccess.md §3.3 "smart constructors enforce SHAPE only")
func NewUseCase(uc UseCase) (*UseCase, error) {
	if uc.Name == "" {
		return nil, errors.New("projectstate.NewUseCase: Name must not be empty")
	}
	out := uc
	return &out, nil
}
