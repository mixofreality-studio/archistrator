// Package construction is the constructionManager component of the aiarch server's
// Manager layer — the use-case façade that drives a project through Phase 3 of
// The Method (Construction), per the senior-frozen contract
// designs/aiarch/implementation/contracts/constructionManager.md (C-MCN).
//
// This is the MANAGER layer. It OWNS Temporal: its public ops map to Temporal
// primitives (Workflow / Signal / Query), it registers the nextActivity (30s) and
// replanSweep (5m) Schedules at startup, defines one Activity per ResourceAccess
// call, owns the Signal/Query handlers and the in-workflow primitives
// (awaitSignal / startTimer / executeChild), and derives the idempotency key
// "${workflowId}:${activityId}" passed down to each RA verb. Temporal lives ONLY
// in this component; the downstream Engines (handOffEngine, interventionEngine,
// reviewEngine — pure, in-workflow, by value) and ResourceAccess ports
// (projectStateAccess, artifactAccess, workerAccess, constructionPipelineAccess,
// durableExecutionAccess) import no Temporal.
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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
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
	handOff                handoff.HandOffEngine
	intervention           intervention.InterventionEngine
	review                 review.ReviewEngine
	pipeline               constructionpipeline.ConstructionPipelineAccess
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

	// designSession (B6) is the generated designSessionAccess dep. Since the B8
	// follow-up it is CONSUMED by the workflows: the pump's whole-aggregate read rides
	// the generated designSessionAccess.readProjectOnBranch invoker with branch ""
	// (main) — the shared projectstate.ProjectEnvelope was extended with the
	// construction-fidelity sections (ActivityConstruction / ServiceContracts /
	// ReviewPolicy, envelope.go) that construction's former local codec carried, which
	// is what retired the last custom Activity (ReadProjectActivity).
	designSession projectstate.DesignSessionAccess
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
	handOff handoff.HandOffEngine,
	interventionEng intervention.InterventionEngine,
	reviewEng review.ReviewEngine,
	pipeline constructionpipeline.ConstructionPipelineAccess,
	rail sourcecontrol.SourceControlAccess,
	constructionTransition projectstate.ConstructionTransitionAccess,
	gitActivityStatus projectstate.GitActivityStatusAccess,
	designSession projectstate.DesignSessionAccess,
	escalationWaitTimeout time.Duration,
	interventionMode string,
	repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool),
) *constructionManager {
	return &constructionManager{
		client:                 c,
		projectState:           projectState,
		artifact:               art,
		handOff:                handOff,
		intervention:           interventionEng,
		review:                 reviewEng,
		pipeline:               pipeline,
		rail:                   rail,
		constructionTransition: constructionTransition,
		gitActivityStatus:      gitActivityStatus,
		designSession:          designSession,
		escalationWaitTimeout:  escalationWaitTimeout,
		interventionMode:       interventionMode,
		repo:                   repo,
	}
}

// ExecuteNextActivity — op 2.1. Temporal Workflow (entry; scheduler-triggered).
// Starts the per-tick PumpNextActivityWorkflow on the construction queue, id
// {projectId}:nextActivity:{tickId}. The pump reads head-state, and on an eligible
// activity executes a per-activity child workflow {projectId}:{activityId}. No
// eligible activity ⇒ PumpResult{Dispatched:false} (a normal quiet tick).
//
// tickID is the scheduler firing id (Temporal-native firing idempotency: schedule
// firing id = workflow id). SYNC from the scheduler's POV: returns THIS tick's
// dispatch outcome (PumpResult{Dispatched:true, ActivityID} for the activity dispatched
// this tick, or {Dispatched:false} when quiescent) as soon as the pump has decided —
// it does NOT block until the per-activity child (or the pump's background self-cascade
// over the dependency frontier) drains. The dispatch decision is read off the pump via
// the queryPumpDispatch Query so a scheduler-style caller gets a prompt, per-tick answer
// while the cascade continues durably in the background.
func (m *constructionManager) ExecuteNextActivity(rc fwm.Context, projectID ProjectID, tickID string) (PumpResult, error) {
	ctx := rc.Context
	if projectID == "" {
		return PumpResult{}, newError(fwm.ContractMisuse, "empty projectId")
	}
	if tickID == "" {
		return PumpResult{}, newError(fwm.ContractMisuse, "empty tickId")
	}

	wfID := pumpWorkflowID(projectID, tickID)
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	we, err := m.client.ExecuteWorkflow(ctx, opts, executionKindPump, pumpInput{ProjectID: projectID})
	if err != nil {
		return PumpResult{}, mapStartError(err)
	}
	return m.awaitDispatchDecision(ctx, we, wfID)
}

