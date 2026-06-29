package construction

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// engine / ResourceAccess interfaces (the dependencies the GENERATED constructor
// NewConstructionManager receives) to the Manager's unexported downstream seams
// (deps.go). Per the founder DI model (2026-06-28) these were retired from cmd/server
// and live HERE, in the one package that knows both sides — the Manager depends on
// each dependency's PUBLISHED interface and adapts it internally (Option-B boundary
// mapping), exactly as systemdesign/projectdesign fold their rail/dispatch adapters.
//
// None of these imports Temporal (the Manager owns it); they are plain value-copy
// bridges run inside the Manager's Activities (RA seams) or directly in-workflow
// (Engine seams). The mechanical enum/struct copies map by IDENTITY, not raw int, so
// a future re-order is safe.

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
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/constructionpipeline"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	workeraccess "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/worker"
)

// ===========================================================================
// nextEligibleActivity — the pump's pure eligibility selection over committed
// head-state (constructionManager.md §6.3 step 1).
// ===========================================================================

// nextEligibleActivity resolves the next eligible construction activity for a project
// from its head-state. An activity is eligible iff it is NotStarted and every dep is
// Done. Iteration is ActivityList declaration order with a name tie-break.
func nextEligibleActivity(proj projectstate.Project) (constructionActivity, bool) { //nolint:gocognit,gocyclo // sequential eligibility guards + dependency walk; inherently branchy
	if proj.Network.Status != projectstate.ReviewCommitted {
		return constructionActivity{}, false
	}
	network, ok := proj.Network.Model.(*projectstate.Network)
	if !ok || network == nil {
		return constructionActivity{}, false
	}
	if proj.ActivityList.Status != projectstate.ReviewCommitted {
		return constructionActivity{}, false
	}
	activityList, ok := proj.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || activityList == nil {
		return constructionActivity{}, false
	}

	itemByName := make(map[string]projectstate.ActivityItem, len(activityList.Activities))
	for _, item := range activityList.Activities {
		itemByName[item.Name] = item
	}

	depsByActivity := make(map[string][]string, len(network.Dependencies))
	for _, dep := range network.Dependencies {
		depsByActivity[dep.Activity] = dep.DependsOn
	}

	type candidate struct {
		declIdx  int
		activity string
	}
	var candidates []candidate
	for i, item := range activityList.Activities {
		name := item.Name
		if !isActivityNotStarted(name, proj.ActivityConstruction) {
			continue
		}
		if !allDepsDone(depsByActivity[name], proj.ActivityConstruction) {
			continue
		}
		candidates = append(candidates, candidate{declIdx: i, activity: name})
	}
	if len(candidates) == 0 {
		return constructionActivity{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].declIdx != candidates[j].declIdx {
			return candidates[i].declIdx < candidates[j].declIdx
		}
		return candidates[i].activity < candidates[j].activity
	})

	chosen := candidates[0].activity
	item := itemByName[chosen]
	var produced []projectstate.ProducedArtifact
	if proj.ActivityConstruction != nil {
		produced = proj.ActivityConstruction[chosen].Produced
	}
	component, ok := resolveComponentID(item.Title, produced, proj.ServiceContracts)
	if !ok {
		slog.Warn("construction pump: no service-contract key resolves for activity — skipping dispatch",
			"activityId", chosen, "title", item.Title)
		return constructionActivity{}, false
	}
	return hydrateConstructionActivity(chosen, item, component), true
}

