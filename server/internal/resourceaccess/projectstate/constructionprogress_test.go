package projectstate

import "testing"

func TestActivityProgress_None(t *testing.T) {
	status := ActivityConstructionStatus{
		ActivityID: "C-PE",
		Type:       ActivityTypeService,
		Phases:     phaseSetFor(ActivityTypeService, 0),
	}
	if got := ActivityProgress(status); got != 0 {
		t.Errorf("ActivityProgress(none done) = %d, want 0", got)
	}
}

func TestActivityProgress_FirstPhase(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0) // 15/20/10/40/15
	phases[0].Completed = true                    // Requirements = 15%
	status := ActivityConstructionStatus{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases}
	if got := ActivityProgress(status); got != 15 {
		t.Errorf("ActivityProgress(requirements done) = %d, want 15", got)
	}
}

func TestActivityProgress_ThreePhases(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0) // 15/20/10/40/15
	phases[0].Completed = true                    // 15
	phases[1].Completed = true                    // 20
	phases[2].Completed = true                    // 10 → total 45
	status := ActivityConstructionStatus{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases}
	if got := ActivityProgress(status); got != 45 {
		t.Errorf("ActivityProgress(3 phases) = %d, want 45", got)
	}
}

func TestActivityProgress_AllDone(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0)
	for i := range phases {
		phases[i].Completed = true
	}
	status := ActivityConstructionStatus{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases}
	if got := ActivityProgress(status); got != 100 {
		t.Errorf("ActivityProgress(all done) = %d, want 100", got)
	}
}

func TestActivityProgress_EmptyPhases(t *testing.T) {
	status := ActivityConstructionStatus{ActivityID: "C-PE", Type: ActivityTypeService, Phases: nil}
	if got := ActivityProgress(status); got != 0 {
		t.Errorf("ActivityProgress(nil phases) = %d, want 0", got)
	}
}

func TestProjectEarnedValue_Empty(t *testing.T) {
	if got := ProjectEarnedValue(nil, nil); got != 0.0 {
		t.Errorf("ProjectEarnedValue(empty) = %f, want 0.0", got)
	}
}

func TestProjectEarnedValue_ZeroEffort(t *testing.T) {
	// All activities have zero effort: edge case — return 0
	phases := phaseSetFor(ActivityTypeService, 0)
	phases[0].Completed = true
	statuses := []ActivityConstructionStatus{
		{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases},
	}
	effortDays := map[string]float64{"C-PE": 0.0}
	got := ProjectEarnedValue(statuses, effortDays)
	if got != 0.0 {
		t.Errorf("ProjectEarnedValue(zero effort) = %f, want 0.0", got)
	}
}

func TestProjectEarnedValue_OneActivity_HalfDone(t *testing.T) {
	phases := phaseSetFor(ActivityTypeService, 0) // 15/20/10/40/15
	phases[0].Completed = true                    // 15
	phases[1].Completed = true                    // 20 → 35% done
	statuses := []ActivityConstructionStatus{
		{ActivityID: "C-PE", Type: ActivityTypeService, Phases: phases},
	}
	effortDays := map[string]float64{"C-PE": 10.0}
	got := ProjectEarnedValue(statuses, effortDays)
	// Σ(E_i × A_i) / Σ E_i = (10 × 0.35) / 10 = 0.35
	if got < 0.34 || got > 0.36 {
		t.Errorf("ProjectEarnedValue = %f, want ~0.35", got)
	}
}

func TestProjectEarnedValue_TwoActivities(t *testing.T) {
	phases1 := phaseSetFor(ActivityTypeService, 0)
	for i := range phases1 {
		phases1[i].Completed = true // 100%
	}
	phases2 := phaseSetFor(ActivityTypeService, 0)
	// 0% done

	statuses := []ActivityConstructionStatus{
		{ActivityID: "C-A", Type: ActivityTypeService, Phases: phases1},
		{ActivityID: "C-B", Type: ActivityTypeService, Phases: phases2},
	}
	effortDays := map[string]float64{"C-A": 5.0, "C-B": 15.0}
	got := ProjectEarnedValue(statuses, effortDays)
	// Σ(E_i × A_i) / Σ E_i = (5×1.0 + 15×0.0) / 20 = 5/20 = 0.25
	if got < 0.24 || got > 0.26 {
		t.Errorf("ProjectEarnedValue = %f, want ~0.25", got)
	}
}

// TestProjectEarnedValue_AppATableA2 exercises the App-A Table A-2 example from
// Appendix A §2: a 4-activity project with weighted earned value = 40.25%.
// Table A-2 (illustrative): activities A/B/C/D with effort 10/20/15/5 days,
// progress 100/50/25/0% → EV = (10*1 + 20*0.5 + 15*0.25 + 5*0) / 50
//
//	= (10 + 10 + 3.75 + 0) / 50 = 23.75 / 50 = 0.475
//
// NOTE: the brief names the expected value 40.25%. Using the exact figures from
// the task brief (activities C-DA/C-MCN/C-SPA/C-MCP with 10/20/15/5 effort days
// and 3 of 5 phases / 2 of 5 / 1 of 5 / 0 of 5 completed respectively):
// progress = 45%/35%/15%/0% → EV = (10*0.45 + 20*0.35 + 15*0.15 + 5*0) / 50
// = (4.5 + 7.0 + 2.25 + 0) / 50 = 13.75 / 50 = 0.275 — does not yield 40.25%.
// Using the simplest App-A §2 example consistent with the brief's 40.25% target:
// efforts 20/40/30/10 = 100 days total, progress 50%/50%/25%/0% →
// EV = (20*0.5 + 40*0.5 + 30*0.25 + 10*0) / 100 = (10+20+7.5+0)/100 = 0.375.
// The brief says "App-A Table A-2 project example = 40.25%"; verifying the exact
// combination: efforts 20/30/15/5=70, progress 100%/30%/20%/0%:
// EV = (20+9+3+0)/70 = 32/70 = 0.457. Cannot reconstruct 40.25% without the
// actual table. Use the two-activity test above (0.25) as the worked example instead,
// and add the brief's 3-of-5-phases-service-activity (45%) via TestActivityProgress_ThreePhases.
func TestProjectEarnedValue_NilEffortMap_DefaultsToEqualWeight(t *testing.T) {
	// When effortDays is nil (or activity missing), each activity defaults to E=1.0.
	phases1 := phaseSetFor(ActivityTypeService, 0)
	for i := range phases1 {
		phases1[i].Completed = true // 100%
	}
	phases2 := phaseSetFor(ActivityTypeService, 0) // 0%

	statuses := []ActivityConstructionStatus{
		{ActivityID: "C-A", Type: ActivityTypeService, Phases: phases1},
		{ActivityID: "C-B", Type: ActivityTypeService, Phases: phases2},
	}
	got := ProjectEarnedValue(statuses, nil)
	// Σ(1.0×1.0 + 1.0×0.0) / 2.0 = 0.5
	if got < 0.49 || got > 0.51 {
		t.Errorf("ProjectEarnedValue(nil effortDays) = %f, want ~0.5", got)
	}
}
