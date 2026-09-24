package systemdesign

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
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

// critiqueMissingVerdictDiagnosticFor renders the neutral human-facing reason surfaced
// as the StageDraftFailed FailureReason when a critique job committed no verdict,
// naming the actual critic (PM, or the architect self-critique) honestly.
func critiqueMissingVerdictDiagnosticFor(critic ActiveRole) string {
	return "the " + criticLabel(critic) + " job committed no verdict"
}

// criticLabel renders the human noun phrase for a critique round's critic: the
// PM-critique for the business-alignment kinds, the architect self-critique for the
// architecture (critiqueCriticFor). Used in gate copy and logs so an
// architect-critiqued kind is never presented as PM-reviewed.
func criticLabel(critic ActiveRole) string {
	if critic == ActiveRoleArchitect {
		return "architect self-critique"
	}
	return "PM-critique"
}

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
	// decision (PM-P2-4), derived from the SubmitReviewDecision caller's security.Principal.
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

// autoApproverVibes is the Approver provenance label recorded on a vibes-policy AUTO-approve at
// the design review gate (the vibes autogate) — it distinguishes a policy-driven approval from
// a human reviewer's in the commit's approvedBy provenance.
const autoApproverVibes = "policy:vibes"

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

	proj, headVersion, feedback, initErr := wf.beginCoAuthorSession(ctx, in, state)
	if initErr != nil {
		return coAuthorUnknown, initErr
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
	// reviewable draft (dispatch → observe → read-back, plus the critique round for
	// critiqued kinds — see critiqueCriticFor), stages it, suspends on the human gate, and acts on the
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
		// ROUND LEDGER (stage 3 task 6): the draft has entered awaitingReview, so the review
		// occurrence it is about to be judged in exists — open the round with the roster the
		// engine computed and the subject it judges, and land the critic's verdict on it. A
		// redraft's re-entry opens a NEW round. Inert off the fence.
		wf.openDesignRound(ctx, in, gf, reviewRound, state)

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

// beginCoAuthorSession runs the deterministic session preamble for the spine: the
// architect-critique version gate, the Step-1 head-state read, the committed-model
// captures, and the initial feedback seed. Extracted verbatim from the workflow body —
// the ordering of the GetVersion marker relative to the readProject Activity is
// replay-load-bearing and must not change.
func (wf *workflows) beginCoAuthorSession(ctx workflow.Context, in coAuthorInput, state *coAuthorState) (projectstate.Project, projectstate.Version, ReviewFeedback, error) {
	// REPLAY SAFETY (system-architect-critique): resolve the architect self-critique
	// version gate ONCE, at session start, for the architect-critiqued kind. Placing the
	// GetVersion HERE (not at the critique point) is what keeps an execution in flight at
	// deploy time — e.g. the running gtdapp:5 System redraft — on the OLD no-critique
	// command sequence for its whole run: its history has no marker at this point, so
	// replay resolves DefaultVersion, while every post-deploy session records v1 and runs
	// the critique round. Conditioned on the deterministic input kind so other kinds'
	// histories stay marker-free.
	if critic, ok := critiqueCriticFor(toPSKind(in.ArtifactKind)); ok && critic == ActiveRoleArchitect {
		state.architectCritiqueEnabled = workflow.GetVersion(ctx, "system-architect-critique", workflow.DefaultVersion, 1) >= 1
	}

	// REPLAY SAFETY (design-vibes-autogate): resolve the vibes-autogate version gate ONCE, at
	// session start, UNCONDITIONALLY (every kind can auto-approve under a vibes policy). Same
	// discipline as the critique gate above: a session in flight at deploy time has no marker
	// here, so replay resolves DefaultVersion → the autogate stays OFF for its whole run (it
	// waits on the human gate exactly as before), while every post-deploy session records v1
	// and may auto-approve. The injection point is awaitReviewGate.
	state.vibesAutogateEnabled = workflow.GetVersion(ctx, "design-vibes-autogate", workflow.DefaultVersion, 1) >= 1

	// REPLAY SAFETY (design-round-ledger): resolve the round-ledger fence ONCE, here, for
	// the same reason and with the same discipline. Every write it gates is a Temporal
	// Activity, so a session suspended at its gate when this deploys has a history with no
	// marker at this point: it replays DefaultVersion, writes no round for its whole run,
	// and its recorded command sequence is unchanged. Sessions started after the deploy
	// resolve v1 and dual-write.
	state.roundLedgerEnabled = workflow.GetVersion(ctx, changeDesignRoundLedger, workflow.DefaultVersion, 1) >= 1

	// Carry expectedVersion forward in workflow state (read-your-writes; D-PA §6).
	var headVersion projectstate.Version

	// Step 1: read the project head-state once (prior typed models + ResearchInput + version).
	var proj projectstate.Project
	if p, err := wf.readProject(ctx, in.ProjectID); err != nil {
		if !isReadNotFound(err) {
			return projectstate.Project{}, headVersion, ReviewFeedback{}, err
		}
		proj = projectstate.Project{ID: projectstate.ProjectID(in.ProjectID)}
	} else {
		proj = p
		headVersion = p.Version
		// The round ledger is a MAIN-side write, so it starts from main's tip (see
		// coAuthorState.ledgerVersion). A NotFound leaves it 0, which applyRecovering's
		// re-read resolves on the first write.
		state.ledgerVersion = p.Version
	}

	// VIBES AUTOGATE (F-R3 vibes-everywhere, founder-ratified): snapshot the review policy at
	// session start — a vibes preset auto-approves this session's drafts at the review gate
	// (awaitReviewGate), honoring ReviewPolicy exactly like construction. Snapshot-at-start
	// mirrors construction: a policy change applies to the NEXT session, not one in flight.
	//
	// The review engine owns the autogate RULE now (spec 2026-09-20 §5.4): it is asked
	// once, here, with this artifact's design activity type and lifecycle phase, and its
	// answer is the same one the inline Preset check gave — vibes auto-approves, every
	// other preset (including the unset legacy value) holds for the human. A refusal
	// reads as "a human must decide", the safe arm for a design gate. A pure engine call
	// emits no commands, so the "design-vibes-autogate" GetVersion fence above still
	// governs replay and no new fence is needed.
	designType, lifecyclePhase := designActivityFor(toPSKind(in.ArtifactKind))
	gateSet, perr := review.NewReviewEngine().ProposeReviews(fweng.Context{Context: context.Background()},
		review.ReviewChange{ActivityID: string(in.ProjectID)}, designType, lifecyclePhase, "",
		engineReviewPolicy(proj.ReviewPolicy), false, nil)
	if perr != nil {
		workflow.GetLogger(ctx).Error("review engine refused to decide the design gate; the session holds for a human",
			"projectId", string(in.ProjectID), "artifactKind", artifactKindString(in.ArtifactKind), "err", perr.Error())
	}
	state.policyAutoApprove = perr == nil && !gateSet.RequiresHuman
	// The SAME engine answer is the round's ROSTER (stage 3 task 6). Snapshot it here rather
	// than asking again at the gate: a second call would be a second chance to disagree with
	// the gate decision the session is already running under. A refusal leaves it empty — a
	// round with no roster is the honest record of a gate the engine could not staff.
	if perr == nil {
		state.roundReviewers = designRoundReviewers(gateSet)
	}

	// Capture the committed CoreUseCases once (founder extension, 2026-07-05): the
	// KindSystem read-back check needs it to flag a System draft that leaves any
	// committed use case without a dynamic view (USECASE-DYNAMIC-MISSING). Read
	// deterministically from the head-state proj, so it is replay-safe.
	if cuc, ok := proj.CoreUseCases.Model.(*projectstate.CoreUseCases); ok {
		state.committedCoreUseCases = cuc
	}
	// Capture the committed Volatilities the same way (F10 gate lint, QA amendment
	// 2026-07-17): the KindSystem read-back check needs it to flag committed
	// volatilities the System draft neither claims nor dispositions
	// (SYS-VOLATILITY-COVERAGE). Deterministic head-state read — replay-safe.
	if vol, ok := proj.Volatilities.Model.(*projectstate.Volatilities); ok {
		state.committedVolatilities = vol
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
	return proj, headVersion, feedback, nil
}

// awaitReviewGate suspends at the AwaitingReview gate, multiplexing the review DECISION
// signal with the SetReviewCommentStatus (resolve / reopen) signal. A status signal mutates
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
	// VIBES AUTOGATE (F-R3 vibes-everywhere, founder-ratified): under a vibes ReviewPolicy a
	// CLEAN draft (no open change-requests) is auto-approved at gate entry WITHOUT waiting for a
	// human — the design gate honors ReviewPolicy exactly like construction (a design merge is
	// still the Method's commit authority; vibes simply removes the human hold). Attempted ONCE,
	// before the human selector, and only under the "design-vibes-autogate" GetVersion (an
	// in-flight session stays on the human gate). Open change-requests (amendment seeds, critique
	// feedback) ⇒ the human gate as today. If the synthesized approve returns actionReAwait (a
	// merge-window fault was contained — QA F35), FALL THROUGH to the human selector below: never
	// hot-loop auto-approves against a persistent fault (the queryable failureReason is the honest
	// surface; a human re-approves).
	if state.vibesAutogateEnabled && state.policyAutoApprove &&
		len(projectstate.OpenReviewCommentIDs(state.reviewThread)) == 0 {
		autoSig := reviewDecisionSignal{Decision: ReviewApprove, Approver: autoApproverVibes}
		if decision := wf.handleReviewDecision(ctx, in, autoSig, headVersion, reviewRound, redraftCount, feedback, &gf, state); decision.action != actionReAwait {
			return decision
		}
	}
	for {
		// REVIEW LEDGER: multiplex the review decision with the SetReviewCommentStatus
		// signal. A status signal mutates the durable ledger on the branch and re-suspends
		// at THIS gate WITHOUT redrafting; a review decision proceeds exactly as before.
		var sig reviewDecisionSignal
		var stSig setCommentStatusSignal
		var gotStatus bool
		sel := workflow.NewSelector(ctx)
		addDecision := func() {
			sel.AddReceive(workflow.GetSignalChannel(ctx, signalReviewDecision), func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &sig)
			})
		}
		addStatus := func() {
			sel.AddReceive(workflow.GetSignalChannel(ctx, signalSetCommentStatus), func(c workflow.ReceiveChannel, _ bool) {
				c.Receive(ctx, &stSig)
				gotStatus = true
			})
		}
		// STATUS BEFORE DECISION (design §3.4). A Selector with several ready channels picks
		// the FIRST REGISTERED, and Approve now arrives as a burst — one resolve signal per
		// ANSWERED thread, then the decision — which routinely lands in ONE workflow task. With
		// the decision registered first, the workflow would commit and end while those resolves
		// sat unread in their channel, silently dropping the bulk-resolve. Draining status
		// first costs the decision nothing: each status apply re-suspends at THIS gate, so the
		// decision is picked on the next pass with the ledger already closed. GetVersion-gated
		// because the registration order decides which handler ran, so flipping it unversioned
		// would be a replay non-determinism for a session already suspended here.
		if workflow.GetVersion(ctx, "resolve-before-decision", workflow.DefaultVersion, 1) >= 1 {
			addStatus()
			addDecision()
		} else {
			addDecision()
			addStatus()
		}
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

// produceReviewableDraft runs one draft round-trip and (for critiqued kinds) the
// critique round — PM-critique for the business-alignment kinds, architect
// self-critique for the architecture — returning the read-back draft, the
// per-iteration git session, and the loop-control step. It short-circuits after the
// draft round unless that round asked the spine to proceed.
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
	// F40: the critique commits its verdict to the SAME persistent session branch
	// (sequentially after the draft; the asset template opens no critique PR), so the draft
	// read-back version stays the correct expected version for the AwaitingReview stage —
	// the critique's own commit advances the branch, and the stage re-reads it (QA F29).
	return gf, draft, readBackVersion, wf.runCritiqueRound(ctx, in, gf, headVersion, feedback, redraftCount, state)
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
	sessionBranch := projectstate.DesignBranch(projectstate.ProjectID(in.ProjectID), toPSKind(in.ArtifactKind), in.Amendment)

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
		unchanged, cmpErr := projectstate.SameArtifactModel(model, slotFor(proj, in.ArtifactKind).Model)
		if cmpErr != nil {
			return draft, gf, 0, stepErr(cmpErr)
		}
		if unchanged {
			logger.Warn("amendment draft committed no change to the artifact; entering StageDraftFailed")
			return draft, gf, 0, wf.recoverAtFailedGate(ctx, in, headVersion, projectstate.AmendmentNoChangeReason(), "", state, feedback, redraftCount)
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
		// Capture-seam only: the FIRST dispatch of a session is a fresh draft; anything
		// after a reject (reviewRound) or a failed-gate retry (redraftCount) is rework.
		// Both counters are consulted because the two recovery paths bump different ones.
		Redraft: *redraftCount > 0 || *reviewRound > 0,
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
			return nil, 0, wf.recoverAtFailedGate(ctx, in, headVersion, projectstate.ReadBackDecodeFailedReason(decodeMsg), "", state, feedback, redraftCount)
		}
		return nil, 0, stepErr(rbErr)
	}
	// SUB-STEP (Plan-3 C1): the draft dispatch is observed complete — clear the in-flight
	// architect stamp. A critiqued kind re-stamps it as <critic>-critiquing next
	// (runCritiqueRound); an uncritiqued kind proceeds to staging, where the
	// AwaitingReview clear is a no-op.
	state.clearActive()
	return model, readBackVersion, stepProceed()
}

