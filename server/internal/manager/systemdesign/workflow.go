package systemdesign

import (
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (systemDesignManager.md §6.1/§6.2/§6.5).
// ---------------------------------------------------------------------------

// TaskQueue is the one queue per Manager that the in-process Temporal Worker in
// the server polls (systemDesignManager.md §6.1).
const TaskQueue = "system-design"

// Signal and query names (systemDesignManager.md §6.5).
const (
	// SignalReviewDecision resumes a suspended CoAuthorArtifactWorkflow at the
	// AwaitingReview gate; backs submitReviewDecision.
	SignalReviewDecision = "reviewDecision"
	// SignalRedraft resumes a CoAuthorArtifactWorkflow that ended a draft attempt in
	// the StageRefused terminal-but-live state (a terminal worker fault: the LLM
	// worker is unavailable / out of credits, or produced an unconstructable
	// response). It re-enters the draft loop in the SAME live workflow so the user's
	// "Retry draft" recovers without a fresh run. Backs requestArtifactDraft's retry
	// path (signal-with-start; systemDesignManager.md §2.1).
	SignalRedraft = "redraft"
	// QuerySessionState returns a SessionStateView; backs getSessionState.
	QuerySessionState = "sessionState"
)

// ExecutionKinds for the durable-execution control plane (systemDesignManager.md §6.2).
const (
	// ExecutionKindPhase is the PARENT SystemDesignPhaseWorkflow (2026-05-29), the
	// ordered 7-step Phase-1 sequence started by startSystemDesign.
	ExecutionKindPhase = "systemDesignPhase"
	// ExecutionKindCoAuthor is the per-step child CoAuthorArtifactWorkflow gate.
	ExecutionKindCoAuthor = "systemDesignCoAuthor"
	// ExecutionKindPhaseAdvance is the short-lived phase-seal gating workflow.
	ExecutionKindPhaseAdvance = "systemDesignPhaseAdvance"
)

// Workflows is the single systemDesignManager component struct. It holds ALL the
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
type Workflows struct {
	ProjectState projectstate.ProjectStateAccess
	Pipeline     ConstructionPipelineAccess

	// Rail + Repo are the OPTIONAL git-forward PR rail (I-DESIGN-DISPATCH §2b). When
	// both are non-nil AND a repo resolves for the project, the CoAuthor spine wraps
	// each draft in the settled branch→PR→read-back→+1→merge model: ensure the session
	// branch, open a PR (head=sessionBranch, base=main), read back + stage on the
	// session branch, then on Approve guard-check + relay the +1 + merge to main before
	// committing on main. When either is nil (the Postgres/non-git composition, or every
	// existing test) the spine runs UNCHANGED — read-back/stage on main, no branch/PR
	// ops — so the branch-aware path is purely additive and dormant-when-unwired,
	// exactly like the construction Manager's git-forward slice.
	Rail SourceControlRail
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

	// PR-rail Activity names (I-DESIGN-DISPATCH §2b).
	actMintRepoCredential   = "MintRepoCredentialActivity"
	actOpenBranch           = "OpenBranchActivity"
	actOpenPullRequest      = "OpenPullRequestActivity"
	actGetPullRequestStatus = "GetPullRequestStatusActivity"
	actPostReview           = "PostReviewActivity"
	actMergePullRequest     = "MergePullRequestActivity"
)

// maxRedraftAttempts bounds the PM-revise / draft-failure redraft loop before the
// workflow stages best-effort for the human gate (core-use-cases.md §1a alt-path).
// A pure in-workflow guard; not a contract surface.
const maxRedraftAttempts = 5

// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
// loop (D-PA §6/§7). A stale expectedVersion surfaces as fwra.Conflict
// (non-retryable per the fixed framework enum). The idempotency key is stable per
// Activity invocation, so a re-apply that races a prior committed attempt
// collapses to an idempotent no-op success. The bound guards a write-contention
// pathology. A pure in-workflow guard.
const maxMutateConflictAttempts = 20

// Activity option presets (systemDesignManager.md §6.4). Concrete RetryPolicy /
// timeout choices live here, in the Manager.
func readProjectOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

func mutateOpts(ctx workflow.Context) workflow.Context {
	// Retry Transient via Activity RetryPolicy; Conflict is handled by the
	// workflow-level re-read→re-apply loop (D-PA §6/§7). Terminal on ContractMisuse.
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// raConflictErrType is the canonical Temporal Type() a head-state mutation
// Activity surfaces when the optimistic-concurrency token (expectedVersion) is
// stale. The workflow recovers with the bounded re-read→re-apply loop.
var raConflictErrType = fwmanager.RAErrType(fwra.Conflict)

// raNotFoundErrType is the canonical Temporal Type() the ReadProject Activity
// surfaces when the addressed aggregate has NO row yet — a brand-new project.
var raNotFoundErrType = fwmanager.RAErrType(fwra.NotFound)

// readProject runs the ReadProject Activity and returns the whole head-state
// aggregate. A brand-new project surfaces fwra.NotFound (see isReadNotFound).
func (wf *Workflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
	c := readProjectOpts(ctx)
	var pe projectEnvelope
	if err := workflow.ExecuteActivity(c, wf.ReadProjectActivity, projectID).Get(ctx, &pe); err != nil {
		return projectstate.Project{}, err
	}
	return pe.decode()
}

// readVersion runs the cheap ReadProjectVersion Activity and returns only the
// head-state optimistic-concurrency token — the single value the Conflict re-read
// loop needs to seed its next attempt. A brand-new project surfaces fwra.NotFound
// (see isReadNotFound), identical to readProject's absence semantics. Replaces the
// wasteful whole-aggregate read that shipped the entire encoded Project across the
// Temporal Activity boundary for a uint64.
func (wf *Workflows) readVersion(ctx workflow.Context, projectID ProjectID) (projectstate.Version, error) {
	c := readProjectOpts(ctx)
	var v projectstate.Version
	if err := workflow.ExecuteActivity(c, wf.ReadProjectVersionActivity, projectID).Get(ctx, &v); err != nil {
		return 0, err
	}
	return v, nil
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (D-PA §6/§7).
func (wf *Workflows) applyRecovering(
	ctx workflow.Context,
	projectID ProjectID,
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
		v, rerr := wf.readVersion(ctx, projectID)
		if rerr != nil {
			if isReadNotFound(rerr) {
				expected = 0
				continue
			}
			return 0, rerr
		}
		expected = v
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
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

// critiqueReadBackEmptyType is the Temporal Type() readBackCritique raises when a
// critique job reached PhaseSucceeded but committed no verdict (the missing-verdict
// safe default — dispatch.go). The caller routes it to the StageDraftFailed gate.
const critiqueReadBackEmptyType = "CritiqueReadBackEmpty"

// critiqueMissingVerdictDiagnostic is the neutral human-facing reason surfaced as the
// StageDraftFailed FailureReason when a critique job committed no verdict.
const critiqueMissingVerdictDiagnostic = "the PM-critique job committed no verdict"

// isCritiqueReadBackEmpty reports whether err is the missing-verdict read-back fault.
func isCritiqueReadBackEmpty(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == critiqueReadBackEmptyType
	}
	return false
}

// ===========================================================================
// SystemDesignPhaseWorkflow — the PARENT (2026-05-29; systemDesignManager.md
// §0b / §6, rework §2.2). Drives the seven Phase-1 steps in fixed Method order,
// spawning the per-step child gate via executeChild, auto-advancing on each
// human Approve, and sealing Phase 1 after step 7.
//
//   mission → glossary → scrubbed-requirements → volatilities → core-use-cases
//   → system(architecture) → operational-concepts → standard-check → SEAL
//
// (Phase1RequiredKinds() is the fixed ordered sequence — the single source of
// truth shared with the seal gate.)
// ===========================================================================

// systemDesignPhaseWorkflowID derives the parent continuity token:
// {projectId}:systemDesign (systemDesignManager.md §2.0).
func systemDesignPhaseWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:systemDesign", projectID)
}

// PhaseInput is the start payload for SystemDesignPhaseWorkflow.
type PhaseInput struct {
	ProjectID ProjectID
}

func (wf *Workflows) SystemDesignPhaseWorkflow(ctx workflow.Context, in PhaseInput) error {
	logger := workflow.GetLogger(ctx)

	// Drive the seven steps in fixed Method order. For each step, spawn the child
	// gate and auto-advance only on the child's Approve outcome; a Withdraw holds
	// the phase at that step (the operator re-enters via requestArtifactDraft).
	for _, kind := range phase1RequiredKinds() {
		childID := coAuthorWorkflowID(in.ProjectID, kind)
		cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: childID,
		})
		var outcome CoAuthorOutcome
		if err := workflow.ExecuteChildWorkflow(cctx, ExecutionKindCoAuthor, CoAuthorInput{
			ProjectID:    in.ProjectID,
			ArtifactKind: kind,
		}).Get(ctx, &outcome); err != nil {
			return err
		}
		if outcome != CoAuthorApproved {
			// The human withdrew this step; the phase does not advance. The parent
			// stops here — re-entry is via a fresh requestArtifactDraft on the step.
			logger.Info("co-author step not approved; halting phase sequence", "kind", ArtifactKindString(kind), "outcome", int(outcome))
			return nil
		}
		logger.Info("co-author step approved; advancing phase sequence", "kind", ArtifactKindString(kind))
	}

	// All seven steps approved → seal Phase 1 (advancePhase). The parent runs the
	// same gate as the standalone PhaseAdvanceWorkflow inline.
	res, err := wf.runPhaseAdvance(ctx, in.ProjectID)
	if err != nil {
		return err
	}
	if !res.Advanced {
		logger.Warn("phase seal blocked despite all steps approved", "missing", res.MissingArtifacts)
	}
	return nil
}

