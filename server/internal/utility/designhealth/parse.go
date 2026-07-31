package designhealth

import (
	"encoding/json"
	"fmt"
	"sort"
)

// parse.go decodes the tolerant slot subset the rule layer needs beyond what the
// published projectmodel parses (projectmodel covers System + Contracts only:
// "dynamicViews and all other fields are deliberately not parsed"). Every struct
// here mirrors the committed JSON shape and ignores unknown fields, exactly like
// projectmodel — a shape addition upstream never breaks this parse.
//
// Slots are located by their numeric `kind`, the stable wire identity that
// wave2-technical-design.md §0.4 keeps fixed across the reshapes (labels rename,
// kinds never). The kind→artifact mapping used here:
//
//	0 businessAlignment (vision/objectives/mission)   3 volatilities
//	2 requirements (Required Behaviors)               4 core use cases
//	5 systemDesign (components/relationships/          6 operational concepts
//	  dynamicViews)                                       (objectiveLinks; legacy
//	                                                      decisions w/ justifyingObjective)
const (
	kindBusinessAlignment   = 0
	kindRequirements        = 2
	kindVolatilities        = 3
	kindCoreUseCases        = 4
	kindSystemDesign        = 5
	kindOperationalConcepts = 6
)

// slotData is the decoded slot subset. Absent slots decode to zero values; a rule
// that joins against an absent counterpart simply finds nothing to check (the same
// posture as the framework's cross-artifact rules).
type slotData struct {
	Objectives   []objective
	Requirements []requirement
	Volatilities []volatility
	CoreUseCases []coreUseCase
	DynamicViews []dynamicView
	// SystemRevision is the systemDesign slot's current revision counter, kept for
	// the deferred slot-revision drift rule (see task report); read now so the
	// wiring is in place when per-finding basisRevision provenance lands.
	SystemRevision int
	// justifyingObjectives is the flat list of objective numbers referenced by
	// LEGACY operational-concepts decisions[].justifyingObjective — the reshape
	// removed decisions[], but older states still carry it, so the harvest is kept.
	justifyingObjectives []int
	// objectiveLinkRefs is the flattened post-reshape objective join: the
	// DeploymentOperationsModel.objectiveLinks map (knob name, e.g.
	// "deploymentScenario" → objective numbers the knob serves), one entry per
	// referenced number, ordered by knob name for deterministic findings. Each
	// entry keeps its source knob so a dangling reference can name it.
	objectiveLinkRefs []objectiveLinkRef
	// encapsulatesBlurbs maps a component id to its systemDesign `encapsulates`
	// prose. projectmodel's SystemComponent does not carry the blurb, so the interim
	// name-in-blurb volatility-encapsulation join reads it from here.
	encapsulatesBlurbs map[string]string
	// encapsulatesVolatilities maps a component id to its typed
	// encapsulatesVolatilities list — the volatility↔component join authored on the
	// systemDesign component side (each entry is the EXACT name of a committed
	// volatility; the edge lives on the component because volatilities commit before
	// the architecture exists). Read from the raw slot because the published
	// projectmodel SystemComponent cannot grow fields without a platform release —
	// the same posture as encapsulatesBlurbs. Only non-empty lists are stored, so
	// len(encapsulatesVolatilities) > 0 means the typed join is authored somewhere.
	encapsulatesVolatilities map[string][]string
}

type objective struct {
	Number    int    `json:"number"`
	Statement string `json:"statement"`
}

type requirement struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

type volatility struct {
	Name   string   `json:"name"`
	Traces []string `json:"traces"`
}

type coreUseCase struct {
	ID             string
	Classification string
	// EssenceRationale is the decision-level essence-of-the-business argument for a
	// core classification (the symmetric twin of the nonCore rejectionReason). Nil
	// when the wire field is absent or null — the DH-UC-ESSENCE-MISSING subject.
	EssenceRationale *string
	// Trigger, Actors, and Activity are the CC-* call-chain family's slot-4 join
	// surface (2026-07-30 call-chain realization): the use case's trigger kind, its
	// declared actor roster, and its activity diagram — the Grammar B half of the
	// correspondence CC-* checks against a use case's step-keyed DynamicView
	// (Grammar A). A committed use case predating these fields decodes them to
	// their zero values (empty trigger, nil actors, nil activity), which the CC
	// rules treat as "nothing to check" exactly like an absent dynamic view.
	Trigger  string
	Actors   []ucActorRef
	Activity *activityDiagram
}

