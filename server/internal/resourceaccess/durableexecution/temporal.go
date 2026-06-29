package durableexecution

// temporal.go is the SOLE file in this RA that touches the Temporal Go SDK. It is
// the concrete durableExecutionRuntime-backed implementation of the
// DurableExecutionAccess port (durableExecutionAccess.md §6 infrastructure
// mapping). Every Temporal type is confined here and NEVER leaks back across the
// port: the four methods accept and return only the infrastructure-opaque value
// types declared in durableexecution.go.
//
// INFRASTRUCTURE MAPPING (caller-opaque; for the senior reviewer and future
// maintainers, durableExecutionAccess.md §6):
//
//   - StartOrSignalExecution → client.ExecuteWorkflow (cold start, reuse-policy
//     RejectDuplicate → returns the existing handle on AlreadyStarted) when no
//     signal is named; client.SignalWithStartWorkflow (signal-with-start) when a
//     signal IS named. Runtime-native idempotency on the workflow id (= the
//     caller-supplied ExecutionID).
//   - DeliverSignal → client.SignalWorkflow(ctx, id, "", signal, payload).
//     At-least-once to the channel; "not found" → fwra.NotFound.
//   - RegisterSchedule → ScheduleClient().Create(...); on
//     ErrScheduleAlreadyRunning, GetHandle(...).Update(...) to converge
//     (idempotent on ScheduleID, last-writer-wins on a changed spec).
//   - QueryExecutionState → DescribeWorkflowExecution (status + timestamps) +
//     QueryWorkflow (the named query handler's result). A rejected query →
//     fwra.ContentPolicy (the logical ErrQueryRejected).
//
// PAYLOAD OPACITY: ExecutionPayload.Bytes are passed to the runtime as a raw
// []byte argument. Temporal's default data converter stores a []byte verbatim
// (ByteSlicePayloadConverter), so the bytes round-trip uninterpreted — this RA is
// a transport, not a serialiser. The receiving workflow / signal / query handler
// owns the payload semantics.
//
// AUTH (durableExecutionAccess.md §6 "Auth model"): the runtime connection is
// authenticated where the client.Client is constructed (mTLS / namespace creds
// acquired by the aiarch-server pod's identity), never threaded through the port.
// Connection-level failures surface as fwra.Infrastructure / fwra.Transient.
//
// This file imports Temporal; that is the WHOLE POINT of confining it here and
// nowhere else — the port (durableexecution.go) and the registry (registry.go)
// import none.

