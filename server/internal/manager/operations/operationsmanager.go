package operations

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usagelog"
)

// OperationsManager is the operationsManager port — the public use-case surface of
// the façade (operationsManager.md §2 + operationsRead-ruling.md). Each op leads with
// the Manager-layer call Context (fwmgr.Context, embedding context.Context + the
// Principal); the *Manager derives ctx := rc.Context inside. The *ReconcileScope /
// *ScaleWhatIfPoints pointer params are load-bearing (nil ⇒ all-apps / run-rate-only).
//
// SCHEMA-FIRST: this interface (and the port I/O types) are GENERATED into
// contract.gen.go from contract.schema.json (edit the schema + `make gen`; do NOT
// hand-edit the generated surface). The concrete *operationsManager below satisfies it;
// the consumer-side dependency seams (deps.go) and the Temporal Workflows struct stay
// hand-written and are NOT part of this contract.

// Compile-time proof the concrete operationsManager satisfies the generated
// OperationsManager port. Each op leads with the Manager-layer call Context
// (fwmgr.Context); the *operationsManager derives ctx := rc.Context inside.
var _ OperationsManager = (*operationsManager)(nil)

// operationsManager is the operationsManager façade. It exposes the five public
// use-case ops (operationsManager.md §2) and OWNS Temporal. The Temporal-backed ops:
//   - DeployAfterConstruction — Workflow (entry; operator deploy / scale / policy)
//   - ReconcileOperatedState  — Workflow (entry; Schedule-triggered observe+autoscale)
//   - WithdrawSystem          — Workflow (entry; terminal withdraw)
//   - QueryCostProjection     — Workflow (entry; short-lived read-only)
//   - ApplyDelinquencyPolicy  — Signal (queued, cross-Manager; signal-with-start)
//
// The façade methods use only the Temporal client; the pre-condition checks (non-empty
// ids, the reason-discriminator rejection) are enforced here before any Temporal call
// (operationsManager.md §2/§3.4). It ALSO stores the PUBLISHED downstream deps the
// GENERATED constructor (contract.gen.go: NewOperationsManager) was given so
// RegisterWorker (worker.go) can fold them (adapters.go) into the hand-written Temporal
// Workflows. Per the founder DI model (2026-06-28) the former exported consumer-mirror
// interfaces + the composition-root adapters are RETIRED; the Manager depends on the
// deps' PUBLISHED interfaces and adapts them internally.
type operationsManager struct {
	client client.Client

	operatedSystemState operatedsystemstate.OperatedSystemStateAccess
	operatedRuntime     operatedruntime.OperatedRuntimeAccess
	usage               usagelog.UsageAccess
	artifact            artifact.ArtifactAccess
	durableExecution    durableexecution.DurableExecutionAccess
	intervention        intervention.InterventionEngine
	autoscaler          autoscaler.AutoscalerEngine
	operationEstimation operationestimation.OperationEstimationEngine

	// Policy/config snapshots folded into the Workflows struct by RegisterWorker. They
	// are construction-time seam defaults (in production the Manager reads them from
	// head-state / the operated app's billing context). InfrastructureKind defaults to
	// the launch infrastructure; the rest are zero (matching what the composition root
	// passes today — the operations Worker carries no policy config yet).
	interventionPolicy interventionPolicy
	autoscalerPolicy   autoscalerPolicy
	infrastructureKind infrastructureKind
	currentCycleID     string
	customerID         customerID
}

