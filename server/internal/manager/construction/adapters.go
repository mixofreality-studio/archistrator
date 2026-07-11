package construction

// adapters.go holds the bridges between the Manager's OWN broader domain vocabulary
// (constructionActivity, this component's generated façade ReviewSet/Reviewer) and
// each dependency's PUBLISHED contract shape, for the calls that are NOT identity —
// either because the Manager's own type carries strictly more fields than the Engine
// needs (handoffActivityFromConstruction), or because the target is this component's
// OWN generated public façade type with a real field-shape divergence
// (reviewSetFromEngine), or because the Manager derives a real config value from raw
// composition-root config (constructionInterventionPolicy).
//
// After Task 6 the three Engines (handoff.HandOffEngine / intervention.InterventionEngine
// / review.ReviewEngine) have NO adapter STRUCT — the workflow calls their published
// contracts DIRECTLY (workflow.go / signals.go), with fweng.Context{Context:
// context.Background()} supplied inline at each call site. The identity enum maps that
// used to bridge Manager-local mirror enums onto the Engines' published enums
// (handoffActivityKind, managerWorkerClass, interventionVarianceKind,
// managerVarianceDirective) are deleted along with the mirror types themselves
// (deps.go) — every Manager-local enum that was ordinal-identical to its published
// counterpart is now typed AS that published enum directly.

import (
	"github.com/mixofreality-studio/archistrator/server/internal/engine/handoff"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
)

// ===========================================================================
// handOffEngine — handoffActivityFromConstruction narrows the Manager's broader
// constructionActivity (used package-wide — git-forward fields, resolved Phases) onto
// the Engine's published handoff.ConstructionActivity input. A REAL (if today
// field-for-field trivial) projection, not an identity mirror to delete: the two
// structs are NOT the same shape (constructionActivity carries CRLabel/IsRevert/Phases
// the Engine never sees).
// ===========================================================================

func handoffActivityFromConstruction(a constructionActivity) handoff.ConstructionActivity {
	return handoff.ConstructionActivity{
		ActivityID:   a.ActivityID,
		Kind:         a.Kind,
		ComponentID:  a.ComponentID,
		Layer:        a.Layer,
		EstimateDays: a.EstimateDays,
	}
}

// ===========================================================================
// interventionEngine — constructionInterventionPolicy is the ONE surviving
// config→contract-type builder: it resolves the composition-root's raw
// interventionMode STRING config (constructionmanager.go) onto the published
// intervention.InterventionPolicy. There is no Manager-local InterventionPolicy mirror
// left to build alongside it (deps.go) — the former second return value (the
// Manager-mirror interventionPolicy) is retired; nothing downstream ever read it.
// ===========================================================================

func constructionInterventionPolicy(mode string) intervention.InterventionPolicy {
	switch mode {
	case "escalate-everything", "escalateEverything", "supervised":
		return intervention.InterventionPolicy{Mode: intervention.EscalateEverything}
	default:
		return intervention.InterventionPolicy{Mode: intervention.Tiered, RetryBudget: 2}
	}
}

// ===========================================================================
// reviewEngine — reviewSetFromEngine bridges the published review.ReviewSet/Reviewer
// onto THIS component's OWN generated façade ReviewSet/Reviewer (contract.gen.go,
// off-limits — DO NOT EDIT). A REAL divergence, not an identity mirror: the façade's
// Reviewer.ReferenceArtifact is *string (optional, omitempty) while the Engine's own
// Reviewer.ReferenceArtifact is a plain string (empty ⇒ none) — the nil/empty-string
// boundary is exactly the kind of zero-value divergence that must be bridged
// explicitly, not cast.
// ===========================================================================

func reviewSetFromEngine(set review.ReviewSet) ReviewSet {
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
	return ReviewSet{Reviewers: reviewers}
}
