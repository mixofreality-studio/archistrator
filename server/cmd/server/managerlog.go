// managerlog.go is the composition-root logging SEAM over the four web/MCP-exposed
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
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
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

// --- SystemDesign ----------------------------------------------------------

type loggingSystemDesignManager struct {
	inner systemdesign.SystemDesignManager
	log   *slog.Logger
}

func (m loggingSystemDesignManager) AdvancePhase(rc fwmanager.Context, projectID systemdesign.ProjectID, acknowledgeStale bool) (systemdesign.PhaseAdvanceResult, error) {
	v, err := m.inner.AdvancePhase(rc, projectID, acknowledgeStale)
	return v, logInfraError(m.log, "SystemDesign.AdvancePhase", string(projectID), err)
}

func (m loggingSystemDesignManager) CreateProject(rc fwmanager.Context, owner systemdesign.OwnerScope, name string) (systemdesign.ProjectID, error) {
	// name-as-identity: the supplied name IS the project id.
	v, err := m.inner.CreateProject(rc, owner, name)
	return v, logInfraError(m.log, "SystemDesign.CreateProject", name, err)
}

func (m loggingSystemDesignManager) GetProject(rc fwmanager.Context, projectID systemdesign.ProjectID) (systemdesign.ProjectState, error) {
	v, err := m.inner.GetProject(rc, projectID)
	return v, logInfraError(m.log, "SystemDesign.GetProject", string(projectID), err)
}

func (m loggingSystemDesignManager) GetDesignHealth(rc fwmanager.Context, projectID systemdesign.ProjectID) (systemdesign.DesignHealth, error) {
	v, err := m.inner.GetDesignHealth(rc, projectID)
	return v, logInfraError(m.log, "SystemDesign.GetDesignHealth", string(projectID), err)
}

func (m loggingSystemDesignManager) GetEpisodeTimeline(rc fwmanager.Context, projectID systemdesign.ProjectID, episodeID string) (systemdesign.EpisodeTimeline, error) {
	v, err := m.inner.GetEpisodeTimeline(rc, projectID, episodeID)
	return v, logInfraError(m.log, "SystemDesign.GetEpisodeTimeline", string(projectID), err)
}

func (m loggingSystemDesignManager) GetSessionState(rc fwmanager.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind) (systemdesign.SessionStateView, error) {
	v, err := m.inner.GetSessionState(rc, projectID, kind)
	return v, logInfraError(m.log, "SystemDesign.GetSessionState", string(projectID), err)
}

func (m loggingSystemDesignManager) ListEpisodesForArtifact(rc fwmanager.Context, projectID systemdesign.ProjectID, artifactKind systemdesign.ArtifactKind) ([]systemdesign.EpisodeRecordView, error) {
	v, err := m.inner.ListEpisodesForArtifact(rc, projectID, artifactKind)
	return v, logInfraError(m.log, "SystemDesign.ListEpisodesForArtifact", string(projectID), err)
}

func (m loggingSystemDesignManager) ListProjects(rc fwmanager.Context, owner systemdesign.OwnerScope) ([]systemdesign.ProjectSummary, error) {
	// A catalog op with no single project — the owner scope is the closest identity.
	v, err := m.inner.ListProjects(rc, owner)
	return v, logInfraError(m.log, "SystemDesign.ListProjects", string(owner), err)
}

func (m loggingSystemDesignManager) RequestArtifactDraft(rc fwmanager.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, feedback *systemdesign.ReviewFeedback) (systemdesign.SessionRef, error) {
	v, err := m.inner.RequestArtifactDraft(rc, projectID, kind, feedback)
	return v, logInfraError(m.log, "SystemDesign.RequestArtifactDraft", string(projectID), err)
}

func (m loggingSystemDesignManager) SetResearchInput(rc fwmanager.Context, projectID systemdesign.ProjectID, research systemdesign.ResearchInput) (systemdesign.Version, error) {
	v, err := m.inner.SetResearchInput(rc, projectID, research)
	return v, logInfraError(m.log, "SystemDesign.SetResearchInput", string(projectID), err)
}

func (m loggingSystemDesignManager) SetOperatingModel(rc fwmanager.Context, projectID systemdesign.ProjectID, model systemdesign.OperatingModel) (systemdesign.Version, error) {
	v, err := m.inner.SetOperatingModel(rc, projectID, model)
	return v, logInfraError(m.log, "SystemDesign.SetOperatingModel", string(projectID), err)
}

func (m loggingSystemDesignManager) StartSystemDesign(rc fwmanager.Context, projectID systemdesign.ProjectID) (systemdesign.SessionRef, error) {
	v, err := m.inner.StartSystemDesign(rc, projectID)
	return v, logInfraError(m.log, "SystemDesign.StartSystemDesign", string(projectID), err)
}

