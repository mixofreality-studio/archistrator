package projectdesign

import (
	"errors"
	"strings"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/settlement"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

// ProjectDesignManager is the generated service-contract interface for this component
// — the public use-case surface of the projectDesignManager façade
// (projectDesignManager.md §2). Each op leads with the Manager-layer call Context
// (fwmanager.Context, embedding context.Context + the Principal); the *projectDesignManager derives
// ctx := rc.Context inside. The concrete *projectDesignManager satisfies it; the consumer-side
// dependency seams (constructionPipelineAccess / sourceControlRail) + the Temporal
// workflows struct stay hand-written and are NOT part of this contract.

// Compile-time proof the concrete projectDesignManager satisfies the generated
// ProjectDesignManager port.
var _ ProjectDesignManager = (*projectDesignManager)(nil)

// projectDesignManager is the projectDesignManager façade. It exposes the public
// use-case ops (projectDesignManager.md §2) and OWNS Temporal. It is the Phase-2 twin
// of the systemdesign Manager. The Temporal-backed ops:
//   - RequestArtifactDraft   — Workflow (entry, per-artifact CoAuthorPhase2ArtifactWorkflow)
//   - RequestSDPCommit       — Workflow (entry, AssembleSDPReviewWorkflow)
//   - SubmitSDPDecision      — Signal (sdpDecision, to the SDP-review workflow)
//   - AdvanceToConstruction  — Workflow (entry, short-lived Phase-2 seal)
//   - GetSessionState        — Query (sessionState, read-only)
//
// plus SubmitReviewDecision — Signal (reviewDecision, the per-artifact OQ-3 gate).
//
// Each op leads with the Manager-layer call Context (fwmanager.Context, embedding
// context.Context + the Principal); the *projectDesignManager derives ctx :=
// rc.Context inside. Pre-condition checks the contract puts on the façade (Phase-2
// kind, non-empty projectId, Commit-requires-optionId, RejectAll-requires-feedback)
// are enforced here before any downstream call (§2, §3).
//
// The façade methods themselves use ONLY the Temporal client. It ALSO stores the
// Worker-side deps it was constructed with — the published
// projectstate.ProjectStateAccess (head-state read-back + thin writes), the published
// constructionpipeline.ConstructionPipelineAccess (Phase-2 design-job dispatch), the
// published sourcecontrol.SourceControlAccess (the PR rail), the three estimation
// Engines (the in-workflow SDP-assembly join), and the per-project repo resolver — so
// RegisterWorker can wire them (via the package's folded adapters) into the
// hand-written Temporal workflows. The former exported consumer-mirror interfaces +
// the composition-root adapters are RETIRED; the manager now depends on the deps'
// PUBLISHED interfaces and adapts them internally (Option-B boundary mapping).
type projectDesignManager struct {
	client       client.Client
	projectState projectstate.ProjectStateAccess
	pipeline     constructionpipeline.ConstructionPipelineAccess
	rail         sourcecontrol.SourceControlAccess
	estimator    estimation.EstimationEngine
	opEstimator  operationestimation.OperationEstimationEngine
	settlement   settlement.SettlementEngine
	repo         func(projectID ProjectID) (sourcecontrol.RepoRef, bool)
}

// newProjectDesignManager is the hand-written, unexported builder the generated
// NewProjectDesignManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only client; projectState /
// pipeline / rail / the three estimators / repo are stored for RegisterWorker (rail
// may be nil — a dev server with no source-control credentials runs the design spine
// repo-less).
func newProjectDesignManager(
	c client.Client,
	projectState projectstate.ProjectStateAccess,
	pipeline constructionpipeline.ConstructionPipelineAccess,
	rail sourcecontrol.SourceControlAccess,
	estimator estimation.EstimationEngine,
	opEstimator operationestimation.OperationEstimationEngine,
	settle settlement.SettlementEngine,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
) *projectDesignManager {
	return &projectDesignManager{
		client:       c,
		projectState: projectState,
		pipeline:     pipeline,
		rail:         rail,
		estimator:    estimator,
		opEstimator:  opEstimator,
		settlement:   settle,
		repo:         repo,
	}
}

// RequestArtifactDraft — op 2.1. Temporal Workflow (entry; StartWorkflow /
// signal-with-start), workflow id {projectId}:{artifactKind}. Idempotent on the id.
//
// Pre: projectID non-nil; kind is a Phase-2 kind AND != KindSdpReview (the SDP
// review is assembled via RequestSDPCommit, not co-authored). The phase-prerequisite
// check (Phase 1 sealed, predecessors committed) is performed by the workflow on
// head-state; the façade only checks kind validity.
func (m *projectDesignManager) RequestArtifactDraft(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) {
		return "", newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	if kind == KindSdpReview {
		return "", newError(fwmanager.FailedPrecondition, "use requestSDPCommit for the SDP review")
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := coAuthorInput{ProjectID: projectID, ArtifactKind: kind, Feedback: feedback}

	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindCoAuthor, in)
	if err != nil {
		return "", mapStartError(err)
	}
	return newSessionRef(we.GetID()), nil
}

// RequestSDPCommit — op 2.2. Temporal Workflow (entry; StartWorkflow /
// signal-with-start), workflow id {projectId}:sdpReview. Idempotent on the id
// (UseExisting): a redundant start (or a replan re-entry) reuses the running
// SDP-review workflow.
func (m *projectDesignManager) RequestSDPCommit(rc fwmanager.Context, projectID ProjectID) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}

	wfID := sdpReviewWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := sdpReviewInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindSDPReview, in)
	if err != nil {
		return "", mapStartError(err)
	}
	return newSessionRef(we.GetID()), nil
}

