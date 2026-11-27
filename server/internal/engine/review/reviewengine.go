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
//   - Imports ONLY pure stdlib, the framework-go Engine error model (fweng) and the platform's own
//     pure readers of the design (methodcheck, scenario, method-assets, and the app-generator's
//     manifests the task spec's generated paths come from). It imports NO Temporal — its
//     determinism is what makes the direct in-workflow ProposeReviews and DeriveTaskSpec calls
//     replay-safe.
//
// The second half of the contract is the TASK SPEC (FPC Task 6.2, ruling 5): DeriveTaskSpec turns
// the project document and the facts the project state owns into what one lifecycle task must
// change and prove — its edit scope, checks and generated paths — and TaskSpecHash is the one hash
// every gate verifies a spec through. See the section below ProposeReviews' helpers.
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/appgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
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
	// phaseCoreUseCases is the requirements step whose PM critique carries a floor of its own
	// (requiresHuman's pmCoverageSendBack row): every test of a surface derives from these
	// diagrams, so the PM's send-back is never auto-approved over.
	phaseCoreUseCases = "coreUseCases"
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

// The concrete ReviewEngine — the empty, stateless reviewEngineImpl — and its
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
func (reviewEngineImpl) ProposeReviews(
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
		ActivityTypeDocumentation, ActivityTypeUIDesign, ActivityTypeIntegration, ActivityTypeMachineClient,
		ActivityTypeMilestone:
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
	case ActivityTypeIntegration, ActivityTypeMachineClient:
		kind = componentReviewKind(p, ReviewKindNoncoding, ReviewKindNoncoding)
	case ActivityTypeTesting, ActivityTypeDocumentation, ActivityTypeMilestone:
		// A document or a test asset with no architecture component; its "integration"
		// phase is a label (Plan Review, Sign-off, Doc Review), not a call-chain integration.
		// A milestone owns no component either: its one review judges a recorded run.
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
		ActivityTypeDocumentation, ActivityTypeUIDesign, ActivityTypeIntegration, ActivityTypeMachineClient,
		ActivityTypeMilestone:
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
// Row 2b is the floor FPC Task 2.14 adds: at the core use cases' review the floor flag means the
// PM's critique sent the draft back (pmCoverageSendBack), and the gate holds for a human under every
// preset, "vibes" included — the one preset that would otherwise auto-approve a design draft. The flag
// is the caller's "this gate's floor condition holds"; which condition it names is read off the gate
// (construction: the contract touches deploy/spend/schema; coreUseCases: the PM's send-back), so no
// contract surface changes.
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
	if t == ActivityTypeRequirements && p == phaseCoreUseCases && floorTouched {
		return true, "the non-overridable floor pmCoverageSendBack: the product manager sent the core use cases back for coverage, so the gate holds for a human under every preset and an approve over it is an override recorded with a reason"
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

// reviewEngineImpl is the concrete ReviewEngine. Engines are pure: it carries no fields.
type reviewEngineImpl struct{}

// newReviewEngine is the hand-written builder the generated NewReviewEngine delegates to.
func newReviewEngine() ReviewEngine { return reviewEngineImpl{} }

// =====================================================================================
// THE TASK SPEC (FPC Task 6.2, ruling 5) — what one lifecycle task must change and prove.
//
// DeriveTaskSpec is the ONE resolver of what an agent task is configured to do: who runs it
// (identity), on which model at what estimated cost, with which prompt and tools, which files it
// may change (edit scope), and which typed checks decide it. The web panel, the MCP run_checks
// verb and the GitHub/local jobs all read it, and the hash proves they read the same thing.
// method-assets owns the method half (MethodSpecFor: identity, model, prompt, tools); this
// owns the project half (edit scope, checks, hash). What only the project state can know — the
// activity's classification, the rate card and effort, the slot rule attribution — arrives as
// TaskFacts; the design (System, service contracts, derived scenarios) is read from the project
// document itself through the platform's own readers.
//
// It is a pure function of the embedded method assets, the project document and the facts: no
// clock, no I/O, no map-order dependence, so the server and the job compute byte-identical
// hashes from the same project.json.
// =====================================================================================

// The kinds of typed check (the same strings projectstate.CheckKind* carry; the gate side
// reads those).
const (
	checkKindRule     = "rule"
	checkKindCommand  = "command"
	checkKindSmoke    = "smoke"
	checkKindScenario = "scenario"
	// checkKindAdmission is Tier 2 admission (projectstate.CheckKindAdmission).
	checkKindAdmission = "admission"
)

// admissionCheckID names the Tier 2 admission check; admissionCheckTimeoutSec is its cap (a scaffold,
// every generator and two builds over a cold module cache).
const (
	admissionCheckID         = "admission:tier2"
	admissionCheckTimeoutSec = 1800
)

// acceptanceContract is the pseudo-contract the acceptance scenarios and their checks are filed
// under (projectstate.AcceptanceContract): the acceptance plan belongs to no component.
const acceptanceContract = "acceptance"

// The task-spec bounds of the edit scope grammar and the check timeouts, in seconds.
const (
	// anyPath is the edit-scope glob that admits every repo path: the explicit scope of a task
	// whose edit set cannot be derived.
	anyPath = "**"
	// generatedFilesGlobSuffix turns "<dir>" into the one non-trivial glob the grammar admits
	// besides "<dir>/**": "<dir>/**/*.gen.*".
	generatedFilesGlobSuffix = "/**/*.gen.*"

	ruleCheckTimeoutSec     = 120
	commandCheckTimeoutSec  = 300
	lintCheckTimeoutSec     = 600
	scenarioCheckTimeoutSec = 900
	smokeCheckTimeoutSec    = 900
	// The null-subject run's allowances (projectstate.Discriminate*Sec / DevstackBootSec):
	// discriminateRunTimeoutSec is one web scenario's scenario-host run; a Go package's runs are
	// subtests of one `go test` per phase, so a package costs discriminatePackageSec and each of
	// its runs discriminateInProcessRunSec; devstackBootSec is one boot of the composed app on top
	// of a client's run.
	discriminateRunTimeoutSec   = 15
	discriminateInProcessRunSec = 5
	discriminatePackageSec      = 30
	devstackBootSec             = 120

	// reviewCommittedStatus is the committed value of a slot's review status.
	reviewCommittedStatus = 2
	// systemSlotKind is the systemDesign slot's kind ordinal.
	systemSlotKind = 5
)

// devstackDir is the package `appgen compose` writes the local run launcher to.
const devstackDir = "cmd/devstack"

// smokeCheckID names the boot smoke; policyCheckID the composition check that holds the policy to
// the generated requests; installUITestsCheck and surfaceCheckID the web checks.
const (
	smokeCheckID        = checkKindSmoke + ":composition"
	policyCheckID       = "policy-admits-generated-requests"
	installUITestsCheck = "install-uitests"
	surfaceCheckID      = "surface-check"
)

// webProjectDir is the one web project every browser surface of an app shares, at the repo
// root whatever the module dir is.
const webProjectDir = "webApp"

// archGateRun is the -run pattern of the seated gate's tests (internal/aiarch_arch_test.go).
const archGateRun = "^(TestFileLayout|TestGeneratedOnlyPublic|TestMethodLayering|TestMethod|TestProjectModel)$"

// compositionGenerators are appgen's subcommands, in the order a composition root needs:
// temporal and clients write what main.gen.go imports, config the Config it reads.
var compositionGenerators = []string{"temporal", "clients", "config", "compose"}

// webProjectSeats is the web project's own files, shared by every surface: what webgen
// bootstrap writes outside src/, and the lockfile npm writes from it.
var webProjectSeats = []string{
	".gitignore", ".tool-versions", "eslint.config.js", "eslint.platform.config.js", "package.json",
	"package-lock.json", "scripts/build-mcpapps.mjs", "tsconfig.app.json", "tsconfig.json", "vite.config.ts", "webgen.json",
}

// uitestsSeats is the Playwright project's own files.
var uitestsSeats = []string{"uitests/.gitignore", "uitests/.tool-versions", "uitests/package.json", "uitests/package-lock.json"}

// checkKindOrder ranks the check kinds: rule, command, smoke, scenario.
var checkKindOrder = map[string]int{checkKindRule: 0, checkKindCommand: 1, checkKindSmoke: 2, checkKindAdmission: 2, checkKindScenario: 3}

// taskItem is the committed activity item's two facts the scope derivation reads.
type taskItem struct {
	componentID string
	surface     string
}

// taskProject is the project document as the task-spec derivation reads it: the module dir, the
// service contracts, the System (committed, and as stated whatever its status) and the derived
// scenarios, derived once and only when a check asks.
type taskProject struct {
	raw       string // the project document as given: the generators read it
	moduleDir string
	doc       methodcheck.Project
	committed *methodcheck.System // the committed System; nil when slot 5 is not committed
	stated    *methodcheck.System // slot 5's model whatever its status; nil when absent

	derived   bool
	scenarios []scenario.Scenario
	deriveErr error

	emitted map[string]emission // each consumer's emission over this project, by consumer and selection
}

// emission is one consumer's output over the project, or why it produced none.
type emission struct {
	outs []plan.Output
	err  error
}

// decodeTaskProject reads the project document: the platform's own structural mirror for the
// design (the same one methodcheck's rules and testgen derive from), and the System slot.
func decodeTaskProject(project string) (*taskProject, error) {
	if strings.TrimSpace(project) == "" {
		return nil, fweng.New(fweng.ContractMisuse, "DeriveTaskSpec: empty project document")
	}
	doc, ok, err := methodcheck.DecodeProject([]byte(project))
	if err != nil || !ok {
		return nil, fweng.Wrap(fweng.ContractMisuse, err, "DeriveTaskSpec: the project document does not decode")
	}
	var top struct {
		ModuleDir string `json:"moduleDir"`
	}
	if err := json.Unmarshal([]byte(project), &top); err != nil {
		return nil, fweng.Wrap(fweng.ContractMisuse, err, "DeriveTaskSpec: the project document does not decode")
	}
	tp := &taskProject{raw: project, moduleDir: top.ModuleDir, doc: doc, emitted: map[string]emission{}}
	for _, slot := range doc.Slots {
		if slot.Kind != systemSlotKind || len(slot.Model) == 0 {
			continue
		}
		var sys methodcheck.System
		if err := json.Unmarshal(slot.Model, &sys); err != nil {
			return nil, fweng.Wrap(fweng.ContractMisuse, err, "DeriveTaskSpec: the System slot does not decode")
		}
		tp.stated = &sys
		if slot.Status == reviewCommittedStatus {
			tp.committed = &sys
		}
	}
	return tp, nil
}

// derive is the project's canonical scenario set (methodcheck.ScenarioInput, then
// scenario.Derive), computed once.
func (tp *taskProject) derive() ([]scenario.Scenario, error) {
	if tp.derived {
		return tp.scenarios, tp.deriveErr
	}
	tp.derived = true
	in, err := methodcheck.ScenarioInput(tp.doc)
	if err != nil {
		tp.deriveErr = err
		return nil, err
	}
	tp.scenarios, tp.deriveErr = scenario.Derive(in)
	return tp.scenarios, tp.deriveErr
}

// contractKeyOf is the serviceContracts key of the committed System's component: false when the
// System is not committed, does not name the component, or names a Resource (no contract).
func (tp *taskProject) contractKeyOf(componentID string) (string, bool) {
	if tp.committed == nil {
		return "", false
	}
	for _, c := range tp.committed.Components {
		if c.ID == componentID {
			return c.ContractKey, c.ContractKey != ""
		}
	}
	return "", false
}

// componentContracts names the contracts a component owns, in name order (the ONE join,
// methodcheck.ContractsOf). None when the System gives no key.
func (tp *taskProject) componentContracts(componentID string) []string {
	key, ok := tp.contractKeyOf(componentID)
	if !ok {
		return nil
	}
	components := make(map[string]string, len(tp.doc.ServiceContracts))
	for name, sc := range tp.doc.ServiceContracts {
		components[name] = sc.Component
	}
	return methodcheck.ContractsOf(key, components)
}

// systemComponent finds a component of the System by id: the committed one, else the System as
// the slot states it.
func (tp *taskProject) systemComponent(id string) *methodcheck.Component {
	sys := tp.committed
	if sys == nil {
		sys = tp.stated
	}
	if sys == nil {
		return nil
	}
	for i := range sys.Components {
		if sys.Components[i].ID == id {
			return &sys.Components[i]
		}
	}
	return nil
}

// isClientComponent reports whether a component of the System is a Client.
func (tp *taskProject) isClientComponent(id string) bool {
	c := tp.systemComponent(id)
	return c != nil && strings.EqualFold(c.Kind, "client")
}

// clientSurfaceKind parses a Client's stated surface; false for a component that is not a
// Client or states none.
func clientSurfaceKind(c *methodcheck.Component) (methodcheck.SurfaceKind, bool) {
	if c == nil || !strings.EqualFold(c.Kind, "client") {
		return nil, false
	}
	kind, err := methodcheck.ParseSurface(string(c.Surface))
	if err != nil {
		return nil, false
	}
	return kind, true
}

// isMachineSurface reports a surface that is a generated machine client: rest or mcpTools.
func isMachineSurface(s string) bool {
	kind, err := methodcheck.ParseSurface(s)
	if err != nil {
		return false
	}
	switch kind.(type) {
	case methodcheck.RESTSurface, methodcheck.MCPToolsSurface:
		return true
	case methodcheck.WebSurface, methodcheck.MCPAppSurface:
		return false
	}
	return false
}

// contractLayer is a contract's Method layer, from either place a contract states it.
func (tp *taskProject) contractLayer(contract string) string {
	sc := tp.doc.ServiceContracts[contract]
	if sc.Layer != "" {
		return sc.Layer
	}
	return sc.Interface.Layer
}

// modelgenGenerates reports a contract modelgen writes Go for: one with a package, unless it is
// a Client's, whose Go comes from appgen clients.
func (tp *taskProject) modelgenGenerates(contract string) bool {
	return tp.doc.ServiceContracts[contract].GoPackage != "" && !strings.EqualFold(tp.contractLayer(contract), "client")
}

// statePathFromModule is the state document's path from the module dir.
func (tp *taskProject) statePathFromModule() string {
	const doc = ".aiarch/state/project.json"
	if tp.moduleDir == "" {
		return doc
	}
	return strings.Repeat("../", strings.Count(strings.Trim(tp.moduleDir, "/"), "/")+1) + doc
}

// goPackagesOf is the contracts' Go packages (module-relative), sorted and unique.
func (tp *taskProject) goPackagesOf(contracts []string) []string {
	var out []string
	for _, c := range contracts {
		if pkg := tp.doc.ServiceContracts[c].GoPackage; pkg != "" {
			out = append(out, pkg)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// ownedPathsOf is the directories the contracts' components own besides their Go packages
// (module-relative), sorted and unique. They come from the decoded document's contracts; the
// application's decode has already refused an entry that is not a clean directory or that
// overlaps another owner.
func (tp *taskProject) ownedPathsOf(contracts []string) []string {
	var out []string
	for _, c := range contracts {
		for _, p := range tp.doc.ServiceContracts[c].OwnedPaths {
			if _, _, ok := workspaceOwned(p); ok {
				continue // a workspace repo's directory is scoped by workspaceOwnedOf
			}
			out = append(out, strings.Trim(path.Clean(p), "/"))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// workspaceDir is one ownedPaths directory of a declared workspace repo.
type workspaceDir struct{ repo, path string }

// qualified is the directory as the edit-scope grammar and a check's dir spell it: the repo
// chosen by id ("@<repo>/<dir>"), never by a relative path out of the project's repo.
func (d workspaceDir) qualified() string { return "@" + d.repo + "/" + d.path }

// workspaceOwned reads one ownedPaths entry as a workspace repo's directory ("@<repo>/<dir>").
// The application's decode has already refused an entry that does not name a declared repo or
// a clean directory.
func workspaceOwned(entry string) (repo, dir string, ok bool) {
	rest, ok := strings.CutPrefix(entry, "@")
	if !ok {
		return "", "", false
	}
	repo, dir, ok = strings.Cut(rest, "/")
	return repo, dir, ok
}

// workspaceOwnedOf is the workspace-repo directories the contracts' components own, sorted and unique.
func (tp *taskProject) workspaceOwnedOf(contracts []string) []workspaceDir {
	var out []workspaceDir
	for _, c := range contracts {
		for _, p := range tp.doc.ServiceContracts[c].OwnedPaths {
			if repo, dir, ok := workspaceOwned(p); ok {
				out = append(out, workspaceDir{repo: repo, path: strings.Trim(path.Clean(dir), "/")})
			}
		}
	}
	slices.SortFunc(out, func(a, b workspaceDir) int { return strings.Compare(a.qualified(), b.qualified()) })
	return slices.Compact(out)
}

// workspaceGoWorkMarker is the GOWORK assignment of the workspace build check's `env` argv. The
// check runner (aiarch-state-mcp) replaces it with a go.work written over the run's checkouts.
const workspaceGoWorkMarker = "GOWORK=@workspace"

// workspaceChecks are the checks of one workspace-repo directory: each runs in that repo's own
// module (the command runner resolves the "@<repo>/" dir, and always runs GOWORK=off, as the
// platform's modules are checked), so the edit is judged against its own pins.
func workspaceChecks(d workspaceDir) []SpecCheck {
	slug := "ws-" + d.repo + "-" + strings.ReplaceAll(d.path, "/", "-")
	dir := d.qualified()
	return []SpecCheck{
		commandCheck("gofmt-"+slug, "gofmt "+dir, []string{"sh", "-c", `test -z "$(gofmt -l .)"`}, dir, commandCheckTimeoutSec),
		commandCheck("build-"+slug, "go build "+dir, []string{"go", "build", "./..."}, dir, commandCheckTimeoutSec),
		commandCheck("vet-"+slug, "go vet "+dir, []string{"go", "vet", "./..."}, dir, commandCheckTimeoutSec),
		commandCheck("lint-"+slug, "golangci-lint "+dir, []string{"golangci-lint", "run", "./..."}, dir, lintCheckTimeoutSec),
		commandCheck("test-"+slug, "go test "+dir, []string{"go", "test", "./..."}, dir, commandCheckTimeoutSec),
	}
}

// inModule is a module-relative path as a repo-relative one.
func (tp *taskProject) inModule(rel string) string {
	if tp.moduleDir == "" {
		return rel
	}
	return path.Join(tp.moduleDir, rel)
}

func (tp *taskProject) packageGlobs(pkgs []string) []string {
	out := make([]string, len(pkgs))
	for i, pkg := range pkgs {
		out[i] = tp.inModule(pkg) + "/**"
	}
	return out
}

// webDirFromModule is the web project's path from the module dir.
func (tp *taskProject) webDirFromModule() string {
	if tp.moduleDir == "" {
		return webProjectDir
	}
	return strings.Repeat("../", strings.Count(strings.Trim(tp.moduleDir, "/"), "/")+1) + webProjectDir
}

// appgenArgv is `go tool appgen <sub>` over this project's state document.
func (tp *taskProject) appgenArgv(sub string, extra ...string) []string {
	return append([]string{"go", "tool", "appgen", sub, "-project", tp.statePathFromModule(), "-root", "."}, extra...)
}

// isManagerComponent reports whether a component's contract is a Manager's. Its Temporal layer
// is generated from construction on, because the hand-written Manager is built against it.
func (tp *taskProject) isManagerComponent(componentID string) bool {
	return slices.ContainsFunc(tp.componentContracts(componentID), func(c string) bool {
		return strings.EqualFold(tp.contractLayer(c), "manager")
	})
}

// composesServer reports whether integrating these contracts builds the composition root: a
// Manager's or a Client's does, since either changes what the server wires or serves. A
// ResourceAccess or Engine integrates against its callers and leaves the server alone.
func (tp *taskProject) composesServer(contracts []string) bool {
	return slices.ContainsFunc(contracts, func(c string) bool {
		layer := tp.contractLayer(c)
		return strings.EqualFold(layer, "manager") || strings.EqualFold(layer, "client")
	})
}

// clientContractKeys is the serviceContracts key of every Client contract, sorted: the cover
// clients scenario.ForAcceptance projects a path through.
func (tp *taskProject) clientContractKeys() []string {
	var keys []string
	for key, sc := range tp.doc.ServiceContracts {
		if strings.EqualFold(sc.Layer, "client") || strings.EqualFold(sc.Interface.Layer, "client") {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// acceptanceScenarios is the residual acceptance variants themselves, sorted by variant id: what
// the acceptance plan binds. A variant covered by one client projection is not among them.
func (tp *taskProject) acceptanceScenarios() ([]scenario.Scenario, error) {
	all, err := tp.derive()
	if err != nil {
		return nil, err
	}
	clients := tp.clientContractKeys()
	residual := scenario.AcceptanceResidual(all, clients)
	var out []scenario.Scenario
	for _, v := range scenario.ForAcceptance(all, clients) {
		if scenario.IsResidual(residual, v.ID) {
			out = append(out, v)
		}
	}
	return out, nil
}

// ---- the generator manifests (D6) ----

// manifestModulePath stands for the app's go.mod module line, which the document does not carry: a
// generator's output PATHS do not depend on it, only the imports inside the files do.
const manifestModulePath = "example.com/app"

// emit is the output of one consumer (appgen.ConsumerNamed) over the project as a fully built tree
// (Tree nil) with the knobs at their defaults: the manifest of what it writes, with each path's role.
// only restricts modelgen to the named contracts, as a construction task names its own. Memoized, so
// the activity's union over every task of its lifecycle emits each consumer once.
func (tp *taskProject) emit(name string, only []string) ([]plan.Output, error) {
	key := name + "|" + strings.Join(only, ",")
	if e, ok := tp.emitted[key]; ok {
		return e.outs, e.err
	}
	c, ok := appgen.ConsumerNamed(name)
	if !ok {
		err := fmt.Errorf("the generator manifests have no consumer %q", name)
		tp.emitted[key] = emission{err: err}
		return nil, err
	}
	files, err := c.Emit(appgen.Input{Project: []byte(tp.raw), ModulePath: manifestModulePath, ModuleDir: tp.moduleDir, Config: appgen.DefaultConfig(), Only: only})
	outs := plan.Outputs(files)
	if err != nil {
		err = fmt.Errorf("%s: %w", name, err)
	}
	tp.emitted[key] = emission{outs: outs, err: err}
	return outs, err
}

// manifestOf is what the job's generators write for a task: modelgen over the contracts the task
// generates, then each generator its spec runs (SpecGenerator.Title is the consumer's name). A
// browser surface's bootstrap writes every surface's entry, so only the paths under this component's
// own source directory are the task's.
func (tp *taskProject) manifestOf(command, componentID string) ([]plan.Output, error) {
	var out []plan.Output
	if contracts := tp.generatedContractsOf(command, componentID); len(contracts) > 0 {
		outs, err := tp.emit("modelgen", contracts)
		if err != nil {
			return nil, err
		}
		out = append(out, outs...)
	}
	for _, g := range tp.generatorsOf(command, componentID) {
		outs, err := tp.emit(g.Title, nil)
		if err != nil {
			return nil, err
		}
		if g.Title == "webgen bootstrap" {
			own := path.Join(webProjectDir, "src", componentID) + "/"
			outs = slices.DeleteFunc(slices.Clone(outs), func(o plan.Output) bool { return !strings.HasPrefix(o.Path, own) })
		}
		out = append(out, outs...)
	}
	return out, nil
}

// compositionScope is what the composition root's consumers (appgen clients, config, compose) write
// or seat once: the files an integrating task may commit, named by the manifests rather than by a
// glob over generated names, so a generated file no glob covers is no longer possible (FIX-053).
func (tp *taskProject) compositionScope() ([]string, error) {
	var paths []string
	for _, name := range []string{"appgen clients", "appgen config", "appgen compose"} {
		outs, err := tp.emit(name, nil)
		if err != nil {
			return nil, err
		}
		paths = append(paths, plan.Paths(outs, plan.Generated{})...)
		paths = append(paths, plan.Paths(outs, plan.Seam{})...)
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

// scenarioSubjectScope is the test subject the job's `make gen-tests` writes with the generated
// scenario tests (appgen scenariocompose): every integration, whatever its layer, owns the paths
// that consumer's manifest names, because the tests it commits import them. It rides in no other
// scope: a ResourceAccess or Engine integration composes no server yet still needs the subject.
func (tp *taskProject) scenarioSubjectScope() ([]string, error) {
	outs, err := tp.emit("appgen scenariocompose", nil)
	if err != nil {
		return nil, err
	}
	paths := plan.Paths(outs, plan.Generated{})
	paths = append(paths, plan.Paths(outs, plan.Seam{})...)
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

// ---- the glob grammar ----

// validEditScopeGlob reports whether g is in the edit-scope glob grammar the gate matches:
// exactly "**" (every path), "<dir>/**" (every path under a literal dir), "<dir>/**/*.gen.*"
// (every generated file under a literal dir), or a literal repo-relative path
// (projectstate.ValidEditScopeGlob, the gate-side matcher, holds the same grammar).
func validEditScopeGlob(g string) bool {
	if g == anyPath {
		return true
	}
	lit, ok := strings.CutSuffix(g, "/**")
	if !ok {
		lit, _ = strings.CutSuffix(g, generatedFilesGlobSuffix)
	}
	return lit != "" && !strings.ContainsAny(lit, "*?[\\") && !strings.HasPrefix(lit, "/") &&
		!strings.HasSuffix(lit, "/") && !slices.Contains(strings.Split(lit, "/"), "..")
}

// validateEditScope refuses a scope holding a glob outside the edit-scope grammar.
func validateEditScope(scope SpecEditScope) error {
	var globs []string
	for _, c := range scope.Components {
		globs = append(globs, c.Paths...)
	}
	globs = append(globs, scope.Extra...)
	for _, g := range globs {
		if !validEditScopeGlob(g) {
			return fmt.Errorf("edit-scope glob %q is not \"**\", \"<dir>/**\", \"<dir>/**/*.gen.*\" or a literal path", g)
		}
	}
	return nil
}

// ---- the operations ----

// DeriveTaskSpec implements ReviewEngine. A project that is empty or does not decode is
// ContractMisuse; facts naming no lifecycle or task are ContractMisuse; a task whose scope or
// checks cannot be derived from the design (a machine client with no contract, a browser
// surface with no scenario) is InvalidInput with the defect named.
func (reviewEngineImpl) DeriveTaskSpec(_ fweng.Context, project string, facts TaskFacts) (TaskSpec, error) {
	tp, err := decodeTaskProject(project)
	if err != nil {
		return TaskSpec{}, err
	}
	key := facts.LifecycleKey
	lc, ok := methodassets.LifecycleFor(key)
	if !ok {
		return TaskSpec{}, fweng.New(fweng.ContractMisuse, fmt.Sprintf("DeriveTaskSpec: the method assets carry no lifecycle %q", key))
	}
	ms, err := methodassets.MethodSpecFor(key, facts.Task, commandArgs(lc, facts.Task, facts.ComponentID, facts.Activity))
	if err != nil {
		return TaskSpec{}, fweng.Wrap(fweng.ContractMisuse, err, "DeriveTaskSpec")
	}
	item := taskItem{componentID: facts.ComponentID, surface: facts.Surface}
	s := TaskSpec{
		Activity: facts.Activity,
		Task:     facts.Task,
		Identity: SpecIdentity{
			Kind: ms.Kind, Title: ms.Title, Phase: ms.Phase, Command: ms.Command, Agent: ms.Agent,
			WorkerClass: ms.WorkerClass, ArtifactKind: ms.ArtifactKind, ExitCriterion: ms.ExitCriterion, Mode: ms.Mode,
		},
		Prompt: SpecPrompt{Text: ms.Prompt, Tokens: len(ms.Prompt) / 4, Charter: ms.Charter, Skills: slices.Clone(ms.Skills)},
		Tools:  SpecTools{MCP: slices.Clone(ms.MCPTools), Builtin: builtinTools(ms)},
		Model: SpecModel{ID: facts.Model.ID, Source: facts.Model.Source, Estimate: SpecEstimate{
			TokensIn: facts.Model.TokensIn, TokensOut: facts.Model.TokensOut, USD: facts.Model.USD,
		}},
	}
	s.EditScope, s.Checks, err = tp.scopeAndChecks(ms, item, facts.SlotRules)
	if err != nil {
		return TaskSpec{}, fweng.New(fweng.InvalidInput, "DeriveTaskSpec: "+err.Error())
	}
	if err := validateEditScope(s.EditScope); err != nil {
		return TaskSpec{}, fweng.New(fweng.InvalidInput, fmt.Sprintf("DeriveTaskSpec: %s/%s: %s", facts.Activity, facts.Task, err.Error()))
	}
	s.ActivityEditScope = tp.activityEditScope(key, lc, item, facts.Activity, facts.SlotRules)
	s.Contracts = tp.generatedContractsOf(ms.Command, item.componentID)
	s.Generate = tp.generatorsOf(ms.Command, item.componentID)
	outs, err := tp.manifestOf(ms.Command, item.componentID)
	if err != nil {
		return TaskSpec{}, fweng.New(fweng.InvalidInput, fmt.Sprintf("DeriveTaskSpec: %s/%s: the generators' manifests: %s", facts.Activity, facts.Task, err.Error()))
	}
	s.Generated = plan.Paths(outs, plan.Generated{})
	slices.Sort(s.Generated)
	s.Generated = slices.Compact(s.Generated)
	s = normalizedSpec(s)
	s.Hash = specHash(s)
	return s, nil
}

// TaskSpecHash implements ReviewEngine: the sha256 (hex) of the spec's canonical JSON with
// Hash cleared. Field order is struct order, every slice is pre-sorted, and nil slices encode
// as [], so the hash is a function of the spec's content alone and survives a JSON round trip.
func (reviewEngineImpl) TaskSpecHash(_ fweng.Context, spec TaskSpec) (string, error) {
	h := specHash(spec)
	if h == "" {
		return "", fweng.New(fweng.InternalInvariant, "TaskSpecHash: the spec does not encode")
	}
	return h, nil
}

// =====================================================================================
// TIER 1 ADMISSION (FPC Task 6.3, ruling 6) — what the generators make of a recorded artifact.
//
// AdmitArtifact runs, in process and over the project document and the module's appgen config alone, every generator that reads
// the artifact's kind and typechecks what they emit against one another (appgen.AdmitTier1): a
// contract the recorder accepts and a generator cannot load, or two generators that disagree
// about a name, is found when it is recorded, not three tasks later in a paid run. It is pure
// (no clock, no I/O, no toolchain), so the server rail calls it in-workflow; the full build is
// Tier 2, a venue check at commit.
// =====================================================================================

// AdmitArtifact implements ReviewEngine. An empty or undecodable project, or a config that does not
// parse, is ContractMisuse; a kind no generator reads admits with no problems. The config is the
// module's appgen.json: the generators compose the module with it (which variant constructors
// return an error, which operations a client exposes), so admission holds a document to the config
// the module is actually built with, and an empty config means the library's defaults. The go.mod
// names the module path the generators import the module's packages by; empty means the path is
// inferred from the project document (a document whose contracts import none of the module's own
// packages carries no path, and a placeholder stands for it).
func (reviewEngineImpl) AdmitArtifact(_ fweng.Context, project string, kind string, appgenConfig string, goMod string) ([]AdmissionProblem, error) {
	k := appgen.ArtifactKind(kind)
	if len(appgen.Consumers(k)) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(project) == "" {
		return nil, fweng.New(fweng.ContractMisuse, "AdmitArtifact: empty project document")
	}
	var top struct {
		ModuleDir        string                   `json:"moduleDir"`
		ServiceContracts map[string]admitContract `json:"serviceContracts"`
	}
	if err := json.Unmarshal([]byte(project), &top); err != nil {
		return nil, fweng.Wrap(fweng.ContractMisuse, err, "AdmitArtifact: the project document does not decode")
	}
	cfg := appgen.DefaultConfig()
	if strings.TrimSpace(appgenConfig) != "" {
		parsed, err := appgen.ParseConfig([]byte(appgenConfig))
		if err != nil {
			return nil, fweng.Wrap(fweng.ContractMisuse, err, "AdmitArtifact: the appgen config does not parse")
		}
		cfg = parsed
	}
	module := inferModulePath(top.ServiceContracts)
	if strings.TrimSpace(goMod) != "" {
		m, ok := goModModule(goMod)
		if !ok {
			return nil, fweng.New(fweng.ContractMisuse, "AdmitArtifact: the go.mod has no module line")
		}
		module = m
	}
	in := appgen.Input{Project: []byte(project), ModulePath: module, ModuleDir: top.ModuleDir, Config: cfg}
	var out []AdmissionProblem
	for _, p := range appgen.AdmitTier1(in, k) {
		out = append(out, AdmissionProblem{Consumer: p.Consumer, Path: p.Path, Message: p.Message, Severity: admissionSeverity(p.Severity), Tier: AdmissionTier1})
	}
	return out, nil
}

// goModModule is the module line of a go.mod document.
func goModModule(goMod string) (string, bool) {
	for _, line := range strings.Split(goMod, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			if m := strings.Trim(strings.TrimSpace(rest), `"`); m != "" {
				return m, true
			}
		}
	}
	return "", false
}

// admitContract is the part of a service contract the module path is read from.
type admitContract struct {
	GoPackage string `json:"goPackage"`
	Deps      []struct {
		GoImport string `json:"goImport"`
	} `json:"deps"`
}

// admissionSeverity maps the generator library's sum type onto the contract's enum. The switch
// has no default: a new Severity cannot fall silently into the branch of the old two.
func admissionSeverity(s appgen.Severity) AdmissionSeverity {
	switch s.(type) {
	case appgen.Local:
		return AdmissionLocal
	case appgen.Incomplete:
		return AdmissionIncomplete
	}
	panic("unreachable: sealed appgen.Severity")
}

// inferModulePath is the app's go.mod module line as far as the project document says it: the
// document does not carry it, but a dependency's goImport of a sibling contract's package is
// "<module>/<goPackage>". The generators write the module into every import they emit, so a
// wrong one makes a sibling a stranger to the contract that imports it by its real name. A
// document that names no sibling import falls back to the placeholder the manifests use.
func inferModulePath(contracts map[string]admitContract) string {
	keys := slices.Sorted(maps.Keys(contracts))
	for _, key := range keys {
		for _, dep := range contracts[key].Deps {
			for _, other := range keys {
				pkg := contracts[other].GoPackage
				if pkg == "" || dep.GoImport == "" {
					continue
				}
				if module, ok := strings.CutSuffix(dep.GoImport, "/"+pkg); ok && module != "" {
					return module
				}
			}
		}
	}
	return manifestModulePath
}

func specHash(s TaskSpec) string {
	c := normalizedSpec(s)
	c.Hash = ""
	raw, err := json.Marshal(c)
	if err != nil {
		// Unreachable: every field is a string, bool, integer, finite float or slice of them.
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// normalizedSpec returns a copy whose non-omitempty slices are non-nil, so a spec built here
// and the same spec decoded from JSON encode identically.
func normalizedSpec(s TaskSpec) TaskSpec {
	s.Prompt.Skills = nonNil(s.Prompt.Skills)
	s.Tools.MCP = nonNil(s.Tools.MCP)
	s.Tools.Builtin = nonNil(s.Tools.Builtin)
	s.EditScope = normalizedScope(s.EditScope)
	s.ActivityEditScope = normalizedScope(s.ActivityEditScope)
	if s.Checks == nil {
		s.Checks = []SpecCheck{}
	}
	return s
}

func normalizedScope(sc SpecEditScope) SpecEditScope {
	sc.Extra = nonNil(sc.Extra)
	comps := make([]SpecScopeComponent, len(sc.Components))
	for i, c := range sc.Components {
		c.Paths = nonNil(c.Paths)
		comps[i] = c
	}
	sc.Components = comps
	return sc
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// strOrNil is the optional string a SpecCheckSource carries: nil when empty.
func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isDesignMode reports whether a manifest mode is a design-slot job mode.
func isDesignMode(mode string) bool {
	return mode == "draft" || mode == "critique" || mode == "answer"
}

// commandArgs is the task command's $ARGUMENTS, exactly as the jobs pass them: design
// commands take none, construction-style commands take <component_id> <activity_id>, and a
// command of an activity that builds no component (the acceptance test plan) takes <activity_id>.
func commandArgs(lc methodassets.Lifecycle, taskID, componentID, activityID string) []string {
	for _, t := range lc.Tasks {
		if t.ID != taskID || t.Command == "" {
			continue
		}
		if m, ok := methodassets.ManifestFor(t.Command); ok && isDesignMode(m.Mode) {
			return nil
		}
		if componentID == "" {
			return []string{activityID}
		}
		return []string{componentID, activityID}
	}
	return nil
}

// builtinTools is the manifest's built-in allowlist plus Skill whenever the step seats
// skills: the --tools allowlist would otherwise block loading them. Sorted, unique.
func builtinTools(ms methodassets.MethodSpec) []string {
	out := slices.Clone(ms.BuiltinTools)
	if len(ms.Skills) > 0 {
		out = append(out, "Skill")
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// activityEditScope is the union of the edit scopes of every task of lc (FIX-040): each
// component's paths and every extra glob any task may change, sorted and unique, and
// StateOnly only when every task is state-only. It is a pure function of the project, the
// activity and the embedded method assets, so the server and the job derive the same one.
//
// A task whose scope does not resolve YET contributes nothing rather than failing this task's
// spec: a machine client's construction scope, for one, resolves only once its Manager's
// contract is recorded, and its earlier tasks must still dispatch. Contributing nothing can
// only make the union stricter, and a task that has not resolved has not run, so the branch
// holds none of its edits.
func (tp *taskProject) activityEditScope(key string, lc methodassets.Lifecycle, item taskItem, activityID string, slot SlotRules) SpecEditScope {
	union := SpecEditScope{StateOnly: true}
	paths := map[string]map[string]bool{}
	extra := map[string]bool{}
	for _, t := range lc.Tasks {
		ms, err := methodassets.MethodSpecFor(key, t.ID, commandArgs(lc, t.ID, item.componentID, activityID))
		if err != nil {
			continue
		}
		scope, _, err := tp.scopeAndChecks(ms, item, slot)
		if err != nil || scope.StateOnly || validateEditScope(scope) != nil {
			continue
		}
		union.StateOnly = false
		for _, c := range scope.Components {
			if paths[c.ID] == nil {
				paths[c.ID] = map[string]bool{}
			}
			for _, g := range c.Paths {
				paths[c.ID][g] = true
			}
		}
		for _, g := range scope.Extra {
			extra[g] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(paths)) {
		union.Components = append(union.Components, SpecScopeComponent{ID: id, Paths: slices.Sorted(maps.Keys(paths[id]))})
	}
	union.Extra = slices.Sorted(maps.Keys(extra))
	return union
}

// scopeAndChecks derives the task's edit scope and checks from its identity, then adds the Tier 2
// admission check to the tasks whose merge commits an artifact the generators read.
func (tp *taskProject) scopeAndChecks(ms methodassets.MethodSpec, item taskItem, slot SlotRules) (SpecEditScope, []SpecCheck, error) {
	scope, checks, err := tp.scopeAndChecksBase(ms, item, slot)
	if err != nil || !commitsAdmittedArtifact(ms) {
		return scope, checks, err
	}
	checks = append(slices.Clone(checks), SpecCheck{
		ID: admissionCheckID, Kind: checkKindAdmission, Title: "The project builds through the conformance chain (Tier 2)",
		TimeoutSec: admissionCheckTimeoutSec,
	})
	sortChecks(checks)
	return scope, checks, nil
}

// commitsAdmittedArtifact reports whether a task is one whose merge commits an artifact the
// generators read: a component's or surface's detailed design (its service contract, or its UI
// design), its test plan and the acceptance plan. Its record verbs ran Tier 1 each time the
// artifact was written; the merge is where the full build chain (Tier 2) holds it. A review
// decides and edits nothing, a job runs no agent, and construction and integration tasks build
// code the checks they already carry run.
func commitsAdmittedArtifact(ms methodassets.MethodSpec) bool {
	if ms.Command == "" || ms.Kind != methodassets.LifecycleTaskDispatch || isDesignMode(ms.Mode) {
		return false
	}
	return ms.Phase == "detailed_design" || ms.Phase == "test_plan" || ms.Phase == "acceptance_plan"
}

// scopeAndChecksBase is the scope and checks a task's own identity gives it.
func (tp *taskProject) scopeAndChecksBase(ms methodassets.MethodSpec, item taskItem, slot SlotRules) (SpecEditScope, []SpecCheck, error) {
	stateOnly := SpecEditScope{StateOnly: true}
	switch {
	case ms.Command == "":
		// A review a human decides: nothing to run, nothing to edit.
		return stateOnly, nil, nil
	case ms.Kind == methodassets.LifecycleTaskJob:
		// A venue job edits no repo file and runs no agent: the venue runs its checks.
		return stateOnly, tp.acceptanceJobChecks(), nil
	case isDesignMode(ms.Mode):
		return stateOnly, []SpecCheck{slotRuleCheck(ms.ArtifactKind, slot)}, nil
	case ms.Phase == "test_plan":
		return stateOnly, tp.testPlanChecks(item.componentID), nil
	case ms.Phase == "acceptance_plan":
		return stateOnly, []SpecCheck{atpRuleCheck()}, nil
	case ms.Phase == "requirements" || ms.Phase == "detailed_design" || ms.Kind == methodassets.LifecycleTaskReview:
		return stateOnly, []SpecCheck{projectRuleCheck()}, nil
	}
	contracts := tp.componentContracts(item.componentID)
	switch ms.Command {
	case "service-construction", "service-integration":
		pkgs := tp.goPackagesOf(contracts)
		if len(pkgs) == 0 && isMachineSurface(item.surface) {
			return SpecEditScope{}, nil, fmt.Errorf("machine client %q has no contract: it is derived when the contract of the Manager its views call is recorded, so that Manager must be designed first and the views must draw the call", item.componentID)
		}
		if len(pkgs) == 0 {
			break // no Go package to scope or check: the underivable posture below
		}
		return tp.serviceScopeAndChecks(ms.Command == "service-integration", item.componentID, pkgs, contracts)
	case "frontend-construction", "frontend-integration":
		return tp.frontendScopeAndChecks(ms.Command == "frontend-integration", item.componentID, contracts)
	case "deployment-integration":
		// The rollout is the composed app's too: it boots through its run contract before it
		// converges anywhere. Its scope stays explicit "any path", as every deployment task's.
		checks := append([]SpecCheck{projectRuleCheck()}, bootChecks(tp.moduleDir)...)
		sortChecks(checks)
		return SpecEditScope{Extra: []string{anyPath}}, checks, nil
	}
	// A construct-mode task whose scope cannot be derived (deployment, documentation, the
	// testing variants, a service with no Go package) changes repo files no rule names, so
	// its scope is an explicit "any path" rather than a stateOnly that would reject its
	// legitimate output.
	return SpecEditScope{Extra: []string{anyPath}}, []SpecCheck{projectRuleCheck()}, nil
}

// testPlanChecks is the test_plan task's rule checks: one per contract the component owns, each
// naming the plan it requires, so a contract with no recorded test plan fails its check rather
// than passing vacuously. A component with no contract names no plan to require, so it keeps the
// single unscoped check.
func (tp *taskProject) testPlanChecks(componentID string) []SpecCheck {
	contracts := tp.componentContracts(componentID)
	if len(contracts) == 0 {
		return []SpecCheck{tpRuleCheck("")}
	}
	checks := make([]SpecCheck, 0, len(contracts))
	for _, c := range contracts {
		checks = append(checks, tpRuleCheck(c))
	}
	sortChecks(checks)
	return checks
}

// tpRuleCheck is the test plan's rule check (TP-*). Given a contract it requires that contract's
// recorded plan (a missing one fails) and runs the TP-* findings over it; given "" it is the
// unscoped check over the whole document, for a component that owns no contract.
func tpRuleCheck(contract string) SpecCheck {
	if contract == "" {
		return SpecCheck{
			ID: "rule:tp", Kind: checkKindRule, Title: "Test plan rules (TP-*)",
			Source: SpecCheckSource{RulePrefixes: []string{"TP-"}}, TimeoutSec: ruleCheckTimeoutSec,
		}
	}
	return SpecCheck{
		ID: "rule:tp:" + contract, Kind: checkKindRule, Title: "Test plan rules (TP-*) for " + contract,
		Source: SpecCheckSource{RulePrefixes: []string{"TP-"}, Contract: &contract}, TimeoutSec: ruleCheckTimeoutSec,
	}
}

// atpRuleCheck is the acceptance plan's rule check (ATP-*, with TP-* over every bound step). The
// plan's own task is gated by it, and the milestone's job runs it again at run time, where a skip
// whose `until` activity has since landed is an Error.
func atpRuleCheck() SpecCheck {
	return SpecCheck{
		ID: "rule:atp", Kind: checkKindRule, Title: "Acceptance plan rules (ATP-*, TP-*)",
		Source: SpecCheckSource{RulePrefixes: []string{"ATP-", "TP-"}}, TimeoutSec: ruleCheckTimeoutSec,
	}
}

// acceptanceJobChecks is the checks of the M-ACCEPT job: the acceptance plan's rules, the composed
// app's boot (the composition root and the run contract build, and the app boots through it), the
// generated tests' currency, and one scenario check per residual acceptance variant, filed under the
// pseudo-contract acceptanceContract. A project whose derivation does not resolve has no scenario
// to run: the rule check then names why.
func (tp *taskProject) acceptanceJobChecks() []SpecCheck {
	dir := tp.moduleDir
	checks := []SpecCheck{atpRuleCheck()}
	checks = append(checks, bootChecks(dir)...)
	checks = append(checks, commandCheck("gen-tests-check", "Generated tests are current", []string{"make", "gen-tests-check"}, dir, commandCheckTimeoutSec))
	scenarios, err := tp.acceptanceScenarios()
	if err == nil {
		for _, v := range scenarios {
			checks = append(checks, SpecCheck{
				ID: checkKindScenario + ":" + acceptanceContract + ":" + v.ID, Kind: checkKindScenario, Title: v.Title,
				Source:     SpecCheckSource{Scenario: strOrNil(v.ID), Contract: strOrNil(acceptanceContract), UseCase: strOrNil(v.UseCase)},
				TimeoutSec: scenarioCheckTimeoutSec,
			})
		}
	}
	sortChecks(checks)
	return checks
}

// slotRuleCheck is a design task's rule check: the rule families attributed to the slot it
// writes, under that slot's severity policy. A kind that names no slot falls back to the
// whole-project check.
func slotRuleCheck(artifactKind string, slot SlotRules) SpecCheck {
	if artifactKind == "" || slot.Slot == "" {
		return projectRuleCheck()
	}
	prefixes := slot.Prefixes
	scope := "whole project"
	if len(prefixes) > 0 {
		globs := make([]string, len(prefixes))
		for i, pre := range prefixes {
			globs[i] = pre + "*"
		}
		scope = strings.Join(globs, ", ")
	}
	return SpecCheck{
		ID: checkKindRule + ":" + slot.Slot, Kind: checkKindRule,
		Title:      artifactKind + " rules (" + scope + ")",
		Source:     SpecCheckSource{RulePrefixes: prefixes, RuleSlot: strOrNil(slot.Slot)},
		TimeoutSec: ruleCheckTimeoutSec,
	}
}

// projectRuleCheck is the whole-document rule check.
func projectRuleCheck() SpecCheck {
	return SpecCheck{ID: "rule:project", Kind: checkKindRule, Title: "Method rules (whole project)", TimeoutSec: ruleCheckTimeoutSec}
}

// generatedContractsOf is the contracts a service task's job generates, sorted: the
// component's own recorded contracts that have a Go package to land in.
func (tp *taskProject) generatedContractsOf(command, componentID string) []string {
	if command != "service-construction" && command != "service-integration" {
		return nil
	}
	var out []string
	for _, c := range tp.componentContracts(componentID) {
		if tp.modelgenGenerates(c) {
			out = append(out, c)
		}
	}
	return out
}

func commandCheck(id, title string, argv []string, dir string, timeout int) SpecCheck {
	return SpecCheck{ID: checkKindCommand + ":" + id, Kind: checkKindCommand, Title: title,
		Source: SpecCheckSource{Argv: argv, Dir: strOrNil(dir)}, TimeoutSec: timeout}
}

// serviceScopeAndChecks is a Go component's construction (integration adds the
// composition root, the generated-tests drift check and the scenario checks).
func (tp *taskProject) serviceScopeAndChecks(integration bool, componentID string, pkgs, contracts []string) (SpecEditScope, []SpecCheck, error) {
	// The component owns its Go packages and the directories its contracts name as owned.
	pkgs = append(slices.Clone(pkgs), tp.ownedPathsOf(contracts)...)
	slices.Sort(pkgs)
	pkgs = slices.Compact(pkgs)
	globs := tp.packageGlobs(pkgs)
	workspace := tp.workspaceOwnedOf(contracts)
	for _, d := range workspace {
		globs = append(globs, d.qualified()+"/**")
	}
	scope := SpecEditScope{
		Components: []SpecScopeComponent{{ID: componentID, Paths: globs}},
		Extra:      []string{tp.inModule("go.mod"), tp.inModule("go.sum")},
	}
	dir := tp.moduleDir
	trees := make([]string, len(pkgs))
	for i, pkg := range pkgs {
		trees[i] = "./" + pkg + "/..."
	}
	checks := []SpecCheck{
		commandCheck("gofmt", "gofmt", []string{"sh", "-c", `test -z "$(gofmt -l ` + strings.Join(pkgs, " ") + `)"`}, dir, commandCheckTimeoutSec),
		commandCheck("build", "go build", append([]string{"go", "build"}, trees...), dir, commandCheckTimeoutSec),
		commandCheck("vet", "go vet", append([]string{"go", "vet"}, trees...), dir, commandCheckTimeoutSec),
		commandCheck("lint", "golangci-lint", append([]string{"golangci-lint", "run"}, trees...), dir, lintCheckTimeoutSec),
		archCheck(dir),
		commandCheck("tidy", "The module graph is tidy", []string{"go", "mod", "tidy", "-diff"}, dir, commandCheckTimeoutSec),
	}
	if len(workspace) > 0 {
		for _, d := range workspace {
			checks = append(checks, workspaceChecks(d)...)
		}
		// The workspace build: the project's own module builds against the workspace repos'
		// edits, where every other check judges the released pins. The marker is resolved by
		// the check runner into a go.work over the run's own checkouts (absolute paths: the
		// project's committed go.work names its siblings relative to a layout a run does not have).
		checks = append(checks, commandCheck("build-workspace", "The project builds against its workspace repos' edits",
			[]string{"env", workspaceGoWorkMarker, "go", "build", "./..."}, dir, commandCheckTimeoutSec))
	}
	if slices.ContainsFunc(contracts, tp.modelgenGenerates) {
		checks = append(checks, tp.genModelsCheck(contracts, dir))
	}
	if tp.isManagerComponent(componentID) {
		checks = append(checks, commandCheck("gen-temporal-check", "appgen temporal output is current", tp.appgenArgv("temporal", "-check"), dir, commandCheckTimeoutSec))
	}
	if integration {
		// An agent writes no test code (GC-1, ARCH-TEST-COMPOSE): the scope holds the wiring
		// (the composition root's seeded hooks.go and what appgen writes) and none of the
		// generated scenario tests, which the job generates from the reviewed plan.
		scope.Extra = append(scope.Extra, tp.inModule("cmd/server/hooks.go"))
		subject, err := tp.scenarioSubjectScope()
		if err != nil {
			return SpecEditScope{}, nil, err
		}
		scope.Extra = append(scope.Extra, subject...)
		checks = append(checks,
			commandCheck("gen-tests-check", "Generated tests are current", []string{"make", "gen-tests-check"}, dir, commandCheckTimeoutSec),
			// TP-DISCRIMINATE: every scenario must fail once its operation is nulled.
			commandCheck("test-discriminate", "Every scenario fails against a null subject", []string{"make", "test-discriminate"}, dir, tp.discriminateTimeoutSec()))
		if tp.composesServer(contracts) {
			composed, err := tp.compositionScope()
			if err != nil {
				return SpecEditScope{}, nil, err
			}
			scope.Extra = append(scope.Extra, composed...)
			checks = append(checks, tp.compositionChecks(dir)...)
		}
		scen, err := tp.scenarioChecks(contracts)
		if err != nil {
			return SpecEditScope{}, nil, err
		}
		if len(scen) == 0 && tp.isClientComponent(componentID) {
			return SpecEditScope{}, nil, fmt.Errorf("machine client %q has no scenario to integrate against: check the System's dynamic views have an actor call it and it call its Manager in that step", componentID)
		}
		checks = append(checks, scen...)
	}
	slices.Sort(scope.Extra)
	scope.Extra = slices.Compact(scope.Extra)
	sortChecks(checks)
	return scope, checks, nil
}

// compositionChecks hold the generated composition root to the design and require it to build.
// The build discards its binary: `go build` of one main package writes it to the working
// directory, where an agent's `git add -A` would commit it and fail its own edit scope.
//
// The policy check runs the framework's generated TestPolicyAdmitsEveryGeneratedRequest
// (cmd/server/authz.gen_test.go): the composition root's policy must admit the local dev
// principal every (verb, own scope) request the generated REST handlers and MCP tools send it.
// Every task that composes the server runs it, so a policy refusing one of them is red in the
// activity that owns hooks.go, and no machine client passes a policy that fails only on the
// other transport (RUN-015: a catalog-only policy 403'd every per-todo call, first seen at the
// SPA, which may not edit hooks.go).
func (tp *taskProject) compositionChecks(dir string) []SpecCheck {
	out := bootChecks(dir)
	for _, sub := range compositionGenerators {
		if sub == "temporal" {
			continue // a Manager's task already checks it; a Client has no Temporal layer of its own
		}
		out = append(out, commandCheck("gen-"+sub+"-check", "appgen "+sub+" output is current", tp.appgenArgv(sub, "-check"), dir, commandCheckTimeoutSec))
	}
	return append(out, commandCheck(policyCheckID, "The composition root's policy admits every request its generated transports send",
		[]string{"go", "test", "./cmd/server/", "-run", "^TestPolicyAdmitsEveryGeneratedRequest$"}, dir, commandCheckTimeoutSec))
}

// bootChecks are what makes an integration the integration of the composed app: the composition
// root and the run contract build, and the app then boots through that run contract (the smoke:
// /readyz, the SPA and the MCP tool table the model generates, then a teardown that leaves nothing
// behind). The two builds stay as sub-steps the smoke needs green first; the launcher acceptance
// boots is compiled in the activity that owns it, not first at acceptance.
func bootChecks(dir string) []SpecCheck {
	return []SpecCheck{
		commandCheck("build-server", "The composition root builds", []string{"go", "build", "-o", "/dev/null", "./cmd/server/"}, dir, commandCheckTimeoutSec),
		commandCheck("build-devstack", "The local run contract builds", []string{"go", "build", "-o", "/dev/null", "./" + devstackDir + "/"}, dir, commandCheckTimeoutSec),
		{ID: smokeCheckID, Kind: checkKindSmoke, Title: "The composed app boots through its run contract and answers /readyz, the SPA and /mcp",
			Source: SpecCheckSource{Dir: strOrNil(dir)}, TimeoutSec: smokeCheckTimeoutSec},
	}
}

// generatorsOf is what the job generates before the agent runs a task, beyond its contracts.
func (tp *taskProject) generatorsOf(command, componentID string) []SpecGenerator {
	var out []SpecGenerator
	switch command {
	case "service-construction":
		if tp.isManagerComponent(componentID) {
			out = append(out, SpecGenerator{Title: "appgen temporal", Argv: tp.appgenArgv("temporal")})
		}
	case "frontend-construction", "frontend-integration":
		web := tp.webDirFromModule()
		// The surface kind seats the entry the surface's host needs: a web page, or an MCP Apps
		// host-bridge seat. A Client with no stated surface passes none, so webgen refuses it.
		bootstrap := []string{"go", "tool", "webgen", "bootstrap", "-C", web, "-surface", componentID}
		if kind, ok := clientSurfaceKind(tp.systemComponent(componentID)); ok {
			bootstrap = append(bootstrap, "-kind", string(kind.Surface()))
		}
		out = append(out,
			SpecGenerator{Title: "webgen bootstrap", Argv: bootstrap},
			SpecGenerator{Title: "webgen generate", Argv: []string{"go", "tool", "webgen", "generate", "-C", web}},
		)
	case "service-integration":
		if tp.composesServer(tp.componentContracts(componentID)) {
			out = append(out, tp.compositionGeneratorsSpec()...)
		}
	}
	return out
}

// compositionGeneratorsSpec runs every generator the composition root needs.
func (tp *taskProject) compositionGeneratorsSpec() []SpecGenerator {
	out := make([]SpecGenerator, 0, len(compositionGenerators))
	for _, sub := range compositionGenerators {
		out = append(out, SpecGenerator{Title: "appgen " + sub, Argv: tp.appgenArgv(sub)})
	}
	return out
}

// archCheck is the seated gate, the same argv in every app (internal/aiarch_arch_test.go):
// file layout, the generated-only public surface, layering and the dependency menu, the
// design rules with design-to-code alignment, and a state every generator loads.
func archCheck(dir string) SpecCheck {
	c := commandCheck("arch", "Architecture gates (file layout, public surface, layering, design alignment, loadable state)",
		[]string{"go", "test", "-count=1", "-v", "-run", archGateRun, "./internal/"}, dir, commandCheckTimeoutSec)
	yes := true
	c.Source.RequireTests = &yes
	return c
}

// genModelsCheck holds the component's generated Go to its contract. The drift gate names
// the component's own contracts, so a package with no contract.gen.go is red rather than
// skipped by the surface gate.
func (tp *taskProject) genModelsCheck(contracts []string, dir string) SpecCheck {
	argv := []string{"go", "tool", "modelgen", "-project", tp.statePathFromModule(), "-root", ".", "-check"}
	for _, c := range contracts {
		if tp.modelgenGenerates(c) {
			argv = append(argv, "-contract", c)
		}
	}
	return commandCheck("gen-models-check", "Generated models are current", argv, dir, commandCheckTimeoutSec)
}

// surfaceEntryFile is the root HTML entry webgen bootstrap seats for a browser surface: a web
// page is <id>.html, an MCP Apps shell is <id>.mcpapp.html (the page build ignores it).
func (tp *taskProject) surfaceEntryFile(componentID string) string {
	if kind, ok := clientSurfaceKind(tp.systemComponent(componentID)); ok {
		if _, isApp := kind.(methodcheck.MCPAppSurface); isApp {
			return componentID + ".mcpapp.html"
		}
	}
	return componentID + ".html"
}

// frontendScopeAndChecks is a browser surface's construction. All surfaces share one web
// project: each owns webApp/src/<id>/ and its entry, they share webApp/src/shared/, and the
// transport seam under src/api and src/contracts is the generators'. Integration adds the UI
// tests (the purge step deletes hand-written specs, so all of uitests/), the Go packages of any
// of the component's contracts that has one, the generated-tests drift check and the scenario
// checks.
func (tp *taskProject) frontendScopeAndChecks(integration bool, componentID string, contracts []string) (SpecEditScope, []SpecCheck, error) {
	surface := []string{path.Join(webProjectDir, "src", componentID) + "/**", path.Join(webProjectDir, "src", "shared") + "/**"}
	slices.Sort(surface)
	scope := SpecEditScope{
		Components: []SpecScopeComponent{{ID: componentID, Paths: surface}},
		Extra: []string{
			path.Join(webProjectDir, tp.surfaceEntryFile(componentID)),
			path.Join(webProjectDir, "src", "api") + "/**",
			path.Join(webProjectDir, "src", "contracts") + "/**",
			path.Join(webProjectDir, "src", "vite-env.d.ts"),
		},
	}
	for _, seat := range webProjectSeats {
		scope.Extra = append(scope.Extra, path.Join(webProjectDir, seat))
	}
	scope.Extra = append(scope.Extra, uitestsSeats...)
	checks := []SpecCheck{
		commandCheck("install", "The web project installs from its lockfile", []string{"npm", "ci", "--ignore-scripts"}, webProjectDir, commandCheckTimeoutSec),
		commandCheck("gen-ops-check", "The generated transport is current", []string{"go", "tool", "webgen", "check", "-C", tp.webDirFromModule()}, tp.moduleDir, commandCheckTimeoutSec),
		commandCheck("typecheck", "TypeScript typecheck", []string{"npm", "run", "typecheck"}, webProjectDir, commandCheckTimeoutSec),
		commandCheck("lint", "ESLint", []string{"npx", "eslint", "src"}, webProjectDir, commandCheckTimeoutSec),
	}
	if integration {
		paths := append(slices.Clone(surface), tp.packageGlobs(tp.goPackagesOf(contracts))...)
		slices.Sort(paths)
		scope.Components[0].Paths = paths
		// The UI tests are generated from the typed UI design and the plan, never written
		// (GC-1): the scope holds the surface's code and the Playwright project's manifests
		// (uitestsSeats), and none of uitests/generated.
		checks = append(checks,
			commandCheck(installUITestsCheck, "The UI tests install", []string{"sh", "-c", "npm ci --ignore-scripts && npx playwright install chromium"}, "uitests", commandCheckTimeoutSec),
			commandCheck("gen-tests-check", "Generated tests are current", []string{"make", "gen-tests-check"}, tp.moduleDir, commandCheckTimeoutSec))
		for _, c := range contracts {
			if tp.doc.ServiceContracts[c].GoPackage != "" {
				continue
			}
			// The generated surface check: every control the design names is on the page with
			// the role and accessible name the design gives it. It runs under the scenario host
			// (go tool webscenarios), which boots the composed app into the host's substrates and
			// tears everything down itself: Playwright's own webServer would SIGKILL the app and
			// leak its Postgres container.
			checks = append(checks, commandCheck(surfaceCheckID, "The surface has every designed element",
				webScenariosArgv(tp.moduleDir, c, "-project", "surface"),
				tp.moduleDir, scenarioCheckTimeoutSec))
		}
		// The browser surface's integration is the app's too: its page is served by the composed
		// server, so the smoke boots it and holds the page and the tool table.
		checks = append(checks, tp.compositionChecks(tp.moduleDir)...)
		scen, err := tp.scenarioChecks(contracts)
		if err != nil {
			return SpecEditScope{}, nil, err
		}
		if len(scen) == 0 {
			// An empty gate must not pass silently: every operation of a browser surface is
			// derived from a label a use case's view carries, so none means a design defect.
			return SpecEditScope{}, nil, fmt.Errorf("browser surface %q has no scenario to integrate against: record its UI design, so its contract is derived from the actor calls into it, and check the System's dynamic views call it (SYS-CLIENT-OP-LABEL)", componentID)
		}
		checks = append(checks, scen...)
	}
	slices.Sort(scope.Extra)
	scope.Extra = slices.Compact(scope.Extra)
	sortChecks(checks)
	return scope, checks, nil
}

// webScenariosArgv is the command that runs a browser surface's generated Playwright project under
// the scenario host (`go tool webscenarios`), from the module directory (projectstate.WebScenariosArgv,
// the gate side's, builds the same one): -C is the repo-root `uitests` project as that directory
// reaches it, -config the contract's generated config, and the rest the Playwright selection.
func webScenariosArgv(moduleDir, contract string, selection ...string) []string {
	ui, err := filepath.Rel(moduleDir, "uitests")
	if err != nil {
		ui = "uitests"
	}
	argv := []string{"go", "tool", "webscenarios", "-C", filepath.ToSlash(ui), "-config", "generated/" + contract + "/playwright.config.gen.ts"}
	return append(argv, selection...)
}

// discriminateTimeoutSec is the budget of the null-subject run. testgen's -discriminate runs every
// scenario of the module once on its real subject and once per stimulus operation with that
// operation nulled. A Go scenario's runs are subtests of one `go test` process per package and
// phase, so each Go package costs discriminatePackageSec and each of its runs
// discriminateInProcessRunSec; a machine client's run also launches the composed app and nulls the
// Manager operation a call reaches in it, so it adds one devstackBootSec; a web scenario's run is
// still one scenario-host process (discriminateRunTimeoutSec plus a boot). Never less than a
// scenario check's.
func (tp *taskProject) discriminateTimeoutSec() int {
	all, err := tp.derive()
	if err != nil {
		return scenarioCheckTimeoutSec
	}
	pkgRuns := map[string]int{} // Go package -> runs it holds, before a client's boot allowance
	budget := 0
	for key, sc := range tp.doc.ServiceContracts {
		runs := 0
		for _, s := range scenario.ForComponent(all, key) {
			runs += 1 + len(s.Stimuli)
		}
		client := strings.EqualFold(tp.contractLayer(key), "client")
		switch {
		case client && sc.GoPackage == "": // a web scenario: one scenario-host process per run
			budget += runs * (discriminateRunTimeoutSec + devstackBootSec)
		case client: // a machine client: in-process runs, each launching the app
			pkgRuns[sc.GoPackage] += runs
			budget += runs * devstackBootSec
		case sc.GoPackage != "":
			pkgRuns[sc.GoPackage] += runs
		}
	}
	for _, runs := range pkgRuns {
		budget += discriminatePackageSec + runs*discriminateInProcessRunSec
	}
	return max(scenarioCheckTimeoutSec, budget)
}

// scenarioChecks is one check per scenario projected onto each contract.
func (tp *taskProject) scenarioChecks(contracts []string) ([]SpecCheck, error) {
	var out []SpecCheck
	for _, contract := range contracts {
		all, err := tp.derive()
		if err != nil {
			return nil, fmt.Errorf("scenarios of %s: %w", contract, err)
		}
		for _, sc := range scenario.ForComponent(all, contract) {
			out = append(out, SpecCheck{
				ID: checkKindScenario + ":" + contract + ":" + sc.ID, Kind: checkKindScenario, Title: sc.Title,
				Source:     SpecCheckSource{Scenario: strOrNil(sc.ID), Contract: strOrNil(contract), UseCase: strOrNil(sc.UseCase)},
				TimeoutSec: scenarioCheckTimeoutSec,
			})
		}
	}
	return out, nil
}

// sortChecks orders checks by kind (rule, command, smoke, scenario), then id.
func sortChecks(checks []SpecCheck) {
	slices.SortStableFunc(checks, func(a, b SpecCheck) int {
		if d := checkKindOrder[a.Kind] - checkKindOrder[b.Kind]; d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})
}
