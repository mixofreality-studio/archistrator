package operations

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the operationsManager
// impl. It supplies the genWorkerManifest (the workflow set codegen cannot know, the
// per-activity option presets, and the genActivities dep threading), the external
// RegisterManagerWorker entrypoint the composition root calls, and the startup Schedule
// registration.

import (
	"context"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
)

// ---------------------------------------------------------------------------
// Registered workflow names (operationsManager.md §6.2). Stable — the deploy-time
// continuity tokens the façade (operationsmanager.go) starts workflows under.
// ---------------------------------------------------------------------------

const (
	// executionKindDeploy is the operator deploy / scale / policy republish workflow.
	executionKindDeploy = "operationsDeploy"
	// executionKindReconcile is the Schedule-triggered observe+autoscale tick.
	executionKindReconcile = "operationsReconcile"
	// executionKindWithdraw is the terminal withdraw workflow.
	executionKindWithdraw = "operationsWithdraw"
	// executionKindCostProjection is the short-lived read-only cost-projection workflow.
	executionKindCostProjection = "operationsCostProjection"
	// executionKindOperatedSystemView is the short-lived read-only operator-view workflow.
	executionKindOperatedSystemView = "operationsOperatedSystemView"
	// executionKindDelinquency is the queued delinquency-enforcement workflow.
	executionKindDelinquency = "operationsDelinquencyEnforcement"
)

// signalApplyDelinquencyPolicy resumes the delinquency-enforcement branch; backs
// ApplyDelinquencyPolicy (ncuc5). Delivered by settlementManager (signals.go).
const signalApplyDelinquencyPolicy = "applyDelinquencyPolicy"

// Schedule id + cadence (operationsManager.md §6.1; operational-concepts.md §4).
const (
	scheduleIDReconcile = "operations:operatedStateReconcile"

	// reconcileInterval is the reconcile-tick cadence (30s; the single tunable knob).
	reconcileInterval = 30 * time.Second
)

// ---------------------------------------------------------------------------
// Per-activity option presets (operationsManager.md §6.4). Concrete RetryPolicy /
// timeout choices live here, in the Manager, keyed by the generated registered
// activity name; the generated invoker's Opts hook applies them per call. FU-MOP-1
// (named RetryPolicy library) is not yet landed; the inline §6.4 parameters are used.
// ---------------------------------------------------------------------------

