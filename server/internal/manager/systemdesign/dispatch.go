package systemdesign

// dispatch.go is the AGENTIC-PIVOT seam (D-MSD-Δ, systemDesignManager.md §0d). The
// drafting MECHANISM flips from a synchronous workerAccess call to an ASYNC
// dispatch → observe → read-back round-trip:
//
//   - DISPATCH  the Manager composes the Method-role prompt IN-MEMORY (never
//               persisted) and dispatches a claude-code-action DESIGN job via the
//               FROZEN constructionPipelineAccess.SubmitConstructionPipeline verb,
//               carrying {artifact_kind, design_prompt, target_branch,
//               prior_state_ref} on the additive PipelineSpec.DispatchInputs field
//               (C-WF-DESIGN input schema). The RA reserves + stamps
//               idempotency_token itself; the Manager MUST NOT set it.
//   - OBSERVE   the Manager polls ObserveConstructionPipeline(handle) between
//               durableExecutionAccess timer waits until a TYPED terminal phase.
//   - READ-BACK on PhaseSucceeded the Manager reads the committed typed Kind via
//               projectStateAccess.ReadProject (the Action committed the JSON;
//               aiarch writes nothing on the draft path).
//
// The claude-code-action job runs OUTSIDE aiarch's call graph (the user's CI, the
// user's token). aiarch only dispatches it, observes it, and reads back its
// committed output — closed layering preserved, no RA→RA edge, no new edge type.
//
// THE IDEMPOTENCY KEY IS DERIVED INSIDE THE DISPATCH ACTIVITY (construction note
// N1). Temporal assigns a distinct ActivityID per ExecuteActivity invocation and
// reuses it across automatic retries of that one invocation. So a REDRAFT loop
// (a fresh ExecuteActivity(DispatchDesignJobActivity)) gets a new ActivityID → a
// distinct key → a fresh, idempotent job (NOT a dedup of the stale prior job);
// a transient auto-retry of a single dispatch keeps the ActivityID → same key →
// the FROZEN submit verb collapses it to the same handle.

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

// pipelineDefaultToolchain names the placeholder toolchain stamped on the design
// dispatch's logical step graph; the real design recipe lives in the user's
// aiarch-design.yml workflow file, so the step is only present to satisfy the RA's
// non-empty-step-graph pre-condition.
const pipelineDefaultToolchain = "go-1.23"

// ===========================================================================
// Internal dispatch seam — constructionPipelineAccess (UNEXPORTED). The manager
// depends on the PUBLISHED constructionpipeline.ConstructionPipelineAccess via its
// generated constructor (NewSystemDesignManager); this seam is the plain-ctx,
// Temporal-serializable PROJECTION of that dependency the hand-written Temporal
// activities consume and the tests fake. The production implementation is
// pipelineDispatchAdapter (below), which the package's RegisterWorker wraps around
// the published interface — folding the former composition-root designPipelineAdapter
// into the manager (Option-B boundary mapping). The former EXPORTED consumer-mirror
// interface is RETIRED: the only public dependency surface is the published interface
// on the constructor.
// ===========================================================================

// constructionPipelineAccess is the subset of the FROZEN constructionPipelineAccess
// surface (constructionPipelineAccess.md §2) the design draft path depends on:
// dispatch (submit) + observe. Cancel is available for a Withdraw-mid-flight path
// but is optional and not on this draft spine (§0d.5).
type constructionPipelineAccess interface {
	SubmitConstructionPipeline(ctx context.Context, spec pipelineSpec, idempotencyKey fwra.IdempotencyKey) (pipelineHandle, error)
	ObserveConstructionPipeline(ctx context.Context, handle pipelineHandle) (pipelineObservation, error)
}

// pipelineDispatchAdapter is the PRODUCTION constructionPipelineAccess: the folded
// composition-root designPipelineAdapter, now living in the manager. It bridges the
// manager's neutral PipelineSpec/Handle/Observation (which carry the additive
// DispatchInputs + the per-project-design-dispatch override + the PipelineCancelled
// phase) to the PUBLISHED constructionpipeline.ConstructionPipelineAccess the
// generated constructor was given — building the RA call Context at the boundary and
// forwarding the four DESIGN-job inputs on DispatchInputs.
type pipelineDispatchAdapter struct {
	inner constructionpipeline.ConstructionPipelineAccess
}

var _ constructionPipelineAccess = pipelineDispatchAdapter{}

