package systemdesign

import (
	"errors"
	"fmt"
	"strings"
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
	ProjectState projectstate.ProjectStateAccess
	Pipeline     constructionPipelineAccess

	// Rail + Repo are the OPTIONAL git-forward PR rail (I-DESIGN-DISPATCH §2b). When
	// both are non-nil AND a repo resolves for the project, the CoAuthor spine wraps
	// each draft in the settled branch→PR→read-back→+1→merge model: ensure the session
	// branch, open a PR (head=sessionBranch, base=main), read back + stage on the
	// session branch, then on Approve guard-check + relay the +1 + merge to main before
	// committing on main. When either is nil (the Postgres/non-git composition, or every
	// existing test) the spine runs UNCHANGED — read-back/stage on main, no branch/PR
	// ops — so the branch-aware path is purely additive and dormant-when-unwired,
	// exactly like the construction Manager's git-forward slice.
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
	// F80c: the server-side diverged-branch reconcile at the approve/merge window.
	actReconcileBranch = "ReconcileBranchActivity"
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
			// BOUND the read retries. A read that faults RETRYABLY (Transient / Infrastructure /
			// RateLimited) must NOT loop forever — pre-fix a decode failure of committed state
			// was mis-classified Infrastructure and retried every ~100s indefinitely with no
			// failure surface (QA F36). Decode failures are now TERMINAL (ContractMisuse, listed
			// below), but a GENUINE persistent infra outage must still surface rather than wedge
			// invisibly, so cap the attempts.
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

// raAuthErrType is the canonical Temporal Type() a rail Activity surfaces for an Auth
// fault. The platform github ClassifyStatus conflates GitHub secondary RATE-LIMIT 403s
// with real permission denials (both → fwra.Auth), and marks it NON-RETRYABLE — so the
// bounded rail retry (QA F35 + its draft-round-trip twin) must run WORKFLOW-SIDE
// (isRailAuthFault), since the Activity RetryPolicy cannot retry a non-retryable
// ApplicationError.
var raAuthErrType = fwmanager.RAErrType(fwra.Auth)

// isRailAuthFault reports whether err is a rail Auth fault (the rate-limit-403-as-Auth
// the bounded workflow-side retry absorbs) — shared by the dispatch-time (OpenBranch /
// OpenPullRequest) and approve-time (status/review/merge) rail verbs.
func isRailAuthFault(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raAuthErrType
	}
	return false
}

// raContractMisuseErrType is the canonical Temporal Type() the read Activities surface when
// the committed state DECODES MALFORMED (a closed-enum field carrying free prose, a type
// mismatch) — the projectstate codec now classifies these ContractMisuse (terminal) rather
// than Infrastructure (QA F36). On a pure READ path there is no bad-argument misuse to
// confuse it with (the addressed absence is NotFound), so a ContractMisuse from a read-back
// is unambiguously a decode-of-committed-state failure.
var raContractMisuseErrType = fwmanager.RAErrType(fwra.ContractMisuse)

// isTerminalReadBack reports whether a read-back error is a TERMINAL decode-of-committed-
// state fault retry cannot fix, and returns the decode diagnostic. fwmanager.MapError
// preserves the RA error's message (the "…is not a recognized Trigger wire name" text) as
// the ApplicationError message, so the caller can surface it verbatim at the human
// StageDraftFailed gate instead of looping the read-back Activity forever (QA F36).
func isTerminalReadBack(err error) (string, bool) {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.Type() == raContractMisuseErrType {
		return appErr.Message(), true
	}
	return "", false
}

