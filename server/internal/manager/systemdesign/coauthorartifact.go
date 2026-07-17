package systemdesign

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// maxRedraftAttempts bounds the PM-revise / draft-failure redraft loop before the
// workflow stages best-effort for the human gate (core-use-cases.md §1a alt-path).
// A pure in-workflow guard; not a contract surface.
const maxRedraftAttempts = 5

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

// readProject runs the generated designSessionAccess.readProjectOnBranch invoker (branch
// "" ⇒ main) and returns the whole head-state aggregate. A brand-new project surfaces
// fwra.NotFound (see isReadNotFound).
// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
	pe, err := wf.Acts.DesignSessionReadProjectOnBranch(ctx, projectstate.ProjectID(projectID), "")
	if err != nil {
		return projectstate.Project{}, err
	}
	return pe.Decode()
}

// readVersion runs the cheap ReadProjectVersion Activity and returns only the
// head-state optimistic-concurrency token — the single value the Conflict re-read
// loop needs to seed its next attempt. A brand-new project surfaces fwra.NotFound
// (see isReadNotFound), identical to readProject's absence semantics. Replaces the
// wasteful whole-aggregate read that shipped the entire encoded Project across the
// Temporal Activity boundary for a uint64.
// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) readVersion(ctx workflow.Context, projectID ProjectID) (projectstate.Version, error) {
	return wf.Acts.ProjectStateReadProjectVersion(ctx, projectstate.ProjectID(projectID))
}

// readVersionOnBranch returns the optimistic-concurrency token of the substrate the
// mutation targets (I-DESIGN-DISPATCH §2a). A branch mutation (stage / reject during the
// AwaitingReview window) advances the SESSION BRANCH, so its Conflict re-read must read
// THAT branch — not main, whose version trails. branch=="" reads main exactly as before.
// This is the fix for QA F29: a Conflict on a branch mutation that re-read main could
// never converge (main's version never catches up to the branch's), wedging the bounded
// loop into a non-retryable MutateConflictExhausted crash.
// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
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
// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
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
	// An AMENDMENT session's reopening feedback is OWNED by the amendment seed path
	// (maybeSeedAmendment, below) — it lands in the ledger at round 0 right after the first
	// stage. Mark it seeded up front so the pre-dispatch failed-gate seed does not race that
	// path and double-seed the same comments on the first draft. A non-amendment session's
	// initial feedback (the OQ6 re-request) is NOT ledger-backed, so it stays false and is
	// seeded before its first dispatch like any other memory-only feedback.
	if in.Amendment > 0 {
		state.feedbackSeeded = true
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
		gf, draft, readBackVersion, step := wf.produceReviewableDraft(ctx, in, proj, &feedback, &redraftCount, &reviewRound, headVersion, state)
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
		// STAGED TRACKING (2026-07-16 incident, gtdapp:1). The workflow — not the RA — knows
		// whether THIS session ever populated its slot. Record the substrate the stage landed
		// on so a later failed-gate Withdraw targets the SAME branch (the session branch under
		// the PR rail; "" == main when the rail is dormant) instead of blindly unstaging on
		// main, and a NEVER-staged session's Withdraw can skip the unstage write entirely
		// (an unpopulated-slot withdraw is a ContractMisuse that killed the whole rail).
		// Workflow-local state derived from recorded Activity results — replay-deterministic.
		state.staged = true
		state.stagedBranch = gf.readBackBranch()
		state.stage = StageAwaitingReview
		// SUB-STEP (Plan-3 C1): staged for the human gate — no role is working. Belt-and-braces
		// (the draft/critique success paths already cleared their stamp before returning).
		state.clearActive()
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
	reviewRound *int,
	headVersion projectstate.Version,
	state *coAuthorState,
) (gitSession, projectstate.ArtifactModel, projectstate.Version, coAuthorStep) {
	draft, gf, readBackVersion, step := wf.runDraftRoundTrip(ctx, in, proj, feedback, headVersion, redraftCount, reviewRound, state)
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
	reviewRound *int,
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
		m, v, step := wf.dispatchDraftAndReadBack(ctx, in, proj, gf, sessionBranch, feedback, headVersion, redraftCount, reviewRound, state)
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
	_ projectstate.Project,
	gf gitSession,
	sessionBranch string,
	feedback *ReviewFeedback,
	headVersion projectstate.Version,
	redraftCount *int,
	reviewRound *int,
	state *coAuthorState,
) (projectstate.ArtifactModel, projectstate.Version, coAuthorStep) {
	logger := workflow.GetLogger(ctx)
	// FAILED-GATE FEEDBACK SEED (thin-dispatch). The memory-only failed-gate recovery paths
	// (a redraft signal, a Retry-via-Reject at a failed gate, a faulted reject, a PM-critique
	// revise) retain the architect's feedback in the workflow's feedback variable ONLY — unlike
	// the review-gate reject and the amendment seed, which fold it into the DURABLE review
	// ledger. Under thin dispatch the drafting agent reads context ONLY via getReviewThread, so
	// that memory-only feedback would evaporate. Seed it here, right BEFORE the redraft dispatch,
	// reusing the SAME seeding activity + comment conversion the reject path uses, so the agent
	// reads it off the branch. state.feedbackSeeded gates it — an already-seeded reject/amendment
	// path is skipped so its comments are never double-seeded.
	//
	// Temporal versioning guard (replay safety; mirrors the managed-scaffold-sync gate in
	// beginSession): this seed was ADDED to the redraft dispatch path AFTER the CoAuthor workflow
	// first shipped, so a design session already in flight at deploy time has NO history event
	// for it — replaying such a history against unguarded new code fails the workflow task with a
	// non-determinism error. GetVersion pins pre-feature executions (DefaultVersion) to the OLD
	// command sequence (they skip the seed for their WHOLE run — including post-recovery redrafts,
	// the version resolved at first replay being cached per execution), while every execution
	// STARTED after this deploy resolves v1 and seeds before each memory-only redraft. The
	// founder's deploy drains in-flight design workflows first, so this gate is belt-and-braces.
	if workflow.GetVersion(ctx, "failed-gate-ledger-seed", workflow.DefaultVersion, 1) >= 1 {
		if !state.feedbackSeeded && wf.seedFailedGateFeedback(ctx, in, gf, headVersion, feedback, reviewRound, state) {
			state.feedbackSeeded = true
		}
	}
	// REVIEW LEDGER: on a redraft, the durable open comments (state.reviewThread, reloaded
	// after the reject-append or the failed-gate seed above) and the reopening feedback reach
	// the drafting agent via the ledger it reads with getReviewThread — no longer woven into a
	// design_prompt.
	//
	// SUB-STEP (Plan-3 C1): the architect is now drafting (round 0) or revising (round N>0)
	// on this session branch. Stamp it for the loading pill immediately BEFORE the dispatch;
	// it is cleared the instant the job is observed done (success or terminal fault, below).
	if *redraftCount == 0 {
		state.markActive(ActiveRoleArchitect, ActiveStepDrafting, *redraftCount)
	} else {
		state.markActive(ActiveRoleArchitect, ActiveStepRevising, *redraftCount)
	}
	draftObs, derr := wf.dispatchAndObserve(ctx, dispatchDesignJobArgs{
		ProjectID:     in.ProjectID,
		ArtifactKind:  in.ArtifactKind,
		Target:        dispatchTargetDraft,
		TargetBranch:  sessionBranch,
		PriorStateRef: "",
		// Per-project-design-dispatch: dispatch to the per-project repo + aiarch-design.yml
		// (the rail's repoRef). "" when the rail is dormant ⇒ RA falls back to construction.
		TargetRepo: gf.dispatchRepo(),
	}, state)
	if derr != nil {
		// The DISPATCH/observe round-trip itself FAILED terminally — e.g. GitHub 422s the
		// workflow_dispatch. Route it to the human-visible StageDraftFailed gate (never an
		// invisible crash; QA F15 gap 2a). A workflow-cancellation still propagates.
		logger.Warn("design draft dispatch failed terminally; entering StageDraftFailed", "error", derr.Error())
		state.clearActive()
		return nil, 0, wf.recoverDispatchFailed(ctx, in, headVersion, derr, state, feedback, redraftCount)
	}
	if draftObs.Phase != pipelineSucceeded {
		// The job RAN and FAILED (drafting failed or CI validation went red): land the session
		// in the human-visible StageDraftFailed and suspend on the gate (§0d.4 anti-wedge).
		logger.Warn("design draft job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", draftObs.Diagnostic)
		state.clearActive()
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
	// SUB-STEP (Plan-3 C1): the draft dispatch is observed complete — clear the in-flight
	// architect stamp. A PM-critiqued kind re-stamps it as PM-critiquing next (runPMCritique);
	// an architect-owned kind proceeds to staging, where the AwaitingReview clear is a no-op.
	state.clearActive()
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
	// SUB-STEP (Plan-3 C1): the PM is now critiquing the draft. Round is not a critique
	// concept (it counts architect redraft rounds), so it stays at its cleared 0.
	state.markActive(ActiveRoleProductManager, ActiveStepCritiquing, 0)
	critObs, cerr := wf.dispatchAndObserve(ctx, dispatchDesignJobArgs{
		ProjectID:     in.ProjectID,
		ArtifactKind:  in.ArtifactKind,
		Target:        dispatchTargetCritique,
		TargetBranch:  sessionBranch,
		PriorStateRef: "",
		// Per-project-design-dispatch: the critique job also runs in the per-project repo.
		TargetRepo: gf.dispatchRepo(),
	}, state)
	if cerr != nil {
		// The critique DISPATCH itself failed terminally — route to the human-visible
		// StageDraftFailed gate (same anti-wedge rule as the draft dispatch), never crash.
		// F-QA2-24: the DRAFT on the session branch is intact (it read back fine before
		// this round) — only the critique failed to start — so arm the critique-retry
		// resume before landing at the gate: a Retry re-runs the CRITIQUE, not a
		// feedbackless redraft. A cancellation is a teardown, not a retryable fault
		// (recoverDispatchFailed propagates it), so it is never armed.
		logger.Warn("PM-critique dispatch failed terminally; entering StageDraftFailed", "error", cerr.Error())
		if !temporal.IsCanceledError(cerr) {
			wf.armCritiqueRetry(ctx, state)
		}
		return wf.recoverDispatchFailed(ctx, in, headVersion, cerr, state, feedback, redraftCount)
	}
	if critObs.Phase != pipelineSucceeded {
		// A terminal PM-critique job failure routes to the same StageDraftFailed human
		// gate as a terminal draft failure — never crash the workflow. F-QA2-24: the
		// DRAFT is complete on the session branch, so the gate's Retry must RE-RUN THE
		// CRITIQUE against it — a feedbackless redraft finds no open comments and no
		// revise verdict, commits nothing, and the template's silent-failure guard reds
		// the run: a retry loop that can never converge (observed live on gtdapp, 2
		// consecutive occurrences). The gate copy names the critique ONLY when the retry
		// semantics actually re-run it (armCritiqueRetry's version pin keeps mid-history
		// executions on the old redraft-on-retry copy AND behavior together).
		logger.Warn("PM-critique job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", critObs.Diagnostic)
		reason := draftFailedReason(critObs.Diagnostic)
		if wf.armCritiqueRetry(ctx, state) {
			reason = critiqueFailedReason(critObs.Diagnostic)
		}
		return wf.recoverAtFailedGate(ctx, in, headVersion, reason, critObs.RunURL, state, feedback, redraftCount)
	}
	// Read the critique verdict back off the SAME session branch it was committed to.
	critique, crbErr := wf.readBackCritiqueOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch())
	if crbErr != nil {
		if isCritiqueReadBackEmpty(crbErr) {
			// A critique job that reported success but committed NO verdict is a
			// ran-but-incomplete job — the missing-verdict safe default (dispatch.go).
			// Route it to the SAME human-visible StageDraftFailed gate as a terminal job
			// failure (NOT a silent approve, NOT a workflow crash — the anti-wedge rule),
			// awaiting human Retry-via-Reject / Withdraw. F-QA2-24: the draft is complete —
			// the CRITIQUE is what committed nothing — so the gate's Retry re-runs the
			// critique against the kept draft (armCritiqueRetry), never a feedbackless
			// redraft. The reason already names the critique on both pinned versions.
			logger.Warn("PM-critique read-back found no verdict (missing-verdict safe default); entering StageDraftFailed")
			wf.armCritiqueRetry(ctx, state)
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
	// F-QA2-7: stamp the PM's conclusion (verdict + rationale + the draft round it
	// judged) on the live session view the moment the workflow observes it, so the
	// human gate shows what the PM concluded — not just the machine validation.
	// Derived from the recorded read-back Activity result; no history command.
	state.critique = critiqueViewFor(critique, *redraftCount)
	if critique.Verdict == critiqueRevise {
		*redraftCount++
		if *redraftCount >= maxRedraftAttempts {
			// Do NOT crash the workflow (that wedges the SPA). The committed draft is
			// valid (it passed the CI check); stage it for the human gate with the
			// unresolved PM critique surfaced as a note so the architect makes the final
			// call instead of an oscillating critic killing the loop.
			logger.Warn("PM-critique did not converge within max attempts; staging for human review")
			state.unresolvedCritique = critique.Notes
			// SUB-STEP (Plan-3 C1): the critique loop is giving up and staging for human
			// review — clear the PM stamp so the query doesn't keep claiming PM-critiquing
			// after PM work has stopped.
			state.clearActive()
			return stepProceed() // fall through to stage for review.
		}
		// Re-dispatch the architect draft with the PM notes woven in. Memory-only feedback
		// (Notes carry no anchored comments, so the pre-dispatch seed is a no-op here, but the
		// flag stays honest for the general case).
		*feedback = ReviewFeedback{Notes: critique.Notes}
		state.feedbackSeeded = false
		state.stage = StageRedrafting
		// SUB-STEP (Plan-3 C1): the critique is observed done and asked for a revise. Clear the
		// PM stamp; the redraft loop re-stamps architect-revising (round N) before its dispatch.
		state.clearActive()
		return stepRedraft()
	}
	// SUB-STEP (Plan-3 C1): the critique is observed done and ratified — clear the PM stamp
	// before proceeding to staging (where the AwaitingReview clear is then a no-op).
	state.clearActive()
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
	branch := gf.readBackBranch()
	return wf.applyRecovering(ctx, in.ProjectID, branch, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionStageArtifactForReviewOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, branch, draftEnvelope)
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
	// F-QA2-41: a fresh decision at this gate supersedes any prior approve/withdraw-fault
	// notice — clear it so the NEXT stage (Committed on a successful re-approve,
	// Redrafting on a send-back) never carries the stale notice forward. A decision arm
	// that faults below re-stamps its own notice (reAwaitAfterApproveFault). Workflow-local
	// view state served by the query; setting it issues NO history command (the same
	// honesty invariant as runURL/activeRole), so no GetVersion gate is needed.
	state.failureReason = ""
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
		// Not YET in the ledger — the reject write below seeds it (and flips this true on
		// success). If that write FAULTS (crash containment, below), the flag stays false so
		// the failed-gate seed persists this feedback before the Retry redraft dispatch.
		state.feedbackSeeded = false
		branch := gf.readBackBranch()
		newVersion, err := wf.applyRecovering(ctx, in.ProjectID, branch, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			// REVIEW LEDGER (review-ledger §2): fold the reviewer's anchored comments into the
			// reject as durable, server-minted ledger entries, round-stamped by the per-reject
			// review-round counter (a distinct, replay-stable monotonic counter → deterministic,
			// non-colliding ids on the ONE accumulating thread — F40). Empty ⇒ a plain reject.
			//
			// Branch-aware Reject (I-DESIGN-DISPATCH §2a): record the Rejected status on the
			// SESSION BRANCH the draft was staged on — where the staged model exists and the
			// session-branch version (headVersion) matches. In the PR rail main is untouched
			// until an approved draft merges, so a main-path reject would mismatch the version
			// AND find the slot unpopulated (the QA F28 crash). "" when the rail is dormant ⇒
			// the reject lands on main exactly as before.
			return wf.Acts.DesignSessionRejectArtifactOnBranchWithComments(ctx, projectstate.ProjectID(in.ProjectID), expected, branch,
				toPSKind(in.ArtifactKind), rejectFeedback.Notes, int64(*reviewRound), feedbackToLedgerComments(rejectFeedback))
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
		// The reject folded the architect's comments into the ledger (feedbackToLedgerComments,
		// above), so this feedback is durably seeded — the pre-dispatch failed-gate seed skips it
		// (no double-seed).
		state.feedbackSeeded = true
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
		// F-QA2-7: the surfaced PM conclusion judged the draft the human just REJECTED —
		// clear it so the view never attributes a stale verdict to the upcoming redraft.
		// The redraft's own critique round re-stamps it before the next gate.
		state.critique = nil
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
		withdrawBranch := gf.readBackBranch()
		if _, err := wf.applyRecovering(ctx, in.ProjectID, withdrawBranch, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			return wf.Acts.DesignSessionWithdrawArtifactOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, withdrawBranch, toPSKind(in.ArtifactKind), notes)
		}); err != nil {
			// ANTI-WEDGE (2026-07-16 incident twin). A fault while RECORDING the Withdraw must
			// NOT terminate the workflow (a terminal error here killed the CoAuthor spine AND
			// its parent phase rail). The staged draft is intact on its branch, so mirror the
			// QA F35 approve-fault containment: return to AwaitingReview carrying an honest
			// queryable notice so the human simply withdraws (or decides) again. Only a
			// workflow-cancellation (teardown) still propagates.
			if temporal.IsCanceledError(err) {
				return stepErr(err)
			}
			workflow.GetLogger(ctx).Warn("withdraw write faulted; returning to AwaitingReview for another decision", "error", err.Error())
			return wf.reAwaitAfterApproveFault(state, withdrawFailedReason(err))
		}
		state.stage = StageWithdrawn
		state.clearActive() // SUB-STEP (Plan-3 C1): terminal — no role is working.
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
		// PM-P2-4 commit provenance: the approving identity + the drafting rail identity.
		return wf.Acts.DesignSessionCommitArtifactWithProvenance(ctx, projectstate.ProjectID(in.ProjectID), expected, toPSKind(in.ArtifactKind), approver, railDraftedBy(in.Amendment))
	}); err != nil {
		// QA F35: contain a post-merge commit fault too (same idempotent re-approve recovery).
		if temporal.IsCanceledError(err) {
			return stepErr(err)
		}
		logger.Warn("approve post-merge commit fault; returning to AwaitingReview for re-approve", "error", err.Error())
		return wf.reAwaitAfterApproveFault(state, approveFailedReason(err))
	}
	state.stage = StageCommitted
	state.clearActive() // SUB-STEP (Plan-3 C1): terminal — no role is working.
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
	// SUB-STEP (Plan-3 C1): back at the human gate for a re-approve — no role is working.
	state.clearActive()
	return stepReAwait()
}

