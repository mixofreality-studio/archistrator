package delivery

import (
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ===========================================================================
// RoundSweepWorkflow — Schedule-triggered entry (5m), platform-wide (stage 4b1
// Task 6). Closes the review rounds that no run will ever decide.
//
// NOT one of the frozen public façade ops: nothing calls it through a Manager
// interface, the Schedule fires it and no caller awaits its result. Mirrors
// PumpSweepWorkflow's structure (pumpsweep.go) and, like it, exists because a
// Temporal Schedule action carries a FIXED workflow type and FIXED args on every
// firing (messagebus.go's RegisterSchedule), so one cannot vary a per-project
// payload per tick.
// ===========================================================================

// roundSweepMaxPerTick bounds one tick's writes. A sweep that stamped an unbounded
// number of rounds would, on a project whose ledger has years of history, make one
// Temporal task do unbounded work; 200 is far above any real backlog and low enough
// that a pathological one is paced across ticks rather than timing a task out.
const roundSweepMaxPerTick = 200

// roundSweepInput is the start payload for RoundSweepWorkflow, and it carries the
// arm discriminator.
//
// An EMPTY ProjectID is the Schedule's own firing — the fan-out arm, which enumerates
// projects and starts one child per project. A NON-EMPTY one is such a child: the
// single-project arm that actually stamps. The discriminator is a field rather than a
// second workflow type because the fan-out is not a different job, it is the same job
// at platform scope, and a second registered type would be a second name to freeze,
// drain and keep in the golden for no behaviour of its own (the same argument
// replanSweepInput's nilable ProjectID already makes for the replan sweep).
type roundSweepInput struct {
	ProjectID ProjectID
	// TickID is a CORRELATION id only — the same string the child's workflow id embeds,
	// carried on the payload so a child's own history says which firing produced it
	// (the posture ExecuteNextActivity's tickID already has: "a CORRELATION id only",
	// deliverymanager.go). The fan-out mints it; a caller never supplies one, because
	// the Schedule cannot.
	TickID string
}

// roundSweepResult is this tick's outcome. Deliberately UNEXPORTED, for the reason
// pumpSweepResult states for itself: this is not a façade op, so it carries no
// service-contract entry and the encapsulation gate's rule holds — the only exported
// symbols this package may carry are its generated contract surface and the five
// documented registration entrypoints (arch_test.go's encapsulationAllowlistData).
// The generated, exported PumpResult / ReplanSweepResult are exported because they ARE
// contract surface; this one is not.
type roundSweepResult struct {
	// Stamped is how many stranded rounds THIS execution withdrew. Only the
	// single-project arm stamps; the fan-out arm reports 0 and never sums its
	// children's counts, because it does not wait for them (see below).
	Stamped int
	// Bounded is true when the single-project arm stopped at roundSweepMaxPerTick with
	// rounds still to sweep — the next tick finishes them.
	Bounded bool
	// SweptProjects is every project the FAN-OUT arm itself STARTED a child for this
	// tick. A project whose child could not be started because one is already running
	// under this tick's id is skipped by the collapse branch (a `continue` BEFORE the
	// append) and does not appear here — this field is "started just now", like
	// pumpSweepResult.PumpedProjects.
	SweptProjects []ProjectID
}

// RoundSweepWorkflow closes rounds that no run will ever decide.
//
// THE WINDOW, as both rails already state it: a child that dies between
// OpenReviewRound and DecideReviewRound leaves round n PENDING forever. The resume
// mints n+1 off the same counter, so nothing is duplicated and no id collides — but
// nobody goes back to close n, and roundOutcome renders it `running` for as long as
// it is the gate's last round. With no live session it never even reads
// awaitingHuman, so the screen shows work in flight that nothing is doing.
//
// THE RULE, and it is deliberately the narrowest one that is always true: a pending
// round is stranded when a LATER round exists on the same (taskId, artifactKind) join
// key. That later round is proof a run already decided to start over, which is the
// only thing a sweep can know from outside — it cannot know whether a run is gone, and
// asking Temporal would make head-state truth depend on a control-plane query — and
// "same gate" is roundGateKey, the task AND the kind.
// A pending round that is its gate's LATEST is left alone: it may be a live gate
// awaiting a human, and stamping that would withdraw a review someone is reading.
//
// The terminal is RoundWithdrawn, which stage 4b1 gave its own wire member: the round
// was pulled back before anyone decided it, which is exactly what happened. decidedBy
// names the sweep, so the ledger never claims a person decided it.
func (wf *csWorkflows) RoundSweepWorkflow(ctx workflow.Context, in roundSweepInput) (roundSweepResult, error) {
	if in.ProjectID == "" {
		return wf.fanOutRoundSweep(ctx)
	}
	return wf.sweepProjectRounds(ctx, in)
}

// fanOutRoundSweep is the Schedule's arm: enumerate every project and start that
// project's own sweep as an ABANDON-policy child of this workflow's OWN type.
//
// NO PHASE FILTER, unlike the pump sweep's. The pump sweep filters to
// PhaseConstruction because that is the only phase its per-project child can do
// anything in; a stranded round, by contrast, is reachable on EVERY rail — the design
// co-author's crash window is the same window (coauthorartifact.go) and the design
// activities hold execution rows too, so filtering here would leave exactly the rounds
// the newest rail produces unswept.
//
// NO OPERATOR-PAUSE FILTER either, and this is the one place the two sweeps
// deliberately disagree. The pump sweep skips a paused project because starting a pump
// would DISPATCH work, which is the thing the operator paused to stop. This sweep
// dispatches nothing and advances no lifecycle: it corrects a record that is already
// wrong, and the operator who paused the project to look at it is the reader a round
// rendering `running` forever misleads most.
//
// The child id carries the TICK (roundSweepWorkflowID), unlike the pump's deliberately
// tick-invariant id: the pump's id is shared so a still-cascading pump absorbs a
// redundant firing, whereas two sweep ticks over the same project are not redundant —
// the later one sees the later ledger. The already-started tolerance therefore only
// collapses a DOUBLE FIRING of the same tick, and two OVERLAPPING ticks are expected to
// race; the terminal-conflict arm below is what makes that race a no-op rather than a
// failure.
func (wf *csWorkflows) fanOutRoundSweep(ctx workflow.Context) (roundSweepResult, error) {
	logger := workflow.GetLogger(ctx)

	summaries, err := wf.Acts.ProjectStateListProjects(ctx, pumpSweepOwnerScope)
	if err != nil {
		return roundSweepResult{}, err
	}

	// The firing's identity, and it has to come from the history rather than a clock:
	// GetInfo is replay-safe, so every replay of this execution mints the same child ids
	// it did the first time.
	tickID := workflow.GetInfo(ctx).WorkflowExecution.RunID

	result := roundSweepResult{SweptProjects: []ProjectID{}}
	for _, s := range summaries {
		projectID := ProjectID(s.ProjectID)
		cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:        roundSweepWorkflowID(projectID, tickID),
			ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON,
		})
		child := workflow.ExecuteChildWorkflow(cctx, executionKindRoundSweep,
			roundSweepInput{ProjectID: projectID, TickID: tickID})
		// Wait only for the START ack, NOT completion: a project with a long ledger can
		// take a while, and the fan-out must stay short so one slow project cannot delay
		// every other project's sweep behind it.
		var childWE workflow.Execution
		if serr := child.GetChildWorkflowExecution().Get(ctx, &childWE); serr != nil {
			if temporal.IsWorkflowExecutionAlreadyStartedError(serr) {
				// The same tick already has this project's sweep running — a double firing,
				// which is the outcome wanted (not two sweeps racing one ledger), not a
				// failure.
				logger.Info("round sweep: this tick is already sweeping the project, skipped",
					"projectId", string(projectID), "tickId", tickID)
				continue
			}
			return roundSweepResult{}, serr
		}
		result.SweptProjects = append(result.SweptProjects, projectID)
	}
	logger.Info("round sweep fan-out complete", "projects", len(result.SweptProjects), "tickId", tickID)
	return result, nil
}

