// Hooks for agenticJobAccess scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file.
//
// THE SUBJECT is the real agenticJobAccess built by its local-executor variant
// constructor (agenticjob.NewLocalExecAgenticJobAccess) over a per-scenario
// working clone of the host's git repo (scenariohost.Host.Repo). Every git
// operation the component performs — worktree add on the activity / design
// session branch, the before/after ref check behind PhaseSucceeded, worktree
// removal — runs for real against that clone.
//
// Why not the generated GitHub-Actions constructor: it takes a
// *framework-go-infrastructure-github.AppClient, a package the hooks file may
// not import (it is neither a component package nor a test double). The
// local-executor constructor takes only stdlib values, and both variants
// implement the same two-verb contract.
//
// THE COLLABORATORS the probes read are real too (DCT §12 B2): the
// projectStateAccess and designSessionAccess components, built through their
// public GitLocal variant constructors over the same clone, read back what the
// run left on main and on the session branch.
//
// THE ONE DOUBLE is the resource this component fronts: the agent runtime. A
// `claude` executable written to a per-scenario temp dir, first on PATH,
// stands in for the Claude Code CLI. It does what an agent run does — commit
// onto the branch the component started it on — and nothing the component
// itself is responsible for. A scenario scripts the run where its guards need
// a particular outcome: agentDouble.failNextRun (the run exits non-zero and
// commits nothing) or agentDouble.writeNextRun (the run's commit writes the
// given state document, as the agent's state tools do). Where that commit
// lands is the component's work, and the probes check it. The rig binary
// (aiarch-state-mcp, which the component runs for `seat-assets` before the
// agent starts) is a no-op double for the same reason: it belongs to the
// agent's runtime, not to this component.
//
// HANDLES: the bindings name handles symbolically ("<scenario>-handle-N" = the
// handle step N's SubmitAgenticJob returned). The subject is wrapped in a
// recorder that passes every call through unchanged and keeps each step's
// returned handle, so an Observe step addresses the real run the way its
// caller (deliveryManager) does: with the handle it was given.
//
// Observe steps poll until the run is terminal, as deliveryManager's observe
// loop does; the binding's expectation is on the terminal observation. A probe,
// and every arrangement, first waits until each run the scenario started has
// finished, so it reads settled state rather than racing the agent.

package agenticjob_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// runTimeout bounds one agent run inside the component (the double finishes in
// milliseconds; this only guards a wedged subprocess).
const runTimeout = 2 * time.Minute

// observeBudget bounds how long an Observe step, or a settle, polls for a
// terminal phase.
const observeBudget = time.Minute

// stateDocPath is where a project repo keeps its state document.
const stateDocPath = ".aiarch/state/project.json"

// Branches the bindings name.
const (
	systemSessionBranch  = "aiarch/session/architecture-system"
	missionSessionBranch = "aiarch/session/requirements-mission"
	reviewEngineBranch   = "activity/C-review-engine"
)

// newSubject builds the component under test: the local-executor
// agenticJobAccess over a working clone of the host's repo, with the agent
// double first on PATH, plus the real state readers the probes use.
func newSubject(t *testing.T, h *scenariohost.Host) agenticjob.AgenticJobAccess {
	t.Helper()
	root := t.TempDir()
	stateMCP := writeExecutable(t, filepath.Join(root, "bin", "aiarch-state-mcp"),
		"#!/bin/sh\n# rig double: seat-assets renders the .claude prompt surface; the agent double reads none.\nexit 0\n")
	return buildSubject(t, h, root, stateMCP)
}

// buildSubject builds the subject in root with stateMCP as the rig binary it runs.
func buildSubject(t *testing.T, h *scenariohost.Host, root, stateMCP string) agenticjob.AgenticJobAccess {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("hooks: git not on PATH: %v", err)
	}
	// The component refuses a bare shared repo (its episode trace sink would sit
	// inside the agent-writable git dir), so it runs over a working clone of the
	// host's bare repo, exactly as a local-profile server is pointed at one.
	clone := filepath.Join(root, "repo")
	runGit(t, root, "clone", "--quiet", h.Repo.URL, clone)

	agent := newAgentDouble(t, root, gitBin)

	t.Setenv("PATH", agent.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Never let a scenario reach the real Claude Code CLI.
	if got, lookErr := exec.LookPath("claude"); lookErr != nil || got != agent.claude {
		t.Fatalf("hooks: claude resolves to %q (%v), want the agent double %q", got, lookErr, agent.claude)
	}

	repoURL := "file://" + clone
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repoURL, "stp-agentic-job-access", stateMCP, runTimeout)
	if err != nil {
		t.Fatalf("hooks: NewLocalExecAgenticJobAccess: %v", err)
	}
	rec := record(callContext, access)
	rec.env = &scenarioEnv{
		agent:    agent,
		clone:    clone,
		state:    projectstate.NewGitLocalProjectStateAccess(repoURL),
		sessions: projectstate.NewGitLocalDesignSessionAccess(repoURL),
	}
	// Registered after t.TempDir, so it runs first: every run the scenario
	// started finishes (and removes its worktree) before the clone is deleted.
	t.Cleanup(func() {
		if err := rec.settle(t, h); err != nil {
			t.Error(err)
		}
	})
	return rec
}

// scenarioEnv is what one scenario's hooks share beside the subject: the agent
// double, the clone the component runs over, and the real state readers.
type scenarioEnv struct {
	agent    *agentDouble
	clone    string
	state    projectstate.ProjectStateAccess
	sessions projectstate.DesignSessionAccess
}

// ---------------------------------------------------------------------------
// Step and probe bodies
// ---------------------------------------------------------------------------

// submitStep calls SubmitAgenticJob exactly as deliveryManager does, with the
// step's own idempotency key.
func submitStep(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, seq int, spec agenticjob.PipelineSpec) (any, error) {
	t.Helper()
	return subject.SubmitAgenticJob(callContext(t, h, strconv.Itoa(seq)), spec)
}

// failingSubmitStep submits a run the agent fails (the guard "the job did not
// succeed").
func failingSubmitStep(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, seq int, spec agenticjob.PipelineSpec) (any, error) {
	t.Helper()
	settled(t, h, subject).agent.failNextRun(t)
	return submitStep(t, h, subject, seq, spec)
}

// observeStep resolves the binding's symbolic handle to the one its submit step
// returned, then polls ObserveAgenticJob until the run is terminal and returns
// that observation.
func observeStep(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, seq int, symbolic agenticjob.PipelineHandle) (any, error) {
	t.Helper()
	rec := recorderOf(t, subject)
	rec.markStep(t, seq)
	handle := rec.resolve(t, symbolic)
	rc := callContext(t, h, strconv.Itoa(seq))
	deadline := time.Now().Add(observeBudget)
	for {
		obs, err := subject.ObserveAgenticJob(rc, handle)
		if err != nil || agenticjob.PipelinePhaseIsTerminal(obs.Phase) {
			return obs, err
		}
		if time.Now().After(deadline) {
			return obs, fmt.Errorf("hooks: run %s still %s after %s", handle, agenticjob.PipelinePhaseString(obs.Phase), observeBudget)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// staleSystemRedraftStep is the reconcile step of "add a use case": the use
// cases were recommitted on main, which left the downstream System committed
// but stale; the agent redrafts the System on its session branch.
func staleSystemRedraftStep(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, spec agenticjob.PipelineSpec) (any, error) {
	t.Helper()
	env := settled(t, h, subject)
	id := projectIDOf(spec)
	onMain := bornProject(id)
	onMain.CoreUseCases = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 2}
	onMain.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 1, StaleBasis: true}
	commitStateDoc(t, env.clone, "main", onMain)

	redraft := onMain
	redraft.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewAwaitingReview, Revisions: 1}
	env.agent.writeNextRun(t, encodeStateDoc(t, redraft))
	return submitStep(t, h, subject, 2, spec)
}

// systemAmendmentStep is the back-to-design step of "replan under scope
// change": the System is committed on main, and the agent drafts its amendment
// on the System session branch.
func systemAmendmentStep(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, spec agenticjob.PipelineSpec) (any, error) {
	t.Helper()
	env := settled(t, h, subject)
	id := projectIDOf(spec)
	onMain := bornProject(id)
	onMain.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 1}
	commitStateDoc(t, env.clone, "main", onMain)

	draft := onMain
	draft.SystemDesign = projectstate.ArtifactSlot{Status: projectstate.ReviewAwaitingReview, Revisions: 1}
	env.agent.writeNextRun(t, encodeStateDoc(t, draft))
	return submitStep(t, h, subject, 1, spec)
}

// missionRedraftStep is the redraft step of "send back change requests": the
// Mission draft sits on its session branch with the reviewer's change request
// open (round 1 is the first send-back; a later round re-opens it, the
// reviewer's "not resolved: iterate"), and the agent redrafts, answering it.
func missionRedraftStep(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, seq int, round int64, spec agenticjob.PipelineSpec) (any, error) {
	t.Helper()
	env := settled(t, h, subject)
	id := projectIDOf(spec)
	if round == 1 {
		commitStateDoc(t, env.clone, "main", bornProject(id))
	}
	sentBack := bornProject(id)
	sentBack.Mission = missionDraft(changeRequest(round, projectstate.ReviewCommentOpen))
	commitStateDoc(t, env.clone, missionSessionBranch, sentBack)

	answered := sentBack
	answered.Mission = missionDraft(changeRequest(round, projectstate.ReviewCommentAnswered))
	env.agent.writeNextRun(t, encodeStateDoc(t, answered))
	return submitStep(t, h, subject, seq, spec)
}