import (
	"errors"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// temporalDurableExecutionAccess is the concrete Temporal-client-backed
// implementation of the DurableExecutionAccess port. It is UNEXPORTED — the
// package's only public surface is the generated DurableExecutionAccess interface +
// models + the generated NewTemporalDurableExecutionAccess constructor (plus the
// KindBinding construction-input type and the value-type behaviour free functions).
// It holds the control-plane client handle and the ExecutionKind registry; it
// imports Temporal but exposes only the opaque port.
type temporalDurableExecutionAccess struct {
	// cl is the Temporal control-plane client (already bound to the runtime's
	// namespace by the constructor's caller). Used ONLY for control-plane RPC —
	// this RA never runs inside a workflow, so it holds no Worker.
	cl client.Client
	// registry resolves a logical ExecutionKind to its infrastructure binding
	// (workflow type name + task queue).
	registry *kindRegistry
}

// Compile-time proof the concrete impl satisfies the port. If the port drifts,
// this line breaks the build — the guard The Method wants between a frozen
// contract and its construction.
var _ DurableExecutionAccess = (*temporalDurableExecutionAccess)(nil)

// KindBinding is the construction-time binding of a logical ExecutionKind to its
// infrastructure workflow-type name and task queue. The aiarch-server bootstrap
// supplies the SAME table to NewTemporalDurableExecutionAccess and to its Worker
// registration so the names line up. It is exported only so the bootstrap can build the table; it
// carries no Temporal lexeme (a "WorkflowType" string is just the control-plane
// address).
type KindBinding struct {
	// WorkflowType is the runtime's workflow-type name for this kind.
	WorkflowType string
	// TaskQueue is the owning Manager's task queue (one per Manager).
	TaskQueue string
}

// newTemporalDurableExecutionAccess is the hand-written, unexported builder behind
// the generated NewTemporalDurableExecutionAccess constructor (option-1 delegated
// DI). It builds the impl over a Temporal control-plane client and the
// ExecutionKind → binding table — constructing the hand-written kindRegistry — and
// returns the DurableExecutionAccess interface so the concrete struct stays
// unexported. The constructor performs no IO; infrastructure failures surface
// lazily on the first call as typed fwra errors. cl must be non-nil; an empty table
// is allowed (every StartOrSignal/Schedule then surfaces fwra.ContractMisuse for an
// unknown kind, which is the correct pre-condition failure).
func newTemporalDurableExecutionAccess(cl client.Client, table map[ExecutionKind]KindBinding) DurableExecutionAccess {
	internal := make(map[ExecutionKind]kindBinding, len(table))
	for k, b := range table {
		internal[k] = kindBinding{workflowType: b.WorkflowType, taskQueue: b.TaskQueue}
	}
	return &temporalDurableExecutionAccess{cl: cl, registry: newKindRegistry(internal)}
}

// StartOrSignalExecution implements the Client entry verb (durableExecutionAccess.md
// §2.1). Empty signalName → cold start (idempotent on ExecutionID); set signalName
// → signal-with-start. Returns once durably accepted.
func (r *temporalDurableExecutionAccess) StartOrSignalExecution(rc fwra.Context, executionKind ExecutionKind, executionID ExecutionID, signalName SignalName, payload ExecutionPayload) (ExecutionHandle, error) {
	// The cross-cutting ctx (and, where a verb needs them, Principal / IdempotencyKey)
	// now ride the ResourceAccess call Context. fwra.Context embeds context.Context; the
	// runtime is natively idempotent on the caller-supplied ExecutionID, so this verb
	// reads only ctx here. The package still imports no Temporal on its surface.
	ctx := rc.Context
	if executionID == "" {
		return "", fwra.New(fwra.ContractMisuse, "durableexecution.StartOrSignalExecution: empty executionID")
	}
	binding, ok := r.registry.resolve(executionKind)
	if !ok {
		// The logical ErrUnknownKind — a caller pre-condition violation owned by this
		// contract, surfaced WITHOUT consulting the runtime.
		return "", fwra.New(fwra.ContractMisuse, "durableexecution.StartOrSignalExecution: unknown executionKind "+string(executionKind))
	}

	opts := client.StartWorkflowOptions{
		ID:        string(executionID),
		TaskQueue: binding.taskQueue,
		// Reject a duplicate start of the same id; we map AlreadyStarted to the
		// existing handle below (the runtime-native idempotency the contract promises).
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
		// Make the cold-start path RETURN the AlreadyStarted error so we can detect it
		// and resolve to the existing handle (rather than silently get a fresh run).
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}

	var (
		run client.WorkflowRun
		err error
	)
	if signalName == "" {
		run, err = r.cl.ExecuteWorkflow(ctx, opts, binding.workflowType, payload.Bytes)
	} else {
		// signal-with-start: atomic start-or-signal. Conflict policy UseExisting so a
		// running execution receives the signal instead of erroring.
		opts.WorkflowIDConflictPolicy = enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING
		opts.WorkflowExecutionErrorWhenAlreadyStarted = false
		run, err = r.cl.SignalWithStartWorkflow(ctx, string(executionID), string(signalName), payload.Bytes, opts, binding.workflowType, payload.Bytes)
	}
	if err != nil {
		// AlreadyStarted on the cold-start path is the idempotent re-issue case: fetch
		// the existing run and return its handle as SUCCESS (durableExecutionAccess.md
		// §2.1, §6: AlreadyExists is mapped to success, never surfaced).
		if temporal.IsWorkflowExecutionAlreadyStartedError(err) {
			existing := r.cl.GetWorkflow(ctx, string(executionID), "")
			return ExecutionHandle(handleString(existing.GetID(), existing.GetRunID())), nil
		}
		return "", mapStartError(err)
	}
	return ExecutionHandle(handleString(run.GetID(), run.GetRunID())), nil
}

// DeliverSignal implements the cross-execution fire-and-forget signal
// (durableExecutionAccess.md §2.2). void return; at-least-once to the channel.
func (r *temporalDurableExecutionAccess) DeliverSignal(rc fwra.Context, targetExecutionID ExecutionID, signalName SignalName, payload ExecutionPayload) error {
	ctx := rc.Context
	if targetExecutionID == "" {
		return fwra.New(fwra.ContractMisuse, "durableexecution.DeliverSignal: empty targetExecutionID")
	}
	if signalName == "" {
		return fwra.New(fwra.ContractMisuse, "durableexecution.DeliverSignal: empty signalName")
	}
	if err := r.cl.SignalWorkflow(ctx, string(targetExecutionID), "", string(signalName), payload.Bytes); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// RegisterSchedule implements idempotent recurring-schedule registration
// (durableExecutionAccess.md §2.3). Idempotent on ScheduleID; last-writer-wins on
// a changed spec.
func (r *temporalDurableExecutionAccess) RegisterSchedule(rc fwra.Context, scheduleID ScheduleID, spec ScheduleSpec) error {
	ctx := rc.Context
	if scheduleID == "" {
		return fwra.New(fwra.ContractMisuse, "durableexecution.RegisterSchedule: empty scheduleID")
	}
	binding, ok := r.registry.resolve(spec.ExecutionKind)
	if !ok {
		return fwra.New(fwra.ContractMisuse, "durableexecution.RegisterSchedule: unknown executionKind "+string(spec.ExecutionKind))
	}
	scheduleSpec, err := toScheduleSpec(spec.Cadence)
	if err != nil {
		return err
	}
	action := &client.ScheduleWorkflowAction{
		ID:        spec.TargetIDTemplate,
		Workflow:  binding.workflowType,
		Args:      []interface{}{spec.StartPayload.Bytes},
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

// QueryExecutionState implements the read-only technical query
// (durableExecutionAccess.md §2.4). Side-effect-free.
func (r *temporalDurableExecutionAccess) QueryExecutionState(rc fwra.Context, executionID ExecutionID, queryName QueryName, args ExecutionPayload) (ExecutionStateView, error) {
	ctx := rc.Context
	if executionID == "" {
		return ExecutionStateView{}, fwra.New(fwra.ContractMisuse, "durableexecution.QueryExecutionState: empty executionID")
	}

	desc, err := r.cl.DescribeWorkflowExecution(ctx, string(executionID), "")
	if err != nil {
		return ExecutionStateView{}, mapQueryError(err)
	}
	info := desc.GetWorkflowExecutionInfo()
	view := ExecutionStateView{
		Handle: ExecutionHandle(handleString(string(executionID), info.GetExecution().GetRunId())),
		Status: mapStatus(info.GetStatus()),
	}
	if st := info.GetStartTime(); st != nil {
		view.StartedAt = pbTime(st)
	}
	if ct := info.GetCloseTime(); ct != nil {
		closed := pbTime(ct)
		view.ClosedAt = &closed
	}

	// Run the named query only when a query name is supplied AND the execution is in
	// a state where a query handler can respond (running, or completed within the
	// retention window). A query against a failed/cancelled execution would be
	// rejected; we leave QueryResult empty rather than surface that as an error,
	// because the status itself already answers the caller.
	if queryName != "" && (view.Status == StatusRunning || view.Status == StatusCompleted) {
		enc, qErr := r.cl.QueryWorkflow(ctx, string(executionID), "", string(queryName), args.Bytes)
		if qErr != nil {
			return ExecutionStateView{}, mapQueryError(qErr)
		}
		if enc != nil && enc.HasValue() {
			var raw []byte
			if getErr := enc.Get(&raw); getErr != nil {
				return ExecutionStateView{}, fwra.Wrap(fwra.Infrastructure, getErr, "durableexecution.QueryExecutionState: decode query result")
			}
			view.QueryResult = raw
		}
	}
	return view, nil
}

// ---- internal infrastructure helpers (no Temporal type crosses the port) ----

// handleString is the opaque ExecutionHandle encoding: "{workflowID}|{runID}". A
// pipe (never present in a workflow id we issue, since ids are business-derived)
// joins the pair; callers treat the whole string as opaque and compare by value.
func handleString(workflowID, runID string) string {
	if runID == "" {
		return workflowID
	}
	return workflowID + "|" + runID
}

// protoTime is the minimal shape of a protobuf timestamp (AsTime). Accepting it
// by interface lets pbTime convert the runtime's timestamps WITHOUT this RA
// importing the protobuf timestamppb package directly (which is not on the
// arch-checker's sanctioned allowlist — only the framework-sanctioned drivers
// are). The Temporal response timestamps satisfy this interface.
type protoTime interface{ AsTime() time.Time }

// pbTime converts a protobuf timestamp to a Go time, returning the zero time for
// a nil/absent timestamp. The nil check is on the concrete value via reflection-
// free comparison: a nil *timestamppb.Timestamp passed as protoTime is a non-nil
// interface wrapping a nil pointer, so we guard by catching the panic-free path —
// callers only pass non-nil timestamps after a GetCloseTime()!=nil / GetStartTime
// guard, so a direct AsTime() is safe here.
func pbTime(ts protoTime) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}

// toScheduleSpec maps the infrastructure-neutral Cadence to the runtime's
// schedule spec. Exactly one of Every / CronExpr must be set.
func toScheduleSpec(c Cadence) (client.ScheduleSpec, error) {
	switch {
	case c.Every > 0 && c.CronExpr != "":
		return client.ScheduleSpec{}, fwra.New(fwra.ContractMisuse, "durableexecution: cadence sets both Every and CronExpr")
	case c.Every > 0:
		return client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: c.Every}}}, nil
	case c.CronExpr != "":
		return client.ScheduleSpec{CronExpressions: []string{c.CronExpr}}, nil
	default:
		return client.ScheduleSpec{}, fwra.New(fwra.ContractMisuse, "durableexecution: cadence sets neither Every nor CronExpr")
	}
}

