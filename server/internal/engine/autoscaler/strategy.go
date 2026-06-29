package autoscaler

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
var strategies = map[InfrastructureKind]infrastructureStrategy{
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
	if idlePauseEnabled(p) && t.TimeSinceLastRequest >= p.IdleThreshold && t.RequestsPerSecond == 0 {
		return Decision{
			Kind:   DecisionPause,
			Reason: DecisionReason{Code: ReasonIdle, Detail: "no traffic for the idle threshold; pausing to zero"},
		}
	}

	// (3) SLO protection — burning/out-of-budget warrants headroom.
	if t.SLOStatus == SLOBurningBudget || t.SLOStatus == SLOOutOfBudget {
		if cur.Replicas < p.MaxReplicas {
			return scaleUp(cur, p, ReasonSLOBurnDown, "error budget burning; scaling up for headroom")
		}
		return noChange(ReasonAlreadyAtMax, "error budget burning but already at MaxReplicas")
	}

	// (4) CPU-high.
	if p.ScaleUpCPU > 0 && t.CPUUtilization >= p.ScaleUpCPU {
		if cur.Replicas < p.MaxReplicas {
			return scaleUp(cur, p, ReasonCPUHigh, "CPU at/above the scale-up threshold")
		}
		return noChange(ReasonAlreadyAtMax, "CPU high but already at MaxReplicas")
	}

	// (5) CPU-sustained-low (anti-flap via the grace window the Manager has already
	// satisfied: TimeSinceLastRequest is traffic-idle time; for CPU-low we require
	// the low CPU to have persisted at least ScaleDownGrace since the last decision).
	if p.ScaleDownCPU > 0 && t.CPUUtilization < p.ScaleDownCPU && lowCPUSustained(t, cur, p) {
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
	base := p.BaselineReplicas + slaTierBump(p.SLATier)
	if base < 1 {
		base = 1
	}
	return clampReplicas(base, p)
}

// slaTierBump is the launch strategy's SLA modulation: Free +0, Paid +1,
// Enterprise +2 over the policy baseline.
func slaTierBump(tier SLATier) int64 {
	switch tier {
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
