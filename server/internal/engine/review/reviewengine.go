// Package review is the reviewEngine component — the Engine that encapsulates the
// ReviewPolicy volatility: given a produced change (a component's contract-design,
// construction code, a UI-design concept, UI code, or a design-rail artifact draft),
// WHICH reviewers review it, from WHAT perspective, WHICH of them may amend the staged
// artifact by mutual agreement with the constructor, and WHETHER a human must sign off
// before the work advances.
//
// SEAM NOTE — the hand-run reviewEngine (constructionManager.md §9 OQ-4): the live
// review ROUTING during construction is a hand-run human/agent activity described
// by the-method-review-routing skill (ReviewPolicy); there is no frozen standalone
// contract file for it. The constructionManager and the two design-rail Managers
// nevertheless call this component's published contract DIRECTLY in-workflow, so this
// package supplies a DETERMINISTIC Go realisation of that seam: ProposeReviews is pure
// computation over the inputs the callers already have (change, activity type,
// lifecycle phase, componentId, the committed ReviewPolicy, the floor flag, contracts)
// — no clock, no RNG, no I/O. The constructionManager fans out one worker dispatch per
// returned Reviewer and gates on the verdicts.
//
// Layer doctrine: [[the-method-layers]] (Engine layer) — PURE, DETERMINISTIC,
// in-workflow computation, mirroring handoff/intervention:
//   - NO I/O, NO time.Now(), NO math/rand, NO goroutines, NO global mutable state.
//   - NO outbound calls — no ResourceAccess, no other Engine, no Manager.
//   - Imports ONLY pure stdlib (slices) and the framework-go Engine error model
//     (fweng). It imports NO Temporal — its determinism is what makes the direct
//     in-workflow ProposeReviews call replay-safe.
//
// v1 ReviewPolicy (documented, minimal-but-honest — see implementation log
// C-MCN-reconcile.md "reviewEngine policy"): the reviewer set is computed
// deterministically from the artifact KIND the activity's (type, lifecycle phase) pair
// resolves to, keyed to The Method's review-routing doctrine:
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
// The three DESIGN activity types take their reviewer rows from the design rail's own
// critique roster instead: the PM critiques the business-alignment steps, the architect
// self-critiques the architecture, and volatility identification and the whole of
// project design take no agent reviewer at all (the plan is computed, and the only
// judge is the human at M0).
//
// The RULES are package-internal compile-time mappings (the ReviewPolicy Strategy),
// swappable per customer/policy without touching this surface — never leaked onto the
// contract. A future policy that consults the contracts input (e.g. to add a security
// reviewer for an edge-touching component) refines reviewersFor without changing
// ProposeReviews.
//
// This engine answers BOTH halves of the review question — who reviews, and whether a
// human must sign off — from the activity's type, its lifecycle phase, the project's
// committed ReviewPolicy and the non-overridable floor flag. The policy DOCUMENT stays
// in project state (projectStateAccess owns ReviewPolicy, its preset vocabulary,
// ReviewPolicyFromGateIDs and ContractTouchesReviewFloor); the policy DECISION lives
// here (spec 2026-09-20 §5.4, stage 2). Splitting the two is how the Manager's kind
// table and the gate policy drifted apart in the first place.
package review

import (
	"slices"

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
	// perspectiveBusinessAlignment — review against the committed mission: does this
	// draft still serve the business objectives the PM ratified (Löwy ch. 7)?
	perspectiveBusinessAlignment = "businessAlignment"
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
	// roleProductManager — the PM, customer proxy, who ratifies business alignment
	// (Löwy ch. 7). The string matches the design rail's critiqueRoleProductManager
	// wire label, so the session view and the reviewer strip name the role identically.
	roleProductManager = "productManager"
)

// The lifecycle-phase wire names the gate table keys on. A CONSTRUCTION activity walks
// the closed five (projectstate.ActivityMethodPhase); a DESIGN activity walks the
// method-assets design lifecycle, whose phase ids are artifact wire names. Both arrive
// as bare strings — see the lifecyclePhase parameter's rationale on ProposeReviews.
const (
	phaseRequirements   = "requirements"
	phaseDetailedDesign = "detailed_design"
	phaseTestPlan       = "test_plan"
	phaseConstruction   = "construction"
	phaseIntegration    = "integration"
	// phaseVolatilities is the one DESIGN phase the tables name: it is the only
	// reviewer-less row on the requirements rail. Its four siblings (mission, glossary,
	// coreUseCases, and the architecture step) all take the rail's default row, so
	// naming them would add constants no branch reads.
	phaseVolatilities = "volatilities"
	// phaseSDP is the projectDesign lifecycle's M0 gate — the SDP review that approves
	// the plan AND the spend it commits.
	phaseSDP = "sdp"
)

