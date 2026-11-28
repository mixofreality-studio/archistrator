package internal_test

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ISS-034-3: a job's generation step refusing the committed design is no variance in the work. RUN-034
// re-dispatched the C-list-manager integration job ten times over the same "go tool appgen clients"
// refusal; its outcome depends only on committed inputs, so the walk ends at the first refusal with
// UpstreamDesignDefect, spends no further job, and the failure detail names the generator and its message.
func TestVariance_GenerationRefusedEndsTheActivityAtOnce(t *testing.T) {
	refused := func() agenticjob.PipelineObservation {
		return failure(&agenticjob.GenerationRefused{
			Generator: "go tool appgen clients",
			Detail:    "go tool appgen clients failed: exit status 1: test plan: binding \"p2\" arrange[0]: unknown field \"as\"",
		}, true)
	}
	r := newVarianceRun(t, nil, []agenticjob.PipelineObservation{refused(), refused(), success()})
	attempts, _ := r.run(t, func([]projectstate.TaskAttempt) bool { return r.rowFailureOK() })
	if len(attempts) != 1 || *r.jobs != 1 {
		t.Fatalf("%d attempts %v, %d jobs: the walk must stop at the first generation refusal", len(attempts), causeKinds(attempts), *r.jobs)
	}
	if got := r.rowFailure(t); got != projectstate.UpstreamDesignDefect {
		t.Fatalf("the activity's failure = %v, want UpstreamDesignDefect", got)
	}
	if k := causeKinds(attempts); k[0] != "generationRefused" {
		t.Errorf("the attempt's cause = %q, want generationRefused", k[0])
	}
	detail := r.rowDetail(t)
	for _, want := range []string{"go tool appgen clients", "unknown field \"as\""} {
		if !strings.Contains(detail, want) {
			t.Errorf("the failure detail %q does not name %q", detail, want)
		}
	}
}
