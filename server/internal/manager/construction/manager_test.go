package construction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/temporalproto"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/types/known/timestamppb"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	projectstatefake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// ---------------------------------------------------------------------------
// Test helpers (fake Temporal client + constructor shim)
// ---------------------------------------------------------------------------

// fakeTemporalClient captures the last SignalWorkflow call. It embeds
// client.Client so the struct satisfies the interface without implementing
// every method; any unimplemented method panics if reached (none should be
// in these unit tests).
type fakeTemporalClient struct {
	client.Client
	lastWorkflowID string
	lastSignalName string
	lastSignalArg  any
	// session is the view the session Query answers (B1.3's façade precheck reads it).
	session ConstructionSessionView
}

// QueryWorkflow answers the session Query with the scripted view.
func (f *fakeTemporalClient) QueryWorkflow(_ context.Context, _ string, _ string, _ string, _ ...any) (converter.EncodedValue, error) {
	return encodedJSON{v: f.session}, nil
}

// encodedJSON satisfies converter.EncodedValue by a JSON round trip — how a real
// Query answer reaches the façade.
type encodedJSON struct{ v any }

func (e encodedJSON) HasValue() bool { return true }
func (e encodedJSON) Get(valuePtr any) error {
	b, err := json.Marshal(e.v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, valuePtr)
}

// awaitingAt is the session view of an activity waiting at gate key.
func awaitingAt(key string) ConstructionSessionView {
	return ConstructionSessionView{Stage: StageAwaitingApproval, AwaitingGate: &key}
}

func (f *fakeTemporalClient) SignalWorkflow(_ context.Context, workflowID string, _ string, signalName string, arg any) error {
	f.lastWorkflowID = workflowID
	f.lastSignalName = signalName
	f.lastSignalArg = arg
	return nil
}

// newTestConstructionManager wires a fake temporal client into a bare
// constructionManager (all other deps nil — only used for pre-Temporal checks
// and signal dispatch tests).
func newTestConstructionManager(c client.Client) *constructionManager {
	// A default project in construction and NOT paused: Begin reads it for the paused
	// precheck (B1.7), and every other façade op ignores it.
	ps := &fakeProjectState{project: projectstate.Project{Phase: projectstate.PhaseConstruction}}
	return newConstructionManager(c, fakeFullProjectState{ps}, nil, nil, nil, nil, nil, fakeConstructionTransition{ps}, nil, nil, nil, nil, 0, "", nil)
}

// testCtx returns a minimal fwmanager.Context backed by context.Background.
func testCtx() fwmanager.Context {
	return fwmanager.Context{Context: context.Background()}
}

// These tests cover the façade-boundary pre-condition checks the contract puts on
// the five public ops (constructionManager.md §2/§3.5). They run BEFORE any
// Temporal client call, so they need no cluster and no client — a nil client is
// safe because the checks short-circuit first.

func asConstructionError(t *testing.T, err error) *fwmanager.Error {
	t.Helper()
	var ce *fwmanager.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *constructionError, got %T: %v", err, err)
	}
	return ce
}

// ---- ExecuteNextActivity (op 2.1) ------------------------------------------

func Test_ExecuteNextActivity_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	_, err := m.ExecuteNextActivity(fwmanager.Context{Context: context.Background()}, ProjectID(""), "tick-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ExecuteNextActivity_EmptyTickID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	_, err := m.ExecuteNextActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// fakePumpRun satisfies client.WorkflowRun for the mocked ExecuteWorkflow result —
// only the ids the façade reads (GetRunID pins the dispatch-decision Query).
type fakePumpRun struct {
	client.WorkflowRun
	id, runID string
}

func (r fakePumpRun) GetID() string    { return r.id }
func (r fakePumpRun) GetRunID() string { return r.runID }

// fakeEncodedPumpDispatch satisfies converter.EncodedValue for the mocked
// queryPumpDispatch answer.
type fakeEncodedPumpDispatch struct{ d pumpDispatch }

func (f fakeEncodedPumpDispatch) HasValue() bool { return true }
func (f fakeEncodedPumpDispatch) Get(valuePtr any) error {
	p, ok := valuePtr.(*pumpDispatch)
	if !ok {
		return errors.New("fakeEncodedPumpDispatch: want *pumpDispatch")
	}
	*p = f.d
	return nil
}

// ONE PUMP PER PROJECT (architect pump ruling, 2026-09-12). Two client-driven calls
// for the same project with DIFFERENT tickIDs (two Begin clicks, a page remount, an
// MCP retry) must address the SAME pump workflow id — {projectId}:nextActivity — so
// the second JOINS the first (USE_EXISTING) instead of forking a second pump racing
// the same dependency frontier; ALLOW_DUPLICATE lets the next call restart a pump
// that already closed (drained quiet / paused). The mock only answers ExecuteWorkflow
// for exactly that id + policy pair: a per-tick id ({p}:nextActivity:{tick}) or a
// stricter reuse policy matches no expectation and the mock panics the test.
func Test_ExecuteNextActivity_DifferentTickIDs_SameProjectSingularPump(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	wantID := string(pid) + ":nextActivity"
	dispatched := ActivityID("C-1")

	mc := &temporalmocks.Client{}
	var startedIDs []string
	mc.On("ExecuteWorkflow", mock.Anything,
		mock.MatchedBy(func(o client.StartWorkflowOptions) bool {
			return o.ID == wantID &&
				o.WorkflowIDConflictPolicy == enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING &&
				o.WorkflowIDReusePolicy == enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE
		}),
		// B1.7: Begin no longer marks its pump operator-driven — the recorded pause binds
		// every pump (pump-honors-recorded-pause v2). A façade still setting it matches
		// nothing here.
		executionKindPump, pumpInput{ProjectID: pid}).
		Run(func(args mock.Arguments) {
			startedIDs = append(startedIDs, args.Get(1).(client.StartWorkflowOptions).ID)
		}).
		Return(fakePumpRun{id: wantID, runID: "run-1"}, nil)
	mc.On("QueryWorkflow", mock.Anything, wantID, "run-1", queryPumpDispatch).
		Return(fakeEncodedPumpDispatch{d: pumpDispatch{Decided: true, Dispatched: true, ActivityID: &dispatched}}, nil)

	m := newTestConstructionManager(mc)
	for _, tick := range []string{"t1", "t2"} {
		res, err := m.ExecuteNextActivity(testCtx(), pid, tick)
		if err != nil {
			t.Fatalf("ExecuteNextActivity(tick %q): %v", tick, err)
		}
		if !res.Dispatched || res.ActivityID == nil || *res.ActivityID != dispatched {
			t.Fatalf("tick %q: want the pump's decided dispatch of %s, got %+v", tick, dispatched, res)
		}
	}
	if len(startedIDs) != 2 || startedIDs[0] != wantID || startedIDs[1] != startedIDs[0] {
		t.Fatalf("both ticks must address the one project pump %q, got %v", wantID, startedIDs)
	}
	mc.AssertExpectations(t)
}

// blockingPumpRun is a WorkflowRun whose Get blocks until its context ends — a joined
// pump still cascading (Get follows the ContinueAsNew chain).
type blockingPumpRun struct{ client.WorkflowRun }

func (blockingPumpRun) GetID() string    { return "cascading" }
func (blockingPumpRun) GetRunID() string { return "run-1" }
func (blockingPumpRun) Get(ctx context.Context, _ any) error {
	<-ctx.Done()
	return ctx.Err()
}

// failedPumpRun is a WorkflowRun that has already FAILED with err (Get returns at once).
type failedPumpRun struct {
	client.WorkflowRun
	err error
}

func (failedPumpRun) GetID() string                    { return "failed" }
func (failedPumpRun) GetRunID() string                 { return "run-1" }
func (r failedPumpRun) Get(context.Context, any) error { return r.err }

// withPumpPollBudgets shortens the façade's dispatch-decision poll budgets for one test.
func withPumpPollBudgets(t *testing.T, dispatch, queryFailure, terminal, closureCheck time.Duration) {
	t.Helper()
	pd, pq, pt, pc := pumpDispatchWaitBudget, pumpQueryFailureBudget, pumpTerminalWaitBudget, pumpClosureCheckInterval
	pumpDispatchWaitBudget, pumpQueryFailureBudget, pumpTerminalWaitBudget, pumpClosureCheckInterval = dispatch, queryFailure, terminal, closureCheck
	t.Cleanup(func() {
		pumpDispatchWaitBudget, pumpQueryFailureBudget, pumpTerminalWaitBudget, pumpClosureCheckInterval = pd, pq, pt, pc
	})
}

// describeStatus is a DescribeWorkflowExecution answer carrying only a run status.
func describeStatus(s enumspb.WorkflowExecutionStatus) *workflowservice.DescribeWorkflowExecutionResponse {
	return &workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Status: s}}
}

// executeWithin runs ExecuteNextActivity under a wall-clock guard, so a regression that
// blocks on the cascade (or waits out a long budget) fails fast instead of hanging.
func executeWithin(t *testing.T, m *constructionManager, pid ProjectID, limit time.Duration) (PumpResult, error) {
	t.Helper()
	type outcome struct {
		res PumpResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := m.ExecuteNextActivity(testCtx(), pid, "t1")
		done <- outcome{res, err}
	}()
	select {
	case o := <-done:
		return o.res, o.err
	case <-time.After(limit):
		t.Fatalf("ExecuteNextActivity did not return within %s", limit)
		return PumpResult{}, nil
	}
}

// FIX ROUND 3, Minor 1 — "success after the budget". The dispatch-decision Query fails for
// LONGER than the terminal-wait budget (no worker polling during a rolling restart), then
// recovers and answers decided. A failing Query is retried, not read as "the run is gone",
// so Begin returns the pump's real decision — not an Infrastructure error while the pump
// in fact carries on and dispatches.
func Test_ExecuteNextActivity_QueryOutageLongerThanTerminalBudget_StillReturnsTheDecision(t *testing.T) {
	withPumpPollBudgets(t, 5*time.Second, 2*time.Second, 20*time.Millisecond, 10*time.Millisecond)
	pid := ProjectID(uuid.NewString())
	wfID := string(pid) + ":nextActivity"
	dispatched := ActivityID("C-1")

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, executionKindPump, mock.Anything).Return(blockingPumpRun{}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "run-1", queryPumpDispatch).
		Return(nil, errors.New("query failed: no poller")).Times(8) // ≈200ms of outage ≫ the 20ms terminal budget
	mc.On("QueryWorkflow", mock.Anything, wfID, "run-1", queryPumpDispatch).
		Return(fakeEncodedPumpDispatch{d: pumpDispatch{Decided: true, Dispatched: true, ActivityID: &dispatched}}, nil)
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "run-1").
		Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING), nil).Maybe()

	res, err := executeWithin(t, newTestConstructionManager(mc), pid, 3*time.Second)
	if err != nil {
		t.Fatalf("a query outage that recovers must return the decision, got %v", err)
	}
	if !res.Dispatched || res.ActivityID == nil || *res.ActivityID != dispatched {
		t.Fatalf("want the pump's decided dispatch of %s, got %+v", dispatched, res)
	}
}

// FIX ROUND 3, Minor 1 — "real failure after the budget". The run FAILS before deciding,
// and the failure is only observable after the terminal-wait budget has elapsed (the Query
// keeps answering "not decided" — closed runs are queried by replay). The façade detects
// the closed run and surfaces ITS real error promptly, not after the whole poll budget.
func Test_ExecuteNextActivity_RunFailsWhileUndecided_SurfacesItsRealError(t *testing.T) {
	withPumpPollBudgets(t, 5*time.Second, 2*time.Second, 20*time.Millisecond, 10*time.Millisecond)
	pid := ProjectID(uuid.NewString())
	wfID := string(pid) + ":nextActivity"

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, executionKindPump, mock.Anything).
		Return(failedPumpRun{err: errors.New("pump failed: readProject: store unreachable")}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "run-1", queryPumpDispatch).
		Return(fakeEncodedPumpDispatch{d: pumpDispatch{Decided: false}}, nil)
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "run-1").
		Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING), nil).Times(6) // ≥60ms ≫ the 20ms terminal budget
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "run-1").
		Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_FAILED), nil)

	_, err := executeWithin(t, newTestConstructionManager(mc), pid, 2*time.Second)
	ce := asConstructionError(t, err)
	if ce.Kind != fwmanager.Infrastructure || !strings.Contains(ce.Error(), "store unreachable") {
		t.Fatalf("want the run's real failure surfaced, got kind %s: %v", ce.Kind, err)
	}
}

// FIX ROUND 3, Minor 1 — "persistent query failure" (also fix-round M7). A Query that KEEPS
// failing falls back to the bounded terminal wait: a Begin that joined a RUNNING pump must
// not block for the whole self-cascade (Get follows ContinueAsNew) — it gets an
// Infrastructure error once pumpQueryFailureBudget + the terminal budget pass, well
// before the 30s poll budget. It is NOT the still-deciding outcome.
func Test_ExecuteNextActivity_QueryFailure_DoesNotWaitOnCascade(t *testing.T) {
	withPumpPollBudgets(t, 30*time.Second, 50*time.Millisecond, 50*time.Millisecond, 10*time.Millisecond)
	pid := ProjectID(uuid.NewString())
	wfID := string(pid) + ":nextActivity"

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, executionKindPump, mock.Anything).Return(blockingPumpRun{}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "run-1", queryPumpDispatch).
		Return(nil, errors.New("query failed: no poller"))
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "run-1").
		Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING), nil).Maybe()

	_, err := executeWithin(t, newTestConstructionManager(mc), pid, 5*time.Second)
	ce := asConstructionError(t, err)
	if ce.Kind != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure once the query keeps failing, got %s", ce.Kind)
	}
	if strings.Contains(ce.Error(), pumpStillDecidingDetail) {
		t.Fatalf("a persistently failing query is not the still-deciding outcome, got %v", err)
	}
}

// completedPumpRun is a WorkflowRun that COMPLETED (without having decided) with result.
type completedPumpRun struct {
	client.WorkflowRun
	result PumpResult
}

func (completedPumpRun) GetID() string    { return "completed" }
func (completedPumpRun) GetRunID() string { return "run-1" }
func (r completedPumpRun) Get(_ context.Context, valuePtr any) error {
	if p, ok := valuePtr.(*PumpResult); ok {
		*p = r.result
	}
	return nil
}

// undecodableAnswer is a Query answer whose payload fails to decode.
type undecodableAnswer struct{ err error }

func (undecodableAnswer) HasValue() bool  { return true }
func (u undecodableAnswer) Get(any) error { return u.err }

// withPumpRPCTimeout shortens the per-RPC bound for one test.
func withPumpRPCTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := pumpRPCTimeout
	pumpRPCTimeout = d
	t.Cleanup(func() { pumpRPCTimeout = prev })
}

// FIX ROUND 4, item 1. pumpRunClosed must treat EVERY non-running status as closed, not
// just FAILED: a run that was Canceled / Terminated / TimedOut surfaces its own error
// promptly, and one that COMPLETED without deciding returns its terminal result promptly.
// Each row keeps answering "not decided" (closed runs are queried by replay), so a check
// narrowed to `== FAILED` would poll the other rows out to the 5s budget and trip the 2s
// guard.
func Test_ExecuteNextActivity_EveryClosedStatus_EndsThePollPromptly(t *testing.T) {
	cases := []struct {
		name    string
		status  enumspb.WorkflowExecutionStatus
		run     client.WorkflowRun
		wantErr string // "" ⇒ want the run's terminal result, no error
	}{
		{"failed", enumspb.WORKFLOW_EXECUTION_STATUS_FAILED, failedPumpRun{err: errors.New("run failed: store unreachable")}, "store unreachable"},
		{"canceled", enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED, failedPumpRun{err: errors.New("run canceled by operator")}, "canceled by operator"},
		{"terminated", enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED, failedPumpRun{err: errors.New("run terminated: drain")}, "terminated: drain"},
		{"timed out", enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT, failedPumpRun{err: errors.New("run timed out")}, "timed out"},
		{"completed without deciding", enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED, completedPumpRun{result: PumpResult{Dispatched: false}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withPumpPollBudgets(t, 5*time.Second, 2*time.Second, 20*time.Millisecond, 10*time.Millisecond)
			pid := ProjectID(uuid.NewString())
			wfID := string(pid) + ":nextActivity"

			mc := &temporalmocks.Client{}
			mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, executionKindPump, mock.Anything).Return(tc.run, nil)
			mc.On("QueryWorkflow", mock.Anything, wfID, "run-1", queryPumpDispatch).
				Return(fakeEncodedPumpDispatch{d: pumpDispatch{Decided: false}}, nil)
			mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "run-1").Return(describeStatus(tc.status), nil)

			res, err := executeWithin(t, newTestConstructionManager(mc), pid, 2*time.Second)
			if tc.wantErr == "" {
				if err != nil || res.Dispatched {
					t.Fatalf("a run that completed without deciding must return its terminal result, got %+v err %v", res, err)
				}
				return
			}
			if ce := asConstructionError(t, err); !strings.Contains(ce.Error(), tc.wantErr) {
				t.Fatalf("want the %s run's own error (%q), got %v", tc.name, tc.wantErr, err)
			}
		})
	}
}

// FIX ROUND 4, item 2. A Query the run DID serve but whose answer fails to decode is a
// real error: it surfaces at once, with its message, and is never polled as "not
// decided" (which would run out to the budget and return the still-deciding outcome).
func Test_ExecuteNextActivity_UndecodableAnswer_SurfacesTheDecodeErrorPromptly(t *testing.T) {
	withPumpPollBudgets(t, 5*time.Second, 2*time.Second, 20*time.Millisecond, 10*time.Millisecond)
	pid := ProjectID(uuid.NewString())
	wfID := string(pid) + ":nextActivity"

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, executionKindPump, mock.Anything).Return(blockingPumpRun{}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "run-1", queryPumpDispatch).
		Return(undecodableAnswer{err: errors.New("payload decode: unknown field \"decidedd\"")}, nil)
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "run-1").
		Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING), nil).Maybe()

	_, err := executeWithin(t, newTestConstructionManager(mc), pid, 2*time.Second)
	ce := asConstructionError(t, err)
	if !strings.Contains(ce.Error(), "payload decode") || strings.Contains(ce.Error(), pumpStillDecidingDetail) {
		t.Fatalf("want the real decode error surfaced at once, got %v", err)
	}
}

// FIX ROUND 4, item 3. Every Query / Describe RPC carries its own pumpRPCTimeout, so a
// single HUNG RPC cannot defeat the wall-clock budgets (they are only checked between
// RPCs). Both RPCs here block until their context is done — with the caller's context
// never cancelled, an unbounded RPC would hang forever. The Query times out into the
// failing-query path, the Describe into "not known closed", and the call ends with the
// bounded fallback's Infrastructure error well inside the guard.
func Test_ExecuteNextActivity_HungRPCs_AreBoundedPerCall(t *testing.T) {
	withPumpPollBudgets(t, 30*time.Second, 100*time.Millisecond, 20*time.Millisecond, 10*time.Millisecond)
	withPumpRPCTimeout(t, 20*time.Millisecond)
	pid := ProjectID(uuid.NewString())
	wfID := string(pid) + ":nextActivity"
	blockUntilDone := func(args mock.Arguments) { <-args.Get(0).(context.Context).Done() }

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, executionKindPump, mock.Anything).Return(blockingPumpRun{}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "run-1", queryPumpDispatch).
		Run(blockUntilDone).Return(nil, context.DeadlineExceeded)
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "run-1").
		Run(blockUntilDone).Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), context.DeadlineExceeded)

	_, err := executeWithin(t, newTestConstructionManager(mc), pid, 3*time.Second)
	if ce := asConstructionError(t, err); ce.Kind != fwmanager.Infrastructure {
		t.Fatalf("want the bounded fallback's Infrastructure error, got %s: %v", ce.Kind, err)
	}
}

// FIX ROUND 3, Minor 1 — the budget-exhausted outcome. The Query keeps answering "not
// decided" and the run is still RUNNING when the poll budget expires: the façade returns
// PROMPTLY with the distinguishable still-deciding Detail, instead of waiting out the
// terminal budget into a generic timeout. (Still an Infrastructure Kind on the wire until
// the contract ruling — see pumpDispatchWaitBudget's OPEN note.)
func Test_ExecuteNextActivity_StillDecidingAtBudget_ReturnsDistinguishableOutcome(t *testing.T) {
	withPumpPollBudgets(t, 100*time.Millisecond, 2*time.Second, 5*time.Second, 10*time.Millisecond)
	pid := ProjectID(uuid.NewString())
	wfID := string(pid) + ":nextActivity"

	mc := &temporalmocks.Client{}
	mc.On("ExecuteWorkflow", mock.Anything, mock.Anything, executionKindPump, mock.Anything).Return(blockingPumpRun{}, nil)
	mc.On("QueryWorkflow", mock.Anything, wfID, "run-1", queryPumpDispatch).
		Return(fakeEncodedPumpDispatch{d: pumpDispatch{Decided: false}}, nil)
	mc.On("DescribeWorkflowExecution", mock.Anything, wfID, "run-1").
		Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING), nil)

	_, err := executeWithin(t, newTestConstructionManager(mc), pid, 2*time.Second) // a 5s terminal wait would blow this guard
	if ce := asConstructionError(t, err); !strings.Contains(ce.Error(), pumpStillDecidingDetail) {
		t.Fatalf("want the distinguishable still-deciding outcome, got %v", err)
	}
}

// ---- RunReplanSweep (op 2.2) ------------------------------------------------

func Test_RunReplanSweep_EmptyTickID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	_, err := m.RunReplanSweep(fwmanager.Context{Context: context.Background()}, nil, "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_RunReplanSweep_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	nilID := ProjectID("")
	_, err := m.RunReplanSweep(fwmanager.Context{Context: context.Background()}, &nilID, "tick-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an explicit nil projectId, got %s", got)
	}
}

// ---- PauseProject (op 2.3) --------------------------------------------------

func Test_PauseProject_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.PauseProject(fwmanager.Context{Context: context.Background()}, ProjectID(""), "reason")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_PauseProject_EmptyReason(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.PauseProject(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an empty pause reason, got %s", got)
	}
}

// ---- OverrideActivity (op 2.4) ----------------------------------------------

func Test_OverrideActivity_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(""), "C-1", ActivityOverride{Kind: OverrideRetry})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_OverrideActivity_EmptyActivityID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "", ActivityOverride{Kind: OverrideRetry})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an empty activityId, got %s", got)
	}
}

func Test_OverrideActivity_UnknownOverrideKind(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	err := m.OverrideActivity(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), "C-1", ActivityOverride{Kind: OverrideUnknown})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an unknown override kind, got %s", got)
	}
}

// ---- GetSessionState (op 2.5) -----------------------------------------------

func Test_GetSessionState_EmptyProjectID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID(""), nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_GetSessionState_EmptyActivityID(t *testing.T) {
	m := newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, 0, "", nil)
	empty := ActivityID("")
	_, err := m.GetSessionState(fwmanager.Context{Context: context.Background()}, ProjectID(uuid.NewString()), &empty)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for an explicit empty activityId, got %s", got)
	}
}

// ---- workflow id derivation -------------------------------------------------

func Test_WorkflowIDDerivation(t *testing.T) {
	pid := ProjectID("11111111-1111-1111-1111-111111111111")

	if got := pumpWorkflowID(pid); got != string(pid)+":nextActivity" {
		t.Fatalf("pump id: %q", got)
	}
	if got := constructActivityWorkflowID(pid, "C-9"); got != string(pid)+":C-9" {
		t.Fatalf("child id: %q", got)
	}
	if got := replanSweepWorkflowID(&pid, "t2"); got != string(pid)+":replanSweep:t2" {
		t.Fatalf("sweep id: %q", got)
	}
	if got := replanSweepWorkflowID(nil, "t3"); got != ":all:replanSweep:t3" {
		t.Fatalf("all-sweep id: %q", got)
	}
	if got := pauseTargetWorkflowID(pid); got != string(pid)+":construction" {
		t.Fatalf("pause target id: %q", got)
	}
}

// ---- OverrideKind / activityKind String coverage --------------

func Test_OverrideKind_String(t *testing.T) {
	cases := map[OverrideKind]string{
		OverrideTakeover: "Takeover", OverrideRetry: "Retry",
		OverrideSkip: "Skip", OverrideReassign: "Reassign", OverrideUnknown: "Unknown",
	}
	for k, want := range cases {
		if got := overrideKindName(k); got != want {
			t.Fatalf("overrideKindName(%d) = %q, want %q", int(k), got, want)
		}
	}
}

// ---- SubmitPhaseDecision (op 2.6) -------------------------------------------

func TestSubmitPhaseDecision_SignalsActivityWorkflowWithPhase(t *testing.T) {
	fc := &fakeTemporalClient{session: awaitingAt("detailed_design")}
	m := newTestConstructionManager(fc)
	if err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseApprove, nil); err != nil {
		t.Fatalf("SubmitPhaseDecision: %v", err)
	}
	if fc.lastWorkflowID != "proj-1:C-Orders" || fc.lastSignalName != signalPhaseDecision {
		t.Fatalf("wfID=%q signal=%q", fc.lastWorkflowID, fc.lastSignalName)
	}
	sig, ok := fc.lastSignalArg.(phaseDecisionSignal)
	if !ok || sig.Phase != "detailed_design" || sig.Decision != PhaseApprove {
		t.Fatalf("payload=%+v", fc.lastSignalArg)
	}
}

func TestSubmitPhaseDecision_SendBackRequiresFeedbackNotes(t *testing.T) {
	m := newTestConstructionManager(&fakeTemporalClient{})
	err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseSendBack, nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for SendBack without feedback, got %s", got)
	}
	err = m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseSendBack, &ReviewFeedback{Notes: ""})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for SendBack with empty notes, got %s", got)
	}
	// M3: a whitespace-only note is an empty one, as it is for an override.
	for _, blank := range []string{" ", "\n\t  \r\n", "\u00a0 "} {
		err = m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseSendBack, &ReviewFeedback{Notes: blank})
		if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
			t.Fatalf("want ContractMisuse for SendBack with the whitespace-only note %q, got %s", blank, got)
		}
	}
}

func TestSubmitPhaseDecision_EmptyProjectID(t *testing.T) {
	m := newTestConstructionManager(nil)
	err := m.SubmitPhaseDecision(testCtx(), "", "C-Orders", "detailed_design", PhaseApprove, nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func TestSubmitPhaseDecision_EmptyActivityID(t *testing.T) {
	m := newTestConstructionManager(nil)
	err := m.SubmitPhaseDecision(testCtx(), "proj-1", "", "detailed_design", PhaseApprove, nil)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// classifyForTest runs the production classifier the pump runs, so a hydrate test
// exercises the same (type, variant) resolution dispatch does rather than a
// hand-picked pair.
func classifyForTest(t *testing.T, id string, item projectstate.ActivityItem) (projectstate.ActivityType, projectstate.TestingVariant) {
	t.Helper()
	typ, variant, err := projectstate.ClassifyActivity(id, item.WorkerClass, item.Coding)
	if err != nil {
		t.Fatalf("ClassifyActivity(%s): %v", id, err)
	}
	return typ, variant
}

func TestHydrateConstructionActivity_ServicePhases(t *testing.T) {
	item := projectstate.ActivityItem{WorkerClass: "junior-developer", Coding: true, EffortDays: 5}
	typ, variant := classifyForTest(t, "C-Orders", item)
	got := hydrateConstructionActivity("C-Orders", item, nil, typ, variant)
	if got.Type != projectstate.ActivityTypeService {
		t.Fatalf("Type = %v, want Service", got.Type)
	}
	want := []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseDetailedDesign,
		projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	}
	if len(got.Phases) != len(want) {
		t.Fatalf("phases len = %d, want %d", len(got.Phases), len(want))
	}
	for i := range want {
		if got.Phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, got.Phases[i], want[i])
		}
	}
}

// The client app activity names the CLIENT it builds as its component, so dispatch stamps
// the Client layer through the ordinary component lookup the pump runs (lookupComponent)
// — not the layer of a manager it screens, which is what the per-manager SPA activities
// D9 removed used to carry as componentId.
func TestHydrateConstructionActivity_ClientAppStampsTheClientLayer(t *testing.T) {
	proj := projectstate.Project{SystemDesign: projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.System{Components: []projectstate.Component{
			{ID: "billing-manager", Layer: projectstate.LayerManager},
			{ID: "web-client", Layer: projectstate.LayerClient},
		}},
	}}
	item := projectstate.ActivityItem{Name: "U-SPA-web-client", WorkerClass: "junior-developer", Coding: true, ComponentID: "web-client"}
	comp := lookupComponent(proj, item.ComponentID)
	if comp == nil {
		t.Fatal("lookupComponent did not resolve web-client against the committed System")
	}
	typ, variant := classifyForTest(t, item.Name, item)
	got := hydrateConstructionActivity(item.Name, item, comp, typ, variant)
	if got.ComponentID != "web-client" || got.Layer != "client" {
		t.Errorf("(ComponentID, Layer) = (%q, %q), want (web-client, client)", got.ComponentID, got.Layer)
	}
	if got.Type != projectstate.ActivityTypeFrontend {
		t.Errorf("Type = %v, want Frontend", got.Type)
	}
}

func TestHydrateConstructionActivity_TestingPlanIsThreePhases(t *testing.T) {
	item := projectstate.ActivityItem{WorkerClass: "test-engineer"}
	typ, variant := classifyForTest(t, "N-STP", item)
	got := hydrateConstructionActivity("N-STP", item, nil, typ, variant)
	if got.Type != projectstate.ActivityTypeTesting || got.Variant != projectstate.TestVariantPlan {
		t.Fatalf("(Type, Variant) = (%v, %v), want (Testing, Plan)", got.Type, got.Variant)
	}
	want := []projectstate.ActivityMethodPhase{
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration,
	}
	if len(got.Phases) != len(want) {
		t.Fatalf("N-STP phases len = %d, want %d", len(got.Phases), len(want))
	}
	for i := range want {
		if got.Phases[i] != want[i] {
			t.Errorf("phase[%d] = %q, want %q", i, got.Phases[i], want[i])
		}
	}
}

func TestDispatchInputsForIncludesCommand(t *testing.T) {
	// A service construction phase -> service-construction command.
	in := dispatchInputsFor(pipelineSpec{
		ActivityID:  "C-BM",
		ComponentID: "billingManager",
		Phase:       "construction",
	})
	if in["command"] != "service-construction" {
		t.Errorf("command = %q, want service-construction", in["command"])
	}
	if in["activity_id"] != "C-BM" || in["component_id"] != "billingManager" {
		t.Errorf("activity/component passthrough wrong: %+v", in)
	}
	if in["phase"] != "construction" {
		t.Errorf("phase = %q, want construction", in["phase"])
	}

	// A testing harness detailed-design phase -> testing-harness-detailed-design.
	// The pair is CARRIED on the spec (classified once by the pump), never re-derived
	// from the id here — an N-* id alone no longer implies testing.
	in2 := dispatchInputsFor(pipelineSpec{
		ActivityID: "N-STH",
		Phase:      "detailed_design",
		Type:       projectstate.ActivityTypeTesting,
		Variant:    projectstate.TestVariantHarness,
	})
	if in2["command"] != "testing-harness-detailed-design" {
		t.Errorf("command = %q, want testing-harness-detailed-design", in2["command"])
	}

	// The N-ENV defect: an infra activity carries the DEPLOYMENT pair, so the same
	// N- prefix that used to force a testing command now produces a deployment one.
	in3 := dispatchInputsFor(pipelineSpec{
		ActivityID: "N-ENV",
		Phase:      "construction",
		Type:       projectstate.ActivityTypeDeployment,
	})
	if in3["command"] != "deployment-construction" {
		t.Errorf("command = %q, want deployment-construction", in3["command"])
	}
}

// constructWorkflowBody reads archistrator's OWN aiarch-construct.yml, located relative
// to this test file (robust to the test's working directory). It is the static workflow
// the construction dispatch (dispatchInputsFor) drives.
func constructWorkflowBody(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// server/internal/manager/construction → repo root is four levels up.
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", ".github", "workflows", "aiarch-construct.yml")
	b, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative test fixture path
	if err != nil {
		t.Fatalf("read aiarch-construct.yml: %v", err)
	}
	return string(b)
}

// TestConstructWorkflowWiresStateMcp asserts aiarch-construct.yml wires the aiarch-state
// MCP server (the twin of sourcecontrol.TestDesignWorkflowWiresStateMcp): it obtains the
// binary, bakes the construction ambient session context as env on the MCP process, and
// passes --mcp-config to claude-code-action.
func TestConstructWorkflowWiresStateMcp(t *testing.T) {
	body := constructWorkflowBody(t)

	// Obtains the MCP binary — `go install <module>@<pin>` of the published state-MCP
	// (the construct workflow runs from a seated scaffold with no in-repo server source, so
	// it installs the pinned module rather than building ./cmd/aiarch-state-mcp). Mirrors
	// the design twin (sourcecontrol.TestDesignWorkflowWiresStateMcp).
	if !strings.Contains(body, `go install github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp@"${AIARCH_STATE_MCP_PIN}"`) {
		t.Errorf("construct workflow must `go install` the pinned aiarch-state MCP server; got:\n%s", body)
	}

	// The MCP config bakes the CONSTRUCTION ambient context.
	for _, key := range []string{
		"AIARCH_PROJECT_ID", "AIARCH_JOB_MODE", "AIARCH_COMPONENT_ID",
		"AIARCH_ACTIVITY_ID", "AIARCH_TARGET_BRANCH", "AIARCH_STATE_ROOT",
	} {
		if !strings.Contains(body, key) {
			t.Errorf("MCP config must set %s on the aiarch-state server process", key)
		}
	}

	// Construct job mode (the construction session context is keyed by component/activity,
	// not an artifact kind). The config is built by `jq -n` from env — never a heredoc of
	// JSON, which a free-text operator note could break (amendment §C.1).
	if !strings.Contains(body, `AIARCH_JOB_MODE: "construct"`) {
		t.Error("MCP config must set AIARCH_JOB_MODE to construct")
	}
	if !strings.Contains(body, "jq -n") || strings.Contains(body, "<<EOF") {
		t.Error("the MCP config must be built with jq -n from env, never a heredoc")
	}

	// --mcp-config wires the server into the Claude CLI.
	if !strings.Contains(body, "--mcp-config") {
		t.Error("claude-code-action must wire the aiarch-state MCP server via --mcp-config")
	}
}

// TestConstructWorkflowKeepsLoadBearingAnchors guards the dispatch contract the MCP wiring
// must not have disturbed: the idempotency run-name anchor and the additive dispatch
// inputs.
func TestConstructWorkflowKeepsLoadBearingAnchors(t *testing.T) {
	body := constructWorkflowBody(t)
	for _, anchor := range []string{
		"run-name: aiarch-cp-${{ inputs.idempotency_token }}",
		"idempotency_token:",
		"activity_id:",
		"component_id:",
		"/${{ inputs.command }} ${{ inputs.component_id }} ${{ inputs.activity_id }}",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("construct workflow lost a load-bearing anchor: %q", anchor)
		}
	}
}

// repoRoot returns the repository root, located relative to this test file.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// server/internal/manager/construction → repo root is four levels up.
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
}

// TestConstructionPromptsUseStateTools asserts the central the-method-project-state
// skill mandates the aiarch-state write tools, and every construction /
// detailed-design command carries the "state changes through the tools" note. This
// is the prompt-side guard that construction agents record state THROUGH the tools,
// not by hand-editing project.json.
func TestConstructionPromptsUseStateTools(t *testing.T) {
	root := repoRoot(t)

	skill := readFileT(t, filepath.Join(root, ".claude", "skills", "the-method-project-state", "SKILL.md"))
	for _, want := range []string{
		"STATE CHANGES GO THROUGH THE `aiarch-state` MCP TOOLS",
		"recordServiceContract",
		"recordPhaseArtifact",
		"recordTestingState",
		"publishDraft",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("the-method-project-state skill must reference %q", want)
		}
	}

	cmdDir := filepath.Join(root, ".claude", "commands")
	var commands []string
	for _, f := range []string{"deployment", "documentation", "frontend", "service", "testing-harness", "testing-perf", "testing-qa"} {
		commands = append(commands, f+"-detailed-design.md")
	}
	for _, f := range []string{"deployment", "documentation", "frontend", "service", "testing-harness", "testing-perf", "testing-plan", "testing-qa", "testing-systemtest"} {
		commands = append(commands, f+"-construction.md")
	}
	const noteMarker = "State changes go through the `aiarch-state` MCP tools"
	for _, c := range commands {
		body := readFileT(t, filepath.Join(cmdDir, c))
		if !strings.Contains(body, noteMarker) {
			t.Errorf("%s must carry the aiarch-state tool note", c)
		}
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- fixed repo-relative test fixture path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// fakeReviewPolicyTransition is a minimal fake satisfying projectstate.ConstructionTransitionAccess
// for UpdateReviewPolicy tests. It embeds the interface (unimplemented methods panic
// if reached — intentional) and only implements the two verbs UpdateReviewPolicy exercises:
// ReadProject (to supply the current version) and RecordReviewPolicy (the write verb).
type fakeReviewPolicyTransition struct {
	projectstate.ConstructionTransitionAccess
	version    projectstate.Version
	lastPolicy *projectstate.ReviewPolicy
}

func (f *fakeReviewPolicyTransition) RecordReviewPolicy(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, policy projectstate.ReviewPolicy, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.version++
	f.lastPolicy = &policy
	return f.version, nil
}

// TestUpdateReviewPolicy asserts that UpdateReviewPolicy maps the ReviewPolicyInput
// through projectstate.ReviewPolicyFromGateIDs and calls RecordReviewPolicy with the
// resulting typed ReviewPolicy. The ad-hoc gate id "svc-contract" maps to
// MethodPhaseDetailedDesign for the "service" activity type; after the call,
// RequiresHuman("service", MethodPhaseDetailedDesign) must be true on the persisted policy.
func TestUpdateReviewPolicy(t *testing.T) {
	fake := &fakeReviewPolicyTransition{version: 7}
	// The version read moved to the base projectStateAccess port (constructionTransitionAccess.ReadProject
	// pruned); RecordReviewPolicy stays on constructionTransition.
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
			return projectstate.Project{Version: 7}, nil
		},
	}
	m := newConstructionManager(nil, ps, nil, nil, nil, nil, nil, fake, nil, nil, nil, nil, 0, "", nil)

	err := m.UpdateReviewPolicy(testCtx(), "proj-1", ReviewPolicyInput{
		GatedPhasesByType: map[string][]string{
			"service": {"svc-contract"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateReviewPolicy: %v", err)
	}
	if fake.lastPolicy == nil {
		t.Fatal("RecordReviewPolicy was not called")
	}
	// "svc-contract" is the ad-hoc gate id that maps to MethodPhaseDetailedDesign
	// via projectstate.gateIDToPhase; ReviewPolicyFromGateIDs must translate it.
	if !fake.lastPolicy.RequiresHuman("service", projectstate.MethodPhaseDetailedDesign) {
		t.Fatalf("expected service/detailed_design to require human, got policy=%+v", fake.lastPolicy)
	}
}

// =============================================================================
// C-MCN-GIT wiring tests. They drive the REAL constructionManager per-activity
// workflow (ConstructActivityWorkflow + its real Activity wrappers + the real
// rail→record sequencing in gitforward.go) over the Temporal in-memory test env,
// with the git-forward slice WIRED. They assert the resulting ActivityGit head-state
// at each lifecycle transition (branch-open → CI → arch-approved → merged) through the
// real Manager seam, and the idempotent-retry invariant (re-running a record step does
// NOT double-record / corrupt the row).
//
// Per [[project_aiarch_testing_no_bdd]] (black-box, wire-level, anti-cheat §7): the
// observable is the recorded head-state side effects on a faithful store — NOT internal
// calls. The GitStatus seam is backed by stubGitStatus, an in-memory store that
// implements the SAME partial-map-key upsert + idempotency-dedup semantics the real
// *projectstate.GitStore proves under gitactivity_test.go (the real store is exercised
// there against a throwaway on-disk repo; here the Manager WIRING is the system under
// test). The rail is a controllable double returning scripted opaque handles + CI.
// =============================================================================

// ---- stubRail: a controllable IPullRequestRail double -----------------------

// stubRail returns scripted opaque handles + a scripted CI rollup, and records every
// call so the test can assert the rail was driven in the expected order. It honors the
// frozen rail surface (opaque returns; the Manager records them).
type stubRail struct {
	mu sync.Mutex

	prRef    string
	ciRollup sourcecontrol.CheckState
	merged   bool

	opened    []string // branch names OpenBranch saw
	prOpened  []sourcecontrol.PullRequestSpec
	statuses  int
	reviews   []sourcecontrol.ReviewSubmission
	merges    int
	credMints int

	// syncs counts SyncManagedScaffold calls (B1.4 / C.1.4); syncErr, when set, fails
	// every one; order, when set, receives "sync" on each.
	syncs   int
	syncErr error
	order   *callLog
}

func (r *stubRail) GetInstallationToken(_ fwra.Context, _ sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credMints++
	return sourcecontrol.RepoCredential{Bytes: []byte("tok")}, nil
}

// OpenBranch returns the ZERO BranchRef: the frozen rail surface exposes
// RepoRefFromString / PullRequestRefFromString but NO BranchRefFromString, so a test
// double cannot mint a non-empty opaque BranchRef. The Manager records whatever
// BranchRef.String() yields (here ""); in production the real rail returns a populated
// handle. Assertions therefore key on the branch NAME + the PR ref (both
// test-constructable), NOT on BranchRef content. (Noted as a minor contract gap in
// C-MCN-GIT.md — non-blocking; the wiring records the rail's return verbatim.)
func (r *stubRail) OpenBranch(_ fwra.Context, _ sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential) (sourcecontrol.BranchRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opened = append(r.opened, string(branch))
	return sourcecontrol.BranchRef(""), nil
}

func (r *stubRail) OpenPullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prOpened = append(r.prOpened, spec)
	return sourcecontrol.PullRequestRefFromString(r.prRef), nil
}

func (r *stubRail) GetPullRequestStatus(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses++
	return sourcecontrol.PullRequestStatus{CheckRollup: r.ciRollup, ApprovalCount: 1, Mergeable: true}, nil
}

func (r *stubRail) PostReview(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, review sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reviews = append(r.reviews, review)
	return nil
}

func (r *stubRail) MergePullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.MergeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.merges++
	return sourcecontrol.MergeResult{Commit: "main-sha", Merged: r.merged}, nil
}

// The remaining SourceControlAccess ops are outside the PR-rail lifecycle the git-forward
// spine drives; the stub satisfies the full contract with inert implementations so it can
// back the GENERATED rail Activities.
func (r *stubRail) AdoptProjectRepo(_ fwra.Context, _ sourcecontrol.RepoAdoptionSpec) (sourcecontrol.RepoRef, error) {
	return sourcecontrol.RepoRef(""), nil
}

func (r *stubRail) CommitManagedFiles(_ fwra.Context, _ sourcecontrol.RepoRef, _ []sourcecontrol.ManagedFile, _ sourcecontrol.RepoCredential) (sourcecontrol.CommitRef, error) {
	return sourcecontrol.CommitRef(""), nil
}

func (r *stubRail) ConfigureBranchProtection(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) error {
	return nil
}

func (r *stubRail) InstallAuthorizeApp(_ fwra.Context, _ sourcecontrol.AccountRef) (sourcecontrol.Installation, error) {
	return sourcecontrol.Installation(""), nil
}

func (r *stubRail) SyncManagedScaffold(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncs++
	if r.order != nil {
		r.order.add("sync")
	}
	if r.syncErr != nil {
		return false, r.syncErr
	}
	return false, nil
}

var _ sourcecontrol.SourceControlAccess = (*stubRail)(nil)

// ---- stubGitStatus: an in-memory git head-state mirror ----------------------

// stubGitStatus faithfully reproduces the real GitStore's per-activity record
// semantics: a partial-map-key upsert keyed by activityID, the PR-tolerant branch-open
// fusing, the CICheck=Pending birth, and dedup-first idempotency on idempotencyKey (a
// retried key returns the prior Version with NO second apply). It exposes the recorded
// rows so the test asserts the head-state the real workflow produced.
type stubGitStatus struct {
	mu sync.Mutex

	rows    map[string]projectstate.ActivityGitStatus
	cons    map[string]projectstate.ActivityConstructionStatus // per-activity construction lifecycle (Task 3)
	version projectstate.Version
	dedup   map[fwra.IdempotencyKey]projectstate.Version
	applies int // count of NON-deduped (real) applies — proves no double-apply
}

func newStubGitStatus(seed projectstate.Version) *stubGitStatus {
	return &stubGitStatus{
		rows:    map[string]projectstate.ActivityGitStatus{},
		cons:    map[string]projectstate.ActivityConstructionStatus{},
		version: seed,
		dedup:   map[fwra.IdempotencyKey]projectstate.Version{},
	}
}

// apply is the shared upsert path: dedup-first, then a partial map-key mutation +
// version bump. It mirrors gitstore.applyMutation (dedup-first; modeRequireExisting is
// irrelevant here since the project is seeded).
func (s *stubGitStatus) apply(key fwra.IdempotencyKey, activityID string, mutate func(g *projectstate.ActivityGitStatus)) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil // dedup-first: no second apply
	}
	s.applies++
	g := s.rows[activityID]
	g.ActivityID = activityID
	mutate(&g)
	s.rows[activityID] = g
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) RecordActivityBranchOpened(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID, branch, branchRef, prRef, crLabel string, isRevert bool, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) {
		g.BranchName = branch
		g.BranchRef = branchRef
		if prRef != "" {
			g.PullRequestRef = prRef
		}
		if crLabel != "" {
			g.CRLabel = crLabel
		}
		if isRevert {
			g.IsRevert = true
		}
		// CICheck=Pending on first birth (real store sets it when first).
		if g.CICheck == 0 {
			g.CICheck = projectstate.CICheckPending
		}
	})
}

func (s *stubGitStatus) RecordActivityCIObserved(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, ci projectstate.CICheckState, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.CICheck = ci })
}

func (s *stubGitStatus) RecordActivityArchApproved(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.ArchApproved = true })
}

func (s *stubGitStatus) RecordActivityMerged(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.Merged = true })
}

func (s *stubGitStatus) RecordActivityStarted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, typ projectstate.ActivityType, variant projectstate.TestingVariant, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil
	}
	s.applies++
	c := s.cons[activityID]
	c.ActivityID = activityID
	c.Phase = projectstate.ActivityConstructionRunning
	c.Type = typ
	c.Variant = variant
	s.cons[activityID] = c
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) RecordActivityCompleted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil
	}
	s.applies++
	c := s.cons[activityID]
	c.ActivityID = activityID
	c.Phase = projectstate.ActivityConstructionDone
	s.cons[activityID] = c
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) row(activityID string) (projectstate.ActivityGitStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.rows[activityID]
	return g, ok
}

// constructionPhase returns the recorded construction lifecycle phase for activityID.
func (s *stubGitStatus) constructionPhase(activityID string) (projectstate.ActivityConstructionPhase, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cons[activityID]
	return c.Phase, ok
}

var _ projectstate.GitActivityStatusAccess = (*stubGitStatus)(nil)

// ---- helpers ----------------------------------------------------------------

// registerGenRail registers the GENERATED PR-rail Activities (backed by the stub rail)
// under their generated registered names — the names the generated invoker surface
// (wf.Acts.Rail*) dispatches by. The workflow reaches the rail only through those invokers.
func registerGenRail(env *testsuite.TestWorkflowEnvironment, rail sourcecontrol.SourceControlAccess) {
	acts := &genActivities{Rail: rail}
	env.RegisterActivityWithOptions(acts.RailGetInstallationToken, activity.RegisterOptions{Name: "sourceControlAccess.getInstallationToken"})
	env.RegisterActivityWithOptions(acts.RailOpenBranch, activity.RegisterOptions{Name: "sourceControlAccess.openBranch"})
	env.RegisterActivityWithOptions(acts.RailOpenPullRequest, activity.RegisterOptions{Name: "sourceControlAccess.openPullRequest"})
	env.RegisterActivityWithOptions(acts.RailGetPullRequestStatus, activity.RegisterOptions{Name: "sourceControlAccess.getPullRequestStatus"})
	env.RegisterActivityWithOptions(acts.RailPostReview, activity.RegisterOptions{Name: "sourceControlAccess.postReview"})
	env.RegisterActivityWithOptions(acts.RailMergePullRequest, activity.RegisterOptions{Name: "sourceControlAccess.mergePullRequest"})
	env.RegisterActivityWithOptions(acts.RailSyncManagedScaffold, activity.RegisterOptions{Name: "sourceControlAccess.syncManagedScaffold"})
}

// registerConstructGit registers the per-activity workflow + ALL activities including
// the git-forward ones — every one GENERATED (B8 + follow-up): the pipeline/
// designSession-read/projectState-version/constructionTransition/rail surfaces (via
// fakes) and the gitActivityStatusAccess Record* activities backed by git (NOT ps —
// the git-forward tests wire wfDeps.GitStatus to a SEPARATE stubGitStatus store,
// distinct from ps, so the registration backing must match; this is deliberately NOT
// built by delegating to registerConstruct, which always backs gitActivityStatusAccess
// with ps).
func registerConstructGit(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, git *stubGitStatus, rail sourcecontrol.SourceControlAccess) {
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	registerGenPipeline(env, &fakePipeline{phase: PipelineSucceeded})
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
	registerGenGitStatus(env, git)
	registerGenRail(env, rail)
}

// gitWiredWorkflows builds a workflows with the git-forward slice wired to the supplied
// rail + git store, a fixed repo resolver, and the happy-path engine fakes. The rail is
// reached through the generated invoker surface (Acts); RailEnabled + the repo resolver +
// the GitStatus mirror are what light up the PR-rail lifecycle.
func gitWiredWorkflows(_ *fakeProjectState, rail *stubRail, git *stubGitStatus, mergeable bool) *workflows {
	rail.merged = mergeable
	d := wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		// git-forward slice wired: the rail is registered as GENERATED Activities and
		// gated on RailEnabled; the GitStatus mirror + repo resolver light the lifecycle.
		RailEnabled: true,
		GitStatus:   git,
		Repo: func(_ ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo-1"), true
		},
	}
	return newWorkflows(d)
}

// gitSampleActivity carries a cr-NN label + revert flag so the recorded row's CR fields
// are asserted end-to-end.
func gitSampleActivity() constructionActivity {
	a := sampleActivity()
	a.ActivityID = "C-MST"
	a.CRLabel = "cr-021"
	return a
}

// ---- Tests ------------------------------------------------------------------

// gitLifecycleAssertRailDriven asserts the full-lifecycle rail choreography for the
// C-MST activity: branch + PR opened (with the cr label riding in Hints), one +1
// (Approve) relayed, one merge performed.
func gitLifecycleAssertRailDriven(t *testing.T, rail *stubRail) {
	t.Helper()
	if len(rail.opened) == 0 || rail.opened[0] != "activity/C-MST" {
		t.Fatalf("want OpenBranch(activity/C-MST), got %v", rail.opened)
	}
	if len(rail.prOpened) != 1 || rail.prOpened[0].Base != mainBranch {
		t.Fatalf("want one OpenPullRequest with base=main, got %+v", rail.prOpened)
	}
	if string(rail.prOpened[0].Hints) != "cr-021" {
		t.Fatalf("cr label must ride in PR Hints, got %q", rail.prOpened[0].Hints)
	}
	if len(rail.reviews) != 1 || rail.reviews[0].Verdict != sourcecontrol.ReviewApprove {
		t.Fatalf("want one +1 (Approve) relayed, got %+v", rail.reviews)
	}
	if rail.merges != 1 {
		t.Fatalf("want one MergePullRequest, got %d", rail.merges)
	}
}

// gitLifecycleAssertHeadStateRow asserts the recorded ActivityGit[C-MST] row mirrors
// the full lifecycle (branch/PR handles, CR label, CI success, arch +1, merged).
func gitLifecycleAssertHeadStateRow(t *testing.T, git *stubGitStatus) {
	t.Helper()
	g, ok := git.row("C-MST")
	if !ok {
		t.Fatal("ActivityGit[C-MST] was never recorded")
	}
	if g.BranchName != "activity/C-MST" || g.PullRequestRef != "pr-7" {
		t.Fatalf("branch/PR handles wrong: %+v", g)
	}
	if g.CRLabel != "cr-021" {
		t.Fatalf("CR label not recorded: %+v", g)
	}
	if g.CICheck != projectstate.CICheckSuccess {
		t.Fatalf("CICheck = %v, want Success", g.CICheck)
	}
	if !g.ArchApproved {
		t.Fatalf("ArchApproved not recorded: %+v", g)
	}
	if !g.Merged {
		t.Fatalf("Merged not recorded: %+v", g)
	}
}

// The full git-forward lifecycle records branch-open → CI(success) → arch-approved →
// merged onto the per-activity head-state, in order, through the real Manager workflow.
func Test_GitForward_FullLifecycle_RecordsHeadState(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 5, Phase: 2}, version: 5}
	rail := &stubRail{prRef: "pr-7", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true /*mergeable*/)
	registerConstructGit(env, wf, ps, git, rail)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-MST", Activity: gitSampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	// Rail was driven: branch + PR opened, CI read, +1 relayed, merge performed.
	gitLifecycleAssertRailDriven(t, rail)

	// Head-state mirror reflects the full lifecycle.
	gitLifecycleAssertHeadStateRow(t, git)

	// Task 3: the per-activity construction lifecycle recorded Running (started) then
	// Done (completed) through the same git-wired spine.
	phase, ok := git.constructionPhase("C-MST")
	if !ok {
		t.Fatal("ActivityConstruction[C-MST] was never recorded (started/completed)")
	}
	if phase != projectstate.ActivityConstructionDone {
		t.Fatalf("construction phase = %v, want Done (completed) after a happy-path spine", phase)
	}
}

// Task 3: the per-activity construction head-state flips to Running at the top of the
// spine and Done at the end — the records the pump's eligibility selection reads. A
// happy-path git-wired run leaves the activity Done so dependents unblock.
func Test_Construction_StartedThenCompleted_RecordedOnHeadState(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 5, Phase: 2}, version: 5}
	rail := &stubRail{prRef: "pr-1", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true /*mergeable*/)
	registerConstructGit(env, wf, ps, git, rail)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-MST", Activity: gitSampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	phase, ok := git.constructionPhase("C-MST")
	if !ok {
		t.Fatal("no construction head-state recorded")
	}
	if phase != projectstate.ActivityConstructionDone {
		t.Fatalf("want Done after a completed activity, got %v", phase)
	}
}

// A CI failure is mirrored as Failure (the dumb reflection); the lifecycle still
// proceeds to record the rest (CI is NOT a gate at this seam — the gate is
// interventionEngine, modeled by the merge mergeable flag, not CI).
func Test_GitForward_CIFailure_MirroredNotGated(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}, version: 1}
	rail := &stubRail{prRef: "pr-1", ciRollup: sourcecontrol.CheckFailure}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true)
	registerConstructGit(env, wf, ps, git, rail)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-CI", Activity: constructionActivity{ActivityID: "C-CI", Kind: activityKindConstruction, ComponentID: "c"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	g, ok := git.row("C-CI")
	if !ok {
		t.Fatal("row never recorded")
	}
	if g.CICheck != projectstate.CICheckFailure {
		t.Fatalf("CICheck = %v, want Failure mirrored", g.CICheck)
	}
}

// The dormant slice (rail/git unwired) leaves the spine untouched: no git rows recorded
// and the activity still completes the non-git records.
func Test_GitForward_Dormant_WhenUnwired(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}, version: 1}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		// no git-forward slice — RailEnabled=false, GitStatus/Repo nil.
	})
	registerConstruct(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-NO-GIT", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Non-git spine still recorded the binary exit.
	if len(ps.exited) != 1 {
		t.Fatalf("dormant slice must still complete the non-git spine, got exited=%v", ps.exited)
	}
}

// Idempotent retry: re-running the branch-opened record step with the SAME idempotency
// key returns the prior Version and does NOT double-apply (the dedup-first invariant the
// crash-safe workflow relies on). Driven directly through the Activity wrapper against
// the stub store (the workflow-replay path uses the same key derivation).
func Test_GitForward_RecordActivity_IdempotentRetry_NoDoubleApply(t *testing.T) {
	git := newStubGitStatus(10)
	ctx := context.Background()
	pid := projectstate.ProjectID(uuid.NewString())

	// Same idempotency key twice (a workflow retry re-runs the same Activity id).
	key := fwra.IdempotencyKey("wf-1:branch")
	v1, err := git.RecordActivityBranchOpened(fwra.Context{Context: ctx}, pid, 10, "C-MST", "activity/C-MST", "ref", "pr-1", "cr-021", false, projectstate.RepoCredential{}, key)
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	v2, err := git.RecordActivityBranchOpened(fwra.Context{Context: ctx}, pid, 0 /*stale*/, "C-MST", "activity/C-MST", "ref", "pr-1", "cr-021", false, projectstate.RepoCredential{}, key)
	if err != nil {
		t.Fatalf("idempotent re-record: %v", err)
	}
	if v1 != v2 {
		t.Fatalf("idempotent re-record version = %d, want prior %d", v2, v1)
	}
	if git.applies != 1 {
		t.Fatalf("dedup-first must apply exactly once, applied %d times (DOUBLE APPLY)", git.applies)
	}
}

// workflowGitForwardOrder asserts the rail+record order at the workflow level by
// checking that, on a successful lifecycle, the head-state row ends in the terminal
// (merged) state — i.e. every step ran and recorded in sequence without a gap.
func Test_GitForward_RecordsConvergeMonotonically(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}, version: 2}
	rail := &stubRail{prRef: "pr-9", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true)
	registerConstructGit(env, wf, ps, git, rail)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: pid, ActivityID: "C-MONO", Activity: constructionActivity{ActivityID: "C-MONO", Kind: activityKindConstruction, ComponentID: "c"},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	g, _ := git.row("C-MONO")
	if g.BranchName == "" || g.CICheck != projectstate.CICheckSuccess || !g.ArchApproved || !g.Merged {
		t.Fatalf("lifecycle did not converge through all record steps: %+v", g)
	}
	// At least 4 distinct applies (branch, ci, approve, merge) landed.
	if git.applies < 4 {
		t.Fatalf("want >=4 record applies across the lifecycle, got %d", git.applies)
	}
}

// makeCommittedNetwork builds a minimal committed ArtifactSlot holding a *projectstate.Network.
func makeCommittedNetwork(deps []projectstate.NetworkDependency) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: deps,
		},
	}
}

// makeCommittedNetworkWithMilestones builds a committed Network ArtifactSlot carrying
// both the authored activity dependencies AND the authored milestones — the
// combination the live benchmark drain (N-PERF/N-IT/N-UAT/N-HARD/N-DEPLOY/N-DOC all
// gating on M3/M4) needs to reproduce.
func makeCommittedNetworkWithMilestones(deps []projectstate.NetworkDependency, milestones []projectstate.NetworkMilestone) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.Network{
			Dependencies: deps,
			Milestones:   milestones,
		},
	}
}

// makeCommittedActivityList builds a minimal committed ArtifactSlot holding a *projectstate.ActivityList.
func makeCommittedActivityList(items []projectstate.ActivityItem) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model: &projectstate.ActivityList{
			Activities: items,
		},
	}
}

// makeCommittedSystemDesign builds a minimal committed ArtifactSlot holding a
// *projectstate.System with the given components — the authored-componentId lookup
// target nextEligibleActivity consults instead of ServiceContracts.
func makeCommittedSystemDesign(comps []projectstate.Component) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{
		Status: projectstate.ReviewCommitted,
		Model:  &projectstate.System{Components: comps},
	}
}

var todoComponents = []projectstate.Component{
	{ID: "todo-list-manager", Name: "TodoListManager", Layer: projectstate.LayerManager},
	{ID: "todo-owner-client", Name: "TodoOwnerClient", Layer: projectstate.LayerClient},
}

func projWithActivities(acts []projectstate.ActivityItem, deps []projectstate.NetworkDependency) projectstate.Project {
	return projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(deps),
		ActivityList: makeCommittedActivityList(acts),
		SystemDesign: makeCommittedSystemDesign(todoComponents),
	}
}

// The regression the whole change exists for: a fresh project with NO service
// contracts must still dispatch its first coding activity.
func TestNextEligibleActivity_DispatchesWithNoServiceContracts(t *testing.T) {
	proj := projWithActivities(
		[]projectstate.ActivityItem{{
			Name: "C-TLM", Title: "TodoListManager — settlement manager",
			Coding: true, EffortDays: 13, ComponentID: "todo-list-manager",
		}},
		[]projectstate.NetworkDependency{{Activity: "C-TLM", DependsOn: []string{}}},
	)
	// Deliberately nil: ServiceContracts must play no part in selection.
	proj.ServiceContracts = nil

	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ComponentID != "todo-list-manager" {
		t.Fatalf("want componentID todo-list-manager, got %q", sel.Activity.ComponentID)
	}
	if sel.Activity.Layer != "manager" {
		t.Fatalf("want layer hydrated to manager, got %q", sel.Activity.Layer)
	}
	if sel.Activity.Kind != activityKindConstruction {
		t.Fatalf("want activityKindConstruction, got %v", sel.Activity.Kind)
	}
}

// Nonstructural coding (ch.13 Table 13-2) and noncoding activities are LEGAL
// with no componentId and dispatch with an empty component_id.
func TestNextEligibleActivity_ComponentlessActivitiesDispatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		item projectstate.ActivityItem
	}{
		{"nonstructural coding", projectstate.ActivityItem{Name: "I-UC1", Title: "Integrate use case 1", Coding: true}},
		{"noncoding", projectstate.ActivityItem{Name: "N-STP", Title: "System Test Plan", WorkerClass: "test-engineer", Coding: false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := projWithActivities(
				[]projectstate.ActivityItem{tc.item},
				[]projectstate.NetworkDependency{{Activity: tc.item.Name, DependsOn: []string{}}},
			)
			sel := nextEligibleActivity(proj, eligibleDispatchable)
			if sel.Verdict != verdictDispatch {
				t.Fatalf("want verdictDispatch, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
			}
			if sel.Activity.ComponentID != "" {
				t.Fatalf("want empty componentID, got %q", sel.Activity.ComponentID)
			}
		})
	}
}

// The ONE blocked condition: a non-empty componentId naming no committed component.
func TestNextEligibleActivity_UnknownComponentIsBlocked(t *testing.T) {
	proj := projWithActivities(
		[]projectstate.ActivityItem{{
			Name: "C-TLM", Title: "TodoListManager", Coding: true, ComponentID: "todo-list-managr",
		}},
		[]projectstate.NetworkDependency{{Activity: "C-TLM", DependsOn: []string{}}},
	)
	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictBlocked {
		t.Fatalf("want verdictBlocked, got %v", sel.Verdict)
	}
	if sel.BlockedActivityID != "C-TLM" {
		t.Fatalf("want blocked activity C-TLM, got %q", sel.BlockedActivityID)
	}
	// The detail must name both the activity and the unresolvable id — it is the
	// only thing an operator sees in the console.
	if !strings.Contains(sel.BlockedReason, "C-TLM") || !strings.Contains(sel.BlockedReason, "todo-list-managr") {
		t.Fatalf("blocked reason must name the activity and the bad id, got %q", sel.BlockedReason)
	}
}

func TestNextEligibleActivity_NothingEligibleIsQuiescent(t *testing.T) {
	// A is authored into the ActivityList (a real, in-flight predecessor of B) so
	// this fixture stays realistic under the milestone-aware resolver: an id that
	// resolves to neither an activity nor a milestone is now a reported plan defect
	// (verdictBlocked), not silently "just not done yet". A is Running (not
	// NotStarted, so it is not itself a candidate this tick, and not Done, so it
	// does not satisfy B) — the fixture now represents "the only candidate's
	// dependency genuinely isn't satisfied yet" instead of leaning on an
	// unauthored id.
	proj := projWithActivities(
		[]projectstate.ActivityItem{
			{Name: "A", Title: "A", Coding: false},
			{Name: "B", Title: "B", Coding: false},
		},
		[]projectstate.NetworkDependency{
			{Activity: "A", DependsOn: []string{}},
			{Activity: "B", DependsOn: []string{"A"}},
		},
	)
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionRunning},
	}
	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("want verdictQuiescent, got %v", sel.Verdict)
	}
}

// TestNextEligibleActivity_Chain exercises the A→B→C network with progressively
// committed construction status entries.
func TestNextEligibleActivity_Chain(t *testing.T) {
	// Network: A has no deps; B dependsOn A; C dependsOn B.
	network := []projectstate.NetworkDependency{
		{Activity: "A", DependsOn: []string{}},
		{Activity: "B", DependsOn: []string{"A"}},
		{Activity: "C", DependsOn: []string{"B"}},
	}
	activities := []projectstate.ActivityItem{
		{Name: "A", Title: "A", EffortDays: 5, WorkerClass: "junior-developer", Coding: true, RiskBucket: 2, ComponentID: "comp-a"},
		{Name: "B", Title: "B", EffortDays: 3, WorkerClass: "junior-developer", Coding: true, RiskBucket: 1, ComponentID: "comp-b"},
		{Name: "C", Title: "C", EffortDays: 8, WorkerClass: "system-architect", Coding: false, RiskBucket: 3},
	}

	// Selection now resolves the authored ComponentID against the committed
	// systemDesign — ServiceContracts play no part. C is noncoding (no ComponentID).
	base := projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
		SystemDesign: makeCommittedSystemDesign([]projectstate.Component{
			{ID: "comp-a", Name: "A", Layer: projectstate.LayerManager},
			{ID: "comp-b", Name: "B", Layer: projectstate.LayerManager},
		}),
	}

	// ---- Case 1: empty ActivityConstruction → A is eligible (no deps). ----
	proj := base
	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("case 1: expected verdictDispatch, got %v", sel.Verdict)
	}
	if sel.Activity.ActivityID != "A" {
		t.Fatalf("case 1: expected A, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.EstimateDays != 5 {
		t.Fatalf("case 1: expected EstimateDays=5, got %f", sel.Activity.EstimateDays)
	}

	// ---- Case 2: A Done → B is eligible. ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
	}
	sel = nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("case 2: expected verdictDispatch, got %v", sel.Verdict)
	}
	if sel.Activity.ActivityID != "B" {
		t.Fatalf("case 2: expected B, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.EstimateDays != 3 {
		t.Fatalf("case 2: expected EstimateDays=3, got %f", sel.Activity.EstimateDays)
	}

	// ---- Case 3: A Done, B Running → nothing eligible (C blocked; B running). ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
		"B": {ActivityID: "B", Phase: projectstate.ActivityConstructionRunning},
	}
	sel = nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("case 3: expected verdictQuiescent, got %v", sel.Verdict)
	}

	// ---- Case 4: A Done, B Done → C is eligible. ----
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
		"B": {ActivityID: "B", Phase: projectstate.ActivityConstructionDone},
	}
	sel = nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("case 4: expected verdictDispatch, got %v", sel.Verdict)
	}
	if sel.Activity.ActivityID != "C" {
		t.Fatalf("case 4: expected C, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.EstimateDays != 8 {
		t.Fatalf("case 4: expected EstimateDays=8, got %f", sel.Activity.EstimateDays)
	}
}

// passedLedger is the attempt ledger cmd/backfill-attempts writes for an activity: one
// passed attempt per non-conditional task of the given phases, origin backfilled, with a
// basis. The row it goes on carries NO stored phase fields — that is the backfill's shape.
func passedLedger(activityID string, phases ...projectstate.ActivityMethodPhase) []projectstate.TaskAttempt {
	var out []projectstate.TaskAttempt
	for _, ph := range phases {
		for _, task := range projectstate.TasksForPhase(ph) {
			if projectstate.IsConditionalTask(task) {
				continue
			}
			out = append(out, ledgerAttempt(activityID, task, 1, projectstate.OutcomePassed))
		}
	}
	return out
}

func ledgerAttempt(activityID string, task projectstate.MethodTask, n int, outcome projectstate.TaskOutcome) projectstate.TaskAttempt {
	return projectstate.TaskAttempt{
		AttemptID: projectstate.AttemptID(activityID, task, n),
		Task:      task,
		Phase:     projectstate.PhaseForTask(task),
		Attempt:   n,
		Outcome:   outcome,
		Provenance: projectstate.AttemptProvenance{
			Origin: projectstate.OriginBackfilled,
			Basis:  "test fixture",
		},
	}
}

// ledgerChain is A → B: A is a Service build (coding, junior-developer, a real
// component), B a noncoding doc activity that depends on A.
func ledgerChain() projectstate.Project {
	return projWithActivities(
		[]projectstate.ActivityItem{
			{Name: "A", Title: "A", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"},
			{Name: "B", Title: "B", WorkerClass: "system-architect", Coding: false},
		},
		[]projectstate.NetworkDependency{
			{Activity: "A", DependsOn: []string{}},
			{Activity: "B", DependsOn: []string{"A"}},
		},
	)
}

var servicePhases = projectstate.ProfileFor(projectstate.ActivityTypeService, projectstate.TestVariantPlan).PhaseIDs()

// Task 7a (architect ruling Q2): a row the backfill wrote — a fully passed ledger and NO
// stored phase/phases/buildStatus — is Done to the pump. It satisfies its dependent, and
// it is never itself dispatched again. Before 7a the pump read the stored Phase (0,
// NotStarted), so it re-dispatched A and held B back.
func TestNextEligibleActivity_BackfilledRowSatisfiesItsDependentAndIsNeverDispatched(t *testing.T) {
	proj := ledgerChain()
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Attempts: passedLedger("A", servicePhases...)},
	}
	if a := proj.ActivityConstruction["A"]; a.Phase != projectstate.ActivityConstructionNotStarted || len(a.Phases) != 0 {
		t.Fatalf("fixture must be the backfill shape (no stored phase fields), got phase=%v phases=%d", a.Phase, len(a.Phases))
	}
	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch of B, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "B" {
		t.Fatalf("want B dispatched (A is Done by its ledger), got %q", sel.Activity.ActivityID)
	}
}

// A Skipped/TakenOver exit (RecordActivityExited) stores Phase=Done, BuildStatus=InReview
// and leaves the stored Phases incomplete. The stored Done must win — the pump wrote this
// row — so B is unblocked. Deriving from the incomplete Phases would read A as Running
// and strand B forever.
func TestNextEligibleActivity_ExitedSkippedRowStillUnblocksItsDependents(t *testing.T) {
	proj := ledgerChain()
	phases := make([]projectstate.PhaseCompletion, 0, len(servicePhases))
	for i, ph := range servicePhases {
		phases = append(phases, projectstate.PhaseCompletion{Phase: ph, Completed: i == 0})
	}
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {
			ActivityID:  "A",
			Phase:       projectstate.ActivityConstructionDone,
			BuildStatus: projectstate.BuildInReview,
			Phases:      phases,
		},
	}
	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch || sel.Activity.ActivityID != "B" {
		t.Fatalf("want B dispatched behind a Skipped-exited A, got verdict=%v activity=%q (blocked=%q)",
			sel.Verdict, sel.Activity.ActivityID, sel.BlockedReason)
	}
}

// A ledger whose latest code review was REJECTED: A passed requirements, test plan and
// detailed design and has built, but its construction gate is decided against it. A is
// Running — so it is not dispatched again — and it does not satisfy B. Nothing is
// eligible.
func TestNextEligibleActivity_RejectedGateRowIsRunningNotDispatchedAndBlocks(t *testing.T) {
	proj := ledgerChain()
	attempts := passedLedger("A",
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseDetailedDesign)
	attempts = append(attempts,
		ledgerAttempt("A", projectstate.TaskConstruction, 1, projectstate.OutcomePassed),
		ledgerAttempt("A", projectstate.TaskCodeReview, 1, projectstate.OutcomeRejected),
	)
	row := projectstate.ActivityConstructionStatus{ActivityID: "A", Attempts: attempts}
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{"A": row}

	if got, _ := projectstate.EffectiveConstructionPhase(row, proj.ActivityList.Model.(*projectstate.ActivityList).Activities[0]); got != projectstate.ActivityConstructionRunning {
		t.Fatalf("a ledger with a rejected latest gate must read Running, got %v", got)
	}
	// The pre-D1 rule (a pump that recorded no ledger-partial-resume marker).
	sel := nextEligibleActivity(proj, eligibleNotStarted)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("want verdictQuiescent (A running, B blocked on it), got %v activity=%q", sel.Verdict, sel.Activity.ActivityID)
	}
}

// Under the D1 rule the same row IS dispatched: no pump wrote it and it reads Running, so
// it is a ledger-partial row and resumes at its first incomplete phase — Construction,
// whose latest code review was rejected (App A: a failing review repeats the task). B
// still waits: A is not Done.
func TestNextEligibleActivity_RejectedGateRowResumesUnderTheLedgerPartialRule(t *testing.T) {
	proj := ledgerChain()
	attempts := passedLedger("A",
		projectstate.MethodPhaseRequirements, projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseDetailedDesign)
	attempts = append(attempts,
		ledgerAttempt("A", projectstate.TaskConstruction, 1, projectstate.OutcomePassed),
		ledgerAttempt("A", projectstate.TaskCodeReview, 1, projectstate.OutcomeRejected),
	)
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{"A": {ActivityID: "A", Attempts: attempts}}
	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch || sel.Activity.ActivityID != "A" {
		t.Fatalf("want A dispatched to resume, got verdict=%v activity=%q", sel.Verdict, sel.Activity.ActivityID)
	}
}

// TestNextEligibleActivity_MilestoneDependencySatisfied reproduces the live benchmark
// drain: an activity (N-DOC) depends on a MILESTONE (M4), not an activity. M4 is
// itself authored with its own dependsOn — I-UC-CONSULT and I-UC-AMEND, both Done.
// The milestone has genuinely been reached; before the fix, allDepsDone looked M4 up
// in the activity-status map, never found it (milestones never get a head-state
// record), and treated it as permanently unsatisfied — this is the exact false block
// that stranded 6 activities in the drain. This test FAILS on the old allDepsDone.
func TestNextEligibleActivity_MilestoneDependencySatisfied(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "I-UC-CONSULT", DependsOn: []string{}},
				{Activity: "I-UC-AMEND", DependsOn: []string{}},
				{Activity: "N-DOC", DependsOn: []string{"M4"}},
			},
			[]projectstate.NetworkMilestone{
				{ID: "M4", Name: "M4", DependsOn: []string{"I-UC-CONSULT", "I-UC-AMEND"}},
			},
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "I-UC-CONSULT", Coding: true},
			{Name: "I-UC-AMEND", Coding: true},
			{Name: "N-DOC", WorkerClass: "system-architect", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
		ActivityConstruction: map[string]projectstate.ActivityConstructionStatus{
			"I-UC-CONSULT": {ActivityID: "I-UC-CONSULT", Phase: projectstate.ActivityConstructionDone},
			"I-UC-AMEND":   {ActivityID: "I-UC-AMEND", Phase: projectstate.ActivityConstructionDone},
		},
	}

	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch (M4 reached via its own dependsOn), got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "N-DOC" {
		t.Fatalf("want N-DOC dispatched, got %q", sel.Activity.ActivityID)
	}
}

// TestNextEligibleActivity_MilestoneDependencyNotSatisfied is the mirror case: M4's
// own dependsOn are NOT all Done (I-UC-AMEND still pending), so the milestone has
// genuinely not been reached and N-DOC must stay ineligible.
func TestNextEligibleActivity_MilestoneDependencyNotSatisfied(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "I-UC-CONSULT", DependsOn: []string{}},
				{Activity: "I-UC-AMEND", DependsOn: []string{}},
				{Activity: "N-DOC", DependsOn: []string{"M4"}},
			},
			[]projectstate.NetworkMilestone{
				{ID: "M4", Name: "M4", DependsOn: []string{"I-UC-CONSULT", "I-UC-AMEND"}},
			},
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "I-UC-CONSULT", Coding: true},
			{Name: "I-UC-AMEND", Coding: true},
			{Name: "N-DOC", WorkerClass: "system-architect", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
		ActivityConstruction: map[string]projectstate.ActivityConstructionStatus{
			"I-UC-CONSULT": {ActivityID: "I-UC-CONSULT", Phase: projectstate.ActivityConstructionDone},
			"I-UC-AMEND":   {ActivityID: "I-UC-AMEND", Phase: projectstate.ActivityConstructionRunning},
		},
	}

	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("want verdictQuiescent (M4 not yet reached), got %v activity=%q", sel.Verdict, sel.Activity.ActivityID)
	}
}

// TestNextEligibleActivity_MilestoneDependsOnMilestone exercises the recursive case:
// M4 depends on M3 (a milestone, not an activity), and M3 depends on an activity
// that's Done. N-DOC (dependsOn M4) must resolve all the way through M4 -> M3 -> A.
func TestNextEligibleActivity_MilestoneDependsOnMilestone(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "A", DependsOn: []string{}},
				{Activity: "N-DOC", DependsOn: []string{"M4"}},
			},
			[]projectstate.NetworkMilestone{
				{ID: "M3", Name: "M3", DependsOn: []string{"A"}},
				{ID: "M4", Name: "M4", DependsOn: []string{"M3"}},
			},
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "A", Coding: true},
			{Name: "N-DOC", WorkerClass: "system-architect", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
		ActivityConstruction: map[string]projectstate.ActivityConstructionStatus{
			"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionDone},
		},
	}

	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch through M4->M3->A, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "N-DOC" {
		t.Fatalf("want N-DOC dispatched, got %q", sel.Activity.ActivityID)
	}

	// Unsatisfied at the bottom of the chain (A not Done) must propagate all the way
	// back up through both milestone hops.
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"A": {ActivityID: "A", Phase: projectstate.ActivityConstructionRunning},
	}
	sel = nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictQuiescent {
		t.Fatalf("want verdictQuiescent (A not done, M3/M4 not reached), got %v", sel.Verdict)
	}
}

// TestNextEligibleActivity_MilestoneCycleTerminates proves the cycle guard: an
// authored network can (however wrongly) declare M-X depends on M-Y depends on M-X.
// Resolution must terminate — reported as verdictBlocked, not an infinite recursion —
// and the test itself is bounded so a regression that reintroduces unbounded
// recursion fails as a hang the test runner's own timeout catches, not silently.
func TestNextEligibleActivity_MilestoneCycleTerminates(t *testing.T) {
	done := make(chan pumpSelection, 1)
	go func() {
		proj := projectstate.Project{
			Phase: projectstate.PhaseConstruction,
			Network: makeCommittedNetworkWithMilestones(
				[]projectstate.NetworkDependency{
					{Activity: "N-DOC", DependsOn: []string{"M-X"}},
				},
				[]projectstate.NetworkMilestone{
					{ID: "M-X", Name: "M-X", DependsOn: []string{"M-Y"}},
					{ID: "M-Y", Name: "M-Y", DependsOn: []string{"M-X"}},
				},
			),
			ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
				{Name: "N-DOC", WorkerClass: "system-architect", Coding: false},
			}),
			SystemDesign: makeCommittedSystemDesign(nil),
		}
		done <- nextEligibleActivity(proj, eligibleDispatchable)
	}()

	select {
	case sel := <-done:
		if sel.Verdict != verdictBlocked {
			t.Fatalf("want verdictBlocked on a milestone cycle, got %v", sel.Verdict)
		}
		if !strings.Contains(sel.BlockedReason, "cycle") {
			t.Fatalf("blocked reason must call out the cycle, got %q", sel.BlockedReason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nextEligibleActivity did not terminate on a milestone dependency cycle (infinite recursion)")
	}
}

// TestNextEligibleActivity_UnknownDependencyIdIsBlocked: an authored dependency id
// that names neither a known activity nor a known milestone is a genuine plan defect
// — loud (verdictBlocked), never silently treated as "just not satisfied yet" (which
// would be indistinguishable from an ordinary quiescent tick).
func TestNextEligibleActivity_UnknownDependencyIdIsBlocked(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "N-DOC", DependsOn: []string{"M-GHOST"}},
			},
			nil,
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "N-DOC", WorkerClass: "system-architect", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
	}

	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictBlocked {
		t.Fatalf("want verdictBlocked for an unresolvable dependency id, got %v", sel.Verdict)
	}
	if sel.BlockedActivityID != "N-DOC" {
		t.Fatalf("want blocked activity N-DOC, got %q", sel.BlockedActivityID)
	}
	if !strings.Contains(sel.BlockedReason, "M-GHOST") {
		t.Fatalf("blocked reason must name the unresolvable id, got %q", sel.BlockedReason)
	}
}

// TestNextEligibleActivity_DependencyDefectDoesNotBlockUnrelatedWork: a dependency
// defect on ONE not-yet-eligible activity must not halt the pump entirely — an
// UNRELATED, currently-eligible activity still dispatches this tick. The defect only
// escalates to verdictBlocked once it is the thing actually stalling progress (see
// TestNextEligibleActivity_UnknownDependencyIdIsBlocked).
func TestNextEligibleActivity_DependencyDefectDoesNotBlockUnrelatedWork(t *testing.T) {
	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "A", DependsOn: []string{}},
				{Activity: "N-DOC", DependsOn: []string{"M-GHOST"}},
			},
			nil,
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "A", Coding: true},
			{Name: "N-DOC", WorkerClass: "system-architect", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
	}

	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("want verdictDispatch for unrelated eligible activity A, got %v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "A" {
		t.Fatalf("want A dispatched despite N-DOC's dependency defect, got %q", sel.Activity.ActivityID)
	}
}

// TestNextEligibleActivity_UncommittedSlots exercises the nil/uncommitted guard.
func TestNextEligibleActivity_UncommittedSlots(t *testing.T) {
	activities := []projectstate.ActivityItem{
		{Name: "A", EffortDays: 5, WorkerClass: "AI", Coding: true, RiskBucket: 2},
	}

	// Uncommitted Network slot (zero value ArtifactSlot).
	t.Run("uncommitted_network", func(t *testing.T) {
		proj := projectstate.Project{
			ActivityList: makeCommittedActivityList(activities),
		}
		sel := nextEligibleActivity(proj, eligibleDispatchable)
		if sel.Verdict != verdictQuiescent {
			t.Fatalf("expected verdictQuiescent for uncommitted network, got %v", sel.Verdict)
		}
	})

	// Uncommitted ActivityList slot.
	t.Run("uncommitted_activity_list", func(t *testing.T) {
		proj := projectstate.Project{
			Network: makeCommittedNetwork([]projectstate.NetworkDependency{
				{Activity: "A", DependsOn: []string{}},
			}),
		}
		sel := nextEligibleActivity(proj, eligibleDispatchable)
		if sel.Verdict != verdictQuiescent {
			t.Fatalf("expected verdictQuiescent for uncommitted activity list, got %v", sel.Verdict)
		}
	})

	// Both uncommitted (zero-value project).
	t.Run("both_uncommitted", func(t *testing.T) {
		sel := nextEligibleActivity(projectstate.Project{}, eligibleDispatchable)
		if sel.Verdict != verdictQuiescent {
			t.Fatalf("expected verdictQuiescent for zero-value project, got %v", sel.Verdict)
		}
	})
}

// TestNextEligibleActivity_RequiresConstructionPhase verifies the pump refuses to
// select work until the project has been sealed into Phase 3 (AdvanceToConstruction)
// — committed Network+ActivityList alone, with the project still in system- or
// project-design, must not dispatch construction activities.
func TestNextEligibleActivity_RequiresConstructionPhase(t *testing.T) {
	network := []projectstate.NetworkDependency{{Activity: "A", DependsOn: []string{}}}
	activities := []projectstate.ActivityItem{
		{Name: "A", Title: "A", EffortDays: 5, WorkerClass: "junior-developer", Coding: true, RiskBucket: 2, ComponentID: "comp-a"},
	}
	base := projectstate.Project{
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
		SystemDesign: makeCommittedSystemDesign([]projectstate.Component{
			{ID: "comp-a", Name: "A", Layer: projectstate.LayerManager},
		}),
	}

	for _, tc := range []struct {
		name  string
		phase projectstate.Phase
	}{
		{"system_design", projectstate.PhaseSystemDesign},
		{"project_design", projectstate.PhaseProjectDesign},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proj := base
			proj.Phase = tc.phase
			if sel := nextEligibleActivity(proj, eligibleDispatchable); sel.Verdict != verdictQuiescent {
				t.Fatalf("expected verdictQuiescent before the construction seal (phase %v), got %v", tc.phase, sel.Verdict)
			}
		})
	}

	t.Run("construction", func(t *testing.T) {
		proj := base
		proj.Phase = projectstate.PhaseConstruction
		sel := nextEligibleActivity(proj, eligibleDispatchable)
		if sel.Verdict != verdictDispatch || sel.Activity.ActivityID != "A" {
			t.Fatalf("expected A eligible once sealed into construction, got verdict=%v id=%q", sel.Verdict, sel.Activity.ActivityID)
		}
	})
}

// TestNextEligibleActivity_ProjectExportDogfood exercises the dogfood activity
// C-PE (projectExport endpoint) introduced in Spec 2. C-PE depends on C-CW (Build Web Client)
// and D-MPD (Detailed design — projectDesignManager), which are both Phase=2 (Done)
// in the live project. This test uses a synthetic project where both deps are Done
// and C-PE is NotStarted, verifying nextEligibleActivity selects it.
//
// Reconciliation note: selection now resolves the authored ComponentID against the
// committed systemDesign, not a service-contract key — C-PE names component
// "projectExport" directly.
func TestNextEligibleActivity_ProjectExportDogfood(t *testing.T) {
	network := []projectstate.NetworkDependency{
		{Activity: "C-CW", DependsOn: []string{}},
		{Activity: "D-MPD", DependsOn: []string{}},
		{Activity: "C-PE", DependsOn: []string{"C-CW", "D-MPD"}},
	}
	activities := []projectstate.ActivityItem{
		{Name: "C-CW", EffortDays: 30, WorkerClass: "junior-developer", Coding: true, RiskBucket: 8},
		{Name: "D-MPD", EffortDays: 5, WorkerClass: "senior-developer", Coding: false, RiskBucket: 2},
		{Name: "C-PE", Title: "C-PE", EffortDays: 3, WorkerClass: "junior-developer", Coding: true, RiskBucket: 1, ComponentID: "projectExport"},
	}
	proj := projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
		SystemDesign: makeCommittedSystemDesign([]projectstate.Component{
			{ID: "projectExport", Name: "projectExport", Layer: projectstate.LayerManager},
		}),
		ActivityConstruction: map[string]projectstate.ActivityConstructionStatus{
			"C-CW":  {ActivityID: "C-CW", Phase: projectstate.ActivityConstructionDone},
			"D-MPD": {ActivityID: "D-MPD", Phase: projectstate.ActivityConstructionDone},
			// C-PE is absent (zero value = NotStarted)
		},
	}
	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("expected C-PE to be eligible, got verdict=%v (blocked=%q)", sel.Verdict, sel.BlockedReason)
	}
	if sel.Activity.ActivityID != "C-PE" {
		t.Fatalf("expected ActivityID=C-PE, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.ComponentID != "projectExport" {
		t.Fatalf("expected ComponentID=projectExport, got %q", sel.Activity.ComponentID)
	}
	if sel.Activity.EstimateDays != 3 {
		t.Fatalf("expected EstimateDays=3, got %f", sel.Activity.EstimateDays)
	}
	if sel.Activity.Kind != activityKindConstruction {
		t.Fatalf("expected Kind=activityKindConstruction (Coding=true), got %v", sel.Activity.Kind)
	}
}

// TestNextEligibleActivity_HydratedFields checks that the returned constructionActivity
// is fully hydrated from the ActivityList item and the resolved systemDesign component.
func TestNextEligibleActivity_HydratedFields(t *testing.T) {
	network := []projectstate.NetworkDependency{
		{Activity: "X", DependsOn: []string{}},
	}
	activities := []projectstate.ActivityItem{
		{Name: "X", Title: "X", EffortDays: 13, WorkerClass: "HumanSenior", Coding: true, RiskBucket: 5, ComponentID: "comp-x"},
	}
	proj := projectstate.Project{
		Phase:        projectstate.PhaseConstruction,
		Network:      makeCommittedNetwork(network),
		ActivityList: makeCommittedActivityList(activities),
		SystemDesign: makeCommittedSystemDesign([]projectstate.Component{
			{ID: "comp-x", Name: "X", Layer: projectstate.LayerEngine},
		}),
	}
	sel := nextEligibleActivity(proj, eligibleDispatchable)
	if sel.Verdict != verdictDispatch {
		t.Fatalf("expected verdictDispatch, got %v", sel.Verdict)
	}
	if sel.Activity.ActivityID != "X" {
		t.Fatalf("expected ActivityID=X, got %q", sel.Activity.ActivityID)
	}
	if sel.Activity.EstimateDays != 13 {
		t.Fatalf("expected EstimateDays=13, got %f", sel.Activity.EstimateDays)
	}
	// Kind is determined by Coding flag: Coding=true → activityKindConstruction.
	if sel.Activity.Kind != activityKindConstruction {
		t.Fatalf("expected Kind=activityKindConstruction, got %v", sel.Activity.Kind)
	}
	if sel.Activity.ComponentID != "comp-x" {
		t.Fatalf("expected ComponentID=comp-x, got %q", sel.Activity.ComponentID)
	}
	if sel.Activity.Layer != "engine" {
		t.Fatalf("expected Layer=engine, got %q", sel.Activity.Layer)
	}
}

// TestPipelineAdapter_DispatchInputs asserts that dispatchInputsFor maps
// ActivityID → "activity_id" and ComponentID → "component_id".
// pipelineAdapter.inner is the agenticjob.AgenticJobAccess interface (not a concrete struct,
// interface), so we test the pure mapping helper directly — no fake adapter needed.
func TestPipelineAdapter_DispatchInputs(t *testing.T) {
	inputs := dispatchInputsFor(pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
	})
	if inputs["activity_id"] != "C-PE" {
		t.Fatalf("expected activity_id=C-PE, got %q", inputs["activity_id"])
	}
	if inputs["component_id"] != "projectExport" {
		t.Fatalf("expected component_id=projectExport, got %q", inputs["component_id"])
	}
}

// TestDispatchInputsFor_WithPhase asserts that a non-empty Phase field on
// pipelineSpec is emitted as the "phase" key in the dispatch inputs map
// (REQ-2 + Plan 1 Task 6).
func TestDispatchInputsFor_WithPhase(t *testing.T) {
	spec := pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
		Phase:       "requirements",
	}
	got := dispatchInputsFor(spec)
	cases := map[string]string{
		"activity_id":  "C-PE",
		"component_id": "projectExport",
		"phase":        "requirements",
	}
	for k, want := range cases {
		if got[k] != want {
			t.Errorf("dispatchInputsFor[%q] = %q, want %q", k, got[k], want)
		}
	}
}

// TestDispatchInputsFor_EmptyPhaseOmitted asserts that an empty Phase value is
// NOT emitted — callers that do not set it get only activity_id/component_id,
// so existing workflow dispatches that rely on workflow-declared defaults are unaffected.
func TestDispatchInputsFor_EmptyPhaseOmitted(t *testing.T) {
	spec := pipelineSpec{
		ActivityID:  "C-PE",
		ComponentID: "projectExport",
		// Phase intentionally empty
	}
	got := dispatchInputsFor(spec)
	if _, ok := got["phase"]; ok {
		t.Error("phase key should not be present when Phase is empty")
	}
}

// fakeQueryClient scripts QueryWorkflow with an error, for the F20 pre-phase read test.
// It embeds client.Client so any unimplemented method panics.
type fakeQueryClient struct {
	client.Client
	queryErr error
}

func (f *fakeQueryClient) QueryWorkflow(_ context.Context, _ string, _ string, _ string, _ ...any) (converter.EncodedValue, error) {
	return nil, f.queryErr
}

// ---- F20: clean not-found altitude on the pre-phase construction read ------

// Before construction starts the pump workflow does not exist; Temporal's raw
// "workflow not found for ID: gtdapp:construction" must NOT reach the client. Map it to
// a clean, user-altitude NotFound.
func Test_GetSessionState_BeforeConstruction_CleanNotFound(t *testing.T) {
	fc := &fakeQueryClient{queryErr: serviceerror.NewNotFound("workflow not found for ID: gtdapp:construction")}
	m := newTestConstructionManager(fc)

	_, err := m.GetSessionState(testCtx(), ProjectID("gtdapp"), nil)
	e := asConstructionError(t, err)
	if e.Kind != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %d", e.Kind)
	}
	if strings.Contains(e.Detail, "workflow not found") || strings.Contains(e.Detail, "gtdapp:construction") {
		t.Fatalf("Temporal internals leaked to the client: %q", e.Detail)
	}
	if !strings.Contains(e.Detail, "construction has not started") {
		t.Fatalf("want a user-altitude message, got %q", e.Detail)
	}
}

// A PER-ACTIVITY miss means only that the pump has not dispatched that activity —
// construction may be under way elsewhere in the project — so the copy names the
// activity and never claims the whole project has not started.
func Test_GetSessionState_ActivityNotDispatched_NamesTheActivity(t *testing.T) {
	fc := &fakeQueryClient{queryErr: serviceerror.NewNotFound("workflow not found for ID: gtdapp:C-billing-manager")}
	m := newTestConstructionManager(fc)
	act := ActivityID("C-billing-manager")

	_, err := m.GetSessionState(testCtx(), ProjectID("gtdapp"), &act)
	e := asConstructionError(t, err)
	if e.Kind != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %d", e.Kind)
	}
	if strings.Contains(e.Detail, "for this project") {
		t.Fatalf("a per-activity miss claims the whole project has not started: %q", e.Detail)
	}
	if !strings.Contains(e.Detail, "C-billing-manager") || !strings.Contains(e.Detail, "not dispatched") {
		t.Fatalf("want the activity named as not dispatched, got %q", e.Detail)
	}
	if strings.Contains(e.Detail, "workflow not found") {
		t.Fatalf("Temporal internals leaked to the client: %q", e.Detail)
	}
}

// QA 2026-07-19 (poll-404 wizard reset twin): a namespace-not-found from a wrong/foreign
// Temporal backend must NOT map to the authoritative "construction has not started"
// NotFound — the polled console trusts that 404 and drops its session view. It stays an
// Infrastructure fault the client tolerates.
func Test_GetSessionState_NamespaceNotFound_IsInfrastructureNot404(t *testing.T) {
	fc := &fakeQueryClient{queryErr: serviceerror.NewNamespaceNotFound("default")}
	m := newTestConstructionManager(fc)

	_, err := m.GetSessionState(testCtx(), ProjectID("gtdapp"), nil)
	e := asConstructionError(t, err)
	if e.Kind == fwmanager.NotFound {
		t.Fatalf("namespace-not-found (wrong Temporal backend) must not claim construction absence, got NotFound %q", e.Detail)
	}
	if e.Kind != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %d (detail %q)", e.Kind, e.Detail)
	}
}

// =============================================================================
// constructionManager workflow unit tests over the Temporal in-memory test
// environment (testsuite.WorkflowTestSuite). The three Engines (handOffEngine,
// interventionEngine, reviewEngine) and the four ResourceAccess ports
// (projectStateAccess, agenticJobAccess, artifactAccess, workerAccess)
// are constructed as interface test doubles (fakes) — the not-yet-built deps are
// driven against their FROZEN CONTRACTS as the Manager-declared consumer interfaces
// (deps.go). These run with no Docker and no dev server (the real-infrastructure
// exercise is a later integration activity).
//
// They assert the UC3 spine (cast → dispatch → submit/observe → stage → review →
// recordChangeReviewed → recordActivityExited), the no-eligible-activity quiet
// tick, the pause branch (NCUC2), the operator-override branch, and the key
// error/variance/conflict paths — per [[the-method-testing]] (black-box where the
// observable is the workflow result/recorded side effects).
// =============================================================================

// ---- Fakes (interface test doubles for the downstream deps) -----------------

// fakeProjectState records the additive Phase-3 transition calls + serves a
// scripted head-state. It satisfies the Manager's ProjectStateAccess consumer
// interface (deps.go) — the read + the three additive transition verbs.
type fakeProjectState struct {
	mu sync.Mutex

	project  projectstate.Project
	notFound bool

	// conflictFirst, when >0, returns fwra.Conflict on the first N transition
	// calls (across all transition verbs) before succeeding — drives the §6.5
	// re-read→re-apply loop.
	conflictFirst int

	reviewed  []string
	exited    []exitCall
	failed    []failCall
	paused    []string
	phaseDone []phaseCompletedCall

	version projectstate.Version

	// order, when set, receives "record" on every RecordOperatorPaused — a call-order
	// log shared with the other fakes (callLog).
	order *callLog

	// notes / delivered record the operator-note verbs (B1.4), in call order.
	notes     []noteCall
	delivered []deliveredCall

	// resumed counts RecordOperatorResumed calls (B1.7).
	resumed int

	// stampConflicts, when >0, returns fwra.Conflict on the next N
	// RecordOperatorNoteDelivered calls only (a stamp that cannot land, M4).
	stampConflicts int
	// afterConflict, when set, runs each time maybeConflict serves a Conflict — the
	// concurrent write that caused it (I2: a new pause landing between two tries).
	afterConflict func(*fakeProjectState)
}

// noteCall is one RecordOperatorNote; deliveredCall one RecordOperatorNoteDelivered.
type noteCall struct {
	activityID string
	note       projectstate.OperatorNoteInput
}

type deliveredCall struct {
	activityID, noteID, attemptID string
}

// phaseCompletedCall records one RecordPhaseCompleted transition (the gate's durable
// per-phase completion record). The gate tests assert on it via phaseCompleted.
type phaseCompletedCall struct {
	activityID string
	phase      string
}

// phaseCompleted reports whether RecordPhaseCompleted landed for (activityID, phase).
func (f *fakeProjectState) phaseCompleted(activityID, phase string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.phaseDone {
		if c.activityID == activityID && c.phase == phase {
			return true
		}
	}
	return false
}

type exitCall struct {
	activityID string
	outcome    projectstate.ActivityOutcome
}

type failCall struct {
	activityID string
	reason     projectstate.FailureReason
	detail     string
}

func (f *fakeProjectState) ReadProject(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return projectstate.Project{}, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project, nil
}

func (f *fakeProjectState) ReadProjectVersion(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notFound {
		return 0, fwra.New(fwra.NotFound, "no row yet")
	}
	return f.project.Version, nil
}

func (f *fakeProjectState) bump() projectstate.Version {
	f.version++
	f.project.Version = f.version
	return f.version
}

func (f *fakeProjectState) maybeConflict() error {
	if f.conflictFirst > 0 {
		f.conflictFirst--
		// Advance the served head version so the re-read sees a newer value.
		f.version++
		f.project.Version = f.version
		if f.afterConflict != nil {
			f.afterConflict(f)
		}
		return fwra.New(fwra.Conflict, "stale version")
	}
	return nil
}

func (f *fakeProjectState) RecordChangeReviewed(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.reviewed = append(f.reviewed, activityID)
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityExited(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, outcome projectstate.ActivityOutcome, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.exited = append(f.exited, exitCall{activityID: activityID, outcome: outcome})
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityFailed(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, reason projectstate.FailureReason, detail string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.failed = append(f.failed, failCall{activityID: activityID, reason: reason, detail: detail})
	return f.bump(), nil
}

func (f *fakeProjectState) RecordOperatorPaused(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, reason string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.paused = append(f.paused, reason)
	// Like the real store (GitStore.RecordOperatorPaused): the pause lands in head-state,
	// so a later read — the pump's readProject — sees it.
	f.project.OperatorPaused = true
	f.project.PauseReason = reason
	f.order.add("record")
	return f.bump(), nil
}

// RecordOperatorResumed records a resume (B1.7): the recorded pause is cleared.
func (f *fakeProjectState) RecordOperatorResumed(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.resumed++
	f.project.OperatorPaused = false
	f.project.PauseReason = ""
	if f.order != nil {
		f.order.add("resume")
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordReviewPolicy(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ projectstate.ReviewPolicy, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordOperatorNote(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, note projectstate.OperatorNoteInput, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.notes = append(f.notes, noteCall{activityID: activityID, note: note})
	if f.order != nil {
		f.order.add("note")
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordOperatorNoteDelivered(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID, noteID, attemptID string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stampConflicts > 0 {
		f.stampConflicts--
		f.version++
		f.project.Version = f.version
		return 0, fwra.New(fwra.Conflict, "stale version (stamp)")
	}
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.delivered = append(f.delivered, deliveredCall{activityID: activityID, noteID: noteID, attemptID: attemptID})
	if f.order != nil {
		f.order.add("delivered")
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseStarted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ActivityMethodPhase, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseCompleted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, phase projectstate.ActivityMethodPhase, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	f.phaseDone = append(f.phaseDone, phaseCompletedCall{activityID: activityID, phase: phase.String()})
	return f.bump(), nil
}

// ---- gitActivityStatusAccess seam (per-activity construction head-state) ----
// The gate tests wire GitStatus so gitOn is true (the LOCAL/dry-run profile — no PR
// rail), which is what drives the phase-started/completed head-state records. These
// are no-op bumps; only RecordPhaseCompleted (a construction-transition verb, above)
// carries the assertion the gate tests read.

func (f *fakeProjectState) RecordActivityBranchOpened(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _, _, _, _, _ string, _ bool, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityCIObserved(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.CICheckState, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityArchApproved(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityMerged(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityStarted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ActivityType, _ projectstate.TestingVariant, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordActivityCompleted(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bump(), nil
}

func (f *fakeProjectState) RecordServiceContractProduced(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ServiceContract, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

func (f *fakeProjectState) RecordPhaseArtifactProduced(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ string, _ projectstate.PhaseArtifactPayload, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.maybeConflict(); err != nil {
		return 0, err
	}
	return f.bump(), nil
}

var _ projectstate.GitActivityStatusAccess = (*fakeProjectState)(nil)

// fakeConstructionTransition widens fakeProjectState onto the FULL
// projectstate.ConstructionTransitionAccess contract (B8): the nine write verbs are
// inherited verbatim (byte-identical signatures); ReadProject gets the extra `cred`
// parameter the fake's own 2-arg ReadProject never carried, so this adapter widens the
// signature by ignoring cred and delegating.
type fakeConstructionTransition struct {
	*fakeProjectState
}

func (f fakeConstructionTransition) ReadProject(rc fwra.Context, projectID projectstate.ProjectID, _ projectstate.RepoCredential) (projectstate.Project, error) {
	return f.fakeProjectState.ReadProject(rc, projectID)
}

var _ projectstate.ConstructionTransitionAccess = fakeConstructionTransition{}

// fakeFullProjectState widens fakeProjectState onto the FULL projectstate.ProjectStateAccess
// contract so the test env can (a) register the GENERATED ProjectStateReadProjectVersion
// activity (B8: migrated off the custom ReadProjectVersionActivity) and (b) serve as the
// BASE of the real projectstate.NewDesignSessionAccess wrapper backing the GENERATED
// designSessionAccess.readProjectOnBranch activity (B8 follow-up: the pump's
// whole-aggregate read; branch "" reads main via ReadProject — the generated
// ProjectStateAccess contract requires ReadProjectOnBranch("") to behave exactly as
// ReadProject, C2 fold code-health-phase-a). ReadProject/ReadProjectVersion are inherited
// verbatim (byte-identical signatures); the remaining eight ops are never exercised by
// these workflow tests — each is an inert stub, matching the stubRail precedent
// (gitforward_test.go) for satisfying an unused portion of a wide contract.
type fakeFullProjectState struct {
	*fakeProjectState
}

func (f fakeFullProjectState) ReadProjectOnBranch(rc fwra.Context, projectID projectstate.ProjectID, _ string) (projectstate.Project, error) {
	return f.ReadProject(rc, projectID)
}

func (fakeFullProjectState) StageArtifactForReviewOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactModel, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) RejectArtifactOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) WithdrawArtifactOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) RejectArtifactOnBranchWithComments(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, int64, []projectstate.ReviewComment, []projectstate.ReviewReply, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) SetReviewCommentStatusOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, string, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) SeedReviewCommentsOnBranch(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, int64, []projectstate.ReviewComment, []projectstate.ReviewReply, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) ReconcileBranchFromMain(fwra.Context, projectstate.ProjectID, projectstate.Version, string, projectstate.ArtifactKind, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) AcknowledgeStaleBasis(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string, fwra.IdempotencyKey) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) AdvancePhase(fwra.Context, projectstate.ProjectID, projectstate.Version) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) CommitArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) CreateProject(fwra.Context, projectstate.ProjectID, projectstate.OwnerScope, string) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) ListProjects(fwra.Context, projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	return nil, nil
}

func (fakeFullProjectState) RejectArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) SetOperatingModel(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.OperatingModel) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) SetResearchInput(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ResearchInput) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) StageArtifactForReview(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactModel) (projectstate.Version, error) {
	return 0, nil
}

func (fakeFullProjectState) WithdrawArtifact(fwra.Context, projectstate.ProjectID, projectstate.Version, projectstate.ArtifactKind, string) (projectstate.Version, error) {
	return 0, nil
}

var _ projectstate.ProjectStateAccess = fakeFullProjectState{}

// contractPipelinePhase maps the Manager-neutral PipelinePhase the fakes are scripted
// with back onto the contract agenticjob.PipelinePhase the GENERATED observe
// activity returns (reverse of managerPipelinePhase) — so the test literals stay written
// in the Manager vocabulary while the fakes honor the contract interface.
func contractPipelinePhase(p PipelinePhase) agenticjob.PipelinePhase {
	switch p {
	case PipelinePending:
		return agenticjob.PhasePending
	case PipelineRunning:
		return agenticjob.PhaseRunning
	case PipelineSucceeded:
		return agenticjob.PhaseSucceeded
	case PipelineFailed:
		return agenticjob.PhaseFailed
	case PipelineCancelled:
		return agenticjob.PhaseCancelled
	default:
		return agenticjob.PhasePending
	}
}

// fakePipeline serves a scripted terminal observation after one running poll. It honors
// the FROZEN agenticjob.AgenticJobAccess contract (the GENERATED
// pipeline Activities are backed by it); the workflow reaches it through the generated
// invoker surface.
type fakePipeline struct {
	mu sync.Mutex

	phase     PipelinePhase // terminal phase to serve
	diag      string
	submitted []agenticjob.PipelineSpec
	cancelled []agenticjob.PipelineHandle
	polls     int

	// order, when set, receives "cancel" on every cancel — a call-order log shared
	// with the other fakes (callLog).
	order *callLog

	// episode, when set, rides EVERY observation — the local executor's mined
	// EpisodeSummary (SP1 capture-seam). nil mirrors the GitHub-Actions arm / a lost run.
	episode *agenticjob.EpisodeSummary
	// runURL, when set, marks the observation as coming from the REMOTE venue (the
	// GitHub-Actions arm stamps the run's html URL; the local executor never does).
	runURL string
}

func (p *fakePipeline) SubmitAgenticJob(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	return agenticjob.PipelineHandle("wf-" + string(spec.ActivityID)), nil
}

func (p *fakePipeline) ObserveAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	ph := p.phase
	if ph == PipelinePhaseUnknown {
		ph = PipelineSucceeded
	}
	return agenticjob.PipelineObservation{
		Phase:      contractPipelinePhase(ph),
		Diagnostic: p.diag,
		RunURL:     p.runURL,
		Episode:    p.episode,
	}, nil
}

func (p *fakePipeline) CancelAgenticJob(_ fwra.Context, handle agenticjob.PipelineHandle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, handle)
	p.order.add("cancel")
	return nil
}

var _ agenticjob.AgenticJobAccess = (*fakePipeline)(nil)

// fakeIntervention returns scripted directives/plans. Satisfies the PUBLISHED
// intervention.InterventionEngine directly (Task 6 — no Manager-local seam); only
// DecideOnVariance/ApplyPausePolicy are exercised by these tests — DecideOnHealth and
// DecideOnSettlementFailure (operations'/billing's verbs) are unused stubs.
//
// directive's zero value is intervention.VarianceRetry (the published enum's own
// zero, unlike the retired local varianceDirective's directiveUnknown=0 sentinel) —
// DecideOnVariance returns it as-is with no special-casing; every test that leaves
// directive unset (&fakeIntervention{}) never actually calls DecideOnVariance (quiet
// ticks / drained network / pause branch / quiet sweep), so this is unobserved.
type fakeIntervention struct {
	directive intervention.VarianceDirective
	plan      intervention.PausePlan
}

func (i *fakeIntervention) DecideOnVariance(_ fweng.Context, _ intervention.ConstructionVariance) (intervention.VarianceDirective, error) {
	return i.directive, nil
}

func (i *fakeIntervention) ApplyPausePolicy(_ fweng.Context, _ intervention.PauseRequestContext) (intervention.PausePlan, error) {
	return i.plan, nil
}

func (i *fakeIntervention) DecideOnHealth(_ fweng.Context, _ intervention.HealthChange) (intervention.HealthDirective, error) {
	return intervention.HealthRetry, nil
}

func (i *fakeIntervention) DecideOnSettlementFailure(_ fweng.Context, _ intervention.SettlementFailure) (intervention.SettlementFailureDirective, error) {
	return intervention.SettlementRetry, nil
}

var _ intervention.InterventionEngine = (*fakeIntervention)(nil)

// fakeReview returns a scripted reviewer set. Satisfies the PUBLISHED
// review.ReviewEngine directly (Task 6 — no Manager-local seam). The scripted `set`
// is the ENGINE's own review.ReviewSet — reviewSetFromEngine (adapters.go) bridges it
// onto the façade ReviewSet the same way production does.
type fakeReview struct {
	set review.ReviewSet
}

func (r *fakeReview) ProposeReviews(_ fweng.Context, _ review.ReviewChange, _ string, _ string, _ string, _ []string) (review.ReviewSet, error) {
	return r.set, nil
}

var _ review.ReviewEngine = (*fakeReview)(nil)

// ---- helpers ----------------------------------------------------------------

// registerGenPipeline registers the GENERATED pipeline Activities (backed by the contract
// pipeline fake) under their generated registered names — the names the generated invoker
// surface (wf.Acts.Pipeline*) dispatches by. The workflow reaches the pipeline only through
// those invokers now.
func registerGenPipeline(env *testsuite.TestWorkflowEnvironment, pipe agenticjob.AgenticJobAccess) {
	acts := &genActivities{Pipeline: pipe}
	env.RegisterActivityWithOptions(acts.PipelineSubmitAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.submitAgenticJob"})
	env.RegisterActivityWithOptions(acts.PipelineObserveAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.observeAgenticJob"})
	env.RegisterActivityWithOptions(acts.PipelineCancelAgenticJob, activity.RegisterOptions{Name: "agenticJobAccess.cancelAgenticJob"})
}

// registerGenProjectStateVersion registers the GENERATED ProjectStateReadProjectVersion
// activity (B8: migrated off the custom ReadProjectVersionActivity) under its generated
// registered name, backed by ps widened onto the full ProjectStateAccess contract
// (fakeFullProjectState — only ReadProjectVersion is ever exercised through this seam).
func registerGenProjectStateVersion(env *testsuite.TestWorkflowEnvironment, ps *fakeProjectState) {
	acts := &genActivities{ProjectState: fakeFullProjectState{ps}}
	env.RegisterActivityWithOptions(acts.ProjectStateReadProjectVersion, activity.RegisterOptions{Name: "projectStateAccess.readProjectVersion"})
}

// registerGenConstructionTransition registers the GENERATED constructionTransitionAccess
// Record* activities (B8: migrated off the custom Record*Activity methods,
// activities_custom.go) under their generated registered names, backed by ps widened onto
// the full ConstructionTransitionAccess contract (fakeConstructionTransition).
func registerGenConstructionTransition(env *testsuite.TestWorkflowEnvironment, ps *fakeProjectState) {
	acts := &genActivities{ConstructionTransition: fakeConstructionTransition{ps}}
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordChangeReviewed, activity.RegisterOptions{Name: "constructionTransitionAccess.recordChangeReviewed"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordActivityExited, activity.RegisterOptions{Name: "constructionTransitionAccess.recordActivityExited"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordActivityFailed, activity.RegisterOptions{Name: "constructionTransitionAccess.recordActivityFailed"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordOperatorPaused, activity.RegisterOptions{Name: "constructionTransitionAccess.recordOperatorPaused"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordPhaseStarted, activity.RegisterOptions{Name: "constructionTransitionAccess.recordPhaseStarted"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordPhaseCompleted, activity.RegisterOptions{Name: "constructionTransitionAccess.recordPhaseCompleted"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordOperatorNote, activity.RegisterOptions{Name: "constructionTransitionAccess.recordOperatorNote"})
	env.RegisterActivityWithOptions(acts.ConstructionTransitionRecordOperatorNoteDelivered, activity.RegisterOptions{Name: "constructionTransitionAccess.recordOperatorNoteDelivered"})
}

// registerGenGitStatus registers the GENERATED gitActivityStatusAccess Record* activities
// (B8: migrated off the custom RecordActivity*Activity methods, gitactivities.go) under
// their generated registered names, backed by gs — either ps (already the full
// projectstate.GitActivityStatusAccess contract — no widening needed) or, for the
// git-forward tests, the separate stubGitStatus store (gitforward_test.go).
func registerGenGitStatus(env *testsuite.TestWorkflowEnvironment, gs projectstate.GitActivityStatusAccess) {
	acts := &genActivities{GitStatus: gs}
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityBranchOpened, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityBranchOpened"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityCIObserved, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityCIObserved"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityArchApproved, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityArchApproved"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityMerged, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityMerged"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityStarted, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityStarted"})
	env.RegisterActivityWithOptions(acts.GitStatusRecordActivityCompleted, activity.RegisterOptions{Name: "gitActivityStatusAccess.recordActivityCompleted"})
}

// registerGenDesignSessionRead registers the GENERATED designSessionAccess
// readProjectOnBranch activity (B8 follow-up: the pump's whole-aggregate read — the
// former custom ReadProjectActivity) under its generated registered name. It is backed
// by the REAL projectstate.NewDesignSessionAccess wrapper over ps (widened onto the full
// base contract via fakeFullProjectState), so the workflow's branch "" read exercises
// the production empty-branch→base.ReadProject fallback chain (designsession.go) and
// the shared ProjectEnvelope encode/decode round trip end-to-end.
func registerGenDesignSessionRead(env *testsuite.TestWorkflowEnvironment, ps *fakeProjectState) {
	acts := &genActivities{DesignSession: projectstate.NewDesignSessionAccess(fakeFullProjectState{ps})}
	env.RegisterActivityWithOptions(acts.DesignSessionReadProjectOnBranch, activity.RegisterOptions{Name: "designSessionAccess.readProjectOnBranch"})
}

// fakeEpisodes is the episodeAccess test double: it RECORDS every appended record and
// counts every attempt, so a test can assert both what was written and how many times the
// append was retried. failN>0 fails the first failN attempts; failAlways fails every one.
// It also HONOURS the call context, so an append handed an already-cancelled context is
// caught by the test rather than silently blessed (the production realisations ignore it).
type fakeEpisodes struct {
	mu         sync.Mutex
	appended   []episode.EpisodeRecord
	attempts   int
	failN      int
	failAlways bool

	// listRecords/listErr back ListEpisodes (Task 9 facet reads); lastQuery
	// captures the query the manager actually issued, for TargetRef-semantics
	// assertions.
	listRecords []episode.EpisodeRecord
	listErr     error
	lastQuery   episode.EpisodeQuery

	// traceEvents/traceErr back ReadTraceEvents; lastTrace* captures the args
	// the manager actually issued.
	traceEvents        []json.RawMessage
	traceErr           error
	lastTraceProjectID episode.ProjectID
	lastTraceEpisodeID string
}

func (f *fakeEpisodes) AppendEpisode(rc fwra.Context, _ episode.ProjectID, record episode.EpisodeRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if rc.Context != nil && rc.Err() != nil {
		return fwra.New(fwra.Infrastructure, "append called with a dead context: "+rc.Err().Error())
	}
	if f.failAlways || f.attempts <= f.failN {
		return fwra.New(fwra.Infrastructure, "episode ledger unavailable")
	}
	f.appended = append(f.appended, record)
	return nil
}

func (f *fakeEpisodes) ListEpisodes(_ fwra.Context, query episode.EpisodeQuery) ([]episode.EpisodeRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastQuery = query
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRecords, nil
}

func (f *fakeEpisodes) ReadTraceEvents(_ fwra.Context, projectID episode.ProjectID, episodeID string) ([]json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastTraceProjectID = projectID
	f.lastTraceEpisodeID = episodeID
	if f.traceErr != nil {
		return nil, f.traceErr
	}
	return f.traceEvents, nil
}

func (f *fakeEpisodes) records() []episode.EpisodeRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]episode.EpisodeRecord(nil), f.appended...)
}

func (f *fakeEpisodes) attemptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts
}

var _ episode.EpisodeAccess = (*fakeEpisodes)(nil)

// registerGenEpisodes registers the GENERATED episodeAccess Activities under their
// registered names. The production Worker registers them for every construction
// execution, so the test env must too — otherwise the capture seam's append would fail
// as unregistered on EVERY workflow test rather than exercising the real path.
// eps is variadic purely so the ~dozen existing register* call sites stay untouched: a
// test that cares about episodes passes its recorder, every other test gets a silent one.
func registerGenEpisodes(env *testsuite.TestWorkflowEnvironment, eps []*fakeEpisodes) {
	var acc episode.EpisodeAccess = &fakeEpisodes{}
	if len(eps) > 0 && eps[0] != nil {
		acc = eps[0]
	}
	acts := &genActivities{Episodes: acc}
	env.RegisterActivityWithOptions(acts.EpisodesAppendEpisode, activity.RegisterOptions{Name: "episodeAccess.appendEpisode"})
}

// registerConstruct registers the per-activity child workflow + its Activities — ALL
// generated (B8 + follow-up): the pipeline/designSession-read/projectState-version/
// constructionTransition/gitStatus surfaces, each backed by the fakes.
func registerConstruct(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, eps ...*fakeEpisodes) {
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	registerGenPipeline(env, pipe)
	registerGenEpisodes(env, eps)
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
	// Phase-gate + per-activity construction-status records (fire only when gitOn;
	// the gate tests wire GitStatus so these must be registered).
	registerGenGitStatus(env, ps)
}

func registerPump(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, eps ...*fakeEpisodes) {
	env.RegisterWorkflowWithOptions(wf.PumpNextActivityWorkflow, workflow.RegisterOptions{Name: executionKindPump})
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	registerGenEpisodes(env, eps)
	// The pump now waits for child COMPLETION (self-cascade), so the per-activity
	// child runs end-to-end and ALL its activities must be registered.
	registerGenPipeline(env, pipe)
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
}

func registerSupervision(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, eps ...*fakeEpisodes) {
	registerSupervisionWithBus(env, wf, ps, pipe, &recordingSignalBus{}, eps...)
}

// registerSupervisionWithBus is registerSupervision with the messageBus.deliverSignal
// Activity backed by a caller-supplied bus — the pause branch relays the pause to the
// project's pump through it.
func registerSupervisionWithBus(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, bus messagebus.MessageBus, eps ...*fakeEpisodes) {
	env.RegisterWorkflowWithOptions(wf.ProjectSupervisionWorkflow, workflow.RegisterOptions{Name: executionKindProjectSupervision})
	registerGenPipeline(env, pipe)
	registerGenEpisodes(env, eps)
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
	acts := &genActivities{MessageBus: bus}
	env.RegisterActivityWithOptions(acts.MessageBusDeliverSignal, activity.RegisterOptions{Name: "messageBus.deliverSignal"})
}

// recordingSignalBus records every DeliverSignal and answers with err (nil ⇒
// delivered). Satisfies messagebus.MessageBus.
type recordingSignalBus struct {
	mu       sync.Mutex
	err      error
	targets  []messagebus.ExecutionID
	names    []messagebus.SignalName
	payloads []messagebus.ExecutionPayload
	// order, when set, receives "relay" on every DeliverSignal (callLog).
	order *callLog
	// onDeliver, when set, runs INSIDE the relay window — at DeliverSignal time, before
	// the answer returns — so a test can put other executions there.
	onDeliver func()
}

func (b *recordingSignalBus) DeliverSignal(_ fwra.Context, target messagebus.ExecutionID, name messagebus.SignalName, payload messagebus.ExecutionPayload) error {
	if b.onDeliver != nil {
		b.onDeliver()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.targets = append(b.targets, target)
	b.names = append(b.names, name)
	b.payloads = append(b.payloads, payload)
	b.order.add("relay")
	return b.err
}

// callLog is one ordered record of calls shared across several fakes, so a test can
// assert the ORDER of effects that land on DIFFERENT ports (state, bus, pipeline).
// nil-safe: a fake without a log records nothing.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(call string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call)
}

func (l *callLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.calls, "→")
}

// pauseRun is one ProjectSupervisionWorkflow pause-branch run against fakes that share
// one call-order log.
type pauseRun struct {
	err   error
	ps    *fakeProjectState
	pipe  *fakePipeline
	bus   *recordingSignalBus
	order *callLog
}

// runPauseBranch signals a pause to a fresh supervision workflow whose plan cancels one
// pipeline and records the pause. busErr is what the relay's DeliverSignal answers;
// setup (optional) adjusts the env before the run (e.g. an OnGetVersion override).
func runPauseBranchRig(busErr error, setup func(*testsuite.TestWorkflowEnvironment)) pauseRun {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	order := &callLog{}
	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}, order: order}
	pipe := &fakePipeline{order: order}
	bus := &recordingSignalBus{err: busErr, order: order}
	wf := newWorkflows(wfDeps{
		Review:       &fakeReview{},
		Intervention: &fakeIntervention{plan: intervention.PausePlan{PipelinesToCancel: []intervention.PipelineRef{"wf-C-1"}, RecordPaused: true}},
	})
	registerSupervisionWithBus(env, wf, ps, pipe, bus)
	if setup != nil {
		setup(env)
	}
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)
	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})
	return pauseRun{err: env.GetWorkflowError(), ps: ps, pipe: pipe, bus: bus, order: order}
}

func (b *recordingSignalBus) RegisterSchedule(fwra.Context, messagebus.ScheduleID, messagebus.ScheduleSpec) error {
	return nil
}

var _ messagebus.MessageBus = (*recordingSignalBus)(nil)

func registerReplanSweep(env *testsuite.TestWorkflowEnvironment, wf *workflows, ps *fakeProjectState) {
	env.RegisterWorkflowWithOptions(wf.ReplanSweepWorkflow, workflow.RegisterOptions{Name: executionKindReplanSweep})
	registerGenDesignSessionRead(env, ps)
}

// fakeProjectLister widens fakeFullProjectState with a SCRIPTED ListProjects — the
// one surface PumpSweepWorkflow's enumeration depends on. Every other method falls
// through to fakeFullProjectState's stubs (never exercised by the sweep itself).
type fakeProjectLister struct {
	fakeFullProjectState
	summaries []projectstate.ProjectSummary
}

func (f fakeProjectLister) ListProjects(fwra.Context, projectstate.OwnerScope) ([]projectstate.ProjectSummary, error) {
	return f.summaries, nil
}

var _ projectstate.ProjectStateAccess = fakeProjectLister{}

// registerPumpSweep registers PumpSweepWorkflow + projectStateAccess.listProjects
// (backed by lister) alongside everything PumpNextActivityWorkflow needs for its
// per-project child dispatch (registerPump's own set) — the sweep starts that exact
// workflow as an ABANDON-policy child per eligible project.
func registerPumpSweep(env *testsuite.TestWorkflowEnvironment, wf *workflows, lister fakeProjectLister, ps *fakeProjectState, pipe agenticjob.AgenticJobAccess, eps ...*fakeEpisodes) {
	env.RegisterWorkflowWithOptions(wf.PumpSweepWorkflow, workflow.RegisterOptions{Name: executionKindPumpSweep})
	acts := &genActivities{ProjectState: lister}
	env.RegisterActivityWithOptions(acts.ProjectStateListProjects, activity.RegisterOptions{Name: "projectStateAccess.listProjects"})
	registerPump(env, wf, ps, pipe, eps...)
}

func sampleActivity() constructionActivity {
	return constructionActivity{
		ActivityID:  "C-XYZ",
		Kind:        activityKindConstruction,
		ComponentID: "comp-1",
		Layer:       "engine",
		Phases:      projectstate.ProfileFor(projectstate.ActivityTypeService, 0).PhaseIDs(),
	}
}

// ---- Tests: per-activity spine (ConstructActivityWorkflow) ------------------

// The happy-path UC3 spine: cast → dispatch → submit/observe(succeeded) → stage →
// review(empty set) → recordChangeReviewed → recordActivityExited(Completed).
func Test_Construct_HappyPath_RecordsReviewedAndExited(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.reviewed) != 1 || ps.reviewed[0] != "C-XYZ" {
		t.Fatalf("want one recordChangeReviewed(C-XYZ), got %v", ps.reviewed)
	}
	if len(ps.exited) != 1 || ps.exited[0].activityID != "C-XYZ" || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want one recordActivityExited(C-XYZ, Completed), got %v", ps.exited)
	}
	// The App-A phase-walk dispatches one pipeline per phase (Requirements →
	// Detailed Design → Test Plan → Construction → Integration).
	if len(pipe.submitted) != len(sampleActivity().Phases) {
		t.Fatalf("want %d pipeline submits (one per App-A phase), got %d", len(sampleActivity().Phases), len(pipe.submitted))
	}
}

// Test_Construct_VenueSwitch_TargetsProjectRepo pins the gh-mode venue switch (B5):
// when the per-project Repo resolver resolves a RepoRef, every construction pipeline
// dispatch carries the DECODED {Owner,Name} TargetRepo + the construct workflow file
// so the agentic construction job runs in the PROJECT's own repo, not the central repo.
func Test_Construct_VenueSwitch_TargetsProjectRepo(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		// Repo resolves — the venue switch fires INDEPENDENTLY of the PR rail
		// (RailEnabled/GitStatus unwired here, so the git-forward slice stays dormant).
		Repo: func(_ ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|acme/gtdapp"), true
		},
	})
	registerConstruct(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(pipe.submitted) == 0 {
		t.Fatal("expected at least one pipeline submit")
	}
	for i, spec := range pipe.submitted {
		if spec.TargetRepo.Owner != "acme" || spec.TargetRepo.Name != "gtdapp" {
			t.Fatalf("submit[%d]: want TargetRepo{acme,gtdapp}, got %+v", i, spec.TargetRepo)
		}
		if spec.WorkflowFile != constructWorkflowFileName {
			t.Fatalf("submit[%d]: want WorkflowFile %q, got %q", i, constructWorkflowFileName, spec.WorkflowFile)
		}
	}
}

// Test_Construct_VenueSwitch_FallbackToCentralRepo pins the legacy fallback: when the
// Repo resolver is absent (nil) OR does not resolve, the dispatch carries a ZERO
// TargetRepo + empty WorkflowFile, so agenticJobAccess.resolveTarget falls
// back to the configured central construction repo (the pre-B5 behavior, preserved for
// unresolvable projects).
func Test_Construct_VenueSwitch_FallbackToCentralRepo(t *testing.T) {
	cases := []struct {
		name string
		repo func(ProjectID) (sourcecontrol.RepoRef, bool)
	}{
		{"resolver absent", nil},
		{"resolver misses", func(_ ProjectID) (sourcecontrol.RepoRef, bool) { return sourcecontrol.RepoRef(""), false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ts testsuite.WorkflowTestSuite
			env := ts.NewTestWorkflowEnvironment()

			ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
			pipe := &fakePipeline{phase: PipelineSucceeded}
			wf := newWorkflows(wfDeps{
				Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
				Review:       &fakeReview{},
				Repo:         tc.repo,
			})
			registerConstruct(env, wf, ps, pipe)

			env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
				ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
			})

			if !env.IsWorkflowCompleted() {
				t.Fatal("workflow did not complete")
			}
			if err := env.GetWorkflowError(); err != nil {
				t.Fatalf("workflow error: %v", err)
			}
			if len(pipe.submitted) == 0 {
				t.Fatal("expected at least one pipeline submit")
			}
			for i, spec := range pipe.submitted {
				if !agenticjob.RepoTargetIsZero(spec.TargetRepo) {
					t.Fatalf("submit[%d]: want ZERO TargetRepo (central-repo fallback), got %+v", i, spec.TargetRepo)
				}
				if spec.WorkflowFile != "" {
					t.Fatalf("submit[%d]: want empty WorkflowFile (central-repo fallback), got %q", i, spec.WorkflowFile)
				}
			}
		})
	}
}

// runPumpWith builds the fakePipeline-backed Temporal test environment, executes
// ConstructActivityWorkflow with the supplied activity, and returns the pipeline
// double so the caller can inspect pipe.submitted.
func runPumpWith(t *testing.T, act constructionActivity) *fakePipeline {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: ActivityID(act.ActivityID), Activity: act,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	return pipe
}

// Test_Construct_TestingPlanWalksThreePhases proves that a testing-plan activity
// (3 canonical phases) drives exactly 3 pipeline submissions, not the service 5.
func Test_Construct_TestingPlanWalksThreePhases(t *testing.T) {
	act := constructionActivity{
		ActivityID:  "N-STP",
		Kind:        activityKindConstruction,
		ComponentID: "system",
		Phases:      projectstate.ProfileFor(projectstate.ActivityTypeTesting, projectstate.TestVariantPlan).PhaseIDs(),
	}
	if len(act.Phases) != 3 {
		t.Fatalf("precondition: testing-plan phases = %d, want 3", len(act.Phases))
	}
	pipe := runPumpWith(t, act)
	if len(pipe.submitted) != 3 {
		t.Fatalf("submitted %d pipelines, want 3", len(pipe.submitted))
	}
}

// architectOnly skips dispatch + pipeline and awaits an operator override; a Skip
// override exits the activity with the operator-skip outcome and no worker dispatch.
// A failed pipeline → variance → DecideOnVariance(Takeover): the takeover re-dispatches;
// with the pipeline now succeeding the activity completes normally on the next loop. (The
// prior phase pipeline is already terminal at intervention time, so takeover abandons
// nothing — the LLM worker-cancel seam is retired under agentic-everywhere.)
func Test_Construct_PipelineFailed_Takeover_ThenCompletes(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	// The pipeline fails on the first run, then a flippable fake makes it succeed.
	pipe := &flippablePipeline{first: PipelineFailed, rest: PipelineSucceeded}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceTakeover},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-PF", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want a completed exit after takeover+re-dispatch, got %v", ps.exited)
	}
}

// flippablePipeline serves `first` on the first terminal observation, then `rest`.
type flippablePipeline struct {
	mu        sync.Mutex
	first     PipelinePhase
	rest      PipelinePhase
	submits   int
	cancelled []agenticjob.PipelineHandle
}

func (p *flippablePipeline) SubmitAgenticJob(_ fwra.Context, _ agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submits++
	return agenticjob.PipelineHandle("wf"), nil
}

func (p *flippablePipeline) ObserveAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.submits <= 1 {
		return agenticjob.PipelineObservation{Phase: contractPipelinePhase(p.first), Diagnostic: "boom"}, nil
	}
	return agenticjob.PipelineObservation{Phase: contractPipelinePhase(p.rest)}, nil
}

func (p *flippablePipeline) CancelAgenticJob(_ fwra.Context, handle agenticjob.PipelineHandle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelled = append(p.cancelled, handle)
	return nil
}

var _ agenticjob.AgenticJobAccess = (*flippablePipeline)(nil)

// The §6.5 Conflict discipline: a recordChangeReviewed that returns fwra.Conflict
// twice before succeeding drives the workflow-level re-read→re-apply loop; the
// activity still completes (reviewed + exited recorded).
func Test_Construct_ConflictOnRecord_ReReadReApply_Succeeds(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}, conflictFirst: 2}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-CONF", Activity: sampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.reviewed) != 1 {
		t.Fatalf("conflict loop must converge to exactly one recorded reviewed, got %v", ps.reviewed)
	}
	if len(ps.exited) != 1 {
		t.Fatalf("want one recorded exit after the conflict loop, got %v", ps.exited)
	}
}

// ---- Tests: pump (PumpNextActivityWorkflow) ---------------------------------

// No eligible activity ⇒ PumpResult{Dispatched:false} — a normal quiet tick, not
// an error (no NextEligibleActivity helper wired).
func Test_Pump_NoEligibleActivity_QuietTick(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: nil,
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: ProjectID(ps.project.ID)})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump error: %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("want Dispatched:false on a quiet tick, got %+v", res)
	}
}

// A brand-new project (ReadProject NotFound) is also a quiet tick, not an error.
func Test_Pump_ProjectNotFound_QuietTick(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{notFound: true}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: ProjectID(uuid.NewString())})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump error: %v", err)
	}
	var res PumpResult
	_ = env.GetWorkflowResult(&res)
	if res.Dispatched {
		t.Fatal("a not-found project must be a quiet tick")
	}
}

// An eligible activity ⇒ the pump runs the per-activity child to COMPLETION, then
// SELF-CASCADES via ContinueAsNew (Task 3). The test env surfaces ContinueAsNew as a
// *workflow.ContinueAsNewError carrying the next pumpInput. The child's spine ran
// end-to-end (one reviewed + one completed exit recorded).
func Test_Pump_EligibleActivity_RunsChild_ThenContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
			return pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if !env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	// A successful eligible dispatch self-cascades: the terminal "error" is a
	// ContinueAsNewError carrying the next tick's pumpInput (NOT a real failure).
	err := env.GetWorkflowError()
	var canErr *workflow.ContinueAsNewError
	if !errors.As(err, &canErr) {
		t.Fatalf("want a ContinueAsNewError (self-cascade), got %v", err)
	}
	// The child ran end-to-end exactly once.
	if len(ps.exited) != 1 || ps.exited[0].activityID != "C-XYZ" {
		t.Fatalf("want the child to have recorded one exit for C-XYZ, got %v", ps.exited)
	}
}

// An eligible dispatch surfaces THIS tick's synchronous dispatch decision via the
// queryPumpDispatch Query — the value ExecuteNextActivity returns to a scheduler-style
// caller WITHOUT awaiting the background self-cascade drain. Even though the pump
// self-cascades (ends this run in ContinueAsNew), its final per-run state carries the
// decided dispatch of the eligible activity.
func Test_Pump_EligibleActivity_SurfacesSyncDispatchDecision(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
			return pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	enc, err := env.QueryWorkflow(queryPumpDispatch)
	if err != nil {
		t.Fatalf("query pump dispatch decision: %v", err)
	}
	var d pumpDispatch
	if err := enc.Get(&d); err != nil {
		t.Fatalf("decode pump dispatch decision: %v", err)
	}
	if !d.Decided {
		t.Fatalf("want a decided dispatch decision, got %+v", d)
	}
	if !d.Dispatched || d.ActivityID == nil || *d.ActivityID != "C-XYZ" {
		t.Fatalf("want Dispatched:true for C-XYZ, got %+v", d)
	}
}

// A drained (quiescent) tick surfaces a DECIDED, non-dispatching decision via the
// queryPumpDispatch Query — the {Dispatched:false} answer ExecuteNextActivity returns
// on a quiet tick.
func Test_Pump_DrainedNetwork_SurfacesQuiescentDecision(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
			return pumpSelection{Verdict: verdictQuiescent}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	enc, err := env.QueryWorkflow(queryPumpDispatch)
	if err != nil {
		t.Fatalf("query pump dispatch decision: %v", err)
	}
	var d pumpDispatch
	if err := enc.Get(&d); err != nil {
		t.Fatalf("decode pump dispatch decision: %v", err)
	}
	if !d.Decided {
		t.Fatalf("want a decided decision on a drained tick, got %+v", d)
	}
	if d.Dispatched || d.ActivityID != nil {
		t.Fatalf("want a quiescent decision (Dispatched:false, nil activity), got %+v", d)
	}
}

// A drained network (nextEligible returns false) ⇒ the pump goes QUIET WITHOUT
// ContinueAsNew (the cascade ends) — Dispatched:false, no error.
func Test_Pump_DrainedNetwork_QuietNoContinueAsNew(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
			return pumpSelection{Verdict: verdictQuiescent} // network drained
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("drained pump must be a clean quiet tick, got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("a drained network must go quiet (Dispatched:false), got %+v", res)
	}
}

// A blocked activity is recorded as a TERMINAL, app-visible failure — not a silent
// skip, and not a workflow error (a failed Temporal execution is invisible in the
// console; the head-state record IS the escalation).
func Test_Pump_BlockedActivity_RecordsTerminalFailure(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	const reason = `activity C-TLM names component "todo-list-managr", which is not in the committed systemDesign — amend the committed activityList`
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
			return pumpSelection{
				Verdict:              verdictBlocked,
				BlockedActivityID:    "C-TLM",
				BlockedFailureReason: projectstate.ComponentUnresolved,
				BlockedReason:        reason,
			}
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if !env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	// The cascade ENDS — no ContinueAsNew, and no error surfaced to Temporal.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a blocked activity must not fail the workflow, got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("want Dispatched:false, got %+v", res)
	}
	// The durable, operator-visible record: the offending activity, the new terminal
	// reason, and the operator-facing detail carried through VERBATIM (it names both
	// the activity and the unresolvable component id — that is what a human reads off
	// the failed node).
	if len(ps.failed) != 1 {
		t.Fatalf("want exactly one failure record, got %v", ps.failed)
	}
	want := failCall{activityID: "C-TLM", reason: projectstate.ComponentUnresolved, detail: reason}
	if ps.failed[0] != want {
		t.Fatalf("failure record mismatch:\n got %+v\nwant %+v", ps.failed[0], want)
	}
	// Nothing was dispatched: no child recorded an exit.
	if len(ps.exited) != 0 {
		t.Fatalf("no child should have run, got %v", ps.exited)
	}
	// The façade's synchronous read sees a DECIDED, non-dispatching tick — the pump
	// must not look like it is still deciding.
	enc, err := env.QueryWorkflow(queryPumpDispatch)
	if err != nil {
		t.Fatalf("query pump dispatch decision: %v", err)
	}
	var d pumpDispatch
	if err := enc.Get(&d); err != nil {
		t.Fatalf("decode pump dispatch decision: %v", err)
	}
	if d != (pumpDispatch{Decided: true}) {
		t.Fatalf("want a decided non-dispatch decision, got %+v", d)
	}
}

// Test_Pump_DependencyUnresolved_RecordsDistinctFailureReason proves a dangling
// dependency id records DependencyUnresolved — a DIFFERENT FailureReason ordinal from
// ComponentUnresolved (Test_Pump_BlockedActivity_RecordsTerminalFailure above) and from
// DependencyCycle (below) — asserted on the recorded failure record itself, not just a
// log line. Mirrors Test_Pump_BlockedActivity_RecordsTerminalFailure's shape, wired
// through nextEligibleActivity's real dependency-defect classification instead of a
// hand-built pumpSelection, so it also exercises projectstate.ResolveDependencySatisfied end to end.
func Test_Pump_DependencyUnresolved_RecordsDistinctFailureReason(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention:         &fakeIntervention{},
		Review:               &fakeReview{},
		NextEligibleActivity: nextEligibleActivity,
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "N-DOC", DependsOn: []string{"M-GHOST"}},
			},
			nil,
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "N-DOC", WorkerClass: "system-architect", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
	}
	ps.project.Network = proj.Network
	ps.project.ActivityList = proj.ActivityList
	ps.project.SystemDesign = proj.SystemDesign

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a blocked activity must not fail the workflow, got %v", err)
	}
	if len(ps.failed) != 1 {
		t.Fatalf("want exactly one failure record, got %v", ps.failed)
	}
	got := ps.failed[0]
	if got.activityID != "N-DOC" {
		t.Fatalf("want failure recorded against N-DOC, got %+v", got)
	}
	if got.reason != projectstate.DependencyUnresolved {
		t.Fatalf("want DependencyUnresolved, got %v", got.reason)
	}
	if !strings.Contains(got.detail, "M-GHOST") {
		t.Fatalf("failure detail must name the dangling id, got %q", got.detail)
	}
}

// Test_Pump_DependencyCycle_RecordsDistinctFailureReason proves a milestone dependency
// cycle records DependencyCycle — distinct from both ComponentUnresolved and
// DependencyUnresolved — asserted on the recorded failure record.
func Test_Pump_DependencyCycle_RecordsDistinctFailureReason(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention:         &fakeIntervention{},
		Review:               &fakeReview{},
		NextEligibleActivity: nextEligibleActivity,
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	proj := projectstate.Project{
		Phase: projectstate.PhaseConstruction,
		Network: makeCommittedNetworkWithMilestones(
			[]projectstate.NetworkDependency{
				{Activity: "N-DOC", DependsOn: []string{"M-X"}},
			},
			[]projectstate.NetworkMilestone{
				{ID: "M-X", Name: "M-X", DependsOn: []string{"M-Y"}},
				{ID: "M-Y", Name: "M-Y", DependsOn: []string{"M-X"}},
			},
		),
		ActivityList: makeCommittedActivityList([]projectstate.ActivityItem{
			{Name: "N-DOC", WorkerClass: "system-architect", Coding: false},
		}),
		SystemDesign: makeCommittedSystemDesign(nil),
	}
	ps.project.Network = proj.Network
	ps.project.ActivityList = proj.ActivityList
	ps.project.SystemDesign = proj.SystemDesign

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a blocked activity must not fail the workflow, got %v", err)
	}
	if len(ps.failed) != 1 {
		t.Fatalf("want exactly one failure record, got %v", ps.failed)
	}
	got := ps.failed[0]
	if got.activityID != "N-DOC" {
		t.Fatalf("want failure recorded against N-DOC, got %+v", got)
	}
	if got.reason != projectstate.DependencyCycle {
		t.Fatalf("want DependencyCycle, got %v", got.reason)
	}
	if !strings.Contains(got.detail, "cycle") {
		t.Fatalf("failure detail must call out the cycle, got %q", got.detail)
	}
}

// A pause Signal delivered to the (cascading) pump halts it BEFORE any dispatch: the
// pump goes quiet WITHOUT ContinueAsNew and WITHOUT starting a child, even though an
// activity is eligible. The resume path re-triggers the pump.
func Test_Pump_PauseSignal_HaltsCascade_NoDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
			return pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()} // an activity IS eligible — but the pause wins
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	// Deliver the pause Signal so it is already queued when the pump checks (the pump's
	// non-blocking ReceiveAsync observes it at the top, before any dispatch).
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, 0)

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("paused pump must be a clean quiet tick (no ContinueAsNew), got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if res.Dispatched {
		t.Fatalf("a paused pump must NOT dispatch, got %+v", res)
	}
	// The child's spine never ran: nothing recorded exited.
	if len(ps.exited) != 0 {
		t.Fatalf("a paused pump must not run any activity, got exits %v", ps.exited)
	}
}

// PAUSE-DELIVERY CO-GATE (architect pump ruling, 2026-09-12). A pause that lands while
// the pump is parked in child.Get (the current activity still running) must stop the
// cascade AFTER that activity: the pump drains its signal channel before the
// self-cascade's ContinueAsNew and goes quiet, so no further child starts. Before the
// fix the pump read its channel only at run start; the buffered pause was discarded by
// ContinueAsNew and the next run dispatched the next activity. The pause is delivered in
// the exact wire form the supervision relay sends (pumpPausePayload's bytes through
// messageBus — binary/plain), which also pins that the pump's receive does not silently
// drop that encoding.
func Test_Pump_PauseDuringChildGet_StopsCascadeAfterCurrentActivity(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
			return pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()} // the frontier never drains on its own
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})

	// Hold the per-activity child open for 10 minutes of workflow time, so the pump is
	// parked in child.Get when the pause lands at minute 1.
	var childStarts int
	env.OnWorkflow(executionKindConstructActivity, mock.Anything, mock.Anything).
		After(10 * time.Minute).
		Run(func(mock.Arguments) { childStarts++ }).
		Return(nil)

	payload, err := pumpPausePayload(pid, "operator halt")
	if err != nil {
		t.Fatalf("encode pause payload: %v", err)
	}
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, payload.Bytes)
	}, time.Minute)

	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})

	if !env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a pause during child.Get must end the cascade quietly (no ContinueAsNew into a next dispatch), got %v", err)
	}
	var res PumpResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	if !res.Dispatched || res.ActivityID == nil || *res.ActivityID != "C-XYZ" {
		t.Fatalf("the run dispatched C-XYZ before the pause; want that reported, got %+v", res)
	}
	if childStarts != 1 {
		t.Fatalf("want exactly the one in-flight child (no further child after the pause), got %d", childStarts)
	}
}

// cascadingPumpRig is a pump whose frontier never drains on its own, with the
// per-activity child mocked to run for childRun of workflow time (counted in
// childStarts). readDelay > 0 delays the head-state read (designSessionAccess.
// readProjectOnBranch) by that much workflow time, delegating to the registered fake.
type cascadingPumpRig struct {
	env         *testsuite.TestWorkflowEnvironment
	pid         ProjectID
	ps          *fakeProjectState
	childStarts *int
}

// recordedPause is a newCascadingPumpRig option: the project's head-state carries the
// operator's RECORDED pause (as RecordOperatorPaused leaves it). The rig's read goes
// through designSessionAccess.ReadProjectOnBranch → EncodeProject → the pump's Decode,
// so the flag reaches the pump only if the envelope carries it.
func recordedPause(p *projectstate.Project) {
	p.OperatorPaused = true
	p.PauseReason = "operator halt"
}

func newCascadingPumpRig(childRun, readDelay time.Duration, opts ...func(*projectstate.Project)) cascadingPumpRig {
	return newPumpRig(pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()}, childRun, readDelay, opts...)
}

// newPumpRig is newCascadingPumpRig with the frontier's selection supplied (e.g. a
// BLOCKED verdict).
func newPumpRig(sel pumpSelection, childRun, readDelay time.Duration, opts ...func(*projectstate.Project)) cascadingPumpRig {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	for _, opt := range opts {
		opt(&ps.project)
	}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
			return sel
		},
	})
	registerPump(env, wf, ps, &fakePipeline{phase: PipelineSucceeded})
	starts := new(int)
	env.OnWorkflow(executionKindConstructActivity, mock.Anything, mock.Anything).
		After(childRun).
		Run(func(mock.Arguments) { *starts++ }).
		Return(nil)
	if readDelay > 0 {
		read := &genActivities{DesignSession: projectstate.NewDesignSessionAccess(fakeFullProjectState{ps})}
		env.OnActivity("designSessionAccess.readProjectOnBranch", mock.Anything, mock.Anything, mock.Anything).
			After(readDelay).
			Return(read.DesignSessionReadProjectOnBranch)
	}
	return cascadingPumpRig{env: env, pid: pid, ps: ps, childStarts: starts}
}

// pauseAt delivers a pause to the pump at workflow time `at`, in the wire form the
// supervision relay sends (pumpPausePayload's bytes, binary/plain).
func (r cascadingPumpRig) pauseAt(t *testing.T, at time.Duration) {
	t.Helper()
	payload, err := pumpPausePayload(r.pid, "operator halt")
	if err != nil {
		t.Fatalf("encode pause payload: %v", err)
	}
	r.env.RegisterDelayedCallback(func() {
		r.env.SignalWorkflow(signalOperatorPauseRequested, payload.Bytes)
	}, at)
}

// run starts the pump sweep-shaped (OperatorDriven false); runInput takes any input.
func (r cascadingPumpRig) run(t *testing.T) (PumpResult, error) {
	t.Helper()
	return r.runInput(t, pumpInput{ProjectID: r.pid})
}

func (r cascadingPumpRig) runInput(t *testing.T, in pumpInput) (PumpResult, error) {
	t.Helper()
	r.env.ExecuteWorkflow(executionKindPump, in)
	if !r.env.IsWorkflowCompleted() {
		t.Fatal("pump did not complete")
	}
	if err := r.env.GetWorkflowError(); err != nil {
		return PumpResult{}, err
	}
	var res PumpResult
	if err := r.env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump result: %v", err)
	}
	return res, nil
}

func isContinueAsNew(err error) bool {
	var canErr *workflow.ContinueAsNewError
	return errors.As(err, &canErr)
}

// FIX ROUND I1 (the reviewer's probe). readProject is an Activity, so a pause can land
// AFTER the run-start check and BEFORE the dispatch. It must not dispatch a NEW
// activity — nothing would cancel it (the pause plan's PipelinesToCancel is empty).
// Pause at 1m while the head-state read is held to 2m: the pump goes quiet, no child.
// THE BOUND this pins: the pause is DELIVERED BEFORE the dispatching workflow task
// starts (the task that runs once readProject completes), so signal check 2 sees it. A
// pause arriving DURING that task is honoured at signal check 3, after exactly one
// activity (Test_Pump_PauseDuringChildGet_StopsCascadeAfterCurrentActivity).
func Test_Pump_PauseDuringReadProject_NoNewDispatch(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 2*time.Minute)
	rig.pauseAt(t, time.Minute)

	res, err := rig.run(t)
	if err != nil {
		t.Fatalf("a pause during readProject must end the run quietly, got %v", err)
	}
	if res.Dispatched || *rig.childStarts != 0 {
		t.Fatalf("a pause that landed before dispatch must dispatch nothing, got %+v with %d child start(s)", res, *rig.childStarts)
	}
}

// M1 (version gate "pump-pause-before-dispatch", DefaultVersion branch). A pre-change
// execution keeps the old sequence: it dispatches straight after readProject even with
// a pause buffered (the post-child drain, at its current version, then quiets it).
func Test_Pump_PreDispatchGate_DefaultVersion_KeepsOldDispatch(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 2*time.Minute)
	rig.env.OnGetVersion("pump-pause-before-dispatch", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	rig.pauseAt(t, time.Minute)

	res, err := rig.run(t)
	if err != nil {
		t.Fatalf("want the drain to quiet the run after the old-sequence dispatch, got %v", err)
	}
	if !res.Dispatched || *rig.childStarts != 1 {
		t.Fatalf("a pre-change execution must dispatch as it always did, got %+v with %d child start(s)", res, *rig.childStarts)
	}
}

// M1 (version gate "pump-drain-pause-before-continue-as-new", DefaultVersion branch). A
// pre-change execution continues-as-new straight after the Sleep, as it always did.
func Test_Pump_DrainGate_DefaultVersion_ContinuesAsNew(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 0)
	rig.env.OnGetVersion("pump-drain-pause-before-continue-as-new", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	rig.pauseAt(t, time.Minute)

	_, err := rig.run(t)
	if !isContinueAsNew(err) {
		t.Fatalf("a pre-change execution must continue-as-new (no drain), got %v", err)
	}
	if *rig.childStarts != 1 {
		t.Fatalf("want the one child, got %d", *rig.childStarts)
	}
}

// M1/M2 (version gate "pump-pause-decode-any", DefaultVersion branch). A pre-change
// execution keeps the OLD struct decode at run start, which drops a binary/plain
// (relayed) pause — ReceiveAsync consumes it as corrupted — so the run dispatches and
// continues-as-new exactly as its history recorded.
func Test_Pump_DecodeGate_DefaultVersion_KeepsOldStructDecode(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 0)
	rig.env.OnGetVersion("pump-pause-decode-any", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	rig.pauseAt(t, 0)

	_, err := rig.run(t)
	if !isContinueAsNew(err) {
		t.Fatalf("a pre-change execution must not see the byte pause (old struct decode), got %v", err)
	}
	if *rig.childStarts != 1 {
		t.Fatalf("want the old-sequence dispatch of one child, got %d", *rig.childStarts)
	}
}

// I2 test 3 (architect ruling, 2026-09-12). A SWEEP-started pump (OperatorDriven
// false) on a project whose pause is RECORDED — no signal at all, an activity eligible
// — must go quiet at the recorded-pause gate: no child, a clean completion (no
// ContinueAsNew), a decided nothing-dispatched answer, and no blocked-activity failure
// record. The read goes through EncodeProject/Decode (see recordedPause), so an
// envelope that drops OperatorPaused fails this test.
func Test_Pump_SweepStarted_RecordedPause_NoDispatch(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 0, recordedPause)

	res, err := rig.run(t)
	if err != nil {
		t.Fatalf("a sweep-started pump must honour the recorded pause quietly (no ContinueAsNew), got %v", err)
	}
	if res.Dispatched || *rig.childStarts != 0 {
		t.Fatalf("a recorded pause must stop a sweep-started pump before dispatch, got %+v with %d child start(s)", res, *rig.childStarts)
	}
	enc, err := rig.env.QueryWorkflow(queryPumpDispatch)
	if err != nil {
		t.Fatalf("query pump dispatch decision: %v", err)
	}
	var d pumpDispatch
	if err := enc.Get(&d); err != nil {
		t.Fatalf("decode pump dispatch decision: %v", err)
	}
	if !d.Decided || d.Dispatched {
		t.Fatalf("want a decided, nothing-dispatched answer, got %+v", d)
	}
	if len(rig.ps.failed) != 0 {
		t.Fatalf("the recorded-pause gate must not write a blocked-activity failure record, got %v", rig.ps.failed)
	}
}

// FIX ROUND 3, Minor 2. The recorded-pause gate sits BEFORE `switch sel.Verdict`: on a
// paused project a sweep-started pump must not act on the frontier at all — not even the
// blocked-verdict branch's durable ActivityConstructionFailed record, which would
// otherwise land while the operator has construction paused.
func Test_Pump_SweepStarted_RecordedPause_BlockedFrontier_NoFailureRecord(t *testing.T) {
	rig := newPumpRig(pumpSelection{
		Verdict:              verdictBlocked,
		BlockedActivityID:    "C-TLM",
		BlockedFailureReason: projectstate.ComponentUnresolved,
		BlockedReason:        "activity C-TLM names a component not in the committed systemDesign",
	}, 10*time.Minute, 0, recordedPause)

	res, err := rig.run(t)
	if err != nil {
		t.Fatalf("a paused, sweep-started pump must go quiet before the verdicts, got %v", err)
	}
	if res.Dispatched || *rig.childStarts != 0 {
		t.Fatalf("nothing may be dispatched, got %+v with %d child start(s)", res, *rig.childStarts)
	}
	if len(rig.ps.failed) != 0 {
		t.Fatalf("the recorded-pause gate must precede the blocked branch — no failure record, got %v", rig.ps.failed)
	}
}

// windowPumps is what the two pumps that ran INSIDE the relay window did.
type windowPumps struct {
	ran                 bool
	cascade             PumpResult
	cascadeErr          error
	sweepErr            error
	sweepPumped         []ProjectID
	sweepChildCompleted bool
	sweepChild          PumpResult
	sweepChildErr       error
	childStarts         int
}

// runPumpsInWindow runs, against the SAME store, the two pumps that can read head-state
// inside the pause's relay window: (a) the cascading pump's next run — a sweep-started
// cascade after ContinueAsNew, so OperatorDriven is false; and (b) a NEW pump that
// PumpSweepWorkflow starts from a STALE listing (the project not yet shown paused). Both
// see an eligible activity; the per-activity child is mocked and counted.
func runPumpsInWindow(ps *fakeProjectState, pid ProjectID) windowPumps {
	out := windowPumps{ran: true}
	newPumpWorkflows := func() *workflows {
		return newWorkflows(wfDeps{
			Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
			Review:       &fakeReview{},
			NextEligibleActivity: func(_ projectstate.Project, _ eligibilityRule) pumpSelection {
				return pumpSelection{Verdict: verdictDispatch, Activity: sampleActivity()}
			},
		})
	}
	countChildren := func(env *testsuite.TestWorkflowEnvironment) {
		env.OnWorkflow(executionKindConstructActivity, mock.Anything, mock.Anything).
			After(time.Minute).
			Run(func(mock.Arguments) { out.childStarts++ }).
			Return(nil)
	}

	var cascadeSuite testsuite.WorkflowTestSuite
	cascadeEnv := cascadeSuite.NewTestWorkflowEnvironment()
	registerPump(cascadeEnv, newPumpWorkflows(), ps, &fakePipeline{phase: PipelineSucceeded})
	countChildren(cascadeEnv)
	cascadeEnv.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: pid})
	if out.cascadeErr = cascadeEnv.GetWorkflowError(); out.cascadeErr == nil {
		_ = cascadeEnv.GetWorkflowResult(&out.cascade)
	}

	var sweepSuite testsuite.WorkflowTestSuite
	sweepEnv := sweepSuite.NewTestWorkflowEnvironment()
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries:            []projectstate.ProjectSummary{{ProjectID: projectstate.ProjectID(pid), Phase: projectstate.PhaseConstruction}},
	}
	registerPumpSweep(sweepEnv, newPumpWorkflows(), lister, ps, &fakePipeline{phase: PipelineSucceeded})
	countChildren(sweepEnv)
	// The test env stops when its ROOT (the sweep) completes, so the ABANDON-policy child
	// pump the sweep started is not run to completion there. Capture the child's exact
	// start input instead, and run THAT pump to completion against the same store below.
	var sweepChildInput *pumpInput
	sweepEnv.SetOnChildWorkflowStartedListener(func(info *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
		if info.WorkflowType.Name != executionKindPump {
			return
		}
		var in pumpInput
		if err := args.Get(&in); err == nil {
			sweepChildInput = &in
		}
	})
	sweepEnv.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})
	if out.sweepErr = sweepEnv.GetWorkflowError(); out.sweepErr == nil {
		var res pumpSweepResult
		_ = sweepEnv.GetWorkflowResult(&res)
		out.sweepPumped = res.PumpedProjects
	}
	if sweepChildInput == nil {
		return out
	}

	var childSuite testsuite.WorkflowTestSuite
	childEnv := childSuite.NewTestWorkflowEnvironment()
	registerPump(childEnv, newPumpWorkflows(), ps, &fakePipeline{phase: PipelineSucceeded})
	countChildren(childEnv)
	childEnv.ExecuteWorkflow(executionKindPump, *sweepChildInput)
	out.sweepChildCompleted = childEnv.IsWorkflowCompleted()
	if out.sweepChildErr = childEnv.GetWorkflowError(); out.sweepChildErr == nil {
		_ = childEnv.GetWorkflowResult(&out.sweepChild)
	}
	return out
}

// FIX ROUND 3, Minor 3: the recorded-pause race, END TO END in one run. Supervision
// RECORDS the pause; then, INSIDE the relay window (the bus's DeliverSignal), the
// cascading pump's next run and a new sweep-started pump both read the SAME store
// (runPumpsInWindow); the relay then answers NotFound (no pump left running). Both pumps
// must go quiet with ZERO children, and supervision must succeed. It fails if record and
// relay are swapped (the pumps would read an unpaused store) or if the envelope drops
// OperatorPaused (the pumps' Decode could not see it).
func Test_PauseRace_PumpsReadingInsideTheRelayWindow_GoQuiet(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	var window windowPumps
	bus := &recordingSignalBus{err: fwra.New(fwra.NotFound, "messagebus: no execution with that id")}
	bus.onDeliver = func() { window = runPumpsInWindow(ps, pid) }
	wf := newWorkflows(wfDeps{
		Review:       &fakeReview{},
		Intervention: &fakeIntervention{plan: intervention.PausePlan{PipelinesToCancel: []intervention.PipelineRef{"wf-C-1"}, RecordPaused: true}},
	})
	registerSupervisionWithBus(env, wf, ps, pipe, bus)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)
	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("supervision must succeed (the relay's NotFound is tolerated), got %v", err)
	}
	if !window.ran {
		t.Fatal("the relay window never opened — DeliverSignal was not reached")
	}
	if window.cascadeErr != nil || window.cascade.Dispatched {
		t.Fatalf("the cascading pump's next run must go quiet on the recorded pause, got result %+v err %v", window.cascade, window.cascadeErr)
	}
	if window.sweepErr != nil || len(window.sweepPumped) != 1 {
		t.Fatalf("the sweep (stale listing) must start its child pump, got pumped %v err %v", window.sweepPumped, window.sweepErr)
	}
	if !window.sweepChildCompleted || window.sweepChildErr != nil || window.sweepChild.Dispatched {
		t.Fatalf("the sweep-started pump must run and go quiet, got completed=%v result %+v err %v",
			window.sweepChildCompleted, window.sweepChild, window.sweepChildErr)
	}
	if window.childStarts != 0 {
		t.Fatalf("no pump may dispatch inside the relay window, got %d child start(s)", window.childStarts)
	}
	if len(ps.paused) != 1 || len(pipe.cancelled) != 1 {
		t.Fatalf("want the pause recorded and the pipeline cancelled, got paused=%v cancels=%d", ps.paused, len(pipe.cancelled))
	}
}

// I2 test 4, PINNED AT v1. An execution that recorded pump-honors-recorded-pause v1 keeps
// the I2 semantics: an OPERATOR-driven pump (Begin, before B1.7) ignores the recorded
// pause and dispatches as usual. v2 removes the exemption
// (Test_Pump_V2_RecordedPauseBindsAnOperatorDrivenPump).
func Test_Pump_OperatorDriven_RecordedPause_StillDispatches(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 0, recordedPause)
	rig.env.OnGetVersion(changePumpHonorsRecordedPause, workflow.DefaultVersion, 2).Return(workflow.Version(1))

	_, err := rig.runInput(t, pumpInput{ProjectID: rig.pid, OperatorDriven: true})
	if !isContinueAsNew(err) {
		t.Fatalf("an operator-driven pump must dispatch and self-cascade through a recorded pause, got %v", err)
	}
	if *rig.childStarts != 1 {
		t.Fatalf("want the one dispatched child, got %d", *rig.childStarts)
	}
}

// I2 test 5. ContinueAsNew must carry the WHOLE input: a Begin-started cascade that
// dropped OperatorDriven would honour the recorded pause on its second iteration and
// stop after one activity.
func Test_Pump_ContinueAsNew_CarriesOperatorDriven(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 0)

	_, err := rig.runInput(t, pumpInput{ProjectID: rig.pid, OperatorDriven: true})
	var canErr *workflow.ContinueAsNewError
	if !errors.As(err, &canErr) {
		t.Fatalf("want a ContinueAsNewError, got %v", err)
	}
	var next pumpInput
	if err := converter.GetDefaultDataConverter().FromPayloads(canErr.Input, &next); err != nil {
		t.Fatalf("decode ContinueAsNew input: %v", err)
	}
	if next.ProjectID != rig.pid || !next.OperatorDriven {
		t.Fatalf("ContinueAsNew must carry the whole input (OperatorDriven true), got %+v", next)
	}
}

// I2 test 8 (version gate "pump-honors-recorded-pause", DefaultVersion branch). A
// pre-change pump execution keeps the old sequence: no recorded-pause gate, so it
// dispatches even with the pause recorded.
func Test_Pump_RecordedPauseGate_DefaultVersion_StillDispatches(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 0, recordedPause)
	rig.env.OnGetVersion(changePumpHonorsRecordedPause, workflow.DefaultVersion, 2).Return(workflow.DefaultVersion)

	_, err := rig.run(t)
	if !isContinueAsNew(err) {
		t.Fatalf("a pre-change pump must dispatch as it always did, got %v", err)
	}
	if *rig.childStarts != 1 {
		t.Fatalf("want the old-sequence dispatch of one child, got %d", *rig.childStarts)
	}
}

// M4 (decided: an undecodable pause COUNTS). The channel name carries the operator's
// intent; dropping a pause over a malformed body would fail open (dispatch through a
// halt), counting it fails safe. See pumpPauseRequested.
func Test_Pump_UndecodablePauseSignal_StillPauses(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 0)
	rig.env.RegisterDelayedCallback(func() {
		rig.env.SignalWorkflow(signalOperatorPauseRequested, []byte("not json"))
	}, 0)

	res, err := rig.run(t)
	if err != nil {
		t.Fatalf("an undecodable pause must still quiet the pump, got %v", err)
	}
	if res.Dispatched || *rig.childStarts != 0 {
		t.Fatalf("an undecodable pause must still stop dispatch, got %+v with %d child start(s)", res, *rig.childStarts)
	}
}

// ---- Tests: pause branch (ProjectSupervisionWorkflow / NCUC2) ---------------

// M1 / I2 test 8 (version gate "pause-relays-to-pump", DefaultVersion branch). A
// pre-change supervision execution keeps main's sequence EXACTLY: cancel, then record,
// and no relay.
func Test_Pause_RelayGate_DefaultVersion_CancelThenRecord_NoRelay(t *testing.T) {
	r := runPauseBranchRig(nil, func(env *testsuite.TestWorkflowEnvironment) {
		env.OnGetVersion("pause-relays-to-pump", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	})
	if r.err != nil {
		t.Fatalf("supervision error: %v", r.err)
	}
	if got := r.order.String(); got != "cancel→record" {
		t.Fatalf("a pre-change execution must run main's cancel→record with no relay, got %q", got)
	}
	if len(r.bus.targets) != 0 {
		t.Fatalf("a pre-change execution must not relay, got %v", r.bus.targets)
	}
}

// I2 test 1 (architect ruling, 2026-09-12): RECORD → RELAY → CANCEL. The pause is
// durable in head-state BEFORE it is relayed, so a pump the sweep (re)starts inside the
// relay window reads it at its recorded-pause gate. One call-order log is shared across
// the fake state (record), bus (relay) and pipeline (cancel).
func Test_Pause_RecordsBeforeRelayingToPump(t *testing.T) {
	r := runPauseBranchRig(nil, nil)
	if r.err != nil {
		t.Fatalf("supervision error: %v", r.err)
	}
	if got := r.order.String(); got != "record→relay→cancel" {
		t.Fatalf("want record→relay→cancel, got %q", got)
	}
}

// I2 test 2 (also fix-round M3). Only NotFound (no pump running) is tolerated: any other
// relay failure FAILS the pause branch loudly. The pause was recorded FIRST, so it STAYS
// recorded (the sweep keeps honouring it), and the pipelines are not cancelled.
// ContractMisuse is used because it is non-retryable under the default Activity options
// (a Transient error would retry indefinitely).
func Test_Pause_RelayFailsAfterRecord_PausedStaysRecorded_WorkflowFails(t *testing.T) {
	r := runPauseBranchRig(fwra.New(fwra.ContractMisuse, "messagebus: invalid argument"), nil)
	if r.err == nil {
		t.Fatal("a non-NotFound relay failure must fail the pause branch, got nil")
	}
	if len(r.ps.paused) != 1 || r.ps.paused[0] != "operator halt" {
		t.Fatalf("the pause was recorded before the relay and must stay recorded, got %v", r.ps.paused)
	}
	if got := r.order.String(); got != "record→relay" {
		t.Fatalf("want record→relay and no cancel after the failed relay, got %q", got)
	}
}

// PauseProject's signal lands on the supervision workflow, and nothing else reaches the
// pump — so the pause branch must RELAY the pause to the project's one pump id through
// messageBus.deliverSignal, in the wire form the pump decodes.
func Test_Pause_RelaysPauseToProjectPump(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	bus := &recordingSignalBus{}
	wf := newWorkflows(wfDeps{
		Review:       &fakeReview{},
		Intervention: &fakeIntervention{plan: intervention.PausePlan{PipelinesToCancel: []intervention.PipelineRef{"wf-C-1"}, RecordPaused: true}},
	})
	registerSupervisionWithBus(env, wf, ps, pipe, bus)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("supervision error: %v", err)
	}
	if len(bus.targets) != 1 || string(bus.targets[0]) != string(pid)+":nextActivity" {
		t.Fatalf("want one pause relayed to the project pump %q, got %v", string(pid)+":nextActivity", bus.targets)
	}
	if bus.names[0] != messagebus.SignalName(signalOperatorPauseRequested) {
		t.Fatalf("want signal %q, got %q", signalOperatorPauseRequested, bus.names[0])
	}
	var sig operatorPauseSignal
	if err := json.Unmarshal(bus.payloads[0].Bytes, &sig); err != nil || sig.Reason != "operator halt" || sig.ProjectID != pid {
		t.Fatalf("relayed payload must decode to the operator's pause, got %+v (err %v)", sig, err)
	}
	// The rest of the pause branch still runs.
	if len(pipe.cancelled) != 1 || len(ps.paused) != 1 {
		t.Fatalf("want the pipeline cancel + recordOperatorPaused as before, got cancels=%d paused=%v", len(pipe.cancelled), ps.paused)
	}
}

// No pump running (never started, or already closed quiet) is the normal case for a
// project paused between cascades: messageBus reports the target NotFound, and the
// pause branch must tolerate it and still cancel + record the pause.
func Test_Pause_NoRunningPump_NotFoundTolerated(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	bus := &recordingSignalBus{err: fwra.New(fwra.NotFound, "messagebus: no execution with that id")}
	wf := newWorkflows(wfDeps{
		Review:       &fakeReview{},
		Intervention: &fakeIntervention{plan: intervention.PausePlan{PipelinesToCancel: []intervention.PipelineRef{"wf-C-1"}, RecordPaused: true}},
	})
	registerSupervisionWithBus(env, wf, ps, pipe, bus)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a pause with no running pump must succeed (NotFound tolerated), got %v", err)
	}
	if len(bus.targets) != 1 {
		t.Fatalf("want the relay attempted once, got %d", len(bus.targets))
	}
	if len(pipe.cancelled) != 1 || len(ps.paused) != 1 || ps.paused[0] != "operator halt" {
		t.Fatalf("want the pipeline cancel + recordOperatorPaused(operator halt), got cancels=%d paused=%v", len(pipe.cancelled), ps.paused)
	}
}

// The operator-pause branch: applyPausePolicy returns a plan naming a pipeline to
// cancel + RecordPaused; the Manager EXECUTES the pipeline cancel + recordOperatorPaused.
// (The LLM worker-cancel abandon step is retired under agentic-everywhere; the in-flight
// dispatch is the GH-Actions pipeline, cancelled via the pause plan's PipelinesToCancel.)
func Test_Pause_AppliesPolicy_CancelsPipeline_RecordsPaused(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	wf := newWorkflows(wfDeps{
		Review:       &fakeReview{},
		Intervention: &fakeIntervention{plan: intervention.PausePlan{PipelinesToCancel: []intervention.PipelineRef{"wf-C-1"}, RecordPaused: true}},
	})
	registerSupervision(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("supervision error: %v", err)
	}
	if len(pipe.cancelled) != 1 {
		t.Fatalf("want one pipeline cancel from the pause plan, got %d", len(pipe.cancelled))
	}
	if len(ps.paused) != 1 || ps.paused[0] != "operator halt" {
		t.Fatalf("want one recordOperatorPaused(operator halt), got %v", ps.paused)
	}
}

// Test_Pause_RealInterventionEngine_PolicyThreaded_ApplyPausePolicySucceeds is the
// seam-cleanup Task 6 regression: runPauseBranch (signals.go) now threads the Manager's
// configured InterventionPolicy into PauseRequestContext.Policy. Against the REAL engine
// (intervention.NewInterventionEngine(), the production wiring — cmd/server/main.gen.go
// via WorkerManifest, workermanifest.go) this is load-bearing, not cosmetic:
// ApplyPausePolicy dispatches on ctx.Policy.Mode (strategy.go strategyFor), and
// InterventionModeUnknown (the zero value) has NO registered strategy — see the
// companion negative test below for the failure that zero value produces. The retired
// pauseRequestContext adapter never populated Policy at all. This test drives the real
// engine with the Manager's actual configured policy (constructionInterventionPolicy,
// the same builder WorkerManifest uses) and asserts the pause branch completes: a real
// PausePlan, not a guaranteed "unknown policy mode" error.
func Test_Pause_RealInterventionEngine_PolicyThreaded_ApplyPausePolicySucceeds(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}}
	pipe := &fakePipeline{}
	// The REAL engine (not fakeIntervention) + the Manager's real config-derived policy
	// builder — the exact production wiring, so a regression that drops Policy threading
	// in runPauseBranch would fail THIS test with "unknown policy mode", not silently pass.
	wf := newWorkflows(wfDeps{
		Review:             &fakeReview{},
		Intervention:       intervention.NewInterventionEngine(),
		InterventionPolicy: constructionInterventionPolicy(""), // default: Tiered, RetryBudget 2
	})
	registerSupervision(env, wf, ps, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorPauseRequested, operatorPauseSignal{ProjectID: pid, Reason: "operator halt"})
	}, time.Millisecond)

	env.ExecuteWorkflow(executionKindProjectSupervision, projectSupervisionInput{ProjectID: pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("real-engine pause branch must succeed once Policy is threaded, got %v", err)
	}
	if len(ps.paused) != 1 || ps.paused[0] != "operator halt" {
		t.Fatalf("want one recordOperatorPaused(operator halt) from the real engine's PausePlan, got %v", ps.paused)
	}
}

// Test_ApplyPausePolicy_ZeroValuePolicy_IsTheOldBug pins the shape of the latent
// production defect the seam-cleanup Task 6 rewrite fixed (see task-6-report.md's
// "Correction + disclosure" section). The retired pauseRequestContext adapter never
// populated PauseRequestContext.Policy, so it always shipped the zero value
// (InterventionMode 0 == InterventionModeUnknown). Against the REAL engine that is not a
// no-op: ApplyPausePolicy resolves a strategy from ctx.Policy.Mode (strategy.go
// strategyFor) and InterventionModeUnknown has no registered strategy, so every operator
// pause request would have failed. This test documents WHY the Policy threading in
// runPauseBranch (signals.go) matters and pins the old-bug shape so it cannot silently
// return: if Policy threading is ever dropped again, Test_Pause_RealInterventionEngine_*
// above fails loudly with exactly this error.
func Test_ApplyPausePolicy_ZeroValuePolicy_IsTheOldBug(t *testing.T) {
	e := intervention.NewInterventionEngine()
	_, err := e.ApplyPausePolicy(fweng.Context{Context: context.Background()}, intervention.PauseRequestContext{
		ProjectID: "p1",
		Reason:    "operator halt",
		// Policy left at its zero value — exactly what the retired adapter sent.
	})
	if err == nil {
		t.Fatalf("expected the zero-value Policy (InterventionModeUnknown) to fail with \"unknown policy mode\", got nil error")
	}
	var ee *fweng.Error
	if !errors.As(err, &ee) {
		t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
	}
	if ee.Kind != fweng.InvalidInput {
		t.Fatalf("error kind = %v, want %v (detail %q)", ee.Kind, fweng.InvalidInput, ee.Detail)
	}
	if ee.Detail != "unknown policy mode" {
		t.Fatalf("error detail = %q, want %q — pin the old-bug shape exactly", ee.Detail, "unknown policy mode")
	}
}

// ---- Tests: replan sweep (ReplanSweepWorkflow) ------------------------------

// A quiet sweep returns an empty result (no auto-replan).
func Test_ReplanSweep_QuietSweep_EmptyResult(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{}, Review: &fakeReview{},
	})
	registerReplanSweep(env, wf, ps)

	env.ExecuteWorkflow(executionKindReplanSweep, replanSweepInput{ProjectID: &pid})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("sweep error: %v", err)
	}
	var res ReplanSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode sweep result: %v", err)
	}
	if len(res.FlaggedVariances) != 0 {
		t.Fatalf("want an empty quiet sweep, got %v", res.FlaggedVariances)
	}
}

// ---- Tests: pump sweep (PumpSweepWorkflow, Task 7c) -------------------------

// No projects on the platform ⇒ an empty, quiet sweep.
func Test_PumpSweep_NoProjects_EmptyResult(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{Version: 1, Phase: 2}}
	lister := fakeProjectLister{fakeFullProjectState: fakeFullProjectState{ps}}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 0 {
		t.Fatalf("want an empty sweep with no projects, got %v", res.PumpedProjects)
	}
}

// Only construction-phase projects are pumped; system-design/project-design-phase
// projects are skipped WITHOUT starting a child pump for them (the eligibility
// filter mirrors nextEligibleActivity's own Phase gate).
func Test_PumpSweep_FiltersToConstructionPhaseOnly(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	constructionProjectID := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries: []projectstate.ProjectSummary{
			{ProjectID: projectstate.ProjectID(uuid.NewString()), Phase: projectstate.PhaseSystemDesign},
			{ProjectID: projectstate.ProjectID(uuid.NewString()), Phase: projectstate.PhaseProjectDesign},
			{ProjectID: constructionProjectID, Phase: projectstate.PhaseConstruction},
		},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(constructionProjectID) {
		t.Fatalf("want exactly the one construction-phase project pumped, got %v", res.PumpedProjects)
	}
}

// boolPtr is a tiny test helper — ProjectSummary.OperatorPaused is *bool
// (generated, omitempty).
func boolPtr(b bool) *bool { return &b }

// Fix round 1 (Task 7c live-firing review), FINDING 2: a paused project is
// EXCLUDED from the fan-out even though it is otherwise eligible
// (construction-phase); an unpaused construction-phase project alongside it is
// still pumped — the pause check must not over- or under-fire.
func Test_PumpSweep_ExcludesPausedProject_IncludesUnpaused(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pausedID := projectstate.ProjectID(uuid.NewString())
	activeID := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries: []projectstate.ProjectSummary{
			{ProjectID: pausedID, Phase: projectstate.PhaseConstruction, OperatorPaused: boolPtr(true)},
			{ProjectID: activeID, Phase: projectstate.PhaseConstruction, OperatorPaused: boolPtr(false)},
		},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(activeID) {
		t.Fatalf("want only the unpaused project pumped (paused excluded), got %v", res.PumpedProjects)
	}
}

// A nil OperatorPaused (the zero value ListProjects reports for a
// never-paused project, per its own omitempty convention) must NOT be
// mistaken for "paused" — it means "not paused", same as an explicit false.
func Test_PumpSweep_NilOperatorPaused_TreatedAsNotPaused(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries:            []projectstate.ProjectSummary{{ProjectID: pid, Phase: projectstate.PhaseConstruction, OperatorPaused: nil}},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(pid) {
		t.Fatalf("want the nil-OperatorPaused project pumped (nil != paused), got %v", res.PumpedProjects)
	}
}

// An eligible construction-phase project gets a per-project child pump started
// (PumpNextActivityWorkflow, unchanged) — this test proves the fan-out actually
// reaches and runs that workflow (a quiet tick: no eligible activity wired), not
// just that the sweep enumerates.
func Test_PumpSweep_ConstructionPhaseProject_StartsChildPump(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries:            []projectstate.ProjectSummary{{ProjectID: pid, Phase: projectstate.PhaseConstruction}},
	}
	wf := newWorkflows(wfDeps{
		Intervention:         &fakeIntervention{},
		Review:               &fakeReview{},
		NextEligibleActivity: nil, // every started child pump goes quiet immediately
	})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(pid) {
		t.Fatalf("want the one construction-phase project pumped, got %v", res.PumpedProjects)
	}
}

// ONE PUMP PER PROJECT (architect pump ruling, 2026-09-12) — the inverse of the
// retired Test_PumpSweepChildWorkflowID_DiffersFromClientDrivenPumpWorkflowID, which
// ENFORCED that the sweep's pump id and the client-driven per-tick id differed and so
// guaranteed two pumps racing one frontier whenever the Schedule ran. The sweep must
// start its per-project child under EXACTLY the id ExecuteNextActivity starts or
// joins. Driven through the real PumpSweepWorkflow (not just the derivation helper):
// the child pump the sweep starts is observed by its workflow id.
func Test_PumpSweep_ChildPumpID_IsTheClientDrivenPumpWorkflowID(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries:            []projectstate.ProjectSummary{{ProjectID: pid, Phase: projectstate.PhaseConstruction}},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	var startedChildIDs []string
	var startedInputs []pumpInput
	var mu sync.Mutex
	env.SetOnChildWorkflowStartedListener(func(info *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
		mu.Lock()
		defer mu.Unlock()
		startedChildIDs = append(startedChildIDs, info.WorkflowExecution.ID)
		var in pumpInput
		if err := args.Get(&in); err != nil {
			t.Errorf("decode child pump input: %v", err)
		}
		startedInputs = append(startedInputs, in)
	})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v", err)
	}
	want := pumpWorkflowID(ProjectID(pid))
	if want != string(pid)+":nextActivity" {
		t.Fatalf("pumpWorkflowID = %q, want the tick-invariant %q", want, string(pid)+":nextActivity")
	}
	if len(startedChildIDs) != 1 || startedChildIDs[0] != want {
		t.Fatalf("the sweep must start its child pump under the client-driven pump id %q, got %v", want, startedChildIDs)
	}
	// I2: the sweep's pump is NOT operator-driven — it must honour a recorded pause.
	if len(startedInputs) != 1 || startedInputs[0].OperatorDriven {
		t.Fatalf("the sweep must start its child pump with OperatorDriven false, got %+v", startedInputs)
	}
}

// Fix round 1 (Task 7c live-firing review), FINDING 6: the "prior tick still
// cascading → collapse, not fail" branch (temporal.
// IsWorkflowExecutionAlreadyStartedError in PumpSweepWorkflow) IS reachable in
// the mocked TestWorkflowEnvironment — the SDK's test env tracks running child
// workflows by ID and rejects a second start against a STILL-RUNNING one with
// a real ChildWorkflowExecutionAlreadyStartedError, exactly like the real
// server (internal_workflow_testsuite.go checks runningWorkflows on start).
// §10d of the earlier report was WRONG to call this untestable — the cheap
// trigger is simply two ProjectSummary entries sharing one ProjectID in the
// SAME tick: the first starts the child; by the time the loop reaches the
// second (same stable pumpWorkflowID, since it depends only on
// ProjectID), that child has not yet completed, so the second start collides
// for real and the collapse branch runs.
func Test_PumpSweep_DuplicateProjectIDInOneTick_SecondCollapsesOntoFirst(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := projectstate.ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: pid, Version: 1, Phase: 2}}
	lister := fakeProjectLister{
		fakeFullProjectState: fakeFullProjectState{ps},
		summaries: []projectstate.ProjectSummary{
			{ProjectID: pid, Phase: projectstate.PhaseConstruction},
			{ProjectID: pid, Phase: projectstate.PhaseConstruction}, // duplicate — same stable child id
		},
	}
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}})
	registerPumpSweep(env, wf, lister, ps, &fakePipeline{phase: PipelineSucceeded})

	env.ExecuteWorkflow(executionKindPumpSweep, pumpSweepInput{})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pump sweep error: %v — the collapse branch must not propagate the already-started error", err)
	}
	var res pumpSweepResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decode pump sweep result: %v", err)
	}
	// The duplicate collapses onto the first (not appended twice, not failed) —
	// per pumpSweepResult.PumpedProjects's doc: a collapsed entry never appears.
	if len(res.PumpedProjects) != 1 || res.PumpedProjects[0] != ProjectID(pid) {
		t.Fatalf("want exactly one pump for the duplicate id (the second collapses onto the first), got %v", res.PumpedProjects)
	}
}

// ---- Tests: RegisterSchedules (Task 7c) -------------------------------------

// fakeScheduleBus records every RegisterSchedule call. Satisfies messagebus.MessageBus.
type fakeScheduleBus struct {
	mu    sync.Mutex
	specs []messagebus.ScheduleSpec
	ids   []messagebus.ScheduleID
}

func (b *fakeScheduleBus) DeliverSignal(fwra.Context, messagebus.ExecutionID, messagebus.SignalName, messagebus.ExecutionPayload) error {
	return nil
}

func (b *fakeScheduleBus) RegisterSchedule(_ fwra.Context, scheduleID messagebus.ScheduleID, spec messagebus.ScheduleSpec) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ids = append(b.ids, scheduleID)
	b.specs = append(b.specs, spec)
	return nil
}

var _ messagebus.MessageBus = (*fakeScheduleBus)(nil)

// RegisterSchedules must register exactly the two platform-wide Schedules — the
// pump sweep (30s, targeting PumpSweepWorkflow) and the replan sweep (5m, targeting
// ReplanSweepWorkflow) — with the right ids/workflow-types/intervals.
func Test_RegisterSchedules_RegistersPumpSweepAndReplanSweep(t *testing.T) {
	bus := &fakeScheduleBus{}

	if err := RegisterSchedules(context.Background(), bus); err != nil {
		t.Fatalf("RegisterSchedules: %v", err)
	}

	if len(bus.ids) != 2 {
		t.Fatalf("want 2 Schedules registered, got %d: %v", len(bus.ids), bus.ids)
	}
	byID := make(map[messagebus.ScheduleID]messagebus.ScheduleSpec, len(bus.ids))
	for i, id := range bus.ids {
		byID[id] = bus.specs[i]
	}

	pumpSpec, ok := byID[messagebus.ScheduleID(scheduleIDPumpSweep)]
	if !ok {
		t.Fatalf("missing pump-sweep Schedule %q; got ids %v", scheduleIDPumpSweep, bus.ids)
	}
	if string(pumpSpec.ExecutionKind) != executionKindPumpSweep {
		t.Fatalf("pump-sweep ExecutionKind = %q, want %q", pumpSpec.ExecutionKind, executionKindPumpSweep)
	}
	if pumpSpec.Cadence.Every != pumpSweepIntervalSecs*time.Second {
		t.Fatalf("pump-sweep interval = %v, want %ds", pumpSpec.Cadence.Every, pumpSweepIntervalSecs)
	}

	replanSpec, ok := byID[messagebus.ScheduleID(scheduleIDReplanSweep)]
	if !ok {
		t.Fatalf("missing replan-sweep Schedule %q; got ids %v", scheduleIDReplanSweep, bus.ids)
	}
	if string(replanSpec.ExecutionKind) != executionKindReplanSweep {
		t.Fatalf("replan-sweep ExecutionKind = %q, want %q", replanSpec.ExecutionKind, executionKindReplanSweep)
	}
	if replanSpec.Cadence.Every != replanSweepIntervalSecs*time.Second {
		t.Fatalf("replan-sweep interval = %v, want %ds", replanSpec.Cadence.Every, replanSweepIntervalSecs)
	}
}

// ---- Tests: conditional per-phase approval gate (Task 6) --------------------

// newFakeProjectStateWithPolicy builds a fakeProjectState whose served project
// carries the given committed ReviewPolicy (the gate's start-snapshot source).
func newFakeProjectStateWithPolicy(policy projectstate.ReviewPolicy) *fakeProjectState {
	return &fakeProjectState{project: projectstate.Project{
		ID:           projectstate.ProjectID(uuid.NewString()),
		Version:      1,
		Phase:        2,
		ReviewPolicy: policy,
	}}
}

// gateDeps builds a wfDeps for the gate tests: GitStatus is wired to the fake project
// state so gitOn is true → the phase records fire (the PR rail stays dormant, so
// branch/PR/merge are no-ops). The read/transition seams are registration-side only
// now (registerConstruct backs the generated activities with the same ps).
func gateDeps(ps *fakeProjectState) wfDeps {
	return wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
		GitStatus:    ps,
	}
}

// newFakePipeline is the default all-phases-succeed pipeline double.
func newFakePipeline() *fakePipeline { return &fakePipeline{phase: PipelineSucceeded} }

// failOncePipeline fails the pipeline exactly once for the named phase, then serves
// success for it (and every other phase). It correlates the observed phase via the
// last-submitted spec (runPipeline submits then immediately observes, sequentially).
type failOncePipeline struct {
	mu        sync.Mutex
	failPhase string
	failed    map[string]bool
	lastPhase string
	submitted []agenticjob.PipelineSpec
}

func newFakePipelineFailingOnce(phase string) *failOncePipeline {
	return &failOncePipeline{failPhase: phase, failed: map[string]bool{}}
}

func (p *failOncePipeline) SubmitAgenticJob(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	// The phase rides in DispatchInputs (the neutral pipelineSpec.Phase is composed into
	// the contract spec's DispatchInputs["phase"] by the workflow-side helper).
	p.lastPhase = spec.DispatchInputs["phase"]
	return agenticjob.PipelineHandle("wf-" + string(spec.ActivityID)), nil
}

func (p *failOncePipeline) ObserveAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ph := p.lastPhase
	if ph == p.failPhase && !p.failed[ph] {
		p.failed[ph] = true
		return agenticjob.PipelineObservation{Phase: agenticjob.PhaseFailed, Diagnostic: "forced one-time failure"}, nil
	}
	return agenticjob.PipelineObservation{Phase: agenticjob.PhaseSucceeded}, nil
}

func (p *failOncePipeline) CancelAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) error {
	return nil
}

var _ agenticjob.AgenticJobAccess = (*failOncePipeline)(nil)

// Empty ReviewPolicy → no suspend, all phases dispatch. Byte-for-byte today's behavior.
func Test_Construct_EmptyPolicy_NoGate_WalksAllPhases(t *testing.T) {
	pipe := runPumpWith(t, sampleActivity()) // fakeProjectState default policy = empty
	if len(pipe.submitted) != 5 {
		t.Fatalf("empty policy submitted %d, want 5", len(pipe.submitted))
	}
}

// A gated phase suspends until the matching-phase Approve arrives, which records the
// phase completion to head-state.
func Test_Construct_GatedPhase_ApproveRecordsCompleted(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}})
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 30*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("expected RecordPhaseCompleted(detailed_design) after approval")
	}
}

// The gate is phase-multiplexed: a decision for a DIFFERENT phase is ignored; only the
// matching-phase decision releases the gate.
func Test_Construct_GatedPhase_StaleSignalIgnored(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}})
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove}) // wrong phase
	}, 10*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 40*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("gate must release only on the matching-phase decision")
	}
}

func Test_Construct_VarianceRetry_DoesNotReGateApprovedPhase(t *testing.T) {
	// THE resumability guarantee: approve an early gated phase, then force a LATER phase's
	// pipeline to fail once (→ variance retry re-walks from index 0). The already-approved
	// phase must NOT re-suspend — the in-memory completedPhases set skips it.
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseRequirements}, // gate phase 0
	}})
	pipe := newFakePipelineFailingOnce("test_plan") // phase 2 fails once, then succeeds
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	approvals := 0
	env.RegisterDelayedCallback(func() {
		approvals++
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove})
	}, 20*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// If the retry re-gated phase 0, the workflow would block waiting for a 2nd approval that
	// never comes (test times out) — reaching completion with a single approval proves it did not.
	if approvals != 1 {
		t.Fatalf("expected exactly 1 approval (phase 0 not re-gated on retry), got %d", approvals)
	}
}

// Test_Construct_VarianceRetry_NonGit_DoesNotReGateApprovedPhase is the non-git
// counterpart of the variance-retry resumability test. It wires NO GitStatus (gitOn=false)
// so the ONLY barrier preventing re-gating on variance re-walk is the in-memory
// completedPhases mark. Because the head-state write in completePhase is gitOn-gated,
// the test ONLY passes if the mark is unconditional (outside the gitOn branch). If the
// mark were inside an "if gitOn" block, the mark would not be set, the re-walk would
// re-gate phase 0, and the workflow would deadlock (no 2nd approval scheduled).
func Test_Construct_VarianceRetry_NonGit_DoesNotReGateApprovedPhase(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseRequirements}, // gate phase 0
	}})
	pipe := newFakePipelineFailingOnce("test_plan") // phase 2 fails once, then succeeds
	// GitStatus intentionally unwired ⇒ gitOn=false. The in-memory completedPhases mark
	// is the ONLY re-gate barrier (no head-state completion record exists on re-walk).
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)
	approvals := 0
	env.RegisterDelayedCallback(func() {
		approvals++
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "requirements", Decision: PhaseApprove})
	}, 20*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// If the completedPhases mark were gitOn-gated the workflow would block waiting for a
	// 2nd approval that never comes (deadlock). Reaching completion with exactly one approval
	// proves the mark is unconditional and the variance re-walk skipped phase 0.
	if approvals != 1 {
		t.Fatalf("expected exactly 1 approval (phase 0 not re-gated on non-git retry), got %d", approvals)
	}
}

// Test_Construct_GatedPhase_SendBackRedraftsThenApprove proves that SendBack redrafts
// the gated phase in place (re-runs its pipeline) and never enters the variance path.
// After the redraft, PhaseApprove completes the phase and the workflow exits normally.
func Test_Construct_GatedPhase_SendBackRedraftsThenApprove(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign}, // gate phase 1
	}})
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)

	// First signal: SendBack (redraft). The gate re-runs detailed_design's pipeline then
	// loops back to StageAwaitingApproval without entering the variance path.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{
			Phase:    "detailed_design",
			Decision: PhaseSendBack,
			Feedback: &ReviewFeedback{Notes: "needs revision"},
		})
	}, 30*time.Second)
	// Second signal: Approve. Completes the phase; the workflow runs remaining phases
	// and exits normally.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// SendBack must NOT trigger the variance path (no recordActivityFailed call).
	if len(ps.failed) != 0 {
		t.Fatalf("SendBack must not enter the variance path; got failed: %v", ps.failed)
	}
	// Phase completed record landed (gitOn=true via gateDeps).
	if !ps.phaseCompleted("C-Orders", "detailed_design") {
		t.Error("expected RecordPhaseCompleted(detailed_design) after SendBack+Approve")
	}
	// Activity exited Completed (not Skipped or Failed).
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want one Completed exit after SendBack+Approve, got %v", ps.exited)
	}
}

// Test_Construct_EmptyPolicy_NonGit_NoPhaseRecords is the "pure vibes = today" guarantee
// (brief B6): an empty ReviewPolicy AND gitOn=false must write ZERO phase head-state records.
// This is the strict inertness proof: with no gating and no git, the construction loop must
// behave exactly as it did before the gate feature was introduced — no RecordPhaseStarted or
// RecordPhaseCompleted calls, phaseDone must be empty. This is distinct from the non-git
// variance-retry tests (which carry a non-empty policy) and from the empty-policy walk test
// (which only checks pipeline submissions, not head-state writes).
func Test_Construct_EmptyPolicy_NonGit_NoPhaseRecords(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{
		ID:      projectstate.ProjectID(uuid.NewString()),
		Version: 3,
		Phase:   2,
		// ReviewPolicy is zero value — no gating at all.
	}}
	pipe := &fakePipeline{phase: PipelineSucceeded}
	// GitStatus intentionally NOT wired → gitOn=false.
	// With no gating and no git, the loop must produce no phase head-state records.
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe)

	act := sampleActivity()
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID:  ProjectID(ps.project.ID),
		ActivityID: ActivityID(act.ActivityID),
		Activity:   act,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Core assertion: empty policy + non-git → no phase records whatsoever.
	if len(ps.phaseDone) != 0 {
		t.Fatalf("empty-policy non-git path wrote %d phase record(s), want 0: %v", len(ps.phaseDone), ps.phaseDone)
	}
	// Sanity check: all phases still dispatched (behavior unchanged vs. pre-gate).
	if len(pipe.submitted) != len(act.Phases) {
		t.Fatalf("empty-policy non-git submitted %d pipelines, want %d", len(pipe.submitted), len(act.Phases))
	}
}

// ---- Tests: preset resolution + non-overridable floor (Task 7) --------------

// vibesPreset builds a ReviewPolicy with the "vibes" preset (auto-approve everything
// short of the non-overridable floor). ReviewPolicy.Preset is *string (modelgen's
// optional-scalar convention).
func vibesPreset() projectstate.ReviewPolicy {
	p := projectstate.ReviewPresetVibes
	return projectstate.ReviewPolicy{Preset: &p}
}

// deployTouchingContract builds a ServiceContract whose Interface carries a
// deploy-shaped operation — trips ContractTouchesReviewFloor.
func deployTouchingContract() projectstate.ServiceContract {
	return projectstate.ServiceContract{
		Component: "comp-1",
		Interface: projectstate.ContractInterface{
			Operations: []projectstate.ContractOperation{{Name: "DeployService"}},
		},
	}
}

// Test_Construct_VibesPreset_NoSuspend_WalksAllPhases proves the brief's Step-1
// "vibes auto-approves a draft commit" scenario end-to-end through the real
// construction workflow: under the "vibes" preset, with no floor-touching contract
// committed, every phase dispatches with NO suspend and NO signal — the workflow
// completes on its own, exactly like the empty-policy "pure vibes" case.
func Test_Construct_VibesPreset_NoSuspend_WalksAllPhases(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset()) // no ServiceContracts committed
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if !env.IsWorkflowCompleted() {
		t.Fatal("vibes preset must not suspend — workflow should complete without any signal")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Phases + the auto-dispatched LOCAL MERGE job (local-merge-and-policy Commit 1):
	// under vibes in the rail-dormant git profile the activity finishes with a
	// policy-auto-approved merge of activity/<id> into main.
	if len(pipe.submitted) != len(sampleActivity().Phases)+1 {
		t.Fatalf("vibes preset submitted %d pipelines, want %d (phases + auto-merge)", len(pipe.submitted), len(sampleActivity().Phases)+1)
	}
	last := pipe.submitted[len(pipe.submitted)-1]
	if last.DispatchInputs[agenticjob.DispatchInputJobKey] != agenticjob.DispatchJobMerge {
		t.Fatalf("last submit must be the merge job, got inputs %v", last.DispatchInputs)
	}
}

// Test_Construct_VibesPreset_FloorSuspendsWithoutApproval proves the brief's Step-1
// "floor gate still blocks a flagged dispatch" scenario: even under "vibes", a
// construction-phase dispatch of an activity whose committed contract touches
// deploy/spend/schema genuinely SUSPENDS — with NO approval signal registered, the
// workflow must record requirements/detailed_design/test_plan completed but NEVER
// construction (the test environment's own runaway-test guard eventually forces the
// still-blocked execution to a ScheduleToClose deadline error, so IsWorkflowCompleted
// alone can't distinguish suspended-forever from truly-inert; the phase-completion
// record is the real red/green signal — an inert, not-actually-gated phase would have
// recorded "construction" too).
func Test_Construct_VibesPreset_FloorSuspendsWithoutApproval(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset())
	ps.project.ServiceContracts = map[string]projectstate.ServiceContract{"comp-1": deployTouchingContract()}
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	// Deliberately NO signal registered — the workflow must never reach the
	// construction phase's completion record on its own.
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	for _, phase := range []string{"requirements", "detailed_design", "test_plan"} {
		if !ps.phaseCompleted("C-Orders", phase) {
			t.Fatalf("expected %s to complete before the floor-gated phase", phase)
		}
	}
	if ps.phaseCompleted("C-Orders", "construction") {
		t.Fatal("floor gate must suspend construction dispatch — it must NOT complete when no approval ever arrives, even under vibes")
	}
}

// Test_Construct_VibesPreset_FloorBlocksFlaggedDispatch proves the release path: an
// explicit approval on the construction phase releases the floor's suspend and
// records the phase completion.
func Test_Construct_VibesPreset_FloorBlocksFlaggedDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset())
	ps.project.ServiceContracts = map[string]projectstate.ServiceContract{"comp-1": deployTouchingContract()}
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
	}, 30*time.Second)
	// The risk floor ALSO holds the local merge (local-merge-and-policy Commit 1):
	// a second approval on the "merge" gate key releases it.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: mergeGateKey, Decision: PhaseApprove})
	}, 60*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if !ps.phaseCompleted("C-Orders", "construction") {
		t.Error("floor gate must suspend construction dispatch until an explicit approval, even under vibes")
	}
}

// Test_Construct_VibesPreset_NoFloor_NoDeadlock is the negative control for the floor
// test above: WITHOUT registering any approval signal, a vibes-preset activity whose
// contract does NOT touch the floor must complete on its own (proves the floor test's
// suspend is caused by the contract, not by vibes gating construction generally).
func Test_Construct_VibesPreset_NoFloor_NoDeadlock(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset())
	ps.project.ServiceContracts = map[string]projectstate.ServiceContract{"comp-1": {
		Component: "comp-1",
		Interface: projectstate.ContractInterface{
			Operations: []projectstate.ContractOperation{{Name: "GenerateArtifact"}},
		},
	}}
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if !env.IsWorkflowCompleted() {
		t.Fatal("a non-floor-touching contract must not suspend construction dispatch under vibes")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
}

// ---- Tests: policy-gated LOCAL merge step (local-merge-and-policy Commit 1) --

// checkpointsPreset builds a ReviewPolicy with the "checkpoints" preset.
func checkpointsPreset() projectstate.ReviewPolicy {
	p := projectstate.ReviewPresetCheckpoints
	return projectstate.ReviewPolicy{Preset: &p}
}

// mergeSubmits returns the merge-job submissions the fake pipeline captured.
func mergeSubmits(specs []agenticjob.PipelineSpec) []agenticjob.PipelineSpec {
	var out []agenticjob.PipelineSpec
	for _, s := range specs {
		if s.DispatchInputs[agenticjob.DispatchInputJobKey] == agenticjob.DispatchJobMerge {
			out = append(out, s)
		}
	}
	return out
}

// Test_Construct_LocalMerge_CheckpointsHoldsUntilMergeApproval proves the
// checkpoints/full hold: after the gated phases are approved, the activity
// holds AGAIN at the merge gate (keyed "merge") and dispatches the merge job
// only on Approve.
func Test_Construct_LocalMerge_CheckpointsHoldsUntilMergeApproval(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(checkpointsPreset())
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	// checkpoints gates detailed_design + construction + integration; merge holds last.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 20*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
	}, 40*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "integration", Decision: PhaseApprove})
	}, 60*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: mergeGateKey, Decision: PhaseApprove})
	}, 80*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	merges := mergeSubmits(pipe.submitted)
	if len(merges) != 1 {
		t.Fatalf("expected exactly 1 merge-job submit after merge approval, got %d", len(merges))
	}
	if got := merges[0].DispatchInputs["activity_id"]; got != "C-Orders" {
		t.Fatalf("merge job activity_id = %q, want C-Orders", got)
	}
	if len(ps.exited) != 1 {
		t.Fatalf("expected the activity to exit after the approved merge, exits = %d", len(ps.exited))
	}
}

// envSignalClient is a Temporal client whose SignalWorkflow delivers into a
// testsuite environment BY WORKFLOW ID. It lets a workflow test drive the gates
// through the real façade — SubmitPhaseDecision's gate-key validation AND its
// workflow-id routing — where env.SignalWorkflow bypasses both (which is how the
// merge hold shipped unreleasable while every test passed).
type envSignalClient struct {
	client.Client
	env *testsuite.TestWorkflowEnvironment
}

func (c *envSignalClient) SignalWorkflow(_ context.Context, workflowID string, _ string, signalName string, arg any) error {
	return c.env.SignalWorkflowByID(workflowID, signalName, arg)
}

// QueryWorkflow serves the façade's session read from the running test workflow, so the
// B1.3 precheck runs against the real view.
func (c *envSignalClient) QueryWorkflow(_ context.Context, _ string, _ string, queryType string, args ...any) (converter.EncodedValue, error) {
	return c.env.QueryWorkflow(queryType, args...)
}

// Test_Construct_LocalMerge_ReleasedThroughFacade is the production path for the
// merge hold: every approval — the three checkpoints phase gates AND the merge
// gate — arrives via constructionManager.SubmitPhaseDecision, not a direct
// signal. If the façade refuses mergeGateKey (the 2026-09-12 wedge) the merge
// callback records ContractMisuse and the activity never merges or exits.
func Test_Construct_LocalMerge_ReleasedThroughFacade(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(checkpointsPreset())
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	// Run under the id the façade computes, so SignalWorkflowByID proves the
	// routing as well as the validation.
	env.SetStartWorkflowOptions(client.StartWorkflowOptions{ID: constructActivityWorkflowID("p", "C-Orders")})
	m := newTestConstructionManager(&envSignalClient{env: env})
	approve := func(phase string) func() {
		return func() {
			if err := m.SubmitPhaseDecision(testCtx(), "p", "C-Orders", phase, PhaseApprove, nil); err != nil {
				t.Errorf("SubmitPhaseDecision(%q, Approve): %v", phase, err)
			}
		}
	}
	env.RegisterDelayedCallback(approve("detailed_design"), 20*time.Second)
	env.RegisterDelayedCallback(approve("construction"), 40*time.Second)
	env.RegisterDelayedCallback(approve("integration"), 60*time.Second)
	env.RegisterDelayedCallback(approve(mergeGateKey), 80*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if merges := mergeSubmits(pipe.submitted); len(merges) != 1 {
		t.Fatalf("expected exactly 1 merge-job submit after a façade merge approval, got %d", len(merges))
	}
	if len(ps.exited) != 1 {
		t.Fatalf("expected the activity to exit after the façade-approved merge, exits = %d", len(ps.exited))
	}
}

// Test_Construct_LocalMerge_CheckpointsHoldsWithoutMergeApproval is the negative
// control: with the phase approvals delivered but NO merge approval, the merge
// job is never dispatched and the activity never exits (the Temporal test env's
// runaway watchdog eventually forces the suspended run down, exactly as the
// Task-7 floor-suspend test documents — the real red/green signal is the
// absence of the merge submit + the exit record).
func Test_Construct_LocalMerge_CheckpointsHoldsWithoutMergeApproval(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(checkpointsPreset())
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 20*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
	}, 40*time.Second)
	// Deliberately NO "merge" approval.
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if got := mergeSubmits(pipe.submitted); len(got) != 0 {
		t.Fatalf("merge job must NOT dispatch without merge approval, got %d submits", len(got))
	}
	if len(ps.exited) != 0 {
		t.Fatalf("activity must NOT exit while the merge gate holds, exits = %d", len(ps.exited))
	}
}

// Test_Construct_LocalMerge_NonGitProfile_NoMergeSubmit: with GitStatus unwired
// (gitOn=false — no git slice at all), the merge step is a no-op: phases only.
func Test_Construct_LocalMerge_NonGitProfile_NoMergeSubmit(t *testing.T) {
	pipe := runPumpWith(t, sampleActivity()) // wires NO GitStatus
	if got := mergeSubmits(pipe.submitted); len(got) != 0 {
		t.Fatalf("non-git profile must not dispatch a merge job, got %d", len(got))
	}
}

// mergeConflictPipeline serves Succeeded for phase dispatches and a FAILED
// "merge conflict" observation for merge-job dispatches — the deterministic-
// conflict double for the intervention-path test.
type mergeConflictPipeline struct {
	mu        sync.Mutex
	submitted []agenticjob.PipelineSpec
}

func (p *mergeConflictPipeline) SubmitAgenticJob(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, spec)
	if spec.DispatchInputs[agenticjob.DispatchInputJobKey] == agenticjob.DispatchJobMerge {
		return agenticjob.PipelineHandle("merge-" + string(spec.ActivityID)), nil
	}
	return agenticjob.PipelineHandle("wf-" + string(spec.ActivityID)), nil
}

func (p *mergeConflictPipeline) ObserveAgenticJob(_ fwra.Context, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	if strings.HasPrefix(string(handle), "merge-") {
		return agenticjob.PipelineObservation{
			Phase:      agenticjob.PhaseFailed,
			Diagnostic: "local merge: merge conflict merging activity/C-Orders into main",
		}, nil
	}
	return agenticjob.PipelineObservation{Phase: agenticjob.PhaseSucceeded}, nil
}

func (p *mergeConflictPipeline) CancelAgenticJob(_ fwra.Context, _ agenticjob.PipelineHandle) error {
	return nil
}

var _ agenticjob.AgenticJobAccess = (*mergeConflictPipeline)(nil)

// Test_Construct_LocalMerge_ConflictRoutesToIntervention proves a merge conflict
// flows through the SAME variance machinery as a failed phase pipeline: under a
// Retry directive the deterministic conflict exhausts the variance budget and
// the activity records a terminal FAILURE (VarianceExhausted) — never a fake
// completed exit, and no partial merge is ever recorded.
func Test_Construct_LocalMerge_ConflictRoutesToIntervention(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(vibesPreset())
	pipe := &mergeConflictPipeline{}
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.failed) != 1 || ps.failed[0].reason != projectstate.VarianceExhausted {
		t.Fatalf("expected a terminal VarianceExhausted failure record, got %+v", ps.failed)
	}
	if len(ps.exited) != 0 {
		t.Fatalf("a conflicted merge must not record a completed exit, exits = %+v", ps.exited)
	}
}

// ---- SetReviewPolicy (op 2.8, local-merge-and-policy Commit 2) --------------

// setReviewPolicyManager wires a constructionManager over the generated
// FakeConstructionTransitionAccess for the preset write-path tests.
func setReviewPolicyManager(ps projectstate.ProjectStateAccess, ct projectstate.ConstructionTransitionAccess) *constructionManager {
	return newConstructionManager(nil, ps, nil, nil, nil, nil, nil, ct, nil, nil, nil, nil, 0, "", nil)
}

func Test_SetReviewPolicy_EmptyProjectID(t *testing.T) {
	m := setReviewPolicyManager(nil, nil)
	err := m.SetReviewPolicy(fwmanager.Context{Context: context.Background()}, ProjectID(""), projectstate.ReviewPresetVibes)
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

// The write path is a CLOSED vocabulary: an unknown preset is rejected before
// any RA call (the fake would panic if touched — nil Fns). This is the fix for
// the documented fail-open corner: EffectiveGate's read path treats an
// unrecognized preset as the legacy explicit-map fallback (empty map → gates
// NOTHING), so a typo must never be persistable.
func Test_SetReviewPolicy_RejectsUnknownPreset(t *testing.T) {
	m := setReviewPolicyManager(nil, &projectstatefake.FakeConstructionTransitionAccess{})
	for _, preset := range []string{"", "vibess", "YOLO", "Full", "VIBES"} {
		err := m.SetReviewPolicy(fwmanager.Context{Context: context.Background()}, ProjectID("p1"), preset)
		if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
			t.Fatalf("preset %q: want ContractMisuse, got %s", preset, got)
		}
	}
}

// A valid preset writes through the projectstate CAS: read the current version,
// record the policy at that expectedVersion with the preset set and the
// committed GatedPhasesByType map PRESERVED (SetReviewPolicy and
// UpdateReviewPolicy own disjoint halves of ReviewPolicy).
func Test_SetReviewPolicy_WritesPresetPreservingGateMap(t *testing.T) {
	existing := map[string][]projectstate.ActivityMethodPhase{
		"service": {projectstate.MethodPhaseDetailedDesign},
	}
	var recorded *projectstate.ReviewPolicy
	var recordedVersion projectstate.Version
	// The read moved to the base projectStateAccess port (constructionTransitionAccess.ReadProject
	// was pruned — both call sites passed empty cred); RecordReviewPolicy stays on constructionTransition.
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(_ fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
			return projectstate.Project{
				ID:           projectID,
				Version:      7,
				ReviewPolicy: projectstate.ReviewPolicy{GatedPhasesByType: existing},
			}, nil
		},
	}
	ct := &projectstatefake.FakeConstructionTransitionAccess{
		RecordReviewPolicyFn: func(_ fwra.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, policy projectstate.ReviewPolicy, _ projectstate.RepoCredential, _ fwra.IdempotencyKey) (projectstate.Version, error) {
			recorded = &policy
			recordedVersion = expectedVersion
			return expectedVersion + 1, nil
		},
	}
	m := setReviewPolicyManager(ps, ct)
	for _, preset := range []string{projectstate.ReviewPresetVibes, projectstate.ReviewPresetCheckpoints, projectstate.ReviewPresetFull} {
		recorded = nil
		if err := m.SetReviewPolicy(fwmanager.Context{Context: context.Background()}, ProjectID("p1"), preset); err != nil {
			t.Fatalf("SetReviewPolicy(%q): %v", preset, err)
		}
		if recorded == nil || recorded.Preset == nil || *recorded.Preset != preset {
			t.Fatalf("preset %q not recorded: %+v", preset, recorded)
		}
		if len(recorded.GatedPhasesByType["service"]) != 1 {
			t.Fatalf("GatedPhasesByType must be preserved, got %+v", recorded.GatedPhasesByType)
		}
		if recordedVersion != 7 {
			t.Fatalf("CAS expectedVersion = %d, want the read version 7", recordedVersion)
		}
	}
}

func Test_SetReviewPolicy_UnknownProject_NotFound(t *testing.T) {
	ps := &projectstatefake.FakeProjectStateAccess{
		ReadProjectFn: func(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
			return projectstate.Project{}, fwra.New(fwra.NotFound, "no such project")
		},
	}
	m := setReviewPolicyManager(ps, nil)
	err := m.SetReviewPolicy(fwmanager.Context{Context: context.Background()}, ProjectID("ghost"), projectstate.ReviewPresetVibes)
	if got := asConstructionError(t, err).Kind; got != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %s", got)
	}
}

// railLifecycleEnabled derives wfDeps.RailEnabled (WorkerManifest). The repo resolver
// is part of the derivation: the local profile binds the GitLocal sourceControlAccess
// for the DESIGN managers while construction stays repo-less there, and that
// rail-without-repo boot MUST read rail-dormant — RailEnabled=true would make
// runLocalMergeStep skip and nothing would merge local activity branches.
func Test_RailLifecycleEnabled_RequiresRailAndRepo(t *testing.T) {
	repo := func(ProjectID) (sourcecontrol.RepoRef, bool) { return sourcecontrol.RepoRef("acct|acct/p"), true }
	cases := []struct {
		name string
		rail sourcecontrol.SourceControlAccess
		repo func(ProjectID) (sourcecontrol.RepoRef, bool)
		want bool
	}{
		{"rail+repo (cloud / creds-ful local)", &stubRail{}, repo, true},
		{"rail without repo (local GitLocal rail; construction repo-less)", &stubRail{}, nil, false},
		{"repo without rail", nil, repo, false},
		{"neither", nil, nil, false},
	}
	for _, tc := range cases {
		if got := railLifecycleEnabled(tc.rail, tc.repo); got != tc.want {
			t.Fatalf("%s: railLifecycleEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ---- Tests: episode capture seam (SP1 Task 7) -------------------------------
//
// The invariant under test: every terminal observation of an agentic construction
// dispatch produces EXACTLY ONE ledger record — the mined summary when there is one, an
// explicit GAP record when there is not — and the append can never take the business
// workflow down with it.

// captureSeamSummary is the mined summary the local executor reports on a terminal
// observation. Every field is checked for VERBATIM carry-through below.
func captureSeamSummary() *agenticjob.EpisodeSummary {
	model := "claude-sonnet-4"
	cost := 0.42
	turns := int64(7)
	trace := ".aiarch/traces/ep-1.jsonl"
	streamed := agenticjob.EpisodeUsage{In: 90, Out: 40, CacheRead: 5, CacheCreate: 2}
	return &agenticjob.EpisodeSummary{
		EpisodeID:      "ep-1",
		Model:          &model,
		Usage:          agenticjob.EpisodeUsage{In: 100, Out: 50, CacheRead: 10, CacheCreate: 3},
		StreamedUsage:  &streamed,
		CostUSD:        &cost,
		NumTurns:       &turns,
		ToolCallCounts: map[string]int64{"Read": 4, "Edit": 2},
		SubagentSpans:  []agenticjob.SubagentSpan{{ToolUseID: "tu-1"}},
		StartedAt:      time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC),
		EndedAt:        time.Date(2026, 8, 2, 10, 5, 0, 0, time.UTC),
		Outcome:        agenticjob.EpisodeSucceeded,
		TracePath:      &trace,
	}
}

// assertSummaryCarriedVerbatim proves the whole mined summary reached the ledger
// unchanged — the capture seam enriches, it never recomputes. Split into a scalar half
// and a pointer half purely to keep each assertion block readable.
func assertSummaryCarriedVerbatim(t *testing.T, rec episode.EpisodeRecord, want *agenticjob.EpisodeSummary) {
	t.Helper()
	if rec.EpisodeID != want.EpisodeID {
		t.Errorf("EpisodeID = %q, want %q", rec.EpisodeID, want.EpisodeID)
	}
	if rec.Usage != episode.EpisodeUsage(want.Usage) {
		t.Errorf("Usage = %+v, want %+v", rec.Usage, want.Usage)
	}
	if !rec.StartedAt.Equal(want.StartedAt) || !rec.EndedAt.Equal(want.EndedAt) {
		t.Errorf("times = %v..%v, want the run's own clock %v..%v", rec.StartedAt, rec.EndedAt, want.StartedAt, want.EndedAt)
	}
	if rec.Outcome != episode.EpisodeSucceeded {
		t.Errorf("Outcome = %d, want EpisodeSucceeded", rec.Outcome)
	}
	if rec.ToolCallCounts["Read"] != want.ToolCallCounts["Read"] || rec.ToolCallCounts["Edit"] != want.ToolCallCounts["Edit"] {
		t.Errorf("ToolCallCounts = %v, want %v", rec.ToolCallCounts, want.ToolCallCounts)
	}
	if len(rec.SubagentSpans) != len(want.SubagentSpans) || rec.SubagentSpans[0].ToolUseID != want.SubagentSpans[0].ToolUseID {
		t.Errorf("SubagentSpans = %+v, want %+v", rec.SubagentSpans, want.SubagentSpans)
	}
	if rec.GapReason != nil {
		t.Errorf("a mined episode carries no GapReason, got %q", *rec.GapReason)
	}
	assertOptionalFieldsCarriedVerbatim(t, rec, want)
}

// assertOptionalFieldsCarriedVerbatim covers the summary's nil-able half.
func assertOptionalFieldsCarriedVerbatim(t *testing.T, rec episode.EpisodeRecord, want *agenticjob.EpisodeSummary) {
	t.Helper()
	if rec.StreamedUsage == nil || *rec.StreamedUsage != episode.EpisodeUsage(*want.StreamedUsage) {
		t.Errorf("StreamedUsage = %+v, want %+v", rec.StreamedUsage, want.StreamedUsage)
	}
	if rec.Model == nil || *rec.Model != *want.Model {
		t.Errorf("Model = %v, want %q", rec.Model, *want.Model)
	}
	if rec.CostUSD == nil || *rec.CostUSD != *want.CostUSD {
		t.Errorf("CostUSD = %v, want %v", rec.CostUSD, *want.CostUSD)
	}
	if rec.NumTurns == nil || *rec.NumTurns != *want.NumTurns {
		t.Errorf("NumTurns = %v, want %v", rec.NumTurns, *want.NumTurns)
	}
	if rec.TracePath == nil || *rec.TracePath != *want.TracePath {
		t.Errorf("TracePath = %v, want %q", rec.TracePath, *want.TracePath)
	}
}

// ---------------------------------------------------------------------------
// episodeRecordFor (Task 10): TargetRef carries the attempt key, not the bare
// activity id. episodeRecordFor needs a workflow.Context (workflow.GetInfo/Now), so it
// cannot be called from a plain Go test — testEpisodeRecordForWorkflow is a
// standalone Temporal-testsuite wrapper that isolates just this computation.
// ---------------------------------------------------------------------------

// testEpisodeRecordForInput carries episodeRecordFor's real (non-ctx) parameters
// through a Temporal test workflow. obs is left at its zero value (Episode nil): the
// GAP branch and the summary branch compute TargetRef identically before branching, so
// the gap branch alone is enough to exercise the attempt-key assembly this test targets.
type testEpisodeRecordForInput struct {
	IDSeed     string
	ActivityID string
	Task       projectstate.MethodTask
	Attempt    int
}

func testEpisodeRecordForWorkflow(ctx workflow.Context, in testEpisodeRecordForInput) (episode.EpisodeRecord, error) {
	return episodeRecordFor(ctx, pipelineObservation{}, in.IDSeed, in.ActivityID, in.Task, in.Attempt), nil
}

func TestEpisodeRecordFor_TargetRefCarriesTheAttemptKey(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(testEpisodeRecordForWorkflow, testEpisodeRecordForInput{
		IDSeed: "seed", ActivityID: "C-billing-manager", Task: projectstate.TaskDetailedDesign, Attempt: 2,
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var rec episode.EpisodeRecord
	if err := env.GetWorkflowResult(&rec); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	want := "C-billing-manager:detailedDesign:2"
	if rec.TargetRef != want {
		t.Errorf("TargetRef = %q, want %q — a bare activityId makes the episode permanently unjoinable", rec.TargetRef, want)
	}
}

func TestEpisodeRecordFor_FirstAttemptIsOne(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.ExecuteWorkflow(testEpisodeRecordForWorkflow, testEpisodeRecordForInput{
		IDSeed: "seed", ActivityID: "C-x", Task: projectstate.TaskConstruction, Attempt: 1,
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var rec episode.EpisodeRecord
	if err := env.GetWorkflowResult(&rec); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	if want := "C-x:construction:1"; rec.TargetRef != want {
		t.Errorf("TargetRef = %q, want %q", rec.TargetRef, want)
	}
}

// A terminal observation carrying a mined summary becomes exactly one EpisodeRecord per
// dispatched phase, with the summary copied verbatim and the Manager-known
// Kind/TargetRef/Lineage stamped on.
func Test_Construct_TerminalObservation_PersistsEpisodeRecord(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded, episode: captureSeamSummary()}
	eps := &fakeEpisodes{}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe, eps)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	got := eps.records()
	// One episode per dispatched App-A phase — the capture rides the same choke point
	// the dispatch does.
	if len(got) != len(sampleActivity().Phases) {
		t.Fatalf("want one appended episode per dispatched phase (%d), got %d", len(sampleActivity().Phases), len(got))
	}
	rec := got[0]
	if rec.Kind != episode.EpisodeKindConstruction {
		t.Errorf("Kind = %d, want EpisodeKindConstruction", rec.Kind)
	}
	// TargetRef carries the attempt key (Task 10), not the bare activity id — and the
	// task it names is the phase's AI-WORK task, never its gate task. runPipeline
	// dispatches the agent that WRITES the artifact; the gate (srsReview, designReview,
	// codeReview, …) is the review of that artifact and belongs to awaitPhaseDecision.
	// Asserting the whole sequence, not just the first record, pins that for every one
	// of the service profile's five phases: a gate-task attribution would read
	// srsReview/designReview/stpReview/codeReview/testing here instead.
	wantRefs := []string{
		"C-XYZ:srs:1",
		"C-XYZ:detailedDesign:1",
		"C-XYZ:stp:1",
		"C-XYZ:construction:1",
		"C-XYZ:integration:1",
	}
	for i, want := range wantRefs {
		if got[i].TargetRef != want {
			t.Errorf("episode[%d].TargetRef = %q, want the AI-work attempt key %q", i, got[i].TargetRef, want)
		}
	}
	if rec.Lineage == nil || rec.Lineage.WorkflowID == "" || rec.Lineage.RunID == "" {
		t.Fatalf("Lineage must carry the durable execution identity, got %+v", rec.Lineage)
	}
	if rec.Lineage.ActivityID == nil || *rec.Lineage.ActivityID != "C-XYZ" {
		t.Errorf("Lineage.ActivityID = %v, want the Method activity id", rec.Lineage.ActivityID)
	}
	assertSummaryCarriedVerbatim(t, rec, captureSeamSummary())
}

// A terminal LOCAL-venue observation with NO summary must still produce a record: an
// explicit gap naming what went missing. A missing record is never silently missing.
func Test_Construct_TerminalObservation_MissingSummary_PersistsGapRecord(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded} // no episode mined, no run URL ⇒ local venue
	eps := &fakeEpisodes{}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe, eps)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	got := eps.records()
	if len(got) != len(sampleActivity().Phases) {
		t.Fatalf("a summary-less terminal observation must still be recorded: want %d, got %d", len(sampleActivity().Phases), len(got))
	}
	rec := got[0]
	if rec.Outcome != episode.EpisodeGap {
		t.Errorf("Outcome = %d, want EpisodeGap", rec.Outcome)
	}
	if rec.GapReason == nil || !strings.Contains(*rec.GapReason, "no episode summary") {
		t.Errorf("GapReason must name the missing summary, got %v", rec.GapReason)
	}
	if rec.EpisodeID == "" || strings.ContainsAny(rec.EpisodeID, ":/ ") {
		t.Errorf("a synthesized gap id must be non-empty and store-safe, got %q", rec.EpisodeID)
	}
	// Same attempt-keyed TargetRef as the summary path (Task 10) — the first dispatched
	// phase (Requirements) on its first attempt, attributed to that phase's AI-WORK task
	// (srs), not to its gate task (srsReview), which reviews the SRS rather than writing it.
	if want := "C-XYZ:srs:1"; rec.Kind != episode.EpisodeKindConstruction || rec.TargetRef != want {
		t.Errorf("a gap still carries its Kind/TargetRef, got kind=%d ref=%q, want ref=%q", rec.Kind, rec.TargetRef, want)
	}
}

// A REMOTE-venue (GitHub-Actions) run mines no episode in v1, so a nil summary there is
// expected rather than lost: it must NOT produce a gap record.
func Test_Construct_RemoteVenue_WritesNoGapRecord(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded, runURL: "https://github.com/acme/repo/actions/runs/7"}
	eps := &fakeEpisodes{}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe, eps)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if got := eps.records(); len(got) != 0 {
		t.Fatalf("the remote venue mines no episode in v1 — it must write nothing, got %+v", got)
	}
}

// A permanently-failing ledger must NOT take construction down: the append is retried
// inside its own envelope and then logged and dropped.
func Test_Construct_EpisodeAppendFailure_DoesNotFailBusinessFlow(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(uuid.NewString()), Version: 3, Phase: 2}}
	pipe := &fakePipeline{phase: PipelineSucceeded, episode: captureSeamSummary()}
	eps := &fakeEpisodes{failAlways: true}
	wf := newWorkflows(wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		Review:       &fakeReview{},
	})
	registerConstruct(env, wf, ps, pipe, eps)

	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{
		ProjectID: ProjectID(ps.project.ID), ActivityID: "C-XYZ", Activity: sampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("a failing episode ledger must not stall the construction workflow")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failing episode ledger must not fail the construction workflow, got %v", err)
	}
	// The business exit still landed, untouched by the bookkeeping fault.
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("business flow must still record its exit, got %v", ps.exited)
	}
	// And the append really was RETRIED (more attempts than dispatches), not given up on
	// after one try.
	if eps.attemptCount() <= len(sampleActivity().Phases) {
		t.Fatalf("want the append retried within its envelope, got %d attempts across %d dispatches",
			eps.attemptCount(), len(sampleActivity().Phases))
	}
	if len(eps.records()) != 0 {
		t.Fatalf("nothing can land in a permanently-failing ledger, got %+v", eps.records())
	}
}

// The local MERGE job spawns no agent at all, so a summary-less terminal observation
// there is not a loss and must never be recorded as a gap. This rides the real
// policy-gated merge path (the same setup as the local-merge tests above), so the merge
// job genuinely dispatches and is genuinely observed.
func Test_Construct_MergeJob_WritesNoGapRecord(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(checkpointsPreset())
	pipe := newFakePipeline()
	eps := &fakeEpisodes{}
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe, eps)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 20*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
	}, 40*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "integration", Decision: PhaseApprove})
	}, 60*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: mergeGateKey, Decision: PhaseApprove})
	}, 80*time.Second)
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Sanity: the merge job really did dispatch and be observed on this path.
	if len(mergeSubmits(pipe.submitted)) != 1 {
		t.Fatalf("this test only means something if the merge job dispatched; got %d", len(mergeSubmits(pipe.submitted)))
	}
	// Exactly one record per AGENTIC phase dispatch — the merge adds none.
	if n := len(eps.records()); n != len(sampleActivity().Phases) {
		t.Fatalf("the merge job must add no ledger record: want %d (one per agentic phase), got %d",
			len(sampleActivity().Phases), n)
	}
}

// ---------------------------------------------------------------------------
// Episode facet read ops (SP1 capture-seam, Task 9): ListEpisodesForActivity +
// GetEpisodeTimeline. Plain methods — no Temporal env needed.
// ---------------------------------------------------------------------------

// episodeMgr builds a constructionManager exercising ONLY the episode facet read
// ops (Task 9): every other dep stays nil since those ops touch only episodes.
func episodeMgr(eps episode.EpisodeAccess) ConstructionManager {
	return newConstructionManager(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, eps, 0, "", nil)
}

// sampleEpisodeRecord returns a fully-populated ledger record (every optional
// field set) so the view mapping can be checked field-by-field.
func sampleEpisodeRecord(id, targetRef string) episode.EpisodeRecord {
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	ended := started.Add(90 * time.Second)
	workerClass := "senior-developer"
	model := "claude-sonnet-5"
	costUSD := 0.42
	numTurns := int64(7)
	gapReason := "the run reported a gap episode"
	tracePath := ".aiarch/state/episodes/traces/" + id + ".jsonl"
	streamed := episode.EpisodeUsage{In: 100, Out: 50, CacheRead: 10, CacheCreate: 5}
	return episode.EpisodeRecord{
		EpisodeID: id,
		Kind:      episode.EpisodeKindConstruction,
		TargetRef: targetRef,
		Lineage: &episode.EpisodeLineage{
			WorkflowID: "wf-1",
			RunID:      "run-1",
			ActivityID: strPtr("C-XYZ"),
		},
		WorkerClass:    &workerClass,
		Model:          &model,
		Usage:          episode.EpisodeUsage{In: 1000, Out: 500, CacheRead: 100, CacheCreate: 50},
		StreamedUsage:  &streamed,
		CostUSD:        &costUSD,
		NumTurns:       &numTurns,
		ToolCallCounts: map[string]int64{"Read": 3, "Edit": 2},
		SubagentSpans: []episode.SubagentSpan{
			{ToolUseID: "toolu_1", StartedAt: timePtr(started.Add(time.Second)), EndedAt: timePtr(started.Add(2 * time.Second))},
		},
		StartedAt: started,
		EndedAt:   ended,
		Outcome:   episode.EpisodeGap,
		GapReason: &gapReason,
		TracePath: &tracePath,
	}
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }

// assertSampleEpisodeRecordView asserts got is sampleEpisodeRecord(id, targetRef)
// mapped field-for-field onto this contract's own EpisodeRecordView. Split into
// three helpers (identity/usage/outcome) purely to keep each under the gocyclo
// budget — together they are one assertion.
func assertSampleEpisodeRecordView(t *testing.T, got EpisodeRecordView, id, targetRef string) {
	t.Helper()
	assertSampleEpisodeRecordIdentity(t, got, id, targetRef)
	assertSampleEpisodeRecordUsage(t, got)
	assertSampleEpisodeRecordOutcome(t, got, id)
}

func assertSampleEpisodeRecordIdentity(t *testing.T, got EpisodeRecordView, id, targetRef string) {
	t.Helper()
	if got.EpisodeID != id {
		t.Fatalf("EpisodeID = %q, want %q", got.EpisodeID, id)
	}
	if got.Kind != EpisodeKindConstruction {
		t.Fatalf("Kind = %v, want EpisodeKindConstruction", got.Kind)
	}
	if got.TargetRef != targetRef {
		t.Fatalf("TargetRef = %q, want %q", got.TargetRef, targetRef)
	}
	if got.Lineage == nil || got.Lineage.WorkflowID != "wf-1" || got.Lineage.RunID != "run-1" || got.Lineage.ActivityID == nil || *got.Lineage.ActivityID != "C-XYZ" {
		t.Fatalf("Lineage = %+v, want {wf-1 run-1 C-XYZ}", got.Lineage)
	}
	if got.WorkerClass == nil || *got.WorkerClass != "senior-developer" {
		t.Fatalf("WorkerClass = %v", got.WorkerClass)
	}
	if got.Model == nil || *got.Model != "claude-sonnet-5" {
		t.Fatalf("Model = %v", got.Model)
	}
}

func assertSampleEpisodeRecordUsage(t *testing.T, got EpisodeRecordView) {
	t.Helper()
	if got.Usage != (EpisodeUsage{In: 1000, Out: 500, CacheRead: 100, CacheCreate: 50}) {
		t.Fatalf("Usage = %+v", got.Usage)
	}
	if got.StreamedUsage == nil || *got.StreamedUsage != (EpisodeUsage{In: 100, Out: 50, CacheRead: 10, CacheCreate: 5}) {
		t.Fatalf("StreamedUsage = %v", got.StreamedUsage)
	}
	if got.CostUSD == nil || *got.CostUSD != 0.42 {
		t.Fatalf("CostUSD = %v", got.CostUSD)
	}
	if got.NumTurns == nil || *got.NumTurns != 7 {
		t.Fatalf("NumTurns = %v", got.NumTurns)
	}
	if got.ToolCallCounts["Read"] != 3 || got.ToolCallCounts["Edit"] != 2 {
		t.Fatalf("ToolCallCounts = %v", got.ToolCallCounts)
	}
	if len(got.SubagentSpans) != 1 || got.SubagentSpans[0].ToolUseID != "toolu_1" {
		t.Fatalf("SubagentSpans = %+v", got.SubagentSpans)
	}
}

func assertSampleEpisodeRecordOutcome(t *testing.T, got EpisodeRecordView, id string) {
	t.Helper()
	if got.Outcome != EpisodeGap {
		t.Fatalf("Outcome = %v, want EpisodeGap", got.Outcome)
	}
	if got.GapReason == nil || *got.GapReason != "the run reported a gap episode" {
		t.Fatalf("GapReason = %v", got.GapReason)
	}
	if got.TracePath == nil || *got.TracePath != ".aiarch/state/episodes/traces/"+id+".jsonl" {
		t.Fatalf("TracePath = %v", got.TracePath)
	}
}

// ---- ListEpisodesForActivity -------------------------------------------------

func Test_ListEpisodesForActivity_MapsRecordsWithTargetRef(t *testing.T) {
	eps := &fakeEpisodes{listRecords: []episode.EpisodeRecord{
		sampleEpisodeRecord("ep-1", "C-Orders"),
	}}
	m := episodeMgr(eps)

	got, err := m.ListEpisodesForActivity(testCtx(), ProjectID("proj-1"), "C-Orders")
	if err != nil {
		t.Fatalf("ListEpisodesForActivity: unexpected error: %v", err)
	}
	if eps.lastQuery.ProjectID != episode.ProjectID("proj-1") {
		t.Fatalf("ListEpisodes query ProjectID = %q, want proj-1", eps.lastQuery.ProjectID)
	}
	if eps.lastQuery.TargetRef == nil || *eps.lastQuery.TargetRef != "C-Orders" {
		t.Fatalf("ListEpisodes query TargetRef = %v, want *\"C-Orders\" (scoped by activityID)", eps.lastQuery.TargetRef)
	}
	if len(got) != 1 {
		t.Fatalf("ListEpisodesForActivity: got %d records, want 1", len(got))
	}
	assertSampleEpisodeRecordView(t, got[0], "ep-1", "C-Orders")
}

func Test_ListEpisodesForActivity_EmptyProjectID_ContractMisuse(t *testing.T) {
	m := episodeMgr(&fakeEpisodes{})
	_, err := m.ListEpisodesForActivity(testCtx(), ProjectID(""), "C-Orders")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ListEpisodesForActivity_EmptyActivityID_ContractMisuse(t *testing.T) {
	m := episodeMgr(&fakeEpisodes{})
	_, err := m.ListEpisodesForActivity(testCtx(), ProjectID("proj-1"), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_ListEpisodesForActivity_RAError_MapsInfrastructure(t *testing.T) {
	eps := &fakeEpisodes{listErr: fwra.New(fwra.Infrastructure, "ledger unavailable")}
	m := episodeMgr(eps)
	_, err := m.ListEpisodesForActivity(testCtx(), ProjectID("proj-1"), "C-Orders")
	if got := asConstructionError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %s", got)
	}
}

// ---- GetEpisodeTimeline ------------------------------------------------------

func Test_GetEpisodeTimeline_StitchesSequentialEventsWithTypes(t *testing.T) {
	eps := &fakeEpisodes{
		listRecords: []episode.EpisodeRecord{sampleEpisodeRecord("ep-1", "C-Orders")},
		traceEvents: []json.RawMessage{
			json.RawMessage(`{"type":"system","subtype":"init"}`),
			json.RawMessage(`{"type":"assistant"}`),
			json.RawMessage(`not-json`),
		},
	}
	m := episodeMgr(eps)

	got, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-1")
	if err != nil {
		t.Fatalf("GetEpisodeTimeline: unexpected error: %v", err)
	}
	// GetEpisodeTimeline resolves the record by scanning EVERY target on the
	// project (episodeAccess has no by-id lookup) — the query must carry no
	// TargetRef.
	if eps.lastQuery.TargetRef != nil {
		t.Fatalf("ListEpisodes query TargetRef = %v, want nil (whole-project scan)", eps.lastQuery.TargetRef)
	}
	assertSampleEpisodeRecordView(t, got.Record, "ep-1", "C-Orders")

	if eps.lastTraceProjectID != episode.ProjectID("proj-1") || eps.lastTraceEpisodeID != "ep-1" {
		t.Fatalf("ReadTraceEvents args = (%q, %q), want (proj-1, ep-1)", eps.lastTraceProjectID, eps.lastTraceEpisodeID)
	}
	if len(got.Events) != 3 {
		t.Fatalf("Events: got %d, want 3", len(got.Events))
	}
	wantTypes := []string{"system", "assistant", "unknown"}
	for i, ev := range got.Events {
		if ev.Seq != int64(i+1) {
			t.Fatalf("Events[%d].Seq = %d, want %d (1-based sequential)", i, ev.Seq, i+1)
		}
		if ev.EventType != wantTypes[i] {
			t.Fatalf("Events[%d].EventType = %q, want %q", i, ev.EventType, wantTypes[i])
		}
		if ev.Raw == nil || string(*ev.Raw) != string(eps.traceEvents[i]) {
			t.Fatalf("Events[%d].Raw = %v, want %s (carried verbatim)", i, ev.Raw, eps.traceEvents[i])
		}
	}
}

func Test_GetEpisodeTimeline_UnknownEpisodeID_NotFound(t *testing.T) {
	eps := &fakeEpisodes{listRecords: []episode.EpisodeRecord{sampleEpisodeRecord("ep-1", "C-Orders")}}
	m := episodeMgr(eps)

	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-does-not-exist")
	if got := asConstructionError(t, err).Kind; got != fwmanager.NotFound {
		t.Fatalf("want NotFound, got %s", got)
	}
	if eps.lastTraceEpisodeID != "" {
		t.Fatalf("ReadTraceEvents must not be called when the episode is unresolved, got episodeID=%q", eps.lastTraceEpisodeID)
	}
}

// Test_GetEpisodeTimeline_GapRecord_ReturnsEmptyTimeline is the review-round F3
// fix: a gap record (built through the REAL write-path helper episodeGapRecord —
// captureEpisode's own gap-construction call, constructactivity.go) has no
// trace file (TracePath stays nil). Before this fix GetEpisodeTimeline
// unconditionally called ReadTraceEvents, which fails NotFound for a gap —
// indistinguishable from an unknown episodeID. The never-silent gap doctrine
// says a gap is a PRESENT outcome: the record must resolve, with an empty (not
// nil, not erroring) timeline.
func Test_GetEpisodeTimeline_GapRecord_ReturnsEmptyTimeline(t *testing.T) {
	gap := episodeGapRecord(episode.EpisodeKindConstruction, "C-Orders", nil, "gap-ep-1", "the run reported a gap episode", time.Now().UTC())
	if gap.TracePath != nil {
		t.Fatalf("sanity: episodeGapRecord must leave TracePath nil, got %v", gap.TracePath)
	}
	eps := &fakeEpisodes{listRecords: []episode.EpisodeRecord{gap}}
	m := episodeMgr(eps)

	got, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "gap-ep-1")
	if err != nil {
		t.Fatalf("GetEpisodeTimeline: unexpected error for a gap record: %v", err)
	}
	if got.Record.EpisodeID != "gap-ep-1" || got.Record.Outcome != EpisodeGap {
		t.Fatalf("Record = %+v, want the gap record itself", got.Record)
	}
	if len(got.Events) != 0 {
		t.Fatalf("Events = %+v, want empty (no trace file for a gap)", got.Events)
	}
	if eps.lastTraceEpisodeID != "" {
		t.Fatalf("ReadTraceEvents must not be called when TracePath is nil, got episodeID=%q", eps.lastTraceEpisodeID)
	}
}

// Test_GetEpisodeTimeline_TraceFileNotFound_ReturnsEmptyTimeline covers the
// sibling case: TracePath IS set (a non-gap record) but the RA can no longer
// resolve it (e.g. a pruned local trace file) — ReadTraceEvents returns
// fwra.NotFound. Same empty-timeline treatment, not an error.
func Test_GetEpisodeTimeline_TraceFileNotFound_ReturnsEmptyTimeline(t *testing.T) {
	rec := sampleEpisodeRecord("ep-1", "C-Orders")
	eps := &fakeEpisodes{
		listRecords: []episode.EpisodeRecord{rec},
		traceErr:    fwra.New(fwra.NotFound, "no trace file for episode ep-1"),
	}
	m := episodeMgr(eps)

	got, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-1")
	if err != nil {
		t.Fatalf("GetEpisodeTimeline: unexpected error when the trace file is gone: %v", err)
	}
	if len(got.Events) != 0 {
		t.Fatalf("Events = %+v, want empty (trace file unresolvable)", got.Events)
	}
}

func Test_GetEpisodeTimeline_EmptyProjectID_ContractMisuse(t *testing.T) {
	m := episodeMgr(&fakeEpisodes{})
	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID(""), "ep-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_GetEpisodeTimeline_EmptyEpisodeID_ContractMisuse(t *testing.T) {
	m := episodeMgr(&fakeEpisodes{})
	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
}

func Test_GetEpisodeTimeline_ListError_MapsInfrastructure(t *testing.T) {
	eps := &fakeEpisodes{listErr: fwra.New(fwra.Infrastructure, "ledger unavailable")}
	m := episodeMgr(eps)
	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %s", got)
	}
}

func Test_GetEpisodeTimeline_TraceReadError_MapsInfrastructure(t *testing.T) {
	eps := &fakeEpisodes{
		listRecords: []episode.EpisodeRecord{sampleEpisodeRecord("ep-1", "C-Orders")},
		traceErr:    fwra.New(fwra.Infrastructure, "trace file unavailable"),
	}
	m := episodeMgr(eps)
	_, err := m.GetEpisodeTimeline(testCtx(), ProjectID("proj-1"), "ep-1")
	if got := asConstructionError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %s", got)
	}
}

// TestSubmitPhaseDecision_RejectsUnknownPhase closes the hole the 2026-08-13
// contract-strictness audit found: `phase` is a bare string with no schema
// vocabulary, so "" (and any typo) was signalled straight through to the child
// workflow's phase gate, where it could never match a real phase and therefore
// did nothing at all — a silent no-op presenting as a successful decision.
func TestSubmitPhaseDecision_RejectsUnknownPhase(t *testing.T) {
	for _, phase := range []string{"", "   ", "detailedDesign", "Construction", "not-a-phase"} {
		m := newTestConstructionManager(nil)
		err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", phase, PhaseApprove, nil)
		if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
			t.Errorf("phase %q: want ContractMisuse, got %s", phase, got)
		}
	}
}

// TestSubmitPhaseDecision_AcceptsEveryCanonicalPhase is the other half: the five
// canonical phases must all pass the new vocabulary gate.
func TestSubmitPhaseDecision_AcceptsEveryCanonicalPhase(t *testing.T) {
	for _, phase := range []string{"requirements", "detailed_design", "test_plan", "construction", "integration"} {
		m := newTestConstructionManager(&fakeTemporalClient{session: awaitingAt(phase)})
		if err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", phase, PhaseApprove, nil); err != nil {
			t.Errorf("phase %q must be accepted, got %v", phase, err)
		}
	}
}

// TestSubmitPhaseDecision_MergeApproveSignalsActivityWorkflow: the local merge
// hold (runLocalMergeStep) suspends on mergeGateKey, and this op is the only
// operator path that releases it. Approve on "merge" must pass validation and
// land on the per-activity workflow with the key intact.
func TestSubmitPhaseDecision_MergeApproveSignalsActivityWorkflow(t *testing.T) {
	fc := &fakeTemporalClient{session: awaitingAt(mergeGateKey)}
	m := newTestConstructionManager(fc)
	if err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", mergeGateKey, PhaseApprove, nil); err != nil {
		t.Fatalf("SubmitPhaseDecision(merge, Approve): %v", err)
	}
	if want := constructActivityWorkflowID("proj-1", "C-Orders"); fc.lastWorkflowID != want || fc.lastSignalName != signalPhaseDecision {
		t.Fatalf("wfID=%q signal=%q, want wfID=%q signal=%q", fc.lastWorkflowID, fc.lastSignalName, want, signalPhaseDecision)
	}
	sig, ok := fc.lastSignalArg.(phaseDecisionSignal)
	if !ok || sig.Phase != mergeGateKey || sig.Decision != PhaseApprove {
		t.Fatalf("payload=%+v", fc.lastSignalArg)
	}
}

// TestSubmitPhaseDecision_MergeSendBackIsContractMisuse: a merge has no draft to
// send back and the hold ignores anything but Approve, so SendBack on "merge"
// would be a silent no-op. Feedback notes are supplied so the ONLY rule that can
// refuse it is the merge-gate rule, and nothing may be signalled.
func TestSubmitPhaseDecision_MergeSendBackIsContractMisuse(t *testing.T) {
	fc := &fakeTemporalClient{}
	m := newTestConstructionManager(fc)
	err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", mergeGateKey, PhaseSendBack, &ReviewFeedback{Notes: "redo the merge"})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for SendBack on the merge gate, got %s", got)
	}
	if fc.lastSignalName != "" {
		t.Fatalf("a refused merge SendBack must not signal, got signal %q to %q", fc.lastSignalName, fc.lastWorkflowID)
	}
}

// TestSubmitPhaseDecision_UnknownGateKeyStillRejected: admitting mergeGateKey
// must not reopen the vocabulary — near-misses of it are still ContractMisuse.
func TestSubmitPhaseDecision_UnknownGateKeyStillRejected(t *testing.T) {
	for _, phase := range []string{"Merge", "merge ", "merged", "local_merge", "approve"} {
		fc := &fakeTemporalClient{}
		m := newTestConstructionManager(fc)
		err := m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", phase, PhaseApprove, nil)
		if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
			t.Errorf("gate key %q: want ContractMisuse, got %s", phase, got)
		}
		if fc.lastSignalName != "" {
			t.Errorf("gate key %q: a rejected key must not signal", phase)
		}
	}
}

// TestOverrideActivity_RequiresNotes — notes is schema-required on ActivityOverride,
// but `required` is presence-only: an empty string satisfied it and left the
// operator's steer with no durable record of why it happened.
func TestOverrideActivity_RequiresNotes(t *testing.T) {
	m := newTestConstructionManager(&fakeTemporalClient{session: ConstructionSessionView{Stage: StageAwaitingTakeover}})
	err := m.OverrideActivity(testCtx(), "proj-1", "C-Orders", ActivityOverride{Kind: OverrideSkip, Notes: "  "})
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse for a blank override note, got %s", got)
	}
	if err := m.OverrideActivity(testCtx(), "proj-1", "C-Orders", ActivityOverride{Kind: OverrideSkip, Notes: "policy skip"}); err != nil {
		t.Fatalf("a noted override must be accepted: %v", err)
	}
}

// ===========================================================================
// B1.0 — THE WORKFLOW REPLAY HARNESS (plan-B1-B2.md §B1.0; amendment B.3 + D.4).
//
// Every history under testdata/replay/ was CAPTURED from a real Temporal dev server
// running the workflow code as it stood when the fixture was taken (pre-b1/ and pre-d/
// on the code BEFORE any B1 or D1 workflow change). Test_Replay_PreB1Histories_StayDeterministic
// replays each one against the CURRENT code: a change that alters the command
// sequence an in-flight execution already recorded fails here, instead of failing a
// parked production workflow task after a deploy. A GetVersion guard is proven
// load-bearing by removing it and watching the matching fixture fail.
//
// Capture and replay build the workflows receiver from the SAME rig, because the
// workflow body branches on its deps (gitOn is GitStatus != nil; the escalation wait
// arms a timer only when EscalationWaitTimeout > 0; the pump selects through
// NextEligibleActivity). Re-capture a directory with
//
//	CONSTRUCT_HISTORY_CAPTURE=1 [CONSTRUCT_HISTORY_CAPTURE_DIR=<dir>] GOWORK=off \
//	  go test ./internal/manager/construction/ -run '^TestCaptureConstructHistories$' -count=1
//
// It needs the `temporal` CLI on PATH (it starts an offline dev server through
// testsuite.StartDevServer's ExistingPath, on its own port and namespace). NEVER
// re-capture pre-* directories on changed code: they are the record of what already
// ran.
// ===========================================================================

const (
	replayProjectID  ProjectID  = "p-replay"
	replayActivityID ActivityID = "C-Orders"
)

// replayRig is one scenario's workflows receiver plus the fakes behind its activities.
type replayRig struct {
	wf   *workflows
	ps   *fakeProjectState
	pipe agenticjob.AgenticJobAccess
	bus  messagebus.MessageBus
	// rail backs the sourceControlAccess activities; a zero stub when the rig does not
	// wire the PR rail (its activities are then never scheduled).
	rail *stubRail
}

// activities backs every generated activity from the rig's fakes, exactly as the
// production worker registers them (RegisterWorker), so a capture runs the real
// registration path.
func (r replayRig) activities() genActivities {
	full := fakeFullProjectState{r.ps}
	bus := r.bus
	if bus == nil {
		bus = &recordingSignalBus{}
	}
	return genActivities{
		ProjectState:           full,
		Pipeline:               r.pipe,
		ConstructionTransition: fakeConstructionTransition{r.ps},
		GitStatus:              r.ps,
		DesignSession:          projectstate.NewDesignSessionAccess(full),
		MessageBus:             bus,
		Episodes:               &fakeEpisodes{},
		Rail:                   r.railOrStub(),
	}
}

func (r replayRig) railOrStub() *stubRail {
	if r.rail != nil {
		return r.rail
	}
	return &stubRail{}
}

// replayScenario is one captured history: its fixture location, its rig, and how the
// capture tool drives it. drive returns the execution to export, and open=true when the
// execution is still running (or continues as new) and must be terminated after export.
type replayScenario struct {
	dir   string
	name  string
	rig   func() replayRig
	drive func(ctx context.Context, t *testing.T, c client.Client, taskQueue string, r replayRig) (wfID, runID string, open bool)
}

func replayFixturePath(sc replayScenario) string {
	return filepath.Join("testdata", "replay", sc.dir, sc.name+".json")
}

// replayRegistrations are the workflows a fixture can belong to, under their
// registered names.
func replayRegistrations(wf *workflows) []genRegisteredWorkflow {
	return []genRegisteredWorkflow{
		{Name: executionKindPump, Fn: wf.PumpNextActivityWorkflow},
		{Name: executionKindConstructActivity, Fn: wf.ConstructActivityWorkflow},
		{Name: executionKindProjectSupervision, Fn: wf.ProjectSupervisionWorkflow},
	}
}

// replayWorkflows builds the receiver with the production invoker option hook.
func replayWorkflows(d wfDeps) *workflows {
	d.Acts = genInvokers{Opts: activityOptions()}
	if d.Review == nil {
		d.Review = &fakeReview{}
	}
	return newWorkflows(d)
}

func replayGateRig(policy projectstate.ReviewPolicy) replayRig {
	ps := newFakeProjectStateWithPolicy(policy)
	ps.project.ID = projectstate.ProjectID(replayProjectID)
	return replayRig{wf: replayWorkflows(gateDeps(ps)), ps: ps, pipe: newFakePipeline()}
}

func replayGatedOn(phases ...projectstate.ActivityMethodPhase) projectstate.ReviewPolicy {
	return projectstate.ReviewPolicy{GatedPhasesByType: map[string][]projectstate.ActivityMethodPhase{"service": phases}}
}

// replayEscalateRig fails detailed_design's first dispatch into an Escalate directive.
// gitOn follows GitStatus; wait is the escalation window (0 = wait forever, no timer).
func replayEscalateRig(gitOn bool, wait time.Duration) replayRig {
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
	ps.project.ID = projectstate.ProjectID(replayProjectID)
	d := wfDeps{Intervention: &fakeIntervention{directive: intervention.VarianceEscalate}, EscalationWaitTimeout: wait}
	if gitOn {
		d.GitStatus = ps
	}
	return replayRig{wf: replayWorkflows(d), ps: ps, pipe: newFakePipelineFailingOnce("detailed_design")}
}

// replayPumpRig serves proj to a pump that selects with the production rule.
func replayPumpRig(proj projectstate.Project) replayRig {
	proj.ID = projectstate.ProjectID(replayProjectID)
	proj.Version = 1
	ps := &fakeProjectState{project: proj}
	return replayRig{
		wf: replayWorkflows(wfDeps{
			Intervention:         &fakeIntervention{directive: intervention.VarianceRetry},
			NextEligibleActivity: nextEligibleActivity,
		}),
		ps:   ps,
		pipe: newFakePipeline(),
	}
}

// replayPartialLedgerPhases are the four service phases an integration-pending row's
// ledger holds passed (architect (D), P1): everything but Integration.
var replayPartialLedgerPhases = []projectstate.ActivityMethodPhase{
	projectstate.MethodPhaseRequirements, projectstate.MethodPhaseTestPlan,
	projectstate.MethodPhaseDetailedDesign, projectstate.MethodPhaseConstruction,
}

// replayScenarios is every captured history: the pre-change set (never re-captured) and
// the post-b1 set, captured from the B1.4 code so its versioned path is pinned too.
func replayScenarios() []replayScenario {
	return append(replayScenariosPreChange(), replayScenariosPostB1()...)
}

// replayScenariosPreChange are the histories captured on the code BEFORE B1/D1.
func replayScenariosPreChange() []replayScenario {
	return []replayScenario{
		{
			dir: "pre-b1", name: "gate-sendback-redraft-approve",
			rig: func() replayRig { return replayGateRig(replayGatedOn(projectstate.MethodPhaseDetailedDesign)) },
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, r replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitView(ctx, t, c, run.GetID(), "the detailed_design gate", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingApproval
				})
				before := replaySubmitted(r.pipe)
				replaySignal(ctx, t, c, run.GetID(), signalPhaseDecision, phaseDecisionSignal{
					Phase: "detailed_design", Decision: PhaseSendBack,
					Feedback: &ReviewFeedback{Notes: "tighten the error model", Comments: []AnchoredComment{{JSONPath: "$.ops[0]", Text: "name the failure"}}},
				})
				replayAwaitView(ctx, t, c, run.GetID(), "the redraft's gate", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingApproval && replaySubmitted(r.pipe) == before+1
				})
				replaySignal(ctx, t, c, run.GetID(), signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
		{
			dir: "pre-b1", name: "escalate-override-retry",
			rig: func() replayRig { return replayEscalateRig(false, time.Hour) },
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitView(ctx, t, c, run.GetID(), "the escalation", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingTakeover
				})
				replaySignal(ctx, t, c, run.GetID(), signalOperatorOverride, operatorOverrideSignal{Override: ActivityOverride{
					Kind: OverrideRetry, Notes: "the fixture server was down; retry",
				}})
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
		{
			dir: "pre-b1", name: "escalate-override-skip",
			rig: func() replayRig { return replayEscalateRig(true, 0) },
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitView(ctx, t, c, run.GetID(), "the escalation", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingTakeover
				})
				replaySignal(ctx, t, c, run.GetID(), signalOperatorOverride, operatorOverrideSignal{Override: ActivityOverride{
					Kind: OverrideSkip, Notes: "built by hand; nothing to construct",
				}})
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
		{
			dir: "pre-b1", name: "parked-at-gate",
			rig: func() replayRig { return replayGateRig(replayGatedOn(projectstate.MethodPhaseDetailedDesign)) },
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitView(ctx, t, c, run.GetID(), "the detailed_design gate", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingApproval
				})
				return run.GetID(), run.GetRunID(), true
			},
		},
		{
			dir: "pre-b1", name: "local-merge-hold-approve",
			rig: func() replayRig { return replayGateRig(replayGatedOn(projectstate.MethodPhaseConstruction)) },
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, r replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitView(ctx, t, c, run.GetID(), "the construction gate", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingApproval
				})
				replaySignal(ctx, t, c, run.GetID(), signalPhaseDecision, phaseDecisionSignal{Phase: "construction", Decision: PhaseApprove})
				// All five phases dispatched and the merge job not yet: the merge hold.
				replayAwaitView(ctx, t, c, run.GetID(), "the merge hold", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingApproval && replaySubmitted(r.pipe) == 5
				})
				replaySignal(ctx, t, c, run.GetID(), signalPhaseDecision, phaseDecisionSignal{Phase: mergeGateKey, Decision: PhaseApprove})
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
		{
			dir: "pre-b1", name: "supervision-pause-record-relay-cancel",
			rig: func() replayRig {
				ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(replayProjectID), Version: 2, Phase: 2}}
				return replayRig{
					wf: replayWorkflows(wfDeps{Intervention: &fakeIntervention{plan: intervention.PausePlan{
						PipelinesToCancel: []intervention.PipelineRef{"wf-C-1"}, RecordPaused: true,
					}}}),
					ps: ps, pipe: newFakePipeline(), bus: &recordingSignalBus{},
				}
			},
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				id := pauseTargetWorkflowID(replayProjectID)
				run, err := c.SignalWithStartWorkflow(ctx, id, signalOperatorPauseRequested,
					operatorPauseSignal{ProjectID: replayProjectID, Reason: "operator halt"},
					client.StartWorkflowOptions{ID: id, TaskQueue: tq}, executionKindProjectSupervision,
					projectSupervisionInput{ProjectID: replayProjectID})
				if err != nil {
					t.Fatalf("signal-with-start supervision: %v", err)
				}
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
		{
			dir: "pre-b1", name: "pump-operator-driven-over-recorded-pause",
			rig: func() replayRig {
				proj := ledgerChain()
				proj.OperatorPaused = true
				proj.PauseReason = "operator halt"
				return replayPumpRig(proj)
			},
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				return replayRunPumpOnce(ctx, t, c, tq, pumpInput{ProjectID: replayProjectID, OperatorDriven: true})
			},
		},
		{
			// Architect (D), D.2: a pump that read a ledger-partial row whose dependencies
			// were all Done, and chose ANOTHER activity (the pre-D1 rule only picks
			// NotStarted). P is declared before O, so the widened rule would pick P.
			dir: "pre-d", name: "pump-other-choice-with-partial-row-deps-done",
			rig: func() replayRig { return replayPumpRig(replayPartialRowProject()) },
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				return replayRunPumpOnce(ctx, t, c, tq, pumpInput{ProjectID: replayProjectID})
			},
		},
		{
			// Architect (D), D.2: a construct run whose start snapshot read a row carrying a
			// ledger and no stored phases. The pre-D1 seed reads the stored Phases only, so
			// it walks all five phases.
			dir: "pre-d", name: "construct-ledger-row-stored-seed",
			rig: func() replayRig {
				r := replayGateRig(projectstate.ReviewPolicy{})
				r.ps.project.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
					string(replayActivityID): {ActivityID: string(replayActivityID), Attempts: passedLedger(string(replayActivityID), replayPartialLedgerPhases...)},
				}
				return r
			},
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
	}
}

// replayPartialRowProject is D (Done), P (integration-pending, depends on D) and O (not
// started, depends on D), declared in that order.
func replayPartialRowProject() projectstate.Project {
	proj := projWithActivities(
		[]projectstate.ActivityItem{
			{Name: "D", Title: "D", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"},
			{Name: "P", Title: "P", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"},
			{Name: "O", Title: "O", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"},
		},
		[]projectstate.NetworkDependency{
			{Activity: "D", DependsOn: []string{}},
			{Activity: "P", DependsOn: []string{"D"}},
			{Activity: "O", DependsOn: []string{"D"}},
		},
	)
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"D": {ActivityID: "D", Phase: projectstate.ActivityConstructionDone},
		"P": {ActivityID: "P", Attempts: passedLedger("P", replayPartialLedgerPhases...)},
	}
	return proj
}

func replayStartConstruct(ctx context.Context, t *testing.T, c client.Client, tq string) client.WorkflowRun {
	t.Helper()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: constructActivityWorkflowID(replayProjectID, replayActivityID), TaskQueue: tq,
	}, executionKindConstructActivity, constructActivityInput{
		ProjectID: replayProjectID, ActivityID: replayActivityID, Activity: sampleActivity(),
	})
	if err != nil {
		t.Fatalf("start construct: %v", err)
	}
	return run
}

// replayRunPumpOnce starts the pump and returns once its FIRST run has closed (it
// dispatched, waited for the child, and continued as new). The chain is still open.
func replayRunPumpOnce(ctx context.Context, t *testing.T, c client.Client, tq string, in pumpInput) (string, string, bool) {
	t.Helper()
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: pumpWorkflowID(in.ProjectID), TaskQueue: tq}, executionKindPump, in)
	if err != nil {
		t.Fatalf("start pump: %v", err)
	}
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		resp, derr := c.DescribeWorkflowExecution(ctx, run.GetID(), run.GetRunID())
		if derr == nil && resp.GetWorkflowExecutionInfo().GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
			return run.GetID(), run.GetRunID(), true
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pump run %s never closed", run.GetRunID())
	return "", "", false
}

func replayAwaitView(ctx context.Context, t *testing.T, c client.Client, wfID, what string, ok func(ConstructionSessionView) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if enc, err := c.QueryWorkflow(ctx, wfID, "", querySessionState); err == nil {
			var v ConstructionSessionView
			if enc.Get(&v) == nil && ok(v) {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s never reached %s", wfID, what)
}

func replaySignal(ctx context.Context, t *testing.T, c client.Client, wfID, name string, arg any) {
	t.Helper()
	if err := c.SignalWorkflow(ctx, wfID, "", name, arg); err != nil {
		t.Fatalf("signal %s to %s: %v", name, wfID, err)
	}
}

func replayAwaitDone(ctx context.Context, t *testing.T, run client.WorkflowRun) {
	t.Helper()
	wctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := run.Get(wctx, nil); err != nil {
		t.Fatalf("%s did not complete cleanly: %v", run.GetID(), err)
	}
}

// replaySubmitted counts the pipeline submits a rig's fake has served.
func replaySubmitted(pipe agenticjob.AgenticJobAccess) int {
	switch p := pipe.(type) {
	case *fakePipeline:
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.submitted)
	case *failOncePipeline:
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.submitted)
	default:
		return -1
	}
}

// replayExportHistory writes one run's full history in the CLI's JSON format, which is
// what WorkflowReplayer.ReplayWorkflowHistoryFromJSONFile reads.
func replayExportHistory(ctx context.Context, c client.Client, wfID, runID, path string) error {
	it := c.GetWorkflowHistory(ctx, wfID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	var h historypb.History
	for it.HasNext() {
		ev, err := it.Next()
		if err != nil {
			return err
		}
		h.Events = append(h.Events, ev)
	}
	b, err := temporalproto.CustomJSONMarshalOptions{Indent: "  "}.Marshal(&h)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// TestCaptureConstructHistories is the CAPTURE TOOL behind the replay fixtures (env-gated,
// like derived-plan-write). See the section header for when, and when never, to run it.
func TestCaptureConstructHistories(t *testing.T) {
	if os.Getenv("CONSTRUCT_HISTORY_CAPTURE") != "1" {
		t.Skip("capture tool: set CONSTRUCT_HISTORY_CAPTURE=1 to (re)write testdata/replay/ fixtures")
	}
	only := os.Getenv("CONSTRUCT_HISTORY_CAPTURE_DIR")
	bin, err := exec.LookPath("temporal")
	if err != nil {
		t.Fatalf("the capture needs the temporal CLI on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	srv, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		ExistingPath:  bin,
		ClientOptions: &client.Options{Namespace: "b1-replay-capture"},
		LogLevel:      "error",
	})
	if err != nil {
		t.Fatalf("start dev server: %v", err)
	}
	defer func() { _ = srv.Stop() }()
	c := srv.Client()

	for i, sc := range replayScenarios() {
		if only != "" && sc.dir != only {
			continue
		}
		t.Run(sc.dir+"/"+sc.name, func(t *testing.T) {
			r := sc.rig()
			tq := fmt.Sprintf("replay-capture-%d", i)
			w := worker.New(c, tq, worker.Options{})
			RegisterWorker(w, genWorkerManifest{
				Workflows:       replayRegistrations(r.wf),
				ActivityOptions: activityOptions(),
				Activities:      r.activities(),
			})
			if err := w.Start(); err != nil {
				t.Fatalf("start worker: %v", err)
			}
			defer w.Stop()
			wfID, runID, open := sc.drive(ctx, t, c, tq, r)
			if err := replayExportHistory(ctx, c, wfID, runID, replayFixturePath(sc)); err != nil {
				t.Fatalf("export %s: %v", replayFixturePath(sc), err)
			}
			if open {
				_ = c.TerminateWorkflow(ctx, wfID, "", "replay capture done")
			}
		})
	}
}

// replayFixture replays one fixture against the current workflow code.
func replayFixture(sc replayScenario) error {
	rep := worker.NewWorkflowReplayer()
	for _, reg := range replayRegistrations(sc.rig().wf) {
		rep.RegisterWorkflowWithOptions(reg.Fn, workflow.RegisterOptions{Name: reg.Name})
	}
	return rep.ReplayWorkflowHistoryFromJSONFile(nil, replayFixturePath(sc))
}

// Test_Replay_PreB1Histories_StayDeterministic replays every captured history against
// the current code; each must replay with no non-determinism error. A missing fixture
// fails (it never skips), and a fixture no scenario names fails too, so nothing under
// testdata/replay/ can sit there unreplayed.
func Test_Replay_PreB1Histories_StayDeterministic(t *testing.T) {
	covered := map[string]bool{}
	for _, sc := range replayScenarios() {
		path := replayFixturePath(sc)
		covered[path] = true
		t.Run(sc.dir+"/"+sc.name, func(t *testing.T) {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("fixture %s is missing (capture it with CONSTRUCT_HISTORY_CAPTURE=1): %v", path, err)
			}
			if err := replayFixture(sc); err != nil {
				t.Fatalf("replaying %s against the current code: %v", path, err)
			}
		})
	}
	files, err := filepath.Glob(filepath.Join("testdata", "replay", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no replay fixtures found under testdata/replay")
	}
	for _, f := range files {
		if !covered[f] {
			t.Errorf("fixture %s has no replay scenario, so nothing replays it", f)
		}
	}
}

// ===========================================================================
// D1 — INTEGRATION-PENDING ROWS, THE PUMP HALF (architect (D), D.1 / D.2 / D.4).
// ===========================================================================

// TestIsActivityDispatchable_Table is D.4's dispatchable table.
func TestIsActivityDispatchable_Table(t *testing.T) {
	item := projectstate.ActivityItem{Name: "A", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"}
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	storedPhases := func(completeFirst bool) []projectstate.PhaseCompletion {
		out := make([]projectstate.PhaseCompletion, 0, len(servicePhases))
		for i, ph := range servicePhases {
			out = append(out, projectstate.PhaseCompletion{Phase: ph, Completed: completeFirst && i == 0})
		}
		return out
	}
	cases := []struct {
		name string
		row  *projectstate.ActivityConstructionStatus
		want bool
	}{
		{"absent", nil, true},
		{"a ledger that decides nothing reads NotStarted", &projectstate.ActivityConstructionStatus{
			Attempts: []projectstate.TaskAttempt{ledgerAttempt("A", projectstate.TaskSRS, 1, projectstate.OutcomePassed)}}, true},
		{"ledger 4/5: integration-pending", &projectstate.ActivityConstructionStatus{Attempts: passedLedger("A", replayPartialLedgerPhases...)}, true},
		{"ledger 5/5: Done", &projectstate.ActivityConstructionStatus{Attempts: passedLedger("A", servicePhases...)}, false},
		{"stored Running with StartedAt: a pump started it", &projectstate.ActivityConstructionStatus{
			Phase: projectstate.ActivityConstructionRunning, StartedAt: &now}, false},
		{"stored Failed", &projectstate.ActivityConstructionStatus{
			Phase: projectstate.ActivityConstructionFailed, FailureReason: projectstate.PipelineFailed}, false},
		{"stored Done-exited with incomplete phases", &projectstate.ActivityConstructionStatus{
			Phase: projectstate.ActivityConstructionDone, BuildStatus: projectstate.BuildInReview, Phases: storedPhases(true)}, false},
		{"stored phases plus a partial ledger: the pump wrote it", &projectstate.ActivityConstructionStatus{
			Phases: storedPhases(false), Attempts: passedLedger("A", replayPartialLedgerPhases...)}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := map[string]projectstate.ActivityConstructionStatus{}
			if c.row != nil {
				r := *c.row
				r.ActivityID = "A"
				status["A"] = r
			}
			if got := isActivityDispatchable("A", item, status); got != c.want {
				t.Fatalf("isActivityDispatchable = %v, want %v", got, c.want)
			}
		})
	}
}

// d1Project is D (built), P (integration-pending, depends on D), Q (depends on P) and O
// (depends on D), declared in that order.
func d1Project() projectstate.Project {
	proj := projWithActivities(
		[]projectstate.ActivityItem{
			{Name: "D", Title: "D", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"},
			{Name: "P", Title: "P", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"},
			{Name: "Q", Title: "Q", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"},
			{Name: "O", Title: "O", WorkerClass: "junior-developer", Coding: true, ComponentID: "todo-list-manager"},
		},
		[]projectstate.NetworkDependency{
			{Activity: "D", DependsOn: []string{}},
			{Activity: "P", DependsOn: []string{"D"}},
			{Activity: "Q", DependsOn: []string{"P"}},
			{Activity: "O", DependsOn: []string{"D"}},
		},
	)
	proj.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"P": {ActivityID: "P", Attempts: passedLedger("P", replayPartialLedgerPhases...)},
	}
	return proj
}

func TestNextEligibleActivity_IntegrationPendingRow(t *testing.T) {
	pick := func(proj projectstate.Project, rule eligibilityRule) string {
		t.Helper()
		sel := nextEligibleActivity(proj, rule)
		if sel.Verdict != verdictDispatch {
			return ""
		}
		return sel.Activity.ActivityID
	}
	doneD := func(proj projectstate.Project) projectstate.Project {
		proj.ActivityConstruction["D"] = projectstate.ActivityConstructionStatus{ActivityID: "D", Phase: projectstate.ActivityConstructionDone}
		return proj
	}

	// Its dependency is not Done: P is not selected (D is), and Q waits behind P.
	if got := pick(d1Project(), eligibleDispatchable); got != "D" {
		t.Fatalf("with D unbuilt, want D selected (P must wait on it), got %q", got)
	}
	// Its dependency is Done: P is selected, in declaration order ahead of O.
	if got := pick(doneD(d1Project()), eligibleDispatchable); got != "P" {
		t.Fatalf("with D Done, want the integration-pending P selected, got %q", got)
	}
	// The pre-D1 rule never picks P: it reads Running.
	if got := pick(doneD(d1Project()), eligibleNotStarted); got != "O" {
		t.Fatalf("under the pre-D1 rule, want O (P is not NotStarted), got %q", got)
	}
	// Once a pump started P (RecordActivityStarted: stored Running + StartedAt), a re-tick
	// must not dispatch it again — O is next, and Q still waits on P.
	started := doneD(d1Project())
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	p := started.ActivityConstruction["P"]
	p.Phase, p.StartedAt = projectstate.ActivityConstructionRunning, &now
	started.ActivityConstruction["P"] = p
	if got := pick(started, eligibleDispatchable); got != "O" {
		t.Fatalf("a P a pump has started must leave the dispatchable set, want O, got %q", got)
	}
	// A fully passed ledger is Done: never dispatched, and it satisfies Q.
	full := doneD(d1Project())
	full.ActivityConstruction["P"] = projectstate.ActivityConstructionStatus{ActivityID: "P", Attempts: passedLedger("P", servicePhases...)}
	if got := pick(full, eligibleDispatchable); got != "Q" {
		t.Fatalf("with P Done by its ledger, want Q, got %q", got)
	}
}

// The seed reads the REAL integration-pending row (C-billing-manager, verbatim from
// project.json): the four phases its ledger passed, not Integration; every task at #1.
func TestSeedResumeFromLedger_RealIntegrationPendingRow(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "integration-pending-row.json"))
	if err != nil {
		t.Fatal(err)
	}
	var row projectstate.ActivityConstructionStatus
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatal(err)
	}
	state := &constructState{completedPhases: map[projectstate.ActivityMethodPhase]bool{}}
	seedResumeFromLedger(state, constructionActivity{Type: projectstate.ActivityTypeService, Variant: projectstate.TestVariantPlan}, row)
	want := map[projectstate.ActivityMethodPhase]bool{
		projectstate.MethodPhaseRequirements: true, projectstate.MethodPhaseTestPlan: true,
		projectstate.MethodPhaseDetailedDesign: true, projectstate.MethodPhaseConstruction: true,
	}
	if !maps.Equal(state.completedPhases, want) {
		t.Fatalf("completedPhases = %v, want %v", state.completedPhases, want)
	}
	for _, task := range []projectstate.MethodTask{
		projectstate.TaskSRS, projectstate.TaskSRSReview, projectstate.TaskSTP, projectstate.TaskSTPReview,
		projectstate.TaskDetailedDesign, projectstate.TaskDesignReview, projectstate.TaskConstruction, projectstate.TaskCodeReview,
	} {
		if state.taskAttempts[task] != 1 {
			t.Errorf("taskAttempts[%s] = %d, want 1", task, state.taskAttempts[task])
		}
	}
	if n := state.taskAttempts[projectstate.TaskIntegration]; n != 0 {
		t.Errorf("taskAttempts[integration] = %d, want 0: the ledger holds no integration attempt", n)
	}
}

// Where the ledger has decided, it overrules the stored slice both ways: the all-false
// phases RecordPhaseStarted seeds do not undo the ledger's passed gates, and a rejected
// Integration gate undoes a stored completion. Where it is silent, stored state stands.
func TestSeedResumeFromLedger_LedgerOverrulesStoredWhereItDecided(t *testing.T) {
	stored := make([]projectstate.PhaseCompletion, 0, len(servicePhases))
	for _, ph := range servicePhases {
		stored = append(stored, projectstate.PhaseCompletion{Phase: ph, Completed: ph == projectstate.MethodPhaseIntegration})
	}
	attempts := passedLedger("A", projectstate.MethodPhaseRequirements, projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseDetailedDesign)
	attempts = append(attempts, ledgerAttempt("A", projectstate.TaskTesting, 3, projectstate.OutcomeRejected))
	row := projectstate.ActivityConstructionStatus{ActivityID: "A", Phases: stored, Attempts: attempts}
	state := &constructState{completedPhases: map[projectstate.ActivityMethodPhase]bool{}}
	seedResumeFromLedger(state, constructionActivity{Type: projectstate.ActivityTypeService, Variant: projectstate.TestVariantPlan}, row)
	want := map[projectstate.ActivityMethodPhase]bool{
		projectstate.MethodPhaseRequirements: true, projectstate.MethodPhaseTestPlan: true, projectstate.MethodPhaseDetailedDesign: true,
	}
	if !maps.Equal(state.completedPhases, want) {
		t.Fatalf("completedPhases = %v, want %v (construction undecided and stored false; integration rejected)", state.completedPhases, want)
	}
	if n := state.taskAttempts[projectstate.TaskTesting]; n != 3 {
		t.Fatalf("taskAttempts[testing] = %d, want 3 (the ledger's highest)", n)
	}
}

// d1ConstructRun runs one construct workflow for C-Orders whose row carries attempts,
// gitOn with no PR rail (the local profile, so the merge job runs), and returns what it did.
type d1Run struct {
	agent  []agenticjob.PipelineSpec
	merges []agenticjob.PipelineSpec
	ps     *fakeProjectState
	eps    *fakeEpisodes
	order  []string
	err    error
}

func d1ConstructRun(t *testing.T, attempts []projectstate.TaskAttempt, setup func(*testsuite.TestWorkflowEnvironment)) d1Run {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
	ps.project.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{
		"C-Orders": {ActivityID: "C-Orders", Attempts: attempts},
	}
	pipe := newFakePipeline()
	eps := &fakeEpisodes{}
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe, eps)
	var order []string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		order = append(order, info.ActivityType.Name)
	})
	if setup != nil {
		setup(env)
	}
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
	var agent []agenticjob.PipelineSpec
	for _, s := range pipe.submitted {
		if s.DispatchInputs[agenticjob.DispatchInputJobKey] != agenticjob.DispatchJobMerge {
			agent = append(agent, s)
		}
	}
	return d1Run{agent: agent, merges: mergeSubmits(pipe.submitted), ps: ps, eps: eps, order: order, err: env.GetWorkflowError()}
}

func (r d1Run) targetRefs() []string {
	var out []string
	for _, rec := range r.eps.records() {
		out = append(out, rec.TargetRef)
	}
	return out
}

// D.4: the ledger-seeded child dispatches Integration only, records the activity started
// BEFORE that dispatch, then merges and finalizes.
func Test_Construct_IntegrationPendingRow_RunsOnlyIntegrationThenMergesAndFinalizes(t *testing.T) {
	r := d1ConstructRun(t, passedLedger("C-Orders", replayPartialLedgerPhases...), nil)
	if r.err != nil {
		t.Fatalf("workflow error: %v", r.err)
	}
	if len(r.agent) != 1 || r.agent[0].DispatchInputs["phase"] != string(projectstate.MethodPhaseIntegration) {
		t.Fatalf("want exactly one agent dispatch, for integration; got %d: %v", len(r.agent), r.agent)
	}
	if len(r.merges) != 1 {
		t.Fatalf("want the local merge after integration, got %d merge submits", len(r.merges))
	}
	if len(r.ps.exited) != 1 || r.ps.exited[0].outcome != projectstate.ActivityOutcomeCompleted {
		t.Fatalf("want one Completed exit, got %v", r.ps.exited)
	}
	started := slices.Index(r.order, "gitActivityStatusAccess.recordActivityStarted")
	submit := slices.Index(r.order, "agenticJobAccess.submitAgenticJob")
	if started < 0 || submit < 0 || started > submit {
		t.Fatalf("RecordActivityStarted (at %d) must precede the pipeline (at %d): %v", started, submit, r.order)
	}
	if got := r.targetRefs(); !slices.Equal(got, []string{"C-Orders:integration:1"}) {
		t.Fatalf("episode TargetRefs = %v, want [C-Orders:integration:1]", got)
	}
}

// D.1.3: a ledger that already holds integration#1 makes the next dispatch #2, never a
// second #1.
func Test_Construct_IntegrationPendingRow_AttemptContinuesTheLedger(t *testing.T) {
	attempts := append(passedLedger("C-Orders", replayPartialLedgerPhases...),
		ledgerAttempt("C-Orders", projectstate.TaskIntegration, 1, projectstate.OutcomeFailed))
	r := d1ConstructRun(t, attempts, nil)
	if r.err != nil {
		t.Fatalf("workflow error: %v", r.err)
	}
	if got := r.targetRefs(); !slices.Equal(got, []string{"C-Orders:integration:2"}) {
		t.Fatalf("episode TargetRefs = %v, want [C-Orders:integration:2]", got)
	}
}

// DefaultVersion (an execution that seeded before D1): the stored-only seed, so the same
// row walks all five phases, numbered from #1.
func Test_Construct_LedgerPartialResume_DefaultVersion_KeepsTheStoredSeed(t *testing.T) {
	r := d1ConstructRun(t, passedLedger("C-Orders", replayPartialLedgerPhases...), func(env *testsuite.TestWorkflowEnvironment) {
		env.OnGetVersion(changeLedgerPartialResume, workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	})
	if r.err != nil {
		t.Fatalf("workflow error: %v", r.err)
	}
	if len(r.agent) != 5 {
		t.Fatalf("DefaultVersion must walk all five phases, got %d agent dispatches", len(r.agent))
	}
	if refs := r.targetRefs(); len(refs) != 5 || refs[0] != "C-Orders:srs:1" {
		t.Fatalf("DefaultVersion must number from #1 with no ledger seed, got %v", refs)
	}
}

// d1PumpRun runs one pump over replayPartialRowProject (D Done, P integration-pending, O
// not started) and returns the agent dispatches its child made.
func d1PumpRun(t *testing.T, setup func(*testsuite.TestWorkflowEnvironment)) []agenticjob.PipelineSpec {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	proj := replayPartialRowProject()
	proj.ID, proj.Version = "p-d1", 1
	ps := &fakeProjectState{project: proj}
	pipe := newFakePipeline()
	wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{}, Review: &fakeReview{}, NextEligibleActivity: nextEligibleActivity})
	registerPump(env, wf, ps, pipe)
	if setup != nil {
		setup(env)
	}
	env.ExecuteWorkflow(executionKindPump, pumpInput{ProjectID: "p-d1"})
	return pipe.submitted
}

// End to end: the pump picks the integration-pending P (its dependency is Done) and its
// child dispatches P's Integration phase only.
func Test_Pump_IntegrationPendingRow_DispatchesOnlyItsIntegration(t *testing.T) {
	got := d1PumpRun(t, nil)
	if len(got) != 1 || got[0].ActivityID != "P" || got[0].DispatchInputs["phase"] != string(projectstate.MethodPhaseIntegration) {
		t.Fatalf("want one dispatch, P's integration; got %v", got)
	}
}

// DefaultVersion (a pump that recorded the pre-D1 selection): O, from its first phase.
func Test_Pump_LedgerPartialResume_DefaultVersion_KeepsTheOldSelection(t *testing.T) {
	got := d1PumpRun(t, func(env *testsuite.TestWorkflowEnvironment) {
		env.OnGetVersion(changeLedgerPartialResume, workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	})
	if len(got) == 0 || got[0].ActivityID != "O" {
		t.Fatalf("DefaultVersion must keep the old choice, O; got %v", got)
	}
}

// ===========================================================================
// B1.2 — THE SESSION VIEW REPORTS THE HUMAN STAGE (plan B1.2).
// ===========================================================================

func b12View(t *testing.T, env *testsuite.TestWorkflowEnvironment) ConstructionSessionView {
	t.Helper()
	enc, err := env.QueryWorkflow(querySessionState)
	if err != nil {
		t.Fatalf("query session state: %v", err)
	}
	var v ConstructionSessionView
	if err := enc.Get(&v); err != nil {
		t.Fatalf("decode session view: %v", err)
	}
	return v
}

func b12Gate(v ConstructionSessionView) string {
	if v.AwaitingGate == nil {
		return ""
	}
	return *v.AwaitingGate
}

func b12Decide(env *testsuite.TestWorkflowEnvironment, key string, d PhaseDecision) func() {
	return func() {
		sig := phaseDecisionSignal{Phase: key, Decision: d}
		if d == PhaseSendBack {
			sig.Feedback = &ReviewFeedback{Notes: "redraft it"}
		}
		env.SignalWorkflow(signalPhaseDecision, sig)
	}
}

func b12Run(env *testsuite.TestWorkflowEnvironment) {
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
}

// A phase gate names itself and its occurrence; a redraft re-enters as a NEW occurrence;
// the decision clears the awaiting fields.
func Test_SessionView_PhaseGate_ReportsTheOccurrenceAndClearsOnDecision(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, newFakePipeline())
	start := env.Now()
	var atGate, afterRedraft ConstructionSessionView
	env.RegisterDelayedCallback(func() { atGate = b12View(t, env) }, 10*time.Second)
	env.RegisterDelayedCallback(b12Decide(env, "detailed_design", PhaseSendBack), 30*time.Second)
	env.RegisterDelayedCallback(func() { afterRedraft = b12View(t, env) }, 45*time.Second)
	env.RegisterDelayedCallback(b12Decide(env, "detailed_design", PhaseApprove), 60*time.Second)
	b12Run(env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if atGate.Stage != StageAwaitingApproval || b12Gate(atGate) != "detailed_design" {
		t.Fatalf("at the gate: stage=%v gate=%q, want awaitingApproval at detailed_design", atGate.Stage, b12Gate(atGate))
	}
	if atGate.AwaitingSince == nil || !atGate.AwaitingSince.Equal(start) {
		t.Fatalf("awaitingSince = %v, want the gate's entry time %v", atGate.AwaitingSince, start)
	}
	if atGate.AwaitingUntil != nil {
		t.Fatalf("an approval gate has no deadline, got awaitingUntil %v", atGate.AwaitingUntil)
	}
	if atGate.RedraftExhausted || atGate.Attempt != 1 || atGate.AttemptBudget != maxVarianceAttempts {
		t.Fatalf("exhausted=%v attempt=%d/%d, want false, 1/%d", atGate.RedraftExhausted, atGate.Attempt, atGate.AttemptBudget, maxVarianceAttempts)
	}
	if afterRedraft.AwaitingSince == nil || !afterRedraft.AwaitingSince.Equal(start.Add(30*time.Second)) {
		t.Fatalf("after the redraft awaitingSince = %v, want the re-entry time %v (a new occurrence)", afterRedraft.AwaitingSince, start.Add(30*time.Second))
	}
	if done := b12View(t, env); done.AwaitingGate != nil || done.AwaitingSince != nil || done.AwaitingUntil != nil {
		t.Fatalf("the decision must clear the awaiting fields, got gate=%v since=%v until=%v", done.AwaitingGate, done.AwaitingSince, done.AwaitingUntil)
	}
}

// An escalation waits at "takeover"; its deadline is since+wait when the wait is bounded
// and absent when it waits indefinitely; the operator's Retry starts the next attempt.
func Test_SessionView_Escalation_ReportsTakeoverAndItsDeadline(t *testing.T) {
	for _, c := range []struct {
		name string
		wait time.Duration
	}{{"bounded wait", time.Hour}, {"waits indefinitely", 0}} {
		t.Run(c.name, func(t *testing.T) {
			var ts testsuite.WorkflowTestSuite
			env := ts.NewTestWorkflowEnvironment()
			ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
			wf := newWorkflows(wfDeps{Intervention: &fakeIntervention{directive: intervention.VarianceEscalate}, Review: &fakeReview{}, EscalationWaitTimeout: c.wait})
			registerConstruct(env, wf, ps, newFakePipelineFailingOnce("detailed_design"))
			start := env.Now()
			var at ConstructionSessionView
			env.RegisterDelayedCallback(func() { at = b12View(t, env) }, time.Minute)
			env.RegisterDelayedCallback(func() {
				env.SignalWorkflow(signalOperatorOverride, operatorOverrideSignal{Override: ActivityOverride{Kind: OverrideRetry, Notes: "retry it"}})
			}, 2*time.Minute)
			b12Run(env)
			if err := env.GetWorkflowError(); err != nil {
				t.Fatalf("workflow error: %v", err)
			}
			b12CheckEscalation(t, at, start, c.wait)
			if done := b12View(t, env); done.AwaitingGate != nil || done.Attempt != 2 {
				t.Fatalf("after the Retry gate=%v attempt=%d, want cleared and attempt 2", done.AwaitingGate, done.Attempt)
			}
		})
	}
}

// b12CheckEscalation asserts the view an escalation reports while it waits.
func b12CheckEscalation(t *testing.T, at ConstructionSessionView, start time.Time, wait time.Duration) {
	t.Helper()
	if at.Stage != StageAwaitingTakeover || b12Gate(at) != takeoverGateKey || at.AwaitingSince == nil || !at.AwaitingSince.Equal(start) {
		t.Fatalf("at the escalation: stage=%v gate=%q since=%v, want awaitingTakeover at takeover since %v", at.Stage, b12Gate(at), at.AwaitingSince, start)
	}
	switch {
	case wait > 0 && (at.AwaitingUntil == nil || !at.AwaitingUntil.Equal(start.Add(wait))):
		t.Fatalf("awaitingUntil = %v, want %v", at.AwaitingUntil, start.Add(wait))
	case wait == 0 && at.AwaitingUntil != nil:
		t.Fatalf("an indefinite wait has no deadline, got %v", at.AwaitingUntil)
	}
	if at.Attempt != 1 || at.RedraftExhausted {
		t.Fatalf("at the escalation attempt=%d exhausted=%v, want 1 and false", at.Attempt, at.RedraftExhausted)
	}
}

// The redraft budget reads spent once a further SendBack could not redraft (after four
// redrafts, not three), and the NEXT gate starts with a fresh budget (plan G5: the flag
// used to leak into every later gate).
func Test_SessionView_RedraftBudget_SpentAfterFourAndResetAtTheNextGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign, projectstate.MethodPhaseConstruction))
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, newFakePipeline())
	var afterThree, afterFour, nextGate ConstructionSessionView
	for i := 1; i <= 4; i++ {
		env.RegisterDelayedCallback(b12Decide(env, "detailed_design", PhaseSendBack), time.Duration(30*i)*time.Second)
	}
	env.RegisterDelayedCallback(func() { afterThree = b12View(t, env) }, 105*time.Second)
	env.RegisterDelayedCallback(func() { afterFour = b12View(t, env) }, 135*time.Second)
	env.RegisterDelayedCallback(b12Decide(env, "detailed_design", PhaseApprove), 150*time.Second)
	env.RegisterDelayedCallback(func() { nextGate = b12View(t, env) }, 165*time.Second)
	env.RegisterDelayedCallback(b12Decide(env, "construction", PhaseApprove), 180*time.Second)
	env.RegisterDelayedCallback(b12Decide(env, mergeGateKey, PhaseApprove), 200*time.Second)
	b12Run(env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if afterThree.RedraftExhausted {
		t.Fatalf("after three redrafts one remains, but the view says the budget is spent")
	}
	if !afterFour.RedraftExhausted || b12Gate(afterFour) != "detailed_design" {
		t.Fatalf("after four redrafts gate=%q exhausted=%v, want detailed_design and spent", b12Gate(afterFour), afterFour.RedraftExhausted)
	}
	if b12Gate(nextGate) != "construction" || nextGate.RedraftExhausted {
		t.Fatalf("at the next gate gate=%q exhausted=%v, want construction with a fresh budget", b12Gate(nextGate), nextGate.RedraftExhausted)
	}
}

// The local merge hold is addressable: it reports awaitingGate "merge".
func Test_SessionView_MergeHold_ReportsTheMergeGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseConstruction))
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, newFakePipeline())
	start := env.Now()
	var hold ConstructionSessionView
	env.RegisterDelayedCallback(b12Decide(env, "construction", PhaseApprove), 20*time.Second)
	env.RegisterDelayedCallback(func() { hold = b12View(t, env) }, 40*time.Second)
	env.RegisterDelayedCallback(b12Decide(env, mergeGateKey, PhaseApprove), 60*time.Second)
	b12Run(env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if hold.Stage != StageAwaitingApproval || b12Gate(hold) != mergeGateKey || hold.AwaitingUntil != nil || hold.RedraftExhausted {
		t.Fatalf("at the merge hold: stage=%v gate=%q until=%v exhausted=%v", hold.Stage, b12Gate(hold), hold.AwaitingUntil, hold.RedraftExhausted)
	}
	if hold.AwaitingSince == nil || !hold.AwaitingSince.Equal(start.Add(20*time.Second)) {
		t.Fatalf("merge hold awaitingSince = %v, want %v", hold.AwaitingSince, start.Add(20*time.Second))
	}
}

// A variance retry starts the next supervision attempt.
func Test_SessionView_Attempt_CountsAVarianceRetry(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseIntegration))
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, newFakePipelineFailingOnce("test_plan"))
	var at ConstructionSessionView
	env.RegisterDelayedCallback(func() { at = b12View(t, env) }, 30*time.Second)
	env.RegisterDelayedCallback(b12Decide(env, "integration", PhaseApprove), 40*time.Second)
	b12Run(env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if b12Gate(at) != "integration" || at.Attempt != 2 || at.AttemptBudget != maxVarianceAttempts {
		t.Fatalf("gate=%q attempt=%d/%d, want integration at attempt 2/%d", b12Gate(at), at.Attempt, at.AttemptBudget, maxVarianceAttempts)
	}
}

// recordingMetrics captures what gateMetrics records (the SDK test environment has no
// metrics hook).
type recordingMetrics struct {
	mu     sync.Mutex
	timers []recordedTimer
}

type recordedTimer struct {
	name  string
	tags  map[string]string
	value time.Duration
}

type recordingMetricsHandler struct {
	rec  *recordingMetrics
	tags map[string]string
}

func (h recordingMetricsHandler) WithTags(tags map[string]string) client.MetricsHandler {
	merged := maps.Clone(h.tags)
	if merged == nil {
		merged = map[string]string{}
	}
	maps.Copy(merged, tags)
	return recordingMetricsHandler{rec: h.rec, tags: merged}
}

func (h recordingMetricsHandler) Counter(name string) client.MetricsCounter {
	return client.MetricsNopHandler.Counter(name)
}

func (h recordingMetricsHandler) Gauge(name string) client.MetricsGauge {
	return client.MetricsNopHandler.Gauge(name)
}

func (h recordingMetricsHandler) Timer(name string) client.MetricsTimer {
	return recordingMetricsTimer{h: h, name: name}
}

type recordingMetricsTimer struct {
	h    recordingMetricsHandler
	name string
}

func (t recordingMetricsTimer) Record(d time.Duration) {
	t.h.rec.mu.Lock()
	defer t.h.rec.mu.Unlock()
	t.h.rec.timers = append(t.h.rec.timers, recordedTimer{name: t.name, tags: t.h.tags, value: d})
}

// The gate-wait timer is recorded ONCE, when the human stage ends, with exactly the
// bounded tag set (never the activity id) and the time the gate actually waited.
func Test_GateWaitMetric_RecordedOnLeaveWithBoundedTags(t *testing.T) {
	rec := &recordingMetrics{}
	orig := gateMetrics
	gateMetrics = func(workflow.Context) client.MetricsHandler { return recordingMetricsHandler{rec: rec} }
	t.Cleanup(func() { gateMetrics = orig })

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, newFakePipeline())
	env.RegisterDelayedCallback(b12Decide(env, "detailed_design", PhaseApprove), 30*time.Second)
	b12Run(env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var waits []recordedTimer
	for _, r := range rec.timers {
		if r.name == "construction_gate_wait" {
			waits = append(waits, r)
		}
	}
	if len(waits) != 1 {
		t.Fatalf("want one construction_gate_wait record, got %+v", rec.timers)
	}
	want := map[string]string{"gate": "phase", "outcome": gateOutcomeApproved, "activity_type": "service"}
	if !maps.Equal(waits[0].tags, want) {
		t.Fatalf("tags = %v, want exactly %v", waits[0].tags, want)
	}
	if waits[0].value != 30*time.Second {
		t.Fatalf("recorded wait = %v, want the 30s the gate waited", waits[0].value)
	}
}

// ===========================================================================
// B1.3 — FAÇADE PRECHECKS → FailedPrecondition (plan B1.3).
// ===========================================================================

// b13Mock is a STRICT client: it answers the session Query with view (or queryErr) and
// expects SignalWorkflow only when signal is true — an unexpected call panics the test,
// which is what proves a refusal never signals.
func b13Mock(view ConstructionSessionView, queryErr error, signal bool) *temporalmocks.Client {
	mc := &temporalmocks.Client{}
	wfID := constructActivityWorkflowID("proj-1", "C-Orders")
	if queryErr != nil {
		mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).Return(nil, queryErr)
	} else {
		mc.On("QueryWorkflow", mock.Anything, wfID, "", querySessionState).Return(encodedJSON{v: view}, nil)
	}
	if signal {
		mc.On("SignalWorkflow", mock.Anything, wfID, "", mock.Anything, mock.Anything).Return(nil)
	}
	return mc
}

func TestSubmitPhaseDecision_Precheck_RefusesWhatTheSessionIsNotAwaiting(t *testing.T) {
	takeover := ConstructionSessionView{Stage: StageAwaitingTakeover, AwaitingGate: ptrTo(takeoverGateKey)}
	exhausted := awaitingAt("detailed_design")
	exhausted.RedraftExhausted = true
	oldWorker := ConstructionSessionView{Stage: StageAwaitingApproval} // served before B1.2: no awaitingGate
	note := &ReviewFeedback{Notes: "redraft it"}
	cases := []struct {
		name     string
		view     ConstructionSessionView
		key      string
		decision PhaseDecision
		feedback *ReviewFeedback
		mention  string
	}{
		{"a running pipeline", ConstructionSessionView{Stage: StagePipelineRunning}, "detailed_design", PhaseApprove, nil, "pipelineRunning/no gate"},
		{"another phase's gate", awaitingAt("requirements"), "detailed_design", PhaseApprove, nil, "awaitingApproval/requirements"},
		{"the merge hold, asked for a phase", awaitingAt(mergeGateKey), "construction", PhaseApprove, nil, "awaitingApproval/merge"},
		{"an escalation", takeover, "detailed_design", PhaseApprove, nil, "awaitingTakeover/takeover"},
		{"a view from an old worker", oldWorker, "detailed_design", PhaseApprove, nil, "no gate"},
		// The stage is checked, not only the gate label: a view that names the gate while
		// its stage is not awaiting approval is refused all the same.
		{"a gate label on a stage that is not awaiting", ConstructionSessionView{Stage: StagePipelineRunning, AwaitingGate: ptrTo("detailed_design")}, "detailed_design", PhaseApprove, nil, "pipelineRunning/detailed_design"},
		{"a send-back at a spent budget", exhausted, "detailed_design", PhaseSendBack, note, "spent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mc := b13Mock(c.view, nil, false)
			err := newTestConstructionManager(mc).SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", c.key, c.decision, c.feedback)
			e := asConstructionError(t, err)
			if e.Kind != fwmanager.FailedPrecondition || !strings.Contains(e.Detail, c.mention) {
				t.Fatalf("want FailedPrecondition naming %q, got %s %q", c.mention, e.Kind, e.Detail)
			}
			mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestSubmitPhaseDecision_Precheck_PassesAtTheAwaitedGate(t *testing.T) {
	exhausted := awaitingAt("detailed_design")
	exhausted.RedraftExhausted = true
	cases := []struct {
		name     string
		view     ConstructionSessionView
		key      string
		decision PhaseDecision
		feedback *ReviewFeedback
	}{
		{"approve at its gate", awaitingAt("detailed_design"), "detailed_design", PhaseApprove, nil},
		{"send back with budget left", awaitingAt("detailed_design"), "detailed_design", PhaseSendBack, &ReviewFeedback{Notes: "n"}},
		{"approve at a spent budget", exhausted, "detailed_design", PhaseApprove, nil},
		{"approve the merge hold", awaitingAt(mergeGateKey), mergeGateKey, PhaseApprove, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mc := b13Mock(c.view, nil, true)
			if err := newTestConstructionManager(mc).SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", c.key, c.decision, c.feedback); err != nil {
				t.Fatalf("want the decision signalled, got %v", err)
			}
			mc.AssertNumberOfCalls(t, "SignalWorkflow", 1)
		})
	}
}

// The check order is pinned: ContractMisuse never reads the session (the strict mock has
// no Query expectation), then a missing session is NotFound, then the precheck; a query
// fault is Infrastructure. None of them signals.
func TestSubmitPhaseDecision_Precheck_OrderIsContractMisuseThenSessionThenPrecondition(t *testing.T) {
	strict := &temporalmocks.Client{}
	m := newTestConstructionManager(strict)
	for name, call := range map[string]func() error{
		"unknown gate key": func() error {
			return m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "merged", PhaseApprove, nil)
		},
		"send-back without note": func() error {
			return m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseSendBack, nil)
		},
		"merge send-back": func() error {
			return m.SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", mergeGateKey, PhaseSendBack, &ReviewFeedback{Notes: "n"})
		},
	} {
		if got := asConstructionError(t, call()).Kind; got != fwmanager.ContractMisuse {
			t.Errorf("%s: want ContractMisuse before any session read, got %s", name, got)
		}
	}
	for name, c := range map[string]struct {
		err  error
		want fwmanager.Kind
	}{
		"no session":  {serviceerror.NewNotFound("workflow not found"), fwmanager.NotFound},
		"query fault": {errors.New("frontend unavailable"), fwmanager.Infrastructure},
	} {
		mc := b13Mock(ConstructionSessionView{}, c.err, false)
		err := newTestConstructionManager(mc).SubmitPhaseDecision(testCtx(), "proj-1", "C-Orders", "detailed_design", PhaseApprove, nil)
		if got := asConstructionError(t, err).Kind; got != c.want {
			t.Errorf("%s: want %s, got %s", name, c.want, got)
		}
		mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	}
}

func TestOverrideActivity_Precheck_OnlyAtATakeover(t *testing.T) {
	retry := ActivityOverride{Kind: OverrideRetry, Notes: "the server was down"}
	for name, view := range map[string]ConstructionSessionView{
		"a phase gate":       awaitingAt("detailed_design"),
		"the merge hold":     awaitingAt(mergeGateKey),
		"a running pipeline": {Stage: StagePipelineRunning},
		"an exited activity": {Stage: StageExited},
	} {
		mc := b13Mock(view, nil, false)
		err := newTestConstructionManager(mc).OverrideActivity(testCtx(), "proj-1", "C-Orders", retry)
		if e := asConstructionError(t, err); e.Kind != fwmanager.FailedPrecondition || !strings.Contains(e.Detail, "not awaiting a takeover") {
			t.Errorf("%s: want FailedPrecondition, got %s %q", name, e.Kind, e.Detail)
		}
		mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	}
	mc := b13Mock(ConstructionSessionView{Stage: StageAwaitingTakeover, AwaitingGate: ptrTo(takeoverGateKey)}, nil, true)
	if err := newTestConstructionManager(mc).OverrideActivity(testCtx(), "proj-1", "C-Orders", retry); err != nil {
		t.Fatalf("an override at a takeover must be signalled, got %v", err)
	}
	mc.AssertNumberOfCalls(t, "SignalWorkflow", 1)
	// ContractMisuse still comes first: blank notes never read the session.
	strict := &temporalmocks.Client{}
	if got := asConstructionError(t, newTestConstructionManager(strict).OverrideActivity(testCtx(), "proj-1", "C-Orders", ActivityOverride{Kind: OverrideRetry, Notes: " "})).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse before the session read, got %s", got)
	}
}

// Plan G7, end to end through the façade: an override sent while the activity waits at
// a PHASE gate is refused and never buffered, so the activity's later escalation still
// waits for a fresh steer — before B1.3 the stray Retry was consumed by that escalation.
func Test_Facade_StrayOverrideAtAGate_IsRefusedAndNotAppliedLater(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
	deps := gateDeps(ps)
	deps.Intervention = &fakeIntervention{directive: intervention.VarianceEscalate}
	registerConstruct(env, newWorkflows(deps), ps, newFakePipelineFailingOnce("construction"))
	env.SetStartWorkflowOptions(client.StartWorkflowOptions{ID: constructActivityWorkflowID("p", "C-Orders")})
	m := newTestConstructionManager(&envSignalClient{env: env})
	var strayErr error
	var atEscalation ConstructionSessionView
	env.RegisterDelayedCallback(func() {
		strayErr = m.OverrideActivity(testCtx(), "p", "C-Orders", ActivityOverride{Kind: OverrideRetry, Notes: "stray"})
	}, 20*time.Second)
	env.RegisterDelayedCallback(func() {
		if err := m.SubmitPhaseDecision(testCtx(), "p", "C-Orders", "detailed_design", PhaseApprove, nil); err != nil {
			t.Errorf("approve: %v", err)
		}
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() { atEscalation = b12View(t, env) }, 60*time.Second)
	env.RegisterDelayedCallback(func() {
		if err := m.OverrideActivity(testCtx(), "p", "C-Orders", ActivityOverride{Kind: OverrideSkip, Notes: "built by hand"}); err != nil {
			t.Errorf("override at the takeover: %v", err)
		}
	}, 90*time.Second)
	b12Run(env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if e := asConstructionError(t, strayErr); e.Kind != fwmanager.FailedPrecondition {
		t.Fatalf("the stray override: want FailedPrecondition, got %s", e.Kind)
	}
	if atEscalation.Stage != StageAwaitingTakeover {
		t.Fatalf("the escalation must wait for a fresh steer, got stage %v (the stray Retry was applied)", atEscalation.Stage)
	}
	if len(ps.exited) != 1 || ps.exited[0].outcome != projectstate.ActivityOutcomeSkipped {
		t.Fatalf("want the one Skip the operator sent at the takeover, got exits %v", ps.exited)
	}
}

func ptrTo[T any](v T) *T { return &v }

// ===========================================================================
// B1.4 — OPERATOR-NOTE DELIVERY (plan B1.4; amendment §C.1.4). A note is pending from
// the moment it is recorded until an agent dispatch carries it; every pending note rides
// the NEXT agent dispatch and is stamped delivered to that dispatch's AttemptID. On the
// GitHub venue the managed scaffold is synced before every dispatch. All of it sits
// behind the operator-note-delivery version.
// ===========================================================================

// replayScenariosPostB1 are captured from the B1.4 code (h1/h2 re-driven, plus a
// rail-wired run whose history carries the managed-scaffold sync), so the versioned path
// is pinned by replay too.
func replayScenariosPostB1() []replayScenario {
	return []replayScenario{
		{
			dir: "post-b1", name: "gate-sendback-redraft-approve",
			rig: func() replayRig { return replayGateRig(replayGatedOn(projectstate.MethodPhaseDetailedDesign)) },
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, r replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitView(ctx, t, c, run.GetID(), "the detailed_design gate", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingApproval
				})
				before := replaySubmitted(r.pipe)
				replaySignal(ctx, t, c, run.GetID(), signalPhaseDecision, phaseDecisionSignal{
					Phase: "detailed_design", Decision: PhaseSendBack,
					Feedback: &ReviewFeedback{Notes: "tighten the error model", Comments: []AnchoredComment{{JSONPath: "$.ops[0]", Text: "name the failure"}}},
				})
				replayAwaitView(ctx, t, c, run.GetID(), "the redraft's gate", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingApproval && replaySubmitted(r.pipe) == before+1
				})
				replaySignal(ctx, t, c, run.GetID(), signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
		{
			dir: "post-b1", name: "escalate-override-retry",
			rig: func() replayRig { return replayEscalateRig(false, time.Hour) },
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitView(ctx, t, c, run.GetID(), "the escalation", func(v ConstructionSessionView) bool {
					return v.Stage == StageAwaitingTakeover
				})
				replaySignal(ctx, t, c, run.GetID(), signalOperatorOverride, operatorOverrideSignal{Override: ActivityOverride{
					Kind: OverrideRetry, Notes: "the fixture server was down; retry",
				}})
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
		{
			// B1.7: at pump-honors-recorded-pause v2 the recorded pause binds EVERY pump —
			// here one started operator-driven (a pre-B1.7 caller's input) goes quiet.
			dir: "post-b17", name: "pump-recorded-pause-binds-every-pump",
			rig: replayPausedPumpRig,
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				return replayRunPumpOnce(ctx, t, c, tq, pumpInput{ProjectID: replayProjectID, OperatorDriven: true})
			},
		},
		{
			dir: "post-b1", name: "rail-sync-before-each-dispatch",
			rig: replayRailRig,
			drive: func(ctx context.Context, t *testing.T, c client.Client, tq string, _ replayRig) (string, string, bool) {
				run := replayStartConstruct(ctx, t, c, tq)
				replayAwaitDone(ctx, t, run)
				return run.GetID(), run.GetRunID(), false
			},
		},
	}
}

// replayPausedPumpRig serves a project whose pause is RECORDED to a pump that selects
// with the production rule.
func replayPausedPumpRig() replayRig {
	proj := ledgerChain()
	proj.OperatorPaused = true
	proj.PauseReason = "operator halt"
	return replayPumpRig(proj)
}

// replayRailRig wires the PR rail (the GitHub venue): every dispatch is preceded by the
// managed-scaffold sync.
func replayRailRig() replayRig {
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
	ps.project.ID = projectstate.ProjectID(replayProjectID)
	rail := &stubRail{prRef: "pr-7", ciRollup: sourcecontrol.CheckSuccess, merged: true}
	d := wfDeps{
		Intervention: &fakeIntervention{directive: intervention.VarianceRetry},
		GitStatus:    ps,
		RailEnabled:  true,
		Repo: func(_ ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo-1"), true
		},
	}
	return replayRig{wf: replayWorkflows(d), ps: ps, pipe: newFakePipeline(), rail: rail}
}

// submittedSpecs snapshots a pipeline double's submissions.
func submittedSpecs(pipe agenticjob.AgenticJobAccess) []agenticjob.PipelineSpec {
	switch p := pipe.(type) {
	case *fakePipeline:
		p.mu.Lock()
		defer p.mu.Unlock()
		return append([]agenticjob.PipelineSpec(nil), p.submitted...)
	case *failOncePipeline:
		p.mu.Lock()
		defer p.mu.Unlock()
		return append([]agenticjob.PipelineSpec(nil), p.submitted...)
	case *mergeConflictPipeline:
		p.mu.Lock()
		defer p.mu.Unlock()
		return append([]agenticjob.PipelineSpec(nil), p.submitted...)
	case orderedPipeline:
		return submittedSpecs(p.fakePipeline)
	case *noteRefusingPipeline:
		return submittedSpecs(p.fakePipeline)
	}
	return nil
}

// phaseSpecs are the agent dispatches of one phase, in order.
func phaseSpecs(specs []agenticjob.PipelineSpec, phase projectstate.ActivityMethodPhase) []agenticjob.PipelineSpec {
	var out []agenticjob.PipelineSpec
	for _, s := range specs {
		if s.DispatchInputs["phase"] == phase.String() {
			out = append(out, s)
		}
	}
	return out
}

// carriedNotes counts the dispatches that carried an operator_note input.
func carriedNotes(specs []agenticjob.PipelineSpec) int {
	n := 0
	for _, s := range specs {
		if _, ok := s.DispatchInputs[dispatchInputOperatorNote]; ok {
			n++
		}
	}
	return n
}

func runNoteConstruct(t *testing.T, env *testsuite.TestWorkflowEnvironment) {
	t.Helper()
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()})
}

// Test_NoteDelivery_SendBackNoteRidesTheRedraftAndIsStamped is the h1 shape: the store
// holds one sendBack note; the redraft's dispatch carries it (text, comment, id); it is
// stamped to detailedDesign#2; nothing else carries a note.
func Test_NoteDelivery_SendBackNoteRidesTheRedraftAndIsStamped(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
	pipe := newFakePipeline()
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, pipe)
	fb := &ReviewFeedback{Notes: "tighten the error model", Comments: []AnchoredComment{{JSONPath: "$.ops[0]", Text: "name the failure"}}}
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseSendBack, Feedback: fb})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 60*time.Second)
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	n := assertOneSendBackNote(t, ps, fb)
	specs := submittedSpecs(pipe)
	dd := phaseSpecs(specs, projectstate.MethodPhaseDetailedDesign)
	if len(dd) != 2 {
		t.Fatalf("want the draft and its redraft, got %d detailed_design dispatches", len(dd))
	}
	if _, ok := dd[0].DispatchInputs[dispatchInputOperatorNote]; ok {
		t.Fatal("the first draft predates the note and must carry none")
	}
	got := dd[1].DispatchInputs[dispatchInputOperatorNote]
	for _, want := range []string{n.note.NoteID, "sendBack at detailed_design", fb.Notes, "comment on $.ops[0]: name the failure"} {
		if !strings.Contains(got, want) {
			t.Errorf("the redraft's operator_note lacks %q:\n%s", want, got)
		}
	}
	if c := carriedNotes(specs); c != 1 {
		t.Fatalf("exactly one dispatch carries the note, got %d", c)
	}
	want := deliveredCall{activityID: "C-Orders", noteID: n.note.NoteID, attemptID: projectstate.AttemptID("C-Orders", projectstate.AgentTaskFor(projectstate.MethodPhaseDetailedDesign), 2)}
	if len(ps.delivered) != 1 || ps.delivered[0] != want {
		t.Fatalf("delivery stamps = %+v, want [%+v]", ps.delivered, want)
	}
}

// assertOneSendBackNote asserts the store holds exactly the send-back's note, verbatim.
func assertOneSendBackNote(t *testing.T, ps *fakeProjectState, fb *ReviewFeedback) noteCall {
	t.Helper()
	if len(ps.notes) != 1 {
		t.Fatalf("want one recorded note, got %+v", ps.notes)
	}
	n := ps.notes[0]
	wantComment := projectstate.NoteComment{JSONPath: "$.ops[0]", Text: "name the failure"}
	switch {
	case n.activityID != "C-Orders", n.note.Kind != projectstate.NoteSendBack, n.note.Gate != "detailed_design", n.note.Text != fb.Notes:
		t.Fatalf("recorded note = %+v", n)
	case len(n.note.Comments) != 1 || n.note.Comments[0] != wantComment:
		t.Fatalf("recorded comments = %+v", n.note.Comments)
	case !strings.HasPrefix(n.note.NoteID, "C-Orders:note:"):
		t.Fatalf("note id = %q", n.note.NoteID)
	}
	return n
}

// noteEscalateRig fails detailed_design's first dispatch into an escalation that waits
// for the operator (no timeout).
func noteEscalateRig(t *testing.T, pipe agenticjob.AgenticJobAccess, policy projectstate.ReviewPolicy, gitOn bool) (*testsuite.TestWorkflowEnvironment, *fakeProjectState) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(policy)
	d := wfDeps{Intervention: &fakeIntervention{directive: intervention.VarianceEscalate}, Review: &fakeReview{}}
	if gitOn {
		d.GitStatus = ps
	}
	registerConstruct(env, newWorkflows(d), ps, pipe)
	return env, ps
}

func sendOverride(env *testsuite.TestWorkflowEnvironment, at time.Duration, kind OverrideKind, notes string) {
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalOperatorOverride, operatorOverrideSignal{Override: ActivityOverride{Kind: kind, Notes: notes}})
	}, at)
}

// Test_NoteDelivery_RetryNoteRidesTheFirstIncompletePhase is the h2 shape: the note
// rides the first incomplete phase's agent dispatch after the retry.
func Test_NoteDelivery_RetryNoteRidesTheFirstIncompletePhase(t *testing.T) {
	pipe := newFakePipelineFailingOnce("detailed_design")
	env, ps := noteEscalateRig(t, pipe, projectstate.ReviewPolicy{}, false)
	sendOverride(env, 2*time.Minute, OverrideRetry, "the fixture server was down; retry")
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.notes) != 1 || ps.notes[0].note.Kind != projectstate.NoteRetry || ps.notes[0].note.Gate != takeoverGateKey {
		t.Fatalf("want one retry note at the takeover, got %+v", ps.notes)
	}
	dd := phaseSpecs(submittedSpecs(pipe), projectstate.MethodPhaseDetailedDesign)
	if len(dd) != 2 || !strings.Contains(dd[1].DispatchInputs[dispatchInputOperatorNote], "the fixture server was down; retry") {
		t.Fatalf("the retried detailed_design dispatch must carry the note; dispatches=%d", len(dd))
	}
	want := projectstate.AttemptID("C-Orders", projectstate.AgentTaskFor(projectstate.MethodPhaseDetailedDesign), 2)
	if len(ps.delivered) != 1 || ps.delivered[0].attemptID != want || ps.delivered[0].noteID != ps.notes[0].note.NoteID {
		t.Fatalf("delivery stamps = %+v, want the note stamped to %s", ps.delivered, want)
	}
}

// Test_NoteDelivery_SkipNoteIsKeptAndNeverPending: nothing runs after a skip.
func Test_NoteDelivery_SkipNoteIsKeptAndNeverPending(t *testing.T) {
	pipe := newFakePipelineFailingOnce("detailed_design")
	env, ps := noteEscalateRig(t, pipe, projectstate.ReviewPolicy{}, false)
	sendOverride(env, 2*time.Minute, OverrideSkip, "built by hand; nothing to construct")
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.notes) != 1 || ps.notes[0].note.Kind != projectstate.NoteSkip {
		t.Fatalf("want one skip note, got %+v", ps.notes)
	}
	if len(ps.delivered) != 0 || carriedNotes(submittedSpecs(pipe)) != 0 {
		t.Fatalf("a skip note is never delivered: stamps=%+v", ps.delivered)
	}
}

// Test_NoteDelivery_RetryWithOnlyTheMergeLeftRecordsAndNeverStamps: a Retry whose phases
// are all complete re-runs only the local merge job, which is not an agent run, so the
// note is kept on the activity, undelivered, with no stamp.
func Test_NoteDelivery_RetryWithOnlyTheMergeLeftRecordsAndNeverStamps(t *testing.T) {
	pipe := &mergeConflictPipeline{}
	env, ps := noteEscalateRig(t, pipe, vibesPreset(), true)
	sendOverride(env, time.Minute, OverrideRetry, "retry the merge after the rebase")
	sendOverride(env, 2*time.Minute, OverrideSkip, "merged by hand")
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.notes) != 2 || ps.notes[0].note.Kind != projectstate.NoteRetry || ps.notes[1].note.Kind != projectstate.NoteSkip {
		t.Fatalf("want the retry note then the skip note, got %+v", ps.notes)
	}
	if ps.notes[0].note.NoteID == ps.notes[1].note.NoteID {
		t.Fatalf("two notes of one run must have distinct ids, both are %q", ps.notes[0].note.NoteID)
	}
	if len(ps.delivered) != 0 || carriedNotes(submittedSpecs(pipe)) != 0 {
		t.Fatalf("no agent dispatch followed the retry, so nothing may be stamped or carried: %+v", ps.delivered)
	}
}

// Test_NoteDelivery_DefaultVersionKeepsTheOldSequence: an execution without the marker
// records, carries and stamps nothing — the pre-B1 command sequence.
func Test_NoteDelivery_DefaultVersionKeepsTheOldSequence(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
	pipe := newFakePipeline()
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, pipe)
	// Mocks follow registration (the test environment refuses the other order).
	env.OnGetVersion(changeOperatorNoteDelivery, workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseSendBack, Feedback: &ReviewFeedback{Notes: "redo it"}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseApprove})
	}, 60*time.Second)
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(ps.notes) != 0 || len(ps.delivered) != 0 || carriedNotes(submittedSpecs(pipe)) != 0 {
		t.Fatalf("DefaultVersion must record/stamp/carry nothing: notes=%+v stamps=%+v", ps.notes, ps.delivered)
	}
}

// TestDispatchInputsFor_NoNoteIsByteIdentical: a no-note dispatch's inputs are exactly
// the pre-B1 set; a note adds exactly one key.
func TestDispatchInputsFor_NoNoteIsByteIdentical(t *testing.T) {
	spec := pipelineSpec{ActivityID: "C-1", ComponentID: "comp", Phase: projectstate.MethodPhaseConstruction.String()}
	plain := dispatchInputsFor(spec)
	if len(plain) != 4 || plain["activity_id"] != "C-1" || plain["component_id"] != "comp" || plain["phase"] != "construction" || plain["command"] == "" {
		t.Fatalf("no-note inputs = %v, want exactly activity_id/component_id/phase/command", plain)
	}
	spec.OperatorNote = "carry this"
	withNote := dispatchInputsFor(spec)
	if len(withNote) != 5 || withNote[dispatchInputOperatorNote] != "carry this" {
		t.Fatalf("with a note, inputs = %v", withNote)
	}
	for k, v := range plain {
		if withNote[k] != v {
			t.Fatalf("the note changed input %q", k)
		}
	}
}

// Test_NoteDelivery_PendingNoteFromTheStoreRidesTheFirstDispatch: a note already pending
// on the row (a re-queue's note, as B2 records it) rides this run's first agent dispatch
// and is stamped; a skip note and an already-delivered note never ride.
func Test_NoteDelivery_PendingNoteFromTheStoreRidesTheFirstDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	ps.project.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{"C-Orders": {OperatorNotes: []projectstate.OperatorNote{
		{NoteID: "C-Orders:note:reopen:1", Kind: projectstate.NoteRequeue, Text: "the flaky dependency is pinned now", RecordedAt: at},
		{NoteID: "C-Orders:note:old:1", Kind: projectstate.NoteSkip, Text: "skip note", RecordedAt: at},
		{NoteID: "C-Orders:note:done:1", Kind: projectstate.NoteRetry, Text: "already delivered", RecordedAt: at, DeliveredToAttemptID: "C-Orders:srs:1", DeliveredAt: &at},
	}}}
	pipe := newFakePipeline()
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, pipe)
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	specs := submittedSpecs(pipe)
	first := specs[0].DispatchInputs[dispatchInputOperatorNote]
	if !strings.Contains(first, "the flaky dependency is pinned now") || strings.Contains(first, "skip note") || strings.Contains(first, "already delivered") {
		t.Fatalf("the first dispatch must carry only the pending re-queue note:\n%s", first)
	}
	if carriedNotes(specs) != 1 {
		t.Fatalf("only the first dispatch carries the store's note, got %d", carriedNotes(specs))
	}
	want := deliveredCall{activityID: "C-Orders", noteID: "C-Orders:note:reopen:1", attemptID: projectstate.AttemptID("C-Orders", projectstate.AgentTaskFor(projectstate.MethodPhaseRequirements), 1)}
	if len(ps.delivered) != 1 || ps.delivered[0] != want {
		t.Fatalf("stamps = %+v, want [%+v]", ps.delivered, want)
	}
}

// noteRefusingPipeline refuses (ContractMisuse, terminal) any submit carrying a note.
type noteRefusingPipeline struct{ *fakePipeline }

func (p *noteRefusingPipeline) SubmitAgenticJob(rc fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	if _, ok := spec.DispatchInputs[dispatchInputOperatorNote]; ok {
		return "", fwra.New(fwra.ContractMisuse, "refused: a note-bearing dispatch")
	}
	return p.fakePipeline.SubmitAgenticJob(rc, spec)
}

// Test_NoteDelivery_NoStampUnlessTheSubmitSucceeded: the stamp follows a SUCCESSFUL
// submit only; a refused one leaves the note undelivered.
func Test_NoteDelivery_NoStampUnlessTheSubmitSucceeded(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(replayGatedOn(projectstate.MethodPhaseDetailedDesign))
	pipe := &noteRefusingPipeline{newFakePipeline()}
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, pipe)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalPhaseDecision, phaseDecisionSignal{Phase: "detailed_design", Decision: PhaseSendBack, Feedback: &ReviewFeedback{Notes: "redo it"}})
	}, 30*time.Second)
	runNoteConstruct(t, env)
	if env.GetWorkflowError() == nil {
		t.Fatal("a refused redraft submit fails the run here")
	}
	if len(ps.notes) != 1 || len(ps.delivered) != 0 {
		t.Fatalf("the note is kept but never stamped when its dispatch was refused: notes=%d stamps=%+v", len(ps.notes), ps.delivered)
	}
}

// orderedPipeline logs "submit" before each dispatch into the shared call log.
type orderedPipeline struct {
	*fakePipeline
	order *callLog
}

func (p orderedPipeline) SubmitAgenticJob(rc fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	p.order.add("submit")
	return p.fakePipeline.SubmitAgenticJob(rc, spec)
}

// noteGitRun runs one rail-wired (GitHub venue) activity with the sync and the submits
// on one call log.
func noteGitRun(t *testing.T, rail *stubRail, version *workflow.Version) (*testsuite.TestWorkflowEnvironment, *fakeProjectState, orderedPipeline) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 5, Phase: 2}, version: 5}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true)
	pipe := orderedPipeline{fakePipeline: newFakePipeline(), order: rail.order}
	env.RegisterWorkflowWithOptions(wf.ConstructActivityWorkflow, workflow.RegisterOptions{Name: executionKindConstructActivity})
	registerGenPipeline(env, pipe)
	registerGenEpisodes(env, nil)
	registerGenDesignSessionRead(env, ps)
	registerGenProjectStateVersion(env, ps)
	registerGenConstructionTransition(env, ps)
	registerGenGitStatus(env, git)
	registerGenRail(env, rail)
	if version != nil {
		env.OnGetVersion(changeOperatorNoteDelivery, workflow.DefaultVersion, 1).Return(*version)
	}
	env.ExecuteWorkflow(executionKindConstructActivity, constructActivityInput{ProjectID: pid, ActivityID: "C-MST", Activity: gitSampleActivity()})
	return env, ps, pipe
}

// Test_NoteDelivery_ScaffoldSyncPrecedesEveryGitHubDispatch: on the GitHub venue the
// managed scaffold is synced immediately before every agent dispatch.
func Test_NoteDelivery_ScaffoldSyncPrecedesEveryGitHubDispatch(t *testing.T) {
	rail := &stubRail{prRef: "pr-7", ciRollup: sourcecontrol.CheckSuccess, order: &callLog{}}
	env, _, _ := noteGitRun(t, rail, nil)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	want := strings.Repeat("sync→submit→", 4) + "sync→submit"
	if got := rail.order.String(); got != want {
		t.Fatalf("call order = %s\nwant       %s", got, want)
	}
}

// Test_NoteDelivery_NoSyncOnTheLocalVenue: the local venue has no seated workflow, so no
// sync is ever issued there (a rail is registered only to catch one).
func Test_NoteDelivery_NoSyncOnTheLocalVenue(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
	rail := &stubRail{}
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, newFakePipeline())
	registerGenRail(env, rail)
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if rail.syncs != 0 {
		t.Fatalf("the local venue must never sync, got %d", rail.syncs)
	}
}

// Test_NoteDelivery_DefaultVersionNeverSyncs: the sync shares the change id, so an old
// execution never issues it.
func Test_NoteDelivery_DefaultVersionNeverSyncs(t *testing.T) {
	rail := &stubRail{prRef: "pr-7", ciRollup: sourcecontrol.CheckSuccess}
	dv := workflow.DefaultVersion
	env, _, _ := noteGitRun(t, rail, &dv)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if rail.syncs != 0 {
		t.Fatalf("DefaultVersion must not sync, got %d", rail.syncs)
	}
}

// Test_NoteDelivery_FailedSyncDispatchesNothing: a sync that fails dispatches nothing and
// reads as a failed run, so the variance path takes it (here, Retry until exhausted).
func Test_NoteDelivery_FailedSyncDispatchesNothing(t *testing.T) {
	rail := &stubRail{prRef: "pr-7", ciRollup: sourcecontrol.CheckSuccess, syncErr: fwra.New(fwra.Auth, "the installation token was refused")}
	env, ps, pipe := noteGitRun(t, rail, nil)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if n := len(submittedSpecs(pipe)); n != 0 {
		t.Fatalf("a failed sync must dispatch nothing, got %d submits", n)
	}
	if rail.syncs != maxVarianceAttempts {
		t.Fatalf("one sync per attempt, got %d", rail.syncs)
	}
	if len(ps.failed) != 1 || ps.failed[0].reason != projectstate.VarianceExhausted {
		t.Fatalf("want the variance path to exhaust, got %+v", ps.failed)
	}
}

func isContractMisuse(err error) bool {
	var fe *fwmanager.Error
	return errors.As(err, &fe) && fe.Kind == fwmanager.ContractMisuse
}

// TestFacade_OperatorNoteIsCappedAt4000Characters: a send-back's feedback and an
// override's notes are at most 4,000 characters, anchored comments included —
// ContractMisuse beyond, and nothing is signalled.
func TestFacade_OperatorNoteIsCappedAt4000Characters(t *testing.T) {
	over := strings.Repeat("é", maxOperatorNoteRunes+1)
	withComment := strings.Repeat("a", maxOperatorNoteRunes-10)
	comments := []AnchoredComment{{JSONPath: "$.ops[0]", Text: strings.Repeat("b", 11)}}
	for name, call := range map[string]func(m *constructionManager) error{
		"send-back text": func(m *constructionManager) error {
			return m.SubmitPhaseDecision(testCtx(), "p", "C-Orders", "detailed_design", PhaseSendBack, &ReviewFeedback{Notes: over})
		},
		"send-back comments": func(m *constructionManager) error {
			return m.SubmitPhaseDecision(testCtx(), "p", "C-Orders", "detailed_design", PhaseSendBack, &ReviewFeedback{Notes: withComment, Comments: comments})
		},
		"override text": func(m *constructionManager) error {
			return m.OverrideActivity(testCtx(), "p", "C-Orders", ActivityOverride{Kind: OverrideRetry, Notes: over})
		},
		"override comments": func(m *constructionManager) error {
			return m.OverrideActivity(testCtx(), "p", "C-Orders", ActivityOverride{Kind: OverrideRetry, Notes: withComment, Comments: comments})
		},
	} {
		fc := &fakeTemporalClient{}
		if err := call(newTestConstructionManager(fc)); !isContractMisuse(err) {
			t.Errorf("%s over the cap: want ContractMisuse, got %v", name, err)
		}
		if fc.lastSignalName != "" {
			t.Errorf("%s: a refused note must not signal", name)
		}
	}
	// M6: a comment's JSONPath counts too ("$.ops[0]" is 8 characters).
	if operatorNoteRunes(strings.Repeat("é", maxOperatorNoteRunes-19), comments) != maxOperatorNoteRunes {
		t.Fatal("the count is characters (runes) of the text plus each comment's text and JSONPath")
	}
	longPath := []AnchoredComment{{JSONPath: "$." + strings.Repeat("p", 3000), Text: "x"}}
	for name, call := range map[string]func(m *constructionManager) error{
		"send-back": func(m *constructionManager) error {
			return m.SubmitPhaseDecision(testCtx(), "p", "C-Orders", "detailed_design", PhaseSendBack, &ReviewFeedback{Notes: strings.Repeat("a", 1500), Comments: longPath})
		},
		"override": func(m *constructionManager) error {
			return m.OverrideActivity(testCtx(), "p", "C-Orders", ActivityOverride{Kind: OverrideRetry, Notes: strings.Repeat("a", 1500), Comments: longPath})
		},
	} {
		fc := &fakeTemporalClient{}
		if err := call(newTestConstructionManager(fc)); !isContractMisuse(err) || !strings.Contains(err.Error(), "paths included") {
			t.Errorf("%s: a note over the cap only by its JSONPaths must be refused, got %v", name, err)
		}
		if fc.lastSignalName != "" {
			t.Errorf("%s: a refused note must not signal", name)
		}
	}
	// The rendered-bytes guard: under 4,000 characters, but over maxOperatorNoteBodyBytes
	// once rendered (four-byte characters), so it could not reach the agent whole.
	wide := strings.Repeat("😀", 3900)
	if operatorNoteRunes(wide, nil) > maxOperatorNoteRunes || len(wide) <= maxOperatorNoteBodyBytes {
		t.Fatal("the fixture must be under the character cap and over the byte cap")
	}
	for name, call := range map[string]func(m *constructionManager) error{
		"send-back": func(m *constructionManager) error {
			return m.SubmitPhaseDecision(testCtx(), "p", "C-Orders", "detailed_design", PhaseSendBack, &ReviewFeedback{Notes: wide})
		},
		"override": func(m *constructionManager) error {
			return m.OverrideActivity(testCtx(), "p", "C-Orders", ActivityOverride{Kind: OverrideRetry, Notes: wide})
		},
	} {
		fc := &fakeTemporalClient{}
		if err := call(newTestConstructionManager(fc)); !isContractMisuse(err) || !strings.Contains(err.Error(), "bytes once rendered") {
			t.Errorf("%s: a note too wide to render whole must be refused, got %v", name, err)
		}
	}
	if maxOperatorNoteRunes != 4000 {
		t.Fatalf("the ruled cap is 4,000 characters, got %d", maxOperatorNoteRunes)
	}
}

// TestRenderOperatorNotes_HeadsEachNote: each note is headed by its id, kind and gate,
// oldest first, and a block that fits carries every note whole.
func TestRenderOperatorNotes_HeadsEachNote(t *testing.T) {
	if r := renderOperatorNotes(nil); r.block != "" || len(r.whole) != 0 || r.withheld != 0 {
		t.Fatalf("no notes render nothing, got %+v", r)
	}
	n1 := projectstate.OperatorNote{NoteID: "n1", Kind: projectstate.NoteSendBack, Gate: "detailed_design", Text: "tighten it",
		Comments: []projectstate.NoteComment{{JSONPath: "$.ops[0]", Text: "name the failure"}}}
	one := renderOperatorNotes([]projectstate.OperatorNote{n1})
	if one.block != "[operator note n1 — sendBack at detailed_design]\ntighten it\n  comment on $.ops[0]: name the failure" {
		t.Fatalf("rendered = %q", one.block)
	}
	if len(one.whole) != 1 || one.whole[0].NoteID != "n1" || one.withheld != 0 {
		t.Fatalf("one note fits whole: %+v", one)
	}
	two := renderOperatorNotes([]projectstate.OperatorNote{n1, {NoteID: "n2", Kind: projectstate.NoteRetry, Text: "retry"}})
	if !strings.HasPrefix(two.block, "[operator note n1") || !strings.HasSuffix(two.block, "[operator note n2 — retry]\nretry") || len(two.whole) != 2 {
		t.Fatalf("two small notes ride whole, oldest first: %+v", two)
	}
}

// TestRenderOperatorNotes_TheCapWithholdsTheOldest (I1): over 16 KiB the NEWEST notes
// ride whole and the oldest are withheld — named by count, never cut — and only the
// notes carried whole are reported for the delivery stamp.
func TestRenderOperatorNotes_TheCapWithholdsTheOldest(t *testing.T) {
	var many []projectstate.OperatorNote
	for i := range 6 {
		many = append(many, projectstate.OperatorNote{NoteID: fmt.Sprintf("n%d", i), Kind: projectstate.NoteRetry, Text: strings.Repeat("😀", 900)})
	}
	r := renderOperatorNotes(many)
	if len(r.block) > maxRenderedOperatorNotesBytes {
		t.Fatalf("the block is %d bytes, over the %d cap", len(r.block), maxRenderedOperatorNotesBytes)
	}
	if r.withheld != 2 || len(r.whole) != 4 || r.whole[0].NoteID != "n2" || r.whole[3].NoteID != "n5" {
		t.Fatalf("want n0 and n1 withheld and n2..n5 whole, got withheld=%d whole=%v", r.withheld, noteIDs(r.whole))
	}
	if !strings.HasPrefix(r.block, withheldNotesLine(2)+notesSeparator+"[operator note n2 — retry]") {
		t.Fatalf("the block must open with the withheld count, then the oldest note it carries:\n%.300s", r.block)
	}
	for _, n := range r.whole {
		if !strings.Contains(r.block, renderNoteSection(n)) {
			t.Fatalf("note %s is not in the block whole", n.NoteID)
		}
	}
	if strings.Contains(r.block, "[operator note n0") || strings.Contains(r.block, "[operator note n1") || strings.Contains(r.block, "truncated") {
		t.Fatal("a withheld note is left out entirely, never cut")
	}
}

// TestRenderOperatorNotes_TheWithheldLineCountsAndFits: the withheld line names how many
// notes it withheld, and the four newest notes alone fit whole (the fit check counts the
// withheld line and the separators too, so withholding never over-drops).
func TestRenderOperatorNotes_TheWithheldLineCountsAndFits(t *testing.T) {
	if withheldNotesLine(1) == withheldNotesLine(2) || !strings.Contains(withheldNotesLine(2), "2 older operator notes") {
		t.Fatal("the withheld line names how many notes it withheld")
	}
	var four []projectstate.OperatorNote
	for i := range 4 {
		four = append(four, projectstate.OperatorNote{NoteID: fmt.Sprintf("n%d", i), Kind: projectstate.NoteRetry, Text: strings.Repeat("😀", 900)})
	}
	if exact := renderOperatorNotes(four); exact.withheld != 0 || len(exact.whole) != 4 {
		t.Fatalf("the four newest alone fit whole: withheld=%d whole=%d", exact.withheld, len(exact.whole))
	}
}

// TestRenderOperatorNotes_TheWithheldLineCountsTowardTheCap: the two newest notes fill
// the block to 10 bytes short of the cap, so they fit together only if the withheld
// line is forgotten. It is counted: the newest note rides alone, both older notes are
// withheld, and the block stays within the cap.
func TestRenderOperatorNotes_TheWithheldLineCountsTowardTheCap(t *testing.T) {
	head := len(renderNoteSection(projectstate.OperatorNote{NoteID: "n1", Kind: projectstate.NoteRetry}))
	n2 := projectstate.OperatorNote{NoteID: "n2", Kind: projectstate.NoteRetry, Text: strings.Repeat("b", 8000-head)}
	n1 := projectstate.OperatorNote{NoteID: "n1", Kind: projectstate.NoteRetry,
		Text: strings.Repeat("a", maxRenderedOperatorNotesBytes-10-len(notesSeparator)-8000-head)}
	n0 := projectstate.OperatorNote{NoteID: "n0", Kind: projectstate.NoteRetry, Text: "oldest"}
	if got := len(renderNoteSection(n1)) + len(notesSeparator) + len(renderNoteSection(n2)); got != maxRenderedOperatorNotesBytes-10 {
		t.Fatalf("fixture: the two newest notes take %d bytes, want the cap minus 10", got)
	}
	r := renderOperatorNotes([]projectstate.OperatorNote{n0, n1, n2})
	if len(r.block) > maxRenderedOperatorNotesBytes {
		t.Fatalf("the block is %d bytes, over the %d cap: the withheld line was not counted", len(r.block), maxRenderedOperatorNotesBytes)
	}
	if r.withheld != 2 || len(r.whole) != 1 || r.whole[0].NoteID != "n2" {
		t.Fatalf("want n2 alone, n0 and n1 withheld; got whole=%v withheld=%d", noteIDs(r.whole), r.withheld)
	}
}

// TestRenderOperatorNotes_AnOversizedNoteIsCutAndNeverWhole: a note the block cannot
// hold whole (only a signal that bypassed the façade) rides cut and marked, and is not
// among the whole notes, so it is never stamped delivered.
func TestRenderOperatorNotes_AnOversizedNoteIsCutAndNeverWhole(t *testing.T) {
	big := projectstate.OperatorNote{NoteID: "big", Kind: projectstate.NoteRetry, Text: strings.Repeat("😀", 5000)}
	for _, notes := range [][]projectstate.OperatorNote{{big}, {{NoteID: "old", Kind: projectstate.NoteRetry, Text: "old"}, big}} {
		r := renderOperatorNotes(notes)
		if len(r.whole) != 0 || r.withheld != len(notes)-1 {
			t.Fatalf("an oversized note is never whole: %+v", r.whole)
		}
		if len(r.block) > maxRenderedOperatorNotesBytes || !strings.HasSuffix(r.block, renderedNotesTruncated) {
			t.Fatalf("the block must be cut to the cap and marked (len %d)", len(r.block))
		}
		if strings.ToValidUTF8(r.block, "\uFFFD") != r.block {
			t.Fatal("the cut must fall on a rune boundary")
		}
	}
}

func noteIDs(notes []projectstate.OperatorNote) []string {
	var out []string
	for _, n := range notes {
		out = append(out, n.NoteID)
	}
	return out
}

// seededPendingNotes stores n pending retry notes of size bytes each on C-Orders.
func seededPendingNotes(ps *fakeProjectState, n, size int) {
	at := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	var notes []projectstate.OperatorNote
	for i := range n {
		notes = append(notes, projectstate.OperatorNote{NoteID: fmt.Sprintf("C-Orders:note:seed:%d", i), Kind: projectstate.NoteRetry,
			Text: fmt.Sprintf("note %d ", i) + strings.Repeat("x", size), RecordedAt: at})
	}
	ps.project.ActivityConstruction = map[string]projectstate.ActivityConstructionStatus{"C-Orders": {OperatorNotes: notes}}
}

// Test_NoteDelivery_OnlyNotesCarriedWholeAreStamped (I1, the workflow): five pending
// notes too big for one block — the first dispatch carries the four newest whole and
// stamps only them; the withheld oldest stays pending and the NEXT dispatch carries it
// and stamps it.
func Test_NoteDelivery_OnlyNotesCarriedWholeAreStamped(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
	seededPendingNotes(ps, 5, 3900)
	pipe := newFakePipeline()
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, pipe)
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	specs := submittedSpecs(pipe)
	first, second := specs[0].DispatchInputs[dispatchInputOperatorNote], specs[1].DispatchInputs[dispatchInputOperatorNote]
	if !strings.HasPrefix(first, withheldNotesLine(1)) || strings.Contains(first, "note 0 ") || !strings.Contains(first, "note 4 ") {
		t.Fatalf("the first dispatch must withhold the oldest and carry the newest:\n%.200s", first)
	}
	if !strings.Contains(second, "note 0 ") || strings.Contains(second, "note 4 ") {
		t.Fatalf("the second dispatch must carry exactly the withheld note:\n%.200s", second)
	}
	if carriedNotes(specs) != 2 {
		t.Fatalf("exactly two dispatches carry notes, got %d", carriedNotes(specs))
	}
	a1 := projectstate.AttemptID("C-Orders", projectstate.AgentTaskFor(projectstate.MethodPhaseRequirements), 1)
	if len(ps.delivered) != 5 {
		t.Fatalf("want five stamps (four, then the withheld one), got %+v", ps.delivered)
	}
	for i, d := range ps.delivered[:4] {
		if d.attemptID != a1 || d.noteID != fmt.Sprintf("C-Orders:note:seed:%d", i+1) {
			t.Fatalf("stamp %d = %+v: the first attempt stamps only notes 1..4", i, d)
		}
	}
	if last := ps.delivered[4]; last.noteID != "C-Orders:note:seed:0" || last.attemptID == a1 {
		t.Fatalf("the withheld note is stamped to the NEXT attempt, got %+v", last)
	}
}

// Test_NoteDelivery_AFailedStampLeavesTheNotePendingAndTheRunGoesOn (M4): the stamp
// cannot land after the submit dispatched the job; the run does not fail, the note stays
// pending, and the next dispatch carries it again (at-least-once) and stamps it there.
func Test_NoteDelivery_AFailedStampLeavesTheNotePendingAndTheRunGoesOn(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
	seededPendingNotes(ps, 1, 10)
	ps.stampConflicts = maxMutateConflictAttempts
	pipe := newFakePipeline()
	registerConstruct(env, newWorkflows(gateDeps(ps)), ps, pipe)
	runNoteConstruct(t, env)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed stamp must not fail the run: %v", err)
	}
	specs := submittedSpecs(pipe)
	if carriedNotes(specs[:2]) != 2 || carriedNotes(specs) != 2 {
		t.Fatalf("the note rides the first dispatch and, unstamped, the next one too; carried=%d", carriedNotes(specs))
	}
	if specs[1].DispatchInputs["phase"] != projectstate.MethodPhaseDetailedDesign.String() {
		t.Fatalf("the fixture's second dispatch is detailed_design, got %q", specs[1].DispatchInputs["phase"])
	}
	a2 := projectstate.AttemptID("C-Orders", projectstate.AgentTaskFor(projectstate.MethodPhaseDetailedDesign), 1)
	if len(ps.delivered) != 1 || ps.delivered[0].attemptID != a2 {
		t.Fatalf("the note is stamped once, to the second dispatch's attempt %s: %+v", a2, ps.delivered)
	}
}

// Test_NoteDelivery_NeverCarriedTwiceIntoTheSameAttempt (M4): a note already carried
// into an attempt is not carried into it again, while another attempt still carries it.
func Test_NoteDelivery_NeverCarriedTwiceIntoTheSameAttempt(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	ps := newFakeProjectStateWithPolicy(projectstate.ReviewPolicy{})
	pipe := newFakePipeline()
	wf := newWorkflows(gateDeps(ps))
	registerConstruct(env, wf, ps, pipe)
	note := projectstate.OperatorNote{NoteID: "C-Orders:note:x:1", Kind: projectstate.NoteRetry, Text: "carry me"}
	env.ExecuteWorkflow(func(ctx workflow.Context) error {
		ctx = workflow.WithActivityOptions(ctx, recordActivityOptions())
		in := constructActivityInput{ProjectID: "p", ActivityID: "C-Orders", Activity: sampleActivity()}
		st := &constructState{pendingNotes: []projectstate.OperatorNote{note}, carriedTo: map[string]string{note.NoteID: "C-Orders:construction:1"}}
		hv := ps.project.Version
		gf := &gitForward{}
		if _, err := wf.submitCarryingNotes(ctx, in, projectstate.MethodPhaseConstruction, st, "C-Orders:construction:1", gf, &hv); err != nil {
			return err
		}
		if _, err := wf.submitCarryingNotes(ctx, in, projectstate.MethodPhaseConstruction, st, "C-Orders:construction:2", gf, &hv); err != nil {
			return err
		}
		return nil
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	specs := submittedSpecs(pipe)
	if len(specs) != 2 {
		t.Fatalf("want two submits, got %d", len(specs))
	}
	if _, ok := specs[0].DispatchInputs[dispatchInputOperatorNote]; ok {
		t.Fatal("a note already carried into attempt 1 must not ride attempt 1 again")
	}
	if !strings.Contains(specs[1].DispatchInputs[dispatchInputOperatorNote], "carry me") {
		t.Fatal("attempt 2 still carries the pending note")
	}
	if len(ps.delivered) != 1 || ps.delivered[0].attemptID != "C-Orders:construction:2" {
		t.Fatalf("stamps = %+v", ps.delivered)
	}
}

// ===========================================================================
// B1.5 — GetPumpStatus (plan B1.5): is the project's one pump running now?
// ===========================================================================

// describeRunning is a RUNNING pump answer whose current run started at start.
func describeRunning(start time.Time) *workflowservice.DescribeWorkflowExecutionResponse {
	return &workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
		Status: enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, StartTime: timestamppb.New(start),
	}}
}

// TestGetPumpStatus_RunningIsOpenWithTheCurrentRunsStart: the pump id is described with
// an EMPTY run id (the latest run, so a cascade reads as open) and RUNNING is open.
func TestGetPumpStatus_RunningIsOpenWithTheCurrentRunsStart(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, pumpWorkflowID(pid), "").Return(describeRunning(start), nil).Once()
	got, err := newTestConstructionManager(mc).GetPumpStatus(testCtx(), pid)
	if err != nil {
		t.Fatalf("GetPumpStatus: %v", err)
	}
	if !got.Open || got.RunStartedAt == nil || !got.RunStartedAt.Equal(start) {
		t.Fatalf("want open with runStartedAt %v, got %+v", start, got)
	}
	mc.AssertExpectations(t)
}

// TestGetPumpStatus_AClosedRunIsNotOpen: every status but RUNNING — a drained, paused,
// failed or terminated pump — is not open, and carries no start.
func TestGetPumpStatus_AClosedRunIsNotOpen(t *testing.T) {
	for _, st := range []enumspb.WorkflowExecutionStatus{
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW,
	} {
		pid := ProjectID(uuid.NewString())
		mc := &temporalmocks.Client{}
		mc.On("DescribeWorkflowExecution", mock.Anything, pumpWorkflowID(pid), "").Return(describeStatus(st), nil)
		got, err := newTestConstructionManager(mc).GetPumpStatus(testCtx(), pid)
		if err != nil || got.Open || got.RunStartedAt != nil {
			t.Errorf("%s: want {open:false}, nil; got %+v, %v", st, got, err)
		}
	}
}

// TestGetPumpStatus_NoPumpIsNotOpenAndNoError: a project whose pump never ran reads
// {open:false} — not an error.
func TestGetPumpStatus_NoPumpIsNotOpenAndNoError(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, pumpWorkflowID(pid), "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), serviceerror.NewNotFound("workflow not found for ID: "+pumpWorkflowID(pid)))
	got, err := newTestConstructionManager(mc).GetPumpStatus(testCtx(), pid)
	if err != nil || got.Open {
		t.Fatalf("want {open:false}, nil; got %+v, %v", got, err)
	}
}

// TestGetPumpStatus_OtherFaultsAreInfrastructure: any other describe fault is
// Infrastructure, never a guessed "closed".
func TestGetPumpStatus_OtherFaultsAreInfrastructure(t *testing.T) {
	pid := ProjectID(uuid.NewString())
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, pumpWorkflowID(pid), "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), serviceerror.NewUnavailable("frontend down"))
	_, err := newTestConstructionManager(mc).GetPumpStatus(testCtx(), pid)
	if got := asConstructionError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure, got %s", got)
	}
}

// TestGetPumpStatus_AHungDescribeIsBounded: the describe is bounded by pumpRPCTimeout,
// so a hung call returns Infrastructure promptly instead of hanging the read.
func TestGetPumpStatus_AHungDescribeIsBounded(t *testing.T) {
	old := pumpRPCTimeout
	pumpRPCTimeout = 50 * time.Millisecond
	t.Cleanup(func() { pumpRPCTimeout = old })
	pid := ProjectID(uuid.NewString())
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, pumpWorkflowID(pid), "").
		Run(func(args mock.Arguments) { <-args.Get(0).(context.Context).Done() }).
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), context.DeadlineExceeded)
	begin := time.Now()
	_, err := newTestConstructionManager(mc).GetPumpStatus(testCtx(), pid)
	if got := asConstructionError(t, err).Kind; got != fwmanager.Infrastructure {
		t.Fatalf("want Infrastructure for a hung describe, got %s", got)
	}
	if waited := time.Since(begin); waited > 2*time.Second {
		t.Fatalf("the describe was not bounded: waited %s", waited)
	}
}

// TestGetPumpStatus_EmptyProjectIsContractMisuse: nothing is described for an empty id.
func TestGetPumpStatus_EmptyProjectIsContractMisuse(t *testing.T) {
	mc := &temporalmocks.Client{}
	_, err := newTestConstructionManager(mc).GetPumpStatus(testCtx(), "")
	if got := asConstructionError(t, err).Kind; got != fwmanager.ContractMisuse {
		t.Fatalf("want ContractMisuse, got %s", got)
	}
	mc.AssertNotCalled(t, "DescribeWorkflowExecution", mock.Anything, mock.Anything, mock.Anything)
}

// ===========================================================================
// B1.7 — RESUME (plan-B1-B2 amendment §B): the recorded pause binds every pump,
// Begin on a paused project is refused, and ResumeProject is the one way back.
// ===========================================================================

// Test_Pump_V2_RecordedPauseBindsAnOperatorDrivenPump: at v2 even a pump started
// operator-driven (a pre-B1.7 caller's input) honours the recorded pause.
func Test_Pump_V2_RecordedPauseBindsAnOperatorDrivenPump(t *testing.T) {
	rig := newCascadingPumpRig(10*time.Minute, 0, recordedPause)
	res, err := rig.runInput(t, pumpInput{ProjectID: rig.pid, OperatorDriven: true})
	if err != nil {
		t.Fatalf("a paused project's pump must go quiet (no ContinueAsNew), got %v", err)
	}
	if res.Dispatched || *rig.childStarts != 0 {
		t.Fatalf("at v2 the recorded pause binds every pump, got %+v with %d child start(s)", res, *rig.childStarts)
	}
}

// Test_Pump_PauseThenResumeThenTheNextTickDispatches is the pause → resume → dispatch
// chain at the pump: with the pause recorded the tick is quiet; once ResumeProject's
// verb has cleared it, the next tick dispatches.
func Test_Pump_PauseThenResumeThenTheNextTickDispatches(t *testing.T) {
	paused := newCascadingPumpRig(10*time.Minute, 0, recordedPause)
	if res, err := paused.run(t); err != nil || res.Dispatched || *paused.childStarts != 0 {
		t.Fatalf("paused: want a quiet tick, got %+v err=%v starts=%d", res, err, *paused.childStarts)
	}
	if _, err := (fakeConstructionTransition{paused.ps}).RecordOperatorResumed(fwra.Context{Context: context.Background()},
		projectstate.ProjectID(paused.pid), paused.ps.project.Version, projectstate.RepoCredential{}, "resume"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	next := newCascadingPumpRig(10*time.Minute, 0, func(p *projectstate.Project) { *p = paused.ps.project })
	if _, err := next.run(t); !isContinueAsNew(err) || *next.childStarts != 1 {
		t.Fatalf("after the resume the next tick must dispatch, got err=%v starts=%d", err, *next.childStarts)
	}
}

// pausedProject is a project in construction whose pause is recorded.
func pausedProject() projectstate.Project {
	return projectstate.Project{ID: "p-res", Phase: projectstate.PhaseConstruction, OperatorPaused: true, PauseReason: "operator halt", Version: 7}
}

// resumeManager wires a façade over mc and a fake store serving proj.
func resumeManager(mc client.Client, proj projectstate.Project) (*constructionManager, *fakeProjectState) {
	ps := &fakeProjectState{project: proj, version: proj.Version}
	return newConstructionManager(mc, fakeFullProjectState{ps}, nil, nil, nil, nil, nil, fakeConstructionTransition{ps}, nil, nil, nil, nil, 0, "", nil), ps
}

func constructionErrorKind(err error) fwmanager.Kind {
	var fe *fwmanager.Error
	if errors.As(err, &fe) {
		return fe.Kind
	}
	return fwmanager.Unknown
}

// TestResumeProject_RefusalsInTheirPinnedOrder: ContractMisuse → NotFound → not in
// construction → not paused → a pause still being applied. Nothing is written, nothing
// is started, nothing is signalled on any refusal.
func TestResumeProject_RefusalsInTheirPinnedOrder(t *testing.T) {
	notConstruction := pausedProject()
	notConstruction.Phase = projectstate.PhaseProjectDesign
	notPaused := pausedProject()
	notPaused.OperatorPaused, notPaused.PauseReason = false, ""
	// Every later refusal is armed in every case (a missing project, a pause in flight),
	// so a refusal that is dropped falls through to the NEXT one — which says something
	// else. The message pins which refusal answered, not just its kind.
	notConstructionNotPaused := notConstruction
	notConstructionNotPaused.OperatorPaused = false
	cases := []struct {
		name     string
		id       ProjectID
		proj     projectstate.Project
		notFound bool
		inFlight bool
		want     fwmanager.Kind
		wantMsg  string
	}{
		{"empty id beats a missing project", "", pausedProject(), true, true, fwmanager.ContractMisuse, "empty projectId"},
		{"no project", "p-res", pausedProject(), true, true, fwmanager.NotFound, ""},
		{"not in construction beats not paused", "p-res", notConstructionNotPaused, false, true, fwmanager.FailedPrecondition, "not in construction"},
		{"not paused beats a pause in flight", "p-res", notPaused, false, true, fwmanager.FailedPrecondition, "not paused"},
		{"a pause still being applied", "p-res", pausedProject(), false, true, fwmanager.FailedPrecondition, "still being applied"},
	}
	for _, c := range cases {
		mc := &temporalmocks.Client{}
		if c.inFlight {
			mc.On("DescribeWorkflowExecution", mock.Anything, "p-res:construction", "").Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING), nil).Maybe()
		}
		m, ps := resumeManager(mc, c.proj)
		ps.notFound = c.notFound
		err := m.ResumeProject(testCtx(), c.id)
		if got := constructionErrorKind(err); got != c.want {
			t.Errorf("%s: want %s, got %v", c.name, c.want, err)
		}
		if c.wantMsg != "" && (err == nil || !strings.Contains(err.Error(), c.wantMsg)) {
			t.Errorf("%s: want the refusal that says %q, got %v", c.name, c.wantMsg, err)
		}
		if ps.resumed != 0 {
			t.Errorf("%s: a refusal must write nothing", c.name)
		}
		mc.AssertNotCalled(t, "ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	}
}

// resumeStartMatcher matches exactly the one start ResumeProject may make: the
// project's one pump id, the one-pump policy pair, and an input with NO OperatorDriven.
func resumeStartMatcher(pid ProjectID) (any, any) {
	return mock.MatchedBy(func(o client.StartWorkflowOptions) bool {
			return o.ID == pumpWorkflowID(pid) &&
				o.WorkflowIDConflictPolicy == enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING &&
				o.WorkflowIDReusePolicy == enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE
		}),
		pumpInput{ProjectID: pid}
}

// TestResumeProject_RecordsOnceThenStartsOrJoinsThePump: exactly one RA write, then one
// start-or-join of the pump (USE_EXISTING + ALLOW_DUPLICATE, not operator-driven), and no
// wait on its decision; the pause is cleared.
func TestResumeProject_RecordsOnceThenStartsOrJoinsThePump(t *testing.T) {
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, "p-res:construction", "").
		Return((*workflowservice.DescribeWorkflowExecutionResponse)(nil), serviceerror.NewNotFound("no supervision run"))
	opts, input := resumeStartMatcher("p-res")
	mc.On("ExecuteWorkflow", mock.Anything, opts, executionKindPump, input).Return(fakePumpRun{id: "p-res:nextActivity", runID: "run-1"}, nil).Once()
	m, ps := resumeManager(mc, pausedProject())
	if err := m.ResumeProject(testCtx(), "p-res"); err != nil {
		t.Fatalf("ResumeProject: %v", err)
	}
	if ps.resumed != 1 || ps.project.OperatorPaused || ps.project.PauseReason != "" {
		t.Fatalf("want one resume write that clears the pause, got resumed=%d paused=%v reason=%q", ps.resumed, ps.project.OperatorPaused, ps.project.PauseReason)
	}
	mc.AssertExpectations(t)
	mc.AssertNotCalled(t, "QueryWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mc.AssertNotCalled(t, "SignalWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestResumeProject_AFailedStartStillResumes: once the record is written the resume has
// landed — the 30s sweep pumps the project — so a failed start is logged, never
// returned.
func TestResumeProject_AFailedStartStillResumes(t *testing.T) {
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, "p-res:construction", "").Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED), nil)
	opts, input := resumeStartMatcher("p-res")
	mc.On("ExecuteWorkflow", mock.Anything, opts, executionKindPump, input).Return(nil, serviceerror.NewUnavailable("frontend down"))
	m, ps := resumeManager(mc, pausedProject())
	if err := m.ResumeProject(testCtx(), "p-res"); err != nil {
		t.Fatalf("a failed pump start after the record must not fail the resume, got %v", err)
	}
	if ps.resumed != 1 {
		t.Fatalf("the resume must be recorded, got %d writes", ps.resumed)
	}
}

// TestResumeProject_ConflictRetryThenGiveUp: a version Conflict is re-read and retried;
// past resumeConflictAttempts it answers FailedPrecondition "changed concurrently".
func TestResumeProject_ConflictRetryThenGiveUp(t *testing.T) {
	for _, c := range []struct {
		conflicts int
		want      fwmanager.Kind
		writes    int
	}{{resumeConflictAttempts - 1, fwmanager.Unknown, 1}, {resumeConflictAttempts, fwmanager.FailedPrecondition, 0}} {
		mc := &temporalmocks.Client{}
		mc.On("DescribeWorkflowExecution", mock.Anything, "p-res:construction", "").Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED), nil)
		opts, input := resumeStartMatcher("p-res")
		mc.On("ExecuteWorkflow", mock.Anything, opts, executionKindPump, input).Return(fakePumpRun{id: "p-res:nextActivity", runID: "run-1"}, nil).Maybe()
		m, ps := resumeManager(mc, pausedProject())
		ps.conflictFirst = c.conflicts
		err := m.ResumeProject(testCtx(), "p-res")
		if got := constructionErrorKind(err); got != c.want {
			t.Errorf("%d conflicts: want kind %s, got %v", c.conflicts, c.want, err)
		}
		if ps.resumed != c.writes {
			t.Errorf("%d conflicts: want %d landed writes, got %d", c.conflicts, c.writes, ps.resumed)
		}
	}
}

// TestResumeProject_ARetryReChecksThePause (I2): the first write conflicts because the
// project moved; the retry re-reads and re-checks everything the first try was allowed
// on. A new pause that landed in between is never cleared: nothing is written after the
// conflict, no pump starts, and the new pause stands.
func TestResumeProject_ARetryReChecksThePause(t *testing.T) {
	cases := []struct {
		name     string
		moved    func(*fakeProjectState)
		inFlight bool
		wantMsg  string
	}{
		{"a new pause landed and is still being applied", func(f *fakeProjectState) { f.project.PauseReason = "second halt" }, true, "still being applied"},
		{"a new pause landed and was relayed", func(f *fakeProjectState) { f.project.PauseReason = "second halt" }, false, "a new pause (second halt) landed"},
		{"the same pause, now being re-applied", func(*fakeProjectState) {}, true, "still being applied"},
		{"someone else resumed", func(f *fakeProjectState) { f.project.OperatorPaused, f.project.PauseReason = false, "" }, false, "not paused"},
		{"the project left construction", func(f *fakeProjectState) { f.project.Phase = projectstate.PhaseProjectDesign }, false, "left construction"},
	}
	for _, c := range cases {
		mc := &temporalmocks.Client{}
		mc.On("DescribeWorkflowExecution", mock.Anything, "p-res:construction", "").Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED), nil).Once()
		second := enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED
		if c.inFlight {
			second = enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING
		}
		mc.On("DescribeWorkflowExecution", mock.Anything, "p-res:construction", "").Return(describeStatus(second), nil).Maybe()
		m, ps := resumeManager(mc, pausedProject())
		ps.conflictFirst = 1
		ps.afterConflict = c.moved
		err := m.ResumeProject(testCtx(), "p-res")
		if got := constructionErrorKind(err); got != fwmanager.FailedPrecondition || !strings.Contains(err.Error(), c.wantMsg) {
			t.Errorf("%s: want FailedPrecondition saying %q, got %v", c.name, c.wantMsg, err)
		}
		if ps.resumed != 0 {
			t.Errorf("%s: nothing may be written after the conflict, got %d resume write(s)", c.name, ps.resumed)
		}
		mc.AssertNotCalled(t, "ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		if c.name != "someone else resumed" && c.name != "the project left construction" && (!ps.project.OperatorPaused || ps.project.PauseReason == "") {
			t.Errorf("%s: the pause must stand, got paused=%v reason=%q", c.name, ps.project.OperatorPaused, ps.project.PauseReason)
		}
	}
}

// TestResumeProject_ARetryOnAnUnrelatedWriteStillResumes: a conflict from a write that
// left the same pause standing and nothing in flight retries and resumes.
func TestResumeProject_ARetryOnAnUnrelatedWriteStillResumes(t *testing.T) {
	mc := &temporalmocks.Client{}
	mc.On("DescribeWorkflowExecution", mock.Anything, "p-res:construction", "").Return(describeStatus(enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED), nil)
	opts, input := resumeStartMatcher("p-res")
	mc.On("ExecuteWorkflow", mock.Anything, opts, executionKindPump, input).Return(fakePumpRun{id: "p-res:nextActivity", runID: "run-1"}, nil).Once()
	m, ps := resumeManager(mc, pausedProject())
	ps.conflictFirst = 1
	if err := m.ResumeProject(testCtx(), "p-res"); err != nil {
		t.Fatalf("an unrelated concurrent write must not stop the resume: %v", err)
	}
	if ps.resumed != 1 || ps.project.OperatorPaused {
		t.Fatalf("want one landed resume, got %d (paused=%v)", ps.resumed, ps.project.OperatorPaused)
	}
	mc.AssertNumberOfCalls(t, "DescribeWorkflowExecution", 2)
}

// TestExecuteNextActivity_PausedProjectIsRefusedBeforeAnyStart: Begin on a paused
// project answers FailedPrecondition naming the reason, and starts nothing (founder
// ruling 2026-09-13).
func TestExecuteNextActivity_PausedProjectIsRefusedBeforeAnyStart(t *testing.T) {
	mc := &temporalmocks.Client{}
	m, _ := resumeManager(mc, pausedProject())
	_, err := m.ExecuteNextActivity(testCtx(), "p-res", "t1")
	if got := constructionErrorKind(err); got != fwmanager.FailedPrecondition {
		t.Fatalf("want FailedPrecondition for Begin on a paused project, got %v", err)
	}
	if !strings.Contains(err.Error(), "paused (operator halt)") || !strings.Contains(err.Error(), "resume it to continue") {
		t.Fatalf("the refusal must name the pause and the way back, got %q", err.Error())
	}
	mc.AssertNotCalled(t, "ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// TestExecuteNextActivity_NoProjectStillStartsThePump: a project that does not exist
// cannot be paused, so Begin proceeds (the pump's own read is the quiet tick).
func TestExecuteNextActivity_NoProjectStillStartsThePump(t *testing.T) {
	mc := &temporalmocks.Client{}
	opts, input := resumeStartMatcher("p-res")
	mc.On("ExecuteWorkflow", mock.Anything, opts, executionKindPump, input).Return(nil, serviceerror.NewUnavailable("frontend down")).Once()
	m, ps := resumeManager(mc, pausedProject())
	ps.notFound = true
	if _, err := m.ExecuteNextActivity(testCtx(), "p-res", "t1"); constructionErrorKind(err) == fwmanager.FailedPrecondition {
		t.Fatalf("a missing project must not read as paused, got %v", err)
	}
	mc.AssertExpectations(t)
}
