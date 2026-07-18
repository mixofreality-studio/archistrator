// Package intervention is the interventionEngine — the Engine that encapsulates
// InterventionPolicy volatility: given that something has gone wrong on a build,
// an operated app, or a settlement (or that an operator has paused a project),
// what does the platform DO about it (retry / escalate / take over / delay /
// pause)?
//
// Contract: designs/aiarch/implementation/contracts/interventionEngine.md
// (APPROVED — FROZEN 2026-05-29). Layer rules: [[the-method-layers]] / Löwy ch. 5
// — the Engine layer. It is the exception-path decision twin of settlementEngine
// (the happy-path settlement computer) and a sibling of autoscalerEngine.
//
// PURE & DETERMINISTIC. This package does NO I/O, reads NO clock (no time.Now()),
// uses NO RNG (no math/rand), starts no goroutines, reads no env at init, and
// makes NO outbound call to any ResourceAccess, Manager, or other Engine (the
// architecture.dsl outbound grep is empty — §5 of the contract). It STATES a
// remediation directive as a VALUE; it never acts (no page sent, no pipeline
// cancelled, no charge retried, no money delayed from inside the Engine). The
// three calling Managers (constructionManager, operationsManager,
// settlementManager) read the trouble context + the committed InterventionPolicy
// from their own RA edges, pass value snapshots in, and execute the returned
// directive themselves. That is what makes the Managers' direct in-workflow calls
// replay-safe — no Temporal Activity wrapper is needed (no Temporal import here).
//
// A NO-OP DIRECTIVE IS A DOMAIN RESULT, not an error: a non-transition health
// change yields HealthRetry; an already-paused project yields an empty-but-valid
// PausePlan. The error channel (fweng.Error) is reserved for programmer/contract
// misuse (ContractMisuse), an unregistered policy mode (InvalidInput "unknown
// policy mode" — the structural analogue of settlementEngine's "unknown terms";
// the shared engine.Kind enum is fixed at four kinds, so the unknown-strategy
// hazard is reported as InvalidInput with a stable detail, NEVER a silent default
// — see implementation log C-IE.md), and broken internal invariants
// (InternalInvariant, e.g. a strategy returning a directive outside an op's
// closed set).
//
// InterventionPolicy is an axis-1 Strategy parameter (volatilities.md 44-45, 122;
// operational-concepts.md line 174). Severity tiers, SLA-class modulation,
// paging targets, self-healing rules, and shortfall tolerance are PACKAGE-INTERNAL
// Strategy keyed off the opaque policy value (strategy.go) — never a contract
// amendment, never a leaked Strategy interface (Variant C, rejected).
//
// Imports ONLY framework-go/engine (the shared Engine error model, aliased fweng).
// The input/output value types are defined package-local here: their eventual
// canonical homes (projectStateAccess / operatedSystemStateAccess /
// settlementStateAccess) are sibling components not yet constructed, so per the
// frozen contract's OQ-6 ("field-level shape is construction-refinable") these are
// kept as self-contained value types and re-homed when those RAs land — flagged in
// the C-IE log. No outbound dependency is introduced (Engine purity preserved).
package intervention

