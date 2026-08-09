package construction

import (
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ===========================================================================
// PumpNextActivityWorkflow — op 2.1 entry (scheduler-triggered, 30s).
// ===========================================================================

// pumpInput is the start payload for PumpNextActivityWorkflow.
type pumpInput struct {
	ProjectID ProjectID
}

// pumpDispatch is THIS pump run's synchronous dispatch decision, surfaced to the
// façade via the queryPumpDispatch Query so ExecuteNextActivity can return this tick's
// outcome (dispatched X, or quiescent) WITHOUT blocking on the background self-cascade
// drain. Decided flips true the moment this run reaches its decision point — quiescent
// (pause / no-project / nothing eligible) or an eligible dispatch — which is BEFORE the
// blocking child.Get self-cascade below; until then the façade keeps polling.
type pumpDispatch struct {
	Decided    bool        `json:"decided"`
	Dispatched bool        `json:"dispatched"`
	ActivityID *ActivityID `json:"activityId,omitempty"`
}

// pumpPaceInterval is the short durable wait between cascade iterations (the pump's
// self-cascade pacing; Task 3) — a workflow.Sleep, NOT time.Sleep. Keeps the
// continue-as-new loop from busy-spinning while still draining the network promptly.
const pumpPaceInterval = 1 * time.Second

func (wf *workflows) PumpNextActivityWorkflow(ctx workflow.Context, in pumpInput) (PumpResult, error) {
	logger := workflow.GetLogger(ctx)

	// The per-run dispatch decision the façade reads synchronously. Registering the
	// Query handler and reading a captured local var emit NO workflow commands, so this
	// is a PURE ADDITION to the pump body — in-flight pump executions replay
	// deterministically against the unchanged command sequence (ExecuteChildWorkflow →
	// child.Get → Sleep → ContinueAsNew), so no GetVersion guard is required.
	var dispatch pumpDispatch
	if err := workflow.SetQueryHandler(ctx, queryPumpDispatch, func() (pumpDispatch, error) {
		return dispatch, nil
	}); err != nil {
		return PumpResult{}, err
	}

	// PAUSE GATE (Task 3): the cascade halts the moment a pause Signal is observed on
	// THIS pump execution. The pump listens on the SAME operatorPauseRequested signal
	// channel the project supervision workflow uses; a pause delivered to the cascading
	// pump is observed here (ReceiveAsync — non-blocking, replay-deterministic) and the
	// pump goes quiet WITHOUT ContinueAsNew. The resume path re-triggers the pump (a
	// fresh begin/schedule firing), which starts a new cascade. Checked BEFORE every
	// dispatch so a pause never races a half-dispatched activity. The signal survives
	// ContinueAsNew (same workflow id across the cascade), so a pause sent mid-cascade
	// is honored on the next iteration even if it arrives between ticks.
	pauseCh := workflow.GetSignalChannel(ctx, signalOperatorPauseRequested)
	var pauseSig operatorPauseSignal
	if pauseCh.ReceiveAsync(&pauseSig) {
		logger.Info("pump cascade paused by operator signal — going quiet without continue-as-new",
			"projectId", string(in.ProjectID), "reason", pauseSig.Reason)
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
	}

	proj, err := wf.readProject(ctx, in.ProjectID)
	if err != nil {
		if isReadNotFound(err) {
			// No project state yet — a normal quiet tick, not an error.
			dispatch = pumpDispatch{Decided: true, Dispatched: false}
			return PumpResult{Dispatched: false}, nil
		}
		return PumpResult{}, err
	}

	sel := wf.nextEligible(proj)
	switch sel.Verdict {
	case verdictBlocked:
		// LOUD, DURABLE, APP-VISIBLE (spec §4.3). The log line alone is the failure mode
		// being eliminated — a warning buried in a serve log is how this defect consumed
		// an entire benchmark run undetected — so the escalation is the HEAD-STATE
		// record: ActivityConstructionFailed is sticky via CoarsePhaseFor, so the
		// operator sees a red node carrying ComponentUnresolved and the reason. NOT a
		// returned workflow error: a failed Temporal execution is invisible in the
		// console. Recording the terminal also takes the activity out of NotStarted, so
		// the next scheduled tick considers the rest of the network instead of
		// re-blocking on this one. An empty credential is correct for the local store;
		// the git adapter mints just-in-time (same as the supervision pause path).
		logger.Error("construction pump: activity cannot be dispatched",
			"projectId", string(in.ProjectID),
			"activityId", sel.BlockedActivityID,
			"reason", sel.BlockedReason)
		if _, ferr := wf.applyRecovering(ctx, in.ProjectID, proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
			return wf.Acts.ConstructionTransitionRecordActivityFailed(
				ctx, projectstate.ProjectID(in.ProjectID), expected,
				sel.BlockedActivityID, projectstate.ComponentUnresolved, sel.BlockedReason,
				railCredEnvelope{}.toProjectState())
		}); ferr != nil {
			return PumpResult{}, ferr
		}
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
	case verdictQuiescent:
		logger.Info("no eligible activity — cascade quiescent", "projectId", string(in.ProjectID))
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
	case verdictDispatch:
		// fall through to the dispatch below
	}
	activity := sel.Activity

	// Eligible ⇒ start a per-activity child workflow (idempotent on its id; a
	// redundant tick collapses to the running child). PARENT_CLOSE_POLICY ABANDON:
	// the construction activity is its own durable execution, independent of this
	// pump tick's continue-as-new chain.
	childID := constructActivityWorkflowID(in.ProjectID, ActivityID(activity.ActivityID))
	cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:        childID,
		ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON,
	})
	child := workflow.ExecuteChildWorkflow(cctx, executionKindConstructActivity, constructActivityInput{
		ProjectID:  in.ProjectID,
		ActivityID: ActivityID(activity.ActivityID),
		Activity:   activity,
	})
	// Record the dispatch decision NOW — after the child-start command is queued but
	// BEFORE the blocking child.Get — so the façade's synchronous ExecuteNextActivity
	// returns {Dispatched:true, ActivityID} for THIS tick while the cascade drains on in
	// the background. The child-start command commits with this same workflow task, so a
	// caller reading queryPumpDispatch after this point observes both the decision and a
	// started (GetSessionState-observable) child.
	dispatchedActivity := ActivityID(activity.ActivityID)
	dispatch = pumpDispatch{Decided: true, Dispatched: true, ActivityID: &dispatchedActivity}
	// SELF-CASCADE (Task 3): wait for the child to COMPLETE (not just start) so the
	// activity's RecordActivityCompleted has landed in head-state before we pick the
	// next eligible activity — otherwise nextEligible would re-select the same
	// still-Running activity. child.Get blocks on the child's terminal result.
	if err := child.Get(ctx, nil); err != nil {
		return PumpResult{}, err
	}

	// Pace the cascade with a short durable wait (workflow.Sleep — replay-safe; NOT
	// time.Sleep), then ContinueAsNew to pick the next eligible activity. ContinueAsNew
	// carries ONLY pumpInput (no accumulated state ⇒ unbounded history is avoided and
	// determinism is trivial). The conflict/quiet-tick semantics keep the prod 30s
	// schedule compatible: a schedule re-fire onto a cascading pump uses the existing
	// USE_EXISTING conflict policy (constructionmanager.go) and the cascade's own
	// drain-to-quiet ends it.
	if err := workflow.Sleep(ctx, pumpPaceInterval); err != nil {
		return PumpResult{}, err
	}
	return PumpResult{}, workflow.NewContinueAsNewError(ctx, executionKindPump, pumpInput{ProjectID: in.ProjectID})
}

