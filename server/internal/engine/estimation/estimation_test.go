package estimation

import (
	"errors"
	"math"
	"reflect"
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

// usd builds a USD Money rate.
func usd(minor int64) Money {
	return Money{MinorUnits: minor, Currency: "USD"}
}

const eps = 1e-9

func approx(a, b float64) bool { return math.Abs(a-b) <= 1e-6 }

// serialChainOption is a 3-activity serial chain a1→a2→a3 (5+10+5 effort) on a 5 d/wk
// calendar (stretch 1.0), ample staffing. Because the chain is serial, EVERY activity is
// on the resource-critical path — a clean hand-computable base case.
func serialChainOption() ProjectOption {
	return ProjectOption{
		OptionId: "normal",
		Network: ActivityNetwork{
			Activities: []OptionActivity{
				{ActivityId: "a1", EffortDays: 5, WorkerClass: "senior"},
				{ActivityId: "a2", EffortDays: 10, WorkerClass: "junior"},
				{ActivityId: "a3", EffortDays: 5, WorkerClass: "senior"},
			},
			Dependencies: []NetworkDependency{
				{Activity: "a2", DependsOn: []string{"a1"}},
				{Activity: "a3", DependsOn: []string{"a2"}},
			},
		},
		WorkerMix: WorkerMix{
			ClassRates:  map[string]Money{"senior": usd(2250), "junior": usd(1350)},
			StaffingCap: 3,
		},
		CalendarDaysPerWeek: 5,
	}
}

func TestEstimateForOption_SerialChain_AllCritical(t *testing.T) {
	eng := NewEstimationEngine()
	got, err := eng.EstimateForOption(fweng.Context{}, serialChainOption())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Duration = serial chain length 5+10+5 = 20 (stretch 1.0).
	if got.DurationDays != 20 {
		t.Errorf("DurationDays = %v, want 20", got.DurationDays)
	}
	// Direct cost = 5*2250 + 10*1350 + 5*2250 = 11250 + 13500 + 11250 = 36000.
	if got.DirectCost != usd(36000) {
		t.Errorf("DirectCost = %+v, want %+v", got.DirectCost, usd(36000))
	}
	// No indirect rate on this option ⇒ BuildCost == DirectCost.
	if got.BuildCost != usd(36000) {
		t.Errorf("BuildCost = %+v, want %+v", got.BuildCost, usd(36000))
	}
	// All 3 activities are on the serial critical path ⇒ all zero-float ⇒
	// criticality = (4*3)/(4*3) = 1.0; activity risk (uniform zero float) = 1.0.
	if !approx(got.Risk.CriticalityRisk, 1.0) {
		t.Errorf("CriticalityRisk = %v, want 1.0", got.Risk.CriticalityRisk)
	}
	if !approx(got.Risk.ActivityRisk, 1.0) {
		t.Errorf("ActivityRisk = %v, want 1.0", got.Risk.ActivityRisk)
	}
}

// TestCriticalityRisk_WeightedBands hand-computes the book's weighted criticality risk
// (ch.10 §3.4) over a network with a genuine float spread. Topology:
//
//	c1 → c2 → c3   (critical chain, 10+10+10 = 30 days, zero float)
//	     s1        (a side activity depending on c1, effort 2, generous float)
//
// With ample staffing: c1,c2,c3 are critical (band critical, weight 4). s1 starts at 10
// (after c1) with effort 2, and the project ends at 30, so its late-finish is 30 ⇒ total
// float = 30 - (10+2) = 18 days ⇒ band yellow (weight 2).
//
//	criticality = (4*3 + 2*1) / (4*4) = 14/16 = 0.875
//	activity risk = 1 - (0+0+0+18)/(4*18) = 1 - 18/72 = 0.75
func TestCriticalityRisk_WeightedBands(t *testing.T) {
	opt := ProjectOption{
		OptionId: "spread",
		Network: ActivityNetwork{
			Activities: []OptionActivity{
				{ActivityId: "c1", EffortDays: 10, WorkerClass: "w"},
				{ActivityId: "c2", EffortDays: 10, WorkerClass: "w"},
				{ActivityId: "c3", EffortDays: 10, WorkerClass: "w"},
				{ActivityId: "s1", EffortDays: 2, WorkerClass: "w"},
			},
			Dependencies: []NetworkDependency{
				{Activity: "c2", DependsOn: []string{"c1"}},
				{Activity: "c3", DependsOn: []string{"c2"}},
				{Activity: "s1", DependsOn: []string{"c1"}},
			},
		},
		WorkerMix:           WorkerMix{ClassRates: map[string]Money{"w": usd(1000)}, StaffingCap: 4},
		CalendarDaysPerWeek: 5,
	}
	got, err := NewEstimationEngine().EstimateForOption(fweng.Context{}, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !approx(got.Risk.CriticalityRisk, 0.875) {
		t.Errorf("CriticalityRisk = %v, want 0.875", got.Risk.CriticalityRisk)
	}
	if !approx(got.Risk.ActivityRisk, 0.75) {
		t.Errorf("ActivityRisk = %v, want 0.75", got.Risk.ActivityRisk)
	}
	if got.DurationDays != 30 {
		t.Errorf("DurationDays = %v, want 30", got.DurationDays)
	}
}

// parallelBaseNetwork is a diamond with a long and a short parallel branch that JOIN:
//
//	a (20) ┐
//	       ├→ join (10)
//	b (10) ┘
//
// With cap == 2 (normal): a and b run in parallel; a is critical (0 float), b has 10 days
// of float, join is critical ⇒ duration 30, some slack in the network.
// With cap == 1 (subcritical): a and b serialize; b's float is consumed and it is pushed
// onto the critical path, extending the join ⇒ duration 40, floats collapse — the trap.
func parallelBaseNetwork() ActivityNetwork {
	return ActivityNetwork{
		Activities: []OptionActivity{
			{ActivityId: "a", EffortDays: 20, WorkerClass: "w"},
			{ActivityId: "b", EffortDays: 10, WorkerClass: "w"},
			{ActivityId: "join", EffortDays: 10, WorkerClass: "w"},
		},
		Dependencies: []NetworkDependency{
			{Activity: "join", DependsOn: []string{"a", "b"}},
		},
	}
}

func estimate(t *testing.T, opt ProjectOption) ConstructionEstimate {
	t.Helper()
	got, err := NewEstimationEngine().EstimateForOption(fweng.Context{}, opt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return got
}

// TestSubcritical_LongerCostlierRiskier is the book's counterintuitive result (ch.11
// §3.3): the SAME network with FEWER resources (cap 1 vs 2) is LONGER, RISKIER, and —
// via the indirect cost that accrues over the longer duration — COSTLIER (F4).
func TestSubcritical_LongerCostlierRiskier(t *testing.T) {
	rates := map[string]Money{"w": usd(1000)}
	indirect := usd(500) // $5/day overhead

	normal := ProjectOption{
		OptionId:            "normal",
		Network:             parallelBaseNetwork(),
		WorkerMix:           WorkerMix{ClassRates: rates, StaffingCap: 2},
		CalendarDaysPerWeek: 5,
		IndirectDailyRate:   indirect,
	}
	subcritical := normal
	subcritical.OptionId = "subcritical"
	subcritical.WorkerMix = WorkerMix{ClassRates: rates, StaffingCap: 1}

	n := estimate(t, normal)
	s := estimate(t, subcritical)

	if !(s.DurationDays > n.DurationDays) {
		t.Errorf("subcritical duration %v must exceed normal %v", s.DurationDays, n.DurationDays)
	}
	if !(s.Risk.Composite > n.Risk.Composite) {
		t.Errorf("subcritical risk %v must exceed normal %v", s.Risk.Composite, n.Risk.Composite)
	}
	if s.BuildCost.MinorUnits <= n.BuildCost.MinorUnits {
		t.Errorf("subcritical total cost %v must exceed normal %v (indirect over longer duration)",
			s.BuildCost.MinorUnits, n.BuildCost.MinorUnits)
	}
	// Direct cost is the SAME (same total effort, same rates) — the whole cost delta is
	// indirect (ch.9 §5). This is the point of the option.
	if s.DirectCost != n.DirectCost {
		t.Errorf("direct cost should be unchanged: subcritical %+v vs normal %+v", s.DirectCost, n.DirectCost)
	}
}

// TestDecompression_DropsRiskTowardTippingPoint: adding a tail buffer to the normal
// solution widens float uniformly, dropping criticality risk toward ~0.5 WITHOUT reducing
// staff (ch.10 §5). Direct cost unchanged; duration + indirect rise slightly (F7).
func TestDecompression_DropsRiskTowardTippingPoint(t *testing.T) {
	rates := map[string]Money{"w": usd(1000)}
	normal := ProjectOption{
		OptionId:            "normal",
		Network:             parallelBaseNetwork(),
		WorkerMix:           WorkerMix{ClassRates: rates, StaffingCap: 2},
		CalendarDaysPerWeek: 5,
		IndirectDailyRate:   usd(500),
	}
	decompressed := normal
	decompressed.OptionId = "decompressed"
	decompressed.BufferDays = 20 // widen float by 20 days

	n := estimate(t, normal)
	d := estimate(t, decompressed)

	if !(d.Risk.CriticalityRisk < n.Risk.CriticalityRisk) {
		t.Errorf("decompressed criticality %v must be below normal %v", d.Risk.CriticalityRisk, n.Risk.CriticalityRisk)
	}
	// The buffer pushes it toward (not past) the tipping point: strictly below normal and
	// no lower than the 0.25 weighted floor.
	if d.Risk.CriticalityRisk < 0.25-eps {
		t.Errorf("decompressed criticality %v fell below the 0.25 weighted floor", d.Risk.CriticalityRisk)
	}
	if !(d.DurationDays > n.DurationDays) {
		t.Errorf("decompressed duration %v must exceed normal %v (buffer tail)", d.DurationDays, n.DurationDays)
	}
	if d.DirectCost != n.DirectCost {
		t.Errorf("decompression must not change direct cost (staffing unchanged): %+v vs %+v", d.DirectCost, n.DirectCost)
	}
}

// TestCompression_HigherCapShortens: a higher staffing cap lets a cap-throttled network
// parallelize (F5). Compression from cap alone can never go below the unconstrained
// critical path (the parallel floor) — the remaining shortening is a top-resources
// earmark. Here cap 1 (throttled, 40d) → cap 2 (parallel floor, 30d).
func TestCompression_HigherCapShortens(t *testing.T) {
	rates := map[string]Money{"w": usd(1000)}
	throttled := ProjectOption{
		OptionId:            "normal",
		Network:             parallelBaseNetwork(),
		WorkerMix:           WorkerMix{ClassRates: rates, StaffingCap: 1},
		CalendarDaysPerWeek: 5,
	}
	compressed := throttled
	compressed.OptionId = "compressed"
	compressed.WorkerMix = WorkerMix{ClassRates: rates, StaffingCap: 2}

	base := estimate(t, throttled)
	comp := estimate(t, compressed)
	if !(comp.DurationDays < base.DurationDays) {
		t.Errorf("compressed duration %v must be below the throttled base %v", comp.DurationDays, base.DurationDays)
	}
	if comp.DurationDays != 30 {
		t.Errorf("compressed duration %v, want 30 (unconstrained parallel floor)", comp.DurationDays)
	}
}

// TestCompression_TopResources: the CriticalSpeedup lever (F5e) speeds up the critical
// path — the compressed option is SHORTER, RISKIER (off-critical float shrinks), and
// COSTLIER (convex rate premium on the sped-up critical activities) than normal, exactly
// the book's compressed profile (ch.9 §2/§3).
func TestCompression_TopResources(t *testing.T) {
	rates := map[string]Money{"w": usd(1000)}
	normal := ProjectOption{
		OptionId:            "normal",
		Network:             parallelBaseNetwork(),
		WorkerMix:           WorkerMix{ClassRates: rates, StaffingCap: 3},
		CalendarDaysPerWeek: 5,
	}
	compressed := normal
	compressed.OptionId = "compressed"
	compressed.CriticalSpeedup = 1.4

	n := estimate(t, normal)
	c := estimate(t, compressed)

	if !(c.DurationDays < n.DurationDays) {
		t.Errorf("compressed duration %v must be below normal %v", c.DurationDays, n.DurationDays)
	}
	if !(c.Risk.Composite > n.Risk.Composite) {
		t.Errorf("compressed risk %v must exceed normal %v (critical speedup shrinks float)", c.Risk.Composite, n.Risk.Composite)
	}
	if c.BuildCost.MinorUnits <= n.BuildCost.MinorUnits {
		t.Errorf("compressed cost %v must exceed normal %v (convex top-resource premium)", c.BuildCost.MinorUnits, n.BuildCost.MinorUnits)
	}
	// Compression must stay within the book's 30% cap on this network.
	comp := (n.DurationDays - c.DurationDays) / n.DurationDays
	if comp > 0.30+1e-9 {
		t.Errorf("compression %.1f%% exceeds the 30%% cap", comp*100)
	}
}

// TestActivityRisk_Edges covers the divide-by-zero / uniform-float edges the book notes.
func TestActivityRisk_Edges(t *testing.T) {
	// Empty set ⇒ 0 risk.
	if r := activityRiskOf(nil); r != 0 {
		t.Errorf("activityRiskOf(nil) = %v, want 0", r)
	}
	// Uniform ZERO float (all critical) ⇒ maximal 1.0, not NaN.
	all := []leveledActivity{{onCP: true, totalFloat: 0}, {onCP: true, totalFloat: 0}}
	if r := activityRiskOf(all); !approx(r, 1.0) {
		t.Errorf("activityRiskOf(all-critical) = %v, want 1.0", r)
	}
	// Uniform NONZERO float ⇒ 1 - (N*F)/(N*F) = 0.
	uni := []leveledActivity{{totalFloat: 30}, {totalFloat: 30}}
	if r := activityRiskOf(uni); !approx(r, 0.0) {
		t.Errorf("activityRiskOf(uniform-float) = %v, want 0.0", r)
	}
}

// TestIndirectCost_LongerDurationCostsMore: with a fixed direct cost, a longer duration
// (via buffer) increases indirect and hence total (F6). Verifies the arithmetic exactly.
func TestIndirectCost_Arithmetic(t *testing.T) {
	opt := serialChainOption()       // duration 20, direct 36000
	opt.IndirectDailyRate = usd(100) // $1/day
	got := estimate(t, opt)
	if got.IndirectCost != usd(2000) { // 20 days * 100
		t.Errorf("IndirectCost = %+v, want %+v", got.IndirectCost, usd(2000))
	}
	if got.BuildCost != usd(38000) { // 36000 + 2000
		t.Errorf("BuildCost = %+v, want %+v", got.BuildCost, usd(38000))
	}
}

// TestIndirectCost_CurrencyMismatch is a ContractMisuse (the Manager mis-assembled).
func TestIndirectCost_CurrencyMismatch(t *testing.T) {
	opt := serialChainOption()
	opt.IndirectDailyRate = Money{MinorUnits: 100, Currency: "EUR"}
	_, err := NewEstimationEngine().EstimateForOption(fweng.Context{}, opt)
	assertKind(t, err, fweng.ContractMisuse)
}

func TestEstimateForOption_ContractMisuse(t *testing.T) {
	eng := NewEstimationEngine()
	cases := []struct {
		name string
		opt  ProjectOption
	}{
		{"empty network", ProjectOption{OptionId: "e", Network: ActivityNetwork{}, WorkerMix: WorkerMix{ClassRates: map[string]Money{}}}},
		{"negative effort", ProjectOption{OptionId: "n", Network: ActivityNetwork{Activities: []OptionActivity{{ActivityId: "a", EffortDays: -1, WorkerClass: "w"}}}, WorkerMix: WorkerMix{ClassRates: map[string]Money{"w": usd(1)}}}},
		{"unknown class", ProjectOption{OptionId: "u", Network: ActivityNetwork{Activities: []OptionActivity{{ActivityId: "a", EffortDays: 5, WorkerClass: "ghost"}}}, WorkerMix: WorkerMix{ClassRates: map[string]Money{"w": usd(1)}}}},
		{"mixed currency", ProjectOption{OptionId: "m", Network: ActivityNetwork{Activities: []OptionActivity{
			{ActivityId: "a", EffortDays: 5, WorkerClass: "usd"},
			{ActivityId: "b", EffortDays: 5, WorkerClass: "eur"},
		}}, WorkerMix: WorkerMix{ClassRates: map[string]Money{"usd": {MinorUnits: 1, Currency: "USD"}, "eur": {MinorUnits: 1, Currency: "EUR"}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eng.EstimateForOption(fweng.Context{}, tc.opt)
			assertKind(t, err, fweng.ContractMisuse)
		})
	}
}

func assertKind(t *testing.T, err error, want fweng.Kind) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error of kind %v, got nil", want)
	}
	var fe *fweng.Error
	if !errors.As(err, &fe) {
		t.Fatalf("expected *fweng.Error, got %T: %v", err, err)
	}
	if fe.Kind != want {
		t.Fatalf("expected kind %v, got %v (%s)", want, fe.Kind, fe.Detail)
	}
	if fe.Retryable {
		t.Errorf("engine errors must never be retryable")
	}
}

// TestDeterminism asserts the pure-function contract: identical input twice → byte-
// identical output (replay safety).
func TestDeterminism(t *testing.T) {
	eng := NewEstimationEngine()
	opt := serialChainOption()
	first, err1 := eng.EstimateForOption(fweng.Context{}, opt)
	second, err2 := eng.EstimateForOption(fweng.Context{}, opt)
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v / %v", err1, err2)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("non-deterministic output:\n first=%+v\nsecond=%+v", first, second)
	}
}
