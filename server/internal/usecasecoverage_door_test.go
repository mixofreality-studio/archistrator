package internal_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// The coverage view's Door cell is a sealed sum: a Client the actor enters through (Door), none (NoDoor,
// the finding the PM sends back), or not yet judged (DoorPendingArchitecture). RUN-025: a door is a
// judgement about the architecture, so it cannot read NONE while no architecture exists.

// todomvcAt is the platform's conformance todomvc fixture (a whole designed app). With
// truncateAfterCoreUseCases every artifact the Method records AFTER the core use cases is taken away:
// the project as it stands the moment the core use cases are committed and the architecture has not
// begun, so nothing downstream exists to be found wanting.
func todomvcAt(t *testing.T, truncateAfterCoreUseCases bool) *projectstate.Project {
	t.Helper()
	raw, err := conformance.Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil || !ok {
		t.Fatalf("decode the todomvc fixture: ok=%v err=%v", ok, err)
	}
	if truncateAfterCoreUseCases {
		none := projectstate.ArtifactSlot{}
		p.SystemDesign, p.OperationalConcepts, p.StandardCheck = none, none, none
		p.PlanningAssumptions, p.ActivityList, p.Network = none, none, none
		p.NormalSolution, p.SubcriticalSolution = none, none
		p.CompressedSolution, p.DecompressedSolution = none, none
		p.RiskModel, p.SdpReview = none, none
		p.ServiceContracts, p.PhaseArtifacts = nil, nil
	}
	return &p
}

// doorKind names a door by exhaustive switch, so a fourth variant fails the build here.
func doorKind(d projectstate.CoverageDoor) string {
	switch d := d.(type) {
	case projectstate.Door:
		return "door:" + d.Client
	case projectstate.NoDoor:
		return "none"
	case projectstate.DoorPendingArchitecture:
		return "pending"
	}
	return "unreachable"
}

// While the System is not recorded there is no Client to enter through, so the door is not "none" (a
// finding the PM sends back) but not yet judged; and the rules that read the architecture and the
// contracts raise nothing against a path, because the artifacts they judge do not exist yet.
func TestCoverageView_BeforeTheSystemExistsEveryDoorIsPendingAndNoScenarioProblemAppears(t *testing.T) {
	cov, err := projectstate.UseCaseCoverageOf(todomvcAt(t, true), false)
	if err != nil {
		t.Fatal(err)
	}
	paths := 0
	for _, uc := range cov {
		for _, p := range uc.Paths {
			paths++
			if got := doorKind(p.Door); got != "pending" {
				t.Errorf("%s: door %s, want pending (the architecture is not recorded)", p.Scenario, got)
			}
			for _, code := range p.Problems {
				t.Errorf("%s: %s reported before the artifacts it judges are recorded", p.Scenario, code)
			}
		}
	}
	if paths == 0 {
		t.Fatal("the truncated fixture derives no path: the test judges nothing")
	}
	table := projectstate.RenderCoverageTable(cov)
	if !strings.Contains(table, "not yet judged (architecture pending)") {
		t.Errorf("the table must say the door is not yet judged:\n%s", table)
	}
	if strings.Contains(table, "SCN-") {
		t.Errorf("the table lists a scenario problem before its prerequisites exist:\n%s", table)
	}
}

// The same project with its whole design is judged as it always was: every door resolves to a Client
// or to none, and the problems are the derivation's own.
func TestCoverageView_OnTheFullFixtureDoorsResolveAsBefore(t *testing.T) {
	p := todomvcAt(t, false)
	cov, err := projectstate.UseCaseCoverageOf(p, false)
	if err != nil {
		t.Fatal(err)
	}
	in, err := projectstate.ScenarioInputOf(p)
	if err != nil {
		t.Fatal(err)
	}
	report, err := scenario.DeriveReport(in)
	if err != nil {
		t.Fatal(err)
	}
	wantProblems := map[string][]string{}
	for _, pr := range report.Problems {
		wantProblems[pr.Scenario] = append(wantProblems[pr.Scenario], pr.Code)
	}
	opened := 0
	for _, uc := range cov {
		for _, pc := range uc.Paths {
			switch d := pc.Door.(type) {
			case projectstate.Door:
				opened++
				if d.Client == "" {
					t.Errorf("%s: a door that names no Client", pc.Scenario)
				}
			case projectstate.NoDoor:
			case projectstate.DoorPendingArchitecture:
				t.Errorf("%s: the door is pending although the System is recorded", pc.Scenario)
			}
			if want := wantProblems[pc.Scenario]; !slices.Equal(append([]string{}, pc.Problems...), append([]string{}, want...)) {
				t.Errorf("%s: problems %v, want the derivation's %v", pc.Scenario, pc.Problems, want)
			}
		}
	}
	if opened == 0 {
		t.Fatal("no path of the full fixture reaches a door")
	}
}

// The served view carries the same three-way door on the wire, one variant per state.
func TestCoverageView_ManagerServesThePendingDoor(t *testing.T) {
	pid := "todomvc-conformance"
	v, err := viewManager(*todomvcAt(t, true), nil).QueryProjectView(viewCtx(), delivery.ProjectViewQuery{Kind: delivery.ProjectViewUseCaseCoverage, ProjectID: &pid})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, uc := range v.UseCaseCoverage.UseCases {
		for _, p := range uc.Paths {
			n++
			switch p.Door.(type) {
			case *delivery.CoverageDoorPending:
			case *delivery.CoverageDoorClient, *delivery.CoverageNoDoor:
				t.Errorf("%s: served door %T, want the pending door", p.Scenario, p.Door)
			}
		}
	}
	if n == 0 {
		t.Fatal("the view serves no path")
	}
}
