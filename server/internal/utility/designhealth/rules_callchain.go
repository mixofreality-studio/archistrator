package designhealth

import (
	"fmt"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// rules_callchain.go is the app-side LIVE-TIER mirror of the platform's CC-*
// CALL-CHAIN CORRESPONDENCE family (framework-go/methodcheck/rules_callchain.go
// + activitypaths.go, 2026-07-30 callchain-realization): the machine check that
// every use case's step-keyed DynamicView realization CORRESPONDS to that use
// case's activity diagram. This is the tier the webApp's Design Health surface
// actually renders (render-on-read over the committed project.json — see the
// package doc comment in designhealth.go), so the ten rules below are re-derived
// over this package's own tolerant slices (dynamicView/callStep/coreUseCase) —
// the same posture as the rest of this package relative to framework-go/
// methodcheck (structural mirror, no shared types).
//
// The ten rules (nine correspondence checks plus the dangling-join-key guard):
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
//	CC-TRIGGER-EVENT   the use-case trigger and the diagram's entry nodes agree
//	CC-PATH-CONNECTED  every activity-diagram PATH is realized as a connected chain
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
// ccContext.stepLoc/ucLoc below. This is the OPPOSITE priority of dvLabel
// (useCaseId-first, used by the pre-existing DH-CHAIN-* rules in
// rules_chains.go) — a view's title/useCaseId-first identifier was flagged
// unstable; the key is the stable identity the app's join relies on.
//
// ACTORS: an endpoint id is resolved against the component index UNION the
// OWNING use case's Actors. Actors are per-use-case, so the same id may name an
// actor in one use case and nothing in another — resolution is always relative
// to the view's use case.
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
	for i, dv := range in.Slots.DynamicViews {
		uc, ok := ucByID[dv.UseCaseID]
		if !ok {
			// CC-VIEW-USECASE. A view whose UseCaseID resolves to NOTHING silently
			// disables every other CC-* rule for it, and no other rule notices —
			// report the dangling join key rather than no-op'ing the whole family.
			out = append(out, ccFinding(RuleCCViewUseCase, i, "dynamicView "+dv.Key,
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
// already carries), ported verbatim including its two review-driven fixes:
//
//  1. the FULL enumeration is computed first — per-branch alternative sets are
//     cross-producted in declared order at a fork (fork-without-join legal,
//     each branch's own alternatives computed exactly once) — and the
//     maxActivityPaths cap is applied ONCE, as a final prefix-truncation of
//     the complete, uncapped result. Nothing inside the recursion is
//     budget-aware, so an asymmetric branch shape can neither over-charge nor
//     silently drop a legitimate combination.
//  2. entries are every "start" node PLUS every UML event node (timeEvent/
//     acceptEvent), wherever they sit; loops (back-edges) are traversed AT
//     MOST ONCE per path via a per-path visited-EDGE set.
// ---------------------------------------------------------------------------

// maxActivityPaths caps the total number of enumerated paths per diagram
// (across ALL entries).
const maxActivityPaths = 512

// pathEntry describes one enumeration root of an activity diagram.
type pathEntry struct {
	NodeID string
	Kind   string // "start", "timeEvent", "acceptEvent"
}

// activityWalk is one in-progress (or completed) DFS walk: the node-id
// sequence produced so far, plus the set of edge indices already consumed
// along it.
type activityWalk struct {
	seq     []string
	visited map[int]bool
}

// activityPaths enumerates every entry→end node-id path of a.
func activityPaths(a activityDiagram) []struct {
	Entry pathEntry
	Nodes []string // node ids in walk order, Entry.NodeID first
} {
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

	var entries []pathEntry
	for _, n := range a.Nodes {
		switch n.Kind {
		case "start", "timeEvent", "acceptEvent":
			entries = append(entries, pathEntry{NodeID: n.ID, Kind: n.Kind})
		}
	}

	var out []struct {
		Entry pathEntry
		Nodes []string
	}
	for _, entry := range entries {
		for _, w := range walkActivity(entry.NodeID, a.Edges, kindByID, edgesByFrom, map[int]bool{}) {
			out = append(out, struct {
				Entry pathEntry
				Nodes []string
			}{Entry: entry, Nodes: w.seq})
		}
	}
	if len(out) > maxActivityPaths {
		out = out[:maxActivityPaths]
	}
	return out
}

// walkActivity performs the recursive DFS described above, returning every
// completed walk starting at nodeID given the edges already visited on the
// path so far. It is NOT budget-aware — activityPaths applies maxActivityPaths
// exactly once, as a final truncation of the complete result.
func walkActivity(nodeID string, edges []activityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool) []activityWalk {
	eligible := eligibleEdges(nodeID, edgesByFrom, visited)

	// An end node always terminates, even with an outgoing edge; otherwise a
	// node with no eligible (unvisited) outgoing edge left terminates too —
	// this bounds a loop to being traversed at most once.
	if kindByID[nodeID] == "end" || len(eligible) == 0 {
		return []activityWalk{{seq: []string{nodeID}, visited: visited}}
	}

	if kindByID[nodeID] == "fork" {
		return walkFork(nodeID, eligible, edges, kindByID, edgesByFrom, visited)
	}

	// Default: decision/switch (and, degenerately, any single-outgoing-edge
	// node) — one walk per eligible outgoing edge.
	return branchOverEdges(nodeID, eligible, edges, kindByID, edgesByFrom, visited)
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
func branchOverEdges(nodeID string, eligible []int, edges []activityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool) []activityWalk {
	var out []activityWalk
	for _, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		for _, sub := range walkActivity(edges[idx].To, edges, kindByID, edgesByFrom, v) {
			out = append(out, activityWalk{seq: append([]string{nodeID}, sub.seq...), visited: sub.visited})
		}
	}
	return out
}

// walkFork implements the fork's semantics: unlike a decision/switch, a fork's
// outgoing edges do NOT branch into alternatives — every branch is taken, so
// their walks are combined into the SAME path. A branch containing internal
// decision/switch branching contributes multiple alternative walks of its
// own, which are CROSS-PRODUCTED against the other branches' alternatives —
// each branch's own alternative set computed exactly once, independent of how
// many combinations have been accumulated from earlier branches so far.
func walkFork(nodeID string, eligible []int, edges []activityEdge, kindByID map[string]string, edgesByFrom map[string][]int, visited map[int]bool) []activityWalk {
	branches := make([][]activityWalk, len(eligible))
	for i, idx := range eligible {
		v := cloneVisited(visited)
		v[idx] = true
		branches[i] = walkActivity(edges[idx].To, edges, kindByID, edgesByFrom, v)
	}

	partials := []activityWalk{{seq: nil, visited: visited}}
	for _, branch := range branches {
		partials = crossProduct(partials, branch)
	}

	out := make([]activityWalk, 0, len(partials))
	for _, p := range partials {
		out = append(out, activityWalk{seq: append([]string{nodeID}, p.seq...), visited: p.visited})
	}
	return out
}

// crossProduct combines every in-progress partial with every alternative of
// the next branch, in order, concatenating sequences and UNIONING
// visited-edge sets.
func crossProduct(partials, branch []activityWalk) []activityWalk {
	var next []activityWalk
	for _, p := range partials {
		for _, b := range branch {
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
