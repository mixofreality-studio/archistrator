package internal_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// refusedBy is a write refusal whose findings the given design slots own ("" for a rule no slot owns).
func refusedBy(owners ...string) *agenticjob.WriteRefused {
	wr := &agenticjob.WriteRefused{Verb: "phase artifact"}
	for i, o := range owners {
		wr.Findings = append(wr.Findings, agenticjob.RefusedFinding{
			RuleID: fmt.Sprintf("RULE-%d", i+1), Severity: "error", Message: fmt.Sprintf("finding %d is wrong", i+1), OwnerSlot: o,
		})
	}
	return wr
}

// rowDetail is the activity row's failure detail.
func (r *varianceRun) rowDetail(t *testing.T) string {
	t.Helper()
	row, err := r.ae.ReadActivityExecution(fwra.Context{Context: context.Background()}, varProject, varActivity)
	if err != nil {
		t.Fatal(err)
	}
	return row.FailureDetail
}

// A write the gate refused for findings only committed design slots own is that slot's defect, not the
// agent's: the activity ends at once with UpstreamDesignDefect, no retry spends the budget, and its
// failure detail names the slot, every rule id and the first message (ISS-028-2: RUN-028 paid ten N-ATP
// attempts for eight UC-OUTCOME-AT-ASKING-SURFACE refusals no attempt could cure).
func TestVariance_UpstreamDesignDefectEndsTheActivityAtOnce(t *testing.T) {
	script := []agenticjob.PipelineObservation{
		failure(refusedBy("coreUseCases", "system"), true), failure(refusedBy("coreUseCases"), true), success(),
	}
	r := newVarianceRun(t, nil, script)
	attempts, _ := r.run(t, func([]projectstate.TaskAttempt) bool { return r.rowFailureOK() })
	if len(attempts) != 1 || *r.jobs != 1 {
		t.Fatalf("%d attempts %v, %d jobs: the walk must stop at the first refusal", len(attempts), causeKinds(attempts), *r.jobs)
	}
	if got := r.rowFailure(t); got != projectstate.UpstreamDesignDefect {
		t.Fatalf("the activity's failure = %v, want UpstreamDesignDefect", got)
	}
	if k := causeKinds(attempts); k[0] != "writeRefused" {
		t.Errorf("the attempt's cause = %q, want writeRefused", k[0])
	}
	detail := r.rowDetail(t)
	for _, want := range []string{"coreUseCases", "system", "RULE-1", "RULE-2", "finding 1 is wrong"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the failure detail %q does not name %q", detail, want)
		}
	}
}

// settled is the poll a routed run stops at: the activity ended, or the walk has been parked on its
// round trip for longer than any retry would take.
func (r *varianceRun) settled() bool {
	return r.rowFailureOK() || r.env.Now().Sub(r.started) > 5*time.Minute
}

// A write refused for findings one committed slot alone owns, that the method assets give exactly one
// gate for, is sent back to that slot's amendment instead of ended: the change request is seeded once on
// the slot's review thread under the write refusal's marker, the target activity is started to re-open the
// slot's gate, and no further construction job is spent meanwhile (ISS-028-2). The re-opened child here
// has no row to re-open, so the round ends held for the operator, which is not the activity's failure.
func TestVariance_UpstreamDesignDefectIsSentBackToTheOwningSlot(t *testing.T) {
	r := newVarianceRun(t, nil, []agenticjob.PipelineObservation{failure(refusedBy("coreUseCases"), true), success()})
	var reopened []map[string]any
	r.env.SetOnChildWorkflowStartedListener(func(info *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
		var in map[string]any
		if err := args.Get(&in); err != nil {
			t.Errorf("the child's input could not be read: %v", err)
			return
		}
		if info.WorkflowExecution.ID == varProject+":activity:requirements" {
			reopened = append(reopened, in)
		}
	})
	attempts, _ := r.run(t, func([]projectstate.TaskAttempt) bool { return r.settled() })
	if len(attempts) != 1 || *r.jobs != 1 {
		t.Fatalf("%d attempts %v, %d jobs: the refused attempt is recorded once and nothing is retried meanwhile", len(attempts), causeKinds(attempts), *r.jobs)
	}
	if got := r.rowFailure(t); got == projectstate.UpstreamDesignDefect {
		t.Fatalf("the activity ended UpstreamDesignDefect, so the refusal was not routed")
	}
	main, err := r.ps.ReadProject(fwra.Context{Context: context.Background()}, varProject)
	if err != nil {
		t.Fatal(err)
	}
	marker := "[write refusal " + projectstate.AttemptID(varActivity, projectstate.MethodTask(varTask), 1) + "]"
	var seeded []projectstate.ReviewComment
	for _, c := range main.CoreUseCases.ReviewThread {
		if strings.Contains(c.Text, marker) {
			seeded = append(seeded, c)
		}
	}
	if len(seeded) != 1 {
		t.Fatalf("%d change requests carry %q on the core use cases' thread, want exactly 1", len(seeded), marker)
	}
	if c := seeded[0]; c.Type != projectstate.ReviewCommentTypeChangeRequest || !strings.Contains(c.Text, "RULE-1: finding 1 is wrong") {
		t.Errorf("the seeded comment = %+v, want a change request that carries the refusal's finding", c)
	}
	if len(reopened) != 1 {
		t.Fatalf("%d walks of requirements were started, want the one re-open", len(reopened))
	}
	reopen, _ := reopened[0]["Reopen"].(map[string]any)
	if reopen == nil || reopen["TaskID"] != "coreUseCasesReview" {
		t.Errorf("the re-open = %v, want the core use cases' gate coreUseCasesReview re-armed", reopened[0]["Reopen"])
	}
}

