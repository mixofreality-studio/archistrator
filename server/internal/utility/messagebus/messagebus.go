// Package messagebus is the messageBus component of the aiarch server's UTILITY
// layer — the messaging surface BETWEEN MANAGERS. Two verbs, and only two:
// deliverSignal delivers a queued Manager→Manager message; registerSchedule
// registers the recurring timers that fire through the SchedulerClient channel.
//
// RESTRICTED CLIENTELE (ch. 5, founder ruling R-F 2026-08-01). Unlike Security /
// Logging / Diagnostics — ambient utilities any layer may call — this one is
// restricted: ONLY Manager-layer packages may import it. Engines are pure
// computation and do no messaging I/O; each ResourceAccess fronts exactly one
// resource; Clients enter the system at a Manager. The rule is executable, not
// aspirational: internal/arch_test.go's TestMessageBusManagersOnly fails the build
// for any non-Manager importer, and the slot-5 edge set mirrors it (every inbound
// message-bus relationship originates at a Manager).
//
// WHAT IT ENCAPSULATES: the Workflow Execution Substrate volatility (B-14) —
// Temporal today, another durable-execution substrate later. A messaging utility
// encapsulating the TRANSPORT choice is ch. 3's canonical utility case (Message
// Bus, Security, Logging all encapsulate technical volatility); the cappuccino
// test bars a utility from owning BUSINESS volatility, which this one does not.
//
// THE SUBSTRATE'S EXECUTION ROLE IS INVISIBLE (operational-concepts decision
// "durable primitives are execution substrate, not architecture edges"). A
// Manager's workflow-internal durable primitives — startTimer, awaitSignal,
// executeChild, continueAsNew, query-self — are deterministic Workflow SDK calls
// made from INSIDE its own workflow body. They are NOT ops here: routing them
// through this port would force it to import the Workflow SDK and run inside the
// replay context, the exact coupling the layering directive forbids. They live in
// the Manager packages and draw no architecture edges. Only the two
// cross-component verbs surface, and a QUEUED Manager→Manager relationship — not
// a call into this package — remains the architectural representation of every
// delivery ("verb calls draw; deliveries don't").
//
// THE LOAD-BEARING LAYER RULE ([[the-method-layers]] "Temporal mapping"): even
// though this utility fronts Temporal ITSELF, its PUBLIC surface imports NO
// Temporal and carries ZERO Temporal lexemes (Workflow, Activity, Signal-type,
// WorkflowID, RunID, TaskQueue, Worker, Namespace). Both verbs are ordinary RPC
// against the runtime's control plane, performed from OUTSIDE any target
// execution's deterministic context.
//
// THE CONCRETE TEMPORAL IMPLEMENTATION IS IN THIS FILE — the unexported
// temporalMessageBus below, reached through the GENERATED public constructor
// NewTemporalMessageBus (contract.gen.go, option-1 delegated DI). This package is
// the sole place in the corpus outside the Manager layer that touches the Temporal
// SDK, which is why internal/arch_test.go lists it in Spec.TemporalExemptPackages;
// no Temporal type ever leaks back across the port.
//
// INFRASTRUCTURE MAPPING (caller-opaque; for the senior reviewer and future
// maintainers):
//
//   - DeliverSignal → client.SignalWorkflow(ctx, id, "", signal, payload).
//     At-least-once to the channel; "not found" → fwra.NotFound.
//   - RegisterSchedule → ScheduleClient().Create(...); on
//     ErrScheduleAlreadyRunning, GetHandle(...).Update(...) to converge
//     (idempotent on ScheduleID, last-writer-wins on a changed spec).
//
// Idempotency is carried by CALLER-SUPPLIED ids — the target ExecutionID for a
// delivery, the ScheduleID for a registration — never read from ambient Temporal
// context; the same move artifactAccess/projectStateAccess make with their
// caller-supplied idempotencyKey. The runtime is natively idempotent on those ids.
//
// PAYLOAD OPACITY: ExecutionPayload.Bytes are passed to the runtime as a raw
// []byte argument. Temporal's default data converter stores a []byte verbatim
// (ByteSlicePayloadConverter), so the bytes round-trip uninterpreted — this
// utility is a transport, not a serialiser. The receiving workflow's signal
// handler owns the payload semantics.
//
// AUTH: the runtime connection is authenticated where the client.Client is
// constructed (mTLS / namespace creds acquired by the aiarch-server pod's
// identity), never threaded through the port. Connection-level failures surface
// as fwra.Infrastructure / fwra.Transient.
package messagebus

