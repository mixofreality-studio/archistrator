package main

// recordcheckrun.go — the `record-check-run` ONE-SHOT subcommand the venue runs after
// the check gate. The agent never records a run; the venue measures it:
//
//	aiarch-state-mcp record-check-run --spec FILE --results FILE --run-id ID \
//	    --artifact NAME [--revision REV]
//
// It turns the gate's results.json (run-checks' []CheckResult) into the task's CheckRun
// on .testingState.checkRuns: the spec names the activity, task and spec hash, the
// flags the venue's run id, uploaded artifact and the dispatch attempt (--revision,
// else $AIARCH_REVISION; stored verbatim, R15). A run id recorded again replaces its
// earlier record.
//
// EVIDENCE, RED OR GREEN (deviation 5). The write is an evidence mutation
// (applyEvidenceMutation: codec read-back only, no methodcheck), because a red rule
// check means the document HAS Error findings, and the validated construction path
// would refuse to record exactly the run that shows them.
//
// LOUDNESS. Results that are empty, carry a status outside pass|fail|skip|timeout,
// name a check the spec does not hold, or miss one it does are refused (exit 1): a
// silently partial run would read as "nothing failed".

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// envRevision is the dispatch attempt id the job runs under ("<activity>:<task>:<n>"),
// stamped by the dispatch. --revision overrides it; one of the two is required.
const envRevision = "AIARCH_REVISION"

// runRecordCheckRun records the gate's results; see the file comment.
func runRecordCheckRun(args []string) error {
	flags := flag.NewFlagSet("record-check-run", flag.ContinueOnError)
	specPath := flags.String("spec", "", "the task spec file task-spec wrote")
	resultsPath := flags.String("results", "", "the results.json run-checks wrote")
	runID := flags.String("run-id", "", "the venue run id (gh-<run_id>-<attempt>, or a local id)")
	artifact := flags.String("artifact", "", "where the check results live: the uploaded artifact name or a local path")
	revision := flags.String("revision", "", "the dispatch attempt id the run was produced under (default: $"+envRevision+")")
	if err := flags.Parse(args); err != nil {
		return err
	}
	for _, f := range []struct{ name, value string }{
		{"spec", *specPath}, {"results", *resultsPath}, {"run-id", *runID}, {"artifact", *artifact},
	} {
		if strings.TrimSpace(f.value) == "" {
			return fmt.Errorf("--%s is required", f.name)
		}
	}
	rev := strings.TrimSpace(*revision)
	if rev == "" {
		rev = strings.TrimSpace(os.Getenv(envRevision))
	}
	if rev == "" {
		return fmt.Errorf("--revision is required when %s is unset: a run without the attempt it was produced under can never be matched to it", envRevision)
	}
	spec, err := readSpecFile(*specPath)
	if err != nil {
		return err
	}
	if spec.Activity == "" || spec.Task == "" {
		return fmt.Errorf("the spec names no activity or task (activity %q, task %q)", spec.Activity, spec.Task)
	}
	results, ended, err := readCheckResults(*resultsPath, spec)
	if err != nil {
		return err
	}
	total := 0
	for _, r := range results {
		total += r.DurationMs
	}
	started := ended.Add(-time.Duration(total) * time.Millisecond)
	now := time.Now().UTC()
	run := projectstate.CheckRun{
		ID: strings.TrimSpace(*runID), Activity: spec.Activity, Task: spec.Task, Revision: rev, SpecHash: spec.Hash,
		StartedAt: &started, EndedAt: &now, Artifact: strings.TrimSpace(*artifact), Results: results,
	}
	s, err := constructSessionFromEnv()
	if err != nil {
		return err
	}
	payload := projectstate.PhaseArtifactPayload{CheckRun: &run}
	return s.applyEvidenceMutation("check run", func(p *projectstate.Project) error {
		projectstate.ApplyPhaseArtifactPayload(p, "", payload)
		return nil
	})
}

// readCheckResults reads run-checks' results file and holds it to the spec: one result
// per spec check, each with a known status. It also returns the file's modification
// time, when the run ended.
func readCheckResults(path string, spec projectstate.TaskSpec) ([]CheckResult, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read the results: %w", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read the results: %w", err)
	}
	var results []CheckResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, time.Time{}, fmt.Errorf("%s is not run-checks results ([]CheckResult): %w", path, err)
	}
	if len(results) == 0 {
		return nil, time.Time{}, fmt.Errorf("%s holds no results: no check ran, refusing to record an empty run", path)
	}
	want := make(map[string]bool, len(spec.Checks))
	for _, c := range spec.Checks {
		want[c.ID] = true
	}
	seen := make(map[string]bool, len(results))
	var errs []error
	for i, r := range results {
		switch {
		case r.CheckId == "":
			errs = append(errs, fmt.Errorf("result %d has no checkId", i))
		case !want[r.CheckId]:
			errs = append(errs, fmt.Errorf("result %q names a check the spec does not hold", r.CheckId))
		case seen[r.CheckId]:
			errs = append(errs, fmt.Errorf("result %q appears twice", r.CheckId))
		}
		seen[r.CheckId] = true
		switch r.Status {
		case checkPass, checkFail, checkSkip, checkTimeout:
		default:
			errs = append(errs, fmt.Errorf("result %q has status %q (want %s|%s|%s|%s)", r.CheckId, r.Status, checkPass, checkFail, checkSkip, checkTimeout))
		}
	}
	for _, c := range spec.Checks {
		if !seen[c.ID] {
			errs = append(errs, fmt.Errorf("the spec's check %q has no result: it did not run", c.ID))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, time.Time{}, fmt.Errorf("%s does not match the spec's checks, refusing to record a partial run:\n%w", path, err)
	}
	return results, info.ModTime().UTC(), nil
}

// constructSessionFromEnv is the session the venue's write goes through: the ambient
// env (state root, project id) with the job mode FORCED to construct, the one mode
// that needs no ambient artifact kind. The design venue records through it too: the
// CheckRun is task evidence, not a design-slot write.
func constructSessionFromEnv() (*Session, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	getenv := func(k string) string {
		if k == envJobMode {
			return jobModeConstruct
		}
		return os.Getenv(k)
	}
	return newSessionFromEnv(getenv, wd)
}
