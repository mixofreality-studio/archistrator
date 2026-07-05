package projectdesign

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billing "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (projectDesignManager.md §6.1/§6.2/§6.5).
// ---------------------------------------------------------------------------

// TaskQueue is the one queue per Manager that the in-process Temporal Worker in
// the server polls (contract §6.1; the operational-concepts.md house spelling).
const TaskQueue = "project-design"

// Signal and query names (contract §6.5).
const (
	// signalReviewDecision resumes a suspended CoAuthorPhase2ArtifactWorkflow at
	// the per-artifact AwaitingReview gate; backs submitReviewDecision (OQ-3).
	signalReviewDecision = "reviewDecision"
	// signalSetCommentStatus resumes a suspended CoAuthorPhase2ArtifactWorkflow at the
	// AwaitingReview gate to apply a durable review-ledger status transition
	// (open->waived / addressed->open) to one comment on the session branch; backs
	// SetReviewCommentStatus (review-ledger feature).
	signalSetCommentStatus = "setCommentStatus"
	// signalRedraft resumes a CoAuthorPhase2ArtifactWorkflow that landed in the
	// StageDraftFailed recovery gate (a terminal Phase-2 design-job failure). It
	// re-enters the dispatch loop in the SAME live workflow so the user's "Retry
	// draft" recovers without a fresh run. Backs requestArtifactDraft's retry path
	// (signal-with-start; projectDesignManager.md §2.1 / §0.5.4).
	signalRedraft = "redraft"
	// signalSDPDecision resumes the AssembleSDPReviewWorkflow at the option-commit
	// gate; backs submitSDPDecision.
	signalSDPDecision = "sdpDecision"
	// querySessionState returns a SessionStateView; backs getSessionState.
	querySessionState = "sessionState"
)

// ExecutionKinds for the durable-execution control plane (contract §6.2).
const (
	// executionKindCoAuthor is the per-artifact Phase-2 co-authoring gate.
	executionKindCoAuthor = "projectDesignCoAuthor"
	// executionKindSDPReview is the UC2 SDP-review assembly + option-commit gate.
	executionKindSDPReview = "projectDesignSDPReview"
	// executionKindPhaseAdvance is the short-lived Phase-2 seal gating workflow.
	executionKindPhaseAdvance = "projectDesignPhaseAdvance"
)

// workflows is the single projectDesignManager component struct. It holds ALL the
// downstream dependencies the Manager orchestrates and is BOTH the workflow
// receiver and the activity receiver — there is no separate Activities type.
//
// How the dependency kinds are reached differs by their determinism class
// (contract §6.3/§6.4):
//
//   - Estimation, OperationEst, Settlement are PURE, deterministic Engines, so the
//     workflow body calls their verbs DIRECTLY — replay-safe, no Activity wrapper.
//     They STAY server-side in-workflow (§0.5.5 "RETAINED, unchanged"): they are
//     by-value joins, NOT LLM work, and do NOT become agentic dispatches.
//   - ProjectState (read-back + thin-writes) and Pipeline (constructionPipelineAccess
//     — submit + observe) are I/O ResourceAccess ports and are NON-deterministic.
//     They are fields here, but the workflow MUST NOT call them on the workflow
//     goroutine. Instead the workflow invokes the Activity methods on this same
//     struct via workflow.ExecuteActivity (activities.go / dispatch.go).
//
// 2026-06-15 agentic-pivot re-cut (projectDesignManager.md §0.5 / D-MPD-Δ): the
// Phase-2 plan-DRAFTING mechanism flips from a synchronous worker call to an ASYNC
// dispatch → observe → read-back round-trip. The per-artifact CoAuthorPhase2-
// ArtifactWorkflow no longer calls workerAccess.GenerateTypedData in-process; instead
// the Manager DISPATCHES a claude-code-action DESIGN job via Pipeline
// (constructionPipelineAccess), OBSERVES it to a typed terminal phase, and READS BACK
// the typed model the Action committed via ProjectState.ReadProject. aiarch makes NO
// synchronous LLM call and writes NO draft JSON on the main path.
//
// DROPPED from the draft path (§0.5.5): workerAccess (no synchronous LLM call
// survives; the in-flight cancel is constructionPipelineAccess.cancel) and
// artifactValidationEngine (Phase-2 validation is the required CI check inside the
// Action, surfaced as the job's terminal phase). Both are removed from this struct.
type workflows struct {
	Estimation   estimation.EstimationEngine
	OperationEst operationestimation.OperationEstimationEngine
	Settlement   billing.BillingEngine
	ProjectState projectstate.ProjectStateAccess
	Pipeline     constructionPipelineAccess

	// Rail + Repo are the OPTIONAL git-forward PR rail (I-DESIGN-DISPATCH §2b). When both
	// are non-nil AND a repo resolves, the per-artifact CoAuthorPhase2ArtifactWorkflow
	// draft path wraps each draft in the settled branch→PR→read-back→+1→merge model + the
	// branch-aware read-back/stage; when nil that path runs UNCHANGED (read-back/stage on
	// main, no branch/PR ops). The AssembleSDPReviewWorkflow (the in-workflow three-Engine
	// join) is UNCHANGED — it gets NO rail (only the per-artifact draft path does).
	Rail sourceControlRail
	// Repo resolves the per-project RepoRef the rail verbs address. nil ⇒ the rail is
	// dormant. Injected so the repo-resolution policy is swappable without a new RA edge.
	Repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)
}

// Activity name constants. The Activity methods are registered under these stable
// names (worker.go / the test suite), and the workflow bodies invoke them by the
// method value on wf, so the registered name and the call stay in lockstep.
const (
	actReadProject         = "ReadProjectActivity"
	actReadProjectVersion  = "ReadProjectVersionActivity"
	actReadProjectOnBranch = "ReadProjectOnBranchActivity"
	actDispatchDesignJob   = "DispatchDesignJobActivity"
	actObserveDesignJob    = "ObserveDesignJobActivity"
	actStageForReview      = "StageArtifactForReviewActivity"
	actCommitArtifact      = "CommitArtifactActivity"
	actRejectArtifact      = "RejectArtifactActivity"
	actWithdrawArtifact    = "WithdrawArtifactActivity"
	actAdvancePhase        = "AdvancePhaseActivity"
	// review-ledger: the human waive/reopen branch mutation.
	actSetReviewCommentStatus = "SetReviewCommentStatusActivity"
	actSeedReviewComments     = "SeedReviewCommentsActivity"

	// PR-rail Activity names (I-DESIGN-DISPATCH §2b).
	actMintRepoCredential   = "MintRepoCredentialActivity" // #nosec G101 -- Temporal activity NAME constant, not a credential
	actOpenBranch           = "OpenBranchActivity"
	actOpenPullRequest      = "OpenPullRequestActivity"
	actGetPullRequestStatus = "GetPullRequestStatusActivity"
	actPostReview           = "PostReviewActivity"
	actMergePullRequest     = "MergePullRequestActivity"
)

// maxSDPReassembleAttempts bounds the SDP RejectAll re-assemble loop (contract
// §6.3 step 7 — bound the loop like systemdesign's maxRedraftAttempts).
const maxSDPReassembleAttempts = 5

// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
// loop. The idempotency key is stable per Activity invocation, so a re-apply that
// races a prior committed attempt collapses to an idempotent no-op success.
const maxMutateConflictAttempts = 20

// Activity option presets (contract §6.4). Concrete RetryPolicy / timeout choices
// live here, in the Manager.
func readProjectOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			// BOUND the read retries so a RETRYABLE fault (Transient / Infrastructure /
			// RateLimited) cannot loop forever — decode failures of committed state are now
			// TERMINAL (ContractMisuse, below), but a genuine persistent infra outage must
			// still surface rather than wedge invisibly (QA F36, mirrors systemdesign).
			MaximumAttempts: 8,
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

func mutateOpts(ctx workflow.Context) workflow.Context {
	// Retry Transient via Activity RetryPolicy; Conflict is handled by the
	// workflow-level re-read→re-apply loop. Terminal on ContractMisuse.
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// raConflictErrType is the canonical Temporal Type() a head-state mutation Activity
// surfaces when the optimistic-concurrency token (expectedVersion) is stale.
var raConflictErrType = fwmanager.RAErrType(fwra.Conflict)

// raAuthErrType is the canonical Temporal Type() a rail Activity surfaces for an Auth
// fault. The platform github ClassifyStatus conflates GitHub secondary RATE-LIMIT 403s
// with real permission denials (both → fwra.Auth), and marks it NON-RETRYABLE — so the
// approve-window bounded retry (QA F35) must run WORKFLOW-SIDE (isApproveAuthFault), since
// the Activity RetryPolicy cannot retry a non-retryable ApplicationError.
var raAuthErrType = fwmanager.RAErrType(fwra.Auth)

// isApproveAuthFault reports whether err is a rail Auth fault (the rate-limit-403-as-Auth
// the approve-window bounded retry absorbs).
func isApproveAuthFault(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raAuthErrType
	}
	return false
}

// raContractMisuseErrType is the canonical Temporal Type() the read Activities surface when
// the committed state DECODES MALFORMED — the projectstate codec now classifies these
// ContractMisuse (terminal) rather than Infrastructure (QA F36). On a pure READ path a
// ContractMisuse is unambiguously a decode-of-committed-state failure (absence is NotFound).
var raContractMisuseErrType = fwmanager.RAErrType(fwra.ContractMisuse)

// isTerminalReadBack reports whether a read-back error is a TERMINAL decode-of-committed-
// state fault retry cannot fix, and returns the decode diagnostic (preserved as the
// ApplicationError message by fwmanager.MapError) so the caller can surface it at the human
// StageDraftFailed gate instead of looping the read-back Activity forever (QA F36).
func isTerminalReadBack(err error) (string, bool) {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.Type() == raContractMisuseErrType {
		return appErr.Message(), true
	}
	return "", false
}

// raNotFoundErrType is the canonical Temporal Type() the ReadProject Activity
// surfaces when the addressed aggregate has NO row yet.
var raNotFoundErrType = fwmanager.RAErrType(fwra.NotFound)

// readProject runs the ReadProject Activity and returns the whole head-state
// aggregate. A brand-new project surfaces fwra.NotFound (see isReadNotFound).
func (wf *workflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
	c := readProjectOpts(ctx)
	var pe projectEnvelope
	if err := workflow.ExecuteActivity(c, wf.ReadProjectActivity, projectstate.ProjectID(projectID)).Get(ctx, &pe); err != nil {
		return projectstate.Project{}, err
	}
	return pe.decode()
}

