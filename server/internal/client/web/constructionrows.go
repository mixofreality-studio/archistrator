package web

import (
	"github.com/davidmarne/archistrator/server/internal/manager/project"
	"github.com/davidmarne/archistrator/server/internal/resourceaccess/projectstate"
)

// constructionrows.go projects the per-activity construction head-state + the project-level
// tracking snapshot onto the camelCase wire DTO (sibling to gitRowsFromState), and computes
// the EV curves server-side via a CPM forward pass. Honest-empty: a row absent from the map
// is simply not emitted; nil progress yields a nil DTO (omitted on the wire).

type producedArtifactDTO struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Source   string `json:"source"`
	Produced bool   `json:"produced"`
	Note     string `json:"note"`
}

type constructionRowDTO struct {
	ActivityID string                `json:"activityId"`
	Kind       string                `json:"kind"`
	Status     string                `json:"status"`
	Phase      string                `json:"phase"`
	Produced   []producedArtifactDTO `json:"produced,omitempty"`
}

type evCurvesDTO struct {
	Weeks   []int     `json:"weeks"`
	Earned  []float64 `json:"earned"`
	Planned []float64 `json:"planned"`
	SPI     float64   `json:"spi"`
}

type constructionProgressDTO struct {
	Week           int         `json:"week"`
	TotalWeeks     int         `json:"totalWeeks"`
	HandOffModel   string      `json:"handOffModel"`
	SupervisionCap int         `json:"supervisionCap"`
	EV             evCurvesDTO `json:"ev"`
}

type evActivity struct {
	Name       string
	EffortDays float64
}
type evDep struct {
	Activity  string
	DependsOn []string
}

// computeEV runs a CPM forward pass to schedule each activity's finish week, then builds the
// cumulative planned curve (all activities, by scheduled finish) and earned curve (integrated
// activities, by scheduled finish), both as % of total effort. SPI = earned/planned at the
// current (last) week.
func computeEV(acts []evActivity, deps []evDep, integrated map[string]bool, totalWeeks, calDaysPerWeek int) evCurvesDTO {
	if calDaysPerWeek <= 0 {
		calDaysPerWeek = 5
	}
	effort := map[string]float64{}
	depsOf := map[string][]string{}
	var total float64
	for _, a := range acts {
		effort[a.Name] = a.EffortDays
		total += a.EffortDays
	}
	for _, d := range deps {
		depsOf[d.Activity] = d.DependsOn
	}
	// memoized earliest-finish (in days) via longest path
	finish := map[string]float64{}
	var ef func(string) float64
	ef = func(n string) float64 {
		if v, ok := finish[n]; ok {
			return v
		}
		finish[n] = 0 // cycle guard
		start := 0.0
		for _, p := range depsOf[n] {
			if pf := ef(p); pf > start {
				start = pf
			}
		}
		v := start + effort[n]
		finish[n] = v
		return v
	}
	for _, a := range acts {
		ef(a.Name)
	}
	if totalWeeks <= 0 {
		totalWeeks = 1
		for _, a := range acts {
			if w := int(finish[a.Name])/calDaysPerWeek + 1; w > totalWeeks {
				totalWeeks = w
			}
		}
	}
	weeks := make([]int, totalWeeks+1)
	planned := make([]float64, totalWeeks+1)
	earned := make([]float64, totalWeeks+1)
	for w := 0; w <= totalWeeks; w++ {
		weeks[w] = w
		var p, e float64
		for _, a := range acts {
			fw := int(finish[a.Name]) / calDaysPerWeek
			if fw <= w {
				p += effort[a.Name]
				if integrated[a.Name] {
					e += effort[a.Name]
				}
			}
		}
		if total > 0 {
			planned[w] = p / total * 100
			earned[w] = e / total * 100
		}
	}
	spi := 0.0
	if planned[totalWeeks] > 0 {
		spi = earned[totalWeeks] / planned[totalWeeks]
	}
	return evCurvesDTO{Weeks: weeks, Earned: earned, Planned: planned, SPI: spi}
}

// constructionFromState projects the head-state map + progress scalars onto the wire shape and
// computes EV. Returns (nil, nil) when there is no construction head-state (honest-empty).
func constructionFromState(
	rows map[string]projectstate.ActivityConstructionStatus,
	progress *projectstate.ConstructionProgress,
	acts []projectstate.ActivityItem,
	deps []projectstate.NetworkDependency,
	calDaysPerWeek int,
) (map[string]constructionRowDTO, *constructionProgressDTO) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make(map[string]constructionRowDTO, len(rows))
	integrated := map[string]bool{}
	for id, r := range rows {
		var prod []producedArtifactDTO
		for _, p := range r.Produced {
			prod = append(prod, producedArtifactDTO{Kind: p.Kind, Title: p.Title, Source: p.Source, Produced: p.Produced, Note: p.Note})
		}
		out[id] = constructionRowDTO{
			ActivityID: r.ActivityID,
			Kind:       r.Kind.String(),
			Status:     r.BuildStatus.String(),
			Phase:      r.Phase.String(),
			Produced:   prod,
		}
		if r.BuildStatus == projectstate.BuildIntegrated {
			integrated[id] = true
		}
	}
	var prog *constructionProgressDTO
	if progress != nil {
		evA := make([]evActivity, 0, len(acts))
		for _, a := range acts {
			evA = append(evA, evActivity{Name: a.Name, EffortDays: a.EffortDays})
		}
		evD := make([]evDep, 0, len(deps))
		for _, d := range deps {
			evD = append(evD, evDep{Activity: d.Activity, DependsOn: d.DependsOn})
		}
		prog = &constructionProgressDTO{
			Week:           progress.Week,
			TotalWeeks:     progress.TotalWeeks,
			HandOffModel:   progress.HandOffModel,
			SupervisionCap: progress.SupervisionCap,
			EV:             computeEV(evA, evD, integrated, progress.TotalWeeks, calDaysPerWeek),
		}
	}
	return out, prog
}

// activityItemsFromState extracts the []ActivityItem from the ActivityList slot model.
// Returns nil when the slot is absent or untyped.
func activityItemsFromState(s project.ProjectState) []projectstate.ActivityItem {
	for _, slot := range s.Slots {
		if slot.Kind == projectstate.KindActivityList && slot.Model != nil {
			if al, ok := slot.Model.(*projectstate.ActivityList); ok {
				return al.Activities
			}
		}
	}
	return nil
}

// networkDepsFromState extracts the []NetworkDependency from the Network slot model.
// Returns nil when the slot is absent or untyped.
func networkDepsFromState(s project.ProjectState) []projectstate.NetworkDependency {
	for _, slot := range s.Slots {
		if slot.Kind == projectstate.KindNetwork && slot.Model != nil {
			if n, ok := slot.Model.(*projectstate.Network); ok {
				return n.Dependencies
			}
		}
	}
	return nil
}

// calendarDaysFromState extracts the CalendarDaysPerWeek from the PlanningAssumptions
// slot model. Returns 5 (the standard 5-day workweek) when the slot is absent.
func calendarDaysFromState(s project.ProjectState) int {
	for _, slot := range s.Slots {
		if slot.Kind == projectstate.KindPlanningAssumptions && slot.Model != nil {
			if pa, ok := slot.Model.(*projectstate.PlanningAssumptions); ok && pa.CalendarDaysPerWeek > 0 {
				return int(pa.CalendarDaysPerWeek)
			}
		}
	}
	return 5
}
