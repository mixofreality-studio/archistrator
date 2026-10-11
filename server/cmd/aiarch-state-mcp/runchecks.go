package main

// runchecks.go — the two entries to the check runner (checks.go), which both use it
// unchanged (R8):
//
//   - the `run-checks` ONE-SHOT subcommand, the venue's check gate:
//
//     aiarch-state-mcp run-checks [--gate] --spec FILE --out DIR [--failed-only]
//
//     It runs the spec's checks in the checkout (cwd) and writes DIR/results.json (one
//     CheckResult per spec check, in spec order) beside the per-check logs under
//     DIR/checks/. --gate deletes DIR first and runs every check, so nothing an earlier
//     run left can count. --failed-only re-runs only the checks DIR/results.json did not
//     pass and keeps the others' verdicts (everything runs when there is none). Exit 0
//     when every check passed, 1 when one did not, 2 when the runner itself failed.
//
//   - the `run_checks` MCP verb, the agent's own pre-flight of that gate. It takes no
//     ids: the session's activity and task (AIARCH_ACTIVITY_ID / AIARCH_TASK_ID) resolve
//     the spec from the checkout's project.json, exactly as task-spec does. It keeps the
//     previous call's results in memory for {failedOnly: true}, returns the failures
//     only (each with its log's tail), and RECORDS NOTHING: a CheckRun is the venue's.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

const (
	runChecksTool = "run_checks"
	// resultsFile is the run's index of verdicts under the results dir.
	resultsFile = "results.json"
	// tailLines / tailBytes cap the log tail the verb returns per failure.
	tailLines = 40
	tailBytes = 4 << 10
)

// checkExecFunc runs one check process (checkRunner.exec's shape).
type checkExecFunc = func(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error)

// runSpecChecks runs checks — only those prev did not pass when onlyFailed and prev is
// non-nil — and returns one result per check in checks' order: a check that did not
// re-run keeps its previous verdict.
func runSpecChecks(ctx context.Context, r checkRunner, checks []projectstate.SpecCheck, prev []CheckResult, onlyFailed bool) []CheckResult {
	run := checks
	if onlyFailed && prev != nil {
		run = failedOnly(prev, checks)
	}
	fresh := map[string]CheckResult{}
	for _, res := range r.Run(ctx, run) {
		fresh[res.CheckId] = res
	}
	old := make(map[string]CheckResult, len(prev))
	for _, res := range prev {
		old[res.CheckId] = res
	}
	out := make([]CheckResult, 0, len(checks))
	for _, c := range checks {
		if res, ok := fresh[c.ID]; ok {
			out = append(out, res)
		} else if res, ok := old[c.ID]; ok {
			out = append(out, res)
		}
	}
	return out
}

// writeCheckResults writes dir/results.json.
func writeCheckResults(dir string, results []CheckResult) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, resultsFile), append(b, '\n'), 0o600)
}

