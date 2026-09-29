package delivery

import (
	"context"
	"encoding/json"
	"maps"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// ===========================================================================
// DeliveryActivityWorkflow — ONE child that walks ANY lifecycle's task DAG
// (stage 4b1 Task 8). It replaces walkPhases' flat []ActivityMethodPhase walk,
// CoAuthorArtifactWorkflow, CoAuthorPhase2ArtifactWorkflow,
// SystemDesignPhaseWorkflow and both phase-advance wrappers.
//
// This file also HOSTS every workflow.Context-taking helper the walk needs,
// because arch.CheckFileLayout's workflow-in-impl-file rule forbids them in
// deliverymanager.go and its workflow-file-name rule ties this file's name to
// its ONE entry func. That is why the strategy seam, the signal router and the
// four row-accessor helpers moved out of coauthorartifact.go all live here: they
// take a workflow.Context, and this is the file that survives the wave.
// ===========================================================================

// ---------------------------------------------------------------------------
// THE ROW ACCESSOR (stage 4b1 Task 7). ONE copy serves the systemDesign +
// projectDesign + construction rails and the generic child. These take a
// workflow.Context, so the file-layout gate forbids them in deliverymanager.go
// (workflow-in-impl-file); they live HERE rather than in coauthorartifact.go
// because this is the file the wave keeps — leaving them beside the co-author
// spine would have made Task 13's deletion of that file break the build. Their
// non-context half — the fence id, terminalConflictErrType,
// terminalConflictMessage, isTerminalConflict, the rowAccessor struct and its
// key — is in deliverymanager.go with isConflict.
// ---------------------------------------------------------------------------

// withRowAccessor binds acc for the rest of this execution. Called once, by each child's
// entry func, right after its state is built — a pure context derivation, no command, so it
// needs no fence of its own.
func withRowAccessor(ctx workflow.Context, acc rowAccessor) workflow.Context {
	return workflow.WithValue(ctx, rowAccessorKey{}, acc)
}

// rowAccessorFrom returns the binding, or ok=false for the callers that hold NO row and
// must keep the project-version-only behaviour: the pump (pumpnextactivity.go) and the
// supervision workflow's pause record (projectsupervision.go). Both write PROJECT-scoped
// head state and address no activity at all, which is why the fenced arm is GUARDED and
// never nil-called — the pump is the one workflow in this package that cannot fail quietly.
//
// The round sweep (roundsweep.go, Task 6) was PREDICTED to be a third such caller, and it
// is not. It walks every activity of a project, so it holds no single row for the whole
// workflow — but it holds exactly ONE per write, and per-write is the granularity this
// accessor is about, so it binds one per iteration. It has to: the decided-round refusal is
// the terminality Conflict this arm exists for and the only one the sweep can provoke, and
// unbound the sweep would burn twenty attempts on it and fail a whole project's sweep as
// MutateConflictExhausted — the exact defect Task 7 removed everywhere else.
func rowAccessorFrom(ctx workflow.Context) (rowAccessor, bool) {
	acc, ok := ctx.Value(rowAccessorKey{}).(rowAccessor)
	if !ok || acc.activityID == "" || acc.version == nil || acc.setVersion == nil {
		return rowAccessor{}, false
	}
	return acc, true
}

// rereadRowVersion asks the store what version the activity's execution row is at now.
//
// A NotFound reads as projectstate.NoActivityVersionExpectation rather than as a failure:
// a row that does not exist cannot have moved, and a birth legitimately holds no number.
// It reaches here on the FIRST attempt without any option preset, because fwra carries
// Retryable PER ERROR and NotFound is not (framework-go manager.MapError → tagError).
func rereadRowVersion(ctx workflow.Context, acts genInvokers, projectID ProjectID, activityID string) (int64, error) {
	row, err := acts.ActivityExecutionReadActivityExecution(ctx, projectstate.ProjectID(projectID), activityID)
	if err != nil {
		if isReadNotFound(err) {
			return projectstate.NoActivityVersionExpectation, nil
		}
		return 0, err
	}
	return row.Version, nil
}

// terminalAfterRowReread is the Conflict arm's second question, asked after the project
// version has been re-read: is this Conflict a race at all?
//
// Two things the old loop could not tell apart. An EXTERNAL row writer — the pump's
// RecordActivityFailed, an operator note filed through the API, or (from this wave) a
// reviewer resolving a comment on a round a live child holds — bumps the ROW version while
// the PROJECT version the loop re-read may or may not move; and a TERMINAL refusal (an
// exited row, a decided round) moves neither, because there is nothing to move. Re-reading
// the row answers both: it re-seeds the CAS the child holds by hand, and its standing still
// is what proves retrying is pointless.
//
// projectVersionMoved is what the caller already learned from its own re-read, passed in
// because the three loops read the version on three different substrates (main, a session
// branch, the construction head).
//
// GetVersion is called FIRST and before any branch that could skip it, so a new execution
// records the marker deterministically on its first Conflict; a recorded history takes the
// DefaultVersion arm and makes no row read, which is what keeps the nineteen replays green.
func terminalAfterRowReread(ctx workflow.Context, acts genInvokers, projectID ProjectID, projectVersionMoved bool) (bool, error) {
	if workflow.GetVersion(ctx, changeRowConflictReread, workflow.DefaultVersion, 1) < 1 {
		return false, nil
	}
	acc, bound := rowAccessorFrom(ctx)
	if !bound {
		return false, nil
	}
	before := acc.version()
	after, err := rereadRowVersion(ctx, acts, projectID, acc.activityID)
	if err != nil {
		return false, err
	}
	if after == projectstate.NoActivityVersionExpectation && before != projectstate.NoActivityVersionExpectation {
		// A NotFound re-read must NEVER DOWNGRADE a held expectation. The mapping above reads
		// "no row" as NoActivityVersionExpectation, and that value is not just a number:
		// activityVersionMismatch short-circuits on it, so re-seeding a run that HELD a number
		// would switch the per-row CAS OFF for the rest of that run and let it write over every
		// interleaving the guard exists to refuse — silently, and long after the conflict that
		// caused it. So keep `before`, and let the Conflict stay a Conflict: a row that vanished
		// under a live run holding a number for it is not a state to be permissive about, and
		// the loop exhausting its bound is the loud answer. (Rows are never deleted, so this is
		// a "cannot happen" that must not degrade quietly if it does.) Not terminal either —
		// `after != before` here by construction.
		return false, nil
	}
	// Re-seed: whatever the store reports IS the row's version, and the run's hand-advanced
	// copy (rowAdvanced) is the thing that was wrong if they differ.
	acc.setVersion(after)
	if before == projectstate.NoActivityVersionExpectation {
		// A run in the "I have not read this row" posture (a BIRTH) cannot be looking at a
		// row's refusal: both terminality Conflicts are reachable only THROUGH a row the run
		// already read — OpenActivity BIRTHS an absent row rather than refusing it, and the
		// round verbs cannot find a round on a row that is not there (NotFound, not
		// Conflict). So the terminal arm requires a row expectation, and without one the loop
		// retries exactly as it did before. Deliberately asymmetric: a FALSE terminal fails a
		// healthy activity non-retryably, which is the very defect this arm removes.
		return false, nil
	}
	return !projectVersionMoved && after == before, nil
}

// csBindRowAccessor binds the construction activity's ROW onto the child's context. A
// pure context derivation that emits no command and so needs no fence of its own. The
// other two csWorkflows bodies that call applyRecovering — the pump and the supervision
// workflow's pause record — never call this, and that absence IS their guard.
func csBindRowAccessor(ctx workflow.Context, activityID ActivityID, state *constructState) workflow.Context {
	return withRowAccessor(ctx, rowAccessor{
		activityID: string(activityID),
		version:    func() int64 { return state.activityVersion },
		setVersion: func(v int64) { state.activityVersion = v },
	})
}

// ---------------------------------------------------------------------------
// THE STRATEGY SEAM (architect Ruling 3(a), 2026-09-26).
//
// Per-type behaviour is DATA — lifecycles.json already carries kind, phase,
// dependsOn, reviews, command, workerClass and artifactKind, and the phase carries
// its gate and weight. Two things the data cannot carry, and the ruling is explicit
// that pretending otherwise is wrong: how a task's output is PRODUCED, and how its
// subject is STAGED. The interactive design session and the CI poll/variance/override
// loop are two real implementations, and a server-side deterministic computation is a
// third. They sit behind ONE interface the walker never branches on.
//
// THE RULE THE WALKER OBEYS, and the arch test that holds it:
// Test_DeliveryActivityWalker_NamesNoTypeOrCommand parses this file and fails if any
// of the four walker funcs names an ActivityType, an ArtifactKind or a command string.
// The moment a walker grows `if activityType ==`, this is a god-workflow worse than
// the two types it replaced, and §9's smaller-than-sum acceptance is the thing that
// pays for it.
// ---------------------------------------------------------------------------

// taskContext is everything a strategy may read. It is assembled ONCE per task
// attempt by the walker, so a strategy cannot reach back into the walk and cannot
// decide what runs next.
type taskContext struct {
	In    deliveryActivityInput
	Task  methodassets.LifecycleTask
	Phase methodassets.LifecyclePhase
	// Lifecycle is the whole pinned lifecycle, and it is here for ONE reason that the Task
	// and Phase fields cannot serve: a REVIEW task's agent critics judge the artifact of the
	// task its `reviews` field names, and only the lifecycle can answer which that is. A
	// critique dispatch reading tc.Task.ArtifactKind would read "" for every review row in
	// method-assets v0.9.0 except sdpReview.
	//
	// It is NOT a way back into the walk: it is the same immutable platform DATA the walker
	// read to schedule this task, carried by value.
	Lifecycle methodassets.Lifecycle
	Revision  int64
	Attempt   int
	State     *constructState
	Feedback  string
	// Inbox is THIS task's channel, handed to the strategy so a long dispatch can act on an
	// operator's steer while the job runs rather than leaving it to be logged as too-late
	// when the task retires. A strategy that does not poll ignores it (it is nil-safe at
	// every read below).
	Inbox workflow.ReceiveChannel
	// Drain pulls whatever the router overflowed for this task out of ws.pending and into the
	// inbox. It is a CLOSURE rather than the walkState itself, deliberately: a strategy that
	// held ws could schedule, re-open or re-state tasks, and the one thing it legitimately needs
	// is drainPending's guarantee — the router never blocks, so a message beyond the inbox's
	// capacity waits in ws.pending until a RECEIVE LOOP asks for it. A blocking receive that
	// skips this never sees a 65th override. Nil-safe, and nil for a strategy that never blocks
	// on its inbox.
	Drain func()
}

// csIn adapts the walk's input onto the construction-shaped input every ledger, git and
// head-state helper in this package already takes. It is an ADAPTER and not a second
// payload: deliveryActivityInput reuses constructionActivity unrenamed, so the two carry
// the same three facts and this conversion loses nothing.
func (in deliveryActivityInput) csIn() constructActivityInput {
	return constructActivityInput{ProjectID: in.ProjectID, ActivityID: in.ActivityID, Activity: in.Activity}
}

// taskStrategy produces one task's output. Exactly one implementation is registered
// per (task kind × how its subject comes to exist); strategyFor is the only place
// that chooses, and it chooses on lifecycle FIELDS, never on an activity type.
type taskStrategy interface {
	Produce(ctx workflow.Context, tc taskContext) (producedSubject, error)
}

// strategyRegistry maps a SLOT to a strategy constructor. ONE registry for all three
// slots, not two hard-coded arms plus a map — because the shape cases must be able to
// substitute a stub for a slot whose real implementation arrives two tasks later, and a
// hard-coded arm has nowhere to register one. It is a field on csWorkflows
// (wf.Strategies), defaulted by csNewWorkflows so an unwired slice cannot read a nil map,
// and overridden per test.
type strategyRegistry map[string]func(*csWorkflows) taskStrategy

const (
	strategySlotDispatch = "dispatch"
	strategySlotJudged   = "judged"
)

// strategySlotCompute keys the COMPUTE slot by the artifactKind the lifecycle spells,
// so a second computation is a registry entry and not a branch.
func strategySlotCompute(artifactKind string) string { return "compute:" + artifactKind }

// artifactKindSdpReview is the artifactKind the `projectDesign` lifecycle's one review
// task spells (method-assets v0.9.0: `"artifactKind": "SdpReview"`). It is the DATA's
// spelling — upper-camel, which lowerFirstRune maps onto the wire kind `sdpReview` — and
// it is a const here rather than a literal at the registry line so the one place that must
// agree with lifecycles.json says so. Pinned by
// Test_DeliveryStrategies_EveryComputeRowIsRegistered, which walks all fourteen
// lifecycles: a platform release that respells it fails there rather than at a run.
const artifactKindSdpReview = "SdpReview"

// productionStrategies is the registry the composition path uses. Stage 4b1 fills
// dispatch in Tasks 10/11; compute:SdpReview is filled HERE (Task 9). Until the dispatch
// slot lands it REFUSES, naming the task that owes it — a strategy that silently succeeded
// would make an out-of-order execution look like a passing walk.
//
// It TAKES the estimate Engines rather than constructing them (architect ruling R9-5: a
// strategy's engines are threaded through the registry constructor, never reached for as a
// package var), which is also what lets a test drive the compute against a stub Engine.
func productionStrategies(eng sdpEngines) strategyRegistry {
	return strategyRegistry{
		strategySlotDispatch: func(wf *csWorkflows) taskStrategy { return agenticDispatchStrategy{wf: wf} },
		strategySlotJudged:   func(*csWorkflows) taskStrategy { return judgedTaskStrategy{} },
		strategySlotCompute(artifactKindSdpReview): func(wf *csWorkflows) taskStrategy {
			return sdpComputeStrategy{wf: wf, eng: eng}
		},
	}
}

// agenticDispatchStrategy runs ONE dispatch task: submit the agentic job the task's
// `command` names, observe it to a terminal phase, read back what it produced, and stage
// that as the task's output on the activity branch.
//
// It is ONE implementation for both rails, because the rails were already venue-blind: the
// design session dispatched through the SAME agenticJobAccess the construction rail does,
// and the composition root is what chooses GH Actions or local. The command is DATA
// (task.Command), the worker class is DATA (task.WorkerClass), and the artifact kind is DATA
// (task.ArtifactKind).
//
// THE PER-RAIL SURFACE, and it is two things rather than the plan's one. The plan predicted
// only the staging CODEC would differ — a design task's output is a slot MODEL read back off
// the branch, a construction task's is a commit the agent already pushed. Measured, the
// WORKFLOW FILE differs too: a design job runs aiarch-design.yml in the project repo and a
// construction job aiarch-construct.yml, and the Manager is what names both
// (designWorkflowFileName / constructWorkflowFileName). The plan's comment says "there is no
// aiarch-*.yml name anywhere in the design path"; there is one, at coauthorartifact.go:2418.
// So the strategy chooses TWO things on the artifact kind, not one — and the artifact kind is
// still data, so nothing here reads an activity type.
//
// BOTH ARMS ARE NOW HERE (stage 4b1 Task 11 filled the construction half). The split is ONE
// question asked of the DATA — does this task's artifact kind name a design slot? — and
// designSlotOfTask is the whole of it. A task that names no slot is a construction task: its
// output is a commit the agent pushed, so it stages NOTHING and its empty StagedRef lets
// gateSubjectRef fall to its second rung (the PR) or its third (the attempt), exactly as the
// retired rail's rounds did.
type agenticDispatchStrategy struct{ wf *csWorkflows }

func (s agenticDispatchStrategy) Produce(ctx workflow.Context, tc taskContext) (producedSubject, error) {
	if kind, ok := designSlotOfTask(tc.Lifecycle, tc.Task); ok {
		return s.wf.produceDesignArtifact(ctx, tc, kind)
	}
	return s.wf.produceConstructionChange(ctx, tc)
}

// designSlotOfTask answers the ONE question that splits the two rails, off the DATA: does
// this task's artifact kind name a design SLOT — a model this platform's own store holds —
// or a build output that lives as a commit?
//
// The test is total and needs no table: projectstate.AllArtifactKinds is exactly the
// seventeen design slots, so `Mission`/`Glossary`/`Volatilities`/`CoreUseCases`/`System`
// resolve and `SRS`/`DetailedDesign`/`STP`/`Construction`/`Integration` — every construction
// lifecycle's kinds — do not. A platform release that added a design lifecycle would be
// carried automatically; one that respelled a construction kind into a slot name would fail
// Test_DesignSlotOfTask_SplitsTheRailsOnTheData rather than misroute a build.
//
// A REVIEW task is resolved through its `reviews` target, which is why the lifecycle is
// taken: the critique of a mission draft stages nothing, but it IS a mission-kind job, and
// its command slug and job mode both hang off that kind.
func designSlotOfTask(lc methodassets.Lifecycle, t methodassets.LifecycleTask) (projectstate.ArtifactKind, bool) {
	k := roundArtifactKind(lc, t)
	if k == nil {
		return 0, false
	}
	return *k, true
}

// ---------------------------------------------------------------------------
// THE DESIGN ARM (stage 4b1 Task 10)
//
// This is the co-author session's command sequence, reproduced inside the generic child
// against the same agenticJobAccess the construction rail uses. What it deliberately does
// NOT reproduce, and why, is written at each site:
//
//   - beginSession / mintCred / RailOpenBranch are GONE from the per-task path: the child
//     opens its branch and PR ONCE, at openActivityRow, before the first task runs. The
//     retired session opened a branch per KIND, because a kind was a whole workflow.
//   - the RESUME PROBE is gone. readBackCommittedModelOn was re-run first when
//     state.resumeFromReadBack was set, and the setter this arm removes is openPR faulting
//     AFTER the read-back: the child opens its branch and PR ONCE, at openActivityRow, before
//     any dispatch, so there is no post-read-back rail step left to fault into that marker.
//     THE FIRST DRAFT OF THIS COMMENT CLAIMED THE MARKER HAD NO OTHER SETTER, AND THAT WAS
//     FALSE (Task 10 review, D): the marker's own doc names ANY critique-round fault as a
//     second one. What replaces that half is the arm below — a critique fault holds the gate
//     for a human instead of arming a probe — and what replaces neither is a crash
//     mid-dispatch, which is carried as an earmark rather than silently.
//   - the AMENDMENT no-change guard is gone with the amendment branch scheme it defended
//     (R12 retires `-amend-N` in 4b2); a send-back's redraft runs on the same activity branch
//     and re-stages, which the round's own subject ref is what makes visible.
//   - StageDraftFailed is gone: the child has no per-session human recovery gate, so a
//     terminal DRAFT failure records a FAILED attempt and fails the walk with the diagnostic,
//     exactly as Task 9's compute failure does. The operator's repair is Task 12's. A terminal
//     CRITIQUE failure is a different thing and takes the never-crash arm — see
//     produceDesignArtifact.
// ---------------------------------------------------------------------------

// produceDesignArtifact is one design task's whole production: submit the job its command
// names, observe it on the shared ladder, read back what it committed on the activity
// branch, and stage that as this task's output.
//
// A DISPATCH task drafts and stages a MODEL. A REVIEW task's job is its agent critics, which
// commit a critique VERDICT and no model — so it stages nothing and reports the verdict
// through the producedSubject's Outcome, which runAgentReviewers turns into a round verdict.
// One function, because the two differ in the read-back alone.
//
// A FAILED CRITIQUE NEVER FAILS THE WALK (Task 10 review, F1), and this is the retired rail's
// whole never-crash discipline: coauthorartifact.go routed ALL THREE critique faults — a
// terminal job failure, a rejected dispatch, a missing verdict — to the human gate, and only
// the third of them survived the first draft of this arm. The defect that made it is not
// theoretical and it is unrecoverable: a red `mission-critique` run failed the child, the pump
// propagated the error with NO head-state record, the row already had StartedAt so
// PumpWroteRow answered true, and isActivityDispatchable then answered false FOREVER — no gate
// to answer, no re-open, and nothing in the ledger saying why. So a critique that did not
// succeed returns a FAILED producedSubject with a NIL error: criticVerdictFor maps it to an
// abstention with judged=false, and runGate holds the gate for a human whatever the review
// policy says. The diagnostic rides the attempt and the round's verdict, so what happened is
// on the record; what does not happen is the activity dying.
func (wf *csWorkflows) produceDesignArtifact(
	ctx workflow.Context, tc taskContext, kind projectstate.ArtifactKind,
) (producedSubject, error) {
	attemptID := projectstate.AttemptID(string(tc.In.ActivityID), projectstate.MethodTask(tc.Task.ID), tc.Attempt)
	critique := tc.Task.Kind == methodassets.LifecycleTaskReview
	obs, err := wf.runDesignJob(ctx, tc, kind)
	if err != nil {
		// A REJECTED DISPATCH is the second of the three faults: an unresolvable repo target, an
		// empty pipeline handle, a submit the venue refused. For a critique it holds the gate; for
		// a draft it fails the walk, because there is no artifact and no gate that could stand in
		// for one.
		return designFaultSubject(ctx, tc, attemptID, err.Error(), critique), critiqueSafeError(err, critique)
	}
	if obs.Phase != PipelineSucceeded {
		// The job RAN and FAILED (the draft failed, or the required CI check went red). Inferring
		// success from a non-success phase is the one thing this arm must never do (§0d.4's
		// anti-wedge rule, kept as its loud half) — but for a critique "loud" means a held gate,
		// not a dead activity.
		detail := dispatchJobFailedDetail(jobLabelDesign, tc.Task.ID, obs)
		return designFaultSubject(ctx, tc, attemptID, detail, critique),
			critiqueSafeError(temporal.NewNonRetryableApplicationError(detail, "DesignJobFailed", nil), critique)
	}
	if critique {
		return wf.readBackCritique(ctx, tc, kind, attemptID)
	}
	return wf.stageDesignDraft(ctx, tc, kind, attemptID)
}

// designFaultSubject is the FAILED producedSubject a design fault leaves behind, and it logs
// the critique arm at Warn so a held gate is not a silent one: the operator sees an activity
// waiting for them, and the log says the critic never answered.
func designFaultSubject(ctx workflow.Context, tc taskContext, attemptID, detail string, critique bool) producedSubject {
	if critique {
		workflow.GetLogger(ctx).Warn("the critique job did not answer; the gate will hold for a human rather than failing the activity",
			"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "detail", detail)
	}
	return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeFailed, Detail: detail}
}

// critiqueSafeError swallows a design fault's error for a CRITIQUE and returns it for a DRAFT.
// It is one function rather than two arms at two call sites so the rule cannot be applied at
// one fault and forgotten at the other, which is exactly how F1's gap was made.
func critiqueSafeError(err error, critique bool) error {
	if critique {
		return nil
	}
	return err
}

// The two job LABELS the shared dispatch machinery names in an operator-facing sentence.
// They are the only thing that differs between the two arms' diagnostics, which is why they
// are two strings rather than two copies of the loop.
const (
	jobLabelDesign       = "design"
	jobLabelConstruction = "construction"
)

// dispatchJobFailedDetail is the one sentence a failed agentic job leaves behind, naming the
// task, the phase it reached and whatever the venue said.
func dispatchJobFailedDetail(label, taskID string, obs csPipelineObservation) string {
	detail := "the " + label + " job for task " + taskID + " reached terminal phase " + pipelinePhaseLabel(obs.Phase)
	if obs.Diagnostic != "" {
		detail += ": " + obs.Diagnostic
	}
	if obs.RunURL != "" {
		detail += " (" + obs.RunURL + ")"
	}
	return detail
}

// pipelinePhaseLabel names a pipeline phase for a HUMAN — the attempt's Detail, a log line —
// because a bare ordinal in an operator-facing sentence is a fact nobody can act on. The
// generated PipelinePhase carries no Stringer (contract.gen.go), and the schema-first rule
// keeps enum types method-free, so the naming lives beside its one caller. Exhaustive, so a
// new phase must be named rather than printed as a number.
func pipelinePhaseLabel(p PipelinePhase) string {
	switch p {
	case PipelinePending:
		return "pending"
	case PipelineRunning:
		return "running"
	case PipelineSucceeded:
		return "succeeded"
	case PipelineFailed:
		return "failed"
	case PipelineCancelled:
		return "cancelled"
	case PipelinePhaseUnknown:
		return "unknown"
	}
	return "unknown"
}

// stageDesignDraft is a DISPATCH task's read-back and staging: the typed model the job
// committed on the activity branch, staged into its slot so the reviewer reads the
// not-yet-merged draft.
func (wf *csWorkflows) stageDesignDraft(
	ctx workflow.Context, tc taskContext, kind projectstate.ArtifactKind, attemptID string,
) (producedSubject, error) {
	_, branch := wf.designVenue(tc.In.ProjectID, tc.In.ActivityID)
	model, branchVersion, err := wf.readBackDesignModelOn(ctx, tc.In.ProjectID, kind, branch)
	if err != nil {
		return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeFailed, Detail: err.Error()}, err
	}
	ref, err := wf.stageDesignOutput(ctx, tc, kind, model, branchVersion)
	if err != nil {
		return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeFailed, Detail: err.Error()}, err
	}
	return producedSubject{
		StagedRef: ref, AttemptID: attemptID, Outcome: projectstate.OutcomePassed,
		Detail: "drafted " + kind.WireName() + " on " + designBranchLabel(branch),
	}, nil
}

// designBranchLabel names where a draft landed, for the attempt's Detail. A dormant venue
// reads "main" rather than an empty string, because "" in a human-facing detail is a fact
// nobody can act on.
func designBranchLabel(branch string) string {
	if branch == "" {
		return mainBranch
	}
	return branch
}

// readBackCritique is a REVIEW task's read-back: the critique verdict its critics committed
// on the activity branch, mapped onto the producedSubject Outcome runAgentReviewers reads.
//
// THE SAFE DEFAULT IS KEPT, and it is the whole reason this is not three lines: a critique job
// that reported SUCCESS and committed NO verdict is a ran-but-incomplete job, and reading its
// silence as an approve would let an unreviewed draft sail to the human gate as if the critic
// had ratified it (coauthorartifact.go's readBackCritiqueOn argues this at length). So the
// empty carrier is OutcomeFailed carrying the diagnostic, which runAgentReviewers records as
// an abstention AND — because the critic did not judge — holds the gate for a human whatever
// the review policy says. NEVER an approve.
func (wf *csWorkflows) readBackCritique(
	ctx workflow.Context, tc taskContext, kind projectstate.ArtifactKind, attemptID string,
) (producedSubject, error) {
	_, branch := wf.designVenue(tc.In.ProjectID, tc.In.ActivityID)
	proj, err := wf.readProjectOnBranch(ctx, tc.In.ProjectID, branch)
	if err != nil {
		return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeFailed, Detail: err.Error()}, err
	}
	slot := slotForKind(proj, kind)
	switch slot.CritiqueVerdict {
	case projectstate.CritiqueVerdictApprove:
		// The notes ride an APPROVE too: the critique prompt's verdict discipline records
		// taste-level reservations as comments ON an approve, and dropping them would show the
		// founder a bare verdict with the rationale erased.
		return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomePassed, Detail: slot.CritiqueNotes}, nil
	case projectstate.CritiqueVerdictRevise:
		return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeRejected, Detail: slot.CritiqueNotes}, nil
	}
	return producedSubject{
		AttemptID: attemptID, Outcome: projectstate.OutcomeFailed,
		Detail: "the critique job for task " + tc.Task.ID + " reported success but committed no critique verdict for " +
			kind.WireName() + " — the read-back carrier is empty, so nothing judged this draft",
	}, nil
}

// designVenue resolves WHERE a design job runs and WHERE its output lands: the opaque
// per-project RepoRef the job dispatches into, and the branch it commits on.
//
// IT IS THE CHILD'S ONE NEW wf.Repo READER, and that is a deliberate exception to R6/R8-22
// ("the child adds no new wf.Repo reader"), because the two rails give the resolver two
// answers and a design job cannot take construction's. csWorkflows.gitEnabled — which every
// other reader in the child routes through — additionally asks wf.RailEnabled, and
// railLifecycleEnabled answers FALSE for the deterministic GitLocal ref: correct for
// construction (its PR rail has never run against a filesystem venue, and the local merge job
// owns that merge) and wrong for design, whose branch → PR → merge lifecycle is exactly what
// that ref runs. Routing the design dispatch through gitEnabled would send it to the CENTRAL
// construction repo on every local boot — the wrong repo AND the wrong workflow file.
//
// The BRANCH is activityBranchName for BOTH rails (R12): spec §5.3 unifies on
// activity/{activityId}, which is the branch openActivityRow already opened when the PR rail
// is live, and which the design job creates on a local venue exactly as a construction job
// does. The per-artifact design-branch resolver that used to name the other branch is DELETED
// (stage 4b2) — that was the one release it survived unused for.
//
// An unresolvable project answers ("", "") — the dormant path: the RA falls back to its
// configured repo and the read-back/stage ride main, which is byte-for-byte the retired
// rail's dormant behaviour.
func (wf *csWorkflows) designVenue(projectID ProjectID, activityID ActivityID) (repo string, branch string) {
	if wf.Repo == nil {
		return "", ""
	}
	ref, ok := wf.Repo(projectID)
	if !ok {
		return "", ""
	}
	return sourcecontrol.RepoRefString(ref), activityBranchName(activityID)
}

// designJobMode is the job_mode discriminator, read off the TASK KIND: a dispatch task drafts,
// a review task's own command critiques. That is the whole of what dispatchTarget carried on
// the retired rail, and it is now one field of the lifecycle data rather than an enum the
// Manager chose.
func designJobMode(t methodassets.LifecycleTask) string {
	if t.Kind == methodassets.LifecycleTaskReview {
		return jobModeCritique
	}
	return jobModeDraft
}

// runDesignJob submits the design job and observes it to a terminal phase.
//
// THE COMMAND IS THE LIFECYCLE'S (tc.Task.Command), not DesignCommandFor's. Both agree today
// — Test_DesignCommands_MatchTheLifecycleData pins that — but the data is the source of
// truth for what a task runs, and re-deriving it here would give the platform two answers to
// keep in step.
func (wf *csWorkflows) runDesignJob(
	ctx workflow.Context, tc taskContext, kind projectstate.ArtifactKind,
) (csPipelineObservation, error) {
	handle, err := wf.submitDesignJob(ctx, tc, kind)
	if err != nil {
		return csPipelineObservation{}, err
	}
	task := projectstate.MethodTask(tc.Task.ID)
	return wf.observeDispatchedJob(ctx, tc, handle, task, tc.Attempt, jobLabelDesign, nil), nil
}