// readVersion runs the cheap ReadProjectVersion Activity and returns only the
// head-state optimistic-concurrency token — the single value the Conflict re-read
// loop needs to seed its next attempt. A brand-new project surfaces fwra.NotFound
// (see isReadNotFound). Replaces the wasteful whole-aggregate read that shipped the
// entire encoded Project across the Temporal Activity boundary for a uint64.
func (wf *workflows) readVersion(ctx workflow.Context, projectID ProjectID) (projectstate.Version, error) {
	c := readProjectOpts(ctx)
	var v projectstate.Version
	if err := workflow.ExecuteActivity(c, wf.ReadProjectVersionActivity, projectstate.ProjectID(projectID)).Get(ctx, &v); err != nil {
		return 0, err
	}
	return v, nil
}

// readVersionOnBranch returns the optimistic-concurrency token of the substrate the
// mutation targets (I-DESIGN-DISPATCH §2a). A branch mutation (stage / reject during the
// AwaitingReview window) advances the SESSION BRANCH, so its Conflict re-read must read
// THAT branch — not main, whose version trails. branch=="" reads main exactly as before.
// This is the fix for QA F29: a Conflict on a branch mutation that re-read main could
// never converge (main's version never catches up to the branch's), wedging the bounded
// loop into a non-retryable MutateConflictExhausted crash.
func (wf *workflows) readVersionOnBranch(ctx workflow.Context, projectID ProjectID, branch string) (projectstate.Version, error) {
	if branch == "" {
		return wf.readVersion(ctx, projectID)
	}
	p, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return 0, err
	}
	return p.Version, nil
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop. branch names the substrate the mutation targets so the
// Conflict re-read reads the RIGHT version (the session branch for a review-window branch
// mutation, main for a main mutation) — see readVersionOnBranch (QA F29). branch=="" is
// the original main-only behavior every existing caller relied on.
func (wf *workflows) applyRecovering(
	ctx workflow.Context,
	projectID ProjectID,
	branch string,
	seed projectstate.Version,
	apply func(expected projectstate.Version) (projectstate.Version, error),
) (projectstate.Version, error) {
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		v, rerr := wf.readVersionOnBranch(ctx, projectID, branch)
		if rerr != nil {
			if isReadNotFound(rerr) {
				expected = 0
				continue
			}
			return 0, rerr
		}
		expected = v
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "branch", branch, "nextExpectedVersion", expected)
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

// isReadNotFound reports whether err is the ReadProject Activity's "no row yet"
// NotFound (a brand-new project).
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
}

// ===========================================================================
// (A) CoAuthorPhase2ArtifactWorkflow — the per-artifact spine (contract §6.3 A /
// §0.5.2; mirrors systemdesign's CoAuthorArtifactWorkflow agentic pivot). Loop until
// Approve/Withdraw:
//
//  1. readProject              -> head-state (prior committed typed slots + Version)
//  2. COMPOSE the Phase-2 architect-role prompt IN-MEMORY (prompts.go) — never
//     persisted; on a redraft the ReviewFeedback.Notes are woven in.
//  3. DISPATCH -> OBSERVE -> READ-BACK (agentic pivot): dispatch a claude-code-action
//     DESIGN job via Pipeline.SubmitConstructionPipeline (FROZEN verb), observe it to a
//     TYPED terminal phase, and on PhaseSucceeded read back the typed Phase-2 model the
//     Action committed via ProjectState.ReadProject. On a terminal FAILURE phase the
//     session lands in StageDraftFailed and suspends at the human gate (the anti-wedge
//     rule, §0.5.4) — never a perpetual Drafting. There is NO PM-critique in Phase 2.
//     (Phase-2 validation is the required CI check inside the Action, surfaced as the
//     observed terminal phase — artifactValidationEngine + workerAccess DROPPED.)
//  4. stageArtifactForReview   -> carry the read-back TYPED model into its slot (AwaitingReview)
//  5. awaitSignal(reviewDecision) -> suspend durably
//  6. Approve  -> commitArtifact(kind); Reject -> loop to a fresh dispatch with feedback;
//     Withdraw -> withdrawArtifact
// ===========================================================================

// coAuthorWorkflowID derives the continuity token for a per-artifact co-authoring
// workflow: {projectId}:{int(kind)} (contract §6.1).
func coAuthorWorkflowID(projectID ProjectID, kind ArtifactKind) string {
	return fmt.Sprintf("%s:%d", projectID, int(kind))
}

// coAuthorInput is the start payload for CoAuthorPhase2ArtifactWorkflow.
type coAuthorInput struct {
	ProjectID    ProjectID
	ArtifactKind ArtifactKind
	// Feedback is the optional re-request feedback for the explicit
	// re-draft-with-notes path.
	Feedback *ReviewFeedback
	// Amendment is the AMENDMENT-session index (F38/F40 founder ruling 2026-07-05).
	// 0 = the original review session (branch aiarch-design/<project>/<kind>). N>0 = the
	// Nth reopening of an already-COMMITTED artifact — a fresh session whose v1 branch/PR
	// already merged, so it drafts on a NEW branch (…-amend-N). Constant for the life of a
	// workflow run, so the session branch is STABLE across every redraft.
	Amendment int
}

// coAuthorOutcome is the workflow's terminal report — whether the human gate
// approved or withdrew.
type coAuthorOutcome int

const (
	coAuthorUnknown coAuthorOutcome = iota
	coAuthorApproved
	coAuthorWithdrawn
)

// coAuthorStep is the control-flow verdict a co-author loop helper returns to the
// CoAuthorPhase2ArtifactWorkflow driver, so the workflow-command sequence stays
// byte-for-byte identical to the pre-extraction inline body:
//   - coAuthorProceed  → fall through to the next block this iteration
//   - coAuthorContinue → restart the loop (redraft)
//   - coAuthorReturn   → terminate the workflow with the returned outcome
//   - coAuthorReAwait  → re-suspend at the SAME AwaitingReview gate WITHOUT redrafting
//     (QA F35): a transient approve/merge-window fault is contained; the staged draft is
//     intact, so the human re-approves. A redraft would discard an approved-quality draft.
type coAuthorStep int

const (
	coAuthorProceed coAuthorStep = iota
	coAuthorContinue
	coAuthorReturn
	coAuthorReAwait
)

// reviewDecisionSignal is the reviewDecision signal payload (contract §6.5).
type reviewDecisionSignal struct {
	Decision ReviewDecision
	Feedback *ReviewFeedback
}

// redraftSignal is the redraft signal payload — the "Retry draft" lever delivered to
// a CoAuthorPhase2ArtifactWorkflow suspended in the StageDraftFailed recovery gate
// (requestArtifactDraft's retry path). Feedback is the optional re-request feedback
// woven into the next draft dispatch.
type redraftSignal struct {
	Feedback *ReviewFeedback
}

func (wf *workflows) CoAuthorPhase2ArtifactWorkflow(ctx workflow.Context, in coAuthorInput) (coAuthorOutcome, error) {
	// The SDP review is NOT co-authored here — it is assembled by
	// AssembleSDPReviewWorkflow (contract §2.1 rejects KindSdpReview at the façade,
	// belt-and-suspenders here).
	if in.ArtifactKind == KindSdpReview {
		return coAuthorUnknown, temporal.NewNonRetryableApplicationError(
			"the SDP review is assembled, not co-authored; use RequestSDPCommit",
			"WrongArtifactKind", nil)
	}

	// Live technical state backing the sessionState Query.
	state := &coAuthorState{
		projectID:    in.ProjectID,
		artifactKind: in.ArtifactKind,
		stage:        StageDrafting,
	}
	if err := workflow.SetQueryHandler(ctx, querySessionState, state.view); err != nil {
		return coAuthorUnknown, err
	}

	// Carry expectedVersion forward in workflow state (read-your-writes).
	var headVersion projectstate.Version

	// Step 1: read the project head-state once (prior typed models + version).
	var proj projectstate.Project
	if p, err := wf.readProject(ctx, in.ProjectID); err != nil {
		if !isReadNotFound(err) {
			return coAuthorUnknown, err
		}
		proj = projectstate.Project{ID: projectstate.ProjectID(in.ProjectID)}
	} else {
		proj = p
		headVersion = p.Version
	}

	feedback := ""
	if in.Feedback != nil {
		feedback = in.Feedback.Notes
	}

	// redraftCount bounds the attempt label progression and drives the StageRedrafting
	// vs StageDrafting query stage. A pure in-workflow guard.
	redraftCount := 0

	// reviewRound is the monotonic REJECT-round counter within THIS session (F40). The
	// session now commits to ONE persistent branch (no branch-per-attempt), so this counter
	// no longer selects a branch — it survives ONLY to stamp the durable review-ledger
	// comment ids (r{round}c{n}) so a fresh reject's comments do not collide with a prior
	// round's on the SAME accumulating thread. Bumped only on an AwaitingReview-gate REJECT.
	reviewRound := 0

	// amendmentSeeded guards the one-time F38 ledger seed (Phase-2 twin): when this is an
	// amendment session (in.Amendment > 0) the reopening feedback is recorded as round-0 OPEN
	// ledger entries right after the first stage.
	amendmentSeeded := false

	for {
		// --- DRAFT round-trip: dispatch -> observe -> read-back (agentic pivot) ---
		// coAuthorDraftRound composes the Phase-2 architect prompt IN-MEMORY, dispatches +
		// observes the DESIGN job, opens the PR, reads back the typed model, and stages it
		// for review. On a terminal FAILURE phase it lands the session in StageDraftFailed
		// and suspends at the human gate (the anti-wedge rule, §0.5.4) — never a perpetual
		// Drafting. The begun git session is written through &gf for the approve/merge step.
		var gf gitSession
		step, outcome, err := wf.coAuthorDraftRound(ctx, in, proj, &feedback, &headVersion, &redraftCount, state, &gf)
		if err != nil {
			return coAuthorUnknown, err
		}
		if step == coAuthorReturn {
			return outcome, nil
		}
		if step == coAuthorContinue {
			continue
		}

		// F38 AMENDMENT SEED: on the first stage of an amendment session, record the reopening
		// feedback as round-0 OPEN ledger entries on the staged session branch. Once only.
		amendmentSeeded = wf.maybeSeedAmendment(ctx, in, gf, &headVersion, amendmentSeeded, state)

		// Step 6/7: the review gate. Await a decision and act on it. An approve/merge-window
		// fault is CONTAINED as coAuthorReAwait (QA F35): the staged draft is intact, so we
		// re-suspend at THIS gate (carrying a queryable notice) and await the next decision —
		// the human re-approves — WITHOUT redrafting. Reject/withdraw exit this inner loop
		// (reject → break to the outer loop which redrafts; withdraw → return).
	gate:
		for {
			// REVIEW LEDGER: multiplex the review decision with the SetReviewCommentStatus
			// signal (waive / reopen). A status signal mutates the durable ledger on the branch
			// and re-suspends at THIS gate WITHOUT redrafting; a review decision proceeds as before.
			var sig reviewDecisionSignal
			var stSig setCommentStatusSignal
			var gotStatus bool
			sel := workflow.NewSelector(ctx)
			sel.AddReceive(workflow.GetSignalChannel(ctx, signalReviewDecision), func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &sig)
			})
			sel.AddReceive(workflow.GetSignalChannel(ctx, signalSetCommentStatus), func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &stSig)
				gotStatus = true
			})
			sel.Select(ctx)

			if gotStatus {
				wf.applyCommentStatus(ctx, in, gf, &headVersion, stSig, state)
				continue gate
			}

			step, outcome, err = wf.coAuthorApplyDecision(ctx, in, sig, &gf, &headVersion, &redraftCount, &reviewRound, &feedback, state)
			if err != nil {
				return coAuthorUnknown, err
			}
			switch step {
			case coAuthorReturn:
				return outcome, nil
			case coAuthorReAwait:
				continue gate
			case coAuthorContinue, coAuthorProceed:
				// coAuthorContinue (reject) — break to the outer loop which redrafts.
				// coAuthorProceed cannot arise from the decision phase; grouped defensively.
				break gate
			}
		}
	}
}