// stagedResumeStep is the resume step of "re-queue a failed construction
// activity": a prior attempt's staged work is on the activity branch, and the
// re-queued run resumes from it.
func stagedResumeStep(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, spec agenticjob.PipelineSpec) (any, error) {
	t.Helper()
	env := settled(t, h, subject)
	runGit(t, env.clone, "branch", reviewEngineBranch, "main")
	wt := filepath.Join(t.TempDir(), "prior-attempt")
	runGit(t, env.clone, "worktree", "add", "--quiet", wt, reviewEngineBranch)
	runGit(t, wt, "-c", "user.name=prior-attempt", "-c", "user.email=prior-attempt@example.invalid",
		"-c", "commit.gpgsign=false", "commit", "--allow-empty", "--no-verify", "--quiet", "-m", "prior attempt: staged work")
	runGit(t, env.clone, "worktree", "remove", "--force", wt)
	return submitStep(t, h, subject, 1, spec)
}

// probeMain calls projectStateAccess.ReadProject (main) on the settled clone.
// The result is returned in the project envelope projectstate.EncodeProject
// puts every Project in at a component boundary — {id, slots{<kind>: …}} —
// the shape the bindings state their expectations in.
func probeMain(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, projectID string) (any, error) {
	t.Helper()
	env := settled(t, h, subject)
	p, err := env.state.ReadProject(callContext(t, h, "probe"), projectstate.ProjectID(projectID))
	if err != nil {
		return nil, err
	}
	return projectstate.EncodeProject(p)
}

// probeBranch calls designSessionAccess.ReadProjectOnBranch on the settled
// clone.
func probeBranch(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, projectID, branch string) (any, error) {
	t.Helper()
	env := settled(t, h, subject)
	return env.sessions.ReadProjectOnBranch(callContext(t, h, "probe"), projectstate.ProjectID(projectID), branch)
}

// ---------------------------------------------------------------------------
// State documents
// ---------------------------------------------------------------------------

func projectIDOf(spec agenticjob.PipelineSpec) projectstate.ProjectID {
	return projectstate.ProjectID(string(spec.ProjectID))
}

// bornProject is a project as CreateProject leaves it.
func bornProject(id projectstate.ProjectID) projectstate.Project {
	return projectstate.Project{
		ID:             id,
		Version:        1,
		Owner:          "stp-org",
		Name:           string(id),
		OperatingModel: projectstate.OperatingModelSelfOperated,
	}
}

func missionDraft(thread ...projectstate.ReviewComment) projectstate.ArtifactSlot {
	return projectstate.ArtifactSlot{Status: projectstate.ReviewAwaitingReview, Revisions: 1, ReviewThread: thread}
}

// changeRequest is the reviewer's one change request on the Mission draft, in
// the given round and status.
func changeRequest(round int64, status string) projectstate.ReviewComment {
	c := projectstate.ReviewComment{
		ID:         "cr-1",
		Anchor:     "mission",
		Text:       "Name the customer the mission serves.",
		AuthorRole: "architect",
		Round:      round,
		Status:     status,
		Reopened:   round > 1,
		Type:       projectstate.ReviewCommentTypeChangeRequest,
		Addressee:  "agent",
	}
	if status == projectstate.ReviewCommentAnswered {
		c.Replies = []projectstate.ReviewCommentReply{{ID: "cr-1-r" + strconv.FormatInt(round, 10), AuthorRole: "agent", Text: "Named the customer in the mission statement."}}
	}
	return c
}

func encodeStateDoc(t *testing.T, p projectstate.Project) []byte {
	t.Helper()
	doc, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatalf("hooks: encode state document: %v", err)
	}
	return doc
}

