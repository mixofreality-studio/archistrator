package systemdesign

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

// SystemDesignManager is the generated service-contract interface for this component
// — the public use-case surface of the systemDesignManager façade
// (systemDesignManager.md §2). Each op leads with the Manager-layer call Context
// (fwmanager.Context, embedding context.Context + the Principal); the
// *systemDesignManager derives ctx := rc.Context inside. The concrete
// *systemDesignManager satisfies it; the internal dependency seams + the Temporal
// Workflows struct stay hand-written and are NOT part of this contract.

// Compile-time proof the concrete systemDesignManager satisfies the generated
// SystemDesignManager port. Each op leads with the Manager-layer call Context
// (fwmanager.Context); the *systemDesignManager derives ctx := rc.Context inside.
var _ SystemDesignManager = (*systemDesignManager)(nil)

// systemDesignManager is the systemDesignManager façade. It exposes the public
// use-case ops (systemDesignManager.md §2) and OWNS Temporal. The 2026-05-29 re-cut
// adds startSystemDesign (parent kickoff). The Temporal-backed ops:
//   - StartSystemDesign  — Workflow (entry, parent SystemDesignPhaseWorkflow)
//   - RequestArtifactDraft — Workflow (entry, child CoAuthorArtifactWorkflow gate)
//   - SubmitReviewDecision — Signal (reviewDecision, to the child gate)
//   - AdvancePhase         — Workflow (entry, short-lived seal)
//   - GetSessionState      — Query (sessionState, read-only)
//
// Rendering is no longer a Manager concern: server-side rendering was removed
// (the client renders typed models). The Manager exposes no RenderArtifact op and
// holds no RenderingEngine.
//
// The façade methods use only the Temporal client + projectStateAccess (for the
// StartSystemDesign ResearchInput precondition + the sync SetResearchInput write op).
// It ALSO stores the three Worker-side deps it was constructed with — the published
// constructionpipeline.ConstructionPipelineAccess (design-job dispatch), the published
// sourcecontrol.SourceControlAccess (the PR rail), and the per-project repo resolver —
// so RegisterWorker can wire them (via the package's folded adapters) into the
// hand-written Temporal Workflows. The former exported consumer-mirror interfaces +
// the composition-root adapters are RETIRED; the manager now depends on the deps'
// PUBLISHED interfaces and adapts them internally (Option-B boundary mapping).
//
// Pre-condition checks the contract puts on the façade (Phase-1 kind, non-empty
// projectId, Reject-requires-feedback, ResearchInput present) are enforced here before
// any downstream call (§2, §3).
type systemDesignManager struct {
	client       client.Client
	projectState projectstate.ProjectStateAccess
	pipeline     constructionpipeline.ConstructionPipelineAccess
	rail         sourcecontrol.SourceControlAccess
	repo         func(projectID ProjectID) (sourcecontrol.RepoRef, bool)
	// estimator + repoBase serve the folded CATALOG ops (CreateProject/GetProject/
	// ListProjects — the former projectManager). estimator is the
	// constructionEstimationEngine run at GetProject READ time (compute-at-read CPM +
	// EV/SPI); nil disables compute. repoBase composes each git row's prUrl
	// (<repoBase>/pull/<ref>); "" omits prUrl. The project's permanent identity is its
	// living system design, so these reads belong on this Manager.
	estimator estimation.EstimationEngine
	repoBase  string
}

// newSystemDesignManager is the hand-written, unexported builder the generated
// NewSystemDesignManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only client + projectState;
// pipeline/rail/repo are stored for RegisterWorker (rail may be nil — a dev server
// with no source-control credentials runs the design spine repo-less).
func newSystemDesignManager(c client.Client, ps projectstate.ProjectStateAccess, pipeline constructionpipeline.ConstructionPipelineAccess, rail sourcecontrol.SourceControlAccess, repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool), estimator estimation.EstimationEngine, repoBase string) *systemDesignManager {
	return &systemDesignManager{client: c, projectState: ps, pipeline: pipeline, rail: rail, repo: repo, estimator: estimator, repoBase: repoBase}
}

