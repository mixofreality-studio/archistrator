package main

// acceptancejob_test.go holds the venue half of the M-ACCEPT milestone's job task (FPC plan 4,
// Amendment C): the local executor runs the task spec's checks and records the CheckRun with no
// agent at all. It lives here, not beside the executor, because the born-repo and state-binary
// helpers do, and a component package holds only its generated and its one handwritten file.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// mAcceptActivityID is the use-case acceptance milestone's node: the fixture's activity list (slot 9)
// carries it, as the derivation emits it.
const mAcceptActivityID = "M-ACCEPT"

// pathWithout is the process PATH minus every directory that holds an executable named bin.
func pathWithout(t *testing.T, bin string) string {
	t.Helper()
	var keep []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(dir, bin)); err == nil {
			continue
		}
		keep = append(keep, dir)
	}
	return strings.Join(keep, string(filepath.ListSeparator))
}

// checkRunOnBranch is the CheckRun the job recorded on branch for (activity, task).
func checkRunOnBranch(t *testing.T, repo, branch, activity, task string) (projectstate.CheckRun, bool) {
	t.Helper()
	raw := inRepo(t, repo, "git", "show", branch+":.aiarch/state/project.json")
	proj, _, err := projectstate.DecodeProjectJSON([]byte(raw), "")
	if err != nil {
		t.Fatal(err)
	}
	if proj.TestingState == nil {
		return projectstate.CheckRun{}, false
	}
	var found projectstate.CheckRun
	ok := false
	for _, run := range proj.TestingState.CheckRuns {
		if run.Activity == activity && run.Task == task {
			found, ok = run, true
		}
	}
	return found, ok
}

// A job task is run by the venue and by nobody else: with no claude on PATH the dispatch is still
// accepted, the run reaches a terminal phase, and the activity's branch holds the CheckRun at the
// dispatched revision naming every check of the spec, the acceptance scenarios among them. The run
// succeeds iff every check passed: a red check is the run's verdict, not a reason it never started.
func TestPump_JobTaskRunsWithoutAgent(t *testing.T) {
	repo := bornRepo(t, freshState(t))
	spec := activitySpec(t, repo, mAcceptActivityID, "integration")
	if spec.Identity.Kind != "job" {
		t.Fatalf("the spec's kind is %q, want job", spec.Identity.Kind)
	}
	t.Setenv("PATH", pathWithout(t, "claude"))
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	const revision = mAcceptActivityID + ":integration:1"
	rc := fwra.Context{Context: context.Background(), IdempotencyKey: "acceptance-job-1"}
	handle, err := access.SubmitAgenticJob(rc, agenticjob.PipelineSpec{
		ProjectID: "todomvc", ActivityID: mAcceptActivityID, Steps: []agenticjob.PipelineStep{{Name: "construct"}},
		DispatchInputs: map[string]string{
			"command": spec.Identity.Command, "component_id": mAcceptActivityID, "activity_id": mAcceptActivityID,
			"task_id": "integration", "spec_hash": spec.Hash, "revision": revision,
		},
	})
	if err != nil {
		t.Fatalf("the dispatch of a job with no claude on PATH must be accepted: %v", err)
	}
	obs := observeToTerminal(t, access, rc, handle)

	run, ok := checkRunOnBranch(t, repo, "activity/"+mAcceptActivityID, mAcceptActivityID, "integration")
	if !ok {
		t.Fatalf("the branch holds no CheckRun for %s/integration (phase %d: %s)", mAcceptActivityID, obs.Phase, obs.Diagnostic)
	}
	if run.Revision != revision {
		t.Errorf("CheckRun revision = %q, want the dispatched %q", run.Revision, revision)
	}
	recorded := map[string]bool{}
	allPassed := true
	for _, r := range run.Results {
		recorded[r.CheckId] = true
		allPassed = allPassed && r.Status == "pass"
	}
	for _, c := range spec.Checks {
		if !recorded[c.ID] {
			t.Errorf("the CheckRun names no result for the spec's check %s: %v", c.ID, slices.Collect(mapsKeys(recorded)))
		}
	}
	if got := obs.Phase == agenticjob.PhaseSucceeded; got != allPassed {
		t.Errorf("run phase %d with every check passed = %v: a job run succeeds iff every result passed", obs.Phase, allPassed)
	}
}

// observeToTerminal polls the run until it leaves running.
func observeToTerminal(t *testing.T, access agenticjob.AgenticJobAccess, rc fwra.Context, handle agenticjob.PipelineHandle) agenticjob.PipelineObservation {
	t.Helper()
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		obs, err := access.ObserveAgenticJob(rc, handle)
		if err != nil {
			t.Fatal(err)
		}
		if obs.Phase != agenticjob.PhaseRunning && obs.Phase != agenticjob.PhasePending {
			return obs
		}
		time.Sleep(time.Second)
	}
	t.Fatal("the job never reached a terminal phase")
	return agenticjob.PipelineObservation{}
}

func mapsKeys(m map[string]bool) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
