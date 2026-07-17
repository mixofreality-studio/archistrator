package projectdesign

import (
	"bytes"
	"errors"
	"fmt"
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

// readProject runs the generated designSessionAccess.readProjectOnBranch invoker with
// branch=="" (B9 — the RA's own empty-branch fallback always serves main, pinned by
// TestDesignSessionAccess_ReadProjectOnBranch_EmptyBranchAlwaysBase) and returns the whole
// head-state aggregate. A brand-new project surfaces fwra.NotFound (see isReadNotFound).
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
// (see isReadNotFound). Replaces the wasteful whole-aggregate read that shipped the
// entire encoded Project across the Temporal Activity boundary for a uint64.
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
// Conflict re-read→re-apply loop. branch names the substrate the mutation targets so the
// Conflict re-read reads the RIGHT version (the session branch for a review-window branch
// mutation, main for a main mutation) — see readVersionOnBranch (QA F29). branch=="" is
// the original main-only behavior every existing caller relied on.
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
	//
	// INVARIANT (set by the manager's amendmentIndexFor): N >= 1 IFF the slot was COMMITTED
	// at request time — the amendment condition. The manager floors a committed slot to 1
	// (a slot committed before the Revisions field existed reads Revisions=0 but is still an
	// amendment). So the spine's "Amendment > 0" checks (branch suffix, amendment prompt
	// framing, and the maybeSeedAmendment ledger seed) are a faithful proxy for "amendment"
	// and fire for EVERY committed slot, including pre-field ones.
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
	// Approver is the human-facing label for the acting identity that submitted this decision
	// (PM-P2-4), derived from the SubmitReviewDecision caller's SecurityPrincipal. Consulted on
	// Approve → recorded as the commit's approvedBy provenance. Empty when no identity reached
	// the manager op. Additive — an older buffered signal decodes it as "".
	Approver string
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

	feedback := ReviewFeedback{}
	if in.Feedback != nil {
		feedback = *in.Feedback
	}
	// An AMENDMENT session's reopening feedback is OWNED by the amendment seed path
	// (maybeSeedAmendment, below) — it lands in the ledger at round 0 right after the first
	// stage. Mark it seeded up front so the pre-dispatch failed-gate seed does not race that
	// path and double-seed the same comments on the first draft. A non-amendment session's
	// initial feedback is NOT ledger-backed, so it stays false and is seeded before its first
	// dispatch like any other memory-only feedback.
	if in.Amendment > 0 {
		state.feedbackSeeded = true
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
		step, outcome, err := wf.coAuthorDraftRound(ctx, in, proj, &feedback, &headVersion, &redraftCount, &reviewRound, state, &gf)
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
// amendmentNoChangeGate is the F40 zero-new-commit defense-in-depth guard, extracted from
// coAuthorDraftRound to keep it under the gocognit budget. For an amendment (in.Amendment > 0)
// it compares the branch read-back model against the committed main model: an amendment branch
// is cut from main (which already carries the committed model), so — unlike a first draft on an
// empty slot — the read-back succeeds even when the job advanced the branch by nothing, and a
// PR opened on such a branch 422s ("no commits between base and head"). Byte-identical ⇒ no
// change ⇒ drive the StageDraftFailed recovery (Retry redrafts on the same branch, Withdraw/other
// terminates). Returns coAuthorProceed + nil err when the branch advanced OR this is not an
// amendment — the caller then opens the PR.
func (wf *workflows) amendmentNoChangeGate(
	ctx workflow.Context,
	in coAuthorInput,
	proj projectstate.Project,
	branchModel projectstate.ArtifactModel,
	headVersion projectstate.Version,
	feedback *ReviewFeedback,
	redraftCount *int,
	state *coAuthorState,
) (coAuthorStep, coAuthorOutcome, error) {
	if in.Amendment == 0 {
		return coAuthorProceed, coAuthorUnknown, nil
	}
	unchanged, cmpErr := sameArtifactModel(branchModel, slotFor(proj, toPSKind(in.ArtifactKind)).Model)
	if cmpErr != nil {
		return coAuthorProceed, coAuthorUnknown, fwmanager.MapError(cmpErr)
	}
	if !unchanged {
		return coAuthorProceed, coAuthorUnknown, nil
	}
	workflow.GetLogger(ctx).Warn("amendment draft committed no change to the artifact; entering StageDraftFailed")
	outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, amendmentNoChangeReason(), state, feedback)
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

// containAtFailedGate suspends the session at the human-visible StageDraftFailed gate with
// the given reason and maps the human decision back into the draft-round control triple:
// Withdraw/other outcome → coAuthorReturn; a Retry → coAuthorContinue (redraft on the SAME
// persistent session branch, counter bumped). Shared by every draft-round failure that must
// be CONTAINED rather than crash the workflow (job-failed, malformed read-back, and the F35-
// twin OpenBranch/openPR rail faults). A recovery-await fault propagates as-is.
func (wf *workflows) containAtFailedGate(
	ctx workflow.Context,
	in coAuthorInput,
	headVersion projectstate.Version,
	reason string,
	state *coAuthorState,
	feedback *ReviewFeedback,
	redraftCount *int,
) (coAuthorStep, coAuthorOutcome, error) {
	outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, reason, state, feedback)
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

// dispatchDraftAndReadBack runs ONE dispatch → observe → read-back on the session branch. On
// success it returns the read-back model + version and coAuthorProceed with a nil error. On a
// terminal job failure or malformed read-back it CONTAINS at the failed gate and returns the
// resulting control triple; on an infra escalation (dispatch/observe retry-budget exhaustion)
// it returns coAuthorProceed WITH the error so the caller closes the workflow. Extracted from
// coAuthorDraftRound so the resume path (which SKIPS this block) reads cleanly and the function
// stays within the gocognit budget.
func (wf *workflows) dispatchDraftAndReadBack(
	ctx workflow.Context,
	in coAuthorInput,
	_ projectstate.Project,
	gf *gitSession,
	sessionBranch string,
	feedback *ReviewFeedback,
	headVersion *projectstate.Version,
	redraftCount *int,
	reviewRound *int,
	state *coAuthorState,
) (projectstate.ArtifactModel, projectstate.Version, coAuthorStep, coAuthorOutcome, error) {
	logger := workflow.GetLogger(ctx)
	// FAILED-GATE FEEDBACK SEED (thin-dispatch). The memory-only failed-gate recovery paths
	// (a redraft signal, a Retry-via-Reject at a failed gate, a faulted reject) retain the
	// architect's feedback in the workflow's feedback variable ONLY — unlike the review-gate
	// reject and the amendment seed, which fold it into the DURABLE review ledger. Under thin
	// dispatch the drafting agent reads context ONLY via getReviewThread, so that memory-only
	// feedback would evaporate. Seed it here, right BEFORE the redraft dispatch, reusing the
	// SAME seeding activity + comment conversion the reject path uses, so the agent reads it off
	// the branch. state.feedbackSeeded gates it — an already-seeded reject/amendment path is
	// skipped so its comments are never double-seeded.
	//
	// Temporal versioning guard (replay safety; mirrors the managed-scaffold-sync gate in
	// beginSession): this seed was ADDED to the redraft dispatch path AFTER the CoAuthor workflow
	// first shipped, so a design session already in flight at deploy time has NO history event
	// for it — replaying such a history against unguarded new code fails the workflow task with a
	// non-determinism error. GetVersion pins pre-feature executions (DefaultVersion) to the OLD
	// command sequence (they skip the seed for their WHOLE run), while every execution STARTED
	// after this deploy resolves v1 and seeds before each memory-only redraft. The founder's
	// deploy drains in-flight design workflows first, so this gate is belt-and-braces.
	if workflow.GetVersion(ctx, "failed-gate-ledger-seed-p2", workflow.DefaultVersion, 1) >= 1 {
		if !state.feedbackSeeded && wf.seedFailedGateFeedback(ctx, in, *gf, *headVersion, feedback, reviewRound, state) {
			state.feedbackSeeded = true
		}
	}
	// REVIEW LEDGER: on a redraft, the durable open comments (state.reviewThread, reloaded after
	// the reject-append or the failed-gate seed above) and the reopening feedback reach the
	// drafting agent via the ledger it reads with getReviewThread — no longer woven into a prompt.
	//
	// SUB-STEP (Plan-3 C2): the architect is now drafting (round 0) or revising (round N>0) on
	// this session branch — Phase 2 has NO PM critique, so this is the ONLY role this workflow
	// ever stamps. Stamp it for the loading pill immediately BEFORE the dispatch; it is cleared
	// the instant the job is observed done (success below, or a terminal fault routed through
	// the StageDraftFailed gate, whose belt-and-braces clear lives in awaitDraftFailedRecovery).
	if *redraftCount == 0 {
		state.markActive(ActiveRoleArchitect, ActiveStepDrafting, *redraftCount)
	} else {
		state.markActive(ActiveRoleArchitect, ActiveStepRevising, *redraftCount)
	}
	draftObs, derr := wf.dispatchAndObserve(ctx, dispatchDesignJobArgs{
		ProjectID:     in.ProjectID,
		ArtifactKind:  in.ArtifactKind,
		TargetBranch:  sessionBranch,
		PriorStateRef: "",
		// Per-project-design-dispatch: dispatch to the per-project repo + aiarch-design.yml
		// (the rail's repoRef). "" when the rail is dormant ⇒ RA falls back to construction.
		TargetRepo: gf.dispatchRepo(),
	})
	if derr != nil {
		// A TRANSIENT dispatch/observe fault that exhausted its retry budget is an
		// infrastructure escalation (not a ran-but-failed job): close the workflow.
		return nil, 0, coAuthorProceed, coAuthorUnknown, derr
	}
	if draftObs.Phase != pipelineSucceeded {
		// The job RAN and FAILED (drafting failed or the required CI validation check went red):
		// land at the human StageDraftFailed gate (§0.5.4 anti-wedge) — never a crash/wedge.
		logger.Warn("Phase-2 design draft job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", draftObs.Diagnostic)
		step, outcome, err := wf.containAtFailedGate(ctx, in, *headVersion, draftFailedReason(draftObs.Diagnostic), state, feedback, redraftCount)
		return nil, 0, step, outcome, err
	}
	// READ-BACK on the SESSION BRANCH (§2a): the Action committed the typed Phase-2 JSON on the
	// session branch; read it back as the not-yet-merged draft (dormant rail reads main). The
	// read-back CONFIRMS a commit landed before openPR opens the PR (F40).
	model, readBackVersion, rbErr := wf.readBackCommittedModelOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch())
	if rbErr != nil {
		if decodeMsg, terminal := isTerminalReadBack(rbErr); terminal {
			// The committed draft DECODES MALFORMED (QA F36) — a terminal fault retry cannot fix.
			// Land at the StageDraftFailed gate carrying the decode diagnostic.
			logger.Warn("Phase-2 read-back decoded MALFORMED committed state; entering StageDraftFailed", "error", decodeMsg)
			step, outcome, err := wf.containAtFailedGate(ctx, in, *headVersion, readBackDecodeFailedReason(decodeMsg), state, feedback, redraftCount)
			return nil, 0, step, outcome, err
		}
		return nil, 0, coAuthorProceed, coAuthorUnknown, rbErr
	}
	// SUB-STEP (Plan-3 C2): the draft dispatch is observed complete — clear the in-flight
	// architect stamp. There is no PM critique to re-stamp next; the caller proceeds straight
	// to staging, where the AwaitingReview clear (below) is then a no-op.
	state.clearActive()
	return model, readBackVersion, coAuthorProceed, coAuthorUnknown, nil
}

func (wf *workflows) coAuthorDraftRound(
	ctx workflow.Context,
	in coAuthorInput,
	proj projectstate.Project,
	feedback *ReviewFeedback,
	headVersion *projectstate.Version,
	redraftCount *int,
	reviewRound *int,
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

	// RESUME CHECKPOINT (F35 twin): consume the marker. When set, a PRIOR attempt of THIS
	// session already committed the draft on the branch and then faulted at a POST-read-back
	// rail step (openPR) — so this Retry must NOT re-dispatch (Claude onto a branch that
	// already carries the model would red the no-commit guard). Cleared here; re-armed only if
	// openPR faults again below.
	resuming := state.resumeFromReadBack
	state.resumeFromReadBack = false

	// Rail (dispatch-time half): mint the credential + ensure the session branch
	// exists BEFORE the Action drafts on it. A dormant rail returns a disabled session
	// and the spine runs unchanged (read-back/stage on main, no branch/PR ops).
	begun, gerr := wf.beginSession(ctx, in.ProjectID, sessionBranch)
	if gerr != nil {
		if temporal.IsCanceledError(gerr) {
			return coAuthorProceed, coAuthorUnknown, gerr
		}
		// OpenBranch / mintCred faulted BEFORE any draft landed — even after the shared bounded
		// Auth retry exhausted (a genuine permission denial or a persistent secondary-rate-limit
		// 403). CONTAIN it (never crash the whole CoAuthor workflow): land at StageDraftFailed.
		// Pre-read-back, so a Retry safely re-dispatches (no resume marker is set).
		logger.Warn("Phase-2 session begin (OpenBranch) faulted after the bounded Auth retry; entering StageDraftFailed", "error", gerr.Error())
		return wf.containAtFailedGate(ctx, in, *headVersion, railStepFailedReason("preparing the review branch", gerr), state, feedback, redraftCount)
	}
	*gf = begun

	var (
		model           projectstate.ArtifactModel
		readBackVersion projectstate.Version
		haveDraft       bool
	)
	if resuming {
		// RESUME PROBE (F35 twin): re-run the read-back FIRST. The draft is already committed on
		// the branch from the faulted attempt; if it is present + decodes, SKIP the re-dispatch —
		// a re-dispatch would red the no-commit guard on a branch that already carries the model
		// and would burn another 20+ minute draft.
		if m, v, rbErr := wf.readBackCommittedModelOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch()); rbErr == nil {
			model, readBackVersion, haveDraft = m, v, true
			logger.Info("resuming Phase-2 draft round from read-back; skipping re-dispatch (draft already committed on the branch)")
		} else {
			logger.Warn("resume read-back found no usable draft; re-dispatching a fresh draft", "error", rbErr.Error())
		}
	}
	if !haveDraft {
		m, v, step, outcome, err := wf.dispatchDraftAndReadBack(ctx, in, proj, gf, sessionBranch, feedback, headVersion, redraftCount, reviewRound, state)
		if step != coAuthorProceed || err != nil {
			return step, outcome, err
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
	if step, outcome, err := wf.amendmentNoChangeGate(ctx, in, proj, model, *headVersion, feedback, redraftCount, state); step != coAuthorProceed || err != nil {
		return step, outcome, err
	}

	// Rail: open the PR (head=sessionBranch, base=main) ONLY NOW — AFTER the read-back
	// CONFIRMED a committed model on the session branch, so the branch has ≥1 commit beyond
	// main and GitHub will not 422 "no commits between base and head" (F40 fix; observed on
	// gtdapp). Idempotent on head — reject/redraft rounds reuse the SAME PR; the server's
	// handle is authoritative for the merge step.
	if err := wf.openPR(ctx, gf, in.ArtifactKind); err != nil {
		if temporal.IsCanceledError(err) {
			return coAuthorProceed, coAuthorUnknown, err
		}
		// POST-read-back rail fault after the shared bounded Auth retry exhausted (QA F35 twin):
		// a genuine permission denial or a persistent secondary-rate-limit 403. The draft is
		// ALREADY committed on the session branch, so DO NOT crash and DO NOT let a naive Retry
		// re-dispatch (that would red the no-commit guard). CONTAIN at the failed gate AND
		// checkpoint a read-back RESUME, so the Retry re-opens the PR on the preserved draft
		// without burning another 20+ minute draft.
		state.resumeFromReadBack = true
		logger.Warn("Phase-2 openPR faulted after read-back (bounded Auth retry exhausted); entering StageDraftFailed — retry resumes from read-back, no re-dispatch", "error", err.Error())
		return wf.containAtFailedGate(ctx, in, *headVersion, railStepFailedReason("opening the review pull request", err), state, feedback, redraftCount)
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
		return wf.Acts.DesignSessionStageArtifactForReviewOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, gf.readBackBranch(), draftEnvelope)
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
	// SUB-STEP (Plan-3 C2): staged for the human gate — no role is working. Belt-and-braces
	// (the draft success path already cleared its stamp before returning, above).
	state.clearActive()
	// A fresh AwaitingReview supersedes any prior approve-fault notice (QA F35 —
	// systemdesign twin parity): without this a send-back after a contained merge-window
	// fault would carry the stale "approving did not complete" notice into the NEXT
	// review round's gate.
	state.failureReason = ""
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
	feedback *ReviewFeedback,
	state *coAuthorState,
) (coAuthorStep, coAuthorOutcome, error) {
	// F-QA2-41 (systemdesign twin parity): a fresh decision at this gate supersedes any
	// prior approve/withdraw-fault notice — clear it so the NEXT stage (Committed on a
	// successful re-approve, Redrafting on a send-back) never carries the stale notice
	// forward. A decision arm that faults below re-stamps its own notice
	// (reAwaitAfterApproveFault). Workflow-local view state served by the query; setting
	// it issues NO history command, so no GetVersion gate is needed.
	state.failureReason = ""
	// F-QA2-44 (systemdesign twin parity): one monotonic sequence number per HANDLED
	// review decision — replay-stable, driven purely by the recorded signal order. It keys
	// the PER-ATTEMPT version gate (gate-decision-token-remint-p2-<seq>) guarding the
	// approve arm's credential re-mint; see coAuthorApprove for why the gate must be
	// per-attempt. Pure workflow-local bookkeeping — no history command. Consumer audit:
	// only the APPROVE arm consumes the session's cached rail credential (mergeOnApprove);
	// Reject / Withdraw / waive-reopen and the failed-gate paths ride designSessionAccess
	// activities that carry no workflow-cached credential, and a failed-gate Retry's
	// re-dispatch re-mints in beginSession — so only the approve arm re-mints.
	state.decisionSeq++
	switch sig.Decision {
	case ReviewApprove:
		// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open.
		// The manager precondition rejects this synchronously; this is the TOCTOU-safe backstop.
		// Re-suspend at the gate; the reviewer sees the open comments in the queryable thread.
		if open := openReviewCommentIDs(state.reviewThread); len(open) > 0 {
			return coAuthorReAwait, coAuthorUnknown, nil
		}
		return wf.coAuthorApprove(ctx, in, gf, headVersion, redraftCount, feedback, state, sig.Approver)

	case ReviewReject:
		notes := signalNotes(sig.Feedback)
		// RETAIN the architect's feedback in workflow state BEFORE the head-state write, so
		// that if the reject write itself faults (below), the crash-containment recovery
		// gate still holds the feedback for a Retry instead of silently discarding the
		// send-back (QA F28). Retain the FULL ReviewFeedback (Notes + anchored Comments) so a
		// faulted-reject Retry can seed those comments to the ledger before redraft (thin dispatch).
		*feedback = reviewFeedbackOrZero(sig.Feedback)
		// Not YET in the ledger — the reject write below seeds it (and flips this true on
		// success). If that write FAULTS (crash containment, below), the flag stays false so the
		// failed-gate seed persists these comments before the Retry redraft dispatch.
		state.feedbackSeeded = false
		newVersion, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			// REVIEW LEDGER (review-ledger §2): fold the reviewer's anchored comments into the
			// reject as durable, server-minted ledger entries, round-stamped by the per-reject
			// review-round counter (replay-stable monotonic → deterministic, non-colliding ids on
			// the ONE accumulating thread — F40). Empty ⇒ plain reject.
			//
			// Branch-aware Reject (I-DESIGN-DISPATCH §2a): record the Rejected status on the
			// SESSION BRANCH the draft was staged on — where the staged model exists and the
			// session-branch version matches. In the PR rail main is untouched until an
			// approved draft merges, so a main-path reject would mismatch the version AND find
			// the slot unpopulated (the QA F28 crash). "" when the rail is dormant ⇒ the reject
			// lands on main exactly as before.
			return wf.Acts.DesignSessionRejectArtifactOnBranchWithComments(ctx, projectstate.ProjectID(in.ProjectID), expected, gf.readBackBranch(), toPSKind(in.ArtifactKind), notes, int64(*reviewRound), feedbackToLedgerComments(sig.Feedback))
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
		// The reject folded the architect's anchored comments into the ledger
		// (feedbackToLedgerComments, above), so this feedback is durably seeded — the pre-dispatch
		// failed-gate seed skips it (no double-seed).
		state.feedbackSeeded = true
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
		// SUB-STEP (Plan-3 C2): the reject is observed done — clear any stale stamp before the
		// outer loop re-enters coAuthorDraftRound (which does branch-prep work BEFORE its own
		// markActive call ahead of the next dispatch) — no role is working during that window.
		state.clearActive()
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
			return wf.Acts.DesignSessionWithdrawArtifactOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, gf.readBackBranch(), toPSKind(in.ArtifactKind), notes)
		}); err != nil {
			return coAuthorProceed, coAuthorUnknown, err
		}
		state.stage = StageWithdrawn
		state.clearActive() // SUB-STEP (Plan-3 C2): terminal — no role is working.
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
	feedback *ReviewFeedback,
	state *coAuthorState,
	approver string,
) (coAuthorStep, coAuthorOutcome, error) {
	logger := workflow.GetLogger(ctx)

	// F-QA2-44 (systemdesign twin parity): RE-MINT the installation token at gate-decision
	// time. The session's cached credential (gf.cred) was minted at DISPATCH time
	// (beginSession), and GitHub App installation tokens expire after ~1 hour — while this
	// approve arrives whenever the human returns to the review (observed live on the
	// systemdesign spine: an approve 8+ hours after the last dispatch 403'd forever on the
	// expired token; the platform classifier reports that 403 as a NON-RETRYABLE Auth
	// fault, so neither the Activity RetryPolicy nor railWithAuthRetry could heal it).
	// Mint a FRESH token for THIS decision's merge window; the dispatch-time mint is
	// unchanged (it still covers sync / openBranch / openPR in the dispatch scope).
	//
	// Temporal versioning guard (replay safety): the mint is a NEW Activity command in the
	// decision path, so an in-flight session whose history recorded merge-window verbs
	// WITHOUT a preceding mint would replay non-deterministically against unguarded new
	// code. The change id is PER DECISION ATTEMPT (gate-decision-token-remint-p2-<seq>) —
	// deliberately NOT a static id — because GetVersion caches its resolution PER CHANGE ID
	// for the lifetime of the execution: a static id resolved to DefaultVersion while
	// replaying an old recorded attempt would pin every FUTURE approve on that execution to
	// the old no-mint behavior too, and a stuck live session could never heal. With a
	// per-attempt id, each OLD recorded attempt resolves DefaultVersion (no mint — replay
	// matches its history) while the NEXT decision on the SAME execution is a first-time
	// GetVersion in executing mode → v1 → re-mints. Decisions are human-gated (a handful
	// per session), so marker/search-attribute growth is bounded.
	if gf.enabled {
		if workflow.GetVersion(ctx, fmt.Sprintf("gate-decision-token-remint-p2-%d", state.decisionSeq), workflow.DefaultVersion, 1) >= 1 {
			cred, cerr := wf.mintCred(ctx, gf.repoRef)
			if cerr != nil {
				// Contain exactly like a merge-window fault (QA F35): the staged draft is
				// intact on the session branch and main is untouched — return to
				// AwaitingReview so the human simply re-approves. Cancellation propagates.
				if temporal.IsCanceledError(cerr) {
					return coAuthorProceed, coAuthorUnknown, cerr
				}
				logger.Warn("approve-time credential re-mint fault; returning to AwaitingReview for re-approve", "error", cerr.Error())
				return wf.reAwaitAfterApproveFault(state, approveFailedReason(cerr)), coAuthorUnknown, nil
			}
			gf.cred = cred
		}
	}

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
		// PM-P2-4 commit provenance: the approving identity + the drafting rail identity.
		return wf.Acts.DesignSessionCommitArtifactWithProvenance(ctx, projectstate.ProjectID(in.ProjectID), expected, toPSKind(in.ArtifactKind), approver, railDraftedBy(in.Amendment))
	}); err != nil {
		// QA F35: contain a post-merge commit fault too (same idempotent re-approve recovery).
		if temporal.IsCanceledError(err) {
			return coAuthorProceed, coAuthorUnknown, err
		}
		logger.Warn("approve post-merge commit fault; returning to AwaitingReview for re-approve", "error", err.Error())
		return wf.reAwaitAfterApproveFault(state, approveFailedReason(err)), coAuthorUnknown, nil
	}
	state.stage = StageCommitted
	state.clearActive() // SUB-STEP (Plan-3 C2): terminal — no role is working.
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
	// SUB-STEP (Plan-3 C2): back at the human gate for a re-approve — no role is working.
	state.clearActive()
	return coAuthorReAwait
}