// approveFailedReason renders the human "why" for the AwaitingReview re-approve notice when
// an approve/merge-window activity faulted transiently (QA F35 — e.g. a GitHub secondary
// rate-limit 403 the platform classifier reports as Auth). It frames a re-approve, NOT a
// redraft. Wording is founder-ratified (F-QA2-41): lead with what failed, state that the
// draft is unchanged, and suggest waiting out a rate limit. The live 403 case gets the
// ratified copy verbatim; other faults keep their honest summary in the same frame.
// Deterministic across replay (pure string ops on the history-reconstructed error).
func approveFailedReason(err error) string {
	summary := dispatchErrSummary(err)
	if strings.Contains(summary, "403") {
		return "The approve could not complete: GitHub rejected the merge step (403 — often a rate limit). The draft is unchanged; try approving again in a few minutes."
	}
	if summary == "" {
		return "The approve could not complete (a transient repository/API fault). The draft is unchanged; try approving again in a few minutes."
	}
	return "The approve could not complete: " + summary + ". The draft is unchanged; try approving again in a few minutes."
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
	// runURL is the LIVE dispatched design job's run URL while a dispatch → observe
	// round-trip is in flight (the Drafting/Redrafting stages): resolved from the run's
	// observations, so the SPA's GENERATING scene can deep-link the operator to the
	// actual GitHub Actions run instead of an unlinked "the job is running in your CI"
	// notice (QA F-GTD-6). Owned entirely by dispatchAndObserve — reset on each fresh
	// dispatch, stamped per observation, cleared on the terminal observation. Empty
	// whenever no run is in flight (or the RA could not resolve the URL — never
	// fabricated). Workflow-local state served by view(); setting it issues NO Temporal
	// history command (the same honesty invariant as activeRole/activeStep).
	runURL string
	// unresolvedCritique, when non-empty, is the PM critique note that did not
	// converge within maxRedraftAttempts; surfaced at the human gate as a WARNING
	// finding so the architect makes the final call (warnings don't block Approve).
	unresolvedCritique string
	// critique is the LAST PM-critique conclusion the workflow observed (verdict +
	// the PM's rationale + the draft round it judged), surfaced on the session view so
	// the founder never approves a PM-reviewed artifact blind to what the PM concluded
	// (F-QA2-7). Stamped on every successful critique read-back — an APPROVE shows the
	// ratification (with any approve-with-reservation notes), a REVISE stays visible
	// through the automatic redraft it triggered (the "why is it redrafting" honesty)
	// and through the non-convergence best-effort stage. Cleared on a human REJECT
	// (that critique judged the now-rejected draft; the redraft's own critique
	// re-stamps it). Nil for kinds with no PM critic and until the first critique
	// completes. Workflow-local state served by view(); setting it issues NO Temporal
	// history command (the same honesty invariant as runURL/activeRole), so no
	// GetVersion gate is needed and mid-history executions replay unchanged.
	critique *CritiqueView
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
	// resumeFromReadBack is the F35-twin checkpoint: set true when a POST-read-back step
	// faulted and the session landed at the failed gate WITH the draft already committed on
	// the branch — an openPR rail fault (F35 twin), or ANY critique-round fault (F-QA2-24,
	// version-gated in armCritiqueRetry: terminal critique job failure, rejected critique
	// dispatch, missing verdict). On the next Retry the draft round-trip consumes it and
	// RESUMES from the read-back — SKIPPING the draft re-dispatch — so it does not
	// redispatch Claude onto a branch that already carries the model (which the no-commit
	// guard would red); for a PM-critiqued kind the spine then falls through to
	// runPMCritique, re-running the critique against the kept draft. Workflow-local,
	// deterministic on replay (set from recorded Activity results, never wall-clock).
	resumeFromReadBack bool
	// staged / stagedBranch record whether THIS session ever staged its draft into the
	// slot, and on which substrate ("" == main; the session branch under the PR rail) —
	// set on every successful stageDraftForReview (2026-07-16 incident). The failed-gate
	// Withdraw consults them: a NEVER-staged session skips the unstage write entirely
	// (an unpopulated-slot withdraw is a non-retryable ContractMisuse that terminated
	// the workflow and its parent phase rail), and a staged session's withdraw targets
	// the branch the stage actually landed on (never a blind main write). Workflow-local
	// state derived from recorded Activity results — deterministic on replay; the
	// command-sequence change it gates is version-pinned (failed-gate-withdraw-honest).
	staged       bool
	stagedBranch string
	// feedbackSeeded reports whether the CURRENT contents of the workflow's feedback variable
	// are already durably in the review ledger. The review-gate REJECT and the AMENDMENT seed
	// fold their feedback into the ledger themselves (feedbackToLedgerComments / seedAmendment
	// Ledger), so they set this true. The MEMORY-ONLY failed-gate paths — a redraft-signal
	// (F47), a Retry-via-Reject AT a failed gate, a faulted reject, a PM-critique revise — only
	// retain the feedback in this workflow variable, so they set it false. Under thin dispatch
	// the drafting agent reads context ONLY via getReviewThread, so before each redraft dispatch
	// a false flag triggers seedFailedGateFeedback (below) to seed the retained feedback,
	// while a true flag skips it so an already-seeded path is never double-seeded.
	feedbackSeeded bool
	// activeRole / activeStep / activeRound are the WORKFLOW-LOCAL sub-step indicator
	// backing the honest role-driven loading pill (Plan-3 C1). They are SET immediately
	// before each dispatch boundary (architect drafting/revising; PM critiquing) and
	// CLEARED to none/none/0 the instant that dispatch is observed complete or the session
	// reaches any terminal / AwaitingReview stage. Pure in-workflow state served by view()
	// (NOT boundary-stamped like StageName) — setting it issues NO Temporal history
	// command, so no GetVersion gate is needed (the honesty invariant).
	activeRole  ActiveRole
	activeStep  ActiveStep
	activeRound int
}

// markActive stamps the in-flight sub-step (role / step / round) the loading pill renders.
// Pure workflow-local state; no history command.
func (s *coAuthorState) markActive(role ActiveRole, step ActiveStep, round int) {
	s.activeRole = role
	s.activeStep = step
	s.activeRound = round
}

