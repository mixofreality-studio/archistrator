package main

// This file holds the COMPOSITION-ROOT adapters that bridge the real engine /
// ResourceAccess packages to the constructionManager's consumer-mirror interfaces
// (internal/manager/construction/deps.go). Each real collaborator (handoff,
// intervention, review, constructionpipeline) was built against its OWN frozen
// contract with its OWN package-local types; the Manager declares narrow consumer
// mirrors of those same shapes (the Go "accept interfaces" idiom) so its test
// fakes stay small and the concrete types are adapted HERE, at the one place that
// knows both sides — never by editing deps.go or the engine/RA packages (those
// are frozen). The adaptations are mechanical value-copies between the mirrored
// enums/structs (their numeric orderings were deliberately aligned at design time,
// but we map by IDENTITY here, not by raw int, so a future re-order is safe).
//
// main.go is OUTSIDE internal/, so these adapters may freely import every concrete
// package; none of them imports Temporal (the Manager owns it).

import (
	"context"
	"sort"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// nextEligibleActivity is the Manager's pure next-activity selection over committed
// head-state — the pump's eligibility policy (constructionManager.md §6.3 step 1).
//
// An activity is eligible iff:
//  1. The activity itself is NotStarted (zero value in ActivityConstruction).
//  2. Every activity listed in its DependsOn has Phase == ActivityConstructionDone.
//
// Only Done deps satisfy a dependency — Running does not.
//
// Iteration order: the order of Dependencies in the committed Network (declaration
// order). This is stable, deterministic, and topology-friendly (activities are
// typically listed in topological order in the authored network). A sort by name is
// applied as a secondary key within a tie to keep the result fully deterministic
// even when multiple activities share the same declaration position.
//
// Guards: if either the Network or ActivityList slot is not committed (Status !=
// ReviewCommitted) or its Model cannot be cast to the expected type, the function
// returns false without panicking.
func nextEligibleActivity(proj projectstate.Project) (construction.ConstructionActivity, bool) {
	// Guard: Network slot must be committed and hold a *projectstate.Network.
	if proj.Network.Status != projectstate.ReviewCommitted {
		return construction.ConstructionActivity{}, false
	}
	network, ok := proj.Network.Model.(*projectstate.Network)
	if !ok || network == nil {
		return construction.ConstructionActivity{}, false
	}

	// Guard: ActivityList slot must be committed and hold a *projectstate.ActivityList.
	if proj.ActivityList.Status != projectstate.ReviewCommitted {
		return construction.ConstructionActivity{}, false
	}
	activityList, ok := proj.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || activityList == nil {
		return construction.ConstructionActivity{}, false
	}

	// Build a name→ActivityItem index for O(1) lookup when hydrating the result.
	itemByName := make(map[string]projectstate.ActivityItem, len(activityList.Activities))
	for _, item := range activityList.Activities {
		itemByName[item.Name] = item
	}

	// Build the predecessor map keyed by activity. An activity that appears ONLY in
	// other activities' dependsOn (a zero-predecessor ROOT — e.g. the D-*/R-*/N-* leaves
	// the network authors as pure prerequisites) has no dependency ROW, so it is absent
	// here and resolves to nil deps (vacuously eligible). The candidate universe is the
	// full ActivityList — NOT just the activities that have dependency rows — so those
	// ~28 roots are actually dispatched (the prior network.Dependencies-only iteration
	// never considered them, walling off everything downstream of them).
	depsByActivity := make(map[string][]string, len(network.Dependencies))
	for _, dep := range network.Dependencies {
		depsByActivity[dep.Activity] = dep.DependsOn
	}

	// Iterate the full activity universe in ActivityList declaration order. Collect
	// eligible candidates and return the first found (declaration-order priority, with a
	// name tie-break for full determinism).
	type candidate struct {
		declIdx  int
		activity string
	}
	var candidates []candidate
	for i, item := range activityList.Activities {
		name := item.Name
		if !isActivityNotStarted(name, proj.ActivityConstruction) {
			continue // already started or done — skip
		}
		if !allDepsDone(depsByActivity[name], proj.ActivityConstruction) {
			continue // at least one dep not yet done — blocked (nil deps ⇒ vacuously done)
		}
		candidates = append(candidates, candidate{declIdx: i, activity: name})
	}
	if len(candidates) == 0 {
		return construction.ConstructionActivity{}, false
	}
	// Stable sort: primary by declaration index (already in order), secondary by name.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].declIdx != candidates[j].declIdx {
			return candidates[i].declIdx < candidates[j].declIdx
		}
		return candidates[i].activity < candidates[j].activity
	})

	chosen := candidates[0].activity
	item := itemByName[chosen]
	return hydrateConstructionActivity(chosen, item), true
}

