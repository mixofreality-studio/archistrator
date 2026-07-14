package harness

// Cross-test workflow-leak containment.
//
// Every harness server registers its Managers' Temporal workers on FIXED per-Manager
// task-queue names (system-design/project-design/construction/...), and — in CI, where
// ARCHISTRATOR_TEMPORAL_NAMESPACE is pinned — the whole suite runs in ONE process against
// ONE shared namespace. A design/construction workflow one test starts but does NOT drive
// to a terminal state stays OPEN on the shared dev server, retrying its activities forever
// (its own fake is gone, so the retries hit a dead port). When a LATER test boots its
// server, that server's worker polls the SAME task queue and can pick the leaked
// workflow's activity up, executing it against the LATER test's fake — cross-test bleed
// that has been observed to corrupt a later test's dispatch log (a "dispatch 0" belonging
// to an earlier test) and to spam the run with TMPRL1100 stale-history panics.
//
// The task queues are compile-time constants in the server (no per-test seam to make them
// unique without re-architecting codegen), so isolation is not available cheaply. Instead
// each server, on teardown, TERMINATES the namespace's still-running workflows — the ones
// it (or an earlier test) leaked — so the next test starts from a clean slate. This is
// safe because the systemtests run sequentially (no t.Parallel) in a namespace dedicated
// to the run: by a server's cleanup time its own assertions are done, and any workflow
// still Running is a leak with nothing left to prove.
//
// Best-effort and CLI-driven (the `temporal` dev server ships the CLI, the same tool
// namespace.go uses): when the CLI is absent or the call fails, we log and move on rather
// than fail the suite — the scoped assertion in agentic_github.go is the deterministic
// backstop.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// TerminateRunningWorkflows terminates every Running workflow execution in the namespace
// on the shared Temporal dev server — leak containment run from each server's teardown so
// a workflow one test failed to drain cannot bleed onto a later test's worker (see the
// file comment). Best-effort: a missing CLI or a failed call is logged, never fatal.
func TerminateRunningWorkflows(hostPort, namespace string) {
	if hostPort == "" || namespace == "" {
		return
	}
	if _, err := exec.LookPath("temporal"); err != nil {
		return // no CLI — the scoped dispatch assertion is the deterministic backstop.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Batch-terminate every open execution via a visibility query. --yes suppresses the
	// interactive confirmation; the reason lands in each terminated history.
	cmd := exec.CommandContext(ctx, "temporal", "workflow", "terminate",
		"--address", hostPort,
		"--namespace", namespace,
		"--query", `ExecutionStatus="Running"`,
		"--reason", "systemtests cross-test leak containment",
		"--yes",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "harness: terminate running workflows in %q failed (best-effort): %v: %s\n",
			namespace, err, out)
	}
}