// StartSystemDesign — op 2.0 (2026-05-29). Temporal Workflow (entry;
// StartWorkflow, id {projectId}:systemDesign) starting the PARENT
// SystemDesignPhaseWorkflow, which drives the seven Phase-1 steps in fixed Method
// order, spawns the per-step child gate, auto-advances on each human Approve, and
// seals Phase 1.
//
// Pre-condition (systemDesignManager.md §2.0): the project exists and its
// ResearchInput slot is PRESENT (read via projectStateAccess.ReadProject) — else
// FailedPrecondition ("research not populated"). Idempotent on the id (a redundant
// start returns the running SessionRef). The ResearchInput is woven into the
// mission-draft prompt at step 1 (inside the child gate's draft step).
//
// SYNC from the Client's POV: returns once the parent start is durably accepted,
// not once Phase 1 completes (it spans days of human review; the SPA polls
// getSessionState / reads head-state).
func (m *systemDesignManager) StartSystemDesign(rc fwmanager.Context, projectID ProjectID) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}

	// Pre-condition: ResearchInput must be present. A brand-new project with no row
	// (fwra.NotFound) likewise fails the precondition — research has not been set.
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isResearchReadNotFound(err) {
			return "", newError(fwmanager.FailedPrecondition, "research not populated (project has no state)")
		}
		return "", mapReadProjectError(err)
	}
	if proj.Research.IsZero() {
		return "", newError(fwmanager.FailedPrecondition, "research not populated")
	}

	wfID := systemDesignPhaseWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindPhase, phaseInput{ProjectID: projectID})
	if err != nil {
		return "", mapStartError(err)
	}
	return newSessionRef(we.GetID()), nil
}

// isResearchReadNotFound reports whether a ReadProject error is the brand-new
// project NotFound (no row yet) — which, for StartSystemDesign, is itself a
// FailedPrecondition (research not set), not an infrastructure fault.
func isResearchReadNotFound(err error) bool {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		return raErr.Kind == fwra.NotFound
	}
	return false
}

// requestArtifactDraft — op 2.1. Temporal SIGNAL-WITH-START on workflow id
// {projectId}:{artifactKind}. This is BOTH the first-draft kickoff AND the
// "Retry draft" recovery lever:
//
//   - First request (no running session): starts the CoAuthorArtifactWorkflow,
//     which drafts immediately. The buffered redraft signal is harmless (the fresh
//     run does not await the refused gate before it drafts).
//   - Retry on a REFUSED session (Bug B; the session ended a draft attempt in the
//     queryable StageRefused state after a terminal worker fault): the redraft
//     signal is delivered to the still-live, suspended workflow, which re-enters the
//     draft loop in place — no new workflow run, the getSessionState Query stays
//     continuously available.
//   - Retry on an otherwise-running session: idempotent — the existing execution is
//     reused (UseExisting), and the redraft signal is consumed only if/when that
//     session is at the refused gate.
//
// Signal-with-start is the one call that covers all three (start-if-absent, signal
// the existing run otherwise), preserving the §2.1 idempotent-on-id post-condition.
//
// RequestArtifactDraft is the exported public op.
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

