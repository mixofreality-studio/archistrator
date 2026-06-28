package web

import (
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
)

// constructionrows.go projects the per-activity construction head-state + the project-level
// tracking snapshot onto the camelCase wire DTO (sibling to gitRowsFromState). The EV/SPI
// earned-value curve is no longer computed here: it is now computed SERVER-SIDE by the
// projectManager (via the constructionEstimationEngine.ComputeEarnedValue op) and arrives on
// project.ConstructionProgress.EV — this layer is a pure pass-through projection onto the
// wire DTO (the relocation off the hand-written web layer, founder gate 2026-06-28). Honest-
// empty: a row absent from the map is simply not emitted; nil progress yields a nil DTO
// (omitted on the wire).

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

// constructionFromState projects the head-state map + progress scalars onto the wire shape,
// consuming the EV/SPI curve the projectManager already computed (progress.EV) rather than
// re-deriving it. Returns (nil, nil) when there is no construction head-state (honest-empty).
func constructionFromState(
	rows map[string]project.ActivityConstructionStatus,
	progress *project.ConstructionProgress,
) (map[string]constructionRowDTO, *constructionProgressDTO) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make(map[string]constructionRowDTO, len(rows))
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
	}
	var prog *constructionProgressDTO
	if progress != nil {
		prog = &constructionProgressDTO{
			Week:           int(progress.Week),
			TotalWeeks:     int(progress.TotalWeeks),
			HandOffModel:   progress.HandOffModel,
			SupervisionCap: int(progress.SupervisionCap),
			EV:             evCurvesFromContract(progress.EV),
		}
	}
	return out, prog
}

// evCurvesFromContract maps the manager-computed project.EVCurve onto the wire DTO — a pure
// pass-through projection (the manager owns the computation now). Weeks narrows int64→int
// for the wire shape the SPA consumes.
func evCurvesFromContract(ev project.EVCurve) evCurvesDTO {
	weeks := make([]int, len(ev.Weeks))
	for i, w := range ev.Weeks {
		weeks[i] = int(w)
	}
	return evCurvesDTO{Weeks: weeks, Earned: ev.Earned, Planned: ev.Planned, SPI: ev.SPI}
}
