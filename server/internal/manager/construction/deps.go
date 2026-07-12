package construction

import (
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// deps.go declares the hand-written domain VALUE types the Manager's workflow
// vocabulary uses. Per the founder DI model (2026-06-28) the constructionManager's
// GENERATED constructor (contract.gen.go: NewConstructionManager) takes the
// dependencies' PUBLISHED interfaces directly. The three Engines (handOff /
// intervention / review) are typed as their PUBLISHED contract interfaces DIRECTLY on
// wfDeps/workflows (workflow.go) — no Manager-local seam interface, no adapter (Task 6).
//
// B8 (custom activities → generated, clean cut) + its follow-up removed EVERY
// Manager-local RA seam that used to live here:
//   - constructionTransitionAccess (10 ops) and gitActivityStatusAccess (6 ops): every
//     write verb is reached through the GENERATED invoker surface (invokers.gen.go:
//     genInvokers.ConstructionTransition* / genInvokers.GitStatus*). wfDeps/workflows
//     carry no ConstructionTransition field; GitStatus survives as a plain
//     projectstate.GitActivityStatusAccess-typed field whose ONLY remaining role is
//     the nil-check "is the per-activity git head-state mirror wired" feature flag
//     (gitforward.go's gitEnabled/startedCred), never a direct method call.
//   - projectStateReader (the whole-aggregate read seam behind the last custom
//     Activity, ReadProjectActivity): GONE in the B8 follow-up. The shared
//     projectstate.ProjectEnvelope (envelope.go) was extended with the three
//     construction-fidelity sections the pump reads (ActivityConstruction /
//     ServiceContracts / ReviewPolicy), so the read now rides the GENERATED
//     designSessionAccess.readProjectOnBranch invoker with branch "" (main) and the
//     Manager's former local codec (codec.go) + activities_custom.go are deleted.
//     Construction now has NO custom Temporal Activities at all.
//
// How each dependency kind is reached differs by determinism class:
//   - the three Engines (handoff.HandOffEngine / intervention.InterventionEngine /
//     review.ReviewEngine) are PURE, deterministic, called DIRECTLY in-workflow (no
//     Activity wrapper — replay-safe) with fweng.Context{Context: context.Background()}
//     supplied inline at each call site (workflow.go / signals.go);
//   - the ResourceAccess ports are I/O and reached EXCLUSIVELY through the generated
//     invoker surface (Acts — invokers.gen.go/activities.gen.go).

// ===========================================================================
// handOffEngine — RETIRED (Task 6). The workflow calls the published
// handoff.HandOffEngine DIRECTLY (workflow.go), with fweng.Context{Context:
// context.Background()} supplied inline at the call site. ActivityKind (5 values,
// Unknown=0/DetailedDesign=1/Construction=2/Integration=3/Noncoding=4) and
// WorkerClass (5 values, Unknown=0/AIWorker=1/HumanSeniorWorker=2/HumanJuniorWorker=3/
// ArchitectOnly=4) are IDENTICAL, ordinal-for-ordinal, to the former Manager-local
// activityKind/workerClass — substituted directly, no converter. HandOffPolicy is
// likewise IDENTICAL (PreferAI, SeniorOnlyLayers) — substituted directly. The former
// identity maps handoffActivityKind/managerWorkerClass (adapters.go) are deleted.
//
// activityKind.String() (used by gitnaming.go's PR body text) has no equivalent
// method on the published handoff.ActivityKind (methods cannot be added to a type
// from another package) — replaced by the free function activityKindName
// (gitnaming.go), producing the IDENTICAL strings.
//
// workerClass.String() had no live caller (dead code) — retired with no replacement.
// ===========================================================================

// constructionActivity is the by-value activity snapshot the Manager's own workflow
// vocabulary uses broadly (eligibility.go, gitforward.go, dispatch) — CRLabel/IsRevert
// are the git-forward per-activity facts threaded into the PR open + the head-state
// mirror, and Phases is the resolved per-activity phase profile; none of these ride
// the handOffEngine call. Kind is typed DIRECTLY as the published handoff.ActivityKind
// (identity substitution — see the retirement note above). At the one handOffEngine
// call site (workflow.go), handoffActivityFromConstruction (adapters.go) narrows this
// broader struct onto the Engine's published handoff.ConstructionActivity — a REAL
// (if now-trivial) projection, since constructionActivity carries strictly MORE fields
// than the Engine needs, not an identity mirror to delete.
type constructionActivity struct {
	ActivityID   string
	Kind         handoff.ActivityKind
	ComponentID  string
	Layer        string
	EstimateDays float64
	CRLabel      string
	IsRevert     bool
	Phases       []projectstate.ActivityMethodPhase
}

// activityTypeName returns the canonical activity-type wire name
// ("service"/"frontend"/"testing") derived from the activity id. These are the exact
// keys the ReviewPolicy's GatedPhasesByType map is keyed by (and the keys the webApp
// PolicyPanel must emit) — the gate consults RequiresHuman(activityTypeName(), phase).
func (a constructionActivity) activityTypeName() string {
	return projectstate.DeriveType(a.ActivityID).String()
}

// ===========================================================================
// interventionEngine — RETIRED (Task 6). The workflow calls the published
// intervention.InterventionEngine DIRECTLY (workflow.go / signals.go), with
// fweng.Context{Context: context.Background()} supplied inline at each call site. The
// consumer-seam interface AND its local data mirrors (interventionMode + consts,
// interventionPolicy, constructionVariance, varianceKind + consts, varianceDirective +
// consts, pauseRequestContext, pausePlan) are retired:
//
//   - interventionMode/interventionPolicy were the Manager-local mirror
//     constructionInterventionPolicy (adapters.go) ALSO returned alongside the real
//     engPolicy (intervention.InterventionPolicy) fed to the retired interventionAdapter.
//     Neither the mirror value nor its Mode/RetryBudget/SLATier fields were EVER read
//     anywhere downstream (dead data threaded through wfDeps.InterventionPolicy) — the
//     field survives, retyped DIRECTLY to intervention.InterventionPolicy (the value the
//     retired adapter actually used), now genuinely read at each DecideOnVariance /
//     ApplyPausePolicy call site (workflow.go / signals.go) since there is no more
//     adapter closure to hold it.
//   - constructionVariance's Detail/OperatorSourced fields were likewise write-only —
//     populated at the single call site (workflow.go handleVariance) but never read by
//     the former converter (interventionVarianceKind only touched Kind). The struct is
//     retired outright; intervention.ConstructionVariance is now built inline at that
//     call site from the still-live inputs (ActivityID, Kind, AttemptCount, Policy),
//     preserving the historical adapter's ProjectID-from-ActivityID quirk verbatim.
//   - varianceKind (5 values) had only ONE live call site (variancePipelineFailed); the
//     other four consts were declared but never constructed anywhere (dead vocabulary).
//     The former converter interventionVarianceKind was a genuine MANY-TO-ONE fold (5
//     local values onto the published VarianceKind's 3), but since only
//     variancePipelineFailed was ever exercised — folding onto intervention.WorkerMiss —
//     the live call site substitutes intervention.WorkerMiss directly; the whole local
//     type + converter retire together with no behavior change.
//   - varianceDirective HAD an explicit directiveUnknown=0 zero-value sentinel the
//     published intervention.VarianceDirective does NOT carry (VarianceRetry=0 is its
//     zero value) — traced: the only place directiveUnknown could reach the workflow's
//     switch was the retired adapter's own error path, which the caller already
//     short-circuits on derr!=nil BEFORE the switch runs, so the switch's
//     `case directiveUnknown` was unreachable dead code. The workflow's switch now
//     matches {VarianceRetry, VarianceEscalate, VarianceTakeover} with a `default:`
//     catch-all for the same non-retryable rejection (identical to operations'
//     healthDirectiveUnknown retirement, Task 5).
//   - pauseRequestContext/pausePlan mirrored only the FIELDS actually read
//     (Reason/PipelinesToCancel/RecordPaused); NotifyTargets/ResumeHint were converted
//     by the retired adapter but never consumed downstream (dead reads). Substituted
//     directly with intervention.PauseRequestContext/PausePlan — PipelinesToCancel's
//     published element type ([]PipelineRef, a named string) is cast to string at the
//     one read site (signals.go), same as InFlightPipelines/ResumeHint simply staying
//     unread (zero value, unchanged behavior).
//
// The former identity maps interventionVarianceKind/managerVarianceDirective
// (adapters.go) are deleted; constructionInterventionPolicy (adapters.go) survives,
// retyped to return ONLY intervention.InterventionPolicy (the manager-mirror second
// return value dies with interventionPolicy).
// ===========================================================================

// ===========================================================================
// reviewEngine — RETIRED (Task 6). The workflow calls the published
// review.ReviewEngine DIRECTLY (workflow.go), with fweng.Context{Context:
// context.Background()} supplied inline at the call site. reviewChange was an EXACT
// 1:1 mirror of review.ReviewChange (ActivityID/ComponentID/ContentAddress,
// identical) — substituted directly, no converter. ReviewSet/Reviewer (contract.gen.go,
// this component's OWN generated public façade — off-limits) are NOT retired: they
// are a REAL divergence from review.ReviewSet/Reviewer (Reviewer.ReferenceArtifact is
// *string, optional, on the façade vs plain string on the Engine's own type), so the
// former reviewAdapter's conversion body survives as the free function
// reviewSetFromEngine (adapters.go).
// ===========================================================================

// ===========================================================================
// constructionPipeline value vocabulary — the Manager's infrastructure-neutral
// dispatch spec / handle / observation. The pipeline ops are GENERATED and reached
// through the generated invoker surface (genInvokers.Pipeline*); these neutral types
// feed the workflow-side composition/mapping helpers (workflow.go) that bridge to the
// contract constructionpipeline.PipelineSpec / PipelineHandle / PipelineObservation.
// ===========================================================================

// pipelineSpec is the Manager's infrastructure-neutral dispatch spec.
type pipelineSpec struct {
	ActivityID  string
	ComponentID string
	RepoURL     string
	Ref         string
	// Phase is the ActivityMethodPhase.String() for the current activity phase.
	Phase string
	// Role is the WorkerClass.String() for the assigned worker role.
	Role string
}

// pipelineHandle is the Manager's opaque handle.
type pipelineHandle struct {
	Name string
}

// pipelineObservation is the Manager's neutral pipeline observation.
type pipelineObservation struct {
	Phase      PipelinePhase
	Diagnostic string
}
