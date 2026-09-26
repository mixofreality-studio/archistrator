// managerlog.go is the composition-root logging SEAM over the two web/MCP-exposed
// Managers. Each Manager is an interface consumed by BOTH the generated REST Handler
// (internal/client/web) and the generated MCP tool Handler (internal/client/mcp); both
// Handlers are handed the SAME wrapped instance, so wrapping once here is the single
// choke point that catches every Infrastructure-kind error surfaced to a client across
// BOTH transports — without touching generated handler code or the external generators.
//
// The client body stays opaque (the transport still returns only the façade Detail); the
// COMPLETE cause chain (err.Error() of the wrapped original), the operation name, and the
// project id are logged server-side via the composition-root slog logger. Non-Infrastructure
// and nil errors pass through unlogged.
//
// This file lives OUTSIDE internal/, so it is not scanned by the Method arch checker — it is
// pure composition-root wiring glue. The real Managers are still handed UNWRAPPED to each
// RegisterWorker (which type-asserts back to the concrete impl); only the client layer sees
// the wrappers.
package main

import (
	"errors"
	"log/slog"

	"github.com/google/uuid"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
)

// logInfraError logs a surfaced Infrastructure-kind manager error with its complete cause
// chain, the operation name, and the project id, then returns err unchanged so callers can
// tail-return it. nil and non-Infrastructure errors are pass-throughs.
func logInfraError(logger *slog.Logger, op, projectID string, err error) error {
	var me *fwmanager.Error
	if errors.As(err, &me) && me.Kind == fwmanager.Infrastructure {
		logger.Error("manager infrastructure error",
			"op", op,
			"projectId", projectID,
			"cause", err.Error())
	}
	return err
}

// --- Delivery --------------------------------------------------------------

type loggingDeliveryManager struct {
	inner delivery.DeliveryManager
	log   *slog.Logger
}

func (m loggingDeliveryManager) StartProject(rc fwmanager.Context, owner delivery.OwnerScope, name string, projectID *string, model *delivery.OperatingModel, research *delivery.ResearchInput, start bool) (delivery.StartProjectResult, error) {
	// name-as-identity: on a create (projectID absent) the supplied name IS the project
	// id; on an update the caller addressed an existing one.
	scope := name
	if projectID != nil {
		scope = *projectID
	}
	v, err := m.inner.StartProject(rc, owner, name, projectID, model, research, start)
	return v, logInfraError(m.log, "Delivery.StartProject", scope, err)
}

func (m loggingDeliveryManager) ExecuteNextActivity(rc fwmanager.Context, projectID delivery.ProjectID, tickID string) (delivery.PumpResult, error) {
	v, err := m.inner.ExecuteNextActivity(rc, projectID, tickID)
	return v, logInfraError(m.log, "Delivery.ExecuteNextActivity", string(projectID), err)
}

func (m loggingDeliveryManager) DispatchActivityTask(rc fwmanager.Context, projectID delivery.ProjectID, activityID delivery.ActivityID, taskID string, feedback *delivery.ReviewFeedback) (delivery.SessionRef, error) {
	v, err := m.inner.DispatchActivityTask(rc, projectID, activityID, taskID, feedback)
	return v, logInfraError(m.log, "Delivery.DispatchActivityTask", string(projectID), err)
}

func (m loggingDeliveryManager) SubmitReviewDecision(rc fwmanager.Context, projectID delivery.ProjectID, activityID delivery.ActivityID, taskID string, decision delivery.ReviewDecisionInput, feedback *delivery.ReviewFeedback) error {
	return logInfraError(m.log, "Delivery.SubmitReviewDecision", string(projectID),
		m.inner.SubmitReviewDecision(rc, projectID, activityID, taskID, decision, feedback))
}

func (m loggingDeliveryManager) AskQuestions(rc fwmanager.Context, projectID delivery.ProjectID, activityID delivery.ActivityID, taskID string, addressee string, questions []delivery.AnchoredComment) error {
	return logInfraError(m.log, "Delivery.AskQuestions", string(projectID),
		m.inner.AskQuestions(rc, projectID, activityID, taskID, addressee, questions))
}

func (m loggingDeliveryManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID delivery.ProjectID, activityID delivery.ActivityID, taskID string, note string) error {
	return logInfraError(m.log, "Delivery.AcknowledgeStaleBasis", string(projectID),
		m.inner.AcknowledgeStaleBasis(rc, projectID, activityID, taskID, note))
}

func (m loggingDeliveryManager) SetProjectRunState(rc fwmanager.Context, projectID delivery.ProjectID, runState delivery.ProjectRunState, reason string) error {
	return logInfraError(m.log, "Delivery.SetProjectRunState", string(projectID),
		m.inner.SetProjectRunState(rc, projectID, runState, reason))
}

func (m loggingDeliveryManager) OverrideActivity(rc fwmanager.Context, projectID delivery.ProjectID, activityID delivery.ActivityID, override delivery.ActivityOverride) error {
	return logInfraError(m.log, "Delivery.OverrideActivity", string(projectID),
		m.inner.OverrideActivity(rc, projectID, activityID, override))
}

