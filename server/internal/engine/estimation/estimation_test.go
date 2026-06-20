package estimation

import (
	"errors"
	"reflect"
	"testing"

	fweng "github.com/davidmarne/archistrator-platform/framework-go/engine"

	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// usd builds a USD Money rate.
func usd(minor int64) projectstate.Money {
	return projectstate.Money{MinorUnits: minor, Currency: "USD"}
}

// mixedActivityOption is a representative happy-path option: 3 activities, 2 of
// them on the critical path, a 5 d/wk calendar (stretch 1.0).
func mixedActivityOption() projectstate.ProjectOption {
	return projectstate.ProjectOption{
		OptionID: "normal",
		Network: projectstate.ActivityNetwork{
			Activities: []projectstate.OptionActivity{
				{ActivityID: "a1", EffortDays: 5, WorkerClass: "senior", OnCriticalPath: true, RiskBucket: 8},
				{ActivityID: "a2", EffortDays: 10, WorkerClass: "junior", OnCriticalPath: true, RiskBucket: 5},
				{ActivityID: "a3", EffortDays: 5, WorkerClass: "senior", OnCriticalPath: false, RiskBucket: 2},
			},
		},
		WorkerMix: projectstate.WorkerMix{
			ClassRates:  map[string]projectstate.Money{"senior": usd(100000), "junior": usd(40000)},
			StaffingCap: 2,
		},
		CalendarDaysPerWeek: 5,
	}
}

func TestEstimateForOption(t *testing.T) {
	type want struct {
		duration  float64
		buildCost projectstate.Money
		risk      RiskScore
	}
	tests := []struct {
		name        string
		option      projectstate.ProjectOption
		wantErrKind fweng.Kind // -1 sentinel below means "no error expected"
		want        want
	}{
		{
			name:        "happy path mixed activities",
			option:      mixedActivityOption(),
			wantErrKind: -1,
			want: want{
				// critical-path effort = 5 + 10 = 15; stretch (5 d/wk) = 1.0.
				duration: 15,
				// 5*100000 + 10*40000 + 5*100000 = 500000+400000+500000 = 1400000.
				buildCost: usd(1400000),
				risk: RiskScore{
					// 2 of 3 critical = 0.6666..; buckets (8+5+2)=15 / (3*13)=39 => 0.3846..
					CriticalityRisk: 2.0 / 3.0,
					ActivityRisk:    15.0 / 39.0,
					Composite:       0.5*(2.0/3.0) + 0.5*(15.0/39.0),
				},
			},
		},
		{
			name: "all activities on critical path",
			option: projectstate.ProjectOption{
				OptionID: "compressed",
				Network: projectstate.ActivityNetwork{
					Activities: []projectstate.OptionActivity{
						{ActivityID: "a1", EffortDays: 4, WorkerClass: "w", OnCriticalPath: true, RiskBucket: 13},
						{ActivityID: "a2", EffortDays: 6, WorkerClass: "w", OnCriticalPath: true, RiskBucket: 13},
					},
				},
				WorkerMix: projectstate.WorkerMix{
					ClassRates:  map[string]projectstate.Money{"w": usd(10000)},
					StaffingCap: 3,
				},
				CalendarDaysPerWeek: 5,
			},
			wantErrKind: -1,
			want: want{
				duration:  10,          // 4 + 6, stretch 1.0
				buildCost: usd(100000), // (4+6)*10000
				risk: RiskScore{
					CriticalityRisk: 1.0, // all critical
					ActivityRisk:    1.0, // (13+13)/(2*13) = 1.0
					Composite:       1.0, // clamp01(0.5+0.5)
				},
			},
		},
		{
			name: "no critical path falls back to parallelism and calendar stretch",
			option: projectstate.ProjectOption{
				OptionID: "subcritical",
				Network: projectstate.ActivityNetwork{
					Activities: []projectstate.OptionActivity{
						{ActivityID: "a1", EffortDays: 10, WorkerClass: "w", OnCriticalPath: false, RiskBucket: 1},
						{ActivityID: "a2", EffortDays: 10, WorkerClass: "w", OnCriticalPath: false, RiskBucket: 1},
					},
				},
				WorkerMix: projectstate.WorkerMix{
					ClassRates:  map[string]projectstate.Money{"w": usd(1000)},
					StaffingCap: 2,
				},
				CalendarDaysPerWeek: 2, // stretch = 5/2 = 2.5
			},
			wantErrKind: -1,
			want: want{
				duration:  25,         // (20 total / cap 2) = 10, * 2.5 stretch
				buildCost: usd(20000), // 20 * 1000
				risk: RiskScore{
					CriticalityRisk: 0,          // none critical
					ActivityRisk:    2.0 / 26.0, // (1+1)/(2*13)
					Composite:       0.5 * (2.0 / 26.0),
				},
			},
		},
		{
			name: "contract misuse: empty network",
			option: projectstate.ProjectOption{
				OptionID:  "empty",
				Network:   projectstate.ActivityNetwork{Activities: nil},
				WorkerMix: projectstate.WorkerMix{ClassRates: map[string]projectstate.Money{}},
			},
			wantErrKind: fweng.ContractMisuse,
		},
		{
			name: "contract misuse: negative effort",
			option: projectstate.ProjectOption{
				OptionID: "bad-effort",
				Network: projectstate.ActivityNetwork{
					Activities: []projectstate.OptionActivity{
						{ActivityID: "a1", EffortDays: -1, WorkerClass: "w", OnCriticalPath: true, RiskBucket: 1},
					},
				},
				WorkerMix: projectstate.WorkerMix{ClassRates: map[string]projectstate.Money{"w": usd(1000)}},
			},
			wantErrKind: fweng.ContractMisuse,
		},
		{
			name: "contract misuse: unknown worker class",
			option: projectstate.ProjectOption{
				OptionID: "bad-class",
				Network: projectstate.ActivityNetwork{
					Activities: []projectstate.OptionActivity{
						{ActivityID: "a1", EffortDays: 5, WorkerClass: "ghost", OnCriticalPath: true, RiskBucket: 1},
					},
				},
				WorkerMix: projectstate.WorkerMix{ClassRates: map[string]projectstate.Money{"w": usd(1000)}},
			},
			wantErrKind: fweng.ContractMisuse,
		},
		{
			name: "contract misuse: mixed rate currencies",
			option: projectstate.ProjectOption{
				OptionID: "bad-currency",
				Network: projectstate.ActivityNetwork{
					Activities: []projectstate.OptionActivity{
						{ActivityID: "a1", EffortDays: 5, WorkerClass: "usd", OnCriticalPath: true, RiskBucket: 1},
						{ActivityID: "a2", EffortDays: 5, WorkerClass: "eur", OnCriticalPath: true, RiskBucket: 1},
					},
				},
				WorkerMix: projectstate.WorkerMix{ClassRates: map[string]projectstate.Money{
					"usd": {MinorUnits: 1000, Currency: "USD"},
					"eur": {MinorUnits: 1000, Currency: "EUR"},
				}},
			},
			wantErrKind: fweng.ContractMisuse,
		},
	}

	eng := New()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eng.EstimateForOption(tc.option)

			if tc.wantErrKind != -1 {
				if err == nil {
					t.Fatalf("expected error of kind %v, got nil (result %+v)", tc.wantErrKind, got)
				}
				var fe *fweng.Error
				if !errors.As(err, &fe) {
					t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
				}
				if fe.Kind != tc.wantErrKind {
					t.Fatalf("expected kind %v, got %v (detail: %s)", tc.wantErrKind, fe.Kind, fe.Detail)
				}
				if fe.Retryable {
					t.Errorf("engine errors must never be retryable")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.DurationDays != tc.want.duration {
				t.Errorf("DurationDays = %v, want %v", got.DurationDays, tc.want.duration)
			}
			if got.BuildCost != tc.want.buildCost {
				t.Errorf("BuildCost = %+v, want %+v", got.BuildCost, tc.want.buildCost)
			}
			if got.Risk != tc.want.risk {
				t.Errorf("Risk = %+v, want %+v", got.Risk, tc.want.risk)
			}
			// Risk components must stay within [0,1].
			for name, v := range map[string]float64{
				"Composite": got.Risk.Composite, "CriticalityRisk": got.Risk.CriticalityRisk,
				"ActivityRisk": got.Risk.ActivityRisk,
			} {
				if v < 0 || v > 1 {
					t.Errorf("%s = %v out of [0,1]", name, v)
				}
			}
		})
	}
}

// TestDeterminism asserts the pure-function contract: identical input twice ->
// byte-identical output (contract §6, FU-EE-B twice-called identical-output).
func TestDeterminism(t *testing.T) {
	eng := New()
	opt := mixedActivityOption()

	first, err1 := eng.EstimateForOption(opt)
	second, err2 := eng.EstimateForOption(opt)

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v / %v", err1, err2)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic output:\n first=%+v\nsecond=%+v", first, second)
	}
}
