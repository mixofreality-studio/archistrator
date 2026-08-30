package main

// steptools.go — the `step-tools` ONE-SHOT subcommand.
//
// WHY IT EXISTS. Both dispatch rails have to scope a step's BUILT-IN tool
// surface (--disallowedTools), and both must scope it identically or the same
// step behaves differently in CI than it does locally. The local executor can
// call the manifest in Go (agenticjobaccess.disallowedBuiltinTools). A GitHub
// Actions workflow cannot — it can only run binaries — so it asks THIS binary,
// which reads the SAME methodassets manifest. The alternative, restating the
// deny list in the workflow YAML, is exactly the duplication that would drift.
//
// Output is a single comma-separated line on stdout, ready to interpolate
// straight into claude_args. A step with no manifest prints nothing and exits
// 0, so the workflow degrades to the unscoped surface rather than failing.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// runStepTools parses the step-tools flags and writes the resolved list to out.
func runStepTools(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("step-tools", flag.ContinueOnError)
	command := fs.String("command", "", "the dispatchable command slug to resolve the tool surface for")
	disallowed := fs.Bool("disallowed", false, "print the comma-separated built-in tools to DENY for this step")
	if err := fs.Parse(args); err != nil {
		return err
	}
	slug := strings.TrimSpace(*command)
	if slug == "" {
		return fmt.Errorf("--command is required (the dispatchable command slug)")
	}
	if !*disallowed {
		return fmt.Errorf("--disallowed is required (it is the only supported query today)")
	}

	// methodassets owns the deny-candidate list AND the per-step resolution, so
	// this rail and the local executor cannot diverge. An unknown step yields an
	// empty line: deny nothing, keep the unscoped surface.
	_, err := fmt.Fprintln(out, strings.Join(methodassets.DisallowedBuiltinTools(slug), ","))
	return err
}
