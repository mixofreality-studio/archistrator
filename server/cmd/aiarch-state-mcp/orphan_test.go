package main

// orphan_test.go — run-checks must not outlive its parent (ISS-033-7). It runs the venue's
// gate, which starts make → testgen → go test, each with a Temporal dev server; a parent that
// died left all of that running at ppid 1 for minutes. These tests drive the real binary.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// runChecksCmd is the ONE way a test in this package execs run-checks. The process leads its
// own group; the test's context cancel sends it SIGTERM (what a venue's shutdown sends), after
// waitDelay it is SIGKILLed, and t.Cleanup SIGKILLs the group if anything is left, so a red
// test never leaks a gate that keeps running make and go test after the test is gone.
func runChecksCmd(ctx context.Context, t *testing.T, dir string, env []string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(ctx, stateBinary(t), append([]string{"run-checks"}, args...)...)
	cmd.Dir = dir
	cmd.Env = env
	groupLeader(t, cmd)
	return cmd
}

// groupLeader makes cmd lead its own process group with the cancel/cleanup discipline above.
func groupLeader(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = waitDelay
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	})
}

// alive reports whether pid is still a process (a zombie counts until reaped).
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// waitGone polls until pid is gone, for at most bound.
func waitGone(pid int, bound time.Duration) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return !alive(pid)
}

func readPid(t *testing.T, path string, bound time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
	return 0
}

// longLivedGrandchildSpec is a spec whose one check starts a long-lived grandchild and
// records its pid, then waits on it.
func longLivedGrandchildSpec(t *testing.T, gcPid string) string {
	t.Helper()
	return writeChecksSpec(t, shCheck("command:long", "Long", "sleep 300 & echo $! > "+gcPid+"; wait"))
}

// killOnCleanup SIGKILLs pid at cleanup, so a red test leaks no survivor.
func killOnCleanup(t *testing.T, pid int) {
	t.Helper()
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
}

// TestRunChecksParentDeathStopsTheGate: SIGKILL only the parent of run-checks; run-checks and
// the grandchild its check started must both go.
func TestRunChecksParentDeathStopsTheGate(t *testing.T) {
	tmp := t.TempDir()
	gcPid, rcPid := filepath.Join(tmp, "gc.pid"), filepath.Join(tmp, "rcpid")
	spec := longLivedGrandchildSpec(t, gcPid)
	out := filepath.Join(tmp, "res")
	script := `"$BIN" run-checks --gate --spec "$SPEC" --out "$OUT" >/dev/null 2>&1 & echo $! > "$RCPID"; wait`
	parent := exec.CommandContext(t.Context(), "sh", "-c", script)
	parent.Dir = tmp
	parent.Env = append(os.Environ(), "BIN="+stateBinary(t), "SPEC="+spec, "OUT="+out, "RCPID="+rcPid, "GOWORK=off")
	groupLeader(t, parent)
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = parent.Wait() }()
	rc := readPid(t, rcPid, 15*time.Second)
	gc := readPid(t, gcPid, 15*time.Second)
	killOnCleanup(t, rc)
	killOnCleanup(t, gc)
	if err := syscall.Kill(parent.Process.Pid, syscall.SIGKILL); err != nil { // only the parent
		t.Fatal(err)
	}
	if !waitGone(rc, 15*time.Second) {
		t.Errorf("run-checks (pid %d) outlived its dead parent", rc)
	}
	if !waitGone(gc, 15*time.Second) {
		t.Errorf("the check's grandchild (pid %d) outlived its dead parent", gc)
	}
}

// TestRunChecksOrphanTimeoutHandlesSIGTERM: the context's cancel SIGTERMs run-checks; it must
// handle that itself (exit code 1, results written, a cancelled check never a pass) inside
// waitDelay, and the grandchild must go.
func TestRunChecksOrphanTimeoutHandlesSIGTERM(t *testing.T) {
	tmp := t.TempDir()
	gcPid := filepath.Join(tmp, "gc.pid")
	spec := longLivedGrandchildSpec(t, gcPid)
	out := filepath.Join(tmp, "res")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	cmd := runChecksCmd(ctx, t, tmp, append(os.Environ(), "GOWORK=off"), "--gate", "--spec", spec, "--out", out)
	log, err := os.Create(filepath.Join(tmp, "rc.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	cmd.Stdout, cmd.Stderr = log, log // files, not pipes: nothing for a straggler to hold
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	gc := readPid(t, gcPid, 15*time.Second)
	killOnCleanup(t, gc)
	err = cmd.Wait()
	elapsed := time.Since(start)
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("want a non-zero exit, got %v", err)
	}
	if elapsed > 3*time.Second+waitDelay-2*time.Second {
		t.Errorf("run-checks took %v to stop: it did not handle SIGTERM within waitDelay", elapsed)
	}
	if code := ee.ExitCode(); code != 1 {
		t.Errorf("exit code = %d, want 1 (SIGTERM handled, not killed)", code)
	}
	if !waitGone(gc, 5*time.Second) {
		t.Errorf("the check's grandchild (pid %d) survived the cancel", gc)
	}
	for _, r := range readResultsFile(t, out) {
		if r.Status == checkPass {
			t.Errorf("cancelled check %s recorded as passed", r.CheckId)
		}
	}
}

// TestWatchParentCancelsOnlyWhenThePpidChanges: the watcher fires when the parent pid changes,
// not while it is stable (a start at ppid 1 included), and it stops when the context is done.
func TestWatchParentCancelsOnlyWhenThePpidChanges(t *testing.T) {
	const every = 5 * time.Millisecond
	for _, start := range []int{4242, 1} {
		var ppid atomicInt
		ppid.set(start)
		ctx, cancel := context.WithCancel(t.Context())
		fired := make(chan struct{})
		done := make(chan struct{})
		go func() {
			watchParent(ctx, func() { close(fired); cancel() }, ppid.get, every)
			close(done)
		}()
		select {
		case <-fired:
			t.Fatalf("start ppid %d: cancelled while the ppid was stable", start)
		case <-time.After(20 * every):
		}
		ppid.set(1 + start%7 + 1) // a different pid
		select {
		case <-fired:
		case <-time.After(5 * time.Second):
			t.Fatalf("start ppid %d: not cancelled after the ppid changed", start)
		}
		<-done
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		watchParent(ctx, func() { t.Error("cancelled by a stable ppid") }, func() int { return 7 }, every)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher kept running after its context was done")
	}
}

type atomicInt struct{ v atomic.Int64 }

func (a *atomicInt) set(n int) { a.v.Store(int64(n)) }
func (a *atomicInt) get() int  { return int(a.v.Load()) }
