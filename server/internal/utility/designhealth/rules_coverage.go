package designhealth

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// rules_coverage.go — the cross-artifact JOIN rules: use-case→dynamic-view,
// volatility→component (encapsulation), volatility→requirement (traces), and
// operational-concept→objective (objectiveLinks + legacy justification). A
// dangling reference is a real defect (SeverityError); a coverage gap is
// advisory (SeverityWarning at most — it flags an orphaned business need
// without blocking the gate).
func coverageFindings(in Input) []methodcheck.Finding {
	var out []methodcheck.Finding
	out = append(out, useCaseDynamicViewFindings(in)...)
	out = append(out, useCaseEssenceFindings(in)...)
	out = append(out, volatilityEncapsulationFindings(in)...)
	out = append(out, componentVolatilityDanglingFindings(in)...)
	out = append(out, componentNoVolatilityFindings(in)...)
	out = append(out, volatilityTraceFindings(in)...)
	out = append(out, objectiveFindings(in)...)
	return out
}

// useCaseDynamicViewFindings — Req 1e: every CORE use case has a dynamic view. A
// core use case with no chain is unarchitected. SeverityError.
func useCaseDynamicViewFindings(in Input) []methodcheck.Finding {
	viewed := map[string]bool{}
	for _, dv := range in.Slots.DynamicViews {
		if dv.UseCaseID != "" {
			viewed[dv.UseCaseID] = true
		}
	}
	var out []methodcheck.Finding
	for i, uc := range in.Slots.CoreUseCases {
		if uc.Classification != "core" || uc.ID == "" {
			continue
		}
		if !viewed[uc.ID] {
			out = append(out, finding(RuleCovUCDynamic, methodcheck.SeverityError, i, "useCase "+uc.ID,
				fmt.Sprintf("core use case %q has no dynamic view — every core use case must be realized by an architecture call chain (Req 1e)", uc.ID)))
		}
	}
	return out
}

// useCaseEssenceFindings — DH-UC-ESSENCE-MISSING: every decision classified core
// must carry a non-empty essenceRationale — the essence-of-the-business argument
// for WHY it is core (ch. 3: core use cases are the essence of the business; the
// field is the symmetric twin of the nonCore rejectionReason). Nil, empty, and
// whitespace-only all count as missing. Skipped while the coreUseCases slot is
// empty/uncommitted — nothing classified yet. SeverityWarning (advisory: the gap
// is an unargued classification, not a broken join).
func useCaseEssenceFindings(in Input) []methodcheck.Finding {
	if len(in.Slots.CoreUseCases) == 0 {
		return nil
	}
	var out []methodcheck.Finding
	for i, uc := range in.Slots.CoreUseCases {
		if uc.Classification != "core" {
			continue
		}
		if uc.EssenceRationale == nil || strings.TrimSpace(*uc.EssenceRationale) == "" {
			out = append(out, finding(RuleUCEssenceMissing, methodcheck.SeverityWarning, i, "useCase "+uc.ID,
				fmt.Sprintf("core use case %q carries no essenceRationale — every core classification must argue WHY the use case is the essence of the business (the symmetric twin of the nonCore rejectionReason)", uc.ID)))
		}
	}
	return out
}

