package main

// mcpserve.go implements `archistrator mcp` — Task 5's Serena-pattern stdio
// server, auto-spawned by Claude Code via `.mcp.json`. See main.go's package
// doc and serverchild.go's package doc for WHY this reuses cmd/server's
// composition as a supervised child process rather than an in-process
// import: Go forbids importing a `package main`, so relaying into the
// ACTUAL compiled cmd/server binary (unmodified — zero forked composition
// logic) is the only way to expose its real Managers/Temporal-backed tool
// catalog and its real Task-4 embedded-SPA HTTP listener without
// reimplementing either.
//
// End-to-end shape of one `archistrator mcp` invocation:
//  1. Preflight (preflight.go) — git/claude present, claude authenticated.
//  2. Singleton guard — refuse to start a second instance against the same
//     port (v1: no proxy cleverness, per the brief).
//  3. Resolve a reachable Temporal (temporal.go) — spawning a managed
//     `temporal server start-dev` if nothing is already listening.
//  4. Spawn archistrator-server (serverchild.go) bound to 127.0.0.1:<port>,
//     local profile, wait for /healthz.
//  5. Bridge: connect an mcp.Client to the child's /mcp, mount its tools onto
//     a local stdio mcp.Server (toolproxy.go).
//  6. Run the local server over stdio until stdin closes (Claude Code session
//     end) or a signal arrives — mcp.Server.Run returns on either.
//  7. Tear down: SIGTERM archistrator-server (which drains Temporal itself,
//     exactly as its own graceful shutdown does today), then the temporal
//     dev-server if this process started one.
//
// Any preflight/discovery/Temporal/child-boot failure DEGRADES rather than
// exits hard once past the singleton guard: the local stdio mcp.Server still
// starts and answers `initialize`, carrying the failure in its Instructions
// text, so Claude Code's driver can see and relay an actionable message
// instead of a silently dead process. The only HARD exit before that point is
// the singleton guard (starting nothing at all is correct there).
import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultMCPPort  = 8877
	mcpPortEnvVar   = "ARCHISTRATOR_MCP_PORT"
	serverReadyWait = 30 * time.Second
	childStopGrace  = 5 * time.Second
)

// mcpOptions is RunMCP's parsed configuration.
type mcpOptions struct {
	Dir            string // project root; defaults to the CWD
	Port           int
	SkipAuthCheck  bool
	ServerBin      string // override for locateServerBinary; "" = discover
	Stdout, Stderr io.Writer
}

// parseMCPArgs parses the `archistrator mcp` flag set. Defaults Dir to the
// CWD and Port to defaultMCPPort (or mcpPortEnvVar, flag wins over env).
func parseMCPArgs(args []string) (mcpOptions, error) {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	port := fs.Int("port", 0, "HTTP listen port for the local stack (default 8877; env "+mcpPortEnvVar+")")
	skipAuth := fs.Bool("skip-auth-check", false, "skip the claude -p authentication probe at startup")
	if err := fs.Parse(args); err != nil {
		return mcpOptions{}, err
	}

	resolvedPort := *port
	if resolvedPort == 0 {
		resolvedPort = defaultMCPPort
		if v := os.Getenv(mcpPortEnvVar); v != "" {
			p, err := strconv.Atoi(v)
			if err != nil {
				return mcpOptions{}, fmt.Errorf("%s=%q: %w", mcpPortEnvVar, v, err)
			}
			resolvedPort = p
		}
	}

	dir, err := os.Getwd()
	if err != nil {
		return mcpOptions{}, err
	}

	return mcpOptions{
		Dir:           dir,
		Port:          resolvedPort,
		SkipAuthCheck: *skipAuth,
		ServerBin:     os.Getenv(serverBinEnvOverride),
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	}, nil
}

