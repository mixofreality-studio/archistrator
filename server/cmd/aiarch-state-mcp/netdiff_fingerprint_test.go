package main

// netdiff_fingerprint_test.go pins ISS-033-6 part 3: the progress fingerprint is over the agent's
// own net diff against main, so the walk's refresh-from-main merge between two attempts that left
// the same output does not make them look different (RUN-033 stp attempts 2-11 never settled as
// noProgress because every one followed an "aiarch: refresh activity from main" merge).

import (
	"context"
	"strconv"
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// branchSpec is the srs task spec the job will resolve: from the state the activity branch holds
// once an earlier attempt committed to it, else from main's.
func branchSpec(t *testing.T, repo string, attempt int) review.TaskSpec {
	t.Helper()
	if attempt == 1 {
		return activitySpec(t, repo, freshActivity, "srs")
	}
	raw := inRepo(t, repo, "git", "show", "activity/"+freshActivity+":.aiarch/state/project.json")
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON([]byte(raw), anyProject)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	spec, err := deriveTaskSpec(&proj, freshActivity, "srs")
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

// submitSRSAttempt dispatches attempt n of the srs task over repo with the scripted agent and
// returns its terminal observation.
func submitSRSAttempt(t *testing.T, access agenticjob.AgenticJobAccess, repo string, n int) agenticjob.PipelineObservation {
	t.Helper()
	spec := branchSpec(t, repo, n)
	rc := fwra.Context{Context: context.Background(), IdempotencyKey: fwra.IdempotencyKey(t.Name() + "-" + strconv.Itoa(n))}
	handle, err := access.SubmitAgenticJob(rc, agenticjob.PipelineSpec{
		ProjectID: "todomvc", ActivityID: freshActivity, Steps: []agenticjob.PipelineStep{{Name: "construct"}},
		DispatchInputs: map[string]string{
			"command": spec.Identity.Command, "component_id": freshComponent, "activity_id": freshActivity,
			"task_id": "srs", "spec_hash": spec.Hash, "revision": freshActivity + ":srs:" + strconv.Itoa(n),
		},
	})
	if err != nil {
		t.Fatalf("submit attempt %d: %v", n, err)
	}
	return observeToTerminal(t, access, rc, handle)
}

// Two attempts that leave the same red output, with main moved and merged into the branch between
// them, carry the same fingerprint: it hashes what the agent changed against main, not the whole
// tree the refresh merge reshaped.
func TestAgenticJob_FingerprintIgnoresARefreshFromMain(t *testing.T) {
	repo := bornRepo(t, freshState(t))
	fakeClaude{breakState: true, stdout: streamReached, exit: 0}.install(t)
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Attempt 1 leaves the red output on the branch; attempt 2 re-affirms it, so its spec is
	// hashed over the state the branch now holds, as every later attempt's is.
	if obs := submitSRSAttempt(t, access, repo, 1); obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("attempt 1: phase %d (%s), want a red run", obs.Phase, obs.Diagnostic)
	}
	first := submitSRSAttempt(t, access, repo, 2)
	if first.Phase != agenticjob.PhaseFailed || first.ProgressFingerprint == nil || *first.ProgressFingerprint == "" {
		t.Fatalf("attempt 2: phase %d (%s), fingerprint %v: want a red run with a fingerprint", first.Phase, first.Diagnostic, first.ProgressFingerprint)
	}

	// Main moves on with work the agent never touched, and the walk's refresh merges it in.
	writeRepoFile(t, repo, "docs/unrelated-main-change.md", []byte("someone else's work\n"))
	inRepo(t, repo, "git", "add", "docs/unrelated-main-change.md")
	inRepo(t, repo, "git", "-c", "user.name=other", "-c", "user.email=other@example.com", "commit", "-q", "-m", "main moves on")
	rc := fwra.Context{Context: context.Background(), IdempotencyKey: fwra.IdempotencyKey(t.Name() + "-refresh")}
	refresh, err := access.SubmitAgenticJob(rc, agenticjob.PipelineSpec{
		ProjectID: "todomvc", ActivityID: freshActivity, Steps: []agenticjob.PipelineStep{{Name: "refresh"}},
		DispatchInputs: map[string]string{
			agenticjob.DispatchInputJobKey: agenticjob.DispatchJobRefresh, "activity_id": freshActivity,
		},
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if obs := observeToTerminal(t, access, rc, refresh); obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("the refresh did not succeed: phase %d (%s)", obs.Phase, obs.Diagnostic)
	}

	second := submitSRSAttempt(t, access, repo, 3)
	if second.ProgressFingerprint == nil || *second.ProgressFingerprint == "" {
		t.Fatalf("attempt 3: phase %d (%s), no fingerprint", second.Phase, second.Diagnostic)
	}
	if *first.ProgressFingerprint != *second.ProgressFingerprint {
		t.Errorf("the fingerprint changed (%s then %s) though the agent's output did not: the refresh from main leaked in",
			*first.ProgressFingerprint, *second.ProgressFingerprint)
	}
}
