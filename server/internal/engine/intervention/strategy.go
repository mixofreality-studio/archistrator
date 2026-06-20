package intervention

// strategy.go holds the PACKAGE-INTERNAL InterventionPolicy decision rule
// (interventionEngine.md §6, FU-IE-B). The interventionStrategy interface and its
// implementations are UNEXPORTED — never leaked onto the contract surface (Variant
// C, rejected). Severity tiers, SLA-class modulation, paging targets, self-healing
// rules, and shortfall tolerance live here; when InterventionPolicy evolves only
// this registry grows — the contract surface is unchanged.
//
// All strategies are PURE: no clock, no RNG, no I/O, no global mutable state. Each
// decision is a deterministic function of the trouble inputs (AttemptCount,
// ShortfallAge as sweep counts, Severity, SLATier) and the policy values. This is
// what makes the Managers' direct in-workflow calls replay-safe (FU-IE-A).

import (
	fweng "github.com/davidmarne/archistrator-platform/framework-go/engine"
)

// interventionStrategy is the unexported per-policy decision rule. Selected from
// the opaque InterventionPolicy.Mode at op entry (strategyFor). NEVER exported.
type interventionStrategy interface {
	decideOnVariance(v ConstructionVariance) VarianceDirective
	decideOnHealth(h HealthChange) HealthDirective
	decideOnSettlementFailure(f SettlementFailure) SettlementFailureDirective
	applyPausePolicy(ctx PauseRequestContext) PausePlan
}

// strategyFor selects the package-internal strategy from the policy mode. An
// unregistered mode is a deploy/config hazard — intervening under a silently
// defaulted policy on a broken build or a failed money path is the correctness
// hazard the no-silent-fallback rule guards against (mirrors settlementEngine's
// "unknown terms"). The shared engine.Kind enum is fixed at four kinds (no
// UnknownPolicyMode member to add without modifying framework-go, which is out of
// scope), so the hazard is reported as fweng.InvalidInput with the stable detail
// "unknown policy mode" — NEVER a silent default. See C-IE.md (flag for architect).
func strategyFor(policy InterventionPolicy) (interventionStrategy, error) {
	switch policy.Mode {
	case EscalateEverything:
		return escalateEverythingStrategy{}, nil
	case Tiered:
		return tieredStrategy{policy: policy}, nil
	default:
		return nil, fweng.New(fweng.InvalidInput, "unknown policy mode")
	}
}

// --- escalateEverythingStrategy (the launch default) ---------------------------
//
// volatilities.md line 45: "Early on, every variance escalates to a single
// operator." Every trouble escalates to a human; no retry budget, no self-heal,
// no takeover. The pause plan cancels everything in flight and notifies the
// operator.
type escalateEverythingStrategy struct{}

func (escalateEverythingStrategy) decideOnVariance(ConstructionVariance) VarianceDirective {
	return VarianceEscalate
}

func (escalateEverythingStrategy) decideOnHealth(HealthChange) HealthDirective {
	return HealthEscalate
}

func (escalateEverythingStrategy) decideOnSettlementFailure(SettlementFailure) SettlementFailureDirective {
	return SettlementEscalate
}

func (escalateEverythingStrategy) applyPausePolicy(ctx PauseRequestContext) PausePlan {
	return PausePlan{
		PipelinesToCancel: append([]PipelineRef(nil), ctx.InFlightPipelines...),
		RecordPaused:      true,
		NotifyTargets:     []NotifyTarget{NotifyOperator},
	}
}

// --- tieredStrategy (severity tiers + retry budgets + SLA modulation) ----------
//
// The maturing-platform regime (volatilities.md line 45 "Later, severity tiers,
// customer SLA classes, and self-healing rules emerge"). Decisions are a
// deterministic function of attempt/age budgets × severity × SLA class.
type tieredStrategy struct {
	policy InterventionPolicy
}

// effectiveRetryBudget modulates the policy's base retry budget by SLA class — a
// VARIABLE on the default (volatilities.md line 122): higher tiers get a larger
// budget before the decision flips to a human/takeover. Deterministic.
func (s tieredStrategy) effectiveRetryBudget() int {
	budget := s.policy.RetryBudget
	switch s.policy.SLATier {
	case SLATierEnterprise:
		budget += 2
	case SLATierPaid:
		budget++
	}
	if budget < 0 {
		budget = 0
	}
	return budget
}

func (s tieredStrategy) decideOnVariance(v ConstructionVariance) VarianceDirective {
	// Within the retry budget AND not high-severity: retry.
	if v.AttemptCount < s.effectiveRetryBudget() && v.Severity != SeverityHigh {
		return VarianceRetry
	}
	// Budget exhausted (or high-severity). A worker miss is recoverable by the
	// platform taking over (re-dispatch under a changed arrangement); an
	// unresolvable review verdict or an estimate over-run needs a human.
	if v.Kind == WorkerMiss {
		return VarianceTakeover
	}
	return VarianceEscalate
}

func (s tieredStrategy) decideOnHealth(h HealthChange) HealthDirective {
	// A non-transition (or a recovery toward healthy) is not actionable — let the
	// runtime self-heal / re-observe next tick.
	if h.FromHealth == h.ToHealth {
		return HealthRetry
	}
	// Out-of-budget or fully unhealthy: page the operator. High severity escalates.
	if h.ToHealth == HealthUnhealthy || h.SLOStatus == SLOOutOfBudget || h.Severity == SeverityHigh {
		return HealthEscalate
	}
	// Degraded but still within budget: transient — re-observe next tick.
	return HealthRetry
}

func (s tieredStrategy) decideOnSettlementFailure(f SettlementFailure) SettlementFailureDirective {
	// A dispute/chargeback is never auto-retried — it goes straight to a human.
	if f.Kind == Disputed || f.Kind == ChargedBack {
		return SettlementEscalate
	}
	// Declined charge: retry now while within the retry budget.
	if f.AttemptCount < s.effectiveRetryBudget() {
		return SettlementRetry
	}
	// Retry budget exhausted: back off and re-attempt on the next sweep, until the
	// BillingTerms tolerance window (in sweeps) is exhausted, then escalate.
	if f.ShortfallAge < s.policy.ShortfallToleranceSweeps {
		return SettlementDelay
	}
	return SettlementEscalate
}

func (s tieredStrategy) applyPausePolicy(ctx PauseRequestContext) PausePlan {
	// No in-flight work ⇒ an empty-but-valid no-op plan (still record the pause).
	if len(ctx.InFlightPipelines) == 0 {
		return PausePlan{RecordPaused: true}
	}
	// Enterprise tier notifies the architect as well as the operator.
	targets := []NotifyTarget{NotifyOperator}
	if s.policy.SLATier == SLATierEnterprise {
		targets = append(targets, NotifyArchitect)
	}
	return PausePlan{
		PipelinesToCancel: append([]PipelineRef(nil), ctx.InFlightPipelines...),
		RecordPaused:      true,
		NotifyTargets:     targets,
	}
}
