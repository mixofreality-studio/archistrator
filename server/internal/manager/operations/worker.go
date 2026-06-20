package operations

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (operationsManager.md §6.1/§6.2).
// ---------------------------------------------------------------------------

// TaskQueue is the one queue per Manager that the in-process Temporal Worker in the
// server polls (operationsManager.md §6.1; operational-concepts.md §6 — "one Worker
// per task queue: ... operations ...").
const TaskQueue = "operations"

// Signal names (operationsManager.md §6.1/§6.2). The delinquency Signal is the one
// inbound queued cross-Manager edge (settlementManager → operationsManager).
const (
	// SignalApplyDelinquencyPolicy resumes the delinquency-enforcement branch; backs
	// ApplyDelinquencyPolicy (ncuc5). Delivered by settlementManager.
	SignalApplyDelinquencyPolicy = "applyDelinquencyPolicy"
)

// ExecutionKinds — the registered workflow names (operationsManager.md §6.2).
const (
	// ExecutionKindDeploy is the operator deploy / scale / policy republish workflow.
	ExecutionKindDeploy = "operationsDeploy"
	// ExecutionKindReconcile is the Schedule-triggered observe+autoscale tick.
	ExecutionKindReconcile = "operationsReconcile"
	// ExecutionKindWithdraw is the terminal withdraw workflow.
	ExecutionKindWithdraw = "operationsWithdraw"
	// ExecutionKindCostProjection is the short-lived read-only cost-projection workflow.
	ExecutionKindCostProjection = "operationsCostProjection"
	// ExecutionKindOperatedSystemView is the short-lived read-only operator-view workflow
	// (operationsRead-ruling.md §A — composes the existing reads, no mutation).
	ExecutionKindOperatedSystemView = "operationsOperatedSystemView"
	// ExecutionKindDelinquency is the queued delinquency-enforcement workflow (resumed
	// by the applyDelinquencyPolicy Signal).
	ExecutionKindDelinquency = "operationsDelinquencyEnforcement"
)

// Schedule id + cadence (operationsManager.md §6.1; operational-concepts.md §4).
const (
	scheduleIDReconcile = "operations:operatedStateReconcile"

	// reconcileInterval is the reconcile-tick cadence (30s; the single tunable knob).
	reconcileInterval = 30 * time.Second
)

// Activity name constants. The Activity methods are registered under these stable
// names and the workflow bodies invoke them by the method value on wf, so the
// registered name and the call stay in lockstep (operationsManager.md §6.4 — 15
// Activities, one per RA call).
const (
	// operatedSystemStateAccess (head-state) Activities.
	actReadOperatedSystem        = "ReadOperatedSystemActivity"
	actReadInFlightOperatedApps  = "ReadInFlightOperatedAppsActivity"
	actRecordPublishDesiredState = "RecordPublishDesiredStateActivity"
	actRecordRuntimeStatusChange = "RecordRuntimeStatusChangeActivity"
	actWithdrawHeadState         = "WithdrawHeadStateActivity"
	actRecordDelinquencyAction   = "RecordDelinquencyActionActivity"

	// operatedRuntimeAccess (GitOps/cluster) Activities.
	actPublishDesiredState    = "PublishDesiredStateActivity"
	actWithdrawRuntime        = "WithdrawRuntimeActivity"
	actGetApplicationHealth   = "GetApplicationHealthActivity"
	actGetSloStatus           = "GetSloStatusActivity"
	actReadComputeAttribution = "ReadComputeAttributionActivity"

	// artifactAccess Activity.
	actRetrieveDeployableBundle = "RetrieveDeployableBundleActivity"

	// usageAccess (append-only ledger) Activities.
	actRecordComputeUsage = "RecordComputeUsageActivity"
	actRecordFinalUsage   = "RecordFinalUsageActivity"
	actReadUsageRange     = "ReadUsageRangeActivity"
)

