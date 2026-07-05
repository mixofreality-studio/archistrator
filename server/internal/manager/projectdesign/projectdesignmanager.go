package projectdesign

import (
	"context"
	"errors"
	"fmt"
	"strings"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billing "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
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
	settlement   billing.BillingEngine
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
	settle billing.BillingEngine,
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
// review is assembled via RequestSDPCommit, not co-authored). The spine-ordering gate
// (the requested kind's immediate Phase-2 predecessor must be Committed) is enforced
// here on head-state — the wire-side mirror of the SPA's Phase-2 buildSpine step lock —
// so a raw API/MCP caller cannot draft out of order (the CoAuthorPhase2ArtifactWorkflow
// itself never gated ordering; it drafts immediately). The first Phase-2 kind
// (planningAssumptions) has no Phase-2 predecessor.
// amendmentIndexFor returns the AMENDMENT index for a draft request against slot: the count
// of prior commits, used as the …-amend-N branch suffix and the "revision N" prompt framing,
// and the signal that gates the amendment path (fresh -amend-N branch, amendment prompt, and
// review-ledger SEED of the reopening feedback). It keys off THE AMENDMENT CONDITION — the
// slot is COMMITTED — NOT off any Revisions magnitude. A committed slot is an amendment even
// when its Revisions reads 0 (a slot committed BEFORE the Revisions field existed): the floor
// of 1 guarantees every committed slot yields an index >= 1, so the workflow's Amendment>0
// checks are a faithful proxy for "committed at request time." A non-committed slot
// (drafting/awaiting/rejected/withdrawn/none) returns 0 — the normal (non-amendment) path.
func amendmentIndexFor(slot projectstate.ArtifactSlot) int {
	if slot.Status != projectstate.ReviewCommitted {
		return 0
	}
	if slot.Revisions < 1 {
		return 1 // pre-field committed slot: grandfathered to revision 1
	}
	return int(slot.Revisions)
}

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

	// Spine-ordering gate (Phase-2 twin of the systemdesign Manager). A Phase-2 kind
	// may only be drafted once its immediate predecessor in the Phase-2 sequence is
	// Committed (the same order the SPA's PHASE2_ORDER locks by).
	if err := m.checkPhase2Predecessor(ctx, projectID, kind); err != nil {
		return "", err
	}

	// F38 BACK-EDGE / AMENDMENT (Phase-2 twin). A draft request on an already-COMMITTED
	// Phase-2 artifact is the legal amendment path: fresh session on a …-amend-N branch
	// (N = the slot's prior commit count) with the reopening feedback seeded into its ledger.
	// A non-committed slot keeps today's behavior (active session redraft / fresh draft).
	amendment := 0
	if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
		amendment = amendmentIndexFor(slotFor(proj, toPSKind(kind)))
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := coAuthorInput{ProjectID: projectID, ArtifactKind: kind, Feedback: feedback, Amendment: amendment}

	// F47: DELIVER the feedback via the redraft SIGNAL, not a bare ExecuteWorkflow. A draft
	// request against an ALREADY-RUNNING session (the retry-at-failed-gate path — the session
	// is suspended at StageDraftFailed awaiting a decision) resolves USE_EXISTING to the running
	// run; a plain ExecuteWorkflow returns that handle WITHOUT delivering `in`, so the request's
	// feedback was silently DROPPED and the redraft repeated the same mistake. SignalWithStart
	// delivers the redraft signal (carrying the feedback) to the running session's gate AND, when
	// no run is live (fresh start / amendment on a committed→closed slot), starts a new run with
	// `in` (whose Feedback the spine seeds into the first prompt). This mirrors the systemdesign
	// Manager. The gate MERGES the signal feedback with any retained feedback (request wins).
	we, err := m.client.SignalWithStartWorkflow(ctx, wfID, signalRedraft, redraftSignal{Feedback: feedback}, opts, executionKindCoAuthor, in)
	if err != nil {
		return "", mapStartError(err)
	}
	return newSessionRef(we.GetID()), nil
}

