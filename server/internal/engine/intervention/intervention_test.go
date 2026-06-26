package intervention

import (
	"errors"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// Service Test Plan (STP) — interventionEngine (C-IE). Black-box-style table tests
// over the four ops: the list of ways to demonstrate the Engine does NOT work.
//
//  1. DecideOnVariance: launch escalate-everything → Escalate always; tiered →
//     Retry within budget, Takeover on budget-exhausted worker miss, Escalate on
//     budget-exhausted unresolvable/overrun; high-severity short-circuits retry;
//     ContractMisuse on empty ids / unset kind / negative attempt.
//  2. DecideOnHealth: launch → Escalate always; tiered → Retry on non-transition,
//     Retry on degraded-within-budget, Escalate on unhealthy / out-of-budget /
//     high-severity; no Takeover outcome (OQ-5); ContractMisuse on empty app id.
//  3. DecideOnSettlementFailure: launch → Escalate always; tiered → Retry within
//     budget, Delay within tolerance, Escalate past tolerance, Escalate on
//     dispute/chargeback; ContractMisuse on empty ids / unset kind / negatives.
//  4. ApplyPausePolicy: launch cancels all + notifies operator + records; tiered
//     no-in-flight → empty-but-valid no-op (records, cancels nothing); enterprise
//     notifies architect too; ContractMisuse on empty ProjectID.
//  5. Unknown policy mode → InvalidInput "unknown policy mode" on every op (no
//     silent default).
//  6. Determinism: identical inputs → identical outputs across repeated calls.

func launchPolicy() InterventionPolicy {
	return InterventionPolicy{Mode: EscalateEverything}
}

func tieredPolicy(budget, tolerance int64, tier SLATier) InterventionPolicy {
	return InterventionPolicy{
		Mode:                     Tiered,
		SLATier:                  tier,
		RetryBudget:              budget,
		ShortfallToleranceSweeps: tolerance,
	}
}

// --- DecideOnVariance ----------------------------------------------------------

func TestDecideOnVariance(t *testing.T) {
	e := New()

	tests := []struct {
		name     string
		variance ConstructionVariance
		want     VarianceDirective
		wantErr  bool
		errKind  fweng.Kind
	}{
		{
			name:     "launch escalate-everything always escalates",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: WorkerMiss, AttemptCount: 0, Severity: SeverityLow, Policy: launchPolicy()},
			want:     VarianceEscalate,
		},
		{
			name:     "tiered retries within budget at low severity",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: EstimateOverrun, AttemptCount: 1, Severity: SeverityLow, Policy: tieredPolicy(3, 0, SLATierFree)},
			want:     VarianceRetry,
		},
		{
			name:     "tiered high severity short-circuits retry to takeover for worker miss",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: WorkerMiss, AttemptCount: 0, Severity: SeverityHigh, Policy: tieredPolicy(3, 0, SLATierFree)},
			want:     VarianceTakeover,
		},
		{
			name:     "tiered budget-exhausted worker miss takes over",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: WorkerMiss, AttemptCount: 3, Severity: SeverityLow, Policy: tieredPolicy(3, 0, SLATierFree)},
			want:     VarianceTakeover,
		},
		{
			name:     "tiered budget-exhausted unresolvable review escalates",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: ReviewFailedUnresolvable, AttemptCount: 3, Severity: SeverityLow, Policy: tieredPolicy(3, 0, SLATierFree)},
			want:     VarianceEscalate,
		},
		{
			name:     "enterprise tier widens the retry budget",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: EstimateOverrun, AttemptCount: 3, Severity: SeverityLow, Policy: tieredPolicy(2, 0, SLATierEnterprise)},
			want:     VarianceRetry, // base 2 + 2 (enterprise) = 4 > attempt 3
		},
		{
			name:     "empty project id is contract misuse",
			variance: ConstructionVariance{ActivityID: "a1", Kind: WorkerMiss, Policy: launchPolicy()},
			wantErr:  true, errKind: fweng.ContractMisuse,
		},
		{
			name:     "unset variance kind is contract misuse",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: VarianceKindUnknown, Policy: launchPolicy()},
			wantErr:  true, errKind: fweng.ContractMisuse,
		},
		{
			name:     "negative attempt count is contract misuse",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: WorkerMiss, AttemptCount: -1, Policy: launchPolicy()},
			wantErr:  true, errKind: fweng.ContractMisuse,
		},
		{
			name:     "unknown policy mode is invalid input",
			variance: ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: WorkerMiss, Policy: InterventionPolicy{Mode: InterventionModeUnknown}},
			wantErr:  true, errKind: fweng.InvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.DecideOnVariance(fweng.Context{}, tt.variance)
			if tt.wantErr {
				assertEngineErr(t, err, tt.errKind)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("directive = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- DecideOnHealth ------------------------------------------------------------

func TestDecideOnHealth(t *testing.T) {
	e := New()

	tests := []struct {
		name    string
		change  HealthChange
		want    HealthDirective
		wantErr bool
		errKind fweng.Kind
	}{
		{
			name:   "launch escalate-everything always escalates",
			change: HealthChange{OperatedAppID: "app1", FromHealth: HealthHealthy, ToHealth: HealthDegraded, SLOStatus: SLOWithinBudget, Policy: launchPolicy()},
			want:   HealthEscalate,
		},
		{
			name:   "tiered non-transition is a no-op retry",
			change: HealthChange{OperatedAppID: "app1", FromHealth: HealthDegraded, ToHealth: HealthDegraded, SLOStatus: SLOBurningBudget, Policy: tieredPolicy(0, 0, SLATierPaid)},
			want:   HealthRetry,
		},
		{
			name:   "tiered degraded within budget re-observes",
			change: HealthChange{OperatedAppID: "app1", FromHealth: HealthHealthy, ToHealth: HealthDegraded, SLOStatus: SLOWithinBudget, Severity: SeverityLow, Policy: tieredPolicy(0, 0, SLATierPaid)},
			want:   HealthRetry,
		},
		{
			name:   "tiered unhealthy escalates",
			change: HealthChange{OperatedAppID: "app1", FromHealth: HealthDegraded, ToHealth: HealthUnhealthy, SLOStatus: SLOWithinBudget, Policy: tieredPolicy(0, 0, SLATierPaid)},
			want:   HealthEscalate,
		},
		{
			name:   "tiered out-of-budget escalates",
			change: HealthChange{OperatedAppID: "app1", FromHealth: HealthHealthy, ToHealth: HealthDegraded, SLOStatus: SLOOutOfBudget, Policy: tieredPolicy(0, 0, SLATierPaid)},
			want:   HealthEscalate,
		},
		{
			name:    "empty operated app id is contract misuse",
			change:  HealthChange{FromHealth: HealthHealthy, ToHealth: HealthUnhealthy, Policy: launchPolicy()},
			wantErr: true, errKind: fweng.ContractMisuse,
		},
		{
			name:    "unknown policy mode is invalid input",
			change:  HealthChange{OperatedAppID: "app1", ToHealth: HealthUnhealthy, Policy: InterventionPolicy{Mode: InterventionModeUnknown}},
			wantErr: true, errKind: fweng.InvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.DecideOnHealth(fweng.Context{}, tt.change)
			if tt.wantErr {
				assertEngineErr(t, err, tt.errKind)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("directive = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- DecideOnSettlementFailure -------------------------------------------------

func TestDecideOnSettlementFailure(t *testing.T) {
	e := New()

	tests := []struct {
		name    string
		failure SettlementFailure
		want    SettlementFailureDirective
		wantErr bool
		errKind fweng.Kind
	}{
		{
			name:    "launch escalate-everything always escalates",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: ChargeDeclined, AttemptCount: 0, ShortfallAge: 0, Policy: launchPolicy()},
			want:    SettlementEscalate,
		},
		{
			name:    "tiered declined charge retries within budget",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: ChargeDeclined, AttemptCount: 1, ShortfallAge: 0, Policy: tieredPolicy(3, 5, SLATierFree)},
			want:    SettlementRetry,
		},
		{
			name:    "tiered budget-exhausted within tolerance delays",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: ChargeDeclined, AttemptCount: 3, ShortfallAge: 2, Policy: tieredPolicy(3, 5, SLATierFree)},
			want:    SettlementDelay,
		},
		{
			name:    "tiered past tolerance escalates",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: ChargeDeclined, AttemptCount: 3, ShortfallAge: 5, Policy: tieredPolicy(3, 5, SLATierFree)},
			want:    SettlementEscalate,
		},
		{
			name:    "tiered dispute escalates immediately",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: Disputed, AttemptCount: 0, ShortfallAge: 0, Policy: tieredPolicy(3, 5, SLATierFree)},
			want:    SettlementEscalate,
		},
		{
			name:    "tiered chargeback escalates immediately",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: ChargedBack, AttemptCount: 0, ShortfallAge: 0, Policy: tieredPolicy(3, 5, SLATierFree)},
			want:    SettlementEscalate,
		},
		{
			name:    "empty customer id is contract misuse",
			failure: SettlementFailure{CycleID: "cyc1", Kind: ChargeDeclined, Policy: launchPolicy()},
			wantErr: true, errKind: fweng.ContractMisuse,
		},
		{
			name:    "unset failure kind is contract misuse",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: SettlementFailureKindUnknown, Policy: launchPolicy()},
			wantErr: true, errKind: fweng.ContractMisuse,
		},
		{
			name:    "negative shortfall age is contract misuse",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: ChargeDeclined, ShortfallAge: -1, Policy: launchPolicy()},
			wantErr: true, errKind: fweng.ContractMisuse,
		},
		{
			name:    "unknown policy mode is invalid input",
			failure: SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: ChargeDeclined, Policy: InterventionPolicy{Mode: InterventionModeUnknown}},
			wantErr: true, errKind: fweng.InvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.DecideOnSettlementFailure(fweng.Context{}, tt.failure)
			if tt.wantErr {
				assertEngineErr(t, err, tt.errKind)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("directive = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- ApplyPausePolicy ----------------------------------------------------------

func TestApplyPausePolicy(t *testing.T) {
	e := New()

	t.Run("launch cancels all in-flight, records, notifies operator", func(t *testing.T) {
		plan, err := e.ApplyPausePolicy(fweng.Context{}, PauseRequestContext{
			ProjectID:         "p1",
			InFlightPipelines: []PipelineRef{"pipe-a", "pipe-b"},
			Reason:            "operator requested",
			Policy:            launchPolicy(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan.PipelinesToCancel) != 2 {
			t.Fatalf("pipelines to cancel = %v, want 2", plan.PipelinesToCancel)
		}
		if !plan.RecordPaused {
			t.Fatalf("RecordPaused = false, want true")
		}
		if len(plan.NotifyTargets) != 1 || plan.NotifyTargets[0] != NotifyOperator {
			t.Fatalf("notify targets = %v, want [operator]", plan.NotifyTargets)
		}
	})

	t.Run("tiered no in-flight yields empty-but-valid no-op plan", func(t *testing.T) {
		plan, err := e.ApplyPausePolicy(fweng.Context{}, PauseRequestContext{
			ProjectID: "p1",
			Policy:    tieredPolicy(0, 0, SLATierPaid),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan.PipelinesToCancel) != 0 {
			t.Fatalf("pipelines to cancel = %v, want none", plan.PipelinesToCancel)
		}
		if !plan.RecordPaused {
			t.Fatalf("RecordPaused = false, want true (a pause is still recorded)")
		}
	})

	t.Run("enterprise tier also notifies architect", func(t *testing.T) {
		plan, err := e.ApplyPausePolicy(fweng.Context{}, PauseRequestContext{
			ProjectID:         "p1",
			InFlightPipelines: []PipelineRef{"pipe-a"},
			Policy:            tieredPolicy(0, 0, SLATierEnterprise),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !containsTarget(plan.NotifyTargets, NotifyArchitect) {
			t.Fatalf("notify targets = %v, want to include architect", plan.NotifyTargets)
		}
	})

	t.Run("empty project id is contract misuse", func(t *testing.T) {
		_, err := e.ApplyPausePolicy(fweng.Context{}, PauseRequestContext{Policy: launchPolicy()})
		assertEngineErr(t, err, fweng.ContractMisuse)
	})

	t.Run("unknown policy mode is invalid input", func(t *testing.T) {
		_, err := e.ApplyPausePolicy(fweng.Context{}, PauseRequestContext{ProjectID: "p1", Policy: InterventionPolicy{Mode: InterventionModeUnknown}})
		assertEngineErr(t, err, fweng.InvalidInput)
	})
}

// --- Determinism (FU-IE-A) -----------------------------------------------------

// TestDeterminism asserts that identical inputs yield identical outputs across
// repeated invocations of every op (the Engine reads no clock/RNG/state).
func TestDeterminism(t *testing.T) {
	e := New()

	variance := ConstructionVariance{ProjectID: "p1", ActivityID: "a1", Kind: WorkerMiss, AttemptCount: 5, Severity: SeverityLow, Policy: tieredPolicy(3, 5, SLATierEnterprise)}
	change := HealthChange{OperatedAppID: "app1", FromHealth: HealthHealthy, ToHealth: HealthDegraded, SLOStatus: SLOOutOfBudget, Policy: tieredPolicy(3, 5, SLATierPaid)}
	failure := SettlementFailure{CustomerID: "c1", CycleID: "cyc1", Kind: ChargeDeclined, AttemptCount: 4, ShortfallAge: 3, Policy: tieredPolicy(3, 5, SLATierFree)}
	pause := PauseRequestContext{ProjectID: "p1", InFlightPipelines: []PipelineRef{"pipe-a"}, Policy: tieredPolicy(3, 5, SLATierEnterprise)}

	v0, err := e.DecideOnVariance(fweng.Context{}, variance)
	if err != nil {
		t.Fatalf("variance seed: %v", err)
	}
	h0, err := e.DecideOnHealth(fweng.Context{}, change)
	if err != nil {
		t.Fatalf("health seed: %v", err)
	}
	s0, err := e.DecideOnSettlementFailure(fweng.Context{}, failure)
	if err != nil {
		t.Fatalf("settlement seed: %v", err)
	}
	p0, err := e.ApplyPausePolicy(fweng.Context{}, pause)
	if err != nil {
		t.Fatalf("pause seed: %v", err)
	}

	for i := 0; i < 100; i++ {
		v, err := e.DecideOnVariance(fweng.Context{}, variance)
		if err != nil || v != v0 {
			t.Fatalf("variance non-deterministic at %d: %v err=%v", i, v, err)
		}
		h, err := e.DecideOnHealth(fweng.Context{}, change)
		if err != nil || h != h0 {
			t.Fatalf("health non-deterministic at %d: %v err=%v", i, h, err)
		}
		s, err := e.DecideOnSettlementFailure(fweng.Context{}, failure)
		if err != nil || s != s0 {
			t.Fatalf("settlement non-deterministic at %d: %v err=%v", i, s, err)
		}
		p, err := e.ApplyPausePolicy(fweng.Context{}, pause)
		if err != nil || !pausePlanEqual(p, p0) {
			t.Fatalf("pause non-deterministic at %d: %+v err=%v", i, p, err)
		}
	}
}

// --- helpers -------------------------------------------------------------------

func assertEngineErr(t *testing.T, err error, wantKind fweng.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %v, got nil", wantKind)
	}
	var ee *fweng.Error
	if !errors.As(err, &ee) {
		t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
	}
	if ee.Kind != wantKind {
		t.Fatalf("error kind = %v, want %v (detail %q)", ee.Kind, wantKind, ee.Detail)
	}
	if ee.Retryable {
		t.Fatalf("engine error must never be retryable")
	}
}

func containsTarget(targets []NotifyTarget, want NotifyTarget) bool {
	for _, t := range targets {
		if t == want {
			return true
		}
	}
	return false
}

func pausePlanEqual(a, b PausePlan) bool {
	if a.RecordPaused != b.RecordPaused || a.ResumeHint != b.ResumeHint {
		return false
	}
	if len(a.PipelinesToCancel) != len(b.PipelinesToCancel) || len(a.NotifyTargets) != len(b.NotifyTargets) {
		return false
	}
	for i := range a.PipelinesToCancel {
		if a.PipelinesToCancel[i] != b.PipelinesToCancel[i] {
			return false
		}
	}
	for i := range a.NotifyTargets {
		if a.NotifyTargets[i] != b.NotifyTargets[i] {
			return false
		}
	}
	return true
}