func (m *systemDesignManager) RequestArtifactDraft(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return "", newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
	}

	// Spine-ordering gate. The Phase-1 spine is strictly ordered
	// (mission → glossary → scrubbedRequirements → volatilities → coreUseCases →
	// system → operationalConcepts → standardCheck): a kind may only be drafted once
	// its immediate predecessor is Committed. The SPA locks steps this way client-side
	// (DesignExperience.buildSpine); the wire surface MUST enforce it too so a raw
	// API/MCP caller cannot draft out of order (systemDesignManager.md §2.1; STP-UC1-B1).
	if err := m.checkPhase1Predecessor(ctx, projectID, kind); err != nil {
		return "", err
	}

	// F38 BACK-EDGE / AMENDMENT (founder ruling 2026-07-05, fixes F37). A draft request on
	// an already-COMMITTED artifact is the LEGAL AMENDMENT path: it reopens the artifact and
	// starts a FRESH review session on a new …-amend-N branch (N = the slot's prior commit
	// count) with the committed model as the draft base and the reopening feedback seeded into
	// the new session's review ledger. On any NON-committed slot (drafting/awaiting-review/
	// rejected/withdrawn) amendment stays 0 and the behavior is exactly as before: an active
	// session consumes the redraft signal (USE_EXISTING); a withdrawn/failed slot starts a
	// fresh original draft. Because a committed slot's prior workflow run is CLOSED, the
	// SignalWithStart below starts a brand-new run (with this Amendment) rather than reusing it.
	amendment := 0
	if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
		amendment = amendmentIndexFor(slotFor(proj, kind))
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	opts := client.StartWorkflowOptions{
		ID:        wfID,
		TaskQueue: TaskQueue,
		// Idempotent on the id: a redundant start of an already-running session
		// reuses the existing execution rather than failing or duplicating
		// (systemDesignManager.md §2.1 post-condition). The signal rides along.
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := coAuthorInput{ProjectID: projectID, ArtifactKind: kind, Feedback: feedback, Amendment: amendment}

	we, err := m.client.SignalWithStartWorkflow(ctx, wfID, lSignalRedraft, redraftSignal{Feedback: feedback}, opts, executionKindCoAuthor, in)
	if err != nil {
		return "", mapStartError(err)
	}
	return newSessionRef(we.GetID()), nil
}

// checkPhase1Predecessor enforces the Phase-1 spine-ordering gate for a draft request:
// the requested kind's immediate predecessor (per phase1PredecessorKind, the same order
// the SPA's buildSpine locks by) must be Committed on head-state. Returns nil when the
// gate is satisfied — the first kind (mission) has no predecessor, so it always passes
// without a read; a redraft of an already in-review / Committed kind also passes because
// a kind only reaches review after its predecessor was Committed (the send-back /
// regenerate path is unaffected). Returns FailedPrecondition naming the uncommitted
// predecessor otherwise. Extracted so the gate is unit-testable without a Temporal
// client (RequestArtifactDraft calls it before the SignalWithStart).
func (m *systemDesignManager) checkPhase1Predecessor(ctx context.Context, projectID ProjectID, kind ArtifactKind) error {
	pred, ok := phase1PredecessorKind(kind)
	if !ok {
		return nil
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isResearchReadNotFound(err) {
			// A brand-new project with no head-state row: no slot is committed, so
			// the predecessor is by definition uncommitted.
			return newError(fwmanager.FailedPrecondition, predecessorNotCommittedMsg(pred))
		}
		return mapReadProjectError(err)
	}
	if slotFor(proj, pred).Status != projectstate.ReviewCommitted {
		return newError(fwmanager.FailedPrecondition, predecessorNotCommittedMsg(pred))
	}
	return nil
}

// submitReviewDecision — op 2.2. Temporal Signal (SignalWorkflow to workflow id
// {projectId}:{artifactKind}, signal reviewDecision). feedback required when
// decision == Reject.
//
// SubmitReviewDecision is the exported public op.
func (m *systemDesignManager) SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
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
	// ignored), yet the op returns success {} — a no-op masquerading as a decision that
	// wedges the reviewer. Query the stage first and refuse a decision the current gate
	// cannot honor with a FailedPrecondition naming the actual stage.
	view, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	if perr := checkReviewPrecondition(decision, view.Stage); perr != nil {
		return perr
	}
	// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open —
	// the reviewer must address (redraft) or waive each one first. The message lists the open ids.
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