// checkPhase2Predecessor enforces the Phase-2 spine-ordering gate for a draft request:
// the requested kind's immediate predecessor (per phase2PredecessorKind) must be
// Committed on head-state. Returns nil when the gate is satisfied — the first Phase-2
// kind (planningAssumptions) has no predecessor, so it always passes without a read,
// mirroring the SPA which unlocks planningAssumptions without a sealed Phase 1; a
// redraft of an already in-review / Committed kind also passes (its predecessor is
// committed by construction). Returns FailedPrecondition naming the uncommitted
// predecessor otherwise. Extracted so the gate is unit-testable without a Temporal
// client. Only checks the Phase-2 order (slots 8..16); Phase-1 sealing is the
// Phase2AdvanceWorkflow's concern.
func (m *projectDesignManager) checkPhase2Predecessor(ctx context.Context, projectID ProjectID, kind ArtifactKind) error {
	pred, ok := phase2PredecessorKind(kind)
	if !ok {
		return nil
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRAReadNotFound(err) {
			// A brand-new project with no head-state row: no slot is committed, so
			// the predecessor is by definition uncommitted.
			return newError(fwmanager.FailedPrecondition, predecessorNotCommittedMsg(pred))
		}
		return mapReadProjectError(err)
	}
	if slotFor(proj, toPSKind(pred)).Status != projectstate.ReviewCommitted {
		return newError(fwmanager.FailedPrecondition, predecessorNotCommittedMsg(pred))
	}
	return nil
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
	case SDPDecisionUnknown:
		// The zero value: a caller that forgot to set Decision, not a legitimate
		// SDP outcome. Reject explicitly rather than falling through silently.
		return newError(fwmanager.ContractMisuse, "unknown SDP decision")
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
	case ReviewDecisionUnknown:
		// The zero value: a caller that forgot to set Decision, not a legitimate
		// review outcome. Reject explicitly rather than falling through silently.
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	default:
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}

	wfID := coAuthorWorkflowID(projectID, kind)

	// F19: precondition — inspect the live session stage BEFORE signaling. A bare
	// SignalWorkflow is fire-and-forget: an approve/reject delivered while the session
	// is drafting, already committed, or was never started is silently BUFFERED or
	// dropped by the workflow (at the failed-recovery gate ReviewApprove is explicitly
	// ignored), yet the op returns success {} — a no-op masquerading as a decision.
	// Query the stage first and refuse a decision the current gate cannot honor with a
	// FailedPrecondition naming the actual stage. (Mirrors systemdesign's F19 fix.)
	view, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	if perr := checkReviewPrecondition(decision, view.Stage); perr != nil {
		return perr
	}
	// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open —
	// the reviewer must address (redraft) or waive each first. The message lists the open ids.
	if decision == ReviewApprove {
		if open := openReviewCommentViewIDs(view.ReviewThread); len(open) > 0 {
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot approve: %d review comment(s) still open (%s) — address or waive them first", len(open), strings.Join(open, ", ")))
		}
	}

	sig := reviewDecisionSignal{Decision: decision, Feedback: feedback}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalReviewDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// reviewGateView returns the session's full gate view (stage + durable review thread) for
// the F19 review precondition AND the review-ledger approve/waive preconditions. A missing
// execution reports SessionStageUnknown; a live run is read from the authoritative
// sessionState query.
func (m *projectDesignManager) reviewGateView(ctx context.Context, wfID string) (SessionStateView, error) {
	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		if isNotFound(err) {
			return SessionStateView{Stage: SessionStageUnknown}, nil
		}
		return SessionStateView{}, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, nil
}

