package projectdesign

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/settlement"
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
	Settlement   settlement.SettlementEngine
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

	// PR-rail Activity names (I-DESIGN-DISPATCH §2b).
	actMintRepoCredential   = "MintRepoCredentialActivity"
	actOpenBranch           = "OpenBranchActivity"
	actOpenPullRequest      = "OpenPullRequestActivity"
	actGetPullRequestStatus = "GetPullRequestStatusActivity"
	actPostReview           = "PostReviewActivity"
	actMergePullRequest     = "MergePullRequestActivity"
)

// maxRedraftAttempts bounds the redraft loop before the workflow escalates.
const maxRedraftAttempts = 5

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

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop.
func (wf *workflows) applyRecovering(
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
}

// coAuthorOutcome is the workflow's terminal report — whether the human gate
// approved or withdrew.
type coAuthorOutcome int

const (
	coAuthorUnknown coAuthorOutcome = iota
	coAuthorApproved
	coAuthorWithdrawn
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
	logger := workflow.GetLogger(ctx)

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

	// branchAttempt is the per-REJECT session-branch attempt counter (I-DESIGN-DISPATCH
	// §2b "sessionBranch"). A within-attempt redraft reuses the same session branch; a
	// fresh REJECT bumps it so the next attempt drafts on a NEW branch + opens a NEW PR.
	// Threaded into designBranch; 0 ⇒ the original deterministic name. Inert when the
	// rail is dormant.
	branchAttempt := 0

	for {
		// --- DRAFT round-trip: dispatch -> observe -> read-back (agentic pivot) ---
		// The Manager composes the Phase-2 architect-role prompt IN-MEMORY (never
		// persisted), dispatches a claude-code-action DESIGN job, observes it to a typed
		// terminal phase, and reads back the typed model the Action committed. On a
		// terminal FAILURE phase the session lands in StageDraftFailed and suspends at the
		// human gate (the anti-wedge rule, §0.5.4) — never a perpetual Drafting.
		var draft projectstate.ArtifactModel
		state.stage = stageForAttempt(redraftCount)

		// The per-attempt SESSION BRANCH the Action drafts + commits + opens its PR on
		// (I-DESIGN-DISPATCH §2b). Inert (just a string) when the rail is dormant.
		sessionBranch := designBranch(in.ProjectID, in.ArtifactKind, branchAttempt)

		// Rail (dispatch-time half): mint the credential + ensure the session branch
		// exists BEFORE the Action drafts on it. A dormant rail returns a disabled session
		// and the spine runs unchanged (read-back/stage on main, no branch/PR ops).
		gf, gerr := wf.beginSession(ctx, in.ProjectID, sessionBranch)
		if gerr != nil {
			return coAuthorUnknown, gerr
		}

		draftPrompt := architectDraftPrompt(toPSKind(in.ArtifactKind), proj, feedback)
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
			return coAuthorUnknown, derr
		}
		if draftObs.Phase != pipelineSucceeded {
			// The job RAN and FAILED (drafting failed or the required CI validation check
			// went red) — a terminal-at-the-Manager fault. Do NOT crash the workflow and do
			// NOT loop: land the session in the human-visible StageDraftFailed and suspend
			// on the gate awaiting Retry (redraft/Reject) or Withdraw (§0.5.4 — the
			// anti-wedge rule).
			logger.Warn("Phase-2 design draft job reached a terminal failure phase; entering StageDraftFailed", "diagnostic", draftObs.Diagnostic)
			outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, draftObs.Diagnostic, state, &feedback)
			if recErr != nil {
				return coAuthorUnknown, recErr
			}
			if !retry {
				return outcome, nil
			}
			redraftCount++
			continue
		}
		// Rail: open the PR (head=sessionBranch, base=main) now the draft is green.
		// Idempotent on head — if the Action already opened it the rail returns the
		// existing handle (authoritative for the merge step).
		if err := wf.openPR(ctx, &gf, in.ArtifactKind); err != nil {
			return coAuthorUnknown, err
		}
		// READ-BACK on the SESSION BRANCH (§2a): the Action committed the typed Phase-2
		// JSON on the session branch; read it back as the not-yet-merged draft. A dormant
		// rail reads main (readBackBranch() == "").
		model, rbErr := wf.readBackCommittedModelOn(ctx, in.ProjectID, in.ArtifactKind, gf.readBackBranch())
		if rbErr != nil {
			return coAuthorUnknown, rbErr
		}
		draft = model
		state.findings = nil

		// Track the staged typed draft for the query.
		state.draft = draft

		// Step 4: stageArtifactForReview, with the workflow-level Conflict loop.
		draftEnvelope, encErr := encodeModel(draft)
		if encErr != nil {
			return coAuthorUnknown, fwmanager.MapError(encErr)
		}
		{
			newVersion, err := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.StageArtifactForReviewActivity, stageArtifactForReviewArgs{
					ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Model: draftEnvelope, Branch: gf.readBackBranch(),
				}).Get(ctx, &v)
				return v, e
			})
			if err != nil {
				return coAuthorUnknown, err
			}
			headVersion = newVersion
		}
		state.stage = StageAwaitingReview

		// Step 6: awaitSignal("reviewDecision") — in-workflow primitive; suspend.
		var sig reviewDecisionSignal
		workflow.GetSignalChannel(ctx, signalReviewDecision).Receive(ctx, &sig)

		// Step 7: branch on the architect's decision (the commit authority).
		switch sig.Decision {
		case ReviewApprove:
			// Rail (approve-time half, §2b): merge GUARD (CI must be green) + the
			// architecture +1 relay + the App-mediated merge of sessionBranch → main. A
			// dormant rail returns merged=true with no rail ops (the non-git spine).
			merged, mErr := wf.mergeOnApprove(ctx, &gf, in.ArtifactKind)
			if mErr != nil {
				return coAuthorUnknown, mErr
			}
			if !merged {
				// The merge guard was NOT green (the required CI check is red on the PR): do
				// NOT merge/commit. Route to the SAME StageDraftFailed recovery gate as a draft
				// failure (the anti-wedge rule) awaiting Retry-via-Reject / Withdraw.
				logger.Warn("Phase-2 design PR not mergeable at approve (CI not green); entering StageDraftFailed")
				outcome, retry, recErr := wf.awaitDraftFailedRecovery(ctx, in.ProjectID, in.ArtifactKind, headVersion, "the design PR is not green — its required CI check has not passed", state, &feedback)
				if recErr != nil {
					return coAuthorUnknown, recErr
				}
				if !retry {
					return outcome, nil
				}
				branchAttempt++
				redraftCount++
				continue
			}
			// After merge the draft lives on main; commitArtifact lands on main. Re-seed
			// headVersion from main so the commit's CAS starts at main's tip. A dormant rail
			// leaves headVersion as-is (it already tracked main).
			if gf.enabled {
				if mp, rerr := wf.readProject(ctx, in.ProjectID); rerr == nil {
					headVersion = mp.Version
				} else if !isReadNotFound(rerr) {
					return coAuthorUnknown, rerr
				}
			}
			newVersion, err := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.CommitArtifactActivity, mutateArtifactArgs{
					ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind),
				}).Get(ctx, &v)
				return v, e
			})
			if err != nil {
				return coAuthorUnknown, err
			}
			headVersion = newVersion
			state.stage = StageCommitted
			return coAuthorApproved, nil

		case ReviewReject:
			notes := signalNotes(sig.Feedback)
			newVersion, err := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.RejectArtifactActivity, mutateArtifactArgs{
					ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind), Notes: notes,
				}).Get(ctx, &v)
				return v, e
			})
			if err != nil {
				return coAuthorUnknown, err
			}
			headVersion = newVersion
			// A fresh REJECT needs a NEW session branch + PR next attempt (the rejected PR
			// cannot be reused). Bump the branch attempt; inert when the rail is dormant.
			branchAttempt++
			feedback = notes
			state.stage = StageRedrafting
			continue

		case ReviewWithdraw:
			notes := signalNotes(sig.Feedback)
			newVersion, err := wf.applyRecovering(ctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				c := mutateOpts(ctx)
				var v projectstate.Version
				e := workflow.ExecuteActivity(c, wf.WithdrawArtifactActivity, mutateArtifactArgs{
					ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, Kind: toPSKind(in.ArtifactKind), Notes: notes,
				}).Get(ctx, &v)
				return v, e
			})
			if err != nil {
				return coAuthorUnknown, err
			}
			headVersion = newVersion
			state.stage = StageWithdrawn
			return coAuthorWithdrawn, nil

		default:
			return coAuthorUnknown, temporal.NewNonRetryableApplicationError("unknown review decision", "UnknownReviewDecision", nil)
		}
	}
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
	logger := workflow.GetLogger(ctx)

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
			if sig.OptionID == nil || !optionInReview(review, projectstate.OptionID(*sig.OptionID)) {
				return temporal.NewNonRetryableApplicationError(
					"SDP commit named an option not in the assembled review", "UnknownOption", nil)
			}
			chosen := projectstate.OptionID(*sig.OptionID)

			// Commit-time confirmation: RE-RUN the three engines on the chosen option,
			// re-stage the review with Recommendation=chosen (records the architect's
			// choice), then commit. `confirmed` is byte-identical to the staged `review`
			// the architect saw (assembleSdpReview is deterministic over the same `proj`,
			// no clock/RNG — §6.8), so the option set the architect chose from cannot
			// skew between the gate and the commit; we re-derive only to bind the choice.
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

		default:
			return temporal.NewNonRetryableApplicationError("unknown SDP decision", "UnknownSDPDecision", nil)
		}
	}
}

