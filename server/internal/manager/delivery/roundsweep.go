package delivery

import (
	"time"

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
// drain and keep in the golden for no behaviour of its own.
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
// The generated, exported PumpResult is exported because it IS contract surface; this
// one is not.
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
	// Reopened is how many activities THIS execution re-opened because they completed
	// their work and failed to land it (stage 4b3 Task 10). Like Stamped, only the
	// single-project arm produces it, and it counts what this tick WROTE — a row another
	// writer healed between the read and the write is not this tick's heal.
	Reopened int
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
//
// SINCE STAGE 4b3 THE PER-PROJECT ARM HAS A SECOND STEP (sweepReopenNotLanded), and it is
// here rather than in a workflow of its own for a reason that is scope, not taste: a new
// registered type would be a new golden name, a new frozen name, a new Schedule and a new
// drain item in the wave that runs the drain and cuts the release. This workflow is already
// Schedule-triggered, already has the fan-out/single-project discriminator, already bounds
// its writes per tick, and already closes the records no run will ever come back to — which
// is the same job one noun over.
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
// would DISPATCH work, which is the thing the operator paused to stop. The ROUND half of
// this sweep dispatches nothing and advances no lifecycle: it corrects a record that is
// already wrong, and the operator who paused the project to look at it is the reader a
// round rendering `running` forever misleads most.
//
// THE PAUSE IS HONOURED ONE LEVEL DOWN INSTEAD (stage 4b3 Task 10), because the second
// step is not like the first: a re-open leads to a dispatch the moment the project
// resumes, and it clears the very fact the operator paused to look at. Filtering it HERE
// would cost the paused project its round sweep too, so sweepReopenNotLanded checks the
// pause itself and the fan-out stays unfiltered.
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
// activity of one project, in a deterministic order, bounded per tick — and then offer the
// re-open heal to every activity that completed its work and failed to land it.
//
// THE TWO STEPS SHARE ONE PROJECT READ and nothing else. The heal is handed the project as
// it was read at the top of this arm, not the head version the round loop advanced: the two
// steps are independent jobs over the same document, and threading a mutated counter between
// them would couple them through a variable for the sake of a re-read that applyRecovering
// already makes correctly. On the rare tick that does BOTH jobs the heal's first write pays
// one Conflict and the loop re-seeds; on every other tick — which is every tick with no
// stranded round, the common case — the seed is exact.
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
				// The tick is at its write bound, so the HEAL does not run either: this
				// execution has already done a tick's worth of work, and a heal deferred 300s
				// on a project that is this far behind is the right trade against a Temporal
				// task that never finishes. The next tick takes both.
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

	reopened, rerr := wf.sweepReopenNotLanded(ctx, in.ProjectID, proj)
	if rerr != nil {
		return roundSweepResult{}, rerr
	}
	result.Reopened = reopened
	return result, nil
}

