// Package delivery is the deliveryManager component of the aiarch server's
// Manager layer — the ONE Manager of the Project Delivery Workflow volatility
// (B-02, B-03, B-04, B-13, B-14). It replaces internal/manager/systemdesign,
// internal/manager/projectdesign and internal/manager/construction, which were
// three choreographies of one workflow: an activity of the committed project
// network becomes eligible, an agent produces its artifact, agents and humans
// review it, and the activity advances through its own task DAG.
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it defines and registers one Activity
// per ResourceAccess call, owns the Signal/Query handlers, and derives the
// idempotency key "${workflowId}:${activityId}" passed down to each RA verb.
// Temporal lives ONLY in this component; the downstream Engines and
// ResourceAccess ports are Temporal-free.
//
// SCHEMA-FIRST (full encapsulation): this component OWNS its contract I/O types.
// The public surface (DeliveryManager port + the I/O value types) is GENERATED
// into contract.gen.go from this component's `.serviceContracts.deliveryManager`
// entry in .aiarch/state/project.json (edit that entry + `make gen`; do NOT
// hand-edit the generated surface).
//
// STAGE 4a IS A MOVE, NOT A REWRITE. The three rails' bodies are here verbatim,
// each under a banner naming the file it came from, and the twelve contract ops
// are a THIN DISPATCHER over the forty implementations they already had. The
// only edits the merge forced are:
//   - the framework-go/manager import alias, which the construction rail spelled
//     `fwm` and the two design rails `fwmanager`; one file can carry one alias,
//     so the construction block reads `fwmanager` here;
//   - the package-private name collisions: byte-identical twins collapsed to one
//     copy (marked in place), everything whose body differed prefixed by rail
//     (`sd` / `pd` / `cs`) — the full 199-row rename table, so a reader of this file
//     can find the symbol the pre-merge blame names, is tracked at
//     docs/superpowers/plans/2026-09-25-stage4a-rename-table.md.
//
// Nineteen Temporal replay fixtures assert that nothing else changed.
//
// File layout within the package (arch.CheckFileLayout, framework-go/arch):
//   - deliverymanager.go  : the Manager + the twelve-op dispatcher (the ONE impl file)
//   - contract.gen.go     : the generated public façade (port + I/O value types)
//   - activities.gen.go / invokers.gen.go / worker.gen.go : the generated Temporal surface
//   - <workflow>.go       : exactly one file per registered workflow entry function
//   - manager_test.go     : the ONE test file the layout rule allows
package delivery

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"maps"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	billing "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// ---------------------------------------------------------------------------
// SYSTEM-DESIGN RAIL — moved verbatim from internal/manager/systemdesign/
// systemdesignmanager.go at stage 4a. Bodies are unchanged; only package-private
// names that collided with another rail were renamed (the collision table is in
// docs/superpowers/plans/2026-09-25-activity-experience-stage4a.md, Task 6 Step 2b,
// and in this commit's message). 4b replaces this block with the generic DAG child.
// ---------------------------------------------------------------------------

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
// agenticjob.AgenticJobAccess (design-job dispatch), the published
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
	pipeline     agenticjob.AgenticJobAccess
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

	// activityExecution (stage 3, task 6) is the generated activityExecutionAccess dep —
	// the fifth facet of the one project-state component, owner of the per-activity review
	// ROUND ledger. The design rail dual-writes every review decision through it beside the
	// slot's ReviewThread: taking the dep HERE is what registers its Temporal activities on
	// this Manager's worker, which is the precondition for the CoAuthor spine's
	// wf.Acts.ActivityExecution* calls. Held only to thread into genActivities — every call
	// is a workflow-side Activity, never a manager-side one.
	activityExecution projectstate.ActivityExecutionAccess

	// designHealth is the DesignHealthEngine port behind the getDesignHealth
	// read-model op — the M→E half of the shared System Design Phase Workflow
	// volatility (this Manager owns the gate choreography; the Engine owns which
	// rules judge a draft). It is NOT a generated constructor dep: the component
	// carries no service contract, and an Engine is pure and stateless, so the
	// builder constructs it directly rather than threading a parameter no
	// composition root could vary.
	designHealth designhealth.Engine

	// episodes (SP1 capture-seam) is the generated episodeAccess dep — the agentic-
	// episode ledger every terminal design dispatch appends to. The WORKFLOW paths reach
	// it through the generated invoker surface (wf.Acts.EpisodesAppendEpisode); this
	// field is held for two reasons: to thread it into genActivities, and because the
	// answer-job capture (answerEpisodeWatch) runs MANAGER-SIDE, outside any workflow,
	// and must call the RA directly.
	episodes episode.EpisodeAccess
}

// newSystemDesignManager is the hand-written, unexported builder the generated
// NewSystemDesignManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only client + projectState;
// pipeline/rail/repo are stored for RegisterWorker (rail may be nil — a dev server
// with no source-control credentials runs the design spine repo-less).
func newSystemDesignManager(c client.Client, ps projectstate.ProjectStateAccess, pipeline agenticjob.AgenticJobAccess, rail sourcecontrol.SourceControlAccess, repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool), estimator estimation.EstimationEngine, designSession projectstate.DesignSessionAccess, activityExecution projectstate.ActivityExecutionAccess, episodes episode.EpisodeAccess, repoBase string) *systemDesignManager {
	return &systemDesignManager{client: c, projectState: ps, pipeline: pipeline, rail: rail, repo: repo, estimator: estimator, designSession: designSession, activityExecution: activityExecution, episodes: episodes, repoBase: repoBase, designHealth: designhealth.NewEngine()}
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
	// A redraft's feedback is OPTIONAL (nil = a fresh draft with no steer), but a
	// non-nil envelope whose notes are empty is a third state that steers nothing
	// while telling the agent it was steered. The sibling SubmitReviewDecision
	// rejects exactly this shape; RequestArtifactDraft must agree.
	if feedback != nil && strings.TrimSpace(feedback.Notes) == "" {
		return "", newError(fwmanager.ContractMisuse, "feedback is present but its notes are empty — omit feedback entirely to request a fresh draft with no steer")
	}
	// A re-request/amendment SEEDS round-0 threads before the session has loaded any thread
	// at all (seedAmendmentLedger runs ahead of the first loadReviewThread), so there is
	// nothing here for a replyTo to name. Refuse it rather than let it reach a seed that
	// could only re-file it as a fresh comment (design §3.7).
	if feedback != nil {
		if perr := checkNoReplyTo("replyTo is not supported on requestArtifactDraft — it opens a new round of threads; file a reply against an existing thread through submitReviewDecision at the review gate", feedback.Comments); perr != nil {
			return "", perr
		}
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
	if err := m.prepareForDraftRequest(rc, projectID, kind); err != nil {
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// wedgedSupersedeReason is the Temporal termination reason recorded when Retry supersedes a
// WEDGED design run (F-R2). Human-readable so the run's close event explains why.
const wedgedSupersedeReason = "superseded by Retry: workflow task stuck in failed state"

// prepareForDraftRequest is the pre-SignalWithStart gate for RequestArtifactDraft. It probes
// the live session directly (Describe + Query) so it can SUPERSEDE a WEDGED run (F-R2): a run
// whose workflow task is perpetually failing shows RUNNING to Describe but rejects the
// sessionState query with the wedged signature, and a SignalWithStart with USE_EXISTING would
// only BUFFER the redraft signal on that corpse forever (the deadlock). On exactly that shape,
// TERMINATE the wedged run (tolerating a NotFound race) so the subsequent SignalWithStart
// starts a genuinely fresh run. Termination is gated STRICTLY on the wedged classification —
// a transient query timeout/Unavailable falls through to the normal receptive check and
// surfaces as today's error, never a terminate. Every non-wedged outcome keeps the established
// checkDraftRequestReceptive behavior (Drafting/Redrafting refusal, NotFound-starts-fresh).
// Purely a manager-side precondition — no workflow logic, replay-safe by construction.
func (m *systemDesignManager) prepareForDraftRequest(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	ctx := rc.Context
	wfID := coAuthorWorkflowID(projectID, kind)
	desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, "")
	if derr != nil {
		if isNotFound(derr) {
			return nil // no session yet — this request starts the first one
		}
		// A Describe blip (non-NotFound): fall back to the query-based receptive check
		// rather than masking a transient fault as receptive.
		return m.checkDraftRequestReceptive(rc, projectID, kind)
	}
	// A non-RUNNING execution (abnormal-closed / completed / paused) is receptive: the
	// SignalWithStart either revives a fresh run or the durable slot is already terminal —
	// none of those is a live Drafting/Redrafting the redraft signal could stale-consume.
	if desc.GetWorkflowExecutionInfo().GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return nil
	}
	// A RUNNING execution: query its live stage — this ONE query ALSO detects the WEDGED shape.
	enc, qerr := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if qerr != nil {
		if isWorkflowTaskFailedQueryErr(qerr) {
			// Wedged RUNNING run — supersede it so the SignalWithStart starts a fresh run.
			// Tolerate a NotFound (it closed between the query and here); any other terminate
			// fault is surfaced so the caller never silently binds the signal to the corpse.
			if terr := m.client.TerminateWorkflow(ctx, wfID, "", wedgedSupersedeReason); terr != nil && !isNotFound(terr) {
				return newError(fwmanager.Infrastructure,
					"could not supersede the stuck design session before retrying: "+terr.Error())
			}
			return nil // proceed to SignalWithStart (starts a fresh run)
		}
		if isNotFound(qerr) {
			return nil // raced to closed between Describe and Query — the start revives it
		}
		return mapQueryError(qerr) // transient — surface, never terminate
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	// The generating guard: a live Drafting/Redrafting session is NOT receptive (a redraft
	// signal would sit buffered and later stale-consume a recovery gate).
	if view.Stage == StageDrafting || view.Stage == StageRedrafting {
		return newError(fwmanager.FailedPrecondition,
			"a draft is already generating for this artifact (currently "+sessionStageLabel(view.Stage)+") — wait for it to finish before requesting another")
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
	if err := validateReviewDecisionArgs(projectID, kind, decision, feedback); err != nil {
		return err
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
		// DEAD-SESSION WITHDRAW (F-R2 scoped). A dormant-mode session stages its slot on main
		// (branch ""), so when its run died leaving an AwaitingReview/Rejected slot there, a
		// Withdraw can still be honored SYNCHRONOUSLY through the RA — the slot resets durably
		// and GetSessionState then renders Withdrawn. Only for Withdraw, and only for a slot
		// actually staged on main; a never-staged slot keeps the refusal (Retry is the lever).
		// A branch-backed session stages on a session branch, not main, so its main slot is not
		// AwaitingReview and it correctly falls through to the refusal (Retry supersedes it).
		if decision == ReviewWithdraw {
			done, werr := m.withdrawDeadSessionOnMain(ctx, projectID, kind, feedback)
			if werr != nil {
				return werr
			}
			if done {
				return nil
			}
		}
		return newError(fwmanager.FailedPrecondition,
			"the design session for this artifact is no longer running (it ended abnormally) — review decisions cannot reach it. Use \"Retry design job\" to start a fresh session, then decide on its review gate")
	}
	if lerr := m.applyReviewLedgerGate(ctx, wfID, decision, feedback, view.ReviewThread); lerr != nil {
		return lerr
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

// applyReviewLedgerGate is SubmitReviewDecision's review-LEDGER half, split out from the op
// body (which reads as the review FLOW) and from its F19 stage gate. Three rules, all over
// the thread the sessionState query just returned:
//
//   - APPROVE is blocked while any change-request thread is still OPEN (review-ledger §4,
//     restated in the design §3.3 vocabulary) — the reviewer sends it back for a redraft or
//     resolves it first. The message lists the open ids.
//   - APPROVE then BULK-RESOLVES every ANSWERED thread (design §3.4). Approving IS accepting
//     every answer the agent gave, so accepting a redraft that answered eight change requests
//     costs one gesture, not eight Resolve clicks. Signaled BEFORE the decision because the
//     gate's selector drains status signals first, so the commit that follows carries a
//     fully-closed ledger. A failed resolve aborts the approve: half-closing the ledger and
//     committing anyway would strand threads answered forever, and the reviewer can simply
//     press Approve again.
//   - REJECT refuses a replyTo naming no thread on this artifact (design §3.7). Here is the
//     only place that refusal reaches the caller — the workflow receives the decision as a
//     fire-and-forget signal, so its own (TOCTOU-safe) re-check can only divert to the
//     failed gate.
func (m *systemDesignManager) applyReviewLedgerGate(ctx context.Context, wfID string, decision ReviewDecision, feedback *ReviewFeedback, thread []ReviewCommentView) error {
	switch decision {
	case ReviewApprove:
		if open := openReviewCommentViewIDs(thread); len(open) > 0 {
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot approve: %d review thread(s) still open (%s) — send them back or resolve them first", len(open), strings.Join(open, ", ")))
		}
		for _, id := range bulkResolveAnswered(thread) {
			resolve := setCommentStatusSignal{CommentID: id, Status: projectstate.ReviewCommentResolved}
			if err := m.client.SignalWorkflow(ctx, wfID, "", signalSetCommentStatus, resolve); err != nil {
				return mapSignalError(err)
			}
		}
	case ReviewReject:
		if feedback != nil {
			return checkReplyTargets(viewCommentIDs(thread), feedback.Comments)
		}
	case ReviewWithdraw, ReviewDecisionUnknown, ReviewAdvance, ReviewSetCommentStatus:
		// ReviewAdvance and ReviewSetCommentStatus are stage-4a ADDITIONS to the enum,
		// made by the merged contract. They never reach this rail: the deliveryManager
		// dispatcher answers both itself (the phase seal, and the comment transition),
		// so they join the ignored arm here rather than changing any rail behaviour.
		// Withdraw abandons the draft, ledger and all — there is nothing to gate on. The
		// zero value never reaches here (validateReviewDecisionArgs refuses it up front).
	}
	return nil
}

// validateReviewDecisionArgs is SubmitReviewDecision's argument gate: the caller's
// identifiers are well-formed, the kind belongs to Phase 1, and the decision is one
// the op can act on — with Reject additionally requiring the feedback it exists to
// carry. Split out so the op body reads as the review FLOW (inspect the gate, refuse
// what cannot be honored, signal) rather than opening with its own validation block.
func validateReviewDecisionArgs(projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase1(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-1 kind")
	}
	switch decision {
	case ReviewApprove, ReviewWithdraw:
		return nil
	case ReviewReject:
		if feedback == nil || feedback.Notes == "" {
			return newError(fwmanager.ContractMisuse, "Reject requires feedback")
		}
		return nil
	case ReviewDecisionUnknown, ReviewAdvance, ReviewSetCommentStatus:
		// ReviewAdvance and ReviewSetCommentStatus are stage-4a ADDITIONS to the enum,
		// made by the merged contract. They never reach this rail: the deliveryManager
		// dispatcher answers both itself (the phase seal, and the comment transition),
		// so they join the ignored arm here rather than changing any rail behaviour.
		// The zero value: a caller that forgot to set Decision, not a legitimate
		// review outcome. Reject explicitly rather than falling through silently.
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	default:
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}
}

// deadWithdrawMaxAttempts bounds the sync-path Conflict re-read/re-apply loop for a
// dead-session withdraw (F-R2).
const deadWithdrawMaxAttempts = 5

// withdrawDeadSessionOnMain honors a Withdraw against a DEAD session whose staged slot is
// still on MAIN (F-R2). It returns done=true when it recorded the withdraw (the slot was
// AwaitingReview/Rejected on main), done=false when the slot is NOT staged on main so the
// caller keeps the refusal. Synchronous RA write on main (branch ""), the same
// manager-calls-RA discipline the sync ops (SetResearchInput) use, with a bounded Conflict
// re-read/re-apply. The idempotency key is pinned to the FIRST-read staged version, so a
// retried withdraw of the SAME staged state dedups while a later distinct staged state (a
// fresh dead session) keys differently. The call shape mirrors the generated withdraw
// activity exactly (fwra.Context carrying the idempotency key + the explicit key param),
// with branch "" for main.
func (m *systemDesignManager) withdrawDeadSessionOnMain(ctx context.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (bool, error) {
	if m.projectState == nil {
		return false, nil // no durable store to consult → cannot do the scoped withdraw; refuse
	}
	fwctx := fwra.Context{Context: ctx}
	psID := projectstate.ProjectID(projectID)
	proj, err := m.projectState.ReadProject(fwctx, psID)
	if err != nil {
		return false, mapReadProjectError(err)
	}
	stagedOnMain := func(slot projectstate.ArtifactSlot) bool {
		return slot.Status == projectstate.ReviewAwaitingReview || slot.Status == projectstate.ReviewRejected
	}
	if !stagedOnMain(slotFor(proj, kind)) {
		return false, nil // not staged on main → not the dead-withdraw case; caller refuses
	}
	notes := ""
	if feedback != nil {
		notes = feedback.Notes
	}
	key := fwra.IdempotencyKey(fmt.Sprintf("%s:%s:deadWithdraw:%d", projectID, artifactKindString(kind), proj.Version))
	expected := proj.Version
	var lastErr error
	for range deadWithdrawMaxAttempts {
		_, werr := m.designSession.WithdrawArtifactOnBranch(
			fwra.Context{Context: ctx, IdempotencyKey: key}, psID, expected, "", toPSKind(kind), notes, key)
		if werr == nil {
			return true, nil
		}
		if !isRAConflict(werr) {
			return false, fwmanager.MapError(werr)
		}
		lastErr = werr
		p, rerr := m.projectState.ReadProject(fwctx, psID)
		if rerr != nil {
			return false, mapReadProjectError(rerr)
		}
		// A concurrent writer already moved the slot off the staged state → the withdraw is
		// done or no longer applicable; treat as handled rather than re-applying blindly.
		if !stagedOnMain(slotFor(p, kind)) {
			return true, nil
		}
		expected = p.Version
	}
	return false, fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "designSessionAccess.WithdrawArtifactOnBranch: exhausted conflict retries")
}

// reviewGateView returns the session's full gate view (stage + the durable review thread)
// for the F19 review precondition AND the review-ledger approve/resolve preconditions, plus
// whether a LIVE workflow can still honor a signal. Same dead-workflow defense as
// GetSessionState: a CLOSED-ABNORMAL run reports StageDraftFailed with live=false (a signal
// to it can never be honored — 2026-07-16 incident), a missing execution reports
// SessionStageUnknown, a live run is read from the authoritative sessionState query.
func (m *systemDesignManager) reviewGateView(ctx context.Context, wfID string) (SessionStateView, bool, error) {
	describeLive := false
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		if status := desc.GetWorkflowExecutionInfo().GetStatus(); isAbnormalClosedStatus(status) {
			return SessionStateView{Stage: StageDraftFailed}, false, nil
		}
		describeLive = true
	} else if isNotFound(derr) {
		return SessionStateView{Stage: SessionStageUnknown}, false, nil
	}
	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		if isNotFound(err) {
			return SessionStateView{Stage: SessionStageUnknown}, false, nil
		}
		// F-R2: a WEDGED run cannot honor a signal any more than a closed one — return
		// live=false with the failed stage so SubmitReviewDecision's !live refusal (which
		// points the human at Retry) fires instead of a raw 5xx. Only when Describe CONFIRMED
		// the run live; a Describe blip + task-failed stays a retryable Infrastructure error.
		if describeLive && isWorkflowTaskFailedQueryErr(err) {
			return SessionStateView{Stage: StageDraftFailed}, false, nil
		}
		return SessionStateView{}, false, mapQueryError(err)
	}
	var view SessionStateView
	if err := enc.Get(&view); err != nil {
		return SessionStateView{}, false, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, true, nil
}

// SetReviewCommentStatus applies a REVIEWER status transition to one durable review-ledger
// thread (design §3.3): resolve an OPEN or ANSWERED thread to close it (resolving an
// untouched thread IS the old waive), or reopen a RESOLVED one to put it back in front of
// the drafting agent. It mirrors SubmitReviewDecision's F19 shape —
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
	case projectstate.ReviewCommentResolved, projectstate.ReviewCommentOpen:
		// close (open|answered -> resolved) or reopen (resolved -> open) — the only
		// reviewer-authored transitions. "answered" is derived by the server from the
		// reply history and is never set by a human.
	default:
		return newError(fwmanager.ContractMisuse, "status must be \"resolved\" (to close a thread) or \"open\" (to reopen a resolved thread)")
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// checkCommentTransition validates a REVIEWER status transition against the live thread: the
// comment must exist and the transition must be legal (open->resolved, answered->resolved,
// resolved->open). Any other case is a FailedPrecondition naming the reason (the durable RA
// verb re-checks, but a synchronous refusal is a better caller experience than a silently
// dropped signal). Note what is ABSENT: answered->open is not a transition at all — it falls
// out of the derive rule when a queued reviewer reply makes the last utterance human
// (design §3.3).
func checkCommentTransition(thread []ReviewCommentView, id, status string) error {
	for _, c := range thread {
		if c.ID != id {
			continue
		}
		switch {
		case (c.Status == projectstate.ReviewCommentOpen || c.Status == projectstate.ReviewCommentAnswered) &&
			status == projectstate.ReviewCommentResolved:
			return nil
		case c.Status == projectstate.ReviewCommentResolved && status == projectstate.ReviewCommentOpen:
			return nil
		default:
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot change comment %s from %q to %q (allowed: open->resolved, answered->resolved, resolved->open)", id, c.Status, status))
		}
	}
	return newError(fwmanager.FailedPrecondition, "review comment "+id+" not found in the thread")
}

// bulkResolveAnswered returns the ids of every ANSWERED thread on the slot. Approve
// resolves them all in one gesture, so accepting a redraft that answered eight change
// requests does not cost eight Resolve clicks (design §3.4). Threads the reviewer
// explicitly reopened are OPEN, not answered, so they are excluded and keep blocking.
func bulkResolveAnswered(thread []ReviewCommentView) []string {
	var ids []string
	for _, c := range thread {
		if c.Status == projectstate.ReviewCommentAnswered {
			ids = append(ids, c.ID)
		}
	}
	return ids
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
	case ReviewDecisionUnknown, ReviewAdvance, ReviewSetCommentStatus:
		// ReviewAdvance and ReviewSetCommentStatus are stage-4a ADDITIONS to the enum,
		// made by the merged contract. They never reach this rail: the deliveryManager
		// dispatcher answers both itself (the phase seal, and the comment transition),
		// so they join the ignored arm here rather than changing any rail behaviour.
		// Unreachable: SubmitReviewDecision rejects the zero value as ContractMisuse
		// before reaching the precondition. Guarded for switch-exhaustiveness.
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}
	return nil
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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
// {projectId}:phaseAdvance:systemDesign). Returns the gating outcome.
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
	//
	// describeLive (F-R2) records that Describe CONFIRMED a live execution — only then is a
	// task-failed query below trustworthy as the WEDGED signal (a Describe blip is not, and
	// stays a clean retryable Infrastructure error).
	describeLive := false
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		switch status := desc.GetWorkflowExecutionInfo().GetStatus(); {
		case isAbnormalClosedStatus(status):
			// F-R2 durable-slot-first: a run can die AFTER its artifact landed on main (a
			// died amendment attempt, or a death just after CommitArtifact), so consult the
			// durable slot before falling back to the failed card (see abnormalClosedSessionView).
			view, err := m.abnormalClosedSessionView(ctx, projectID, kind, status)
			if err != nil {
				return SessionStateView{}, err
			}
			return withStageName(view), nil
		case status == enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
			view, err := m.completedSessionView(ctx, projectID, kind)
			if err != nil {
				return SessionStateView{}, err
			}
			return withStageName(view), nil
		}
		// Describe succeeded and the run is neither abnormal-closed nor completed — a LIVE
		// execution (RUNNING / CONTINUED_AS_NEW / PAUSED). A task-failed query below is now
		// trustworthy as the wedged signal.
		describeLive = true
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
		// F-R2: a WEDGED run (workflow task perpetually failing) shows RUNNING to the Describe
		// above but rejects this query with the wedged signature. Do NOT surface a 5xx that
		// leaves the SPA on an infinite GENERATING screen — synthesize the honest failed card
		// so the human can Retry, which supersedes the stuck run (prepareForDraftRequest). Only
		// when Describe CONFIRMED the run live: a Describe blip + task-failed stays a retryable
		// Infrastructure error (we cannot prove the run is genuinely wedged).
		if describeLive && isWorkflowTaskFailedQueryErr(err) {
			return withStageName(wedgedSessionView(projectID, kind)), nil
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// wedgedSessionView synthesizes the honest failed card for a WEDGED run (F-R2): the workflow
// task is perpetually failing, so the sessionState query cannot answer even though the run
// still reports RUNNING to Describe. It reuses StageDraftFailed (the SPA's retry/withdraw
// card), with copy promising that Retry supersedes the stuck session — which
// prepareForDraftRequest actually does (terminate-then-SignalWithStart).
func wedgedSessionView(projectID ProjectID, kind ArtifactKind) SessionStateView {
	reason := "the design session hit an internal fault and cannot answer — Retry to start a fresh draft (the stuck session will be superseded)"
	return SessionStateView{
		ProjectID:     projectID,
		ArtifactKind:  kind,
		Stage:         StageDraftFailed,
		Draft:         DraftModel{Kind: artifactKindWireName(kind)},
		FailureReason: &reason,
	}
}

// abnormalClosedSessionView derives the honest view for a session whose workflow ended
// ABNORMALLY (FAILED/TERMINATED/TIMED_OUT/CANCELED). Durable-slot-first (F-R2): a run can die
// AFTER its artifact landed on main (a died amendment attempt, or a death just after
// CommitArtifact), so consult main's slot before falling back to the failed card:
//
//   - Committed → the committed view (StageCommitted + the model) CARRYING a FailureReason so
//     the last session's abnormal end stays visible; the committed view's amend affordance IS
//     the retry, so this un-deadlocks the died-amendment case with ZERO writes.
//   - Withdrawn → the withdrawn view.
//   - anything else (the run died before committing) → today's failed card, preserving the
//     2026-07-16 anti-wedge fix for a first-draft death.
//
// If the durable slot cannot be consulted — the store is unavailable (nil) or the read
// faults — it falls back to the failed card rather than erroring or panicking: never wedge on
// a recovery read (the failed card still offers Retry), matching the pre-fix behavior exactly.
func (m *systemDesignManager) abnormalClosedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind, status enumspb.WorkflowExecutionStatus) (SessionStateView, error) {
	if m.projectState == nil {
		return failedSessionView(projectID, kind, status), nil
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return failedSessionView(projectID, kind, status), nil
	}
	slot := slotFor(proj, kind)
	switch slot.Status {
	// A settled slot still has a model worth showing, even though the session
	// that produced it died; every other status has nothing to show but the
	// failure.
	case projectstate.ReviewCommitted, projectstate.ReviewWithdrawn:
		view, verr := committedSessionView(projectID, kind, slot)
		if verr != nil {
			return SessionStateView{}, verr
		}
		if slot.Status == projectstate.ReviewCommitted {
			reason := "the last design session ended unexpectedly (" + workflowStatusLabel(status) + "); the committed model shown is unaffected"
			view.FailureReason = &reason
		}
		return view, nil
	case projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected:
		return failedSessionView(projectID, kind, status), nil
	default:
		return failedSessionView(projectID, kind, status), nil
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// terminatedSessionReason renders the neutral human "why" for a session whose workflow
// died abnormally.
func terminatedSessionReason(status enumspb.WorkflowExecutionStatus) string {
	return "the design session ended unexpectedly and is no longer running (" + workflowStatusLabel(status) + "). Retry to start a fresh draft."
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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
	if isWorkflowTaskFailedQueryErr(err) {
		return newError(fwmanager.Infrastructure,
			"design session state is temporarily unavailable — the session hit an internal fault and is being retried by the server; try again shortly")
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// isWorkflowTaskFailedQueryErr reports whether a QueryWorkflow error is the WEDGED-RUN
// signature (F-R2): the workflow task is perpetually failing (a deploy-time non-determinism
// fault, a panic loop), so Temporal rejects the sessionState query with "...Workflow Task in
// failed state" EVEN THOUGH DescribeWorkflowExecution still reports the run RUNNING. This is
// the classification that (1) lets GetSessionState / reviewGateView synthesize an honest
// failed view instead of a 5xx, and (2) authorizes RequestArtifactDraft to TERMINATE the
// wedged run before starting a fresh one. A transient query timeout/Unavailable does NOT
// match — it must never trigger a terminate. Same substring precedent as mapQueryError.
func isWorkflowTaskFailedQueryErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Workflow Task in failed state")
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// newSessionRef constructs a SessionRef from an infrastructure identity. Internal to
// the Manager; Clients only ever receive and echo SessionRefs.
func newSessionRef(opaque string) SessionRef { return SessionRef(opaque) }

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// toPSKind converts systemdesign's OWN ArtifactKind to the canonical
// projectstate.ArtifactKind (ordinal-preserving) for behavior + RA-boundary calls.
func toPSKind(k ArtifactKind) projectstate.ArtifactKind { return projectstate.ArtifactKind(k) }

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// artifactKindString returns the PascalCase Go-identifier name for an ArtifactKind
// (the dispatch-input + PR-title + diagnostic form). Mirrors projectstate String().
func artifactKindString(k ArtifactKind) string { return toPSKind(k).String() }

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// predecessorNotCommittedMsg is the FailedPrecondition detail naming the uncommitted
// predecessor that blocks the requested draft (by its canonical camelCase wire name).
func predecessorNotCommittedMsg(pred ArtifactKind) string {
	return fmt.Sprintf("predecessor artifact %q must be committed before this kind can be drafted", artifactKindWireName(pred))
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	modelEnvelope = projectstate.ModelEnvelope
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	projectEnvelope = projectstate.ProjectEnvelope
)

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
const acknowledgeStaleMaxAttempts = 5

// AcknowledgeStaleBasis clears the committed slot's StaleBasis and records the reviewer's
// note as a staleAck audit entry. Synchronous OCC write (mirrors SetResearchInput).
func (m *systemDesignManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if strings.TrimSpace(note) == "" {
		return newError(fwmanager.ContractMisuse, "an acknowledgement requires a non-empty note — it is the reviewer's durable justification, and it also keys the idempotency of the ack")
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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
	// A QUESTION THREAD IS A CONVERSATION (comment-margin task 5b). An ask carrying a
	// replyTo is a FOLLOW-UP on a thread the agent already answered, not a new question — so
	// this door ROUTES it into that thread instead of refusing it. (requestArtifactDraft
	// still refuses one: it seeds round-0 threads before any thread is loaded, so it could
	// only re-file a reply as a fresh unanchored comment.) The batch is partitioned ONCE
	// here, before the ledger read, because both the emptiness refusal and the idempotency
	// key must see the whole batch; the replyTo targets are checked against the live thread
	// inside the loop. `at` is stamped once, outside the loop, so an OCC retry re-applies the
	// identical utterance rather than duplicating it.
	at := time.Now().UTC().Format(time.RFC3339)
	freshAsks, replies := partitionIncomingComments(questions, at)
	qs := questionsToLedger(addressee, freshAsks)
	// A REPLY-ONLY batch is a legitimate ask — the follow-up IS the question this round.
	if len(qs) == 0 && len(replies) == 0 {
		return newError(fwmanager.ContractMisuse, "no questions to ask (every question needs text)")
	}

	// Resolve the branch the ledger lives on: a live drafting/review session keeps the
	// thread on its session branch; a committed (or absent) session keeps it on main ("").
	branch := m.resolveQuestionBranch(rc, projectID, kind)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := askQuestionsIdempotencyKey(projectID, kind, branch, qs, replies)

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
		// A replyTo naming no thread on this artifact is a hard refusal, never a silent new
		// thread — the same rule the change-request door applies, run here against the thread
		// just read (the RA would surface a bare NotFound from deep inside the append).
		if perr := checkReplyTargets(ledgerCommentIDs(thread), questions); perr != nil {
			return perr
		}
		round := nextQuestionRound(thread)
		if r, ok := existingQuestionRound(thread, qs); ok {
			// A prior ask already seeded these exact questions (its answer-job dispatch may
			// have been dropped — F82). Reuse their round so the minted ids match the EXISTING
			// ledger entries, and the re-fired answer job answers the right comments.
			round = r
		}
		_, err = m.designSession.SeedReviewCommentsOnBranch(fwra.Context{Context: ctx}, psID, proj.Version, branch, psKind, round, qs, replies, key)
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
			// A REPLY is answered by the role its THREAD is addressed to, not by whoever the
			// caller named — see answerJobAddressee. Dispatching the other role's command would
			// start a session that finds nothing addressed to it, leaving the follow-up
			// unanswered forever with no signal.
			dispatchTo, mixed := answerJobAddressee(thread, addressee, len(qs), replies)
			if mixed {
				slog.Default().Warn("askQuestions: this batch needs BOTH answer roles (a fresh question for one, a reply on the other's thread) — only one answer job is dispatched, so the other half stays unanswered until it is re-asked on its own",
					"op", "systemdesign.AskQuestions", "projectID", string(projectID),
					"artifactKind", artifactKindString(kind), "dispatchedTo", dispatchTo)
			}
			m.dispatchAnswerJob(ctx, projectID, kind, branch, dispatchTo, minted)
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

// readProjectMaybeBranch reads the head-state aggregate from the given branch. The
// on-branch read moved onto the designSessionAccess facet (Wave 1 reconciliation), which
// ships the aggregate as a ProjectEnvelope across the Manager-Temporal boundary; decode it
// back to the concrete Project here. branch=="" reads main exactly as ReadProject.
func (m *systemDesignManager) readProjectMaybeBranch(ctx context.Context, psID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	env, err := m.designSession.ReadProjectOnBranch(fwra.Context{Context: ctx}, psID, branch)
	if err != nil {
		return projectstate.Project{}, err
	}
	return env.Decode()
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// answerJobAddressee decides which role the answer job must be dispatched to, given the batch
// that was just appended. A FRESH question is addressed by its asker, so the caller's argument
// decides. A REPLY is NOT: it lands in a thread that already has its own addressee, and both
// answer commands select only "the OPEN questions addressed to YOU" — so dispatching the other
// role's command starts a session that finds nothing to do, and the follow-up sits unanswered
// forever with no signal anywhere. The client cannot defend this (it does not know the thread's
// addressee either), so the thread the reply names decides. A reply onto a thread with no
// addressee at all (a change-request) falls back to the caller's.
//
// mixed reports that the batch genuinely needs BOTH roles — a fresh question for one and a
// reply on the other's thread, which the SPA can produce because it groups by the STAGED
// addressee, not by the target thread's. One dispatch cannot serve both, so the caller's
// addressee is kept (today's behaviour for the fresh half) and the caller logs the disagreement
// rather than silently answering half the batch.
func answerJobAddressee(thread []projectstate.ReviewComment, callerAddressee string, freshCount int, replies []projectstate.ReviewReply) (string, bool) {
	addresseeOf := make(map[string]string, len(thread))
	for _, c := range thread {
		addresseeOf[c.ID] = c.Addressee
	}
	chosen, mixed := "", false
	note := func(a string) {
		switch {
		case a == "":
		case chosen == "":
			chosen = a
		case chosen != a:
			mixed = true
		}
	}
	if freshCount > 0 {
		note(callerAddressee)
	}
	for _, r := range replies {
		if a := addresseeOf[r.CommentID]; a != "" {
			note(a)
			continue
		}
		note(callerAddressee)
	}
	if chosen == "" {
		chosen = callerAddressee
	}
	return chosen, mixed
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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
//
// The REPLY half of the batch is hashed too (task 5b): a follow-up on an existing question
// thread adds no fresh entry, so a qs-only key would give two different follow-ups — or a
// follow-up and a bare re-ask — the SAME key, and the RA would swallow the second as a
// duplicate. The utterance's `at` is deliberately excluded: it is wall-clock on this sync op,
// and including it would defeat the re-ask dedup the key exists for.
func askQuestionsIdempotencyKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment, replies []projectstate.ReviewReply) fwra.IdempotencyKey {
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
	for _, r := range replies {
		_, _ = h.Write([]byte(r.CommentID))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(r.Text))
		_, _ = h.Write([]byte{0})
	}
	return fwra.IdempotencyKey(fmt.Sprintf("%s:%d:askQuestions:%x", projectID, int(kind), h.Sum64()))
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// answerJobDispatchSeq makes each explicit AskQuestions call produce a UNIQUE answer-job
// dispatch key, so a re-ask RE-FIRES the answer job (the RA dedups on the whole key, so a
// content-only key would swallow the re-fire — F82). AskQuestions is a direct, non-retried
// manager op (exactly one dispatch per successful call), so a per-call nonce cannot
// double-fire a single logical ask; it only enables the re-ask recovery.
var answerJobDispatchSeq atomic.Uint64

// answerJobDispatchKey derives a per-call-unique answer-job idempotency key from the content
// base plus a monotonic nonce (see answerJobDispatchSeq). The reply half is not folded into
// the base: the nonce ALREADY makes every dispatch key distinct, which is this key's whole
// purpose, so a reply-only ask still fires its own answer job.
func answerJobDispatchKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	base := askQuestionsIdempotencyKey(projectID, kind, branch, qs, nil)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:answerJob:%d", base, answerJobDispatchSeq.Add(1)))
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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
	// fire-and-forget submit over the PUBLISHED agenticJobAccess RA. The
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
	spec := agenticjob.PipelineSpec{
		ProjectID: agenticjob.ProjectID(projectID),
		Steps: []agenticjob.PipelineStep{{
			Name:      "design",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
		WorkflowFile:   designWorkflowFileName,
	}
	key := answerJobDispatchKey(projectID, kind, branch, qs)
	handle, err := m.pipeline.SubmitAgenticJob(fwra.Context{Context: ctx, IdempotencyKey: key}, spec)
	if err != nil {
		log.Error("answer job dispatch FAILED — the question is recorded but not auto-answered; re-run AskQuestions with the same question to retry",
			"err", err.Error(), "key", string(key))
		return
	}
	log.Info("answer job dispatched", "key", string(key))
	m.watchAnswerEpisode(ctx, projectID, kind, handle, log)
}

// ---------------------------------------------------------------------------
// Episode capture for the ANSWER job (SP1 capture-seam, Task 7)
// ---------------------------------------------------------------------------
//
// The answer job is the ONE agentic dispatch this Manager makes outside a Temporal
// workflow: AskQuestions submits it fire-and-forget and returns. Nothing observes it, so
// without this watch every answer episode — real tokens, really spent — would be invisible
// to the ledger.
//
// NON-DURABLE BY CONSTRUCTION, and that is accepted: this is a plain goroutine in the
// server process. A restart between the dispatch and the terminal observation loses the
// watch and therefore the record — no gap line either, because nothing is left to write
// one. Only the WORKFLOW-side capture paths carry the durable never-silent guarantee; the
// answer job is auxiliary (it gates nothing) and did not warrant its own workflow.

const (
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// answerEpisodePollInterval spaces the manager-side observe loop. Same order as the
	// workflow-side observePollInterval — an answer job is the same kind of agentic run.
	answerEpisodePollInterval = 15 * time.Second
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// answerEpisodeWatchWindow is the hard deadline on the watch. Past it the episode is
	// recorded as an explicit GAP rather than watched forever by a leaked goroutine.
	answerEpisodeWatchWindow = 30 * time.Minute
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// answerEpisodeAppendWindow bounds the ledger append that follows the watch. It is a
	// SEPARATE budget from answerEpisodeWatchWindow on purpose — see run().
	answerEpisodeAppendWindow = 30 * time.Second
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// answerEpisodeAppendAttempts / answerEpisodeAppendBackoff are this path's stand-in
	// for the Temporal retry envelope the workflow-side append rides. Small and bounded:
	// a local sidecar append that fails three times in a row is not transient.
	answerEpisodeAppendAttempts = 3
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	answerEpisodeAppendBackoff = 250 * time.Millisecond
)

// watchAnswerEpisode spawns the bounded manager-side watch for one dispatched answer job.
// It detaches from the CALLER'S context on purpose: ctx is the AskQuestions request
// context and is cancelled the moment that call returns, while the job it dispatched runs
// for minutes afterwards. WithoutCancel keeps the request's values (tracing, principal)
// and drops only the cancellation.
func (m *systemDesignManager) watchAnswerEpisode(ctx context.Context, projectID ProjectID, kind ArtifactKind, handle agenticjob.PipelineHandle, log *slog.Logger) {
	w := answerEpisodeWatch{
		pipeline: m.pipeline,
		episodes: m.episodes,
		poll:     answerEpisodePollInterval,
		window:   answerEpisodeWatchWindow,
		log:      log,
	}
	go w.run(context.WithoutCancel(ctx), projectID, artifactKindString(kind), handle)
}

// answerEpisodeWatch is the bounded observe-then-append loop behind watchAnswerEpisode,
// broken out with its timings injected so it can be exercised deterministically in tests.
type answerEpisodeWatch struct {
	pipeline agenticjob.AgenticJobAccess
	episodes episode.EpisodeAccess
	poll     time.Duration
	window   time.Duration
	log      *slog.Logger
}

// run polls handle to a terminal phase (or to the window's end) and appends the ONE ledger
// record the dispatch owes. Blocking — watchAnswerEpisode spawns it.
func (w answerEpisodeWatch) run(ctx context.Context, projectID ProjectID, targetRef string, handle agenticjob.PipelineHandle) {
	if w.pipeline == nil || w.episodes == nil {
		return
	}
	watchCtx, cancelWatch := context.WithTimeout(ctx, w.window)
	defer cancelWatch()

	obs, terminal := w.observeToTerminal(watchCtx, handle)
	if terminal && episodeVenueIsRemote(obs.RunURL) {
		// Remote venue mines no episode in v1 — nothing was lost, so record nothing.
		return
	}
	rec := w.answerRecord(obs, terminal, targetRef, handle)

	// THE APPEND MUST NOT RIDE watchCtx. On the DEADLINE path observeToTerminal returned
	// precisely BECAUSE watchCtx expired, so appending under it would hand the ledger an
	// already-cancelled context — making the gap record the deadline exists to write the
	// one write guaranteed to fail. Derive a fresh, cancellation-free budget from the
	// caller's context instead. (Today's AppendEpisode realisations ignore the context
	// entirely, so this is latent rather than live; a store that honours it would turn the
	// never-silent guarantee into a silent loss on exactly the path that needs it most.)
	appendCtx, cancelAppend := context.WithTimeout(context.WithoutCancel(ctx), answerEpisodeAppendWindow)
	defer cancelAppend()
	w.appendRecord(appendCtx, projectID, rec, handle)
}

// appendRecord writes the record with a small BOUNDED retry. The workflow-side capture
// gets Temporal's retry envelope for free; this path has none, so without it a single
// transient store stumble would lose the episode outright.
func (w answerEpisodeWatch) appendRecord(ctx context.Context, projectID ProjectID, rec episode.EpisodeRecord, handle agenticjob.PipelineHandle) {
	key := fwra.IdempotencyKey("answerEpisode:" + string(handle))
	var err error
	for attempt := 1; attempt <= answerEpisodeAppendAttempts; attempt++ {
		err = w.episodes.AppendEpisode(fwra.Context{Context: ctx, IdempotencyKey: key},
			episode.ProjectID(projectID), rec)
		if err == nil {
			return
		}
		if attempt == answerEpisodeAppendAttempts ||
			!waitOrDone(ctx, time.Duration(attempt)*answerEpisodeAppendBackoff) {
			break
		}
	}
	w.log.Error("answer-job episode NOT recorded: ledger append failed after its bounded retry",
		"episodeId", rec.EpisodeID, "attempts", answerEpisodeAppendAttempts, "err", err.Error())
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// waitOrDone sleeps for d, returning false the moment ctx is done instead.
func waitOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// observeToTerminal polls the dispatched job until it reaches a terminal phase, the
// window closes, or the RA faults. terminal=false means the second or third — the caller
// turns that into a gap record.
func (w answerEpisodeWatch) observeToTerminal(ctx context.Context, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, bool) {
	var last agenticjob.PipelineObservation
	cancelGrace := 0
	for {
		obs, err := w.pipeline.ObserveAgenticJob(fwra.Context{Context: ctx}, handle)
		if err != nil {
			return last, false
		}
		last = obs
		if terminal, done := w.classify(obs, &cancelGrace); done {
			return obs, terminal
		}
		if !waitOrDone(ctx, w.poll) {
			return last, false
		}
	}
}

// classify decides whether THIS observation ends the watch. A terminal observation with a
// summary always does. A terminal observation WITHOUT one ends it too — except for the
// CANCEL RACE, where the phase flips synchronously while the agent subprocess is still
// unwinding: that gets maxLateEpisodePolls further polls (the same grace the workflow-side
// capture gives it) before the run is written off.
func (w answerEpisodeWatch) classify(obs agenticjob.PipelineObservation, cancelGrace *int) (terminal, done bool) {
	if !designPipelinePhase(obs.Phase).IsTerminal() {
		return false, false
	}
	if obs.Episode != nil || obs.Phase != agenticjob.PhaseCancelled {
		return true, true
	}
	if *cancelGrace >= maxLateEpisodePolls {
		return true, true
	}
	*cancelGrace++
	return false, false
}

// answerRecord composes the ledger record for a watched answer job: the mined summary, or
// an explicit GAP naming which of the two ways it went missing.
func (w answerEpisodeWatch) answerRecord(obs agenticjob.PipelineObservation, terminal bool, targetRef string, handle agenticjob.PipelineHandle) episode.EpisodeRecord {
	// Lineage is nil BY DESIGN: this dispatch has no durable execution behind it.
	if terminal && obs.Episode != nil {
		return episodeRecordFromSummary(*obs.Episode, episode.EpisodeKindAnswer, targetRef, nil, obs.Diagnostic)
	}
	reason := episodeMissingSummaryReason
	if !terminal {
		reason = "answer job did not reach a terminal phase within the manager-side watch window"
	}
	return episodeGapRecord(episode.EpisodeKindAnswer, targetRef, nil,
		"gap-"+episodeIDSafe(string(handle)),
		episodeGapReason(reason, obs.Diagnostic), time.Now().UTC())
}

// ---------------------------------------------------------------------------
// Episode record composition — shared by the workflow-side capture
// (coauthorartifact.go) and the answer-job watch above.
// ---------------------------------------------------------------------------

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeMissingSummaryReason is the GapReason for the "the run terminated and reported
// no episode at all" case — the one the never-silent rule exists for.
const episodeMissingSummaryReason = "terminal observation carried no episode summary"

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeRecordFromSummary copies a mined EpisodeSummary onto an EpisodeRecord field for
// field — VERBATIM, no recomputation — and stamps the Manager-known Kind/TargetRef/
// Lineage the RA cannot know. WorkerClass is left unset: a design dispatch carries the
// artifact kind and the job mode, never the Phase-2 activity list's workerClass, so there
// is no honest value to put here. diagnostic supplies the GapReason when the RA itself
// reported a GAP outcome (a restart-lost run recovered from its orphaned trace) — the
// observation's diagnostic IS the explanation there, since EpisodeSummary carries no
// reason field.
func episodeRecordFromSummary(s agenticjob.EpisodeSummary, kind episode.EpisodeKind, targetRef string, lineage *episode.EpisodeLineage, diagnostic string) episode.EpisodeRecord {
	rec := episode.EpisodeRecord{
		EpisodeID:      s.EpisodeID,
		Kind:           kind,
		TargetRef:      targetRef,
		Lineage:        lineage,
		Model:          s.Model,
		Usage:          episode.EpisodeUsage(s.Usage),
		CostUSD:        s.CostUSD,
		NumTurns:       s.NumTurns,
		ToolCallCounts: s.ToolCallCounts,
		SubagentSpans:  episodeSubagentSpans(s.SubagentSpans),
		StartedAt:      s.StartedAt,
		EndedAt:        s.EndedAt,
		Outcome:        episodeOutcomeFrom(s.Outcome),
		TracePath:      s.TracePath,
	}
	if s.StreamedUsage != nil {
		u := episode.EpisodeUsage(*s.StreamedUsage)
		rec.StreamedUsage = &u
	}
	if rec.Outcome == episode.EpisodeGap {
		reason := episodeGapReason("the run reported a gap episode", diagnostic)
		rec.GapReason = &reason
	}
	return rec
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeGapRecord composes the SYNTHESIZED gap record for a dispatch that produced no
// summary at all. now is supplied by the caller (workflow.Now on the replay-deterministic
// workflow paths) because the run's own clock is exactly what was lost.
func episodeGapRecord(kind episode.EpisodeKind, targetRef string, lineage *episode.EpisodeLineage, episodeID, reason string, now time.Time) episode.EpisodeRecord {
	return episode.EpisodeRecord{
		EpisodeID: episodeID,
		Kind:      kind,
		TargetRef: targetRef,
		Lineage:   lineage,
		StartedAt: now,
		EndedAt:   now,
		Outcome:   episode.EpisodeGap,
		GapReason: &reason,
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeGapReason joins the Manager's own reason to the observation's diagnostic when
// the RA supplied one, so a gap says both WHAT was lost and what the rail reported.
func episodeGapReason(reason, diagnostic string) string {
	if strings.TrimSpace(diagnostic) == "" {
		return reason
	}
	return reason + " — " + diagnostic
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeSubagentSpans re-types the mined subagent spans onto the ledger contract's own
// span type (identical shapes, distinct contracts — contracts are self-contained).
func episodeSubagentSpans(in []agenticjob.SubagentSpan) []episode.SubagentSpan {
	if len(in) == 0 {
		return nil
	}
	out := make([]episode.SubagentSpan, 0, len(in))
	for _, s := range in {
		out = append(out, episode.SubagentSpan(s))
	}
	return out
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeOutcomeFrom maps the observation contract's outcome onto the ledger contract's.
// Written as a TOTAL switch rather than a numeric cast so a future divergence between the
// two independently-versioned contracts is a compile-time conversation, not silent drift.
func episodeOutcomeFrom(o agenticjob.EpisodeOutcome) episode.EpisodeOutcome {
	switch o {
	case agenticjob.EpisodeSucceeded:
		return episode.EpisodeSucceeded
	case agenticjob.EpisodeFailed:
		return episode.EpisodeFailed
	case agenticjob.EpisodeCancelled:
		return episode.EpisodeCancelled
	case agenticjob.EpisodeGap:
		return episode.EpisodeGap
	default:
		return episode.EpisodeGap
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeIDSafe rewrites s into the [A-Za-z0-9._-] alphabet episodeAccess requires of an
// EpisodeID. A rejected id is ContractMisuse — non-retryable — so a gap record seeded from
// a raw pipeline handle (which carries a ':') would be dropped on the floor, exactly
// defeating the never-silent rule the gap record exists to serve.
func episodeIDSafe(s string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, s)
	if safe == "" {
		return "unknown"
	}
	return safe
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
			return "", sdMapRAError(err, "sourceControlAccess.AdoptProjectRepo")
		}
		cred, err := m.rail.GetInstallationToken(fwra.Context{Context: ctx}, repo)
		if err != nil {
			return "", sdMapRAError(err, "sourceControlAccess.GetInstallationToken")
		}
		files, err := sourcecontrol.ManagedScaffoldFiles(repo, sourcecontrol.RailAppSlug(m.rail))
		if err != nil {
			return "", sdMapRAError(err, "sourceControlAccess.ManagedScaffoldFiles")
		}
		if _, err := m.rail.CommitManagedFiles(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, files, cred); err != nil {
			return "", sdMapRAError(err, "sourceControlAccess.CommitManagedFiles")
		}
	}

	if _, err := m.projectState.CreateProject(fwra.Context{Context: ctx, IdempotencyKey: key},
		projectstate.ProjectID(projectID), projectstate.OwnerScope(owner), name); err != nil {
		return "", sdMapRAError(err, "projectStateAccess.CreateProject")
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
			return 0, sdMapRAError(err, "projectStateAccess.ReadProject")
		}
		newVersion, err := m.projectState.SetOperatingModel(fwra.Context{Context: ctx, IdempotencyKey: key}, psID, proj.Version, psModel)
		if err == nil {
			return Version(newVersion), nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue // re-read head Version, re-apply (same idempotencyKey)
		}
		return 0, sdMapRAError(err, "projectStateAccess.SetOperatingModel")
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
		return nil, sdMapRAError(err, "projectStateAccess.ListProjects")
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
		return ProjectState{}, sdMapRAError(err, "projectStateAccess.ReadProject")
	}
	m.computeNetworkAtRead(&proj)
	m.computeDeploymentEdgesAtRead(&proj)
	return m.projectStateToContract(proj), nil
}

// GetDesignHealth returns the LIVE design-health read-model for one project: the
// mechanical Method-rule findings evaluated render-on-read over the COMMITTED
// project.json, PLUS the committed waiver / attestation ledgers, stamped with the
// state revision the findings ran against. It never mutates state: a clean design
// returns empty finding/waiver/attestation slices. This is the getDesignHealth
// VIEW op (ui.view "design-health" — the view slug, not the component id); the
// DesignHealthEngine call below is the code that BACKS the
// SystemDesignManager → DesignHealthEngine architecture edge, an ordinary
// downward M→E call.
func (m *systemDesignManager) GetDesignHealth(rc fwmanager.Context, projectID ProjectID) (DesignHealth, error) {
	ctx := rc.Context
	if projectID == "" {
		return DesignHealth{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		// Same clean NotFound mapping as GetProject — do not leak the git call chain.
		if raErr := (*fwra.Error)(nil); errors.As(err, &raErr) && raErr.Kind == fwra.NotFound {
			return DesignHealth{}, fwmanager.Wrap(fwmanager.NotFound, err, fmt.Sprintf("project %q not found", projectID))
		}
		return DesignHealth{}, sdMapRAError(err, "projectStateAccess.ReadProject")
	}

	// Live findings: re-serialize the committed aggregate to its canonical
	// project.json bytes (the same bytes-in contract putDraftModel and CI feed) and
	// run the shared live-tier rule engine over them.
	raw, err := projectstate.EncodeProjectJSON(proj)
	if err != nil {
		return DesignHealth{}, fwmanager.Wrap(fwmanager.Infrastructure, err, "projectStateAccess.EncodeProjectJSON")
	}
	live, err := m.designHealth.EvaluateDesignHealth(fweng.Context{Context: ctx}, raw)
	if err != nil {
		return DesignHealth{}, fwmanager.Wrap(fwmanager.Infrastructure, err, "designHealthEngine.EvaluateDesignHealth")
	}
	findings := findingsToContract(live)

	// Committed ledgers: waivers live on BOTH the systemDesign slot (App-C standard
	// items) and the volatilities slot; attestations live on the systemDesign slot.
	// Initialized non-nil so the required contract arrays serialize as [] not null.
	waivers := []CheckItem{}
	attestations := []CheckItem{}
	if sys, ok := slotFor(proj, KindSystem).Model.(*projectstate.System); ok && sys != nil {
		waivers = append(waivers, checkItemsToContract(sys.Waivers)...)
		attestations = append(attestations, checkItemsToContract(sys.Attestations)...)
	}
	if vol, ok := slotFor(proj, KindVolatilities).Model.(*projectstate.Volatilities); ok && vol != nil {
		waivers = append(waivers, checkItemsToContract(vol.Waivers)...)
	}

	return DesignHealth{
		Findings:            findings,
		Waivers:             waivers,
		Attestations:        attestations,
		EvaluatedAtRevision: int64(proj.Version),
	}, nil
}

// findingsToContract maps the platform methodcheck.Finding values the live-tier
// rule engine mints into the systemDesignManager contract Finding VIEW shape. The
// slice is always non-nil so the required contract array serializes as [] not null.
func findingsToContract(in []methodcheck.Finding) []Finding {
	out := make([]Finding, 0, len(in))
	for _, f := range in {
		fc := Finding{
			RuleID:   RuleID(f.RuleID),
			Severity: severityToContract(f.Severity),
			Message:  f.Message,
		}
		if f.Location != nil {
			fc.Location = &Location{Ordinal: int64(f.Location.Ordinal), Section: f.Location.Section}
		}
		out = append(out, fc)
	}
	return out
}

// severityToContract renders a methodcheck severity ordinal as its VIEW string
// (info/warning/error) — the contract Severity is a string enum where methodcheck's
// is an int, so the value is translated by the same names the wire form uses.
func severityToContract(s methodcheck.Severity) Severity {
	switch s {
	case methodcheck.SeverityError:
		return SeverityError
	case methodcheck.SeverityWarning:
		return SeverityWarning
	case methodcheck.SeverityInfo:
		return SeverityInfo
	default:
		return SeverityInfo
	}
}

// checkItemsToContract maps committed RA CheckItem ledger entries (waivers /
// attestations) to the contract CheckItem VIEW shape, translating the RA int
// CheckStatus enum to its wire string exactly as GetProject converts its enums.
func checkItemsToContract(in []projectstate.CheckItem) []CheckItem {
	out := make([]CheckItem, 0, len(in))
	for _, it := range in {
		out = append(out, CheckItem{
			Section:       it.Section,
			Guideline:     it.Guideline,
			Status:        checkStatusToView(it.Status),
			Justification: it.Justification,
		})
	}
	return out
}

// checkStatusToView maps the RA CheckStatus int enum to its VIEW wire string,
// mirroring projectstate's own checkStatusNames (pass/waived/fail).
func checkStatusToView(s projectstate.CheckStatus) string {
	switch s {
	case projectstate.CheckWaived:
		return "waived"
	case projectstate.CheckFail:
		return "fail"
	case projectstate.CheckPass:
		return "pass"
	default:
		return "pass"
	}
}

// sdMapRAError translates a projectStateAccess / sourceControlAccess error into the
// Manager façade error model. fwra.NotFound → NotFound; fwra.ContractMisuse →
// ContractMisuse; everything else (incl. Conflict — a thin read/catalog op has no
// optimistic-concurrency loop to recover it) → Infrastructure with the original
// retryability preserved. label identifies the ACTUAL failing dependency+op (e.g.
// "sourceControlAccess.AdoptProjectRepo") — CreateProject fans across two RAs, so a
// fixed label would misattribute a source-control fault to projectStateAccess. It is
// the opaque Detail returned to the client; the full cause chain stays server-side
// (Cause), surfaced only in the composition-root log.
func sdMapRAError(err error, label string) error {
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

// computeDeploymentEdgesAtRead populates each deployment environment's
// COMPUTE-AT-READ block with the relationships derived from the committed System
// model. NO-OP when either slot is absent — a project that has not reached its
// architecture yet has nothing to derive from, and the authored edges (if any)
// still serve on their own.
func (m *systemDesignManager) computeDeploymentEdgesAtRead(p *projectstate.Project) {
	if m.designHealth == nil {
		return
	}
	op, ok := p.OperationalConcepts.Model.(*projectstate.DeploymentOperationsModel)
	if !ok || op == nil || len(op.Deployment.Environments) == 0 {
		return
	}
	sys, ok := p.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return
	}

	derived, err := m.designHealth.DeriveDeploymentEdges(
		fweng.Context{Context: context.Background()},
		toMethodcheckSystem(*sys),
		toMethodcheckTopology(op.Deployment),
	)
	if err != nil {
		return // degenerate input guard — serve the authored edges unenriched
	}

	for i := range op.Deployment.Environments {
		env := &op.Deployment.Environments[i]
		edges, has := derived[env.Profile.String()]
		if !has {
			env.Computed = nil
			continue
		}
		env.Computed = &projectstate.DeploymentEnvironmentComputed{
			DerivedRelationships: fromMethodcheckRelationships(edges),
		}
	}
}

// toMethodcheckSystem maps the committed System onto the platform's string-typed
// model. Only the fields derivation reads are carried: it joins components to
// containers by NAME and to infrastructure by name slug, and needs each
// component's kind to exempt the utilities.
func toMethodcheckSystem(s projectstate.System) methodcheck.System {
	out := methodcheck.System{
		Components:    make([]methodcheck.Component, 0, len(s.Components)),
		Relationships: make([]methodcheck.Relationship, 0, len(s.Relationships)),
	}
	for _, c := range s.Components {
		out.Components = append(out.Components, methodcheck.Component{
			ID: c.ID, Name: c.Name, Kind: c.Kind.String(),
		})
	}
	for _, r := range s.Relationships {
		out.Relationships = append(out.Relationships, methodcheck.Relationship{
			From: r.From, To: r.To, Mode: r.Mode.String(), Label: r.Label,
		})
	}
	return out
}

// toMethodcheckTopology maps the committed deployment topology onto the
// platform's model. The AUTHORED relationships are deliberately NOT carried:
// derivation must not see them, or a re-read would fold its own previous output
// back into its input.
func toMethodcheckTopology(t projectstate.DeploymentTopology) methodcheck.DeploymentTopology {
	out := methodcheck.DeploymentTopology{
		DeliveryStyle: t.DeliveryStyle.String(),
		Containers:    make([]methodcheck.DeployContainer, 0, len(t.Containers)),
		Environments:  make([]methodcheck.DeploymentEnvironment, 0, len(t.Environments)),
	}
	for _, c := range t.Containers {
		out.Containers = append(out.Containers, methodcheck.DeployContainer{
			Key: c.Key, Name: c.Name, Technology: c.Technology,
			Description: c.Description, Components: c.Components,
			Surface: string(c.Surface),
		})
	}
	for _, env := range t.Environments {
		out.Environments = append(out.Environments, methodcheck.DeploymentEnvironment{
			Profile: env.Profile.String(),
			Title:   env.Title,
			Nodes:   toMethodcheckNodes(env.Nodes),
		})
	}
	return out
}

func toMethodcheckNodes(nodes []projectstate.DeploymentNode) []methodcheck.DeploymentNode {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]methodcheck.DeploymentNode, 0, len(nodes))
	for _, n := range nodes {
		mapped := methodcheck.DeploymentNode{
			Key: n.Key, Name: n.Name, Technology: n.Technology,
			Description: n.Description, Instances: n.Instances,
			Children: toMethodcheckNodes(n.Children),
		}
		for _, ci := range n.ContainerInstances {
			mapped.ContainerInstances = append(mapped.ContainerInstances, methodcheck.ContainerInstance{
				Key: ci.Key, ContainerKey: ci.ContainerKey, Note: ci.Note,
			})
		}
		for _, in := range n.InfrastructureNodes {
			mapped.InfrastructureNodes = append(mapped.InfrastructureNodes, methodcheck.InfrastructureNode{
				Key: in.Key, Name: in.Name, Technology: in.Technology,
				Description: in.Description, Role: string(in.Role),
			})
		}
		for _, ss := range n.SoftwareSystemInstances {
			mapped.SoftwareSystemInstances = append(mapped.SoftwareSystemInstances, methodcheck.SoftwareSystemInstance{
				Key: ss.Key, Name: ss.Name, Technology: ss.Technology,
				Description: ss.Description, Role: string(ss.Role),
			})
		}
		out = append(out, mapped)
	}
	return out
}

// fromMethodcheckRelationships maps the derived edges back onto the served model.
// The mode round-trips through the wire name so the served edge carries the same
// sync/queued vocabulary the System relationship did.
func fromMethodcheckRelationships(edges []methodcheck.DeploymentRelationship) []projectstate.DeploymentRelationship {
	out := make([]projectstate.DeploymentRelationship, 0, len(edges))
	for _, e := range edges {
		out = append(out, projectstate.DeploymentRelationship{
			From: e.From, To: e.To, Label: e.Label, Technology: e.Technology,
			Mode: callModeFromWireName(e.Mode),
		})
	}
	return out
}

// callModeFromWireName resolves the platform model's camelCase mode name back to
// the typed CallMode, defaulting to sync for a name this build does not know.
func callModeFromWireName(name string) projectstate.CallMode {
	switch name {
	case "queued":
		return projectstate.CallQueued
	case "eventPubSub":
		return projectstate.CallEventPubSub
	default:
		return projectstate.CallSync
	}
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
		ProjectID: ProjectID(s.ProjectID),
		Name:      s.Name,
		Owner:     OwnerScope(s.Owner),
		Phase:     phase,
		PhaseName: phaseName(phase),
		// ConstructionComplete (Task 13 fix round 1): the RA-level derived flag
		// (projectstate.isConstructionComplete, surfaced on ListProjects) carried
		// straight through — this manager's ProjectSummary is what the SPA catalog
		// actually reads (the RA's own ProjectSummary never crosses the client
		// boundary), so the field must be copied here for the browser to see it.
		ConstructionComplete: s.ConstructionComplete,
		CommittedCount:       int64(s.CommittedCount),
		TotalCount:           int64(s.TotalCount),
		UpdatedAt:            s.UpdatedAt,
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
		OperatingModel:      OperatingModel(string(p.OperatingModel.OrDefault())),
		Research:            researchToContract(p.Research),
		Slots:               slotsToContract(p),
		GitRows:             m.gitRowsToContract(ProjectID(p.ID), p.ActivityGit),
		ActivityExecution:   constructionRowsToContract(p.ActivityExecution, activityMetaByID(p), componentLayerByID(p), constructionPlanFor(p)),
		ConstructionStarted: constructionStartedFor(p.ActivityExecution),
		// The recorded operator pause, passed through as stored (plan B1.7): the console
		// offers Resume in Begin's place while it holds.
		OperatorPaused:       p.OperatorPaused,
		PauseReason:          pauseReasonToContract(p.PauseReason),
		ConstructionProgress: m.constructionProgressToContract(p),
		ServiceContracts:     serviceContractsToContract(p.ServiceContracts),
		ReviewPolicy:         reviewPolicyToContract(p.ReviewPolicy),
		TestingState:         testingStateToContract(p.TestingState),
	}
}

// pauseReasonToContract is the pause reason on the wire: omitted when there is none.
func pauseReasonToContract(reason string) *string {
	if reason == "" {
		return nil
	}
	return &reason
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

// worstOriginFor is the wire worstOrigin: the ledger's roll-up on a recorded row, and
// omitted (nil) on a planned-no-record one, which has no ledger to roll up and whose
// empty-ledger seed ("observed") would read as a claim about a row nothing backs.
func worstOriginFor(recorded bool, attempts []projectstate.TaskAttempt) *string {
	if !recorded {
		return nil
	}
	o := string(projectstate.AttemptsWorstOrigin(attempts))
	return &o
}

// constructionRowsToContract maps the per-activity construction head-state map
// (honest-empty: nil in ⇒ nil out). activityMeta carries the Phase-2 activity-list
// metadata (worker class + coding + componentId) keyed by activity id, used to
// classify each activity's ActivityType (see projectstate.ClassifyType) — the N-* id
// namespace alone is too coarse — and to resolve its layer-stack projection.
// componentLayer is the id -> Method-layer-name lookup built once per call from the
// committed .systemDesign components (see componentLayerByID); empty when no system
// design is committed.
//
// The committed activity list is authoritative for WHAT EXISTS (construction UI spec,
// "slot 9 is authoritative for what exists"): every listed activity is emitted, and a
// listed activity with no stored head-state yet is emitted as a PLANNED-NO-RECORD row —
// "not started / no record", never Done. It goes through exactly the same resolution
// as a stored row, from the zero-value head-state: the server classifies it (so its
// profile's phases and tasks can be drawn), and because it has neither stored phases
// nor a ledger it resolves to no completions, so HasBuildEvidence is false and Phases
// and Attempts are empty.
//
// That row is NOT indistinguishable from a stored one, and the wire must not pretend it
// is. BuildStatus and Phase are required, non-omitempty enums, so it still carries
// their zero values (BuildInConstruction / phase 0) — values the contract marks
// meaningless while HasBuildEvidence is false, and which an MCP reader with no such
// gate would otherwise report as six activities "in construction". So the row says
// what it is instead: Recorded is false, and WorstOrigin (whose empty-ledger seed is
// "observed") is omitted. Without this, an activity nobody has touched yet was absent
// from the view altogether rather than shown as not started.
// A stored row the list no longer names is still emitted (it classifies as whatever
// its metadata allows, which for an unlisted id is nothing).
func constructionRowsToContract(
	rows map[string]projectstate.ActivityExecution,
	activityMeta map[string]projectstate.ActivityItem,
	componentLayer map[string]string,
	plan constructionPlan,
) map[string]ActivityConstructionStatus {
	if len(rows) == 0 && len(activityMeta) == 0 {
		return nil
	}
	all := make(map[string]projectstate.ActivityExecution, len(rows)+len(activityMeta))
	for id := range activityMeta {
		all[id] = projectstate.ActivityExecution{ActivityID: id}
	}
	maps.Copy(all, rows)
	out := make(map[string]ActivityConstructionStatus, len(all))
	for id, r := range all {
		// Recorded is the one explicit wire signal that a stored head-state row backs
		// this row. Everything else a planned-no-record row carries is either
		// honestly empty (no phases, no attempts) or a zero value the contract marks
		// meaningless; Recorded lets a reader tell the two populations apart without
		// re-deriving it from those zeros.
		_, recorded := rows[id]
		meta := activityMeta[id]
		typ, typVariant, resolved, classified := classifiedRowView(r, activityMeta[id])
		// Type/Kind/Variant/Phases/BuildStatus/Phase form a DISCRIMINATED UNION with
		// Classified: an activity the classifier refused to type asserts nothing about
		// what it is, how far its lifecycle has run, or what its coarse status is.
		// ClassifyType's failure return is ActivityTypeService — which is also the
		// generated enum's zero — so passing typ through unconditionally rendered the
		// unclassifiable rows (about 60 under the legacy activity list D9 deleted) as
		// Service builds carrying a Service
		// lifecycle skeleton. That is a different lie, not the honest blank the design
		// requires ("the caller MUST render the row as Unclassified with NO lifecycle
		// sub-rows at all" — and, per the same reasoning, no coarse status chip
		// either: a row with no sub-rows to justify it must not assert "Integrated").
		// The generated enums are closed unions with no Unclassified member, so honesty
		// is expressed by OMISSION rather than by a sentinel: Classified is the tag,
		// and Type/Kind/Variant/Phase/BuildStatus are left at their zero values and
		// MUST NOT be read while it is false; Phases — the half of the claim the view
		// actually renders sub-rows for — is empty rather than a seeded skeleton.
		var (
			wireType    ActivityType
			variant     TestingVariant
			phases      []PhaseCompletion
			coarsePhase ActivityConstructionPhase
			buildStatus ActivityBuildStatus
		)
		if classified {
			wireType = ActivityType(int(typ))
			if typ == projectstate.ActivityTypeTesting {
				variant = TestingVariant(int(typVariant))
			}
			// The row's coarse BuildStatus/Phase must be derived from the SAME phase
			// completions phasesToContract actually emits below — never from the raw
			// stored r.Phases directly. projectstate.ResolveConstructionRow (through
			// classifiedRowView) applies the identical profile-wins, ledger-preferred-over-stored
			// resolution once; both the emitted Phases and the coarse derivation read
			// off its result, so neither a partial attempt ledger nor a stored slice
			// that contradicts the profile can make the coarse chip disagree with the
			// very phase ticks rendered beneath it.
			phases = phasesToContract(resolved)
			coarsePhase = ActivityConstructionPhase(int(projectstate.CoarsePhaseFor(r, resolved)))
			buildStatus = ActivityBuildStatus(int(projectstate.CoarseBuildStatusFor(r, resolved)))
		}
		layer, band := projectstate.LayerForActivity(componentLayer[meta.ComponentID])
		out[id] = ActivityConstructionStatus{
			ActivityID:    r.ActivityID,
			Type:          wireType,
			Kind:          wireType,
			Variant:       variant,
			Phase:         coarsePhase,
			Phases:        phases,
			CurrentPhase:  ActivityMethodPhase(string(projectstate.CurrentLifecyclePhase(resolved))),
			StartedAt:     r.StartedAt,
			CompletedAt:   r.CompletedAt,
			BuildStatus:   buildStatus,
			Produced:      producedToContract(r.Produced),
			FailureReason: FailureReason(int(r.FailureReason)),
			FailureDetail: r.FailureDetail,
			Attempts:      attemptsToContract(r.Attempts),
			Classified:    classified,
			// The SECOND half of the discriminated union, and deliberately not the
			// same bit as Classified: a row can be perfectly well classified and
			// still have NO record that any work happened on it. Every
			// planned-no-record row is exactly that (six of the committed 29 today) —
			// neither stored phases nor an attempt ledger — so
			// projectstate.ResolvePhaseCompletions returns nil for them and
			// CoarseBuildStatusFor falls through to its zero value,
			// BuildInConstruction. That is a named, non-omitempty member the SPA
			// rendered as a confident "In construction" chip over work that had not
			// begun, and it also short-circuited the SPA's network-derived
			// eligible/blocked readiness for those rows.
			//
			// Derived from the SAME `resolved` slice the coarse status and the
			// emitted Phases read off — never recomputed from r.Phases/r.Attempts —
			// so the flag cannot drift from the claim it gates. len(resolved) > 0
			// means the profile materialized, which happens exactly when the row had
			// stored phases or a ledger. An unclassified row resolves to nil too, so
			// it reports no evidence as well; the consumer distinguishes the two
			// cases by Classified.
			HasBuildEvidence: len(resolved) > 0,
			Recorded:         recorded,
			// OMITTED on an unrecorded row, and MEANINGFUL ONLY ALONGSIDE A NON-EMPTY
			// Attempts ledger on a recorded one (the contract's own field descriptions
			// say both, and they reach the MCP output schema). Over an empty ledger the
			// value is the aggregate seed "observed", which read on its own says
			// "recorded" about a row where nothing was recorded; a planned-no-record
			// row has no ledger by construction, so it carries no origin at all.
			WorstOrigin:   worstOriginFor(recorded, r.Attempts),
			Layer:         layer,
			LayerBand:     band,
			PendingResume: pendingResumeFor(id, r, meta, resolved, rows, activityMeta, plan),
			OperatorNotes: operatorNotesToContract(r.OperatorNotes),
		}
	}
	return out
}

// operatorNotesToContract maps a row's stored operator notes onto the wire as they are:
// recorded order, and the delivery stamp only where the store holds one. Nothing is
// derived.
func operatorNotesToContract(notes []projectstate.OperatorNote) []OperatorNote {
	if len(notes) == 0 {
		return nil
	}
	out := make([]OperatorNote, 0, len(notes))
	for _, n := range notes {
		v := OperatorNote{
			NoteID:      n.NoteID,
			Kind:        OperatorNoteKind(int(n.Kind)),
			Text:        n.Text,
			RecordedAt:  n.RecordedAt,
			DeliveredAt: n.DeliveredAt,
		}
		if n.Gate != "" {
			g := n.Gate
			v.Gate = &g
		}
		if n.DeliveredToAttemptID != "" {
			a := n.DeliveredToAttemptID
			v.DeliveredToAttemptID = &a
		}
		for _, c := range n.Comments {
			v.Comments = append(v.Comments, NoteComment{JSONPath: c.JSONPath, Text: c.Text})
		}
		out = append(out, v)
	}
	return out
}

// constructionPlan is the committed network's dependency edges and milestones, indexed
// once per read, for pendingResumeFor. The zero value (no network) has no edges.
type constructionPlan struct {
	depsByActivity map[string][]string
	milestones     map[string]projectstate.NetworkMilestone
}

// constructionPlanFor indexes the network the project holds, read the way
// activityMetaByID reads the activity list: whatever model the slot carries.
func constructionPlanFor(p projectstate.Project) constructionPlan {
	network, ok := p.Network.Model.(*projectstate.Network)
	if !ok || network == nil {
		return constructionPlan{}
	}
	deps := make(map[string][]string, len(network.Dependencies))
	for _, d := range network.Dependencies {
		deps[d.Activity] = d.DependsOn
	}
	return constructionPlan{depsByActivity: deps, milestones: projectstate.MilestonesByID(network)}
}

// The wire reasons a PendingDependency carries (the contract's PendingDependency.reason).
const (
	pendingReasonNotBuilt            = "notBuilt"
	pendingReasonBuiltNotIntegrated  = "builtNotIntegrated"
	pendingReasonMilestoneNotReached = "milestoneNotReached"
	pendingReasonUnresolved          = "unresolved"
)

// isPendingResume reports whether a row is integration-pending (architect (D), D.3): no
// pump wrote it (projectstate.PumpWroteRow), yet its effective state is Running — which,
// for a row no pump wrote, means its attempt ledger holds some phases complete and not
// others. Nothing runs it and nothing reviews it, so it is not in flight.
func isPendingResume(r projectstate.ActivityExecution, meta projectstate.ActivityItem) bool {
	if projectstate.PumpWroteRow(r) {
		return false
	}
	effective, _ := projectstate.EffectiveConstructionPhase(r, meta)
	return effective == projectstate.ActivityConstructionRunning
}

// pendingResumeFor is the wire pendingResume for one row, or nil when the row is not
// integration-pending. fromPhase is the first profile phase its resolved phase set does
// not hold complete (resolved is the row's profile-ordered ResolveConstructionRow set);
// waitsOn is every direct network dependency the pump's own rule
// (projectstate.ResolveDependencySatisfied) does not find satisfied, in authored order,
// and is empty, never nil, when the row is next in line.
func pendingResumeFor(
	id string,
	r projectstate.ActivityExecution,
	meta projectstate.ActivityItem,
	resolved []projectstate.PhaseCompletion,
	rows map[string]projectstate.ActivityExecution,
	activityMeta map[string]projectstate.ActivityItem,
	plan constructionPlan,
) *PendingResume {
	if !isPendingResume(r, meta) {
		return nil
	}
	from := projectstate.CurrentLifecyclePhase(resolved)
	if from == "" {
		return nil
	}
	waitsOn := []PendingDependency{}
	for _, dep := range plan.depsByActivity[id] {
		res := projectstate.ResolveDependencySatisfied(dep, activityMeta, rows, plan.milestones, map[string]bool{})
		if res.Satisfied && res.ProblemReason == "" {
			continue
		}
		waitsOn = append(waitsOn, PendingDependency{Id: dep, Reason: pendingReasonFor(dep, res, rows, activityMeta, plan)})
	}
	return &PendingResume{FromPhase: ActivityMethodPhase(string(from)), WaitsOn: waitsOn}
}

// pendingReasonFor names why one unsatisfied dependency is unsatisfied, in the order
// projectstate.ResolveDependencySatisfied itself tries: a plan defect, a milestone, then
// an activity — which is "built but not integrated" when it is itself integration-pending
// (the backfill's own wording), and "not built" otherwise.
func pendingReasonFor(
	dep string,
	res projectstate.DependencyResolution,
	rows map[string]projectstate.ActivityExecution,
	activityMeta map[string]projectstate.ActivityItem,
	plan constructionPlan,
) string {
	if res.ProblemReason != "" {
		return pendingReasonUnresolved
	}
	if _, isMilestone := plan.milestones[dep]; isMilestone {
		return pendingReasonMilestoneNotReached
	}
	if r, exists := rows[dep]; exists && isPendingResume(r, activityMeta[dep]) {
		return pendingReasonBuiltNotIntegrated
	}
	return pendingReasonNotBuilt
}

// constructionStartedFor answers the Begin-versus-Resume question — has construction
// started for this project? — from the STORED head-state, once, on the server.
//
// True iff some stored row carries state only the construction pump writes (a start
// time, a coarse phase past NotStarted, a phase set, a recorded failure) or an attempt
// the running system OBSERVED. A reconstructed attempt never counts, whatever its
// outcome: the backfill wrote 214 backfilled attempts onto 23 activities no pump ever
// ran, and counting them read "Resume construction" on a project whose pump had never
// started. A planned-no-record row cannot count either — it is not a stored row.
//
// It replaces the SPA probing one construction-session endpoint per committed activity
// on every load (29 GETs), which also stopped answering once Temporal retention expired.
func constructionStartedFor(rows map[string]projectstate.ActivityExecution) bool {
	for _, r := range rows {
		if rowCarriesPumpState(r) || hasObservedAttempt(r.Attempts) {
			return true
		}
	}
	return false
}

// rowCarriesPumpState reports whether a stored row holds any head fact only the pump
// writes: the start stamp, the exit stamp, or a recorded failure. The coarse roll-up and
// the phase set it also used to name are DERIVED now (spec §5.3), and a derivation is not
// evidence that anything ran.
func rowCarriesPumpState(r projectstate.ActivityExecution) bool {
	return r.StartedAt != nil ||
		r.CompletedAt != nil ||
		r.FailureReason != projectstate.FailureReasonUnknown ||
		r.FailureDetail != ""
}

// hasObservedAttempt reports whether the ledger holds an attempt of origin observed.
func hasObservedAttempt(attempts []projectstate.TaskAttempt) bool {
	for _, a := range attempts {
		if a.Provenance.Origin == projectstate.OriginObserved {
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

// componentLayerByID builds the id → Method-layer-name lookup from the committed
// .systemDesign components (empty map when no system design is committed, exactly as
// activityMetaByID tolerates a missing activity list). It feeds LayerForActivity in
// constructionRowsToContract, keyed by each row's componentId — never by its activity id.
func componentLayerByID(p projectstate.Project) map[string]string {
	out := map[string]string{}
	if sys, ok := p.SystemDesign.Model.(*projectstate.System); ok && sys != nil {
		for _, c := range sys.Components {
			out[c.ID] = c.Layer.String()
		}
	}
	return out
}

// classifiedRowView resolves everything constructionRowsToContract and computeEVAtRead
// must agree about for ONE stored construction row: its classified type/variant and the
// reconciled phase set both of them derive from. Sharing it is the point — the EV curve
// used to read r.Phases raw and with no classified check, so an unclassified row could
// contribute to the curve while the row beside it refused to assert a status at all.
//
// The resolution itself lives in projectstate (ResolveConstructionRow), so the
// construction pump reads a row exactly as this view renders it. This is a pure call.
func classifiedRowView(
	r projectstate.ActivityExecution,
	meta projectstate.ActivityItem,
) (typ projectstate.ActivityType, variant projectstate.TestingVariant, resolved []projectstate.PhaseCompletion, classified bool) {
	return projectstate.ResolveConstructionRow(r, meta)
}

// resolvedPhaseCompletions is this package's name for projectstate.ResolvePhaseCompletions,
// the profile-wins, ledger-per-phase resolution that classifiedRowView's phase set comes
// from. The rule moved down into projectstate so the construction pump can share it; this
// name stays because the view-model's tests pin the rule against it directly, with an
// explicit profile, and must keep passing unmodified across the move.
func resolvedPhaseCompletions(
	profile projectstate.Profile,
	attempts []projectstate.TaskAttempt,
) []projectstate.PhaseCompletion {
	return projectstate.ResolvePhaseCompletions(profile, attempts)
}

// phasesToContract maps the App-A internal phase-completion records onto the wire.
// The caller (constructionRowsToContract) passes the ALREADY-RESOLVED phase set from
// projectstate.ResolveConstructionRow (through classifiedRowView) — this function
// performs no further derivation, so it
// cannot drift from the coarse BuildStatus/Phase computed alongside it.
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
			Label:       ph.Label,
		})
	}
	return out
}

// attemptsToContract maps the append-only Figure A-1 task ledger onto the wire.
// Provenance is copied VERBATIM and never defaulted — the zero origin means
// "synthesized" and must survive the boundary as such; a mapper that filled in a
// missing origin would launder a fabricated row into an observed one.
func attemptsToContract(attempts []projectstate.TaskAttempt) []TaskAttempt {
	if len(attempts) == 0 {
		return nil
	}
	out := make([]TaskAttempt, 0, len(attempts))
	for _, a := range attempts {
		out = append(out, TaskAttempt{
			AttemptId: a.AttemptID,
			Task:      string(a.Task),
			Phase:     ActivityMethodPhase(string(a.Phase)),
			Attempt:   int64(a.Attempt),
			Actor:     strPtrOrNil(string(a.Actor)),
			StartedAt: a.StartedAt,
			EndedAt:   a.EndedAt,
			Outcome:   string(a.Outcome),
			Evidence:  EvidenceRef{Kind: string(a.Evidence.Kind), Ref: a.Evidence.Ref},
			Provenance: AttemptProvenance{
				Origin:      string(a.Provenance.Origin),
				Generator:   strPtrOrNil(a.Provenance.Generator),
				GeneratedAt: a.Provenance.GeneratedAt,
				Basis:       strPtrOrNil(a.Provenance.Basis),
			},
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

	// The integrated set is read through the SAME derivation the rest of the read path
	// uses (classifiedRowView + CoarseBuildStatusFor), not off the raw stored
	// BuildStatus and not off the raw stored r.Phases. Both paths used to read stored
	// and therefore agreed; once constructionRowsToContract began deriving, a raw read
	// here let the EV/SPI curve contradict the build status rendered beside it on the
	// same screen — and, with no `classified` check, let a row the classifier refused
	// to type contribute to the curve while the row beside it refused to assert a
	// status at all.
	activityMeta := activityMetaByID(p)
	integrated := make([]string, 0, len(p.ActivityExecution))
	for id, r := range p.ActivityExecution {
		_, _, resolved, classified := classifiedRowView(r, activityMeta[id])
		if !classified {
			continue
		}
		if projectstate.CoarseBuildStatusFor(r, resolved) == projectstate.BuildIntegrated {
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// pipelineDefaultToolchain names the placeholder toolchain stamped on the design
// dispatch's logical step graph; the real design recipe lives in the user's
// aiarch-design.yml workflow file, so the step is only present to satisfy the RA's
// non-empty-step-graph pre-condition.
const pipelineDefaultToolchain = "go-1.23"

// ===========================================================================
// Workflow-side pipeline helpers. The temporalgen migration routes the submit/observe
// design-job pair through the GENERATED agenticJobAccess invokers (wf.Acts.
// PipelineSubmit/ObserveAgenticJob); the value mapping that lived on the folded
// pipelineDispatchAdapter — the RepoRef→RepoTarget decode, the PipelineSpec composition,
// and the RA-phase→neutral-phase mapping — is now these PURE workflow-side helpers
// (mirrors construction's dispatch.go). The idempotency key is stamped INSIDE the
// generated submit Activity (genActivityIdempotencyKey, the same run-scoped 3-part scheme
// the old hand-derived key used), so the redraft-vs-auto-retry distinction is unchanged.
// The former EXPORTED consumer-mirror interface + the folded pipelineDispatchAdapter +
// the neutral pipelineSpec/pipelineHandle carriers are RETIRED.
// ===========================================================================

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// designRepoTarget decodes an opaque per-project RepoRef String() into the RA's
// infrastructure-neutral RepoTarget{Owner, Name} for the per-project-design-dispatch.
// An empty repoRef is the dormant-rail case → a zero RepoTarget (the RA falls back to
// the configured construction repo). A malformed ref surfaces as the RA's
// ContractMisuse (the dispatch Activity maps it to a terminal error). It uses
// sourcecontrol's own OwnerRepo accessor so the RepoRef encoding stays owned by
// sourceControlAccess (no encoding leak here).
//
// NOT promotable to projectstate (code-health-phase-bd task D3 verification): it needs
// agenticjob.RepoTarget + sourcecontrol.RepoRefOwnerRepo/RepoRefFromString —
// both sibling ResourceAccess packages, and TestMethodLayering forbids RA→RA sideways
// imports (the RA-layer analog of "no Manager→Manager sideways"). Stays duplicated
// per-manager alongside designBranch's twin.
func designRepoTarget(repoRef string) (agenticjob.RepoTarget, error) {
	if repoRef == "" {
		return agenticjob.RepoTarget{}, nil
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(sourcecontrol.RepoRefFromString(repoRef))
	if err != nil {
		return agenticjob.RepoTarget{}, err
	}
	return agenticjob.RepoTarget{Owner: owner, Name: name}, nil
}

// ===========================================================================
// Dispatch inputs (C-WF-DESIGN workflow_dispatch schema). These exact key names
// are the binding contract with aiarch-design.yml's workflow_dispatch.inputs.
// idempotency_token is RA-controlled and is NOT set here.
// ===========================================================================

const (
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	dispatchInputArtifactKind = "artifact_kind"
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	// dispatchInputCommand carries the .claude command slug the seated design job runs
	// (DesignCommandFor). It REPLACES the retired design_prompt input: the Method doctrine
	// that used to be composed into a prompt now lives in the command's method-assets, so
	// the Manager ships only the command NAME, not prose.
	dispatchInputCommand = "command"
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	dispatchInputTargetBranch = "target_branch"
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// dispatchActivityOptions is the option preset for the generated
// agenticJobAccess.submitAgenticJob Activity (consumed by the manager's
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// appendEpisodeRetryWindow is the HARD wall-clock bound on the episode-append's own retry
// envelope. Attempts are UNCAPPED inside it (bookkeeping must not lose to a transient
// store fault) but they cannot run forever, because the workflow WAITS on this activity.
//
// bounded-latency ruling 2026-08-02: local sidecar append failing >2m is not transient;
// business outcome must not stall on telemetry.
const appendEpisodeRetryWindow = 2 * time.Minute

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// appendEpisodeActivityOptions is the episode-append preset — DELIBERATELY its own
// envelope, independent of every business preset (§capture-seam): a generous per-attempt
// timeout, UNCAPPED attempts inside appendEpisodeRetryWindow (MaxAttempts unset ⇒
// Temporal treats it as unlimited), and ContractMisuse terminal (a malformed record will
// never become well-formed by retrying — the caller logs it instead).
func appendEpisodeActivityOptions() workflow.ActivityOptions {
	o := fwmanager.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
	o.ScheduleToCloseTimeout = appendEpisodeRetryWindow
	return o
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// observeActivityOptions is the option preset for the generated
// agenticJobAccess.observeAgenticJob Activity. Transient reads retry;
// a NotFound (GC'd handle) is non-retryable and surfaces.
func observeActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// designWorkflowFileName is the per-project DESIGN workflow file the agentic design
// dispatch must target (per-project-design-dispatch) — the BASENAME of
// sourcecontrol.DesignWorkflowPath (".github/workflows/aiarch-design.yml"), i.e.
// "aiarch-design.yml". Derived from the RA's single source of truth so the dispatch
// target and the project-birth workflow-file seat can never drift. This is the
// workflow file the design dispatch selects in place of the construction default
// (aiarch-construct.yml).
var designWorkflowFileName = path.Base(sourcecontrol.DesignWorkflowPath)

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// reviewAuthorRole is the role stamped on every comment the architect files at the
// System-Design review gate. The ledger records WHO filed each comment; in the design
// phase the reviewer at the gate is always the architect.
const reviewAuthorRole = "architect"

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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
		Replies:    toViewReplies(c.Replies),
		Reopened:   c.Reopened,
		Type:       c.Type,
		Addressee:  c.Addressee,
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// toViewReplies projects a stored entry's utterance history onto the wire shape. The
// DEPRECATED scalar ReviewComment.Response is deliberately NOT read here: the shared
// decode point already migrates a legacy response into a synthesized first reply
// (migrateLegacyReviewThread, design §3.5), so reading it again would double-render it.
func toViewReplies(in []projectstate.ReviewCommentReply) []ReviewCommentReply {
	if len(in) == 0 {
		return nil
	}
	out := make([]ReviewCommentReply, 0, len(in))
	for _, r := range in {
		out = append(out, ReviewCommentReply{ID: r.ID, AuthorRole: r.AuthorRole, Text: r.Text, At: r.At})
	}
	return out
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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
	// Stage 4a: ONE copy now serves the systemDesign+construction rails (byte-identical
	// twins, collapsed by the package merge).
	// querySessionState returns a SessionStateView; backs getSessionState.
	querySessionState = "sessionState"
	// signalSetCommentStatus resumes a CoAuthorArtifactWorkflow suspended at the
	// AwaitingReview gate to apply a durable review-ledger status transition
	// (open|answered->resolved / resolved->open) to one comment on the session branch; backs
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
// claude-code-action DESIGN job via Pipeline (agenticJobAccess), OBSERVES
// it to a typed terminal phase, and READS BACK the typed model the Action committed
// via ProjectState.ReadProject. aiarch makes NO synchronous LLM call and writes NO
// draft JSON on the main path (the Action commits it inside the user's CI; the
// required CI validation check is the trust boundary).
//
//   - Pipeline (agenticJobAccess) — submit + observe, both Activity-
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
	// readProjectVersion / advancePhase, the agenticJobAccess submit/observe
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// raConflictErrType is the canonical Temporal Type() a head-state mutation
// Activity surfaces when the optimistic-concurrency token (expectedVersion) is
// stale. The workflow recovers with the bounded re-read→re-apply loop.
var raConflictErrType = fwmanager.RAErrType(fwra.Conflict)

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// raNotFoundErrType is the canonical Temporal Type() the ReadProject Activity
// surfaces when the addressed aggregate has NO row yet — a brand-new project.
var raNotFoundErrType = fwmanager.RAErrType(fwra.NotFound)

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
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

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// coAuthorOutcome is the child gate's terminal report to the parent — whether the
// step's human gate approved (advance) or withdrew (halt).
type coAuthorOutcome int

const (
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	coAuthorUnknown coAuthorOutcome = iota
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	coAuthorApproved
	// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
	// twins, collapsed by the package merge).
	coAuthorWithdrawn
)

// phaseAdvanceWorkflowID derives the continuity token for the short-lived gating
// workflow: {projectId}:phaseAdvance:systemDesign (systemDesignManager.md §6.1).
//
// The rail suffix is NOT decoration. projectDesignManager derived the same
// {projectId}:phaseAdvance string; the two never collided only because they polled
// different task queues and never ran at once. Stage 4a puts both on the single
// `delivery` queue, where USE_EXISTING would silently join the OTHER rail's advance.
// Existing in-flight advances keep the old id — they are covered by the stage-4 drain,
// and a phase advance is seconds long.
func phaseAdvanceWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:phaseAdvance:systemDesign", projectID)
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
		"agenticJobAccess.submitAgenticJob":                      dispatchActivityOptions(),
		"agenticJobAccess.observeAgenticJob":                     observeActivityOptions(),
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
		// The ROUND-ledger dual-write (stage 3 task 6). Every one is a head-state mutation
		// through the same applyMutation funnel the designSession verbs ride, so it takes
		// the same envelope: the workflow's own Conflict re-read loop (applyRecovering) is
		// what resolves a CAS loss, not a longer retry here.
		"activityExecutionAccess.openActivity":           mutateActivityOptions(),
		"activityExecutionAccess.openReviewRound":        mutateActivityOptions(),
		"activityExecutionAccess.appendReviewVerdict":    mutateActivityOptions(),
		"activityExecutionAccess.decideReviewRound":      mutateActivityOptions(),
		"activityExecutionAccess.setReviewCommentStatus": mutateActivityOptions(),
		// SP1 capture-seam: the episode ledger append rides its OWN envelope, never a
		// business one (see appendEpisodeActivityOptions).
		"episodeAccess.appendEpisode": appendEpisodeActivityOptions(),
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
			ProjectState:      m.projectState,
			Pipeline:          m.pipeline,
			Rail:              m.rail,
			DesignSession:     m.designSession,
			ActivityExecution: m.activityExecution,
			Episodes:          m.episodes,
		},
	}
}

// ---------------------------------------------------------------------------
// Episode facet read ops (SP1 capture-seam, Task 9 — founder ruling 2026-08-02:
// episode observability is a facet of the existing use cases, not a new
// episodeManager). Both ops are PLAIN METHODS that consult episodeAccess directly
// — no Temporal — the same shape as ListProjects/GetProject above. The whole-
// project exportEpisodes op is cut from v1 (per-target export is client-side,
// Task 10).
// ---------------------------------------------------------------------------

// ListEpisodesForArtifact returns every episode record (design/review/rework runs,
// or gaps) captured against one System-Design artifact, in episodeAccess's own
// (append) order. A pass-through over episodeAccess.ListEpisodes scoped by
// TargetRef=artifactKind, mapped to the contract EpisodeRecordView.
func (m *systemDesignManager) ListEpisodesForArtifact(rc fwmanager.Context, projectID ProjectID, artifactKind ArtifactKind) ([]EpisodeRecordView, error) {
	ctx := rc.Context
	if projectID == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	// TargetRef on the ledger is the PascalCase artifactKindString(kind) form —
	// exactly what the capture-seam write path (episodeRecordFromSummary via
	// dispatchAndObserve) stamps as TargetRef, NOT the wire-name form. artifactKind
	// is typed as the contract's own ArtifactKind enum (not a bare string) so this
	// conversion cannot be skipped by a caller that only has the wire name.
	targetRef := artifactKindString(artifactKind)
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{
		ProjectID: episode.ProjectID(projectID),
		TargetRef: &targetRef,
	})
	if err != nil {
		return nil, sdMapRAError(err, "episodeAccess.ListEpisodes")
	}
	return sdEpisodeRecordViews(records), nil
}

// GetEpisodeTimeline returns one episode's full timeline: its ledger record plus
// the sequenced trace events mined from its run. NotFound if episodeID does not
// name a record on this project.
func (m *systemDesignManager) GetEpisodeTimeline(rc fwmanager.Context, projectID ProjectID, episodeID string) (EpisodeTimeline, error) {
	ctx := rc.Context
	if projectID == "" {
		return EpisodeTimeline{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if episodeID == "" {
		return EpisodeTimeline{}, newError(fwmanager.ContractMisuse, "empty episodeId")
	}
	// ListEpisodes has no by-id lookup (episodeAccess.md — the ledger is append-
	// scanned by TargetRef); querying with no TargetRef and finding the one record
	// whose EpisodeID matches is the only way to resolve one episode across every
	// target on the project.
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{ProjectID: episode.ProjectID(projectID)})
	if err != nil {
		return EpisodeTimeline{}, sdMapRAError(err, "episodeAccess.ListEpisodes")
	}
	rec, ok := findEpisodeRecord(records, episodeID)
	if !ok {
		return EpisodeTimeline{}, newError(fwmanager.NotFound, fmt.Sprintf("episode %q not found", episodeID))
	}
	// A GAP record (episode.EpisodeGap — the dispatch that produced no summary at
	// all) has no trace file: TracePath is nil on the ledger record. The
	// never-silent gap doctrine (Task 2/7) treats a gap as a PRESENT, first-class
	// outcome, not an absence — the record itself must always resolve; only its
	// timeline is empty. Skip the RA round-trip entirely when TracePath says
	// there is nothing to read, and treat a NotFound FROM ReadTraceEvents (e.g. a
	// TracePath that no longer resolves) the same way, rather than erroring the
	// whole timeline — either would otherwise be indistinguishable from an
	// unknown episodeID.
	if rec.TracePath == nil || *rec.TracePath == "" {
		return EpisodeTimeline{Record: sdEpisodeRecordToView(rec), Events: episodeTimelineEvents(nil)}, nil
	}
	raw, err := m.episodes.ReadTraceEvents(fwra.Context{Context: ctx}, episode.ProjectID(projectID), episodeID)
	if err != nil {
		if isEpisodeTraceNotFound(err) {
			return EpisodeTimeline{Record: sdEpisodeRecordToView(rec), Events: episodeTimelineEvents(nil)}, nil
		}
		return EpisodeTimeline{}, sdMapRAError(err, "episodeAccess.ReadTraceEvents")
	}
	return EpisodeTimeline{
		Record: sdEpisodeRecordToView(rec),
		Events: episodeTimelineEvents(raw),
	}, nil
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// isEpisodeTraceNotFound reports whether err is episodeAccess.ReadTraceEvents's
// fwra.NotFound ("no trace file for episode ...") — the signal that a record
// with a stamped TracePath still has nothing to read (a gap's trace was never
// written, or a local trace file was pruned). Distinct from sdMapRAError's general
// NotFound handling: here it is NOT an error at all, it means "empty timeline".
func isEpisodeTraceNotFound(err error) bool {
	var raErr *fwra.Error
	return errors.As(err, &raErr) && raErr.Kind == fwra.NotFound
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// findEpisodeRecord returns the record whose EpisodeID matches id, if any.
func findEpisodeRecord(records []episode.EpisodeRecord, id string) (episode.EpisodeRecord, bool) {
	for _, r := range records {
		if r.EpisodeID == id {
			return r, true
		}
	}
	return episode.EpisodeRecord{}, false
}

// sdEpisodeRecordViews maps a slice of ledger records onto the contract view type.
func sdEpisodeRecordViews(records []episode.EpisodeRecord) []EpisodeRecordView {
	out := make([]EpisodeRecordView, 0, len(records))
	for _, r := range records {
		out = append(out, sdEpisodeRecordToView(r))
	}
	return out
}

// sdEpisodeRecordToView maps one episodeAccess ledger record onto this contract's
// OWN copy of the view shape (EpisodeRecordView mirrors episodeAccess.EpisodeRecord
// field-for-field; contracts are self-contained, so this is an intentional
// duplicate of the mapping episodeAccess itself owns, not a shared function).
func sdEpisodeRecordToView(r episode.EpisodeRecord) EpisodeRecordView {
	v := EpisodeRecordView{
		EpisodeID:      r.EpisodeID,
		Kind:           episodeViewKind(r.Kind),
		TargetRef:      r.TargetRef,
		WorkerClass:    r.WorkerClass,
		Model:          r.Model,
		Usage:          EpisodeUsage(r.Usage),
		CostUSD:        r.CostUSD,
		NumTurns:       r.NumTurns,
		ToolCallCounts: r.ToolCallCounts,
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		Outcome:        sdEpisodeViewOutcome(r.Outcome),
		GapReason:      r.GapReason,
		TracePath:      r.TracePath,
	}
	if r.Lineage != nil {
		l := EpisodeLineage(*r.Lineage)
		v.Lineage = &l
	}
	if r.StreamedUsage != nil {
		u := EpisodeUsage(*r.StreamedUsage)
		v.StreamedUsage = &u
	}
	if len(r.SubagentSpans) > 0 {
		spans := make([]SubagentSpan, 0, len(r.SubagentSpans))
		for _, s := range r.SubagentSpans {
			spans = append(spans, SubagentSpan(s))
		}
		v.SubagentSpans = spans
	}
	return v
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeViewKind maps the episodeAccess RA's Kind onto this contract's own copy
// of the enum. Written as a TOTAL switch rather than a numeric cast so a future
// divergence between the two independently-versioned contracts is a compile-time
// conversation, not silent drift (mirrors episodeOutcomeFrom above).
func episodeViewKind(k episode.EpisodeKind) EpisodeKind {
	switch k {
	case episode.EpisodeKindDesign:
		return EpisodeKindDesign
	case episode.EpisodeKindConstruction:
		return EpisodeKindConstruction
	case episode.EpisodeKindReview:
		return EpisodeKindReview
	case episode.EpisodeKindRework:
		return EpisodeKindRework
	case episode.EpisodeKindAnswer:
		return EpisodeKindAnswer
	default:
		// Unreachable for the five defined episode.EpisodeKind values above (the
		// exhaustive linter enforces that every real variant has its own case);
		// kept as a defensive fallback for an out-of-range ordinal.
		return EpisodeKindDesign
	}
}

// sdEpisodeViewOutcome maps the episodeAccess RA's Outcome onto this contract's own
// copy of the enum. Same total-switch rationale as episodeViewKind.
func sdEpisodeViewOutcome(o episode.EpisodeOutcome) EpisodeOutcome {
	switch o {
	case episode.EpisodeSucceeded:
		return EpisodeSucceeded
	case episode.EpisodeFailed:
		return EpisodeFailed
	case episode.EpisodeCancelled:
		return EpisodeCancelled
	case episode.EpisodeGap:
		return EpisodeGap
	default:
		// Unreachable for the four defined episode.EpisodeOutcome values above;
		// defensive fallback for an out-of-range ordinal (mirrors episodeOutcomeFrom's
		// own default, which also lands on the "gap" reading — the safe direction).
		return EpisodeGap
	}
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeTimelineEvents stitches the raw trace lines mined from episodeAccess into
// sequenced TimelineEvents: seq is 1-based and positional (the ledger's own
// ordering — trace files are append-only), eventType is lifted from each line's
// top-level "type" field (the same field streamEvent.Type reads off the CLI's
// stream-json protocol), and raw is carried through verbatim for the UI.
func episodeTimelineEvents(raw []json.RawMessage) []TimelineEvent {
	events := make([]TimelineEvent, 0, len(raw))
	for i := range raw {
		events = append(events, TimelineEvent{
			Seq:       int64(i + 1),
			EventType: episodeTraceEventType(raw[i]),
			Raw:       &raw[i],
		})
	}
	return events
}

// Stage 4a: ONE copy now serves the systemDesign+projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeTraceEventType extracts the "type" field from one raw trace event line.
// A line that fails to decode, or decodes with no "type", maps to "unknown"
// rather than failing the whole timeline — a partially-corrupt trace still owes
// every OTHER event its identity.
func episodeTraceEventType(raw json.RawMessage) string {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Type == "" {
		return "unknown"
	}
	return probe.Type
}

// ---------------------------------------------------------------------------
// PROJECT-DESIGN RAIL — moved verbatim from internal/manager/projectdesign/
// projectdesignmanager.go at stage 4a. Bodies are unchanged; only package-private
// names that collided with another rail were renamed (the collision table is in
// docs/superpowers/plans/2026-09-25-activity-experience-stage4a.md, Task 6 Step 2b,
// and in this commit's message). 4b replaces this block with the generic DAG child.
// ---------------------------------------------------------------------------

// ProjectDesignManager is the generated service-contract interface for this component
// — the public use-case surface of the projectDesignManager façade
// (projectDesignManager.md §2). Each op leads with the Manager-layer call Context
// (fwmanager.Context, embedding context.Context + the Principal); the *projectDesignManager derives
// ctx := rc.Context inside. The concrete *projectDesignManager satisfies it; the consumer-side
// dependency seams (agenticJobAccess / sourceControlRail) + the Temporal
// pdWorkflows struct stay hand-written and are NOT part of this contract.

// projectDesignManager is the projectDesignManager façade. It exposes the public
// use-case ops (projectDesignManager.md §2) and OWNS Temporal. It is the Phase-2 twin
// of the systemdesign Manager. The Temporal-backed ops:
//   - RequestArtifactDraft   — Workflow (entry, per-artifact CoAuthorPhase2ArtifactWorkflow)
//   - RequestSDPCommit       — Workflow (entry, AssembleSDPReviewWorkflow)
//   - SubmitSDPDecision      — Signal (sdpDecision, to the SDP-review workflow)
//   - AdvanceToConstruction  — Workflow (entry, short-lived Phase-2 seal)
//   - GetSessionState        — Query (sessionState, read-only)
//
// plus SubmitReviewDecision — Signal (reviewDecision, the per-artifact OQ-3 gate).
//
// Each op leads with the Manager-layer call Context (fwmanager.Context, embedding
// context.Context + the Principal); the *projectDesignManager derives ctx :=
// rc.Context inside. Pre-condition checks the contract puts on the façade (Phase-2
// kind, non-empty projectId, Commit-requires-optionId, RejectAll-requires-feedback)
// are enforced here before any downstream call (§2, §3).
//
// The façade methods themselves use ONLY the Temporal client. It ALSO stores the
// Worker-side deps it was constructed with — the published
// projectstate.ProjectStateAccess (head-state read-back + thin writes), the published
// agenticjob.AgenticJobAccess (Phase-2 design-job dispatch), the
// published sourcecontrol.SourceControlAccess (the PR rail), the three estimation
// Engines (the in-workflow SDP-assembly join), and the per-project repo resolver — so
// RegisterWorker can wire them (via the package's folded adapters) into the
// hand-written Temporal pdWorkflows. The former exported consumer-mirror interfaces +
// the composition-root adapters are RETIRED; the manager now depends on the deps'
// PUBLISHED interfaces and adapts them internally (Option-B boundary mapping).
type projectDesignManager struct {
	client       client.Client
	projectState projectstate.ProjectStateAccess
	pipeline     agenticjob.AgenticJobAccess
	rail         sourcecontrol.SourceControlAccess
	estimator    estimation.EstimationEngine
	opEstimator  operationestimation.OperationEstimationEngine
	settlement   billing.BillingEngine

	// designSession (B6) is the generated designSessionAccess dep. Since B9, every
	// branch-scoped design flow EXCEPT StageArtifactForReview reaches it through the
	// generated invoker surface (invokers.gen.go/activities.gen.go, via wf.Acts) —
	// read-back, commit/reject/withdraw, and the review-ledger set/seed verbs. Stage
	// alone stays on the manager-local capability-fallback custom Activity
	// (activities_custom.go): the generated invoker's `model` parameter is the sealed
	// projectstate.ArtifactModel interface, which Temporal's default JSON DataConverter
	// cannot decode across the wire (verified; see activities_custom.go's file doc).
	designSession projectstate.DesignSessionAccess

	// activityExecution (stage 3, task 6) is the generated activityExecutionAccess dep —
	// the fifth facet of the one project-state component, owner of the per-activity review
	// ROUND ledger. The Phase-2 design rail dual-writes every review decision through it
	// beside the slot's ReviewThread: taking the dep HERE is what registers its Temporal
	// activities on this Manager's worker, which is the precondition for the CoAuthor
	// spine's wf.Acts.ActivityExecution* calls. Held only to thread into genActivities —
	// every call is a workflow-side Activity, never a manager-side one.
	activityExecution projectstate.ActivityExecutionAccess

	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// episodes (SP1 capture-seam) is the generated episodeAccess dep — the agentic-
	// episode ledger every terminal design dispatch appends to. The WORKFLOW paths reach
	// it through the generated invoker surface (wf.Acts.EpisodesAppendEpisode); this
	// field is held for two reasons: to thread it into genActivities, and because the
	// answer-job capture (pdAnswerEpisodeWatch) runs MANAGER-SIDE, outside any workflow,
	// and must call the RA directly.
	episodes episode.EpisodeAccess
}

// newProjectDesignManager is the hand-written, unexported builder the generated
// NewProjectDesignManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only client; projectState /
// pipeline / rail / the three estimators / repo are stored for RegisterWorker (rail
// may be nil — a dev server with no source-control credentials runs the design spine
// repo-less).
func newProjectDesignManager(
	c client.Client,
	projectState projectstate.ProjectStateAccess,
	pipeline agenticjob.AgenticJobAccess,
	rail sourcecontrol.SourceControlAccess,
	estimator estimation.EstimationEngine,
	opEstimator operationestimation.OperationEstimationEngine,
	settle billing.BillingEngine,
	designSession projectstate.DesignSessionAccess,
	activityExecution projectstate.ActivityExecutionAccess,
	episodes episode.EpisodeAccess,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
) *projectDesignManager {
	return &projectDesignManager{
		client:            c,
		projectState:      projectState,
		pipeline:          pipeline,
		rail:              rail,
		estimator:         estimator,
		opEstimator:       opEstimator,
		settlement:        settle,
		designSession:     designSession,
		activityExecution: activityExecution,
		episodes:          episodes,
		repo:              repo,
	}
}

// RequestArtifactDraft — op 2.1. Temporal Workflow (entry; StartWorkflow /
// signal-with-start), workflow id {projectId}:{artifactKind}. Idempotent on the id.
//
// Pre: projectID non-nil; kind is a Phase-2 kind AND != KindSdpReview (the SDP
// review is assembled via RequestSDPCommit, not co-authored). The spine-ordering gate
// (the requested kind's immediate Phase-2 predecessor must be Committed) is enforced
// here on head-state — the wire-side mirror of the SPA's Phase-2 buildSpine step lock —
// so a raw API/MCP caller cannot draft out of order (the CoAuthorPhase2ArtifactWorkflow
// itself never gated ordering; it drafts immediately). The first Phase-2 kind
// (planningAssumptions) has no Phase-2 predecessor.
// amendmentIndexFor PROMOTED to projectstate.AmendmentIndexFor (code-health-phase-bd task
// D3) — byte-identical pure resolver, no longer duplicated with systemdesign's twin. It
// returns the AMENDMENT index for a draft request against slot: the count of prior
// commits, used as the …-amend-N branch suffix and the "revision N" prompt framing, and
// the signal that gates the amendment path (fresh -amend-N branch, amendment prompt, and
// review-ledger SEED of the reopening feedback).

func (m *projectDesignManager) RequestArtifactDraft(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) {
		return "", newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	if kind == KindSdpReview {
		return "", newError(fwmanager.FailedPrecondition, "use requestSDPCommit for the SDP review")
	}
	// A redraft's feedback is OPTIONAL (nil = a fresh draft with no steer), but a
	// non-nil envelope whose notes are empty is a third state that steers nothing
	// while telling the agent it was steered. The sibling SubmitReviewDecision
	// rejects exactly this shape; RequestArtifactDraft must agree.
	if feedback != nil && strings.TrimSpace(feedback.Notes) == "" {
		return "", newError(fwmanager.ContractMisuse, "feedback is present but its notes are empty — omit feedback entirely to request a fresh draft with no steer")
	}
	// RULING P13: refuse a queued reply rather than let the amendment seed re-file it as a
	// new round-0 thread. See pdCheckNoReplyTo.
	if feedback != nil {
		if perr := pdCheckNoReplyTo(feedback.Comments); perr != nil {
			return "", perr
		}
	}

	// Spine-ordering gate (Phase-2 twin of the systemdesign Manager). A Phase-2 kind
	// may only be drafted once its immediate predecessor in the Phase-2 sequence is
	// Committed (the same order the SPA's PHASE2_ORDER locks by).
	if err := m.checkPhase2Predecessor(ctx, projectID, kind); err != nil {
		return "", err
	}

	// F-R2 (Phase-2 port): the generating guard + WEDGED-run supersede. Refuse the request
	// while the live session is Drafting/Redrafting (a buffered redraft signal would later
	// stale-consume a recovery gate), and TERMINATE a wedged RUNNING run so the SignalWithStart
	// below starts a fresh run instead of binding the signal to a corpse. Ports the 2026-07-16
	// systemdesign fixes that were never mirrored here.
	if err := m.prepareForDraftRequest(rc, projectID, kind); err != nil {
		return "", err
	}

	// F38 BACK-EDGE / AMENDMENT (Phase-2 twin). A draft request on an already-COMMITTED
	// Phase-2 artifact is the legal amendment path: fresh session on a …-amend-N branch
	// (N = the slot's prior commit count) with the reopening feedback seeded into its ledger.
	// A non-committed slot keeps today's behavior (active session redraft / fresh draft).
	amendment := 0
	if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
		amendment = projectstate.AmendmentIndexFor(pdSlotFor(proj, toPSKind(kind)))
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		// F-R2 (Phase-2 port): a session whose previous run CLOSED (committed/withdrawn →
		// amendment/fresh draft, or died abnormally) must be revivable — this SignalWithStart
		// STARTS a brand-new run. ALLOW_DUPLICATE is the server default; pinned explicitly
		// because the dead-session recovery path depends on it (a stricter policy silently
		// turns "Retry" into a no-op 200).
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}
	in := pdCoAuthorInput{ProjectID: projectID, ArtifactKind: kind, Feedback: feedback, Amendment: amendment}

	// F47: DELIVER the feedback via the redraft SIGNAL, not a bare ExecuteWorkflow. A draft
	// request against an ALREADY-RUNNING session (the retry-at-failed-gate path — the session
	// is suspended at ProjectStageDraftFailed awaiting a decision) resolves USE_EXISTING to the running
	// run; a plain ExecuteWorkflow returns that handle WITHOUT delivering `in`, so the request's
	// feedback was silently DROPPED and the redraft repeated the same mistake. SignalWithStart
	// delivers the redraft signal (carrying the feedback) to the running session's gate AND, when
	// no run is live (fresh start / amendment on a committed→closed slot), starts a new run with
	// `in` (whose Feedback the spine seeds into the first prompt). This mirrors the systemdesign
	// Manager. The gate MERGES the signal feedback with any retained feedback (request wins).
	we, err := m.client.SignalWithStartWorkflow(ctx, wfID, pdSignalRedraft, redraftSignal{Feedback: feedback}, opts, pdExecutionKindCoAuthor, in)
	if err != nil {
		return "", pdMapStartError(err)
	}
	// F-R2 (Phase-2 port): NO FALSE 200s. SignalWithStart's return alone cannot distinguish
	// "fresh run started" from "signal bound to something that will never act", so VERIFY the
	// session's latest execution is now live. Best-effort — only a confirmed abnormal-closed
	// latest run is refused (a Describe blip never masks a genuine start).
	if err := m.verifySessionRevived(ctx, wfID); err != nil {
		return "", err
	}
	return newSessionRef(we.GetID()), nil
}

// verifySessionRevived confirms the co-author session's LATEST execution is not sitting
// abnormally CLOSED right after a SignalWithStart (F-R2 Phase-2 port) — the honest-error
// backstop for the false-200 revival failure. Describe errors are ignored (best-effort; the
// start already durably succeeded).
func (m *projectDesignManager) verifySessionRevived(ctx context.Context, wfID string) error {
	desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, "")
	if derr != nil {
		return nil
	}
	if status := desc.GetWorkflowExecutionInfo().GetStatus(); isAbnormalClosedStatus(status) {
		return newError(fwmanager.Infrastructure,
			"the design session could not be revived — the previous session ended abnormally and no fresh run started; restart the phase or try again")
	}
	return nil
}

// prepareForDraftRequest is the pre-SignalWithStart gate for RequestArtifactDraft (F-R2
// Phase-2 port; mirrors systemdesign). It probes the live session directly (Describe + Query)
// so it can SUPERSEDE a WEDGED run — one whose workflow task is perpetually failing shows
// RUNNING to Describe but rejects the sessionState query with the wedged signature, and a
// SignalWithStart with USE_EXISTING would only BUFFER the redraft signal on that corpse
// forever. On exactly that shape, TERMINATE the wedged run (tolerating a NotFound race) so the
// subsequent SignalWithStart starts a fresh run. Termination is gated STRICTLY on the wedged
// classification — a transient query fault falls through to the normal receptive check and
// surfaces as today's error, never a terminate. Every non-wedged outcome keeps the established
// checkDraftRequestReceptive behavior.
func (m *projectDesignManager) prepareForDraftRequest(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	ctx := rc.Context
	wfID := coAuthorWorkflowID(projectID, kind)
	desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, "")
	if derr != nil {
		if isNotFound(derr) {
			return nil // no session yet — this request starts the first one
		}
		// A Describe blip (non-NotFound): fall back to the query-based receptive check
		// rather than masking a transient fault as receptive.
		return m.checkDraftRequestReceptive(rc, projectID, kind)
	}
	// A non-RUNNING execution (abnormal-closed / completed / paused) is receptive: the
	// SignalWithStart either revives a fresh run or the durable slot is already terminal —
	// none of those is a live Drafting/Redrafting the redraft signal could stale-consume.
	if desc.GetWorkflowExecutionInfo().GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return nil
	}
	// A RUNNING execution: query its live stage — this ONE query ALSO detects the WEDGED shape.
	enc, qerr := m.client.QueryWorkflow(ctx, wfID, "", pdQuerySessionState)
	if qerr != nil {
		if isWorkflowTaskFailedQueryErr(qerr) {
			// Wedged RUNNING run — supersede it so the SignalWithStart starts a fresh run.
			// Tolerate a NotFound (it closed between the query and here); any other terminate
			// fault is surfaced so the caller never silently binds the signal to the corpse.
			if terr := m.client.TerminateWorkflow(ctx, wfID, "", wedgedSupersedeReason); terr != nil && !isNotFound(terr) {
				return newError(fwmanager.Infrastructure,
					"could not supersede the stuck design session before retrying: "+terr.Error())
			}
			return nil // proceed to SignalWithStart (starts a fresh run)
		}
		if isNotFound(qerr) {
			return nil // raced to closed between Describe and Query — the start revives it
		}
		return pdMapQueryError(qerr) // transient — surface, never terminate
	}
	var view ProjectSessionStateView
	if err := enc.Get(&view); err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	// The generating guard: a live Drafting/Redrafting session is NOT receptive (a redraft
	// signal would sit buffered and later stale-consume a recovery gate).
	if view.Stage == ProjectStageDrafting || view.Stage == ProjectStageRedrafting {
		return newError(fwmanager.FailedPrecondition,
			"a draft is already generating for this artifact (currently "+pdSessionStageLabel(view.Stage)+") — wait for it to finish before requesting another")
	}
	return nil
}

// checkDraftRequestReceptive is the manager-side generating guard for RequestArtifactDraft
// (F-R2 Phase-2 port): reject the request while the live session's stage is Drafting or
// Redrafting — a redraft signal sent then is consumable by NO open gate and would sit buffered
// until it stale-consumes a later recovery gate. The stage is read through GetSessionState —
// the SAME Describe-then-Query path (a dead run synthesizes ProjectStageDraftFailed, a COMPLETED run
// is rebuilt from the durable slot, a live run answers the sessionState query) — so the refusal
// always agrees with what the founder sees on screen. NotFound (no session yet) is receptive:
// the request STARTS the first session. Purely a manager-side precondition — replay-safe.
func (m *projectDesignManager) checkDraftRequestReceptive(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil {
		var me *fwmanager.Error
		if errors.As(err, &me) && me.Kind == fwmanager.NotFound {
			return nil // no session yet — this request starts one
		}
		return err
	}
	switch view.Stage {
	case ProjectStageDrafting, ProjectStageRedrafting:
		return newError(fwmanager.FailedPrecondition,
			"a draft is already generating for this artifact (currently "+pdSessionStageLabel(view.Stage)+") — wait for it to finish before requesting another")
	// Every other stage is a settled (or not-yet-started) session: a new request
	// is free to start one.
	case ProjectSessionStageUnknown, ProjectStageAssemblingSDP, ProjectStageAwaitingReview,
		ProjectStageCommitted, ProjectStageWithdrawn, ProjectStageRefused, ProjectStageDraftFailed:
		return nil
	default:
		return nil
	}
}

// checkPhase2Predecessor enforces the Phase-2 spine-ordering gate for a draft request:
// the requested kind's immediate predecessor (per phase2PredecessorKind) must be
// Committed on head-state. Returns nil when the gate is satisfied — the first Phase-2
// kind (planningAssumptions) has no predecessor, so it always passes without a read,
// mirroring the SPA which unlocks planningAssumptions without a sealed Phase 1; a
// redraft of an already in-review / Committed kind also passes (its predecessor is
// committed by construction). Returns FailedPrecondition naming the uncommitted
// predecessor otherwise. Extracted so the gate is unit-testable without a Temporal
// client. Only checks the Phase-2 order (slots 8..16); Phase-1 sealing is the
// Phase2AdvanceWorkflow's concern.
func (m *projectDesignManager) checkPhase2Predecessor(ctx context.Context, projectID ProjectID, kind ArtifactKind) error {
	pred, ok := phase2PredecessorKind(kind)
	if !ok {
		return nil
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRAReadNotFound(err) {
			// A brand-new project with no head-state row: no slot is committed, so
			// the predecessor is by definition uncommitted.
			return newError(fwmanager.FailedPrecondition, predecessorNotCommittedMsg(pred))
		}
		return pdMapReadProjectError(err)
	}
	if pdSlotFor(proj, toPSKind(pred)).Status != projectstate.ReviewCommitted {
		return newError(fwmanager.FailedPrecondition, predecessorNotCommittedMsg(pred))
	}
	return nil
}

// RequestSDPCommit — op 2.2. Temporal Workflow (entry; StartWorkflow /
// signal-with-start), workflow id {projectId}:sdpReview. Idempotent on the id
// (UseExisting): a redundant start (or a replan re-entry) reuses the running
// SDP-review workflow.
func (m *projectDesignManager) RequestSDPCommit(rc fwmanager.Context, projectID ProjectID) (SessionRef, error) {
	ctx := rc.Context
	if projectID == "" {
		return "", newError(fwmanager.ContractMisuse, "empty projectId")
	}

	wfID := sdpReviewWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	in := sdpReviewInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, pdExecutionKindSDPReview, in)
	if err != nil {
		return "", pdMapStartError(err)
	}
	return newSessionRef(we.GetID()), nil
}

// SubmitSDPDecision — op 2.3. Temporal Signal (SignalWorkflow to workflow id
// {projectId}:sdpReview, signal sdpDecision).
//
// Validate: decision ∈ {SDPCommit, SDPRejectAll}; SDPCommit requires a non-empty
// optionID (ContractMisuse otherwise); SDPRejectAll requires feedback with
// non-empty Notes (ContractMisuse otherwise).
func (m *projectDesignManager) SubmitSDPDecision(rc fwmanager.Context, projectID ProjectID, decision SDPDecision, optionID *OptionID, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	switch decision {
	case SDPCommit:
		if optionID == nil || *optionID == "" {
			return newError(fwmanager.ContractMisuse, "Commit requires a non-empty optionId")
		}
	case SDPRejectAll:
		if feedback == nil || feedback.Notes == "" {
			return newError(fwmanager.ContractMisuse, "RejectAll requires feedback")
		}
	case SDPDecisionUnknown:
		// The zero value: a caller that forgot to set Decision, not a legitimate
		// SDP outcome. Reject explicitly rather than falling through silently.
		return newError(fwmanager.ContractMisuse, "unknown SDP decision")
	default:
		return newError(fwmanager.ContractMisuse, "unknown SDP decision")
	}
	// RULING P13: the SDP reject path lands through the same comment-less ledger verb, so a
	// replyTo here would be dropped outright. See pdCheckNoReplyTo.
	if feedback != nil {
		if perr := pdCheckNoReplyTo(feedback.Comments); perr != nil {
			return perr
		}
	}

	wfID := sdpReviewWorkflowID(projectID)
	// PM-P2-4: capture the acting identity for the SdpReview commit's approvedBy provenance.
	sig := sdpDecisionSignal{Decision: decision, OptionID: optionID, Feedback: feedback, Approver: principalLabel(rc.Principal)}
	if err := m.client.SignalWorkflow(ctx, wfID, "", pdSignalSDPDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// SubmitReviewDecision — the per-artifact Phase-2 review gate (OQ-3). Temporal
// Signal (SignalWorkflow to workflow id {projectId}:{artifactKind}, signal
// reviewDecision). feedback required when decision == Reject. kind must be a
// Phase-2 kind other than the SDP review.
func (m *projectDesignManager) SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if err := pdValidateReviewDecisionArgs(projectID, kind, decision, feedback); err != nil {
		return err
	}

	wfID := coAuthorWorkflowID(projectID, kind)

	// F19: precondition — inspect the live session stage BEFORE signaling. A bare
	// SignalWorkflow is fire-and-forget: an approve/reject delivered while the session
	// is drafting, already committed, or was never started is silently BUFFERED or
	// dropped by the workflow (at the failed-recovery gate ReviewApprove is explicitly
	// ignored), yet the op returns success {} — a no-op masquerading as a decision.
	// Query the stage first and refuse a decision the current gate cannot honor with a
	// FailedPrecondition naming the actual stage. (Mirrors systemdesign's F19 fix.)
	view, live, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	if perr := pdCheckReviewPrecondition(decision, view.Stage); perr != nil {
		return perr
	}
	// DEAD-SESSION HONESTY (F-R2 Phase-2 port). An abnormally-CLOSED or WEDGED run synthesizes a
	// ProjectStageDraftFailed view (so the SPA renders the failed card), which PASSES the reject/
	// withdraw precondition above — but a signal to that corpse can never be honored (Temporal
	// refuses it, or the wedged run never processes it). Refuse with an actionable
	// FailedPrecondition instead: the ONLY lever on a dead session is requestArtifactDraft
	// ("Retry"), which starts a fresh run. Ordered AFTER the precondition so a never-started
	// session keeps its "not started" message (pdCheckReviewPrecondition refuses at
	// ProjectSessionStageUnknown). NOTE (F-R2 asymmetry with systemdesign 2.1e): the systemdesign twin
	// additionally honors a Withdraw against a dead session whose slot is staged on MAIN; the
	// spec's 2.1f enumeration did not list that scoped withdraw for Phase-2, so it is NOT ported
	// here — flagged for the architect.
	if !live {
		return newError(fwmanager.FailedPrecondition,
			"the design session for this artifact is no longer running (it ended abnormally) — review decisions cannot reach it. Use \"Retry\" to start a fresh session, then decide on its review gate")
	}
	// REVIEW LEDGER (review-ledger §4): approve is blocked while any comment is still open —
	// the reviewer must send it back (redraft) or resolve each first. The message lists the open ids.
	if decision == ReviewApprove {
		if open := openReviewCommentViewIDs(view.ReviewThread); len(open) > 0 {
			return newError(fwmanager.FailedPrecondition,
				fmt.Sprintf("cannot approve: %d review thread(s) still open (%s) — send them back or resolve them first", len(open), strings.Join(open, ", ")))
		}
	}

	// PM-P2-4: capture the acting reviewer identity for the commit's approvedBy provenance.
	sig := pdReviewDecisionSignal{Decision: decision, Feedback: feedback, Approver: principalLabel(rc.Principal)}
	if err := m.client.SignalWorkflow(ctx, wfID, "", pdSignalReviewDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// reviewGateView returns the session's full gate view (stage + durable review thread) for the
// F19 review precondition AND the review-ledger approve/resolve preconditions, plus whether a
// LIVE workflow can still honor a signal (F-R2 Phase-2 port). Same dead-workflow defense as
// GetSessionState: a CLOSED-ABNORMAL run reports ProjectStageDraftFailed with live=false (a signal to
// it can never be honored), a WEDGED run likewise (live=false), a missing execution reports
// ProjectSessionStageUnknown, and a live run is read from the authoritative sessionState query.
func (m *projectDesignManager) reviewGateView(ctx context.Context, wfID string) (ProjectSessionStateView, bool, error) {
	describeLive := false
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		if status := desc.GetWorkflowExecutionInfo().GetStatus(); isAbnormalClosedStatus(status) {
			return ProjectSessionStateView{Stage: ProjectStageDraftFailed}, false, nil
		}
		describeLive = true
	} else if isNotFound(derr) {
		return ProjectSessionStateView{Stage: ProjectSessionStageUnknown}, false, nil
	}
	enc, err := m.client.QueryWorkflow(ctx, wfID, "", pdQuerySessionState)
	if err != nil {
		if isNotFound(err) {
			return ProjectSessionStateView{Stage: ProjectSessionStageUnknown}, false, nil
		}
		// F-R2: a WEDGED run cannot honor a signal any more than a closed one — return
		// live=false with the failed stage so the !live refusal (which points the human at
		// Retry) fires instead of a raw 5xx. Only when Describe CONFIRMED the run live; a
		// Describe blip + task-failed stays a retryable Infrastructure error.
		if describeLive && isWorkflowTaskFailedQueryErr(err) {
			return ProjectSessionStateView{Stage: ProjectStageDraftFailed}, false, nil
		}
		return ProjectSessionStateView{}, false, pdMapQueryError(err)
	}
	var view ProjectSessionStateView
	if err := enc.Get(&view); err != nil {
		return ProjectSessionStateView{}, false, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, true, nil
}

// SetReviewCommentStatus applies a REVIEWER status transition to one durable review-ledger
// thread (design §3.3): resolve an OPEN or ANSWERED thread to close it (resolving an
// untouched thread IS the old waive), or reopen a RESOLVED one to send it back for another
// redraft. Mirrors SubmitReviewDecision's F19 shape — a
// synchronous precondition check via the sessionState query before signaling the (fire-and-
// forget) branch mutation.
func (m *projectDesignManager) SetReviewCommentStatus(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, commentID string, status string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) || kind == KindSdpReview {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a co-authored Phase-2 kind")
	}
	if commentID == "" {
		return newError(fwmanager.ContractMisuse, "empty commentId")
	}
	switch status {
	case projectstate.ReviewCommentResolved, projectstate.ReviewCommentOpen:
		// close (open|answered -> resolved) or reopen (resolved -> open) — the only
		// reviewer-authored transitions. "answered" is derived by the server from the
		// reply history and is never set by a human.
	default:
		return newError(fwmanager.ContractMisuse, "status must be \"resolved\" (to close a thread) or \"open\" (to reopen a resolved thread)")
	}

	wfID := coAuthorWorkflowID(projectID, kind)
	view, live, err := m.reviewGateView(ctx, wfID)
	if err != nil {
		return err
	}
	// A dead (abnormally-closed/wedged) session synthesizes ProjectStageDraftFailed and a
	// never-started one ProjectSessionStageUnknown — both are !AwaitingReview, so folding !live into
	// this check refuses them with the same honest message (no separate !live branch needed).
	if view.Stage != ProjectStageAwaitingReview || !live {
		return newError(fwmanager.FailedPrecondition,
			"cannot change a review comment: the design is not awaiting review (current stage: "+pdSessionStageLabel(view.Stage)+")")
	}
	if perr := checkCommentTransition(view.ReviewThread, commentID, status); perr != nil {
		return perr
	}

	sig := setCommentStatusSignal{CommentID: commentID, Status: status}
	if err := m.client.SignalWorkflow(ctx, wfID, "", pdSignalSetCommentStatus, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// pdValidateReviewDecisionArgs is SubmitReviewDecision's pure ARGUMENT gate, split out from the
// op body (which reads as the review FLOW): the identifiers are well-formed, the kind belongs
// to the co-authored Phase-2 set, the decision is one the op can act on — with Reject
// additionally requiring the feedback it exists to carry — and no comment in that feedback
// carries a replyTo this Manager cannot route (ruling P13; see pdCheckNoReplyTo). Every check
// here needs nothing but its arguments, so all of them refuse BEFORE the session query rather
// than after a pointless round-trip. Mirrors the systemdesign twin.
func pdValidateReviewDecisionArgs(projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) || kind == KindSdpReview {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a co-authored Phase-2 kind")
	}
	switch decision {
	case ReviewApprove, ReviewWithdraw:
		// ok
	case ReviewReject:
		if feedback == nil || feedback.Notes == "" {
			return newError(fwmanager.ContractMisuse, "Reject requires feedback")
		}
	case ReviewDecisionUnknown, ReviewAdvance, ReviewSetCommentStatus:
		// ReviewAdvance and ReviewSetCommentStatus are stage-4a ADDITIONS to the enum,
		// made by the merged contract. They never reach this rail: the deliveryManager
		// dispatcher answers both itself (the phase seal, and the comment transition),
		// so they join the ignored arm here rather than changing any rail behaviour.
		// The zero value: a caller that forgot to set Decision, not a legitimate
		// review outcome. Reject explicitly rather than falling through silently.
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	default:
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}
	if feedback != nil {
		return pdCheckNoReplyTo(feedback.Comments)
	}
	return nil
}

// pdCheckNoReplyTo refuses a batch carrying ANY replyTo (CONTROLLER RULING P13). Phase-2 reply
// ROUTING is a Stage-2 deliverable: neither this Manager nor its co-author workflow can append
// an utterance into an existing thread, so a replyTo that arrived here could only be converted
// into a fresh unanchored comment — silently detaching the reply from the conversation it
// answers, which is the exact loss design §3.7 exists to prevent. Until Stage 2 routes it,
// refuse loudly: an obvious ContractMisuse beats a silent corruption of the ledger.
//
// THE REFUSAL STAYS, AND THE CLIENT FOLDS (stage-4a pre-final ruling, mirroring R2's
// "the asymmetry is honest until 4b"): the M0 gate DOES offer replies, so the SPA folds a
// Phase-2 reply's text into the comment or question it sends and drops the replyTo it
// cannot route (webApp reviewBatch.ts decisionFeedbackFor / askEntriesFor). The Manager
// does NOT fold on the caller's behalf — a server that quietly rewrote a routed reply
// into a fresh thread would be the silent detachment this check exists to refuse.
func pdCheckNoReplyTo(incoming []AnchoredComment) error {
	for _, c := range incoming {
		if c.ReplyTo != "" {
			return newError(fwmanager.ContractMisuse,
				"replyTo is not supported on Phase-2 (project design) yet — threaded replies are a Stage-2 deliverable; until then a Phase-2 comment can only open a new thread (offending replyTo: "+c.ReplyTo+")")
		}
	}
	return nil
}

// pdCheckReviewPrecondition enforces that the submitted decision is meaningful at the
// session's current stage (F19): approve is honored only at ProjectStageAwaitingReview;
// reject and withdraw are honored at ProjectStageAwaitingReview OR the ProjectStageDraftFailed
// recovery gate (where reject means retry-with-feedback — see awaitDraftFailedRecovery).
// Any other stage yields a FailedPrecondition naming the actual stage.
func pdCheckReviewPrecondition(decision ReviewDecision, stage ProjectSessionStage) error {
	switch decision {
	case ReviewApprove:
		if stage != ProjectStageAwaitingReview {
			return newError(fwmanager.FailedPrecondition,
				"cannot approve: the design is not awaiting review (current stage: "+pdSessionStageLabel(stage)+")")
		}
	case ReviewReject:
		if stage != ProjectStageAwaitingReview && stage != ProjectStageDraftFailed {
			return newError(fwmanager.FailedPrecondition,
				"cannot send back: the design is not at a review or recovery gate (current stage: "+pdSessionStageLabel(stage)+")")
		}
	case ReviewWithdraw:
		if stage != ProjectStageAwaitingReview && stage != ProjectStageDraftFailed {
			return newError(fwmanager.FailedPrecondition,
				"cannot withdraw: no review or recovery gate is open (current stage: "+pdSessionStageLabel(stage)+")")
		}
	case ReviewDecisionUnknown, ReviewAdvance, ReviewSetCommentStatus:
		// ReviewAdvance and ReviewSetCommentStatus are stage-4a ADDITIONS to the enum,
		// made by the merged contract. They never reach this rail: the deliveryManager
		// dispatcher answers both itself (the phase seal, and the comment transition),
		// so they join the ignored arm here rather than changing any rail behaviour.
		// Unreachable: SubmitReviewDecision rejects the zero value as ContractMisuse
		// before reaching the precondition. Guarded for switch-exhaustiveness.
		return newError(fwmanager.ContractMisuse, "unknown review decision")
	}
	return nil
}

// pdSessionStageLabel renders a ProjectSessionStage as a short human label for the precondition
// messages.
func pdSessionStageLabel(s ProjectSessionStage) string {
	switch s {
	case ProjectSessionStageUnknown:
		return "not started"
	case ProjectStageDrafting:
		return "drafting"
	case ProjectStageAssemblingSDP:
		return "assembling SDP"
	case ProjectStageAwaitingReview:
		return "awaiting review"
	case ProjectStageRedrafting:
		return "redrafting"
	case ProjectStageCommitted:
		return "committed"
	case ProjectStageWithdrawn:
		return "withdrawn"
	case ProjectStageRefused:
		return "refused"
	case ProjectStageDraftFailed:
		return "draft failed"
	}
	// Unreachable for the nine defined ProjectSessionStage values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "unknown"
}

// AdvanceToConstruction — op 2.4. Temporal Workflow (entry; StartWorkflow,
// workflow id {projectId}:phaseAdvance:projectDesign). Returns the gating outcome.
//
// F55 STALE-SLOT GATE (Phase-2 twin). A back-edge amendment flags every downstream committed
// slot StaleBasis. Sealing Phase 2 over a stale committed slot silently advances to
// construction on a shifted basis. Before starting the seal workflow, refuse with
// FailedPrecondition naming the stale in-scope (Phase-2) slots — UNLESS the caller explicitly
// acknowledges (acknowledgeStale). The message names the slots so a consumer knows what to
// reconcile.
func (m *projectDesignManager) AdvanceToConstruction(rc fwmanager.Context, projectID ProjectID, acknowledgeStale bool) (PhaseAdvanceResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PhaseAdvanceResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	if !acknowledgeStale {
		if proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID)); rerr == nil {
			if stale := staleCommittedPhase2Kinds(proj); len(stale) > 0 {
				return PhaseAdvanceResult{}, newError(fwmanager.FailedPrecondition,
					fmt.Sprintf("cannot advance to construction: %d committed artifact(s) are stale and must be reconciled first (%s). Re-run the design for each, or advance anyway by acknowledging the staleness.",
						len(stale), strings.Join(stale, ", ")))
			}
		}
	}

	wfID := pdPhaseAdvanceWorkflowID(projectID)
	opts := client.StartWorkflowOptions{
		ID:        wfID,
		TaskQueue: TaskQueue,
	}
	in := phaseAdvanceInput{ProjectID: projectID}

	we, err := m.client.ExecuteWorkflow(ctx, opts, pdExecutionKindPhaseAdvance, in)
	if err != nil {
		return PhaseAdvanceResult{}, pdMapStartError(err)
	}

	var result PhaseAdvanceResult
	if err := we.Get(ctx, &result); err != nil {
		return PhaseAdvanceResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// GetSessionState — op 2.5. Temporal Query (QueryWorkflow, query sessionState,
// read-only). When kind == KindSdpReview, queries {projectId}:sdpReview; otherwise
// {projectId}:{kind}.
func (m *projectDesignManager) GetSessionState(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) (ProjectSessionStateView, error) {
	ctx := rc.Context
	if projectID == "" {
		return ProjectSessionStateView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	var wfID string
	if kind == KindSdpReview {
		wfID = sdpReviewWorkflowID(projectID)
	} else {
		wfID = coAuthorWorkflowID(projectID, kind)
	}

	// F15/F28 + P0-2 (query-side defense, Phase-2 twin). A CoAuthor/SDP workflow answers the
	// sessionState Query by HISTORY-REPLAY even after it has CLOSED, returning its last in-
	// memory stage. For a run that died ABNORMALLY that replayed value lies "drafting in
	// progress" and wedges the SPA on an infinite "GENERATING" screen; for a run that closed
	// NORMALLY (COMPLETED) after committing (or withdrawing) it can ALSO be a stale mid-flight
	// ProjectStageDrafting — the same wedge on a SUCCESSFUL, long-committed artifact. Describe the
	// execution first: an abnormal-closed run synthesizes an honest ProjectStageDraftFailed view; a
	// COMPLETED run is rebuilt from the durable slot on main (committed slot → ProjectStageCommitted +
	// the committed model; any other terminal → honest terminal, never Drafting). A RUNNING /
	// CONTINUED_AS_NEW run (incl. an amendment's fresh run) falls through to the live query,
	// which is authoritative for those. A Describe error other than NotFound is best-effort:
	// fall through to the query rather than masking a transient Describe blip as a failure.
	//
	// describeLive (F-R2) records that Describe CONFIRMED a live execution — only then is a
	// task-failed query below trustworthy as the WEDGED signal (a Describe blip is not).
	describeLive := false
	if desc, derr := m.client.DescribeWorkflowExecution(ctx, wfID, ""); derr == nil {
		switch status := desc.GetWorkflowExecutionInfo().GetStatus(); {
		case isAbnormalClosedStatus(status):
			// F-R2 durable-slot-first: a run can die AFTER its artifact landed on main (a
			// died amendment attempt, or a death just after CommitArtifact), so consult the
			// durable slot before falling back to the failed card (see abnormalClosedSessionView).
			view, err := m.abnormalClosedSessionView(ctx, projectID, kind, status)
			if err != nil {
				return ProjectSessionStateView{}, err
			}
			return pdWithStageName(view), nil
		case status == enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
			view, err := m.completedSessionView(ctx, projectID, kind)
			if err != nil {
				return ProjectSessionStateView{}, err
			}
			return pdWithStageName(view), nil
		}
		// Describe succeeded and the run is neither abnormal-closed nor completed — a LIVE
		// execution (RUNNING / CONTINUED_AS_NEW / PAUSED). A task-failed query below is now
		// trustworthy as the wedged signal.
		describeLive = true
	} else if isNotFound(derr) {
		return ProjectSessionStateView{}, newError(fwmanager.NotFound, "project design has not started for this project")
	}

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", pdQuerySessionState)
	if err != nil {
		// F20 (error altitude): before Phase 2 the co-author/SDP workflow does not
		// exist, and Temporal's raw "workflow not found for ID: <proj>:<n>" leaks the
		// internal execution id to the client. Map that to a clean, user-altitude
		// NotFound; other query faults keep their generic mapping.
		if isNotFound(err) {
			return ProjectSessionStateView{}, newError(fwmanager.NotFound, "project design has not started for this project")
		}
		// F-R2: a WEDGED run (workflow task perpetually failing) shows RUNNING to the Describe
		// above but rejects this query with the wedged signature. Do NOT surface a 5xx that
		// leaves the SPA on an infinite GENERATING screen — synthesize the honest failed card
		// so the human can Retry, which supersedes the stuck run (prepareForDraftRequest). Only
		// when Describe CONFIRMED the run live; a Describe blip + task-failed stays a retryable
		// Infrastructure error.
		if describeLive && isWorkflowTaskFailedQueryErr(err) {
			return pdWithStageName(pdWedgedSessionView(projectID, kind)), nil
		}
		return ProjectSessionStateView{}, pdMapQueryError(err)
	}
	var view ProjectSessionStateView
	if err := enc.Get(&view); err != nil {
		return ProjectSessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return pdWithStageName(view), nil
}

// pdWithStageName stamps the F72 human-readable StageName label alongside the bare Stage int
// on the public ProjectSessionStateView, using pdSessionStageLabel as the single authoritative map
// (the Phase-2 stage enum values DIFFER from Phase-1's, so the label removes the ambiguity).
// Applied at the GetSessionState boundary; StageName is purely additive to the wire shape.
func pdWithStageName(v ProjectSessionStateView) ProjectSessionStateView {
	v.StageName = pdSessionStageLabel(v.Stage)
	return v
}

// staleCommittedPhase2Kinds returns the wire names of every COMMITTED Phase-2 slot that carries
// StaleBasis (a back-edge amendment invalidated its basis) — the set AdvanceToConstruction must
// refuse to seal over unless the caller acknowledges. Order follows Phase2RequiredKinds so the
// message reads deterministically.
func staleCommittedPhase2Kinds(proj projectstate.Project) []string {
	var stale []string
	for _, kind := range projectstate.Phase2RequiredKinds() {
		slot := pdSlotFor(proj, kind)
		if slot.Status == projectstate.ReviewCommitted && slot.StaleBasis {
			stale = append(stale, kind.WireName())
		}
	}
	return stale
}

// pdFailedSessionView synthesizes the human-visible failed view for a session whose workflow
// died abnormally. It reuses ProjectStageDraftFailed — the SAME terminal-failure stage the live
// anti-wedge gate uses — so the SPA renders its existing "design job failed → retry / withdraw"
// card. Carries a neutral human FailureReason.
func pdFailedSessionView(projectID ProjectID, kind ArtifactKind, status enumspb.WorkflowExecutionStatus) ProjectSessionStateView {
	reason := terminatedSessionReason(status)
	return ProjectSessionStateView{
		ProjectID:     projectID,
		ArtifactKind:  kind,
		Stage:         ProjectStageDraftFailed,
		Draft:         DraftModel{Kind: artifactKindWireName(kind)},
		FailureReason: &reason,
	}
}

// completedSessionView derives the honest session view for a CoAuthor/SDP run that closed
// NORMALLY (COMPLETED). The replayed sessionState query is NOT trusted for such a run (it can
// return a stale mid-flight stage — the P0-2 "GENERATING forever" wedge on an already-committed
// artifact), so the view is rebuilt from the DURABLE slot on main.
func (m *projectDesignManager) completedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind) (ProjectSessionStateView, error) {
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return ProjectSessionStateView{}, pdMapReadProjectError(err)
	}
	return pdCommittedSessionView(projectID, kind, pdSlotFor(proj, toPSKind(kind)))
}

// pdCommittedSessionView projects the durable slot of a COMPLETED session onto a
// ProjectSessionStateView. A committed slot renders the committed view (ProjectStageCommitted + the committed
// model + the durable review thread). A withdrawn slot renders ProjectStageWithdrawn. Any other
// terminal-but-uncommitted state renders an honest ProjectStageDraftFailed terminal carrying a neutral
// reason — NEVER ProjectStageDrafting, so the SPA never wedges on an infinite "GENERATING" spinner.
func pdCommittedSessionView(projectID ProjectID, kind ArtifactKind, slot projectstate.ArtifactSlot) (ProjectSessionStateView, error) {
	switch slot.Status {
	case projectstate.ReviewCommitted:
		draft, err := draftModelFor(kind, slot.Model)
		if err != nil {
			return ProjectSessionStateView{}, newError(fwmanager.Infrastructure, err.Error())
		}
		return ProjectSessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        ProjectStageCommitted,
			Draft:        draft,
			ReviewThread: reviewThreadToView(slot.ReviewThread),
		}, nil
	case projectstate.ReviewWithdrawn:
		return ProjectSessionStateView{
			ProjectID:    projectID,
			ArtifactKind: kind,
			Stage:        ProjectStageWithdrawn,
			Draft:        DraftModel{Kind: artifactKindWireName(kind)},
		}, nil
	case projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected:
		// Any non-committed / non-withdrawn terminal status renders the honest
		// ProjectStageDraftFailed view (never ProjectStageDrafting — the anti-wedge rule).
		fallthrough
	default:
		reason := "the design session ended without committing an artifact. Retry to start a fresh draft."
		return ProjectSessionStateView{
			ProjectID:     projectID,
			ArtifactKind:  kind,
			Stage:         ProjectStageDraftFailed,
			Draft:         DraftModel{Kind: artifactKindWireName(kind)},
			FailureReason: &reason,
		}, nil
	}
}

// pdWedgedSessionView synthesizes the honest failed card for a WEDGED run (F-R2): the workflow
// task is perpetually failing, so the sessionState query cannot answer even though the run
// still reports RUNNING to Describe. It reuses ProjectStageDraftFailed (the SPA's retry/withdraw
// card), with copy promising that Retry supersedes the stuck session — which
// prepareForDraftRequest actually does (terminate-then-SignalWithStart).
func pdWedgedSessionView(projectID ProjectID, kind ArtifactKind) ProjectSessionStateView {
	reason := "the design session hit an internal fault and cannot answer — Retry to start a fresh draft (the stuck session will be superseded)"
	return ProjectSessionStateView{
		ProjectID:     projectID,
		ArtifactKind:  kind,
		Stage:         ProjectStageDraftFailed,
		Draft:         DraftModel{Kind: artifactKindWireName(kind)},
		FailureReason: &reason,
	}
}

// abnormalClosedSessionView derives the honest view for a session whose workflow ended
// ABNORMALLY (FAILED/TERMINATED/TIMED_OUT/CANCELED). Durable-slot-first (F-R2): a run can die
// AFTER its artifact landed on main (a died amendment attempt, or a death just after
// CommitArtifact), so consult main's slot before falling back to the failed card:
//
//   - Committed → the committed view (ProjectStageCommitted + the model) CARRYING a FailureReason so
//     the last session's abnormal end stays visible; the committed view's amend affordance IS
//     the retry, so this un-deadlocks the died-amendment case with ZERO writes.
//   - Withdrawn → the withdrawn view.
//   - anything else (the run died before committing) → today's failed card, preserving the
//     anti-wedge fix for a first-draft death.
//
// If the durable slot cannot be consulted — the store is unavailable (nil) or the read
// faults — it falls back to the failed card rather than erroring or panicking: never wedge on
// a recovery read (the failed card still offers Retry), matching the pre-fix behavior exactly.
func (m *projectDesignManager) abnormalClosedSessionView(ctx context.Context, projectID ProjectID, kind ArtifactKind, status enumspb.WorkflowExecutionStatus) (ProjectSessionStateView, error) {
	if m.projectState == nil {
		return pdFailedSessionView(projectID, kind, status), nil
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return pdFailedSessionView(projectID, kind, status), nil
	}
	slot := pdSlotFor(proj, toPSKind(kind))
	switch slot.Status {
	// A settled slot still has a model worth showing, even though the session
	// that produced it died; every other status has nothing to show but the
	// failure.
	case projectstate.ReviewCommitted, projectstate.ReviewWithdrawn:
		view, verr := pdCommittedSessionView(projectID, kind, slot)
		if verr != nil {
			return ProjectSessionStateView{}, verr
		}
		if slot.Status == projectstate.ReviewCommitted {
			reason := "the last design session ended unexpectedly (" + workflowStatusLabel(status) + "); the committed model shown is unaffected"
			view.FailureReason = &reason
		}
		return view, nil
	case projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected:
		return pdFailedSessionView(projectID, kind, status), nil
	default:
		return pdFailedSessionView(projectID, kind, status), nil
	}
}

// --- error mapping at the façade boundary -----------------------------------

// isRAReadNotFound reports whether err is a RAW projectStateAccess fwra.NotFound
// (a brand-new / unknown project) returned DIRECTLY on the sync façade read path —
// distinct from workflow.go's isReadNotFound, which inspects the Temporal-wrapped
// ApplicationError on the replayed Activity path.
func isRAReadNotFound(err error) bool {
	var raErr *fwra.Error
	return errors.As(err, &raErr) && raErr.Kind == fwra.NotFound
}

// pdMapReadProjectError converts a projectStateAccess.ReadProject error on the sync
// spine-ordering-gate read path into a fwmanager.Error: fwra.NotFound → NotFound
// (unknown project), everything else → Infrastructure. (fwra.NotFound is handled
// specially by the RequestArtifactDraft caller as an uncommitted predecessor; this
// mapper covers the non-NotFound faults.)
func pdMapReadProjectError(err error) error {
	if isRAReadNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

func pdMapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; the
	// SDK returns the existing handle without error. Any error here is treated as a
	// infrastructure fault.
	return newError(fwmanager.Infrastructure, err.Error())
}

func pdMapQueryError(err error) error {
	if isNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	// A session whose workflow task is FAILING (e.g. a deploy-time non-determinism
	// fault being retried) rejects queries with the raw Temporal internals
	// "Unable to query workflow due to Workflow Task in failed state" (observed on
	// the systemdesign twin, gtdapp:5). Same error-hygiene rule as the 065a9e7
	// not-found cleanup: clients get a clean, actionable Detail.
	if isWorkflowTaskFailedQueryErr(err) {
		return newError(fwmanager.Infrastructure,
			"design session state is temporarily unavailable — the session hit an internal fault and is being retried by the server; try again shortly")
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// ---------------------------------------------------------------------------
// Identity / domain scalars (projectdesign's OWN named types — value-identical to
// projectstate; the Manager converts at the projectStateAccess boundary). They are
// PURE DATA on the generated surface; behavior lives in behavior.go as free
// functions so contract.gen.go imports no projectstate.
// ---------------------------------------------------------------------------

// ProjectID is the project aggregate identifier — its value IS the user-supplied
// adopted repo name (name-as-identity). Mirrors projectstate.ProjectID.

// OptionID names one project-design option in the SDP review (the architect commits
// one at the option-commitment gate). Mirrors projectstate.OptionID.

// ArtifactKind is the closed artifact-slot enum. The ordinals MIRROR
// projectstate.ArtifactKind so int(...) conversion at the boundary is
// meaning-preserving; behavior (WireName/IsPhase2/...) lives in behavior.go as free
// functions over a projectstate conversion so the generated type stays pure data.

// ---- Phase 1 (carried for ordinal parity with projectstate; not driven here) ----

// ---- Phase 2 ----

// ---------------------------------------------------------------------------
// Session reference + review surface.
// ---------------------------------------------------------------------------

// SessionRef is an opaque, infrastructure-opaque reference to a running Phase-2
// session (an artifact-co-authoring session or the SDP-review session — contract
// §3.1). It wraps the underlying durable-execution identity as an opaque string the
// Client persists/echoes and never parses. Construction is via the newSessionRef
// free function (behavior.go).

// ReviewDecision is the architect's commit-authority decision at the per-artifact
// Phase-2 review gate (contract §10 OQ-3 — Phase-2 artifacts ARE individually
// gated, mirroring Phase 1).

// commit the typed model in its slot
// loop back to draft with feedback
// abandon the draft

// ReviewFeedback is the architect's free-text rejection/withdraw rationale
// (contract §3.2). Required on Reject and on an SDP RejectAll; optional on
// Withdraw; ignored on Approve.

// SDPDecision is the architect's decision at the option-commitment gate
// (contract §3.2). Commit binds the named option; RejectAll re-enters Phase 2
// with feedback to produce a fresh SDP review.

// bind the named option, commit the review
// record the rejected outcome; re-assemble with feedback

// PhaseAdvanceResult is the gating outcome of advanceToConstruction
// (contract §3.3). A non-Advanced result is the NORMAL "you still owe artifacts
// X, Y / no option bound" answer (not an error).

// ---------------------------------------------------------------------------
// Session read view (getSessionState) + the OPAQUE staged-draft envelope.
//
// DraftModel is the discriminated {kind, model} envelope the staged typed draft /
// assembled SdpReview is carried as — IDENTICAL on the wire to the systemdesign
// DraftModel envelope. The model is carried OPAQUELY as raw JSON: projectdesign
// never names the concrete projectstate model types or the sealed ArtifactModel sum
// here.
// ---------------------------------------------------------------------------

// DraftModel is the opaque {kind, model} envelope carrying the staged typed draft (or
// the assembled SdpReview) as raw JSON. Model is omitted when no draft is staged.
// Kind is the canonical camelCase wire name (e.g. "planningAssumptions").

// ProjectSessionStage collapses the technical workflow state into the handful of stages
// the UI needs (contract §3.4). ProjectStageAssemblingSDP sits between drafting and
// awaiting-review for the SDP-review session.

// worker dispatched; typed model not yet produced
// SDP-review workflow: assembling options + joining Engine outputs
// model staged (status AwaitingReview); suspended on the review signal
// architect rejected; looping back with feedback
// commitArtifact applied; terminal for this kind/option
// withdrawArtifact applied; terminal
// worker refused/cancelled and could not produce a model; terminal
// ProjectStageDraftFailed (agentic-pivot D-MPD-Δ, §3.4 — the twin of systemDesignManager
// ProjectStageDraftFailed) is the human-visible, human-actionable stage the session lands
// in when the dispatched agentic Phase-2 DESIGN job reaches a TYPED terminal failure
// phase. It carries the job's neutral Diagnostic in FailureReason. Surfaced by
// getSessionState so the SPA renders an actionable failure and NEVER a perpetual
// ProjectStageDrafting / ProjectStageAssemblingSDP spinner (the anti-wedge requirement).

// ProjectSessionStateView is a point-in-time, read-only view of one Phase-2 session's
// TECHNICAL progress (contract §3.4) — the answer to getSessionState (a Temporal
// Query), NOT the business-state read. The staged TYPED draft / assembled SdpReview
// is carried OPAQUELY via DraftModel; Findings explain "why it's being redrafted".

// Draft is the staged typed draft / SdpReview awaiting review, carried as the
// opaque {kind, model} envelope (model nil before the first stage).

// FailureReason is a short, human, non-leaking explanation set ONLY when Stage is
// ProjectStageDraftFailed (a terminal Phase-2 design-job failure). It gives the SPA a
// message + recovery affordance instead of a wedged "generating" screen. Empty
// (nil) otherwise.

// ---------------------------------------------------------------------------
// Façade error model (projectDesignManager.md §3.5).
// These are CALLER/PROGRAMMER errors at the façade boundary — distinct from the
// workflow's own failure handling. Kinds follow the framework-go standard set.
// ---------------------------------------------------------------------------

// behavior.go holds the FREE FUNCTIONS that carry behavior over the contract value
// types. The generated contract surface (contract.gen.go) is PURE DATA — enums and
// structs with no methods — so any logic over a contract value (the canonical-name
// lookups that used to be methods on the projectstate enums, the opaque SessionRef
// constructor) lives here as a free function.
//
// projectdesign's OWN ArtifactKind mirrors projectstate.ArtifactKind ordinal-for-
// ordinal, so its behavior is derived by a meaning-preserving int conversion to the
// canonical projectstate type rather than re-implemented here. This is the Phase-2
// twin of systemdesign/behavior.go.

// fromPSKind converts a canonical projectstate.ArtifactKind to projectdesign's OWN
// ArtifactKind (ordinal-preserving) at the read boundary.
func fromPSKind(k projectstate.ArtifactKind) ArtifactKind { return ArtifactKind(k) }

// artifactKindIsPhase2 reports whether the kind belongs to The Method's Phase 2.
func artifactKindIsPhase2(k ArtifactKind) bool { return toPSKind(k).IsPhase2() }

// phase2RequiredKinds returns the ordered set of Phase-2 artifact kinds (projectdesign's
// OWN type), mirroring projectstate.Phase2RequiredKinds() — the same order the SPA's
// PHASE2_ORDER locks steps by.
func phase2RequiredKinds() []ArtifactKind {
	ps := projectstate.Phase2RequiredKinds()
	out := make([]ArtifactKind, 0, len(ps))
	for _, k := range ps {
		out = append(out, fromPSKind(k))
	}
	return out
}

// phase2PredecessorKind returns the Phase-2 kind that must be Committed immediately
// before `kind` may be drafted — the wire-side mirror of the SPA's Phase-2 buildSpine
// step lock. The first required kind (planningAssumptions) has no predecessor and
// returns (_, false); a kind not in the Phase-2 set likewise returns (_, false).
func phase2PredecessorKind(kind ArtifactKind) (ArtifactKind, bool) {
	req := phase2RequiredKinds()
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

// pdEncodeProject wraps the head-state aggregate for the Temporal boundary, delegating
// to the promoted projectstate.EncodeProject.
//
// F16 (payload slimming): the Phase-1 ResearchInput corpus is DELIBERATELY NOT
// carried here — projectstate.EncodeProject leaves ProjectEnvelope.Research nil by
// default and this wrapper does NOT opt in (unlike systemdesign's own pdEncodeProject).
// A research source can be a whole book (660KB observed), and every projectdesign
// Activity payload crosses the Temporal boundary — dead weight that pushes toward
// Temporal's 2MB kill threshold. Phase-2 project design never reads the corpus (unlike
// systemdesign, whose mission-draft step legitimately weaves it in — that Manager's
// envelope opts in), so dropping the field costs nothing here.
func pdEncodeProject(p projectstate.Project) (projectEnvelope, error) {
	return projectstate.EncodeProject(p)
}

// findings.go owns the SESSION-TRANSIENT validation-finding value types this Manager
// surfaces on its getSessionState read (ProjectSessionStateView.Findings). The SPA renders
// findings[] to explain "why it's being redrafted". They are part of this component's
// OWN generated contract surface (registered in cmd/schemagen) — pure data, no methods.
//
// Defined LOCALLY (mirroring systemdesign/findings.go) because a Manager importing
// another Manager is a sideways edge the layer model forbids (TestMethodLayering
// NoSideways); systemdesign and projectdesign each own their own copy.
//
// WIRE: severity is a camelCase STRING name ("info"|"warning"|"error"). It is a STRING
// enum (the value IS the wire name) so the generated type is pure data AND the wire
// form is byte-identical — f.severity === 'error' / 'warning' in the SPA decodes
// unchanged.

// Severity is a finding severity. Only SeverityError fails a verdict; Warning/Info
// ride along advisory. The value IS its canonical camelCase wire name.

// RuleID is the stable, namespaced id of a validation rule. Stable across runs for
// finding-diff and worker-prompt continuity.

// Location locates a finding within a typed model. NO Line field: the input is a
// typed model, not bytes.

// stable position used for deterministic finding ordering
// human-readable locus, e.g. "Objective 4"

// Finding is a single machine-checkable rule violation surfaced to the SPA.

// human-readable; safe to weave into a redraft prompt; no PII
// optional; where in the model the finding sits

// acknowledgestale.go implements the F45 per-slot staleness-acknowledge op for Project Design
// (twin of the systemdesign impl): a reviewer marks a stale COMMITTED Phase-2 artifact
// "reviewed — unaffected", clearing its StaleBasis WITHOUT a redraft, with a durable staleAck
// audit entry — both committed atomically on main.

func (m *projectDesignManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, note string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if strings.TrimSpace(note) == "" {
		return newError(fwmanager.ContractMisuse, "an acknowledgement requires a non-empty note — it is the reviewer's durable justification, and it also keys the idempotency of the ack")
	}
	if !artifactKindIsPhase2(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
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
			return pdMapReadProjectError(err)
		}
		_, err = m.projectState.AcknowledgeStaleBasis(fwra.Context{Context: ctx}, psID, proj.Version, psKind, note, key)
		if err == nil {
			return nil
		}
		if isRAConflict(err) {
			lastErr = err
			continue
		}
		return mapStaleAckError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AcknowledgeStaleBasis: exhausted conflict retries")
}

// refuseAckDuringLiveSession is the F-GTD-12 guard: while the target kind has a LIVE
// co-author (amendment) session, the acknowledge is refused with a FailedPrecondition
// (the wire's 409/"failed_precondition" conflict shape). Liveness is read through
// GetSessionState — the SAME Describe-then-Query path the review gate and the SPA trust
// (a dead run synthesizes ProjectStageDraftFailed; a COMPLETED run is rebuilt from the durable
// slot) — so ack gating always agrees with what the reviewer sees on screen. A NotFound
// (no session ever ran for this slot) passes.
func (m *projectDesignManager) refuseAckDuringLiveSession(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) error {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil {
		var me *fwmanager.Error
		if errors.As(err, &me) && me.Kind == fwmanager.NotFound {
			return nil
		}
		return err
	}
	if !pdSessionStageIsLive(view.Stage) {
		return nil
	}
	return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
		"cannot mark this artifact reviewed: its amendment session is still open (currently %s). Reconcile rides the amendment — acknowledging now would commit to main and merge-conflict the amendment's review PR. Approve or withdraw the session first.",
		pdSessionStageLabel(view.Stage)))
}

// pdSessionStageIsLive reports whether a co-author session stage means the session still
// OWNS the slot (its branch/PR is open or recoverable): drafting / assembling /
// awaiting review / redrafting, plus the ProjectStageDraftFailed recovery gate (the session is
// suspended there with its branch and PR intact — a Retry resumes it). The terminal
// stages (committed / withdrawn / refused) and the unknown zero value are NOT live.
func pdSessionStageIsLive(s ProjectSessionStage) bool {
	switch s {
	case ProjectStageDrafting, ProjectStageAssemblingSDP, ProjectStageAwaitingReview, ProjectStageRedrafting, ProjectStageDraftFailed:
		return true
	case ProjectSessionStageUnknown, ProjectStageCommitted, ProjectStageWithdrawn, ProjectStageRefused:
		return false
	default:
		return false
	}
}

// mapStaleAckError surfaces the RA's ContractMisuse (uncommitted / unknown kind) and NotFound
// (unknown project) as their manager equivalents; everything else is Infrastructure.
func mapStaleAckError(err error) error {
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.ContractMisuse:
			return newError(fwmanager.ContractMisuse, err.Error())
		case fwra.NotFound:
			return newError(fwmanager.NotFound, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "everything else is Infrastructure" per the doc comment above.
			return newError(fwmanager.Infrastructure, err.Error())
		default:
			return newError(fwmanager.Infrastructure, err.Error())
		}
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// askquestions.go implements the question-comments op for Project Design (twin of the
// systemdesign implementation; founder-ratified 2026-07-05): AskQuestions appends clarifying
// QUESTIONS to a Phase-2 artifact's review ledger WITHOUT a redraft and dispatches a
// lightweight ANSWER job so the addressed role answers each in place. Open questions do NOT
// block approve; asking works on a committed artifact (main) and on a live session (branch).

// Dispatch inputs for the design jobs. Project Design has no PM-critique, so its dispatch
// path historically carried no job_mode; under thin dispatch the MCP scopes its ambient
// mode on this input, so BOTH the draft and answer jobs now set it — pdJobModeDraft on the
// workflow-side draft dispatch (dispatch.go), pdJobModeAnswer on this manager-side answer job.
const (
	pdDispatchInputJobMode = "job_mode"
	pdJobModeDraft         = "draft"
	pdJobModeAnswer        = "answer"
)

// AskQuestions — the Project-Design question-comments op. See the systemdesign twin for the
// full contract; the only differences are the Phase-2 kind gate and the Phase-2 pdSlotFor.
//
// DISPATCH RECOVERY (F82): the answer job is BEST-EFFORT — the questions are seeded durably
// first, then a lightweight answer job is dispatched. A dispatch MISS (pipeline/repo not
// configured, repo unresolved, or a workflow_dispatch fault) is now LOGGED LOUDLY server-side
// (it was previously discarded, and the construction-pipeline RA has no logger, so a miss
// vanished — an open question that would never be answered with zero operator signal). To
// RECOVER a dropped dispatch, simply CALL AskQuestions AGAIN with the same questions: the seed
// is idempotent on its content key, so NO ledger entry is duplicated (the existing entries'
// round is reused so the minted ids still match), while the answer-job dispatch RE-FIRES via a
// per-call-unique key.
func (m *projectDesignManager) AskQuestions(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, addressee string, questions []AnchoredComment) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if !artifactKindIsPhase2(kind) {
		return newError(fwmanager.FailedPrecondition, "artifactKind is not a Phase-2 kind")
	}
	switch addressee {
	case projectstate.ReviewAddresseePM, projectstate.ReviewAddresseeArchitect:
		// ok
	default:
		return newError(fwmanager.ContractMisuse, "addressee must be \"pm\" or \"architect\"")
	}
	// RULING P13 + design §3.7: a question OPENS its own thread, so a replyTo has no meaning
	// here and questionsToLedger would drop it. See pdCheckNoReplyTo.
	if perr := pdCheckNoReplyTo(questions); perr != nil {
		return perr
	}
	qs := questionsToLedger(addressee, questions)
	if len(qs) == 0 {
		return newError(fwmanager.ContractMisuse, "no questions to ask (every question needs text)")
	}

	branch := m.resolveQuestionBranch(rc, projectID, kind)
	psID := projectstate.ProjectID(projectID)
	psKind := toPSKind(kind)
	key := pdAskQuestionsIdempotencyKey(projectID, kind, branch, qs)

	var lastErr error
	for range askQuestionsMaxAttempts {
		proj, err := m.readProjectMaybeBranch(ctx, psID, branch)
		if err != nil {
			return pdMapReadProjectError(err)
		}
		thread := pdSlotFor(proj, psKind).ReviewThread
		round := nextQuestionRound(thread)
		if r, ok := existingQuestionRound(thread, qs); ok {
			// A prior ask already seeded these exact questions (its answer-job dispatch may
			// have been dropped — F82). Reuse their round so the minted ids match the EXISTING
			// ledger entries, and the re-fired answer job answers the right comments.
			round = r
		}
		_, err = m.designSession.SeedReviewCommentsOnBranch(fwra.Context{Context: ctx}, psID, proj.Version, branch, psKind, round, qs, nil, key)
		if err == nil {
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
		return pdMapReadProjectError(err)
	}
	return fwmanager.Wrap(fwmanager.Infrastructure, lastErr, "AskQuestions: exhausted conflict retries")
}

// resolveQuestionBranch — twin of the systemdesign impl (see there for the full F73 rationale).
// A GENUINELY ACTIVE session (co-author workflow OPEN and in a non-terminal stage) keeps its
// ledger on the session branch; every closed/completed/withdrawn/failed/absent run falls back
// to main (""). Resolution reuses the P0-2 Describe-first machinery via GetSessionState rather
// than a bare sessionState Query, which would REPLAY a dead run's stale live stage and wrongly
// resolve an abandoned amendment's leftover branch.
func (m *projectDesignManager) resolveQuestionBranch(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) string {
	view, err := m.GetSessionState(rc, projectID, kind)
	if err != nil || !pdIsLiveSessionStage(view.Stage) {
		return ""
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: rc.Context}, projectstate.ProjectID(projectID))
	if err != nil {
		return ""
	}
	return projectstate.DesignBranch(projectstate.ProjectID(projectID), toPSKind(kind), projectstate.AmendmentIndexFor(pdSlotFor(proj, toPSKind(kind))))
}

// readProjectMaybeBranch reads the head-state aggregate from the given branch. The
// on-branch read moved onto the designSessionAccess facet (Wave 1 reconciliation), which
// ships the aggregate as a ProjectEnvelope across the Manager-Temporal boundary; decode it
// back to the concrete Project here. branch=="" reads main exactly as ReadProject.
func (m *projectDesignManager) readProjectMaybeBranch(ctx context.Context, psID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	env, err := m.designSession.ReadProjectOnBranch(fwra.Context{Context: ctx}, psID, branch)
	if err != nil {
		return projectstate.Project{}, err
	}
	return env.Decode()
}

// pdIsLiveSessionStage reports whether a co-author session is live (its ledger lives on the
// session branch, not main).
func pdIsLiveSessionStage(stage ProjectSessionStage) bool {
	switch stage {
	case ProjectStageDrafting, ProjectStageAwaitingReview, ProjectStageRedrafting, ProjectStageRefused:
		return true
	case ProjectSessionStageUnknown, ProjectStageAssemblingSDP, ProjectStageCommitted, ProjectStageWithdrawn, ProjectStageDraftFailed:
		return false
	default:
		return false
	}
}

func pdAskQuestionsIdempotencyKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
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

// pdAnswerJobDispatchKey derives a per-call-unique answer-job idempotency key from the
// content base plus a monotonic nonce (see answerJobDispatchSeq).
func pdAnswerJobDispatchKey(projectID ProjectID, kind ArtifactKind, branch string, qs []projectstate.ReviewComment) fwra.IdempotencyKey {
	base := pdAskQuestionsIdempotencyKey(projectID, kind, branch, qs)
	return fwra.IdempotencyKey(fmt.Sprintf("%s:answerJob:%d", base, answerJobDispatchSeq.Add(1)))
}

// dispatchAnswerJob fires the BEST-EFFORT answer job for the freshly-seeded questions and
// LOGS the outcome loudly (F82). A dispatch miss previously vanished (the error was discarded
// and the construction-pipeline RA has no logger); now every failure mode is logged at ERROR
// (or WARN when the rail is simply not configured) with the projectID/kind/addressee/branch,
// and a success at INFO. The questions are already recorded, so a miss is recoverable by
// re-calling AskQuestions (see the op doc) — never silent.
func (m *projectDesignManager) dispatchAnswerJob(ctx context.Context, projectID ProjectID, kind ArtifactKind, branch, addressee string, qs []projectstate.ReviewComment) {
	log := slog.Default().With(
		"op", "projectdesign.AskQuestions.dispatchAnswerJob",
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
	// fire-and-forget submit over the PUBLISHED agenticJobAccess RA. The
	// RepoRef→RepoTarget decode + the placeholder step graph that the retired
	// pipelineDispatchAdapter added are inlined here (the workflow-side twin is
	// dispatchDesignJob in dispatch.go).
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
		pdDispatchInputJobMode:     pdJobModeAnswer,
	}
	spec := agenticjob.PipelineSpec{
		ProjectID: agenticjob.ProjectID(projectID),
		Steps: []agenticjob.PipelineStep{{
			Name:      "design",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
		WorkflowFile:   designWorkflowFileName,
	}
	key := pdAnswerJobDispatchKey(projectID, kind, branch, qs)
	handle, err := m.pipeline.SubmitAgenticJob(fwra.Context{Context: ctx, IdempotencyKey: key}, spec)
	if err != nil {
		log.Error("answer job dispatch FAILED — the question is recorded but not auto-answered; re-run AskQuestions with the same question to retry",
			"err", err.Error(), "key", string(key))
		return
	}
	log.Info("answer job dispatched", "key", string(key))
	m.watchAnswerEpisode(ctx, projectID, kind, handle, log)
}

// ---------------------------------------------------------------------------
// Episode capture for the ANSWER job (SP1 capture-seam, Task 7)
// ---------------------------------------------------------------------------
//
// The answer job is the ONE agentic dispatch this Manager makes outside a Temporal
// workflow: AskQuestions submits it fire-and-forget and returns. Nothing observes it, so
// without this watch every answer episode — real tokens, really spent — would be invisible
// to the ledger.
//
// NON-DURABLE BY CONSTRUCTION, and that is accepted: this is a plain goroutine in the
// server process. A restart between the dispatch and the terminal observation loses the
// watch and therefore the record — no gap line either, because nothing is left to write
// one. Only the WORKFLOW-side capture paths carry the durable never-silent guarantee; the
// answer job is auxiliary (it gates nothing) and did not warrant its own workflow.

// watchAnswerEpisode spawns the bounded manager-side watch for one dispatched answer job.
// It detaches from the CALLER'S context on purpose: ctx is the AskQuestions request
// context and is cancelled the moment that call returns, while the job it dispatched runs
// for minutes afterwards. WithoutCancel keeps the request's values (tracing, principal)
// and drops only the cancellation.
func (m *projectDesignManager) watchAnswerEpisode(ctx context.Context, projectID ProjectID, kind ArtifactKind, handle agenticjob.PipelineHandle, log *slog.Logger) {
	w := pdAnswerEpisodeWatch{
		pipeline: m.pipeline,
		episodes: m.episodes,
		poll:     answerEpisodePollInterval,
		window:   answerEpisodeWatchWindow,
		log:      log,
	}
	go w.run(context.WithoutCancel(ctx), projectID, artifactKindString(kind), handle)
}

// pdAnswerEpisodeWatch is the bounded observe-then-append loop behind watchAnswerEpisode,
// broken out with its timings injected so it can be exercised deterministically in tests.
type pdAnswerEpisodeWatch struct {
	pipeline agenticjob.AgenticJobAccess
	episodes episode.EpisodeAccess
	poll     time.Duration
	window   time.Duration
	log      *slog.Logger
}

// run polls handle to a terminal phase (or to the window's end) and appends the ONE ledger
// record the dispatch owes. Blocking — watchAnswerEpisode spawns it.
func (w pdAnswerEpisodeWatch) run(ctx context.Context, projectID ProjectID, targetRef string, handle agenticjob.PipelineHandle) {
	if w.pipeline == nil || w.episodes == nil {
		return
	}
	watchCtx, cancelWatch := context.WithTimeout(ctx, w.window)
	defer cancelWatch()

	obs, terminal := w.observeToTerminal(watchCtx, handle)
	if terminal && episodeVenueIsRemote(obs.RunURL) {
		// Remote venue mines no episode in v1 — nothing was lost, so record nothing.
		return
	}
	rec := w.answerRecord(obs, terminal, targetRef, handle)

	// THE APPEND MUST NOT RIDE watchCtx. On the DEADLINE path observeToTerminal returned
	// precisely BECAUSE watchCtx expired, so appending under it would hand the ledger an
	// already-cancelled context — making the gap record the deadline exists to write the
	// one write guaranteed to fail. Derive a fresh, cancellation-free budget from the
	// caller's context instead. (Today's AppendEpisode realisations ignore the context
	// entirely, so this is latent rather than live; a store that honours it would turn the
	// never-silent guarantee into a silent loss on exactly the path that needs it most.)
	appendCtx, cancelAppend := context.WithTimeout(context.WithoutCancel(ctx), answerEpisodeAppendWindow)
	defer cancelAppend()
	w.appendRecord(appendCtx, projectID, rec, handle)
}

// appendRecord writes the record with a small BOUNDED retry. The workflow-side capture
// gets Temporal's retry envelope for free; this path has none, so without it a single
// transient store stumble would lose the episode outright.
func (w pdAnswerEpisodeWatch) appendRecord(ctx context.Context, projectID ProjectID, rec episode.EpisodeRecord, handle agenticjob.PipelineHandle) {
	key := fwra.IdempotencyKey("answerEpisode:" + string(handle))
	var err error
	for attempt := 1; attempt <= answerEpisodeAppendAttempts; attempt++ {
		err = w.episodes.AppendEpisode(fwra.Context{Context: ctx, IdempotencyKey: key},
			episode.ProjectID(projectID), rec)
		if err == nil {
			return
		}
		if attempt == answerEpisodeAppendAttempts ||
			!waitOrDone(ctx, time.Duration(attempt)*answerEpisodeAppendBackoff) {
			break
		}
	}
	w.log.Error("answer-job episode NOT recorded: ledger append failed after its bounded retry",
		"episodeId", rec.EpisodeID, "attempts", answerEpisodeAppendAttempts, "err", err.Error())
}

// observeToTerminal polls the dispatched job until it reaches a terminal phase, the window
// closes, or the RA faults. terminal=false means the second or third — the caller turns
// that into a gap record.
func (w pdAnswerEpisodeWatch) observeToTerminal(ctx context.Context, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, bool) {
	var last agenticjob.PipelineObservation
	cancelGrace := 0
	for {
		obs, err := w.pipeline.ObserveAgenticJob(fwra.Context{Context: ctx}, handle)
		if err != nil {
			return last, false
		}
		last = obs
		if terminal, done := w.classify(obs, &cancelGrace); done {
			return obs, terminal
		}
		if !waitOrDone(ctx, w.poll) {
			return last, false
		}
	}
}

// classify decides whether THIS observation ends the watch. A terminal observation with a
// summary always does. A terminal observation WITHOUT one ends it too — except for the
// CANCEL RACE, where the phase flips synchronously while the agent subprocess is still
// unwinding: that gets maxLateEpisodePolls further polls (the same grace the workflow-side
// capture gives it) before the run is written off.
func (w pdAnswerEpisodeWatch) classify(obs agenticjob.PipelineObservation, cancelGrace *int) (terminal, done bool) {
	if !pdDesignPipelinePhase(obs.Phase).IsTerminal() {
		return false, false
	}
	if obs.Episode != nil || obs.Phase != agenticjob.PhaseCancelled {
		return true, true
	}
	if *cancelGrace >= maxLateEpisodePolls {
		return true, true
	}
	*cancelGrace++
	return false, false
}

// answerRecord composes the ledger record for a watched answer job: the mined summary, or
// an explicit GAP naming which of the two ways it went missing.
func (w pdAnswerEpisodeWatch) answerRecord(obs agenticjob.PipelineObservation, terminal bool, targetRef string, handle agenticjob.PipelineHandle) episode.EpisodeRecord {
	// Lineage is nil BY DESIGN: this dispatch has no durable execution behind it.
	if terminal && obs.Episode != nil {
		return episodeRecordFromSummary(*obs.Episode, episode.EpisodeKindAnswer, targetRef, nil, obs.Diagnostic)
	}
	reason := episodeMissingSummaryReason
	if !terminal {
		reason = "answer job did not reach a terminal phase within the manager-side watch window"
	}
	return episodeGapRecord(episode.EpisodeKindAnswer, targetRef, nil,
		"gap-"+episodeIDSafe(string(handle)),
		episodeGapReason(reason, obs.Diagnostic), time.Now().UTC())
}

// ---------------------------------------------------------------------------
// Episode record composition — shared by the workflow-side capture
// (coauthorphase2artifact.go) and the answer-job watch above.
// ---------------------------------------------------------------------------

// designBranch PROMOTED to projectstate.DesignBranch (code-health-phase-bd task D3) —
// byte-identical pure resolver, no longer duplicated with systemdesign's twin.

// ===========================================================================
// Dispatch inputs (C-WF-DESIGN workflow_dispatch schema). These exact key names are
// the binding contract with aiarch-design.yml's workflow_dispatch.inputs.
// idempotency_token is RA-controlled and is NOT set here.
// ===========================================================================

// reviewledger.go holds the durable review-ledger seam for the projectDesign Manager
// (review-ledger feature, founder-ratified 2026-07-05) — the structural twin of the
// systemDesign Manager's reviewledger.go. Ledger STORAGE + transition rules live in
// projectstate (reviewthread.go); this is the Manager-side wiring: the ReviewComment ↔
// ReviewCommentView projection and the open-comment gate. The SetReviewCommentStatus /
// SeedReviewComments branch-mutation Activities MIGRATED (B9) onto the generated
// designSessionAccess.setReviewCommentStatusOnBranch / seedReviewCommentsOnBranch
// invokers (invokers.gen.go, reached via wf.Acts) — the ledger-extension fallback those
// custom bodies ran now lives inside the RA (projectstate/designsession.go).

// ---------------------------------------------------------------------------
// Shared Temporal identity constants (projectDesignManager.md §6.1/§6.2/§6.5).
// TaskQueue is defined in the generated worker.gen.go.
// ---------------------------------------------------------------------------

// Signal and query names (contract §6.5).
const (
	// pdSignalReviewDecision resumes a suspended CoAuthorPhase2ArtifactWorkflow at
	// the per-artifact AwaitingReview gate; backs submitReviewDecision (OQ-3).
	pdSignalReviewDecision = "reviewDecision"
	// pdSignalSetCommentStatus resumes a suspended CoAuthorPhase2ArtifactWorkflow at the
	// AwaitingReview gate to apply a durable review-ledger status transition
	// (open|answered->resolved / resolved->open) to one comment on the session branch; backs
	// SetReviewCommentStatus (review-ledger feature).
	pdSignalSetCommentStatus = "setCommentStatus"
	// pdSignalRedraft resumes a CoAuthorPhase2ArtifactWorkflow that landed in the
	// ProjectStageDraftFailed recovery gate (a terminal Phase-2 design-job failure). It
	// re-enters the dispatch loop in the SAME live workflow so the user's "Retry
	// draft" recovers without a fresh run. Backs requestArtifactDraft's retry path
	// (signal-with-start; projectDesignManager.md §2.1 / §0.5.4).
	pdSignalRedraft = "redraft"
	// pdSignalSDPDecision resumes the AssembleSDPReviewWorkflow at the option-commit
	// gate; backs submitSDPDecision.
	pdSignalSDPDecision = "sdpDecision"
	// pdQuerySessionState returns a ProjectSessionStateView; backs getSessionState.
	pdQuerySessionState = "sessionState"
)

// ExecutionKinds for the durable-execution control plane (contract §6.2).
const (
	// pdExecutionKindCoAuthor is the per-artifact Phase-2 co-authoring gate.
	pdExecutionKindCoAuthor = "projectDesignCoAuthor"
	// pdExecutionKindSDPReview is the UC2 SDP-review assembly + option-commit gate.
	pdExecutionKindSDPReview = "projectDesignSDPReview"
	// pdExecutionKindPhaseAdvance is the short-lived Phase-2 seal gating workflow.
	pdExecutionKindPhaseAdvance = "projectDesignPhaseAdvance"
)

// pdWorkflows is the single projectDesignManager component struct — the workflow
// receiver. It carries ZERO custom Temporal Activities and NO I/O ResourceAccess dep
// (B9 + its follow-up ruling): every RA op is a GENERATED activity reached through the
// typed invoker surface (Acts), and the last custom Activity
// (StageArtifactForReviewActivity) was deleted when the designSessionAccess Stage op's
// model param became the codable ModelEnvelope at the schema.
//
//   - Estimation, OperationEst, Settlement are PURE, deterministic Engines, so the
//     workflow body calls their verbs DIRECTLY — replay-safe, no Activity wrapper.
//     They STAY server-side in-workflow (§0.5.5 "RETAINED, unchanged"): they are
//     by-value joins, NOT LLM work, and do NOT become agentic dispatches.
//
// 2026-06-15 agentic-pivot re-cut (projectDesignManager.md §0.5 / D-MPD-Δ): the
// Phase-2 plan-DRAFTING mechanism flips from a synchronous worker call to an ASYNC
// dispatch → observe → read-back round-trip. The per-artifact CoAuthorPhase2-
// ArtifactWorkflow no longer calls workerAccess.GenerateTypedData in-process; instead
// the Manager DISPATCHES a claude-code-action DESIGN job via the generated
// agenticJobAccess submit/observe activities, OBSERVES it to a typed terminal
// phase, and READS BACK the typed model the Action committed via the generated
// designSessionAccess.readProjectOnBranch activity. aiarch makes NO synchronous LLM
// call and writes NO draft JSON on the main path.
//
// DROPPED from the draft path (§0.5.5): workerAccess (no synchronous LLM call
// survives; the in-flight cancel is agenticJobAccess.cancel) and
// artifactValidationEngine (Phase-2 validation is the required CI check inside the
// Action, surfaced as the job's terminal phase). Both are removed from this struct.
type pdWorkflows struct {
	Estimation   estimation.EstimationEngine
	OperationEst operationestimation.OperationEstimationEngine
	Settlement   billing.BillingEngine

	// Acts is the GENERATED typed invoker surface (invokers.gen.go) — the workflow's call
	// surface for EVERY contract-backed RA op: projectStateAccess readProjectVersion /
	// advancePhase, the agenticJobAccess submit/observe design-job pair, the
	// seven sourceControlAccess PR-rail verbs, and the eight designSessionAccess
	// branch-session verbs. Each invoker consults the manager's per-op preset hook
	// (workermanifest.go pdActivityOptions), keyed by the generated activity name.
	Acts genInvokers

	// Rail + Repo are the OPTIONAL git-forward PR rail (I-DESIGN-DISPATCH §2b). When both
	// are non-nil AND a repo resolves, the per-artifact CoAuthorPhase2ArtifactWorkflow
	// draft path wraps each draft in the settled branch→PR→read-back→+1→merge model + the
	// branch-aware read-back/stage; when nil that path runs UNCHANGED (read-back/stage on
	// main, no branch/PR ops). The AssembleSDPReviewWorkflow (the in-workflow three-Engine
	// join) is UNCHANGED — it gets NO rail (only the per-artifact draft path does).
	//
	// Rail is the PUBLISHED sourceControlAccess RA. The rail verbs are reached through
	// the generated invoker surface (wf.Acts.Rail*); this field is held directly ONLY for
	// the nil/dormant gate (gitEnabled).
	Rail sourcecontrol.SourceControlAccess
	// Repo resolves the per-project RepoRef the rail verbs address. nil ⇒ the rail is
	// dormant. Injected so the repo-resolution policy is swappable without a new RA edge.
	Repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)
}

// Activity option presets (contract §6.4). Concrete RetryPolicy / timeout choices live
// here, in the Manager. Each preset is an ActivityOptions VALUE consumed by the
// generated-invoker option hook in workermanifest.go, keyed by the generated activity
// name. This Manager has ZERO custom Temporal Activities (B9 follow-up — the last one,
// StageArtifactForReviewActivity, was deleted when the contract op's model param became
// the codable ModelEnvelope), so no ctx-wrapper forms remain.

// pdReadProjectActivityOptions is the preset for the read path: since B9 this is EXCLUSIVELY
// the generated designSessionAccess.readProjectOnBranch invoker (both branch=="" main reads
// and branch-aware reads funnel through it), plus the generated
// projectStateAccess.readProjectVersion. Both are keyed onto this VALUE via the
// workermanifest.go option hook.
func pdReadProjectActivityOptions() workflow.ActivityOptions {
	// BOUND the read retries so a RETRYABLE fault (Transient / Infrastructure /
	// RateLimited) cannot loop forever — decode failures of committed state are now
	// TERMINAL (ContractMisuse, below), but a genuine persistent infra outage must
	// still surface rather than wedge invisibly (QA F36, mirrors systemdesign).
	return fwmanager.ActivityPreset{
		Timeout:     10 * time.Second,
		MaxAttempts: 8,
		TerminalRA:  []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// sdpReviewWorkflowID derives the continuity token: {projectId}:sdpReview.
func sdpReviewWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:sdpReview", projectID)
}

// pdPhaseAdvanceWorkflowID derives the continuity token: {projectId}:phaseAdvance:projectDesign.
// See systemDesign's pdPhaseAdvanceWorkflowID for why the rail suffix is load-bearing —
// the two Managers derived the same string before stage 4a put them on one task queue.
func pdPhaseAdvanceWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:phaseAdvance:projectDesign", projectID)
}

// ---------------------------------------------------------------------------
// Internal helpers (deterministic; no clock, no RNG).
// ---------------------------------------------------------------------------

// pdCoAuthorState is the live technical state backing the sessionState Query. Reused
// (in a slightly fuller form) by both the per-artifact and the SDP-review pdWorkflows.
type pdCoAuthorState struct {
	projectID    ProjectID
	artifactKind ArtifactKind
	stage        ProjectSessionStage
	draft        projectstate.ArtifactModel
	findings     []Finding
	headVersion  projectstate.Version
	// failureReason is set only on ProjectStageDraftFailed: the neutral job Diagnostic, the
	// human "why" for the SPA's retry/withdraw screen (the anti-wedge requirement).
	failureReason string
	// reviewThread is the durable review ledger for this artifact (review-ledger feature),
	// refreshed from the session branch after every (re)stage and every resolve/reopen so the
	// query + approve gate see the live thread. Nil until a read-back carries comments.
	reviewThread []projectstate.ReviewComment
	// policyAutoApprove and vibesAutogateEnabled drive the VIBES AUTOGATE (F-R3 vibes-everywhere,
	// founder-ratified): when this session's committed ReviewPolicy preset is "vibes"
	// (policyAutoApprove), the review gate AUTO-APPROVES a clean draft (no open change-requests)
	// instead of waiting for a human — honoring ReviewPolicy exactly like construction. Both are
	// snapshot ONCE at session start (coAuthorSessionSetup): policyAutoApprove from the head-state
	// ReviewPolicy.Preset, vibesAutogateEnabled from the "design-vibes-autogate" GetVersion gate
	// (an in-flight session replays DefaultVersion → autogate OFF → the human gate for its whole
	// run). See the review-gate loop in CoAuthorPhase2ArtifactWorkflow.
	policyAutoApprove    bool
	vibesAutogateEnabled bool
	// roundLedgerEnabled drives the ROUND-LEDGER DUAL-WRITE (stage 3 task 6): resolved ONCE
	// at session start from the "design-round-ledger" GetVersion fence, exactly like
	// vibesAutogateEnabled above, so a session in flight at deploy time replays
	// DefaultVersion and runs its WHOLE life on the old command sequence. See the round
	// ledger section of coauthorphase2artifact.go for what the dual-write records and why
	// the slot write stays.
	roundLedgerEnabled bool
	// roundReviewers is the ROSTER the review engine computed for this session's design
	// gate, snapshot at session start beside policyAutoApprove. On THIS rail the engine
	// returns no agent reviewers at all (the plan is computed, not drafted), so the roster
	// is the human row alone whenever the policy holds for a person.
	roundReviewers []projectstate.RoundReviewer
	// roundBase is the highest round number the DURABLE ledger already holds for this
	// session's artifact kind at its gate, read ONCE at session start
	// (seedRoundBaseFromLedger) and never changed after. Every round this session opens is
	// numbered above it, which is what stops a second session of the same kind re-minting
	// the first session's round ids. Zero for a first session, for a kind whose lifecycle
	// carries no review task — which today is every Phase-2 kind — and whenever the read
	// could not be made.
	roundBase int
	// ledgerVersion is the optimistic-concurrency token for the MAIN-side execution ledger.
	// It is deliberately NOT headVersion: headVersion tracks whichever substrate the design
	// session is writing (the session branch while a draft is staged), whereas the round
	// ledger lives on main like construction's, so the two genuinely differ during the
	// review window. Seeded from the session-start main read and advanced by each round
	// write; applyRecovering re-reads main on any drift.
	ledgerVersion projectstate.Version
	// activityVersion is the PER-ACTIVITY CAS token: the version the design activity's own
	// execution row was at the last time this session wrote it. ledgerVersion is the whole
	// document's token; this one is scoped to the row, so two sessions writing DIFFERENT
	// activities never contend while two writing the SAME row cannot interleave.
	//
	// Seeded from the row seedRoundBaseFromLedger already reads, and advanced by one per
	// APPLIED transition (the store stamps exactly one). 0 —
	// projectstate.NoActivityVersionExpectation — for a first session, whose OpenActivity
	// births the row, and for every session whose seed read could not be made: those write
	// no round either way.
	activityVersion int64
	// activityOpened records that this session has already birthed the design activity's
	// execution row. OpenActivity is idempotent, so this saves a command rather than
	// guarding correctness.
	activityOpened bool
	// round is the review round currently OPEN at the human gate — empty between gates, and
	// empty for a kind whose lifecycle carries no review task, which today is every Phase-2
	// kind (see the round ledger section). Every write point treats an empty round as
	// "write nothing".
	round designRound
	// roundComments pairs a SLOT comment id with where the same comment landed on the round
	// ledger, so a resolve / reopen filed against the slot's id can be mirrored.
	roundComments map[string]roundCommentRef
	// resumeFromReadBack is the F35-twin checkpoint: set true when a POST-read-back rail step
	// (openPR) faulted and the session landed at the failed gate WITH the draft already
	// committed on the branch. On the next Retry the draft round consumes it and RESUMES from
	// the read-back — SKIPPING the re-dispatch — so it does not redispatch Claude onto a branch
	// that already carries the model (which the no-commit guard would red). Workflow-local,
	// deterministic on replay (set from a recorded Activity error, never wall-clock).
	resumeFromReadBack bool
	// feedbackSeeded reports whether the CURRENT contents of the workflow's feedback variable
	// are already durably in the review ledger. The review-gate REJECT and the AMENDMENT seed
	// fold their feedback into the ledger themselves (pdFeedbackToLedgerComments / seedAmendment
	// Ledger), so they set this true. The MEMORY-ONLY failed-gate paths — a redraft-signal
	// (F47), a Retry-via-Reject AT a failed gate, a faulted reject — only retain the feedback in
	// this workflow variable, so they set it false. Under thin dispatch the drafting agent reads
	// context ONLY via getReviewThread, so before each redraft dispatch a false flag triggers
	// seedFailedGateFeedback to seed the retained anchored comments, while a true flag skips it
	// so an already-seeded path is never double-seeded.
	feedbackSeeded bool
	// decisionSeq counts the review decisions HANDLED at the AwaitingReview gate — one
	// monotonic increment per received reviewDecision signal (F-QA2-44, systemdesign twin
	// parity). Replay-stable: driven purely by the recorded signal order. It keys the
	// per-attempt version gate (gate-decision-token-remint-p2-<seq>) guarding the approve
	// arm's gate-time credential re-mint; see coAuthorApprove for why that gate is
	// per-attempt rather than static. Stays zero for AssembleSDPReviewWorkflow (no gate).
	decisionSeq int
	// activeRole / activeStep / activeRound are the WORKFLOW-LOCAL sub-step indicator
	// backing the honest role-driven loading pill (Plan-3 C2, mirroring systemdesign's C1).
	// They are SET immediately before each dispatch boundary (architect drafting/revising —
	// Phase 2 has NO PM critique, so ActiveRoleProductManager / ActiveStepCritiquing are
	// never stamped here) and CLEARED to none/none/0 the instant that dispatch is observed
	// complete or the session reaches any terminal / AwaitingReview stage. Pure in-workflow
	// state served by view() (NOT boundary-stamped like StageName) — setting it issues NO
	// Temporal history command, so no GetVersion gate is needed (the honesty invariant). Also
	// reused, always at its zero value, by AssembleSDPReviewWorkflow — the SDP assembly is
	// server-side (no role/step to stamp), so its view() naturally reports none/none/0.
	activeRole  ActiveRole
	activeStep  ActiveStep
	activeRound int
}

// markActive stamps the in-flight sub-step (role / step / round) the loading pill renders.
// Pure workflow-local state; no history command.
func (s *pdCoAuthorState) markActive(role ActiveRole, step ActiveStep, round int) {
	s.activeRole = role
	s.activeStep = step
	s.activeRound = round
}

// clearActive resets the sub-step to none/none/0 — the honest "no role is working" state
// the pill falls back to today's plain "DRAFTING…" copy for. Called on observed dispatch
// completion and on every terminal / AwaitingReview stage.
func (s *pdCoAuthorState) clearActive() {
	s.activeRole = ActiveRoleNone
	s.activeStep = ActiveStepNone
	s.activeRound = 0
}

func (s *pdCoAuthorState) view() (ProjectSessionStateView, error) {
	dm, err := draftModelFor(s.artifactKind, s.draft)
	if err != nil {
		return ProjectSessionStateView{}, err
	}
	return ProjectSessionStateView{
		ProjectID:     s.projectID,
		ArtifactKind:  s.artifactKind,
		Stage:         s.stage,
		Draft:         dm,
		Findings:      s.findings,
		FailureReason: strPtrOrNil(s.failureReason),
		ReviewThread:  reviewThreadToView(s.reviewThread),
		ActiveRole:    s.activeRole,
		ActiveStep:    s.activeStep,
		Round:         int64(s.activeRound),
	}, nil
}

// slotAccessors is the flat kind→slot dispatch behind pdSlotFor, in table form so the
// exhaustive gate (check: map) still fails on a missing ArtifactKind exactly like the
// former switch did.
var slotAccessors = map[projectstate.ArtifactKind]func(projectstate.Project) projectstate.ArtifactSlot{
	projectstate.KindMission:              func(p projectstate.Project) projectstate.ArtifactSlot { return p.Mission },
	projectstate.KindGlossary:             func(p projectstate.Project) projectstate.ArtifactSlot { return p.Glossary },
	projectstate.KindScrubbedRequirements: func(p projectstate.Project) projectstate.ArtifactSlot { return p.ScrubbedRequirements },
	projectstate.KindVolatilities:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.Volatilities },
	projectstate.KindCoreUseCases:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.CoreUseCases },
	projectstate.KindSystem:               func(p projectstate.Project) projectstate.ArtifactSlot { return p.SystemDesign },
	projectstate.KindOperationalConcepts:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.OperationalConcepts },
	projectstate.KindStandardCheck:        func(p projectstate.Project) projectstate.ArtifactSlot { return p.StandardCheck },
	projectstate.KindPlanningAssumptions:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.PlanningAssumptions },
	projectstate.KindActivityList:         func(p projectstate.Project) projectstate.ArtifactSlot { return p.ActivityList },
	projectstate.KindNetwork:              func(p projectstate.Project) projectstate.ArtifactSlot { return p.Network },
	projectstate.KindNormalSolution:       func(p projectstate.Project) projectstate.ArtifactSlot { return p.NormalSolution },
	projectstate.KindSubcriticalSolution:  func(p projectstate.Project) projectstate.ArtifactSlot { return p.SubcriticalSolution },
	projectstate.KindCompressedSolution:   func(p projectstate.Project) projectstate.ArtifactSlot { return p.CompressedSolution },
	projectstate.KindDecompressedSolution: func(p projectstate.Project) projectstate.ArtifactSlot { return p.DecompressedSolution },
	projectstate.KindRiskModel:            func(p projectstate.Project) projectstate.ArtifactSlot { return p.RiskModel },
	projectstate.KindSdpReview:            func(p projectstate.Project) projectstate.ArtifactSlot { return p.SdpReview },
}

// pdSlotFor returns the named Project slot for a kind (Phase 1 + Phase 2). Internal
// (operates on the canonical projectstate.ArtifactKind); own-kind callers convert via
// toPSKind at the boundary. An unknown kind yields the zero slot, as before.
func pdSlotFor(proj projectstate.Project, kind projectstate.ArtifactKind) projectstate.ArtifactSlot {
	accessor, ok := slotAccessors[kind]
	if !ok {
		return projectstate.ArtifactSlot{}
	}
	return accessor(proj)
}

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the projectDesignManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the three workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go). This Manager has ZERO
// custom Temporal Activities (B9 + its follow-up ruling: the last one,
// StageArtifactForReviewActivity, was deleted when the designSessionAccess Stage op's
// model param became the codable ModelEnvelope at the schema) — every Activity is
// generated and registered by the generated RegisterWorker.
//
// The three estimate Engines (Estimation / OperationEst / Settlement) are called DIRECTLY
// in-workflow (deterministic, by value) and are NOT Activities; the durable-execution
// in-workflow primitives (awaitSignal / startTimer) are the Manager's own code.

// pdActivityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities (projectState / pipeline / rail / designSession). A name
// with no entry falls back to the generated default (invokers.gen.go). Keyed by the
// generated registered activity name (<componentKey>.<opName>); every
// designSessionAccess.* entry below uses the same readProjectOpts/mutateOpts preset
// as the equivalent projectStateAccess entry.
func pdActivityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"projectStateAccess.readProjectVersion":                  pdReadProjectActivityOptions(),
		"projectStateAccess.advancePhase":                        mutateActivityOptions(),
		"agenticJobAccess.submitAgenticJob":                      dispatchActivityOptions(),
		"agenticJobAccess.observeAgenticJob":                     observeActivityOptions(),
		"sourceControlAccess.getInstallationToken":               mintCredActivityOptions(),
		"sourceControlAccess.openBranch":                         railActivityOptions(),
		"sourceControlAccess.openPullRequest":                    railActivityOptions(),
		"sourceControlAccess.getPullRequestStatus":               railActivityOptions(),
		"sourceControlAccess.postReview":                         railActivityOptions(),
		"sourceControlAccess.mergePullRequest":                   railActivityOptions(),
		"sourceControlAccess.syncManagedScaffold":                railActivityOptions(),
		"designSessionAccess.readProjectOnBranch":                pdReadProjectActivityOptions(),
		"designSessionAccess.stageArtifactForReviewOnBranch":     mutateActivityOptions(),
		"designSessionAccess.commitArtifactWithProvenance":       mutateActivityOptions(),
		"designSessionAccess.rejectArtifactOnBranchWithComments": mutateActivityOptions(),
		"designSessionAccess.withdrawArtifactOnBranch":           mutateActivityOptions(),
		"designSessionAccess.setReviewCommentStatusOnBranch":     mutateActivityOptions(),
		"designSessionAccess.seedReviewCommentsOnBranch":         mutateActivityOptions(),
		// The ROUND-ledger dual-write (stage 3 task 6). Every one is a head-state mutation
		// through the same applyMutation funnel the designSession verbs ride, so it takes
		// the same envelope: the workflow's own Conflict re-read loop (applyRecovering) is
		// what resolves a CAS loss, not a longer retry here.
		"activityExecutionAccess.openActivity":           mutateActivityOptions(),
		"activityExecutionAccess.openReviewRound":        mutateActivityOptions(),
		"activityExecutionAccess.appendReviewVerdict":    mutateActivityOptions(),
		"activityExecutionAccess.decideReviewRound":      mutateActivityOptions(),
		"activityExecutionAccess.setReviewCommentStatus": mutateActivityOptions(),
		// SP1 capture-seam: the episode ledger append rides its OWN envelope, never a
		// business one (see appendEpisodeActivityOptions).
		"episodeAccess.appendEpisode": appendEpisodeActivityOptions(),
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
// The pdWorkflows receiver holds the generated invoker surface (Acts) — every
// contract-backed RA op (readProjectVersion / advancePhase / submit / observe / the
// seven rail verbs / the eight designSession verbs) is reached through it; the receiver
// carries no RA dep of its own.
func (m *projectDesignManager) WorkerManifest() genWorkerManifest {
	optsHook := pdActivityOptions()

	wf := &pdWorkflows{
		Estimation:   m.estimator,
		OperationEst: m.opEstimator,
		Settlement:   m.settlement,
		Acts:         genInvokers{Opts: optsHook},
		// Rail is the PUBLISHED sourceControlAccess: nil ⇒ the PR rail is dormant and the
		// CoAuthor draft path runs the original main-path behavior. Held directly for the
		// gitEnabled gate; the seven rail verbs (including syncManagedScaffold, since B9) go
		// through the generated invoker surface (wf.Acts.Rail*).
		Rail: m.rail,
		Repo: m.repo,
	}

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: pdExecutionKindCoAuthor, Fn: wf.CoAuthorPhase2ArtifactWorkflow},
			{Name: pdExecutionKindSDPReview, Fn: wf.AssembleSDPReviewWorkflow},
			{Name: pdExecutionKindPhaseAdvance, Fn: wf.Phase2AdvanceWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:      m.projectState,
			Pipeline:          m.pipeline,
			Rail:              m.rail,
			DesignSession:     m.designSession,
			ActivityExecution: m.activityExecution,
			Episodes:          m.episodes,
		},
	}
}

// ---------------------------------------------------------------------------
// Episode facet read ops (SP1 capture-seam, Task 9 — founder ruling 2026-08-02:
// episode observability is a facet of the existing use cases, not a new
// episodeManager). Both ops are PLAIN METHODS that consult episodeAccess directly
// — no Temporal — the same shape as systemDesignManager.ListProjects/GetProject
// (these facets collapse into one when the ratified DesignManager merge lands).
// The whole-project exportEpisodes op is cut from v1 (per-target export is
// client-side, Task 10).
// ---------------------------------------------------------------------------

// ListEpisodesForArtifact returns every episode record (design/rework runs, or
// gaps) captured against one Project-Design (Phase 2) artifact, in episodeAccess's
// own (append) order. A pass-through over episodeAccess.ListEpisodes scoped by
// TargetRef=artifactKind, mapped to the contract EpisodeRecordView.
func (m *projectDesignManager) ListEpisodesForArtifact(rc fwmanager.Context, projectID ProjectID, artifactKind ArtifactKind) ([]EpisodeRecordView, error) {
	ctx := rc.Context
	if projectID == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	// TargetRef on the ledger is the PascalCase artifactKindString(kind) form —
	// exactly what the capture-seam write path (episodeRecordFromSummary via
	// dispatchAndObserve) stamps as TargetRef, NOT the wire-name form. artifactKind
	// is typed as the contract's own ArtifactKind enum (not a bare string) so this
	// conversion cannot be skipped by a caller that only has the wire name.
	targetRef := artifactKindString(artifactKind)
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{
		ProjectID: episode.ProjectID(projectID),
		TargetRef: &targetRef,
	})
	if err != nil {
		return nil, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	return episodeRecordViews(records), nil
}

// GetEpisodeTimeline returns one episode's full timeline: its ledger record plus
// the sequenced trace events mined from its run. NotFound if episodeID does not
// name a record on this project.
func (m *projectDesignManager) GetEpisodeTimeline(rc fwmanager.Context, projectID ProjectID, episodeID string) (EpisodeTimeline, error) {
	ctx := rc.Context
	if projectID == "" {
		return EpisodeTimeline{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if episodeID == "" {
		return EpisodeTimeline{}, newError(fwmanager.ContractMisuse, "empty episodeId")
	}
	// ListEpisodes has no by-id lookup (episodeAccess.md — the ledger is append-
	// scanned by TargetRef); querying with no TargetRef and finding the one record
	// whose EpisodeID matches is the only way to resolve one episode across every
	// target on the project.
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{ProjectID: episode.ProjectID(projectID)})
	if err != nil {
		return EpisodeTimeline{}, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	rec, ok := findEpisodeRecord(records, episodeID)
	if !ok {
		return EpisodeTimeline{}, newError(fwmanager.NotFound, fmt.Sprintf("episode %q not found", episodeID))
	}
	// A GAP record (episode.EpisodeGap — the dispatch that produced no summary at
	// all) has no trace file: TracePath is nil on the ledger record. The
	// never-silent gap doctrine (Task 2/7) treats a gap as a PRESENT, first-class
	// outcome, not an absence — the record itself must always resolve; only its
	// timeline is empty. Skip the RA round-trip entirely when TracePath says
	// there is nothing to read, and treat a NotFound FROM ReadTraceEvents (e.g. a
	// TracePath that no longer resolves) the same way, rather than erroring the
	// whole timeline — either would otherwise be indistinguishable from an
	// unknown episodeID.
	if rec.TracePath == nil || *rec.TracePath == "" {
		return EpisodeTimeline{Record: episodeRecordToView(rec), Events: episodeTimelineEvents(nil)}, nil
	}
	raw, err := m.episodes.ReadTraceEvents(fwra.Context{Context: ctx}, episode.ProjectID(projectID), episodeID)
	if err != nil {
		if isEpisodeTraceNotFound(err) {
			return EpisodeTimeline{Record: episodeRecordToView(rec), Events: episodeTimelineEvents(nil)}, nil
		}
		return EpisodeTimeline{}, mapRAError(err, "episodeAccess.ReadTraceEvents")
	}
	return EpisodeTimeline{
		Record: episodeRecordToView(rec),
		Events: episodeTimelineEvents(raw),
	}, nil
}

// episodeRecordViews maps a slice of ledger records onto the contract view type.
func episodeRecordViews(records []episode.EpisodeRecord) []EpisodeRecordView {
	out := make([]EpisodeRecordView, 0, len(records))
	for _, r := range records {
		out = append(out, episodeRecordToView(r))
	}
	return out
}

// episodeRecordToView maps one episodeAccess ledger record onto this contract's
// OWN copy of the view shape (EpisodeRecordView mirrors episodeAccess.EpisodeRecord
// field-for-field; contracts are self-contained, so this is an intentional
// duplicate of the mapping episodeAccess itself owns, not a shared function).
func episodeRecordToView(r episode.EpisodeRecord) EpisodeRecordView {
	v := EpisodeRecordView{
		EpisodeID:      r.EpisodeID,
		Kind:           episodeViewKind(r.Kind),
		TargetRef:      r.TargetRef,
		WorkerClass:    r.WorkerClass,
		Model:          r.Model,
		Usage:          EpisodeUsage(r.Usage),
		CostUSD:        r.CostUSD,
		NumTurns:       r.NumTurns,
		ToolCallCounts: r.ToolCallCounts,
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		Outcome:        episodeViewOutcome(r.Outcome),
		GapReason:      r.GapReason,
		TracePath:      r.TracePath,
	}
	if r.Lineage != nil {
		l := EpisodeLineage(*r.Lineage)
		v.Lineage = &l
	}
	if r.StreamedUsage != nil {
		u := EpisodeUsage(*r.StreamedUsage)
		v.StreamedUsage = &u
	}
	if len(r.SubagentSpans) > 0 {
		spans := make([]SubagentSpan, 0, len(r.SubagentSpans))
		for _, s := range r.SubagentSpans {
			spans = append(spans, SubagentSpan(s))
		}
		v.SubagentSpans = spans
	}
	return v
}

// Stage 4a: ONE copy now serves the projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// episodeViewOutcome maps the episodeAccess RA's Outcome onto this contract's own
// copy of the enum. Same total-switch rationale as episodeViewKind.
func episodeViewOutcome(o episode.EpisodeOutcome) EpisodeOutcome {
	switch o {
	case episode.EpisodeSucceeded:
		return EpisodeSucceeded
	case episode.EpisodeFailed:
		return EpisodeFailed
	case episode.EpisodeCancelled:
		return EpisodeCancelled
	case episode.EpisodeGap:
		return EpisodeGap
	default:
		// Unreachable for the four defined episode.EpisodeOutcome values above;
		// defensive fallback for an out-of-range ordinal (the "gap" reading is the
		// safe direction).
		return EpisodeGap
	}
}

// Stage 4a: ONE copy now serves the projectDesign+construction rails (byte-identical
// twins, collapsed by the package merge — arch.CheckFileLayout allows one impl file and one test file).
// mapRAError translates an episodeAccess error into the Manager façade error
// model. fwra.NotFound → NotFound; fwra.ContractMisuse → ContractMisuse;
// everything else → Infrastructure with the original retryability preserved.
// label identifies the actual failing op (the opaque Detail returned to the
// client; the full cause chain stays server-side on Cause). Mirrors
// systemDesignManager's mapRAError of the same name (I4: accepted triplication —
// the ratified DesignManager merge collapses this copy).
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
	// A non-fwra error still carries its cause for the server log while keeping
	// the client Detail opaque (label).
	return fwmanager.Wrap(fwmanager.Infrastructure, err, label)
}

// --- Phase-2 activity-list derivation: the projectstate <-> estimation conversion
// boundary (Option B full encapsulation: the estimation Engine redefines every domain
// type it uses as its own generated def and imports NO projectstate, so the Manager
// maps field-by-field here — exactly as toEstimationOption above does for
// EstimateForOption). Task 10 wires toEstimationSystemView + toProjectStateActivityList
// into the read path; DerivePlan itself (Task 5) and its call are exercised there. ---

// derefString returns *p, or fallback when p is nil or points at the empty string —
// the shared "unauthored optional -> concrete default" rule this boundary applies to
// every optional projectstate.Component attribute the Engine needs resolved.
func derefString(p *string, fallback string) string {
	if p == nil || *p == "" {
		return fallback
	}
	return *p
}

// derefBool returns *p, or false when p is nil — projectstate's UiSurface is an
// unauthored-means-absent tri-state; the Engine only ever sees a resolved bool.
func derefBool(p *bool) bool { return p != nil && *p }

// toEstimationSystemView converts the canonical System to the estimation Engine's OWN
// slim SystemView at the call boundary. Only what the derivation reads crosses:
// identity, kind, and the four typed doctrine attributes (constructionProfile,
// provisioning, uiSurface, buildStatus) — the fourth of which decides whether there is
// anything to build at all.
//
// An unauthored constructionProfile defaults to "handwritten" — the CONSERVATIVE
// direction. Defaulting to "generated" would silently delete real planned work, which
// is the one failure mode this whole derivation design exists to prevent. An
// unauthored provisioning defaults to "owned" (no vendor assumed). An unauthored
// buildStatus defaults to "" — the same conservative direction: only an explicit
// "planned" says the component has no code to build yet.
func toEstimationSystemView(sys projectstate.System) estimation.SystemView {
	comps := make([]estimation.SystemComponent, 0, len(sys.Components))
	for _, c := range sys.Components {
		comps = append(comps, estimation.SystemComponent{
			ID:                  c.ID,
			Name:                c.Name,
			Kind:                c.Kind.String(),
			ConstructionProfile: derefString(c.ConstructionProfile, "handwritten"),
			Provisioning:        derefString(c.Provisioning, "owned"),
			UiSurface:           derefBool(c.UiSurface),
			BuildStatus:         derefString(c.BuildStatus, ""),
		})
	}
	rels := make([]estimation.SystemRelationship, 0, len(sys.Relationships))
	for _, r := range sys.Relationships {
		rels = append(rels, estimation.SystemRelationship{From: r.From, To: r.To})
	}
	return estimation.SystemView{Components: comps, Relationships: rels}
}

// toProjectStateActivityList converts the Engine's derived plan back to the canonical
// ActivityList shape every existing reader already consumes (the SPA catalog
// projection, the construction pump, earned value). Only the Activities cross here —
// DerivedPlan's Dependencies/Milestones feed the Network artifact, not this slot.
func toProjectStateActivityList(plan estimation.DerivedPlan) projectstate.ActivityList {
	acts := make([]projectstate.ActivityItem, 0, len(plan.Activities))
	for _, a := range plan.Activities {
		acts = append(acts, projectstate.ActivityItem{
			Name:        a.Name,
			Title:       a.Title,
			EffortDays:  a.EffortDays,
			RiskBucket:  int(a.RiskBucket),
			WorkerClass: a.WorkerClass,
			Coding:      a.Coding,
			ComponentID: a.ComponentID,
		})
	}
	return projectstate.ActivityList{Activities: acts}
}

// toProjectStateMilestones converts the Engine's derived milestones to the projectstate
// shape (I1, 2026-08-10). Only Id/DependsOn cross here — Name and Public are display-only
// authoring decorations with no derivation source (DerivedPlan carries no opinion on
// them, same as AdditiveMilestone in the delta vocabulary), so they are left at their
// zero value; a caller rendering these for a human MUST overlay the existing committed
// Name/Public rather than take them from here. What this DOES make comparable — the
// thing the drift gate (TestDerivedPlanMatchesCommittedState) actually needs — is the
// STRUCTURE: which milestones exist and what they depend on.
func toProjectStateMilestones(plan estimation.DerivedPlan) []projectstate.NetworkMilestone {
	out := make([]projectstate.NetworkMilestone, 0, len(plan.Milestones))
	for _, m := range plan.Milestones {
		out = append(out, projectstate.NetworkMilestone{ID: m.Id, DependsOn: m.DependsOn})
	}
	return out
}

// MaterializeActivityPlan is the single render-on-read entry point for the Phase-2 plan:
// it derives the baseline from the committed System, applies the authored deltas, and
// returns the canonical shapes every existing reader already consumes.
//
// No core-use-case ids are consumed here: the founder's 2026-08-09 ruling drops I-*
// integration activities entirely (App A makes integration a phase of every activity's
// own lifecycle, so a separate I-* would charge the same work twice), which was the only
// thing that ever read them. SystemView.CoreUseCaseIDs is gone from the contract
// (Task 10a); do not resurrect a use-case-id parameter here to feed it.
//
// Milestones are returned alongside activities/dependencies (I1, 2026-08-10) so a caller
// — chiefly the derived-plan drift gate — can compare the FULL derived network, not just
// the activity list: before this, a slot-5 relationship edit that only shifted milestone
// fan-in re-derived nothing the gate could see.
//
// A delta that violates the vocabulary fails the READ, loudly. Silently dropping a bad
// delta would be the zombie failure mode returning by another door.
func MaterializeActivityPlan(
	sys projectstate.System,
	deltas estimation.ActivityListDeltas,
) (projectstate.ActivityList, []projectstate.NetworkDependency, []projectstate.NetworkMilestone, error) {
	view := toEstimationSystemView(sys)

	plan, err := estimation.NewEstimationEngine().DerivePlan(fweng.Context{}, view, deltas)
	if err != nil {
		return projectstate.ActivityList{}, nil, nil, err
	}

	deps := make([]projectstate.NetworkDependency, 0, len(plan.Dependencies))
	for _, d := range plan.Dependencies {
		deps = append(deps, projectstate.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	return toProjectStateActivityList(plan), deps, toProjectStateMilestones(plan), nil
}

// materializePhase2Draft is the PRODUCTION caller of MaterializeActivityPlan — the
// render-on-read `the-method-activity-list` mandates: "the server applies the deltas onto
// the derived baseline (DerivePlan) and stages the result (StageArtifactForReview)". For
// KindActivityList and KindNetwork it REPLACES the model the drafting agent committed on
// the session branch with the plan derived from the COMMITTED System (slot 5); every other
// kind passes through byte-identical, so this is inert for the other fifteen slots.
//
// DerivePlan yields three things and all three are written: slot 9 takes the activities,
// slot 10 takes the dependencies and milestones (see materializeNetwork). Until
// 2026-09-12 the dependencies and milestones were discarded here, so slot 10 was whatever
// the drafting agent typed, kept honest only by the CI drift gate — a real first run
// would not have staged the derived network.
//
// Without it slot 9 is whatever the agent typed, and on a freshly drafted project that is
// `{"activities": null}` — after which assembleSdpReview dies with "option network has
// zero activities" and the construction pump has nothing to dispatch. The one project
// that DOES hold a materialized slot 9 (this repo's own) got there by a hand backfill;
// this is that backfill made mechanical.
//
// DELTAS ARE EMPTY, AND THAT IS A KNOWN CONTRACT GAP, not an oversight. The wire model for
// this slot is projectstate.ActivityList — `activities` and nothing else — so the authored
// ActivityListDeltas vocabulary (overrides / additive / additiveMilestones) has no way to
// reach here: an agent that commits a deltas document has its unknown fields dropped by
// the codec and lands the empty list above. The authored deltas live in the root-level
// `activityListOverrides` sibling, which projectDoc does not carry, so the Manager cannot
// read them either (2026-08-10 spec amendment §1). Deriving deltas by DIFFING the drafted
// list against the baseline is deliberately NOT done — that would silently re-bless the
// hand-typed materialized list the doctrine forbids and re-open the zombie-activity door
// the derivation closed. Closing the gap needs a contract change to the slot's model.
func materializePhase2Draft(
	proj projectstate.Project,
	kind projectstate.ArtifactKind,
	draft projectstate.ArtifactModel,
) (projectstate.ArtifactModel, error) {
	if kind != projectstate.KindActivityList && kind != projectstate.KindNetwork {
		return draft, nil
	}

	sysSlot := pdSlotFor(proj, projectstate.KindSystem)
	if sysSlot.Status != projectstate.ReviewCommitted || sysSlot.Model == nil {
		return nil, fwmanager.New(fwmanager.FailedPrecondition,
			"cannot materialize the Phase-2 plan: the systemDesign slot is not committed — the whole Phase-2 plan derives from it, so Phase 1 must be approved before a plan is staged")
	}
	sys, ok := sysSlot.Model.(*projectstate.System)
	if !ok {
		return nil, wrongModelType(projectstate.KindSystem, sysSlot.Model)
	}

	list, deps, milestones, err := MaterializeActivityPlan(*sys, estimation.ActivityListDeltas{})
	if err != nil {
		return nil, fwmanager.Wrap(fwmanager.FailedPrecondition, err,
			"cannot materialize the Phase-2 plan from the committed systemDesign")
	}
	// LOUD, never a silent empty list: a System with no components derives no activities
	// from the architecture at all. Staging that would reproduce exactly the zero-activity
	// failure this seam exists to prevent, one phase later and with no trace of where it
	// came from.
	//
	// The condition is the COMPONENT count, not len(list.Activities): the derivation emits
	// the design prefix (requirements/architecture/projectDesign) for every system
	// including the empty one — a project before its architecture is committed still owes
	// the work that produces it — so the activity list is never empty and the old form of
	// this guard would now be dead code. It is the same reachable rule stated where it is
	// still true: the empty System was always the only input that produced an empty list
	// (a fully suppressed architecture still derives N-STP and N-IT).
	if len(sys.Components) == 0 {
		return nil, fwmanager.New(fwmanager.FailedPrecondition,
			"the committed systemDesign holds ZERO components, so it derives ZERO construction activities — fix slot 5 before staging a plan")
	}
	if kind == projectstate.KindActivityList {
		return &list, nil
	}

	authored, ok := draft.(*projectstate.Network)
	if !ok {
		return nil, wrongModelType(projectstate.KindNetwork, draft)
	}
	var authoredNet projectstate.Network
	if authored != nil {
		authoredNet = *authored
	}
	net, err := materializeNetwork(list, deps, milestones, authoredNet)
	if err != nil {
		return nil, err
	}
	return &net, nil
}

// materializeNetwork builds slot 10 from a derived plan. Dependencies and the milestone
// SET with its fan-in are the derivation's, verbatim — a milestone the draft authored but
// the derivation does not produce is dropped, and M0 keeps the [projectDesign] fan-in the
// derivation gives it (the SDP review is what that activity ends with), never whatever
// the draft typed.
// Each milestone's Name and Public are carried across from the authored network by id:
// they are display decorations with no derivation source (see toProjectStateMilestones).
// criticalPath is recomputed by ComputeNetwork over the derived graph and written as the
// alphabetically-sorted zero-float activity set (projectstate.Network.CriticalPath).
//
// A derived milestone with no authored decoration matching its id — the drafting agent
// omitted it, or typo'd the id — has no Name to carry across. NetworkMilestone.Name has no
// non-emptiness check anywhere else, and the founder ruled (2026-08-13) that non-emptiness
// is enforced in Go code, not by a schema minLength: so this is refused LOUDLY, naming the
// anonymous milestone, rather than silently committing it with an empty Name.
//
// It is the one function both the co-author staging seam and the drift gate
// (TestDerivedPlanMatchesCommittedState) run, so what a real first run stages and what CI
// holds slot 10 to can never disagree.
func materializeNetwork(
	list projectstate.ActivityList,
	deps []projectstate.NetworkDependency,
	milestones []projectstate.NetworkMilestone,
	authored projectstate.Network,
) (projectstate.Network, error) {
	decorations := make(map[string]projectstate.NetworkMilestone, len(authored.Milestones))
	for _, m := range authored.Milestones {
		decorations[m.ID] = m
	}
	outMilestones := make([]projectstate.NetworkMilestone, 0, len(milestones))
	for _, m := range milestones {
		a := decorations[m.ID]
		// The guard and the stored value read the SAME trimmed name: a guard that
		// refuses "   " but then stores "  Engines Complete " would commit the padding
		// it just judged meaningless.
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return projectstate.Network{}, newError(fwmanager.ContractMisuse,
				fmt.Sprintf("derived milestone %q has no authored Name — the draft must author a Name (and Public) decoration for this milestone id", m.ID))
		}
		outMilestones = append(outMilestones, projectstate.NetworkMilestone{
			ID: m.ID, Name: name, Public: a.Public, DependsOn: m.DependsOn,
		})
	}

	cp, err := derivedCriticalPath(list, deps, milestones)
	if err != nil {
		return projectstate.Network{}, err
	}
	return projectstate.Network{Dependencies: deps, CriticalPath: cp, Milestones: outMilestones}, nil
}

// derivedCriticalPath solves the derived network with the estimation Engine's
// ComputeNetwork and returns the sorted set of activities it places on the critical path.
// Milestones are zero-duration nodes in the same solve but are not activities, so they
// never appear in the result (ComputeNetwork reports them separately).
func derivedCriticalPath(
	list projectstate.ActivityList,
	deps []projectstate.NetworkDependency,
	milestones []projectstate.NetworkMilestone,
) ([]string, error) {
	acts := estimation.ActivityList{Activities: make([]estimation.ActivityItem, 0, len(list.Activities))}
	for _, a := range list.Activities {
		acts.Activities = append(acts.Activities, estimation.ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	nw := estimation.Network{Dependencies: make([]estimation.NetworkDependency, 0, len(deps))}
	for _, d := range deps {
		nw.Dependencies = append(nw.Dependencies, estimation.NetworkDependency{Activity: d.Activity, DependsOn: d.DependsOn})
	}
	for _, m := range milestones {
		nw.Milestones = append(nw.Milestones, estimation.NetworkMilestone{Id: m.ID, DependsOn: m.DependsOn})
	}

	sol, err := estimation.NewEstimationEngine().ComputeNetwork(fweng.Context{}, acts, nw)
	if err != nil {
		return nil, fwmanager.Wrap(fwmanager.FailedPrecondition, err,
			"cannot compute the critical path of the derived network")
	}
	cp := make([]string, 0, len(sol.Nodes))
	for id, n := range sol.Nodes {
		if n.OnCriticalPath {
			cp = append(cp, id)
		}
	}
	sort.Strings(cp)
	return cp, nil
}

// ---------------------------------------------------------------------------
// CONSTRUCTION RAIL — moved verbatim from internal/manager/construction/
// constructionmanager.go at stage 4a. Bodies are unchanged; only package-private
// names that collided with another rail were renamed (the collision table is in
// docs/superpowers/plans/2026-09-25-activity-experience-stage4a.md, Task 6 Step 2b,
// and in this commit's message). 4b replaces this block with the generic DAG child.
// ---------------------------------------------------------------------------

// constructionManager is the constructionManager façade — the concrete
// implementation of the GENERATED ConstructionManager interface (contract.gen.go). It
// exposes the five public use-case ops (constructionManager.md §2) and OWNS Temporal.
// The Temporal-backed ops:
//   - ExecuteNextActivity — Workflow (entry; scheduler-triggered pump)
//   - RunReplanSweep      — Workflow (entry; scheduler-triggered variance sweep)
//   - PauseProject        — Signal (operatorPauseRequested)
//   - OverrideActivity    — Signal (operatorOverride, to the per-activity child)
//   - GetSessionState     — Query (sessionState, read-only)
//
// The façade methods use only the Temporal client; the pre-condition checks
// (non-empty ids, non-empty reason, known OverrideKind) are enforced here before any
// Temporal call (§2/§3.5). It ALSO stores the PUBLISHED downstream deps the GENERATED
// constructor was given so RegisterWorker can fold them (adapters.go) into the
// hand-written Temporal csWorkflows. The former exported consumer-mirror interfaces +
// the composition-root adapters are RETIRED; the Manager depends on the deps'
// PUBLISHED interfaces and adapts them internally.
type constructionManager struct {
	client client.Client

	projectState           projectstate.ProjectStateAccess
	artifact               artifact.ArtifactAccess
	intervention           intervention.InterventionEngine
	review                 review.ReviewEngine
	pipeline               agenticjob.AgenticJobAccess
	rail                   sourcecontrol.SourceControlAccess
	constructionTransition projectstate.ConstructionTransitionAccess
	gitActivityStatus      projectstate.GitActivityStatusAccess
	escalationWaitTimeout  time.Duration
	interventionMode       string

	// repo (B5) is the per-project Repo resolver the gh-mode venue switch dispatches
	// through: projectID → the project's own RepoRef. nil ⇒ every construction dispatch
	// falls back to the configured central construction repo AND the PR-rail slice stays
	// dormant (gitEnabled). Non-nil retargets the dispatch (aiarch-construct.yml in the
	// project repo) AND activates the branch→PR rail. Threaded into the csWorkflows via
	// wfDeps.Repo (WorkerManifest).
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// messageBus (7b) is the generated messageBus Utility dep — the restricted
	// Manager-only signal/schedule surface. Threaded into genActivities so the
	// csWorkflows can reach registerSchedule/deliverSignal through the generated
	// invokers. Task 7c (landed 2026-08-01) added this Manager's RegisterSchedules
	// (the pump tick and the replan sweep) plus the startup wiring — the
	// composition root now threads the real messageBus.MessageBus here, exactly as
	// it does for billing/operations (main.gen.go; CONSTRUCTION_DRYRUN gates only
	// which Schedules that shared bus actually registers, via hooks.go's
	// FinalizeMessageBus/dryRunConstructionScheduleGate).
	messageBus messagebus.MessageBus

	// designSession (B6) is the generated designSessionAccess dep. Since the B8
	// follow-up it is CONSUMED by the csWorkflows: the pump's whole-aggregate read rides
	// the generated designSessionAccess.readProjectOnBranch invoker with branch ""
	// (main) — the shared projectstate.ProjectEnvelope was extended with the
	// construction-fidelity sections (ActivityConstruction / ServiceContracts /
	// ReviewPolicy, envelope.go) that construction's former local codec carried, which
	// is what retired the last custom Activity (ReadProjectActivity).
	designSession projectstate.DesignSessionAccess

	// episodes (SP1 capture-seam) is the generated episodeAccess dep — the agentic-
	// episode ledger every terminal pipeline observation appends to. Reached ONLY
	// through the generated invoker surface (Acts.EpisodesAppendEpisode) inside the
	// csWorkflows; this field exists to thread it into genActivities.
	episodes episode.EpisodeAccess

	// activityExecution (stage 3) is the generated activityExecutionAccess dep — the
	// fifth facet of the one project-state component, owner of the per-activity attempt
	// and review-round ledgers. Taking the dep HERE is what registers its twelve
	// Temporal activities on this Manager's worker, which is the precondition for task
	// 5: the construction child workflow switches onto them behind workflow.GetVersion,
	// with the old branch still calling the deprecated-in-place facets whose activity
	// names the replay fixtures record. Nothing in this wave CALLS these verbs yet.
	activityExecution projectstate.ActivityExecutionAccess
}

// newConstructionManager is the hand-written, unexported builder the generated
// NewConstructionManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only the client; the deps are
// stored for RegisterWorker (worker.go), which folds them into the Temporal csWorkflows.
func newConstructionManager(
	c client.Client,
	projectState projectstate.ProjectStateAccess,
	art artifact.ArtifactAccess,
	interventionEng intervention.InterventionEngine,
	reviewEng review.ReviewEngine,
	pipeline agenticjob.AgenticJobAccess,
	rail sourcecontrol.SourceControlAccess,
	constructionTransition projectstate.ConstructionTransitionAccess,
	gitActivityStatus projectstate.GitActivityStatusAccess,
	designSession projectstate.DesignSessionAccess,
	activityExecution projectstate.ActivityExecutionAccess,
	messageBus messagebus.MessageBus,
	episodes episode.EpisodeAccess,
	escalationWaitTimeout time.Duration,
	interventionMode string,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
) *constructionManager {
	return &constructionManager{
		client:                 c,
		projectState:           projectState,
		artifact:               art,
		intervention:           interventionEng,
		review:                 reviewEng,
		pipeline:               pipeline,
		rail:                   rail,
		constructionTransition: constructionTransition,
		gitActivityStatus:      gitActivityStatus,
		designSession:          designSession,
		activityExecution:      activityExecution,
		messageBus:             messageBus,
		episodes:               episodes,
		escalationWaitTimeout:  escalationWaitTimeout,
		interventionMode:       interventionMode,
		repo:                   repo,
	}
}

// ExecuteNextActivity — op 2.1. Temporal Workflow (entry; client/MCP-driven).
// Starts — or JOINS — the project's ONE PumpNextActivityWorkflow on the construction
// queue, id {projectId}:nextActivity (pumpWorkflowID). The pump reads head-state, and
// on an eligible activity executes a per-activity child workflow
// {projectId}:{activityId}. No eligible activity ⇒ PumpResult{Dispatched:false} (a
// normal quiet tick).
//
// ONE PUMP PER PROJECT (architect pump ruling, 2026-09-12). The pump is the single
// writer walking the project's dependency frontier; two pumps racing the same frontier
// double-dispatch. Every entry — this façade (Begin / MCP) AND PumpSweepWorkflow's
// Schedule fan-out — derives the SAME id from pumpWorkflowID, so:
//   - WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING: a call while the pump runs (its
//     self-cascade chains ContinueAsNew under the same id) JOINS that run instead of
//     starting a second pump.
//   - WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE: once the previous pump CLOSED (drained
//     quiet, paused, or failed), the next call starts a fresh one. NOT
//     ALLOW_DUPLICATE_FAILED_ONLY — a quiet-completed pump must be restartable, or the
//     project could never be pumped again after its first drain.
//
// PAUSED PROJECTS (plan B1.7; founder ruling 2026-09-13, "Begin on a paused project
// refuses"): a recorded operator pause refuses this op with FailedPrecondition — "resume
// it to continue" — BEFORE any pump is started. The pause is an operator decision; a
// caller's Begin silently overriding it would be the same class of override the sweep
// exclusion exists to prevent. ResumeProject is the one way back: it clears the record
// and starts the pump. The pump this op starts no longer carries OperatorDriven, so it
// honours the recorded pause like every other pump (pump-honors-recorded-pause v2).
//
// tickID is a CORRELATION id only (logged here); it no longer shapes the workflow id,
// so it cannot fork a second pump. It stays a required, non-empty input (the contract
// shape is unchanged). SYNC: returns the pump's dispatch decision
// (PumpResult{Dispatched:true, ActivityID}, or {Dispatched:false} when quiescent) as
// soon as the pump run has decided — it does NOT block until the per-activity child (or
// the background self-cascade over the dependency frontier) drains. A caller that
// joined a running pump reads THAT run's decision. The decision is read off the pump
// via the queryPumpDispatch Query while the cascade continues durably in the
// background.
func (m *constructionManager) ExecuteNextActivity(rc fwmanager.Context, projectID ProjectID, tickID string) (PumpResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if tickID == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}

	if err := m.refuseWhilePaused(ctx, projectID); err != nil {
		return PumpResult{}, err
	}

	wfID := pumpWorkflowID(projectID)
	slog.Default().InfoContext(ctx, "construction pump: start-or-join",
		"projectId", string(projectID), "workflowId", wfID, "tickId", tickID)
	we, err := m.startOrJoinPump(ctx, projectID)
	if err != nil {
		return PumpResult{}, csMapStartError(err)
	}
	return m.awaitDispatchDecision(ctx, we, wfID)
}

// startOrJoinPump starts — or JOINS — the project's ONE pump (pumpWorkflowID) with the
// policy pair the one-pump ruling fixes: USE_EXISTING joins a running pump (its
// self-cascade included), ALLOW_DUPLICATE restarts one that closed. The shared start of
// ExecuteNextActivity (which awaits the pump's decision) and ResumeProject (which does
// not). The input carries no OperatorDriven: every new pump honours the recorded pause.
func (m *constructionManager) startOrJoinPump(ctx context.Context, projectID ProjectID) (client.WorkflowRun, error) {
	opts := client.StartWorkflowOptions{
		ID:                       pumpWorkflowID(projectID),
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
		WorkflowIDReusePolicy:    enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}
	return m.client.ExecuteWorkflow(ctx, opts, executionKindPump, pumpInput{ProjectID: projectID})
}

// refuseWhilePaused is ExecuteNextActivity's paused precheck (B1.7): a recorded pause
// refuses Begin with FailedPrecondition, naming the operator's reason. A project that
// does not exist cannot be paused (the pump's own read is the quiet tick then); any
// other read fault is Infrastructure — Begin never dispatches past a pause it could
// not rule out.
func (m *constructionManager) refuseWhilePaused(ctx context.Context, projectID ProjectID) error {
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRANotFound(err) {
			return nil
		}
		return newError(fwmanager.Infrastructure, "read the project before starting construction: "+err.Error())
	}
	if !proj.OperatorPaused {
		return nil
	}
	return newError(fwmanager.FailedPrecondition, pausedDetail(proj.PauseReason))
}

// pausedDetail is the refusal a paused project gets from Begin.
func pausedDetail(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return "construction is paused — resume it to continue"
	}
	return fmt.Sprintf("construction is paused (%s) — resume it to continue", reason)
}

// pumpDispatchPollInterval paces the façade's poll of the pump's synchronous
// dispatch-decision Query. The pump sets its decision after reading head-state and
// picking (or finding no) eligible activity — a couple of activity round-trips — so
// the poll converges within a few iterations.
const pumpDispatchPollInterval = 25 * time.Millisecond

// pumpDispatchWaitBudget bounds the poll, so a run that never reaches its decision point
// cannot hold the caller forever. A var (not a const) only so tests can shorten it.
//
// OPEN (fix round 3, pending a contract ruling): when this budget expires while the run
// is still RUNNING and undecided, the honest answer is "still deciding — the pump is
// running", which is not a failure. PumpResult has no such outcome and fwmanager has no
// non-failure Kind, so saying it on the wire needs a contract change
// (project.json .serviceContracts), which this branch must not make while D9 rewrites
// project.json. Until then the façade returns promptly with a DISTINGUISHABLE
// Infrastructure Detail (pumpStillDecidingDetail) instead of waiting out the terminal
// budget into a generic timeout.
var pumpDispatchWaitBudget = 30 * time.Second

// pumpQueryFailureBudget is how long the dispatch-decision Query may KEEP failing (e.g.
// no worker polling during a rolling restart) before the façade stops retrying it and
// falls back to the bounded terminal wait. A single failed Query is retried, never read
// as "the run is gone". A var only so tests can shorten it.
var pumpQueryFailureBudget = 10 * time.Second

// pumpClosureCheckInterval paces the DescribeWorkflowExecution check that detects a run
// which CLOSED without deciding (it failed before its decision point). Queries against a
// closed run are served by replay and keep answering "not decided", so without this
// check the caller would wait out the whole budget and could lose the run's real error.
// A var only so tests can shorten it.
var pumpClosureCheckInterval = 250 * time.Millisecond

// pumpStillDecidingDetail marks the budget-exhausted, run-still-RUNNING outcome, so a
// caller can tell a slow pump from a failed one (see pumpDispatchWaitBudget's OPEN note).
const pumpStillDecidingDetail = "construction pump is still deciding — it is running, not failed; re-check with GetSessionState"

// awaitDispatchDecision returns THIS run's dispatch outcome as soon as the pump run has
// decided, WITHOUT waiting for the background self-cascade to drain the dependency
// frontier. It polls queryPumpDispatch against the exact run ExecuteWorkflow started or
// joined (pinned RunID), so the answer stays that run's decision even after the pump
// ContinueAsNews into the next cascade iteration. A slow run is not a failed one:
//   - "not decided" answers keep the poll going;
//   - a FAILING Query is retried for up to pumpQueryFailureBudget before falling back to
//     the bounded terminal wait;
//   - a run that CLOSED without deciding surfaces its own terminal result / real error
//     promptly (throttled Describe);
//   - a run still RUNNING and undecided at the budget returns pumpStillDecidingDetail.
func (m *constructionManager) awaitDispatchDecision(ctx context.Context, we client.WorkflowRun, wfID string) (PumpResult, error) {
	runID := we.GetRunID()
	deadline := time.Now().Add(pumpDispatchWaitBudget)
	var queryFailingSince, lastClosureCheck time.Time
	for {
		d, qerr, fatal := m.pollPumpDispatch(ctx, wfID, runID)
		if fatal != nil {
			return PumpResult{}, fatal
		}
		if qerr == nil && d.Decided {
			return PumpResult{Dispatched: d.Dispatched, ActivityID: d.ActivityID}, nil
		}
		now := time.Now()
		switch {
		case qerr == nil:
			queryFailingSince = time.Time{}
		case queryFailingSince.IsZero():
			queryFailingSince = now
		}
		if now.Sub(lastClosureCheck) >= pumpClosureCheckInterval {
			lastClosureCheck = now
			if m.pumpRunClosed(ctx, wfID, runID) {
				return m.terminalPumpResult(ctx, we)
			}
		}
		if qerr != nil && now.Sub(queryFailingSince) >= pumpQueryFailureBudget {
			return m.terminalPumpResult(ctx, we)
		}
		if now.After(deadline) {
			if m.pumpRunClosed(ctx, wfID, runID) {
				return m.terminalPumpResult(ctx, we)
			}
			return PumpResult{}, newError(fwmanager.Infrastructure, pumpStillDecidingDetail)
		}
		select {
		case <-ctx.Done():
			return PumpResult{}, newError(fwmanager.Infrastructure, ctx.Err().Error())
		case <-time.After(pumpDispatchPollInterval):
		}
	}
}

// pumpRPCTimeout bounds EACH Query / Describe RPC the dispatch-decision poll makes — the
// same move terminalPumpResult makes for we.Get. Without it a single hung RPC would block
// past every wall-clock budget (pumpDispatchWaitBudget, pumpQueryFailureBudget), since
// those are only checked between RPCs. A timed-out Query reads as a failing Query
// (retried, then the bounded fallback); a timed-out Describe as "not known closed". A var
// only so tests can shorten it.
var pumpRPCTimeout = 5 * time.Second

// pollPumpDispatch runs one queryPumpDispatch against the pinned run, bounded by
// pumpRPCTimeout. queryErr means the run could not SERVE the Query right now (the caller
// retries); fatal is a decode failure of an answer it did serve — surfaced at once, never
// polled as "not decided".
func (m *constructionManager) pollPumpDispatch(ctx context.Context, wfID, runID string) (d pumpDispatch, queryErr, fatal error) {
	qctx, cancel := context.WithTimeout(ctx, pumpRPCTimeout)
	defer cancel()
	enc, err := m.client.QueryWorkflow(qctx, wfID, runID, queryPumpDispatch)
	if err != nil {
		return pumpDispatch{}, err, nil
	}
	if err := enc.Get(&d); err != nil {
		return pumpDispatch{}, nil, newError(fwmanager.Infrastructure, err.Error())
	}
	return d, nil, nil
}

// pumpRunClosed reports whether the pinned pump run has CLOSED (any status but
// Running — Failed, Canceled, Terminated, TimedOut, or Completed without having decided).
// Bounded by pumpRPCTimeout. A Describe failure, or an empty answer, reads as "not known
// closed" — the poll carries on.
func (m *constructionManager) pumpRunClosed(ctx context.Context, wfID, runID string) bool {
	dctx, cancel := context.WithTimeout(ctx, pumpRPCTimeout)
	defer cancel()
	resp, err := m.client.DescribeWorkflowExecution(dctx, wfID, runID)
	if err != nil {
		return false
	}
	info := resp.GetWorkflowExecutionInfo()
	if info == nil {
		return false
	}
	return info.GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING
}

// pumpTerminalWaitBudget bounds terminalPumpResult's wait. A var (not a const) only so
// a test can shorten it.
var pumpTerminalWaitBudget = 10 * time.Second

// terminalPumpResult is the safety-net fallback: it awaits the pump's terminal result
// (used only when the dispatch decision never surfaced — a failed run or a run that
// finished before it could be polled).
//
// BOUNDED (fix round, M7): WorkflowRun.Get FOLLOWS the ContinueAsNew chain, so for a
// caller that joined a RUNNING pump (one pump per project) and then hit a query failure,
// an unbounded Get would block until the whole self-cascade drains — hours of
// construction. A failed run returns well inside the budget, so its error still
// surfaces; past the budget the caller gets an Infrastructure error instead of a hang.
func (m *constructionManager) terminalPumpResult(ctx context.Context, we client.WorkflowRun) (PumpResult, error) {
	wctx, cancel := context.WithTimeout(ctx, pumpTerminalWaitBudget)
	defer cancel()
	var result PumpResult
	if err := we.Get(wctx, &result); err != nil {
		return PumpResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// RunReplanSweep — op 2.2. Temporal Workflow (entry; scheduler-triggered, short).
// Reads in-flight construction state, flags over-threshold variances, surfaces
// them to the operator dashboard — it does NOT auto-replan. An empty result is a
// normal quiet sweep. A nil projectID sweeps all in-flight projects (workflow id
// :all:replanSweep:{tickId}).
func (m *constructionManager) RunReplanSweep(rc fwmanager.Context, projectID *ProjectID, tickID string) (ReplanSweepResult, error) {
	ctx := rc.Context
	if tickID == "" {
		return ReplanSweepResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}
	var in replanSweepInput
	if projectID != nil {
		if *projectID == "" {
			return ReplanSweepResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
		}
		pid := *projectID
		in.ProjectID = &pid
	}

	wfID := replanSweepWorkflowID(projectID, tickID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindReplanSweep, in)
	if err != nil {
		return ReplanSweepResult{}, csMapStartError(err)
	}
	var result ReplanSweepResult
	if err := we.Get(ctx, &result); err != nil {
		return ReplanSweepResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// PauseProject — op 2.3. Temporal Signal (operatorPauseRequested) to the project's
// in-flight construction execution(s). The suspended supervision resumes on its
// awaitSignal and runs the pause branch (interventionEngine.applyPausePolicy →
// pausePlan, then the Manager EXECUTES the cancels/records). The pause branch also
// relays the pause to the project's ONE pump ({projectId}:nextActivity) through
// messageBus.deliverSignal, so a cascading pump stops after its current activity
// instead of dispatching through the pause (runPauseBranch, projectsupervision.go).
// SYNC from the operator's POV: returns once the signal is durably enqueued.
func (m *constructionManager) PauseProject(rc fwmanager.Context, projectID ProjectID, reason string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if reason == "" {
		return newError(fwmanager.ContractMisuse, "empty pause reason")
	}

	wfID := pauseTargetWorkflowID(projectID)
	sig := operatorPauseSignal{ProjectID: projectID, Reason: reason}
	// Signal-with-start: the project-level supervision workflow resumes on its
	// awaitSignal and runs the pause branch; if not running, it is started
	// (constructionManager.md §6.2 — startOrSignalExecution semantics).
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, signalOperatorPauseRequested, sig,
		opts, executionKindProjectSupervision, projectSupervisionInput{ProjectID: projectID})
	if err != nil {
		return mapSignalError(err)
	}
	return nil
}

// ResumeProject — op 2.10 (plan B1.7; amendment §B.1). Clears the project's RECORDED
// operator pause and starts (or joins) its pump. A write straight through the RA (the
// SetReviewPolicy pattern): no supervision workflow is involved, because the pause
// branch handles exactly one signal and exits.
//
// Refusals, in a PINNED order: ContractMisuse (empty id) → NotFound (no project) →
// FailedPrecondition, not in construction → FailedPrecondition, not paused (never a
// silent no-op) → FailedPrecondition, a pause is still being applied (its supervision
// run {p}:construction is RUNNING: the pause is recorded but not yet relayed, and a
// resume now could be followed by a relay that stops the fresh pump). Nothing is
// written on any refusal.
//
// The write (RecordOperatorResumed) is retried on a version Conflict up to
// resumeConflictAttempts times, re-reading the version between tries. Then the pump is
// started or joined WITHOUT awaiting its decision. If that start fails, the resume has
// still landed: the record is clear, so the 30s sweep pumps the project. That is logged,
// never returned as an error.
func (m *constructionManager) ResumeProject(rc fwmanager.Context, projectID ProjectID) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRANotFound(err) {
			return newError(fwmanager.NotFound, err.Error())
		}
		return newError(fwmanager.Infrastructure, err.Error())
	}
	if proj.Phase != projectstate.PhaseConstruction {
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf("project %s is not in construction, so there is no construction to resume", projectID))
	}
	if !proj.OperatorPaused {
		return newError(fwmanager.FailedPrecondition, "construction is not paused — there is nothing to resume")
	}
	if err := m.refuseWhilePauseInFlight(ctx, projectID); err != nil {
		return err
	}
	if err := m.recordResumed(ctx, projectID, proj); err != nil {
		return err
	}
	if _, err := m.startOrJoinPump(ctx, projectID); err != nil {
		slog.Default().WarnContext(ctx, "construction resumed, but the pump did not start now; the 30-second sweep will start it",
			"projectId", string(projectID), "error", err.Error())
	}
	return nil
}

// resumeConflictAttempts bounds ResumeProject's re-read → re-write loop on a version
// Conflict before it answers "changed concurrently; retry".
const resumeConflictAttempts = 3

// refuseWhilePauseInFlight refuses a resume while the project's supervision run — the
// pause branch, {p}:construction — is RUNNING (bounded by pumpRPCTimeout). No such run
// is fine; any other describe fault is Infrastructure.
func (m *constructionManager) refuseWhilePauseInFlight(ctx context.Context, projectID ProjectID) error {
	dctx, cancel := context.WithTimeout(ctx, pumpRPCTimeout)
	defer cancel()
	resp, err := m.client.DescribeWorkflowExecution(dctx, pauseTargetWorkflowID(projectID), "")
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return newError(fwmanager.Infrastructure, "check whether a pause is still being applied: "+err.Error())
	}
	if info := resp.GetWorkflowExecutionInfo(); info != nil && info.GetStatus() == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return newError(fwmanager.FailedPrecondition, "a pause is still being applied — retry in a moment")
	}
	return nil
}

// recordResumed writes RecordOperatorResumed at seen's version, retrying on a Conflict
// (one idempotency key per call, so a retried transport write dedupes). A Conflict means
// the project moved, so every retry RE-CHECKS what the first try was allowed on (I2):
// still in construction, still paused, the SAME pause the operator resumed (its reason
// unchanged), and no pause being applied now. A pause that landed between two tries is
// never cleared by a resume that did not see it.
func (m *constructionManager) recordResumed(ctx context.Context, projectID ProjectID, seen projectstate.Project) error {
	key := fwra.IdempotencyKey("resume:" + string(projectID) + ":" + uuid.NewString())
	version := seen.Version
	for range resumeConflictAttempts {
		_, err := m.constructionTransition.RecordOperatorResumed(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), version, projectstate.RepoCredential{}, key)
		if err == nil {
			return nil
		}
		if !csIsRAConflict(err) {
			return newError(fwmanager.Infrastructure, err.Error())
		}
		proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
		if rerr != nil {
			return newError(fwmanager.Infrastructure, rerr.Error())
		}
		// The façade's pinned order: still in construction, still paused, no pause being
		// applied now, and only then the same pause.
		if err := resumableStill(projectID, proj); err != nil {
			return err
		}
		if err := m.refuseWhilePauseInFlight(ctx, projectID); err != nil {
			return err
		}
		if proj.PauseReason != seen.PauseReason {
			return newError(fwmanager.FailedPrecondition, fmt.Sprintf("a new pause (%s) landed while resuming — review it, then resume again", proj.PauseReason))
		}
		version = proj.Version
	}
	return newError(fwmanager.FailedPrecondition, "the project changed concurrently while resuming — retry")
}

// resumableStill re-checks, on a re-read, the first two preconditions ResumeProject
// checked on its first read: the project is still in construction and still paused.
// (recordResumed then re-checks the pause in flight, and that the pause is the one the
// operator resumed: a different reason is a different pause the operator has not seen.)
func resumableStill(projectID ProjectID, now projectstate.Project) error {
	switch {
	case now.Phase != projectstate.PhaseConstruction:
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf("project %s left construction while resuming, so there is no construction to resume", projectID))
	case !now.OperatorPaused:
		return newError(fwmanager.FailedPrecondition, "construction is not paused — there is nothing to resume")
	}
	return nil
}

// csIsRAConflict reports whether err is (or wraps) a ResourceAccess version Conflict.
func csIsRAConflict(err error) bool {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind == fwra.Conflict
	}
	return false
}

// OverrideActivity — op 2.4. Temporal Signal (operatorOverride) to the per-activity
// child workflow {projectId}:{activityId}. The operator's steer is fed through the
// SAME decide→execute machinery as the automatic variance path. SYNC: returns once
// the signal is durably enqueued.
//
// PRECHECK (B1.3): after the ContractMisuse checks, the op reads the activity's session
// (the Query GetSessionState serves; no session is NotFound) and refuses with
// FailedPrecondition unless the activity is awaiting a takeover. The workflow buffers
// override signals, so an override sent at any other time used to be consumed by the
// activity's NEXT escalation — a steer applied to a situation the operator never saw
// (plan G7). This is honesty at the façade, not a lock: the residual check-then-act
// window is milliseconds, and draining a stale buffered override inside the workflow
// is a command change that is EARMARKED behind its own GetVersion. During a rolling
// deploy a view served by an old worker carries no awaitingGate; the refusal is then
// transient and fails safe.
func (m *constructionManager) OverrideActivity(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, override ActivityOverride) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwmanager.ContractMisuse, "empty activityId")
	}
	switch override.Kind {
	case OverrideTakeover, OverrideRetry, OverrideSkip, OverrideReassign:
		// ok
	case OverrideUnknown:
		// zero-value sentinel, not a real override kind — same as any unmapped value.
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown override kind %d", int(override.Kind)))
	default:
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown override kind %d", int(override.Kind)))
	}
	if strings.TrimSpace(override.Notes) == "" {
		return newError(fwmanager.ContractMisuse, "an override requires non-empty notes — it is the operator's durable record of WHY the automatic path was steered")
	}
	if err := checkOperatorNoteSize("an override's notes", override.Notes, override.Comments); err != nil {
		return err
	}
	view, err := m.activitySession(ctx, projectID, activityID)
	if err != nil {
		return err
	}
	if view.Stage != StageAwaitingTakeover {
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"activity %s is at %s, not awaiting a takeover — an override steers an escalation; decide a gate with SubmitPhaseDecision",
			activityID, sessionStageName(view.Stage)))
	}

	wfID := constructActivityWorkflowID(projectID, activityID)
	sig := operatorOverrideSignal{Override: override}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalOperatorOverride, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// GetSessionState — op 2.5. Temporal Query (sessionState, read-only). Returns a
// point-in-time technical view without mutating state. When activityID is non-nil
// it queries the per-activity child {projectId}:{activityId}; otherwise the
// project-level pump view (constructionManager.md §6.2).
func (m *constructionManager) GetSessionState(rc fwmanager.Context, projectID ProjectID, activityID *ActivityID) (ConstructionSessionView, error) {
	ctx := rc.Context
	if projectID == "" {
		return ConstructionSessionView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}

	var wfID string
	if activityID != nil {
		if *activityID == "" {
			return ConstructionSessionView{}, newError(fwmanager.ContractMisuse, "empty activityId")
		}
		wfID = constructActivityWorkflowID(projectID, *activityID)
	} else {
		wfID = pauseTargetWorkflowID(projectID)
	}

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", querySessionState)
	if err != nil {
		// F20 (error altitude): before construction starts the pump/per-activity
		// workflow does not exist, and Temporal's raw "workflow not found for ID:
		// <proj>:construction" leaks the internal execution id to the client. Map that
		// to a clean, user-altitude NotFound; other query faults keep their generic
		// mapping.
		if isNotFound(err) {
			// Which session is absent decides the sentence. A per-activity miss means only
			// that the pump has not dispatched THIS activity — construction may well be
			// under way elsewhere in the project, so "construction has not started for this
			// project" was false for it.
			if activityID != nil {
				return ConstructionSessionView{}, newError(fwmanager.NotFound,
					"no construction session for activity "+string(*activityID)+": the pump has not dispatched it")
			}
			return ConstructionSessionView{}, newError(fwmanager.NotFound, "construction has not started for this project")
		}
		return ConstructionSessionView{}, csMapQueryError(err)
	}
	var view ConstructionSessionView
	if err := enc.Get(&view); err != nil {
		return ConstructionSessionView{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return view, nil
}

// QueryActivityView — op 2.13 (unified-activity spec 2026-09-20, stage 0). The Activity
// Experience's single read: one activity's platform-fixed lifecycle (method-assets),
// each task's state and revisions, and the live review set. A PLAIN METHOD like the
// episode reads — no workflow, no signal; it asks the activity's session (the same
// Temporal Query GetSessionState serves) only while the row is Running, so a Done or
// not-started activity reads with Temporal down.
//
// A gate the row holds PERSISTED rounds for is a projection of that ledger (stage 3):
// its verdicts, thread, roster, subject, round number and decision are read, not derived.
// The reconstruction below (normalizeAttempts, deriveTaskViews) survives for the gates
// that have no round — every gate of every row written before the ledger existed — and
// the revision's provenance is what tells a reader which of the two they are looking at.
// An id the committed activity list does not
// hold is NotFound. Since stage 2 that list HOLDS requirements, architecture and
// projectDesign (slot 9 opens with all three), so a design activity reads like any
// other — ClassifyType tolerates ErrDesignActivityNotDispatchable for exactly this
// reason, and LifecycleKeyFor resolves all three against method-assets.
//
// The live session is read ONCE and both the attempt list and the gate come out of that
// one read: state rule 1 answers awaitingHuman for the gate task matching the live gate
// even with no revision at all, so a stale gate beside fresh attempts would show a gate
// with no history.
func (m *constructionManager) QueryActivityView(rc fwmanager.Context, projectID ProjectID, activityID ActivityID) (ActivityView, error) {
	ctx := rc.Context
	if projectID == "" {
		return ActivityView{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return ActivityView{}, newError(fwmanager.ContractMisuse, "empty activityId")
	}
	id := string(activityID)
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return ActivityView{}, mapRAError(err, "projectStateAccess.ReadProject")
	}
	item, ok := committedActivityItem(proj, id)
	if !ok {
		return ActivityView{}, newError(fwmanager.NotFound, "no activity "+id+" in the committed activity list")
	}
	row := proj.ActivityExecution[id]
	row.ActivityID = id
	typ, variant, resolved, classified := projectstate.ResolveConstructionRow(row, item)
	if !classified {
		return ActivityView{}, newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"activity %s (workerClass %q, coding=%v) matches no activity-classification rule, so it has no lifecycle — amend workerClass or coding in the committed activity list",
			id, item.WorkerClass, item.Coding))
	}
	key := projectstate.LifecycleKeyFor(typ, variant)
	lc, ok := methodassets.LifecycleFor(key)
	if !ok {
		return ActivityView{}, newError(fwmanager.Infrastructure, "the platform's method assets carry no lifecycle for activity type "+key)
	}
	coarse, _ := projectstate.EffectiveConstructionPhase(row, item)
	live, err := m.liveSessionFor(ctx, projectID, activityID, coarse)
	if err != nil {
		return ActivityView{}, err
	}
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{ProjectID: episode.ProjectID(projectID), TargetRef: &id})
	if err != nil {
		return ActivityView{}, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	// The gate is the session's awaitingGate verbatim: a lifecycle-phase id matches a
	// phase, and the merge hold and an escalation simply match none.
	liveGate, _ := liveApprovalGate(live)
	tasks := deriveTaskViews(lc, normalizeAttempts(id, row, resolved, records, live), row.OperatorNotes, row.Reviews, liveGate)
	view := activityViewFrom(activityID, item, typ, variant, lc, resolved, tasks)
	view.State = activityViewState(coarse, live)
	// The roster and the engine's refusal to produce one are the SAME fact about the live
	// gate, so they travel together under the one condition: a refusal without a live gate
	// would explain an absence nobody is looking at.
	if liveGate != "" {
		view.ReviewSet, view.ReviewSetError = live.ReviewSet, live.ReviewSetError
	}
	return view, nil
}

// GetPumpStatus — op 2.9 (plan B1.5). Reports whether the project's ONE construction
// pump ({projectId}:nextActivity, pumpWorkflowID) has a RUNNING execution now. It
// describes the pump id with an EMPTY run id, which reads the latest run — so a pump
// cascading between activities (ContinueAsNew keeps the id) reads as open. No pump for
// the project is {open:false} with no error; any other describe fault is Infrastructure.
// The describe is bounded by pumpRPCTimeout, like the dispatch poll's describes.
//
// It is ONE fact on purpose. It does not fold in the recorded operator pause or any
// activity's live session: the client combines them (a paused project's pump is not
// open; an open pump can be between sessions). It is not the project-level
// GetSessionState either: that targets the supervision execution ({p}:construction),
// which is not the pump.
func (m *constructionManager) GetPumpStatus(rc fwmanager.Context, projectID ProjectID) (PumpStatus, error) {
	if projectID == "" {
		return PumpStatus{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	dctx, cancel := context.WithTimeout(rc.Context, pumpRPCTimeout)
	defer cancel()
	resp, err := m.client.DescribeWorkflowExecution(dctx, pumpWorkflowID(projectID), "")
	if err != nil {
		if isNotFound(err) {
			return PumpStatus{Open: false}, nil
		}
		return PumpStatus{}, newError(fwmanager.Infrastructure, "describe the construction pump: "+err.Error())
	}
	info := resp.GetWorkflowExecutionInfo()
	if info == nil || info.GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return PumpStatus{Open: false}, nil
	}
	status := PumpStatus{Open: true}
	if ts := info.GetStartTime(); ts != nil {
		started := ts.AsTime()
		status.RunStartedAt = &started
	}
	return status, nil
}

// SubmitPhaseDecision — op 2.6. Temporal Signal (phaseDecision) to the
// per-activity child workflow {projectId}:{activityId}. Delivers the operator's
// phase-gated approve/send-back decision (and optional feedback) through the same
// signal machinery as OverrideActivity. SYNC: returns once the signal is durably
// enqueued. SendBack requires non-empty feedback notes.
//
// phase is one of the five ActivityMethodPhase wire names OR mergeGateKey
// ("merge") — the local merge hold (runLocalMergeStep) suspends on the same
// signal, and this op is the ONLY operator path that releases it. The merge gate
// takes Approve only (see validatePhaseDecision).
//
// PRECHECK (B1.3), in a pinned order: the ContractMisuse checks first (ids, then
// validatePhaseDecision, then SendBack notes); then the activity's session is read
// (no session is NotFound); then the op refuses with FailedPrecondition unless the
// session is awaiting approval at exactly this gate (awaitingGate), and refuses a
// SendBack at a gate whose redraft budget is spent (redraftExhausted) — never a silent
// no-op presenting as success. Nothing is signalled on any refusal. The workflow still
// matches decisions by key, so this is honesty, not safety: a stale decision cannot
// close the wrong gate either way. During a rolling deploy a view served by an old
// worker carries no awaitingGate; the refusal is then transient and fails safe.
func (m *constructionManager) SubmitPhaseDecision(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, phase string, decision PhaseDecision, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwmanager.ContractMisuse, "empty activityId")
	}
	if err := validatePhaseDecision(phase, decision); err != nil {
		return err
	}
	// A whitespace-only note is an empty one (M3), as it is for an override.
	if decision == PhaseSendBack && (feedback == nil || strings.TrimSpace(feedback.Notes) == "") {
		return newError(fwmanager.ContractMisuse, "SendBack requires non-empty feedback notes")
	}
	if decision == PhaseSendBack {
		if err := checkOperatorNoteSize("a send-back note", feedback.Notes, feedback.Comments); err != nil {
			return err
		}
	}
	view, err := m.activitySession(ctx, projectID, activityID)
	if err != nil {
		return err
	}
	if err := precheckPhaseDecision(view, activityID, phase, decision); err != nil {
		return err
	}

	wfID := constructActivityWorkflowID(projectID, activityID)
	sig := phaseDecisionSignal{Phase: phase, Decision: decision, Feedback: feedback}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalPhaseDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// maxOperatorNoteRunes caps one operator note — its text plus its anchored comments'
// text and JSONPaths — at the façade (plan B1.4): a send-back's feedback and an
// override's notes are persisted on the activity and carried to the next agent attempt.
const maxOperatorNoteRunes = 4000

// operatorNoteRunes counts a note's characters: its text, and each anchored comment's
// text AND JSONPath (M6), since the rendered note carries both.
func operatorNoteRunes(notes string, comments []AnchoredComment) int {
	n := utf8.RuneCountInString(notes)
	for _, c := range comments {
		n += utf8.RuneCountInString(c.Text) + utf8.RuneCountInString(c.JSONPath)
	}
	return n
}

// checkOperatorNoteSize is the façade's size gate for one note, named by what: at most
// maxOperatorNoteRunes characters, and at most maxOperatorNoteBodyBytes once rendered, so
// the note always reaches the agent whole (the 16 KiB block keeps the newest note whole).
func checkOperatorNoteSize(what, notes string, comments []AnchoredComment) error {
	if operatorNoteRunes(notes, comments) > maxOperatorNoteRunes {
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("%s is at most %d characters, anchored comments and their paths included", what, maxOperatorNoteRunes))
	}
	if len(renderNoteBody(notes, noteComments(comments))) > maxOperatorNoteBodyBytes {
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("%s is at most %d bytes once rendered (anchored comments included); shorten it", what, maxOperatorNoteBodyBytes))
	}
	return nil
}

// activitySession reads one activity's session through the SAME Query GetSessionState
// serves, with its error mapping: no session is NotFound, any other query fault is
// Infrastructure.
func (m *constructionManager) activitySession(ctx context.Context, projectID ProjectID, activityID ActivityID) (ConstructionSessionView, error) {
	return m.GetSessionState(fwmanager.Context{Context: ctx}, projectID, &activityID)
}

// precheckPhaseDecision is SubmitPhaseDecision's FailedPrecondition gate over the
// activity's session view (B1.3).
func precheckPhaseDecision(v ConstructionSessionView, activityID ActivityID, key string, decision PhaseDecision) error {
	gate := "no gate"
	if v.AwaitingGate != nil {
		gate = *v.AwaitingGate
	}
	if v.Stage != StageAwaitingApproval || gate != key {
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf("activity %s is at %s/%s, not awaiting %s",
			activityID, sessionStageName(v.Stage), gate, key))
	}
	if decision == PhaseSendBack && v.RedraftExhausted {
		return newError(fwmanager.FailedPrecondition, fmt.Sprintf(
			"the redraft budget for %s is spent — approve, or steer with OverrideActivity", key))
	}
	return nil
}

// sessionStageName is a ConstructionStage's wire word, for refusal messages. A free
// function so the generated enum stays pure data (same rule as overrideKindName).
func sessionStageName(s ConstructionStage) string {
	switch s {
	case StageDispatching:
		return "dispatching"
	case StagePipelineRunning:
		return "pipelineRunning"
	case StageReviewing:
		return "reviewing"
	case StageAwaitingTakeover:
		return "awaitingTakeover"
	case StagePaused:
		return "paused"
	case StageExited:
		return "exited"
	case StageAwaitingApproval:
		return "awaitingApproval"
	case ConstructionStageUnknown:
		return "unknown"
	}
	return "unknown"
}

// SetReviewPolicy — op 2.8 (local-merge-and-policy Commit 2). Sets the project's
// review-policy PRESET (the Task-7 sophistication dial: vibes / checkpoints /
// full) while PRESERVING the committed GatedPhasesByType map (UpdateReviewPolicy's
// surface — the two ops write disjoint halves of the same ReviewPolicy).
//
// The preset is validated HERE, at the write path: the reviewEngine's read path
// (ProposeReviews, keyed off this policy's Preset) deliberately treats an
// unrecognized preset as the legacy explicit-map fallback, and with an empty map
// that gates NOTHING — the documented fail-open corner. A
// closed write vocabulary (rejecting unknowns as ContractMisuse) is what keeps a
// typo'd preset from silently degrading a project to "gate nothing".
func (m *constructionManager) SetReviewPolicy(rc fwmanager.Context, projectID ProjectID, preset string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	switch preset {
	case projectstate.ReviewPresetVibes, projectstate.ReviewPresetCheckpoints, projectstate.ReviewPresetFull:
		// closed vocabulary — fall through to the write.
	default:
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown review-policy preset %q (want %q, %q, or %q)",
			preset, projectstate.ReviewPresetVibes, projectstate.ReviewPresetCheckpoints, projectstate.ReviewPresetFull))
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRANotFound(err) {
			return newError(fwmanager.NotFound, err.Error())
		}
		return newError(fwmanager.Infrastructure, err.Error())
	}
	policy := proj.ReviewPolicy
	policy.Preset = &preset
	if _, err := m.constructionTransition.RecordReviewPolicy(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), proj.Version, policy, projectstate.RepoCredential{}, fwra.IdempotencyKey(uuid.NewString())); err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	return nil
}

// isRANotFound reports whether err is (or wraps) a ResourceAccess NotFound —
// the read path's "no such project" signal, mapped to the façade's own NotFound
// so the transport answers 404 rather than 500.
func isRANotFound(err error) bool {
	var fe *fwra.Error
	if errors.As(err, &fe) {
		return fe.Kind == fwra.NotFound
	}
	return false
}

// UpdateReviewPolicy — op 2.7. Persists the per-project ReviewPolicy.
// Converts the input's GatedPhasesByType (map[string][]string of ad-hoc or canonical
// gate ids) via projectstate.ReviewPolicyFromGateIDs to a typed ReviewPolicy, reads the
// current project version, then calls RecordReviewPolicy on the constructionTransition RA.
func (m *constructionManager) UpdateReviewPolicy(rc fwmanager.Context, projectID ProjectID, input ReviewPolicyInput) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	policy := projectstate.ReviewPolicyFromGateIDs(input.GatedPhasesByType)
	if _, err := m.constructionTransition.RecordReviewPolicy(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), proj.Version, policy, projectstate.RepoCredential{}, fwra.IdempotencyKey(uuid.NewString())); err != nil {
		return newError(fwmanager.Infrastructure, err.Error())
	}
	return nil
}

// --- workflow id derivation (continuity tokens; constructionManager.md §6.1) ---

// pumpWorkflowID derives the project's ONE pump workflow id, {projectId}:nextActivity.
// It is the single derivation every pump entry uses — ExecuteNextActivity (Begin /
// MCP) and PumpSweepWorkflow (the 30s Schedule fan-out) — so no two entries can start
// competing pumps over the same dependency frontier (architect pump ruling,
// 2026-09-12). Deliberately tick-invariant: a tick/firing id in the id would give each
// caller its own pump.
func pumpWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:nextActivity", projectID)
}

// replanSweepWorkflowID derives {projectId}:replanSweep:{tickId} or, for the
// all-projects sweep, :all:replanSweep:{tickId}.
func replanSweepWorkflowID(projectID *ProjectID, tickID string) string {
	if projectID == nil {
		return fmt.Sprintf(":all:replanSweep:%s", tickID)
	}
	return fmt.Sprintf("%s:replanSweep:%s", *projectID, tickID)
}

// constructActivityWorkflowID derives the per-activity child id {projectId}:{activityId}.
func constructActivityWorkflowID(projectID ProjectID, activityID ActivityID) string {
	return fmt.Sprintf("%s:%s", projectID, activityID)
}

// pauseTargetWorkflowID derives the project-level pump workflow id pause/sweep
// signals + the project-level session query address. The pause Signal targets the
// project's in-flight construction execution; the project-level pump id is the
// stable continuity token for the project's supervision.
func pauseTargetWorkflowID(projectID ProjectID) string {
	return fmt.Sprintf("%s:construction", projectID)
}

// --- error mapping at the façade boundary (constructionManager.md §3.5) -------

func csMapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; any
	// other error is treated as an infrastructure fault at the transport layer.
	return newError(fwmanager.Infrastructure, err.Error())
}

func csMapQueryError(err error) error {
	if isNotFound(err) {
		return newError(fwmanager.NotFound, err.Error())
	}
	// Failing-workflow-task hygiene (mirrors the design managers): a session being
	// retried after a deploy-time fault rejects queries with raw Temporal internals
	// ("Unable to query workflow due to Workflow Task in failed state") — clients
	// get a clean, actionable Detail instead.
	if strings.Contains(err.Error(), "Workflow Task in failed state") {
		return newError(fwmanager.Infrastructure,
			"construction session state is temporarily unavailable — the session hit an internal fault and is being retried by the server; try again shortly")
	}
	return newError(fwmanager.Infrastructure, err.Error())
}

// overrideKindName returns the canonical name for an override kind. Kept as a FREE
// FUNCTION (not a method) so the generated OverrideKind scalar carries no behavior
// (the contract surface is pure data).
func overrideKindName(k OverrideKind) string {
	switch k {
	case OverrideUnknown:
		// zero-value sentinel, not a real override kind.
		return "Unknown"
	case OverrideTakeover:
		return "Takeover"
	case OverrideRetry:
		return "Retry"
	case OverrideSkip:
		return "Skip"
	case OverrideReassign:
		return "Reassign"
	}
	// Unreachable for the five defined OverrideKind values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "Unknown"
}

// ---------------------------------------------------------------------------
// Façade error model (constructionManager.md §3.5).
// CALLER/PROGRAMMER errors at the façade boundary — distinct from the workflow's
// own failure handling (Temporal RetryPolicy + the intervention/variance
// alternative paths inside the workflow body). Kinds used: ContractMisuse,
// FailedPrecondition, NotFound, Unauthorized, Infrastructure.
// ---------------------------------------------------------------------------

// deps.go declares the hand-written domain VALUE types the Manager's workflow
// vocabulary uses. Per the founder DI model (2026-06-28) the constructionManager's
// GENERATED constructor (contract.gen.go: NewConstructionManager) takes the
// dependencies' PUBLISHED interfaces directly. The two Engines (intervention /
// review) are typed as their PUBLISHED contract interfaces DIRECTLY on
// wfDeps/csWorkflows (workflow.go) — no Manager-local seam interface, no adapter (Task 6).
//
// B8 (custom activities → generated, clean cut) + its follow-up removed EVERY
// Manager-local RA seam that used to live here:
//   - constructionTransitionAccess (10 ops) and gitActivityStatusAccess (6 ops): every
//     write verb is reached through the GENERATED invoker surface (invokers.gen.go:
//     genInvokers.ConstructionTransition* / genInvokers.GitStatus*). wfDeps/csWorkflows
//     carry no ConstructionTransition field; GitStatus survives as a plain
//     projectstate.GitActivityStatusAccess-typed field whose ONLY remaining role is
//     the nil-check "is the per-activity git head-state mirror wired" feature flag
//     (gitforward.go's gitEnabled/startedCred), never a direct method call.
//   - projectStateReader (the whole-aggregate read seam behind the last custom
//     Activity, ReadProjectActivity): GONE in the B8 follow-up. The shared
//     projectstate.ProjectEnvelope (envelope.go) was extended with the three
//     construction-fidelity sections the pump reads (ActivityConstruction /
//     ServiceContracts / ReviewPolicy), so the read now rides the GENERATED
//     designSessionAccess.readProjectOnBranch invoker with branch "" (main) and the
//     Manager's former local codec (codec.go) + activities_custom.go are deleted.
//     Construction now has NO custom Temporal Activities at all.
//
// How each dependency kind is reached differs by determinism class:
//   - the two Engines (intervention.InterventionEngine / review.ReviewEngine) are
//     PURE, deterministic, called DIRECTLY in-workflow (no Activity wrapper —
//     replay-safe) with fweng.Context{Context: context.Background()} supplied inline
//     at each call site (workflow.go / signals.go);
//   - the ResourceAccess ports are I/O and reached EXCLUSIVELY through the generated
//     invoker surface (Acts — invokers.gen.go/activities.gen.go).

// constructionActivity is the by-value activity snapshot the Manager's own workflow
// vocabulary uses broadly (eligibility.go, gitforward.go, dispatch) — CRLabel/IsRevert
// are the git-forward per-activity facts threaded into the PR open + the head-state
// mirror, and Phases is the resolved per-activity phase profile. Kind is the
// Manager-owned activityKind (Construction vs Noncoding), fed to activityKindName for
// the PR-body text.

// activityKind classifies a construction activity for display / PR-body purposes
// (Construction vs Noncoding). It was formerly the published handoff.ActivityKind;
// with the handOffEngine removed (agent-class selection is now review policy, not a
// worker-class cast) the Manager owns this small enum. The ordinal set is preserved
// (Unknown=0 … Noncoding=4) so the Temporal-payload wire form is unchanged.
type activityKind int

const (
	activityKindUnknown activityKind = iota
	activityKindDetailedDesign
	activityKindConstruction
	activityKindIntegration
	activityKindNoncoding
)

type constructionActivity struct {
	ActivityID   string
	Kind         activityKind
	ComponentID  string
	Layer        string
	EstimateDays float64
	CRLabel      string
	IsRevert     bool
	Phases       []projectstate.ActivityMethodPhase
	// Type/Variant are the classification the pump resolved ONCE
	// (projectstate.ClassifyActivity) at selection time, which every downstream
	// consumer then CARRIES rather than re-deriving: the Phases above, the review
	// policy's GatedPhasesByType key (activityTypeName), the head-state Type/Variant
	// stamp (RecordActivityStarted) and the per-phase slash command (CommandFor).
	// Classify once, carry the result — a second derivation is a second chance to
	// disagree, which is exactly how a phase profile and its commands drifted apart.
	// The zero pair (Service, Plan) is what a legacy Temporal payload decodes to,
	// preserving pre-change behavior for a workflow already in flight.
	Type    projectstate.ActivityType
	Variant projectstate.TestingVariant
}

// validatePhaseDecision rejects a gate key that is empty or outside the closed
// vocabulary the child workflow actually waits on: the five ActivityMethodPhase
// wire names plus mergeGateKey. The wire type is a bare string, and JSON Schema
// `required` only proves the KEY was sent — so "" (and any typo) reached the child
// workflow's phase gate as a signal that could never match a real gate, silently
// doing nothing. Closed vocabularies are validated here for the same reason
// SetReviewPolicy validates its preset and SetReviewCommentStatus its status.
//
// The merge gate accepts Approve ONLY: a merge has no draft to send back, and the
// hold ignores any other decision, so a SendBack on it would be another silent
// no-op presenting as success. It is refused here, where the operator sees it.
func validatePhaseDecision(phase string, decision PhaseDecision) error {
	if phase == mergeGateKey {
		if decision != PhaseApprove {
			return newError(fwmanager.ContractMisuse, fmt.Sprintf("the %q gate accepts Approve only — a merge has no draft to send back; steer the activity with OverrideActivity instead", mergeGateKey))
		}
		return nil
	}
	switch projectstate.ActivityMethodPhase(phase) {
	case projectstate.MethodPhaseRequirements, projectstate.MethodPhaseDetailedDesign,
		projectstate.MethodPhaseTestPlan, projectstate.MethodPhaseConstruction,
		projectstate.MethodPhaseIntegration:
		return nil
	}
	if strings.TrimSpace(phase) == "" {
		return newError(fwmanager.ContractMisuse, "empty phase")
	}
	return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown phase %q — expected one of requirements|detailed_design|test_plan|construction|integration|%s", phase, mergeGateKey))
}

// activityTypeName returns the canonical activity-type wire name
// ("service"/"frontend"/"testing"/…) for the activity's STAMPED type. These are the
// exact keys the ReviewPolicy's GatedPhasesByType map is keyed by (and the keys the
// webApp PolicyPanel must emit) — the gate consults the reviewEngine's
// ProposeReviews(activityTypeName(), phase, …), reading its RequiresHuman
// verdict. It reads the stamped Type rather than re-deriving from the id: re-deriving
// would let the gate map be keyed by a different type than the phases being walked.
func (a constructionActivity) activityTypeName() string {
	return a.Type.String()
}

// ===========================================================================
// constructionPipeline value vocabulary — the Manager's infrastructure-neutral
// dispatch spec / handle / observation. The pipeline ops are GENERATED and reached
// through the generated invoker surface (genInvokers.Pipeline*); these neutral types
// feed the workflow-side composition/mapping helpers (workflow.go) that bridge to the
// contract agenticjob.PipelineSpec / PipelineHandle / PipelineObservation.
// ===========================================================================

// pipelineHandle is the Manager's opaque handle.
type pipelineHandle struct {
	Name string
}

// ===========================================================================
// constructionInterventionPolicy resolves the composition-root's raw
// interventionMode STRING config into the published intervention.InterventionPolicy.
// ===========================================================================

func constructionInterventionPolicy(mode string) intervention.InterventionPolicy {
	switch mode {
	case "escalate-everything", "escalateEverything", "supervised":
		return intervention.InterventionPolicy{Mode: intervention.EscalateEverything}
	default:
		return intervention.InterventionPolicy{Mode: intervention.Tiered, RetryBudget: 2}
	}
}

// eligibility.go holds the pump's PURE eligibility selection over committed head-state
// (constructionManager.md §6.3 step 1) — the Manager's own workflow-side selection logic,
// deterministic and replay-safe (called directly in-workflow via the injected
// NextEligibleActivity helper). It was folded out of adapters.go so adapters.go carries
// only the engine boundary adapters; none of this touches Temporal or any RA seam.

// pumpVerdict is the pump's three-state selection outcome. It replaces the former
// (activity, bool) pair, whose false arm conflated "the network is drained" with
// "this activity cannot be dispatched" — the conflation that let a stalled network
// masquerade as a quiescent one for a whole benchmark run.
type pumpVerdict int

const (
	verdictQuiescent pumpVerdict = iota
	verdictDispatch
	verdictBlocked
)

// pumpSelection carries the verdict plus whichever payload it implies: the hydrated
// activity on verdictDispatch, the offending id + operator-facing reason + the
// discriminating FailureReason on verdictBlocked, nothing on verdictQuiescent.
// BlockedFailureReason picks the repair CLASS (componentId vs. dangling dependency
// id vs. dependency cycle); BlockedReason is the human-readable detail WITHIN that
// class — the governing rule is one variant per repair class, detail discriminates
// instances, never classes.
type pumpSelection struct {
	Activity             constructionActivity
	Verdict              pumpVerdict
	BlockedActivityID    string
	BlockedReason        string
	BlockedFailureReason projectstate.FailureReason
	// SkippedDesign names every design activity the scan walked past this tick. A design
	// activity is eligible work the CONSTRUCTION pump does not do (stage 4's
	// DeliveryManager does), so it is reported, never blocked — it rides every verdict,
	// including a dispatch of some later activity.
	SkippedDesign []string
}

// eligibilityRule is which activities the pump's selection may pick. It is chosen by the
// pump's GetVersion(changeLedgerPartialResume) — never by the selection itself — so a
// pump replaying a history recorded under the old rule re-selects exactly what it chose
// then (architect (D), D.2).
type eligibilityRule int

const (
	// eligibleNotStarted is the pre-D1 rule: only an activity whose effective state is
	// NotStarted (isActivityNotStarted).
	eligibleNotStarted eligibilityRule = iota
	// eligibleDispatchable is architect (D), D.1.2: also an integration-pending row, one no
	// pump wrote whose ledger holds some phases complete (isActivityDispatchable).
	eligibleDispatchable
)

// changeLedgerPartialResume is the ONE change id guarding D1 in both csWorkflows: the pump's
// widened selection and the construct workflow's ledger-aware start seed. A v1 pump only
// ever starts a v1 child, so every execution is wholly old or wholly new.
const changeLedgerPartialResume = "ledger-partial-resume"

// nextEligibleActivity resolves the next eligible construction activity for a project
// from its head-state. An activity is eligible iff the rule admits it (eligibleUnder) and
// every dep is satisfied, both read through projectstate.EffectiveConstructionPhase (the
// stored state where the pump wrote it, the attempt ledger where it did not) — an activity
// dependency requires a Done record, a milestone dependency is satisfied DERIVEDLY (it
// never has a Done record of its own; see projectstate.AllDepsSatisfied /
// projectstate.MilestonesByID). The dependency rule gates the activity's whole remaining
// lifecycle: a row resuming at Integration waits on exactly what a fresh row would.
// Iteration is ActivityList declaration order; the first eligible activity in that order
// is chosen (the candidate-list name tie-break below is currently unreachable, since
// declIdx is already unique per activity).
func nextEligibleActivity(proj projectstate.Project, rule eligibilityRule) pumpSelection {
	// Committed Network+ActivityList alone are not authorization to build: the
	// Phase-2 seal (AdvanceToConstruction — every slot committed, SDP review binding
	// an option) is what moves the project into PhaseConstruction. Selecting work
	// before that would start construction on an unvalidated project design.
	if proj.Phase != projectstate.PhaseConstruction {
		return pumpSelection{Verdict: verdictQuiescent}
	}
	network, activityList, ok := committedPlanInputs(proj)
	if !ok {
		return pumpSelection{Verdict: verdictQuiescent}
	}

	// itemByName is both the ActivityItem lookup AND the membership set of authored
	// activity names (the two ideas share exactly one key set, so one map serves both:
	// projectstate.ResolveDependencySatisfied/AllDepsSatisfied below only ever probe it for
	// presence via `_, isActivity := itemByName[depID]`).
	itemByName := make(map[string]projectstate.ActivityItem, len(activityList.Activities))
	for _, item := range activityList.Activities {
		itemByName[item.Name] = item
	}

	depsByActivity := make(map[string][]string, len(network.Dependencies))
	for _, dep := range network.Dependencies {
		depsByActivity[dep.Activity] = dep.DependsOn
	}

	milestones := projectstate.MilestonesByID(network)

	type candidate struct {
		declIdx  int
		activity string
	}
	var candidates []candidate
	// problemActivityID/problemReason/problemKind capture the FIRST authored-dependency
	// defect (an id naming neither an activity nor a milestone, or a milestone cycle)
	// encountered while scanning in declaration order — deterministic, since
	// activityList.Activities is an authored slice, never a map. It is used ONLY as
	// a fallback explanation when nothing else is eligible this tick (below): a
	// defect on an activity that ISN'T currently blocking progress must not halt
	// otherwise-dispatchable work, but a defect that WOULD otherwise present as an
	// ordinary quiet tick must not go unreported — that silent-quiescent disguise is
	// exactly the failure mode this change closes for milestone dependencies.
	var problemActivityID, problemReason string
	var problemKind projectstate.FailureReason
	// skippedDesign collects the design activities walked past below. The skip is
	// deliberately ahead of the dependency check: a design activity is not this pump's
	// work whatever its dependencies say, so the report names every unfinished one, not
	// only the one whose turn it happened to be.
	var skippedDesign []string
	for i, item := range activityList.Activities {
		name := item.Name
		if !eligibleUnder(rule, name, item, proj.ActivityExecution) {
			continue
		}
		if isDesignActivity(name, item) {
			skippedDesign = append(skippedDesign, name)
			continue
		}
		res := projectstate.AllDepsSatisfied(depsByActivity[name], itemByName, proj.ActivityExecution, milestones)
		if res.ProblemReason != "" {
			if problemReason == "" {
				problemActivityID, problemReason, problemKind = name, res.ProblemReason, res.ProblemKind
			}
			continue
		}
		if !res.Satisfied {
			continue
		}
		candidates = append(candidates, candidate{declIdx: i, activity: name})
	}
	if len(candidates) == 0 {
		if problemReason != "" {
			return pumpSelection{
				Verdict:              verdictBlocked,
				BlockedActivityID:    problemActivityID,
				BlockedFailureReason: problemKind,
				BlockedReason: fmt.Sprintf(
					"activity %s: %s — terminally failed; amending the committed network alone will NOT restart it (RecordActivityFailed is sticky and there is no reopen/retry path)",
					problemActivityID, problemReason),
				SkippedDesign: skippedDesign,
			}
		}
		return pumpSelection{Verdict: verdictQuiescent, SkippedDesign: skippedDesign}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].declIdx != candidates[j].declIdx {
			return candidates[i].declIdx < candidates[j].declIdx
		}
		return candidates[i].activity < candidates[j].activity
	})

	chosen := candidates[0].activity
	sel := dispatchSelectionFor(proj, chosen, itemByName[chosen])
	// The scan's skips ride whatever verdict the chosen activity produced: a tick that
	// dispatched something else still reports the design work it walked past.
	sel.SkippedDesign = append(skippedDesign, sel.SkippedDesign...)
	return sel
}

// isDesignActivity reports whether the construction pump must walk past this activity:
// ClassifyActivity types it but refuses it for dispatch, because running a design
// lifecycle's slash-command as a construction pipeline is the N-ENV defect in a new
// costume (08-30 S2 ruling). Stage 4's DeliveryManager is what dispatches these.
func isDesignActivity(name string, item projectstate.ActivityItem) bool {
	_, _, err := projectstate.ClassifyActivity(name, item.WorkerClass, item.Coding)
	return errors.Is(err, projectstate.ErrDesignActivityNotDispatchable)
}

// dispatchSelectionFor resolves the CHOSEN activity into its dispatchable selection:
// authored component identity, then classification. Both failure arms are plan
// defects that block terminally rather than dispatch a guess. Folded out of
// nextEligibleActivity so the eligibility scan and the chosen-activity resolution
// each stay under the complexity gate on their own.
func dispatchSelectionFor(proj projectstate.Project, chosen string, item projectstate.ActivityItem) pumpSelection {
	// Component identity is AUTHORED (spec §2.5): its presence declares the activity
	// structural, its absence declares it nonstructural or noncoding. ServiceContracts
	// play NO part in selection — requiring one was the chicken-and-egg that stalled
	// every fresh project, since the contract is produced by the detailed-design PHASE
	// of the very activity being selected.
	var comp *projectstate.Component
	if item.ComponentID != "" {
		comp = lookupComponent(proj, item.ComponentID)
		if comp == nil {
			return pumpSelection{
				Verdict:              verdictBlocked,
				BlockedActivityID:    chosen,
				BlockedFailureReason: projectstate.ComponentUnresolved,
				BlockedReason: fmt.Sprintf(
					"activity %s names component %q, which is not in the committed systemDesign — terminally failed; amending the committed activityList alone will NOT restart it (RecordActivityFailed is sticky and there is no reopen/retry path)",
					chosen, item.ComponentID),
			}
		}
	}
	// Classification is resolved HERE, once, from the three facts the committed plan
	// authored (id, workerClass, coding) — and the resolved pair then rides the
	// dispatched constructionActivity to every consumer. An activity the rules cannot
	// classify has no phase profile and no slash command, so dispatching it would mean
	// guessing: the id-prefix guess is what handed infra activity N-ENV a testing
	// command and killed it with VarianceExhausted. Block instead, as a plan defect.
	typ, variant, cerr := projectstate.ClassifyActivity(chosen, item.WorkerClass, item.Coding)
	// A design activity is CLASSIFIED and still refused: the scan above already walks
	// past it, so reaching here means some other path chose it, and going quiet is the
	// only safe answer. NEVER verdictBlocked — that writes RecordActivityFailed, which
	// is sticky and has no reopen path, so blocking here would terminally fail the very
	// activity stage 4 exists to run.
	if errors.Is(cerr, projectstate.ErrDesignActivityNotDispatchable) {
		return pumpSelection{Verdict: verdictQuiescent, SkippedDesign: []string{chosen}}
	}
	if cerr != nil {
		return pumpSelection{
			Verdict:              verdictBlocked,
			BlockedActivityID:    chosen,
			BlockedFailureReason: projectstate.ActivityUnclassifiable,
			BlockedReason: fmt.Sprintf(
				"activity %s has workerClass %q with coding=%v, which matches no activity-classification rule, so no phase profile or construction command can be resolved for it — terminally failed; repair by amending workerClass or coding in the committed activity list (RecordActivityFailed is sticky and there is no reopen/retry path)",
				chosen, item.WorkerClass, item.Coding),
		}
	}
	return pumpSelection{Verdict: verdictDispatch, Activity: hydrateConstructionActivity(chosen, item, comp, typ, variant)}
}

// lookupComponent resolves a component id against the committed systemDesign by EXACT
// id match. Returns nil when the slot is uncommitted/unpopulated or no component has
// that id — both are the caller's blocked case. No normalization, no name matching:
// the authored id is the identity (spec §2.1).
func lookupComponent(proj projectstate.Project, id string) *projectstate.Component {
	if proj.SystemDesign.Status != projectstate.ReviewCommitted {
		return nil
	}
	sys, ok := proj.SystemDesign.Model.(*projectstate.System)
	if !ok || sys == nil {
		return nil
	}
	for i := range sys.Components {
		if sys.Components[i].ID == id {
			return &sys.Components[i]
		}
	}
	return nil
}

// committedPlanInputs returns the committed typed Network + ActivityList head-state
// models the eligibility selection reads, or ok=false when either slot is not committed
// or not populated — the pump then has nothing to select.
func committedPlanInputs(proj projectstate.Project) (*projectstate.Network, *projectstate.ActivityList, bool) {
	if proj.Network.Status != projectstate.ReviewCommitted {
		return nil, nil, false
	}
	network, ok := proj.Network.Model.(*projectstate.Network)
	if !ok || network == nil {
		return nil, nil, false
	}
	if proj.ActivityList.Status != projectstate.ReviewCommitted {
		return nil, nil, false
	}
	activityList, ok := proj.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || activityList == nil {
		return nil, nil, false
	}
	return network, activityList, true
}

// eligibleUnder applies the pump's eligibility rule to one activity.
func eligibleUnder(rule eligibilityRule, activityID string, item projectstate.ActivityItem, status map[string]projectstate.ActivityExecution) bool {
	if rule == eligibleDispatchable {
		return isActivityDispatchable(activityID, item, status)
	}
	return isActivityNotStarted(activityID, item, status)
}

// isActivityDispatchable is the D1 eligibility (architect (D), D.1.2): the activity has
// no construction row, or no pump wrote its row (projectstate.PumpWroteRow is false) and
// its effective state is NotStarted or Running. A row no pump wrote that reads Running is,
// by construction, a ledger-partial row: its recorded phases are complete except some it
// has not run, so it is resumed at its first incomplete phase (loadReviewSnapshot's
// ledger-aware seed). A pump-written row never qualifies — RecordActivityStarted, the
// child's first durable write, makes PumpWroteRow true, so a row leaves this set before
// the pump can look again — and neither does a Done or Failed one.
func isActivityDispatchable(activityID string, item projectstate.ActivityItem, status map[string]projectstate.ActivityExecution) bool {
	s, exists := status[activityID]
	if !exists {
		return true
	}
	if projectstate.PumpWroteRow(s) {
		return false
	}
	effective, _ := projectstate.EffectiveConstructionPhase(s, item)
	return effective == projectstate.ActivityConstructionNotStarted || effective == projectstate.ActivityConstructionRunning
}

// isActivityNotStarted reports whether the activity has not started: it has no
// construction row, or its EFFECTIVE state is NotStarted. Effective, not stored:
// projectstate.EffectiveConstructionPhase lets the stored Phase win wherever the pump
// wrote it and reads the attempt ledger only where it did not, so a row whose history
// lives in the ledger alone (the backfill's rows: attempts, no stored phase fields) is
// never re-dispatched as if nothing had happened. item is the activity's committed
// ActivityItem — the ledger read needs its classification.
func isActivityNotStarted(activityID string, item projectstate.ActivityItem, status map[string]projectstate.ActivityExecution) bool {
	s, exists := status[activityID]
	if !exists {
		return true
	}
	effective, _ := projectstate.EffectiveConstructionPhase(s, item)
	return effective == projectstate.ActivityConstructionNotStarted
}

// hydrateConstructionActivity populates a constructionActivity from the activity id +
// its ActivityList item. Coding=true → Construction; Coding=false → Noncoding. comp is
// the resolved systemDesign component, or nil for a componentless (nonstructural or
// noncoding) activity — it supplies BOTH the ComponentID passed to the dispatch as
// component_id AND the Layer, which had no populator before this change and printed
// as an empty string into every PR body.
//
// typ/variant are the caller's ALREADY-RESOLVED classification (nextEligibleActivity's
// single ClassifyActivity call): this function stamps them and derives Phases from
// them, so the phase walk and the stamped pair can never name different profiles.
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, comp *projectstate.Component, typ projectstate.ActivityType, variant projectstate.TestingVariant) constructionActivity {
	kind := activityKindNoncoding
	if item.Coding {
		kind = activityKindConstruction
	}
	act := constructionActivity{
		ActivityID:   activityID,
		Kind:         kind,
		EstimateDays: item.EffortDays,
		Type:         typ,
		Variant:      variant,
		Phases:       projectstate.ProfileFor(typ, variant).PhaseIDs(),
	}
	if comp != nil {
		act.ComponentID = comp.ID
		act.Layer = comp.Layer.String()
	}
	return act
}

// gitactivities.go held the CUSTOM per-activity git head-state Record Activities
// (branch-open / CI-observed / arch-approved / merged / started / completed). B8
// (custom activities → generated, clean cut) migrated all six onto the GENERATED
// invoker surface (invokers.gen.go: genInvokers.GitStatus*), called directly from
// gitforward.go — the projectStateAccess §GIT-HEAD-STATE facet is now a real generated
// contract (projectstate.GitActivityStatusAccess), not a plain-goType dep temporalgen
// has no op for. This file now holds only the git-forward VALUE CARRIERS (Phase C
// folding candidates, per the task brief): the credential envelope, the PR-status
// projection, and the CI-state mapper.
//
// The PR-rail verbs (mint / OpenBranch / OpenPullRequest / GetPullRequestStatus /
// PostReview / MergePullRequest) are likewise GENERATED (activities.gen.go) and reached
// through the generated invoker surface (genInvokers.Rail*); the workflow-side value
// mapping (opaque-handle *FromString/*String marshalling, CheckState→CICheckState,
// cr-label→Hints) lives in gitforward.go.
//
// CRED OPACITY ACROSS THE RA SEAM: the rail returns a sourcecontrol.RepoCredential; the
// git head-state verbs take a projectstate.RepoCredential. These are
// structurally-identical-but-distinct opaque carriers (the NoSideways layer rule keeps
// projectstate from importing sourcecontrol — projectstate/credential.go). The Manager is
// the one seam allowed to touch both, so it converts (railCredEnvelope.toRail /
// toProjectState).

func (c railCredEnvelope) toProjectState() projectstate.RepoCredential {
	return projectstate.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

// ---------------------------------------------------------------------------
// git Activity option presets (constructionManager.md §6.4 pattern). Concrete
// RetryPolicy / timeout choices live here, in the Manager.
// ---------------------------------------------------------------------------

// wfDeps bundles every downstream dependency the constructionManager orchestrates,
// assembled by WorkerManifest (workermanifest.go) from the Manager's stored PUBLISHED
// deps and held on the csWorkflows struct. The three Engines are typed as their
// PUBLISHED contract interfaces (no Manager-local seam), called DIRECTLY in-workflow.
// The ResourceAccess layer is reached ENTIRELY through the generated invoker surface
// (Acts) — the whole-aggregate read included (B8 follow-up); the unit tests register
// contract-typed fakes behind the generated activity names. It is a package-internal
// builder input. There is no ProjectState/ConstructionTransition field anymore: the
// reads ride Acts.DesignSessionReadProjectOnBranch / Acts.ProjectStateReadProjectVersion
// and the cred-threaded writes ride Acts.ConstructionTransition* (B8).
type wfDeps struct {
	Intervention intervention.InterventionEngine
	Review       review.ReviewEngine

	// GitStatus is the OPTIONAL per-activity git head-state mirror (C-MCN-GIT). Its
	// writes are reached through the GENERATED invoker surface (Acts.GitStatus*); this
	// field's ONLY remaining role is the nil-check "is the mirror wired" feature flag
	// (gitforward.go's gitEnabled/startedCred) that gates the started/completed records
	// and the branch→PR→CI→+1→merge mirror.
	GitStatus projectstate.GitActivityStatusAccess

	// Acts is the GENERATED workflow-side call surface for the contract-backed RA
	// Activities (pipeline / artifact / rail); its Opts hook applies the per-op presets.
	Acts genInvokers

	// RailEnabled reports whether the PR-rail LIFECYCLE is available for construction
	// (impl.rail != nil AND impl.repo != nil): the rail dep alone is not enough — the
	// local profile now binds the GitLocal sourceControlAccess for the DESIGN managers
	// while construction keeps its local-merge-job flow (ConstructionManagerRepo stays
	// nil there), so a rail-without-repo boot must read as rail-dormant here or
	// runLocalMergeStep would skip and nothing would merge local activity branches.
	// It gates the PR-rail lifecycle (gitEnabled) alongside GitStatus + Repo.
	RailEnabled bool

	// Repo resolves the per-project RepoRef the rail verbs address. nil ⇒ the
	// PR-rail lifecycle is dormant (no repo to open branches/PRs in).
	Repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// NextEligibleActivity resolves the next eligible construction activity for a
	// project from its head-state (the Manager's own pure selection), under the
	// eligibility rule the pump's GetVersion chose.
	NextEligibleActivity func(proj projectstate.Project, rule eligibilityRule) pumpSelection

	// InterventionPolicy is the project's committed policy snapshot the Manager feeds
	// the interventionEngine by value, typed DIRECTLY as the Engine's own published
	// input. It is resolved ONCE from the composition root's raw interventionMode config
	// via constructionInterventionPolicy (WorkerManifest() — the SAME fixed value every
	// DecideOnVariance / ApplyPausePolicy call fed under the retired per-call adapter
	// conversion).
	InterventionPolicy intervention.InterventionPolicy

	// EscalationWaitTimeout bounds how long an escalated/architectOnly activity waits
	// for an operator override before it terminally FAILS the activity. 0 == wait-forever.
	EscalationWaitTimeout time.Duration
}

// csWorkflows is the single constructionManager component struct — the workflow receiver
// (it no longer hosts any Activity methods; every RA op is reached through the
// generated invoker surface, Acts).
type csWorkflows struct {
	Intervention intervention.InterventionEngine
	Review       review.ReviewEngine

	GitStatus projectstate.GitActivityStatusAccess

	Acts genInvokers

	RailEnabled bool
	Repo        func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	NextEligibleActivity  func(proj projectstate.Project, rule eligibilityRule) pumpSelection
	InterventionPolicy    intervention.InterventionPolicy
	EscalationWaitTimeout time.Duration
}

// csNewWorkflows builds the csWorkflows receiver from the injected seams.
func csNewWorkflows(d wfDeps) *csWorkflows {
	return &csWorkflows{
		Intervention:          d.Intervention,
		Review:                d.Review,
		GitStatus:             d.GitStatus,
		Acts:                  d.Acts,
		RailEnabled:           d.RailEnabled,
		Repo:                  d.Repo,
		NextEligibleActivity:  d.NextEligibleActivity,
		InterventionPolicy:    d.InterventionPolicy,
		EscalationWaitTimeout: d.EscalationWaitTimeout,
	}
}

// ---------------------------------------------------------------------------
// Activity option presets (constructionManager.md §6.4). Concrete RetryPolicy /
// timeout choices live here, in the Manager.
// ---------------------------------------------------------------------------

// csReadProjectActivityOptions is the read preset VALUE (10s; NotFound+ContractMisuse
// terminal) the manifest's Opts hook (workermanifest.go) applies to the two GENERATED
// read invokers the csWorkflows consume — "projectStateAccess.readProjectVersion" and
// "designSessionAccess.readProjectOnBranch" (the whole-aggregate read) — identically
// for both. NotFound stays terminal so a brand-new project's read fails fast into the
// pump's quiet-tick handling (isReadNotFound) instead of retrying.
func csReadProjectActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// submitPipelineActivityOptions / observePipelineActivityOptions are the pipeline preset
// VALUES the manifest's Opts hook (workermanifest.go) applies to the GENERATED pipeline
// invokers by registered name (submit 60s Auth/ContractMisuse-terminal;
// observe/cancel 30s NotFound/Auth-terminal).
func submitPipelineActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    60 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.ContractMisuse},
	}.Options()
}

func observePipelineActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.Auth},
	}.Options()
}

// recordActivityOptions is the head-state Record-verb preset VALUE (10s; ContractMisuse
// terminal only — Conflict must reach the workflow so the §6.5 re-read→re-apply loop can
// recover it) the manifest's Opts hook (workermanifest.go) applies to the GENERATED
// constructionTransitionAccess / gitActivityStatusAccess Record* invokers by registered
// name. Every Record* verb goes through the generated invoker surface, so only the
// VALUE form is needed (no direct-ExecuteActivity call site for this preset).
func recordActivityOptions() workflow.ActivityOptions {
	return fwmanager.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
}

// stampNoteDeliveredActivityOptions is the delivery stamp's preset (M4): the record
// preset's per-attempt timeout, uncapped attempts inside noteStampRetryWindow, and
// ContractMisuse terminal (the store refusing to stamp one note to two attempts).
func stampNoteDeliveredActivityOptions() workflow.ActivityOptions {
	o := recordActivityOptions()
	o.ScheduleToCloseTimeout = noteStampRetryWindow
	return o
}

// isRAContractMisuse reports whether err is (or wraps) an RA ContractMisuse.
func isRAContractMisuse(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raContractMisuseErrType
	}
	return false
}

// constructState is the live technical state backing the sessionState Query.
type constructState struct {
	projectID     ProjectID
	activityID    ActivityID
	stage         ConstructionStage
	pipelinePhase *PipelinePhase
	reviewSet     *ReviewSet
	// reviewSetError is why reviewSet is nil at the current gate ("" when the engine answered).
	reviewSetError string
	variance       *FlaggedVariance

	// completedPhases is the LIVE in-memory skip-guard the phase loop consults so an
	// already-completed phase is never re-dispatched or re-gated. It is SEEDED at
	// workflow start from the start-snapshot activity's PhaseCompletion slice and
	// MARKED unconditionally on EVERY phase completion (Approve / no-gate / inert) —
	// independent of gitOn. This is what stops the outer variance-retry loop (which
	// re-walks phases from index 0) from re-gating an already-approved phase across a
	// non-git execution where no head-state completion record exists to re-read.
	completedPhases map[projectstate.ActivityMethodPhase]bool

	// redraftExhausted reports that the phase gate the workflow is waiting at can take no
	// further SendBack redraft: its human-paced budget (maxPhaseRedrafts) is spent. It does
	// NOT fail the activity or re-enter the variance loop — the gate keeps awaiting the
	// human; the flag surfaces that redrafting is spent. RECOMPUTED on entry to every gate
	// (B1.2): it used to be set once and never reset, so it leaked into every later gate of
	// the same run (plan G5).
	redraftExhausted bool

	// awaitingGate / awaitingSince / awaitingUntil describe the human stage the workflow is
	// in right now (B1.2): which gate (a lifecycle phase's wire name, mergeGateKey or
	// takeoverGateKey), when THIS occurrence of it began, and — for an escalation with a
	// bounded wait — when it gives up. awaitingSince is workflow.Now, so a query served by
	// replay rebuilds the original time, and a redraft re-entering its gate starts a new
	// occurrence. Written only by enterHumanStage and cleared only by leaveHumanStage.
	awaitingGate  string
	awaitingSince time.Time
	awaitingUntil *time.Time

	// attempt is the current supervision attempt, 1-based (set by runAttempt); 0 before
	// the first attempt.
	attempt int

	// reviewContracts is the per-execution set of contract identifiers captured from
	// the start-snapshot project (B5) and fed to reviewEngine.ProposeReviews so the
	// gate's reviewer set is display-populated without re-reading mid-loop.
	reviewContracts []string

	// floorTouched is the Task 7 non-overridable-floor snapshot: whether the
	// activity's committed contract (start-snapshot, B5-style — never re-read
	// mid-loop) touches deploy/spend/schema (projectstate.ContractTouchesReviewFloor).
	// Consulted by runPhaseGate via the reviewEngine's ProposeReviews (this is the
	// floor flag it passes) to force a human gate at MethodPhaseConstruction
	// regardless of preset, including "vibes".
	floorTouched bool

	// mergeCompleted is the LIVE in-memory skip-guard for the local merge step
	// (local-merge-and-policy Commit 1, same discipline as completedPhases):
	// marked once the merge job landed, so a variance retry of a LATER finalize
	// fault does not re-dispatch a merge whose activity branch is already
	// merged and deleted (which would honestly — and wrongly — fail).
	mergeCompleted bool

	// taskAttempts counts, per Figure A-1 task (MethodTask), how many times a pipeline
	// has been dispatched for that task's phase on this activity — seeded at start from
	// the row's attempt ledger (loadReviewSnapshot, v1 of changeLedgerPartialResume), so
	// a new run's AttemptIDs continue the ledger's instead of colliding with them — the join key
	// projectstate.AttemptID needs to attribute an episode to the (activity, task,
	// attempt) it was actually burned on (Task 10, constructactivity.go). It counts
	// across BOTH the outer variance-retry loop and a gated phase's human-paced redraft
	// loop, since both re-enter runPipeline for the SAME phase. Workflow-local (rebuilt
	// deterministically on replay, never persisted); lazily initialized by
	// constructState.nextTaskAttempt so a state that never dispatches a pipeline
	// (ProjectSupervisionWorkflow's) allocates nothing.
	taskAttempts map[projectstate.MethodTask]int

	// noteDelivery is true on an execution that recorded the operator-note-delivery
	// marker (plan B1.4): only then are notes recorded, carried and stamped, and the
	// managed scaffold synced before a GitHub-venue dispatch.
	noteDelivery bool
	// noteSeq numbers the notes this run records (operatorNoteID).
	noteSeq int
	// pendingNotes are the notes the next agent dispatch carries, oldest first: seeded
	// from the row at start (projectstate.PendingOperatorNotes), appended to as notes are
	// recorded, and each dropped once a dispatch carried it whole and it was stamped.
	pendingNotes []projectstate.OperatorNote
	// carriedTo names, per note id, the last attempt a dispatch carried the note into,
	// so a note is never carried twice into the same attempt (M4).
	carriedTo map[string]string

	// executionLedger is true on an execution that recorded the execution-ledger marker
	// (changeExecutionLedger, stage 3): only then does this run WRITE what it does to the
	// per-activity attempt and review-round ledgers. An execution that recorded no marker
	// stays wholly on the retired facet — no activity opened, no attempt recorded, no
	// round opened, no verdict appended — because its history holds no events for those
	// Activities and never will.
	executionLedger bool

	// activityVersion is this run's copy of the per-activity CAS token: the version the
	// activity's own execution row was at the last time this workflow wrote it. It is
	// deliberately NOT headVersion — headVersion is the whole document's token, and two
	// children writing DIFFERENT activities would contend on it while never touching each
	// other's rows. This one is scoped to the row, so it refuses exactly the interleaving
	// that matters and nothing else, which is the guard 4b's parallel pump rests on.
	//
	// Seeded at session start from the row the start snapshot already read
	// (loadReviewSnapshot), 0 for an activity with no row yet — which is
	// projectstate.NoActivityVersionExpectation, the honest posture of a writer about to
	// BIRTH the row. Advanced by rowAdvanced on every applied transition.
	activityVersion int64

	// workAttemptID is the AttemptID of the last AGENT-WORK dispatch runPipeline minted.
	// The gate that follows judges exactly that attempt, so it is what the round cites as
	// its subject and what every verdict on that round names — the join that makes a
	// verdict traceable to the work it judged and to the episode that burned it.
	workAttemptID string

	// gate is the execution-ledger identity of the review round the workflow is at right
	// now. Exactly one gate is live at a time (the phase walk is sequential), so this is
	// one value rather than a map; it is rebuilt deterministically on replay like every
	// other workflow-local field.
	gate gateLedger

	// ephemeralNotes are the ids of the workflow-local notes that carry a send-back's
	// feedback into the redraft WITHOUT being recorded (stage 3): the round IS the record
	// of the send-back now, so a NoteSendBack beside it would be one fact stored twice.
	// They render into the dispatch block like any other note and are never stamped
	// delivered, because there is no stored note to stamp.
	ephemeralNotes map[string]bool
}

// rowAdvanced records that ONE transition applied to the activity's execution row, which
// is exactly what the store stamped on it: BOTH write paths onto a row — the facet's
// (withActivityVersion) and the retired facets' (upsertActivityExecution) — advance the
// stored counter by one per applied transition, and neither advances it on a refusal.
//
// EVERY verb that writes the row calls this, including the retired facets' verbs, which
// still write these rows for the length of the wave (RecordPhaseStarted,
// RecordChangeReviewed, RecordOperatorNote, RecordOperatorNoteDelivered all run on a
// ledger-on execution). A counter that tracked only the new rail would go stale on the
// first write from the old one, and the next CAS would then refuse a caller that is not
// stale at all — a self-inflicted conflict the re-read arm cannot resolve, because
// applyRecovering re-reads the PROJECT version and no row.
func (s *constructState) rowAdvanced() { s.activityVersion++ }

// gateLedger is the execution-ledger identity of ONE gate occurrence: the review task it
// belongs to, its 1-based number, the round id derived from the pair, the subject the
// round judges and who decided it.
//
// The number is BOTH the round's and its gate attempt's, deliberately: one gate
// occurrence is one round and one attempt at the review task, so giving them separate
// counters would let the two ledgers disagree about which review a passing gate came
// from. nextTaskAttempt is the single counter, seeded from whichever of the two ledgers
// has gone further (seedResumeFromLedger).
type gateLedger struct {
	task    projectstate.MethodTask
	number  int
	roundID string
	subject projectstate.SubjectRef
	// actor is who passed or rejected the gate — stamped when the round is decided and
	// read back by the gate attempt the completion writes.
	actor projectstate.TaskActor
}

func (s *constructState) view() (ConstructionSessionView, error) {
	aid := s.activityID
	v := ConstructionSessionView{
		ProjectID:        s.projectID,
		ActivityID:       &aid,
		Stage:            s.stage,
		PipelinePhase:    s.pipelinePhase,
		ReviewSet:        s.reviewSet,
		Variance:         s.variance,
		RedraftExhausted: s.redraftExhausted,
		Attempt:          int64(s.attempt),
		AttemptBudget:    maxVarianceAttempts,
	}
	if s.reviewSetError != "" {
		e := s.reviewSetError
		v.ReviewSetError = &e
	}
	if s.awaitingGate != "" {
		gate, since := s.awaitingGate, s.awaitingSince
		v.AwaitingGate, v.AwaitingSince = &gate, &since
		if s.awaitingUntil != nil {
			until := *s.awaitingUntil
			v.AwaitingUntil = &until
		}
	}
	return v, nil
}

// operatorPauseSignal is the operatorPauseRequested payload (constructionManager.md
// §2.3). The Reason rides on the signal and is safe to log.
type operatorPauseSignal struct {
	ProjectID ProjectID
	Reason    string
}

// workermanifest.go is the hand-written bridge between the generated Temporal layer
// (activities.gen.go / invokers.gen.go / worker.gen.go) and the constructionManager
// impl. It supplies the genWorkerManifest RegisterWorker consumes: the four workflow
// bodies under their registered names, the per-activity option-preset hook, and the
// genActivities dep threading. It also hosts the external RegisterManagerWorker
// entrypoint the composition root calls (cmd/server/main.go).
//
// B8 (custom activities → generated, clean cut) + its follow-up migrated ALL of the
// former 14 CUSTOM Activities (activities_custom.go / gitactivities.go, both since
// deleted or reduced to value carriers) onto the GENERATED invoker surface — the last
// one, the whole-aggregate ReadProjectActivity, once the shared
// projectstate.ProjectEnvelope grew the construction-fidelity sections the pump reads
// (envelope.go). Registration is now ENTIRELY automatic via the generated
// RegisterWorker (worker.gen.go), which registers every genActivities op
// unconditionally; construction has NO hand-registered Activities. This also closed a
// real pre-existing defect: since B6 dropped the CustomActivities manifest surface,
// NONE of the 14 custom Activities had been registered in production — any
// workflow.ExecuteActivity call reaching one would have failed with "unable to find
// activity type" on a real worker (the identical systemic gap billing's B7 rewire
// found for its 3 revenue-ledger ops).
//
// The two Engines (intervention / review) are called DIRECTLY in-workflow
// (deterministic, by value) and are NOT Activities; the durableExecutionAccess in-workflow
// primitives (awaitSignal / startTimer / executeChild) are the Manager's own code.

// Signal and query names (constructionManager.md §6.1/§6.2).
const (
	// signalOperatorPauseRequested resumes a suspended construction execution at
	// its awaitSignal; backs PauseProject (NCUC2).
	signalOperatorPauseRequested = "operatorPauseRequested"
	// signalOperatorOverride resumes a per-activity child workflow; backs
	// OverrideActivity.
	signalOperatorOverride = "operatorOverride"
	// signalPhaseDecision delivers a phase-gated approval/send-back decision to a
	// per-activity child workflow; backs SubmitPhaseDecision.
	signalPhaseDecision = "phaseDecision"
	// queryPumpDispatch returns THIS pump run's pumpDispatch decision; backs the
	// synchronous dispatch outcome ExecuteNextActivity returns WITHOUT awaiting the
	// background self-cascade drain (constructionManager.md §2.1).
	queryPumpDispatch = "pumpDispatchDecision"
)

// ExecutionKinds — the registered workflow names (constructionManager.md §6.2).
const (
	// executionKindPump is PumpNextActivityWorkflow — the project's ONE pump,
	// {projectId}:nextActivity, started or joined by ExecuteNextActivity and by the
	// 30s pump sweep (not one execution per tick).
	executionKindPump = "constructionPumpNextActivity"
	// executionKindConstructActivity is the per-activity child workflow.
	executionKindConstructActivity = "constructionConstructActivity"
	// executionKindReplanSweep is the per-tick ReplanSweepWorkflow (the 5m sweep).
	executionKindReplanSweep = "constructionReplanSweep"
	// executionKindProjectSupervision is the long-lived project-level supervision
	// workflow that hosts the operator-pause branch + project-level session Query.
	executionKindProjectSupervision = "constructionProjectSupervision"
	// executionKindPumpSweep is the Schedule-triggered, platform-wide fan-out
	// (the 30s pump sweep; pumpsweep.go) — the actual Schedule target, since a
	// Schedule cannot itself vary executionKindPump's ProjectID per firing.
	executionKindPumpSweep = "constructionPumpSweep"
)

// Schedule ids + cadences (constructionManager.md §6.1; Task 7c). Namespaced with
// the manager's own name (mirroring operations' "operations:operatedStateReconcile"
// over billing's bare "shortfallSweep") since Schedule ids are namespace-global —
// a manager-scoped prefix keeps two managers from ever colliding on one.
//
// STAGE 4a RENAMED THE PREFIX construction: → delivery:, because the manager whose
// name it carries no longer exists: both sweeps now register from, and fire into, the
// ONE delivery Manager and its `delivery` task queue. The workflow TYPE names keep
// their construction* spelling (R2 — a rename there would strand in-flight
// executions); only the two Schedule ids move.
//
// AT CUTOVER THE OLD IDS MUST BE DELETED, and by hand. RegisterSchedules creates an
// absent Schedule and is a harmless no-op on a present one — it cannot MOVE a
// Schedule's task queue, and it never deletes. So `construction:pumpSweep` and
// `construction:replanSweep` survive this release as Schedules whose action targets
// the dead `construction` queue that no worker polls: a silent dead sweep, not an
// error. Worse, re-registering under the SAME id would ADOPT the old Schedule rather
// than replace it, which is precisely why the id had to change instead. Run
// `temporal schedule delete --schedule-id construction:pumpSweep` (and
// `construction:replanSweep`) BEFORE the release, then confirm with
// `temporal schedule list` — see docs/bugs/2026-09-24-stage3-rail-earmarks.md.
const (
	// scheduleIDPumpSweep is the platform-wide pump-sweep Schedule id.
	scheduleIDPumpSweep = "delivery:pumpSweep"
	// pumpSweepIntervalSecs is the pump-sweep cadence — the single tunable knob.
	pumpSweepIntervalSecs = 30

	// scheduleIDReplanSweep is the platform-wide replan-sweep Schedule id.
	scheduleIDReplanSweep = "delivery:replanSweep"
	// replanSweepIntervalSecs is the replan-sweep cadence (5m) — the single tunable knob.
	replanSweepIntervalSecs = 5 * 60
)

// csActivityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities. A name with no entry falls back to the generated
// default (invokers.gen.go). Keyed by the generated registered activity name
// (<componentKey>.<opName>), including the 14 head-state Record*/read presets
// (recordOpts / readProjectOpts's VALUE forms — recordActivityOptions /
// csReadProjectActivityOptions, workflow.go).
func csActivityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"agenticJobAccess.submitAgenticJob":        submitPipelineActivityOptions(),
		"agenticJobAccess.observeAgenticJob":       observePipelineActivityOptions(),
		"agenticJobAccess.cancelAgenticJob":        observePipelineActivityOptions(),
		"sourceControlAccess.getInstallationToken": mintCredActivityOptions(),
		"sourceControlAccess.openBranch":           railActivityOptions(),
		"sourceControlAccess.openPullRequest":      railActivityOptions(),
		"sourceControlAccess.getPullRequestStatus": railActivityOptions(),
		"sourceControlAccess.postReview":           railActivityOptions(),
		"sourceControlAccess.mergePullRequest":     railActivityOptions(),
		// B8 (+ follow-up): re-keyed from the retired custom-Activity call sites onto
		// their generated registered names, preserving the identical timeout/retry scope.
		// designSessionAccess.readProjectOnBranch is the whole-aggregate read the pump
		// runs (branch "" ⇒ main) — the former ReadProjectActivity preset.
		"designSessionAccess.readProjectOnBranch":           csReadProjectActivityOptions(),
		"projectStateAccess.readProjectVersion":             csReadProjectActivityOptions(),
		"constructionTransitionAccess.recordChangeReviewed": recordActivityOptions(),
		"constructionTransitionAccess.recordActivityExited": recordActivityOptions(),
		"constructionTransitionAccess.recordActivityFailed": recordActivityOptions(),
		"constructionTransitionAccess.recordOperatorPaused": recordActivityOptions(),
		"constructionTransitionAccess.recordPhaseStarted":   recordActivityOptions(),
		"constructionTransitionAccess.recordPhaseCompleted": recordActivityOptions(),
		// B1.4: the operator note and its delivery stamp are head-state Record verbs.
		"constructionTransitionAccess.recordOperatorNote": recordActivityOptions(),
		// The delivery stamp has its own bounded envelope (M4): it follows a submit that
		// already dispatched the job, so it retries rather than fail the run.
		"constructionTransitionAccess.recordOperatorNoteDelivered": stampNoteDeliveredActivityOptions(),
		// C.1.4: the managed-scaffold sync before a GitHub-venue dispatch is a rail verb.
		"sourceControlAccess.syncManagedScaffold":            railActivityOptions(),
		"gitActivityStatusAccess.recordActivityBranchOpened": recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityCIObserved":   recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityArchApproved": recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityMerged":       recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityStarted":      recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityCompleted":    recordActivityOptions(),
		// SP1 capture-seam: the episode ledger append rides its OWN envelope, never a
		// business one (see appendEpisodeActivityOptions).
		"episodeAccess.appendEpisode": appendEpisodeActivityOptions(),
		// EXECUTION LEDGER (stage 3, changeExecutionLedger): the attempt and review-round
		// writes are head-state Record verbs and take the Record preset for the same
		// reason — ContractMisuse terminal, and Conflict deliberately NOT, so the §6.5
		// re-read→re-apply loop in applyRecovering is what resolves it rather than a
		// Temporal retry re-issuing the same stale expected version forever.
		"activityExecutionAccess.openActivity":          recordActivityOptions(),
		"activityExecutionAccess.recordAttemptOutcome":  recordActivityOptions(),
		"activityExecutionAccess.openReviewRound":       recordActivityOptions(),
		"activityExecutionAccess.appendReviewVerdict":   recordActivityOptions(),
		"activityExecutionAccess.decideReviewRound":     recordActivityOptions(),
		"activityExecutionAccess.recordActivityOutcome": recordActivityOptions(),
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the four workflow bodies under their registered names, the per-activity option-preset
// hook, and the genActivities threaded from the impl's stored published deps.
// railLifecycleEnabled derives wfDeps.RailEnabled: the PR-rail LIFECYCLE needs BOTH
// the rail dep and the per-project repo resolver. The rail dep alone is not enough —
// the local profile binds the GitLocal sourceControlAccess for the DESIGN managers
// while construction stays repo-less there (its ConstructionManagerRepo hook returns
// nil), and a rail-without-repo boot must read as rail-dormant or runLocalMergeStep
// would skip and nothing would merge local activity branches.
func railLifecycleEnabled(rail sourcecontrol.SourceControlAccess, repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)) bool {
	return rail != nil && repo != nil
}

func (m *constructionManager) WorkerManifest() genWorkerManifest {
	optsHook := csActivityOptions()
	wf := csNewWorkflows(wfDeps{
		Intervention: m.intervention,
		Review:       m.review,
		// GitStatus's ONLY remaining role is the "is the mirror wired" nil-check feature
		// flag (gitforward.go) — its writes are reached through Acts.GitStatus* (B8), so
		// no type-assertion onto a local seam is needed; m.gitActivityStatus already
		// speaks projectstate.GitActivityStatusAccess directly (constructionmanager.go).
		GitStatus: m.gitActivityStatus,
		Acts:      genInvokers{Opts: optsHook},
		// RailEnabled gates the PR-rail lifecycle (gitEnabled) alongside GitStatus + Repo.
		// Repo (B5) is the per-project venue resolver: non-nil retargets every construction
		// dispatch to the project's own repo (aiarch-construct.yml) AND activates the
		// branch→PR rail; nil keeps the central-repo fallback + dormant rail. The repo
		// resolver is part of the derivation (not just the runWithGitForward composite) so
		// the local GitLocal rail — bound for the design managers, repo-less for
		// construction — keeps runLocalMergeStep firing (see the wfDeps.RailEnabled doc).
		RailEnabled:           railLifecycleEnabled(m.rail, m.repo),
		Repo:                  m.repo,
		NextEligibleActivity:  nextEligibleActivity,
		InterventionPolicy:    constructionInterventionPolicy(m.interventionMode),
		EscalationWaitTimeout: m.escalationWaitTimeout,
	})

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindPump, Fn: wf.PumpNextActivityWorkflow},
			{Name: executionKindConstructActivity, Fn: wf.ConstructActivityWorkflow},
			{Name: executionKindReplanSweep, Fn: wf.ReplanSweepWorkflow},
			{Name: executionKindProjectSupervision, Fn: wf.ProjectSupervisionWorkflow},
			{Name: executionKindPumpSweep, Fn: wf.PumpSweepWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:           m.projectState,
			Artifact:               m.artifact,
			Pipeline:               m.pipeline,
			Rail:                   m.rail,
			ConstructionTransition: m.constructionTransition,
			GitStatus:              m.gitActivityStatus,
			Episodes:               m.episodes,
			DesignSession:          m.designSession,
			MessageBus:             m.messageBus,
			// The execution ledger's twelve activities are registered by worker.gen.go the
			// moment the dep exists; THREADING it is what gives them something to call. It
			// was taken but not threaded while nothing invoked them (stage 3 task 3), which
			// a task-5 write would have found as a nil-receiver panic inside the Activity.
			ActivityExecution: m.activityExecution,
		},
	}
}

// ===========================================================================
// messageBusSeam — mirrors billingManager's/operationsManager's narrow startup
// seam (internal/utility/messagebus). ONLY the startup RegisterSchedule verb is
// consumed here; the workflow-invoked category-B verbs (registerSchedule /
// deliverSignal, reached through the generated invokers per Acts.MessageBus*)
// already speak the real messagebus.MessageBus contract types directly and need
// no adapter. The in-workflow primitives (awaitSignal / startTimer / executeChild)
// are the Manager's OWN workflow code (D-DA category A), NOT bus verbs.
// ===========================================================================

// messageBusSeam is the Manager's consumer view for the STARTUP Schedule
// registration only. UNEXPORTED; the folded adapter below bridges the published
// messagebus.MessageBus to it.
type messageBusSeam interface {
	// RegisterSchedule registers (idempotently, by id) a recurring Schedule.
	RegisterSchedule(ctx context.Context, spec scheduleSpec) error
}

// scheduleSpec mirrors messagebus.ScheduleSpec for the two Schedules this Manager
// registers at startup. The composition root adapts the concrete utility.
type scheduleSpec struct {
	ID           string
	WorkflowType string
	TaskQueue    string
	IntervalSecs int
}

// messageBusAdapter adapts the published messagebus.MessageBus onto messageBusSeam.
// Only the startup RegisterSchedule verb is consumed (the published ScheduleSpec
// resolves the task queue via its KindBinding table, so the seam's TaskQueue is not
// threaded).
type messageBusAdapter struct {
	inner messagebus.MessageBus
}

var _ messageBusSeam = messageBusAdapter{}

func (a messageBusAdapter) RegisterSchedule(ctx context.Context, spec scheduleSpec) error {
	return a.inner.RegisterSchedule(
		fwra.Context{Context: ctx},
		messagebus.ScheduleID(spec.ID),
		messagebus.ScheduleSpec{
			ExecutionKind: messagebus.ExecutionKind(spec.WorkflowType),
			Cadence:       messagebus.Cadence{Every: time.Duration(spec.IntervalSecs) * time.Second},
		},
	)
}

// RegisterSchedules registers (idempotently) the TWO platform-wide delivery
// Temporal Schedules at startup via the messageBus utility (constructionManager.md
// §6.1; Task 7c): the pump sweep (30s — targets PumpSweepWorkflow, which fans out to
// every construction-phase project's own PumpNextActivityWorkflow; see this file's
// header + pumpsweep.go) and the replan sweep (5m — targets ReplanSweepWorkflow with
// no ProjectID, its existing "sweep all in-flight projects" scope). Called once at
// process start; a re-registration with the same id+spec is a harmless no-op
// (last-writer-wins Update, messagebus.go).
func RegisterSchedules(ctx context.Context, bus messagebus.MessageBus) error {
	adapter := messageBusAdapter{inner: bus}
	if err := adapter.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDPumpSweep,
		WorkflowType: executionKindPumpSweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: pumpSweepIntervalSecs,
	}); err != nil {
		return err
	}
	return adapter.RegisterSchedule(ctx, scheduleSpec{
		ID:           scheduleIDReplanSweep,
		WorkflowType: executionKindReplanSweep,
		TaskQueue:    TaskQueue,
		IntervalSecs: replanSweepIntervalSecs,
	})
}

// ---------------------------------------------------------------------------
// Episode facet read ops (SP1 capture-seam, Task 9 — founder ruling 2026-08-02:
// episode observability is a facet of the existing use cases, not a new
// episodeManager). Both ops are PLAIN METHODS that consult episodeAccess directly
// — no Temporal — the same shape as systemDesignManager.ListProjects/GetProject.
// The whole-project exportEpisodes op is cut from v1 (per-target export is
// client-side, Task 10).
// ---------------------------------------------------------------------------

// ListEpisodesForActivity returns every episode record (dispatch runs, or gaps)
// captured against one construction activity, in episodeAccess's own (append)
// order. A pass-through over episodeAccess.ListEpisodes scoped by
// TargetRef=activityID, mapped to the contract EpisodeRecordView.
func (m *constructionManager) ListEpisodesForActivity(rc fwmanager.Context, projectID ProjectID, activityID string) ([]EpisodeRecordView, error) {
	ctx := rc.Context
	if projectID == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return nil, newError(fwmanager.ContractMisuse, "empty activityId")
	}
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{
		ProjectID: episode.ProjectID(projectID),
		TargetRef: &activityID,
	})
	if err != nil {
		return nil, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	return csEpisodeRecordViews(records), nil
}

// GetEpisodeTimeline returns one episode's full timeline: its ledger record plus
// the sequenced trace events mined from its run. NotFound if episodeID does not
// name a record on this project.
func (m *constructionManager) GetEpisodeTimeline(rc fwmanager.Context, projectID ProjectID, episodeID string) (EpisodeTimeline, error) {
	ctx := rc.Context
	if projectID == "" {
		return EpisodeTimeline{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if episodeID == "" {
		return EpisodeTimeline{}, newError(fwmanager.ContractMisuse, "empty episodeId")
	}
	// ListEpisodes has no by-id lookup (episodeAccess.md — the ledger is append-
	// scanned by TargetRef); querying with no TargetRef and finding the one record
	// whose EpisodeID matches is the only way to resolve one episode across every
	// target on the project.
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{ProjectID: episode.ProjectID(projectID)})
	if err != nil {
		return EpisodeTimeline{}, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	rec, ok := findEpisodeRecord(records, episodeID)
	if !ok {
		return EpisodeTimeline{}, newError(fwmanager.NotFound, fmt.Sprintf("episode %q not found", episodeID))
	}
	// A GAP record (episode.EpisodeGap — the dispatch that produced no summary at
	// all) has no trace file: TracePath is nil on the ledger record. The
	// never-silent gap doctrine (Task 2/7) treats a gap as a PRESENT, first-class
	// outcome, not an absence — the record itself must always resolve; only its
	// timeline is empty. Skip the RA round-trip entirely when TracePath says
	// there is nothing to read, and treat a NotFound FROM ReadTraceEvents (e.g. a
	// TracePath that no longer resolves) the same way, rather than erroring the
	// whole timeline — either would otherwise be indistinguishable from an
	// unknown episodeID.
	if rec.TracePath == nil || *rec.TracePath == "" {
		return EpisodeTimeline{Record: csEpisodeRecordToView(rec), Events: episodeTimelineEvents(nil)}, nil
	}
	raw, err := m.episodes.ReadTraceEvents(fwra.Context{Context: ctx}, episode.ProjectID(projectID), episodeID)
	if err != nil {
		if isEpisodeTraceNotFound(err) {
			return EpisodeTimeline{Record: csEpisodeRecordToView(rec), Events: episodeTimelineEvents(nil)}, nil
		}
		return EpisodeTimeline{}, mapRAError(err, "episodeAccess.ReadTraceEvents")
	}
	return EpisodeTimeline{
		Record: csEpisodeRecordToView(rec),
		Events: episodeTimelineEvents(raw),
	}, nil
}

// csEpisodeRecordViews maps a slice of ledger records onto the contract view type.
func csEpisodeRecordViews(records []episode.EpisodeRecord) []EpisodeRecordView {
	out := make([]EpisodeRecordView, 0, len(records))
	for _, r := range records {
		out = append(out, csEpisodeRecordToView(r))
	}
	return out
}

// csEpisodeRecordToView maps one episodeAccess ledger record onto this contract's
// OWN copy of the view shape (EpisodeRecordView mirrors episodeAccess.EpisodeRecord
// field-for-field; contracts are self-contained, so this is an intentional
// duplicate of the mapping episodeAccess itself owns, not a shared function).
func csEpisodeRecordToView(r episode.EpisodeRecord) EpisodeRecordView {
	v := EpisodeRecordView{
		EpisodeID:      r.EpisodeID,
		Kind:           csEpisodeViewKind(r.Kind),
		TargetRef:      r.TargetRef,
		WorkerClass:    r.WorkerClass,
		Model:          r.Model,
		Usage:          EpisodeUsage(r.Usage),
		CostUSD:        r.CostUSD,
		NumTurns:       r.NumTurns,
		ToolCallCounts: r.ToolCallCounts,
		StartedAt:      r.StartedAt,
		EndedAt:        r.EndedAt,
		Outcome:        episodeViewOutcome(r.Outcome),
		GapReason:      r.GapReason,
		TracePath:      r.TracePath,
	}
	if r.Lineage != nil {
		l := EpisodeLineage(*r.Lineage)
		v.Lineage = &l
	}
	if r.StreamedUsage != nil {
		u := EpisodeUsage(*r.StreamedUsage)
		v.StreamedUsage = &u
	}
	if len(r.SubagentSpans) > 0 {
		spans := make([]SubagentSpan, 0, len(r.SubagentSpans))
		for _, s := range r.SubagentSpans {
			spans = append(spans, SubagentSpan(s))
		}
		v.SubagentSpans = spans
	}
	return v
}

// csEpisodeViewKind maps the episodeAccess RA's Kind onto this contract's own copy
// of the enum. Written as a TOTAL switch rather than a numeric cast so a future
// divergence between the two independently-versioned contracts is a compile-time
// conversation, not silent drift.
func csEpisodeViewKind(k episode.EpisodeKind) EpisodeKind {
	switch k {
	case episode.EpisodeKindDesign:
		return EpisodeKindDesign
	case episode.EpisodeKindConstruction:
		return EpisodeKindConstruction
	case episode.EpisodeKindReview:
		return EpisodeKindReview
	case episode.EpisodeKindRework:
		return EpisodeKindRework
	case episode.EpisodeKindAnswer:
		return EpisodeKindAnswer
	default:
		// Unreachable for the five defined episode.EpisodeKind values above (the
		// exhaustive linter enforces that every real variant has its own case);
		// kept as a defensive fallback for an out-of-range ordinal.
		return EpisodeKindConstruction
	}
}

// ---------------------------------------------------------------------------
// QueryActivityView — revision derivation (spec 2026-09-20 §2). PURE: no I/O, no
// clock. normalizeAttempts turns today's scattered evidence into one attempt list;
// deriveTaskViews cuts that list into revisions and decides every task's state.
//
// WHY NORMALIZE. The running construct workflow does not write the attempt ledger: it
// counts attempts in memory to mint the AttemptID it stamps on the episode's TargetRef,
// and a gate attempt is written by nothing. Only cmd/backfill-attempts appends to
// ActivityConstructionStatus.Attempts. A live run's history is its episodes, its
// send-back OperatorNotes, its stored phase completions and its live session. Stage 3
// of the unified-activity spec persists revisions; this block is deleted then.
// ---------------------------------------------------------------------------

// Revision outcomes and task states: the wire values of the ActivityView contract.
const (
	revRunning       = "running"
	revAwaitingHuman = "awaitingHuman"
	revPassed        = "passed"
	revSentBack      = "sentBack"
	revFailed        = "failed"
	revSkipped       = "skipped"

	taskPending       = "pending"
	taskLocked        = "locked"
	taskRunning       = "running"
	taskAwaitingHuman = "awaitingHuman"
	taskPassed        = "passed"
	taskSentBack      = "sentBack"
	taskFailed        = "failed"

	normalizeGenerator = "constructionManager.normalizeAttempts"
)

// taskRevision is revision n of one lifecycle task: the n-th work that reached the gate
// (with every failed or retried attempt before it) for a dispatch task, the n-th gate
// attempt for a review task. The two share n.
type taskRevision struct {
	N          int
	Outcome    string
	StartedAt  *time.Time
	EndedAt    *time.Time
	AttemptIDs []string
	EpisodeID  string
	Note       string
	Comments   []projectstate.NoteComment
	Provenance projectstate.RecordOrigin
	// The rest are the PERSISTED round's own facts (stage 3, task 7). A revision the
	// reconstruction produced carries none of them except Round, which it takes from the
	// gate attempt's number — the reconstruction's own de-facto round.
	Round      int64
	Verdicts   []projectstate.ReviewVerdict
	Thread     []projectstate.ReviewComment
	Reviewers  []projectstate.RoundReviewer
	SubjectRef projectstate.SubjectRef
	DecidedBy  string
	DecidedAt  string
}

// taskView is one lifecycle task's derived state and history.
type taskView struct {
	ID        string
	State     string
	Revisions []taskRevision
}

// normalizeAttempts builds the one attempt list the derivation reads (rules N1–N4): the
// ledger verbatim, then a work attempt per episode the ledger does not hold, then the
// dispatch running now, then the gate attempts the send-back notes, the RESOLVED phase
// completions and the live gate imply. Everything it adds is stamped backfilled —
// "reconstructed from real evidence recorded elsewhere" — with the evidence as its basis.
//
// resolved is projectstate.ResolveConstructionRow's third return, never row.Phases: the
// phase set a row HAS and the completion state it is IN are one fact with one rule
// (ResolvePhaseCompletions), and every reader of this row derives from that one answer.
func normalizeAttempts(activityID string, row projectstate.ActivityExecution, resolved []projectstate.PhaseCompletion, episodes []episode.EpisodeRecord, live *ConstructionSessionView) []projectstate.TaskAttempt {
	out := slices.Clone(row.Attempts)
	index := make(map[string]int, len(out))
	for i, a := range out {
		index[a.AttemptID] = i
	}
	for _, ep := range episodes {
		task, n, ok := parseAttemptRef(activityID, ep.TargetRef)
		if !ok {
			continue
		}
		evidence := projectstate.EvidenceRef{Kind: projectstate.EvidenceEpisode, Ref: ep.EpisodeID}
		if i, held := index[ep.TargetRef]; held {
			if out[i].Evidence.Kind == projectstate.EvidenceNone {
				out[i].Evidence = evidence // N1
			}
			continue
		}
		started, ended := ep.StartedAt, ep.EndedAt
		index[ep.TargetRef] = len(out)
		out = append(out, projectstate.TaskAttempt{ // N2
			AttemptID: ep.TargetRef, Task: task, Phase: projectstate.PhaseForTask(task), Attempt: n,
			Actor: projectstate.ActorAgent, StartedAt: &started, EndedAt: &ended,
			Outcome: taskOutcomeOfEpisode(ep.Outcome), Evidence: evidence,
			Provenance: reconstructed("episodes[" + ep.EpisodeID + "]"),
		})
	}
	out = appendRunningAttempt(out, activityID, resolved, live)
	return appendGateAttempts(out, activityID, row, resolved, live)
}

// parseAttemptRef reads "<activityId>:<task>:<n>" (projectstate.AttemptID). A legacy
// TargetRef (the bare activity id) and another activity's ref are not attempts of this one.
func parseAttemptRef(activityID, ref string) (projectstate.MethodTask, int, bool) {
	rest, ok := strings.CutPrefix(ref, activityID+":")
	if !ok {
		return "", 0, false
	}
	name, num, ok := strings.Cut(rest, ":")
	if !ok {
		return "", 0, false
	}
	n, err := strconv.Atoi(num)
	task := projectstate.MethodTask(name)
	if err != nil || n < 1 || projectstate.PhaseForTask(task) == "" {
		return "", 0, false
	}
	return task, n, true
}

// taskOutcomeOfEpisode: an episode that did not succeed — failed, cancelled, or a gap
// with no summary at all — is a failed attempt; the dispatch burned and produced nothing.
func taskOutcomeOfEpisode(o episode.EpisodeOutcome) projectstate.TaskOutcome {
	switch o {
	case episode.EpisodeSucceeded:
		return projectstate.OutcomePassed
	case episode.EpisodeFailed, episode.EpisodeCancelled, episode.EpisodeGap:
		return projectstate.OutcomeFailed
	}
	return projectstate.OutcomeFailed
}

func reconstructed(basis string) projectstate.AttemptProvenance {
	return projectstate.AttemptProvenance{Origin: projectstate.OriginBackfilled, Generator: normalizeGenerator, Basis: basis}
}

func highestAttempt(attempts []projectstate.TaskAttempt, task projectstate.MethodTask) int {
	n := 0
	for _, a := range attempts {
		if a.Task == task && a.Attempt > n {
			n = a.Attempt
		}
	}
	return n
}

// ledgerRejections counts the gate task's rejections ALREADY in out — the ones a real
// run recorded. N4 reconstructs only the send-backs beyond them.
func ledgerRejections(out []projectstate.TaskAttempt, gate projectstate.MethodTask) int {
	n := 0
	for _, a := range out {
		if a.Task == gate && a.Outcome == projectstate.OutcomeRejected {
			n++
		}
	}
	return n
}

// roundsForTask is the row's PERSISTED rounds for one review task, in the APPEND-ONLY
// ledger's own order — which is the order they were opened in, and therefore the order
// they are read as revisions.
//
// IT DOES NOT SORT BY ROUND NUMBER, and the design rails are why. They mint a FOUR-part
// round id (activity:gate:artifactKind:n) because three artifact kinds share the
// architecture gate, and each kind counts ITS OWN rounds — so one row holds two rounds
// numbered 1 at the same gate, and ordering by number would interleave two unrelated
// review histories into one invented sequence. Ledger order is the only total order the
// two kinds share, and it is a real one. Nothing here parses a round id into segments
// either (equality is all any code in this repo asks of one): the join to the attempt
// ledger is by FIELDS, and a revision's number is its position in this order, never the
// round number — exactly as it already is for a gate attempt.
func roundsForTask(rounds []projectstate.ReviewRound, task projectstate.MethodTask) []projectstate.ReviewRound {
	out := make([]projectstate.ReviewRound, 0, len(rounds))
	for _, r := range rounds {
		if r.TaskID == task {
			out = append(out, r)
		}
	}
	return out
}

// sendBackNotesFor is the phase's send-back notes in recorded order (append-only slice
// order IS RecordedAt order).
func sendBackNotesFor(notes []projectstate.OperatorNote, p projectstate.ActivityMethodPhase) []projectstate.OperatorNote {
	out := make([]projectstate.OperatorNote, 0, len(notes))
	for _, note := range notes {
		if note.Kind == projectstate.NoteSendBack && note.Gate == string(p) {
			out = append(out, note)
		}
	}
	return out
}

// lowestAttempt is the smallest attempt number the list holds for the task, and whether
// it holds any at all. A gate whose ledger starts at #2 has room at #1 beneath it.
func lowestAttempt(attempts []projectstate.TaskAttempt, task projectstate.MethodTask) (int, bool) {
	low, held := 0, false
	for _, a := range attempts {
		if a.Task == task && (!held || a.Attempt < low) {
			low, held = a.Attempt, true
		}
	}
	return low, held
}

// appendPreLedgerRejections is N4's reconstruction half: the phase's send-back notes the
// ledger holds no rejection for. They are the OLDEST notes (tails aligned like R4 —
// notes exist only since B1.1, so it is the oldest rejections that have no note and the
// oldest notes that have no recorded rejection), and they are OLDER than every attempt
// the gate has recorded. So they must SORT BEFORE the ledger's own: phaseRevisions orders
// a gate's revisions by .Attempt and reviewEvidenceState reads the LAST one, so numbering
// a reconstruction after the ledger's highest turns a passed, merged gate into sentBack.
// Renumbering a recorded attempt is forbidden (N1 keeps the ledger verbatim), so the
// block is placed immediately BELOW the lowest recorded number instead:
// lowest-unrecorded .. lowest-1, which runs to 0 and below on a gate whose ledger already
// starts at #1. That block is contiguous and strictly below anything recorded, so the
// AttemptIDs stay unique and the placement deterministic, and "≤ 0" reads as exactly what
// it is — an attempt from before this gate kept a ledger. A gate with NO recorded attempt
// has no ledger to sit under and numbers from 1, exactly as it always did.
func appendPreLedgerRejections(out []projectstate.TaskAttempt, activityID string, gate projectstate.MethodTask, p projectstate.ActivityMethodPhase, notes []projectstate.OperatorNote) []projectstate.TaskAttempt {
	unrecorded := len(notes) - ledgerRejections(out, gate)
	if unrecorded <= 0 {
		return out
	}
	n := 1
	if lowest, held := lowestAttempt(out, gate); held {
		n = lowest - unrecorded
	}
	for _, note := range notes[:unrecorded] {
		at := note.RecordedAt
		out = append(out, projectstate.TaskAttempt{
			AttemptID: projectstate.AttemptID(activityID, gate, n), Task: gate, Phase: p, Attempt: n,
			EndedAt: &at, Outcome: projectstate.OutcomeRejected,
			Provenance: reconstructed("operatorNotes[" + note.NoteID + "]"),
		})
		n++
	}
	return out
}

// appendRunningAttempt is N3: the dispatch a live session is running now, which has no
// episode until it ends.
//
// The phase it is running is DERIVED from the resolved set — the first lifecycle phase
// the ledger does not hold complete — rather than read off a stored CurrentPhase. The
// stored field is gone (spec §5.3: it was a second answer to a question the ledger
// already answers), and it was the less trustworthy of the two anyway: it was stamped at
// phase entry and never cleared, so a row that had moved on still named the phase it was
// stamped in. A row whose every phase is complete is running nothing, and returns none.
func appendRunningAttempt(out []projectstate.TaskAttempt, activityID string, resolved []projectstate.PhaseCompletion, live *ConstructionSessionView) []projectstate.TaskAttempt {
	if live == nil || (live.Stage != StageDispatching && live.Stage != StagePipelineRunning) {
		return out
	}
	current := projectstate.CurrentLifecyclePhase(resolved)
	task := projectstate.AgentTaskFor(current)
	if task == "" {
		return out
	}
	for _, a := range out {
		if a.Task == task && a.Outcome == projectstate.OutcomePending {
			return out
		}
	}
	n := highestAttempt(out, task) + 1
	return append(out, projectstate.TaskAttempt{
		AttemptID: projectstate.AttemptID(activityID, task, n), Task: task, Phase: current, Attempt: n,
		Actor: projectstate.ActorAgent, Provenance: reconstructed("session.stage"),
	})
}

// liveApprovalGate is the lifecycle phase a live session awaits approval at, if any. The
// merge hold and an escalation are not phase gates and never match a phase id.
func liveApprovalGate(live *ConstructionSessionView) (string, *time.Time) {
	if live == nil || live.Stage != StageAwaitingApproval || live.AwaitingGate == nil {
		return "", nil
	}
	return *live.AwaitingGate, live.AwaitingSince
}

// appendGateAttempts is N4, over the resolved phase set.
func appendGateAttempts(out []projectstate.TaskAttempt, activityID string, row projectstate.ActivityExecution, resolved []projectstate.PhaseCompletion, live *ConstructionSessionView) []projectstate.TaskAttempt {
	liveGate, liveSince := liveApprovalGate(live)
	// ResolveConstructionRow's reconciled set IS the phase inventory and the completion
	// state, in profile order. There is no second inventory: canonicalMethodPhases was one,
	// and two inventories over one row is exactly what ResolvePhaseCompletions exists to
	// remove ("when the two disagree, the profile wins"). Reading row.Phases here instead
	// reconstructed a passed gate for a phase the row's read-time lifecycle does not have.
	for _, pc := range resolved {
		p := pc.Phase
		gate := projectstate.GateTaskFor(p)
		// ROUNDS BEAT NOTES BEAT NOTHING, per GATE. A gate the row holds a round for is a
		// gate a real run wrote: its revisions are read from that ledger (phaseRevisions),
		// so every reconstruction here would be a second, competing record of the same
		// review — the note-shaped double count Task 1 removed, and its phase-completion
		// and live-gate shaped twins. The rule is per gate, not per row: a row written
		// across the ledger's arrival has rounds at one gate and only notes at an older one.
		if len(roundsForTask(row.Reviews, gate)) > 0 {
			continue
		}
		passed := false
		for _, a := range out {
			passed = passed || (a.Task == gate && a.Outcome == projectstate.OutcomePassed)
		}
		// A send-back note and a RECORDED rejection of the same gate are one event, not
		// two. The workflow records both (the note is how the feedback reaches the next
		// dispatch — PendingOperatorNotes), so reconstructing one attempt per note on top
		// of the ledger would double every revision. Only the notes the ledger has no
		// rejection for are reconstructed, and they go BELOW it — see the function.
		out = appendPreLedgerRejections(out, activityID, gate, p, sendBackNotesFor(row.OperatorNotes, p))
		// The newest attempt continues the ledger's numbering, AFTER the reconstruction
		// (which either sits below the ledger or, on a gate with no ledger at all, IS the
		// numbering so far).
		n := highestAttempt(out, gate)
		add := func(outcome projectstate.TaskOutcome, started, ended *time.Time, basis string) {
			n++
			out = append(out, projectstate.TaskAttempt{
				AttemptID: projectstate.AttemptID(activityID, gate, n), Task: gate, Phase: p, Attempt: n,
				StartedAt: started, EndedAt: ended, Outcome: outcome, Provenance: reconstructed(basis),
			})
		}
		switch {
		case pc.Completed && !passed:
			add(projectstate.OutcomePassed, nil, pc.CompletedAt, "phases["+string(p)+"].completed")
		case liveGate == string(p):
			add(projectstate.OutcomePending, liveSince, nil, "session.awaitingGate")
		}
	}
	return out
}

// phaseSegment is one revision's work: the phase's work-task attempts up to the one
// that reached the gate, plus any conditional-task attempts (someConstruction,
// testClient) made alongside them.
type phaseSegment struct {
	main, extra []projectstate.TaskAttempt
}

func (s phaseSegment) members() []projectstate.TaskAttempt {
	return append(slices.Clone(s.main), s.extra...)
}

// deriveTaskViews returns one view per lifecycle task, in lifecycle order (rules R1–R6
// and the task state table; both are spelled out in the stage-0 plan, Task 7).
//
// rounds is the row's PERSISTED review ledger. Where a gate has rounds they ARE its
// revisions; the note-and-attempt reconstruction survives only for the gates that have
// none, which is every gate of every row written before this ledger existed.
func deriveTaskViews(lc methodassets.Lifecycle, attempts []projectstate.TaskAttempt, notes []projectstate.OperatorNote, rounds []projectstate.ReviewRound, liveGate string) []taskView {
	revs := make(map[string][]taskRevision, len(lc.Tasks))
	gates := make(map[string]bool, len(lc.Phases))
	for _, ph := range lc.Phases {
		work := phaseWorkTask(lc, ph.ID)
		workRevs, gateRevs := phaseRevisions(ph, work, attempts, notes, rounds, liveGate)
		if work != "" {
			revs[work] = workRevs
		}
		revs[ph.Gate], gates[ph.Gate] = gateRevs, true
	}
	byID := make(map[string]methodassets.LifecycleTask, len(lc.Tasks))
	reviewerOf := make(map[string]string, len(lc.Tasks))
	for _, t := range lc.Tasks {
		byID[t.ID] = t
		if t.Kind == methodassets.LifecycleTaskReview && t.Reviews != "" {
			reviewerOf[t.Reviews] = t.ID
		}
	}
	states := make(map[string]string, len(lc.Tasks))
	var stateOf func(id string) string
	stateOf = func(id string) string {
		if s, ok := states[id]; ok {
			return s
		}
		states[id] = taskLocked // a cycle reads locked; a validated lifecycle has none
		t := byID[id]
		s := evidenceState(t, gates[id], revs, reviewerOf, liveGate)
		if s == "" {
			s = taskPending
			for _, dep := range t.DependsOn {
				if stateOf(dep) != taskPassed {
					s = taskLocked // rule 9
					break
				}
			}
		}
		states[id] = s
		return s
	}
	out := make([]taskView, 0, len(lc.Tasks))
	for _, t := range lc.Tasks {
		out = append(out, taskView{ID: t.ID, State: stateOf(t.ID), Revisions: revs[t.ID]})
	}
	return out
}

// phaseWorkTask is the phase's dispatch task ("" for a phase with none, e.g. the
// projectDesign activity's review-only phase).
func phaseWorkTask(lc methodassets.Lifecycle, phaseID string) string {
	for _, t := range lc.Tasks {
		if t.Phase == phaseID && t.Kind == methodassets.LifecycleTaskDispatch {
			return t.ID
		}
	}
	return ""
}

// phaseRevisions cuts one lifecycle phase's attempts into the work task's revisions and
// the gate task's revisions (R1–R4), and chooses which of the two review ledgers the gate
// reads: the PERSISTED rounds where the row holds any for this gate, the reconstruction
// where it holds none.
func phaseRevisions(ph methodassets.LifecyclePhase, work string, attempts []projectstate.TaskAttempt, notes []projectstate.OperatorNote, rounds []projectstate.ReviewRound, liveGate string) (workRevs, gateRevs []taskRevision) {
	var main, extra, gate []projectstate.TaskAttempt
	for _, a := range attempts {
		switch {
		case string(a.Phase) != ph.ID:
		case string(a.Task) == ph.Gate:
			gate = append(gate, a)
		case string(a.Task) == work:
			main = append(main, a)
		default:
			extra = append(extra, a) // R2: a conditional task is a sub-attempt of the work task
		}
	}
	// .Attempt is the total order of a task's attempts, and for a gate it INCLUDES the
	// pre-ledger rejections N4 reconstructs from send-back notes, which carry numbers
	// below the ledger's lowest — 0 and down — precisely so this sort puts them first
	// (appendPreLedgerRejections says why). The revision number below is the position
	// in this order, never the attempt number, so a "≤ 0" attempt is still revision 1.
	byAttempt := func(x, y projectstate.TaskAttempt) int { return cmp.Compare(x.Attempt, y.Attempt) }
	slices.SortStableFunc(main, byAttempt)
	slices.SortStableFunc(gate, byAttempt)
	for i, seg := range foldConditional(cutSegments(main), extra) {
		workRevs = append(workRevs, dispatchRevision(i+1, seg))
	}
	live := liveGate == ph.ID
	persisted := roundsForTask(rounds, projectstate.MethodTask(ph.Gate))
	if len(persisted) == 0 {
		return workRevs, reconstructedReviewRevisions(gate, notes, ph.ID, live)
	}
	// THE BOUNDARY IS INSIDE ONE GATE, not between gates. A row mid-flight when the round
	// ledger arrived has gate attempts the ledger recorded BEFORE any round existed, and
	// the first round it opens is numbered off that ledger (seedResumeFromLedger seeds the
	// counter from both), so it starts at 2 or higher. "Any round wins outright" would
	// drop attempt #1 — a real, recorded send-back — out of the history entirely.
	//
	// So the split is by NUMBER: every gate attempt below the lowest round number is a
	// review from before the rounds and reconstructs as it always did (with its note, and
	// with its own provenance); the rounds take it from there. A gate whose ledger starts
	// at or above the lowest round has no such attempts and this costs it nothing, which
	// is every gate on the design rails — they record no attempts at all.
	pre, joined := splitAtLowestRound(gate, persisted)
	// live is FALSE for the pre-round block on purpose: a session waiting at this gate is
	// waiting at the OPEN ROUND, never at an attempt recorded before the rounds began.
	gateRevs = reconstructedReviewRevisions(pre, notes, ph.ID, false)
	// ONE offset, fixed before the append: the rounds continue the numbering after the
	// whole pre-round block, they do not each start after the one before them.
	before := len(gateRevs)
	for _, rev := range roundRevisions(persisted, joined, live) {
		rev.N += before
		gateRevs = append(gateRevs, rev)
	}
	return workRevs, gateRevs
}

// splitAtLowestRound cuts a gate's recorded attempts at the lowest round number the row
// holds for it: pre is the attempts from before the rounds began, joined is the rest —
// the ones a round can settle (roundRevisions joins them by number).
func splitAtLowestRound(gate []projectstate.TaskAttempt, rounds []projectstate.ReviewRound) (pre, joined []projectstate.TaskAttempt) {
	lowest := rounds[0].Round
	for _, r := range rounds[1:] {
		if r.Round < lowest {
			lowest = r.Round
		}
	}
	for _, a := range gate {
		if int64(a.Attempt) < lowest {
			pre = append(pre, a)
		} else {
			joined = append(joined, a)
		}
	}
	return pre, joined
}

// roundRevisions turns the PERSISTED rounds for one review task into that task's
// revisions. The round is the record: its verdicts, its thread, its roster, its subject,
// its number and its decision are carried verbatim, and no ordering heuristic gets a vote.
//
// THE JOIN TO THE ATTEMPT LEDGER IS BY FIELDS. The construction rail mints a round's id
// with projectstate.AttemptID, so its <n> IS the gate attempt's number — but the design
// rails mint a four-part id for the same gate, and splitting either into segments is a
// parse nothing else in this repo does (equality is all any code asks of a round id). So
// the attempt this round settled is found as "the gate attempt whose number is the round's
// number", which is true on both rails and false for neither.
//
// EARMARK, stage 4. That join is unique only while the design rails record NO attempts.
// Two artifact kinds share the architecture gate and each counts its own rounds, so once
// the design rail writes its attempt ledger, two rounds numbered 1 would both bind the one
// attempt numbered 1. The fix belongs where the ambiguity is born — a round that names the
// attempt it judged, as ReviewVerdict.AttemptID already does — not in a wider join here.
//
// A ROUND WITH NO GATE ATTEMPT IS NORMAL, not a gap. The construction rail opens the round
// when the gate is reached and writes the gate attempt only when the round is DECIDED, so
// every gate a human is looking at right now is a pending round with no attempt; a run that
// died in between (constructactivity.go's stated crash window) leaves one pending forever;
// and the design rails record no attempt ledger at all yet. In all three the round alone
// is the revision, and it cites no attempt because none exists.
func roundRevisions(rounds []projectstate.ReviewRound, gate []projectstate.TaskAttempt, live bool) []taskRevision {
	out := make([]taskRevision, 0, len(rounds))
	for i, r := range rounds {
		rev := taskRevision{
			N: i + 1, Outcome: roundOutcome(r.Outcome, live), Round: r.Round,
			Verdicts: r.Verdicts, Thread: r.Thread, Reviewers: r.Reviewers, SubjectRef: r.SubjectRef,
			DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt, Provenance: r.Provenance.Origin,
			Note: sendBackNote(r), Comments: threadAnchors(r.Thread),
			StartedAt: rfc3339OrNil(r.OpenedAt), EndedAt: rfc3339OrNil(r.DecidedAt),
		}
		for _, a := range gate {
			if int64(a.Attempt) == r.Round {
				rev.AttemptIDs = []string{a.AttemptID}
				break
			}
		}
		out = append(out, rev)
	}
	return out
}

// roundOutcome renders a stored round outcome as a revision outcome. Total over the
// vocabulary with no default arm.
//
// EARMARK. RoundWithdrawn — a round pulled back before anyone decided it — has no wire
// name of its own on TaskRevisionOutcome and reads as failed: the revision did not clear
// its gate, which is the part a reader must not be lied to about. "Failed" overstates the
// drama (a withdrawal is deliberate, not a fault); giving it its own wire value belongs
// with the Activity Experience screen that will render it (stage 5).
//
// Its neighbour, same earmark: a round STRANDED pending by a run that died renders
// `running` for as long as it is the gate's last round — and with no session there is no
// live gate, so it never even reads awaitingHuman. Both rails state that crash window and
// both leave it to the stage-4 sweep, which is the only thing that can know the run is
// gone; RoundWithdrawn is the terminal it will stamp.
func roundOutcome(o projectstate.ReviewRoundOutcome, live bool) string {
	switch o {
	case projectstate.RoundPassed:
		return revPassed
	case projectstate.RoundSentBack:
		return revSentBack
	case projectstate.RoundWithdrawn:
		return revFailed
	case projectstate.RoundPending:
		if live {
			return revAwaitingHuman
		}
		return revRunning
	}
	return revFailed // an outcome outside the vocabulary is not a pass
}

// threadAnchors is the round's thread as the revision's flat anchored comments — the same
// two fields a reconstructed revision offers, so a reader that has only ever known
// `comments` keeps working on a round-backed revision. It is a PROJECTION of `thread`, not
// a second record: the replies, the open/answered/resolved status and the reopen flag live
// there and only there, and a reader that needs them reads them there.
func threadAnchors(thread []projectstate.ReviewComment) []projectstate.NoteComment {
	if len(thread) == 0 {
		return nil
	}
	out := make([]projectstate.NoteComment, 0, len(thread))
	for _, c := range thread {
		out = append(out, projectstate.NoteComment{JSONPath: c.Anchor, Text: c.Text})
	}
	return out
}

// sendBackNote is the round's send-back note: the LAST send-back verdict's summary, which
// is the prose the operator typed into the decision that closed the round. It replaces the
// OperatorNote the reconstruction had to go looking for — same words, read off the record
// that owns them instead of matched to it by position.
//
// ONLY on a round DECIDED sentBack, which is what `note` means on the wire ("omitted unless
// outcome is sentBack"). A round that passed can still hold a send-back verdict — one
// reviewer dissented and the gate went through anyway — and rendering that dissent as the
// revision's send-back note would say the work was returned when it was not. The dissent
// is not lost: it is a row in `verdicts`, which is the whole point of carrying them.
func sendBackNote(r projectstate.ReviewRound) string {
	if r.Outcome != projectstate.RoundSentBack {
		return ""
	}
	note := ""
	for _, v := range r.Verdicts {
		if v.Verdict == projectstate.VerdictSendBack && v.Summary != "" {
			note = v.Summary
		}
	}
	return note
}

// rfc3339OrNil parses a store-stamped timestamp. The round ledger holds its times as
// RFC3339 STRINGS (the store stamps them; the caller never does), and a string that does
// not parse — or an empty one, which is what an undecided round's DecidedAt is — becomes
// no time at all rather than the zero instant.
func rfc3339OrNil(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

// reconstructedReviewRevisions is stage 0's R3/R4 path, kept for the gates of rows that
// predate the round ledger: one revision per gate attempt, with the phase's send-back
// notes matched onto the rejections TAILS ALIGNED (notes exist only since B1.1, so it is
// the oldest rejections that have no note). A gate with ANY persisted round never reaches
// it.
func reconstructedReviewRevisions(gate []projectstate.TaskAttempt, notes []projectstate.OperatorNote, phaseID string, live bool) []taskRevision {
	var sendBacks []projectstate.OperatorNote
	for _, n := range notes {
		if n.Kind == projectstate.NoteSendBack && n.Gate == phaseID {
			sendBacks = append(sendBacks, n)
		}
	}
	rejected := 0
	for _, g := range gate {
		if g.Outcome == projectstate.OutcomeRejected {
			rejected++
		}
	}
	// R4: the j-th rejection takes note j + (len(sendBacks) - rejected) — tails aligned.
	next := len(sendBacks) - rejected
	out := make([]taskRevision, 0, len(gate))
	for i, g := range gate {
		rev := reviewRevision(i+1, g, live)
		if g.Outcome == projectstate.OutcomeRejected {
			if next >= 0 && next < len(sendBacks) {
				rev.Note, rev.Comments = sendBacks[next].Text, sendBacks[next].Comments
			}
			next++
		}
		out = append(out, rev)
	}
	return out
}

// cutSegments is R1: a segment closes at the attempt that reached the gate.
func cutSegments(main []projectstate.TaskAttempt) []phaseSegment {
	var out []phaseSegment
	var cur []projectstate.TaskAttempt
	for _, a := range main {
		cur = append(cur, a)
		if a.Outcome == projectstate.OutcomePassed || a.Outcome == projectstate.OutcomeSkipped {
			out, cur = append(out, phaseSegment{main: cur}), nil
		}
	}
	if len(cur) > 0 {
		out = append(out, phaseSegment{main: cur})
	}
	return out
}

// foldConditional is R2: each conditional-task attempt joins the first revision whose
// reaching attempt ended at or after it started, else the last revision.
func foldConditional(segs []phaseSegment, extra []projectstate.TaskAttempt) []phaseSegment {
	for _, x := range extra {
		if len(segs) == 0 {
			segs = append(segs, phaseSegment{})
		}
		at := len(segs) - 1
		for i, s := range segs {
			if len(s.main) == 0 {
				continue
			}
			reached := s.main[len(s.main)-1]
			if x.StartedAt != nil && reached.EndedAt != nil && !x.StartedAt.After(*reached.EndedAt) {
				at = i
				break
			}
		}
		segs[at].extra = append(segs[at].extra, x)
	}
	return segs
}

func dispatchRevision(n int, seg phaseSegment) taskRevision {
	members := seg.members()
	rev := taskRevision{N: n, Outcome: revFailed, Provenance: projectstate.AttemptsWorstOrigin(members)} // R5
	decisive := members[len(members)-1]
	if len(seg.main) > 0 {
		decisive = seg.main[len(seg.main)-1]
	}
	switch decisive.Outcome {
	case projectstate.OutcomePassed:
		rev.Outcome = revPassed
	case projectstate.OutcomeSkipped:
		rev.Outcome = revSkipped
	case projectstate.OutcomePending, projectstate.OutcomeRejected, projectstate.OutcomeFailed:
		// pending is decided below over every member; a work attempt is never "rejected".
	}
	for _, a := range members {
		rev.AttemptIDs = append(rev.AttemptIDs, a.AttemptID)
		if a.Outcome == projectstate.OutcomePending {
			rev.Outcome = revRunning
		}
	}
	for _, a := range slices.Backward(members) { // R6: main members first, so search them last-to-first
		if a.Evidence.Kind == projectstate.EvidenceEpisode && a.Task == decisive.Task {
			rev.EpisodeID = a.Evidence.Ref
			break
		}
	}
	rev.StartedAt, rev.EndedAt = attemptSpan(members)
	return rev
}

// reviewRevision renders one gate attempt as a revision. n is the ORDINAL of g over the
// gate attempts sorted by Attempt — 1 for the first, 2 for the second — not the attempt's
// own number: the two coincide only while the ledger has no gaps, so a ledger missing
// designReview#2 numbers its third attempt revision 2, and the attempt number stays
// visible inside attemptIds. live marks the occurrence the session is waiting at now.
func reviewRevision(n int, g projectstate.TaskAttempt, live bool) taskRevision {
	// Round: the attempt's own number is the de-facto round of a row that kept no round
	// ledger, and it is what the construction rail's round number IS. A number ≤ 0 is NOT
	// one: appendPreLedgerRejections places a reconstruction from before the ledger
	// beneath the ledger's lowest, which runs to 0 and below, and that number is a sort
	// position rather than a count of reviews. Such a revision carries no round at all,
	// which is the truth — nothing counted this gate's rounds when it happened.
	rev := taskRevision{N: n, AttemptIDs: []string{g.AttemptID},
		Provenance: projectstate.AttemptsWorstOrigin([]projectstate.TaskAttempt{g})}
	if g.Attempt > 0 {
		rev.Round = int64(g.Attempt)
	}
	switch g.Outcome {
	case projectstate.OutcomePending:
		rev.Outcome = revRunning
		if live {
			rev.Outcome = revAwaitingHuman
		}
	case projectstate.OutcomePassed:
		rev.Outcome = revPassed
	case projectstate.OutcomeRejected:
		rev.Outcome = revSentBack
	case projectstate.OutcomeFailed:
		rev.Outcome = revFailed
	case projectstate.OutcomeSkipped:
		rev.Outcome = revSkipped
	}
	rev.StartedAt, rev.EndedAt = attemptSpan([]projectstate.TaskAttempt{g})
	return rev
}

// attemptSpan is R6's times: the earliest start, and the latest end once nothing is pending.
func attemptSpan(members []projectstate.TaskAttempt) (started, ended *time.Time) {
	open := false
	for _, a := range members {
		if a.StartedAt != nil && (started == nil || a.StartedAt.Before(*started)) {
			started = a.StartedAt
		}
		if a.EndedAt != nil && (ended == nil || a.EndedAt.After(*ended)) {
			ended = a.EndedAt
		}
		open = open || a.Outcome == projectstate.OutcomePending
	}
	if open {
		ended = nil
	}
	return started, ended
}

// evidenceState applies state rules 1–8; "" means the task has no evidence and its
// dependsOn decide between locked and pending.
func evidenceState(t methodassets.LifecycleTask, isGate bool, revs map[string][]taskRevision, reviewerOf map[string]string, liveGate string) string {
	if t.Kind == methodassets.LifecycleTaskReview {
		if isGate && liveGate != "" && liveGate == t.Phase {
			return taskAwaitingHuman // 1
		}
		return reviewEvidenceState(revs[t.ID], len(revs[t.Reviews]))
	}
	return dispatchEvidenceState(revs[t.ID], revs[reviewerOf[t.ID]])
}

func reviewEvidenceState(mine []taskRevision, workRevisions int) string {
	if len(mine) == 0 {
		return ""
	}
	switch last := mine[len(mine)-1]; last.Outcome {
	case revRunning, revAwaitingHuman:
		return taskRunning // 2
	case revSentBack:
		if workRevisions > last.N {
			return taskPending // 3: the redraft is under way
		}
		return taskSentBack // 4
	case revFailed:
		return taskFailed // 6
	}
	return taskPassed // 7
}

func dispatchEvidenceState(mine, reviewer []taskRevision) string {
	var verdict *taskRevision
	if len(reviewer) > 0 {
		verdict = &reviewer[len(reviewer)-1]
	}
	if len(mine) == 0 {
		if verdict != nil && verdict.Outcome == revPassed {
			return taskPassed // 8: the gate is the exit criterion; silence is not denial
		}
		return ""
	}
	last := mine[len(mine)-1]
	switch {
	case last.Outcome == revRunning:
		return taskRunning // 2
	case verdict != nil && verdict.Outcome == revSentBack && last.N <= verdict.N:
		return taskSentBack // 5
	case last.Outcome == revFailed:
		return taskFailed // 6
	}
	return taskPassed // 7
}

// committedActivityItem finds an activity in the committed Phase-2 activity list.
func committedActivityItem(proj projectstate.Project, id string) (projectstate.ActivityItem, bool) {
	_, list, ok := committedPlanInputs(proj)
	if !ok {
		return projectstate.ActivityItem{}, false
	}
	for _, it := range list.Activities {
		if it.Name == id {
			return it, true
		}
	}
	return projectstate.ActivityItem{}, false
}

// liveSessionFor asks the activity's session only while its row is Running: a
// not-started, done or failed activity has no gate to show, and must read with Temporal
// down. No session (never dispatched, or past retention) is not an error.
func (m *constructionManager) liveSessionFor(ctx context.Context, projectID ProjectID, activityID ActivityID, coarse projectstate.ActivityConstructionPhase) (*ConstructionSessionView, error) {
	if coarse != projectstate.ActivityConstructionRunning {
		return nil, nil //nolint:nilnil // "no live session" is a value here, not a failure
	}
	v, err := m.activitySession(ctx, projectID, activityID)
	if err != nil {
		var fe *fwmanager.Error
		if errors.As(err, &fe) && fe.Kind == fwmanager.NotFound {
			return nil, nil //nolint:nilnil // see above
		}
		return nil, err
	}
	return &v, nil
}

// activityViewState folds the row's effective coarse state and the live session into
// the view's five states. KNOWN STAGE-0 GAP: the coarse state comes from the row, the
// task states from the evidence, and a Running row that stored no CurrentPhase has no
// running task to show — the activity reads running while every task reads pending or
// locked. That is honest about what today's records hold; stage 3 stores the revisions
// and the two stop being separate derivations.
func activityViewState(coarse projectstate.ActivityConstructionPhase, live *ConstructionSessionView) ActivityViewState {
	switch coarse {
	case projectstate.ActivityConstructionNotStarted:
		return ActivityViewNotStarted
	case projectstate.ActivityConstructionDone:
		return ActivityViewDone
	case projectstate.ActivityConstructionFailed:
		return ActivityViewFailed
	case projectstate.ActivityConstructionRunning:
		if live != nil && (live.Stage == StageAwaitingApproval || live.Stage == StageAwaitingTakeover) {
			return ActivityViewAwaitingHuman
		}
	}
	return ActivityViewRunning
}

// activityViewFrom assembles the contract view. Every array is non-nil: the wire carries
// [] for "none", never null.
func activityViewFrom(activityID ActivityID, item projectstate.ActivityItem, typ projectstate.ActivityType, variant projectstate.TestingVariant, lc methodassets.Lifecycle, resolved []projectstate.PhaseCompletion, tasks []taskView) ActivityView {
	states := make(map[string]string, len(tasks))
	revisions := make(map[string][]taskRevision, len(tasks))
	for _, t := range tasks {
		states[t.ID], revisions[t.ID] = t.State, t.Revisions
	}
	view := ActivityView{ActivityID: activityID, Name: item.Title, Type: typ.String(),
		Phases: make([]ActivityLifecyclePhase, 0, len(lc.Phases)), Tasks: make([]ActivityTaskView, 0, len(lc.Tasks))}
	if view.Name == "" {
		view.Name = item.Name
	}
	if typ == projectstate.ActivityTypeTesting {
		view.Variant = strPtrOrNil(variant.String())
	}
	view.ComponentID = strPtrOrNil(item.ComponentID)
	done := make(map[projectstate.ActivityMethodPhase]bool, len(resolved))
	for _, pc := range resolved {
		done[pc.Phase] = pc.Completed
	}
	for _, ph := range lc.Phases {
		// ONE rule: ResolvePhaseCompletions. The gate task's derived STATE is a view of
		// the same evidence, but it is derived through normalizeAttempts' reconstruction
		// and can disagree with the resolver over a partial row — and a screen that
		// disagrees with the pump about whether a phase is done is the defect this
		// collapses.
		view.Phases = append(view.Phases, ActivityLifecyclePhase{
			ID: ph.ID, Label: ph.Label, Weight: int64(ph.Weight), GateTaskID: ph.Gate,
			Completed: done[projectstate.ActivityMethodPhase(ph.ID)],
		})
	}
	for _, t := range lc.Tasks {
		view.Tasks = append(view.Tasks, ActivityTaskView{
			ID: t.ID, Kind: ActivityTaskKind(t.Kind), Title: t.Title, LifecyclePhaseID: t.Phase,
			DependsOn: append([]string{}, t.DependsOn...), Reviews: strPtrOrNil(t.Reviews),
			State: ActivityTaskState(states[t.ID]), Revisions: revisionViews(revisions[t.ID]),
		})
	}
	return view
}

func revisionViews(revs []taskRevision) []TaskRevisionView {
	out := make([]TaskRevisionView, 0, len(revs))
	for _, r := range revs {
		comments := make([]TaskRevisionComment, 0, len(r.Comments))
		for _, c := range r.Comments {
			comments = append(comments, TaskRevisionComment{JSONPath: c.JSONPath, Text: c.Text})
		}
		out = append(out, TaskRevisionView{
			N: int64(r.N), Outcome: TaskRevisionOutcome(r.Outcome), StartedAt: r.StartedAt, EndedAt: r.EndedAt,
			AttemptIDs: append([]string{}, r.AttemptIDs...), EpisodeID: strPtrOrNil(r.EpisodeID),
			CommentCount: int64(len(r.Comments)), Comments: comments, Note: strPtrOrNil(r.Note),
			Verdicts: verdictViews(r.Verdicts), Thread: threadViews(r.Thread), Reviewers: rosterViews(r.Reviewers),
			SubjectRef: subjectRefView(r.SubjectRef), Round: roundNumberOrNil(r.Round),
			DecidedBy: strPtrOrNil(r.DecidedBy), DecidedAt: strPtrOrNil(r.DecidedAt),
			Provenance: revisionProvenance(r.Provenance),
		})
	}
	return out
}

// verdictViews carries the round's verdicts onto the wire. A revision with none — a
// dispatch revision, or one reconstructed from a row that predates the round ledger —
// carries NO array rather than an empty one: "this record holds no verdicts" and "this
// review was decided with no verdict cast" are different facts, and the omitted field is
// the first of them.
func verdictViews(verdicts []projectstate.ReviewVerdict) []ReviewVerdictView {
	if len(verdicts) == 0 {
		return nil
	}
	out := make([]ReviewVerdictView, 0, len(verdicts))
	for _, v := range verdicts {
		out = append(out, ReviewVerdictView{
			ReviewerRole: v.ReviewerRole, Actor: strPtrOrNil(v.Actor), Verdict: verdictKind(v.Verdict),
			Summary: strPtrOrNil(v.Summary), At: v.At, AttemptID: strPtrOrNil(v.AttemptID),
		})
	}
	return out
}

// verdictKind names the stored verdict on the wire. Total over the vocabulary with no
// default arm; an out-of-vocabulary value reaches the wire verbatim rather than being
// blessed into an approval it was not.
func verdictKind(v projectstate.VerdictKind) ReviewVerdictKind {
	switch v {
	case projectstate.VerdictApprove:
		return VerdictApprove
	case projectstate.VerdictSendBack:
		return VerdictSendBack
	case projectstate.VerdictAbstain:
		return VerdictAbstain
	}
	return ReviewVerdictKind(v)
}

// threadViews carries the round's comment thread — replies, status and all — verbatim.
func threadViews(thread []projectstate.ReviewComment) []ReviewThreadComment {
	if len(thread) == 0 {
		return nil
	}
	out := make([]ReviewThreadComment, 0, len(thread))
	for _, c := range thread {
		replies := make([]ReviewThreadReply, 0, len(c.Replies))
		for _, rep := range c.Replies {
			replies = append(replies, ReviewThreadReply{ID: rep.ID, AuthorRole: rep.AuthorRole, Text: rep.Text, At: rep.At})
		}
		out = append(out, ReviewThreadComment{
			ID: c.ID, Anchor: c.Anchor, AnchorText: strPtrOrNil(c.AnchorText), Text: c.Text,
			AuthorRole: c.AuthorRole, Round: c.Round, Status: c.Status, Replies: replies,
			Reopened: c.Reopened, Type: c.Type, Addressee: strPtrOrNil(c.Addressee),
		})
	}
	return out
}

// rosterViews carries the roster the round was opened with.
func rosterViews(seats []projectstate.RoundReviewer) []ReviewRosterSeat {
	if len(seats) == 0 {
		return nil
	}
	out := make([]ReviewRosterSeat, 0, len(seats))
	for _, s := range seats {
		out = append(out, ReviewRosterSeat{Role: s.Role, Actor: s.Actor, Required: s.Required})
	}
	return out
}

// subjectRefView carries what the round judged. A revision with no subject at all — every
// reconstructed one, since a pre-ledger row recorded none — omits the field rather than
// shipping an empty ref that reads like a subject nobody can open.
func subjectRefView(s projectstate.SubjectRef) *ReviewSubjectRef {
	if s.Ref == "" {
		return nil
	}
	return &ReviewSubjectRef{Kind: string(s.Kind), Ref: s.Ref}
}

// roundNumberOrNil omits the round on a revision that has none: a dispatch revision, and
// a reconstruction placed beneath the ledger, whose attempt number is a sort position
// rather than a count of reviews (reviewRevision says why).
func roundNumberOrNil(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}

// revisionProvenance names the origin on the wire. OriginSynthesized is the EMPTY string
// in storage on purpose (a dropped stamp fails suspicious); the wire spells it out.
func revisionProvenance(o projectstate.RecordOrigin) TaskRevisionProvenance {
	switch o {
	case projectstate.OriginObserved:
		return TaskRevisionObserved
	case projectstate.OriginBackfilled:
		return TaskRevisionBackfilled
	case projectstate.OriginSynthesized:
		return TaskRevisionSynthesized
	}
	return TaskRevisionSynthesized // an unknown origin is as bad as synthesized (originRank)
}

// ---------------------------------------------------------------------------
// STAGE 4a — THE TWELVE-OP DISPATCHER
//
// Everything above this banner is the three rails, moved verbatim. Everything
// below is the ONE Manager the model now carries: the twelve contract ops, the
// rail resolver they route on, the single worker manifest and the two Schedules.
// 4b replaces the three rails with one generic DAG walker and these twelve
// bodies stop being a switch.
// ---------------------------------------------------------------------------

// deliveryManager is the ONE Manager of the Project Delivery Workflow volatility. In
// stage 4a it is deliberately a DISPATCHER: the three rails it replaces are still here,
// moved verbatim, and the twelve ops route onto their forty. That is the whole point of
// splitting stage 4 — the model, the package and the wire move in one reviewable commit
// while the choreography stays byte-for-byte what nineteen replay fixtures already pin.
// Stage 4b replaces the three rails with one generic DAG walker and these twelve bodies
// stop being a switch.
type deliveryManager struct {
	sd *systemDesignManager
	pd *projectDesignManager
	cs *constructionManager

	// railFor's input, held once so no op re-reads it from a rail's own field.
	projectState projectstate.ProjectStateAccess
}

var _ DeliveryManager = (*deliveryManager)(nil)

// newDeliveryManager is the hand-written builder the GENERATED NewDeliveryManager
// delegates to. Its parameter list is the contract's `deps` list in order, and it
// splits that union back out across the three moved rails — which is the only place
// in this package that still knows there were three.
func newDeliveryManager(
	c client.Client,
	projectState projectstate.ProjectStateAccess,
	art artifact.ArtifactAccess,
	interventionEng intervention.InterventionEngine,
	reviewEng review.ReviewEngine,
	estimator estimation.EstimationEngine,
	operationEstimator operationestimation.OperationEstimationEngine,
	billingEstimator billing.BillingEngine,
	pipeline agenticjob.AgenticJobAccess,
	rail sourcecontrol.SourceControlAccess,
	constructionTransition projectstate.ConstructionTransitionAccess,
	gitStatus projectstate.GitActivityStatusAccess,
	designSession projectstate.DesignSessionAccess,
	activityExecution projectstate.ActivityExecutionAccess,
	bus messagebus.MessageBus,
	episodes episode.EpisodeAccess,
	escalationWaitTimeout time.Duration,
	interventionMode string,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
	repoBase string,
) *deliveryManager {
	return &deliveryManager{
		sd: newSystemDesignManager(c, projectState, pipeline, rail, repo, estimator,
			designSession, activityExecution, episodes, repoBase),
		pd: newProjectDesignManager(c, projectState, pipeline, rail, estimator,
			operationEstimator, billingEstimator, designSession, activityExecution, episodes, repo),
		cs: newConstructionManager(c, projectState, art, interventionEng, reviewEng, pipeline,
			rail, constructionTransition, gitStatus, designSession, activityExecution, bus,
			episodes, escalationWaitTimeout, interventionMode, repo),
		projectState: projectState,
	}
}

// rail names which of the three moved choreographies owns an activity. It is derived,
// never stored: projectstate.ClassifyActivity already maps the committed activity id to
// an ActivityType, and the design types are exactly the three the plan derivation emits
// as activities 1-3 (requirements, architecture, projectDesign). A construction activity
// is anything else with a committed row.
type rail int

const (
	railUnknown rail = iota
	railSystemDesign
	railProjectDesign
	railConstruction
)

// railFor reads the project once, classifies the activity, and returns both the rail
// that owns it and the lifecycle its task ids come from.
//
// projectstate.ErrDesignActivityNotDispatchable is TOLERATED here: it is rule 0 of
// ClassifyActivity and says only that the PUMP does not dispatch the activity, not that
// it is unclassifiable — and the three activities it names are exactly the two design
// rails' own.
func (m *deliveryManager) railFor(rc fwmanager.Context, projectID ProjectID, activityID ActivityID) (rail, methodassets.Lifecycle, error) {
	if projectID == "" {
		return railUnknown, methodassets.Lifecycle{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return railUnknown, methodassets.Lifecycle{}, newError(fwmanager.ContractMisuse, "empty activityId")
	}
	id := string(activityID)
	proj, err := m.projectState.ReadProject(fwra.Context{Context: rc.Context}, projectstate.ProjectID(projectID))
	if err != nil {
		return railUnknown, methodassets.Lifecycle{}, mapRAError(err, "projectStateAccess.ReadProject")
	}
	item, ok := committedActivityItem(proj, id)
	if !ok {
		return railUnknown, methodassets.Lifecycle{}, newError(fwmanager.NotFound,
			"no activity "+id+" in the committed activity list")
	}
	typ, variant, err := projectstate.ClassifyActivity(id, item.WorkerClass, item.Coding)
	if err != nil && !errors.Is(err, projectstate.ErrDesignActivityNotDispatchable) {
		return railUnknown, methodassets.Lifecycle{}, newError(fwmanager.FailedPrecondition, err.Error())
	}
	lc, ok := methodassets.LifecycleFor(projectstate.LifecycleKeyFor(typ, variant))
	if !ok {
		return railUnknown, methodassets.Lifecycle{}, newError(fwmanager.Infrastructure,
			"the platform's method assets carry no lifecycle for activity type "+projectstate.LifecycleKeyFor(typ, variant))
	}
	if typ == projectstate.ActivityTypeRequirements || typ == projectstate.ActivityTypeArchitecture {
		return railSystemDesign, lc, nil
	}
	if typ == projectstate.ActivityTypeProjectDesign {
		return railProjectDesign, lc, nil
	}
	return railConstruction, lc, nil
}

// artifactKindForTask resolves a task id to the artifact the task is ABOUT. A dispatch
// task names its own; a review task carries `reviews`, naming the dispatch it judges,
// and that dispatch names the artifact. This is the same chain
// webApp/src/components/activity/activityViewToGraph.ts artifactKindOf walks
// client-side; keep the two consistent. (The SPA has a third fallback, on the task's
// revisionGroup — method-assets' Go LifecycleTask carries no such field, so the Go side
// stops at the two-step chain and reports a ContractMisuse rather than guessing.)
func artifactKindForTask(lc methodassets.Lifecycle, taskID string) (ArtifactKind, bool) {
	t, ok := lifecycleTaskByID(lc, taskID)
	if !ok {
		return 0, false
	}
	name := t.ArtifactKind
	if name == "" && t.Reviews != "" {
		if judged, ok := lifecycleTaskByID(lc, t.Reviews); ok {
			name = judged.ArtifactKind
		}
	}
	if name == "" {
		return 0, false
	}
	kind, ok := projectstate.ArtifactKindFromWireName(lowerFirstRune(name))
	if !ok {
		return 0, false
	}
	return ArtifactKind(kind), true
}

func lifecycleTaskByID(lc methodassets.Lifecycle, taskID string) (methodassets.LifecycleTask, bool) {
	for _, t := range lc.Tasks {
		if t.ID == taskID {
			return t, true
		}
	}
	return methodassets.LifecycleTask{}, false
}

// lowerFirstRune turns a lifecycle's PascalCase artifactKind ("CoreUseCases",
// "SdpReview") into the canonical camelCase wire name projectstate keys on.
func lowerFirstRune(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToLower(string(r[0])) + string(r[1:])
}

// designKindFor is the rail-scoped resolution the design ops share: it insists the task
// names an artifact, and names the op in the misuse when it does not.
func designKindFor(lc methodassets.Lifecycle, taskID, op string) (ArtifactKind, error) {
	kind, ok := artifactKindForTask(lc, taskID)
	if !ok {
		return 0, newError(fwmanager.ContractMisuse,
			"deliveryManager."+op+": task "+taskID+" names no artifact in this activity's lifecycle")
	}
	return kind, nil
}

// phase1Kind reports whether an artifact kind belongs to Phase 1 (KindMission …
// KindStandardCheck) rather than Phase 2 (KindPlanningAssumptions … KindSdpReview).
// It is the split QueryProjectView's `session` and `episodes` reads route on.
func phase1Kind(kind ArtifactKind) bool {
	return kind <= KindStandardCheck
}

// sdpReviewTaskID is the projectDesign lifecycle's ONE task — the single deterministic
// M0 cost-approval gate (spec §6/R7), which is computed rather than dispatched, so it
// carries an artifactKind of its own and no `reviews` target.
const sdpReviewTaskID = "sdpReview"

// requireActivityTask is the emptiness guard every activity-addressed op leads with.
// The contract's `required` is PRESENCE-only (2026-08-13 ruling), so "" arrives as a
// legitimately-sent value and the Manager is what rejects it. Each of the three moved
// rails rejected the same emptiness one hop further down; this hoists the check to the
// one entry point so every op states its own precondition.
func requireActivityTask(projectID ProjectID, activityID ActivityID, taskID string) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwmanager.ContractMisuse, "empty activityId")
	}
	if strings.TrimSpace(taskID) == "" {
		return newError(fwmanager.ContractMisuse, "empty taskId")
	}
	return nil
}

// requireActivity is requireActivityTask for the two ops that address an activity but
// no single task of it.
func requireActivity(projectID ProjectID, activityID ActivityID) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwmanager.ContractMisuse, "empty activityId")
	}
	return nil
}

// ---- op 1: StartProject ----------------------------------------------------

// StartProject folds the four former catalog/entry ops into one: create the project
// (when no id is given), set the operating model, set the research input, and start the
// first design activity. Every step is SKIPPED when its argument is absent, so the same
// op serves "create and start" and "add research to an existing project".
//
// projectID IS THE CREATE SIGNAL, and it is a *string rather than a *ProjectID on
// purpose. The REST route carries it in the BODY, not the path: the http generator
// routes any param whose schema $refs a scalar-string $def ending in "ID" into a path
// segment and ignores `pointer` (framework-go-http-generator httpgen/plan.go
// isIDPathParam), and net/http's mux cannot match an empty segment — so while it was a
// $ref the "absent id" that MEANS create was unreachable over REST and only the MCP
// surface could create a project. A plain string with x-go-name keeps it out of the
// path, which is the same idiom every other body-carried id in this contract uses
// (ProjectViewQuery.projectId, ReviewDecisionInput.optionId).
func (m *deliveryManager) StartProject(rc fwmanager.Context, owner OwnerScope, name string, projectID *string, model *OperatingModel, research *ResearchInput, start bool) (StartProjectResult, error) {
	var out StartProjectResult
	id := ProjectID("")
	if projectID == nil {
		if owner == "" {
			return out, newError(fwmanager.ContractMisuse, "deliveryManager.StartProject: a new project needs an owner")
		}
		if name == "" {
			return out, newError(fwmanager.ContractMisuse, "deliveryManager.StartProject: a new project needs a name")
		}
		created, err := m.sd.CreateProject(rc, owner, name)
		if err != nil {
			return out, err
		}
		id = created
	} else {
		id = ProjectID(*projectID)
	}
	out.ProjectID = id
	if model != nil {
		v, err := m.sd.SetOperatingModel(rc, id, *model)
		if err != nil {
			return out, err
		}
		out.Version = v
	}
	if research != nil {
		v, err := m.sd.SetResearchInput(rc, id, *research)
		if err != nil {
			return out, err
		}
		out.Version = v
	}
	if out.Version == 0 {
		st, err := m.sd.GetProject(rc, id)
		if err != nil {
			return out, err
		}
		out.Version = Version(st.Version)
	}
	if start {
		ref, err := m.sd.StartSystemDesign(rc, id)
		if err != nil {
			return out, err
		}
		out.Session = &ref
	}
	return out, nil
}

// ---- op 2: ExecuteNextActivity ---------------------------------------------

// ExecuteNextActivity is the delivery pump's one tick, forwarded unchanged.
func (m *deliveryManager) ExecuteNextActivity(rc fwmanager.Context, projectID ProjectID, tickID string) (PumpResult, error) {
	if projectID == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if strings.TrimSpace(tickID) == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}
	return m.cs.ExecuteNextActivity(rc, projectID, tickID)
}

// ---- op 3: DispatchActivityTask --------------------------------------------

// DispatchActivityTask asks for one task of one activity to be produced. On the two
// design rails that is the artifact draft (or re-draft, when feedback rides along); on
// the projectDesign rail's single M0 gate it is the SDP assembly. The construction rail
// has no run/re-run op before stage 4b — a send-back re-dispatches its task from inside
// the activity's own workflow — so it answers FailedPrecondition rather than pretending.
func (m *deliveryManager) DispatchActivityTask(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, feedback *ReviewFeedback) (SessionRef, error) {
	if err := requireActivityTask(projectID, activityID, taskID); err != nil {
		return "", err
	}
	r, lc, err := m.railFor(rc, projectID, activityID)
	if err != nil {
		return "", err
	}
	switch r {
	case railSystemDesign:
		kind, err := designKindFor(lc, taskID, "DispatchActivityTask")
		if err != nil {
			return "", err
		}
		return m.sd.RequestArtifactDraft(rc, projectID, kind, feedback)
	case railProjectDesign:
		if taskID == sdpReviewTaskID {
			return m.pd.RequestSDPCommit(rc, projectID)
		}
		kind, err := designKindFor(lc, taskID, "DispatchActivityTask")
		if err != nil {
			return "", err
		}
		return m.pd.RequestArtifactDraft(rc, projectID, kind, feedback)
	case railConstruction:
		return "", newError(fwmanager.FailedPrecondition,
			"deliveryManager.DispatchActivityTask: construction run/re-run has no op before stage 4b — a send-back re-dispatches the task")
	case railUnknown:
		return "", newError(fwmanager.FailedPrecondition, "deliveryManager.DispatchActivityTask: the activity has no rail")
	}
	return "", newError(fwmanager.FailedPrecondition, "deliveryManager.DispatchActivityTask: the activity has no rail")
}

// ---- op 4: SubmitReviewDecision --------------------------------------------

// SubmitReviewDecision is the single write behind every gate in the product. It routes
// the decision onto whichever of the nine writers the rail and the decision name.
func (m *deliveryManager) SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, decision ReviewDecisionInput, feedback *ReviewFeedback) error {
	if err := requireActivityTask(projectID, activityID, taskID); err != nil {
		return err
	}
	r, lc, err := m.railFor(rc, projectID, activityID)
	if err != nil {
		return err
	}
	switch r {
	case railSystemDesign:
		return m.submitSystemDesignDecision(rc, projectID, lc, taskID, decision, feedback)
	case railProjectDesign:
		return m.submitProjectDesignDecision(rc, projectID, lc, taskID, decision, feedback)
	case railConstruction:
		return m.submitConstructionDecision(rc, projectID, activityID, taskID, decision, feedback)
	case railUnknown:
		return newError(fwmanager.FailedPrecondition, "deliveryManager.SubmitReviewDecision: the activity has no rail")
	}
	return newError(fwmanager.FailedPrecondition, "deliveryManager.SubmitReviewDecision: the activity has no rail")
}

func (m *deliveryManager) submitSystemDesignDecision(rc fwmanager.Context, projectID ProjectID, lc methodassets.Lifecycle, taskID string, decision ReviewDecisionInput, feedback *ReviewFeedback) error {
	if decision.Decision == ReviewAdvance {
		// The gating outcome is readable through QueryProjectView(summary); the
		// PhaseAdvanceResult is not part of the twelve-op surface.
		_, err := m.sd.AdvancePhase(rc, projectID, deliveryDerefBool(decision.AcknowledgeStale))
		return err
	}
	kind, err := designKindFor(lc, taskID, "SubmitReviewDecision")
	if err != nil {
		return err
	}
	if decision.Decision == ReviewSetCommentStatus {
		return m.sd.SetReviewCommentStatus(rc, projectID, kind, deliveryDerefString(decision.CommentID), deliveryDerefString(decision.CommentStatus))
	}
	return m.sd.SubmitReviewDecision(rc, projectID, kind, decision.Decision, feedback)
}

func (m *deliveryManager) submitProjectDesignDecision(rc fwmanager.Context, projectID ProjectID, lc methodassets.Lifecycle, taskID string, decision ReviewDecisionInput, feedback *ReviewFeedback) error {
	if decision.Decision == ReviewAdvance {
		_, err := m.pd.AdvanceToConstruction(rc, projectID, deliveryDerefBool(decision.AcknowledgeStale))
		return err
	}
	if taskID == sdpReviewTaskID && (decision.Decision == ReviewApprove || decision.Decision == ReviewReject) {
		sdp := SDPCommit
		if decision.Decision == ReviewReject {
			sdp = SDPRejectAll
		}
		var opt *OptionID
		if decision.OptionID != nil {
			o := OptionID(*decision.OptionID)
			opt = &o
		}
		return m.pd.SubmitSDPDecision(rc, projectID, sdp, opt, feedback)
	}
	kind, err := designKindFor(lc, taskID, "SubmitReviewDecision")
	if err != nil {
		return err
	}
	if decision.Decision == ReviewSetCommentStatus {
		return m.pd.SetReviewCommentStatus(rc, projectID, kind, deliveryDerefString(decision.CommentID), deliveryDerefString(decision.CommentStatus))
	}
	return m.pd.SubmitReviewDecision(rc, projectID, kind, decision.Decision, feedback)
}

func (m *deliveryManager) submitConstructionDecision(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, decision ReviewDecisionInput, feedback *ReviewFeedback) error {
	switch decision.Decision {
	case ReviewApprove:
		return m.cs.SubmitPhaseDecision(rc, projectID, activityID, taskID, PhaseApprove, feedback)
	case ReviewReject:
		return m.cs.SubmitPhaseDecision(rc, projectID, activityID, taskID, PhaseSendBack, feedback)
	case ReviewDecisionUnknown, ReviewWithdraw, ReviewAdvance, ReviewSetCommentStatus:
		return newError(fwmanager.ContractMisuse,
			"deliveryManager.SubmitReviewDecision: the construction rail has no comment-status or withdraw verb until stage 4b")
	}
	return newError(fwmanager.ContractMisuse,
		"deliveryManager.SubmitReviewDecision: the construction rail has no comment-status or withdraw verb until stage 4b")
}

// ---- op 5: AskQuestions ----------------------------------------------------

// AskQuestions records anchored questions against a task's review thread and dispatches
// the lightweight answer job to the addressed role.
func (m *deliveryManager) AskQuestions(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, addressee string, questions []AnchoredComment) error {
	if err := requireActivityTask(projectID, activityID, taskID); err != nil {
		return err
	}
	if strings.TrimSpace(addressee) == "" {
		return newError(fwmanager.ContractMisuse, "empty addressee")
	}
	r, lc, err := m.railFor(rc, projectID, activityID)
	if err != nil {
		return err
	}
	if r == railConstruction {
		return newError(fwmanager.ContractMisuse,
			"deliveryManager.AskQuestions: the construction rail has no question verb until stage 4b")
	}
	kind, err := designKindFor(lc, taskID, "AskQuestions")
	if err != nil {
		return err
	}
	if r == railProjectDesign {
		return m.pd.AskQuestions(rc, projectID, kind, addressee, questions)
	}
	return m.sd.AskQuestions(rc, projectID, kind, addressee, questions)
}

// ---- op 6: AcknowledgeStaleBasis -------------------------------------------

// AcknowledgeStaleBasis records the audited "reviewed — unaffected" acknowledgement
// that clears a committed artifact's stale-basis flag without re-opening it.
func (m *deliveryManager) AcknowledgeStaleBasis(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, taskID string, note string) error {
	if err := requireActivityTask(projectID, activityID, taskID); err != nil {
		return err
	}
	if strings.TrimSpace(note) == "" {
		return newError(fwmanager.ContractMisuse, "acknowledging a stale basis requires a note")
	}
	r, lc, err := m.railFor(rc, projectID, activityID)
	if err != nil {
		return err
	}
	if r == railConstruction {
		return newError(fwmanager.ContractMisuse,
			"deliveryManager.AcknowledgeStaleBasis: the construction rail has no stale-basis verb until stage 4b")
	}
	kind, err := designKindFor(lc, taskID, "AcknowledgeStaleBasis")
	if err != nil {
		return err
	}
	if r == railProjectDesign {
		return m.pd.AcknowledgeStaleBasis(rc, projectID, kind, note)
	}
	return m.sd.AcknowledgeStaleBasis(rc, projectID, kind, note)
}

// ---- op 7: SetProjectRunState ----------------------------------------------

// SetProjectRunState is the operator's pause/resume, as one op over an enum rather than
// two verbs. `paused` carries the reason; `running` clears the recorded pause.
func (m *deliveryManager) SetProjectRunState(rc fwmanager.Context, projectID ProjectID, runState ProjectRunState, reason string) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	// A pause is recorded WITH its reason (the construction rail's own precondition,
	// hoisted); a resume clears the recorded pause and carries none.
	if runState == ProjectPaused && strings.TrimSpace(reason) == "" {
		return newError(fwmanager.ContractMisuse, "pausing a project requires a reason")
	}
	switch runState {
	case ProjectPaused:
		return m.cs.PauseProject(rc, projectID, reason)
	case ProjectRunning:
		return m.cs.ResumeProject(rc, projectID)
	}
	return newError(fwmanager.ContractMisuse, "deliveryManager.SetProjectRunState: unknown run state")
}

// ---- op 8: OverrideActivity ------------------------------------------------

// OverrideActivity is the operator's steer on one escalated activity, forwarded
// unchanged.
func (m *deliveryManager) OverrideActivity(rc fwmanager.Context, projectID ProjectID, activityID ActivityID, override ActivityOverride) error {
	if err := requireActivity(projectID, activityID); err != nil {
		return err
	}
	if strings.TrimSpace(override.Notes) == "" {
		return newError(fwmanager.ContractMisuse, "an operator override requires notes")
	}
	return m.cs.OverrideActivity(rc, projectID, activityID, override)
}

// ---- op 9: ReplanProject ---------------------------------------------------

// ReplanProject is the replan sweep's one tick, forwarded unchanged.
func (m *deliveryManager) ReplanProject(rc fwmanager.Context, projectID *ProjectID, tickID string) (ReplanSweepResult, error) {
	if strings.TrimSpace(tickID) == "" {
		return ReplanSweepResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}
	return m.cs.RunReplanSweep(rc, projectID, tickID)
}

// ---- op 10: SetProjectExecutionPolicy --------------------------------------

// SetProjectExecutionPolicy sets the project's review policy. A preset alone names one
// of the shipped presets; an explicit policy overrides it field by field.
func (m *deliveryManager) SetProjectExecutionPolicy(rc fwmanager.Context, projectID ProjectID, policy ExecutionPolicyInput) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if policy.Policy == nil && strings.TrimSpace(policy.Preset) == "" {
		return newError(fwmanager.ContractMisuse, "an execution policy needs a preset or an explicit policy")
	}
	if policy.Policy == nil {
		return m.cs.SetReviewPolicy(rc, projectID, policy.Preset)
	}
	return m.cs.UpdateReviewPolicy(rc, projectID, *policy.Policy)
}

// ---- op 11: QueryProjectView -----------------------------------------------

// QueryProjectView is the ONE read of the twelve. Its kind selects which of the
// thirteen former readers answers, and the query object carries the selector that kind
// needs; a missing selector is a ContractMisuse that names it.
//
// GetEpisodeTimeline had three byte-identical implementations (all three read the same
// episodeAccess); the construction one is the copy this keeps.
func (m *deliveryManager) QueryProjectView(rc fwmanager.Context, query ProjectViewQuery) (ProjectView, error) {
	out := ProjectView{Kind: query.Kind}
	switch query.Kind {
	case ProjectViewSummary:
		return m.queryProjectSummary(rc, query, out)
	case ProjectViewProjects:
		return m.queryProjectList(rc, query, out)
	case ProjectViewSession:
		return m.querySessionView(rc, query, out)
	case ProjectViewPump:
		return m.queryPumpView(rc, query, out)
	case ProjectViewDesignHealth:
		return m.queryDesignHealthView(rc, query, out)
	case ProjectViewEpisodes:
		return m.queryEpisodesView(rc, query, out)
	case ProjectViewTimeline:
		return m.queryTimelineView(rc, query, out)
	}
	return out, nil
}

// queryProjectSummary answers the `summary` kind: one project's head state.
func (m *deliveryManager) queryProjectSummary(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "summary")
	if err != nil {
		return out, err
	}
	st, err := m.sd.GetProject(rc, id)
	if err != nil {
		return out, err
	}
	out.Summary = &st
	return out, nil
}

// queryProjectList answers the `projects` kind: every project of one owner.
func (m *deliveryManager) queryProjectList(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	if query.Owner == nil {
		return out, missingSelector("projects", "owner")
	}
	list, err := m.sd.ListProjects(rc, *query.Owner)
	if err != nil {
		return out, err
	}
	out.Projects = list
	return out, nil
}

// queryPumpView answers the `pump` kind: whether the project's delivery pump is running.
func (m *deliveryManager) queryPumpView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "pump")
	if err != nil {
		return out, err
	}
	st, err := m.cs.GetPumpStatus(rc, id)
	if err != nil {
		return out, err
	}
	out.Pump = &st
	return out, nil
}

// queryDesignHealthView answers the `designHealth` kind: the live Method-rule findings.
func (m *deliveryManager) queryDesignHealthView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "designHealth")
	if err != nil {
		return out, err
	}
	dh, err := m.sd.GetDesignHealth(rc, id)
	if err != nil {
		return out, err
	}
	out.DesignHealth = &dh
	return out, nil
}

// queryTimelineView answers the `timeline` kind: one episode's full trace.
func (m *deliveryManager) queryTimelineView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "timeline")
	if err != nil {
		return out, err
	}
	if query.EpisodeID == nil {
		return out, missingSelector("timeline", "episodeId")
	}
	tl, err := m.cs.GetEpisodeTimeline(rc, id, *query.EpisodeID)
	if err != nil {
		return out, err
	}
	out.Timeline = &tl
	return out, nil
}

// querySessionView answers the `session` kind: an artifactKind selects the Phase-1 or
// Phase-2 design session by its phase, an activityId selects the construction session.
func (m *deliveryManager) querySessionView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "session")
	if err != nil {
		return out, err
	}
	if query.ArtifactKind != nil {
		if phase1Kind(*query.ArtifactKind) {
			v, err := m.sd.GetSessionState(rc, id, *query.ArtifactKind)
			if err != nil {
				return out, err
			}
			out.Session = &v
			return out, nil
		}
		v, err := m.pd.GetSessionState(rc, id, *query.ArtifactKind)
		if err != nil {
			return out, err
		}
		out.ProjectSession = &v
		return out, nil
	}
	if query.ActivityID != nil {
		act := ActivityID(*query.ActivityID)
		v, err := m.cs.GetSessionState(rc, id, &act)
		if err != nil {
			return out, err
		}
		out.ConstructionSession = &v
		return out, nil
	}
	v, err := m.cs.GetSessionState(rc, id, nil)
	if err != nil {
		return out, err
	}
	out.ConstructionSession = &v
	return out, nil
}

// queryEpisodesView answers the `episodes` kind: an artifactKind lists a design
// artifact's episodes (Phase-1 or Phase-2 by its phase), an activityId lists a
// construction activity's.
func (m *deliveryManager) queryEpisodesView(rc fwmanager.Context, query ProjectViewQuery, out ProjectView) (ProjectView, error) {
	id, err := requireProjectID(query, "episodes")
	if err != nil {
		return out, err
	}
	if query.ArtifactKind != nil {
		if phase1Kind(*query.ArtifactKind) {
			recs, err := m.sd.ListEpisodesForArtifact(rc, id, *query.ArtifactKind)
			if err != nil {
				return out, err
			}
			out.Episodes = recs
			return out, nil
		}
		recs, err := m.pd.ListEpisodesForArtifact(rc, id, *query.ArtifactKind)
		if err != nil {
			return out, err
		}
		out.Episodes = recs
		return out, nil
	}
	if query.ActivityID == nil {
		return out, missingSelector("episodes", "artifactKind or activityId")
	}
	recs, err := m.cs.ListEpisodesForActivity(rc, id, *query.ActivityID)
	if err != nil {
		return out, err
	}
	out.Episodes = recs
	return out, nil
}

func requireProjectID(query ProjectViewQuery, kind string) (ProjectID, error) {
	if query.ProjectID == nil || *query.ProjectID == "" {
		return "", missingSelector(kind, "projectId")
	}
	return ProjectID(*query.ProjectID), nil
}

func missingSelector(kind, selector string) error {
	return newError(fwmanager.ContractMisuse,
		"deliveryManager.QueryProjectView: the "+kind+" view needs "+selector)
}

// ---- op 12: QueryActivityView ----------------------------------------------

// QueryActivityView is the Activity Experience's single read, forwarded unchanged.
func (m *deliveryManager) QueryActivityView(rc fwmanager.Context, projectID ProjectID, activityID ActivityID) (ActivityView, error) {
	if err := requireActivity(projectID, activityID); err != nil {
		return ActivityView{}, err
	}
	return m.cs.QueryActivityView(rc, projectID, activityID)
}

// ---- ONE worker, ONE queue -------------------------------------------------

// RegisterManagerWorker registers the ONE delivery worker: eleven workflow types under
// their EXISTING registered names (R2 — nineteen replay fixtures replay against those
// strings) and the generated Activity set of the merged contract, all on task queue
// "delivery".
func RegisterManagerWorker(w worker.Worker, m DeliveryManager) {
	impl, ok := m.(*deliveryManager)
	if !ok {
		panic("delivery: RegisterManagerWorker requires a *deliveryManager from NewDeliveryManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
}

// WorkerManifest is the union of the three rails' manifests: their eleven workflow
// entry functions under their unchanged names, one merged ActivityOptions hook, and one
// genActivities threading every dep of the merged contract.
func (m *deliveryManager) WorkerManifest() genWorkerManifest {
	sdmf := m.sd.WorkerManifest()
	pdmf := m.pd.WorkerManifest()
	csmf := m.cs.WorkerManifest()
	wfs := make([]genRegisteredWorkflow, 0, len(sdmf.Workflows)+len(pdmf.Workflows)+len(csmf.Workflows))
	wfs = append(wfs, sdmf.Workflows...)
	wfs = append(wfs, pdmf.Workflows...)
	wfs = append(wfs, csmf.Workflows...)
	return genWorkerManifest{
		Workflows: wfs,
		// deliveryActivityOptions resolves a name against the three hooks in rail order
		// with CONSTRUCTION LAST-WINS, because construction's presets are the tuned ones
		// (see the doc comment there).
		ActivityOptions: deliveryActivityOptions(sdmf.ActivityOptions, pdmf.ActivityOptions, csmf.ActivityOptions),
		Activities:      m.genActivities(),
	}
}

// deliveryActivityOptions merges the three rails' per-activity option hooks. Where two
// rails answer for the SAME generated activity name they agree in all but a handful of
// cases; where they differ the CONSTRUCTION answer wins, because construction's presets
// are the tuned ones (the design rails inherited the shape and never re-tuned it). The
// divergence is earmarked for 4b, when one rail leaves one answer.
func deliveryActivityOptions(hooks ...func(activityName string) (workflow.ActivityOptions, bool)) func(activityName string) (workflow.ActivityOptions, bool) {
	return func(name string) (workflow.ActivityOptions, bool) {
		var out workflow.ActivityOptions
		found := false
		for _, h := range hooks {
			if h == nil {
				continue
			}
			if opts, ok := h(name); ok {
				out, found = opts, true
			}
		}
		return out, found
	}
}

// genActivities threads every dep of the merged contract into the ONE generated
// Activity set. The three rails' own manifests each threaded their own subset; the
// merged worker registers the union, which is what takes the registered-name golden
// from three workers' overlapping sets to one.
func (m *deliveryManager) genActivities() genActivities {
	return genActivities{
		ProjectState:           m.cs.projectState,
		Artifact:               m.cs.artifact,
		Pipeline:               m.cs.pipeline,
		Rail:                   m.cs.rail,
		ConstructionTransition: m.cs.constructionTransition,
		GitStatus:              m.cs.gitActivityStatus,
		Episodes:               m.cs.episodes,
		DesignSession:          m.cs.designSession,
		MessageBus:             m.cs.messageBus,
		ActivityExecution:      m.cs.activityExecution,
	}
}

// deliveryDerefBool / deliveryDerefString read ReviewDecisionInput's OPTIONAL extras.
// The contract marks only `decision` required — presence-only, per the 2026-08-13
// strictness ruling — so the extras arrive as pointers and their absence is the zero
// value the forwarded op already means by it. (The projectDesign rail already had a
// derefString of its own, with a caller-supplied fallback; these two are the
// dispatcher's, with the zero value baked in.)
func deliveryDerefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func deliveryDerefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