// stageReview stages the SdpReview into its slot (status AwaitingReview), updating
// headVersion via the workflow-level Conflict loop.
func (wf *workflows) stageReview(ctx workflow.Context, projectID ProjectID, review *projectstate.SdpReview, headVersion *projectstate.Version) error {
	env, encErr := encodeModel(review)
	if encErr != nil {
		return fwmanager.MapError(encErr)
	}
	v, err := wf.applyRecovering(ctx, projectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
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
	v, err := wf.applyRecovering(ctx, projectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
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
	v, err := wf.applyRecovering(ctx, projectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
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

	// Seal Phase 2.
	if _, err := wf.applyRecovering(ctx, in.ProjectID, proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
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
func toSettlementOption(opt projectstate.ProjectOption) settlement.ProjectOption {
	t := opt.Terms
	return settlement.ProjectOption{
		OptionID: settlement.OptionID(opt.OptionID),
		Terms: settlement.SettlementTerms{
			RevenueShare:         settlement.RevenueShareKind(t.RevenueShare),
			RevenueSharePercent:  t.RevenueSharePercent,
			ComputeCost:          settlement.ComputeCostKind(t.ComputeCost),
			ComputeMarkupPercent: t.ComputeMarkupPercent,
			Schedule:             settlement.ScheduleKind(t.Schedule),
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

	activities := make([]projectstate.OptionActivity, 0, len(al.Activities))
	for _, a := range al.Activities {
		activities = append(activities, projectstate.OptionActivity{
			ActivityID:     a.Name,
			EffortDays:     a.EffortDays,
			WorkerClass:    a.WorkerClass,
			OnCriticalPath: onCritical[a.Name],
			RiskBucket:     a.RiskBucket,
		})
	}

	calendar := sol.CalendarDaysPerWeek
	if calendar == 0 {
		calendar = pa.CalendarDaysPerWeek
	}

	return projectstate.ProjectOption{
		OptionID:            projectstate.OptionID(kind.String()),
		SolutionKind:        kind,
		Network:             projectstate.ActivityNetwork{Activities: activities},
		WorkerMix:           projectstate.WorkerMix{ClassRates: sol.ClassRates, StaffingCap: sol.StaffingCap},
		CalendarDaysPerWeek: calendar,
		Terms:               pa.Terms,
		DeclaredUsage:       pa.DeclaredUsage,
		InfrastructureKind:  pa.InfrastructureKind,
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
// constructionEstimationEngine's OWN SLIM ProjectOption snapshot at the call boundary
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
	return estimation.ProjectOption{
		OptionId:            estimation.OptionID(opt.OptionID),
		Network:             estimation.ActivityNetwork{Activities: activities},
		WorkerMix:           estimation.WorkerMix{ClassRates: rates, StaffingCap: int64(opt.WorkerMix.StaffingCap)},
		CalendarDaysPerWeek: opt.CalendarDaysPerWeek,
	}
}

// toProjectStateMoneyFromEstimation converts the constructionEstimationEngine's OWN
// Money back to the canonical projectstate.Money at the call boundary (Option B full
// encapsulation).
func toProjectStateMoneyFromEstimation(m estimation.Money) projectstate.Money {
	return projectstate.Money{MinorUnits: m.MinorUnits, Currency: m.Currency}
}

// recommendOption picks the row with the best (lowest CompositeRisk, tie-break
// lowest DurationDays) and returns its OptionID + a short deterministic rationale.
func recommendOption(rows []projectstate.SdpOptionRow) (projectstate.OptionID, string) {
	if len(rows) == 0 {
		return "", "no options assembled"
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.CompositeRisk < best.CompositeRisk ||
			(r.CompositeRisk == best.CompositeRisk && r.DurationDays < best.DurationDays) {
			best = r
		}
	}
	return best.OptionID, fmt.Sprintf(
		"recommend %s: lowest composite risk (%.3f) at %.1f days",
		best.OptionID, best.CompositeRisk, best.DurationDays)
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
	diagnostic string,
	state *coAuthorState,
	feedback *string,
) (coAuthorOutcome, bool, error) {
	// Surface the human-visible failed stage + the neutral diagnostic for the Query.
	state.stage = StageDraftFailed
	state.failureReason = draftFailedReason(diagnostic)

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
			if _, err := wf.applyRecovering(ctx, projectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
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
