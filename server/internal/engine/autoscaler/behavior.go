package autoscaler

// behavior.go holds the hand-written behaviour over the generated contract enums
// (DecisionKind, ReasonCode). Per the schema-first contract rule, the generated
// contract types carry NO methods — behaviour the generator cannot produce lives
// here as FREE FUNCTIONS that take the enum value as a parameter. The enum consts
// (DecisionNoChange, ReasonCPUHigh, …) are the generated contract surface
// (contract.gen.go); these functions reference them by name.

// DecisionKindString returns the canonical name for a decision kind (matches the
// architecture-edge enumeration labels; safe to log).
func DecisionKindString(k DecisionKind) string {
	switch k {
	case DecisionNoChange:
		return "NoChange"
	case DecisionScaleUp:
		return "ScaleUp"
	case DecisionScaleDown:
		return "ScaleDown"
	case DecisionPause:
		return "Pause"
	case DecisionResume:
		return "Resume"
	default:
		return "Unknown"
	}
}

// ReasonCodeString returns a stable name for a reason code (safe to log; no PII).
func ReasonCodeString(c ReasonCode) string {
	switch c {
	case ReasonCPUHigh:
		return "CPUHigh"
	case ReasonCPUSustainedLow:
		return "CPUSustainedLow"
	case ReasonIdle:
		return "Idle"
	case ReasonTrafficResumed:
		return "TrafficResumed"
	case ReasonManualMode:
		return "ManualMode"
	case ReasonPinned:
		return "Pinned"
	case ReasonSLOBurnDown:
		return "SLOBurnDown"
	case ReasonAlreadyAtMin:
		return "AlreadyAtMin"
	case ReasonAlreadyAtMax:
		return "AlreadyAtMax"
	case ReasonSteady:
		return "Steady"
	default:
		return "Unknown"
	}
}
