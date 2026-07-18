// Package handoff implements the handOffEngine component — the Engine that
// encapsulates the HandOffPolicy volatility: WHICH worker class (AI vs human-senior
// vs human-junior vs architect-only) fills the worker role for one construction
// activity, per the project's committed hand-off policy.
//
// Contract: methodpoc/designs/aiarch/implementation/contracts/handOffEngine.md
// (APPROVED — FROZEN 2026-05-29). Layer doctrine: [[the-method-layers]] (Engine
// layer) — Engines are PURE, DETERMINISTIC, in-workflow computation:
//
//   - NO I/O, NO time.Now(), NO math/rand, NO goroutines, NO global mutable state.
//   - NO outbound calls — no ResourceAccess (in particular NO workerAccess: the
//     Dispatch is the Manager's edge, UC3 line 541), no other Engine (no
//     reviewEngine, no interventionEngine), no Manager.
//   - Imports ONLY the framework-go Engine error model (fweng). It imports NO
//     Temporal — its determinism is what makes the constructionManager's direct
//     in-workflow PickWorkerClass call replay-safe (contract §6).
//
// Single operation PickWorkerClass (contract §2.1), named verbatim from the
// architecture.dsl edge label (lines 306/532). It returns ONLY the worker class;
// the dispatch and any human-review gating are the Manager's orchestration
// (contract §2.1 Notes, §4 Non-goals, FU-HE-D).
//
// The HandOffPolicy casting RULE (review-everything vs fully-automated vs mixed)
// is a package-internal compile-time Strategy (handOffStrategy below), swappable
// per customer without touching this surface (contract §6, FU-HE-B). It is NEVER
// leaked onto the contract (Variant C, rejected).
//
// ArchitectOnly is a NORMAL returned class (contract OQ-2), not an error: it tells
// the Manager to skip dispatch and await the Architect User. A FAILING input is a
// PROGRAMMER error (the Manager mis-assembled the call) — the error channel is
// reserved for programmer/contract misuse ONLY (every well-formed activity+policy
// yields a class). See contract §3 "Error model".
package handoff

import (
	"strings"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// Layer (on ConstructionActivity) is the Method layer (e.g. "manager", "engine",
// "resourceaccess", "client") the activity's component lives in — the
// SeniorOnlyLayers policy keys on it. Normalized case-insensitively at the policy
// boundary.

// HandOffEngineImpl and NewHandOffEngine are generated (contract.gen.go); the
// behaviour below is hand-written on that generated struct.

// PickWorkerClass implements HandOffEngine. It validates the input, selects the
// package-internal Strategy for the policy, runs it, and guards the result —
// returning ONLY the class (contract §2.1; the dispatch is the Manager's, §4).
func (HandOffEngineImpl) PickWorkerClass(_ fweng.Context, activity ConstructionActivity, policy HandOffPolicy) (WorkerClass, error) {
	// --- ContractMisuse pre-conditions (programmer error, not a domain result) ---
	if activity.ActivityID == "" {
		return WorkerClassUnknown, fweng.New(fweng.ContractMisuse,
			"PickWorkerClass: activity has empty ActivityID")
	}
	if activity.Kind == ActivityKindUnknown {
		return WorkerClassUnknown, fweng.New(fweng.ContractMisuse,
			"PickWorkerClass: activity "+quote(activity.ActivityID)+" has unknown ActivityKind")
	}

	// --- Strategy selection (package-internal; the casting RULE, never leaked). ---
	strategy := selectStrategy(policy)

	cast := strategy.pickWorkerClass(activity)

	// --- InvalidInput: the policy cast a class the build does not support. The
	// Engine must NOT silently fall back to a default class (silent mis-casting) —
	// mirrors settlementEngine refusing an unregistered settlement regime and
	// autoscalerEngine refusing unknown infrastructure. (Mapped to the shared
	// fixed InvalidInput kind; the contract names this "UnknownWorkerClass" — see
	// the C-HE log for the deviation flag, the shared engine.Kind has no such
	// member and FU-HE-C forbids redefining the error model.) ---
	if cast == WorkerClassUnknown {
		return WorkerClassUnknown, fweng.New(fweng.InvalidInput,
			"PickWorkerClass: policy cast an unsupported worker class for activity "+
				quote(activity.ActivityID))
	}

	// --- InternalInvariant guard: a Strategy bug if it returned a class outside
	// the registered set (contract §3 InternalInvariant). ---
	if !workerClassValid(cast) {
		return WorkerClassUnknown, fweng.New(fweng.InternalInvariant,
			"PickWorkerClass: strategy returned a class outside the registered set for activity "+
				quote(activity.ActivityID))
	}

	return cast, nil
}

// quote wraps s in double quotes for readable error detail (no fmt dependency
// needed for this single use, keeping the import set minimal — same idiom as
// estimationEngine).
func quote(s string) string { return "\"" + s + "\"" }

// behavior.go holds the hand-written behaviour over the generated contract enums
// (WorkerClass, ActivityKind). Per the schema-first contract rule, the generated
// contract types carry NO methods — behaviour the generator cannot produce lives
// here as FREE FUNCTIONS that take the enum value as a parameter. The enum consts
// (AIWorker, ActivityKindConstruction, …) are the generated contract surface
// (contract.gen.go); these functions reference them by name.

// workerClassString returns the canonical worker-class name (the logical class the
// Manager hands to workerAccess). Mirrors the constructionManager consumer mirror.
func workerClassString(c WorkerClass) string {
	switch c {
	case WorkerClassUnknown:
		// The zero value — never a valid casting result (handoff.go).
		return "unknown"
	case AIWorker:
		return "ai"
	case HumanSeniorWorker:
		return "humanSenior"
	case HumanJuniorWorker:
		return "humanJunior"
	case ArchitectOnly:
		return "architectOnly"
	}
	// Unreachable for the five defined WorkerClass values above (the exhaustive
	// linter enforces that every real variant has its own case); kept as a
	// defensive fallback for an out-of-range ordinal.
	return "unknown"
}

// workerClassValid reports whether c is a real casting result the build supports
// (i.e. a registered class, not the zero value). Used to guard the Strategy output.
func workerClassValid(c WorkerClass) bool {
	switch c {
	case AIWorker, HumanSeniorWorker, HumanJuniorWorker, ArchitectOnly:
		return true
	case WorkerClassUnknown:
		// The zero value — never a valid casting result (handoff.go).
		return false
	default:
		return false
	}
}

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