func (a pipelineDispatchAdapter) SubmitConstructionPipeline(ctx context.Context, spec pipelineSpec, idempotencyKey fwra.IdempotencyKey) (pipelineHandle, error) {
	// Per-project-design-dispatch: decode the opaque per-project RepoRef → owner/repo so
	// the RA dispatches the agentic DESIGN job to the USER'S per-project repo +
	// aiarch-design.yml (NOT the central construction repo + aiarch-construct.yml). Empty
	// TargetRepo ⇒ zero RepoTarget ⇒ the RA falls back to the configured construction repo
	// (the dormant-rail / non-git path — UC3 untouched).
	target, terr := designRepoTarget(spec.TargetRepo)
	if terr != nil {
		return pipelineHandle{}, terr
	}
	handle, err := a.inner.SubmitConstructionPipeline(fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey}, constructionpipeline.PipelineSpec{
		ProjectID: constructionpipeline.ProjectID(spec.ProjectID),
		// A non-empty, well-formed step graph satisfies the RA's §2.1 pre-condition;
		// the design recipe lives in the user's aiarch-design.yml workflow file, so the
		// step is a logical placeholder. The DESIGN-job parameters ride on DispatchInputs.
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
		return pipelineHandle{}, err
	}
	return pipelineHandle{Name: constructionpipeline.PipelineHandleString(handle)}, nil
}

func (a pipelineDispatchAdapter) ObserveConstructionPipeline(ctx context.Context, handle pipelineHandle) (pipelineObservation, error) {
	obs, err := a.inner.ObserveConstructionPipeline(fwra.Context{Context: ctx}, constructionpipeline.ParsePipelineHandle(handle.Name))
	if err != nil {
		return pipelineObservation{}, err
	}
	return pipelineObservation{
		Phase:      designPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
	}, nil
}

// designRepoTarget decodes an opaque per-project RepoRef String() into the RA's
// infrastructure-neutral RepoTarget{Owner, Name} for the per-project-design-dispatch.
// An empty repoRef is the dormant-rail case → a zero RepoTarget (the RA falls back to
// the configured construction repo). A malformed ref surfaces as the RA's
// ContractMisuse (the dispatch Activity maps it to a terminal error). It uses
// sourcecontrol's own OwnerRepo accessor so the RepoRef encoding stays owned by
// sourceControlAccess (no encoding leak here).
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

// designPipelinePhase maps the RA's phase to the manager's neutral phase, preserving
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
		return lPipelinePhaseUnknown
	}
}

// pipelineSpec mirrors constructionPipelineAccess.md §3 (infrastructure-neutral),
// carrying ONLY the fields the design dispatch fills. DispatchInputs is the
// additive optional field (D-MSD-Δ Part 1) that forwards the four DESIGN-job
// parameters; the RA stamps idempotency_token itself.
//
// TargetRepo + WorkflowFile are the additive PER-PROJECT-DESIGN-DISPATCH override
// (sibling to DispatchInputs): the design dispatch must target the PER-PROJECT repo
// (the user's repo, where aiarch-design.yml was committed at project birth) + the
// aiarch-design.yml workflow, NOT the central construction repo + aiarch-construct.yml.
// TargetRepo is the opaque per-project RepoRef String() (the rail's repoRef); empty ⇒
// the RA falls back to the configured construction repo (the dormant-rail / non-git
// path is unchanged). WorkflowFile is the design workflow file name (DesignWorkflowPath
// basename); empty ⇒ aiarch-construct.yml.
type pipelineSpec struct {
	ProjectID      ProjectID
	DispatchInputs map[string]string
	// TargetRepo is the opaque per-project RepoRef (sourcecontrol.RepoRef.String()).
	// Empty ⇒ the RA's configured construction repo (dormant-rail behavior).
	TargetRepo string
	// WorkflowFile is the per-project design workflow file (e.g. "aiarch-design.yml").
	// Empty ⇒ the RA's configured construction workflow file.
	WorkflowFile string
}

// pipelineHandle mirrors constructionPipelineAccess.md §3 — an opaque, immutable
// identity for one dispatched job; persisted across the Activity boundary as a
// plain string (the Manager never parses it).
type pipelineHandle struct {
	Name string
}

// IsZero reports whether no job is addressed.
func (h pipelineHandle) IsZero() bool { return h.Name == "" }

// pipelinePhase mirrors constructionPipelineAccess.md §3 — the infrastructure-
// neutral lifecycle phase the Manager branches on. The terminal trio drives the
// observe loop's exit + the failure path.
type pipelinePhase int

