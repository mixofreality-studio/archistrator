package designhealth

import (
	"fmt"

	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// rules_cardinality.go — App-C component-count guidelines over the systemDesign
// inventory. These are GUIDELINES (SeverityWarning) and an informational count
// (SeverityInfo), never an authoring-gate block: the sweet spots are advisory and
// a project can legitimately sit at the edge. The bounds are deliberately generous
// (the App-C "~10" is treated as an over-bound alarm at >12) so a healthy design
// stays quiet and only a real inventory blow-out surfaces.
func cardinalityFindings(in Input) []methodcheck.Finding {
	if in.Model == nil || in.Model.System == nil {
		return nil
	}
	counts := map[string]int{}
	for _, c := range in.Model.System.Components {
		counts[c.Kind]++
	}

	var out []methodcheck.Finding
	// §2a: at most ~5 Managers — more than 5 usually means a missing subsystem cut.
	if n := counts["manager"]; n > 5 {
		out = append(out, finding(RuleCardManagers, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d Manager components — The Method keeps Managers few (≈5); more usually signals a use-case explosion or a missing subsystem partition", n)))
	}
	// ch. 4 smallest-set band starts at 2 Managers: a single-Manager system cannot be
	// validated against the other use cases (there is nothing to swap or compare).
	// Skipped entirely on a zero-component system — mid-draft, nothing to judge yet.
	if len(in.Model.System.Components) > 0 && counts["manager"] == 1 {
		out = append(out, finding(RuleCardManagersMin, methodcheck.SeverityInfo, 1, "components",
			"1 Manager component — ch. 4's smallest-set band starts at 2 Managers; a single-Manager system is unvalidatable (no sibling use-case family to validate the decomposition against). Check for a missed family of use cases"))
	}
	// ch. 4 smallest-set band: 2–3 Engines.
	if n := counts["engine"]; n > 3 {
		out = append(out, finding(RuleCardEngines, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d Engine components — past the ch. 4 smallest-set band of 2–3 Engines; check for functional decomposition wearing an Engine costume, or fold activities that are not genuinely volatile business rules", n)))
	}
	// §2f: ResourceAccess ≈10 — flag a clear over-bound.
	if n := counts["resourceAccess"]; n > 12 {
		out = append(out, finding(RuleCardRA, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d ResourceAccess components — well past the ≈10 guideline; consider folding facet-duplicate access components", n)))
	}
	// §2g: Resources ≈10 — flag a clear over-bound.
	if n := counts["resource"]; n > 12 {
		out = append(out, finding(RuleCardResource, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d Resource components — well past the ≈10 guideline", n)))
	}
	// ch. 4 bounds ResourceAccess + Resources TOGETHER at 3–8 (the individual >12
	// alarms above stay as the blow-out backstop).
	if n := counts["resourceAccess"] + counts["resource"]; n > 8 {
		out = append(out, finding(RuleCardRAResources, methodcheck.SeverityWarning, n, "components",
			fmt.Sprintf("%d combined ResourceAccess + Resource components — past the ch. 4 smallest-set band of 3–8 for the two storage layers together; consider folding facet-duplicate access components or consolidating storage", n)))
	}
	// ch. 4: "a half-dozen Utilities" — informational once past 6.
	if n := counts["utility"]; n > 6 {
		out = append(out, finding(RuleCardUtilities, methodcheck.SeverityInfo, n, "components",
			fmt.Sprintf("%d Utility components — past ch. 4's half-dozen; informational, but check whether any Utility is really a mislabeled business component or duplicated cross-cutting concern", n)))
	}
	// §2: core use case count in [2,6].
	core := 0
	for _, uc := range in.Slots.CoreUseCases {
		if uc.Classification == "core" {
			core++
		}
	}
	if core > 0 && (core < 2 || core > 6) {
		out = append(out, finding(RuleCardCoreUC, methodcheck.SeverityWarning, core, "coreUseCases",
			fmt.Sprintf("%d core use cases — The Method targets 2–6 core use cases as the architecture's load-bearing set", core)))
	}
	// §2h: volatility count is informational — waiver-backed, never blocking.
	if n := len(in.Slots.Volatilities); n > 0 {
		out = append(out, finding(RuleCardVolatility, methodcheck.SeverityInfo, n, "volatilities",
			fmt.Sprintf("%d volatilities identified — informational; the count is waiver-backed, not bounded", n)))
	}
	return out
}

// componentsByKind is a small shared helper for the graph rules.
func componentsByKind(sys *projectmodel.System, kind string) []projectmodel.SystemComponent {
	var out []projectmodel.SystemComponent
	for _, c := range sys.Components {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}