// submitDesignJob composes and submits ONE design job: the five dispatch inputs the seated
// aiarch-design.yml declares, the per-project repo target, and that workflow file.
func (wf *csWorkflows) submitDesignJob(
	ctx workflow.Context, tc taskContext, kind projectstate.ArtifactKind,
) (pipelineHandle, error) {
	repo, branch := wf.designVenue(tc.In.ProjectID, tc.In.ActivityID)
	target, terr := designRepoTarget(repo)
	if terr != nil {
		return pipelineHandle{}, terr
	}
	spec := agenticjob.PipelineSpec{
		ProjectID: agenticjob.ProjectID(tc.In.ProjectID),
		// A non-empty, well-formed step graph satisfies the RA's §2.1 pre-condition; the design
		// recipe lives in the project's own workflow file, so the step is a placeholder and the
		// DESIGN parameters ride on DispatchInputs.
		Steps: []agenticjob.PipelineStep{{
			Name:      "design",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: map[string]string{
			dispatchInputArtifactKind:  kind.WireName(),
			dispatchInputCommand:       tc.Task.Command,
			dispatchInputTargetBranch:  branch,
			dispatchInputPriorStateRef: "",
			dispatchInputJobMode:       designJobMode(tc.Task),
		},
		TargetRepo: target,
	}
	if repo != "" {
		spec.WorkflowFile = designWorkflowFileName
	}
	handle, err := wf.Acts.PipelineSubmitAgenticJob(ctx, spec)
	if err != nil {
		return pipelineHandle{}, err
	}
	if agenticjob.PipelineHandleIsZero(handle) {
		return pipelineHandle{}, temporal.NewNonRetryableApplicationError(
			"the design dispatch for task "+tc.Task.ID+" returned an empty pipeline handle", "EmptyPipelineHandle", nil)
	}
	return pipelineHandle{Name: agenticjob.PipelineHandleString(handle)}, nil
}

// observeDispatchedJob polls the job to a terminal phase on the ONE ladder both rails share,
// draining this task's inbox between polls, and NEVER infers success from exhaustion: a stuck
// job comes back as an explicit PipelineFailed with a neutral diagnostic.
//
// ONE LOOP FOR BOTH ARMS (stage 4b1 Task 11). The design and construction observe loops
// differed in exactly two things, and both are parameters here: the LABEL an operator-facing
// sentence names, and whether the PR's CI rollup is mirrored onto the head-state on the same
// cadence (gf — the construction rail's poll-loop verb, D-PA-GIT §5; nil, or a dormant
// forward, for the design arm and for the local profile). Everything else — the ladder, the
// episode capture, the exhaustion synthetic, the inbox rule — was already identical prose in
// two places.
//
// THE MISROUTE RULE (Task 8 review finding 1, written at drainPending). This loop receives on
// the task's own inbox while it waits, because a twenty-minute dispatch is exactly when an
// operator steers. What it can act on it acts on; what it cannot it parks in a LOCAL
// `deferred` slice and re-offers to the inbox when the loop ends — NEVER back into ws.pending,
// because drainPending runs at the top of every receive loop and would re-offer it
// immediately, and each iteration here builds a fresh workflow.NewTimer: the loop would spin,
// growing durable history as fast as it can loop, and the history budget would trip on an
// activity that did nothing. Re-offering at the end is what makes closeInbox report them as
// undelivered rather than losing them silently.
//
// A FAILED OBSERVE — of the job OR of the PR's CI rollup — is a warning and not a terminal.
// The design arm already read it that way; the retired construction loop RETURNED the error,
// which failed the whole activity over a single transient read of a venue it had already
// dispatched into — and (because runAttempt propagated it) left the activity stuck Running
// with no terminal record at all. One rule for both arms, and it is the safer one: a read of
// the venue is not the job's verdict, and the ladder's bound is what decides.
func (wf *csWorkflows) observeDispatchedJob(
	ctx workflow.Context, tc taskContext, handle pipelineHandle,
	task projectstate.MethodTask, attempt int, label string, gf *gitForward,
) csPipelineObservation {
	var (
		last     csPipelineObservation
		deferred []routedSignal
		// steered is the override COALESCING flag, and it is per DISPATCH (fix round 1). See
		// drainInboxWhileDispatching for the storm it bounds.
		steered bool
	)
	for poll := range maxObserveTotalPolls {
		obs, err := wf.observePipeline(ctx, handle)
		if err != nil {
			// A read of the venue is not the job's verdict. Treat a failed observe as
			// non-terminal and let the ladder's bound decide, rather than failing an activity
			// over one poll.
			workflow.GetLogger(ctx).Warn("observing the "+label+" job failed; the ladder keeps polling",
				"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "err", err.Error())
		} else {
			ph := obs.Phase
			tc.State.pipelinePhase = &ph
			wf.mirrorCIRollup(ctx, tc, gf)
			if obs.Phase == PipelineSucceeded || obs.Phase == PipelineFailed {
				wf.captureEpisode(ctx, tc.In.csIn(), handle, obs, true, task, attempt)
				wf.reofferDeferred(ctx, tc, deferred)
				return obs
			}
			last = obs
		}
		deferred, steered = wf.drainInboxWhileDispatching(ctx, tc, deferred, steered)
		_ = workflow.Sleep(ctx, observeInterval(poll))
	}
	exhausted := csPipelineObservation{
		Phase:      PipelineFailed,
		Diagnostic: "the " + label + " job did not reach a terminal phase within the observation window",
		RunURL:     last.RunURL,
		Episode:    last.Episode,
	}
	// The stuck job still burned tokens, so it still owes the ledger a record (a gap when
	// nothing was mined) — never silent.
	wf.captureEpisode(ctx, tc.In.csIn(), handle, exhausted, true, task, attempt)
	wf.reofferDeferred(ctx, tc, deferred)
	return csPipelineObservation{Phase: exhausted.Phase, Diagnostic: exhausted.Diagnostic}
}

// mirrorCIRollup reads the PR's CI rollup once and mirrors it onto the head-state, on the
// observe cadence — the construction rail's own poll-loop verb, reached through the SAME
// helper it always used. A nil or dormant forward is a no-op, which is the design arm and
// every local profile.
//
// A failure is logged and swallowed for the reason observeDispatchedJob's doc gives: the
// mirror is a decoration on a head-state row, and the retired loop's returned error failed the
// activity over it.
func (wf *csWorkflows) mirrorCIRollup(ctx workflow.Context, tc taskContext, gf *gitForward) {
	if gf == nil || !gf.enabled {
		return
	}
	if _, err := wf.observeCIAndRecord(ctx, tc.In.csIn(), gf, &tc.State.walk.headVersion); err != nil {
		workflow.GetLogger(ctx).Warn("mirroring the PR's CI rollup failed; the ladder keeps polling",
			"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "err", err.Error())
	}
}

// drainInboxWhileDispatching takes whatever is waiting on this task's inbox, NON-BLOCKING,
// and returns the messages this loop could not handle appended to deferred, plus whether an
// override has now been recorded for this dispatch.
//
// It handles exactly ONE kind: an operator OVERRIDE, which is recorded as the operator note
// the construction rail already records for one, so a steer sent during a twenty-minute draft
// lands in the ledger instead of being logged as too-late twenty minutes later. A decision or
// a redraft belongs to the gate that has not opened yet; it is deferred, not answered.
//
// ONE OVERRIDE PER DISPATCH, and the rest are logged too-late (fix round 1). MEASURED before the
// bound existed: 65 overrides delivered to a live dispatch became 65 SEQUENTIAL
// recordOperatorNote writes from one coroutine while a sibling fork branch wrote its own
// attempts, and the per-activity CAS genuinely exhausted — MutateConflictExhausted, an activity
// killed by an operator pressing a button repeatedly. The bound is the honest reading as well as
// the safe one: an override is a DECISION about the dispatch in flight, a second one says nothing
// new about it, and the operator's next decision belongs to the next attempt. The façade caps one
// note's SIZE (maxOperatorNoteBodyBytes) and nothing capped the RATE.
func (wf *csWorkflows) drainInboxWhileDispatching(
	ctx workflow.Context, tc taskContext, deferred []routedSignal, steered bool,
) ([]routedSignal, bool) {
	if tc.Inbox == nil {
		return deferred, steered
	}
	// The overrides of THIS pass, collected before ANY of them is written — see
	// recordGateOverrides. Collecting first is the whole fix: writing inside the receive loop is
	// what made a storm one CAS write per signal.
	var storm []*operatorOverrideSignal
	for {
		var msg routedSignal
		if !tc.Inbox.ReceiveAsync(&msg) {
			break
		}
		if !routedPayloadPresent(msg) {
			workflow.GetLogger(ctx).Error("a routed signal reached a dispatch with no payload for its kind; dropped",
				"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "kind", msg.Kind)
			continue
		}
		if msg.Kind == routedKindOverride {
			storm = append(storm, msg.Override)
			continue
		}
		deferred = append(deferred, msg)
	}
	switch {
	case len(storm) == 0:
		return deferred, steered
	case steered:
		// A LATER pass's overrides are too late for a dispatch that already carries one: the job
		// has been steered, and the operator's next decision belongs to the next attempt.
		workflow.GetLogger(ctx).Info("this dispatch already carries an operator override; the later ones are too late for it",
			"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "dropped", len(storm))
		return deferred, steered
	}
	wf.recordGateOverrides(ctx, tc, storm)
	return deferred, true
}

// recordGateOverrides writes the overrides one drain pass collected as ONE note PER NOTE KIND,
// with their texts in one body.
//
// THE DEFECT IT FIXES, reproduced twice (Task 11 review, finding 3 — first measured as a test
// artifact and then reproduced as a real one): a flood of overrides landing inside a drain window
// became one recordOperatorNote per signal, each a per-activity CAS write from this task's
// coroutine, while a sibling fork branch wrote its own attempts — and the CAS genuinely exhausted
// (MutateConflictExhausted), an activity killed by an operator pressing a button repeatedly. The
// façade caps one note's SIZE and nothing capped the RATE.
//
// GROUPING BY KIND rather than writing one note for all of them keeps the record readable: the
// note's Kind is what operatorNoteKindName renders and what PendingOperatorNotes reads to decide
// what rides the next dispatch, so a retry and a skip must not be folded into one note claiming to
// be either. Within a kind the texts join with the same separator renderOperatorNotes uses between
// notes, so the agent reads them exactly as it would have read several. FIRST-SEEN kind order, so
// the writes are deterministic under replay.
func (wf *csWorkflows) recordGateOverrides(ctx workflow.Context, tc taskContext, storm []*operatorOverrideSignal) {
	var order []projectstate.OperatorNoteKind
	byKind := map[projectstate.OperatorNoteKind]noteFeedback{}
	for _, sig := range storm {
		if sig == nil {
			continue
		}
		kind, ok := overrideNoteKind(sig.Override.Kind)
		if !ok {
			workflow.GetLogger(ctx).Error("an override with no note kind reached a dispatch; nothing recorded",
				"activityId", tc.In.ActivityID, "taskId", tc.Task.ID)
			continue
		}
		held, seen := byKind[kind]
		if !seen {
			order = append(order, kind)
		} else if held.text != "" && sig.Override.Notes != "" {
			held.text += notesSeparator
		}
		held.text += sig.Override.Notes
		held.comments = append(held.comments, sig.Override.Comments...)
		byKind[kind] = held
	}
	for _, kind := range order {
		if err := wf.recordOperatorNote(ctx, tc.In.csIn(), tc.State, &tc.State.walk.headVersion,
			tc.State.walk.cred, kind, tc.Task.ID, byKind[kind]); err != nil {
			workflow.GetLogger(ctx).Error("the override could not be recorded; the dispatch runs on",
				"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "err", err.Error())
		}
	}
}

// reofferDeferred puts the messages the dispatch could not answer back on the task's inbox,
// once, as the loop ends. SendAsync and no retry: the inbox has just been drained by this very
// loop so there is room, and a message that still does not fit is one closeInbox would have
// reported anyway. It is the ONLY correct destination — see drainInboxWhileDispatching for why
// ws.pending would spin the loop it came from.
func (wf *csWorkflows) reofferDeferred(ctx workflow.Context, tc taskContext, deferred []routedSignal) {
	if len(deferred) == 0 {
		return
	}
	ch, ok := tc.Inbox.(workflow.Channel)
	if !ok {
		return
	}
	for _, msg := range deferred {
		if !ch.SendAsync(msg) {
			workflow.GetLogger(ctx).Info("a signal deferred during dispatch did not fit back on the inbox; dropped",
				"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "kind", msg.Kind)
		}
	}
}

// readBackDesignModelOn reads the typed model the job committed, on the activity branch when
// there is one and on main when the venue is dormant. An EMPTY slot after a job that reported
// success is a contract violation between the job and the read-back, and it is terminal: a
// retry cannot conjure a model the job never wrote.
//
// It returns the read-back substrate's own Version alongside the model, and that second
// return is load-bearing (QA F29): the branch's version and main's are two different
// documents, so the stage that follows must expect the BRANCH's — a fresh walk re-using a
// branch that already carries commits sees it advanced, and staging against a main-captured
// number would Conflict on its first attempt every time.
func (wf *csWorkflows) readBackDesignModelOn(
	ctx workflow.Context, projectID ProjectID, kind projectstate.ArtifactKind, branch string,
) (projectstate.ArtifactModel, projectstate.Version, error) {
	proj, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		// QA F36, RE-WIRED: the committed draft READS BACK MALFORMED. CI validated it green
		// (its Go mirror types the offending enum as a free string) and the server codec then
		// refuses the value, so the fault is neither infrastructure nor a CI failure and must
		// not read as either. projectstate.ReadBackDecodeFailedReason owns that wording and
		// had NO production caller left after the co-author files went — the guard had
		// degraded to a raw codec error on a failed walk. It is a walk terminal rather than a
		// human gate here (the child has no gate at a dispatch task's read-back), but it is a
		// NAMED one carrying the decode diagnostic, which is what a retry needs to be worth
		// running.
		if isRAContractMisuse(err) || isDecodeFault(err) {
			return nil, 0, temporal.NewNonRetryableApplicationError(
				projectstate.ReadBackDecodeFailedReason(err.Error()), "ReadBackDecodeFailed", nil)
		}
		return nil, 0, err
	}
	slot := slotForKind(proj, kind)
	if slot.Model == nil {
		return nil, 0, temporal.NewNonRetryableApplicationError(
			"the design job reported success but committed no "+kind.WireName()+" model to read back on "+
				designBranchLabel(branch), "ReadBackEmpty", nil)
	}
	return slot.Model, proj.Version, nil
}

// readProjectOnBranch is the child's branch-aware whole-aggregate read. wf.readProject is the
// same call pinned to main; a design read-back must reach the branch the job committed on.
func (wf *csWorkflows) readProjectOnBranch(
	ctx workflow.Context, projectID ProjectID, branch string,
) (projectstate.Project, error) {
	env, err := wf.Acts.DesignSessionReadProjectOnBranch(ctx, projectstate.ProjectID(projectID), branch)
	if err != nil {
		return projectstate.Project{}, err
	}
	proj, derr := env.Decode()
	if derr != nil {
		// A DECODE IS A NAMED TERMINAL, not an anonymous one. The envelope came off committed
		// state, so a decode failure means the STORED document does not type — a retry cannot
		// change that, and the caller needs to be able to tell this apart from an
		// infrastructure read fault (QA F36). Naming it is what lets readBackDesignModelOn
		// render the human "why" for it.
		return projectstate.Project{}, temporal.NewNonRetryableApplicationError(
			"the committed project state did not decode: "+derr.Error(), decodeFaultErrType, nil)
	}
	return proj, nil
}

// stageDesignOutput stages one design task's read-back model as the task's output and returns
// the ref its review round cites.
//
// It goes through activityExecutionAccess.StageTaskOutput — the execution ledger's own verb,
// the same one Task 9's compute stages through — and NOT through the three designSessionAccess
// verbs the retired session used. That re-homing is what leaves stage 4b2 a facet it can
// delete rather than a rail with two owners.
//
// state.rowAdvanced() is deliberately NOT called, for the reason StageTaskOutput's own doc
// gives: it is the one verb on the facet that asserts no per-activity version, because the
// write lands on the branch while the row and its counter live on main.
//
// AND NEITHER IS state.walk.headVersion ADVANCED, unless the venue is dormant. headVersion is
// the run's belief about MAIN; a branch write returns the BRANCH's next number, and storing
// that as main's would make the next main write (the round open, the verdict, the commit) CAS
// against a number from another document. When the branch is "" the two are the same document,
// and then the advance is exactly the read-your-writes seed every other write here keeps.
func (wf *csWorkflows) stageDesignOutput(
	ctx workflow.Context, tc taskContext, kind projectstate.ArtifactKind,
	model projectstate.ArtifactModel, branchVersion projectstate.Version,
) (string, error) {
	env, encErr := encodeModel(model)
	if encErr != nil {
		return "", fwmanager.MapError(encErr)
	}
	_, branch := wf.designVenue(tc.In.ProjectID, tc.In.ActivityID)
	in := tc.In
	state := tc.State
	var staged projectstate.StagedRef
	v, err := wf.applyRecoveringOnBranch(ctx, in.ProjectID, branch, branchVersion,
		func(expected projectstate.Version) (projectstate.Version, error) {
			sr, sErr := wf.Acts.ActivityExecutionStageTaskOutput(ctx, projectstate.ProjectID(in.ProjectID), expected,
				state.activityVersion, string(in.ActivityID), tc.Task.ID, branch, env, state.walk.cred.toProjectState())
			if sErr != nil {
				return 0, sErr
			}
			staged = sr
			return sr.Version, nil
		})
	if err != nil {
		return "", err
	}
	if branch == "" {
		state.walk.headVersion = v
	}
	return stagedRefString(staged, kind), nil
}

// applyRecoveringOnBranch is applyRecovering for a write that lands on a BRANCH: the same
// Conflict re-read→re-apply discipline, except the re-read asks that branch for its version
// rather than main for its own. Seeding a branch retry from main's number is how a stage that
// Conflicts once Conflicts twenty times and dies MutateConflictExhausted.
//
// branch == "" delegates, because then the branch IS main and one loop is better than two.
//
// IT DOES NOT CARRY TASK 7's TERMINALITY ARM, and the omission is deliberate rather than
// missed (Task 10 review, D). terminalAfterRowReread asks the ACTIVITY'S EXECUTION ROW whether
// it moved, and that row — with its per-activity counter — lives on MAIN: a branch write that
// Conflicts has nothing to do with the row's version, so re-seeding the run's copy of it from
// here would hand a main-scoped CAS a number a branch write produced, and "the row stood still"
// would read as terminal for a Conflict the row was never part of. That is the FALSE terminal
// the arm itself exists to prevent, arriving through the other door. Nor is the arm's other
// half reachable: both terminality Conflicts (an exited row, a decided round) are raised by
// row verbs, and the only verb that lands here is StageTaskOutput, which asserts no row
// version at all.
//
// THE CONSEQUENCE, recorded: a branch store that REFUSES this write for a reason that is not a
// race burns the whole bound and dies MutateConflictExhausted, where a main-scoped write would
// have been told so on its second attempt. The bound is maxMutateConflictAttempts and the
// diagnostic names the branch.
func (wf *csWorkflows) applyRecoveringOnBranch(
	ctx workflow.Context,
	projectID ProjectID,
	branch string,
	seed projectstate.Version,
	apply func(expected projectstate.Version) (projectstate.Version, error),
) (projectstate.Version, error) {
	if branch == "" {
		return wf.applyRecovering(ctx, projectID, seed, apply)
	}
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"branch head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		proj, rerr := wf.readProjectOnBranch(ctx, projectID, branch)
		if rerr != nil {
			if isReadNotFound(rerr) {
				expected = 0
				continue
			}
			return 0, rerr
		}
		expected = proj.Version
		workflow.GetLogger(ctx).Info("branch head-state conflict; re-read the branch version and retrying",
			"branch", branch, "attempt", attempt+1, "nextExpectedVersion", expected)
	}
}

// ---------------------------------------------------------------------------
// THE CONSTRUCTION ARM (stage 4b1 Task 11)
//
// This is walkPhases' per-phase dispatch, reproduced for ONE lifecycle task instead of one
// flat phase, through the SAME helpers: submitCarryingNotes (the operator's steer rides the
// dispatch), submitPipeline → constructRepoTarget (the venue, where isGitLocalVenue
// recognition already lives — R6/R-D: no new wf.Repo reader), the shared observe ladder with
// the PR's CI mirror on it, and the pending→resolved attempt pair on the execution ledger.
//
// WHAT IT DELIBERATELY DOES NOT REPRODUCE, and where each went:
//
//   - the FLAT WALK. walkPhases iterated in.Activity.Phases from index 0 and the run's
//     completedPhases skip-guard is what stopped a variance retry re-dispatching a finished
//     phase. The DAG replaces both: a task runs when its dependsOn have passed, and a passed
//     task is never ready again. in.Activity.Phases is not read at all (Step 1's finding: the
//     field's only readers were walkPhases and its own ProfileFor fallback).
//   - ProfileFor. The phase a dispatch names is tc.Phase.ID — the lifecycle's own phase id,
//     which is the SAME wire string ActivityMethodPhase carries — so nothing re-derives a
//     profile the walk did not schedule from.
//   - CommandFor. The command is tc.Task.Command, the lifecycle DATA. The two agree today
//     (CommandFor is itself a lookup into this data) and Test_ConstructionCommands_MatchTheLifecycleData
//     pins that; carrying the task's own field is what stops the platform holding two answers.
// WHAT IT DOES REPRODUCE, and the first draft of this task did not (fix round 1): the VARIANCE
// LOOP. handleVariance/executeOverride were the retired supervision loop's whole answer to a
// failed job — DECIDE through the intervention Engine, retry or take over up to
// maxVarianceAttempts, or escalate to a bounded operator override with a Skip terminal — and
// dropping it made a red CI run cost a whole activity once instead of ten dispatches and a human.
// It is reproduced HERE, per task, rather than in the walk: see runTaskVariance.
// ---------------------------------------------------------------------------

// produceConstructionChange runs ONE construction task: open the attempt the dispatch is
// about to burn, carry the operator's steer, submit the agentic job the task's command names,
// observe it to a terminal phase, and report what it did.
//
// IT STAGES NOTHING, and that is the whole difference from the design arm. A construction
// task's output is a COMMIT the agent pushed to activity/<id>; this store holds no model for
// it, so the producedSubject's StagedRef stays empty and gateSubjectRef falls to the PR (rung
// two) or the attempt (rung three) — byte-for-byte what openGateRound did on the retired rail.
// It is the SUPERVISION LOOP, not one dispatch: a failed job goes to the variance machinery,
// which re-dispatches under the intervention Engine's directive up to maxVarianceAttempts times
// and then escalates to the operator. `attempt` is 0-based and the budget is checked at the TOP,
// exactly as runAttempt's own loop did, so the tenth failure escalates rather than the eleventh.
//
// EACH DISPATCH IS ITS OWN NUMBERED ATTEMPT (state.nextTaskAttempt), while the WALK's revision
// does not move: a variance retry is not a send-back. That is why the returned subject carries
// AttemptRecorded — runTask must not file the last dispatch's outcome under the first attempt's
// number and erase the rest.
func (wf *csWorkflows) produceConstructionChange(ctx workflow.Context, tc taskContext) (producedSubject, error) {
	// THE SEND-BACK'S STEER (walk-local, carried in walkState.feedback across a continue).
	// Without this a redraft runs against the prompt that was just rejected, which is the
	// send-back doing nothing — the retired rail carried it as an ephemeral operator note and so
	// does this, through the same renderer and the same dispatch input. ONCE, before the loop:
	// carrySendBackFeedback appends a note per call, and the pending-note queue is what carries
	// it into every retry that follows.
	carryWalkFeedback(ctx, tc)
	var deferred []routedSignal
	for attempt := 0; ; attempt++ {
		if attempt >= maxVarianceAttempts {
			// Terminal: the supervision budget is spent. failVarianceExhausted's own record.
			return wf.constructionGaveUp(ctx, tc, deferred,
				projectstate.ActivityOutcomeUnknown, projectstate.VarianceExhausted,
				"construction supervision exceeded max attempts")
		}
		tc.State.attempt = attempt + 1
		attemptID, obs, err := wf.dispatchConstructionOnce(ctx, tc)
		if err != nil {
			// A REJECTED DISPATCH fails the walk, and that is the retired rail's behaviour too:
			// submitPipeline's error propagated out of runPipeline and out of the supervision loop
			// without reaching handleVariance, because a dispatch that could not be submitted is a
			// wiring fault rather than a variance in the work. The scaffold-sync refusal, which the
			// rail DID route through variance, arrives as a failed observation instead (see
			// dispatchConstructionOnce).
			wf.reofferDeferred(ctx, tc, deferred)
			return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeFailed,
				Detail: err.Error(), AttemptRecorded: attemptID != ""}, err
		}
		if obs.Phase == PipelineSucceeded {
			wf.reofferDeferred(ctx, tc, deferred)
			return producedSubject{
				AttemptID: attemptID, Outcome: projectstate.OutcomePassed,
				EpisodeID: episodeIDOf(obs), AttemptRecorded: true,
				Detail: "dispatched " + tc.Task.Command + " for " + tc.Phase.ID,
			}, nil
		}
		verdict, held, verr := wf.runTaskVariance(ctx, tc, obs, attempt, deferred)
		deferred = held
		switch {
		case verr != nil:
			wf.reofferDeferred(ctx, tc, deferred)
			return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeFailed,
				Detail: verr.Error(), EpisodeID: episodeIDOf(obs), AttemptRecorded: true}, verr
		case verdict == varianceRedispatch:
			continue
		default:
			return wf.constructionExited(ctx, tc, deferred, attemptID, obs, verdict)
		}
	}
}

// dispatchConstructionOnce is ONE dispatch of a construction task: the numbered attempt opened
// PENDING before the job it describes (so a run that dies mid-dispatch leaves an attempt that
// says it started and never resolved rather than nothing at all — openWorkAttempt's own reason,
// and what redraftDispatched reads), the submit, the observe, and the attempt RESOLVED against
// the terminal observation with the episode it burned as its evidence.
//
// The attempt pair is resolveWorkAttempt's, verbatim: one id, one attempt, two writes, and the
// episode append runs first so the id it cites is already on the ledger.
func (wf *csWorkflows) dispatchConstructionOnce(
	ctx workflow.Context, tc taskContext,
) (string, csPipelineObservation, error) {
	in := tc.In.csIn()
	task := projectstate.MethodTask(tc.Task.ID)
	n := tc.State.nextTaskAttempt(task)
	attemptID := projectstate.AttemptID(string(tc.In.ActivityID), task, n)
	tc.State.workAttemptID = attemptID
	if err := wf.openWorkAttempt(ctx, in, tc.State, &tc.State.walk.headVersion, tc.State.walk.cred,
		task, n, attemptID); err != nil {
		return "", csPipelineObservation{}, err
	}
	dispatch, ok, err := wf.submitConstructionJob(ctx, tc, attemptID)
	if err != nil {
		// A REFUSED SUBMIT STILL RESOLVES ITS ATTEMPT (Task-11 round-2 defect D1). The attempt
		// above was opened PENDING so a run that dies mid-dispatch leaves a record saying it
		// started; returning here left it pending for a dispatch that will never resume, which
		// is the one state this pair's own comment forbids — and the caller reports
		// AttemptRecorded, so nothing downstream resolved it either. The failed resolve is
		// deliberately swallowed: the SUBMIT error is the cause and the one the walk must see.
		if rerr := wf.resolveWorkAttempt(ctx, in, tc.State, &tc.State.walk.headVersion, tc.State.walk.cred,
			task, n, attemptID, csPipelineObservation{Phase: PipelineFailed, Diagnostic: err.Error()}); rerr != nil {
			workflow.GetLogger(ctx).Error("the refused dispatch's attempt could not be resolved; it stays pending on the ledger",
				"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "attemptId", attemptID, "err", rerr.Error())
		}
		return attemptID, csPipelineObservation{}, err
	}
	obs := dispatch.obs
	if ok {
		obs = wf.observeDispatchedJob(ctx, tc, dispatch.handle, task, n, jobLabelConstruction, &tc.State.walk.gf)
	}
	// A scaffold-sync refusal dispatched NOTHING, so there is no episode to capture and no run
	// to observe — but the attempt still resolves, as a failure, because an attempt left pending
	// is indistinguishable from a run that died.
	if rerr := wf.resolveWorkAttempt(ctx, in, tc.State, &tc.State.walk.headVersion, tc.State.walk.cred,
		task, n, attemptID, obs); rerr != nil {
		return attemptID, obs, rerr
	}
	return attemptID, obs, nil
}

// varianceVerdict is what the variance machinery decided about a failed dispatch.
type varianceVerdict int

const (
	// varianceRedispatch: try the job again (Retry, Takeover, or an operator's Retry/Reassign/
	// Takeover override).
	varianceRedispatch varianceVerdict = iota
	// varianceSkipped: the operator SKIPPED the activity. It exits Skipped, which the pump reads
	// as Done so the dependents unblock — App A's "a skipped activity is an exit, not a stall".
	varianceSkipped
	// varianceEscalationTimedOut: nobody answered the escalation inside EscalationWaitTimeout.
	varianceEscalationTimedOut
)

// runTaskVariance is handleVariance and executeOverride, through the child, for ONE task.
//
// WHY IT LIVES IN THE STRATEGY AND NOT IN THE WALK (architect Ruling 3(a)): the walk must not
// know that a task's output comes from a pipeline that can fail in a venue, and the DECIDE is a
// judgement about a DISPATCH — the intervention Engine is asked about a construction variance,
// which is not a thing a computed Project-Design task or a design critique has. The walker sees
// only the producedSubject this returns through, and the arch guard still passes.
//
// TWO DIFFERENCES FROM THE RETIRED LOOP, both structural:
//
//   - the operator's override arrives on THIS TASK'S INBOX, not on the shared
//     operatorOverride channel. A fork has two dispatches in flight and a shared
//     ReceiveChannel hands each message to exactly one of them, so an override aimed at the
//     escalated task would be eaten by its sibling — the hole the router closes. It is
//     TaskID-addressed, and whatever this loop cannot answer is parked in the local `deferred`
//     slice and re-offered when the task retires, never re-queued into ws.pending.
//   - a RETRY re-dispatches THIS TASK, not the activity's whole phase list from index 0. The
//     completedPhases skip-guard existed because the retired retry re-walked everything; the
//     DAG has no such re-walk, so there is nothing to skip.
func (wf *csWorkflows) runTaskVariance(
	ctx workflow.Context, tc taskContext, obs csPipelineObservation, attempt int, deferred []routedSignal,
) (varianceVerdict, []routedSignal, error) {
	detail := dispatchJobFailedDetail(jobLabelConstruction, tc.Task.ID, obs)
	// The FLAGGED variance is the field the Activity Experience renders, and the retired
	// handleVariance set it before deciding anything.
	tc.State.variance = &FlaggedVariance{
		ProjectID: tc.In.ProjectID, ActivityID: tc.In.ActivityID, Summary: detail,
	}
	workflow.GetLogger(ctx).Error("delivery.construction.jobFailed",
		"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "attempt", attempt+1,
		"failureReason", int(deriveFailureReason(obs.Phase, obs.Diagnostic)))

	directive, derr := wf.Intervention.DecideOnVariance(fweng.Context{Context: context.Background()},
		intervention.ConstructionVariance{
			// ProjectID is deliberately fed from the ACTIVITY id, not the project id: the retired
			// handleVariance only ever carried this value and the Engine's refusal is on emptiness.
			// Do not "fix" it here — it would change what the Engine is asked about mid-wave.
			ProjectID:    intervention.ProjectID(tc.In.ActivityID),
			ActivityID:   intervention.ActivityID(tc.In.ActivityID),
			Kind:         intervention.WorkerMiss,
			AttemptCount: int64(attempt),
			Policy:       wf.InterventionPolicy,
		})
	if derr != nil {
		return varianceRedispatch, deferred, fwmanager.MapError(derr)
	}
	switch directive {
	case intervention.VarianceRetry, intervention.VarianceTakeover:
		// EXECUTE retry / takeover: re-dispatch. The prior pipeline already reached a terminal
		// before intervention was consulted, so there is nothing in flight to abandon.
		tc.State.stage = StageDispatching
		return varianceRedispatch, deferred, nil
	case intervention.VarianceEscalate:
		return wf.escalateTaskVariance(ctx, tc, detail, deferred)
	}
	// VarianceDirective has no Unknown sentinel (VarianceRetry is its zero value), so anything
	// outside the closed set is an unrecognized engine decision.
	return varianceRedispatch, deferred, temporal.NewNonRetryableApplicationError(
		"intervention returned an unknown directive", "UnknownDirective", nil)
}

// escalateTaskVariance surfaces the variance to the operator and waits for their override on
// this task's inbox, BOUNDED by EscalationWaitTimeout. A timeout is a terminal, not a hang: the
// retired rail recorded EscalationTimedOut rather than waiting for an override that never comes.
func (wf *csWorkflows) escalateTaskVariance(
	ctx workflow.Context, tc taskContext, detail string, deferred []routedSignal,
) (varianceVerdict, []routedSignal, error) {
	tc.State.redraftExhausted = false
	// Named at the moment of escalation, not only at its terminal: an operator who opens the log
	// because an activity is waiting on them must be able to read WHAT they are being asked about.
	workflow.GetLogger(ctx).Warn("delivery.construction.escalated",
		"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "detail", detail,
		"window", wf.EscalationWaitTimeout.String())
	tc.State.enterHumanStage(ctx, StageAwaitingTakeover, takeoverGateKey, wf.EscalationWaitTimeout)
	override, got, held := wf.awaitRoutedOverride(ctx, tc, deferred)
	if !got {
		tc.State.leaveHumanStage(ctx, tc.In.Activity.activityTypeName(), gateOutcomeTimedOut)
		return varianceEscalationTimedOut, held, nil
	}
	tc.State.leaveHumanStage(ctx, tc.In.Activity.activityTypeName(),
		gateOutcomeOverridePrefix+strings.ToLower(overrideKindName(override.Kind)))
	// The operator's steer is kept on the activity and — for a retry, takeover or reassign —
	// carried by the next dispatch. A skip's note is kept and never pending: nothing runs after
	// a skip. Identical to executeOverride's first act.
	if kind, ok := overrideNoteKind(override.Kind); ok {
		if err := wf.recordOperatorNote(ctx, tc.In.csIn(), tc.State, &tc.State.walk.headVersion,
			tc.State.walk.cred, kind, takeoverGateKey,
			noteFeedback{text: override.Notes, comments: override.Comments}); err != nil {
			return varianceRedispatch, held, err
		}
	}
	switch override.Kind {
	case OverrideRetry, OverrideReassign, OverrideTakeover:
		tc.State.stage = StageDispatching
		return varianceRedispatch, held, nil
	case OverrideSkip:
		return varianceSkipped, held, nil
	case OverrideUnknown:
		// The zero-value sentinel is not a real override kind, and guessing at one would run work
		// nobody asked for.
		return varianceRedispatch, held, temporal.NewNonRetryableApplicationError(
			"unknown operator override kind", "UnknownOverride", nil)
	}
	return varianceRedispatch, held, temporal.NewNonRetryableApplicationError(
		"unknown operator override kind", "UnknownOverride", nil)
}

// awaitRoutedOverride is awaitOverrideBounded over the task's own INBOX rather than the shared
// operatorOverride channel: the router already addressed the message to this task, so there is
// no filter here and no sibling can steal it.
//
// A timeout of 0 means wait forever, which is the supervised EscalateEverything mode's own
// behaviour and is preserved verbatim. Anything the escalation cannot answer — a gate decision,
// a redraft, a comment status — is parked in `deferred` and re-offered when the task retires;
// putting it back in ws.pending would spin the loop it came from (drainPending's rule).
func (wf *csWorkflows) awaitRoutedOverride(
	ctx workflow.Context, tc taskContext, deferred []routedSignal,
) (ActivityOverride, bool, []routedSignal) {
	if tc.Inbox == nil {
		return ActivityOverride{}, false, deferred
	}
	var timer workflow.Future
	if wf.EscalationWaitTimeout > 0 {
		timerCtx, cancel := workflow.WithCancel(ctx)
		defer cancel()
		timer = workflow.NewTimer(timerCtx, wf.EscalationWaitTimeout)
	}
	for {
		if tc.Drain != nil {
			// The receiver is what pulls an overflowed message through (drainPending): the router
			// never blocks, so a 65th override sits in ws.pending until a receive loop asks for it.
			tc.Drain()
		}
		var msg routedSignal
		timedOut := false
		sel := workflow.NewSelector(ctx)
		sel.AddReceive(tc.Inbox, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, &msg) })
		if timer != nil {
			sel.AddFuture(timer, func(workflow.Future) { timedOut = true })
		}
		sel.Select(ctx)
		switch {
		case timedOut:
			workflow.GetLogger(ctx).Warn("the escalation window closed with no operator override",
				"activityId", tc.In.ActivityID, "taskId", tc.Task.ID)
			return ActivityOverride{}, false, deferred
		case msg.Kind == routedKindOverride && msg.Override != nil:
			return msg.Override.Override, true, deferred
		case !routedPayloadPresent(msg):
			workflow.GetLogger(ctx).Error("a routed signal reached an escalation with no payload for its kind; dropped",
				"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "kind", msg.Kind)
		default:
			deferred = append(deferred, msg)
		}
	}
}

// constructionGaveUp records the ACTIVITY's terminal for a give-up the operator never had to
// answer — the spent supervision budget — and hands the walk a subject that stops it without a
// second terminal.
func (wf *csWorkflows) constructionGaveUp(
	ctx workflow.Context, tc taskContext, deferred []routedSignal,
	outcome projectstate.ActivityOutcome, reason projectstate.FailureReason, detail string,
) (producedSubject, error) {
	wf.reofferDeferred(ctx, tc, deferred)
	if err := wf.recordExecutionOutcome(ctx, tc.In.csIn(), tc.State, &tc.State.walk.headVersion,
		tc.State.walk.cred, outcome, reason, detail); err != nil {
		return producedSubject{}, err
	}
	tc.State.stage = StageExited
	workflow.GetLogger(ctx).Error("delivery.construction.gaveUp",
		"activityId", tc.In.ActivityID, "taskId", tc.Task.ID, "reason", int(reason), "detail", detail)
	return producedSubject{Outcome: projectstate.OutcomeFailed, Detail: detail, ActivityExited: true}, nil
}

// constructionExited records the ACTIVITY's terminal for the variance loop's two operator-facing
// answers, so the walk stops with the right one on the ledger rather than a re-derived guess.
func (wf *csWorkflows) constructionExited(
	ctx workflow.Context, tc taskContext, deferred []routedSignal,
	attemptID string, obs csPipelineObservation, verdict varianceVerdict,
) (producedSubject, error) {
	detail := dispatchJobFailedDetail(jobLabelConstruction, tc.Task.ID, obs)
	if verdict == varianceSkipped {
		subject, err := wf.constructionGaveUp(ctx, tc, deferred,
			projectstate.ActivityOutcomeSkipped, projectstate.FailureReasonUnknown, "")
		// A skip is not a failure: the attempt that failed stays on the ledger with its own
		// diagnostic, and the ACTIVITY reads Skipped. The subject cites the skipped attempt so a
		// reader can follow what the operator was looking at.
		subject.AttemptID, subject.EpisodeID, subject.AttemptRecorded = attemptID, episodeIDOf(obs), true
		subject.Outcome, subject.Detail = projectstate.OutcomeSkipped, detail
		return subject, err
	}
	timedOut := "escalation timed out: no operator override within the escalation-wait window (underlying: " + detail + ")"
	subject, err := wf.constructionGaveUp(ctx, tc, deferred,
		projectstate.ActivityOutcomeUnknown, projectstate.EscalationTimedOut, timedOut)
	subject.AttemptID, subject.EpisodeID, subject.AttemptRecorded = attemptID, episodeIDOf(obs), true
	return subject, err
}

// constructionDispatch is what submitConstructionJob hands back: the handle when something
// was dispatched, or the refusal's own observation when nothing was.
type constructionDispatch struct {
	handle pipelineHandle
	obs    csPipelineObservation
}

// submitConstructionJob composes and submits ONE construction job. ok=false means the
// managed-scaffold sync refused and NOTHING was dispatched (§C.1.4: a note must never ride
// into a YAML that would reject it), with the refusal's observation in obs.
//
// The spec carries the lifecycle's own COMMAND and leaves Type/Variant zero, deliberately:
// dispatchInputsFor re-derives the command from that pair only for the caller that has no
// command to give, which is the retired rail. Passing both would be two answers to keep in
// step — the exact defect the "classify once, carry the result" comment on
// constructionActivity.Type describes, arriving one level down.
func (wf *csWorkflows) submitConstructionJob(
	ctx workflow.Context, tc taskContext, attemptID string,
) (constructionDispatch, bool, error) {
	in := tc.In.csIn()
	if tc.State.noteDelivery {
		if obs, ok := wf.syncScaffoldBeforeDispatch(ctx, in, &tc.State.walk.gf); !ok {
			return constructionDispatch{obs: obs}, false, nil
		}
	}
	handle, err := wf.submitCarryingNotes(ctx, in, pipelineSpec{
		ProjectID:   tc.In.ProjectID,
		ActivityID:  string(tc.In.ActivityID),
		ComponentID: tc.In.Activity.ComponentID,
		Phase:       tc.Phase.ID,
		Command:     tc.Task.Command,
	}, tc.State, attemptID, &tc.State.walk.gf, &tc.State.walk.headVersion)
	if err != nil {
		return constructionDispatch{}, false, err
	}
	return constructionDispatch{handle: handle}, true, nil
}

// carryWalkFeedback turns the walk's own send-back notes into the ephemeral operator note the
// next dispatch carries. The walk keeps the steer in walkState.feedback (a string, keyed by
// the JUDGED task) and hands it to the strategy as taskContext.Feedback, so this is the point
// where a walk-local fact becomes a dispatch input.
//
// THE GATE IT NAMES is the task's own lifecycle PHASE, which is what the retired rail recorded
// (carrySendBackFeedback took lifecyclePhase.String()) — so a note rendered by either rail
// reads the same. A no-op when nothing was sent back, and a no-op off the note-delivery fence.
//
// The COMMENTS do not survive: walkState.feedback is signalNotes(fb), a string, so a
// send-back's anchored comments are already dropped by the walk itself (Task 8). Earmarked
// rather than papered over here — the renderer takes them and there is nothing to give it.
func carryWalkFeedback(ctx workflow.Context, tc taskContext) {
	if tc.Feedback == "" {
		return
	}
	carrySendBackFeedback(ctx, tc.In.csIn(), tc.State, tc.Phase.ID, &ReviewFeedback{Notes: tc.Feedback})
}

// episodeIDOf is the agentic episode a terminal observation carried, or "" when the venue
// mined none (the GitHub-Actions arm mines no episode in v1, and a lost run carries nothing).
// A ref nobody can follow is worse than an absent one.
func episodeIDOf(obs csPipelineObservation) string {
	if obs.Episode == nil {
		return ""
	}
	return obs.Episode.EpisodeID
}

// judgedTaskStrategy is the JUDGED slot, and it produces NOTHING — deliberately, and from
// the start rather than as a refusal. A review task's subject IS the output of the task its
// `reviews` field names, which runGate reads out of walkState.produced; a review that
// produced a subject of its own would be judging itself.
type judgedTaskStrategy struct{}

func (judgedTaskStrategy) Produce(_ workflow.Context, _ taskContext) (producedSubject, error) {
	return producedSubject{}, nil
}

// sdpComputeStrategy is the FIRST computation registered against the strategy table's
// THIRD row: a review task that names no `reviews` target, whose subject the platform
// DERIVES rather than dispatches. Spec §6/R7 — Project Design is deterministic, there are
// no agent-drafted steps, and the reviewer's job is to approve a plan and its cost.
//
// WHAT IT WRITES, and what it deliberately does not: see projectDesignComputedKinds and
// computeProjectPlanSlots (deliverymanager.go), which hold the whole derivation as a pure
// function. This type is the workflow half — the read, the eight staging writes, and the
// attempt.
//
// Recorded as a TaskAttempt with the ENGINE as actor, so a failed computation is visible
// and retryable exactly like a failed draft (architect Ruling 3(c)). A silent compute
// failure at M0 would be a project that never starts with nothing saying why.
type sdpComputeStrategy struct {
	wf  *csWorkflows
	eng sdpEngines
}

func (s sdpComputeStrategy) Produce(ctx workflow.Context, tc taskContext) (producedSubject, error) {
	attemptID := projectstate.AttemptID(string(tc.In.ActivityID), projectstate.MethodTask(tc.Task.ID), tc.Attempt)
	ref, defaulted, err := s.wf.computeProjectPlan(ctx, tc, s.eng)
	if err != nil {
		// The FAILED attempt is recorded by runTask from this producedSubject, and the error
		// is returned too: the attempt is what an operator reads, the error is what fails the
		// walk. Returning only one of them would either hide the failure or leave the ledger
		// silent about it.
		return producedSubject{AttemptID: attemptID, Outcome: projectstate.OutcomeFailed, Detail: err.Error()}, err
	}
	return producedSubject{
		StagedRef: ref, AttemptID: attemptID, Outcome: projectstate.OutcomePassed,
		// The defaulting rides the attempt's Detail because that is what the M0 screen reads
		// back: an assumed number the founder never saw is the one way a computed cost can
		// mislead. Task 14 renders the sentence; recording it is this task's.
		Detail: defaultedDetail(defaulted),
	}, nil
}

// computeProjectPlan derives the whole Project-Design plan and STAGES it, returning the
// ref the M0 round cites and the assumption families it had to default.
//
// Each of the eight slots is staged through activityExecutionAccess.StageTaskOutput — the
// execution ledger's own verb — and NOT through the three designSessionAccess verbs the
// retired SDP assembly used. That re-homing is what leaves stage 4b2 a facet it can delete
// rather than a rail with two owners.
//
// The subject the round cites is the SDP REVIEW's staged ref, the last of the eight: that
// is the artifact M0 judges, and it advances with every recompute, which is what lets the
// ledger say which revision of the plan a round looked at.
func (wf *csWorkflows) computeProjectPlan(
	ctx workflow.Context, tc taskContext, eng sdpEngines,
) (string, []string, error) {
	in := tc.In
	proj, err := wf.readProject(ctx, in.ProjectID)
	if err != nil {
		if isReadNotFound(err) {
			return "", nil, newError(fwmanager.FailedPrecondition,
				"cannot compute the project plan: project "+string(in.ProjectID)+" has no state")
		}
		return "", nil, err
	}
	slots, defaulted, err := computeProjectPlanSlots(proj, eng)
	if err != nil {
		return "", nil, err
	}
	ref := ""
	for _, slot := range slots {
		staged, sErr := wf.stageComputedSlot(ctx, in, tc.Task.ID, slot, tc.State)
		if sErr != nil {
			return "", nil, sErr
		}
		ref = staged
	}
	workflow.GetLogger(ctx).Info("delivery.projectDesign.computed",
		"activityId", in.ActivityID, "slots", len(slots), "defaulted", len(defaulted))
	return ref, defaulted, nil
}

// stageComputedSlot stages ONE computed slot and returns the ref a round can cite.
//
// state.rowAdvanced() is deliberately NOT called: StageTaskOutput is the one verb on the
// facet that asserts no per-activity version, because the write lands on the session
// branch while the row and its counter live on main. Bumping the run's row expectation
// after a write that never touched the row would make the NEXT row write fail its CAS.
func (wf *csWorkflows) stageComputedSlot(
	ctx workflow.Context, in deliveryActivityInput, taskID string,
	slot computedSlot, state *constructState,
) (string, error) {
	env, encErr := encodeModel(slot.Model)
	if encErr != nil {
		return "", fwmanager.MapError(encErr)
	}
	var staged projectstate.StagedRef
	v, err := wf.applyRecovering(ctx, in.ProjectID, state.walk.headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		sr, sErr := wf.Acts.ActivityExecutionStageTaskOutput(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), taskID, "", env, state.walk.cred.toProjectState())
		if sErr != nil {
			return 0, sErr
		}
		staged = sr
		return sr.Version, nil
	})
	if err != nil {
		return "", err
	}
	state.walk.headVersion = v
	return stagedRefString(staged, slot.Kind), nil
}

// stagedRefString renders a StagedRef as the string SubjectRef.Ref carries. It names the
// KIND as well as the task, because Project Design stages EIGHT slots under one task id and
// a ref that named only the task could not say which of them a round judges.
func stagedRefString(ref projectstate.StagedRef, kind projectstate.ArtifactKind) string {
	return ref.ActivityID + ":" + ref.TaskID + ":" + kind.String() + "@v" + strconv.FormatInt(int64(ref.Version), 10)
}

// strategyFor resolves a task to its strategy SLOT from the lifecycle fields ALONE
// (R5: v0.9.0's field set, since parseLifecycles DisallowUnknownFields makes a new
// field a platform release), then reads that slot out of the registry. The table,
// one row per (kind × subject class) — three rows, and there is no fourth: a review
// task's own `command` runs its AGENT REVIEWERS, which runGate does, not strategyFor.
//
//	kind=dispatch, command!="" , artifactKind!="" , workerClass!=""  -> slot "dispatch"
//	kind=review  , reviews!=""                                       -> slot "judged" (its subject is the judged task's output; it produces nothing of its own)
//	kind=review  , reviews==""  , artifactKind!=""                   -> slot "compute:<artifactKind>"
//
// The third row IS the carve-out that makes deterministic Project Design a generic
// child rather than a special case: ValidateLifecycle permits "a review that names no
// dispatch, in a lifecycle with no dispatch at all", so the compute must NOT be
// modelled as a task — that would be a method-assets release inside this wave. A
// review task with no `reviews` target has its subject produced by the strategy
// registered for its artifactKind.
func strategyFor(reg strategyRegistry, wf *csWorkflows, lc methodassets.Lifecycle, t methodassets.LifecycleTask) (taskStrategy, error) {
	slot, err := strategySlotFor(lc, t)
	if err != nil {
		return nil, err
	}
	ctor, ok := reg[slot]
	if !ok {
		return nil, newError(fwmanager.FailedPrecondition,
			"lifecycle "+lc.Type+" task "+t.ID+" needs strategy slot "+slot+", which nothing has registered")
	}
	return ctor(wf), nil
}

// strategySlotFor is strategyFor's table, split out so the resolution stays one readable
// switch and the registry lookup stays one line (gocyclo, and the two questions are
// genuinely separate: WHICH slot the data asks for, and whether anything filled it).
func strategySlotFor(lc methodassets.Lifecycle, t methodassets.LifecycleTask) (string, error) {
	switch t.Kind {
	case methodassets.LifecycleTaskDispatch:
		if t.Command == "" || t.WorkerClass == "" || t.ArtifactKind == "" {
			return "", newError(fwmanager.FailedPrecondition,
				"lifecycle "+lc.Type+" task "+t.ID+" is a dispatch with no command, workerClass or artifactKind; the platform's lifecycle data is malformed")
		}
		return strategySlotDispatch, nil
	case methodassets.LifecycleTaskReview:
		if t.Reviews != "" {
			return strategySlotJudged, nil
		}
		if t.ArtifactKind != "" {
			return strategySlotCompute(t.ArtifactKind), nil
		}
		return "", newError(fwmanager.FailedPrecondition,
			"lifecycle "+lc.Type+" task "+t.ID+" reviews nothing and names no artifact kind, so nothing can produce its subject")
	}
	return "", newError(fwmanager.FailedPrecondition,
		"lifecycle "+lc.Type+" task "+t.ID+" has task kind "+t.Kind+", which is neither dispatch nor review")
}

// ---------------------------------------------------------------------------
// THE WALK
// ---------------------------------------------------------------------------

// walkState is the walk's own bookkeeping. FOUR maps, and each earns its place:
//
//	byTask    each task's position. A revision is a DERIVED grouping everywhere else
//	          (spec §5.3) and stored nowhere; this is walk-local and rebuilt on resume.
//	revision  the counter the next round id is minted from.
//	feedback  the send-back notes a re-opened task must be re-dispatched WITH. Without
//	          it a redraft runs against the same prompt that was just rejected, which
//	          is the send-back doing nothing.
//	produced  what each task's strategy produced, keyed by TASK. This is how a review
//	          task reaches its JUDGED task's staged ref and attempt id: a review
//	          produces nothing of its own (judgedTaskStrategy), so without this map
//	          gateSubjectRef would fall to its third rung on every round and "the
//	          subject changes between rounds 1 and 2" could not hold.
//
// Those four are JSON-serialisable, which is what makes walkSnapshot possible. Two more
// are DELIVERY state, not walk state, and the signal router is what they are for:
//
//	inbox     one per RUNNING task, created when the task is scheduled. It is the ONLY
//	          channel a task coroutine receives from.
//	pending   messages the router took off a shared channel for a task that has no
//	          inbox yet. Flushed into the inbox at creation, carried across a continue.
type walkState struct {
	byTask   map[string]walkTaskState
	revision map[string]int64
	feedback map[string]string
	produced map[string]producedSubject
	inbox    map[string]workflow.Channel
	pending  map[string][]routedSignal

	// rec is the lifecycle-shape oracle's delivery hook, nil in production. It is here
	// rather than at the router because WHICH TASK received a message is the thing the
	// routing cases assert, and deliver is the one place that knows.
	rec walkDeliveryRecorder
}

// walkDeliveryRecorder observes which task a routed signal was delivered to. Its one
// production implementation is nil: the assertion "the gate acted on the override and the
// polling task did not" is about delivery, and a case that only checked the walk's
// terminal would pass with the override lost — a gate that never receives one just waits
// for its decision instead.
type walkDeliveryRecorder interface {
	signalDelivered(taskID, kind string)
	// signalDropped is the OTHER half, and it is not symmetry for its own sake: "the
	// message for a finished task was DROPPED" and "it was quietly buffered for the task's
	// next revision" are indistinguishable from the delivered side alone — neither ever
	// reaches an inbox in the run under test — and the second is the leak that lets an
	// override of revision 1 decide revision 2.
	signalDropped(taskID, kind string)
}

// newWalkState builds an empty walk with every map allocated, so nothing writes a nil map.
func newWalkState(lc methodassets.Lifecycle) *walkState {
	ws := &walkState{
		byTask:   make(map[string]walkTaskState, len(lc.Tasks)),
		revision: make(map[string]int64, len(lc.Tasks)),
		feedback: map[string]string{},
		produced: map[string]producedSubject{},
		inbox:    map[string]workflow.Channel{},
		pending:  map[string][]routedSignal{},
	}
	for _, t := range lc.Tasks {
		ws.byTask[t.ID] = walkTaskPending
	}
	// The LOCAL merge hold is a gate with no lifecycle task of its own, and the router's
	// addressing unit is a task, so it is seeded here: an approve that arrives before the
	// hold opens is then BUFFERED (walkTaskPending is "too early") rather than dropped as
	// too-late, which is the difference between a merge that releases and one that hangs.
	ws.byTask[mergeGateTaskID] = walkTaskPending
	return ws
}

// snapshot is walkState across ContinueAsNew: the four walk maps plus the undelivered
// messages, and never `inbox` — it holds channels, which the resumed run re-creates.
func (ws *walkState) snapshot() *walkSnapshot {
	snap := &walkSnapshot{
		ByTask:   make(map[string]int, len(ws.byTask)),
		Revision: make(map[string]int64, len(ws.revision)),
		Feedback: make(map[string]string, len(ws.feedback)),
		Produced: make(map[string]producedSubject, len(ws.produced)),
		Pending:  make(map[string][]routedSignal, len(ws.pending)),
	}
	for id, st := range ws.byTask {
		snap.ByTask[id] = int(st)
	}
	maps.Copy(snap.Revision, ws.revision)
	maps.Copy(snap.Feedback, ws.feedback)
	maps.Copy(snap.Produced, ws.produced)
	for id, msgs := range ws.pending {
		snap.Pending[id] = append([]routedSignal(nil), msgs...)
	}
	return snap
}

// walkStateFrom is snapshot's inverse: it re-creates the walk from the carried maps and
// re-creates `inbox` EMPTY, because at the one point a continue is taken (inflight == 0)
// no task holds an inbox and every undelivered message is in Pending.
func walkStateFrom(lc methodassets.Lifecycle, snap *walkSnapshot) *walkState {
	ws := newWalkState(lc)
	for id, st := range snap.ByTask {
		ws.byTask[id] = walkTaskState(st)
	}
	maps.Copy(ws.revision, snap.Revision)
	maps.Copy(ws.feedback, snap.Feedback)
	maps.Copy(ws.produced, snap.Produced)
	for id, msgs := range snap.Pending {
		ws.pending[id] = append([]routedSignal(nil), msgs...)
	}
	return ws
}

// readyTasks returns every task whose dependsOn are all PASSED and which is not
// already running, passed or failed — in lifecycle declaration order, which
// ValidateLifecycle guarantees is a topological order (every dependsOn appears
// earlier). Declaration order is what makes the fan-out deterministic under replay:
// a map walk here would be non-determinism, and a sort by id would be a second rule
// to keep in step with the data.
func readyTasks(lc methodassets.Lifecycle, ws *walkState) []methodassets.LifecycleTask {
	var out []methodassets.LifecycleTask
	for _, t := range lc.Tasks {
		if ws.byTask[t.ID] != walkTaskPending {
			continue
		}
		ready := true
		for _, dep := range t.DependsOn {
			if ws.byTask[dep] != walkTaskPassed {
				ready = false
				break
			}
		}
		if ready {
			out = append(out, t)
		}
	}
	return out
}

// DeliveryActivityWorkflow walks ONE activity's lifecycle DAG to its terminal.
//
// It replaces walkPhases (a flat []ActivityMethodPhase from ProfileFor, which threw
// the task DAG away), CoAuthorArtifactWorkflow, CoAuthorPhase2ArtifactWorkflow,
// SystemDesignPhaseWorkflow and both phase-advance wrappers. The DAG, the gates, the
// ledger appends and the send-back machinery are one thing; 7,712 lines of near-twin
// co-author code was the price of keeping them two.
//
// THE LOOP: take every ready task, run them CONCURRENTLY as workflow goroutines, and
// wait until one finishes. A review task suspends on its gate inside its own goroutine,
// so a fork with a gate on one branch does not stall the other — which is the whole
// reason `stp` and `detailedDesign` can now overlap. A join task simply never becomes
// ready until all of its dependsOn have passed, so "the join waits for all" needs no
// code of its own: it is readyTasks' definition.
//
// A SEND-BACK re-opens ONLY the judged pair: the review task and the task its
// `reviews` field names go back to pending at revision n+1, and nothing else is
// touched. The flat walk could not express that — it re-walked the phase list — and
// it is the behaviour the send-back shape case asserts.
func (wf *csWorkflows) DeliveryActivityWorkflow(ctx workflow.Context, in deliveryActivityInput) error {
	lcKey := projectstate.LifecycleKeyFor(in.Activity.Type, in.Activity.Variant)
	lc, ok := methodassets.LifecycleFor(lcKey)
	if !ok {
		return temporal.NewNonRetryableApplicationError(
			"the platform's method assets carry no lifecycle for "+lcKey, "LifecycleUnknown", nil)
	}
	state := &constructState{
		projectID: in.ProjectID, activityID: in.ActivityID, stage: StageDispatching,
		completedPhases: map[projectstate.ActivityMethodPhase]bool{},
	}
	ctx = wf.bindRowAccessors(ctx, in, state)
	if err := workflow.SetQueryHandler(ctx, querySessionState, state.view); err != nil {
		return err
	}
	// All FOUR shared signal channels are opened here and handed to the ROUTER, which is
	// the only coroutine that receives from any of them. The redraft channel is the one
	// DispatchActivityTask's re-run lands on (Task 12).
	decisions := workflow.GetSignalChannel(ctx, signalTaskDecision)
	statuses := workflow.GetSignalChannel(ctx, signalSetCommentStatus)
	overrides := workflow.GetSignalChannel(ctx, signalOperatorOverride)
	redrafts := workflow.GetSignalChannel(ctx, lSignalRedraft)

	reviewPolicy, snap, err := wf.loadReviewSnapshot(ctx, in.csIn(), state)
	if err != nil {
		return err
	}
	state.walk.policy = reviewPolicy
	if err := wf.openActivityRow(ctx, in, state); err != nil {
		return err
	}
	// THE PER-DISPATCH ATTEMPT COUNTER, seeded off the SAME read (fix round 1). The walk's
	// revision map is in walkSnapshot; this counter is not, because it is not walk state — it is
	// the ledger's own numbering, and the ledger is what a resumed or continued run must not
	// collide with. A fresh constructState restarts it at 1, so without this seed a variance
	// retry after a continue-as-new would mint an AttemptID the ledger already holds and the
	// store would absorb the new dispatch as a replay of the old one.
	seedTaskAttempts(state, snap.ActivityExecution[string(in.ActivityID)])
	ws := wf.seedWalkFromLedger(ctx, in, lc, snap)
	// The router starts BEFORE the first task is scheduled, so a signal that arrives
	// during the very first dispatch is buffered against its task rather than lost.
	workflow.Go(ctx, func(gctx workflow.Context) {
		wf.routeSignals(gctx, ws, decisions, statuses, overrides, redrafts)
	})
	return wf.walkTasks(ctx, in, lc, ws, state, reviewPolicy)
}

// walkDone is one task coroutine's report back to the walk.
type walkDone struct {
	taskID string
	state  walkTaskState
	err    error
}

// walkTasks is the loop itself, split out of the entry func so the entry func stays the
// session's setup and this stays the DAG (gocognit, and they are two different readings).
func (wf *csWorkflows) walkTasks(
	ctx workflow.Context,
	in deliveryActivityInput,
	lc methodassets.Lifecycle,
	ws *walkState,
	state *constructState,
	policy projectstate.ReviewPolicy,
) error {
	// BUFFERED to len(lc.Tasks), deliberately. On failWalk this workflow returns while
	// sibling coroutines are still running, and an unbuffered channel would leave them
	// blocked in Send — a leaked-coroutine warning at best. Capacity equal to the task
	// count means every task that was ever started can always deliver its result,
	// whether or not anyone is left to receive it.
	results := workflow.NewBufferedChannel(ctx, len(lc.Tasks))
	inflight := 0
	for {
		for _, t := range readyTasks(lc, ws) {
			ws.byTask[t.ID] = walkTaskRunning
			inflight++
			task := t
			// The task's inbox is created HERE, before its coroutine runs, and anything
			// the router already buffered for it is flushed in first — so a signal that
			// beat the task to the walk arrives in order rather than being dropped.
			inbox := ws.openInbox(ctx, task.ID)
			workflow.Go(ctx, func(gctx workflow.Context) {
				st, rerr := wf.runTask(gctx, in, lc, task, ws, state, policy, inbox)
				results.Send(gctx, walkDone{taskID: task.ID, state: st, err: rerr})
			})
		}
		if inflight == 0 {
			return wf.finalizeWalk(ctx, in, lc, ws, state)
		}
		var d walkDone
		results.Receive(ctx, &d)
		inflight--
		if d.err != nil {
			ws.byTask[d.taskID] = walkTaskFailed
			return wf.failWalk(ctx, in, state, d.taskID, d.err)
		}
		ws.byTask[d.taskID] = d.state
		ws.closeInbox(workflow.GetLogger(ctx), d.taskID)
		if d.state == walkTaskExited {
			// The task's own supervision reached a terminal for the WHOLE activity and recorded
			// it. Returning nil — not an error — is what the retired supervision loop did for the
			// same three answers, and it is what keeps the pump's cascade alive: a child that
			// errors fails the pump run that is blocked on it, so a single activity giving up
			// would stop the project's one pump.
			workflow.GetLogger(ctx).Info("delivery.walk.exitedInTask",
				"activityId", in.ActivityID, "taskId", d.taskID)
			// AND IT IS REPORTED TO THE PUMP (stage 4b2). This is a terminal the platform
			// RECORDED — the task's own supervision wrote it — so it is a finish, not a
			// failure: the pump drops the activity from its started set, frees any lease and
			// carries the cascade on. The walk never reached its tail, so no lease was ever
			// taken and the epoch is zero.
			wf.signalActivityFinished(ctx, in, projectstate.ActivityOutcomeUnknown, 0)
			return nil
		}
		if d.state == walkTaskSentBack {
			reopenJudgedPair(lc, ws, d.taskID)
		}
		// THE ONLY SAFE CONTINUE-AS-NEW POINT. inflight == 0 means no coroutine holds a
		// gate or a poll, so the snapshot is complete and nothing is abandoned
		// mid-dispatch.
		if inflight == 0 && shouldContinueAsNew(ctx) {
			return workflow.NewContinueAsNewError(ctx, executionKindDeliveryActivity,
				deliveryActivityInput{ProjectID: in.ProjectID, ActivityID: in.ActivityID,
					Activity: in.Activity, Resume: ws.snapshot()})
		}
	}
}

// reopenJudgedPair is the send-back rule, and it is four lines because the data says
// which pair: a review task names the dispatch it judges. Both go back to PENDING at
// revision n+1 and NOTHING ELSE is touched — not the sibling branch, not a later
// phase, not a task that already passed. The flat walk had no way to say this.
func reopenJudgedPair(lc methodassets.Lifecycle, ws *walkState, reviewTaskID string) {
	t, ok := lifecycleTaskByID(lc, reviewTaskID)
	if !ok || t.Reviews == "" {
		return
	}
	ws.revision[t.Reviews]++
	ws.revision[t.ID]++
	ws.byTask[t.Reviews] = walkTaskPending
	ws.byTask[t.ID] = walkTaskPending
}

// ---------------------------------------------------------------------------
// THE SIGNAL ROUTER — one owner per shared channel, one inbox per task.
//
// THE DEFECT IT PREVENTS, STATED: with two tasks in flight on a fork — `stp` polling
// its pipeline while `designReview` waits at its gate — two coroutines would each be
// selecting on the SHARED operatorOverride channel. A ReceiveChannel delivers each
// message to exactly one receiver, so an override aimed at the gate is consumed by
// whichever coroutine the SDK schedules first and SILENTLY LOST. Filtering inside each
// receiver does not fix it either: the message is already gone by the time the filter
// runs, and re-publishing it would put the workflow in a forwarding loop with no
// ordering guarantee. So no task coroutine ever touches a shared channel.
// ---------------------------------------------------------------------------

// deliveryTaskInboxCapacity sizes one task's inbox. 64 is far above any real backlog
// (an operator sends single digits), and the overflow rule is the ONE thing this const
// must not get wrong: **the router NEVER blocks.**
//
// WHY NOT BACK-PRESSURE, which is what a first reading suggests. If deliver blocked on
// a full inbox, that Send would still be parked when the task retired: closeInbox
// deletes the map entry, the coroutine that would have received is gone, no receiver
// can ever exist for that channel again, and the ROUTER never selects a second time. One
// wedged task would then silently swallow every later signal in the whole activity —
// including a sibling gate's decision — and the parked message would sit in neither
// `inbox` nor `pending`, so ContinueAsNew would lose it too. A bound whose overflow
// behaviour can deadlock the thing that enforces it is not a bound.
//
// So: SendAsync, and a failed send falls back to `pending`, which is exactly where an
// undelivered message already belongs. "A decision is never dropped" lives in `pending`
// and never in blocking.
//
// `pending` IS THE ROUTER'S OVERFLOW AND NOTHING ELSE. A task coroutine that receives a
// message it cannot handle yet must hold it LOCALLY, never hand it back here — see
// drainPending for why a re-queue spins a polling loop's history.
const deliveryTaskInboxCapacity = 64

// routeSignals is the ONE coroutine that receives from a shared signal channel. It
// takes each message off whichever of the four channels carried it, reads the TaskID
// every payload carries, and forwards it to that task's inbox — or buffers it in
// ws.pending when the task has not been scheduled yet.
//
// THE ORDERING GUARANTEE, and why this is replay-safe: the router receives in signal
// DELIVERY order (the order the history records the signals) and forwards
// SYNCHRONOUSLY and WITHOUT EVER BLOCKING, before selecting again. So per task, the
// order a coroutine reads its messages is exactly the order the history delivered them,
// on every replay, with no dependence on how the SDK interleaved the task coroutines —
// and no task can stall the router, which is what keeps that guarantee true for the
// OTHER tasks. workflow.Selector, NewNamedBufferedChannel and Channel.SendAsync are all
// SDK primitives the replayer schedules itself; nothing here reads a clock or a map.
//
// A signal for a task that has already reached its TERMINAL is logged and dropped: the
// task it names is over, and delivering it to the next revision's coroutine would let an
// override of revision 1 decide revision 2. closeInbox is what makes that state
// observable — an absent inbox plus a non-pending walkTaskState is "too late", where an
// absent inbox plus walkTaskPending is "too early" and buffers.
func (wf *csWorkflows) routeSignals(ctx workflow.Context, ws *walkState, decisions, statuses, overrides, redrafts workflow.ReceiveChannel) {
	logger := workflow.GetLogger(ctx)
	for {
		var msg routedSignal
		sel := workflow.NewSelector(ctx)
		sel.AddReceive(decisions, func(c workflow.ReceiveChannel, _ bool) {
			var s taskDecisionSignal
			c.Receive(ctx, &s)
			msg = routedSignal{Kind: routedKindDecision, TaskID: s.TaskID, Decision: &s}
		})
		sel.AddReceive(statuses, func(c workflow.ReceiveChannel, _ bool) {
			var s setCommentStatusSignal
			c.Receive(ctx, &s)
			msg = routedSignal{Kind: routedKindStatus, TaskID: s.TaskID, Status: &s}
		})
		sel.AddReceive(overrides, func(c workflow.ReceiveChannel, _ bool) {
			var s operatorOverrideSignal
			c.Receive(ctx, &s)
			msg = routedSignal{Kind: routedKindOverride, TaskID: s.TaskID, Override: &s}
		})
		sel.AddReceive(redrafts, func(c workflow.ReceiveChannel, _ bool) {
			var s redraftSignal
			c.Receive(ctx, &s)
			msg = routedSignal{Kind: routedKindRedraft, TaskID: s.TaskID, Redraft: &s}
		})
		sel.Select(ctx)
		ws.deliver(logger, msg)
	}
}

// openInbox creates a task's inbox and flushes whatever the router buffered for it
// BEFORE its coroutine reads, so a signal that beat the task into the walk keeps its
// place in the order rather than being dropped or re-ordered behind a later one.
func (ws *walkState) openInbox(ctx workflow.Context, taskID string) workflow.Channel {
	ch := workflow.NewNamedBufferedChannel(ctx, "inbox:"+taskID, deliveryTaskInboxCapacity)
	ws.inbox[taskID] = ch
	ws.drainPending(taskID)
	return ch
}

// drainPending moves as much of a task's pending queue into its inbox as will fit, in
// order, keeping the remainder queued. It is called at openInbox AND at the top of every
// receive-loop iteration, because that is the only moment the inbox is known to have just
// freed a slot — the router cannot wait for one, so the RECEIVER is what pulls the
// overflow through.
//
// THE RULE FOR A POLLING LOOP THAT CANNOT HANDLE WHAT IT RECEIVED (Task 8 review finding 1;
// Tasks 10 and 11 write those loops). A strategy that polls while receiving on its inbox
// will sometimes take a message it has no answer for — a gate decision arriving mid-dispatch.
// It must park that message in a LOCAL `deferred` slice and re-offer it when the task
// retires. It must NEVER put it back in ws.pending: this function runs at the TOP of every
// loop iteration, so a re-queued message is re-offered immediately, and each iteration of a
// polling loop builds a fresh workflow.NewTimer — the loop would spin, growing durable
// history as fast as it can loop, and the history budget would trip on a walk that did
// nothing.
func (ws *walkState) drainPending(taskID string) {
	ch := ws.inbox[taskID]
	if ch == nil {
		return
	}
	held := ws.pending[taskID]
	sent := 0
	for _, m := range held {
		if !ch.SendAsync(m) {
			break
		}
		sent++
		ws.recordDelivery(m)
	}
	switch {
	case sent == len(held):
		delete(ws.pending, taskID)
	case sent > 0:
		ws.pending[taskID] = held[sent:]
	}
}

// closeInbox retires a finished task's undelivered messages — from BOTH places they can
// sit, the channel and the pending queue — so nothing is left held by a channel no
// coroutine will ever read again and nothing is left queued for a task that is over.
// Each is logged as too-late rather than re-queued: the task they name is finished, and
// handing them to the next revision's coroutine would let an override of revision 1
// decide revision 2. Draining rather than dropping silently is what makes the loss
// auditable.
//
// CLEARING pending IS THE LOAD-BEARING HALF, and it is easy to leave out. A message the
// inbox was too full to take lives in ws.pending, which walkSnapshot carries — so an
// entry left behind here would ride every ContinueAsNew for the rest of the activity,
// and on a SEND-BACK reopenJudgedPair puts the task back to walkTaskPending, whose next
// openInbox would flush that stale message into revision n+1. That is exactly the leak
// the paragraph above exists to prevent, arriving through the other door.
// It takes NO workflow.Context, deliberately: nothing in it can block or emit a command
// (ReceiveAsync and a map delete are both commandless), and a parameter no body reads is
// exactly the lie proposeReviewSet's doc calls out about its retired architectureGraph
// argument — one the compiler cannot catch.
func (ws *walkState) closeInbox(logger log.Logger, taskID string) {
	tooLate := func(msg routedSignal) {
		logger.Info("signal was undelivered when its task retired",
			"kind", msg.Kind, "taskId", taskID)
	}
	for _, msg := range ws.pending[taskID] {
		tooLate(msg)
	}
	delete(ws.pending, taskID)

	ch := ws.inbox[taskID]
	delete(ws.inbox, taskID)
	if ch == nil {
		return
	}
	for {
		var msg routedSignal
		if !ch.ReceiveAsync(&msg) {
			return
		}
		tooLate(msg)
	}
}

// deliver is the router's one write, and it NEVER BLOCKS (see
// deliveryTaskInboxCapacity's comment for the deadlock it would otherwise open). An
// unnamed TaskID is a caller error the Manager should have refused, so it is logged
// loudly and dropped rather than broadcast — a signal delivered to every task is worse
// than one delivered to none.
func (ws *walkState) deliver(logger log.Logger, msg routedSignal) {
	switch {
	case msg.TaskID == "":
		logger.Error("signal names no task; dropped", "kind", msg.Kind)
	case ws.inbox[msg.TaskID] != nil:
		// A full inbox falls back to `pending`, which is where an undelivered message
		// belongs anyway: the receiver drains it on its next loop iteration, and
		// ContinueAsNew carries it.
		if !ws.inbox[msg.TaskID].SendAsync(msg) {
			ws.pending[msg.TaskID] = append(ws.pending[msg.TaskID], msg)
			return
		}
		ws.recordDelivery(msg)
	case ws.byTask[msg.TaskID] == walkTaskPending:
		ws.pending[msg.TaskID] = append(ws.pending[msg.TaskID], msg)
	default:
		logger.Info("signal arrived for a task that has already finished; dropped",
			"kind", msg.Kind, "taskId", msg.TaskID, "state", ws.byTask[msg.TaskID])
		if ws.rec != nil {
			ws.rec.signalDropped(msg.TaskID, msg.Kind)
		}
	}
}

// recordDelivery tells the shape oracle which task a message actually REACHED — an inbox
// took it, whether straight from the router or pulled through from `pending`. A message that overflowed to `pending` is deliberately NOT recorded: it has not
// been delivered to anything, and counting it would make the overflow case unfalsifiable.
// Nil in production.
func (ws *walkState) recordDelivery(msg routedSignal) {
	if ws.rec != nil {
		ws.rec.signalDelivered(msg.TaskID, msg.Kind)
	}
}

// ---------------------------------------------------------------------------
// ONE TASK
// ---------------------------------------------------------------------------

// runTask runs ONE task to its terminal. It is the only function in the walk that
// reads a task's kind, and it reads only the kind — never the activity type.
//
// A DISPATCH task: produce, record the attempt, done. Its gate belongs to the REVIEW
// task that judges it, which is a separate node.
// A REVIEW task: open the round with the roster the engine proposes, run the agent
// reviewers the data names, then either auto-pass (the engine says no human is
// required) or SUSPEND on the gate signal. Approve commits; send back records the
// verdict and its comments and returns walkTaskSentBack, which is what re-opens the pair.
// inbox is THIS task's channel and the only one it may read. It carries all four signal
// kinds for this task and nothing for any other, so nothing downstream filters by
// TaskID and nothing can steal a sibling's message.
func (wf *csWorkflows) runTask(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	t methodassets.LifecycleTask, ws *walkState, state *constructState,
	policy projectstate.ReviewPolicy, inbox workflow.ReceiveChannel,
) (walkTaskState, error) {
	strat, err := strategyFor(wf.Strategies, wf, lc, t)
	if err != nil {
		return walkTaskFailed, err
	}
	phase, _ := lifecyclePhaseByID(lc, t.Phase)
	tc := taskContext{
		In: in, Task: t, Phase: phase, Lifecycle: lc,
		Revision: ws.revision[t.ID], Attempt: int(ws.revision[t.ID]) + 1,
		State: state, Feedback: ws.feedback[t.ID], Inbox: inbox,
		Drain: func() { ws.drainPending(t.ID) },
	}

	produced, perr := strat.Produce(ctx, tc)
	// THE ATTEMPT IS RECORDED EVEN WHEN THE PRODUCTION FAILED, and that is a fix rather than a
	// nicety (stage 4b1 Task 10). Both failing strategies return a FAILED producedSubject
	// ALONGSIDE their error, precisely so the ledger says what happened while the error fails the
	// walk — and Task 8's ordering returned before recording anything, so the two claims
	// sdpComputeStrategy and the design arm make in their own doc comments were both false: an
	// operator saw a failed activity with an EMPTY attempt ledger and no diagnostic anywhere but
	// the workflow error. Recorded first, then returned. A recording failure on top of a
	// production failure keeps the PRODUCTION error, because that is the cause.
	// AttemptRecorded is the strategy saying it already wrote this task's attempts — which the
	// construction arm does, because ONE task can hold several numbered dispatches under its
	// variance loop and a single record here would erase all but the last (producedSubject).
	if produced.AttemptID != "" && !produced.AttemptRecorded {
		if err := wf.recordTaskAttempt(ctx, in, t, tc, produced, state); err != nil && perr == nil {
			return walkTaskFailed, err
		}
	}
	if perr != nil {
		return walkTaskFailed, perr
	}
	// THE ACTIVITY ALREADY EXITED, inside this task. The variance loop's three terminal answers
	// record the activity's own outcome — Skipped, or Unknown carrying VarianceExhausted or
	// EscalationTimedOut — so the walk must stop WITHOUT recording a second one over it. It is a
	// flag and not an inferred outcome because the walk cannot tell those three apart, and
	// reporting the same terminal for all three is how "the operator skipped it" becomes
	// "it ran out of attempts" in the one view a founder reads.
	if produced.ActivityExited {
		ws.produced[t.ID] = produced
		return walkTaskExited, nil
	}
	// The output is what a REVIEW task will cite, keyed by the task that produced it, so
	// runGate can reach it through t.Reviews — a review task with a `reviews` target
	// produces nothing of its own, and without this hand-off every round would cite the
	// third rung of gateSubjectRef's ladder. A COMPUTE review (no `reviews` target)
	// produced its own subject, which is why this is not gated on the kind.
	ws.produced[t.ID] = produced
	if t.Kind == methodassets.LifecycleTaskDispatch {
		return walkTaskPassed, nil
	}
	return wf.runGate(ctx, in, lc, t, tc, ws, state, policy, inbox)
}

// judgedSubject is the subject a review round cites: the output of the task the review
// JUDGES, or — for the one row that names no `reviews` target — its own, because the
// compute strategy registered for its artifactKind produced it under this task's id.
func judgedSubject(ws *walkState, t methodassets.LifecycleTask) producedSubject {
	if t.Reviews != "" {
		return ws.produced[t.Reviews]
	}
	return ws.produced[t.ID]
}

// roundJudges is the task a round records as its subject's producer. The store refuses a
// round with an empty `reviews` ("a round that judges no task judges nothing"), and for
// the compute row that producer IS the review task itself.
func roundJudges(t methodassets.LifecycleTask) projectstate.MethodTask {
	if t.Reviews != "" {
		return projectstate.MethodTask(t.Reviews)
	}
	return projectstate.MethodTask(t.ID)
}

// runGate is the review half: the roster, the agent reviewers, and the suspend.
//
// The ROSTER comes from the reviewEngine, ONE injected instance (wf.Review), called with
// the activity type and the task's own PHASE. That phase is what deletes the third copy
// of the design slot->activity/phase table: the lifecycle data already answers what
// designActivityFor answered, because missionReview.phase is "mission" and
// sdpReview.phase is "sdp".
//
// The AGENT REVIEWERS are the review task's own command, when it has one. In v0.9.0
// missionReview, glossaryReview, coreUseCasesReview and architectureReview each carry
// a *-critique command and a workerClass, and volatilitiesReview carries neither —
// which is exactly the one reviewer-less row the engine's phaseVolatilities constant
// already names. So the critique round is not a special case any more: it is a review
// task with a command.
func (wf *csWorkflows) runGate(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	t methodassets.LifecycleTask, tc taskContext,
	ws *walkState, state *constructState, policy projectstate.ReviewPolicy,
	inbox workflow.ReceiveChannel,
) (walkTaskState, error) {
	set, err := wf.proposeReviewSet(in.csIn(), tc.Phase, policy, state)
	if err != nil {
		return walkTaskFailed, err
	}
	// The round's SUBJECT and its KIND both come from the task this review JUDGES, not
	// from the review task itself.
	//
	// The subject: judgedSubject is the staged ref and attempt id the judged dispatch
	// reported. A review with a `reviews` target produces nothing of its own, so reading
	// its own producedSubject here would hand gateSubjectRef two empty strings and every
	// round would cite the ladder's third rung.
	//
	// The kind: MEASURED in method-assets v0.9.0 — NO review task carries an
	// artifactKind of its own except `sdpReview`; missionReview, glossaryReview,
	// volatilitiesReview, coreUseCasesReview, architectureReview, srsReview,
	// designReview, codeReview, stpReview and testing all carry "". So the kind is the
	// JUDGED task's, and `sdpReview` — which names no `reviews` target — is the one
	// task that answers with its own. Pinned by
	// Test_ReviewRounds_KindComesFromTheJudgedTask over all fourteen lifecycles.
	var gate gateLedger
	if err := wf.openRound(ctx, in, t, tc, judgedSubject(ws, t), roundArtifactKind(lc, t), set, state, &gate); err != nil {
		return walkTaskFailed, err
	}
	// THE CRITIQUE ROUND, now an ordinary reviewer (stage 4b1 Task 10). The retired rail ran
	// it as a phase of the co-author spine — ~300 lines of bespoke machinery around one
	// dispatch, with its own critic table, its own verdict vocabulary and its own retry
	// budget. Here it is a review task that carries a command, dispatched through the SAME
	// strategy slot a work task's dispatch uses, landing an ordinary ReviewVerdict.
	//
	// THE CRITIC'S ANSWER IS TWO FACTS, and criticHoldsTheGate is where they meet the policy:
	// whether it judged at all, and — since stage 4b1 Task 13 — WHAT it judged. A critique that
	// ran and committed nothing has NOT judged this draft, and a critique that asked for a
	// REVISE has judged it badly; the policy's no-human arm below would close the gate on both.
	// The first is the silent approve readBackCritiqueOn's safe default was built to refuse; the
	// second is the same silence wearing a verdict. Under a `vibes` preset the pair is the
	// difference between "nobody had to look" and "nobody did look".
	criticVerdict, criticJudged := projectstate.VerdictApprove, true
	if t.Command != "" {
		verdict, judged, err := wf.runAgentReviewers(ctx, in, t, tc, state, &gate)
		if err != nil {
			return walkTaskFailed, err
		}
		criticVerdict, criticJudged = verdict, judged
	}
	if reason, held := criticHoldsTheGate(criticVerdict, criticJudged); held {
		workflow.GetLogger(ctx).Warn(reason, "activityId", in.ActivityID, "taskId", t.ID,
			"roundId", gate.roundID, "criticVerdict", string(criticVerdict))
		state.reviewSet, state.reviewSetError = &set, ""
		state.stage = StageAwaitingApproval
		// A HUMAN gate, not a held autogate: neither of the two holds is released by a resolve.
		// A critic that did not judge has nothing for a resolve to address, and a critic that
		// asked for a revise filed no comment to resolve (runAgentReviewers appends its verdict
		// with a nil comment list).
		return wf.awaitTaskDecision(ctx, in, lc, t, tc, ws, state, &gate, inbox, nil)
	}
	if set.RequiresHuman == nil || !*set.RequiresHuman {
		// AN OPEN COMMENT HOLDS THE AUTOGATE (stage 4b1 Task 12, fix round 2, review finding F1).
		// The retired rail made "no open comments" a PRECONDITION of the synthesized approve
		// (coauthorartifact.go's autogate) and this child had no such check at all: a change
		// request — or a question the founder filed while the critic was still running — landed on
		// the round and the gate passed anyway, with nobody ever seeing it.
		//
		// ANY open comment holds here, QUESTIONS INCLUDED, and that is deliberately STRICTER than
		// the human approve below. An open question is doctrine-bound to be "a soft warning at the
		// approve gate, never a hard block" (ReviewCommentBlocksApprove) — but a warning needs
		// somebody to warn. The autogate's whole premise is that nobody has to look, so a comment
		// nobody will ever read is the one thing it must not synthesize an approve over. A human
		// gate keeps the ratified rule: change requests block, questions warn.
		held, err := wf.roundHoldsOpenComments(ctx, in, state, gate.roundID)
		switch {
		case err != nil:
			// A read that failed is NOT an empty thread. Holding for a human is the safe default —
			// the alternative is the silent ratification this whole arm exists to refuse.
			workflow.GetLogger(ctx).Error("the round's thread could not be read; the gate holds for a human rather than auto-passing",
				"activityId", in.ActivityID, "taskId", t.ID, "err", err.Error())
		case !held:
			// No roster goes up for a gate nobody is asked to answer, mirroring the retired rail's
			// no-human arm: a reviewer set left on the session view outlives the occurrence it
			// described, and the Activity Experience reads one as a LIVE gate.
			state.reviewSet, state.reviewSetError = nil, ""
			return wf.passRound(ctx, in, lc, t, tc, state, &gate, gateActorSystem, reasonOf(set))
		default:
			workflow.GetLogger(ctx).Warn("delivery.gate.heldOnOpenComments",
				"activityId", in.ActivityID, "taskId", t.ID, "roundId", gate.roundID,
				"reason", "the review policy asked for no human, but this round carries an open comment")
		}
		// HELD, NOT DECIDED: the roster goes up so the screen shows a live gate, and the gate
		// auto-passes the moment the last open comment is resolved (awaitTaskDecision's autogate
		// arm) — which is what makes the Manager's comment-status mirror signal load-bearing.
		state.reviewSet, state.reviewSetError = &set, ""
		state.stage = StageAwaitingApproval
		return wf.awaitTaskDecision(ctx, in, lc, t, tc, ws, state, &gate, inbox, autogateOn(set))
	}
	state.reviewSet, state.reviewSetError = &set, ""
	state.stage = StageAwaitingApproval
	return wf.awaitTaskDecision(ctx, in, lc, t, tc, ws, state, &gate, inbox, nil)
}

// autogateOn is the reason a HELD autogate will pass with, once its round has no open comment
// left. Carrying the ReviewSet's own reason rather than a literal keeps the ledger's account of
// WHY a round passed identical whether the comments were clear at the start or cleared later.
func autogateOn(set ReviewSet) *string {
	reason := reasonOf(set)
	return &reason
}

// roundHoldsOpenComments reports whether the round carries ANY open comment — change request or
// question. It is a fresh READ, because the comments it is about are written by a human through
// the Manager while this gate is open, and the child holds no copy of the thread.
func (wf *csWorkflows) roundHoldsOpenComments(
	ctx workflow.Context, in deliveryActivityInput, state *constructState, roundID string,
) (bool, error) {
	round, err := wf.readRound(ctx, in, state, roundID)
	if err != nil {
		return false, err
	}
	for _, c := range round.Thread {
		if c.Status == projectstate.ReviewCommentOpen {
			return true, nil
		}
	}
	return false, nil
}

// roundBlockingComments is the HUMAN approve's own question: the open CHANGE REQUESTS on this
// round (projectstate.OpenReviewCommentIDs — open questions are a soft warning, never a block).
func (wf *csWorkflows) roundBlockingComments(
	ctx workflow.Context, in deliveryActivityInput, state *constructState, roundID string,
) ([]string, error) {
	round, err := wf.readRound(ctx, in, state, roundID)
	if err != nil {
		return nil, err
	}
	return projectstate.OpenReviewCommentIDs(round.Thread), nil
}

// readRound reads ONE round off the activity's row through the narrow execution read. A round the
// row does not hold is an empty round rather than an error: the only caller that can see that is
// a gate whose own round vanished, which no verb can do.
func (wf *csWorkflows) readRound(
	ctx workflow.Context, in deliveryActivityInput, state *constructState, roundID string,
) (projectstate.ReviewRound, error) {
	if roundID == "" || !state.executionLedger {
		return projectstate.ReviewRound{}, nil
	}
	row, err := wf.Acts.ActivityExecutionReadActivityExecution(ctx, projectstate.ProjectID(in.ProjectID), string(in.ActivityID))
	if err != nil {
		return projectstate.ReviewRound{}, err
	}
	for _, r := range row.Reviews {
		if r.RoundID == roundID {
			return r, nil
		}
	}
	return projectstate.ReviewRound{}, nil
}

// roundArtifactKind resolves what a review round judges: the review task's own
// artifactKind when it has one (only `sdpReview` does), else the artifactKind of the
// task its `reviews` field names. Returns nil for a construction round, whose subject
// is a commit and not a slot model, which is exactly what ReviewRound.ArtifactKind's
// optionality means.
func roundArtifactKind(lc methodassets.Lifecycle, t methodassets.LifecycleTask) *projectstate.ArtifactKind {
	name := t.ArtifactKind
	if name == "" && t.Reviews != "" {
		if judged, ok := lifecycleTaskByID(lc, t.Reviews); ok {
			name = judged.ArtifactKind
		}
	}
	if name == "" {
		return nil
	}
	k, ok := projectstate.ArtifactKindFromWireName(lowerFirstRune(name))
	if !ok {
		return nil
	}
	return &k
}

// openRound opens the review round this gate is about to run, with the roster the engine
// computed, the subject the JUDGED task produced and the artifact kind that task names.
// The gate identity is written into an out-parameter rather than onto constructState,
// because the walk runs several gates AT ONCE on a fork and constructState.gate holds
// exactly one — see gateLedger.
func (wf *csWorkflows) openRound(
	ctx workflow.Context, in deliveryActivityInput, t methodassets.LifecycleTask, tc taskContext,
	judged producedSubject, kind *projectstate.ArtifactKind, set ReviewSet,
	state *constructState, gate *gateLedger,
) error {
	task := projectstate.MethodTask(t.ID)
	n := int(tc.Revision) + 1
	*gate = gateLedger{
		task:            task,
		number:          n,
		roundID:         projectstate.AttemptID(string(in.ActivityID), task, n),
		subject:         gateSubjectRef(&state.walk.gf, judged.StagedRef, judged.AttemptID),
		judgedAttemptID: judged.AttemptID,
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, state.walk.headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionOpenReviewRound(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID),
			projectstate.ReviewRoundInput{
				RoundID:      gate.roundID,
				TaskID:       task,
				Reviews:      roundJudges(t),
				ArtifactKind: kind,
				Round:        int64(n),
				SubjectRef:   gate.subject,
				Reviewers:    roundReviewers(set),
			}, state.walk.cred.toProjectState())
	})
	if err != nil {
		return err
	}
	state.walk.headVersion = v
	state.rowAdvanced()
	return nil
}

