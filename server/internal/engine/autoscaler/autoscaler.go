// Package autoscaler is the autoscalerEngine — the Engine that encapsulates
// AutoscalerPolicy volatility: given what telemetry says is currently happening
// to one Operated System, what should the platform do to that system's desired
// capacity (NoChange | ScaleUp | ScaleDown | Pause | Resume).
//
// Contract: designs/aiarch/implementation/contracts/autoscalerEngine.md (FROZEN
// 2026-05-23, Amendment 1 applied). Layer rules: [[the-method-layers]] / Löwy
// ch. 5 — the Engine layer.
//
// PURE & DETERMINISTIC. This package does NO I/O, reads NO clock (no time.Now()),
// uses NO RNG (no math/rand), starts no goroutines, and makes NO outbound calls
// to any ResourceAccess, Manager, or other Engine. It IMPORTS NO TEMPORAL. It is
// a plain Go function the operationsManager invokes directly from its
// reconcileOperatedState workflow body (Path C). Determinism per
// (telemetry, currentDesired, policy, infrastructureKind) is what makes that
// direct in-workflow call replay-safe — no Activity wrapper, no RetryPolicy, no
// timeout (autoscalerEngine.md §1, §6).
//
// The op CONSUMES operationEstimationEngine forecasts only IMPLICITLY via the
// telemetry input (cost-relevant facts are already baked in upstream). It does
// NOT embed the cost model and does NOT call operationEstimationEngine — there is
// no Engine→Engine edge (autoscalerEngine.md §1, Non-goal #4).
//
// A "no decision possible" situation is NOT an error: it is Decision{Kind:
// NoChange}, a normal return value. The error channel is reserved for programmer
// / contract misuse (ContractMisuse), an unregistered infrastructure
// (InvalidInput — there is no strategy compiled in for it, so we never silently
// scale a infrastructure with the wrong rules), and broken internal invariants
// (InternalInvariant) only (autoscalerEngine.md §3, §6 "Error model").
//
// The CustomerAppInfrastructure strategy axis is package-internal: infrastructureKind
// selects a strategy from a compile-time table (no exported Strategy interface,
// no RegisterStrategy op, no KnownInfrastructures op — autoscalerEngine.md §2, §6
// "Strategy axis"). Adding an infrastructure is a new strategy file + a new
// InfrastructureKind constant, not a contract amendment.
//
// Imports ONLY framework-go/engine (the shared Engine error model, aliased fweng)
// plus the foundational time/uuid types. Per Option B full encapsulation the
// contract redefines InfrastructureKind as the autoscaler's OWN type (mirroring
// projectstate, the canonical ResourceAccess home), so this package imports NO
// projectstate — callers convert projectstate.InfrastructureKind ↔
// autoscaler.InfrastructureKind at the boundary (same int values).
package autoscaler

