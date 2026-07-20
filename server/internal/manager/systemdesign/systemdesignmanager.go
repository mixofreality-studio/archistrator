// Package systemdesign is the systemDesignManager component of the aiarch
// server's Manager layer — the use-case façade that drives a project through
// Phase 1 of The Method (System Design), per the senior-passed contract
// designs/aiarch/implementation/contracts/systemDesignManager.md.
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it defines and registers one Activity
// per ResourceAccess call, owns the Signal/Query handlers, and derives the
// idempotency key "${workflowId}:${activityId}" passed down to each RA verb.
// Temporal lives ONLY in this component; the downstream ResourceAccess
// (resourceaccess/*) ports are Temporal-free.
//
// SCHEMA-FIRST (full encapsulation): this component OWNS its contract I/O types.
// The public surface (SystemDesignManager port + the I/O value types) is GENERATED
// into contract.gen.go from this component's `.serviceContracts` entry in
// .aiarch/state/project.json (edit that entry + `make gen`; do
// NOT hand-edit the generated surface). The generated contract imports NEITHER the
// projectstate ResourceAccess NOR Temporal: systemdesign mirrors the head-state
// value shapes (ProjectID / ArtifactKind / ResearchInput / Version) as its OWN
// named types and field-maps from projectstate at the Manager boundary (the
// project/construction/operations Manager precedent). The staged typed DRAFT is
// carried OPAQUELY — a {kind, model} envelope (DraftModel) — so systemdesign never
// regenerates or shares projectstate's sealed ArtifactModel sum or its 17 variants.
//
// The consumer-side dependency interfaces (ConstructionPipelineAccess /
// SourceControlRail), the Temporal Workflows struct + workflow inputs/signals, the
// PM-critique value types (Critique / CritiqueVerdict), and the behavior over the
// contract value types (behavior.go) stay HAND-WRITTEN and are NOT part of the
// generated contract.
//
// File layout within the package (the systemDesignManager component):
//   - systemdesignmanager.go : the Manager + the SystemDesignManager port (§6.2)
//   - contract.go            : the public façade types (§2, §3) — generated surface
//   - behavior.go            : free functions over the contract value types
//   - workflow.go            : the Workflows deps struct + workflow bodies + signal/query handlers (§6.3, §6.6)
//   - activities.go          : the Manager-owned Activity wrappers, as methods on Workflows (§6.4)
//   - errors.go              : the port-error -> Temporal-error translation (§6.4)
//   - worker.go              : worker registration of workflows + activities (§6.1)
package systemdesign

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

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

	// designSession is the generated designSessionAccess dep. Every branch-scoped design
	// flow (read-back, stage/commit/reject/withdraw, reconcile, the review-ledger branch
	// mutations) is reached through the generated wf.Acts.DesignSession* invoker surface,
	// backed by this dep (B10: the manager-local capability-fallback custom activities in
	// activities_custom.go/reviewledger.go/gitrail.go that used to duplicate this RA's
	// BranchAware/Ledger/Provenance/Reconciling type-assertion chains are deleted — this
	// Manager now has ZERO custom Temporal Activities).
	designSession projectstate.DesignSessionAccess
}

// newSystemDesignManager is the hand-written, unexported builder the generated
// NewSystemDesignManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only client + projectState;
// pipeline/rail/repo are stored for RegisterWorker (rail may be nil — a dev server
// with no source-control credentials runs the design spine repo-less).
func newSystemDesignManager(c client.Client, ps projectstate.ProjectStateAccess, pipeline constructionpipeline.ConstructionPipelineAccess, rail sourcecontrol.SourceControlAccess, repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool), estimator estimation.EstimationEngine, designSession projectstate.DesignSessionAccess, repoBase string) *systemDesignManager {
	return &systemDesignManager{client: c, projectState: ps, pipeline: pipeline, rail: rail, repo: repo, estimator: estimator, designSession: designSession, repoBase: repoBase}
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
		ID:        wfID,
		TaskQueue: TaskQueue,
		// A RUNNING phase is reused (idempotent start); a CLOSED phase — FAILED (the
		// 2026-07-16 incident: a child crash killed the rail pre-containment) or COMPLETED
		// (a step was withdrawn / a child failure was contained and the phase halted
		// gracefully) — is RESTARTED as a fresh run. The restarted run skips already-
		// committed steps (SystemDesignPhaseWorkflow's skip-committed gate) and resumes at
		// the first open step. ALLOW_DUPLICATE is the server default; pinned explicitly
		// because the restart-a-dead-phase recovery path depends on it.
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
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
//     which drafts immediately. The buffered ride-along redraft signal is harmless:
//     the fresh run does not await a recovery gate before it drafts, and the
//     failed-gate entry DRAIN discards it if the first draft fails (it must never
//     auto-consume the human gate — QA incident 2026-07-15).
//   - Retry on a REFUSED session (Bug B; the session ended a draft attempt in the
//     queryable StageRefused state after a terminal worker fault): the redraft
//     signal is delivered to the still-live, suspended workflow, which re-enters the
//     draft loop in place — no new workflow run, the getSessionState Query stays
//     continuously available.
//   - A session that is currently DRAFTING/REDRAFTING is NOT receptive: the request
//     is refused with FailedPrecondition (checkDraftRequestReceptive) — a signal
//     sent then would buffer and later stale-consume a recovery gate.
//
// Signal-with-start is the one call that covers all three (start-if-absent, signal
// the existing run otherwise), preserving the §2.1 idempotent-on-id post-condition.
//
// RequestArtifactDraft is the exported public op.
// amendmentIndexFor PROMOTED to projectstate.AmendmentIndexFor (code-health-phase-bd task
// D3) — byte-identical pure resolver, no longer duplicated with projectdesign's twin. It
// returns the AMENDMENT index for a draft request against slot: the count of prior
// commits, used as the …-amend-N branch suffix and the "revision N" prompt framing, and
// the signal that gates the amendment path (fresh -amend-N branch, amendment prompt, and
// review-ledger SEED of the reopening feedback).

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

	// GENERATING GUARD (QA incident 2026-07-15, gtdapp:1). While the session is DRAFTING /
	// REDRAFTING no gate consumes the redraft signal — SignalWithStart would BUFFER it in the
	// workflow's signal channel, where it later auto-satisfies the StageDraftFailed recovery
	// selector the instant that gate arms, silently skipping the human Retry/Withdraw decision
	// (observed live: a stale "Request draft" click queued during drafting consumed the failed
	// gate after a PM-critique flake, and an unwanted redraft round ran with nobody ever seeing
	// the failure). Refuse the request up front with a FailedPrecondition naming the stage.
	// Every other stage is receptive: AwaitingReview / DraftFailed have an open human gate, and
	// the terminal/no-session stages mean the SignalWithStart STARTS a fresh run (re-begin /
	// amendment semantics), whose gate-entry drain discards the start-path signal if unused.
	if err := m.checkDraftRequestReceptive(rc, projectID, kind); err != nil {
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
		amendment = projectstate.AmendmentIndexFor(slotFor(proj, kind))
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	opts := client.StartWorkflowOptions{
		ID:        wfID,
		TaskQueue: TaskQueue,
		// Idempotent on the id: a redundant start of an already-running session
		// reuses the existing execution rather than failing or duplicating
		// (systemDesignManager.md §2.1 post-condition). The signal rides along.
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		// REVIVAL (2026-07-16 incident): a session whose previous run CLOSED — normally
		// (committed/withdrawn → amendment/fresh draft) or ABNORMALLY (the run FAILED, as
		// gtdapp:1 did) — must be revivable: this SignalWithStart STARTS a brand-new run.
		// ALLOW_DUPLICATE is the server default; pinned explicitly because the dead-session
		// recovery path depends on it (a stricter policy silently turns "Retry design job"
		// into a no-op 200 — the observed false success).
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}
	in := coAuthorInput{ProjectID: projectID, ArtifactKind: kind, Feedback: feedback, Amendment: amendment}

	we, err := m.client.SignalWithStartWorkflow(ctx, wfID, lSignalRedraft, redraftSignal{Feedback: feedback}, opts, executionKindCoAuthor, in)
	if err != nil {
		return "", mapStartError(err)
	}
	// NO FALSE 200s (2026-07-16 incident): the founder's "Retry design job" against the dead
	// gtdapp:1 returned success while no run started. SignalWithStart's return alone cannot
	// distinguish "fresh run started" from "signal bound to something that will never act", so
	// VERIFY: the session's latest execution must now be live. Best-effort — only a confirmed
	// abnormal-closed latest run is refused (a Describe blip never masks a genuine start).
	if err := m.verifySessionRevived(ctx, wfID); err != nil {
		return "", err
	}
	return newSessionRef(we.GetID()), nil
}

// verifySessionRevived confirms the co-author session's LATEST execution is not sitting
// abnormally CLOSED right after a SignalWithStart — the honest-error backstop for the
// false-200 revival failure (see RequestArtifactDraft). Describe errors are ignored
// (best-effort verification; the start already durably succeeded).
func (m *systemDesignManager) verifySessionRevived(ctx context.Context, wfID string) error {
	desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, "")
	if derr != nil {
		return nil
	}
	if status := desc.GetWorkflowExecutionInfo().GetStatus(); isAbnormalClosedStatus(status) {
		return newError(fwmanager.Infrastructure,
			"the design session could not be revived — the previous session ended abnormally and no fresh run started; restart the phase (Start System Design) or try again")
	}
	return nil
}

// checkDraftRequestReceptive is the manager-side generating guard for RequestArtifactDraft
// (QA incident 2026-07-15): reject the request while the live session's stage is Drafting or
// Redrafting — a redraft signal sent then is consumable by NO open gate and would sit buffered
// until it stale-consumes a later recovery gate. The stage is read through GetSessionState —
// the SAME Describe-then-Query path the review-decision precondition (F19) and the SPA trust
// (a dead run synthesizes StageDraftFailed, a COMPLETED run is rebuilt from the durable slot,
// a live run answers the authoritative sessionState query) — so the refusal always agrees
// with what the founder sees on screen. NotFound (no session ever ran) is receptive: the
// request STARTS the first session. Purely a manager-side precondition — no workflow logic
// changes, so it is replay-safe by construction.
func (m *systemDesignManager) checkDraftRequestReceptive(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil {
		var me *fwmanager.Error
		if errors.As(err, &me) && me.Kind == fwmanager.NotFound {
			return nil // no session yet — this request starts one
		}
		return err
	}
	switch view.Stage {
	case StageDrafting, StageRedrafting:
		return newError(fwmanager.FailedPrecondition,
			"a draft is already generating for this artifact (currently "+sessionStageLabel(view.Stage)+") — wait for it to finish before requesting another")
	case SessionStageUnknown, StageAwaitingReview, StageCommitted, StageWithdrawn, StageRefused, StageDraftFailed:
		return nil
	default:
		return nil
	}
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
	view, live, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	if perr := checkReviewPrecondition(decision, view.Stage); perr != nil {
		return perr
	}
	// DEAD-SESSION HONESTY (2026-07-16 incident). An abnormally-CLOSED run synthesizes a
	// StageDraftFailed view (so the SPA renders the failed card), which PASSES the reject/
	// withdraw precondition above — but a signal to that corpse is refused by Temporal
	// ("workflow execution already completed") and pre-fix surfaced as 503 noise with zero
	// feedback. Refuse with an actionable FailedPrecondition instead: the ONLY lever on a
	// dead session is requestArtifactDraft ("Retry design job"), which starts a fresh run.
	// (Ordered AFTER the precondition so a never-started session keeps its "not started"
	// message — checkReviewPrecondition refuses every decision at SessionStageUnknown.)
	if !live {
		return newError(fwmanager.FailedPrecondition,
			"the design session for this artifact is no longer running (it ended abnormally) — review decisions cannot reach it. Use \"Retry design job\" to start a fresh session, then decide on its review gate")
	}
	// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open —
	// the reviewer must address (redraft) or waive each one first. The message lists the open ids.
	if decision == ReviewApprove {
		if open := openReviewCommentViewIDs(view.ReviewThread); len(open) > 0 {
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot approve: %d review comment(s) still open (%s) — address or waive them first", len(open), strings.Join(open, ", ")))
		}
	}

	// PM-P2-4: capture the acting reviewer identity here (the one place a security.Principal
	// reaches the review flow) and thread it through the signal so the eventual approve→commit
	// records it as the commit's approvedBy provenance.
	sig := reviewDecisionSignal{Decision: decision, Feedback: feedback, Approver: principalLabel(rc.Principal)}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalReviewDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// reviewGateView returns the session's full gate view (stage + the durable review thread)
// for the F19 review precondition AND the review-ledger approve/waive preconditions, plus
// whether a LIVE workflow can still honor a signal. Same dead-workflow defense as
// GetSessionState: a CLOSED-ABNORMAL run reports StageDraftFailed with live=false (a signal
// to it can never be honored — 2026-07-16 incident), a missing execution reports
// SessionStageUnknown, a live run is read from the authoritative sessionState query.
func (m *systemDesignManager) reviewGateView(ctx context.Context, wfID string) (SessionStateView, bool, error) {
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		if status := desc.GetWorkflowExecutionInfo().GetStatus(); isAbnormalClosedStatus(status) {
			return SessionStateView{Stage: StageDraftFailed}, false, nil
		}
	} else if isNotFound(derr) {
		return SessionStateView{Stage: SessionStageUnknown}, false, nil
	}
	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		if isNotFound(err) {
			return SessionStateView{Stage: SessionStageUnknown}, false, nil
		}
		return SessionStateView{}, false, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, false, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, true, nil
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
	view, live, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	// A dead (abnormally-closed) session synthesizes StageDraftFailed and a never-started
	// one SessionStageUnknown — both refuse below (neither is AwaitingReview), so the
	// !live case needs no separate message here.
	if view.Stage != StageAwaitingReview || !live {
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

// openReviewCommentViewIDs returns the ids of every OPEN CHANGE-REQUEST in a wire thread —
// the approve blocker set. Open QUESTIONS are deliberately excluded: an unanswered question
// is a soft warning at the approve gate (surfaced via the SPA confirm-strip), never a hard
// block (question-comments §approve).
func openReviewCommentViewIDs(thread []ReviewCommentView) []string {
	var ids []string
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentOpen && c.Type != projectstate.ReviewCommentTypeQuestion {
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

// principalLabel renders a security.Principal as a short human-facing label for PM-P2-4
// provenance (approvedBy): the username (GitHub login / preferred_username), else email,
// else display name, else the opaque subject (dev-mode identity). Empty when no identity was
// resolved — the commit then records no approvedBy (absent provenance is allowed).
func principalLabel(p security.Principal) string {
	switch {
	case p.Username != "":
		return p.Username
	case p.Email != "":
		return p.Email
	case p.Name != "":
		return p.Name
	default:
		return p.Subject
	}
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
	}
	// Unreachable for the eight defined SessionStage values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "unknown"
}

// advancePhase — op 2.3. Temporal Workflow (entry; StartWorkflow, workflow id
// {projectId}:phaseAdvance). Returns the gating outcome.
//
// AdvancePhase is the exported public op.
//
// F55 STALE-SLOT GATE. A back-edge amendment (CommitArtifact staleness propagation) flags
// every downstream committed slot StaleBasis when an earlier slot is re-committed. Sealing the
// phase over a stale committed slot silently advances the project on a design whose basis has
// shifted (the observed failure: advanced to Phase 2 while scrubbedRequirements was stale). So
// before starting the seal workflow, refuse with FailedPrecondition naming the stale in-scope
// slots — UNLESS the caller explicitly acknowledges (acknowledgeStale) that it intends to
// advance over them. The message names the slots so a user/MCP consumer knows what to reconcile.
func (m *systemDesignManager) AdvancePhase(rc fwmanager.Context, projectID ProjectID, acknowledgeStale bool) (PhaseAdvanceResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PhaseAdvanceResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	// Pre-seal gates over the committed head-state (read once).
	if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
		// STD-FAIL-OPEN: a committed standard check that still carries a FAIL item means the
		// Phase-1 design gate is red; sealing over it would advance on an unmet standard. A
		// fail is NOT a staleness the caller can wave through, so this gate ignores
		// acknowledgeStale.
		if fails := standardCheckFailItems(proj); len(fails) > 0 {
			return PhaseAdvanceResult{}, newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot advance phase: the system-design standard check has %d failing item(s) (%s); resolve or waive them before sealing Phase 1.",
					len(fails), strings.Join(fails, "; ")))
		}
		// STALE-UNACKED (F55): refuse to seal over a stale committed slot unless the caller
		// explicitly acknowledges the staleness.
		if !acknowledgeStale {
			if stale := staleCommittedPhase1Kinds(proj); len(stale) > 0 {
				return PhaseAdvanceResult{}, newError(fwmanager.FailedPrecondition,
					fmt.Sprintf("cannot advance phase: %d committed artifact(s) are stale and must be reconciled first (%s). Re-run the design for each, or advance anyway by acknowledging the staleness.",
						len(stale), strings.Join(stale, ", ")))
			}
		}
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

// staleCommittedPhase1Kinds returns the wire names of every COMMITTED Phase-1 slot that
// carries StaleBasis (a back-edge amendment invalidated its basis) — the set AdvancePhase must
// refuse to seal over unless the caller acknowledges. Order follows the canonical Phase-1
// spine so the message reads deterministically. A non-committed slot is never "stale" here (it
// isn't part of the seal), so only committed slots are inspected.
func staleCommittedPhase1Kinds(proj projectstate.Project) []string {
	var stale []string
	for _, kind := range phase1RequiredKinds() {
		slot := slotFor(proj, kind)
		if slot.Status == projectstate.ReviewCommitted && slot.StaleBasis {
			label := artifactKindWireName(kind)
			// STALE-UNACKED cause thread: name WHAT shifted the basis when the amendment
			// recorded it (absent for slots that went stale before the cause field existed).
			if c := slot.StaleBasisCause; c != nil {
				label = fmt.Sprintf("%s (basis changed by %s rev %d)", label, c.UpstreamKind, c.UpstreamRevision)
			}
			stale = append(stale, label)
		}
	}
	return stale
}

// standardCheckFailItems returns a human label for every FAIL item in the COMMITTED
// standard-check slot (STD-FAIL-OPEN). Empty when the standard check is not committed or
// carries no fail item — Phase 1 may seal only when the gate is fail-free.
func standardCheckFailItems(proj projectstate.Project) []string {
	slot := slotFor(proj, KindStandardCheck)
	if slot.Status != projectstate.ReviewCommitted {
		return nil
	}
	sc, ok := slot.Model.(*projectstate.StandardCheck)
	if !ok || sc == nil {
		return nil
	}
	var fails []string
	for i, it := range sc.Items {
		if it.Status != projectstate.CheckFail {
			continue
		}
		label := strings.TrimSpace(it.Guideline)
		if label == "" {
			label = strings.TrimSpace(it.Section)
		}
		if label == "" {
			label = fmt.Sprintf("item %d", i+1)
		}
		fails = append(fails, label)
	}
	return fails
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
			return withStageName(failedSessionView(projectID, kind, status)), nil
		case status == enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
			view, err := m.completedSessionView(ctx, projectID, kind)
			if err != nil {
				return SessionStateView{}, err
			}
			return withStageName(view), nil
		}
	} else if isNotFound(derr) {
		return SessionStateView{}, noActiveSessionError(projectID)
	}

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		// F20 (error altitude): before a design session exists the CoAuthor workflow
		// does not exist, and Temporal's raw "workflow not found for ID: <proj>:<n>"
		// leaks the internal execution id to the client. Map that to a clean,
		// user-altitude NotFound; other query faults keep their generic mapping.
		if isNotFound(err) {
			return SessionStateView{}, noActiveSessionError(projectID)
		}
		return SessionStateView{}, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return withStageName(view), nil
}