// isActivityNotStarted reports whether the given activity id is in the
// ActivityConstructionNotStarted phase (zero value — not yet in the map OR
// explicitly stored as NotStarted).
func isActivityNotStarted(activityID string, status map[string]projectstate.ActivityConstructionStatus) bool {
	if status == nil {
		return true // nil map means no activity has started yet
	}
	s, exists := status[activityID]
	if !exists {
		return true // absent entry == NotStarted (zero value)
	}
	return s.Phase == projectstate.ActivityConstructionNotStarted
}

// allDepsDone reports whether every dependency in deps has Phase == ActivityConstructionDone.
func allDepsDone(deps []string, status map[string]projectstate.ActivityConstructionStatus) bool {
	for _, dep := range deps {
		if status == nil {
			return false // any dep requires Done; nil map has no Done entries
		}
		s, exists := status[dep]
		if !exists || s.Phase != projectstate.ActivityConstructionDone {
			return false
		}
	}
	return true
}

// hydrateConstructionActivity populates a construction.ConstructionActivity from
// the network activity id and its matching ActivityList item. Fields that have no
// direct source in the ActivityList (ComponentID, Layer, CRLabel, IsRevert) are
// left at their zero values; the pump does not require them for dispatch (the
// handOffEngine reads Kind and EstimateDays to cast the worker class).
//
// Kind mapping: Coding=true → ActivityKindConstruction; Coding=false → ActivityKindNoncoding.
// This is the coarse two-valued mapping the ActivityList supports; a finer-grained
// Kind (DetailedDesign, Integration) would require an explicit field in ActivityItem.
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem) construction.ConstructionActivity {
	kind := construction.ActivityKindNoncoding
	if item.Coding {
		kind = construction.ActivityKindConstruction
	}
	return construction.ConstructionActivity{
		ActivityID: activityID,
		Kind:       kind,
		// ComponentID is the per-activity component the reviewEngine needs to assemble a
		// reviewer set (ProposeReviews rejects an EMPTY componentID). The ActivityList has
		// no component column, and the network activity id IS the per-component build
		// identity (name-as-identity), so the id doubles as the component id here — enough
		// for the engine's non-empty pre-condition + the dry-run reviewer fan-out.
		ComponentID:  activityID,
		EstimateDays: item.EffortDays,
		// Layer, CRLabel, IsRevert: zero values — not present in ActivityList.
	}
}

// ===========================================================================
// handOffEngine adapter — construction.HandOffEngine over handoff.HandOffEngine.
// ===========================================================================

type handoffAdapter struct{ inner handoff.HandOffEngine }

var _ construction.HandOffEngine = handoffAdapter{}

func (a handoffAdapter) PickWorkerClass(
	activity construction.ConstructionActivity,
	policy construction.HandOffPolicy,
) (construction.WorkerClass, error) {
	cls, err := a.inner.PickWorkerClass(
		handoff.ConstructionActivity{
			ActivityID:   activity.ActivityID,
			Kind:         handoffActivityKind(activity.Kind),
			ComponentID:  activity.ComponentID,
			Layer:        activity.Layer,
			EstimateDays: activity.EstimateDays,
		},
		handoff.HandOffPolicy{
			PreferAI:         policy.PreferAI,
			SeniorOnlyLayers: policy.SeniorOnlyLayers,
		},
	)
	if err != nil {
		// The engine's *fweng.Error flows back verbatim; the Manager maps it via
		// fwmanager.MapError at the call site (workflow.go).
		return construction.WorkerClassUnknown, err
	}
	return managerWorkerClass(cls), nil
}

func handoffActivityKind(k construction.ActivityKind) handoff.ActivityKind {
	switch k {
	case construction.ActivityKindDetailedDesign:
		return handoff.ActivityKindDetailedDesign
	case construction.ActivityKindConstruction:
		return handoff.ActivityKindConstruction
	case construction.ActivityKindIntegration:
		return handoff.ActivityKindIntegration
	case construction.ActivityKindNoncoding:
		return handoff.ActivityKindNoncoding
	default:
		return handoff.ActivityKindUnknown
	}
}

