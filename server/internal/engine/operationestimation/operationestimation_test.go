package operationestimation

import (
	"errors"
	"reflect"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

func sampleOption() projectstate.ProjectOption {
	return projectstate.ProjectOption{
		OptionID:           "opt-normal",
		InfrastructureKind: projectstate.InfrastructureKindGoTemporalPostgres,
		Terms: projectstate.SettlementTerms{
			RevenueShare:        projectstate.RevenueShareLaunchFlat10,
			RevenueSharePercent: 10.0,
		},
		DeclaredUsage: projectstate.UsageAssumption{
			ExpectedDailyActiveUsers: 1000,
			RequestsPerMinute:        50,
			AvgPayloadBytes:          2048,
		},
	}
}

func sampleUsage() projectstate.UsageAssumption {
	return projectstate.UsageAssumption{
		ExpectedDailyActiveUsers: 1000,
		RequestsPerMinute:        50,
		AvgPayloadBytes:          2048,
	}
}

// asEngineError unwraps to a *fweng.Error and reports its Kind.
func asEngineError(t *testing.T, err error) *fweng.Error {
	t.Helper()
	var e *fweng.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
	}
	return e
}

func TestEstimateForOption(t *testing.T) {
	tests := []struct {
		name       string
		option     projectstate.ProjectOption
		usage      projectstate.UsageAssumption
		infra      projectstate.InfrastructureKind
		wantErr    bool
		wantKind   fweng.Kind
		assertGood func(t *testing.T, f OperationForecast)
	}{
		{
			name:   "happy path GoTemporalPostgres",
			option: sampleOption(),
			usage:  sampleUsage(),
			infra:  projectstate.InfrastructureKindGoTemporalPostgres,
			assertGood: func(t *testing.T, f OperationForecast) {
				pts := f.UsageCostCurve.Points
				if len(pts) == 0 {
					t.Fatal("expected a non-empty usage cost curve")
				}
				// monotonic non-decreasing in cost
				for i := 1; i < len(pts); i++ {
					if pts[i].ProjectedMonthlyCost.MinorUnits < pts[i-1].ProjectedMonthlyCost.MinorUnits {
						t.Fatalf("curve not monotonic at %d: %d < %d",
							i, pts[i].ProjectedMonthlyCost.MinorUnits, pts[i-1].ProjectedMonthlyCost.MinorUnits)
					}
				}
				// includes the 1.0 point
				found := false
				for _, p := range pts {
					if p.LoadMultiplier == 1.0 {
						found = true
					}
				}
				if !found {
					t.Fatal("usage cost curve missing the 1.0 load point")
				}
				// signed net: SensitivityLow (cheaper) >= ExpectedNet >= SensitivityHigh (costlier)
				net := f.PayoutVsShortfallForecast
				if net.SensitivityLow.MinorUnits < net.ExpectedPerCycleNet.MinorUnits {
					t.Fatalf("SensitivityLow (%d) should be >= ExpectedNet (%d)",
						net.SensitivityLow.MinorUnits, net.ExpectedPerCycleNet.MinorUnits)
				}
				if net.ExpectedPerCycleNet.MinorUnits < net.SensitivityHigh.MinorUnits {
					t.Fatalf("ExpectedNet (%d) should be >= SensitivityHigh (%d)",
						net.ExpectedPerCycleNet.MinorUnits, net.SensitivityHigh.MinorUnits)
				}
				if net.ExpectedPerCycleNet.Currency != "USD" {
					t.Fatalf("expected USD currency, got %q", net.ExpectedPerCycleNet.Currency)
				}
			},
		},
		{
			name:     "unknown infrastructure",
			option:   sampleOption(),
			usage:    sampleUsage(),
			infra:    projectstate.InfrastructureKindUnknown,
			wantErr:  true,
			wantKind: fweng.InvalidInput,
		},
		{
			name:   "all-zero declared usage is contract misuse",
			option: sampleOption(),
			usage: projectstate.UsageAssumption{
				ExpectedDailyActiveUsers: 0,
				RequestsPerMinute:        0,
				AvgPayloadBytes:          0,
			},
			infra:    projectstate.InfrastructureKindGoTemporalPostgres,
			wantErr:  true,
			wantKind: fweng.ContractMisuse,
		},
	}

	eng := New()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eng.EstimateForOption(tc.option, tc.usage, tc.infra)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (%+v)", got)
				}
				if k := asEngineError(t, err).Kind; k != tc.wantKind {
					t.Fatalf("expected kind %v, got %v", tc.wantKind, k)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.assertGood(t, got)
		})
	}
}