// sweepReopenNotLanded offers the heal for an activity that completed its work and failed
// to land it (CoarsePhaseFor -> completedNotLanded). It writes a REQUEUE note, which is the
// one operator-note kind that changes the row: it clears the five head facts, keeps both
// ledgers and the lifecycle pin, and the pump selects the activity again because the note
// itself is the evidence it reads (RequeuedAfterExit).
//
// WHY A SWEEP MAY PRESS THIS BUTTON. It is not a decision about the work — the walk already
// decided that and the ledger keeps it — it is a decision about a LANDING that provably did
// not happen, and the re-run re-seeds every task that passed rather than redoing them.
// Leaving it to a human means leaving it to a human who was shown nothing until stage 4b3
// Task 9 gave the state a name. And nothing else was ever going to re-run it: a not-landed
// row is not Done, so it does not unblock its dependents either — the cascade simply stops.
//
// IT KEYS ON THE TAIL'S OWN MARK, NOT ON UNCOMMITTED SLOTS, and that is the precision this
// sweep needs rather than eagerness. "Completed with a slot still AwaitingReview" is also
// the honest MID-FLIGHT shape of a design activity whose gate has not run, and a sweep that
// re-ran those would re-dispatch work nobody asked for. The three facts that make the
// difference — a recorded completion, no recorded failure, and a tail-failure detail — are
// exactly what CoarsePhaseFor folds into completedNotLanded, so this asks the DERIVATION
// the operator's node is drawn from rather than restating its conditions here. It is asked
// as a switch, because a sum-type linter covers a switch and not an `==` (Task 9 §3, where
// two `if` sites over this same enum were silently wrong).
//
// IT DOES NOT RUN ON A PAUSED PROJECT, and this is the one place the two halves of this
// workflow disagree. The round half deliberately ignores the operator pause because it
// dispatches nothing and only corrects a record that is already wrong (fanOutRoundSweep
// says so in those words). A re-open is the opposite on both counts: it leads to a dispatch
// the moment the project resumes, which is the thing the operator paused to stop, and it
// CLEARS TailFailureDetail — the very evidence the operator paused to look at. The operator
// can still press the button themselves; what may not happen is the platform pressing it
// behind them.
//
// IT CANNOT LOOP ON ONE BROKEN LANDING. RecordOperatorNote dedups on NoteID before it
// re-arms, and RequeuedAfterExit requires the newest requeue note to be newer than the newest
// RESOLVED attempt — so an activity re-opened, re-run and finished again is finished, and its
// stale requeue note does not re-arm it a second time. The NoteID is reopenNoteID, the
// operator button's own derivation: it keys on the row's EXIT STAMP, so one broken landing
// mints one note however many ticks see it, and the heal converges with an operator's rather
// than filing a second reason against the same terminal. It is derived, never minted — a
// workflow that generated a random id would be non-deterministic on replay.
//
// AND IT CANNOT PING-PONG ACROSS BROKEN LANDINGS EITHER (stage 4b3 Task 11), which is a
// SECOND and separate claim. Each cycle of heal -> re-run -> break-again is legitimate in
// isolation and each one has a NEW exit stamp, hence a new NoteID, so the dedup above never
// sees it: a deterministically broken tail would otherwise be re-opened every 300s for ever,
// growing a git-as-DB document's note ledger without bound. healWouldRepeatOneThatChangedNothing
// is that bound, and it is asked BEFORE the per-tick budget so a refused row costs nothing.
//
// BOUNDED per tick, the same reason roundSweepMaxPerTick states for the rounds: one Temporal
// task may not do unbounded work on a project with years of history. The budget is its OWN
// rather than shared with the rounds, because a project holding 200 stranded rounds must
// still heal, and reaching this bound at all would mean 200 activities broke their landings
// in one plan — an incident, not a paced backlog.
func (wf *csWorkflows) sweepReopenNotLanded(ctx workflow.Context, projectID ProjectID, proj projectstate.Project) (int, error) {
	logger := workflow.GetLogger(ctx)
	if proj.OperatorPaused {
		return 0, nil
	}

	head := proj.Version
	reopened := 0
	for _, activityID := range sortedActivityIDs(proj.ActivityExecution) {
		row := proj.ActivityExecution[activityID]
		switch projectstate.CoarsePhaseFor(row, nil) {
		case projectstate.ActivityConstructionCompletedNotLanded:
			// The one state this heal exists for.
		case projectstate.ActivityConstructionNotStarted, projectstate.ActivityConstructionRunning,
			projectstate.ActivityConstructionDone, projectstate.ActivityConstructionFailed:
			// Running and NotStarted have nothing to re-open; Done landed; Failed is a
			// decision about the WORK, and re-running it on a timer would be the platform
			// overriding a terminal a walk established. Only an operator re-opens those.
			continue
		}
		if healWouldRepeatOneThatChangedNothing(row) {
			// THE BOUND. Not a cap on a counter — the row itself says the last re-open
			// produced no work, so pressing the same button again cannot produce any either.
			// The red node stays red, which is the legible outcome: an operator reading the
			// activity finds one requeue note, the tail's own detail, and a state that has
			// stopped flapping.
			logger.Info("activity completed its work and did not land it, but the last re-open resolved no task; not re-opened again",
				"projectId", string(projectID), "activityId", activityID,
				"tailFailure", row.TailFailureDetail)
			continue
		}
		if reopened >= roundSweepMaxPerTick {
			logger.Info("round sweep hit its per-tick heal bound; the rest is re-opened on the next tick",
				"projectId", string(projectID), "reopened", reopened)
			return reopened, nil
		}

		// THE ROW THIS ITERATION WRITES, bound for the reason the round loop states: without
		// it applyRecovering's Conflict arm cannot re-read the row, so a heal racing another
		// writer would burn twenty attempts and fail the whole sweep as
		// MutateConflictExhausted instead of recognising the state it is in.
		rowVersion := row.Version
		rctx := withRowAccessor(ctx, rowAccessor{
			activityID: activityID,
			version:    func() int64 { return rowVersion },
			setVersion: func(v int64) { rowVersion = v },
		})
		note := projectstate.OperatorNoteInput{
			NoteID: reopenNoteID(ActivityID(activityID), row),
			Kind:   projectstate.NoteRequeue,
			Gate:   reopenGateKey,
			Text:   sweepReopenNoteText(row),
		}
		v, werr := wf.applyRecovering(rctx, projectID, head, func(expected projectstate.Version) (projectstate.Version, error) {
			return wf.Acts.ActivityExecutionRecordOperatorNote(rctx, projectstate.ProjectID(projectID), expected,
				rowVersion, activityID, note, "", railCredEnvelope{}.toProjectState())
		})
		if werr != nil {
			// A row that stopped being re-openable between the read and the write — an
			// operator who pressed the button first, or a re-run already under way — is
			// refused by the store as a terminality Conflict that moves nothing. The heal
			// happened; it was simply not this tick's. Anything else is real.
			if isTerminalConflict(werr) {
				logger.Info("activity is no longer waiting to be re-opened; nothing to heal",
					"projectId", string(projectID), "activityId", activityID)
				continue
			}
			return 0, werr
		}
		head = v
		reopened++
		logger.Info("activity completed its work and did not land it; re-opened",
			"projectId", string(projectID), "activityId", activityID,
			"tailFailure", row.TailFailureDetail)
	}
	return reopened, nil
}