// withStageName stamps the F72 human-readable StageName label alongside the bare Stage int
// on the public SessionStateView, using sessionStageLabel as the single authoritative map.
// Applied at the GetSessionState boundary so every wire consumer (web + MCP) sees the label
// regardless of which internal path built the view. The Stage int (whose enum values DIFFER
// across managers) is unchanged; StageName is purely additive.
func withStageName(v SessionStateView) SessionStateView {
	v.StageName = sessionStageLabel(v.Stage)
	return v
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
	case projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected:
		// Any non-committed / non-withdrawn terminal status renders the honest
		// StageDraftFailed view (never StageDrafting — the anti-wedge rule).
		fallthrough
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
	}
	// Unreachable for the nine defined enumspb.WorkflowExecutionStatus values above
	// (the exhaustive linter enforces that every real variant has its own case);
	// kept as a defensive fallback for an out-of-range ordinal (e.g. a future
	// Temporal SDK addition not yet triaged here).
	return "the job stopped"
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
	if problem := researchSourceProblem(research); problem != "" {
		return 0, newError(fwmanager.ContractMisuse, problem)
	}

	key := researchInputIdempotencyKey(projectID, research)
	psID := projectstate.ProjectID(projectID)
	psResearch := toPSResearch(research)

	// Sync-path optimistic-concurrency loop. The first write uses the head Version
	// just read; on a Conflict (a concurrent mutation bumped the row) re-read and
	// re-apply. Bounded so a pathological write-storm cannot spin forever.
	var lastErr error
	for range setResearchInputMaxAttempts {
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

// noActiveSessionError is the clean, user-altitude NotFound returned when no
// design session (CoAuthor workflow) exists for the project — the no-active-session
// read. It replaces Temporal's raw "workflow not found for ID: <proj>:<kind>" leak
// (which exposed the internal execution-id format) with a client-appropriate message.
func noActiveSessionError(projectID ProjectID) error {
	return newError(fwmanager.NotFound, fmt.Sprintf("no active design session for project %q", projectID))
}

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
	// A session whose workflow task is FAILING (e.g. a deploy-time non-determinism
	// fault being retried) rejects queries with the raw Temporal internals
	// "Unable to query workflow due to Workflow Task in failed state" — observed
	// live on gtdapp:5 during the managed-scaffold-sync versioning incident. Same
	// error-hygiene rule as the 065a9e7 not-found cleanup: clients get a clean,
	// actionable Detail; the raw cause stays in the server-side log line at the
	// call site.
	if strings.Contains(err.Error(), "Workflow Task in failed state") {
		return newError(fwmanager.Infrastructure,
			"design session state is temporarily unavailable — the session hit an internal fault and is being retried by the server; try again shortly")
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// isNotFound reports whether the Temporal error indicates the addressed
// execution does not exist — typed as *serviceerror.NotFound, the canonical
// "no such workflow" error the SDK returns.
//
// QA 2026-07-19 (poll-404 wizard reset): this used to substring-match "not
// found"/"NotFound" over ANY error, which classified *serviceerror.
// NamespaceNotFound ("Namespace default is not found" — the server talking to
// a wrong/foreign Temporal backend, observed live when the systemtests dev
// server took over the shared port) as the authoritative "no active design
// session" NotFound. The SPA trusts that 404 and resets the wizard, so a
// backend-identity fault destroyed client state. Only the typed
// execution-NotFound may claim session absence; everything else stays an
// Infrastructure fault the client tolerates.
func isNotFound(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound)
}

// AnchoredComment's JSONPath is OPAQUE guidance text the architect anchors a
// "send back" comment to in the typed artifact model — the server does not
// evaluate it.

// PhaseAdvanceResult is the gating outcome of AdvancePhase: a non-Advanced result
// is the NORMAL "you still owe artifacts X, Y" answer, not an error.

// DraftModel (the staged-draft envelope on SessionStateView) is IDENTICAL on the
// wire to the project ArtifactSlotModel envelope, so the SPA decodes a draft the
// same way regardless of which read produced it.

// StageDraftFailed is the human-visible, human-actionable stage the session lands
// in when the dispatched agentic DESIGN job reaches a TYPED terminal failure phase
// (PhaseFailed / PhaseCancelled). It carries the job's neutral Diagnostic in
// FailureReason. Surfaced by getSessionState so the SPA renders "your design job
// failed: <diagnostic> — retry or withdraw" and NEVER a perpetual StageDrafting
// spinner.

// ---------------------------------------------------------------------------
// PM-critique value types (systemDesignManager.md §3.6). OWNED by this Manager and
// used ONLY internally (the workflow / readBackCritique) — NOT part of the public
// port surface, so they stay hand-written and are NOT in the generated contract.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Façade error model (systemDesignManager.md §3.5). CALLER/PROGRAMMER errors at the
// façade boundary — distinct from the workflow's own failure handling.
// ---------------------------------------------------------------------------

func newError(kind fwmanager.Kind, detail string) *fwmanager.Error {
	return fwmanager.New(kind, detail)
}

// behavior.go holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract value (the canonical-name
// lookups that used to be methods on the projectstate enums, the opaque SessionRef
// constructor) lives here as a free function.
//
// systemdesign's OWN ArtifactKind mirrors projectstate.ArtifactKind ordinal-for-
// ordinal, so its behavior is derived by a meaning-preserving int conversion to the
// canonical projectstate type rather than re-implemented here.

// newSessionRef constructs a SessionRef from an infrastructure identity. Internal to
// the Manager; Clients only ever receive and echo SessionRefs.
func newSessionRef(opaque string) SessionRef { return SessionRef(opaque) }

// toPSKind converts systemdesign's OWN ArtifactKind to the canonical
// projectstate.ArtifactKind (ordinal-preserving) for behavior + RA-boundary calls.
func toPSKind(k ArtifactKind) projectstate.ArtifactKind { return projectstate.ArtifactKind(k) }

// artifactKindString returns the PascalCase Go-identifier name for an ArtifactKind
// (the dispatch-input + PR-title + diagnostic form). Mirrors projectstate String().
func artifactKindString(k ArtifactKind) string { return toPSKind(k).String() }

// artifactKindWireName returns the canonical camelCase wire name for an ArtifactKind.
func artifactKindWireName(k ArtifactKind) string { return toPSKind(k).WireName() }

// artifactKindIsPhase1 reports whether the kind belongs to The Method's Phase 1.
func artifactKindIsPhase1(k ArtifactKind) bool { return toPSKind(k).IsPhase1() }

// phase1RequiredKinds returns the ordered set of Phase-1 artifact kinds (systemdesign's
// OWN type), mirroring projectstate.Phase1RequiredKinds().
func phase1RequiredKinds() []ArtifactKind {
	ps := projectstate.Phase1RequiredKinds()
	out := make([]ArtifactKind, 0, len(ps))
	for _, k := range ps {
		out = append(out, ArtifactKind(k))
	}
	return out
}

// phase1PredecessorKind returns the Phase-1 kind that must be Committed immediately
// before `kind` may be drafted — the wire-side mirror of the SPA's buildSpine step
// lock (a step is locked until its immediate predecessor is committed). The first
// required kind (mission) has no predecessor and returns (_, false); a kind not in the
// Phase-1 set likewise returns (_, false) (the caller has already gated on IsPhase1).
func phase1PredecessorKind(kind ArtifactKind) (ArtifactKind, bool) {
	req := phase1RequiredKinds()
	for i, k := range req {
		if k == kind {
			if i == 0 {
				return 0, false
			}
			return req[i-1], true
		}
	}
	return 0, false
}

// predecessorNotCommittedMsg is the FailedPrecondition detail naming the uncommitted
// predecessor that blocks the requested draft (by its canonical camelCase wire name).
func predecessorNotCommittedMsg(pred ArtifactKind) string {
	return fmt.Sprintf("predecessor artifact %q must be committed before this kind can be drafted", artifactKindWireName(pred))
}

// strPtrOrNil maps a failure-reason string to the optional contract field: nil for
// the empty string (omitted on the wire), &s otherwise (the project notesPtr pattern).
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// researchIsZero reports whether the ResearchInput is unprovided (no Sources). The
// SetResearchInput pre-condition rejects a zero value.
func researchIsZero(r ResearchInput) bool { return len(r.Sources) == 0 }

// researchSourceProblem reports the first per-source shape violation in a
// non-empty ResearchInput as a clean, client-facing detail string naming the
// offending source by its 1-based position (e.g. `research source 2: title must
// not be empty`). It returns "" when every source carries a non-whitespace title
// AND non-whitespace content. The empty-corpus (no sources at all) case is handled
// separately by researchIsZero — this function assumes at least one source and
// validates the shape of each. Whitespace-only fields are treated as empty so a
// source cannot smuggle a blank title/content past the gate with a stray space.
func researchSourceProblem(r ResearchInput) string {
	for i, s := range r.Sources {
		pos := i + 1 // 1-based, client-facing
		if strings.TrimSpace(s.Title) == "" {
			return fmt.Sprintf("research source %d: title must not be empty", pos)
		}
		if strings.TrimSpace(s.Content) == "" {
			return fmt.Sprintf("research source %d: content must not be empty", pos)
		}
	}
	return ""
}

// toPSResearch converts the contract ResearchInput to projectstate.ResearchInput at
// the projectStateAccess boundary.
func toPSResearch(r ResearchInput) projectstate.ResearchInput {
	sources := make([]projectstate.ResearchSource, 0, len(r.Sources))
	for _, s := range r.Sources {
		sources = append(sources, projectstate.ResearchSource{Title: s.Title, Content: s.Content})
	}
	return projectstate.ResearchInput{Sources: sources}
}

// findings.go owns the SESSION-TRANSIENT validation-finding value types this Manager
// surfaces on its getSessionState read (SessionStateView.Findings). The SPA renders
// findings[] to explain "why it's being redrafted" (the PM-critique-unresolved
// warning is one). They are part of this component's OWN generated contract surface
// (registered in cmd/schemagen) — pure data, no methods.
//
// WIRE: severity is a camelCase STRING name ("info"|"warning"|"error") — a string
// enum keeps the generated type pure data (no custom MarshalJSON) while the wire
// form stays byte-identical for the SPA (f.severity === 'error' / 'warning').

// Severity is a finding severity. Only SeverityError fails a verdict; Warning/Info
// ride along advisory. The value IS its canonical camelCase wire name.

// RuleID is the stable, namespaced id of a validation rule. Stable across runs for
// finding-diff and worker-prompt continuity.

// Location locates a finding within a typed model. NO Line field: the input is a
// typed model, not bytes.

// stable position used for deterministic finding ordering
// human-readable locus, e.g. "core use case 3"

// Finding is a single machine-checkable rule violation surfaced to the SPA.

// human-readable; safe to weave into a redraft prompt; no PII
// optional; where in the model the finding sits

// modelEnvelope/projectEnvelope are ALIASES to the projectstate types (the shared
// wire codec lives in projectstate/envelope.go: EncodeModel/EncodeProject/Decode).
// Aliasing preserves type identity for every existing declaration/field/call site
// in this package; call the promoted methods by their exported names (Decode, not
// decode).
type (
	modelEnvelope   = projectstate.ModelEnvelope
	projectEnvelope = projectstate.ProjectEnvelope
)

// draftModelFor builds the OPAQUE public DraftModel envelope ({kind, model}) the
// session read carries the staged typed draft as. Kind is the artifactKind's canonical
// camelCase wire name (always set, so the SPA gets {"kind":"mission"} even before a
// draft is staged); Model is the concrete model's own JSON, omitted when nil. This is
// the public-surface twin of modelEnvelope (the Temporal/Activity carrier) — the same
// {kind, model} wire shape the SPA decodes, with Kind as a plain string so the
// generated contract carries no projectstate ArtifactKind.
func draftModelFor(kind ArtifactKind, model projectstate.ArtifactModel) (DraftModel, error) {
	env := DraftModel{Kind: artifactKindWireName(kind)}
	if model != nil {
		raw, err := json.Marshal(model)
		if err != nil {
			return DraftModel{}, fmt.Errorf("encode draft model %s: %w", model.Kind(), err)
		}
		rm := json.RawMessage(raw)
		env.Model = &rm
	}
	return env, nil
}

// encodeProject wraps the head-state aggregate for the Temporal boundary, delegating
// the shared slot/model codec to projectstate.EncodeProject and then OPTING IN to
// carrying the Research corpus pointer (projectstate/envelope.go doc: EncodeProject
// leaves Research nil by default — a plain struct field's `omitempty` would not
// suppress the key, so the promoted type uses a pointer and requires an explicit
// opt-in). The persisted corpus (F42) is a set of {Title, Path, ContentBytes}
// POINTERS — the book-sized Content lives as files at .aiarch/state/research/, NOT in
// this envelope — so it round-trips whole and stays inherently tiny (the QA F29
// titles-only slimming is now structural, not a special case). The mission-draft step
// reads Title + Path off it.
func encodeProject(p projectstate.Project) (projectEnvelope, error) {
	env, err := projectstate.EncodeProject(p)
	if err != nil {
		return projectEnvelope{}, err
	}
	env.Research = &p.Research
	return env, nil
}

// acknowledgestale.go implements the F45 per-slot staleness-acknowledge op: a reviewer marks
// a stale COMMITTED artifact "reviewed — unaffected", clearing its StaleBasis flag WITHOUT a
// redraft (which, for an unaffected artifact, would be a byte-identical no-op that dies at the
// no-change gate). The clear + a durable staleAck audit entry commit atomically on main.

const acknowledgeStaleMaxAttempts = 5

// AcknowledgeStaleBasis clears the committed slot's StaleBasis and records the reviewer's
// note as a staleAck audit entry. Synchronous OCC write (mirrors SetResearchInput).
func (m *systemDesignManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
	}
	// F-GTD-12: an acknowledge is a MAIN-branch write (the StaleBasis clear + the staleAck
	// entry commit on main). While a co-author session is LIVE for this slot — on a committed
	// slot that is by definition an in-flight AMENDMENT — that main write turns the session's
	// review PR merge-DIRTY, so the eventual approve's merge fails with a Conflict and the
	// workflow bounces back to AwaitingReview looking like a silent no-op to the reviewer.
	// Refuse up front: reconcile RIDES the amendment (its merge clears the staleness).
	if err := m.refuseAckDuringLiveSession(rc, projectID, kind); err != nil {
		return err
	}
	key := acknowledgeStaleIdempotencyKey(projectID, kind, note)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)

	var lastErr error
	for range acknowledgeStaleMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return mapReadProjectError(err)
		}
		_, err = m.projectState.AcknowledgeStaleBasis(fwra.Context{Context: ctx}, psID, proj.Version, psKind, note, key)
		if err == nil {
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapSetResearchInputError(err) // shares the ContractMisuse/NotFound/else mapping
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AcknowledgeStaleBasis: exhausted conflict retries")
}

