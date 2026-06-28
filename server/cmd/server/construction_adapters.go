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
	"log/slog"
	"sort"
	"strings"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
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
func nextEligibleActivity(proj projectstate.Project) (construction.ConstructionActivity, bool) { //nolint:gocognit,gocyclo // sequential eligibility guards + dependency walk; inherently branchy
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
	// Resolve the component id, preferring the activity's produced[] service-contract
	// hint over a fuzzy title match. produced lives on the per-activity construction
	// head-state record (proj.ActivityConstruction[chosen].Produced).
	var produced []projectstate.ProducedArtifact
	if proj.ActivityConstruction != nil {
		produced = proj.ActivityConstruction[chosen].Produced
	}
	component, ok := resolveComponentID(item.Title, produced, proj.ServiceContracts)
	if !ok {
		// No produced hint AND no fuzzy title match resolved to a real .serviceContracts
		// key. Dispatching the raw activity id would doom the run (contract extraction
		// fails downstream), so SKIP this activity this tick and log it. A quiet tick is
		// safe — a later tick (after a contract is recorded, or a manual fix) can dispatch.
		slog.Warn("construction pump: no service-contract key resolves for activity — skipping dispatch",
			"activityId", chosen, "title", item.Title)
		return construction.ConstructionActivity{}, false
	}
	return hydrateConstructionActivity(chosen, item, component), true
}