// readPrevResults reads dir/results.json; no file is nil.
func readPrevResults(dir string) ([]CheckResult, error) {
	raw, err := os.ReadFile(filepath.Join(dir, resultsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []CheckResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode %s: %w", filepath.Join(dir, resultsFile), err)
	}
	return out, nil
}

// ---- run-checks (the venue's gate) ----

// runChecksArgs are run-checks' flags, checked, with --out made absolute.
type runChecksArgs struct {
	gate, onlyFailed bool
	spec             projectstate.TaskSpec
	out              string
}

func parseRunChecksArgs(args []string) (runChecksArgs, error) {
	flags := flag.NewFlagSet("run-checks", flag.ContinueOnError)
	gate := flags.Bool("gate", false, "the venue's gate: start from an empty --out and run every check")
	specPath := flags.String("spec", "", "the task spec file task-spec wrote")
	outDir := flags.String("out", "", "the results dir (results.json + checks/<id>.log)")
	onlyFailed := flags.Bool("failed-only", false, "re-run only the checks --out's results.json did not pass")
	if err := flags.Parse(args); err != nil {
		return runChecksArgs{}, err
	}
	if strings.TrimSpace(*specPath) == "" || strings.TrimSpace(*outDir) == "" {
		return runChecksArgs{}, errors.New("--spec and --out are required")
	}
	spec, err := readSpecFile(*specPath)
	if err != nil {
		return runChecksArgs{}, err
	}
	out, err := filepath.Abs(*outDir)
	if err != nil {
		return runChecksArgs{}, err
	}
	return runChecksArgs{gate: *gate, onlyFailed: *onlyFailed && !*gate, spec: spec, out: out}, nil
}

// runRunChecks is the run-checks subcommand; see the file comment.
func runRunChecks(args []string, stdout io.Writer) error {
	a, err := parseRunChecksArgs(args)
	if err != nil {
		return exitError{2, err}
	}
	root, err := os.Getwd()
	if err != nil {
		return exitError{2, err}
	}
	var prev []CheckResult
	switch {
	case a.gate:
		if err := os.RemoveAll(a.out); err != nil {
			return exitError{2, fmt.Errorf("clear %s: %w", a.out, err)}
		}
	case a.onlyFailed:
		if prev, err = readPrevResults(a.out); err != nil {
			return exitError{2, err}
		}
	}
	results := runSpecChecks(context.Background(), checkRunner{root: root, outDir: a.out}, a.spec.Checks, prev, a.onlyFailed)
	if err := writeCheckResults(a.out, results); err != nil {
		return exitError{2, fmt.Errorf("write the results: %w", err)}
	}
	return reportCheckResults(stdout, results, a.out)
}

// reportCheckResults prints one status line per check and fails with exit 1 when any
// check did not pass.
func reportCheckResults(stdout io.Writer, results []CheckResult, out string) error {
	red := 0
	for _, r := range results {
		if _, err := fmt.Fprintf(stdout, "%-7s %s\n", r.Status, r.CheckId); err != nil {
			return exitError{2, err}
		}
		if r.Status != checkPass {
			red++
		}
	}
	if red > 0 {
		return exitError{1, fmt.Errorf("%d of %d checks did not pass (logs under %s)", red, len(results), filepath.Join(out, "checks"))}
	}
	return nil
}

// ---- run_checks (the agent's verb) ----

// checkSessionState is run_checks' memory across calls in one session.
type checkSessionState struct {
	mu   sync.Mutex
	last []CheckResult
}

type runChecksInput struct {
	FailedOnly bool `json:"failedOnly,omitempty" jsonschema:"re-run only the checks that did not pass on this session's previous run_checks call (every check when there was none)"`
}

// runChecksFailure is one check that did not pass, with the tail of its log.
type runChecksFailure struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Tail   string `json:"tail"`
}

// runChecksOutput is the verb's structured result: the counts over the whole spec and
// the failures alone.
type runChecksOutput struct {
	Passed  int                `json:"passed"`
	Failed  int                `json:"failed"`
	Results []runChecksFailure `json:"results"`
	LogRef  string             `json:"logRef"`
}

// checksOutDir is where the verb's runs write, repo-relative: self-ignoring scratch
// (ignoreScratchDir) the state tools never stage.
var checksOutDir = filepath.Join(".aiarch", "tmp", "checks")