// healWouldRepeatOneThatChangedNothing reports whether this row has ALREADY been re-opened
// and the re-open achieved nothing — in which case re-opening it again will achieve nothing
// either, and the sweep stops.
//
// THE HAZARD IT BOUNDS (stage 4b3 Task 11, carried out of Task 10's own concern list). The
// anti-loop property Task 10 proved is exact and narrow: ONE broken landing is healed once,
// because the heal clears the fact the sweep keys on and the NoteID is derived from the exit
// stamp. A DETERMINISTICALLY broken tail defeats it, not by looping on one landing but by
// producing a new one every cycle: heal -> re-run -> the tail breaks the same way -> a new
// CompletedAt -> a new reopenNoteID -> heal. Every 300s, for ever, each cycle appending a note
// to a row inside a git-as-DB document that nothing prunes.
//
// WHY NOT AN ATTEMPT CAP. A counter ("heal at most N times") cannot tell the two cases apart:
// an activity legitimately re-opened three times because each re-run got further is the SAME
// number as one re-opened three times to no effect, and the honest distinction is not how
// often the button was pressed but whether pressing it did anything. So this asks the row.
//
// "DID ANYTHING" IS THE ATTEMPT LEDGER, and specifically a RESOLVED attempt recorded after the
// newest requeue note. That is the one measure of progress available and it is the right one:
//   - CompletedAt cannot serve. It changes on EVERY cycle by construction — a fresh exit stamp
//     is precisely what makes the ping-pong possible — so "CompletedAt moved" is true of the
//     pathological case as loudly as of the healthy one.
//   - TailFailureDetail cannot serve either. Comparing it against the previous note's text
//     would make the bound depend on a Temporal error string staying byte-identical across
//     runs, and those carry run ids and timestamps; a detail that varies in its noise would
//     read as "something changed" and ping-pong exactly as before.
//   - The attempt ledger is the fact the re-run itself is built on. A re-run seeds every task
//     that already passed FROM the ledger rather than redoing it (that is what makes the heal
//     cheap), so a walk whose tasks all passed and whose TAIL broke records no new resolved
//     attempt at all. Nothing new resolved == the re-run did the same nothing it did last time.
//
// WHAT THIS ACTUALLY BOUNDS, corrected 2026-09-30 after two review rounds. The clause above is
// the RATIONALE, and the rationale reads as though a re-run that made progress would record a
// resolved attempt and be healed again. It would not — but NOT because nothing records one.
// Three writers do, with fresh ids: recordTaskAttempt (deliveryactivity.go:3309),
// resolveWorkAttempt (:5027) and passGateAttempt (:5150). A first correction of this comment
// said "no path records a resolved attempt on a re-run"; that was over-broad and is itself
// corrected here.
//
// THE REASON THE PROPERTY HOLDS IS REACHABILITY, NOT ABSENCE. In the state this sweep heals,
// every task of the walk already passed and only the main-writing TAIL broke. seedWalkFromLedger
// (:3189) marks every such task walkTaskPassed from the ledger, and finalizeWalk's precondition
// (:3377) is that every task in the lifecycle passed — so the re-run runs NO task, and all three
// writers above are unreachable. What is left is the tail: a merge and, for a design activity,
// N slot commits, which record zero attempts by construction.
//
// SO THE PREDICATE RETURNS TRUE ON THE SECOND SWEEP OF A ROW WHETHER OR NOT THE RE-RUN GOT
// FURTHER. A requirements activity holding four slots whose run 2 commits mission and glossary
// and fails on volatilities has made real progress, and this function cannot see any of it.
//
// THE TRUE PROPERTY IS "AT MOST ONE AUTOMATIC HEAL PER ROW, EVER" — not one per unit of
// measurable progress. That is STRONGER than advertised and it fails CLOSED: the row keeps its
// red node, the operator's button is never inhibited (see below), and nothing is lost but a
// second free retry. The behaviour is therefore deliberately UNCHANGED; only this statement of
// it is. The measure that would actually tell the two cases apart is uncommittedSlotsOf
// shrinking between ticks, earmarked in docs/bugs/2026-09-29-stage4b3-earmarks.md rather than
// built here, because Task 11 rejected the slot set as the TRIGGER's key and re-opening that for
// the BOUND is a design decision. Note also that
// Test_RoundSweep_ATailThatResolvedATaskSinceTheHealIsHealedAgain pins the progress arm with a
// HAND-BUILT resolved attempt that the production path cannot produce in that state — a true
// statement about this predicate, not about any reachable run.
//
// IT DOES NOT CARE WHO PRESSED THE BUTTON, and that is deliberate rather than a limitation to
// route around. OperatorNoteInput carries no author member (the round half's decidedBy is a
// field; this provenance lives in the note's free TEXT), so a query cannot tell a sweep-heal
// from an operator's. It does not need to: the reasoning holds for both. A re-open that
// resolved no task is a re-open that changed nothing, whoever filed it, and the platform
// declining to repeat a human's ineffective re-open is the same correctness as declining to
// repeat its own. THE OPERATOR IS NEVER INHIBITED — reopenActivity has no such check and this
// task did not give it one; a human who wants to try again always can, and their button is the
// documented way past this bound.
func healWouldRepeatOneThatChangedNothing(row projectstate.ActivityExecution) bool {
	var reopenedAt time.Time
	for _, n := range row.OperatorNotes {
		if n.Kind == projectstate.NoteRequeue && n.Gate == reopenGateKey && n.RecordedAt.After(reopenedAt) {
			reopenedAt = n.RecordedAt
		}
	}
	if reopenedAt.IsZero() {
		// Never re-opened, so this is the FIRST heal of this row and the bound has nothing to
		// say about it.
		return false
	}
	for _, a := range row.Attempts {
		if a.Outcome == projectstate.OutcomePending || a.EndedAt == nil {
			continue
		}
		if a.EndedAt.After(reopenedAt) {
			// A task resolved since the re-open: the re-run did real work, so this broken
			// landing is a different one from the one that was healed and it gets its own heal.
			return false
		}
	}
	return true
}

// sweepReopenNoteText is the requeue note the sweep files, and it has to be two things at
// once. DETERMINISTIC, because RecordOperatorNote refuses a second note under one id with
// different content ("one id names one note") — so a re-delivery of the same heal has to
// produce the same sentence, which it does: every word comes from the row.
//
// And it CARRIES THE CAUSE, because the heal itself destroys it — reopenTerminalRow clears
// TailFailureDetail with the other four head facts, so an operator reading the row after the
// re-open would otherwise find no trace of why it was re-opened. The note is where the fact
// goes to survive.
//
// TRUNCATED AT maxOperatorNoteRunes, which is the limit the façade already rules for every
// operator note (checkOperatorNoteSize) rather than a number invented here. It matters
// because TailFailureDetail carries a Temporal error string verbatim and nothing bounds it,
// and this note lands in project.json, which is a git-as-DB document.
func sweepReopenNoteText(row projectstate.ActivityExecution) string {
	text := "re-opened by the platform sweep: the activity completed its work and failed to land it — " +
		row.TailFailureDetail
	if r := []rune(text); len(r) > maxOperatorNoteRunes {
		return string(r[:maxOperatorNoteRunes])
	}
	return text
}
