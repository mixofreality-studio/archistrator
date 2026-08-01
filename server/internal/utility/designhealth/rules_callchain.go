package designhealth

import (
	"fmt"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// rules_callchain.go is the app-side LIVE-TIER mirror of the platform's CC-*
// CALL-CHAIN CORRESPONDENCE family (framework-go/methodcheck/rules_callchain.go
// + activitypaths.go, 2026-07-30 callchain-realization, extended 2026-07-31 by
// the rollout rulings with decidedBy resolution and a work-bounded walker): the
// machine check that every use case's step-keyed DynamicView realization
// CORRESPONDS to that use case's activity diagram. This is the tier the
// webApp's Design Health surface actually renders (render-on-read over the
// committed project.json — see the package doc comment in designhealth.go), so
// the eleven rules below are re-derived over this package's own tolerant slices
// (dynamicView/callStep/coreUseCase) — the same posture as the rest of this
// package relative to framework-go/methodcheck (structural mirror, no shared
// types).
//
// The eleven rules (nine correspondence checks, the dangling-join-key guard,
// and the decider-attribution check):
//
//	CC-VIEW-USECASE    a view's useCaseId must resolve to a use case in slot 4
//	CC-STEP-NODE       every step keys a node the diagram declares
//	CC-STEP-UNIQUE     at most one step per activity node
//	CC-COVERAGE        every step-REQUIRING node is realized, and every step keys a
//	                   step-ELIGIBLE node
//	CC-STEP-NONEMPTY   a realized step makes at least one call
//	CC-ENDPOINT-RESOLVES  every call endpoint resolves to exactly one of {Component, Actor}
//	CC-ACTOR-EDGE      an actor may only interact, synchronously, with a Client
//	CC-ACTOR-LANE      a lane-linked node's step must touch that actor
//	CC-DECIDED-BY      a node's decider sits on a branching node and resolves
//	CC-TRIGGER-EVENT   the use-case trigger and the diagram's entry nodes agree
//	CC-PATH-CONNECTED  every activity-diagram PATH is realized as a connected chain
//
// A twelfth rule, CUC-ACTOR-REQUIRED, lives in this file too (see
// actorRequiredFindings below) even though it is CoreUseCases-attributed rather
// than per-dynamic-view: the rollout rulings shipped it alongside CC-DECIDED-BY
// as the pass's two new rules, and designhealth has no separate
// artifact-family split the way the platform's rules.go/rules_callchain.go do.
//
// SEVERITY: the whole family is advisory in this PoC (ccLiveSeverity =
// SeverityWarning below) — the post-QA rollout flips it to Error, mirroring the
// platform's ccGateSeverity.
//
// SECTION GRAMMAR (binding — the webApp's Design Health surface joins its
// per-step finding badges on these exact strings, NOT on a title-first label):
// step-scoped findings use "dynamicView " + <view key, falling back to
// useCaseId only when the key is empty> + " step " + <activity node id>;
// use-case-scoped findings use "useCase " + <use case id>. See ccViewLabel and
// ccContext.stepLoc/ucLoc below. Both tiers now share this key-first grammar
// (rollout rulings 2026-07-31 brought the platform in line — see its
// ccKeyLabel), so the comparison this comment used to draw against the
// platform's title-first section grammar no longer applies; what's still true,
// and worth keeping in mind, is that this remains the OPPOSITE priority of
// dvLabel (useCaseId-first, used by the pre-existing DH-CHAIN-* rules in
// rules_chains.go) — the key, not the title or the use-case id, is the stable
// identity the app's join relies on. MESSAGE TEXT is a different matter: this
// tier has always led with the key there too (ccViewLabel), while the platform
// keeps its title-first viewLabel in message text — that divergence is
// intentional (only Section is the cross-tier join key) and is not "corrected"
// by this port.
//
// ACTORS: an endpoint id is resolved against the component index UNION the
// OWNING use case's Actors. Actors are per-use-case, so the same id may name an
// actor in one use case and nothing in another — resolution is always relative
// to the view's use case. ActivityNode.DecidedBy resolves in exactly the same
// two namespaces.
const ccLiveSeverity = methodcheck.SeverityWarning

// ccMustHaveStep is the set of activity-node kinds that MUST carry a realizing
// step: they are the nodes that DO something, so a call chain has to say what
// calls they make.
var ccMustHaveStep = map[string]bool{
	"action":      true,
	"timeEvent":   true,
	"acceptEvent": true,
}

// ccMayHaveStep is the set of node kinds a step is ALLOWED but not required to
// key: a decision/switch may itself make a call (asking an Engine for the
// verdict it branches on) or may branch purely on already-held state. Every
// other kind (start/end/merge/join/fork/swimLane/note/loop/goto/interruptEdge)
// is pure control flow and must NOT carry a step.
var ccMayHaveStep = map[string]bool{
	"decision": true,
	"switch":   true,
}

// callChainFindings validates every dynamic view's realization against its
// owning use case's activity diagram: the dangling-join-key guard, coverage,
// endpoint resolution, actor legality, trigger alignment, and per-path chain
// connectivity.
func callChainFindings(in Input) []methodcheck.Finding {
	idx := in.componentIndex()
	ucByID := useCaseIndex(in.Slots.CoreUseCases)
	var out []methodcheck.Finding
	out = append(out, actorRequiredFindings(in.Slots.CoreUseCases)...)
	for i, dv := range in.Slots.DynamicViews {
		uc, ok := ucByID[dv.UseCaseID]
		if !ok {
			// CC-VIEW-USECASE. A view whose UseCaseID resolves to NOTHING silently
			// disables every other CC-* rule for it, and no other rule notices —
			// report the dangling join key rather than no-op'ing the whole family.
			out = append(out, ccFinding(RuleCCViewUseCase, i, "dynamicView "+ccViewLabel(dv),
				"dynamic view %q references useCaseId %q, which resolves to no use case in the committed set; the call chain cannot be checked against any activity diagram until the join key is fixed",
				ccViewLabel(dv), dv.UseCaseID))
			continue
		}
		// A use case with no activity diagram has nothing to correspond TO — that
		// gap belongs to the activity-diagram-presence rule, not this family.
		if uc.Activity == nil {
			continue
		}
		out = append(out, newCCContext(dv, uc, idx, i).findings()...)
	}
	return out
}

// ---- CUC-ACTOR-REQUIRED ----

// actorRequiredFindings — CUC-ACTOR-REQUIRED (founder ruling R-A, rollout
// rulings 2026-07-31). A clientAction use case is, by definition, initiated BY
// somebody: declaring zero actors leaves the initiator unnamed, and leaves the
// realization with no legal chain root either (CC-PATH-CONNECTED roots a
// clientAction path on actor→Client). Timer- and busMessage-triggered use
// cases are started by the clock or the bus and legitimately declare none.
//
// Unlike the rest of this file's CC-* family, this rule is NOT per-dynamic-view
// — it runs over every committed use case regardless of whether a dynamic view
// exists for it yet — so callChainFindings calls it directly rather than
// folding it into ccContext.findings().
func actorRequiredFindings(ucs []coreUseCase) []methodcheck.Finding {
	var out []methodcheck.Finding
	for i, uc := range ucs {
		if uc.Trigger != "clientAction" || len(uc.Actors) > 0 {
			continue
		}
		out = append(out, ccFinding(RuleCUCActorRequired, i, "useCase "+uc.ID,
			"use case %q is clientAction-triggered but declares no actors; a client-initiated use case must name who initiates it (and its call chain needs that actor as its root)",
			uc.ID))
	}
	return out
}

// useCaseIndex maps use-case id → coreUseCase across the whole slot-4 decision
// set (core AND nonCore/variation — every one of them owns a dynamic view).
// Last decision wins on a duplicate id.
func useCaseIndex(ucs []coreUseCase) map[string]coreUseCase {
	idx := make(map[string]coreUseCase, len(ucs))
	for _, uc := range ucs {
		idx[uc.ID] = uc
	}
	return idx
}

// actorIDs returns the id set of a use case's declared actors.
func actorIDs(uc coreUseCase) map[string]bool {
	ids := make(map[string]bool, len(uc.Actors))
	for _, a := range uc.Actors {
		ids[a.ID] = true
	}
	return ids
}

// ccViewLabel is the CC-* section-grammar view identifier: the view's Key,
// falling back to its UseCaseID only when Key is empty.
func ccViewLabel(dv dynamicView) string {
	if dv.Key != "" {
		return dv.Key
	}
	return dv.UseCaseID
}

// ccContext is one dynamic view's evaluation context: the view, its owning use
// case, and the indices every CC rule joins on. Bundling them keeps each rule a
// method with no parameter train.
type ccContext struct {
	dv       dynamicView
	uc       coreUseCase
	idx      map[string]projectmodel.SystemComponent // component id → Component
	actors   map[string]bool                         // actor id (of THIS use case) → true
	nodes    map[string]activityNode                 // activity node id → node
	steps    map[string]callStep                     // activity node id → the step realizing it (first wins)
	incoming map[string]int                          // activity node id → count of incoming edges
	ordinal  int                                     // the dynamic view's position (finding order key)
}

func newCCContext(dv dynamicView, uc coreUseCase, idx map[string]projectmodel.SystemComponent, ordinal int) ccContext {
	nodes := make(map[string]activityNode, len(uc.Activity.Nodes))
	for _, n := range uc.Activity.Nodes {
		nodes[n.ID] = n
	}
	steps := make(map[string]callStep, len(dv.Steps))
	for _, st := range dv.Steps {
		if _, dup := steps[st.ActivityNodeID]; !dup {
			steps[st.ActivityNodeID] = st
		}
	}
	incoming := make(map[string]int, len(uc.Activity.Nodes))
	for _, e := range uc.Activity.Edges {
		incoming[e.To]++
	}
	return ccContext{
		dv: dv, uc: uc, idx: idx, actors: actorIDs(uc),
		nodes: nodes, steps: steps, incoming: incoming, ordinal: ordinal,
	}
}

// findings runs the whole family over one view, in stable rule order.
func (cc ccContext) findings() []methodcheck.Finding {
	var out []methodcheck.Finding
	out = append(out, cc.stepIdentity()...)
	out = append(out, cc.coverage()...)
	out = append(out, cc.stepNonempty()...)
	out = append(out, cc.endpointResolves()...)
	out = append(out, cc.actorEdges()...)
	out = append(out, cc.actorLane()...)
	out = append(out, cc.decidedBy()...)
	out = append(out, cc.triggerEvent()...)
	out = append(out, cc.pathConnected()...)
	return out
}

// stepLoc is the STEP-SCOPED section grammar. The webApp joins its per-step
// finding badges on this exact string — do not reshape it without the app side.
func (cc ccContext) stepLoc(nodeID string) string {
	return "dynamicView " + ccViewLabel(cc.dv) + " step " + nodeID
}

// ucLoc is the USE-CASE-SCOPED section grammar (coverage + trigger findings,
// which are statements about the use case as a whole, not about any one step).
func (cc ccContext) ucLoc() string {
	return "useCase " + cc.uc.ID
}

// ccFinding builds a finding at the family's shared PoC severity.
func ccFinding(id methodcheck.RuleID, ordinal int, section, format string, args ...any) methodcheck.Finding {
	return finding(id, ccLiveSeverity, ordinal, section, fmt.Sprintf(format, args...))
}

// ---- CC-STEP-NODE / CC-STEP-UNIQUE ----

// stepIdentity checks that each step names a REAL activity node (CC-STEP-NODE)
// and that no node is realized twice (CC-STEP-UNIQUE).
func (cc ccContext) stepIdentity() []methodcheck.Finding {
	var out []methodcheck.Finding
	seen := make(map[string]bool, len(cc.dv.Steps))
	for _, st := range cc.dv.Steps {
		if _, ok := cc.nodes[st.ActivityNodeID]; !ok {
			out = append(out, ccFinding(RuleCCStepNode, cc.ordinal, cc.stepLoc(st.ActivityNodeID),
				"dynamic view %q has a step keyed on %q, which is not a node of use case %s's activity diagram; every step must realize a declared activity node",
				ccViewLabel(cc.dv), st.ActivityNodeID, cc.uc.ID))
		}
		if seen[st.ActivityNodeID] {
			out = append(out, ccFinding(RuleCCStepUnique, cc.ordinal, cc.stepLoc(st.ActivityNodeID),
				"dynamic view %q realizes activity node %q with more than one step; a node's calls belong to exactly one step",
				ccViewLabel(cc.dv), st.ActivityNodeID))
		}
		seen[st.ActivityNodeID] = true
	}
	return out
}

// ---- CC-COVERAGE ----

// coverage is the BIDIRECTIONAL step/node correspondence: every node that must
// be realized IS (diagram → view), and every step keys a node that may legally
// carry one (view → diagram). Dangling steps are CC-STEP-NODE's business and
// are skipped here.
func (cc ccContext) coverage() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, n := range cc.uc.Activity.Nodes {
		if !ccMustHaveStep[n.Kind] {
			continue
		}
		if _, ok := cc.steps[n.ID]; ok {
			continue
		}
		out = append(out, ccFinding(RuleCCCoverage, cc.ordinal, cc.ucLoc(),
			"activity node %q (%s) of use case %s is realized by no step of dynamic view %q; every action/timeEvent/acceptEvent node must say which calls it makes",
			n.ID, n.Kind, cc.uc.ID, ccViewLabel(cc.dv)))
	}
	for _, st := range cc.dv.Steps {
		n, ok := cc.nodes[st.ActivityNodeID]
		if !ok || ccMustHaveStep[n.Kind] || ccMayHaveStep[n.Kind] {
			continue
		}
		out = append(out, ccFinding(RuleCCCoverage, cc.ordinal, cc.ucLoc(),
			"dynamic view %q attaches a step to %s node %q; only action/timeEvent/acceptEvent nodes carry calls (decision/switch may), every other node is pure control flow",
			ccViewLabel(cc.dv), n.Kind, n.ID))
	}
	return out
}

