package projectstate

import "testing"

func TestTasksForPhase_MatchesFigureA2Grouping(t *testing.T) {
	cases := []struct {
		phase ActivityMethodPhase
		want  []MethodTask
	}{
		{MethodPhaseRequirements, []MethodTask{TaskSRS, TaskSRSReview}},
		{MethodPhaseTestPlan, []MethodTask{TaskSTP, TaskSTPReview}},
		{MethodPhaseDetailedDesign, []MethodTask{TaskSomeConstruction, TaskDetailedDesign, TaskDesignReview}},
		{MethodPhaseConstruction, []MethodTask{TaskConstruction, TaskTestClient, TaskCodeReview}},
		{MethodPhaseIntegration, []MethodTask{TaskIntegration, TaskTesting}},
	}
	for _, c := range cases {
		got := TasksForPhase(c.phase)
		if len(got) != len(c.want) {
			t.Fatalf("TasksForPhase(%v) = %v, want %v", c.phase, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("TasksForPhase(%v)[%d] = %q, want %q", c.phase, i, got[i], c.want[i])
			}
		}
	}
}

func TestTasksForPhase_TwelveTasksTotal(t *testing.T) {
	all := map[MethodTask]bool{}
	for _, p := range []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseTestPlan, MethodPhaseDetailedDesign,
		MethodPhaseConstruction, MethodPhaseIntegration,
	} {
		for _, task := range TasksForPhase(p) {
			all[task] = true
		}
	}
	if len(all) != 12 {
		t.Errorf("total distinct tasks = %d, want 12 (Figure A-1)", len(all))
	}
}

func TestGateTaskFor_IsTheBinaryExitCriterion(t *testing.T) {
	cases := map[ActivityMethodPhase]MethodTask{
		MethodPhaseRequirements:    TaskSRSReview,
		MethodPhaseTestPlan:        TaskSTPReview,
		MethodPhaseDetailedDesign:  TaskDesignReview,
		MethodPhaseConstruction:    TaskCodeReview,
		MethodPhaseIntegration:     TaskTesting,
	}
	for phase, want := range cases {
		if got := GateTaskFor(phase); got != want {
			t.Errorf("GateTaskFor(%v) = %q, want %q", phase, got, want)
		}
		if !IsGateTask(want) {
			t.Errorf("IsGateTask(%q) = false, want true", want)
		}
	}
}

func TestIsConditionalTask_OnlySomeConstructionAndTestClient(t *testing.T) {
	if !IsConditionalTask(TaskSomeConstruction) {
		t.Error("someConstruction must be conditional-emit")
	}
	if !IsConditionalTask(TaskTestClient) {
		t.Error("testClient must be conditional-emit")
	}
	if IsConditionalTask(TaskDetailedDesign) {
		t.Error("detailedDesign must be invariant, not conditional")
	}
}

func TestPhaseForTask_RoundTrips(t *testing.T) {
	for _, p := range []ActivityMethodPhase{
		MethodPhaseRequirements, MethodPhaseTestPlan, MethodPhaseDetailedDesign,
		MethodPhaseConstruction, MethodPhaseIntegration,
	} {
		for _, task := range TasksForPhase(p) {
			if got := PhaseForTask(task); got != p {
				t.Errorf("PhaseForTask(%q) = %v, want %v", task, got, p)
			}
		}
	}
}

func TestTasksForProfile_PerTypeCounts(t *testing.T) {
	cases := []struct {
		name string
		typ  ActivityType
		want int
	}{
		{"service", ActivityTypeService, 12},
		{"frontend", ActivityTypeFrontend, 12},
		{"deployment", ActivityTypeDeployment, 8},
		{"documentation", ActivityTypeDocumentation, 8},
		{"uiDesign", ActivityTypeUIDesign, 5},
		{"integration", ActivityTypeIntegration, 2},
	}
	for _, c := range cases {
		got := TasksForProfile(ProfileFor(c.typ, TestVariantPlan))
		if len(got) != c.want {
			t.Errorf("%s: TasksForProfile len = %d, want %d (got %v)", c.name, len(got), c.want, got)
		}
	}
}

func TestAttemptID_Format(t *testing.T) {
	got := AttemptID("C-billing-manager", TaskDesignReview, 2)
	want := "C-billing-manager:designReview:2"
	if got != want {
		t.Errorf("AttemptID = %q, want %q", got, want)
	}
}