// nextEligible resolves the next selection via the injected helper. With no helper
// wired it is a quiet tick.
func (wf *workflows) nextEligible(proj projectstate.Project) pumpSelection {
	if wf.NextEligibleActivity == nil {
		return pumpSelection{Verdict: verdictQuiescent}
	}
	return wf.NextEligibleActivity(proj)
}

// Shared workflow-context helper (used by 3 workflows); lives in its first caller's file per the file-layout standard.
// readProject reads the whole-aggregate head-state through the GENERATED
// designSessionAccess.readProjectOnBranch invoker with branch "" — the RA-side
// empty-branch fallback always reads main (pinned by
// TestDesignSessionAccess_ReadProjectOnBranch_EmptyBranchAlwaysBase,
// projectstate/designsession_test.go). This replaced the last CUSTOM Activity
// (ReadProjectActivity) once the shared projectstate.ProjectEnvelope grew the three
// construction-fidelity sections the pump reads (ActivityConstruction /
// ServiceContracts / ReviewPolicy — B8 follow-up, envelope.go); Decode restores the
// committed Network/ActivityList slots concretely typed, so nextEligibleActivity's
// committed-slot guards and type assertions are served identically to the former
// local codec. Decode is pure JSON reconstruction over the history-recorded activity
// result — deterministic, replay-safe in-workflow.
func (wf *workflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
	env, err := wf.Acts.DesignSessionReadProjectOnBranch(ctx, projectstate.ProjectID(projectID), "")
	if err != nil {
		return projectstate.Project{}, err
	}
	return env.Decode()
}
