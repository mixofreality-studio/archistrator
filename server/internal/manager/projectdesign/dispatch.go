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
//               prior_state_ref} on the additive PipelineSpec.DispatchInputs field
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
	"context"
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

// ===========================================================================
// Internal activity/test seam — constructionPipelineAccess. The manager DEPENDS on
// the PUBLISHED constructionpipeline.ConstructionPipelineAccess (taken by its
// generated constructor); this UNEXPORTED seam is the Temporal-free, plain-ctx
// projection the hand-written design Activities consume + the test fakes inject. The
// folded pipelineDispatchAdapter (below) maps the published RA — formerly the
// composition-root designProjectDesignPipelineAdapter — onto this seam. The former
// EXPORTED consumer-mirror interface is RETIRED.
// ===========================================================================

// constructionPipelineAccess is the subset of the FROZEN constructionPipelineAccess
// surface (constructionPipelineAccess.md §2) the Phase-2 draft path depends on:
// dispatch (submit) + observe.
type constructionPipelineAccess interface {
	SubmitConstructionPipeline(ctx context.Context, spec PipelineSpec, idempotencyKey fwra.IdempotencyKey) (PipelineHandle, error)
	ObserveConstructionPipeline(ctx context.Context, handle PipelineHandle) (PipelineObservation, error)
}

// pipelineDefaultToolchain is the placeholder toolchain stamped on the logical design
// step (the real design recipe lives in the user's aiarch-design.yml workflow file).
const pipelineDefaultToolchain = "go-1.23"

// pipelineDispatchAdapter is the FOLDED composition-root designProjectDesignPipelineAdapter:
// it maps this package's neutral, Temporal-serializable PipelineSpec/Handle/Observation
// onto the PUBLISHED constructionpipeline.ConstructionPipelineAccess (building the fwra
// call Context at the boundary). RegisterWorker wraps the published dep in this adapter
// so the hand-written Workflows keep their plain-ctx seam.
type pipelineDispatchAdapter struct {
	inner constructionpipeline.ConstructionPipelineAccess
}

var _ constructionPipelineAccess = pipelineDispatchAdapter{}

