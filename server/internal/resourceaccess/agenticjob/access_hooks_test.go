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
	stateMCP := writeExecutable(t, filepath.Join(root, "bin", "aiarch-state-mcp"),
		"#!/bin/sh\n# rig double: seat-assets renders the .claude prompt surface; the agent double reads none.\nexit 0\n")

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
	}
	a.claude = writeExecutable(t, filepath.Join(a.binDir, "claude"), fmt.Sprintf(`#!/bin/sh
# Agent double for the Claude Code CLI. Started by agenticJobAccess in the run's
# worktree with the AIARCH_* rig in its environment.
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
exec %[3]q -c user.name=agent-double -c user.email=agent-double@example.invalid -c commit.gpgsign=false \
  commit --allow-empty --no-verify --quiet -m "agent double: $AIARCH_COMMAND on $AIARCH_TARGET_BRANCH"
`, a.failFlag, a.nextState, gitBin, filepath.Dir(stateDocPath), stateDocPath))
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

// stepRequeueAFailedConstructionActivityP1S1 — Re-queue a Failed Construction Activity, step 1: SubmitAgenticJob
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