// volatilityEncapsulationFindings — §mission: every volatility is encapsulated by
// a component. TWO regimes:
//
// TYPED JOIN (authoritative): the systemDesign components carry the typed
// encapsulatesVolatilities lists (each entry the EXACT name of a committed
// volatility — the edge lives on the component side because volatilities commit
// before the architecture exists). The moment ANY component carries a non-empty
// list, the typed join governs the WHOLE system: a volatility owned by zero typed
// lists is a real gap (SeverityError, DH-VOL-ENCAP-MISSING). Multiple typed owners
// are LEGITIMATE — the doctrine allows a shared component/facet group — so no
// ambiguity finding is minted for typed joins.
//
// FALLBACK (older states with no typed field anywhere): the interim name-in-blurb
// substring join over the component encapsulates text (the same interim posture as
// the volatility screen). A volatility no component mentions is the same real gap
// (SeverityError); a name that matches several blurbs is the known imprecision of
// the substring join over facet groups (SeverityInfo, DH-VOL-ENCAP-AMBIGUOUS)
// rather than a false "duplicate owner".
func volatilityEncapsulationFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	if in.Slots.typedEncapsulationActive() {
		return typedEncapsulationFindings(in)
	}
	comps := in.Model.System.Components
	var out []methodcheck.Finding
	for i, v := range in.Slots.Volatilities {
		if v.Name == "" {
			continue
		}
		var matches []string
		for _, c := range comps {
			// SystemComponent carries no encapsulates blurb in the projectmodel
			// slice; the interim join reads it from the raw slot (parse.go
			// encapsulatesBlurbs).
			if blurb, ok := in.Slots.encapsulatesBlurbs[c.ID]; ok && containsFold(blurb, v.Name) {
				matches = append(matches, c.ID)
			}
		}
		switch {
		case len(matches) == 0:
			out = append(out, finding(RuleVolEncapMissing, methodcheck.SeverityError, i, "volatility "+v.Name,
				fmt.Sprintf("volatility %q is encapsulated by no component — every volatility must be owned by exactly one component (or a ratified facet group)", v.Name)))
		case len(matches) > 1:
			sort.Strings(matches)
			out = append(out, finding(RuleVolEncapAmbig, methodcheck.SeverityInfo, i, "volatility "+v.Name,
				fmt.Sprintf("volatility %q matches %d component blurbs (%v) under the interim name-in-blurb join — resolve to a single owner or a ratified facet group via the typed encapsulatesVolatilities field", v.Name, len(matches), matches)))
		}
	}
	return out
}

// typedEncapsulationFindings is the authoritative regime of
// volatilityEncapsulationFindings: exact-name ownership over the union of every
// component's typed list. Shared owners are legitimate; only a volatility NO typed
// list names is a gap.
func typedEncapsulationFindings(in Input) []methodcheck.Finding {
	owned := map[string]bool{}
	for _, list := range in.Slots.encapsulatesVolatilities {
		for _, name := range list {
			owned[name] = true
		}
	}
	var out []methodcheck.Finding
	for i, v := range in.Slots.Volatilities {
		if v.Name == "" || owned[v.Name] {
			continue
		}
		out = append(out, finding(RuleVolEncapMissing, methodcheck.SeverityError, i, "volatility "+v.Name,
			fmt.Sprintf("volatility %q appears in no component's encapsulatesVolatilities — every volatility must be owned by at least one component (a shared component/facet group is legitimate)", v.Name)))
	}
	return out
}

// componentVolatilityDanglingFindings — DH-COMP-VOL-DANGLING: every entry of a
// component's typed encapsulatesVolatilities list must EXACTLY match a committed
// volatility name; a miss is a dangling reference (stale residue of a volatility
// rename/removal). Skipped while the volatilities slot is empty/uncommitted —
// nothing to join against yet (the same posture as the other joins). SeverityError.
func componentVolatilityDanglingFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	names := in.Slots.volatilityNames()
	if len(names) == 0 {
		return nil
	}
	var out []methodcheck.Finding
	for i, c := range in.Model.System.Components {
		for _, ref := range in.Slots.encapsulatesVolatilities[c.ID] {
			if !names[ref] {
				out = append(out, finding(RuleCompVolDangling, methodcheck.SeverityError, i, "component "+c.ID,
					fmt.Sprintf("component %q lists %q in encapsulatesVolatilities, which is not a committed volatility name — a dangling reference (stale after a volatility rename/removal)", c.ID, ref)))
			}
		}
	}
	return out
}