// GENERATED CONTRACT SURFACE — the I/O models (ReviewChange, Reviewer, ReviewSet,
// ReviewPolicy, ActivityType, ReviewArtifactKind) AND the ReviewEngine interface are
// generated from this component's `.serviceContracts` entry in
// .aiarch/state/project.json into contract.gen.go. Schema-first: edit that entry and
// run `make gen` (or `make gen-models`); do not hand-edit the generated surface.
//
// Design rationale (the part not captured by the generated signature):
//   - ReviewEngine is the pure, deterministic review-routing port — the hand-run
//     reviewEngine seam given a concrete deterministic realisation. One behavioural
//     operation (matches the handoff 1-op precedent). Every caller depends on the
//     PUBLISHED contract directly; there is no Manager-local consumer mirror.
//   - ProposeReviews is pure and deterministic: identical inputs → identical
//     ReviewSet, always. The error is *fweng.Error and signals programmer/contract
//     misuse ONLY (the Engine does no I/O): ContractMisuse (an empty ActivityID, an
//     unrecognised activityType, or a component-scoped kind with no component — each a
//     caller bug) and InternalInvariant (a CONSTRUCTION type yielded an empty reviewer
//     set — an engine bug). contracts is accepted by value for forward-compatible
//     policy refinement; the v1 policy ignores it.
//   - ActivityType is the engine's OWN copy of the ten activity-type WIRE NAMES. An
//     Engine may not import projectstate (F3), the ordinals are that RA's storage
//     concern, and the ten strings are already the shared vocabulary (they key
//     method-assets' lifecycles.json too).
//   - ReviewPolicy is the engine's OWN copy of the committed policy DOCUMENT, with
//     Preset a plain string ("" is the legacy/explicit mode) where projectstate stores
//     a *string; each caller dereferences in its own five-line adapter.

// The concrete ReviewEngine — the empty, stateless ReviewEngineImpl — and its
// constructor NewReviewEngine() are GENERATED into contract.gen.go (an engine is
// pure: no fields => no mutable state => trivially deterministic and reentrant).
// The behaviour below is hand-written on the generated struct.