// commitStateDoc commits p as the state document on branch of the clone
// (creating the branch off main when it does not exist yet).
func commitStateDoc(t *testing.T, clone, branch string, p projectstate.Project) {
	t.Helper()
	dir := clone
	if branch != "main" {
		if !branchExists(clone, branch) {
			runGit(t, clone, "branch", branch, "main")
		}
		dir = filepath.Join(t.TempDir(), "arrange")
		runGit(t, clone, "worktree", "add", "--quiet", dir, branch)
		defer runGit(t, clone, "worktree", "remove", "--force", dir)
	}
	path := filepath.Join(dir, stateDocPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	if err := os.WriteFile(path, encodeStateDoc(t, p), 0o600); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	runGit(t, dir, "add", stateDocPath)
	runGit(t, dir, "-c", "user.name=arrange", "-c", "user.email=arrange@example.invalid",
		"-c", "commit.gpgsign=false", "commit", "--no-verify", "--quiet", "-m", "arrange: state on "+branch)
}

func branchExists(clone, branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	cmd.Dir = clone
	return cmd.Run() == nil
}

// ---------------------------------------------------------------------------
// The agent double
// ---------------------------------------------------------------------------

// agentDouble is the stand-in for the Claude Code CLI the component spawns.
type agentDouble struct {
	binDir    string // first on PATH
	claude    string // the executable
	failFlag  string // present ⇒ the next run fails
	nextState string // present ⇒ the next run commits it as the state document
	argv      string // the last run's argv, one argument per line
	settings  string // a copy of the last run's --settings file
	switchTo  string // present ⇒ after its commit, the next run switches to a scratch branch and commits there
}

func newAgentDouble(t *testing.T, root, gitBin string) *agentDouble {
	t.Helper()
	ctl := filepath.Join(root, "agent-ctl")
	if err := os.MkdirAll(ctl, 0o750); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	a := &agentDouble{
		binDir:    filepath.Join(root, "agent-bin"),
		failFlag:  filepath.Join(ctl, "fail-next"),
		nextState: filepath.Join(ctl, "next-state.json"),
		argv:      filepath.Join(ctl, "argv"),
		settings:  filepath.Join(ctl, "settings.json"),
		switchTo:  filepath.Join(ctl, "switch-next"),
	}
	a.claude = writeExecutable(t, filepath.Join(a.binDir, "claude"), fmt.Sprintf(`#!/bin/sh
# Agent double for the Claude Code CLI. Started by agenticJobAccess in the run's
# worktree with the AIARCH_* rig in its environment. It keeps the argv it was
# started with and the settings file beside the worktree, for the probes.
printf '%%s\n' "$@" > %[6]q
cp ../aiarch-settings.json %[7]q 2>/dev/null || :
if [ -f %[1]q ]; then
  rm -f %[1]q
  echo "agent double: run failed (arranged by the scenario)" >&2
  exit 1
fi
if [ -f %[2]q ]; then
  mkdir -p %[4]q || exit 1
  mv %[2]q %[5]q || exit 1
  %[3]q add %[5]q || exit 1
fi
%[3]q -c user.name=agent-double -c user.email=agent-double@example.invalid -c commit.gpgsign=false \
  commit --allow-empty --no-verify --quiet -m "agent double: $AIARCH_COMMAND on $AIARCH_TARGET_BRANCH" || exit 1
if [ -f %[8]q ]; then
  rm -f %[8]q
  %[3]q switch --quiet -c scratch || exit 1
  echo stray > stray.txt
  %[3]q add stray.txt || exit 1
  %[3]q -c user.name=agent-double -c user.email=agent-double@example.invalid -c commit.gpgsign=false \
    commit --no-verify --quiet -m "agent double: left on scratch" || exit 1
fi
`, a.failFlag, a.nextState, gitBin, filepath.Dir(stateDocPath), stateDocPath, a.argv, a.settings, a.switchTo))
	return a
}

// failNextRun arranges that the next agent run exits non-zero without
// committing.
func (a *agentDouble) failNextRun(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(a.failFlag, nil, 0o600); err != nil {
		t.Fatalf("hooks: arrange failing run: %v", err)
	}
}

// writeNextRun arranges that the next agent run commits doc as the state
// document of the worktree it runs in.
func (a *agentDouble) writeNextRun(t *testing.T, doc []byte) {
	t.Helper()
	if err := os.WriteFile(a.nextState, doc, 0o600); err != nil {
		t.Fatalf("hooks: arrange agent output: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The recorder
// ---------------------------------------------------------------------------

// contractCaller is the AgenticJobAccess contract with the call-context type
// left as a parameter: the hooks file may not import the package that declares
// it, so record infers it from callContext.
type contractCaller[C any] interface {
	SubmitAgenticJob(rc C, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error)
	ObserveAgenticJob(rc C, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error)
}

// recorder passes every call through to the real component unchanged and
// remembers which handle each step's submit returned. Steps are counted in
// order: every SubmitAgenticJob is one step, and every Observe step announces
// itself with markStep (it polls, so its calls are not counted).
type recorder[C any] struct {
	inner contractCaller[C]
	ctx   func(*testing.T, *scenariohost.Host, string) C
	env   *scenarioEnv

	mu      sync.Mutex
	steps   int
	handles map[int]agenticjob.PipelineHandle
}

func record[C any](ctx func(*testing.T, *scenariohost.Host, string) C, inner contractCaller[C]) *recorder[C] {
	return &recorder[C]{inner: inner, ctx: ctx, handles: map[int]agenticjob.PipelineHandle{}}
}

func (r *recorder[C]) SubmitAgenticJob(rc C, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
	handle, err := r.inner.SubmitAgenticJob(rc, spec)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps++
	if err == nil {
		r.handles[r.steps] = handle
	}
	return handle, err
}

func (r *recorder[C]) ObserveAgenticJob(rc C, handle agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
	return r.inner.ObserveAgenticJob(rc, handle)
}

// stepRecorder is the recorder's non-generic face, reachable from a hook that
// holds the subject as the contract interface.
type stepRecorder interface {
	markStep(t *testing.T, seq int)
	resolve(t *testing.T, symbolic agenticjob.PipelineHandle) agenticjob.PipelineHandle
	scenario() *scenarioEnv
	settle(t *testing.T, h *scenariohost.Host) error
}

func recorderOf(t *testing.T, subject agenticjob.AgenticJobAccess) stepRecorder {
	t.Helper()
	rec, ok := subject.(stepRecorder)
	if !ok {
		t.Fatal("hooks: subject was not built by newSubject")
	}
	return rec
}

// settled waits until every run the scenario started has finished, then
// returns the scenario's environment.
func settled(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) *scenarioEnv {
	t.Helper()
	rec := recorderOf(t, subject)
	if err := rec.settle(t, h); err != nil {
		t.Fatal(err)
	}
	return rec.scenario()
}

func (r *recorder[C]) markStep(t *testing.T, seq int) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps++
	if r.steps != seq {
		t.Fatalf("hooks: observe step %d ran as the scenario's call #%d", seq, r.steps)
	}
}

// resolve maps "<scenario>-handle-N" to the handle step N's submit returned.
func (r *recorder[C]) resolve(t *testing.T, symbolic agenticjob.PipelineHandle) agenticjob.PipelineHandle {
	t.Helper()
	s := string(symbolic)
	i := strings.LastIndex(s, "-handle-")
	if i < 0 {
		t.Fatalf("hooks: binding handle %q is not a step reference (<scenario>-handle-N)", s)
	}
	n, err := strconv.Atoi(s[i+len("-handle-"):])
	if err != nil {
		t.Fatalf("hooks: binding handle %q: %v", s, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	handle, ok := r.handles[n]
	if !ok {
		t.Fatalf("hooks: binding handle %q names step %d, which returned no handle", s, n)
	}
	return handle
}

func (r *recorder[C]) scenario() *scenarioEnv { return r.env }

// settle waits for every run the scenario started to reach a terminal phase.
func (r *recorder[C]) settle(t *testing.T, h *scenariohost.Host) error {
	t.Helper()
	r.mu.Lock()
	handles := make([]agenticjob.PipelineHandle, 0, len(r.handles))
	for _, hd := range r.handles {
		handles = append(handles, hd)
	}
	r.mu.Unlock()
	rc := r.ctx(t, h, "settle")
	for _, hd := range handles {
		deadline := time.Now().Add(observeBudget)
		for {
			obs, err := r.inner.ObserveAgenticJob(rc, hd)
			if err != nil || agenticjob.PipelinePhaseIsTerminal(obs.Phase) {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("hooks: run %s did not finish within %s", hd, observeBudget)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func writeExecutable(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	return path
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hooks: git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

// stepAddAUseCaseToAnInFlightProjectP1S2 — Add a Use Case to an In-Flight Project — [variation] · [no], step 2: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"reconcile","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"add-a-use-case-to-an-in-flight-project/reconcile/5"},"expectedOutputs":null,"expectedStates":[{"node":"stale-artifacts-reconciled","object":"Stale downstream artifacts","inState":"[amended or acknowledged unaffected]"}],"hints":["The acknowledgement is acknowledgeStaleBasis with the literal 'reviewed — unaffected' (em dash, as in the dynamic view). The diagram label uses a hyphen; bind to the em dash. The acknowledgement is audited, so it is observable state."]}
// Binding (JSON): {"seq":2,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-auc-P1\",\"ActivityID\":\"architecture\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/system-draft\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-architecture\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/architecture-system\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-auc-P1-2\",\"artifact_kind\":\"System\",\"command\":\"system-draft\",\"target_branch\":\"aiarch/session/architecture-system\",\"prior_state_ref\":\"main\",\"job_mode\":\"draft\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-auc-P1\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aja-auc-P1","schemaRef":"#/$defs/ProjectID"}],"expect":{"result":"{\"id\":\"stp-aja-auc-P1\",\"slots\":{\"5\":{\"status\":2}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arrangement: the agent redrafts the stale System on its session branch.
func stepAddAUseCaseToAnInFlightProjectP1S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAddAUseCaseToAnInFlightProjectP1S2) (any, error) {
	t.Helper()
	return staleSystemRedraftStep(t, h, subject, in.Spec)
}

// probeAddAUseCaseToAnInFlightProjectP1S2N1 — Add a Use Case to an In-Flight Project — [variation] · [no], step 2, probe 1: projectStateAccess.ReadProject proves stale-artifacts-reconciled.
// Probe (JSON): {"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aja-auc-P1","schemaRef":"#/$defs/ProjectID"}],"expect":{"result":"{\"id\":\"stp-aja-auc-P1\",\"slots\":{\"5\":{\"status\":2}}}","errorExpected":false}}
// Arrangement: read main back through the real projectStateAccess.
func probeAddAUseCaseToAnInFlightProjectP1S2N1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) (any, error) {
	t.Helper()
	return probeMain(t, h, subject, "stp-aja-auc-P1")
}

// stepAddAUseCaseToAnInFlightProjectP2S2 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 2: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"reconcile","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"add-a-use-case-to-an-in-flight-project/reconcile/5"},"expectedOutputs":null,"expectedStates":[{"node":"stale-artifacts-reconciled","object":"Stale downstream artifacts","inState":"[amended or acknowledged unaffected]"}],"hints":["The acknowledgement is acknowledgeStaleBasis with the literal 'reviewed — unaffected' (em dash, as in the dynamic view). The diagram label uses a hyphen; bind to the em dash. The acknowledgement is audited, so it is observable state."]}
// Binding (JSON): {"seq":2,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-auc-P2\",\"ActivityID\":\"architecture\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/system-draft\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-architecture\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/architecture-system\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-auc-P2-2\",\"artifact_kind\":\"System\",\"command\":\"system-draft\",\"target_branch\":\"aiarch/session/architecture-system\",\"prior_state_ref\":\"main\",\"job_mode\":\"draft\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-auc-P2\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aja-auc-P2","schemaRef":"#/$defs/ProjectID"}],"expect":{"result":"{\"id\":\"stp-aja-auc-P2\",\"slots\":{\"5\":{\"status\":2}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arrangement: the agent redrafts the stale System on its session branch.
func stepAddAUseCaseToAnInFlightProjectP2S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAddAUseCaseToAnInFlightProjectP2S2) (any, error) {
	t.Helper()
	return staleSystemRedraftStep(t, h, subject, in.Spec)
}

// probeAddAUseCaseToAnInFlightProjectP2S2N1 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 2, probe 1: projectStateAccess.ReadProject proves stale-artifacts-reconciled.
// Probe (JSON): {"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aja-auc-P2","schemaRef":"#/$defs/ProjectID"}],"expect":{"result":"{\"id\":\"stp-aja-auc-P2\",\"slots\":{\"5\":{\"status\":2}}}","errorExpected":false}}
// Arrangement: read main back through the real projectStateAccess.
func probeAddAUseCaseToAnInFlightProjectP2S2N1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) (any, error) {
	t.Helper()
	return probeMain(t, h, subject, "stp-aja-auc-P2")
}

// stepAddAUseCaseToAnInFlightProjectP3S2 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 2: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"reconcile","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"add-a-use-case-to-an-in-flight-project/reconcile/5"},"expectedOutputs":null,"expectedStates":[{"node":"stale-artifacts-reconciled","object":"Stale downstream artifacts","inState":"[amended or acknowledged unaffected]"}],"hints":["The acknowledgement is acknowledgeStaleBasis with the literal 'reviewed — unaffected' (em dash, as in the dynamic view). The diagram label uses a hyphen; bind to the em dash. The acknowledgement is audited, so it is observable state."]}
// Binding (JSON): {"seq":2,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-auc-P3\",\"ActivityID\":\"architecture\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/system-draft\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-architecture\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/architecture-system\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-auc-P3-2\",\"artifact_kind\":\"System\",\"command\":\"system-draft\",\"target_branch\":\"aiarch/session/architecture-system\",\"prior_state_ref\":\"main\",\"job_mode\":\"draft\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-auc-P3\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aja-auc-P3","schemaRef":"#/$defs/ProjectID"}],"expect":{"result":"{\"id\":\"stp-aja-auc-P3\",\"slots\":{\"5\":{\"status\":2}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arrangement: the agent redrafts the stale System on its session branch.
func stepAddAUseCaseToAnInFlightProjectP3S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAddAUseCaseToAnInFlightProjectP3S2) (any, error) {
	t.Helper()
	return staleSystemRedraftStep(t, h, subject, in.Spec)
}

// probeAddAUseCaseToAnInFlightProjectP3S2N1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 2, probe 1: projectStateAccess.ReadProject proves stale-artifacts-reconciled.
// Probe (JSON): {"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aja-auc-P3","schemaRef":"#/$defs/ProjectID"}],"expect":{"result":"{\"id\":\"stp-aja-auc-P3\",\"slots\":{\"5\":{\"status\":2}}}","errorExpected":false}}
// Arrangement: read main back through the real projectStateAccess.
func probeAddAUseCaseToAnInFlightProjectP3S2N1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) (any, error) {
	t.Helper()
	return probeMain(t, h, subject, "stp-aja-auc-P3")
}

// stepAddAUseCaseToAnInFlightProjectP4S2 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 2: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"reconcile","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"add-a-use-case-to-an-in-flight-project/reconcile/5"},"expectedOutputs":null,"expectedStates":[{"node":"stale-artifacts-reconciled","object":"Stale downstream artifacts","inState":"[amended or acknowledged unaffected]"}],"hints":["The acknowledgement is acknowledgeStaleBasis with the literal 'reviewed — unaffected' (em dash, as in the dynamic view). The diagram label uses a hyphen; bind to the em dash. The acknowledgement is audited, so it is observable state."]}
// Binding (JSON): {"seq":2,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-auc-P4\",\"ActivityID\":\"architecture\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/system-draft\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-architecture\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/architecture-system\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-auc-P4-2\",\"artifact_kind\":\"System\",\"command\":\"system-draft\",\"target_branch\":\"aiarch/session/architecture-system\",\"prior_state_ref\":\"main\",\"job_mode\":\"draft\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-auc-P4\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aja-auc-P4","schemaRef":"#/$defs/ProjectID"}],"expect":{"result":"{\"id\":\"stp-aja-auc-P4\",\"slots\":{\"5\":{\"status\":2}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arrangement: the agent redrafts the stale System on its session branch.
func stepAddAUseCaseToAnInFlightProjectP4S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAddAUseCaseToAnInFlightProjectP4S2) (any, error) {
	t.Helper()
	return staleSystemRedraftStep(t, h, subject, in.Spec)
}

// probeAddAUseCaseToAnInFlightProjectP4S2N1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 2, probe 1: projectStateAccess.ReadProject proves stale-artifacts-reconciled.
// Probe (JSON): {"for":"stale-artifacts-reconciled","component":"projectStateAccess","operation":"ReadProject","inputs":[{"name":"projectID","value":"stp-aja-auc-P4","schemaRef":"#/$defs/ProjectID"}],"expect":{"result":"{\"id\":\"stp-aja-auc-P4\",\"slots\":{\"5\":{\"status\":2}}}","errorExpected":false}}
// Arrangement: read main back through the real projectStateAccess.
func probeAddAUseCaseToAnInFlightProjectP4S2N1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) (any, error) {
	t.Helper()
	return probeMain(t, h, subject, "stp-aja-auc-P4")
}

// stepAskAClarifyingQuestionDuringReviewP1S2 — Ask a Clarifying Question During Review — [yes: session branch] · [yes], step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"dispatch-answer-job","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"ask-a-clarifying-question-during-review/dispatch-answer-job/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-acq-P1-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepAskAClarifyingQuestionDuringReviewP1S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAskAClarifyingQuestionDuringReviewP1S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepAskAClarifyingQuestionDuringReviewP2S1 — Ask a Clarifying Question During Review — [yes: session branch] · [no: dispatch miss], step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-answer-job","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"ask-a-clarifying-question-during-review/dispatch-answer-job/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-acq-P2\",\"ActivityID\":\"architecture\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/design-answer\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-architecture\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/architecture-system\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-acq-P2-1\",\"artifact_kind\":\"System\",\"command\":\"design-answer\",\"target_branch\":\"aiarch/session/architecture-system\",\"prior_state_ref\":\"aiarch/session/architecture-system\",\"job_mode\":\"answer\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-acq-P2\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: the agent run fails.
func stepAskAClarifyingQuestionDuringReviewP2S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAskAClarifyingQuestionDuringReviewP2S1) (any, error) {
	t.Helper()
	return failingSubmitStep(t, h, subject, 1, in.Spec)
}

// stepAskAClarifyingQuestionDuringReviewP2S2 — Ask a Clarifying Question During Review — [yes: session branch] · [no: dispatch miss], step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"dispatch-answer-job","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"ask-a-clarifying-question-during-review/dispatch-answer-job/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-acq-P2-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":3,\"FailedStep\":\"agent\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepAskAClarifyingQuestionDuringReviewP2S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAskAClarifyingQuestionDuringReviewP2S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepAskAClarifyingQuestionDuringReviewP3S2 — Ask a Clarifying Question During Review — [no: committed] · [yes], step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"dispatch-answer-job","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"ask-a-clarifying-question-during-review/dispatch-answer-job/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-acq-P3-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepAskAClarifyingQuestionDuringReviewP3S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAskAClarifyingQuestionDuringReviewP3S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepAskAClarifyingQuestionDuringReviewP4S1 — Ask a Clarifying Question During Review — [no: committed] · [no: dispatch miss], step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-answer-job","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"ask-a-clarifying-question-during-review/dispatch-answer-job/1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-acq-P4\",\"ActivityID\":\"architecture\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/design-answer\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-architecture\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/answer/architecture-system\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-acq-P4-1\",\"artifact_kind\":\"System\",\"command\":\"design-answer\",\"target_branch\":\"aiarch/answer/architecture-system\",\"prior_state_ref\":\"main\",\"job_mode\":\"answer\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-acq-P4\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: the agent run fails.
func stepAskAClarifyingQuestionDuringReviewP4S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAskAClarifyingQuestionDuringReviewP4S1) (any, error) {
	t.Helper()
	return failingSubmitStep(t, h, subject, 1, in.Spec)
}