import (
	"github.com/google/uuid"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// InfrastructureKind is the opaque CustomerAppInfrastructure discriminator (the
// same kind the sibling operationEstimationEngine pivots on). Per Option B full
// encapsulation it is the autoscaler's OWN type, mirroring projectstate's values
// and names; callers convert projectstate.InfrastructureKind ↔
// autoscaler.InfrastructureKind at the boundary (same int values). Defining it
// once here keeps the Engine free of any projectstate import (autoscalerEngine.md §3).

// InfrastructureKindUnknown is the zero value (no strategy registered).

// InfrastructureKindGoTemporalPostgres is the launch infrastructure
// (Go + Temporal + Postgres + Git + S3).

// OperatedAppID identifies one Operated System. Carried for identity/labeling on
// Telemetry and DesiredState; the Engine never branches on it (autoscalerEngine.md §3).
type OperatedAppID = uuid.UUID

// --- Telemetry (autoscalerEngine.md §3) --------------------------------------

// HealthStatus is the observed application health signal.

// HealthUnknown is the zero value (health not yet observed).

// HealthHealthy — the app reports healthy.

// HealthDegraded — the app is serving but impaired.

// HealthUnhealthy — the app is failing health checks.

// SLOStatus is the observed error-budget signal.

// SLOUnknown is the zero value (SLO status not yet observed).

// SLOWithinBudget — comfortably inside the error budget.

// SLOBurningBudget — burning the error budget faster than allowed.

// SLOOutOfBudget — the error budget is exhausted.

// Telemetry is a point-in-time, infrastructure-neutral snapshot of the
// observations the autoscale decision needs. The Manager assembles it from the
// Path B reads (operatedRuntimeAccess.getApplicationHealth / getSloStatus /
// readComputeAttribution) and passes it in by value. The Engine reads it; it
// never compares ObservedAt to a wall clock (determinism — autoscalerEngine.md §3).

// Identity & timing.

// ObservedAt is pinned by the Manager from the Path B observation; the Engine
// reads it but does NOT compare it to time.Now() (the Engine reads no clock).

// Load signals (per CustomerAppInfrastructure strategy; not all signals are
// populated for every infrastructure).
// 0.0 when paused or no traffic since last tick
// 0.0 when paused

// Capacity signals.
// observed; may diverge from currentDesired during convergence
// 0.0..1.0 average across replicas; 0.0 when paused
// 0.0..1.0 average across replicas

// Health signals.

// Idle-pause signal: Manager-computed, infrastructure-agnostic.

// --- DesiredState (autoscalerEngine.md §3) -----------------------------------

// DesiredState is the Operated System's current logical desired capacity. The
// Manager owns its lifecycle; the Engine reads it and proposes a Decision against
// it. The Engine is stateless across calls — no per-call state is round-tripped
// through DesiredState (Amendment 1: BaselineRef removed; autoscalerEngine.md §3).

// InfrastructureKind must equal the infrastructureKind passed to
// ProposeDesiredState; the Engine asserts the invariant.

// 0 == paused
// when the last non-NoChange decision was applied; for debounce

// --- AutoscalerPolicy (autoscalerEngine.md §3) -------------------------------

// AutoscalerMode is the operator-level autoscaler switch.

// AutoscalerModeAuto enables the decision logic.

// AutoscalerModeManual ⇒ the Engine always returns NoChange (autoscaler off).

// SLATier is the SLA-class tier; it modulates baseline/min/anti-flap grace inside
// a strategy.

// SLATierFree is the zero value — the free tier.

// SLATierPaid — the paid tier.

// SLATierEnterprise — the enterprise tier.

// AutoscalerPolicy is the customer-tunable policy that drives the Engine. The
// Engine does NOT validate well-formedness beyond the policy.Kind ==
// infrastructureKind invariant (whoever stores the policy owns well-formedness;
// autoscalerEngine.md §3 "Validation").

// policy authored for this infrastructure
// Auto | Manual (manual ⇒ always NoChange)

// Capacity bounds.
// ≥ 0; 0 enables idle-pause
// ≥ MinReplicas
// Min ≤ Baseline ≤ Max; the resume-from-pause target
// upper bound on Δ replicas per decision; debounces flap

// Idle-pause.
// duration without traffic before Pause; 0 == disabled

// Load thresholds (per infrastructure strategy; some use a subset).
// 0.0..1.0; above ⇒ scale up
// 0.0..1.0; below for ScaleDownGrace ⇒ scale down
// must observe low CPU this long before scale down (anti-flap)

// SLA-class tier.
// Free | Paid | Enterprise

// Operator overrides.
// if true, Engine returns NoChange (operator pinned replicas)
// upper cap on a single ScaleUp delta even if the strategy proposes more

// --- Decision (autoscalerEngine.md §3) ---------------------------------------

// DecisionKind is the closed decision set per architecture.dsl line 229 /
// core-use-cases.md UC4 Path C.

// DecisionNoChange — the common quiet-tick outcome.

// DecisionScaleUp — increment replicas by Delta.

// DecisionScaleDown — decrement replicas by Delta.

// DecisionPause — idle-pause (the Manager publishes replicas=0).

// DecisionResume — resume from zero to ToBaseline.

// ReasonCode is the closed, build-time-fixed set of structured causes for the
// audit event the Manager appends (DesiredStatePublished(reason=autoscale,
// decision=...)). No dynamic registration (autoscalerEngine.md §3).

// ReasonUnknown is the zero value.

// ReasonCPUHigh — CPU above the scale-up threshold.

// ReasonCPUSustainedLow — CPU below the scale-down threshold for the grace window.

// ReasonIdle — no traffic for the idle threshold ⇒ pause.

// ReasonTrafficResumed — traffic resumed while paused ⇒ resume-to-baseline.

// ReasonManualMode — policy is manual; always NoChange.

// ReasonPinned — operator pinned replicas; always NoChange.

// ReasonSLOBurnDown — error budget burning/out ⇒ scale up to protect the SLO.

// ReasonAlreadyAtMin — would scale down but already at MinReplicas.

// ReasonAlreadyAtMax — would scale up but already at MaxReplicas.

// ReasonSteady — within thresholds; no change warranted.

// DecisionReason carries the structured cause the Manager writes into the audit
// event. Detail is a short human-readable explanation; safe to log; no PII.

// Decision is the sum-type output. Delta is populated on ScaleUp/ScaleDown;
// ToBaseline is populated on Resume (the replica count the policy considers the
// baseline for this app's SLA class — autoscalerEngine.md §3).

// populated when Kind == ScaleUp | ScaleDown (always > 0)
// populated when Kind == Resume
// structured cause for the audit event

// --- Public entry point (autoscalerEngine.md §2.1, §6) -----------------------

// AutoscalerEngine is the pure, deterministic scaling-decision port
// (autoscalerEngine.md §2). One behavioural operation, one caller
// (operationsManager), ZERO outbound edges. Defined here as the Engine's own
// surface; the operationsManager holds an independent consumer mirror it adapts to.

// ProposeDesiredState selects what the platform should do to one Operated
// System's desired capacity. Pure and deterministic (autoscalerEngine.md §2.1).

// engine is the concrete, stateless AutoscalerEngine. No fields => no mutable
// state => trivially deterministic and reentrant (autoscalerEngine.md §6).
type engine struct{}

// New returns the production AutoscalerEngine.
func New() AutoscalerEngine { return engine{} }

// Compile-time assertion that engine satisfies the port.
var _ AutoscalerEngine = engine{}

// ProposeDesiredState answers, deterministically, what the platform should do to
// one Operated System's desired capacity given the telemetry, the current desired
// state, the policy, and the infrastructure kind.
//
// It is a PLAIN deterministic Go function — it imports no Temporal and is called
// directly from the operationsManager workflow body (no Activity wrapper). Given
// the same four inputs it always returns the same Decision.
//
// Pre-conditions (violations are ContractMisuse — a Manager bug, NOT a
// "no-decision-possible" outcome): policy.Kind == infrastructureKind;
// currentDesired.InfrastructureKind == infrastructureKind. An infrastructureKind
// with no registered strategy is InvalidInput (the running build lacks a strategy
// — never a silent fall-through to a default).
//
// The two policy-universal short-circuits (ManualMode, Pinned) live here, not in
// each strategy: they are operator overrides, not infrastructure-specific
// (autoscalerEngine.md §6).
func (engine) ProposeDesiredState(
	telemetry Telemetry,
	currentDesired DesiredState,
	policy AutoscalerPolicy,
	infrastructureKind InfrastructureKind,
) (Decision, error) {
	if policy.Kind != infrastructureKind {
		return Decision{}, fweng.New(fweng.ContractMisuse,
			"proposeDesiredState: policy.Kind does not match infrastructureKind (Manager passed a mismatched policy)")
	}
	if currentDesired.InfrastructureKind != infrastructureKind {
		return Decision{}, fweng.New(fweng.ContractMisuse,
			"proposeDesiredState: currentDesired.InfrastructureKind does not match infrastructureKind (Manager passed a mismatched desired state)")
	}

	// Operator-level overrides are policy-universal short-circuits.
	if policy.Mode == AutoscalerModeManual {
		return noChange(ReasonManualMode, "autoscaler is in manual mode"), nil
	}
	if policy.Pinned {
		return noChange(ReasonPinned, "operator pinned replicas to the current desired state"), nil
	}

	strat, ok := strategies[infrastructureKind]
	if !ok {
		// No strategy compiled in for this infrastructure — never fall back to a
		// "default" strategy (that would silently scale with the wrong rules).
		return Decision{}, fweng.New(fweng.InvalidInput,
			"proposeDesiredState: unknown infrastructure (no scaling strategy registered for this infrastructureKind)")
	}

	decision := strat.propose(telemetry, currentDesired, policy)

	// Internal-invariant guards (Engine bugs, not domain outcomes).
	if err := assertDecisionInvariants(decision, policy); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

// noChange builds a NoChange decision with a structured reason.
func noChange(code ReasonCode, detail string) Decision {
	return Decision{Kind: DecisionNoChange, Reason: DecisionReason{Code: code, Detail: detail}}
}

// assertDecisionInvariants enforces the post-conditions the contract guarantees
// (autoscalerEngine.md §2.1 "Post-conditions"). A violation is an Engine bug
// (a strategy returned a malformed Decision) — InternalInvariant, never silent.
func assertDecisionInvariants(d Decision, policy AutoscalerPolicy) error {
	switch d.Kind {
	case DecisionNoChange, DecisionPause:
		// Delta/ToBaseline are not load-bearing here.
	case DecisionScaleUp, DecisionScaleDown:
		if d.Delta <= 0 {
			return fweng.New(fweng.InternalInvariant,
				"proposeDesiredState: scale decision has non-positive Delta")
		}
		if policy.MaxStepDelta > 0 && d.Delta > policy.MaxStepDelta {
			return fweng.New(fweng.InternalInvariant,
				"proposeDesiredState: scale Delta exceeds policy.MaxStepDelta")
		}
	case DecisionResume:
		if d.ToBaseline <= 0 {
			return fweng.New(fweng.InternalInvariant,
				"proposeDesiredState: resume decision has non-positive ToBaseline")
		}
	default:
		return fweng.New(fweng.InternalInvariant,
			"proposeDesiredState: strategy returned an unknown DecisionKind")
	}
	return nil
}
