// Command archistrator is the local-first CLI (local-first-init-funnel Task 5,
// docs/superpowers/plans/2026-07-19-local-first-init-funnel.md): the Serena
// pattern entry point Claude Code auto-spawns via `.mcp.json`.
//
//   - `archistrator init` scaffolds the CURRENT directory into an
//     archistrator-ready local project and NEVER starts anything long-running
//     (init.go).
//   - `archistrator mcp` is the stdio MCP server `.mcp.json` registers: it
//     preflights the local toolchain, guards against a second instance for the
//     same project, and boots the full local stack — the SAME composition
//     root `cmd/server` builds for the cloud container, reused unmodified as a
//     child process bound to loopback, fronted by a stdio<->HTTP MCP bridge
//     carrying the SAME tool catalog the child's own `/mcp` mount exposes
//     (mcpserve.go). This file only dispatches; it owns no composition logic
//     of its own — see mcpserve.go's package doc for why a child process
//     (not an in-process import) is how this reuses cmd/server: Go forbids
//     importing a package that declares `package main` cmd/server is one),
//     so the ONLY way to reuse its ACTUAL compiled composition without
//     forking it into a second copy is to run the same built binary and
//     bridge into it — not to reimplement its boot walk here.
package main

import (
	"context"
	"fmt"
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

	case "mcp":
		opts, err := parseMCPArgs(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "archistrator mcp:", err)
			return 2
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if err := RunMCP(ctx, opts, logger); err != nil {
			fmt.Fprintln(os.Stderr, "archistrator mcp:", err)
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
	fmt.Fprint(w, `archistrator — local-first design + construction (Serena pattern)

Usage:
  archistrator init            Scaffold the current directory for archistrator.
  archistrator mcp [flags]     Run the stdio MCP server (auto-started by Claude Code
                                via .mcp.json — you normally never invoke this by hand).

mcp flags:
  --port int            HTTP listen port for the local stack (default 8877;
                         env ARCHISTRATOR_MCP_PORT).
  --skip-auth-check      Skip the "claude -p" authentication probe at startup.
`)
}
