// Package construction is the constructionManager component of the aiarch server's
// Manager layer — the use-case façade that drives a project through Phase 3 of
// The Method (Construction), per the senior-frozen contract
// designs/aiarch/implementation/contracts/constructionManager.md (C-MCN).
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it exposes RegisterSchedules — the
// platform-wide pump-sweep (30s) and replanSweep (5m) Temporal Schedules,
// registered via the messageBus Utility's registerSchedule verb, for the
// composition root to call once at process start (Task 7c;
// constructionManager.md §6.1) — defines one Activity per ResourceAccess call,
// owns the Signal/Query handlers and the in-workflow primitives (awaitSignal /
// startTimer / executeChild), and derives the idempotency key
// "${workflowId}:${activityId}" passed down to each RA verb. Temporal lives ONLY
// in this component; the downstream Engines (interventionEngine,
// reviewEngine — pure, in-workflow, by value), the ResourceAccess ports
// (projectStateAccess, artifactAccess, workerAccess, agenticJobAccess) and the
// messageBus Utility import no Temporal.
//
// The pump-sweep Schedule targets PumpSweepWorkflow (pumpsweep.go), NOT
// PumpNextActivityWorkflow directly: a Temporal Schedule's action carries a FIXED
// workflow type + FIXED args on every firing (messagebus.go's RegisterSchedule),
// so it cannot itself vary pumpInput.ProjectID per tick the way ExecuteNextActivity's
// client-driven call does. PumpSweepWorkflow is the thin, platform-wide fan-out this
// forces: it enumerates every construction-phase project (projectStateAccess.
// listProjects) and starts (or, if that project's pump is already cascading, leaves
// alone) that project's own PumpNextActivityWorkflow — which keeps every one of its
// existing single-project semantics (self-cascade, pause gate, dispatch query)
// unchanged. The sweep and ExecuteNextActivity share ONE pump id per project
// (pumpWorkflowID), so whichever entry started the pump, the other joins or skips it.
//
// The FIVE frozen public ops (constructionManager.md §2):
//   - ExecuteNextActivity — Workflow (entry; scheduler-triggered pump; per-activity child)
//   - RunReplanSweep      — Workflow (entry; scheduler-triggered variance sweep)
//   - PauseProject        — Signal (operatorPauseRequested)
//   - OverrideActivity    — Signal (operatorOverride)
//   - GetSessionState     — Query (sessionState, read-only)
//
// File layout (mirrors internal/manager/systemdesign):
//   - constructionmanager.go : the Manager that translates public ops into Temporal client calls (§6.2)
//   - contract.go            : the public façade types + the consumer-side dep interfaces (§3, §5)
//   - workflow.go            : the workflows deps struct + workflow bodies + signal/query handlers (§6.3, §6.6)
//   - activities.go          : the Manager-owned Activity wrappers, as methods on workflows (§6.4)
//   - errors.go              : the port-error -> Temporal-error translation (§6.4)
//   - worker.go              : worker registration of workflows + activities + Schedules (§6.1)
//
// 2026-05-29 agent-role rework note (constructionManager.md top note + workerAccess.md
// §0b): the worker-text → typed-ConstructionOutput parse is NOT a "future
// constructionEngine" / Dispatch-FileUpload concern — workerAccess is now the
// generic typed worker (Generate / GenerateTypedData[T] / Cancel). This Manager's
// SEQUENCE owns the per-step prompt and asks worker.GenerateTypedData[artifact.ConstructionOutput]
// (Manager-Activity-wrapped) for the produced change, and worker.Cancel for the
// operator-pause / takeover abandon path (the DSL-static Cancel(key) edge). The
// five frozen public ops are stable across this; see C-MCN.md completion notes.
package construction

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

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
// hand-written Temporal workflows. The former exported consumer-mirror interfaces +
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
	// project repo) AND activates the branch→PR rail. Threaded into the workflows via
	// wfDeps.Repo (WorkerManifest).
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// messageBus (7b) is the generated messageBus Utility dep — the restricted
	// Manager-only signal/schedule surface. Threaded into genActivities so the
	// workflows can reach registerSchedule/deliverSignal through the generated
	// invokers. Task 7c (landed 2026-08-01) added this Manager's RegisterSchedules
	// (the pump tick and the replan sweep) plus the startup wiring — the
	// composition root now threads the real messageBus.MessageBus here, exactly as
	// it does for billing/operations (main.gen.go; CONSTRUCTION_DRYRUN gates only
	// which Schedules that shared bus actually registers, via hooks.go's
	// FinalizeMessageBus/dryRunConstructionScheduleGate).
	messageBus messagebus.MessageBus

	// designSession (B6) is the generated designSessionAccess dep. Since the B8
	// follow-up it is CONSUMED by the workflows: the pump's whole-aggregate read rides
	// the generated designSessionAccess.readProjectOnBranch invoker with branch ""
	// (main) — the shared projectstate.ProjectEnvelope was extended with the
	// construction-fidelity sections (ActivityConstruction / ServiceContracts /
	// ReviewPolicy, envelope.go) that construction's former local codec carried, which
	// is what retired the last custom Activity (ReadProjectActivity).
	designSession projectstate.DesignSessionAccess

	// episodes (SP1 capture-seam) is the generated episodeAccess dep — the agentic-
	// episode ledger every terminal pipeline observation appends to. Reached ONLY
	// through the generated invoker surface (Acts.EpisodesAppendEpisode) inside the
	// workflows; this field exists to thread it into genActivities.
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

