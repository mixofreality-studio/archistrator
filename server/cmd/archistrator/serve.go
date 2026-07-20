package main

// serve.go implements `archistrator serve` — the amended (2026-07-19,
// "standalone serve, drop Serena pattern") Task-5 daemon: a simple,
// long-lived process rather than a stdio child of one Claude Code session.
// It boots the SAME local stack the earlier `archistrator mcp` command did —
// the real archistrator-server composition root (cmd/server) as a supervised
// child process bound to loopback (serverchild.go), plus a reachable
// Temporal (temporal.go) — and keeps it running until SIGINT/SIGTERM, at
// which point it tears both down in the SAME order the earlier stdio bridge
// used (server child first — its own graceful shutdown drains ITS Temporal
// client — then the locally-spawned Temporal dev-server, if this process
// started one).
//
// The stdio MCP transport and the stdio<->HTTP tool-proxy bridge
// (toolproxy.go, deleted with this amendment) are GONE: the child's own
// `/mcp` streamable-HTTP mount (cmd/server/mcp_mount.go) is now the ONLY MCP
// surface. Claude Code talks to it directly over HTTP per the `.mcp.json`
// entry `archistrator init` now writes
// ({"type":"http","url":"http://127.0.0.1:<port>/mcp"}) — there is no tool
// catalog to relay here anymore, and therefore no local mcp.Server at all in
// this binary.
//
// One casualty of dropping the stdio relay: toolproxy.go's mountProxiedTools
// used to append a trailing "SPA: <url>" text block to certain proxied tool
// results (the "tool responses that reference session/project state include
// the SPA URL" requirement from the original plan). That behavior had NO
// server-side equivalent — grep across cmd/server confirms the child's own
// `/mcp` mount (mcp_mount.go) never touched it; it lived entirely in the
// stdio bridge this amendment removes. Moving it into cmd/server (e.g. via
// mcp.Server.AddReceivingMiddleware on newMCPServer, mcp_mount.go) would
// touch a different composition root than this task's scope and is NOT done
// here — left as a follow-up (see this task's report). The daemon-shaped
// equivalent kept in this file is simpler: print the SPA URL once, directly,
// to stdout when the stack becomes ready (below).
//
// End-to-end shape of one `archistrator serve` invocation:
//  1. Singleton guard — refuse to start a second instance against the same
//     port (v1: no proxy cleverness, per the brief) — unchanged from `mcp`.
//  2. Preflight (preflight.go) — git/claude present, claude authenticated. A
//     Fatal() finding is now a hard startup error (there is no MCP session
//     left to carry a degrade-instructions message through) — main.go prints
//     it to stderr and exits non-zero.
//  3. Resolve a reachable Temporal (temporal.go) — spawning a managed
//     `temporal server start-dev` if nothing is already listening.
//  4. Spawn archistrator-server (serverchild.go) bound to 127.0.0.1:<port>,
//     local profile, wait for /healthz.
//  5. Print the SPA URL and block until ctx is Done — SIGINT/SIGTERM, wired
//     by main.go via signal.NotifyContext.
//  6. Tear down in order (deferred, LIFO): SIGTERM archistrator-server
//     (which drains ITS Temporal client itself, exactly as its own graceful
//     shutdown does today), then the locally-spawned Temporal dev-server, if
//     this process started one.
import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"
)

const (
	defaultServePort = 8877
	servePortEnvVar  = "ARCHISTRATOR_SERVE_PORT"
	serverReadyWait  = 30 * time.Second
	childStopGrace   = 5 * time.Second
)

// serveOptions is RunServe's parsed configuration.
type serveOptions struct {
	Dir            string // project root; defaults to the CWD
	Port           int
	SkipAuthCheck  bool
	ServerBin      string // override for locateServerBinary; "" = discover
	Stdout, Stderr io.Writer
}

// parseServeArgs parses the `archistrator serve` flag set. Defaults Dir to
// the CWD and Port to defaultServePort (or servePortEnvVar, flag wins over
// env).
func parseServeArgs(args []string) (serveOptions, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	port := fs.Int("port", 0, "HTTP listen port for the local stack (default 8877; env "+servePortEnvVar+")")
	skipAuth := fs.Bool("skip-auth-check", false, "skip the claude -p authentication probe at startup")
	if err := fs.Parse(args); err != nil {
		return serveOptions{}, err
	}

	resolvedPort := *port
	if resolvedPort == 0 {
		resolvedPort = defaultServePort
		if v := os.Getenv(servePortEnvVar); v != "" {
			p, err := strconv.Atoi(v)
			if err != nil {
				return serveOptions{}, fmt.Errorf("%s=%q: %w", servePortEnvVar, v, err)
			}
			resolvedPort = p
		}
	}

	dir, err := os.Getwd()
	if err != nil {
		return serveOptions{}, err
	}

	return serveOptions{
		Dir:           dir,
		Port:          resolvedPort,
		SkipAuthCheck: *skipAuth,
		ServerBin:     os.Getenv(serverBinEnvOverride),
		Stdout:        os.Stdout,
		Stderr:        os.Stderr,
	}, nil
}