// resolveComponentID maps an activity to its service-contract component KEY, returning
// (key, true) only when it resolves to a REAL key present in the ServiceContracts
// corpus. Resolution order:
//
//  1. The activity's produced[] service-contract HINT — a ProducedArtifact whose Title
//     names the contract explicitly (e.g. "operatedRuntimeAccess — service contract").
//     This is authoritative: the seeded corpus tells us exactly which contract the
//     activity produces, so we never have to guess. The hinted name is normalized and
//     matched against the contract keys (exact-normalized match wins).
//  2. Fuzzy title match — the ActivityList has no component column, so the title
//     (e.g. "Build Operated Runtime Access") is the fallback link to the key
//     (operatedRuntimeAccess). The parenthetical is stripped first (it often names
//     OTHER components, e.g. "reuses sunk settlementManager skeleton") so it can't
//     steal the match; the longest normalized substring match wins.
//
// If NEITHER yields a key present in contracts, it returns ("", false) — the caller
// LOGS and SKIPS dispatch rather than dispatching a component_id that is not a contract
// key (a doomed run that fails contract extraction downstream).
func resolveComponentID(title string, produced []projectstate.ProducedArtifact, contracts map[string]projectstate.ServiceContract) (string, bool) {
	// 1. Produced service-contract hint (authoritative).
	for _, art := range produced {
		if art.Kind != "service-contract" {
			continue
		}
		if key, ok := matchContractKey(art.Title, contracts); ok {
			return key, true
		}
	}

	// 2. Fuzzy title match (longest normalized substring present in contracts).
	base := title
	if i := strings.IndexByte(base, '('); i >= 0 {
		base = base[:i]
	}
	n := normalizeIdent(base)
	best, bestLen := "", 0
	for comp := range contracts {
		cn := normalizeIdent(comp)
		if cn != "" && len(cn) > bestLen && strings.Contains(n, cn) {
			best, bestLen = comp, len(cn)
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// matchContractKey resolves a produced service-contract artifact title (e.g.
// "operatedRuntimeAccess — service contract") to a real ServiceContracts key. It
// prefers an exact normalized-equality match, then the longest normalized substring
// match, so the explicit contract name in the title wins over incidental overlaps.
func matchContractKey(title string, contracts map[string]projectstate.ServiceContract) (string, bool) {
	n := normalizeIdent(title)
	if n == "" {
		return "", false
	}
	best, bestLen := "", 0
	for comp := range contracts {
		cn := normalizeIdent(comp)
		if cn == "" {
			continue
		}
		if cn == n {
			return comp, true // exact normalized match — unambiguous.
		}
		if len(cn) > bestLen && strings.Contains(n, cn) {
			best, bestLen = comp, len(cn)
		}
	}
	if best != "" {
		return best, true
	}
	return "", false
}

// normalizeIdent lowercases s and keeps only [a-z0-9] so a human title and a
// camelCase component key compare on their letters alone.
func normalizeIdent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
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
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, componentID string) construction.ConstructionActivity {
	kind := construction.ActivityKindNoncoding
	if item.Coding {
		kind = construction.ActivityKindConstruction
	}
	return construction.ConstructionActivity{
		ActivityID: activityID,
		Kind:       kind,
		// ComponentID is the per-activity component, resolved from the activity Title
		// against the ServiceContracts corpus (resolveComponentID). It is the key the
		// aiarch-construct.yml workflow uses to look up the service contract in
		// project.json, and the reviewEngine needs it non-empty for ProposeReviews.
		// Falls back to the activity id when no contract matches.
		ComponentID:  componentID,
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
		fweng.Context{Context: context.Background()},
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
	// policy is the composition-supplied intervention regime the Manager feeds the
	// Engine by value. It replaces the hard-coded EscalateEverything: the default is
	// Tiered{RetryBudget:2} (autonomous retry, escalate after budget) with
	// EscalateEverything available for supervised mode. Set ONCE at wiring time.
	policy intervention.InterventionPolicy
}

var _ construction.InterventionEngine = interventionAdapter{}

func (a interventionAdapter) DecideOnVariance(
	v construction.ConstructionVariance,
) (construction.VarianceDirective, error) {
	d, err := a.inner.DecideOnVariance(fweng.Context{Context: context.Background()}, intervention.ConstructionVariance{
		// The Manager's mirror carries no ProjectID on the variance; the workflow
		// passes ActivityID + Kind + Detail. The Engine requires a non-empty
		// ProjectID, so we carry the ActivityID into both identity fields (the
		// Engine keys decisions on Kind/AttemptCount/Severity/Policy, not on the
		// id values themselves — they are only validated non-empty).
		ProjectID:  intervention.ProjectID(v.ActivityID),
		ActivityID: intervention.ActivityID(v.ActivityID),
		Kind:       interventionVarianceKind(v.Kind),
		// AttemptCount drives Tiered retry-budget exhaustion (the Manager threads its
		// supervision-loop attempt counter into the mirror's AttemptCount field).
		AttemptCount: int64(v.AttemptCount),
		// Composition-supplied regime (Tiered{RetryBudget:2} default). Without a
		// registered Mode the Engine rejects every variance with "unknown policy mode",
		// so the composition root guarantees a non-Unknown mode is set.
		Policy: a.policy,
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
	plan, err := a.inner.ApplyPausePolicy(fweng.Context{Context: context.Background()}, intervention.PauseRequestContext{
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

// constructionInterventionPolicy maps the configured intervention-mode string to the
// paired (engine, manager-mirror) intervention policies. Default Tiered{RetryBudget:2}
// (autonomous retry, escalate after budget); "escalate-everything" selects the
// supervised regime (every variance escalates). The two are derived together so the
// adapter (engine-facing) and the Manager mirror stay in lock-step.
func constructionInterventionPolicy(mode string) (intervention.InterventionPolicy, construction.InterventionPolicy) {
	switch mode {
	case "escalate-everything", "escalateEverything", "supervised":
		return intervention.InterventionPolicy{Mode: intervention.EscalateEverything},
			construction.InterventionPolicy{Mode: construction.InterventionModeEscalateEverything}
	default: // "tiered" / anything else → the ratified autonomous-retry default.
		return intervention.InterventionPolicy{Mode: intervention.Tiered, RetryBudget: 2},
			construction.InterventionPolicy{Mode: construction.InterventionModeTiered, RetryBudget: 2}
	}
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
	// Stopgap zero engine.Context — the constructionManager mirror doesn't carry a
	// context yet (added when the Manager layer is bootstrapped); the pure
	// reviewEngine ignores it.
	set, err := a.inner.ProposeReviews(
		fweng.Context{Context: context.Background()},
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
		cr := construction.Reviewer{
			Role:        r.Role,
			Perspective: r.Perspective,
			MayAmend:    r.MayAmend,
		}
		// ReferenceArtifact is a *string on the generated construction contract
		// (the ,omitempty → pointer rule); empty maps to nil (omitted on the wire).
		if r.ReferenceArtifact != "" {
			ref := r.ReferenceArtifact
			cr.ReferenceArtifact = &ref
		}
		reviewers = append(reviewers, cr)
	}
	return construction.ReviewSet{Reviewers: reviewers}, nil
}

// ===========================================================================
// constructionPipelineAccess adapter — construction.ConstructionPipelineAccess
// over the constructionpipeline.ConstructionPipelineAccess interface. The Manager's mirror carries an
// infrastructure-neutral PipelineSpec/Handle/Observation distinct from the RA's
// own (richer) types; this bridges them.
// ===========================================================================

type pipelineAdapter struct {
	inner constructionpipeline.ConstructionPipelineAccess
}

var _ construction.ConstructionPipelineAccess = pipelineAdapter{}

// pipelineToolchain is the logical toolchain the Manager's neutral PipelineSpec
// implies. The Manager's mirror carries no per-step toolchain/command (it submits
// "build this repo@ref"); the composition root supplies a single default build
// step here. The toolchain image map is configured in config.go and passed to
// NewGitHubActionsConstructionPipelineAccess, so this logical ref resolves to a concrete image.
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
	handle, err := a.inner.SubmitConstructionPipeline(fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey}, constructionpipeline.PipelineSpec{
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
		return construction.PipelineHandle{}, err
	}
	return construction.PipelineHandle{Name: constructionpipeline.PipelineHandleString(handle)}, nil
}

func (a pipelineAdapter) ObserveConstructionPipeline(
	ctx context.Context,
	handle construction.PipelineHandle,
) (construction.PipelineObservation, error) {
	obs, err := a.inner.ObserveConstructionPipeline(fwra.Context{Context: ctx}, constructionpipeline.ParsePipelineHandle(handle.Name))
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
	return a.inner.CancelConstructionPipeline(fwra.Context{Context: ctx}, constructionpipeline.ParsePipelineHandle(handle.Name))
}

func managerPipelinePhase(p constructionpipeline.PipelinePhase) construction.PipelinePhase {
	switch p {
	case constructionpipeline.PhasePending:
		return construction.PipelinePending
	case constructionpipeline.PhaseRunning:
		return construction.PipelineRunning
	case constructionpipeline.PhaseSucceeded:
		return construction.PipelineSucceeded
	case constructionpipeline.PhaseFailed:
		return construction.PipelineFailed
	case constructionpipeline.PhaseCancelled:
		// STOP flattening cancelled to failed — the Manager derives a distinct
		// FailureReason (PipelineCancelled) for head-state from this terminal.
		return construction.PipelineCancelled
	default:
		return construction.PipelinePhaseUnknown
	}
}

// ===========================================================================
// artifactAccess adapter — construction.ArtifactAccess over *artifact.Store. The
// concrete artifactAccess port now takes the ResourceAccess call Context (it embeds
// context.Context and carries the idempotency key); the Manager's narrow consumer
// seam still passes ctx + idempotencyKey explicitly. This bridges the two: it builds
// fwra.Context{Context, IdempotencyKey} at the boundary, exactly like the workerAccess
// seam adapter (the workerAccess bootstrap precedent). The Manager seam stays
// unchanged; only this composition-root adapter knows the rc-shaped port.
// ===========================================================================

type artifactAdapter struct{ inner artifact.ArtifactAccess }

var _ construction.ArtifactAccess = artifactAdapter{}

func (a artifactAdapter) StoreConstructionOutput(ctx context.Context, output artifact.ConstructionOutput, idempotencyKey fwra.IdempotencyKey) (string, error) {
	return a.inner.StoreConstructionOutput(fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey}, output)
}

func (a artifactAdapter) RetrieveConstructionOutput(ctx context.Context, contentAddress string) (artifact.ConstructionOutput, error) {
	return a.inner.RetrieveConstructionOutput(fwra.Context{Context: ctx}, contentAddress)
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