func TestProjectForOperatedApp(t *testing.T) {
	observed := ObservedUsage{
		ComputeUnitSeconds: 100_000,
		RequestCount:       5_000_000,
		StorageBytesMonths: 50 * bytesPerGiB,
		EgressBytes:        20 * bytesPerGiB,
		ObservedReplicas:   3,
	}

	tests := []struct {
		name       string
		observed   ObservedUsage
		infra      projectstate.InfrastructureKind
		points     []ScalePoint
		wantErr    bool
		wantKind   fweng.Kind
		assertGood func(t *testing.T, p CostProjection)
	}{
		{
			name:     "happy path with what-if points",
			observed: observed,
			infra:    projectstate.InfrastructureKindGoTemporalPostgres,
			points:   []ScalePoint{{LoadMultiplier: 2.0}, {LoadMultiplier: 5.0}},
			assertGood: func(t *testing.T, p CostProjection) {
				if p.CurrentRunRate.MinorUnits <= 0 {
					t.Fatalf("expected positive run rate, got %d", p.CurrentRunRate.MinorUnits)
				}
				if p.ProjectedMonthlyCost != p.CurrentRunRate {
					t.Fatalf("projected monthly (%v) should equal run rate (%v)",
						p.ProjectedMonthlyCost, p.CurrentRunRate)
				}
				pts := p.ScaleWhatIfCurve.Points
				// current-load 1.0 point must be present
				found1 := false
				for _, wp := range pts {
					if wp.LoadMultiplier == 1.0 {
						found1 = true
					}
				}
				if !found1 {
					t.Fatal("what-if curve missing the 1.0 current-load point")
				}
				// monotonic non-decreasing
				for i := 1; i < len(pts); i++ {
					if pts[i].ProjectedMonthlyCost.MinorUnits < pts[i-1].ProjectedMonthlyCost.MinorUnits {
						t.Fatalf("what-if curve not monotonic at %d", i)
					}
				}
			},
		},
		{
			name:     "empty what-if points yields only current-load point",
			observed: observed,
			infra:    projectstate.InfrastructureKindGoTemporalPostgres,
			points:   nil,
			assertGood: func(t *testing.T, p CostProjection) {
				if len(p.ScaleWhatIfCurve.Points) != 1 {
					t.Fatalf("expected exactly the current-load point, got %d", len(p.ScaleWhatIfCurve.Points))
				}
				if p.ScaleWhatIfCurve.Points[0].LoadMultiplier != 1.0 {
					t.Fatalf("expected 1.0 load point, got %v", p.ScaleWhatIfCurve.Points[0].LoadMultiplier)
				}
			},
		},
		{
			name:     "non-positive load multiplier is invalid input",
			observed: observed,
			infra:    projectstate.InfrastructureKindGoTemporalPostgres,
			points:   []ScalePoint{{LoadMultiplier: 0}},
			wantErr:  true,
			wantKind: fweng.InvalidInput,
		},
		{
			name:     "negative load multiplier is invalid input",
			observed: observed,
			infra:    projectstate.InfrastructureKindGoTemporalPostgres,
			points:   []ScalePoint{{LoadMultiplier: -2.0}},
			wantErr:  true,
			wantKind: fweng.InvalidInput,
		},
		{
			name:     "unknown infrastructure",
			observed: observed,
			infra:    projectstate.InfrastructureKindUnknown,
			points:   nil,
			wantErr:  true,
			wantKind: fweng.InvalidInput,
		},
	}

	eng := New()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := eng.ProjectForOperatedApp(tc.observed, tc.infra, tc.points)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (%+v)", got)
				}
				if k := asEngineError(t, err).Kind; k != tc.wantKind {
					t.Fatalf("expected kind %v, got %v", tc.wantKind, k)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.assertGood(t, got)
		})
	}
}

// TestDeterminism asserts both ops are pure: identical inputs → identical output,
// across repeated and independent-instance calls.
func TestDeterminism(t *testing.T) {
	opt := sampleOption()
	usage := sampleUsage()
	infra := projectstate.InfrastructureKindGoTemporalPostgres

	f1, err := New().EstimateForOption(opt, usage, infra)
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	f2, err := New().EstimateForOption(opt, usage, infra)
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if !reflect.DeepEqual(f1, f2) {
		t.Fatalf("EstimateForOption not deterministic:\n%+v\n!=\n%+v", f1, f2)
	}

	observed := ObservedUsage{
		ComputeUnitSeconds: 100_000,
		RequestCount:       5_000_000,
		StorageBytesMonths: 50 * bytesPerGiB,
		EgressBytes:        20 * bytesPerGiB,
		ObservedReplicas:   3,
	}
	points := []ScalePoint{{LoadMultiplier: 3.0}, {LoadMultiplier: 1.5}}

	p1, err := New().ProjectForOperatedApp(observed, infra, points)
	if err != nil {
		t.Fatalf("project call 1: %v", err)
	}
	p2, err := New().ProjectForOperatedApp(observed, infra, points)
	if err != nil {
		t.Fatalf("project call 2: %v", err)
	}
	if !reflect.DeepEqual(p1, p2) {
		t.Fatalf("ProjectForOperatedApp not deterministic:\n%+v\n!=\n%+v", p1, p2)
	}
}
