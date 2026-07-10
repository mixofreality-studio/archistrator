package projectdesign

// dispatch.go is the AGENTIC-PIVOT seam (D-MPD-Δ, projectDesignManager.md §0.5) —
// the Phase-2 TWIN of systemdesign/dispatch.go. The Phase-2 plan-DRAFTING mechanism
// flips from a synchronous workerAccess call to an ASYNC dispatch → observe →
// read-back round-trip:
//
//   - DISPATCH  the Manager composes the Method Phase-2 role prompt IN-MEMORY
//               (never persisted) and dispatches a claude-code-action DESIGN job via
//               the FROZEN constructionPipelineAccess.SubmitConstructionPipeline verb,
//               carrying {artifact_kind, design_prompt, target_branch,
//               prior_state_ref} on the additive pipelineSpec.DispatchInputs field
//               (C-WF-DESIGN input schema, ADDED by C-MSD-Δ). The RA reserves +
//               stamps idempotency_token itself; the Manager MUST NOT set it.
//   - OBSERVE   the Manager polls ObserveConstructionPipeline(handle) between
//               durableExecutionAccess timer waits until a TYPED terminal phase.
//   - READ-BACK on PhaseSucceeded the Manager reads the committed typed Phase-2 Kind
//               via projectStateAccess.ReadProject (the Action committed the JSON;
//               aiarch writes nothing on the draft path).
//
// The ONE structural difference from the twin (projectDesignManager.md §0.5.5): the
// three estimation Engines (constructionEstimationEngine / operationEstimationEngine
// / settlementEngine) STAY server-side in-workflow — they are deterministic, pure,
// by-value joins, NOT LLM work, and do NOT dispatch. There is also NO PM-critique in
// Phase 2 (the architect owns the project-design artifacts and recommends to
// management at the SDP gate), so this file has NO critique round-trip — only the
// DRAFT round-trip. workerAccess and artifactValidationEngine are DROPPED from the
// draft path (§0.5.5).
//
// THE IDEMPOTENCY KEY IS DERIVED INSIDE THE DISPATCH ACTIVITY (construction note
// N1). Temporal assigns a distinct ActivityID per ExecuteActivity invocation and
// reuses it across automatic retries of that one invocation. So a REDRAFT loop
// (a fresh ExecuteActivity(DispatchDesignJobActivity)) gets a new ActivityID → a
// distinct key → a fresh, idempotent job (NOT a dedup of the stale prior job); a
// transient auto-retry of a single dispatch keeps the ActivityID → same key → the
// FROZEN submit verb collapses it to the same handle.

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// pipelineDefaultToolchain is the placeholder toolchain stamped on the logical design
// step (the real design recipe lives in the user's aiarch-design.yml workflow file).
const pipelineDefaultToolchain = "go-1.23"

// ===========================================================================
// Workflow-side pipeline helpers. The temporalgen migration routes the submit/observe
// design-job pair through the GENERATED constructionPipelineAccess invokers (wf.Acts.
// PipelineSubmit/ObserveConstructionPipeline); the value mapping that lived on the folded
// pipelineDispatchAdapter — the RepoRef→RepoTarget decode, the PipelineSpec composition,
// and the RA-phase→neutral-phase mapping — is now these PURE workflow-side helpers
// (mirrors construction's dispatch.go). The idempotency key is stamped INSIDE the
// generated submit Activity (genActivityIdempotencyKey, the same run-scoped 3-part scheme
// the old hand-derived key used), so the redraft-vs-auto-retry distinction is unchanged.
// ===========================================================================

