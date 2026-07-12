package construction

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// This file holds the workflows struct (the Manager's downstream dependency set),
// the three workflow bodies (the encapsulated ConstructionPhaseWorkflow volatility
// — constructionManager.md §6.3), the signal/query handlers, the workflow-level
// Conflict re-read→re-apply loop (§6.5), and the pump's eligibility helper.
//
// How the two dependency kinds are reached differs by determinism class:
//   - The three Engines (handoff.HandOffEngine / intervention.InterventionEngine /
//     review.ReviewEngine — the PUBLISHED contracts, no Manager-local seam) are PURE,
//     deterministic, called DIRECTLY in-workflow (no Activity wrapper — replay-safe),
//     with fweng.Context{Context: context.Background()} supplied inline at each call site.
//   - The ResourceAccess ports are I/O and NON-deterministic. Almost all of them
//     (pipeline / artifact / rail / projectState-version / constructionTransition /
//     gitActivityStatus) are GENERATED and reached through the generated invoker surface
//     (Acts) — B8 migrated the constructionTransition + git head-state writes off their
//     former hand-written Activity wrappers. The ONE surviving CUSTOM op
//     (ReadProjectActivity, the projectEnvelope-codec whole-aggregate read) is an
//     Activity method on this same struct, invoked via workflow.ExecuteActivity
//     (activities_custom.go) — kept custom because the generated
//     designSessionAccess.readProjectOnBranch invoker returns a structurally narrower
//     envelope that cannot carry what the pump's eligibility selection needs (deps.go).

// wfDeps bundles every downstream dependency the constructionManager orchestrates,
// assembled by WorkerManifest (workermanifest.go) from the Manager's stored PUBLISHED
// deps and held on the workflows struct. The three Engines are typed as their
// PUBLISHED contract interfaces (no Manager-local seam), called DIRECTLY in-workflow.
// The ResourceAccess layer is reached through the surviving RA seams (deps.go) or the
// generated invoker surface; the unit tests inject in-package fakes. It is a
// package-internal builder input.
type wfDeps struct {
	HandOff      handoff.HandOffEngine
	Intervention intervention.InterventionEngine
	Review       review.ReviewEngine

	// ProjectState serves the whole-aggregate read (CUSTOM projectEnvelope codec,
	// ReadProjectActivity — see deps.go for why this ONE op stays custom post-B8). The
	// cred-threaded Phase-3 head-state transition writes are reached through the
	// GENERATED invoker surface (Acts.ConstructionTransition*) — there is no
	// wfDeps.ConstructionTransition field anymore (B8).
	ProjectState projectStateReader

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

// workflows is the single constructionManager component struct — BOTH the workflow
// receiver and the CUSTOM-activity receiver (mirroring systemdesign). The contract-backed
// RA ops are reached through the generated invoker surface (Acts).
type workflows struct {
	HandOff      handoff.HandOffEngine
	Intervention intervention.InterventionEngine
	Review       review.ReviewEngine

	ProjectState projectStateReader
	GitStatus    projectstate.GitActivityStatusAccess

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
		ProjectState:          d.ProjectState,
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

// Bounds + cadences (in-workflow guards; NOT contract surface).
const (
	// maxMutateConflictAttempts bounds the workflow-level Conflict re-read→re-apply
	// loop (§6.5).
	maxMutateConflictAttempts = 20
	// maxVarianceAttempts bounds the dispatch→review→variance supervision loop
	// before the Engine's Escalate/Takeover must terminate it.
	maxVarianceAttempts = 10
	// maxPhaseRedrafts bounds a gated phase's human-paced SendBack redraft budget —
	// SEPARATE from maxVarianceAttempts. SendBack is NOT a variance: it redrafts THIS
	// phase in place; on exhaustion the gate keeps awaiting the human (it never
	// re-enters the variance loop or fails the activity).
	maxPhaseRedrafts = 5
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

// readProjectActivityOptions is the read preset VALUE (10s; NotFound+ContractMisuse
// terminal) shared by: (a) the CUSTOM ReadProjectActivity, applied directly at its
// workflow.ExecuteActivity call site via readProjectOpts, and (b) the migrated (B8)
// ReadProjectVersion GENERATED invoker, applied via the manifest's Opts hook
// (workermanifest.go, keyed "projectStateAccess.readProjectVersion") — reproducing the
// identical pre-migration preset for both.
func readProjectActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	}
}

func readProjectOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, readProjectActivityOptions())
}

// submitPipelineActivityOptions / observePipelineActivityOptions are the pipeline preset
// VALUES the manifest's Opts hook (workermanifest.go) applies to the GENERATED pipeline
// invokers by registered name — reproducing the pre-migration per-call-site presets
// exactly (submit 60s Auth/ContractMisuse-terminal; observe/cancel 30s NotFound/Auth-terminal).
func submitPipelineActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 60 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	}
}

func observePipelineActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.Auth),
			},
		},
	}
}

// pipelineDefaultToolchain is the single logical build step the Manager's neutral
// pipelineSpec implies (the image map resolves it to a concrete image).
const pipelineDefaultToolchain = "go-1.23"

