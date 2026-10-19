// Hooks for messageBus scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file. Fill the bodies.
//
// The subject is the REAL Temporal-backed bus, built through the generated
// public constructor NewTemporalMessageBus against the Temporal dev server the
// scenario host booted (h.TemporalHostPort / h.TemporalNamespace), with the
// composition root's ExecutionKind table keyed on the owning Managers' own
// exported TaskQueue constants. The fronted substrate of this utility IS the
// durable-execution runtime, so the hooks speak its control-plane SDK to
// arrange and observe what the port itself never exposes: a running target
// execution for DeliverSignal, and the removal of the schedules a scenario
// registered (the dev server's database persists across runs).

package messagebus_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// receiverTaskQueue is the task queue the scenario-owned signal receiver
// listens on. It is no Manager's queue, so no Manager worker sharing the dev
// server's database ever picks up the receiver's executions.
const receiverTaskQueue = "dct-messagebus-receiver"

// receiverWorkflowType is the registered name of awaitOneSignal.
const receiverWorkflowType = "dctMessageBusAwaitOneSignal"

// settleTimeout bounds every hook-side wait on the runtime.
const settleTimeout = 30 * time.Second

// kindTable is the composition root's ExecutionKind → KindBinding table
// (cmd/server/hooks.go MessageBusTemporalArgs), keyed on the owning Managers'
// exported TaskQueue constants so a renamed queue breaks this file too.
func kindTable() map[messagebus.ExecutionKind]messagebus.KindBinding {
	return map[messagebus.ExecutionKind]messagebus.KindBinding{
		"billingShortfallSweep": {WorkflowType: "billingShortfallSweep", TaskQueue: billing.TaskQueue},
		"billingCloseCycle":     {WorkflowType: "billingCloseCycle", TaskQueue: billing.TaskQueue},
		"operationsReconcile":   {WorkflowType: "operationsReconcile", TaskQueue: operations.TaskQueue},
		"constructionPumpSweep": {WorkflowType: "constructionPumpSweep", TaskQueue: delivery.TaskQueue},
		"deliveryRoundSweep":    {WorkflowType: "deliveryRoundSweep", TaskQueue: delivery.TaskQueue},
	}
}

// dialTemporal opens a control-plane client on the host's dev server, closed
// when t ends.
func dialTemporal(t *testing.T, h *scenariohost.Host) client.Client {
	t.Helper()
	cl, err := client.Dial(client.Options{HostPort: h.TemporalHostPort, Namespace: h.TemporalNamespace})
	if err != nil {
		t.Fatalf("hooks: dial temporal %s/%s: %v", h.TemporalHostPort, h.TemporalNamespace, err)
	}
	t.Cleanup(cl.Close)
	return cl
}

// scheduleSweeper remembers every schedule id a scenario registers through the
// subject and deletes them when the scenario ends, so the persistent dev-server
// database never carries a scenario's schedule into the next run (a fresh
// registration stays a fresh registration) and no left-over schedule ever
// fires into a Manager's task queue.
type scheduleSweeper struct {
	messagebus.MessageBus

	mu  sync.Mutex
	ids []messagebus.ScheduleID
}

// RegisterSchedule records scheduleID, then delegates to the real bus.
func (s *scheduleSweeper) RegisterSchedule(rc fwra.Context, scheduleID messagebus.ScheduleID, spec messagebus.ScheduleSpec) error {
	s.mu.Lock()
	if !slices.Contains(s.ids, scheduleID) {
		s.ids = append(s.ids, scheduleID)
	}
	s.mu.Unlock()
	return s.MessageBus.RegisterSchedule(rc, scheduleID, spec)
}

// sweep deletes every recorded schedule; one that is already gone is fine.
func (s *scheduleSweeper) sweep(t *testing.T, cl client.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.ids {
		err := cl.ScheduleClient().GetHandle(ctx, string(id)).Delete(ctx)
		var notFound *serviceerror.NotFound
		if err != nil && !errors.As(err, &notFound) {
			t.Errorf("hooks: delete schedule %s: %v", id, err)
		}
	}
}

// newSubject builds the component under test against the host's real downstream
// doubles: the generated constructor over a client of the host's Temporal dev
// server and the composition root's kind table.
func newSubject(t *testing.T, h *scenariohost.Host) messagebus.MessageBus {
	t.Helper()
	cl := dialTemporal(t, h)
	s := &scheduleSweeper{MessageBus: messagebus.NewTemporalMessageBus(cl, kindTable())}
	t.Cleanup(func() { s.sweep(t, cl) })
	return s
}

