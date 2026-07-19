package main

// mcpserve_test.go covers Task-5 Step 1(b): `archistrator mcp` responds to an
// MCP `initialize` over stdio AND serves the HTTP port in the SAME managed
// process tree — a real subprocess integration test, not a mock: it builds
// the real archistrator-server binary (the composition root `cmd/server`
// already exercises its own unit/systemtests; this test does not re-prove
// that boot walk, only that `archistrator mcp` correctly locates, spawns, and
// bridges into it), runs `archistrator mcp` as a child, speaks newline-JSON
// MCP over its stdin/stdout, and separately probes the HTTP port.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestMCP_Initialize_And_HTTP_SameManagedProcess is Task-5 Step 1(b).
func TestMCP_Initialize_And_HTTP_SameManagedProcess(t *testing.T) {
	requireTemporalCLI(t)

	archistratorBin, _ := buildBinaries(t)
	projectDir := t.TempDir()

	var initOut bytes.Buffer
	if err := RunInit(projectDir, &initOut); err != nil {
		t.Fatalf("RunInit: %v", err)
	}

	port := freePort(t)

	// Deliberately NOT exec.CommandContext: binding this process to a ctx
	// whose CancelFunc fires via `defer` at test-function-return would race
	// the graceful shutdown below — os/exec's default ctx-cancel behavior is
	// an IMMEDIATE Kill, executed by a background goroutine as soon as the
	// context is Done, and `defer cancel()` runs BEFORE any t.Cleanup
	// callback (which is where the graceful stdin.Close()-then-Wait sequence
	// lives) — so the Kill would win the race and SIGKILL this process
	// before its own deferred cleanup (stopServerChild/stopTemporal) ever
	// runs, orphaning the archistrator-server + temporal-dev-server children.
	// Confirmed by reproducing exactly that leak during this test's own
	// development. The `go test -timeout` flag remains the outer safety net.
	cmd := exec.Command(archistratorBin, "mcp", "--port", fmt.Sprintf("%d", port), "--skip-auth-check")
	cmd.Dir = projectDir
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start archistrator mcp: %v", err)
	}
	// Continuously drain stdout for the child's ENTIRE lifetime via a
	// background pump into a buffered channel. This is not merely
	// convenience: os/exec's own docs warn it is "incorrect to call Wait
	// before all reads from the pipe have completed" — reading exactly one
	// line and then leaving the rest of the pipe undrained while later
	// calling cmd.Wait() (in Cleanup below) reproduced a real hang + orphaned
	// archistrator-server/temporal-dev-server child processes during this
	// test's own development; always keep the pipe draining instead.
	lines := make(chan []byte, 16)
	go func() {
		defer close(lines)
		r := bufio.NewReader(stdout)
		for {
			l, err := r.ReadBytes('\n')
			if len(l) > 0 {
				lines <- bytes.TrimSpace(l)
			}
			if err != nil {
				return
			}
		}
	}()

	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
		}
		if testing.Verbose() || t.Failed() {
			t.Logf("archistrator mcp stderr:\n%s", stderr.String())
		}
	})

	// (a) send an MCP `initialize` request over stdin, framed as
	// StdioTransport documents: newline-delimited JSON.
	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-driver", "version": "0.0.0"},
		},
	}
	line, err := json.Marshal(initReq)
	if err != nil {
		t.Fatalf("marshal initialize: %v", err)
	}
	if _, err := stdin.Write(append(line, '\n')); err != nil {
		t.Fatalf("write initialize: %v", err)
	}

	respLine, err := readLineFromChan(t, lines, 60*time.Second)
	if err != nil {
		t.Fatalf("read initialize response: %v\nstderr so far:\n%s", err, stderr.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(respLine, &resp); err != nil {
		t.Fatalf("initialize response is not valid JSON: %v\nraw: %s", err, respLine)
	}
	if _, ok := resp["result"]; !ok {
		t.Fatalf("initialize response has no result: %s", respLine)
	}
	result, _ := resp["result"].(map[string]any)
	if _, ok := result["capabilities"]; !ok {
		t.Fatalf("initialize result missing capabilities: %s", respLine)
	}

	// (b) the HTTP listener answers in the SAME managed process tree — poll
	// /healthz (always present regardless of the localdist SPA build tag;
	// see spa_handler.go's doc for why "/" itself needs a real webApp dist).
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if err := waitHealthy(context.Background(), addr, 60*time.Second); err != nil {
		t.Fatalf("HTTP listener never became healthy: %v\nstderr so far:\n%s", err, stderr.String())
	}
	httpResp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", httpResp.StatusCode)
	}
}