// refuseAckDuringLiveSession is the F-GTD-12 guard (Phase-1 twin of the projectdesign
// impl): while the target kind has a LIVE co-author (amendment) session, the acknowledge
// is refused with a FailedPrecondition (the wire's 409/"failed_precondition" conflict
// shape). Liveness is read through GetSessionState — the SAME Describe-then-Query path
// the review gate and the SPA trust (a dead run synthesizes StageDraftFailed; a COMPLETED
// run is rebuilt from the durable slot) — so ack gating always agrees with what the
// reviewer sees on screen. A NotFound (no session ever ran for this slot) passes.
func (m *systemDesignManager) refuseAckDuringLiveSession(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil {
		var me *fwmanager.Error
		if errors.As(err, &me) && me.Kind == fwmanager.NotFound {
			return nil
		}
		return err
	}
	if !sessionStageIsLive(view.Stage) {
		return nil
	}
	return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
		"cannot mark this artifact reviewed: its amendment session is still open (currently %s). Reconcile rides the amendment — acknowledging now would commit to main and merge-conflict the amendment's review PR. Approve or withdraw the session first.",
		sessionStageLabel(view.Stage)))
}

// sessionStageIsLive reports whether a co-author session stage means the session still
// OWNS the slot (its branch/PR is open or recoverable): drafting / awaiting review /
// redrafting, plus the StageDraftFailed recovery gate (the session is suspended there
// with its branch and PR intact — a Retry resumes it). The terminal stages (committed /
// withdrawn / refused) and the unknown zero value are NOT live.
func sessionStageIsLive(s SessionStage) bool {
	switch s {
	case StageDrafting, StageAwaitingReview, StageRedrafting, StageDraftFailed:
		return true
	case SessionStageUnknown, StageCommitted, StageWithdrawn, StageRefused:
		return false
	default:
		return false
	}
}

func acknowledgeStaleIdempotencyKey(projectID ProjectID, kind ArtifactKind, note string) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(note))
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:ackStale:%x", projectID, int(kind), h.Sum64()))
}

// askquestions.go implements the question-comments op (founder-ratified 2026-07-05):
// AskQuestions appends one or more clarifying QUESTIONS to an artifact's review ledger
// WITHOUT sending the draft back for a redraft, and dispatches a lightweight ANSWER job so
// the addressed role (pm / architect) answers each in place via the aiarch-state MCP's
// respondToReviewComment. Unlike change-request comments, open questions do NOT block
// approve (they surface as a soft warning at the approve gate). It works on a COMMITTED
// artifact too — seeding a question-only thread on main without opening an amendment
// session — and on a live AwaitingReview session (appending on that session's branch).

// askQuestionsMaxAttempts bounds the sync-path OCC re-read/re-apply loop.
const askQuestionsMaxAttempts = 5

// AskQuestions — the question-comments op. Appends the given questions to the artifact's
// durable review ledger as type="question" entries addressed to `addressee`, then
// dispatches an answer job. Synchronous (no Temporal workflow): the append is the durable,
// user-visible effect; the answer job is best-effort (a dispatch miss leaves the questions
// recorded and unanswered, exactly as if the addressee has not answered yet).
//
// DISPATCH RECOVERY (F82): a dispatch MISS is now LOGGED LOUDLY server-side (it was
// previously discarded, and the construction-pipeline RA has no logger, so a miss vanished
// with zero operator signal). To RECOVER a dropped dispatch, simply CALL AskQuestions AGAIN
// with the same questions: the seed is idempotent on its content key, so NO ledger entry is
// duplicated (the existing entries' round is reused so the minted ids still match), while the
// answer-job dispatch RE-FIRES via a per-call-unique key.
func (m *systemDesignManager) AskQuestions(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, addressee string, questions []AnchoredComment) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
	}
	switch addressee {
	case projectstate.ReviewAddresseePM, projectstate.ReviewAddresseeArchitect:
		// ok
	default:
		return newError(fwmanager.ContractMisuse, "addressee must be \"pm\" or \"architect\"")
	}
	qs := questionsToLedger(addressee, questions)
	if len(qs) == 0 {
		return newError(fwmanager.ContractMisuse, "no questions to ask (every question needs text)")
	}

	// Resolve the branch the ledger lives on: a live drafting/review session keeps the
	// thread on its session branch; a committed (or absent) session keeps it on main ("").
	branch := m.resolveQuestionBranch(rc, projectID, kind)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := askQuestionsIdempotencyKey(projectID, kind, branch, qs)

	// Sync-path optimistic-concurrency loop (mirrors SetResearchInput): read the head
	// version on the resolved branch, compute a fresh question round from the live thread
	// so the minted ids never collide with prior entries, and append. Re-read on Conflict.
	var lastErr error
	for range askQuestionsMaxAttempts {
		proj, err := m.readProjectMaybeBranch(ctx, psID, branch)
		if err != nil {
			return mapReadProjectError(err)
		}
		thread := slotFor(proj, kind).ReviewThread
		round := nextQuestionRound(thread)
		if r, ok := existingQuestionRound(thread, qs); ok {
			// A prior ask already seeded these exact questions (its answer-job dispatch may
			// have been dropped — F82). Reuse their round so the minted ids match the EXISTING
			// ledger entries, and the re-fired answer job answers the right comments.
			round = r
		}
		_, err = m.projectState.SeedReviewCommentsOnBranch(fwra.Context{Context: ctx}, psID, proj.Version, branch, psKind, round, qs, key)
		if err == nil {
			// Best-effort dispatch of the answer job. A dispatch failure is logged by the
			// pipeline access; the questions are already durably recorded, so we do not fail
			// the op — the addressee can be re-prompted, and the SPA already shows the asks.
			// Stamp the deterministic minted ids onto a copy so the answer prompt can name
			// each question by the id the addressee must call respondToReviewComment with.
			minted := make([]projectstate.ReviewComment, len(qs))
			for i := range qs {
				minted[i] = qs[i]
				minted[i].ID = projectstate.ReviewCommentID(round, i)
			}
			m.dispatchAnswerJob(ctx, projectID, kind, branch, addressee, minted)
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapReadProjectError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AskQuestions: exhausted conflict retries")
}

// resolveQuestionBranch returns the branch the artifact's review ledger currently lives on:
// the session branch when a GENUINELY ACTIVE session exists (so questions land beside the
// draft under review), else "" (main) for a committed or session-less artifact. It is
// best-effort — any read/query miss falls back to main, the safe default.
//
// F73: an ACTIVE session means the co-author Temporal workflow is OPEN and in a non-terminal
// (live) stage. Resolution reuses the P0-2 Describe-first machinery via GetSessionState —
// NOT a bare sessionState Query. A bare query REPLAYS a CLOSED run's last in-memory stage,
// which for a completed/committed (or abandoned) amendment is a stale mid-flight LIVE stage.
// Trusting it wrongly resolved a DEAD amendment's leftover branch (e.g. .../2-amend-1) — and
// because amendmentIndexFor returns >=1 for any committed slot, that branch gets synthesized
// and the seeded questions land where nothing ever merges. GetSessionState synthesizes an
// honest terminal for every closed run (StageCommitted / StageWithdrawn / StageDraftFailed)
// and errors NotFound when there is no workflow — all of which fall back to main here.
func (m *systemDesignManager) resolveQuestionBranch(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) string {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil || !isLiveSessionStage(view.Stage) {
		return ""
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: rc.Context}, projectstate.ProjectID(projectID))
	if err != nil {
		return ""
	}
	return projectstate.DesignBranch(projectstate.ProjectID(projectID), toPSKind(kind), projectstate.AmendmentIndexFor(slotFor(proj, kind)))
}

// readProjectMaybeBranch reads the head-state aggregate from the given branch: the
// generated ProjectStateAccess contract is uniformly branch-aware post-C2-fold
// (branch=="" reads main exactly as ReadProject), so this is a direct forward.
func (m *systemDesignManager) readProjectMaybeBranch(ctx context.Context, psID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	return m.projectState.ReadProjectOnBranch(fwra.Context{Context: ctx}, psID, branch)
}

// isLiveSessionStage reports whether a co-author session is live (its ledger lives on the
// session branch, not main). SessionStageUnknown (no execution) and StageDraftFailed mean
// there is no live branch to append to → main.
func isLiveSessionStage(stage SessionStage) bool {
	switch stage {
	case StageDrafting, StageAwaitingReview, StageRedrafting, StageRefused:
		return true
	case SessionStageUnknown, StageCommitted, StageWithdrawn, StageDraftFailed:
		return false
	default:
		return false
	}
}

// questionsToLedger converts inbound anchored questions into the projectstate.ReviewComment
// shape the append verb stamps, marking each type="question" + addressee. An empty-text
// question is dropped (defensive). Id / round / open status / empty response are minted in
// appendReviewComments.
func questionsToLedger(addressee string, questions []AnchoredComment) []projectstate.ReviewComment {
	out := make([]projectstate.ReviewComment, 0, len(questions))
	for _, q := range questions {
		if strings.TrimSpace(q.Text) == "" {
			continue
		}
		out = append(out, projectstate.ReviewComment{
			Anchor:     q.JSONPath,
			AnchorText: q.AnchorText,
			Text:       q.Text,
			AuthorRole: reviewAuthorRole,
			Type:       projectstate.ReviewCommentTypeQuestion,
			Addressee:  addressee,
		})
	}
	return out
}

// nextQuestionRound returns a round number one past the highest round already present in the
// thread (min 1), so appendReviewComments mints fresh, non-colliding ids for a new batch of
// questions regardless of how many reject/amendment rounds preceded them.
func nextQuestionRound(thread []projectstate.ReviewComment) int64 {
	var maxRound int64
	for _, c := range thread {
		if c.Round > maxRound {
			maxRound = c.Round
		}
	}
	return maxRound + 1
}

// askQuestionsIdempotencyKey derives the stable logical key for "ask this batch of questions
// on this artifact/branch". Content-derived (no Temporal context on this sync op), so a
// retried identical Ask collapses to a no-op in the RA dedup ledger while a genuinely new
// batch is a distinct mutation.
func askQuestionsIdempotencyKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(branch))
	_, _ = h.Write([]byte{0})
	for _, q := range qs {
		_, _ = h.Write([]byte(q.Addressee))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Anchor))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(q.Text))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:askQuestions:%x", projectID, int(kind), h.Sum64()))
}

// answerJobDispatchSeq makes each explicit AskQuestions call produce a UNIQUE answer-job
// dispatch key, so a re-ask RE-FIRES the answer job (the RA dedups on the whole key, so a
// content-only key would swallow the re-fire — F82). AskQuestions is a direct, non-retried
// manager op (exactly one dispatch per successful call), so a per-call nonce cannot
// double-fire a single logical ask; it only enables the re-ask recovery.
var answerJobDispatchSeq atomic.Uint64

// answerJobDispatchKey derives a per-call-unique answer-job idempotency key from the content
// base plus a monotonic nonce (see answerJobDispatchSeq).
func answerJobDispatchKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	base := askQuestionsIdempotencyKey(projectID, kind, branch, qs)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:answerJob:%d", base, answerJobDispatchSeq.Add(1)))
}

// existingQuestionRound returns the round of an EARLIER identical seeding of qs (matched by
// addressee + anchor + text of the first question), so a re-ask reuses that round rather than
// minting a fresh one — keeping the minted ids aligned with the already-seeded ledger entries
// (F82 re-dispatch correctness). ok=false when these questions were never seeded.
func existingQuestionRound(thread []projectstate.ReviewComment, qs []projectstate.ReviewComment) (int64, bool) {
	if len(qs) == 0 {
		return 0, false
	}
	first := qs[0]
	for _, c := range thread {
		if c.Type == projectstate.ReviewCommentTypeQuestion &&
			c.Addressee == first.Addressee && c.Anchor == first.Anchor && c.Text == first.Text {
			return c.Round, true
		}
	}
	return 0, false
}