// SetReviewCommentStatus applies a human status transition to one durable review-ledger
// comment (review-ledger §4): waive an OPEN comment to dismiss it, or reopen an ADDRESSED
// comment to send it back for another redraft. Mirrors SubmitReviewDecision's F19 shape — a
// synchronous precondition check via the sessionState query before signaling the (fire-and-
// forget) branch mutation.
func (m *projectDesignManager) SetReviewCommentStatus(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, commentID string, status string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) || kind == KindSdpReview {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a co-authored Phase-2 kind")
	}
	if commentID == "" {
		return newError(fwmanager.ContractMisuse, "empty commentId")
	}
	switch status {
	case projectstate.ReviewCommentWaived, projectstate.ReviewCommentOpen:
		// waive (open->waived) or reopen (addressed->open) — the only human-authored transitions.
	default:
		return newError(fwmanager.ContractMisuse, "status must be \"waived\" (to dismiss an open comment) or \"open\" (to reopen an addressed comment)")
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	view, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	if view.Stage != StageAwaitingReview {
		return newError(fwmanager.FailedPrecondition,
			"cannot change a review comment: the design is not awaiting review (current stage: "+sessionStageLabel(view.Stage)+")")
	}
	if perr := checkCommentTransition(view.ReviewThread, commentID, status); perr != nil {
		return perr
	}

	sig := setCommentStatusSignal{CommentID: commentID, Status: status}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalSetCommentStatus, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// openReviewCommentViewIDs returns the ids of every OPEN comment in a wire thread.
func openReviewCommentViewIDs(thread []ReviewCommentView) []string {
	var ids []string
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentOpen {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// checkCommentTransition validates a human status transition against the live thread: the
// comment must exist and the transition must be legal (open->waived, addressed->open).
func checkCommentTransition(thread []ReviewCommentView, id, status string) error {
	for _, c := range thread {
		if c.ID != id {
			continue
		}
		switch {
		case c.Status == projectstate.ReviewCommentOpen && status == projectstate.ReviewCommentWaived:
			return nil
		case c.Status == projectstate.ReviewCommentAddressed && status == projectstate.ReviewCommentOpen:
			return nil
		default:
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot change comment %s from %q to %q (allowed: open->waived, addressed->open)", id, c.Status, status))
		}
	}
	return newError(fwmanager.FailedPrecondition, "review comment "+id+" not found in the thread")
}

// checkReviewPrecondition enforces that the submitted decision is meaningful at the
// session's current stage (F19): approve is honored only at StageAwaitingReview;
// reject and withdraw are honored at StageAwaitingReview OR the StageDraftFailed
// recovery gate (where reject means retry-with-feedback — see awaitDraftFailedRecovery).
// Any other stage yields a FailedPrecondition naming the actual stage.
func checkReviewPrecondition(decision ReviewDecision, stage SessionStage) error {
	switch decision {
	case ReviewApprove:
		if stage != StageAwaitingReview {
			return newError(fwmanager.FailedPrecondition,
				"cannot approve: the design is not awaiting review (current stage: "+sessionStageLabel(stage)+")")
		}
	case ReviewReject:
		if stage != StageAwaitingReview && stage != StageDraftFailed {
			return newError(fwmanager.FailedPrecondition,
				"cannot send back: the design is not at a review or recovery gate (current stage: "+sessionStageLabel(stage)+")")
		}
	case ReviewWithdraw:
		if stage != StageAwaitingReview && stage != StageDraftFailed {
			return newError(fwmanager.FailedPrecondition,
				"cannot withdraw: no review or recovery gate is open (current stage: "+sessionStageLabel(stage)+")")
		}
	case ReviewDecisionUnknown:
		// Unreachable: SubmitReviewDecision rejects the zero value as ContractMisuse
		// before reaching the precondition. Guarded for switch-exhaustiveness.
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}
	return nil
}

// sessionStageLabel renders a SessionStage as a short human label for the precondition
// messages.
func sessionStageLabel(s SessionStage) string {
	switch s {
	case SessionStageUnknown:
		return "not started"
	case StageDrafting:
		return "drafting"
	case StageAssemblingSDP:
		return "assembling SDP"
	case StageAwaitingReview:
		return "awaiting review"
	case StageRedrafting:
		return "redrafting"
	case StageCommitted:
		return "committed"
	case StageWithdrawn:
		return "withdrawn"
	case StageRefused:
		return "refused"
	case StageDraftFailed:
		return "draft failed"
	default:
		return "unknown"
	}
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

	// F15/F28 + P0-2 (query-side defense, Phase-2 twin). A CoAuthor/SDP workflow answers the
	// sessionState Query by HISTORY-REPLAY even after it has CLOSED, returning its last in-
	// memory stage. For a run that died ABNORMALLY that replayed value lies "drafting in
	// progress" and wedges the SPA on an infinite "GENERATING" screen; for a run that closed
	// NORMALLY (COMPLETED) after committing (or withdrawing) it can ALSO be a stale mid-flight
	// StageDrafting — the same wedge on a SUCCESSFUL, long-committed artifact. Describe the
	// execution first: an abnormal-closed run synthesizes an honest StageDraftFailed view; a
	// COMPLETED run is rebuilt from the durable slot on main (committed slot → StageCommitted +
	// the committed model; any other terminal → honest terminal, never Drafting). A RUNNING /
	// CONTINUED_AS_NEW run (incl. an amendment's fresh run) falls through to the live query,
	// which is authoritative for those. A Describe error other than NotFound is best-effort:
	// fall through to the query rather than masking a transient Describe blip as a failure.
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		switch status := desc.GetWorkflowExecutionInfo().GetStatus(); {
		case isAbnormalClosedStatus(status):
			return failedSessionView(projectID, kind, status), nil
		case status == enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
			return m.completedSessionView(ctx, projectID, kind)
		}
	} else if isNotFound(derr) {
		return SessionStateView{}, newError(fwmanager.NotFound, "project design has not started for this project")
	}

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		// F20 (error altitude): before Phase 2 the co-author/SDP workflow does not
		// exist, and Temporal's raw "workflow not found for ID: <proj>:<n>" leaks the
		// internal execution id to the client. Map that to a clean, user-altitude
		// NotFound; other query faults keep their generic mapping.
		if isNotFound(err) {
			return SessionStateView{}, newError(fwmanager.NotFound, "project design has not started for this project")
		}
		return SessionStateView{}, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, nil
}

// isAbnormalClosedStatus reports whether a workflow-execution status is a CLOSED-ABNORMAL
// terminal state — the session died without a clean commit/withdraw. A normally COMPLETED
// or still-RUNNING (or CONTINUED_AS_NEW) execution is NOT abnormal.
func isAbnormalClosedStatus(s enumspb.WorkflowExecutionStatus) bool {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return true
	default:
		return false
	}
}

// failedSessionView synthesizes the human-visible failed view for a session whose workflow
// died abnormally. It reuses StageDraftFailed — the SAME terminal-failure stage the live
// anti-wedge gate uses — so the SPA renders its existing "design job failed → retry / withdraw"
// card. Carries a neutral human FailureReason.
func failedSessionView(projectID ProjectID, kind ArtifactKind, status enumspb.WorkflowExecutionStatus) SessionStateView {
	reason := terminatedSessionReason(status)
	return SessionStateView{
		ProjectID:     projectID,
		ArtifactKind:  kind,
		Stage:         StageDraftFailed,
		Draft:         DraftModel{Kind: artifactKindWireName(kind)},
		FailureReason: &reason,
	}
}

// completedSessionView derives the honest session view for a CoAuthor/SDP run that closed
// NORMALLY (COMPLETED). The replayed sessionState query is NOT trusted for such a run (it can
// return a stale mid-flight stage — the P0-2 "GENERATING forever" wedge on an already-committed
// artifact), so the view is rebuilt from the DURABLE slot on main.
func (m *projectDesignManager) completedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return SessionStateView{}, mapReadProjectError(err)
	}
	return committedSessionView(projectID, kind, slotFor(proj, toPSKind(kind)))
}