// runAgentReviewers dispatches the review task's OWN command — its agent critics — and
// lands what they answered on the round as a verdict. This is what stops the critique
// round being a special case: it is a review task with a command, dispatched through the
// SAME strategy slot a work task's dispatch uses, so nothing here knows which command it
// is running.
//
// THE THREE ARMS, and each says something different about the draft:
//
//	passed   -> APPROVE.  The critic read it and had no blocking objection.
//	rejected -> SEND BACK. The critic asked for a revise, which is a verdict somebody DID
//	            cast. Recording it as an abstention would erase the one thing the critique
//	            produced, and the round's verdict list is what the human reads.
//	anything -> ABSTAIN carrying the diagnostic, and judged=false. A critic that did not
//	            finish has not judged; recording its silence as a rejection would put a
//	            verdict in the ledger nobody cast, and recording it as an approve is the
//	            silent ratification the safe default exists to refuse.
//
// judged is what the caller uses to hold the gate for a human when the critic did not answer.
// It does NOT fail the task: the retired rail's whole anti-wedge discipline was that a
// ran-but-incomplete critique lands at a human gate rather than crashing the session, and the
// child's equivalent of that gate is this one, held open.
func (wf *csWorkflows) runAgentReviewers(
	ctx workflow.Context, in deliveryActivityInput, t methodassets.LifecycleTask,
	tc taskContext, state *constructState, gate *gateLedger,
) (cast projectstate.VerdictKind, judged bool, err error) {
	ctor, ok := wf.Strategies[strategySlotDispatch]
	if !ok {
		return projectstate.VerdictAbstain, false, newError(fwmanager.FailedPrecondition,
			"review task "+t.ID+" names agent reviewers but nothing has registered strategy slot "+strategySlotDispatch)
	}
	produced, perr := ctor(wf).Produce(ctx, tc)
	if perr != nil {
		return projectstate.VerdictAbstain, false, perr
	}
	verdict, judged := criticVerdictFor(produced.Outcome)
	return verdict, judged, wf.appendVerdict(ctx, in.csIn(), state, gate, &state.walk.headVersion, state.walk.cred,
		projectstate.ReviewVerdict{
			ReviewerRole: t.WorkerClass,
			Actor:        t.WorkerClass,
			Verdict:      verdict,
			Summary:      produced.Detail,
			AttemptID:    gate.judgedAttemptID,
		}, nil)
}