func managerWorkerClass(c handoff.WorkerClass) construction.WorkerClass {
	switch c {
	case handoff.AIWorker:
		return construction.AIWorker
	case handoff.HumanSeniorWorker:
		return construction.HumanSeniorWorker
	case handoff.HumanJuniorWorker:
		return construction.HumanJuniorWorker
	case handoff.ArchitectOnly:
		return construction.ArchitectOnly
	default:
		return construction.WorkerClassUnknown
	}
}

// ===========================================================================
// interventionEngine adapter — construction.InterventionEngine over
// intervention.InterventionEngine. The Manager's mirror exposes only the two ops
// it uses (DecideOnVariance / ApplyPausePolicy); the real Engine has four.
// ===========================================================================

type interventionAdapter struct {
	inner intervention.InterventionEngine
}

var _ construction.InterventionEngine = interventionAdapter{}

func (a interventionAdapter) DecideOnVariance(
	v construction.ConstructionVariance,
) (construction.VarianceDirective, error) {
	d, err := a.inner.DecideOnVariance(intervention.ConstructionVariance{
		// The Manager's mirror carries no ProjectID on the variance; the workflow
		// passes ActivityID + Kind + Detail. The Engine requires a non-empty
		// ProjectID, so we carry the ActivityID into both identity fields (the
		// Engine keys decisions on Kind/AttemptCount/Severity/Policy, not on the
		// id values themselves — they are only validated non-empty).
		ProjectID:  intervention.ProjectID(v.ActivityID),
		ActivityID: intervention.ActivityID(v.ActivityID),
		Kind:       interventionVarianceKind(v.Kind),
	})
	if err != nil {
		return construction.DirectiveUnknown, err
	}
	return managerVarianceDirective(d), nil
}

func (a interventionAdapter) ApplyPausePolicy(
	projectID string,
	ctx construction.PauseRequestContext,
) (construction.PausePlan, error) {
	plan, err := a.inner.ApplyPausePolicy(intervention.PauseRequestContext{
		ProjectID: intervention.ProjectID(projectID),
		Reason:    ctx.Reason,
	})
	if err != nil {
		return construction.PausePlan{}, err
	}
	cancels := make([]string, 0, len(plan.PipelinesToCancel))
	for _, p := range plan.PipelinesToCancel {
		cancels = append(cancels, string(p))
	}
	notify := make([]string, 0, len(plan.NotifyTargets))
	for _, n := range plan.NotifyTargets {
		notify = append(notify, string(n))
	}
	return construction.PausePlan{
		PipelinesToCancel: cancels,
		RecordPaused:      plan.RecordPaused,
		NotifyTargets:     notify,
	}, nil
}

func interventionVarianceKind(k construction.VarianceKind) intervention.VarianceKind {
	switch k {
	case construction.VarianceReviewFailed:
		return intervention.ReviewFailedUnresolvable
	case construction.VarianceWorkerRefused:
		return intervention.WorkerMiss
	case construction.VarianceScheduleOverrun:
		return intervention.EstimateOverrun
	case construction.VariancePipelineFailed:
		// A failed pipeline is a worker/build miss from the intervention Engine's
		// taxonomy (the closest of its three kinds — there is no dedicated
		// pipeline-failure variance kind on the Engine surface).
		return intervention.WorkerMiss
	case construction.VarianceOperatorOverride:
		// An operator-sourced override is surfaced as an estimate-overrun-class
		// variance for decision purposes; the operator's explicit steer overrides
		// the directive downstream (the Manager executes the override directly).
		return intervention.EstimateOverrun
	default:
		return intervention.VarianceKindUnknown
	}
}

func managerVarianceDirective(d intervention.VarianceDirective) construction.VarianceDirective {
	switch d {
	case intervention.VarianceRetry:
		return construction.DirectiveRetry
	case intervention.VarianceEscalate:
		return construction.DirectiveEscalate
	case intervention.VarianceTakeover:
		return construction.DirectiveTakeover
	default:
		return construction.DirectiveUnknown
	}
}

// ===========================================================================
// reviewEngine adapter — construction.ReviewEngine over review.ReviewEngine.
// ===========================================================================

type reviewAdapter struct{ inner review.ReviewEngine }

var _ construction.ReviewEngine = reviewAdapter{}

