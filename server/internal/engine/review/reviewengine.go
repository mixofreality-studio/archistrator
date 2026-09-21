// Package review is the reviewEngine component — the Engine that encapsulates the
// ReviewPolicy volatility: given a produced change (a component's contract-design,
// construction code, a UI-design concept, or UI code), WHICH reviewers review it,
// from WHAT perspective, and WHICH of them may amend the staged artifact by mutual
// agreement with the constructor.
//
// SEAM NOTE — the hand-run reviewEngine (constructionManager.md §9 OQ-4): the live
// review ROUTING during construction is a hand-run human/agent activity described
// by the-method-review-routing skill (ReviewPolicy); there is no frozen standalone
// contract file for it. The constructionManager nevertheless declares a concrete
// `ReviewEngine` consumer interface (internal/manager/construction/deps.go) it
// calls DIRECTLY in-workflow, so this package supplies a DETERMINISTIC Go
// realisation of that seam: ProposeReviews is pure computation over the inputs the
// Manager already has (change, componentId, artifactKind, architectureGraph,
// contracts) — no clock, no RNG, no I/O. The Manager fans out one worker dispatch
// per returned Reviewer and gates on the verdicts.
//
// Layer doctrine: [[the-method-layers]] (Engine layer) — PURE, DETERMINISTIC,
// in-workflow computation, mirroring handoff/intervention:
//   - NO I/O, NO time.Now(), NO math/rand, NO goroutines, NO global mutable state.
//   - NO outbound calls — no ResourceAccess, no other Engine, no Manager.
//   - Imports ONLY the framework-go Engine error model (fweng). It imports NO
//     Temporal — its determinism is what makes the constructionManager's direct
//     in-workflow ProposeReviews call replay-safe.
//
// v1 ReviewPolicy (documented, minimal-but-honest — see implementation log
// C-MCN-reconcile.md "reviewEngine policy"): the reviewer set is computed
// deterministically from the artifact KIND being reviewed, keyed to The Method's
// review-routing doctrine:
//
//   - DetailedDesign  → an architect reviews the service-contract against the
//     architecture (mayAmend: the architect+constructor may re-stage an amended
//     contract by agreement — the-method-review-routing "mayAmend" on contracts).
//   - Construction    → a senior reviewer reviews the code against the committed
//     detailed-design (no amend — code is corrected by the constructor, not the
//     reviewer).
//   - Integration     → a senior reviewer reviews integration against the
//     architecture call-chains (no amend).
//   - Noncoding       → a single architect sign-off (no amend).
//   - UI-design       → a UI designer reviews the concept against platform HIG /
//     Material guidance (mayAmend: designer+constructor may re-stage).
//   - UI code         → a senior reviewer reviews the UI code against the
//     UI-design (no amend).
//
// The KIND→reviewer-set RULE is a package-internal compile-time mapping (the
// ReviewPolicy Strategy), swappable per customer/policy without touching this
// surface — never leaked onto the contract. A future policy that consults the
// architectureGraph / contracts inputs (e.g. to add a security reviewer for an
// edge-touching component) refines reviewersFor without changing ProposeReviews.
//
// The kind VOCABULARY is the generated ReviewArtifactKind; the Manager owns the
// (activity type, lifecycle phase) → kind table until ProposeReviews takes the
// activity type itself (spec 2026-09-20 §5.4, stage 2).
package review

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// Perspective names a reviewer's review lens (the "from what perspective" axis of
// the-method-review-routing). Stable strings the Manager carries onto each worker
// dispatch; their IDENTITIES, not any numeric value, are load-bearing.
const (
	// perspectiveArchitecture — review against the committed architecture.dsl
	// (decomposition, call chains, layer rules).
	perspectiveArchitecture = "architecture"
	// perspectiveDetailedDesign — review against the component's D### detailed-design
	// / service-contract.
	perspectiveDetailedDesign = "detailedDesign"
	// perspectiveUIDesign — review against the committed UI-design concept.
	perspectiveUIDesign = "uiDesign"
)

// Role names a reviewer role. Stable strings; the Manager maps a Role onto a
// worker-class logical name for the dispatch (it is the reviewer's logical class).
const (
	// roleArchitect — the architect User / architect-class reviewer.
	roleArchitect = "architect"
	// roleSeniorReviewer — a human-senior (or senior-class agent) reviewer.
	roleSeniorReviewer = "seniorReviewer"
	// roleUIDesigner — a UI-design reviewer.
	roleUIDesigner = "uiDesigner"
)

