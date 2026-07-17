package main

// seatassets.go — the `seat-assets` ONE-SHOT subcommand (runtime prompt
// materialization, founder-ratified 2026-07-17).
//
// DOCTRINE. Operated repos NO LONGER carry the ~100-file .claude prompt surface
// as committed content — only the workflow files (plus the birth-only go-test
// gate scaffold) are repo-committed. Instead, the design/construction CI jobs
// materialize the prompts into the RUNNER CHECKOUT at job start by running
//
//	aiarch-state-mcp seat-assets --dest .
//
// against the SAME pinned binary generation the job installs for its MCP/validate
// steps (sourcecontrol.StateMcpModulePin). Because this binary and the rendered
// assets come from ONE module resolution, the prompt surface a job runs is BY
// CONSTRUCTION the generation the dispatching server's validators understand —
// the pin is the provenance; no committed copy exists to drift.
//
// SEMANTICS. methodassets.Materialize is the single rendering authority (the same
// library API the platform's cmd/method-assets install path uses): it force-
// overwrites every owned file under <dest>/.claude (os.WriteFile — a stale
// committed copy in a legacy checkout cannot shadow the rendered generation),
// prunes files the previous seat manifest owned that no longer exist in the asset
// set, and rewrites the manifest. Idempotent; touches no git state and makes no
// network call.

import (
	"flag"
	"fmt"
	"strings"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// runSeatAssets parses the seat-assets flags and materializes the full .claude
// prompt surface (commands/skills/agents + seat manifest) into --dest.
func runSeatAssets(args []string) error {
	fs := flag.NewFlagSet("seat-assets", flag.ContinueOnError)
	dest := fs.String("dest", "", "directory to render the .claude prompt surface into (the runner checkout root)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*dest) == "" {
		return fmt.Errorf("--dest is required (the checkout directory to render .claude into)")
	}
	if err := methodassets.Materialize(*dest); err != nil {
		return fmt.Errorf("materialize method-assets into %s: %w", *dest, err)
	}
	return nil
}
