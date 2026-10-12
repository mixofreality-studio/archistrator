// Package testinfra is the scenario hooks' shared venue double over this state tool. The
// delivery Manager's and the MCP client's hooks files both run the REAL local executor,
// which holds every lifecycle run to its agent task spec with the REAL state tool
// (cmd/aiarch-state-mcp: task-spec, edit-scope, record-check-run). What the venues double
// is what lies outside this repo: the coding agent, and the toolchains the check gate and
// the scenario-test generation run (RunChecksDouble, RunMakeDouble). The helpers live here
// once rather than once per hooks file.
package testinfra

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// VenueJob is one agent run as the venue started it: its job mode, command and activity,
// the worktree it runs in and the rig env the dispatch stamped (AIARCH_*).
type VenueJob struct {
	Mode     string
	Command  string
	Activity string
	Dir      string
	Env      map[string]string
}

// ToolEnv is the state tool's process env for one run: PATH and HOME, then the rig.
func ToolEnv(j VenueJob) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for k, v := range j.Env {
		env = append(env, k+"="+v)
	}
	return env
}

// ResolveTaskSpec runs the state tool's task-spec over the job's worktree for the task the
// dispatch names (AIARCH_TASK_ID), writes it to out and returns its hash — what the
// construct and design templates do before the agent starts.
func ResolveTaskSpec(tool string, j VenueJob, out string) (string, error) {
	task := j.Env["AIARCH_TASK_ID"]
	if task == "" {
		return "", fmt.Errorf("the %s dispatch of %s carries no task id (AIARCH_TASK_ID)", j.Command, j.Activity)
	}
	cmd := exec.Command(tool, "task-spec", "--activity", j.Activity, "--task", task, "--root", j.Dir, "--out", out)
	cmd.Dir = j.Dir
	cmd.Env = ToolEnv(j)
	got, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("task-spec %s/%s: %w", j.Activity, task, err)
	}
	return strings.TrimSpace(string(got)), nil
}

// WritePassingResults writes run-checks' results file for the spec at specPath: one
// passing result per check, which is what a green gate run records.
func WritePassingResults(specPath, out string) (string, error) {
	spec, err := ReadSpec(specPath)
	if err != nil {
		return "", err
	}
	results := make([]projectstate.CheckResult, 0, len(spec.Checks))
	for _, c := range spec.Checks {
		results = append(results, projectstate.CheckResult{CheckId: c.ID, Kind: c.Kind, Status: "pass", DurationMs: 1})
	}
	b, err := json.Marshal(results)
	if err != nil {
		return "", err
	}
	return out, os.WriteFile(out, b, 0o600)
}

// ReadSpec reads a spec file task-spec wrote.
func ReadSpec(specPath string) (projectstate.TaskSpec, error) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return projectstate.TaskSpec{}, err
	}
	var spec projectstate.TaskSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return projectstate.TaskSpec{}, err
	}
	return spec, nil
}

// WriteSpec writes spec to specPath re-hashed, so a hand-narrowed spec (a check removed to
// stage a run that never ran it) is still one record-check-run accepts.
func WriteSpec(specPath string, spec projectstate.TaskSpec) error {
	spec.Hash = spec.ComputeHash()
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(specPath, append(b, '\n'), 0o600)
}

// RunChecksDouble is the venues' `aiarch-state-mcp run-checks`: the check gate's runner
// runs toolchains (go build, golangci-lint, the scenario tests themselves) that a scenario
// venue has no module for, so the double writes what a green gate run writes — one passing
// result per check the spec at --spec holds, in --out/results.json — prints run-checks'
// status lines and exits 0. args are run-checks' own (after the subcommand).
func RunChecksDouble(args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("run-checks", flag.ContinueOnError)
	flags.Bool("gate", false, "")
	flags.Bool("failed-only", false, "")
	specPath := flags.String("spec", "", "")
	out := flags.String("out", "", "")
	if err := flags.Parse(args); err != nil || *specPath == "" || *out == "" {
		_, _ = fmt.Fprintln(os.Stderr, "venue run-checks: --spec and --out are required")
		return 2
	}
	if err := os.MkdirAll(*out, 0o750); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "venue run-checks:", err)
		return 2
	}
	if _, err := WritePassingResults(*specPath, filepath.Join(*out, "results.json")); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "venue run-checks:", err)
		return 2
	}
	spec, err := ReadSpec(*specPath)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "venue run-checks:", err)
		return 2
	}
	for _, c := range spec.Checks {
		_, _ = fmt.Fprintf(stdout, "%-7s %s\n", "pass", c.ID)
	}
	return 0
}

// RunMakeDouble is the venues' `make`: the executor runs `make gen-tests` before an
// integration agent, which regenerates the app's scenario tests from its plan — a toolchain
// step a scenario venue has no module for. The double accepts exactly that target.
func RunMakeDouble(args []string) int {
	if !slices.Equal(args, []string{"gen-tests"}) {
		_, _ = fmt.Fprintf(os.Stderr, "venue make: no target %q (the venue runs only gen-tests)\n", strings.Join(args, " "))
		return 2
	}
	return 0
}

// WorkPath is where a scripted construction step commits its work: inside the task's edit
// scope, as a real agent's change is — under the first component directory the spec lets
// it edit, anywhere for an any-path scope, and among the state files for a state-only task
// (the one path every scope permits).
func WorkPath(spec projectstate.TaskSpec, activity, command string) string {
	name := "scripted-" + command + ".md"
	for _, c := range spec.EditScope.Components {
		for _, p := range c.Paths {
			if dir, ok := strings.CutSuffix(p, "/**"); ok {
				return path.Join(dir, name)
			}
		}
	}
	if slices.Contains(spec.EditScope.Extra, "**") {
		return path.Join("work", activity, name)
	}
	return path.Join(".aiarch/state/work", activity, name)
}