// approveFailedReason renders the human "why" for the AwaitingReview re-approve notice when
// an approve/merge-window activity faulted (QA F35). It frames a re-approve, NOT a redraft.
// Wording per F-QA2-41 as corrected by F-QA2-44 (systemdesign twin parity): the 403 copy is
// CAUSE-NEUTRAL — a 403 here is not reliably a rate limit (the live gtdapp incident was an
// EXPIRED installation token), so the notice neither blames a rate limit nor promises that
// waiting helps; it points at the operator credential on repetition instead. Deterministic
// across replay (pure string ops on the reconstructed error).
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
	feedback *ReviewFeedback,
) (coAuthorOutcome, bool, error) {
	// Surface the human-visible failed stage + the pre-formatted human reason for the
	// Query. Callers pass the rendered reason (draftFailedReason for a job failure,
	// rejectFailedReason for a review-write fault) so this gate is reason-agnostic.
	state.stage = StageDraftFailed
	state.failureReason = reason
	// SUB-STEP (Plan-3 C2): the failed-gate sink for EVERY draft/stage/reject/approve fault —
	// no role is working while the human decides Retry/Withdraw. This is the SINGLE clear that
	// covers every StageDraftFailed entry (the job-failed and terminal-readback-decode branches
	// route here with no per-site clear of their own — safe, since Temporal never answers a
	// query mid-workflow-task, only at the next blocking point, which is this function's own
	// selector wait below).
	state.clearActive()

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
				// F47: MERGE the request feedback (from RequestArtifactDraft) with any gate-
				// retained feedback — the request WINS/appends — so the operator's new
				// instruction reaches the next draft dispatch without discarding retained context.
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
				// the CORE gap thin dispatch exposes — at a FAILED gate (unlike the review gate)
				// the reject never touches the ledger, so the feedback is memory-only until the
				// pre-dispatch failed-gate seed persists its anchored comments before the redraft.
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

// reviewFeedbackOrZero dereferences the signal's optional ReviewFeedback, returning the zero
// value (empty Notes, no Comments) when absent. Used on the Reject / Retry-via-Reject paths,
// whose anchored Comments are seeded into the review ledger before the redraft (thin dispatch).
func reviewFeedbackOrZero(f *ReviewFeedback) ReviewFeedback {
	if f != nil {
		return *f
	}
	return ReviewFeedback{}
}

// railStepFailedReason renders the human "why" for the StageDraftFailed screen when a rail
// step in the draft round (OpenBranch or OpenPullRequest) faulted AFTER the shared bounded
// workflow-side Auth retry exhausted (QA F35 twin) — a genuine permission denial or a
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

// dispatch.go is the AGENTIC-PIVOT seam (D-MPD-Δ, projectDesignManager.md §0.5) —
// the Phase-2 TWIN of systemdesign/dispatch.go. The Phase-2 plan-DRAFTING mechanism
// flips from a synchronous workerAccess call to an ASYNC dispatch → observe →
// read-back round-trip:
//
//   - DISPATCH  the Manager selects the Method Phase-2 role .claude command slug
//               (DesignCommandFor) and dispatches a claude-code-action DESIGN job via
//               the FROZEN constructionPipelineAccess.SubmitConstructionPipeline verb,
//               carrying {artifact_kind, command, target_branch, prior_state_ref,
//               job_mode} on the additive PipelineSpec.DispatchInputs field
//               (C-WF-DESIGN input schema). The Method Phase-2 doctrine lives in the
//               command's method-assets, not a composed in-memory prompt. The RA
//               reserves + stamps idempotency_token itself; the Manager MUST NOT set it.
//   - OBSERVE   the Manager polls ObserveConstructionPipeline(handle) between
//               durableExecutionAccess timer waits until a TYPED terminal phase.
//   - READ-BACK on PhaseSucceeded the Manager reads the committed typed Phase-2 Kind
//               via projectStateAccess.ReadProject (the Action committed the JSON;
//               aiarch writes nothing on the draft path).
//
// The ONE structural difference from the twin (projectDesignManager.md §0.5.5): the
// three estimation Engines (constructionEstimationEngine / operationEstimationEngine
// / settlementEngine) STAY server-side in-workflow — they are deterministic, pure,
// by-value joins, NOT LLM work, and do NOT dispatch. There is also NO PM-critique in
// Phase 2 (the architect owns the project-design artifacts and recommends to
// management at the SDP gate), so this file has NO critique round-trip — only the
// DRAFT round-trip. workerAccess and artifactValidationEngine are DROPPED from the
// draft path (§0.5.5).
//
// THE IDEMPOTENCY KEY IS DERIVED INSIDE THE DISPATCH ACTIVITY (construction note
// N1). Temporal assigns a distinct ActivityID per ExecuteActivity invocation and
// reuses it across automatic retries of that one invocation. So a REDRAFT loop
// (a fresh ExecuteActivity(DispatchDesignJobActivity)) gets a new ActivityID → a
// distinct key → a fresh, idempotent job (NOT a dedup of the stale prior job); a
// transient auto-retry of a single dispatch keeps the ActivityID → same key → the
// FROZEN submit verb collapses it to the same handle.

// ===========================================================================
// Workflow-side pipeline helpers. The temporalgen migration routes the submit/observe
// design-job pair through the GENERATED constructionPipelineAccess invokers (wf.Acts.
// PipelineSubmit/ObserveConstructionPipeline); the value mapping that lived on the folded
// pipelineDispatchAdapter — the RepoRef→RepoTarget decode, the PipelineSpec composition,
// and the RA-phase→neutral-phase mapping — is now these PURE workflow-side helpers
// (mirrors construction's dispatch.go). The idempotency key is stamped INSIDE the
// generated submit Activity (genActivityIdempotencyKey, the same run-scoped 3-part scheme
// the old hand-derived key used), so the redraft-vs-auto-retry distinction is unchanged.
// ===========================================================================

// dispatchDesignJob composes the constructionpipeline.PipelineSpec for one design job and
// submits it through the generated invoker, returning the opaque handle. The four DESIGN
// parameters ride on DispatchInputs; a per-project TargetRepo (decoded from the opaque
// RepoRef) + WorkflowFile target the user's per-project repo + aiarch-design.yml, else an
// empty target falls back to the RA's configured construction repo.
func (wf *workflows) dispatchDesignJob(ctx workflow.Context, a dispatchDesignJobArgs) (constructionpipeline.PipelineHandle, error) {
	// The .claude command slug the design job runs — the Phase-2 doctrine that used to be
	// composed into design_prompt now lives in that command's method-assets. Project Design
	// only ever dispatches a DRAFT (there is no PM-critique; the answer job dispatches manager-
	// side). An empty slug is contract misuse (an undispatchable kind — e.g. SdpReview, which is
	// assembled server-side, never dispatched); fail terminally before dispatch.
	command := projectstate.DesignCommandFor(toPSKind(a.ArtifactKind), projectstate.DesignJobModeDraft, "")
	if command == "" {
		return constructionpipeline.PipelineHandle(""), temporal.NewNonRetryableApplicationError(
			"no design command slug for this artifactKind — undispatchable design job", "UndispatchableDesignJob", nil)
	}
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(a.ArtifactKind),
		dispatchInputCommand:       command,
		dispatchInputTargetBranch:  a.TargetBranch,
		dispatchInputPriorStateRef: a.PriorStateRef,
		dispatchInputJobMode:       jobModeDraft,
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
		// a logical placeholder. The Phase-2 DESIGN-job parameters ride on DispatchInputs.
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
	}, nil
}

