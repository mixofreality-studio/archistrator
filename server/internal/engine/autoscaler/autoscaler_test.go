package autoscaler

// Service Test Plan (STP) — autoscalerEngine (C-AE).
//
// Per [[the-method-testing]]: the STP is the list of all the ways to demonstrate
// the Engine does NOT work, written before the code. NO BDD/Gherkin. These are
// regression-style table-driven Go unit tests asserting BEHAVIOUR (decision kind,
// reason code, delta/baseline bounds, error kind) — not exact human strings.
//
// Contract guarantees under test (autoscalerEngine.md §2.1, §3, §6):
//
//   STP-1  Pre-condition: policy.Kind != infrastructureKind ⇒ ContractMisuse.
//   STP-2  Pre-condition: currentDesired.InfrastructureKind != infrastructureKind ⇒ ContractMisuse.
//   STP-3  Unknown infrastructure (no strategy registered) ⇒ InvalidInput (NOT a default fall-through).
//   STP-4  Manual mode ⇒ NoChange/ManualMode regardless of telemetry (operator override).
//   STP-5  Pinned ⇒ NoChange/Pinned regardless of telemetry (operator override).
//   STP-6  Resume-from-zero: paused + traffic resumed ⇒ Resume(ToBaseline>0)/TrafficResumed
//          (distinct from ScaleUp).
//   STP-7  Idle-pause: idle ≥ threshold + zero traffic + Min==0 ⇒ Pause/Idle.
//   STP-8  Idle-pause disabled when MinReplicas>0 (never pause below the floor).
//   STP-9  Idle-pause disabled when IdleThreshold==0.
//   STP-10 SLO burning + room ⇒ ScaleUp/SLOBurnDown; at max ⇒ NoChange/AlreadyAtMax.
//   STP-11 CPU ≥ ScaleUpCPU + room ⇒ ScaleUp/CPUHigh; at max ⇒ NoChange/AlreadyAtMax.
//   STP-12 CPU < ScaleDownCPU sustained + room ⇒ ScaleDown/CPUSustainedLow; at min ⇒ NoChange/AlreadyAtMin.
//   STP-13 CPU low but NOT sustained (within grace) ⇒ NoChange (anti-flap).
//   STP-14 Steady (all within thresholds) ⇒ NoChange/Steady.
//   STP-15 Post-condition: ScaleUp delta bounded by MaxStepDelta, MaxBurstCap, and MaxReplicas headroom; ≥1.
//   STP-16 Post-condition: ScaleDown delta bounded by MaxStepDelta and MinReplicas floor; ≥1.
//   STP-17 Resume ToBaseline modulated by SLA tier, clamped to [Min,Max], ≥1.
//   STP-18 Determinism: identical inputs ⇒ identical Decision across many invocations
//          (the determinism assertion — no clock/RNG/state).
//
// All errors are programmer errors and MUST be non-retryable (Engine does no I/O).

import (
	"errors"
	"testing"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// eng is the stateless engine under test, exercised through the exported port.
var eng = New()

const launchKind = InfrastructureKindGoTemporalPostgres

// autoPolicy is a well-formed launch policy in Auto mode with idle-pause enabled
// (Min==0) and reactive CPU thresholds. Tests override individual fields.
func autoPolicy() AutoscalerPolicy {
	return AutoscalerPolicy{
		Kind:             launchKind,
		Mode:             AutoscalerModeAuto,
		MinReplicas:      0,
		MaxReplicas:      10,
		BaselineReplicas: 2,
		MaxStepDelta:     3,
		IdleThreshold:    5 * time.Minute,
		ScaleUpCPU:       0.80,
		ScaleDownCPU:     0.20,
		ScaleDownGrace:   2 * time.Minute,
		SLATier:          SLATierFree,
		MaxBurstCap:      5,
	}
}

// desired builds a DesiredState on the launch infrastructure at n replicas.
func desired(n int64) DesiredState {
	return DesiredState{InfrastructureKind: launchKind, Replicas: n}
}

func TestProposeDesiredState_Preconditions(t *testing.T) {
	tests := []struct {
		name    string
		policy  AutoscalerPolicy
		cur     DesiredState
		kind    InfrastructureKind
		errKind fweng.Kind
	}{
		{ // STP-1
			name:    "policy kind mismatch is contract misuse",
			policy:  AutoscalerPolicy{Kind: InfrastructureKindUnknown, Mode: AutoscalerModeAuto},
			cur:     desired(2),
			kind:    launchKind,
			errKind: fweng.ContractMisuse,
		},
		{ // STP-2
			name:    "desired-state kind mismatch is contract misuse",
			policy:  autoPolicy(),
			cur:     DesiredState{InfrastructureKind: InfrastructureKindUnknown, Replicas: 2},
			kind:    launchKind,
			errKind: fweng.ContractMisuse,
		},
		{ // STP-3
			name:    "unknown infrastructure is invalid input (no default fall-through)",
			policy:  AutoscalerPolicy{Kind: InfrastructureKindUnknown, Mode: AutoscalerModeAuto},
			cur:     DesiredState{InfrastructureKind: InfrastructureKindUnknown, Replicas: 2},
			kind:    InfrastructureKindUnknown,
			errKind: fweng.InvalidInput,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := eng.ProposeDesiredState(fweng.Context{}, Telemetry{}, tt.cur, tt.policy, tt.kind)
			assertEngineErr(t, err, tt.errKind)
		})
	}
}