// ---- CC-STEP-NONEMPTY ----

// stepNonempty: a realized step that makes no call says nothing — either the
// node makes calls (and they belong here) or it should carry no step at all.
func (cc ccContext) stepNonempty() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, st := range cc.dv.Steps {
		if len(st.Calls) > 0 {
			continue
		}
		out = append(out, ccFinding(RuleCCStepNonempty, cc.ordinal, cc.stepLoc(st.ActivityNodeID),
			"dynamic view %q step %q makes no call; a realized step must carry at least one call (drop the step if the node makes none)",
			ccViewLabel(cc.dv), st.ActivityNodeID))
	}
	return out
}

// ---- CC-ENDPOINT-RESOLVES ----

// endpointResolves checks that every call endpoint resolves to EXACTLY ONE of
// the two namespaces a call chain draws from: the System's components, or the
// owning use case's actors. Reported once per distinct id per view, at the
// first step that names it.
func (cc ccContext) endpointResolves() []methodcheck.Finding {
	var out []methodcheck.Finding
	reported := map[string]bool{}
	for _, st := range cc.dv.Steps {
		for _, call := range st.Calls {
			out = append(out, cc.callEndpointFindings(call, st.ActivityNodeID, reported)...)
		}
	}
	return out
}

// callEndpointFindings resolves one call's two ends, skipping ids already
// reported for this view.
func (cc ccContext) callEndpointFindings(call viewEdge, nodeID string, reported map[string]bool) []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, id := range []string{call.From, call.To} {
		if reported[id] {
			continue
		}
		if f, bad := cc.endpointFinding(id, nodeID); bad {
			reported[id] = true
			out = append(out, f)
		}
	}
	return out
}

