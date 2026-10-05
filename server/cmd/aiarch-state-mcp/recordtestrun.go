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
// DIR/<contract>/results.json, for every contract of the component, is the
// scenariohost.ScenarioResult[] index the
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
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// envRevision is the revision the run is "at": the integration attempt id
// ("<activity>:integration:<n>") the dispatch stamps on the job, which the testing gate
// matches against the attempt its round judges (DCT plan 5 r2 ruling: the walk holds
// the attempt, never a commit SHA, and a SHA could not name the commit that records
// the run anyway). --revision overrides it; one of the two is required.
const envRevision = "AIARCH_REVISION"

// resultsIndexFile is the per-component index the scenario host flushes.
const resultsIndexFile = "results.json"

// testRunSink receives each assembled run. nil selects the real working-tree write
// (recordTestRunInSession over the ambient construct session, which also lets the
// facets of a component resolve against the committed project); tests inject a
// capture.
type testRunSink func(projectstate.TestRun) error

// recordTestRunArgs are record-test-run's flags, trimmed and checked.
type recordTestRunArgs struct {
	activity, component, runID, artifact, results, revision string
}

// parseRecordTestRunArgs parses and checks the flags; the revision falls back to
// AIARCH_REVISION, and one of the two is required.
func parseRecordTestRunArgs(args []string) (recordTestRunArgs, error) {
	flags := flag.NewFlagSet("record-test-run", flag.ContinueOnError)
	activity := flags.String("activity", "", "activity id the tests belong to (e.g. C-BG)")
	component := flags.String("component", "", "the activity's component (kebab or contract name); every contract of it with a DIR/<contract>/results.json is recorded")
	runID := flags.String("run-id", "", "venue run id (GitHub run id or local uuid)")
	artifact := flags.String("artifact", "", "where the test-results tree lives: the GitHub artifact name or the local path")
	results := flags.String("results", "", "the test-results directory the scenario host flushed into")
	revision := flags.String("revision", "", "the integration attempt the run was produced under (default: $"+envRevision+", which the dispatch stamps)")
	if err := flags.Parse(args); err != nil {
		return recordTestRunArgs{}, err
	}
	a := recordTestRunArgs{
		activity:  strings.TrimSpace(*activity),
		component: strings.TrimSpace(*component),
		runID:     strings.TrimSpace(*runID),
		artifact:  strings.TrimSpace(*artifact),
		results:   strings.TrimSpace(*results),
		revision:  strings.TrimSpace(*revision),
	}
	for _, f := range []struct{ name, value string }{
		{"component", a.component}, {"run-id", a.runID}, {"artifact", a.artifact}, {"results", a.results},
	} {
		if f.value == "" {
			return recordTestRunArgs{}, fmt.Errorf("--%s is required", f.name)
		}
	}
	if a.revision == "" {
		a.revision = strings.TrimSpace(os.Getenv(envRevision))
	}
	if a.revision == "" {
		return recordTestRunArgs{}, fmt.Errorf("--revision is required when %s is unset: a run without the integration attempt it was produced under can never satisfy the testing gate", envRevision)
	}
	return a, nil
}

// runRecordTestRun parses the flags, reads DIR/<contract>/results.json for every
// contract of --component (resultContracts), assembles one TestRun per contract and
// hands each to sink (the checkout write when nil). A component with several
// contracts records one run per contract, its id suffixed "-<contract>".
func runRecordTestRun(args []string, sink testRunSink) error {
	a, err := parseRecordTestRunArgs(args)
	if err != nil {
		return err
	}
	if !anyResultsIndex(a.results) {
		return fmt.Errorf("no %s under %s: the scenario host wrote nothing — the tests did not run, refusing to record an empty run", resultsIndexFile, a.results)
	}
	proj, write, err := testRunWriter(sink)
	if err != nil {
		return err
	}
	if err := checkRunActivity(a.activity, a.component, proj); err != nil {
		return err
	}
	contracts, err := resultContracts(a.results, a.component, proj)
	if err != nil {
		return err
	}
	runs := make([]projectstate.TestRun, 0, len(contracts))
	for _, contract := range contracts {
		run, err := buildTestRun(a.results, contract)
		if err != nil {
			return err
		}
		run.ID = a.runID
		if len(contracts) > 1 {
			run.ID += "-" + contract
		}
		run.Activity, run.Revision, run.Artifact = a.activity, a.revision, a.artifact
		runs = append(runs, run)
	}
	for _, run := range runs {
		if err := write(run); err != nil {
			return err
		}
	}
	return nil
}

