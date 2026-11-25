package internal_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-conformance"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	agenticjobfake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
	messagebusfake "github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus/fake"
)

// The construction loop's variance budget (FPC plan 6, Task 6.4): a dispatch that failed for want of a
// model, a runner or a network says nothing about the work, so it never consumes an attempt; one the
// agent failed does; a gate's red is the recorded run's truth. Nothing here calls the loop's helpers.
// The REAL delivery Manager's worker runs the REAL activity walk of the fixture's `todo-list-access`
// service activity in Temporal's in-process test environment, over the REAL git-backed facets in a
// throwaway repo, with the agentic-job access the one double: it answers each construction job from a
// script. Every probe is a contract read of the attempt ledger or a public operation of the Manager.

const (
	varProject  = "todomvc-conformance"
	varActivity = "C-todo-list-access"
	varTask     = "srs"
)

// varianceRepo is the project's repo: main carries the todomvc conformance fixture, and the working
// copy sits on another branch so pushes to main are accepted. runs, when set, are check runs the
// activity branch holds (the venue records them there), each as {revision, [[checkId, status]...]}.
func varianceRepo(t *testing.T, runs map[string][][2]string) string {
	t.Helper()
	raw, err := conformance.Fixture("todomvc")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "variance")
	if err := os.MkdirAll(filepath.Join(dir, ".aiarch", "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aiarch", "state", "project.json"), raw, 0o644); err != nil { //nolint:gosec // a repo file
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=seed", "-c", "user.email=seed@aiarch.local"}, args...)...)
		cmd.Dir = dir
		if out, gerr := cmd.CombinedOutput(); gerr != nil {
			t.Fatalf("git %v: %v\n%s", args, gerr, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("add", ".")
	git("commit", "-q", "-m", "arranged state")
	if len(runs) > 0 {
		// The venue's record of a gate: the activity branch holds the project with its check runs.
		var top map[string]json.RawMessage
		if err := json.Unmarshal(raw, &top); err != nil {
			t.Fatal(err)
		}
		var recorded []map[string]any
		for revision, results := range runs {
			var rs []map[string]any
			for _, r := range results {
				rs = append(rs, map[string]any{"checkId": r[0], "kind": "command", "status": r[1], "durationMs": 1})
			}
			recorded = append(recorded, map[string]any{
				"id": "run-" + revision, "activity": varActivity, "task": varTask, "revision": revision,
				"specHash": "h", "artifact": "a", "results": rs,
			})
		}
		top["testingState"], err = json.Marshal(map[string]any{"checkRuns": recorded})
		if err != nil {
			t.Fatal(err)
		}
		out, err := json.MarshalIndent(top, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		git("checkout", "-q", "-b", "activity/"+varActivity)
		if err := os.WriteFile(filepath.Join(dir, ".aiarch", "state", "project.json"), out, 0o644); err != nil { //nolint:gosec // a repo file
			t.Fatal(err)
		}
		git("commit", "-q", "-am", "the venue's check run")
	}
	git("checkout", "-q", "-b", "checkout")
	return "file://" + dir
}

// failure is one scripted construction job: it failed, as the venue classified it.
func failure(cause agenticjob.FailureCause, reachedModel bool) agenticjob.PipelineObservation {
	return agenticjob.PipelineObservation{
		Phase: agenticjob.PhaseFailed, Diagnostic: "the scripted job failed", ReachedModel: reachedModel, Cause: &cause,
	}
}

// success is one scripted construction job that finished.
func success() agenticjob.PipelineObservation {
	return agenticjob.PipelineObservation{Phase: agenticjob.PhaseSucceeded, ReachedModel: true}
}

// varianceRun is one arranged activity, its construction jobs answered from a script.
type varianceRun struct {
	env     *testsuite.TestWorkflowEnvironment
	mgr     delivery.DeliveryManager
	ae      projectstate.ActivityExecutionAccess
	started time.Time
	// jobs is how many construction jobs were submitted.
	jobs *int
}

func newVarianceRun(t *testing.T, repoRuns map[string][][2]string, script []agenticjob.PipelineObservation) *varianceRun {
	t.Helper()
	url := varianceRepo(t, repoRuns)
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.SetTestTimeout(10 * time.Minute)
	ae := projectstate.NewGitLocalActivityExecutionAccess(url)
	submitted := 0
	answers := map[agenticjob.PipelineHandle]agenticjob.PipelineObservation{}
	jobs := &agenticjobfake.FakeAgenticJobAccess{
		SubmitAgenticJobFn: func(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
			h := agenticjob.PipelineHandle(fmt.Sprintf("job-%d", len(answers)+1))
			if spec.DispatchInputs[agenticjob.DispatchInputJobKey] == agenticjob.DispatchJobRefresh {
				answers[h] = success()
				return h, nil
			}
			obs := success()
			if submitted < len(script) {
				obs = script[submitted]
			}
			submitted++
			obs.Handle = h
			answers[h] = obs
			return h, nil
		},
		ObserveAgenticJobFn: func(_ fwra.Context, h agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
			return answers[h], nil
		},
	}
	bus := &messagebusfake.FakeMessageBus{
		DeliverSignalFn: func(fwra.Context, messagebus.ExecutionID, messagebus.SignalName, messagebus.ExecutionPayload, *messagebus.ExecutionStart) error {
			return nil
		},
	}
	mgr := delivery.NewDeliveryManager(envClient{env: env}, projectstate.NewGitLocalProjectStateAccess(url), nil,
		intervention.NewInterventionEngine(), review.NewReviewEngine(), estimation.NewEstimationEngine(),
		operationestimation.NewOperationEstimationEngine(), billing.NewBillingEngine(), designhealth.NewDesignHealthEngine(),
		jobs, nil, projectstate.NewGitLocalGitActivityStatusAccess(url), projectstate.NewGitLocalDesignSessionAccess(url),
		ae, bus, episode.NewLocalFSEpisodeAccess(url), 0, "tiered")
	delivery.RegisterManagerWorker(envWorker{env: env}, mgr)
	return &varianceRun{env: env, mgr: mgr, ae: ae, jobs: &submitted}
}

// attempts is the srs task's attempt ledger, oldest first.
func (r *varianceRun) attempts(t *testing.T) []projectstate.TaskAttempt {
	t.Helper()
	row, err := r.ae.ReadActivityExecution(fwra.Context{Context: context.Background()}, varProject, varActivity)
	if err != nil {
		return nil
	}
	var out []projectstate.TaskAttempt
	for _, a := range row.Attempts {
		if string(a.Task) == varTask {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b projectstate.TaskAttempt) int { return a.Attempt - b.Attempt })
	return out
}

// causeKinds is the kind each attempt's cause names ("" for none).
func causeKinds(attempts []projectstate.TaskAttempt) []string {
	out := make([]string, len(attempts))
	for i, a := range attempts {
		if a.Cause != nil && *a.Cause != nil {
			out[i] = string((*a.Cause).Kind())
		}
	}
	return out
}

// run starts the activity's walk and stops it (cancelling the workflow) the first time done reports
// true, polled once a virtual minute. It returns the ledger and the virtual time the walk took.
func (r *varianceRun) run(t *testing.T, done func(attempts []projectstate.TaskAttempt) bool) ([]projectstate.TaskAttempt, time.Duration) {
	t.Helper()
	typ, err := json.Marshal(projectstate.ActivityTypeService)
	if err != nil {
		t.Fatal(err)
	}
	r.started = r.env.Now()
	var poll func()
	poll = func() {
		if done(r.attempts(t)) {
			r.env.CancelWorkflow()
			return
		}
		r.env.RegisterDelayedCallback(poll, time.Minute)
	}
	r.env.RegisterDelayedCallback(poll, time.Minute)
	r.env.ExecuteWorkflow("deliveryActivity", map[string]any{
		"ProjectID":  varProject,
		"ActivityID": varActivity,
		"Activity": map[string]any{
			"ActivityID": varActivity, "Kind": 2, "ComponentID": "todo-list-access", "Layer": "ResourceAccess",
			"Type": json.RawMessage(typ),
		},
	})
	return r.attempts(t), r.env.Now().Sub(r.started)
}

// rowFailure is the activity's recorded terminal failure reason, or the zero value.
func (r *varianceRun) rowFailure(t *testing.T) projectstate.FailureReason {
	t.Helper()
	row, err := r.ae.ReadActivityExecution(fwra.Context{Context: context.Background()}, varProject, varActivity)
	if err != nil {
		t.Fatal(err)
	}
	return row.FailureReason
}

// hostCauses cycle through the four host kinds, so the whole class is held to the rule.
func hostCause(i int) agenticjob.FailureCause {
	switch i % 4 {
	case 0:
		return &agenticjob.ModelUnreachable{Detail: "connection error"}
	case 1:
		return &agenticjob.RateLimited{Detail: "429"}
	case 2:
		return &agenticjob.RunnerLost{Detail: "the server restarted"}
	default:
		return &agenticjob.InfraTimeout{Detail: "timed out"}
	}
}

// Twelve host faults in a row are twelve dispatches and no attempt: the walk waits between them,
// doubling from a minute to a quarter of an hour, and the thirteenth, which succeeds, is the work's
// first judged attempt. The budget (ten) was never reached, though more dispatches than it failed.
func TestVariance_HostFaultNeverConsumesAttempt(t *testing.T) {
	var script []agenticjob.PipelineObservation
	for i := range 12 {
		script = append(script, failure(hostCause(i), false))
	}
	script = append(script, success())
	r := newVarianceRun(t, nil, script)
	attempts, took := r.run(t, func(a []projectstate.TaskAttempt) bool {
		return len(a) == 13 && a[12].Outcome == projectstate.OutcomePassed
	})
	kinds := causeKinds(attempts)
	if len(attempts) != 13 {
		t.Fatalf("the ledger holds %d attempts %v, want 13 (twelve host faults and the success)", len(attempts), kinds)
	}
	for i, k := range kinds[:12] {
		if want := string((hostCause(i)).Kind()); k != want {
			t.Errorf("attempt %d cause = %q, want %q", i+1, k, want)
		}
	}
	if kinds[12] != "" {
		t.Errorf("the successful attempt carries the cause %q", kinds[12])
	}
	if got := r.rowFailure(t); got != projectstate.FailureReasonUnknown {
		t.Errorf("the activity failed (%v) though no attempt of the work was spent", got)
	}
	// 1, 2, 4, 8, then fifteen minutes seven times: the wait the walk took before the thirteenth dispatch.
	wait := time.Duration(1+2+4+8+7*15) * time.Minute
	if took < wait {
		t.Errorf("the walk took %v of virtual time, want at least the backoff %v", took, wait)
	}
}

// A streak of host faults that outlives the window is no longer the loop's to wait out: it is escalated to
// the operator, the same hold a repeatedly failing task earns, and never recorded as the work's failure.
func TestVariance_HostFaultEscalatesAfterTheWindow(t *testing.T) {
	var script []agenticjob.PipelineObservation
	for i := range 40 {
		script = append(script, failure(hostCause(i), false))
	}
	r := newVarianceRun(t, nil, script)
	// The walk is left to run well past the window; an escalation parks it, a loop that kept waiting
	// would keep dispatching every quarter of an hour.
	attempts, took := r.run(t, func([]projectstate.TaskAttempt) bool { return r.env.Now().Sub(r.started) >= 3*time.Hour })
	if took < 3*time.Hour {
		t.Fatalf("the walk ended after %v of virtual time, before the poll that stops it: %v", took, r.env.GetWorkflowError())
	}
	// Dispatches 1-12 are answered 1, 2, 4, 8 and then fifteen minutes apart; the thirteenth is seen
	// 135 minutes after the first fault, which is past the 2h window, so the walk parks there.
	if n := len(attempts); n != 13 {
		t.Errorf("the ledger holds %d attempts %v, want 13: the walk should park at the thirteenth dispatch, not keep dispatching", n, causeKinds(attempts))
	}
	if *r.jobs != 13 {
		t.Errorf("%d construction jobs were submitted, want 13", *r.jobs)
	}
	if got := r.rowFailure(t); got == projectstate.VarianceExhausted {
		t.Errorf("a host fault streak spent the variance budget (%v)", got)
	}
	for i, k := range causeKinds(attempts) {
		if k == "" {
			t.Errorf("attempt %d records no cause", i+1)
		}
	}
}

// Ten attempts the agent failed spend the budget, as they always have: the activity ends VarianceExhausted.
func TestVariance_AgentFaultConsumes(t *testing.T) {
	agentCauses := []agenticjob.FailureCause{
		&agenticjob.AgentGaveUp{Detail: "no commits"},
		&agenticjob.ToolRefusedByPolicy{Paths: []string{"cmd/server/main.go"}},
		&agenticjob.UnknownCause{Detail: "nobody knows"},
	}
	var script []agenticjob.PipelineObservation
	for i := range 12 {
		script = append(script, failure(agentCauses[i%len(agentCauses)], true))
	}
	r := newVarianceRun(t, nil, script)
	attempts, _ := r.run(t, func([]projectstate.TaskAttempt) bool { return r.rowFailureOK() })
	if len(attempts) != 10 {
		t.Fatalf("the ledger holds %d attempts %v, want exactly the budget, 10", len(attempts), causeKinds(attempts))
	}
	if got := r.rowFailure(t); got != projectstate.VarianceExhausted {
		t.Errorf("the activity's failure = %v, want VarianceExhausted", got)
	}
}

// rowFailureOK is whether the activity row records a terminal failure yet (a poll helper).
func (r *varianceRun) rowFailureOK() bool {
	row, err := r.ae.ReadActivityExecution(fwra.Context{Context: context.Background()}, varProject, varActivity)
	return err == nil && row.FailureReason != projectstate.FailureReasonUnknown
}

// The gate's truth is the recorded run's, not the venue's summary or the agent's word (FIX-054, FIX-018).
// The venue reports a red build; the run it recorded on the branch for that attempt holds the build green
// and a scenario red, so the scenario is the cause the ledger keeps. A red gate with no recorded run is
// unknown, and spends an attempt like any failure the work is to blame for.
func TestGateRed_TruthFromRunnerResults(t *testing.T) {
	attempt1 := projectstate.AttemptID(varActivity, projectstate.MethodTask(varTask), 1)
	runs := map[string][][2]string{attempt1: {{"command:build", "pass"}, {"scenario:listManager:x", "fail"}}}
	script := []agenticjob.PipelineObservation{
		failure(&agenticjob.GateRed{Checks: []string{"command:build"}}, true), // the run is recorded: the truth overrides
		failure(&agenticjob.GateRed{Checks: []string{"command:build"}}, true), // no run for this attempt: unknown
		success(),
	}
	r := newVarianceRun(t, runs, script)
	attempts, _ := r.run(t, func(a []projectstate.TaskAttempt) bool {
		return len(a) == 3 && a[2].Outcome == projectstate.OutcomePassed
	})
	if len(attempts) != 3 {
		t.Fatalf("the ledger holds %d attempts %v, want 3", len(attempts), causeKinds(attempts))
	}
	red, ok := (*attempts[0].Cause).(*projectstate.GateRed)
	if !ok || !slices.Equal(red.Checks, []string{"scenario:listManager:x"}) {
		t.Errorf("attempt 1 cause = %+v, want gateRed naming the recorded run's scenario:listManager:x", *attempts[0].Cause)
	}
	if kinds := causeKinds(attempts); kinds[1] != "unknown" {
		t.Errorf("attempt 2 (a red gate with no recorded run) cause = %q, want unknown (%v)", kinds[1], kinds)
	}
}

// Every cause kind the schema declares is a kind the ledger's own codec accepts and an undeclared one is
// refused; and each kind's class is one of the four the loop switches on. The loop's own classification is a
// switch over the sealed sum with no default, so a kind added to the schema without a class does not compile
// through `make lint` (gochecksumtype); the variance tests above hold the classes' behaviour.
func TestCause_SwitchExhaustive(t *testing.T) {
	_, raw := ownProjectJSON(t)
	var doc struct {
		ServiceContracts map[string]struct {
			Defs map[string]struct {
				Sum *struct {
					Variants []struct {
						Kind string `json:"kind"`
					} `json:"variants"`
				} `json:"x-go-sumtype"`
			} `json:"$defs"`
		} `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	class := map[string]string{
		"modelUnreachable": "host", "rateLimited": "host", "runnerLost": "host", "infraTimeout": "host",
		"agentGaveUp": "agent", "noProgress": "agent", "toolRefusedByPolicy": "agent",
		"writeRefused": "agent", "gateRed": "gate", "unknown": "unknown",
	}
	for _, contract := range []string{"agenticJobAccess", "activityExecutionAccess"} {
		sum := doc.ServiceContracts[contract].Defs["FailureCause"].Sum
		if sum == nil {
			t.Fatalf("%s declares no FailureCause sum", contract)
		}
		var kinds []string
		for _, v := range sum.Variants {
			kinds = append(kinds, v.Kind)
			if _, ok := class[v.Kind]; !ok {
				t.Errorf("%s declares the cause kind %q, which this test classifies nowhere: classify it in the loop's faultClassOf and here", contract, v.Kind)
			}
			envelope := fmt.Sprintf(`{"kind":%q,"model":{}}`, v.Kind)
			if _, err := agenticjob.UnmarshalFailureCause([]byte(envelope)); err != nil {
				t.Errorf("agenticjob refuses its own kind %q: %v", v.Kind, err)
			}
			if _, err := projectstate.UnmarshalFailureCause([]byte(envelope)); err != nil {
				t.Errorf("projectstate refuses its own kind %q: %v", v.Kind, err)
			}
		}
		for k := range class {
			if !slices.Contains(kinds, k) {
				t.Errorf("%s no longer declares the cause kind %q", contract, k)
			}
		}
	}
	if _, err := agenticjob.UnmarshalFailureCause([]byte(`{"kind":"cosmicRay","model":{}}`)); err == nil || !strings.Contains(err.Error(), "cosmicRay") {
		t.Errorf("an undeclared kind was accepted: %v", err)
	}
}