func (cc ccContext) endpointFinding(id, nodeID string) (methodcheck.Finding, bool) {
	_, isComponent := cc.idx[id]
	isActor := cc.actors[id]
	switch {
	case isComponent && isActor:
		return ccFinding(RuleCCEndpoint, cc.ordinal, cc.stepLoc(nodeID),
			"dynamic view %q names endpoint %q, which resolves to BOTH a System Component and an actor of use case %s; an endpoint id must denote exactly one of them",
			ccViewLabel(cc.dv), id, cc.uc.ID), true
	case !isComponent && !isActor:
		return ccFinding(RuleCCEndpoint, cc.ordinal, cc.stepLoc(nodeID),
			"dynamic view %q names endpoint %q, which is neither a System Component nor an actor of use case %s",
			ccViewLabel(cc.dv), id, cc.uc.ID), true
	}
	return methodcheck.Finding{}, false
}

// ---- CC-ACTOR-EDGE ----

// actorEdges enforces the actor-interaction grammar: a person touches the
// system only through a Client, and only synchronously. Two actors talking to
// each other is not a system call at all.
func (cc ccContext) actorEdges() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, st := range cc.dv.Steps {
		for _, call := range st.Calls {
			out = append(out, cc.actorEdgeFindings(call, st.ActivityNodeID)...)
		}
	}
	return out
}

