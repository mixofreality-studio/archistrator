// Package usecases holds the black-box, wire-level SYSTEM TEST + REGRESSION
// flows — one per core use case, driven through the running server's published
// Client surfaces. This is the load-bearing (and only routine) test tier per the
// test-authoring constitution ([[the-method-testing]] §7 / operational-concepts
// §17). It boots the REAL server binary and links zero server code.
package usecases

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// The wire harness needs a PROVISIONED stack (Postgres + Temporal + Ollama) and
// builds + boots the real server binary against it. Absent that — under -short,
// or with no ARCHISTRATOR_* infra env in a container-less sandbox — every wire
// test SKIPS, matching the server module's own integration tests. Provision
// locally with `docker compose up` (docker-compose.yaml), export the
// ARCHISTRATOR_* endpoints, then `go test ./...`. CI provisions them as services.
var (
	infra      harness.Infra
	serverBin  string
	infraReady bool
)

func TestMain(m *testing.M) {
	flag.Parse() // required before testing.Short() is legal
	if testing.Short() {
		os.Exit(m.Run())
	}
	in, ok := harness.InfraFromEnv()
	if !ok {
		// No provisioned stack → leave infraReady false; each test skips itself.
		os.Exit(m.Run())
	}
	infra = in

	bin, err := harness.BuildServerBinary(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "systemtests: build server binary: %v\n", err)
		os.Exit(1)
	}
	serverBin = bin
	infraReady = true

	code := m.Run()
	_ = os.Remove(bin)
	os.Exit(code)
}

// requireStack skips a test unless a provisioned stack + built server are ready.
func requireStack(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("systemtests: skipped under -short (requires a provisioned Postgres+Temporal+Ollama stack)")
	}
	if !infraReady {
		t.Skip("systemtests: no provisioned stack — set ARCHISTRATOR_POSTGRES_URL / _TEMPORAL_HOSTPORT / _OLLAMA_BASEURL (see docker-compose.yaml)")
	}
}

// startServer boots a fresh server process for one test against the shared
// provisioned infra. Each test gets its own process + ephemeral port so the
// dev-auth on/off variants don't interfere.
func startServer(t *testing.T, devAuth bool) *harness.Server {
	t.Helper()
	return startServerWithEnv(t, devAuth, nil)
}

// startServerWithEnv is startServer plus per-test server-profile overrides — used
// by the E2E git-commit proof to boot the server in the LOCAL project-state-git
// substrate (harness.gitLocalEnv) so design artifacts commit to an on-disk repo.
func startServerWithEnv(t *testing.T, devAuth bool, extraEnv []string) *harness.Server {
	t.Helper()
	srv, err := harness.StartServer(context.Background(), serverBin, harness.ServerConfig{
		Infra:    infra,
		DevAuth:  devAuth,
		ExtraEnv: extraEnv,
	})
	if err != nil {
		t.Fatalf("start server (devAuth=%t): %v", devAuth, err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}
