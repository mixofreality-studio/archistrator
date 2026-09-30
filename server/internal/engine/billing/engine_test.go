package billing

import (
	"errors"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// launchTerms is the registered launch regime: flat compute markup of 20%, monthly
// schedule. The pivot regime is known. Uses the Engine's OWN generated BillingTerms
// (Option B full encapsulation — no projectstate import).
//
// It used to carry a flat 10% REVENUE SHARE as well. Revenue share left the vocabulary
// with stage 4b2 (founder ruling), so there is no share to register, no share to apply
// and no unknown-share regime to refuse — and every expectation below is the same
// arithmetic with that term removed rather than set to zero.
func launchTerms() BillingTerms {
	return BillingTerms{
		ComputeCost:          ComputeCostFlatMarkup,
		ComputeMarkupPercent: 20.0,
		Schedule:             ScheduleMonthly,
	}
}

func usd(minor int64) Money {
	return Money{MinorUnits: minor, Currency: "USD"}
}

func TestProjectCommitTimeComputeCost(t *testing.T) {
	e := NewBillingEngine()

	tests := []struct {
		name      string
		terms     BillingTerms
		want      Projection
		wantErr   bool
		errKind   fweng.Kind
		errDetail string
	}{
		{
			name:  "happy path echoes the regime kind and percent",
			terms: launchTerms(),
			want: Projection{
				ComputeCostKind:      ComputeCostFlatMarkup,
				ComputeMarkupPercent: 20.0,
			},
		},
		{
			name: "unknown compute cost is unknown-terms error",
			terms: BillingTerms{
				ComputeCost: ComputeCostUnknown,
			},
			wantErr:   true,
			errKind:   fweng.InvalidInput,
			errDetail: "unknown terms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.ProjectCommitTimeComputeCost(
				fweng.Context{},
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
	e := NewBillingEngine()

	tests := []struct {
		name      string
		revenue   CycleRevenue
		usage     CycleUsage
		terms     BillingTerms
		want      BillingResult
		wantErr   bool
		errKind   fweng.Kind
		errDetail string
	}{
		{
			// Charge-only: gross 100000, compute = 100 units * 1 cent = 100,
			// * (1 + 20/100) = 120. net = 100000 - 120 = 99880 (> 0) —
			// a positive net no longer pays out; it routes NoAction.
			name:    "no-action when net is positive (charge-only, no payout)",
			revenue: CycleRevenue{GrossInbound: usd(100000), EventCount: 7},
			usage:   CycleUsage{ComputeUnitSeconds: 100},
			terms:   launchTerms(),
			want: BillingResult{
				SignedNet:          usd(99880),
				RoutingDirective:   RoutingNoAction,
				ComputeCostApplied: usd(120),
			},
		},
		{
			// Charge: tiny gross, heavy usage drives net negative.
			// gross 50, compute = 1000 units * 1 cent = 1000, * 1.20 = 1200.
			// net = 50 - 1200 = -1150 (< 0).
			name:    "charge when net is negative",
			revenue: CycleRevenue{GrossInbound: usd(50)},
			usage:   CycleUsage{ComputeUnitSeconds: 1000},
			terms:   launchTerms(),
			want: BillingResult{
				SignedNet:          usd(-1150),
				RoutingDirective:   RoutingCharge,
				ComputeCostApplied: usd(1200),
			},
		},
		{
			// Zero revenue and zero usage: net 0, NoAction — a normal return, no error.
			name:    "zero net routes no action",
			revenue: CycleRevenue{GrossInbound: usd(0)},
			usage:   CycleUsage{},
			terms:   launchTerms(),
			want: BillingResult{
				SignedNet:          usd(0),
				RoutingDirective:   RoutingNoAction,
				ComputeCostApplied: usd(0),
			},
		},
		{
			name:    "unknown terms is unknown-terms error",
			revenue: CycleRevenue{GrossInbound: usd(100000)},
			usage:   CycleUsage{ComputeUnitSeconds: 100},
			terms: BillingTerms{
				ComputeCost: ComputeCostUnknown,
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
			got, err := e.ComputeNet(fweng.Context{}, tt.revenue, tt.usage, tt.terms)
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
	e := NewBillingEngine()

	// A chargeback reversal halved the gross from 100000 to 50000. The corrected
	// net is computed fresh from the reversal-adjusted revenue; the Manager computes
	// the delta vs PriorSettled (not the Engine's job).
	prior, err := e.ComputeNet(fweng.Context{}, CycleRevenue{GrossInbound: usd(100000)}, CycleUsage{ComputeUnitSeconds: 100}, launchTerms())
	if err != nil {
		t.Fatalf("seeding prior billing: %v", err)
	}

	got, err := e.RecomputeNet(fweng.Context{}, ReBillingInput{
		Revenue:      CycleRevenue{GrossInbound: usd(50000)},
		Usage:        CycleUsage{ComputeUnitSeconds: 100},
		Terms:        launchTerms(),
		PriorSettled: prior,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// gross 50000, compute 100*1*1.20 = 120. net = 50000-120 = 49880 (> 0)
	// — charge-only routes a positive net to NoAction (no payout).
	want := BillingResult{
		SignedNet:          usd(49880),
		RoutingDirective:   RoutingNoAction,
		ComputeCostApplied: usd(120),
	}
	if got != want {
		t.Fatalf("recomputed result = %+v, want %+v", got, want)
	}

	// RecomputeNet honours the same money-safety guard.
	_, err = e.RecomputeNet(fweng.Context{}, ReBillingInput{
		Revenue: CycleRevenue{GrossInbound: usd(50000)},
		Terms:   BillingTerms{ComputeCost: ComputeCostUnknown},
	})
	assertEngineErr(t, err, fweng.InvalidInput, "unknown terms")
}

// TestDeterminism asserts that identical inputs yield identical outputs across
// repeated invocations (the Engine reads no clock/RNG/state).
func TestDeterminism(t *testing.T) {
	e := NewBillingEngine()
	revenue := CycleRevenue{GrossInbound: usd(123456), EventCount: 3}
	usage := CycleUsage{ComputeUnitSeconds: 250, StorageBytesMonths: 10, EgressBytes: 5}
	terms := launchTerms()

	first, err := e.ComputeNet(fweng.Context{}, revenue, usage, terms)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	for i := range 100 {
		got, err := e.ComputeNet(fweng.Context{}, revenue, usage, terms)
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
// hand-computed int64, and cost+net reconcile exactly.
//
// The exactness subject is now the MARKUP, because the revenue-share percent it used to
// exercise is gone (stage 4b2). The same hazard is here — a fractional percent over
// integer minor units — so the test keeps its job: 33% markup is the awkward one, and
// the assertion is the hand-computed integer floor, not a float comparison.
func TestMoneyIsExactInt64(t *testing.T) {
	e := NewBillingEngine()
	// gross 99 cents, 99 compute-unit-seconds at 1 cent = base 99, 33% markup:
	// 99*13300/10000 = 1316700/10000 = 131 (integer floor). net = 99 - 131 = -32.
	got, err := e.ComputeNet(
		fweng.Context{},
		CycleRevenue{GrossInbound: usd(99)},
		CycleUsage{ComputeUnitSeconds: 99},
		BillingTerms{
			ComputeCost:          ComputeCostFlatMarkup,
			ComputeMarkupPercent: 33.0,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ComputeCostApplied.MinorUnits != 131 {
		t.Fatalf("compute cost = %d minor units, want exact int64 131", got.ComputeCostApplied.MinorUnits)
	}
	if got.SignedNet.MinorUnits != -32 {
		t.Fatalf("signed net = %d minor units, want exact int64 -32", got.SignedNet.MinorUnits)
	}
	if got.RoutingDirective != RoutingCharge {
		t.Fatalf("routing = %v, want Charge for a negative net", got.RoutingDirective)
	}
	// Reconciliation: net == gross − cost, exactly, in int64.
	reconciled := int64(99) - got.ComputeCostApplied.MinorUnits
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