func (cc ccContext) actorEdgeFindings(call viewEdge, nodeID string) []methodcheck.Finding {
	fromActor, toActor := cc.actors[call.From], cc.actors[call.To]
	if !fromActor && !toActor {
		return nil
	}
	section := cc.stepLoc(nodeID)
	if fromActor && toActor {
		return []methodcheck.Finding{ccFinding(RuleCCActorEdge, cc.ordinal, section,
			"dynamic view %q step %q draws actor %s → actor %s; an actor edge models a person entering the system through a Client, not two people interacting",
			ccViewLabel(cc.dv), nodeID, call.From, call.To)}
	}
	component := call.To
	if toActor {
		component = call.From
	}
	var out []methodcheck.Finding
	if c, ok := cc.idx[component]; !ok || c.Kind != "client" {
		out = append(out, ccFinding(RuleCCActorEdge, cc.ordinal, section,
			"dynamic view %q step %q draws actor edge %s→%s, whose non-actor end %s is not a Client component; an actor enters the system only through a Client",
			ccViewLabel(cc.dv), nodeID, call.From, call.To, component))
	}
	if call.Mode != "sync" {
		out = append(out, ccFinding(RuleCCActorEdge, cc.ordinal, section,
			"dynamic view %q step %q draws actor edge %s→%s with mode %q; an actor interaction is always synchronous",
			ccViewLabel(cc.dv), nodeID, call.From, call.To, call.Mode))
	}
	return out
}

// ---- CC-ACTOR-LANE ----

// actorLane ties the diagram's swim-lane assignment to the realization: a node
// placed in an actor's lane claims that actor performs it, so the step
// realizing that node must actually touch the actor. A lane no call honors is
// decoration.
func (cc ccContext) actorLane() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, n := range cc.uc.Activity.Nodes {
		if n.LinkedActorID == "" {
			continue
		}
		st, ok := cc.steps[n.ID]
		if !ok || stepTouches(st, n.LinkedActorID) {
			continue
		}
		out = append(out, ccFinding(RuleCCActorLane, cc.ordinal, cc.stepLoc(n.ID),
			"activity node %q is laned to actor %s, but dynamic view %q's step for it touches that actor in no call; realize the actor's participation or drop the lane link",
			n.ID, n.LinkedActorID, ccViewLabel(cc.dv)))
	}
	return out
}

// stepTouches reports whether any of a step's calls names id at either end.
func stepTouches(st callStep, id string) bool {
	for _, call := range st.Calls {
		if call.From == id || call.To == id {
			return true
		}
	}
	return false
}

// ---- CC-DECIDED-BY ----

// decidedBy checks the optional decider attribution an activity node may carry
// (rollout rulings 2026-07-31), in its two halves:
//
//   - PLACEMENT: only a decision/switch RESOLVES a branch, so only those kinds
//     can name who resolves it. A decidedBy anywhere else is misplaced even
//     when its value resolves perfectly well.
//   - RESOLUTION: the value resolves exactly like a call endpoint — against
//     the System's components UNION the owning use case's actors. Naming
//     neither is a dangling attribution; naming BOTH is ambiguous, for the
//     same reason CC-ENDPOINT-RESOLVES treats a both-match as one finding.
//
// The rule is USE-CASE-scoped: a node is not a step, so there is no step
// section to hang it on.
func (cc ccContext) decidedBy() []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, n := range cc.uc.Activity.Nodes {
		if n.DecidedBy == "" {
			continue
		}
		if !ccResolvesBranch(n.Kind) {
			out = append(out, ccFinding(RuleCCDecidedBy, cc.ordinal, cc.ucLoc(),
				"activity node %q (%s) of use case %s carries decidedBy %q; only a decision/switch node resolves a branch, so only those kinds may name who resolves it",
				n.ID, n.Kind, cc.uc.ID, n.DecidedBy))
			continue
		}
		if f, bad := cc.decidedByResolution(n); bad {
			out = append(out, f)
		}
	}
	return out
}

// ccResolvesBranch reports whether a node kind resolves a branch — the only
// kinds a decidedBy may sit on. It coincides with ccMayHaveStep's membership,
// but for a different reason (that set is about carrying CALLS), so the two
// are kept apart.
func ccResolvesBranch(kind string) bool {
	return kind == "decision" || kind == "switch"
}

// decidedByResolution resolves one branching node's decider against the two
// namespaces, mirroring endpointFinding.
func (cc ccContext) decidedByResolution(n activityNode) (methodcheck.Finding, bool) {
	_, isComponent := cc.idx[n.DecidedBy]
	isActor := cc.actors[n.DecidedBy]
	switch {
	case isComponent && isActor:
		return ccFinding(RuleCCDecidedBy, cc.ordinal, cc.ucLoc(),
			"activity node %q of use case %s is decidedBy %q, which resolves to BOTH a System Component and an actor of that use case; a decider id must denote exactly one of them",
			n.ID, cc.uc.ID, n.DecidedBy), true
	case !isComponent && !isActor:
		return ccFinding(RuleCCDecidedBy, cc.ordinal, cc.ucLoc(),
			"activity node %q of use case %s is decidedBy %q, which is neither a System Component nor an actor of that use case; name the component or the person who resolves the branch",
			n.ID, cc.uc.ID, n.DecidedBy), true
	}
	return methodcheck.Finding{}, false
}

// ---- CC-TRIGGER-EVENT ----