// ===========================================================================
// CoAuthorArtifactWorkflow — the per-step CHILD GATE (the UC1a spine;
// systemDesignManager.md §0b §3 / §6). Loop until Approve / Withdraw:
//
//  1. readProject              -> head-state (prior committed typed slots + ResearchInput + Version)
//  2. DRAFT (architect role)   -> typed <Kind> via GenerateTypedDataActivity(Target=Draft);
//                                 the Manager assembles the architect-role prompt (prompts.go)
//  3. MACHINE VALIDATE         -> kind-appropriate artifactValidationEngine verb, DIRECT in-workflow;
//                                 VerdictFail w/ retries -> loop to 2 with findings woven in
//  4. PM-CRITIQUE              -> only mission/glossary+scrubbed/core-use-cases:
//                                 GenerateTypedDataActivity(Target=Critique) -> Critique;
//                                 Revise -> loop to 2 with Notes, BEFORE the human gate
//  5. stageArtifactForReview   -> carry the TYPED model into its slot (status AwaitingReview)
//  6. awaitSignal(reviewDecision) -> suspend durably
//  7. Approve  -> commitArtifact(kind), return CoAuthorApproved
//     Reject   -> rejectArtifact(kind, notes), loop to 2 (and re-run PM-critique)
//     Withdraw -> withdrawArtifact(kind, notes), return CoAuthorWithdrawn
//
// TERMINAL DRAFT FAILURE (prod incident 2026-06-01 / Bug B). When the draft step's
// generic-worker dispatch returns an UNRECOVERABLE error — the worker refused
// (produced an unconstructable response) OR a TERMINAL fwra kind (Auth /
// QuotaExhausted / ContractMisuse / ContentPolicy, e.g. the Anthropic account is
// out of credits) — the workflow does NOT `return ..., err`. Returning an error
// would close this child (and, via Get, the parent SystemDesignPhaseWorkflow)
// FAILED while the sessionState Query still reports StageDrafting, leaving the SPA
// on an infinite "generating" screen with no recovery.
//
// Instead the workflow records the terminal fault on the live query state
// (state.stage = StageRefused + a short human FailureReason) and SUSPENDS at a
// recovery gate, awaiting EITHER:
//   - SignalRedraft     -> re-enter the draft loop in this SAME live workflow
//                          (the user's "Retry draft" via requestArtifactDraft), or
//   - SignalReviewDecision{Withdraw} -> withdraw + end gracefully (CoAuthorWithdrawn).
//
// The workflow stays OPEN and QUERYABLE throughout (getSessionState returns
// `refused` + the reason), and the parent does NOT crash — it only advances on a
// child Approve and otherwise halts gracefully, exactly as it does for Withdraw.
// We chose suspend-await-redraft over graceful-complete-and-restart because the
// account-credit fault is transient-to-the-business (top up and retry): keeping the
// session live lets Retry resume in place with no new workflow run, and the query
// stays continuously available for the SPA poll.
// ===========================================================================

