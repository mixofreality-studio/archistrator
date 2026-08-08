package main

// serve_test.go covers the amended (2026-07-19, "standalone serve, drop
// Serena pattern") `archistrator serve` daemon: it boots the managed local
// stack over plain HTTP — no stdio transport anymore, the spawned
// archistrator-server child's own /mcp mount is the ONLY MCP surface — and a
// SIGTERM must cleanly reap every process it started (server child, and the
// Temporal dev-server if one was spawned).
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// requireTemporalCLI skips tests that need a real local Temporal dev-server
// when the `temporal` CLI is not on PATH — see temporal.go's package doc for
// why this is a known, documented gap (no embedded-as-library Temporal exists
// yet) rather than something this test works around.
func requireTemporalCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("temporal"); err != nil {
		t.Skip("temporal CLI not on PATH — see temporal.go's package doc; skipping the live local-stack test")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// buildBinaries compiles ./cmd/archistrator and ./cmd/server into dir once
// per test run and returns their paths, laid out side by side exactly as
// locateServerBinary's sibling-discovery expects.
func buildBinaries(t *testing.T) (archistratorBin, serverBin string) {
	t.Helper()
	dir := t.TempDir()
	archistratorBin = filepath.Join(dir, "archistrator")
	serverBin = filepath.Join(dir, "archistrator-server")

	// module root is the parent of this package's directory (cmd/archistrator).
	moduleRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}

	build := func(out, pkg string) {
		cmd := exec.Command("go", "build", "-o", out, pkg)
		cmd.Dir = moduleRoot
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", pkg, err, b)
		}
	}
	build(archistratorBin, "./cmd/archistrator")
	build(serverBin, "./cmd/server")
	return archistratorBin, serverBin
}

// freePort asks the OS for an unused loopback TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// childPIDs lists the direct OS children of parentPID via `pgrep -P` — used
// to find (and later confirm the death of) whatever archistrator-server
// (and, if spawned, the Temporal dev-server) `archistrator serve` starts as
// its own child processes. A pgrep exit of "no matches" is not a test
// failure; it just means no children were found (yet, or anymore).
func childPIDs(parentPID int) []int {
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(parentPID)).Output()
	if err != nil {
		return nil
	}
	var pids []int
	for line := range strings.FieldsSeq(strings.TrimSpace(string(out))) {
		if pid, err := strconv.Atoi(line); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// processAlive reports whether pid is still a live process, via a signal-0
// probe (the standard Unix "is this pid alive" check — sends no actual
// signal, just tests deliverability).
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// TestServe_BootsStack_AnswersMCPOverHTTP_And_SIGTERMLeavesNoOrphans is the
// amendment's live-shape test: `archistrator serve` boots the managed stack
// (server child + Temporal), serves the SPA + /healthz + /mcp over plain
// HTTP (no stdio transport — the child's own /mcp mount is the only MCP
// surface now), and a SIGTERM cleanly reaps every child process it spawned
// within a bounded window — the "Ctrl-C leaves zero orphans" gate.
// assertLiveChildren asserts the serve process actually spawned its
// archistrator-server child and that every pid pgrep reports is really alive.
// Returns the pids, which the orphan check re-uses after shutdown.
func assertLiveChildren(t *testing.T, parentPID int) []int {
	t.Helper()
	before := childPIDs(parentPID)
	if len(before) == 0 {
		t.Fatal("expected archistrator serve to have spawned at least one child process (archistrator-server) by the time it is healthy")
	}
	for _, pid := range before {
		if !processAlive(pid) {
			t.Fatalf("child pid %d reported by pgrep is not actually alive", pid)
		}
	}
	return before
}

// assertMCPInitializes drives an MCP initialize over HTTP — the ONLY MCP surface
// now (no stdio bridge).
func assertMCPInitializes(t *testing.T, addr string, stderr *bytes.Buffer) {
	t.Helper()
	mcpCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-driver", Version: "0.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp"}
	cs, err := client.Connect(mcpCtx, transport, nil)
	if err != nil {
		t.Fatalf("MCP initialize over HTTP at http://%s/mcp: %v\nstderr so far:\n%s", addr, err, stderr.String())
	}
	if cs.InitializeResult() == nil {
		t.Fatal("MCP session has no InitializeResult after Connect — initialize did not complete")
	}
	_ = cs.Close()
}

// assertUserInfoIsDevPrincipal pins the local-profile auth contract end-to-end.
//
// GET /api/userinfo is the SPA's FIRST call on load, and the one that surfaced as
// "Failed to load user info: 500" (QA 2026-07-19) when a stack came up without a
// working auth composition: the serve child-spawn env defaults
// ARCHISTRATOR_AUTH_DEV_MODE=true (loopback-only, serverchild.go), cmd/server's
// auth middleware injects the dev principal, and the probe answers identity JSON
// — never a 401/500 on a fresh boot.
func assertUserInfoIsDevPrincipal(t *testing.T, addr string, stderr *bytes.Buffer) {
	t.Helper()
	uiResp, err := http.Get("http://" + addr + "/api/userinfo")
	if err != nil {
		t.Fatalf("GET /api/userinfo: %v", err)
	}
	uiBody, readErr := io.ReadAll(uiResp.Body)
	_ = uiResp.Body.Close()
	if readErr != nil {
		t.Fatalf("read /api/userinfo body: %v", readErr)
	}
	if uiResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/userinfo = %d, want 200 (dev principal); body: %s\nstderr so far:\n%s", uiResp.StatusCode, uiBody, stderr.String())
	}
	var userInfo struct {
		Kind string `json:"kind"`
		Sub  string `json:"sub"`
	}
	if err := json.Unmarshal(uiBody, &userInfo); err != nil {
		t.Fatalf("decode /api/userinfo body %q: %v", uiBody, err)
	}
	// "dev-architect" is cmd/server's devPrincipal() default subject
	// (ARCHISTRATOR_DEV_SUBJECT unset — this test sets no such env).
	if userInfo.Kind != "user" || userInfo.Sub != "dev-architect" {
		t.Fatalf("/api/userinfo principal = kind %q sub %q, want the dev principal (kind %q, sub %q); body: %s", userInfo.Kind, userInfo.Sub, "user", "dev-architect", uiBody)
	}
}