// triggerEvent aligns the use-case trigger with the diagram's ENTRY nodes: a
// timer trigger must enter on a timeEvent, a bus-message trigger on an
// acceptEvent, and a client-action trigger on neither. An "entry" here is an
// event node with no incoming edge — nothing leads into a trigger.
func (cc ccContext) triggerEvent() []methodcheck.Finding {
	hasTimeEntry, hasAcceptEntry := cc.eventEntries()
	switch cc.uc.Trigger {
	case "timer":
		if !hasTimeEntry {
			return []methodcheck.Finding{ccFinding(RuleCCTriggerEvent, cc.ordinal, cc.ucLoc(),
				"use case %s is timer-triggered but its activity diagram declares no timeEvent entry node; a scheduled use case enters on the timer that fires it",
				cc.uc.ID)}
		}
	case "busMessage":
		if !hasAcceptEntry {
			return []methodcheck.Finding{ccFinding(RuleCCTriggerEvent, cc.ordinal, cc.ucLoc(),
				"use case %s is busMessage-triggered but its activity diagram declares no acceptEvent entry node; a message-driven use case enters on the signal it accepts",
				cc.uc.ID)}
		}
	case "clientAction":
		if hasTimeEntry || hasAcceptEntry {
			return []methodcheck.Finding{ccFinding(RuleCCTriggerEvent, cc.ordinal, cc.ucLoc(),
				"use case %s is clientAction-triggered but its activity diagram enters on a UML event node; a client-initiated use case enters at a start node — reclassify the trigger as timer/busMessage or remove the event entry",
				cc.uc.ID)}
		}
	}
	return nil
}

// eventEntries reports whether the diagram carries a timeEvent (resp.
// acceptEvent) node with no incoming edge — the shape that makes an event node
// the diagram's entry. An event node WITH an incoming edge is a mid-flow event,
// not a trigger, and does not count.
func (cc ccContext) eventEntries() (timeEntry, acceptEntry bool) {
	for _, n := range cc.uc.Activity.Nodes {
		if cc.incoming[n.ID] > 0 {
			continue
		}
		switch n.Kind {
		case "timeEvent":
			timeEntry = true
		case "acceptEvent":
			acceptEntry = true
		}
	}
	return timeEntry, acceptEntry
}

// ---- CC-PATH-CONNECTED ----

// pathConnected is the heart of the correspondence: for EVERY entry→end path
// the activity diagram admits (activityPaths below enumerates them), the steps
// realized along that path must compose into a CONNECTED call chain.
//
// A call is connected when any of these holds:
//   - it is an actor→Client call — a person entering the system always
//     re-seeds the chain, at any point;
//   - it is the path's FIRST call and matches the entry kind's root shape
//     (timeEvent → Client→Manager; acceptEvent → a queued call into a
//     Manager; a start/clientAction entry → the actor→Client case above);
//   - its From is already in the reached set.
//
// Findings are deduplicated across paths by (node, from, to): one disconnect
// reported once, not once per path that happens to traverse it.
//
// EVENT-ROOTED SUFFIX PATHS ARE SKIPPED (see ccIsSuffixPath): activityPaths
// treats every EVENT node as an enumeration root wherever it sits, so a
// mid-flow event node also yields the bare suffix path starting at it. Walking
// that suffix would restart with an empty reached set and misjudge the event's
// step against the wrong root shape. A START-rooted path is NEVER a suffix —
// it is always a primary entry, incoming back-edge or not.
func (cc ccContext) pathConnected() []methodcheck.Finding {
	var out []methodcheck.Finding
	reported := map[string]bool{}
	for _, p := range activityPaths(*cc.uc.Activity) {
		if cc.ccIsSuffixPath(p.Entry) {
			continue
		}
		out = append(out, cc.walkRealizedPath(p.Entry, p.Nodes, reported)...)
	}
	return out
}

// ccIsSuffixPath reports whether an enumerated path is a mere SUFFIX of
// another path this walk already covers. The predicate is membership in the
// EVENT kinds AND a non-zero incoming count — both, never the count alone (a
// start-rooted path with a back-edge is an ordinary authored "retry" shape and
// must always be walked as a primary entry).
func (cc ccContext) ccIsSuffixPath(entry pathEntry) bool {
	switch entry.Kind {
	case "timeEvent", "acceptEvent":
		return cc.incoming[entry.NodeID] > 0
	default: // "start" — always a primary entry, always walked.
		return false
	}
}

// ccPathWalk is the mutable state carried ALONG one path: which endpoints the
// chain has reached so far, and whether the next call is still the path's
// first.
type ccPathWalk struct {
	reached map[string]bool
	first   bool
}

func (cc ccContext) walkRealizedPath(entry pathEntry, nodeIDs []string, reported map[string]bool) []methodcheck.Finding {
	w := &ccPathWalk{reached: map[string]bool{}, first: true}
	var out []methodcheck.Finding
	for _, nodeID := range nodeIDs {
		st, ok := cc.steps[nodeID]
		if !ok {
			continue
		}
		out = append(out, cc.walkStepCalls(st, nodeID, entry, w, reported)...)
	}
	return out
}

// walkStepCalls threads one realized step's call fragment through the walk
// state, reporting each call that is neither legally rooted nor continuing
// from a reached endpoint.
func (cc ccContext) walkStepCalls(st callStep, nodeID string, entry pathEntry, w *ccPathWalk, reported map[string]bool) []methodcheck.Finding {
	var out []methodcheck.Finding
	for _, call := range st.Calls {
		if !cc.callConnects(call, entry, w.first, w.reached) {
			if f, fresh := cc.disconnectFinding(call, nodeID, entry, reported); fresh {
				out = append(out, f)
			}
		}
		w.reached[call.From] = true
		w.reached[call.To] = true
		w.first = false
	}
	return out
}