// criticHoldsTheGate is the HUMAN FLOOR under an agent critic: given the verdict the critic
// cast and whether it judged at all, it answers whether the gate must be held for a human
// whatever the review policy says, and the sentence the log records for it.
//
// TWO CASES, and the second is stage 4b1 Task 13's (Task 10's concern 7, Task 12's fix round 2
// carry):
//
//   - NOT JUDGED — a critique that ran and committed nothing. The autogate's premise is that
//     nobody has to look, so a draft nobody looked at is the one thing it must not ratify.
//   - SEND-BACK — the critic asked for a revise, with or without comments. Auto-passing here
//     records a round that PASSED carrying its own reviewer's rejection, which no reader can
//     explain. The alternative — synthesizing the redraft the critic asked for — is a loop
//     with no bound under `vibes`: the critic that rejected draft n rejects draft n+1 for the
//     same reason, `maxPhaseRedrafts` is spent without a human ever being told, and the
//     activity fails WalkStalled with nothing on the record naming a decision. So a human is
//     asked, which is the redraft's one legitimate author.
//
// An ABSTENTION does NOT hold on its own account: an abstain from a critic that judged is
// impossible by criticVerdictFor's construction (every non-judging outcome abstains and is
// caught by the first case), and reading "abstain" as a hold would make a future
// judged-but-neutral verdict a gate nobody asked for.
func criticHoldsTheGate(verdict projectstate.VerdictKind, judged bool) (string, bool) {
	if !judged {
		return "the agent critic did not judge this draft; the gate holds for a human whatever the policy says", true
	}
	if verdict == projectstate.VerdictSendBack {
		return "the agent critic asked for a revise; the gate holds for a human rather than auto-passing or looping on redrafts", true
	}
	return "", false
}

// criticVerdictFor maps what the critique produced onto the verdict the round records, and
// says whether the critic JUDGED at all. Exhaustive over TaskOutcome so a new outcome must
// decide what it means for a critic rather than inheriting "abstain".
func criticVerdictFor(outcome projectstate.TaskOutcome) (projectstate.VerdictKind, bool) {
	switch outcome {
	case projectstate.OutcomePassed:
		return projectstate.VerdictApprove, true
	case projectstate.OutcomeRejected:
		return projectstate.VerdictSendBack, true
	case projectstate.OutcomeFailed, projectstate.OutcomePending, projectstate.OutcomeSkipped:
		return projectstate.VerdictAbstain, false
	}
	return projectstate.VerdictAbstain, false
}

// passRound is the gate's PASS: the round decided passed and the gate attempt recorded,
// which is App A's binary exit criterion and the one fact every reader derives a
// lifecycle phase's completion from.
//
// It takes lc because Task 9's M0 handler asks the lifecycle whether this is the M0 gate.
func (wf *csWorkflows) passRound(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	t methodassets.LifecycleTask, tc taskContext, state *constructState, gate *gateLedger,
	decidedBy, reason string,
) (walkTaskState, error) {
	if reason != "" {
		workflow.GetLogger(ctx).Info("delivery.gate.passed", "activityId", in.ActivityID,
			"taskId", t.ID, "decidedBy", decidedBy, "reason", reason)
	}
	csIn := in.csIn()
	if err := wf.decideRound(ctx, csIn, state, gate, &state.walk.headVersion, state.walk.cred,
		projectstate.RoundPassed, decidedBy); err != nil {
		return walkTaskFailed, err
	}
	if err := wf.passGateAttempt(ctx, csIn, state, gate, &state.walk.headVersion, state.walk.cred); err != nil {
		return walkTaskFailed, err
	}
	// The phase this gate exits, marked on the run's own skip-guard. It is read off the
	// resolved PHASE rather than off the task, because the phase is what a lifecycle's gate
	// belongs to and tc already holds the resolved struct.
	state.completedPhases[projectstate.ActivityMethodPhase(tc.Phase.ID)] = true
	// M0 COMMITS THE DERIVED PLAN AND MOVES THE ROOT PHASE, and the spec's "derived from
	// milestone position" is amended to say so (stage 4b1, R4; derivation is stage 6).
	//
	// MEASURED, and this is the whole reason the write is here: nextEligibleActivity returns
	// verdictQuiescent unless proj.Phase == PhaseConstruction, and PumpSweepWorkflow filters
	// on the same, while AdvancePhase is literally p.Phase++. So a gate-passed handler that
	// did not advance would approve the plan and then leave the pump permanently quiet —
	// construction would never start, and nothing would say why.
	if isM0Gate(lc, t) {
		if err := wf.completeProjectDesign(ctx, in, state); err != nil {
			return taskFailedFromM0(ctx, in, err)
		}
	}
	return walkTaskPassed, nil
}

// taskFailedFromM0 names the failure the M0 seal could not complete. Split out so
// passRound's tail stays one statement per fact.
func taskFailedFromM0(ctx workflow.Context, in deliveryActivityInput, err error) (walkTaskState, error) {
	workflow.GetLogger(ctx).Error("delivery.projectDesign.sealFailed",
		"activityId", in.ActivityID, "err", err.Error())
	return walkTaskFailed, err
}

