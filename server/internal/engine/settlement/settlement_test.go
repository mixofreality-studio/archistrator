package settlement

import (
	"errors"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// launchTerms is the registered launch regime: flat 10% revenue share, flat compute
// markup of 20%, monthly schedule. Both pivot regimes are known. Uses the Engine's
// OWN generated SettlementTerms (Option B full encapsulation — no projectstate import).
func launchTerms() SettlementTerms {
	return SettlementTerms{
		RevenueShare:         RevenueShareLaunchFlat10,
		RevenueSharePercent:  10.0,
		ComputeCost:          ComputeCostFlatMarkup,
		ComputeMarkupPercent: 20.0,
		Schedule:             ScheduleMonthly,
	}
}

func usd(minor int64) Money {
	return Money{MinorUnits: minor, Currency: "USD"}
}

func TestProjectCommitTimeRevenueShareAndComputeCost(t *testing.T) {
	e := New()

	tests := []struct {
		name      string
		terms     SettlementTerms
		want      Projection
		wantErr   bool
		errKind   fweng.Kind
		errDetail string
	}{
		{
			name:  "happy path echoes the regime kinds and percents",
			terms: launchTerms(),
			want: Projection{
				RevenueShareKind:     RevenueShareLaunchFlat10,
				RevenueSharePercent:  10.0,
				ComputeCostKind:      ComputeCostFlatMarkup,
				ComputeMarkupPercent: 20.0,
			},
		},
		{
			name: "unknown revenue share is unknown-terms error",
			terms: SettlementTerms{
				RevenueShare: RevenueShareUnknown,
				ComputeCost:  ComputeCostFlatMarkup,
			},
			wantErr:   true,
			errKind:   fweng.InvalidInput,
			errDetail: "unknown terms",
		},
		{
			name: "unknown compute cost is unknown-terms error",
			terms: SettlementTerms{
				RevenueShare: RevenueShareLaunchFlat10,
				ComputeCost:  ComputeCostUnknown,
			},
			wantErr:   true,
			errKind:   fweng.InvalidInput,
			errDetail: "unknown terms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.ProjectCommitTimeRevenueShareAndComputeCost(
				ProjectOption{OptionID: "opt-1", Terms: tt.terms},
			)
			if tt.wantErr {
				assertEngineErr(t, err, tt.errKind, tt.errDetail)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("projection = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestComputeNet(t *testing.T) {
	e := New()

	tests := []struct {
		name      string
		revenue   CycleRevenue
		usage     CycleUsage
		terms     SettlementTerms
		want      SettlementResult
		wantErr   bool
		errKind   fweng.Kind
		errDetail string
	}{
		{
			// Payout: gross 100000, 10% share = 10000, compute = 100 units * 1 cent
			// = 100, * (1 + 20/100) = 120. net = 100000 - 10000 - 120 = 89880 (> 0).
			name:    "payout when net is positive",
			revenue: CycleRevenue{GrossInbound: usd(100000), EventCount: 7},
			usage:   CycleUsage{ComputeUnitSeconds: 100},
			terms:   launchTerms(),
			want: SettlementResult{
				SignedNet:           usd(89880),
				RoutingDirective:    RoutingPayout,
				RevenueShareApplied: usd(10000),
				ComputeCostApplied:  usd(120),
			},
		},
		{
			// Charge: tiny gross, heavy usage drives net negative.
			// gross 50, 10% share = 5, compute = 1000 units * 1 cent = 1000,
			// * 1.20 = 1200. net = 50 - 5 - 1200 = -1155 (< 0).
			name:    "charge when net is negative",
			revenue: CycleRevenue{GrossInbound: usd(50)},
			usage:   CycleUsage{ComputeUnitSeconds: 1000},
			terms:   launchTerms(),
			want: SettlementResult{
				SignedNet:           usd(-1155),
				RoutingDirective:    RoutingCharge,
				RevenueShareApplied: usd(5),
				ComputeCostApplied:  usd(1200),
			},
		},
		{
			// Zero revenue and zero usage: net 0, NoAction — a normal return, no error.
			name:    "zero net routes no action",
			revenue: CycleRevenue{GrossInbound: usd(0)},
			usage:   CycleUsage{},
			terms:   launchTerms(),
			want: SettlementResult{
				SignedNet:           usd(0),
				RoutingDirective:    RoutingNoAction,
				RevenueShareApplied: usd(0),
				ComputeCostApplied:  usd(0),
			},
		},
		{
			name:    "unknown terms is unknown-terms error",
			revenue: CycleRevenue{GrossInbound: usd(100000)},
			usage:   CycleUsage{ComputeUnitSeconds: 100},
			terms: SettlementTerms{
				RevenueShare: RevenueShareUnknown,
				ComputeCost:  ComputeCostUnknown,
			},
			wantErr:   true,
			errKind:   fweng.InvalidInput,
			errDetail: "unknown terms",
		},
		{
			name:    "negative gross inbound is contract misuse",
			revenue: CycleRevenue{GrossInbound: usd(-1)},
			usage:   CycleUsage{},
			terms:   launchTerms(),
			wantErr: true,
			errKind: fweng.ContractMisuse,
		},
		{
			name:    "empty currency is contract misuse",
			revenue: CycleRevenue{GrossInbound: Money{MinorUnits: 100000, Currency: ""}},
			usage:   CycleUsage{},
			terms:   launchTerms(),
			wantErr: true,
			errKind: fweng.ContractMisuse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.ComputeNet(tt.revenue, tt.usage, tt.terms)
			if tt.wantErr {
				assertEngineErr(t, err, tt.errKind, tt.errDetail)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("result = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRecomputeNet(t *testing.T) {
	e := New()

	// A chargeback reversal halved the gross from 100000 to 50000. The corrected
	// net is computed fresh from the reversal-adjusted revenue; the Manager computes
	// the delta vs PriorSettled (not the Engine's job).
	prior, err := e.ComputeNet(CycleRevenue{GrossInbound: usd(100000)}, CycleUsage{ComputeUnitSeconds: 100}, launchTerms())
	if err != nil {
		t.Fatalf("seeding prior settlement: %v", err)
	}

	got, err := e.RecomputeNet(ReSettlementInput{
		Revenue:      CycleRevenue{GrossInbound: usd(50000)},
		Usage:        CycleUsage{ComputeUnitSeconds: 100},
		Terms:        launchTerms(),
		PriorSettled: prior,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// gross 50000, 10% = 5000, compute 100*1*1.20 = 120. net = 50000-5000-120 = 44880.
	want := SettlementResult{
		SignedNet:           usd(44880),
		RoutingDirective:    RoutingPayout,
		RevenueShareApplied: usd(5000),
		ComputeCostApplied:  usd(120),
	}
	if got != want {
		t.Fatalf("recomputed result = %+v, want %+v", got, want)
	}

	// RecomputeNet honours the same money-safety guard.
	_, err = e.RecomputeNet(ReSettlementInput{
		Revenue: CycleRevenue{GrossInbound: usd(50000)},
		Terms:   SettlementTerms{RevenueShare: RevenueShareUnknown, ComputeCost: ComputeCostUnknown},
	})
	assertEngineErr(t, err, fweng.InvalidInput, "unknown terms")
}

// TestDeterminism asserts that identical inputs yield identical outputs across
// repeated invocations (the Engine reads no clock/RNG/state).
func TestDeterminism(t *testing.T) {
	e := New()
	revenue := CycleRevenue{GrossInbound: usd(123456), EventCount: 3}
	usage := CycleUsage{ComputeUnitSeconds: 250, StorageBytesMonths: 10, EgressBytes: 5}
	terms := launchTerms()

	first, err := e.ComputeNet(revenue, usage, terms)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	for i := 0; i < 100; i++ {
		got, err := e.ComputeNet(revenue, usage, terms)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got != first {
			t.Fatalf("call %d non-deterministic: %+v != %+v", i, got, first)
		}
	}
}

// TestMoneyIsExactInt64 asserts result money carries exact int64 minor units (no
// float money path): the computation over money produces equality with a
// hand-computed int64, and the share+cost+net reconcile exactly.
func TestMoneyIsExactInt64(t *testing.T) {
	e := New()
	// gross 99 cents, 33% share. 99*3300/10000 = 326700/10000 = 32 (integer floor).
	// compute 0. net = 99 - 32 - 0 = 67.
	got, err := e.ComputeNet(
		CycleRevenue{GrossInbound: usd(99)},
		CycleUsage{},
		SettlementTerms{
			RevenueShare:         RevenueShareNegotiatedRate,
			RevenueSharePercent:  33.0,
			ComputeCost:          ComputeCostFlatMarkup,
			ComputeMarkupPercent: 0,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RevenueShareApplied.MinorUnits != 32 {
		t.Fatalf("revenue share = %d minor units, want exact int64 32", got.RevenueShareApplied.MinorUnits)
	}
	if got.SignedNet.MinorUnits != 67 {
		t.Fatalf("signed net = %d minor units, want exact int64 67", got.SignedNet.MinorUnits)
	}
	// Reconciliation: net == gross − share − cost, exactly, in int64.
	reconciled := int64(99) - got.RevenueShareApplied.MinorUnits - got.ComputeCostApplied.MinorUnits
	if reconciled != got.SignedNet.MinorUnits {
		t.Fatalf("money does not reconcile exactly: %d != %d", reconciled, got.SignedNet.MinorUnits)
	}
}

func assertEngineErr(t *testing.T, err error, wantKind fweng.Kind, wantDetail string) {
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
	if wantDetail != "" && ee.Detail != wantDetail {
		t.Fatalf("error detail = %q, want %q", ee.Detail, wantDetail)
	}
	if ee.Retryable {
		t.Fatalf("engine error must never be retryable")
	}
}