// pumpDispatchPollInterval paces the façade's poll of the pump's synchronous
// dispatch-decision Query. The pump sets its decision after reading head-state and
// picking (or finding no) eligible activity — a couple of activity round-trips — so
// the poll converges within a few iterations.
const pumpDispatchPollInterval = 25 * time.Millisecond

// pumpDispatchWaitBudget bounds the poll so a run that fails BEFORE reaching its
// decision point (an infra fault in the head-state read) cannot spin forever; on
// expiry the terminal workflow result/error is surfaced instead (a failed run returns
// promptly — there is no cascade to await on that path).
const pumpDispatchWaitBudget = 30 * time.Second

// awaitDispatchDecision returns THIS tick's dispatch outcome as soon as the pump run
// has decided, WITHOUT waiting for the background self-cascade to drain the dependency
// frontier. It polls queryPumpDispatch against the exact run ExecuteWorkflow started
// (pinned RunID) so the answer stays this tick's FIRST decision even after the pump
// ContinueAsNews into the next cascade iteration.
func (m *constructionManager) awaitDispatchDecision(ctx context.Context, we client.WorkflowRun, wfID string) (PumpResult, error) {
	runID := we.GetRunID()
	deadline := time.Now().Add(pumpDispatchWaitBudget)
	for {
		enc, qerr := m.client.QueryWorkflow(ctx, wfID, runID, queryPumpDispatch)
		if qerr != nil {
			// The run cannot serve the Query (gone / not-found). Surface the terminal
			// result/error — prompt for a failed or already-quiescent run.
			return m.terminalPumpResult(ctx, we)
		}
		var d pumpDispatch
		if derr := enc.Get(&d); derr != nil {
			return PumpResult{}, newError(fwm.Infrastructure, derr.Error())
		}
		if d.Decided {
			return PumpResult{Dispatched: d.Dispatched, ActivityID: d.ActivityID}, nil
		}
		if time.Now().After(deadline) {
			return m.terminalPumpResult(ctx, we)
		}
		select {
		case <-ctx.Done():
			return PumpResult{}, newError(fwm.Infrastructure, ctx.Err().Error())
		case <-time.After(pumpDispatchPollInterval):
		}
	}
}