func (a reviewAdapter) ProposeReviews(
	change construction.ReviewChange,
	componentID string,
	artifactKind string,
	architectureGraph string,
	contracts []string,
) (construction.ReviewSet, error) {
	set, err := a.inner.ProposeReviews(
		review.ReviewChange{
			ActivityID:     change.ActivityID,
			ComponentID:    change.ComponentID,
			ContentAddress: change.ContentAddress,
		},
		componentID,
		artifactKind,
		architectureGraph,
		contracts,
	)
	if err != nil {
		return construction.ReviewSet{}, err
	}
	reviewers := make([]construction.Reviewer, 0, len(set.Reviewers))
	for _, r := range set.Reviewers {
		reviewers = append(reviewers, construction.Reviewer{
			Role:              r.Role,
			Perspective:       r.Perspective,
			ReferenceArtifact: r.ReferenceArtifact,
			MayAmend:          r.MayAmend,
		})
	}
	return construction.ReviewSet{Reviewers: reviewers}, nil
}

// ===========================================================================
// constructionPipelineAccess adapter — construction.ConstructionPipelineAccess
// over *constructionpipeline.Access. The Manager's mirror carries an
// infrastructure-neutral PipelineSpec/Handle/Observation distinct from the RA's
// own (richer) types; this bridges them.
// ===========================================================================

type pipelineAdapter struct{ inner *constructionpipeline.Access }

var _ construction.ConstructionPipelineAccess = pipelineAdapter{}

// pipelineToolchain is the logical toolchain the Manager's neutral PipelineSpec
// implies. The Manager's mirror carries no per-step toolchain/command (it submits
// "build this repo@ref"); the composition root supplies a single default build
// step here. The toolchain image map is configured in config.go and passed to
// constructionpipeline.New, so this logical ref resolves to a concrete image.
const pipelineDefaultToolchain = "go-1.23"

// dispatchInputsFor builds the DispatchInputs bag for a construction pipeline
// dispatch. The activity_id and component_id are the load-bearing keys the
// aiarch-construct.yml workflow reads to locate the service contract.
// phase and role (when non-empty) carry the Method phase + worker role so the
// phase-aware aiarch-phase.yml can branch its prompt correctly (REQ-2 + Plan 1 Task 6).
// Empty Phase/Role are omitted — the workflow uses its declared defaults.
func dispatchInputsFor(spec construction.PipelineSpec) map[string]string {
	m := map[string]string{
		"activity_id":  spec.ActivityID,
		"component_id": spec.ComponentID,
	}
	if spec.Phase != "" {
		m["phase"] = spec.Phase
	}
	if spec.Role != "" {
		m["role"] = spec.Role
	}
	return m
}

func (a pipelineAdapter) SubmitConstructionPipeline(
	ctx context.Context,
	spec construction.PipelineSpec,
	idempotencyKey fwra.IdempotencyKey,
) (construction.PipelineHandle, error) {
	handle, err := a.inner.SubmitConstructionPipeline(ctx, constructionpipeline.PipelineSpec{
		ActivityID: constructionpipeline.ConstructionActivityID(spec.ActivityID),
		Steps: []constructionpipeline.PipelineStep{{
			Name:      "build",
			Toolchain: constructionpipeline.ToolchainRef(pipelineDefaultToolchain),
			Command:   []string{"sh", "-c", "true"},
		}},
		WorkspaceRef:   constructionpipeline.ArtifactRef(spec.RepoURL + "@" + spec.Ref),
		DispatchInputs: dispatchInputsFor(spec),
	}, idempotencyKey)
	if err != nil {
		return construction.PipelineHandle{}, err
	}
	return construction.PipelineHandle{Name: handle.String()}, nil
}

func (a pipelineAdapter) ObserveConstructionPipeline(
	ctx context.Context,
	handle construction.PipelineHandle,
) (construction.PipelineObservation, error) {
	obs, err := a.inner.ObserveConstructionPipeline(ctx, constructionpipeline.HandleFromString(handle.Name))
	if err != nil {
		return construction.PipelineObservation{}, err
	}
	return construction.PipelineObservation{
		Phase:      managerPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
	}, nil
}

func (a pipelineAdapter) CancelConstructionPipeline(
	ctx context.Context,
	handle construction.PipelineHandle,
) error {
	return a.inner.CancelConstructionPipeline(ctx, constructionpipeline.HandleFromString(handle.Name))
}