// designPipelinePhase maps the RA's phase to this Manager's neutral phase, preserving
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
		return pipelinePhaseUnknown
	}
}

// pipelinePhase mirrors constructionPipelineAccess.md §3 — the infrastructure-neutral
// lifecycle phase the Manager branches on. The terminal trio drives the observe
// loop's exit + the failure path.
type pipelinePhase int

const (
	pipelinePhaseUnknown pipelinePhase = iota
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
	case pipelinePhaseUnknown, pipelinePending, pipelineRunning:
		return false
	default:
		return false
	}
}

// pipelineObservation mirrors constructionPipelineAccess.md §3 — a point-in-time,
// infrastructure-neutral view carrying the phase and (on terminal failure) a neutral
// Diagnostic summary (NOT a log firehose).
type pipelineObservation struct {
	Phase      pipelinePhase
	Diagnostic string
}

// observePollInterval spaces the observe-poll loop's durable timer waits. A design
// job runs minutes in the user's CI; this is the in-workflow timer the contract
// prescribes (§0.5.2 step 4). Kept modest so the test's time-skipping env settles
// quickly.
const observePollInterval = 15 * time.Second

// maxObservePolls bounds the observe loop so a stuck (never-terminal) job cannot spin
// forever; exceeding it is treated as a terminal infrastructure failure and routed to
// the human gate (never a perpetual Drafting — the anti-wedge rule).
const maxObservePolls = 240 // 240 * 15s = 1h ceiling