// isM0Gate reads the DATA and nothing else: a REVIEW task that judges no other task and
// names the SDP review as its own artifact kind. That is the `projectDesign` lifecycle's
// single row and it is the only shape in method-assets v0.9.0 that matches — every other
// review task either names a `reviews` target or carries no artifactKind at all (pinned by
// Test_ReviewRounds_KindComesFromTheJudgedTask).
//
// It reads the data so the WALKER still names no activity type: the M0 seal is a property
// of the lifecycle's shape, not of a type the walk switches on. lc is taken (and not just
// t) because "this is the whole lifecycle's only task" is the half that makes the shape
// unambiguous — a future lifecycle that grew a second compute row would stop matching here
// rather than silently sealing a project mid-walk.
func isM0Gate(lc methodassets.Lifecycle, t methodassets.LifecycleTask) bool {
	return t.Kind == methodassets.LifecycleTaskReview &&
		t.Reviews == "" &&
		t.ArtifactKind == artifactKindSdpReview &&
		len(lc.Tasks) == 1
}

// completeProjectDesign is M0's approve: the eight computed slots are COMMITTED and the
// root phase advances.
//
// The commit comes first and the advance second, deliberately: a phase advanced over
// slots still sitting in AwaitingReview would put the pump into construction against a
// plan nobody committed, and committedPlanInputs would then find nothing to dispatch. The
// reverse order fails safe — committed slots with the phase unmoved is a project that can
// be re-approved.
//
// It is IDEMPOTENT by the phase's own ordering: the advance is skipped unless the re-read
// phase is still BELOW construction, so a replay or a re-decided round cannot push the
// project past it. The slot commits are idempotent in the store (committing a committed
// slot is a no-op success).
func (wf *csWorkflows) completeProjectDesign(ctx workflow.Context, in deliveryActivityInput, state *constructState) error {
	for _, kind := range projectDesignComputedKinds() {
		v, err := wf.applyRecovering(ctx, in.ProjectID, state.walk.headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			return wf.Acts.DesignSessionCommitArtifactWithProvenance(ctx, projectstate.ProjectID(in.ProjectID), expected,
				kind, gateActorOperator, projectDesignDraftedBy)
		})
		if err != nil {
			return err
		}
		state.walk.headVersion = v
	}
	return wf.advanceToConstruction(ctx, in, state)
}

// projectDesignDraftedBy is the draftedBy provenance on every slot M0 commits. It says
// PLATFORM rather than an agent charter because nothing drafted these: the plan is derived
// and the numbers are doctrine, so attributing them to an agent would invent an author.
const projectDesignDraftedBy = "platform:deterministic-project-design"

// advanceToConstruction seals Phase 2. Guarded on the READ-BACK phase rather than on the
// run's own belief, because the seal is reachable from a replay and from a re-decided
// round, and AdvancePhase has no ceiling of its own.
func (wf *csWorkflows) advanceToConstruction(ctx workflow.Context, in deliveryActivityInput, state *constructState) error {
	proj, err := wf.readProject(ctx, in.ProjectID)
	if err != nil && !isReadNotFound(err) {
		return err
	}
	if proj.Phase >= projectstate.PhaseConstruction {
		workflow.GetLogger(ctx).Info("delivery.projectDesign.alreadySealed",
			"activityId", in.ActivityID, "phase", int(proj.Phase))
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, state.walk.headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ProjectStateAdvancePhase(ctx, projectstate.ProjectID(in.ProjectID), expected)
	})
	if err != nil {
		return err
	}
	state.walk.headVersion = v
	return nil
}

// reasonOf is the engine's one-line reason, "" when the set carries none.
func reasonOf(set ReviewSet) string {
	if set.Reason == nil {
		return ""
	}
	return *set.Reason
}

// gateActorSystem is who the ledger records when the committed review policy — not a
// person — closed the gate. Beside gateActorOperator (constructactivity.go).
const gateActorSystem = "system"

// lifecyclePhaseByID finds a lifecycle's phase by its id.
func lifecyclePhaseByID(lc methodassets.Lifecycle, phaseID string) (methodassets.LifecyclePhase, bool) {
	for _, p := range lc.Phases {
		if p.ID == phaseID {
			return p, true
		}
	}
	return methodassets.LifecyclePhase{}, false
}

// ---------------------------------------------------------------------------
// THE GATE'S SUSPEND
// ---------------------------------------------------------------------------

// awaitTaskDecision suspends this task's gate until a person answers it, reading ONE
// channel — its own inbox — and dispatching on the message kind. It carries the existing
// gate's vocabulary and budgets verbatim: maxPhaseRedrafts, the outcome strings, and the
// enterPhaseGate / enterHumanStage / leaveHumanStage human-stage bookkeeping.
//
// NO TaskID FILTER APPEARS HERE. The inbox is per-task by construction, which is what
// replaces the construction gate's filter-by-gate-KEY and the design session's implicit
// one-session-one-kind assumption.
//
// RESOLVE BEFORE DECISION. A comment status is applied before a decision is acted on, so
// a bulk resolve lands first. That guarantee is STRONGER here than the old registration
// order it replaces: the router already put the messages in history order, so this loop
// does not have to prefer one channel over another to get it.
//
// EVERY ITERATION BEGINS WITH drainPending. The router cannot wait for a slot (it never
// blocks), so the RECEIVER is what pulls an overflowed message through, and the one
// moment a slot is known to have freed is just after a receive. Skip that call and a 65th
// signal sits in `pending` until the task retires.
//
// A SEND-BACK PAST THE BUDGET KEEPS AWAITING rather than failing, exactly as sendBackGate
// does today. Changing that is not in this wave.
// autogateReason, when non-nil, says this gate is held ONLY because its round carries an open
// comment: the review policy asked for no human. The moment the last one is resolved the gate
// passes itself, with that reason.
func (wf *csWorkflows) awaitTaskDecision(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	t methodassets.LifecycleTask, tc taskContext, ws *walkState, state *constructState,
	gate *gateLedger, inbox workflow.ReceiveChannel, autogateReason *string,
) (walkTaskState, error) {
	redraft := 0
	activityType := in.Activity.activityTypeName()
	state.enterPhaseGate(ctx, t.ID, redraft)
	for {
		ws.drainPending(t.ID)
		var msg routedSignal
		inbox.Receive(ctx, &msg)
		// EVERY PAYLOAD IS NIL-CHECKED, not just the two whose helpers guard (Task 8 review
		// finding 4). routedSignal round-trips through walkSnapshot.Pending as JSON, so a
		// message whose Kind and payload pointer disagree — a hand-crafted signal, a payload
		// the codec could not decode, a snapshot written by an older image — arrives here with
		// a nil pointer. Dereferencing it panics the WORKFLOW TASK, which Temporal retries
		// forever: the activity never moves and the ledger says nothing. So the missing payload
		// is logged and dropped, the same as the two guarded helpers already do, and the gate
		// keeps awaiting the decision it is actually there for.
		if !routedPayloadPresent(msg) {
			workflow.GetLogger(ctx).Error("a routed signal arrived with no payload for its kind; dropped",
				"activityId", in.ActivityID, "taskId", t.ID, "kind", msg.Kind)
			continue
		}
		switch msg.Kind {
		case routedKindStatus:
			// THE MIRROR SIGNAL'S ONE JOB (fix round 2): the Manager already wrote the status, and
			// this is the child re-asking whether a HELD AUTOGATE can now pass. Without it the
			// operator would resolve the last comment and the gate would sit there for ever with
			// nobody left who has to decide it.
			st, done, err := wf.reconsiderAutogate(ctx, in, lc, t, tc, state, gate, autogateReason, msg.Status)
			if err != nil {
				return walkTaskFailed, err
			}
			if done {
				return st, nil
			}
		case routedKindOverride:
			wf.recordGateOverride(ctx, in, state, t, msg.Override)
		case routedKindRedraft:
			// A re-draft at M0 is the same refusal a send-back is (noSendBackAtM0): there is no
			// draft to re-run — the plan is DERIVED — and withdrawing the round would strand the
			// walk exactly as a send-back would. The gate keeps awaiting its approve.
			if isM0Gate(lc, t) {
				logNoSendBackAtM0(ctx, in, t, "redraft")
				continue
			}
			// The operator asked for a re-draft instead of judging what is in front of them,
			// so the round is WITHDRAWN (nobody judged it) and the judged pair re-opens —
			// which IS the re-dispatch, expressed in the walker's own vocabulary. Executing
			// the re-run's own dispatch inputs is Task 12's.
			state.leaveHumanStage(ctx, activityType, gateOutcomeSentBack)
			if err := wf.decideRound(ctx, in.csIn(), state, gate, &state.walk.headVersion, state.walk.cred,
				projectstate.RoundWithdrawn, gateActorOperator); err != nil {
				return walkTaskFailed, err
			}
			ws.feedback[string(roundJudges(t))] = signalNotes(msg.Redraft.Feedback)
			return walkTaskSentBack, nil
		case routedKindDecision:
			st, done, err := wf.decideTaskGate(ctx, in, lc, t, tc, ws, state, gate, msg.Decision, &redraft)
			if err != nil {
				return walkTaskFailed, err
			}
			if done {
				return st, nil
			}
		}
	}
}

// routedPayloadPresent reports whether a routed signal carries the payload its Kind names.
// The four kinds each have exactly one pointer, and an unknown kind carries none — which is
// also "not present", because a message this build cannot interpret must be dropped rather
// than silently taken for one of the four.
func routedPayloadPresent(msg routedSignal) bool {
	switch msg.Kind {
	case routedKindDecision:
		return msg.Decision != nil
	case routedKindStatus:
		return msg.Status != nil
	case routedKindOverride:
		return msg.Override != nil
	case routedKindRedraft:
		return msg.Redraft != nil
	}
	return false
}

// reconsiderAutogate is what a comment-status signal does at a gate (stage 4b1 Task 12, fix
// round 2). The Manager owns the WRITE — it landed the transition synchronously, so it could tell
// the reviewer that the comment does not exist or that the transition is illegal — and this is
// the half only the child can do: re-evaluate a gate that is held ONLY because the round carried
// an open comment, and pass it when the last one is gone.
//
// done=false keeps the gate awaiting, which is every case but the autogate's own release: a gate
// that a HUMAN has to answer is not affected by a resolve, and a nil autogateReason says so.
func (wf *csWorkflows) reconsiderAutogate(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	t methodassets.LifecycleTask, tc taskContext, state *constructState,
	gate *gateLedger, autogateReason *string, sig *setCommentStatusSignal,
) (walkTaskState, bool, error) {
	logger := workflow.GetLogger(ctx)
	if autogateReason == nil {
		logger.Info("a comment status was mirrored to a gate a human must answer; nothing to reconsider",
			"activityId", in.ActivityID, "taskId", t.ID, "commentId", sig.CommentID)
		return walkTaskFailed, false, nil
	}
	held, err := wf.roundHoldsOpenComments(ctx, in, state, gate.roundID)
	if err != nil {
		logger.Error("the round's thread could not be re-read; the gate keeps awaiting",
			"activityId", in.ActivityID, "taskId", t.ID, "err", err.Error())
		return walkTaskFailed, false, nil
	}
	if held {
		logger.Info("delivery.gate.stillHeldOnOpenComments",
			"activityId", in.ActivityID, "taskId", t.ID, "commentId", sig.CommentID)
		return walkTaskFailed, false, nil
	}
	state.leaveHumanStage(ctx, in.Activity.activityTypeName(), gateOutcomeApproved)
	state.reviewSet, state.reviewSetError = nil, ""
	st, err := wf.passRound(ctx, in, lc, t, tc, state, gate, gateActorSystem, *autogateReason)
	return st, true, err
}

// decideTaskGate acts on ONE decision at a gate. done=false means the gate keeps
// awaiting: an unknown decision, or a send-back whose human-paced budget is spent.
func (wf *csWorkflows) decideTaskGate(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	t methodassets.LifecycleTask, tc taskContext, ws *walkState, state *constructState,
	gate *gateLedger, sig *taskDecisionSignal, redraft *int,
) (walkTaskState, bool, error) {
	activityType := in.Activity.activityTypeName()
	switch sig.Decision {
	case ReviewApprove:
		// THE TOCTOU RE-CHECK (fix round 2, review finding F1). The façade refuses an approve while
		// a change request is open, but a signal is fire-and-forget: a comment filed in the
		// millisecond after that check would otherwise be approved over. The retired rail re-checked
		// in the same place and for the same reason. Open QUESTIONS deliberately do NOT block — a
		// human is looking at them (ReviewCommentBlocksApprove).
		if open, err := wf.roundBlockingComments(ctx, in, state, gate.roundID); err != nil {
			workflow.GetLogger(ctx).Error("the round's thread could not be read; the approve is not applied and the gate keeps awaiting",
				"activityId", in.ActivityID, "taskId", t.ID, "err", err.Error())
			return walkTaskFailed, false, nil
		} else if len(open) > 0 {
			workflow.GetLogger(ctx).Warn("delivery.gate.approveRefusedOnOpenComments",
				"activityId", in.ActivityID, "taskId", t.ID, "open", len(open))
			return walkTaskFailed, false, nil
		}
		state.leaveHumanStage(ctx, activityType, gateOutcomeApproved)
		if err := wf.closeGateRound(ctx, in.csIn(), state, gate, &state.walk.headVersion, state.walk.cred,
			projectstate.VerdictApprove, projectstate.RoundPassed, sig.Feedback); err != nil {
			return walkTaskFailed, true, err
		}
		st, err := wf.passRound(ctx, in, lc, t, tc, state, gate, gateActorOperator, decidedByOperator)
		return st, true, err
	case ReviewReject:
		// M0 HAS NO SEND-BACK, AND THE GATE IS WHERE THAT IS ENFORCED (Task 8 review finding
		// 6). Recording the rejection instead would leave the ledger in a state no reader can
		// explain: sendBackRound writes a sendBack verdict and a REJECTED gate attempt,
		// reopenJudgedPair then does NOTHING (the row names no `reviews` target, so there is no
		// pair to re-open), finalizeWalk finds a task that never passed and fails the activity
		// WalkStalled/VarianceExhausted — a rejection filed against a variance-exhausted
		// activity, for a decision the product does not offer.
		//
		// So the round stays OPEN, awaiting its approve, and nothing is written. The typed
		// refusal the founder actually sees is at the façade
		// (deliveryManager.SubmitReviewDecision, which answers FailedPrecondition naming the
		// amendment path and matches the SPA's own NO_SDP_SEND_BACK copy); reaching here means
		// a signal arrived past that guard, and the gate's job is then to refuse it rather than
		// to absorb it.
		if isM0Gate(lc, t) {
			logNoSendBackAtM0(ctx, in, t, "sendBack")
			return walkTaskFailed, false, nil
		}
		*redraft++
		if *redraft >= maxPhaseRedrafts {
			// Exhausted the human-paced redraft budget. Do NOT fail the activity and do NOT
			// re-enter the variance loop — keep awaiting the human, surfacing that
			// redrafting is spent (mirrors sendBackGate's anti-wedge staging).
			workflow.GetLogger(ctx).Warn("task redraft budget exhausted; keep awaiting human decision",
				"activityId", in.ActivityID, "taskId", t.ID)
			state.leaveHumanStage(ctx, activityType, gateOutcomeSentBackExhausted)
			state.enterPhaseGate(ctx, t.ID, *redraft)
			return walkTaskFailed, false, nil
		}
		state.leaveHumanStage(ctx, activityType, gateOutcomeSentBack)
		if err := wf.sendBackRound(ctx, in, t, state, gate, ws, sig.Feedback); err != nil {
			return walkTaskFailed, true, err
		}
		return walkTaskSentBack, true, nil
	case ReviewDecisionUnknown, ReviewWithdraw, ReviewAdvance, ReviewSetCommentStatus:
		// Not a verdict on this gate: the zero-value sentinel, or a decision this rail
		// answers through its own signal. Ignore and keep awaiting.
		return walkTaskFailed, false, nil
	}
	return walkTaskFailed, false, nil
}

// noSendBackAtM0 is the one sentence both M0 refusal sites give, and it is the SERVER's
// copy of the SPA's NO_SDP_SEND_BACK: to change a derived plan you amend the Architecture,
// and re-opening Architecture makes the compute recompute rather than ask for an
// acknowledgement. Stated once so the two sites cannot drift.
const noSendBackAtM0 = "M0 has no send-back: the project plan is DERIVED, so changing it means amending the Architecture (slot 5) — re-opening it recomputes the plan and opens a fresh M0 round"

// logNoSendBackAtM0 records the refusal and says the gate is still waiting, so an operator
// reading the log is not left thinking their decision landed.
func logNoSendBackAtM0(ctx workflow.Context, in deliveryActivityInput, t methodassets.LifecycleTask, kind string) {
	workflow.GetLogger(ctx).Warn("delivery.gate.refusedSendBack",
		"activityId", in.ActivityID, "taskId", t.ID, "decision", kind,
		"reason", noSendBackAtM0, "gate", "still awaiting approve")
}

// sendBackRound records the rejection: the human's verdict with the comments that rode
// with it, the round decided sentBack, and the REJECTED attempt at the review task —
// which is what leaves the phase incomplete and makes the redraft render as App A's "a
// failing review causes the developer to repeat the preceding internal task". The
// feedback is carried WALK-LOCALLY onto the judged task, so its redraft is dispatched
// with the steer that rejected it rather than against the prompt that was just refused.
func (wf *csWorkflows) sendBackRound(
	ctx workflow.Context, in deliveryActivityInput, t methodassets.LifecycleTask,
	state *constructState, gate *gateLedger, ws *walkState, fb *ReviewFeedback,
) error {
	csIn := in.csIn()
	if err := wf.closeGateRound(ctx, csIn, state, gate, &state.walk.headVersion, state.walk.cred,
		projectstate.VerdictSendBack, projectstate.RoundSentBack, fb); err != nil {
		return err
	}
	if err := wf.rejectGateAttempt(ctx, csIn, state, gate, &state.walk.headVersion, state.walk.cred); err != nil {
		return err
	}
	ws.feedback[string(roundJudges(t))] = signalNotes(fb)
	return nil
}

// (applyRoundCommentStatus IS GONE, and its absence is the point — stage 4b1 Task 12, fix round
// 2. The child used to be the WRITER of a comment status, through a signal nothing ever sent.
// The Manager writes it now, synchronously, so it can tell the reviewer that the comment does not
// exist or that the transition is illegal; the mirror signal that follows carries no write at all,
// and what the child does with it is reconsiderAutogate — re-ask whether a gate held on an open
// comment may now pass. Two writers of one transition would have meant one of them erroring:
// applyReviewCommentStatus refuses resolved->resolved as a ContractMisuse.)

// recordGateOverride records an operator override that arrived at a gate. The operator
// steered the ACTIVITY rather than answering the gate, so the override is recorded as the
// operator note the construction rail already records for one (overrideNoteKind) and the
// gate KEEPS AWAITING its decision. Executing the override's own directive — retry, skip,
// takeover — against a task DAG rather than a flat phase list is Task 12's.
func (wf *csWorkflows) recordGateOverride(
	ctx workflow.Context, in deliveryActivityInput, state *constructState,
	t methodassets.LifecycleTask, sig *operatorOverrideSignal,
) {
	if sig == nil {
		return
	}
	kind, ok := overrideNoteKind(sig.Override.Kind)
	if !ok {
		workflow.GetLogger(ctx).Error("an override with no note kind reached a gate; nothing recorded",
			"activityId", in.ActivityID, "taskId", t.ID)
		return
	}
	if err := wf.recordOperatorNote(ctx, in.csIn(), state, &state.walk.headVersion, state.walk.cred,
		kind, t.ID, noteFeedback{text: sig.Override.Notes, comments: sig.Override.Comments}); err != nil {
		workflow.GetLogger(ctx).Error("the override could not be recorded; the gate keeps awaiting",
			"activityId", in.ActivityID, "taskId", t.ID, "err", err.Error())
	}
}

// ---------------------------------------------------------------------------
// THE OBSERVE LADDER (R-L mitigation i)
//
// Today both observe loops are a flat 15s × 240, and each poll costs ≈4 history events
// (timer started, timer fired, activity scheduled, activity completed), so a stuck job
// burns ≈960 events before its 1-hour ceiling. ONE child now holds ELEVEN of those for a
// `requirements` activity. The ladder keeps the ceiling and spends a tenth of the
// history: fast while a job plausibly finishes in a minute, then coarse, because a job
// that has run ten minutes will not finish in the next fifteen seconds.
//
//	4 polls × 15s   =  1 min   (the common case: a job that just finished)
//	9 polls × 60s   =  9 min   (to the 10-minute mark)
//	10 polls × 300s = 50 min   (to the same 1-hour ceiling the flat loop had)
//	-------------------------------------------------------------------
//	23 polls ≈ 92 events, against 240 polls ≈ 960. Same ceiling, one tenth the history.
//
// The CEILING and both escalations are unchanged; only the poll SCHEDULE moves.
// ---------------------------------------------------------------------------

const (
	observeFastInterval = 15 * time.Second
	observeFastPolls    = 4
	observeMedInterval  = 60 * time.Second
	observeMedPolls     = 9
	observeSlowInterval = 300 * time.Second
	observeSlowPolls    = 10
	// maxObserveTotalPolls is the ladder's length — the ONE bound both observe loops use,
	// so there is one ladder and not two that drift.
	maxObserveTotalPolls = observeFastPolls + observeMedPolls + observeSlowPolls // 23
)

// observeInterval is the ladder as a pure function of the poll index, so the schedule
// is one readable table rather than three loops.
func observeInterval(poll int) time.Duration {
	switch {
	case poll < observeFastPolls:
		return observeFastInterval
	case poll < observeFastPolls+observeMedPolls:
		return observeMedInterval
	}
	return observeSlowInterval
}

// ---------------------------------------------------------------------------
// CONTINUE-AS-NEW ON A HISTORY BUDGET (R-L mitigation ii)
// ---------------------------------------------------------------------------

// deliveryActivityHistoryBudget is the child's own ceiling, below the server's. The
// server's suggestion (GetContinueAsNewSuggested) is the primary signal because it
// knows the namespace's real limits; the budget is the floor for a deployment that
// does not set one, and 4000 leaves room for the ~92-event dispatch the ladder produces
// plus a gate, so a continue never lands mid-task.
const deliveryActivityHistoryBudget = 4000

// shouldContinueAsNew is checked ONLY where inflight == 0: no coroutine holds a gate
// or a poll, so walkSnapshot is complete and nothing is abandoned mid-dispatch.
//
// Why the child needs this AT ALL, when neither rail it replaces did: today
// SystemDesignPhaseWorkflow starts a CHILD co-author per kind, so a `requirements`
// activity is FOUR histories (measured: 187 and 311 events per co-author fixture, in
// a test env whose poll returns instantly). The generic child collapses all eight
// tasks and three critique dispatches into ONE execution. Collapsing the histories is
// the point of the wave; letting one of them grow unbounded is not.
// It is a plain function over the SDK's own two accessors, with NO injectable budget: the
// test environment exposes SetCurrentHistoryLength, so the case that drives a continue
// drives the PRODUCTION const rather than a smaller number substituted for it.
func shouldContinueAsNew(ctx workflow.Context) bool {
	info := workflow.GetInfo(ctx)
	return info.GetContinueAsNewSuggested() || info.GetCurrentHistoryLength() > deliveryActivityHistoryBudget
}

// ---------------------------------------------------------------------------
// THE SESSION'S EDGES — seeded from the ledger, opened, finalised, failed
// ---------------------------------------------------------------------------

// bindRowAccessors binds the activity's execution ROW onto the walk's context, so every
// applyRecovering call below reaches the Conflict arm holding the row it is writing. ONE
// row per walk: the child writes exactly one activity's.
func (wf *csWorkflows) bindRowAccessors(ctx workflow.Context, in deliveryActivityInput, state *constructState) workflow.Context {
	return csBindRowAccessor(ctx, in.ActivityID, state)
}

// seedWalkFromLedger rebuilds the walk's position. It PREFERS in.Resume over the ledger
// when the snapshot is present, because the snapshot knows the in-memory feedback,
// produced refs and undelivered signals that the ledger does not record per task; the
// ledger is what a FRESH execution of an activity that already ran must read.
//
// Off the ledger: a task whose LATEST attempt passed is passed, and every task's revision
// is the highest number the attempt and round ledgers hold for it — so the next round id
// continues the ledger's numbering instead of colliding with it.
//
// THE SKIP-IF-COMMITTED GUARD LIVES HERE NOW (stage 4b1 Task 10), and it is the second half
// of the seed rather than a nicety. SystemDesignPhaseWorkflow read the head-state once at
// start and skipped every already-committed step, because a restart that re-spawned the
// mission child over a committed mission is a real 2026-07-16 incident. Retiring that parent
// without moving its guard would re-introduce the incident.
//
// WHAT IT ACTUALLY PROTECTS, re-measured (Task 10 review, F2), because the first draft of this
// paragraph claimed this repo's three design activities have NO execution row and that is
// FALSE: `requirements` holds eight passed attempts, `architecture` two plus a passed round 2,
// `projectDesign` one. Those rows exist because the stage-3 backfill wrote them, so on THIS
// project the two answers agree and the guard is belt-and-braces. It is load-bearing for a
// project whose LEDGER IS EMPTY and whose SLOTS ARE COMMITTED — every project onboarded before
// the execution ledger existed, and any whose backfill has not run — where a ledger-only seed
// marks every task pending and the first thing the pump does is re-draft a mission that was
// committed months ago. A task whose artifact kind is COMMITTED on main is therefore seeded
// passed, whatever the ledger holds.
//
// Pure over values already in workflow history (the start snapshot's recorded read).
func (wf *csWorkflows) seedWalkFromLedger(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle, proj projectstate.Project,
) *walkState {
	if in.Resume != nil {
		resumed := walkStateFrom(lc, in.Resume)
		resumed.rec = wf.Deliveries
		workflow.GetLogger(ctx).Info("delivery.walk.resumed", "activityId", in.ActivityID,
			"tasks", len(in.Resume.ByTask), "pending", len(in.Resume.Pending))
		return resumed
	}
	row := proj.ActivityExecution[string(in.ActivityID)]
	ws := newWalkState(lc)
	ws.rec = wf.Deliveries
	for _, t := range lc.Tasks {
		latest, n := latestTaskOutcome(row, projectstate.MethodTask(t.ID))
		if latest == projectstate.OutcomePassed || committedArtifactOfTask(proj, lc, t) {
			ws.byTask[t.ID] = walkTaskPassed
		}
		if int64(n) > ws.revision[t.ID] {
			ws.revision[t.ID] = int64(n)
		}
	}
	for _, r := range row.Reviews {
		if r.Round > ws.revision[string(r.TaskID)] {
			ws.revision[string(r.TaskID)] = r.Round
		}
	}
	return ws
}

// seedTaskAttempts raises the run's per-task dispatch counter to whatever the ledger already
// holds, off both ledgers. It is seedResumeFromLedger's counter half, kept because the variance
// loop mints an AttemptID per dispatch and a colliding id is silently absorbed as a replay — the
// second dispatch would then vanish into the first. A GATE task is counted off BOTH the attempts
// and the rounds, because one counter mints both and a resume that looked only at the attempts
// could re-open a round the review ledger already holds.
//
// Pure over values already in workflow history (the start snapshot's recorded read).
func seedTaskAttempts(state *constructState, row projectstate.ActivityExecution) {
	for _, a := range row.Attempts {
		seedTaskCount(state, a.Task, a.Attempt)
	}
	for _, r := range row.Reviews {
		seedTaskCount(state, r.TaskID, int(r.Round))
	}
}

// committedArtifactOfTask reports whether the artifact THIS task is about is already
// committed on main — the skip-if-committed guard's one question.
//
// It answers false for every construction task by construction, and that is not an accident
// worth relying on quietly: designSlotOfTask resolves only the seventeen artifact SLOTS, and a
// construction task's output is a commit this store does not hold, so there is no slot whose
// status could say the work is done. Only a design task can be skipped this way, which is
// exactly the guard SystemDesignPhaseWorkflow had.
//
// The M0 review task is deliberately included: its `artifactKind` is sdpReview, so a
// projectDesign activity whose SDP review is already committed does not recompute the plan.
func committedArtifactOfTask(proj projectstate.Project, lc methodassets.Lifecycle, t methodassets.LifecycleTask) bool {
	kind, ok := designSlotOfTask(lc, t)
	if !ok {
		return false
	}
	return slotForKind(proj, kind).Status == projectstate.ReviewCommitted
}

// latestTaskOutcome is the outcome and number of the highest-numbered attempt the ledger
// holds at a task, or (OutcomePending, 0) when it holds none.
func latestTaskOutcome(row projectstate.ActivityExecution, task projectstate.MethodTask) (projectstate.TaskOutcome, int) {
	out, best := projectstate.OutcomePending, 0
	for _, a := range row.Attempts {
		if a.Task != task || a.Attempt < best {
			continue
		}
		out, best = a.Outcome, a.Attempt
	}
	return out, best
}

// openActivityRow births the activity's execution row, pins the lifecycle in force and
// opens the per-activity branch + PR — the same three facts ConstructActivityWorkflow's
// step 0 and step 2a record, reached through the same helpers so the two rails cannot
// disagree about what starting an activity means. The git half is dormant (a no-op) when
// the slice is unwired, and it adds NO second reader of wf.Repo: startedCred and
// openActivityBranchAndPR both route through the one gitEnabled, which is where
// isGitLocalVenue recognition already lives (R6).
func (wf *csWorkflows) openActivityRow(ctx workflow.Context, in deliveryActivityInput, state *constructState) error {
	csIn := in.csIn()
	state.walk.headVersion = wf.readVersion(ctx, in.ProjectID)
	cred, gitOn, err := wf.startedCred(ctx, in.ProjectID)
	if err != nil {
		return err
	}
	state.walk.cred, state.walk.gitOn = cred, gitOn
	switch {
	case state.executionLedger:
		if err := wf.openActivity(ctx, csIn, state, cred, &state.walk.headVersion); err != nil {
			return err
		}
	case gitOn:
		if err := wf.recordActivityStarted(ctx, csIn, cred, &state.walk.headVersion); err != nil {
			return err
		}
	}
	gf, err := wf.openActivityBranchAndPR(ctx, csIn, cred, &state.walk.headVersion)
	if err != nil {
		return err
	}
	state.walk.gf = gf
	return nil
}

// recordTaskAttempt records what a strategy produced as ONE attempt on the append-only
// task ledger.
//
// It is a NO-OP when the strategy produced no attempt id, and that is the honest reading
// rather than a guard: judgedTaskStrategy produces nothing at all (a review's subject is
// its judged task's output), and the attempt that matters for a review task is the GATE
// attempt passRound and sendBackRound write. A strategy that dispatched something and
// wants its attempt on the ledger returns the id it recorded.
func (wf *csWorkflows) recordTaskAttempt(
	ctx workflow.Context, in deliveryActivityInput, t methodassets.LifecycleTask,
	tc taskContext, produced producedSubject, state *constructState,
) error {
	if !state.executionLedger || produced.AttemptID == "" {
		return nil
	}
	kind, ref := attemptEvidence(produced)
	return wf.recordAttempt(ctx, in.csIn(), state, &state.walk.headVersion, state.walk.cred,
		projectstate.TaskAttemptInput{
			AttemptID:    produced.AttemptID,
			TaskID:       projectstate.MethodTask(t.ID),
			Attempt:      int64(tc.Attempt),
			Actor:        projectstate.ActorAgent,
			Outcome:      produced.Outcome,
			EvidenceKind: kind,
			EvidenceRef:  ref,
			// The strategy's own sentence, DURABLY (stage 4b2). It was produced and dropped:
			// sdpComputeStrategy has recorded which planning-assumption families the compute
			// had to default since 4b1, the ledger had nowhere to put it, and the M0 review —
			// a SPEND APPROVAL — showed the founder a cost with no trace of the numbers it
			// was computed on.
			Detail: produced.Detail,
		})
}

// attemptEvidence is what an attempt cites, and the ORDER is the point: the EPISODE first,
// because a construction task's whole output is a dispatch whose tokens the episode ledger
// holds and whose commit this store does not (resolveWorkAttempt cited exactly this on the
// retired rail); then the STAGED model, which is a design or compute task's output; then
// nothing, because a ref nobody can follow is worse than an absent one. No strategy returns
// both today — a design job's episode is captured under the same task but its attempt's
// evidence is the model a reviewer opens — and if one ever does, the episode is the one that
// says what was spent.
func attemptEvidence(produced producedSubject) (projectstate.EvidenceKind, string) {
	switch {
	case produced.EpisodeID != "":
		return projectstate.EvidenceEpisode, produced.EpisodeID
	case produced.StagedRef != "":
		return projectstate.EvidenceGit, produced.StagedRef
	}
	return projectstate.EvidenceNone, ""
}

