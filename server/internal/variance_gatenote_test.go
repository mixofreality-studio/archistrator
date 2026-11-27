package internal_test

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ISS-033-6 part 1: an attempt the job's gate failed (a refused edit scope, red checks) tells the
// next attempt of the same task so, as an operator note. RUN-033's stp attempts 2-11 each asked for
// their operator notes, got "No operator notes", and re-affirmed the plan the gate had refused.

const operatorNoteInput = "operator_note"

// A refused edit scope reaches the retry's operator note with the refused paths and the failed
// attempt's id; the first dispatch, which has no predecessor, carries none.
func TestGateFailureNote_ToolRefusedByPolicyReachesTheNextAttempt(t *testing.T) {
	refused := &agenticjob.ToolRefusedByPolicy{Paths: []string{"cmd/server/main.go", "docs/notes.md"}}
	r := newVarianceRun(t, nil, []agenticjob.PipelineObservation{failure(refused, true), success()})
	r.run(t, func(a []projectstate.TaskAttempt) bool {
		return len(a) == 2 && a[1].Outcome == projectstate.OutcomePassed
	})
	specs := *r.specs
	if len(specs) < 2 {
		t.Fatalf("%d construction jobs were submitted, want the refused attempt and its retry", len(specs))
	}
	if n := specs[0].DispatchInputs[operatorNoteInput]; n != "" {
		t.Errorf("the first dispatch carries the note %q", n)
	}
	note := specs[1].DispatchInputs[operatorNoteInput]
	failed := projectstate.AttemptID(varActivity, projectstate.MethodTask(varTask), 1)
	for _, want := range []string{failed, "cmd/server/main.go", "docs/notes.md", "toolRefusedByPolicy"} {
		if !strings.Contains(note, want) {
			t.Errorf("the retry's operator note %q does not name %q", note, want)
		}
	}
}

// A red gate's note names the checks the recorded run holds red, not the venue's summary.
func TestGateFailureNote_GateRedReachesTheNextAttempt(t *testing.T) {
	attempt1 := projectstate.AttemptID(varActivity, projectstate.MethodTask(varTask), 1)
	runs := map[string][][2]string{attempt1: {{"command:build", "pass"}, {"scenario:listManager:x", "fail"}}}
	r := newVarianceRun(t, runs, []agenticjob.PipelineObservation{
		failure(&agenticjob.GateRed{Checks: []string{"command:build"}}, true), success(),
	})
	r.run(t, func(a []projectstate.TaskAttempt) bool {
		return len(a) == 2 && a[1].Outcome == projectstate.OutcomePassed
	})
	specs := *r.specs
	if len(specs) < 2 {
		t.Fatalf("%d construction jobs were submitted, want two", len(specs))
	}
	note := specs[1].DispatchInputs[operatorNoteInput]
	for _, want := range []string{attempt1, "gateRed", "scenario:listManager:x"} {
		if !strings.Contains(note, want) {
			t.Errorf("the retry's operator note %q does not name %q", note, want)
		}
	}
}

// A cause that is not the gate's leaves no note.
func TestGateFailureNote_OtherCausesLeaveNone(t *testing.T) {
	r := newVarianceRun(t, nil, []agenticjob.PipelineObservation{
		failure(&agenticjob.AgentGaveUp{Detail: "no commits"}, true), success(),
	})
	r.run(t, func(a []projectstate.TaskAttempt) bool {
		return len(a) == 2 && a[1].Outcome == projectstate.OutcomePassed
	})
	specs := *r.specs
	if len(specs) < 2 {
		t.Fatalf("%d construction jobs were submitted, want two", len(specs))
	}
	if n := specs[1].DispatchInputs[operatorNoteInput]; n != "" {
		t.Errorf("an agentGaveUp attempt left the note %q", n)
	}
}

// On a fork the note belongs to the task whose attempt failed. construction and stp run side by
// side after detailedDesign: the first of them to be dispatched is refused by the edit scope, and
// the sibling's dispatch beside it must neither carry nor drain the refused task's note; the
// refused task's retry carries it, naming the attempt that failed, not the sibling's.
func TestGateFailureNote_IsKeptForItsOwnTaskOnAFork(t *testing.T) {
	refused := &agenticjob.ToolRefusedByPolicy{Paths: []string{"cmd/server/main.go"}}
	script := []agenticjob.PipelineObservation{success(), success(), failure(refused, true)} // srs, detailedDesign, the first fork task
	r := newVarianceRun(t, nil, script)
	r.run(t, func([]projectstate.TaskAttempt) bool { return len(*r.specs) >= 5 || r.settled() })
	specs := *r.specs
	if len(specs) < 5 {
		t.Fatalf("%d construction jobs were submitted, want srs, detailedDesign, both fork tasks and the refused task's retry", len(specs))
	}
	failedTask := specs[2].DispatchInputs["task_id"]
	var sibling, retry []agenticjob.PipelineSpec
	for _, s := range specs[3:] {
		if s.DispatchInputs["task_id"] == failedTask {
			retry = append(retry, s)
		} else {
			sibling = append(sibling, s)
		}
	}
	if len(retry) == 0 || len(sibling) == 0 {
		t.Fatalf("the jobs after the refusal were not one retry and one sibling dispatch: %d retries, %d siblings", len(retry), len(sibling))
	}
	for _, s := range sibling {
		if n := s.DispatchInputs[operatorNoteInput]; n != "" {
			t.Errorf("the sibling %s carried the refused task's note %q", s.DispatchInputs["task_id"], n)
		}
	}
	want := projectstate.AttemptID(varActivity, projectstate.MethodTask(failedTask), 1)
	note := retry[0].DispatchInputs[operatorNoteInput]
	if !strings.Contains(note, want) || !strings.Contains(note, "cmd/server/main.go") {
		t.Errorf("the retry of %s carries %q, want the note for attempt %s naming the refused path", failedTask, note, want)
	}
}

// ISS-033-6 part 3, walk half: the note a gate failure leaves is no new input, so two attempts the
// gate refused with the same net diff (the same fingerprint) still settle as noProgress. If the note
// counted as steering it would reset the comparison and excuse the repeat for ever.
func TestNoProgress_RepeatedGateRefusalSettlesDespiteTheGateNote(t *testing.T) {
	fp := "same-net-diff"
	refuse := func() agenticjob.PipelineObservation {
		obs := failure(&agenticjob.ToolRefusedByPolicy{Paths: []string{"cmd/server/main.go"}}, true)
		obs.ProgressFingerprint = &fp
		return obs
	}
	r := newVarianceRun(t, nil, []agenticjob.PipelineObservation{refuse(), refuse(), refuse()})
	attempts, _ := r.run(t, func(a []projectstate.TaskAttempt) bool { return len(a) >= 2 || r.rowFailureOK() })
	kinds := causeKinds(attempts)
	if len(kinds) < 2 || kinds[0] != "toolRefusedByPolicy" || kinds[1] != "noProgress" {
		t.Fatalf("the attempts' causes are %v, want toolRefusedByPolicy then noProgress", kinds)
	}
}
