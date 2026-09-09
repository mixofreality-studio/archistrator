package projectstate

import "fmt"

// MethodTask is one of the twelve internal tasks of Figure A-1 (Löwy, Righting
// Software, Appendix A) — the unit BELOW a lifecycle phase and ABOVE an attempt.
//
// Figure A-1 is not a linear chain. After SRS Review the graph forks: the Test Plan
// branch (STP → STP Review) runs in parallel with the Detailed Design → Construction
// branch, and the two rejoin at Testing. Construction and Test Client are built in
// tandem (a bidirectional edge in the figure).
//
// Naming follows the R3 ruling: the bare word "phase" is banned; this level is a
// TASK, the level above it is a LifecyclePhase, one execution of a task is an Attempt.
type MethodTask string

// The twelve Figure A-1 tasks.
const (
	TaskSRS              MethodTask = "srs"
	TaskSRSReview        MethodTask = "srsReview"
	TaskSTP              MethodTask = "stp"
	TaskSTPReview        MethodTask = "stpReview"
	TaskSomeConstruction MethodTask = "someConstruction"
	TaskDetailedDesign   MethodTask = "detailedDesign"
	TaskDesignReview     MethodTask = "designReview"
	TaskConstruction     MethodTask = "construction"
	TaskTestClient       MethodTask = "testClient"
	TaskCodeReview       MethodTask = "codeReview"
	TaskIntegration      MethodTask = "integration"
	TaskTesting          MethodTask = "testing"
)

// phaseTasks is the Figure A-2 grouping: which tasks make up each lifecycle phase.
// Order within a phase is execution order.
var phaseTasks = map[ActivityMethodPhase][]MethodTask{
	MethodPhaseRequirements:   {TaskSRS, TaskSRSReview},
	MethodPhaseTestPlan:       {TaskSTP, TaskSTPReview},
	MethodPhaseDetailedDesign: {TaskSomeConstruction, TaskDetailedDesign, TaskDesignReview},
	MethodPhaseConstruction:   {TaskConstruction, TaskTestClient, TaskCodeReview},
	MethodPhaseIntegration:    {TaskIntegration, TaskTesting},
}

// gateTasks is the binary exit criterion per phase (App A: "the Construction phase is
// complete once you have had the code review, not simply when the code is checked in").
var gateTasks = map[ActivityMethodPhase]MethodTask{
	MethodPhaseRequirements:   TaskSRSReview,
	MethodPhaseTestPlan:       TaskSTPReview,
	MethodPhaseDetailedDesign: TaskDesignReview,
	MethodPhaseConstruction:   TaskCodeReview,
	MethodPhaseIntegration:    TaskTesting,
}

// conditionalTasks are emitted ONLY when a real attempt record exists for them.
// someConstruction is Löwy's pre-design spike and our agentic detailed-design dispatch
// is a single episode; testClient is the tandem partner of Construction and often does
// not exist for a deployment or a doc. Rendering a row for work that never happened is
// the "view states something false" failure this stage exists to remove.
var conditionalTasks = map[MethodTask]bool{
	TaskSomeConstruction: true,
	TaskTestClient:       true,
}

// TasksForPhase returns the Figure A-1 tasks belonging to a lifecycle phase, in
// execution order. An unknown phase returns nil.
func TasksForPhase(p ActivityMethodPhase) []MethodTask {
	src := phaseTasks[p]
	if len(src) == 0 {
		return nil
	}
	out := make([]MethodTask, len(src))
	copy(out, src)
	return out
}

// GateTaskFor returns the task whose success IS the phase's binary exit criterion.
func GateTaskFor(p ActivityMethodPhase) MethodTask { return gateTasks[p] }

// IsGateTask reports whether a task is some phase's binary exit criterion.
func IsGateTask(t MethodTask) bool {
	for _, gate := range gateTasks {
		if gate == t {
			return true
		}
	}
	return false
}

// IsConditionalTask reports whether a task is emitted only when an attempt exists.
func IsConditionalTask(t MethodTask) bool { return conditionalTasks[t] }

// PhaseForTask returns the lifecycle phase a task belongs to (the empty phase when
// the task is unknown).
func PhaseForTask(t MethodTask) ActivityMethodPhase {
	for p, tasks := range phaseTasks {
		for _, candidate := range tasks {
			if candidate == t {
				return p
			}
		}
	}
	return ""
}

// TasksForProfile returns every task an activity with this profile can have, in phase
// order then execution order. This is the ROW SET of the list view: it is derived from
// the profile, never from storage, so the tasks that have not happened still render.
func TasksForProfile(pr Profile) []MethodTask {
	out := make([]MethodTask, 0, 12)
	for _, ph := range pr.Phases {
		out = append(out, TasksForPhase(ph.Phase)...)
	}
	return out
}

// AttemptID is THE join key of the construction model: the ledger primary key, the UI
// click target, the sub-graph node label, the unit provenance applies to, and the
// TargetRef stamped onto every EpisodeRecord. n is 1-based per (activity, task).
//
// Its format must be identical everywhere it is produced. Do not inline it.
func AttemptID(activityID string, t MethodTask, n int) string {
	return fmt.Sprintf("%s:%s:%d", activityID, t, n)
}