// stepAskAClarifyingQuestionDuringReviewP4S2 — Ask a Clarifying Question During Review — [no: committed] · [no: dispatch miss], step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"dispatch-answer-job","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"ask-a-clarifying-question-during-review/dispatch-answer-job/2"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-acq-P4-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":3,\"FailedStep\":\"agent\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepAskAClarifyingQuestionDuringReviewP4S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputAskAClarifyingQuestionDuringReviewP4S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepExecuteAProjectActivityP1S2 — Execute a Project Activity — dispatch · yes, step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"execute-a-project-activity/observe-validate/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-epa-P1-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepExecuteAProjectActivityP1S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP1S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepExecuteAProjectActivityP2S1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"SubmitAgenticJob","call":"execute-a-project-activity/dispatch-task/3"},"expectedOutputs":[{"node":"dispatch-task","kind":"inferred","to":"","label":"Dispatch the task's agentic job; stage its artifact on the activity branch"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-epa-P2\",\"ActivityID\":\"C-review-engine\",\"Steps\":[{\"Name\":\"construct\",\"Toolchain\":\"claude-code\",\"Command\":[\"/service-construction\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-C-review-engine\"]}],\"Edges\":[],\"WorkspaceRef\":\"activity/C-review-engine\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-epa-P2-1\",\"activity_id\":\"C-review-engine\",\"component_id\":\"reviewEngine\",\"command\":\"service-construction\",\"phase\":\"construction\",\"role\":\"senior-developer\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-epa-P2\"},\"WorkflowFile\":\"aiarch-construct.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":["dispatch-task"],"hook":true}
// Arrangement: the agent run fails.
func stepExecuteAProjectActivityP2S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP2S1) (any, error) {
	t.Helper()
	return failingSubmitStep(t, h, subject, 1, in.Spec)
}

// stepExecuteAProjectActivityP2S2 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"execute-a-project-activity/observe-validate/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-epa-P2-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":3,\"FailedStep\":\"construct\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepExecuteAProjectActivityP2S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP2S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepExecuteAProjectActivityP3S2 — Execute a Project Activity — review · agents only · approve, step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"dispatch-reviewers","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"execute-a-project-activity/dispatch-reviewers/0.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-epa-P3-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepExecuteAProjectActivityP3S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP3S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepExecuteAProjectActivityP5S1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"SubmitAgenticJob","call":"execute-a-project-activity/dispatch-task/3"},"expectedOutputs":[{"node":"dispatch-task","kind":"inferred","to":"","label":"Dispatch the task's agentic job; stage its artifact on the activity branch"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-epa-P5\",\"ActivityID\":\"C-review-engine\",\"Steps\":[{\"Name\":\"construct\",\"Toolchain\":\"claude-code\",\"Command\":[\"/service-construction\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-C-review-engine\"]}],\"Edges\":[],\"WorkspaceRef\":\"activity/C-review-engine\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-epa-P5-1\",\"activity_id\":\"C-review-engine\",\"component_id\":\"reviewEngine\",\"command\":\"service-construction\",\"phase\":\"construction\",\"role\":\"senior-developer\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-epa-P5\"},\"WorkflowFile\":\"aiarch-construct.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":["dispatch-task"],"hook":true}
// Arrangement: the agent run fails.
func stepExecuteAProjectActivityP5S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP5S1) (any, error) {
	t.Helper()
	return failingSubmitStep(t, h, subject, 1, in.Spec)
}

// stepExecuteAProjectActivityP5S2 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"execute-a-project-activity/observe-validate/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-epa-P5-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":3,\"FailedStep\":\"construct\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepExecuteAProjectActivityP5S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP5S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepExecuteAProjectActivityP5S4 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 4: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":4,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"execute-a-project-activity/observe-validate/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-epa-P5-handle-3","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepExecuteAProjectActivityP5S4(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP5S4) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 4, in.Handle)
}

// stepExecuteAProjectActivityP6S1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"dispatch-task","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"SubmitAgenticJob","call":"execute-a-project-activity/dispatch-task/3"},"expectedOutputs":[{"node":"dispatch-task","kind":"inferred","to":"","label":"Dispatch the task's agentic job; stage its artifact on the activity branch"}],"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-epa-P6\",\"ActivityID\":\"C-review-engine\",\"Steps\":[{\"Name\":\"construct\",\"Toolchain\":\"claude-code\",\"Command\":[\"/service-construction\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-C-review-engine\"]}],\"Edges\":[],\"WorkspaceRef\":\"activity/C-review-engine\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-epa-P6-1\",\"activity_id\":\"C-review-engine\",\"component_id\":\"reviewEngine\",\"command\":\"service-construction\",\"phase\":\"construction\",\"role\":\"senior-developer\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-epa-P6\"},\"WorkflowFile\":\"aiarch-construct.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":["dispatch-task"],"hook":true}
// Arrangement: the agent run fails.
func stepExecuteAProjectActivityP6S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP6S1) (any, error) {
	t.Helper()
	return failingSubmitStep(t, h, subject, 1, in.Spec)
}

// stepExecuteAProjectActivityP6S2 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"execute-a-project-activity/observe-validate/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-epa-P6-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":3,\"FailedStep\":\"construct\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepExecuteAProjectActivityP6S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP6S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepExecuteAProjectActivityP7S2 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"dispatch-reviewers","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"execute-a-project-activity/dispatch-reviewers/0.1"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-epa-P7-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepExecuteAProjectActivityP7S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP7S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// stepExecuteAProjectActivityP7S4 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 4: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":4,"node":"observe-validate","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"ObserveAgenticJob","call":"execute-a-project-activity/observe-validate/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-epa-P7-handle-3","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepExecuteAProjectActivityP7S4(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputExecuteAProjectActivityP7S4) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 4, in.Handle)
}

