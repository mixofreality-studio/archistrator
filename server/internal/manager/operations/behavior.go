package operations

// This file holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract enum (e.g. the canonical
// name lookup that used to be a String() method) lives here as a free function. This
// is the OutputPath / PipelineHandle precedent (a contract-value-type method becomes
// a free function so the generated scalar/enum carries no behavior).

// desiredStateReasonName returns the canonical wire name for a desired-state reason.
// Kept as a FREE FUNCTION (not a DesiredStateReason method) so the generated enum is
// pure data.
func desiredStateReasonName(r DesiredStateReason) string {
	switch r {
	case ReasonDeployAfterConstruction:
		return "deployAfterConstruction"
	case ReasonOperator:
		return "operator"
	case ReasonAutoscale:
		return "autoscale"
	case ReasonDelinquency:
		return "delinquency"
	default:
		return "unknown"
	}
}

// autoscaleActionName returns the canonical name for an autoscale action. Kept as a
// FREE FUNCTION (not an AutoscaleAction method) so the generated enum is pure data.
func autoscaleActionName(a AutoscaleAction) string {
	switch a {
	case AutoscaleNoChange:
		return "NoChange"
	case AutoscaleScaleUp:
		return "ScaleUp"
	case AutoscaleScaleDown:
		return "ScaleDown"
	case AutoscalePause:
		return "Pause"
	case AutoscaleResume:
		return "Resume"
	default:
		return "Unknown"
	}
}