// ProposeReviews implements ReviewEngine. It validates the input, resolves the artifact
// kind the activity's (type, lifecycle phase) pair is reviewed as, computes the policy's
// reviewer set for it, and decides whether a human must sign off at this gate.
//
// lifecyclePhase is a bare string, not an enum, and that is deliberate. A design
// activity's lifecycle phases are mission/glossary/volatilities/coreUseCases/
// architecture/sdp (method-assets data), which are not members of the construction
// rail's closed five. A closed enum here would either have to absorb them — freezing
// platform-fixed data into a wire contract that "never renumber" then binds forever —
// or force a caller to lie. The tables below are TOTAL over the string instead, and the
// callers' parity tests are what keep the strings honest.
func (ReviewEngineImpl) ProposeReviews(
	_ fweng.Context, // pure engine: carries identity/cancellation, ignored by v1 policy
	change ReviewChange,
	activityType ActivityType,
	lifecyclePhase string,
	componentID string,
	policy ReviewPolicy,
	floorTouched bool,
	_ []string, // contracts — reserved for a future policy refinement (v1 ignores)
) (ReviewSet, error) {
	// --- ContractMisuse pre-conditions (programmer error, not a domain result) ---
	if change.ActivityID == "" {
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: change has empty ActivityID (caller failed to assemble a valid ReviewChange)")
	}

	hasComponent := componentID != "" || change.ComponentID != ""
	kind := artifactKindFor(activityType, lifecyclePhase, hasComponent)

	reviewers, known := reviewersFor(activityType, lifecyclePhase, kind)
	if !known {
		// The type is a string on the wire (the internal MCP tool decodes JSON into it), so
		// an out-of-vocabulary value is still reachable and still refused here.
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: unrecognised activityType "+quote(string(activityType)))
	}

	// Belt and braces: artifactKindFor degrades a component-scoped kind to Noncoding
	// when there is no component, so this cannot fire today. It stays as the invariant's
	// restatement — a future table edit that forgets the degrade is refused, not
	// silently routed to a reviewer with nothing to review.
	if componentScoped(kind) && !hasComponent {
		return ReviewSet{}, fweng.New(fweng.ContractMisuse,
			"ProposeReviews: "+string(kind)+" reviews one component's artifact and no componentID was given")
	}

	// --- InternalInvariant guard: a CONSTRUCTION type must yield ≥1 reviewer ---
	// NARROWED for the design rail: an empty reviewer set is legal exactly where nobody
	// reviews by design — volatility identification is the architect's own signature
	// skill and has never had a critique round, and a project-design option is COMPUTED,
	// so its only judge is the human at M0. For the seven construction types an empty
	// set is still an engine bug.
	if len(reviewers) == 0 && !designActivityType(activityType) {
		return ReviewSet{}, fweng.New(fweng.InternalInvariant,
			"ProposeReviews: policy produced an empty reviewer set for construction activity type "+
				quote(string(activityType))+" at lifecycle phase "+quote(lifecyclePhase))
	}

	human, reason := requiresHuman(activityType, lifecyclePhase, policy, floorTouched)
	return ReviewSet{Reviewers: reviewers, RequiresHuman: human, Reason: reason, ArtifactKind: kind}, nil
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

// designActivityType reports whether the type belongs to the DESIGN rail (Phase 1 / 2)
// rather than the construction rail. The two rails differ in three places: their
// lifecycle-phase vocabulary, their reviewer rows, and their legacy-preset fallback.
func designActivityType(t ActivityType) bool {
	switch t {
	case ActivityTypeRequirements, ActivityTypeArchitecture, ActivityTypeProjectDesign:
		return true
	case ActivityTypeService, ActivityTypeFrontend, ActivityTypeTesting, ActivityTypeDeployment,
		ActivityTypeDocumentation, ActivityTypeUIDesign, ActivityTypeIntegration:
		return false
	}
	return false
}

// artifactKindFor is the TOTAL (activity type, lifecycle phase) → review kind table:
// every cell is decided, so it returns no error and no bool, and a gate can never go
// without reviewers because of what a caller passed. It moved here from the
// constructionManager when ProposeReviews took the activity type (spec 2026-09-20 §5.4,
// stage 2); the engine owns the VOCABULARY, so a wrong value does not compile.
//
// A component-scoped kind for an activity with NO component degrades to Noncoding:
// there is no contract or UI design to hold the work against, so the architect signs it
// off. Off-profile cells (a phase the type's lifecycle does not carry) are decided too,
// so a lifecycle change cannot make this partial. The rows are justified in the plan
// (docs/superpowers/plans/2026-09-21-activity-experience-stage0.md, Task 6).
func artifactKindFor(t ActivityType, p string, hasComponent bool) ReviewArtifactKind {
	kind := ReviewKindNoncoding
	switch t {
	case ActivityTypeService, ActivityTypeDeployment:
		kind = componentReviewKind(p, ReviewKindDetailedDesign, ReviewKindConstruction)
	case ActivityTypeFrontend, ActivityTypeUIDesign:
		kind = componentReviewKind(p, ReviewKindUIDesign, ReviewKindUICode)
	case ActivityTypeIntegration:
		kind = componentReviewKind(p, ReviewKindNoncoding, ReviewKindNoncoding)
	case ActivityTypeTesting, ActivityTypeDocumentation:
		// A document or a test asset with no architecture component; its "integration"
		// phase is a label (Plan Review, Sign-off, Doc Review), not a call-chain integration.
	case ActivityTypeRequirements, ActivityTypeArchitecture, ActivityTypeProjectDesign:
		// A design artifact is a document: the architect (or the PM) signs it off. The
		// design rail's reviewer rows are in reviewersFor, not in the kind.
	}
	if !hasComponent && kind != ReviewKindIntegration {
		return ReviewKindNoncoding
	}
	return kind
}

// componentReviewKind is one row of the table for a type that designs and then builds:
// its documents (requirements, test plan) are signed off, its design and its build take
// the row's two kinds, and its integration is reviewed against the call chains.
func componentReviewKind(p string, design, build ReviewArtifactKind) ReviewArtifactKind {
	switch p {
	case phaseRequirements, phaseTestPlan:
		return ReviewKindNoncoding
	case phaseDetailedDesign:
		return design
	case phaseConstruction:
		return build
	case phaseIntegration:
		return ReviewKindIntegration
	}
	return ReviewKindNoncoding
}

// reviewersFor is the package-internal ReviewPolicy: the deterministic reviewer-set
// mapping (the-method-review-routing). known is false for an activityType outside the
// generated vocabulary. Swappable per policy without touching the ProposeReviews
// surface.
func reviewersFor(t ActivityType, p string, kind ReviewArtifactKind) (reviewers []Reviewer, known bool) {
	switch t {
	case ActivityTypeService, ActivityTypeFrontend, ActivityTypeTesting, ActivityTypeDeployment,
		ActivityTypeDocumentation, ActivityTypeUIDesign, ActivityTypeIntegration:
		return constructionReviewersFor(kind), true
	case ActivityTypeRequirements, ActivityTypeArchitecture, ActivityTypeProjectDesign:
		return designReviewersFor(t, p), true
	}
	return nil, false
}

// constructionReviewersFor is the kind-keyed roster of the construction rail.
func constructionReviewersFor(kind ReviewArtifactKind) []Reviewer {
	switch kind {
	case ReviewKindDetailedDesign:
		// The architect reviews the service-contract against the architecture; the
		// architect+constructor may re-stage an amended contract by agreement.
		return []Reviewer{{
			Role:              roleArchitect,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          true,
		}}
	case ReviewKindConstruction:
		// A senior reviews the code against the committed detailed-design.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveDetailedDesign,
			ReferenceArtifact: "detailedDesign",
			MayAmend:          false,
		}}
	case ReviewKindIntegration:
		// A senior reviews integration against the architecture call-chains.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          false,
		}}
	case ReviewKindNoncoding:
		// A single architect sign-off.
		return []Reviewer{{
			Role:              roleArchitect,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          false,
		}}
	case ReviewKindUIDesign:
		// A UI designer reviews the concept; designer+constructor may re-stage.
		return []Reviewer{{
			Role:              roleUIDesigner,
			Perspective:       perspectiveUIDesign,
			ReferenceArtifact: "uiDesign",
			MayAmend:          true,
		}}
	case ReviewKindUICode:
		// A senior reviews the UI code against the committed UI-design.
		return []Reviewer{{
			Role:              roleSeniorReviewer,
			Perspective:       perspectiveUIDesign,
			ReferenceArtifact: "uiDesign",
			MayAmend:          false,
		}}
	}
	return nil
}

