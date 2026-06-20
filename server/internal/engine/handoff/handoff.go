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
	fweng "github.com/davidmarne/archistrator-platform/framework-go/engine"
)

// WorkerClass is the worker arrangement cast onto an activity's worker role
// (contract §3). Engine-facing enum (the Worker-volatility set, OQ-3) — NOT a
// persisted head-state field; the Manager maps it onto a workerAccess.Dispatch
// spec. The member set + numeric ordering mirror the constructionManager's
// consumer mirror (internal/manager/construction/deps.go) so the Manager's
// adaptation is mechanical.
type WorkerClass int

const (
	// WorkerClassUnknown is the zero value — never a valid casting result.
	WorkerClassUnknown WorkerClass = iota
	// AIWorker — default; LLM/agent via the Manager's workerAccess.Dispatch.
	AIWorker
	// HumanSeniorWorker — human contractor, senior (sourced via the marketplace).
	HumanSeniorWorker
	// HumanJuniorWorker — human contractor, junior.
	HumanJuniorWorker
	// ArchitectOnly — customer-as-architect; no separate worker produced. A NORMAL
	// returned class (contract OQ-2): the Manager skips dispatch and awaits the
	// Architect User. NOT an error.
	ArchitectOnly
)

// String returns the canonical worker-class name (the logical class the Manager
// hands to workerAccess). Mirrors the constructionManager consumer mirror.
func (c WorkerClass) String() string {
	switch c {
	case AIWorker:
		return "ai"
	case HumanSeniorWorker:
		return "humanSenior"
	case HumanJuniorWorker:
		return "humanJunior"
	case ArchitectOnly:
		return "architectOnly"
	default:
		return "unknown"
	}
}

// valid reports whether c is a real casting result the build supports (i.e. a
// registered class, not the zero value). Used to guard the Strategy output.
func (c WorkerClass) valid() bool {
	switch c {
	case AIWorker, HumanSeniorWorker, HumanJuniorWorker, ArchitectOnly:
		return true
	default:
		return false
	}
}

// ActivityKind is the activity-type the policy keys on (contract §3). Mirrors the
// constructionManager consumer mirror's ActivityKind (deps.go).
type ActivityKind int

const (
	// ActivityKindUnknown is the zero value — a ContractMisuse input (the Manager
	// must build the activity with a real kind before calling).
	ActivityKindUnknown ActivityKind = iota
	// ActivityKindDetailedDesign — a component's contract-design activity.
	ActivityKindDetailedDesign
	// ActivityKindConstruction — a component's construction activity.
	ActivityKindConstruction
	// ActivityKindIntegration — an integration activity.
	ActivityKindIntegration
	// ActivityKindNoncoding — a non-coding activity.
	ActivityKindNoncoding
)

// String returns the canonical name for an activity kind.
func (k ActivityKind) String() string {
	switch k {
	case ActivityKindDetailedDesign:
		return "DetailedDesign"
	case ActivityKindConstruction:
		return "Construction"
	case ActivityKindIntegration:
		return "Integration"
	case ActivityKindNoncoding:
		return "Noncoding"
	default:
		return "Unknown"
	}
}

// ConstructionActivity is the by-value snapshot of the activity being dispatched
// (contract §3). The Manager reads the next eligible activity from
// projectStateAccess (UC3 line 531) and passes it in by value; this Engine reads
// it and owns none of it. Fields mirror the constructionManager consumer mirror
// (deps.go) so adaptation is mechanical.
//
// Layer is the Method layer (e.g. "manager", "engine", "resourceaccess",
// "client") the activity's component lives in — the SeniorOnlyLayers policy keys
// on it. It is normalized case-insensitively at the policy boundary.
type ConstructionActivity struct {
	ActivityID   string
	Kind         ActivityKind
	ComponentID  string
	Layer        string
	EstimateDays float64
}

// HandOffPolicy is this project's committed human-vs-AI casting policy
// (volatilities.md 83-84), passed BY VALUE (contract §3). It is the Strategy
// PARAMETER the package-internal casting rule reads — NOT the rule itself. Fields
// mirror the constructionManager consumer mirror (deps.go):
//
//   - PreferAI         — when true, the default class is AIWorker (fully-automated
//     leaning); when false, the default leans to a human senior (review-heavy).
//   - SeniorOnlyLayers — layers the customer requires a human-senior worker on,
//     regardless of PreferAI (e.g. "manager", "resourceaccess"). Matched
//     case-insensitively against ConstructionActivity.Layer.
//
// The committed customer-as-architect arrangement (glossary.md line 10) is the
// zero policy (PreferAI=false, no senior-only layers) ONLY insofar as a future
// policy mode names it; in v1 ArchitectOnly is cast by the dedicated
// architectOnly registration selected via a non-zero policy — see selectStrategy.
type HandOffPolicy struct {
	PreferAI         bool
	SeniorOnlyLayers []string
}

// HandOffEngine is the worker-casting facet over the HandOffPolicy volatility. One
// behavioural operation (contract §2 — 1-op count investigated & waived; matches
// the estimationEngine / autoscalerEngine precedent). Defined here as the Engine's
// own surface; the constructionManager holds an independent consumer mirror it
// adapts to (internal/manager/construction/deps.go).
type HandOffEngine interface {
	// PickWorkerClass selects the worker class the policy casts onto this
	// activity's worker role. Pure and deterministic: identical (activity, policy)
	// -> identical WorkerClass, always (contract §2.1, §6).
	//
	// The error is *fweng.Error and signals programmer/contract misuse ONLY
	// (the Engine does no I/O, so there is no transient failure to retry):
	//   - ContractMisuse: the activity carries no ActivityID, or an unknown
	//     ActivityKind — a constructionManager bug (it failed to build a valid
	//     input before the call). nil/empty inputs are NOT a "no-class-possible"
	//     outcome (contract §2.1 pre-conditions).
	//   - InvalidInput: the policy casts a worker class the running build does not
	//     support (the structural analogue of the contract's UnknownWorkerClass —
	//     see the package log note re: the fixed shared error model). The Engine
	//     does NOT silently fall back to a default class (silent mis-casting),
	//     exactly as settlementEngine refuses an unknown settlement regime.
	//   - InternalInvariant: the selected Strategy returned a class outside the
	//     registered set — an engine bug (a guard).
	PickWorkerClass(activity ConstructionActivity, policy HandOffPolicy) (WorkerClass, error)
}

// engine is the concrete, stateless HandOffEngine. No fields => no mutable state
// => trivially deterministic and reentrant (contract §6 invariant 3).
type engine struct{}

// New returns the production HandOffEngine.
func New() HandOffEngine { return engine{} }

// PickWorkerClass implements HandOffEngine. It validates the input, selects the
// package-internal Strategy for the policy, runs it, and guards the result —
// returning ONLY the class (contract §2.1; the dispatch is the Manager's, §4).
func (engine) PickWorkerClass(activity ConstructionActivity, policy HandOffPolicy) (WorkerClass, error) {
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
	if !cast.valid() {
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
