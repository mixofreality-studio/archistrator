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
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// AutoscalerEngineImpl and NewAutoscalerEngine are generated (contract.gen.go);
// the behaviour below is hand-written on that generated struct.

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
func (AutoscalerEngineImpl) ProposeDesiredState(
	_ fweng.Context,
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
	if !ok || strat == nil {
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

// behavior.go holds the hand-written behaviour over the generated contract enums
// (DecisionKind, ReasonCode). Per the schema-first contract rule, the generated
// contract types carry NO methods — behaviour the generator cannot produce lives
// here as FREE FUNCTIONS that take the enum value as a parameter. The enum consts
// (DecisionNoChange, ReasonCPUHigh, …) are the generated contract surface
// (contract.gen.go); these functions reference them by name.

// The CustomerAppInfrastructure strategy axis (autoscalerEngine.md §6). This is
// PACKAGE-INTERNAL: there is no exported Strategy interface, no RegisterStrategy
// op, and no KnownInfrastructures op on the contract — infrastructureKind is an
// opaque discriminator, and adding an infrastructure is a new strategy file + a
// new InfrastructureKind constant + a new table entry, NOT a contract amendment.
//
// The launch set is Go + Temporal + Postgres + Git + S3
// (InfrastructureKindGoTemporalPostgres). Future kinds (pgvector, WASM compute,
// Kafka, …) register their own strategy behind the same surface.

// infrastructureStrategy is the unexported strategy port. Each infrastructure
// constituent implements propose for its scaling rules. It is PURE: no I/O, no
// clock, no RNG, no state — every time input arrives through Telemetry.
type infrastructureStrategy interface {
	propose(telemetry Telemetry, currentDesired DesiredState, policy AutoscalerPolicy) Decision
}

// strategies is the compile-time strategy table. It is NOT mutated at runtime
// (no RegisterStrategy); entries are added by editing this map (autoscalerEngine.md
// §6 "Strategy axis" — strategy registration is compile-time wiring).
// InfrastructureKindUnknown is present with an explicit nil so the exhaustive
// gate proves every enum member has a deliberate entry; nil means "no strategy
// registered" and takes the same InvalidInput path as an unlisted kind.
var strategies = map[InfrastructureKind]infrastructureStrategy{
	InfrastructureKindUnknown:            nil,
	InfrastructureKindGoTemporalPostgres: goTemporalPostgresStrategy{},
}

// goTemporalPostgresStrategy is the launch infrastructure's reactive
// CPU-+-idle-pause scaling strategy. Pure deterministic decision logic over one
// telemetry snapshot; anti-flap is achieved through thresholds + grace windows
// carried on the policy and on telemetry (no sliding window / no internal state —
// autoscalerEngine.md Non-goal #11).
type goTemporalPostgresStrategy struct{}

// propose implements the launch reactive policy. Decision precedence (highest
// first), all pure over the single snapshot:
//
//  1. Resume-from-zero: paused (currentDesired.Replicas == 0) AND traffic resumed.
//  2. Idle-pause: idle-pause enabled AND idle for ≥ IdleThreshold AND not already paused.
//  3. SLO protection: error budget burning/out AND room to grow ⇒ scale up.
//  4. CPU-high: CPU ≥ ScaleUpCPU AND room to grow ⇒ scale up.
//  5. CPU-sustained-low: CPU < ScaleDownCPU for ≥ ScaleDownGrace AND room to shrink ⇒ scale down.
//  6. Otherwise: NoChange (Steady), or the AlreadyAtMin/AlreadyAtMax reason when a
//     scale was warranted but clamped away.
func (goTemporalPostgresStrategy) propose(t Telemetry, cur DesiredState, p AutoscalerPolicy) Decision {
	// (1) Resume-from-zero. Distinct from ScaleUp: from-zero to baseline, an
	// infrastructure-driven scale-from-zero, not an increment over a non-zero base.
	if cur.Replicas == 0 {
		if trafficResumed(t) {
			return Decision{
				Kind:       DecisionResume,
				ToBaseline: resumeBaseline(p),
				Reason:     DecisionReason{Code: ReasonTrafficResumed, Detail: "traffic resumed while paused; resuming to baseline"},
			}
		}
		// Paused and still idle: nothing to do.
		return noChange(ReasonSteady, "paused and no traffic observed; staying paused")
	}

	// (2) Idle-pause: only when MinReplicas allows scaling to zero.
	if idlePauseWarranted(t, p) {
		return Decision{
			Kind:   DecisionPause,
			Reason: DecisionReason{Code: ReasonIdle, Detail: "no traffic for the idle threshold; pausing to zero"},
		}
	}

	// (3) SLO protection — burning/out-of-budget warrants headroom.
	if sloBudgetAtRisk(t) {
		if cur.Replicas < p.MaxReplicas {
			return scaleUp(cur, p, ReasonSLOBurnDown, "error budget burning; scaling up for headroom")
		}
		return noChange(ReasonAlreadyAtMax, "error budget burning but already at MaxReplicas")
	}

	// (4) CPU-high.
	if cpuHigh(t, p) {
		if cur.Replicas < p.MaxReplicas {
			return scaleUp(cur, p, ReasonCPUHigh, "CPU at/above the scale-up threshold")
		}
		return noChange(ReasonAlreadyAtMax, "CPU high but already at MaxReplicas")
	}

	// (5) CPU-sustained-low (anti-flap via the grace window the Manager has already
	// satisfied: TimeSinceLastRequest is traffic-idle time; for CPU-low we require
	// the low CPU to have persisted at least ScaleDownGrace since the last decision).
	if cpuSustainedLow(t, cur, p) {
		if cur.Replicas > p.MinReplicas {
			return scaleDown(cur, p, ReasonCPUSustainedLow, "CPU sustained below the scale-down threshold")
		}
		return noChange(ReasonAlreadyAtMin, "CPU low but already at MinReplicas")
	}

	// (6) Steady.
	return noChange(ReasonSteady, "all signals within thresholds")
}

// trafficResumed reports whether observable traffic has returned while paused.
func trafficResumed(t Telemetry) bool {
	return t.RequestsPerSecond > 0 || t.InflightRequests > 0
}

// idlePauseEnabled reports whether the policy permits idle-pause: MinReplicas
// must allow scaling to zero AND a positive IdleThreshold must be configured
// (IdleThreshold == 0 disables idle-pause per the policy contract).
func idlePauseEnabled(p AutoscalerPolicy) bool {
	return p.MinReplicas == 0 && p.IdleThreshold > 0
}

// idlePauseWarranted reports whether the idle-pause rule fires: the policy
// permits idle-pause AND the app has been traffic-idle for at least the
// threshold with no requests flowing right now.
func idlePauseWarranted(t Telemetry, p AutoscalerPolicy) bool {
	return idlePauseEnabled(p) && t.TimeSinceLastRequest >= p.IdleThreshold && t.RequestsPerSecond == 0
}

// sloBudgetAtRisk reports whether the SLO status warrants protective headroom
// (error budget burning or already exhausted).
func sloBudgetAtRisk(t Telemetry) bool {
	return t.SLOStatus == SLOBurningBudget || t.SLOStatus == SLOOutOfBudget
}

// cpuHigh reports whether CPU is at/above the scale-up threshold (a zero
// ScaleUpCPU disables the rule per the policy contract).
func cpuHigh(t Telemetry, p AutoscalerPolicy) bool {
	return p.ScaleUpCPU > 0 && t.CPUUtilization >= p.ScaleUpCPU
}

// cpuSustainedLow reports whether CPU is below the scale-down threshold AND has
// stayed there through the anti-flap grace window (a zero ScaleDownCPU disables
// the rule per the policy contract).
func cpuSustainedLow(t Telemetry, cur DesiredState, p AutoscalerPolicy) bool {
	return p.ScaleDownCPU > 0 && t.CPUUtilization < p.ScaleDownCPU && lowCPUSustained(t, cur, p)
}

// lowCPUSustained reports whether low CPU has persisted long enough to justify a
// scale-down (anti-flap). The grace window is measured against the last decision
// time the Manager pinned on the desired state and ObservedAt the Manager pinned
// on the telemetry — both inputs, no clock read. When ScaleDownGrace is zero the
// grace requirement is satisfied immediately.
func lowCPUSustained(t Telemetry, cur DesiredState, p AutoscalerPolicy) bool {
	if p.ScaleDownGrace <= 0 {
		return true
	}
	if cur.LastDecisionAt.IsZero() {
		// No prior decision recorded: treat the grace window as satisfied (the
		// app has been running steadily; nothing to debounce against).
		return true
	}
	return t.ObservedAt.Sub(cur.LastDecisionAt) >= p.ScaleDownGrace
}

// resumeBaseline computes the from-zero resume target. The strategy may modulate
// by SLA tier; the launch strategy bumps the baseline by one replica per tier
// step above Free, clamped into [MinReplicas, MaxReplicas]. ToBaseline is always
// ≥ 1 (resuming from zero must bring the app back up).
func resumeBaseline(p AutoscalerPolicy) int64 {
	base := max(p.BaselineReplicas+slaTierBump(p.SLATier), 1)
	return clampReplicas(base, p)
}

// slaTierBump is the launch strategy's SLA modulation: Free +0, Paid +1,
// Enterprise +2 over the policy baseline.
func slaTierBump(tier SLATier) int64 {
	switch tier {
	case SLATierFree:
		// The zero value — the free tier (autoscaler.go). No bump over baseline.
		return 0
	case SLATierPaid:
		return 1
	case SLATierEnterprise:
		return 2
	default:
		return 0
	}
}

// scaleUp builds a ScaleUp decision with a delta bounded by MaxStepDelta and
// MaxBurstCap, clamped so the resulting replica count stays ≤ MaxReplicas, and
// guaranteed ≥ 1.
func scaleUp(cur DesiredState, p AutoscalerPolicy, code ReasonCode, detail string) Decision {
	delta := boundStep(p)
	// Clamp so cur.Replicas + delta ≤ MaxReplicas.
	if room := p.MaxReplicas - cur.Replicas; delta > room {
		delta = room
	}
	if delta < 1 {
		delta = 1
	}
	return Decision{Kind: DecisionScaleUp, Delta: delta, Reason: DecisionReason{Code: code, Detail: detail}}
}

// scaleDown builds a ScaleDown decision with a delta bounded by MaxStepDelta,
// clamped so the resulting replica count stays ≥ MinReplicas, and guaranteed ≥ 1.
func scaleDown(cur DesiredState, p AutoscalerPolicy, code ReasonCode, detail string) Decision {
	delta := boundStepDown(p)
	// Clamp so cur.Replicas - delta ≥ MinReplicas.
	if room := cur.Replicas - p.MinReplicas; delta > room {
		delta = room
	}
	if delta < 1 {
		delta = 1
	}
	return Decision{Kind: DecisionScaleDown, Delta: delta, Reason: DecisionReason{Code: code, Detail: detail}}
}

// boundStep returns the scale-up step bounded by MaxStepDelta and MaxBurstCap.
// A non-positive MaxStepDelta means "no per-step bound" and defaults the step to 1.
func boundStep(p AutoscalerPolicy) int64 {
	step := p.MaxStepDelta
	if step <= 0 {
		step = 1
	}
	if p.MaxBurstCap > 0 && step > p.MaxBurstCap {
		step = p.MaxBurstCap
	}
	return step
}

// boundStepDown returns the scale-down step bounded by MaxStepDelta (MaxBurstCap
// caps bursts up, not down). A non-positive MaxStepDelta defaults the step to 1.
func boundStepDown(p AutoscalerPolicy) int64 {
	step := p.MaxStepDelta
	if step <= 0 {
		step = 1
	}
	return step
}

// clampReplicas clamps a replica count into [MinReplicas, MaxReplicas].
func clampReplicas(n int64, p AutoscalerPolicy) int64 {
	if n < p.MinReplicas {
		n = p.MinReplicas
	}
	if p.MaxReplicas > 0 && n > p.MaxReplicas {
		n = p.MaxReplicas
	}
	return n
}
