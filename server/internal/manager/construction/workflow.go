package construction

import (
	"errors"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// This file holds the Workflows struct (the Manager's downstream dependency set),
// the three workflow bodies (the encapsulated ConstructionPhaseWorkflow volatility
// — constructionManager.md §6.3), the signal/query handlers, the workflow-level
// Conflict re-read→re-apply loop (§6.5), and the pump's eligibility helper.
//
// How the two dependency kinds are reached differs by determinism class:
//   - The three Engines (HandOff / Intervention / Review) are PURE, deterministic,
//     called DIRECTLY in-workflow (no Activity wrapper — replay-safe).
//   - The ResourceAccess ports (ProjectState / Pipeline / Artifacts / Workers) are
//     I/O and NON-deterministic; the workflow invokes the Activity methods on this
//     same struct via workflow.ExecuteActivity (activities.go).

// Deps bundles every downstream dependency the constructionManager orchestrates,
// passed to RegisterWorker (worker.go) and held on the Workflows struct. Each
// field is a CONSUMER-DEFINED interface (deps.go): the existing concrete RA types
// are adapted at the composition root; the not-yet-built Engines/RA are unit-tested
// with fakes.
type Deps struct {
	HandOff      HandOffEngine
	Intervention InterventionEngine
	Review       ReviewEngine

	ProjectState ProjectStateAccess
	Pipeline     ConstructionPipelineAccess
	Artifacts    ArtifactAccess
	Workers      WorkerAccess

	// Rail + GitStatus are the OPTIONAL git-forward slice (C-MCN-GIT). When both are
	// non-nil the per-activity spine wraps each activity in a branch→PR→CI→+1→merge
	// lifecycle and mirrors the rail returns onto the per-activity git head-state.
	// When either is nil the git-forward lifecycle is dormant (the live Postgres-store
	// composition that predates the GitStore) and the spine runs unchanged.
	Rail      SourceControlRail
	GitStatus GitActivityStatusAccess

	// Repo resolves the per-project RepoRef the rail verbs address. nil ⇒ the
	// git-forward slice is dormant (no repo to open branches/PRs in). Injected so the
	// repo-resolution policy (deterministic per-project repo name) is swappable
	// without a new RA edge — exactly like NextEligibleActivity.
	Repo func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	// NextEligibleActivity resolves the next eligible construction activity for a
	// project from its head-state (the Manager's own pure selection over the
	// committed network — constructionManager.md §6.3 step 1). It returns
	// (activity, true) when one is eligible, or (_, false) for a quiet tick. It is
	// a Manager-internal pure helper (NOT a downstream edge); injected so the pump's
	// eligibility policy is testable and swappable without a new RA edge.
	NextEligibleActivity func(proj projectstate.Project) (ConstructionActivity, bool)

	// Policy is the project's committed HandOffPolicy / InterventionPolicy snapshot
	// the Manager feeds the Engines by value. In production the Manager reads it
	// from head-state; held here as the construction-time seam value.
	HandOffPolicy      HandOffPolicy
	InterventionPolicy InterventionPolicy

	// EscalationWaitTimeout bounds how long an escalated/ArchitectOnly activity waits
	// for an operator override before it terminally FAILS the activity (head-state
	// reflects EscalationTimedOut instead of hanging forever). 0 == wait-forever
	// (the supervised mode). Default 30m from config.
	EscalationWaitTimeout time.Duration
}

// Workflows is the single constructionManager component struct — BOTH the workflow
// receiver and the activity receiver (no separate Activities type, mirroring
// systemdesign).
type Workflows struct {
	HandOff      HandOffEngine
	Intervention InterventionEngine
	Review       ReviewEngine

	ProjectState ProjectStateAccess
	Pipeline     ConstructionPipelineAccess
	Artifacts    ArtifactAccess
	Workers      WorkerAccess

	Rail      SourceControlRail
	GitStatus GitActivityStatusAccess
	Repo      func(projectID ProjectID) (sourcecontrol.RepoRef, bool)

	NextEligibleActivity  func(proj projectstate.Project) (ConstructionActivity, bool)
	HandOffPolicy         HandOffPolicy
	InterventionPolicy    InterventionPolicy
	EscalationWaitTimeout time.Duration
}

// newWorkflows builds the Workflows receiver from the injected Deps.
func newWorkflows(d Deps) *Workflows {
	return &Workflows{
		HandOff:               d.HandOff,
		Intervention:          d.Intervention,
		Review:                d.Review,
		ProjectState:          d.ProjectState,
		Pipeline:              d.Pipeline,
		Artifacts:             d.Artifacts,
		Workers:               d.Workers,
		Rail:                  d.Rail,
		GitStatus:             d.GitStatus,
		Repo:                  d.Repo,
		NextEligibleActivity:  d.NextEligibleActivity,
		HandOffPolicy:         d.HandOffPolicy,
		InterventionPolicy:    d.InterventionPolicy,
		EscalationWaitTimeout: d.EscalationWaitTimeout,
	}
}

// Bounds + cadences (in-workflow guards; NOT contract surface).
const (
	// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
	// loop (§6.5).
	maxMutateConflictAttempts = 20
	// maxVarianceAttempts bounds the dispatch→review→variance supervision loop
	// before the Engine's Escalate/Takeover must terminate it.
	maxVarianceAttempts = 10
	// pipelinePollInterval is the durable wait between observeConstructionPipeline
	// polls (the Manager's own startTimer cadence; §6.3 step 3).
	pipelinePollInterval = 15 * time.Second
	// maxPipelinePolls bounds the observe loop (a stuck pipeline escalates).
	maxPipelinePolls = 240
	// pumpPaceInterval is the short durable wait between cascade iterations (the pump's
	// self-cascade pacing; Task 3) — a workflow.Sleep, NOT time.Sleep. Keeps the
	// continue-as-new loop from busy-spinning while still draining the network promptly.
	pumpPaceInterval = 1 * time.Second
)

// ---------------------------------------------------------------------------
// Activity option presets (constructionManager.md §6.4). Concrete RetryPolicy /
// timeout choices live here, in the Manager.
// ---------------------------------------------------------------------------

func readProjectOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

func generateWorkerOpts(ctx workflow.Context) workflow.Context {
	// Long StartToClose + small retry budget for the worker round-trip
	// (constructionManager.md §6.4 DispatchWorkerActivity). The
	// unconstructable-response terminal (workerRefusedErrType) is non-retryable.
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.QuotaExhausted),
				fwmanager.RAErrType(fwra.ContractMisuse),
				workerRefusedErrType,
			},
		},
	})
}

func cancelWorkerOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{},
	})
}

func submitPipelineOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

func observePipelineOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.Auth),
			},
		},
	})
}

func storeOutputOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

func recordOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// raConflictErrType is the canonical Temporal Type() a head-state mutation Activity
// surfaces when expectedVersion is stale; the workflow recovers with the bounded
// re-read→re-apply loop (§6.5).
var raConflictErrType = fwmanager.RAErrType(fwra.Conflict)

// raNotFoundErrType is the canonical Temporal Type() ReadProject surfaces for a
// brand-new project (no row yet).
var raNotFoundErrType = fwmanager.RAErrType(fwra.NotFound)

// ===========================================================================
// PumpNextActivityWorkflow — op 2.1 entry (scheduler-triggered, 30s).
// ===========================================================================

// PumpInput is the start payload for PumpNextActivityWorkflow.
type PumpInput struct {
	ProjectID ProjectID
}

func (wf *Workflows) PumpNextActivityWorkflow(ctx workflow.Context, in PumpInput) (PumpResult, error) {
	logger := workflow.GetLogger(ctx)

	// PAUSE GATE (Task 3): the cascade halts the moment a pause Signal is observed on
	// THIS pump execution. The pump listens on the SAME operatorPauseRequested signal
	// channel the project supervision workflow uses; a pause delivered to the cascading
	// pump is observed here (ReceiveAsync — non-blocking, replay-deterministic) and the
	// pump goes quiet WITHOUT ContinueAsNew. The resume path re-triggers the pump (a
	// fresh begin/schedule firing), which starts a new cascade. Checked BEFORE every
	// dispatch so a pause never races a half-dispatched activity. The signal survives
	// ContinueAsNew (same workflow id across the cascade), so a pause sent mid-cascade
	// is honored on the next iteration even if it arrives between ticks.
	pauseCh := workflow.GetSignalChannel(ctx, SignalOperatorPauseRequested)
	var pauseSig OperatorPauseSignal
	if pauseCh.ReceiveAsync(&pauseSig) {
		logger.Info("pump cascade paused by operator signal — going quiet without continue-as-new",
			"projectId", string(in.ProjectID), "reason", pauseSig.Reason)
		return PumpResult{Dispatched: false}, nil
	}

	proj, err := wf.readProject(ctx, in.ProjectID)
	if err != nil {
		if isReadNotFound(err) {
			// No project state yet — a normal quiet tick, not an error.
			return PumpResult{Dispatched: false}, nil
		}
		return PumpResult{}, err
	}

	activity, eligible := wf.nextEligible(proj)
	if !eligible {
		// Network drained (or nothing eligible this tick) ⇒ the cascade ENDS here:
		// return quiet WITHOUT ContinueAsNew so the pump goes dormant (the next
		// begin/schedule firing re-triggers it).
		logger.Info("no eligible activity — cascade quiescent", "projectId", string(in.ProjectID))
		return PumpResult{Dispatched: false}, nil
	}

	// Eligible ⇒ start a per-activity child workflow (idempotent on its id; a
	// redundant tick collapses to the running child). PARENT_CLOSE_POLICY ABANDON:
	// the construction activity is its own durable execution, independent of this
	// pump tick's continue-as-new chain.
	childID := constructActivityWorkflowID(in.ProjectID, ActivityID(activity.ActivityID))
	cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID:        childID,
		ParentClosePolicy: enumspb.PARENT_CLOSE_POLICY_ABANDON,
	})
	child := workflow.ExecuteChildWorkflow(cctx, ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID:  in.ProjectID,
		ActivityID: ActivityID(activity.ActivityID),
		Activity:   activity,
	})
	// SELF-CASCADE (Task 3): wait for the child to COMPLETE (not just start) so the
	// activity's RecordActivityCompleted has landed in head-state before we pick the
	// next eligible activity — otherwise nextEligible would re-select the same
	// still-Running activity. child.Get blocks on the child's terminal result.
	if err := child.Get(ctx, nil); err != nil {
		return PumpResult{}, err
	}

	// Pace the cascade with a short durable wait (workflow.Sleep — replay-safe; NOT
	// time.Sleep), then ContinueAsNew to pick the next eligible activity. ContinueAsNew
	// carries ONLY PumpInput (no accumulated state ⇒ unbounded history is avoided and
	// determinism is trivial). The conflict/quiet-tick semantics keep the prod 30s
	// schedule compatible: a schedule re-fire onto a cascading pump uses the existing
	// USE_EXISTING conflict policy (constructionmanager.go) and the cascade's own
	// drain-to-quiet ends it.
	if err := workflow.Sleep(ctx, pumpPaceInterval); err != nil {
		return PumpResult{}, err
	}
	return PumpResult{}, workflow.NewContinueAsNewError(ctx, ExecutionKindPump, PumpInput{ProjectID: in.ProjectID})
}