// testRunWriter is where the runs go: an injected sink as is (no project — the
// contracts then resolve from the results directory alone), or, for nil, the
// ambient construct session's checkout write, together with the committed project
// that lets a component's facets resolve.
func testRunWriter(sink testRunSink) (*projectstate.Project, testRunSink, error) {
	if sink != nil {
		return nil, sink, nil
	}
	s, err := constructSessionFromEnv()
	if err != nil {
		return nil, nil, err
	}
	p, _, err := s.readProject()
	if err != nil {
		return nil, nil, err
	}
	return &p, func(run projectstate.TestRun) error { return recordTestRunInSession(s, run) }, nil
}

// checkRunActivity holds --activity to the plan. A run names the activity whose
// integration produced it, and the testing gate judges an activity by its latest
// run, so a run recorded under ANOTHER component's activity would hide that
// activity's own. A component the derived plan builds no activity for (a provided
// utility such as messageBus, a generated client such as mcpClient's facets) has no
// gate to feed: its run is the component's evidence alone and carries no activity
// (DCT plan 5 r2). So --activity may be empty exactly when the committed activity
// list holds no activity for the component, and is required otherwise.
func checkRunActivity(activity, component string, proj *projectstate.Project) error {
	if activity != "" {
		return nil
	}
	if proj == nil {
		return fmt.Errorf("--activity is required")
	}
	if owner := activityOfComponent(proj, component); owner != "" {
		return fmt.Errorf("--activity is required: component %q is built by activity %q", component, owner)
	}
	return nil
}

// activityOfComponent names the committed activity-list entry that builds component
// (its componentId, or a contract of it, normalizes to it), or "" when none does.
func activityOfComponent(proj *projectstate.Project, component string) string {
	list, ok := proj.ActivityList.Model.(*projectstate.ActivityList)
	if !ok || list == nil {
		return ""
	}
	want := map[string]bool{scenario.Normalize(component): true}
	if sc, ok := proj.ServiceContracts[component]; ok && sc.Component != "" {
		want[scenario.Normalize(sc.Component)] = true
	}
	for _, it := range list.Activities {
		if it.ComponentID != "" && want[scenario.Normalize(it.ComponentID)] {
			return it.Name
		}
	}
	return ""
}

// anyResultsIndex reports whether some DIR/<contract>/results.json exists — checked
// before anything else, so a run that wrote nothing fails as loudly in an empty
// checkout as in a seated one.
func anyResultsIndex(dir string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, "*", resultsIndexFile))
	return err == nil && len(matches) > 0
}

// resultContracts names the contracts whose results the run records, each the name
// of a DIR/<contract>/results.json the scenario host flushed. The venue names the
// activity's component (kebab, "review-engine") while the host writes under the
// contract name ("reviewEngine"), so a directory matches when its name normalizes
// to the component's (scenario.Normalize, the join the plan view uses). With the
// committed project at hand, every contract whose component normalizes to it
// matches too — the facets of one component (projectStateAccess's four), which one
// integration runs together — and a facet the project binds a test plan for but the
// host wrote nothing for is a hard failure, like an empty run: its tests did not
// run. --component may also name one contract, which then matches alone.
func resultContracts(dir, component string, proj *projectstate.Project) ([]string, error) {
	want := scenario.Normalize(component)
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	present := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			present[e.Name()] = true
		}
	}
	matched := map[string]bool{}
	for name := range present {
		if scenario.Normalize(name) == want {
			matched[name] = true
		}
	}
	if proj != nil {
		for name, sc := range proj.ServiceContracts {
			if scenario.Normalize(sc.Component) != want {
				continue
			}
			if present[name] {
				matched[name] = true
				continue
			}
			if proj.PhaseArtifacts != nil {
				if _, bound := proj.PhaseArtifacts.TestPlan[name]; bound {
					return nil, fmt.Errorf("no %s under %s: contract %q of component %q has a test plan but the scenario host wrote nothing for it — its tests did not run, refusing to record a partial run",
						resultsIndexFile, filepath.Join(dir, name), name, component)
				}
			}
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("no %s under %s for component %q: the scenario host wrote nothing for it — the tests did not run, refusing to record an empty run",
			resultsIndexFile, dir, component)
	}
	return slices.Sorted(maps.Keys(matched)), nil
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

// constructSessionFromEnv is the session the real write goes through: a
// construct-mode session over the ambient env (state root, project id, target branch —
// exactly what the construct job already stamps). The job mode is FORCED to construct:
// this subcommand only ever runs in a construction venue, and the construct mode is the
// one that needs no ambient artifact kind.
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

// recordTestRunInSession writes the run through applyConstructionMutation, the same
// gate recordTestingState passes. The run's own activity is the ambient activity when
// the env names none.
func recordTestRunInSession(s *Session, run projectstate.TestRun) error {
	if s.ActivityID == "" {
		s.ActivityID = run.Activity
	}
	payload := projectstate.PhaseArtifactPayload{TestRun: &run}
	return s.applyConstructionMutation("test run", func(p *projectstate.Project) error {
		projectstate.ApplyPhaseArtifactPayload(p, "", payload)
		return nil
	})
}