// dispatchInputsFor builds the DispatchInputs bag for a construction pipeline dispatch.
// The `command` input is the thin slash-command the workflow runs; it is computed here
// from the activity's derived type/variant and the current phase so the workflow itself
// holds no routing logic. component_id is a Manager-resolved passthrough. (Moved workflow-
// side from the retired pipelineAdapter — it only reads workflow state + projectstate.CommandFor.)
func dispatchInputsFor(spec pipelineSpec) map[string]string {
	m := map[string]string{
		"activity_id":  spec.ActivityID,
		"component_id": spec.ComponentID,
	}
	if spec.Phase != "" {
		m["phase"] = spec.Phase
		typ := projectstate.DeriveType(spec.ActivityID)
		variant := projectstate.DeriveVariant(spec.ActivityID)
		m["command"] = projectstate.CommandFor(typ, variant, projectstate.ActivityMethodPhase(spec.Phase))
	}
	if spec.Role != "" {
		m["role"] = spec.Role
	}
	return m
}

// managerPipelinePhase maps the contract PipelinePhase onto the Manager-neutral
// PipelinePhase (mapped here so a future re-order is safe). Moved workflow-side from the
// retired pipelineAdapter.
func managerPipelinePhase(p constructionpipeline.PipelinePhase) PipelinePhase {
	switch p {
	case constructionpipeline.PhasePending:
		return PipelinePending
	case constructionpipeline.PhaseRunning:
		return PipelineRunning
	case constructionpipeline.PhaseSucceeded:
		return PipelineSucceeded
	case constructionpipeline.PhaseFailed:
		return PipelineFailed
	case constructionpipeline.PhaseCancelled:
		return PipelineCancelled
	default:
		return PipelinePhaseUnknown
	}
}

