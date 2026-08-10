package estimation

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
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

// TestComputeEarnedValue_EarnedAndPlannedMonotoneAndSPI is the relocation of the former
// web-layer computeEV test onto the Engine that now owns the math (founder gate 2026-06-28).
// A→B→C chain: A(5) and B(5) integrated, C(10) not — earned should reach 50% (10 of 20
// effort days), planned 100%, SPI positive.
func TestComputeEarnedValue_EarnedAndPlannedMonotoneAndSPI(t *testing.T) {
	al := ActivityList{Activities: []ActivityItem{
		{Name: "A", EffortDays: 5},
		{Name: "B", EffortDays: 5},
		{Name: "C", EffortDays: 10},
	}}
	net := Network{Dependencies: []NetworkDependency{
		{Activity: "B", DependsOn: []string{"A"}},
		{Activity: "C", DependsOn: []string{"B"}},
	}}
	integrated := []string{"A", "B"} // C not done

	ev, err := NewEstimationEngine().ComputeEarnedValue(fweng.Context{}, al, net, integrated, 4, 5)
	if err != nil {
		t.Fatalf("ComputeEarnedValue: %v", err)
	}

	if ev.SPI <= 0 {
		t.Errorf("SPI should be positive, got %v", ev.SPI)
	}
	// earned must be monotone non-decreasing and end at 50% (10 of 20 effort days)
	for i := 1; i < len(ev.Earned); i++ {
		if ev.Earned[i] < ev.Earned[i-1] {
			t.Errorf("earned not monotone at %d", i)
		}
	}
	last := ev.Earned[len(ev.Earned)-1]
	if last < 49 || last > 51 {
		t.Errorf("final earned want ~50%%, got %v", last)
	}
	// planned must also be monotone and reach 100%
	if ev.Planned[len(ev.Planned)-1] < 99 {
		t.Errorf("planned should reach ~100%%, got %v", ev.Planned[len(ev.Planned)-1])
	}
}

// TestComputeEarnedValue_EmptyIsZeroNotError proves an empty activity list is a normal
// domain result (zero curve), never an error.
func TestComputeEarnedValue_EmptyIsZeroNotError(t *testing.T) {
	ev, err := NewEstimationEngine().ComputeEarnedValue(fweng.Context{}, ActivityList{}, Network{}, nil, 0, 0)
	if err != nil {
		t.Fatalf("ComputeEarnedValue empty: %v", err)
	}
	if ev.SPI != 0 {
		t.Errorf("empty SPI want 0, got %v", ev.SPI)
	}
}

// diamond builds a small diamond network for the CPM tests: A(5) → B(5),C(15) → D(5).
// Longest path A→C→D = 25 days; B carries 10 days of total float.
func diamond() (ActivityList, Network) {
	al := ActivityList{Activities: []ActivityItem{
		{Name: "A", EffortDays: 5},
		{Name: "B", EffortDays: 5},
		{Name: "C", EffortDays: 15},
		{Name: "D", EffortDays: 5},
	}}
	net := Network{
		Dependencies: []NetworkDependency{
			{Activity: "B", DependsOn: []string{"A"}},
			{Activity: "C", DependsOn: []string{"A"}},
			{Activity: "D", DependsOn: []string{"B", "C"}},
		},
	}
	return al, net
}

// fbPassNodeCheck asserts one diamond node's forward/backward-pass figures — earliest
// start/finish, total float, and the on-critical-path flag — with the same single
// "NAME: %+v" fatal the inlined checks produced.
func fbPassNodeCheck(t *testing.T, name string, n NetworkNode, wantES, wantEF, wantFloat float64, wantOnCP bool) {
	t.Helper()
	if n.EarliestStart != wantES || n.EarliestFinish != wantEF || n.TotalFloat != wantFloat || n.OnCriticalPath != wantOnCP {
		t.Fatalf("%s: %+v", name, n)
	}
}

func TestComputeNetwork_ForwardBackwardPass(t *testing.T) {
	al, net := diamond()
	sol, err := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	if err != nil {
		t.Fatalf("ComputeNetwork: %v", err)
	}

	if sol.Summary.TotalDurationDays != 25 {
		t.Fatalf("project duration = %v, want 25", sol.Summary.TotalDurationDays)
	}
	if sol.Summary.CriticalPathDays != 25 {
		t.Fatalf("CP days = %v, want 25", sol.Summary.CriticalPathDays)
	}

	fbPassNodeCheck(t, "A", sol.Nodes["A"], 0, 5, 0, true)
	fbPassNodeCheck(t, "C", sol.Nodes["C"], 5, 20, 0, true)
	// B: ES 5, EF 10, latest start = 20 (so D can start at 20), float = 10. Off-CP.
	fbPassNodeCheck(t, "B", sol.Nodes["B"], 5, 10, 10, false)
	d := sol.Nodes["D"]
	if d.EarliestStart != 20 || d.EarliestFinish != 25 || !d.OnCriticalPath {
		t.Fatalf("D: %+v", d)
	}
}

func TestComputeNetwork_BandClassification(t *testing.T) {
	al, net := diamond()
	sol, _ := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)

	// On-CP nodes are critical.
	if sol.Nodes["A"].Band != bandCritical || sol.Nodes["C"].Band != bandCritical {
		t.Fatalf("on-CP nodes not critical: A=%s C=%s", sol.Nodes["A"].Band, sol.Nodes["C"].Band)
	}
	// B has 10 days float: > red (5) and ≤ yellow (25) ⇒ yellow.
	if sol.Nodes["B"].Band != bandYellow {
		t.Fatalf("B band = %s, want yellow (float 10)", sol.Nodes["B"].Band)
	}
}

func TestComputeNetwork_BandPolicyThresholdsTunable(t *testing.T) {
	// The band thresholds are a Strategy on the policy. Verify the boundaries directly.
	p := bandPolicy{RedMaxDays: 5, YellowMaxDays: 25}
	cases := []struct {
		onCP  bool
		float float64
		want  string
	}{
		{true, 0, bandCritical},
		{true, 100, bandCritical}, // on-CP always critical regardless of float
		{false, 0, bandRed},
		{false, 5, bandRed},
		{false, 6, bandYellow},
		{false, 25, bandYellow},
		{false, 26, bandGreen},
	}
	for _, c := range cases {
		if got := p.classify(c.onCP, c.float); got != c.want {
			t.Errorf("classify(onCP=%v, float=%v) = %s, want %s", c.onCP, c.float, got, c.want)
		}
	}
	if !p.nearCritical(false, 5) || p.nearCritical(false, 6) || p.nearCritical(true, 0) {
		t.Fatal("nearCritical boundary wrong")
	}
}