// Seed the target's row with the note an earlier bounce of this task left, then refuse again: one round
// trip per task is the loop's bound, so the second refusal ends as UpstreamDesignDefect and says why.
func TestVariance_UpstreamDesignDefectIsSentBackOnlyOncePerTask(t *testing.T) {
	r := newVarianceRun(t, nil, []agenticjob.PipelineObservation{failure(refusedBy("coreUseCases"), true), success()})
	rc := fwra.Context{Context: context.Background()}
	if _, err := r.ae.OpenActivity(rc, varProject, 0, "requirements", projectstate.ActivityTypeRequirements,
		projectstate.TestVariantNone, projectstate.LifecyclePin{
			TypeKey:       projectstate.LifecycleKeyFor(projectstate.ActivityTypeRequirements, projectstate.TestVariantNone),
			AssetsVersion: "test",
		}, projectstate.RepoCredential{}, "seed-open"); err != nil {
		t.Fatal(err)
	}
	earlier := "requirements:note:upstream:" + varActivity + ":" + varTask + ":" + varActivity + ":" + varTask + ":0"
	if _, err := r.ae.RecordOperatorNote(rc, varProject, 1, "requirements",
		projectstate.OperatorNoteInput{NoteID: earlier, Kind: projectstate.NoteSendBack, Text: "an earlier bounce"}, "",
		projectstate.RepoCredential{}, "seed-note"); err != nil {
		t.Fatal(err)
	}
	attempts, _ := r.run(t, func([]projectstate.TaskAttempt) bool { return r.settled() })
	if len(attempts) != 1 || *r.jobs != 1 {
		t.Fatalf("%d attempts, %d jobs, want the one refused attempt", len(attempts), *r.jobs)
	}
	if got := r.rowFailure(t); got != projectstate.UpstreamDesignDefect {
		t.Fatalf("the activity's failure = %v, want UpstreamDesignDefect (%s)", got, r.rowDetail(t))
	}
	if d := r.rowDetail(t); !strings.Contains(d, "; not routed: ") || !strings.Contains(d, "already sent the coreUseCases back once") {
		t.Errorf("the failure detail %q does not say the second refusal was not routed for the earlier bounce", d)
	}
}

// A refusal no round trip can serve is not routed and ends UpstreamDesignDefect with the reason in its
// detail: a slot that is not committed on main, and a slot the method assets give no single gate for.
func TestVariance_UpstreamDesignDefectIsNotRoutedWithoutACommittedSlotAndOneGate(t *testing.T) {
	for _, tc := range []struct{ owner, reason string }{
		{"riskModel", "give 0 gates that judge the riskModel"},
		{"scrubbedRequirements", "scrubbedRequirements is not committed on main"},
	} {
		r := newVarianceRun(t, nil, []agenticjob.PipelineObservation{failure(refusedBy(tc.owner), true), success()})
		attempts, _ := r.run(t, func([]projectstate.TaskAttempt) bool { return r.settled() })
		if len(attempts) != 1 || *r.jobs != 1 {
			t.Fatalf("%s: %d attempts, %d jobs, want the one refused attempt", tc.owner, len(attempts), *r.jobs)
		}
		if got := r.rowFailure(t); got != projectstate.UpstreamDesignDefect {
			t.Fatalf("%s: the activity's failure = %v, want UpstreamDesignDefect", tc.owner, got)
		}
		if d := r.rowDetail(t); !strings.Contains(d, "; not routed: ") || !strings.Contains(d, tc.reason) {
			t.Errorf("%s: the failure detail %q does not carry the reason %q", tc.owner, d, tc.reason)
		}
	}
}

// A refusal with a rule no committed design slot owns is the agent's to fix, so it spends the budget
// like any failure the work is to blame for.
func TestVariance_WriteRefusedWithAnUnownedFindingConsumes(t *testing.T) {
	var script []agenticjob.PipelineObservation
	for range 12 {
		script = append(script, failure(refusedBy("coreUseCases", ""), true))
	}
	r := newVarianceRun(t, nil, script)
	attempts, _ := r.run(t, func([]projectstate.TaskAttempt) bool { return r.rowFailureOK() })
	if len(attempts) != 10 {
		t.Fatalf("the ledger holds %d attempts, want exactly the budget, 10", len(attempts))
	}
	if got := r.rowFailure(t); got != projectstate.VarianceExhausted {
		t.Errorf("the activity's failure = %v, want VarianceExhausted", got)
	}
}