// stepReplanUnderScopeChangeP1S1 — Replan Under Scope Change — [yes], step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"back-to-design","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"SubmitAgenticJob","call":"replan-under-scope-change/back-to-design/5"},"expectedOutputs":[{"node":"notify-returned-to-design","kind":"sendSignal","to":"architect-user","label":"Notify the architect: not a scope change, returned to system design"}],"expectedStates":[{"node":"system-amendment-open","object":"System design amendment session","inState":"[open at the System]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-rsc-P1\",\"ActivityID\":\"architecture\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/system-draft\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-architecture\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/architecture-system\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-rsc-P1-1\",\"artifact_kind\":\"System\",\"command\":\"system-draft\",\"target_branch\":\"aiarch/session/architecture-system\",\"prior_state_ref\":\"main\",\"job_mode\":\"draft\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-rsc-P1\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[{"for":"system-amendment-open","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aja-rsc-P1","schemaRef":"#/$defs/ProjectID"},{"name":"branch","value":"aiarch/session/architecture-system"}],"expect":{"result":"{\"id\":\"stp-aja-rsc-P1\",\"slots\":{\"5\":{\"status\":1}}}","errorExpected":false}}],"unobservable":["notify-returned-to-design"],"hook":true}
// Arrangement: the agent drafts the System amendment on its session branch.
func stepReplanUnderScopeChangeP1S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputReplanUnderScopeChangeP1S1) (any, error) {
	t.Helper()
	return systemAmendmentStep(t, h, subject, in.Spec)
}

// probeReplanUnderScopeChangeP1S1N1 — Replan Under Scope Change — [yes], step 1, probe 1: designSessionAccess.ReadProjectOnBranch proves system-amendment-open.
// Probe (JSON): {"for":"system-amendment-open","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aja-rsc-P1","schemaRef":"#/$defs/ProjectID"},{"name":"branch","value":"aiarch/session/architecture-system"}],"expect":{"result":"{\"id\":\"stp-aja-rsc-P1\",\"slots\":{\"5\":{\"status\":1}}}","errorExpected":false}}
// Arrangement: read the System session branch back through the real designSessionAccess.
func probeReplanUnderScopeChangeP1S1N1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) (any, error) {
	t.Helper()
	return probeBranch(t, h, subject, "stp-aja-rsc-P1", systemSessionBranch)
}

// stepRequeueAFailedConstructionActivityP1S1 — Requeue a Failed Construction Activity, step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"resume-activity","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"SubmitAgenticJob","call":"requeue-a-failed-construction-activity/resume-activity/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-rfa-P1\",\"ActivityID\":\"C-review-engine\",\"Steps\":[{\"Name\":\"construct\",\"Toolchain\":\"claude-code\",\"Command\":[\"/service-construction\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-C-review-engine\"]}],\"Edges\":[],\"WorkspaceRef\":\"activity/C-review-engine\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-rfa-P1-1\",\"activity_id\":\"C-review-engine\",\"component_id\":\"reviewEngine\",\"command\":\"service-construction\",\"phase\":\"construction\",\"role\":\"senior-developer\",\"operator_note\":\"requeue: the prior attempt failed on a flaky venue step; resume from the staged branch\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-rfa-P1\"},\"WorkflowFile\":\"aiarch-construct.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: a prior attempt staged work on the activity branch; the re-queued run resumes from it.
func stepRequeueAFailedConstructionActivityP1S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputRequeueAFailedConstructionActivityP1S1) (any, error) {
	t.Helper()
	return stagedResumeStep(t, h, subject, in.Spec)
}

// stepSendBackChangeRequestsForARedraftP1S1 — Send Back Change Requests for a Redraft — [yes], step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"redraft","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"send-back-change-requests-for-a-redraft/redraft/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-sbr-P1\",\"ActivityID\":\"requirements\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/mission-draft\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-requirements\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/requirements-mission\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-sbr-P1-1\",\"artifact_kind\":\"Mission\",\"command\":\"mission-draft\",\"target_branch\":\"aiarch/session/requirements-mission\",\"prior_state_ref\":\"main\",\"job_mode\":\"draft\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-sbr-P1\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: the change request is open on the Mission draft; the agent redrafts, answering it.
func stepSendBackChangeRequestsForARedraftP1S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputSendBackChangeRequestsForARedraftP1S1) (any, error) {
	t.Helper()
	return missionRedraftStep(t, h, subject, 1, 1, in.Spec)
}

// stepSendBackChangeRequestsForARedraftP1S2 — Send Back Change Requests for a Redraft — [yes], step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"respond-per-comment","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"send-back-change-requests-for-a-redraft/respond-per-comment/0"},"expectedOutputs":null,"expectedStates":[{"node":"obj-thread-answered","object":"Review thread","inState":"[every change request has a response]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-sbr-P1-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[{"for":"obj-thread-answered","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aja-sbr-P1","schemaRef":"#/$defs/ProjectID"},{"name":"branch","value":"aiarch/session/requirements-mission"}],"expect":{"result":"{\"id\":\"stp-aja-sbr-P1\",\"slots\":{\"0\":{\"reviewThread\":[{\"type\":\"changeRequest\",\"status\":\"answered\"}]}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepSendBackChangeRequestsForARedraftP1S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputSendBackChangeRequestsForARedraftP1S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// probeSendBackChangeRequestsForARedraftP1S2N1 — Send Back Change Requests for a Redraft — [yes], step 2, probe 1: designSessionAccess.ReadProjectOnBranch proves obj-thread-answered.
// Probe (JSON): {"for":"obj-thread-answered","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aja-sbr-P1","schemaRef":"#/$defs/ProjectID"},{"name":"branch","value":"aiarch/session/requirements-mission"}],"expect":{"result":"{\"id\":\"stp-aja-sbr-P1\",\"slots\":{\"0\":{\"reviewThread\":[{\"type\":\"changeRequest\",\"status\":\"answered\"}]}}}","errorExpected":false}}
// Arrangement: read the Mission session branch back through the real designSessionAccess.
func probeSendBackChangeRequestsForARedraftP1S2N1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) (any, error) {
	t.Helper()
	return probeBranch(t, h, subject, "stp-aja-sbr-P1", missionSessionBranch)
}

// stepSendBackChangeRequestsForARedraftP2S1 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 1: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":1,"node":"redraft","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"send-back-change-requests-for-a-redraft/redraft/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-sbr-P2\",\"ActivityID\":\"requirements\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/mission-draft\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-requirements\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/requirements-mission\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-sbr-P2-1\",\"artifact_kind\":\"Mission\",\"command\":\"mission-draft\",\"target_branch\":\"aiarch/session/requirements-mission\",\"prior_state_ref\":\"main\",\"job_mode\":\"draft\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-sbr-P2\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: the change request is open on the Mission draft; the agent redrafts, answering it.
func stepSendBackChangeRequestsForARedraftP2S1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputSendBackChangeRequestsForARedraftP2S1) (any, error) {
	t.Helper()
	return missionRedraftStep(t, h, subject, 1, 1, in.Spec)
}

// stepSendBackChangeRequestsForARedraftP2S2 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 2: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":2,"node":"respond-per-comment","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"send-back-change-requests-for-a-redraft/respond-per-comment/0"},"expectedOutputs":null,"expectedStates":[{"node":"obj-thread-answered","object":"Review thread","inState":"[every change request has a response]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-sbr-P2-handle-1","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[{"for":"obj-thread-answered","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aja-sbr-P2","schemaRef":"#/$defs/ProjectID"},{"name":"branch","value":"aiarch/session/requirements-mission"}],"expect":{"result":"{\"id\":\"stp-aja-sbr-P2\",\"slots\":{\"0\":{\"reviewThread\":[{\"type\":\"changeRequest\",\"status\":\"answered\"}]}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepSendBackChangeRequestsForARedraftP2S2(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputSendBackChangeRequestsForARedraftP2S2) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 2, in.Handle)
}

// probeSendBackChangeRequestsForARedraftP2S2N1 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 2, probe 1: designSessionAccess.ReadProjectOnBranch proves obj-thread-answered.
// Probe (JSON): {"for":"obj-thread-answered","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aja-sbr-P2","schemaRef":"#/$defs/ProjectID"},{"name":"branch","value":"aiarch/session/requirements-mission"}],"expect":{"result":"{\"id\":\"stp-aja-sbr-P2\",\"slots\":{\"0\":{\"reviewThread\":[{\"type\":\"changeRequest\",\"status\":\"answered\"}]}}}","errorExpected":false}}
// Arrangement: read the Mission session branch back through the real designSessionAccess.
func probeSendBackChangeRequestsForARedraftP2S2N1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) (any, error) {
	t.Helper()
	return probeBranch(t, h, subject, "stp-aja-sbr-P2", missionSessionBranch)
}

// stepSendBackChangeRequestsForARedraftP2S3 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 3: SubmitAgenticJob
// Projected stimulus (JSON): {"seq":3,"node":"redraft","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"send-back-change-requests-for-a-redraft/redraft/0"},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"SubmitAgenticJob","inputs":[{"name":"spec","value":"{\"ProjectID\":\"stp-aja-sbr-P2\",\"ActivityID\":\"requirements\",\"Steps\":[{\"Name\":\"agent\",\"Toolchain\":\"claude-code\",\"Command\":[\"/mission-draft\"],\"Resources\":{\"CPUMillis\":2000,\"MemMiB\":4096,\"GPUs\":0},\"CacheKeys\":[\"aiarch-requirements\"]}],\"Edges\":[],\"WorkspaceRef\":\"aiarch/session/requirements-mission\",\"DispatchInputs\":{\"idempotency_token\":\"stp-aja-sbr-P2-3\",\"artifact_kind\":\"Mission\",\"command\":\"mission-draft\",\"target_branch\":\"aiarch/session/requirements-mission\",\"prior_state_ref\":\"main\",\"job_mode\":\"draft\"},\"TargetRepo\":{\"Owner\":\"stp-org\",\"Name\":\"stp-aja-sbr-P2\"},\"WorkflowFile\":\"aiarch-design.yml\"}","schemaRef":"#/$defs/PipelineSpec"}],"expect":{"errorExpected":false},"probes":[],"unobservable":[],"hook":true}
// Arrangement: the reviewer re-opened the change request (iterate); the agent redrafts, answering it again.
func stepSendBackChangeRequestsForARedraftP2S3(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputSendBackChangeRequestsForARedraftP2S3) (any, error) {
	t.Helper()
	return missionRedraftStep(t, h, subject, 3, 2, in.Spec)
}