// sweepProjectRounds is the per-project arm: withdraw every stranded round on every
// activity of one project, in a deterministic order, bounded per tick.
func (wf *csWorkflows) sweepProjectRounds(ctx workflow.Context, in roundSweepInput) (roundSweepResult, error) {
	logger := workflow.GetLogger(ctx)
	proj, err := wf.readProject(ctx, in.ProjectID)
	if err != nil {
		if isReadNotFound(err) {
			// A project with no document yet has no rounds. Not an error, and not
			// something to retry every 300s.
			return roundSweepResult{}, nil
		}
		return roundSweepResult{}, err
	}

	headVersion := proj.Version
	result := roundSweepResult{}
	for _, activityID := range sortedActivityIDs(proj.ActivityExecution) {
		row := proj.ActivityExecution[activityID]
		stranded := strandedRounds(row)
		if len(stranded) == 0 {
			continue
		}

		// THE ROW THIS ITERATION WRITES, bound so applyRecovering's Conflict arm can
		// re-read it (Task 7). The sweep holds no single row for the whole workflow, but
		// it holds EXACTLY ONE per write, which is the granularity the accessor is about:
		// without this binding the decided-round refusal — the terminality Conflict Task 7
		// built the arm FOR, and the only one this sweep can provoke — would burn twenty
		// attempts and fail the whole project's sweep as MutateConflictExhausted, and the
		// isTerminalConflict branch below would be unreachable by construction.
		rowVersion := row.Version
		rctx := withRowAccessor(ctx, rowAccessor{
			activityID: activityID,
			version:    func() int64 { return rowVersion },
			setVersion: func(v int64) { rowVersion = v },
		})

		for _, r := range stranded {
			if result.Stamped >= roundSweepMaxPerTick {
				logger.Info("round sweep hit its per-tick bound; the rest is swept on the next tick",
					"projectId", string(in.ProjectID), "stamped", result.Stamped)
				result.Bounded = true
				return result, nil
			}
			v, serr := wf.applyRecovering(rctx, in.ProjectID, headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
				return wf.Acts.ActivityExecutionDecideReviewRound(
					rctx, projectstate.ProjectID(in.ProjectID), expected,
					rowVersion, activityID, r.RoundID,
					projectstate.RoundWithdrawn, roundSweepDecidedBy,
					railCredEnvelope{}.toProjectState())
			})
			if serr != nil {
				// A round another writer decided between the read and the write is ALREADY
				// closed, which is this sweep's goal; anything else is real.
				if isTerminalConflict(serr) {
					logger.Info("round already decided by someone else; nothing to sweep",
						"projectId", string(in.ProjectID), "activityId", activityID, "roundId", r.RoundID)
					continue
				}
				return roundSweepResult{}, serr
			}
			headVersion = v
			// The write stamped the row's per-activity counter, so the run's copy advances by
			// hand — the same discipline as the construction rail's rowAdvanced(). Without it
			// the NEXT stranded round on this same row would present a stale expectation and
			// pay a Conflict to learn what this line already knows.
			rowVersion++
			result.Stamped++
			logger.Info("stranded review round withdrawn",
				"projectId", string(in.ProjectID), "activityId", activityID,
				"roundId", r.RoundID, "tickId", in.TickID)
		}
	}
	return result, nil
}
