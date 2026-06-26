package durableexecution

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

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	temporalinfra "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-temporal/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

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
func scheduledWorkflow(ctx workflow.Context, _ []byte) error {
	return nil
}

// integrationRuntime spins a test Worker registering the two workflow types and
// returns a Runtime over the dev-server client bound to the test registry.
func integrationRuntime(t *testing.T) (*Runtime, client.Client) {
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

	r := NewRuntime(c, map[ExecutionKind]KindBinding{
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
func waitForStatus(t *testing.T, r *Runtime, id ExecutionID, want ExecutionStatus) ExecutionStateView {
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
