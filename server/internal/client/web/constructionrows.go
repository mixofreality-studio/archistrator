package web

import (
	"encoding/json"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
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
	// FailureReason / FailureDetail surface WHY a terminally-failed activity
	// (status=="failed") is no longer pending, so the Interventions tab can explain it.
	// Empty for non-failed activities.
	FailureReason string `json:"failureReason,omitempty"`
	FailureDetail string `json:"failureDetail,omitempty"`
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
	rows map[string]project.ActivityConstructionStatus,
	progress *project.ConstructionProgress,
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
		row := constructionRowDTO{
			ActivityID: r.ActivityID,
			Kind:       project.ActivityTypeName(r.Kind),
			Status:     project.ActivityBuildStatusName(r.BuildStatus),
			Phase:      project.ActivityConstructionPhaseName(r.Phase),
			Produced:   prod,
		}
		// Surface the terminal-failure cause so the console can explain a failed node
		// (the bounded-wait / autonomous-retry fix: a cancelled/failed run no longer
		// shows as pending forever).
		if r.Phase == project.ActivityConstructionFailed || r.BuildStatus == project.BuildFailed {
			row.FailureReason = project.FailureReasonName(r.FailureReason)
			row.FailureDetail = r.FailureDetail
		}
		out[id] = row
		if r.BuildStatus == project.BuildIntegrated {
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
			Week:           int(progress.Week),
			TotalWeeks:     int(progress.TotalWeeks),
			HandOffModel:   progress.HandOffModel,
			SupervisionCap: int(progress.SupervisionCap),
			EV:             computeEV(evA, evD, integrated, int(progress.TotalWeeks), calDaysPerWeek),
		}
	}
	return out, prog
}

// slotModelRaw returns the opaque model JSON for the slot whose kind matches the
// given projectstate ArtifactKind's canonical wire name, or nil when the slot is
// absent/empty. The projectManager carries each slot's typed model OPAQUELY (a
// {kind, raw-json} envelope), so the webClient decodes the raw JSON into the canonical
// projectstate model here — the read-time enrichment seam (mirrors the manager's own
// computeNetworkAtRead, which casts the same slots internally).
func slotModelRaw(s project.ProjectState, kind projectstate.ArtifactKind) []byte {
	wire := kind.WireName()
	for _, slot := range s.Slots {
		if slot.Kind == wire && slot.Model.Model != nil {
			return *slot.Model.Model
		}
	}
	return nil
}

// activityItemsFromState extracts the []ActivityItem from the ActivityList slot model.
// Returns nil when the slot is absent or empty.
func activityItemsFromState(s project.ProjectState) []projectstate.ActivityItem {
	raw := slotModelRaw(s, projectstate.KindActivityList)
	if raw == nil {
		return nil
	}
	var al projectstate.ActivityList
	if err := json.Unmarshal(raw, &al); err != nil {
		return nil
	}
	return al.Activities
}

// networkDepsFromState extracts the []NetworkDependency from the Network slot model.
// Returns nil when the slot is absent or empty.
func networkDepsFromState(s project.ProjectState) []projectstate.NetworkDependency {
	raw := slotModelRaw(s, projectstate.KindNetwork)
	if raw == nil {
		return nil
	}
	var n projectstate.Network
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil
	}
	return n.Dependencies
}

// calendarDaysFromState extracts the CalendarDaysPerWeek from the PlanningAssumptions
// slot model. Returns 5 (the standard 5-day workweek) when the slot is absent.
func calendarDaysFromState(s project.ProjectState) int {
	raw := slotModelRaw(s, projectstate.KindPlanningAssumptions)
	if raw == nil {
		return 5
	}
	var pa projectstate.PlanningAssumptions
	if err := json.Unmarshal(raw, &pa); err == nil && pa.CalendarDaysPerWeek > 0 {
		return int(pa.CalendarDaysPerWeek)
	}
	return 5
}