// finalizeWalk closes a walk whose every task passed: the policy-gated LOCAL merge, then
// the clean-pass tail (the architecture +1 relay, the change-reviewed record, the gated
// merge, and the activity's binary exit). Both are reached through the helpers the
// construction rail already uses, so "the activity finished" means the same thing on
// either rail.
//
// A walk that ran out of ready tasks with some task UNPASSED has not finished, it has
// STOPPED, and saying otherwise would record a completed activity whose work never
// happened. The one way to get there is a send-back on a review task that names no
// judged pair to re-open, so the stuck task is named in the failure.
//
// THIS IS THE LAST GATE'S SITE, and it is identified from the DATA rather than asserted
// (stage 4b1 Task 11). The plan asks for the merge to hang off "the gate task of the LAST
// phase in lc.Phases"; measured across all fourteen lifecycles, that task is the last task in
// declaration order and nothing depends on it — so "every task passed", which is the condition
// this function is entered under, IS "the last phase's gate passed", and there is exactly one
// of them. Pinned by Test_LastPhaseGateIsTheWalksFinalTask, so a platform release that grew a
// task after the final gate fails there instead of merging a branch mid-walk. Writing the
// question out as a second branch inside passRound would have been a THIRD place that has to
// agree about when an activity is over, for no behaviour.
func (wf *csWorkflows) finalizeWalk(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	ws *walkState, state *constructState,
) (err error) {
	for _, t := range lc.Tasks {
		if ws.byTask[t.ID] != walkTaskPassed {
			return wf.failWalk(ctx, in, state, t.ID, temporal.NewNonRetryableApplicationError(
				"the walk has no task left to run and task "+t.ID+" never passed", "WalkStalled", nil))
		}
	}
	csIn := in.csIn()
	// THE MAIN-WRITE LEASE IS TAKEN HERE, AND HERE IS WHY HERE (stage 4b2). Everything
	// above this line writes to the ACTIVITY BRANCH or to the activity's own row, and both
	// are already serialised — the row by its CAS, the branch by being this activity's
	// alone. Everything below writes MAIN: runWalkMerge's merge, finalizeActivity's
	// mergeAndRecord, and commitDesignArtifacts' N slot commits. So the lease brackets
	// exactly the main-writing tail and nothing else, and a walk that never reaches its
	// tail never asks for one.
	//
	// THE RELEASE IS DEFERRED, over every exit including the error paths: a lease leaked by
	// a returning error is a project that stops until the pump's deadline check reaps it.
	// It carries the epoch, so a release cannot free a lease a successor was granted after
	// the pump decided this execution was gone.
	//
	// AND IT REPORTS THE OUTCOME IT ACTUALLY REACHED (fix round 1, F5). It used to report
	// ActivityOutcomeCompleted unconditionally, which was a LIE on two of the three exits —
	// the `exited` return, where the variance loop gave the activity up, and every error
	// return, where the tail BROKE. That lie was the blocker's other half: the pump's finish
	// arm read it, marked the activity finished and deleted the child's future, so the
	// failure that followed a microsecond later had no channel left to arrive on and the run
	// reported a CLEAN, QUIESCENT cascade. The pump no longer trusts a signal over a future
	// it holds (pumpnextactivity.go, applyFinish), and this end of the wire no longer says
	// something it does not know.
	var exited bool
	epoch, leased := wf.requestMainWriteLease(ctx, in, state)
	if leased {
		defer func() { wf.releaseMainWriteLease(ctx, in, epoch, mainWriteTailOutcome(exited, err)) }()
	}
	exited, err = wf.runWalkMerge(ctx, in, lc, ws, state)
	if err != nil {
		return err
	}
	if exited {
		// The merge's own variance loop reached a terminal for the activity and recorded it: no
		// binary exit to write over it, and no design slots to commit off a branch that never
		// landed.
		return nil
	}
	// EARMARK, RECORDED RATHER THAN FIXED (fix round 1, F1's second half). An error from
	// either call below returns straight through walkTasks WITHOUT passing through failWalk,
	// so no terminal FAILURE row is written: the activity is left reading Running (or, if
	// finalizeActivity already recorded the binary exit and only commitDesignArtifacts broke,
	// Completed with uncommitted slots — the state the commitDesignArtifacts header already
	// documents as heal-by-re-open). The pump now STOPS on this error via the child's future,
	// which is the protection that was missing; what is still owed is the durable row, and
	// with it pumpReconcile's pre-ContinueAsNew arm ("terminal failure row, no finish
	// reported") for a child that outlives its pump run. Routing the tail through failWalk
	// would write VarianceExhausted over an already-recorded Completed, so it is a decision
	// about that heal path and not a line to add here in a fix round.
	if err := wf.finalizeActivity(ctx, csIn, &state.walk.gf, &state.walk.headVersion, state, state.walk.gitOn, state.walk.cred,
		reconcileTargetOf(lc)); err != nil {
		return err
	}
	return wf.commitDesignArtifacts(ctx, in, lc, ws, state)
}

// mainWriteTailOutcome is what the merge tail tells the pump it reached. Completed is
// reserved for the ONE exit that earned it — no error and no give-up — and the other two
// report Unknown, which is what walkTasks' own exited arm has always reported.
//
// IT IS NOT WHAT STOPS THE CASCADE, and the distinction is worth keeping straight:
// ActivityOutcome cannot tell a clean give-up from a broken tail (both are Unknown), so the
// pump keys its stop-the-cascade rule on the child's FUTURE, not on this value. This makes
// the message honest and the log readable; applyFinish is what makes it safe.
func mainWriteTailOutcome(exited bool, err error) projectstate.ActivityOutcome {
	if err != nil || exited {
		return projectstate.ActivityOutcomeUnknown
	}
	return projectstate.ActivityOutcomeCompleted
}

// changeActivityMainWriteLease fences the child's lease request. Its DefaultVersion arm is
// the pre-4b2 sequence — no request, no wait, no release — and it exists for ONE reason:
// eight captured deliveryActivity histories replay against this body, every one of them
// reaches its tail, and a new Activity command in a recorded position is a
// non-determinism failure. A new execution records v1 and takes the lease.
const changeActivityMainWriteLease = "delivery-activity-main-write-lease"

// activityLeaseGrantWaitBudget bounds the wait for a grant. It is generous because the
// queue ahead of this child is other children's MERGE TAILS, and a merge tail can hold a
// human approval gate — but it is BOUNDED, because a pump that dies between the request
// and the grant would otherwise wedge this activity forever with nothing to signal it.
const activityLeaseGrantWaitBudget = 2 * time.Hour

// requestMainWriteLease asks the project's ONE pump for admission to write main, waits for
// the grant, and reports the epoch it was granted at.
//
// IT FAILS OPEN, DELIBERATELY, AND THE REASON IS WRITTEN DOWN RATHER THAN ASSUMED. A
// request that cannot be delivered — no pump running (RA NotFound, the normal case for an
// activity an operator started by hand), or any other delivery fault — logs and lets the
// tail proceed UNLEASED. The lease is a second mechanism over row-level serialisation, not
// a replacement for it: applyMutationOnBranchFiles' dedup, version guard and ref-CAS are
// untouched and still serialise per ROW. Failing an activity that did all of its work
// because the admission queue is unreachable would be strictly worse than the state this
// wave started from, which had no lease at all.
func (wf *csWorkflows) requestMainWriteLease(
	ctx workflow.Context, in deliveryActivityInput, state *constructState,
) (int64, bool) {
	if workflow.GetVersion(ctx, changeActivityMainWriteLease, workflow.DefaultVersion, 1) < 1 {
		return 0, false
	}
	logger := workflow.GetLogger(ctx)
	grants := workflow.GetSignalChannel(ctx, signalActivityLeaseGranted)
	b, err := json.Marshal(activityLeaseRequest{ActivityID: in.ActivityID})
	if err != nil {
		logger.Error("delivery.lease.encodeFailed", "activityId", in.ActivityID, "err", err.Error())
		return 0, false
	}
	if derr := wf.Acts.MessageBusDeliverSignal(ctx,
		messagebus.ExecutionID(pumpWorkflowID(in.ProjectID)),
		messagebus.SignalName(signalActivityLeaseRequested),
		messagebus.ExecutionPayload{Bytes: b}); derr != nil {
		logger.Error("delivery.lease.unreachable",
			"activityId", in.ActivityID, "err", derr.Error(),
			"consequence", "the merge tail runs UNLEASED; the per-row CAS and the branch-file version guard still serialise per row")
		return 0, false
	}
	state.stage = StageDispatching
	// THE GRANT CHANNEL IS READ DIRECTLY HERE and is deliberately NOT an arm of
	// routeSignals. The router's addressing unit is a TASK and a lease grant names the
	// ACTIVITY; and this read happens where walkTasks has already reached inflight == 0, so
	// no sibling coroutine exists to race for the message — which is the one defect the
	// router was built to prevent.
	tctx, cancel := workflow.WithCancel(ctx)
	defer cancel()
	timeout := workflow.NewTimer(tctx, activityLeaseGrantWaitBudget)
	for {
		var grant activityLeaseGrant
		timedOut := false
		sel := workflow.NewSelector(ctx)
		sel.AddReceive(grants, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, &grant) })
		sel.AddFuture(timeout, func(workflow.Future) { timedOut = true })
		sel.Select(ctx)
		switch {
		case timedOut:
			logger.Error("delivery.lease.timedOut",
				"activityId", in.ActivityID, "waited", activityLeaseGrantWaitBudget.String(),
				"consequence", "the merge tail runs UNLEASED; the per-row CAS and the branch-file version guard still serialise per row")
			return 0, false
		case grant.Epoch <= pumpLivenessProbeEpoch || grant.ActivityID != in.ActivityID:
			// A LIVENESS PROBE (pumpLivenessProbeEpoch) or a grant addressed to somebody else.
			// Both are inert here BY CONSTRUCTION: the pump bumps LeaseEpoch before it delivers
			// any real grant, so no genuine grant can ever carry the probe's epoch, and the
			// probe's only observable is its own DELIVERY answer at the pump.
			continue
		}
		logger.Info("delivery.lease.granted", "activityId", in.ActivityID, "epoch", grant.Epoch)
		return grant.Epoch, true
	}
}

// releaseMainWriteLease tells the pump this activity's merge tail is over, at whatever
// outcome it reached, so the lease is free. Best-effort by design: the pump's reconcile tick
// is the backstop, and an activity that finished must not fail because the pump it was
// reporting to has closed.
//
// THE OUTCOME IS THE CALLER'S, NOT A CONSTANT (fix round 1, F5). This used to be handed
// ActivityOutcomeCompleted at its one call site whatever had happened, including the give-up
// and error exits, and the pump believed it. It now carries mainWriteTailOutcome's answer,
// and the pump treats a finish as a lease release rather than as proof the activity is over
// whenever it still holds the child's future.
func (wf *csWorkflows) releaseMainWriteLease(
	ctx workflow.Context, in deliveryActivityInput, epoch int64, outcome projectstate.ActivityOutcome,
) {
	wf.signalActivityFinished(ctx, in, outcome, epoch)
}

// signalActivityFinished reports an activity's RECORDED terminal to the pump.
//
// IT IS NOT CALLED BY failWalk, and that is not an oversight: an activity that GIVES UP
// cleanly records a failure row and returns nil so the cascade continues past it (stage
// 4b1's deliberate decision), while failWalk returns the cause and FAILS the execution.
// The absence of this signal is therefore the only thing that distinguishes the two in
// head-state, and pumpReconcile is what reads that distinction.
func (wf *csWorkflows) signalActivityFinished(
	ctx workflow.Context, in deliveryActivityInput, outcome projectstate.ActivityOutcome, epoch int64,
) {
	if workflow.GetVersion(ctx, changeActivityMainWriteLease, workflow.DefaultVersion, 1) < 1 {
		return
	}
	logger := workflow.GetLogger(ctx)
	b, err := json.Marshal(activityFinishedSignal{ActivityID: in.ActivityID, Outcome: outcome})
	if err != nil {
		logger.Error("delivery.finish.encodeFailed", "activityId", in.ActivityID, "err", err.Error())
		return
	}
	if derr := wf.Acts.MessageBusDeliverSignal(ctx,
		messagebus.ExecutionID(pumpWorkflowID(in.ProjectID)),
		messagebus.SignalName(signalActivityFinished),
		messagebus.ExecutionPayload{Bytes: b}); derr != nil && !isSignalTargetNotFound(derr) {
		logger.Error("delivery.finish.undelivered",
			"activityId", in.ActivityID, "epoch", epoch, "err", derr.Error(),
			"consequence", "the pump learns this terminal from its 30s reconcile instead of at once")
	}
}

// commitDesignArtifacts lands every design slot this walk produced on MAIN, and seals Phase 1
// when that was the last required kind. It is a no-op for a lifecycle that produces no slot
// model, which is every construction lifecycle.
//
// WHY IT RUNS AFTER BOTH MERGES, and not per gate. DesignSessionCommitArtifactWithProvenance
// commits on main and takes no branch, so the model has to BE on main first — and the model is
// staged on the activity branch. The retired session merged its branch and then committed, in
// that order, for exactly this reason. The child's merges are both inside finalizeWalk
// (runWalkMerge for the rail-dormant profile, finalizeActivity's mergeAndRecord for the PR
// rail), so the earliest honest commit point is after them. A per-gate commit is not available
// at all: one activity holds FOUR kinds and one branch, so committing mission when its gate
// passes would mean four merges of one branch.
//
// THE COST: the activity's binary exit is recorded by finalizeActivity BEFORE these commits, so
// a commit that fails here leaves an activity reading Completed with its slots still
// AwaitingReview. THAT NOW HEALS (stage 4b1 Task 12, fix round 1, controller carry 4): the
// operator re-opens the activity (a requeue note clears its terminal), the pump re-selects it,
// and the re-run commits what is still uncommitted — see the no-work arm below, which had to
// LEARN to do that. It is still not automatic: nothing re-opens an activity on its own.
func (wf *csWorkflows) commitDesignArtifacts(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	ws *walkState, state *constructState,
) error {
	kinds := designSlotsOfLifecycle(lc)
	if len(kinds) == 0 {
		return nil
	}
	// A WALK THAT RAN NO TASK STILL HAS ONE JOB, and this arm used to skip it (Task 12, fix
	// round 1). Its old reasoning — "every task was seeded passed off an already-committed slot,
	// so re-committing would be a no-op write per kind on a project that is already correct" —
	// is true only while the PREMISE holds, and the post-exit commit window is exactly the state
	// where it does not: a walk whose tasks all passed and whose slot commit FAILED leaves an
	// activity Completed with an uncommitted slot. Skipping there made the re-run a no-op and the
	// broken state permanent — the one repair path an operator has (a re-open) could not repair
	// it. So the arm now ASKS, once, which of this lifecycle's slots are still uncommitted, and
	// commits those. All committed ⇒ the old skip, unchanged.
	if !walkRanAnyTask(ws) {
		pending, err := wf.uncommittedSlotsOf(ctx, in, kinds)
		switch {
		case err != nil:
			// The repair pass could not read the project. Skipping is the old behaviour and the
			// safe one (a re-open can try again); it is logged at Error because a design activity
			// that cannot read its own project state is not a normal exit.
			workflow.GetLogger(ctx).Error("delivery.design.commitRepairSkipped",
				"activityId", in.ActivityID, "err", err.Error(),
				"consequence", "a slot left AwaitingReview by a failed commit stays that way until the activity is re-opened again")
			return nil
		case len(pending) == 0:
			workflow.GetLogger(ctx).Info("delivery.design.nothingToCommit",
				"activityId", in.ActivityID, "reason", "every slot this activity produces is already committed")
			return nil
		}
		workflow.GetLogger(ctx).Info("delivery.design.commitRepair",
			"activityId", in.ActivityID, "kinds", len(pending),
			"reason", "the walk ran no task and these slots are still uncommitted — the post-exit commit window")
		kinds = pending
	}
	for _, kind := range kinds {
		v, err := wf.applyRecovering(ctx, in.ProjectID, state.walk.headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
			return wf.Acts.DesignSessionCommitArtifactWithProvenance(ctx, projectstate.ProjectID(in.ProjectID), expected,
				kind, gateActorOperator, designDraftedBy)
		})
		if err != nil {
			return err
		}
		state.walk.headVersion = v
	}
	return wf.sealSystemDesign(ctx, in, state)
}

// uncommittedSlotsOf answers which of kinds are NOT committed on main right now, off a FRESH
// read. It is asked only on the no-work path, where the alternative was to guess — the start
// snapshot is from before the walk, and on a re-opened activity the whole question is what
// changed since.
func (wf *csWorkflows) uncommittedSlotsOf(
	ctx workflow.Context, in deliveryActivityInput, kinds []projectstate.ArtifactKind,
) ([]projectstate.ArtifactKind, error) {
	proj, err := wf.readProject(ctx, in.ProjectID)
	if err != nil {
		return nil, err
	}
	var pending []projectstate.ArtifactKind
	for _, kind := range kinds {
		if slotForKind(proj, kind).Status != projectstate.ReviewCommitted {
			pending = append(pending, kind)
		}
	}
	return pending, nil
}

// designDraftedBy is the draftedBy provenance on a slot the generic child commits. It names
// the RAIL rather than an agent charter, exactly as railDraftedBy did on the retired session:
// the drafting identity that matters to a reader is "an agentic design job on the delivery
// child", and the agent's own charter is on the attempt.
const designDraftedBy = "delivery:agentic-design"

// designSlotsOfLifecycle is every artifact SLOT this lifecycle's tasks produce, in declaration
// order and without duplicates — so `requirements` answers mission, glossary, volatilities,
// coreUseCases and `architecture` answers system. A DISPATCH task's kind only: a review task
// resolves to the kind it JUDGES, which is already in the list, and the M0 compute row's eight
// slots are committed by completeProjectDesign at its gate rather than here.
func designSlotsOfLifecycle(lc methodassets.Lifecycle) []projectstate.ArtifactKind {
	var out []projectstate.ArtifactKind
	seen := map[projectstate.ArtifactKind]bool{}
	for _, t := range lc.Tasks {
		if t.Kind != methodassets.LifecycleTaskDispatch {
			continue
		}
		kind, ok := designSlotOfTask(lc, t)
		if !ok || seen[kind] {
			continue
		}
		seen[kind] = true
		out = append(out, kind)
	}
	return out
}

// walkRanAnyTask reports whether any task of this walk actually PRODUCED something in this
// execution — that is, whether any strategy recorded an attempt.
//
// The attempt id is the right signal and the staged ref is not: a construction task's output
// is a commit the agent pushed and it stages no model at all, so a staged-ref test would
// answer "nothing ran" for every construction walk and skip the local merge that lands its
// branch. An attempt id is what every producing strategy returns and what only a judged review
// (whose subject is another task's output) leaves empty.
//
// A walk with no attempts at all is one whose every task was seeded passed by the
// skip-if-committed guard: it opened no round, ran no job and advanced no branch.
func walkRanAnyTask(ws *walkState) bool {
	for _, p := range ws.produced {
		if p.AttemptID != "" {
			return true
		}
	}
	return false
}

// sealSystemDesign is the PHASE-1 SEAL, and it is the one piece of SystemDesignPhaseWorkflow
// that had to MOVE rather than simply go away.
//
// THE MAPPING, exactly, because the plan requires it stated and it is what makes the deletion
// safe: projectstate.Phase1RequiredKinds() is FIVE kinds — mission, glossary, volatilities,
// coreUseCases, system. The `requirements` lifecycle is FOUR phases (mission, glossary,
// volatilities, coreUseCases), each one draft task plus its review, and its four dispatch
// tasks name the first four kinds. The `architecture` lifecycle is ONE phase whose one
// dispatch task names the fifth. Four-then-one, ten tasks, five kinds, NO RESIDUE — no sixth
// kind, and no kind produced by neither activity. The parent's fixed
// `mission → glossary → volatilities → coreUseCases → system → SEAL` sequence is therefore
// the pump's eligibility over slot 10's `requirements → architecture` edge plus each child's
// own dependsOn walk, and this function is its final step.
//
// It is asked after EVERY design commit rather than of one nominated activity, because the
// condition is a property of the SLOTS and not of a lifecycle: `runPhaseAdvance` asked the
// same question ("is every required kind committed"), it is idempotent, and it costs one read
// on a path that has just written up to four times. A walk that commits the fifth kind seals;
// one that commits the second does not.
func (wf *csWorkflows) sealSystemDesign(ctx workflow.Context, in deliveryActivityInput, state *constructState) error {
	proj, err := wf.readProject(ctx, in.ProjectID)
	if err != nil {
		if isReadNotFound(err) {
			return nil
		}
		return err
	}
	if proj.Phase != projectstate.PhaseSystemDesign {
		return nil
	}
	for _, kind := range projectstate.Phase1RequiredKinds() {
		if slotForKind(proj, kind).Status != projectstate.ReviewCommitted {
			return nil
		}
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, state.walk.headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ProjectStateAdvancePhase(ctx, projectstate.ProjectID(in.ProjectID), expected)
	})
	if err != nil {
		return err
	}
	state.walk.headVersion = v
	workflow.GetLogger(ctx).Info("delivery.systemDesign.sealed",
		"activityId", in.ActivityID, "requiredKinds", len(projectstate.Phase1RequiredKinds()))
	return nil
}

// mergeGateTaskID is the inbox key the LOCAL merge hold waits on. It is mergeGateKey used as a
// task id, because the router's addressing unit is a task and the merge is a gate with no
// lifecycle task of its own. It is seeded walkTaskPending in newWalkState so an approve that
// arrives before the hold opens is BUFFERED rather than read as too-late.
const mergeGateTaskID = mergeGateKey

// runWalkMerge is the policy-gated LOCAL merge, for the rail-dormant git profile where
// nothing else lands activity/<id> on main. It consults the SAME engine call the gates use
// (vibes → auto-merge, checkpoints/full → hold, the risk floor → always hold) and
// dispatches the merge through runMergePipeline, the construction rail's own job.
//
// TWO DELIBERATE DIFFERENCES from runLocalMergeStep, which it deliberately does not call:
//
//   - The hold reads the merge gate's INBOX, not workflow.GetSignalChannel. A fifth shared
//     channel read from the walk would re-open exactly the hole the router closes.
//   - the VARIANCE LOOP is the SAME one a dispatch gets (fix round 1), and the reason it is
//     not runLocalMergeStep's is that the retired retry re-walked the whole phase list from
//     index 0 with completedPhases skipping what had passed. Here a merge retry re-runs the
//     MERGE and nothing else, which is what that skip-guard was for.
//
// It returns exited=true when the merge's variance reached a terminal for the ACTIVITY and
// recorded it, so finalizeWalk stops without writing a binary exit over it.
func (wf *csWorkflows) runWalkMerge(
	ctx workflow.Context, in deliveryActivityInput, lc methodassets.Lifecycle,
	ws *walkState, state *constructState,
) (bool, error) {
	if !state.walk.gitOn || wf.RailEnabled(in.ProjectID) || state.mergeCompleted {
		return false, nil
	}
	// A LIFECYCLE THAT DISPATCHES NOTHING HAS NOTHING TO MERGE (stage 4b1 Task 9). The
	// local merge lands the activity's branch on main; `projectDesign` is ONE computed
	// review task, so it opens no branch and pushes no commit, and running the merge job
	// for it would dispatch a pipeline against a branch that does not exist — the
	// activity's terminal would then hang on a merge nobody could satisfy. Read off the
	// DATA (does any task dispatch?) rather than off an activity type, so the walker's rule
	// holds here too.
	if !lifecycleDispatchesWork(lc) {
		workflow.GetLogger(ctx).Info("delivery.merge.skipped",
			"activityId", in.ActivityID, "reason", "the lifecycle dispatches no work, so there is no branch to merge")
		return false, nil
	}
	// AND A WALK THAT DISPATCHED NOTHING HAS NOTHING TO MERGE EITHER (stage 4b1 Task 10). The
	// guard above asks the LIFECYCLE; this one asks this RUN. The skip-if-committed seed marks
	// every task of an already-committed design activity passed, so the walk reaches its
	// terminal having opened no round, run no job and — decisively — pushed no commit to
	// activity/<id>. Dispatching the merge job then merges a branch that does not exist, whose
	// failure fails a walk that did nothing wrong. Read off ws.produced, which is the run's own
	// record of what it staged, rather than off a type.
	if !walkRanAnyTask(ws) {
		workflow.GetLogger(ctx).Info("delivery.merge.skipped",
			"activityId", in.ActivityID, "reason", "this run staged nothing, so no branch was advanced to merge")
		return false, nil
	}
	csIn := in.csIn()
	// THE MERGE GATE'S INBOX IS OPENED ONCE, HERE, and retired once, because TWO things read it
	// now: the policy hold below and the variance loop's escalation. holdForMergeApproval opened
	// its own until fix round 1, and a second openInbox would have replaced the channel — losing
	// whatever the router had already buffered for a gate that was seeded walkTaskPending
	// precisely so an early approve is kept.
	inbox := ws.openInbox(ctx, mergeGateTaskID)
	defer func() {
		// RETIRE the merge gate, not just its inbox (Task 8 review finding 5): leaving it pending
		// keeps the "too early, buffer it" arm live, so every later merge-gate signal queues in
		// ws.pending for a hold that will never re-open — and rides every continue-as-new.
		ws.byTask[mergeGateTaskID] = walkTaskPassed
		ws.closeInbox(workflow.GetLogger(ctx), mergeGateTaskID)
	}()
	set, err := wf.proposeReviewSet(csIn, methodassets.LifecyclePhase{ID: projectstate.MethodPhaseConstruction.String()},
		state.walk.policy, state)
	if err != nil {
		// A refusal reads as "no hold", matching the gate's conservative arm: the merge is
		// what the vibes profile does unattended today, and an engine defect must not strand
		// the branch. Logged either way.
		workflow.GetLogger(ctx).Error("review engine refused to decide the merge gate; the merge proceeds unheld",
			"activityId", in.ActivityID, "err", err.Error())
	} else if set.RequiresHuman != nil && *set.RequiresHuman {
		wf.holdForMergeApproval(ctx, in, ws, state, inbox)
	}
	return wf.mergeUnderSupervision(ctx, in, ws, state, inbox)
}

// mergeUnderSupervision runs the merge job under the SAME variance loop a dispatch gets: the
// intervention Engine decides, a retry re-runs the merge and nothing else, and an escalation
// waits on the merge gate's own inbox.
//
// The merge is addressed as the task `merge` (mergeGateTaskID) throughout, which is what the
// router already does with it — it is a gate with no lifecycle task, and giving it a task id is
// what lets an operator steer it at all.
func (wf *csWorkflows) mergeUnderSupervision(
	ctx workflow.Context, in deliveryActivityInput, ws *walkState,
	state *constructState, inbox workflow.ReceiveChannel,
) (bool, error) {
	tc := taskContext{
		In: in, State: state, Inbox: inbox,
		Drain: func() { ws.drainPending(mergeGateTaskID) },
		// A SYNTHETIC lifecycle task, and the synthesis is honest rather than convenient: the
		// merge has no node in lifecycles.json (App A's twelve tasks stop at Code Review; landing
		// the reviewed branch is this platform's own automation), and every reader below wants
		// exactly its id.
		Task: methodassets.LifecycleTask{ID: mergeGateTaskID},
	}
	var deferred []routedSignal
	for attempt := 0; ; attempt++ {
		if attempt >= maxVarianceAttempts {
			subject, err := wf.constructionGaveUp(ctx, tc, deferred,
				projectstate.ActivityOutcomeUnknown, projectstate.VarianceExhausted,
				"the local merge exceeded max attempts")
			_ = subject
			return true, err
		}
		state.attempt = attempt + 1
		state.stage = StagePipelineRunning
		obs, merr := wf.runMergePipeline(ctx, in.csIn(), state)
		if merr != nil {
			wf.reofferDeferred(ctx, tc, deferred)
			return false, merr
		}
		if obs.Phase == PipelineSucceeded {
			wf.reofferDeferred(ctx, tc, deferred)
			state.mergeCompleted = true
			return false, nil
		}
		verdict, held, verr := wf.runTaskVariance(ctx, tc, obs, attempt, deferred)
		deferred = held
		switch {
		case verr != nil:
			wf.reofferDeferred(ctx, tc, deferred)
			return false, verr
		case verdict == varianceRedispatch:
			continue
		default:
			_, err := wf.constructionExited(ctx, tc, deferred, "", obs, verdict)
			return true, err
		}
	}
}

// lifecycleDispatchesWork reports whether any task of this lifecycle DISPATCHES — that is,
// whether anything in it produces a commit. Every lifecycle in method-assets v0.9.0 does
// except `projectDesign`, whose one task is a server-side computation.
func lifecycleDispatchesWork(lc methodassets.Lifecycle) bool {
	for _, t := range lc.Tasks {
		if t.Kind == methodassets.LifecycleTaskDispatch {
			return true
		}
	}
	return false
}

// holdForMergeApproval suspends until an operator approves the merge. A non-approve
// decision has no redraft meaning for a merge (there is no draft to send back), so it is
// logged and the hold keeps awaiting — the operator steers the activity itself with an
// override, exactly as the retired rail's merge gate behaves.
// It TAKES the inbox (fix round 1) rather than opening one: the variance loop's escalation reads
// the same gate, and two openInbox calls would replace the channel and drop whatever the router
// had buffered for a gate that newWalkState seeds walkTaskPending precisely so an early approve
// survives. Its retirement moved to runWalkMerge's defer for the same reason.
func (wf *csWorkflows) holdForMergeApproval(
	ctx workflow.Context, in deliveryActivityInput, ws *walkState,
	state *constructState, inbox workflow.ReceiveChannel,
) {
	state.redraftExhausted = false
	state.enterHumanStage(ctx, StageAwaitingApproval, mergeGateTaskID, 0)
	for {
		ws.drainPending(mergeGateTaskID)
		var msg routedSignal
		inbox.Receive(ctx, &msg)
		// Nil-checked for the same reason awaitTaskDecision is (review finding 4): a payload
		// that did not survive the snapshot's JSON round-trip would panic the workflow task
		// into infinite retry, with the merge hold looking simply unanswered.
		if msg.Kind == routedKindDecision && msg.Decision != nil && msg.Decision.Decision == ReviewApprove {
			state.leaveHumanStage(ctx, in.Activity.activityTypeName(), gateOutcomeApproved)
			return
		}
		workflow.GetLogger(ctx).Info("merge gate: ignoring a non-approve message; awaiting Approve",
			"activityId", in.ActivityID, "kind", msg.Kind)
	}
}

// failWalk records the activity's terminal FAILURE — so the activity is no longer stuck
// Running — and returns the error that caused it, unchanged, because the caller (and the
// pump reading the row) must see the real cause and not a summary of it.
func (wf *csWorkflows) failWalk(
	ctx workflow.Context, in deliveryActivityInput, state *constructState, taskID string, cause error,
) error {
	detail := "task " + taskID + " failed: " + cause.Error()
	if err := wf.recordExecutionOutcome(ctx, in.csIn(), state, &state.walk.headVersion, state.walk.cred,
		projectstate.ActivityOutcomeUnknown, projectstate.VarianceExhausted, detail); err != nil {
		workflow.GetLogger(ctx).Error("the walk's failure could not be recorded",
			"activityId", in.ActivityID, "taskId", taskID, "err", err.Error())
	}
	state.stage = StageExited
	workflow.GetLogger(ctx).Error("delivery.walk.failed", "activityId", in.ActivityID, "taskId", taskID)
	return cause
}