// readLineFromChan waits for the next line the background stdout pump
// (started right after cmd.Start(), above) delivers, or times out.
func readLineFromChan(t *testing.T, lines <-chan []byte, timeout time.Duration) ([]byte, error) {
	t.Helper()
	select {
	case l, ok := <-lines:
		if !ok {
			return nil, fmt.Errorf("stdout closed before a line arrived")
		}
		return l, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out after %s waiting for a line", timeout)
	}
}

// TestMCP_SingletonGuard_RefusesSecondInstance covers the v1 singleton guard:
// a second `archistrator mcp` against a port already answering exits with a
// clear, non-zero, "one session per project" message BEFORE touching
// anything (no child spawned, no Temporal contacted) — proven here with a
// plain HTTP listener standing in for "a live archistrator instance", since
// the guard only checks reachability, not identity (documented v1 scope).
func TestMCP_SingletonGuard_RefusesSecondInstance(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
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

	opts := mcpOptions{
		Dir:           t.TempDir(),
		Port:          port,
		SkipAuthCheck: true,
		Stdout:        &bytes.Buffer{},
		Stderr:        &bytes.Buffer{},
	}
	logger := testLogger(t)

	err = RunMCP(context.Background(), opts, logger)
	if err == nil {
		t.Fatal("expected RunMCP to refuse a second instance, got nil error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("error = %q, want it to mention 'already running'", err.Error())
	}
}

// TestMCP_Degrades_WhenServerBinaryMissing covers the "any Step 3-5 failure
// degrades rather than exits hard" contract (mcpserve.go's degrade): with
// git+claude present (so preflight is non-fatal) but ARCHISTRATOR_SERVER_BIN
// pointing at nothing, RunMCP must still answer a real MCP `initialize` over
// stdio — carrying the failure as Instructions text — rather than crash or
// hang. This exercises the degrade path without needing a real Temporal or
// archistrator-server binary at all (locateServerBinary fails first).
func TestMCP_Degrades_WhenServerBinaryMissing(t *testing.T) {
	archistratorBin, _ := buildBinaries(t)
	projectDir := t.TempDir()

	var initOut bytes.Buffer
	if err := RunInit(projectDir, &initOut); err != nil {
		t.Fatalf("RunInit: %v", err)
	}

	port := freePort(t)
	cmd := exec.Command(archistratorBin, "mcp", "--port", fmt.Sprintf("%d", port), "--skip-auth-check")
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "ARCHISTRATOR_SERVER_BIN="+filepath.Join(t.TempDir(), "does-not-exist"))

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start archistrator mcp: %v", err)
	}
	lines := make(chan []byte, 16)
	go func() {
		defer close(lines)
		r := bufio.NewReader(stdout)
		for {
			l, err := r.ReadBytes('\n')
			if len(l) > 0 {
				lines <- bytes.TrimSpace(l)
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
		if testing.Verbose() || t.Failed() {
			t.Logf("archistrator mcp stderr:\n%s", stderr.String())
		}
	})

	initReq := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-driver", "version": "0.0.0"},
		},
	}
	line, err := json.Marshal(initReq)
	if err != nil {
		t.Fatalf("marshal initialize: %v", err)
	}
	if _, err := stdin.Write(append(line, '\n')); err != nil {
		t.Fatalf("write initialize: %v", err)
	}

	respLine, err := readLineFromChan(t, lines, 30*time.Second)
	if err != nil {
		t.Fatalf("read initialize response: %v\nstderr:\n%s", err, stderr.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(respLine, &resp); err != nil {
		t.Fatalf("initialize response is not valid JSON: %v\nraw: %s", err, respLine)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize response has no result: %s", respLine)
	}
	instructions, _ := result["instructions"].(string)
	if !strings.Contains(instructions, "could not start") || !strings.Contains(instructions, "ARCHISTRATOR_SERVER_BIN") {
		t.Fatalf("instructions did not name the missing-binary failure, got: %q", instructions)
	}

	// No archistrator-server should ever have been spawned (locateServerBinary
	// fails before startServerChild is reached) — the HTTP port stays closed.
	if portInUse(fmt.Sprintf("127.0.0.1:%d", port)) {
		t.Fatalf("port %d is in use, but no child should have been spawned on this degrade path", port)
	}
}