// assertHTTPSurfaceAnswers proves the listener this test cares about — the
// standalone daemon's HTTP mount — is genuinely up.
//
// It deliberately does NOT assert "/" returns 200: buildBinaries builds
// archistrator-server WITHOUT -tags localdist (no embedded webApp dist to stage),
// so mountSPA's documented no-op arm correctly 404s here (see cmd/server/
// spa_handler_test.go's TestMountSPANoOpWhenBuiltWithoutLocaldistTag) — SPA-serving
// itself is Task 4's own gate, exercised for real by scripts/build-local.sh's
// -tags localdist build, not by this unit-scoped process test.
func assertHTTPSurfaceAnswers(t *testing.T, addr string) {
	t.Helper()
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	_ = resp.Body.Close()
}

func TestServe_BootsStack_AnswersMCPOverHTTP_And_SIGTERMLeavesNoOrphans(t *testing.T) {
	requireTemporalCLI(t)

	archistratorBin, _ := buildBinaries(t)
	projectDir := t.TempDir()

	var initOut bytes.Buffer
	if err := RunInit(projectDir, &initOut); err != nil {
		t.Fatalf("RunInit: %v", err)
	}

	port := freePort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Deliberately NOT exec.CommandContext: binding this process to a ctx
	// whose CancelFunc fires via `defer` at test-function-return would race
	// the graceful shutdown below with an immediate Kill — see the
	// (superseded) mcpserve_test.go this file replaces for the reproduction
	// history. The explicit SIGTERM + bounded Wait below is the deliberate
	// shutdown path under test.
	cmd := exec.Command(archistratorBin, "serve", "--port", strconv.Itoa(port), "--skip-auth-check")
	cmd.Dir = projectDir
	// This test's scope is the standalone daemon boot/shutdown shape, not the
	// local construction executor (Task 6) — buildBinaries stages only
	// archistrator + archistrator-server, no aiarch-state-mcp sibling, so the
	// spawned archistrator-server must be told to dry-run construction (see
	// serverchild.go's env() doc for why this is a pass-through, not forced).
	cmd.Env = append(os.Environ(), "ARCHISTRATOR_CONSTRUCTION_DRYRUN=true")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start archistrator serve: %v", err)
	}
	reaped := false
	t.Cleanup(func() {
		// Graceful-first, even on an early test failure that never reaches the
		// deliberate SIGTERM step below: a bare Kill() here would SIGKILL
		// "archistrator serve" without letting IT gracefully SIGTERM its own
		// archistrator-server child, orphaning it — exactly the failure mode
		// this test exists to catch, so the safety-net cleanup must not
		// introduce it.
		if !reaped {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		}
		if testing.Verbose() || t.Failed() {
			t.Logf("archistrator serve stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		}
	})

	if err := waitHealthy(context.Background(), addr, 60*time.Second); err != nil {
		t.Fatalf("HTTP listener never became healthy: %v\nstderr so far:\n%s", err, stderr.String())
	}

	before := assertLiveChildren(t, cmd.Process.Pid)

	assertMCPInitializes(t, addr, &stderr)

	assertUserInfoIsDevPrincipal(t, addr, &stderr)
	assertHTTPSurfaceAnswers(t, addr)

	// Ctrl-C: SIGTERM the parent, confirm a clean (nil-error) exit within a
	// bounded window.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		reaped = true
		if err != nil {
			t.Fatalf("archistrator serve exited with error after SIGTERM: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("archistrator serve did not exit within 20s of SIGTERM\nstderr:\n%s", stderr.String())
	}

	assertNoOrphansLeft(t, before, addr, &stderr)
}

// assertNoOrphansLeft is the point of this test: every child pid observed while
// the stack was healthy must be gone within a bounded window of the parent's
// SIGTERM, and the port must be released. An orphaned archistrator-server is the
// exact failure this test exists to catch.
func assertNoOrphansLeft(t *testing.T, before []int, addr string, stderr *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for _, pid := range before {
		for processAlive(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("child pid %d still alive 10s after SIGTERM — orphaned\nstderr:\n%s", pid, stderr.String())
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if portInUse(addr) {
		t.Fatalf("port %s still in use after shutdown", addr)
	}
}

// TestRunServe_SingletonGuard_RefusesSecondInstance covers the v1 singleton
// guard: RunServe against a port already answering refuses immediately with
// a clear, "one instance per project" error BEFORE touching anything (no
// child spawned, no Temporal contacted) — proven here with a plain HTTP
// listener standing in for "a live archistrator instance", since the guard
// only checks reachability, not identity (documented v1 scope).
func TestRunServe_SingletonGuard_RefusesSecondInstance(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port

	opts := serveOptions{
		Dir:           t.TempDir(),
		Port:          port,
		SkipAuthCheck: true,
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
	}
	logger := testLogger(t)

	err = RunServe(context.Background(), opts, logger)
	if err == nil {
		t.Fatal("expected RunServe to refuse a second instance, got nil error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("error = %q, want it to mention 'already running'", err.Error())
	}
}

// TestRunServe_ErrorsWhenServerBinaryMissing covers the amendment's replaced
// error handling: with no stdio MCP session left to "degrade" into, a Step
// 3-5 startup failure (here: the archistrator-server binary cannot be
// located) is now a hard, synchronous error return from RunServe — main.go
// prints it to stderr and exits non-zero. git+claude are shimmed present (so
// preflight itself is non-fatal) to isolate this test to the binary-missing
// failure specifically.
func TestRunServe_ErrorsWhenServerBinaryMissing(t *testing.T) {
	installShim(t, "git", "#!/bin/sh\nexit 0\n")
	installShim(t, "claude", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '2.0.0 (Claude Code)'; exit 0; fi\nexit 0\n")

	port := freePort(t)
	opts := serveOptions{
		Dir:           t.TempDir(),
		Port:          port,
		SkipAuthCheck: true,
		ServerBin:     filepath.Join(t.TempDir(), "does-not-exist"),
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
	}
	logger := testLogger(t)

	err := RunServe(context.Background(), opts, logger)
	if err == nil {
		t.Fatal("expected RunServe to return an error when the server binary cannot be located")
	}
	// opts.ServerBin is an explicit override (locateServerBinary's FIRST
	// resolution path, serverchild.go), so the error names the override env
	// var and the stat failure directly — not the generic "could not find ...
	// (looked for a sibling / PATH)" message, which is only reachable when no
	// override is set at all.
	if !strings.Contains(err.Error(), serverBinEnvOverride) || !strings.Contains(err.Error(), opts.ServerBin) {
		t.Fatalf("error = %q, want it to name %s and the missing path %q", err.Error(), serverBinEnvOverride, opts.ServerBin)
	}

	// No archistrator-server should ever have been spawned (locateServerBinary
	// fails before startServerChild is reached) — the HTTP port stays closed.
	if portInUse(fmt.Sprintf("127.0.0.1:%d", port)) {
		t.Fatalf("port %d is in use, but no child should have been spawned on this failure path", port)
	}
}

// TestRunServe_FatalPreflight_ReturnsError covers the OTHER half of the
// "no MCP session to degrade into" contract: git missing is a Fatal()
// preflight finding, which must now surface as a plain error naming the
// issue, not a silently-started (or silently-hung) process.
func TestRunServe_FatalPreflight_ReturnsError(t *testing.T) {
	installShim(t, "claude", "#!/bin/sh\necho '2.0.0'\nexit 0\n")
	dir := filepath.Dir(mustLookPath(t, "claude"))
	t.Setenv("PATH", dir)

	opts := serveOptions{
		Dir:           t.TempDir(),
		Port:          freePort(t),
		SkipAuthCheck: true,
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
	}
	logger := testLogger(t)

	err := RunServe(context.Background(), opts, logger)
	if err == nil {
		t.Fatal("expected RunServe to return an error when preflight is Fatal (git missing)")
	}
	if !strings.Contains(err.Error(), "git was not found") {
		t.Fatalf("error = %q, want it to name the missing git finding", err.Error())
	}
}