// reviewGateView returns the session's full gate view (stage + the durable review thread)
// for the F19 review precondition AND the review-ledger approve/waive preconditions. Same
// dead-workflow defense as GetSessionState: a CLOSED-ABNORMAL run reports StageDraftFailed,
// a missing execution reports SessionStageUnknown, a live run is read from the authoritative
// sessionState query.
func (m *systemDesignManager) reviewGateView(ctx context.Context, wfID string) (SessionStateView, error) {
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		if status := desc.GetWorkflowExecutionInfo().GetStatus(); isAbnormalClosedStatus(status) {
			return SessionStateView{Stage: StageDraftFailed}, nil
		}
	} else if isNotFound(derr) {
		return SessionStateView{Stage: SessionStageUnknown}, nil
	}
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
// comment to send it back for another redraft. It mirrors SubmitReviewDecision's F19 shape —
// a synchronous precondition check via the sessionState query before signaling the (fire-and-
// forget) branch mutation, so a bad request fails loudly rather than silently no-op'ing.
func (m *systemDesignManager) SetReviewCommentStatus(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, commentID string, status string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
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

// openReviewCommentViewIDs returns the ids of every OPEN comment in a wire thread — the
// approve blocker set.
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
// comment must exist and the transition must be legal (open->waived, addressed->open). Any
// other case is a FailedPrecondition naming the reason (the durable RA verb re-checks, but a
// synchronous refusal is a better caller experience than a silently dropped signal).
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
// Any other stage — drafting, already committed/withdrawn/refused, or no session at all
// — yields a FailedPrecondition naming the actual stage.
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

// advancePhase — op 2.3. Temporal Workflow (entry; StartWorkflow, workflow id
// {projectId}:phaseAdvance). Returns the gating outcome.
//
// AdvancePhase is the exported public op.
func (m *systemDesignManager) AdvancePhase(rc fwmanager.Context, projectID ProjectID) (PhaseAdvanceResult, error) {
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

// getSessionState — op 2.4. Temporal Query (QueryWorkflow, query sessionState,
// read-only). Returns a point-in-time technical view without mutating state.
//
// GetSessionState is the exported public op.
func (m *systemDesignManager) GetSessionState(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	ctx := rc.Context
	if projectID == "" {
		return SessionStateView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	wfID := coAuthorWorkflowID(projectID, kind)

	// F15 gap 2a (query-side defense): a CoAuthorArtifactWorkflow that ended ABNORMALLY
	// (FAILED / TERMINATED / TIMED_OUT / CANCELED — e.g. an activity crashed the run)
	// STILL answers the sessionState Query by HISTORY-REPLAY, returning its last in-memory
	// stage (typically StageDrafting). That LIES that drafting is in progress and wedges the
	// SPA on an infinite "GENERATING" screen with no recovery. Describe the execution first;
	// when it is closed-ABNORMAL, synthesize an explicit StageDraftFailed view instead of
	// trusting the replayed query — supervision must reflect the real state.
	//
	// P0-2 (closed-COMPLETED case): a run that closed NORMALLY (COMPLETED) ALSO answers the
	// sessionState Query by history-replay, returning its LAST in-memory stage. For a session
	// that committed (or withdrew) and then completed, that replayed value can be a stale
	// mid-flight StageDrafting — the SAME "GENERATING · MISSION forever" wedge, but for a
	// SUCCESSFUL session whose artifact is long since committed on main. So a COMPLETED run is
	// ALSO not trusted for its stage: derive the honest view from the durable slot on main —
	// a committed slot renders the committed view (StageCommitted + the committed model), any
	// other terminal-but-uncommitted slot renders an honest terminal (never Drafting).
	//
	// A RUNNING (or CONTINUED_AS_NEW / an amendment's fresh run) execution falls through to the
	// live query, which is authoritative for those. A Describe error other than NotFound is
	// best-effort: fall through to the query rather than masking a transient Describe blip.
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		switch status := desc.GetWorkflowExecutionInfo().GetStatus(); {
		case isAbnormalClosedStatus(status):
			return failedSessionView(projectID, kind, status), nil
		case status == enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
			return m.completedSessionView(ctx, projectID, kind)
		}
	} else if isNotFound(derr) {
		return SessionStateView{}, newError(fwmanager.NotFound, derr.Error())
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
	case enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED,
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW,
		enumspb.WORKFLOW_EXECUTION_STATUS_PAUSED:
		return false
	default:
		return false
	}
}

// failedSessionView synthesizes the human-visible failed view for a session whose
// workflow died abnormally (see GetSessionState). It reuses StageDraftFailed — the SAME
// terminal-failure stage the live anti-wedge gate uses — so the SPA renders its existing
// "design job failed → retry / withdraw" card (Retry re-dispatches via signal-with-start,
// starting a fresh run). Carries a neutral human FailureReason; no run URL (the death was
// not a specific CI run).
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

// completedSessionView derives the honest session view for a CoAuthor run that closed
// NORMALLY (COMPLETED). The replayed sessionState query is NOT trusted for such a run
// (it can return a stale mid-flight stage — the P0-2 "GENERATING forever" wedge on an
// already-committed artifact), so the view is rebuilt from the DURABLE slot on main.
func (m *systemDesignManager) completedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error) {
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return SessionStateView{}, mapReadProjectError(err)
	}
	return committedSessionView(projectID, kind, slotFor(proj, kind))
}