// clearActive resets the sub-step to none/none/0 — the honest "no role is working" state
// the pill falls back to today's plain "DRAFTING…" copy for. Called on observed dispatch
// completion and on every terminal / AwaitingReview stage.
func (s *coAuthorState) clearActive() {
	s.activeRole = ActiveRoleNone
	s.activeStep = ActiveStepNone
	s.activeRound = 0
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
		RunURL:        strPtrOrNil(s.runURL),
		Critique:      s.critique,
		ReviewThread:  reviewThreadToView(s.reviewThread),
		ActiveRole:    s.activeRole,
		ActiveStep:    s.activeStep,
		Round:         int64(s.activeRound),
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
	// SUB-STEP (Plan-3 C1): the failed-gate sink for EVERY draft/critique/stage/approve
	// fault — no role is working while the human decides Retry/Withdraw. Belt-and-braces
	// over the per-site clears at the dispatch failure returns.
	state.clearActive()

	redraftCh := workflow.GetSignalChannel(ctx, lSignalRedraft)
	reviewCh := workflow.GetSignalChannel(ctx, signalReviewDecision)

	// STALE-SIGNAL DRAIN (QA incident 2026-07-15, gtdapp:1 — gate hygiene). Any redraft
	// signal ALREADY buffered when this gate opens was sent BEFORE the failure was human-
	// visible (the query could not have reported DraftFailed yet: state.stage flips above,
	// in the same workflow task this drain runs in), so it cannot be an informed Retry.
	// Two senders produce such signals: (a) RequestArtifactDraft's SignalWithStart START
	// path — every user-initiated first draft rides in with one redraft signal the drafting
	// spine never consumes; (b) a "Request draft" click landing while the session was
	// drafting (now ALSO refused at the manager — checkDraftRequestReceptive — but raw
	// signals and anything already buffered remain). Letting the selector consume one would
	// auto-satisfy Retry the instant the gate arms, skipping the human decision (observed
	// live: a queued click auto-redrafted over a PM-critique failure nobody ever saw).
	// Discarding is deterministic: buffered signals are part of workflow history, and
	// ReceiveAsync consumes them identically on replay. Any feedback such a signal carried
	// is not lost where it matters — the start-path signal's feedback also rides
	// coAuthorInput.Feedback.
	//
	// Temporal versioning guard (replay safety; mirrors the failed-gate-ledger-seed gate):
	// executions in flight at deploy time have histories in which a buffered redraft DID
	// satisfy this gate immediately. GetVersion pins them (DefaultVersion, cached per
	// execution at first replay) to the old arm-immediately sequence, while every execution
	// STARTED after this deploy resolves v1 and drains at each failed-gate entry.
	if workflow.GetVersion(ctx, "failed-gate-redraft-drain", workflow.DefaultVersion, 1) >= 1 {
		drained := 0
		for {
			var stale redraftSignal
			if !redraftCh.ReceiveAsync(&stale) {
				break
			}
			drained++
		}
		if drained > 0 {
			workflow.GetLogger(ctx).Info("discarded buffered redraft signal(s) at StageDraftFailed gate entry — sent before the failure was visible, so they cannot auto-consume the human gate",
				"count", drained)
		}
	}

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
			// Memory-only until the pre-dispatch failed-gate seed persists it (thin dispatch).
			state.feedbackSeeded = false
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
				// Retry-via-Reject: re-dispatch with the architect's feedback woven in. This is
				// the CORE gap this fix closes — at a FAILED gate (unlike the review gate) the
				// reject never touches the ledger, so the feedback is memory-only until the
				// pre-dispatch failed-gate seed persists it before the redraft dispatch.
				*feedback = reviewFeedbackOrZero(sig.Feedback)
				state.feedbackSeeded = false
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
			done, werr := wf.withdrawAtFailedGate(ctx, projectID, kind, headVersion, withdrawNotes, state)
			if werr != nil {
				return coAuthorUnknown, false, werr
			}
			if done {
				return coAuthorWithdrawn, false, nil
			}
			// The withdraw write faulted and was CONTAINED (Fix 2 anti-wedge): the gate is
			// re-armed with the honest withdraw-failed reason — stay suspended for another
			// human decision.
			continue
		}
		// A non-actionable review decision at the failed gate: stay suspended.
	}
}

// withdrawAtFailedGate performs the failed-gate Withdraw (2026-07-16 incident, gtdapp:1 +
// its parent phase rail killed). The old path was a blind MAIN write with two crash modes:
//
//  1. NEVER-STAGED session (the incident): no slot is populated ANYWHERE — the unstage
//     write raises non-retryable ContractMisuse ("slot X is unpopulated"), which terminated
//     this workflow AND the parent phase workflow. The workflow KNOWS it never staged
//     (state.staged) — there is nothing durable to flip, so the correct withdraw simply
//     ends the session as withdrawn with NO write.
//  2. STAGED-ON-BRANCH session (reject-fault / not-green gates): the slot lives on the
//     SESSION BRANCH, not main — the main write was the same unpopulated-slot crash (QA
//     F30's failed-gate twin). Target the branch the stage landed on (state.stagedBranch;
//     "" == main when the rail is dormant).
//
// Temporal versioning guard (replay safety; mirrors failed-gate-redraft-drain): GetVersion
// pins executions whose history already recorded the old main-write (DefaultVersion — e.g.
// a query replay of a dead run) to the old command sequence, while every fresh decision
// resolves v1. A LIVE suspended session receiving its first withdraw post-deploy executes
// fresh code (no recorded commands here yet), so it gets the fix too.
//
// Returns (done, err): done=true → the session is withdrawn (the caller returns the
// terminal outcome); done=false with nil err → the write FAULTED and was CONTAINED (Fix 2
// anti-wedge — the gate is re-armed carrying withdrawFailedReason; NO recovery-path error
// may terminate the workflow); a non-nil err is ONLY a workflow-cancellation (teardown).
func (wf *workflows) withdrawAtFailedGate(
	ctx workflow.Context,
	projectID ProjectID,
	kind ArtifactKind,
	headVersion projectstate.Version,
	notes string,
	state *coAuthorState,
) (bool, error) {
	v := workflow.GetVersion(ctx, "failed-gate-withdraw-honest", workflow.DefaultVersion, 1)
	if v >= 1 && !state.staged {
		workflow.GetLogger(ctx).Info("withdraw at the failed gate with nothing ever staged; skipping the unstage write and ending withdrawn")
		state.stage = StageWithdrawn
		state.clearActive()
		return true, nil
	}
	withdrawBranch := ""
	if v >= 1 {
		withdrawBranch = state.stagedBranch
	}
	if _, err := wf.applyRecovering(ctx, projectID, withdrawBranch, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionWithdrawArtifactOnBranch(ctx, projectstate.ProjectID(projectID), expected, withdrawBranch, toPSKind(kind), notes)
	}); err != nil {
		if temporal.IsCanceledError(err) {
			return false, err
		}
		workflow.GetLogger(ctx).Warn("failed-gate withdraw write faulted; staying at the failed gate", "error", err.Error())
		state.failureReason = withdrawFailedReason(err)
		state.failureRunURL = ""
		return false, nil
	}
	state.stage = StageWithdrawn
	state.clearActive()
	return true, nil
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

// critiqueFailedReason renders the human "why" for the StageDraftFailed screen when the
// PM-CRITIQUE job (not the draft) reached a terminal failure phase (F-QA2-24). The draft
// is intact on the session branch, so the copy names the critique and frames Retry as
// re-running it — never the generic "the design job failed in CI", which reads as a draft
// failure and misleads the operator about what a Retry does. Used ONLY when
// armCritiqueRetry armed the critique-retry resume (v1 semantics) so copy and behavior
// stay honest together on every pinned version.
func critiqueFailedReason(diagnostic string) string {
	if diagnostic == "" {
		return "the PM-critique job failed in CI — your draft is kept; retry re-runs the critique, or withdraw"
	}
	return "the PM-critique job failed in CI: " + diagnostic + " — your draft is kept; retry re-runs the critique, or withdraw"
}

