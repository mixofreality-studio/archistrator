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
// FIXME(file-layout): shared workflow-context helper — reachable from more than
// one workflow's call tree; cannot legally live in systemdesignmanager.go (the
// gate forbids workflow.Context funcs there). Placed in its first caller's file
// pending a controller ruling. See task-C5-C9-report.md.
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
// FIXME(file-layout): shared workflow-context helper — reachable from more than
// one workflow's call tree; cannot legally live in systemdesignmanager.go (the
// gate forbids workflow.Context funcs there). Placed in its first caller's file
// pending a controller ruling. See task-C5-C9-report.md.
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
// FIXME(file-layout): shared workflow-context helper — reachable from more than
// one workflow's call tree; cannot legally live in systemdesignmanager.go (the
// gate forbids workflow.Context funcs there). Placed in its first caller's file
// pending a controller ruling. See task-C5-C9-report.md.
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
// FIXME(file-layout): shared workflow-context helper — reachable from more than
// one workflow's call tree; cannot legally live in systemdesignmanager.go (the
// gate forbids workflow.Context funcs there). Placed in its first caller's file
// pending a controller ruling. See task-C5-C9-report.md.
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
		withdrawBranch := gf.readBackBranch()
		if _, err := wf.applyRecovering(ctx, in.ProjectID, withdrawBranch, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			return wf.Acts.DesignSessionWithdrawArtifactOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, withdrawBranch, toPSKind(in.ArtifactKind), notes)
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
				return wf.Acts.DesignSessionWithdrawArtifactOnBranch(ctx, projectstate.ProjectID(projectID), expected, "", toPSKind(kind), withdrawNotes)
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

// dispatch.go is the AGENTIC-PIVOT seam (D-MSD-Δ, systemDesignManager.md §0d). The
// drafting MECHANISM flips from a synchronous workerAccess call to an ASYNC
// dispatch → observe → read-back round-trip:
//
//   - DISPATCH  the Manager composes the Method-role prompt IN-MEMORY (never
//               persisted) and dispatches a claude-code-action DESIGN job via the
//               FROZEN constructionPipelineAccess.SubmitConstructionPipeline verb,
//               carrying {artifact_kind, design_prompt, target_branch,
//               prior_state_ref} on the additive PipelineSpec.DispatchInputs field
//               (C-WF-DESIGN input schema). The RA reserves + stamps
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
	// RunURL is the failed CI run's URL on a terminal-failure observation (QA F15 gap
	// 2b) — the "why" pointer the Manager threads onto the StageDraftFailed card. Empty
	// when the RA could not resolve it (or on a non-failure observation).
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