// designReviewersFor is the design rail's roster, read off the Managers' own
// critiqueCriticFor: the PM critiques the business-alignment steps against the mission
// (mission / glossary+scrubbed-requirements / core use cases — Löwy ch. 7), the
// architect SELF-critiques the architecture and may amend it, volatility identification
// takes no critique round at all (the architect's own signature skill), and project
// design takes none either — the option is computed, and the only judge is the human at
// M0. The role strings are the design rail's own wire labels, so both surfaces name the
// role identically.
func designReviewersFor(t ActivityType, p string) []Reviewer {
	if t == ActivityTypeProjectDesign {
		// The plan is COMPUTED, not drafted: nobody critiques an option network, and the
		// only judge is the human at M0 (requiresHuman's sdp row).
		return nil
	}
	if t == ActivityTypeArchitecture {
		// The architect SELF-critiques the architecture and may amend it — the ratified
		// "architect-owned steps skip PM critique" doctrine (QA amendment 2026-07-17).
		return []Reviewer{{
			Role:              roleArchitect,
			Perspective:       perspectiveArchitecture,
			ReferenceArtifact: "architecture",
			MayAmend:          true,
		}}
	}
	// The requirements activity's four steps: mission / glossary (which the
	// scrubbed-requirements pass shares) / core use cases take the PM's critique round,
	// reviewing business alignment against the mission, while volatility identification
	// is the architect's own signature skill and has never had a critique round at all.
	// An unlisted phase takes the PM too — the same default critiqueCriticFor gives a
	// requirements-family kind.
	if p == phaseVolatilities {
		return nil
	}
	return []Reviewer{{
		Role:              roleProductManager,
		Perspective:       perspectiveBusinessAlignment,
		ReferenceArtifact: "mission",
		MayAmend:          false,
	}}
}