func managerPipelinePhase(p constructionpipeline.PipelinePhase) construction.PipelinePhase {
	switch p {
	case constructionpipeline.PhasePending:
		return construction.PipelinePending
	case constructionpipeline.PhaseRunning:
		return construction.PipelineRunning
	case constructionpipeline.PhaseSucceeded:
		return construction.PipelineSucceeded
	case constructionpipeline.PhaseFailed, constructionpipeline.PhaseCancelled:
		return construction.PipelineFailed
	default:
		return construction.PipelinePhaseUnknown
	}
}

// ===========================================================================
// DESIGN-dispatch adapter — systemdesign.ConstructionPipelineAccess over
// *constructionpipeline.Access (the UC1 agentic-pivot, D-MSD-Δ). The same FROZEN
// constructionPipelineAccess RA backs BOTH the construction Manager (UC3) and the
// design Manager (UC1) — the design Manager is a NEW caller. This adapter bridges
// systemdesign's own neutral PipelineSpec/Handle/Observation (which carry the
// additive DispatchInputs and the PipelineCancelled phase) to the RA types,
// forwarding the four DESIGN-job inputs on PipelineSpec.DispatchInputs.
// ===========================================================================

type designPipelineAdapter struct{ inner *constructionpipeline.Access }

var _ systemdesign.ConstructionPipelineAccess = designPipelineAdapter{}

func (a designPipelineAdapter) SubmitConstructionPipeline(
	ctx context.Context,
	spec systemdesign.PipelineSpec,
	idempotencyKey fwra.IdempotencyKey,
) (systemdesign.PipelineHandle, error) {
	// Per-project-design-dispatch: decode the opaque per-project RepoRef → owner/repo so
	// the RA dispatches the agentic DESIGN job to the USER'S per-project repo +
	// aiarch-design.yml (NOT the central construction repo + aiarch-construct.yml). Empty
	// TargetRepo ⇒ zero RepoTarget ⇒ the RA falls back to the configured construction repo
	// (the dormant-rail / non-git path — UC3 untouched).
	target, terr := designRepoTarget(spec.TargetRepo)
	if terr != nil {
		return systemdesign.PipelineHandle{}, terr
	}
	handle, err := a.inner.SubmitConstructionPipeline(ctx, constructionpipeline.PipelineSpec{
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
	}, idempotencyKey)
	if err != nil {
		return systemdesign.PipelineHandle{}, err
	}
	return systemdesign.PipelineHandle{Name: handle.String()}, nil
}

// designRepoTarget decodes an opaque per-project RepoRef String() into the RA's
// infrastructure-neutral RepoTarget{Owner, Name} for the per-project-design-dispatch.
// An empty repoRef is the dormant-rail case → a zero RepoTarget (the RA falls back to
// the configured construction repo). A malformed ref is surfaced as the RA's
// ContractMisuse (the dispatch Activity maps it to a terminal error). This is the ONE
// composition-root seam that crosses the per-project RepoRef from sourceControlAccess
// to constructionPipelineAccess — using sourcecontrol's own OwnerRepo accessor so the
// RepoRef encoding stays owned by sourceControlAccess (no encoding leak here).
func designRepoTarget(repoRef string) (constructionpipeline.RepoTarget, error) {
	if repoRef == "" {
		return constructionpipeline.RepoTarget{}, nil
	}
	owner, name, err := sourcecontrol.RepoRefFromString(repoRef).OwnerRepo()
	if err != nil {
		return constructionpipeline.RepoTarget{}, err
	}
	return constructionpipeline.RepoTarget{Owner: owner, Name: name}, nil
}

func (a designPipelineAdapter) ObserveConstructionPipeline(
	ctx context.Context,
	handle systemdesign.PipelineHandle,
) (systemdesign.PipelineObservation, error) {
	obs, err := a.inner.ObserveConstructionPipeline(ctx, constructionpipeline.HandleFromString(handle.Name))
	if err != nil {
		return systemdesign.PipelineObservation{}, err
	}
	return systemdesign.PipelineObservation{
		Phase:      designPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
	}, nil
}

// designPipelinePhase maps the RA's phase to the systemdesign Manager's neutral
// phase, preserving the Cancelled terminal distinctly (the design Manager treats
// any non-Succeeded terminal as a StageDraftFailed gate).
func designPipelinePhase(p constructionpipeline.PipelinePhase) systemdesign.PipelinePhase {
	switch p {
	case constructionpipeline.PhasePending:
		return systemdesign.PipelinePending
	case constructionpipeline.PhaseRunning:
		return systemdesign.PipelineRunning
	case constructionpipeline.PhaseSucceeded:
		return systemdesign.PipelineSucceeded
	case constructionpipeline.PhaseFailed:
		return systemdesign.PipelineFailed
	case constructionpipeline.PhaseCancelled:
		return systemdesign.PipelineCancelled
	default:
		return systemdesign.PipelinePhaseUnknown
	}
}