// nextEligible resolves the next eligible activity via the injected helper. With no
// helper wired (or no eligible activity) it is a quiet tick.
func (wf *Workflows) nextEligible(proj projectstate.Project) (ConstructionActivity, bool) {
	if wf.NextEligibleActivity == nil {
		return ConstructionActivity{}, false
	}
	return wf.NextEligibleActivity(proj)
}

// ===========================================================================
// ConstructActivityWorkflow — the per-activity UC3 spine (constructionManager.md
// §6.3). Loop/supervise until exited.
// ===========================================================================

// ConstructActivityInput is the start payload for the per-activity child workflow.
type ConstructActivityInput struct {
	ProjectID  ProjectID
	ActivityID ActivityID
	Activity   ConstructionActivity
}

// constructState is the live technical state backing the sessionState Query.
type constructState struct {
	projectID     ProjectID
	activityID    ActivityID
	stage         ConstructionStage
	pipelinePhase *PipelinePhase
	reviewSet     *ReviewSet
	variance      *FlaggedVariance
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

func (wf *Workflows) ConstructActivityWorkflow(ctx workflow.Context, in ConstructActivityInput) error {
	logger := workflow.GetLogger(ctx)

	state := &constructState{projectID: in.ProjectID, activityID: in.ActivityID, stage: StageDispatching}
	if err := workflow.SetQueryHandler(ctx, QuerySessionState, state.view); err != nil {
		return err
	}

	// Operator-override signal channel (constructionManager.md §6.3 override branch).
	overrideCh := workflow.GetSignalChannel(ctx, SignalOperatorOverride)

	// Carry expectedVersion forward (read-your-writes; §6.5).
	headVersion := wf.readVersion(ctx, in.ProjectID)

	// --- Step 0: record the activity STARTED (Task 3) ----------------------------
	// Mint the per-activity credential ONCE (reused by the branch/PR lifecycle below
	// and the completed record at the end) and flip the activity to Running in the
	// per-activity construction head-state BEFORE any dispatch. This is what removes
	// the activity from the pump's NotStarted eligibility set so a concurrent/redundant
	// pump tick does not re-dispatch it. Dormant (no-op) when the git slice is unwired.
	startedCred, gitOn, scErr := wf.startedCred(ctx, in.ProjectID)
	if scErr != nil {
		return scErr
	}
	if gitOn {
		if err := wf.recordActivityStarted(ctx, in, startedCred, &headVersion); err != nil {
			return err
		}
	}

	// git-forward lifecycle state (C-MCN-GIT). Opened lazily on the first non-
	// ArchitectOnly dispatch and carried across supervision-loop iterations (a branch
	// + PR is born once per activity, not per retry). Dormant when the slice is unwired.
	var gf gitForward

	for attempt := 0; ; attempt++ {
		if attempt >= maxVarianceAttempts {
			// Terminal: the supervision loop exhausted its variance/retry budget. Record
			// the FAILURE in head-state (so the activity is no longer stuck Running) before
			// returning the terminal error.
			if v, e := wf.recordActivityFailed(ctx, in, headVersion, projectstate.VarianceExhausted,
				"construction supervision exceeded max attempts"); e != nil {
				return e
			} else {
				headVersion = v
			}
			state.stage = StageExited
			logger.Info("construction activity failed — variance budget exhausted", "activityId", in.ActivityID)
			return nil
		}

		// --- Step 1: cast worker class (DECIDE — direct in-workflow Engine call) --
		class, herr := wf.HandOff.PickWorkerClass(in.Activity, wf.HandOffPolicy)
		if herr != nil {
			return fwmanager.MapError(herr)
		}

		// ArchitectOnly ⇒ skip dispatch + pipeline; await the architect via override
		// (handOffEngine OQ-2). The architect's steer arrives on operatorOverride, BOUNDED
		// by EscalationWaitTimeout: if no architect override arrives within the window the
		// activity terminally FAILS (EscalationTimedOut) instead of hanging forever.
		if class == ArchitectOnly {
			state.stage = StageAwaitingTakeover
			sig, got := wf.awaitOverrideBounded(ctx, overrideCh)
			if !got {
				v, e := wf.recordActivityFailed(ctx, in, headVersion, projectstate.EscalationTimedOut,
					"architect override timed out: no operator steer within the escalation-wait window")
				if e != nil {
					return e
				}
				headVersion = v
				state.stage = StageExited
				return nil
			}
			done, exErr := wf.executeOverride(ctx, in, sig.Override, &headVersion, state, gitOn, startedCred)
			if exErr != nil {
				return exErr
			}
			if done {
				return nil
			}
			continue
		}

		// --- Step 2a: open the per-activity branch + PR and mirror it (git-forward,
		// C-MCN-GIT). Lazy + once: the row is born on the first dispatch and reused on
		// retries. Dormant (no-op) when the git slice is unwired. ----------------------
		if gf.enabled {
			// already opened on a prior loop iteration — reuse the handles.
		} else if opened, oerr := wf.openActivityBranchAndPR(ctx, in, startedCred, &headVersion); oerr != nil {
			return oerr
		} else {
			gf = opened
		}

		// --- Steps 2-5: walk the App-A service life cycle, dispatching ONE GH-Actions
		// job per phase (Requirements → Detailed Design → Test Plan → Construction →
		// Integration; aiarch-phase.yml, a distinct prompt per phase). Each phase's
		// pipeline observe rides the CI poll cadence (observeCIAndRecord). A phase whose
		// pipeline fails routes to intervention (App-A: a failing review repeats the
		// preceding task), then the activity retries from the first phase. This replaces
		// the legacy single-shot dispatchWork→runPipeline→storeOutput→routeReview
		// (the server-LLM path; dispatchWork/storeOutput/routeReview now dead — Plan 3
		// worker-RA deletion is the follow-up). --------------------------------------
		phaseFailed := false
		for _, phase := range servicePhases {
			state.stage = StagePipelineRunning
			obs, perr := wf.runPipeline(ctx, in, phase, state, &gf, &headVersion)
			if perr != nil {
				return perr
			}
			if obs.Phase == PipelineFailed || obs.Phase == PipelineCancelled {
				failReason := deriveFailureReason(obs.Phase, obs.Diagnostic)
				done, vErr := wf.handleVariance(ctx, in, VariancePipelineFailed, obs.Diagnostic, failReason, attempt, &headVersion, state, overrideCh, gitOn, startedCred)
				if vErr != nil {
					return vErr
				}
				if done {
					return nil
				}
				phaseFailed = true
				break
			}
		}
		if phaseFailed {
			continue // retry the activity from the first phase
		}

		// --- Step 5a: relay the architecture +1 and record it (git-forward) ------
		// The activity's technical review passed; relay the architect's in-app
		// architecture sign-off onto the PR and record the audit-worthy ArchApproved
		// fact. Dormant when the git slice is unwired.
		if err := wf.relayArchApprovalAndRecord(ctx, in, &gf, &headVersion); err != nil {
			return err
		}

		// --- Step 6: record the change reviewed (head-state) --------------------
		if v, e := wf.recordChangeReviewed(ctx, in, headVersion); e != nil {
			return e
		} else {
			headVersion = v
		}

		// --- Step 6a: perform the gated merge and record it (git-forward) --------
		// interventionEngine is the merge AUTHORITY (App-only-merge): the change is
		// reviewed + arch-approved + CI-clean, so the gate is cleared and the Manager
		// PERFORMS the merge to main, then records the terminal git fact. Dormant when
		// the git slice is unwired.
		if err := wf.mergeAndRecord(ctx, in, &gf, &headVersion); err != nil {
			return err
		}

		// --- Step 8: record the binary activity exit (head-state) ---------------
		if v, e := wf.recordActivityExited(ctx, in, headVersion, projectstate.ActivityOutcomeCompleted); e != nil {
			return e
		} else {
			headVersion = v
		}

		// --- Step 8a: record the per-activity construction COMPLETED (Task 3) ---
		// Flip the activity to Done so the pump's eligibility selection unblocks its
		// dependents on the next tick. Dormant (no-op) when the git slice is unwired.
		if gitOn {
			if err := wf.recordActivityCompleted(ctx, in, startedCred, &headVersion); err != nil {
				return err
			}
		}

		state.stage = StageExited
		logger.Info("construction activity exited", "activityId", in.ActivityID)
		return nil
	}
}

// dispatchWork runs the worker dispatch Activity for the cast class.
func (wf *Workflows) dispatchWork(ctx workflow.Context, class WorkerClass, activity ConstructionActivity) (artifact.ConstructionOutput, error) {
	c := generateWorkerOpts(ctx)
	var out artifact.ConstructionOutput
	err := workflow.ExecuteActivity(c, wf.GenerateWorkActivity, GenerateWorkArgs{
		WorkerClass: class.String(),
		Prompt:      constructionPrompt(activity, class),
	}).Get(ctx, &out)
	return out, err
}

// servicePhases is the App-A service development life cycle (Appendix-A Figure A-1 /
// Table A-1) in order: Requirements → Detailed Design → Test Plan → Construction →
// Integration. The Manager dispatches one GH-Actions job per phase; the per-phase
// weights (15/20/10/40/15) live in the store's phaseSetFor (seeded on RecordPhaseStarted).
var servicePhases = []projectstate.ActivityMethodPhase{
	projectstate.MethodPhaseRequirements,
	projectstate.MethodPhaseDetailedDesign,
	projectstate.MethodPhaseTestPlan,
	projectstate.MethodPhaseConstruction,
	projectstate.MethodPhaseIntegration,
}

// runPipeline submits the pipeline then polls observe between durable startTimer
// waits until the pipeline reaches a terminal phase (§6.3 step 3). On each observe it
// ALSO reads the PR's CI rollup and mirrors it onto the head-state (the git-forward
// poll-loop verb, C-MCN-GIT) — dormant when the git slice is unwired.
func (wf *Workflows) runPipeline(ctx workflow.Context, in ConstructActivityInput, phase projectstate.ActivityMethodPhase, state *constructState, gf *gitForward, headVersion *projectstate.Version) (PipelineObservation, error) {
	sc := submitPipelineOpts(ctx)
	var handle PipelineHandle
	if err := workflow.ExecuteActivity(sc, wf.SubmitPipelineActivity, PipelineSpec{
		ActivityID:  string(in.ActivityID),
		ComponentID: in.Activity.ComponentID,
		Phase:       phase.String(),
	}).Get(ctx, &handle); err != nil {
		return PipelineObservation{}, err
	}

	oc := observePipelineOpts(ctx)
	for poll := 0; poll < maxPipelinePolls; poll++ {
		var obs PipelineObservation
		if err := workflow.ExecuteActivity(oc, wf.ObservePipelineActivity, handle).Get(ctx, &obs); err != nil {
			return PipelineObservation{}, err
		}
		ph := obs.Phase
		state.pipelinePhase = &ph

		// Mirror the PR's CI rollup onto the head-state on the same cadence.
		if _, cerr := wf.observeCIAndRecord(ctx, in, gf, headVersion); cerr != nil {
			return PipelineObservation{}, cerr
		}

		if obs.Phase == PipelineSucceeded || obs.Phase == PipelineFailed {
			return obs, nil
		}
		// Durable wait between polls (the Manager's own startTimer — category A).
		_ = workflow.Sleep(ctx, pipelinePollInterval)
	}
	return PipelineObservation{Phase: PipelineFailed, Diagnostic: "pipeline did not reach a terminal phase within the poll budget"}, nil
}

// storeOutput stages the construction output via artifactAccess (the content
// address is a plain string — artifactAccess.md §2).
func (wf *Workflows) storeOutput(ctx workflow.Context, output artifact.ConstructionOutput) (string, error) {
	c := storeOutputOpts(ctx)
	var addr string
	err := workflow.ExecuteActivity(c, wf.StoreConstructionOutputActivity, output).Get(ctx, &addr)
	return addr, err
}

// routeReview computes the reviewer set (DECIDE — direct in-workflow Engine call),
// fans out one worker dispatch per reviewer, and gates on the verdicts. On a
// mayAmend verdict it re-stages the amended output. Returns reviewOK.
func (wf *Workflows) routeReview(ctx workflow.Context, in ConstructActivityInput, state *constructState) (bool, error) {
	set, rerr := wf.Review.ProposeReviews(
		ReviewChange{ActivityID: string(in.ActivityID), ComponentID: in.Activity.ComponentID},
		in.Activity.ComponentID,
		in.Activity.Kind.String(),
		"", // architectureGraph — Manager-supplied snapshot (seam)
		nil,
	)
	if rerr != nil {
		return false, fwmanager.MapError(rerr)
	}
	state.reviewSet = &set

	c := generateWorkerOpts(ctx)
	for _, reviewer := range set.Reviewers {
		var out artifact.ConstructionOutput
		if err := workflow.ExecuteActivity(c, wf.GenerateWorkActivity, GenerateWorkArgs{
			WorkerClass: reviewer.Role,
			Prompt:      reviewPrompt(in.Activity, reviewer),
		}).Get(ctx, &out); err != nil {
			// A reviewer dispatch that cannot produce a verdict surfaces a failed,
			// unresolvable review verdict (routed to intervention by the caller).
			if isWorkerRefused(err) {
				return false, nil
			}
			return false, err
		}
		// On a mayAmend reviewer, re-stage the amended output (§6.3 step 5).
		if reviewer.MayAmend {
			if _, err := wf.storeOutput(ctx, out); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

// handleVariance is the DECIDE→EXECUTE machinery for an automatically-detected
// variance (constructionManager.md §6.3 step 7). It calls interventionEngine
// (DECIDE) and EXECUTES the directive: Retry → loop again (return done=false);
// Escalate → await an operator override and execute it; Takeover → re-dispatch
// (loop). Returns done=true when the activity has reached a terminal exit.
func (wf *Workflows) handleVariance(
	ctx workflow.Context,
	in ConstructActivityInput,
	kind VarianceKind,
	detail string,
	failReason projectstate.FailureReason,
	attempt int,
	headVersion *projectstate.Version,
	state *constructState,
	overrideCh workflow.ReceiveChannel,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, error) {
	state.variance = &FlaggedVariance{ProjectID: in.ProjectID, ActivityID: in.ActivityID, Summary: detail}

	directive, derr := wf.Intervention.DecideOnVariance(ConstructionVariance{
		ActivityID:   string(in.ActivityID),
		Kind:         kind,
		Detail:       detail,
		AttemptCount: attempt,
	})
	if derr != nil {
		return false, fwmanager.MapError(derr)
	}

	switch directive {
	case DirectiveRetry:
		state.stage = StageDispatching
		return false, nil // loop to re-dispatch
	case DirectiveTakeover:
		// EXECUTE takeover: abandon the in-flight worker, then loop to re-dispatch
		// under a changed arrangement.
		if err := wf.cancelWorker(ctx); err != nil {
			return false, err
		}
		state.stage = StageDispatching
		return false, nil
	case DirectiveEscalate:
		// EXECUTE escalate: surface to the operator + await an override signal, BOUNDED
		// by EscalationWaitTimeout. On timeout (no operator answered the escalation), the
		// activity terminally FAILS (head-state reflects EscalationTimedOut) instead of
		// hanging forever waiting for an override that never comes.
		state.stage = StageAwaitingTakeover
		sig, got := wf.awaitOverrideBounded(ctx, overrideCh)
		if !got {
			_ = failReason // underlying cause is carried in detail below; the terminal reason is EscalationTimedOut
			v, e := wf.recordActivityFailed(ctx, in, *headVersion, projectstate.EscalationTimedOut,
				"escalation timed out: no operator override within the escalation-wait window (underlying: "+detail+")")
			if e != nil {
				return false, e
			}
			*headVersion = v
			state.stage = StageExited
			return true, nil
		}
		return wf.executeOverride(ctx, in, sig.Override, headVersion, state, gitOn, startedCred)
	default:
		return false, temporal.NewNonRetryableApplicationError(
			"intervention returned an unknown directive", "UnknownDirective", nil)
	}
}

// executeOverride runs the operator's manual steer through the same execute
// machinery as the automatic variance path (constructionManager.md §2.4 / §6.3
// override branch). Returns done=true when the override terminally exits the
// activity (Skip), false when it loops back into supervision (Retry/Takeover/Reassign).
func (wf *Workflows) executeOverride(
	ctx workflow.Context,
	in ConstructActivityInput,
	override ActivityOverride,
	headVersion *projectstate.Version,
	state *constructState,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, error) {
	switch override.Kind {
	case OverrideRetry, OverrideReassign:
		// Re-enter the dispatch path (Reassign re-casts via handOffEngine on the
		// next loop iteration — the committed constructionManager → handOffEngine
		// edge, OQ-3).
		state.stage = StageDispatching
		return false, nil
	case OverrideTakeover:
		if err := wf.cancelWorker(ctx); err != nil {
			return false, err
		}
		state.stage = StageDispatching
		return false, nil
	case OverrideSkip:
		v, e := wf.recordActivityExited(ctx, in, *headVersion, projectstate.ActivityOutcomeSkipped)
		if e != nil {
			return false, e
		}
		*headVersion = v
		// Record the per-activity construction COMPLETED on a Skip terminal too
		// (Task 3): a skipped activity is Done from the pump's eligibility POV so its
		// dependents unblock. Dormant when the git slice is unwired.
		if gitOn {
			if err := wf.recordActivityCompleted(ctx, in, startedCred, headVersion); err != nil {
				return false, err
			}
		}
		state.stage = StageExited
		return true, nil
	default:
		return false, temporal.NewNonRetryableApplicationError(
			"unknown operator override kind", "UnknownOverride", nil)
	}
}

// deriveFailureReason maps a terminal pipeline phase + neutral diagnostic to the
// head-state FailureReason: a cancelled run → PipelineCancelled; a timed-out
// diagnostic (the RA's neutralDiagnostic for timed_out / the poll-budget exhaustion
// synthetic) → PipelineTimedOut; otherwise PipelineFailed.
func deriveFailureReason(phase PipelinePhase, diagnostic string) projectstate.FailureReason {
	if phase == PipelineCancelled {
		return projectstate.PipelineCancelled
	}
	if strings.Contains(diagnostic, "timed out") || strings.Contains(diagnostic, "did not reach a terminal phase") {
		return projectstate.PipelineTimedOut
	}
	return projectstate.PipelineFailed
}

// awaitOverrideBounded waits for an operator override on overrideCh, BOUNDED by the
// configured EscalationWaitTimeout. It returns (sig, true) when an override arrived,
// or (zero, false) when the bounded wait elapsed first. A timeout of 0 means
// wait-forever (the supervised EscalateEverything mode) — it blocks on the receive
// with no timer, preserving the legacy behaviour. The timer is a durable
// workflow.NewTimer (replay-safe), raced via a workflow.NewSelector.
func (wf *Workflows) awaitOverrideBounded(ctx workflow.Context, overrideCh workflow.ReceiveChannel) (OperatorOverrideSignal, bool) {
	var sig OperatorOverrideSignal
	if wf.EscalationWaitTimeout <= 0 {
		// Supervised / wait-forever: block on the override receive (legacy behaviour).
		overrideCh.Receive(ctx, &sig)
		return sig, true
	}
	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	defer cancelTimer()
	timer := workflow.NewTimer(timerCtx, wf.EscalationWaitTimeout)
	got := false
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(overrideCh, func(ch workflow.ReceiveChannel, _ bool) {
		ch.Receive(ctx, &sig)
		got = true
	})
	sel.AddFuture(timer, func(workflow.Future) {
		got = false
	})
	sel.Select(ctx)
	return sig, got
}

// cancelWorker runs the worker-abandon Activity (the takeover / operator-pause
// path — workerAccess.Cancel).
func (wf *Workflows) cancelWorker(ctx workflow.Context) error {
	c := cancelWorkerOpts(ctx)
	return workflow.ExecuteActivity(c, wf.CancelWorkerActivity, struct{}{}).Get(ctx, nil)
}

// ===========================================================================
// ReplanSweepWorkflow — op 2.2 (scheduler-triggered, 5m). Flags over-threshold
// variances; NO dispatch, NO auto-replan.
// ===========================================================================

// ReplanSweepInput is the start payload for ReplanSweepWorkflow.
type ReplanSweepInput struct {
	ProjectID *ProjectID // nil ⇒ sweep all in-flight projects
}

func (wf *Workflows) ReplanSweepWorkflow(ctx workflow.Context, in ReplanSweepInput) (ReplanSweepResult, error) {
	// v1: the sweep reads the named project's head-state (the all-projects sweep is
	// a future fan-out — constructionManager.md §2.2). It surfaces over-threshold
	// variances; it never dispatches and never auto-replans. With no project named
	// (the all-sweep) it returns an empty (quiet) result — the per-project fan-out
	// is the documented follow-up, not a new façade op.
	if in.ProjectID == nil {
		return ReplanSweepResult{}, nil
	}

	proj, err := wf.readProject(ctx, *in.ProjectID)
	if err != nil {
		if isReadNotFound(err) {
			return ReplanSweepResult{}, nil
		}
		return ReplanSweepResult{}, err
	}

	flagged := wf.flagVariances(proj)
	return ReplanSweepResult{FlaggedVariances: flagged}, nil
}

// flagVariances surfaces over-threshold variances for the project. v1 surfaces an
// empty set unless an eligibility/variance helper is wired (the head-state
// variance-aggregate fill is the D-PA follow-up); the sweep's role is to SURFACE,
// never to auto-replan.
func (wf *Workflows) flagVariances(_ projectstate.Project) []FlaggedVariance {
	return nil
}

// ---------------------------------------------------------------------------
// Head-state read + recovering write helpers (§6.5).
// ---------------------------------------------------------------------------

// readProject runs the ReadProjectActivity and returns the projected head-state.
func (wf *Workflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
	c := readProjectOpts(ctx)
	var pe projectEnvelope
	// Convert the Manager's OWN ProjectID to projectStateAccess's at the RA boundary.
	if err := workflow.ExecuteActivity(c, wf.ReadProjectActivity, projectstate.ProjectID(projectID)).Get(ctx, &pe); err != nil {
		return projectstate.Project{}, err
	}
	return decodeProject(pe), nil
}

// readVersion reads the current head Version (0 for a brand-new project).
func (wf *Workflows) readVersion(ctx workflow.Context, projectID ProjectID) projectstate.Version {
	p, err := wf.readProject(ctx, projectID)
	if err != nil {
		return 0
	}
	return p.Version
}

// recordChangeReviewed applies the head-state transition with the Conflict loop.
func (wf *Workflows) recordChangeReviewed(ctx workflow.Context, in ConstructActivityInput, seed projectstate.Version) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		c := recordOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.RecordChangeReviewedActivity, RecordChangeReviewedArgs{
			ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, ActivityID: string(in.ActivityID),
		}).Get(ctx, &v)
		return v, e
	})
}