// dispatchAnswerJob dispatches ONE lightweight agentic ANSWER job (job_mode=answer) to the
// per-project design repo so the addressed role answers each question in place via the
// aiarch-state MCP. Best-effort and fire-and-forget (it does NOT wait for the job — questions
// are auxiliary and never gate anything). F82: every outcome is LOGGED LOUDLY server-side — a
// miss (rail not configured, repo unresolved, or a submit fault) was previously discarded and
// the construction-pipeline RA has no logger, so it vanished with zero operator signal. A miss
// is recoverable by re-calling AskQuestions (see the op doc) — never silent.
func (m *systemDesignManager) dispatchAnswerJob(ctx context.Context, projectID ProjectID, kind ArtifactKind, branch, addressee string, qs []projectstate.ReviewComment) {
	log := slog.Default().With(
		"op", "systemdesign.AskQuestions.dispatchAnswerJob",
		"projectID", string(projectID), "artifactKind", artifactKindString(kind),
		"addressee", addressee, "branch", branch)
	if m.pipeline == nil || m.repo == nil {
		log.Warn("answer job NOT dispatched: design pipeline/repo not configured (rail dormant) — the question is recorded but will not be auto-answered")
		return
	}
	repoRef, ok := m.repo(projectID)
	if !ok {
		log.Error("answer job NOT dispatched: could not resolve the project repo — the question is recorded but will not be auto-answered; re-run AskQuestions to retry")
		return
	}
	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch): an answer job runs the same seated
	// aiarch-design.yml (and installs the same aiarch-state-mcp binary) as a draft, so it
	// too must never run against a stale scaffold. Failure keeps the answer-job miss
	// semantics: recorded question, loud log, no dispatch — re-run AskQuestions to retry.
	if m.rail != nil {
		cred, cerr := m.rail.GetInstallationToken(fwra.Context{Context: ctx}, repoRef)
		if cerr != nil {
			log.Error("answer job NOT dispatched: could not mint the repo credential for the managed-scaffold sync; re-run AskQuestions to retry", "err", cerr.Error())
			return
		}
		if _, serr := sourcecontrol.SyncManagedScaffold(ctx, m.rail, repoRef, cred); serr != nil {
			log.Error("answer job NOT dispatched: managed-scaffold sync failed — the seated design workflow could not be proven current; re-run AskQuestions to retry", "err", serr.Error())
			return
		}
	}
	// Direct manager-side dispatch (NOT a Temporal workflow): the answer job is a
	// fire-and-forget submit over the PUBLISHED constructionPipelineAccess RA. The
	// RepoRef→RepoTarget decode + the placeholder step graph the retired pipelineDispatchAdapter
	// added are inlined here (the workflow-side twin is dispatchDesignJob in dispatch.go).
	target, terr := designRepoTarget(sourcecontrol.RepoRefString(repoRef))
	if terr != nil {
		log.Error("answer job NOT dispatched: could not resolve the target repo for the answer job; re-run AskQuestions to retry", "err", terr.Error())
		return
	}
	// The addressee rides the .claude command NAME now (design-answer vs design-answer-pm)
	// rather than a composed answer prompt. An empty slug is contract misuse — an addressee
	// that is neither "architect" nor "pm"; keep the answer-job miss semantics (recorded
	// question, loud log, no dispatch).
	command := projectstate.DesignCommandFor(toPSKind(kind), projectstate.DesignJobModeAnswer, addressee)
	if command == "" {
		log.Error("answer job NOT dispatched: no design-answer command slug for the addressee (contract misuse — expected \"architect\" or \"pm\")")
		return
	}
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(kind),
		dispatchInputCommand:       command,
		dispatchInputTargetBranch:  branch,
		dispatchInputPriorStateRef: "",
		dispatchInputJobMode:       jobModeAnswer,
	}
	spec := constructionpipeline.PipelineSpec{
		ProjectID: constructionpipeline.ProjectID(projectID),
		Steps: []constructionpipeline.PipelineStep{{
			Name:      "design",
			Toolchain: constructionpipeline.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
		WorkflowFile:   designWorkflowFileName,
	}
	key := answerJobDispatchKey(projectID, kind, branch, qs)
	if _, err := m.pipeline.SubmitConstructionPipeline(fwra.Context{Context: ctx, IdempotencyKey: key}, spec); err != nil {
		log.Error("answer job dispatch FAILED — the question is recorded but not auto-answered; re-run AskQuestions with the same question to retry",
			"err", err.Error(), "key", string(key))
		return
	}
	log.Info("answer job dispatched", "key", string(key))
}

// catalog.go holds the three CATALOG / cross-phase typed-read ops folded onto the
// systemDesignManager from the former projectManager (dissolved 2026-06-28): a
// project's permanent identity IS its living system design, so the project CATALOG
// + the cross-phase typed head-state read belong on this Manager. These ops own NO
// Temporal workflow; they are thin synchronous reads/writes over the published
// projectStateAccess (head state), sourceControlAccess (project-birth adopt + seat),
// and the estimationEngine (compute-at-read CPM + EV/SPI).
//
// SCHEMA-FIRST: the public surface (the 3 ops + the ProjectState projection types)
// is GENERATED into contract.gen.go from project.json .serviceContracts; this file
// is the hand-written impl on the unexported *systemDesignManager. The generated
// contract imports neither projectstate nor Temporal — the aggregate value shapes
// are field-mapped to the Manager's OWN contract types at the boundary, and the
// per-slot artifact MODEL is carried OPAQUELY as an {kind, raw-json} envelope.

// CreateProject births a new project. NAME-AS-IDENTITY (C-PM-Δ): the USER supplies
// the repo name, which IS the project identity (project name == repo name). The
// supplied name is validated, then — IN ORDER, preserving the I-RA call-order
// guarantee + idempotent re-convergence — the Manager:
//
//  1. ADOPTS the user's existing repo (sourceControlAccess.AdoptProjectRepo).
//  2. SEATS the agentic-design workflow file: mint a short-lived credential, then
//     commit the claude-code-action DESIGN workflow file.
//  3. creates the head-state row (projectStateAccess.CreateProject), STRICTLY AFTER
//     the above, keyed on the repo name as identity.
//
// Returns the project id (== the adopted repo name). Validation errors (empty
// owner/name) surface as ContractMisuse before any RA call. Every write is idempotent
// — a retry after a partial failure RE-CONVERGES rather than duplicating. The rail
// (sourceControlAccess) is optional: nil ⇒ repo-less create (a dev server with no
// GitHub App credentials).
func (m *systemDesignManager) CreateProject(rc fwmanager.Context, owner OwnerScope, name string) (ProjectID, error) {
	ctx := rc.Context
	if owner == "" {
		return "", newError(fwmanager.ContractMisuse, "empty owner")
	}
	if name == "" {
		return "", newError(fwmanager.ContractMisuse, "empty name")
	}

	// NAME-AS-IDENTITY: the user-supplied name IS the project identity == repo name.
	projectID := ProjectID(name)
	key := createProjectIdempotencyKey(projectID)

	// Adopt the user's existing repo + seat the workflow file FIRST (project birth,
	// before the head-state row). Skipped only when source-control is unconfigured
	// (nil) — a repo-less dev server. Every step is idempotent; a retry re-converges.
	if m.rail != nil {
		repo, err := m.rail.AdoptProjectRepo(fwra.Context{Context: ctx, IdempotencyKey: key}, sourcecontrol.RepoAdoptionSpec{
			RepoName: name, // name-as-identity: the project id IS the repo name
			Title:    name,
		})
		if err != nil {
			return "", mapRAError(err, "sourceControlAccess.AdoptProjectRepo")
		}
		cred, err := m.rail.GetInstallationToken(fwra.Context{Context: ctx}, repo)
		if err != nil {
			return "", mapRAError(err, "sourceControlAccess.GetInstallationToken")
		}
		files, err := sourcecontrol.ManagedScaffoldFiles(repo, sourcecontrol.RailAppSlug(m.rail))
		if err != nil {
			return "", mapRAError(err, "sourceControlAccess.ManagedScaffoldFiles")
		}
		if _, err := m.rail.CommitManagedFiles(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, files, cred); err != nil {
			return "", mapRAError(err, "sourceControlAccess.CommitManagedFiles")
		}
	}

	if _, err := m.projectState.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: key},
		projectstate.ProjectID(projectID), projectstate.OwnerScope(owner), name); err != nil {
		return "", mapRAError(err, "projectStateAccess.CreateProject")
	}
	return projectID, nil
}

// SetOperatingModel records the project-level WHO-OPERATES choice (founder ruling
// 2026-07-05). SYNCHRONOUS, non-Temporal, mirroring SetResearchInput: a single
// idempotent head-state write via projectStateAccess.SetOperatingModel with a bounded
// sync optimistic-concurrency loop (re-read the head Version, re-apply on Conflict). The
// UI/MCP calls it at creation — after CreateProject, before StartSystemDesign — to pick
// self-operated (the default the project is born with) or archistrator-operated (which
// constrains the deployment design to the platform palette). Returns the head Version.
func (m *systemDesignManager) SetOperatingModel(rc fwmanager.Context, projectID ProjectID, model OperatingModel) (Version, error) {
	ctx := rc.Context
	if projectID == "" {
		return 0, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	psModel := projectstate.OperatingModel(string(model))
	if !psModel.Valid() {
		return 0, newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown operating model %q", string(model)))
	}

	key := fwra.IdempotencyKey(fmt.Sprintf("%s:setOperatingModel:%s", projectID, model))
	psID := projectstate.ProjectID(projectID)

	var lastErr error
	for range setOperatingModelMaxAttempts {
		proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, psID)
		if err != nil {
			return 0, mapRAError(err, "projectStateAccess.ReadProject")
		}
		newVersion, err := m.projectState.SetOperatingModel(fwra.Context{Context: ctx, IdempotencyKey: key}, psID, proj.Version, psModel)
		if err == nil {
			return Version(newVersion), nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue // re-read head Version, re-apply (same idempotencyKey)
		}
		return 0, mapRAError(err, "projectStateAccess.SetOperatingModel")
	}
	return 0, fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "projectStateAccess.SetOperatingModel: exhausted conflict retries")
}

// setOperatingModelMaxAttempts bounds the sync-path re-read/re-apply loop.
const setOperatingModelMaxAttempts = 5

// createProjectIdempotencyKey derives the stable logical idempotency key for "create
// this project". The project id IS the user-supplied repo name and unique per
// project, so it is itself the natural dedup token.
func createProjectIdempotencyKey(projectID ProjectID) fwra.IdempotencyKey {
	return fwra.IdempotencyKey(fmt.Sprintf("%s:createProject", projectID))
}

// ListProjects returns the landing-grid catalog for owner, newest-first (the RA's
// ordering). A pass-through over projectStateAccess.ListProjects, mapped to the
// contract ProjectSummary.
func (m *systemDesignManager) ListProjects(rc fwmanager.Context, owner OwnerScope) ([]ProjectSummary, error) {
	ctx := rc.Context
	if owner == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty owner")
	}
	summaries, err := m.projectState.ListProjects(fwra.Context{Context: ctx}, projectstate.OwnerScope(owner))
	if err != nil {
		return nil, mapRAError(err, "projectStateAccess.ListProjects")
	}
	out := make([]ProjectSummary, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, summaryToContract(s))
	}
	return out, nil
}

// GetProject returns the full typed head-state for one project, mapping the
// projectstate.Project aggregate's named typed slots into the contract ProjectState.
// fwra.NotFound passes through as fwmanager.NotFound.
func (m *systemDesignManager) GetProject(rc fwmanager.Context, projectID ProjectID) (ProjectState, error) {
	ctx := rc.Context
	if projectID == "" {
		return ProjectState{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		// A NotFound for an unknown project must NOT leak the internal git call chain
		// (e.g. "resourceaccess: github.GitStore.clone: repository not found: repository
		// not found: Repository not found." — the message stutters as each layer re-wraps
		// its own "not found" text). Map it to a single, clean, project-scoped Detail;
		// the full cause chain is preserved on Cause for the server-side log.
		if raErr := (*fwra.Error)(nil); errors.As(err, &raErr) && raErr.Kind == fwra.NotFound {
			return ProjectState{}, fwmanager.Wrap(fwmanager.NotFound, err, fmt.Sprintf("project %q not found", projectID))
		}
		return ProjectState{}, mapRAError(err, "projectStateAccess.ReadProject")
	}
	m.computeNetworkAtRead(&proj)
	return m.projectStateToContract(proj), nil
}

// mapRAError translates a projectStateAccess / sourceControlAccess error into the
// Manager façade error model. fwra.NotFound → NotFound; fwra.ContractMisuse →
// ContractMisuse; everything else (incl. Conflict — a thin read/catalog op has no
// optimistic-concurrency loop to recover it) → Infrastructure with the original
// retryability preserved. label identifies the ACTUAL failing dependency+op (e.g.
// "sourceControlAccess.AdoptProjectRepo") — CreateProject fans across two RAs, so a
// fixed label would misattribute a source-control fault to projectStateAccess. It is
// the opaque Detail returned to the client; the full cause chain stays server-side
// (Cause), surfaced only in the composition-root log.
func mapRAError(err error, label string) error {
	if err == nil {
		return nil
	}
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "Everything else... → Infrastructure" per the doc comment above.
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, label)
			mapped.Retryable = raErr.Retryable
			return mapped
		default:
			mapped := fwmanager.Wrap(fwmanager.Infrastructure, err, label)
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	// A non-fwra error (e.g. ManagedScaffoldFiles scaffold assembly) still carries
	// its cause for the server log while keeping the client Detail opaque (label).
	return fwmanager.Wrap(fwmanager.Infrastructure, err, label)
}

// ---------------------------------------------------------------------------
// Compute-at-read enrichment (INTERNAL impl). Operates on the projectstate.Project
// aggregate BEFORE mapping to the contract.
// ---------------------------------------------------------------------------

// computeNetworkAtRead populates the Network slot's COMPUTE-AT-READ block (per-node CPM
// figures, criticality bands, milestone event times, summary) by running the
// estimationEngine.ComputeNetwork over the AUTHORED network × activity list.
// NO-OP when the estimator is nil or the Network slot has no authored model.
func (m *systemDesignManager) computeNetworkAtRead(p *projectstate.Project) {
	if m.estimator == nil {
		return
	}
	net, ok := p.Network.Model.(*projectstate.Network)
	if !ok || net == nil {
		return
	}
	var activities projectstate.ActivityList
	if al, alok := p.ActivityList.Model.(*projectstate.ActivityList); alok && al != nil {
		activities = *al
	}

	solution, err := m.estimator.ComputeNetwork(fweng.Context{Context: context.Background()}, toEstimationActivityList(activities), toEstimationNetwork(*net))
	if err != nil {
		return // degenerate input guard — serve the authored network unenriched
	}

	computed := make(map[string]projectstate.NetworkNodeCompute, len(solution.Nodes))
	for id, n := range solution.Nodes {
		computed[id] = projectstate.NetworkNodeCompute{
			EarliestStart:  n.EarliestStart,
			EarliestFinish: n.EarliestFinish,
			LatestStart:    n.LatestStart,
			LatestFinish:   n.LatestFinish,
			TotalFloat:     n.TotalFloat,
			FreeFloat:      n.FreeFloat,
			OnCriticalPath: n.OnCriticalPath,
			NearCritical:   n.NearCritical,
			Band:           n.Band,
			Column:         int(n.Column),
		}
	}
	net.Computed = computed

	// Overwrite the served criticalPath[] with the engine's computed float-0 ACTIVITY
	// set (the authored criticalPath[] may be stale). Sorted for a deterministic wire order.
	computedCP := make([]string, 0, len(solution.Nodes))
	for id, n := range solution.Nodes {
		if n.OnCriticalPath {
			computedCP = append(computedCP, id)
		}
	}
	sort.Strings(computedCP)
	net.CriticalPath = computedCP

	net.Summary = &projectstate.NetworkSummary{
		TotalDurationDays:         solution.Summary.TotalDurationDays,
		CriticalPathActivityCount: int(solution.Summary.CriticalPathActivityCount),
		CriticalPathDays:          solution.Summary.CriticalPathDays,
		MaxFloat:                  solution.Summary.MaxFloat,
		NearCriticalCount:         int(solution.Summary.NearCriticalCount),
	}

	// Merge the computed milestone facets back onto the authored milestone rows (matched
	// by id), preserving authored id/name/public/dependsOn order.
	computedByID := make(map[string]estimation.NetworkMilestoneSolution, len(solution.Milestones))
	for _, ms := range solution.Milestones {
		computedByID[ms.ID] = ms
	}
	for i := range net.Milestones {
		if ms, found := computedByID[net.Milestones[i].ID]; found {
			onCP := ms.OnCriticalPath
			event := ms.EventTime
			net.Milestones[i].OnCriticalPath = &onCP
			net.Milestones[i].EventTime = &event
		}
	}
}