// ucActorRef is a use case's actor participant — just the id the CC-* endpoint/
// edge rules resolve calls against (the use-case-scoped actor namespace).
type ucActorRef struct {
	ID string `json:"id"`
}

// activityDiagram mirrors a use case's Grammar B activity diagram: the nodes and
// edges CC-* walks (activitypaths.go) to check the DynamicView call chain against
// it.
type activityDiagram struct {
	Nodes []activityNode `json:"nodes"`
	Edges []activityEdge `json:"edges"`
}

// activityNode is one activity-diagram node. RoleName/LinkedActorID are populated
// only for a swim-lane node (CC-ACTOR-LANE's join key); LinkedActorID mirrors the
// app's nullable *string as a plain string ("" when null/absent) — JSON null into
// a non-pointer string field is a no-op for encoding/json, leaving the zero value.
type activityNode struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Label         string `json:"label"`
	RoleName      string `json:"roleName"`
	LinkedActorID string `json:"linkedActorId"`
}

// activityEdge is one directed activity-diagram edge.
type activityEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Guard string `json:"guard"`
}

// coreUseCaseSlot mirrors the core-use-cases slot model: decisions[].useCase plus
// the decision-level essenceRationale sibling.
type coreUseCaseSlot struct {
	Decisions []struct {
		UseCase struct {
			ID             string           `json:"id"`
			Classification string           `json:"classification"`
			Trigger        string           `json:"trigger"`
			Actors         []ucActorRef     `json:"actors"`
			Activity       *activityDiagram `json:"activity"`
		} `json:"useCase"`
		EssenceRationale *string `json:"essenceRationale"`
	} `json:"decisions"`
}

// dynamicView mirrors the step-keyed DynamicView wire shape (2026-07-30
// call-chain realization reshape): one callStep per realized activity node,
// each naming the calls dispatched at that node. There is no separate
// participants list — participants are derived from the steps' call
// endpoints (see chainFindings). A committed view still on the OLD
// participants/edges shape decodes here as zero steps (tolerant decode:
// unknown JSON keys are ignored), which the chain rules treat as nothing to
// check, not an error.
type dynamicView struct {
	UseCaseID string     `json:"useCaseId"`
	Key       string     `json:"key"`
	Steps     []callStep `json:"steps"`
}

type callStep struct {
	ActivityNodeID string     `json:"activityNodeId"`
	Calls          []viewEdge `json:"calls"`
}

type viewEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Mode  string `json:"mode"`
	Label string `json:"label"`
}

// opConceptsSlot mirrors the operational-concepts slot's two objective-reference
// surfaces: the typed post-reshape objectiveLinks map (knob → objective numbers)
// and the legacy decisions[].justifyingObjective back-reference — both feed the
// objective-coverage rules.
type opConceptsSlot struct {
	ObjectiveLinks map[string][]int64 `json:"objectiveLinks"`
	Decisions      []struct {
		JustifyingObjective int `json:"justifyingObjective"`
	} `json:"decisions"`
}

// objectiveLinkRef is one flattened objectiveLinks reference: the operational
// knob and the objective number it cites.
type objectiveLinkRef struct {
	Knob   string
	Number int
}

// rawProject is the tolerant top-level slice: the slots map keyed by slot number,
// each carrying its kind and raw model. Mirrors projectmodel.projectDoc.
type rawProject struct {
	Slots map[string]struct {
		Kind      int             `json:"kind"`
		Model     json.RawMessage `json:"model"`
		Revisions json.RawMessage `json:"revisions"`
	} `json:"slots"`
}