// disconnectFinding builds the CC-PATH-CONNECTED finding for one disconnected
// call, returning fresh=false when this (node, from, to) was already reported
// on an earlier path.
func (cc ccContext) disconnectFinding(call viewEdge, nodeID string, entry pathEntry, reported map[string]bool) (methodcheck.Finding, bool) {
	key := nodeID + "\x00" + call.From + "\x00" + call.To
	if reported[key] {
		return methodcheck.Finding{}, false
	}
	reported[key] = true
	return ccFinding(RuleCCPathConnected, cc.ordinal, cc.stepLoc(nodeID),
		"dynamic view %q step %q calls %s→%s, but %s is not reached by any earlier call on the activity path entered at %q, and the call is not a legal chain root (actor→Client, or the entry's own root shape)",
		ccViewLabel(cc.dv), nodeID, call.From, call.To, call.From, entry.NodeID), true
}

func (cc ccContext) callConnects(call viewEdge, entry pathEntry, first bool, reached map[string]bool) bool {
	if cc.isActorToClient(call) {
		return true
	}
	if first {
		return cc.rootsEntry(call, entry)
	}
	return reached[call.From]
}

// isActorToClient reports the always-legal chain root: an actor of this use
// case entering a Client component.
func (cc ccContext) isActorToClient(call viewEdge) bool {
	if !cc.actors[call.From] {
		return false
	}
	to, ok := cc.idx[call.To]
	return ok && to.Kind == "client"
}