// submitPipeline composes the contract PipelineSpec (default toolchain / single build step
// / workspaceRef / dispatch inputs) from the Manager's neutral pipelineSpec and calls the
// GENERATED submit invoker, mapping the opaque handle back to the neutral pipelineHandle.
func (wf *workflows) submitPipeline(ctx workflow.Context, spec pipelineSpec) (pipelineHandle, error) {
	handle, err := wf.Acts.PipelineSubmitConstructionPipeline(ctx, constructionpipeline.PipelineSpec{
		ActivityID: constructionpipeline.ConstructionActivityID(spec.ActivityID),
		Steps: []constructionpipeline.PipelineStep{{
			Name:      "build",
			Toolchain: constructionpipeline.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		WorkspaceRef:   constructionpipeline.ArtifactRef(spec.RepoURL + "@" + spec.Ref),
		DispatchInputs: dispatchInputsFor(spec),
	})
	if err != nil {
		return pipelineHandle{}, err
	}
	return pipelineHandle{Name: constructionpipeline.PipelineHandleString(handle)}, nil
}

// observePipeline calls the GENERATED observe invoker and maps the contract observation
// back to the Manager-neutral pipelineObservation.
func (wf *workflows) observePipeline(ctx workflow.Context, handle pipelineHandle) (pipelineObservation, error) {
	obs, err := wf.Acts.PipelineObserveConstructionPipeline(ctx, constructionpipeline.ParsePipelineHandle(handle.Name))
	if err != nil {
		return pipelineObservation{}, err
	}
	return pipelineObservation{Phase: managerPipelinePhase(obs.Phase), Diagnostic: obs.Diagnostic}, nil
}

// cancelPipeline calls the GENERATED cancel invoker (idempotent-on-intent in the RA).
func (wf *workflows) cancelPipeline(ctx workflow.Context, handle pipelineHandle) error {
	return wf.Acts.PipelineCancelConstructionPipeline(ctx, constructionpipeline.ParsePipelineHandle(handle.Name))
}

// recordActivityOptions is the head-state Record-verb preset VALUE (10s; ContractMisuse
// terminal only — Conflict must reach the workflow so the §6.5 re-read→re-apply loop can
// recover it) the manifest's Opts hook (workermanifest.go) applies to the GENERATED
// constructionTransitionAccess / gitActivityStatusAccess Record* invokers by registered
// name — reproducing the pre-migration (B8) recordOpts preset exactly. Unlike
// readProjectActivityOptions, there is no remaining direct-ExecuteActivity call site for
// this preset (every Record* verb now goes through the generated invoker surface), so
// only the VALUE form survives.
func recordActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	}
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

	activity, eligible := wf.nextEligible(proj)
	if !eligible {
		// Network drained (or nothing eligible this tick) ⇒ the cascade ENDS here:
		// return quiet WITHOUT ContinueAsNew so the pump goes dormant (the next
		// begin/schedule firing re-triggers it).
		logger.Info("no eligible activity — cascade quiescent", "projectId", string(in.ProjectID))
		dispatch = pumpDispatch{Decided: true, Dispatched: false}
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

// nextEligible resolves the next eligible activity via the injected helper. With no
// helper wired (or no eligible activity) it is a quiet tick.
func (wf *workflows) nextEligible(proj projectstate.Project) (constructionActivity, bool) {
	if wf.NextEligibleActivity == nil {
		return constructionActivity{}, false
	}
	return wf.NextEligibleActivity(proj)
}

// ===========================================================================
// ConstructActivityWorkflow — the per-activity UC3 spine (constructionManager.md
// §6.3). Loop/supervise until exited.
// ===========================================================================

// constructActivityInput is the start payload for the per-activity child workflow.
type constructActivityInput struct {
	ProjectID  ProjectID
	ActivityID ActivityID
	Activity   constructionActivity
}

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

func (wf *workflows) ConstructActivityWorkflow(ctx workflow.Context, in constructActivityInput) error {
	state := &constructState{
		projectID:       in.ProjectID,
		activityID:      in.ActivityID,
		stage:           StageDispatching,
		completedPhases: map[projectstate.ActivityMethodPhase]bool{},
	}
	if err := workflow.SetQueryHandler(ctx, querySessionState, state.view); err != nil {
		return err
	}

	// Operator-override signal channel (constructionManager.md §6.3 override branch).
	overrideCh := workflow.GetSignalChannel(ctx, signalOperatorOverride)

	// Per-execution start snapshot (B5): capture the committed ReviewPolicy, seed the
	// completedPhases skip-guard, and capture the contract keys — replay-guarded.
	reviewPolicy, err := wf.loadReviewSnapshot(ctx, in, state)
	if err != nil {
		return err
	}

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
	// architectOnly dispatch and carried across supervision-loop iterations (a branch
	// + PR is born once per activity, not per retry). Dormant when the slice is unwired.
	var gf gitForward

	// Supervision loop: each attempt runs the UC3 spine once (constructionManager.md
	// §6.3). runAttempt reports back whether the activity terminally exited (attemptDone,
	// the workflow returns) or the loop should try again (attemptRetry).
	for attempt := 0; ; attempt++ {
		ctrl, err := wf.runAttempt(ctx, in, attempt, reviewPolicy, state, &gf, &headVersion, overrideCh, gitOn, startedCred)
		if err != nil {
			return err
		}
		if ctrl == attemptDone {
			return nil
		}
	}
}

// ---------------------------------------------------------------------------
// ConstructActivityWorkflow attempt helpers (mechanical decomposition of the UC3
// spine; NO change to the ORDER of workflow commands). Each helper runs its
// activities/timers/signals in the same sequence the inline loop did.
// ---------------------------------------------------------------------------

// attemptControl is the loop-control verb runAttempt hands back to the supervision loop.
type attemptControl int

const (
	// attemptRetry: re-enter the supervision loop for another attempt.
	attemptRetry attemptControl = iota
	// attemptDone: the activity reached a terminal exit; return from the workflow.
	attemptDone
)

// runAttempt executes ONE supervision attempt of the per-activity UC3 spine: guard the
// variance budget, cast the worker class, handle the architectOnly / dispatch paths, walk
// the phase profile, and (on a clean pass) finalize the activity. It returns attemptDone
// when the activity has terminally exited or attemptRetry when the supervision loop should
// try again. The ORDER of workflow commands is identical to the former inline loop body.
func (wf *workflows) runAttempt(
	ctx workflow.Context,
	in constructActivityInput,
	attempt int,
	reviewPolicy projectstate.ReviewPolicy,
	state *constructState,
	gf *gitForward,
	headVersion *projectstate.Version,
	overrideCh workflow.ReceiveChannel,
	gitOn bool,
	startedCred railCredEnvelope,
) (attemptControl, error) {
	if attempt >= maxVarianceAttempts {
		// Terminal: the supervision loop exhausted its variance/retry budget. Record the
		// FAILURE in head-state (so the activity is no longer stuck Running) before exit.
		return attemptDone, wf.failVarianceExhausted(ctx, in, headVersion, state, startedCred)
	}

	// --- Step 1: cast worker class (DECIDE — direct in-workflow Engine call) --
	class, herr := wf.HandOff.PickWorkerClass(fweng.Context{Context: context.Background()},
		handoffActivityFromConstruction(in.Activity), wf.HandOffPolicy)
	if herr != nil {
		return attemptDone, fwmanager.MapError(herr)
	}

	// architectOnly ⇒ skip dispatch + pipeline; await the architect via override
	// (handOffEngine OQ-2). The architect's steer arrives on operatorOverride, BOUNDED
	// by EscalationWaitTimeout: if no architect override arrives within the window the
	// activity terminally FAILS (EscalationTimedOut) instead of hanging forever.
	if class == handoff.ArchitectOnly {
		done, err := wf.runArchitectOnly(ctx, in, overrideCh, headVersion, state, gitOn, startedCred)
		if err != nil {
			return attemptDone, err
		}
		if done {
			return attemptDone, nil
		}
		return attemptRetry, nil
	}

	// --- Step 2a: open the per-activity branch + PR and mirror it (git-forward,
	// C-MCN-GIT). Lazy + once: the row is born on the first dispatch and reused on
	// retries. Dormant (no-op) when the git slice is unwired. ----------------------
	if !gf.enabled {
		opened, oerr := wf.openActivityBranchAndPR(ctx, in, startedCred, headVersion)
		if oerr != nil {
			return attemptDone, oerr
		}
		*gf = opened
	}

	// --- Steps 2-5: walk the activity's profile phases, dispatching ONE GH-Actions
	// job per phase (the phase sequence is determined by the activity's resolved
	// profile — e.g. service: Requirements → Detailed Design → Test Plan →
	// Construction → Integration; testing-plan: Requirements → Test Plan →
	// Construction). A phase whose pipeline fails routes to intervention (App-A: a
	// failing review repeats the preceding task), then the activity retries from the
	// first phase. --------------------------------------------------------------
	if len(in.Activity.Phases) == 0 {
		in.Activity.Phases = projectstate.ProfileFor(projectstate.ActivityTypeService, 0).PhaseIDs()
	}
	phaseFailed, done, err := wf.walkPhases(ctx, in, attempt, reviewPolicy, state, gf, headVersion, overrideCh, gitOn, startedCred)
	if err != nil {
		return attemptDone, err
	}
	if done {
		return attemptDone, nil
	}
	if phaseFailed {
		// retry the activity; the completedPhases skip-guard resumes from the first
		// incomplete phase.
		return attemptRetry, nil
	}

	// --- Steps 5a-8a: finalize (arch +1 relay, change reviewed, gated merge, binary
	// exit, per-activity COMPLETED). ---------------------------------------------
	if err := wf.finalizeActivity(ctx, in, gf, headVersion, state, gitOn, startedCred); err != nil {
		return attemptDone, err
	}
	return attemptDone, nil
}

// loadReviewSnapshot performs the per-execution start snapshot (B5): it reads the project
// ONCE (an Activity, recorded in history → replay-safe) and captures the committed
// ReviewPolicy BY VALUE (the gate's ONLY policy source; NEVER re-read mid-loop), seeds the
// LIVE completedPhases skip-guard (B2 resumability) from the activity's PhaseCompletion
// slice, and captures the contract keys for the gate's reviewer set.
//
// Temporal versioning guard (replay safety): this readProject call was ADDED by the
// construction-review-policy-snapshot feature AFTER the workflow was first shipped.
// Workflows already in flight at deploy time have no history event for this call; replaying
// them against new code would produce a non-determinism error. GetVersion guards the new
// block so pre-feature in-flight executions (DefaultVersion) skip it entirely — reviewPolicy
// stays zero (empty → inert → no gate) and completedPhases stays initialized-empty. The gate
// takes effect only for workflows started after the feature deployed (v >= 1).
func (wf *workflows) loadReviewSnapshot(
	ctx workflow.Context,
	in constructActivityInput,
	state *constructState,
) (projectstate.ReviewPolicy, error) {
	var reviewPolicy projectstate.ReviewPolicy
	v := workflow.GetVersion(ctx, "construction-review-policy-snapshot", workflow.DefaultVersion, 1)
	if v < 1 {
		return reviewPolicy, nil
	}
	snap, srErr := wf.readProject(ctx, in.ProjectID)
	if srErr != nil && !isReadNotFound(srErr) {
		return reviewPolicy, srErr
	}
	reviewPolicy = snap.ReviewPolicy
	if acs, ok := snap.ActivityConstruction[string(in.ActivityID)]; ok {
		for _, pc := range acs.Phases {
			if pc.Completed {
				state.completedPhases[pc.Phase] = true
			}
		}
	}
	state.reviewContracts = snapshotContractKeys(snap)
	return reviewPolicy, nil
}

// failVarianceExhausted records the terminal FAILURE in head-state when the supervision
// loop exhausts its variance/retry budget (so the activity is no longer stuck Running).
func (wf *workflows) failVarianceExhausted(
	ctx workflow.Context,
	in constructActivityInput,
	headVersion *projectstate.Version,
	state *constructState,
	startedCred railCredEnvelope,
) error {
	v, e := wf.recordActivityFailed(ctx, in, *headVersion, projectstate.VarianceExhausted,
		"construction supervision exceeded max attempts", startedCred)
	if e != nil {
		return e
	}
	*headVersion = v
	state.stage = StageExited
	workflow.GetLogger(ctx).Info("construction activity failed — variance budget exhausted", "activityId", in.ActivityID)
	return nil
}

// runArchitectOnly handles the architectOnly hand-off: skip dispatch + pipeline and await
// the architect's steer on operatorOverride, BOUNDED by EscalationWaitTimeout. Returns
// done=true when the activity terminally exits (an escalation timeout, or an override that
// exits — e.g. Skip), false when the override loops back into supervision.
func (wf *workflows) runArchitectOnly(
	ctx workflow.Context,
	in constructActivityInput,
	overrideCh workflow.ReceiveChannel,
	headVersion *projectstate.Version,
	state *constructState,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, error) {
	state.stage = StageAwaitingTakeover
	sig, got := wf.awaitOverrideBounded(ctx, overrideCh)
	if !got {
		v, e := wf.recordActivityFailed(ctx, in, *headVersion, projectstate.EscalationTimedOut,
			"architect override timed out: no operator steer within the escalation-wait window", startedCred)
		if e != nil {
			return false, e
		}
		*headVersion = v
		state.stage = StageExited
		return true, nil
	}
	return wf.executeOverride(ctx, in, sig.Override, headVersion, state, gitOn, startedCred)
}

// walkPhases dispatches ONE GH-Actions job per profile phase, riding the CI poll cadence
// (observeCIAndRecord). The LIVE completedPhases skip-guard (B2) keeps the outer variance-
// retry (which re-walks from index 0) from re-dispatching or re-gating an already-completed
// phase. It returns (phaseFailed, done, err): done=true means a phase gate terminally
// recorded the activity (the workflow returns); phaseFailed=true means the caller should
// retry the activity.
func (wf *workflows) walkPhases(
	ctx workflow.Context,
	in constructActivityInput,
	attempt int,
	reviewPolicy projectstate.ReviewPolicy,
	state *constructState,
	gf *gitForward,
	headVersion *projectstate.Version,
	overrideCh workflow.ReceiveChannel,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, bool, error) {
	for _, phase := range in.Activity.Phases {
		if state.completedPhases[phase] {
			continue
		}
		state.stage = StagePipelineRunning
		obs, perr := wf.runPipeline(ctx, in, phase, state, gf, headVersion)
		if perr != nil {
			return false, false, perr
		}
		if obs.Phase == PipelineFailed || obs.Phase == PipelineCancelled {
			failReason := deriveFailureReason(obs.Phase, obs.Diagnostic)
			// intervention.WorkerMiss — the historical variancePipelineFailed → WorkerMiss
			// fold (the retired interventionVarianceKind many-to-one map, deps.go), the
			// only local variance kind this call site ever exercised.
			done, vErr := wf.handleVariance(ctx, in, intervention.WorkerMiss, obs.Diagnostic, failReason, attempt, headVersion, state, overrideCh, gitOn, startedCred)
			if vErr != nil {
				return false, false, vErr
			}
			if done {
				return false, true, nil
			}
			return true, false, nil
		}
		// Conditional per-phase approval gate (Task 6): records the phase start and — iff
		// the policy requires a human for this (activityType, phase) — suspends on the
		// phase-multiplexed decision signal. Approve/no-gate mark completion; a terminal
		// gate exit (done) has already recorded the activity.
		if done, gErr := wf.runPhaseGate(ctx, in, phase, reviewPolicy, state, gf, headVersion, gitOn, startedCred); gErr != nil {
			return false, false, gErr
		} else if done {
			return false, true, nil
		}
	}
	return false, false, nil
}

// finalizeActivity runs the clean-pass tail of an attempt (constructionManager.md §6.3
// steps 5a-8a): relay the architecture +1, record the change reviewed, perform the gated
// merge (interventionEngine is the App-only-merge authority), record the binary activity
// exit, and record the per-activity construction COMPLETED. The git-forward steps are
// no-ops when the slice is unwired.
func (wf *workflows) finalizeActivity(
	ctx workflow.Context,
	in constructActivityInput,
	gf *gitForward,
	headVersion *projectstate.Version,
	state *constructState,
	gitOn bool,
	startedCred railCredEnvelope,
) error {
	// --- Step 5a: relay the architecture +1 and record it (git-forward). ---
	if err := wf.relayArchApprovalAndRecord(ctx, in, gf, headVersion); err != nil {
		return err
	}

	// --- Step 6: record the change reviewed (head-state). ---
	v, e := wf.recordChangeReviewed(ctx, in, *headVersion, startedCred)
	if e != nil {
		return e
	}
	*headVersion = v

	// --- Step 6a: perform the gated merge and record it (git-forward). ---
	if err := wf.mergeAndRecord(ctx, in, gf, headVersion); err != nil {
		return err
	}

	// --- Step 8: record the binary activity exit (head-state). ---
	v2, e2 := wf.recordActivityExited(ctx, in, *headVersion, projectstate.ActivityOutcomeCompleted, startedCred)
	if e2 != nil {
		return e2
	}
	*headVersion = v2

	// --- Step 8a: record the per-activity construction COMPLETED (Task 3). Flip the
	// activity to Done so the pump's eligibility selection unblocks its dependents on the
	// next tick. Dormant (no-op) when the git slice is unwired. ---
	if gitOn {
		if err := wf.recordActivityCompleted(ctx, in, startedCred, headVersion); err != nil {
			return err
		}
	}

	state.stage = StageExited
	workflow.GetLogger(ctx).Info("construction activity exited", "activityId", in.ActivityID)
	return nil
}

// runPipeline submits the pipeline then polls observe between durable startTimer
// waits until the pipeline reaches a terminal phase (§6.3 step 3). On each observe it
// ALSO reads the PR's CI rollup and mirrors it onto the head-state (the git-forward
// poll-loop verb, C-MCN-GIT) — dormant when the git slice is unwired.
func (wf *workflows) runPipeline(ctx workflow.Context, in constructActivityInput, phase projectstate.ActivityMethodPhase, state *constructState, gf *gitForward, headVersion *projectstate.Version) (pipelineObservation, error) {
	handle, err := wf.submitPipeline(ctx, pipelineSpec{
		ActivityID:  string(in.ActivityID),
		ComponentID: in.Activity.ComponentID,
		Phase:       phase.String(),
	})
	if err != nil {
		return pipelineObservation{}, err
	}

	for poll := 0; poll < maxPipelinePolls; poll++ {
		obs, err := wf.observePipeline(ctx, handle)
		if err != nil {
			return pipelineObservation{}, err
		}
		ph := obs.Phase
		state.pipelinePhase = &ph

		// Mirror the PR's CI rollup onto the head-state on the same cadence.
		if _, cerr := wf.observeCIAndRecord(ctx, in, gf, headVersion); cerr != nil {
			return pipelineObservation{}, cerr
		}

		if obs.Phase == PipelineSucceeded || obs.Phase == PipelineFailed {
			return obs, nil
		}
		// Durable wait between polls (the Manager's own startTimer — category A).
		_ = workflow.Sleep(ctx, pipelinePollInterval)
	}
	return pipelineObservation{Phase: PipelineFailed, Diagnostic: "pipeline did not reach a terminal phase within the poll budget"}, nil
}

// ---------------------------------------------------------------------------
// Conditional per-phase approval gate (Task 6). runPhaseGate records the phase
// start, and — iff the committed ReviewPolicy requires a human for this
// (activityType, phase) — suspends on the phase-multiplexed decision signal. Approve
// records completion; SendBack redrafts THIS phase up to maxPhaseRedrafts and then
// (mirroring systemdesign) KEEPS awaiting the human — it NEVER re-enters the variance
// loop and never fails the activity. Returns done=true only when the gate has
// terminally recorded this activity (there is no such terminal in v1, but the
// signature preserves that seam). The phase-start / head-state records are gated on
// gitOn; under the NON-GIT profile an empty policy is inert and produces no
// head-state writes (aside from the in-memory completedPhases bookkeeping). Under the
// GIT profile, RecordPhaseStarted and RecordPhaseCompleted are emitted for EVERY
// phase regardless of whether a human gate is active — this is intentional progress
// tracking (gitOn-gated) and is NOT byte-for-byte the non-git path.
func (wf *workflows) runPhaseGate(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	policy projectstate.ReviewPolicy,
	state *constructState,
	gf *gitForward,
	headVersion *projectstate.Version,
	gitOn bool,
	cred railCredEnvelope,
) (bool, error) {
	if gitOn {
		v, e := wf.recordPhaseStarted(ctx, in, phase, *headVersion, cred)
		if e != nil {
			return false, e
		}
		*headVersion = v
	}

	// Inert policy (or a phase this policy does not gate) → complete immediately (no
	// suspend). completePhase marks the in-memory set UNCONDITIONALLY.
	if !policy.RequiresHuman(in.Activity.activityTypeName(), phase) {
		return false, wf.completePhase(ctx, in, phase, state, headVersion, gitOn, cred)
	}

	// Surface the reviewer set on the session view (display-only in v1; on engine error
	// leave it unset and still gate — the human Approve/SendBack is the enforced gate).
	if rs, e := wf.proposeReviewSet(in, phase, state); e == nil {
		state.reviewSet = &rs // NOTE: *ReviewSet (B6)
	}

	return wf.awaitPhaseDecision(ctx, in, phase, state, gf, headVersion, gitOn, cred)
}

// awaitPhaseDecision is the suspend + redraft loop of the gate (extracted so
// runPhaseGate stays under the gocognit budget). It drains the phase-multiplexed
// decision channel until a decision for THIS phase arrives, then acts on it: Approve
// completes the phase; SendBack redrafts THIS phase in place (its OWN redraft budget,
// NOT the variance budget); on redraft exhaustion it keeps awaiting the human.
func (wf *workflows) awaitPhaseDecision(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	gf *gitForward,
	headVersion *projectstate.Version,
	gitOn bool,
	cred railCredEnvelope,
) (bool, error) {
	ch := workflow.GetSignalChannel(ctx, signalPhaseDecision)
	redraft := 0
	for {
		state.stage = StageAwaitingApproval
		sig := receivePhaseDecision(ctx, ch, phase)
		switch sig.Decision {
		case PhaseDecisionUnknown:
			// zero-value sentinel, not a real decision — ignore and keep awaiting, same as default.
		case PhaseApprove:
			return false, wf.completePhase(ctx, in, phase, state, headVersion, gitOn, cred)
		case PhaseSendBack:
			redraft++
			if redraft >= maxPhaseRedrafts {
				// Exhausted the human-paced redraft budget. Do NOT fail the activity and do
				// NOT re-enter the variance loop — keep awaiting the human, surfacing that
				// redrafting is spent (mirrors systemdesign's anti-wedge staging).
				state.redraftExhausted = true
				workflow.GetLogger(ctx).Warn("phase redraft budget exhausted; keep awaiting human decision",
					"activityId", in.ActivityID, "phase", phase.String(), "exhausted", state.redraftExhausted)
				continue
			}
			state.stage = StagePipelineRunning
			if _, e := wf.runPipeline(ctx, in, phase, state, gf, headVersion); e != nil {
				return false, e
			}
		default:
			// Unknown decision: ignore and keep awaiting the human.
		}
	}
}

// receivePhaseDecision blocks on the decision channel, draining and DISCARDING
// decisions for other phases (stale/multiplexed), until one for THIS phase arrives.
func receivePhaseDecision(ctx workflow.Context, ch workflow.ReceiveChannel, phase projectstate.ActivityMethodPhase) phaseDecisionSignal {
	var sig phaseDecisionSignal
	for {
		ch.Receive(ctx, &sig)
		if sig.Phase == phase.String() {
			return sig
		}
	}
}

// completePhase is the SINGLE phase-completion path (both the no-gate branch and the
// Approve branch call it). It MARKS the LIVE in-memory completedPhases set
// UNCONDITIONALLY (this is what closes the variance-retry re-gate and the non-git
// case where no head-state completion record exists to re-read), THEN records the
// completion to head-state via the Task-5 RecordPhaseCompleted (artifactRef="") ONLY
// when gitOn.
func (wf *workflows) completePhase(
	ctx workflow.Context,
	in constructActivityInput,
	phase projectstate.ActivityMethodPhase,
	state *constructState,
	headVersion *projectstate.Version,
	gitOn bool,
	cred railCredEnvelope,
) error {
	state.completedPhases[phase] = true
	if !gitOn {
		return nil
	}
	v, err := wf.applyRecovering(ctx, in.ProjectID, *headVersion, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordPhaseCompleted(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), phase, "", cred.toProjectState())
	})
	if err != nil {
		return err
	}
	*headVersion = v
	return nil
}