// coAuthorDraftRound runs one DRAFT round-trip of the co-author loop: it composes the
// architect prompt in-memory, dispatches + observes the design job, opens the PR, reads
// the committed model back off the session branch, and stages it for review. On a
// terminal job failure it lands the session in StageDraftFailed and suspends at the human
// gate (§0.5.4). The begun git session is written through *gf for the approve/merge step.
// The returned coAuthorStep tells the driver how to proceed (Proceed → await review;
// Continue → redraft; Return → terminate with the outcome). The sequence of workflow
// commands is identical to the pre-extraction inline body.
func (wf *workflows) coAuthorDraftRound(
	ctx workflow.Context,
	in coAuthorInput,
	proj projectstate.Project,
	feedback *string,
	headVersion *projectstate.Version,
	redraftCount *int,
	state *coAuthorState,
	gf *gitSession,
) (coAuthorStep, coAuthorOutcome, error) {
	logger := workflow.GetLogger(ctx)

	var draft projectstate.ArtifactModel
	state.stage = stageForAttempt(*redraftCount)

	// The ONE persistent SESSION BRANCH the Action drafts + commits + opens its PR on (F40).
	// STABLE across every redraft/reject round of this session; a fresh amendment session
	// selects a new branch via in.Amendment. Inert (just a string) when the rail is dormant.
	sessionBranch := designBranch(in.ProjectID, in.ArtifactKind, in.Amendment)

	// Rail (dispatch-time half): mint the credential + ensure the session branch
	// exists BEFORE the Action drafts on it. A dormant rail returns a disabled session
	// and the spine runs unchanged (read-back/stage on main, no branch/PR ops).
	begun, gerr := wf.beginSession(ctx, in.ProjectID, sessionBranch)
	if gerr != nil {
		return coAuthorProceed, coAuthorUnknown, gerr
	}
	*gf = begun

	// REVIEW LEDGER: on a redraft, state.reviewThread carries the durable open comments
	// (reloaded after the reject-append); the prompt lists each for the agent to respond to.
	draftPrompt := architectDraftPrompt(toPSKind(in.ArtifactKind), proj, *feedback, state.reviewThread, in.Amendment)
	draftObs, derr := wf.dispatchAndObserve(ctx, dispatchDesignJobArgs{
		ProjectID:     in.ProjectID,
		ArtifactKind:  in.ArtifactKind,
		Prompt:        draftPrompt,
		TargetBranch:  sessionBranch,
		PriorStateRef: "",
		// Per-project-design-dispatch: dispatch to the per-project repo + aiarch-design.yml
		// (the rail's repoRef). "" when the rail is dormant ⇒ RA falls back to construction.
		TargetRepo: gf.dispatchRepo(),
	})
	if derr != nil {
		// A TRANSIENT dispatch/observe fault that exhausted its retry budget is an
		// infrastructure escalation (not a ran-but-failed job): close the workflow.
		return coAuthorProceed, coAuthorUnknown, derr
	}
	if draftObs.Phase != pipelineSucceeded {
		// The job RAN and FAILED (drafting failed or the required CI validation check
		// went red) — a terminal-at-the-Manager fault. Do NOT crash the workflow and do
		// NOT loop: land the session in the human-visible StageDraftFailed and suspend
		// on the gate awaiting Retry (redraft/Reject) or Withdraw (§0.5.4 — the
		// anti-wedge rule).
		logger.Warn("Phase-2 design draft job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", draftObs.Diagnostic)
		outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, *headVersion, draftFailedReason(draftObs.Diagnostic), state, feedback)
		if recErr != nil {
			return coAuthorProceed, coAuthorUnknown, recErr
		}
		if !retry {
			return coAuthorReturn, outcome, nil
		}
		// F40: a human Retry at the StageDraftFailed gate redrafts on the SAME persistent
		// session branch (no branch bump — the template's refresh-from-main handles a stale
		// base). The retained feedback rides into the redraft unchanged.
		*redraftCount++
		return coAuthorContinue, coAuthorUnknown, nil
	}
	// READ-BACK on the SESSION BRANCH (§2a): the Action committed the typed Phase-2
	// JSON on the session branch; read it back as the not-yet-merged draft. A dormant
	// rail reads main (readBackBranch() == ""). The read-back happens BEFORE opening the
	// PR (below): it confirms the draft actually LANDED a commit on the branch, so a session
	// that fails before any commit leaves NO PR (correct).
	model, readBackVersion, rbErr := wf.readBackCommittedModelOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch())
	if rbErr != nil {
		if decodeMsg, terminal := isTerminalReadBack(rbErr); terminal {
			// The committed draft DECODES MALFORMED — a terminal fault retry cannot fix (QA
			// F36). Land it at the human StageDraftFailed gate carrying the decode diagnostic
			// (a Retry redrafts on a FRESH branch with the reason visible), instead of the
			// pre-fix behavior of looping the read-back Activity every ~100s forever.
			logger.Warn("Phase-2 read-back decoded MALFORMED committed state; entering StageDraftFailed", "error", decodeMsg)
			outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, *headVersion, readBackDecodeFailedReason(decodeMsg), state, feedback)
			if recErr != nil {
				return coAuthorProceed, coAuthorUnknown, recErr
			}
			if !retry {
				return coAuthorReturn, outcome, nil
			}
			// F40: a human Retry redrafts on the SAME persistent session branch (no branch bump).
			*redraftCount++
			return coAuthorContinue, coAuthorUnknown, nil
		}
		return coAuthorProceed, coAuthorUnknown, rbErr
	}
	draft = model
	state.findings = nil

	// Rail: open the PR (head=sessionBranch, base=main) ONLY NOW — AFTER the read-back
	// CONFIRMED a committed model on the session branch, so the branch has ≥1 commit beyond
	// main and GitHub will not 422 "no commits between base and head" (F40 fix; observed on
	// gtdapp). Idempotent on head — reject/redraft rounds reuse the SAME PR; the server's
	// handle is authoritative for the merge step.
	if err := wf.openPR(ctx, gf, in.ArtifactKind); err != nil {
		return coAuthorProceed, coAuthorUnknown, err
	}

	// QA F29: adopt the ACTUAL read-back substrate version as the head version before
	// staging. The read-back read the session branch (rail) or main (dormant); its Version
	// is the correct optimistic-concurrency token for the stage-on-branch. A fresh workflow
	// reusing a dirty session branch (prior draft/critique commits left it ahead of main)
	// would otherwise stage against the stale main-captured version and Conflict
	// non-recoverably. In the dormant path the read-back version equals the main head, so
	// this is a no-op there.
	*headVersion = readBackVersion

	// Track the staged typed draft for the query.
	state.draft = draft

	// Step 4: stageArtifactForReview, with the workflow-level Conflict loop. The Conflict
	// re-read targets the SESSION BRANCH (gf.readBackBranch()) so it converges on a dirty
	// reused branch (QA F29).
	draftEnvelope, encErr := encodeModel(draft)
	if encErr != nil {
		return coAuthorProceed, coAuthorUnknown, fwmanager.MapError(encErr)
	}
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.StageArtifactForReviewActivity, stageArtifactForReviewArgs{
			ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Model: draftEnvelope, Branch: gf.readBackBranch(),
		}).Get(ctx, &v)
		return v, e
	})
	if err != nil {
		// CRASH CONTAINMENT (QA F29). A stage-for-review activity fault must NOT kill the
		// workflow — it had no recoverable gate (only dispatch/reject faults were contained
		// after F15/F28). Land at the human-visible StageDraftFailed gate keeping the
		// feedback, so a Retry redrafts. A workflow-cancellation still propagates.
		if temporal.IsCanceledError(err) {
			return coAuthorProceed, coAuthorUnknown, err
		}
		outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, *headVersion, stageFailedReason(err), state, feedback)
		if recErr != nil {
			return coAuthorProceed, coAuthorUnknown, recErr
		}
		if !retry {
			return coAuthorReturn, outcome, nil
		}
		// F40: a Retry after a stage fault redrafts on the SAME persistent session branch
		// (no branch bump); the retained feedback rides the redraft unchanged.
		*redraftCount++
		return coAuthorContinue, coAuthorUnknown, nil
	}
	*headVersion = newVersion
	state.stage = StageAwaitingReview
	// REVIEW LEDGER: refresh the durable thread from the branch the draft was staged on so the
	// query surfaces the live comments (with the agent's responses, normalized on stage) and the
	// approve gate can block while any comment is open. Best-effort — a miss keeps the last thread.
	if thread, terr := wf.loadReviewThread(ctx, in, *gf); terr == nil {
		state.reviewThread = thread
	}
	return coAuthorProceed, coAuthorUnknown, nil
}