// readProject runs the ReadProject Activity and returns the whole head-state
// aggregate. A brand-new project surfaces fwra.NotFound (see isReadNotFound).
func (wf *workflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
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
func (wf *workflows) readVersion(ctx workflow.Context, projectID ProjectID) (projectstate.Version, error) {
	c := readProjectOpts(ctx)
	var v projectstate.Version
	if err := workflow.ExecuteActivity(c, wf.ReadProjectVersionActivity, projectID).Get(ctx, &v); err != nil {
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
// Conflict re-read→re-apply loop (D-PA §6/§7). branch names the substrate the mutation
// targets so the Conflict re-read reads the RIGHT version (the session branch for a
// review-window branch mutation, main for a main mutation) — see readVersionOnBranch (QA
// F29). branch=="" is the original main-only behavior every existing caller relied on.
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

// phaseInput is the start payload for SystemDesignPhaseWorkflow.
type phaseInput struct {
	ProjectID ProjectID
}

func (wf *workflows) SystemDesignPhaseWorkflow(ctx workflow.Context, in phaseInput) error {
	logger := workflow.GetLogger(ctx)

	// Drive the seven steps in fixed Method order. For each step, spawn the child
	// gate and auto-advance only on the child's Approve outcome; a Withdraw holds
	// the phase at that step (the operator re-enters via requestArtifactDraft).
	for _, kind := range phase1RequiredKinds() {
		childID := coAuthorWorkflowID(in.ProjectID, kind)
		cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID: childID,
		})
		var outcome coAuthorOutcome
		if err := workflow.ExecuteChildWorkflow(cctx, executionKindCoAuthor, coAuthorInput{
			ProjectID:    in.ProjectID,
			ArtifactKind: kind,
		}).Get(ctx, &outcome); err != nil {
			return err
		}
		if outcome != coAuthorApproved {
			// The human withdrew this step; the phase does not advance. The parent
			// stops here — re-entry is via a fresh requestArtifactDraft on the step.
			logger.Info("co-author step not approved; halting phase sequence", "kind", artifactKindString(kind), "outcome", int(outcome))
			return nil
		}
		logger.Info("co-author step approved; advancing phase sequence", "kind", artifactKindString(kind))
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

// reviewDecisionSignal is the reviewDecision signal payload (systemDesignManager.md §6.5).
type reviewDecisionSignal struct {
	Decision ReviewDecision
	Feedback *ReviewFeedback
	// Approver is the human-facing label for the acting identity that submitted this
	// decision (PM-P2-4), derived from the SubmitReviewDecision caller's SecurityPrincipal.
	// Consulted only on Approve → recorded as the commit's approvedBy provenance. Empty when
	// no identity reached the manager op (absent provenance allowed). Additive to the signal
	// payload — an older buffered signal decodes it as "".
	Approver string
}

// railDraftedBy renders the PM-P2-4 draftedBy provenance: the agentic design rail identity,
// plus the amendment-session marker when this run is a reopening (Amendment > 0). v1 does
// not carry a PR number here (the rail branch is deterministic from project+kind+amendment).
func railDraftedBy(amendment int) string {
	if amendment > 0 {
		return fmt.Sprintf("agentic-design-rail (amend-%d)", amendment)
	}
	return "agentic-design-rail"
}

// redraftSignal is the redraft signal payload — the "Retry draft" lever delivered
// to a CoAuthorArtifactWorkflow suspended in the StageRefused recovery gate
// (requestArtifactDraft's retry path). Feedback is the optional re-request feedback
// woven into the next draft dispatch.
type redraftSignal struct {
	Feedback *ReviewFeedback
}

func (wf *workflows) CoAuthorArtifactWorkflow(ctx workflow.Context, in coAuthorInput) (coAuthorOutcome, error) {
	// Live technical state backing the sessionState Query (§6.5/§6.6).
	state := &coAuthorState{
		projectID:    in.ProjectID,
		artifactKind: in.ArtifactKind,
		stage:        StageDrafting,
	}
	if err := workflow.SetQueryHandler(ctx, querySessionState, state.view); err != nil {
		return coAuthorUnknown, err
	}

	// Carry expectedVersion forward in workflow state (read-your-writes; D-PA §6).
	var headVersion projectstate.Version

	// Step 1: read the project head-state once (prior typed models + ResearchInput + version).
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

	// Capture the committed CoreUseCases once (founder extension, 2026-07-05): the
	// KindSystem read-back check needs it to flag a System draft that leaves any
	// committed use case without a dynamic view (USECASE-DYNAMIC-MISSING). Read
	// deterministically from the head-state proj, so it is replay-safe.
	if cuc, ok := proj.CoreUseCases.Model.(*projectstate.CoreUseCases); ok {
		state.committedCoreUseCases = cuc
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

	// reviewRound is the monotonic REJECT-round counter within THIS session (F40). The
	// session now commits to ONE persistent branch (no branch-per-attempt), so this counter
	// no longer selects a branch — it survives ONLY to stamp the durable review-ledger
	// comment ids (r{round}c{n}) so a fresh reject's comments do not collide with a prior
	// round's on the SAME accumulating thread. Bumped only on an AwaitingReview-gate REJECT.
	reviewRound := 0

	// amendmentSeeded guards the one-time F38 ledger seed: when this is an AMENDMENT session
	// (in.Amendment > 0) the reopening feedback is recorded as round-0 OPEN ledger entries
	// right after the first stage, so the reviewer/agent track the "why" of the reopening.
	amendmentSeeded := false

	// The UC1a spine (systemDesignManager.md §0b §3): each iteration produces a
	// reviewable draft (dispatch → observe → read-back, plus the PM-critique round for
	// PM-reviewed kinds), stages it, suspends on the human gate, and acts on the
	// architect's decision. The per-step control flow (proceed / redraft / return) is
	// carried out of the phase helpers as a coAuthorStep so the loop body stays flat.
	for {
		gf, draft, readBackVersion, step := wf.produceReviewableDraft(ctx, in, proj, &feedback, &redraftCount, headVersion, state)
		switch step.action {
		case actionReturn:
			return step.outcome, step.err
		case actionRedraft, actionReAwait:
			// actionReAwait cannot arise from the draft phase; grouped defensively (re-loop).
			continue
		case actionProceed:
		}

		// QA F29: adopt the ACTUAL read-back substrate version as the head version before
		// staging. The read-back read the session branch (rail) or main (dormant); its
		// Version is the correct optimistic-concurrency token for the stage-on-branch. A
		// fresh workflow reusing a dirty session branch (prior draft/critique commits left
		// it ahead of main) would otherwise stage against the stale main-captured version
		// and Conflict non-recoverably. In the dormant path the read-back version equals
		// the main head, so this is a no-op there.
		headVersion = readBackVersion

		// Track the staged typed draft for the query (render is off the spine).
		state.draft = draft

		// Step 5: stageArtifactForReview, with the workflow-level Conflict loop.
		newVersion, err := wf.stageDraftForReview(ctx, in, draft, gf, headVersion)
		if err != nil {
			// CRASH CONTAINMENT (QA F29). A stage-for-review activity fault must NOT kill the
			// workflow — it had no recoverable gate (only dispatch/reject faults were contained
			// after F15/F28). Land at the human-visible StageDraftFailed gate keeping the
			// feedback, so a Retry redrafts. A workflow-cancellation still propagates.
			if temporal.IsCanceledError(err) {
				return coAuthorUnknown, err
			}
			stageStep := wf.recoverAtFailedGate(ctx, in, headVersion, stageFailedReason(err), "", state, &feedback, &redraftCount)
			switch stageStep.action {
			case actionReturn:
				return stageStep.outcome, stageStep.err
			case actionRedraft, actionProceed, actionReAwait:
				// recoverAtFailedGate returns only redraft/return; the rest re-loop defensively.
				continue
			}
		}
		headVersion = newVersion
		state.stage = StageAwaitingReview
		// A fresh AwaitingReview supersedes any prior approve-fault notice (QA F35).
		state.failureReason = ""
		// F38 AMENDMENT SEED: on the first stage of an amendment session, record the reopening
		// feedback as round-0 OPEN ledger entries on the (now-staged) session branch so the
		// reviewer sees the "why" and the redraft loop can track it. Once only.
		amendmentSeeded = wf.maybeSeedAmendment(ctx, in, gf, &headVersion, amendmentSeeded, state)
		// REVIEW LEDGER: refresh the durable thread from the branch the draft was staged on so
		// the sessionState Query surfaces the live comments (with the drafting agent's responses,
		// normalized on the stage) and the approve gate can block while any comment is open.
		// Best-effort — a transient read miss keeps the last-known thread.
		if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
			state.reviewThread = thread
		}

		// Step 6/7: the review gate. Await a decision and act on it (see awaitReviewGate).
		// An approve/merge-window fault is CONTAINED as actionReAwait (QA F35) INSIDE the gate
		// helper — it re-suspends there WITHOUT redrafting, so the helper only ever hands back
		// return (withdraw) or redraft/proceed (reject → the outer loop redrafts; approve →
		// the outer loop re-derives the next draft).
		gateStep := wf.awaitReviewGate(ctx, in, gf, &headVersion, &reviewRound, &redraftCount, &feedback, state)
		switch gateStep.action {
		case actionReturn:
			return gateStep.outcome, gateStep.err
		case actionRedraft, actionProceed, actionReAwait:
			continue
		}
	}
}

// awaitReviewGate suspends at the AwaitingReview gate, multiplexing the review DECISION
// signal with the SetReviewCommentStatus (waive / reopen) signal. A status signal mutates
// the durable review ledger on the session branch and re-suspends at THIS gate WITHOUT
// redrafting; an approve/merge-window fault is contained as actionReAwait and likewise
// re-suspends here (the staged draft is intact — QA F35). Only a review DECISION that the
// gate cannot recover in place returns: withdraw → actionReturn; reject → actionRedraft;
// approve+merge → actionProceed. gf is the per-iteration git session; it is loop-local in
// the spine (re-derived every outer iteration), so passing it by value is safe.
func (wf *workflows) awaitReviewGate(
	ctx workflow.Context,
	in coAuthorInput,
	gf gitSession,
	headVersion *projectstate.Version,
	reviewRound *int,
	redraftCount *int,
	feedback *ReviewFeedback,
	state *coAuthorState,
) coAuthorStep {
	for {
		// REVIEW LEDGER: multiplex the review decision with the SetReviewCommentStatus
		// signal. A status signal mutates the durable ledger on the branch and re-suspends
		// at THIS gate WITHOUT redrafting; a review decision proceeds exactly as before.
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
			wf.applyCommentStatus(ctx, in, gf, headVersion, stSig, state)
			continue
		}

		decision := wf.handleReviewDecision(ctx, in, sig, headVersion, reviewRound, redraftCount, feedback, &gf, state)
		if decision.action == actionReAwait {
			continue
		}
		return decision
	}
}

// ---------------------------------------------------------------------------
// CoAuthorArtifactWorkflow phase helpers (mechanical decomposition of the UC1a
// spine; NO change to the ORDER of workflow commands). Each helper runs its
// activities/signals in the same sequence the inline loop did and reports back a
// coAuthorStep telling the loop whether to proceed to the human gate, redraft, or
// return an outcome.
// ---------------------------------------------------------------------------

// coAuthorAction is the loop-control verb a phase helper hands back to the spine.
type coAuthorAction int

const (
	// actionProceed: the sub-step produced a reviewable draft; advance to staging.
	actionProceed coAuthorAction = iota
	// actionRedraft: re-enter the draft loop (counters already advanced).
	actionRedraft
	// actionReturn: terminate the workflow with the carried outcome/err.
	actionReturn
	// actionReAwait: re-suspend at the SAME AwaitingReview gate WITHOUT redrafting (QA F35).
	// Used when a transient approve/merge-window fault is contained: the staged draft is
	// intact on the session branch, so the session returns to AwaitingReview carrying a
	// queryable notice and awaits another reviewDecision (the human simply re-approves). A
	// redraft would discard an approved-quality draft, so it MUST NOT be used here.
	actionReAwait
)

// coAuthorStep is a phase helper's report to the spine: what to do next, plus the
// terminal outcome/err when the action is actionReturn.
type coAuthorStep struct {
	outcome coAuthorOutcome
	action  coAuthorAction
	err     error
}

func stepProceed() coAuthorStep { return coAuthorStep{action: actionProceed} }
func stepRedraft() coAuthorStep { return coAuthorStep{action: actionRedraft} }
func stepReAwait() coAuthorStep { return coAuthorStep{action: actionReAwait} }
func stepReturn(o coAuthorOutcome) coAuthorStep {
	return coAuthorStep{action: actionReturn, outcome: o}
}
func stepErr(err error) coAuthorStep {
	return coAuthorStep{action: actionReturn, outcome: coAuthorUnknown, err: err}
}

// produceReviewableDraft runs one draft round-trip and (for PM-reviewed kinds) the
// PM-critique round, returning the read-back draft, the per-iteration git session, and
// the loop-control step. It short-circuits after the draft round unless that round
// asked the spine to proceed.
func (wf *workflows) produceReviewableDraft(
	ctx workflow.Context,
	in coAuthorInput,
	proj projectstate.Project,
	feedback *ReviewFeedback,
	redraftCount *int,
	headVersion projectstate.Version,
	state *coAuthorState,
) (gitSession, projectstate.ArtifactModel, projectstate.Version, coAuthorStep) {
	draft, gf, readBackVersion, step := wf.runDraftRoundTrip(ctx, in, proj, feedback, headVersion, redraftCount, state)
	if step.action != actionProceed {
		return gf, draft, readBackVersion, step
	}
	// F40: the PM-critique commits its verdict to the SAME persistent session branch
	// (sequentially after the draft; the asset template opens no critique PR), so the draft
	// read-back version stays the correct expected version for the AwaitingReview stage —
	// the critique's own commit advances the branch, and the stage re-reads it (QA F29).
	return gf, draft, readBackVersion, wf.runPMCritique(ctx, in, draft, gf, headVersion, feedback, redraftCount, state)
}

// runDraftRoundTrip is the DRAFT round-trip (agentic pivot): compose the architect-role
// prompt IN-MEMORY, dispatch a claude-code-action DESIGN job, observe it to a typed
// terminal phase, and read back the typed model the Action committed. On a terminal
// FAILURE phase the session lands in StageDraftFailed and suspends at the human gate
// (the anti-wedge rule) — never a perpetual Drafting.
func (wf *workflows) runDraftRoundTrip(
	ctx workflow.Context,
	in coAuthorInput,
	proj projectstate.Project,
	feedback *ReviewFeedback,
	headVersion projectstate.Version,
	redraftCount *int,
	state *coAuthorState,
) (projectstate.ArtifactModel, gitSession, projectstate.Version, coAuthorStep) {
	logger := workflow.GetLogger(ctx)
	var draft projectstate.ArtifactModel
	state.stage = stageForAttempt(*redraftCount)

	// The ONE persistent SESSION BRANCH the Action drafts + commits + opens its PR on (F40).
	// STABLE across every redraft/reject round of this session (no per-attempt suffix); a
	// fresh amendment session selects a new branch via in.Amendment. Inert (just a string)
	// when the rail is dormant. beginSession's OpenBranch is idempotent (a no-op re-open on
	// the second and later rounds), and openPR returns the existing PR handle.
	sessionBranch := designBranch(in.ProjectID, in.ArtifactKind, in.Amendment)

	// RESUME CHECKPOINT (F35 twin): consume the marker. When set, a PRIOR attempt of THIS
	// session already committed the draft on the branch and then faulted at a POST-read-back
	// rail step (openPR) — so this Retry must NOT re-dispatch (Claude onto a branch that
	// already carries the model would red the no-commit guard). Cleared here; re-armed only if
	// openPR faults again below.
	resuming := state.resumeFromReadBack
	state.resumeFromReadBack = false

	// Rail (dispatch-time half): mint the credential + ensure the session branch exists
	// BEFORE the Action drafts on it. A dormant rail returns a disabled session and the
	// spine runs unchanged (read-back/stage on main, no branch/PR ops).
	gf, gerr := wf.beginSession(ctx, in.ProjectID, sessionBranch)
	if gerr != nil {
		if temporal.IsCanceledError(gerr) {
			return draft, gf, 0, stepErr(gerr)
		}
		// OpenBranch / mintCred faulted BEFORE any draft landed — even after the shared bounded
		// Auth retry exhausted (a genuine permission denial or a persistent secondary-rate-limit
		// 403). CONTAIN it (never crash the whole CoAuthor workflow): land at the human-visible
		// StageDraftFailed gate. This is pre-read-back, so a Retry safely re-dispatches (no
		// resume marker is set).
		logger.Warn("session begin (OpenBranch) faulted after the bounded Auth retry; entering StageDraftFailed", "error", gerr.Error())
		return draft, gf, 0, wf.recoverAtFailedGate(ctx, in, headVersion, railStepFailedReason("preparing the review branch", gerr), "", state, feedback, redraftCount)
	}

	var (
		model           projectstate.ArtifactModel
		readBackVersion projectstate.Version
		haveDraft       bool
	)
	if resuming {
		// RESUME PROBE (F35 twin): re-run the read-back FIRST. The draft is already committed on
		// the branch from the faulted attempt; if it is present + decodes, SKIP the re-dispatch —
		// a re-dispatch would red the no-commit guard ("claude committed nothing") on a branch
		// that already carries the model, and would burn another 20+ minute draft.
		if m, v, rbErr := wf.readBackCommittedModelOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch()); rbErr == nil {
			model, readBackVersion, haveDraft = m, v, true
			logger.Info("resuming draft round-trip from read-back; skipping re-dispatch (draft already committed on the branch)")
		} else {
			// No usable draft on the branch after all (e.g. it was never committed) — fall through
			// to a fresh dispatch. The acceptable-minimum resume rule: read-back first, dispatch only
			// if the model is absent.
			logger.Warn("resume read-back found no usable draft; re-dispatching a fresh draft", "error", rbErr.Error())
		}
	}
	if !haveDraft {
		m, v, step := wf.dispatchDraftAndReadBack(ctx, in, proj, gf, sessionBranch, feedback, headVersion, redraftCount, state)
		if step.action != actionProceed {
			return draft, gf, 0, step
		}
		model, readBackVersion = m, v
	}
	draft = model
	state.findings = nil
	// AMENDMENT NO-CHANGE GUARD (defense-in-depth for the F40 zero-new-commit 422): an
	// amendment branch is cut from main, which ALREADY carries the committed model, so — unlike
	// a first draft on an empty slot — the read-back above still SUCCEEDS even when the job
	// advanced the branch by nothing. Opening a PR on such an un-advanced branch 422s ("no
	// commits between base and head"). So for an amendment, verify the branch actually MOVED
	// the artifact beyond main before opening the PR: compare the branch read-back to the
	// committed main model (proj was read on main at session start). Byte-identical ⇒ the
	// amendment produced no change ⇒ land the honest failure at the human gate (Retry/Withdraw)
	// instead of 422-crashing the rail. The run-scoped idempotency key is the primary fix (a
	// fresh run now genuinely dispatches + seeds, so this rarely trips); this guard closes the
	// residual "job ran but changed nothing" case the template's no-commit guard may miss.
	if in.Amendment > 0 {
		unchanged, cmpErr := sameArtifactModel(model, slotFor(proj, in.ArtifactKind).Model)
		if cmpErr != nil {
			return draft, gf, 0, stepErr(cmpErr)
		}
		if unchanged {
			logger.Warn("amendment draft committed no change to the artifact; entering StageDraftFailed")
			return draft, gf, 0, wf.recoverAtFailedGate(ctx, in, headVersion, amendmentNoChangeReason(), "", state, feedback, redraftCount)
		}
	}
	// Rail: open the PR (head=sessionBranch, base=main) ONLY NOW — AFTER the read-back
	// CONFIRMED a committed model on the session branch, so the branch has ≥1 commit beyond
	// main and GitHub will not 422 "no commits between base and head" (F40 fix). Opening it
	// before the first commit lands 422s on a freshly-cut branch (observed on gtdapp amendment
	// kind 1: the -amend-N branch was cut from main with zero commits). Idempotent on head —
	// subsequent reject/redraft rounds reuse the SAME PR; the server's handle is authoritative
	// for the merge step.
	if err := wf.openPR(ctx, &gf, in.ArtifactKind); err != nil {
		if temporal.IsCanceledError(err) {
			return draft, gf, 0, stepErr(err)
		}
		// POST-read-back rail fault after the shared bounded Auth retry exhausted (QA F35 twin):
		// a genuine permission denial or a persistent secondary-rate-limit 403. The draft is
		// ALREADY committed on the session branch, so DO NOT crash and DO NOT let a naive Retry
		// re-dispatch (that would red the no-commit guard). CONTAIN at the failed gate AND
		// checkpoint a read-back RESUME, so the Retry re-opens the PR on the preserved draft
		// without burning another 20+ minute draft.
		state.resumeFromReadBack = true
		logger.Warn("openPR faulted after read-back (bounded Auth retry exhausted); entering StageDraftFailed — retry resumes from read-back, no re-dispatch", "error", err.Error())
		return draft, gf, 0, wf.recoverAtFailedGate(ctx, in, headVersion, railStepFailedReason("opening the review pull request", err), "", state, feedback, redraftCount)
	}
	return draft, gf, readBackVersion, stepProceed()
}

// dispatchDraftAndReadBack runs ONE dispatch → observe → read-back on the session branch. On
// success it returns the read-back model + version and stepProceed(). On any terminal failure it
// returns a non-Proceed step already routed to the correct recovery (dispatch-failed / job-failed
// / malformed-read-back gate) — the caller returns it verbatim. Extracted from runDraftRoundTrip
// so the resume path (which SKIPS this whole block) reads cleanly and the function stays within
// the gocognit budget.
func (wf *workflows) dispatchDraftAndReadBack(
	ctx workflow.Context,
	in coAuthorInput,
	proj projectstate.Project,
	gf gitSession,
	sessionBranch string,
	feedback *ReviewFeedback,
	headVersion projectstate.Version,
	redraftCount *int,
	state *coAuthorState,
) (projectstate.ArtifactModel, projectstate.Version, coAuthorStep) {
	logger := workflow.GetLogger(ctx)
	// REVIEW LEDGER: on a redraft, state.reviewThread carries the durable open comments
	// (reloaded after the reject-append); the prompt lists each for the agent to respond to.
	draftPrompt := architectDraftPrompt(toPSKind(in.ArtifactKind), proj, *feedback, state.reviewThread, in.Amendment)
	draftObs, derr := wf.dispatchAndObserve(ctx, dispatchDesignJobArgs{
		ProjectID:     in.ProjectID,
		ArtifactKind:  in.ArtifactKind,
		Target:        dispatchTargetDraft,
		Prompt:        draftPrompt,
		TargetBranch:  sessionBranch,
		PriorStateRef: "",
		// Per-project-design-dispatch: dispatch to the per-project repo + aiarch-design.yml
		// (the rail's repoRef). "" when the rail is dormant ⇒ RA falls back to construction.
		TargetRepo: gf.dispatchRepo(),
	})
	if derr != nil {
		// The DISPATCH/observe round-trip itself FAILED terminally — e.g. GitHub 422s the
		// workflow_dispatch. Route it to the human-visible StageDraftFailed gate (never an
		// invisible crash; QA F15 gap 2a). A workflow-cancellation still propagates.
		logger.Warn("design draft dispatch failed terminally; entering StageDraftFailed", "error", derr.Error())
		return nil, 0, wf.recoverDispatchFailed(ctx, in, headVersion, derr, state, feedback, redraftCount)
	}
	if draftObs.Phase != pipelineSucceeded {
		// The job RAN and FAILED (drafting failed or CI validation went red): land the session
		// in the human-visible StageDraftFailed and suspend on the gate (§0d.4 anti-wedge).
		logger.Warn("design draft job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", draftObs.Diagnostic)
		return nil, 0, wf.recoverDraftFailed(ctx, in, headVersion, draftObs.Diagnostic, draftObs.RunURL, state, feedback, redraftCount)
	}
	// READ-BACK on the SESSION BRANCH (§2a): the Action committed the typed JSON on the session
	// branch; read it back as the not-yet-merged draft (a dormant rail reads main). The read-back
	// Version is the ACTUAL branch version the stage must expect (QA F29), and it CONFIRMS a
	// commit landed before openPR opens the PR (a session that fails before any commit leaves NO
	// PR — F40).
	model, readBackVersion, rbErr := wf.readBackCommittedModelOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch())
	if rbErr != nil {
		if decodeMsg, terminal := isTerminalReadBack(rbErr); terminal {
			// The committed draft DECODES MALFORMED (QA F36) — a terminal fault retry cannot fix.
			// Land at the StageDraftFailed gate carrying the decode diagnostic.
			logger.Warn("design read-back decoded MALFORMED committed state; entering StageDraftFailed", "error", decodeMsg)
			return nil, 0, wf.recoverAtFailedGate(ctx, in, headVersion, readBackDecodeFailedReason(decodeMsg), "", state, feedback, redraftCount)
		}
		return nil, 0, stepErr(rbErr)
	}
	return model, readBackVersion, stepProceed()
}