func TestComputeNetwork_MilestoneEventTimeAndRiskExclusion(t *testing.T) {
	al, net := diamond()
	// A milestone fanning in on D (max predecessor EF = 25) and one fanning in on B (10).
	net.Milestones = []NetworkMilestone{
		{Id: "M-END", DependsOn: []string{"D"}},
		{Id: "M-MID", DependsOn: []string{"B"}},
		{Id: "M-START", DependsOn: nil},
	}
	sol, err := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	if err != nil {
		t.Fatalf("ComputeNetwork: %v", err)
	}
	if len(sol.Milestones) != 3 {
		t.Fatalf("milestones = %d, want 3", len(sol.Milestones))
	}
	byID := map[string]NetworkMilestoneSolution{}
	for _, m := range sol.Milestones {
		byID[m.ID] = m
	}
	// DETERMINING-PREDECESSOR on-CP rule: M-END's determining pred is D (EF 25, on-CP) ⇒
	// M-END on-CP. M-MID's determining pred is B (EF 10, off-CP) ⇒ M-MID off-CP.
	if byID["M-END"].EventTime != 25 || !byID["M-END"].OnCriticalPath {
		t.Fatalf("M-END: %+v (want eventTime 25, on-CP via determining pred D)", byID["M-END"])
	}
	if byID["M-MID"].EventTime != 10 || byID["M-MID"].OnCriticalPath {
		t.Fatalf("M-MID: %+v (want eventTime 10, off-CP via determining pred B)", byID["M-MID"])
	}
	// M-START has NO predecessors: the ROOT convention puts the project-start gate on-CP,
	// eventTime 0 (it marks the project origin).
	if byID["M-START"].EventTime != 0 || !byID["M-START"].OnCriticalPath {
		t.Fatalf("M-START: %+v (want eventTime 0, on-CP via root convention)", byID["M-START"])
	}
	// Milestones are NOT activity nodes (excluded from the node set + the CP count).
	if _, isNode := sol.Nodes["M-END"]; isNode {
		t.Fatal("milestone leaked into activity node set")
	}
	if sol.Summary.CriticalPathActivityCount != 3 {
		t.Fatalf("CP activity count = %d, want 3 (milestones excluded)", sol.Summary.CriticalPathActivityCount)
	}
}

// TestComputeNetwork_MilestoneChaining verifies a milestone may dependOn another
// milestone (the N-DOGFOOD → M5 shape): both are zero-duration nodes in the unified CPM
// graph, so the chained milestone's eventTime follows its predecessor milestone, and its
// on-CP follows its determining (milestone) predecessor — regardless of authored order.
func TestComputeNetwork_MilestoneChaining(t *testing.T) {
	al, net := diamond()
	// N-LATE depends on M-END (a milestone). Authored BEFORE M-END to prove order-
	// independence (the dependency-order milestone pass resolves the chain).
	net.Milestones = []NetworkMilestone{
		{Id: "N-LATE", DependsOn: []string{"M-END"}},
		{Id: "M-END", DependsOn: []string{"D"}},
	}
	sol, err := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	if err != nil {
		t.Fatalf("ComputeNetwork: %v", err)
	}
	byID := map[string]NetworkMilestoneSolution{}
	for _, m := range sol.Milestones {
		byID[m.ID] = m
	}
	// M-END: eventTime 25 (= D.EF), determining pred D on-CP ⇒ on-CP.
	if byID["M-END"].EventTime != 25 || !byID["M-END"].OnCriticalPath {
		t.Fatalf("M-END: %+v", byID["M-END"])
	}
	// N-LATE chains off M-END (eventTime 25). Its determining pred is M-END, a milestone
	// at the project frontier (25 == projectDuration) ⇒ POST-TERMINAL override forces it
	// OFF-CP (the N-DOGFOOD → M5 shape: a post-frontier marker chained off a milestone).
	if byID["N-LATE"].EventTime != 25 || byID["N-LATE"].OnCriticalPath {
		t.Fatalf("N-LATE: %+v (want eventTime 25, off-CP via post-terminal override)", byID["N-LATE"])
	}
	// Returned slice preserves authored order (N-LATE first).
	if sol.Milestones[0].ID != "N-LATE" || sol.Milestones[1].ID != "M-END" {
		t.Fatalf("authored order not preserved: %v", []string{sol.Milestones[0].ID, sol.Milestones[1].ID})
	}
}

// TestComputeNetwork_MilestoneDeterminingPredOffCP verifies the determining-predecessor
// rule: M-X fans in on {A (on-CP, EF 5), B (off-CP, EF 10)} ⇒ determining pred is B (the
// max-EF node, EF 10 = eventTime) which is OFF-CP ⇒ M-X off-CP. (A is on-CP but is NOT
// the determining predecessor — it finishes earlier, so it does not gate the event.)
func TestComputeNetwork_MilestoneDeterminingPredOffCP(t *testing.T) {
	al, net := diamond()
	net.Milestones = []NetworkMilestone{
		{Id: "M-X", DependsOn: []string{"A", "B"}},
	}
	sol, _ := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	m := sol.Milestones[0]
	if m.EventTime != 10 {
		t.Fatalf("M-X eventTime = %v, want 10 (determining pred B)", m.EventTime)
	}
	if m.OnCriticalPath {
		t.Fatal("M-X should be OFF-CP: determining pred B is off-CP (A is on-CP but not determining)")
	}
}

// TestComputeNetwork_StartGateMilestone verifies the ROOT convention: a no-predecessor
// start-gate milestone (M0 "SDP Review Approved") is on-CP at eventTime 0 — it marks the
// project origin. No fan-out edge is required under the determining-predecessor rule.
func TestComputeNetwork_StartGateMilestone(t *testing.T) {
	al, net := diamond()
	net.Milestones = []NetworkMilestone{
		{Id: "M0", DependsOn: nil},
	}
	sol, _ := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	m := sol.Milestones[0]
	if m.EventTime != 0 || !m.OnCriticalPath {
		t.Fatalf("M0 start gate: %+v (want eventTime 0, on-CP via root convention)", m)
	}
}