// coAuthorApplyDecision executes Step 7 — branching on the architect's reviewDecision.
// It is the pre-extraction switch verbatim (the approve arm delegated to coAuthorApprove);
// the workflow-command order is unchanged. It returns coAuthorReturn or coAuthorContinue.
func (wf *workflows) coAuthorApplyDecision(
	ctx workflow.Context,
	in coAuthorInput,
	sig reviewDecisionSignal,
	gf *gitSession,
	headVersion *projectstate.Version,
	redraftCount *int,
	reviewRound *int,
	feedback *string,
	state *coAuthorState,
) (coAuthorStep, coAuthorOutcome, error) {
	switch sig.Decision {
	case ReviewApprove:
		// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open.
		// The manager precondition rejects this synchronously; this is the TOCTOU-safe backstop.
		// Re-suspend at the gate; the reviewer sees the open comments in the queryable thread.
		if open := openReviewCommentIDs(state.reviewThread); len(open) > 0 {
			return coAuthorReAwait, coAuthorUnknown, nil
		}
		return wf.coAuthorApprove(ctx, in, gf, headVersion, redraftCount, feedback, state)

	case ReviewReject:
		notes := signalNotes(sig.Feedback)
		// RETAIN the architect's feedback in workflow state BEFORE the head-state write, so
		// that if the reject write itself faults (below), the crash-containment recovery
		// gate still holds the feedback for a Retry instead of silently discarding the
		// send-back (QA F28).
		*feedback = notes
		newVersion, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			c := mutateOpts(ctx)
			var v projectstate.Version
			e := workflow.ExecuteActivity(c, wf.RejectArtifactActivity, mutateArtifactArgs{
				ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind), Notes: notes,
				// REVIEW LEDGER (review-ledger §2): fold the reviewer's anchored comments into the
				// reject as durable, server-minted ledger entries, round-stamped by the per-reject
				// review-round counter (replay-stable monotonic → deterministic, non-colliding ids on
				// the ONE accumulating thread — F40). Empty ⇒ plain reject.
				Round: int64(*reviewRound), Comments: feedbackToLedgerComments(sig.Feedback),
				// Branch-aware Reject (I-DESIGN-DISPATCH §2a): record the Rejected status on the
				// SESSION BRANCH the draft was staged on — where the staged model exists and the
				// session-branch version matches. In the PR rail main is untouched until an
				// approved draft merges, so a main-path reject would mismatch the version AND find
				// the slot unpopulated (the QA F28 crash). "" when the rail is dormant ⇒ the reject
				// lands on main exactly as before.
				Branch: gf.readBackBranch(),
			}).Get(ctx, &v)
			return v, e
		})
		if err != nil {
			// CRASH CONTAINMENT (QA F28). An activity fault while recording the Reject must
			// NOT kill the workflow (that ends the CoAuthor spine FAILED and loses the feedback
			// that rode the signal). Land at the human-visible StageDraftFailed gate carrying a
			// reason, KEEPING the received feedback (*feedback set above) so a Retry redrafts
			// with the architect's notes woven in. A workflow-cancellation still propagates.
			if temporal.IsCanceledError(err) {
				return coAuthorProceed, coAuthorUnknown, err
			}
			outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, *headVersion, rejectFailedReason(err), state, feedback)
			if recErr != nil {
				return coAuthorProceed, coAuthorUnknown, recErr
			}
			if !retry {
				return coAuthorReturn, outcome, nil
			}
			// F40: a Retry after a faulted reject redrafts on the SAME persistent session branch
			// (no branch bump); the retained feedback rides unchanged.
			*redraftCount++
			return coAuthorContinue, coAuthorUnknown, nil
		}
		*headVersion = newVersion
		// REVIEW LEDGER: reload the thread from the SAME persistent session branch the reject
		// just wrote so it carries the freshly-appended OPEN comments — the redraft prompt lists
		// them for the drafting agent to respond to. Under the F40 single-branch topology the
		// redraft stays on THIS branch, so the durable thread truly accumulates round-over-round
		// (closing the review-ledger cross-reject earmark). Best-effort.
		if thread, terr := wf.loadReviewThread(ctx, in, *gf); terr == nil {
			state.reviewThread = thread
		}
		// F40: the redraft stays on the SAME session branch + PR (no branch bump). Advance only
		// the review-round counter so the NEXT reject's ledger ids do not collide with this round's.
		*reviewRound++
		state.stage = StageRedrafting
		return coAuthorContinue, coAuthorUnknown, nil

	case ReviewWithdraw:
		notes := signalNotes(sig.Feedback)
		// Branch-aware Withdraw (I-DESIGN-DISPATCH §2a; QA F30). The draft under review was
		// staged on the SESSION BRANCH, so the Withdrawn status flip + notes must ride that
		// SAME branch — where the staged model exists and the session-branch version
		// (headVersion) matches. In the PR rail main is untouched until an approved draft
		// merges, so a main-path withdraw would mismatch the version AND find the slot
		// unpopulated (a crash). "" when the rail is dormant ⇒ the withdraw lands on main
		// exactly as before, and the Conflict re-read then targets main.
		if _, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			c := mutateOpts(ctx)
			var v projectstate.Version
			e := workflow.ExecuteActivity(c, wf.WithdrawArtifactActivity, mutateArtifactArgs{
				ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind), Notes: notes, Branch: gf.readBackBranch(),
			}).Get(ctx, &v)
			return v, e
		}); err != nil {
			return coAuthorProceed, coAuthorUnknown, err
		}
		state.stage = StageWithdrawn
		return coAuthorReturn, coAuthorWithdrawn, nil

	case ReviewDecisionUnknown:
		// The zero value: no legitimate signal carries it. Same terminal
		// rejection as the default case below.
		return coAuthorProceed, coAuthorUnknown, temporal.NewNonRetryableApplicationError("unknown review decision", "UnknownReviewDecision", nil)

	default:
		return coAuthorProceed, coAuthorUnknown, temporal.NewNonRetryableApplicationError("unknown review decision", "UnknownReviewDecision", nil)
	}
}

// coAuthorApprove handles the ReviewApprove arm: the merge GUARD (CI must be green) + the
// architecture +1 relay + the App-mediated merge of the session branch → main, then
// commitArtifact on main. On a not-green merge guard it routes to the SAME StageDraftFailed
// recovery gate as a draft failure (the anti-wedge rule). Command order is identical to
// the pre-extraction inline arm.
func (wf *workflows) coAuthorApprove(
	ctx workflow.Context,
	in coAuthorInput,
	gf *gitSession,
	headVersion *projectstate.Version,
	redraftCount *int,
	feedback *string,
	state *coAuthorState,
) (coAuthorStep, coAuthorOutcome, error) {
	logger := workflow.GetLogger(ctx)

	// Rail (approve-time half, §2b): merge GUARD (CI must be green) + the architecture
	// +1 relay + the App-mediated merge of sessionBranch → main. A dormant rail returns
	// merged=true with no rail ops (the non-git spine).
	merged, mErr := wf.mergeOnApprove(ctx, gf, in.ArtifactKind)
	if mErr != nil {
		// QA F35: a merge-window fault (PR-status read / +1 relay / merge) must NOT kill the
		// workflow. The staged draft is intact on the session branch and main is untouched,
		// so contain it — return to AwaitingReview with a queryable notice so the human can
		// simply RE-APPROVE (never a redraft, which would discard an approved-quality draft).
		// Cancellation still propagates.
		if temporal.IsCanceledError(mErr) {
			return coAuthorProceed, coAuthorUnknown, mErr
		}
		logger.Warn("approve merge-window fault; returning to AwaitingReview for re-approve", "error", mErr.Error())
		return wf.reAwaitAfterApproveFault(state, approveFailedReason(mErr)), coAuthorUnknown, nil
	}
	if !merged {
		// The merge guard was NOT green (the required CI check is red on the PR): do
		// NOT merge/commit. Route to the SAME StageDraftFailed recovery gate as a draft
		// failure (the anti-wedge rule) awaiting Retry-via-Reject / Withdraw.
		logger.Warn("Phase-2 design PR not mergeable at approve (CI not green); entering StageDraftFailed")
		outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, *headVersion, draftFailedReason("the design PR is not green — its required CI check has not passed"), state, feedback)
		if recErr != nil {
			return coAuthorProceed, coAuthorUnknown, recErr
		}
		if !retry {
			return coAuthorReturn, outcome, nil
		}
		// F40: Retry-via-Reject from the not-green gate redrafts on the SAME session branch +
		// PR (no branch bump — the template's refresh-from-main handles a stale base).
		*redraftCount++
		return coAuthorContinue, coAuthorUnknown, nil
	}
	// After merge the draft lives on main; commitArtifact lands on main. Re-seed
	// headVersion from main so the commit's CAS starts at main's tip. A dormant rail
	// leaves headVersion as-is (it already tracked main).
	if gf.enabled {
		if mp, rerr := wf.readProject(ctx, in.ProjectID); rerr == nil {
			*headVersion = mp.Version
		} else if !isReadNotFound(rerr) {
			// QA F35: a post-merge re-seed read fault is contained too. The merge already
			// landed on main, so a re-approve re-runs mergeOnApprove idempotently (a merged PR
			// re-merges to a no-op) and re-reads/commits — no redraft, no crash.
			if temporal.IsCanceledError(rerr) {
				return coAuthorProceed, coAuthorUnknown, rerr
			}
			logger.Warn("approve post-merge re-seed fault; returning to AwaitingReview for re-approve", "error", rerr.Error())
			return wf.reAwaitAfterApproveFault(state, approveFailedReason(rerr)), coAuthorUnknown, nil
		}
	}
	// Commit lands on MAIN after the merge (the re-seed above set headVersion to main's
	// tip), so its Conflict re-read targets main (branch=="").
	if _, err := wf.applyRecovering(ctx, in.ProjectID, "", *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.CommitArtifactActivity, mutateArtifactArgs{
			ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind),
		}).Get(ctx, &v)
		return v, e
	}); err != nil {
		// QA F35: contain a post-merge commit fault too (same idempotent re-approve recovery).
		if temporal.IsCanceledError(err) {
			return coAuthorProceed, coAuthorUnknown, err
		}
		logger.Warn("approve post-merge commit fault; returning to AwaitingReview for re-approve", "error", err.Error())
		return wf.reAwaitAfterApproveFault(state, approveFailedReason(err)), coAuthorUnknown, nil
	}
	state.stage = StageCommitted
	return coAuthorReturn, coAuthorApproved, nil
}

