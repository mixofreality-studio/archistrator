package main

// taskspeccmd.go — the `task-spec` ONE-SHOT subcommand. A job resolves the spec of the
// task it was dispatched for from its own checkout and compares the hash with the one
// the dispatch carried, so the model, tools, edit scope and checks it runs are the ones
// the panel showed:
//
//	aiarch-state-mcp task-spec --activity A --task T [--root DIR] --out FILE
//
// It writes the spec as indented JSON to FILE and prints its hash and a newline. The
// hash is projectstate.TaskSpecFor's own, over the same decoded project.json the server
// reads, so the two sides cannot compute different hashes (R13). Any failure exits 2.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// runTaskSpec resolves and writes the spec; see the file comment.
func runTaskSpec(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("task-spec", flag.ContinueOnError)
	activity := flags.String("activity", "", "the activity id (e.g. C-billing-engine)")
	task := flags.String("task", "", "the lifecycle task id (e.g. integration, glossaryDraft)")
	root := flags.String("root", ".", "the checkout root holding .aiarch/state/project.json")
	out := flags.String("out", "", "the file the spec JSON is written to")
	if err := flags.Parse(args); err != nil {
		return exitError{2, err}
	}
	for _, f := range []struct{ name, value string }{{"activity", *activity}, {"task", *task}, {"out", *out}} {
		if strings.TrimSpace(f.value) == "" {
			return exitError{2, fmt.Errorf("--%s is required", f.name)}
		}
	}
	p, err := readCheckoutProject(*root)
	if err != nil {
		return exitError{2, err}
	}
	spec, err := projectstate.TaskSpecFor(&p, strings.TrimSpace(*activity), strings.TrimSpace(*task))
	if err != nil {
		return exitError{2, err}
	}
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return exitError{2, fmt.Errorf("encode the spec: %w", err)}
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o600); err != nil {
		return exitError{2, fmt.Errorf("write %s: %w", *out, err)}
	}
	if _, err := fmt.Fprintln(stdout, spec.Hash); err != nil {
		return exitError{2, err}
	}
	return nil
}

// runVersion prints projectstate.StateToolVersion and a newline. It takes no arguments.
func runVersion(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return exitError{2, fmt.Errorf("takes no arguments, got %q", args)}
	}
	if _, err := fmt.Fprintln(stdout, projectstate.StateToolVersion()); err != nil {
		return exitError{2, err}
	}
	return nil
}

// readCheckoutProject decodes root's .aiarch/state/project.json through the server
// codec. The project id is the document's own.
func readCheckoutProject(root string) (projectstate.Project, error) {
	path := filepath.Join(root, statePathPrefix, projectFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return projectstate.Project{}, fmt.Errorf("read %s: %w", path, err)
	}
	p, ok, err := projectstate.DecodeProjectJSON(raw, "")
	if err != nil {
		return projectstate.Project{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if !ok {
		return projectstate.Project{}, fmt.Errorf("%s is empty", path)
	}
	return p, nil
}

// readSpecFile reads a spec written by task-spec. Its hash must be its content's: a
// spec edited after it was resolved is refused rather than run or recorded under a
// hash the server would not compute.
func readSpecFile(path string) (projectstate.TaskSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return projectstate.TaskSpec{}, fmt.Errorf("read the spec: %w", err)
	}
	var spec projectstate.TaskSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return projectstate.TaskSpec{}, fmt.Errorf("%s is not a task spec: %w", path, err)
	}
	if got := spec.ComputeHash(); spec.Hash != got {
		return projectstate.TaskSpec{}, fmt.Errorf("%s carries hash %q but its content hashes to %q: the spec was changed after task-spec resolved it", path, spec.Hash, got)
	}
	return spec, nil
}