// coAuthorWorkflowID derives the continuity token for a per-artifact co-authoring
// workflow: {projectId}:{artifactKind} (systemDesignManager.md §6.1).
func coAuthorWorkflowID(projectID ProjectID, kind ArtifactKind) string {
	return fmt.Sprintf("%s:%d", projectID, int(kind))
}

// CoAuthorInput is the start payload for CoAuthorArtifactWorkflow.
type CoAuthorInput struct {
	ProjectID    ProjectID
	ArtifactKind ArtifactKind
	// Feedback is the optional re-request feedback for the explicit
	// withdraw-then-redraft-with-notes path (systemDesignManager.md §2.1, OQ6).
	Feedback *ReviewFeedback
}

// CoAuthorOutcome is the child gate's terminal report to the parent — whether the
// step's human gate approved (advance) or withdrew (halt).
type CoAuthorOutcome int

const (
	CoAuthorUnknown CoAuthorOutcome = iota
	CoAuthorApproved
	CoAuthorWithdrawn
)

// ReviewDecisionSignal is the reviewDecision signal payload (systemDesignManager.md §6.5).
type ReviewDecisionSignal struct {
	Decision ReviewDecision
	Feedback *ReviewFeedback
}

// RedraftSignal is the redraft signal payload — the "Retry draft" lever delivered
// to a CoAuthorArtifactWorkflow suspended in the StageRefused recovery gate
// (requestArtifactDraft's retry path). Feedback is the optional re-request feedback
// woven into the next draft dispatch.
type RedraftSignal struct {
	Feedback *ReviewFeedback
}

