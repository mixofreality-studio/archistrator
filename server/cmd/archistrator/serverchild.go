package main

// serverchild.go locates and supervises the REAL archistrator-server
// composition root (server/cmd/server, the SAME container the cloud image
// runs) as a child process bound to loopback, configured for the local
// profile. This — not an in-process Go import — is how `archistrator serve`
// reuses cmd/server's composition without forking it: cmd/server is
// `package main`, and Go refuses to import a package that declares
// `package main` from anywhere else (verified empirically: "import ... is a
// program, not an importable package"), so the ONLY way to run its ACTUAL
// boot walk (main.gen.go's RunGenerated — telemetry, Temporal client, every
// profile-switched ResourceAccess, the four Managers + embedded Temporal
// Workers, the generated web/MCP transports, graceful shutdown) unmodified
// is to run the compiled binary and bridge into it.
import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// serverBinEnvOverride lets callers (tests, packagers) pin exactly which
// archistrator-server binary to spawn, bypassing discovery.
const serverBinEnvOverride = "ARCHISTRATOR_SERVER_BIN"

// locateServerBinary resolves the archistrator-server binary to spawn, in
// order: an explicit override, a binary named archistrator-server sitting
// next to the currently running archistrator executable (the shape
// scripts/build-local.sh's packaging step is expected to produce — both
// binaries staged side by side), then PATH. A discovery miss is NOT fatal to
// the caller by itself — RunServe (serve.go) returns it as a hard error,
// printed to stderr by main.go, exactly like a Fatal() preflight finding.
func locateServerBinary(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("%s=%s: %w", serverBinEnvOverride, override, err)
		}
		return override, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "archistrator-server")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if p, err := exec.LookPath("archistrator-server"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf(
		"could not find the archistrator-server binary (looked for %s, a sibling of this executable, and PATH) — "+
			"build it with `scripts/build-local.sh` or set %s", "archistrator-server", serverBinEnvOverride)
}

// serverChildConfig is the local-profile env this package composes for the
// spawned archistrator-server: zero external dependencies beyond git+claude
// (Task 2's local profile) plus the loopback listen address and Temporal
// hostport this package resolved (temporal.go).
type serverChildConfig struct {
	Bin              string
	RepoDir          string // absolute path to the project repo (git-forward projectstate substrate)
	ListenAddr       string // "127.0.0.1:<port>"
	TemporalHostport string
	// TemporalNamespace is the stack's dedicated namespace (temporal.go's
	// localTemporalNamespace) — the identity seam that turns a wrong/foreign
	// Temporal backend into a typed NamespaceNotFound instead of a
	// destructive "no active design session" 404 (QA 2026-07-19).
	TemporalNamespace string
}

// env composes the child's environment: the parent's environment (so PATH,
// HOME, and an operator-supplied ANTHROPIC_API_KEY override all pass
// through — resolveWorkerProvider in cmd/server/hooks.go honors that override
// exactly as the cloud profile does) plus the local-profile settings this
// package is responsible for.
func (c serverChildConfig) env() []string {
	extra := map[string]string{
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL":    "true",
		"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL": "file://" + c.RepoDir,
		"ARCHISTRATOR_LISTEN_ADDR":                c.ListenAddr,
		"ARCHISTRATOR_TEMPORAL_HOSTPORT":          c.TemporalHostport,
		"ARCHISTRATOR_TEMPORAL_NAMESPACE":         c.TemporalNamespace,
		// Loopback-only local stack — the auth floor doc (plan's Global
		// Constraints) permits dev-mode auth ONLY on loopback, which
		// ListenAddr above is.
		"ARCHISTRATOR_AUTH_DEV_MODE": "true",
		// ARCHISTRATOR_CONSTRUCTION_DRYRUN is deliberately ABSENT from this
		// forced set (I2, local-first-init-funnel final review). The local
		// construction executor (Task 6, server/internal/resourceaccess/
		// constructionpipeline/constructionpipelineaccess.go's localexec.go
		// section — headless `claude`, sandboxed-by-default fail-closed) is
		// BUILT, and scripts/build-local.sh now stages a compiled
		// aiarch-state-mcp binary alongside archistrator-server (the
		// prerequisite cmd/server/hooks.go's locateStateMCPBinary needs — see
		// its discovery order, mirrored by this package's own
		// locateServerBinary above). So this key is left for the loop below to
		// pass through unmodified from the PARENT (archistrator serve) process's
		// own environment: an operator's explicit override still reaches the
		// child untouched, and when the parent leaves it unset too, cmd/server's
		// own config.gen.go default (getenvBool("ARCHISTRATOR_CONSTRUCTION_DRYRUN",
		// "false")) governs — real local construction by default now, not a
		// forced dry run.
	}
	out := make([]string, 0, len(os.Environ())+len(extra))
	seen := make(map[string]bool, len(extra))
	for _, kv := range os.Environ() {
		key, _, ok := splitEnv(kv)
		if ok && !seen[key] {
			if v, override := extra[key]; override {
				out = append(out, key+"="+v)
				seen[key] = true
				continue
			}
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		if !seen[k] {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func splitEnv(kv string) (key, value string, ok bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}

// startServerChild launches archistrator-server per cfg with stdout/stderr
// forwarded to logOut (so its boot log rides the SAME stream `archistrator
// mcp`'s own diagnostics use — visible to whoever is watching stderr) and
// waits (bounded by readyTimeout) for its /healthz to answer, so the caller
// never proxies tool calls to a not-yet-ready child. The returned *exec.Cmd
// is running; the caller owns terminating it (stopServerChild).
func startServerChild(ctx context.Context, cfg serverChildConfig, logOut io.Writer, readyTimeout time.Duration) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, cfg.Bin) //nolint:gosec // resolved binary path, not user-controlled input
	cmd.Env = cfg.env()
	cmd.Stdout = logOut
	cmd.Stderr = logOut
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start archistrator-server: %w", err)
	}

	if err := waitHealthy(ctx, cfg.ListenAddr, readyTimeout); err != nil {
		_ = stopProcess(cmd, 5*time.Second)
		return nil, err
	}
	return cmd, nil
}

// waitHealthy polls GET http://addr/healthz until it returns 200, the
// process appears to have exited (best-effort, via a TCP probe), or timeout
// elapses.
func waitHealthy(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		resp, err := client.Get("http://" + addr + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("archistrator-server did not become healthy at %s within %s", addr, timeout)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// portInUse reports whether something is already accepting TCP connections
// at addr — the singleton guard serve.go uses BEFORE spawning anything.
func portInUse(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// stopProcess sends SIGTERM and waits up to grace for the process to exit,
// escalating to Kill if it does not — the "SIGTERM the construction
// subprocesses, drain Temporal" shutdown contract, applied here to the
// archistrator-server child (whose OWN graceful-shutdown path — main.gen.go's
// srv.Shutdown + Temporal client Close — is what actually drains Temporal;
// this function just gives it the SIGTERM+grace window to run it).
func stopProcess(cmd *exec.Cmd, grace time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(grace):
		_ = cmd.Process.Kill()
		return <-done
	}
}
