package projectstate

import "strings"

// profileSlug is the .claude/commands filename stem for an activity profile.
// For testing it encodes the variant (testing-plan/harness/perf/systemtest/qa);
// all other types map 1:1 to their wire name.
func profileSlug(t ActivityType, v TestingVariant) string {
	switch t {
	case ActivityTypeFrontend:
		return "frontend"
	case ActivityTypeDeployment:
		return "deployment"
	case ActivityTypeDocumentation:
		return "documentation"
	case ActivityTypeTesting:
		switch v {
		case TestVariantHarness:
			return "testing-harness"
		case TestVariantPerf:
			return "testing-perf"
		case TestVariantSystemTest:
			return "testing-systemtest"
		case TestVariantQAProcess:
			return "testing-qa"
		default: // TestVariantPlan
			return "testing-plan"
		}
	default: // ActivityTypeService
		return "service"
	}
}

// kebabPhase renders a canonical phase id as a command slug segment
// (detailed_design -> detailed-design).
func kebabPhase(p ActivityMethodPhase) string {
	return strings.ReplaceAll(string(p), "_", "-")
}

// CommandFor returns the .claude slash-command name for a (type, variant, phase)
// cell: "<profileSlug>-<phaseSlug>". It is total over exactly the phases
// ProfileFor(t, v) emits, and matches a .claude/commands/<name>.md file.
func CommandFor(t ActivityType, v TestingVariant, p ActivityMethodPhase) string {
	return profileSlug(t, v) + "-" + kebabPhase(p)
}