// requiresHuman is the gate decision — the body of the retired
// projectstate.ReviewPolicy.EffectiveGate plus the two design rails' inline
// `Preset == vibes` read, in one table. It returns the verdict WITH the one-line reason
// that explains it (spec §7.2: every gate verdict is legible on the reviewer strip).
//
// Row 1, the non-overridable floor, comes first for every activity type: a construction
// dispatch whose committed contract touches deploy/spend/schema
// (projectstate.ContractTouchesReviewFloor) stays gated under every preset, "vibes"
// included. No preset value can widen or narrow it.
//
// Row 2 is the second non-overridable floor, new in stage 2: the projectDesign SDP gate
// approves the plan AND the spend it commits (spec §6/§5.4), so M0 always holds for a
// human.
//
// Rows 3 and 4 then split by rail, and their legacy ("") arms genuinely differ —
// construction falls back to the committed explicit map, design falls back to "a human
// decides". That is what the two code paths did before they were one, and preserving
// the difference is the point.
func requiresHuman(t ActivityType, p string, policy ReviewPolicy, floorTouched bool) (bool, string) {
	if p == phaseConstruction && floorTouched {
		return true, "the non-overridable floor: this activity's contract touches deploy/spend/schema, so its construction dispatch is gated under every preset"
	}
	if t == ActivityTypeProjectDesign && p == phaseSDP {
		return true, "the non-overridable spend floor: the SDP review approves the plan and its cost, so M0 always holds for a human"
	}
	if designActivityType(t) {
		if policy.Preset == presetVibes {
			return false, "preset \"vibes\": a clean design draft auto-approves at the review gate"
		}
		return true, "preset " + quote(policy.Preset) + ": every design draft outside \"vibes\" holds for a human decision"
	}
	return constructionGate(t, p, policy)
}

// The committed ReviewPolicy.Preset vocabulary, mirrored from projectstate (the RA owns
// the DOCUMENT and its write-path validation; an Engine may not import it). "" is the
// legacy/explicit mode — the committed GatedPhasesByType map, unchanged pre-preset
// behaviour (e.g. the webApp PolicyPanel's ReviewPolicyFromGateIDs output).
const (
	presetVibes       = "vibes"
	presetCheckpoints = "checkpoints"
	presetFull        = "full"
)

// constructionGate resolves the preset switch for a construction activity's phase —
// EffectiveGate's switch, moved verbatim.
//
// checkpoints gates the per-activity contract/architecture commit (detailed_design),
// the construction dispatch (construction) and the integration pass (integration) — the
// funnel checkpoints this per-activity, per-phase mechanism can express. integration is
// in the list because the integration TYPE's lifecycle is integration-ONLY: without it
// an I-* activity would be the one family that runs entirely ungated under
// "checkpoints", which is exactly backwards — an integration activity is where a use
// case is first exercised end to end (founder-ratified).
func constructionGate(t ActivityType, p string, policy ReviewPolicy) (bool, string) {
	switch policy.Preset {
	case presetVibes:
		return false, "preset \"vibes\": nothing is gated beyond the non-overridable floor"
	case presetFull:
		return true, "preset \"full\": every phase is gated"
	case presetCheckpoints:
		if p == phaseDetailedDesign || p == phaseConstruction || p == phaseIntegration {
			return true, "preset \"checkpoints\": " + quote(p) + " is one of the three funnel checkpoints"
		}
		return false, "preset \"checkpoints\": " + quote(p) + " is not a funnel checkpoint"
	}
	if slices.Contains(policy.GatedPhasesByType[string(t)], p) {
		return true, "legacy policy: the committed gate map lists " + quote(p) + " for " + quote(string(t))
	}
	return false, "legacy policy: the committed gate map does not list " + quote(p) + " for " + quote(string(t))
}

// quote wraps s in double quotes for readable error detail (the same minimal idiom
// the handoff Engine uses, keeping the import set to fweng only).
func quote(s string) string { return "\"" + s + "\"" }
