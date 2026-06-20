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
	"errors"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

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
	r := NewRuntime(nil, testTable()) // nil client: a pre-condition failure must NOT touch it
	_, err := r.StartOrSignalExecution(t.Context(), "systemDesignPhase1", "", "", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestStartOrSignal_UnknownKind_ContractMisuse_NoRuntime(t *testing.T) {
	r := NewRuntime(nil, testTable()) // nil client proves the unknown-kind check is local
	_, err := r.StartOrSignalExecution(t.Context(), "noSuchKind", "proj:phase1", "", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U3, U4: DeliverSignal pre-conditions ----

func TestDeliverSignal_EmptyTarget_ContractMisuse(t *testing.T) {
	r := NewRuntime(nil, testTable())
	err := r.DeliverSignal(t.Context(), "", "applyDelinquencyPolicy", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestDeliverSignal_EmptySignal_ContractMisuse(t *testing.T) {
	r := NewRuntime(nil, testTable())
	err := r.DeliverSignal(t.Context(), "operations:reconcile", "", ExecutionPayload{})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U5, U6: RegisterSchedule pre-conditions ----

func TestRegisterSchedule_EmptyID_ContractMisuse(t *testing.T) {
	r := NewRuntime(nil, testTable())
	err := r.RegisterSchedule(t.Context(), "", ScheduleSpec{ExecutionKind: "settlementCycle", Cadence: Cadence{Every: time.Hour}})
	assertKind(t, err, fwra.ContractMisuse)
}

func TestRegisterSchedule_UnknownKind_ContractMisuse_NoRuntime(t *testing.T) {
	r := NewRuntime(nil, testTable())
	err := r.RegisterSchedule(t.Context(), "shortfallSweep", ScheduleSpec{ExecutionKind: "noSuchKind", Cadence: Cadence{Every: time.Hour}})
	assertKind(t, err, fwra.ContractMisuse)
}

// ---- U7: QueryExecutionState pre-condition ----

func TestQueryExecutionState_EmptyID_ContractMisuse(t *testing.T) {
	r := NewRuntime(nil, testTable())
	_, err := r.QueryExecutionState(t.Context(), "", "costProjection", ExecutionPayload{})
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
	a := ExecutionHandle{opaque: handleString("proj:phase1", "run-1")}
	b := ExecutionHandle{opaque: handleString("proj:phase1", "run-1")}
	c := ExecutionHandle{opaque: handleString("proj:phase1", "run-2")}

	if !a.Equal(b) {
		t.Errorf("expected equal handles a==b")
	}
	if a.Equal(c) {
		t.Errorf("expected unequal handles a!=c")
	}
	if a.String() != "proj:phase1|run-1" {
		t.Errorf("unexpected String(): %q", a.String())
	}
	if a.IsZero() {
		t.Errorf("non-empty handle reported IsZero")
	}
	if !(ExecutionHandle{}).IsZero() {
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
