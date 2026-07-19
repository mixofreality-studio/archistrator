package durableexecution

// SERVICE TEST PLAN (STP) — durableExecutionAccess (C-DA).
//
// Per [[the-method-testing]], the STP enumerates every way to demonstrate the
// component does NOT work. It is written before the code and split into two
// tiers:
//
//   UNIT (this file, always-run, no infrastructure) — the pure / pre-condition /
//   mapping surface that needs no runtime:
//     U1  StartOrSignalExecution rejects an empty executionID         → ContractMisuse
//     U2  StartOrSignalExecution rejects an unknown executionKind     → ContractMisuse
//         (and does so WITHOUT consulting the runtime — nil client proves it)
//     U3  DeliverSignal rejects an empty targetExecutionID            → ContractMisuse
//     U4  DeliverSignal rejects an empty signalName                   → ContractMisuse
//     U5  RegisterSchedule rejects an empty scheduleID                → ContractMisuse
//     U6  RegisterSchedule rejects an unknown executionKind           → ContractMisuse
//     U7  QueryExecutionState rejects an empty executionID            → ContractMisuse
//     U8  Cadence mapping: Every → interval spec; CronExpr → cron spec;
//         both-set and neither-set → ContractMisuse
//     U9  Status mapping: every runtime status collapses to the right
//         infrastructure-neutral ExecutionStatus (RUNNING/PAUSED/CONT-AS-NEW →
//         Running; terminal kinds distinct)
//     U10 ExecutionHandle value semantics: Equal / String / IsZero
//     U11 Error-vocabulary mapping: QueryFailed → ContentPolicy; NotFound →
//         NotFound; InvalidArgument → ContractMisuse; Unavailable → Transient;
//         unclassified → Transient (and the default-retryable flags are correct)
//     U12 Registry resolve hit/miss
//
//   INTEGRATION (temporal_integration_test.go, gated under -short) — the four ops
//   against a REAL embedded Temporal dev server with a test Worker:
//     I1  startOrSignalExecution cold-starts a fresh execution
//     I2  startOrSignalExecution is idempotent: a second start of the SAME id
//         converges on the SAME handle (no duplicate execution)
//     I3  startOrSignalExecution signal-with-start delivers a signal into a
//         running execution and completes it
//     I4  deliverSignal delivers a signal to a running execution's channel
//     I5  deliverSignal to a non-existent id → NotFound
//     I6  registerSchedule registers a recurring schedule; re-register same id is
//         an idempotent success
//     I7  queryExecutionState returns status + the named query handler's result
//     I8  queryExecutionState of a non-existent id → NotFound
//
// NO Temporal lexeme appears on this test file's references to the PUBLIC surface
// (it constructs ExecutionKind/ExecutionID/SignalName/ScheduleSpec/Cadence and
// reads ExecutionStateView) — the package-internal _test.go does reach the
// unexported helpers (mapStatus, toScheduleSpec, classifyCommon) to unit-test the
// mapping in isolation.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	temporalinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// rc builds the ResourceAccess call Context the contract verbs now take (the
// established RA seam: fwra.Context embeds context.Context). The unit tests carry no
// idempotency key — the runtime is natively idempotent on the caller-supplied
// ExecutionID.
func rc(ctx context.Context) fwra.Context { return fwra.Context{Context: ctx} }

// testTable is the canonical kind→binding table the unit tests register.
func testTable() map[ExecutionKind]KindBinding {
	return map[ExecutionKind]KindBinding{
		"systemDesignPhase1": {WorkflowType: "SystemDesignPhase1", TaskQueue: "system-design"},
		"settlementCycle":    {WorkflowType: "SettlementCycleClose", TaskQueue: "settlement"},
	}
}

// assertKind asserts err is an *fwra.Error of the wanted kind.
func assertKind(t *testing.T, err error, want fwra.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %s, got nil", want)
	}
	var e *fwra.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fwra.Error, got %T: %v", err, err)
	}
	if e.Kind != want {
		t.Fatalf("expected kind %s, got %s (detail: %s)", want, e.Kind, e.Detail)
	}
}

// ---- U1, U2: StartOrSignalExecution pre-conditions (nil client: never reached) ----