// Compile-time proof the concrete constructionManager satisfies the generated port.
var _ ConstructionManager = (*constructionManager)(nil)

// newConstructionManager is the hand-written, unexported builder the generated
// NewConstructionManager constructor delegates to. It wires the Temporal client + the
// published deps into the façade. The façade itself uses only the client; the deps are
// stored for RegisterWorker (worker.go), which folds them into the Temporal workflows.
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
func (m *constructionManager) ExecuteNextActivity(rc fwm.Context, projectID ProjectID, tickID string) (PumpResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PumpResult{}, newError(fwm.ContractMisuse, "empty projectId")
	}
	if tickID == "" {
		return PumpResult{}, newError(fwm.ContractMisuse, "empty tickId")
	}

	if err := m.refuseWhilePaused(ctx, projectID); err != nil {
		return PumpResult{}, err
	}

	wfID := pumpWorkflowID(projectID)
	slog.Default().InfoContext(ctx, "construction pump: start-or-join",
		"projectId", string(projectID), "workflowId", wfID, "tickId", tickID)
	we, err := m.startOrJoinPump(ctx, projectID)
	if err != nil {
		return PumpResult{}, mapStartError(err)
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
		return newError(fwm.Infrastructure, "read the project before starting construction: "+err.Error())
	}
	if !proj.OperatorPaused {
		return nil
	}
	return newError(fwm.FailedPrecondition, pausedDetail(proj.PauseReason))
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
// running", which is not a failure. PumpResult has no such outcome and fwm has no
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
			return PumpResult{}, newError(fwm.Infrastructure, pumpStillDecidingDetail)
		}
		select {
		case <-ctx.Done():
			return PumpResult{}, newError(fwm.Infrastructure, ctx.Err().Error())
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
		return pumpDispatch{}, nil, newError(fwm.Infrastructure, err.Error())
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
		return PumpResult{}, newError(fwm.Infrastructure, err.Error())
	}
	return result, nil
}

// RunReplanSweep — op 2.2. Temporal Workflow (entry; scheduler-triggered, short).
// Reads in-flight construction state, flags over-threshold variances, surfaces
// them to the operator dashboard — it does NOT auto-replan. An empty result is a
// normal quiet sweep. A nil projectID sweeps all in-flight projects (workflow id
// :all:replanSweep:{tickId}).
func (m *constructionManager) RunReplanSweep(rc fwm.Context, projectID *ProjectID, tickID string) (ReplanSweepResult, error) {
	ctx := rc.Context
	if tickID == "" {
		return ReplanSweepResult{}, newError(fwm.ContractMisuse, "empty tickId")
	}
	var in replanSweepInput
	if projectID != nil {
		if *projectID == "" {
			return ReplanSweepResult{}, newError(fwm.ContractMisuse, "empty projectId")
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
		return ReplanSweepResult{}, mapStartError(err)
	}
	var result ReplanSweepResult
	if err := we.Get(ctx, &result); err != nil {
		return ReplanSweepResult{}, newError(fwm.Infrastructure, err.Error())
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
func (m *constructionManager) PauseProject(rc fwm.Context, projectID ProjectID, reason string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwm.ContractMisuse, "empty projectId")
	}
	if reason == "" {
		return newError(fwm.ContractMisuse, "empty pause reason")
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
func (m *constructionManager) ResumeProject(rc fwm.Context, projectID ProjectID) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwm.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRANotFound(err) {
			return newError(fwm.NotFound, err.Error())
		}
		return newError(fwm.Infrastructure, err.Error())
	}
	if proj.Phase != projectstate.PhaseConstruction {
		return newError(fwm.FailedPrecondition, fmt.Sprintf("project %s is not in construction, so there is no construction to resume", projectID))
	}
	if !proj.OperatorPaused {
		return newError(fwm.FailedPrecondition, "construction is not paused — there is nothing to resume")
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
		return newError(fwm.Infrastructure, "check whether a pause is still being applied: "+err.Error())
	}
	if info := resp.GetWorkflowExecutionInfo(); info != nil && info.GetStatus() == enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return newError(fwm.FailedPrecondition, "a pause is still being applied — retry in a moment")
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
		if !isRAConflict(err) {
			return newError(fwm.Infrastructure, err.Error())
		}
		proj, rerr := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
		if rerr != nil {
			return newError(fwm.Infrastructure, rerr.Error())
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
			return newError(fwm.FailedPrecondition, fmt.Sprintf("a new pause (%s) landed while resuming — review it, then resume again", proj.PauseReason))
		}
		version = proj.Version
	}
	return newError(fwm.FailedPrecondition, "the project changed concurrently while resuming — retry")
}