func (m loggingSystemDesignManager) SubmitReviewDecision(rc fwmanager.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, decision systemdesign.ReviewDecision, feedback *systemdesign.ReviewFeedback) error {
	return logInfraError(m.log, "SystemDesign.SubmitReviewDecision", string(projectID), m.inner.SubmitReviewDecision(rc, projectID, kind, decision, feedback))
}

func (m loggingSystemDesignManager) SetReviewCommentStatus(rc fwmanager.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, commentID string, status string) error {
	return logInfraError(m.log, "SystemDesign.SetReviewCommentStatus", string(projectID), m.inner.SetReviewCommentStatus(rc, projectID, kind, commentID, status))
}

func (m loggingSystemDesignManager) AskQuestions(rc fwmanager.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, addressee string, questions []systemdesign.AnchoredComment) error {
	return logInfraError(m.log, "SystemDesign.AskQuestions", string(projectID), m.inner.AskQuestions(rc, projectID, kind, addressee, questions))
}

func (m loggingSystemDesignManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, note string) error {
	return logInfraError(m.log, "SystemDesign.AcknowledgeStaleBasis", string(projectID), m.inner.AcknowledgeStaleBasis(rc, projectID, kind, note))
}

// --- ProjectDesign ---------------------------------------------------------

type loggingProjectDesignManager struct {
	inner projectdesign.ProjectDesignManager
	log   *slog.Logger
}

func (m loggingProjectDesignManager) AdvanceToConstruction(rc fwmanager.Context, projectID projectdesign.ProjectID, acknowledgeStale bool) (projectdesign.PhaseAdvanceResult, error) {
	v, err := m.inner.AdvanceToConstruction(rc, projectID, acknowledgeStale)
	return v, logInfraError(m.log, "ProjectDesign.AdvanceToConstruction", string(projectID), err)
}

func (m loggingProjectDesignManager) GetEpisodeTimeline(rc fwmanager.Context, projectID projectdesign.ProjectID, episodeID string) (projectdesign.EpisodeTimeline, error) {
	v, err := m.inner.GetEpisodeTimeline(rc, projectID, episodeID)
	return v, logInfraError(m.log, "ProjectDesign.GetEpisodeTimeline", string(projectID), err)
}

func (m loggingProjectDesignManager) GetSessionState(rc fwmanager.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind) (projectdesign.SessionStateView, error) {
	v, err := m.inner.GetSessionState(rc, projectID, kind)
	return v, logInfraError(m.log, "ProjectDesign.GetSessionState", string(projectID), err)
}

func (m loggingProjectDesignManager) ListEpisodesForArtifact(rc fwmanager.Context, projectID projectdesign.ProjectID, artifactKind projectdesign.ArtifactKind) ([]projectdesign.EpisodeRecordView, error) {
	v, err := m.inner.ListEpisodesForArtifact(rc, projectID, artifactKind)
	return v, logInfraError(m.log, "ProjectDesign.ListEpisodesForArtifact", string(projectID), err)
}

func (m loggingProjectDesignManager) RequestArtifactDraft(rc fwmanager.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, feedback *projectdesign.ReviewFeedback) (projectdesign.SessionRef, error) {
	v, err := m.inner.RequestArtifactDraft(rc, projectID, kind, feedback)
	return v, logInfraError(m.log, "ProjectDesign.RequestArtifactDraft", string(projectID), err)
}

func (m loggingProjectDesignManager) RequestSDPCommit(rc fwmanager.Context, projectID projectdesign.ProjectID) (projectdesign.SessionRef, error) {
	v, err := m.inner.RequestSDPCommit(rc, projectID)
	return v, logInfraError(m.log, "ProjectDesign.RequestSDPCommit", string(projectID), err)
}

func (m loggingProjectDesignManager) SetReviewCommentStatus(rc fwmanager.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, commentID string, status string) error {
	return logInfraError(m.log, "ProjectDesign.SetReviewCommentStatus", string(projectID), m.inner.SetReviewCommentStatus(rc, projectID, kind, commentID, status))
}

func (m loggingProjectDesignManager) AskQuestions(rc fwmanager.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, addressee string, questions []projectdesign.AnchoredComment) error {
	return logInfraError(m.log, "ProjectDesign.AskQuestions", string(projectID), m.inner.AskQuestions(rc, projectID, kind, addressee, questions))
}

func (m loggingProjectDesignManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, note string) error {
	return logInfraError(m.log, "ProjectDesign.AcknowledgeStaleBasis", string(projectID), m.inner.AcknowledgeStaleBasis(rc, projectID, kind, note))
}