// runPMCritique is the PM-CRITIQUE round-trip — only for the kinds the Method assigns a
// PM reviewer (mission / glossary+scrubbed / core-use-cases). A SECOND dispatch → observe
// → read-back producing a typed Critique. On CritiqueRevise the loop re-dispatches the
// architect-role draft with the critique Notes woven in, BEFORE the human gate.
// Architect-owned steps proceed straight through.
func (wf *workflows) runPMCritique(
	ctx workflow.Context,
	in coAuthorInput,
	draft projectstate.ArtifactModel,
	gf gitSession,
	headVersion projectstate.Version,
	feedback *ReviewFeedback,
	redraftCount *int,
	state *coAuthorState,
) coAuthorStep {
	if !kindHasPMCritique(toPSKind(in.ArtifactKind)) {
		return stepProceed()
	}
	logger := workflow.GetLogger(ctx)

	// F40: the PM-critique commits its verdict carrier to the SAME persistent session
	// branch as the draft (sequentially, right after the draft's commit) — no separate
	// critique branch, no PR/merge for critique (the asset template opens no critique PR).
	// Inert when the rail is dormant.
	sessionBranch := designBranch(in.ProjectID, in.ArtifactKind, in.Amendment)
	critPrompt := pmCritiquePrompt(toPSKind(in.ArtifactKind), draft)
	critObs, cerr := wf.dispatchAndObserve(ctx, dispatchDesignJobArgs{
		ProjectID:     in.ProjectID,
		ArtifactKind:  in.ArtifactKind,
		Target:        dispatchTargetCritique,
		Prompt:        critPrompt,
		TargetBranch:  sessionBranch,
		PriorStateRef: "",
		// Per-project-design-dispatch: the critique job also runs in the per-project repo.
		TargetRepo: gf.dispatchRepo(),
	})
	if cerr != nil {
		// The critique DISPATCH itself failed terminally — route to the human-visible
		// StageDraftFailed gate (same anti-wedge rule as the draft dispatch), never crash.
		logger.Warn("PM-critique dispatch failed terminally; entering StageDraftFailed", "error", cerr.Error())
		return wf.recoverDispatchFailed(ctx, in, headVersion, cerr, state, feedback, redraftCount)
	}
	if critObs.Phase != pipelineSucceeded {
		// A terminal PM-critique job failure routes to the same StageDraftFailed human
		// gate as a terminal draft failure — never crash the workflow.
		logger.Warn("PM-critique job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", critObs.Diagnostic)
		return wf.recoverDraftFailed(ctx, in, headVersion, critObs.Diagnostic, critObs.RunURL, state, feedback, redraftCount)
	}
	// Read the critique verdict back off the SAME session branch it was committed to.
	critique, crbErr := wf.readBackCritiqueOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch())
	if crbErr != nil {
		if isCritiqueReadBackEmpty(crbErr) {
			// A critique job that reported success but committed NO verdict is a
			// ran-but-incomplete job — the missing-verdict safe default (dispatch.go).
			// Route it to the SAME human-visible StageDraftFailed gate as a terminal job
			// failure (NOT a silent approve, NOT a workflow crash — the anti-wedge rule),
			// awaiting human Retry-via-Reject / Withdraw.
			logger.Warn("PM-critique read-back found no verdict (missing-verdict safe default); entering StageDraftFailed")
			return wf.recoverDraftFailed(ctx, in, headVersion, critiqueMissingVerdictDiagnostic, "", state, feedback, redraftCount)
		}
		if decodeMsg, terminal := isTerminalReadBack(crbErr); terminal {
			// The critique read-back decoded MALFORMED committed state (QA F36) — the same
			// terminal fault as the draft read-back. Land at the human StageDraftFailed gate
			// with the decode diagnostic instead of looping the read-back Activity forever.
			logger.Warn("PM-critique read-back decoded MALFORMED committed state; entering StageDraftFailed", "error", decodeMsg)
			return wf.recoverAtFailedGate(ctx, in, headVersion, readBackDecodeFailedReason(decodeMsg), "", state, feedback, redraftCount)
		}
		return stepErr(crbErr)
	}
	if critique.Verdict == critiqueRevise {
		*redraftCount++
		if *redraftCount >= maxRedraftAttempts {
			// Do NOT crash the workflow (that wedges the SPA). The committed draft is
			// valid (it passed the CI check); stage it for the human gate with the
			// unresolved PM critique surfaced as a note so the architect makes the final
			// call instead of an oscillating critic killing the loop.
			logger.Warn("PM-critique did not converge within max attempts; staging for human review")
			state.unresolvedCritique = critique.Notes
			return stepProceed() // fall through to stage for review.
		}
		// Re-dispatch the architect draft with the PM notes woven in.
		*feedback = ReviewFeedback{Notes: critique.Notes}
		state.stage = StageRedrafting
		return stepRedraft()
	}
	return stepProceed()
}