// armCritiqueRetry checkpoints the F-QA2-24 critique-retry resume: the DRAFT is already
// committed (and read back) on the session branch — only the PM-CRITIQUE round failed
// (terminal job failure, rejected dispatch, or a success that committed no verdict) — so
// the StageDraftFailed gate's Retry must resume from the draft read-back and re-dispatch
// the CRITIQUE, not a redraft. A feedbackless redraft against an already-complete draft
// finds no open comments and no revise verdict, does no commit, and the template's
// silent-failure guard reds the run — a retry loop that can never converge (observed live
// on gtdapp: 2 consecutive occurrences). Setting resumeFromReadBack reuses the F35-twin
// resume path: the retry probes the read-back, SKIPS the draft dispatch, and
// produceReviewableDraft falls through to runPMCritique — the full dispatch → observe →
// read-back → verdict routing, including the F-QA2-7 critique-view stamp.
//
// Temporal versioning guard (replay safety; mirrors the failed-gate-redraft-drain gate):
// executions in flight at deploy time (gtdapp:1 is suspended at the glossary failed gate
// with critique-fail → Retry → DRAFT-dispatch rounds already RECORDED) have histories in
// which the retry scheduled a draft dispatch; replaying them against un-gated new code
// would schedule the resume read-back where history recorded a dispatch — a
// non-determinism failure. GetVersion pins pre-feature executions (DefaultVersion,
// resolved at first replay and cached per execution) to the OLD redraft-on-retry sequence
// for their WHOLE run, while every execution started after this deploy resolves v1 and
// resumes the critique. Returns whether the resume was armed so the caller keeps the gate
// copy honest per pinned version (never promising a critique re-run a pinned execution
// will not perform).
func (wf *workflows) armCritiqueRetry(ctx workflow.Context, state *coAuthorState) bool {
	if workflow.GetVersion(ctx, "failed-gate-critique-retry", workflow.DefaultVersion, 1) < 1 {
		return false
	}
	state.resumeFromReadBack = true
	return true
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

// withdrawFailedReason renders the human "why" when the write RECORDING a Withdraw
// faulted (2026-07-16 anti-wedge): at the failed gate it re-arms the SAME gate with this
// reason; at the review gate it rides the AwaitingReview notice (reAwaitAfterApproveFault).
// Either way the session stays alive and the human simply retries or withdraws again —
// a recovery-path fault must never terminate the workflow.
func withdrawFailedReason(err error) string {
	summary := dispatchErrSummary(err)
	if summary == "" {
		return "withdraw failed — retry or withdraw again"
	}
	return "withdraw failed: " + summary + " — retry or withdraw again"
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

// dispatch.go is the AGENTIC-PIVOT seam (D-MSD-Δ, systemDesignManager.md §0d). The
// drafting MECHANISM flips from a synchronous workerAccess call to an ASYNC
// dispatch → observe → read-back round-trip:
//
//   - DISPATCH  the Manager selects the Method-role .claude command slug
//               (DesignCommandFor) and dispatches a claude-code-action DESIGN job via
//               the FROZEN constructionPipelineAccess.SubmitConstructionPipeline verb,
//               carrying {artifact_kind, command, target_branch, prior_state_ref,
//               job_mode} on the additive PipelineSpec.DispatchInputs field
//               (C-WF-DESIGN input schema). The doctrine lives in the command's
//               method-assets, not a composed prompt. The RA reserves + stamps
//               idempotency_token itself; the Manager MUST NOT set it.
//   - OBSERVE   the Manager polls ObserveConstructionPipeline(handle) between
//               durableExecutionAccess timer waits until a TYPED terminal phase.
//   - READ-BACK on PhaseSucceeded the Manager reads the committed typed Kind via
//               projectStateAccess.ReadProject (the Action committed the JSON;
//               aiarch writes nothing on the draft path).
//
// The claude-code-action job runs OUTSIDE aiarch's call graph (the user's CI, the
// user's token). aiarch only dispatches it, observes it, and reads back its
// committed output — closed layering preserved, no RA→RA edge, no new edge type.
//
// THE IDEMPOTENCY KEY IS DERIVED INSIDE THE DISPATCH ACTIVITY (construction note
// N1). Temporal assigns a distinct ActivityID per ExecuteActivity invocation and
// reuses it across automatic retries of that one invocation. So a REDRAFT loop
// (a fresh ExecuteActivity(DispatchDesignJobActivity)) gets a new ActivityID → a
// distinct key → a fresh, idempotent job (NOT a dedup of the stale prior job);
// a transient auto-retry of a single dispatch keeps the ActivityID → same key →
// the FROZEN submit verb collapses it to the same handle.

// designPipelinePhase maps the RA's phase to the manager's neutral phase, preserving
// the Cancelled terminal distinctly (the design Manager treats any non-Succeeded
// terminal as a StageDraftFailed gate).
func designPipelinePhase(p constructionpipeline.PipelinePhase) pipelinePhase {
	switch p {
	case constructionpipeline.PhasePending:
		return pipelinePending
	case constructionpipeline.PhaseRunning:
		return pipelineRunning
	case constructionpipeline.PhaseSucceeded:
		return pipelineSucceeded
	case constructionpipeline.PhaseFailed:
		return pipelineFailed
	case constructionpipeline.PhaseCancelled:
		return pipelineCancelled
	default:
		return lPipelinePhaseUnknown
	}
}

// pipelinePhase mirrors constructionPipelineAccess.md §3 — the infrastructure-
// neutral lifecycle phase the Manager branches on. The terminal trio drives the
// observe loop's exit + the failure path.
type pipelinePhase int

const (
	lPipelinePhaseUnknown pipelinePhase = iota
	pipelinePending
	pipelineRunning
	pipelineSucceeded
	pipelineFailed
	pipelineCancelled
)

// IsTerminal reports whether the phase is one the job can no longer leave.
func (p pipelinePhase) IsTerminal() bool {
	switch p {
	case pipelineSucceeded, pipelineFailed, pipelineCancelled:
		return true
	case lPipelinePhaseUnknown, pipelinePending, pipelineRunning:
		return false
	default:
		return false
	}
}

// pipelineObservation mirrors constructionPipelineAccess.md §3 — a point-in-time,
// infrastructure-neutral view carrying the phase and (on terminal failure) a
// neutral Diagnostic summary (NOT a log firehose).
type pipelineObservation struct {
	Phase      pipelinePhase
	Diagnostic string
	// RunURL is the CI run's URL on ANY observation the RA resolved it for: while the
	// run is live it is the generating view's "view the run" deep-link (F-GTD-6);
	// on a terminal failure it is the "why" pointer the Manager threads onto the
	// StageDraftFailed card (QA F15 gap 2b). Empty when the RA could not resolve it.
	RunURL string
}

// jobModeFor maps a DispatchTarget to its job_mode dispatch value.
func jobModeFor(target dispatchTarget) string {
	switch target {
	case dispatchTargetDraft:
		return jobModeDraft
	case dispatchTargetCritique:
		return jobModeCritique
	case dispatchTargetAnswer:
		return jobModeAnswer
	default:
		return jobModeDraft
	}
}

// designModeFor maps a dispatchTarget to the projectstate.DesignJobMode that
// DesignCommandFor consumes — the command-slug counterpart of jobModeFor. Draft
// and Critique are the only targets dispatchDesignJob ever carries (the Answer job
// dispatches directly from the Manager via dispatchAnswerJob); Answer is mapped for
// completeness so the switch stays total.
func designModeFor(target dispatchTarget) projectstate.DesignJobMode {
	switch target {
	case dispatchTargetDraft:
		return projectstate.DesignJobModeDraft
	case dispatchTargetCritique:
		return projectstate.DesignJobModeCritique
	case dispatchTargetAnswer:
		return projectstate.DesignJobModeAnswer
	default:
		return projectstate.DesignJobModeDraft
	}
}

// dispatchTarget discriminates which Method-role agentic job the dispatch round-
// trip produces: an architect/PM DRAFT of the artifact, or a PM CRITIQUE of the
// just-committed draft. Both are dispatch → observe → read-back round-trips; only
// the prompt role + the read-back differ.
type dispatchTarget int

const (
	dispatchTargetDraft    dispatchTarget = iota // draft the artifact named by ArtifactKind
	dispatchTargetCritique                       // PM-critique the just-committed draft
	dispatchTargetAnswer                         // answer open QUESTION ledger entries in place
)

// observePollInterval spaces the observe-poll loop's durable timer waits. A
// design job runs minutes in the user's CI; this is the in-workflow timer the
// contract prescribes (§0d.2 step 4). Kept modest so the test's time-skipping env
// settles quickly.
const observePollInterval = 15 * time.Second

// maxObservePolls bounds the observe loop so a stuck (never-terminal) job cannot
// spin forever; exceeding it is treated as a terminal infrastructure failure and
// routed to the human gate (never a perpetual Drafting — the anti-wedge rule).
const maxObservePolls = 240 // 240 * 15s = 1h ceiling

// dispatchDesignJobArgs bundles the dispatch inputs for the Activity boundary.
// ArtifactKind + Target select the .claude command slug (DesignCommandFor); Branch
// + PriorStateRef ride into the DispatchInputs map inside the Activity. The prompt
// prose is GONE — the doctrine lives in the method-assets .claude commands the design
// job runs; the Manager ships only the command name + the target metadata.
type dispatchDesignJobArgs struct {
	ProjectID     ProjectID
	ArtifactKind  ArtifactKind
	Target        dispatchTarget
	TargetBranch  string
	PriorStateRef string
	// TargetRepo is the opaque per-project RepoRef (gitSession.repoRef.String()) the
	// design job must dispatch to — the user's per-project repo where aiarch-design.yml
	// was committed at project birth (per-project-design-dispatch). Empty ⇒ the RA falls
	// back to the configured construction repo (the dormant-rail / non-git path).
	TargetRepo string
}

// dispatchDesignJob composes the constructionpipeline.PipelineSpec for one design job and
// submits it through the generated invoker, returning the opaque handle. The four DESIGN
// parameters (plus the job_mode discriminator) ride on DispatchInputs; a per-project
// TargetRepo (decoded from the opaque RepoRef) + WorkflowFile target the user's per-project
// repo + aiarch-design.yml, else an empty target falls back to the RA's configured
// construction repo. The idempotency key is stamped INSIDE the generated submit Activity
// (genActivityIdempotencyKey), so a redraft (fresh ExecuteActivity → new ActivityID) is a
// distinct job while a transient auto-retry collapses to the same handle at the RA.
func (wf *workflows) dispatchDesignJob(ctx workflow.Context, a dispatchDesignJobArgs) (constructionpipeline.PipelineHandle, error) {
	// The .claude command slug the design job runs — the doctrine that used to be
	// composed into design_prompt now lives in that command's method-assets. An empty
	// slug is contract misuse (an undispatchable (kind, mode) — e.g. SdpReview, which is
	// assembled server-side, never dispatched); fail terminally before dispatch.
	command := projectstate.DesignCommandFor(toPSKind(a.ArtifactKind), designModeFor(a.Target), "")
	if command == "" {
		return constructionpipeline.PipelineHandle(""), temporal.NewNonRetryableApplicationError(
			"no design command slug for this (artifactKind, jobMode) — undispatchable design job", "UndispatchableDesignJob", nil)
	}
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(a.ArtifactKind),
		dispatchInputCommand:       command,
		dispatchInputTargetBranch:  a.TargetBranch,
		dispatchInputPriorStateRef: a.PriorStateRef,
		dispatchInputJobMode:       jobModeFor(a.Target),
	}
	// Per-project-design-dispatch: decode the opaque per-project RepoRef → owner/repo so
	// the RA dispatches to the USER'S per-project repo + aiarch-design.yml (NOT the central
	// construction repo). Empty TargetRepo ⇒ zero RepoTarget ⇒ the RA falls back.
	target, terr := designRepoTarget(a.TargetRepo)
	if terr != nil {
		return constructionpipeline.PipelineHandle(""), terr
	}
	spec := constructionpipeline.PipelineSpec{
		ProjectID: constructionpipeline.ProjectID(a.ProjectID),
		// A non-empty, well-formed step graph satisfies the RA's §2.1 pre-condition; the
		// design recipe lives in the user's aiarch-design.yml workflow file, so the step is
		// a logical placeholder. The DESIGN-job parameters ride on DispatchInputs.
		Steps: []constructionpipeline.PipelineStep{{
			Name:      "design",
			Toolchain: constructionpipeline.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
	}
	if a.TargetRepo != "" {
		spec.WorkflowFile = designWorkflowFileName
	}
	return wf.Acts.PipelineSubmitConstructionPipeline(ctx, spec)
}

// observeDesignJob reads the dispatched job's phase once (pull-shaped, side-effect-free;
// constructionPipelineAccess.md §2.2) through the generated invoker and maps the RA phase
// onto this Manager's neutral phase.
func (wf *workflows) observeDesignJob(ctx workflow.Context, handle constructionpipeline.PipelineHandle) (pipelineObservation, error) {
	obs, err := wf.Acts.PipelineObserveConstructionPipeline(ctx, handle)
	if err != nil {
		return pipelineObservation{}, err
	}
	return pipelineObservation{
		Phase:      designPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
		RunURL:     obs.RunURL,
	}, nil
}

// dispatchAndObserve runs ONE dispatch → observe round-trip: it dispatches the design
// job (the generated submit invoker via dispatchDesignJob) and then polls the observe
// invoker (observeDesignJob) between durable startTimer waits until the job reaches a
// TYPED terminal phase. It returns the terminal observation; the caller decides success
// (read-back) vs failure (the StageDraftFailed gate). It NEVER infers failure from a
// timeout-as-success (§0d.4): a stuck job that never terminates within the bounded poll
// budget is surfaced as an explicit PipelineFailed with a neutral diagnostic, so the
// caller still lands the session at the human gate.
//
// While the round-trip is in flight it OWNS state.runURL (F-GTD-6): reset on the fresh
// dispatch, stamped from each observation that resolved the run's URL (so the
// sessionState Query's generating view can deep-link the live GitHub Actions run), and
// cleared on the terminal observation / any exit — the failed card gets its OWN
// failureRunURL from the returned observation instead. Setting it is workflow-local
// state served by view(); no Temporal history command, so no GetVersion gate is needed
// (the activeRole honesty invariant).
func (wf *workflows) dispatchAndObserve(ctx workflow.Context, args dispatchDesignJobArgs, state *coAuthorState) (pipelineObservation, error) {
	// Fresh dispatch — no observation yet, so no run to link (never a stale one).
	state.runURL = ""
	defer func() { state.runURL = "" }()
	handle, err := wf.dispatchDesignJob(ctx, args)
	if err != nil {
		return pipelineObservation{}, err
	}
	if constructionpipeline.PipelineHandleIsZero(handle) {
		return pipelineObservation{}, temporal.NewNonRetryableApplicationError(
			"dispatch returned an empty pipeline handle", "EmptyPipelineHandle", nil)
	}

	for poll := 0; poll < maxObservePolls; poll++ {
		obs, err := wf.observeDesignJob(ctx, handle)
		if err != nil {
			return pipelineObservation{}, err
		}
		if obs.RunURL != "" {
			state.runURL = obs.RunURL
		}
		if obs.Phase.IsTerminal() {
			return obs, nil
		}
		// Not yet terminal — space the next observe with a durable in-workflow timer.
		if err := workflow.Sleep(ctx, observePollInterval); err != nil {
			return pipelineObservation{}, err
		}
	}
	// Bounded poll budget exhausted without a terminal phase. Treat as an explicit
	// terminal failure (NOT a success, NOT a perpetual Drafting) so the caller routes
	// to the StageDraftFailed human gate.
	return pipelineObservation{
		Phase:      pipelineFailed,
		Diagnostic: "design job did not reach a terminal state within the observation window",
	}, nil
}

// readBackCritique reads back the PM-critique verdict the critique Action produced,
// via projectStateAccess.ReadProject of the Kind slot (§0d.2 step 6 — "steps 2–5
// with the PM-role prompt … the Manager reads back"). The critique job runs over
// the just-committed draft; on CritiqueRevise the Action records its revision
// guidance, on CritiqueApprove it ratifies the draft unchanged.
//
// RATIFIED D-MSD-Δ amendment (2026-06-15): the read-back uses the FIRST-CLASS
// optional ArtifactSlot.CritiqueVerdict / CritiqueNotes carrier (artifactmodel.go),
// NOT the frozen ArtifactSlot.Notes field. The senior review of C-MSD-Δ escalated
// the prior Notes-overload as a genuine contract-design gap: Notes carries the
// architect's reject/withdraw rationale (a distinct writer), so a PM-kind reject
// loop (RejectArtifact writes slot.Notes; then draft→critique→readBackCritique with
// NO intervening Stage) would misread the reject notes as the PM verdict, and
// "empty Notes = approve" cannot represent a legit empty-notes revise. The
// dedicated carrier is the single read-back location, written ONLY by the critique
// Action and cleared by every stage/status-transition verb, so no collision and no
// ambiguity remain.
//
// SAFE DEFAULT — missing verdict is a DRAFT FAILURE, not a silent approve. After a
// critique dispatch reached PhaseSucceeded, the Action is contractually obligated to
// have committed an explicit CritiqueVerdict ("approve" | "revise"). An EMPTY verdict
// means the job claimed success but committed no verdict — a contract violation
// between the Action and the read-back, exactly like readBackCommittedModel's empty-
// model case. We surface it as a terminal error (routed to the StageDraftFailed human
// gate by the caller), NEVER a silent CritiqueApprove. Justification: a silent approve
// on a missing verdict would let an unreviewed (or half-failed) draft sail to the human
// gate as if the PM ratified it — the worse failure mode. Treating it as a draft
// failure keeps the human in the loop with a clear "retry/withdraw" affordance and is
// consistent with the anti-wedge discipline (a ran-but-incomplete job is terminal-at-
// the-Manager, escalated to the human, not absorbed).
// readBackCritiqueOn computes the read-back critique with an OPTIONAL branch override (§2a): the
// PM-critique Action commits its verdict carrier on the critique session branch, so the
// read-back reads that branch when the rail is enabled. branch=="" reads main (the
// dormant-rail / non-git behavior).
func (wf *workflows) readBackCritiqueOn(ctx workflow.Context, projectID ProjectID, kind ArtifactKind, branch string) (critique, error) {
	proj, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return critique{}, err
	}
	slot := slotFor(proj, kind)
	switch slot.CritiqueVerdict {
	case projectstate.CritiqueVerdictApprove:
		// Carry the PM's notes on APPROVE too (F-QA2-7): the critique prompt's verdict
		// discipline records taste-level reservations as comments ON an approve, and the
		// session view now surfaces the PM conclusion at the human gate — dropping the
		// notes here would show the founder a bare verdict with the rationale erased.
		// The spine itself still consults Notes only on Revise, so this is display-only.
		return critique{Verdict: critiqueApprove, Notes: slot.CritiqueNotes}, nil
	case projectstate.CritiqueVerdictRevise:
		return critique{Verdict: critiqueRevise, Notes: slot.CritiqueNotes}, nil
	default:
		// Empty / unknown verdict after a PhaseSucceeded critique job: the safe default
		// is a draft failure, not a silent approve (see the doc comment's justification).
		return critique{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("critique job reported success but committed no critique verdict for %s (read-back carrier empty)", artifactKindString(kind)),
			"CritiqueReadBackEmpty", nil)
	}
}

// readBackCommittedModelOn reads the typed model with an OPTIONAL branch override
// (§2a): the draft Action commits the typed JSON on the SESSION BRANCH, so the read-back
// reads that branch while the human reviews the not-yet-merged draft. branch=="" reads
// main (the dormant-rail / non-git behavior). It returns the read-back substrate's
// Version alongside the model so the caller can stage against the ACTUAL branch version
// — a fresh workflow reusing a dirty session branch (prior draft/critique commits) sees
// the branch already advanced, and staging against a stale main-captured version would
// Conflict (QA F29).
func (wf *workflows) readBackCommittedModelOn(ctx workflow.Context, projectID ProjectID, kind ArtifactKind, branch string) (projectstate.ArtifactModel, projectstate.Version, error) {
	proj, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return nil, 0, err
	}
	slot := slotFor(proj, kind)
	if slot.Model == nil {
		return nil, 0, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("design job reported success but committed no %s model to read back", artifactKindString(kind)),
			"ReadBackEmpty", nil)
	}
	return slot.Model, proj.Version, nil
}