// resumableStill re-checks, on a re-read, the first two preconditions ResumeProject
// checked on its first read: the project is still in construction and still paused.
// (recordResumed then re-checks the pause in flight, and that the pause is the one the
// operator resumed: a different reason is a different pause the operator has not seen.)
func resumableStill(projectID ProjectID, now projectstate.Project) error {
	switch {
	case now.Phase != projectstate.PhaseConstruction:
		return newError(fwm.FailedPrecondition, fmt.Sprintf("project %s left construction while resuming, so there is no construction to resume", projectID))
	case !now.OperatorPaused:
		return newError(fwm.FailedPrecondition, "construction is not paused — there is nothing to resume")
	}
	return nil
}

// isRAConflict reports whether err is (or wraps) a ResourceAccess version Conflict.
func isRAConflict(err error) bool {
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
func (m *constructionManager) OverrideActivity(rc fwm.Context, projectID ProjectID, activityID ActivityID, override ActivityOverride) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwm.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwm.ContractMisuse, "empty activityId")
	}
	switch override.Kind {
	case OverrideTakeover, OverrideRetry, OverrideSkip, OverrideReassign:
		// ok
	case OverrideUnknown:
		// zero-value sentinel, not a real override kind — same as any unmapped value.
		return newError(fwm.ContractMisuse, fmt.Sprintf("unknown override kind %d", int(override.Kind)))
	default:
		return newError(fwm.ContractMisuse, fmt.Sprintf("unknown override kind %d", int(override.Kind)))
	}
	if strings.TrimSpace(override.Notes) == "" {
		return newError(fwm.ContractMisuse, "an override requires non-empty notes — it is the operator's durable record of WHY the automatic path was steered")
	}
	if err := checkOperatorNoteSize("an override's notes", override.Notes, override.Comments); err != nil {
		return err
	}
	view, err := m.activitySession(ctx, projectID, activityID)
	if err != nil {
		return err
	}
	if view.Stage != StageAwaitingTakeover {
		return newError(fwm.FailedPrecondition, fmt.Sprintf(
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
func (m *constructionManager) GetSessionState(rc fwm.Context, projectID ProjectID, activityID *ActivityID) (ConstructionSessionView, error) {
	ctx := rc.Context
	if projectID == "" {
		return ConstructionSessionView{}, newError(fwm.ContractMisuse, "empty projectId")
	}

	var wfID string
	if activityID != nil {
		if *activityID == "" {
			return ConstructionSessionView{}, newError(fwm.ContractMisuse, "empty activityId")
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
				return ConstructionSessionView{}, newError(fwm.NotFound,
					"no construction session for activity "+string(*activityID)+": the pump has not dispatched it")
			}
			return ConstructionSessionView{}, newError(fwm.NotFound, "construction has not started for this project")
		}
		return ConstructionSessionView{}, mapQueryError(err)
	}
	var view ConstructionSessionView
	if err := enc.Get(&view); err != nil {
		return ConstructionSessionView{}, newError(fwm.Infrastructure, err.Error())
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
func (m *constructionManager) QueryActivityView(rc fwm.Context, projectID ProjectID, activityID ActivityID) (ActivityView, error) {
	ctx := rc.Context
	if projectID == "" {
		return ActivityView{}, newError(fwm.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return ActivityView{}, newError(fwm.ContractMisuse, "empty activityId")
	}
	id := string(activityID)
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return ActivityView{}, mapRAError(err, "projectStateAccess.ReadProject")
	}
	item, ok := committedActivityItem(proj, id)
	if !ok {
		return ActivityView{}, newError(fwm.NotFound, "no activity "+id+" in the committed activity list")
	}
	row := proj.ActivityExecution[id]
	row.ActivityID = id
	typ, variant, resolved, classified := projectstate.ResolveConstructionRow(row, item)
	if !classified {
		return ActivityView{}, newError(fwm.FailedPrecondition, fmt.Sprintf(
			"activity %s (workerClass %q, coding=%v) matches no activity-classification rule, so it has no lifecycle — amend workerClass or coding in the committed activity list",
			id, item.WorkerClass, item.Coding))
	}
	key := projectstate.LifecycleKeyFor(typ, variant)
	lc, ok := methodassets.LifecycleFor(key)
	if !ok {
		return ActivityView{}, newError(fwm.Infrastructure, "the platform's method assets carry no lifecycle for activity type "+key)
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
func (m *constructionManager) GetPumpStatus(rc fwm.Context, projectID ProjectID) (PumpStatus, error) {
	if projectID == "" {
		return PumpStatus{}, newError(fwm.ContractMisuse, "empty projectId")
	}
	dctx, cancel := context.WithTimeout(rc.Context, pumpRPCTimeout)
	defer cancel()
	resp, err := m.client.DescribeWorkflowExecution(dctx, pumpWorkflowID(projectID), "")
	if err != nil {
		if isNotFound(err) {
			return PumpStatus{Open: false}, nil
		}
		return PumpStatus{}, newError(fwm.Infrastructure, "describe the construction pump: "+err.Error())
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
func (m *constructionManager) SubmitPhaseDecision(rc fwm.Context, projectID ProjectID, activityID ActivityID, phase string, decision PhaseDecision, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwm.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwm.ContractMisuse, "empty activityId")
	}
	if err := validatePhaseDecision(phase, decision); err != nil {
		return err
	}
	// A whitespace-only note is an empty one (M3), as it is for an override.
	if decision == PhaseSendBack && (feedback == nil || strings.TrimSpace(feedback.Notes) == "") {
		return newError(fwm.ContractMisuse, "SendBack requires non-empty feedback notes")
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
		return newError(fwm.ContractMisuse, fmt.Sprintf("%s is at most %d characters, anchored comments and their paths included", what, maxOperatorNoteRunes))
	}
	if len(renderNoteBody(notes, noteComments(comments))) > maxOperatorNoteBodyBytes {
		return newError(fwm.ContractMisuse, fmt.Sprintf("%s is at most %d bytes once rendered (anchored comments included); shorten it", what, maxOperatorNoteBodyBytes))
	}
	return nil
}

// activitySession reads one activity's session through the SAME Query GetSessionState
// serves, with its error mapping: no session is NotFound, any other query fault is
// Infrastructure.
func (m *constructionManager) activitySession(ctx context.Context, projectID ProjectID, activityID ActivityID) (ConstructionSessionView, error) {
	return m.GetSessionState(fwm.Context{Context: ctx}, projectID, &activityID)
}

// precheckPhaseDecision is SubmitPhaseDecision's FailedPrecondition gate over the
// activity's session view (B1.3).
func precheckPhaseDecision(v ConstructionSessionView, activityID ActivityID, key string, decision PhaseDecision) error {
	gate := "no gate"
	if v.AwaitingGate != nil {
		gate = *v.AwaitingGate
	}
	if v.Stage != StageAwaitingApproval || gate != key {
		return newError(fwm.FailedPrecondition, fmt.Sprintf("activity %s is at %s/%s, not awaiting %s",
			activityID, sessionStageName(v.Stage), gate, key))
	}
	if decision == PhaseSendBack && v.RedraftExhausted {
		return newError(fwm.FailedPrecondition, fmt.Sprintf(
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
func (m *constructionManager) SetReviewPolicy(rc fwm.Context, projectID ProjectID, preset string) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwm.ContractMisuse, "empty projectId")
	}
	switch preset {
	case projectstate.ReviewPresetVibes, projectstate.ReviewPresetCheckpoints, projectstate.ReviewPresetFull:
		// closed vocabulary — fall through to the write.
	default:
		return newError(fwm.ContractMisuse, fmt.Sprintf("unknown review-policy preset %q (want %q, %q, or %q)",
			preset, projectstate.ReviewPresetVibes, projectstate.ReviewPresetCheckpoints, projectstate.ReviewPresetFull))
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		if isRANotFound(err) {
			return newError(fwm.NotFound, err.Error())
		}
		return newError(fwm.Infrastructure, err.Error())
	}
	policy := proj.ReviewPolicy
	policy.Preset = &preset
	if _, err := m.constructionTransition.RecordReviewPolicy(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), proj.Version, policy, projectstate.RepoCredential{}, fwra.IdempotencyKey(uuid.NewString())); err != nil {
		return newError(fwm.Infrastructure, err.Error())
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
func (m *constructionManager) UpdateReviewPolicy(rc fwm.Context, projectID ProjectID, input ReviewPolicyInput) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwm.ContractMisuse, "empty projectId")
	}
	proj, err := m.projectState.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID))
	if err != nil {
		return newError(fwm.Infrastructure, err.Error())
	}
	policy := projectstate.ReviewPolicyFromGateIDs(input.GatedPhasesByType)
	if _, err := m.constructionTransition.RecordReviewPolicy(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), proj.Version, policy, projectstate.RepoCredential{}, fwra.IdempotencyKey(uuid.NewString())); err != nil {
		return newError(fwm.Infrastructure, err.Error())
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

func mapStartError(err error) error {
	// A "workflow already started" race under UseExisting policy is benign; any
	// other error is treated as an infrastructure fault at the transport layer.
	return newError(fwm.Infrastructure, err.Error())
}

func mapSignalError(err error) error {
	if isNotFound(err) {
		return newError(fwm.NotFound, err.Error())
	}
	return newError(fwm.Infrastructure, err.Error())
}

func mapQueryError(err error) error {
	if isNotFound(err) {
		return newError(fwm.NotFound, err.Error())
	}
	// Failing-workflow-task hygiene (mirrors the design managers): a session being
	// retried after a deploy-time fault rejects queries with raw Temporal internals
	// ("Unable to query workflow due to Workflow Task in failed state") — clients
	// get a clean, actionable Detail instead.
	if strings.Contains(err.Error(), "Workflow Task in failed state") {
		return newError(fwm.Infrastructure,
			"construction session state is temporarily unavailable — the session hit an internal fault and is being retried by the server; try again shortly")
	}
	return newError(fwm.Infrastructure, err.Error())
}

// isNotFound reports whether the Temporal error indicates the addressed execution
// does not exist — typed as *serviceerror.NotFound, the canonical "no such
// workflow" error the SDK returns (mirrors systemdesign's matcher).
//
// QA 2026-07-19 (poll-404 wizard reset): the old substring match ("not found")
// classified *serviceerror.NamespaceNotFound — the server talking to a
// wrong/foreign Temporal backend — as the authoritative session/execution
// NotFound, which clients trust and act on destructively. Only the typed
// execution-NotFound may claim absence; everything else stays Infrastructure.
func isNotFound(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound)
}

var _ ConstructionManager = (*constructionManager)(nil)

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

func newError(kind fwm.Kind, detail string) *fwm.Error {
	return fwm.New(kind, detail)
}

// deps.go declares the hand-written domain VALUE types the Manager's workflow
// vocabulary uses. Per the founder DI model (2026-06-28) the constructionManager's
// GENERATED constructor (contract.gen.go: NewConstructionManager) takes the
// dependencies' PUBLISHED interfaces directly. The two Engines (intervention /
// review) are typed as their PUBLISHED contract interfaces DIRECTLY on
// wfDeps/workflows (workflow.go) — no Manager-local seam interface, no adapter (Task 6).
//
// B8 (custom activities → generated, clean cut) + its follow-up removed EVERY
// Manager-local RA seam that used to live here:
//   - constructionTransitionAccess (10 ops) and gitActivityStatusAccess (6 ops): every
//     write verb is reached through the GENERATED invoker surface (invokers.gen.go:
//     genInvokers.ConstructionTransition* / genInvokers.GitStatus*). wfDeps/workflows
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
			return newError(fwm.ContractMisuse, fmt.Sprintf("the %q gate accepts Approve only — a merge has no draft to send back; steer the activity with OverrideActivity instead", mergeGateKey))
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
		return newError(fwm.ContractMisuse, "empty phase")
	}
	return newError(fwm.ContractMisuse, fmt.Sprintf("unknown phase %q — expected one of requirements|detailed_design|test_plan|construction|integration|%s", phase, mergeGateKey))
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

// changeLedgerPartialResume is the ONE change id guarding D1 in both workflows: the pump's
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

// railCredEnvelope carries the opaque short-lived credential across the Activity
// boundary (and back into the workflow, where it is held for the activity's git
// lifecycle). It is the Manager's OWN transport carrier — it converts to either RA's
// credential type at the call site (the Manager is the seam allowed to touch both).
// The Bytes are write-only at every consumer (never logged); they ride the Temporal
// payload exactly as the rail itself returns them.
type railCredEnvelope struct {
	Bytes     []byte
	ExpiresAt time.Time
}

func (c railCredEnvelope) toRail() sourcecontrol.RepoCredential {
	return sourcecontrol.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

func (c railCredEnvelope) toProjectState() projectstate.RepoCredential {
	return projectstate.RepoCredential{Bytes: c.Bytes, ExpiresAt: c.ExpiresAt}
}

// ---------------------------------------------------------------------------
// git Activity option presets (constructionManager.md §6.4 pattern). Concrete
// RetryPolicy / timeout choices live here, in the Manager.
// ---------------------------------------------------------------------------

// mintCredActivityOptions — the credential mint preset VALUE the manifest's Opts hook
// (workermanifest.go) applies to the GENERATED getInstallationToken invoker. A
// rejected/expired App identity is terminal (fwra.Auth); transport blips retry.
func mintCredActivityOptions() workflow.ActivityOptions {
	return fwm.ActivityPreset{
		Timeout:    15 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.ContractMisuse},
	}.Options()
}

// railActivityOptions — the PR-rail verbs preset VALUE (OpenBranch / OpenPullRequest /
// GetPullRequestStatus / PostReview / MergePullRequest), applied to the GENERATED rail
// invokers via the manifest's Opts hook. Auth + a merge Conflict (not-mergeable) + bad
// input are terminal; transport/rate-limit retry.
func railActivityOptions() workflow.ActivityOptions {
	return fwm.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.NotFound, fwra.Conflict, fwra.ContractMisuse},
	}.Options()
}

