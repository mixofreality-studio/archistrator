package internal_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