func (wf *Workflows) CoAuthorArtifactWorkflow(ctx workflow.Context, in CoAuthorInput) (CoAuthorOutcome, error) {
	logger := workflow.GetLogger(ctx)

	// Live technical state backing the sessionState Query (§6.5/§6.6).
	state := &coAuthorState{
		projectID:    in.ProjectID,
		artifactKind: in.ArtifactKind,
		stage:        StageDrafting,
	}
	if err := workflow.SetQueryHandler(ctx, QuerySessionState, state.view); err != nil {
		return CoAuthorUnknown, err
	}

	// Carry expectedVersion forward in workflow state (read-your-writes; D-PA §6).
	var headVersion projectstate.Version

	// Step 1: read the project head-state once (prior typed models + ResearchInput + version).
	var proj projectstate.Project
	if p, err := wf.readProject(ctx, in.ProjectID); err != nil {
		if !isReadNotFound(err) {
			return CoAuthorUnknown, err
		}
		proj = projectstate.Project{ID: projectstate.ProjectID(in.ProjectID)}
	} else {
		proj = p
		headVersion = p.Version
	}

	// feedback carried into the next draft dispatch: seeded from the explicit
	// re-request feedback (OQ6), then replaced by PM-revise / reject-loop / validation
	// feedback. Carries Notes + the architect's JSONPath-anchored Comments (the
	// latter only ever set on the reject loop).
	feedback := ReviewFeedback{}
	if in.Feedback != nil {
		feedback = *in.Feedback
	}

	// redraftCount bounds the PM-critique-revise / draft-failure retry loop before the
	// workflow stages best-effort for the human gate. It persists across the outer
	// Reject loop (a fresh human Reject is a new round but does not reset the
	// non-convergence guard within a session). A pure in-workflow guard.
	redraftCount := 0

	// branchAttempt is the per-REJECT session-branch attempt counter (I-DESIGN-DISPATCH
	// §2b "sessionBranch"). A within-attempt redraft (PM-revise / dispatch auto-retry)
	// reuses the same session branch (its PR tracks head); a fresh AwaitingReview-gate
	// REJECT bumps it so the next attempt drafts on a NEW branch + opens a NEW PR (a
	// merged/closed PR cannot be reused). Threaded into designBranch; 0 ⇒ the original
	// deterministic branch name. Inert when the rail is dormant.
	branchAttempt := 0

	for {
		// --- DRAFT round-trip: dispatch -> observe -> read-back (agentic pivot) ---
		// The Manager composes the Method-role prompt IN-MEMORY (never persisted),
		// dispatches a claude-code-action DESIGN job, observes it to a typed terminal
		// phase, and reads back the typed model the Action committed. On a terminal
		// FAILURE phase the session lands in StageDraftFailed and suspends at the human
		// gate (the anti-wedge rule) — never a perpetual Drafting.
		var draft projectstate.ArtifactModel
		state.stage = stageForAttempt(redraftCount)

		// The per-attempt SESSION BRANCH the Action drafts + commits + opens its PR on
		// (I-DESIGN-DISPATCH §2b). Deterministic from project+kind+attempt; bumped only on
		// a fresh REJECT. Inert (just a string) when the rail is dormant.
		sessionBranch := designBranch(in.ProjectID, in.ArtifactKind, DispatchTargetDraft, branchAttempt)

		// Rail (dispatch-time half): mint the credential + ensure the session branch
		// exists BEFORE the Action drafts on it. A dormant rail returns a disabled session
		// and the spine runs unchanged (read-back/stage on main, no branch/PR ops).
		gf, gerr := wf.beginSession(ctx, in.ProjectID, sessionBranch)
		if gerr != nil {
			return CoAuthorUnknown, gerr
		}

		draftPrompt := architectDraftPrompt(toPSKind(in.ArtifactKind), proj, feedback)
		draftObs, derr := wf.dispatchAndObserve(ctx, DispatchDesignJobArgs{
			ProjectID:     in.ProjectID,
			ArtifactKind:  in.ArtifactKind,
			Target:        DispatchTargetDraft,
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
			return CoAuthorUnknown, derr
		}
		if draftObs.Phase != PipelineSucceeded {
			// The job RAN and FAILED (drafting failed or CI validation went red) — a
			// terminal-at-the-Manager fault. Do NOT crash the workflow and do NOT loop:
			// land the session in the human-visible StageDraftFailed and suspend on the
			// gate awaiting Retry (redraft) or Withdraw (§0d.4 — the anti-wedge rule).
			logger.Warn("design draft job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", draftObs.Diagnostic)
			outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, draftObs.Diagnostic, state, &feedback)
			if recErr != nil {
				return CoAuthorUnknown, recErr
			}
			if !retry {
				return outcome, nil
			}
			redraftCount++
			continue
		}
		// Rail: open the PR (head=sessionBranch, base=main) now the draft is green.
		// Idempotent on head — if the Action already opened it the rail returns the
		// existing handle (the server's handle is authoritative for the merge step).
		if err := wf.openPR(ctx, &gf, in.ArtifactKind); err != nil {
			return CoAuthorUnknown, err
		}
		// READ-BACK on the SESSION BRANCH (§2a): the Action committed the typed JSON on
		// the session branch; read it back as the not-yet-merged draft. A dormant rail
		// reads main (readBackBranch() == "").
		model, rbErr := wf.readBackCommittedModelOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch())
		if rbErr != nil {
			return CoAuthorUnknown, rbErr
		}
		draft = model
		state.findings = nil

		// PM-CRITIQUE round-trip — only for the kinds the Method assigns a PM reviewer
		// (mission / glossary+scrubbed / core-use-cases). A SECOND dispatch → observe →
		// read-back producing a typed Critique. On CritiqueRevise the loop re-dispatches
		// the architect-role draft with the critique Notes woven in, BEFORE the human
		// gate. Architect-owned steps skip this entirely.
		if kindHasPMCritique(toPSKind(in.ArtifactKind)) {
			// The critique session branch (per-attempt). The PM-critique Action commits its
			// verdict carrier here; no PR/merge happens for critique (only the draft path
			// gets the rail). Inert when the rail is dormant.
			critiqueBranch := designBranch(in.ProjectID, in.ArtifactKind, DispatchTargetCritique, branchAttempt)
			critPrompt := pmCritiquePrompt(toPSKind(in.ArtifactKind), draft)
			critObs, cerr := wf.dispatchAndObserve(ctx, DispatchDesignJobArgs{
				ProjectID:     in.ProjectID,
				ArtifactKind:  in.ArtifactKind,
				Target:        DispatchTargetCritique,
				Prompt:        critPrompt,
				TargetBranch:  critiqueBranch,
				PriorStateRef: "",
				// Per-project-design-dispatch: the critique job also runs in the per-project repo.
				TargetRepo: gf.dispatchRepo(),
			})
			if cerr != nil {
				return CoAuthorUnknown, cerr
			}
			if critObs.Phase != PipelineSucceeded {
				// A terminal PM-critique job failure routes to the same StageDraftFailed
				// human gate as a terminal draft failure — never crash the workflow.
				logger.Warn("PM-critique job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", critObs.Diagnostic)
				outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, critObs.Diagnostic, state, &feedback)
				if recErr != nil {
					return CoAuthorUnknown, recErr
				}
				if !retry {
					return outcome, nil
				}
				redraftCount++
				continue
			}
			critiqueReadBranch := ""
			if gf.enabled {
				critiqueReadBranch = critiqueBranch
			}
			critique, crbErr := wf.readBackCritiqueOn(ctx, in.ProjectID, in.ArtifactKind, critiqueReadBranch)
			if crbErr != nil {
				if isCritiqueReadBackEmpty(crbErr) {
					// A critique job that reported success but committed NO verdict is a
					// ran-but-incomplete job — the missing-verdict safe default (dispatch.go).
					// Route it to the SAME human-visible StageDraftFailed gate as a terminal
					// job failure (NOT a silent approve, NOT a workflow crash — the anti-wedge
					// rule), awaiting human Retry-via-Reject / Withdraw.
					logger.Warn("PM-critique read-back found no verdict (missing-verdict safe default); entering StageDraftFailed")
					outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, critiqueMissingVerdictDiagnostic, state, &feedback)
					if recErr != nil {
						return CoAuthorUnknown, recErr
					}
					if !retry {
						return outcome, nil
					}
					redraftCount++
					continue
				}
				return CoAuthorUnknown, crbErr
			}
			if critique.Verdict == CritiqueRevise {
				redraftCount++
				if redraftCount >= maxRedraftAttempts {
					// Do NOT crash the workflow (that wedges the SPA). The committed draft
					// is valid (it passed the CI check); stage it for the human gate with the
					// unresolved PM critique surfaced as a note so the architect makes the
					// final call instead of an oscillating critic killing the loop.
					logger.Warn("PM-critique did not converge within max attempts; staging for human review")
					state.unresolvedCritique = critique.Notes
					// fall through to stage for review.
				} else {
					// Re-dispatch the architect draft with the PM notes woven in.
					feedback = ReviewFeedback{Notes: critique.Notes}
					state.stage = StageRedrafting
					continue
				}
			}
		}

		// Track the staged typed draft for the query (render is off the spine).
		state.draft = draft

		// Step 5: stageArtifactForReview, with the workflow-level Conflict loop.
		draftEnvelope, encErr := encodeModel(draft)
		if encErr != nil {
			return CoAuthorUnknown, fwmanager.MapError(encErr)
		}
		{
			newVersion, err := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.StageArtifactForReviewActivity, StageArtifactForReviewArgs{
					ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Model: draftEnvelope, Branch: gf.readBackBranch(),
				}).Get(ctx, &v)
				return v, e
			})
			if err != nil {
				return CoAuthorUnknown, err
			}
			headVersion = newVersion
		}
		state.stage = StageAwaitingReview

		// Step 6: awaitSignal("reviewDecision") — in-workflow primitive; suspend.
		var sig ReviewDecisionSignal
		workflow.GetSignalChannel(ctx, SignalReviewDecision).Receive(ctx, &sig)

		// Step 7: branch on the architect's decision (the commit authority).
		switch sig.Decision {
		case ReviewApprove:
			// Rail (approve-time half, §2b): merge GUARD (CI must be green) + the
			// architecture +1 relay + the App-mediated merge of sessionBranch → main. A
			// dormant rail returns merged=true with no rail ops (the non-git spine).
			merged, mErr := wf.mergeOnApprove(ctx, &gf, in.ArtifactKind)
			if mErr != nil {
				return CoAuthorUnknown, mErr
			}
			if !merged {
				// The merge guard was NOT green (the required CI check is red on the PR): do
				// NOT merge, do NOT commit. Route to the SAME StageDraftFailed recovery gate
				// as a draft failure (the anti-wedge rule) awaiting Retry-via-Reject / Withdraw.
				logger.Warn("design PR not mergeable at approve (CI not green); entering StageDraftFailed")
				outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, "the design PR is not green — its required CI check has not passed", state, &feedback)
				if recErr != nil {
					return CoAuthorUnknown, recErr
				}
				if !retry {
					return outcome, nil
				}
				// Retry-via-Reject from the not-green gate is a fresh attempt: new session
				// branch + PR (a closed/abandoned PR cannot be reused).
				branchAttempt++
				redraftCount++
				continue
			}
			// After merge the draft lives on main; commitArtifact + advancePhase land on
			// main (the canonical head). Re-seed headVersion from main so the commit's CAS
			// starts at main's tip (the session-branch version no longer applies). A dormant
			// rail leaves headVersion as-is (it already tracked main).
			if gf.enabled {
				if mp, rerr := wf.readProject(ctx, in.ProjectID); rerr == nil {
					headVersion = mp.Version
				} else if !isReadNotFound(rerr) {
					return CoAuthorUnknown, rerr
				}
			}
			newVersion, err := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.CommitArtifactActivity, MutateArtifactArgs{
					ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind),
				}).Get(ctx, &v)
				return v, e
			})
			if err != nil {
				return CoAuthorUnknown, err
			}
			headVersion = newVersion
			state.stage = StageCommitted
			return CoAuthorApproved, nil

		case ReviewReject:
			rejectFeedback := reviewFeedbackOrZero(sig.Feedback)
			newVersion, err := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.RejectArtifactActivity, MutateArtifactArgs{
					ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind), Notes: rejectFeedback.Notes,
				}).Get(ctx, &v)
				return v, e
			})
			if err != nil {
				return CoAuthorUnknown, err
			}
			headVersion = newVersion
			// A fresh REJECT needs a NEW session branch + PR next attempt (the rejected
			// PR cannot be reused). Bump the branch attempt (§2b "sessionBranch"); inert
			// when the rail is dormant.
			branchAttempt++
			// Loop to step 2 (re-draft AND re-run PM-critique) with the architect's
			// feedback woven in — both the free-text Notes AND the JSONPath-anchored
			// Comments (consulted ONLY here, on Reject).
			feedback = rejectFeedback
			state.stage = StageRedrafting
			continue

		case ReviewWithdraw:
			notes := signalNotes(sig.Feedback)
			newVersion, err := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.WithdrawArtifactActivity, MutateArtifactArgs{
					ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind), Notes: notes,
				}).Get(ctx, &v)
				return v, e
			})
			if err != nil {
				return CoAuthorUnknown, err
			}
			headVersion = newVersion
			state.stage = StageWithdrawn
			return CoAuthorWithdrawn, nil

		default:
			return CoAuthorUnknown, temporal.NewNonRetryableApplicationError("unknown review decision", "UnknownReviewDecision", nil)
		}
	}
}