// TestComputeNetwork_PostTerminalMilestoneOffCP verifies the POST-TERMINAL override that
// distinguishes the terminal release milestone (stays on-CP) from a post-v1 marker chained
// off it (forced off-CP) — the M5 vs N-DOGFOOD distinction. M-REL's determining pred is an
// ACTIVITY at the frontier ⇒ on-CP; M-POST chains off M-REL (a milestone at the frontier)
// ⇒ post-terminal ⇒ off-CP, even though its determining pred M-REL is on-CP.
func TestComputeNetwork_PostTerminalMilestoneOffCP(t *testing.T) {
	al, net := diamond()
	net.Milestones = []NetworkMilestone{
		{Id: "M-REL", DependsOn: []string{"D"}},      // det pred D (activity, on-CP, EF 25 = duration)
		{Id: "M-POST", DependsOn: []string{"M-REL"}}, // chained off the release milestone
	}
	sol, _ := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	byID := map[string]NetworkMilestoneSolution{}
	for _, m := range sol.Milestones {
		byID[m.ID] = m
	}
	if !byID["M-REL"].OnCriticalPath {
		t.Fatalf("M-REL should be ON-CP (terminal release, determining pred is an on-CP activity): %+v", byID["M-REL"])
	}
	if byID["M-POST"].EventTime != 25 || byID["M-POST"].OnCriticalPath {
		t.Fatalf("M-POST should be OFF-CP (post-terminal, chained off milestone M-REL): %+v", byID["M-POST"])
	}
}

func TestComputeNetwork_EmptyNetworkIsEmptyResultNotError(t *testing.T) {
	sol, err := NewEstimationEngine().ComputeNetwork(fweng.Context{}, ActivityList{}, Network{})
	if err != nil {
		t.Fatalf("empty network should not error: %v", err)
	}
	if len(sol.Nodes) != 0 || sol.Summary.TotalDurationDays != 0 {
		t.Fatalf("empty network not empty: %+v", sol)
	}
}

func TestComputeNetwork_Deterministic(t *testing.T) {
	al, net := diamond()
	a, _ := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	b, _ := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	if a.Summary != b.Summary {
		t.Fatal("summary not deterministic")
	}
	for id, n := range a.Nodes {
		if n != b.Nodes[id] {
			t.Fatalf("node %s not deterministic: %+v vs %+v", id, n, b.Nodes[id])
		}
	}
}

func TestComputeNetwork_SummaryRollups(t *testing.T) {
	al, net := diamond()
	sol, _ := NewEstimationEngine().ComputeNetwork(fweng.Context{}, al, net)
	// 3 on-CP (A,C,D), max float 10 (B), 0 near-critical (B's 10 > red threshold 5).
	if sol.Summary.CriticalPathActivityCount != 3 {
		t.Fatalf("CP count = %d, want 3", sol.Summary.CriticalPathActivityCount)
	}
	if sol.Summary.MaxFloat != 10 {
		t.Fatalf("max float = %v, want 10", sol.Summary.MaxFloat)
	}
	if sol.Summary.NearCriticalCount != 0 {
		t.Fatalf("near-critical = %d, want 0", sol.Summary.NearCriticalCount)
	}
}

// --- Derivation tests: DerivePlan (derive_test.go) ---

func TestWorkerClassFor(t *testing.T) {
	cases := []struct {
		prefix, want string
	}{
		{"C", "junior-developer"},
		{"U", "junior-developer"},
		{"R", "senior-developer"},
		{"I", "senior-developer"},
		{"G", "ui-designer"},
	}
	for _, c := range cases {
		if got := workerClassFor(c.prefix); got != c.want {
			t.Errorf("workerClassFor(%q) = %q, want %q", c.prefix, got, c.want)
		}
	}
}

