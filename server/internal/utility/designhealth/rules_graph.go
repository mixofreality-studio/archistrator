package designhealth

import (
	"fmt"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// rules_graph.go — the closed-architecture directives (§4c) and guidelines
// (§5, §6c/§6d) over the systemDesign relationship edges. Directives that a legal
// Method design can never violate are SeverityError (they block the authoring
// gate); the reachability/coverage guidelines are SeverityWarning.
//
// Utilities are cross-cutting: an edge touching a utility is exempt from the
// DIRECTION rules (up/sideways/entry/engine-IO) — only the utility-reachability
// guideline concerns them.
func graphFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	sys := in.Model.System
	idx := in.componentIndex()

	var out []methodcheck.Finding
	for i, r := range sys.Relationships {
		from, okF := idx[r.From]
		to, okT := idx[r.To]
		if !okF || !okT {
			// projectmodel.Load already rejects unresolved endpoints before we run;
			// this guard is belt-and-suspenders for a directly-constructed Input.
			continue
		}
		fk, tk := from.Kind, to.Kind
		section := r.From + "→" + r.To

		// §4c.i — no up-calls. An edge whose target sits at a HIGHER layer than its
		// source inverts the closed architecture. Utilities exempt.
		if isDirectional(fk, tk) && kindRank(tk) < kindRank(fk) {
			out = append(out, finding(RuleGraphUpcall, methodcheck.SeverityError, i, section,
				fmt.Sprintf("up-call: %s (%s) calls %s (%s) — a higher layer; the closed architecture only calls DOWN the layers", r.From, fk, r.To, tk)))
		}
		// §4c.ii / §5d — sideways calls only when queued (M→M is the live case).
		if isDirectional(fk, tk) && kindRank(tk) == kindRank(fk) && r.Mode != "queued" {
			out = append(out, finding(RuleGraphSidewaysSync, methodcheck.SeverityError, i, section,
				fmt.Sprintf("sideways sync call: %s→%s are the same layer (%s) but the call is %q — same-layer calls (e.g. Manager→Manager) are permitted only when queued", r.From, r.To, fk, r.Mode)))
		}
		// §4c.iii — a Client may enter only at a Manager (or a cross-cutting utility).
		if fk == "client" && tk != "manager" && tk != "utility" {
			out = append(out, finding(RuleGraphClientEntry, methodcheck.SeverityError, i, section,
				fmt.Sprintf("client %s calls %s (%s) — a Client may enter the system only at a Manager, never at %s directly", r.From, r.To, tk, tk)))
		}
		// §6c / §6d — Engines and ResourceAccess receive no QUEUED calls (they are
		// synchronous, request/response building blocks).
		if r.Mode == "queued" && (tk == "engine" || tk == "resourceAccess") {
			out = append(out, finding(RuleGraphQueuedTarget, methodcheck.SeverityError, i, section,
				fmt.Sprintf("queued call into a %s (%s): Engines and ResourceAccess are synchronous — only Managers receive queued calls", tk, r.To)))
		}
		// §5b — Engines do no IO: no Engine→ResourceAccess / Engine→Resource edge
		// (Managers own all IO orchestration). SeverityWarning: some Method dialects
		// permit Engine→ResourceAccess, so this is a guideline for the archistrator
		// "Managers own IO" posture rather than a hard directive.
		if fk == "engine" && (tk == "resourceAccess" || tk == "resource") {
			out = append(out, finding(RuleGraphEngineIO, methodcheck.SeverityWarning, i, section,
				fmt.Sprintf("engine %s calls %s (%s): Engines are pure computation in this architecture — route IO through a Manager", r.From, r.To, tk)))
		}
	}

	out = append(out, utilityReachabilityFindings(in)...)
	out = append(out, managerOrchestrationFindings(in)...)
	return out
}

// utilityReachabilityFindings — §5a: every declared utility must be reachable
// (have ≥1 inbound edge); an unreferenced utility is dead architecture.
func utilityReachabilityFindings(in Input) []methodcheck.Finding {
	sys := in.Model.System
	inbound := map[string]bool{}
	for _, r := range sys.Relationships {
		inbound[r.To] = true
	}
	var out []methodcheck.Finding
	for i, u := range componentsByKind(sys, "utility") {
		if !inbound[u.ID] {
			out = append(out, finding(RuleGraphUtilReach, methodcheck.SeverityWarning, i, "utility "+u.ID,
				fmt.Sprintf("utility %q has no inbound edge — it is unreachable/dead in the architecture, or the caller relationships are missing", u.ID)))
		}
	}
	return out
}

// managerOrchestrationFindings — §5: a Manager must ORCHESTRATE something: it needs
// at least one outbound edge to an Engine OR a ResourceAccess. A Manager with an
// Engine (computation orchestration) or with ResourceAccess (IO orchestration) — or
// both — is a legitimate Manager; The Method does NOT require every Manager to call
// an Engine (a Manager sequencing several ResourceAccess components is orchestrating
// IO, which is exactly its job). The real defect this catches is a Manager that
// orchestrates NOTHING — no Engine and no ResourceAccess edge — i.e. an empty
// component (missing all its downstream edges, or a mislabeled leaf). SeverityWarning.
func managerOrchestrationFindings(in Input) []methodcheck.Finding {
	sys := in.Model.System
	idx := in.componentIndex()
	orchestrates := map[string]bool{}
	for _, r := range sys.Relationships {
		from, okF := idx[r.From]
		to, okT := idx[r.To]
		if okF && okT && from.Kind == "manager" && (to.Kind == "engine" || to.Kind == "resourceAccess") {
			orchestrates[from.ID] = true
		}
	}
	var out []methodcheck.Finding
	for i, m := range componentsByKind(sys, "manager") {
		if !orchestrates[m.ID] {
			out = append(out, finding(RuleGraphMgrEmpty, methodcheck.SeverityWarning, i, "manager "+m.ID,
				fmt.Sprintf("Manager %q has no outbound edge to an Engine or ResourceAccess — a Manager must orchestrate something; one with no downstream edges is empty (missing its edges, or a mislabeled component)", m.ID)))
		}
	}
	return out
}