func TestStartOrSignal_EmptyID_ContractMisuse(t *testing.T) {
	r := NewTemporalDurableExecutionAccess(nil, testTable()) // nil client: a pre-condition failure must NOT touch it
	_, err := r.StartOrSignalExecution(rc(t.Context()), "systemDesignPhase1", "", "", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestStartOrSignal_UnknownKind_ContractMisuse_NoRuntime(t *testing.T) {
	r := NewTemporalDurableExecutionAccess(nil, testTable()) // nil client proves the unknown-kind check is local
	_, err := r.StartOrSignalExecution(rc(t.Context()), "noSuchKind", "proj:phase1", "", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U3, U4: DeliverSignal pre-conditions ----

func TestDeliverSignal_EmptyTarget_ContractMisuse(t *testing.T) {
	r := NewTemporalDurableExecutionAccess(nil, testTable())
	err := r.DeliverSignal(rc(t.Context()), "", "applyDelinquencyPolicy", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestDeliverSignal_EmptySignal_ContractMisuse(t *testing.T) {
	r := NewTemporalDurableExecutionAccess(nil, testTable())
	err := r.DeliverSignal(rc(t.Context()), "operations:reconcile", "", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U5, U6: RegisterSchedule pre-conditions ----

func TestRegisterSchedule_EmptyID_ContractMisuse(t *testing.T) {
	r := NewTemporalDurableExecutionAccess(nil, testTable())
	err := r.RegisterSchedule(rc(t.Context()), "", ScheduleSpec{ExecutionKind: "settlementCycle", Cadence: Cadence{Every: time.Hour}})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestRegisterSchedule_UnknownKind_ContractMisuse_NoRuntime(t *testing.T) {
	r := NewTemporalDurableExecutionAccess(nil, testTable())
	err := r.RegisterSchedule(rc(t.Context()), "shortfallSweep", ScheduleSpec{ExecutionKind: "noSuchKind", Cadence: Cadence{Every: time.Hour}})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U7: QueryExecutionState pre-condition ----

func TestQueryExecutionState_EmptyID_ContractMisuse(t *testing.T) {
	r := NewTemporalDurableExecutionAccess(nil, testTable())
	_, err := r.QueryExecutionState(rc(t.Context()), "", "costProjection", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U8: Cadence mapping ----

func TestToScheduleSpec_Cadence(t *testing.T) {
	t.Run("every", func(t *testing.T) {
		spec, err := toScheduleSpec(Cadence{Every: 30 * time.Second})
		if err != nil {
			t.Fatalf("toScheduleSpec(every): %v", err)
		}
		if len(spec.Intervals) != 1 || spec.Intervals[0].Every != 30*time.Second {
			t.Fatalf("expected one 30s interval, got %+v", spec.Intervals)
		}
		if len(spec.CronExpressions) != 0 {
			t.Fatalf("expected no cron expressions, got %v", spec.CronExpressions)
		}
	})
	t.Run("cron", func(t *testing.T) {
		spec, err := toScheduleSpec(Cadence{CronExpr: "0 * * * *"})
		if err != nil {
			t.Fatalf("toScheduleSpec(cron): %v", err)
		}
		if len(spec.CronExpressions) != 1 || spec.CronExpressions[0] != "0 * * * *" {
			t.Fatalf("expected one cron expr, got %v", spec.CronExpressions)
		}
		if len(spec.Intervals) != 0 {
			t.Fatalf("expected no intervals, got %+v", spec.Intervals)
		}
	})
	t.Run("both_set_misuse", func(t *testing.T) {
		_, err := toScheduleSpec(Cadence{Every: time.Hour, CronExpr: "0 * * * *"})
		assertKind(t, err, fwra.ContractMisuse)
	})
	t.Run("neither_set_misuse", func(t *testing.T) {
		_, err := toScheduleSpec(Cadence{})
		assertKind(t, err, fwra.ContractMisuse)
	})
}

// ---- U9: Status mapping ----

func TestMapStatus(t *testing.T) {
	cases := []struct {
		in   enumspb.WorkflowExecutionStatus
		want ExecutionStatus
	}{
		{enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, StatusRunning},
		{enumspb.WORKFLOW_EXECUTION_STATUS_PAUSED, StatusRunning},
		{enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW, StatusRunning},
		{enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED, StatusCompleted},
		{enumspb.WORKFLOW_EXECUTION_STATUS_FAILED, StatusFailed},
		{enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED, StatusCancelled},
		{enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED, StatusCancelled},
		{enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT, StatusTimedOut},
		{enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED, StatusUnknown},
	}
	for _, c := range cases {
		if got := mapStatus(c.in); got != c.want {
			t.Errorf("mapStatus(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// ---- U10: ExecutionHandle value semantics ----

func TestExecutionHandle_ValueSemantics(t *testing.T) {
	a := ExecutionHandle(handleString("proj:phase1", "run-1"))
	b := ExecutionHandle(handleString("proj:phase1", "run-1"))
	c := ExecutionHandle(handleString("proj:phase1", "run-2"))

	if !ExecutionHandleEqual(a, b) {
		t.Errorf("expected equal handles a==b")
	}
	if ExecutionHandleEqual(a, c) {
		t.Errorf("expected unequal handles a!=c")
	}
	if ExecutionHandleString(a) != "proj:phase1|run-1" {
		t.Errorf("unexpected ExecutionHandleString(): %q", ExecutionHandleString(a))
	}
	if ExecutionHandleIsZero(a) {
		t.Errorf("non-empty handle reported IsZero")
	}
	if !ExecutionHandleIsZero(ExecutionHandle("")) {
		t.Errorf("zero handle reported not-IsZero")
	}
	// runID-less handle is just the workflow id.
	if got := handleString("proj:phase1", ""); got != "proj:phase1" {
		t.Errorf("handleString(no run) = %q, want proj:phase1", got)
	}
}

// ---- U11: error-vocabulary mapping ----

func TestErrorMapping(t *testing.T) {
	t.Run("query_rejected_is_content_policy", func(t *testing.T) {
		err := mapQueryError(serviceerror.NewQueryFailed("handler said no"))
		assertKind(t, err, fwra.ContentPolicy)
		var e *fwra.Error
		_ = errors.As(err, &e)
		if e.Retryable {
			t.Errorf("query-rejected must be terminal (non-retryable)")
		}
	})
	t.Run("not_found_on_signal", func(t *testing.T) {
		err := mapSignalError(serviceerror.NewNotFound("no such execution"))
		assertKind(t, err, fwra.NotFound)
	})
	t.Run("not_found_on_query", func(t *testing.T) {
		err := mapQueryError(serviceerror.NewNotFound("no such execution"))
		assertKind(t, err, fwra.NotFound)
	})
	t.Run("invalid_argument_is_contract_misuse", func(t *testing.T) {
		err := mapStartError(serviceerror.NewInvalidArgument("bad type"))
		assertKind(t, err, fwra.ContractMisuse)
	})
	t.Run("unavailable_is_transient", func(t *testing.T) {
		err := mapStartError(serviceerror.NewUnavailable("cluster blip"))
		assertKind(t, err, fwra.Transient)
		var e *fwra.Error
		_ = errors.As(err, &e)
		if !e.Retryable {
			t.Errorf("unavailable must be retryable")
		}
	})
	t.Run("unclassified_is_transient", func(t *testing.T) {
		err := mapSignalError(errors.New("opaque gRPC blip"))
		assertKind(t, err, fwra.Transient)
	})
}

// ---- U12: registry resolve ----

func TestKindRegistry_Resolve(t *testing.T) {
	reg := newKindRegistry(map[ExecutionKind]kindBinding{
		"k1": {workflowType: "WT1", taskQueue: "tq1"},
	})
	if b, ok := reg.resolve("k1"); !ok || b.workflowType != "WT1" || b.taskQueue != "tq1" {
		t.Fatalf("resolve(k1) = %+v, %v", b, ok)
	}
	if _, ok := reg.resolve("missing"); ok {
		t.Fatalf("resolve(missing) reported a hit")
	}
}

// INTEGRATION TESTS (STP tier I1–I8) — the four control-plane ops exercised
// against a REAL embedded Temporal dev server (framework-go-infrastructure-temporal
// /testinfra) with a test Worker that registers the workflow types the kind
// registry names. Gated behind testing.Short(): TestMain skips the dev-server
// boot under -short, so `go test -short ./...` stays fast and infra-free.
//
// The test Worker registers two trivial workflow types whose ONLY job is to make
// the control-plane verbs demonstrable:
//
//   - signalWaiterWorkflow: starts, exposes a "state" query handler returning the
//     last payload it has seen, waits for a "go" signal, then completes returning
//     the signal payload. This drives cold-start (I1), idempotent re-start (I2),
//     signal-with-start (I3), deliverSignal (I4), and the query (I7).
//   - scheduledWorkflow: a no-op that completes immediately, used as a schedule
//     target (I6).
//
// These workflow funcs import Temporal — but they are TEST CODE standing in for
// the Manager workflow bodies, NOT part of the RA. The RA (Runtime) drives them
// purely through the control-plane client, exactly as production Clients/Managers
// will.

const (
	testTaskQueue            = "durableexecution-test"
	kindSignalWaiter         = ExecutionKind("signalWaiter")
	kindScheduled            = ExecutionKind("scheduledNoop")
	wtSignalWaiter           = "SignalWaiterWorkflow"
	wtScheduled              = "ScheduledNoopWorkflow"
	signalGo                 = "go"
	queryState               = "state"
	integrationWaitTimeout   = 20 * time.Second
	integrationPollFrequency = 100 * time.Millisecond
)

var sharedDevServer *temporalinfra.DevServer

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	srv, err := temporalinfra.StartDevServer(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "durableexecution integration: start dev server: %v\n", err)
		os.Exit(1)
	}
	sharedDevServer = srv
	code := m.Run()
	if stopErr := srv.Stop(); stopErr != nil {
		fmt.Fprintf(os.Stderr, "durableexecution integration: stop dev server: %v\n", stopErr)
	}
	os.Exit(code)
}

// signalWaiterWorkflow: query "state" returns the last payload seen; waits for the
// "go" signal, then completes returning the signal payload. Payloads are raw
// []byte (matching the RA's byte-transport convention).
func signalWaiterWorkflow(ctx workflow.Context, start []byte) ([]byte, error) {
	last := start
	if err := workflow.SetQueryHandler(ctx, queryState, func() ([]byte, error) {
		return last, nil
	}); err != nil {
		return nil, err
	}
	ch := workflow.GetSignalChannel(ctx, signalGo)
	var got []byte
	ch.Receive(ctx, &got)
	last = got
	return got, nil
}

// scheduledWorkflow: a no-op schedule target that completes immediately.
func scheduledWorkflow(_ workflow.Context, _ []byte) error {
	return nil
}

// integrationRuntime spins a test Worker registering the two workflow types and
// returns a Runtime over the dev-server client bound to the test registry.
func integrationRuntime(t *testing.T) (DurableExecutionAccess, client.Client) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration: skipped under -short (requires the Temporal dev server)")
	}
	c := sharedDevServer.Client()

	w := worker.New(c, testTaskQueue, worker.Options{})
	w.RegisterWorkflowWithOptions(signalWaiterWorkflow, workflow.RegisterOptions{Name: wtSignalWaiter})
	w.RegisterWorkflowWithOptions(scheduledWorkflow, workflow.RegisterOptions{Name: wtScheduled})
	if err := w.Start(); err != nil {
		t.Fatalf("worker.Start: %v", err)
	}
	t.Cleanup(w.Stop)

	r := NewTemporalDurableExecutionAccess(c, map[ExecutionKind]KindBinding{
		kindSignalWaiter: {WorkflowType: wtSignalWaiter, TaskQueue: testTaskQueue},
		kindScheduled:    {WorkflowType: wtScheduled, TaskQueue: testTaskQueue},
	})
	return r, c
}

// uniqueID derives a per-test execution id so concurrent / repeat runs do not
// collide on the persistent dev-server DB.
func uniqueID(t *testing.T, prefix string) ExecutionID {
	t.Helper()
	return ExecutionID(fmt.Sprintf("%s:%d", prefix, time.Now().UnixNano()))
}

// waitForStatus polls queryExecutionState until the status matches or the timeout
// elapses.
func waitForStatus(t *testing.T, r DurableExecutionAccess, id ExecutionID, want ExecutionStatus) ExecutionStateView {
	t.Helper()
	ctx := t.Context()
	deadline := time.Now().Add(integrationWaitTimeout)
	var last ExecutionStateView
	for time.Now().Before(deadline) {
		v, err := r.QueryExecutionState(rc(ctx), id, "", ExecutionPayload{})
		if err == nil {
			last = v
			if v.Status == want {
				return v
			}
		}
		time.Sleep(integrationPollFrequency)
	}
	t.Fatalf("execution %s did not reach status %v (last: %v)", id, want, last.Status)
	return last
}

// I1: cold-start a fresh execution.
func TestIntegration_StartOrSignal_ColdStart(t *testing.T) {
	r, c := integrationRuntime(t)
	id := uniqueID(t, "cold")
	h, err := r.StartOrSignalExecution(rc(t.Context()), kindSignalWaiter, id, "", ExecutionPayload{Bytes: []byte(`"hello"`)})
	if err != nil {
		t.Fatalf("StartOrSignalExecution: %v", err)
	}
	if ExecutionHandleIsZero(h) {
		t.Fatalf("expected a non-zero handle")
	}
	waitForStatus(t, r, id, StatusRunning)
	// terminate the waiter so the run closes (signal it then let the worker finish).
	if err := r.DeliverSignal(rc(t.Context()), id, signalGo, ExecutionPayload{Bytes: []byte(`"done"`)}); err != nil {
		t.Fatalf("DeliverSignal teardown: %v", err)
	}
	_ = c
	dumpHistory(t, id)
}

// I2: idempotent re-start — a second cold start of the SAME id returns the SAME
// handle, no duplicate execution.
func TestIntegration_StartOrSignal_IdempotentReissue(t *testing.T) {
	r, _ := integrationRuntime(t)
	id := uniqueID(t, "idem")
	h1, err := r.StartOrSignalExecution(rc(t.Context()), kindSignalWaiter, id, "", ExecutionPayload{Bytes: []byte(`"first"`)})
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	waitForStatus(t, r, id, StatusRunning)

	h2, err := r.StartOrSignalExecution(rc(t.Context()), kindSignalWaiter, id, "", ExecutionPayload{Bytes: []byte(`"second"`)})
	if err != nil {
		t.Fatalf("idempotent re-start surfaced an error (must map AlreadyExists to success): %v", err)
	}
	if !ExecutionHandleEqual(h1, h2) {
		t.Fatalf("idempotent re-start returned a DIFFERENT handle: %s vs %s", h1, h2)
	}
	// teardown
	_ = r.DeliverSignal(rc(t.Context()), id, signalGo, ExecutionPayload{Bytes: []byte(`"done"`)})
	dumpHistory(t, id)
}

// I3: signal-with-start cold-starts then drives the execution to completion via the
// start path's signal.
func TestIntegration_StartOrSignal_SignalWithStart(t *testing.T) {
	r, _ := integrationRuntime(t)
	id := uniqueID(t, "sws")
	// signal-with-start a fresh id: starts the workflow AND delivers "go", so the
	// waiter receives the signal and completes.
	h, err := r.StartOrSignalExecution(rc(t.Context()), kindSignalWaiter, id, signalGo, ExecutionPayload{Bytes: []byte(`"sws-payload"`)})
	if err != nil {
		t.Fatalf("signal-with-start: %v", err)
	}
	if ExecutionHandleIsZero(h) {
		t.Fatalf("expected a non-zero handle")
	}
	waitForStatus(t, r, id, StatusCompleted)
	dumpHistory(t, id)
}

// I4: deliverSignal to a running execution's channel completes it.
func TestIntegration_DeliverSignal_ToRunning(t *testing.T) {
	r, _ := integrationRuntime(t)
	id := uniqueID(t, "deliver")
	if _, err := r.StartOrSignalExecution(rc(t.Context()), kindSignalWaiter, id, "", ExecutionPayload{Bytes: []byte(`"start"`)}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, r, id, StatusRunning)
	if err := r.DeliverSignal(rc(t.Context()), id, signalGo, ExecutionPayload{Bytes: []byte(`"delivered"`)}); err != nil {
		t.Fatalf("DeliverSignal: %v", err)
	}
	waitForStatus(t, r, id, StatusCompleted)
	dumpHistory(t, id)
}

// I5: deliverSignal to a non-existent id → NotFound.
func TestIntegration_DeliverSignal_NotFound(t *testing.T) {
	r, _ := integrationRuntime(t)
	err := r.DeliverSignal(rc(t.Context()), uniqueID(t, "ghost"), signalGo, ExecutionPayload{Bytes: []byte(`"x"`)})
	assertKind(t, err, fwra.NotFound)
}

// I6: registerSchedule registers a recurring schedule; re-register the same id is
// an idempotent success.
func TestIntegration_RegisterSchedule_Idempotent(t *testing.T) {
	r, c := integrationRuntime(t)
	scheduleID := ScheduleID(fmt.Sprintf("sched:%d", time.Now().UnixNano()))
	spec := ScheduleSpec{
		ExecutionKind:    kindScheduled,
		Cadence:          Cadence{Every: time.Hour}, // long cadence: we only assert registration, not firing
		TargetIDTemplate: string(scheduleID) + "-{{.ScheduledTime.Unix}}",
		StartPayload:     ExecutionPayload{Bytes: []byte(`"tick"`)},
	}
	if err := r.RegisterSchedule(rc(t.Context()), scheduleID, spec); err != nil {
		t.Fatalf("RegisterSchedule (create): %v", err)
	}
	t.Cleanup(func() {
		_ = c.ScheduleClient().GetHandle(context.Background(), string(scheduleID)).Delete(context.Background())
	})
	// Re-register the SAME id with the SAME spec: must converge as an idempotent
	// success (no error), exercising the AlreadyRunning → Update path.
	if err := r.RegisterSchedule(rc(t.Context()), scheduleID, spec); err != nil {
		t.Fatalf("RegisterSchedule (idempotent re-register): %v", err)
	}
	// Re-register with a CHANGED spec: last-writer-wins, still a success.
	spec.Cadence = Cadence{Every: 2 * time.Hour}
	if err := r.RegisterSchedule(rc(t.Context()), scheduleID, spec); err != nil {
		t.Fatalf("RegisterSchedule (changed spec): %v", err)
	}
}

// I7: queryExecutionState returns status + the named query handler's result.
func TestIntegration_QueryExecutionState_ReturnsResult(t *testing.T) {
	r, _ := integrationRuntime(t)
	id := uniqueID(t, "query")
	payload := []byte(`"queried-state"`)
	if _, err := r.StartOrSignalExecution(rc(t.Context()), kindSignalWaiter, id, "", ExecutionPayload{Bytes: payload}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForStatus(t, r, id, StatusRunning)

	view, err := r.QueryExecutionState(rc(t.Context()), id, queryState, ExecutionPayload{})
	if err != nil {
		t.Fatalf("QueryExecutionState: %v", err)
	}
	if view.Status != StatusRunning {
		t.Fatalf("expected StatusRunning, got %v", view.Status)
	}
	if string(view.QueryResult) != string(payload) {
		t.Fatalf("query result = %q, want %q", view.QueryResult, payload)
	}
	if view.StartedAt.IsZero() {
		t.Fatalf("expected a non-zero StartedAt")
	}
	if view.ClosedAt != nil {
		t.Fatalf("expected nil ClosedAt while running, got %v", *view.ClosedAt)
	}
	// teardown
	_ = r.DeliverSignal(rc(t.Context()), id, signalGo, ExecutionPayload{Bytes: []byte(`"done"`)})
	dumpHistory(t, id)
}

// I8: queryExecutionState of a non-existent id → NotFound.
func TestIntegration_QueryExecutionState_NotFound(t *testing.T) {
	r, _ := integrationRuntime(t)
	_, err := r.QueryExecutionState(rc(t.Context()), uniqueID(t, "ghost"), queryState, ExecutionPayload{})
	assertKind(t, err, fwra.NotFound)
}

// dumpHistory writes the workflow event history as a replayable artifact (like
// playwright), matching the projectstate/systemdesign integration convention. It
// is best-effort: a closed/absent execution simply logs.
func dumpHistory(t *testing.T, id ExecutionID) {
	t.Helper()
	if sharedDevServer == nil {
		return
	}
	// Give the worker a beat to flush the closing event before dumping.
	time.Sleep(200 * time.Millisecond)
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Logf("dumpHistory(%s): %v", id, rec)
			}
		}()
		sharedDevServer.DumpHistory(t.Context(), t, string(id), "")
	}()
}