// recoverDraftFailed lands a RAN-BUT-FAILED design job (a terminal PhaseFailed /
// PhaseCancelled observation, or a missing critique verdict) at the StageDraftFailed
// human gate (the anti-wedge rule). runURL is the failed GitHub Actions run's URL when
// the job actually ran (deep-linked on the SPA's failed card); "" when unavailable.
func (wf *workflows) recoverDraftFailed(
	ctx workflow.Context,
	in coAuthorInput,
	headVersion projectstate.Version,
	diagnostic string,
	runURL string,
	state *coAuthorState,
	feedback *ReviewFeedback,
	redraftCount *int,
) coAuthorStep {
	return wf.recoverAtFailedGate(ctx, in, headVersion, draftFailedReason(diagnostic), runURL, state, feedback, redraftCount)
}

// recoverDispatchFailed lands a terminal DISPATCH/observe fault (the round-trip itself
// errored — e.g. GitHub 422 rejecting the workflow_dispatch, ContractMisuse non-
// retryable) at the SAME StageDraftFailed human gate instead of crashing the workflow
// (QA F15 gap 2a). A workflow-CANCELLATION error is NOT a job failure — it means the
// workflow is being torn down, so it propagates unchanged rather than being masked as a
// draft failure. There is no run to deep-link (dispatch never created one), so runURL="".
func (wf *workflows) recoverDispatchFailed(
	ctx workflow.Context,
	in coAuthorInput,
	headVersion projectstate.Version,
	err error,
	state *coAuthorState,
	feedback *ReviewFeedback,
	redraftCount *int,
) coAuthorStep {
	if temporal.IsCanceledError(err) {
		return stepErr(err)
	}
	return wf.recoverAtFailedGate(ctx, in, headVersion, dispatchFailedReason(err), "", state, feedback, redraftCount)
}

