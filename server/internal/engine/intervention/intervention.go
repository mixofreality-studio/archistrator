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

// --- Identifier value types ----------------------------------------------------
//
// String newtypes for the entity identifiers the inputs carry. Self-contained
// (the owning RAs are not yet built); the Engine only reads these by value and
// compares/keys on them — it never resolves them against any store.

// ProjectID identifies a construction project.

// ActivityID identifies a construction activity within a project.

// OperatedAppID identifies an operated application.

// CustomerID identifies a billing customer.

// CycleID identifies a settlement cycle.

// PipelineRef references an in-flight construction pipeline (the value the
// Manager later passes to constructionPipelineAccess.cancelConstructionPipeline).

// NotifyTarget names a party the policy says to notify. The Engine names the
// target; the Manager sends the actual notification (no I/O here).

// NotifyOperator — page/notify the operator.

// NotifyArchitect — page/notify the architect.

// --- InterventionPolicy (the Strategy parameter) -------------------------------

// InterventionMode is the policy's coarse intervention regime — the discriminator
// the package-internal Strategy registry keys on (strategy.go). It is an opaque
// policy value to callers; the casting RULE behind each mode is package-internal.

// InterventionModeUnknown — no mode set. Settling/intervening under an
// unregistered regime is forbidden; ops return fweng.InvalidInput "unknown
// policy mode" rather than a silent default.

// EscalateEverything — the launch default: every trouble escalates to a
// single operator (volatilities.md line 45 "Early on, every variance
// escalates to a single operator").

// Tiered — severity tiers + retry budgets + SLA-class modulation decide
// retry vs escalate vs takeover/delay before flipping to a human.

// SLATier is the customer/app SLA class. It is a VARIABLE on top of the platform's
// default policy (volatilities.md line 122 — per-customer overrides are variables,
// not separate policies), read by the Strategy to modulate budgets/targets.

// SLATierFree — best-effort.

// SLATierPaid — standard paid tier.

// SLATierEnterprise — premium tier (tighter escalation, larger budgets).

// InterventionPolicy is the project/app/customer's committed intervention policy
// (volatilities.md 44-45). The policy VALUE the Strategy reads arrives here by
// value; the casting RULE (severity tiers, paging targets, self-healing, budgets,
// shortfall tolerance) is package-internal (strategy.go). For settlement,
// BillingTerms tolerance rides INSIDE this policy (contract OQ-3 ratified:
// ShortfallToleranceSweeps), not as a sibling parameter on SettlementFailure.

// Mode is the coarse regime the Strategy registry keys on. Unknown ⇒
// InvalidInput "unknown policy mode".

// SLATier modulates budgets/targets (a variable on the default).

// RetryBudget is the max retries before the decision flips to
// Escalate/Takeover/Delay. A retry-budget-exhausted trouble deterministically
// yields a non-Retry directive — never a silent retry loop.

// ShortfallToleranceSweeps is the number of shortfallSweeps a declined charge
// is delayed (backed off) before the directive flips to Escalate. Carries the
// BillingTerms tolerance into the policy (OQ-3). Counted in sweeps, NOT
// wall-clock — the Engine reads no clock.

// --- Inputs --------------------------------------------------------------------

// VarianceKind classifies the construction trouble decideOnVariance keys on.

// VarianceKindUnknown — unset (a ContractMisuse on input).

// ReviewFailedUnresolvable — a review verdict that cannot be resolved.

// WorkerMiss — a dispatched worker missed/failed to produce.

// EstimateOverrun — the activity over-ran its estimate.

// Severity is a coarse, policy-keyed severity grade carried on the trouble inputs
// (the Manager derives it; the Engine reads no external state to compute it).

// SeverityLow — minor; favours retry/self-heal.

// SeverityHigh — serious; favours escalate/takeover.

// ConstructionVariance is the input to DecideOnVariance. The Manager detects the
// variance (failed/unresolvable review verdict, worker miss, estimate over-run)
// from projectStateAccess / the replan sweep and passes it in by value, carrying
// the committed InterventionPolicy. The Engine reads it; it owns none of it and
// fetches none of it.

// attempts so far on the affected activity (drives budget exhaustion)

// HealthStatus is an observed operated-app health grade.

// HealthUnknown — health not yet known.

// HealthHealthy — nominal.

// HealthDegraded — degraded but serving.

// HealthUnhealthy — failing.

// SLOStatus is the observed SLO error-budget posture.