// committedSessionView projects the durable slot of a COMPLETED session onto a
// SessionStateView. A committed slot renders the committed view (StageCommitted + the committed
// model + the durable review thread). A withdrawn slot renders StageWithdrawn. Any other
// terminal-but-uncommitted state renders an honest StageDraftFailed terminal carrying a neutral
// reason — NEVER StageDrafting, so the SPA never wedges on an infinite "GENERATING" spinner.
func committedSessionView(projectID ProjectID, kind ArtifactKind, slot projectstate.ArtifactSlot) (SessionStateView, error) {
	switch slot.Status {
	case projectstate.ReviewCommitted:
		draft, err := draftModelFor(kind, slot.Model)
		if err != nil {
			return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
		}
		return SessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        StageCommitted,
			Draft:        draft,
			ReviewThread: reviewThreadToView(slot.ReviewThread),
		}, nil
	case projectstate.ReviewWithdrawn:
		return SessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        StageWithdrawn,
			Draft:        DraftModel{Kind: artifactKindWireName(kind)},
		}, nil
	default:
		reason := "the design session ended without committing an artifact. Retry to start a fresh draft."
		return SessionStateView{
			ProjectID:     projectID,
			ArtifactKind:  kind,
			Stage:         StageDraftFailed,
			Draft:         DraftModel{Kind: artifactKindWireName(kind)},
			FailureReason: &reason,
		}, nil
	}
}

// terminatedSessionReason renders the neutral human "why" for a session whose workflow died
// abnormally.
func terminatedSessionReason(status enumspb.WorkflowExecutionStatus) string {
	return "the design session ended unexpectedly and is no longer running (" + workflowStatusLabel(status) + "). Retry to start a fresh draft."
}

// workflowStatusLabel maps an abnormal-closed status to a short, infrastructure-neutral label
// for the failed card.
func workflowStatusLabel(s enumspb.WorkflowExecutionStatus) string {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "the job failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "the job timed out"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "the job was terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "the job was canceled"
	default:
		return "the job stopped"
	}
}

// --- error mapping at the façade boundary -----------------------------------

// isRAReadNotFound reports whether err is a RAW projectStateAccess fwra.NotFound
// (a brand-new / unknown project) returned DIRECTLY on the sync façade read path —
// distinct from workflow.go's isReadNotFound, which inspects the Temporal-wrapped
// ApplicationError on the replayed Activity path.
func isRAReadNotFound(err error) bool {
	var raErr *fwra.Error
	return errors.As(err, &raErr) && raErr.Kind == fwra.NotFound
}

// mapReadProjectError converts a projectStateAccess.ReadProject error on the sync
// spine-ordering-gate read path into a fwmanager.Error: fwra.NotFound → NotFound
// (unknown project), everything else → Infrastructure. (fwra.NotFound is handled
// specially by the RequestArtifactDraft caller as an uncommitted predecessor; this
// mapper covers the non-NotFound faults.)
func mapReadProjectError(err error) error {
	if isRAReadNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

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
