package main

// temporal.go resolves a reachable Temporal server for the local stack.
// cmd/server's boot walk (main.gen.go) DIALS Temporal synchronously —
// verified empirically (a local boot with nothing listening on
// ARCHISTRATOR_TEMPORAL_HOSTPORT fails fast with a gRPC dial error before
// "http server listening") — so `archistrator serve` cannot hand it an
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
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

const temporalReadyTimeout = 20 * time.Second

// localTemporalNamespace is the local stack's IDENTITY seam on whatever
// Temporal it talks to (QA 2026-07-19, poll-404 wizard reset). Every stray
// `temporal server start-dev` carries "default", so running there is
// indistinguishable from running against a foreign backend — and a foreign
// backend answers every session lookup "workflow not found", which the API
// used to relay as an authoritative 404 that reset the SPA wizard. A
// dedicated namespace makes the foreign case a typed NamespaceNotFound
// (Infrastructure — the polling client tolerates it and self-heals).
const localTemporalNamespace = "archistrator-local"

// ensureTemporal returns a Temporal frontend hostport that is reachable AND
// carries localTemporalNamespace by the time it returns: hostport itself, if
// already reachable and identity-verified; otherwise a freshly spawned
// `temporal server start-dev --headless` on hostport (with the namespace
// pre-created and a persistent DB file, so a restart does not vaporize every
// in-flight design session the SPA is polling), if the `temporal` CLI is on
// PATH. cleanup is always non-nil and safe to call exactly once; it is a
// no-op when nothing was spawned.
func ensureTemporal(ctx context.Context, hostport string, logOut io.Writer) (cleanup func(), err error) {
	noop := func() {}
	if tcpReachable(hostport, 300*time.Millisecond) {
		// Adopt-path identity check: something is already listening — only
		// adopt it if it is (or was pointed at as) archistrator's Temporal.
		if err := verifyTemporalNamespace(ctx, hostport, temporalReadyTimeout); err != nil {
			return noop, err
		}
		return noop, nil
	}

	temporalBin, lookErr := exec.LookPath("temporal")
	if lookErr != nil {
		return noop, fmt.Errorf(
			"cannot reach Temporal at %s, and the `temporal` CLI is not on PATH to start a local dev server "+
				"(install it — e.g. `brew install temporal` — or point ARCHISTRATOR_TEMPORAL_HOSTPORT at an already-running one): %w",
			hostport, lookErr)
	}

	host, port, splitErr := net.SplitHostPort(hostport)
	if splitErr != nil {
		return noop, fmt.Errorf("ARCHISTRATOR_TEMPORAL_HOSTPORT %q: %w", hostport, splitErr)
	}

	dbFile, err := localTemporalDBFile()
	if err != nil {
		return noop, err
	}

	//nolint:gosec // resolved trusted binary + fixed args
	cmd := exec.CommandContext(ctx, temporalBin, temporalStartDevArgs(host, port, dbFile)...)
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
	// The --namespace pre-creation completes during boot; confirm it is
	// answerable before handing the hostport to the child, so the child's
	// first calls never race the namespace registration.
	if err := verifyTemporalNamespace(ctx, hostport, temporalReadyTimeout); err != nil {
		cleanupFn()
		return noop, err
	}
	_, _ = fmt.Fprintf(logOut, "embedded Temporal dev-server ready at %s (namespace %s, db %s)\n", hostport, localTemporalNamespace, dbFile)
	return cleanupFn, nil
}

// temporalStartDevArgs builds the `temporal` CLI argv (after the binary) for
// the local stack's dev server: headless, loopback, the stack's dedicated
// namespace pre-created, and a PERSISTENT SQLite file so design sessions
// survive a dev-server restart.
func temporalStartDevArgs(host, port, dbFile string) []string {
	return []string{
		"server", "start-dev",
		"--headless",
		"--ip", host,
		"--port", port,
		"--namespace", localTemporalNamespace,
		"--db-filename", dbFile,
		"--log-format", "json",
	}
}

// localTemporalDBFile resolves (and ensures the parent directory of) the
// machine-wide persistent dev-server database: ~/.archistrator/temporal.db.
// Machine-wide is coherent with the one-stack-per-machine singleton guard
// (serve.go); the durable design state itself lives in git — this DB only
// carries the in-flight session workflows.
func localTemporalDBFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir for the local Temporal DB: %w", err)
	}
	dir := filepath.Join(home, ".archistrator")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return filepath.Join(dir, "temporal.db"), nil
}

// verifyTemporalNamespace confirms the Temporal at hostport carries the local
// stack's namespace, polling (bounded by timeout) so a just-booted dev server
// finishing its namespace registration passes. A definitive
// NamespaceNotFound after the deadline names the likely cause — another
// tool's dev server squatting the port — instead of silently adopting a
// foreign backend.
func verifyTemporalNamespace(ctx context.Context, hostport string, timeout time.Duration) error {
	nc, err := client.NewNamespaceClient(client.Options{HostPort: hostport})
	if err != nil {
		return fmt.Errorf("connect to Temporal at %s to verify namespace %q: %w", hostport, localTemporalNamespace, err)
	}
	defer nc.Close()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, lastErr = nc.Describe(ctx, localTemporalNamespace)
		if lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	var nsNotFound *serviceerror.NamespaceNotFound
	if errors.As(lastErr, &nsNotFound) {
		return fmt.Errorf(
			"a Temporal server is listening at %s but does not carry archistrator's namespace %q — it is probably another tool's dev server on this port; "+
				"stop it, or point ARCHISTRATOR_TEMPORAL_HOSTPORT at archistrator's own Temporal",
			hostport, localTemporalNamespace)
	}
	return fmt.Errorf("verify Temporal namespace %q at %s: %w", localTemporalNamespace, hostport, lastErr)
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
// already-running shared dev Temporal), else loopback on archistrator's OWN
// dedicated port — NOT the well-known 7233 (QA 2026-07-19): other tooling's
// `temporal server start-dev` (the systemtests suite on this repo, or any
// unrelated project) defaults to 7233, and adopting a foreign dev server
// there made every session lookup answer "workflow not found" → an
// authoritative 404 that reset the SPA wizard mid-use-case.
func defaultTemporalHostport() string {
	if v := strings.TrimSpace(os.Getenv("ARCHISTRATOR_TEMPORAL_HOSTPORT")); v != "" {
		return v
	}
	return "127.0.0.1:7943"
}