// ===========================================================================
// PhaseAdvanceWorkflow — seals Phase 1 (systemDesignManager.md §6.3). Retained as
// a public, standalone short-lived gating workflow (advancePhase op) AND invoked
// inline by the parent on Phase-1 seal (runPhaseAdvance).
// ===========================================================================

// phaseAdvanceWorkflowID derives the continuity token for the short-lived gating
// workflow: {projectId}:phaseAdvance (systemDesignManager.md §6.1).
func phaseAdvanceWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:phaseAdvance", projectID)
}

// PhaseAdvanceInput is the start payload for PhaseAdvanceWorkflow.
type PhaseAdvanceInput struct {
	ProjectID ProjectID
}

func (wf *Workflows) PhaseAdvanceWorkflow(ctx workflow.Context, in PhaseAdvanceInput) (PhaseAdvanceResult, error) {
	return wf.runPhaseAdvance(ctx, in.ProjectID)
}

// runPhaseAdvance is the shared seal gate body, called by both the standalone
// PhaseAdvanceWorkflow and the parent SystemDesignPhaseWorkflow.
func (wf *Workflows) runPhaseAdvance(ctx workflow.Context, projectID ProjectID) (PhaseAdvanceResult, error) {
	var proj projectstate.Project
	if p, err := wf.readProject(ctx, projectID); err != nil {
		if !isReadNotFound(err) {
			return PhaseAdvanceResult{}, err
		}
		proj = projectstate.Project{ID: projectstate.ProjectID(projectID)}
	} else {
		proj = p
	}

	// Gate: every required Phase-1 kind must be Committed.
	var missing []ArtifactKind
	for _, kind := range phase1RequiredKinds() {
		if slotFor(proj, kind).Status != projectstate.ReviewCommitted {
			missing = append(missing, kind)
		}
	}
	if len(missing) > 0 {
		return PhaseAdvanceResult{Advanced: false, MissingArtifacts: missing}, nil
	}

	// All required slots committed → seal. Per the agentic pivot (§0d.5) the
	// artifactValidationEngine is DROPPED from this Manager: validity is the required
	// CI check inside the Action (a slot only reaches ReviewCommitted after its design
	// job's CI validation went green AND the architect Approved), so an in-workflow
	// re-validation of the standard-check here would re-implement the CI gate the
	// Action already enforces. The all-committed gate is the seal condition.

	// Seal Phase 1.
	if _, err := wf.applyRecovering(ctx, projectID, proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.AdvancePhaseActivity, AdvancePhaseArgs{
			ProjectID: projectstate.ProjectID(projectID), ExpectedVersion: expected,
		}).Get(ctx, &v)
		return v, e
	}); err != nil {
		return PhaseAdvanceResult{}, err
	}
	return PhaseAdvanceResult{Advanced: true}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers (deterministic; no clock, no RNG).