// dispatchDesignJobArgs bundles the dispatch inputs for the Activity boundary. The
// Manager's SEQUENCE composed Prompt in-memory (prompts.go); ArtifactKind + Target
// + Branch + PriorStateRef ride into the DispatchInputs map inside the Activity.
type dispatchDesignJobArgs struct {
	ProjectID     ProjectID
	ArtifactKind  ArtifactKind
	Target        dispatchTarget
	Prompt        string
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
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(a.ArtifactKind),
		dispatchInputDesignPrompt:  a.Prompt,
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
func (wf *workflows) dispatchAndObserve(ctx workflow.Context, args dispatchDesignJobArgs) (pipelineObservation, error) {
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
		return critique{Verdict: critiqueApprove}, nil
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
// FIXME(file-layout): shared workflow-context helper — reachable from more than
// one workflow's call tree; cannot legally live in systemdesignmanager.go (the
// gate forbids workflow.Context funcs there). Placed in its first caller's file
// pending a controller ruling. See task-C5-C9-report.md.
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
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
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

// The Manager OWNS the per-step prompt corpus (2026-05-29 rework §2.1; rework §6).
// The fixed Method sequence drives WHICH role-prompt the Manager sends at each
// step — that is the SystemDesignPhaseWorkflow volatility (the sequence), made
// explicit. The generic worker (workerAccess.generateTypedData[T]) holds NO
// Method-specific prompt corpus; the prompt + tool choice are the CALLER's. This
// file is where the deleted systemDesignEngine's prompts.go content belongs —
// owned by the sequence that drives it.
//
// Two role families:
//   - ARCHITECT-role draft prompts (one per Phase-1 artifact kind) — the worker
//     returns the typed <Kind> model.
//   - PM-role critique prompts (only for mission / glossary+scrubbed /
//     core-use-cases — the kinds the Method assigns a PM reviewer) — the worker
//     returns a typed Critique.
//
// Each prompt is plain text composed IN-MEMORY by the Manager and shipped as a
// DISPATCH INPUT to the claude-code-action DESIGN job (§0d.2 step 2 — never
// aiarch-persisted). It carries a role header, the target artifact kind, a pointer
// to the prior committed state BY PATH/KIND (the Action runs IN the user's repo and
// reads .aiarch/state/ directly — priors are NOT embedded as bytes), the Method
// doctrine for HOW to draft a good X, and (optionally) a feedback block woven in
// verbatim on a redraft. The Action drafts the typed JSON into .aiarch/state/ and
// the required CI validation check enforces its shape — the schema/DTO injection the
// old in-process worker needed is GONE (validation is the CI check, §0d.5).

const architectHeader = "You are the Architect agent drafting a typed Method artifact for an architecture project, following Juval Lowy's The Method to the letter. You author the project's Method state ONLY through the aiarch-state MCP tools — never hand-edit files and never run git. Read prior committed artifacts with getCommittedSlot, read your current draft (on an amendment) with getDraftSlot, submit your draft with putDraftModel (it validates the model and returns actionable errors if it is wrong — fix them and resubmit), and finish by calling publishDraft.\n"

const pmHeader = "You are the Product Manager agent critiquing a drafted Method artifact, following Juval Lowy's The Method. You work ONLY through the aiarch-state MCP tools — never hand-edit files and never run git. Read the drafted artifact with getDraftSlot and its prior committed predecessors with getCommittedSlot; record your verdict with setCritiqueVerdict; finish by calling publishDraft.\n"

// architectDraftPrompt assembles the architect-role draft prompt for the given
// Phase-1 artifact kind. It points the Action at the prior committed state by
// path/kind (NOT embedded bytes — the Action reads .aiarch/state/ in the repo),
// carries the Method drafting doctrine, and weaves in any rejection / PM-revision
// feedback. The ResearchInput pointer is named for the MISSION step. The composed
// prompt is the DESIGN job's design_prompt dispatch input.
func architectDraftPrompt(kind projectstate.ArtifactKind, proj projectstate.Project, feedback ReviewFeedback, reviewThread []projectstate.ReviewComment, amendment int) string {
	var b strings.Builder
	b.WriteString(architectHeader)
	fmt.Fprintf(&b, "Target artifact: %s. The design job already fixes which artifact you are drafting — putDraftModel writes it to the correct slot; you never choose a slot or a kind.\n", kind.WireName())

	// F38 AMENDMENT: this session REOPENS an already-committed artifact. State that the agent
	// is AMENDING the committed version (read it back with getDraftSlot) rather than drafting
	// from scratch, and that the reopening reasons are the OPEN review ledger entries below.
	if amendment > 0 {
		fmt.Fprintf(&b, "\nThis is an AMENDMENT (revision %d) of the already-COMMITTED %s. Read the committed version with getDraftSlot and REVISE it to address the reopening feedback — do NOT discard it and redraft from scratch. The specific reasons this artifact was reopened are the OPEN review-ledger comments listed below; address each and respond to it with respondToReviewComment.\n", amendment, kind.WireName())
	}

	// Per-kind priors: name the committed predecessor artifacts the Method draws on,
	// by kind (the Action reads them from .aiarch/state/project.json in the repo).
	switch kind {
	case projectstate.KindMission:
		writeResearch(&b, proj.Research)
	case projectstate.KindGlossary:
		writePriorsPointer(&b, "Mission")
	case projectstate.KindScrubbedRequirements:
		writePriorsPointer(&b, "Mission", "Glossary")
	case projectstate.KindVolatilities:
		writePriorsPointer(&b, "Mission", "Glossary", "ScrubbedRequirements")
	case projectstate.KindCoreUseCases:
		writePriorsPointer(&b, "Mission", "Glossary", "Volatilities")
	case projectstate.KindSystem:
		writePriorsPointer(&b, "Mission", "Glossary", "Volatilities", "CoreUseCases")
	case projectstate.KindOperationalConcepts:
		writePriorsPointer(&b, "Mission", "System")
	case projectstate.KindStandardCheck:
		writePriorsPointer(&b, "System", "OperationalConcepts")
	case projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution, projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution, projectstate.KindRiskModel, projectstate.KindSdpReview:
		// Phase-2 kinds never reach this Phase-1-only prompt assembler (see the
		// doc comment above); no priors pointer to add — same no-op as before
		// this switch was made exhaustive.
	}

	writeFeedback(&b, feedback)
	// REVIEW LEDGER (review-ledger §3): on a redraft, weave in every OPEN durable ledger
	// comment (with its stable id + anchor + anchor-text snapshot) and the response-carrier
	// contract, mirroring the PM-critique carrier language. The agent commits a per-comment
	// response back into the slot's reviewThread; the server reads it back and decides the
	// effective status. Empty on the first draft (no ledger).
	writeReviewLedger(&b, reviewThread)
	fmt.Fprintf(&b, "\nTask: %s\n", draftTask(kind))
	// CLOSED-ENUM discipline is no longer taught in the prompt (QA F36 was the enum-prose
	// stall): putDraftModel validates the model through the FULL server codec, so a
	// closed-enum field carrying free prose is rejected in-loop with an actionable error the
	// agent self-corrects — the enum wire-name dump the prompt used to carry is obsolete.
	// OPERATING-MODEL CONSTRAINT (founder ruling 2026-07-05): when the project is
	// archistrator-operated the deployment topology is CONSTRAINED to the platform
	// palette — the OperationalConcepts draft carries the deployment topology, so this
	// is where the constraint is enforced. Self-operated emits nothing (today's open
	// guidance is preserved verbatim).
	if kind == projectstate.KindOperationalConcepts {
		if c := operatingModelDeploymentConstraint(proj.OperatingModel); c != "" {
			b.WriteString("\n")
			b.WriteString(c)
		}
	}
	b.WriteString("\nSubmit the finished artifact with putDraftModel; if it reports validation errors, fix the model and submit again. When it is accepted (and every open review comment has a response), call publishDraft.\n")
	return b.String()
}

// operatingModelDeploymentConstraint returns the deployment-topology infrastructure
// constraint the OperationalConcepts draft prompt carries for the project's operating
// model (founder ruling 2026-07-05, from live QA — the gtdapp deployment artifact
// drafted an arbitrary AWS EKS/RDS/CloudFront topology).
//
//   - archistrator-operated: archistrator OPERATES the app on the shared platform, so
//     the design is CONSTRAINED to the archistrator-platform palette ONLY (CNPG
//     Postgres, Temporal, Keycloak, the otel stack, deployed to the platform k8s
//     cluster via ArgoCD at software/k8s) and FORBIDDEN bespoke cloud (AWS/GCP/Azure).
//   - self-operated (the default): the customer runs the app in their OWN infra, so
//     today's OPEN guidance stands — emit nothing extra.
func operatingModelDeploymentConstraint(m projectstate.OperatingModel) string {
	if m.OrDefault() != projectstate.OperatingModelArchistratorOperated {
		return ""
	}
	return "OPERATING MODEL — ARCHISTRATOR-OPERATED (platform-constrained deployment). " +
		"This project is OPERATED BY ARCHISTRATOR on the shared platform, so the deployment topology is CONSTRAINED to the archistrator-platform infrastructure ONLY. " +
		"Model the deployment using EXACTLY these platform building blocks and do NOT introduce any bespoke or third-party cloud infrastructure:\n" +
		"- Data / persistence: CloudNativePG (CNPG) Postgres — the framework-go-infrastructure-postgres module. Model every relational Resource as a CNPG Postgres cluster infrastructureNode.\n" +
		"- Workflows / durable execution: Temporal — the framework-go-infrastructure-temporal module (the SHARED platform Temporal at software/k8s/shared/temporal). Do NOT model a bespoke queue or worker pool.\n" +
		"- Authentication / identity: Keycloak — the framework-go-infrastructure-keycloak module (the archistrator auth platform lib, software/k8s/argocd/auth).\n" +
		"- Observability: the OpenTelemetry stack — the framework-go-infrastructure-otel module.\n" +
		"- Deploy target: the platform Kubernetes cluster via the ArgoCD stack at software/k8s (namespaces/apps under k8s/argocd/applications). deliveryStyle MUST be cloud; every container is a Kubernetes Deployment in the platform cluster and every infrastructureNode names the exact framework-go-infrastructure-* module above.\n" +
		"FORBIDDEN for this operating model: AWS (RDS, EKS, ECS, CloudFront, S3, Lambda), GCP, Azure, or any other bespoke / self-managed / third-party-managed cloud infrastructure — those are legitimate ONLY for self-operated projects. If a Resource needs a database it is CNPG Postgres; if it needs workflows it is Temporal; if it needs auth it is Keycloak; if it needs telemetry it is the otel stack."
}

// pmCritiquePrompt assembles the PM-role critique prompt for a drafted artifact.
// Only mission / glossary+scrubbed / core-use-cases route through PM-critique (the
// kinds the Method assigns a PM reviewer). The PM reads the just-committed draft from
// .aiarch/state/ and either ratifies it (Approve) or records concrete revision
// guidance (Revise); Revise loops back to the architect-role draft BEFORE the human
// gate. The composed prompt is the critique DESIGN job's design_prompt dispatch input.
func pmCritiquePrompt(kind projectstate.ArtifactKind, draft projectstate.ArtifactModel) string {
	var b strings.Builder
	b.WriteString(pmHeader)
	fmt.Fprintf(&b, "Artifact under review: %s (read its just-drafted model with getDraftSlot)\n", kind.WireName())
	b.WriteString("\nTask: as the Product Manager, ratify the draft (approve) or request a concrete revision (revise, with notes naming the revision the architect should make). Ratify only what faithfully serves the business; the human makes the final commit decision.\n")
	// Per-kind critique doctrine — kept in lockstep with draftTask so the
	// draft<->critique loop is CONVERGENT (QA finding F27, founder ruling 2026-07-05).
	// For the Mission the critique enforces exactly what the mission draft prompt now
	// instructs: business/user language only, no component/architecture terminology,
	// no pre-decided decomposition (that is derived later from volatility analysis).
	if kind == projectstate.KindMission {
		b.WriteString("\nMission doctrine you MUST enforce: the mission and vision must describe the BUSINESS CAPABILITY and USER-FACING VALUE in business and user language only. REVISE the draft if it uses the words component, module, service, subsystem, layer, or any other system-architecture / software-decomposition terminology, or if it asserts or implies any breakdown of the system into parts — the structural boundaries are derived LATER from volatility analysis, so pre-deciding a decomposition in the mission is a defect to send back. Do NOT ask the architect to ADD component or architecture language; that is exactly what must be kept out.\n")
	}
	// CRITIQUE READ-BACK CONTRACT (D-MSD-Δ amendment). The PM-critique job does NOT rewrite
	// the artifact model. It records its verdict through setCritiqueVerdict — the first-class
	// carrier the Manager reads back — never touching the architect's model or notes.
	b.WriteString("\nRecord your verdict with setCritiqueVerdict: verdict is exactly \"approve\" or \"revise\". On \"revise\", give concrete revision notes naming the change the architect should make; on \"approve\" leave the notes empty. Do NOT rewrite the model. A verdict is REQUIRED. Then call publishDraft to commit it.\n")
	return b.String()
}

// activityDiagramGuide teaches the architect role HOW TO COMPOSE a well-formed UML
// activity diagram from the typed node/edge model — not just the per-field shape
// the JSON Schema already carries. It is woven into the Core Use Cases draft prompt.
// The rules mirror the artifactValidationEngine's UC-ACTDIAG checks, so the model is
// told exactly what the machine will reject (decision must branch >=2 and reconverge
// at a merge; fork is unguarded concurrency that joins; guards only on decisions).
// No backticks appear inside — JSON examples use their natural double quotes — so
// this stays a single raw string literal.
const activityDiagramGuide = `ACTIVITY DIAGRAM: EVERY use case — CORE and SUPPORTING (nonCore) alike — MUST carry a NON-EMPTY "activity": a WELL-FORMED UML activity diagram, a graph of "nodes" (each {ref, kind, label, roleName, linkedActor, linkedComp}) and "edges" (each {from, to, kind, guard}). There is NO "purely linear, so leave it null" exemption — a use case with a null or empty "activity" (missing "nodes" or "edges") is an INCOMPLETE DRAFT and will be rejected. At an ABSOLUTE MINIMUM the diagram has a start node, at least one action node, and an end node wired start -> action -> end; a use case that branches or runs steps concurrently adds decision/merge or fork/join per the rules below. Walk the use case's real flow — do not stub a placeholder one-action diagram to satisfy the rule when the use case genuinely has steps. NEVER emit a bare string for "activity" — it is always a non-empty object with "nodes" and "edges".

IDENTITY BY NAME (no ids): you NEVER emit any opaque id or uuid. Give each node a short "ref" slug of your own (e.g. "n1", "n2") UNIQUE within the diagram; edges reference nodes by that "ref" in "from"/"to". "linkedActor" (optional) is an actor's ROLE name from this use case; "linkedComp" (optional) is a System component NAME. The server resolves all of these by name.

Node kinds and their edge cardinality:
- start: one per diagram; 0 incoming, exactly 1 outgoing.
- action: a step; 1 incoming, 1 outgoing.
- decision: a CHOICE; 1 incoming, >=2 outgoing.
- merge: rejoins a decision's alternative branches; >=2 incoming, 1 outgoing.
- fork: splits into CONCURRENT paths; 1 incoming, >=2 outgoing.
- join: synchronizes concurrent paths; >=2 incoming, 1 outgoing.
- end: a final node; >=1 incoming, 0 outgoing.
Put every node in its business-role swim-lane via "roleName" (e.g. "Customer", "Trusted System") — a business role or area of interest, NOT a Method layer or subsystem name.

Edge kinds:
- guardedFlow: carries a "guard" condition; used ONLY on the outgoing edges of a decision.
- controlFlow: no guard (set "guard" to ""); EVERY other edge, including ALL fork outgoing edges.

Composition rules you MUST follow (a violation is rejected and redrafted):
0. EVERY use case has a non-empty "activity" with EXACTLY ONE start node (0 incoming, 1 outgoing), at least ONE action node, and at least ONE end node — a diagram-less or node-less use case is an incomplete draft. This is NON-NEGOTIABLE for core use cases and equally REQUIRED for supporting (nonCore) ones; never leave "activity" null.
1. A decision is a CHOICE: it MUST have >=2 outgoing guardedFlow edges, each with a distinct, mutually-exclusive guard; give exactly ONE edge the guard "[else]" for the remaining case. Its branches MUST reconverge at a merge node before the flow continues — a branch must not run straight into the next step or dangle.
2. A fork is CONCURRENCY (not a choice): >=2 outgoing controlFlow (UNguarded) edges, ALL of which run; the concurrent paths MUST reconverge at a join. Never put a guard on a fork edge.
3. guardedFlow edges originate ONLY from decision nodes; every other node's outgoing edges are controlFlow.
4. A LOOP is a merge loop-head -> ...body... -> a decision whose "[repeat]" guarded edge BACK-EDGES to the loop-head merge and whose "[else]" guarded edge exits.
Decision/merge model an ALTERNATIVE (exactly one branch taken); fork/join model CONCURRENCY (all paths taken) — do not confuse them.

Worked examples (each node carries your own short "ref" slug — NOT a uuid; edges reference those refs):

if/else — a decision's two branches reconverge at a merge:
{"nodes":[{"ref":"n1","kind":"decision","label":"Is the item actionable?","roleName":"Trusted System"},{"ref":"n2","kind":"action","label":"Create next step and assign context","roleName":"Trusted System"},{"ref":"n3","kind":"action","label":"File or incubate item","roleName":"Trusted System"},{"ref":"n4","kind":"merge","label":"","roleName":"Trusted System"}],"edges":[{"from":"n1","to":"n2","kind":"guardedFlow","guard":"[actionable]"},{"from":"n1","to":"n3","kind":"guardedFlow","guard":"[else]"},{"from":"n2","to":"n4","kind":"controlFlow","guard":""},{"from":"n3","to":"n4","kind":"controlFlow","guard":""}]}

fork/join — two concurrent paths synchronize:
{"nodes":[{"ref":"n1","kind":"fork","label":"","roleName":"Marketplace"},{"ref":"n2","kind":"action","label":"Search the registry","roleName":"Marketplace"},{"ref":"n3","kind":"action","label":"Notify the tradesman","roleName":"Tradesman"},{"ref":"n4","kind":"join","label":"","roleName":"Marketplace"}],"edges":[{"from":"n1","to":"n2","kind":"controlFlow","guard":""},{"from":"n1","to":"n3","kind":"controlFlow","guard":""},{"from":"n2","to":"n4","kind":"controlFlow","guard":""},{"from":"n3","to":"n4","kind":"controlFlow","guard":""}]}

while-loop — a decision back-edges to the loop-head merge:
{"nodes":[{"ref":"n1","kind":"merge","label":"","roleName":"Trusted System"},{"ref":"n2","kind":"action","label":"Process the next item","roleName":"Trusted System"},{"ref":"n3","kind":"decision","label":"More items?","roleName":"Trusted System"},{"ref":"n4","kind":"end","label":"","roleName":"Trusted System"}],"edges":[{"from":"n1","to":"n2","kind":"controlFlow","guard":""},{"from":"n2","to":"n3","kind":"controlFlow","guard":""},{"from":"n3","to":"n1","kind":"guardedFlow","guard":"[more]"},{"from":"n3","to":"n4","kind":"guardedFlow","guard":"[else]"}]}`

// draftTask returns the per-kind task instruction — the Method doctrine for HOW to
// draft a good artifact of this kind, distilled from Juval Lowy's The Method (the
// the-method-* skills). The schema (draftSchema) already fixes the SHAPE; this prose
// teaches the architect role the THINKING the kind demands so the draft is sound,
// not merely well-typed.
func draftTask(kind projectstate.ArtifactKind) string {
	switch kind {
	case projectstate.KindMission:
		return "Produce the mission from the research corpus. The vision is ONE terse sentence naming the future the system creates. First distill the 2-3 business pillars that DIFFERENTIATE this system from competitors; ground the vision, mission, and objectives in those. The mission narrative describes the BUSINESS CAPABILITY and USER-FACING VALUE of the end-to-end workflow — why it matters, and what outcome or trust it produces for the user — NOT a feature list. Write it PURELY in business and user language: you MUST NOT use the words component, module, service, subsystem, layer, or any system-architecture / software-decomposition terminology, and you MUST NOT assert or imply any breakdown of the system into parts. The structural boundaries are derived LATER from volatility analysis in the Structure artifact — pre-deciding a decomposition here is a defect. Each objective is a numbered, measurable BUSINESS outcome (not a feature deliverable)."

	case projectstate.KindGlossary:
		return "Extract the system's ubiquitous-language terms, each categorised by the Four Questions: Who interacts with the system, What is required of it, How (the business activity), Where (state lives). Define each term crisply in business language with NO solution/implementation wording. These terms are the shared vocabulary every later artifact must reuse verbatim."

	case projectstate.KindScrubbedRequirements:
		return "Scrub every solution out of the requirements and emit the underlying NEEDS only. A need states what the business requires; a solution states how to build it — strip the how. 'Users log in with OAuth' is a solution; 'the system authenticates users' is the need. Each item must be solution-free and traceable to the mission."

	case projectstate.KindVolatilities:
		return "Identify the areas of VOLATILITY the architecture must encapsulate, along TWO independent axes. Axis sameCustomerOverTime: for each requirement ask 'what in THIS customer's business will change in 1, 3, 5 years?'. Axis allCustomersAtOneTime: ask 'do ALL customers do this identically today, or do markets/regulations/languages/customer-types vary?'. Encapsulate the open-ended (VOLATILE); REJECT anything a simple conditional handles (that is merely VARIABLE). Reject by-reflex 'Logging'/'Reporting' blocks with no business volatility, speculative 'might-need-someday' encapsulation, and nature-of-the-business items competitors do identically. Aim for ~6-15 entries, each with a rationale paragraph and its axis."

	case projectstate.KindCoreUseCases:
		return "Select the CORE use cases by ABSTRACTION, not by listing what the customer asked for. For each candidate ask: does this capture the ESSENCE of the business (what differentiates it, what creates value), or is it a permutation/utility (onboarding, payment, account admin)? Could a single higher abstraction — often a NEW name not in the customer's vocabulary — subsume several raw use cases? Target 2-6 core use cases; if you have more than 6 you have not abstracted enough. Sanity check: a one-slide brochure for the system would have roughly this many bullets. Record each rejected permutation with its rejection reason and link it to the core it permutes by setting its \"variationOf\" to that core use case's NAME (exactly as you wrote it).\n\n" +
			"IDENTITY BY NAME: every use case and actor is identified by its human-readable NAME — you do NOT emit any id. Use case names must be UNIQUE; actor roles must be unique within a use case. Reference the core use case in \"variationOf\" by its name; the server assigns and resolves all internal ids.\n\n" +
			activityDiagramGuide

	case projectstate.KindSystem:
		return "Decompose the system by VOLATILITY into layered components, then validate by drawing the call chains. Bin each volatility with the Four Questions: Who -> Client, What -> Manager, How(activity) -> Engine, How(resource) -> ResourceAccess, Where(state) -> Resource, cross-cutting reuse -> Utility. Each component encapsulates EXACTLY ONE volatility and sits in EXACTLY ONE layer; Component.Layer MUST equal Component.Kind. Obey closed layering: calls go downward only, never upward, never sideways except queued Manager->Manager. REJECT functional decomposition (components named after features) and domain decomposition (components named after entities) — name components after the volatility they hide. Keep it small: order-of-magnitude ~10 components, Managers <=5, fewer Engines than Managers. Emit one dynamicView per use case — CORE and SUPPORTING (nonCore) variations ALIKE — tracing its call chain (exactly one Manager entered from the Client; every edge labelled in the destination layer's vocabulary, not infrastructure terms). FOUNDER EXTENSION (beyond Löwy, who validates only the core): EVERY use case in the committed CoreUseCases set MUST carry its own dynamic view — you may NOT ship the architecture with any use case (core or a nonCore variation) left without a call chain. If a use case cannot be drawn cleanly, the DECOMPOSITION is wrong — fix the components, not the use case.\n\nIDENTITY BY NAME: every component is identified by its NAME — you do NOT emit any id, and you do NOT emit a component's layer (it is fixed by its kind and the server derives it). Component names must be UNIQUE. In \"relationships\" and a dynamic view's \"participants\"/\"edges\", reference components by their NAME (the from/to are component names). In each dynamic view set \"useCase\" to that use case's NAME (exactly as it appears in the CoreUseCases context — core OR nonCore) — do NOT emit a view key; the server derives it. The server resolves every name to its internal id and rejects any name that does not match a component or use case."

	case projectstate.KindOperationalConcepts:
		return "Document the runtime/operational decisions that bring the static architecture to life: communication topology (direct vs message bus), manager-execution infrastructure (in-process vs durable workflow engine), the sync-vs-queued boundary for each cross-component edge (prefer queued for Manager<->Manager), and every pub/sub event (only Clients and Managers may publish or subscribe). Each decision MUST cite the numbered mission objective it serves and state its cost; if a decision cannot be justified against an objective, cut it as gratuitous complexity.\n\n" +
			"Then populate the deployment topology in C4-container shape. First declare the system's deliveryStyle (cloud, local, or both). The set of deployment environments is DERIVED from it and a test profile is ALWAYS present: cloud -> {cloud, test}; local -> {local, test}; both -> {cloud, local, test}. Emit exactly that set of environments — no more, no fewer. Next declare the top-level \"containers\" array — the deployable UNITS, not the components — each with a \"key\", \"name\", \"technology\", \"description\", and \"components\" listing the exact NAMES of the System components it packages (e.g. an application-server container packages the Managers, Engines, ResourceAccess, and Utilities; a web/SPA container packages the web Client). Every CODE component — every Client, Manager, Engine, and ResourceAccess, plus every Utility — MUST be packaged into EXACTLY ONE container; none may be left out and none may appear in two containers. Resources are NOT container members — they are deployment INFRASTRUCTURE, never packaged: model each Resource (database, queue, external API) as an infrastructureNode (a self-describing name/technology/description) or, for a genuinely external third-party system, as a softwareSystemInstance. The SAME logical Resource may be realized differently per environment (a managed Postgres cluster in cloud vs a local docker/sqlite instance in test) — that per-profile realization detail belongs on the infrastructure node, never on the abstract Resource. Each environment nests deploymentNodes (e.g. cluster -> namespace -> deployment) whose containerInstances reference a declared container BY ITS \"containerKey\" (not a component name) and set an \"instances\" integer for its replica count (e.g. 2); put infrastructureNodes and softwareSystemInstances on whichever deploymentNode they run alongside. CROSS-PROFILE INVARIANT: operating mode is configuration, not architecture — the set of deployed CONTAINERS MUST be IDENTICAL across the cloud and local environments (the underlying infrastructure MAY legitimately differ per profile — a managed database in cloud vs a local one in test is exactly the point of separate environments, not a violation). The test environment MUST instance EVERY container so every code component is covered; represent external systems and resources there as stubs. Reference containers in a deploymentNode's containerInstances by \"containerKey\", and reference System components inside a container's \"components\" list by their NAME exactly as they appear in the System context — you do NOT emit any id for either; the server resolves both by name/key."

	case projectstate.KindStandardCheck:
		return "Walk the App C design standard, but ONLY the items checkable at THIS system-design gate — the design directives and the System Design guideline section. Check the design directives: avoid functional decomposition, decompose based on volatility, provide a composable design, treat features as aspects of integration (not as building blocks), design iteratively while building incrementally, and — where the design makes an architectural choice that had real alternatives — drive that decision with options. Then walk the System Design guideline section: capture behaviour not functionality, every component traces to a volatility (no functional or domain decomposition), cardinality limits respected (Managers a handful, fewer Engines than Managers), volatility decreases and reuse increases top-down, Managers do no I/O, closed-layer rules respected (no calling up, sideways, or skipping layers), and the interaction don'ts (one Manager per client call chain; no queued or pub/sub from the wrong layers). For each IN-SCOPE item emit pass (the design satisfies it), waived (with a concrete justification for why THIS system consciously accepts the exception — e.g. a cardinality guideline deliberately exceeded for a documented reason), or fail (the design violates it). A waiver without a real justification is itself a fail.\n\n" +
			"SCOPE — do NOT walk the project-design or project-tracking parts of the standard at this gate. The project directives (design the project to build the system, build along the critical path, be on time throughout), the Project Design guideline sections (general, staffing, integration, estimations, network, time-and-cost, risk), and the Project Tracking guideline section are OUT OF SCOPE at the system-design gate — no project design exists yet, so there is nothing to check them against. Do NOT emit them at all, and in particular do NOT emit them as waived: waived is reserved for genuine, justified exceptions to IN-SCOPE system-design items, NOT for phase-inapplicable items (marking an out-of-scope item 'waived: no project design exists yet' pollutes the waiver as a conscious-exception signal). Those items are checked at their own Phase-2 SDP gate (the project-design standard check), so nothing is lost by leaving them out here."

	case projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution, projectstate.KindCompressedSolution,
		projectstate.KindDecompressedSolution, projectstate.KindRiskModel, projectstate.KindSdpReview:
		// Phase-2 kinds are never drafted through this Phase-1-only task table;
		// same generic fallback as the default below.
		return "draft the artifact."

	default:
		return "draft the artifact."
	}
}

// kindHasPMCritique reports whether the Method assigns a PM reviewer to this kind
// (mission / glossary+scrubbed / core-use-cases — rework §2.1, §6.6). The
// architect-owned steps (volatilities, architecture, standard-check) skip PM
// critique entirely.
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

// writePriorsPointer names the committed predecessor artifacts (by kind) the Method
// step draws on, pointing the Action at .aiarch/state/project.json rather than
// embedding model bytes (§0d.2 step 2 — the Action runs in the repo and reads the
// priors by path/kind). An empty list writes nothing.
func writePriorsPointer(b *strings.Builder, kinds ...string) {
	if len(kinds) == 0 {
		return
	}
	fmt.Fprintf(b, "Read these prior committed artifacts as context with getCommittedSlot: %s.\n", strings.Join(kinds, ", "))
}

// writeResearch POINTS the mission-draft prompt at the Phase-1 research corpus
// committed in .aiarch/state/project.json rather than INLINING the source content
// (rework §2.6 / §8; QA finding F11). The corpus is already committed at the JSON path
// .research.Sources[] on the checked-out project state (the Action runs IN the repo and
// prior_state_ref is always empty ⇒ github.ref, the default branch, which carries the
// committed research from the very first SetResearchInput). Inlining a book-sized corpus
// blew both the Temporal workflow-payload budget (TMPRL1103) and GitHub's 64KB
// workflow_dispatch input cap (422 ContractMisuse), making system design impossible. We
// UNIFORMLY point — never inline, no size cliff — listing only each source's short TITLE
// so the drafting agent knows what is there and can read the full text by title. An empty
// corpus is skipped (IsZero guard preserved).
func writeResearch(b *strings.Builder, research projectstate.ResearchCorpus) {
	if research.IsZero() {
		return
	}
	// The corpus never rides this prompt (it can be book-sized). The agent enumerates the
	// sources with listResearchSources and reads each one's full text with getResearchSource
	// — so no path list and no content is inlined here.
	b.WriteString("\nResearch corpus (the raw material for the mission): call listResearchSources to see every source, then getResearchSource to read each one's full text. Do NOT expect any research content inline in this prompt.\n")
}

// writeFeedback appends a revision-feedback block weaving in the architect's
// free-text Notes (or PM-critique / validation notes) AND each JSONPath-anchored
// comment as a "- at {jsonPath}: {text}" guidance line beneath the notes. An empty
// ReviewFeedback (no notes, no comments) writes nothing. The JSONPath is carried
// verbatim — the server does not evaluate it (systemDesignManager.md §3.2).
func writeFeedback(b *strings.Builder, feedback ReviewFeedback) {
	notes := strings.TrimSpace(feedback.Notes)
	comments := nonEmptyComments(feedback.Comments)
	if notes == "" && len(comments) == 0 {
		return
	}
	b.WriteString("\nThis is a revision. Address the following feedback:\n")
	if notes != "" {
		fmt.Fprintf(b, "%s\n", notes)
	}
	for _, c := range comments {
		fmt.Fprintf(b, "- at %s: %s\n", c.JSONPath, strings.TrimSpace(c.Text))
	}
}

// writeReviewLedger weaves the OPEN durable review-ledger comments into a redraft prompt
// and states the response-carrier contract the drafting agent must honor (review-ledger §3).
// It mirrors the PM-critique carrier (pmCritiquePrompt): the agent does NOT invent or reorder
// comments — it commits a per-comment "response" (and a proposed "addressed" status) back onto
// the SAME slot's "reviewThread" array in .aiarch/state/project.json, matched by the stable
// comment "id". The server, not the agent, decides the effective status on read-back (an empty
// response keeps a comment open — so a comment the agent cannot honestly address must be left
// with an empty response or a reasoned pushback the human then waives). Nothing is written when
// no comment is open (the common first-draft case).
func writeReviewLedger(b *strings.Builder, thread []projectstate.ReviewComment) {
	var open []projectstate.ReviewComment
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentOpen && strings.TrimSpace(c.Text) != "" {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return
	}
	b.WriteString("\nThis artifact has OPEN reviewer comments in its durable review ledger (read them with getReviewThread). For EACH open comment listed below you MUST: (1) revise the draft to address it; and (2) call respondToReviewComment with the matching comment id and your response — how you addressed it, or a concise reasoned pushback if you disagree. A comment whose response you leave empty STAYS OPEN and blocks approval — so respond to every one.\n")
	for _, c := range open {
		anchor := c.Anchor
		if strings.TrimSpace(anchor) == "" {
			anchor = "(whole artifact)"
		}
		if strings.TrimSpace(c.AnchorText) != "" {
			fmt.Fprintf(b, "- comment %s at %s (%q): %s\n", c.ID, anchor, c.AnchorText, strings.TrimSpace(c.Text))
		} else {
			fmt.Fprintf(b, "- comment %s at %s: %s\n", c.ID, anchor, strings.TrimSpace(c.Text))
		}
	}
}

// nonEmptyComments filters out anchored comments with no text — defensive against
// a wire payload that sent an empty comment.
func nonEmptyComments(comments []AnchoredComment) []AnchoredComment {
	out := comments[:0:0]
	for _, c := range comments {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		out = append(out, c)
	}
	return out
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
