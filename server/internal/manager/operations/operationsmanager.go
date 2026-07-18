// Package operations is the operationsManager component of the archistrator
// server's Manager layer — the use-case façade that drives a delivered system
// through its operational life (UC4 "Operate a delivered system"), per the
// senior-frozen contract
// designs/aiarch/implementation/contracts/operationsManager.md (C-MOP).
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Schedule), it registers the operatedStateReconcile
// (30s) Schedule at startup, defines one Activity per ResourceAccess call, owns the
// Signal handler + the in-workflow primitives (awaitSignal / startTimer /
// registerSchedule), and derives the idempotency key "${workflowId}:${activityId}"
// passed down to each head-state RA write. Temporal lives ONLY in this component;
// the downstream Engines (interventionEngine, autoscalerEngine,
// operationEstimationEngine — pure, in-workflow, by value) and ResourceAccess ports
// (operatedSystemStateAccess, operatedRuntimeAccess, usageAccess, artifactAccess,
// durableExecutionAccess) import no Temporal.
//
// The FIVE frozen public ops (operationsManager.md §2):
//   - DeployAfterConstruction — Workflow (entry; operator deploy / scale / policy)
//   - ReconcileOperatedState  — Workflow (entry; Schedule-triggered observe+autoscale)
//   - WithdrawSystem          — Workflow (entry; terminal withdraw)
//   - QueryCostProjection     — Workflow (entry; short-lived read-only, no mutation)
//   - ApplyDelinquencyPolicy  — Signal (queued, cross-Manager from settlementManager)
//
// File layout (mirrors internal/manager/construction):
//   - operationsmanager.go : the Manager that translates public ops into Temporal client calls (§6.2)
//   - contract.go          : the public façade types (§3)
//   - deps.go              : the consumer-side dep interfaces + frozen-collaborator seams (§5)
//   - workflow.go          : the Workflows deps struct + workflow bodies + the Conflict loop (§6.3, §6.5)
//   - activities.go        : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - signals.go           : the queued delinquency Signal payload + enforcement branch (§6.3)
//   - errors.go            : the port-error -> Temporal-error mapping helper (§6.4)
//   - worker.go            : worker registration of workflows + activities + the Schedule (§6.1)
package operations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fwmgr "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/autoscaler"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedruntime"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/operatedsystemstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/usage"
)

// OperationsManager is the operationsManager port — the public use-case surface of
// the façade (operationsManager.md §2 + operationsRead-ruling.md). Each op leads with
// the Manager-layer call Context (fwmgr.Context, embedding context.Context + the
// Principal); the *Manager derives ctx := rc.Context inside. The *ReconcileScope /
// *ScaleWhatIfPoints pointer params are load-bearing (nil ⇒ all-apps / run-rate-only).
//
// SCHEMA-FIRST: this interface (and the port I/O types) are GENERATED into
// contract.gen.go from this component's `.serviceContracts` entry in
// .aiarch/state/project.json (edit that entry + `make gen`; do NOT
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
	usage               usage.UsageAccess
	artifact            artifact.ArtifactAccess
	durableExecution    durableexecution.DurableExecutionAccess
	intervention        intervention.InterventionEngine
	autoscaler          autoscaler.AutoscalerEngine
	operationEstimation operationestimation.OperationEstimationEngine

	// Policy/config snapshots folded into the Workflows struct by RegisterWorker. They
	// are construction-time defaults (in production the Manager reads them from
	// head-state / the operated app's billing context). InfrastructureKind defaults to
	// the launch infrastructure; the rest are zero (matching what the composition root
	// passes today — the operations Worker carries no further policy config yet).
	//
	// interventionRetryBudget / interventionSLATier are the raw config surface for
	// intervention.InterventionPolicy (RetryBudget as a plain count; SLATier as a raw
	// string — no typed config source is wired yet). WorkerManifest() (workermanifest.go)
	// resolves SLATier via slaTierFromString (adapters.go) into the published
	// intervention.InterventionPolicy fed to wfDeps.
	interventionRetryBudget int64
	interventionSLATier     string
	// autoscalerPolicy is held in this package's OWN façade currency (the
	// autoscalerPolicy type, workflow.go), NOT the autoscaler Engine's published
	// AutoscalerPolicy: the façade's AutoscalerMode zero value is AutoscalerModeUnknown
	// (matching "no policy configured"), whereas the Engine's own AutoscalerMode has no
	// Unknown value (its zero value IS Auto). WorkerManifest() (workermanifest.go) folds
	// this straight through into wfDeps.AutoscalerPolicy; autoscalerPolicyToEngine
	// (adapters.go) converts it to the Engine's own shape at the one call site that
	// needs it (workflow.go, ProposeDesiredState).
	autoscalerPolicy   autoscalerPolicy
	infrastructureKind autoscaler.InfrastructureKind
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
	usage usage.UsageAccess,
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
		infrastructureKind:  autoscaler.InfrastructureKindGoTemporalPostgres,
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
	case ReasonUnknown:
		// zero-value sentinel — same "unknown reason" rejection as any true
		// unrecognized value.
		return DeployResult{}, newError(fwmgr.ContractMisuse,
			fmt.Sprintf("unknown desired-state reason %d", int(change.Reason)))
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

