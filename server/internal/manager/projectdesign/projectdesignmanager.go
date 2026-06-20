package projectdesign

import (
	"context"
	"errors"
	"strings"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
	fwmanager "github.com/davidmarne/archistrator-platform/framework-go/manager"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

// Manager is the projectDesignManager façade. It exposes the public use-case ops
// (projectDesignManager.md §2) and OWNS Temporal. It is the Phase-2 twin of the
// systemdesign Manager. The Temporal-backed ops:
//   - RequestArtifactDraft   — Workflow (entry, per-artifact CoAuthorPhase2ArtifactWorkflow)
//   - RequestSDPCommit       — Workflow (entry, AssembleSDPReviewWorkflow)
//   - SubmitSDPDecision      — Signal (sdpDecision, to the SDP-review workflow)
//   - AdvanceToConstruction  — Workflow (entry, short-lived Phase-2 seal)
//   - GetSessionState        — Query (sessionState, read-only)
//
// plus SubmitReviewDecision — Signal (reviewDecision, the per-artifact OQ-3 gate).
//
// Pre-condition checks the contract puts on the façade (Phase-2 kind, non-empty
// projectId, Commit-requires-optionId, RejectAll-requires-feedback) are enforced
// here before any downstream call (§2, §3). The Manager holds only a Temporal
// client — the head-state and engine deps live on the worker-side Workflows struct.
type Manager struct {
	client client.Client
}

// NewManager constructs a Manager over an existing Temporal client.
func NewManager(c client.Client) *Manager { return &Manager{client: c} }

// RequestArtifactDraft — op 2.1. Temporal Workflow (entry; StartWorkflow /
// signal-with-start), workflow id {projectId}:{artifactKind}. Idempotent on the id.
//
// Pre: projectID non-nil; kind is a Phase-2 kind AND != KindSdpReview (the SDP
// review is assembled via RequestSDPCommit, not co-authored). The phase-prerequisite
// check (Phase 1 sealed, predecessors committed) is performed by the workflow on
// head-state; the façade only checks kind validity.
func (m *Manager) RequestArtifactDraft(ctx context.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (SessionRef, error) {
	if projectID == "" {
		return SessionRef{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !kind.IsPhase2() {
		return SessionRef{}, newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	if kind == projectstate.KindSdpReview {
		return SessionRef{}, newError(fwmanager.FailedPrecondition, "use requestSDPCommit for the SDP review")
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := CoAuthorInput{ProjectID: projectID, ArtifactKind: kind, Feedback: feedback}

	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindCoAuthor, in)
	if err != nil {
		return SessionRef{}, mapStartError(err)
	}
	return NewSessionRef(we.GetID()), nil
}

// RequestSDPCommit — op 2.2. Temporal Workflow (entry; StartWorkflow /
// signal-with-start), workflow id {projectId}:sdpReview. Idempotent on the id
// (UseExisting): a redundant start (or a replan re-entry) reuses the running
// SDP-review workflow.
func (m *Manager) RequestSDPCommit(ctx context.Context, projectID ProjectID) (SessionRef, error) {
	if projectID == "" {
		return SessionRef{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	wfID := sdpReviewWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := SDPReviewInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindSDPReview, in)
	if err != nil {
		return SessionRef{}, mapStartError(err)
	}
	return NewSessionRef(we.GetID()), nil
}

// SubmitSDPDecision — op 2.3. Temporal Signal (SignalWorkflow to workflow id
// {projectId}:sdpReview, signal sdpDecision).
//
// Validate: decision ∈ {SDPCommit, SDPRejectAll}; SDPCommit requires a non-empty
// optionID (ContractMisuse otherwise); SDPRejectAll requires feedback with
// non-empty Notes (ContractMisuse otherwise).
func (m *Manager) SubmitSDPDecision(ctx context.Context, projectID ProjectID, decision SDPDecision, optionID *OptionID, feedback *ReviewFeedback) error {
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
	sig := SDPDecisionSignal{Decision: decision, OptionID: optionID, Feedback: feedback}
	if err := m.client.SignalWorkflow(ctx, wfID, "", SignalSDPDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// SubmitReviewDecision — the per-artifact Phase-2 review gate (OQ-3). Temporal
// Signal (SignalWorkflow to workflow id {projectId}:{artifactKind}, signal
// reviewDecision). feedback required when decision == Reject. kind must be a
// Phase-2 kind other than the SDP review.
func (m *Manager) SubmitReviewDecision(ctx context.Context, projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !kind.IsPhase2() || kind == projectstate.KindSdpReview {
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
	sig := ReviewDecisionSignal{Decision: decision, Feedback: feedback}
	if err := m.client.SignalWorkflow(ctx, wfID, "", SignalReviewDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// AdvanceToConstruction — op 2.4. Temporal Workflow (entry; StartWorkflow,
// workflow id {projectId}:phaseAdvance). Returns the gating outcome.
func (m *Manager) AdvanceToConstruction(ctx context.Context, projectID ProjectID) (PhaseAdvanceResult, error) {
	if projectID == "" {
		return PhaseAdvanceResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	wfID := phaseAdvanceWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:        wfID,
		TaskQueue: TaskQueue,
	}
	in := PhaseAdvanceInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindPhaseAdvance, in)
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
func (m *Manager) GetSessionState(ctx context.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	if projectID == "" {
		return SessionStateView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	var wfID string
	if kind == projectstate.KindSdpReview {
		wfID = sdpReviewWorkflowID(projectID)
	} else {
		wfID = coAuthorWorkflowID(projectID, kind)
	}

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", QuerySessionState)
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