// The always-emit noncoding inventory has FIXED worker classes per Löwy ch. 9's three
// distinct quality roles — test engineer (builds harnesses), software tester (runs
// system testing), QA engineer (process). They must never collapse into one class.
func TestWorkerClassForNoncodingInventory(t *testing.T) {
	cases := map[string]string{
		"N-STP":   "test-engineer",
		"N-STH":   "test-engineer",
		"N-PERF":  "test-engineer",
		"N-RTH":   "senior-developer",
		"N-SMOKE": "senior-developer",
		"N-QA":    "qa-engineer",
		"N-IT":    "software-tester",
	}
	for name, want := range cases {
		if got := noncodingInventoryClass(name); got != want {
			t.Errorf("noncodingInventoryClass(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDefaultEffortFor(t *testing.T) {
	cases := []struct {
		kind string
		want float64
	}{
		{"manager", 25},
		{"engine", 15},
		{"resourceAccess", 10},
		{"client", 25},
		{"utility", 10},
		{"resource", 10},
	}
	for _, c := range cases {
		if got := defaultEffortFor(c.kind); got != c.want {
			t.Errorf("defaultEffortFor(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// Every default must satisfy App C §4.4: a 5-day quantum, no god activity (>35d).
func TestDefaultEffortObeysQuantumAndCap(t *testing.T) {
	for _, kind := range []string{"manager", "engine", "resourceAccess", "client", "utility", "resource"} {
		e := defaultEffortFor(kind)
		if e <= 0 || e > 35 {
			t.Errorf("defaultEffortFor(%q) = %v, out of (0,35]", kind, e)
		}
		if int(e)%5 != 0 {
			t.Errorf("defaultEffortFor(%q) = %v, breaks the 5-day quantum", kind, e)
		}
	}
}

func TestDefaultRiskFor(t *testing.T) {
	cases := []struct {
		effort float64
		want   int64
	}{
		{5, 2}, {10, 2}, {15, 3}, {20, 3}, {25, 5}, {30, 5}, {35, 5},
	}
	for _, c := range cases {
		if got := defaultRiskFor(c.effort); got != c.want {
			t.Errorf("defaultRiskFor(%v) = %d, want %d", c.effort, got, c.want)
		}
	}
}

// Risk buckets are Fibonacci (1,2,3,5,8,13). A non-Fibonacci default would corrupt
// every downstream activity-risk roll-up.
func TestDefaultRiskIsFibonacci(t *testing.T) {
	fib := map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}
	for e := 5.0; e <= 35; e += 5 {
		if r := defaultRiskFor(e); !fib[r] {
			t.Errorf("defaultRiskFor(%v) = %d, not a Fibonacci bucket", e, r)
		}
	}
}

// sampleSystem is a miniature but complete System: one handwritten manager, one engine,
// one resourceAccess, one GENERATED client that carries a UI surface, one vendor
// resource, one owned resource, and one utility. It exercises every emission rule.
func sampleSystem() SystemView {
	return SystemView{
		Components: []SystemComponent{
			{ID: "order-manager", Name: "OrderManager", Kind: "manager", ConstructionProfile: "handwritten"},
			{ID: "pricing-engine", Name: "PricingEngine", Kind: "engine", ConstructionProfile: "handwritten"},
			{ID: "order-access", Name: "OrderAccess", Kind: "resourceAccess", ConstructionProfile: "handwritten"},
			{ID: "web-client", Name: "WebClient", Kind: "client", ConstructionProfile: "generated", UiSurface: true},
			{ID: "stripe", Name: "Stripe", Kind: "resource", Provisioning: "vendor"},
			{ID: "order-db", Name: "OrderDB", Kind: "resource", Provisioning: "owned"},
			{ID: "logging", Name: "Logging", Kind: "utility", ConstructionProfile: "handwritten"},
		},
		CoreUseCaseIDs: []string{"UC1", "UC2"},
	}
}

func names(acts []DerivedActivity) map[string]DerivedActivity {
	m := make(map[string]DerivedActivity, len(acts))
	for _, a := range acts {
		m[a.Name] = a
	}
	return m
}

func TestDeriveActivitiesEmitsOneCodingActivityPerHandwrittenComponent(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"C-order-manager", "C-pricing-engine", "C-order-access", "C-logging"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing derived coding activity %q", want)
		}
	}
	if a := got["C-order-manager"]; a.ComponentID != "order-manager" || !a.Coding || a.EffortDays != 25 {
		t.Errorf("C-order-manager = %+v, want componentID order-manager, coding, 25d", a)
	}
}

// The platform GENERATES the whole transport tier (REST handlers, typed clients, MCP
// tools, OAS) from the committed contracts. Planning work the generator does is the
// defect this rule exists to prevent — and the live committed list violates it three
// times (C-CW, C-CM, C-CS).
func TestDeriveActivitiesEmitsNoCodingActivityForGeneratedComponents(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["C-web-client"]; ok {
		t.Error("emitted a coding activity for a generated-transport client; the generator does that work")
	}
}

// R-* is one per VENDOR resource. Owned stores (the schema/deploy work rides additive
// noncoding) get none — the live list gets this wrong in both directions.
func TestDeriveActivitiesEmitsProvisioningOnlyForVendorResources(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["R-stripe"]; !ok {
		t.Error("missing R-stripe for the vendor resource")
	}
	if _, ok := got["R-order-db"]; ok {
		t.Error("emitted R-order-db for an OWNED store; its work rides additive noncoding")
	}
	if a := got["R-stripe"]; a.Coding || a.WorkerClass != "senior-developer" {
		t.Errorf("R-stripe = %+v, want noncoding senior-developer", a)
	}
}

// One U-SPA per Manager: a Client calls Managers, a use case IS a Manager, and the
// verbs-as-tools doctrine makes a manager's generated tool surface its widget set. Plus
// the always-emit scaffold, and G-SPA sequenced before the UI work.
func TestDeriveActivitiesEmitsOneSPAActivityPerManagerPlusScaffold(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	if _, ok := got["U-SPA-order-manager"]; !ok {
		t.Error("missing U-SPA-order-manager")
	}
	if _, ok := got["U-SPA-S"]; !ok {
		t.Error("missing the always-emit U-SPA-S scaffold")
	}
	if _, ok := got["G-SPA"]; !ok {
		t.Error("missing G-SPA for a system with a UI surface")
	}
	if a := got["G-SPA"]; a.WorkerClass != "ui-designer" {
		t.Errorf("G-SPA worker class = %q, want ui-designer", a.WorkerClass)
	}
}

// uiSurface is a SEPARATE axis from constructionProfile: web-client is generated
// (no C-*) AND carries a UI surface (SPA work is real). Collapsing them loses the SPA.
func TestDeriveActivitiesNoSPAWorkWithoutAUISurface(t *testing.T) {
	sys := sampleSystem()
	for i := range sys.Components {
		sys.Components[i].UiSurface = false
	}
	got := names(deriveActivities(sys))
	for _, unwanted := range []string{"U-SPA-S", "G-SPA", "U-SPA-order-manager"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("emitted %q for a system with no UI surface", unwanted)
		}
	}
}

func TestDeriveActivitiesEmitsOneIntegrationActivityPerCoreUseCase(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"I-UC1", "I-UC2"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %q", want)
		}
	}
}

func TestDeriveActivitiesAlwaysEmitsTheTestingInventory(t *testing.T) {
	got := names(deriveActivities(sampleSystem()))
	for _, want := range []string{"N-STP", "N-STH", "N-RTH", "N-SMOKE", "N-QA", "N-PERF", "N-IT"} {
		a, ok := got[want]
		if !ok {
			t.Errorf("missing always-emit %q", want)
			continue
		}
		if a.WorkerClass != noncodingInventoryClass(want) {
			t.Errorf("%s worker class = %q, want %q", want, a.WorkerClass, noncodingInventoryClass(want))
		}
	}
}

// Purity: identical input must give a byte-identical, stably ordered result. Map
// iteration order leaking into the output would make every downstream CPM solve
// nondeterministic.
func TestDeriveActivitiesIsDeterministicAndSorted(t *testing.T) {
	first := deriveActivities(sampleSystem())
	for i := 0; i < 20; i++ {
		next := deriveActivities(sampleSystem())
		if len(next) != len(first) {
			t.Fatalf("length varies across runs: %d vs %d", len(next), len(first))
		}
		for j := range first {
			if next[j].Name != first[j].Name {
				t.Fatalf("order varies across runs at %d: %q vs %q", j, next[j].Name, first[j].Name)
			}
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Name >= first[i].Name {
			t.Fatalf("not sorted ascending by name at %d: %q then %q", i, first[i-1].Name, first[i].Name)
		}
	}
}

// Every derived activity must be plan-legal on its face (App C 4.4 + the fixed roster).
func TestDeriveActivitiesAllObeyTheEstimationRules(t *testing.T) {
	roster := map[string]bool{
		"system-architect": true, "product-manager": true, "project-manager": true,
		"senior-developer": true, "junior-developer": true, "ui-designer": true,
		"ux-reviewer": true, "qa-engineer": true, "test-engineer": true, "software-tester": true,
	}
	fib := map[int64]bool{1: true, 2: true, 3: true, 5: true, 8: true, 13: true}
	for _, a := range deriveActivities(sampleSystem()) {
		if int(a.EffortDays)%5 != 0 || a.EffortDays <= 0 || a.EffortDays > 35 {
			t.Errorf("%s effort %v breaks the quantum/cap rule", a.Name, a.EffortDays)
		}
		if !roster[a.WorkerClass] {
			t.Errorf("%s worker class %q is not on the fixed roster", a.Name, a.WorkerClass)
		}
		if !fib[a.RiskBucket] {
			t.Errorf("%s risk bucket %d is not Fibonacci", a.Name, a.RiskBucket)
		}
		if !a.Derived {
			t.Errorf("%s is not flagged Derived", a.Name)
		}
	}
}

// --- Derivation tests: dependency edges and milestones (derive_edges_test.go) ---

// Löwy Fig 11-4 → Fig 11-5: Client A depends on Manager A AND Security; Manager A also
// depends on Security. The Client→Security edge is INHERITED through Manager A and must
// be eliminated. This is the canonical worked example in ch. 11 §1.2.
func TestTransitiveReductionEliminatesInheritedDependencies(t *testing.T) {
	in := map[string][]string{
		"client-a":  {"manager-a", "security"},
		"manager-a": {"security"},
	}
	got := transitiveReduction(in)
	want := map[string][]string{
		"client-a":  {"manager-a"},
		"manager-a": {"security"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("transitiveReduction = %v, want %v", got, want)
	}
}

// A longer inherited chain: A→B→C→D plus the direct A→D and A→C shortcuts. Only the
// immediate edge survives at each hop.
func TestTransitiveReductionEliminatesMultiHopInheritance(t *testing.T) {
	in := map[string][]string{
		"a": {"b", "c", "d"},
		"b": {"c"},
		"c": {"d"},
	}
	got := transitiveReduction(in)
	want := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"d"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("transitiveReduction = %v, want %v", got, want)
	}
}

// A diamond has no redundant edge — both paths are load-bearing and must survive.
func TestTransitiveReductionKeepsDiamondEdges(t *testing.T) {
	in := map[string][]string{
		"top":   {"left", "right"},
		"left":  {"bottom"},
		"right": {"bottom"},
	}
	got := transitiveReduction(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("transitiveReduction dropped a load-bearing diamond edge: %v", got)
	}
}

// Determinism: predecessor lists must come back sorted, never in map order.
func TestTransitiveReductionIsSorted(t *testing.T) {
	in := map[string][]string{"x": {"c", "a", "b"}}
	got := transitiveReduction(in)
	if !sort.StringsAreSorted(got["x"]) {
		t.Errorf("predecessors not sorted: %v", got["x"])
	}
}

// A cycle is bad input, but the reduction must TERMINATE rather than hang or overflow —
// a malformed committed System must never wedge the derivation.
func TestTransitiveReductionTerminatesOnCycles(_ *testing.T) {
	in := map[string][]string{"a": {"b"}, "b": {"a"}}
	_ = transitiveReduction(in) // must simply return
}

func edgeSystem() SystemView {
	sys := sampleSystem()
	sys.Relationships = []SystemRelationship{
		{From: "order-manager", To: "pricing-engine"},
		{From: "order-manager", To: "order-access"},
		{From: "pricing-engine", To: "order-access"},
		{From: "order-access", To: "order-db"},
		// order-access -> logging exists so C-order-access appears as a dependent at
		// all; without it the dangling-edge test below is vacuous (its loop body never
		// runs, because the only edge out of order-access is dropped before it becomes
		// a key). Adding it changes no other expectation: logging is a handwritten
		// utility with its own C-* activity, and it introduces no new inherited path.
		{From: "order-access", To: "logging"},
	}
	return sys
}

func depsByActivity(deps []NetworkDependency) map[string][]string {
	m := make(map[string][]string, len(deps))
	for _, d := range deps {
		m[d.Activity] = d.DependsOn
	}
	return m
}

// Architecture edges become activity edges, reduced. order-manager→order-access is
// inherited via pricing-engine and must not survive.
func TestDeriveDependenciesMapsRelationshipsAndReduces(t *testing.T) {
	got := depsByActivity(deriveDependencies(edgeSystem(), deriveActivities(edgeSystem())))
	if !reflect.DeepEqual(got["C-order-manager"], []string{"C-pricing-engine"}) {
		t.Errorf("C-order-manager dependsOn = %v, want [C-pricing-engine] after reduction", got["C-order-manager"])
	}
	if !reflect.DeepEqual(got["C-pricing-engine"], []string{"C-order-access"}) {
		t.Errorf("C-pricing-engine dependsOn = %v, want [C-order-access]", got["C-pricing-engine"])
	}
}

// An edge pointing at a component with NO derived activity (an owned store, a generated
// client) must be dropped, not emitted as a dangling reference into the CPM solve.
// The ComponentID contract this whole file depends on: deriveDependencies indexes
// activities by ComponentID to rewrite architecture edges as activity edges, so a
// componentless activity that carried a stray ComponentID would silently capture edges
// meant for a real component. Asserted here, at the consumer, rather than in Task 3.
func TestComponentIDIsSetOnlyOnComponentBoundActivities(t *testing.T) {
	for _, a := range deriveActivities(edgeSystem()) {
		componentBound := strings.HasPrefix(a.Name, "C-") || strings.HasPrefix(a.Name, "R-") ||
			(strings.HasPrefix(a.Name, "U-SPA-") && a.Name != "U-SPA-S")
		switch {
		case componentBound && a.ComponentID == "":
			t.Errorf("%s is component-bound but carries no ComponentID", a.Name)
		case !componentBound && a.ComponentID != "":
			t.Errorf("%s is componentless but carries ComponentID %q; it would capture edges meant for that component",
				a.Name, a.ComponentID)
		}
	}
}

// An edge pointing at a component with NO derived activity (an owned store, a generated
// client) must be dropped, not emitted as a dangling reference into the CPM solve — a
// dangling predecessor injects a zero-duration phantom node and silently distorts the
// critical path.
//
// Two things this test has to get right, both of which an earlier version got wrong: it
// must not be VACUOUS (C-order-access has to actually appear as a dependent, or the loop
// never runs), and it must assert on RESOLVABILITY rather than on specific names — a
// dropped !okTo guard emits the EMPTY STRING as a predecessor, not a readable id like
// "C-order-db", so a literal comparison sails straight past the regression.
func TestDeriveDependenciesDropsEdgesToComponentsWithNoActivity(t *testing.T) {
	sys := edgeSystem()
	acts := deriveActivities(sys)
	known := make(map[string]bool, len(acts))
	for _, a := range acts {
		known[a.Name] = true
	}
	deps := deriveDependencies(sys, acts)
	got := depsByActivity(deps)

	// Vacuity guard: if order-access ever stops appearing as a dependent, this test
	// proves nothing and must fail loudly rather than pass silently.
	if _, ok := got["C-order-access"]; !ok {
		t.Fatal("fixture went vacuous: C-order-access has no dependency row, so a leaked dangling edge could not be observed")
	}

	for _, d := range deps {
		for _, p := range d.DependsOn {
			switch {
			case p == "":
				t.Errorf("activity %q has an EMPTY predecessor — an edge to a component with no derived activity leaked through", d.Activity)
			case !known[p]:
				t.Errorf("activity %q depends on %q, which is not a derived activity; dangling predecessors inject zero-duration phantom nodes into the CPM solve", d.Activity, p)
			}
		}
	}

	// And specifically: the owned store must never have produced an activity edge.
	for _, p := range got["C-order-access"] {
		if strings.Contains(p, "order-db") {
			t.Errorf("edge to the owned store order-db survived: %v", got["C-order-access"])
		}
	}
}

// Fixed pattern edges: the UI design gates SPA construction, the scaffold gates the
// per-manager screens, the test plan gates the harness, and every integration gates the
// terminal system-testing activity.
func TestDeriveDependenciesEmitsFixedPatternEdges(t *testing.T) {
	sys := edgeSystem()
	got := depsByActivity(deriveDependencies(sys, deriveActivities(sys)))
	assertContains := func(activity, want string) {
		t.Helper()
		for _, p := range got[activity] {
			if p == want {
				return
			}
		}
		t.Errorf("%s dependsOn %v, missing %q", activity, got[activity], want)
	}
	assertContains("U-SPA-order-manager", "G-SPA")
	assertContains("U-SPA-order-manager", "U-SPA-S")
	assertContains("N-STH", "N-STP")
	assertContains("N-IT", "I-UC1")
	assertContains("N-IT", "I-UC2")
}

// M0 is the SDP-review milestone: Löwy makes it an explicit forced dependency so that
// no construction activity starts before the review. M1-M3 are layer-completion
// milestones, M4 is use-cases-demonstrable.
func TestDeriveMilestones(t *testing.T) {
	sys := edgeSystem()
	ms := deriveMilestones(sys, deriveActivities(sys))
	byID := make(map[string]NetworkMilestone, len(ms))
	for _, m := range ms {
		byID[m.Id] = m
	}
	for _, want := range []string{"M0", "M1", "M2", "M3", "M4"} {
		if _, ok := byID[want]; !ok {
			t.Errorf("missing derived milestone %q", want)
		}
	}
	if got := byID["M4"].DependsOn; !reflect.DeepEqual(got, []string{"I-UC1", "I-UC2"}) {
		t.Errorf("M4 dependsOn = %v, want the integration set", got)
	}
	if got := byID["M3"].DependsOn; !reflect.DeepEqual(got, []string{"C-order-manager"}) {
		t.Errorf("M3 (managers complete) dependsOn = %v, want [C-order-manager]", got)
	}
	// M5 (v1 Production Live) depends entirely on additive noncoding, so it is NOT
	// derived — it arrives as an additive delta.
	if _, ok := byID["M5"]; ok {
		t.Error("M5 must not be derived; it depends entirely on additive noncoding")
	}
}

// --- Derivation tests: authored delta application (derive_deltas_test.go) ---

func planFor(t *testing.T, deltas ActivityListDeltas) DerivedPlan {
	t.Helper()
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), deltas)
	if err != nil {
		t.Fatalf("DerivePlan returned error: %v", err)
	}
	return plan
}