// RunServe is the full Step 1..6 flow described in the package doc above. It
// blocks until ctx is Done (SIGINT/SIGTERM) or an unrecoverable startup
// failure occurs, and always reaps whatever children it started (deferred,
// LIFO — server child before Temporal) before returning. A clean
// signal-triggered shutdown returns nil, not an error.
func RunServe(ctx context.Context, opts serveOptions, logger *slog.Logger) error {
	addr := fmt.Sprintf("127.0.0.1:%d", opts.Port)

	// Singleton guard (v1): a live instance already answering on addr is the
	// ONLY hard-exit path before anything is spawned — starting a second
	// stack against the same port would either fail confusingly or silently
	// shadow the first.
	if portInUse(addr) {
		return fmt.Errorf("archistrator is already running for this project at http://%s — one instance per project (v1); stop it before starting a new one", addr)
	}

	preflight := runPreflight(ctx, opts.SkipAuthCheck)
	logger.Info("preflight", "instructions", preflight.Instructions())
	if preflight.Fatal() {
		if shuttingDown(ctx) {
			return gracefulStartupAbort(logger)
		}
		return fmt.Errorf("%s", preflight.Instructions())
	}
	if opts.Stderr != nil {
		fmt.Fprint(opts.Stderr, preflight.Instructions())
	}

	serverBin, err := locateServerBinary(opts.ServerBin)
	if err != nil {
		return err
	}

	// Version-skew guard: a read-only comparison of both binaries' VCS
	// revisions, cheap enough to do before spawning anything. A mismatch is
	// a WARNING, never a refusal to start — see versioncheck.go's package
	// doc. With no MCP session left to carry it as Instructions text, it is
	// logged and printed directly instead.
	if warning := versionSkewWarning(ownBuildIdentity(), childBuildIdentity(serverBin), serverBin); warning != "" {
		logger.Warn("version skew detected between archistrator and archistrator-server", "warning", warning)
		if opts.Stderr != nil {
			fmt.Fprintln(opts.Stderr, warning)
		}
	}

	temporalHostport := defaultTemporalHostport()
	stopTemporal, err := ensureTemporal(ctx, temporalHostport, opts.Stderr)
	if err != nil {
		if shuttingDown(ctx) {
			return gracefulStartupAbort(logger)
		}
		return err
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
		if shuttingDown(ctx) {
			return gracefulStartupAbort(logger)
		}
		return err
	}
	defer func() { _ = stopProcess(child, childStopGrace) }()

	spaURL := "http://" + addr + "/"
	logger.Info("local stack ready", "httpAddr", addr, "spaURL", spaURL)
	if opts.Stdout != nil {
		fmt.Fprintf(opts.Stdout, "archistrator: local stack ready — SPA at %s (MCP at %smcp)\n", spaURL, spaURL)
	}

	<-ctx.Done()
	logger.Info("shutdown signal received — stopping local stack")
	return nil
}

// shuttingDown reports whether ctx is already Done — used to reinterpret a
// Step 3-4 startup failure (ensureTemporal, startServerChild) that is
// actually a SIGINT/SIGTERM racing with startup, not a genuine failure:
// temporal.go's and serverchild.go's polling loops (waitTCPReachable,
// waitHealthy, verifyTemporalNamespace) check ctx.Err() before their own
// success condition on every iteration, so a signal arriving in the narrow
// window right as the resource actually becomes ready can surface as e.g.
// "context canceled" even though the resource WOULD have reported healthy —
// and each of those call sites already stops whatever it spawned before
// returning that error (startServerChild's and ensureTemporal's own cleanup
// calls), so by the time RunServe sees this it is a clean shutdown, not a
// leak, and should be reported as one.
func shuttingDown(ctx context.Context) bool { return ctx.Err() != nil }

// gracefulStartupAbort logs and returns the nil RunServe uses when
// shuttingDown(ctx) explains an otherwise-error-shaped Step 3-4 return —
// SIGINT/SIGTERM during startup is a normal, successful "never mind" outcome
// (Ctrl-C is not a failure), never printed to the user as one.
func gracefulStartupAbort(logger *slog.Logger) error {
	logger.Info("shutdown signal received during startup — stopping")
	return nil
}