// rcFor is the call context an arranging call runs with.
func rcFor(t *testing.T, h *scenariohost.Host, key string) fwra.Context {
	t.Helper()
	return fwra.Context{Context: h.Context(t), IdempotencyKey: fwra.IdempotencyKey(t.Name() + "/" + key)}
}

// stepOnboardANewCustomerP1S1 — Onboard a New Customer — [yes], step 1: RegisterSchedule
// Projected stimulus (JSON): {"seq":1,"node":"register-schedule","input":{"kind":"call","from":"billing-manager","to":"messagebus","op":"RegisterSchedule","call":"onboard-a-new-customer/register-schedule/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"RegisterSchedule","inputs":[{"name":"scheduleID","value":"closeBillingCycle:0b5c0000-0000-4000-8000-000000000201"},{"name":"spec","value":"{\"ExecutionKind\":\"billingCloseCycle\",\"Cadence\":{\"Every\":86400000000000,\"CronExpr\":\"\"},\"TargetIDTemplate\":\"billingCloseCycle:0b5c0000-0000-4000-8000-000000000201\",\"StartPayload\":{\"Bytes\":\"\",\"ContentType\":\"\"}}"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
//
// The [yes] branch is the customer's app already registered: its billing
// cycle schedule already exists, under an older spec. The arrangement
// registers that older spec (a cron cadence and a different target) and
// proves it landed; the step's registration must then converge the existing
// schedule onto the new spec (idempotent on the id, last writer wins), which
// the generated describe step and probe observe.
func stepOnboardANewCustomerP1S1(t *testing.T, h *scenariohost.Host, subject messagebus.MessageBus, in inputOnboardANewCustomerP1S1) (any, error) {
	t.Helper()
	older := messagebus.ScheduleSpec{
		ExecutionKind:    in.Spec.ExecutionKind,
		Cadence:          messagebus.Cadence{CronExpr: "0 0 1 1 *"},
		TargetIDTemplate: in.Spec.TargetIDTemplate + ":older",
	}
	if err := subject.RegisterSchedule(rcFor(t, h, "arrange-older"), in.ScheduleID, older); err != nil {
		t.Fatalf("hooks: arrange the already-registered schedule %s: %v", in.ScheduleID, err)
	}
	got, err := subject.DescribeSchedule(rcFor(t, h, "arrange-describe"), in.ScheduleID)
	if err != nil {
		t.Fatalf("hooks: describe the arranged schedule %s: %v", in.ScheduleID, err)
	}
	if !got.Registered || got.TargetIDTemplate != older.TargetIDTemplate || got.ExecutionKind != older.ExecutionKind {
		t.Fatalf("hooks: arranged schedule %s reads back as %+v, want registered %s → %s", in.ScheduleID, got, older.ExecutionKind, older.TargetIDTemplate)
	}
	return nil, subject.RegisterSchedule(rcFor(t, h, "1"), in.ScheduleID, in.Spec)
}

// awaitOneSignal is the receiving execution a DeliverSignal targets: it blocks
// on the named signal channel and returns the delivered payload bytes
// unchanged, so the hook can prove the bus is a transport, not a serialiser.
func awaitOneSignal(ctx workflow.Context, signalName string) ([]byte, error) {
	var got []byte
	workflow.GetSignalChannel(ctx, signalName).Receive(ctx, &got)
	return got, nil
}

// stepPauseConstructionP1S1 — Pause Construction, step 1: DeliverSignal
// Projected stimulus (JSON): {"seq":1,"node":"stop-pump","input":{"kind":"call","from":"delivery-manager","to":"messagebus","op":"DeliverSignal","call":"pause-construction/stop-pump/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliverSignal","inputs":[{"name":"targetExecutionID","value":"dct-messagebus-pause-p1:nextActivity"},{"name":"signalName","value":"operatorPauseRequested"},{"name":"payload","value":"{\"Bytes\":\"eyJQcm9qZWN0SUQiOiJkY3QtbWVzc2FnZWJ1cy1wYXVzZS1wMSIsIlJlYXNvbiI6Im9wZXJhdG9yIHBhdXNlZCBjb25zdHJ1Y3Rpb24ifQ==\",\"ContentType\":\"application/json\"}"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
//
// The pump must be running for the stop to reach it: the arrangement starts a
// receiving execution under the target id, waiting on the signal's channel.
// After the delivery, the receiver must complete with exactly the payload
// bytes the delivery carried.
func stepPauseConstructionP1S1(t *testing.T, h *scenariohost.Host, subject messagebus.MessageBus, in inputPauseConstructionP1S1) (any, error) {
	t.Helper()
	return nil, deliverToReceiver(t, h, subject, in.TargetExecutionID, in.SignalName, in.Payload, in.Start)
}