// mapStatus maps the runtime's execution status to the infrastructure-neutral
// ExecutionStatus. RUNNING and PAUSED (suspended) collapse to StatusRunning per
// the contract's deliberate status collapsing (durableExecutionAccess.md §3,
// §9 OQ6).
func mapStatus(s enumspb.WorkflowExecutionStatus) ExecutionStatus {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		enumspb.WORKFLOW_EXECUTION_STATUS_PAUSED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return StatusRunning
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return StatusCompleted
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return StatusFailed
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return StatusCancelled
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return StatusTimedOut
	default:
		return StatusUnknown
	}
}

// ---- error mapping: Temporal/gRPC error → shared fwra.Error ----
//
// Each mapper translates a runtime error into the shared RA error model with an
// accurate kind (and thus Retryable flag). No Temporal type crosses the port: the
// CALLER sees only *fwra.Error.

// mapStartError classifies a start/signal-with-start failure.
func mapStartError(err error) error {
	if k, ok := classifyCommon(err); ok {
		return k
	}
	return fwra.Wrap(fwra.Transient, err, "durableexecution.StartOrSignalExecution: runtime error")
}

// mapSignalError classifies a signal-delivery failure. A missing target execution
// is the logical ErrNotFound.
func mapSignalError(err error) error {
	if k, ok := classifyCommon(err); ok {
		return k
	}
	return fwra.Wrap(fwra.Transient, err, "durableexecution.DeliverSignal: runtime error")
}