// ---------------------------------------------------------------------------

// coAuthorState is the live technical state backing the sessionState Query.
type coAuthorState struct {
	projectID    ProjectID
	artifactKind ArtifactKind
	stage        SessionStage
	draft        projectstate.ArtifactModel
	findings     []Finding
	// failureReason is set only on StageDraftFailed: the neutral job Diagnostic, the
	// human "why" for the SPA's retry/withdraw screen.
	failureReason string
	// unresolvedCritique, when non-empty, is the PM critique note that did not
	// converge within maxRedraftAttempts; surfaced at the human gate as a WARNING
	// finding so the architect makes the final call (warnings don't block Approve).
	unresolvedCritique string
}

func (s *coAuthorState) view() (SessionStateView, error) {
	findings := s.findings
	if s.unresolvedCritique != "" {
		findings = append(append([]Finding{}, findings...), Finding{
			RuleID:   "PM-CRITIQUE-UNRESOLVED",
			Severity: SeverityWarning,
			Message:  "PM critique did not converge after max attempts; latest note: " + s.unresolvedCritique,
		})
	}
	draft, err := draftModelFor(s.artifactKind, s.draft)
	if err != nil {
		return SessionStateView{}, err
	}
	return SessionStateView{
		ProjectID:     s.projectID,
		ArtifactKind:  s.artifactKind,
		Stage:         s.stage,
		Draft:         draft,
		Findings:      findings,
		FailureReason: strPtrOrNil(s.failureReason),
	}, nil
}