// recordPhaseStarted records the phase-started head-state transition (Task-5
// RecordPhaseStarted) through the §6.5 Conflict loop. Gated on gitOn by the caller.
func (wf *workflows) recordPhaseStarted(ctx workflow.Context, in constructActivityInput, phase projectstate.ActivityMethodPhase, seed projectstate.Version, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordPhaseStarted(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), phase, cred.toProjectState())
	})
}

// proposeReviewSet builds the review.ReviewChange + artifactKind for this
// activity/phase and calls the PURE published review.ReviewEngine directly
// (deterministic, replay-safe). The contracts are sourced from the start-snapshot
// project (B5); the architecture graph is not carried across the pump's
// projectEnvelope, so it is passed empty (the reviewer set is display-only in v1).
// reviewSetFromEngine (adapters.go) bridges the Engine's own ReviewSet onto this
// component's generated façade ReviewSet (contract.gen.go) — a real divergence, not
// an identity mirror (see deps.go's reviewEngine retirement note).
func (wf *workflows) proposeReviewSet(in constructActivityInput, phase projectstate.ActivityMethodPhase, state *constructState) (ReviewSet, error) {
	change := review.ReviewChange{ActivityID: string(in.ActivityID), ComponentID: in.Activity.ComponentID}
	set, err := wf.Review.ProposeReviews(fweng.Context{Context: context.Background()},
		change, in.Activity.ComponentID, phase.String(), "", state.reviewContracts)
	if err != nil {
		return ReviewSet{}, err
	}
	return reviewSetFromEngine(set), nil
}

