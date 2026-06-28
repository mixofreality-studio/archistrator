package project

// behavior.go holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract enum (the canonical-name
// lookups that used to be String() methods on the projectstate enums) lives here as
// a free function. The webClient renders the per-activity construction head-state via
// these (constructionrows.go), so the strings are the canonical wire names the SPA
// consumes.

// ActivityTypeName returns the canonical wire name for an activity type.
func ActivityTypeName(t ActivityType) string {
	switch t {
	case ActivityTypeFrontend:
		return "frontend"
	case ActivityTypeTesting:
		return "testing"
	case ActivityTypeDeployment:
		return "deployment"
	case ActivityTypeDocumentation:
		return "documentation"
	default:
		return "service"
	}
}

// ActivityBuildStatusName returns the canonical wire name for a build status
// (matches the ux-mock BuildStatus union).
func ActivityBuildStatusName(s ActivityBuildStatus) string {
	switch s {
	case BuildInReview:
		return "in-review"
	case BuildIntegrated:
		return "integrated"
	case BuildFailed:
		return "failed"
	default:
		return "in-construction"
	}
}

// ActivityConstructionPhaseName returns the canonical wire name for the coarse
// construction phase.
func ActivityConstructionPhaseName(p ActivityConstructionPhase) string {
	switch p {
	case ActivityConstructionRunning:
		return "running"
	case ActivityConstructionDone:
		return "done"
	case ActivityConstructionFailed:
		return "failed"
	default:
		return "notStarted"
	}
}

// FailureReasonName returns the canonical wire name for a terminal-failure cause.
func FailureReasonName(r FailureReason) string {
	switch r {
	case PipelineFailed:
		return "pipelineFailed"
	case PipelineCancelled:
		return "pipelineCancelled"
	case PipelineTimedOut:
		return "pipelineTimedOut"
	case VarianceExhausted:
		return "varianceExhausted"
	case EscalationTimedOut:
		return "escalationTimedOut"
	default:
		return "unknown"
	}
}