func (a pipelineDispatchAdapter) SubmitConstructionPipeline(ctx context.Context, spec PipelineSpec, idempotencyKey fwra.IdempotencyKey) (PipelineHandle, error) {
	// Per-project-design-dispatch: decode the opaque per-project RepoRef → owner/repo so
	// the RA dispatches the agentic DESIGN job to the USER'S per-project repo +
	// aiarch-design.yml (NOT the central construction repo). Empty TargetRepo ⇒ zero
	// RepoTarget ⇒ the RA falls back to the configured construction repo.
	target, terr := designRepoTarget(spec.TargetRepo)
	if terr != nil {
		return PipelineHandle{}, terr
	}
	handle, err := a.inner.SubmitConstructionPipeline(fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey}, constructionpipeline.PipelineSpec{
		ProjectID: constructionpipeline.ProjectID(spec.ProjectID),
		// A non-empty, well-formed step graph satisfies the RA's §2.1 pre-condition; the
		// design recipe lives in the user's aiarch-design.yml workflow file, so the step is
		// a logical placeholder. The Phase-2 DESIGN-job parameters ride on DispatchInputs.
		Steps: []constructionpipeline.PipelineStep{{
			Name:      "design",
			Toolchain: constructionpipeline.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		DispatchInputs: spec.DispatchInputs,
		TargetRepo:     target,
		WorkflowFile:   spec.WorkflowFile,
	})
	if err != nil {
		return PipelineHandle{}, err
	}
	return PipelineHandle{Name: constructionpipeline.PipelineHandleString(handle)}, nil
}

func (a pipelineDispatchAdapter) ObserveConstructionPipeline(ctx context.Context, handle PipelineHandle) (PipelineObservation, error) {
	obs, err := a.inner.ObserveConstructionPipeline(fwra.Context{Context: ctx}, constructionpipeline.ParsePipelineHandle(handle.Name))
	if err != nil {
		return PipelineObservation{}, err
	}
	return PipelineObservation{
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
func designPipelinePhase(p constructionpipeline.PipelinePhase) PipelinePhase {
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

// PipelineSpec mirrors constructionPipelineAccess.md §3 (infrastructure-neutral),
// carrying ONLY the fields the design dispatch fills. DispatchInputs is the additive
// optional field (ADDED by C-MSD-Δ) that forwards the four DESIGN-job parameters;
// the RA stamps idempotency_token itself.
// TargetRepo + WorkflowFile are the additive PER-PROJECT-DESIGN-DISPATCH override:
// the design dispatch must target the PER-PROJECT repo (the user's repo, where
// aiarch-design.yml was committed at project birth) + the aiarch-design.yml workflow,
// NOT the central construction repo + aiarch-construct.yml. TargetRepo is the opaque
// per-project RepoRef String() (the rail's repoRef); empty ⇒ the RA falls back to the
// configured construction repo (the dormant-rail / non-git path is unchanged).
type PipelineSpec struct {
	ProjectID      ProjectID
	DispatchInputs map[string]string
	// TargetRepo is the opaque per-project RepoRef (sourcecontrol.RepoRef.String()).
	// Empty ⇒ the RA's configured construction repo (dormant-rail behavior).
	TargetRepo string
	// WorkflowFile is the per-project design workflow file (e.g. "aiarch-design.yml").
	// Empty ⇒ the RA's configured construction workflow file.
	WorkflowFile string
}

// PipelineHandle mirrors constructionPipelineAccess.md §3 — an opaque, immutable
// identity for one dispatched job; persisted across the Activity boundary as a plain
// string (the Manager never parses it).
type PipelineHandle struct {
	Name string
}

// IsZero reports whether no job is addressed.
func (h PipelineHandle) IsZero() bool { return h.Name == "" }

// PipelinePhase mirrors constructionPipelineAccess.md §3 — the infrastructure-neutral
// lifecycle phase the Manager branches on. The terminal trio drives the observe
// loop's exit + the failure path.
type PipelinePhase int

const (
	PipelinePhaseUnknown PipelinePhase = iota
	PipelinePending
	PipelineRunning
	PipelineSucceeded
	PipelineFailed
	PipelineCancelled
)

// IsTerminal reports whether the phase is one the job can no longer leave.
func (p PipelinePhase) IsTerminal() bool {
	switch p {
	case PipelineSucceeded, PipelineFailed, PipelineCancelled:
		return true
	default:
		return false
	}
}

// PipelineObservation mirrors constructionPipelineAccess.md §3 — a point-in-time,
// infrastructure-neutral view carrying the phase and (on terminal failure) a neutral
// Diagnostic summary (NOT a log firehose).
type PipelineObservation struct {
	Phase      PipelinePhase
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

// designBranch derives the per-artifact design SESSION branch the Action drafts +
// commits on (Manager owns branch naming — branch-per-activity). Deterministic from the
// project + kind + attempt so a within-attempt redraft reuses a stable branch, while a
// fresh REJECT attempt gets a NEW branch (attempt+1) — a merged/closed PR cannot be
// reused (I-DESIGN-DISPATCH §2b "sessionBranch"). attempt 0 omits the suffix so the
// first branch reads exactly as the original deterministic name.
func designBranch(projectID ProjectID, kind ArtifactKind, attempt int) string {
	base := fmt.Sprintf("aiarch-design/%s/%d-draft", projectID, int(kind))
	if attempt > 0 {
		return fmt.Sprintf("%s-a%d", base, attempt)
	}
	return base
}

// DispatchDesignJobArgs bundles the dispatch inputs for the Activity boundary. The
// Manager's SEQUENCE composed Prompt in-memory (prompts.go); ArtifactKind + Branch +
// PriorStateRef ride into the DispatchInputs map inside the Activity.
type DispatchDesignJobArgs struct {
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

// DispatchDesignJobActivity dispatches one claude-code-action DESIGN job via the
// FROZEN constructionPipelineAccess.SubmitConstructionPipeline verb and returns its
// opaque handle. The idempotency key is derived INSIDE this Activity body from
// activity.GetInfo (N1) so a redraft (a fresh ExecuteActivity invocation → new
// ActivityID) is a distinct, idempotent job, while a transient auto-retry of this one
// invocation (same ActivityID) collapses to the same job at the RA. The RA reserves +
// stamps idempotency_token; the Manager forwards only the four DESIGN parameters in
// DispatchInputs.
func (wf *Workflows) DispatchDesignJobActivity(ctx context.Context, a DispatchDesignJobArgs) (PipelineHandle, error) {
	key := activityIdempotencyKey(ctx)
	inputs := map[string]string{
		dispatchInputArtifactKind:  ArtifactKindString(a.ArtifactKind),
		dispatchInputDesignPrompt:  a.Prompt,
		dispatchInputTargetBranch:  a.TargetBranch,
		dispatchInputPriorStateRef: a.PriorStateRef,
	}
	// Per-project-design-dispatch: target the per-project repo + aiarch-design.yml when
	// the rail resolved a repo (TargetRepo non-empty), else leave both empty so the RA
	// falls back to the configured construction repo (dormant-rail / non-git path).
	spec := PipelineSpec{ProjectID: a.ProjectID, DispatchInputs: inputs}
	if a.TargetRepo != "" {
		spec.TargetRepo = a.TargetRepo
		spec.WorkflowFile = designWorkflowFileName
	}
	handle, err := wf.Pipeline.SubmitConstructionPipeline(ctx, spec, key)
	if err != nil {
		return PipelineHandle{}, fwmanager.MapError(err)
	}
	return handle, nil
}

// ObserveDesignJobActivity is a single point-in-time read of the dispatched job's
// phase (pull-shaped, side-effect-free; constructionPipelineAccess.md §2.2). The
// workflow loops it between durable timer waits until the observation is terminal.
func (wf *Workflows) ObserveDesignJobActivity(ctx context.Context, handle PipelineHandle) (PipelineObservation, error) {
	obs, err := wf.Pipeline.ObserveConstructionPipeline(ctx, handle)
	if err != nil {
		return PipelineObservation{}, fwmanager.MapError(err)
	}
	return obs, nil
}

// dispatchAndObserve runs ONE dispatch → observe round-trip: it dispatches the design
// job (wrapped in DispatchDesignJobActivity) and then polls ObserveDesignJobActivity
// between durable startTimer waits until the job reaches a TYPED terminal phase. It
// returns the terminal observation; the caller decides success (read-back) vs failure
// (the StageDraftFailed gate). It NEVER infers failure from a timeout-as-success
// (§0.5.4): a stuck job that never terminates within the bounded poll budget is
// surfaced as an explicit PipelineFailed with a neutral diagnostic, so the caller
// still lands the session at the human gate.
func (wf *Workflows) dispatchAndObserve(ctx workflow.Context, args DispatchDesignJobArgs) (PipelineObservation, error) {
	var handle PipelineHandle
	if err := workflow.ExecuteActivity(dispatchOpts(ctx), wf.DispatchDesignJobActivity, args).Get(ctx, &handle); err != nil {
		return PipelineObservation{}, err
	}
	if handle.IsZero() {
		return PipelineObservation{}, temporal.NewNonRetryableApplicationError(
			"dispatch returned an empty pipeline handle", "EmptyPipelineHandle", nil)
	}

	for poll := 0; poll < maxObservePolls; poll++ {
		var obs PipelineObservation
		if err := workflow.ExecuteActivity(observeOpts(ctx), wf.ObserveDesignJobActivity, handle).Get(ctx, &obs); err != nil {
			return PipelineObservation{}, err
		}
		if obs.Phase.IsTerminal() {
			return obs, nil
		}
		// Not yet terminal — space the next observe with a durable in-workflow timer.
		if err := workflow.Sleep(ctx, observePollInterval); err != nil {
			return PipelineObservation{}, err
		}
	}
	// Bounded poll budget exhausted without a terminal phase. Treat as an explicit
	// terminal failure (NOT a success, NOT a perpetual Drafting) so the caller routes
	// to the StageDraftFailed human gate.
	return PipelineObservation{
		Phase:      PipelineFailed,
		Diagnostic: "design job did not reach a terminal state within the observation window",
	}, nil
}

// dispatchOpts is the Activity option preset for the dispatch Activity. A transient
// submit error (ErrTransient / Retryable) auto-retries via this RetryPolicy; a
// terminal RA fault (ContractMisuse / Auth / QuotaExhausted) is non-retryable and
// surfaces to the workflow body. A PhaseFailed is NOT a dispatch error — it is a
// successful observation of a failed job, handled by the caller (§0.5.4).
func dispatchOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 5,
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.ContractMisuse),
				fwmanager.RAErrType(fwra.Auth),
				fwmanager.RAErrType(fwra.QuotaExhausted),
			},
		},
	})
}

