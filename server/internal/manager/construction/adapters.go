package construction

// adapters.go holds the FOLDED composition-root adapters that bridge the published
// engine interfaces (the dependencies the GENERATED constructor NewConstructionManager
// receives) to the Manager's unexported downstream engine seams (deps.go). Per the
// founder DI model (2026-06-28) these were retired from cmd/server and live HERE, in the
// one package that knows both sides — the Manager depends on each dependency's PUBLISHED
// interface and adapts it internally (Option-B boundary mapping).
//
// After the temporalgen migration this file carries ONLY the three ENGINE adapters
// (handOff / intervention / review). The engines are pure, deterministic, called DIRECTLY
// in-workflow (no Activity wrapper — replay-safe). The RA adapters (pipeline / artifact /
// rail) are retired: those ops are GENERATED and reached through the generated invoker
// surface (genInvokers); the workflow-side value mapping that lived on the old RA adapters
// (dispatchInputsFor / PipelineSpec composition / CheckState + Hints mapping) is now a set
// of pure workflow-side helpers (workflow.go / gitforward.go). The eligibility selection
// moved to eligibility.go. The mechanical enum/struct copies map by IDENTITY, not raw int,
// so a future re-order is safe.

import (
	"context"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
)

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
	case activityKindUnknown:
		// zero-value sentinel, not a real activity kind — same as any unmapped value.
		return handoff.ActivityKindUnknown
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
	case handoff.WorkerClassUnknown:
		// zero-value sentinel, not a real worker class — same as any unmapped value.
		return workerClassUnknown
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
	case varianceKindUnknown:
		// zero-value sentinel, not a real variance kind — same as any unmapped value.
		return intervention.VarianceKindUnknown
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
