package messagebus

// SERVICE TEST PLAN (STP) — messageBus (C-DA, folded from durableExecutionAccess
// by the 2026-08-01 reclassification).
//
// Per [[the-method-testing]], the STP enumerates every way to demonstrate the
// component does NOT work. It is written before the code and split into two
// tiers. The ratified verb set is exactly TWO — deliverSignal and
// registerSchedule — so the plan covers those and nothing else; the former RA's
// speculative substrate surface (startOrSignal / queryExecutionState) was shed
// with the fold and its cases went with it.
//
//   UNIT (this file, always-run, no infrastructure) — the pure / pre-condition /
//   mapping surface that needs no runtime:
//     U1  DeliverSignal rejects an empty targetExecutionID            → ContractMisuse
//     U2  DeliverSignal rejects an empty signalName                   → ContractMisuse
//     U3  RegisterSchedule rejects an empty scheduleID                → ContractMisuse
//     U4  RegisterSchedule rejects an unknown executionKind           → ContractMisuse
//         (and does so WITHOUT consulting the runtime — nil client proves it)
//     U5  Cadence mapping: Every → interval spec; CronExpr → cron spec;
//         both-set and neither-set → ContractMisuse
//     U6  Error-vocabulary mapping: NotFound → NotFound; InvalidArgument →
//         ContractMisuse; Unavailable → Transient; unclassified → Transient
//         (and the default-retryable flags are correct)
//     U7  Registry resolve hit/miss
//
//   INTEGRATION (gated under -short) — the two ops against a REAL embedded
//   Temporal dev server with a test Worker:
//     I1  deliverSignal delivers a signal to a running execution's channel
//     I2  deliverSignal to a non-existent id → NotFound
//     I3  registerSchedule registers a recurring schedule; re-register same id is
//         an idempotent success; re-register with a CHANGED spec converges
//         (last-writer-wins)
//
// NO Temporal lexeme appears on this test file's references to the PUBLIC surface
// (it constructs ExecutionKind/ExecutionID/SignalName/ScheduleSpec/Cadence) — the
// package-internal _test.go does reach the unexported helpers (toScheduleSpec,
// classifyCommon) to unit-test the mapping in isolation, and the integration tier
// drives the Temporal client DIRECTLY for arrange/assert, because starting and
// observing an execution are no longer verbs on this port.

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

// rc builds the call Context the contract verbs take (the Utility layer reuses
// the ResourceAccess seam: fwra.Context embeds context.Context). The unit tests
// carry no idempotency key — the runtime is natively idempotent on the
// caller-supplied ExecutionID / ScheduleID.
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

// ---- U1, U2: DeliverSignal pre-conditions (nil client: never reached) ----

