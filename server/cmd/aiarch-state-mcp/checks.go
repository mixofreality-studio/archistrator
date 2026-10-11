package main

// checks.go is the check runner behind `run-checks` and the `run_checks` verb: it runs a
// task spec's checks (projectstate.TaskSpec.Checks) against the checkout and returns one
// compact CheckResult per check, writing each check's full output to a log under the
// results directory. Three kinds:
//
//   - rule: the policy-aware Method gate (validateFindings, the same machinery as the
//     `validate` subcommand) filtered to the check's rule prefixes; it fails iff an Error
//     finding survives.
//   - command: one process in the checkout, capped at the check's TimeoutSec.
//   - scenario: the generated scenario tests of one contract, batched into one process
//     per contract (`go test` for a contract with a Go package, Playwright for a web
//     client), the verdicts read back from the scenario host's results.json index.
//
// Statuses are pass | fail | timeout (and skip, carried through from a scenario index).
// A check the runner cannot even start is a fail whose log says why: the runner always
// returns one result per check, so a red check can still be recorded.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// CheckResult is one check's compact verdict.
type CheckResult = projectstate.CheckResult

// Check statuses.
const (
	checkPass    = "pass"
	checkFail    = "fail"
	checkSkip    = "skip"
	checkTimeout = "timeout"
)

const (
	// defaultScenarioTimeoutSec is a scenario's cap when its check names none (R8).
	defaultScenarioTimeoutSec = 900
	// defaultCheckTimeoutSec caps a rule or command check that names none.
	defaultCheckTimeoutSec = 600
	// goTestSlackSec is added to a Go batch's summed caps for go test's own -timeout,
	// which otherwise defaults to 10m and panics a long multi-scenario run.
	goTestSlackSec = 60
	// batchSlackSec is added to a batch's summed caps for the process context: build,
	// browser start and the index flush happen outside any one scenario.
	batchSlackSec = 120
	// maxFallbackLog caps how much of a batch's output a scenario log falls back to.
	maxFallbackLog = 64 << 10
	// waitDelay bounds how long a cancelled process may keep its output pipes open
	// (a straggler that left the killed process group).
	waitDelay = 10 * time.Second
	// noVerdict is the log of a selected scenario its run recorded nothing for.
	noVerdict = "no verdict recorded: the run left no results.json entry for this scenario"
)

// checkRunner runs checks against one checkout.
type checkRunner struct {
	root   string // repo root (the checkout)
	outDir string // results dir; logs at outDir/checks/<sanitized id>.log
	exec   func(ctx context.Context, dir string, env []string, argv ...string) (out []byte, err error)
	now    func() time.Time
}

// Run runs the given checks; scenario checks are batched per contract into one process.
// Results are in check order.
func (r checkRunner) Run(ctx context.Context, checks []projectstate.SpecCheck) []CheckResult {
	if r.exec == nil {
		r.exec = execCommand
	}
	if r.now == nil {
		r.now = time.Now
	}
	results := make([]CheckResult, len(checks))
	batched := map[string]bool{}
	for i, c := range checks {
		switch c.Kind {
		case projectstate.CheckKindRule:
			results[i] = r.runRule(ctx, c)
		case projectstate.CheckKindCommand:
			results[i] = r.runCommand(ctx, c)
		case projectstate.CheckKindScenario:
			contract := c.Source.Contract
			if batched[contract] {
				continue
			}
			batched[contract] = true
			var idx []int
			for j, o := range checks {
				if o.Kind == projectstate.CheckKindScenario && o.Source.Contract == contract {
					idx = append(idx, j)
				}
			}
			batch := make([]projectstate.SpecCheck, len(idx))
			for k, j := range idx {
				batch[k] = checks[j]
			}
			for k, res := range r.runScenarios(ctx, contract, batch) {
				results[idx[k]] = res
			}
		default:
			results[i] = r.finish(c, checkFail, 0, fmt.Appendf(nil, "unknown check kind %q", c.Kind))
		}
	}
	return results
}