// recoverAtFailedGate suspends at the StageDraftFailed human gate carrying the human
// reason + optional failed-run URL, and maps the recovery outcome to a coAuthorStep: a
// Retry redrafts on the SAME persistent session branch (F40 — the branch-per-retry F32
// topology is unwound; the stale-base problem it addressed is now handled by the workflow
// template's refresh-from-main git step, which re-merges origin/main into the branch before
// each draft) keeping the retained feedback; a Withdraw returns the terminal outcome.
func (wf *workflows) recoverAtFailedGate(
	ctx workflow.Context,
	in coAuthorInput,
	headVersion projectstate.Version,
	reason string,
	runURL string,
	state *coAuthorState,
	feedback *ReviewFeedback,
	redraftCount *int,
) coAuthorStep {
	outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, reason, runURL, state, feedback)
	if recErr != nil {
		return stepErr(recErr)
	}
	if !retry {
		return stepReturn(outcome)
	}
	*redraftCount++
	return stepRedraft()
}

// stageDraftForReview encodes the read-back draft and stages it into its slot
// (status AwaitingReview) through the workflow-level Conflict loop, returning the new
// head version.
func (wf *workflows) stageDraftForReview(
	ctx workflow.Context,
	in coAuthorInput,
	draft projectstate.ArtifactModel,
	gf gitSession,
	headVersion projectstate.Version,
) (projectstate.Version, error) {
	draftEnvelope, encErr := encodeModel(draft)
	if encErr != nil {
		return 0, fwmanager.MapError(encErr)
	}
	return wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.StageArtifactForReviewActivity, stageArtifactForReviewArgs{
			ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Model: draftEnvelope, Branch: gf.readBackBranch(),
		}).Get(ctx, &v)
		return v, e
	})
}

// handleReviewDecision branches on the architect's gate decision (the commit authority),
// applying the corresponding head-state transition and returning the loop-control step.
func (wf *workflows) handleReviewDecision(
	ctx workflow.Context,
	in coAuthorInput,
	sig reviewDecisionSignal,
	headVersion *projectstate.Version,
	reviewRound *int,
	redraftCount *int,
	feedback *ReviewFeedback,
	gf *gitSession,
	state *coAuthorState,
) coAuthorStep {
	switch sig.Decision {
	case ReviewApprove:
		// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open.
		// The manager's SetReviewCommentStatus/approve precondition rejects this synchronously,
		// but this workflow-side guard is the TOCTOU-safe backstop (a comment could be reopened
		// between the manager's query and the signal). Re-suspend at the gate; the reviewer sees
		// the open comments in the queryable thread and waives or redrafts.
		if open := openReviewCommentIDs(state.reviewThread); len(open) > 0 {
			return stepReAwait()
		}
		return wf.commitOnApprove(ctx, in, headVersion, redraftCount, feedback, gf, state, sig.Approver)

	case ReviewReject:
		rejectFeedback := reviewFeedbackOrZero(sig.Feedback)
		// RETAIN the architect's feedback in workflow state BEFORE the head-state write —
		// both the free-text Notes AND the JSONPath-anchored Comments (consulted ONLY on
		// Reject). Setting it first means that if the reject write itself faults (below), the
		// crash-containment recovery gate still holds the feedback so a Retry reuses it
		// instead of silently discarding the architect's send-back (QA F28).
		*feedback = rejectFeedback
		newVersion, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			c := mutateOpts(ctx)
			var v projectstate.Version
			e := workflow.ExecuteActivity(c, wf.RejectArtifactActivity, mutateArtifactArgs{
				ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind), Notes: rejectFeedback.Notes,
				// REVIEW LEDGER (review-ledger §2): fold the reviewer's anchored comments into the
				// reject as durable, server-minted ledger entries, round-stamped by the per-reject
				// review-round counter (a distinct, replay-stable monotonic counter → deterministic,
				// non-colliding ids on the ONE accumulating thread — F40). Empty ⇒ a plain reject.
				Round: int64(*reviewRound), Comments: feedbackToLedgerComments(rejectFeedback),
				// Branch-aware Reject (I-DESIGN-DISPATCH §2a): record the Rejected status on the
				// SESSION BRANCH the draft was staged on — where the staged model exists and the
				// session-branch version (headVersion) matches. In the PR rail main is untouched
				// until an approved draft merges, so a main-path reject would mismatch the version
				// AND find the slot unpopulated (the QA F28 crash). "" when the rail is dormant ⇒
				// the reject lands on main exactly as before.
				Branch: gf.readBackBranch(),
			}).Get(ctx, &v)
			return v, e
		})
		if err != nil {
			// CRASH CONTAINMENT (QA F28). An activity fault while recording the Reject must
			// NOT kill the workflow (that ends the CoAuthor spine FAILED and loses the
			// feedback that rode the signal). Mirror the recoverDispatchFailed pattern: land
			// at the human-visible StageDraftFailed gate carrying a reason, KEEPING the
			// received feedback (*feedback set above) so a Retry redrafts with the architect's
			// comments woven in. A workflow-cancellation still propagates.
			if temporal.IsCanceledError(err) {
				return stepErr(err)
			}
			return wf.recoverAtFailedGate(ctx, in, *headVersion, rejectFailedReason(err), "", state, feedback, redraftCount)
		}
		*headVersion = newVersion
		// REVIEW LEDGER: reload the thread from the SAME persistent session branch the reject
		// just wrote so it carries the freshly-appended OPEN comments — the redraft prompt lists
		// them for the drafting agent to respond to. Under the F40 single-branch topology the
		// redraft stays on THIS branch, so the durable thread truly accumulates round-over-round
		// (closing the review-ledger cross-reject earmark). Best-effort: a miss keeps the prior
		// thread (the comments are durable on the branch either way).
		if thread, terr := wf.loadReviewThread(ctx, in, *gf); terr == nil {
			state.reviewThread = thread
		}
		// F40: the redraft stays on the SAME session branch + PR (no branch bump). Advance only
		// the review-round counter so the NEXT reject's ledger ids do not collide with this
		// round's on the accumulating thread.
		*reviewRound++
		// Loop to step 2 (re-draft AND re-run PM-critique) with the architect's feedback woven in.
		state.stage = StageRedrafting
		return stepRedraft()

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
			return stepErr(err)
		}
		state.stage = StageWithdrawn
		return stepReturn(coAuthorWithdrawn)

	case ReviewDecisionUnknown:
		// The zero value: no legitimate signal carries it. Same terminal rejection as
		// the default case below.
		return stepErr(temporal.NewNonRetryableApplicationError("unknown review decision", "UnknownReviewDecision", nil))

	default:
		return stepErr(temporal.NewNonRetryableApplicationError("unknown review decision", "UnknownReviewDecision", nil))
	}
}

