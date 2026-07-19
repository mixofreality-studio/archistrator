package main

// temporal.go resolves a reachable Temporal server for the local stack.
// cmd/server's boot walk (main.gen.go) DIALS Temporal synchronously —
// verified empirically (a local boot with nothing listening on
// ARCHISTRATOR_TEMPORAL_HOSTPORT fails fast with a gRPC dial error before
// "http server listening") — so `archistrator mcp` cannot hand it an
// unreachable hostport and hope for the best.
//
// SCOPE NOTE (documented, not silently punted): a TRULY embedded
// Temporal-as-a-library dev server (what `temporal server start-dev`
// programmatically wraps, go.temporal.io/server/temporal.NewServer) is a
// substantial new dependency (persistence store, dynamic config, the
// frontend/history/matching services) that does not exist anywhere in this
// module today and is realistically its own task, not a Task-5 increment.
// This file instead spawns the OFFICIAL `temporal` CLI's `server start-dev`
// as a managed subprocess IF ARCHISTRATOR_TEMPORAL_HOSTPORT is already
// unreachable and `temporal` is on PATH — the same "managed subprocess,
// SIGTERM'd on shutdown" shape Task 6's local construction executor will use
// for headless claude. This is a KNOWN GAP against the plan's "zero external
// dependencies... nothing installed but git and claude" thesis: local
// construction genuinely needs a THIRD tool today (the `temporal` CLI). See
// this task's report for the residual writeup and the follow-up this implies
// (embed go.temporal.io/server as a library, or vendor the temporal binary
// via the build script) — out of scope here.
import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

const temporalReadyTimeout = 20 * time.Second

// ensureTemporal returns a Temporal frontend hostport that is reachable by
// the time it returns: hostport itself, if already reachable; otherwise a
// freshly spawned `temporal server start-dev --headless` on hostport, if the
// `temporal` CLI is on PATH. cleanup is always non-nil and safe to call
// exactly once; it is a no-op when nothing was spawned.
func ensureTemporal(ctx context.Context, hostport string, logOut io.Writer) (cleanup func(), err error) {
	noop := func() {}
	if tcpReachable(hostport, 300*time.Millisecond) {
		return noop, nil
	}

	temporalBin, lookErr := exec.LookPath("temporal")
	if lookErr != nil {
		return noop, fmt.Errorf(
			"Temporal is not reachable at %s and the `temporal` CLI is not on PATH to start a local dev server "+
				"(install it — e.g. `brew install temporal` — or point ARCHISTRATOR_TEMPORAL_HOSTPORT at an already-running one): %w",
			hostport, lookErr)
	}

	host, port, splitErr := net.SplitHostPort(hostport)
	if splitErr != nil {
		return noop, fmt.Errorf("ARCHISTRATOR_TEMPORAL_HOSTPORT %q: %w", hostport, splitErr)
	}

	cmd := exec.CommandContext(ctx, temporalBin, "server", "start-dev", //nolint:gosec // resolved trusted binary + fixed args
		"--headless", "--ip", host, "--port", port, "--log-format", "json")
	cmd.Stdout = logOut
	cmd.Stderr = logOut
	if err := cmd.Start(); err != nil {
		return noop, fmt.Errorf("start `temporal server start-dev`: %w", err)
	}

	cleanupFn := func() { _ = stopProcess(cmd, 5*time.Second) }

	if err := waitTCPReachable(ctx, hostport, temporalReadyTimeout); err != nil {
		cleanupFn()
		return noop, fmt.Errorf("embedded Temporal dev-server did not become reachable at %s within %s: %w", hostport, temporalReadyTimeout, err)
	}
	fmt.Fprintf(logOut, "embedded Temporal dev-server ready at %s\n", hostport)
	return cleanupFn, nil
}

func tcpReachable(hostport string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", hostport, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitTCPReachable(ctx context.Context, hostport string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if tcpReachable(hostport, 500*time.Millisecond) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// defaultTemporalHostport resolves the hostport to target: an explicit
// override (ARCHISTRATOR_TEMPORAL_HOSTPORT, so an operator can point at an
// already-running shared dev Temporal), else loopback on the standard
// front-end gRPC port.
func defaultTemporalHostport() string {
	if v := strings.TrimSpace(os.Getenv("ARCHISTRATOR_TEMPORAL_HOSTPORT")); v != "" {
		return v
	}
	return "127.0.0.1:7233"
}