// gitsession.go is the WORKFLOW-LEVEL wiring of the settled branch→PR→read-back→+1→merge
// design model (I-DESIGN-DISPATCH §2b) into the CoAuthorArtifactWorkflow spine. It
// MIRRORS the construction Manager's gitforward.go: the rail OWNS the git provider
// interaction (ensure branch, open PR, read CI rollup, relay +1, perform merge) and
// RETURNS opaque handles; the Manager threads a once-minted credential into every verb;
// the branch-aware read-back/stage (§2a) rides over the session branch while the human
// reviews, then commit/advance land on main AFTER the merge.
//
// DORMANT-WHEN-UNWIRED: every helper checks gf.enabled. When the rail/repo is not wired
// the session is disabled and each helper is a no-op that leaves the spine on the
// original main-path behavior (the read-back branch is "" ⇒ main).

// gitSession is the per-draft-attempt git-lifecycle state the spine carries. It is
// workflow-local (rebuilt deterministically on replay) and holds the opaque handles the
// rail returned + the once-minted credential. branch is the session branch the Action
// drafts/commits + opens its PR on; readBackBranch returns "" (main) when disabled so
// the branch-aware read-back/stage collapse to the original behavior.
type gitSession struct {
	enabled bool
	repoRef sourcecontrol.RepoRef
	cred    railCredEnvelope
	branch  string
	prRef   string
}

// readBackBranch is the branch the read-back + AwaitingReview-stage ride over. The
// session branch while a draft is staged for review (so the human sees the not-yet-
// merged draft); "" (main) when the rail is dormant (the original behavior).
func (gf gitSession) readBackBranch() string {
	if gf.enabled {
		return gf.branch
	}
	return ""
}

// dispatchRepo is the opaque per-project RepoRef the agentic design job dispatches to
// (per-project-design-dispatch): the user's per-project repo where aiarch-design.yml
// was committed at project birth. "" when the rail is dormant ⇒ the RA falls back to
// the configured construction repo (the non-git / Postgres path is unchanged).
func (gf gitSession) dispatchRepo() string {
	if gf.enabled {
		return sourcecontrol.RepoRefString(gf.repoRef)
	}
	return ""
}

// gitEnabled reports whether the PR rail is wired AND a repo resolves for this project.
// When false the spine runs unchanged (read-back/stage on main, no branch/PR ops).
func (wf *workflows) gitEnabled(projectID ProjectID) (sourcecontrol.RepoRef, bool) {
	if wf.Rail == nil || wf.Repo == nil {
		return sourcecontrol.RepoRef(""), false
	}
	return wf.Repo(projectID)
}

// beginSession runs the dispatch-time half of the rail lifecycle for one draft attempt:
// mint the credential, then OpenBranch(sessionBranch) (ensure the branch exists before
// the Action drafts on it). A dormant slice returns a disabled session and touches
// nothing. The session branch is per-attempt (designBranch threads the attempt suffix).
func (wf *workflows) beginSession(ctx workflow.Context, projectID ProjectID, sessionBranch string) (gitSession, error) {
	repoRef, ok := wf.gitEnabled(projectID)
	if !ok {
		return gitSession{enabled: false}, nil
	}
	gf := gitSession{enabled: true, repoRef: repoRef, branch: sessionBranch}

	cred, err := wf.mintCred(ctx, repoRef)
	if err != nil {
		return gitSession{}, err
	}
	gf.cred = cred

	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch, 2026-07-06): before ANY design job is
	// dispatched, converge the seated aiarch-design.yml onto the CURRENT template
	// rendering (drift → one refresh commit on the default branch; identical → no-op).
	// The birth seat runs ONCE under a constant idempotency key, so without this a
	// server release that moves the aiarch-state-mcp pin strands every live repo on a
	// binary the new validators reject (the gtdapp F81 incident). A sync failure BLOCKS
	// the dispatch — never run a design job against a scaffold we could not prove
	// current — and is CONTAINED by the caller at the failed gate like every other
	// dispatch-time rail fault.
	//
	// Temporal versioning guard (replay safety; mirrors construction-review-policy-
	// snapshot): this activity was ADDED to beginSession AFTER the CoAuthor workflow
	// first shipped, so a design session already in flight at deploy time has NO history
	// event for it — replaying such a history against unguarded new code fails the
	// workflow task with a non-determinism error (observed live: gtdapp:5 amendment
	// session — queries dead with "Workflow Task in failed state", the Retry signal
	// unprocessable). GetVersion pins pre-feature executions (DefaultVersion) to the OLD
	// command sequence: they skip the sync for their WHOLE run — including post-recovery
	// redrafts, because the version resolved at first replay is cached per execution —
	// while every execution STARTED after this deploy resolves v1 and syncs before each
	// dispatch. A pre-feature session that keeps failing on a stale scaffold heals via
	// Withdraw + a fresh amendment session (a new execution → v1 → sync).
	if workflow.GetVersion(ctx, "managed-scaffold-sync", workflow.DefaultVersion, 1) >= 1 {
		var scaffoldChanged bool
		// SyncManagedScaffold is the GENERATED sourceControlAccess.syncManagedScaffold
		// invoker (B10), wrapped in the shared bounded Auth retry exactly as every other
		// dispatch-time rail verb.
		if serr := wf.railWithAuthRetry(ctx, func() error {
			changed, e := wf.Acts.RailSyncManagedScaffold(ctx, repoRef, cred.toRail())
			scaffoldChanged = changed
			return e
		}); serr != nil {
			return gitSession{}, fmt.Errorf("managed-scaffold sync failed — the seated %s could not be refreshed to this server's current template, so the design job was NOT dispatched (a stale scaffold pins an aiarch-state-mcp binary this server's validators reject); Retry re-runs the sync: %w", designWorkflowFileName, serr)
		}
		if scaffoldChanged {
			workflow.GetLogger(ctx).Info("managed scaffold drifted; refreshed the seated design workflow to the current template before dispatch",
				"file", designWorkflowFileName)
		}
	}

	// OpenBranch through the shared bounded Auth retry: a secondary-rate-limit 403 here no
	// longer kills the session (QA F35 twin). A genuine denial exhausts the budget and the
	// caller (runDraftRoundTrip) CONTAINS the fault at the failed gate. The opened BranchRef
	// is not retained (the deterministic session-branch name is the addressing key).
	if err := wf.railWithAuthRetry(ctx, func() error {
		_, e := wf.Acts.RailOpenBranch(ctx, repoRef, sourcecontrol.BranchName(sessionBranch), cred.toRail())
		return e
	}); err != nil {
		return gitSession{}, err
	}
	return gf, nil
}

// openPR opens the PR (head=sessionBranch, base=main) AFTER the draft observe succeeds.
// Idempotent on head — if the Action already opened a PR the rail returns the existing
// handle (the server's open is the authoritative handle for the merge step). A dormant
// session is a no-op.
func (wf *workflows) openPR(ctx workflow.Context, gf *gitSession, kind ArtifactKind) error {
	if !gf.enabled {
		return nil
	}
	// OpenPullRequest through the shared bounded Auth retry (QA F35 twin): openPR runs in the
	// draft round-trip AFTER a 20+ minute draft, so a single secondary-rate-limit 403 must not
	// discard that work. A genuine permission denial exhausts the budget and the caller CONTAINS
	// the fault at the failed gate (the committed draft is preserved; Retry resumes).
	var prRef string
	if err := wf.railWithAuthRetry(ctx, func() error {
		pr, e := wf.Acts.RailOpenPullRequest(ctx, gf.repoRef, sourcecontrol.PullRequestSpec{
			Head:  sourcecontrol.BranchName(gf.branch),
			Base:  sourcecontrol.BranchName(mainBranch),
			Title: designPRTitle(kind),
			Body:  designPRBody(kind),
		}, gf.cred.toRail())
		if e != nil {
			return e
		}
		prRef = sourcecontrol.PullRequestRefString(pr)
		return nil
	}); err != nil {
		return err
	}
	gf.prRef = prRef
	return nil
}