// resolveComponentID maps an activity to its service-contract component KEY.
func resolveComponentID(title string, produced []projectstate.ProducedArtifact, contracts map[string]projectstate.ServiceContract) (string, bool) {
	for _, art := range produced {
		if art.Kind != "service-contract" {
			continue
		}
		if key, ok := matchContractKey(art.Title, contracts); ok {
			return key, true
		}
	}

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

// matchContractKey resolves a produced service-contract artifact title to a real key.
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
			return comp, true
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

// normalizeIdent lowercases s and keeps only [a-z0-9].
func normalizeIdent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isActivityNotStarted reports whether the activity is in the NotStarted phase.
func isActivityNotStarted(activityID string, status map[string]projectstate.ActivityConstructionStatus) bool {
	if status == nil {
		return true
	}
	s, exists := status[activityID]
	if !exists {
		return true
	}
	return s.Phase == projectstate.ActivityConstructionNotStarted
}

// allDepsDone reports whether every dependency has Phase == Done.
func allDepsDone(deps []string, status map[string]projectstate.ActivityConstructionStatus) bool {
	for _, dep := range deps {
		if status == nil {
			return false
		}
		s, exists := status[dep]
		if !exists || s.Phase != projectstate.ActivityConstructionDone {
			return false
		}
	}
	return true
}

// hydrateConstructionActivity populates a constructionActivity from the activity id +
// its ActivityList item. Coding=true → Construction; Coding=false → Noncoding.
func hydrateConstructionActivity(activityID string, item projectstate.ActivityItem, componentID string) constructionActivity {
	kind := activityKindNoncoding
	if item.Coding {
		kind = activityKindConstruction
	}
	return constructionActivity{
		ActivityID:   activityID,
		Kind:         kind,
		ComponentID:  componentID,
		EstimateDays: item.EffortDays,
	}
}

// ===========================================================================
// handOffEngine adapter — handOffEngine seam over handoff.HandOffEngine.
// ===========================================================================

type handoffAdapter struct{ inner handoff.HandOffEngine }

var _ handOffEngine = handoffAdapter{}

func (a handoffAdapter) PickWorkerClass(activity constructionActivity, policy handOffPolicy) (workerClass, error) {
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
		return workerClassUnknown, err
	}
	return managerWorkerClass(cls), nil
}

func handoffActivityKind(k activityKind) handoff.ActivityKind {
	switch k {
	case activityKindDetailedDesign:
		return handoff.ActivityKindDetailedDesign
	case activityKindConstruction:
		return handoff.ActivityKindConstruction
	case activityKindIntegration:
		return handoff.ActivityKindIntegration
	case activityKindNoncoding:
		return handoff.ActivityKindNoncoding
	default:
		return handoff.ActivityKindUnknown
	}
}

func managerWorkerClass(c handoff.WorkerClass) workerClass {
	switch c {
	case handoff.AIWorker:
		return aiWorker
	case handoff.HumanSeniorWorker:
		return humanSeniorWorker
	case handoff.HumanJuniorWorker:
		return humanJuniorWorker
	case handoff.ArchitectOnly:
		return architectOnly
	default:
		return workerClassUnknown
	}
}

// ===========================================================================
// interventionEngine adapter — interventionEngine seam over
// intervention.InterventionEngine + the composition-supplied regime.
// ===========================================================================

type interventionAdapter struct {
	inner  intervention.InterventionEngine
	policy intervention.InterventionPolicy
}

var _ interventionEngine = interventionAdapter{}

func (a interventionAdapter) DecideOnVariance(v constructionVariance) (varianceDirective, error) {
	d, err := a.inner.DecideOnVariance(fweng.Context{Context: context.Background()}, intervention.ConstructionVariance{
		ProjectID:    intervention.ProjectID(v.ActivityID),
		ActivityID:   intervention.ActivityID(v.ActivityID),
		Kind:         interventionVarianceKind(v.Kind),
		AttemptCount: int64(v.AttemptCount),
		Policy:       a.policy,
	})
	if err != nil {
		return directiveUnknown, err
	}
	return managerVarianceDirective(d), nil
}

