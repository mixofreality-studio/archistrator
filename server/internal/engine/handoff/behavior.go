package handoff

// behavior.go holds the hand-written behaviour over the generated contract enums
// (WorkerClass, ActivityKind). Per the schema-first contract rule, the generated
// contract types carry NO methods — behaviour the generator cannot produce lives
// here as FREE FUNCTIONS that take the enum value as a parameter. The enum consts
// (AIWorker, ActivityKindConstruction, …) are the generated contract surface
// (contract.gen.go); these functions reference them by name.

// workerClassString returns the canonical worker-class name (the logical class the
// Manager hands to workerAccess). Mirrors the constructionManager consumer mirror.
func workerClassString(c WorkerClass) string {
	switch c {
	case AIWorker:
		return "ai"
	case HumanSeniorWorker:
		return "humanSenior"
	case HumanJuniorWorker:
		return "humanJunior"
	case ArchitectOnly:
		return "architectOnly"
	default:
		return "unknown"
	}
}

// workerClassValid reports whether c is a real casting result the build supports
// (i.e. a registered class, not the zero value). Used to guard the Strategy output.
func workerClassValid(c WorkerClass) bool {
	switch c {
	case AIWorker, HumanSeniorWorker, HumanJuniorWorker, ArchitectOnly:
		return true
	default:
		return false
	}
}
