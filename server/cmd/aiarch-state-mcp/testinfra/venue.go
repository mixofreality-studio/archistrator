// Package testinfra is the scenario hooks' shared venue double over this state tool: what the seated
// construct and design workflows do around an agent run, reduced to the calls a scripted
// agent makes against the venue's REAL state tool (cmd/aiarch-state-mcp). The delivery
// Manager's and the MCP client's hooks files both drive the same venue, so the agent task
// spec steps live here once rather than once per hooks file.
package testinfra

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// VerifyTaskSpec is the templates' first gate: the spec the job resolves over its
// checkout must hash to the spec_hash the Manager dispatched, so what the panel shows is
// what runs.
//
// An ANSWER job is exempt: it answers an AskQuestions round (AnswerJobWorkflow), which is
// no lifecycle task, so the Manager resolves and dispatches no spec for it.
func VerifyTaskSpec(tool string, j VenueJob) error {
	if j.Mode == "answer" {
		return nil
	}
	want := j.Env["AIARCH_SPEC_HASH"]
	if len(want) != 64 {
		return fmt.Errorf("the %s dispatch of %s carries spec hash %q, want a sha256 hex digest", j.Command, j.Activity, want)
	}
	tmp, err := os.MkdirTemp("", "dct-spec-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	got, err := ResolveTaskSpec(tool, j, filepath.Join(tmp, "spec.json"))
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("task spec drifted for %s/%s: dispatched %s, the worktree resolves %s", j.Activity, j.Env["AIARCH_TASK_ID"], want, got)
	}
	return nil
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