// ---------------------------------------------------------------------------
// Public data contracts (operationsManager.md §3) — the Client surface.
// Infrastructure-opaque: no Temporal id is exposed here. The operated-system
// head-state model and the Engine directive/projection types are referenced from
// their owning ResourceAccess / Engine packages (deps.go seams), not redefined.
// ---------------------------------------------------------------------------

// operatedAppID is the operated-system aggregate identifier; a plain uuid.UUID,
// canonical in operatedSystemStateAccess (operationsManager.md §3.0 / OQ-3 →
// standardised on OperatedAppId, not deployedAppId).
type operatedAppID = uuid.UUID

// customerID is the billing-customer aggregate identifier; a plain uuid.UUID,
// canonical in settlementStateAccess (operationsManager.md §3.0).
type customerID = uuid.UUID

// ReconcileScope: empty AppIDs means all in-flight operated apps (the default
// schedule firing).

// costProjection is the read-only op-time cost projection returned by
// QueryCostProjection (operationsManager.md §3.3 — CANONICAL in
// operationEstimationEngine.md §3). Mirrored as the Manager-local seam shape (deps.go
// costProjection); re-exported here as the façade result. NO state mutation produces
// it.
type costProjection = CostProjectionSeam

// ---------------------------------------------------------------------------
// Façade error model (operationsManager.md §3.4).
// CALLER/PROGRAMMER errors at the façade boundary — distinct from the workflow's
// own failure handling (Temporal RetryPolicy + the intervention/autoscaler
// alternative paths inside the workflow body). Kinds used: ContractMisuse,
// FailedPrecondition, NotFound, Unauthorized, Infrastructure.
// ---------------------------------------------------------------------------

func newError(kind fwmgr.Kind, detail string) *fwmgr.Error {
	return fwmgr.New(kind, detail)
}

// This file declares the Manager's CONSUMER-SIDE dependency interfaces (the Go
// "accept interfaces" idiom) for the collaborators that are NOT yet built as Go
// packages, plus the narrow consumer seams for the two that exist but whose frozen
// verb isn't on them yet:
//
//   - ArtifactAccess         — exists as internal/resourceaccess/artifact, BUT the
//     frozen retrieveDeployableBundle verb is NOT yet on the package (it currently
//     has RetrieveConstructionOutput). Consumed here via a NARROW seam interface
//     mirroring the frozen verb; the composition root adapts the concrete *artifact.Store
//     once that verb lands (escalation E-1 in C-MOP.md).
//   - DurableExecutionAccess — exists as internal/resourceaccess/durableexecution;
//     only RegisterSchedule is a contract op this Manager calls (at startup). The
//     in-workflow primitives (awaitSignal / startTimer) are the Manager's OWN
//     workflow code (D-DA category A), NOT RA methods — they live in workflow.go /
//     operationsmanager.go.
//
// operatedSystemStateAccess, operatedRuntimeAccess, and usageAccess are reached ONLY
// through the generated typed invokers (invokers.gen.go); interventionEngine,
// autoscalerEngine, and operationEstimationEngine are reached through their PUBLISHED
// contracts directly (workflow.go) — see the retirement note at the end of this file.
// No Manager-local mirror remains for any of the six.