// stepSendBackChangeRequestsForARedraftP2S4 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 4: ObserveAgenticJob
// Projected stimulus (JSON): {"seq":4,"node":"respond-per-comment","input":{"kind":"call","from":"delivery-manager","to":"agenticjobaccess","op":"","call":"send-back-change-requests-for-a-redraft/respond-per-comment/0"},"expectedOutputs":null,"expectedStates":[{"node":"obj-thread-answered","object":"Review thread","inState":"[every change request has a response]"}],"hints":null}
// Binding (JSON): {"seq":4,"operation":"ObserveAgenticJob","inputs":[{"name":"handle","value":"stp-aja-sbr-P2-handle-3","schemaRef":"#/$defs/PipelineHandle"}],"expect":{"result":"{\"Phase\":2,\"FailedStep\":\"\"}","errorExpected":false},"probes":[{"for":"obj-thread-answered","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aja-sbr-P2","schemaRef":"#/$defs/ProjectID"},{"name":"branch","value":"aiarch/session/requirements-mission"}],"expect":{"result":"{\"id\":\"stp-aja-sbr-P2\",\"slots\":{\"0\":{\"reviewThread\":[{\"type\":\"changeRequest\",\"status\":\"answered\"}]}}}","errorExpected":false}}],"unobservable":[],"hook":true}
// Arrangement: poll the submitted run to its terminal observation.
func stepSendBackChangeRequestsForARedraftP2S4(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess, in inputSendBackChangeRequestsForARedraftP2S4) (any, error) {
	t.Helper()
	return observeStep(t, h, subject, 4, in.Handle)
}

// probeSendBackChangeRequestsForARedraftP2S4N1 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 4, probe 1: designSessionAccess.ReadProjectOnBranch proves obj-thread-answered.
// Probe (JSON): {"for":"obj-thread-answered","component":"designSessionAccess","operation":"ReadProjectOnBranch","inputs":[{"name":"projectID","value":"stp-aja-sbr-P2","schemaRef":"#/$defs/ProjectID"},{"name":"branch","value":"aiarch/session/requirements-mission"}],"expect":{"result":"{\"id\":\"stp-aja-sbr-P2\",\"slots\":{\"0\":{\"reviewThread\":[{\"type\":\"changeRequest\",\"status\":\"answered\"}]}}}","errorExpected":false}}
// Arrangement: read the Mission session branch back through the real designSessionAccess.
func probeSendBackChangeRequestsForARedraftP2S4N1(t *testing.T, h *scenariohost.Host, subject agenticjob.AgenticJobAccess) (any, error) {
	t.Helper()
	return probeBranch(t, h, subject, "stp-aja-sbr-P2", missionSessionBranch)
}

// ---------------------------------------------------------------------------
// The local task-spec sequence (agent task spec, Task B6)
//
// These tests are not projected from the test plan: its bindings predate the
// task-spec envelope (task_id / spec_hash / revision), so no scenario dispatches
// a gated run. They drive the subject the way the scenarios do — the real local
// executor over a clone of the host's repo, the agent double first on PATH — with
// one difference: the rig binary is a SCRIPTED state-tool double instead of a
// no-op, because the gated sequence runs it (task-spec, edit-scope, run-checks,
// record-check-run) and each test arranges what it answers. The double logs
// every call it receives; the tests read the log, the agent double's argv and
// settings, and the branch the run left.
// ---------------------------------------------------------------------------

// stpSpecHash is the hash the state-tool double resolves the task spec to by
// default — the spec_hash a gated dispatch carries.
const stpSpecHash = "99ec1af48d521c00697724d27aa29782b1abf3599c903327e1b13d526714b4eb"

// stpTaskSpec is the spec the state-tool double's task-spec writes: the shape
// task-spec writes, reduced to what the local sequence reads.
const stpTaskSpec = `{
  "activity": "C-review-engine",
  "task": "construction",
  "model": {"id": "sonnet"},
  "tools": {"mcp": ["run_checks"], "builtin": ["Bash", "Edit", "Glob", "Grep", "Read", "Skill", "Write"]},
  "editScope": {"components": [{"id": "review-engine", "paths": ["internal/engine/review/**"]}], "extra": [], "stateOnly": false},
  "checks": [
    {"id": "command:build", "kind": "command", "source": {"argv": ["go", "build", "./internal/engine/review/..."]}},
    {"id": "command:vet", "kind": "command", "source": {"argv": ["go", "vet", "./internal/engine/review/..."]}}
  ],
  "hash": "` + stpSpecHash + `"
}
`

// stpGreenResults / stpRedResults are what the double's run-checks writes.
const (
	stpGreenResults = `[{"checkId":"command:build","kind":"command","status":"pass","durationMs":1},{"checkId":"command:vet","kind":"command","status":"pass","durationMs":1}]`
	stpRedResults   = `[{"checkId":"command:build","kind":"command","status":"pass","durationMs":1},{"checkId":"command:vet","kind":"command","status":"fail","durationMs":1}]`
)

// gateVenue is one gate test's subject and the state-tool double's control dir.
type gateVenue struct {
	subject agenticjob.AgenticJobAccess
	env     *scenarioEnv
	ctl     string
}

// newGateVenue builds the subject with the scripted state-tool double, answering
// green by default: the spec resolves to stpSpecHash, the edit scope holds, every
// check passes.
func newGateVenue(t *testing.T, h *scenariohost.Host) *gateVenue {
	t.Helper()
	root := t.TempDir()
	ctl := filepath.Join(root, "tool-ctl")
	if err := os.MkdirAll(ctl, 0o750); err != nil {
		t.Fatalf("hooks: %v", err)
	}
	tool := writeExecutable(t, filepath.Join(root, "bin", "aiarch-state-mcp"), fmt.Sprintf(`#!/bin/sh
# State-tool double: logs each call, then answers the gated sequence's
# subcommands as the test arranged in its control dir.
ctl=%[1]q
printf '%%s\n' "$*" >> "$ctl/calls.log"
sub=$1; shift
env > "$ctl/env-$sub"
out=
run=
while [ $# -gt 0 ]; do
  case "$1" in --out) out=$2; shift ;; --run-id) run=$2; shift ;; esac
  shift
done
case "$sub" in
task-spec)
  cp "$ctl/spec.json" "$out" || exit 2
  cat "$ctl/spec-hash"
  exit 0 ;;
edit-scope)
  if [ -f "$ctl/edit-scope-red" ]; then
    echo "edit-scope: 1 changed path(s) lie outside the task's edit scope: stray.txt" >&2
    exit 1
  fi
  exit 0 ;;
run-checks)
  git symbolic-ref HEAD > "$ctl/checked-ref"
  mkdir -p "$out" && cp "$ctl/results.json" "$out/results.json" || exit 2
  exit "$(cat "$ctl/run-checks-rc")" ;;
record-check-run)
  mkdir -p .aiarch/state && printf '{"recorded":"%%s"}\n' "$run" > .aiarch/state/project.json || exit 2
  exit 0 ;;
esac
exit 0
`, ctl))
	v := &gateVenue{ctl: ctl}
	v.set(t, "spec.json", stpTaskSpec)
	v.set(t, "spec-hash", stpSpecHash+"\n")
	v.set(t, "results.json", stpGreenResults)
	v.set(t, "run-checks-rc", "0")
	v.subject = buildSubject(t, h, root, tool)
	v.env = recorderOf(t, v.subject).scenario()
	return v
}

func (v *gateVenue) set(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(v.ctl, name), []byte(content), 0o600); err != nil {
		t.Fatalf("hooks: %v", err)
	}
}