// RunMCP is the full Step-3..7 flow described in the package doc above.
func RunMCP(ctx context.Context, opts mcpOptions, logger *slog.Logger) error {
	addr := fmt.Sprintf("127.0.0.1:%d", opts.Port)

	// Singleton guard (v1): a live instance already answering on addr is the
	// ONLY hard-exit path — starting a second stack against the same port
	// would either fail confusingly or silently shadow the first.
	if portInUse(addr) {
		return fmt.Errorf("archistrator is already running for this project at http://%s — one session per project (v1); stop it before starting a new one", addr)
	}

	preflight := runPreflight(ctx, opts.SkipAuthCheck)
	logger.Info("preflight", "instructions", preflight.Instructions())

	if preflight.Fatal() {
		logger.Warn("preflight failed — serving instructions-only, no tools mounted", "instructions", preflight.Instructions())
		return runInstructionsOnly(ctx, preflight.Instructions())
	}

	serverBin, err := locateServerBinary(opts.ServerBin)
	if err != nil {
		return degrade(ctx, err, logger)
	}

	// Version-skew guard (Task-5 review finding 3): a read-only comparison of
	// both binaries' VCS revisions, cheap enough to do before spawning
	// anything. A mismatch is a WARNING folded into the Instructions text
	// below, never a refusal to start — see versioncheck.go's package doc.
	instructions := preflight.Instructions()
	if warning := versionSkewWarning(ownBuildIdentity(), childBuildIdentity(serverBin), serverBin); warning != "" {
		logger.Warn("version skew detected between archistrator and archistrator-server", "warning", warning)
		instructions += "\n" + warning
	}

	temporalHostport := defaultTemporalHostport()
	stopTemporal, err := ensureTemporal(ctx, temporalHostport, opts.Stderr)
	if err != nil {
		return degrade(ctx, err, logger)
	}
	defer stopTemporal()

	childCfg := serverChildConfig{
		Bin:               serverBin,
		RepoDir:           opts.Dir,
		ListenAddr:        addr,
		TemporalHostport:  temporalHostport,
		TemporalNamespace: localTemporalNamespace,
	}
	child, err := startServerChild(ctx, childCfg, opts.Stderr, serverReadyWait)
	if err != nil {
		return degrade(ctx, err, logger)
	}
	defer func() { _ = stopProcess(child, childStopGrace) }()

	upstream, err := connectUpstream(ctx, addr)
	if err != nil {
		return degrade(ctx, err, logger)
	}
	defer func() { _ = upstream.Close() }()

	local := mcp.NewServer(&mcp.Implementation{Name: "archistrator", Version: mcpVersion},
		&mcp.ServerOptions{Instructions: instructions})

	spaURL := "http://" + addr + "/"
	n, err := mountProxiedTools(ctx, local, upstream, spaURL)
	if err != nil {
		return degrade(ctx, err, logger)
	}
	logger.Info("local stack ready", "toolsProxied", n, "httpAddr", addr)
	fmt.Fprintf(opts.Stderr, "archistrator: local stack ready — SPA/API at http://%s, %d tools available\n", addr, n)

	return local.Run(ctx, &mcp.StdioTransport{})
}

// runInstructionsOnly serves a stdio MCP session carrying instructions and no
// tools — used both for a Fatal() preflight and for any Step 3-5 failure
// (degrade below), so the driver always gets a live, actionable MCP session
// instead of a silently dead process.
func runInstructionsOnly(ctx context.Context, instructions string) error {
	srv := mcp.NewServer(&mcp.Implementation{Name: "archistrator", Version: mcpVersion},
		&mcp.ServerOptions{Instructions: instructions})
	return srv.Run(ctx, &mcp.StdioTransport{})
}

// degrade folds a Step 3-5 failure into an instructions-only session.
func degrade(ctx context.Context, cause error, logger *slog.Logger) error {
	logger.Error("local stack did not start — serving instructions-only", "err", cause)
	return runInstructionsOnly(ctx, "archistrator: the local stack could not start:\n  - "+cause.Error()+"\n")
}