// recordActivityExited applies the binary-exit head-state transition.
func (wf *Workflows) recordActivityExited(ctx workflow.Context, in ConstructActivityInput, seed projectstate.Version, outcome projectstate.ActivityOutcome) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		c := recordOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.RecordActivityExitedActivity, RecordActivityExitedArgs{
			ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, ActivityID: string(in.ActivityID), Outcome: outcome,
		}).Get(ctx, &v)
		return v, e
	})
}

// recordActivityFailed applies the terminal-FAILURE head-state transition (the
// bounded-wait / autonomous-retry fix) with the same head-version Conflict re-read
// loop as recordActivityExited. It lands Phase=Failed / BuildStatus=BuildFailed and
// records the reason+detail so head-state reflects the terminal instead of leaving
// the activity stuck Running.
func (wf *Workflows) recordActivityFailed(ctx workflow.Context, in ConstructActivityInput, seed projectstate.Version, reason projectstate.FailureReason, detail string) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		c := recordOpts(ctx)
		var v projectstate.Version
		e := workflow.ExecuteActivity(c, wf.RecordActivityFailedActivity, RecordActivityFailedArgs{
			ProjectID: projectstate.ProjectID(in.ProjectID), ExpectedVersion: expected, ActivityID: string(in.ActivityID), Reason: reason, Detail: detail,
		}).Get(ctx, &v)
		return v, e
	})
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to systemdesign).
func (wf *Workflows) applyRecovering(
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
		p, rerr := wf.readProject(ctx, projectID)
		if rerr != nil {
			if isReadNotFound(rerr) {
				expected = 0
				continue
			}
			return 0, rerr
		}
		expected = p.Version
		workflow.GetLogger(ctx).Info("head-state conflict; re-read version and retrying",
			"attempt", attempt+1, "nextExpectedVersion", expected)
	}
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

// isWorkerRefused reports whether err is the unconstructable-response terminal
// surfaced by GenerateWorkActivity (worker ran but produced a non-ConstructionOutput).
func isWorkerRefused(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == workerRefusedErrType
	}
	return false
}