// ===========================================================================
// DESIGN-dispatch adapter (Phase 2) — projectdesign.ConstructionPipelineAccess
// over *constructionpipeline.Access (the UC2 agentic-pivot, D-MPD-Δ — the twin of
// the systemdesign adapter above). The SAME FROZEN constructionPipelineAccess RA
// backs the construction Manager (UC3) AND both design Managers (UC1/UC2 — each a
// NEW caller). This adapter bridges projectdesign's own neutral PipelineSpec/Handle/
// Observation (which carry the additive DispatchInputs and the PipelineCancelled
// phase) to the RA types, forwarding the four Phase-2 DESIGN-job inputs on
// PipelineSpec.DispatchInputs.
// ===========================================================================

type designProjectDesignPipelineAdapter struct{ inner *constructionpipeline.Access }

var _ projectdesign.ConstructionPipelineAccess = designProjectDesignPipelineAdapter{}

func (a designProjectDesignPipelineAdapter) SubmitConstructionPipeline(
	ctx context.Context,
	spec projectdesign.PipelineSpec,
	idempotencyKey fwra.IdempotencyKey,
) (projectdesign.PipelineHandle, error) {
	// Per-project-design-dispatch (UC2 twin of the systemdesign adapter): decode the
	// opaque per-project RepoRef so the Phase-2 design job dispatches to the USER'S
	// per-project repo + aiarch-design.yml. Empty ⇒ the configured construction repo.
	target, terr := designRepoTarget(spec.TargetRepo)
	if terr != nil {
		return projectdesign.PipelineHandle{}, terr
	}
	handle, err := a.inner.SubmitConstructionPipeline(ctx, constructionpipeline.PipelineSpec{
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
	}, idempotencyKey)
	if err != nil {
		return projectdesign.PipelineHandle{}, err
	}
	return projectdesign.PipelineHandle{Name: handle.String()}, nil
}

func (a designProjectDesignPipelineAdapter) ObserveConstructionPipeline(
	ctx context.Context,
	handle projectdesign.PipelineHandle,
) (projectdesign.PipelineObservation, error) {
	obs, err := a.inner.ObserveConstructionPipeline(ctx, constructionpipeline.HandleFromString(handle.Name))
	if err != nil {
		return projectdesign.PipelineObservation{}, err
	}
	return projectdesign.PipelineObservation{
		Phase:      designProjectDesignPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
	}, nil
}

// designProjectDesignPipelinePhase maps the RA's phase to the projectdesign Manager's
// neutral phase, preserving the Cancelled terminal distinctly (the design Manager
// treats any non-Succeeded terminal as a StageDraftFailed gate).
func designProjectDesignPipelinePhase(p constructionpipeline.PipelinePhase) projectdesign.PipelinePhase {
	switch p {
	case constructionpipeline.PhasePending:
		return projectdesign.PipelinePending
	case constructionpipeline.PhaseRunning:
		return projectdesign.PipelineRunning
	case constructionpipeline.PhaseSucceeded:
		return projectdesign.PipelineSucceeded
	case constructionpipeline.PhaseFailed:
		return projectdesign.PipelineFailed
	case constructionpipeline.PhaseCancelled:
		return projectdesign.PipelineCancelled
	default:
		return projectdesign.PipelinePhaseUnknown
	}
}

// NOTE — durableExecutionAccess / schedule registration is intentionally NOT wired
// here. The constructionManager declares its DurableExecutionAccess consumer
// interface with an UNEXPORTED parameter type (construction.scheduleSpec, deps.go),
// so no external type — including a composition-root adapter — can satisfy it, and
// the manager's RegisterSchedules helper consequently cannot be called from
// cmd/server. This is a genuine frozen-contract SEAM GAP (flagged in
// implementation/log/C-MCN-reconcile.md): the nextActivity (30s) / replanSweep (5m)
// Temporal Schedules belong to the (unbuilt) schedulerClient, which will own the
// real durableexecution.Runtime and register the schedules at boot. The console
// renders correctly with empty/quiet sessions in the meantime (a live Argo cluster
// — R-CPR — is provisioned separately), so leaving the pump unwired is acceptable
// for this integration. See the TODO at the schedule wiring site in main.go.