// activityOptions returns the option-preset hook the generated invokers consult. A
// name with no entry falls back to the generated default (invokers.gen.go).
func activityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	// readHeadOpts — pure head-state reads (15s; terminal NotFound/ContractMisuse).
	readHeadOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	}
	// recordHeadOpts — head-state write transitions (10s; terminal NotFound/
	// ContractMisuse; Conflict is surfaced for the workflow-level re-read loop, so it
	// is NOT non-retryable here — the workflow body recovers it).
	recordHeadOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.ContractMisuse),
				fwmgr.RAErrType(fwra.Conflict),
			},
		},
	}
	// publishOpts — operatedRuntimeAccess writes (60s; git commit + push; terminal
	// Auth/ContractMisuse). Git-content-idempotent — no version guard.
	publishOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.Auth),
				fwmgr.RAErrType(fwra.ContractMisuse),
			},
		},
	}
	// runtimeReadOpts — operatedRuntimeAccess pure reads (30s; terminal Auth/NotFound).
	runtimeReadOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.Auth),
				fwmgr.RAErrType(fwra.NotFound),
			},
		},
	}
	// artifactReadOpts — artifactAccess read (30s; terminal NotFound/Auth).
	artifactReadOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.NotFound),
				fwmgr.RAErrType(fwra.Auth),
			},
		},
	}
	// usageOpts — usageAccess appends + reads (20s; terminal ContractMisuse/NotFound).
	// Append-only ledger: NO Conflict (dedup-id idempotent).
	usageOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 20 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmgr.RAErrType(fwra.ContractMisuse),
				fwmgr.RAErrType(fwra.NotFound),
			},
		},
	}

	presets := map[string]workflow.ActivityOptions{
		"operatedSystemStateAccess.readOperatedSystem":        readHeadOpts,
		"operatedSystemStateAccess.readInFlightOperatedApps":  readHeadOpts,
		"operatedSystemStateAccess.publishDesiredState":       recordHeadOpts,
		"operatedSystemStateAccess.recordRuntimeStatusChange": recordHeadOpts,
		"operatedSystemStateAccess.withdrawSystem":            recordHeadOpts,
		"operatedSystemStateAccess.recordDelinquencyAction":   recordHeadOpts,
		"operatedRuntimeAccess.publishDesiredState":           publishOpts,
		"operatedRuntimeAccess.withdraw":                      publishOpts,
		"operatedRuntimeAccess.getApplicationHealth":          runtimeReadOpts,
		"operatedRuntimeAccess.getSloStatus":                  runtimeReadOpts,
		"operatedRuntimeAccess.readComputeAttribution":        runtimeReadOpts,
		"artifactAccess.retrieveConstructionOutput":           artifactReadOpts,
		"usageAccess.recordComputeUsage":                      usageOpts,
		"usageAccess.recordFinalUsage":                        usageOpts,
		"usageAccess.readRange":                               usageOpts,
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go)
// consumes: the six workflow bodies under their registered names, no custom
// (hand-written) activities, the per-activity option-preset hook, and the
// genActivities threaded from the impl's stored published deps.
//
// durableExecutionAccess is threaded nil: the Manager never calls its generated
// activities from any workflow (the in-workflow primitives — awaitSignal — are the
// Manager's own code, and the startup Schedule is registered directly via
// RegisterSchedules, not through an Activity). This matches the retired hand code,
// which never wired durableExecution into the Workflows struct.
func (m *operationsManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()
	wf := newWorkflows(wfDeps{
		Intervention: m.intervention,
		Autoscaler:   m.autoscaler,
		Estimation:   m.operationEstimation,
		Acts:         genInvokers{Opts: optsHook},

		// InterventionPolicy is resolved ONCE here from the Manager's raw config
		// (interventionRetryBudget / interventionSLATier, operationsmanager.go) via
		// slaTierFromString (adapters.go) — the SAME fixed value every DecideOnHealth
		// call would have received under the retired per-call adapter conversion.
		InterventionPolicy: intervention.InterventionPolicy{
			RetryBudget: m.interventionRetryBudget,
			SLATier:     slaTierFromString(m.interventionSLATier),
		},
		AutoscalerPolicy:   m.autoscalerPolicy,
		InfrastructureKind: m.infrastructureKind,
		CurrentCycleID:     m.currentCycleID,
		CustomerID:         m.customerID,
	})

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindDeploy, Fn: wf.DeployWorkflow},
			{Name: executionKindReconcile, Fn: wf.ReconcileWorkflow},
			{Name: executionKindWithdraw, Fn: wf.WithdrawWorkflow},
			{Name: executionKindCostProjection, Fn: wf.CostProjectionWorkflow},
			{Name: executionKindOperatedSystemView, Fn: wf.ViewWorkflow},
			{Name: executionKindDelinquency, Fn: wf.DelinquencyEnforcementWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			OperatedSystemState: m.operatedSystemState,
			OperatedRuntime:     m.operatedRuntime,
			Usage:               m.usage,
			Artifact:            m.artifact,
			DurableExecution:    nil,
		},
	}
}

// RegisterManagerWorker wires the operationsManager onto a Temporal Worker polling the
// operations task queue (operationsManager.md §6.1). It preserves the external call
// shape the composition root used before the generated-layer migration, asserting to
// the concrete *operationsManager the generated constructor returns and delegating to
// the generated RegisterWorker with the impl's WorkerManifest.
func RegisterManagerWorker(w worker.Worker, m OperationsManager) {
	impl, ok := m.(*operationsManager)
	if !ok {
		panic("operations: RegisterManagerWorker requires a *operationsManager from NewOperationsManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}

// RegisterSchedules registers (idempotently) the operatedStateReconcile (30s) Temporal
// Schedule at startup via durableExecutionAccess (operationsManager.md §6.1; C-MOP-3).
// Called once at process start. The cadence is the single tunable knob.
func RegisterSchedules(ctx context.Context, durable durableexecution.DurableExecutionAccess) error {
	return durableAdapter{inner: durable}.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDReconcile,
		WorkflowType: executionKindReconcile,
		TaskQueue:    TaskQueue,
		IntervalSecs: int(reconcileInterval / time.Second),
	})
}