func (a interventionAdapter) ApplyPausePolicy(projectID string, ctx pauseRequestContext) (pausePlan, error) {
	plan, err := a.inner.ApplyPausePolicy(fweng.Context{Context: context.Background()}, intervention.PauseRequestContext{
		ProjectID: intervention.ProjectID(projectID),
		Reason:    ctx.Reason,
	})
	if err != nil {
		return pausePlan{}, err
	}
	cancels := make([]string, 0, len(plan.PipelinesToCancel))
	for _, p := range plan.PipelinesToCancel {
		cancels = append(cancels, string(p))
	}
	notify := make([]string, 0, len(plan.NotifyTargets))
	for _, n := range plan.NotifyTargets {
		notify = append(notify, string(n))
	}
	return pausePlan{
		PipelinesToCancel: cancels,
		RecordPaused:      plan.RecordPaused,
		NotifyTargets:     notify,
	}, nil
}

// constructionInterventionPolicy maps the configured intervention-mode string to the
// paired (engine, manager-mirror) intervention policies.
func constructionInterventionPolicy(mode string) (intervention.InterventionPolicy, interventionPolicy) {
	switch mode {
	case "escalate-everything", "escalateEverything", "supervised":
		return intervention.InterventionPolicy{Mode: intervention.EscalateEverything},
			interventionPolicy{Mode: interventionModeEscalateEverything}
	default:
		return intervention.InterventionPolicy{Mode: intervention.Tiered, RetryBudget: 2},
			interventionPolicy{Mode: interventionModeTiered, RetryBudget: 2}
	}
}

func interventionVarianceKind(k varianceKind) intervention.VarianceKind {
	switch k {
	case varianceReviewFailed:
		return intervention.ReviewFailedUnresolvable
	case varianceWorkerRefused:
		return intervention.WorkerMiss
	case varianceScheduleOverrun:
		return intervention.EstimateOverrun
	case variancePipelineFailed:
		return intervention.WorkerMiss
	case varianceOperatorOverride:
		return intervention.EstimateOverrun
	default:
		return intervention.VarianceKindUnknown
	}
}

func managerVarianceDirective(d intervention.VarianceDirective) varianceDirective {
	switch d {
	case intervention.VarianceRetry:
		return directiveRetry
	case intervention.VarianceEscalate:
		return directiveEscalate
	case intervention.VarianceTakeover:
		return directiveTakeover
	default:
		return directiveUnknown
	}
}

// ===========================================================================
// reviewEngine adapter — reviewEngine seam over review.ReviewEngine.
// ===========================================================================

type reviewAdapter struct{ inner review.ReviewEngine }

var _ reviewEngine = reviewAdapter{}

func (a reviewAdapter) ProposeReviews(change reviewChange, componentID string, artifactKind string, architectureGraph string, contracts []string) (ReviewSet, error) {
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
		return ReviewSet{}, err
	}
	reviewers := make([]Reviewer, 0, len(set.Reviewers))
	for _, r := range set.Reviewers {
		cr := Reviewer{
			Role:        r.Role,
			Perspective: r.Perspective,
			MayAmend:    r.MayAmend,
		}
		if r.ReferenceArtifact != "" {
			ref := r.ReferenceArtifact
			cr.ReferenceArtifact = &ref
		}
		reviewers = append(reviewers, cr)
	}
	return ReviewSet{Reviewers: reviewers}, nil
}

// ===========================================================================
// constructionPipelineAccess adapter — constructionPipelineAccess seam over the
// published constructionpipeline.ConstructionPipelineAccess.
// ===========================================================================

type pipelineAdapter struct {
	inner constructionpipeline.ConstructionPipelineAccess
}

var _ constructionPipelineAccess = pipelineAdapter{}

// pipelineDefaultToolchain is the single logical build step the Manager's neutral
// pipelineSpec implies (the image map resolves it to a concrete image).
const pipelineDefaultToolchain = "go-1.23"

// dispatchInputsFor builds the DispatchInputs bag for a construction pipeline dispatch.
func dispatchInputsFor(spec pipelineSpec) map[string]string {
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

func (a pipelineAdapter) SubmitConstructionPipeline(ctx context.Context, spec pipelineSpec, idempotencyKey fwra.IdempotencyKey) (pipelineHandle, error) {
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
		return pipelineHandle{}, err
	}
	return pipelineHandle{Name: constructionpipeline.PipelineHandleString(handle)}, nil
}