// mergeOnApprove runs the approve-time half of the rail lifecycle: the merge GUARD
// (GetPullRequestStatus — CheckRollup must be green), the architecture +1 relay
// (PostReview Approve), and the App-mediated merge (MergePullRequest sessionBranch →
// main). It returns ok=true only when the merge landed; ok=false means the merge guard
// was not green (the caller routes that to the StageDraftFailed recovery gate — the PR
// is not green, do NOT merge, never wedge). A dormant session returns ok=true (the
// non-git spine commits on main with no rail).
func (wf *workflows) mergeOnApprove(ctx workflow.Context, projectID ProjectID, gf *gitSession, kind ArtifactKind) (bool, error) {
	if !gf.enabled {
		return true, nil
	}

	// Merge guard: the required CI check must be green before the App merges (the
	// "blocks merge" trust boundary). A non-green PR is NOT merged — the caller routes
	// to recovery. execRailActivityWithAuthRetry absorbs a transient (rate-limit) 403 within
	// a bounded WORKFLOW-SIDE budget (QA F35) so a single secondary-rate-limit blip no longer
	// kills the approve.
	var st pullRequestStatusView
	if err := wf.railWithAuthRetry(ctx, func() error {
		prStatus, e := wf.Acts.RailGetPullRequestStatus(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef), gf.cred.toRail())
		if e != nil {
			return e
		}
		st = pullRequestStatusView{
			CheckGreen:    prStatus.CheckRollup == sourcecontrol.CheckSuccess,
			ApprovalCount: int(prStatus.ApprovalCount),
			Mergeable:     prStatus.Mergeable,
		}
		return nil
	}); err != nil {
		return false, err
	}
	if !st.CheckGreen {
		return false, nil
	}

	// F80c: the required check is green, but the PR may be MERGEABLE=false — main advanced
	// under the session branch (a staleness ack, a question seed) and their project.json
	// (a server-owned, single-writer-per-slot document) conflicts, so mergeable_state is
	// dirty. Attempting the merge here would fail and, worse, RE-APPROVING would loop
	// forever (the branch stays dirty). Instead RECONCILE the branch server-side — overlay
	// main's other slots onto the branch tip so it differs from main only in the in-flight
	// slot — which pushes a new commit that makes the PR mergeable. That push re-triggers
	// the required CI check, so we cannot merge in THIS pass; return the honest not-merged
	// path carrying an actionable reason (the caller re-awaits, and the next approve — once
	// CI is green again — merges cleanly). If the substrate cannot reconcile, the same
	// honest fallback applies.
	if !st.Mergeable {
		if rerr := wf.reconcileDivergedBranch(ctx, projectID, gf, kind); rerr != nil {
			return false, rerr
		}
		return false, temporal.NewNonRetryableApplicationError(
			"design PR was not mergeable (main advanced under the session branch); the branch was reconciled with main and CI is re-validating — re-approve once it is green",
			"DesignBranchReconciled", nil)
	}

	// Relay the architecture +1 (the counted approval + audit). The ReviewApprove verdict is
	// supplied here at the workflow call site (the generated PostReview invoker is verdict-
	// neutral — design only ever approves).
	if err := wf.railWithAuthRetry(ctx, func() error {
		return wf.Acts.RailPostReview(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef),
			sourcecontrol.ReviewSubmission{Verdict: sourcecontrol.ReviewApprove, Body: designArchApprovalBody(kind)}, gf.cred.toRail())
	}); err != nil {
		return false, err
	}

	// App-mediated merge of sessionBranch → main.
	var merged bool
	if err := wf.railWithAuthRetry(ctx, func() error {
		mr, e := wf.Acts.RailMergePullRequest(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef), gf.cred.toRail())
		if e != nil {
			return e
		}
		merged = mr.Merged
		return nil
	}); err != nil {
		return false, err
	}
	if !merged {
		// The guard was green but the merge did not complete (a race / not-mergeable):
		// surface as terminal so the spine does not commit a false merge.
		return false, temporal.NewNonRetryableApplicationError(
			"design PR merge did not complete (not mergeable)", "DesignMergeNotCompleted", nil)
	}
	return true, nil
}

// reconcileDivergedBranch overlays main's slots (bar the in-flight one) onto the session
// branch tip so a MERGEABLE=false PR becomes mergeable again (F80c). It runs through
// applyRecovering so a stale-version Conflict re-reads the branch version and retries
// within bounded attempts; a substrate that lacks the reconcile extension surfaces an
// honest fwra.NotFound (designSessionAccess.reconcileBranchFromMain — B10; the RETIRED
// custom Activity's bespoke non-retryable "ReconcileUnsupported" Type() had no downstream
// consumer, so converging onto the standard fwra.Error→fwmanager.MapError path is
// behavior-preserving here: the caller only ever checks err != nil) the caller contains
// as an honest re-await. Seeding expectedVersion 0 is safe: an existing branch row trips
// the version guard → Conflict → applyRecovering re-reads the real branch version and
// retries.
func (wf *workflows) reconcileDivergedBranch(ctx workflow.Context, projectID ProjectID, gf *gitSession, kind ArtifactKind) error {
	branch := gf.readBackBranch()
	if branch == "" {
		return nil // dormant rail: no session branch to reconcile
	}
	_, err := wf.applyRecovering(ctx, projectID, branch, 0, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionReconcileBranchFromMain(ctx, projectstate.ProjectID(projectID), expected, branch, toPSKind(kind))
	})
	return err
}

// railAuthRetry* bound the workflow-side rail retry on a transient-403-as-Auth fault
// (QA F35 + its draft-round-trip twin). Shared by BOTH halves of the rail lifecycle:
// the dispatch-time half (OpenBranch / OpenPullRequest) and the approve-time half
// (GetPullRequestStatus / PostReview / MergePullRequest).
const (
	railAuthRetryMaxAttempts = 3
	railAuthRetryBaseBackoff = 5 * time.Second
	railAuthRetryMaxBackoff  = 15 * time.Second
)

// railWithAuthRetry runs ANY rail call (a closure over a generated invoker, incl.
// syncManagedScaffold — B10) with a bounded WORKFLOW-SIDE retry on a transient-403-as-Auth
// fault (QA F35 + its draft-round-trip twin). The platform github ClassifyStatus conflates
// GitHub secondary rate-limit 403s with real permission denials — both become a NON-RETRYABLE
// Auth ApplicationError the Activity RetryPolicy cannot retry — so the workflow retries here:
// up to railAuthRetryMaxAttempts over ~30s (5s → 10s → cap 15s), with workflow.Sleep for
// deterministic backoff. A GENUINE permission denial exhausts the budget and the error
// propagates to the CALLER, which CONTAINS it (openPR/OpenBranch → the StageDraftFailed gate;
// the approve window → back to AwaitingReview for re-approve) — never a crash. Transport blips
// (Transient) are still retried INSIDE the Activity by railActivityOptions. Cancellation
// propagates immediately. This is the ONE shared helper — the approve window and the draft
// round-trip do NOT duplicate the retry loop.
func (wf *workflows) railWithAuthRetry(ctx workflow.Context, call func() error) error {
	backoff := railAuthRetryBaseBackoff
	for attempt := 1; ; attempt++ {
		err := call()
		if err == nil {
			return nil
		}
		if temporal.IsCanceledError(err) || !isRailAuthFault(err) || attempt >= railAuthRetryMaxAttempts {
			return err
		}
		workflow.GetLogger(ctx).Warn("rail 403 (auth/rate-limit); bounded workflow-side retry", "attempt", attempt)
		_ = workflow.Sleep(ctx, backoff)
		if backoff *= 2; backoff > railAuthRetryMaxBackoff {
			backoff = railAuthRetryMaxBackoff
		}
	}
}

// mintCred runs the generated sourceControlAccess.getInstallationToken invoker → the
// short-lived credential threaded into every rail verb for this draft attempt's lifecycle.
func (wf *workflows) mintCred(ctx workflow.Context, repoRef sourcecontrol.RepoRef) (railCredEnvelope, error) {
	cred, err := wf.Acts.RailGetInstallationToken(ctx, repoRef)
	if err != nil {
		return railCredEnvelope{}, err
	}
	return railCredEnvelope{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

// readProjectOnBranch reads the head-state on an OPTIONAL branch override (§2a). When
// branch=="" or the ProjectState substrate does not support the branch-aware extension,
// it falls back to the original main-path ReadProject — so the branch-aware read-back is
// purely additive and the default path is unchanged. The read runs through the generated
// designSessionAccess.readProjectOnBranch invoker (B10) for both cases; branch=="" is
// short-circuited to readProject (its own DesignSessionReadProjectOnBranch call) so the
// two collapse to the SAME activity registration, matching pre-migration behavior.
// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
func (wf *workflows) readProjectOnBranch(ctx workflow.Context, projectID ProjectID, branch string) (projectstate.Project, error) {
	if branch == "" {
		return wf.readProject(ctx, projectID)
	}
	pe, err := wf.Acts.DesignSessionReadProjectOnBranch(ctx, projectstate.ProjectID(projectID), branch)
	if err != nil {
		return projectstate.Project{}, err
	}
	return pe.Decode()
}

// gitrail.go is the PR-rail consumer port the design Manager uses to wire the agentic
// DESIGN draft onto the git-forward branch→PR→read-back→+1→merge model
// (I-DESIGN-DISPATCH §2b). It holds the non-Activity value carriers (railCredEnvelope,
// pullRequestStatusView), the provider-neutral PR text builders, and the ActivityOptions
// presets the workflow-side helpers in gitsession.go consume — it holds NO Temporal
// Activities of its own (B10: every rail verb, including syncManagedScaffold, is
// GENERATED and reached through the generated invoker surface, wf.Acts.Rail*).
//
// SUBSET. The design spine needs only the rail verbs the settled flow uses:
// getInstallationToken (mint), openBranch (ensure the session branch), openPullRequest
// (head=sessionBranch, base=main), getPullRequestStatus (the merge guard),
// postReview (the architecture +1 relay), mergePullRequest (the App-mediated merge),
// syncManagedScaffold (the pre-dispatch scaffold-drift refresh). configureBranchProtection
// is a project-birth concern (FU-DD-3), unused here.
//
// DORMANT-WHEN-UNWIRED. The whole rail is OPTIONAL/nil-tolerant exactly like the
// construction git-forward slice: when wf.Rail == nil or wf.Repo == nil (or no repo
// resolves for the project) the CoAuthor workflow runs UNCHANGED — read-back/stage on
// main, no branch/PR ops — so every existing test and the Postgres/non-git composition
// are unperturbed.

// ===========================================================================
// Activity-boundary value carriers (mirrors gitactivities.go).
// ===========================================================================

// railCredEnvelope carries the opaque short-lived credential across the Activity
// boundary. The Bytes are write-only at every consumer (never logged); they ride the
// Temporal payload exactly as the rail returns them.
type railCredEnvelope struct {
	Bytes     []byte
	ExpiresAt time.Time
}

func (c railCredEnvelope) toRail() sourcecontrol.RepoCredential {
	return sourcecontrol.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

// pullRequestStatusView is the Manager-local Activity-boundary projection of the rail's
// PullRequestStatus — the merge-guard reflection the workflow reads before approve/merge.
type pullRequestStatusView struct {
	CheckGreen    bool
	ApprovalCount int
	Mergeable     bool
}

// ===========================================================================
// Provider-neutral naming + Activity option presets (mirrors gitnaming.go).
// ===========================================================================

// mainBranch is the flat git-forward base every design PR targets (op-concepts §15).
const mainBranch = "main"

// designPRTitle / designPRBody are the human-facing PR text the Manager owns.
func designPRTitle(kind ArtifactKind) string {
	return fmt.Sprintf("aiarch: design %s", artifactKindString(kind))
}

func designPRBody(kind ArtifactKind) string {
	return fmt.Sprintf("Automated agentic design draft of %s (aiarch system-design).", artifactKindString(kind))
}

// designArchApprovalBody is the +1 relay's review body — the architect's in-app
// approval relayed onto the PR (the "architecture +1").
func designArchApprovalBody(kind ArtifactKind) string {
	return fmt.Sprintf("architecture +1 relayed for %s", artifactKindString(kind))
}

// setCommentStatusSignal is the SetReviewCommentStatus signal payload. It rides the
// signalSetCommentStatus channel to the CoAuthorArtifactWorkflow suspended at the
// AwaitingReview gate, which applies the branch mutation (open->waived / addressed->open).
type setCommentStatusSignal struct {
	CommentID string
	Status    string
}

// feedbackToLedgerComments converts the architect's inbound anchored comments (the wire
// AnchoredComment carried on a Reject's ReviewFeedback) into the projectstate.ReviewComment
// shape the append verb stamps into the durable thread. Only Anchor / AnchorText / Text /
// AuthorRole are filled — the id / round / open status / empty response are server-minted
// in appendReviewComments. Free-text-only Notes are NOT comments (they stay the reject
// notes); an anchored comment with empty Text is dropped (defensive).
func feedbackToLedgerComments(feedback ReviewFeedback) []projectstate.ReviewComment {
	out := make([]projectstate.ReviewComment, 0, len(feedback.Comments))
	for _, c := range feedback.Comments {
		if c.Text == "" {
			continue
		}
		out = append(out, projectstate.ReviewComment{
			Anchor:     c.JSONPath,
			AnchorText: c.AnchorText,
			Text:       c.Text,
			AuthorRole: reviewAuthorRole,
		})
	}
	return out
}

// openReviewCommentIDs returns the ids of every OPEN CHANGE-REQUEST — the comments that
// gate approve (review-ledger §4). Open QUESTIONS never gate (question-comments §approve),
// so they are excluded. Empty ⇒ approve is unblocked.
func openReviewCommentIDs(thread []projectstate.ReviewComment) []string {
	var ids []string
	for _, c := range thread {
		if projectstate.ReviewCommentBlocksApprove(c) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// seedAmendmentLedger records the reopening feedback (coAuthorInput.Feedback) as round-0 OPEN
// ledger entries on the amendment session branch, right after the first stage, then reloads
// the in-memory thread so the query + prompt surface them. Best-effort: a seed miss (e.g. a
// non-ledger substrate) leaves the feedback in the prompt only. No-op when there are no
// anchored comments to seed.
// maybeSeedAmendment seeds the amendment ledger exactly once, the first time an amendment
// session reaches AwaitingReview, returning the (possibly-updated) seeded flag. Keeps the
// spine flat (the F38 guard lives here, not inline in the workflow body).
func (wf *workflows) maybeSeedAmendment(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, seeded bool, state *coAuthorState) bool {
	if in.Amendment > 0 && !seeded {
		wf.seedAmendmentLedger(ctx, in, gf, headVersion, state)
		return true
	}
	return seeded
}

func (wf *workflows) seedAmendmentLedger(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, state *coAuthorState) {
	if in.Feedback == nil {
		return
	}
	comments := feedbackToLedgerComments(*in.Feedback)
	if len(comments) == 0 {
		return
	}
	branch := gf.readBackBranch()
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, branch, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSeedReviewCommentsOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, branch, toPSKind(in.ArtifactKind), 0, comments)
	})
	if err != nil {
		return
	}
	*headVersion = newVersion
	// The amendment feedback is now durably in the ledger; keep feedbackSeeded true so the
	// pre-dispatch failed-gate seed does not re-seed the same round-0 comments.
	state.feedbackSeeded = true
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
}

// seedFailedGateFeedback durably records the architect feedback that a MEMORY-ONLY failed-gate
// recovery path (a redraft signal / a Retry-via-Reject AT a failed gate / a faulted reject /
// a PM-critique revise) retained ONLY in the workflow's feedback variable. Unlike the review-
// gate reject and the amendment seed, those paths never wrote it to the durable review ledger —
// so under thin dispatch (the drafting agent reads context ONLY via getReviewThread) it would
// evaporate. This folds the SAME anchored comments the reject path uses (feedbackToLedgerComments)
// into the ledger on the SAME session branch, consuming a review round (reviewRound, like a
// reject) so the seeded ids do not collide with a later reject's on the one accumulating thread.
// Best-effort, mirroring seedAmendmentLedger: a Notes-only feedback (no anchored comments), an
// unpopulated slot (no prior staged draft to anchor to on a first-round dispatch), a non-ledger
// substrate, or a transient fault leaves the feedback un-seeded and RETRIES on the next redraft
// dispatch. Returns whether the seed durably landed, so the caller marks feedbackSeeded and
// stops re-seeding. headVersion is a hint only — applyRecovering re-reads on a version conflict.
func (wf *workflows) seedFailedGateFeedback(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion projectstate.Version, feedback *ReviewFeedback, reviewRound *int, state *coAuthorState) bool {
	comments := feedbackToLedgerComments(*feedback)
	if len(comments) == 0 {
		return false
	}
	branch := gf.readBackBranch()
	round := int64(*reviewRound)
	if _, err := wf.applyRecovering(ctx, in.ProjectID, branch, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSeedReviewCommentsOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, branch, toPSKind(in.ArtifactKind), round, comments)
	}); err != nil {
		return false
	}
	// A durable ledger write consumes a review round (exactly like the reject path), so a LATER
	// reject's r{round}c{n} ids do not collide with these on the accumulating thread.
	*reviewRound++
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
	return true
}