const (
	lPipelinePhaseUnknown pipelinePhase = iota
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
	default:
		return false
	}
}

// pipelineObservation mirrors constructionPipelineAccess.md §3 — a point-in-time,
// infrastructure-neutral view carrying the phase and (on terminal failure) a
// neutral Diagnostic summary (NOT a log firehose).
type pipelineObservation struct {
	Phase      pipelinePhase
	Diagnostic string
}

// ===========================================================================
// Dispatch inputs (C-WF-DESIGN workflow_dispatch schema). These exact key names
// are the binding contract with aiarch-design.yml's workflow_dispatch.inputs.
// idempotency_token is RA-controlled and is NOT set here.
// ===========================================================================

const (
	dispatchInputArtifactKind  = "artifact_kind"
	dispatchInputDesignPrompt  = "design_prompt"
	dispatchInputTargetBranch  = "target_branch"
	dispatchInputPriorStateRef = "prior_state_ref"
	// dispatchInputJobMode discriminates a DRAFT job (the Action commits the typed
	// Kind model into the slot) from a CRITIQUE job (the Action commits the slot's
	// critiqueVerdict / critiqueNotes read-back carrier — D-MSD-Δ amendment). The
	// Action template branches its commit-target instruction on this value. Defaulted
	// to "draft" in the template so a job dispatched without it (e.g. a UC2 draft)
	// behaves exactly as before.
	dispatchInputJobMode = "job_mode"
)

// Job-mode dispatch values. These exact strings are a contract with the
// aiarch-design.yml template's job_mode input.
const (
	jobModeDraft    = "draft"
	jobModeCritique = "critique"
)

// jobModeFor maps a DispatchTarget to its job_mode dispatch value.
func jobModeFor(target dispatchTarget) string {
	if target == dispatchTargetCritique {
		return jobModeCritique
	}
	return jobModeDraft
}

// dispatchTarget discriminates which Method-role agentic job the dispatch round-
// trip produces: an architect/PM DRAFT of the artifact, or a PM CRITIQUE of the
// just-committed draft. Both are dispatch → observe → read-back round-trips; only
// the prompt role + the read-back differ.
type dispatchTarget int

const (
	dispatchTargetDraft    dispatchTarget = iota // draft the artifact named by ArtifactKind
	dispatchTargetCritique                       // PM-critique the just-committed draft
)

// observePollInterval spaces the observe-poll loop's durable timer waits. A
// design job runs minutes in the user's CI; this is the in-workflow timer the
// contract prescribes (§0d.2 step 4). Kept modest so the test's time-skipping env
// settles quickly.
const observePollInterval = 15 * time.Second

// maxObservePolls bounds the observe loop so a stuck (never-terminal) job cannot
// spin forever; exceeding it is treated as a terminal infrastructure failure and
// routed to the human gate (never a perpetual Drafting — the anti-wedge rule).
const maxObservePolls = 240 // 240 * 15s = 1h ceiling

// designBranch derives the per-artifact design SESSION branch the Action drafts +
// commits on (Manager owns branch naming — branch-per-activity). Deterministic from
// the project + kind + target + attempt so a within-attempt redraft (PM-revise / a
// dispatch auto-retry) reuses a stable, human-legible branch, while a fresh REJECT
// attempt gets a NEW branch (attempt+1) — a merged/closed PR cannot be reused
// (I-DESIGN-DISPATCH §2b "sessionBranch"). attempt 0 omits the suffix so the first
// branch reads exactly as the original deterministic name.
func designBranch(projectID ProjectID, kind ArtifactKind, target dispatchTarget, attempt int) string {
	suffix := "draft"
	if target == dispatchTargetCritique {
		suffix = "critique"
	}
	base := fmt.Sprintf("aiarch-design/%s/%d-%s", projectID, int(kind), suffix)
	if attempt > 0 {
		return fmt.Sprintf("%s-a%d", base, attempt)
	}
	return base
}