// parseSlots decodes the slot subset the rules need. It is tolerant: a missing or
// empty slot yields an empty section rather than an error. It returns an error
// only when the top-level document itself is not JSON (which EvaluateRaw treats as
// a structural failure the framework gate owns).
func parseSlots(raw []byte) (slotData, error) {
	var top rawProject
	if err := json.Unmarshal(raw, &top); err != nil {
		return slotData{}, fmt.Errorf("designhealth: parse slots: %w", err)
	}
	var out slotData

	for _, slot := range top.Slots {
		if len(slot.Model) == 0 {
			continue
		}
		switch slot.Kind {
		case kindBusinessAlignment:
			var m struct {
				Objectives []objective `json:"objectives"`
			}
			_ = json.Unmarshal(slot.Model, &m)
			out.Objectives = append(out.Objectives, m.Objectives...)
		case kindRequirements:
			var m struct {
				Items []requirement `json:"items"`
			}
			_ = json.Unmarshal(slot.Model, &m)
			out.Requirements = append(out.Requirements, m.Items...)
		case kindVolatilities:
			var m struct {
				Items []volatility `json:"items"`
			}
			_ = json.Unmarshal(slot.Model, &m)
			out.Volatilities = append(out.Volatilities, m.Items...)
		case kindCoreUseCases:
			var m coreUseCaseSlot
			_ = json.Unmarshal(slot.Model, &m)
			for _, d := range m.Decisions {
				out.CoreUseCases = append(out.CoreUseCases, coreUseCase{
					ID:               d.UseCase.ID,
					Classification:   d.UseCase.Classification,
					EssenceRationale: d.EssenceRationale,
					Trigger:          d.UseCase.Trigger,
					Actors:           d.UseCase.Actors,
					Activity:         d.UseCase.Activity,
				})
			}
		case kindSystemDesign:
			var m struct {
				Components []struct {
					ID                       string   `json:"id"`
					Encapsulates             string   `json:"encapsulates"`
					EncapsulatesVolatilities []string `json:"encapsulatesVolatilities"`
				} `json:"components"`
				DynamicViews []dynamicView `json:"dynamicViews"`
			}
			_ = json.Unmarshal(slot.Model, &m)
			out.DynamicViews = append(out.DynamicViews, m.DynamicViews...)
			if out.encapsulatesBlurbs == nil {
				out.encapsulatesBlurbs = map[string]string{}
			}
			if out.encapsulatesVolatilities == nil {
				out.encapsulatesVolatilities = map[string][]string{}
			}
			for _, c := range m.Components {
				if c.Encapsulates != "" {
					out.encapsulatesBlurbs[c.ID] = c.Encapsulates
				}
				if len(c.EncapsulatesVolatilities) > 0 {
					out.encapsulatesVolatilities[c.ID] = c.EncapsulatesVolatilities
				}
			}
			// revisions is a scalar counter; tolerate absence.
			var rev int
			if len(slot.Revisions) > 0 {
				_ = json.Unmarshal(slot.Revisions, &rev)
			}
			out.SystemRevision = rev
		case kindOperationalConcepts:
			var m opConceptsSlot
			_ = json.Unmarshal(slot.Model, &m)
			for _, d := range m.Decisions {
				if d.JustifyingObjective != 0 {
					out.justifyingObjectives = append(out.justifyingObjectives, d.JustifyingObjective)
				}
			}
			knobs := make([]string, 0, len(m.ObjectiveLinks))
			for knob := range m.ObjectiveLinks {
				knobs = append(knobs, knob)
			}
			sort.Strings(knobs)
			for _, knob := range knobs {
				for _, n := range m.ObjectiveLinks[knob] {
					out.objectiveLinkRefs = append(out.objectiveLinkRefs, objectiveLinkRef{Knob: knob, Number: int(n)})
				}
			}
		}
	}
	return out, nil
}

// requirementIDs returns the set of requirement ids for the trace-resolution join.
func (s slotData) requirementIDs() map[string]bool {
	ids := make(map[string]bool, len(s.Requirements))
	for _, r := range s.Requirements {
		if r.ID != "" {
			ids[r.ID] = true
		}
	}
	return ids
}

// objectiveNumbers returns the set of declared objective numbers.
func (s slotData) objectiveNumbers() map[int]bool {
	nums := make(map[int]bool, len(s.Objectives))
	for _, o := range s.Objectives {
		nums[o.Number] = true
	}
	return nums
}

// volatilityNames returns the set of committed volatility names — the join domain of
// the typed component↔volatility rules (exact-name membership).
func (s slotData) volatilityNames() map[string]bool {
	names := make(map[string]bool, len(s.Volatilities))
	for _, v := range s.Volatilities {
		if v.Name != "" {
			names[v.Name] = true
		}
	}
	return names
}

// typedEncapsulationActive reports whether ANY component carries a non-empty typed
// encapsulatesVolatilities list — the switch that makes the typed join authoritative
// for the whole system (parseSlots stores only non-empty lists).
func (s slotData) typedEncapsulationActive() bool {
	return len(s.encapsulatesVolatilities) > 0
}
