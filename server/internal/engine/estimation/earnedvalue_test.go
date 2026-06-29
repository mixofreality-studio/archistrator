package estimation

import (
	"testing"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

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