// ===========================================================================
// THE CONSTRUCTION SPINE'S SURVIVING WORKFLOW HALF — moved here from
// constructactivity.go when stage 4b1 Task 13 deleted ConstructActivityWorkflow.
//
// The file went ENTIRELY, because a handwritten file with context-taking funcs and
// no registered entry func is what TestFileLayout calls file-not-allowed. What moved
// is everything the generic child already calls: the execution-ledger block (open /
// attempt / round / verdict / outcome), the pipeline submit-observe-episode family,
// the operator-note family, the git-forward edges and the two budgets. What did NOT
// move is the flat phase walk the child replaced — runAttempt, walkPhases,
// runPhaseGate, runPipeline, sendBackGate, completePhase, receivePhaseDecision,
// openGateRound, gateWithoutHuman, awaitPhaseDecision and the variance loop
// (handleVariance / executeOverride / awaitOverrideBounded / failVarianceExhausted),
// which Task 12's runTaskVariance re-expressed per TASK — plus runLocalMergeStep,
// which runWalkMerge superseded (R8-10) and whose runMergePipeline half survives.
// ===========================================================================
// submitPipeline composes the contract PipelineSpec (default toolchain / single build step
// / workspaceRef / dispatch inputs) from the Manager's neutral pipelineSpec and calls the
// GENERATED submit invoker, mapping the opaque handle back to the neutral pipelineHandle.
func (wf *csWorkflows) submitPipeline(ctx workflow.Context, spec pipelineSpec) (pipelineHandle, error) {
	// gh-mode venue switch (B5): retarget the dispatch to the project's OWN repo +
	// aiarch-construct.yml when the per-project Repo resolves; a zero target leaves the
	// central-repo fallback (resolveTarget) intact for unresolvable projects.
	target, workflowFile, terr := wf.constructRepoTarget(spec.ProjectID)
	if terr != nil {
		return pipelineHandle{}, terr
	}
	handle, err := wf.Acts.PipelineSubmitAgenticJob(ctx, agenticjob.PipelineSpec{
		ProjectID:  agenticjob.ProjectID(spec.ProjectID),
		ActivityID: agenticjob.ConstructionActivityID(spec.ActivityID),
		Steps: []agenticjob.PipelineStep{{
			Name:      "build",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		WorkspaceRef:   agenticjob.ArtifactRef(spec.RepoURL + "@" + spec.Ref),
		DispatchInputs: dispatchInputsFor(spec),
		TargetRepo:     target,
		WorkflowFile:   workflowFile,
	})
	if err != nil {
		return pipelineHandle{}, err
	}
	return pipelineHandle{Name: agenticjob.PipelineHandleString(handle)}, nil
}

// observePipeline calls the GENERATED observe invoker and maps the contract observation
// back to the Manager-neutral csPipelineObservation.
func (wf *csWorkflows) observePipeline(ctx workflow.Context, handle pipelineHandle) (csPipelineObservation, error) {
	obs, err := wf.Acts.PipelineObserveAgenticJob(ctx, agenticjob.ParsePipelineHandle(handle.Name))
	if err != nil {
		return csPipelineObservation{}, err
	}
	return csPipelineObservation{
		Phase:      managerPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
		RunURL:     obs.RunURL,
		Episode:    obs.Episode,
	}, nil
}

// ---------------------------------------------------------------------------
// Episode capture (SP1 capture-seam, Task 7)
// ---------------------------------------------------------------------------
//
// EVERY terminal observation of an AGENTIC dispatch this Manager takes becomes
// EXACTLY ONE EpisodeRecord in the episode ledger — either the mined summary or an
// explicit GAP record. A missing record is never silently missing; the ledger is the
// only place the platform can later answer "what did this activity actually cost".
//
// THREE disciplines hold here, and each has a reason:
//
//   - BUSINESS FIRST, EPISODE SECOND. The append is the LAST thing a terminal poll
//     does: the in-loop business handling (the CI-rollup mirror, the phase stamp) has
//     already run, and the append's own failure is swallowed. A ledger line claiming
//     something happened when it did not is worse than a missing line.
//   - THE APPEND NEVER FAILS THE BUSINESS FLOW. appendEpisodeActivityOptions gives it
//     its own retry envelope, wholly independent of the business retries; when it is
//     still failing at the end of that envelope the error is LOGGED and dropped.
//     Construction must not fail because a bookkeeping write did.
//   - VENUE. The GitHub-Actions arm mines no episode in v1, so a nil summary there is
//     EXPECTED, not a gap — writing a gap record per GH run would fill the ledger with
//     noise that means nothing. The only venue signal a workflow can see is the run URL
//     (see csPipelineObservation.RunURL), so that is what gates it.
//
// DETERMINISM: the append is a plain ExecuteActivity — a NEW command in an EXISTING
// workflow body. In-flight executions must be DRAINED before deploying (the standing
// convention; no GetVersion guard is carried — contrast pumpnextactivity.go:44, which
// documents the pure-addition case that needs none).

// awaitLateEpisode gives a CANCELLED run's episode summary a bounded chance to arrive
// (see maxLateEpisodePolls). It returns the observation to RECORD: the later one that
// carried a summary when it arrived, else the caller's original — which becomes a gap.
// The business phase is taken from the ORIGINAL observation either way; this only ever
// upgrades the episode payload.
func (wf *csWorkflows) awaitLateEpisode(ctx workflow.Context, handle pipelineHandle, obs csPipelineObservation) csPipelineObservation {
	if obs.Phase != PipelineCancelled || obs.Episode != nil {
		return obs
	}
	for range maxLateEpisodePolls {
		if err := workflow.Sleep(ctx, lateEpisodePollInterval); err != nil {
			return obs
		}
		next, err := wf.observePipeline(ctx, handle)
		if err != nil {
			return obs
		}
		if next.Episode != nil {
			obs.Episode = next.Episode
			return obs
		}
	}
	return obs
}

// captureEpisode appends the ONE ledger record this terminal observation owes.
// agentic=false marks a dispatch that spawns no agent at all (the local merge job): such
// a run has no episode to lose, so a nil summary is recorded as NOTHING rather than as a
// gap. A REMOTE-venue run is skipped entirely for the same "nothing to lose" reason.
//
// task/attempt are the Figure A-1 attribution key the caller already has in hand — the
// task this dispatch's episode is burned on, and how many times that task has been
// dispatched for this activity (see episodeRecordFor / constructState.nextTaskAttempt,
// Task 10).
func (wf *csWorkflows) captureEpisode(ctx workflow.Context, in constructActivityInput, handle pipelineHandle, obs csPipelineObservation, agentic bool, task projectstate.MethodTask, attempt int) {
	if episodeVenueIsRemote(obs.RunURL) {
		return
	}
	if obs.Episode == nil && !agentic {
		return
	}
	// The cancel-race grace is worth waiting for ONLY on a dispatch that could have mined
	// an episode at all: a non-agentic job has nothing in flight to wait for.
	if agentic {
		obs = wf.awaitLateEpisode(ctx, handle, obs)
	}
	rec := episodeRecordFor(ctx, obs, csEpisodeIDSeed(handle, in), string(in.ActivityID), task, attempt)
	if err := wf.Acts.EpisodesAppendEpisode(ctx, episode.ProjectID(in.ProjectID), rec); err != nil {
		// Swallowed BY DESIGN — see the "never fails the business flow" discipline above.
		workflow.GetLogger(ctx).Error("episode append failed after its full retry envelope; this episode is NOT in the ledger",
			"activityId", string(in.ActivityID), "episodeId", rec.EpisodeID, "error", err.Error())
	}
}

// episodeRecordFor composes the ledger record for ONE terminal observation. With a
// summary it copies every mined field VERBATIM and stamps only what the Manager alone
// knows (Kind/TargetRef/Lineage); with no summary it composes an explicit GAP record so
// the loss is visible. Pure apart from workflow.GetInfo/Now, both replay-deterministic.
//
// TargetRef carries the ATTEMPT key (projectstate.AttemptID: "<activityId>:<task>:<n>"),
// NOT the bare activity id (Task 10). Episode CAPTURE itself stays deferred (SP1), but
// the KEY cannot wait: the (task, attempt) pair that joins this episode to the Figure A-1
// unit that burned it exists only HERE, at write time — supplied by the caller from the
// lifecycle phase and redraft/attempt count it already has in hand — and cannot be
// reconstructed once it is gone. Lineage.ActivityID is left as the bare Method activity
// id (a separate concern: joining the episode to the project network), so only TargetRef
// changes shape.
func episodeRecordFor(ctx workflow.Context, obs csPipelineObservation, idSeed, activityID string, task projectstate.MethodTask, attempt int) episode.EpisodeRecord {
	exec := workflow.GetInfo(ctx).WorkflowExecution
	lineage := &episode.EpisodeLineage{
		WorkflowID: exec.ID,
		RunID:      exec.RunID,
		// The METHOD activity this episode was burned on (there is no way to read the
		// Temporal activity id from inside a workflow, and the Method id is the one that
		// makes the lineage joinable to the project network).
		ActivityID: &activityID,
	}
	targetRef := projectstate.AttemptID(activityID, task, attempt)
	// Construction dispatches are always EpisodeKindConstruction: the phase profile
	// (requirements/detailed_design/test_plan/construction/integration) draws no
	// review-vs-rework distinction, so there is nothing here to map onto the other kinds.
	const kind = episode.EpisodeKindConstruction
	if obs.Episode == nil {
		return episodeGapRecord(kind, targetRef, lineage, "gap-"+episodeIDSafe(idSeed),
			episodeGapReason(episodeMissingSummaryReason, obs.Diagnostic), workflow.Now(ctx))
	}
	return episodeRecordFromSummary(*obs.Episode, kind, targetRef, lineage, obs.Diagnostic)
}

// openActivityBranchAndPR runs the dispatch-time half of the lifecycle: mint the
// credential, OpenBranch + OpenPullRequest on the rail, then RecordActivityBranchOpened
// (the PR-tolerant fused upsert — births the row with branch+PR and CICheck=Pending).
// It returns the populated gitForward and advances *headVersion. A nil/dormant slice
// returns a disabled gitForward and touches nothing.
func (wf *csWorkflows) openActivityBranchAndPR(
	ctx workflow.Context,
	in constructActivityInput,
	preMintedCred railCredEnvelope,
	headVersion *projectstate.Version,
) (gitForward, error) {
	repoRef, ok := wf.gitEnabled(in.ProjectID)
	if !ok {
		return gitForward{enabled: false}, nil
	}

	gf := gitForward{
		enabled:  true,
		repoRef:  repoRef,
		branch:   activityBranchName(in.ActivityID),
		crLabel:  in.Activity.CRLabel,
		isRevert: in.Activity.IsRevert,
	}

	// REUSE the credential minted ONCE at the top of the spine for the started
	// record (Task 3) — one mint per activity git lifecycle, threaded into every
	// rail + record verb. (Empty when no started cred was minted, which only happens
	// if the slice is dormant — and then gitEnabled is false above and we never get
	// here.)
	gf.cred = preMintedCred

	// Rail: cut the per-activity branch (GENERATED invoker). The openPR pair is the
	// API-heaviest moment of the whole lifecycle and the one the secondary rate limit
	// actually hit in the field, so both calls ride the bounded workflow-side retry.
	var br sourcecontrol.BranchRef
	if err := wf.railWithAuthRetry(ctx, func() error {
		b, e := wf.Acts.RailOpenBranch(ctx, repoRef, sourcecontrol.BranchName(gf.branch), gf.cred.toRail())
		br = b
		return e
	}); err != nil {
		return gitForward{}, err
	}
	gf.branchRef = sourcecontrol.BranchRefString(br)

	// Rail: open the PR (base = main; cr-NN label rides in Hints) (GENERATED invoker).
	var pr sourcecontrol.PullRequestRef
	if err := wf.railWithAuthRetry(ctx, func() error {
		p, e := wf.Acts.RailOpenPullRequest(ctx, repoRef, sourcecontrol.PullRequestSpec{
			Head:  sourcecontrol.BranchName(gf.branch),
			Base:  sourcecontrol.BranchName(mainBranch),
			Title: prTitle(in.ActivityID),
			Body:  prBody(in.Activity),
			Hints: crLabelHints(gf.crLabel),
		}, gf.cred.toRail())
		pr = p
		return e
	}); err != nil {
		return gitForward{}, err
	}
	gf.prRef = sourcecontrol.PullRequestRefString(pr)

	// Mirror: birth the per-activity git head-state row (PR-tolerant fused upsert).
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityBranchOpened(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID),
			gf.branch, gf.branchRef, gf.prRef, gf.crLabel, gf.isRevert, gf.cred.toProjectState())
	})
	if err != nil {
		return gitForward{}, err
	}
	*headVersion = v
	return gf, nil
}

// observeCIAndRecord reads the PR's CI rollup once and mirrors it onto the head-state
// (the poll-loop verb — D-PA-GIT §5). Called between the spine's durable waits while
// the pipeline runs. Returns the observed reflection so the caller can feed it into the
// variance machinery. A dormant slice is a no-op returning Pending.
func (wf *csWorkflows) observeCIAndRecord(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
) (csPullRequestStatusView, error) {
	if !gf.enabled {
		return csPullRequestStatusView{CheckRollup: projectstate.CICheckPending}, nil
	}

	st, err := wf.readPRStatus(ctx, gf)
	if err != nil {
		return csPullRequestStatusView{}, err
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityCIObserved(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID),
			st.CheckRollup, gf.cred.toProjectState())
	})
	if err != nil {
		return csPullRequestStatusView{}, err
	}
	*headVersion = v
	return st, nil
}

// readPRStatus is the ONE read of the PR's rollup + mergeability, shared by the CI
// observer and the approve-time merge guard so the two cannot disagree about what the
// rail said. Credential-fresh and 403-bounded like every other rail verb.
func (wf *csWorkflows) readPRStatus(ctx workflow.Context, gf *gitForward) (csPullRequestStatusView, error) {
	cred, err := wf.liveCred(ctx, gf)
	if err != nil {
		return csPullRequestStatusView{}, err
	}
	var st csPullRequestStatusView
	if rerr := wf.railWithAuthRetry(ctx, func() error {
		prStatus, e := wf.Acts.RailGetPullRequestStatus(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef), cred.toRail())
		if e != nil {
			return e
		}
		st = csPullRequestStatusView{
			CheckRollup:   mapCheckState(prStatus.CheckRollup),
			ApprovalCount: int(prStatus.ApprovalCount),
			Mergeable:     prStatus.Mergeable,
		}
		return nil
	}); rerr != nil {
		return csPullRequestStatusView{}, rerr
	}
	return st, nil
}

