package construction

import (
	"context"
	"errors"
	"fmt"
	"strings"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	fwmanager "github.com/davidmarne/archistrator-platform/framework-go/manager"
)

// Manager is the constructionManager façade. It exposes the five public use-case
// ops (constructionManager.md §2) and OWNS Temporal. The Temporal-backed ops:
//   - ExecuteNextActivity — Workflow (entry; scheduler-triggered pump)
//   - RunReplanSweep      — Workflow (entry; scheduler-triggered variance sweep)
//   - PauseProject        — Signal (operatorPauseRequested)
//   - OverrideActivity    — Signal (operatorOverride, to the per-activity child)
//   - GetSessionState     — Query (sessionState, read-only)
//
// The Manager holds only a Temporal client; the façade pre-condition checks
// (non-empty ids, non-empty reason, known OverrideKind) are enforced here before
// any Temporal call (constructionManager.md §2/§3.5). The downstream Engines/RA
// are held on the Workflows struct (workflow.go), reached only from workflow code.
type Manager struct {
	client client.Client
}

// NewManager constructs a Manager over an existing Temporal client.
func NewManager(c client.Client) *Manager {
	return &Manager{client: c}
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
func (m *Manager) ExecuteNextActivity(ctx context.Context, projectID ProjectID, tickID string) (PumpResult, error) {
	if projectID == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if tickID == "" {
		return PumpResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
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
		return PumpResult{}, newError(fwmanager.Infrastructure, err.Error())
	}
	return result, nil
}

// RunReplanSweep — op 2.2. Temporal Workflow (entry; scheduler-triggered, short).
// Reads in-flight construction state, flags over-threshold variances, surfaces
// them to the operator dashboard — it does NOT auto-replan. An empty result is a
// normal quiet sweep. A nil projectID sweeps all in-flight projects (workflow id
// :all:replanSweep:{tickId}).
func (m *Manager) RunReplanSweep(ctx context.Context, projectID *ProjectID, tickID string) (ReplanSweepResult, error) {
	if tickID == "" {
		return ReplanSweepResult{}, newError(fwmanager.ContractMisuse, "empty tickId")
	}
	var in ReplanSweepInput
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
	we, err := m.client.ExecuteWorkflow(ctx, opts, ExecutionKindReplanSweep, in)
	if err != nil {
		return ReplanSweepResult{}, mapStartError(err)
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
// PausePlan, then the Manager EXECUTES the cancels/records). SYNC from the
// operator's POV: returns once the signal is durably enqueued.
func (m *Manager) PauseProject(ctx context.Context, projectID ProjectID, reason string) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if reason == "" {
		return newError(fwmanager.ContractMisuse, "empty pause reason")
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
func (m *Manager) OverrideActivity(ctx context.Context, projectID ProjectID, activityID ActivityID, override ActivityOverride) error {
	if projectID == "" {
		return newError(fwmanager.ContractMisuse, "empty projectId")
	}
	if activityID == "" {
		return newError(fwmanager.ContractMisuse, "empty activityId")
	}
	switch override.Kind {
	case OverrideTakeover, OverrideRetry, OverrideSkip, OverrideReassign:
		// ok
	default:
		return newError(fwmanager.ContractMisuse, fmt.Sprintf("unknown override kind %d", int(override.Kind)))
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
func (m *Manager) GetSessionState(ctx context.Context, projectID ProjectID, activityID *ActivityID) (ConstructionSessionView, error) {
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

	enc, err := m.client.QueryWorkflow(ctx, wfID, "", QuerySessionState)
	if err != nil {
		return ConstructionSessionView{}, mapQueryError(err)
	}
	var view ConstructionSessionView
	if err := enc.Get(&view); err != nil {
		return ConstructionSessionView{}, newError(fwmanager.Infrastructure, err.Error())
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
	return newError(fwmanager.Infrastructure, err.Error())
}

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
	return newError(fwmanager.Infrastructure, err.Error())
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
