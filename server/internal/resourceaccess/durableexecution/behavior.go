package durableexecution

// behavior.go carries the FREE-FUNCTION behaviour of the named-scalar / enum value
// types in this component's contract — the established "behavioral value type →
// generated scalar + free functions" pattern (same as constructionpipeline's
// PipelineHandle and artifactAccess's OutputPath). ExecutionHandle is generated as a
// $def named scalar (`type ExecutionHandle string`, contract.gen.go) and
// ExecutionStatus as a generated enum; their methods would not survive codegen, so
// they live here as free functions the impl + callers call. The opaque token the
// impl packs ("{workflowID}|{runID}") IS the string value, so the handle behaviour
// is a thin, parse-free pass over that value.

// ---------------------------------------------------------------------------
// ExecutionHandle behaviour (free functions over the generated named scalar)
// ---------------------------------------------------------------------------

// ExecutionHandleString returns the canonical printable form of a handle (for logs,
// audit events, and persistence). It is the round-trip inverse of
// ParseExecutionHandle. Replaces the former ExecutionHandle.String() method (the
// generated contract type carries no methods).
func ExecutionHandleString(h ExecutionHandle) string { return string(h) }

// ParseExecutionHandle reconstructs an ExecutionHandle from the exact string form a
// prior StartOrSignal/Query returned (the round-trip inverse of
// ExecutionHandleString). A caller that PERSISTS a handle as a plain string (a
// Manager recording an execution reference in a business event) re-materialises the
// value-type handle for a later comparison. It is a pure value reconstruction — no
// validation here.
func ParseExecutionHandle(s string) ExecutionHandle { return ExecutionHandle(s) }

// ExecutionHandleEqual reports value equality of two handles. Replaces the former
// ExecutionHandle.Equal() method.
func ExecutionHandleEqual(a, b ExecutionHandle) bool { return a == b }

// ExecutionHandleIsZero reports whether the handle is the zero value (no execution
// addressed). Replaces the former ExecutionHandle.IsZero() method.
func ExecutionHandleIsZero(h ExecutionHandle) bool { return h == "" }

// ---------------------------------------------------------------------------
// ExecutionStatus behaviour (free function over the generated enum)
// ---------------------------------------------------------------------------

var statusNames = map[ExecutionStatus]string{
	StatusUnknown: "Unknown", StatusRunning: "Running", StatusCompleted: "Completed",
	StatusFailed: "Failed", StatusCancelled: "Cancelled", StatusTimedOut: "TimedOut",
}

// ExecutionStatusString returns the stable name (logs, audit). Replaces the former
// ExecutionStatus.String() method (the generated contract type carries no methods).
func ExecutionStatusString(s ExecutionStatus) string {
	if n, ok := statusNames[s]; ok {
		return n
	}
	return "Unknown"
}