func (m loggingProjectDesignManager) SubmitReviewDecision(rc fwmanager.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, decision projectdesign.ReviewDecision, feedback *projectdesign.ReviewFeedback) error {
	return logInfraError(m.log, "ProjectDesign.SubmitReviewDecision", string(projectID), m.inner.SubmitReviewDecision(rc, projectID, kind, decision, feedback))
}

func (m loggingProjectDesignManager) SubmitSDPDecision(rc fwmanager.Context, projectID projectdesign.ProjectID, decision projectdesign.SDPDecision, optionID *projectdesign.OptionID, feedback *projectdesign.ReviewFeedback) error {
	return logInfraError(m.log, "ProjectDesign.SubmitSDPDecision", string(projectID), m.inner.SubmitSDPDecision(rc, projectID, decision, optionID, feedback))
}

// --- Construction ----------------------------------------------------------

type loggingConstructionManager struct {
	inner construction.ConstructionManager
	log   *slog.Logger
}

func (m loggingConstructionManager) ExecuteNextActivity(rc fwmanager.Context, projectID construction.ProjectID, tickID string) (construction.PumpResult, error) {
	v, err := m.inner.ExecuteNextActivity(rc, projectID, tickID)
	return v, logInfraError(m.log, "Construction.ExecuteNextActivity", string(projectID), err)
}

func (m loggingConstructionManager) GetEpisodeTimeline(rc fwmanager.Context, projectID construction.ProjectID, episodeID string) (construction.EpisodeTimeline, error) {
	v, err := m.inner.GetEpisodeTimeline(rc, projectID, episodeID)
	return v, logInfraError(m.log, "Construction.GetEpisodeTimeline", string(projectID), err)
}

func (m loggingConstructionManager) GetSessionState(rc fwmanager.Context, projectID construction.ProjectID, activityID *construction.ActivityID) (construction.ConstructionSessionView, error) {
	v, err := m.inner.GetSessionState(rc, projectID, activityID)
	return v, logInfraError(m.log, "Construction.GetSessionState", string(projectID), err)
}

func (m loggingConstructionManager) ListEpisodesForActivity(rc fwmanager.Context, projectID construction.ProjectID, activityID string) ([]construction.EpisodeRecordView, error) {
	v, err := m.inner.ListEpisodesForActivity(rc, projectID, activityID)
	return v, logInfraError(m.log, "Construction.ListEpisodesForActivity", string(projectID), err)
}

func (m loggingConstructionManager) OverrideActivity(rc fwmanager.Context, projectID construction.ProjectID, activityID construction.ActivityID, override construction.ActivityOverride) error {
	return logInfraError(m.log, "Construction.OverrideActivity", string(projectID), m.inner.OverrideActivity(rc, projectID, activityID, override))
}

func (m loggingConstructionManager) PauseProject(rc fwmanager.Context, projectID construction.ProjectID, reason string) error {
	return logInfraError(m.log, "Construction.PauseProject", string(projectID), m.inner.PauseProject(rc, projectID, reason))
}

func (m loggingConstructionManager) RunReplanSweep(rc fwmanager.Context, projectID *construction.ProjectID, tickID string) (construction.ReplanSweepResult, error) {
	// A cross-project sweep addresses no single project when projectID is nil.
	scope := ""
	if projectID != nil {
		scope = string(*projectID)
	}
	v, err := m.inner.RunReplanSweep(rc, projectID, tickID)
	return v, logInfraError(m.log, "Construction.RunReplanSweep", scope, err)
}

func (m loggingConstructionManager) SetReviewPolicy(rc fwmanager.Context, projectID construction.ProjectID, preset string) error {
	return logInfraError(m.log, "Construction.SetReviewPolicy", string(projectID), m.inner.SetReviewPolicy(rc, projectID, preset))
}

func (m loggingConstructionManager) SubmitPhaseDecision(rc fwmanager.Context, projectID construction.ProjectID, activityID construction.ActivityID, phase string, decision construction.PhaseDecision, feedback *construction.ReviewFeedback) error {
	return logInfraError(m.log, "Construction.SubmitPhaseDecision", string(projectID), m.inner.SubmitPhaseDecision(rc, projectID, activityID, phase, decision, feedback))
}

func (m loggingConstructionManager) UpdateReviewPolicy(rc fwmanager.Context, projectID construction.ProjectID, policy construction.ReviewPolicyInput) error {
	return logInfraError(m.log, "Construction.UpdateReviewPolicy", string(projectID), m.inner.UpdateReviewPolicy(rc, projectID, policy))
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