// reAwaitAfterApproveFault contains a transient approve/merge-window fault (QA F35): it
// returns the session to AwaitingReview carrying a queryable notice (surfaced as the
// sessionState FailureReason — no schema change; the STAGE disambiguates it from a
// StageDraftFailed reason) and asks the driver to re-await the gate so the human can simply
// re-approve. The staged draft is untouched.
func (wf *workflows) reAwaitAfterApproveFault(state *coAuthorState, reason string) coAuthorStep {
	state.stage = StageAwaitingReview
	state.failureReason = reason
	return coAuthorReAwait
}

// approveFailedReason renders the human "why" for the AwaitingReview re-approve notice when
// an approve/merge-window activity faulted transiently (QA F35 — e.g. a GitHub secondary
// rate-limit 403 the platform classifier reports as Auth). It frames a re-approve, NOT a
// redraft.
func approveFailedReason(err error) string {
	summary := dispatchErrSummary(err)
	if summary == "" {
		return "approving your draft did not complete (a transient repository/API error) — please approve again"
	}
	return "approving your draft did not complete: " + summary + " — please approve again"
}

// ===========================================================================
// (B) AssembleSDPReviewWorkflow — the UC2 spine (contract §6.3 B). Loop until
// Commit/RejectAll-exhausted:
//
//  1. readProject -> require committed PlanningAssumptions + ActivityList + Network
//     + the four Solution slots; missing -> non-retryable "sdp inputs incomplete".
//  2. ASSEMBLE the four ProjectOptions deterministically from the committed slots.
//  3. For EACH option call the three Engines DIRECTLY; JOIN into an SdpOptionRow.
//  4. Build the SdpReview (four rows + recommendation + rationale).
//  5. stageArtifactForReview(SdpReview) -> AwaitingReview.
//  6. awaitSignal("sdpDecision").
//  7. Commit(optionId) -> re-run the three Engines on the chosen option, re-stage
//     with Recommendation=chosen, commitArtifact(KindSdpReview), exit;
//     RejectAll(feedback) -> rejectArtifact(KindSdpReview), loop to step 2 (bounded).
// ===========================================================================

// sdpReviewWorkflowID derives the continuity token: {projectId}:sdpReview.
func sdpReviewWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:sdpReview", projectID)
}

// sdpReviewInput is the start payload for AssembleSDPReviewWorkflow.
type sdpReviewInput struct {
	ProjectID ProjectID
}

// sdpDecisionSignal is the sdpDecision signal payload (contract §6.5).
type sdpDecisionSignal struct {
	Decision SDPDecision
	OptionID *OptionID
	Feedback *ReviewFeedback
}

func (wf *workflows) AssembleSDPReviewWorkflow(ctx workflow.Context, in sdpReviewInput) error {
	state := &coAuthorState{
		projectID:    in.ProjectID,
		artifactKind: KindSdpReview,
		stage:        StageAssemblingSDP,
	}
	if err := workflow.SetQueryHandler(ctx, querySessionState, state.view); err != nil {
		return err
	}

	// feedback woven into the next assembly on a RejectAll loop.
	feedback := ""

	for attempt := 0; ; attempt++ {
		state.stage = StageAssemblingSDP

		// Step 1: read the committed Phase-2 inputs.
		proj, err := wf.readProject(ctx, in.ProjectID)
		if err != nil {
			if isReadNotFound(err) {
				return temporal.NewNonRetryableApplicationError(
					"sdp inputs incomplete (project has no state)", "SDPInputsIncomplete", nil)
			}
			return err
		}

		// Steps 2-4: assemble the four options, run the three Engines per option,
		// join into the SdpReview. Factored into a pure helper so it is unit-testable
		// without Temporal and so it stays deterministic (no clock, no RNG).
		review, asmErr := wf.assembleSdpReview(proj, feedback)
		if asmErr != nil {
			// A missing prerequisite is a non-retryable precondition failure; an Engine
			// error is escalated as a non-retryable terminal (the Manager mis-assembled
			// the option, or an engine invariant broke — neither is retryable).
			return asmErr
		}
		state.draft = review

		// Step 5: stageArtifactForReview(SdpReview) -> AwaitingReview.
		if err := wf.stageReview(ctx, in.ProjectID, review, &state.headVersion); err != nil {
			return err
		}
		state.stage = StageAwaitingReview

		// Step 6: awaitSignal("sdpDecision") — suspend durably.
		var sig sdpDecisionSignal
		workflow.GetSignalChannel(ctx, signalSDPDecision).Receive(ctx, &sig)

		// Step 7: branch on the architect's decision.
		switch sig.Decision {
		case SDPCommit:
			// Commit-time confirmation: bind the architect's chosen option, RE-RUN the
			// three engines on it, re-stage with Recommendation=chosen, then commit.
			if err := wf.sdpCommit(ctx, in, sig, proj, feedback, review, state); err != nil {
				return err
			}
			return nil

		case SDPRejectAll:
			notes := signalNotes(sig.Feedback)
			if err := wf.rejectReview(ctx, in.ProjectID, notes, &state.headVersion); err != nil {
				return err
			}
			if attempt+1 >= maxSDPReassembleAttempts {
				return temporal.NewNonRetryableApplicationError(
					"SDP review rejected past max re-assemble attempts", "SDPReassembleExhausted", nil)
			}
			feedback = notes
			state.stage = StageRedrafting
			continue

		case SDPDecisionUnknown:
			// The zero value: no legitimate signal carries it. Same terminal
			// rejection as the default case below.
			return temporal.NewNonRetryableApplicationError("unknown SDP decision", "UnknownSDPDecision", nil)

		default:
			return temporal.NewNonRetryableApplicationError("unknown SDP decision", "UnknownSDPDecision", nil)
		}
	}
}

// sdpCommit handles the SDPCommit arm of AssembleSDPReviewWorkflow: it binds the
// architect's chosen option, RE-RUNS the three engines on the chosen option (so the
// committed review records Recommendation=chosen), re-stages, and commits KindSdpReview.
// `confirmed` is byte-identical to the staged `review` the architect saw (assembleSdpReview
// is deterministic over the same `proj`, no clock/RNG — §6.8), so the option set cannot skew
// between the gate and the commit; we re-derive only to bind the choice. The workflow-command
// order is identical to the pre-extraction inline arm.
func (wf *workflows) sdpCommit(
	ctx workflow.Context,
	in sdpReviewInput,
	sig sdpDecisionSignal,
	proj projectstate.Project,
	feedback string,
	review *projectstate.SdpReview,
	state *coAuthorState,
) error {
	logger := workflow.GetLogger(ctx)

	if sig.OptionID == nil || !optionInReview(review, projectstate.OptionID(*sig.OptionID)) {
		return temporal.NewNonRetryableApplicationError(
			"SDP commit named an option not in the assembled review", "UnknownOption", nil)
	}
	chosen := projectstate.OptionID(*sig.OptionID)

	confirmed, cErr := wf.assembleSdpReview(proj, feedback)
	if cErr != nil {
		return cErr
	}
	confirmed.Recommendation = chosen
	confirmed.Rationale = fmt.Sprintf("architect committed option %s", chosen)
	state.draft = confirmed

	if err := wf.stageReview(ctx, in.ProjectID, confirmed, &state.headVersion); err != nil {
		return err
	}
	if err := wf.commitReview(ctx, in.ProjectID, &state.headVersion); err != nil {
		return err
	}
	state.stage = StageCommitted
	logger.Info("SDP review committed", "option", string(chosen))
	return nil
}