// toEstimationActivityList converts the canonical projectstate.ActivityList to the
// estimationEngine's OWN SLIM ActivityList at the call boundary.
func toEstimationActivityList(al projectstate.ActivityList) estimation.ActivityList {
	out := estimation.ActivityList{Activities: make([]estimation.ActivityItem, 0, len(al.Activities))}
	for _, a := range al.Activities {
		out.Activities = append(out.Activities, estimation.ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	return out
}

// toEstimationNetwork converts the canonical projectstate.Network to the
// estimationEngine's OWN SLIM Network at the call boundary.
func toEstimationNetwork(net projectstate.Network) estimation.Network {
	deps := make([]estimation.NetworkDependency, 0, len(net.Dependencies))
	for _, d := range net.Dependencies {
		deps = append(deps, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	var milestones []estimation.NetworkMilestone
	if len(net.Milestones) > 0 {
		milestones = make([]estimation.NetworkMilestone, 0, len(net.Milestones))
		for _, mlst := range net.Milestones {
			milestones = append(milestones, estimation.NetworkMilestone{Id: mlst.ID, DependsOn: mlst.DependsOn})
		}
	}
	return estimation.Network{Dependencies: deps, Milestones: milestones}
}

// ---------------------------------------------------------------------------
// projectstate → contract conversions (the Manager boundary).
// ---------------------------------------------------------------------------

// phaseLabels is the SINGLE SOURCE OF TRUTH mapping the 0-indexed project
// lifecycle Phase to its human-readable label (PM-P2-5: clients kept misreading
// the bare int). Kept aligned with the Phase enum in contract.gen.go — 0/1/2.
var phaseLabels = map[Phase]string{
	PhaseSystemDesign:  "system-design",
	PhaseProjectDesign: "project-design",
	PhaseConstruction:  "construction",
}

// phaseName returns the human-readable label for a Phase, or "" when the phase
// is outside the known 0/1/2 range — a map miss yields the zero value, so an
// out-of-range Phase reads as empty rather than a fabricated label.
func phaseName(p Phase) string {
	return phaseLabels[p]
}

// summaryToContract maps a projectstate.ProjectSummary onto the contract ProjectSummary.
func summaryToContract(s projectstate.ProjectSummary) ProjectSummary {
	phase := Phase(int(s.Phase))
	return ProjectSummary{
		ProjectID:      ProjectID(s.ProjectID),
		Name:           s.Name,
		Owner:          OwnerScope(s.Owner),
		Phase:          phase,
		PhaseName:      phaseName(phase),
		CommittedCount: int64(s.CommittedCount),
		TotalCount:     int64(s.TotalCount),
		UpdatedAt:      s.UpdatedAt,
	}
}

// projectStateToContract maps the head-state Project aggregate to the contract
// ProjectState transport shape. Read-time projections (each git row's prUrl/prNumber
// composed from the per-project repo base + the opaque ref, and the EV/SPI earned-value
// curve from m.estimator) are sourced server-side here rather than re-derived by the webClient.
func (m *systemDesignManager) projectStateToContract(p projectstate.Project) ProjectState {
	phase := Phase(int(p.Phase))
	return ProjectState{
		ProjectID: ProjectID(p.ID),
		Name:      p.Name,
		Owner:     OwnerScope(p.Owner),
		Phase:     phase,
		PhaseName: phaseName(phase),
		Version:   int64(p.Version),
		// OrDefault: a pre-field project (empty model) reads as self-operated on the
		// wire — the back-compat default — so the SPA never sees an empty operating model.
		OperatingModel:       OperatingModel(string(p.OperatingModel.OrDefault())),
		Research:             researchToContract(p.Research),
		Slots:                slotsToContract(p),
		GitRows:              m.gitRowsToContract(ProjectID(p.ID), p.ActivityGit),
		ActivityConstruction: constructionRowsToContract(p.ActivityConstruction, activityMetaByID(p)),
		ConstructionProgress: m.constructionProgressToContract(p),
		ServiceContracts:     serviceContractsToContract(p.ServiceContracts),
		ReviewPolicy:         reviewPolicyToContract(p.ReviewPolicy),
		TestingState:         testingStateToContract(p.TestingState),
	}
}

// testingStateToContract converts the head-state TestingState to the contract
// view. Returns nil when absent so the field is omitted from the read.
func testingStateToContract(ts *projectstate.TestingState) *TestingStateView {
	if ts == nil {
		return nil
	}
	runs := make([]TestRunView, len(ts.TestRuns))
	for i, r := range ts.TestRuns {
		runs[i] = TestRunView{Id: r.ID, Passed: int64(r.Passed), Failed: int64(r.Failed), Note: r.Note}
	}
	defects := make([]DefectView, len(ts.Defects))
	for i, d := range ts.Defects {
		defects[i] = DefectView{Id: d.ID, Title: d.Title, Severity: d.Severity, Note: d.Note}
	}
	return &TestingStateView{TestRuns: runs, Defects: defects, SystemTestPlan: systemTestPlanToContract(ts.SystemTestPlan)}
}

// systemTestPlanToContract maps the black-box operation-sequence scenarios of the
// system test plan. Returns nil when there is no plan or no scenarios (the plan's
// prose/index fields are not part of this view — only the renderable sequences).
func systemTestPlanToContract(p *projectstate.SystemTestPlan) *SystemTestPlanView {
	if p == nil || len(p.Scenarios) == 0 {
		return nil
	}
	scenarios := make([]TestScenarioView, len(p.Scenarios))
	for i, s := range p.Scenarios {
		cases := make([]TestCaseView, len(s.Cases))
		for j, c := range s.Cases {
			steps := make([]TestStepView, len(c.Steps))
			for k, st := range c.Steps {
				inputs := make([]TestArgView, len(st.Inputs))
				for m, a := range st.Inputs {
					inputs[m] = TestArgView{Name: a.Name, Value: a.Value, SchemaRef: a.SchemaRef}
				}
				steps[k] = TestStepView{
					Seq:       int64(st.Seq),
					Component: st.Component,
					Operation: st.Operation,
					Status:    st.Status,
					Inputs:    inputs,
					Expect:    TestExpectView{Result: st.Expect.Result, ErrorExpected: st.Expect.ErrorExpected, ErrorCode: st.Expect.ErrorCode},
					Assertion: st.Assertion,
				}
			}
			cases[j] = TestCaseView{Id: c.ID, Kind: c.Kind, Title: c.Title, Proves: c.Proves, ExpectedOutcome: c.ExpectedOutcome, Steps: steps}
		}
		scenarios[i] = TestScenarioView{Id: s.ID, UseCase: s.UseCase, Title: s.Title, Description: s.Description, Cases: cases}
	}
	return &SystemTestPlanView{Scenarios: scenarios}
}

// reviewPolicyToContract converts the head-state ReviewPolicy to the contract
// ReviewPolicyView. Returns nil when the policy is empty (no gates configured
// AND no preset chosen) — matching EncodeProject's own emptiness gate, so the
// webApp's preset control (local-merge-and-policy Commit 3) reads the committed
// preset back rather than always showing an unset dial.
func reviewPolicyToContract(p projectstate.ReviewPolicy) *ReviewPolicyView {
	if len(p.GatedPhasesByType) == 0 && p.Preset == nil {
		return nil
	}
	byType := make(map[string][]string, len(p.GatedPhasesByType))
	for typ, phases := range p.GatedPhasesByType {
		strs := make([]string, len(phases))
		for i, ph := range phases {
			strs[i] = string(ph)
		}
		byType[typ] = strs
	}
	return &ReviewPolicyView{GatedPhasesByType: byType, Preset: p.Preset}
}

// researchToContract maps the Phase-1 research corpus onto the read view. F22
// (read-model slimming): the corpus Content — a source can be a whole 660KB book —
// is deliberately NOT shipped on the project read. GetProject is polled at 1.5s by
// the construction console and paid on every HomeBase/design load, yet the SPA never
// renders corpus content; carrying it made a single read ~686KB. We keep the sources
// array shape (title stays, so the UI can list what is loaded) but EMPTY the content
// and surface each source's byte-size as ContentBytes so the UI can still show "N KB
// loaded". The full corpus is read from git by the design Action, not through this
// endpoint — see setResearchInput (write path) which is unchanged.
func researchToContract(r projectstate.ResearchCorpus) ResearchInput {
	sources := make([]ResearchSource, 0, len(r.Sources))
	for _, s := range r.Sources {
		// F42: the corpus is persisted as pointers now — ContentBytes comes straight off the
		// stored pointer (no Content to measure); Content stays empty on the read model.
		n := s.ContentBytes
		sources = append(sources, ResearchSource{Title: s.Title, Content: "", ContentBytes: &n})
	}
	return ResearchInput{Sources: sources}
}

// slotsToContract emits one ArtifactSlotView per defined ArtifactKind in the stable
// slot order, deriving each slot's Stage from its stored ArtifactReviewStatus and
// carrying its typed Model OPAQUELY (the {kind, raw-json} envelope).
func slotsToContract(p projectstate.Project) []ArtifactSlotView {
	kinds := projectstate.AllArtifactKinds()
	slots := make([]ArtifactSlotView, 0, len(kinds))
	for _, kind := range kinds {
		slot := slotForKind(p, kind)
		slots = append(slots, ArtifactSlotView{
			Kind:  kind.WireName(),
			Stage: stageForStatus(slot.Status),
			Model: encodeSlotModel(kind, slot.Model),
			Notes: notesPtr(slot.Notes),
			// F38: surface the staleness chip + the amendment (commit) count so the SPA can
			// flag "basis shifted — reconcile" and show the revision. Both omitempty on the wire.
			StaleBasis:      staleBasisPtr(slot.StaleBasis),
			StaleBasisCause: staleBasisCauseView(slot.StaleBasisCause),
			Revisions:       revisionsPtr(slot.Revisions),
			// PM-P2-4: surface the committed-slot provenance (who/when/rail) under the
			// committed strip. nil (omitempty) for uncommitted / pre-provenance slots.
			Provenance: provenanceView(slot.Provenance),
		})
	}
	return slots
}

// encodeSlotModel carries the slot's typed model OPAQUELY: the canonical camelCase
// kind wire name + the concrete model's own JSON (nil when the slot is empty).
func encodeSlotModel(kind projectstate.ArtifactKind, m projectstate.ArtifactModel) ArtifactSlotModel {
	env := ArtifactSlotModel{Kind: kind.WireName()}
	if m != nil {
		if raw, err := json.Marshal(m); err == nil {
			rm := json.RawMessage(raw)
			env.Model = &rm
		}
	}
	return env
}

// notesPtr maps an architect-notes string to the optional contract field.
func notesPtr(notes string) *string {
	if notes == "" {
		return nil
	}
	n := notes
	return &n
}

// staleBasisPtr surfaces the F38 staleness chip only when the slot is actually stale
// (omitempty on the wire: absent ⇒ not stale).
func staleBasisPtr(stale bool) *bool {
	if !stale {
		return nil
	}
	b := true
	return &b
}

// StaleCauseView is the read-model projection of projectstate.StaleCause: WHY a committed
// slot went stale (the upstream slot kind + its new revision), so the SPA can say
// "Volatilities rev 2 changed after this was committed". Absent when the slot is not stale
// or went stale before the cause was recorded (no back-fill).
type StaleCauseView struct {
	UpstreamKind     string `json:"upstreamKind"`
	UpstreamRevision int64  `json:"upstreamRevision"`
}

// staleBasisCauseView projects the stored stale-cause onto the read model, nil-safe
// (omitempty on the wire: absent ⇒ not stale or cause unknown).
func staleBasisCauseView(c *projectstate.StaleCause) *StaleCauseView {
	if c == nil {
		return nil
	}
	return &StaleCauseView{UpstreamKind: c.UpstreamKind, UpstreamRevision: c.UpstreamRevision}
}

// revisionsPtr surfaces the F38 commit/amendment count only once the slot has been
// committed at least once (omitempty on the wire: absent ⇒ 0).
func revisionsPtr(n int64) *int64 {
	if n == 0 {
		return nil
	}
	v := n
	return &v
}

// ProvenanceView is the read-model projection of projectstate.Provenance (PM-P2-4): WHO
// committed a slot / WHEN / which rail drafted it, so the SPA can render a muted
// "committed <date> · approved by X · drafted by Y" line under the committed strip. Absent
// (nil, omitempty on the wire) for an uncommitted slot or one committed before provenance
// was recorded (no back-fill). Each field is independently optional.
type ProvenanceView struct {
	CommittedAt string `json:"committedAt,omitempty"`
	ApprovedBy  string `json:"approvedBy,omitempty"`
	DraftedBy   string `json:"draftedBy,omitempty"`
}

// provenanceView projects the stored commit provenance onto the read model, nil-safe
// (omitempty on the wire: absent ⇒ not committed or provenance unknown).
func provenanceView(p *projectstate.Provenance) *ProvenanceView {
	if p == nil {
		return nil
	}
	return &ProvenanceView{CommittedAt: p.CommittedAt, ApprovedBy: p.ApprovedBy, DraftedBy: p.DraftedBy}
}

// stageForStatus maps the stored per-slot ArtifactReviewStatus to the contract stage.
func stageForStatus(s projectstate.ArtifactReviewStatus) ArtifactStage {
	switch s {
	case projectstate.ReviewNone:
		// No review has happened yet (slot not yet drafted) — same as the
		// fallback for any other not-yet-meaningful status.
		return ArtifactStageEmpty
	case projectstate.ReviewAwaitingReview:
		return ArtifactStageAwaitingReview
	case projectstate.ReviewCommitted:
		return ArtifactStageCommitted
	case projectstate.ReviewRejected:
		return ArtifactStageRejected
	case projectstate.ReviewWithdrawn:
		return ArtifactStageWithdrawn
	default:
		return ArtifactStageEmpty
	}
}

// gitRowsToContract maps the per-activity git head-state map (honest-empty: nil in ⇒
// nil out). It composes each row's READ-TIME prUrl/prNumber projections from the
// PER-PROJECT repo base + the opaque pullRequestRef — the durable aggregate stays
// provider-opaque; prUrl/prNumber are pure read-time projections, never stored.
//
// Since the venue switch (0df2ce0) gh-mode construction PRs open in the PROJECT's own
// repo, not the central construction repo, so the base is resolved per-project via
// projectRepoBase(projectID) (which falls back to the central m.repoBase exactly when
// the dispatch resolver is nil or misses — links stay central-pointing precisely when
// dispatch does).
func (m *systemDesignManager) gitRowsToContract(projectID ProjectID, rows map[string]projectstate.ActivityGitStatus) map[string]ActivityGitStatus {
	if len(rows) == 0 {
		return nil
	}
	base := m.projectRepoBase(projectID)
	out := make(map[string]ActivityGitStatus, len(rows))
	for id, g := range rows {
		prNumber, prURL := projectPRRef(g.PullRequestRef, base)
		out[id] = ActivityGitStatus{
			ActivityID:     g.ActivityID,
			BranchName:     g.BranchName,
			BranchRef:      g.BranchRef,
			PullRequestRef: g.PullRequestRef,
			PrNumber:       int64(prNumber),
			PrURL:          prURL,
			CICheck:        CICheckState(int(g.CICheck)),
			ArchApproved:   g.ArchApproved,
			Merged:         g.Merged,
			CRLabel:        g.CRLabel,
			IsRevert:       g.IsRevert,
			UpdatedAt:      g.UpdatedAt,
		}
	}
	return out
}

// projectPRRef is the SINGLE server-side site that turns the OPAQUE pullRequestRef into
// the SPA's two read-time render fields (D-PA-GIT-PRURL-ruling R1/R2). It isolates BOTH
// the "the opaque ref is a decimal PR number" assumption AND the GitHub "/pull/<n>" URL
// grammar to one place — the durable aggregate stays provider-opaque.
//
//   - prNumber: strconv.Atoi(ref). Zero (→ omitted by the web wire's omitempty) when ref
//     is "" (branch-only first touch) or unparseable — never panics, never fabricates.
//   - prURL: <repoBase>/pull/<ref>, ONLY when ref != "" AND repoBase != "". Otherwise "".
func projectPRRef(ref, repoBase string) (prNumber int, prURL string) {
	if ref == "" {
		return 0, ""
	}
	if n, err := strconv.Atoi(ref); err == nil {
		prNumber = n
	}
	if repoBase != "" {
		prURL = repoBase + "/pull/" + ref
	}
	return prNumber, prURL
}

// projectRepoBase resolves the WEB base each git row's prUrl is composed against FOR THIS
// PROJECT. Since the venue switch (0df2ce0) gh-mode construction PRs open in the project's
// OWN repo, so a per-project base must be projected rather than reusing the central
// construction repo's base (m.repoBase) — those URLs would otherwise point at the wrong
// repo and lie. The host stays the same as the configured central base (github.com or the
// GHES web root); only owner/repo swap to the project's own.
//
// Fallback (mirrors the dispatch fallback so links stay central-pointing EXACTLY when
// dispatch stays central): the central m.repoBase is returned verbatim when the resolver
// is nil, misses the project, yields a malformed ref, or the central base has no host to
// borrow (unconfigured ⇒ "" ⇒ prUrl omitted downstream).
func (m *systemDesignManager) projectRepoBase(projectID ProjectID) string {
	if m.repo == nil {
		return m.repoBase
	}
	repoRef, ok := m.repo(projectID)
	if !ok {
		return m.repoBase
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(repoRef)
	if err != nil {
		return m.repoBase
	}
	host := repoWebHost(m.repoBase)
	if host == "" {
		return m.repoBase
	}
	return host + "/" + owner + "/" + name
}

// repoWebHost recovers the <host> prefix from a <host>/<owner>/<repo> web base by
// stripping the final two path segments. The host retains its scheme (https://…); a
// GHES subpath host (e.g. https://ghe.example.com/prefix) is preserved because only the
// trailing owner/repo pair is removed. "" in (unconfigured central base) ⇒ "" out.
func repoWebHost(repoBase string) string {
	s := strings.TrimRight(repoBase, "/")
	i := strings.LastIndex(s, "/")
	if i < 0 {
		return ""
	}
	s = s[:i] // drop /<repo>
	i = strings.LastIndex(s, "/")
	if i < 0 {
		return ""
	}
	return s[:i] // drop /<owner>
}

// constructionRowsToContract maps the per-activity construction head-state map
// (honest-empty: nil in ⇒ nil out). activityMeta carries the Phase-2 activity-list
// metadata (worker class + coding) keyed by activity id, used to classify each
// activity's ActivityType (see projectstate.ClassifyType) — the N-* id namespace
// alone is too coarse.
func constructionRowsToContract(
	rows map[string]projectstate.ActivityConstructionStatus,
	activityMeta map[string]projectstate.ActivityItem,
) map[string]ActivityConstructionStatus {
	if len(rows) == 0 {
		return nil
	}
	out := make(map[string]ActivityConstructionStatus, len(rows))
	for id, r := range rows {
		meta := activityMeta[id]
		typ := projectstate.ClassifyType(r.ActivityID, meta.WorkerClass, meta.Coding, rowHasServiceContract(r))
		var variant TestingVariant
		if typ == projectstate.ActivityTypeTesting {
			variant = TestingVariant(int(projectstate.DeriveVariant(r.ActivityID)))
		}
		out[id] = ActivityConstructionStatus{
			ActivityID:    r.ActivityID,
			Type:          ActivityType(int(typ)),
			Kind:          ActivityType(int(typ)),
			Variant:       variant,
			Phase:         ActivityConstructionPhase(int(r.Phase)),
			Phases:        phasesToContract(r.Phases),
			CurrentPhase:  ActivityMethodPhase(string(r.CurrentPhase)),
			StartedAt:     r.StartedAt,
			CompletedAt:   r.CompletedAt,
			BuildStatus:   ActivityBuildStatus(int(r.BuildStatus)),
			Produced:      producedToContract(r.Produced),
			FailureReason: FailureReason(int(r.FailureReason)),
			FailureDetail: r.FailureDetail,
		}
	}
	return out
}

// rowHasServiceContract reports whether the activity produced a frozen service
// contract (the signal that it built a component, regardless of its id family).
func rowHasServiceContract(r projectstate.ActivityConstructionStatus) bool {
	for _, a := range r.Produced {
		if a.Kind == "service-contract" {
			return true
		}
	}
	return false
}

// activityMetaByID builds the id → ActivityItem lookup from the committed
// Phase-2 activity list (empty map when no list is committed).
func activityMetaByID(p projectstate.Project) map[string]projectstate.ActivityItem {
	out := map[string]projectstate.ActivityItem{}
	if al, ok := p.ActivityList.Model.(*projectstate.ActivityList); ok && al != nil {
		for _, a := range al.Activities {
			out[a.Name] = a
		}
	}
	return out
}

// phasesToContract maps the App-A internal phase-completion records.
func phasesToContract(phases []projectstate.PhaseCompletion) []PhaseCompletion {
	if len(phases) == 0 {
		return nil
	}
	out := make([]PhaseCompletion, 0, len(phases))
	for _, ph := range phases {
		out = append(out, PhaseCompletion{
			Phase:       ActivityMethodPhase(string(ph.Phase)),
			Weight:      int64(ph.Weight),
			Completed:   ph.Completed,
			CompletedAt: ph.CompletedAt,
			ArtifactRef: ph.ArtifactRef,
		})
	}
	return out
}

// producedToContract maps the produced-artifact cards.
func producedToContract(produced []projectstate.ProducedArtifact) []ProducedArtifact {
	if len(produced) == 0 {
		return nil
	}
	out := make([]ProducedArtifact, 0, len(produced))
	for _, p := range produced {
		out = append(out, ProducedArtifact{Kind: p.Kind, Title: p.Title, Source: p.Source, Produced: p.Produced, Note: p.Note})
	}
	return out
}

// constructionProgressToContract maps the project-level Phase-3 framing scalars
// (nil in ⇒ nil out) AND computes the EV/SPI earned-value curve server-side via the
// estimationEngine (compute-at-read).
func (m *systemDesignManager) constructionProgressToContract(p projectstate.Project) *ConstructionProgress {
	cp := p.ConstructionProgress
	if cp == nil {
		return nil
	}
	return &ConstructionProgress{
		Week:           int64(cp.Week),
		TotalWeeks:     int64(cp.TotalWeeks),
		HandOffModel:   cp.HandOffModel,
		SupervisionCap: int64(cp.SupervisionCap),
		EV:             m.computeEVAtRead(p, int64(cp.TotalWeeks)),
		Points:         evPointsToContract(cp.Points),
	}
}

// evPointsToContract surfaces the recorded weekly earned-value observation series
// (the ground-truth points captured by the-method-project-tracking, stored on
// .constructionProgress.points) onto the read view. Distinct from computeEVAtRead's
// estimator-derived curve: these are what the team ACTUALLY earned each week.
func evPointsToContract(pts []projectstate.EvPoint) []EvPoint {
	if len(pts) == 0 {
		return nil
	}
	out := make([]EvPoint, 0, len(pts))
	for _, p := range pts {
		out = append(out, EvPoint{
			Week:       int64(p.Week),
			EarnedPct:  p.EarnedPct,
			PlannedPct: p.PlannedPct,
			Note:       p.Note,
			AcPct:      p.AcPct,
		})
	}
	return out
}

// computeEVAtRead computes the EV/SPI earned-value curve via the
// estimationEngine.ComputeEarnedValue over the AUTHORED activity list ×
// network, the integrated activity set, the calendar days/week, and the total-week
// framing. Zero EVCurve when the estimator is nil or inputs are degenerate.
func (m *systemDesignManager) computeEVAtRead(p projectstate.Project, totalWeeks int64) EVCurve {
	if m.estimator == nil {
		return EVCurve{}
	}
	var activities projectstate.ActivityList
	if al, ok := p.ActivityList.Model.(*projectstate.ActivityList); ok && al != nil {
		activities = *al
	}
	var network projectstate.Network
	if net, ok := p.Network.Model.(*projectstate.Network); ok && net != nil {
		network = *net
	}

	integrated := make([]string, 0, len(p.ActivityConstruction))
	for id, r := range p.ActivityConstruction {
		if r.BuildStatus == projectstate.BuildIntegrated {
			integrated = append(integrated, id)
		}
	}

	curve, err := m.estimator.ComputeEarnedValue(
		fweng.Context{Context: context.Background()},
		toEstimationActivityList(activities),
		toEstimationNetwork(network),
		integrated,
		totalWeeks,
		int64(calendarDaysPerWeek(p)),
	)
	if err != nil {
		return EVCurve{}
	}
	return EVCurve{Weeks: curve.Weeks, Earned: curve.Earned, Planned: curve.Planned, SPI: curve.SPI}
}

// calendarDaysPerWeek reads the working days/week from the PlanningAssumptions slot,
// defaulting to the standard 5-day workweek when the slot is absent or non-positive.
func calendarDaysPerWeek(p projectstate.Project) int {
	if pa, ok := p.PlanningAssumptions.Model.(*projectstate.PlanningAssumptions); ok && pa != nil && pa.CalendarDaysPerWeek > 0 {
		return int(pa.CalendarDaysPerWeek)
	}
	return 5
}

// serviceContractsToContract maps the typed service-contract corpus (honest-empty:
// nil in ⇒ nil out) onto the web-transport ServiceContract DTO. The contract
// DOCUMENT (its `interface` operations resolved against the document's `$defs`) is
// the source of truth: each op's parameters become input ContractStructs, its result
// becomes an output ContractStruct, and — when the op can fail — the layer's typed
// error becomes a final output box. Every struct's fields are resolved from the
// referenced `$def`'s properties (order-preserved). This is what feeds the SPA's
// «interface» diagram boxes; nothing is fabricated and nothing is served empty.
func serviceContractsToContract(scs map[string]projectstate.ServiceContract) map[string]ServiceContract {
	if len(scs) == 0 {
		return nil
	}
	out := make(map[string]ServiceContract, len(scs))
	for name, sc := range scs {
		layerErr := layerErrorName(sc.Layer)
		anyError := false
		for _, op := range sc.Interface.Operations {
			if op.Error {
				anyError = true
				break
			}
		}
		errorModel := ""
		if anyError {
			errorModel = "Operations fail with " + layerErr + " — the typed " + sc.Layer + " fault."
		}
		out[name] = ServiceContract{
			Component:     sc.Component,
			Layer:         sc.Layer,
			Stereotype:    sc.Title,
			Ops:           opsFromInterface(sc.Interface, sc.Defs, layerErr),
			DataContracts: dataContractNames(sc.Defs),
			ErrorModel:    errorModel,
		}
	}
	return out
}

// opsFromInterface derives the transport op list from the contract document's
// interface, resolving each op's params/result/error against the document's `$defs`
// into the input/output ContractStructs + a `name(params) → (result, error)`
// signature the SPA diagram renders. Returns nil for an empty interface.
func opsFromInterface(iface projectstate.ContractInterface, defs map[string]json.RawMessage, layerErr string) []ContractOp {
	if len(iface.Operations) == 0 {
		return nil
	}
	out := make([]ContractOp, 0, len(iface.Operations))
	for _, op := range iface.Operations {
		inputs := make([]ContractStruct, 0, len(op.Params))
		for _, p := range op.Params {
			inputs = append(inputs, structFromSchema(p.Name, p.Schema, defs))
		}
		var outputs []ContractStruct
		if len(op.Result) > 0 {
			outputs = append(outputs, structFromSchema("result", op.Result, defs))
		}
		if op.Error {
			outputs = append(outputs, ContractStruct{
				Name:   layerErr,
				Fields: []GoField{{Name: "fault", Type: layerErr}},
			})
		}
		out = append(out, ContractOp{
			Signature: opSignature(op, layerErr),
			Inputs:    inputs,
			Outputs:   outputs,
		})
	}
	return out
}

// opSignature renders one operation as `name(p: T, …) → (Result, error)`, using the
// same `→` separator the SPA signature parser recognises. Pointer params are starred.
func opSignature(op projectstate.ContractOperation, layerErr string) string {
	params := make([]string, 0, len(op.Params))
	for _, p := range op.Params {
		t := schemaTypeName(p.Schema, defaultTypeName)
		if p.Pointer {
			t = "*" + t
		}
		params = append(params, p.Name+": "+t)
	}
	sig := op.Name + "(" + strings.Join(params, ", ") + ")"
	var rets []string
	if len(op.Result) > 0 {
		rets = append(rets, schemaTypeName(op.Result, defaultTypeName))
	}
	if op.Error {
		rets = append(rets, layerErr)
	}
	switch len(rets) {
	case 0:
		// no declared return
	case 1:
		sig += " → " + rets[0]
	default:
		sig += " → (" + strings.Join(rets, ", ") + ")"
	}
	return sig
}

// structFromSchema resolves one JSON Schema node into a ContractStruct: the box is
// titled with the node's resolved Go-ish type name, and its fields are the referenced
// `$def`'s (or inline object's) properties. A scalar / array / external type has no
// sub-fields, so it carries a single self-field named selfName so no box is empty.
func structFromSchema(selfName string, raw json.RawMessage, defs map[string]json.RawMessage) ContractStruct {
	typeName := schemaTypeName(raw, selfName)
	fields := objectFields(raw, defs)
	if len(fields) == 0 {
		fields = []GoField{{Name: selfName, Type: typeName}}
	}
	return ContractStruct{Name: typeName, Fields: fields}
}

const defaultTypeName = "value"

// layerErrorName maps a Method layer to its framework error type, the typed fault
// every op on that layer returns on failure.
func layerErrorName(layer string) string {
	switch strings.ToLower(layer) {
	case "resourceaccess":
		return "fwra.Error"
	case "engine":
		return "fweng.Error"
	case "manager":
		return "fwm.Error"
	default:
		return "error"
	}
}

// dataContractNames returns the document's `$defs` names (the data contracts),
// sorted for a deterministic wire order. nil when there are none.
func dataContractNames(defs map[string]json.RawMessage) []string {
	if len(defs) == 0 {
		return nil
	}
	names := make([]string, 0, len(defs))
	for k := range defs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// schemaTypeName resolves a JSON Schema node to a Go-ish type name: an array → []T,
// an explicit x-go-type → that, a `$ref` → its base name, otherwise the mapped
// primitive. fallback is returned when the node is empty / unrecognised.
func schemaTypeName(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	var n struct {
		Ref     string          `json:"$ref"`
		Type    json.RawMessage `json:"type"`
		Items   json.RawMessage `json:"items"`
		XGoType string          `json:"x-go-type"`
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		return fallback
	}
	if len(n.Items) > 0 {
		return "[]" + schemaTypeName(n.Items, fallback)
	}
	if n.XGoType != "" {
		return n.XGoType
	}
	if n.Ref != "" {
		return refBase(n.Ref)
	}
	return primitiveTypeName(n.Type, fallback)
}

// primitiveTypeName maps a JSON Schema `type` (a string OR a ["null", T] union) to a
// Go-ish primitive name.
func primitiveTypeName(rawType json.RawMessage, fallback string) string {
	if len(rawType) == 0 {
		return fallback
	}
	kind := ""
	var single string
	if err := json.Unmarshal(rawType, &single); err == nil {
		kind = single
	} else {
		var union []string
		if err := json.Unmarshal(rawType, &union); err == nil {
			for _, k := range union {
				if k != "null" {
					kind = k
					break
				}
			}
		}
	}
	switch kind {
	case "string":
		return "string"
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "object":
		return "object"
	case "array":
		return "[]any"
	case "":
		return fallback
	default:
		return kind
	}
}

// refBase returns the trailing name of a JSON Schema `$ref` (e.g. "#/$defs/Foo" → "Foo").
func refBase(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// objectFields resolves a schema node's properties into ordered GoFields. It follows
// a single `$ref` into defs, then reads the resolved object's `properties` in
// declaration order (json.Decoder token stream preserves key order). Non-object
// nodes (scalars, arrays, enums) have no properties → nil.
func objectFields(raw json.RawMessage, defs map[string]json.RawMessage) []GoField {
	if len(raw) == 0 {
		return nil
	}
	var head struct {
		Ref        string          `json:"$ref"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil
	}
	if head.Ref != "" {
		target, ok := defs[refBase(head.Ref)]
		if !ok {
			return nil
		}
		return objectFields(target, defs)
	}
	if len(head.Properties) == 0 {
		return nil
	}
	return orderedProperties(head.Properties)
}

// orderedProperties decodes a JSON Schema `properties` object into ordered GoFields,
// preserving the on-disk key order. Each field's type is resolved from its schema and
// its name honours an `x-go-name` override when present.
func orderedProperties(props json.RawMessage) []GoField {
	dec := json.NewDecoder(bytes.NewReader(props))
	tok, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil
	}
	var fields []GoField
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fields
		}
		key, _ := keyTok.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return fields
		}
		name := key
		var override struct {
			XGoName string `json:"x-go-name"`
		}
		if json.Unmarshal(val, &override) == nil && override.XGoName != "" {
			name = override.XGoName
		}
		fields = append(fields, GoField{Name: name, Type: schemaTypeName(val, key)})
	}
	return fields
}

// slotForKind reads the named slot for kind off the Project aggregate. The kind→slot
// mapping is split by lifecycle phase (system-design vs project-design kinds) purely to
// keep each switch under the gocyclo gate; the union covers every ArtifactKind.
func slotForKind(p projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	if slot, ok := designSlotForKind(p, kind); ok {
		return slot
	}
	return planSlotForKind(p, kind)
}

// designSlotForKind maps the Phase-1 (system-design) kinds to their Project slots.
func designSlotForKind(p projectstate.Project, kind projectstate.ArtifactKind) (projectstate.ArtifactSlot, bool) {
	switch kind {
	case projectstate.KindMission:
		return p.Mission, true
	case projectstate.KindGlossary:
		return p.Glossary, true
	case projectstate.KindScrubbedRequirements:
		return p.ScrubbedRequirements, true
	case projectstate.KindVolatilities:
		return p.Volatilities, true
	case projectstate.KindCoreUseCases:
		return p.CoreUseCases, true
	case projectstate.KindSystem:
		return p.SystemDesign, true
	case projectstate.KindOperationalConcepts:
		return p.OperationalConcepts, true
	case projectstate.KindStandardCheck:
		return p.StandardCheck, true
	case projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution, projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution, projectstate.KindRiskModel, projectstate.KindSdpReview:
		// project-design kinds — resolved by planSlotForKind.
		return projectstate.ArtifactSlot{}, false
	default:
		return projectstate.ArtifactSlot{}, false
	}
}

// planSlotForKind maps the Phase-2 (project-design) kinds to their Project slots.
func planSlotForKind(p projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	switch kind {
	case projectstate.KindPlanningAssumptions:
		return p.PlanningAssumptions
	case projectstate.KindActivityList:
		return p.ActivityList
	case projectstate.KindNetwork:
		return p.Network
	case projectstate.KindNormalSolution:
		return p.NormalSolution
	case projectstate.KindSubcriticalSolution:
		return p.SubcriticalSolution
	case projectstate.KindCompressedSolution:
		return p.CompressedSolution
	case projectstate.KindDecompressedSolution:
		return p.DecompressedSolution
	case projectstate.KindRiskModel:
		return p.RiskModel
	case projectstate.KindSdpReview:
		return p.SdpReview
	case projectstate.KindMission, projectstate.KindGlossary, projectstate.KindScrubbedRequirements,
		projectstate.KindVolatilities, projectstate.KindCoreUseCases, projectstate.KindSystem,
		projectstate.KindOperationalConcepts, projectstate.KindStandardCheck:
		// system-design kinds — designSlotForKind resolved them before this helper runs.
		return projectstate.ArtifactSlot{}
	default:
		return projectstate.ArtifactSlot{}
	}
}

// pipelineDefaultToolchain names the placeholder toolchain stamped on the design
// dispatch's logical step graph; the real design recipe lives in the user's
// aiarch-design.yml workflow file, so the step is only present to satisfy the RA's
// non-empty-step-graph pre-condition.
const pipelineDefaultToolchain = "go-1.23"

// ===========================================================================
// Workflow-side pipeline helpers. The temporalgen migration routes the submit/observe
// design-job pair through the GENERATED constructionPipelineAccess invokers (wf.Acts.
// PipelineSubmit/ObserveConstructionPipeline); the value mapping that lived on the folded
// pipelineDispatchAdapter — the RepoRef→RepoTarget decode, the PipelineSpec composition,
// and the RA-phase→neutral-phase mapping — is now these PURE workflow-side helpers
// (mirrors construction's dispatch.go). The idempotency key is stamped INSIDE the
// generated submit Activity (genActivityIdempotencyKey, the same run-scoped 3-part scheme
// the old hand-derived key used), so the redraft-vs-auto-retry distinction is unchanged.
// The former EXPORTED consumer-mirror interface + the folded pipelineDispatchAdapter +
// the neutral pipelineSpec/pipelineHandle carriers are RETIRED.
// ===========================================================================

// designRepoTarget decodes an opaque per-project RepoRef String() into the RA's
// infrastructure-neutral RepoTarget{Owner, Name} for the per-project-design-dispatch.
// An empty repoRef is the dormant-rail case → a zero RepoTarget (the RA falls back to
// the configured construction repo). A malformed ref surfaces as the RA's
// ContractMisuse (the dispatch Activity maps it to a terminal error). It uses
// sourcecontrol's own OwnerRepo accessor so the RepoRef encoding stays owned by
// sourceControlAccess (no encoding leak here).
//
// NOT promotable to projectstate (code-health-phase-bd task D3 verification): it needs
// constructionpipeline.RepoTarget + sourcecontrol.RepoRefOwnerRepo/RepoRefFromString —
// both sibling ResourceAccess packages, and TestMethodLayering forbids RA→RA sideways
// imports (the RA-layer analog of "no Manager→Manager sideways"). Stays duplicated
// per-manager alongside designBranch's twin.
func designRepoTarget(repoRef string) (constructionpipeline.RepoTarget, error) {
	if repoRef == "" {
		return constructionpipeline.RepoTarget{}, nil
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(sourcecontrol.RepoRefFromString(repoRef))
	if err != nil {
		return constructionpipeline.RepoTarget{}, err
	}
	return constructionpipeline.RepoTarget{Owner: owner, Name: name}, nil
}

// ===========================================================================
// Dispatch inputs (C-WF-DESIGN workflow_dispatch schema). These exact key names
// are the binding contract with aiarch-design.yml's workflow_dispatch.inputs.
// idempotency_token is RA-controlled and is NOT set here.
// ===========================================================================

const (
	dispatchInputArtifactKind = "artifact_kind"
	// dispatchInputCommand carries the .claude command slug the seated design job runs
	// (DesignCommandFor). It REPLACES the retired design_prompt input: the Method doctrine
	// that used to be composed into a prompt now lives in the command's method-assets, so
	// the Manager ships only the command NAME, not prose.
	dispatchInputCommand       = "command"
	dispatchInputTargetBranch  = "target_branch"
	dispatchInputPriorStateRef = "prior_state_ref"
	// dispatchInputJobMode discriminates a DRAFT job (the Action commits the typed
	// Kind model into the slot) from a CRITIQUE job (the Action commits the slot's
	// critiqueVerdict / critiqueNotes read-back carrier — D-MSD-Δ amendment). The
	// Action template branches its commit-target instruction on this value. Defaulted
	// to "draft" in the template so a job dispatched without it (e.g. a UC2 draft)
	// behaves exactly as before.
	dispatchInputJobMode = "job_mode"
)

// Job-mode dispatch values. These exact strings are a contract with the
// aiarch-design.yml template's job_mode input.
const (
	jobModeDraft    = "draft"
	jobModeCritique = "critique"
	// jobModeAnswer is the question-comments answer job: the addressed role (pm/architect)
	// answers open QUESTION ledger entries in place via respondToReviewComment (no
	// putDraftModel, no setCritiqueVerdict). Like critique, it does NOT open a PR.
	jobModeAnswer = "answer"
)

// designBranch PROMOTED to projectstate.DesignBranch (code-health-phase-bd task D3) —
// byte-identical pure resolver, no longer duplicated with projectdesign's twin.

// dispatchActivityOptions is the option preset for the generated
// constructionPipelineAccess.submitConstructionPipeline Activity (consumed by the manager's
// option hook — workermanifest.go). A transient submit error (ErrTransient / Retryable)
// auto-retries via this RetryPolicy; a terminal RA fault (ContractMisuse / Auth /
// QuotaExhausted) is non-retryable and surfaces to the workflow body. A PhaseFailed is NOT
// a dispatch error — it is a successful observation of a failed job (§0d.4).
func dispatchActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:     30 * time.Second,
		MaxAttempts: 5,
		TerminalRA:  []fwra.Kind{fwra.ContractMisuse, fwra.Auth, fwra.QuotaExhausted},
	}.Options()
}

// observeActivityOptions is the option preset for the generated
// constructionPipelineAccess.observeConstructionPipeline Activity. Transient reads retry;
// a NotFound (GC'd handle) is non-retryable and surfaces.
func observeActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// designWorkflowFileName is the per-project DESIGN workflow file the agentic design
// dispatch must target (per-project-design-dispatch) — the BASENAME of
// sourcecontrol.DesignWorkflowPath (".github/workflows/aiarch-design.yml"), i.e.
// "aiarch-design.yml". Derived from the RA's single source of truth so the dispatch
// target and the project-birth workflow-file seat can never drift. This is the
// workflow file the design dispatch selects in place of the construction default
// (aiarch-construct.yml).
var designWorkflowFileName = path.Base(sourcecontrol.DesignWorkflowPath)

// mintCredActivityOptions — the credential mint (generated sourceControlAccess.
// getInstallationToken). A rejected/expired App identity is terminal. Feeds the manager's
// option hook (workermanifest.go).
func mintCredActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.ContractMisuse},
	}.Options()
}

// railActivityOptions — the generated sourceControlAccess PR-rail ops, including
// syncManagedScaffold (B10). Auth + a merge Conflict (not-mergeable) + bad input are
// terminal; transport/rate-limit retry. Feeds the manager's option hook (workermanifest.go),
// keyed by each op's generated activity name.
// scaffoldSyncActivityOptions carries a StartToClose long enough for a FULL scaffold
// converge — ~100 file reads plus up to a whole-tree of contents-API writes on a torn
// or version-bumped repo (F-QA2-36 addendum: the shared 30s rail deadline expired
// mid-loop and the sync only progressed via retry-persisted writes). The sync is
// resumable/idempotent (manifest written last), so a long deadline is safe.
func scaffoldSyncActivityOptions() workflow.ActivityOptions {
	o := railActivityOptions()
	o.StartToCloseTimeout = 5 * time.Minute
	return o
}

func railActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.NotFound, fwra.Conflict, fwra.ContractMisuse},
	}.Options()
}

// reviewledger.go holds the durable review-ledger seam for the systemDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05): the projectstate.ReviewComment
// ↔ ReviewCommentView projection the sessionState Query surfaces, the open-comment gate
// the approve precondition reads, and the SetReviewCommentStatus branch mutation. The
// ledger STORAGE + transition rules live in projectstate (reviewthread.go); the branch
// mutation itself is the GENERATED designSessionAccess.setReviewCommentStatusOnBranch /
// seedReviewCommentsOnBranch invoker (B10) — this file is only the Manager-side wiring
// (the wire-view projections, the reject/seed comment shaping, and the workflow-side
// apply/reload helpers).

// reviewAuthorRole is the role stamped on every comment the architect files at the
// System-Design review gate. The ledger records WHO filed each comment; in the design
// phase the reviewer at the gate is always the architect.
const reviewAuthorRole = "architect"

// toReviewCommentView projects one stored ledger entry onto its wire view.
func toReviewCommentView(c projectstate.ReviewComment) ReviewCommentView {
	return ReviewCommentView{
		ID:         c.ID,
		Anchor:     c.Anchor,
		AnchorText: c.AnchorText,
		Text:       c.Text,
		AuthorRole: c.AuthorRole,
		Round:      c.Round,
		Status:     c.Status,
		Response:   c.Response,
		Type:       c.Type,
		Addressee:  c.Addressee,
	}
}

// reviewThreadToView projects the durable ledger onto the wire thread the sessionState
// Query returns (nil stays nil so the omitempty wire shape is unchanged for slots that
// never carried a comment).
func reviewThreadToView(thread []projectstate.ReviewComment) []ReviewCommentView {
	if len(thread) == 0 {
		return nil
	}
	out := make([]ReviewCommentView, 0, len(thread))
	for _, c := range thread {
		out = append(out, toReviewCommentView(c))
	}
	return out
}

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (systemDesignManager.md §6.1/§6.2/§6.5).
// TaskQueue is defined in the generated worker.gen.go.
// ---------------------------------------------------------------------------

// Signal and query names (systemDesignManager.md §6.5).
const (
	// signalReviewDecision resumes a suspended CoAuthorArtifactWorkflow at the
	// AwaitingReview gate; backs submitReviewDecision.
	signalReviewDecision = "reviewDecision"
	// lSignalRedraft resumes a CoAuthorArtifactWorkflow that ended a draft attempt in
	// the StageRefused terminal-but-live state (a terminal worker fault: the LLM
	// worker is unavailable / out of credits, or produced an unconstructable
	// response). It re-enters the draft loop in the SAME live workflow so the user's
	// "Retry draft" recovers without a fresh run. Backs requestArtifactDraft's retry
	// path (signal-with-start; systemDesignManager.md §2.1).
	lSignalRedraft = "redraft"
	// querySessionState returns a SessionStateView; backs getSessionState.
	querySessionState = "sessionState"
	// signalSetCommentStatus resumes a CoAuthorArtifactWorkflow suspended at the
	// AwaitingReview gate to apply a durable review-ledger status transition
	// (open->waived / addressed->open) to one comment on the session branch; backs
	// SetReviewCommentStatus (review-ledger feature).
	signalSetCommentStatus = "setCommentStatus"
)

// ExecutionKinds for the durable-execution control plane (systemDesignManager.md §6.2).
const (
	// executionKindPhase is the PARENT SystemDesignPhaseWorkflow (2026-05-29), the
	// ordered 7-step Phase-1 sequence started by startSystemDesign.
	executionKindPhase = "systemDesignPhase"
	// executionKindCoAuthor is the per-step child CoAuthorArtifactWorkflow gate.
	executionKindCoAuthor = "systemDesignCoAuthor"
	// executionKindPhaseAdvance is the short-lived phase-seal gating workflow.
	executionKindPhaseAdvance = "systemDesignPhaseAdvance"
)

// workflows is the single systemDesignManager component struct. It holds ALL the
// downstream dependencies the Manager orchestrates and is BOTH the workflow
// receiver and the activity receiver — there is no separate Activities type.
//
// How the two dependency kinds are reached differs by their determinism class,
// per the contract (systemDesignManager.md §6.3/§6.4):
//
//   - Validator (artifactValidationEngine) is a PURE, deterministic Engine, so
//     the workflow body calls its named verbs DIRECTLY — replay-safe, no Activity
//     wrapper (artifactValidationEngine.md §2.1).
//   - ProjectState / Workers are I/O ResourceAccess ports and are NON-deterministic.
//     They are fields here, but the workflow MUST NOT call them on the workflow
//     goroutine. Instead the workflow invokes the Activity methods on this same
//     struct via workflow.ExecuteActivity (activities.go).
//
// 2026-06-15 agentic-pivot re-cut (systemDesignManager.md §0d / D-MSD-Δ): the
// drafting MECHANISM flips from a synchronous worker call to an ASYNC dispatch →
// observe → read-back round-trip. DRAFT and PM-CRITIQUE no longer call
// workerAccess.GenerateTypedData in-process; instead the Manager DISPATCHES a
// claude-code-action DESIGN job via Pipeline (constructionPipelineAccess), OBSERVES
// it to a typed terminal phase, and READS BACK the typed model the Action committed
// via ProjectState.ReadProject. aiarch makes NO synchronous LLM call and writes NO
// draft JSON on the main path (the Action commits it inside the user's CI; the
// required CI validation check is the trust boundary).
//
//   - Pipeline (constructionPipelineAccess) — submit + observe, both Activity-
//     wrapped (I/O). The claude-code-action job runs OUTSIDE aiarch's call graph
//     (user's CI, user's token).
//   - ProjectState — read-back of the committed Kind + the human-gate thin-writes
//     (stage/commit/reject/withdraw/advancePhase), all Activity-wrapped.
//
// DROPPED from the draft path (server-shrink §1/§2): workerAccess (no synchronous
// LLM call survives) and artifactValidationEngine (validation is now the required
// CI check inside the Action, surfaced as the job's terminal phase). They are
// removed from this struct.
//
// Rendering is not a server concern: server-side rendering was removed (the
// client renders the typed models the query/head-state expose), so there is no
// Rendering field here.
type workflows struct {
	// Acts is the GENERATED typed invoker surface (invokers.gen.go) — the workflow's call
	// surface for EVERY contract-backed RA op this Manager reaches: projectStateAccess
	// readProjectVersion / advancePhase, the constructionPipelineAccess submit/observe
	// design-job pair, the six sourceControlAccess PR-rail verbs plus syncManagedScaffold,
	// and the eight designSessionAccess verbs (the envelope-parameter Stage op, the
	// branch-aware read-back/commit/reject/withdraw/reconcile/review-ledger mutations —
	// B10). Each invoker consults the manager's per-op preset hook (workermanifest.go
	// activityOptions), keyed by the generated activity name. This Manager carries NO RA
	// dep of its own — every Activity it executes is generated (B10: the systemdesign
	// rewire deleted the last custom Activities; activities_custom.go / errors.go are
	// gone, and reviewledger.go/gitrail.go keep only non-Activity value carriers).
	Acts genInvokers

	// Rail + Repo are the OPTIONAL git-forward PR rail (I-DESIGN-DISPATCH §2b). When
	// both are non-nil AND a repo resolves for the project, the CoAuthor spine wraps
	// each draft in the settled branch→PR→read-back→+1→merge model: ensure the session
	// branch, open a PR (head=sessionBranch, base=main), read back + stage on the
	// session branch, then on Approve guard-check + relay the +1 + merge to main before
	// committing on main. When either is nil (the Postgres/non-git composition, or every
	// existing test) the spine runs UNCHANGED — read-back/stage on main, no branch/PR
	// ops — so the branch-aware path is purely additive and dormant-when-unwired,
	// exactly like the construction Manager's git-forward slice.
	//
	// Rail is the PUBLISHED sourceControlAccess RA. Every rail verb (including
	// syncManagedScaffold, since B10) is reached through the generated invoker surface
	// (wf.Acts.Rail*); this field is held directly ONLY for the nil/dormant gitEnabled
	// gate — a plain presence/absence check, never a call.
	Rail sourcecontrol.SourceControlAccess
	// Repo resolves the per-project RepoRef the rail verbs address. nil ⇒ the rail is
	// dormant. Injected so the repo-resolution policy is swappable without a new RA edge.
	Repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)
}

// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
// loop (D-PA §6/§7). A stale expectedVersion surfaces as fwra.Conflict
// (non-retryable per the fixed framework enum). The idempotency key is stable per
// Activity invocation, so a re-apply that races a prior committed attempt
// collapses to an idempotent no-op success. The bound guards a write-contention
// pathology. A pure in-workflow guard.
const maxMutateConflictAttempts = 20

// Activity option presets (systemDesignManager.md §6.4). Concrete RetryPolicy / timeout
// choices live here, in the Manager. Each preset is exposed as an ActivityOptions VALUE,
// consumed by the generated-invoker option hook (workermanifest.go activityOptions),
// keyed by the generated activity name — every Activity this Manager executes is
// generated (B10), so no ctx-wrapper form is needed anymore.

// readProjectActivityOptions is the preset for the generated
// designSessionAccess.readProjectOnBranch and projectStateAccess.readProjectVersion ops.
func readProjectActivityOptions() workflow.ActivityOptions {
	// BOUND the read retries. A read that faults RETRYABLY (Transient / Infrastructure /
	// RateLimited) must NOT loop forever — pre-fix a decode failure of committed state
	// was mis-classified Infrastructure and retried every ~100s indefinitely with no
	// failure surface (QA F36). Decode failures are now TERMINAL (ContractMisuse, listed
	// below), but a GENUINE persistent infra outage must still surface rather than wedge
	// invisibly, so cap the attempts.
	return fwmanager.ActivityPreset{
		Timeout:     10 * time.Second,
		MaxAttempts: 8,
		TerminalRA:  []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// mutateActivityOptions is the preset for the head-state mutation ops (the generated
// designSessionAccess Stage / Commit / Reject / Withdraw / Reconcile / review-ledger
// verbs) and the generated projectStateAccess.advancePhase. Retry Transient via the
// Activity RetryPolicy; Conflict is handled by the workflow-level re-read→re-apply loop
// (D-PA §6/§7). Terminal on ContractMisuse.
func mutateActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
}

// raConflictErrType is the canonical Temporal Type() a head-state mutation
// Activity surfaces when the optimistic-concurrency token (expectedVersion) is
// stale. The workflow recovers with the bounded re-read→re-apply loop.
var raConflictErrType = fwmanager.RAErrType(fwra.Conflict)

// raNotFoundErrType is the canonical Temporal Type() the ReadProject Activity
// surfaces when the addressed aggregate has NO row yet — a brand-new project.
var raNotFoundErrType = fwmanager.RAErrType(fwra.NotFound)

// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

// isReadNotFound reports whether err is the ReadProject Activity's "no row yet"
// NotFound (a brand-new project).
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// systemDesignPhaseWorkflowID derives the parent continuity token:
// {projectId}:systemDesign (systemDesignManager.md §2.0).
func systemDesignPhaseWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:systemDesign", projectID)
}

// coAuthorWorkflowID derives the continuity token for a per-artifact co-authoring
// workflow: {projectId}:{artifactKind} (systemDesignManager.md §6.1).
func coAuthorWorkflowID(projectID ProjectID, kind ArtifactKind) string {
	return fmt.Sprintf("%s:%d", projectID, int(kind))
}

// coAuthorInput is the start payload for CoAuthorArtifactWorkflow.
type coAuthorInput struct {
	ProjectID    ProjectID
	ArtifactKind ArtifactKind
	// Feedback is the optional re-request feedback for the explicit
	// withdraw-then-redraft-with-notes path (systemDesignManager.md §2.1, OQ6).
	Feedback *ReviewFeedback
	// Amendment is the AMENDMENT-session index (F38/F40 founder ruling 2026-07-05).
	// 0 = the original review session (branch aiarch-design/<project>/<kind>). N>0 =
	// the Nth reopening of an already-COMMITTED artifact — a fresh session whose v1
	// branch/PR already merged, so it drafts on a NEW branch (…-amend-N). Constant for
	// the life of a workflow run, so the session branch is STABLE across every redraft.
	//
	// INVARIANT (set by the manager's amendmentIndexFor): N >= 1 IFF the slot was COMMITTED
	// at request time — the amendment condition. The manager floors a committed slot to 1
	// (a slot committed before the Revisions field existed reads Revisions=0 but is still an
	// amendment). So the spine's "Amendment > 0" checks (branch suffix, amendment prompt
	// framing, and the maybeSeedAmendment ledger seed) are a faithful proxy for "amendment"
	// and fire for EVERY committed slot, including pre-field ones.
	Amendment int
}

// coAuthorOutcome is the child gate's terminal report to the parent — whether the
// step's human gate approved (advance) or withdrew (halt).
type coAuthorOutcome int

const (
	coAuthorUnknown coAuthorOutcome = iota
	coAuthorApproved
	coAuthorWithdrawn
)

// phaseAdvanceWorkflowID derives the continuity token for the short-lived gating
// workflow: {projectId}:phaseAdvance (systemDesignManager.md §6.1).
func phaseAdvanceWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:phaseAdvance", projectID)
}

// slotFor returns the named Project slot for a Phase-1 kind.
func slotFor(proj projectstate.Project, kind ArtifactKind) projectstate.ArtifactSlot {
	switch kind {
	case KindMission:
		return proj.Mission
	case KindGlossary:
		return proj.Glossary
	case KindScrubbedRequirements:
		return proj.ScrubbedRequirements
	case KindVolatilities:
		return proj.Volatilities
	case KindCoreUseCases:
		return proj.CoreUseCases
	case KindSystem:
		return proj.SystemDesign
	case KindOperationalConcepts:
		return proj.OperationalConcepts
	case KindStandardCheck:
		return proj.StandardCheck
	case KindPlanningAssumptions, KindActivityList, KindNetwork, KindNormalSolution,
		KindSubcriticalSolution, KindCompressedSolution, KindDecompressedSolution,
		KindRiskModel, KindSdpReview:
		// Phase-2 kinds have no Phase-1 slot here — same zero-value fallback as
		// the default below (this func is only ever called with Phase-1 kinds).
		return projectstate.ArtifactSlot{}
	default:
		return projectstate.ArtifactSlot{}
	}
}

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the systemDesignManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the three workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go). This Manager has ZERO
// custom Temporal Activities (B10: the last ones — the projectEnvelope-codec reads, the
// head-state mutation writes carrying the BranchAware/Ledger/Provenance/Reconciling
// capability type-assertions, the review-ledger branch mutations, and the free-function
// managed-scaffold sync — were deleted when their call sites migrated onto the generated
// designSessionAccess / sourceControlAccess.syncManagedScaffold invokers); every Activity
// is generated and registered by the generated RegisterWorker.
//
// The Engine dependencies are called DIRECTLY in-workflow (deterministic, by value) and
// are NOT Activities; the durable-execution in-workflow primitives (awaitSignal /
// startTimer) are the Manager's own code.

// activityOptions returns the option-preset hook the generated invokers consult for
// EVERY Activity this Manager executes (projectState / pipeline / rail / designSession —
// this is the complete set). A name with no entry falls back to the generated default
// (invokers.gen.go). Keyed by the generated registered activity name
// (<componentKey>.<opName>); each designSessionAccess.* entry uses the same
// readProjectOpts/mutateOpts preset as the equivalent projectStateAccess entry, and
// syncManagedScaffold uses the same railOpts preset as the other rail entries.
func activityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"projectStateAccess.readProjectVersion":                  readProjectActivityOptions(),
		"projectStateAccess.advancePhase":                        mutateActivityOptions(),
		"constructionPipelineAccess.submitConstructionPipeline":  dispatchActivityOptions(),
		"constructionPipelineAccess.observeConstructionPipeline": observeActivityOptions(),
		"sourceControlAccess.getInstallationToken":               mintCredActivityOptions(),
		"sourceControlAccess.openBranch":                         railActivityOptions(),
		"sourceControlAccess.openPullRequest":                    railActivityOptions(),
		"sourceControlAccess.getPullRequestStatus":               railActivityOptions(),
		"sourceControlAccess.postReview":                         railActivityOptions(),
		"sourceControlAccess.mergePullRequest":                   railActivityOptions(),
		"sourceControlAccess.syncManagedScaffold":                scaffoldSyncActivityOptions(),
		"designSessionAccess.readProjectOnBranch":                readProjectActivityOptions(),
		"designSessionAccess.stageArtifactForReviewOnBranch":     mutateActivityOptions(),
		"designSessionAccess.commitArtifactWithProvenance":       mutateActivityOptions(),
		"designSessionAccess.rejectArtifactOnBranchWithComments": mutateActivityOptions(),
		"designSessionAccess.withdrawArtifactOnBranch":           mutateActivityOptions(),
		"designSessionAccess.reconcileBranchFromMain":            mutateActivityOptions(),
		"designSessionAccess.setReviewCommentStatusOnBranch":     mutateActivityOptions(),
		"designSessionAccess.seedReviewCommentsOnBranch":         mutateActivityOptions(),
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the three workflow bodies under their registered names, the per-activity option-preset
// hook, and the genActivities threaded from the impl's stored published deps.
//
// The workflows receiver holds the generated invoker surface (Acts) — every contract-
// backed RA op (readProjectVersion / advancePhase / submit / observe / the seven rail
// verbs / the eight designSession verbs) is reached through it — plus the published Rail
// (held directly ONLY for the nil/dormant gitEnabled gate) and Repo. The receiver carries
// no RA dep of its own; every Activity it executes is generated (B10).
func (m *systemDesignManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()

	wf := &workflows{
		Acts: genInvokers{Opts: optsHook},
		// Rail is the PUBLISHED sourceControlAccess: nil ⇒ the PR rail is dormant and the
		// CoAuthor spine runs the original main-path behavior. Held directly ONLY for the
		// gitEnabled gate; every rail verb (including syncManagedScaffold) goes through the
		// generated invoker surface (wf.Acts.Rail*).
		Rail: m.rail,
		Repo: m.repo,
	}

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindPhase, Fn: wf.SystemDesignPhaseWorkflow},
			{Name: executionKindCoAuthor, Fn: wf.CoAuthorArtifactWorkflow},
			{Name: executionKindPhaseAdvance, Fn: wf.PhaseAdvanceWorkflow},
		},
		// Every Activity this Manager's workflows execute is generated, so the generated
		// RegisterWorker registers the complete set — no explicit custom-Activity
		// registration remains (B10).
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:  m.projectState,
			Pipeline:      m.pipeline,
			Rail:          m.rail,
			DesignSession: m.designSession,
		},
	}
}

// RegisterManagerWorker wires the systemDesignManager onto a Temporal Worker polling the
// system-design task queue (systemDesignManager.md §6.1). It preserves the external call
// shape the composition root used before the generated-layer migration, asserting to the
// concrete *systemDesignManager the generated constructor returns and delegating to the
// generated RegisterWorker with the impl's WorkerManifest.
func RegisterManagerWorker(w worker.Worker, m SystemDesignManager) {
	impl, ok := m.(*systemDesignManager)
	if !ok {
		panic("systemdesign: RegisterManagerWorker requires a *systemDesignManager from NewSystemDesignManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}