func stageForAttempt(attempt int) SessionStage {
	if attempt > 0 {
		return StageRedrafting
	}
	return StageDrafting
}

func signalNotes(f *ReviewFeedback) string {
	if f != nil {
		return f.Notes
	}
	return ""
}

// reviewFeedbackOrZero dereferences the signal's optional ReviewFeedback, returning
// the zero value (empty Notes, no Comments) when absent. Used on the Reject loop,
// which weaves both Notes and the JSONPath-anchored Comments into the redraft.
func reviewFeedbackOrZero(f *ReviewFeedback) ReviewFeedback {
	if f != nil {
		return *f
	}
	return ReviewFeedback{}
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
	default:
		return projectstate.ArtifactSlot{}
	}
}

// awaitDraftFailedRecovery lands a failed/non-converging design job in the human-
// visible StageDraftFailed and suspends at the EXISTING reviewDecision gate (plus
// the requestArtifactDraft redraft lever), awaiting a human decision (§0d.4 — the
// anti-wedge requirement). The workflow stays OPEN and QUERYABLE as StageDraftFailed
// throughout, carrying the neutral job Diagnostic as the FailureReason, so the SPA
// renders "your design job failed: <diagnostic> — retry or withdraw" and NEVER an
// infinite Drafting spinner. A ran-but-failed job is terminal-at-the-Manager — it is
// escalated to the human gate, not absorbed in an auto-retry budget.
//
// Recovery levers:
//   - SignalRedraft (requestArtifactDraft's "Retry draft") → re-dispatch in place.
//   - SignalReviewDecision{Reject} → Retry-via-Reject: re-dispatch with the reject
//     feedback woven in (the contract's "human Retry (via reject)" path).
//   - SignalReviewDecision{Withdraw} → withdraw + end gracefully (CoAuthorWithdrawn).
//
// Returns (outcome, retry, err): retry==true means re-dispatch the draft (the caller
// increments redraftCount and loops); retry==false means end with outcome.
func (wf *Workflows) awaitDraftFailedRecovery(
	ctx workflow.Context,
	projectID ProjectID,
	kind ArtifactKind,
	headVersion projectstate.Version,
	diagnostic string,
	state *coAuthorState,
	feedback *ReviewFeedback,
) (CoAuthorOutcome, bool, error) {
	// Surface the human-visible failed stage + the neutral diagnostic for the Query.
	state.stage = StageDraftFailed
	state.failureReason = draftFailedReason(diagnostic)

	redraftCh := workflow.GetSignalChannel(ctx, SignalRedraft)
	reviewCh := workflow.GetSignalChannel(ctx, SignalReviewDecision)

	for {
		var retry bool
		var withdraw bool
		var withdrawNotes string

		sel := workflow.NewSelector(ctx)
		sel.AddReceive(redraftCh, func(c workflow.ReceiveChannel, _ bool) {
			var sig RedraftSignal
			c.Receive(ctx, &sig)
			if sig.Feedback != nil {
				*feedback = *sig.Feedback
			}
			retry = true
		})
		sel.AddReceive(reviewCh, func(c workflow.ReceiveChannel, _ bool) {
			var sig ReviewDecisionSignal
			c.Receive(ctx, &sig)
			switch sig.Decision {
			case ReviewWithdraw:
				withdraw = true
				withdrawNotes = signalNotes(sig.Feedback)
			case ReviewReject:
				// Retry-via-Reject: re-dispatch with the architect's feedback woven in.
				*feedback = reviewFeedbackOrZero(sig.Feedback)
				retry = true
			default:
				// Approve at a failed gate is meaningless (no staged draft) — ignored.
			}
		})
		sel.Select(ctx)

		if retry {
			// Clear the failed state before re-entering the draft loop.
			state.stage = StageRedrafting
			state.failureReason = ""
			return CoAuthorUnknown, true, nil
		}
		if withdraw {
			if _, err := wf.applyRecovering(ctx, projectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.WithdrawArtifactActivity, MutateArtifactArgs{
					ProjectID: projectstate.ProjectID(projectID), ExpectedVersion: expected, Kind: toPSKind(kind), Notes: withdrawNotes,
				}).Get(ctx, &v)
				return v, e
			}); err != nil {
				return CoAuthorUnknown, false, err
			}
			return CoAuthorWithdrawn, false, nil
		}
		// A non-actionable review decision at the failed gate: stay suspended.
	}
}

// draftFailedReason renders the human "why" for the StageDraftFailed screen from
// the job's neutral Diagnostic. It is infrastructure-neutral (the Diagnostic is
// already a summary, not a log firehose — constructionPipelineAccess.md Non-goal #4).
func draftFailedReason(diagnostic string) string {
	if diagnostic == "" {
		return "the design job failed in CI — retry or withdraw"
	}
	return "the design job failed in CI: " + diagnostic + " — retry or withdraw"
}