// calls is every call the state-tool double received, in order.
func (v *gateVenue) calls(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(v.ctl, "calls.log"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("hooks: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// subcommands is the subcommand of each call, in order.
func (v *gateVenue) subcommands(t *testing.T) []string {
	t.Helper()
	var subs []string
	for _, c := range v.calls(t) {
		if sub, _, _ := strings.Cut(c, " "); sub != "" {
			subs = append(subs, sub)
		}
	}
	return subs
}

// call is the first call of subcommand sub, or "".
func (v *gateVenue) call(t *testing.T, sub string) string {
	t.Helper()
	for _, c := range v.calls(t) {
		if strings.HasPrefix(c, sub+" ") {
			return c
		}
	}
	return ""
}

// agentArgv is the argv the agent double was last started with ("" when it never ran).
func (v *gateVenue) agentArgv(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(v.env.agent.argv)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("hooks: %v", err)
	}
	return string(raw)
}

// runToEnd submits spec and polls until the run is terminal.
func (v *gateVenue) runToEnd(t *testing.T, h *scenariohost.Host, spec agenticjob.PipelineSpec) agenticjob.PipelineObservation {
	t.Helper()
	rc := callContext(t, h, "1")
	handle, err := v.subject.SubmitAgenticJob(rc, spec)
	if err != nil {
		t.Fatalf("SubmitAgenticJob: %v", err)
	}
	deadline := time.Now().Add(observeBudget)
	for {
		obs, err := v.subject.ObserveAgenticJob(rc, handle)
		if err != nil {
			t.Fatalf("ObserveAgenticJob: %v", err)
		}
		if agenticjob.PipelinePhaseIsTerminal(obs.Phase) {
			return obs
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s still %s after %s", handle, agenticjob.PipelinePhaseString(obs.Phase), observeBudget)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// constructSpec is a construction dispatch of C-review-engine's task, carrying the
// task-spec envelope as deliveryManager sends it.
func constructSpec(task string) agenticjob.PipelineSpec {
	return agenticjob.PipelineSpec{
		ProjectID:  "stp-gates",
		ActivityID: "C-review-engine",
		Steps:      []agenticjob.PipelineStep{{Name: "construct", Toolchain: "claude-code", Command: []string{"/service-" + task}}},
		DispatchInputs: map[string]string{
			"activity_id":  "C-review-engine",
			"component_id": "reviewEngine",
			"command":      "service-" + task,
			"task_id":      task,
			"spec_hash":    stpSpecHash,
			"revision":     "C-review-engine:" + task + ":1",
		},
	}
}

// branchFiles lists the files at branch's tip in the clone.
func branchFiles(t *testing.T, clone, branch string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-tree", "-r", "--name-only", "refs/heads/"+branch)
	cmd.Dir = clone
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hooks: ls-tree %s: %v", branch, err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// flagValue is the value following flag in a logged call.
func flagValue(call, flag string) string {
	fields := strings.Fields(call)
	for i, f := range fields {
		if f == flag && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// TestLocalRailRunsTheAgentOnItsTaskSpec: a gated construction dispatch resolves its
// spec, runs the agent on the spec's model and built-in allowlist (no deny list), then
// gates the agent's commits — edit scope against SHAs captured before it ran, the check
// gate, the record — commits the CheckRun onto the activity branch, and keeps the run's
// results (uncommitted) where the testing gate reads them.
func TestLocalRailRunsTheAgentOnItsTaskSpec(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	obs := v.runToEnd(t, h, constructSpec("construction"))
	if obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("run ended %s (%s), want succeeded", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	if got, want := v.subcommands(t), []string{"task-spec", "edit-scope", "run-checks", "record-check-run"}; !slices.Equal(got, want) {
		t.Fatalf("state-tool calls %q, want %q", got, want)
	}
	spec := v.call(t, "task-spec")
	if flagValue(spec, "--activity") != "C-review-engine" || flagValue(spec, "--task") != "construction" {
		t.Errorf("task-spec call %q, want the dispatched activity and task", spec)
	}
	v.assertAgentOnSpec(t)
	v.assertGatesJudgedTheAgent(t)
	v.assertRunRecorded(t)
}

// assertAgentOnSpec: the agent ran on the spec's model and built-in allowlist, with no
// deny list in argv or settings.
func (v *gateVenue) assertAgentOnSpec(t *testing.T) {
	t.Helper()
	argv := v.agentArgv(t)
	if !strings.Contains(argv, "--model\nsonnet\n--tools\nBash,Edit,Glob,Grep,Read,Skill,Write\n") {
		t.Errorf("agent argv %q, want --model sonnet --tools Bash,Edit,Glob,Grep,Read,Skill,Write", argv)
	}
	if strings.Contains(argv, "--disallowedTools") {
		t.Errorf("agent argv %q carries --disallowedTools", argv)
	}
	settings, err := os.ReadFile(v.env.agent.settings)
	if err != nil {
		t.Fatalf("hooks: %v", err)
	}
	if strings.Contains(string(settings), `"permissions"`) {
		t.Errorf("settings %s deny built-ins, want the --tools allowlist alone", settings)
	}
}

// assertGatesJudgedTheAgent: the edit scope judged the agent's commits from SHAs, and
// the check gate ran.
func (v *gateVenue) assertGatesJudgedTheAgent(t *testing.T) {
	t.Helper()
	scope := v.call(t, "edit-scope")
	for _, flag := range []string{"--base", "--head", "--main"} {
		if !fullSHA.MatchString(flagValue(scope, flag)) {
			t.Errorf("edit-scope call %q: %s is not a full SHA", scope, flag)
		}
	}
	if flagValue(scope, "--base") == flagValue(scope, "--head") {
		t.Errorf("edit-scope call %q judges no agent commit", scope)
	}
	if gate := v.call(t, "run-checks"); !strings.Contains(gate, "--gate") {
		t.Errorf("run-checks call %q, want the gate", gate)
	}
}

// assertRunRecorded: the run was recorded under a local id naming the dispatched attempt,
// at the dispatched revision, with its repo-relative artifact; its results were kept
// where the testing gate reads them before any merge (the shared checkout's
// self-ignoring .aiarch/test-results, never a commit); the record itself was committed
// onto the activity branch; and the check output is kept out of commits.
func (v *gateVenue) assertRunRecorded(t *testing.T) {
	t.Helper()
	rec := v.call(t, "record-check-run")
	runID := flagValue(rec, "--run-id")
	if !strings.HasPrefix(runID, "local-C-review-engine:construction:1-") || flagValue(rec, "--revision") != "C-review-engine:construction:1" {
		t.Errorf("record-check-run call %q, want a local run id naming the attempt, and the dispatched revision", rec)
	}
	if got := flagValue(rec, "--artifact"); got != ".aiarch/test-results/"+runID {
		t.Errorf("record-check-run artifact %q, want the repo-relative .aiarch/test-results/%s", got, runID)
	}
	if _, err := os.Stat(filepath.Join(v.env.clone, ".aiarch", "test-results", runID, "check-results", "results.json")); err != nil {
		t.Errorf("the results of %s were not kept: %v", runID, err)
	}
	for _, f := range branchFiles(t, v.env.clone, reviewEngineBranch) {
		if strings.HasPrefix(f, ".aiarch/test-results/") {
			t.Errorf("activity branch commits %s, want results kept out of commits", f)
		}
	}
	v.assertRecordedOn(t, reviewEngineBranch, runID)
	exclude, err := os.ReadFile(filepath.Join(v.env.clone, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("hooks: %v", err)
	}
	for _, line := range []string{"/test-results/", "/check-results/"} {
		if strings.Count(string(exclude), line+"\n") != 1 {
			t.Errorf("info/exclude %q, want %s exactly once", exclude, line)
		}
	}
}

// assertRecordedOn: branch's tip is the record commit of runID, carrying the state
// document the record wrote.
func (v *gateVenue) assertRecordedOn(t *testing.T, branch, runID string) {
	t.Helper()
	if got := gitOut(t, v.env.clone, "log", "-1", "--format=%s", "refs/heads/"+branch); got != "checks: record run "+runID {
		t.Errorf("%s's tip is %q, want the record commit of %s", branch, got, runID)
	}
	if got := gitOut(t, v.env.clone, "show", "refs/heads/"+branch+":"+stateDocPath); !strings.Contains(got, runID) {
		t.Errorf("%s's state document %q, want the record of %s", branch, got, runID)
	}
}

// gitOut is git's trimmed stdout in dir.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hooks: git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// TestLocalRailRefusesADriftedTaskSpec: the spec the worktree resolves hashes to
// something other than the dispatched spec_hash — the run fails before the agent starts
// and no gate runs.
func TestLocalRailRefusesADriftedTaskSpec(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	v.set(t, "spec-hash", strings.Repeat("0", 64)+"\n")
	obs := v.runToEnd(t, h, constructSpec("construction"))
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "task spec drifted") {
		t.Fatalf("run ended %s (%q), want failed: task spec drifted", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	if argv := v.agentArgv(t); argv != "" {
		t.Errorf("the agent ran (argv %q) on a drifted spec", argv)
	}
	if got := v.subcommands(t); !slices.Equal(got, []string{"task-spec"}) {
		t.Errorf("state-tool calls %q, want task-spec alone", got)
	}
}

// TestLocalRailFailsARedCheckAndStillRecordsIt: a check does not pass — the CheckRun is
// recorded all the same (evidence red or green) and the run fails naming that check.
func TestLocalRailFailsARedCheckAndStillRecordsIt(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	v.set(t, "results.json", stpRedResults)
	v.set(t, "run-checks-rc", "1")
	obs := v.runToEnd(t, h, constructSpec("construction"))
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "check command:vet did not pass (fail)") {
		t.Fatalf("run ended %s (%q), want failed on command:vet", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	if v.call(t, "record-check-run") == "" {
		t.Errorf("state-tool calls %q, want the red run recorded", v.subcommands(t))
	}
}

// TestLocalRailFailsAnOutOfScopeEdit: the agent's commits leave the spec's edit scope —
// the run fails on the edit-scope gate, and the check gate and the record still run.
func TestLocalRailFailsAnOutOfScopeEdit(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	v.set(t, "edit-scope-red", "")
	obs := v.runToEnd(t, h, constructSpec("construction"))
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "edit scope") || !strings.Contains(obs.Diagnostic, "stray.txt") {
		t.Fatalf("run ended %s (%q), want failed on the edit scope", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	if got, want := v.subcommands(t), []string{"task-spec", "edit-scope", "run-checks", "record-check-run"}; !slices.Equal(got, want) {
		t.Errorf("state-tool calls %q, want %q", got, want)
	}
}

// TestLocalRailGatesAFailedAgentRun: the agent fails — the gates still run (the
// templates' always()) and the run fails with the agent's own diagnostic.
func TestLocalRailGatesAFailedAgentRun(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	v.env.agent.failNextRun(t)
	obs := v.runToEnd(t, h, constructSpec("construction"))
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "agent double: run failed") {
		t.Fatalf("run ended %s (%q), want the agent's failure", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	if v.call(t, "record-check-run") == "" {
		t.Errorf("state-tool calls %q, want the gates run after a failed agent", v.subcommands(t))
	}
}

// TestLocalRailGeneratesTheScenarioTestsBeforeTheIntegrationAgent: the integration task
// generates the scenario tests (make gen-tests, in the module dir the spec's
// generated-tests check runs in) before the agent starts.
func TestLocalRailGeneratesTheScenarioTestsBeforeTheIntegrationAgent(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	makeLog := v.makeDouble(t, 0)
	obs := v.runToEnd(t, h, constructSpec("integration"))
	if obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("run ended %s (%s), want succeeded", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	got, err := os.ReadFile(makeLog)
	if err != nil {
		t.Fatalf("make never ran: %v", err)
	}
	if strings.TrimSpace(string(got)) != "gen-tests before-agent GOWORK=off in wt" {
		t.Errorf("make ran as %q, want gen-tests once, before the agent, GOWORK=off, at the repo root", got)
	}
}

// makeDouble puts a `make` first on PATH that logs its target, whether the agent has run
// yet, GOWORK and the directory it ran in, then exits rc. It returns the log's path.
func (v *gateVenue) makeDouble(t *testing.T, rc int) string {
	t.Helper()
	makeLog := filepath.Join(v.ctl, "make.log")
	writeExecutable(t, filepath.Join(v.env.agent.binDir, "make"), fmt.Sprintf(`#!/bin/sh
# make double: logs the target, whether the agent has run yet, GOWORK and its dir.
if [ -f %[1]q ]; then ran=after-agent; else ran=before-agent; fi
echo "$* $ran GOWORK=$GOWORK in $(basename "$PWD")" >> %[2]q
exit %[3]d
`, v.env.agent.argv, makeLog, rc))
	return makeLog
}

// TestLocalRailRunsAnAnswerJobUngated: an answer job is no lifecycle task (ruling R30) —
// no spec, no gates, no --model/--tools; the step manifest's deny list scopes it as
// before. It stays ungated even when its dispatch names a task and a hash, as the
// design template's answer steps do.
func TestLocalRailRunsAnAnswerJobUngated(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	obs := v.runToEnd(t, h, agenticjob.PipelineSpec{
		ProjectID:  "stp-gates",
		ActivityID: "architecture",
		Steps:      []agenticjob.PipelineStep{{Name: "agent", Toolchain: "claude-code", Command: []string{"/design-answer"}}},
		DispatchInputs: map[string]string{
			"artifact_kind": "System", "command": "design-answer", "target_branch": systemSessionBranch,
			"prior_state_ref": systemSessionBranch, "job_mode": "answer",
			"activity_id": "architecture", "task_id": "systemDraft", "spec_hash": stpSpecHash,
		},
	})
	if obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("run ended %s (%s), want succeeded", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	if got := v.subcommands(t); len(got) != 0 {
		t.Errorf("state-tool calls %q, want none for an answer job", got)
	}
	if argv := v.agentArgv(t); strings.Contains(argv, "--model") || strings.Contains(argv, "--tools") {
		t.Errorf("answer agent argv %q carries the task spec's flags", argv)
	}
}

// TestLocalRailRefusesAHalfEnvelope: a dispatch whose envelope is partial or malformed —
// a task without a hash, a hash without a task, a hash that is not a digest, an id or a
// revision outside the templates' id shape — is refused at Submit: it can be held to no
// spec, and the agent never starts.
func TestLocalRailRefusesAHalfEnvelope(t *testing.T) {
	h := scenariohost.Start(t)
	for name, mutate := range map[string]func(map[string]string){
		"task without hash":    func(in map[string]string) { delete(in, "spec_hash") },
		"hash without task":    func(in map[string]string) { delete(in, "task_id") },
		"malformed hash":       func(in map[string]string) { in["spec_hash"] = "not-a-hash" },
		"task id not an id":    func(in map[string]string) { in["task_id"] = "../construction" },
		"revision not an id":   func(in map[string]string) { in["revision"] = "a\nb" },
		"revision with spaces": func(in map[string]string) { in["revision"] = "C-review-engine construction 1" },
	} {
		t.Run(name, func(t *testing.T) {
			v := newGateVenue(t, h)
			spec := constructSpec("construction")
			mutate(spec.DispatchInputs)
			if _, err := v.subject.SubmitAgenticJob(callContext(t, h, "1"), spec); err == nil {
				t.Fatal("SubmitAgenticJob accepted the envelope")
			}
			if argv := v.agentArgv(t); argv != "" {
				t.Errorf("the agent ran (argv %q) on a refused dispatch", argv)
			}
		})
	}
}

// TestLocalRailGatesTheActivityBranchWhateverTheAgentLeftCheckedOut: the agent commits,
// then switches to a scratch branch and commits there. The check gate still runs on the
// activity branch and the CheckRun lands on it; the scratch branch gets no record.
func TestLocalRailGatesTheActivityBranchWhateverTheAgentLeftCheckedOut(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	if err := os.WriteFile(v.env.agent.switchTo, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	obs := v.runToEnd(t, h, constructSpec("construction"))
	if obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("run ended %s (%s), want succeeded", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	checked, err := os.ReadFile(filepath.Join(v.ctl, "checked-ref"))
	if err != nil {
		t.Fatalf("the check gate never ran: %v", err)
	}
	if got := strings.TrimSpace(string(checked)); got != "refs/heads/"+reviewEngineBranch {
		t.Errorf("the check gate ran on %s, want refs/heads/%s", got, reviewEngineBranch)
	}
	runID := flagValue(v.call(t, "record-check-run"), "--run-id")
	v.assertRecordedOn(t, reviewEngineBranch, runID)
	if got := gitOut(t, v.env.clone, "log", "-1", "--format=%s", "refs/heads/scratch"); got != "agent double: left on scratch" {
		t.Errorf("scratch's tip is %q, want the agent's own commit and no record", got)
	}
}

// TestLocalRailKeepsTheServersSecretsFromTheGates: a secret in the server's environment
// reaches none of the gate subprocesses (the check gate runs code the agent wrote), while
// the toolchain and the rig do.
func TestLocalRailKeepsTheServersSecretsFromTheGates(t *testing.T) {
	h := scenariohost.Start(t)
	t.Setenv("ARCHISTRATOR_STRIPE_SECRET_KEY", "sk_test_should_not_leak")
	t.Setenv("GOFLAGS", "-mod=mod")
	v := newGateVenue(t, h)
	if obs := v.runToEnd(t, h, constructSpec("construction")); obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("run ended %s (%s), want succeeded", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	for _, sub := range []string{"task-spec", "edit-scope", "run-checks", "record-check-run"} {
		env, err := os.ReadFile(filepath.Join(v.ctl, "env-"+sub))
		if err != nil {
			t.Fatalf("%s never ran: %v", sub, err)
		}
		if strings.Contains(string(env), "sk_test_should_not_leak") {
			t.Errorf("%s ran with the server's secret in its env", sub)
		}
		for _, want := range []string{"GOFLAGS=-mod=mod", "AIARCH_TASK_ID=construction", "PATH="} {
			if !strings.Contains(string(env), want) {
				t.Errorf("%s ran without %s", sub, want)
			}
		}
	}
}

// TestLocalRailGatesARunWhoseScenarioTestsDidNotGenerate: make gen-tests fails — the
// agent never starts, the gates still run and record (the templates' always()), and the
// run fails with the generation's reason.
func TestLocalRailGatesARunWhoseScenarioTestsDidNotGenerate(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	v.makeDouble(t, 2)
	obs := v.runToEnd(t, h, constructSpec("integration"))
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "scenario tests could not be generated") {
		t.Fatalf("run ended %s (%q), want failed: the scenario tests could not be generated", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	if argv := v.agentArgv(t); argv != "" {
		t.Errorf("the agent ran (argv %q) without its scenario tests", argv)
	}
	if got, want := v.subcommands(t), []string{"task-spec", "edit-scope", "run-checks", "record-check-run"}; !slices.Equal(got, want) {
		t.Errorf("state-tool calls %q, want %q", got, want)
	}
}

// TestLocalRailGatesADesignDraft: a design draft dispatch is held to its spec like a
// construction one — resolved on the session branch, run on the spec's model, gated and
// recorded onto the session branch.
func TestLocalRailGatesADesignDraft(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	obs := v.runToEnd(t, h, agenticjob.PipelineSpec{
		ProjectID:  "stp-gates",
		ActivityID: "architecture",
		Steps:      []agenticjob.PipelineStep{{Name: "agent", Toolchain: "claude-code", Command: []string{"/system-draft"}}},
		DispatchInputs: map[string]string{
			"artifact_kind": "System", "command": "system-draft", "target_branch": systemSessionBranch,
			"prior_state_ref": "", "job_mode": "draft",
			"activity_id": "architecture", "task_id": "systemDraft", "spec_hash": stpSpecHash, "revision": "architecture:systemDraft:1",
		},
	})
	if obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("run ended %s (%s), want succeeded", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	if got, want := v.subcommands(t), []string{"task-spec", "edit-scope", "run-checks", "record-check-run"}; !slices.Equal(got, want) {
		t.Fatalf("state-tool calls %q, want %q", got, want)
	}
	spec := v.call(t, "task-spec")
	if flagValue(spec, "--activity") != "architecture" || flagValue(spec, "--task") != "systemDraft" {
		t.Errorf("task-spec call %q, want the design activity and task", spec)
	}
	if !strings.Contains(v.agentArgv(t), "--model\nsonnet\n") {
		t.Errorf("design agent argv %q, want the spec's model", v.agentArgv(t))
	}
	v.assertRecordedOn(t, systemSessionBranch, flagValue(v.call(t, "record-check-run"), "--run-id"))
}

// TestLocalRailJudgesTheWholeDiffWithoutMain: a repo with no main — the edit-scope gate
// judges the whole base..head diff (--no-main), as the templates do when origin has none.
func TestLocalRailJudgesTheWholeDiffWithoutMain(t *testing.T) {
	h := scenariohost.Start(t)
	v := newGateVenue(t, h)
	runGit(t, v.env.clone, "branch", reviewEngineBranch, "main")
	runGit(t, v.env.clone, "checkout", "--quiet", "--detach")
	runGit(t, v.env.clone, "branch", "-D", "main")
	if obs := v.runToEnd(t, h, constructSpec("construction")); obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("run ended %s (%s), want succeeded", agenticjob.PipelinePhaseString(obs.Phase), obs.Diagnostic)
	}
	scope := v.call(t, "edit-scope")
	if !strings.Contains(scope, "--no-main") || strings.Contains(scope, "--main ") {
		t.Errorf("edit-scope call %q, want --no-main and no --main", scope)
	}
}