// mapScheduleError classifies a schedule create/update failure.
func mapScheduleError(err error) error {
	if k, ok := classifyCommon(err); ok {
		return k
	}
	return fwra.Wrap(fwra.Transient, err, "durableexecution.RegisterSchedule: runtime error")
}

// mapQueryError classifies a describe/query failure. A rejected query is the
// logical ErrQueryRejected (mapped to fwra.ContentPolicy — terminal).
func mapQueryError(err error) error {
	var qErr *serviceerror.QueryFailed
	if errors.As(err, &qErr) {
		return fwra.Wrap(fwra.ContentPolicy, err, "durableexecution.QueryExecutionState: query rejected by handler")
	}
	if k, ok := classifyCommon(err); ok {
		return k
	}
	return fwra.Wrap(fwra.Transient, err, "durableexecution.QueryExecutionState: runtime error")
}

// classifyCommon maps the runtime error kinds shared across every op. Returns
// (mapped, true) when it recognises the error; (nil, false) otherwise so each
// caller applies its own default.
func classifyCommon(err error) (error, bool) {
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return fwra.Wrap(fwra.NotFound, err, "durableexecution: no execution with that id"), true
	}
	var invalid *serviceerror.InvalidArgument
	if errors.As(err, &invalid) {
		return fwra.Wrap(fwra.ContractMisuse, err, "durableexecution: invalid argument"), true
	}
	var unavailable *serviceerror.Unavailable
	if errors.As(err, &unavailable) {
		return fwra.Wrap(fwra.Transient, err, "durableexecution: runtime unavailable"), true
	}
	return nil, false
}