// newOperationsManager is the hand-written, unexported builder the generated
// NewOperationsManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only the client; the deps are
// stored for RegisterWorker (worker.go), which folds them into the Temporal Workflows.
func newOperationsManager(
	c client.Client,
	operatedSystemState operatedsystemstate.OperatedSystemStateAccess,
	operatedRuntime operatedruntime.OperatedRuntimeAccess,
	usage usagelog.UsageAccess,
	art artifact.ArtifactAccess,
	durableExecution durableexecution.DurableExecutionAccess,
	interventionEng intervention.InterventionEngine,
	autoscalerEng autoscaler.AutoscalerEngine,
	operationEstimation operationestimation.OperationEstimationEngine,
) *operationsManager {
	return &operationsManager{
		client:              c,
		operatedSystemState: operatedSystemState,
		operatedRuntime:     operatedRuntime,
		usage:               usage,
		artifact:            art,
		durableExecution:    durableExecution,
		intervention:        interventionEng,
		autoscaler:          autoscalerEng,
		operationEstimation: operationEstimation,
		infrastructureKind:  infrastructureKindGoTemporalPostgres,
	}
}

// DeployAfterConstruction — op 2.1. Temporal Workflow (entry; StartWorkflow, id
// {operatedAppId}:deploy:{changeId}). Reads head-state → (first deploy) retrieves the
// deployable bundle → publishes desired state (git commit) → records the head-state
// transition. Idempotent on the id (a redundant deploy returns the existing result).
//
// REASON DISCRIMINATOR (operationsManager.md §2.6/§3.4/OQ-5): this op accepts only
// Reason ∈ {ReasonDeployAfterConstruction, ReasonOperator}; ReasonAutoscale (reserved
// for 2.2) and ReasonDelinquency (reserved for 2.5) are rejected with a ContractMisuse
// — the compile-time/façade guard against operator-deploy and platform-automatic
// republishes colliding on the shared PublishDesiredStateActivity.
//
// SYNC from the Client's POV: returns once the desired state is durably published,
// NOT once ArgoCD has converged (convergence is observed later via 2.2).
func (m *operationsManager) DeployAfterConstruction(rc fwmgr.Context, operatedAppID operatedAppID, change DesiredStateChange) (DeployResult, error) {
	ctx := rc.Context
	if operatedAppID == uuid.Nil {
		return DeployResult{}, newError(fwmgr.ContractMisuse, "empty operatedAppId")
	}
	if change.ChangeID == "" {
		return DeployResult{}, newError(fwmgr.ContractMisuse, "empty changeId")
	}
	switch change.Reason {
	case ReasonDeployAfterConstruction, ReasonOperator:
		// ok — operator-driven republish.
	case ReasonAutoscale, ReasonDelinquency:
		return DeployResult{}, newError(fwmgr.ContractMisuse,
			fmt.Sprintf("reason %q is reserved for internal republish (reconcile/delinquency) and is rejected on deployAfterConstruction", desiredStateReasonName(change.Reason)))
	default:
		return DeployResult{}, newError(fwmgr.ContractMisuse,
			fmt.Sprintf("unknown desired-state reason %d", int(change.Reason)))
	}

	wfID := deployWorkflowID(operatedAppID, change.ChangeID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindDeploy, deployInput{
		OperatedAppID: operatedAppID,
		Change:        change,
	})
	if err != nil {
		return DeployResult{}, mapStartError(err)
	}
	var result DeployResult
	if err := we.Get(ctx, &result); err != nil {
		return DeployResult{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// ReconcileOperatedState — op 2.2. Temporal Workflow (entry; Schedule-triggered, id
// operatedStateReconcile:{tickId}). Runs Path B (observe) + Path C (autoscale) in one
// execution per firing. Not invoked directly by a human caller — fired by
// schedulerClient via the operatedStateReconcile Schedule. SYNC within the firing:
// returns once the tick's observations + any republishes are durably recorded.
func (m *operationsManager) ReconcileOperatedState(rc fwmgr.Context, tickID string, scope *ReconcileScope) (ReconcileResult, error) {
	ctx := rc.Context
	if tickID == "" {
		return ReconcileResult{}, newError(fwmgr.ContractMisuse, "empty tickId")
	}
	in := reconcileInput{}
	if scope != nil {
		in.Scope = scope.AppIDs
	}

	wfID := reconcileWorkflowID(tickID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindReconcile, in)
	if err != nil {
		return ReconcileResult{}, mapStartError(err)
	}
	var result ReconcileResult
	if err := we.Get(ctx, &result); err != nil {
		return ReconcileResult{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// WithdrawSystem — op 2.3. Temporal Workflow (entry; StartWorkflow, id
// {operatedAppId}:withdraw:{changeId}). Withdraws the runtime → records final usage →
// withdraws the head-state. Idempotent on the id; an already-withdrawn app is a no-op
// success. SYNC: returns once the withdrawal is durably recorded.
func (m *operationsManager) WithdrawSystem(rc fwmgr.Context, operatedAppID operatedAppID, changeID string, reason WithdrawReason) (WithdrawResult, error) {
	ctx := rc.Context
	if operatedAppID == uuid.Nil {
		return WithdrawResult{}, newError(fwmgr.ContractMisuse, "empty operatedAppId")
	}
	if changeID == "" {
		return WithdrawResult{}, newError(fwmgr.ContractMisuse, "empty changeId")
	}

	wfID := withdrawWorkflowID(operatedAppID, changeID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindWithdraw, withdrawInput{
		OperatedAppID: operatedAppID,
		Reason:        reason,
	})
	if err != nil {
		return WithdrawResult{}, mapStartError(err)
	}
	var result WithdrawResult
	if err := we.Get(ctx, &result); err != nil {
		return WithdrawResult{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// QueryCostProjection — op 2.4. Temporal Workflow (entry; short-lived read-only, id
// {operatedAppId}:costProjection:{requestId}). Reads observed usage + recent
// desired-state history → runs operationEstimationEngine.ProjectForOperatedApp
// (direct in-workflow). MUTATES NO STATE. SYNC, side-effect-free.
func (m *operationsManager) QueryCostProjection(rc fwmgr.Context, operatedAppID operatedAppID, requestID string, points *ScaleWhatIfPoints) (costProjection, error) {
	ctx := rc.Context
	if operatedAppID == uuid.Nil {
		return costProjection{}, newError(fwmgr.ContractMisuse, "empty operatedAppId")
	}
	if requestID == "" {
		return costProjection{}, newError(fwmgr.ContractMisuse, "empty requestId")
	}
	in := costProjectionInput{OperatedAppID: operatedAppID}
	if points != nil {
		in.ScaleWhatIfPoints = points.Points
	}

	wfID := costProjectionWorkflowID(operatedAppID, requestID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindCostProjection, in)
	if err != nil {
		return costProjection{}, mapStartError(err)
	}
	var result costProjection
	if err := we.Get(ctx, &result); err != nil {
		return costProjection{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// QueryOperatedSystemView — op 2.7 (read). Short-lived read-only Temporal Workflow
// (StartWorkflow, id {operatedAppId}:view:{requestId}). Composes the operator display
// view: head-state (ReadOperatedSystemActivity → phase + inFlight) + observed health/SLO
// (GetApplicationHealthActivity / GetSloStatusActivity) + autoscaler mode/decision history
// + current run-rate (operationEstimationEngine, run-rate only — nil what-if). MUTATES
// NO STATE. SYNC, side-effect-free. Mirrors QueryCostProjection (§2.4) in shape
// (operationsRead-ruling.md §A).
func (m *operationsManager) QueryOperatedSystemView(rc fwmgr.Context, operatedAppID operatedAppID, requestID string) (OperatedSystemView, error) {
	ctx := rc.Context
	if operatedAppID == uuid.Nil {
		return OperatedSystemView{}, newError(fwmgr.ContractMisuse, "empty operatedAppId")
	}
	if requestID == "" {
		return OperatedSystemView{}, newError(fwmgr.ContractMisuse, "empty requestId")
	}

	wfID := viewWorkflowID(operatedAppID, requestID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindOperatedSystemView, viewInput{
		OperatedAppID: operatedAppID,
	})
	if err != nil {
		return OperatedSystemView{}, mapStartError(err)
	}
	var result OperatedSystemView
	if err := we.Get(ctx, &result); err != nil {
		return OperatedSystemView{}, newError(fwmgr.Infrastructure, err.Error())
	}
	return result, nil
}

// ApplyDelinquencyPolicy — op 2.5. Temporal Signal (applyDelinquencyPolicy, queued,
// cross-Manager). Delivered by settlementManager. The Manager resumes (or starts +
// resumes via signal-with-start) the delinquency-enforcement workflow for the
// customer, which reads the customer's in-flight apps and publishes a
// pause-or-withdraw patch per BillingTerms. QUEUED/async: returns once the signal is
// durably enqueued; the enforcement runs in the workflow. Late/duplicate delivery is
// idempotent (signal-with-start re-derivation).
func (m *operationsManager) ApplyDelinquencyPolicy(rc fwmgr.Context, customerID customerID, delinquencyContext DelinquencyContext) error {
	ctx := rc.Context
	if customerID == uuid.Nil {
		return newError(fwmgr.ContractMisuse, "empty customerId")
	}

	wfID := delinquencyWorkflowID(customerID)
	sig := applyDelinquencySignal{CustomerID: customerID, Context: delinquencyContext}
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, signalApplyDelinquencyPolicy, sig,
		opts, executionKindDelinquency, delinquencyInput{CustomerID: customerID})
	if err != nil {
		return mapSignalError(err)
	}
	return nil
}

// --- workflow id derivation (continuity tokens; operationsManager.md §6.1) ----

// deployWorkflowID derives {operatedAppId}:deploy:{changeId}.
func deployWorkflowID(operatedAppID operatedAppID, changeID string) string {
	return fmt.Sprintf("%s:deploy:%s", operatedAppID, changeID)
}

// reconcileWorkflowID derives operatedStateReconcile:{tickId} (schedule firing id =
// workflow id, Temporal-native firing idempotency).
func reconcileWorkflowID(tickID string) string {
	return fmt.Sprintf("operatedStateReconcile:%s", tickID)
}

// withdrawWorkflowID derives {operatedAppId}:withdraw:{changeId}.
func withdrawWorkflowID(operatedAppID operatedAppID, changeID string) string {
	return fmt.Sprintf("%s:withdraw:%s", operatedAppID, changeID)
}

// costProjectionWorkflowID derives {operatedAppId}:costProjection:{requestId}.
func costProjectionWorkflowID(operatedAppID operatedAppID, requestID string) string {
	return fmt.Sprintf("%s:costProjection:%s", operatedAppID, requestID)
}

// viewWorkflowID derives {operatedAppId}:view:{requestId} (the short-lived read-only
// operator-view continuity token; operationsRead-ruling.md §A).
func viewWorkflowID(operatedAppID operatedAppID, requestID string) string {
	return fmt.Sprintf("%s:view:%s", operatedAppID, requestID)
}

// delinquencyWorkflowID derives the customer's delinquency-enforcement workflow id
// {customerId}:delinquency (the signal-with-start continuity token).
func delinquencyWorkflowID(customerID customerID) string {
	return fmt.Sprintf("%s:delinquency", customerID)
}

// --- error mapping at the façade boundary (operationsManager.md §3.4) ---------

func mapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; any other
	// error is treated as an infrastructure fault at the transport layer.
	return newError(fwmgr.Infrastructure, err.Error())
}

func mapSignalError(err error) error {
	if isNotFound(err) {
		return newError(fwmgr.NotFound, err.Error())
	}
	return newError(fwmgr.Infrastructure, err.Error())
}

// isNotFound reports whether the Temporal error indicates the addressed execution
// does not exist (mirrors construction's matcher).
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errNotFoundSentinel) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "NotFound")
}

var errNotFoundSentinel = errors.New("not found")
