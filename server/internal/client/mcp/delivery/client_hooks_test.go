// Hooks for mcpClient scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file.
//
// THE SUBJECT is the delivery tool surface of the MCP client: this package's
// generated Handler, served in-process (serveTools), bound to the REAL
// deliveryManager — built through its public generated constructor
// (mgr.NewDeliveryManager) and served by a real worker on the host's Temporal
// (Host.StartWorker with the composition root's own mgr.RegisterManagerWorker).
// Its collaborators are the LOCAL profile's wiring in cmd/server/main.gen.go and
// hooks.go, collaborator for collaborator:
//
//   - projectStateAccess and its three facets, sourceControlAccess,
//     artifactAccess and episodeAccess are the GitLocal / LocalFS variants over ONE
//     real working repo, a fresh one per scenario (the local profile is one
//     project per repo);
//   - agenticJobAccess is the local profile's venue: the real local executor
//     (agenticjob.NewLocalExecAgenticJobAccess), which adds a worktree per run,
//     seats the prompt surface with the venue's state tool, starts `claude` in it
//     and judges the run by whether the branch advanced;
//   - every Engine is the real (pure) Engine, and messageBus is the real Temporal
//     message bus with the delivery rows of the composition root's kind table.
//
// THE ONE DOUBLE is the resource outside this repo that the venue runs: the coding
// agent. The `claude` the venue starts is this test binary (see the venue
// section): a scripted agent that does in its worktree what a real agent run does —
// it drives the venue's REAL state tool (cmd/aiarch-state-mcp, built once for the
// test process) over MCP, or commits its construction work, or records the
// integration step's check run with that tool's record-check-run — and ends as the
// scenario scripts (succeeds, fails, or is still running when the operator looks).
//
// ARRANGEMENT GOES THROUGH PUBLIC OPS ONLY (dct plan 5, round-2 ruling): nothing
// here writes the repo's bytes. A project is born through the subject's own
// StartProject; its design artifacts are committed through designSessionAccess's
// validated stage-and-commit writes; everything after that is the system's own
// walk, driven by the subject's ops (a pump tick, a gate decision, a pause). The
// app's committed .aiarch/state/project.json is READ as source data for those
// writes and for what the scripted agent authors — the design models and the
// review-engine contract — and nothing is copied into
// this package. The one composed input is the System: the app's own, with every
// component but review-engine marked buildStatus "planned", so the plan the M0
// compute derives from it builds exactly one component (C-review-engine).

package delivery_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/cmd/aiarch-state-mcp/testinfra"
	"github.com/mixofreality-studio/archistrator/server/internal/client/mcp/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	mgr "github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// init hands the process to the venue's double when the venue started this test
// binary under one of the names it runs (see the venue section): as `claude`, the
// scripted agent; as `aiarch-state-mcp`, the venue's state tool. Started any other
// way, it is the test binary and init does nothing.
func init() {
	switch filepath.Base(os.Args[0]) {
	case agentName:
		os.Exit(runScriptedAgent())
	case stateToolName:
		os.Exit(runVenueStateTool())
	}
}

// The project every in-flight binding names, the scenario project's construction
// activity and the contract it builds, and where the source data is read.
const (
	planOwner        = "dct-owner"
	planName         = "dct-mcp"
	planProject      = planOwner + "/" + planName
	fixtureActivity  = "C-review-engine"
	fixtureComponent = "review-engine"
	fixtureContract  = "reviewEngine"
	// designState is the app's own committed project state, relative to this
	// package: the source data every arrangement write and every scripted draft
	// reads.
	designState = "../../../../../.aiarch/state/project.json"
	// pauseReason is the operator's reason in the pause / resume bindings.
	pauseReason = "Holding construction while the billing vendor contract is renegotiated."
)

// ---------------------------------------------------------------------------
// The stack
// ---------------------------------------------------------------------------

// stack is one scenario's downstream wiring, kept so the step hooks reach the same
// instances the subject's worker runs against.
type stack struct {
	t        *testing.T
	h        *scenariohost.Host
	scenario string
	project  string
	repo     workingRepo
	mgr      mgr.DeliveryManager
	ps       projectstate.ProjectStateAccess
	dsa      projectstate.DesignSessionAccess
	aea      projectstate.ActivityExecutionAccess
	venue    *venue

	mu       sync.Mutex
	arranged map[int]bool
	// revisionsBefore is each design activity's revision count per task, taken
	// before a step whose settler waits on a design walk.
	revisionsBefore map[string]map[string]int
	keys            int
}

var (
	stackMu  sync.Mutex
	curStack *stack
)

// current is the stack newSubject built for the running scenario. Scenarios of a
// package run one at a time, so there is at most one.
func current(t *testing.T) *stack {
	t.Helper()
	stackMu.Lock()
	defer stackMu.Unlock()
	if curStack == nil {
		t.Fatal("hooks: no subject was built for this scenario")
	}
	return curStack
}

// scenarioOf is the scenario id RunScenario named t's sub-test after.
func scenarioOf(t *testing.T) string {
	name := t.Name()
	return name[strings.LastIndex(name, "/")+1:]
}

// projectOf is the project a scenario's bindings name: manage-projects creates its
// own (owner/name, name-as-identity); every other scenario works on the plan's.
func projectOf(scenario string) string {
	switch scenario {
	case "manage-projects-P1", "manage-projects-P2":
		// Name-as-identity: StartProject mints the project id from the name alone.
		return "dct-gtd-app"
	}
	return planProject
}

// venueRunTimeout bounds one agent run inside the venue, as the local profile's
// configuration does; the scripted agent finishes in well under a second, so this
// only guards a run the scenario holds open past its end.
const venueRunTimeout = 2 * time.Minute

// newSubject builds the in-process MCP server the scenarios call: the delivery
// Handler of this package bound to the real deliveryManager on the host's stack.
// The tools read no principal, so the server runs under the host's context.
func newSubject(t *testing.T, h *scenariohost.Host) toolServer {
	t.Helper()
	scenario := scenarioOf(t)
	project := projectOf(scenario)
	repo := startWorkingRepo(t)
	v := openVenue(t, h, repo)
	jobs, err := agenticjob.NewLocalExecAgenticJobAccess(repo.URL, project, v.stateTool, venueRunTimeout)
	if err != nil {
		t.Fatalf("hooks: NewLocalExecAgenticJobAccess: %v", err)
	}
	m := buildManager(h, repo.URL, jobs)
	s := &stack{t: t, h: h, scenario: scenario, project: project, repo: repo, mgr: m,
		ps:    projectstate.NewGitLocalProjectStateAccess(repo.URL),
		dsa:   projectstate.NewGitLocalDesignSessionAccess(repo.URL),
		aea:   projectstate.NewGitLocalActivityExecutionAccess(repo.URL),
		venue: v, arranged: map[int]bool{}}
	if !workerDeferred[scenario] {
		s.startWorker(t)
	}
	stackMu.Lock()
	curStack = s
	stackMu.Unlock()
	t.Cleanup(func() {
		stackMu.Lock()
		curStack = nil
		stackMu.Unlock()
	})
	return toolServer{Handlers: []toolRegistrar{&delivery.Handler{Manager: m}}}
}

// buildManager is the composition root's delivery wiring for the LOCAL profile
// (cmd/server/main.gen.go): every collaborator over the repo at url, the agentic
// job venue being jobs, the host's Temporal client, and the configuration's
// defaults (a 30-minute escalation wait, tiered intervention).
func buildManager(h *scenariohost.Host, url string, jobs agenticjob.AgenticJobAccess) mgr.DeliveryManager {
	return mgr.NewDeliveryManager(h.TemporalClient(), projectstate.NewGitLocalProjectStateAccess(url),
		artifact.NewGitLocalArtifactAccess(url),
		intervention.NewInterventionEngine(), review.NewReviewEngine(), estimation.NewEstimationEngine(),
		operationestimation.NewOperationEstimationEngine(), billing.NewBillingEngine(), designhealth.NewDesignHealthEngine(),
		jobs, sourcecontrol.NewGitLocalSourceControlAccess(url),
		projectstate.NewGitLocalGitActivityStatusAccess(url), projectstate.NewGitLocalDesignSessionAccess(url),
		projectstate.NewGitLocalActivityExecutionAccess(url),
		messagebus.NewTemporalMessageBus(h.TemporalClient(), deliveryKinds()),
		episode.NewLocalFSEpisodeAccess(url),
		30*time.Minute, "tiered",
		// The local profile's repo resolver (cmd/server DeliveryManagerRepo, GitLocal arm).
		func(pid mgr.ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.GitLocalRepoRefForProject(sourcecontrol.ProjectID(projectstate.ProjectID(pid).String())), true
		},
		"")
}

// deliveryKinds is the messageBus kind table the delivery Manager's schedules
// resolve against — the delivery rows of cmd/server's MessageBusTemporalArgs.
func deliveryKinds() map[messagebus.ExecutionKind]messagebus.KindBinding {
	return map[messagebus.ExecutionKind]messagebus.KindBinding{
		"constructionPumpSweep": {WorkflowType: "constructionPumpSweep", TaskQueue: mgr.TaskQueue},
		"deliveryRoundSweep":    {WorkflowType: "deliveryRoundSweep", TaskQueue: mgr.TaskQueue},
	}
}

// workingRepo is the scenario's project repo: a non-bare repository, which the
// venue requires (its episode traces must sit outside the git dir an agent can
// write), whose own checkout stays on a branch of its own. Every project branch —
// main included — is only ever pushed to, never checked out here, so no push has to
// update a working tree.
type workingRepo struct {
	Dir string // the repository
	URL string // file://Dir, what every GitLocal collaborator is configured with
}

// startWorkingRepo creates the scenario's repo with one empty commit on main.
func startWorkingRepo(t *testing.T) workingRepo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "dct-mcp")
	git(t, filepath.Dir(dir), "init", "-q", "-b", "checkout", dir)
	git(t, dir, "-c", "user.name=seed", "-c", "user.email=seed@aiarch.local", "commit", "-q", "--allow-empty", "-m", "seed")
	git(t, dir, "branch", "main")
	return workingRepo{Dir: dir, URL: "file://" + dir}
}

// runGit runs one git command in dir.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return string(out), nil
}