// SubmitSDPDecision — op 2.3. Temporal Signal (SignalWorkflow to workflow id
// {projectId}:sdpReview, signal sdpDecision).
//
// Validate: decision ∈ {SDPCommit, SDPRejectAll}; SDPCommit requires a non-empty
// optionID (ContractMisuse otherwise); SDPRejectAll requires feedback with
// non-empty Notes (ContractMisuse otherwise).
func (m *projectDesignManager) SubmitSDPDecision(rc fwmanager.Context, projectID ProjectID, decision SDPDecision, optionID *OptionID, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	switch decision {
	case SDPCommit:
		if optionID == nil || *optionID == "" {
			return newError(fwmanager.ContractMisuse, "Commit requires a non-empty optionId")
		}
	case SDPRejectAll:
		if feedback == nil || feedback.Notes == "" {
			return newError(fwmanager.ContractMisuse, "RejectAll requires feedback")
		}
	default:
		return newError(fwmanager.ContractMisuse, "unknown SDP decision")
	}

	wfID := sdpReviewWorkflowID(projectID)
	sig := sdpDecisionSignal{Decision: decision, OptionID: optionID, Feedback: feedback}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalSDPDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// SubmitReviewDecision — the per-artifact Phase-2 review gate (OQ-3). Temporal
// Signal (SignalWorkflow to workflow id {projectId}:{artifactKind}, signal
// reviewDecision). feedback required when decision == Reject. kind must be a
// Phase-2 kind other than the SDP review.
func (m *projectDesignManager) SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) || kind == KindSdpReview {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a co-authored Phase-2 kind")
	}
	switch decision {
	case ReviewApprove, ReviewWithdraw:
		// ok
	case ReviewReject:
		if feedback == nil || feedback.Notes == "" {
			return newError(fwmanager.ContractMisuse, "Reject requires feedback")
		}
	default:
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	sig := reviewDecisionSignal{Decision: decision, Feedback: feedback}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalReviewDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// AdvanceToConstruction — op 2.4. Temporal Workflow (entry; StartWorkflow,
// workflow id {projectId}:phaseAdvance). Returns the gating outcome.
func (m *projectDesignManager) AdvanceToConstruction(rc fwmanager.Context, projectID ProjectID) (PhaseAdvanceResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PhaseAdvanceResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	wfID := phaseAdvanceWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:        wfID,
		TaskQueue: TaskQueue,
	}
	in := phaseAdvanceInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindPhaseAdvance, in)
	if err != nil {
		return PhaseAdvanceResult{}, mapStartError(err)
	}

	var result PhaseAdvanceResult
	if err := we.Get(ctx, &result); err != nil {
		return PhaseAdvanceResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// GetSessionState — op 2.5. Temporal Query (QueryWorkflow, query sessionState,
// read-only). When kind == KindSdpReview, queries {projectId}:sdpReview; otherwise
// {projectId}:{kind}.
func (m *projectDesignManager) GetSessionState(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	ctx := rc.Context
	if projectID == "" {
		return SessionStateView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	var wfID string
	if kind == KindSdpReview {
		wfID = sdpReviewWorkflowID(projectID)
	} else {
		wfID = coAuthorWorkflowID(projectID, kind)
	}

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		return SessionStateView{}, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, nil
}

// --- error mapping at the façade boundary -----------------------------------

func mapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; the
	// SDK returns the existing handle without error. Any error here is treated as a
	// infrastructure fault.
	return newError(fwmanager.Infrastructure, err.Error())
}

func mapSignalError(err error) error {
	if isNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

func mapQueryError(err error) error {
	if isNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// isNotFound reports whether the Temporal error indicates the addressed execution
// does not exist.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errNotFoundSentinel) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "NotFound")
}

var errNotFoundSentinel = errors.New("not found")