// rootsEntry reports whether a path's FIRST call is a legal root for that
// path's entry kind. A start entry (the clientAction shape) has only ONE legal
// root — the actor→Client call callConnects already accepted — so it is false
// here.
func (cc ccContext) rootsEntry(call viewEdge, entry pathEntry) bool {
	from, fromOK := cc.idx[call.From]
	to, toOK := cc.idx[call.To]
	switch entry.Kind {
	case "timeEvent":
		return fromOK && toOK && from.Kind == "client" && to.Kind == "manager"
	case "acceptEvent":
		return toOK && to.Kind == "manager" && call.Mode == "queued"
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// activityPaths — PURE, package-private plumbing enumerating every entry→end
// node-id path through an activityDiagram. This is a package-local copy of the
// platform's framework-go/methodcheck/activitypaths.go (the same
// designhealth-vs-methodcheck twin duplication the rest of this package
// already carries), ported verbatim including its bounding rules:
//
//   - entries are every "start" node PLUS every UML event node (timeEvent/
//     acceptEvent), wherever they sit; loops (back-edges) are traversed AT
//     MOST ONCE per path via a per-path visited-EDGE set; a fork's branches
//     cross-product (each branch's own alternative set computed exactly once)
//     and concatenate into the SAME path (fork-without-join legal).
//   - the OUTPUT cap (maxActivityPaths) truncates the returned set, applied
//     EXACTLY ONCE as a final truncation of whatever was enumerated.
//   - the WORK budget (maxWalkWork, 2026-07-31 rollout rulings) bounds the
//     RECURSION itself: designhealth runs this render-on-read over committed
//     state, so a pathological diagram is a CPU/memory sink even with the
//     output capped — truncating the output alone still requires
//     materializing the complete result first. The budget is charged per
//     WALK-STEP (one node id materialized into a sequence — see spend/carry),
//     never per final path.
//
// On exhaustion the walk stops EXPLORING; the walks it had already COMPLETED
// are still carried up to the caller, capped at maxActivityPaths per level
// (carry), so a blowup degrades to a smaller — often still cap-sized — answer
// instead of an empty or fabricated one. The degradation is deterministic
// (entries in declared order, branches in declared edge order) and only ever
// UNDER-approximates: every returned path is a real, complete path of the
// diagram, so CC-PATH-CONNECTED can lose a finding to a pathological diagram
// but can never gain a false one.
//
// carry's ONE exclusion: crossProduct is deliberately NOT wrapped in carry —
// it charges via spend directly, raw. Exempting it from the post-exhaustion
// escape hatch is what keeps the fork's per-combination visited-edge-set
// allocation (the memory-heaviest shape) from reopening the hole
// charge-by-length closed. One consequence, deliberate and NOT a bug: a
// pure-fork-only blowup — the diagram's only entry leads into a fork that
// never finishes folding — degrades to exhausted=true with a length-0 result,
// not a partial answer. That is the pre-existing all-or-nothing-fork design
// (walkFork returns nil the moment any branch or fold comes back empty),
// unchanged by this port; see TestPaths_PureForkOnlyBlowupYieldsEmptyResult.
// ---------------------------------------------------------------------------

// maxActivityPaths caps the total number of enumerated paths per diagram
// (across ALL entries).
const maxActivityPaths = 512

// maxWalkWork caps the enumeration's WALK-STEPS per diagram. One step is one
// node id MATERIALIZED into a walk sequence — see spend and carry, which
// between them charge every sequence the walk builds, both where one is
// created and where one is COPIED a level up. Charging the copies is what
// makes the budget a memory bound and not merely a CPU one.
//
// SIZING (re-derived from the platform's 2026-07-31 fix-round-1 measurement):
// the honest claim is stated in terms of the OUTPUT CAP, not node count — any
// diagram whose COMPLETE enumeration would fit the cap (<=512 paths, <=40
// nodes deep, widest admitted fan) costs at most ~550k steps, so it is never
// budget-truncated: its result is bit-identical to an unbounded walk's. A
// 22-decision reconverging chain (4.2M true paths, no fork) returns a full
// 512 paths at this budget having allocated well under the 256MB ceiling
// TestPaths_BudgetBoundsDecisionChain asserts; an 8-branch fork of 5-way
// decisions (390,625 combinations) trips the budget promptly. Raising the
// budget further is not free: the fork shape allocates far more per step than
// the decision shape (each combination unions two visited-edge SETS), so a
// budget generous enough to fully enumerate a several-thousand-path chain
// would put the fork-shaped worst case back into the memory-sink range this
// bound exists to prevent — and such a diagram is truncated to the 512-path
// cap either way, so nothing real is lost by holding the line here.
const maxWalkWork = 1_000_000

// pathEntry describes one enumeration root of an activity diagram.
type pathEntry struct {
	NodeID string
	Kind   string // "start", "timeEvent", "acceptEvent"
}

// activityPath is one enumerated entry→end path: its root and the node ids it
// visits, in walk order.
type activityPath struct {
	Entry pathEntry
	Nodes []string // node ids in walk order, Entry.NodeID first
}

// activityWalk is one in-progress (or completed) DFS walk: the node-id
// sequence produced so far, plus the set of edge indices already consumed
// along it.
type activityWalk struct {
	seq     []string
	visited map[int]bool
}

// walker carries the diagram-wide, walk-invariant enumeration state — the
// edge index, the node kinds, and the remaining work budget — so each step of
// the recursion is a method with no parameter train.
type walker struct {
	edges       []activityEdge
	kindByID    map[string]string
	edgesByFrom map[string][]int // edge INDICES by From node, in declared order
	remaining   int              // walk-steps left in this diagram's budget
	exhausted   bool             // sticky: set the first time a charge is refused
}

// activityPaths enumerates every entry→end node-id path of a (see the file
// header for the two bounds).
func activityPaths(a activityDiagram) []activityPath {
	paths, _ := boundedActivityPaths(a)
	return paths
}

// boundedActivityPaths is activityPaths plus the budget verdict: exhausted
// reports whether the walk stopped early because maxWalkWork ran out — the
// difference between "this diagram has 3 paths" and "this diagram has more
// paths than anyone can enumerate". Kept package-private (rules read
// activityPaths; the walker's own tests read this) so the tests can assert on
// the verdict instead of a flaky wall-clock assertion.
func boundedActivityPaths(a activityDiagram) (paths []activityPath, exhausted bool) {
	w := newWalker(a)
	var out []activityPath
	for _, entry := range diagramEntries(a) {
		for _, walk := range w.walkFrom(entry.NodeID, map[int]bool{}) {
			out = append(out, activityPath{Entry: entry, Nodes: walk.seq})
		}
	}
	if len(out) > maxActivityPaths {
		out = out[:maxActivityPaths]
	}
	return out, w.exhausted
}

func newWalker(a activityDiagram) *walker {
	kindByID := make(map[string]string, len(a.Nodes))
	for _, n := range a.Nodes {
		kindByID[n.ID] = n.Kind
	}
	// edgesByFrom groups edge INDICES (not copies) by their From node,
	// preserving the diagram's declared Edges order — that order decides both
	// branch order (decision/switch) and concatenation order (fork).
	edgesByFrom := make(map[string][]int, len(a.Nodes))
	for i, e := range a.Edges {
		edgesByFrom[e.From] = append(edgesByFrom[e.From], i)
	}
	return &walker{edges: a.Edges, kindByID: kindByID, edgesByFrom: edgesByFrom, remaining: maxWalkWork}
}

// diagramEntries lists the diagram's enumeration roots in declared node order.
func diagramEntries(a activityDiagram) []pathEntry {
	var entries []pathEntry
	for _, n := range a.Nodes {
		switch n.Kind {
		case "start", "timeEvent", "acceptEvent":
			entries = append(entries, pathEntry{NodeID: n.ID, Kind: n.Kind})
		}
	}
	return entries
}

// spend charges n walk-steps of MATERIALIZATION — a terminal walk, a fork
// combination, or (through carry) a completed sub-walk copied a level up.
// Every sequence the walk builds goes through here, which is what makes the
// budget a memory bound and not merely a CPU one. Refusal is sticky — once
// the walk is out of budget it stays out, so the result cannot depend on the
// order in which the remainder of the recursion happened to ask.
func (w *walker) spend(n int) bool {
	if w.exhausted || w.remaining < n {
		w.exhausted = true
		return false
	}
	w.remaining -= n
	return true
}

// carry charges n walk-steps of ASSEMBLY — copying an ALREADY-COMPLETED
// sub-walk up one level — where assembled is how many walks this level has
// carried so far.
//
// While the budget holds, assembly is charged exactly like exploration: the
// copy is real materialization, and NOT charging it collapses the budget to a
// walk-COUNT bound instead of a materialization bound (a decision-shaped
// blowup then spikes memory well past the intended ceiling — the same defect
// class the platform's fix-round-1 measured and fixed).
//
// Once exploration is exhausted, assembly continues UNBUDGETED but capped at
// maxActivityPaths per level. That is the graceful-degradation half: the
// walks already completed must be able to reach the caller instead of being
// stranded one frame below it (charging assembly with no escape hatch would
// drain the budget bottom-up and collapse every blowup to zero paths), and
// carrying more than the output cap is pure waste because the caller
// truncates to it anyway. The escape hatch is structurally bounded — at most
// cap x depth node ids per level, over at most depth levels — so it cannot
// reopen the memory hole the charging closed.
//
// crossProduct does NOT go through carry (it calls spend directly) — see the
// file header's carry-exclusion note.
func (w *walker) carry(assembled, n int) bool {
	if !w.exhausted && w.spend(n) {
		return true
	}
	return assembled < maxActivityPaths
}

// walkFrom performs the recursive DFS described on activityPaths, returning
// every completed walk starting at nodeID given the edges already visited on
// the path so far. An EMPTY return means the budget ran out before ANYTHING
// below this node completed (an unexhausted walk always yields at least one
// walk — worst case, the terminal one — and an exhausted one still carries up
// whatever did complete). walkFork relies on that invariant to tell "this
// branch contributed nothing" from "this branch contributed fewer
// alternatives than it would have".
func (w *walker) walkFrom(nodeID string, visited map[int]bool) []activityWalk {
	if w.exhausted {
		return nil
	}
	eligible := eligibleEdges(nodeID, w.edgesByFrom, visited)

	// An end node always terminates, even with an outgoing edge; otherwise a
	// node with no eligible (unvisited) outgoing edge left terminates too —
	// this bounds a loop to being traversed at most once.
	if w.kindByID[nodeID] == "end" || len(eligible) == 0 {
		if !w.spend(1) {
			return nil
		}
		return []activityWalk{{seq: []string{nodeID}, visited: visited}}
	}

	if w.kindByID[nodeID] == "fork" {
		return w.walkFork(nodeID, eligible, visited)
	}

	// Default: decision/switch (and, degenerately, any single-outgoing-edge
	// node) — one walk per eligible outgoing edge.
	return w.branchOverEdges(nodeID, eligible, visited)
}

// eligibleEdges lists nodeID's outgoing edge indices, in declared order,
// EXCLUDING any edge already consumed on this path (loop-once).
func eligibleEdges(nodeID string, edgesByFrom map[string][]int, visited map[int]bool) []int {
	var eligible []int
	for _, idx := range edgesByFrom[nodeID] {
		if !visited[idx] {
			eligible = append(eligible, idx)
		}
	}
	return eligible
}

// branchOverEdges implements the decision/switch (and single-eligible-edge
// default) case: one walk per eligible outgoing edge, each prefixed with
// nodeID. Alternatives are mutually exclusive, so each starts from an
// independent copy of the pre-branch visited set.
//
// A branch that comes back empty is one the budget cut short; its
// already-completed siblings are kept and returned. That is the graceful
// half of exhaustion: a decision blowup returns the alternatives enumerated
// before the budget ran out, in declared order, truncated to what carry
// still allows.
func (w *walker) branchOverEdges(nodeID string, eligible []int, visited map[int]bool) []activityWalk {
	var out []activityWalk
	for _, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		for _, sub := range w.walkFrom(w.edges[idx].To, v) {
			if !w.carry(len(out), 1+len(sub.seq)) {
				return out
			}
			out = append(out, activityWalk{seq: append([]string{nodeID}, sub.seq...), visited: sub.visited})
		}
	}
	return out
}

// walkFork implements the fork's semantics: unlike a decision/switch, a
// fork's outgoing edges do NOT branch into alternatives — every branch is
// taken, so their walks are combined into the SAME path. A branch containing
// internal decision/switch branching contributes multiple alternative walks
// of its own, which are CROSS-PRODUCTED against the other branches'
// alternatives — each branch's own alternative set computed exactly once,
// independent of how many combinations have been accumulated from earlier
// branches so far.
//
// Exhaustion is ALL-OR-NOTHING at the BRANCH level, unlike a decision's
// graceful truncation: a fork path is only a real path of the diagram once
// EVERY branch is folded into it, so a fork whose branch (or whose fold) came
// back with NOTHING contributes no path at all, rather than a fabricated one
// missing a parallel branch (which could make a perfectly connected call
// chain look disconnected to CC-PATH-CONNECTED). This is what makes a
// pure-fork-only blowup degrade to exhausted=true, len=0 rather than a
// partial answer — pre-existing, deliberate, and not "fixed" by this bound.
func (w *walker) walkFork(nodeID string, eligible []int, visited map[int]bool) []activityWalk {
	partials := []activityWalk{{seq: nil, visited: visited}}
	for _, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		branch := w.walkFrom(w.edges[idx].To, v)
		if len(branch) == 0 {
			return nil // the budget cut this branch short — see the doc comment
		}
		partials = w.crossProduct(partials, branch)
		if len(partials) == 0 {
			return nil
		}
	}

	out := make([]activityWalk, 0, len(partials))
	for _, p := range partials {
		if !w.carry(len(out), 1+len(p.seq)) {
			break
		}
		out = append(out, activityWalk{seq: append([]string{nodeID}, p.seq...), visited: p.visited})
	}
	return out
}