// observeOpts is the Activity option preset for the observe read. Transient reads
// retry; a NotFound (GC'd handle) is non-retryable and surfaces.
func observeOpts(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			NonRetryableErrorTypes: []string{
				fwmanager.RAErrType(fwra.NotFound),
				fwmanager.RAErrType(fwra.ContractMisuse),
			},
		},
	})
}

// readBackCommittedModel reads the typed Phase-2 model the Action committed for kind
// via projectStateAccess.ReadProject (the read-back path, §0.5.2 step 5). The Action
// drafts + commits the JSON in the user's repo; aiarch reads it back as the staged
// draft. A missing / non-populated slot after a PhaseSucceeded is a contract
// violation between the Action and the read-back (the job claimed success but
// committed nothing) — surfaced as a terminal error routed to the gate, never a
// silent empty draft.
func (wf *Workflows) readBackCommittedModel(ctx workflow.Context, projectID ProjectID, kind ArtifactKind) (projectstate.ArtifactModel, error) {
	return wf.readBackCommittedModelOn(ctx, projectID, kind, "")
}

// readBackCommittedModelOn is readBackCommittedModel with an OPTIONAL branch override
// (I-DESIGN-DISPATCH §2a): the draft Action commits the typed JSON on the SESSION
// BRANCH, so the read-back reads that branch while the human reviews the not-yet-merged
// draft. branch=="" reads main (the dormant-rail / non-git behavior).
func (wf *Workflows) readBackCommittedModelOn(ctx workflow.Context, projectID ProjectID, kind ArtifactKind, branch string) (projectstate.ArtifactModel, error) {
	proj, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return nil, err
	}
	slot := slotFor(proj, toPSKind(kind))
	if slot.Model == nil {
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("design job reported success but committed no %s model to read back", toPSKind(kind)),
			"ReadBackEmpty", nil)
	}
	return slot.Model, nil
}