// componentNoVolatilityFindings — DH-COMP-NO-VOLATILITY: the REVERSE
// anti-functional-decomposition check (Righting Software ch. 2 — functional
// decomposition is the "siren song"): every Manager/Engine/ResourceAccess component
// must encapsulate at least one volatility, because a code-layer component owning no
// volatility is a functional block wearing a Method costume. Clients, Resources, and
// Utilities are exempt (they encapsulate entry channels, storage technology, and
// cross-cutting concerns rather than product volatility). Ownership is the typed
// list when the typed join is active; under the interim fallback, a blurb mentioning
// ≥1 committed volatility name (containsFold) counts. Skipped entirely while the
// volatilities slot is empty/uncommitted. SeverityWarning.
func componentNoVolatilityFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	if len(in.Slots.volatilityNames()) == 0 {
		return nil
	}
	typed := in.Slots.typedEncapsulationActive()
	var out []methodcheck.Finding
	for i, c := range in.Model.System.Components {
		switch c.Layer {
		case "manager", "engine", "resourceAccess":
			// the layers bound to encapsulate business/product volatility
		default:
			continue
		}
		owns := false
		if typed {
			owns = len(in.Slots.encapsulatesVolatilities[c.ID]) > 0
		} else {
			blurb := in.Slots.encapsulatesBlurbs[c.ID]
			for _, v := range in.Slots.Volatilities {
				if v.Name != "" && containsFold(blurb, v.Name) {
					owns = true
					break
				}
			}
		}
		if !owns {
			out = append(out, finding(RuleCompNoVolatility, methodcheck.SeverityWarning, i, "component "+c.ID,
				fmt.Sprintf("%s component %q encapsulates no volatility — every Manager/Engine/ResourceAccess must encapsulate at least one area of volatility (Righting Software ch. 2: a component owning none is functional decomposition, the siren song)", c.Layer, c.ID)))
		}
	}
	return out
}

// volatilityTraceFindings — every volatility trace resolves to a requirement id.
// A dangling trace is a stale reference left by a requirement renumber/removal.
// SeverityError.
func volatilityTraceFindings(in Input) []methodcheck.Finding {
	reqIDs := in.Slots.requirementIDs()
	if len(reqIDs) == 0 {
		return nil // no requirements committed to join against
	}
	var out []methodcheck.Finding
	for i, v := range in.Slots.Volatilities {
		for _, tr := range v.Traces {
			if !reqIDs[tr] {
				out = append(out, finding(RuleVolTrace, methodcheck.SeverityError, i, "volatility "+v.Name,
					fmt.Sprintf("volatility %q traces to %q, which is not a committed requirement id — a dangling trace (stale reference after a requirement renumber/removal)", v.Name, tr)))
			}
		}
	}
	return out
}

// objectiveFindings — the two objective-reference joins (ch. 5: bidirectional
// objective↔architecture traceability), over BOTH reference sources — the typed
// post-reshape home DeploymentOperationsModel.objectiveLinks (knob name →
// objective numbers) and the legacy decisions[].justifyingObjective older states
// still carry:
//
//	DH-OBJ-RESOLVE  (Error)   every objective reference resolves to a live objective
//	DH-OBJ-COVERAGE (Warning) every objective is referenced by ≥1 link/justification
//
// Coverage is Warning, not Error: with objectiveLinks the reference model has its
// post-reshape home, so an unreferenced objective is a real orphaned-business-need
// signal (ch. 5: "the alternatives are pointless designs and orphaned business
// needs") — but it stays advisory (non-blocking), a subject for the review rather
// than an authoring-gate failure.
func objectiveFindings(in Input) []methodcheck.Finding {
	objNums := in.Slots.objectiveNumbers()
	if len(objNums) == 0 {
		return nil
	}
	var out []methodcheck.Finding

	referenced := map[int]bool{}
	for i, n := range in.Slots.justifyingObjectives {
		referenced[n] = true
		if !objNums[n] {
			out = append(out, finding(RuleObjResolve, methodcheck.SeverityError, i, "justifyingObjective",
				fmt.Sprintf("a decision cites justifyingObjective %d, which is not a declared objective number — a dangling objective reference", n)))
		}
	}
	for i, l := range in.Slots.objectiveLinkRefs {
		referenced[l.Number] = true
		if !objNums[l.Number] {
			out = append(out, finding(RuleObjResolve, methodcheck.SeverityError, i, "objectiveLinks "+l.Knob,
				fmt.Sprintf("operational knob %q links objective %d, which is not a declared objective number — a dangling objective reference", l.Knob, l.Number)))
		}
	}

	nums := make([]int, 0, len(objNums))
	for n := range objNums {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var uncovered []int
	for _, n := range nums {
		if !referenced[n] {
			uncovered = append(uncovered, n)
		}
	}
	if len(uncovered) > 0 {
		out = append(out, finding(RuleObjCoverage, methodcheck.SeverityWarning, 0, "objectives",
			fmt.Sprintf("objective(s) %v are referenced by no objectiveLinks entry or justifyingObjective — an unreferenced objective is an orphaned business need (ch. 5); link every objective to at least one operational choice (advisory, non-blocking)", uncovered)))
	}
	return out
}
