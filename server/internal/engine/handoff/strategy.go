package handoff

import "strings"

// handOffStrategy is the package-internal casting RULE for one HandOffPolicy mode
// (contract §6). It is NEVER exposed on the contract surface (Variant C, rejected;
// FU-HE-B). It is selected per the project's HandOffPolicy at call entry by
// selectStrategy and runs on the by-value activity snapshot.
//
// Strategies are PURE: no clock, no RNG, no global mutable state (contract §6
// invariants 1-3). If a future policy ever expresses a probabilistic split (e.g.
// "10% of activities to human review"), that selection MUST be a deterministic
// function of a value carried on the activity (e.g. a stable hash of ActivityID),
// NEVER math/rand — otherwise constructionManager's replay would diverge
// (contract §6 invariant 2, FU-HE-A).
type handOffStrategy interface {
	pickWorkerClass(activity ConstructionActivity) WorkerClass
}

// selectStrategy maps the committed HandOffPolicy to its casting Strategy. This is
// compile-time package-internal wiring (contract §6, §8 Variant C) — adding a new
// customer split or a new worker class is a new Strategy registration here, behind
// the unchanged PickWorkerClass surface (no contract amendment).
//
// v1 modes derived from the constructionManager consumer mirror's HandOffPolicy
// fields (PreferAI, SeniorOnlyLayers):
//
//   - PreferAI=true   → fullyAutomatedStrategy: AI everywhere EXCEPT a senior-only
//     layer, which the customer still forces to a human senior.
//   - PreferAI=false  → seniorReviewsAllStrategy: a human senior by default; this
//     is the review-heavy customer.
//
// Both honor SeniorOnlyLayers. The architect-only arrangement (glossary.md line
// 10) is cast by architectOnlyStrategy, reserved for the future explicit mode
// (the v1 field set has no architect-only flag yet — OQ-2 keeps ArchitectOnly a
// legitimate returned class so adding that mode is a Strategy-only change).
func selectStrategy(policy HandOffPolicy) handOffStrategy {
	seniorOnly := normalizeLayers(policy.SeniorOnlyLayers)
	if policy.PreferAI {
		return fullyAutomatedStrategy{seniorOnlyLayers: seniorOnly}
	}
	return seniorReviewsAllStrategy{seniorOnlyLayers: seniorOnly}
}

// normalizeLayers lowercases and indexes the senior-only layer set for
// case-insensitive matching. Returns a non-nil set (possibly empty). Pure — no
// global state; a fresh map per call keeps the Engine reentrant.
func normalizeLayers(layers []string) map[string]struct{} {
	set := make(map[string]struct{}, len(layers))
	for _, l := range layers {
		set[strings.ToLower(strings.TrimSpace(l))] = struct{}{}
	}
	return set
}

// isSeniorOnly reports whether the activity's layer is in the senior-only set.
func isSeniorOnly(seniorOnly map[string]struct{}, activity ConstructionActivity) bool {
	_, ok := seniorOnly[strings.ToLower(strings.TrimSpace(activity.Layer))]
	return ok
}

// fullyAutomatedStrategy casts AI for everything except a customer-forced
// senior-only layer. The fully-automated customer: the customer acts as the
// architect and lets AI build, while still pinning a human senior on the most
// sensitive layers if SeniorOnlyLayers names them.
type fullyAutomatedStrategy struct {
	seniorOnlyLayers map[string]struct{}
}

func (s fullyAutomatedStrategy) pickWorkerClass(activity ConstructionActivity) WorkerClass {
	if isSeniorOnly(s.seniorOnlyLayers, activity) {
		return HumanSeniorWorker
	}
	return AIWorker
}

// seniorReviewsAllStrategy casts a human senior by default — the review-everything
// customer who wants every line owned by a human senior worker. SeniorOnlyLayers
// is redundant here (already senior everywhere) but honored for symmetry.
type seniorReviewsAllStrategy struct {
	seniorOnlyLayers map[string]struct{}
}

func (s seniorReviewsAllStrategy) pickWorkerClass(activity ConstructionActivity) WorkerClass {
	return HumanSeniorWorker
}

// architectOnlyStrategy casts ArchitectOnly for every activity — the
// customer-as-architect arrangement (glossary.md line 10), where no separate
// worker produces the activity and the Manager awaits the Architect User
// (contract OQ-2). Reserved for the future explicit architect-only policy mode;
// kept here so wiring it in is a Strategy-only change behind the unchanged
// contract surface.
type architectOnlyStrategy struct{}

func (architectOnlyStrategy) pickWorkerClass(activity ConstructionActivity) WorkerClass {
	return ArchitectOnly
}