// snapshotContractKeys derives the deterministic (sorted) set of contract identifiers
// from the start-snapshot project's committed service contracts — the display input
// for the gate's reviewer set. Sorted so the derived slice is replay-stable (map
// iteration order is randomized).
func snapshotContractKeys(p projectstate.Project) []string {
	if len(p.ServiceContracts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(p.ServiceContracts))
	for k := range p.ServiceContracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// handleVariance is the DECIDE→EXECUTE machinery for an automatically-detected
// variance (constructionManager.md §6.3 step 7). It calls interventionEngine
// (DECIDE) and EXECUTES the directive: Retry → loop again (return done=false);
// Escalate → await an operator override and execute it; Takeover → re-dispatch
// (loop). Returns done=true when the activity has reached a terminal exit.
func (wf *workflows) handleVariance(
	ctx workflow.Context,
	in constructActivityInput,
	kind intervention.VarianceKind,
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

	// NOTE: ProjectID is fed from in.ActivityID, not in.ProjectID — this mirrors the
	// retired interventionAdapter's historical behavior verbatim (zero behavior change;
	// deps.go's interventionEngine retirement note). Detail/OperatorSourced (the retired
	// constructionVariance struct's other fields) were write-only under the old adapter —
	// never forwarded to the Engine — so they are simply not carried here.
	directive, derr := wf.Intervention.DecideOnVariance(fweng.Context{Context: context.Background()}, intervention.ConstructionVariance{
		ProjectID:    intervention.ProjectID(in.ActivityID),
		ActivityID:   intervention.ActivityID(in.ActivityID),
		Kind:         kind,
		AttemptCount: int64(attempt),
		Policy:       wf.InterventionPolicy,
	})
	if derr != nil {
		return false, fwmanager.MapError(derr)
	}

	switch directive {
	case intervention.VarianceRetry:
		state.stage = StageDispatching
		return false, nil // loop to re-dispatch
	case intervention.VarianceTakeover:
		// EXECUTE takeover: loop to re-dispatch under a changed arrangement. The
		// prior phase pipeline already reached a terminal state before intervention
		// was consulted, so there is no in-flight dispatch to abandon here.
		state.stage = StageDispatching
		return false, nil
	case intervention.VarianceEscalate:
		// EXECUTE escalate: surface to the operator + await an override signal, BOUNDED
		// by EscalationWaitTimeout. On timeout (no operator answered the escalation), the
		// activity terminally FAILS (head-state reflects EscalationTimedOut) instead of
		// hanging forever waiting for an override that never comes.
		state.stage = StageAwaitingTakeover
		sig, got := wf.awaitOverrideBounded(ctx, overrideCh)
		if !got {
			_ = failReason // underlying cause is carried in detail below; the terminal reason is EscalationTimedOut
			v, e := wf.recordActivityFailed(ctx, in, *headVersion, projectstate.EscalationTimedOut,
				"escalation timed out: no operator override within the escalation-wait window (underlying: "+detail+")", startedCred)
			if e != nil {
				return false, e
			}
			*headVersion = v
			state.stage = StageExited
			return true, nil
		}
		return wf.executeOverride(ctx, in, sig.Override, headVersion, state, gitOn, startedCred)
	default:
		// intervention.VarianceDirective has no Unknown sentinel (VarianceRetry is its
		// zero value) — any value outside {VarianceRetry, VarianceTakeover,
		// VarianceEscalate} is an unrecognized engine decision, same non-retryable
		// rejection as before (deps.go's interventionEngine retirement note).
		return false, temporal.NewNonRetryableApplicationError(
			"intervention returned an unknown directive", "UnknownDirective", nil)
	}
}

// executeOverride runs the operator's manual steer through the same execute
// machinery as the automatic variance path (constructionManager.md §2.4 / §6.3
// override branch). Returns done=true when the override terminally exits the
// activity (Skip), false when it loops back into supervision (Retry/Takeover/Reassign).
func (wf *workflows) executeOverride(
	ctx workflow.Context,
	in constructActivityInput,
	override ActivityOverride,
	headVersion *projectstate.Version,
	state *constructState,
	gitOn bool,
	startedCred railCredEnvelope,
) (bool, error) {
	switch override.Kind {
	case OverrideUnknown:
		// zero-value sentinel, not a real override kind — same as any unmapped value.
		return false, temporal.NewNonRetryableApplicationError(
			"unknown operator override kind", "UnknownOverride", nil)
	case OverrideRetry, OverrideReassign:
		// Re-enter the dispatch path (Reassign re-casts via handOffEngine on the
		// next loop iteration — the committed constructionManager → handOffEngine
		// edge, OQ-3).
		state.stage = StageDispatching
		return false, nil
	case OverrideTakeover:
		// Loop to re-dispatch; the prior phase pipeline is already terminal (see the
		// directiveTakeover note), so there is no in-flight dispatch to abandon.
		state.stage = StageDispatching
		return false, nil
	case OverrideSkip:
		v, e := wf.recordActivityExited(ctx, in, *headVersion, projectstate.ActivityOutcomeSkipped, startedCred)
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
func (wf *workflows) awaitOverrideBounded(ctx workflow.Context, overrideCh workflow.ReceiveChannel) (operatorOverrideSignal, bool) {
	var sig operatorOverrideSignal
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

// ===========================================================================
// ReplanSweepWorkflow — op 2.2 (scheduler-triggered, 5m). Flags over-threshold
// variances; NO dispatch, NO auto-replan.
// ===========================================================================

// replanSweepInput is the start payload for ReplanSweepWorkflow.
type replanSweepInput struct {
	ProjectID *ProjectID // nil ⇒ sweep all in-flight projects
}

func (wf *workflows) ReplanSweepWorkflow(ctx workflow.Context, in replanSweepInput) (ReplanSweepResult, error) {
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
func (wf *workflows) flagVariances(_ projectstate.Project) []FlaggedVariance {
	return nil
}

// ---------------------------------------------------------------------------
// Head-state read + recovering write helpers (§6.5).
// ---------------------------------------------------------------------------

// readProject runs the ReadProjectActivity and returns the projected head-state.
func (wf *workflows) readProject(ctx workflow.Context, projectID ProjectID) (projectstate.Project, error) {
	c := readProjectOpts(ctx)
	var pe projectEnvelope
	// Convert the Manager's OWN ProjectID to projectStateAccess's at the RA boundary.
	if err := workflow.ExecuteActivity(c, wf.ReadProjectActivity, projectstate.ProjectID(projectID)).Get(ctx, &pe); err != nil {
		return projectstate.Project{}, err
	}
	return decodeProject(pe), nil
}

// readVersionE runs the cheap ReadProjectVersion GENERATED invoker (B8: migrated off the
// custom ReadProjectVersionActivity) and returns ONLY the head-state optimistic-
// concurrency token, surfacing errors (including the brand-new project's fwra.NotFound)
// to the caller. Replaces the wasteful whole-aggregate read that shipped the entire
// encoded Project across the Temporal Activity boundary for a uint64 (architect's
// fast-follow). The invoker's Opts hook applies readProjectActivityOptions (identical
// preset, keyed "projectStateAccess.readProjectVersion" — workermanifest.go).
func (wf *workflows) readVersionE(ctx workflow.Context, projectID ProjectID) (projectstate.Version, error) {
	return wf.Acts.ProjectStateReadProjectVersion(ctx, projectstate.ProjectID(projectID))
}

// readVersion reads the current head Version (0 for a brand-new project or on any
// read error — the read-your-writes seed treats absence as version 0).
func (wf *workflows) readVersion(ctx workflow.Context, projectID ProjectID) projectstate.Version {
	v, err := wf.readVersionE(ctx, projectID)
	if err != nil {
		return 0
	}
	return v
}

// recordChangeReviewed applies the head-state transition with the Conflict loop. The
// Manager-minted cred is threaded into the write (empty/zero in dev/dry-run).
func (wf *workflows) recordChangeReviewed(ctx workflow.Context, in constructActivityInput, seed projectstate.Version, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordChangeReviewed(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), cred.toProjectState())
	})
}

// recordActivityExited applies the binary-exit head-state transition.
func (wf *workflows) recordActivityExited(ctx workflow.Context, in constructActivityInput, seed projectstate.Version, outcome projectstate.ActivityOutcome, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordActivityExited(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), outcome, cred.toProjectState())
	})
}