// runCritiqueRound is the CRITIQUE round-trip — only for the kinds the Method assigns a
// critic (PM for mission / glossary+scrubbed / core-use-cases; the ARCHITECT itself for
// the architecture, see critiqueCriticFor). A SECOND dispatch → observe → read-back
// producing a typed Critique. On CritiqueRevise the loop re-dispatches the
// architect-role draft with the critique Notes woven in, BEFORE the human gate.
// Uncritiqued kinds proceed straight through.
func (wf *workflows) runCritiqueRound(
	ctx workflow.Context,
	in coAuthorInput,
	gf gitSession,
	headVersion projectstate.Version,
	feedback *ReviewFeedback,
	redraftCount *int,
	state *coAuthorState,
) coAuthorStep {
	critic, hasCritique := critiqueCriticFor(toPSKind(in.ArtifactKind))
	if !hasCritique {
		return stepProceed()
	}
	if critic == ActiveRoleArchitect && !state.architectCritiqueEnabled {
		// REPLAY SAFETY (system-architect-critique): a KindSystem session in flight at
		// deploy time (e.g. the running gtdapp:5 redraft) resolved DefaultVersion at
		// its workflow start, so it completes on the OLD no-critique command sequence.
		return stepProceed()
	}
	logger := workflow.GetLogger(ctx)

	// F40: the PM-critique commits its verdict carrier to the SAME persistent session
	// branch as the draft (sequentially, right after the draft's commit) — no separate
	// critique branch, no PR/merge for critique (the asset template opens no critique PR).
	// Inert when the rail is dormant.
	sessionBranch := projectstate.DesignBranch(projectstate.ProjectID(in.ProjectID), toPSKind(in.ArtifactKind), in.Amendment)
	// SUB-STEP (Plan-3 C1): the assigned critic (PM, or the architect self-critiquing
	// the architecture) is now critiquing the draft. Round is not a critique concept
	// (it counts architect redraft rounds), so it stays at its cleared 0.
	state.markActive(critic, ActiveStepCritiquing, 0)
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
		logger.Warn("critique dispatch failed terminally; entering StageDraftFailed", "critic", criticLabel(critic), "error", cerr.Error())
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
		logger.Warn("critique job reached a terminal failure phase; entering StageDraftFailed", "critic", criticLabel(critic), "diagnostic", critObs.Diagnostic)
		reason := draftFailedReason(critObs.Diagnostic)
		if wf.armCritiqueRetry(ctx, state) {
			reason = critiqueFailedReason(critic, critObs.Diagnostic)
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
			logger.Warn("critique read-back found no verdict (missing-verdict safe default); entering StageDraftFailed", "critic", criticLabel(critic))
			wf.armCritiqueRetry(ctx, state)
			return wf.recoverDraftFailed(ctx, in, headVersion, critiqueMissingVerdictDiagnosticFor(critic), "", state, feedback, redraftCount)
		}
		if decodeMsg, terminal := isTerminalReadBack(crbErr); terminal {
			// The critique read-back decoded MALFORMED committed state (QA F36) — the same
			// terminal fault as the draft read-back. Land at the human StageDraftFailed gate
			// with the decode diagnostic instead of looping the read-back Activity forever.
			logger.Warn("critique read-back decoded MALFORMED committed state; entering StageDraftFailed", "critic", criticLabel(critic), "error", decodeMsg)
			return wf.recoverAtFailedGate(ctx, in, headVersion, projectstate.ReadBackDecodeFailedReason(decodeMsg), "", state, feedback, redraftCount)
		}
		return stepErr(crbErr)
	}
	// F-QA2-7: stamp the PM's conclusion (verdict + rationale + the draft round it
	// judged) on the live session view the moment the workflow observes it, so the
	// human gate shows what the PM concluded — not just the machine validation.
	// Derived from the recorded read-back Activity result; no history command.
	state.critique = critiqueViewFor(critique, critic, *redraftCount)
	if critique.Verdict == critiqueRevise {
		*redraftCount++
		if *redraftCount >= maxRedraftAttempts {
			// Do NOT crash the workflow (that wedges the SPA). The committed draft is
			// valid (it passed the CI check); stage it for the human gate with the
			// unresolved critique surfaced as a note so the human makes the final
			// call instead of an oscillating critic killing the loop.
			logger.Warn("critique did not converge within max attempts; staging for human review", "critic", criticLabel(critic))
			state.unresolvedCritique = critique.Notes
			// SUB-STEP (Plan-3 C1): the critique loop is giving up and staging for human
			// review — clear the PM stamp so the query doesn't keep claiming PM-critiquing
			// after PM work has stopped.
			state.clearActive()
			return stepProceed() // fall through to stage for review.
		}
		// Re-dispatch the architect draft with the critic's notes woven in. Memory-only feedback
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
	// F-QA2-44: one monotonic sequence number per HANDLED review decision — replay-stable
	// because it is driven purely by the recorded signal order. It keys the PER-ATTEMPT
	// version gate (gate-decision-token-remint-<seq>) guarding the approve arm's
	// credential re-mint; see commitOnApprove for why that gate must be per-attempt.
	// Pure workflow-local bookkeeping — the increment itself issues no history command.
	// Consumer audit (F-QA2-44): only the APPROVE arm consumes the session's cached rail
	// credential (mergeOnApprove: status guard / +1 relay / merge). The Reject, Withdraw,
	// and resolve/reopen (applyCommentStatus) paths — and the failed-gate Retry/Withdraw —
	// ride designSessionAccess/projectState activities that carry no workflow-cached
	// credential (the RA authenticates per call), and a failed-gate Retry's re-dispatch
	// re-mints in beginSession. So only the approve arm re-mints here.
	state.decisionSeq++
	switch sig.Decision {
	case ReviewApprove:
		// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open.
		// The manager's SetReviewCommentStatus/approve precondition rejects this synchronously,
		// but this workflow-side guard is the TOCTOU-safe backstop (a comment could be reopened
		// between the manager's query and the signal). Re-suspend at the gate; the reviewer sees
		// the open threads in the queryable thread and resolves them or redrafts.
		if open := projectstate.OpenReviewCommentIDs(state.reviewThread); len(open) > 0 {
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
		// QUEUED REPLIES (design §3.7): the submitted batch mixes utterances answering threads
		// that ALREADY exist on this slot with fresh anchored comments. Split it once, here —
		// stamping the whole batch with ONE workflow-clock timestamp, which is both replay-stable
		// and the key that makes the RA's reply append idempotent across activity retries (a
		// timestamp re-read inside the retry loop would duplicate every reviewer utterance).
		freshComments, replies, splitErr := splitIncomingComments(state.reviewThread, rejectFeedback.Comments,
			workflow.Now(ctx).UTC().Format(time.RFC3339))
		if splitErr != nil {
			// A replyTo naming no thread on this slot. SubmitReviewDecision refuses this
			// synchronously, so reaching here means the thread moved between the reviewer's
			// query and this signal. Land at the same human-visible failed gate a faulted
			// reject uses (below), keeping the feedback — never silently re-file the reply as
			// a new thread, which is exactly the loss §3.7 exists to prevent.
			return wf.recoverAtFailedGate(ctx, in, *headVersion, rejectFailedReason(splitErr), "", state, feedback, redraftCount)
		}
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
				toPSKind(in.ArtifactKind), rejectFeedback.Notes, int64(*reviewRound), freshComments, replies)
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
		// ROUND LEDGER (stage 3 task 6): the SAME send-back, as a verdict carrying the SAME
		// comments the slot thread just received, then the round's terminal. Two ledgers, one
		// content — the property the dual-write exists for, and what the twin test pins.
		wf.appendDesignVerdict(ctx, in, state, projectstate.ReviewVerdict{
			ReviewerRole: designRoleHuman,
			Actor:        designActorOperator,
			Verdict:      projectstate.VerdictSendBack,
			Summary:      rejectFeedback.Notes,
			AttemptID:    state.round.attemptID,
		}, freshComments, newSlotCommentIDs(*reviewRound, freshComments))
		wf.decideDesignRound(ctx, in, state, projectstate.RoundSentBack, designActorOperator)
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
		// ROUND LEDGER (stage 3 task 6): a withdrawn session's open round is CLOSED, not
		// abandoned. RoundWithdrawn exists for exactly this, and a round left pending forever
		// is the shape the stage-4 sweep has to clean up — here the workflow knows it is
		// ending, so it says so.
		wf.decideDesignRound(ctx, in, state, projectstate.RoundWithdrawn, designActorOperator)
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
	if step, ok := wf.remintApproveCred(ctx, gf, state); !ok {
		return step
	}
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
	// ROUND LEDGER (stage 3 task 6): the approve is recorded only once it has actually
	// HAPPENED — after the merge and the commit. Every earlier exit from this function is a
	// contained fault that returns the session to the gate, and a round stamped passed for a
	// decision that did not land is the kind of lie this wave exists to end. approver is who
	// settled it, the vibes auto-approver included, which is how an auto-approved design gate
	// stops being invisible in the data.
	actor := designApproverActor(approver)
	wf.appendDesignVerdict(ctx, in, state, projectstate.ReviewVerdict{
		ReviewerRole: designRoleHuman,
		Actor:        actor,
		Verdict:      projectstate.VerdictApprove,
		AttemptID:    state.round.attemptID,
	}, nil, nil)
	wf.decideDesignRound(ctx, in, state, projectstate.RoundPassed, actor)
	state.stage = StageCommitted
	state.clearActive() // SUB-STEP (Plan-3 C1): terminal — no role is working.
	return stepReturn(coAuthorApproved)
}

// remintApproveCred is the approve-time credential re-mint half of commitOnApprove.
// It returns ok=true when the merge window may proceed (rail dormant, version gate off,
// or a fresh token minted onto gf.cred), and ok=false with the containment step to
// return when the mint faulted.
//
// F-QA2-44: RE-MINT the installation token at gate-decision time. The session's cached
// credential (gf.cred) was minted at DISPATCH time (beginSession), and GitHub App
// installation tokens expire after ~1 hour — while this approve arrives whenever the
// human returns to the review (observed live: gtdapp kind=3 approved 8+ hours after the
// last dispatch, and every merge-window verb 403'd forever on the expired token; the
// platform classifier reports that 403 as a NON-RETRYABLE Auth fault, so neither the
// Activity RetryPolicy nor the bounded railWithAuthRetry could heal it). Mint a FRESH
// token for THIS decision's merge window (status guard / +1 relay / merge, plus the
// post-merge idempotent re-runs on a contained fault); the dispatch-time mint is
// unchanged — it still covers the dispatch scope (sync / openBranch / openPR).
//
// Temporal versioning guard (replay safety; F-QA2-44): the mint is a NEW Activity
// command in the decision path, so an in-flight session whose history already recorded
// merge-window verbs WITHOUT a preceding mint (the live gtdapp:3 carries three such
// failed approve attempts) would replay non-deterministically against unguarded new
// code. The change id is PER DECISION ATTEMPT (gate-decision-token-remint-<seq>) —
// deliberately NOT a static id like the other gates — because GetVersion caches its
// resolution PER CHANGE ID for the lifetime of the execution: a static id resolved to
// DefaultVersion while replaying the old recorded attempts would pin every FUTURE
// approve on that execution to the old no-mint behavior too, and the stuck live session
// could never heal. With a per-attempt id, each OLD recorded attempt resolves
// DefaultVersion (no mint — replay matches its history) while the NEXT decision on the
// SAME execution is a first-time GetVersion in executing mode → v1 → re-mints. Decisions
// are human-gated (a handful per session), so marker/search-attribute growth is bounded.
func (wf *workflows) remintApproveCred(ctx workflow.Context, gf *gitSession, state *coAuthorState) (coAuthorStep, bool) {
	if !gf.enabled {
		return coAuthorStep{}, true
	}
	if workflow.GetVersion(ctx, fmt.Sprintf("gate-decision-token-remint-%d", state.decisionSeq), workflow.DefaultVersion, 1) < 1 {
		return coAuthorStep{}, true
	}
	cred, cerr := wf.mintCred(ctx, gf.repoRef)
	if cerr != nil {
		// Contain exactly like a merge-window fault (QA F35): the staged draft is
		// intact on the session branch and main is untouched — return to
		// AwaitingReview so the human simply re-approves. Cancellation propagates.
		if temporal.IsCanceledError(cerr) {
			return stepErr(cerr), false
		}
		workflow.GetLogger(ctx).Warn("approve-time credential re-mint fault; returning to AwaitingReview for re-approve", "error", cerr.Error())
		return wf.reAwaitAfterApproveFault(state, approveFailedReason(cerr)), false
	}
	gf.cred = cred
	return coAuthorStep{}, true
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
// an approve/merge-window activity faulted (QA F35). It frames a re-approve, NOT a redraft.
// Wording per F-QA2-41 as corrected by F-QA2-44: the 403 copy is CAUSE-NEUTRAL — a 403 here
// is not reliably a rate limit (the live gtdapp incident was an EXPIRED installation token),
// so the notice neither blames a rate limit nor promises that waiting helps; it points at
// the operator credential on repetition instead. Deterministic across replay (pure string
// ops on the history-reconstructed error).
func approveFailedReason(err error) string {
	summary := dispatchErrSummary(err)
	if strings.Contains(summary, "403") {
		return "The approve could not complete: GitHub rejected the merge step. The draft is unchanged; try approving again — if this repeats, the operator credential may need refreshing."
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
	// architectCritiqueEnabled reports whether THIS session runs the architect
	// self-critique round for an architect-critiqued kind (KindSystem). Resolved ONCE at
	// workflow start from the "system-architect-critique" GetVersion gate: a session in
	// flight at deploy time replays DefaultVersion (no marker at its start) and completes
	// on the old no-critique command sequence for its WHOLE run; every session started
	// after the deploy resolves v1 and dispatches the system-critique job. Only consulted
	// for critics == ActiveRoleArchitect (PM-critiqued kinds are untouched).
	architectCritiqueEnabled bool
	// policyAutoApprove and vibesAutogateEnabled drive the VIBES AUTOGATE (F-R3 vibes-
	// everywhere, founder-ratified): when a session's committed ReviewPolicy preset is "vibes"
	// (policyAutoApprove), the review gate AUTO-APPROVES a clean draft (no open change-requests)
	// instead of waiting for a human — the design gate honors ReviewPolicy exactly like
	// construction. Both are snapshot ONCE at session start (beginCoAuthorSession):
	// policyAutoApprove from the head-state ReviewPolicy.Preset, vibesAutogateEnabled from the
	// "design-vibes-autogate" GetVersion gate (a session in flight at deploy time replays
	// DefaultVersion → autogate OFF → the human gate for its whole run). See awaitReviewGate.
	policyAutoApprove    bool
	vibesAutogateEnabled bool
	// roundLedgerEnabled drives the ROUND-LEDGER DUAL-WRITE (stage 3 task 6): resolved ONCE
	// at session start from the "design-round-ledger" GetVersion fence, exactly like
	// vibesAutogateEnabled above, so a session in flight at deploy time replays
	// DefaultVersion and runs its WHOLE life on the old command sequence. See the round
	// ledger section for what the dual-write records and why the slot write stays.
	roundLedgerEnabled bool
	// roundReviewers is the ROSTER the review engine computed for this session's design
	// gate, snapshot at session start beside policyAutoApprove (the same engine answer,
	// which is why it costs no second call) and persisted on every round this session
	// opens. Pure workflow-local state — no history command.
	roundReviewers []projectstate.RoundReviewer
	// ledgerVersion is the optimistic-concurrency token for the MAIN-side execution ledger.
	// It is deliberately NOT headVersion: headVersion tracks whichever substrate the design
	// session is writing (the session branch while a draft is staged), whereas the round
	// ledger lives on main like construction's, so the two versions genuinely differ during
	// the review window. Seeded from the session-start main read and advanced by each round
	// write; applyRecovering re-reads main on any drift.
	ledgerVersion projectstate.Version
	// activityOpened records that this session has already birthed the design activity's
	// execution row. OpenActivity is idempotent, so this saves a command rather than
	// guarding correctness.
	activityOpened bool
	// round is the review round currently OPEN at the human gate — empty between gates, and
	// empty for a kind whose lifecycle carries no review task. Every ledger write point
	// treats an empty round as "write nothing".
	round designRound
	// roundComments pairs a SLOT comment id with where the same comment landed on the round
	// ledger, so a resolve / reopen filed against the slot's id can be mirrored. It
	// accumulates across the session because a bulk-resolve at one gate routinely settles
	// comments filed at the previous one, whose round is already decided (the store allows a
	// status change on a decided round — the comment's fate is not the round's).
	roundComments map[string]roundCommentRef
	// reviewThread is the durable review ledger for this artifact (review-ledger feature),
	// refreshed from the session branch after every (re)stage and after every resolve/reopen
	// so the sessionState Query surfaces the live thread and the approve gate can block
	// while any comment is still open. Nil until the first read-back that carries comments.
	reviewThread []projectstate.ReviewComment
	// committedCoreUseCases is the head-state CoreUseCases (captured once at workflow
	// start), threaded here so the KindSystem read-back check can flag a System draft
	// that leaves any committed use case without a dynamic view (USECASE-DYNAMIC-MISSING,
	// founder extension 2026-07-05). Nil for every other kind and until it is populated.
	committedCoreUseCases *projectstate.CoreUseCases
	// committedVolatilities is the head-state Volatilities (captured once at workflow
	// start, exactly like committedCoreUseCases), threaded here so the KindSystem
	// read-back check can flag a System draft that leaves any committed volatility
	// unclaimed and undispositioned (SYS-VOLATILITY-COVERAGE, F10 gate lint, QA
	// amendment 2026-07-17). Nil for every other kind and until it is populated.
	committedVolatilities *projectstate.Volatilities
	// resumeFromReadBack is the F35-twin checkpoint: set true when a POST-read-back step
	// faulted and the session landed at the failed gate WITH the draft already committed on
	// the branch — an openPR rail fault (F35 twin), or ANY critique-round fault (F-QA2-24,
	// version-gated in armCritiqueRetry: terminal critique job failure, rejected critique
	// dispatch, missing verdict). On the next Retry the draft round-trip consumes it and
	// RESUMES from the read-back — SKIPPING the draft re-dispatch — so it does not
	// redispatch Claude onto a branch that already carries the model (which the no-commit
	// guard would red); for a PM-critiqued kind the spine then falls through to
	// runCritiqueRound, re-running the critique against the kept draft. Workflow-local,
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
	// decisionSeq counts the review decisions HANDLED at the AwaitingReview gate — one
	// monotonic increment per received reviewDecision signal (F-QA2-44). Replay-stable:
	// driven purely by the recorded signal order. It keys the per-attempt version gate
	// (gate-decision-token-remint-<seq>) guarding the approve arm's gate-time credential
	// re-mint; see commitOnApprove for why that gate is per-attempt rather than static.
	decisionSeq int
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
	// F10 gate lints (QA amendment 2026-07-17) — the two cross-artifact System checks
	// that need COMMITTED priors (the (kind, draft)-only F10 lint, DV-TITLE-EMPTY, rides
	// stateValidationFindingGenerators below): every committed volatility must be claimed
	// or dispositioned in the draft's encapsulates prose (SYS-VOLATILITY-COVERAGE, error),
	// and a one-Manager-per-core-use-case decomposition with mirrored names is flagged as
	// the services-explosion fingerprint (SYS-SERVICES-EXPLOSION, warning).
	if extra := volatilityCoverageFindings(s.artifactKind, s.draft, s.committedVolatilities); len(extra) > 0 {
		findings = append(append([]Finding{}, findings...), extra...)
	}
	if extra := servicesExplosionFindings(s.artifactKind, s.draft, s.committedCoreUseCases); len(extra) > 0 {
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
		// Name the actual critic (the stamped critique view's Role is set on every path
		// that also sets unresolvedCritique) — an architect-critiqued kind must never be
		// presented as PM-reviewed (role-honesty, architect self-critique amendment).
		ruleID, label := RuleID("PM-CRITIQUE-UNRESOLVED"), "PM critique"
		if s.critique != nil && s.critique.Role == critiqueRoleArchitect {
			ruleID, label = RuleID("ARCHITECT-CRITIQUE-UNRESOLVED"), "Architect self-critique"
		}
		findings = append(append([]Finding{}, findings...), Finding{
			RuleID:   ruleID,
			Severity: SeverityWarning,
			Message:  label + " did not converge after max attempts; latest note: " + s.unresolvedCritique,
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
// non-empty activity diagram with an ENTRY (a start node, or a timeEvent/
// acceptEvent node with no incoming edge — tier parity with methodcheck's
// activityHasEntryAndAction, framework-go/methodcheck/rules_statevalidation.go,
// ratified 2026-07-30) plus at least one action step. The Action's CI validate
// check does NOT enforce this (the committed gtdapp CoreUseCases shipped core
// use cases with "activity": null), so this read-back check is the app-side
// surface that flags a diagram-less use case at the review panel. It classifies
// the defect only — full UML well-formedness stays the Action's CI concern.
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
			Message:  fmt.Sprintf("Use case %q %s; every use case (core AND supporting) must carry a non-empty activity diagram with an entry (a start node, or an edge-less timeEvent/acceptEvent) and at least one action step.", label, reason),
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
// non-empty floor, or "" when it is acceptable (present, with an ENTRY — a start
// node, or a timeEvent/acceptEvent node with no incoming edge (tier parity with
// methodcheck's activityHasEntryAndAction, framework-go/methodcheck/
// rules_statevalidation.go, ratified 2026-07-30) — AND at least one action node).
// It deliberately does NOT re-validate full UML well-formedness (decision/merge,
// fork/join, guards) — that is the Action's CI check; this only enforces "the
// diagram exists and carries the minimum meaningful nodes".
func activityDefect(a *projectstate.ActivityDiagram) string {
	if a == nil {
		return "has no activity diagram (activity is null)"
	}
	if len(a.Nodes) == 0 {
		return "has an empty activity diagram (no nodes)"
	}
	incoming := make(map[string]int, len(a.Nodes))
	for _, e := range a.Edges {
		incoming[e.To]++
	}
	var hasEntry, hasAction bool
	for _, n := range a.Nodes {
		// Only the entry + action node kinds matter to the founder's floor; every other
		// node kind is irrelevant here (plain comparisons, not a switch, so the exhaustive
		// linter is not drawn into the full ActivityNodeKind set).
		if n.Kind == projectstate.NodeStart {
			hasEntry = true
		}
		if (n.Kind == projectstate.NodeTimeEvent || n.Kind == projectstate.NodeAcceptEvent) && incoming[n.ID] == 0 {
			hasEntry = true
		}
		if n.Kind == projectstate.NodeAction {
			hasAction = true
		}
	}
	switch {
	case !hasEntry && !hasAction:
		return "has an activity diagram with no entry (no start node, and no edge-less timeEvent/acceptEvent) and no action step"
	case !hasEntry:
		return "has an activity diagram with no entry (no start node, and no edge-less timeEvent/acceptEvent)"
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
// already a summary, not a log firehose — agenticJobAccess.md Non-goal #4).
func draftFailedReason(diagnostic string) string {
	if diagnostic == "" {
		return "the design job failed in CI — retry or withdraw"
	}
	return "the design job failed in CI: " + diagnostic + " — retry or withdraw"
}

// critiqueFailedReason renders the human "why" for the StageDraftFailed screen when the
// CRITIQUE job (not the draft) reached a terminal failure phase (F-QA2-24), naming the
// actual critic (PM-critique, or the architect self-critique for KindSystem). The draft
// is intact on the session branch, so the copy names the critique and frames Retry as
// re-running it — never the generic "the design job failed in CI", which reads as a draft
// failure and misleads the operator about what a Retry does. Used ONLY when
// armCritiqueRetry armed the critique-retry resume (v1 semantics) so copy and behavior
// stay honest together on every pinned version.
func critiqueFailedReason(critic ActiveRole, diagnostic string) string {
	if diagnostic == "" {
		return "the " + criticLabel(critic) + " job failed in CI — your draft is kept; retry re-runs the critique, or withdraw"
	}
	return "the " + criticLabel(critic) + " job failed in CI: " + diagnostic + " — your draft is kept; retry re-runs the critique, or withdraw"
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
// produceReviewableDraft falls through to runCritiqueRound — the full dispatch → observe →
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

// readBackDecodeFailedReason / amendmentNoChangeReason PROMOTED to
// projectstate.ReadBackDecodeFailedReason / projectstate.AmendmentNoChangeReason
// (code-health-phase-bd task D3) — byte-identical pure formatters, no longer duplicated
// with projectdesign's twin.

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
//               the FROZEN agenticJobAccess.SubmitAgenticJob verb,
//               carrying {artifact_kind, command, target_branch, prior_state_ref,
//               job_mode} on the additive PipelineSpec.DispatchInputs field
//               (C-WF-DESIGN input schema). The doctrine lives in the command's
//               method-assets, not a composed prompt. The RA reserves + stamps
//               idempotency_token itself; the Manager MUST NOT set it.
//   - OBSERVE   the Manager polls ObserveAgenticJob(handle) between
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
func designPipelinePhase(p agenticjob.PipelinePhase) pipelinePhase {
	switch p {
	case agenticjob.PhasePending:
		return pipelinePending
	case agenticjob.PhaseRunning:
		return pipelineRunning
	case agenticjob.PhaseSucceeded:
		return pipelineSucceeded
	case agenticjob.PhaseFailed:
		return pipelineFailed
	case agenticjob.PhaseCancelled:
		return pipelineCancelled
	default:
		return lPipelinePhaseUnknown
	}
}

// pipelinePhase mirrors agenticJobAccess.md §3 — the infrastructure-
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

// pipelineObservation mirrors agenticJobAccess.md §3 — a point-in-time,
// infrastructure-neutral view carrying the phase and (on terminal failure) a
// neutral Diagnostic summary (NOT a log firehose).
type pipelineObservation struct {
	Phase      pipelinePhase
	Diagnostic string
	// RunURL is the CI run's URL on ANY observation the RA resolved it for: while the
	// run is live it is the generating view's "view the run" deep-link (F-GTD-6);
	// on a terminal failure it is the "why" pointer the Manager threads onto the
	// StageDraftFailed card (QA F15 gap 2b). Empty when the RA could not resolve it.
	//
	// It doubles as the only VENUE signal a workflow ever sees — see episodeVenueIsRemote.
	RunURL string
	// Episode is the terminal run's captured agentic-episode summary (SP1 capture-seam):
	// the tokens/turns/tools the design agent actually burned. Nil on every non-terminal
	// observation, on the GitHub-Actions arm (which mines no episode in v1), and —
	// legitimately — on a CANCELLED run's FIRST terminal observation, whose summary lands
	// only once the subprocess has unwound (see awaitLateEpisode).
	Episode *agenticjob.EpisodeSummary
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
	// Redraft marks a re-dispatch of the SAME draft after a human send-back / PM revise.
	// It is a CAPTURE-SEAM field only — dispatchDesignJob ignores it, and the job the RA
	// receives is byte-identical either way. It exists so the episode ledger can tell a
	// first draft (EpisodeKindDesign) from a rework round (EpisodeKindRework), which is
	// exactly the distinction the self-improvement pipeline is built to measure.
	Redraft bool
}

// dispatchDesignJob composes the agenticjob.PipelineSpec for one design job and
// submits it through the generated invoker, returning the opaque handle. The four DESIGN
// parameters (plus the job_mode discriminator) ride on DispatchInputs; a per-project
// TargetRepo (decoded from the opaque RepoRef) + WorkflowFile target the user's per-project
// repo + aiarch-design.yml, else an empty target falls back to the RA's configured
// construction repo. The idempotency key is stamped INSIDE the generated submit Activity
// (genActivityIdempotencyKey), so a redraft (fresh ExecuteActivity → new ActivityID) is a
// distinct job while a transient auto-retry collapses to the same handle at the RA.
func (wf *workflows) dispatchDesignJob(ctx workflow.Context, a dispatchDesignJobArgs) (agenticjob.PipelineHandle, error) {
	// The .claude command slug the design job runs — the doctrine that used to be
	// composed into design_prompt now lives in that command's method-assets. An empty
	// slug is contract misuse (an undispatchable (kind, mode) — e.g. SdpReview, which is
	// assembled server-side, never dispatched); fail terminally before dispatch.
	command := projectstate.DesignCommandFor(toPSKind(a.ArtifactKind), designModeFor(a.Target), "")
	if command == "" {
		return agenticjob.PipelineHandle(""), temporal.NewNonRetryableApplicationError(
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
		return agenticjob.PipelineHandle(""), terr
	}
	spec := agenticjob.PipelineSpec{
		ProjectID: agenticjob.ProjectID(a.ProjectID),
		// A non-empty, well-formed step graph satisfies the RA's §2.1 pre-condition; the
		// design recipe lives in the user's aiarch-design.yml workflow file, so the step is
		// a logical placeholder. The DESIGN-job parameters ride on DispatchInputs.
		Steps: []agenticjob.PipelineStep{{
			Name:      "design",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
	}
	if a.TargetRepo != "" {
		spec.WorkflowFile = designWorkflowFileName
	}
	return wf.Acts.PipelineSubmitAgenticJob(ctx, spec)
}

// observeDesignJob reads the dispatched job's phase once (pull-shaped, side-effect-free;
// agenticJobAccess.md §2.2) through the generated invoker and maps the RA phase
// onto this Manager's neutral phase.
func (wf *workflows) observeDesignJob(ctx workflow.Context, handle agenticjob.PipelineHandle) (pipelineObservation, error) {
	obs, err := wf.Acts.PipelineObserveAgenticJob(ctx, handle)
	if err != nil {
		return pipelineObservation{}, err
	}
	return pipelineObservation{
		Phase:      designPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
		RunURL:     obs.RunURL,
		Episode:    obs.Episode,
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
	if agenticjob.PipelineHandleIsZero(handle) {
		return pipelineObservation{}, temporal.NewNonRetryableApplicationError(
			"dispatch returned an empty pipeline handle", "EmptyPipelineHandle", nil)
	}

	var last pipelineObservation
	for range maxObservePolls {
		obs, err := wf.observeDesignJob(ctx, handle)
		if err != nil {
			return pipelineObservation{}, err
		}
		if obs.RunURL != "" {
			state.runURL = obs.RunURL
		}
		if obs.Phase.IsTerminal() {
			// Episode capture LAST, after this poll's business handling (§capture-seam).
			wf.captureEpisode(ctx, args, handle, wf.awaitLateEpisode(ctx, handle, obs))
			return obs, nil
		}
		last = obs
		// Not yet terminal — space the next observe with a durable in-workflow timer.
		if err := workflow.Sleep(ctx, observePollInterval); err != nil {
			return pipelineObservation{}, err
		}
	}
	// Bounded poll budget exhausted without a terminal phase. Treat as an explicit
	// terminal failure (NOT a success, NOT a perpetual Drafting) so the caller routes
	// to the StageDraftFailed human gate.
	exhausted := pipelineObservation{
		Phase:      pipelineFailed,
		Diagnostic: "design job did not reach a terminal state within the observation window",
		RunURL:     last.RunURL,
		Episode:    last.Episode,
	}
	// The stuck job still burned tokens, so it still owes the ledger a record (a gap when
	// nothing was mined) — never silent.
	wf.captureEpisode(ctx, args, handle, exhausted)
	return pipelineObservation{Phase: exhausted.Phase, Diagnostic: exhausted.Diagnostic}, nil
}

// ---------------------------------------------------------------------------
// Episode capture (SP1 capture-seam, Task 7)
// ---------------------------------------------------------------------------
//
// EVERY terminal observation of a design dispatch — draft AND critique, since
// dispatchAndObserve is the single choke point both go through — becomes EXACTLY ONE
// EpisodeRecord in the episode ledger: either the mined summary or an explicit GAP
// record. A missing record is never silently missing.
//
// THREE disciplines hold here, and each has a reason:
//
//   - BUSINESS FIRST, EPISODE SECOND. The append is the LAST thing a terminal poll does
//     (the run-URL stamp has already run) and its own failure is swallowed. A ledger line
//     claiming something happened when it did not is worse than a missing line.
//   - THE APPEND NEVER FAILS THE SESSION. appendEpisodeActivityOptions gives it its own
//     retry envelope, wholly independent of the business retries; still failing at the
//     end of that envelope means LOG and continue. A co-author session must not die
//     because a bookkeeping write did.
//   - VENUE. The GitHub-Actions arm mines no episode in v1, so a nil summary there is
//     EXPECTED, not a gap — a gap record per GH run would be noise that means nothing.
//
// DETERMINISM: the append is a plain ExecuteActivity — a NEW command in an EXISTING
// workflow body. In-flight executions must be DRAINED before deploying (no GetVersion
// guard is carried).

// maxLateEpisodePolls bounds the EXTRA observe polls a CANCELLED run is given before its
// episode is written off as a gap. Cancel flips the RA's phase SYNCHRONOUSLY while the
// agent subprocess is still unwinding, so a cancelled run's FIRST terminal observation
// legitimately carries no summary — it appears on a later poll.
const maxLateEpisodePolls = 4

// lateEpisodePollInterval spaces the late-episode grace polls. DELIBERATELY tighter than
// the business poll interval: the wait is pure bookkeeping, but the workflow is blocked on
// it, so a cancelled run would otherwise sit visibly "generating" for a further minute
// before landing at its failure gate. Five seconds comfortably clears the executor's own
// subprocess wait, and four of them cap the whole grace window at 20s.
const lateEpisodePollInterval = 5 * time.Second

// episodeVenueIsRemote reports whether this observation came from the REMOTE
// (GitHub-Actions) venue, which mines no episode summary in v1. The run URL is the only
// venue fact an observation carries: the Actions arm stamps it, and neither the local
// executor nor the dry-run stub ever does. A GH run whose URL the RA could not resolve
// therefore reads as local and earns a gap — deliberately the safe direction (a visible,
// labelled gap beats a silent loss).
func episodeVenueIsRemote(runURL string) bool {
	return runURL != ""
}

// awaitLateEpisode gives a CANCELLED run's episode summary a bounded chance to arrive
// (see maxLateEpisodePolls). It returns the observation to RECORD: the caller's original
// with a late summary folded in when one arrived, else the original unchanged — which
// becomes a gap. The business phase is never altered.
func (wf *workflows) awaitLateEpisode(ctx workflow.Context, handle agenticjob.PipelineHandle, obs pipelineObservation) pipelineObservation {
	if obs.Phase != pipelineCancelled || obs.Episode != nil {
		return obs
	}
	for range maxLateEpisodePolls {
		if err := workflow.Sleep(ctx, lateEpisodePollInterval); err != nil {
			return obs
		}
		next, err := wf.observeDesignJob(ctx, handle)
		if err != nil {
			return obs
		}
		if next.Episode != nil {
			obs.Episode = next.Episode
			return obs
		}
	}
	return obs
}

// captureEpisode appends the ONE ledger record this terminal observation owes.
func (wf *workflows) captureEpisode(ctx workflow.Context, args dispatchDesignJobArgs, handle agenticjob.PipelineHandle, obs pipelineObservation) {
	if episodeVenueIsRemote(obs.RunURL) {
		return
	}
	targetRef := artifactKindString(args.ArtifactKind)
	kind := episodeKindFor(args)
	var rec episode.EpisodeRecord
	lineage := episodeLineage(ctx)
	if obs.Episode == nil {
		rec = episodeGapRecord(kind, targetRef, lineage,
			"gap-"+episodeIDSafe(episodeIDSeed(handle, args)),
			episodeGapReason(episodeMissingSummaryReason, obs.Diagnostic), workflow.Now(ctx))
	} else {
		rec = episodeRecordFromSummary(*obs.Episode, kind, targetRef, lineage, obs.Diagnostic)
	}
	if err := wf.Acts.EpisodesAppendEpisode(ctx, episode.ProjectID(args.ProjectID), rec); err != nil {
		// Swallowed BY DESIGN — see the "never fails the session" discipline above.
		workflow.GetLogger(ctx).Error("episode append failed after its full retry envelope; this episode is NOT in the ledger",
			"artifactKind", targetRef, "episodeId", rec.EpisodeID, "error", err.Error())
	}
}

// episodeKindFor classifies a design dispatch for the ledger: a critique is a REVIEW
// episode, a re-dispatch of the same draft is REWORK, a first draft is DESIGN. (The
// answer job never reaches this workflow-side path — it dispatches straight from the
// Manager — but the switch stays total.)
func episodeKindFor(args dispatchDesignJobArgs) episode.EpisodeKind {
	switch args.Target {
	case dispatchTargetCritique:
		return episode.EpisodeKindReview
	case dispatchTargetAnswer:
		return episode.EpisodeKindAnswer
	case dispatchTargetDraft:
		if args.Redraft {
			return episode.EpisodeKindRework
		}
		return episode.EpisodeKindDesign
	default:
		return episode.EpisodeKindDesign
	}
}

// episodeIDSeed is the deterministic, replay-stable seed a GAP record's EpisodeID is
// built from — the dispatch handle (unique per dispatch, already in workflow history),
// falling back to the artifact kind for a zero handle.
func episodeIDSeed(handle agenticjob.PipelineHandle, args dispatchDesignJobArgs) string {
	if h := agenticjob.PipelineHandleString(handle); h != "" {
		return h
	}
	return artifactKindString(args.ArtifactKind)
}

// episodeLineage stamps the durable-execution lineage every workflow-side episode
// carries. ActivityID is left unset: a design episode is anchored to an ARTIFACT (the
// TargetRef), not to a Phase-3 Method activity, and the Temporal activity id is not
// readable from inside a workflow.
func episodeLineage(ctx workflow.Context) *episode.EpisodeLineage {
	exec := workflow.GetInfo(ctx).WorkflowExecution
	return &episode.EpisodeLineage{WorkflowID: exec.ID, RunID: exec.RunID}
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

	// F-QA2-49: GitHub SECONDARY rate limits demand a >=60s cool-down before any retry can
	// succeed, so the original ~30s budget (5s → 10s → 15s) expired ENTIRELY INSIDE the
	// cool-down window after an API-heavy draft job (observed live: 3 openPR attempts across
	// 15s → all 403 → StageDraftFailed; a manual retry 15 min later succeeded first try).
	// v1 ("rail-403-long-backoff") lengthens the 403/auth class to 60s → 120s → 240s
	// (~7 min budget, 4 attempts) — long enough to outlast a secondary-rate-limit window,
	// still bounded so a GENUINE permission denial reaches the honest containment gates.
	railAuthRetryLongMaxAttempts = 4
	railAuthRetryLongBaseBackoff = 60 * time.Second
	railAuthRetryLongMaxBackoff  = 240 * time.Second
)

// railWithAuthRetry runs ANY rail call (a closure over a generated invoker, incl.
// syncManagedScaffold — B10) with a bounded WORKFLOW-SIDE retry on a transient-403-as-Auth
// fault (QA F35 + its draft-round-trip twin). The platform github ClassifyStatus conflates
// GitHub secondary rate-limit 403s with real permission denials — both become a NON-RETRYABLE
// Auth ApplicationError the Activity RetryPolicy cannot retry — so the workflow retries here:
// under the "rail-403-long-backoff" version gate, up to railAuthRetryLongMaxAttempts over
// ~7 min (60s → 120s → 240s, F-QA2-49 — secondary rate limits need a >=60s cool-down);
// pre-gate executions keep the OLD ~30s budget (5s → 10s → cap 15s). workflow.Sleep gives
// deterministic backoff. A GENUINE permission denial exhausts the budget and the error
// propagates to the CALLER, which CONTAINS it (openPR/OpenBranch → the StageDraftFailed gate;
// the approve window → back to AwaitingReview for re-approve) — never a crash. Transport blips
// (Transient) are still retried INSIDE the Activity by railActivityOptions. Cancellation
// propagates immediately. This is the ONE shared helper — the approve window and the draft
// round-trip do NOT duplicate the retry loop.
func (wf *workflows) railWithAuthRetry(ctx workflow.Context, call func() error) error {
	maxAttempts, backoff, maxBackoff := railAuthRetryMaxAttempts, railAuthRetryBaseBackoff, railAuthRetryMaxBackoff
	gated := false
	for attempt := 1; ; attempt++ {
		err := call()
		if err == nil {
			return nil
		}
		if temporal.IsCanceledError(err) || !isRailAuthFault(err) {
			return err
		}
		// F-QA2-49 replay safety: the long-backoff schedule changes the durable timer
		// sequence, so it is GetVersion-gated (the failed-gate-critique-retry pattern).
		// The gate is resolved LAZILY — only when a 403 fault actually occurs — so
		// fault-free histories carry no version marker. GetVersion caches per changeID,
		// so an in-flight execution whose replayed history already resolved
		// DefaultVersion (an old 5s/10s timer burst) stays pinned to the OLD schedule;
		// a first-time fault in executing mode resolves v1 → the long schedule.
		if !gated {
			gated = true
			if workflow.GetVersion(ctx, "rail-403-long-backoff", workflow.DefaultVersion, 1) >= 1 {
				maxAttempts, backoff, maxBackoff = railAuthRetryLongMaxAttempts, railAuthRetryLongBaseBackoff, railAuthRetryLongMaxBackoff
			}
		}
		if attempt >= maxAttempts {
			return err
		}
		workflow.GetLogger(ctx).Warn("rail 403 (auth/rate-limit); bounded workflow-side retry", "attempt", attempt)
		_ = workflow.Sleep(ctx, backoff)
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
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
// designSessionAccess.readProjectOnBranch invoker for both cases; branch=="" is
// short-circuited to readProject (its own DesignSessionReadProjectOnBranch call) so the
// two collapse to the SAME activity registration.
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
// AwaitingReview gate, which applies the branch mutation (open|answered->resolved /
// resolved->open).
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
	return anchoredToLedgerComments(feedback.Comments)
}

// anchoredToLedgerComments is feedbackToLedgerComments' inner half, over a bare comment
// slice — the shape the §3.7 split hands back once the replies have been taken out.
func anchoredToLedgerComments(comments []AnchoredComment) []projectstate.ReviewComment {
	out := make([]projectstate.ReviewComment, 0, len(comments))
	for _, c := range comments {
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

// reviewerUtteranceRole is the role stamped on a REPLY the human reviewer files into an
// existing thread. It differs from reviewAuthorRole ("architect", the role stamped on the
// comments the reviewer OPENS) because the derive rule reads it: projectstate.isReviewerRole
// treats "architect" and "pm" as AGENT roles, so a reviewer reply stamped "architect" would
// leave the thread reading as answered by its own author. Design §3.2 names this role.
const reviewerUtteranceRole = "architect-user"

// splitIncomingComments splits one submitted batch into the two things it can be: utterances
// answering an EXISTING thread (replyTo non-empty), and fresh anchored comments, which open
// new threads for this round. It only SPLITS — projectstate.ApplyReviewBatch performs both
// appends in one atomic commit, so utterance-id minting and the reopen-bit rule stay in the
// RA that owns the ledger rather than being mirrored here (ruling P2).
//
// A replyTo naming no thread is a ContractMisuse rather than a silent new thread — silently
// reinterpreting a reply as a new comment would lose the reviewer's place in the
// conversation. at is the caller's single timestamp for the whole batch; it must be stamped
// ONCE (from the deterministic workflow clock), because it is also what makes the RA's reply
// append idempotent under Temporal activity retry.
func splitIncomingComments(thread []projectstate.ReviewComment, incoming []AnchoredComment, at string) ([]projectstate.ReviewComment, []projectstate.ReviewReply, error) {
	if err := checkReplyTargets(ledgerCommentIDs(thread), incoming); err != nil {
		return nil, nil, err
	}
	fresh, replies := partitionIncomingComments(incoming, at)
	return anchoredToLedgerComments(fresh), replies, nil
}

// splitIncomingQuestions is splitIncomingComments' ASK-door twin: the same split, the same
// refusal, but the fresh half opens QUESTION-typed entries addressed to a role rather than
// change-requests (questionsToLedger). It exists because a question thread is the
// conversational case — "with questions, the ai will respond like a comment response" — so a
// follow-up filed against an answered question must land INSIDE that thread. The reviewer
// utterance is stamped with the same reviewerUtteranceRole the change-request path uses, so
// the RA's derive rule reads it as human and re-opens the thread for the answer job.
func splitIncomingQuestions(thread []projectstate.ReviewComment, addressee string, incoming []AnchoredComment, at string) ([]projectstate.ReviewComment, []projectstate.ReviewReply, error) {
	if err := checkReplyTargets(ledgerCommentIDs(thread), incoming); err != nil {
		return nil, nil, err
	}
	fresh, replies := partitionIncomingComments(incoming, at)
	return questionsToLedger(addressee, fresh), replies, nil
}

// partitionIncomingComments is the thread-INDEPENDENT half of the split: it sorts one batch
// into the entries that open a thread (no replyTo) and the utterances that answer one, with
// no knowledge of which threads exist. Kept separate so a caller that must know the shape of
// a batch BEFORE it reads the ledger (AskQuestions derives its idempotency key and its
// emptiness refusal up front) partitions once and runs checkReplyTargets against the thread
// it later reads — without duplicating the reply-stamping rule.
func partitionIncomingComments(incoming []AnchoredComment, at string) ([]AnchoredComment, []projectstate.ReviewReply) {
	var fresh []AnchoredComment
	var replies []projectstate.ReviewReply
	for _, c := range incoming {
		if c.ReplyTo == "" {
			fresh = append(fresh, c)
			continue
		}
		if strings.TrimSpace(c.Text) == "" {
			continue // an empty utterance is not a reply (mirrors the fresh-comment drop)
		}
		replies = append(replies, projectstate.ReviewReply{
			CommentID:  c.ReplyTo,
			AuthorRole: reviewerUtteranceRole,
			Text:       c.Text,
			At:         at,
		})
	}
	return fresh, replies
}

// checkReplyTargets refuses a batch whose replyTo names no thread on this artifact. Split
// out from splitIncomingComments so the Manager op can run the SAME refusal synchronously
// against the queried wire thread, where a ContractMisuse still reaches the caller (a signal
// payload's error cannot).
func checkReplyTargets(known map[string]bool, incoming []AnchoredComment) error {
	for _, c := range incoming {
		if c.ReplyTo != "" && !known[c.ReplyTo] {
			return newError(fwmanager.ContractMisuse, "replyTo names no thread on this artifact: "+c.ReplyTo)
		}
	}
	return nil
}

// checkNoReplyTo refuses a batch carrying ANY replyTo on an op that cannot route one into an
// existing thread. Such an op has only one other option — convert the reply into a fresh
// unanchored comment — and that silent detachment is precisely what replyTo exists to
// prevent, so it fails loudly instead. reason names the op's own limitation.
func checkNoReplyTo(reason string, incoming []AnchoredComment) error {
	for _, c := range incoming {
		if c.ReplyTo != "" {
			return newError(fwmanager.ContractMisuse, reason+" (offending replyTo: "+c.ReplyTo+")")
		}
	}
	return nil
}

// ledgerCommentIDs / viewCommentIDs collect the thread's entry ids from the durable and the
// wire projection respectively — the two shapes checkReplyTargets is asked about.
func ledgerCommentIDs(thread []projectstate.ReviewComment) map[string]bool {
	ids := make(map[string]bool, len(thread))
	for _, c := range thread {
		ids[c.ID] = true
	}
	return ids
}

func viewCommentIDs(thread []ReviewCommentView) map[string]bool {
	ids := make(map[string]bool, len(thread))
	for _, c := range thread {
		ids[c.ID] = true
	}
	return ids
}

// openReviewCommentIDs PROMOTED to projectstate.OpenReviewCommentIDs
// (code-health-phase-bd task D3) — byte-identical pure predicate, no longer duplicated
// with projectdesign's twin.

// seedAmendmentLedger records the reopening feedback (coAuthorInput.Feedback) as round-0 OPEN
// ledger entries on the amendment session branch, right after the first stage, then reloads
// the in-memory thread so the query + prompt surface them. Both the anchored Comments AND the
// free-text Notes rationale seed (the Notes as one unanchored comment — the amend-seed-notes
// fix; the webApp Amend composer sends its whole payload as Notes). Best-effort: a seed miss
// (e.g. a non-ledger substrate) leaves the feedback in the prompt only. No-op only when the
// feedback carries neither Notes nor anchored comments.
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
	// Free-text rationale (Feedback.Notes) is NOT an anchored comment, so
	// feedbackToLedgerComments drops it — yet the webApp Amend composer folds the user's
	// rationale AND every queued rail comment into Notes (it sends no structured
	// Comments). Left unseeded, a Notes-only amendment reopens the ledger EMPTY and the
	// redraft agent reconciles on a stale basis with the user's direction silently lost
	// (the reported data-loss bug). Synthesize an unanchored round-0 comment carrying the
	// Notes so it reaches the agent via the ledger, alongside any anchored comments.
	//
	// REPLAY SAFETY (mirrors the failed-gate-ledger-seed gate): this seed was ADDED after
	// the workflow first shipped, so an amendment session in flight at deploy time has no
	// history event for it. GetVersion pins such pre-feature executions to DefaultVersion
	// (they skip the Notes synthesis for their whole run — the version resolved at first
	// replay is cached per execution), while executions started after this deploy resolve
	// v1 and seed the Notes.
	if workflow.GetVersion(ctx, "amend-seed-notes", workflow.DefaultVersion, 1) >= 1 {
		if notes := strings.TrimSpace(in.Feedback.Notes); notes != "" {
			comments = append(comments, projectstate.ReviewComment{Text: notes, AuthorRole: reviewAuthorRole})
		}
	}
	if len(comments) == 0 {
		return
	}
	branch := gf.readBackBranch()
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, branch, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		// nil replies: an amendment's reopening feedback opens round-0 threads and cannot
		// answer one. This seed runs BEFORE the first loadReviewThread of the session, so
		// there is no thread to route a reply against — which is why RequestArtifactDraft
		// refuses a replyTo at the door rather than letting one arrive here to be re-filed.
		return wf.Acts.DesignSessionSeedReviewCommentsOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, branch, toPSKind(in.ArtifactKind), 0, comments, nil)
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
// into the ledger on the SAME session branch — PLUS the free-text Notes rationale as one
// unanchored comment (the amend-seed-notes fix; a memory-only feedback is often Notes-only) —
// consuming a review round (reviewRound, like a reject) so the seeded ids do not collide with a
// later reject's on the one accumulating thread. Best-effort, mirroring seedAmendmentLedger: an
// empty feedback (neither Notes nor anchored comments), an unpopulated slot, a non-ledger
// substrate, or a transient fault leaves the feedback un-seeded and RETRIES on the next redraft
// dispatch. Returns whether the seed durably landed, so the caller marks feedbackSeeded and
// stops re-seeding. headVersion is a hint only — applyRecovering re-reads on a version conflict.
func (wf *workflows) seedFailedGateFeedback(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion projectstate.Version, feedback *ReviewFeedback, reviewRound *int, state *coAuthorState) bool {
	// SPLIT, NEVER RE-FILE (design §3.7). The retained feedback is the SAME batch the reject
	// arm received, replies included, so converting it with anchoredToLedgerComments alone
	// would drop every ReplyTo and seed the reviewer's "still vague" as a fresh unanchored
	// comment — the exact detachment replyTo exists to prevent, one recovery path down. Split
	// it against the last-known thread and hand both halves to the seed verb.
	comments, replies, serr := splitIncomingComments(state.reviewThread, feedback.Comments,
		workflow.Now(ctx).UTC().Format(time.RFC3339))
	if serr != nil {
		// A replyTo naming no thread in the last-known ledger. REFUSE the seed rather than
		// re-file the reply as a new comment: the feedback stays in workflow memory (and in
		// the failed gate's reason), the reviewer sees an un-seeded gate, and a Retry re-runs
		// this against a freshly loaded thread. Silent detachment is the one outcome ruled out.
		workflow.GetLogger(ctx).Error("review-ledger: failed-gate seed REFUSED — a queued reply names no thread in the last-known ledger; not re-filing it as a new comment",
			"artifactKind", artifactKindString(in.ArtifactKind), "error", serr.Error())
		return false
	}
	// Fold the free-text rationale (Notes) into an unanchored comment too — a memory-only
	// failed-gate feedback (a PM-critique revise, a redraft signal) is Notes-only, so
	// without this the durable seed is empty and the redraft agent loses the direction
	// under thin dispatch. GetVersion-gated on the SAME change id as the amendment seed
	// (see seedAmendmentLedger) so in-flight executions replay deterministically.
	if workflow.GetVersion(ctx, "amend-seed-notes", workflow.DefaultVersion, 1) >= 1 {
		if notes := strings.TrimSpace(feedback.Notes); notes != "" {
			comments = append(comments, projectstate.ReviewComment{Text: notes, AuthorRole: reviewAuthorRole})
		}
	}
	// A replies-ONLY batch is still feedback worth seeding, so the emptiness test spans both
	// halves (pre-feature executions carry no replies, so this reads exactly as before).
	if len(comments) == 0 && len(replies) == 0 {
		return false
	}
	branch := gf.readBackBranch()
	round := int64(*reviewRound)
	if _, err := wf.applyRecovering(ctx, in.ProjectID, branch, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSeedReviewCommentsOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, branch, toPSKind(in.ArtifactKind), round, comments, replies)
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
// every (re)stage and after every resolve/reopen so the sessionState Query + the approve gate
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

// applyCommentStatus applies one reviewer review-ledger transition (resolve / reopen) to the
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
	// ROUND LEDGER (stage 3 task 6): the same transition on the round's own copy of the
	// comment. Inert off the fence and for a comment this session did not append to a round.
	wf.mirrorCommentStatus(ctx, in, state, sig)
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
	}
}

// ---------------------------------------------------------------------------
// THE ROUND LEDGER (spec 2026-09-20 §5.3, stage 3 task 6) — the design rail's
// DUAL-WRITE.
//
// Every review decision this rail takes is now recorded TWICE: once where it has always
// been recorded (the artifact slot's ReviewThread + status, which the SPA still reads
// through GetSessionState → committedSessionView → reviewThreadToView) and once as a
// ReviewRound on the activity execution ledger, through activityExecutionAccess. The slot
// write is not a legacy path to be cut over here — deleting it would blank the design
// review UI for the length of the wave, so it stays until stage 6 re-points the SPA.
//
// What the round carries that the thread never could: the ROSTER the review engine
// computed, the VERDICTS (the critic's and the human's, each with its role and its
// summary), the SUBJECT the round judged, the round NUMBER as a first-class field, and
// the terminal — passed, sent back or withdrawn — with who decided it. A vibes preset
// auto-approving with nothing in the data to show for it is the defect this wave exists
// to end, and on this rail the auto-approver's own name lands in DecidedBy.
//
// KEYED LIKE A CONSTRUCTION ROUND, from the same tables. The activity id is the design
// PREFIX activity the derived plan carries (requirements / architecture / projectDesign)
// — designActivityFor(kind) resolves the kind to it — and the task id is that lifecycle
// phase's gate task, read out of method-assets through the SAME projectstate.GateTaskFor
// / AgentTaskFor tables the construction child workflow reads. Nothing here names a task
// id literally: a round that cited a task the pinned lifecycle does not carry would be a
// round no reader could place in the DAG, which is exactly what the LifecyclePin exists
// to prevent.
//
// AND WHERE THAT RESOLUTION FAILS, NOTHING IS WRITTEN. method-assets v0.9.0 models the
// requirements lifecycle (four phases) and the architecture lifecycle (one), so every
// Phase-1 kind resolves. The projectDesign lifecycle models ONLY the M0 SDP gate — one
// review task, "sdpReview", with no dispatch task under it — so none of the nine Phase-2
// artifact drafts has a review task to key a round on, and the Phase-2 rail therefore
// writes no round today. That is stated rather than papered over: inventing
// "planningAssumptionsReview" would put a task id in the ledger that no lifecycle has.
// EARMARK: extending the projectDesign lifecycle is a method-assets release (a founder
// STOP), and the moment it lands this code lights up with no edit.
// ---------------------------------------------------------------------------

// changeDesignRoundLedger is the ONE version marker gating every round-ledger write on
// this rail. One id, not one per write point, for the reason task 5 gives on the
// construction rail: the writes are one feature and a session is either on it or off it,
// where separate ids would admit a half-fenced session that opens a round and never
// decides it. It is resolved ONCE at session start (beginCoAuthorSession), the same
// discipline as "design-vibes-autogate": a session in flight at deploy time has no marker
// there, so it replays DefaultVersion and runs its WHOLE life on the old command sequence.
const changeDesignRoundLedger = "design-round-ledger"

// The design gate's ledger vocabulary.
const (
	// designRoleHuman is the role the human reviewer's verdict carries. It is the SAME
	// wire label their thread utterances carry (reviewerUtteranceRole), because it is the
	// same person: a round whose verdict said "architect" and whose comments said
	// "architect-user" would read as two reviewers.
	designRoleHuman = reviewerUtteranceRole
	// designActorOperator is who the platform can honestly name when a decision signal
	// carries no identity of its own. An approve DOES carry one (the approver, or the
	// vibes auto-approver), and that name is used instead wherever it is present.
	designActorOperator = "operator"
)

// designApproverActor is WHO the platform can honestly say approved a design gate: the
// identity the approve signal carried — a person, or the vibes auto-approver — falling back
// to the operator when the signal named nobody. Never fabricated.
func designApproverActor(approver string) string {
	if approver == "" {
		return designActorOperator
	}
	return approver
}

// designRoundKey is one design artifact kind's coordinates on the execution ledger.
type designRoundKey struct {
	// activityID is the design prefix activity the round hangs off — the id the derived
	// plan carries for it, which is the design activity type's own wire name.
	activityID string
	// typ is that same activity type in the store's vocabulary, for OpenActivity.
	typ projectstate.ActivityType
	// gate is the lifecycle phase's REVIEW task — the task this round IS an occurrence of.
	gate projectstate.MethodTask
	// work is the dispatch task the gate judges — the round's Reviews field, which the
	// store refuses to leave empty ("a round that judges no task judges nothing").
	work projectstate.MethodTask
}

// designRoundKeyFor resolves an artifact kind onto those coordinates, or reports that the
// pinned lifecycles carry no review task for it (see the section doc). Pure table lookup
// over designActivityFor + the method-assets-derived phase→task tables; no literals.
func designRoundKeyFor(kind projectstate.ArtifactKind) (designRoundKey, bool) {
	designType, lifecyclePhase := designActivityFor(kind)
	psType, ok := psActivityTypeFor(designType)
	if !ok {
		return designRoundKey{}, false
	}
	p := projectstate.ActivityMethodPhase(lifecyclePhase)
	gate, work := projectstate.GateTaskFor(p), projectstate.AgentTaskFor(p)
	if gate == "" || work == "" {
		return designRoundKey{}, false
	}
	return designRoundKey{activityID: string(designType), typ: psType, gate: gate, work: work}, true
}

// psActivityTypeFor maps the review engine's activity-type vocabulary onto the store's.
// The two are the same names either side of a layer boundary the engine cannot cross.
// Exhaustive with no default arm — the trailing return is the out-of-vocabulary catch, so
// a new member is a build-time decision rather than a value that silently classifies as
// "not a design type". The seven construction types are named and refused: they never
// reach a design rail, and a design round keyed on one would be a fabrication.
func psActivityTypeFor(t review.ActivityType) (projectstate.ActivityType, bool) {
	switch t {
	case review.ActivityTypeRequirements:
		return projectstate.ActivityTypeRequirements, true
	case review.ActivityTypeArchitecture:
		return projectstate.ActivityTypeArchitecture, true
	case review.ActivityTypeProjectDesign:
		return projectstate.ActivityTypeProjectDesign, true
	case review.ActivityTypeService, review.ActivityTypeFrontend, review.ActivityTypeTesting,
		review.ActivityTypeDeployment, review.ActivityTypeDocumentation, review.ActivityTypeUIDesign,
		review.ActivityTypeIntegration:
		return 0, false
	}
	return 0, false
}

// designRoundID names ONE design review round. It deliberately does NOT use
// projectstate.AttemptID's <activityId>:<taskId>:<n>, and the artifact kind in the middle
// is the reason: several kinds share one lifecycle phase (scrubbedRequirements shares the
// glossary phase; operationalConcepts and standardCheck share the architecture phase), so
// two independent co-author sessions would mint the SAME three-part id for their own first
// round. OpenReviewRound is idempotent on that id — the property the whole ledger rests on
// — so the second session's round would silently vanish into the first's, and its verdicts
// would append to a round that judged a different artifact. That is precisely the history
// corruption this wave exists to end, so the kind is part of the key.
func designRoundID(k designRoundKey, kind projectstate.ArtifactKind, round int) string {
	return fmt.Sprintf("%s:%s:%s:%d", k.activityID, k.gate, kind.WireName(), round)
}

// designRound is the round this session currently has open at the human gate.
type designRound struct {
	key designRoundKey
	// roundID is the store key; empty means "no round is open", which every write point
	// below treats as "write nothing" rather than as an error.
	roundID string
	// number is the round's 1-based occurrence number. The slot ledger's own round counter
	// is 0-based (its comment ids are r0c1, r1c1 …) and OpenReviewRound refuses a round
	// below 1, so the round number is the slot round PLUS ONE — stated here because the
	// comment-id mirror below depends on exactly that offset.
	number int
	// subject is what the round judges.
	subject projectstate.SubjectRef
	// attemptID is the draft attempt this round's verdicts judged, in the ledger's own key
	// format. The design rail does not yet WRITE the attempt ledger (stage 3 gives it
	// rounds only), so nothing joins to this id today; it is minted in the canonical format
	// so it joins the moment the rail records its draft attempts. EARMARK: stage 4.
	attemptID string
}

// roundCommentRef locates one comment on the round ledger — the round it landed in and the
// id the store minted for it there.
type roundCommentRef struct {
	roundID   string
	commentID string
}

// designSubjectRef names WHAT a design round judges. The commit sha of the draft is NOT
// knowable to this workflow — the design job's agent pushes it and the workflow only ever
// reads back a typed model and a version — so the honest subject is the pull request the
// reviewer actually opens when there is one, and the staged artifact on its branch when
// there is not. Deterministic: both halves come from recorded Activity results.
func designSubjectRef(gf gitSession, kind projectstate.ArtifactKind) projectstate.SubjectRef {
	if gf.enabled && gf.prRef != "" {
		return projectstate.SubjectRef{Kind: projectstate.SubjectPullRequest, Ref: gf.prRef}
	}
	if branch := gf.readBackBranch(); branch != "" {
		return projectstate.SubjectRef{Kind: projectstate.SubjectArtifact, Ref: kind.WireName() + "@" + branch}
	}
	return projectstate.SubjectRef{Kind: projectstate.SubjectArtifact, Ref: kind.WireName()}
}

// designRoundReviewers is the roster the round persists: the rows the review engine
// computed for this design gate, plus the human row when the policy holds for a person.
// The engine's rows are NOT Required — the design rail dispatches no reviewer from them —
// and a row marked required that nothing waits for would make the round claim a gate it
// never had. Same rule, same reason, as the construction rail's roundReviewers.
func designRoundReviewers(set review.ReviewSet) []projectstate.RoundReviewer {
	out := make([]projectstate.RoundReviewer, 0, len(set.Reviewers)+1)
	for _, r := range set.Reviewers {
		out = append(out, projectstate.RoundReviewer{Role: r.Role, Actor: r.Role, Required: false})
	}
	if set.RequiresHuman {
		out = append(out, projectstate.RoundReviewer{Role: designRoleHuman, Actor: designActorOperator, Required: true})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// openDesignRound opens the round for the gate the session has just reached, and lands the
// critic's verdict on it. It is called on EVERY entry to the AwaitingReview gate — a
// redraft's re-entry is a NEW round — so the ledger shows a send-back and its retry as two
// rounds rather than one mutated row.
//
// It is also where the activity row is born: OpenActivity is idempotent and write-once on
// the pin, so a second kind under the same prefix activity (operationalConcepts after
// system) resumes the row the first kind opened rather than re-dating it.
//
// THE CRASH WINDOW, STATED, exactly as the construction rail states it: a session that
// dies between the open and the decision leaves this round PENDING forever. A resume is a
// fresh workflow that opens the NEXT round (the slot's round counter is durable), so
// nothing collides and nothing is duplicated — but nobody goes back to close this one.
// Closing an abandoned round needs to know the session is gone, which a workflow cannot
// know about itself; the stage-4 sweep owns it, and RoundWithdrawn exists for it.
func (wf *workflows) openDesignRound(
	ctx workflow.Context,
	in coAuthorInput,
	gf gitSession,
	reviewRound int,
	state *coAuthorState,
) {
	state.round = designRound{}
	if !state.roundLedgerEnabled {
		return
	}
	kind := toPSKind(in.ArtifactKind)
	key, ok := designRoundKeyFor(kind)
	if !ok {
		// Not a fault: the pinned lifecycles carry no review task for this kind (see the
		// section doc). Logged, because a silent absence is what this wave is replacing.
		workflow.GetLogger(ctx).Info("round ledger: no review task in the pinned lifecycle for this kind; no round is opened",
			"artifactKind", artifactKindString(in.ArtifactKind))
		return
	}
	if !wf.openDesignActivity(ctx, in, key, state) {
		return
	}
	round := designRound{
		key:       key,
		roundID:   designRoundID(key, kind, reviewRound+1),
		number:    reviewRound + 1,
		subject:   designSubjectRef(gf, kind),
		attemptID: projectstate.AttemptID(key.activityID, key.work, reviewRound+1),
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, "", state.ledgerVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionOpenReviewRound(ctx, projectstate.ProjectID(in.ProjectID), expected, key.activityID,
			projectstate.ReviewRoundInput{
				RoundID:    round.roundID,
				TaskID:     key.gate,
				Reviews:    key.work,
				Round:      int64(round.number),
				SubjectRef: round.subject,
				Reviewers:  state.roundReviewers,
			}, projectstate.RepoCredential{})
	})
	if err != nil {
		// Best-effort, exactly like the thread reload beside it: the slot write is still the
		// read path for the length of the wave, so a ledger miss must not cost the reviewer
		// their gate. The round simply does not exist, and every write point below is a
		// no-op without it.
		workflow.GetLogger(ctx).Error("round ledger: could not open the review round; the session continues on the slot ledger alone",
			"artifactKind", artifactKindString(in.ArtifactKind), "err", err.Error())
		return
	}
	state.ledgerVersion = v
	state.round = round
	wf.appendCriticVerdict(ctx, in, state)
}

// openDesignActivity births the execution row and pins the lifecycle in force, once per
// session. The pin is what stops a method-assets release landing mid-session from
// re-shaping the DAG the rounds below were written under.
func (wf *workflows) openDesignActivity(ctx workflow.Context, in coAuthorInput, key designRoundKey, state *coAuthorState) bool {
	if state.activityOpened {
		return true
	}
	pin := projectstate.LifecyclePin{
		TypeKey:       projectstate.LifecycleKeyFor(key.typ, projectstate.TestVariantPlan),
		AssetsVersion: methodassets.Version(),
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, "", state.ledgerVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionOpenActivity(ctx, projectstate.ProjectID(in.ProjectID), expected,
			key.activityID, key.typ, projectstate.TestVariantPlan, pin, projectstate.RepoCredential{})
	})
	if err != nil {
		workflow.GetLogger(ctx).Error("round ledger: could not open the design activity row; no round is written this session",
			"activityId", key.activityID, "err", err.Error())
		return false
	}
	state.ledgerVersion = v
	state.activityOpened = true
	return true
}

// appendCriticVerdict lands the critique round's conclusion on the open round as an
// ORDINARY verdict — the disposition spec §5.3 gives ArtifactSlot.CritiqueVerdict /
// CritiqueNotes, whose content this is. (The two slot fields are NOT deleted here: they
// are the SPA's read path for the critique panel exactly as ReviewThread is for the
// thread, so they are deprecated in place and cut in stage 6 with it.)
//
// The role is the critic the Method assigns the kind — the product manager for the
// business-alignment steps, the architect for the architecture — carried verbatim off the
// view the workflow already stamped, which is the SAME wire label the review engine put on
// the roster, so the verdict row and its roster row name one reviewer rather than two.
//
// It is appended right after the round opens rather than when the critique was observed,
// because the critique runs BEFORE staging and there is no round yet at that point. The
// verdict recorded is therefore the one that judged the draft the human is about to see —
// including a REVISE that never converged, which reached the gate as a warning and belongs
// on the round as a send-back verdict rather than as an absence.
func (wf *workflows) appendCriticVerdict(ctx workflow.Context, in coAuthorInput, state *coAuthorState) {
	if state.critique == nil || state.critique.Role == "" {
		return
	}
	wf.appendDesignVerdict(ctx, in, state, projectstate.ReviewVerdict{
		ReviewerRole: state.critique.Role,
		Actor:        state.critique.Role,
		Verdict:      criticVerdictKind(state.critique.Verdict),
		Summary:      state.critique.Summary,
		AttemptID:    state.round.attemptID,
	}, nil, nil)
}

// criticVerdictKind maps a critique conclusion onto the round's verdict vocabulary. A
// REVISE is a send-back — it is the critic asking for another draft — and the other member
// of that closed two-value carrier is a ratification.
func criticVerdictKind(verdict string) projectstate.VerdictKind {
	if verdict == projectstate.CritiqueVerdictRevise {
		return projectstate.VerdictSendBack
	}
	return projectstate.VerdictApprove
}

// appendDesignVerdict lands one reviewer's judgement and the comments it cites on the open
// round, in ONE commit. It also remembers where each comment landed, so a later resolve /
// reopen of that comment can be mirrored onto the round without reading it back.
//
// A no-op when no round is open, and best-effort when one is: the slot ledger is still the
// read path, so a round write that faults must not take the reviewer's decision with it.
func (wf *workflows) appendDesignVerdict(
	ctx workflow.Context,
	in coAuthorInput,
	state *coAuthorState,
	verdict projectstate.ReviewVerdict,
	comments []projectstate.ReviewComment,
	slotIDs []string,
) {
	if state.round.roundID == "" {
		return
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, "", state.ledgerVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionAppendReviewVerdict(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.round.key.activityID, state.round.roundID, verdict, comments, nil, projectstate.RepoCredential{})
	})
	if err != nil {
		workflow.GetLogger(ctx).Error("round ledger: could not append the verdict; the slot ledger still carries the decision",
			"roundId", state.round.roundID, "reviewerRole", verdict.ReviewerRole, "err", err.Error())
		return
	}
	state.ledgerVersion = v
	state.rememberRoundComments(slotIDs, len(comments))
}

// rememberRoundComments records the slot-comment-id → round-comment-id pairing for the
// batch just appended, so a resolve / reopen filed against the SLOT's id can be mirrored
// onto the round.
//
// The pairing is computed, not read back, because both stores mint deterministically from
// the batch's own index: the slot stamps r{slotRound}c{i+1} (appendReviewComments) and the
// round stamps r{roundNumber}c{i+1} (applyRoundReviewBatch, onto a thread this rail only
// ever appends to once per round). The two differ ONLY in the round number, which is the
// +1 offset designRound.number documents. A batch whose two halves disagree in length is
// not paired at all rather than paired wrongly.
func (s *coAuthorState) rememberRoundComments(slotIDs []string, appended int) {
	if len(slotIDs) != appended || appended == 0 {
		return
	}
	if s.roundComments == nil {
		s.roundComments = map[string]roundCommentRef{}
	}
	for i, slotID := range slotIDs {
		s.roundComments[slotID] = roundCommentRef{
			roundID:   s.round.roundID,
			commentID: projectstate.ReviewCommentID(int64(s.round.number), i),
		}
	}
}

// decideDesignRound stamps the round's terminal — a separate, later fact from the verdicts
// on it, which is why the store makes it a second verb and not a field of the first.
// decidedBy is WHO settled it: the approver's own name where the signal carried one (the
// vibes auto-approver included, which is how an auto-approved design gate stops being
// invisible), and the operator otherwise.
func (wf *workflows) decideDesignRound(
	ctx workflow.Context,
	in coAuthorInput,
	state *coAuthorState,
	outcome projectstate.ReviewRoundOutcome,
	decidedBy string,
) {
	if state.round.roundID == "" {
		return
	}
	if decidedBy == "" {
		decidedBy = designActorOperator
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, "", state.ledgerVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionDecideReviewRound(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.round.key.activityID, state.round.roundID, outcome, decidedBy, projectstate.RepoCredential{})
	})
	if err != nil {
		workflow.GetLogger(ctx).Error("round ledger: could not decide the review round; it stays pending for the stage-4 sweep",
			"roundId", state.round.roundID, "outcome", string(outcome), "err", err.Error())
		return
	}
	state.ledgerVersion = v
	// The round is settled: the next gate entry opens a fresh one.
	state.round = designRound{}
}

// mirrorCommentStatus applies the reviewer's resolve / reopen to the ROUND thread as well
// as the slot thread. It fires only for comments this session itself appended to a round
// (rememberRoundComments): a comment seeded straight onto the slot — an amendment's
// reopening feedback, a failed-gate feedback seed, an asked question — has no round
// counterpart to move, and naming one would be a NotFound on every retry the Activity is
// given. EARMARK: those three seed doors join the round ledger in stage 4.
func (wf *workflows) mirrorCommentStatus(ctx workflow.Context, in coAuthorInput, state *coAuthorState, sig setCommentStatusSignal) {
	ref, ok := state.roundComments[sig.CommentID]
	if !ok {
		return
	}
	key, ok := designRoundKeyFor(toPSKind(in.ArtifactKind))
	if !ok {
		return
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, "", state.ledgerVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionSetReviewCommentStatus(ctx, projectstate.ProjectID(in.ProjectID), expected,
			key.activityID, ref.roundID, ref.commentID, sig.Status, projectstate.RepoCredential{})
	})
	if err != nil {
		workflow.GetLogger(ctx).Error("round ledger: could not mirror the comment status; the slot ledger carries it",
			"commentId", sig.CommentID, "status", sig.Status, "err", err.Error())
		return
	}
	state.ledgerVersion = v
}

// newSlotCommentIDs names the ids the SLOT ledger is about to mint for a fresh batch in
// round slotRound — r{slotRound}c{i+1}, the store's own deterministic minting — so the
// round-side pairing can be built without a read-back. Comments are the only half that
// mints ids; replies land inside threads that already have one.
func newSlotCommentIDs(slotRound int, comments []projectstate.ReviewComment) []string {
	out := make([]string, 0, len(comments))
	for i := range comments {
		out = append(out, projectstate.ReviewCommentID(int64(slotRound), i))
	}
	return out
}

// critiqueCriticFor returns the Method critic assigned to kind's critique round
// (and ok=false when the kind takes no critique round at all). The PM critiques
// the business-alignment kinds (mission / glossary+scrubbed / core-use-cases —
// rework §2.1, §6.6). The ARCHITECT self-critiques the architecture (KindSystem —
// QA amendment 2026-07-17: gtdapp's architecture draft reached the human gate
// with THREE blockers and zero internal critique; the ratified "architect-owned
// steps skip PM critique" doctrine stands — the PM must NOT critique
// architecture — so the critic for KindSystem is the architect itself, running
// the architect-role system-critique command). The remaining architect-owned
// Phase-1 kinds (volatilities, operational concepts, standard-check) and every
// Phase-2 kind still skip critique entirely (EARMARK: no live QA evidence yet;
// extend only on evidence).
//
// LOCKSTEP PIN: projectstate.DesignCommandFor's critique-slug gate
// (designKindHasCritique, resourceaccess/projectstate/projectstateaccess.go)
// is a deliberate, non-imported duplicate of this switch's case list — RA sits
// below this Manager layer and cannot import it. Edit both switches together.
func critiqueCriticFor(kind projectstate.ArtifactKind) (ActiveRole, bool) {
	switch kind {
	case projectstate.KindMission,
		projectstate.KindGlossary,
		projectstate.KindScrubbedRequirements,
		projectstate.KindCoreUseCases:
		return ActiveRoleProductManager, true
	case projectstate.KindSystem:
		return ActiveRoleArchitect, true
	case projectstate.KindVolatilities, projectstate.KindOperationalConcepts,
		projectstate.KindStandardCheck, projectstate.KindPlanningAssumptions, projectstate.KindActivityList,
		projectstate.KindNetwork, projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution,
		projectstate.KindCompressedSolution, projectstate.KindDecompressedSolution, projectstate.KindRiskModel,
		projectstate.KindSdpReview:
		// Uncritiqued architect-owned Phase-1 steps and all Phase-2 kinds
		// (Phase 2 has no critique step at all) — same as the default below.
		return ActiveRoleNone, false
	default:
		return ActiveRoleNone, false
	}
}

// designActivityFor maps an artifact kind onto the DESIGN activity the reviewEngine
// keys its rows on: the activity type and the lifecycle phase within it. It is the
// design rail's half of the vocabulary the engine's tables are total over (spec
// 2026-09-20 §5.4, stage 2); the construction rail's half is the ActivityMethodPhase
// wire names.
//
// The requirements activity carries the four business-alignment / volatility steps.
// scrubbedRequirements shares the GLOSSARY phase deliberately: the scrubbing pass runs
// with the glossary inside the-method-requirements-analysis step and shares its gate.
// The architecture activity carries the System draft and the two architect-owned
// documents that hang off it (operational concepts, standard check).
//
// EVERY Phase-2 kind maps to the projectDesign type at the kind's OWN wire name, and
// deliberately NOT to the phase id "sdp". "sdp" is the M0 gate of the projectDesign
// lifecycle, which the engine makes always-human because M0 approves spend; the nine
// Phase-2 artifact DRAFTS are not that gate, and mapping them there would gate nine
// drafts that auto-approve under vibes today. Pinned by
// Test_DesignActivityFor_Phase2KindsAreNotTheSdpGate.
func designActivityFor(kind projectstate.ArtifactKind) (review.ActivityType, string) {
	switch kind {
	case projectstate.KindMission:
		return review.ActivityTypeRequirements, "mission"
	case projectstate.KindGlossary, projectstate.KindScrubbedRequirements:
		return review.ActivityTypeRequirements, "glossary"
	case projectstate.KindVolatilities:
		return review.ActivityTypeRequirements, "volatilities"
	case projectstate.KindCoreUseCases:
		return review.ActivityTypeRequirements, "coreUseCases"
	case projectstate.KindSystem, projectstate.KindOperationalConcepts, projectstate.KindStandardCheck:
		return review.ActivityTypeArchitecture, "architecture"
	case projectstate.KindPlanningAssumptions, projectstate.KindActivityList, projectstate.KindNetwork,
		projectstate.KindNormalSolution, projectstate.KindSubcriticalSolution,
		projectstate.KindCompressedSolution, projectstate.KindDecompressedSolution,
		projectstate.KindRiskModel, projectstate.KindSdpReview:
		return review.ActivityTypeProjectDesign, kind.WireName()
	}
	// An out-of-vocabulary kind cannot reach here through the typed façade; route it to
	// the architect's own step so the engine still answers rather than refusing.
	return review.ActivityTypeArchitecture, "architecture"
}

// engineReviewPolicy converts the COMMITTED policy document into the reviewEngine's own
// copy — Preset dereferenced (nil ⇒ "", the legacy/explicit mode), GatedPhasesByType
// re-keyed to the phases' wire names because an Engine may not import projectstate (F3).
//
// BYTE-IDENTICAL COPY in internal/manager/construction/constructactivity.go and
// internal/manager/projectdesign/coauthorphase2artifact.go: three packages, and no
// shared home for a five-line conversion that would not cost an
// internal/arch_test.go allowlist entry. Edit all three together; their parity is
// pinned by Test_EngineReviewPolicy_CarriesTheStoredDocument in each package.
func engineReviewPolicy(p projectstate.ReviewPolicy) review.ReviewPolicy {
	out := review.ReviewPolicy{}
	if p.Preset != nil {
		out.Preset = *p.Preset
	}
	if len(p.GatedPhasesByType) > 0 {
		out.GatedPhasesByType = make(map[string][]string, len(p.GatedPhasesByType))
		for typ, phases := range p.GatedPhasesByType {
			names := make([]string, 0, len(phases))
			for _, ph := range phases {
				names = append(names, ph.String())
			}
			out.GatedPhasesByType[typ] = names
		}
	}
	return out
}

// sameArtifactModel PROMOTED to projectstate.SameArtifactModel
// (code-health-phase-bd task D3) — byte-identical pure comparator, no longer duplicated
// with projectdesign's twin.

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

// critiqueRoleProductManager / critiqueRoleArchitect are the CritiqueView.Role wire
// labels for the two critique-issuing roles (critiqueCriticFor). They match the SPA's
// ActiveRole wire naming ("productManager" / "architect") so both surfaces name the
// role identically.
const (
	critiqueRoleProductManager = "productManager"
	critiqueRoleArchitect      = "architect"
)

// critiqueRoleWire maps the critic ActiveRole to its CritiqueView.Role wire label.
func critiqueRoleWire(critic ActiveRole) string {
	if critic == ActiveRoleArchitect {
		return critiqueRoleArchitect
	}
	return critiqueRoleProductManager
}

// critiqueViewFor renders the observed critique conclusion as the SessionStateView
// carrier (F-QA2-7): Role names the actual critic (PM, or the architect self-critique
// for KindSystem), the wire verdict reuses the projectstate carrier's closed string
// set ("approve" | "revise"), Summary is the critic's rationale verbatim, and round is
// the redraft-round counter of the draft the critique judged. Pure mapping over the
// recorded read-back result — deterministic on replay, no history command.
func critiqueViewFor(c critique, critic ActiveRole, round int) *CritiqueView {
	verdict := projectstate.CritiqueVerdictApprove
	if c.Verdict == critiqueRevise {
		verdict = projectstate.CritiqueVerdictRevise
	}
	return &CritiqueView{
		Role:    critiqueRoleWire(critic),
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
	raOrphanFindings,     // SYS-RA-ORPHAN
	encapsulatesFindings, // SYS-ENCAPSULATES
	relDupFindings,       // SYS-REL-DUP
	// DV-CHAIN-CONNECTED (dvChainFindings) RETIRED 2026-07-30 (callchain-realization
	// Task 6): it demanded a Client-rooted chain and duplicated — and, under the
	// step-keyed DynamicView shape, CONTRADICTED — platform methodcheck's
	// CC-PATH-CONNECTED, which also blesses actor-rooted chains. The rule now lives
	// solely as CC-PATH-CONNECTED in framework-go/methodcheck.
	dvTitleFindings,       // DV-TITLE-EMPTY (F10)
	variationRefFindings,  // UC-VARIATION-REF
	glossaryFourQFindings, // GLOSS-FOURQ
	scrubbedIDFindings,    // SR-ID-UNIQUE
	opcTopicFindings,      // OPC-TOPIC-COVERAGE
}

// dvTitleFindings — DV-TITLE-EMPTY (error; F10 gate lint, QA amendment 2026-07-17: the
// gtdapp architecture staged dynamic views with empty titles and no lint flagged them).
// Every dynamic view must carry a non-empty, non-whitespace title — the title is the
// human name of the call chain at the review panel and in the rendered DSL; an untitled
// view is unreviewable.
func dvTitleFindings(kind ArtifactKind, draft projectstate.ArtifactModel) []Finding {
	if kind != KindSystem {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	var out []Finding
	for i, dv := range sys.DynamicViews {
		if strings.TrimSpace(dv.Title) != "" {
			continue
		}
		label := dv.Key
		if label == "" {
			label = fmt.Sprintf("dynamic view %d", i+1)
		}
		out = append(out, Finding{
			RuleID:   "DV-TITLE-EMPTY",
			Severity: SeverityError,
			Message:  fmt.Sprintf("dynamic view %q (use case %q) has an empty title; every dynamic view must carry a human-readable call-chain title.", label, dv.UseCaseID),
			Location: &Location{Ordinal: int64(i), Section: "dynamic view " + label},
		})
	}
	return out
}

// volatilityCoverageFindings — SYS-VOLATILITY-COVERAGE (error; F10 gate lint, QA
// amendment 2026-07-17: gtdapp committed volatilities that no component claimed and
// nothing flagged the hole). Every COMMITTED volatility must be encapsulated by a
// component — i.e. its name appears in some component's encapsulates prose — or carry
// an explicit disposition. The System model has no dedicated disposition field, so a
// disposition note also lives in a component's encapsulates prose (e.g. "storage
// volatility: deferred — variable, handled by configuration"); this lint therefore
// fires only on TOTAL SILENCE — a committed volatility that appears in NO component's
// encapsulates text at all. Nil for every other kind, for a nil/absent draft, and when
// no Volatilities model is committed yet.
func volatilityCoverageFindings(kind ArtifactKind, draft projectstate.ArtifactModel, committed *projectstate.Volatilities) []Finding {
	if kind != KindSystem || committed == nil {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	var encapsulations []string
	for _, c := range sys.Components {
		if e := strings.ToLower(strings.TrimSpace(c.Encapsulates)); e != "" {
			encapsulations = append(encapsulations, e)
		}
	}
	var out []Finding
	for i, v := range committed.Items {
		name := strings.ToLower(strings.TrimSpace(v.Name))
		if name == "" {
			continue // an unnamed volatility is the Volatilities artifact's own defect
		}
		mentioned := false
		for _, e := range encapsulations {
			if strings.Contains(e, name) {
				mentioned = true
				break
			}
		}
		if mentioned {
			continue
		}
		out = append(out, Finding{
			RuleID:   "SYS-VOLATILITY-COVERAGE",
			Severity: SeverityError,
			Message:  fmt.Sprintf("committed volatility %q is claimed by no component's encapsulates and carries no disposition note; encapsulate it in a component, or record an explicit disposition (e.g. \"%s: deferred — <why>\") in the owning component's encapsulates prose.", v.Name, v.Name),
			Location: &Location{Ordinal: int64(i), Section: "volatility " + v.Name},
		})
	}
	return out
}

// servicesExplosionFindings — SYS-SERVICES-EXPLOSION (warning; F10 gate lint, QA
// amendment 2026-07-17). The one-Manager-per-use-case anti-pattern (ch. 3 services
// explosion / functional decomposition in disguise): when the Manager count exactly
// equals the committed CORE use-case count AND at least 60% of Manager names mirror a
// core use case's name, the decomposition likely encapsulates use cases, not
// volatilities. Heuristic — a WARNING, not an error: a small system can legitimately
// land on equal counts, so the name-mirroring threshold gates the signal. Nil for every
// other kind, for a nil/absent draft, and when no CoreUseCases is committed yet.
func servicesExplosionFindings(kind ArtifactKind, draft projectstate.ArtifactModel, committed *projectstate.CoreUseCases) []Finding {
	if kind != KindSystem || committed == nil {
		return nil
	}
	sys, ok := draft.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	var managerStems []string
	var managerNames []string
	for _, c := range sys.Components {
		if c.Kind != projectstate.CompManager {
			continue
		}
		managerNames = append(managerNames, c.Name)
		managerStems = append(managerStems, normalizeNameToken(strings.TrimSuffix(strings.TrimSpace(c.Name), "Manager")))
	}
	var coreNames []string
	for _, d := range committed.Decisions {
		if d.UseCase.Classification == projectstate.ClassCore {
			coreNames = append(coreNames, normalizeNameToken(d.UseCase.Name))
		}
	}
	managers := len(managerStems)
	if managers == 0 || managers != len(coreNames) {
		return nil
	}
	mirrored, mirroredNames := mirroredManagerNames(managerStems, managerNames, coreNames)
	// >= 60% name-mirroring (integer arithmetic; no float drift in workflow code).
	if mirrored*100 < 60*managers {
		return nil
	}
	return []Finding{{
		RuleID:   "SYS-SERVICES-EXPLOSION",
		Severity: SeverityWarning,
		Message: fmt.Sprintf(
			"the System declares exactly one Manager per committed core use case (%d each) and %d of %d Manager names mirror a core use case (%s); this is the services-explosion fingerprint — a Manager encapsulates a VOLATILITY for a family of use cases, not a single use case. Re-examine the decomposition axis.",
			managers, mirrored, managers, strings.Join(mirroredNames, ", ")),
		Location: &Location{Section: "system managers"},
	}}
}

// mirroredManagerNames counts the Manager stems that mirror a committed core use-case
// name (substring containment either way, on normalized tokens) and returns the display
// names of the mirroring Managers for the finding message. Pure — deterministic over
// its inputs.
func mirroredManagerNames(managerStems, managerNames, coreNames []string) (int, []string) {
	mirrored := 0
	var mirroredNames []string
	for i, stem := range managerStems {
		if stem == "" {
			continue
		}
		for _, ucName := range coreNames {
			if ucName == "" {
				continue
			}
			if strings.Contains(ucName, stem) || strings.Contains(stem, ucName) {
				mirrored++
				mirroredNames = append(mirroredNames, managerNames[i])
				break
			}
		}
	}
	return mirrored, mirroredNames
}

// normalizeNameToken lowercases and strips every non-alphanumeric rune so Manager
// stems and use-case names compare on their word content ("Match Tradesman" ==
// "MatchTradesman" == "match-tradesman").
func normalizeNameToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
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

// encapsulatesFindings — SYS-ENCAPSULATES. Scoped to the volatility-OWNING kinds only
// (manager/engine/resourceAccess), per the-method-architecture doctrine (QA rider R5,
// 2026-07-17): Clients encapsulate the client volatility as a LAYER (a transport entry
// point owns no per-component volatility), and Resources/Utilities are required to be
// empty (a physical store or cappuccino-machine utility owns nothing). Firing on those
// kinds produced only false positives (gtdapp's approved architecture: 2 client ERR +
// 4 resource WARN, all bogus). The same M/E/RA non-empty rule is ALSO enforced hard on
// the write path by projectstate.RequireModelFields; this read-back twin keeps a
// pre-existing violating committed state rendering with the finding visible.
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
		switch c.Kind {
		case projectstate.CompManager, projectstate.CompEngine, projectstate.CompResourceAccess:
			// volatility-owning kinds — the rule applies
		case projectstate.CompClient, projectstate.CompResource, projectstate.CompUtility:
			continue // client/resource/utility legitimately carry an empty encapsulates
		}
		if strings.TrimSpace(c.Encapsulates) != "" {
			continue
		}
		label := componentDisplayLabel(c, i)
		out = append(out, Finding{
			RuleID:   "SYS-ENCAPSULATES",
			Severity: SeverityError,
			Message:  fmt.Sprintf("component %q has an empty encapsulates; a manager, engine, or resource-access must name the volatility it owns.", label),
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
		switch {
		case id == "":
			out = append(out, Finding{
				RuleID:   "SR-ID-UNIQUE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("scrubbed requirement %d has an empty id; every requirement needs a stable non-empty id.", i+1),
				Location: loc,
			})
		case seen[id]:
			out = append(out, Finding{
				RuleID:   "SR-ID-UNIQUE",
				Severity: SeverityError,
				Message:  fmt.Sprintf("scrubbed requirement id %q is duplicated; requirement ids must be unique.", id),
				Location: loc,
			})
		default:
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

// opcTopicFindings — OPC-TOPIC-COVERAGE is OBSOLETE under the Wave-2 typed
// DeploymentOperationsModel: the free-text decisions[].topic list it nudged over is gone,
// replaced by required typed fields (deploymentScenario, constructionVenue, scaling/infra
// blocks, trust summaries) that the schema itself enforces — there is nothing left to
// nudge. Kept as an inert rule (returns no findings) so the rule registry is unchanged;
// a typed-model successor is design-health's Wave-2 concern, not this seam.
func opcTopicFindings(_ ArtifactKind, _ projectstate.ArtifactModel) []Finding {
	return nil
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

func modeWire(m projectstate.CallMode) string {
	b, err := m.MarshalJSON()
	if err != nil {
		return fmt.Sprintf("mode(%d)", int(m))
	}
	return strings.Trim(string(b), `"`)
}