// wfDeps bundles every downstream dependency the constructionManager orchestrates,
// assembled by WorkerManifest (workermanifest.go) from the Manager's stored PUBLISHED
// deps and held on the workflows struct. The three Engines are typed as their
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

// workflows is the single constructionManager component struct — the workflow receiver
// (it no longer hosts any Activity methods; every RA op is reached through the
// generated invoker surface, Acts).
type workflows struct {
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

// newWorkflows builds the workflows receiver from the injected seams.
func newWorkflows(d wfDeps) *workflows {
	return &workflows{
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

// Bounds (in-workflow guards; NOT contract surface).
// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
// loop (§6.5).
const maxMutateConflictAttempts = 20

// ---------------------------------------------------------------------------
// Activity option presets (constructionManager.md §6.4). Concrete RetryPolicy /
// timeout choices live here, in the Manager.
// ---------------------------------------------------------------------------

// readProjectActivityOptions is the read preset VALUE (10s; NotFound+ContractMisuse
// terminal) the manifest's Opts hook (workermanifest.go) applies to the two GENERATED
// read invokers the workflows consume — "projectStateAccess.readProjectVersion" and
// "designSessionAccess.readProjectOnBranch" (the whole-aggregate read) — identically
// for both. NotFound stays terminal so a brand-new project's read fails fast into the
// pump's quiet-tick handling (isReadNotFound) instead of retrying.
func readProjectActivityOptions() workflow.ActivityOptions {
	return fwm.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []fwra.Kind{fwra.NotFound, fwra.ContractMisuse},
	}.Options()
}

// submitPipelineActivityOptions / observePipelineActivityOptions are the pipeline preset
// VALUES the manifest's Opts hook (workermanifest.go) applies to the GENERATED pipeline
// invokers by registered name (submit 60s Auth/ContractMisuse-terminal;
// observe/cancel 30s NotFound/Auth-terminal).
func submitPipelineActivityOptions() workflow.ActivityOptions {
	return fwm.ActivityPreset{
		Timeout:    60 * time.Second,
		TerminalRA: []fwra.Kind{fwra.Auth, fwra.ContractMisuse},
	}.Options()
}

func observePipelineActivityOptions() workflow.ActivityOptions {
	return fwm.ActivityPreset{
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
	return fwm.ActivityPreset{
		Timeout:    10 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
}

// appendEpisodeRetryWindow is the HARD wall-clock bound on the episode-append's own
// retry envelope. Attempts are UNCAPPED inside it (bookkeeping must not lose to a
// transient store fault) but they cannot run forever, because the workflow WAITS on this
// activity.
//
// bounded-latency ruling 2026-08-02: local sidecar append failing >2m is not transient;
// business outcome must not stall on telemetry.
const appendEpisodeRetryWindow = 2 * time.Minute

// appendEpisodeActivityOptions is the episode-append preset — DELIBERATELY its own
// envelope, independent of every business preset (§capture-seam): a generous
// per-attempt timeout, UNCAPPED attempts inside appendEpisodeRetryWindow (MaxAttempts
// unset ⇒ Temporal treats it as unlimited), and ContractMisuse terminal (a malformed
// record will never become well-formed by retrying — the caller logs it instead).
// Built from the framework preset plus the ScheduleToCloseTimeout the preset cannot
// express.
func appendEpisodeActivityOptions() workflow.ActivityOptions {
	o := fwm.ActivityPreset{
		Timeout:    30 * time.Second,
		TerminalRA: []fwra.Kind{fwra.ContractMisuse},
	}.Options()
	o.ScheduleToCloseTimeout = appendEpisodeRetryWindow
	return o
}

// stampNoteDeliveredActivityOptions is the delivery stamp's preset (M4): the record
// preset's per-attempt timeout, uncapped attempts inside noteStampRetryWindow, and
// ContractMisuse terminal (the store refusing to stamp one note to two attempts).
func stampNoteDeliveredActivityOptions() workflow.ActivityOptions {
	o := recordActivityOptions()
	o.ScheduleToCloseTimeout = noteStampRetryWindow
	return o
}

// raContractMisuseErrType is the Type() an RA ContractMisuse surfaces as.
var raContractMisuseErrType = fwm.RAErrType(fwra.ContractMisuse)

// isRAContractMisuse reports whether err is (or wraps) an RA ContractMisuse.
func isRAContractMisuse(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raContractMisuseErrType
	}
	return false
}

// raConflictErrType is the canonical Temporal Type() a head-state mutation Activity
// surfaces when expectedVersion is stale; the workflow recovers with the bounded
// re-read→re-apply loop (§6.5).
var raConflictErrType = fwm.RAErrType(fwra.Conflict)

// raNotFoundErrType is the canonical Temporal Type() ReadProject surfaces for a
// brand-new project (no row yet).
var raNotFoundErrType = fwm.RAErrType(fwra.NotFound)

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

// isConflict reports whether err is a head-state mutation's stale-version Conflict.
func isConflict(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raConflictErrType
	}
	return false
}

// isReadNotFound reports whether err is ReadProject's "no row yet" NotFound.
func isReadNotFound(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == raNotFoundErrType
	}
	return false
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
	// querySessionState returns a ConstructionSessionView; backs GetSessionState.
	querySessionState = "sessionState"
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
const (
	// scheduleIDPumpSweep is the platform-wide pump-sweep Schedule id.
	scheduleIDPumpSweep = "construction:pumpSweep"
	// pumpSweepIntervalSecs is the pump-sweep cadence — the single tunable knob.
	pumpSweepIntervalSecs = 30

	// scheduleIDReplanSweep is the platform-wide replan-sweep Schedule id.
	scheduleIDReplanSweep = "construction:replanSweep"
	// replanSweepIntervalSecs is the replan-sweep cadence (5m) — the single tunable knob.
	replanSweepIntervalSecs = 5 * 60
)

// activityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities. A name with no entry falls back to the generated
// default (invokers.gen.go). Keyed by the generated registered activity name
// (<componentKey>.<opName>), including the 14 head-state Record*/read presets
// (recordOpts / readProjectOpts's VALUE forms — recordActivityOptions /
// readProjectActivityOptions, workflow.go).
func activityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
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
		"designSessionAccess.readProjectOnBranch":           readProjectActivityOptions(),
		"projectStateAccess.readProjectVersion":             readProjectActivityOptions(),
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
	optsHook := activityOptions()
	wf := newWorkflows(wfDeps{
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

// RegisterManagerWorker wires the constructionManager onto a Temporal Worker polling the
// construction task queue (constructionManager.md §6.1). It preserves the external call
// shape the composition root used before the generated-layer migration, asserting to the
// concrete *constructionManager the generated constructor returns and delegating to the
// generated RegisterWorker with the impl's WorkerManifest.
func RegisterManagerWorker(w worker.Worker, m ConstructionManager) {
	impl, ok := m.(*constructionManager)
	if !ok {
		panic("construction: RegisterManagerWorker requires a *constructionManager from NewConstructionManager")
	}
	RegisterWorker(w, impl.WorkerManifest())
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

// RegisterSchedules registers (idempotently) the TWO platform-wide construction
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
func (m *constructionManager) ListEpisodesForActivity(rc fwm.Context, projectID ProjectID, activityID string) ([]EpisodeRecordView, error) {
	ctx := rc.Context
	if projectID == "" {
		return nil, newError(fwm.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return nil, newError(fwm.ContractMisuse, "empty activityId")
	}
	records, err := m.episodes.ListEpisodes(fwra.Context{Context: ctx}, episode.EpisodeQuery{
		ProjectID: episode.ProjectID(projectID),
		TargetRef: &activityID,
	})
	if err != nil {
		return nil, mapRAError(err, "episodeAccess.ListEpisodes")
	}
	return episodeRecordViews(records), nil
}

// GetEpisodeTimeline returns one episode's full timeline: its ledger record plus
// the sequenced trace events mined from its run. NotFound if episodeID does not
// name a record on this project.
func (m *constructionManager) GetEpisodeTimeline(rc fwm.Context, projectID ProjectID, episodeID string) (EpisodeTimeline, error) {
	ctx := rc.Context
	if projectID == "" {
		return EpisodeTimeline{}, newError(fwm.ContractMisuse, "empty projectId")
	}
	if episodeID == "" {
		return EpisodeTimeline{}, newError(fwm.ContractMisuse, "empty episodeId")
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
		return EpisodeTimeline{}, newError(fwm.NotFound, fmt.Sprintf("episode %q not found", episodeID))
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

// findEpisodeRecord returns the record whose EpisodeID matches id, if any.
func findEpisodeRecord(records []episode.EpisodeRecord, id string) (episode.EpisodeRecord, bool) {
	for _, r := range records {
		if r.EpisodeID == id {
			return r, true
		}
	}
	return episode.EpisodeRecord{}, false
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

// episodeViewKind maps the episodeAccess RA's Kind onto this contract's own copy
// of the enum. Written as a TOTAL switch rather than a numeric cast so a future
// divergence between the two independently-versioned contracts is a compile-time
// conversation, not silent drift.
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
		return EpisodeKindConstruction
	}
}

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

// episodeTimelineEvents stitches the raw trace lines mined from episodeAccess into
// sequenced TimelineEvents: seq is 1-based and positional (the ledger's own
// ordering — trace files are append-only), eventType is lifted from each line's
// top-level "type" field (the same field the agentic-job trace miner reads off
// the CLI's stream-json protocol), and raw is carried through verbatim for the UI.
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

// mapRAError translates an episodeAccess error into the Manager façade error
// model. fwra.NotFound → NotFound; fwra.ContractMisuse → ContractMisuse;
// everything else → Infrastructure with the original retryability preserved.
// label identifies the actual failing op (the opaque Detail returned to the
// client; the full cause chain stays server-side on Cause). Mirrors
// systemDesignManager's mapRAError of the same name (I4: accepted triplication —
// the ratified DesignManager merge will not touch construction, so this copy is
// permanent).
func mapRAError(err error, label string) error {
	if err == nil {
		return nil
	}
	var raErr *fwra.Error
	if errors.As(err, &raErr) {
		switch raErr.Kind {
		case fwra.NotFound:
			return newError(fwm.NotFound, err.Error())
		case fwra.ContractMisuse:
			return newError(fwm.ContractMisuse, err.Error())
		case fwra.Unknown, fwra.Transient, fwra.RateLimited, fwra.Infrastructure,
			fwra.Auth, fwra.Conflict, fwra.QuotaExhausted, fwra.ContentPolicy:
			// "Everything else... → Infrastructure" per the doc comment above.
			mapped := fwm.Wrap(fwm.Infrastructure, err, label)
			mapped.Retryable = raErr.Retryable
			return mapped
		default:
			mapped := fwm.Wrap(fwm.Infrastructure, err, label)
			mapped.Retryable = raErr.Retryable
			return mapped
		}
	}
	// A non-fwra error still carries its cause for the server log while keeping
	// the client Detail opaque (label).
	return fwm.Wrap(fwm.Infrastructure, err, label)
}

// isEpisodeTraceNotFound reports whether err is episodeAccess.ReadTraceEvents's
// fwra.NotFound ("no trace file for episode ...") — the signal that a record
// with a stamped TracePath still has nothing to read (a gap's trace was never
// written, or a local trace file was pruned). Distinct from mapRAError's general
// NotFound handling: here it is NOT an error at all, it means "empty timeline".
func isEpisodeTraceNotFound(err error) bool {
	var raErr *fwra.Error
	return errors.As(err, &raErr) && raErr.Kind == fwra.NotFound
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
		var fe *fwm.Error
		if errors.As(err, &fe) && fe.Kind == fwm.NotFound {
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

// strPtrOrNil is the optional-string idiom of the generated contract: "" is omitted.
func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