// committedSessionView projects the durable slot of a COMPLETED session onto a
// SessionStateView. A committed slot renders the committed view (StageCommitted + the
// committed model + the durable review thread) — the same {kind, model} shape the SPA
// consumes for a live session. A withdrawn slot renders StageWithdrawn. Any other
// terminal-but-uncommitted state (the run completed without landing a commit) renders an
// honest StageDraftFailed terminal carrying a neutral reason — NEVER StageDrafting, so
// the SPA never wedges on an infinite "GENERATING" spinner for a dead session.
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

// terminatedSessionReason renders the neutral human "why" for a session whose workflow
// died abnormally.
func terminatedSessionReason(status enumspb.WorkflowExecutionStatus) string {
	return "the design session ended unexpectedly and is no longer running (" + workflowStatusLabel(status) + "). Retry to start a fresh draft."
}

// workflowStatusLabel maps an abnormal-closed status to a short, infrastructure-neutral
// label for the failed card.
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
	case enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED,
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING,
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW,
		enumspb.WORKFLOW_EXECUTION_STATUS_PAUSED:
		return "the job stopped"
	default:
		return "the job stopped"
	}
}

// SetResearchInput — op 2.6 (2026-05-30). SYNCHRONOUS, non-Temporal: it records
// the Phase-1 ResearchInput Method INPUT so a fresh project can satisfy the
// StartSystemDesign ResearchInput-present precondition through the UI. A single
// idempotent head-state write via projectStateAccess.SetResearchInput, with no
// Temporal primitive (no workflow, signal, gate, or slot transition).
//
// Body (systemDesignManager.md §2.6): read the current head Version via
// ReadProject, derive a stable idempotencyKey for "set research input on this
// project", and write. On the RA's fwra.Conflict (a concurrent writer bumped the
// version under us) re-read and re-apply on the sync path, bounded. There is NO
// workflow, signal, gate, or slot transition — ResearchInput is a Method INPUT,
// not a co-authored artifact (no AwaitingReview/Committed lifecycle).
//
// Returns the resulting head Version (the SPA may use it for optimistic display;
// the frozen surface is the write itself).
func (m *systemDesignManager) SetResearchInput(rc fwmanager.Context, projectID ProjectID, research ResearchInput) (Version, error) {
	ctx := rc.Context
	if projectID == "" {
		return 0, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if researchIsZero(research) {
		return 0, newError(fwmanager.ContractMisuse, "empty research (no sources)")
	}

	key := researchInputIdempotencyKey(projectID, research)
	psID := projectstate.ProjectID(projectID)
	psResearch := toPSResearch(research)

	// Sync-path optimistic-concurrency loop. The first write uses the head Version
	// just read; on a Conflict (a concurrent mutation bumped the row) re-read and
	// re-apply. Bounded so a pathological write-storm cannot spin forever.
	var lastErr error
	for attempt := 0; attempt < setResearchInputMaxAttempts; attempt++ {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return 0, mapReadProjectError(err)
		}

		newVersion, err := m.projectState.SetResearchInput(fwra.Context{Context: ctx, IdempotencyKey: key}, psID, proj.Version, psResearch)
		if err == nil {
			return Version(newVersion), nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue // re-read head Version, re-apply (same idempotencyKey)
		}
		return 0, mapSetResearchInputError(err)
	}
	return 0, fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "projectStateAccess.SetResearchInput: exhausted conflict retries")
}