// dispatchDesignJobArgs bundles the dispatch inputs for the Activity boundary. ArtifactKind
// selects the .claude command slug (DesignCommandFor); Branch + PriorStateRef ride into the
// DispatchInputs map inside the Activity. The prompt prose is GONE — the Phase-2 doctrine
// lives in the method-assets .claude command the design job runs; the Manager ships only the
// command name + the target metadata.
type dispatchDesignJobArgs struct {
	ProjectID     ProjectID
	ArtifactKind  ArtifactKind
	TargetBranch  string
	PriorStateRef string
	// TargetRepo is the opaque per-project RepoRef (gitSession.repoRef.String()) the
	// design job must dispatch to — the user's per-project repo where aiarch-design.yml
	// was committed at project birth (per-project-design-dispatch). Empty ⇒ the RA falls
	// back to the configured construction repo (the dormant-rail / non-git path).
	TargetRepo string
}

// dispatchAndObserve runs ONE dispatch → observe round-trip: it dispatches the design
// job (the generated submit invoker via dispatchDesignJob) and then polls the observe
// invoker (observeDesignJob) between durable startTimer waits until the job reaches a
// TYPED terminal phase. It returns the terminal observation; the caller decides success
// (read-back) vs failure (the StageDraftFailed gate). It NEVER infers failure from a
// timeout-as-success (§0.5.4): a stuck job that never terminates within the bounded poll
// budget is surfaced as an explicit pipelineFailed with a neutral diagnostic, so the
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

