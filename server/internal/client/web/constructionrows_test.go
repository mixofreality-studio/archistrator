package web

import "testing"

func TestComputeEV_EarnedAndPlannedMonotoneAndSPI(t *testing.T) {
	acts := []evActivity{
		{Name: "A", EffortDays: 5},
		{Name: "B", EffortDays: 5},
		{Name: "C", EffortDays: 10},
	}
	deps := []evDep{{Activity: "B", DependsOn: []string{"A"}}, {Activity: "C", DependsOn: []string{"B"}}}
	integrated := map[string]bool{"A": true, "B": true} // C not done
	ev := computeEV(acts, deps, integrated, 4, 5)

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