// ===========================================================================
// operatedSystemStateAccess — reached ONLY through the generated typed invokers
// (invokers.gen.go), which carry the contract types (operatedsystemstate.OperatedSystem
// / OperatedSystemSummary / InFlightScope / Version / DelinquencyAction / RuntimeStatus)
// directly. Task 4 (seam-adapter cleanup) retired the Manager-local mirrors that used
// to duplicate these shapes 1:1 (operatedSystem, operatedSystemSummary, inFlightScope,
// delinquencyAction, version) — the workflow now speaks operatedsystemstate.* directly;
// no fold happens at the RA boundary. RuntimeStatusSeam (this package's OWN generated
// façade enum, contract.gen.go) is NOT one of those mirrors — it stays, because it is
// also the public OperatedSystemView/HealthSnapshotView field type; adapters.go keeps
// runtimeStatusFromState to bridge operatedsystemstate.RuntimeStatus into it at that
// façade boundary. Task 5 retired the OTHER caller of runtimeStatusFromState (the
// interventionEngine healthChange boundary) — that hop now goes straight from
// operatedsystemstate.RuntimeStatus to intervention.HealthStatus (healthStatusFromRuntimeStatus,
// adapters.go), since the engine is reached through its published contract now.
// ===========================================================================

// ===========================================================================
// operatedRuntimeAccess — reached ONLY through the generated typed invokers (see the
// operatedSystemStateAccess note above). Task 4 retired the Manager-local mirrors that
// duplicated its shapes 1:1 (runtimeDesiredState, sloStatusSeam, computeAttribution) —
// the workflow now speaks operatedruntime.RuntimeDesiredState / SloStatus /
// ComputeAttribution directly. Task 5 retired computeUnitsSeam (it was kept alive only
// as operationEstimationEngine's seam input shape via usageEventSeam below; the engine
// is now reached through its published contract, whose ObservedUsage the workflow
// builds directly from usage.UsageEvent — see observedUsageFromEvents, workflow.go).
// ===========================================================================

// ===========================================================================
// usageAccess — reached ONLY through the generated typed invokers (see the
// operatedSystemStateAccess note above). Task 4 retired usageRangeQuerySeam (an exact
// 1:1 mirror of usage.UsageRangeQuery with no other consumer) — the workflow now builds
// usage.UsageRangeQuery directly. Task 5 retired usageEventSeam — its only remaining
// role was operationEstimationEngine's (seam) input shape; readUsageRange (workflow.go)
// now returns []usage.UsageEvent straight from the generated invoker with no fold, and
// observedUsageFromEvents (workflow.go) aggregates it directly into the published
// operationestimation.ObservedUsage the Engine consumes.
// ===========================================================================

// ===========================================================================
// durableExecutionAccess — EXISTS (internal/resourceaccess/durableexecution). Only
// RegisterSchedule is a contract op this Manager calls (at startup). Consumed via a
// narrow seam interface so the composition root adapts the concrete
// *durableexecution.Runtime (whose RegisterSchedule signature is
// RegisterSchedule(ctx, ScheduleID, ScheduleSpec)). The in-workflow primitives
// (awaitSignal / startTimer) are the Manager's OWN workflow code (D-DA category A),
// NOT RA methods.
// ===========================================================================

