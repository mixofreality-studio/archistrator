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

// reviewKind classifies the produced change under review (mirrors the
// constructionManager's notion of the activity kind / artifact kind it passes as
// the artifactKind string). The numeric ordering is Engine-internal and not a wire
// contract.
type reviewKind int

const (
	// kindUnknown — unset (a ContractMisuse on input: the Manager must pass
	// a recognised artifactKind string).
	kindUnknown reviewKind = iota
	// kindDetailedDesign — a component's contract-design (service contract).
	kindDetailedDesign
	// kindConstruction — a component's construction code.
	kindConstruction
	// kindIntegration — an integration activity's output.
	kindIntegration
	// kindNoncoding — a non-coding work-product.
	kindNoncoding
	// kindUIDesign — a UI-design concept.
	kindUIDesign
	// kindUICode — UI code.
	kindUICode
)

// artifactKindByName maps the artifactKind string the Manager passes (the
// ActivityKind.String() canonical names, plus the UI kinds) to the typed kind. The
// names mirror constructionManager's ActivityKind.String() ("DetailedDesign",
// "Construction", "Integration", "Noncoding") so the Manager's call is mechanical,
// plus "UIDesign"/"UICode" for the client-facet review-routing cases.
var artifactKindByName = map[string]reviewKind{
	"DetailedDesign": kindDetailedDesign,
	"Construction":   kindConstruction,
	"Integration":    kindIntegration,
	"Noncoding":      kindNoncoding,
	"UIDesign":       kindUIDesign,
	"UICode":         kindUICode,
}

// GENERATED CONTRACT SURFACE — the I/O models (ReviewChange, Reviewer, ReviewSet)
// AND the ReviewEngine interface are generated from contract.schema.json into
// contract.gen.go. Schema-first: edit the schema and run `make gen` (or
// gen-schemas/gen-models); do not hand-edit the generated surface.
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

// ProposeReviews implements ReviewEngine. It validates the input, classifies the
// artifactKind, and computes the policy's reviewer set for that kind.
func (ReviewEngineImpl) ProposeReviews(
	_ fweng.Context, // pure engine: carries identity/cancellation, ignored by v1 policy
	change ReviewChange,
	componentID string,
	artifactKind string,
	_ string, // architectureGraph — reserved for a future policy refinement (v1 ignores)
	_ []string, // contracts — reserved for a future policy refinement (v1 ignores)
) (ReviewSet, error) {
	// --- ContractMisuse pre-conditions (programmer error, not a domain result) ---
	if change.ActivityID == "" {
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: change has empty ActivityID (Manager failed to assemble a valid ReviewChange)")
	}
	if componentID == "" && change.ComponentID == "" {
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: empty componentID (Manager failed to assemble a valid call)")
	}

	kind, ok := artifactKindByName[artifactKind]
	if !ok || kind == kindUnknown {
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: unrecognised artifactKind "+quote(artifactKind))
	}

	reviewers := reviewersFor(kind)

	// --- InternalInvariant guard: every recognised kind must yield ≥1 reviewer ---
	if len(reviewers) == 0 {
		return ReviewSet{}, fweng.New(fweng.InternalInvariant,
			"ProposeReviews: policy produced an empty reviewer set for a recognised kind "+quote(artifactKind))
	}

	return ReviewSet{Reviewers: reviewers}, nil
}

// reviewersFor is the package-internal ReviewPolicy: the deterministic
// artifactKind → reviewer-set mapping (the-method-review-routing). Swappable per
// policy without touching the ProposeReviews surface.
func reviewersFor(kind reviewKind) []Reviewer {
	switch kind {
	case kindDetailedDesign:
		// The architect reviews the service-contract against the architecture; the
		// architect+constructor may re-stage an amended contract by agreement.
		return []Reviewer{{
			Role:              roleArchitect,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          true,
		}}
	case kindConstruction:
		// A senior reviews the code against the committed detailed-design.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveDetailedDesign,
			ReferenceArtifact: "detailedDesign",
			MayAmend:          false,
		}}
	case kindIntegration:
		// A senior reviews integration against the architecture call-chains.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          false,
		}}
	case kindNoncoding:
		// A single architect sign-off.
		return []Reviewer{{
			Role:              roleArchitect,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          false,
		}}
	case kindUIDesign:
		// A UI designer reviews the concept; designer+constructor may re-stage.
		return []Reviewer{{
			Role:              roleUIDesigner,
			Perspective:       perspectiveUIDesign,
			ReferenceArtifact: "uiDesign",
			MayAmend:          true,
		}}
	case kindUICode:
		// A senior reviews the UI code against the committed UI-design.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveUIDesign,
			ReferenceArtifact: "uiDesign",
			MayAmend:          false,
		}}
	default:
		return nil
	}
}

// quote wraps s in double quotes for readable error detail (the same minimal idiom
// the handoff Engine uses, keeping the import set to fweng only).
func quote(s string) string { return "\"" + s + "\"" }