// commitOnApprove runs the approve-time rail half (§2b): the merge GUARD (CI must be
// green) + the architecture +1 relay + the App-mediated merge of sessionBranch → main,
// then commitArtifact on main. A dormant rail returns merged=true with no rail ops (the
// non-git spine). A not-green PR routes to the StageDraftFailed recovery gate.
func (wf *workflows) commitOnApprove(
	ctx workflow.Context,
	in coAuthorInput,
	headVersion *projectstate.Version,
	redraftCount *int,
	feedback *ReviewFeedback,
	gf *gitSession,
	state *coAuthorState,
	approver string,
) coAuthorStep {
	logger := workflow.GetLogger(ctx)
	merged, mErr := wf.mergeOnApprove(ctx, in.ProjectID, gf, in.ArtifactKind)
	if mErr != nil {
		// QA F35: a merge-window fault (PR-status read / +1 relay / merge) must NOT kill the
		// workflow. The staged draft is intact on the session branch and main is untouched,
		// so contain it — return to AwaitingReview with a queryable notice so the human can
		// simply RE-APPROVE (never a redraft, which would discard an approved-quality draft).
		// Cancellation still propagates.
		if temporal.IsCanceledError(mErr) {
			return stepErr(mErr)
		}
		logger.Warn("approve merge-window fault; returning to AwaitingReview for re-approve", "error", mErr.Error())
		return wf.reAwaitAfterApproveFault(state, approveFailedReason(mErr))
	}
	if !merged {
		// The merge guard was NOT green (the required CI check is red on the PR): do NOT
		// merge, do NOT commit. Route to the SAME StageDraftFailed recovery gate as a
		// draft failure (the anti-wedge rule) awaiting Retry-via-Reject / Withdraw.
		logger.Warn("design PR not mergeable at approve (CI not green); entering StageDraftFailed")
		outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, *headVersion, draftFailedReason("the design PR is not green — its required CI check has not passed"), "", state, feedback)
		if recErr != nil {
			return stepErr(recErr)
		}
		if !retry {
			return stepReturn(outcome)
		}
		// F40: Retry-via-Reject from the not-green gate redrafts on the SAME session branch +
		// PR (no branch bump — the template's refresh-from-main handles a stale base).
		*redraftCount++
		return stepRedraft()
	}
	// After merge the draft lives on main; commitArtifact + advancePhase land on main
	// (the canonical head). Re-seed headVersion from main so the commit's CAS starts at
	// main's tip (the session-branch version no longer applies). A dormant rail leaves
	// headVersion as-is (it already tracked main).
	if gf.enabled {
		if mp, rerr := wf.readProject(ctx, in.ProjectID); rerr == nil {
			*headVersion = mp.Version
		} else if !isReadNotFound(rerr) {
			// QA F35: a post-merge re-seed read fault is contained too. The merge already
			// landed on main, so a re-approve re-runs mergeOnApprove idempotently (a merged
			// PR re-merges to a no-op) and re-reads/commits — no redraft, no crash.
			if temporal.IsCanceledError(rerr) {
				return stepErr(rerr)
			}
			logger.Warn("approve post-merge re-seed fault; returning to AwaitingReview for re-approve", "error", rerr.Error())
			return wf.reAwaitAfterApproveFault(state, approveFailedReason(rerr))
		}
	}
	// Commit lands on MAIN after the merge (the re-seed above set headVersion to main's
	// tip), so its Conflict re-read targets main (branch=="").
	if _, err := wf.applyRecovering(ctx, in.ProjectID, "", *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.CommitArtifactActivity, mutateArtifactArgs{
			ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind),
			// PM-P2-4 commit provenance: the approving identity + the drafting rail identity.
			ApprovedBy: approver, DraftedBy: railDraftedBy(in.Amendment),
		}).Get(ctx, &v)
		return v, e
	}); err != nil {
		// QA F35: contain a post-merge commit fault too (same idempotent re-approve recovery).
		if temporal.IsCanceledError(err) {
			return stepErr(err)
		}
		logger.Warn("approve post-merge commit fault; returning to AwaitingReview for re-approve", "error", err.Error())
		return wf.reAwaitAfterApproveFault(state, approveFailedReason(err))
	}
	state.stage = StageCommitted
	return stepReturn(coAuthorApproved)
}

// reAwaitAfterApproveFault contains a transient approve/merge-window fault (QA F35): it
// returns the session to AwaitingReview carrying a queryable notice (surfaced as the
// sessionState FailureReason — no schema change; the STAGE disambiguates it from a
// StageDraftFailed reason) and asks the spine to re-await the gate so the human can simply
// re-approve. The staged draft is untouched.
func (wf *workflows) reAwaitAfterApproveFault(state *coAuthorState, reason string) coAuthorStep {
	state.stage = StageAwaitingReview
	state.failureReason = reason
	state.failureRunURL = ""
	return stepReAwait()
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
// PhaseAdvanceWorkflow — seals Phase 1 (systemDesignManager.md §6.3). Retained as
// a public, standalone short-lived gating workflow (advancePhase op) AND invoked
// inline by the parent on Phase-1 seal (runPhaseAdvance).
// ===========================================================================

// phaseAdvanceWorkflowID derives the continuity token for the short-lived gating
// workflow: {projectId}:phaseAdvance (systemDesignManager.md §6.1).
func phaseAdvanceWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:phaseAdvance", projectID)
}

// phaseAdvanceInput is the start payload for PhaseAdvanceWorkflow.
type phaseAdvanceInput struct {
	ProjectID ProjectID
}

func (wf *workflows) PhaseAdvanceWorkflow(ctx workflow.Context, in phaseAdvanceInput) (PhaseAdvanceResult, error) {
	return wf.runPhaseAdvance(ctx, in.ProjectID)
}