// relayArchApprovalAndRecord relays the architecture +1 (PostReview Approve) to the PR
// and records the audit-worthy ArchApproved fact (D-PA-GIT §5). Called once the
// activity's review has passed (the architect's in-app sign-off). A dormant slice is a
// no-op.
//
// This is a GATE-DECISION-TIME rail call: the human it waits for may have been away for
// hours, so it re-mints an expired credential before it speaks to the rail (F-QA2-44).
func (wf *csWorkflows) relayArchApprovalAndRecord(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
) error {
	if !gf.enabled {
		return nil
	}

	cred, err := wf.liveCred(ctx, gf)
	if err != nil {
		return err
	}
	if err := wf.railWithAuthRetry(ctx, func() error {
		return wf.Acts.RailPostReview(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef),
			sourcecontrol.ReviewSubmission{Verdict: sourcecontrol.ReviewApprove, Body: archApprovalBody(in.ActivityID)},
			cred.toRail())
	}); err != nil {
		return err
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityArchApproved(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID), gf.cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// mergeAndRecord PERFORMS the gated merge (the interventionEngine gate already
// cleared in workflow code) and, on a Merged result, records the terminal git fact
// (D-PA-GIT §5). A dormant slice is a no-op.
//
// THE TWO PRECONDITIONS ARE THE GUARD, and neither survived the co-author files'
// deletion. `RailMergePullRequest` was called bare, over a PR whose CI rollup this code
// had never asked about and whose mergeability it had never checked:
//
//   - CI GREEN is the "blocks merge" trust boundary. The whole point of the required check
//     is that nothing lands over a red build; mirrorCIRollup reads the rollup but only as
//     DECORATION (it swallows its own error), so a red or still-pending PR reached the
//     merge verb and the rail refused it — surfacing as the generic MergeNotCompleted,
//     which says "not mergeable" for a PR that is simply not finished building.
//
//   - MERGEABLE (F80c) is the diverged-branch case. main advances under the activity
//     branch — a staleness ack, a question seed, another activity's own commit — and their
//     project.json (a server-owned, single-writer-per-slot document) conflicts, so
//     GitHub's mergeable_state goes dirty. A merge attempt then fails, and re-approving
//     fails IDENTICALLY, forever: the branch stays diverged, so the loop has no exit. The
//     fix is to RECONCILE the branch from main and try once more.
//
// A red rollup is NOT retried here and NOT merged: it is mirrored onto the head state (so
// the screen says what the rail said) and returned as a NAMED terminal the operator can
// act on. Re-opening the activity is the documented repair (see commitDesignArtifacts),
// and it is the right one — nothing this workflow can do makes a build go green.
func (wf *csWorkflows) mergeAndRecord(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	preserveKinds []projectstate.ArtifactKind,
	headVersion *projectstate.Version,
) error {
	if !gf.enabled {
		return nil
	}

	if err := wf.guardMergePreconditions(ctx, in, gf, preserveKinds, headVersion); err != nil {
		return err
	}

	cred, err := wf.liveCred(ctx, gf)
	if err != nil {
		return err
	}
	var merged bool
	if err := wf.railWithAuthRetry(ctx, func() error {
		mr, e := wf.Acts.RailMergePullRequest(ctx, gf.repoRef, sourcecontrol.PullRequestRefFromString(gf.prRef), cred.toRail())
		if e != nil {
			return e
		}
		merged = mr.Merged
		return nil
	}); err != nil {
		return err
	}
	if !merged {
		return temporal.NewNonRetryableApplicationError(
			"gated merge did not complete (PR not mergeable)", "MergeNotCompleted", nil)
	}

	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityMerged(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID), gf.cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// guardMergePreconditions is mergeAndRecord's two refusals, and it returns nil ONLY when the
// PR may actually be merged. Split out from the merge body so each half reads as the one
// question it asks.
func (wf *csWorkflows) guardMergePreconditions(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	preserveKinds []projectstate.ArtifactKind,
	headVersion *projectstate.Version,
) error {
	st, err := wf.readPRStatus(ctx, gf)
	if err != nil {
		return err
	}

	// Precondition 1: the required check must be green. Mirror what the rail said before
	// refusing, so the head state carries the reason rather than only the refusal.
	if st.CheckRollup != projectstate.CICheckSuccess {
		if verr := wf.mirrorObservedRollup(ctx, in, gf, st, headVersion); verr != nil {
			return verr
		}
		return temporal.NewNonRetryableApplicationError(
			"the activity PR was not merged because its required CI check is "+st.CheckRollup.String()+
				" — nothing lands over a build that is not green; re-open the activity once CI passes",
			"MergeBlockedByCI", nil)
	}

	// Precondition 2 (F80c): green but DIVERGED. Reconcile from main — which pushes a commit
	// and therefore re-triggers CI — then ask ONCE more. Exactly once: a loop here would be
	// the infinite re-approve wearing a different hat.
	if st.Mergeable {
		return nil
	}
	if rerr := wf.reconcileDivergedBranch(ctx, in, gf, preserveKinds, headVersion); rerr != nil {
		return rerr
	}
	if st, err = wf.readPRStatus(ctx, gf); err != nil {
		return err
	}
	if st.CheckRollup == projectstate.CICheckSuccess && st.Mergeable {
		return nil
	}
	if verr := wf.mirrorObservedRollup(ctx, in, gf, st, headVersion); verr != nil {
		return verr
	}
	return temporal.NewNonRetryableApplicationError(
		"the activity PR had diverged from main; the branch was reconciled and CI is re-validating — "+
			"re-open the activity once the check is green",
		"MergeBranchReconciled", nil)
}

// mirrorObservedRollup records a refused merge's OBSERVED rollup onto the per-activity git
// head state. Unlike mirrorCIRollup, which is decoration on the poll path and swallows its
// error, this write is load-bearing: it is the only place the reason a merge was refused
// becomes readable off the project state, so its failure is the caller's failure.
func (wf *csWorkflows) mirrorObservedRollup(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	st csPullRequestStatusView,
	headVersion *projectstate.Version,
) error {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityCIObserved(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID),
			st.CheckRollup, gf.cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// reconcileTargetOf reads the PRESERVE SET off the LIFECYCLE's own slots, so a lifecycle that
// gains or loses a design task moves this answer with it. Every kind the walk drafts on the
// activity branch is preserved by the reconcile; every other slot is adopted from main. A
// construction lifecycle drafts none, and the empty set adopts main's every slot — which is
// exactly right, and now SAYS so rather than relying on a zero value (see
// reconcileDivergedBranch, where the zero value's real meaning is written down).
func reconcileTargetOf(lc methodassets.Lifecycle) []projectstate.ArtifactKind {
	return designSlotsOfLifecycle(lc)
}

// reconcileDivergedBranch overlays main's slots onto the activity branch tip so a
// MERGEABLE=false PR becomes mergeable again (F80c). It runs through
// applyRecoveringOnBranch so a stale-version Conflict re-reads the BRANCH version and
// retries within bounded attempts; seeding expectedVersion 0 is safe because an existing
// branch row trips the version guard → Conflict → the loop re-reads the real one.
//
// This is the ONLY workflow caller of designSessionAccess.reconcileBranchFromMain. The verb
// has been registered and implemented with none since the co-author files went, which is
// why F80c came back.
//
// THE WHOLE PRESERVE SET NOW CROSSES THE WIRE (stage 4b2 Task 7). Task 6 widened the store
// while the generated Activity still took one kind, so a lifecycle with two or more in-flight
// design slots took a refusal here — `reconcileWireKind`, the `!expressible` arm and the
// `delivery.merge.reconcileUnavailable` log key — because sending preserveKinds[0] of four IS
// F80c: three live drafts replaced by main's older copies, in the path whose whole job is to
// rescue the branch. All three are gone with the `kinds` param. An EMPTY set is passed
// verbatim and means "preserve nothing, adopt main entirely", which is the construction case;
// it is no longer spelled as the zero ArtifactKind, which was KindMission and had been
// quietly preserving the branch's mission slot.
func (wf *csWorkflows) reconcileDivergedBranch(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	preserveKinds []projectstate.ArtifactKind,
	headVersion *projectstate.Version,
) error {
	if gf.branch == "" {
		return nil
	}
	if _, err := wf.applyRecoveringOnBranch(ctx, in.ProjectID, gf.branch, 0,
		func(expected projectstate.Version) (projectstate.Version, error) {
			return wf.Acts.DesignSessionReconcileBranchFromMain(ctx, projectstate.ProjectID(in.ProjectID), expected, gf.branch, preserveKinds)
		}); err != nil {
		return err
	}
	// The branch write does NOT advance main's version token, so headVersion is left alone
	// deliberately — feeding a branch-scoped version into a main-scoped CAS is the exact
	// mistake applyRecoveringOnBranch's own comment warns about.
	_ = headVersion
	return nil
}

// recordActivityStarted marks the activity Running in the per-activity construction
// head-state at the TOP of the spine (Task 3), BEFORE any dispatch. This is what
// flips the activity out of NotStarted so the pump's eligibility selection
// (nextEligibleActivity over proj.ActivityExecution) does not re-dispatch it on a
// concurrent/redundant tick. Cred-threaded like the four git head-state records; a
// dormant slice (git unwired) is a no-op (the live Postgres composition has no
// per-activity construction head-state, so the gate degrades to the child-workflow-id
// idempotency the pump already relies on). It mints a credential ONCE for the
// started+completed pair via the supplied gitForward.cred when the branch lifecycle
// has already minted one, else mints its own.
//
// It also stamps the activity's classified (Type, Variant) onto the head-state row —
// the RA seeds its Phases slice from that pair, so an unstamped row seeded the 5-phase
// SERVICE set for every activity and made earned value disagree with the profile the
// workflow walks.
func (wf *csWorkflows) recordActivityStarted(
	ctx workflow.Context,
	in constructActivityInput,
	cred railCredEnvelope,
	headVersion *projectstate.Version,
) error {
	if wf.GitStatus == nil {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityStarted(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID),
			in.Activity.Type, in.Activity.Variant, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// recordActivityCompleted marks the activity Done in the per-activity construction
// head-state at the END of the spine (Task 3), alongside RecordActivityExited. This
// is what unblocks dependents in the pump's eligibility selection (projectstate.AllDepsSatisfied). A
// dormant slice is a no-op.
func (wf *csWorkflows) recordActivityCompleted(
	ctx workflow.Context,
	in constructActivityInput,
	cred railCredEnvelope,
	headVersion *projectstate.Version,
) error {
	if wf.GitStatus == nil {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.GitStatusRecordActivityCompleted(ctx, projectstate.ProjectID(in.ProjectID), expected, string(in.ActivityID), cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// startedCred resolves the credential the construction started/completed records
// thread, and reports whether those records fire at all. It deliberately gates on the
// CONSTRUCTION-STATUS slice (GitStatus), NOT the full PR-rail slice — the per-activity
// Running/Done head-state is what drives the pump's eligibility cascade and is
// independent of the branch→PR→merge lifecycle:
//
//   - PR-rail wired (gitEnabled — Rail+GitStatus+Repo, the CLOUD GitHub profile): mint
//     the short-lived installation token via the rail; it is reused by the branch/PR
//     lifecycle AND the started/completed records.
//   - GitStatus wired but no PR rail (the LOCAL/dry-run profile — file:// repo, no
//     GitHub): the status records still fire so the cascade advances, threading a ZERO
//     credential. The local git store's gitAuth ignores the credential entirely
//     (GitAuth{Local:true}), so no token is needed; the PR-rail lifecycle stays dormant
//     (gitEnabled is false, so gf.enabled is false on every branch/PR/CI/merge step).
//   - GitStatus unwired (the legacy Postgres-store composition): false — the
//     started/completed records are no-ops and the pump degrades to child-workflow-id
//     idempotency.
//
// Minted/resolved ONCE at the top of the spine and reused for the completed record.
func (wf *csWorkflows) startedCred(ctx workflow.Context, projectID ProjectID) (railCredEnvelope, bool, error) {
	if wf.GitStatus == nil {
		return railCredEnvelope{}, false, nil
	}
	// CLOUD profile: a PR rail + repo resolve ⇒ mint the real installation token.
	if repoRef, ok := wf.gitEnabled(projectID); ok {
		cred, err := wf.mintCred(ctx, repoRef)
		if err != nil {
			return railCredEnvelope{}, false, err
		}
		return cred, true, nil
	}
	// LOCAL/dry-run profile: status records fire with a zero (ignored) credential.
	return railCredEnvelope{}, true, nil
}

// mintCred runs the GENERATED getInstallationToken invoker → the short-lived credential
// the Manager threads into every rail + record verb for this activity's lifecycle.
func (wf *csWorkflows) mintCred(ctx workflow.Context, repoRef sourcecontrol.RepoRef) (railCredEnvelope, error) {
	cred, err := wf.Acts.RailGetInstallationToken(ctx, repoRef)
	if err != nil {
		return railCredEnvelope{}, err
	}
	return railCredEnvelope{Bytes: cred.Bytes, ExpiresAt: cred.ExpiresAt}, nil
}

// liveCred RE-MINTS THE INSTALLATION TOKEN WHEN THE ONE IN HAND HAS RUN OUT, and it is
// the whole of F-QA2-44 expressed on the child's shape.
//
// The child mints ONE token at openActivityRow and threads it through the entire walk.
// A walk is not a burst: it parks at human gates. A GitHub App installation token lives
// about an hour; a founder who approves two hours after the gate opened is the ORDINARY
// case, not the pathological one (observed live on the retired rail: an approve 8+ hours
// after the dispatch, every merge-window verb 403ing forever). The platform's github
// ClassifyStatus renders that 403 as a NON-RETRYABLE Auth ApplicationError, so neither
// the Activity RetryPolicy nor railWithAuthRetry can heal it — the walk simply fails at
// the merge, and re-approving fails the same way. railCredEnvelope has carried ExpiresAt
// since it was written; nothing ever read it.
//
// LAZY WITH AN EXPIRY CHECK rather than a re-mint bolted onto each gate-decision site:
// the child reaches its rail verbs from several places and the question every one of them
// asks is the same one — "is this token still good?" — so it is asked ONCE, here, and a
// call site that never runs long never pays for a mint.
//
// The credential is refreshed ON gf, so the head-state record that follows each rail verb
// (which threads the same credential into the git store) gets the fresh token too.
//
// REPLAY SAFETY without a version gate. The decision reads workflow.Now(ctx) and the
// ExpiresAt of a credential that a recorded Activity result produced — BOTH from history
// — so a replay reaches the identical verdict and schedules the identical commands. What
// it cannot cover is an execution STARTED under code that had no such check: replaying
// that history with this code would mint where history recorded a rail call. Stage 4b1
// already mandates a drain before this branch deploys (docs/bugs/…-stage4b1-earmarks.md),
// and that drain is what makes the gate unnecessary here.
func (wf *csWorkflows) liveCred(ctx workflow.Context, gf *gitForward) (railCredEnvelope, error) {
	// A dormant rail threads a ZERO credential the local git store ignores entirely
	// (GitAuth{Local:true}); its zero ExpiresAt must not read as "expired".
	if !gf.enabled {
		return gf.cred, nil
	}
	if !workflow.Now(ctx).Add(railCredRenewSkew).After(gf.cred.ExpiresAt) {
		return gf.cred, nil
	}
	cred, err := wf.mintCred(ctx, gf.repoRef)
	if err != nil {
		return railCredEnvelope{}, err
	}
	gf.cred = cred
	return cred, nil
}

// railWithAuthRetry runs ANY rail call with a BOUNDED WORKFLOW-SIDE retry on a
// transient-403-as-Auth fault (QA F35 + F-QA2-49). Restored onto the generic child, where
// all seven rail call sites were left bare by the co-author files' deletion.
//
// WHY THE RETRY CANNOT LIVE IN THE ACTIVITY RETRY POLICY, which is the whole reason this
// helper exists: the platform github ClassifyStatus CONFLATES GitHub's secondary
// rate-limit 403 with a real permission denial — both become fwra.Auth, which the
// generated invokers mark NON-RETRYABLE. So the one fault an API-heavy draft job is most
// likely to hit is the one the RetryPolicy refuses to retry, and the first blip fails the
// whole walk terminally. That is a logged incident, not a hypothetical: three openPR
// attempts inside 15s, all 403, StageDraftFailed — and a manual retry 15 minutes later
// succeeded on the first try.
//
// The budget is the long one by default (60s → 120s → 240s, four attempts, ~7 min):
// secondary rate limits demand a >=60s cool-down, so the original ~30s budget expired
// ENTIRELY INSIDE the cool-down window and retried three times for nothing. It stays
// BOUNDED so a GENUINE permission denial still reaches the caller, which contains it.
// workflow.Sleep gives deterministic backoff; cancellation propagates immediately;
// transport blips (Transient) are still retried INSIDE the Activity by its own options.
//
// The old two-budget/GetVersion arrangement is deliberately NOT restored: it existed to
// keep pre-F-QA2-49 histories on the short schedule, and those histories belong to
// workflow types this branch deleted.
func (wf *csWorkflows) railWithAuthRetry(ctx workflow.Context, call func() error) error {
	backoff := railAuthRetryBaseBackoff
	for attempt := 1; ; attempt++ {
		err := call()
		if err == nil {
			return nil
		}
		if temporal.IsCanceledError(err) || !isRailAuthFault(err) {
			return err
		}
		if attempt >= railAuthRetryMaxAttempts {
			return err
		}
		workflow.GetLogger(ctx).Warn("rail 403 (auth or secondary rate limit); bounded workflow-side retry",
			"attempt", attempt, "ofAttempts", railAuthRetryMaxAttempts)
		if serr := workflow.Sleep(ctx, backoff); serr != nil {
			return serr
		}
		if backoff *= 2; backoff > railAuthRetryMaxBackoff {
			backoff = railAuthRetryMaxBackoff
		}
	}
}

// gitnaming.go holds the Manager's provider-NEUTRAL, DETERMINISTIC naming + the git
// Activity option presets for the git-forward slice (C-MCN-GIT). The names are
// Manager-derived (the branch/PR/label vocabulary the rail maps to a git ref INSIDE
// the seam); determinism is load-bearing for the rail's deterministic-name idempotency
// (a workflow retry re-opening the same branch/PR is a no-op in the rail).

// loadReviewSnapshot performs the per-execution start snapshot (B5): it reads the project
// ONCE (an Activity, recorded in history → replay-safe) and captures the committed
// ReviewPolicy BY VALUE (the gate's ONLY policy source; NEVER re-read mid-loop), seeds the
// LIVE completedPhases skip-guard (B2 resumability) from the activity's PhaseCompletion
// slice, and captures the contract keys for the gate's reviewer set.
//
// It returns the WHOLE read (stage 4b1: Task 8's R8-5 returned the activity's execution row;
// Task 10 widened that to the project it was taken off). Two readers need more than the row
// and neither may make a SECOND whole-project read — a second durable command, and a second
// chance for the two reads to disagree: seedWalkFromLedger needs the row AND the committed
// design SLOTS (a design task whose artifact is already committed on main must not be
// re-drafted), and the caller needs the ReviewPolicy. One read, three readers.
//
// Temporal versioning guard (replay safety): this readProject call was ADDED by the
// construction-review-policy-snapshot feature AFTER the workflow was first shipped.
// Workflows already in flight at deploy time have no history event for this call; replaying
// them against new code would produce a non-determinism error. GetVersion guards the new
// block so pre-feature in-flight executions (DefaultVersion) skip it entirely — reviewPolicy
// stays zero (empty → inert → no gate) and completedPhases stays initialized-empty. The gate
// takes effect only for csWorkflows started after the feature deployed (v >= 1).
func (wf *csWorkflows) loadReviewSnapshot(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
) (projectstate.ReviewPolicy, projectstate.Project, error) {
	var reviewPolicy projectstate.ReviewPolicy
	var snap projectstate.Project
	v := workflow.GetVersion(ctx, "construction-review-policy-snapshot", workflow.DefaultVersion, 1)
	if v < 1 {
		return reviewPolicy, snap, nil
	}
	snap, srErr := wf.readProject(ctx, in.ProjectID)
	if srErr != nil && !isReadNotFound(srErr) {
		return reviewPolicy, snap, srErr
	}
	reviewPolicy = snap.ReviewPolicy
	// LEDGER-AWARE SEED (architect (D), D.1.3). The pump now dispatches an
	// integration-pending row — one whose history lives in the attempt ledger alone — so
	// the seed must read the row as the view does, or the run would redo phases the
	// ledger records as passed. GetVersion (always called, same change id as the pump's
	// selection) pins an execution that seeded from the stored Phases only to that seed.
	//
	// The GetVersion call stays unconditional (a recorded history must see the same
	// marker it recorded), but its DEFAULT arm is now empty: that arm read the row's
	// stored phase-completion slice, which stage-3 task 4 stopped storing — a lifecycle
	// phase is complete iff its gate task's latest attempt passed, and the ledger is the
	// only record of that. No pre-marker history it replays carries stored completions
	// anyway (the two ledger-seed fixtures hold attempts and no phase set), so the arm
	// seeds exactly what it seeded before: nothing.
	ledgerSeed := workflow.GetVersion(ctx, changeLedgerPartialResume, workflow.DefaultVersion, 1) >= 1
	if acs, ok := snap.ActivityExecution[string(in.ActivityID)]; ok && ledgerSeed {
		seedResumeFromLedger(state, in.Activity, acs)
	}
	// OPERATOR-NOTE DELIVERY (plan B1.4). GetVersion is always called here, so a new
	// execution records the marker before its first dispatch and an execution that
	// recorded none stays wholly old: no note recorded, carried or stamped, and no
	// scaffold sync. Notes still pending on the row (a re-queue's note, or one an
	// earlier run recorded but never dispatched) ride this run's first agent dispatch.
	state.noteDelivery = workflow.GetVersion(ctx, changeOperatorNoteDelivery, workflow.DefaultVersion, 1) >= 1
	if acs, ok := snap.ActivityExecution[string(in.ActivityID)]; ok && state.noteDelivery {
		state.pendingNotes = projectstate.PendingOperatorNotes(acs)
	}
	// EXECUTION LEDGER (stage 3). GetVersion is always called here, so a new execution
	// records the marker before its first dispatch and an execution that recorded none
	// stays wholly old: no activity opened, no attempt recorded, no round opened, no
	// verdict appended. Its history has no events for those Activities and never will.
	state.executionLedger = workflow.GetVersion(ctx, changeExecutionLedger, workflow.DefaultVersion, 1) >= 1
	// A send-back's feedback is the ROUND's now, not a stored note, so a run that ended
	// between the rejection and the redraft's dispatch must recover it from there. Without
	// this the next run re-walks the rejected phase and dispatches the redraft with NO
	// steer at all — strictly worse than the NoteSendBack this replaced, which survived a
	// run boundary because it was stored. Reads only; emits no command.
	if acs, ok := snap.ActivityExecution[string(in.ActivityID)]; ok && state.executionLedger {
		seedSendBackCarry(ctx, in, state, acs)
		// And the per-activity CAS token, off the SAME read — which is the whole reason
		// arming the guard needed no new Temporal command: the child already holds the row.
		// An activity with no row yet leaves it 0 (NoActivityVersionExpectation), the
		// posture of the writer that is about to birth it.
		state.activityVersion = acs.Version
	}
	state.reviewContracts = snapshotContractKeys(snap)
	// Task 7 non-overridable floor: snapshot ONCE whether the activity's committed
	// contract touches deploy/spend/schema — never re-evaluated mid-loop, mirroring
	// reviewPolicy itself. A missing contract (nil map lookup) reads as the zero
	// ServiceContract, which never touches the floor.
	state.floorTouched = projectstate.ContractTouchesReviewFloor(snap.ServiceContracts[in.Activity.ComponentID])
	return reviewPolicy, snap, nil
}

// finalizeActivity runs the clean-pass tail of an attempt (constructionManager.md §6.3
// steps 5a-8a): relay the architecture +1, record the change reviewed, perform the gated
// merge (interventionEngine is the App-only-merge authority), record the binary activity
// exit, and record the per-activity construction COMPLETED. The git-forward steps are
// no-ops when the slice is unwired.
func (wf *csWorkflows) finalizeActivity(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
	state *constructState,
	gitOn bool,
	startedCred railCredEnvelope,
	preserveKinds []projectstate.ArtifactKind,
) error {
	// --- Step 5a: relay the architecture +1 and record it (git-forward). ---
	if err := wf.relayArchApprovalAndRecord(ctx, in, gf, headVersion); err != nil {
		return err
	}

	// --- Step 6: record the change reviewed (head-state). ---
	v, e := wf.recordChangeReviewed(ctx, in, state, *headVersion, startedCred)
	if e != nil {
		return e
	}
	*headVersion = v

	// --- Step 6a: perform the gated merge and record it (git-forward). ---
	if err := wf.mergeAndRecord(ctx, in, gf, preserveKinds, headVersion); err != nil {
		return err
	}

	// --- Step 8: record the binary activity exit. Behind the fence RecordActivityOutcome
	// is the fold of the exited + completed pair below: both stamped the SAME write-once
	// CompletedAt, which is the whole of an exit now that the coarse roll-up is derived,
	// so the two calls became one. ---
	if state.executionLedger {
		if err := wf.recordExecutionOutcome(ctx, in, state, headVersion, startedCred,
			projectstate.ActivityOutcomeCompleted, projectstate.FailureReasonUnknown, ""); err != nil {
			return err
		}
	} else {
		v2, e2 := wf.recordActivityExited(ctx, in, *headVersion, projectstate.ActivityOutcomeCompleted, startedCred)
		if e2 != nil {
			return e2
		}
		*headVersion = v2

		// --- Step 8a: record the per-activity construction COMPLETED (Task 3). Flip the
		// activity to Done so the pump's eligibility selection unblocks its dependents on
		// the next tick. Dormant (no-op) when the git slice is unwired. ---
		if gitOn {
			if err := wf.recordActivityCompleted(ctx, in, startedCred, headVersion); err != nil {
				return err
			}
		}
	}

	state.stage = StageExited
	workflow.GetLogger(ctx).Info("construction activity exited", "activityId", in.ActivityID)
	return nil
}

// enterPhaseGate enters a phase approval gate after redrafts SendBack redrafts: the gate
// has no budget left once a further SendBack could not redraft it.
func (s *constructState) enterPhaseGate(ctx workflow.Context, key string, redrafts int) {
	s.redraftExhausted = redrafts+1 >= maxPhaseRedrafts
	s.enterHumanStage(ctx, StageAwaitingApproval, key, 0)
}

// enterHumanStage starts one occurrence of a human stage: the stage, the gate it waits
// at, the occurrence identity (workflow.Now), and — when wait > 0 — when it gives up.
func (s *constructState) enterHumanStage(ctx workflow.Context, stage ConstructionStage, gate string, wait time.Duration) {
	s.stage = stage
	s.awaitingGate = gate
	s.awaitingSince = workflow.Now(ctx)
	s.awaitingUntil = nil
	if wait > 0 {
		until := s.awaitingSince.Add(wait)
		s.awaitingUntil = &until
	}
}

// leaveHumanStage ends the current occurrence: it records the construction_gate_wait
// timer (tags: the gate CLASS, the outcome and the activity type — never the activity
// id, which would make the series unbounded) and the construction.gate.decided log line
// (the dependable surface while the prod OTLP export is an open earmark), then clears
// the awaiting fields AND the reviewer set.
//
// I1: the roster (and an engine refusal) describes the OCCURRENCE, not the activity, so
// it comes down with it. It used to be cleared on gate ENTRY only, which left a decided
// gate's reviewers on the session view until the next gate opened — and the Activity
// Experience's takeover card read them as live. surfaceReviewSet puts a fresh roster up
// on every entry, including a redraft's re-entry, so the pair stays balanced.
func (s *constructState) leaveHumanStage(ctx workflow.Context, activityType, outcome string) {
	waited := workflow.Now(ctx).Sub(s.awaitingSince)
	gateMetrics(ctx).WithTags(map[string]string{
		"gate":          humanGateClass(s.awaitingGate),
		"outcome":       outcome,
		"activity_type": activityType,
	}).Timer("construction_gate_wait").Record(waited)
	workflow.GetLogger(ctx).Info("construction.gate.decided",
		"projectId", string(s.projectID), "activityId", string(s.activityID),
		"gate", s.awaitingGate, "outcome", outcome,
		"waitedMs", waited.Milliseconds(), "awaitingSince", s.awaitingSince)
	s.awaitingGate, s.awaitingSince, s.awaitingUntil = "", time.Time{}, nil
	s.reviewSet, s.reviewSetError = nil, ""
}

// openActivity births the activity's execution row and pins the lifecycle in force.
func (wf *csWorkflows) openActivity(ctx workflow.Context, in constructActivityInput, state *constructState, cred railCredEnvelope, headVersion *projectstate.Version) error {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionOpenActivity(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), in.Activity.Type, in.Activity.Variant, lifecyclePinFor(in.Activity), cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// recordAttempt appends or resolves ONE attempt on the append-only task ledger. One id
// names one attempt: the pending record this opens and the terminal that resolves it are
// the SAME AttemptID, which is also the key the episode ledger carries as TargetRef.
func (wf *csWorkflows) recordAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	attempt projectstate.TaskAttemptInput,
) error {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionRecordAttemptOutcome(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), attempt, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// openWorkAttempt records the PENDING agent-work attempt a dispatch is about to burn, so
// a run that dies mid-dispatch leaves an attempt that says it started and never resolved
// rather than nothing at all. A no-op off the fence.
func (wf *csWorkflows) openWorkAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	task projectstate.MethodTask,
	attempt int,
	attemptID string,
) error {
	if !state.executionLedger || task == "" {
		return nil
	}
	return wf.recordAttempt(ctx, in, state, headVersion, cred, projectstate.TaskAttemptInput{
		AttemptID: attemptID,
		TaskID:    task,
		Attempt:   int64(attempt),
		Actor:     projectstate.ActorAgent,
		Outcome:   projectstate.OutcomePending,
	})
}

// resolveWorkAttempt resolves that attempt against the terminal observation, citing the
// episode the dispatch burned as its evidence. The episode append runs FIRST (the
// capture-seam's own ordering), so by the time this cites an episode id the ledger holds
// it; a dispatch that mined no summary cites nothing rather than a ref nobody can follow.
func (wf *csWorkflows) resolveWorkAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	task projectstate.MethodTask,
	attempt int,
	attemptID string,
	obs csPipelineObservation,
) error {
	if !state.executionLedger || task == "" {
		return nil
	}
	rec := projectstate.TaskAttemptInput{
		AttemptID: attemptID,
		TaskID:    task,
		Attempt:   int64(attempt),
		Actor:     projectstate.ActorAgent,
		Outcome:   attemptOutcomeFor(obs.Phase),
	}
	if obs.Episode != nil {
		rec.EvidenceKind, rec.EvidenceRef = projectstate.EvidenceEpisode, obs.Episode.EpisodeID
	}
	return wf.recordAttempt(ctx, in, state, headVersion, cred, rec)
}

// appendVerdict lands one reviewer's judgement and the comments it cites in ONE commit.
// A no-op when this gate opened no round.
func (wf *csWorkflows) appendVerdict(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	gate *gateLedger,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	verdict projectstate.ReviewVerdict,
	comments []projectstate.ReviewComment,
) error {
	if gate.roundID == "" {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionAppendReviewVerdict(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), gate.roundID, verdict, comments, nil, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// decideRound stamps the round's terminal — a separate, later fact from the verdicts on
// it, which is why it is a second verb and not a field of the first. It also records who
// the gate attempt this round settles will name as its actor.
func (wf *csWorkflows) decideRound(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	gate *gateLedger,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	outcome projectstate.ReviewRoundOutcome,
	decidedBy string,
) error {
	if gate.roundID == "" {
		return nil
	}
	gate.actor = projectstate.ActorSystem
	if decidedBy == decidedByOperator {
		gate.actor = projectstate.ActorHuman
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionDecideReviewRound(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), gate.roundID, outcome, decidedBy, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// closeGateRound appends the human's verdict — with the comments that rode with it — and
// then decides the round. Two verbs, not one: other reviewers append to the same round
// before the human's, and the decision is a separate terminal fact about the round.
func (wf *csWorkflows) closeGateRound(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	gate *gateLedger,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	verdict projectstate.VerdictKind,
	outcome projectstate.ReviewRoundOutcome,
	fb *ReviewFeedback,
) error {
	if !state.executionLedger || gate.roundID == "" {
		return nil
	}
	f := feedbackText(fb)
	if err := wf.appendVerdict(ctx, in, state, gate, headVersion, cred, projectstate.ReviewVerdict{
		ReviewerRole: gateRoleHuman,
		Actor:        gateActorOperator,
		Verdict:      verdict,
		Summary:      f.text,
		AttemptID:    gate.judgedAttemptID,
	}, roundComments(f.comments)); err != nil {
		return err
	}
	return wf.decideRound(ctx, in, state, gate, headVersion, cred, outcome, decidedByOperator)
}

// passGateAttempt records the PASSED attempt at the phase's review task — App A's binary
// exit criterion, written where every reader derives phase completion from. It is the
// fact RecordPhaseCompleted used to synthesize on the workflow's behalf; the workflow
// writes it itself now, which is why that verb is no longer called behind the fence.
//
// Evidence is what the round judged, so a completion points at the thing that was
// reviewed rather than at the empty artifactRef the retired verb always passed.
func (wf *csWorkflows) passGateAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	gate *gateLedger,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
) error {
	if gate.task == "" {
		return nil
	}
	kind, ref := gateEvidence(gate.subject)
	return wf.recordAttempt(ctx, in, state, headVersion, cred, projectstate.TaskAttemptInput{
		AttemptID:    projectstate.AttemptID(string(in.ActivityID), gate.task, gate.number),
		TaskID:       gate.task,
		Attempt:      int64(gate.number),
		Actor:        gate.actor,
		Outcome:      projectstate.OutcomePassed,
		EvidenceKind: kind,
		EvidenceRef:  ref,
	})
}

// rejectGateAttempt is passGateAttempt's send-back twin: a REJECTED attempt at the review
// task, which is what leaves the phase incomplete and makes the redraft that follows
// render as Löwy's "a failing review repeats the preceding task".
func (wf *csWorkflows) rejectGateAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	gate *gateLedger,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
) error {
	if !state.executionLedger || gate.task == "" {
		return nil
	}
	kind, ref := gateEvidence(gate.subject)
	return wf.recordAttempt(ctx, in, state, headVersion, cred, projectstate.TaskAttemptInput{
		AttemptID:    projectstate.AttemptID(string(in.ActivityID), gate.task, gate.number),
		TaskID:       gate.task,
		Attempt:      int64(gate.number),
		Actor:        gate.actor,
		Outcome:      projectstate.OutcomeRejected,
		EvidenceKind: kind,
		EvidenceRef:  ref,
	})
}

// recordExecutionOutcome stamps the activity's terminal on the execution row — the fold
// of the three retired terminals (exited / failed / completed). A non-zero reason IS the
// failure arm.
func (wf *csWorkflows) recordExecutionOutcome(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	outcome projectstate.ActivityOutcome,
	reason projectstate.FailureReason,
	detail string,
) error {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ActivityExecutionRecordActivityOutcome(ctx, projectstate.ProjectID(in.ProjectID), expected,
			state.activityVersion, string(in.ActivityID), outcome, reason, detail, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	state.rowAdvanced()
	return nil
}

// carrySendBackFeedback puts the send-back's feedback in front of the redraft WITHOUT
// recording it. Before stage 3 this was a NoteSendBack on the activity, and it was the
// only trace a send-back left anywhere. The round now holds the verdict, its summary and
// its comments, so a note beside it would be one fact stored twice; what the redraft
// still needs is the TEXT, and that rides the next dispatch as a workflow-local note
// nothing ever stamps delivered. Emits no command.
// THE ROUND IS THE SOURCE OF TRUTH AND THIS IS A CACHE OF IT. The note lives only in
// workflow memory, so it does not survive the run that made it — seedSendBackCarry
// rebuilds it from the durable round at the start of the next one.
//
// It is gated on noteDelivery for the same reason the recorded notes are: an execution
// without THAT marker must not add the operator_note dispatch input, because the seated
// construct workflow it dispatches into may predate the key.
func carrySendBackFeedback(ctx workflow.Context, in constructActivityInput, state *constructState, gate string, fb *ReviewFeedback) {
	f := feedbackText(fb)
	if !state.noteDelivery || strings.TrimSpace(f.text) == "" {
		return
	}
	state.noteSeq++
	note := projectstate.OperatorNote{
		NoteID:     operatorNoteID(in.ActivityID, workflow.GetInfo(ctx).WorkflowExecution.RunID, state.noteSeq),
		Kind:       projectstate.NoteSendBack,
		Gate:       gate,
		Text:       f.text,
		Comments:   noteComments(f.comments),
		RecordedAt: workflow.Now(ctx),
	}
	if state.ephemeralNotes == nil {
		state.ephemeralNotes = map[string]bool{}
	}
	state.ephemeralNotes[note.NoteID] = true
	state.pendingNotes = append(state.pendingNotes, note)
}

// seedSendBackCarry rebuilds the carry note from the DURABLE round, for every lifecycle
// phase whose latest gate round was sent back and whose redraft never went out. It runs
// once, at the start of a run, and is what makes the send-back's feedback survive a run
// boundary now that no note is stored for it.
func seedSendBackCarry(ctx workflow.Context, in constructActivityInput, state *constructState, acs projectstate.ActivityExecution) {
	for _, lifecyclePhase := range projectstate.ProfileFor(in.Activity.Type, in.Activity.Variant).PhaseIDs() {
		r, owed := owedSendBackRound(acs, lifecyclePhase)
		if !owed {
			continue
		}
		carrySendBackFeedback(ctx, in, state, lifecyclePhase.String(), roundFeedback(r))
	}
}

// recordOperatorNote keeps one operator note on the activity and, when its kind is
// delivered at all, queues it for the next agent dispatch. A no-op on an execution
// without the operator-note-delivery marker, and for a blank note (only a signal that
// bypassed the façade's checks can carry one).
func (wf *csWorkflows) recordOperatorNote(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
	headVersion *projectstate.Version,
	cred railCredEnvelope,
	kind projectstate.OperatorNoteKind,
	gate string,
	fb noteFeedback,
) error {
	if !state.noteDelivery || strings.TrimSpace(fb.text) == "" {
		return nil
	}
	state.noteSeq++
	note := projectstate.OperatorNoteInput{
		NoteID:   operatorNoteID(in.ActivityID, workflow.GetInfo(ctx).WorkflowExecution.RunID, state.noteSeq),
		Kind:     kind,
		Gate:     gate,
		Text:     fb.text,
		Comments: noteComments(fb.comments),
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordOperatorNote(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), note, cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	// A retired-facet write onto the row, fenced on note delivery rather than on the
	// execution ledger, so it lands on a ledger-on run too (see rowAdvanced).
	state.rowAdvanced()
	recorded := projectstate.OperatorNote{
		NoteID: note.NoteID, Kind: note.Kind, Gate: note.Gate, Text: note.Text, Comments: note.Comments,
		RecordedAt: workflow.Now(ctx),
	}
	// The RA's own rule decides what is pending (a skip note never is).
	state.pendingNotes = append(state.pendingNotes,
		projectstate.PendingOperatorNotes(projectstate.ActivityExecution{OperatorNotes: []projectstate.OperatorNote{recorded}})...)
	return nil
}

// submitCarryingNotes dispatches one agent job carrying the pending notes, then — only
// once the submit has succeeded — stamps delivered to attemptID each note the block
// carried IN FULL, and drops those from the queue. A note the cap withheld, a note whose
// stamp failed (at-least-once, see the section comment) and every note of a failed
// submit stay pending for the next dispatch. A note already carried into attemptID is
// never carried into it again.
//
// It takes the COMPOSED spec (stage 4b1 Task 11) rather than composing one from (in, phase):
// the two callers describe a dispatch differently — the retired flat walk holds a phase and
// the activity's classified pair, the generic child holds a lifecycle task with its own
// command — and the note-carrying rule is identical for both. The one field this function
// owns is OperatorNote, which is the whole of what it is for.
func (wf *csWorkflows) submitCarryingNotes(
	ctx workflow.Context,
	in constructActivityInput,
	spec pipelineSpec,
	state *constructState,
	attemptID string,
	gf *gitForward,
	headVersion *projectstate.Version,
) (pipelineHandle, error) {
	var carry []projectstate.OperatorNote
	for _, n := range state.pendingNotes {
		if state.carriedTo[n.NoteID] != attemptID {
			carry = append(carry, n)
		}
	}
	notes := renderOperatorNotes(carry)
	spec.OperatorNote = notes.block
	handle, err := wf.submitPipeline(ctx, spec)
	if err != nil {
		return pipelineHandle{}, err
	}
	delivered := map[string]bool{}
	for _, n := range notes.whole {
		if state.carriedTo == nil {
			state.carriedTo = map[string]string{}
		}
		state.carriedTo[n.NoteID] = attemptID
		// A workflow-local send-back note (stage 3) has no stored note to stamp: the review
		// round is its record, and the block it rode is its delivery. It leaves the queue
		// having been carried, without a store call that would only fail NotFound.
		if state.ephemeralNotes[n.NoteID] {
			delivered[n.NoteID] = true
			continue
		}
		if wf.stampNoteDelivered(ctx, in, state, n.NoteID, attemptID, gf, headVersion) {
			delivered[n.NoteID] = true
		}
	}
	var still []projectstate.OperatorNote
	for _, n := range state.pendingNotes {
		if !delivered[n.NoteID] {
			still = append(still, n)
		}
	}
	state.pendingNotes = still
	if len(still) > 0 {
		workflow.GetLogger(ctx).Info("operator notes stay pending after this dispatch",
			"activityId", string(in.ActivityID), "attemptId", attemptID, "pending", len(still), "withheldByTheCap", notes.withheld)
	}
	return handle, nil
}

// stampNoteDelivered records noteID delivered to attemptID and reports whether the store
// now says so. It never fails the run: the job is already dispatched. A stamp that still
// fails after its retry window leaves the note pending (at-least-once); a stamp the store
// refuses because the note was already delivered to another attempt means it is no
// longer pending, so that reads as delivered.
func (wf *csWorkflows) stampNoteDelivered(ctx workflow.Context, in constructActivityInput, state *constructState, noteID, attemptID string, gf *gitForward, headVersion *projectstate.Version) bool {
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordOperatorNoteDelivered(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), noteID, attemptID, gf.cred.toProjectState())
	})
	if err == nil {
		*headVersion = v
		// The stamp applied, so the row advanced (see rowAdvanced). The two arms below did
		// NOT apply one — a ContractMisuse refusal writes nothing — so neither advances it.
		state.rowAdvanced()
		return true
	}
	if isRAContractMisuse(err) {
		workflow.GetLogger(ctx).Warn("the store already holds this note as delivered to another attempt; it is not pending",
			"activityId", string(in.ActivityID), "noteId", noteID, "attemptId", attemptID, "error", err.Error())
		return true
	}
	workflow.GetLogger(ctx).Warn("the note rode the dispatch but its delivery stamp failed; it stays pending and the next attempt carries it again",
		"activityId", string(in.ActivityID), "noteId", noteID, "attemptId", attemptID, "error", err.Error())
	return false
}

// syncScaffoldBeforeDispatch converges the repo's seated managed scaffold (the construct
// workflow among it) onto this server's template before a GitHub-venue dispatch. The
// local venue has no seated workflow (gf is dormant), so it is a no-op there. ok=false
// means the sync failed and nothing may be dispatched; obs is the failed run to report.
func (wf *csWorkflows) syncScaffoldBeforeDispatch(ctx workflow.Context, in constructActivityInput, gf *gitForward) (csPipelineObservation, bool) {
	if !gf.enabled {
		return csPipelineObservation{}, true
	}
	cred, cerr := wf.liveCred(ctx, gf)
	if cerr != nil {
		workflow.GetLogger(ctx).Error("the installation token could not be re-minted; nothing was dispatched",
			"activityId", string(in.ActivityID), "error", cerr.Error())
		return csPipelineObservation{
			Phase:      PipelineFailed,
			Diagnostic: "the rail credential could not be minted, so nothing was dispatched: " + cerr.Error(),
		}, false
	}
	var changed bool
	err := wf.railWithAuthRetry(ctx, func() error {
		c, e := wf.Acts.RailSyncManagedScaffold(ctx, gf.repoRef, cred.toRail())
		changed = c
		return e
	})
	if err != nil {
		workflow.GetLogger(ctx).Error("managed-scaffold sync failed; nothing was dispatched",
			"activityId", string(in.ActivityID), "error", err.Error())
		return csPipelineObservation{
			Phase: PipelineFailed,
			Diagnostic: "managed-scaffold sync failed — the seated construct workflow could not be proven current, " +
				"so nothing was dispatched: " + err.Error(),
		}, false
	}
	if changed {
		workflow.GetLogger(ctx).Info("managed scaffold drifted; re-seated the construct workflow before dispatch",
			"activityId", string(in.ActivityID))
	}
	return csPipelineObservation{}, true
}

// runMergePipeline dispatches the merge job through the pipeline seam and polls
// to a terminal observation (the local arm performs the merge synchronously, so
// the first observe is normally already terminal; the bounded poll mirrors
// runPipeline's discipline). The spec deliberately carries NO "command"/"phase"
// inputs — the job key routes it inside the local arm; it never spawns claude.
func (wf *csWorkflows) runMergePipeline(ctx workflow.Context, in constructActivityInput, state *constructState) (csPipelineObservation, error) {
	handle, err := wf.Acts.PipelineSubmitAgenticJob(ctx, agenticjob.PipelineSpec{
		ProjectID:  agenticjob.ProjectID(string(in.ProjectID)),
		ActivityID: agenticjob.ConstructionActivityID(string(in.ActivityID)),
		Steps: []agenticjob.PipelineStep{{
			Name:      "build",
			Toolchain: agenticjob.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: map[string]string{
			agenticjob.DispatchInputJobKey: agenticjob.DispatchJobMerge,
			"activity_id":                  string(in.ActivityID),
		},
	})
	if err != nil {
		return csPipelineObservation{}, err
	}
	h := pipelineHandle{Name: agenticjob.PipelineHandleString(handle)}
	for poll := range maxObserveTotalPolls {
		obs, oerr := wf.observePipeline(ctx, h)
		if oerr != nil {
			return csPipelineObservation{}, oerr
		}
		ph := obs.Phase
		state.pipelinePhase = &ph
		if obs.Phase == PipelineSucceeded || obs.Phase == PipelineFailed || obs.Phase == PipelineCancelled {
			// agentic=false: the merge job merges a branch, it never spawns an agent, so a
			// missing summary here is not a loss and must not be recorded as a gap. The
			// capture stays wired so a merge job that ever DOES mine one is not dropped.
			// NOTE the late-episode grace is deliberately NOT taken here — it lives inside
			// captureEpisode, gated on agentic, so a cancelled merge never spends 20s
			// waiting for a summary a merge can by construction never produce.
			//
			// Task attribution (Task 10): the merge job has no Figure A-1 task of its own —
			// App A's twelve tasks stop at Code Review, and landing the reviewed branch on
			// main is this platform's own automation, not a Method task. TaskConstruction is
			// the best-available attribution (merge only runs after Construction's gate has
			// passed, as the mechanical tail of landing that task's work), sharing its
			// attempt counter rather than inventing a task that Figure A-1 does not have.
			mergeTask := projectstate.TaskConstruction
			wf.captureEpisode(ctx, in, h, obs, false, mergeTask, state.nextTaskAttempt(mergeTask))
			return obs, nil
		}
		_ = workflow.Sleep(ctx, observeInterval(poll))
	}
	return csPipelineObservation{Phase: PipelineFailed, Diagnostic: "merge pipeline did not reach a terminal phase within the poll budget"}, nil
}

// recordChangeReviewed applies the head-state transition with the Conflict loop. The
// Manager-minted cred is threaded into the write (empty/zero in dev/dry-run).
//
// Another retired-facet verb that writes the row unfenced (see recordPhaseStarted): it
// lands between the gate's last round write and the outcome the ledger records, so the
// run's copy of the row version follows its stamp.
func (wf *csWorkflows) recordChangeReviewed(ctx workflow.Context, in constructActivityInput, state *constructState, seed projectstate.Version, cred railCredEnvelope) (projectstate.Version, error) {
	v, err := wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordChangeReviewed(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), cred.toProjectState())
	})
	if err != nil {
		return 0, err
	}
	state.rowAdvanced()
	return v, nil
}

// recordActivityExited applies the binary-exit head-state transition.
func (wf *csWorkflows) recordActivityExited(ctx workflow.Context, in constructActivityInput, seed projectstate.Version, outcome projectstate.ActivityOutcome, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordActivityExited(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), outcome, cred.toProjectState())
	})
}

// Shared workflow-context helper (used by 2 csWorkflows); lives in its first caller's file per the file-layout standard.
// readVersionE runs the cheap ReadProjectVersion GENERATED invoker (B8: migrated off the
// custom ReadProjectVersionActivity) and returns ONLY the head-state optimistic-
// concurrency token, surfacing errors (including the brand-new project's fwra.NotFound)
// to the caller. Replaces the wasteful whole-aggregate read that shipped the entire
// encoded Project across the Temporal Activity boundary for a uint64 (architect's
// fast-follow). The invoker's Opts hook applies csReadProjectActivityOptions (identical
// preset, keyed "projectStateAccess.readProjectVersion" — workermanifest.go).
func (wf *csWorkflows) readVersionE(ctx workflow.Context, projectID ProjectID) (projectstate.Version, error) {
	return wf.Acts.ProjectStateReadProjectVersion(ctx, projectstate.ProjectID(projectID))
}

// Shared workflow-context helper (used by 2 csWorkflows); lives in its first caller's file per the file-layout standard.
// readVersion reads the current head Version (0 for a brand-new project or on any
// read error — the read-your-writes seed treats absence as version 0).
func (wf *csWorkflows) readVersion(ctx workflow.Context, projectID ProjectID) projectstate.Version {
	v, err := wf.readVersionE(ctx, projectID)
	if err != nil {
		return 0
	}
	return v
}

// Shared workflow-context helper (used by 2 csWorkflows); lives in its first caller's file per the file-layout standard.
// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to systemdesign).
func (wf *csWorkflows) applyRecovering(
	ctx workflow.Context,
	projectID ProjectID,
	seed projectstate.Version,
	apply func(expected projectstate.Version) (projectstate.Version, error),
) (projectstate.Version, error) {
	expected := seed
	for attempt := 0; ; attempt++ {
		v, err := apply(expected)
		if err == nil {
			return v, nil
		}
		if !isConflict(err) {
			return 0, err
		}
		if attempt+1 >= maxMutateConflictAttempts {
			return 0, temporal.NewNonRetryableApplicationError(
				"head-state conflict did not converge within bounded attempts",
				"MutateConflictExhausted", err)
		}
		next, rerr := wf.readVersionE(ctx, projectID)
		if rerr != nil {
			if isReadNotFound(rerr) {
				expected = 0
				continue
			}
			return 0, rerr
		}
		// THE ROW RE-READ (stage 4b1, ruling R-A). The project version alone cannot tell an
		// external row writer apart from a store that is REFUSING this transition; the row
		// version can, and re-reading it also re-seeds the CAS this run holds by hand
		// (rowAdvanced). Fenced, guarded for the callers that hold no row, and terminal only
		// when NEITHER version moved — see terminalAfterRowReread.
		terminal, terr := terminalAfterRowReread(ctx, wf.Acts, projectID, next != expected)
		if terr != nil {
			return 0, terr
		}
		if terminal {
			return 0, temporal.NewNonRetryableApplicationError(terminalConflictMessage, terminalConflictErrType, err)
		}
		expected = next
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
}
