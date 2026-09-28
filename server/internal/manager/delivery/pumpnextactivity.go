package delivery

import (
	"encoding/json"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ===========================================================================
// PumpNextActivityWorkflow — op 2.1's execution: the project's ONE pump,
// {projectId}:nextActivity (pumpWorkflowID). Started — or joined — by
// ExecuteNextActivity (Begin / MCP) and by PumpSweepWorkflow's 30s Schedule fan-out;
// it self-cascades via ContinueAsNew under the same id until the frontier drains.
// ===========================================================================

// pumpInput is the start (and ContinueAsNew) payload for PumpNextActivityWorkflow.
type pumpInput struct {
	ProjectID ProjectID
	// OperatorDriven is RETIRED for new pumps (plan B1.7). It marked a pump the operator
	// started (Begin), which used to ignore the RECORDED pause — the de-facto resume
	// before ResumeProject existed. No caller sets it any more, and a pump on
	// pump-honors-recorded-pause v2 ignores it: the recorded pause binds every pump, and
	// ResumeProject clears it. The field stays only so that an in-flight pump's
	// ContinueAsNew input and a history recorded at v1 still decode and replay (v1 keeps
	// its exemption). Retire it with the version markers after the drain. Explicit pause
	// SIGNALS are honoured regardless.
	OperatorDriven bool
}

// pumpDispatch is THIS pump RUN's synchronous dispatch decision, surfaced to the
// façade via the queryPumpDispatch Query so ExecuteNextActivity can return the run's
// outcome (dispatched X, or quiescent) WITHOUT blocking on the background self-cascade
// drain. A caller that joined an already-running pump reads that run's decision, not a
// fresh one of its own. Decided flips true the moment this run reaches its decision
// point — quiescent (pause / no-project / nothing eligible) or an eligible dispatch —
// which is BEFORE the blocking child.Get self-cascade below; until then the façade
// keeps polling.
type pumpDispatch struct {
	Decided    bool        `json:"decided"`
	Dispatched bool        `json:"dispatched"`
	ActivityID *ActivityID `json:"activityId,omitempty"`
}

// pumpPaceInterval is the short durable wait between cascade iterations (the pump's
// self-cascade pacing; Task 3) — a workflow.Sleep, NOT time.Sleep. Keeps the
// continue-as-new loop from busy-spinning while still draining the network promptly.
const pumpPaceInterval = 1 * time.Second

func (wf *csWorkflows) PumpNextActivityWorkflow(ctx workflow.Context, in pumpInput) (PumpResult, error) {
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

	// PAUSE GATE (Task 3; pause-delivery co-gate 2026-09-12). The cascade halts when a
	// pause Signal is observed on THIS pump execution. PauseProject reaches the pump
	// through the supervision workflow's pause branch (runPauseBranch,
	// projectsupervision.go), which relays the pause onto this pump's id — the one
	// per-project pumpWorkflowID — via messageBus.deliverSignal, on the SAME
	// operatorPauseRequested signal name. The channel is checked non-blocking
	// (ReceiveAsync — emits no command, replay-deterministic) at THREE points, one per
	// place this run can block and let a signal in:
	//   1. here, at run start;
	//   2. immediately before ExecuteChildWorkflow — readProject is an Activity, so a
	//      pause can land after (1) and before the dispatch. BOUND: it honours a pause
	//      delivered before the dispatching workflow task starts; a pause arriving
	//      DURING that task is honoured at (3), after exactly one activity;
	//   3. just before the self-cascade's ContinueAsNew — a signal still buffered on a
	//      run that ends in ContinueAsNew is NOT carried into the next run, so a pause
	//      landing while this run is parked in child.Get or the pace Sleep would
	//      otherwise be lost.
	// The signal checks are honoured regardless of OperatorDriven: an explicit pause
	// always wins. Separately, between readProject and nextEligible, the RECORDED-pause
	// gate quiets a pump on a project whose pause is already recorded — every pump since
	// plan B1.7 (v2; the I2 ruling's v1 exempted operator-started pumps). A paused pump
	// goes quiet WITHOUT ContinueAsNew. The resume path is ResumeProject: it clears the
	// recorded pause and starts or joins the pump. Begin on a paused project is refused
	// at the façade.
	pauseCh := workflow.GetSignalChannel(ctx, signalOperatorPauseRequested)
	if reason, paused := pumpPausedAtRunStart(ctx, pauseCh); paused {
		logger.Info("pump cascade paused by operator signal — going quiet without continue-as-new",
			"projectId", string(in.ProjectID), "reason", reason)
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

	// RECORDED-PAUSE GATE (I2 ruling, 2026-09-12). The supervision pause branch RECORDS
	// the pause before relaying it, so a pump the sweep (re)starts inside the
	// relay window — or any time before an operator resumes — sees the recorded pause
	// here and goes quiet, BEFORE nextEligible: no dispatch, no ContinueAsNew, and no
	// blocked-activity failure record. At v2 it binds EVERY pump (plan B1.7); v1's
	// operator-driven exemption survives only for histories that recorded it.
	if pumpHonorsRecordedPause(ctx, in, proj) {
		logger.Info("pump honours the recorded operator pause — going quiet without continue-as-new",
			"projectId", string(in.ProjectID), "reason", proj.PauseReason)
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
	}

	// LEDGER-PARTIAL RESUME (architect (D), D.2). The widened rule can pick a different
	// activity from the same recorded readProject result — a different child id — so the
	// rule is version-gated: an execution that recorded the old choice replays it.
	sel := wf.nextEligible(proj, pumpEligibilityRule(ctx))
	// (The design-skip log stood HERE until stage 4b1 Task 10: the pump reported at Info
	// every design activity it walked past, so a project whose only remaining work was design
	// did not read as an unexplained quiet tick. The pump dispatches the three now, so there
	// is nothing to report — and the SELECTION change is version-gated in
	// pumpEligibilityRule, because a recorded history that walked past `requirements` and
	// dispatched a construction activity would otherwise re-select a different child.)
	switch sel.Verdict {
	case verdictBlocked:
		// LOUD, DURABLE, APP-VISIBLE (spec §4.3). The log line alone is the failure mode
		// being eliminated — a warning buried in a serve log is how this defect consumed
		// an entire benchmark run undetected — so the escalation is the HEAD-STATE
		// record: ActivityConstructionFailed is sticky via CoarsePhaseFor, so the
		// operator sees a red node carrying its FailureReason and the reason. NOT a
		// returned workflow error: a failed Temporal execution is invisible in the
		// console. Recording the terminal also takes the activity out of NotStarted, so
		// the next scheduled tick considers the rest of the network instead of
		// re-blocking on this one. An empty credential is correct for the local store;
		// the git adapter mints just-in-time (same as the supervision pause path).
		//
		// verdictBlocked also covers two SIBLING plan defects surfaced by
		// nextEligibleActivity: an authored dependency id (network.dependencies[].dependsOn)
		// that names neither a known activity nor a known milestone (DependencyUnresolved),
		// or a milestone dependency cycle (DependencyCycle) — see projectstate.ResolveDependencySatisfied
		// (called from constructionmanager.go). Each defect class is recorded through its OWN
		// FailureReason variant — sel.BlockedFailureReason, set by nextEligibleActivity at
		// the point the defect is classified — per the ruling that one FailureReason variant
		// covers one repair class; FailureDetail (sel.BlockedReason below) discriminates
		// instances WITHIN a class (which id, which cycle path), never between classes.
		logger.Error("construction pump: activity cannot be dispatched",
			"projectId", string(in.ProjectID),
			"activityId", sel.BlockedActivityID,
			"reason", sel.BlockedReason)
		if _, ferr := wf.applyRecovering(ctx, in.ProjectID, proj.Version, func(expected projectstate.Version) (projectstate.Version, error) {
			return wf.Acts.ConstructionTransitionRecordActivityFailed(
				ctx, projectstate.ProjectID(in.ProjectID), expected,
				sel.BlockedActivityID, sel.BlockedFailureReason, sel.BlockedReason,
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

	// PRE-DISPATCH RE-CHECK (signal check 2 above). A pause that landed while readProject
	// ran must not dispatch a NEW activity: nothing would cancel it — the pause plan's
	// PipelinesToCancel is empty because InFlightPipelines is never populated.
	// THE BOUND: this honours a pause DELIVERED BEFORE the dispatching workflow task
	// starts (a signal buffered by then is visible to ReceiveAsync). A pause arriving
	// DURING that task is not visible until the next task, so it is honoured at signal
	// check 3, after exactly one activity. Signal-only, regardless of OperatorDriven: an
	// explicit pause always wins. GetVersion pins pre-change executions to the old
	// sequence (straight to the child start).
	if reason, paused := pumpPausedBehindGate(ctx, "pump-pause-before-dispatch", pauseCh); paused {
		logger.Info("pump cascade paused by operator signal before dispatch — going quiet without continue-as-new",
			"projectId", string(in.ProjectID), "activityId", activity.ActivityID, "reason", reason)
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
		return PumpResult{Dispatched: false}, nil
	}

	// Eligible ⇒ start a per-activity child workflow (idempotent on its id; a
	// redundant tick collapses to the running child). PARENT_CLOSE_POLICY ABANDON:
	// the activity is its own durable execution, independent of this pump tick's
	// continue-as-new chain.
	//
	// ONE CHILD FOR EVERY ACTIVITY (stage 4b1 Task 11). Task 10 pointed the three DESIGN
	// lifecycles at the generic DeliveryActivityWorkflow and left the other eleven on the
	// retired ConstructActivityWorkflow for exactly one commit-range, because the construction
	// half of the dispatch strategy refused by name until this commit. It no longer does, so the
	// ROSTER split (runsOnTheDeliveryChild, its two-armed id and its two-armed start) is gone.
	//
	// WHAT REPLACES IT IS A VERSION FENCE AND NOT A ROSTER, and the difference matters: the two
	// arms below are the SAME question every other change in this file asks — did this execution
	// record the marker? A pump parked in child.Get across the deploy replays its recorded
	// ChildWorkflow command, and that command names a workflow TYPE and an ID; taking the new
	// arm on replay is a non-determinism panic that wedges the project's one pump (measured —
	// two pre-b1/pre-d pump fixtures went red before this fence existed). A new pump records
	// v1 and starts the generic child for everything.
	child := wf.startActivityChild(ctx, in.ProjectID, activity)
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
	//
	// THIS BLOCKING Get IS DELIBERATELY UNCHANGED BY STAGE 4b1 (architect Ruling 3(b)): the
	// react-by-signal pump — one that starts a child and returns, and is re-woken by the
	// child's terminal — is stage 4b2's, and moving the pump's shape in the same wave as the
	// child's would make a cascade failure unattributable to either. It is not an oversight.
	if err := child.Get(ctx, nil); err != nil {
		return PumpResult{}, err
	}

	// Pace the cascade with a short durable wait (workflow.Sleep — replay-safe; NOT
	// time.Sleep), then ContinueAsNew to pick the next eligible activity. ContinueAsNew
	// carries ONLY pumpInput (no accumulated state ⇒ unbounded history is avoided and
	// determinism is trivial). ContinueAsNew keeps the SAME workflow id — the project's
	// one pump id, pumpWorkflowID — so a re-fire onto a cascading pump never forks a
	// second one: ExecuteNextActivity joins it (USE_EXISTING, constructionmanager.go)
	// and the 30s sweep's child start collapses on "already started" (pumpsweep.go).
	// The cascade's own drain-to-quiet ends it.
	if err := workflow.Sleep(ctx, pumpPaceInterval); err != nil {
		return PumpResult{}, err
	}
	// DRAIN BEFORE THE HAND-OFF (pause-delivery co-gate). A pause that arrived while
	// this run was parked in child.Get or the Sleep above sits in this run's signal
	// buffer, which ContinueAsNew discards — so honor it here: the current activity has
	// finished, and no further child starts. GetVersion pins pre-change executions to
	// the old command sequence (straight to ContinueAsNew): replaying a history that
	// continued-as-new with a pause buffered would otherwise take the new quiet-return
	// branch and fail the task with a non-determinism error.
	if reason, paused := pumpPausedBehindGate(ctx, "pump-drain-pause-before-continue-as-new", pauseCh); paused {
		logger.Info("pump cascade paused by operator signal after the current activity — going quiet without continue-as-new",
			"projectId", string(in.ProjectID), "activityId", string(dispatchedActivity), "reason", reason)
		return PumpResult{Dispatched: true, ActivityID: &dispatchedActivity}, nil
	}
	// The WHOLE input rides ContinueAsNew, so a cascade recorded at v1 keeps the
	// OperatorDriven mandate it started with.
	return PumpResult{}, workflow.NewContinueAsNewError(ctx, executionKindPump, in)
}

// pumpPausedBehindGate is a signal pause check (2: pre-dispatch, 3: pre-ContinueAsNew)
// behind its OWN GetVersion change id. Pre-change executions (DefaultVersion) skip the
// check entirely, keeping their recorded command sequence. GetVersion is always called
// first, so the marker is recorded deterministically on every new run. (Extracted so
// the pump body stays within the gocyclo budget — behavior identical to the inline
// GetVersion-then-ReceiveAsync form.)
func pumpPausedBehindGate(ctx workflow.Context, changeID string, ch workflow.ReceiveChannel) (reason string, paused bool) {
	if workflow.GetVersion(ctx, changeID, workflow.DefaultVersion, 1) < 1 {
		return "", false
	}
	return pumpPauseRequested(ch)
}

// changePumpHonorsRecordedPause versions the recorded-pause gate. Bumped to 2 by plan
// B1.7 (the semantics changed, not in place: local histories recorded v1).
const changePumpHonorsRecordedPause = "pump-honors-recorded-pause"

// pumpHonorsRecordedPause reports whether this run must go quiet on the project's
// RECORDED pause. GetVersion is always called, so a new run records v2:
//   - DefaultVersion (pre-I2 executions): no gate;
//   - v1 (I2 ruling): only a pump the operator did NOT start honours it;
//   - v2 (B1.7): every pump honours it — ResumeProject is the one way back.
func pumpHonorsRecordedPause(ctx workflow.Context, in pumpInput, proj projectstate.Project) bool {
	switch workflow.GetVersion(ctx, changePumpHonorsRecordedPause, workflow.DefaultVersion, 2) {
	case workflow.DefaultVersion:
		return false
	case 1:
		return !in.OperatorDriven && proj.OperatorPaused
	default:
		return proj.OperatorPaused
	}
}

// pumpPausedAtRunStart is pause check 1, with the decode change version-gated. The
// pre-change body received into an operatorPauseSignal struct, which silently drops a
// relayed (binary/plain) pause; pumpPauseRequested counts it. Replaying a pre-change
// history that buffered such a pause through the new decode would take the quiet-return
// branch where the history recorded a dispatch — a non-determinism error — so
// pre-change executions keep the old struct decode.
func pumpPausedAtRunStart(ctx workflow.Context, ch workflow.ReceiveChannel) (reason string, paused bool) {
	if workflow.GetVersion(ctx, "pump-pause-decode-any", workflow.DefaultVersion, 1) >= 1 {
		return pumpPauseRequested(ch)
	}
	var sig operatorPauseSignal
	if ch.ReceiveAsync(&sig) {
		return sig.Reason, true
	}
	return "", false
}

// pumpPauseRequested drains one pending operatorPauseRequested signal, non-blocking,
// and reports whether a pause was pending (plus its best-effort reason, for the log).
//
// It decodes into `any`, deliberately: ReceiveAsync SILENTLY DISCARDS a signal whose
// payload cannot decode into the target (the SDK logs "Corrupted signal" and moves
// on), and the pump's pause arrives as messageBus.deliverSignal's raw bytes
// (binary/plain — see relayPauseToPump) while a directly-sent operatorPauseSignal
// arrives as JSON. A struct target would drop the former; `any` accepts both.
//
// AN UNDECODABLE PAUSE STILL COUNTS AS A PAUSE (decided, pinned by
// Test_Pump_UndecodablePauseSignal_StillPauses). The channel NAME carries the
// operator's intent; the body only carries a reason for the log. Dropping a pause over
// a malformed body fails OPEN — the cascade keeps dispatching through an operator halt,
// the exact defect this gate exists to prevent — while counting it fails SAFE: the pump
// goes quiet, nothing is lost, and the next Begin resumes. Only the reason is lost.
func pumpPauseRequested(ch workflow.ReceiveChannel) (reason string, paused bool) {
	var raw any
	if !ch.ReceiveAsync(&raw) {
		return "", false
	}
	switch v := raw.(type) {
	case []byte:
		var sig operatorPauseSignal
		if err := json.Unmarshal(v, &sig); err != nil {
			return "", true
		}
		return sig.Reason, true
	case map[string]any:
		r, _ := v["Reason"].(string)
		return r, true
	default:
		return "", true
	}
}

// changeDesignActivitiesDispatchable versions the DESIGN admission (stage 4b1 Task 10).
// It is its own change id rather than a bump of changeLedgerPartialResume because the two
// rules answer different questions and a shared id would pin them together forever.
const changeDesignActivitiesDispatchable = "design-activities-dispatchable"

// pumpEligibilityRule is the selection rule this pump run uses: the pre-D1
// eligibleNotStarted for an execution that recorded no changeLedgerPartialResume marker,
// then eligibleDispatchable, then eligibleWithDesign once the design activities have a child
// to run on. GetVersion is always called at every rung, so the markers are recorded
// deterministically on every new run and a recorded history resolves the ladder it recorded.
//
// WHY THE DESIGN RUNG NEEDS A FENCE AT ALL, measured: this repo's committed slot 9 opens with
// requirements/architecture/projectDesign and none of the three has an execution row, so a
// pump history that walked past them and dispatched a construction activity would — replayed
// under eligibleWithDesign — select `requirements` instead and start a DIFFERENT child id.
func pumpEligibilityRule(ctx workflow.Context) eligibilityRule {
	if workflow.GetVersion(ctx, changeLedgerPartialResume, workflow.DefaultVersion, 1) < 1 {
		return eligibleNotStarted
	}
	if workflow.GetVersion(ctx, changeDesignActivitiesDispatchable, workflow.DefaultVersion, 1) < 1 {
		return eligibleDispatchable
	}
	return eligibleWithDesign
}

// startActivityChild starts the child that runs ONE activity and returns its future.
//
// ONE CHILD, ONE ID. Stage 4b1 Task 11 fenced this with changeGenericActivityChild because a
// pump parked in child.Get across the deploy would replay a recorded ChildWorkflow command
// naming the RETIRED type and id — a non-determinism panic on the project's one pump. Task 13
// RETIRED THAT FENCE together with the workflow it protected: the drain this wave requires
// before deploy is what discharges it, and a GetVersion marker whose other arm names a workflow
// the build no longer has is worse than no marker — it compiles, records a version, and then
// panics differently. The drain sequence is in docs/bugs/2026-09-24-stage3-rail-earmarks.md.
func (wf *csWorkflows) startActivityChild(
	ctx workflow.Context, projectID ProjectID, activity constructionActivity,
) workflow.ChildWorkflowFuture {
	id := ActivityID(activity.ActivityID)
	cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:        deliveryActivityWorkflowID(projectID, id),
		ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON,
	})
	workflow.GetLogger(ctx).Info("delivery pump: starting the generic DAG child",
		"projectId", string(projectID), "activityId", activity.ActivityID, "activityType", activity.Type.String())
	return workflow.ExecuteChildWorkflow(cctx, executionKindDeliveryActivity, deliveryActivityInput{
		ProjectID: projectID, ActivityID: id, Activity: activity,
	})
}

// nextEligible resolves the next selection via the injected helper. With no helper
// wired it is a quiet tick.
func (wf *csWorkflows) nextEligible(proj projectstate.Project, rule eligibilityRule) pumpSelection {
	if wf.NextEligibleActivity == nil {
		return pumpSelection{Verdict: verdictQuiescent}
	}
	return wf.NextEligibleActivity(proj, rule)
}

// Shared workflow-context helper (used by 3 csWorkflows); lives in its first caller's file per the file-layout standard.
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
func (wf *csWorkflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
	env, err := wf.Acts.DesignSessionReadProjectOnBranch(ctx, projectstate.ProjectID(projectID), "")
	if err != nil {
		return projectstate.Project{}, err
	}
	return env.Decode()
}
