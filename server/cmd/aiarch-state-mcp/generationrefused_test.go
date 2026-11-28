package main

// generationrefused_test.go pins RUN-034 (ISS-034-3): a job whose generation step refuses is a gate
// fault with a named cause, not "unknown", and its attempt carries a stable job-input fingerprint so
// a re-dispatch over the unchanged branch is seen to be the same failure.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// refusingGenTests puts a make on PATH whose gen-tests target refuses with refusalMessage; every
// other target still runs the real make.
const refusalMessage = "test plan: binding: unknown field \"as\""

func refusingGenTests(t *testing.T) {
	t.Helper()
	realMake, err := exec.LookPath("make")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	stub := "#!/bin/sh\nif [ \"$1\" = gen-tests ]; then echo '" + refusalMessage + "' >&2; exit 2; fi\nexec " + realMake + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "make"), []byte(stub), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+pathWithout(t, "claude"))
}

// runRefusedJob dispatches the acceptance job task over repo and returns its terminal observation.
func runRefusedJob(t *testing.T, repo, key string) agenticjob.PipelineObservation {
	t.Helper()
	spec := activitySpec(t, repo, mAcceptActivityID, "integration")
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rc := fwra.Context{Context: context.Background(), IdempotencyKey: fwra.IdempotencyKey(key)}
	handle, err := access.SubmitAgenticJob(rc, agenticjob.PipelineSpec{
		ProjectID: "todomvc", ActivityID: mAcceptActivityID, Steps: []agenticjob.PipelineStep{{Name: "construct"}},
		DispatchInputs: map[string]string{
			"command": spec.Identity.Command, "component_id": mAcceptActivityID, "activity_id": mAcceptActivityID,
			"task_id": "integration", "spec_hash": spec.Hash, "revision": mAcceptActivityID + ":integration:1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return observeToTerminal(t, access, rc, handle)
}

// A job whose generation refuses fails with GenerationRefused naming the refusing step and holding its
// message, classed a gate fault, with a progress fingerprint and the CheckRun still on the branch; the
// same job over the unchanged branch yields the same fingerprint.
func TestJob_GenerationRefusedIsNamedAndFingerprinted(t *testing.T) {
	repo := bornRepo(t, freshState(t))
	refusingGenTests(t)

	first := runRefusedJob(t, repo, "generation-refused-1")
	if first.Phase != agenticjob.PhaseFailed {
		t.Fatalf("phase = %d (%s), want failed", first.Phase, first.Diagnostic)
	}
	refused, ok := causeOf(first).(*agenticjob.GenerationRefused)
	if !ok {
		t.Fatalf("cause = %T, want *GenerationRefused (%s)", causeOf(first), first.Diagnostic)
	}
	if refused.Generator != "make gen-tests" || !strings.Contains(refused.Detail, refusalMessage) {
		t.Errorf("cause = %+v, want generator %q and a detail holding %q", refused, "make gen-tests", refusalMessage)
	}
	if first.FaultClass != agenticjob.FaultGate {
		t.Errorf("fault class = %q, want gate", first.FaultClass)
	}
	if first.ProgressFingerprint == nil || *first.ProgressFingerprint == "" {
		t.Fatal("a refused job carries no progress fingerprint")
	}
	if _, ok := checkRunOnBranch(t, repo, "activity/"+mAcceptActivityID, mAcceptActivityID, "integration"); !ok {
		t.Error("the branch holds no CheckRun for the refused job")
	}

	second := runRefusedJob(t, repo, "generation-refused-2")
	if second.ProgressFingerprint == nil || *second.ProgressFingerprint != *first.ProgressFingerprint {
		t.Errorf("a second run over the unchanged branch fingerprinted %v, want the first's %q", second.ProgressFingerprint, *first.ProgressFingerprint)
	}
}

// The attempt's cause crosses the state document as JSON: GenerationRefused must survive unchanged.
func TestFailureCause_GenerationRefusedRoundTrips(t *testing.T) {
	var in projectstate.FailureCause = &projectstate.GenerationRefused{Generator: "go tool appgen clients", Detail: refusalMessage}
	raw, err := projectstate.MarshalFailureCause(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"generationRefused"`) {
		t.Errorf("wire form %s does not carry the generationRefused kind", raw)
	}
	out, err := projectstate.UnmarshalFailureCause(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip changed the cause: %+v -> %+v", in, out)
	}
	attempt := projectstate.TaskAttempt{Cause: &in}
	data, err := attempt.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var back projectstate.TaskAttempt
	if err := back.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	if back.Cause == nil || !reflect.DeepEqual(*back.Cause, in) {
		t.Errorf("TaskAttempt round trip changed the cause: %+v", back.Cause)
	}
}