// git runs one git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := runGit(dir, args...); err != nil {
		t.Fatalf("hooks: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Source data: the app's own committed design state, READ
// ---------------------------------------------------------------------------

// sourceDoc is the app's committed project state as a generic document.
type sourceDoc struct {
	Slots            map[string]struct{ Model json.RawMessage } `json:"slots"`
	ServiceContracts map[string]json.RawMessage                 `json:"serviceContracts"`
}

var (
	sourceOnce sync.Once
	source     sourceDoc
	sourceErr  error
)

func readSource(t *testing.T) sourceDoc {
	t.Helper()
	sourceOnce.Do(func() {
		b, err := os.ReadFile(designState)
		if err != nil {
			sourceErr = err
			return
		}
		sourceErr = json.Unmarshal(b, &source)
	})
	if sourceErr != nil {
		t.Fatalf("hooks: read the app's design state: %v", sourceErr)
	}
	return source
}

// sourceModel is the scenario project's model of kind: the app's own committed
// model, except the System, whose every component but review-engine is planned.
func sourceModel(t *testing.T, kind projectstate.ArtifactKind) json.RawMessage {
	t.Helper()
	raw := readSource(t).Slots[strconv.Itoa(int(kind))].Model
	if len(raw) == 0 {
		t.Fatalf("hooks: the app's design state holds no %s model", kind.WireName())
	}
	if kind != projectstate.KindSystem {
		return raw
	}
	trimmed, err := planOneComponent(raw)
	if err != nil {
		t.Fatalf("hooks: compose the scenario System: %v", err)
	}
	return trimmed
}

// planOneComponent marks every component of a System model but fixtureComponent
// buildStatus "planned" — designed, not yet built — so the plan derived from it
// builds that one component.
func planOneComponent(raw json.RawMessage) (json.RawMessage, error) {
	var sys map[string]any
	if err := json.Unmarshal(raw, &sys); err != nil {
		return nil, err
	}
	comps, _ := sys["components"].([]any)
	for _, c := range comps {
		comp, _ := c.(map[string]any)
		if comp["id"] != fixtureComponent {
			comp["buildStatus"] = "planned"
		}
	}
	return json.Marshal(sys)
}

// kindOf resolves a dispatcher's spelling of an artifact kind (its wire name or
// its PascalCase name).
func kindOf(name string) (projectstate.ArtifactKind, bool) {
	for _, k := range projectstate.AllArtifactKinds() {
		if name == k.WireName() || name == k.String() {
			return k, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// The venue
// ---------------------------------------------------------------------------
//
// The venue is the local profile's: the real local executor (agenticJobAccess)
// adds a worktree on the job's branch, seats the prompt surface with the venue's
// state tool (`aiarch-state-mcp seat-assets`), starts `claude` in the worktree with
// the AIARCH_* rig and an MCP config naming that same state tool, and reports the
// run succeeded only when the branch advanced. Both executables it starts are this
// test binary, first on PATH under those two names (venueBinaries), and init hands
// the process to the matching double:
//
//   - `aiarch-state-mcp` is the venue's REAL state tool (cmd/aiarch-state-mcp, built
//     once for the test process) for every call, except a seat the scenario scripts
//     the venue to refuse — a job the venue never accepts.
//   - `claude` is the scripted agent. It asks this test process what the scenario
//     scripts for the run: fail (exit non-zero, commit nothing), stay running (the
//     operator looks while the job is in flight), or succeed. A succeeding run does in
//     its worktree what a real agent run does there: a design job drives the real state
//     tool over MCP (putDraftModel / setCritiqueVerdict / respondToReviewComment, then
//     publishDraft); the detailed-design job records the component's contract and the
//     test-plan job its test plan (recordServiceContract / recordPhaseArtifact, the
//     app's committed ones); the integration job records its check run with the state
//     tool's record-check-run; any other construction job commits its work. Its stdout
//     is the stream-json a real run writes, so the venue mines the episode from it.

const (
	agentName     = "claude"
	stateToolName = "aiarch-state-mcp"
	venueFile     = "venue.json"
)

// venueRig is what the two doubles read from venueFile, which sits next to them in
// the directory that is first on PATH.
type venueRig struct {
	URL  string `json:"url"`  // this test process's venue endpoint
	Tool string `json:"tool"` // the real state tool
}

var (
	rigOnce  sync.Once
	rigDir   string
	rigErr   error
	rigState venueRig

	currentMu    sync.Mutex
	currentVenue *venue
)

// venueBinaries prepares, once per test process, the directory the venue's
// executables are found in: `claude` and `aiarch-state-mcp` (this test binary
// under the two names), the real state tool built from cmd/aiarch-state-mcp, and
// venueFile pointing the doubles back at this process. It lives beside the test
// binary, in the directory `go test` removes when the run ends.
func venueBinaries(t *testing.T, h *scenariohost.Host) string {
	t.Helper()
	rigOnce.Do(func() { rigDir, rigState, rigErr = buildVenue(filepath.Dir(h.ResultsRoot)) })
	if rigErr != nil {
		t.Fatalf("hooks: prepare the venue: %v", rigErr)
	}
	return rigDir
}

func buildVenue(moduleRoot string) (string, venueRig, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", venueRig{}, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", venueRig{}, err
	}
	dir := filepath.Join(filepath.Dir(exe), "dct-venue")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", venueRig{}, err
	}
	for _, name := range []string{agentName, stateToolName} {
		link := filepath.Join(dir, name)
		_ = os.Remove(link)
		if err := os.Symlink(exe, link); err != nil {
			return "", venueRig{}, err
		}
	}
	tool := filepath.Join(dir, "state-tool")
	build := exec.Command("go", "build", "-o", tool, "./cmd/aiarch-state-mcp")
	build.Dir = moduleRoot
	// The tool builds in the mode this test binary was built in (the ambient GOWORK: off
	// under the make targets, the workspace during a pre-release development cycle), so
	// the venue's state tool and the subject resolve the same platform modules.
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		return "", venueRig{}, fmt.Errorf("build the state tool: %w\n%s", err, out)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/agent", serveAgent)
	mux.HandleFunc("/seat", serveSeat)
	srv := httptest.NewServer(mux)
	r := venueRig{URL: srv.URL, Tool: tool}
	b, err := json.Marshal(r)
	if err != nil {
		return "", venueRig{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, venueFile), b, 0o600); err != nil {
		return "", venueRig{}, err
	}
	return dir, r, nil
}

// runOutcome is how the scenario scripts one agent run to end.
type runOutcome int

const (
	runSucceeds runOutcome = iota
	runFails
	runStaysRunning
)

// agentJob is one run of the agent, as the venue started it: the job's rig (the
// AIARCH_* environment) and the worktree it runs in.
type agentJob struct {
	Mode      string // AIARCH_JOB_MODE: draft | critique | answer | construct
	Command   string // AIARCH_COMMAND: the lifecycle task's command
	Activity  string
	Component string
	Kind      string // AIARCH_ARTIFACT_KIND, design jobs only
	dir       string
	env       map[string]string
}

// isConstruction reports whether the job is a construction activity's construction
// step (the lifecycle's `construction` task).
func (j agentJob) isConstruction() bool {
	return j.Mode == "construct" && strings.HasSuffix(j.Command, "-construction")
}

// venue is one scenario's view of the venue: the runs it started and the script
// they follow.
type venue struct {
	t         *testing.T
	repo      workingRepo
	stateTool string // what the venue is configured with: the double on PATH
	tool      string // the real state tool
	results   string // where the integration step's check runs are kept

	mu        sync.Mutex
	submitted []agentJob
	outcome   func(n int, j agentJob) runOutcome
	refuse    func(command string) bool
	release   chan struct{}
	inFlight  sync.WaitGroup
}

// openVenue points the venue at this scenario: the doubles first on PATH (never the
// real Claude Code CLI) and every run routed here until the scenario ends. At the end
// it releases a run the scenario held open and waits until every run is over and its
// worktree removed, so nothing outlives the scenario's repo.
func openVenue(t *testing.T, h *scenariohost.Host, repo workingRepo) *venue {
	t.Helper()
	dir := venueBinaries(t, h)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got, err := exec.LookPath(agentName); err != nil || got != filepath.Join(dir, agentName) {
		t.Fatalf("hooks: claude resolves to %q (%v), want the scripted agent in %s", got, err, dir)
	}
	v := &venue{t: t, repo: repo, stateTool: filepath.Join(dir, stateToolName), tool: rigState.Tool,
		results: t.TempDir(), release: make(chan struct{})}
	currentMu.Lock()
	currentVenue = v
	currentMu.Unlock()
	t.Cleanup(func() {
		close(v.release)
		v.inFlight.Wait()
		v.awaitWorktreesRemoved()
		currentMu.Lock()
		currentVenue = nil
		currentMu.Unlock()
	})
	return v
}

// awaitWorktreesRemoved waits until the venue has removed every run's worktree.
func (v *venue) awaitWorktreesRemoved() {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, err := runGit(v.repo.Dir, "worktree", "list", "--porcelain")
		if err != nil || strings.Count(out, "worktree ") <= 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (v *venue) script(f func(n int, j agentJob) runOutcome) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.outcome = f
}

func (v *venue) refuseSeat(f func(command string) bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refuse = f
}

// jobs returns the agent runs the venue started so far.
func (v *venue) jobs() []agentJob {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.submitted)
}

func currentVenueOrNil() *venue {
	currentMu.Lock()
	defer currentMu.Unlock()
	return currentVenue
}

// agentRequest / agentResponse are the scripted agent's exchange with this process.
type agentRequest struct {
	Dir string            `json:"dir"`
	Env map[string]string `json:"env"`
}

type agentResponse struct {
	Exit   int    `json:"exit"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

type seatRequest struct {
	Command string `json:"command"`
}

type seatResponse struct {
	Refused string `json:"refused"`
}

func serveAgent(w http.ResponseWriter, r *http.Request) {
	var req agentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := agentResponse{Exit: 1, Stderr: "scripted agent: no scenario is running"}
	if v := currentVenueOrNil(); v != nil {
		resp = v.run(req)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func serveSeat(w http.ResponseWriter, r *http.Request) {
	var req seatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var resp seatResponse
	if v := currentVenueOrNil(); v != nil {
		v.mu.Lock()
		refuse := v.refuse
		v.mu.Unlock()
		if refuse != nil && refuse(req.Command) {
			resp.Refused = "the venue could not seat " + req.Command + ": no runner is available"
		}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// run is one agent run: recorded, then ended as the scenario scripts it.
func (v *venue) run(req agentRequest) agentResponse {
	v.inFlight.Add(1)
	defer v.inFlight.Done()
	j := agentJob{
		Mode: req.Env["AIARCH_JOB_MODE"], Command: req.Env["AIARCH_COMMAND"],
		Activity: req.Env["AIARCH_ACTIVITY_ID"], Component: req.Env["AIARCH_COMPONENT_ID"],
		Kind: req.Env["AIARCH_ARTIFACT_KIND"], dir: req.Dir, env: req.Env,
	}
	v.mu.Lock()
	n := len(v.submitted)
	v.submitted = append(v.submitted, j)
	script := v.outcome
	v.mu.Unlock()
	outcome := runSucceeds
	if script != nil {
		outcome = script(n, j)
	}
	switch outcome {
	case runFails:
		return agentResponse{Exit: 1, Stdout: streamEvents(true), Stderr: "scripted agent: the run failed"}
	case runStaysRunning:
		<-v.release
		return agentResponse{Exit: 1, Stderr: "scripted agent: still running when the scenario ended"}
	case runSucceeds:
	}
	if err := v.perform(n, j); err != nil {
		v.t.Logf("hooks: the scripted agent's %s run on %s could not do its work: %v", j.Command, j.Activity, err)
		return agentResponse{Exit: 1, Stdout: streamEvents(true), Stderr: "scripted agent: " + err.Error()}
	}
	return agentResponse{Stdout: streamEvents(false)}
}

// streamEvents is the stream-json a run writes: the session's init and its terminal
// result, which is what the venue mines the episode from.
func streamEvents(failed bool) string {
	result := map[string]any{"type": "result", "subtype": "success", "is_error": failed, "num_turns": 1,
		"total_cost_usd": 0, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}}
	if failed {
		result["subtype"] = "error_during_execution"
	}
	initEv, _ := json.Marshal(map[string]any{"type": "system", "subtype": "init", "model": "scripted-agent"})
	resultEv, _ := json.Marshal(result)
	return string(initEv) + "\n" + string(resultEv) + "\n"
}

// perform is what a succeeding run does in its worktree.
func (v *venue) perform(n int, j agentJob) error {
	if err := testinfra.VerifyTaskSpec(v.tool, venueJob(j)); err != nil {
		return err
	}
	switch j.Mode {
	case "draft":
		return v.draft(j)
	case "critique":
		return v.withStateTool(j, func(s *stateSession) error {
			_, err := s.call("setCritiqueVerdict", map[string]any{"verdict": "approve", "notes": ""})
			return err
		})
	case "answer":
		return v.answer(j)
	case "construct":
		return v.construct(n, j)
	}
	return fmt.Errorf("no script for job mode %q", j.Mode)
}

// construct is a construction job's work, by the lifecycle task's command.
func (v *venue) construct(n int, j agentJob) error {
	switch {
	case strings.HasSuffix(j.Command, "-integration"):
		return v.integrate(n, j)
	case strings.HasSuffix(j.Command, "-detailed-design"):
		contract, err := v.sourceJSON(readSource(v.t).ServiceContracts[fixtureContract])
		if err != nil {
			return err
		}
		return v.withStateTool(j, func(s *stateSession) error {
			_, err := s.call("recordServiceContract", map[string]any{"contract": contract})
			return err
		})
	}
	// Every other step — the test plan included — is a change on the activity branch.
	// The test plan is not recorded from the app's own: the app's plan binds the
	// scenarios projected over the app's System, and the scenario project's System
	// builds only review-engine, so its projection differs and the app's bindings
	// would not validate against it (TP-STATE). The integration step reads the
	// scenario project's own projection instead (integrate).
	return commitWork(n, j)
}

// sourceJSON decodes one source-data document, refusing an absent one.
func (v *venue) sourceJSON(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, errors.New("the app's design state holds no " + fixtureContract + " record to author from")
	}
	var doc map[string]any
	return doc, json.Unmarshal(raw, &doc)
}

// draft authors the slot through putDraftModel and publishes it: a redraft keeps
// what the worktree already holds for the slot (an amendment that keeps the
// content); a FIRST draft — no model of the slot yet — authors the scenario
// project's own model of it (sourceModel).
func (v *venue) draft(j agentJob) error {
	doc, err := readWorktreeProject(j.dir)
	if err != nil {
		return err
	}
	kind, ok := kindOf(j.Kind)
	if !ok {
		return fmt.Errorf("unknown artifact kind %q", j.Kind)
	}
	raw := doc.Slots[strconv.Itoa(int(kind))].Model
	if len(raw) == 0 || string(raw) == "null" {
		raw = sourceModel(v.t, kind)
	}
	var model map[string]any
	if err := json.Unmarshal(raw, &model); err != nil {
		return fmt.Errorf("no %s model to draft: %w", j.Kind, err)
	}
	return v.withStateTool(j, func(s *stateSession) error {
		_, err := s.call("putDraftModel", map[string]any{"model": model})
		return err
	})
}

// answer replies to every open question on the slot's thread.
func (v *venue) answer(j agentJob) error {
	doc, err := readWorktreeProject(j.dir)
	if err != nil {
		return err
	}
	kind, ok := kindOf(j.Kind)
	if !ok {
		return fmt.Errorf("unknown artifact kind %q", j.Kind)
	}
	slot := doc.Slots[strconv.Itoa(int(kind))]
	return v.withStateTool(j, func(s *stateSession) error {
		for _, c := range slot.Thread {
			if c.Type != "question" || c.Status != "open" {
				continue
			}
			if _, err := s.call("respondToReviewComment", map[string]any{"id": c.ID,
				"response": "The review roster is the Manager's because it sequences the reviewers; the engine only proposes them."}); err != nil {
				return err
			}
		}
		return nil
	})
}

// integrate is the integration step's venue write, the construct template's gate in
// miniature: the task spec resolved in the worktree exactly as the job resolves it
// (task-spec), every check it holds — the scenario checks are every scenario projected
// onto the component — ran green against the integration attempt the activity row holds
// pending, the results written where run-checks writes them, and that run recorded with
// record-check-run and published.
func (v *venue) integrate(n int, j agentJob) error {
	revision := j.env["AIARCH_REVISION"]
	if revision == "" {
		ledger, lerr := readMainProject(j.dir)
		if lerr != nil {
			return lerr
		}
		revision = ledger.pendingAttempt(j.Activity, "integration")
	}
	if revision == "" {
		return fmt.Errorf("no pending integration attempt on %s", j.Activity)
	}
	runID := fmt.Sprintf("dct-run-%04d", n+1)
	dir := filepath.Join(v.results, runID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	specPath := filepath.Join(dir, "spec.json")
	if _, err := testinfra.ResolveTaskSpec(v.tool, venueJob(j), specPath); err != nil {
		return err
	}
	resultsPath, err := testinfra.WritePassingResults(specPath, filepath.Join(dir, "results.json"))
	if err != nil {
		return err
	}
	record := exec.Command(v.tool, "record-check-run", "--spec", specPath, "--results", resultsPath,
		"--run-id", runID, "--artifact", dir, "--revision", revision)
	record.Dir = j.dir
	record.Env = toolEnv(j)
	if out, err := record.CombinedOutput(); err != nil {
		return fmt.Errorf("record-check-run: %w\n%s", err, out)
	}
	return v.withStateTool(j, func(*stateSession) error { return nil })
}

// venueJob is the job as the shared venue double (testinfra) takes it.
func venueJob(j agentJob) testinfra.VenueJob {
	return testinfra.VenueJob{Mode: j.Mode, Command: j.Command, Activity: j.Activity, Dir: j.dir, Env: j.env}
}

// commitWork is a construction step's work: a change on the activity branch.
func commitWork(n int, j agentJob) error {
	rel := filepath.Join("work", j.Activity, j.Command+".md")
	p := filepath.Join(j.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(p, fmt.Appendf(nil, "%s for %s, run %d\n", j.Command, j.Activity, n+1), 0o600); err != nil {
		return err
	}
	if _, err := runGit(j.dir, "add", rel); err != nil {
		return err
	}
	_, err := runGit(j.dir, "-c", "user.name=scripted-agent", "-c", "user.email=scripted-agent@aiarch.local",
		"commit", "-q", "-m", j.Command+" "+j.Activity)
	return err
}

// toolEnv is the state tool's environment: the job's rig, as the venue hands it.
func toolEnv(j agentJob) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for k, val := range j.env {
		env = append(env, k+"="+val)
	}
	return env
}

// withStateTool runs body against the job's state tool over MCP, then publishes —
// the last thing every agent run that writes state does.
func (v *venue) withStateTool(j agentJob, body func(*stateSession) error) error {
	s, err := openStateSession(v.tool, j)
	if err != nil {
		return err
	}
	if err := body(s); err != nil {
		return errors.Join(err, s.close())
	}
	if _, err := s.call("publishDraft", map[string]any{"message": "scripted agent: " + j.Command}); err != nil {
		return errors.Join(err, s.close())
	}
	return s.close()
}

// stateSession is an MCP client over the state tool's stdio: what the agent's
// session does with the server its MCP config names.
type stateSession struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr *bytes.Buffer
	next   int
}

func openStateSession(tool string, j agentJob) (*stateSession, error) {
	cmd := exec.Command(tool)
	cmd.Dir = j.dir
	cmd.Env = toolEnv(j)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &stateSession{cmd: cmd, in: in, out: bufio.NewReader(out), stderr: &stderr}
	if _, err := s.request("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "scripted-agent", "version": "1"}}); err != nil {
		return nil, errors.Join(err, s.close())
	}
	if err := s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return nil, errors.Join(err, s.close())
	}
	return s, nil
}

func (s *stateSession) send(msg map[string]any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = s.in.Write(append(b, '\n'))
	return err
}

// request sends one JSON-RPC request and reads lines until its response.
func (s *stateSession) request(method string, params any) (json.RawMessage, error) {
	s.next++
	id := s.next
	if err := s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		line, err := s.out.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("%s: the state tool closed its output: %w (stderr: %s)", method, err, s.stderr.String())
		}
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &msg) != nil || msg.ID == nil || *msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

// call invokes one tool; a tool result flagged isError is the tool refusing.
func (s *stateSession) call(tool string, args map[string]any) (string, error) {
	raw, err := s.request("tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return "", err
	}
	var res struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("%s: %w", tool, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		text.WriteString(c.Text)
	}
	if res.IsError {
		return "", fmt.Errorf("%s refused: %s", tool, text.String())
	}
	return text.String(), nil
}

func (s *stateSession) close() error {
	_ = s.in.Close()
	if err := s.cmd.Wait(); err != nil {
		return fmt.Errorf("the state tool: %w (stderr: %s)", err, s.stderr.String())
	}
	return nil
}

// worktreeProject is the part of the worktree's project.json the agent reads.
type worktreeProject struct {
	Slots             map[string]worktreeSlot `json:"slots"`
	ActivityExecution map[string]struct {
		Attempts []struct {
			AttemptID string `json:"attemptId"`
			Task      string `json:"task"`
			Outcome   string `json:"outcome"`
		} `json:"attempts"`
	} `json:"activityExecution"`
}

// readMainProject reads the project as main holds it, from a worktree of the repo.
func readMainProject(dir string) (worktreeProject, error) {
	var doc worktreeProject
	out, err := runGit(dir, "show", "main:.aiarch/state/project.json")
	if err != nil {
		return doc, err
	}
	return doc, json.Unmarshal([]byte(out), &doc)
}

func readWorktreeProject(dir string) (worktreeProject, error) {
	var doc worktreeProject
	b, err := os.ReadFile(filepath.Join(dir, ".aiarch", "state", "project.json"))
	if err != nil {
		return doc, err
	}
	return doc, json.Unmarshal(b, &doc)
}

// worktreeSlot is one slot of the worktree's project.json: its model and thread.
type worktreeSlot struct {
	Model  json.RawMessage `json:"model"`
	Thread []struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Status string `json:"status"`
	} `json:"reviewThread"`
}

// pendingAttempt is the id of the activity's pending attempt at task.
func (p worktreeProject) pendingAttempt(activity, task string) string {
	id := ""
	for _, a := range p.ActivityExecution[activity].Attempts {
		if a.Task == task && a.Outcome == string(projectstate.OutcomePending) {
			id = a.AttemptID
		}
	}
	return id
}

// runScriptedAgent is the process the venue starts as `claude`: it asks the test
// process that owns the venue (venueFile, beside it on PATH) to run the job its rig
// describes, and ends the way the scenario scripted.
func runScriptedAgent() int {
	dir, _, _ := strings.Cut(os.Getenv("PATH"), string(os.PathListSeparator))
	r, err := readVenueRig(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scripted agent:", err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "scripted agent:", err)
		return 1
	}
	req := agentRequest{Dir: cwd, Env: map[string]string{}}
	for _, kv := range os.Environ() {
		if k, val, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "AIARCH_") {
			req.Env[k] = val
		}
	}
	var resp agentResponse
	if err := post(r.URL+"/agent", req, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "scripted agent:", err)
		return 1
	}
	_, _ = os.Stdout.WriteString(resp.Stdout)
	_, _ = os.Stderr.WriteString(resp.Stderr)
	return resp.Exit
}

// runVenueStateTool is the process the venue starts as `aiarch-state-mcp`: the real
// state tool, unless the scenario scripts the venue to refuse this job's seat.
func runVenueStateTool() int {
	r, err := readVenueRig(filepath.Dir(os.Args[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "venue:", err)
		return 1
	}
	if len(os.Args) > 1 && os.Args[1] == "seat-assets" {
		req := seatRequest{}
		for i, a := range os.Args {
			if a == "--command" && i+1 < len(os.Args) {
				req.Command = os.Args[i+1]
			}
		}
		var resp seatResponse
		if err := post(r.URL+"/seat", req, &resp); err != nil {
			fmt.Fprintln(os.Stderr, "venue:", err)
			return 1
		}
		if resp.Refused != "" {
			fmt.Fprintln(os.Stderr, "venue:", resp.Refused)
			return 1
		}
	}
	if err := syscall.Exec(r.Tool, append([]string{r.Tool}, os.Args[1:]...), os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "venue:", err)
	}
	return 1
}

func readVenueRig(dir string) (venueRig, error) {
	var r venueRig
	b, err := os.ReadFile(filepath.Join(dir, venueFile))
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(b, &r)
}

func post(url string, body, into any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("venue answered %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// ---------------------------------------------------------------------------
// The step: arrange, call the tool, settle
// ---------------------------------------------------------------------------

// runStep is every hooked step: arrange the stack for step seq of scenario (once),
// call the tool exactly as the client's caller does, and — where the tool only
// ENQUEUES work (a decision, a pause, a re-dispatch) — settle by waiting, through the
// subject's own read ops, until the work it asked for has been applied.
func runStep(t *testing.T, h *scenariohost.Host, subject *toolSession, scenario string, seq int, tool, args string) (any, error) {
	t.Helper()
	s := current(t)
	if s.markArranged(seq) {
		if fn, ok := arrangements[scenario]; ok {
			fn(t, s, seq)
		}
		if _, ok := settlers[settleKey(scenario, seq)]; ok {
			s.snapshotRevisions(t)
		}
	}
	out, err := subject.Call(h.Context(t), tool, args)
	if fn, ok := settlers[settleKey(scenario, seq)]; ok && err == nil {
		fn(t, s)
	}
	return out, err
}

func settleKey(scenario string, seq int) string { return fmt.Sprintf("%s#%d", scenario, seq) }

// markArranged records that step seq has been arranged, reporting whether this was
// the first time.
func (s *stack) markArranged(seq int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.arranged[seq] {
		return false
	}
	s.arranged[seq] = true
	return true
}

func (s *stack) arrangedStep(seq int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.arranged[seq]
}

// arrangement prepares the stack before step seq of a scenario runs.
type arrangement func(t *testing.T, s *stack, seq int)

// settler runs after a step's call succeeded.
type settler func(t *testing.T, s *stack)

// ---------------------------------------------------------------------------
// Arrangement through public ops
// ---------------------------------------------------------------------------

// The review policies the scenarios run under: "agents only" (no gate asks a human
// short of the risk floor), "human at the code review" (the construction phase's
// gate), and the birth default (every gate asks a human).
var (
	vibes               = &mgr.ExecutionPolicyInput{Preset: "vibes"}
	humanAtConstruction = &mgr.ExecutionPolicyInput{Policy: &mgr.ReviewPolicyInput{
		GatedPhasesByType: map[string][]string{"service": {"construction"}}}}
)

// requirementsSlots are the artifacts the requirements activity produces. The
// architecture activity drafts the System; the deployment & operations model and the
// planning assumptions are committed inputs the project-design compute reads.
var requirementsSlots = []projectstate.ArtifactKind{projectstate.KindMission, projectstate.KindGlossary,
	projectstate.KindScrubbedRequirements, projectstate.KindVolatilities, projectstate.KindCoreUseCases}

// nextKey is a fresh, scenario-scoped idempotency key suffix for an arrangement write.
func (s *stack) nextKey(what string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys++
	return fmt.Sprintf("arrange-%s-%d", what, s.keys)
}

// born creates the scenario project through the subject's own StartProject (not yet
// started), under policy when one is given.
func (s *stack) born(t *testing.T, policy *mgr.ExecutionPolicyInput) {
	t.Helper()
	owner, _, _ := strings.Cut(s.project, "/")
	// Name-as-identity: the name a project is created under IS its id, and every
	// binding names the plan's project by its whole id.
	if _, err := s.mgr.StartProject(s.h.ManagerContext(t), mgr.OwnerScope(owner), s.project, nil, nil, nil, false); err != nil {
		t.Fatalf("hooks: create the project: %v", err)
	}
	if policy != nil {
		if err := s.mgr.SetProjectExecutionPolicy(s.h.ManagerContext(t), mgr.ProjectID(s.project), *policy); err != nil {
			t.Fatalf("hooks: set the review policy: %v", err)
		}
	}
}

// commitSlots commits each kind's source model as the design session commits an
// approved draft: staged for review on main, then committed with provenance.
func (s *stack) commitSlots(t *testing.T, kinds ...projectstate.ArtifactKind) {
	t.Helper()
	pid := projectstate.ProjectID(s.project)
	for _, kind := range kinds {
		v, err := s.ps.ReadProjectVersion(s.h.ResourceAccessContext(t, s.nextKey("version")), pid)
		if err != nil {
			t.Fatalf("hooks: read the project version: %v", err)
		}
		env := projectstate.ModelEnvelope{Kind: kind, Model: sourceModel(t, kind)}
		rc := s.h.ResourceAccessContext(t, s.nextKey("stage-"+kind.WireName()))
		if v, err = s.dsa.StageArtifactForReviewOnBranch(rc, pid, v, "", env, rc.IdempotencyKey); err != nil {
			t.Fatalf("hooks: stage %s: %v", kind.WireName(), err)
		}
		if _, err = s.dsa.CommitArtifactWithProvenance(s.h.ResourceAccessContext(t, s.nextKey("commit-"+kind.WireName())), pid, v,
			kind, "operator", "agent"); err != nil {
			t.Fatalf("hooks: commit %s: %v", kind.WireName(), err)
		}
	}
}

// tick is one pump tick — the op the 30-second sweep calls.
func (s *stack) tick(t *testing.T, label string) mgr.PumpResult {
	t.Helper()
	r, err := s.mgr.ExecuteNextActivity(s.h.ManagerContext(t), mgr.ProjectID(s.project), "dct-mcp-arrange-"+label)
	if err != nil {
		t.Fatalf("hooks: tick the pump: %v", err)
	}
	return r
}

// designToArchitecture is the project born under policy with every design input
// but the System committed, and one pump tick: the requirements walk finds every slot
// it produces committed and exits; the architecture walk drafts the System (the
// scripted agent authors the scenario System) and its critic approves it.
func (s *stack) designToArchitecture(t *testing.T, policy *mgr.ExecutionPolicyInput) {
	t.Helper()
	s.born(t, policy)
	s.commitSlots(t, requirementsSlots...)
	s.commitSlots(t, projectstate.KindOperationalConcepts, projectstate.KindPlanningAssumptions)
	s.tick(t, "design")
}

// architectureAtGate holds the architecture walk at its review gate for the human
// (the birth policy gates every review).
func (s *stack) architectureAtGate(t *testing.T) {
	t.Helper()
	s.designToArchitecture(t, nil)
	s.awaitGate(t, "architecture", "architectureReview")
}

// atM0 is the project-design walk holding at M0 for the founder's decision: the
// architecture review approved (by the architect where the policy gates it), the
// architecture exit sealing system design, and the project-design walk computing the
// plan off the committed System.
func (s *stack) atM0(t *testing.T, policy *mgr.ExecutionPolicyInput) {
	t.Helper()
	if policy != nil {
		s.designToArchitecture(t, policy)
	} else {
		s.architectureAtGate(t)
		s.approve(t, "architecture", "architectureReview")
		s.approveMerge(t, "architecture")
	}
	s.awaitGate(t, "projectDesign", "sdpReview")
}

// approveMerge lands the activity's branch on main the way the reviewer sees it land:
// where the walk holds a merge gate for a human, the reviewer approves it; where the
// review approve IS the merge (a design activity's approve-merge), the walk lands its
// branch unattended and its session exits, and there is nothing left to approve.
func (s *stack) approveMerge(t *testing.T, activity string) {
	t.Helper()
	pid := s.project
	held := false
	eventually(t, walkWindow, activity+" holding its merge for a human or landing it", func() (bool, string) {
		v, err := s.mgr.QueryProjectView(s.h.ManagerContext(t), mgr.ProjectViewQuery{Kind: mgr.ProjectViewSession,
			ProjectID: &pid, ActivityID: &activity})
		if err != nil || v.ConstructionSession == nil {
			return false, "no live session"
		}
		for _, g := range v.ConstructionSession.AwaitingTasks {
			if g.TaskID == "merge" {
				held = true
				return true, ""
			}
		}
		return v.ConstructionSession.Stage == mgr.StageExited,
			fmt.Sprintf("stage %d, awaiting %v", v.ConstructionSession.Stage, v.ConstructionSession.AwaitingTasks)
	})
	if held {
		s.approve(t, activity, "merge")
	}
}

// approve is the reviewer's approve at activity's task gate.
func (s *stack) approve(t *testing.T, activity, task string) {
	t.Helper()
	if err := s.mgr.SubmitReviewDecision(s.h.ManagerContext(t), mgr.ProjectID(s.project), mgr.ActivityID(activity), task,
		mgr.ReviewDecisionInput{Decision: mgr.ReviewApprove}, nil); err != nil {
		t.Fatalf("hooks: approve %s/%s: %v", activity, task, err)
	}
}

// awaitGate waits until the activity's live session holds task's gate for a human
// and the round a decision is cast against is open — the gate the reviewer's screen
// offers a decision on.
func (s *stack) awaitGate(t *testing.T, activity, task string) {
	t.Helper()
	pid := s.project
	eventually(t, walkWindow, fmt.Sprintf("%s/%s holding for a human", activity, task), func() (bool, string) {
		v, err := s.mgr.QueryProjectView(s.h.ManagerContext(t), mgr.ProjectViewQuery{Kind: mgr.ProjectViewSession,
			ProjectID: &pid, ActivityID: &activity})
		if err != nil {
			return false, err.Error()
		}
		if v.ConstructionSession == nil {
			return false, "no live session"
		}
		awaiting := false
		for _, g := range v.ConstructionSession.AwaitingTasks {
			awaiting = awaiting || g.TaskID == task
		}
		if !awaiting {
			return false, fmt.Sprintf("stage %d, awaiting %v", v.ConstructionSession.Stage, v.ConstructionSession.AwaitingTasks)
		}
		return s.roundPending(t, activity, task), "the session awaits the gate but its round is not open yet"
	})
	if av, err := s.mgr.QueryActivityView(s.h.ManagerContext(t), mgr.ProjectID(s.project), mgr.ActivityID(activity)); err == nil &&
		av.State != mgr.ActivityViewAwaitingHuman {
		t.Logf("hooks: %s holds its %s gate for a human while its activity view reads %q (tasks %v)", activity, task, av.State, taskStates(av))
	}
}

// roundPending reports whether the activity's row holds an open (pending) review
// round on task — the round a decision is cast against.
func (s *stack) roundPending(t *testing.T, activity, task string) bool {
	t.Helper()
	row, err := s.aea.ReadActivityExecution(s.h.ResourceAccessContext(t, s.nextKey("read-row")), projectstate.ProjectID(s.project), activity)
	if err != nil {
		return false
	}
	for _, r := range row.Reviews {
		if string(r.TaskID) == task && r.Outcome == projectstate.RoundPending {
			return true
		}
	}
	return false
}

// taskStates renders an activity view's tasks as id=state/last-revision-outcome.
func taskStates(v mgr.ActivityView) []string {
	out := make([]string, 0, len(v.Tasks))
	for _, tv := range v.Tasks {
		last := ""
		if n := len(tv.Revisions); n > 0 {
			last = string(tv.Revisions[n-1].Outcome)
		}
		out = append(out, fmt.Sprintf("%s=%s/%s", tv.ID, tv.State, last))
	}
	return out
}

// commitM0 is the founder's M0 approve of the normal option — named by the id the
// compute presents it under, its solution kind — and it settles when the seal has
// moved the project into construction.
func (s *stack) commitM0(t *testing.T) {
	t.Helper()
	opt := projectstate.KindNormalSolution.String()
	if err := s.mgr.SubmitReviewDecision(s.h.ManagerContext(t), mgr.ProjectID(s.project), "projectDesign", "sdpReview",
		mgr.ReviewDecisionInput{Decision: mgr.ReviewApprove, OptionID: &opt}, nil); err != nil {
		t.Fatalf("hooks: approve M0: %v", err)
	}
	s.awaitSummary(t, "the M0 seal moving the project into construction", func(p *mgr.ProjectState) bool {
		return p.PhaseName == "construction"
	})
}

// inFlight is the committed project in construction under policy: at M0, approved,
// and the plan's one construction activity started by the pump the M0 seal hands on.
func (s *stack) inFlight(t *testing.T, policy *mgr.ExecutionPolicyInput) {
	t.Helper()
	s.atM0(t, policy)
	s.commitM0(t)
	s.awaitActivity(t, fixtureActivity, mgr.ActivityViewRunning, mgr.ActivityViewAwaitingHuman,
		mgr.ActivityViewDone, mgr.ActivityViewFailed)
}

// pausedAtCommit is the committed plan, paused: the founder pauses the project while
// M0 is open and then approves M0. The seal moves the project into construction and
// the paused pump stays quiet, so the construction activity is eligible and not
// started, and the pause is recorded.
func (s *stack) pausedAtCommit(t *testing.T, policy *mgr.ExecutionPolicyInput) {
	t.Helper()
	s.atM0(t, policy)
	s.pause(t)
	s.commitM0(t)
	s.awaitActivity(t, "projectDesign", mgr.ActivityViewDone)
	s.awaitPumpIdle(t)
}

// committedNotStarted is the committed plan with no pump running: the project in
// construction under policy, its construction activity eligible and not started, no
// pause. The system holds this state when a resume's pump start fails (ResumeProject:
// "the record is clear, so the 30s sweep pumps the project"), and it is reached the
// same way: the design walked under the birth policy and paused at commit
// (pausedAtCommit), the construction policy chosen, then the pause record cleared
// through projectStateAccess's RecordOperatorResumed with no pump started. (An
// explicit policy that gates no project-design phase lets M0 pass unattended, so the
// construction policy is chosen after the commitment, not before it.)
func (s *stack) committedNotStarted(t *testing.T, policy *mgr.ExecutionPolicyInput) {
	t.Helper()
	s.pausedAtCommit(t, nil)
	if policy != nil {
		// The seal's last writes can still be landing on main when the pump goes idle;
		// the founder whose policy write loses that race to a newer head re-submits it,
		// as the store's compare-and-swap asks.
		eventually(t, walkWindow, "the construction review policy recorded", func() (bool, string) {
			if err := s.mgr.SetProjectExecutionPolicy(s.h.ManagerContext(t), mgr.ProjectID(s.project), *policy); err != nil {
				return false, err.Error()
			}
			return true, ""
		})
	}
	pid := projectstate.ProjectID(s.project)
	eventually(t, walkWindow, "the pause record cleared", func() (bool, string) {
		v, err := s.ps.ReadProjectVersion(s.h.ResourceAccessContext(t, s.nextKey("version")), pid)
		if err != nil {
			return false, err.Error()
		}
		rc := s.h.ResourceAccessContext(t, s.nextKey("resume-record"))
		if _, err := s.ps.RecordOperatorResumed(rc, pid, v, rc.IdempotencyKey); err != nil {
			return false, err.Error()
		}
		return true, ""
	})
}

// pause is the operator's pause, settled: recorded, and the pump gone quiet.
func (s *stack) pause(t *testing.T) {
	t.Helper()
	if err := s.mgr.SetProjectRunState(s.h.ManagerContext(t), mgr.ProjectID(s.project), mgr.ProjectPaused, pauseReason); err != nil {
		t.Fatalf("hooks: pause the project: %v", err)
	}
	settlePaused(t, s)
}

// ---------------------------------------------------------------------------
// Waiting on the asynchronous half, through the subject's own read ops
// ---------------------------------------------------------------------------

// walkWindow bounds a wait on the asynchronous walk. The venue observes every
// dispatch on the delivery Manager's own poll ladder (the first re-poll is 15 s after
// the submit), so a walk through a construction activity's tasks takes minutes.
const walkWindow = 6 * time.Minute

// eventually polls probe until it reports done or the window closes.
func eventually(t *testing.T, window time.Duration, what string, probe func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(window)
	last := ""
	for time.Now().Before(deadline) {
		ok, detail := probe()
		if ok {
			return
		}
		last = detail
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("hooks: %s did not happen within %s (last: %s)", what, window, last)
}

// awaitActivity waits until the activity view reads one of states.
func (s *stack) awaitActivity(t *testing.T, activity string, states ...mgr.ActivityViewState) mgr.ActivityView {
	t.Helper()
	var view mgr.ActivityView
	eventually(t, walkWindow, fmt.Sprintf("activity %s reaching %v", activity, states), func() (bool, string) {
		v, err := s.mgr.QueryActivityView(s.h.ManagerContext(t), mgr.ProjectID(s.project), mgr.ActivityID(activity))
		if err != nil {
			return false, err.Error()
		}
		view = v
		return slices.Contains(states, v.State), string(v.State)
	})
	return view
}

// summary reads the project's head state.
func (s *stack) summary(t *testing.T) (*mgr.ProjectState, error) {
	t.Helper()
	pid := s.project
	v, err := s.mgr.QueryProjectView(s.h.ManagerContext(t), mgr.ProjectViewQuery{Kind: mgr.ProjectViewSummary, ProjectID: &pid})
	if err != nil {
		return nil, err
	}
	if v.Summary == nil {
		return nil, errors.New("no summary")
	}
	return v.Summary, nil
}

// awaitSummary waits until the head state satisfies ok.
func (s *stack) awaitSummary(t *testing.T, what string, ok func(*mgr.ProjectState) bool) {
	t.Helper()
	eventually(t, walkWindow, what, func() (bool, string) {
		p, err := s.summary(t)
		if err != nil {
			return false, err.Error()
		}
		return ok(p), fmt.Sprintf("phase %s, paused %v, construction started %v", p.PhaseName, p.OperatorPaused, p.ConstructionStarted)
	})
}

// awaitPumpIdle waits until the project's pump reports no open run.
func (s *stack) awaitPumpIdle(t *testing.T) {
	t.Helper()
	pid := s.project
	eventually(t, walkWindow, "the pump going idle", func() (bool, string) {
		v, err := s.mgr.QueryProjectView(s.h.ManagerContext(t), mgr.ProjectViewQuery{Kind: mgr.ProjectViewPump, ProjectID: &pid})
		if err != nil {
			return false, err.Error()
		}
		return v.Pump != nil && !v.Pump.Open, "pump open"
	})
}

// revisionCounts is how many revisions each task of an activity holds.
func revisionCounts(v mgr.ActivityView) map[string]int {
	counts := map[string]int{}
	for _, task := range v.Tasks {
		counts[task.ID] = len(task.Revisions)
	}
	return counts
}

// newRoundAwaitsHuman reports whether a task opened a revision since before was taken and
// that revision is a round awaiting the reviewer.
func newRoundAwaitsHuman(v mgr.ActivityView, before map[string]int) bool {
	for _, task := range v.Tasks {
		n := len(task.Revisions)
		if n > before[task.ID] && task.Revisions[n-1].Outcome == mgr.TaskRevisionAwaitingHuman {
			return true
		}
	}
	return false
}

// designOrder is the reserved design activities in plan order.
var designOrder = []string{"requirements", "architecture", "projectDesign"}

// snapshotRevisions records, before a step whose settler waits on a design walk, the
// revisions every design activity holds — what that settler measures "newer" against.
func (s *stack) snapshotRevisions(t *testing.T) {
	t.Helper()
	before := map[string]map[string]int{}
	for _, activity := range designOrder {
		v, err := s.mgr.QueryActivityView(s.h.ManagerContext(t), mgr.ProjectID(s.project), mgr.ActivityID(activity))
		if err != nil {
			continue
		}
		before[activity] = revisionCounts(v)
	}
	s.mu.Lock()
	s.revisionsBefore = before
	s.mu.Unlock()
}

// settleDesignWalk waits until the design activity's walk has consumed the decision
// the step signalled, as the reviewer's screen would show it: the activity finished
// (or failed), or it is back at a gate on a NEWER revision than the step found.
func settleDesignWalk(activity string) settler {
	return func(t *testing.T, s *stack) {
		t.Helper()
		s.mu.Lock()
		before := s.revisionsBefore[activity]
		s.mu.Unlock()
		eventually(t, walkWindow, "the "+activity+" walk acting on the step", func() (bool, string) {
			v, err := s.mgr.QueryActivityView(s.h.ManagerContext(t), mgr.ProjectID(s.project), mgr.ActivityID(activity))
			if err != nil {
				return false, err.Error()
			}
			switch v.State {
			case mgr.ActivityViewDone, mgr.ActivityViewFailed:
				return true, ""
			case mgr.ActivityViewAwaitingHuman:
				return newRoundAwaitsHuman(v, before), "still at the same gate"
			case mgr.ActivityViewNotStarted, mgr.ActivityViewRunning:
			}
			return false, string(v.State)
		})
	}
}

// settleCommitment settles an M0 approve: the walk acted on it, and the commitment it
// records — the seal that moves the project into construction, the step after the gate
// passes — has landed. The activity reads done the moment its gate attempt passes, a
// beat before the seal's own write. The wait is bounded and does not fail: a seal that
// never lands is the probe's to report.
func settleCommitment(t *testing.T, s *stack) {
	t.Helper()
	settleDesignWalk("projectDesign")(t, s)
	pid := s.project
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		v, err := s.mgr.QueryProjectView(s.h.ManagerContext(t), mgr.ProjectViewQuery{Kind: mgr.ProjectViewSummary, ProjectID: &pid})
		if err == nil && v.Summary != nil && v.Summary.PhaseName == "construction" {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// settleAtGate waits for a re-entered design activity to hold its gate for a human
// again: the re-opened walk re-runs the activity and presents the revision. A done
// activity is the walk before the re-entry took effect, not its outcome.
func settleAtGate(activity string) settler {
	return func(t *testing.T, s *stack) {
		t.Helper()
		s.awaitActivity(t, activity, mgr.ActivityViewAwaitingHuman)
	}
}

// settleActivityStates waits until the construction activity reads one of states.
func settleActivityStates(states ...mgr.ActivityViewState) settler {
	return func(t *testing.T, s *stack) {
		t.Helper()
		s.awaitActivity(t, fixtureActivity, states...)
	}
}

// settled is every state a walk rests in until someone acts.
var settled = []mgr.ActivityViewState{mgr.ActivityViewDone, mgr.ActivityViewFailed, mgr.ActivityViewAwaitingHuman}

// settleRedispatched waits until an override's re-dispatch has run and the walk has
// settled again.
func settleRedispatched(t *testing.T, s *stack) {
	t.Helper()
	before := len(s.venue.jobs())
	eventually(t, walkWindow, "the override's re-dispatch", func() (bool, string) {
		return len(s.venue.jobs()) > before, "no new job"
	})
	s.awaitActivity(t, fixtureActivity, settled...)
}

// settlePumpOpen waits until the pump a resume started is running and has started
// the construction activity it parks on.
func settlePumpOpen(t *testing.T, s *stack) {
	t.Helper()
	s.awaitActivity(t, fixtureActivity, mgr.ActivityViewRunning, mgr.ActivityViewAwaitingHuman)
	pid := s.project
	eventually(t, walkWindow, "the resumed pump running", func() (bool, string) {
		v, err := s.mgr.QueryProjectView(s.h.ManagerContext(t), mgr.ProjectViewQuery{Kind: mgr.ProjectViewPump, ProjectID: &pid})
		if err != nil {
			return false, err.Error()
		}
		return v.Pump != nil && v.Pump.Open, "pump closed"
	})
}

// settlePaused waits until the pause the step requested is recorded and applied.
func settlePaused(t *testing.T, s *stack) {
	t.Helper()
	s.awaitSummary(t, "the operator pause being recorded", func(p *mgr.ProjectState) bool { return p.OperatorPaused })
	s.awaitPumpIdle(t)
}

// ---------------------------------------------------------------------------
// The venue scripts
// ---------------------------------------------------------------------------

// failConstructionTimes fails the first n construction dispatches.
func failConstructionTimes(n int) func(*stack) func(int, agentJob) runOutcome {
	return func(*stack) func(int, agentJob) runOutcome {
		var mu sync.Mutex
		failed := 0
		return func(_ int, j agentJob) runOutcome {
			mu.Lock()
			defer mu.Unlock()
			if j.isConstruction() && failed < n {
				failed++
				return runFails
			}
			return runSucceeds
		}
	}
}

// failConstructionUntil fails every construction dispatch until step `step` of the
// scenario has been arranged — the operator's override at that step is what fixes the
// cause (a flaky runner, a namespace collision).
func failConstructionUntil(step int) func(*stack) func(int, agentJob) runOutcome {
	return func(s *stack) func(int, agentJob) runOutcome {
		return func(_ int, j agentJob) runOutcome {
			if j.isConstruction() && !s.arrangedStep(step) {
				return runFails
			}
			return runSucceeds
		}
	}
}

// ---------------------------------------------------------------------------
// The arrangements, by scenario
// ---------------------------------------------------------------------------

// arrangeExecute: the plan committed under policy with the venue scripted and its
// construction activity eligible, not yet started (committedNotStarted); step 1's
// tick is the one that dispatches it. Before every later step the walk the pump
// started has settled where a human (or nobody) is next.
func arrangeExecute(policy *mgr.ExecutionPolicyInput, script func(*stack) func(int, agentJob) runOutcome) arrangement {
	return func(t *testing.T, s *stack, seq int) {
		t.Helper()
		switch seq {
		case 1:
			if script != nil {
				s.venue.script(script(s))
			}
			s.committedNotStarted(t, policy)
		case 2:
			s.awaitActivity(t, fixtureActivity, settled...)
		default:
			s.awaitActivity(t, fixtureActivity, settled...)
			v, err := s.mgr.QueryActivityView(s.h.ManagerContext(t), mgr.ProjectID(s.project), fixtureActivity)
			if err == nil && v.State != mgr.ActivityViewAwaitingHuman {
				s.awaitPumpIdle(t)
			}
		}
	}
}

// onFirst runs fn before step 1 only.
func onFirst(fn func(t *testing.T, s *stack)) arrangement {
	return func(t *testing.T, s *stack, seq int) {
		t.Helper()
		if seq == 1 {
			fn(t, s)
		}
	}
}

// inFlightUnder is the project in construction under policy.
func inFlightUnder(policy *mgr.ExecutionPolicyInput) arrangement {
	return onFirst(func(t *testing.T, s *stack) { s.inFlight(t, policy) })
}

// arrangeBuilt drives C-review-engine's walk to done under vibes — the activity
// whose generated source the owner then fetches.
func arrangeBuilt(t *testing.T, s *stack) {
	t.Helper()
	s.inFlight(t, vibes)
	s.awaitActivity(t, fixtureActivity, mgr.ActivityViewDone)
	s.awaitPumpIdle(t)
}

// arrangeFailed puts C-review-engine where the requeue use case finds it: its walk
// ran, every construction dispatch failed until the supervision budget was spent, and
// the activity exited failed. The venue keeps failing until the operator's requeue
// (step 2), after which the re-dispatched job is still in progress when the operator
// looks.
func arrangeFailed(t *testing.T, s *stack) {
	t.Helper()
	s.venue.script(func(_ int, j agentJob) runOutcome {
		switch {
		case !j.isConstruction():
			return runSucceeds
		case s.arrangedStep(2):
			return runStaysRunning
		}
		return runFails
	})
	s.inFlight(t, vibes)
	s.awaitActivity(t, fixtureActivity, mgr.ActivityViewFailed)
	s.awaitPumpIdle(t)
}

// arrangePaused is the project in construction with the operator's pause recorded and
// its construction activity not started (pausedAtCommit).
func arrangePaused(t *testing.T, s *stack) {
	t.Helper()
	s.pausedAtCommit(t, nil)
}

// arrangeAsk: [yes: session branch] finds the architecture walk live at its review
// gate, [no: committed] finds it done; [no: dispatch miss] is the venue refusing the
// answer job's seat.
func arrangeAsk(live, dispatchMiss bool) arrangement {
	return onFirst(func(t *testing.T, s *stack) {
		if live {
			s.architectureAtGate(t)
		} else {
			s.atM0(t, nil)
		}
		if dispatchMiss {
			s.venue.refuseSeat(func(command string) bool { return strings.Contains(command, "answer") })
		}
	})
}

// arrangeAtM0 holds the project-design walk at M0.
func arrangeAtM0(t *testing.T, s *stack) {
	t.Helper()
	s.atM0(t, nil)
}

// arrangeArchitectureAtGate holds the architecture walk at its review gate.
func arrangeArchitectureAtGate(t *testing.T, s *stack) {
	t.Helper()
	s.architectureAtGate(t)
}

// arrangements is keyed by scenario id; a scenario without an entry arranges nothing.
var arrangements = map[string]arrangement{
	"add-a-use-case-to-an-in-flight-project-P1": inFlightUnder(nil),
	"add-a-use-case-to-an-in-flight-project-P2": inFlightUnder(nil),
	"add-a-use-case-to-an-in-flight-project-P3": inFlightUnder(nil),
	"add-a-use-case-to-an-in-flight-project-P4": inFlightUnder(nil),

	"ask-a-clarifying-question-during-review-P1": arrangeAsk(true, false),
	"ask-a-clarifying-question-during-review-P2": arrangeAsk(true, true),
	"ask-a-clarifying-question-during-review-P3": arrangeAsk(false, false),
	"ask-a-clarifying-question-during-review-P4": arrangeAsk(false, true),

	"commit-to-a-project-option-P1": onFirst(arrangeAtM0),
	"commit-to-a-project-option-P2": onFirst(arrangeAtM0),
	"commit-to-a-project-option-P3": onFirst(arrangeAtM0),

	"download-generated-source-code-P1": onFirst(arrangeBuilt),
	"download-generated-source-code-P2": onFirst(arrangeBuilt),

	// execute-a-project-activity: the review policy names who judges, the agent script
	// names whether the construction dispatch lands.
	"execute-a-project-activity-P1": arrangeExecute(vibes, nil),
	"execute-a-project-activity-P2": arrangeExecute(vibes, failConstructionUntil(3)),
	"execute-a-project-activity-P3": arrangeExecute(vibes, nil),
	"execute-a-project-activity-P4": arrangeExecute(humanAtConstruction, nil),
	"execute-a-project-activity-P5": arrangeExecute(vibes, failConstructionTimes(1)),
	"execute-a-project-activity-P6": arrangeExecute(vibes, failConstructionUntil(4)),
	"execute-a-project-activity-P7": arrangeExecute(vibes, nil),

	"pause-construction-P1": inFlightUnder(nil),

	"replan-under-scope-change-P1": inFlightUnder(nil),
	"replan-under-scope-change-P2": inFlightUnder(nil),
	"replan-under-scope-change-P3": inFlightUnder(nil),

	"requeue-a-failed-construction-activity-P1": onFirst(arrangeFailed),

	"resume-paused-construction-P1": onFirst(arrangePaused),
	"resume-paused-construction-P2": onFirst(arrangePauseInFlight),

	"send-back-change-requests-for-a-redraft-P1": onFirst(arrangeArchitectureAtGate),
	"send-back-change-requests-for-a-redraft-P2": onFirst(arrangeArchitectureAtGate),

	"track-weekly-project-progress-P1": inFlightUnder(nil),
	"track-weekly-project-progress-P2": inFlightUnder(nil),
	"track-weekly-project-progress-P3": inFlightUnder(nil),
	"track-weekly-project-progress-P4": inFlightUnder(nil),

	"view-the-project-state-log-P1": onFirst(arrangeStateLog),
	"view-the-project-state-log-P2": onFirst(arrangeStateLog),
}

// settlers is keyed by "<scenario>#<seq>": the steps whose tool only enqueues work.
var settlers = map[string]settler{
	"add-a-use-case-to-an-in-flight-project-P1#2": settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P1#3": settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P2#2": settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P2#3": settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P3#2": settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P3#3": settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P4#2": settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P4#3": settleDesignWalk("requirements"),

	"execute-a-project-activity-P2#3": settleRedispatched,
	"execute-a-project-activity-P4#3": settleActivityStates(mgr.ActivityViewDone, mgr.ActivityViewFailed),
	"execute-a-project-activity-P6#3": settleRedispatched,
	"execute-a-project-activity-P6#4": settleRedispatched,

	"pause-construction-P1#1": settlePaused,

	"resume-paused-construction-P1#2": settlePumpOpen,

	"replan-under-scope-change-P1#2": settleDesignWalk("architecture"),
	"replan-under-scope-change-P2#2": settleAtGate("projectDesign"),
	"replan-under-scope-change-P2#3": settleAtGate("projectDesign"),
	"replan-under-scope-change-P3#2": settleAtGate("projectDesign"),
	"replan-under-scope-change-P3#3": settleAtGate("projectDesign"),

	"requeue-a-failed-construction-activity-P1#2": settleActivityStates(mgr.ActivityViewRunning),

	"commit-to-a-project-option-P1#2": settleCommitment,
	"commit-to-a-project-option-P2#2": settleCommitment,
	"commit-to-a-project-option-P2#3": settleDesignWalk("projectDesign"),

	"send-back-change-requests-for-a-redraft-P1#1": settleDesignWalk("architecture"),
	"send-back-change-requests-for-a-redraft-P2#1": settleDesignWalk("architecture"),
	"send-back-change-requests-for-a-redraft-P2#3": settleDesignWalk("architecture"),
}

// startWorker serves the subject's workflows on the host's Temporal until t ends:
// the composition root's own registration over the subject.
func (s *stack) startWorker(t *testing.T) {
	t.Helper()
	s.h.StartWorker(t, mgr.TaskQueue, func(w scenariohost.Worker) { mgr.RegisterManagerWorker(w, s.mgr) })
}

// workerDeferred names the scenarios whose arrangement owns the worker's lifetime
// (see arrangePauseInFlight); every other scenario's worker serves it whole.
var workerDeferred = map[string]bool{"resume-paused-construction-P2": true}

// arrangePauseInFlight is the [no] arm of the pause-settled guard: the operator's
// pause is recorded, and a pause request is still being applied when the operator
// asks to resume. The project is put in construction and paused while the Manager's
// worker serves it (a sub-test owns that worker); with the worker gone, the
// operator's next pause request is accepted — its supervision run started — but
// nothing consumes it, so it is still being applied.
func arrangePauseInFlight(t *testing.T, s *stack) {
	t.Helper()
	t.Run("arrange-with-worker", func(at *testing.T) {
		s.startWorker(at)
		arrangePaused(at, s)
	})
	if t.Failed() {
		t.FailNow()
	}
	if err := s.mgr.SetProjectRunState(s.h.ManagerContext(t), mgr.ProjectID(s.project), mgr.ProjectPaused, pauseReason); err != nil {
		t.Fatalf("hooks: request the in-flight pause: %v", err)
	}
}

// arrangeStateLog is the project whose state log the owner reads: born, then its
// research corpus attached and its review policy chosen — each a recorded write.
func arrangeStateLog(t *testing.T, s *stack) {
	t.Helper()
	s.born(t, nil)
	owner, _, _ := strings.Cut(s.project, "/")
	pid := s.project
	research := &mgr.ResearchInput{Sources: []mgr.ResearchSource{{Title: "GTD method notes",
		Content: "Capture, clarify, organize, reflect, engage. Weekly review is mandatory."}}}
	if _, err := s.mgr.StartProject(s.h.ManagerContext(t), mgr.OwnerScope(owner), s.project, &pid, nil, research, false); err != nil {
		t.Fatalf("hooks: attach the research corpus: %v", err)
	}
	if err := s.mgr.SetProjectExecutionPolicy(s.h.ManagerContext(t), mgr.ProjectID(s.project),
		mgr.ExecutionPolicyInput{Preset: "checkpoints"}); err != nil {
		t.Fatalf("hooks: choose the review policy: %v", err)
	}
}

// stepAddAUseCaseToAnInFlightProjectP1S1 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"capture-uc","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"add-a-use-case-to-an-in-flight-project/capture-uc/1.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"Phase\":2,\"PhaseName\":\"construction\"}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[variation], replan-check=[no] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 1, "deliveryQueryProjectView", args)
}

// stepAddAUseCaseToAnInFlightProjectP1S2 — Add a Use Case to an In-Flight Project — [variation] · [no], step 2: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":2,"node":"capture-uc","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"add-a-use-case-to-an-in-flight-project/capture-uc/1.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"feedback","value":"{\"notes\":\"Amendment: add the use case 'Export the project audit trail' (a variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[variation], replan-check=[no] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP1S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 2, "deliveryDispatchActivityTask", args)
}

// stepAddAUseCaseToAnInFlightProjectP1S3 — Add a Use Case to an In-Flight Project — [variation] · [no], step 3: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Projected stimulus (JSON): {"seq":3,"node":"redraft-review","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliverySubmitReviewDecision","call":"add-a-use-case-to-an-in-flight-project/redraft-review/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliverySubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-recommitted","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4,\"stage\":1}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[variation], replan-check=[no] hold (runStep), then calls deliverySubmitReviewDecision as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP1S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 3, "deliverySubmitReviewDecision", args)
}

// stepAddAUseCaseToAnInFlightProjectP2S1 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"capture-uc","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"add-a-use-case-to-an-in-flight-project/capture-uc/1.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"Phase\":2,\"PhaseName\":\"construction\"}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[variation], replan-check=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 1, "deliveryQueryProjectView", args)
}

// stepAddAUseCaseToAnInFlightProjectP2S2 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 2: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":2,"node":"capture-uc","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"add-a-use-case-to-an-in-flight-project/capture-uc/1.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"feedback","value":"{\"notes\":\"Amendment: add the use case 'Export the project audit trail' (a variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[variation], replan-check=[yes] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP2S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 2, "deliveryDispatchActivityTask", args)
}

// stepAddAUseCaseToAnInFlightProjectP2S3 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 3: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Projected stimulus (JSON): {"seq":3,"node":"redraft-review","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliverySubmitReviewDecision","call":"add-a-use-case-to-an-in-flight-project/redraft-review/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliverySubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-recommitted","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4,\"stage\":1}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[variation], replan-check=[yes] hold (runStep), then calls deliverySubmitReviewDecision as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP2S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 3, "deliverySubmitReviewDecision", args)
}

// stepAddAUseCaseToAnInFlightProjectP3S1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"capture-uc","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"add-a-use-case-to-an-in-flight-project/capture-uc/1.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"designHealth\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"designHealth\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[core: rare], replan-check=[no] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP3S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 1, "deliveryQueryProjectView", args)
}

// stepAddAUseCaseToAnInFlightProjectP3S2 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 2: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":2,"node":"capture-uc","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"add-a-use-case-to-an-in-flight-project/capture-uc/1.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"feedback","value":"{\"notes\":\"Amendment: add the use case 'Export the project audit trail' (a variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[core: rare], replan-check=[no] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP3S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 2, "deliveryDispatchActivityTask", args)
}

// stepAddAUseCaseToAnInFlightProjectP3S3 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 3: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Projected stimulus (JSON): {"seq":3,"node":"redraft-review","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliverySubmitReviewDecision","call":"add-a-use-case-to-an-in-flight-project/redraft-review/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliverySubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-recommitted","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4,\"stage\":1}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[core: rare], replan-check=[no] hold (runStep), then calls deliverySubmitReviewDecision as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP3S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 3, "deliverySubmitReviewDecision", args)
}

// stepAddAUseCaseToAnInFlightProjectP4S1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"capture-uc","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"add-a-use-case-to-an-in-flight-project/capture-uc/1.0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"designHealth\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"designHealth\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[core: rare], replan-check=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP4S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 1, "deliveryQueryProjectView", args)
}

// stepAddAUseCaseToAnInFlightProjectP4S2 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 2: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":2,"node":"capture-uc","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"add-a-use-case-to-an-in-flight-project/capture-uc/1.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"feedback","value":"{\"notes\":\"Amendment: add the use case 'Export the project audit trail' (a variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[core: rare], replan-check=[yes] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP4S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 2, "deliveryDispatchActivityTask", args)
}

// stepAddAUseCaseToAnInFlightProjectP4S3 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 3: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Projected stimulus (JSON): {"seq":3,"node":"redraft-review","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliverySubmitReviewDecision","call":"add-a-use-case-to-an-in-flight-project/redraft-review/3"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliverySubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-recommitted","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":4,\"stage\":1}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards core-check=[core: rare], replan-check=[yes] hold (runStep), then calls deliverySubmitReviewDecision as its caller does and returns its result.
func stepAddAUseCaseToAnInFlightProjectP4S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 3, "deliverySubmitReviewDecision", args)
}

// stepAskAClarifyingQuestionDuringReviewP1S1 — Ask a Clarifying Question During Review — [yes: session branch] · [yes], step 1: DeliveryAskQuestions (tool deliveryAskQuestions)
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryAskQuestions","call":"ask-a-clarifying-question-during-review/write-questions/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryAskQuestions","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"addressee","value":"architect"},{"name":"questions","value":"[{\"jsonPath\":\"$.containers[2].responsibilities\",\"text\":\"Why does delivery-manager own the review roster rather than review-engine?\",\"anchorText\":\"review roster\",\"replyTo\":\"\"}]"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-open-on-session-branch","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"}],"expect":{"result":"{\"result\":{\"activityId\":\"architecture\",\"state\":\"awaitingHuman\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards artifact-state=[yes: session branch], answer-ran=[yes] hold (runStep), then calls deliveryAskQuestions as its caller does and returns its result.
func stepAskAClarifyingQuestionDuringReviewP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "ask-a-clarifying-question-during-review-P1", 1, "deliveryAskQuestions", args)
}

// stepAskAClarifyingQuestionDuringReviewP2S1 — Ask a Clarifying Question During Review — [yes: session branch] · [no: dispatch miss], step 1: DeliveryAskQuestions (tool deliveryAskQuestions)
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryAskQuestions","call":"ask-a-clarifying-question-during-review/write-questions/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryAskQuestions","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"addressee","value":"architect"},{"name":"questions","value":"[{\"jsonPath\":\"$.containers[2].responsibilities\",\"text\":\"Why does delivery-manager own the review roster rather than review-engine?\",\"anchorText\":\"review roster\",\"replyTo\":\"\"}]"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-open-on-session-branch","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"}],"expect":{"result":"{\"result\":{\"activityId\":\"architecture\",\"state\":\"awaitingHuman\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards artifact-state=[yes: session branch], answer-ran=[no: dispatch miss] hold (runStep), then calls deliveryAskQuestions as its caller does and returns its result.
func stepAskAClarifyingQuestionDuringReviewP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "ask-a-clarifying-question-during-review-P2", 1, "deliveryAskQuestions", args)
}

// stepAskAClarifyingQuestionDuringReviewP3S1 — Ask a Clarifying Question During Review — [no: committed] · [yes], step 1: DeliveryAskQuestions (tool deliveryAskQuestions)
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryAskQuestions","call":"ask-a-clarifying-question-during-review/write-questions/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryAskQuestions","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"addressee","value":"architect"},{"name":"questions","value":"[{\"jsonPath\":\"$.containers[2].responsibilities\",\"text\":\"Why does delivery-manager own the review roster rather than review-engine?\",\"anchorText\":\"review roster\",\"replyTo\":\"\"}]"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-seeded-on-main","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"}],"expect":{"result":"{\"result\":{\"activityId\":\"architecture\",\"state\":\"done\"}}","errorExpected":false}},{"for":"thread-seeded-on-main","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5,\"stage\":1}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards artifact-state=[no: committed], answer-ran=[yes] hold (runStep), then calls deliveryAskQuestions as its caller does and returns its result.
func stepAskAClarifyingQuestionDuringReviewP3S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "ask-a-clarifying-question-during-review-P3", 1, "deliveryAskQuestions", args)
}

// stepAskAClarifyingQuestionDuringReviewP4S1 — Ask a Clarifying Question During Review — [no: committed] · [no: dispatch miss], step 1: DeliveryAskQuestions (tool deliveryAskQuestions)
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryAskQuestions","call":"ask-a-clarifying-question-during-review/write-questions/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryAskQuestions","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"addressee","value":"architect"},{"name":"questions","value":"[{\"jsonPath\":\"$.containers[2].responsibilities\",\"text\":\"Why does delivery-manager own the review roster rather than review-engine?\",\"anchorText\":\"review roster\",\"replyTo\":\"\"}]"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-seeded-on-main","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"}],"expect":{"result":"{\"result\":{\"activityId\":\"architecture\",\"state\":\"done\"}}","errorExpected":false}},{"for":"thread-seeded-on-main","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5,\"stage\":1}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards artifact-state=[no: committed], answer-ran=[no: dispatch miss] hold (runStep), then calls deliveryAskQuestions as its caller does and returns its result.
func stepAskAClarifyingQuestionDuringReviewP4S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "ask-a-clarifying-question-during-review-P4", 1, "deliveryAskQuestions", args)
}

// stepCommitToAProjectOptionP1S1 — Commit to a Project Option — [commit] · [no], step 1: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"commit-to-a-project-option/await-decision/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"present-sdp-review","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"result\":{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards decision=[commit], revoke-window=[no] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepCommitToAProjectOptionP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "commit-to-a-project-option-P1", 1, "deliveryDispatchActivityTask", args)
}

// stepCommitToAProjectOptionP2S1 — Commit to a Project Option — [commit] · [yes], step 1: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"commit-to-a-project-option/await-decision/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"present-sdp-review","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"result\":{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards decision=[commit], revoke-window=[yes] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepCommitToAProjectOptionP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "commit-to-a-project-option-P2", 1, "deliveryDispatchActivityTask", args)
}

// stepCommitToAProjectOptionP1S2 — Commit to a Project Option — [commit] · [no], step 2: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// The founder approves the option the review presents; the walk records the commitment
// and seals the project into construction (settleCommitment) before the phase probe.
func stepCommitToAProjectOptionP1S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "commit-to-a-project-option-P1", 2, "deliverySubmitReviewDecision", args)
}

// stepCommitToAProjectOptionP2S2 — Commit to a Project Option — [commit] · [yes], step 2: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// As P1's approve: the commitment lands before the phase probe.
func stepCommitToAProjectOptionP2S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "commit-to-a-project-option-P2", 2, "deliverySubmitReviewDecision", args)
}

// stepCommitToAProjectOptionP2S3 — Commit to a Project Option — [commit] · [yes], step 3: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Inside the grace window the founder revokes the commitment; the walk re-opens
// project design before the phase probe.
func stepCommitToAProjectOptionP2S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "commit-to-a-project-option-P2", 3, "deliverySubmitReviewDecision", args)
}

// stepCommitToAProjectOptionP3S1 — Commit to a Project Option — [reject-all + feedback], step 1: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"commit-to-a-project-option/await-decision/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"present-sdp-review","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"result\":{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards decision=[reject-all + feedback] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepCommitToAProjectOptionP3S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "commit-to-a-project-option-P3", 1, "deliveryDispatchActivityTask", args)
}

// stepDownloadGeneratedSourceCodeP1S1 — Download Generated Source Code — [clone or pull], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"open-repo","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"download-generated-source-code/open-repo/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"constructionStarted\":true}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards fetch-how=[clone or pull] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepDownloadGeneratedSourceCodeP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "download-generated-source-code-P1", 1, "deliveryQueryProjectView", args)
}

// stepDownloadGeneratedSourceCodeP2S1 — Download Generated Source Code — [archive], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"open-repo","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"download-generated-source-code/open-repo/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"constructionStarted\":true}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards fetch-how=[archive] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepDownloadGeneratedSourceCodeP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "download-generated-source-code-P2", 1, "deliveryQueryProjectView", args)
}

// stepExecuteAProjectActivityP1S1 — Execute a Project Activity — dispatch · yes, step 1: DeliveryExecuteNextActivity (tool deliveryExecuteNextActivity)
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"operator","to":"mcpclient","op":"DeliveryExecuteNextActivity","call":"execute-a-project-activity/pump-fires/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"tickID","value":"dct-mcp-exec-P1-tick-0001"}],"expect":{"result":"{\"result\":{\"dispatched\":true,\"activityIds\":[\"C-review-engine\"]}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=yes hold (runStep), then calls deliveryExecuteNextActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P1", 1, "deliveryExecuteNextActivity", args)
}

// stepExecuteAProjectActivityP1S2 — Execute a Project Activity — dispatch · yes, step 2: DeliveryQueryActivityView (tool deliveryQueryActivityView)
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryActivityView","call":"execute-a-project-activity/activity-eligible/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"type\":\"service\"}}","errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"done\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=yes hold (runStep), then calls deliveryQueryActivityView as its caller does and returns its result.
func stepExecuteAProjectActivityP1S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P1", 2, "deliveryQueryActivityView", args)
}

// stepExecuteAProjectActivityP2S1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 1: DeliveryExecuteNextActivity (tool deliveryExecuteNextActivity)
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"operator","to":"mcpclient","op":"DeliveryExecuteNextActivity","call":"execute-a-project-activity/pump-fires/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"tickID","value":"dct-mcp-exec-P2-tick-0001"}],"expect":{"result":"{\"result\":{\"dispatched\":true,\"activityIds\":[\"C-review-engine\"]}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=yes hold (runStep), then calls deliveryExecuteNextActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P2", 1, "deliveryExecuteNextActivity", args)
}

// stepExecuteAProjectActivityP2S2 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 2: DeliveryQueryActivityView (tool deliveryQueryActivityView)
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryActivityView","call":"execute-a-project-activity/activity-eligible/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"type\":\"service\"}}","errorExpected":false},"probes":[{"for":"escalate-operator","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"failed\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=yes hold (runStep), then calls deliveryQueryActivityView as its caller does and returns its result.
func stepExecuteAProjectActivityP2S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P2", 2, "deliveryQueryActivityView", args)
}

// stepExecuteAProjectActivityP2S3 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 3: DeliveryOverrideActivity (tool deliveryOverrideActivity)
// Projected stimulus (JSON): {"seq":3,"node":"operator-steers","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryOverrideActivity","call":"execute-a-project-activity/operator-steers/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliveryOverrideActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"},{"name":"override","value":"{\"kind\":2,\"notes\":\"Retry: the last attempt failed on a flaky CI runner, not on the code; rerun with the change unchanged.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"done\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=yes hold (runStep), then calls deliveryOverrideActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP2S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P2", 3, "deliveryOverrideActivity", args)
}

// stepExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: DeliveryExecuteNextActivity (tool deliveryExecuteNextActivity)
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"operator","to":"mcpclient","op":"DeliveryExecuteNextActivity","call":"execute-a-project-activity/pump-fires/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"tickID","value":"dct-mcp-exec-P3-tick-0001"}],"expect":{"result":"{\"result\":{\"dispatched\":true,\"activityIds\":[\"C-review-engine\"]}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=approve hold (runStep), then calls deliveryExecuteNextActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P3", 1, "deliveryExecuteNextActivity", args)
}

// stepExecuteAProjectActivityP3S2 — Execute a Project Activity — review · agents only · approve, step 2: DeliveryQueryActivityView (tool deliveryQueryActivityView)
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryActivityView","call":"execute-a-project-activity/activity-eligible/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"type\":\"service\"}}","errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"done\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=approve hold (runStep), then calls deliveryQueryActivityView as its caller does and returns its result.
func stepExecuteAProjectActivityP3S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P3", 2, "deliveryQueryActivityView", args)
}

// stepExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: DeliveryExecuteNextActivity (tool deliveryExecuteNextActivity)
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"operator","to":"mcpclient","op":"DeliveryExecuteNextActivity","call":"execute-a-project-activity/pump-fires/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"tickID","value":"dct-mcp-exec-P4-tick-0001"}],"expect":{"result":"{\"result\":{\"dispatched\":true,\"activityIds\":[\"C-review-engine\"]}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=human required, verdict=approve hold (runStep), then calls deliveryExecuteNextActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P4", 1, "deliveryExecuteNextActivity", args)
}

// stepExecuteAProjectActivityP4S2 — Execute a Project Activity — review · human required · approve, step 2: DeliveryQueryActivityView (tool deliveryQueryActivityView)
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryActivityView","call":"execute-a-project-activity/activity-eligible/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"type\":\"service\"}}","errorExpected":false},"probes":[{"for":"requires-human","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"awaitingHuman\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=human required, verdict=approve hold (runStep), then calls deliveryQueryActivityView as its caller does and returns its result.
func stepExecuteAProjectActivityP4S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P4", 2, "deliveryQueryActivityView", args)
}

// stepExecuteAProjectActivityP4S3 — Execute a Project Activity — review · human required · approve, step 3: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Projected stimulus (JSON): {"seq":3,"node":"human-decision","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliverySubmitReviewDecision","call":"execute-a-project-activity/human-decision/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliverySubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"},{"name":"taskID","value":"codeReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"done\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=human required, verdict=approve hold (runStep), then calls deliverySubmitReviewDecision as its caller does and returns its result.
func stepExecuteAProjectActivityP4S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P4", 3, "deliverySubmitReviewDecision", args)
}

// stepExecuteAProjectActivityP5S1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 1: DeliveryExecuteNextActivity (tool deliveryExecuteNextActivity)
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"operator","to":"mcpclient","op":"DeliveryExecuteNextActivity","call":"execute-a-project-activity/pump-fires/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"tickID","value":"dct-mcp-exec-P5-tick-0001"}],"expect":{"result":"{\"result\":{\"dispatched\":true,\"activityIds\":[\"C-review-engine\"]}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=yes: retry, exit-met=yes hold (runStep), then calls deliveryExecuteNextActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP5S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P5", 1, "deliveryExecuteNextActivity", args)
}

// stepExecuteAProjectActivityP5S2 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 2: DeliveryQueryActivityView (tool deliveryQueryActivityView)
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryActivityView","call":"execute-a-project-activity/activity-eligible/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"type\":\"service\"}}","errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"done\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=yes: retry, exit-met=yes hold (runStep), then calls deliveryQueryActivityView as its caller does and returns its result.
func stepExecuteAProjectActivityP5S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P5", 2, "deliveryQueryActivityView", args)
}

// stepExecuteAProjectActivityP6S1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 1: DeliveryExecuteNextActivity (tool deliveryExecuteNextActivity)
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"operator","to":"mcpclient","op":"DeliveryExecuteNextActivity","call":"execute-a-project-activity/pump-fires/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"tickID","value":"dct-mcp-exec-P6-tick-0001"}],"expect":{"result":"{\"result\":{\"dispatched\":true,\"activityIds\":[\"C-review-engine\"]}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=no, operator-intervened=yes hold (runStep), then calls deliveryExecuteNextActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP6S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P6", 1, "deliveryExecuteNextActivity", args)
}

// stepExecuteAProjectActivityP6S2 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 2: DeliveryQueryActivityView (tool deliveryQueryActivityView)
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryActivityView","call":"execute-a-project-activity/activity-eligible/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"type\":\"service\"}}","errorExpected":false},"probes":[{"for":"escalate-operator","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"failed\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=no, operator-intervened=yes hold (runStep), then calls deliveryQueryActivityView as its caller does and returns its result.
func stepExecuteAProjectActivityP6S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P6", 2, "deliveryQueryActivityView", args)
}

// stepExecuteAProjectActivityP6S3 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 3: DeliveryOverrideActivity (tool deliveryOverrideActivity)
// Projected stimulus (JSON): {"seq":3,"node":"operator-steers","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryOverrideActivity","call":"execute-a-project-activity/operator-steers/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliveryOverrideActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"},{"name":"override","value":"{\"kind\":2,\"notes\":\"Retry: the last attempt failed on a flaky CI runner, not on the code; rerun with the change unchanged.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"escalate-operator","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"failed\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=no, operator-intervened=yes hold (runStep), then calls deliveryOverrideActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP6S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P6", 3, "deliveryOverrideActivity", args)
}

// stepExecuteAProjectActivityP6S4 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 4: DeliveryOverrideActivity (tool deliveryOverrideActivity)
// Projected stimulus (JSON): {"seq":4,"node":"operator-steers","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryOverrideActivity","call":"execute-a-project-activity/operator-steers/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"DeliveryOverrideActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"},{"name":"override","value":"{\"kind\":2,\"notes\":\"Retry again: pin the integration test's Temporal namespace; the earlier failure was a namespace collision.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"done\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=dispatch, exit-met=no, retry-remain=no: exhausted, operator-intervened=no, operator-intervened=yes hold (runStep), then calls deliveryOverrideActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP6S4(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P6", 4, "deliveryOverrideActivity", args)
}

// stepExecuteAProjectActivityP7S1 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 1: DeliveryExecuteNextActivity (tool deliveryExecuteNextActivity)
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"operator","to":"mcpclient","op":"DeliveryExecuteNextActivity","call":"execute-a-project-activity/pump-fires/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"tickID","value":"dct-mcp-exec-P7-tick-0001"}],"expect":{"result":"{\"result\":{\"dispatched\":true,\"activityIds\":[\"C-review-engine\"]}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=sendBack: reopens the judged task as revision n+1, task-kind=dispatch, exit-met=yes hold (runStep), then calls deliveryExecuteNextActivity as its caller does and returns its result.
func stepExecuteAProjectActivityP7S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P7", 1, "deliveryExecuteNextActivity", args)
}

// stepExecuteAProjectActivityP7S2 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 2: DeliveryQueryActivityView (tool deliveryQueryActivityView)
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryActivityView","call":"execute-a-project-activity/activity-eligible/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"type\":\"service\"}}","errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"done\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards task-kind=review, requires-human=agents only, verdict=sendBack: reopens the judged task as revision n+1, task-kind=dispatch, exit-met=yes hold (runStep), then calls deliveryQueryActivityView as its caller does and returns its result.
func stepExecuteAProjectActivityP7S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "execute-a-project-activity-P7", 2, "deliveryQueryActivityView", args)
}

// stepManageProjectsP1S1 — Manage Projects — [no: self-operated], step 1: DeliveryStartProject (tool deliveryStartProject)
// Projected stimulus (JSON): {"seq":1,"node":"prepare-repo","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryStartProject","call":"manage-projects/prepare-repo/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryStartProject","inputs":[{"name":"owner","value":"dct-mcp-owner-p1"},{"name":"name","value":"dct-gtd-app"},{"name":"model","value":"selfOperated"},{"name":"start","value":"true"}],"expect":{"result":"{\"result\":{\"projectId\":\"dct-mcp-owner-p1/dct-gtd-app\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards operating-model=[no: self-operated] hold (runStep), then calls deliveryStartProject as its caller does and returns its result.
func stepManageProjectsP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "manage-projects-P1", 1, "deliveryStartProject", args)
}

// stepManageProjectsP2S1 — Manage Projects — [yes], step 1: DeliveryStartProject (tool deliveryStartProject)
// Projected stimulus (JSON): {"seq":1,"node":"prepare-repo","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryStartProject","call":"manage-projects/prepare-repo/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryStartProject","inputs":[{"name":"owner","value":"dct-mcp-owner-p2"},{"name":"name","value":"dct-gtd-app"},{"name":"model","value":"archistratorOperated"},{"name":"start","value":"true"}],"expect":{"result":"{\"result\":{\"projectId\":\"dct-mcp-owner-p2/dct-gtd-app\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards operating-model=[yes] hold (runStep), then calls deliveryStartProject as its caller does and returns its result.
func stepManageProjectsP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "manage-projects-P2", 1, "deliveryStartProject", args)
}

// stepPauseConstructionP1S1 — Pause Construction, step 1: DeliverySetProjectRunState (tool deliverySetProjectRunState)
// Projected stimulus (JSON): {"seq":1,"node":"choose-pause","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliverySetProjectRunState","call":"pause-construction/choose-pause/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliverySetProjectRunState","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"runState","value":"paused"},{"name":"reason","value":"Holding construction while the billing vendor contract is renegotiated."}],"expect":{"errorExpected":false},"probes":[{"for":"run-state-paused","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"operatorPaused\":true,\"pauseReason\":\"Holding construction while the billing vendor contract is renegotiated.\"}}}","errorExpected":false}},{"for":"stop-pump","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"pump\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"pump\",\"pump\":{\"open\":false}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards (none) hold (runStep), then calls deliverySetProjectRunState as its caller does and returns its result.
func stepPauseConstructionP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "pause-construction-P1", 1, "deliverySetProjectRunState", args)
}

// stepReplanUnderScopeChangeP1S1 — Replan Under Scope Change — [yes], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"raise-scope-trigger","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"replan-under-scope-change/raise-scope-trigger/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"Phase\":2,\"PhaseName\":\"construction\"}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepReplanUnderScopeChangeP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "replan-under-scope-change-P1", 1, "deliveryQueryProjectView", args)
}

// stepReplanUnderScopeChangeP1S2 — Replan Under Scope Change — [yes], step 2: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":2,"node":"back-to-design","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"replan-under-scope-change/back-to-design/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureDraft"},{"name":"feedback","value":"{\"notes\":\"Architecture change: split billing into metering and invoicing components; this is not a scope change.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"system-amendment-open","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[yes] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepReplanUnderScopeChangeP1S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "replan-under-scope-change-P1", 2, "deliveryDispatchActivityTask", args)
}

// stepReplanUnderScopeChangeP2S1 — Replan Under Scope Change — [no: scope change] · [accept], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"raise-scope-trigger","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"replan-under-scope-change/raise-scope-trigger/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"Phase\":2,\"PhaseName\":\"construction\"}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[accept] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepReplanUnderScopeChangeP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "replan-under-scope-change-P2", 1, "deliveryQueryProjectView", args)
}

// stepReplanUnderScopeChangeP2S2 — Replan Under Scope Change — [no: scope change] · [accept], step 2: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":2,"node":"reenter","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"replan-under-scope-change/reenter/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"feedback","value":"{\"notes\":\"Scope change: add the audit-export use case; re-plan from the activity list against the current committed network.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[accept] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepReplanUnderScopeChangeP2S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "replan-under-scope-change-P2", 2, "deliveryDispatchActivityTask", args)
}

// stepReplanUnderScopeChangeP2S3 — Replan Under Scope Change — [no: scope change] · [accept], step 3: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":3,"node":"present","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"replan-under-scope-change/present/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-revision-staged","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"result\":{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[accept] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepReplanUnderScopeChangeP2S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "replan-under-scope-change-P2", 3, "deliveryDispatchActivityTask", args)
}

// stepReplanUnderScopeChangeP3S1 — Replan Under Scope Change — [no: scope change] · [reject], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"raise-scope-trigger","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"replan-under-scope-change/raise-scope-trigger/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"Phase\":2,\"PhaseName\":\"construction\"}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[reject] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepReplanUnderScopeChangeP3S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "replan-under-scope-change-P3", 1, "deliveryQueryProjectView", args)
}

// stepReplanUnderScopeChangeP3S2 — Replan Under Scope Change — [no: scope change] · [reject], step 2: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":2,"node":"reenter","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"replan-under-scope-change/reenter/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"feedback","value":"{\"notes\":\"Scope change: add the audit-export use case; re-plan from the activity list against the current committed network.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[reject] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepReplanUnderScopeChangeP3S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "replan-under-scope-change-P3", 2, "deliveryDispatchActivityTask", args)
}

// stepReplanUnderScopeChangeP3S3 — Replan Under Scope Change — [no: scope change] · [reject], step 3: DeliveryDispatchActivityTask (tool deliveryDispatchActivityTask)
// Projected stimulus (JSON): {"seq":3,"node":"present","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryDispatchActivityTask","call":"replan-under-scope-change/present/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DeliveryDispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-revision-staged","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"result\":{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards classify=[no: scope change], mgmt=[reject] hold (runStep), then calls deliveryDispatchActivityTask as its caller does and returns its result.
func stepReplanUnderScopeChangeP3S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "replan-under-scope-change-P3", 3, "deliveryDispatchActivityTask", args)
}

// stepRequeueAFailedConstructionActivityP1S1 — Requeue a Failed Construction Activity, step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"review-failure","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryQueryProjectView","call":"requeue-a-failed-construction-activity/review-failure/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"episodes\",\"projectId\":\"dct-owner/dct-mcp\",\"activityId\":\"C-review-engine\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"episodes\"}}","errorExpected":false},"probes":[{"for":"read-failed-trace","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"timeline\",\"projectId\":\"dct-owner/dct-mcp\",\"episodeId\":\"dct-ep-review-engine-0003\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"timeline\",\"timeline\":{\"record\":{\"episodeId\":\"dct-ep-review-engine-0003\",\"outcome\":1}}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards (none) hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepRequeueAFailedConstructionActivityP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "requeue-a-failed-construction-activity-P1", 1, "deliveryQueryProjectView", args)
}

// probeRequeueAFailedConstructionActivityP1S1N1 — Requeue a Failed Construction Activity, step 1, probe 1: deliveryManager.QueryProjectView proves read-failed-trace.
// The binding names the failed attempt's episode symbolically (dct-ep-review-engine-000n,
// the n-th construction attempt): the venue mints the real id, so the probe reads the
// episode the walk recorded against THAT attempt — targetRef C-review-engine:construction:n
// — the way the operator opens the attempt they see failed, never the n-th of a listing.
func probeRequeueAFailedConstructionActivityP1S1N1(t *testing.T, h *scenariohost.Host, _ *toolSession) (any, error) {
	t.Helper()
	s := current(t)
	const symbolic = "dct-ep-review-engine-0003"
	pid, activity := s.project, fixtureActivity
	list, err := s.mgr.QueryProjectView(h.ManagerContext(t), mgr.ProjectViewQuery{Kind: mgr.ProjectViewEpisodes, ProjectID: &pid, ActivityID: &activity})
	if err != nil {
		t.Fatalf("hooks: list the episodes to resolve %s: %v", symbolic, err)
	}
	attempt := fmt.Sprintf("%s:construction:%d", fixtureActivity, 3)
	var seen []string
	for _, e := range list.Episodes {
		if e.TargetRef == attempt {
			id := e.EpisodeID
			return s.mgr.QueryProjectView(h.ManagerContext(t), mgr.ProjectViewQuery{Kind: mgr.ProjectViewTimeline, ProjectID: &pid, EpisodeID: &id})
		}
		seen = append(seen, e.TargetRef)
	}
	return nil, fmt.Errorf("hooks: %s lists no episode for attempt %s (the binding names %s); listed: %v", fixtureActivity, attempt, symbolic, seen)
}

// stepRequeueAFailedConstructionActivityP1S2 — Requeue a Failed Construction Activity, step 2: DeliveryOverrideActivity (tool deliveryOverrideActivity)
// Projected stimulus (JSON): {"seq":2,"node":"write-change","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryOverrideActivity","call":"requeue-a-failed-construction-activity/write-change/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryOverrideActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"},{"name":"override","value":"{\"kind\":2,\"notes\":\"Re-queue: the construction attempt failed because the contract's ReviewSet result changed; regenerate the fake and rerun from the first incomplete phase.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-resumed-state","component":"mcpClient","operation":"DeliveryQueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"result\":{\"activityId\":\"C-review-engine\",\"state\":\"running\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards (none) hold (runStep), then calls deliveryOverrideActivity as its caller does and returns its result.
func stepRequeueAFailedConstructionActivityP1S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "requeue-a-failed-construction-activity-P1", 2, "deliveryOverrideActivity", args)
}

// stepResumePausedConstructionP1S1 — Resume Paused Construction — [yes], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"see-paused","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryQueryProjectView","call":"resume-paused-construction/see-paused/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"operatorPaused\":true}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards pause-settled=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepResumePausedConstructionP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "resume-paused-construction-P1", 1, "deliveryQueryProjectView", args)
}

// stepResumePausedConstructionP1S2 — Resume Paused Construction — [yes], step 2: DeliverySetProjectRunState (tool deliverySetProjectRunState)
// Projected stimulus (JSON): {"seq":2,"node":"choose-resume","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliverySetProjectRunState","call":"resume-paused-construction/choose-resume/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliverySetProjectRunState","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"runState","value":"running"},{"name":"reason","value":"Vendor contract signed; resume construction."}],"expect":{"errorExpected":false},"probes":[{"for":"run-state-running","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"operatorPaused\":false}}}","errorExpected":false}},{"for":"pump-fires","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"pump\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"pump\",\"pump\":{\"open\":true}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards pause-settled=[yes] hold (runStep), then calls deliverySetProjectRunState as its caller does and returns its result.
func stepResumePausedConstructionP1S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "resume-paused-construction-P1", 2, "deliverySetProjectRunState", args)
}

// stepResumePausedConstructionP2S1 — Resume Paused Construction — [no], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"see-paused","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryQueryProjectView","call":"resume-paused-construction/see-paused/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"operatorPaused\":true}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards pause-settled=[no] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepResumePausedConstructionP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "resume-paused-construction-P2", 1, "deliveryQueryProjectView", args)
}

// stepResumePausedConstructionP2S2 — Resume Paused Construction — [no], step 2: DeliverySetProjectRunState (tool deliverySetProjectRunState)
// Projected stimulus (JSON): {"seq":2,"node":"choose-resume","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliverySetProjectRunState","call":"resume-paused-construction/choose-resume/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliverySetProjectRunState","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"runState","value":"running"},{"name":"reason","value":"Vendor contract signed; resume construction."}],"expect":{"errorExpected":true,"errorCode":"FailedPrecondition"},"probes":[{"for":"run-state-still-paused","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"operatorPaused\":true}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards pause-settled=[no] hold (runStep), then calls deliverySetProjectRunState as its caller does and returns its result.
func stepResumePausedConstructionP2S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "resume-paused-construction-P2", 2, "deliverySetProjectRunState", args)
}

// stepSendBackChangeRequestsForARedraftP1S1 — Send Back Change Requests for a Redraft — [yes], step 1: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Projected stimulus (JSON): {"seq":1,"node":"anchor-comments","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliverySubmitReviewDecision","call":"send-back-change-requests-for-a-redraft/anchor-comments/1"},"expectedOutputs":null,"expectedStates":null,"hints":["Each change request is anchored by a jsonPath into the staged artifact plus the quoted anchorText. A per-comment response binds to its request through replyTo = the request's commentId, not through position in the thread."]}
// Binding (JSON): {"seq":1,"operation":"DeliverySubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"decision","value":"{\"decision\":2}"},{"name":"feedback","value":"{\"notes\":\"Change requests before approval.\",\"comments\":[{\"jsonPath\":\"$.containers[3].name\",\"text\":\"Rename to the volatility it encapsulates.\",\"anchorText\":\"BillingThing\",\"replyTo\":\"\"}]}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-draft-sent-back","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5,\"reviewThread\":[{\"anchor\":\"$.containers[3].name\",\"anchorText\":\"BillingThing\",\"status\":\"open\"}]}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards resolved=[yes] hold (runStep), then calls deliverySubmitReviewDecision as its caller does and returns its result.
func stepSendBackChangeRequestsForARedraftP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "send-back-change-requests-for-a-redraft-P1", 1, "deliverySubmitReviewDecision", args)
}

// stepSendBackChangeRequestsForARedraftP1S2 — Send Back Change Requests for a Redraft — [yes], step 2: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":2,"node":"re-review","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"send-back-change-requests-for-a-redraft/re-review/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards resolved=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepSendBackChangeRequestsForARedraftP1S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "send-back-change-requests-for-a-redraft-P1", 2, "deliveryQueryProjectView", args)
}

// stepSendBackChangeRequestsForARedraftP2S1 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 1: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Projected stimulus (JSON): {"seq":1,"node":"anchor-comments","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliverySubmitReviewDecision","call":"send-back-change-requests-for-a-redraft/anchor-comments/1"},"expectedOutputs":null,"expectedStates":null,"hints":["Each change request is anchored by a jsonPath into the staged artifact plus the quoted anchorText. A per-comment response binds to its request through replyTo = the request's commentId, not through position in the thread."]}
// Binding (JSON): {"seq":1,"operation":"DeliverySubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"decision","value":"{\"decision\":2}"},{"name":"feedback","value":"{\"notes\":\"Change requests before approval.\",\"comments\":[{\"jsonPath\":\"$.containers[3].name\",\"text\":\"Rename to the volatility it encapsulates.\",\"anchorText\":\"BillingThing\",\"replyTo\":\"\"}]}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-draft-sent-back","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5,\"reviewThread\":[{\"anchor\":\"$.containers[3].name\",\"anchorText\":\"BillingThing\",\"status\":\"open\"}]}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards resolved=[no: iterate], resolved=[yes] hold (runStep), then calls deliverySubmitReviewDecision as its caller does and returns its result.
func stepSendBackChangeRequestsForARedraftP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 1, "deliverySubmitReviewDecision", args)
}

// stepSendBackChangeRequestsForARedraftP2S2 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 2: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":2,"node":"re-review","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"send-back-change-requests-for-a-redraft/re-review/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards resolved=[no: iterate], resolved=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepSendBackChangeRequestsForARedraftP2S2(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 2, "deliveryQueryProjectView", args)
}

// stepSendBackChangeRequestsForARedraftP2S3 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 3: DeliverySubmitReviewDecision (tool deliverySubmitReviewDecision)
// Projected stimulus (JSON): {"seq":3,"node":"anchor-comments","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliverySubmitReviewDecision","call":"send-back-change-requests-for-a-redraft/anchor-comments/1"},"expectedOutputs":null,"expectedStates":null,"hints":["Each change request is anchored by a jsonPath into the staged artifact plus the quoted anchorText. A per-comment response binds to its request through replyTo = the request's commentId, not through position in the thread."]}
// Binding (JSON): {"seq":3,"operation":"DeliverySubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-mcp"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"decision","value":"{\"decision\":2}"},{"name":"feedback","value":"{\"notes\":\"Second round: one request is not resolved.\",\"comments\":[{\"jsonPath\":\"$.containers[3].responsibilities\",\"text\":\"The response renamed it but still lists invoicing; move invoicing out.\",\"anchorText\":\"invoicing\",\"replyTo\":\"\"}]}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-draft-sent-back","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5,\"reviewThread\":[{\"anchor\":\"$.containers[3].name\"},{\"anchor\":\"$.containers[3].responsibilities\",\"anchorText\":\"invoicing\",\"status\":\"open\"}]}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards resolved=[no: iterate], resolved=[yes] hold (runStep), then calls deliverySubmitReviewDecision as its caller does and returns its result.
func stepSendBackChangeRequestsForARedraftP2S3(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 3, "deliverySubmitReviewDecision", args)
}

// stepSendBackChangeRequestsForARedraftP2S4 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 4: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":4,"node":"re-review","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"send-back-change-requests-for-a-redraft/re-review/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}"}],"expect":{"result":"{\"result\":{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-mcp\",\"artifactKind\":5}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards resolved=[no: iterate], resolved=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepSendBackChangeRequestsForARedraftP2S4(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 4, "deliveryQueryProjectView", args)
}

// stepTrackWeeklyProjectProgressP1S1 — Track Weekly Project Progress — [no] · [yes], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"week-elapses","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"track-weekly-project-progress/week-elapses/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"PhaseName\":\"construction\",\"constructionStarted\":true}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards enough-points=[no], off-track=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepTrackWeeklyProjectProgressP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "track-weekly-project-progress-P1", 1, "deliveryQueryProjectView", args)
}

// stepTrackWeeklyProjectProgressP2S1 — Track Weekly Project Progress — [no] · [no], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"week-elapses","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"track-weekly-project-progress/week-elapses/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"PhaseName\":\"construction\",\"constructionStarted\":true}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards enough-points=[no], off-track=[no] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepTrackWeeklyProjectProgressP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "track-weekly-project-progress-P2", 1, "deliveryQueryProjectView", args)
}

// stepTrackWeeklyProjectProgressP3S1 — Track Weekly Project Progress — [yes] · [yes], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"week-elapses","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"track-weekly-project-progress/week-elapses/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"PhaseName\":\"construction\",\"constructionStarted\":true}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards enough-points=[yes], off-track=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepTrackWeeklyProjectProgressP3S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "track-weekly-project-progress-P3", 1, "deliveryQueryProjectView", args)
}

// stepTrackWeeklyProjectProgressP4S1 — Track Weekly Project Progress — [yes] · [no], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"week-elapses","input":{"kind":"action","from":"architect-user","to":"mcpclient","op":"DeliveryQueryProjectView","call":"track-weekly-project-progress/week-elapses/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"PhaseName\":\"construction\",\"constructionStarted\":true}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
// Arranges the state in which guards enough-points=[yes], off-track=[no] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepTrackWeeklyProjectProgressP4S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "track-weekly-project-progress-P4", 1, "deliveryQueryProjectView", args)
}

// stepViewTheProjectStateLogP1S1 — View the Project State Log — [yes], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"open-history","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryQueryProjectView","call":"view-the-project-state-log/open-history/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\"}}}","errorExpected":false},"probes":[{"for":"project-state-unchanged","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"Version\":7}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards inspect=[yes] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepViewTheProjectStateLogP1S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "view-the-project-state-log-P1", 1, "deliveryQueryProjectView", args)
}

// stepViewTheProjectStateLogP2S1 — View the Project State Log — [no], step 1: DeliveryQueryProjectView (tool deliveryQueryProjectView)
// Projected stimulus (JSON): {"seq":1,"node":"open-history","input":{"kind":"action","from":"operator","to":"mcpclient","op":"DeliveryQueryProjectView","call":"view-the-project-state-log/open-history/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\"}}}","errorExpected":false},"probes":[{"for":"project-state-unchanged","component":"mcpClient","operation":"DeliveryQueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-mcp\"}"}],"expect":{"result":"{\"result\":{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-mcp\",\"Version\":7}}}","errorExpected":false}}],"unobservable":null,"hook":true}
// Arranges the state in which guards inspect=[no] hold (runStep), then calls deliveryQueryProjectView as its caller does and returns its result.
func stepViewTheProjectStateLogP2S1(t *testing.T, h *scenariohost.Host, subject *toolSession, args string) (any, error) {
	t.Helper()
	return runStep(t, h, subject, "view-the-project-state-log-P2", 1, "deliveryQueryProjectView", args)
}