// runPhaseAdvance is the shared seal gate body, called by both the standalone
// PhaseAdvanceWorkflow and the parent SystemDesignPhaseWorkflow.
func (wf *workflows) runPhaseAdvance(ctx workflow.Context, projectID ProjectID) (PhaseAdvanceResult, error) {
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

	// Seal Phase 1. AdvancePhase is a MAIN write (Conflict re-read targets main, branch=="").
	if _, err := wf.applyRecovering(ctx, projectID, "", proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
		c := mutateOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.AdvancePhaseActivity, advancePhaseArgs{
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
	// failureRunURL is set only on StageDraftFailed when the failure came from a design
	// job that actually RAN (the observe-failed path): the URL of the failed GitHub
	// Actions run, so the SPA's failed card can deep-link the operator to the run/logs
	// that explain WHY (QA F15 gap 2b). Empty for the dispatch-REJECTION path (no run
	// was ever created) and for the not-green-PR gate.
	failureRunURL string
	// unresolvedCritique, when non-empty, is the PM critique note that did not
	// converge within maxRedraftAttempts; surfaced at the human gate as a WARNING
	// finding so the architect makes the final call (warnings don't block Approve).
	unresolvedCritique string
	// reviewThread is the durable review ledger for this artifact (review-ledger feature),
	// refreshed from the session branch after every (re)stage and after every waive/reopen
	// so the sessionState Query surfaces the live thread and the approve gate can block
	// while any comment is still open. Nil until the first read-back that carries comments.
	reviewThread []projectstate.ReviewComment
	// committedCoreUseCases is the head-state CoreUseCases (captured once at workflow
	// start), threaded here so the KindSystem read-back check can flag a System draft
	// that leaves any committed use case without a dynamic view (USECASE-DYNAMIC-MISSING,
	// founder extension 2026-07-05). Nil for every other kind and until it is populated.
	committedCoreUseCases *projectstate.CoreUseCases
	// resumeFromReadBack is the F35-twin checkpoint: set true when a POST-read-back rail step
	// (openPR) faulted and the session landed at the failed gate WITH the draft already
	// committed on the branch. On the next Retry the draft round-trip consumes it and RESUMES
	// from the read-back — SKIPPING the re-dispatch — so it does not redispatch Claude onto a
	// branch that already carries the model (which the no-commit guard would red). Workflow-
	// local, deterministic on replay (set from a recorded Activity error, never wall-clock).
	resumeFromReadBack bool
}

func (s *coAuthorState) view() (SessionStateView, error) {
	findings := s.findings
	// APP-SIDE ACTIVITY-DIAGRAM GATE (founder ruling 2026-07-05): the platform
	// artifactValidationEngine is dropped, so shape validity is the Action's CI check;
	// but the CI check does NOT enforce that EVERY use case carries an activity diagram
	// (the committed gtdapp CoreUseCases shipped core use cases with "activity": null).
	// This read-back-time check surfaces one ERROR finding per use case whose activity is
	// null or structurally empty (no start node + action) so the review panel flags it at
	// the human gate. Findings are advisory display — they do not auto-block Approve — so
	// the architect sees the defect and sends the draft back rather than committing it.
	// Appended only for the CoreUseCases kind (nil for every other kind), so the
	// nil-when-empty wire form of Findings is preserved for all other artifacts.
	if extra := useCaseActivityFindings(s.artifactKind, s.draft); len(extra) > 0 {
		findings = append(append([]Finding{}, findings...), extra...)
	}
	// FOUNDER EXTENSION (2026-07-05): a System draft must carry a dynamic view for EVERY
	// committed use case (core AND nonCore variation). Twin of the activity check above;
	// surfaces one ERROR finding per uncovered use case at the review panel.
	if extra := useCaseDynamicFindings(s.artifactKind, s.draft, s.committedCoreUseCases); len(extra) > 0 {
		findings = append(append([]Finding{}, findings...), extra...)
	}
	// F81 (2026-07-05): a System draft must not be layer-DEGENERATE. A drafting agent that
	// omits every component's layer produces an all-client architecture the strict codec
	// silently accepts and the layer-interaction rules pass VACUOUSLY. This read-back
	// surface flags a system with no Managers / no ResourceAccess, or any component whose
	// NAME contradicts its layer, as an ERROR at the review panel — the app-side twin of
	// methodcheck's SYSTEM-LAYER-DEGENERATE (the authoritative gate putDraftModel enforces).
	if extra := systemLayerDegenerateFindings(s.artifactKind, s.draft); len(extra) > 0 {
		findings = append(append([]Finding{}, findings...), extra...)
	}
	// State-validation read-back findings (architect ratification 2026-07-05). Each
	// early-returns for a non-matching kind, so appending them all is safe and only the
	// generators for s.artifactKind produce anything. They are advisory display — the
	// authoritative write-path gate is the platform methodcheck twin (docs/later.md).
	for _, gen := range stateValidationFindingGenerators {
		if extra := gen(s.artifactKind, s.draft); len(extra) > 0 {
			findings = append(append([]Finding{}, findings...), extra...)
		}
	}
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
		FailureRunURL: strPtrOrNil(s.failureRunURL),
		ReviewThread:  reviewThreadToView(s.reviewThread),
	}, nil
}

// useCaseActivityFindings returns one ERROR finding per use case whose activity
// diagram is missing or structurally empty, for the CoreUseCases artifact ONLY
// (nil for every other kind and for a nil/absent draft). The founder ruling
// (2026-07-05) requires EVERY use case — core AND supporting — to carry a
// non-empty activity diagram (a start node plus at least one action step). The
// Action's CI validate check does NOT enforce this (the committed gtdapp
// CoreUseCases shipped core use cases with "activity": null), so this read-back
// check is the app-side surface that flags a diagram-less use case at the review
// panel. It classifies the defect only — full UML well-formedness stays the
// Action's CI concern.
func useCaseActivityFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindCoreUseCases {
		return nil
	}
	cuc, ok := draft.(*projectstate.CoreUseCases)
	if !ok || cuc == nil {
		return nil
	}
	var out []Finding
	for i, d := range cuc.Decisions {
		uc := d.UseCase
		reason := activityDefect(uc.Activity)
		if reason == "" {
			continue
		}
		label := uc.Name
		if label == "" {
			label = fmt.Sprintf("use case %d", i+1)
		}
		out = append(out, Finding{
			RuleID:   "USECASE-ACTIVITY-MISSING",
			Severity: SeverityError,
			Message:  fmt.Sprintf("Use case %q %s; every use case (core AND supporting) must carry a non-empty activity diagram with a start node and at least one action step.", label, reason),
			Location: &Location{Ordinal: int64(i), Section: "use case " + label},
		})
	}
	return out
}

// useCaseDynamicFindings returns one ERROR finding per committed use case that the
// System draft leaves without a dynamic view, for the KindSystem artifact ONLY (nil
// for every other kind, for a nil/absent draft, and when no CoreUseCases is committed
// yet). The founder extension (2026-07-05) requires EVERY use case — core AND nonCore
// variation — to carry a call chain in the architecture, going beyond Löwy who
// validates only the core (that core subset is the twin ARCH-CHAINCOV / methodcheck
// rule). This is the read-back surface at the human review panel; the authoritative
// gate is methodcheck's USECASE-DYNAMIC-MISSING, which putDraftModel enforces while
// the agent authors.
func useCaseDynamicFindings(kind ArtifactKind, draft projectstate.ArtifactModel, committed *projectstate.CoreUseCases) []Finding {
	if kind != KindSystem || committed == nil {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	covered := make(map[projectstate.UseCaseID]bool, len(sys.DynamicViews))
	for _, dv := range sys.DynamicViews {
		covered[projectstate.UseCaseID(dv.UseCaseID)] = true
	}
	var out []Finding
	for i, d := range committed.Decisions {
		uc := d.UseCase
		if covered[uc.ID] {
			continue
		}
		label := uc.Name
		if label == "" {
			label = fmt.Sprintf("use case %d", i+1)
		}
		kindWord := "use case"
		if uc.Classification != projectstate.ClassCore {
			kindWord = "nonCore use-case variation"
		}
		out = append(out, Finding{
			RuleID:   "USECASE-DYNAMIC-MISSING",
			Severity: SeverityError,
			Message:  fmt.Sprintf("Use case %q has no dynamic view in the System; every %s (core AND nonCore variation) must carry its own call chain.", label, kindWord),
			Location: &Location{Ordinal: int64(i), Section: "use case " + label},
		})
	}
	return out
}

// activityDefect classifies why a use case's activity diagram fails the founder's
// non-empty floor, or "" when it is acceptable (present, with a start node AND at
// least one action node). It deliberately does NOT re-validate full UML
// well-formedness (decision/merge, fork/join, guards) — that is the Action's CI
// check; this only enforces "the diagram exists and carries the minimum
// meaningful nodes".
func activityDefect(a *projectstate.ActivityDiagram) string {
	if a == nil {
		return "has no activity diagram (activity is null)"
	}
	if len(a.Nodes) == 0 {
		return "has an empty activity diagram (no nodes)"
	}
	var hasStart, hasAction bool
	for _, n := range a.Nodes {
		// Only the start + action node kinds matter to the founder's floor; every other
		// node kind is irrelevant here (plain comparisons, not a switch, so the exhaustive
		// linter is not drawn into the full ActivityNodeKind set).
		if n.Kind == projectstate.NodeStart {
			hasStart = true
		}
		if n.Kind == projectstate.NodeAction {
			hasAction = true
		}
	}
	switch {
	case !hasStart && !hasAction:
		return "has an activity diagram with no start node and no action step"
	case !hasStart:
		return "has an activity diagram with no start node"
	case !hasAction:
		return "has an activity diagram with no action step"
	}
	return ""
}

// systemLayerDegenerateFindings returns ERROR findings for a layer-DEGENERATE System
// draft, for the KindSystem artifact ONLY (nil for every other kind and for a nil/absent
// draft). It is the app-side review-panel twin of methodcheck's SYSTEM-LAYER-DEGENERATE.
// Two independent degeneracy signals (F81):
//
//  1. STRUCTURE: a Method system decomposes into at least one Manager (the workflow
//     encapsulation) AND at least one ResourceAccess (the resource encapsulation). A
//     system with zero of either is degenerate — the classic all-client corruption
//     (every component's layer omitted → defaulted to client) has zero of both.
//  2. NAME↔LAYER: a component whose NAME carries a Method stereotype suffix must sit in
//     the matching layer ("…Manager"→manager, "…Engine"→engine, "…Access"→resourceAccess,
//     "…Client"→client, "…Store"/"…Resource"→resource). A name/layer contradiction is the
//     fingerprint of a defaulted layer (e.g. "OrderManager" carrying layer=client).
func systemLayerDegenerateFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	var out []Finding
	var managers, resourceAccess int
	for _, c := range sys.Components {
		switch c.Kind {
		case projectstate.CompManager:
			managers++
		case projectstate.CompResourceAccess:
			resourceAccess++
		case projectstate.CompClient, projectstate.CompEngine, projectstate.CompResource, projectstate.CompUtility:
			// Not counted — only Managers and ResourceAccess gate the degenerate-layer check.
		}
	}
	if managers == 0 {
		out = append(out, Finding{
			RuleID:   "SYSTEM-LAYER-DEGENERATE",
			Severity: SeverityError,
			Message:  "the System has zero Managers; a Method system must encapsulate at least one workflow in a Manager (an all-client architecture is the F81 corruption where every component's layer was omitted and defaulted to \"client\")",
			Location: &Location{Section: "system layers"},
		})
	}
	if resourceAccess == 0 {
		out = append(out, Finding{
			RuleID:   "SYSTEM-LAYER-DEGENERATE",
			Severity: SeverityError,
			Message:  "the System has zero ResourceAccess components; a Method system must encapsulate at least one resource behind a ResourceAccess (an all-client architecture is the F81 corruption where every component's layer was omitted and defaulted to \"client\")",
			Location: &Location{Section: "system layers"},
		})
	}
	for i, c := range sys.Components {
		if want, suffix, mismatch := nameLayerMismatch(c.Name, c.Layer); mismatch {
			label := c.Name
			if label == "" {
				label = fmt.Sprintf("component %d", i+1)
			}
			out = append(out, Finding{
				RuleID:   "SYSTEM-LAYER-DEGENERATE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("component %q ends in %q but declares layer %q instead of %q; a component's name stereotype and its layer must agree (a mismatch is the fingerprint of an omitted, defaulted layer)", label, suffix, layerWire(c.Layer), layerWire(want)),
				Location: &Location{Ordinal: int64(i), Section: "component " + label},
			})
		}
	}
	return out
}