// RegisterWorker wires the operationsManager onto a Temporal Worker polling the
// operations task queue (operationsManager.md §6.1). The Manager's workflow
// dependencies — the three Engines (interventionEngine, autoscalerEngine,
// operationEstimationEngine, called DIRECTLY in-workflow) and the ResourceAccess
// ports (operatedSystemStateAccess, operatedRuntimeAccess, usageAccess,
// artifactAccess) — live on a single Workflows struct (there is no separate
// Activities type).
//
// The three Engines' verbs are called DIRECTLY from workflow code (deterministic, by
// value) and are NOT Activities. The durableExecutionAccess in-workflow primitives
// (awaitSignal / startTimer) are the Manager's own code (category A) and are NOT
// Activities either.
func RegisterWorker(w worker.Worker, deps Deps) {
	wf := newWorkflows(deps)

	w.RegisterWorkflowWithOptions(wf.DeployWorkflow, workflow.RegisterOptions{Name: ExecutionKindDeploy})
	w.RegisterWorkflowWithOptions(wf.ReconcileWorkflow, workflow.RegisterOptions{Name: ExecutionKindReconcile})
	w.RegisterWorkflowWithOptions(wf.WithdrawWorkflow, workflow.RegisterOptions{Name: ExecutionKindWithdraw})
	w.RegisterWorkflowWithOptions(wf.CostProjectionWorkflow, workflow.RegisterOptions{Name: ExecutionKindCostProjection})
	w.RegisterWorkflowWithOptions(wf.ViewWorkflow, workflow.RegisterOptions{Name: ExecutionKindOperatedSystemView})
	w.RegisterWorkflowWithOptions(wf.DelinquencyEnforcementWorkflow, workflow.RegisterOptions{Name: ExecutionKindDelinquency})

	// The 15 Activities (operationsManager.md §6.4), one per RA call.
	w.RegisterActivityWithOptions(wf.ReadOperatedSystemActivity, activity.RegisterOptions{Name: actReadOperatedSystem})
	w.RegisterActivityWithOptions(wf.ReadInFlightOperatedAppsActivity, activity.RegisterOptions{Name: actReadInFlightOperatedApps})
	w.RegisterActivityWithOptions(wf.RecordPublishDesiredStateActivity, activity.RegisterOptions{Name: actRecordPublishDesiredState})
	w.RegisterActivityWithOptions(wf.RecordRuntimeStatusChangeActivity, activity.RegisterOptions{Name: actRecordRuntimeStatusChange})
	w.RegisterActivityWithOptions(wf.WithdrawHeadStateActivity, activity.RegisterOptions{Name: actWithdrawHeadState})
	w.RegisterActivityWithOptions(wf.RecordDelinquencyActionActivity, activity.RegisterOptions{Name: actRecordDelinquencyAction})

	w.RegisterActivityWithOptions(wf.PublishDesiredStateActivity, activity.RegisterOptions{Name: actPublishDesiredState})
	w.RegisterActivityWithOptions(wf.WithdrawRuntimeActivity, activity.RegisterOptions{Name: actWithdrawRuntime})
	w.RegisterActivityWithOptions(wf.GetApplicationHealthActivity, activity.RegisterOptions{Name: actGetApplicationHealth})
	w.RegisterActivityWithOptions(wf.GetSloStatusActivity, activity.RegisterOptions{Name: actGetSloStatus})
	w.RegisterActivityWithOptions(wf.ReadComputeAttributionActivity, activity.RegisterOptions{Name: actReadComputeAttribution})

	w.RegisterActivityWithOptions(wf.RetrieveDeployableBundleActivity, activity.RegisterOptions{Name: actRetrieveDeployableBundle})

	w.RegisterActivityWithOptions(wf.RecordComputeUsageActivity, activity.RegisterOptions{Name: actRecordComputeUsage})
	w.RegisterActivityWithOptions(wf.RecordFinalUsageActivity, activity.RegisterOptions{Name: actRecordFinalUsage})
	w.RegisterActivityWithOptions(wf.ReadUsageRangeActivity, activity.RegisterOptions{Name: actReadUsageRange})
}

// RegisterSchedules registers (idempotently) the operatedStateReconcile (30s)
// Temporal Schedule at startup via durableExecutionAccess (operationsManager.md
// §6.1; C-MOP-3). Called once at process start. The cadence is the single tunable
// knob.
func RegisterSchedules(ctx context.Context, durable DurableExecutionAccess) error {
	return durable.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDReconcile,
		WorkflowType: ExecutionKindReconcile,
		TaskQueue:    TaskQueue,
		IntervalSecs: int(reconcileInterval / time.Second),
	})
}