func activityNamed(plan DerivedPlan, name string) (DerivedActivity, bool) {
	for _, a := range plan.Activities {
		if a.Name == name {
			return a, true
		}
	}
	return DerivedActivity{}, false
}

func TestDerivePlanWithNoDeltasReturnsTheBaseline(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{})
	if len(plan.Activities) == 0 {
		t.Fatal("DerivePlan returned no activities for a populated System")
	}
	if _, ok := activityNamed(plan, "C-order-manager"); !ok {
		t.Error("baseline missing C-order-manager")
	}
	if len(plan.Dependencies) == 0 {
		t.Error("baseline produced no dependency edges")
	}
}

func TestOverrideReplacesEffortAndRisk(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Overrides: []ActivityOverride{{
		Activity: "C-order-manager", EffortDays: ptrFloat(35), RiskBucket: ptrInt(8),
		Justification: "orchestrates five downstream contracts; the band midpoint is optimistic",
	}}})
	a, ok := activityNamed(plan, "C-order-manager")
	if !ok {
		t.Fatal("C-order-manager missing after override")
	}
	if a.EffortDays != 35 || a.RiskBucket != 8 {
		t.Errorf("override not applied: %+v", a)
	}
	if !a.Derived {
		t.Error("an overridden activity is still a DERIVED activity")
	}
}