// crossProduct combines every in-progress partial with every alternative of
// the next branch, in order (partials outer, branch inner — so branch N's
// alternatives vary fastest), concatenating sequences and UNIONING
// visited-edge sets. Deliberately EXCLUDED from carry — charged via spend
// directly: the fork shape is the memory-heaviest case (each combination
// allocates a fresh visited-edge-set union), so wrapping it in the
// post-exhaustion unbudgeted escape hatch would reopen the memory hole
// charge-by-length closed.
func (w *walker) crossProduct(partials, branch []activityWalk) []activityWalk {
	var next []activityWalk
	for _, p := range partials {
		for _, b := range branch {
			if !w.spend(len(p.seq) + len(b.seq)) {
				return next
			}
			next = append(next, activityWalk{
				seq:     append(append([]string{}, p.seq...), b.seq...),
				visited: unionVisited(p.visited, b.visited),
			})
		}
	}
	return next
}

// cloneVisited copies a visited-edge set so a branch point can extend it
// independently without mutating the state its siblings (or its own caller)
// still hold.
func cloneVisited(v map[int]bool) map[int]bool {
	out := make(map[int]bool, len(v)+1)
	for k := range v {
		out[k] = true
	}
	return out
}

// unionVisited merges two visited-edge sets (see crossProduct).
func unionVisited(a, b map[int]bool) map[int]bool {
	out := make(map[int]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
