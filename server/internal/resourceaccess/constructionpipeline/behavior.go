package constructionpipeline

// behavior.go carries the FREE-FUNCTION behaviour of the named-scalar value types
// in this component's contract — the established "behavioral value type →
// generated scalar + free functions" pattern (same as artifactAccess's OutputPath
// and the handoff enums). PipelineHandle is generated as a $def named scalar
// (`type PipelineHandle string`, contract.gen.go); its methods would not survive
// codegen, so they live here as free functions the impl + callers call. The
// opaque token the impl packs ("run/<id>" / "run/<id>@owner/repo/wf") IS the
// string value, so the behaviour is a thin, parse-free pass over that value.

// PipelineHandleString returns the canonical printable form of a handle (for logs,
// audit events, and persistence). It is the round-trip inverse of
// ParsePipelineHandle.
func PipelineHandleString(h PipelineHandle) string { return string(h) }

// ParsePipelineHandle reconstructs a PipelineHandle from the exact string form a
// prior Submit/Observe returned (the round-trip inverse of PipelineHandleString).
// A caller that PERSISTS a handle as a plain string (a Manager recording a pipeline
// reference in head-state, or a Temporal Manager serialising the handle across an
// Activity boundary) re-materialises the value-type handle for a later
// Observe/Cancel. It is a pure value reconstruction — no validation here; an
// unaddressable / malformed handle is rejected by the verb that consumes it
// (Observe/Cancel map a bad handle to ContractMisuse/NotFound). Additive: it adds
// no new business op and leaves the three-verb port surface unchanged. (Replaces
// the former HandleFromString method.)
func ParsePipelineHandle(s string) PipelineHandle { return PipelineHandle(s) }

// PipelineHandleEqual reports value equality of two handles.
func PipelineHandleEqual(a, b PipelineHandle) bool { return a == b }

// PipelineHandleIsZero reports whether the handle is the zero value (no pipeline
// addressed).
func PipelineHandleIsZero(h PipelineHandle) bool { return h == "" }

// ---------------------------------------------------------------------------
// PipelinePhase behaviour (free functions over the generated enum)
// ---------------------------------------------------------------------------

var phaseNames = map[PipelinePhase]string{
	PhasePending: "Pending", PhaseRunning: "Running", PhaseSucceeded: "Succeeded",
	PhaseFailed: "Failed", PhaseCancelled: "Cancelled",
}

// PipelinePhaseString returns the stable name (logs, audit). Replaces the former
// PipelinePhase.String() method (the generated contract type carries no methods).
func PipelinePhaseString(p PipelinePhase) string {
	if n, ok := phaseNames[p]; ok {
		return n
	}
	return "Pending"
}

// PipelinePhaseIsTerminal reports whether the phase is one a running pipeline can no
// longer leave (Succeeded / Failed / Cancelled). Cancelling or re-observing a
// terminal pipeline is stable. Replaces the former PipelinePhase.IsTerminal() method.
func PipelinePhaseIsTerminal(p PipelinePhase) bool {
	switch p {
	case PhaseSucceeded, PhaseFailed, PhaseCancelled:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// StepOutcome behaviour (free function over the generated enum)
// ---------------------------------------------------------------------------

var stepOutcomeNames = map[StepOutcome]string{
	StepPending: "Pending", StepRunning: "Running", StepSucceeded: "Succeeded",
	StepFailed: "Failed", StepSkipped: "Skipped",
}

// StepOutcomeString returns the stable name (logs, audit). Replaces the former
// StepOutcome.String() method.
func StepOutcomeString(o StepOutcome) string {
	if n, ok := stepOutcomeNames[o]; ok {
		return n
	}
	return "Pending"
}

// ---------------------------------------------------------------------------
// RepoTarget behaviour (free function over the generated struct)
// ---------------------------------------------------------------------------

// RepoTargetIsZero reports whether the target addresses no repo (the
// fall-back-to-default case). Replaces the former RepoTarget.IsZero() method.
func RepoTargetIsZero(t RepoTarget) bool { return t.Owner == "" && t.Name == "" }