func TestProposeDesiredState_OperatorOverrides(t *testing.T) {
	// Telemetry that would otherwise force a strong scale-up, to prove the
	// override wins regardless of telemetry.
	hot := Telemetry{RequestsPerSecond: 1000, CPUUtilization: 0.99, SLOStatus: SLOOutOfBudget}

	t.Run("manual mode always NoChange (STP-4)", func(t *testing.T) {
		p := autoPolicy()
		p.Mode = AutoscalerModeManual
		got, err := eng.ProposeDesiredState(fweng.Context{}, hot, desired(2), p, launchKind)
		mustNoErr(t, err)
		assertKind(t, got, DecisionNoChange, ReasonManualMode)
	})

	t.Run("pinned always NoChange (STP-5)", func(t *testing.T) {
		p := autoPolicy()
		p.Pinned = true
		got, err := eng.ProposeDesiredState(fweng.Context{}, hot, desired(2), p, launchKind)
		mustNoErr(t, err)
		assertKind(t, got, DecisionNoChange, ReasonPinned)
	})
}

func TestProposeDesiredState_PauseResume(t *testing.T) {
	t.Run("paused + traffic resumed ⇒ Resume to baseline (STP-6)", func(t *testing.T) {
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 12},
			desired(0),
			autoPolicy(),
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionResume, ReasonTrafficResumed)
		if got.ToBaseline != 2 { // Free tier, baseline 2, no bump
			t.Fatalf("ToBaseline = %d, want 2", got.ToBaseline)
		}
	})

	t.Run("paused + still idle ⇒ NoChange (stays paused)", func(t *testing.T) {
		got, err := eng.ProposeDesiredState(fweng.Context{}, Telemetry{}, desired(0), autoPolicy(), launchKind)
		mustNoErr(t, err)
		assertKind(t, got, DecisionNoChange, ReasonSteady)
	})

	t.Run("idle ≥ threshold + zero traffic + Min==0 ⇒ Pause (STP-7)", func(t *testing.T) {
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 0, TimeSinceLastRequest: 10 * time.Minute},
			desired(2),
			autoPolicy(),
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionPause, ReasonIdle)
	})

	t.Run("idle-pause disabled when MinReplicas>0 (STP-8)", func(t *testing.T) {
		p := autoPolicy()
		p.MinReplicas = 1
		// Idle telemetry; but low CPU sustained would scale down toward min, not pause.
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 0, TimeSinceLastRequest: 10 * time.Minute, CPUUtilization: 0.05},
			desired(1),
			p,
			launchKind,
		)
		mustNoErr(t, err)
		if got.Kind == DecisionPause {
			t.Fatalf("must not Pause when MinReplicas>0, got %v", got.Kind)
		}
	})

	t.Run("idle-pause disabled when IdleThreshold==0 (STP-9)", func(t *testing.T) {
		p := autoPolicy()
		p.IdleThreshold = 0
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 0, TimeSinceLastRequest: time.Hour, CPUUtilization: 0.5},
			desired(2),
			p,
			launchKind,
		)
		mustNoErr(t, err)
		if got.Kind == DecisionPause {
			t.Fatalf("must not Pause when IdleThreshold==0, got %v", got.Kind)
		}
	})
}

func TestProposeDesiredState_ScaleUp(t *testing.T) {
	t.Run("SLO burning + room ⇒ ScaleUp/SLOBurnDown (STP-10)", func(t *testing.T) {
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 50, SLOStatus: SLOBurningBudget, CPUUtilization: 0.3},
			desired(2),
			autoPolicy(),
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionScaleUp, ReasonSLOBurnDown)
		if got.Delta <= 0 {
			t.Fatalf("ScaleUp Delta must be > 0, got %d", got.Delta)
		}
	})

	t.Run("SLO burning at max ⇒ NoChange/AlreadyAtMax (STP-10)", func(t *testing.T) {
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 50, SLOStatus: SLOOutOfBudget},
			desired(10), // == MaxReplicas
			autoPolicy(),
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionNoChange, ReasonAlreadyAtMax)
	})

	t.Run("CPU high + room ⇒ ScaleUp/CPUHigh (STP-11)", func(t *testing.T) {
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 80, CPUUtilization: 0.95},
			desired(2),
			autoPolicy(),
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionScaleUp, ReasonCPUHigh)
	})

	t.Run("CPU high at max ⇒ NoChange/AlreadyAtMax (STP-11)", func(t *testing.T) {
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 80, CPUUtilization: 0.95},
			desired(10),
			autoPolicy(),
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionNoChange, ReasonAlreadyAtMax)
	})
}