// dispatchDesignJob composes the constructionpipeline.PipelineSpec for one design job and
// submits it through the generated invoker, returning the opaque handle. The four DESIGN
// parameters ride on DispatchInputs; a per-project TargetRepo (decoded from the opaque
// RepoRef) + WorkflowFile target the user's per-project repo + aiarch-design.yml, else an
// empty target falls back to the RA's configured construction repo.
func (wf *workflows) dispatchDesignJob(ctx workflow.Context, a dispatchDesignJobArgs) (constructionpipeline.PipelineHandle, error) {
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(a.ArtifactKind),
		dispatchInputDesignPrompt:  a.Prompt,
		dispatchInputTargetBranch:  a.TargetBranch,
		dispatchInputPriorStateRef: a.PriorStateRef,
	}
	// Per-project-design-dispatch: decode the opaque per-project RepoRef → owner/repo so
	// the RA dispatches to the USER'S per-project repo + aiarch-design.yml (NOT the central
	// construction repo). Empty TargetRepo ⇒ zero RepoTarget ⇒ the RA falls back.
	target, terr := designRepoTarget(a.TargetRepo)
	if terr != nil {
		return constructionpipeline.PipelineHandle(""), terr
	}
	spec := constructionpipeline.PipelineSpec{
		ProjectID: constructionpipeline.ProjectID(a.ProjectID),
		// A non-empty, well-formed step graph satisfies the RA's §2.1 pre-condition; the
		// design recipe lives in the user's aiarch-design.yml workflow file, so the step is
		// a logical placeholder. The Phase-2 DESIGN-job parameters ride on DispatchInputs.
		Steps: []constructionpipeline.PipelineStep{{
			Name:      "design",
			Toolchain: constructionpipeline.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: inputs,
		TargetRepo:     target,
	}
	if a.TargetRepo != "" {
		spec.WorkflowFile = designWorkflowFileName
	}
	return wf.Acts.PipelineSubmitConstructionPipeline(ctx, spec)
}

// observeDesignJob reads the dispatched job's phase once (pull-shaped, side-effect-free;
// constructionPipelineAccess.md §2.2) through the generated invoker and maps the RA phase
// onto this Manager's neutral phase.
func (wf *workflows) observeDesignJob(ctx workflow.Context, handle constructionpipeline.PipelineHandle) (pipelineObservation, error) {
	obs, err := wf.Acts.PipelineObserveConstructionPipeline(ctx, handle)
	if err != nil {
		return pipelineObservation{}, err
	}
	return pipelineObservation{
		Phase:      designPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
	}, nil
}

// designRepoTarget decodes an opaque per-project RepoRef String() into the RA's
// infrastructure-neutral RepoTarget{Owner, Name}. Empty ⇒ a zero RepoTarget (the RA
// falls back to the configured construction repo); a malformed ref surfaces the RA's
// ContractMisuse. Uses sourcecontrol's own OwnerRepo accessor so the RepoRef encoding
// stays owned by sourceControlAccess (no encoding leak here).
func designRepoTarget(repoRef string) (constructionpipeline.RepoTarget, error) {
	if repoRef == "" {
		return constructionpipeline.RepoTarget{}, nil
	}
	owner, name, err := sourcecontrol.RepoRefOwnerRepo(sourcecontrol.RepoRefFromString(repoRef))
	if err != nil {
		return constructionpipeline.RepoTarget{}, err
	}
	return constructionpipeline.RepoTarget{Owner: owner, Name: name}, nil
}

// designPipelinePhase maps the RA's phase to this Manager's neutral phase, preserving
// the Cancelled terminal distinctly (the design Manager treats any non-Succeeded
// terminal as a StageDraftFailed gate).
func designPipelinePhase(p constructionpipeline.PipelinePhase) pipelinePhase {
	switch p {
	case constructionpipeline.PhasePending:
		return pipelinePending
	case constructionpipeline.PhaseRunning:
		return pipelineRunning
	case constructionpipeline.PhaseSucceeded:
		return pipelineSucceeded
	case constructionpipeline.PhaseFailed:
		return pipelineFailed
	case constructionpipeline.PhaseCancelled:
		return pipelineCancelled
	default:
		return pipelinePhaseUnknown
	}
}

// pipelinePhase mirrors constructionPipelineAccess.md §3 — the infrastructure-neutral
// lifecycle phase the Manager branches on. The terminal trio drives the observe
// loop's exit + the failure path.
type pipelinePhase int

const (
	pipelinePhaseUnknown pipelinePhase = iota
	pipelinePending
	pipelineRunning
	pipelineSucceeded
	pipelineFailed
	pipelineCancelled
)

// IsTerminal reports whether the phase is one the job can no longer leave.
func (p pipelinePhase) IsTerminal() bool {
	switch p {
	case pipelineSucceeded, pipelineFailed, pipelineCancelled:
		return true
	case pipelinePhaseUnknown, pipelinePending, pipelineRunning:
		return false
	default:
		return false
	}
}

// pipelineObservation mirrors constructionPipelineAccess.md §3 — a point-in-time,
// infrastructure-neutral view carrying the phase and (on terminal failure) a neutral
// Diagnostic summary (NOT a log firehose).
type pipelineObservation struct {
	Phase      pipelinePhase
	Diagnostic string
}

// ===========================================================================
// Dispatch inputs (C-WF-DESIGN workflow_dispatch schema). These exact key names are
// the binding contract with aiarch-design.yml's workflow_dispatch.inputs.
// idempotency_token is RA-controlled and is NOT set here.
// ===========================================================================

const (
	dispatchInputArtifactKind  = "artifact_kind"
	dispatchInputDesignPrompt  = "design_prompt"
	dispatchInputTargetBranch  = "target_branch"
	dispatchInputPriorStateRef = "prior_state_ref"
)

// observePollInterval spaces the observe-poll loop's durable timer waits. A design
// job runs minutes in the user's CI; this is the in-workflow timer the contract
// prescribes (§0.5.2 step 4). Kept modest so the test's time-skipping env settles
// quickly.
const observePollInterval = 15 * time.Second

// maxObservePolls bounds the observe loop so a stuck (never-terminal) job cannot spin
// forever; exceeding it is treated as a terminal infrastructure failure and routed to
// the human gate (never a perpetual Drafting — the anti-wedge rule).
const maxObservePolls = 240 // 240 * 15s = 1h ceiling

// designBranch derives the ONE persistent design SESSION branch per Phase-2 artifact
// review session (F40 founder ruling 2026-07-05: commit to the same branch until it
// merges; the history of changes lives in git). ALL jobs of a session commit here — the
// initial draft and every redraft — and ONE PR (opened once, idempotent on head) merges
// it on approve. STABLE across every redraft/reject round (no per-attempt suffix; the F32
// branch-per-attempt topology is unwound, the stale-base problem now handled by the
// workflow template's refresh-from-main git step). amendment > 0 selects a FRESH branch
// for an AMENDMENT session (F38) whose v1 branch/PR already merged.
func designBranch(projectID ProjectID, kind ArtifactKind, amendment int) string {
	base := fmt.Sprintf("aiarch-design/%s/%d", projectID, int(kind))
	if amendment > 0 {
		return fmt.Sprintf("%s-amend-%d", base, amendment)
	}
	return base
}

// dispatchDesignJobArgs bundles the dispatch inputs for the Activity boundary. The
// Manager's SEQUENCE composed Prompt in-memory (prompts.go); ArtifactKind + Branch +
// PriorStateRef ride into the DispatchInputs map inside the Activity.
type dispatchDesignJobArgs struct {
	ProjectID     ProjectID
	ArtifactKind  ArtifactKind
	Prompt        string
	TargetBranch  string
	PriorStateRef string
	// TargetRepo is the opaque per-project RepoRef (gitSession.repoRef.String()) the
	// design job must dispatch to — the user's per-project repo where aiarch-design.yml
	// was committed at project birth (per-project-design-dispatch). Empty ⇒ the RA falls
	// back to the configured construction repo (the dormant-rail / non-git path).
	TargetRepo string
}

// dispatchAndObserve runs ONE dispatch → observe round-trip: it dispatches the design
// job (the generated submit invoker via dispatchDesignJob) and then polls the observe
// invoker (observeDesignJob) between durable startTimer waits until the job reaches a
// TYPED terminal phase. It returns the terminal observation; the caller decides success
// (read-back) vs failure (the StageDraftFailed gate). It NEVER infers failure from a
// timeout-as-success (§0.5.4): a stuck job that never terminates within the bounded poll
// budget is surfaced as an explicit pipelineFailed with a neutral diagnostic, so the
// caller still lands the session at the human gate.
func (wf *workflows) dispatchAndObserve(ctx workflow.Context, args dispatchDesignJobArgs) (pipelineObservation, error) {
	handle, err := wf.dispatchDesignJob(ctx, args)
	if err != nil {
		return pipelineObservation{}, err
	}
	if constructionpipeline.PipelineHandleIsZero(handle) {
		return pipelineObservation{}, temporal.NewNonRetryableApplicationError(
			"dispatch returned an empty pipeline handle", "EmptyPipelineHandle", nil)
	}

	for poll := 0; poll < maxObservePolls; poll++ {
		obs, err := wf.observeDesignJob(ctx, handle)
		if err != nil {
			return pipelineObservation{}, err
		}
		if obs.Phase.IsTerminal() {
			return obs, nil
		}
		// Not yet terminal — space the next observe with a durable in-workflow timer.
		if err := workflow.Sleep(ctx, observePollInterval); err != nil {
			return pipelineObservation{}, err
		}
	}
	// Bounded poll budget exhausted without a terminal phase. Treat as an explicit
	// terminal failure (NOT a success, NOT a perpetual Drafting) so the caller routes
	// to the StageDraftFailed human gate.
	return pipelineObservation{
		Phase:      pipelineFailed,
		Diagnostic: "design job did not reach a terminal state within the observation window",
	}, nil
}

// dispatchActivityOptions is the option preset for the generated
// constructionPipelineAccess.submitConstructionPipeline Activity (consumed by the
// manager's option hook — workermanifest.go). A transient submit error (ErrTransient /
// Retryable) auto-retries via this RetryPolicy; a terminal RA fault (ContractMisuse / Auth
// / QuotaExhausted) is non-retryable and surfaces to the workflow body. A PhaseFailed is
// NOT a dispatch error — it is a successful observation of a failed job (§0.5.4).
func dispatchActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 5,
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.ContractMisuse),
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.QuotaExhausted),
			},
		},
	}
}