// GENERATED CONTRACT SURFACE — the I/O models (ReviewChange, Reviewer, ReviewSet)
// AND the ReviewEngine interface are generated from this component's
// `.serviceContracts` entry in .aiarch/state/project.json into
// contract.gen.go. Schema-first: edit that entry and run `make gen`
// (or `make gen-models`); do not hand-edit the generated surface.
//
// Design rationale (the part not captured by the generated signature):
//   - ReviewEngine is the pure, deterministic review-routing port — the hand-run
//     reviewEngine seam given a concrete deterministic realisation. One behavioural
//     operation (matches the handoff 1-op precedent). The constructionManager holds
//     an independent consumer mirror it adapts to (deps.go).
//   - ProposeReviews is pure and deterministic: identical inputs → identical
//     ReviewSet, always. The error is *fweng.Error and signals programmer/contract
//     misuse ONLY (the Engine does no I/O): ContractMisuse (empty change
//     identifiers or an unrecognised artifactKind — a constructionManager bug) and
//     InternalInvariant (a recognised kind yielded an empty reviewer set — an engine
//     bug). architectureGraph + contracts are accepted by value for forward-compatible
//     policy refinement; the v1 policy keys on artifactKind alone and ignores them.

// The concrete ReviewEngine — the empty, stateless ReviewEngineImpl — and its
// constructor NewReviewEngine() are GENERATED into contract.gen.go (an engine is
// pure: no fields => no mutable state => trivially deterministic and reentrant).
// The behaviour below is hand-written on the generated struct.

// ProposeReviews implements ReviewEngine. It validates the input and computes the
// policy's reviewer set for the artifact kind.
func (ReviewEngineImpl) ProposeReviews(
	_ fweng.Context, // pure engine: carries identity/cancellation, ignored by v1 policy
	change ReviewChange,
	componentID string,
	artifactKind ReviewArtifactKind,
	_ string, // architectureGraph — reserved for a future policy refinement (v1 ignores)
	_ []string, // contracts — reserved for a future policy refinement (v1 ignores)
) (ReviewSet, error) {
	// --- ContractMisuse pre-conditions (programmer error, not a domain result) ---
	if change.ActivityID == "" {
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: change has empty ActivityID (Manager failed to assemble a valid ReviewChange)")
	}

	reviewers, known := reviewersFor(artifactKind)
	if !known {
		// The type is a string on the wire (the internal MCP tool decodes JSON into it), so
		// an out-of-vocabulary value is still reachable and still refused here.
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: unrecognised artifactKind "+quote(string(artifactKind)))
	}

	if componentScoped(artifactKind) && componentID == "" && change.ComponentID == "" {
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: "+string(artifactKind)+" reviews one component's artifact and no componentID was given")
	}

	// --- InternalInvariant guard: every recognised kind must yield ≥1 reviewer ---
	if len(reviewers) == 0 {
		return ReviewSet{}, fweng.New(fweng.InternalInvariant,
			"ProposeReviews: policy produced an empty reviewer set for a recognised kind "+quote(string(artifactKind)))
	}

	return ReviewSet{Reviewers: reviewers}, nil
}

// componentScoped reports whether a kind reviews ONE component's artifact (its service
// contract, its code, its UI design, its UI code) and so cannot be proposed without a
// component. Integration and Noncoding review against the system-level architecture:
// the system test plan and system testing have no component at all.
func componentScoped(kind ReviewArtifactKind) bool {
	switch kind {
	case ReviewKindDetailedDesign, ReviewKindConstruction, ReviewKindUIDesign, ReviewKindUICode:
		return true
	case ReviewKindIntegration, ReviewKindNoncoding:
		return false
	}
	return false
}

// reviewersFor is the package-internal ReviewPolicy: the deterministic
// artifactKind → reviewer-set mapping (the-method-review-routing). known is false for
// a value outside the generated vocabulary. Swappable per policy without touching the
// ProposeReviews surface.
func reviewersFor(kind ReviewArtifactKind) (reviewers []Reviewer, known bool) {
	switch kind {
	case ReviewKindDetailedDesign:
		// The architect reviews the service-contract against the architecture; the
		// architect+constructor may re-stage an amended contract by agreement.
		return []Reviewer{{
			Role:              roleArchitect,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          true,
		}}, true
	case ReviewKindConstruction:
		// A senior reviews the code against the committed detailed-design.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveDetailedDesign,
			ReferenceArtifact: "detailedDesign",
			MayAmend:          false,
		}}, true
	case ReviewKindIntegration:
		// A senior reviews integration against the architecture call-chains.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          false,
		}}, true
	case ReviewKindNoncoding:
		// A single architect sign-off.
		return []Reviewer{{
			Role:              roleArchitect,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          false,
		}}, true
	case ReviewKindUIDesign:
		// A UI designer reviews the concept; designer+constructor may re-stage.
		return []Reviewer{{
			Role:              roleUIDesigner,
			Perspective:       perspectiveUIDesign,
			ReferenceArtifact: "uiDesign",
			MayAmend:          true,
		}}, true
	case ReviewKindUICode:
		// A senior reviews the UI code against the committed UI-design.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveUIDesign,
			ReferenceArtifact: "uiDesign",
			MayAmend:          false,
		}}, true
	}
	return nil, false
}

// quote wraps s in double quotes for readable error detail (the same minimal idiom
// the handoff Engine uses, keeping the import set to fweng only).
func quote(s string) string { return "\"" + s + "\"" }