// readBackCommittedModelOn is readBackCommittedModel with an OPTIONAL branch override
// (I-DESIGN-DISPATCH §2a): the draft Action commits the typed JSON on the SESSION
// BRANCH, so the read-back reads that branch while the human reviews the not-yet-merged
// draft. branch=="" reads main (the dormant-rail / non-git behavior). It returns the
// read-back substrate's Version alongside the model so the caller can stage against the
// ACTUAL branch version — a fresh workflow reusing a dirty session branch (prior
// draft/critique commits) sees the branch already advanced, and staging against a stale
// main-captured version would Conflict (QA F29).
func (wf *workflows) readBackCommittedModelOn(ctx workflow.Context, projectID ProjectID, kind ArtifactKind, branch string) (projectstate.ArtifactModel, projectstate.Version, error) {
	proj, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return nil, 0, err
	}
	slot := slotFor(proj, toPSKind(kind))
	if slot.Model == nil {
		return nil, 0, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("design job reported success but committed no %s model to read back", toPSKind(kind)),
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

	// MANAGED-SCAFFOLD SYNC (sync-on-dispatch, 2026-07-06; mirrors systemdesign): before
	// ANY design job is dispatched, converge the seated aiarch-design.yml onto the CURRENT
	// template rendering (drift → one refresh commit on the default branch; identical →
	// no-op). A sync failure BLOCKS the dispatch — never run a design job against a
	// scaffold we could not prove current — and is CONTAINED by the caller at the failed
	// gate like every other dispatch-time rail fault.
	//
	// Temporal versioning guard (replay safety; mirrors construction-review-policy-
	// snapshot and the systemdesign twin): this activity was ADDED to beginSession AFTER
	// the CoAuthor workflow first shipped, so a Phase-2 design session already in flight
	// at deploy time has NO history event for it — replaying such a history against
	// unguarded new code fails the workflow task with a non-determinism error. GetVersion
	// pins pre-feature executions (DefaultVersion) to the OLD command sequence: they skip
	// the sync for their WHOLE run — including post-recovery redrafts, because the
	// version resolved at first replay is cached per execution — while every execution
	// STARTED after this deploy resolves v1 and syncs before each dispatch. A pre-feature
	// session that keeps failing on a stale scaffold heals via Withdraw + a fresh
	// session (a new execution → v1 → sync).
	if workflow.GetVersion(ctx, "managed-scaffold-sync", workflow.DefaultVersion, 1) >= 1 {
		var scaffoldChanged bool
		// SyncManagedScaffold rides the GENERATED sourceControlAccess.syncManagedScaffold
		// invoker (B9) — B5 already promoted the free-function composition helper onto the
		// frozen rail contract, so this is a clean cut. Still wrapped in the shared bounded
		// Auth retry (its railActivityOptions preset applies via the invoker's option hook).
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
	// caller (coAuthorDraftRound) CONTAINS the fault at the failed gate. The opened BranchRef
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
func (wf *workflows) mergeOnApprove(ctx workflow.Context, gf *gitSession, kind ArtifactKind) (bool, error) {
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
	// cool-down window after an API-heavy draft job (observed live on the systemdesign
	// twin: 3 openPR attempts across 15s → all 403 → StageDraftFailed; a manual retry
	// 15 min later succeeded first try). v1 ("rail-403-long-backoff") lengthens the
	// 403/auth class to 60s → 120s → 240s (~7 min budget, 4 attempts) — long enough to
	// outlast a secondary-rate-limit window, still bounded so a GENUINE permission
	// denial reaches the honest containment gates.
	railAuthRetryLongMaxAttempts = 4
	railAuthRetryLongBaseBackoff = 60 * time.Second
	railAuthRetryLongMaxBackoff  = 240 * time.Second
)

// railWithAuthRetry runs ANY rail call (a closure over a generated invoker or the custom
// SyncManagedScaffold Activity) with a bounded WORKFLOW-SIDE retry on a transient-403-as-Auth
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
		// sequence, so it is GetVersion-gated (the failed-gate-ledger-seed-p2 pattern).
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
// branch=="" it delegates to readProject (workflow.go) for a byte-identical call
// pattern; a non-empty branch goes straight through the generated
// designSessionAccess.readProjectOnBranch invoker (B9), which runs the SAME
// branch-aware-extension-or-main fallback internally (projectstate/designsession.go) —
// so the branch-aware read-back stays purely additive and the default path is unchanged.
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

// gitrail.go is the PR-rail consumer port + Temporal Activity wrappers the design
// Manager uses to wire the agentic DESIGN draft onto the git-forward branch→PR→read-
// back→+1→merge model (I-DESIGN-DISPATCH §2b). It MIRRORS the construction Manager's
// gitactivities.go / gitnaming.go pattern EXACTLY (same railCredEnvelope cred carrier,
// same Activity-per-rail-verb shape, same deterministic-name idempotency): the cred is
// MINTED by the Manager (MintRepoCredentialActivity → GetInstallationToken, a call
// DOWN) and threaded INTO every rail verb as a parameter; the RA never reads Temporal
// context and never fetches the credential itself ([[feedback_temporal_manager_layer_only]]).
//
// SUBSET. The design spine needs only the rail verbs the settled flow uses:
// GetInstallationToken (mint), OpenBranch (ensure the session branch), OpenPullRequest
// (head=sessionBranch, base=main), GetPullRequestStatus (the merge guard),
// PostReview (the architecture +1 relay), MergePullRequest (the App-mediated merge).
// ConfigureBranchProtection is a project-birth concern (FU-DD-3), absent here.
//
// DORMANT-WHEN-UNWIRED. The whole rail is OPTIONAL/nil-tolerant exactly like the
// construction git-forward slice: when wf.Rail == nil or wf.Repo == nil (or no repo
// resolves for the project) the CoAuthor workflow runs UNCHANGED — read-back/stage on
// main, no branch/PR ops — so every existing test and the Postgres/non-git composition
// are unperturbed.

// ===========================================================================
// Rail migration to the generated invoker surface.
//
// The SEVEN PR-rail verbs (GetInstallationToken/mint, OpenBranch, OpenPullRequest,
// GetPullRequestStatus, PostReview, MergePullRequest, SyncManagedScaffold) are GENERATED
// (activities.gen.go) and reached through the generated invoker surface (wf.Acts.Rail*)
// from the workflow-side helpers in gitsession.go. The folded railAdapterImpl + the
// plain-ctx sourceControlRail seam + the per-verb Activity wrappers are RETIRED; the
// workflow-side value mapping (opaque-handle *FromString/*String marshalling,
// PullRequestStatus→pullRequestStatusView, the ReviewApprove verdict now supplied at the
// call site) lives in gitsession.go. The per-op ActivityOptions presets
// (mintCredActivityOptions / railActivityOptions, below) feed the manager's option hook
// (workermanifest.go).
//
// B9: SyncManagedScaffold used to STAY a CUSTOM Activity (SyncManagedScaffoldActivity)
// wrapping the free-function sourcecontrol.SyncManagedScaffold composition helper — the
// generated layer had no single contract op for it. B5 promoted that helper onto the
// frozen sourceControlAccess contract as the syncManagedScaffold op (the concrete
// *access.SyncManagedScaffold impl, internal/resourceaccess/sourcecontrol/github.go, is
// now literally `return SyncManagedScaffold(rc.Context, a, repo, cred)` — the SAME free
// function, just reached through the contract instead of directly). B9 migrates the
// custom Activity wrapper onto the now-generated wf.Acts.RailSyncManagedScaffold invoker
// — a clean cut (repo/cred are concrete structs; no interface-across-the-wire hazard).
// ===========================================================================

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
	return fmt.Sprintf("aiarch: Phase-2 design %s", artifactKindString(kind))
}

func designPRBody(kind ArtifactKind) string {
	return fmt.Sprintf("Automated agentic design draft of %s (aiarch project-design).", artifactKindString(kind))
}

// designArchApprovalBody is the +1 relay's review body — the architect's in-app
// approval relayed onto the PR (the "architecture +1").
func designArchApprovalBody(kind ArtifactKind) string {
	return fmt.Sprintf("architecture +1 relayed for %s", artifactKindString(kind))
}

// setCommentStatusSignal is the SetReviewCommentStatus signal payload — the waive/reopen
// transition delivered to the CoAuthor workflow suspended at the AwaitingReview gate.
type setCommentStatusSignal struct {
	CommentID string
	Status    string
}

// feedbackToLedgerComments converts the architect's inbound anchored comments (on a Reject's
// ReviewFeedback) into the projectstate.ReviewComment shape the append verb stamps into the
// durable thread. Only Anchor / AnchorText / Text / AuthorRole are filled — id / round / open
// status are server-minted in appendReviewComments. An anchored comment with empty Text is
// dropped (defensive); free-text Notes stay the reject notes, not ledger comments.
func feedbackToLedgerComments(feedback *ReviewFeedback) []projectstate.ReviewComment {
	if feedback == nil {
		return nil
	}
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

// openReviewCommentIDs returns the ids of every OPEN CHANGE-REQUEST — the comments that gate
// approve (review-ledger §4). Open QUESTIONS never gate (question-comments §approve). Empty
// ⇒ approve is unblocked.
func openReviewCommentIDs(thread []projectstate.ReviewComment) []string {
	var ids []string
	for _, c := range thread {
		if projectstate.ReviewCommentBlocksApprove(c) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// seedAmendmentLedger records the reopening feedback as round-0 OPEN ledger entries on the
// amendment session branch after the first stage, then reloads the in-memory thread.
// Best-effort; no-op with no anchored comments.
// maybeSeedAmendment seeds the amendment ledger exactly once when an amendment session first
// reaches AwaitingReview, returning the updated seeded flag (keeps the spine flat).
func (wf *workflows) maybeSeedAmendment(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, seeded bool, state *coAuthorState) bool {
	if in.Amendment > 0 && !seeded {
		wf.seedAmendmentLedger(ctx, in, gf, headVersion, state)
		return true
	}
	return seeded
}

func (wf *workflows) seedAmendmentLedger(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, state *coAuthorState) {
	comments := feedbackToLedgerComments(in.Feedback)
	if len(comments) == 0 {
		return
	}
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSeedReviewCommentsOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, gf.readBackBranch(), toPSKind(in.ArtifactKind), 0, comments)
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
// recovery path (a redraft signal / a Retry-via-Reject AT a failed gate / a faulted reject)
// retained ONLY in the workflow's feedback variable. Unlike the review-gate reject and the
// amendment seed, those paths never wrote it to the durable review ledger — so under thin
// dispatch (the drafting agent reads context ONLY via getReviewThread) it would evaporate. This
// folds the SAME anchored comments the reject path uses (feedbackToLedgerComments) into the
// ledger on the SAME session branch, consuming a review round (reviewRound, like a reject) so
// the seeded ids do not collide with a later reject's on the one accumulating thread. Best-
// effort, mirroring seedAmendmentLedger: a Notes-only feedback (no anchored comments), an
// unpopulated slot, a non-ledger substrate, or a transient fault leaves the feedback un-seeded
// and RETRIES on the next redraft dispatch. Returns whether the seed durably landed, so the
// caller marks feedbackSeeded and stops re-seeding. headVersion is a hint only — applyRecovering
// re-reads on a version conflict.
func (wf *workflows) seedFailedGateFeedback(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion projectstate.Version, feedback *ReviewFeedback, reviewRound *int, state *coAuthorState) bool {
	comments := feedbackToLedgerComments(feedback)
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

// loadReviewThread reads the artifact slot's durable ledger from the session branch ("" ⇒
// main). Called after every (re)stage and every waive/reopen so the query + approve gate see
// the live thread. A read fault is returned; the caller keeps the last-known thread.
func (wf *workflows) loadReviewThread(ctx workflow.Context, in coAuthorInput, gf gitSession) ([]projectstate.ReviewComment, error) {
	proj, err := wf.readProjectOnBranch(ctx, in.ProjectID, gf.readBackBranch())
	if err != nil {
		return nil, err
	}
	return slotFor(proj, toPSKind(in.ArtifactKind)).ReviewThread, nil
}

// applyCommentStatus applies one human review-ledger transition (waive / reopen) on the
// session branch during the AwaitingReview window, then refreshes the in-memory thread.
// Best-effort: an illegal transition / unknown id / transient fault leaves the review session
// at the gate with the unchanged thread (the manager pre-check already rejected most bad
// requests synchronously).
func (wf *workflows) applyCommentStatus(ctx workflow.Context, in coAuthorInput, gf gitSession, headVersion *projectstate.Version, sig setCommentStatusSignal, state *coAuthorState) {
	newVersion, err := wf.applyRecovering(ctx, in.ProjectID, gf.readBackBranch(), *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.DesignSessionSetReviewCommentStatusOnBranch(ctx, projectstate.ProjectID(in.ProjectID), expected, gf.readBackBranch(), toPSKind(in.ArtifactKind), sig.CommentID, sig.Status)
	})
	if err != nil {
		return
	}
	*headVersion = newVersion
	if thread, terr := wf.loadReviewThread(ctx, in, gf); terr == nil {
		state.reviewThread = thread
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