func (a pipelineAdapter) ObserveConstructionPipeline(ctx context.Context, handle pipelineHandle) (pipelineObservation, error) {
	obs, err := a.inner.ObserveConstructionPipeline(fwra.Context{Context: ctx}, constructionpipeline.ParsePipelineHandle(handle.Name))
	if err != nil {
		return pipelineObservation{}, err
	}
	return pipelineObservation{
		Phase:      managerPipelinePhase(obs.Phase),
		Diagnostic: obs.Diagnostic,
	}, nil
}

func (a pipelineAdapter) CancelConstructionPipeline(ctx context.Context, handle pipelineHandle) error {
	return a.inner.CancelConstructionPipeline(fwra.Context{Context: ctx}, constructionpipeline.ParsePipelineHandle(handle.Name))
}

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

// ===========================================================================
// artifactAccess adapter — artifactAccess seam over the published artifact.ArtifactAccess.
// ===========================================================================

type artifactAdapter struct{ inner artifact.ArtifactAccess }

var _ artifactAccess = artifactAdapter{}

func (a artifactAdapter) StoreConstructionOutput(ctx context.Context, output artifact.ConstructionOutput, idempotencyKey fwra.IdempotencyKey) (string, error) {
	return a.inner.StoreConstructionOutput(fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey}, output)
}

func (a artifactAdapter) RetrieveConstructionOutput(ctx context.Context, contentAddress string) (artifact.ConstructionOutput, error) {
	return a.inner.RetrieveConstructionOutput(fwra.Context{Context: ctx}, contentAddress)
}

// ===========================================================================
// workerAccess adapter — workerAccess seam over the published worker.WorkerAccess.
// ===========================================================================

type workerAdapter struct{ inner workeraccess.WorkerAccess }

var _ workerAccess = workerAdapter{}

func (a workerAdapter) Generate(ctx context.Context, spec workerGenerateSpec, idempotencyKey fwra.IdempotencyKey) ([]byte, error) {
	rc := fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey}
	raw, err := a.inner.Generate(rc, workeraccess.GenerateSpec{
		WorkerClass: workeraccess.WorkerClass(spec.WorkerClass),
		Prompt:      spec.Prompt,
	})
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (a workerAdapter) Cancel(ctx context.Context, idempotencyKey fwra.IdempotencyKey) error {
	return a.inner.Cancel(fwra.Context{Context: ctx, IdempotencyKey: idempotencyKey})
}

// ===========================================================================
// sourceControlRail adapter — sourceControlRail seam over the published
// sourcecontrol.SourceControlAccess (the folded composition-root railAdapter).
// ===========================================================================

type railAdapter struct {
	inner sourcecontrol.SourceControlAccess
}

var _ sourceControlRail = railAdapter{}

func (r railAdapter) GetInstallationToken(ctx context.Context, repo sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	return r.inner.GetInstallationToken(fwra.Context{Context: ctx}, repo)
}

func (r railAdapter) OpenBranch(ctx context.Context, repo sourcecontrol.RepoRef, branch sourcecontrol.BranchName, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.BranchRef, error) {
	return r.inner.OpenBranch(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, branch, cred)
}

func (r railAdapter) OpenPullRequest(ctx context.Context, repo sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.PullRequestRef, error) {
	return r.inner.OpenPullRequest(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, spec, cred)
}

func (r railAdapter) GetPullRequestStatus(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	return r.inner.GetPullRequestStatus(fwra.Context{Context: ctx}, repo, pr, cred)
}

func (r railAdapter) PostReview(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, review sourcecontrol.ReviewSubmission, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) error {
	return r.inner.PostReview(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, pr, review, cred)
}

func (r railAdapter) MergePullRequest(ctx context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, cred sourcecontrol.RepoCredential, key fwra.IdempotencyKey) (sourcecontrol.MergeResult, error) {
	return r.inner.MergePullRequest(fwra.Context{Context: ctx, IdempotencyKey: key}, repo, pr, cred)
}
