package harness

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Server is a running archistrator-server process the harness drives over the
// wire. It is the built cmd/server binary started as a SUBPROCESS — the harness
// links ZERO server code (R3); it speaks only HTTP/MCP to it.
type Server struct {
	cmd     *exec.Cmd
	baseURL string
}

// ServerConfig is the boot configuration for one server process.
type ServerConfig struct {
	Infra Infra
	// DevAuth toggles ARCHISTRATOR_AUTH_DEV_MODE: true injects a dev architect
	// principal (the happy path); false leaves the surface unauthenticated so the
	// auth-boundary test can assert a 401.
	DevAuth bool
	// ExtraEnv is appended to the server process env AFTER the infra + worker env,
	// so a test can flip a server profile (e.g. the LOCAL project-state-git
	// substrate via gitLocalEnv) without widening this struct per knob. Each entry
	// is a "KEY=VALUE" string.
	ExtraEnv []string
}

// BuildServerBinary compiles ../server/cmd/server into a temp binary via a
// `go build` SUBPROCESS — not a Go import, so the harness's own test binary
// stays free of server code. The server module is the sibling ../server,
// resolved from this source file's location at build time.
func BuildServerBinary(ctx context.Context) (string, error) {
	root, err := moduleRoot()
	if err != nil {
		return "", err
	}
	serverDir := filepath.Join(root, "..", "server")
	bin := filepath.Join(os.TempDir(), fmt.Sprintf("archistrator-server-%d", os.Getpid()))

	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/server")
	cmd.Dir = serverDir
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build server binary in %s: %w", serverDir, err)
	}
	return bin, nil
}

// StartServer execs the built binary on a free loopback port wired to the
// provisioned infra, then blocks until /healthz answers 200. Stop() must be
// called (use t.Cleanup).
func StartServer(ctx context.Context, bin string, cfg ServerConfig) (*Server, error) {
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("reserve port: %w", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(),
		"ARCHISTRATOR_LISTEN_ADDR="+addr,
		"ARCHISTRATOR_POSTGRES_URL="+cfg.Infra.PostgresURL,
		"ARCHISTRATOR_TEMPORAL_HOSTPORT="+cfg.Infra.TemporalHostPort,
		"ARCHISTRATOR_TEMPORAL_NAMESPACE="+cfg.Infra.TemporalNamespace,
		fmt.Sprintf("ARCHISTRATOR_AUTH_DEV_MODE=%t", cfg.DevAuth),
	)
	cmd.Env = append(cmd.Env, workerEnv(cfg.Infra.Drafting, cfg.Infra, cassetteDir())...)
	// Per-test profile overrides (e.g. the LOCAL project-state-git substrate) go
	// LAST so they win over any default set above.
	cmd.Env = append(cmd.Env, cfg.ExtraEnv...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start server process: %w", err)
	}

	s := &Server{cmd: cmd, baseURL: "http://" + addr}
	if err := s.waitHealthy(ctx, 60*time.Second); err != nil {
		_ = s.Stop()
		return nil, err
	}
	return s, nil
}

// BaseURL is the http://host:port the server listens on.
func (s *Server) BaseURL() string { return s.baseURL }

// Stop interrupts the server and waits for it to exit, killing it if it does not
// drain within the grace window.
func (s *Server) Stop() error {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	_ = s.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = s.cmd.Process.Kill()
		<-done
	}
	return nil
}

func (s *Server) waitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/healthz", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("server at %s never became healthy within %s", s.baseURL, timeout)
}

// freePort reserves an ephemeral loopback port and releases it, returning the
// number for the server to bind. The small TOCTOU window is acceptable for a
// local/CI harness.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// cassetteDir resolves the committed systemtests cassette directory
// (<harness module root>/testdata/cassettes). A best-effort fallback keeps the
// path non-empty if moduleRoot fails (the server then errors clearly on a bad dir).
func cassetteDir() string {
	root, err := moduleRoot()
	if err != nil {
		return "testdata/cassettes"
	}
	return filepath.Join(root, "testdata", "cassettes")
}

// moduleRoot walks up from this source file to the directory holding go.mod (the
// harness module root). Used to locate the sibling ../server for the build.
func moduleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot resolve caller for module root")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", file)
		}
		dir = parent
	}
}
