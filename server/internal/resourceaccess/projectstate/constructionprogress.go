package projectstate

// constructionprogress.go implements the App-A §2 weighted-progress formulas as
// pure functions (no I/O, no Temporal). Called at read time by the caller
// (projectManager.GetProject / constructionManager) — never persisted, so progress
// can never drift from the Phases completion record.

// ActivityProgress returns the App-A §2 progress for one activity: the sum of
// Phase.Weight for all Completed phases (0–100). Returns 0 for an activity with
// no phases yet (NotStarted). Pure: no I/O.
func ActivityProgress(status ActivityConstructionStatus) int {
	sum := 0
	for _, pc := range status.Phases {
		if pc.Completed {
			sum += pc.Weight
		}
	}
	return sum
}

// ProjectEarnedValue returns the App-A §2 project-level earned value as a fraction
// in [0, 1]: Σ(E_i × A_i(t)) / Σ E_i, where E_i = effortDays[activityID] and
// A_i(t) = ActivityProgress / 100. Activities not present in effortDays contribute
// E_i = 1.0 (equal weighting default). Returns 0 for empty input or zero total effort.
// Pure: no I/O.
func ProjectEarnedValue(statuses []ActivityConstructionStatus, effortDays map[string]float64) float64 {
	if len(statuses) == 0 {
		return 0.0
	}
	var totalEffort float64
	var earnedEffort float64
	for _, st := range statuses {
		e := 1.0
		if effortDays != nil {
			if v, ok := effortDays[st.ActivityID]; ok {
				e = v
			}
		}
		a := float64(ActivityProgress(st)) / 100.0
		earnedEffort += e * a
		totalEffort += e
	}
	if totalEffort == 0 {
		return 0.0
	}
	return earnedEffort / totalEffort
}