// runChecks resolves this session's task spec and runs its checks.
func (s *Session) runChecks(ctx context.Context, onlyFailed bool) (runChecksOutput, error) {
	switch {
	case s.ActivityID == "":
		return runChecksOutput{}, fmt.Errorf("run_checks needs the ambient activity (%s); this session has none, so it has no task spec", envActivityID)
	case s.TaskID == "":
		return runChecksOutput{}, fmt.Errorf("run_checks needs the ambient task (%s); this session has none, so it has no task spec", envTaskID)
	}
	proj, _, err := s.readProject()
	if err != nil {
		return runChecksOutput{}, err
	}
	spec, err := projectstate.TaskSpecFor(&proj, s.ActivityID, s.TaskID)
	if err != nil {
		return runChecksOutput{}, err
	}
	s.checks.mu.Lock()
	defer s.checks.mu.Unlock()
	if err := ignoreScratchDir(filepath.Join(s.StateRoot, filepath.Dir(checksOutDir))); err != nil {
		return runChecksOutput{}, err
	}
	out := filepath.Join(s.StateRoot, checksOutDir)
	r := checkRunner{root: s.StateRoot, outDir: out, exec: s.checkExec}
	results := runSpecChecks(ctx, r, spec.Checks, s.checks.last, onlyFailed)
	s.checks.last = results
	if err := writeCheckResults(out, results); err != nil {
		return runChecksOutput{}, fmt.Errorf("write the results: %w", err)
	}
	titles := make(map[string]string, len(spec.Checks))
	for _, c := range spec.Checks {
		titles[c.ID] = c.Title
	}
	res := runChecksOutput{Results: []runChecksFailure{}, LogRef: filepath.Join(checksOutDir, "checks")}
	for _, c := range results {
		if c.Status == checkPass {
			res.Passed++
			continue
		}
		res.Failed++
		res.Results = append(res.Results, runChecksFailure{ID: c.CheckId, Title: titles[c.CheckId], Status: c.Status, Tail: s.checkLog(c)})
	}
	return res, nil
}

// ignoreScratchDir creates dir with a self-ignoring .gitignore ("*"), the way the
// episode store keeps .aiarch/traces out of git: an operated repo's own .gitignore need
// not know the dir, so an agent's `git add -A` can never commit the verb's scratch.
func ignoreScratchDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte("*\n"), 0o600)
}

// checkLog is the tail of c's log, or a note when it has none.
func (s *Session) checkLog(c CheckResult) string {
	if c.OutputRef == nil {
		return "(no log was written)"
	}
	p := *c.OutputRef
	if !filepath.IsAbs(p) {
		p = filepath.Join(s.StateRoot, p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "(log unreadable: " + err.Error() + ")"
	}
	return checkLogTail(string(b))
}

// checkLogTail is at most the last tailLines lines and tailBytes bytes of log.
func checkLogTail(log string) string {
	if len(log) > tailBytes {
		log = log[len(log)-tailBytes:]
		if i := strings.IndexByte(log, '\n'); i >= 0 {
			log = log[i+1:]
		}
	}
	lines := strings.SplitAfter(log, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}
	return strings.Join(lines, "")
}

// runChecksText is the verb's text: the counts, each failure with its tail, and where
// the full logs are. Passing checks are never listed.
func runChecksText(o runChecksOutput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d passed, %d failed.\n", o.Passed, o.Failed)
	for _, f := range o.Results {
		fmt.Fprintf(&b, "\n✗ %s [%s]\n%s", f.Title, f.Status, f.Tail)
		if !strings.HasSuffix(f.Tail, "\n") {
			b.WriteByte('\n')
		}
	}
	fmt.Fprintf(&b, "\nFull logs: %s\n", o.LogRef)
	return b.String()
}

// registerRunChecks adds the run_checks verb to srv.
func registerRunChecks(srv *mcp.Server, s *Session) {
	mcp.AddTool(srv, &mcp.Tool{
		Name: runChecksTool,
		Description: "Run THIS task's checks — the same checks, by the same runner, that the job's gate runs after you finish — and report the ones that do not pass, each with the tail of its log. " +
			"You pass no ids: the task is fixed by the job. Run it before you publish, fix what fails, then call it again with failedOnly: true to re-run only what failed. " +
			"It records nothing; the gate's run is what counts.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in runChecksInput) (*mcp.CallToolResult, runChecksOutput, error) {
		out, err := s.runChecks(ctx, in.FailedOnly)
		if err != nil {
			return nil, runChecksOutput{}, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: runChecksText(out)}}}, out, nil
	})
}