// durableExecutionAccess is the Manager's consumer view: the one startup op.
// UNEXPORTED seam; the folded adapter bridges the published
// durableexecution.DurableExecutionAccess to it.
type durableExecutionAccess interface {
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// scheduleSpec mirrors durableexecution.ScheduleSpec for the one startup op (the
// operatedStateReconcile Schedule). The composition root adapts the concrete RA.
type scheduleSpec struct {
	ID           string
	WorkflowType string
	TaskQueue    string
	IntervalSecs int
}

// ===========================================================================
// interventionEngine / autoscalerEngine / operationEstimationEngine — RETIRED. The
// consumer-seam interfaces AND their local data mirrors (healthChange,
// interventionPolicy, healthDirective + its consts, telemetry, autoscalerDesiredState,
// infrastructureKind + its const, autoscaleDecisionSeam, computeUnitsSeam,
// usageEventSeam, observedUsage) are retired — the workflow reaches all three Engines
// through their PUBLISHED contracts (intervention.InterventionEngine /
// autoscaler.AutoscalerEngine / operationestimation.OperationEstimationEngine, each
// component's contract.gen.go), called DIRECTLY in-workflow by value (no Activity, no
// idempotency key, imports no Temporal), with
// fweng.Context{Context: context.Background()} supplied inline at each call site
// (workflow.go).
//
// autoscalerPolicy (workflow.go) is NOT one of the retired seam mirrors above, despite
// the name: it is the Manager's OWN config-currency type for the autoscaler policy,
// kept distinct from the Engine's published AutoscalerPolicy because the two Mode
// enums disagree on zero value (façade Unknown=0 vs the Engine's own zero value, which
// IS Auto) — a config → contract builder input (same allowed-survivor class as
// slaTierFromString below), not an identity seam mirror to a not-yet-built collaborator.
//
// adapters.go keeps the REAL divergence bridges that remain:
//   - healthStatusFromRuntimeStatus — operatedsystemstate.RuntimeStatus (5 values) to
//     intervention.HealthStatus (4 values); genuinely different enums, one hop
//     (collapsing the former two-hop path through this package's own RuntimeStatusSeam).
//   - slaTierFromString — the Manager's raw string SLA-tier config (no typed config
//     source is wired yet) to intervention.SLATier.
//   - autoscalerPolicyToEngine (+ autoscalerModeToEngine) — bridges this package's own
//     façade autoscalerPolicy (Mode: AutoscalerMode, Unknown=0/Auto=1/Manual=2) onto the
//     autoscaler Engine's own published AutoscalerPolicy (Mode: Auto=0/Manual=1, no
//     Unknown) at the ProposeDesiredState call site; genuinely divergent VALUES, not
//     just names. The OperatedSystemView façade output reads wf.AutoscalerPolicy.Mode
//     straight through (no bridge needed on that side).
//   - autoscaleActionToState — autoscaler.DecisionKind straight to
//     operatedsystemstate.AutoscaleAction (collapsing the former two-hop path through
//     this package's own façade AutoscaleAction).
//   - infrastructureKindForEstimation — autoscaler.InfrastructureKind (this Manager's
//     canonical config currency, since autoscalerPolicy/DesiredState carry it too) to
//     operationestimation.InfrastructureKind; two independently generated enums that
//     happen to share values today.
//   - moneyFromEstimation / whatIfCurveFromEstimation / scalePointsToEstimation —
//     the façade's OWN generated Money/WhatIfCurve/WhatIfPoint/ScalePoint
//     (contract.gen.go) are genuinely distinct types from operationestimation's (the
//     façade's WhatIfPoint/ScalePoint carry Replicas int64; the engine's carry
//     LoadMultiplier float64 — a real unit divergence, not a rename).
//
// observedUsageFromEvents (workflow.go) is the real aggregation (Σ Units.Amount, count)
// that folds the usage RA's read range into the Engine's ObservedUsage input — kept
// alongside billing's foldRevenue/foldUsage precedent, not an identity mirror.
// ===========================================================================

// This file holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract enum (e.g. the canonical
// name lookup that used to be a String() method) lives here as a free function. This
// is the OutputPath / PipelineHandle precedent (a contract-value-type method becomes
// a free function so the generated scalar/enum carries no behavior).

// desiredStateReasonName returns the canonical wire name for a desired-state reason.
// Kept as a FREE FUNCTION (not a DesiredStateReason method) so the generated enum is
// pure data.
func desiredStateReasonName(r DesiredStateReason) string {
	switch r {
	case ReasonUnknown:
		// zero-value sentinel.
		return "unknown"
	case ReasonDeployAfterConstruction:
		return "deployAfterConstruction"
	case ReasonOperator:
		return "operator"
	case ReasonAutoscale:
		return "autoscale"
	case ReasonDelinquency:
		return "delinquency"
	}
	// Unreachable for the five defined DesiredStateReason values above (the
	// exhaustive linter enforces that every real variant has its own case); kept
	// as a defensive fallback for an out-of-range ordinal.
	return "unknown"
}

// autoscaleActionName returns the canonical name for an autoscale action. Kept as a
// FREE FUNCTION (not an AutoscaleAction method) so the generated enum is pure data.
func autoscaleActionName(a AutoscaleAction) string {
	switch a {
	case AutoscaleNoChange:
		return "NoChange"
	case AutoscaleScaleUp:
		return "ScaleUp"
	case AutoscaleScaleDown:
		return "ScaleDown"
	case AutoscalePause:
		return "Pause"
	case AutoscaleResume:
		return "Resume"
	}
	// Unreachable for the five defined AutoscaleAction values above (the
	// exhaustive linter enforces that every real variant has its own case); kept
	// as a defensive fallback for an out-of-range ordinal.
	return "Unknown"
}

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// ResourceAccess interfaces (the dependencies the GENERATED constructor
// NewOperationsManager receives) to the Manager's unexported downstream seams
// (deps.go), plus the REAL Engine-contract divergence bridges (healthStatusFromRuntimeStatus,
// slaTierFromString, autoscalerPolicyToEngine, autoscaleActionToState,
// infrastructureKindForEstimation, moneyFromEstimation, whatIfCurveFromEstimation,
// scalePointsToEstimation). Per the founder DI model (2026-06-28) these were retired
// from cmd/server and live HERE, in the one package that knows both sides — the
// Manager depends on each dependency's PUBLISHED interface and adapts it internally
// (Option-B boundary mapping), exactly as construction/systemdesign/projectdesign fold
// their adapters.
//
// None of these imports Temporal (the Manager owns it); they are plain value-copy
// bridges run inside the Manager's Activities (RA seams). The three Engines
// (intervention.InterventionEngine / autoscaler.AutoscalerEngine /
// operationestimation.OperationEstimationEngine) have NO adapter — the workflow calls
// their published contracts directly (workflow.go). The mechanical enum/struct copies
// map by IDENTITY (an explicit switch), not by raw int, so a future re-order on either
// side is safe. Where the published shape is RICHER than the Manager-local config
// (extra telemetry/policy fields) the unset fields default to zero — the operations
// Worker carries no further policy config yet, and the stub RAs return
// not-implemented at runtime regardless.

// ===========================================================================
// operatedSystemStateAccess contract converters. The former operatedSystemStateAdapter
// struct is retired — the workflow reaches the RA through the generated invokers
// (invokers.gen.go) and now speaks operatedsystemstate.* directly as its internal
// currency (Task 4: the operatedSystem / operatedSystemSummary / inFlightScope /
// delinquencyAction / version Manager-local mirrors that used to require folding here
// are deleted). The two converters below are NOT fold-boundary artifacts — each bridges
// operatedsystemstate.RuntimeStatus to a genuinely distinct GENERATED type this package
// does not own outright: DesiredStateReason (this package's own façade input type) and
// RuntimeStatusSeam (this package's own façade output type, the OperatedSystemView /
// HealthSnapshotView field type).
// ===========================================================================

func desiredStateReasonToState(r DesiredStateReason) operatedsystemstate.DesiredStateReason {
	switch r {
	case ReasonUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return operatedsystemstate.ReasonUnknown
	case ReasonDeployAfterConstruction:
		return operatedsystemstate.ReasonDeployAfterConstruction
	case ReasonOperator:
		return operatedsystemstate.ReasonOperator
	case ReasonAutoscale:
		return operatedsystemstate.ReasonAutoscale
	case ReasonDelinquency:
		return operatedsystemstate.ReasonDelinquency
	default:
		return operatedsystemstate.ReasonUnknown
	}
}

// autoscaleActionToState bridges the autoscaler Engine's own DecisionKind straight to
// operatedsystemstate's persisted AutoscaleAction — two independently generated enums
// that happen to share order/values today (an explicit switch, not a raw cast, so a
// future re-order on either side stays safe). Task 5 collapsed the former two-hop path
// (autoscaler.DecisionKind -> this package's own façade AutoscaleAction ->
// operatedsystemstate.AutoscaleAction) into this one direct converter, now that the
// Engine is reached through its published contract and no longer folds its decision
// into the façade's own enum along the way.
func autoscaleActionToState(k autoscaler.DecisionKind) operatedsystemstate.AutoscaleAction {
	switch k {
	case autoscaler.DecisionNoChange:
		// no-op decision, nothing to do — explicit mapping to the state package's own
		// no-op constant.
		return operatedsystemstate.AutoscaleNoChange
	case autoscaler.DecisionScaleUp:
		return operatedsystemstate.AutoscaleScaleUp
	case autoscaler.DecisionScaleDown:
		return operatedsystemstate.AutoscaleScaleDown
	case autoscaler.DecisionPause:
		return operatedsystemstate.AutoscalePause
	case autoscaler.DecisionResume:
		return operatedsystemstate.AutoscaleResume
	default:
		return operatedsystemstate.AutoscaleNoChange
	}
}

func autoscaleDecisionToState(d *autoscaler.Decision) *operatedsystemstate.AutoscaleDecision {
	if d == nil {
		return nil
	}
	return &operatedsystemstate.AutoscaleDecision{
		Action:     autoscaleActionToState(d.Kind),
		Delta:      d.Delta,
		ToBaseline: d.ToBaseline,
	}
}

// ===========================================================================
// operatedRuntimeAccess -> operatedsystemstate contract converter (the
// operatedRuntimeAdapter struct is retired; see the operatedSystemStateAccess note).
// DIVERGENT survivor (Task 4): operatedruntime.RuntimeStatus and
// operatedsystemstate.RuntimeStatus are two RAs' independently generated enums that
// happen to share values today; the workflow canonicalizes observed health into the
// operatedsystemstate vocabulary at this ONE boundary (generated <-> generated, never
// seam <-> generated) so a future re-order on either side stays safe.
// ===========================================================================

func runtimeStatusFromRuntime(s operatedruntime.RuntimeStatus) operatedsystemstate.RuntimeStatus {
	switch s {
	case operatedruntime.RuntimeStatusUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return operatedsystemstate.RuntimeStatusUnknown
	case operatedruntime.RuntimeStatusPending:
		return operatedsystemstate.RuntimeStatusPending
	case operatedruntime.RuntimeStatusHealthy:
		return operatedsystemstate.RuntimeStatusHealthy
	case operatedruntime.RuntimeStatusDegraded:
		return operatedsystemstate.RuntimeStatusDegraded
	case operatedruntime.RuntimeStatusWithdrawn:
		return operatedsystemstate.RuntimeStatusWithdrawn
	default:
		return operatedsystemstate.RuntimeStatusUnknown
	}
}

// ===========================================================================
// durableExecutionAccess adapter — over durableexecution.DurableExecutionAccess. Only
// the startup RegisterSchedule verb is consumed (the published ScheduleSpec resolves
// the task queue via its KindBinding table, so the seam's TaskQueue is not threaded).
// ===========================================================================

type durableAdapter struct {
	inner durableexecution.DurableExecutionAccess
}

var _ durableExecutionAccess = durableAdapter{}

func (a durableAdapter) RegisterSchedule(ctx context.Context, spec scheduleSpec) error {
	return a.inner.RegisterSchedule(
		fwra.Context{Context: ctx},
		durableexecution.ScheduleID(spec.ID),
		durableexecution.ScheduleSpec{
			ExecutionKind: durableexecution.ExecutionKind(spec.WorkflowType),
			Cadence:       durableexecution.Cadence{Every: time.Duration(spec.IntervalSecs) * time.Second},
		},
	)
}

// slaTierFromString resolves the Manager's raw string SLA-tier config (no typed config
// source is wired yet — the operations Worker carries no policy config today) onto
// intervention.SLATier. Kept as a genuine config -> engine-input builder (not an
// identity seam mirror): unlike the retired local interventionPolicy struct, there is
// no shadow SLATier enum on this side to eliminate — the source really is a string.
func slaTierFromString(s string) intervention.SLATier {
	switch s {
	case "paid":
		return intervention.SLATierPaid
	case "enterprise":
		return intervention.SLATierEnterprise
	default:
		return intervention.SLATierFree
	}
}

// ===========================================================================
// operationEstimationEngine — REAL divergence bridges. The workflow calls the
// published operationestimation.OperationEstimationEngine.ProjectForOperatedApp
// DIRECTLY (workflow.go). observedUsageFromEvents (the real Σ-Units.Amount aggregation
// off the usage RA's read range) lives in workflow.go, alongside billing's
// foldRevenue/foldUsage precedent — it is workflow-owned folding, not a boundary
// adapter.
// ===========================================================================

// infrastructureKindForEstimation bridges this Manager's canonical InfrastructureKind
// currency (autoscaler.InfrastructureKind — also autoscalerPolicy/DesiredState's type)
// onto operationestimation's OWN InfrastructureKind. Two independently generated enums
// that happen to share values today; an explicit switch, not a raw cast.
func infrastructureKindForEstimation(k autoscaler.InfrastructureKind) operationestimation.InfrastructureKind {
	switch k {
	case autoscaler.InfrastructureKindUnknown:
		// zero-value sentinel — no equivalent Unknown case to translate to yet.
		return operationestimation.InfrastructureKindUnknown
	case autoscaler.InfrastructureKindGoTemporalPostgres:
		return operationestimation.InfrastructureKindGoTemporalPostgres
	default:
		return operationestimation.InfrastructureKindUnknown
	}
}

// moneyFromEstimation bridges operationestimation's own Money onto this package's
// generated façade Money (contract.gen.go) — same shape today, but genuinely distinct
// generated types (their JSON tags already diverge: MinorUnits/Currency here vs.
// minorUnits/currency on the engine's side), so a field-by-field copy, not a cast.
func moneyFromEstimation(m operationestimation.Money) Money {
	return Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// This file holds the Workflows struct (the Manager's downstream dependency set),
// the four workflow bodies + the delinquency-enforcement branch (the encapsulated
// OperationsWorkflow volatility — operationsManager.md §6.3), the workflow-level
// Conflict re-read→re-apply loop (§6.5), and the activity-option presets.
//
// How the two dependency kinds are reached differs by determinism class:
//   - The three Engines (intervention.InterventionEngine / autoscaler.AutoscalerEngine
//     / operationestimation.OperationEstimationEngine — the PUBLISHED contracts, no
//     Manager-local seam) are PURE, deterministic, called DIRECTLY in-workflow (no
//     Activity wrapper — replay-safe), with fweng.Context{Context: context.Background()}
//     supplied inline at each call site.
//   - The ResourceAccess ports (OperatedSystemState / OperatedRuntime / Usage /
//     Artifacts) are I/O and NON-deterministic; the workflow invokes the Activity
//     methods on this same struct via workflow.ExecuteActivity (activities.go).

// wfDeps bundles every downstream dependency the operationsManager orchestrates,
// passed to newWorkflows (from WorkerManifest, workermanifest.go) and held on the
// Workflows struct. The three Engines are typed as their PUBLISHED contract interfaces
// (no Manager-local seam), called DIRECTLY in-workflow. The ResourceAccess layer is
// reached ONLY through the generated typed invoker surface (Acts, invokers.gen.go) —
// the former RA consumer seams + composition-root adapters are retired.
type wfDeps struct {
	Intervention intervention.InterventionEngine
	Autoscaler   autoscaler.AutoscalerEngine
	Estimation   operationestimation.OperationEstimationEngine

	// Acts is the generated workflow-side invoker surface (invokers.gen.go): one
	// method per ResourceAccess activity, carrying contract types. Its Opts hook
	// supplies the per-activity option presets (workermanifest.go).
	Acts genInvokers

	// Policy snapshots fed to the Engines by value. In production the Manager reads
	// them from head-state; held here as the construction-time config values.
	// InterventionPolicy is built from the Manager's raw string SLA-tier config via
	// slaTierFromString at WorkerManifest() construction time (adapters.go) — already
	// typed as the Engine's own published input. AutoscalerPolicy stays in the
	// Manager's OWN façade currency (autoscalerPolicy, below) rather than the autoscaler
	// Engine's published AutoscalerPolicy: the two Mode enums genuinely disagree on
	// zero value (façade Unknown=0 vs the Engine's own zero value, which IS Auto), and
	// this is also this package's OperatedSystemView.Autoscaler.Mode field's currency —
	// autoscalerPolicyToEngine (adapters.go) converts it at the ProposeDesiredState call
	// site (the one place that needs the Engine's own shape).
	InterventionPolicy intervention.InterventionPolicy
	AutoscalerPolicy   autoscalerPolicy
	// InfrastructureKind is this Manager's canonical currency for the concept shared
	// by the autoscaler and estimation Engines — autoscaler.InfrastructureKind, since
	// AutoscalerPolicy/DesiredState carry it too; infrastructureKindForEstimation
	// (adapters.go) bridges it onto the estimation Engine's own, independently
	// generated InfrastructureKind at that one remaining boundary.
	InfrastructureKind autoscaler.InfrastructureKind

	// CurrentCycleID is the billing cycle the Manager attributes observed usage to
	// (carried onto the usage events). Held here as the construction-time seam value;
	// in production the Manager derives it from the operated app's billing context.
	CurrentCycleID string
	CustomerID     customerID
}

// autoscalerPolicy is the Manager's OWN façade-shaped autoscaler policy config
// currency (Kind still autoscaler.InfrastructureKind — the Manager's canonical
// currency for that concept, per InfrastructureKind above — but Mode is THIS
// package's own generated AutoscalerMode, contract.gen.go, whose zero value is
// AutoscalerModeUnknown). Deliberately NOT the autoscaler Engine's published
// AutoscalerPolicy: that type's own Mode has no Unknown value (its zero value IS
// Auto), so holding the Manager's config in the Engine's shape would silently report
// an unconfigured policy as "Auto" on the OperatedSystemView (regression: the Task 5
// engine-contract retype did exactly this). autoscalerPolicyToEngine (adapters.go)
// bridges this to the Engine's own AutoscalerPolicy at the one call site that needs
// it (ProposeDesiredState, below); ViewWorkflow reads Mode straight off this façade
// type with no bridge needed. A config → contract builder input (allowed survivor
// class), not an identity seam mirror.
type autoscalerPolicy struct {
	Kind             autoscaler.InfrastructureKind
	Mode             AutoscalerMode
	MinReplicas      int64
	BaselineReplicas int64
}

// workflows is the single operationsManager component struct — the workflow receiver.
// The RA activities are the generated genActivities (activities.gen.go); this struct
// reaches them through the typed invokers (Acts).
type workflows struct {
	Intervention intervention.InterventionEngine
	Autoscaler   autoscaler.AutoscalerEngine
	Estimation   operationestimation.OperationEstimationEngine

	Acts genInvokers

	InterventionPolicy intervention.InterventionPolicy
	AutoscalerPolicy   autoscalerPolicy
	InfrastructureKind autoscaler.InfrastructureKind
	CurrentCycleID     string
	CustomerID         customerID
}

// newWorkflows builds the Workflows receiver from the injected wfDeps.
func newWorkflows(d wfDeps) *workflows {
	return &workflows{
		Intervention:       d.Intervention,
		Autoscaler:         d.Autoscaler,
		Estimation:         d.Estimation,
		Acts:               d.Acts,
		InterventionPolicy: d.InterventionPolicy,
		AutoscalerPolicy:   d.AutoscalerPolicy,
		InfrastructureKind: d.InfrastructureKind,
		CurrentCycleID:     d.CurrentCycleID,
		CustomerID:         d.CustomerID,
	}
}

// Bounds (in-workflow guards; NOT contract surface).
const (
	// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
	// loop (§6.5).
	maxMutateConflictAttempts = 20
)

// raConflictErrType is the canonical Temporal Type() a head-state mutation Activity
// surfaces when expectedVersion is stale; the workflow recovers with the bounded
// re-read→re-apply loop (§6.5).
var raConflictErrType = fwmgr.RAErrType(fwra.Conflict)

// observedUsageFromEvents aggregates the workflow's read usage.UsageEvent range into
// operationEstimationEngine's published ObservedUsage input (ComputeUnitSeconds =
// Σ Units.Amount, RequestCount = len(events)) — the real folding operation the former
// estimationAdapter performed, kept alongside billing's foldRevenue/foldUsage
// precedent (workflow-owned aggregation, not a boundary adapter). StorageBytesMonths /
// EgressBytes / ObservedReplicas are not sourced by any RA read today (same as before
// Task 5); left zero.
func observedUsageFromEvents(events []usage.UsageEvent) operationestimation.ObservedUsage {
	var computeUnitSeconds float64
	for _, e := range events {
		computeUnitSeconds += e.Units.Amount
	}
	return operationestimation.ObservedUsage{
		ComputeUnitSeconds: computeUnitSeconds,
		RequestCount:       int64(len(events)),
	}
}

// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

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