func (m loggingDeliveryManager) ReplanProject(rc fwmanager.Context, projectID *delivery.ProjectID, tickID string) (delivery.ReplanSweepResult, error) {
	// A cross-project sweep addresses no single project when projectID is nil.
	scope := ""
	if projectID != nil {
		scope = string(*projectID)
	}
	v, err := m.inner.ReplanProject(rc, projectID, tickID)
	return v, logInfraError(m.log, "Delivery.ReplanProject", scope, err)
}

func (m loggingDeliveryManager) SetProjectExecutionPolicy(rc fwmanager.Context, projectID delivery.ProjectID, policy delivery.ExecutionPolicyInput) error {
	return logInfraError(m.log, "Delivery.SetProjectExecutionPolicy", string(projectID),
		m.inner.SetProjectExecutionPolicy(rc, projectID, policy))
}

func (m loggingDeliveryManager) QueryProjectView(rc fwmanager.Context, query delivery.ProjectViewQuery) (delivery.ProjectView, error) {
	// The projects view addresses an owner rather than one project.
	scope := ""
	if query.ProjectID != nil {
		scope = *query.ProjectID
	}
	v, err := m.inner.QueryProjectView(rc, query)
	return v, logInfraError(m.log, "Delivery.QueryProjectView", scope, err)
}

func (m loggingDeliveryManager) QueryActivityView(rc fwmanager.Context, projectID delivery.ProjectID, activityID delivery.ActivityID) (delivery.ActivityView, error) {
	v, err := m.inner.QueryActivityView(rc, projectID, activityID)
	return v, logInfraError(m.log, "Delivery.QueryActivityView", string(projectID), err)
}

// --- Operations ------------------------------------------------------------

type loggingOperationsManager struct {
	inner operations.OperationsManager
	log   *slog.Logger
}

func (m loggingOperationsManager) ApplyDelinquencyPolicy(rc fwmanager.Context, customerID uuid.UUID, delinquencyContext operations.DelinquencyContext) error {
	return logInfraError(m.log, "Operations.ApplyDelinquencyPolicy", customerID.String(), m.inner.ApplyDelinquencyPolicy(rc, customerID, delinquencyContext))
}

func (m loggingOperationsManager) DeployAfterConstruction(rc fwmanager.Context, operatedAppID uuid.UUID, change operations.DesiredStateChange) (operations.DeployResult, error) {
	v, err := m.inner.DeployAfterConstruction(rc, operatedAppID, change)
	return v, logInfraError(m.log, "Operations.DeployAfterConstruction", operatedAppID.String(), err)
}

func (m loggingOperationsManager) QueryCostProjection(rc fwmanager.Context, operatedAppID uuid.UUID, requestID string, points *operations.ScaleWhatIfPoints) (operations.CostProjectionSeam, error) {
	v, err := m.inner.QueryCostProjection(rc, operatedAppID, requestID, points)
	return v, logInfraError(m.log, "Operations.QueryCostProjection", operatedAppID.String(), err)
}

func (m loggingOperationsManager) QueryOperatedSystemView(rc fwmanager.Context, operatedAppID uuid.UUID, requestID string) (operations.OperatedSystemView, error) {
	v, err := m.inner.QueryOperatedSystemView(rc, operatedAppID, requestID)
	return v, logInfraError(m.log, "Operations.QueryOperatedSystemView", operatedAppID.String(), err)
}

func (m loggingOperationsManager) QueryDeploymentHealth(rc fwmanager.Context, operatedAppID uuid.UUID) (operations.DeploymentHealth, error) {
	v, err := m.inner.QueryDeploymentHealth(rc, operatedAppID)
	return v, logInfraError(m.log, "Operations.QueryDeploymentHealth", operatedAppID.String(), err)
}

func (m loggingOperationsManager) ReconcileOperatedState(rc fwmanager.Context, tickID string, scope *operations.ReconcileScope) (operations.ReconcileResult, error) {
	// A reconcile tick addresses no single operated app.
	v, err := m.inner.ReconcileOperatedState(rc, tickID, scope)
	return v, logInfraError(m.log, "Operations.ReconcileOperatedState", "", err)
}

func (m loggingOperationsManager) RegisterOperatedApp(rc fwmanager.Context, operatedAppID uuid.UUID, customerID uuid.UUID, projectRef string, deployableBundleRef string) (operations.Version, error) {
	v, err := m.inner.RegisterOperatedApp(rc, operatedAppID, customerID, projectRef, deployableBundleRef)
	return v, logInfraError(m.log, "Operations.RegisterOperatedApp", operatedAppID.String(), err)
}

func (m loggingOperationsManager) WithdrawSystem(rc fwmanager.Context, operatedAppID uuid.UUID, changeID string, reason operations.WithdrawReason) (operations.WithdrawResult, error) {
	v, err := m.inner.WithdrawSystem(rc, operatedAppID, changeID, reason)
	return v, logInfraError(m.log, "Operations.WithdrawSystem", operatedAppID.String(), err)
}