// recordActivityFailed applies the terminal-FAILURE head-state transition (the
// bounded-wait / autonomous-retry fix) with the same head-version Conflict re-read
// loop as recordActivityExited. It lands Phase=Failed / BuildStatus=BuildFailed and
// records the reason+detail so head-state reflects the terminal instead of leaving
// the activity stuck Running.
func (wf *workflows) recordActivityFailed(ctx workflow.Context, in constructActivityInput, seed projectstate.Version, reason projectstate.FailureReason, detail string, cred railCredEnvelope) (projectstate.Version, error) {
	return wf.applyRecovering(ctx, in.ProjectID, seed, func(expected projectstate.Version) (projectstate.Version, error) {
		return wf.Acts.ConstructionTransitionRecordActivityFailed(ctx, projectstate.ProjectID(in.ProjectID), expected,
			string(in.ActivityID), reason, detail, cred.toProjectState())
	})
}

// applyRecovering executes one head-state mutation Activity with a workflow-level
// Conflict re-read→re-apply loop (§6.5; identical discipline to systemdesign).
func (wf *workflows) applyRecovering(
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
		v, rerr := wf.readVersionE(ctx, projectID)
		if rerr != nil {
			if isReadNotFound(rerr) {
				expected = 0
				continue
			}
			return 0, rerr
		}
		expected = v
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
