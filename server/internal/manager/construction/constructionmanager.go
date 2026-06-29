package construction

import (
	"errors"
	"fmt"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/durableexecution"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	workeraccess "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/worker"
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
// hand-written Temporal Workflows. The former exported consumer-mirror interfaces +
// the composition-root adapters are RETIRED; the Manager depends on the deps'
// PUBLISHED interfaces and adapts them internally.
type constructionManager struct {
	client client.Client

	projectState           projectstate.ProjectStateAccess
	artifact               artifact.ArtifactAccess
	worker                 workeraccess.WorkerAccess
	durable                durableexecution.DurableExecutionAccess
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
// stored for RegisterWorker (worker.go), which folds them into the Temporal Workflows.
func newConstructionManager(
	c client.Client,
	projectState projectstate.ProjectStateAccess,
	art artifact.ArtifactAccess,
	wrk workeraccess.WorkerAccess,
	durable durableexecution.DurableExecutionAccess,
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
		worker:                 wrk,
		durable:                durable,
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
// firing id = workflow id). SYNC from the scheduler's POV: returns once the pump
// tick is durably accepted + run (the child runs asynchronously).
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
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindPump, PumpInput{ProjectID: projectID})
	if err != nil {
		return PumpResult{}, mapStartError(err)
	}
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
	var in ReplanSweepInput
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
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindReplanSweep, in)
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
// PausePlan, then the Manager EXECUTES the cancels/records). SYNC from the
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
	sig := OperatorPauseSignal{ProjectID: projectID, Reason: reason}
	// Signal-with-start: the project-level supervision workflow resumes on its
	// awaitSignal and runs the pause branch; if not running, it is started
	// (constructionManager.md §6.2 — startOrSignalExecution semantics).
	opts := client.StartWorkflowOptions{
		ID:                       wfID,
		TaskQueue:                TaskQueue,
		WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
	}
	_, err := m.client.SignalWithStartWorkflow(ctx, wfID, SignalOperatorPauseRequested, sig,
		opts, ExecutionKindProjectSupervision, ProjectSupervisionInput{ProjectID: projectID})
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
	default:
		return newError(fwm.ContractMisuse, fmt.Sprintf("unknown override kind %d", int(override.Kind)))
	}

	wfID := constructActivityWorkflowID(projectID, activityID)
	sig := OperatorOverrideSignal{Override: override}
	if err := m.client.SignalWorkflow(ctx, wfID, "", SignalOperatorOverride, sig); err != nil {
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

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", QuerySessionState)
	if err != nil {
		return ConstructionSessionView{}, mapQueryError(err)
	}
	var view ConstructionSessionView
	if err := enc.Get(&view); err != nil {
		return ConstructionSessionView{}, newError(fwm.Infrastructure, err.Error())
	}
	return view, nil
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