// terminalPumpResult is the safety-net fallback: it awaits the pump's terminal result
// (used only when the dispatch decision never surfaced — a failed run or a run that
// finished before it could be polled).
func (m *constructionManager) terminalPumpResult(ctx context.Context, we client.WorkflowRun) (PumpResult, error) {
	var result PumpResult
	if err := we.Get(ctx, &result); err != nil {
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
// pausePlan, then the Manager EXECUTES the cancels/records). SYNC from the
// operator's POV: returns once the signal is durably enqueued.
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

// OverrideActivity — op 2.4. Temporal Signal (operatorOverride) to the per-activity
// child workflow {projectId}:{activityId}. The operator's steer is fed through the
// SAME decide→execute machinery as the automatic variance path. SYNC: returns once
// the signal is durably enqueued.
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

// SubmitPhaseDecision — op 2.6. Temporal Signal (phaseDecision) to the
// per-activity child workflow {projectId}:{activityId}. Delivers the operator's
// phase-gated approve/send-back decision (and optional feedback) through the same
// signal machinery as OverrideActivity. SYNC: returns once the signal is durably
// enqueued. SendBack requires non-empty feedback notes.
func (m *constructionManager) SubmitPhaseDecision(rc fwm.Context, projectID ProjectID, activityID ActivityID, phase string, decision PhaseDecision, feedback *ReviewFeedback) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwm.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwm.ContractMisuse, "empty activityId")
	}
	if decision == PhaseSendBack && (feedback == nil || feedback.Notes == "") {
		return newError(fwm.ContractMisuse, "SendBack requires non-empty feedback notes")
	}

	wfID := constructActivityWorkflowID(projectID, activityID)
	sig := phaseDecisionSignal{Phase: phase, Decision: decision, Feedback: feedback}
	if err := m.client.SignalWorkflow(ctx, wfID, "", signalPhaseDecision, sig); err != nil {
		return mapSignalError(err)
	}
	return nil
}

// SetReviewPolicy — op 2.8 (local-merge-and-policy Commit 2). Sets the project's
// review-policy PRESET (the Task-7 sophistication dial: vibes / checkpoints /
// full) while PRESERVING the committed GatedPhasesByType map (UpdateReviewPolicy's
// surface — the two ops write disjoint halves of the same ReviewPolicy).
//
// The preset is validated HERE, at the write path: EffectiveGate's read path
// deliberately treats an unrecognized preset as the legacy explicit-map fallback,
// and with an empty map that gates NOTHING — the documented fail-open corner. A
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
	proj, err := m.constructionTransition.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), projectstate.RepoCredential{})
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
	proj, err := m.constructionTransition.ReadProject(fwra.Context{Context: ctx}, projectstate.ProjectID(projectID), projectstate.RepoCredential{})
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