func TestProposeDesiredState_ScaleDown(t *testing.T) {
	t.Run("CPU low sustained + room ⇒ ScaleDown/CPUSustainedLow (STP-12)", func(t *testing.T) {
		p := autoPolicy()
		p.MinReplicas = 1 // so idle-pause does not preempt; floor at 1
		cur := desired(5)
		cur.LastDecisionAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{
				RequestsPerSecond: 1, // non-zero so it is not an idle-pause candidate
				CPUUtilization:    0.05,
				ObservedAt:        cur.LastDecisionAt.Add(5 * time.Minute), // > ScaleDownGrace
			},
			cur,
			p,
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionScaleDown, ReasonCPUSustainedLow)
	})

	t.Run("CPU low at min ⇒ NoChange/AlreadyAtMin (STP-12)", func(t *testing.T) {
		p := autoPolicy()
		p.MinReplicas = 1
		cur := desired(1)
		cur.LastDecisionAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 1, CPUUtilization: 0.05, ObservedAt: cur.LastDecisionAt.Add(time.Hour)},
			cur,
			p,
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionNoChange, ReasonAlreadyAtMin)
	})

	t.Run("CPU low but within grace ⇒ NoChange anti-flap (STP-13)", func(t *testing.T) {
		p := autoPolicy()
		p.MinReplicas = 1
		cur := desired(5)
		cur.LastDecisionAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{
				RequestsPerSecond: 1,
				CPUUtilization:    0.05,
				ObservedAt:        cur.LastDecisionAt.Add(30 * time.Second), // < ScaleDownGrace (2m)
			},
			cur,
			p,
			launchKind,
		)
		mustNoErr(t, err)
		if got.Kind == DecisionScaleDown {
			t.Fatalf("must not scale down within the grace window (anti-flap)")
		}
		assertKind(t, got, DecisionNoChange, ReasonSteady)
	})
}

func TestProposeDesiredState_Steady(t *testing.T) {
	// STP-14: mid-range CPU, traffic flowing, SLO healthy ⇒ NoChange/Steady.
	got, err := eng.ProposeDesiredState(
		fweng.Context{},
		Telemetry{RequestsPerSecond: 40, CPUUtilization: 0.50, SLOStatus: SLOWithinBudget},
		desired(3),
		autoPolicy(),
		launchKind,
	)
	mustNoErr(t, err)
	assertKind(t, got, DecisionNoChange, ReasonSteady)
}

