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
	"errors"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// runOneShotCommand dispatches the one-shot subcommands. ONE-SHOT subcommands run and
// exit WITHOUT starting the stdio MCP server. argv is the argument list with the program
// name already stripped. It reports handled=false when argv names none of them, leaving
// main() to fall through to the MCP server.
func runOneShotCommand(argv []string) (handled bool, err error) {
	if len(argv) == 0 {
		return false, nil
	}
	switch argv[0] {
	// `reconcile` is the deterministic project.json merge-conflict resolver the design
	// workflow's refresh step invokes when a session branch has diverged from main (F80):
	// it keeps ALL state-file logic in ProjectStateAccess code
	// (projectstate.ReconcileSlotOntoBase) rather than a bash/jq hack in the workflow.
	case "reconcile":
		return true, wrapOneShot("reconcile", runReconcile(os.Getenv, argv[1:]))
	// `validate` is the Method-invariant REQUIRED CI check the seated design workflow
	// runs on every design PR (validate.go): the same methodcheck rules putDraftModel
	// enforces in-loop, with the staleness-aware cross-artifact severity policy
	// (staleness.go), over the checkout's committed .aiarch/state/project.json.
	case "validate":
		return true, wrapOneShot("validate", runValidate(argv[1:], os.Stdout))
	// `seat-assets` materializes the .claude prompt surface into the runner checkout
	// at job start (seatassets.go): operated repos do not commit the prompt surface —
	// the pinned binary generation renders it, so the pin is the provenance.
	case "seat-assets":
		return true, wrapOneShot("seat-assets", runSeatAssets(argv[1:]))
	// `step-tools` prints a step's built-in tool deny list so the CLOUD rail's answer
	// job (the one job with no task spec, R30) can pass --disallowedTools without
	// restating the manifest in YAML. The local executor calls the manifest
	// in-process (answerJobDeniedTools); both read the SAME method-assets manifest,
	// so the two rails cannot diverge.
	case "step-tools":
		return true, wrapOneShot("step-tools", runStepTools(argv[1:], os.Stdout))
	// The task-spec venue subcommands the construct and design workflows run around
	// the agent (agent task spec, §4–§6): `task-spec` resolves the spec the dispatch
	// hashed, `edit-scope` gates the agent's commits against its edit scope,
	// `run-checks` is the check gate (the runner the run_checks verb uses), and
	// `record-check-run` records the gate's results as the task's CheckRun.
	case "task-spec":
		return true, wrapOneShot("task-spec", runTaskSpec(argv[1:], os.Stdout))
	// `version` prints the task-spec generation this binary carries
	// (projectstate.StateToolVersion): its method-assets module version and the spec
	// schema version. The local rail compares it with the server's own before gating, so
	// a stale binary is refused by name rather than as a drifted spec (R56).
	case "version":
		return true, wrapOneShot("version", runVersion(argv[1:], os.Stdout))
	case "edit-scope":
		return true, wrapOneShot("edit-scope", runEditScope(argv[1:]))
	case "run-checks":
		return true, wrapOneShot("run-checks", runRunChecks(argv[1:], os.Stdout))
	case "record-check-run":
		return true, wrapOneShot("record-check-run", runRecordCheckRun(argv[1:]))
	// `record-test-plan` is the single writer for one component's test plan while the
	// plans are bound one at a time (recordtestplan.go): a byte splice gated on that
	// component's TP-* rules plus no new errors.
	case "record-test-plan":
		return true, wrapOneShot("record-test-plan", runRecordTestPlan(argv[1:], os.Stdout))
	}
	return false, nil
}

// wrapOneShot prefixes a subcommand failure with the subcommand name, preserving the
// stderr message shape each one-shot reported before the dispatch was extracted.
func wrapOneShot(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", name, err)
}

// exitError is a one-shot failure that names its process exit code. A failure that is
// not one exits 1. run-checks distinguishes a red check (1) from the runner's own
// failure (2), which the workflows branch on.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

// exitCodeOf is the process exit code of a one-shot failure.
func exitCodeOf(err error) int {
	var ee exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

func main() {
	if handled, err := runOneShotCommand(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintf(os.Stderr, "aiarch-state-mcp: %v\n", err)
			os.Exit(exitCodeOf(err))
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