// setResearchInputMaxAttempts bounds the sync-path re-read/re-apply loop.
const setResearchInputMaxAttempts = 5

// researchInputIdempotencyKey derives the stable logical idempotency key for
// "set research input on this project". Unlike the workflow Activities (which key
// by "${workflowId}:${activityId}"), this sync op has no Temporal context, so the
// key is derived from the project id plus a content fingerprint: a retried write
// of the SAME research collapses to a no-op in the RA dedup ledger, while a
// genuinely new research payload is a distinct logical mutation.
func researchInputIdempotencyKey(projectID ProjectID, research ResearchInput) fwra.IdempotencyKey {
	h := fnv.New64a()
	for _, s := range research.Sources {
		_, _ = h.Write([]byte(s.Title))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(s.Content))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:setResearchInput:%x", projectID, h.Sum64()))
}

// mapSetResearchInputError converts projectStateAccess SetResearchInput errors
// into fwmanager.Error on the sync write path. fwra.NotFound → NotFound (no
// project aggregate yet — the caller may need to open it first); fwra.ContractMisuse
// → ContractMisuse; everything else (incl. unrecovered Conflict) → Infrastructure
// with retryability preserved.
func mapSetResearchInputError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "Everything else (incl. unrecovered Conflict) → Infrastructure with
			// retryability preserved" per the doc comment above. These 8 kinds
			// carry no distinct handling on this sync write path: Auth/QuotaExhausted/
			// ContentPolicy are terminal-but-not-actionable-by-the-caller here, and
			// Conflict that reaches this far means the RA's own retry-on-conflict
			// loop gave up — surface it as Infrastructure so the caller's generic
			// retry policy applies, same as Transient/RateLimited/Unknown.
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.SetResearchInput")
			mapped.Retryable = raErr.Retryable
			return mapped
		default:
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.SetResearchInput")
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// isRAConflict reports whether err is the RA optimistic-concurrency conflict
// (fwra.Conflict) returned DIRECTLY on the sync path — the signal to re-read the
// head Version and re-apply. (Distinct from workflow.go's isConflict, which
// inspects the Temporal-wrapped ApplicationError on the replayed Activity path.)
func isRAConflict(err error) bool {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		return raErr.Kind == fwra.Conflict
	}
	return false
}

// mapReadProjectError converts projectStateAccess errors into fwmanager.Error
// for the sync read op. fwra.NotFound → NotFound (a brand-new / unknown project),
// other fwra.* errors → Infrastructure with the original retryability preserved.
func mapReadProjectError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// Same "everything else → Infrastructure" rationale as
			// mapSetResearchInputError above: no distinct handling for these
			// kinds on this sync read path.
			return fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.ReadProject")
		default:
			return fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.ReadProject")
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// --- error mapping at the façade boundary -----------------------------------

func mapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; the
	// SDK surfaces it as *serviceerror.WorkflowExecutionAlreadyStarted, but with
	// UseExisting the ExecuteWorkflow returns the existing handle without error.
	// Any error here is treated as a infrastructure fault.
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

// isNotFound reports whether the Temporal error indicates the addressed
// execution does not exist.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	// serviceerror.NotFound is the canonical "no such workflow" error; matched by
	// string to avoid a hard import of the api serviceerror package surface here.
	return errors.Is(err, errNotFoundSentinel) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "NotFound")
}

var errNotFoundSentinel = errors.New("not found")
