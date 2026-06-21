package operations

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
)

// Manager is the operationsManager façade. It exposes the five public use-case ops
// (operationsManager.md §2) and OWNS Temporal. The Temporal-backed ops:
//   - DeployAfterConstruction — Workflow (entry; operator deploy / scale / policy)
//   - ReconcileOperatedState  — Workflow (entry; Schedule-triggered observe+autoscale)
//   - WithdrawSystem          — Workflow (entry; terminal withdraw)
//   - QueryCostProjection     — Workflow (entry; short-lived read-only)
//   - ApplyDelinquencyPolicy  — Signal (queued, cross-Manager; signal-with-start)
//
// The Manager holds only a Temporal client; the façade pre-condition checks
// (non-empty ids, the reason-discriminator rejection) are enforced here before any
// Temporal call (operationsManager.md §2/§3.4). The downstream Engines/RA are held
// on the Workflows struct (workflow.go), reached only from workflow code.
type Manager struct {
	client client.Client
}

// NewManager constructs a Manager over an existing Temporal client.
func NewManager(c client.Client) *Manager {
	return &Manager{client: c}
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
func (m *Manager) DeployAfterConstruction(ctx context.Context, operatedAppID OperatedAppID, change DesiredStateChange) (DeployResult, error) {
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
			fmt.Sprintf("reason %q is reserved for internal republish (reconcile/delinquency) and is rejected on deployAfterConstruction", change.Reason))
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
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindDeploy, DeployInput{
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
func (m *Manager) ReconcileOperatedState(ctx context.Context, tickID string, scope *ReconcileScope) (ReconcileResult, error) {
	if tickID == "" {
		return ReconcileResult{}, newError(fwmgr.ContractMisuse, "empty tickId")
	}
	in := ReconcileInput{}
	if scope != nil {
		in.Scope = scope.AppIDs
	}

	wfID := reconcileWorkflowID(tickID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindReconcile, in)
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
func (m *Manager) WithdrawSystem(ctx context.Context, operatedAppID OperatedAppID, changeID string, reason WithdrawReason) (WithdrawResult, error) {
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
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindWithdraw, WithdrawInput{
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
func (m *Manager) QueryCostProjection(ctx context.Context, operatedAppID OperatedAppID, requestID string, points *ScaleWhatIfPoints) (CostProjection, error) {
	if operatedAppID == uuid.Nil {
		return CostProjection{}, newError(fwmgr.ContractMisuse, "empty operatedAppId")
	}
	if requestID == "" {
		return CostProjection{}, newError(fwmgr.ContractMisuse, "empty requestId")
	}
	in := CostProjectionInput{OperatedAppID: operatedAppID}
	if points != nil {
		in.ScaleWhatIfPoints = points.Points
	}

	wfID := costProjectionWorkflowID(operatedAppID, requestID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindCostProjection, in)
	if err != nil {
		return CostProjection{}, mapStartError(err)
	}
	var result CostProjection
	if err := we.Get(ctx, &result); err != nil {
		return CostProjection{}, newError(fwmgr.Infrastructure, err.Error())
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
func (m *Manager) QueryOperatedSystemView(ctx context.Context, operatedAppID OperatedAppID, requestID string) (OperatedSystemView, error) {
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
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindOperatedSystemView, ViewInput{
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
func (m *Manager) ApplyDelinquencyPolicy(ctx context.Context, customerID CustomerID, delinquencyContext DelinquencyContext) error {
	if customerID == uuid.Nil {
		return newError(fwmgr.ContractMisuse, "empty customerId")
	}

	wfID := delinquencyWorkflowID(customerID)
	sig := ApplyDelinquencySignal{CustomerID: customerID, Context: delinquencyContext}
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, SignalApplyDelinquencyPolicy, sig,
		opts, ExecutionKindDelinquency, DelinquencyInput{CustomerID: customerID})
	if err != nil {
		return mapSignalError(err)
	}
	return nil
}

// --- workflow id derivation (continuity tokens; operationsManager.md §6.1) ----

// deployWorkflowID derives {operatedAppId}:deploy:{changeId}.
func deployWorkflowID(operatedAppID OperatedAppID, changeID string) string {
	return fmt.Sprintf("%s:deploy:%s", operatedAppID, changeID)
}

// reconcileWorkflowID derives operatedStateReconcile:{tickId} (schedule firing id =
// workflow id, Temporal-native firing idempotency).
func reconcileWorkflowID(tickID string) string {
	return fmt.Sprintf("operatedStateReconcile:%s", tickID)
}

// withdrawWorkflowID derives {operatedAppId}:withdraw:{changeId}.
func withdrawWorkflowID(operatedAppID OperatedAppID, changeID string) string {
	return fmt.Sprintf("%s:withdraw:%s", operatedAppID, changeID)
}

// costProjectionWorkflowID derives {operatedAppId}:costProjection:{requestId}.
func costProjectionWorkflowID(operatedAppID OperatedAppID, requestID string) string {
	return fmt.Sprintf("%s:costProjection:%s", operatedAppID, requestID)
}

// viewWorkflowID derives {operatedAppId}:view:{requestId} (the short-lived read-only
// operator-view continuity token; operationsRead-ruling.md §A).
func viewWorkflowID(operatedAppID OperatedAppID, requestID string) string {
	return fmt.Sprintf("%s:view:%s", operatedAppID, requestID)
}

// delinquencyWorkflowID derives the customer's delinquency-enforcement workflow id
// {customerId}:delinquency (the signal-with-start continuity token).
func delinquencyWorkflowID(customerID CustomerID) string {
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