func TestProposeDesiredState_DeltaAndBaselineBounds(t *testing.T) {
	t.Run("ScaleUp delta bounded by MaxBurstCap and headroom (STP-15)", func(t *testing.T) {
		p := autoPolicy()
		p.MaxStepDelta = 100 // unbounded by step
		p.MaxBurstCap = 4    // capped by burst
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 80, CPUUtilization: 0.99},
			desired(2),
			p,
			launchKind,
		)
		mustNoErr(t, err)
		if got.Delta != 4 {
			t.Fatalf("Delta = %d, want 4 (MaxBurstCap)", got.Delta)
		}
	})

	t.Run("ScaleUp delta clamped by MaxReplicas headroom (STP-15)", func(t *testing.T) {
		p := autoPolicy()
		p.MaxStepDelta = 100
		p.MaxBurstCap = 100
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 80, CPUUtilization: 0.99},
			desired(9), // only 1 of headroom to Max=10
			p,
			launchKind,
		)
		mustNoErr(t, err)
		if got.Delta != 1 {
			t.Fatalf("Delta = %d, want 1 (headroom to MaxReplicas)", got.Delta)
		}
	})

	t.Run("ScaleDown delta clamped by MinReplicas floor (STP-16)", func(t *testing.T) {
		p := autoPolicy()
		p.MinReplicas = 3
		p.MaxStepDelta = 100
		cur := desired(4) // only 1 of room to Min=3
		cur.LastDecisionAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		got, err := eng.ProposeDesiredState(
			fweng.Context{},
			Telemetry{RequestsPerSecond: 1, CPUUtilization: 0.01, ObservedAt: cur.LastDecisionAt.Add(time.Hour)},
			cur,
			p,
			launchKind,
		)
		mustNoErr(t, err)
		assertKind(t, got, DecisionScaleDown, ReasonCPUSustainedLow)
		if got.Delta != 1 {
			t.Fatalf("Delta = %d, want 1 (room to MinReplicas)", got.Delta)
		}
	})

	t.Run("Resume ToBaseline modulated by SLA tier and clamped (STP-17)", func(t *testing.T) {
		p := autoPolicy()
		p.SLATier = SLATierEnterprise // +2 over baseline 2 ⇒ 4
		got, err := eng.ProposeDesiredState(fweng.Context{}, Telemetry{RequestsPerSecond: 5}, desired(0), p, launchKind)
		mustNoErr(t, err)
		assertKind(t, got, DecisionResume, ReasonTrafficResumed)
		if got.ToBaseline != 4 {
			t.Fatalf("ToBaseline = %d, want 4 (baseline 2 + enterprise +2)", got.ToBaseline)
		}
	})

	t.Run("Resume ToBaseline clamped to MaxReplicas (STP-17)", func(t *testing.T) {
		p := autoPolicy()
		p.BaselineReplicas = 9
		p.MaxReplicas = 10
		p.SLATier = SLATierEnterprise // 9 + 2 = 11 ⇒ clamp to 10
		got, err := eng.ProposeDesiredState(fweng.Context{}, Telemetry{RequestsPerSecond: 5}, desired(0), p, launchKind)
		mustNoErr(t, err)
		if got.ToBaseline != 10 {
			t.Fatalf("ToBaseline = %d, want 10 (clamped to MaxReplicas)", got.ToBaseline)
		}
	})
}

// TestDeterminism (STP-18) asserts identical inputs yield identical Decisions
// across many invocations — the Engine reads no clock/RNG/state. This exercises
// all five DecisionKind outcomes plus the two operator short-circuits.
func TestDeterminism(t *testing.T) {
	type scenario struct {
		name string
		tel  Telemetry
		cur  DesiredState
		pol  AutoscalerPolicy
	}
	graceAnchor := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	downPolicy := func() AutoscalerPolicy { p := autoPolicy(); p.MinReplicas = 1; return p }

	scenarios := []scenario{
		{"NoChange/Steady", Telemetry{RequestsPerSecond: 40, CPUUtilization: 0.5, SLOStatus: SLOWithinBudget}, desired(3), autoPolicy()},
		{"ScaleUp", Telemetry{RequestsPerSecond: 80, CPUUtilization: 0.99}, desired(2), autoPolicy()},
		{"ScaleDown", Telemetry{RequestsPerSecond: 1, CPUUtilization: 0.01, ObservedAt: graceAnchor.Add(time.Hour)}, mustDesiredAt(5, graceAnchor), downPolicy()},
		{"Pause", Telemetry{RequestsPerSecond: 0, TimeSinceLastRequest: 10 * time.Minute}, desired(2), autoPolicy()},
		{"Resume", Telemetry{RequestsPerSecond: 5}, desired(0), autoPolicy()},
		{"ManualMode", Telemetry{CPUUtilization: 0.99}, desired(2), func() AutoscalerPolicy { p := autoPolicy(); p.Mode = AutoscalerModeManual; return p }()},
		{"Pinned", Telemetry{CPUUtilization: 0.99}, desired(2), func() AutoscalerPolicy { p := autoPolicy(); p.Pinned = true; return p }()},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			first, err := eng.ProposeDesiredState(fweng.Context{}, sc.tel, sc.cur, sc.pol, launchKind)
			mustNoErr(t, err)
			for i := 0; i < 200; i++ {
				got, err := eng.ProposeDesiredState(fweng.Context{}, sc.tel, sc.cur, sc.pol, launchKind)
				mustNoErr(t, err)
				if got != first {
					t.Fatalf("call %d non-deterministic: %+v != %+v", i, got, first)
				}
			}
		})
	}
}

// --- helpers ----------------------------------------------------------------

func mustDesiredAt(n int64, lastDecision time.Time) DesiredState {
	d := desired(n)
	d.LastDecisionAt = lastDecision
	return d
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertKind(t *testing.T, d Decision, wantKind DecisionKind, wantReason ReasonCode) {
	t.Helper()
	if d.Kind != wantKind {
		t.Fatalf("decision kind = %v, want %v (reason %v)", d.Kind, wantKind, d.Reason.Code)
	}
	if d.Reason.Code != wantReason {
		t.Fatalf("reason code = %v, want %v", d.Reason.Code, wantReason)
	}
}

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