import (
	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// NOTE: the iota ORDER of the directive enums below (VarianceDirective,
// HealthDirective, SettlementFailureDirective) is Engine-internal and NOT a
// wire/persistence contract — the directive IDENTITIES, not their numeric
// values, are load-bearing (senior freeze-time note, contract §3). Nothing in
// this package or its callers may depend on the numeric values.

// InterventionEngineImpl and NewInterventionEngine are generated
// (contract.gen.go); the behaviour below is hand-written on that generated
// struct.

// DecideOnVariance maps a construction variance to a remediation directive.
func (InterventionEngineImpl) DecideOnVariance(_ fweng.Context, variance ConstructionVariance) (VarianceDirective, error) {
	if variance.ProjectID == "" || variance.ActivityID == "" {
		return 0, fweng.New(fweng.ContractMisuse,
			"DecideOnVariance: empty ProjectID/ActivityID (Manager failed to assemble a valid ConstructionVariance)")
	}
	if variance.Kind == VarianceKindUnknown {
		return 0, fweng.New(fweng.ContractMisuse,
			"DecideOnVariance: variance Kind is unset (Manager failed to assemble a valid ConstructionVariance)")
	}
	if variance.AttemptCount < 0 {
		return 0, fweng.New(fweng.ContractMisuse,
			"DecideOnVariance: AttemptCount is negative")
	}
	s, err := strategyFor(variance.Policy)
	if err != nil {
		return 0, err
	}
	d := s.decideOnVariance(variance)
	if !varianceDirectiveValid(d) {
		return 0, fweng.New(fweng.InternalInvariant,
			"DecideOnVariance: strategy returned a directive outside the closed set {Retry|Escalate|Takeover}")
	}
	return d, nil
}

// DecideOnHealth maps an operated-app health transition to a remediation directive.
func (InterventionEngineImpl) DecideOnHealth(_ fweng.Context, healthChange HealthChange) (HealthDirective, error) {
	if healthChange.OperatedAppID == "" {
		return 0, fweng.New(fweng.ContractMisuse,
			"DecideOnHealth: empty OperatedAppID (Manager failed to assemble a valid HealthChange)")
	}
	s, err := strategyFor(healthChange.Policy)
	if err != nil {
		return 0, err
	}
	d := s.decideOnHealth(healthChange)
	if !healthDirectiveValid(d) {
		return 0, fweng.New(fweng.InternalInvariant,
			"DecideOnHealth: strategy returned a directive outside the closed set {Retry|Escalate}")
	}
	return d, nil
}

// DecideOnSettlementFailure maps a failed settlement action to a remediation
// directive (retry now / back off to the next sweep / escalate to delinquency).
func (InterventionEngineImpl) DecideOnSettlementFailure(_ fweng.Context, failure SettlementFailure) (SettlementFailureDirective, error) {
	if failure.CustomerID == "" || failure.CycleID == "" {
		return 0, fweng.New(fweng.ContractMisuse,
			"DecideOnSettlementFailure: empty CustomerID/CycleID (Manager failed to assemble a valid SettlementFailure)")
	}
	if failure.Kind == SettlementFailureKindUnknown {
		return 0, fweng.New(fweng.ContractMisuse,
			"DecideOnSettlementFailure: failure Kind is unset (Manager failed to assemble a valid SettlementFailure)")
	}
	if failure.AttemptCount < 0 || failure.ShortfallAge < 0 {
		return 0, fweng.New(fweng.ContractMisuse,
			"DecideOnSettlementFailure: AttemptCount/ShortfallAge is negative")
	}
	s, err := strategyFor(failure.Policy)
	if err != nil {
		return 0, err
	}
	d := s.decideOnSettlementFailure(failure)
	if !settlementDirectiveValid(d) {
		return 0, fweng.New(fweng.InternalInvariant,
			"DecideOnSettlementFailure: strategy returned a directive outside the closed set {Retry|Escalate|Delay}")
	}
	return d, nil
}

// ApplyPausePolicy computes the pause plan the policy prescribes for an operator
// pause request. The Manager executes the plan; the Engine returns the plan.
func (InterventionEngineImpl) ApplyPausePolicy(_ fweng.Context, ctx PauseRequestContext) (PausePlan, error) {
	if ctx.ProjectID == "" {
		return PausePlan{}, fweng.New(fweng.ContractMisuse,
			"ApplyPausePolicy: empty ProjectID (Manager failed to assemble a valid PauseRequestContext)")
	}
	s, err := strategyFor(ctx.Policy)
	if err != nil {
		return PausePlan{}, err
	}
	return s.applyPausePolicy(ctx), nil
}

// --- Closed-set validators (InternalInvariant guards) --------------------------

func varianceDirectiveValid(d VarianceDirective) bool {
	switch d {
	case VarianceRetry, VarianceEscalate, VarianceTakeover:
		return true
	default:
		return false
	}
}

func healthDirectiveValid(d HealthDirective) bool {
	switch d {
	case HealthRetry, HealthEscalate:
		return true
	default:
		return false
	}
}

func settlementDirectiveValid(d SettlementFailureDirective) bool {
	switch d {
	case SettlementRetry, SettlementDelay, SettlementEscalate:
		return true
	default:
		return false
	}
}

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
	case InterventionModeUnknown:
		// No mode set. Settling/intervening under an unregistered regime is
		// forbidden — never a silent default (intervention.go).
		return nil, fweng.New(fweng.InvalidInput, "unknown policy mode")
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
func (s tieredStrategy) effectiveRetryBudget() int64 {
	budget := s.policy.RetryBudget
	switch s.policy.SLATier {
	case SLATierEnterprise:
		budget += 2
	case SLATierPaid:
		budget++
	case SLATierFree:
		// The zero-value tier: base budget, no bump.
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