// loadReviewThread reads the artifact slot's durable ledger from the session branch (the
// same branch the draft is staged on; "" ⇒ main). Called on the workflow goroutine after
// every (re)stage and after every waive/reopen so the sessionState Query + the approve gate
// see the live thread. A read fault is returned to the caller, which keeps the last-known
// thread (the ledger is auxiliary display/gate state — a transient read miss must not derail
// the review session). Delegates to the shared readProjectOnBranch helper (gitsession.go)
// rather than duplicating the read-and-decode inline.
func (wf *workflows) loadReviewThread(ctx workflow.Context, in coAuthorInput, gf gitSession) ([]projectstate.ReviewComment, error) {
	proj, err := wf.readProjectOnBranch(ctx, in.ProjectID, gf.readBackBranch())
	if err != nil {
		return nil, err
	}
	return slotFor(proj, in.ArtifactKind).ReviewThread, nil
}

// applyCommentStatus applies one human review-ledger transition (waive / reopen) to the
// session branch during the AwaitingReview window, then refreshes the in-memory thread so
// the query + approve gate reflect it. Best-effort: an illegal transition / unknown id /
// transient fault leaves the review session at the gate with the unchanged thread (the
// manager's SetReviewCommentStatus pre-check already rejects most bad requests
// synchronously; this is the durable apply, not the validation point).
func (wf *workflows) applyCommentStatus(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, sig setCommentStatusSignal, state *coAuthorState) {
	branch := gf.readBackBranch()
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, branch, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSetReviewCommentStatusOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, branch, toPSKind(in.ArtifactKind), sig.CommentID, sig.Status)
	})
	if err != nil {
		return
	}
	*headVersion = newVersion
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
}

// kindHasPMCritique reports whether the Method assigns a PM reviewer to this kind
// (mission / glossary+scrubbed / core-use-cases — rework §2.1, §6.6). The
// architect-owned steps (volatilities, architecture, standard-check) skip PM
// critique entirely.
//
// LOCKSTEP PIN: projectstate.DesignCommandFor's critique-slug gate
// (designKindHasPMCritique, resourceaccess/projectstate/projectstateaccess.go)
// is a deliberate, non-imported duplicate of this switch's case list — RA sits
// below this Manager layer and cannot import it. Edit both switches together.
func kindHasPMCritique(kind projectstate.ArtifactKind) bool {
	switch kind {
	case projectstate.KindMission,
		projectstate.KindGlossary,
		projectstate.KindScrubbedRequirements,
		projectstate.KindCoreUseCases:
		return true
	case projectstate.KindVolatilities, projectstate.KindSystem, projectstate.KindOperationalConcepts,
		projectstate.KindStandardCheck, projectstate.KindPlanningAssumptions, projectstate.KindActivityList,
		projectstate.KindNetwork, projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution,
		projectstate.KindCompressedSolution, projectstate.KindDecompressedSolution, projectstate.KindRiskModel,
		projectstate.KindSdpReview:
		// Architect-owned Phase-1 steps (no PM critique) and all Phase-2 kinds
		// (Phase 2 has no PM-critique step at all) — same as the default below.
		return false
	default:
		return false
	}
}

// sameArtifactModel reports whether two typed models are byte-identical in their
// canonical JSON form. Go marshals a given concrete struct deterministically (field
// order is declaration order; map keys are sorted), so this is a stable, replay-safe
// value comparison the workflow goroutine may call directly (no I/O). Used by the
// amendment no-change guard: when an amendment session's branch read-back is identical
// to the committed main model, the draft advanced the branch by nothing, so there is
// no change to review or merge and the session must land at the failed gate rather than
// 422 on an effectively-empty PR.
func sameArtifactModel(a, b projectstate.ArtifactModel) (bool, error) {
	ea, err := encodeModel(a)
	if err != nil {
		return false, err
	}
	eb, err := encodeModel(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ea.Model, eb.Model), nil
}

// encodeModel delegates to the promoted projectstate.EncodeModel. Kept as a
// package-level wrapper (rather than rewriting every call site to the qualified name)
// so this move stays a minimal, mechanical diff.
func encodeModel(model projectstate.ArtifactModel) (modelEnvelope, error) {
	return projectstate.EncodeModel(model)
}

// critique is the PM-critique result. On Revise the Manager's sequence loops back to
// the architect-role draft step with Notes woven in, BEFORE the human gate.
type critique struct {
	Verdict critiqueVerdict `json:"verdict"`
	Notes   string          `json:"notes"`
}

// critiqueRoleProductManager is the CritiqueView.Role wire label for the PM critic —
// the only critique-issuing role today. Matches the SPA's ActiveRole wire naming
// ("productManager") so both surfaces name the role identically.
const critiqueRoleProductManager = "productManager"

// critiqueViewFor renders the observed PM-critique conclusion as the SessionStateView
// carrier (F-QA2-7): the wire verdict reuses the projectstate carrier's closed string
// set ("approve" | "revise"), Summary is the PM's rationale verbatim, and round is the
// redraft-round counter of the draft the critique judged. Pure mapping over the
// recorded read-back result — deterministic on replay, no history command.
func critiqueViewFor(c critique, round int) *CritiqueView {
	verdict := projectstate.CritiqueVerdictApprove
	if c.Verdict == critiqueRevise {
		verdict = projectstate.CritiqueVerdictRevise
	}
	return &CritiqueView{
		Role:    critiqueRoleProductManager,
		Verdict: verdict,
		Summary: c.Notes,
		Round:   int64(round),
	}
}

// critiqueVerdict is the closed PM verdict set.
type critiqueVerdict int

const (
	critiqueUnknown critiqueVerdict = iota
	critiqueApprove                 // PM ratifies the draft; proceed to the human gate
	critiqueRevise                  // PM asks for revision; loop back to the draft step with Notes
)

// Validate is the optional mechanical shape hook GenerateTypedData[Critique] runs
// after unmarshal. A Revise verdict must carry Notes; an out-of-range verdict is
// unconstructable.
func (c *critique) Validate() error {
	switch c.Verdict {
	case critiqueApprove:
		return nil
	case critiqueRevise:
		if c.Notes == "" {
			return fmt.Errorf("critique: revise verdict requires Notes")
		}
		return nil
	case critiqueUnknown:
		// The zero value: an unset/unconstructed Verdict, not a real critique
		// outcome — falls through to the same "unknown ordinal" rejection.
		return fmt.Errorf("critique: unknown verdict ordinal %d", int(c.Verdict))
	default:
		return fmt.Errorf("critique: unknown verdict ordinal %d", int(c.Verdict))
	}
}

// statevalidationfindings.go holds the APP-SIDE read-back finding generators for the
// state-validation rules the architect ratified 2026-07-05. Each is the review-panel
// twin of an authoritative platform methodcheck rule (tracked "platform twin pending" in
// docs/later.md); the app surfaces them as SessionStateView.Findings so the reviewer sees
// the defect at the human gate. They are DISPLAY findings — they do not hard-fail a read
// (a committed state that violates them, e.g. gtdapp's orphan ResourceAccess and
// empty-encapsulates clients, must keep rendering with the finding visible until an
// amendment fixes it). The presence/consistency rules that CAN hard-fail safely (every
// committed state already satisfies them) live in projectstate.RequireModelFields instead.
//
// Each generator early-returns nil for a non-matching artifact kind / nil draft, mirroring
// useCaseActivityFindings / systemLayerDegenerateFindings, so view() can append them all
// unconditionally.

// stateValidationFindingGenerators is the ordered set of read-back finding generators
// view() appends. Each takes the drafted artifact's kind + model and returns nil for a
// non-matching kind, so the whole set can be applied unconditionally.
var stateValidationFindingGenerators = []func(ArtifactKind, projectstate.ArtifactModel) []Finding{
	raOrphanFindings,      // SYS-RA-ORPHAN
	encapsulatesFindings,  // SYS-ENCAPSULATES
	relDupFindings,        // SYS-REL-DUP
	dvChainFindings,       // DV-CHAIN-CONNECTED
	variationRefFindings,  // UC-VARIATION-REF
	glossaryFourQFindings, // GLOSS-FOURQ
	scrubbedIDFindings,    // SR-ID-UNIQUE
	opcTopicFindings,      // OPC-TOPIC-COVERAGE
}

// raOrphanFindings — SYS-RA-ORPHAN (error). Every ResourceAccess component must have at
// least one outbound sync/queued relationship to a Resource (or to a documented external
// system — an edge target that is not itself a modeled component). A ResourceAccess that
// reaches no resource encapsulates nothing.
func raOrphanFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	kindByID := make(map[string]projectstate.ComponentKind, len(sys.Components))
	for _, c := range sys.Components {
		kindByID[c.ID] = c.Kind
	}
	var out []Finding
	for i, c := range sys.Components {
		if c.Kind != projectstate.CompResourceAccess {
			continue
		}
		reaches := false
		for _, r := range sys.Relationships {
			if r.From != c.ID {
				continue
			}
			if r.Mode != projectstate.CallSync && r.Mode != projectstate.CallQueued {
				continue
			}
			toKind, known := kindByID[r.To]
			// A Resource target, or an external target (not a modeled component),
			// satisfies the rule.
			if !known || toKind == projectstate.CompResource {
				reaches = true
				break
			}
		}
		if !reaches {
			label := componentDisplayLabel(c, i)
			out = append(out, Finding{
				RuleID:   "SYS-RA-ORPHAN",
				Severity: SeverityError,
				Message:  fmt.Sprintf("ResourceAccess %q has no outbound sync/queued relationship to a resource (or documented external system); every ResourceAccess must encapsulate at least one resource.", label),
				Location: &Location{Ordinal: int64(i), Section: "component " + label},
			})
		}
	}
	return out
}