import (
	"errors"
	"maps"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// temporalMessageBus is the concrete Temporal-client-backed implementation of the
// MessageBus port. It is UNEXPORTED — the package's only public surface is the
// generated MessageBus interface + models + the generated NewTemporalMessageBus
// constructor, plus the KindBinding construction-input type the composition root
// needs to build the kind table. It holds the control-plane client handle and the
// ExecutionKind registry; it imports Temporal but exposes only the opaque port.
type temporalMessageBus struct {
	// cl is the Temporal control-plane client (already bound to the runtime's
	// namespace by the constructor's caller). Used ONLY for control-plane RPC —
	// this utility never runs inside a workflow, so it holds no Worker.
	cl client.Client
	// registry resolves a logical ExecutionKind to its infrastructure binding
	// (workflow type name + task queue).
	registry *kindRegistry
}

// Compile-time proof the concrete impl satisfies the port. If the port drifts,
// this line breaks the build — the guard The Method wants between a frozen
// contract and its construction.
var _ MessageBus = (*temporalMessageBus)(nil)

// KindBinding is the construction-time binding of a logical ExecutionKind to its
// infrastructure workflow-type name and task queue. The aiarch-server bootstrap
// supplies the SAME table to NewTemporalMessageBus and to its Worker
// registration so the names line up. It is exported only so the bootstrap can build the table; it
// carries no Temporal lexeme (a "WorkflowType" string is just the control-plane
// address).
type KindBinding struct {
	// WorkflowType is the runtime's workflow-type name for this kind.
	WorkflowType string
	// TaskQueue is the owning Manager's task queue (one per Manager).
	TaskQueue string
}

// newTemporalMessageBus is the hand-written, unexported builder behind
// the generated NewTemporalMessageBus constructor (option-1 delegated
// DI). It builds the impl over a Temporal control-plane client and the
// ExecutionKind → binding table — constructing the hand-written kindRegistry — and
// returns the MessageBus interface so the concrete struct stays
// unexported. The constructor performs no IO; infrastructure failures surface
// lazily on the first call as typed fwra errors. cl must be non-nil; an empty table
// is allowed (every RegisterSchedule then surfaces fwra.ContractMisuse for an
// unknown kind, which is the correct pre-condition failure).
func newTemporalMessageBus(cl client.Client, table map[ExecutionKind]KindBinding) MessageBus {
	internal := make(map[ExecutionKind]kindBinding, len(table))
	for k, b := range table {
		internal[k] = kindBinding{workflowType: b.WorkflowType, taskQueue: b.TaskQueue}
	}
	return &temporalMessageBus{cl: cl, registry: newKindRegistry(internal)}
}

// DeliverSignal implements the cross-execution fire-and-forget signal — the
// transport under a queued Manager→Manager edge. void return; at-least-once to
// the channel.
func (r *temporalMessageBus) DeliverSignal(rc fwra.Context, targetExecutionID ExecutionID, signalName SignalName, payload ExecutionPayload) error {
	ctx := rc.Context
	if targetExecutionID == "" {
		return fwra.New(fwra.ContractMisuse, "messagebus.DeliverSignal: empty targetExecutionID")
	}
	if signalName == "" {
		return fwra.New(fwra.ContractMisuse, "messagebus.DeliverSignal: empty signalName")
	}
	if err := r.cl.SignalWorkflow(ctx, string(targetExecutionID), "", string(signalName), payload.Bytes); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// RegisterSchedule implements idempotent recurring-schedule registration.
// Idempotent on ScheduleID; last-writer-wins on a changed spec.
func (r *temporalMessageBus) RegisterSchedule(rc fwra.Context, scheduleID ScheduleID, spec ScheduleSpec) error {
	ctx := rc.Context
	if scheduleID == "" {
		return fwra.New(fwra.ContractMisuse, "messagebus.RegisterSchedule: empty scheduleID")
	}
	binding, ok := r.registry.resolve(spec.ExecutionKind)
	if !ok {
		return fwra.New(fwra.ContractMisuse, "messagebus.RegisterSchedule: unknown executionKind "+string(spec.ExecutionKind))
	}
	scheduleSpec, err := toScheduleSpec(spec.Cadence)
	if err != nil {
		return err
	}
	action := &client.ScheduleWorkflowAction{
		ID:        spec.TargetIDTemplate,
		Workflow:  binding.workflowType,
		Args:      []any{spec.StartPayload.Bytes},
		TaskQueue: binding.taskQueue,
	}
	sc := r.cl.ScheduleClient()
	_, createErr := sc.Create(ctx, client.ScheduleOptions{
		ID:     string(scheduleID),
		Spec:   scheduleSpec,
		Action: action,
	})
	if createErr == nil {
		return nil
	}
	if errors.Is(createErr, temporal.ErrScheduleAlreadyRunning) {
		// Converge an existing schedule to the new spec (last-writer-wins): Update the
		// handle in place. Re-registering the SAME spec is then a harmless no-op write.
		handle := sc.GetHandle(ctx, string(scheduleID))
		updateErr := handle.Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(in client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				// Mutate the EXISTING schedule in place: overwrite the volatile parts
				// (Spec + Action) and preserve the runtime-managed Policy/State pointers
				// so the SDK's proto conversion (which dereferences Policy) does not panic.
				updated := in.Description.Schedule
				updated.Spec = &scheduleSpec
				updated.Action = action
				return &client.ScheduleUpdate{Schedule: &updated}, nil
			},
		})
		if updateErr != nil {
			return mapScheduleError(updateErr)
		}
		return nil
	}
	return mapScheduleError(createErr)
}