// failedOnly is the checks of all whose previous result was not a pass (fail, timeout,
// skip, or no result at all), in all's order. A skip is not a pass anywhere (R24), so it
// re-runs like any other red check.
func failedOnly(prev []CheckResult, all []projectstate.SpecCheck) []projectstate.SpecCheck {
	status := make(map[string]string, len(prev))
	for _, p := range prev {
		status[p.CheckId] = p.Status
	}
	var out []projectstate.SpecCheck
	for _, c := range all {
		if status[c.ID] != checkPass {
			out = append(out, c)
		}
	}
	return out
}

// --- rule ---

func (r checkRunner) runRule(ctx context.Context, c projectstate.SpecCheck) CheckResult {
	start := r.now()
	var ambient projectstate.ArtifactKind
	hasAmbient := false
	if s := strings.TrimSpace(c.Source.RuleSlot); s != "" {
		k, err := parseArtifactKind(s)
		if err != nil {
			return r.finish(c, checkFail, r.since(start), []byte("rule check: "+err.Error()+"\n"))
		}
		ambient, hasAmbient = k, true
	}
	type outcome struct {
		findings []methodcheck.Finding
		err      error
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutOf(c, defaultCheckTimeoutSec))
	defer cancel()
	done := make(chan outcome, 1)
	go func() {
		f, err := validateFindings(r.root, ambient, hasAmbient)
		done <- outcome{f, err}
	}()
	var o outcome
	select {
	case o = <-done:
	case <-ctx.Done():
		return r.finish(c, checkTimeout, r.since(start), []byte("rule check: "+ctx.Err().Error()+"\n"))
	}
	if o.err != nil {
		return r.finish(c, checkFail, r.since(start), []byte("rule check: "+o.err.Error()+"\n"))
	}
	var log bytes.Buffer
	status := checkPass
	kept := 0
	for _, f := range o.findings {
		if !hasRulePrefix(string(f.RuleID), c.Source.RulePrefixes) {
			continue
		}
		kept++
		log.WriteString(formatGateFinding(f) + "\n")
		if f.Severity == methodcheck.SeverityError {
			status = checkFail
		}
	}
	if kept == 0 {
		log.WriteString("no findings in scope\n")
	}
	return r.finish(c, status, r.since(start), log.Bytes())
}

// hasRulePrefix reports whether id is in scope; no prefixes means every rule.
func hasRulePrefix(id string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// --- command ---

func (r checkRunner) runCommand(ctx context.Context, c projectstate.SpecCheck) CheckResult {
	start := r.now()
	if len(c.Source.Argv) == 0 {
		return r.finish(c, checkFail, 0, []byte("command check has no argv\n"))
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutOf(c, defaultCheckTimeoutSec))
	defer cancel()
	out, err := r.exec(ctx, filepath.Join(r.root, c.Source.Dir), commandEnv(c.Source.Argv[0]), c.Source.Argv...)
	status := checkPass
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		status = checkTimeout
		out = append(out, fmt.Appendf(nil, "\n[timed out after %ds]\n", int(timeoutOf(c, defaultCheckTimeoutSec)/time.Second))...)
	case err != nil:
		status = checkFail
		out = append(out, []byte("\n["+err.Error()+"]\n")...)
	}
	return r.finish(c, status, r.since(start), out)
}

// commandEnv is the process env, plus GOWORK=off for the Go toolchain (go, make): a
// check judges the checkout's own module pins, never a developer's workspace.
func commandEnv(argv0 string, extra ...string) []string {
	env := os.Environ()
	if argv0 == "go" || argv0 == "make" {
		env = append(env, "GOWORK=off")
	}
	return append(env, extra...)
}

// --- scenario ---