// nameLayerMismatch reports whether a component NAME's Method stereotype suffix
// contradicts its declared layer. Returns the layer the name IMPLIES, the matched
// suffix, and whether there is a mismatch. A name with no recognized suffix never
// mismatches.
func nameLayerMismatch(name string, layer projectstate.Layer) (projectstate.Layer, string, bool) {
	type rule struct {
		suffix string
		want   projectstate.Layer
	}
	// Order matters: "…Resource" and "…Store" both imply resource; check specific suffixes.
	rules := []rule{
		{"Manager", projectstate.LayerManager},
		{"Engine", projectstate.LayerEngine},
		{"Access", projectstate.LayerResourceAccess},
		{"Client", projectstate.LayerClient},
		{"Store", projectstate.LayerResource},
		{"Resource", projectstate.LayerResource},
	}
	trimmed := strings.TrimSpace(name)
	for _, r := range rules {
		if strings.HasSuffix(trimmed, r.suffix) {
			if layer != r.want {
				return r.want, r.suffix, true
			}
			return r.want, r.suffix, false
		}
	}
	return layer, "", false
}

// layerWire renders a Layer as its wire name for a finding message.
func layerWire(l projectstate.Layer) string {
	b, err := l.MarshalJSON()
	if err != nil {
		return fmt.Sprintf("layer(%d)", int(l))
	}
	return strings.Trim(string(b), `"`)
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
func (wf *workflows) awaitDraftFailedRecovery(
	ctx workflow.Context,
	projectID ProjectID,
	kind ArtifactKind,
	headVersion projectstate.Version,
	reason string,
	runURL string,
	state *coAuthorState,
	feedback *ReviewFeedback,
) (coAuthorOutcome, bool, error) {
	// Surface the human-visible failed stage + the human reason (+ optional failed-run
	// URL) for the Query.
	state.stage = StageDraftFailed
	state.failureReason = reason
	state.failureRunURL = runURL

	redraftCh := workflow.GetSignalChannel(ctx, lSignalRedraft)
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
				// F47: MERGE the request feedback (from RequestArtifactDraft) with any gate-
				// retained feedback — the request WINS/appends — so the operator's new
				// instruction reaches the next draft prompt without discarding retained context.
				*feedback = mergeRedraftFeedback(*feedback, *sig.Feedback)
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
				*feedback = reviewFeedbackOrZero(sig.Feedback)
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
			state.failureRunURL = ""
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

// draftFailedReason renders the human "why" for the StageDraftFailed screen from
// the job's neutral Diagnostic. It is infrastructure-neutral (the Diagnostic is
// already a summary, not a log firehose — constructionPipelineAccess.md Non-goal #4).
func draftFailedReason(diagnostic string) string {
	if diagnostic == "" {
		return "the design job failed in CI — retry or withdraw"
	}
	return "the design job failed in CI: " + diagnostic + " — retry or withdraw"
}

// dispatchFailedReason renders the human "why" for the StageDraftFailed screen when the
// DISPATCH itself failed terminally (the job never ran — e.g. GitHub rejected the
// workflow_dispatch). It frames it distinctly from a ran-but-failed job (no CI run to
// point at) and folds in a neutral summary of the terminal error.
func dispatchFailedReason(err error) string {
	summary := dispatchErrSummary(err)
	if summary == "" {
		return "the design job could not be started in your repository — retry or withdraw"
	}
	return "the design job could not be started in your repository: " + summary + " — retry or withdraw"
}

// readBackDecodeFailedReason renders the human "why" for the StageDraftFailed screen when
// the committed draft READS BACK MALFORMED (QA F36): the CI validate went GREEN (its Go
// mirror types the offending enum as a free string) but the server codec rejects the value
// on read-back (a closed-enum field carrying free prose). It frames it distinctly from a
// CI failure and carries the decode diagnostic so a Retry redrafts with full visibility.
func readBackDecodeFailedReason(decodeMsg string) string {
	if strings.TrimSpace(decodeMsg) == "" {
		return "the committed draft could not be read back — its typed shape is invalid — retry or withdraw"
	}
	return "the committed draft could not be read back — its typed shape is invalid: " + decodeMsg + " — retry or withdraw"
}

// amendmentNoChangeReason renders the human "why" for the StageDraftFailed screen when
// an amendment session's draft committed nothing that changed the artifact — the branch
// read-back is byte-identical to the committed main model, so there is no advancement to
// open a PR on (opening one would 422 "no commits between base and head"). A Retry
// re-runs the amendment; a Withdraw abandons it.
func amendmentNoChangeReason() string {
	return "the amendment draft committed no changes to the artifact — there is nothing to review or merge — retry or withdraw"
}

// mergeRedraftFeedback merges the request feedback (from a RequestArtifactDraft redraft signal)
// with any gate-retained feedback (F47). The request WINS: its Notes are APPENDED after the
// retained Notes (newest instruction present and last, earlier context kept), and its anchored
// Comments are unioned. Empty request Notes keep the retained; empty retained takes the request.
func mergeRedraftFeedback(retained, req ReviewFeedback) ReviewFeedback {
	out := retained
	if reqNotes := strings.TrimSpace(req.Notes); reqNotes != "" {
		if strings.TrimSpace(out.Notes) == "" {
			out.Notes = reqNotes
		} else {
			out.Notes = strings.TrimSpace(out.Notes) + "\n\n" + reqNotes
		}
	}
	out.Comments = append(out.Comments, req.Comments...)
	return out
}

// railStepFailedReason renders the human "why" for the StageDraftFailed screen when a rail
// step in the draft round-trip (OpenBranch or OpenPullRequest) faulted AFTER the shared
// bounded workflow-side Auth retry exhausted (QA F35 twin) — a genuine permission denial or a
// persistent GitHub secondary-rate-limit 403. `what` names the step ("preparing the review
// branch" / "opening the review pull request"). For an openPR fault the draft is preserved and
// a Retry resumes from read-back (no re-dispatch); a Retry after an OpenBranch fault
// re-dispatches. Both are Retry/Withdraw from the same gate.
func railStepFailedReason(what string, err error) string {
	summary := dispatchErrSummary(err)
	if summary == "" {
		return what + " failed (a GitHub auth or rate-limit fault) — retry or withdraw"
	}
	return what + " failed (a GitHub auth or rate-limit fault): " + summary + " — retry or withdraw"
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

// dispatchErrSummary extracts a neutral, bounded summary from a terminal dispatch error.
// A Temporal ApplicationError (the wrapped RA fault, e.g. ContractMisuse from a rejected
// dispatch) carries a human Message(); otherwise the error string is used. Deterministic
// across replay — the error is reconstructed identically from workflow history.
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
