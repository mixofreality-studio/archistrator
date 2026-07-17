// Command aiarch-state-mcp is the local stdio MCP server the aiarch DESIGN workflow
// launches inside the GitHub Actions job (via claude-code-action's MCP config). It IS
// ProjectStateAccess code (the agentic-managers spec, §Construction application): it
// operates DIRECTLY on the checked-out working tree — .aiarch/state/project.json + the
// research corpus — decoding and encoding through the SAME projectstate codec the server
// uses, and validating every drafted model through that codec PLUS the Method CI rules
// (methodcheck) before it is written. The agent authors Method artifacts through these
// verbs instead of hand-editing project.json; the ambient design job (project id,
// artifact kind, job mode, target branch — from env) fixes the slot so the agent never
// guesses it.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// ONE-SHOT subcommands run and exit WITHOUT starting the stdio MCP server. `reconcile`
	// is the deterministic project.json merge-conflict resolver the design workflow's
	// refresh step invokes when a session branch has diverged from main (F80): it keeps
	// ALL state-file logic in ProjectStateAccess code (projectstate.ReconcileSlotOntoBase)
	// rather than a bash/jq hack in the workflow.
	if len(os.Args) > 1 && os.Args[1] == "reconcile" {
		if err := runReconcile(os.Getenv, os.Args[2:]); err != nil {
			fatalf("reconcile: %v", err)
		}
		return
	}
	// `validate` is the Method-invariant REQUIRED CI check the seated design workflow
	// runs on every design PR (validate.go): the same methodcheck rules putDraftModel
	// enforces in-loop, with the staleness-aware cross-artifact severity policy
	// (staleness.go), over the checkout's committed .aiarch/state/project.json.
	if len(os.Args) > 1 && os.Args[1] == "validate" {
		if err := runValidate(os.Args[2:], os.Stdout); err != nil {
			fatalf("validate: %v", err)
		}
		return
	}
	// `seat-assets` materializes the .claude prompt surface into the runner checkout
	// at job start (seatassets.go): operated repos do not commit the prompt surface —
	// the pinned binary generation renders it, so the pin is the provenance.
	if len(os.Args) > 1 && os.Args[1] == "seat-assets" {
		if err := runSeatAssets(os.Args[2:]); err != nil {
			fatalf("seat-assets: %v", err)
		}
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		fatalf("resolve working directory: %v", err)
	}
	session, err := newSessionFromEnv(os.Getenv, wd)
	if err != nil {
		fatalf("%v", err)
	}
	srv := buildServer(session)
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fatalf("serve: %v", err)
	}
}

// marshalInputModel renders the agent-supplied model object back to JSON bytes for the
// strict codec decode. A nil model is rejected with an actionable message.
func marshalInputModel(model map[string]any) ([]byte, error) {
	if model == nil {
		return nil, fmt.Errorf("putDraftModel requires a 'model' object")
	}
	return json.Marshal(model)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "aiarch-state-mcp: "+format+"\n", args...)
	os.Exit(1)
}
