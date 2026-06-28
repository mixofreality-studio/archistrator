package web

import (
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
)

// TestConstructionFromState_ConsumesManagerEV proves the web layer now PASSES THROUGH the
// EV/SPI curve the projectManager computed (project.ConstructionProgress.EV) onto the wire
// DTO rather than re-deriving it (the relocation, founder gate 2026-06-28). int64 weeks
// narrow to int; earned/planned/spi flow verbatim.
func TestConstructionFromState_ConsumesManagerEV(t *testing.T) {
	rows := map[string]project.ActivityConstructionStatus{
		"A": {ActivityID: "A"},
	}
	progress := &project.ConstructionProgress{
		Week:           2,
		TotalWeeks:     4,
		HandOffModel:   "senior",
		SupervisionCap: 3,
		EV: project.EVCurve{
			Weeks:   []int64{0, 1, 2, 3, 4},
			Earned:  []float64{0, 25, 50, 50, 50},
			Planned: []float64{0, 25, 50, 75, 100},
			SPI:     0.5,
		},
	}

	_, prog := constructionFromState(rows, progress)
	if prog == nil {
		t.Fatal("expected non-nil progress DTO")
	}
	if prog.EV.SPI != 0.5 {
		t.Errorf("SPI passthrough: got %v want 0.5", prog.EV.SPI)
	}
	if len(prog.EV.Weeks) != 5 || prog.EV.Weeks[4] != 4 {
		t.Errorf("weeks passthrough: got %v", prog.EV.Weeks)
	}
	if prog.EV.Earned[2] != 50 || prog.EV.Planned[4] != 100 {
		t.Errorf("curve passthrough mismatch: earned=%v planned=%v", prog.EV.Earned, prog.EV.Planned)
	}
}
