package projectstate

// construction.go holds the shared Phase-3 construction-transition VALUE types. The
// legacy Postgres-only construction-transition store (the pgTransitionAccess interface
// + its *store Record* methods) was RETIRED with the Postgres projectstate store; the
// live cred-threaded construction-transition surface is ConstructionTransitionAccess
// (construction_transition_port.go), satisfied by the git store (gitconstruction.go).

// ActivityOutcome is the closed terminal outcome recorded for a construction
// activity's binary exit (constructionManager.md §2.1 / §6.3 step 8).
type ActivityOutcome int

const (
	// ActivityOutcomeUnknown is the zero value.
	ActivityOutcomeUnknown ActivityOutcome = iota
	// ActivityOutcomeCompleted is a normal, reviewed binary exit.
	ActivityOutcomeCompleted
	// ActivityOutcomeSkipped is an operator-skip exit (overrideActivity Skip).
	ActivityOutcomeSkipped
	// ActivityOutcomeTakenOver is an exit after an operator/automatic takeover.
	ActivityOutcomeTakenOver
)

// String returns the canonical name for the outcome.
func (o ActivityOutcome) String() string {
	switch o {
	case ActivityOutcomeCompleted:
		return "Completed"
	case ActivityOutcomeSkipped:
		return "Skipped"
	case ActivityOutcomeTakenOver:
		return "TakenOver"
	default:
		return "Unknown"
	}
}
