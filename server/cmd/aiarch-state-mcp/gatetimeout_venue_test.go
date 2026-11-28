package main

// gatetimeout_venue_test.go pins ISS-033-7 part B at the venue: a check gate that times out
// cancels run-checks through the venue's own discipline (agenticjob.groupCommand), and nothing
// run-checks started survives. run-checks' children lead their own process groups, so a venue that
// only SIGKILLed the run-checks pid (exec.CommandContext's default Cancel) left them running.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// slowGoTest puts a `go` first on PATH whose `test` subcommand starts a long-lived grandchild,
// records its pid (and its own) and waits on it; every other subcommand is the real go. The
// construction gate's test check therefore runs for as long as the gate lets it.
func slowGoTest(t *testing.T, gcPid, stubPid string) {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = test ]; then echo $$ > " + stubPid + "; sleep 300 & echo $! > " + gcPid + "; wait; fi\nexec " + realGo + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(filepath.ListSeparator)+os.Getenv("PATH"))
}

// A gate that outlives its bound is cancelled as a group: run-checks winds its check down on the
// SIGTERM the venue sends the group, and neither the check's process nor its grandchild is left.
func TestGateTimeoutVenue_ATimedOutGateLeavesNoDescendant(t *testing.T) {
	repo, _, _, _, spec, ag := workspaceConstruction(t)
	ag.does(t, "true")
	tmp := t.TempDir()
	gcPid, stubPid := filepath.Join(tmp, "gc.pid"), filepath.Join(tmp, "stub.pid")
	slowGoTest(t, gcPid, stubPid)
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), 10*time.Minute,
		agenticjob.WithCheckGateTimeout(90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan agenticjob.PipelineObservation, 1)
	go func() { done <- constructionOf(t, access, spec, 1) }()
	gc, stub := readPid(t, gcPid, 80*time.Second), readPid(t, stubPid, 5*time.Second)
	killOnCleanup(t, gc)
	killOnCleanup(t, stub)
	var obs agenticjob.PipelineObservation
	select {
	case obs = <-done:
	case <-time.After(3 * time.Minute):
		t.Fatal("the gate never settled after its bound")
	}
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("a gate that timed out did not fail the run: %+v", obs)
	}
	if !waitGone(gc, 15*time.Second) {
		t.Errorf("the check's grandchild (pid %d) outlived the timed-out gate", gc)
	}
	if !waitGone(stub, 15*time.Second) {
		t.Errorf("the check process (pid %d) outlived the timed-out gate", stub)
	}
}
