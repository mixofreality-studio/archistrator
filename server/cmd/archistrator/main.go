// Command archistrator is the local-first CLI:
//
//   - `archistrator init` scaffolds the CURRENT directory into an
//     archistrator-ready local project and NEVER starts anything long-running
//     (init.go).
//   - `archistrator serve` is the standalone, long-lived local stack daemon
//     `.mcp.json` (written by init) points Claude Code's HTTP MCP client at:
//     the SAME composition root `cmd/server` builds for the cloud container,
//     reused unmodified as a supervised child process bound to loopback
//     (serverchild.go), fronting the embedded SPA + REST API + `/mcp`
//     (streamable-HTTP — the child's own mount; there is no stdio bridge
//     anymore) (serve.go). Runs until SIGINT/SIGTERM.
//
// Amendment 2026-07-19 (founder scope change — "standalone serve, drop
// Serena pattern"): `serve` replaces the earlier `mcp` stdio-auto-start
// design. Coupling the whole local stack's lifetime to a single Claude Code
// session's stdin lost in-memory design sessions on every restart
// (implicated in the state-reset bug class). `serve` is instead a plain
// daemon: start it once, point Claude Code's `.mcp.json` at its HTTP `/mcp`
// mount, and it keeps running independent of any one editor session — Go
// forbids importing `cmd/server` (it is `package main`), so this file only
// dispatches; serve.go owns no composition logic of its own beyond
// supervising the real composition root as a child process — see
// serverchild.go's package doc for why a child process (not an in-process
// import) is how this reuses cmd/server.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return 2
	}

	switch args[0] {
	case "init":
		dir, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "archistrator init:", err)
			return 1
		}
		if err := RunInit(dir, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "archistrator init:", err)
			return 1
		}
		return 0

	case "serve":
		opts, err := parseServeArgs(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "archistrator serve:", err)
			return 2
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if err := RunServe(ctx, opts, logger); err != nil {
			fmt.Fprintln(os.Stderr, "archistrator serve:", err)
			return 1
		}
		return 0

	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "archistrator: unknown subcommand %q\n\n", args[0])
		printUsage(os.Stderr)
		return 2
	}
}

func printUsage(w *os.File) {
	sayf(w, "%s", `archistrator — local-first design + construction

Usage:
  archistrator init            Scaffold the current directory for archistrator.
  archistrator serve [flags]   Run the standalone local stack (embedded SPA +
                                API + MCP on 127.0.0.1:8877 by default). Start
                                it once, then open Claude Code in this
                                directory — .mcp.json already points at its
                                HTTP /mcp mount. Runs until Ctrl-C.

serve flags:
  --port int            HTTP listen port for the local stack (default 8877;
                         env ARCHISTRATOR_SERVE_PORT).
  --skip-auth-check      Skip the "claude -p" authentication probe at startup.
`)
}

// ---------------------------------------------------------------------------
// Human-facing output.
// ---------------------------------------------------------------------------

// say writes one progress line to the CLI's own output stream.
//
// The write error is deliberately dropped. This is progress text on stdout/stderr,
// and a failed write to a closed or full pipe is not a reason to fail an `init`
// that has already created the repo, or a `serve` that has already booted the
// stack. Every human-facing write in this command goes through say/sayf so that
// judgement is made in one place instead of at each call site.
func say(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

// sayf is say with a format string, and no trailing newline of its own.
func sayf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
