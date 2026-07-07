package construction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

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
	escalationWaitTimeout time.Duration,
	interventionMode string,
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
		escalationWaitTimeout:  escalationWaitTimeout,
		interventionMode:       interventionMode,
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

// UpdateReviewPolicy — op 2.7. Persists the per-project ReviewPolicy.
// Converts the input's GatedPhasesByType (map[string][]string of ad-hoc or canonical
// gate ids) via projectstate.ReviewPolicyFromGateIDs to a typed ReviewPolicy, reads the
// current project version, then calls RecordReviewPolicy on the constructionTransition RA.
func (m *constructionManager) UpdateReviewPolicy(rc fwm.Context, projectID ProjectID, input ReviewPolicyInput) error {
	ctx := rc.Context
	if projectID == "" {
		return newError(fwm.ContractMisuse, "empty projectId")
	}
	proj, err := m.constructionTransition.ReadProject(ctx, projectstate.ProjectID(projectID), projectstate.RepoCredential{})
	if err != nil {
		return newError(fwm.Infrastructure, err.Error())
	}
	policy := projectstate.ReviewPolicyFromGateIDs(input.GatedPhasesByType)
	if _, err := m.constructionTransition.RecordReviewPolicy(ctx, projectstate.ProjectID(projectID), proj.Version, policy, projectstate.RepoCredential{}, fwra.IdempotencyKey(uuid.NewString())); err != nil {
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
// does not exist (mirrors systemdesign's matcher).
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errNotFoundSentinel) ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "NotFound")
}

var errNotFoundSentinel = errors.New("not found")