// observeActivityOptions is the option preset for the generated
// constructionPipelineAccess.observeConstructionPipeline Activity. Transient reads retry;
// a NotFound (GC'd handle) is non-retryable and surfaces.
func observeActivityOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	}
}

// readBackCommittedModelOn is readBackCommittedModel with an OPTIONAL branch override
// (I-DESIGN-DISPATCH §2a): the draft Action commits the typed JSON on the SESSION
// BRANCH, so the read-back reads that branch while the human reviews the not-yet-merged
// draft. branch=="" reads main (the dormant-rail / non-git behavior). It returns the
// read-back substrate's Version alongside the model so the caller can stage against the
// ACTUAL branch version — a fresh workflow reusing a dirty session branch (prior
// draft/critique commits) sees the branch already advanced, and staging against a stale
// main-captured version would Conflict (QA F29).
func (wf *workflows) readBackCommittedModelOn(ctx workflow.Context, projectID ProjectID, kind ArtifactKind, branch string) (projectstate.ArtifactModel, projectstate.Version, error) {
	proj, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return nil, 0, err
	}
	slot := slotFor(proj, toPSKind(kind))
	if slot.Model == nil {
		return nil, 0, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("design job reported success but committed no %s model to read back", toPSKind(kind)),
			"ReadBackEmpty", nil)
	}
	return slot.Model, proj.Version, nil
}