// scenarioVerdict is one entry of a scenario index (results.json), the shape the Go
// scenario host and the generated Playwright reporter both write.
type scenarioVerdict struct {
	Scenario   string `json:"scenario"`
	Status     string `json:"status"`
	DurationMs int    `json:"durationMs"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	VideoRef   string `json:"videoRef,omitempty"`
	TraceRef   string `json:"traceRef,omitempty"`
}

// scenarioBatch is how one contract's selected scenarios run and where their verdicts
// land.
type scenarioBatch struct {
	goTest   bool     // a Go contract (go test); otherwise a web client (Playwright)
	dir      string   // the process's working directory
	indexDir string   // where the run's results.json lands
	refBase  string   // repo-relative directory the index's video/trace refs resolve against
	iface    string   // the contract's interface name (the Go test name infix)
	argv     []string // the process
	env      []string
	ctxSec   int // the process context's cap
}

// newScenarioBatch derives the batch for contract's scenarios ids. TimeoutSec defaults
// to 900 (R8): each scenario gets the largest cap as its own, go test's -timeout is the
// summed caps plus slack, and the process context a little more.
func newScenarioBatch(r checkRunner, proj projectstate.Project, contract string, checks []projectstate.SpecCheck) scenarioBatch {
	ids := make([]string, len(checks))
	var sumSec, maxSec int
	for i, c := range checks {
		ids[i] = c.Source.Scenario
		sec := int(timeoutOf(c, defaultScenarioTimeoutSec) / time.Second)
		sumSec += sec
		maxSec = max(maxSec, sec)
	}
	sc := proj.ServiceContracts[contract]
	b := scenarioBatch{goTest: sc.GoPackage != "", iface: sc.Interface.Name, ctxSec: sumSec + batchSlackSec}
	if b.iface == "" && contract != "" {
		b.iface = strings.ToUpper(contract[:1]) + contract[1:]
	}
	if b.goTest {
		b.dir = filepath.Join(r.root, proj.ModuleDir)
		b.indexDir = filepath.Join(b.dir, "test-results", contract)
		b.refBase = filepath.ToSlash(filepath.Join(proj.ModuleDir, "test-results", contract))
		b.argv = []string{"go", "test", "-p", "1", "-count=1", "-timeout", strconv.Itoa(sumSec+goTestSlackSec) + "s",
			"./" + sc.GoPackage + "/", "-run", goScenarioRunRegex(b.iface, ids), "-json"}
		b.env = commandEnv("go", "AIARCH_SCENARIO_TIMEOUT="+strconv.Itoa(maxSec)+"s")
		return b
	}
	b.dir = filepath.Join(r.root, "uitests")
	b.indexDir = filepath.Join(b.dir, "test-results", contract)
	b.refBase = "test-results/" + contract
	b.argv = []string{"npx", "playwright", "test", "-c", "generated/" + contract + "/playwright.config.gen.ts",
		"--grep", webScenarioGrep(ids)}
	b.env = commandEnv("npx", "AIARCH_SCENARIO_TIMEOUT_MS="+strconv.Itoa(maxSec*1000))
	return b
}

// runScenarios runs one contract's selected scenarios in one process and maps each to
// its verdict in the contract's index.
func (r checkRunner) runScenarios(ctx context.Context, contract string, checks []projectstate.SpecCheck) []CheckResult {
	start := r.now()
	failAll := func(msg string) []CheckResult {
		out := make([]CheckResult, len(checks))
		for i, c := range checks {
			out[i] = r.finish(c, checkFail, r.since(start), []byte(msg+"\n"))
		}
		return out
	}
	proj, err := r.project()
	if err != nil {
		return failAll("scenario checks: " + err.Error())
	}
	b := newScenarioBatch(r, proj, contract, checks)
	ids := make([]string, len(checks))
	for i, c := range checks {
		ids[i] = c.Source.Scenario
	}

	// A verdict an earlier run left for a scenario selected now must never count for
	// this run: prune the selected ids so only what this run records is read back.
	if err := pruneIndex(b.indexDir, ids); err != nil {
		return failAll("scenario checks: " + err.Error())
	}
	bctx, cancel := context.WithTimeout(ctx, time.Duration(b.ctxSec)*time.Second)
	defer cancel()
	out, runErr := r.exec(bctx, b.dir, b.env, b.argv...)
	if errors.Is(bctx.Err(), context.DeadlineExceeded) {
		runErr = fmt.Errorf("the batch was stopped after %ds", b.ctxSec)
	}

	if !b.goTest {
		// The frontend-integration copy: the web index and its media move to the
		// repo-root test-results/<contract>/, where the gate artifact carries them.
		if err := copyTree(b.indexDir, filepath.Join(r.root, "test-results", contract)); err != nil {
			return failAll("scenario checks: copy the Playwright results: " + err.Error())
		}
	}
	index, err := readScenarioIndex(b.indexDir)
	if err != nil {
		return failAll("scenario checks: " + err.Error() + "\n" + string(out))
	}
	results := make([]CheckResult, len(checks))
	for i, c := range checks {
		results[i] = r.scenarioResult(b, c, index, out, runErr)
	}
	return results
}

// scenarioResult is one selected scenario's result out of its contract's index. A
// scenario the run recorded nothing for fails: no verdict is never a pass.
func (r checkRunner) scenarioResult(b scenarioBatch, c projectstate.SpecCheck, index map[string]scenarioVerdict, out []byte, runErr error) CheckResult {
	v, ok := index[c.Source.Scenario]
	if !ok {
		msg := noVerdict + "\n"
		if runErr != nil {
			msg += "[" + runErr.Error() + "]\n"
		}
		return r.finish(c, checkFail, 0, append([]byte(msg+"\n"), logTail(out)...))
	}
	var log string
	if b.goTest {
		log = goTestOutputFor(out, goScenarioTestNames(b.iface, c.Source.Scenario))
	}
	if log == "" {
		log = v.Stdout + v.Stderr
	}
	if log == "" {
		log = string(logTail(out))
	}
	res := r.finish(c, scenarioStatus(v.Status), v.DurationMs, []byte(log))
	if v.VideoRef != "" {
		res.VideoRef = ptr(b.refBase + "/" + v.VideoRef)
	}
	if v.TraceRef != "" {
		res.TraceRef = ptr(b.refBase + "/" + v.TraceRef)
	}
	return res
}

// scenarioStatus folds an index status onto the check statuses; anything unknown fails.
func scenarioStatus(s string) string {
	switch s {
	case checkPass, checkFail, checkSkip, checkTimeout:
		return s
	}
	return checkFail
}

// project reads the checkout's committed project state.
func (r checkRunner) project() (projectstate.Project, error) {
	path := filepath.Join(r.root, statePathPrefix, projectFile)
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

// testIdent folds a scenario id (or any label) to a Go identifier fragment the way
// app-generator testgen's unexported ident does: non-alphanumerics become "_".
// TestScenarioIdentMatchesTestgen pins it against generated test names.
func testIdent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// goScenarioTestNames is the two spellings testgen gives a scenario's Go test: the plain
// TestScenario_<id> of a single-contract package and the TestScenario_<Iface>_<id> of a
// package several contracts share.
func goScenarioTestNames(iface, id string) []string {
	return []string{"TestScenario_" + testIdent(id), "TestScenario_" + testIdent(iface) + "_" + testIdent(id)}
}

// goScenarioRunRegex selects exactly the scenarios' tests under either spelling of
// their names (plain, or with the contract's interface infix), so an id never selects
// another id it is a suffix of.
func goScenarioRunRegex(iface string, ids []string) string {
	idents := make([]string, len(ids))
	for i, id := range ids {
		idents[i] = testIdent(id)
	}
	alts := strings.Join(idents, "|")
	return "^(TestScenario_(" + alts + ")|TestScenario_" + testIdent(iface) + "_(" + alts + "))$"
}

// webScenarioGrep selects the scenarios' Playwright tests. Playwright matches --grep
// against the test's title path joined with spaces ("<project> <file> <title> <tags>",
// an empty project leaving a bare separator), and a generated title is
// "<id> — <title>": the id is anchored on the left by the space before the title (or
// the subject's start) and on the right by the " — " separator, so x-P1 selects neither
// y-x-P1 nor x-P10.
func webScenarioGrep(ids []string) string {
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = regexp.QuoteMeta(id)
	}
	return "(?:^|\\s)(" + strings.Join(q, "|") + ") — "
}

// goTestOutputFor is the output of the named tests and their subtests out of a
// `go test -json` stream (the scenario host runs a scenario body under t.Run).
func goTestOutputFor(stream []byte, names []string) string {
	var out strings.Builder
	for line := range bytes.SplitSeq(stream, []byte("\n")) {
		var ev struct {
			Action, Test, Output string
		}
		if json.Unmarshal(line, &ev) != nil || ev.Action != "output" {
			continue
		}
		for _, n := range names {
			if ev.Test == n || strings.HasPrefix(ev.Test, n+"/") {
				out.WriteString(ev.Output)
				break
			}
		}
	}
	return out.String()
}

// readScenarioIndex reads dir/results.json keyed by scenario id; no index is empty.
func readScenarioIndex(dir string) (map[string]scenarioVerdict, error) {
	entries, err := readIndexEntries(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]scenarioVerdict, len(entries))
	for _, e := range entries {
		out[e.Scenario] = e
	}
	return out, nil
}

func readIndexEntries(dir string) ([]scenarioVerdict, error) {
	path := filepath.Join(dir, "results.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var entries []scenarioVerdict
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return entries, nil
}

// pruneIndex drops ids' entries from dir/results.json, keeping every other verdict.
func pruneIndex(dir string, ids []string) error {
	entries, err := readIndexEntries(dir)
	if err != nil || entries == nil {
		return err
	}
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}
	kept := make([]scenarioVerdict, 0, len(entries))
	for _, e := range entries {
		if !drop[e.Scenario] {
			kept = append(kept, e)
		}
	}
	b, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "results.json"), b, 0o600)
}

// copyTree copies src into dst (cp -R src/. dst/); a missing src copies nothing.
func copyTree(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		outF, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(outF, in); err != nil {
			_ = outF.Close()
			return err
		}
		return outF.Close()
	})
}

// --- shared ---

// finish writes c's log and builds its result. A log that cannot be written leaves the
// result without an outputRef and says so in its place on stderr; the verdict stands.
func (r checkRunner) finish(c projectstate.SpecCheck, status string, durationMs int, log []byte) CheckResult {
	res := CheckResult{CheckId: c.ID, Kind: c.Kind, Status: status, DurationMs: durationMs}
	path := filepath.Join(r.outDir, "checks", sanitizeCheckID(c.ID)+".log")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "run-checks: %s: %v\n", c.ID, err)
		return res
	}
	if err := os.WriteFile(path, log, 0o600); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "run-checks: %s: %v\n", c.ID, err)
		return res
	}
	res.OutputRef = ptr(r.repoRelative(path))
	return res
}

// repoRelative is path relative to the repo root when it lies under it, so a ref
// resolves inside the uploaded artifact; otherwise the path as given.
func (r checkRunner) repoRelative(path string) string {
	absRoot, err1 := filepath.Abs(r.root)
	absPath, err2 := filepath.Abs(path)
	if err1 == nil && err2 == nil {
		if rel, err := filepath.Rel(absRoot, absPath); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func (r checkRunner) since(start time.Time) int {
	return int(r.now().Sub(start) / time.Millisecond)
}

var unsafeIDRune = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// sanitizeCheckID is a check id as a file name: "command:build" → "command_build".
func sanitizeCheckID(id string) string {
	return unsafeIDRune.ReplaceAllString(id, "_")
}

// timeoutOf is c's cap, def seconds when it names none.
func timeoutOf(c projectstate.SpecCheck, def int) time.Duration {
	sec := c.TimeoutSec
	if sec <= 0 {
		sec = def
	}
	return time.Duration(sec) * time.Second
}

func ptr[T any](v T) *T { return &v }

// logTail is at most the last maxFallbackLog bytes of out.
func logTail(out []byte) []byte {
	if len(out) <= maxFallbackLog {
		return out
	}
	return append([]byte("[… output truncated to its last 64 KB]\n"), out[len(out)-maxFallbackLog:]...)
}

// execCommand runs argv in dir with env and returns its combined output. The process
// leads its own process group and a cancelled context kills the whole group, so the
// test binary under `go test`, make's children and Playwright's workers and browsers
// die with it; WaitDelay still bounds how long a straggler can hold the output pipes.
func execCommand(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay
	return cmd.CombinedOutput()
}