// SLOUnknown — budget posture not yet known.

// SLOWithinBudget — comfortably within the error budget.

// SLOBurningBudget — burning the error budget faster than nominal.

// SLOOutOfBudget — error budget exhausted.

// HealthChange is the input to DecideOnHealth. The Manager reads the health/SLO
// transition via operatedRuntimeAccess and passes it in by value with the policy.

// SettlementFailureKind classifies the failed settlement action.

// SettlementFailureKindUnknown — unset (a ContractMisuse on input).

// ChargeDeclined — a declined charge on a shortfall.

// Disputed — a disputed cycle.

// ChargedBack — a charged-back cycle.

// SettlementFailure is the input to DecideOnSettlementFailure. The Manager passes
// the failed-action context by value with the policy (which carries the
// BillingTerms tolerance via Policy.ShortfallToleranceSweeps — OQ-3).

// ShortfallAge is the number of shortfallSweeps elapsed since the first
// shortfall (drives the Delay→Escalate flip). Counted in SWEEPS, NOT a clock
// read — the Engine reads no wall-clock.

// PauseRequestContext is the input to ApplyPausePolicy (it carries ProjectID, so
// the op takes one value per the contract's intent — "ApplyPausePolicy(projectId)
// → PausePlan" — with the in-flight snapshot + policy the Manager reads first).

// in-flight pipeline(s) the plan may cancel (read by the Manager)
// operator's stated reason (safe to log)

// --- Outputs (per-op closed directive enums — contract OQ-2 ratified) ----------
//
// NOTE: the iota ORDER below is Engine-internal and NOT a wire/persistence
// contract — the directive IDENTITIES, not their numeric values, are load-bearing
// (senior freeze-time note, contract §3). Nothing in this package or its callers
// may depend on the numeric values.

// VarianceDirective is the output of DecideOnVariance — closed set per
// architecture.dsl line 307/551: {Retry | Escalate | Takeover}.

// VarianceRetry — re-attempt the activity (within the retry budget).

// VarianceEscalate — hand to the architect/operator (page a human).

// VarianceTakeover — the platform takes over (re-dispatch / reset). The
// DECISION, not the act; the Manager performs the takeover via its RA edges.

// HealthDirective is the output of DecideOnHealth — closed set per
// architecture.dsl line 309/589: {Retry | Escalate}. No Takeover (OQ-5).

// HealthRetry — no human action; let the runtime self-heal / re-observe next tick.

// HealthEscalate — page the operator.

// SettlementFailureDirective is the output of DecideOnSettlementFailure — closed
// set per architecture.dsl line 313: {Retry | Escalate | Delay}.

// SettlementRetry — re-attempt the charge now (within budget).

// SettlementDelay — back off; re-attempt on the next shortfallSweep (grace).

// SettlementEscalate — tolerance exhausted; flag delinquency / page.

// PausePlan is the output of ApplyPausePolicy: a value describing the pause
// actions the policy prescribes. The Manager EXECUTES the plan (cancel pipelines,
// record paused, notify). Richer than a one-of directive because the pause is a
// multi-action plan (NCUC2). An already-paused / no-in-flight project yields an
// empty-but-valid plan (a no-op), not an error.

// → constructionPipelineAccess.cancelConstructionPipeline
// → projectStateAccess.recordOperatorPaused
// who the policy says to notify
// grace/resume guidance (no clock read here)

// --- Port + implementation -----------------------------------------------------

// InterventionEngine is the pure, deterministic intervention-decision port
// (interventionEngine.md §2). Four ops, three callers, ZERO outbound edges.

// DecideOnVariance — UC3 construction variance / takeover decision.
// Called by constructionManager. (interventionEngine.md §2.1)

// DecideOnHealth — UC4 operations health intervention decision.
// Called by operationsManager. (interventionEngine.md §2.2)

// DecideOnSettlementFailure — UC6 / ncuc4 settlement-failure decision.
// Called by settlementManager. (interventionEngine.md §2.3)

// ApplyPausePolicy — NCUC2 operator-pause plan.
// Called by constructionManager. (interventionEngine.md §2.4)

// The stateless implementation of InterventionEngine — InterventionEngineImpl —
// and its constructor NewInterventionEngine() are GENERATED into contract.gen.go.
// It holds no fields, does no I/O, reads no clock/RNG, and starts no goroutines —
// safe to call directly from workflow code. The behaviour below is hand-written on
// the generated struct.

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