// encapsulatesFindings — SYS-ENCAPSULATES. Every component should name the volatility it
// encapsulates. ERROR for the volatility-owning kinds (client/manager/engine/
// resourceAccess); WARNING for resource/utility (which legitimately own no volatility but
// benefit from a one-line "what this is"). The manager/engine/resourceAccess non-empty
// rule is ALSO enforced hard on the write path by projectstate.RequireModelFields, so in
// practice only empty-encapsulates CLIENTS (error) and resources/utilities (warning) reach
// this read-back surface — which is exactly the gtdapp case that must render, not crash.
func encapsulatesFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	var out []Finding
	for i, c := range sys.Components {
		if strings.TrimSpace(c.Encapsulates) != "" {
			continue
		}
		var sev Severity
		switch c.Kind {
		case projectstate.CompClient, projectstate.CompManager, projectstate.CompEngine, projectstate.CompResourceAccess:
			sev = SeverityError
		case projectstate.CompResource, projectstate.CompUtility:
			sev = SeverityWarning
		}
		label := componentDisplayLabel(c, i)
		out = append(out, Finding{
			RuleID:   "SYS-ENCAPSULATES",
			Severity: sev,
			Message:  fmt.Sprintf("component %q has an empty encapsulates; state the volatility (or, for a resource/utility, the responsibility) it owns.", label),
			Location: &Location{Ordinal: int64(i), Section: "component " + label},
		})
	}
	return out
}

// relDupFindings — SYS-REL-DUP. An EXACT duplicate relationship (same from, to AND mode)
// is an ERROR (a redundant edge). Two edges on the SAME (from,to) pair that differ (a
// label-split) are a WARNING suggesting the labels be aggregated with " | " onto one edge.
func relDupFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	type pair struct{ from, to string }
	exact := map[string]int{}           // from|to|mode → count
	byPair := map[pair]map[string]int{} // (from,to) → distinct label → count
	order := []pair{}
	for _, r := range sys.Relationships {
		ek := r.From + "|" + r.To + "|" + modeWire(r.Mode)
		exact[ek]++
		p := pair{r.From, r.To}
		if byPair[p] == nil {
			byPair[p] = map[string]int{}
			order = append(order, p)
		}
		byPair[p][r.Label]++
	}
	var out []Finding
	for _, p := range order {
		labels := byPair[p]
		total := 0
		for _, n := range labels {
			total += n
		}
		if total < 2 {
			continue
		}
		// Exact duplicate on any (from,to,mode)?
		dup := false
		for _, r := range sys.Relationships {
			if r.From == p.from && r.To == p.to && exact[r.From+"|"+r.To+"|"+modeWire(r.Mode)] > 1 {
				dup = true
				break
			}
		}
		if dup {
			out = append(out, Finding{
				RuleID:   "SYS-REL-DUP",
				Severity: SeverityError,
				Message:  fmt.Sprintf("relationship %s → %s is declared more than once with the same mode; remove the exact duplicate edge.", p.from, p.to),
				Location: &Location{Section: fmt.Sprintf("relationship %s → %s", p.from, p.to)},
			})
		} else if len(labels) > 1 {
			out = append(out, Finding{
				RuleID:   "SYS-REL-DUP",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("relationship %s → %s is split across %d edges with different labels; aggregate them onto one edge with a \" | \"-joined label.", p.from, p.to, len(labels)),
				Location: &Location{Section: fmt.Sprintf("relationship %s → %s", p.from, p.to)},
			})
		}
	}
	return out
}

// dvChainFindings — DV-CHAIN-CONNECTED (warning). Each dynamic view's edges should form a
// connected chain rooted at a Client participant: every participant must be reachable by
// following the directed edges out of some Client-kind participant. An unrooted or
// disconnected call chain is a modeling smell (a participant nothing calls).
func dvChainFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	kindByID := make(map[string]projectstate.ComponentKind, len(sys.Components))
	for _, c := range sys.Components {
		kindByID[c.ID] = c.Kind
	}
	var out []Finding
	for i, dv := range sys.DynamicViews {
		if len(dv.Participants) <= 1 {
			continue
		}
		adj := map[string][]string{}
		for _, e := range dv.Edges {
			adj[e.From] = append(adj[e.From], e.To)
		}
		roots := []string{}
		for _, pid := range dv.Participants {
			if kindByID[pid] == projectstate.CompClient {
				roots = append(roots, pid)
			}
		}
		label := dvLabel(dv, i)
		if len(roots) == 0 {
			out = append(out, Finding{
				RuleID:   "DV-CHAIN-CONNECTED",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("dynamic view %q has no Client participant to root its call chain; a use-case call chain should originate at a Client.", label),
				Location: &Location{Ordinal: int64(i), Section: "dynamic view " + label},
			})
			continue
		}
		seen := map[string]bool{}
		stack := append([]string{}, roots...)
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[n] {
				continue
			}
			seen[n] = true
			stack = append(stack, adj[n]...)
		}
		var unreached []string
		for _, pid := range dv.Participants {
			if !seen[pid] {
				unreached = append(unreached, pid)
			}
		}
		if len(unreached) > 0 {
			sort.Strings(unreached)
			out = append(out, Finding{
				RuleID:   "DV-CHAIN-CONNECTED",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("dynamic view %q is not a connected chain from its Client root(s): %s unreachable via its edges.", label, strings.Join(unreached, ", ")),
				Location: &Location{Ordinal: int64(i), Section: "dynamic view " + label},
			})
		}
	}
	return out
}

// variationRefFindings — UC-VARIATION-REF (error). variationOf, when set, must resolve to
// an existing use-case id whose target is CORE. A nonCore use case must carry a non-empty
// rejectionReason. A core use case must NOT carry a variationOf (it is the base, not a
// permutation).
func variationRefFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindCoreUseCases {
		return nil
	}
	cuc, ok := draft.(*projectstate.CoreUseCases)
	if !ok || cuc == nil {
		return nil
	}
	coreIDs := map[projectstate.UseCaseID]bool{}
	for _, d := range cuc.Decisions {
		if d.UseCase.Classification == projectstate.ClassCore {
			coreIDs[d.UseCase.ID] = true
		}
	}
	var out []Finding
	for i, d := range cuc.Decisions {
		uc := d.UseCase
		label := uc.Name
		if label == "" {
			label = fmt.Sprintf("use case %d", i+1)
		}
		loc := &Location{Ordinal: int64(i), Section: "use case " + label}
		if uc.Classification == projectstate.ClassCore {
			if uc.VariationOf != nil && strings.TrimSpace(string(*uc.VariationOf)) != "" {
				out = append(out, Finding{
					RuleID:   "UC-VARIATION-REF",
					Severity: SeverityError,
					Message:  fmt.Sprintf("core use case %q declares a variationOf (%q); a core use case is a base, not a variation — clear variationOf or reclassify it nonCore.", label, string(*uc.VariationOf)),
					Location: loc,
				})
			}
			continue
		}
		// nonCore
		if uc.VariationOf == nil || strings.TrimSpace(string(*uc.VariationOf)) == "" {
			out = append(out, Finding{
				RuleID:   "UC-VARIATION-REF",
				Severity: SeverityError,
				Message:  fmt.Sprintf("nonCore use case %q has no variationOf; a nonCore use case must link to the core use case it permutes.", label),
				Location: loc,
			})
		} else if !coreIDs[*uc.VariationOf] {
			out = append(out, Finding{
				RuleID:   "UC-VARIATION-REF",
				Severity: SeverityError,
				Message:  fmt.Sprintf("nonCore use case %q has variationOf %q, which does not resolve to an existing CORE use case.", label, string(*uc.VariationOf)),
				Location: loc,
			})
		}
		if strings.TrimSpace(d.RejectionReason) == "" {
			out = append(out, Finding{
				RuleID:   "UC-VARIATION-REF",
				Severity: SeverityError,
				Message:  fmt.Sprintf("nonCore use case %q has an empty rejectionReason; state why it is not core.", label),
				Location: loc,
			})
		}
	}
	return out
}

// canonicalGlossaryCategories is the closed Four-Questions category set (ch. 4).
var canonicalGlossaryCategories = map[string]bool{"Who": true, "What": true, "How": true, "Where": true}

// glossaryFourQFindings — GLOSS-FOURQ. WARNING coverage: at least one term should cover
// each of Who / What / How / Where. ERROR: a term whose category is not one of the four
// canonical values.
func glossaryFourQFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindGlossary {
		return nil
	}
	g, ok := draft.(*projectstate.Glossary)
	if !ok || g == nil {
		return nil
	}
	var out []Finding
	counts := map[string]int{}
	for i, it := range g.Items {
		cat := strings.TrimSpace(it.Category)
		if !canonicalGlossaryCategories[cat] {
			out = append(out, Finding{
				RuleID:   "GLOSS-FOURQ",
				Severity: SeverityError,
				Message:  fmt.Sprintf("glossary term %q has non-canonical category %q; use one of Who|What|How|Where.", it.Term, it.Category),
				Location: &Location{Ordinal: int64(i), Section: "glossary term " + it.Term},
			})
			continue
		}
		counts[cat]++
	}
	for _, cat := range []string{"Who", "What", "How", "Where"} {
		if counts[cat] == 0 {
			out = append(out, Finding{
				RuleID:   "GLOSS-FOURQ",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("no glossary term covers the %q question; the Four Questions each want at least one term.", cat),
				Location: &Location{Section: "glossary"},
			})
		}
	}
	return out
}

// scrubbedIDFindings — SR-ID-UNIQUE (error). Every scrubbed requirement must carry a
// non-empty, unique id and a non-empty statement.
func scrubbedIDFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindScrubbedRequirements {
		return nil
	}
	sr, ok := draft.(*projectstate.ScrubbedRequirements)
	if !ok || sr == nil {
		return nil
	}
	var out []Finding
	seen := map[string]bool{}
	for i, it := range sr.Items {
		id := strings.TrimSpace(it.ID)
		loc := &Location{Ordinal: int64(i), Section: fmt.Sprintf("requirement %d", i+1)}
		if id == "" {
			out = append(out, Finding{
				RuleID:   "SR-ID-UNIQUE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("scrubbed requirement %d has an empty id; every requirement needs a stable non-empty id.", i+1),
				Location: loc,
			})
		} else if seen[id] {
			out = append(out, Finding{
				RuleID:   "SR-ID-UNIQUE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("scrubbed requirement id %q is duplicated; requirement ids must be unique.", id),
				Location: loc,
			})
		} else {
			seen[id] = true
		}
		if strings.TrimSpace(it.Statement) == "" {
			out = append(out, Finding{
				RuleID:   "SR-ID-UNIQUE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("scrubbed requirement %q has an empty statement.", it.ID),
				Location: loc,
			})
		}
	}
	return out
}

// opcCanonicalTopics maps a canonical ch.5 operational-concept topic to the substrings
// that evidence it appears among decisions[].topic.
var opcCanonicalTopics = []struct {
	name  string
	needs []string
}{
	{"topology", []string{"topology"}},
	{"sync/queued", []string{"sync", "queued"}},
	{"layering style", []string{"layering"}},
	{"state handling", []string{"state"}},
}

// opcTopicFindings — OPC-TOPIC-COVERAGE (info). Nudge when a canonical ch.5 topic
// (topology, sync/queued, layering style, state handling) is absent from decisions[].topic.
func opcTopicFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindOperationalConcepts {
		return nil
	}
	op, ok := draft.(*projectstate.OperationalConcepts)
	if !ok || op == nil {
		return nil
	}
	var topics []string
	for _, d := range op.Decisions {
		topics = append(topics, strings.ToLower(d.Topic))
	}
	joined := strings.Join(topics, " | ")
	var out []Finding
	for _, t := range opcCanonicalTopics {
		covered := false
		for _, need := range t.needs {
			if strings.Contains(joined, need) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, Finding{
				RuleID:   "OPC-TOPIC-COVERAGE",
				Severity: SeverityInfo,
				Message:  fmt.Sprintf("no operational-concept decision addresses %q; ch.5 expects topology, sync/queued, layering style, and state handling to be decided.", t.name),
				Location: &Location{Section: "operational concepts"},
			})
		}
	}
	return out
}

// ---- small shared helpers ----

func componentDisplayLabel(c projectstate.Component, i int) string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	if strings.TrimSpace(c.ID) != "" {
		return c.ID
	}
	return fmt.Sprintf("component %d", i+1)
}

func dvLabel(dv projectstate.DynamicView, i int) string {
	if strings.TrimSpace(dv.Title) != "" {
		return dv.Title
	}
	if strings.TrimSpace(dv.Key) != "" {
		return dv.Key
	}
	if strings.TrimSpace(dv.UseCaseID) != "" {
		return dv.UseCaseID
	}
	return fmt.Sprintf("dynamic view %d", i+1)
}

func modeWire(m projectstate.CallMode) string {
	b, err := m.MarshalJSON()
	if err != nil {
		return fmt.Sprintf("mode(%d)", int(m))
	}
	return strings.Trim(string(b), `"`)
}
