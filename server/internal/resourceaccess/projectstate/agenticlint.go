package projectstate

import "fmt"

// agenticlint.go — machine validation for AGENTIC SUB-WORKFLOWS in the System model
// (agentic-managers doctrine; founder 2026-07-05). An agentic sub-workflow is a
// dynamic-view step whose orchestration an AGENT decides at run time, sequencing a
// bounded tool PALETTE (Relationship.Palette) — never a fixed internal sequence. Two
// schema fields express it:
//
//   - Component.Implementation (coded|hybrid|agentic) — HOW the component's behavior is
//     realized. A STRATEGY axis orthogonal to the Method layer. Absent ⇒ coded (the
//     safe default; see enumjson.go / modelfields.go).
//   - Relationship.Agentic — the step IS an agentic sub-workflow ("may call any of the
//     palette tools, any order, zero or more times"), rendered dashed + unnumbered.
//
// These lints keep the two fields consistent. They are PURE functions over the System,
// shared by the app-side review-panel findings (manager/systemdesign) and their
// release-gated methodcheck twins — exactly like LintPalettesWithinEdges. The
// palette-⊆-edges rule (DV-PALETTE-NOT-IN-EDGES) stays in toolpalette.go unchanged;
// this file adds only the agentic/implementation consistency rules.

// Rule IDs for the agentic-consistency lints (stable strings; the app-side findings and
// the methodcheck twins reuse them verbatim).
const (
	// RuleAgenticStepOwnerNotAgentic — an agentic step's owning component (edge.From) is
	// not declared implementation agentic|hybrid. ERROR.
	RuleAgenticStepOwnerNotAgentic = "DV-AGENTIC-STEP-OWNER-NOT-AGENTIC"
	// RulePaletteRequiresAgentic — a step carries a non-empty tool palette but is not
	// flagged agentic. ERROR: a palette only means "the agent MAY call these", which is
	// meaningless on a plain solid+numbered call.
	RulePaletteRequiresAgentic = "DV-PALETTE-REQUIRES-AGENTIC"
	// RuleAgenticComponentNoPalette — a component declared agentic|hybrid appears as a
	// step owner in the dynamics but no agentic step anywhere documents a tool palette
	// for it. WARNING (bootstrap-tolerant: the palette may not be authored yet).
	RuleAgenticComponentNoPalette = "COMPONENT-AGENTIC-NO-PALETTE"
)

// AgenticViolation is one agentic-consistency lint failure.
type AgenticViolation struct {
	// RuleID is the stable rule identifier (one of the Rule* constants above).
	RuleID string
	// Warning is true for advisory findings (COMPONENT-AGENTIC-NO-PALETTE) and false for
	// hard ERRORs that must fail a verdict.
	Warning bool
	// UseCaseID is the offending dynamic view (empty for a component-level violation).
	UseCaseID string
	// From / To identify the offending step (empty for a component-level violation).
	From string
	To   string
	// Component is the component the violation concerns (the step owner, or the
	// under-documented agentic component).
	Component string
	// Reason is the human explanation.
	Reason string
}

// LintAgenticWorkflows lints the agentic/implementation consistency of the whole System.
// It returns every violation across all dynamic views plus the component-level warnings.
func LintAgenticWorkflows(sys System) []AgenticViolation {
	impl := implementationByComponent(sys)

	var out []AgenticViolation
	// Per-step rules.
	for _, dv := range sys.DynamicViews {
		for _, e := range dv.Edges {
			if stepIsAgentic(e) {
				owner := impl[canonicalComponent(e.From)]
				if owner != ImplAgentic && owner != ImplHybrid {
					out = append(out, AgenticViolation{
						RuleID: RuleAgenticStepOwnerNotAgentic, UseCaseID: dv.UseCaseID,
						From: e.From, To: e.To, Component: e.From,
						Reason: fmt.Sprintf("its owning component is implementation %q, but an agentic sub-workflow step requires the owner to be implementation agentic or hybrid",
							enumName(implementationNames, owner)),
					})
				}
			}
			if len(e.Palette) > 0 && !stepIsAgentic(e) {
				out = append(out, AgenticViolation{
					RuleID: RulePaletteRequiresAgentic, UseCaseID: dv.UseCaseID,
					From: e.From, To: e.To, Component: e.From,
					Reason: "carries a tool palette but is not flagged agentic; a palette is only meaningful on an agentic sub-workflow step",
				})
			}
		}
	}

	// Component-level warning: an agentic|hybrid component that owns a step somewhere in
	// the dynamics but has no agentic step documenting a palette anywhere.
	ownsStep := map[string]bool{}   // component appears as a dynamic-view step owner
	hasPalette := map[string]bool{} // component owns an agentic step with a non-empty palette
	for _, dv := range sys.DynamicViews {
		for _, e := range dv.Edges {
			from := canonicalComponent(e.From)
			ownsStep[from] = true
			if stepIsAgentic(e) && len(e.Palette) > 0 {
				hasPalette[from] = true
			}
		}
	}
	for _, c := range sys.Components {
		ci := implementationOf(c)
		if ci != ImplAgentic && ci != ImplHybrid {
			continue
		}
		cc := canonicalComponent(c.ID)
		if ownsStep[cc] && !hasPalette[cc] {
			out = append(out, AgenticViolation{
				RuleID: RuleAgenticComponentNoPalette, Warning: true, Component: c.ID,
				Reason: fmt.Sprintf("is implementation %q and appears in the dynamics, but no agentic sub-workflow step documents its tool palette",
					enumName(implementationNames, ci)),
			})
		}
	}
	return out
}

// implementationByComponent maps each component's canonicalized id to its declared
// implementation strategy (absent ⇒ coded).
func implementationByComponent(sys System) map[string]Implementation {
	m := make(map[string]Implementation, len(sys.Components))
	for _, c := range sys.Components {
		m[canonicalComponent(c.ID)] = implementationOf(c)
	}
	return m
}

// implementationOf returns a component's implementation strategy, mapping the absent
// (nil) field to the safe default ImplCoded.
func implementationOf(c Component) Implementation {
	if c.Implementation == nil {
		return ImplCoded
	}
	return *c.Implementation
}

// stepIsAgentic reports whether a dynamic-view step is flagged as an agentic
// sub-workflow (the absent/nil flag means non-agentic).
func stepIsAgentic(r Relationship) bool {
	return r.Agentic != nil && *r.Agentic
}
