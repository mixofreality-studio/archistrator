package durableexecution

// This file owns the ExecutionKind → (infrastructure workflow-type name, task
// queue) mapping (durableExecutionAccess.md §6: "The mapping of ExecutionKind →
// Temporal workflow-type-name + task-queue is owned INSIDE this package,
// populated from the same registry the Worker bootstrap uses"). It is the seam
// that keeps the contract surface infrastructure-opaque: callers name a LOGICAL
// ExecutionKind; this registry resolves it to the concrete workflow type name and
// the per-Manager task queue that the embedded Worker bootstrap registers under.
//
// The registry is a plain in-memory lookup with NO Temporal lexeme on it (a
// "workflow type name" here is just the string the runtime addresses; nothing in
// this file imports Temporal). The concrete Temporal bridge (temporal.go) reads
// it; the aiarch-server Worker bootstrap that registers the matching workflow
// functions is expected to be seeded from the SAME table so the names line up.

// kindBinding is the resolved infrastructure address for one ExecutionKind: the
// workflow type name the runtime dispatches and the task queue the owning
// Manager's Worker listens on (one task queue per Manager,
// operational-concepts.md §2/§4).
type kindBinding struct {
	// workflowType is the runtime's workflow-type name for this kind. A plain
	// string addressed by the control plane — not a Temporal type.
	workflowType string
	// taskQueue is the owning Manager's task queue (one per Manager).
	taskQueue string
}

// kindRegistry resolves a logical ExecutionKind to its infrastructure binding.
// Unknown kinds resolve to (binding{}, false) so callers can surface
// fwra.ContractMisuse (the logical ErrUnknownKind) without ever consulting the
// runtime — a pre-condition check the contract owns (durableExecutionAccess.md
// §2.1 pre-conditions).
type kindRegistry struct {
	bindings map[ExecutionKind]kindBinding
}

// newKindRegistry builds a registry from a kind → (workflowType, taskQueue)
// table. The aiarch-server bootstrap supplies the same table to both this RA and
// the Worker registration so the names are guaranteed consistent.
func newKindRegistry(table map[ExecutionKind]kindBinding) *kindRegistry {
	bindings := make(map[ExecutionKind]kindBinding, len(table))
	for k, b := range table {
		bindings[k] = b
	}
	return &kindRegistry{bindings: bindings}
}

// resolve returns the binding for kind and whether it is registered.
func (r *kindRegistry) resolve(kind ExecutionKind) (kindBinding, bool) {
	b, ok := r.bindings[kind]
	return b, ok
}