// An override naming no derived activity is the zombie failure mode: the live committed
// list carries C-HE, C-WIA and R-WIT against components that do not exist. Loud failure.
func TestOverrideOfUnknownActivityIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Overrides: []ActivityOverride{{Activity: "C-hand-off-engine", Justification: "x"}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an override of an unknown activity, got %v", err)
	}
}

func TestOverrideWithoutJustificationIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Overrides: []ActivityOverride{{Activity: "C-order-manager", EffortDays: ptrFloat(35)}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an unjustified override, got %v", err)
	}
}

func TestOverrideBreakingTheQuantumIsRejected(t *testing.T) {
	for _, bad := range []float64{7, 11, 40} {
		_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
			Overrides: []ActivityOverride{{
				Activity: "C-order-manager", EffortDays: ptrFloat(bad), Justification: "j",
			}},
		})
		if err == nil {
			t.Errorf("override effort %v should be rejected (quantum 5, cap 35)", bad)
		}
	}
}

func TestAdditiveActivityIsAppended(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Additive: []AdditiveActivity{{
		Name: "N-SCHEMA", Title: "Schema design for owned stores",
		EffortDays: 10, RiskBucket: 2, WorkerClass: "system-architect",
		DependsOn:     []string{"C-order-access"},
		Justification: "owned stores carry schema work no component activity covers",
	}}})
	a, ok := activityNamed(plan, "N-SCHEMA")
	if !ok {
		t.Fatal("additive activity not appended")
	}
	if a.Derived {
		t.Error("an additive activity must NOT be flagged Derived")
	}
	var found bool
	for _, d := range plan.Dependencies {
		if d.Activity == "N-SCHEMA" && len(d.DependsOn) == 1 && d.DependsOn[0] == "C-order-access" {
			found = true
		}
	}
	if !found {
		t.Error("the additive activity's own incident edge was not emitted")
	}
}

// C2: an additive carrying a componentId is a covert per-component exclusion or
// replacement channel. It is exactly how C-HE and C-WIA would come back.
func TestAdditiveWithComponentIDIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "junior-developer", Justification: "j",
			ComponentID: ptrString("order-manager"),
		}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive carrying a componentId, got %v", err)
	}
}

// An additive may not shadow a derived activity — that is an exclusion in disguise.
func TestAdditiveCollidingWithADerivedNameIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "C-order-manager", Title: "shadow", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "junior-developer", Justification: "j",
		}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive shadowing a derived activity, got %v", err)
	}
}