// dispatchDesignJobArgs bundles the dispatch inputs for the Activity boundary. The
// Manager's SEQUENCE composed Prompt in-memory (prompts.go); ArtifactKind + Target
// + Branch + PriorStateRef ride into the DispatchInputs map inside the Activity.
type dispatchDesignJobArgs struct {
	ProjectID     ProjectID
	ArtifactKind  ArtifactKind
	Target        dispatchTarget
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
// ActivityID) is a distinct, idempotent job, while a transient auto-retry of this
// one invocation (same ActivityID) collapses to the same job at the RA. The RA
// reserves + stamps idempotency_token; the Manager forwards only the four DESIGN
// parameters in DispatchInputs.
func (wf *workflows) DispatchDesignJobActivity(ctx context.Context, a dispatchDesignJobArgs) (pipelineHandle, error) {
	key := activityIdempotencyKey(ctx)
	inputs := map[string]string{
		dispatchInputArtifactKind:  artifactKindString(a.ArtifactKind),
		dispatchInputDesignPrompt:  a.Prompt,
		dispatchInputTargetBranch:  a.TargetBranch,
		dispatchInputPriorStateRef: a.PriorStateRef,
		dispatchInputJobMode:       jobModeFor(a.Target),
	}
	// Per-project-design-dispatch: target the per-project repo + aiarch-design.yml when
	// the rail resolved a repo (TargetRepo non-empty), else leave both empty so the RA
	// falls back to the configured construction repo (dormant-rail / non-git path).
	spec := pipelineSpec{ProjectID: a.ProjectID, DispatchInputs: inputs}
	if a.TargetRepo != "" {
		spec.TargetRepo = a.TargetRepo
		spec.WorkflowFile = designWorkflowFileName
	}
	handle, err := wf.Pipeline.SubmitConstructionPipeline(ctx, spec, key)
	if err != nil {
		return pipelineHandle{}, fwmanager.MapError(err)
	}
	return handle, nil
}

// ObserveDesignJobActivity is a single point-in-time read of the dispatched job's
// phase (pull-shaped, side-effect-free; constructionPipelineAccess.md §2.2). The
// workflow loops it between durable timer waits until the observation is terminal.
func (wf *workflows) ObserveDesignJobActivity(ctx context.Context, handle pipelineHandle) (pipelineObservation, error) {
	obs, err := wf.Pipeline.ObserveConstructionPipeline(ctx, handle)
	if err != nil {
		return pipelineObservation{}, fwmanager.MapError(err)
	}
	return obs, nil
}

// dispatchAndObserve runs ONE dispatch → observe round-trip: it dispatches the
// design job (wrapped in DispatchDesignJobActivity) and then polls
// ObserveDesignJobActivity between durable startTimer waits until the job reaches
// a TYPED terminal phase. It returns the terminal observation; the caller decides
// success (read-back) vs failure (the StageDraftFailed gate). It NEVER infers
// failure from a timeout-as-success (§0d.4): a stuck job that never terminates
// within the bounded poll budget is surfaced as an explicit PipelineFailed with a
// neutral diagnostic, so the caller still lands the session at the human gate.
func (wf *workflows) dispatchAndObserve(ctx workflow.Context, args dispatchDesignJobArgs) (pipelineObservation, error) {
	var handle pipelineHandle
	if err := workflow.ExecuteActivity(dispatchOpts(ctx), wf.DispatchDesignJobActivity, args).Get(ctx, &handle); err != nil {
		return pipelineObservation{}, err
	}
	if handle.IsZero() {
		return pipelineObservation{}, temporal.NewNonRetryableApplicationError(
			"dispatch returned an empty pipeline handle", "EmptyPipelineHandle", nil)
	}

	for poll := 0; poll < maxObservePolls; poll++ {
		var obs pipelineObservation
		if err := workflow.ExecuteActivity(observeOpts(ctx), wf.ObserveDesignJobActivity, handle).Get(ctx, &obs); err != nil {
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

// dispatchOpts is the Activity option preset for the dispatch Activity. A transient
// submit error (ErrTransient / Retryable) auto-retries via this RetryPolicy; a
// terminal RA fault (ContractMisuse / Auth / QuotaExhausted) is non-retryable and
// surfaces to the workflow body. A PhaseFailed is NOT a dispatch error — it is a
// successful observation of a failed job, handled by the caller (§0d.4).
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

// readBackCritique reads back the PM-critique verdict the critique Action produced,
// via projectStateAccess.ReadProject of the Kind slot (§0d.2 step 6 — "steps 2–5
// with the PM-role prompt … the Manager reads back"). The critique job runs over
// the just-committed draft; on CritiqueRevise the Action records its revision
// guidance, on CritiqueApprove it ratifies the draft unchanged.
//
// RATIFIED D-MSD-Δ amendment (2026-06-15): the read-back uses the FIRST-CLASS
// optional ArtifactSlot.CritiqueVerdict / CritiqueNotes carrier (artifactmodel.go),
// NOT the frozen ArtifactSlot.Notes field. The senior review of C-MSD-Δ escalated
// the prior Notes-overload as a genuine contract-design gap: Notes carries the
// architect's reject/withdraw rationale (a distinct writer), so a PM-kind reject
// loop (RejectArtifact writes slot.Notes; then draft→critique→readBackCritique with
// NO intervening Stage) would misread the reject notes as the PM verdict, and
// "empty Notes = approve" cannot represent a legit empty-notes revise. The
// dedicated carrier is the single read-back location, written ONLY by the critique
// Action and cleared by every stage/status-transition verb, so no collision and no
// ambiguity remain.
//
// SAFE DEFAULT — missing verdict is a DRAFT FAILURE, not a silent approve. After a
// critique dispatch reached PhaseSucceeded, the Action is contractually obligated to
// have committed an explicit CritiqueVerdict ("approve" | "revise"). An EMPTY verdict
// means the job claimed success but committed no verdict — a contract violation
// between the Action and the read-back, exactly like readBackCommittedModel's empty-
// model case. We surface it as a terminal error (routed to the StageDraftFailed human
// gate by the caller), NEVER a silent CritiqueApprove. Justification: a silent approve
// on a missing verdict would let an unreviewed (or half-failed) draft sail to the human
// gate as if the PM ratified it — the worse failure mode. Treating it as a draft
// failure keeps the human in the loop with a clear "retry/withdraw" affordance and is
// consistent with the anti-wedge discipline (a ran-but-incomplete job is terminal-at-
// the-Manager, escalated to the human, not absorbed).
func (wf *workflows) readBackCritique(ctx workflow.Context, projectID ProjectID, kind ArtifactKind) (critique, error) {
	return wf.readBackCritiqueOn(ctx, projectID, kind, "")
}

// readBackCritiqueOn is readBackCritique with an OPTIONAL branch override (§2a): the
// PM-critique Action commits its verdict carrier on the critique session branch, so the
// read-back reads that branch when the rail is enabled. branch=="" reads main (the
// dormant-rail / non-git behavior).
func (wf *workflows) readBackCritiqueOn(ctx workflow.Context, projectID ProjectID, kind ArtifactKind, branch string) (critique, error) {
	proj, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return critique{}, err
	}
	slot := slotFor(proj, kind)
	switch slot.CritiqueVerdict {
	case projectstate.CritiqueVerdictApprove:
		return critique{Verdict: critiqueApprove}, nil
	case projectstate.CritiqueVerdictRevise:
		return critique{Verdict: critiqueRevise, Notes: slot.CritiqueNotes}, nil
	default:
		// Empty / unknown verdict after a PhaseSucceeded critique job: the safe default
		// is a draft failure, not a silent approve (see the doc comment's justification).
		return critique{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("critique job reported success but committed no critique verdict for %s (read-back carrier empty)", artifactKindString(kind)),
			"CritiqueReadBackEmpty", nil)
	}
}

// readBackCommittedModel reads the typed model the Action committed for kind via
// projectStateAccess.ReadProject (the read-back path, §0d.2 step 5). The Action
// drafts + commits the JSON in the user's repo; aiarch reads it back as the staged
// draft. A missing / non-populated slot after a PhaseSucceeded is a contract
// violation between the Action and the read-back (the job claimed success but
// committed nothing) — surfaced as a terminal error routed to the gate, never a
// silent empty draft.
func (wf *workflows) readBackCommittedModel(ctx workflow.Context, projectID ProjectID, kind ArtifactKind) (projectstate.ArtifactModel, error) {
	return wf.readBackCommittedModelOn(ctx, projectID, kind, "")
}

// readBackCommittedModelOn is readBackCommittedModel with an OPTIONAL branch override
// (§2a): the draft Action commits the typed JSON on the SESSION BRANCH, so the read-back
// reads that branch while the human reviews the not-yet-merged draft. branch=="" reads
// main (the dormant-rail / non-git behavior).
func (wf *workflows) readBackCommittedModelOn(ctx workflow.Context, projectID ProjectID, kind ArtifactKind, branch string) (projectstate.ArtifactModel, error) {
	proj, err := wf.readProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return nil, err
	}
	slot := slotFor(proj, kind)
	if slot.Model == nil {
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("design job reported success but committed no %s model to read back", artifactKindString(kind)),
			"ReadBackEmpty", nil)
	}
	return slot.Model, nil
}