// toScheduleSpec maps the infrastructure-neutral Cadence to the runtime's
// schedule spec. Exactly one of Every / CronExpr must be set.
func toScheduleSpec(c Cadence) (client.ScheduleSpec, error) {
	switch {
	case c.Every > 0 && c.CronExpr != "":
		return client.ScheduleSpec{}, fwra.New(fwra.ContractMisuse, "messagebus: cadence sets both Every and CronExpr")
	case c.Every > 0:
		return client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: c.Every}}}, nil
	case c.CronExpr != "":
		return client.ScheduleSpec{CronExpressions: []string{c.CronExpr}}, nil
	default:
		return client.ScheduleSpec{}, fwra.New(fwra.ContractMisuse, "messagebus: cadence sets neither Every nor CronExpr")
	}
}

// ---- error mapping: Temporal/gRPC error → shared fwra.Error ----
//
// Each mapper translates a runtime error into the shared RA error model with an
// accurate kind (and thus Retryable flag). No Temporal type crosses the port: the
// CALLER sees only *fwra.Error.

// mapSignalError classifies a signal-delivery failure. A missing target execution
// is the logical ErrNotFound.
func mapSignalError(err error) error {
	if k := classifyCommon(err); k != nil {
		return k
	}
	return fwra.Wrap(fwra.Transient, err, "messagebus.DeliverSignal: runtime error")
}

// mapScheduleError classifies a schedule create/update failure.
func mapScheduleError(err error) error {
	if k := classifyCommon(err); k != nil {
		return k
	}
	return fwra.Wrap(fwra.Transient, err, "messagebus.RegisterSchedule: runtime error")
}

// classifyCommon maps the runtime error kinds shared across every op. Returns
// the mapped error when it recognises the input; nil otherwise so each caller
// applies its own default.
func classifyCommon(err error) error {
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return fwra.Wrap(fwra.NotFound, err, "messagebus: no execution with that id")
	}
	var invalid *serviceerror.InvalidArgument
	if errors.As(err, &invalid) {
		return fwra.Wrap(fwra.ContractMisuse, err, "messagebus: invalid argument")
	}
	var unavailable *serviceerror.Unavailable
	if errors.As(err, &unavailable) {
		return fwra.Wrap(fwra.Transient, err, "messagebus: runtime unavailable")
	}
	return nil
}

// Error is the shared ResourceAccess error model (framework-go), re-exported as
// an alias so this component's contract reads in its own terms while every
// port-bearing component shares one fixed enum (the Utility layer reuses the
// ResourceAccess call Context and error model — see the modelgen layerContext
// rationale). Construct with fwra.New / fwra.Wrap using the shared kinds. The
// contract's logical error vocabulary maps onto the shared kinds as follows:
//
//   - ErrTransient        → fwra.Transient        (retryable: gRPC blip / 5xx)
//   - ErrNotFound         → fwra.NotFound         (terminal: no execution with id)
//   - ErrUnknownKind      → fwra.ContractMisuse   (terminal: kind/signal not registered)
//   - ErrInfrastructure   → fwra.Infrastructure   (escalate)
//   - ErrContractMisuse   → fwra.ContractMisuse   (terminal: caller pre-condition)
type Error = fwra.Error

// The ExecutionKind → (infrastructure workflow-type name, task queue) mapping is
// owned INSIDE this package, populated from the same registry the Worker
// bootstrap uses. It is the seam that keeps the port surface
// infrastructure-opaque: callers name a LOGICAL ExecutionKind; this registry
// resolves it to the concrete workflow type name and the per-Manager task queue
// that the embedded Worker bootstrap registers under.
//
// The registry is a plain in-memory lookup with NO Temporal lexeme on it (a
// "workflow type name" here is just the string the runtime addresses). The
// aiarch-server Worker bootstrap that registers the matching workflow functions
// is expected to be seeded from the SAME table so the names line up.

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
// runtime — a pre-condition check the contract owns.
type kindRegistry struct {
	bindings map[ExecutionKind]kindBinding
}

// newKindRegistry builds a registry from a kind → (workflowType, taskQueue)
// table. The aiarch-server bootstrap supplies the same table to both this utility and
// the Worker registration so the names are guaranteed consistent.
func newKindRegistry(table map[ExecutionKind]kindBinding) *kindRegistry {
	bindings := make(map[ExecutionKind]kindBinding, len(table))
	maps.Copy(bindings, table)
	return &kindRegistry{bindings: bindings}
}

// resolve returns the binding for kind and whether it is registered.
func (r *kindRegistry) resolve(kind ExecutionKind) (kindBinding, bool) {
	b, ok := r.bindings[kind]
	return b, ok
}