// C3: an additive declares its OWN incident edges only. Pointing at a nonexistent
// activity would inject a dangling node into the CPM solve.
func TestAdditiveEdgeToUnknownActivityIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "junior-developer", Justification: "j",
			DependsOn: []string{"C-does-not-exist"},
		}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive edge to an unknown activity, got %v", err)
	}
}

func TestAdditiveWithOffRosterWorkerClassIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "Platform-DevOps-Engineer", Justification: "j",
		}},
	})
	if err == nil {
		t.Fatal("an off-roster worker class must be rejected; it would silently ride default token rates")
	}
}

// C4: M5 "v1 Production Live" depends entirely on additive noncoding, so it cannot
// derive — it is authored as an additive milestone.
func TestAdditiveMilestoneIsAppended(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{
		Additive: []AdditiveActivity{{
			Name: "N-DEP", Title: "Production deployment", EffortDays: 10, RiskBucket: 3,
			WorkerClass: "senior-developer", Justification: "deployment is componentless project work",
		}},
		AdditiveMilestones: []AdditiveMilestone{{
			Id: "M5", DependsOn: []string{"N-DEP", "N-IT"},
			Justification: "v1 production live gates on deployment plus the terminal system-testing gate",
		}},
	})
	var found bool
	for _, m := range plan.Milestones {
		if m.Id == "M5" {
			found = true
		}
	}
	if !found {
		t.Error("additive milestone M5 was not appended")
	}
}

func TestAdditiveMilestoneShadowingADerivedOneIsRejected(t *testing.T) {
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{Id: "M0", Justification: "j"}},
	})
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("want ContractMisuse for an additive milestone shadowing a derived one, got %v", err)
	}
}

// An empty System is a normal DOMAIN result (a project read before its architecture is
// committed), never an error.
func TestDerivePlanOnEmptySystemIsAnEmptyPlanNotAnError(t *testing.T) {
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, SystemView{}, ActivityListDeltas{})
	if err != nil {
		t.Fatalf("empty System must be a domain result, got error %v", err)
	}
	if len(plan.Activities) != 0 {
		t.Errorf("empty System produced %d activities", len(plan.Activities))
	}
}

// The deferred two-pass validation exists so two additives may legally depend on each
// other: appendAdditives indexes EVERY additive before validateAdditiveEdges checks any
// edge. Inlining the edge check into the append loop would reject this valid input — and
// that regression is invisible to every other test in this file, because their edge
// targets are all derived baseline activities that exist before additive processing.
// The reference is FORWARD on purpose: N-FIRST depends on N-SECOND, which is appended
// after it.
func TestAdditivesMayDependOnEachOther(t *testing.T) {
	plan := planFor(t, ActivityListDeltas{Additive: []AdditiveActivity{
		{
			Name: "N-FIRST", Title: "first", EffortDays: 5, RiskBucket: 2,
			WorkerClass: "senior-developer", DependsOn: []string{"N-SECOND"},
			Justification: "depends on a sibling additive declared after it",
		},
		{
			Name: "N-SECOND", Title: "second", EffortDays: 5, RiskBucket: 2,
			WorkerClass:   "senior-developer",
			Justification: "the forward-referenced sibling",
		},
	}})
	if _, ok := activityNamed(plan, "N-FIRST"); !ok {
		t.Error("N-FIRST missing from the plan")
	}
	if _, ok := activityNamed(plan, "N-SECOND"); !ok {
		t.Error("N-SECOND missing from the plan")
	}
	var found bool
	for _, d := range plan.Dependencies {
		if d.Activity == "N-FIRST" && len(d.DependsOn) == 1 && d.DependsOn[0] == "N-SECOND" {
			found = true
		}
	}
	if !found {
		t.Error("N-FIRST -> N-SECOND edge missing; the two-pass ordering has regressed to single-pass")
	}
}

// mustReject asserts the deltas are refused with a ContractMisuse. Every caller varies
// exactly ONE field away from an otherwise-legal delta, so the named guard is the only
// one eligible — a rejection test that passed because an EARLIER guard fired would not
// be testing what its name claims.
func mustReject(t *testing.T, what string, deltas ActivityListDeltas) {
	t.Helper()
	_, err := NewEstimationEngine().DerivePlan(fweng.Context{}, edgeSystem(), deltas)
	var fe *fweng.Error
	if !errors.As(err, &fe) || fe.Kind != fweng.ContractMisuse {
		t.Fatalf("%s: want ContractMisuse, got %v", what, err)
	}
}

// legalAdditive passes every guard, so a caller can break exactly one field and know
// which guard fired.
func legalAdditive() AdditiveActivity {
	return AdditiveActivity{
		Name: "N-X", Title: "x", EffortDays: 5, RiskBucket: 2,
		WorkerClass: "senior-developer", Justification: "j",
	}
}

func TestOverrideWithNonFibonacciRiskIsRejected(t *testing.T) {
	mustReject(t, "override with risk bucket 4", ActivityListDeltas{Overrides: []ActivityOverride{{
		Activity: "C-order-manager", RiskBucket: ptrInt(4), Justification: "j",
	}}})
}

func TestAdditiveWithoutJustificationIsRejected(t *testing.T) {
	a := legalAdditive()
	a.Justification = ""
	mustReject(t, "additive without justification", ActivityListDeltas{Additive: []AdditiveActivity{a}})
}

func TestAdditiveWithIllegalEffortIsRejected(t *testing.T) {
	for _, bad := range []float64{7, 40} {
		a := legalAdditive()
		a.EffortDays = bad
		mustReject(t, "additive with off-quantum or oversized effort",
			ActivityListDeltas{Additive: []AdditiveActivity{a}})
	}
}

func TestAdditiveWithNonFibonacciRiskIsRejected(t *testing.T) {
	a := legalAdditive()
	a.RiskBucket = 4
	mustReject(t, "additive with risk bucket 4", ActivityListDeltas{Additive: []AdditiveActivity{a}})
}

func TestAdditiveMilestoneWithoutJustificationIsRejected(t *testing.T) {
	mustReject(t, "additive milestone without justification", ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{Id: "M9"}},
	})
}

func TestAdditiveMilestoneWithDanglingDependencyIsRejected(t *testing.T) {
	mustReject(t, "additive milestone with dangling dependsOn", ActivityListDeltas{
		AdditiveMilestones: []AdditiveMilestone{{
			Id: "M9", DependsOn: []string{"N-DOES-NOT-EXIST"}, Justification: "j",
		}},
	})
}

func ptrFloat(f float64) *float64 { return &f }
func ptrInt(i int64) *int64       { return &i }
func ptrString(s string) *string  { return &s }

