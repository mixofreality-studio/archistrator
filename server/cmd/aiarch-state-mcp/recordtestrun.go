package main

// recordtestrun.go — the `record-test-run` ONE-SHOT subcommand the construction
// VENUE runs after the deterministic scenario tests (deterministic component testing
// design, §6.1 run record / §6.2 venue). It is the venue's half of recordTestingState:
// the agent never writes a TestRun — the run is a measured fact of the venue, so the
// workflow step records it from the test-results directory the scenario host flushed:
//
//	aiarch-state-mcp record-test-run --activity A --component C --run-id R \
//	    --artifact NAME --results DIR [--revision REV]
//
// DIR/<component>/results.json is the scenariohost.ScenarioResult[] index the
// generated TestMain (server) or the Playwright reporter (uitests) wrote. The run is
// recorded with PhaseArtifactPayload{TestRun: &run} through the SAME construction-
// mutation path recordPhaseArtifact / recordTestingState use (applyConstructionMutation:
// strict codec read-back + methodcheck, then the working-tree write), so a run that
// survives this subcommand is byte-for-byte what the server reads back; publishDraft /
// the workflow's commit step carries it to the target branch as usual.
//
// LOUDNESS. An empty test-results directory, a results.json with zero entries, an
// entry without a scenario id or with a status outside pass|fail|skip, and a run
// without a revision are each a HARD failure (exit 1). A silently recorded empty or
// unanchored run would read as "nothing failed" to the testing gate (Review Focus 1
// and 5); failing the step is the venue's only honest signal.

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// envRevision is the construction revision the venue stamps on the job (the commit
// the scenario tests ran against). --revision overrides it; one of the two is required.
const envRevision = "AIARCH_REVISION"

// resultsIndexFile is the per-component index the scenario host flushes.
const resultsIndexFile = "results.json"

// testRunSink receives the assembled run. nil selects the real working-tree write
// (recordTestRunToCheckout); tests inject a capture.
type testRunSink func(projectstate.TestRun) error

// runRecordTestRun parses the flags, reads DIR/<component>/results.json, assembles the
// TestRun and hands it to sink (the checkout write when nil).
func runRecordTestRun(args []string, sink testRunSink) error {
	flags := flag.NewFlagSet("record-test-run", flag.ContinueOnError)
	activity := flags.String("activity", "", "activity id the tests belong to (e.g. C-BG)")
	component := flags.String("component", "", "component the scenarios were bound for; names DIR/<component>/results.json")
	runID := flags.String("run-id", "", "venue run id (GitHub run id or local uuid)")
	artifact := flags.String("artifact", "", "where the test-results tree lives: the GitHub artifact name or the local path")
	results := flags.String("results", "", "the test-results directory the scenario host flushed into")
	revision := flags.String("revision", "", "construction revision tested (default: $"+envRevision+")")
	if err := flags.Parse(args); err != nil {
		return err
	}
	for _, f := range []struct{ name, value string }{
		{"activity", *activity}, {"component", *component}, {"run-id", *runID}, {"artifact", *artifact}, {"results", *results},
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
		return fmt.Errorf("--revision is required when %s is unset: a run without the construction revision it tested can never satisfy the testing gate", envRevision)
	}

	run, err := buildTestRun(*results, strings.TrimSpace(*component))
	if err != nil {
		return err
	}
	run.ID = strings.TrimSpace(*runID)
	run.Activity = strings.TrimSpace(*activity)
	run.Revision = rev
	run.Artifact = strings.TrimSpace(*artifact)

	if sink == nil {
		sink = recordTestRunToCheckout
	}
	return sink(run)
}

// buildTestRun reads DIR/<component>/results.json into the run's scenarios and derives
// StartedAt / EndedAt from the mtimes of everything under DIR/<component> (the earliest
// file the host wrote and the last). The caller stamps the identity fields.
func buildTestRun(dir, component string) (projectstate.TestRun, error) {
	componentDir := filepath.Join(dir, component)
	indexPath := filepath.Join(componentDir, resultsIndexFile)
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return projectstate.TestRun{}, fmt.Errorf("no %s under %s: the scenario host wrote nothing for component %q — the tests did not run, refusing to record an empty run", resultsIndexFile, componentDir, component)
		}
		return projectstate.TestRun{}, fmt.Errorf("read %s: %w", indexPath, err)
	}
	var entries []scenariohost.ScenarioResult
	if err := json.Unmarshal(raw, &entries); err != nil {
		return projectstate.TestRun{}, fmt.Errorf("%s is not a scenariohost results index ([]ScenarioResult): %w", indexPath, err)
	}
	if len(entries) == 0 {
		return projectstate.TestRun{}, fmt.Errorf("%s holds no scenarios: nothing was bound or run for component %q, refusing to record an empty run", indexPath, component)
	}
	scenarios := make([]projectstate.ScenarioResult, 0, len(entries))
	for i, e := range entries {
		if strings.TrimSpace(e.Scenario) == "" {
			return projectstate.TestRun{}, fmt.Errorf("%s entry %d has no scenario id", indexPath, i)
		}
		switch e.Status {
		case scenariohost.StatusPass, scenariohost.StatusFail, scenariohost.StatusSkip:
		default:
			return projectstate.TestRun{}, fmt.Errorf("%s entry %q has status %q (want %s|%s|%s)", indexPath, e.Scenario, e.Status,
				scenariohost.StatusPass, scenariohost.StatusFail, scenariohost.StatusSkip)
		}
		scenarios = append(scenarios, projectstate.ScenarioResult{
			Scenario:   e.Scenario,
			Status:     e.Status,
			DurationMs: e.DurationMs,
			VideoRef:   e.VideoRef,
			TraceRef:   e.TraceRef,
		})
	}
	started, ended, err := mtimeSpan(componentDir)
	if err != nil {
		return projectstate.TestRun{}, err
	}
	return projectstate.TestRun{
		Component: component,
		StartedAt: &started,
		EndedAt:   &ended,
		Scenarios: scenarios,
	}, nil
}

// mtimeSpan returns the earliest and latest modification time of the regular files
// under root — the only timestamps the venue leaves behind for the run's duration.
func mtimeSpan(root string) (earliest, latest time.Time, err error) {
	err = filepath.WalkDir(root, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		m := info.ModTime().UTC()
		if earliest.IsZero() || m.Before(earliest) {
			earliest = m
		}
		if m.After(latest) {
			latest = m
		}
		return nil
	})
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("scan %s: %w", root, err)
	}
	return earliest, latest, nil
}

// recordTestRunToCheckout is the real sink: a construct-mode session over the ambient
// env (state root, project id, target branch — exactly what the construct job already
// stamps) writes the run through applyConstructionMutation, the same gate
// recordTestingState passes. The job mode is FORCED to construct: this subcommand only
// ever runs in a construction venue, and the construct mode is the one that needs no
// ambient artifact kind. The run's own activity is the ambient activity.
func recordTestRunToCheckout(run projectstate.TestRun) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	getenv := func(k string) string {
		if k == envJobMode {
			return jobModeConstruct
		}
		return os.Getenv(k)
	}
	s, err := newSessionFromEnv(getenv, wd)
	if err != nil {
		return err
	}
	if s.ActivityID == "" {
		s.ActivityID = run.Activity
	}
	payload := projectstate.PhaseArtifactPayload{TestRun: &run}
	return s.applyConstructionMutation("test run", func(p *projectstate.Project) error {
		projectstate.ApplyPhaseArtifactPayload(p, "", payload)
		return nil
	})
}