func TestDeliverSignal_EmptyTarget_ContractMisuse(t *testing.T) {
	r := NewTemporalMessageBus(nil, testTable()) // nil client: a pre-condition failure must NOT touch it
	err := r.DeliverSignal(rc(t.Context()), "", "applyDelinquencyPolicy", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestDeliverSignal_EmptySignal_ContractMisuse(t *testing.T) {
	r := NewTemporalMessageBus(nil, testTable())
	err := r.DeliverSignal(rc(t.Context()), "operations:reconcile", "", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U3, U4: RegisterSchedule pre-conditions ----

func TestRegisterSchedule_EmptyID_ContractMisuse(t *testing.T) {
	r := NewTemporalMessageBus(nil, testTable())
	err := r.RegisterSchedule(rc(t.Context()), "", ScheduleSpec{ExecutionKind: "settlementCycle", Cadence: Cadence{Every: time.Hour}})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestRegisterSchedule_UnknownKind_ContractMisuse_NoRuntime(t *testing.T) {
	r := NewTemporalMessageBus(nil, testTable()) // nil client proves the unknown-kind check is local
	err := r.RegisterSchedule(rc(t.Context()), "shortfallSweep", ScheduleSpec{ExecutionKind: "noSuchKind", Cadence: Cadence{Every: time.Hour}})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U5: Cadence mapping ----

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

// ---- U6: error-vocabulary mapping ----

func TestErrorMapping(t *testing.T) {
	t.Run("not_found_on_signal", func(t *testing.T) {
		err := mapSignalError(serviceerror.NewNotFound("no such execution"))
		assertKind(t, err, fwra.NotFound)
	})
	t.Run("not_found_on_schedule", func(t *testing.T) {
		err := mapScheduleError(serviceerror.NewNotFound("no such schedule"))
		assertKind(t, err, fwra.NotFound)
	})
	t.Run("invalid_argument_is_contract_misuse", func(t *testing.T) {
		err := mapScheduleError(serviceerror.NewInvalidArgument("bad spec"))
		assertKind(t, err, fwra.ContractMisuse)
	})
	t.Run("unavailable_is_transient", func(t *testing.T) {
		err := mapSignalError(serviceerror.NewUnavailable("cluster blip"))
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

// ---- U7: registry resolve ----

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

// INTEGRATION TESTS (STP tier I1–I3) — the two control-plane verbs exercised
// against a REAL embedded Temporal dev server (framework-go-infrastructure-temporal
// /testinfra) with a test Worker that registers the workflow types the kind
// registry names. Gated behind testing.Short(): TestMain skips the dev-server
// boot under -short, so `go test -short ./...` stays fast and infra-free.
//
// The test Worker registers two trivial workflow types whose ONLY job is to make
// the verbs demonstrable:
//
//   - signalWaiterWorkflow: starts, waits for a "go" signal, then completes
//     returning the signal payload. This drives deliverSignal (I1).
//   - scheduledWorkflow: a no-op that completes immediately, used as a schedule
//     target (I3).
//
// STARTING and OBSERVING an execution are NOT verbs on this utility — a Manager
// owns its own executions and the substrate's execution role is invisible by
// design — so the arrange/assert steps drive the Temporal client directly. These
// workflow funcs and client calls import Temporal, but they are TEST CODE
// standing in for the Manager workflow bodies, not part of the component.

const (
	testTaskQueue            = "messagebus-test"
	kindSignalWaiter         = ExecutionKind("signalWaiter")
	kindScheduled            = ExecutionKind("scheduledNoop")
	wtSignalWaiter           = "SignalWaiterWorkflow"
	wtScheduled              = "ScheduledNoopWorkflow"
	signalGo                 = "go"
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
		fmt.Fprintf(os.Stderr, "messagebus integration: start dev server: %v\n", err)
		os.Exit(1)
	}
	sharedDevServer = srv
	code := m.Run()
	if stopErr := srv.Stop(); stopErr != nil {
		fmt.Fprintf(os.Stderr, "messagebus integration: stop dev server: %v\n", stopErr)
	}
	os.Exit(code)
}

// signalWaiterWorkflow waits for the "go" signal, then completes returning the
// signal payload. Payloads are raw []byte (matching the byte-transport convention).
func signalWaiterWorkflow(ctx workflow.Context, _ []byte) ([]byte, error) {
	ch := workflow.GetSignalChannel(ctx, signalGo)
	var got []byte
	ch.Receive(ctx, &got)
	return got, nil
}

// scheduledWorkflow: a no-op schedule target that completes immediately.
func scheduledWorkflow(_ workflow.Context, _ []byte) error {
	return nil
}

// integrationBus spins a test Worker registering the two workflow types and
// returns a MessageBus over the dev-server client bound to the test registry,
// alongside that raw client for the test's own arrange/assert steps.
func integrationBus(t *testing.T) (MessageBus, client.Client) {
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

	r := NewTemporalMessageBus(c, map[ExecutionKind]KindBinding{
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

// startWaiter starts a signalWaiterWorkflow through the raw client (the Manager's
// job in production) and returns its id.
func startWaiter(t *testing.T, c client.Client, id ExecutionID) {
	t.Helper()
	_, err := c.ExecuteWorkflow(t.Context(),
		client.StartWorkflowOptions{ID: string(id), TaskQueue: testTaskQueue},
		wtSignalWaiter, []byte(`"start"`))
	if err != nil {
		t.Fatalf("arrange: start %s: %v", id, err)
	}
}

// waitForStatus polls the raw client's DescribeWorkflowExecution until the status
// matches or the timeout elapses.
func waitForStatus(t *testing.T, c client.Client, id ExecutionID, want enumspb.WorkflowExecutionStatus) {
	t.Helper()
	ctx := t.Context()
	deadline := time.Now().Add(integrationWaitTimeout)
	last := enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED
	for time.Now().Before(deadline) {
		desc, err := c.DescribeWorkflowExecution(ctx, string(id), "")
		if err == nil {
			last = desc.GetWorkflowExecutionInfo().GetStatus()
			if last == want {
				return
			}
		}
		time.Sleep(integrationPollFrequency)
	}
	t.Fatalf("execution %s did not reach status %v (last: %v)", id, want, last)
}

// I1: deliverSignal to a running execution's channel completes it.
func TestIntegration_DeliverSignal_ToRunning(t *testing.T) {
	r, c := integrationBus(t)
	id := uniqueID(t, "deliver")
	startWaiter(t, c, id)
	waitForStatus(t, c, id, enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING)

	if err := r.DeliverSignal(rc(t.Context()), id, signalGo, ExecutionPayload{Bytes: []byte(`"delivered"`)}); err != nil {
		t.Fatalf("DeliverSignal: %v", err)
	}
	waitForStatus(t, c, id, enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED)
	dumpHistory(t, id)
}

// I2: deliverSignal to a non-existent id → NotFound.
func TestIntegration_DeliverSignal_NotFound(t *testing.T) {
	r, _ := integrationBus(t)
	err := r.DeliverSignal(rc(t.Context()), uniqueID(t, "ghost"), signalGo, ExecutionPayload{Bytes: []byte(`"x"`)})
	assertKind(t, err, fwra.NotFound)
}

// I3: registerSchedule registers a recurring schedule; re-register the same id is
// an idempotent success; a changed spec converges (last-writer-wins).
func TestIntegration_RegisterSchedule_Idempotent(t *testing.T) {
	r, c := integrationBus(t)
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