// pumpWorkflowID derives {projectId}:nextActivity:{tickId}.
func pumpWorkflowID(projectID ProjectID, tickID string) string {
	return fmt.Sprintf("%s:nextActivity:%s", projectID, tickID)
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
// dependencies' PUBLISHED interfaces directly. The three Engines (handOff /
// intervention / review) are typed as their PUBLISHED contract interfaces DIRECTLY on
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
//   - the three Engines (handoff.HandOffEngine / intervention.InterventionEngine /
//     review.ReviewEngine) are PURE, deterministic, called DIRECTLY in-workflow (no
//     Activity wrapper — replay-safe) with fweng.Context{Context: context.Background()}
//     supplied inline at each call site (workflow.go / signals.go);
//   - the ResourceAccess ports are I/O and reached EXCLUSIVELY through the generated
//     invoker surface (Acts — invokers.gen.go/activities.gen.go).

// constructionActivity is the by-value activity snapshot the Manager's own workflow
// vocabulary uses broadly (eligibility.go, gitforward.go, dispatch) — CRLabel/IsRevert
// are the git-forward per-activity facts threaded into the PR open + the head-state
// mirror, and Phases is the resolved per-activity phase profile; none of these ride
// the handOffEngine call. Kind is typed DIRECTLY as the published handoff.ActivityKind
// (no converter — the ordinal sets are identical). At the one handOffEngine call site
// (workflow.go), handoffActivityFromConstruction (constructactivity.go) narrows this
// broader struct onto the Engine's published handoff.ConstructionActivity — a REAL
// (if now-trivial) projection, since constructionActivity carries strictly MORE fields
// than the Engine needs, not an identity mirror to delete.
type constructionActivity struct {
	ActivityID   string
	Kind         handoff.ActivityKind
	ComponentID  string
	Layer        string
	EstimateDays float64
	CRLabel      string
	IsRevert     bool
	Phases       []projectstate.ActivityMethodPhase
}

// activityTypeName returns the canonical activity-type wire name
// ("service"/"frontend"/"testing") derived from the activity id. These are the exact
// keys the ReviewPolicy's GatedPhasesByType map is keyed by (and the keys the webApp
// PolicyPanel must emit) — the gate consults RequiresHuman(activityTypeName(), phase).
func (a constructionActivity) activityTypeName() string {
	return projectstate.DeriveType(a.ActivityID).String()
}

// ===========================================================================
// constructionPipeline value vocabulary — the Manager's infrastructure-neutral
// dispatch spec / handle / observation. The pipeline ops are GENERATED and reached
// through the generated invoker surface (genInvokers.Pipeline*); these neutral types
// feed the workflow-side composition/mapping helpers (workflow.go) that bridge to the
// contract constructionpipeline.PipelineSpec / PipelineHandle / PipelineObservation.
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

// nextEligibleActivity resolves the next eligible construction activity for a project
// from its head-state. An activity is eligible iff it is NotStarted and every dep is
// Done. Iteration is ActivityList declaration order with a name tie-break.
func nextEligibleActivity(proj projectstate.Project) (constructionActivity, bool) {
	network, activityList, ok := committedPlanInputs(proj)
	if !ok {
		return constructionActivity{}, false
	}

	itemByName := make(map[string]projectstate.ActivityItem, len(activityList.Activities))
	for _, item := range activityList.Activities {
		itemByName[item.Name] = item
	}

	depsByActivity := make(map[string][]string, len(network.Dependencies))
	for _, dep := range network.Dependencies {
		depsByActivity[dep.Activity] = dep.DependsOn
	}

	type candidate struct {
		declIdx  int
		activity string
	}
	var candidates []candidate
	for i, item := range activityList.Activities {
		name := item.Name
		if !isActivityNotStarted(name, proj.ActivityConstruction) {
			continue
		}
		if !allDepsDone(depsByActivity[name], proj.ActivityConstruction) {
			continue
		}
		candidates = append(candidates, candidate{declIdx: i, activity: name})
	}
	if len(candidates) == 0 {
		return constructionActivity{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].declIdx != candidates[j].declIdx {
			return candidates[i].declIdx < candidates[j].declIdx
		}
		return candidates[i].activity < candidates[j].activity
	})

	chosen := candidates[0].activity
	item := itemByName[chosen]
	var produced []projectstate.ProducedArtifact
	if proj.ActivityConstruction != nil {
		produced = proj.ActivityConstruction[chosen].Produced
	}
	component, ok := resolveComponentID(item.Title, produced, proj.ServiceContracts)
	if !ok {
		slog.Warn("construction pump: no service-contract key resolves for activity — skipping dispatch",
			"activityId", chosen, "title", item.Title)
		return constructionActivity{}, false
	}
	return hydrateConstructionActivity(chosen, item, component), true
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

// resolveComponentID maps an activity to its service-contract component KEY.
func resolveComponentID(title string, produced []projectstate.ProducedArtifact, contracts map[string]projectstate.ServiceContract) (string, bool) {
	for _, art := range produced {
		if art.Kind != "service-contract" {
			continue
		}
		if key, ok := matchContractKey(art.Title, contracts); ok {
			return key, true
		}
	}

	base := title
	if i := strings.IndexByte(base, '('); i >= 0 {
		base = base[:i]
	}
	n := normalizeIdent(base)
	best, bestLen := "", 0
	for comp := range contracts {
		cn := normalizeIdent(comp)
		if cn != "" && len(cn) > bestLen && strings.Contains(n, cn) {
			best, bestLen = comp, len(cn)
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// matchContractKey resolves a produced service-contract artifact title to a real key.
func matchContractKey(title string, contracts map[string]projectstate.ServiceContract) (string, bool) {
	n := normalizeIdent(title)
	if n == "" {
		return "", false
	}
	best, bestLen := "", 0
	for comp := range contracts {
		cn := normalizeIdent(comp)
		if cn == "" {
			continue
		}
		if cn == n {
			return comp, true
		}
		if len(cn) > bestLen && strings.Contains(n, cn) {
			best, bestLen = comp, len(cn)
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// normalizeIdent lowercases s and keeps only [a-z0-9].
func normalizeIdent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isActivityNotStarted reports whether the activity is in the NotStarted phase.
func isActivityNotStarted(activityID string, status map[string]projectstate.ActivityConstructionStatus) bool {
	if status == nil {
		return true
	}
	s, exists := status[activityID]
	if !exists {
		return true
	}
	return s.Phase == projectstate.ActivityConstructionNotStarted
}

// allDepsDone reports whether every dependency has Phase == Done.
func allDepsDone(deps []string, status map[string]projectstate.ActivityConstructionStatus) bool {
	for _, dep := range deps {
		if status == nil {
			return false
		}
		s, exists := status[dep]
		if !exists || s.Phase != projectstate.ActivityConstructionDone {
			return false
		}
	}
	return true
}

// hydrateConstructionActivity populates a constructionActivity from the activity id +
// its ActivityList item. Coding=true → Construction; Coding=false → Noncoding.
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, componentID string) constructionActivity {
	kind := handoff.ActivityKindNoncoding
	if item.Coding {
		kind = handoff.ActivityKindConstruction
	}
	typ := projectstate.DeriveType(activityID)
	variant := projectstate.DeriveVariant(activityID)
	return constructionActivity{
		ActivityID:   activityID,
		Kind:         kind,
		ComponentID:  componentID,
		EstimateDays: item.EffortDays,
		Phases:       projectstate.ProfileFor(typ, variant).PhaseIDs(),
	}
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
	HandOff      handoff.HandOffEngine
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

	// RailEnabled reports whether the PR rail dep is wired (impl.rail != nil). It gates
	// the PR-rail lifecycle (gitEnabled) alongside GitStatus + Repo.
	RailEnabled bool

	// Repo resolves the per-project RepoRef the rail verbs address. nil ⇒ the
	// PR-rail lifecycle is dormant (no repo to open branches/PRs in).
	Repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// NextEligibleActivity resolves the next eligible construction activity for a
	// project from its head-state (the Manager's own pure selection).
	NextEligibleActivity func(proj projectstate.Project) (constructionActivity, bool)

	// HandOffPolicy / InterventionPolicy are the project's committed policy snapshots
	// the Manager feeds the Engines by value, typed DIRECTLY as each Engine's own
	// published input. InterventionPolicy is resolved ONCE from the composition root's
	// raw interventionMode config via constructionInterventionPolicy (adapters.go,
	// WorkerManifest() — workermanifest.go) — the SAME fixed value every DecideOnVariance
	// / ApplyPausePolicy call fed under the retired per-call adapter conversion.
	HandOffPolicy      handoff.HandOffPolicy
	InterventionPolicy intervention.InterventionPolicy

	// EscalationWaitTimeout bounds how long an escalated/architectOnly activity waits
	// for an operator override before it terminally FAILS the activity. 0 == wait-forever.
	EscalationWaitTimeout time.Duration
}

// workflows is the single constructionManager component struct — the workflow receiver
// (it no longer hosts any Activity methods; every RA op is reached through the
// generated invoker surface, Acts).
type workflows struct {
	HandOff      handoff.HandOffEngine
	Intervention intervention.InterventionEngine
	Review       review.ReviewEngine

	GitStatus projectstate.GitActivityStatusAccess

	Acts genInvokers

	RailEnabled bool
	Repo        func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	NextEligibleActivity  func(proj projectstate.Project) (constructionActivity, bool)
	HandOffPolicy         handoff.HandOffPolicy
	InterventionPolicy    intervention.InterventionPolicy
	EscalationWaitTimeout time.Duration
}

// newWorkflows builds the workflows receiver from the injected seams.
func newWorkflows(d wfDeps) *workflows {
	return &workflows{
		HandOff:               d.HandOff,
		Intervention:          d.Intervention,
		Review:                d.Review,
		GitStatus:             d.GitStatus,
		Acts:                  d.Acts,
		RailEnabled:           d.RailEnabled,
		Repo:                  d.Repo,
		NextEligibleActivity:  d.NextEligibleActivity,
		HandOffPolicy:         d.HandOffPolicy,
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
	variance      *FlaggedVariance

	// completedPhases is the LIVE in-memory skip-guard the phase loop consults so an
	// already-completed phase is never re-dispatched or re-gated. It is SEEDED at
	// workflow start from the start-snapshot activity's PhaseCompletion slice and
	// MARKED unconditionally on EVERY phase completion (Approve / no-gate / inert) —
	// independent of gitOn. This is what stops the outer variance-retry loop (which
	// re-walks phases from index 0) from re-gating an already-approved phase across a
	// non-git execution where no head-state completion record exists to re-read.
	completedPhases map[projectstate.ActivityMethodPhase]bool

	// redraftExhausted records that a gated phase burned its human-paced SendBack
	// redraft budget. It does NOT fail the activity or re-enter the variance loop — the
	// gate keeps awaiting the human; the flag surfaces that redrafting is spent.
	redraftExhausted bool

	// reviewContracts is the per-execution set of contract identifiers captured from
	// the start-snapshot project (B5) and fed to reviewEngine.ProposeReviews so the
	// gate's reviewer set is display-populated without re-reading mid-loop.
	reviewContracts []string

	// floorTouched is the Task 7 non-overridable-floor snapshot: whether the
	// activity's committed contract (start-snapshot, B5-style — never re-read
	// mid-loop) touches deploy/spend/schema (projectstate.ContractTouchesReviewFloor).
	// Consulted by runPhaseGate via ReviewPolicy.EffectiveGate to force a human gate
	// at MethodPhaseConstruction regardless of preset, including "vibes".
	floorTouched bool

	// mergeCompleted is the LIVE in-memory skip-guard for the local merge step
	// (local-merge-and-policy Commit 1, same discipline as completedPhases):
	// marked once the merge job landed, so a variance retry of a LATER finalize
	// fault does not re-dispatch a merge whose activity branch is already
	// merged and deleted (which would honestly — and wrongly — fail).
	mergeCompleted bool
}

func (s *constructState) view() (ConstructionSessionView, error) {
	aid := s.activityID
	return ConstructionSessionView{
		ProjectID:     s.projectID,
		ActivityID:    &aid,
		Stage:         s.stage,
		PipelinePhase: s.pipelinePhase,
		ReviewSet:     s.reviewSet,
		Variance:      s.variance,
	}, nil
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
// The three Engines (handOff / intervention / review) are called DIRECTLY in-workflow
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
	// executionKindPump is the per-tick PumpNextActivityWorkflow (the 30s pump).
	executionKindPump = "constructionPumpNextActivity"
	// executionKindConstructActivity is the per-activity child workflow.
	executionKindConstructActivity = "constructionConstructActivity"
	// executionKindReplanSweep is the per-tick ReplanSweepWorkflow (the 5m sweep).
	executionKindReplanSweep = "constructionReplanSweep"
	// executionKindProjectSupervision is the long-lived project-level supervision
	// workflow that hosts the operator-pause branch + project-level session Query.
	executionKindProjectSupervision = "constructionProjectSupervision"
)

// activityOptions returns the option-preset hook the generated invokers consult for the
// contract-backed RA Activities. A name with no entry falls back to the generated
// default (invokers.gen.go). Keyed by the generated registered activity name
// (<componentKey>.<opName>), including the 14 head-state Record*/read presets
// (recordOpts / readProjectOpts's VALUE forms — recordActivityOptions /
// readProjectActivityOptions, workflow.go).
func activityOptions() func(activityName string) (workflow.ActivityOptions, bool) {
	presets := map[string]workflow.ActivityOptions{
		"constructionPipelineAccess.submitConstructionPipeline":  submitPipelineActivityOptions(),
		"constructionPipelineAccess.observeConstructionPipeline": observePipelineActivityOptions(),
		"constructionPipelineAccess.cancelConstructionPipeline":  observePipelineActivityOptions(),
		"sourceControlAccess.getInstallationToken":               mintCredActivityOptions(),
		"sourceControlAccess.openBranch":                         railActivityOptions(),
		"sourceControlAccess.openPullRequest":                    railActivityOptions(),
		"sourceControlAccess.getPullRequestStatus":               railActivityOptions(),
		"sourceControlAccess.postReview":                         railActivityOptions(),
		"sourceControlAccess.mergePullRequest":                   railActivityOptions(),
		// B8 (+ follow-up): re-keyed from the retired custom-Activity call sites onto
		// their generated registered names, preserving the identical timeout/retry scope.
		// designSessionAccess.readProjectOnBranch is the whole-aggregate read the pump
		// runs (branch "" ⇒ main) — the former ReadProjectActivity preset.
		"designSessionAccess.readProjectOnBranch":            readProjectActivityOptions(),
		"projectStateAccess.readProjectVersion":              readProjectActivityOptions(),
		"constructionTransitionAccess.recordChangeReviewed":  recordActivityOptions(),
		"constructionTransitionAccess.recordActivityExited":  recordActivityOptions(),
		"constructionTransitionAccess.recordActivityFailed":  recordActivityOptions(),
		"constructionTransitionAccess.recordOperatorPaused":  recordActivityOptions(),
		"constructionTransitionAccess.recordPhaseStarted":    recordActivityOptions(),
		"constructionTransitionAccess.recordPhaseCompleted":  recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityBranchOpened": recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityCIObserved":   recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityArchApproved": recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityMerged":       recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityStarted":      recordActivityOptions(),
		"gitActivityStatusAccess.recordActivityCompleted":    recordActivityOptions(),
	}
	return func(name string) (workflow.ActivityOptions, bool) {
		o, ok := presets[name]
		return o, ok
	}
}

// WorkerManifest assembles the genWorkerManifest RegisterWorker (worker.gen.go) consumes:
// the four workflow bodies under their registered names, the per-activity option-preset
// hook, and the genActivities threaded from the impl's stored published deps.
func (m *constructionManager) WorkerManifest() genWorkerManifest {
	optsHook := activityOptions()
	wf := newWorkflows(wfDeps{
		HandOff:      m.handOff,
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
		// branch→PR rail; nil keeps the central-repo fallback + dormant rail.
		RailEnabled:           m.rail != nil,
		Repo:                  m.repo,
		NextEligibleActivity:  nextEligibleActivity,
		HandOffPolicy:         handoff.HandOffPolicy{},
		InterventionPolicy:    constructionInterventionPolicy(m.interventionMode),
		EscalationWaitTimeout: m.escalationWaitTimeout,
	})

	return genWorkerManifest{
		Workflows: []genRegisteredWorkflow{
			{Name: executionKindPump, Fn: wf.PumpNextActivityWorkflow},
			{Name: executionKindConstructActivity, Fn: wf.ConstructActivityWorkflow},
			{Name: executionKindReplanSweep, Fn: wf.ReplanSweepWorkflow},
			{Name: executionKindProjectSupervision, Fn: wf.ProjectSupervisionWorkflow},
		},
		ActivityOptions: optsHook,
		Activities: genActivities{
			ProjectState:           m.projectState,
			Artifact:               m.artifact,
			Pipeline:               m.pipeline,
			Rail:                   m.rail,
			ConstructionTransition: m.constructionTransition,
			GitStatus:              m.gitActivityStatus,
			DesignSession:          m.designSession,
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