// --- Derivation tests: parity against the live committed System (derive_parity_test.go) ---

// loadSystemFixture reads the frozen slim view of the live committed System (37
// components) with the Task-7 typed attributes stamped on.
func loadSystemFixture(t *testing.T) SystemView {
	t.Helper()
	raw, err := os.ReadFile("testdata/system_view.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var sv SystemView
	if err := json.Unmarshal(raw, &sv); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(sv.Components) != 37 {
		t.Fatalf("fixture has %d components, want the live 37", len(sv.Components))
	}
	return sv
}

func parityPlan(t *testing.T) DerivedPlan {
	t.Helper()
	plan, err := NewEstimationEngine().DerivePlan(fweng.Context{}, loadSystemFixture(t), ActivityListDeltas{})
	if err != nil {
		t.Fatalf("DerivePlan over the live System: %v", err)
	}
	return plan
}

func parityNames(plan DerivedPlan) map[string]bool {
	m := make(map[string]bool, len(plan.Activities))
	for _, a := range plan.Activities {
		m[a.Name] = true
	}
	return m
}

// Correction 1 — the three zombie activities (C-HE / C-WIA / R-WIT in the committed
// plan, all Done+Integrated against components that no longer exist) must not appear.
//
// HONEST SCOPE: this test cannot prove the deriver "excludes" them, because there is no
// exclusion branch to exercise — deriveActivities emits C-*/R-* names ONLY by iterating
// system.Components, so a component that does not exist can never produce an activity.
// Corrections 2 and 3 below DO have real guard branches (constructionProfile ==
// "generated", provisioning != "vendor") and their tests genuinely exercise them.
//
// What this test is actually worth: (a) a fixture-staleness tripwire, failing if a
// future re-extraction reintroduces one of these three components, and (b) a structural
// tripwire against any regression that sourced activity names from somewhere other than
// system.Components. The real evidence for Correction 1 is the derived-vs-committed
// diff in the task report, not this assertion in isolation.
func TestParityDropsTheZombieActivities(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, zombie := range []string{"C-hand-off-engine", "C-work-item-access", "R-work-item-tracker"} {
		if got[zombie] {
			t.Errorf("derived the zombie activity %q", zombie)
		}
	}
}

// Correction 2: the generated transport tier gets no coding activity. C-CW, C-CM and
// C-CS are committed today in violation of standing doctrine.
func TestParityDropsGeneratedClientCodingActivities(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, c := range []string{"C-web-client", "C-mcp-client", "C-scheduler-client"} {
		if got[c] {
			t.Errorf("derived %q for a generated-transport client", c)
		}
	}
}

// Correction 3: R-* only for vendor resources. The four owned stores get none.
func TestParityEmitsProvisioningOnlyForVendorResources(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, want := range []string{"R-github", "R-merchant-gateway", "R-construction-pipeline-runtime", "R-operated-runtime"} {
		if !got[want] {
			t.Errorf("missing vendor provisioning activity %q", want)
		}
	}
	for _, unwanted := range []string{"R-project-git-repo", "R-operated-system-state", "R-billing-state", "R-usage-log"} {
		if got[unwanted] {
			t.Errorf("derived %q for an OWNED store; its work rides additive noncoding", unwanted)
		}
	}
}

// Correction 4: one U-SPA per manager. Five managers, five activities, plus scaffold.
func TestParityEmitsOneSPAActivityPerManager(t *testing.T) {
	got := parityNames(parityPlan(t))
	for _, m := range []string{"system-design-manager", "project-design-manager", "construction-manager", "operations-manager", "billing-manager"} {
		if !got["U-SPA-"+m] {
			t.Errorf("missing U-SPA-%s", m)
		}
	}
	if !got["U-SPA-S"] || !got["G-SPA"] {
		t.Error("missing the always-emit scaffold / UI-design activities")
	}
}

// Every code-layer component that is not generated transport must be covered exactly
// once. This is the invariant that ACT-COMPONENT-COVERAGE used to enforce as a gate and
// that derivation now makes true by construction.
func TestParityCoversEveryHandwrittenCodeComponentExactlyOnce(t *testing.T) {
	sys := loadSystemFixture(t)
	plan := parityPlan(t)
	count := map[string]int{}
	for _, a := range plan.Activities {
		if a.Coding && a.ComponentID != "" && len(a.Name) > 2 && a.Name[:2] == "C-" {
			count[a.ComponentID]++
		}
	}
	for _, c := range sys.Components {
		if !isCodeLayer(c.Kind) || c.ConstructionProfile == "generated" {
			continue
		}
		if count[c.ID] != 1 {
			t.Errorf("component %s has %d coding activities, want exactly 1", c.ID, count[c.ID])
		}
	}
}

// No dependency edge may name an activity that does not exist — a dangling predecessor
// silently corrupts the CPM solve (it contributes a zero-duration phantom node).
func TestParityHasNoDanglingDependencyEdges(t *testing.T) {
	plan := parityPlan(t)
	known := parityNames(plan)
	for _, m := range plan.Milestones {
		known[m.Id] = true
	}
	for _, d := range plan.Dependencies {
		if !known[d.Activity] {
			t.Errorf("dependency row for unknown activity %q", d.Activity)
		}
		for _, p := range d.DependsOn {
			if !known[p] {
				t.Errorf("activity %q depends on unknown %q", d.Activity, p)
			}
		}
	}
}

// The derived plan must feed the EXISTING CPM solve without adaptation — that is the
// point of deriving into the same shapes ComputeNetwork already consumes.
func TestParityPlanSolvesThroughComputeNetwork(t *testing.T) {
	plan := parityPlan(t)
	items := make([]ActivityItem, 0, len(plan.Activities))
	for _, a := range plan.Activities {
		items = append(items, ActivityItem{Name: a.Name, EffortDays: a.EffortDays})
	}
	sol, err := NewEstimationEngine().ComputeNetwork(fweng.Context{},
		ActivityList{Activities: items},
		Network{Dependencies: plan.Dependencies, Milestones: plan.Milestones})
	if err != nil {
		t.Fatalf("ComputeNetwork over the derived plan: %v", err)
	}
	if len(sol.Nodes) == 0 {
		t.Fatal("ComputeNetwork produced no nodes for the derived plan")
	}
	if sol.Summary.TotalDurationDays <= 0 {
		t.Errorf("derived plan solves to a non-positive duration %v", sol.Summary.TotalDurationDays)
	}
	if sol.Summary.CriticalPathActivityCount == 0 {
		t.Error("derived plan has no critical path; the edge derivation produced a disconnected graph")
	}
}