// stepResumePausedConstructionP3S1 — Resume Paused Construction — [yes], step 1: DeliverSignal
// Projected stimulus (JSON): {"seq":1,"node":"release-halt","input":{"kind":"call","from":"delivery-manager","to":"messagebus","op":"DeliverSignal","call":"resume-paused-construction/release-halt/0"},"expectedOutputs":null,"expectedStates":[{"node":"run-state-running","object":"Project run state","inState":"[running]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliverSignal","inputs":[{"name":"targetExecutionID","value":"dct-messagebus-resume-p3:nextActivity"},{"name":"signalName","value":"operatorResumeRequested"},{"name":"payload","value":"{\"Bytes\":\"eyJQcm9qZWN0SUQiOiJkY3QtbWVzc2FnZWJ1cy1yZXN1bWUtcDMiLCJIYWx0ZWRPbiI6IkMtcmV2aWV3LWVuZ2luZSIsIkhhbHRlZEF0IjoiMjAyNi0xMC0xN1QyMTo0Nzo1MVoiLCJIYWx0Q2F1c2UiOiJoZWFkLXN0YXRlIGNvbmZsaWN0IGRpZCBub3QgY29udmVyZ2Ugd2l0aGluIGJvdW5kZWQgYXR0ZW1wdHMifQ==\"}"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
//
// The resume releases a HALTED pump (FIX-003), which is parked and running under the target
// id: the arrangement starts a receiving execution there, waiting on the signal's channel,
// and the receiver must complete with exactly the payload bytes the delivery carried.
func stepResumePausedConstructionP3S1(t *testing.T, h *scenariohost.Host, subject messagebus.MessageBus, in inputResumePausedConstructionP3S1) (any, error) {
	t.Helper()
	return nil, deliverToReceiver(t, h, subject, in.TargetExecutionID, in.SignalName, in.Payload, in.Start)
}

// deliverToReceiver is the two DeliverSignal steps' arrangement and check: a receiving
// execution is started under target, waiting on signal's channel; the delivery is made; and
// a delivery that succeeded must reach it with its payload bytes unchanged.
func deliverToReceiver(t *testing.T, h *scenariohost.Host, subject messagebus.MessageBus, target messagebus.ExecutionID, signal messagebus.SignalName, payload messagebus.ExecutionPayload, start *messagebus.ExecutionStart) error {
	t.Helper()
	cl := dialTemporal(t, h)
	w := worker.New(cl, receiverTaskQueue, worker.Options{})
	w.RegisterWorkflowWithOptions(awaitOneSignal, workflow.RegisterOptions{Name: receiverWorkflowType})
	if err := w.Start(); err != nil {
		t.Fatalf("hooks: start the receiver worker: %v", err)
	}
	t.Cleanup(w.Stop)

	run, err := cl.ExecuteWorkflow(h.Context(t), client.StartWorkflowOptions{
		ID:                       string(target),
		TaskQueue:                receiverTaskQueue,
		WorkflowExecutionTimeout: time.Minute,
	}, receiverWorkflowType, string(signal))
	if err != nil {
		t.Fatalf("hooks: start the receiving execution %s: %v", target, err)
	}

	err = subject.DeliverSignal(rcFor(t, h, "1"), target, signal, payload, start)
	if err == nil {
		requireReceived(t, h, run, target, signal, payload)
	}
	return err
}

// requireReceived waits for the receiving execution to complete and fails the
// scenario unless it received exactly the delivered payload bytes.
func requireReceived(t *testing.T, h *scenariohost.Host, run client.WorkflowRun, target messagebus.ExecutionID, signal messagebus.SignalName, payload messagebus.ExecutionPayload) {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.Context(t), settleTimeout)
	defer cancel()
	var received []byte
	if err := run.Get(ctx, &received); err != nil {
		t.Fatalf("hooks: the receiving execution %s did not complete on %s: %v", target, signal, err)
	}
	if !bytes.Equal(received, payload.Bytes) {
		t.Fatalf("hooks: %s received %q on %s, delivered %q", target, received, signal, payload.Bytes)
	}
}