// stageReview stages the SdpReview into its slot (status AwaitingReview), updating
// headVersion via the workflow-level Conflict loop.
func (wf *workflows) stageReview(ctx workflow.Context, projectID ProjectID, review *projectstate.SdpReview, headVersion *projectstate.Version) error {
	env, encErr := encodeModel(review)
	if encErr != nil {
		return fwmanager.MapError(encErr)
	}
	// The SDP review is assembled and staged directly on MAIN (no agentic draft rail /
	// session branch), so the Conflict re-read targets main (branch=="").
	v, err := wf.applyRecovering(ctx, projectID, "", *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var got projectstate.Version
		e := workflow.ExecuteActivity(c, wf.StageArtifactForReviewActivity, stageArtifactForReviewArgs{
			ProjectID: projectstate.ProjectID(projectID), ExpectedVersion: expected, Model: env,
		}).Get(ctx, &got)
		return got, e
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// commitReview commits the SdpReview slot.
func (wf *workflows) commitReview(ctx workflow.Context, projectID ProjectID, headVersion *projectstate.Version) error {
	v, err := wf.applyRecovering(ctx, projectID, "", *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var got projectstate.Version
		e := workflow.ExecuteActivity(c, wf.CommitArtifactActivity, mutateArtifactArgs{
			ProjectID: projectstate.ProjectID(projectID), ExpectedVersion: expected, Kind: projectstate.KindSdpReview,
		}).Get(ctx, &got)
		return got, e
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// rejectReview records a rejected SdpReview outcome.
func (wf *workflows) rejectReview(ctx workflow.Context, projectID ProjectID, notes string, headVersion *projectstate.Version) error {
	v, err := wf.applyRecovering(ctx, projectID, "", *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var got projectstate.Version
		e := workflow.ExecuteActivity(c, wf.RejectArtifactActivity, mutateArtifactArgs{
			ProjectID: projectstate.ProjectID(projectID), ExpectedVersion: expected, Kind: projectstate.KindSdpReview, Notes: notes,
		}).Get(ctx, &got)
		return got, e
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// ===========================================================================
// (C) Phase2AdvanceWorkflow — seals Phase 2 (contract §6.3 C; mirrors
// systemdesign's runPhaseAdvance). The gate: every Phase2RequiredKinds() slot is
// ReviewCommitted AND an option is bound (the committed SdpReview's Recommendation
// is non-empty). No artifactValidationEngine call — there is no Phase-2 verb on the
// frozen surface; the slot-committed + option-bound gate IS the standard check for
// this construction increment (OQ-1/FU-MPD-1: the per-kind Phase-2 validation verbs
// are additive and deferred).
// ===========================================================================

// phaseAdvanceWorkflowID derives the continuity token: {projectId}:phaseAdvance.
func phaseAdvanceWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:phaseAdvance", projectID)
}

// phaseAdvanceInput is the start payload for Phase2AdvanceWorkflow.
type phaseAdvanceInput struct {
	ProjectID ProjectID
}

func (wf *workflows) Phase2AdvanceWorkflow(ctx workflow.Context, in phaseAdvanceInput) (PhaseAdvanceResult, error) {
	var proj projectstate.Project
	if p, err := wf.readProject(ctx, in.ProjectID); err != nil {
		if !isReadNotFound(err) {
			return PhaseAdvanceResult{}, err
		}
		proj = projectstate.Project{ID: projectstate.ProjectID(in.ProjectID)}
	} else {
		proj = p
	}

	// Gate: every required Phase-2 kind must be Committed, AND an option must be bound.
	var missing []ArtifactKind
	for _, kind := range projectstate.Phase2RequiredKinds() {
		if slotFor(proj, kind).Status != projectstate.ReviewCommitted {
			missing = append(missing, fromPSKind(kind))
		}
	}
	// Option-bound check: the committed SdpReview slot's Model carries a non-empty
	// Recommendation. If the SdpReview slot is itself missing it is already in
	// `missing`; only flag the unbound-option case when the review IS committed.
	if !optionBound(proj) && slotFor(proj, projectstate.KindSdpReview).Status == projectstate.ReviewCommitted {
		missing = append(missing, KindSdpReview)
	}
	if len(missing) > 0 {
		return PhaseAdvanceResult{Advanced: false, MissingArtifacts: missing}, nil
	}

	// Seal Phase 2. AdvancePhase is a MAIN write (Conflict re-read targets main, branch=="").
	if _, err := wf.applyRecovering(ctx, in.ProjectID, "", proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.AdvancePhaseActivity, advancePhaseArgs{
			ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected,
		}).Get(ctx, &v)
		return v, e
	}); err != nil {
		return PhaseAdvanceResult{}, err
	}
	return PhaseAdvanceResult{Advanced: true}, nil
}

// optionBound reports whether the project's committed SdpReview binds an option
// (a non-empty Recommendation).
func optionBound(proj projectstate.Project) bool {
	slot := proj.SdpReview
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		return false
	}
	rev, ok := slot.Model.(*projectstate.SdpReview)
	return ok && rev.Recommendation != ""
}

// ===========================================================================
// Deterministic SDP-review assembly (contract §6.3 B steps 2-4). Pure helper —
// no clock, no RNG, no I/O — unit-testable without Temporal and replay-safe.
// ===========================================================================

// assembleSdpReview builds the four ProjectOptions from the committed Phase-2 head
// state, runs the three Engines per option, joins into the SdpReview, and picks the
// recommendation. Returns a non-retryable terminal on a missing prerequisite or an
// Engine error. feedback is woven into the Rationale on a re-assembly.
func (wf *workflows) assembleSdpReview(proj projectstate.Project, feedback string) (*projectstate.SdpReview, error) {
	pa, alErr := committedPlanningAssumptions(proj)
	if alErr != nil {
		return nil, sdpIncomplete(alErr)
	}
	al, alErr2 := committedActivityList(proj)
	if alErr2 != nil {
		return nil, sdpIncomplete(alErr2)
	}
	nw, nwErr := committedNetwork(proj)
	if nwErr != nil {
		return nil, sdpIncomplete(nwErr)
	}

	rows := make([]projectstate.SdpOptionRow, 0, len(projectstate.SolutionKinds()))
	for _, kind := range projectstate.SolutionKinds() {
		sol, sErr := committedSolution(proj, kind)
		if sErr != nil {
			return nil, sdpIncomplete(sErr)
		}
		opt := assembleOption(kind, pa, al, nw, sol)

		ce, eErr := wf.Estimation.EstimateForOption(fweng.Context{Context: context.Background()}, toEstimationOption(opt))
		if eErr != nil {
			return nil, escalateEngine("estimationEngine", kind, eErr)
		}
		of, oErr := wf.OperationEst.EstimateForOption(
			fweng.Context{Context: context.Background()},
			toOperationOption(opt),
			toOperationUsage(opt.DeclaredUsage),
			operationestimation.InfrastructureKind(opt.InfrastructureKind),
		)
		if oErr != nil {
			return nil, escalateEngine("operationEstimationEngine", kind, oErr)
		}
		proj2, pErr := wf.Settlement.ProjectCommitTimeRevenueShareAndComputeCost(fweng.Context{Context: context.Background()}, toSettlementOption(opt))
		if pErr != nil {
			return nil, escalateEngine("settlementEngine", kind, pErr)
		}

		rows = append(rows, projectstate.SdpOptionRow{
			OptionID:             opt.OptionID,
			SolutionKind:         kind,
			DurationDays:         ce.DurationDays,
			BuildCost:            toProjectStateMoneyFromEstimation(ce.BuildCost),
			CompositeRisk:        ce.Risk.Composite,
			ProjectedMonthlyCost: monthlyCostAtDeclaredLoad(of.UsageCostCurve),
			ExpectedPerCycleNet:  toProjectStateMoney(of.PayoutVsShortfallForecast.ExpectedPerCycleNet),
			RevenueSharePercent:  proj2.RevenueSharePercent,
		})
	}

	rec, rationale := recommendOption(rows)
	if feedback != "" {
		rationale = rationale + " (re-assembled with architect feedback: " + feedback + ")"
	}
	return &projectstate.SdpReview{Options: rows, Recommendation: rec, Rationale: rationale}, nil
}

// toSettlementOption converts the canonical projectstate option to the
// settlementEngine's OWN ProjectOption snapshot at the call boundary (Option B full
// encapsulation: the Engine redefines every domain type it uses as its own generated
// def and imports no projectstate, so the Manager maps field-by-field here). The
// Engine reads only the option's settlement Terms, so only OptionID + Terms cross.
func toSettlementOption(opt projectstate.ProjectOption) billing.ProjectOption {
	t := opt.Terms
	return billing.ProjectOption{
		OptionID: billing.OptionID(opt.OptionID),
		Terms: billing.BillingTerms{
			RevenueShare:         billing.RevenueShareKind(t.RevenueShare),
			RevenueSharePercent:  t.RevenueSharePercent,
			ComputeCost:          billing.ComputeCostKind(t.ComputeCost),
			ComputeMarkupPercent: t.ComputeMarkupPercent,
			Schedule:             billing.ScheduleKind(t.Schedule),
		},
	}
}

// toOperationOption converts the canonical projectstate option to the
// operationEstimationEngine's OWN slim ProjectOption snapshot at the call boundary
// (Option B full encapsulation: the Engine redefines every domain type it uses as its
// own generated def and imports no projectstate, so the Manager maps field-by-field
// here). The Engine reads only the option's settlement Terms, so only OptionID + Terms
// cross.
func toOperationOption(opt projectstate.ProjectOption) operationestimation.ProjectOption {
	t := opt.Terms
	return operationestimation.ProjectOption{
		OptionID: operationestimation.OptionID(opt.OptionID),
		Terms: operationestimation.SettlementTerms{
			RevenueShare:         operationestimation.RevenueShareKind(t.RevenueShare),
			RevenueSharePercent:  t.RevenueSharePercent,
			ComputeCost:          operationestimation.ComputeCostKind(t.ComputeCost),
			ComputeMarkupPercent: t.ComputeMarkupPercent,
			Schedule:             operationestimation.ScheduleKind(t.Schedule),
		},
	}
}

// toOperationUsage converts the canonical declared-usage snapshot to the
// operationEstimationEngine's OWN UsageAssumption at the call boundary. The integer
// fields widen to int64 in the generated contract def.
func toOperationUsage(u projectstate.UsageAssumption) operationestimation.UsageAssumption {
	return operationestimation.UsageAssumption{
		ExpectedDailyActiveUsers: int64(u.ExpectedDailyActiveUsers),
		RequestsPerMinute:        u.RequestsPerMinute,
		AvgPayloadBytes:          int64(u.AvgPayloadBytes),
	}
}

// assembleOption builds one ProjectOption by value from the committed Phase-2 slots
// (contract §6.3 B step 2). DETERMINISTIC — no clock, no RNG; the activity ordering
// is preserved from the ActivityList so the Engine join replays identically.
func assembleOption(
	kind projectstate.ArtifactKind,
	pa projectstate.PlanningAssumptions,
	al projectstate.ActivityList,
	nw projectstate.Network,
	sol projectstate.Solution,
) projectstate.ProjectOption {
	onCritical := make(map[string]bool, len(nw.CriticalPath))
	for _, name := range nw.CriticalPath {
		onCritical[name] = true
	}

	classSet := map[string]struct{}{}
	activities := make([]projectstate.OptionActivity, 0, len(al.Activities))
	for _, a := range al.Activities {
		classSet[a.WorkerClass] = struct{}{}
		activities = append(activities, projectstate.OptionActivity{
			ActivityID:  a.Name,
			EffortDays:  a.EffortDays,
			WorkerClass: a.WorkerClass,
			// OnCriticalPath/RiskBucket are authored METADATA only — the estimationEngine
			// computes its OWN per-option critical path + float-based risk from the network
			// (Phase-2 rework F2/F3); it no longer trusts these fields for the math.
			OnCriticalPath: onCritical[a.Name],
			RiskBucket:     a.RiskBucket,
		})
	}
	classes := make([]string, 0, len(classSet))
	for c := range classSet {
		classes = append(classes, c)
	}

	// Calendar is a SHARED planning assumption for EVERY option (Phase-2 rework F5): the
	// per-option Solution.CalendarDaysPerWeek "cheat" (compressed silently switching 2→5
	// d/wk) is retired. Compression now comes from a higher StaffingCap (parallelism where
	// the network allows) + the 30% exclusion guard in recommendOption.
	//
	// EARMARK (F5e — deferred): the book's SECOND compression lever, "top resources" (a
	// model-tier upgrade, e.g. junior sonnet→opus, that shortens critical-activity effort
	// at higher $/day), is NOT modeled here — cap-based parallelism cannot shorten below
	// the unconstrained critical path. When implemented, the compressed option would carry
	// a per-class effort/throughput multiplier fed to the estimationEngine.
	return projectstate.ProjectOption{
		OptionID:     projectstate.OptionID(kind.String()),
		SolutionKind: kind,
		Network:      projectstate.ActivityNetwork{Activities: activities},
		// AI-derived per-class $/day rates (Phase-2 rework F11) — not the old flat human rates.
		WorkerMix:           projectstate.WorkerMix{ClassRates: deriveClassRates(pa, classes), StaffingCap: sol.StaffingCap},
		CalendarDaysPerWeek: pa.CalendarDaysPerWeek,
		Terms:               pa.Terms,
		DeclaredUsage:       pa.DeclaredUsage,
		InfrastructureKind:  pa.InfrastructureKind,
		Dependencies:        nw.Dependencies,
		Milestones:          nw.Milestones,
		IndirectDailyRate:   indirectDailyRateOf(pa),
		BufferDays:          sol.BufferDays,
		// Top-resource compression lever (F5e): >1 for the compressed option, which speeds
		// up its critical path (shorter + riskier) at a convex cost premium in the engine.
		CriticalSpeedup: sol.CriticalSpeedup,
	}
}

// monthlyCostAtDeclaredLoad picks the UsageCostCurve point nearest LoadMultiplier==1.0
// (the declared-usage point). Deterministic.
func monthlyCostAtDeclaredLoad(curve operationestimation.UsageCostCurve) projectstate.Money {
	best := operationestimation.Money{}
	bestDist := math.MaxFloat64
	for _, p := range curve.Points {
		d := math.Abs(p.LoadMultiplier - 1.0)
		if d < bestDist {
			bestDist = d
			best = p.ProjectedMonthlyCost
		}
	}
	// Convert the Engine's OWN Money back to the canonical projectstate.Money at the
	// boundary (Option B full encapsulation).
	return toProjectStateMoney(best)
}

// toProjectStateMoney converts the operationEstimationEngine's OWN Money back to the
// canonical projectstate.Money at the call boundary (Option B full encapsulation).
func toProjectStateMoney(m operationestimation.Money) projectstate.Money {
	return projectstate.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// toEstimationOption converts the canonical projectstate option to the
// estimationEngine's OWN SLIM ProjectOption snapshot at the call boundary
// (Option B full encapsulation: the Engine redefines every domain type it uses as its
// own generated def and imports no projectstate, so the Manager maps field-by-field
// here). The Engine reads only the construction-side network + worker mix + calendar,
// so only those (plus OptionID for audit) cross — the settlement Terms / declared usage
// / infra / solution kind do NOT. The generated WorkerMix.StaffingCap + OptionActivity.
// RiskBucket widen int → int64.
func toEstimationOption(opt projectstate.ProjectOption) estimation.ProjectOption {
	activities := make([]estimation.OptionActivity, 0, len(opt.Network.Activities))
	for _, a := range opt.Network.Activities {
		activities = append(activities, estimation.OptionActivity{
			ActivityId:     a.ActivityID,
			EffortDays:     a.EffortDays,
			WorkerClass:    a.WorkerClass,
			OnCriticalPath: a.OnCriticalPath,
			RiskBucket:     int64(a.RiskBucket),
		})
	}
	rates := make(map[string]estimation.Money, len(opt.WorkerMix.ClassRates))
	for cls, m := range opt.WorkerMix.ClassRates {
		rates[cls] = estimation.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
	}
	deps := make([]estimation.NetworkDependency, 0, len(opt.Dependencies))
	for _, d := range opt.Dependencies {
		deps = append(deps, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	milestones := make([]estimation.NetworkMilestone, 0, len(opt.Milestones))
	for _, m := range opt.Milestones {
		milestones = append(milestones, estimation.NetworkMilestone{Id: m.ID, DependsOn: m.DependsOn})
	}
	return estimation.ProjectOption{
		OptionId:            estimation.OptionID(opt.OptionID),
		Network:             estimation.ActivityNetwork{Activities: activities, Dependencies: deps, Milestones: milestones},
		WorkerMix:           estimation.WorkerMix{ClassRates: rates, StaffingCap: int64(opt.WorkerMix.StaffingCap)},
		CalendarDaysPerWeek: opt.CalendarDaysPerWeek,
		IndirectDailyRate:   estimation.Money{MinorUnits: opt.IndirectDailyRate.MinorUnits, Currency: opt.IndirectDailyRate.Currency},
		BufferDays:          opt.BufferDays,
		CriticalSpeedup:     opt.CriticalSpeedup,
	}
}

// toProjectStateMoneyFromEstimation converts the estimationEngine's OWN
// Money back to the canonical projectstate.Money at the call boundary (Option B full
// encapsulation).
func toProjectStateMoneyFromEstimation(m estimation.Money) projectstate.Money {
	return projectstate.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// Exclusion-zone bounds (App C §4.7g–i; the-method-risk-modeling Step 5). Options with
// composite risk above tooRisky or below overSafe are OUT; a compressed option more than
// maxCompression shorter than normal is OUT (death-zone proximity / >30% rule, F8).
const (
	riskTooRisky   = 0.75
	riskOverSafe   = 0.30
	maxCompression = 0.30
)

// recommendOption applies the App C exclusion zones across the option rows, then picks the
// best IN-band option (lowest CompositeRisk, tie-break lowest DurationDays) — this is the
// cost-risk sweet spot the book expects to land on the decompressed-normal (F8). If every
// option is out of band it falls back to the lowest-risk row so the recommendation is never
// empty (management still needs a pointer, with the caveat in the rationale).
func recommendOption(rows []projectstate.SdpOptionRow) (projectstate.OptionID, string) {
	if len(rows) == 0 {
		return "", "no options assembled"
	}
	normalDur := 0.0
	for _, r := range rows {
		if r.SolutionKind == projectstate.KindNormalSolution {
			normalDur = r.DurationDays
		}
	}
	included := func(r projectstate.SdpOptionRow) bool {
		if r.CompositeRisk > riskTooRisky || r.CompositeRisk < riskOverSafe {
			return false
		}
		if normalDur > 0 && r.DurationDays < normalDur {
			if (normalDur-r.DurationDays)/normalDur > maxCompression {
				return false
			}
		}
		return true
	}
	pickBest := func(pred func(projectstate.SdpOptionRow) bool) (projectstate.SdpOptionRow, bool) {
		var best projectstate.SdpOptionRow
		found := false
		for _, r := range rows {
			if !pred(r) {
				continue
			}
			if !found || r.CompositeRisk < best.CompositeRisk ||
				(r.CompositeRisk == best.CompositeRisk && r.DurationDays < best.DurationDays) {
				best = r
				found = true
			}
		}
		return best, found
	}
	if best, ok := pickBest(included); ok {
		return best.OptionID, fmt.Sprintf(
			"recommend %s: lowest in-band composite risk (%.3f) at %.1f days (App C risk-crossover exclusions applied)",
			best.OptionID, best.CompositeRisk, best.DurationDays)
	}
	best, _ := pickBest(func(projectstate.SdpOptionRow) bool { return true })
	return best.OptionID, fmt.Sprintf(
		"recommend %s: ALL options fell outside the App C risk band [%.2f,%.2f]; picked lowest composite risk (%.3f) — review before committing",
		best.OptionID, riskOverSafe, riskTooRisky, best.CompositeRisk)
}

// optionInReview reports whether id names one of the assembled option rows.
func optionInReview(review *projectstate.SdpReview, id projectstate.OptionID) bool {
	if review == nil {
		return false
	}
	for _, r := range review.Options {
		if r.OptionID == id {
			return true
		}
	}
	return false
}

// sdpIncomplete wraps a missing-prerequisite error as a non-retryable terminal.
func sdpIncomplete(cause error) error {
	return temporal.NewNonRetryableApplicationError(
		"sdp inputs incomplete: "+cause.Error(), "SDPInputsIncomplete", cause)
}

// escalateEngine wraps an Engine error as a non-retryable terminal (the option was
// mis-assembled or an engine invariant broke — neither is retryable).
func escalateEngine(engineName string, kind projectstate.ArtifactKind, cause error) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("%s failed for option %s: %s", engineName, kind, cause.Error()),
		"SDPEngineError", cause)
}

// ---------------------------------------------------------------------------
// Internal helpers (deterministic; no clock, no RNG).
// ---------------------------------------------------------------------------

// coAuthorState is the live technical state backing the sessionState Query. Reused
// (in a slightly fuller form) by both the per-artifact and the SDP-review workflows.
type coAuthorState struct {
	projectID    ProjectID
	artifactKind ArtifactKind
	stage        SessionStage
	draft        projectstate.ArtifactModel
	findings     []Finding
	headVersion  projectstate.Version
	// failureReason is set only on StageDraftFailed: the neutral job Diagnostic, the
	// human "why" for the SPA's retry/withdraw screen (the anti-wedge requirement).
	failureReason string
	// reviewThread is the durable review ledger for this artifact (review-ledger feature),
	// refreshed from the session branch after every (re)stage and every waive/reopen so the
	// query + approve gate see the live thread. Nil until a read-back carries comments.
	reviewThread []projectstate.ReviewComment
}

func (s *coAuthorState) view() (SessionStateView, error) {
	dm, err := draftModelFor(s.artifactKind, s.draft)
	if err != nil {
		return SessionStateView{}, err
	}
	return SessionStateView{
		ProjectID:     s.projectID,
		ArtifactKind:  s.artifactKind,
		Stage:         s.stage,
		Draft:         dm,
		Findings:      s.findings,
		FailureReason: strPtrOrNil(s.failureReason),
		ReviewThread:  reviewThreadToView(s.reviewThread),
	}, nil
}

func stageForAttempt(attempt int) SessionStage {
	if attempt > 0 {
		return StageRedrafting
	}
	return StageDrafting
}

// awaitDraftFailedRecovery lands a failed/non-converging Phase-2 design job in the
// human-visible StageDraftFailed and suspends at the EXISTING reviewDecision gate (plus
// the requestArtifactDraft redraft lever), awaiting a human decision (§0.5.4 — the
// anti-wedge requirement). The workflow stays OPEN and QUERYABLE as StageDraftFailed
// throughout, carrying the neutral job Diagnostic as the FailureReason, so the SPA
// renders "your design job failed: <diagnostic> — retry or withdraw" and NEVER an
// infinite Drafting spinner. A ran-but-failed job is terminal-at-the-Manager — it is
// escalated to the human gate, not absorbed in an auto-retry budget.
//
// Recovery levers:
//   - signalRedraft (requestArtifactDraft's "Retry draft") → re-dispatch in place.
//   - signalReviewDecision{Reject} → Retry-via-Reject: re-dispatch with the reject
//     feedback woven in (the contract's "human Retry (via reject)" path).
//   - signalReviewDecision{Withdraw} → withdraw + end gracefully (coAuthorWithdrawn).
//
// Returns (outcome, retry, err): retry==true means re-dispatch the draft (the caller
// increments redraftCount and loops); retry==false means end with outcome.
func (wf *workflows) awaitDraftFailedRecovery(
	ctx workflow.Context,
	projectID ProjectID,
	kind ArtifactKind,
	headVersion projectstate.Version,
	reason string,
	state *coAuthorState,
	feedback *string,
) (coAuthorOutcome, bool, error) {
	// Surface the human-visible failed stage + the pre-formatted human reason for the
	// Query. Callers pass the rendered reason (draftFailedReason for a job failure,
	// rejectFailedReason for a review-write fault) so this gate is reason-agnostic.
	state.stage = StageDraftFailed
	state.failureReason = reason

	redraftCh := workflow.GetSignalChannel(ctx, signalRedraft)
	reviewCh := workflow.GetSignalChannel(ctx, signalReviewDecision)

	for {
		var retry bool
		var withdraw bool
		var withdrawNotes string

		sel := workflow.NewSelector(ctx)
		sel.AddReceive(redraftCh, func(c workflow.ReceiveChannel, _ bool) {
			var sig redraftSignal
			c.Receive(ctx, &sig)
			if sig.Feedback != nil {
				*feedback = sig.Feedback.Notes
			}
			retry = true
		})
		sel.AddReceive(reviewCh, func(c workflow.ReceiveChannel, _ bool) {
			var sig reviewDecisionSignal
			c.Receive(ctx, &sig)
			switch sig.Decision {
			case ReviewWithdraw:
				withdraw = true
				withdrawNotes = signalNotes(sig.Feedback)
			case ReviewReject:
				// Retry-via-Reject: re-dispatch with the architect's feedback woven in.
				*feedback = signalNotes(sig.Feedback)
				retry = true
			case ReviewDecisionUnknown, ReviewApprove:
				// Approve at a failed gate is meaningless (no staged draft); the zero
				// value carries no signal either — both ignored, same as default.
			default:
				// Approve at a failed gate is meaningless (no staged draft) — ignored.
			}
		})
		sel.Select(ctx)

		if retry {
			// Clear the failed state before re-entering the draft loop.
			state.stage = StageRedrafting
			state.failureReason = ""
			return coAuthorUnknown, true, nil
		}
		if withdraw {
			// Withdraw at the failed gate is a MAIN write; its Conflict re-read targets main
			// (branch=="").
			if _, err := wf.applyRecovering(ctx, projectID, "", headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.WithdrawArtifactActivity, mutateArtifactArgs{
					ProjectID: projectstate.ProjectID(projectID), ExpectedVersion: expected, Kind: toPSKind(kind), Notes: withdrawNotes,
				}).Get(ctx, &v)
				return v, e
			}); err != nil {
				return coAuthorUnknown, false, err
			}
			return coAuthorWithdrawn, false, nil
		}
		// A non-actionable review decision at the failed gate: stay suspended.
	}
}

// draftFailedReason renders the human "why" for the StageDraftFailed screen from the
// job's neutral Diagnostic. It is infrastructure-neutral (the Diagnostic is already a
// summary, not a log firehose — constructionPipelineAccess.md Non-goal #4).
func draftFailedReason(diagnostic string) string {
	if diagnostic == "" {
		return "the Phase-2 design job failed in CI — retry or withdraw"
	}
	return "the Phase-2 design job failed in CI: " + diagnostic + " — retry or withdraw"
}

// readBackDecodeFailedReason renders the human "why" for the StageDraftFailed screen when
// the committed draft READS BACK MALFORMED (QA F36): CI validate went green (its Go mirror
// types the offending enum as a free string) but the server codec rejects the value on
// read-back. It carries the decode diagnostic so a Retry redrafts with full visibility.
func readBackDecodeFailedReason(decodeMsg string) string {
	if strings.TrimSpace(decodeMsg) == "" {
		return "the committed draft could not be read back — its typed shape is invalid — retry or withdraw"
	}
	return "the committed draft could not be read back — its typed shape is invalid: " + decodeMsg + " — retry or withdraw"
}

// stageFailedReason renders the human "why" for the StageDraftFailed screen when the
// AwaitingReview stage-for-review write FAULTED terminally (QA F29 crash containment). The
// draft is valid (its CI check passed); only the head-state thin-write failed, so a Retry
// re-attempts staging it.
func stageFailedReason(err error) string {
	summary := dispatchErrSummary(err)
	if summary == "" {
		return "staging the draft for your review failed — retry or withdraw"
	}
	return "staging the draft for your review failed: " + summary + " — retry or withdraw"
}

// rejectFailedReason renders the human "why" for the StageDraftFailed screen when the
// architect's Reject was received but the head-state write recording it FAULTED terminally
// (QA F28 crash containment). The architect's feedback is retained in workflow state, so
// the message frames a Retry as re-applying the send-back rather than a lost review.
func rejectFailedReason(err error) string {
	summary := dispatchErrSummary(err)
	if summary == "" {
		return "recording your send-back failed — retry to re-apply your feedback, or withdraw"
	}
	return "recording your send-back failed: " + summary + " — retry to re-apply your feedback, or withdraw"
}

// dispatchErrSummary extracts a neutral, bounded summary from a terminal error. A Temporal
// ApplicationError (the wrapped RA fault, e.g. ContractMisuse) carries a human Message();
// otherwise the error string is used. Deterministic across replay — the error is
// reconstructed identically from workflow history.
func dispatchErrSummary(err error) string {
	if err == nil {
		return ""
	}
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		if msg := appErr.Message(); msg != "" {
			return msg
		}
	}
	return err.Error()
}

func signalNotes(f *ReviewFeedback) string {
	if f != nil {
		return f.Notes
	}
	return ""
}

// slotFor returns the named Project slot for a kind (Phase 1 + Phase 2). Internal
// (operates on the canonical projectstate.ArtifactKind); own-kind callers convert via
// toPSKind at the boundary.
func slotFor(proj projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	switch kind {
	case projectstate.KindMission:
		return proj.Mission
	case projectstate.KindGlossary:
		return proj.Glossary
	case projectstate.KindScrubbedRequirements:
		return proj.ScrubbedRequirements
	case projectstate.KindVolatilities:
		return proj.Volatilities
	case projectstate.KindCoreUseCases:
		return proj.CoreUseCases
	case projectstate.KindSystem:
		return proj.SystemDesign
	case projectstate.KindOperationalConcepts:
		return proj.OperationalConcepts
	case projectstate.KindStandardCheck:
		return proj.StandardCheck
	case projectstate.KindPlanningAssumptions:
		return proj.PlanningAssumptions
	case projectstate.KindActivityList:
		return proj.ActivityList
	case projectstate.KindNetwork:
		return proj.Network
	case projectstate.KindNormalSolution:
		return proj.NormalSolution
	case projectstate.KindSubcriticalSolution:
		return proj.SubcriticalSolution
	case projectstate.KindCompressedSolution:
		return proj.CompressedSolution
	case projectstate.KindDecompressedSolution:
		return proj.DecompressedSolution
	case projectstate.KindRiskModel:
		return proj.RiskModel
	case projectstate.KindSdpReview:
		return proj.SdpReview
	default:
		return projectstate.ArtifactSlot{}
	}
}

// committedModel returns the committed typed model in the slot named by kind, or a
// FailedPrecondition error if the slot is not committed / not populated.
func committedModel(proj projectstate.Project, kind projectstate.ArtifactKind) (projectstate.ArtifactModel, error) {
	slot := slotFor(proj, kind)
	if slot.Status != projectstate.ReviewCommitted || slot.Model == nil {
		return nil, fwmanager.New(fwmanager.FailedPrecondition,
			fmt.Sprintf("SDP prerequisite %s is not committed", kind))
	}
	return slot.Model, nil
}

func committedPlanningAssumptions(proj projectstate.Project) (projectstate.PlanningAssumptions, error) {
	m, err := committedModel(proj, projectstate.KindPlanningAssumptions)
	if err != nil {
		return projectstate.PlanningAssumptions{}, err
	}
	pa, ok := m.(*projectstate.PlanningAssumptions)
	if !ok {
		return projectstate.PlanningAssumptions{}, wrongModelType(projectstate.KindPlanningAssumptions, m)
	}
	return *pa, nil
}

func committedActivityList(proj projectstate.Project) (projectstate.ActivityList, error) {
	m, err := committedModel(proj, projectstate.KindActivityList)
	if err != nil {
		return projectstate.ActivityList{}, err
	}
	al, ok := m.(*projectstate.ActivityList)
	if !ok {
		return projectstate.ActivityList{}, wrongModelType(projectstate.KindActivityList, m)
	}
	return *al, nil
}

func committedNetwork(proj projectstate.Project) (projectstate.Network, error) {
	m, err := committedModel(proj, projectstate.KindNetwork)
	if err != nil {
		return projectstate.Network{}, err
	}
	nw, ok := m.(*projectstate.Network)
	if !ok {
		return projectstate.Network{}, wrongModelType(projectstate.KindNetwork, m)
	}
	return *nw, nil
}

func committedSolution(proj projectstate.Project, kind projectstate.ArtifactKind) (projectstate.Solution, error) {
	m, err := committedModel(proj, kind)
	if err != nil {
		return projectstate.Solution{}, err
	}
	sol, ok := m.(*projectstate.Solution)
	if !ok {
		return projectstate.Solution{}, wrongModelType(kind, m)
	}
	return *sol, nil
}

func wrongModelType(want projectstate.ArtifactKind, got projectstate.ArtifactModel) error {
	gotKind := "nil"
	if got != nil {
		gotKind = got.Kind().String()
	}
	return fwmanager.New(fwmanager.ContractMisuse,
		fmt.Sprintf("expected a %s model, got %s", want, gotKind))
}
