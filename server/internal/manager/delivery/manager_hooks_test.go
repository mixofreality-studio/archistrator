// Hooks for deliveryManager scenario tests. Generated ONCE by testgen; owned by the construction agent.
//
// Every func here is a hook the generated scenarios file calls; the drift check
// (testgen -check) requires exactly this symbol set, so add or remove hooks by
// changing the test plan, not this file.
//
// THE SUBJECT is the REAL deliveryManager, built through its public generated
// constructor (delivery.NewDeliveryManager) and served by a real worker
// (delivery.RegisterManagerWorker) that the scenario host starts on its Temporal dev
// server (Host.StartWorker) — the LOCAL profile's wiring in cmd/server/main.gen.go and
// hooks.go, collaborator for collaborator:
//
//   - projectStateAccess and its three facets (designSessionAccess,
//     gitActivityStatusAccess, activityExecutionAccess), sourceControlAccess,
//     artifactAccess and episodeAccess are the GitLocal / LocalFS variants over ONE
//     real working repo — a fresh one per scenario (every binding names the same
//     project id, and the local profile is one project per repo). Like the local
//     profile's own, it is a non-bare repository (see workingRepo);
//   - agenticJobAccess is the local profile's construction venue: the real local
//     executor (agenticjob.NewLocalExecAgenticJobAccess), which adds a worktree per
//     run, seats the prompt surface with the venue's state tool, starts `claude` in
//     it and judges the run by whether the branch advanced;
//   - every Engine is the real (pure) Engine;
//   - messageBus is the real Temporal message bus with the delivery kind table.
//
// THE ONE DOUBLE is the resource outside this repo that the venue runs: the coding
// agent. The `claude` the venue starts is this test binary (see the venue section):
// a scripted agent that does, in its worktree, what a real agent does — it drives the
// venue's REAL state tool (cmd/aiarch-state-mcp, built for the run) over MCP, records
// the integration step's check run with that tool's record-check-run, or commits its
// construction work — first holding the dispatch to the task spec it names, as the venue
// templates do — and ends as the scenario scripts (succeeds, fails, or is still
// running when the operator looks).
//
// ARRANGEMENT goes through public operations only (round-2 ruling): the project is
// created, researched, and its design artifacts staged and committed through
// projectStateAccess's and designSessionAccess's validated writes; the phase moves
// through AdvancePhase; and every activity row is written by the SUBJECT — a pump tick
// walks the design activities whose artifacts are committed (the walk's own
// skip-if-committed seed), a review policy is the subject's SetProjectExecutionPolicy,
// a pause its SetProjectRunState. The models those writes carry are READ from the
// app's own committed design state (designState) as source data; nothing is copied
// into this package. The one model the arrangement composes is the plan (slots 9 and
// 10): the committed plan narrowed to the three design activities and the one
// construction activity the scenarios drive (drivenActivity), so a construction tick
// dispatches that activity and nothing else. Every probe is a read-only contract op.
//
// DETERMINISM (spec §12 B4) is the host's: replayWorkflows registers the workflows
// exactly as the composition root does, and every history a scenario started is
// replayed against them when the scenario's verdict is flushed.

package delivery_test

import (
	"bufio"
	"bytes"
	"context"
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
	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/artifact"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
)

// init hands the process to the venue's double when the venue started this test
// binary under one of the names it runs (see the venue section): as `claude`, the
// scripted agent; as `aiarch-state-mcp`, the venue's state tool; as `make`, the
// scenario-test generation the executor runs before an integration agent. Started any
// other way, it is the test binary and init does nothing.
func init() {
	switch filepath.Base(os.Args[0]) {
	case agentName:
		os.Exit(runScriptedAgent())
	case stateToolName:
		os.Exit(runVenueStateTool())
	case makeName:
		os.Exit(testinfra.RunMakeDouble(os.Args[1:]))
	}
}

// The project every in-flight binding names, and the design state its artifacts are
// read from.
const (
	planProject = "dct-owner/dct-delivery"
	planOwner   = "dct-owner"
	planName    = "dct-delivery"
	// designState is the app's own committed project, relative to this package; its
	// research corpus is committed beside it.
	designState    = "../../../../.aiarch/state/project.json"
	designResearch = "../../../../.aiarch/state/research"
	// drivenActivity is the one construction activity the narrowed plan keeps, and
	// drivenContract the contract it builds.
	drivenActivity = "C-review-engine"
	drivenContract = "reviewEngine"
)

// ---------------------------------------------------------------------------
// The stack
// ---------------------------------------------------------------------------

// replayWorkflows tells the scenario host which workflows this package's workers
// run: at every flush the host replays each workflow history a scenario started
// against them, and a non-determinism error fails that scenario. The registration
// is the composition root's own (delivery.RegisterManagerWorker over a manager built
// by buildManager), with collaborators of the SAME shape as the live worker's — the
// workflow code reads which collaborators are wired (the rail, the repo resolver, the
// status mirror), so a replay registry wired differently replays a different branch.
// Replay executes no activity, so the GitLocal collaborators' repo is never opened, the
// venue is the dry-run one nothing calls, and the host is one that serves no Temporal
// (its client is nil). The live worker runs with the SDK's default options, so the
// replayer does too.
func replayWorkflows() scenariohost.Workflows {
	return scenariohost.Workflows{Register: func(w scenariohost.Worker) {
		delivery.RegisterManagerWorker(w, buildManager(&scenariohost.Host{}, "file:///replay-registry-never-opens",
			agenticjob.NewDryRunAgenticJobAccess()))
	}}
}

// buildManager is the composition root's delivery wiring for the LOCAL profile
// (cmd/server/main.gen.go): every collaborator over the repo at url, the agentic job
// venue being jobs, the Temporal client the host's.
func buildManager(h *scenariohost.Host, url string, jobs agenticjob.AgenticJobAccess) delivery.DeliveryManager {
	tc := h.TemporalClient()
	return delivery.NewDeliveryManager(tc, projectstate.NewGitLocalProjectStateAccess(url),
		artifact.NewGitLocalArtifactAccess(url),
		intervention.NewInterventionEngine(), review.NewReviewEngine(), estimation.NewEstimationEngine(),
		operationestimation.NewOperationEstimationEngine(), billing.NewBillingEngine(), designhealth.NewDesignHealthEngine(),
		jobs, sourcecontrol.NewGitLocalSourceControlAccess(url),
		projectstate.NewGitLocalGitActivityStatusAccess(url), projectstate.NewGitLocalDesignSessionAccess(url),
		projectstate.NewGitLocalActivityExecutionAccess(url),
		messagebus.NewTemporalMessageBus(tc, deliveryKinds()),
		episode.NewLocalFSEpisodeAccess(url),
		escalationWait, interventionMode, localRepo, "")
}

// The composition root's two scalar delivery settings on the local profile.
const (
	escalationWait   = 30 * time.Minute
	interventionMode = "tiered"
)

// localRepo is the local profile's repo resolver (cmd/server DeliveryManagerRepo,
// GitLocal arm): every project resolves to its deterministic GitLocal RepoRef.
func localRepo(pid delivery.ProjectID) (sourcecontrol.RepoRef, bool) {
	return sourcecontrol.GitLocalRepoRefForProject(sourcecontrol.ProjectID(pid)), true
}

// stack is one scenario's downstream wiring, kept so Step and Probe hooks reach the
// same instances the subject's worker runs against.
type stack struct {
	repo     workingRepo
	ps       projectstate.ProjectStateAccess
	sessions projectstate.DesignSessionAccess
	venue    *venue
	// serve starts the subject's worker on the host, once. Every arrangement serves it
	// first, but for the one that needs the subject's queue unconsumed (a pause request
	// still being applied).
	serve func()
	// revisionsBefore is each design activity's revision count per task, taken before a
	// step whose settler waits on a design walk (snapshotRevisions).
	revisionsBefore map[string]map[string]int
	mu              sync.Mutex
	arranged        map[int]bool
}

var (
	stacksMu sync.Mutex
	stacks   = map[delivery.DeliveryManager]*stack{}
)

func stackOf(t *testing.T, subject delivery.DeliveryManager) *stack {
	t.Helper()
	stacksMu.Lock()
	defer stacksMu.Unlock()
	s, ok := stacks[subject]
	if !ok {
		t.Fatal("hooks: subject was not built by newSubject")
	}
	return s
}

// deliveryKinds is the messageBus kind table the delivery Manager's schedules
// resolve against — the delivery rows of cmd/server's MessageBusTemporalArgs.
func deliveryKinds() map[messagebus.ExecutionKind]messagebus.KindBinding {
	return map[messagebus.ExecutionKind]messagebus.KindBinding{
		"constructionPumpSweep": {WorkflowType: "constructionPumpSweep", TaskQueue: delivery.TaskQueue},
		"deliveryRoundSweep":    {WorkflowType: "deliveryRoundSweep", TaskQueue: delivery.TaskQueue},
	}
}

// venueRunTimeout bounds one agent run inside the venue, as the local profile's
// configuration does; the scripted agent finishes in well under a second, so this
// only guards a run the scenario holds open past its end.
const venueRunTimeout = 2 * time.Minute

// newSubject builds the component under test: the generated constructor over this
// scenario's own stack — the LOCAL profile's delivery wiring (cmd/server/main.gen.go),
// every collaborator over the scenario's repo and the agentic job venue being the
// local executor — served by a worker on the host's Temporal.
func newSubject(t *testing.T, h *scenariohost.Host) delivery.DeliveryManager {
	t.Helper()
	repo := startWorkingRepo(t)
	url := repo.URL
	v := openVenue(t, h, repo)
	jobs, err := agenticjob.NewLocalExecAgenticJobAccess(url, planProject, v.stateTool, projectstate.StateToolVersion(), venueRunTimeout)
	if err != nil {
		t.Fatalf("hooks: NewLocalExecAgenticJobAccess: %v", err)
	}
	subject := buildManager(h, url, jobs)

	var once sync.Once
	st := &stack{repo: repo, ps: projectstate.NewGitLocalProjectStateAccess(url),
		sessions: projectstate.NewGitLocalDesignSessionAccess(url), venue: v, arranged: map[int]bool{},
		serve: func() {
			once.Do(func() {
				h.StartWorker(t, delivery.TaskQueue, func(w scenariohost.Worker) { delivery.RegisterManagerWorker(w, subject) })
			})
		}}
	stacksMu.Lock()
	stacks[subject] = st
	stacksMu.Unlock()
	t.Cleanup(func() {
		stacksMu.Lock()
		delete(stacks, subject)
		stacksMu.Unlock()
	})
	return subject
}

// workingRepo is the scenario's project repo: a non-bare repository, which the
// venue requires (its episode traces must sit outside the git dir an agent can
// write), whose own checkout stays on a branch of its own. Every project branch —
// main included — is only ever pushed to, never checked out here, so no push has to
// update a working tree. (A checkout ON main, pushed to with
// receive.denyCurrentBranch=updateInstead, is not safe under the walk's concurrent
// writes: two racing pushes leave its index ahead of its ref and every later push is
// refused — measured in round 2; the local topology that removes it is escalated.)
type workingRepo struct {
	Dir string // the repository
	URL string // file://Dir, what every GitLocal collaborator is configured with
}

// startWorkingRepo creates the scenario's repo with one empty commit on main.
func startWorkingRepo(t *testing.T) workingRepo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "dct-delivery")
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
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, args...)
	if err != nil {
		t.Fatalf("hooks: %v", err)
	}
	return out
}

// ---------------------------------------------------------------------------
// The design state the arrangement reads
// ---------------------------------------------------------------------------

// designSource is the app's own committed design state, read once per test process as
// SOURCE DATA: the typed model of every slot (what the stage writes carry), each slot's
// model as the document holds it (what the scripted agent drafts), and the research
// corpus. Nothing of it is written anywhere but through a public operation.
type designSource struct {
	models    map[projectstate.ArtifactKind]projectstate.ModelEnvelope
	drafts    map[string]json.RawMessage // slot key → the bare model JSON
	contracts map[string]json.RawMessage // contract name → the service contract
	research  projectstate.ResearchInput
}

var (
	sourceOnce sync.Once
	sourceVal  designSource
	sourceErr  error
)

// source is the design state, failing t when it cannot be read.
func source(t *testing.T) designSource {
	t.Helper()
	sourceOnce.Do(func() { sourceVal, sourceErr = readDesignSource() })
	if sourceErr != nil {
		t.Fatalf("hooks: read the app's committed design state: %v", sourceErr)
	}
	return sourceVal
}

func readDesignSource() (designSource, error) {
	raw, err := os.ReadFile(designState)
	if err != nil {
		return designSource{}, err
	}
	var doc struct {
		ID        string                                     `json:"id"`
		Slots     map[string]struct{ Model json.RawMessage } `json:"slots"`
		Contracts map[string]json.RawMessage                 `json:"serviceContracts"`
		Research  struct {
			Sources []struct {
				Title string `json:"title"`
				Path  string `json:"path"`
			} `json:"sources"`
		} `json:"research"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return designSource{}, err
	}
	proj, _, err := projectstate.DecodeProjectJSON(raw, projectstate.ProjectID(doc.ID))
	if err != nil {
		return designSource{}, err
	}
	env, err := projectstate.EncodeProject(proj)
	if err != nil {
		return designSource{}, err
	}
	src := designSource{models: map[projectstate.ArtifactKind]projectstate.ModelEnvelope{}, drafts: map[string]json.RawMessage{},
		contracts: doc.Contracts}
	for kind, slot := range env.Slots {
		if slot.Model.HasModel() {
			src.models[kind] = slot.Model
		}
	}
	for key, slot := range doc.Slots {
		if len(slot.Model) > 0 && string(slot.Model) != "null" {
			src.drafts[key] = slot.Model
		}
	}
	for _, s := range doc.Research.Sources {
		b, err := os.ReadFile(filepath.Join(designResearch, filepath.Base(s.Path)))
		if err != nil {
			return designSource{}, err
		}
		src.research.Sources = append(src.research.Sources, projectstate.ResearchSource{Title: s.Title, Content: string(b)})
	}
	return src, nil
}

// planActivities is the narrowed plan's activity set: the three reserved design
// activities and the one construction activity the scenarios drive.
var planActivities = []string{"requirements", "architecture", "projectDesign", drivenActivity}

// model is the envelope the stage write of kind carries: the committed model, but for
// the plan (slots 9 and 10), which is narrowed to planActivities.
func (src designSource) model(t *testing.T, kind projectstate.ArtifactKind) projectstate.ModelEnvelope {
	t.Helper()
	env, ok := src.models[kind]
	if !ok {
		t.Fatalf("hooks: the design state holds no model for %s", kind.WireName())
	}
	if kind != projectstate.KindActivityList && kind != projectstate.KindNetwork {
		return env
	}
	decoded, err := env.Decode()
	if err != nil {
		t.Fatalf("hooks: decode the committed %s: %v", kind.WireName(), err)
	}
	var narrowed projectstate.ArtifactModel
	switch m := decoded.(type) {
	case *projectstate.ActivityList:
		narrowed = narrowActivityList(*m)
	case *projectstate.Network:
		narrowed = narrowNetwork(*m)
	default:
		t.Fatalf("hooks: %s decodes to %T", kind.WireName(), decoded)
	}
	out, err := projectstate.EncodeModel(narrowed)
	if err != nil {
		t.Fatalf("hooks: encode the narrowed %s: %v", kind.WireName(), err)
	}
	return out
}

func narrowActivityList(l projectstate.ActivityList) *projectstate.ActivityList {
	out := projectstate.ActivityList{}
	for _, a := range l.Activities {
		if slices.Contains(planActivities, a.Name) {
			out.Activities = append(out.Activities, a)
		}
	}
	return &out
}

// narrowNetwork keeps the kept activities' edges, the milestones whose whole fan-in is
// kept, and the critical path's kept members; an edge to anything else is dropped.
func narrowNetwork(n projectstate.Network) *projectstate.Network {
	out := projectstate.Network{CriticalPath: []string{}}
	kept := slices.Clone(planActivities)
	for _, m := range n.Milestones {
		whole := true
		for _, d := range m.DependsOn {
			whole = whole && slices.Contains(planActivities, d)
		}
		if whole {
			out.Milestones = append(out.Milestones, m)
			kept = append(kept, m.ID)
		}
	}
	for _, d := range n.Dependencies {
		if !slices.Contains(planActivities, d.Activity) {
			continue
		}
		dep := projectstate.NetworkDependency{Activity: d.Activity}
		for _, on := range d.DependsOn {
			if slices.Contains(kept, on) {
				dep.DependsOn = append(dep.DependsOn, on)
			}
		}
		out.Dependencies = append(out.Dependencies, dep)
	}
	for _, a := range n.CriticalPath {
		if slices.Contains(planActivities, a) {
			out.CriticalPath = append(out.CriticalPath, a)
		}
	}
	return &out
}

// ---------------------------------------------------------------------------
// Building the scenario project through public operations
// ---------------------------------------------------------------------------

// designOwner is the design activity whose walk produces each slot the arrangement may
// leave open; a slot no design activity drafts (operationalConcepts, planningAssumptions)
// is always committed, and standardCheck — withdrawn in the design state — is never
// staged.
var designOwner = map[projectstate.ArtifactKind]string{
	projectstate.KindMission:              "requirements",
	projectstate.KindGlossary:             "requirements",
	projectstate.KindScrubbedRequirements: "requirements",
	projectstate.KindVolatilities:         "requirements",
	projectstate.KindCoreUseCases:         "requirements",
	projectstate.KindSystem:               "architecture",
	projectstate.KindNormalSolution:       "projectDesign",
	projectstate.KindSubcriticalSolution:  "projectDesign",
	projectstate.KindCompressedSolution:   "projectDesign",
	projectstate.KindDecompressedSolution: "projectDesign",
	projectstate.KindRiskModel:            "projectDesign",
	projectstate.KindSdpReview:            "projectDesign",
}

// designOrder is the reserved design activities in plan order, and designPhase the
// project phase each runs in.
var (
	designOrder = []string{"requirements", "architecture", "projectDesign"}
	designPhase = map[string]int{"requirements": 0, "architecture": 0, "projectDesign": 1}
)

// writer chains one scenario's arrangement writes on the project version each returns,
// failing the test on the first refusal.
type writer struct {
	t *testing.T
	h *scenariohost.Host
	s *stack
	v projectstate.Version
	n int
}

func (w *writer) ok(op string, v projectstate.Version, err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatalf("hooks: arrange %s: %v", op, err)
	}
	w.v = v
}

// key is a fresh idempotency key for one arrangement write.
func (w *writer) key() string {
	w.n++
	return "arrange-" + strconv.Itoa(w.n)
}

func (w *writer) create() {
	w.t.Helper()
	v, err := w.s.ps.CreateProject(w.h.ResourceAccessContext(w.t, w.key()), planProject, planOwner, planName)
	w.ok("CreateProject", v, err)
}

func (w *writer) research(in projectstate.ResearchInput) {
	w.t.Helper()
	v, err := w.s.ps.SetResearchInput(w.h.ResourceAccessContext(w.t, w.key()), planProject, w.v, in)
	w.ok("SetResearchInput", v, err)
}

// commit stages kind's model on main and commits it with provenance — the two verbs
// project birth and M0 land a slot with.
func (w *writer) commit(kind projectstate.ArtifactKind, model projectstate.ModelEnvelope) {
	w.t.Helper()
	rc := w.h.ResourceAccessContext(w.t, w.key())
	v, err := w.s.sessions.StageArtifactForReviewOnBranch(rc, planProject, w.v, "", model, rc.IdempotencyKey)
	w.ok("StageArtifactForReviewOnBranch "+kind.WireName(), v, err)
	v, err = w.s.sessions.CommitArtifactWithProvenance(w.h.ResourceAccessContext(w.t, w.key()), planProject, w.v, kind,
		"architect", "agent:system-architect")
	w.ok("CommitArtifactWithProvenance "+kind.WireName(), v, err)
}

func (w *writer) advance() {
	w.t.Helper()
	v, err := w.s.ps.AdvancePhase(w.h.ResourceAccessContext(w.t, w.key()), planProject, w.v)
	w.ok("AdvancePhase", v, err)
}

// arrangeProject builds the scenario project through public operations, every design
// activity before `open` done and `open` (one of designOrder) live at its review gate —
// or, with open "", the project in construction with drivenActivity eligible and not
// started:
//
//  1. projectStateAccess creates the project and records its research;
//  2. every slot the walks before `open` produce, every slot no walk drafts, and the
//     narrowed plan are staged and committed (designSessionAccess) in kind order, so no
//     commit flags an earlier one stale; the phase advances to the one `open` runs in
//     (project design for a finished design, construction comes last);
//  3. one pump tick — the op the sweep calls — walks the design activities: each whose
//     artifacts are committed seeds passed and exits, written by the subject itself, and
//     the walk of `open` drafts (the scripted agent) and holds its gate for a human;
//  4. with open "", the pump goes idle on the design prefix (a construction activity
//     waits for the seal) and the phase is advanced into construction.
func arrangeProject(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, open string) {
	t.Helper()
	src := source(t)
	w := &writer{t: t, h: h, s: s}
	w.create()
	w.research(src.research)
	openAt := slices.Index(designOrder, open)
	for _, kind := range projectstate.AllArtifactKinds() {
		if kind == projectstate.KindStandardCheck {
			continue
		}
		if owner, drafted := designOwner[kind]; drafted && openAt >= 0 && slices.Index(designOrder, owner) >= openAt {
			continue
		}
		w.commit(kind, src.model(t, kind))
	}
	phase := designPhase[open]
	if open == "" {
		phase = designPhase["projectDesign"]
	}
	for range phase {
		w.advance()
	}
	if _, err := subject.ExecuteNextActivity(callContext(t, h, ""), planProject, "dct-dm-arrange-"+strconv.Itoa(openAt+1)); err != nil {
		t.Fatalf("hooks: tick the pump: %v", err)
	}
	if open != "" {
		awaitActivityState(t, h, subject, delivery.ActivityID(open), delivery.ActivityViewAwaitingHuman)
		return
	}
	for _, a := range designOrder {
		awaitActivityState(t, h, subject, delivery.ActivityID(a), delivery.ActivityViewDone)
	}
	awaitPumpIdle(t, h, subject)
	w.v = readVersion(t, h, s)
	w.advance()
}

// readVersion is the project's head version, read through projectStateAccess.
func readVersion(t *testing.T, h *scenariohost.Host, s *stack) projectstate.Version {
	t.Helper()
	v, err := s.ps.ReadProjectVersion(h.ResourceAccessContext(t, "arrange-read-version"), planProject)
	if err != nil {
		t.Fatalf("hooks: read the project version: %v", err)
	}
	return v
}

// ---------------------------------------------------------------------------
// Waiting on the asynchronous half
// ---------------------------------------------------------------------------

// walkWindow bounds a wait on the asynchronous walk. The venue observes every
// dispatch on the delivery Manager's own poll ladder (the first re-poll is 15 s after
// the submit), so a walk through a construction activity's tasks takes minutes, not
// seconds.
const walkWindow = 5 * time.Minute

// eventually polls probe until it reports done or the window closes; it is how a
// step observes an activity the pump started asynchronously, as its caller would.
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

// awaitActivityState waits until the activity view reads one of states.
func awaitActivityState(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, activityID delivery.ActivityID, states ...delivery.ActivityViewState) delivery.ActivityView {
	t.Helper()
	var view delivery.ActivityView
	eventually(t, walkWindow, fmt.Sprintf("activity %s reaching %v", activityID, states), func() (bool, string) {
		v, err := subject.QueryActivityView(callContext(t, h, ""), planProject, activityID)
		if err != nil {
			return false, err.Error()
		}
		view = v
		if slices.Contains(states, v.State) {
			return true, ""
		}
		return false, string(v.State)
	})
	return view
}

// ---------------------------------------------------------------------------
// The construction venue
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
//     publishDraft); a construction job commits its work; the integration job records
//     commits its wiring. Its stdout is the stream-json a real run writes, so the venue
//     mines the episode from it.
//
// The EXECUTOR itself holds every lifecycle run to its task spec with the real state
// tool — task-spec before the agent (a drifted hash refuses the run), edit-scope and
// record-check-run after it — as the seated templates do. Two of the steps it runs reach
// toolchains a scenario venue has no module for, and those are doubled: `run-checks`
// (the venue's check runner writes a green result for every check the spec holds —
// every scenario projected onto the component among them; testinfra.RunChecksDouble)
// and `make gen-tests` (testinfra.RunMakeDouble).

const (
	agentName     = "claude"
	stateToolName = "aiarch-state-mcp"
	makeName      = "make"
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
	for _, name := range []string{agentName, stateToolName, makeName} {
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
	mux.HandleFunc("/checks", serveChecks)
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

	mu        sync.Mutex
	submitted []agentJob
	outcome   func(n int, j agentJob) runOutcome
	refuse    func(command string) bool
	drafts    map[string]json.RawMessage // slot key → the model a first draft authors
	contract  json.RawMessage            // the service contract the detailed design records
	release   chan struct{}
	inFlight  sync.WaitGroup
	// narrow makes the integration step's check gate run, and so record, a narrowed
	// spec that never ran one projected scenario (dropped), so the testing gate has a
	// run to hold on.
	narrow  bool
	dropped string
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
		release: make(chan struct{}), drafts: source(t).drafts,
		contract: source(t).contracts[drivenContract]}
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

func current() *venue {
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
	if v := current(); v != nil {
		resp = v.run(req)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// checksRequest is the venue check runner's exchange with this process: the spec the
// executor's check gate runs, which the venue narrows when the scenario says so.
type checksRequest struct {
	Spec string `json:"spec"`
}

type checksResponse struct {
	Error string `json:"error"`
}

func serveChecks(w http.ResponseWriter, r *http.Request) {
	var req checksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var resp checksResponse
	if v := current(); v != nil {
		if err := v.narrowSpec(req.Spec); err != nil {
			resp.Error = err.Error()
		}
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
	if v := current(); v != nil {
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

// perform is what a succeeding run does in its worktree (the executor has already held
// the dispatch to its task spec).
func (v *venue) perform(n int, j agentJob) error {
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
		if strings.HasSuffix(j.Command, "-detailed-design") && foldName(j.Component) == foldName(drivenContract) {
			return v.recordContract(j)
		}
		// Every other step — integration's wiring among them — is a change on the
		// activity branch; the executor's gates record integration's check run.
		return v.commitWork(n, j)
	}
	return fmt.Errorf("no script for job mode %q", j.Mode)
}

// draftFor is the scripted first draft of kind.
func (v *venue) draftFor(kind string) json.RawMessage {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, k := range projectstate.AllArtifactKinds() {
		if kind == k.WireName() || kind == k.String() {
			return v.drafts[strconv.Itoa(int(k))]
		}
	}
	return nil
}

// draft re-drafts the slot from what the worktree already holds for it — the
// committed model (an amendment that keeps the content) or, for a first draft, the
// model the app's own design state holds for the slot — through putDraftModel, and
// publishes it.
func (v *venue) draft(j agentJob) error {
	doc, err := readWorktreeProject(j.dir)
	if err != nil {
		return err
	}
	slot, err := doc.slot(j.Kind)
	if err != nil {
		return err
	}
	raw := slot.Model
	if len(raw) == 0 {
		// A FIRST draft: the worktree holds no model of the slot yet, and what the agent
		// authors is the scenario's script — the slot as the app's design state holds it.
		raw = v.draftFor(j.Kind)
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
	slot, err := doc.slot(j.Kind)
	if err != nil {
		return err
	}
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

// narrowSpec, when the venue is set to narrow, drops the integration spec's first
// scenario check (re-hashed, so record-check-run takes it) and remembers the scenario it
// dropped. The executor's check gate runs it on the spec file the executor resolved, so
// the run the executor then records is the narrowed one.
func (v *venue) narrowSpec(specPath string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.narrow {
		return nil
	}
	spec, err := testinfra.ReadSpec(specPath)
	if err != nil {
		return err
	}
	if spec.Task != "integration" {
		return nil
	}
	i := slices.IndexFunc(spec.Checks, func(c projectstate.SpecCheck) bool { return c.Kind == projectstate.CheckKindScenario })
	if i < 0 {
		return fmt.Errorf("the integration spec of %s holds no scenario check to drop", spec.Activity)
	}
	v.dropped = spec.Checks[i].Source.Scenario
	spec.Checks = slices.Delete(spec.Checks, i, i+1)
	return testinfra.WriteSpec(specPath, spec)
}

// venueJob is the job as the shared venue double (testinfra) takes it.
func venueJob(j agentJob) testinfra.VenueJob {
	return testinfra.VenueJob{Mode: j.Mode, Command: j.Command, Activity: j.Activity, Dir: j.dir, Env: j.env}
}

// foldName folds a component's kebab id and its contract's camel name together (lower
// case, letters and digits only) — the spelling-insensitive join the scenario projection
// makes between them.
func foldName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// recordContract is the driven activity's detailed-design work (any other activity a
// scenario's pump reaches commits work like the other steps): the component's service contract —
// the one the app's design state holds for the driven contract — recorded through the state
// tool's recordServiceContract (validated by the server codec and the Method rules), then
// published.
func (v *venue) recordContract(j agentJob) error {
	var contract map[string]any
	if err := json.Unmarshal(v.contract, &contract); err != nil {
		return fmt.Errorf("no %s contract to record: %w", drivenContract, err)
	}
	return v.withStateTool(j, func(s *stateSession) error {
		_, err := s.call("recordServiceContract", map[string]any{"contract": contract})
		return err
	})
}

// commitWork is a construction step's work: a change on the activity branch, inside the
// task's edit scope (testinfra.WorkPath) as a real agent's is.
func (v *venue) commitWork(n int, j agentJob) error {
	tmp, err := os.MkdirTemp("", "dct-work-spec-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	specPath := filepath.Join(tmp, "spec.json")
	if _, err := testinfra.ResolveTaskSpec(v.tool, venueJob(j), specPath); err != nil {
		return err
	}
	spec, err := testinfra.ReadSpec(specPath)
	if err != nil {
		return err
	}
	rel := filepath.FromSlash(testinfra.WorkPath(spec, j.Activity, j.Command))
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
	_, err = runGit(j.dir, "-c", "user.name=scripted-agent", "-c", "user.email=scripted-agent@aiarch.local",
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
	Slots map[string]worktreeSlot `json:"slots"`
}

func readWorktreeProject(dir string) (worktreeProject, error) {
	var doc worktreeProject
	b, err := os.ReadFile(filepath.Join(dir, ".aiarch", "state", "project.json"))
	if err != nil {
		return doc, err
	}
	return doc, json.Unmarshal(b, &doc)
}

// slot is the slot of the job's artifact kind (named by its wire name or its
// PascalCase name, as the two dispatchers spell it).
func (p worktreeProject) slot(kind string) (worktreeSlot, error) {
	for _, k := range projectstate.AllArtifactKinds() {
		if kind == k.WireName() || kind == k.String() {
			return p.Slots[strconv.Itoa(int(k))], nil
		}
	}
	return worktreeSlot{}, fmt.Errorf("unknown artifact kind %q", kind)
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
// state tool, unless the scenario scripts the venue to refuse this job's seat, and but
// for the check gate's runner, which is the venue's (testinfra.RunChecksDouble — after
// this process narrows the spec, when the scenario says so).
func runVenueStateTool() int {
	r, err := readVenueRig(filepath.Dir(os.Args[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "venue:", err)
		return 1
	}
	if len(os.Args) > 1 && os.Args[1] == "run-checks" {
		return runVenueChecks(r)
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

// runVenueChecks is the venue's check runner (the state tool's run-checks): this test
// process narrows the spec first when the scenario says so (serveChecks), then the run
// writes a green result for every check the spec holds (testinfra.RunChecksDouble).
func runVenueChecks(r venueRig) int {
	req := checksRequest{}
	for i, a := range os.Args {
		if a == "--spec" && i+1 < len(os.Args) {
			req.Spec = os.Args[i+1]
		}
	}
	var resp checksResponse
	if err := post(r.URL+"/checks", req, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "venue:", err)
		return 2
	}
	if resp.Error != "" {
		fmt.Fprintln(os.Stderr, "venue:", resp.Error)
		return 2
	}
	return testinfra.RunChecksDouble(os.Args[2:], os.Stdout)
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
// Arrangement and settling
// ---------------------------------------------------------------------------

// arrangement prepares the stack before step seq of a scenario runs: it builds the
// scenario project (arrangeProject), scripts the agent venue, or waits for the
// asynchronous half to reach the state the step's guard names. It runs once per step.
type arrangement func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int)

// settler runs after a step's call. A step whose operation only ENQUEUES work (a
// pause signal, a decision signal) settles by waiting — through the subject's own
// read ops, as the operator's screen would — until the work it asked for has been
// applied; every other step returns the call's outcome untouched.
type settler func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, out any, err error) (any, error)

const pauseReason = "Holding construction while the billing vendor contract is renegotiated."

// arrangements is keyed by scenario id; a scenario without an entry gets the
// in-flight project (construction, drivenActivity not started) before its first step.
var arrangements = map[string]arrangement{
	"manage-projects-P1":            arrangeNothing,
	"manage-projects-P2":            arrangeNothing,
	"view-the-project-state-log-P2": arrangeSeventhVersion,
	"resume-paused-construction-P1": arrangePausedProject(false),
	"resume-paused-construction-P2": arrangePausedProject(true),
	// execute-a-project-activity: the review policy names who judges, the agent script
	// names whether the construction dispatch lands.
	"execute-a-project-activity-P1":             arrangeExecute(vibes, nil),
	"execute-a-project-activity-P2":             arrangeExecute(vibes, failConstructionUntil(3)),
	"execute-a-project-activity-P3":             arrangeExecute(vibes, nil),
	"execute-a-project-activity-P4":             arrangeExecute(humanAtConstruction, nil),
	"execute-a-project-activity-P5":             arrangeExecute(vibes, failConstructionTimes(1)),
	"execute-a-project-activity-P6":             arrangeExecute(vibes, failConstructionUntil(4)),
	"execute-a-project-activity-P7":             arrangeExecute(vibes, nil),
	"requeue-a-failed-construction-activity-P1": arrangeFailedActivity,
	"download-generated-source-code-P1":         arrangeBuiltActivity,
	"download-generated-source-code-P2":         arrangeBuiltActivity,
	// ask: [yes: session branch] finds the architecture walk live at its review gate,
	// [no: committed] finds it done; [no: dispatch miss] is a venue refusing the answer job.
	"ask-a-clarifying-question-during-review-P1": arrangeAsk(true, false),
	"ask-a-clarifying-question-during-review-P2": arrangeAsk(true, true),
	"ask-a-clarifying-question-during-review-P3": arrangeAsk(false, false),
	"ask-a-clarifying-question-during-review-P4": arrangeAsk(false, true),
	// commit / send back: the design activity is live at its review gate.
	"commit-to-a-project-option-P1":              arrangeGateOpen("projectDesign"),
	"commit-to-a-project-option-P2":              arrangeGateOpen("projectDesign"),
	"commit-to-a-project-option-P3":              arrangeGateOpen("projectDesign"),
	"send-back-change-requests-for-a-redraft-P1": arrangeGateOpen("architecture"),
	"send-back-change-requests-for-a-redraft-P2": arrangeGateOpen("architecture"),
	// amend / replan: every design activity is done; the dispatch re-opens one, and the
	// decision that follows is made at the gate the re-opened walk holds.
	"add-a-use-case-to-an-in-flight-project-P1": arrangeReopened("requirements", 2),
	"add-a-use-case-to-an-in-flight-project-P2": arrangeReopened("requirements", 2),
	"add-a-use-case-to-an-in-flight-project-P3": arrangeReopened("requirements", 3),
	"add-a-use-case-to-an-in-flight-project-P4": arrangeReopened("requirements", 3),
	"replan-under-scope-change-P2":              arrangeReopened("projectDesign", 3, 4),
	"replan-under-scope-change-P3":              arrangeReopened("projectDesign", 3, 4),
}

// arrangeReopened builds the in-flight project and, before each of gateSteps, waits until
// the re-opened `activity` holds its gate for a human — the state the reviewer acts on.
func arrangeReopened(activity string, gateSteps ...int) arrangement {
	return func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
		t.Helper()
		if seq == 1 {
			arrangeProject(t, h, subject, s, "")
		}
		for _, g := range gateSteps {
			if g == seq {
				awaitActivityState(t, h, subject, delivery.ActivityID(activity), delivery.ActivityViewAwaitingHuman)
			}
		}
	}
}

// arrangeGateOpen puts design activity `activity` live at its review gate before the
// first step.
func arrangeGateOpen(activity string) arrangement {
	return func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
		t.Helper()
		if seq == 1 {
			arrangeProject(t, h, subject, s, activity)
		}
	}
}

// arrangeAsk puts the architecture artifact where the ask use case finds it — live at its
// review gate on the activity branch, or committed — and, for a dispatch miss, scripts the
// venue to refuse the answer job's seat.
func arrangeAsk(live, dispatchMiss bool) arrangement {
	return func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
		t.Helper()
		if seq != 1 {
			return
		}
		open := ""
		if live {
			open = "architecture"
		}
		arrangeProject(t, h, subject, s, open)
		if dispatchMiss {
			s.venue.refuseSeat(func(command string) bool { return strings.HasPrefix(command, "design-answer") })
		}
	}
}

// arrangeBuiltActivity drives C-review-engine's walk to done under "vibes" — the
// activity whose generated source the owner then fetches.
func arrangeBuiltActivity(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
	t.Helper()
	if seq != 1 {
		return
	}
	arrangeProject(t, h, subject, s, "")
	setPolicy(t, h, subject, vibes)
	if _, err := subject.ExecuteNextActivity(callContext(t, h, ""), planProject, "dct-dm-download-arrange-0001"); err != nil {
		t.Fatalf("hooks: tick the pump: %v", err)
	}
	awaitActivityState(t, h, subject, drivenActivity, delivery.ActivityViewDone)
	awaitPumpIdle(t, h, subject)
}

// settlers is keyed by "<scenario>#<seq>".
var settlers = map[string]settler{
	"pause-construction-P1#1": settlePaused,
	// An ask on a committed artifact settles when the round's answer workflow has closed:
	// the job ran (or its seat was refused) and its replies, if any, landed on main.
	"ask-a-clarifying-question-during-review-P3#1": settleAnswerJob,
	"ask-a-clarifying-question-during-review-P4#1": settleAnswerJob,
	// The Override / decision steps only signal the activity's walk; they settle when
	// the walk has acted on them.
	"execute-a-project-activity-P2#3":             settleActivityRedispatched,
	"execute-a-project-activity-P4#3":             settleActivityLeaves(delivery.ActivityViewAwaitingHuman),
	"execute-a-project-activity-P6#3":             settleActivityRedispatched,
	"execute-a-project-activity-P6#4":             settleActivityRedispatched,
	"requeue-a-failed-construction-activity-P1#3": settleActivityRuns,
	// A gate decision settles when the walk has acted on it: the M0 approve when the
	// seal moved the project, a send-back when the redraft is back at the gate.
	"commit-to-a-project-option-P1#2":              settleCommitment,
	"commit-to-a-project-option-P2#2":              settleCommitment,
	"commit-to-a-project-option-P2#3":              settleDesignWalk("projectDesign"),
	"commit-to-a-project-option-P3#2":              settleDesignWalk("projectDesign"),
	"send-back-change-requests-for-a-redraft-P1#1": settleDesignWalk("architecture"),
	"send-back-change-requests-for-a-redraft-P2#1": settleDesignWalk("architecture"),
	"send-back-change-requests-for-a-redraft-P2#3": settleDesignWalk("architecture"),
	"add-a-use-case-to-an-in-flight-project-P1#2":  settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P2#2":  settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P3#3":  settleDesignWalk("requirements"),
	"add-a-use-case-to-an-in-flight-project-P4#3":  settleDesignWalk("requirements"),
	"replan-under-scope-change-P2#4":               settleCommitment,
	"replan-under-scope-change-P3#4":               settleDesignWalk("projectDesign"),
}

// settleDesignWalk waits until the design activity's walk has consumed the decision the
// step signalled, as the reviewer's screen would show it: the activity finished (or
// failed), or it is back at a gate on a NEWER revision than the step found (a redraft,
// or the M0 options re-computed) — counted against the revisions the activity held
// before the step (revisionsBefore).
func settleDesignWalk(activity string) settler {
	return func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, out any, err error) (any, error) {
		t.Helper()
		if err != nil {
			return out, err
		}
		before := s.revisionsBefore[activity]
		eventually(t, walkWindow, "the "+activity+" walk acting on the decision", func() (bool, string) {
			v, qerr := subject.QueryActivityView(callContext(t, h, ""), planProject, delivery.ActivityID(activity))
			if qerr != nil {
				return false, qerr.Error()
			}
			switch v.State {
			case delivery.ActivityViewDone, delivery.ActivityViewFailed:
				return true, ""
			case delivery.ActivityViewAwaitingHuman:
				return newRoundAwaitsHuman(v, before), "still at the same gate"
			}
			return false, string(v.State)
		})
		awaitPumpSettled(t, h, subject, activity)
		return out, err
	}
}

// revisionCounts is how many revisions each task of an activity holds.
func revisionCounts(v delivery.ActivityView) map[string]int {
	counts := map[string]int{}
	for _, task := range v.Tasks {
		counts[task.ID] = len(task.Revisions)
	}
	return counts
}

// newRoundAwaitsHuman reports whether a task opened a revision since before was taken and
// that revision is a round awaiting the reviewer.
func newRoundAwaitsHuman(v delivery.ActivityView, before map[string]int) bool {
	for _, task := range v.Tasks {
		n := len(task.Revisions)
		if n > before[task.ID] && task.Revisions[n-1].Outcome == delivery.TaskRevisionAwaitingHuman {
			return true
		}
	}
	return false
}

// snapshotRevisions records, before a step whose settler waits on a design walk, the
// revisions every design activity holds — what that settler measures "newer" against.
func snapshotRevisions(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack) {
	t.Helper()
	s.revisionsBefore = map[string]map[string]int{}
	for _, activity := range designOrder {
		v, err := subject.QueryActivityView(callContext(t, h, ""), planProject, delivery.ActivityID(activity))
		if err != nil {
			continue
		}
		s.revisionsBefore[activity] = revisionCounts(v)
	}
}

// settleCommitment settles an M0 approve: the walk acted on it, and the commitment it
// records — the seal that moves the project into construction, the step after the gate
// passes — has landed. The activity reads done the moment its gate attempt passes, a
// beat before the seal's own write, and the founder's screen shows the phase once that
// write is on main. The wait is bounded and does not fail: a seal that never lands is the
// probe's to report.
func settleCommitment(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, out any, err error) (any, error) {
	t.Helper()
	out, err = settleDesignWalk("projectDesign")(t, h, subject, s, out, err)
	if err != nil {
		return out, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		v, qerr := subject.QueryProjectView(callContext(t, h, ""), summaryQuery())
		if qerr == nil && v.Summary != nil && v.Summary.PhaseName == "construction" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return out, err
}

// awaitPumpSettled waits, after a design activity finished, for the pump that ran it to
// stop (a finished design walk hands the cascade on, and the project's phase moves with
// the M0 seal); while the activity is still at a gate there is nothing to wait for.
func awaitPumpSettled(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, activity string) {
	t.Helper()
	v, err := subject.QueryActivityView(callContext(t, h, ""), planProject, delivery.ActivityID(activity))
	if err != nil || v.State == delivery.ActivityViewAwaitingHuman {
		return
	}
	awaitPumpIdle(t, h, subject)
}

// arrangeFailedActivity puts C-review-engine where the requeue use case finds it: its
// walk ran, every construction dispatch failed until the supervision budget was spent,
// and the activity exited failed. It is driven, not seeded — one pump tick, the same op
// the sweep calls — and the venue keeps failing until the operator's requeue (step 3),
// after which the re-dispatched job is still in progress when the operator looks.
func arrangeFailedActivity(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
	t.Helper()
	if seq != 1 {
		return
	}
	arrangeProject(t, h, subject, s, "")
	setPolicy(t, h, subject, vibes)
	s.venue.script(func(_ int, j agentJob) runOutcome {
		switch {
		case !j.isConstruction():
			return runSucceeds
		case s.arrangedStep(3):
			return runStaysRunning
		}
		return runFails
	})
	if _, err := subject.ExecuteNextActivity(callContext(t, h, ""), planProject, "dct-dm-requeue-arrange-0001"); err != nil {
		t.Fatalf("hooks: tick the pump: %v", err)
	}
	awaitActivityState(t, h, subject, drivenActivity, delivery.ActivityViewFailed)
	awaitPumpIdle(t, h, subject)
}

// settleActivityRuns waits until the requeued activity's walk is running again.
func settleActivityRuns(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, _ *stack, out any, err error) (any, error) {
	t.Helper()
	if err != nil {
		return out, err
	}
	awaitActivityState(t, h, subject, drivenActivity, delivery.ActivityViewRunning)
	return out, err
}

// The review policies the execute scenarios run under: "agents only" (no gate asks a
// human) and "human required" at the code review (the construction phase's gate).
var (
	vibes               = delivery.ExecutionPolicyInput{Preset: "vibes"}
	humanAtConstruction = delivery.ExecutionPolicyInput{Policy: &delivery.ReviewPolicyInput{
		GatedPhasesByType: map[string][]string{"service": {"construction"}}}}
)

// setPolicy sets the project's review policy through the subject's own op.
func setPolicy(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, policy delivery.ExecutionPolicyInput) {
	t.Helper()
	if err := subject.SetProjectExecutionPolicy(callContext(t, h, ""), planProject, policy); err != nil {
		t.Fatalf("hooks: set the review policy: %v", err)
	}
}

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

// arrangeExecute builds the in-flight project with C-review-engine eligible and not
// started under policy, scripts the venue, and — before every later step — waits for
// the walk the pump started to settle where a human (or nobody) is next.
func arrangeExecute(policy delivery.ExecutionPolicyInput, script func(*stack) func(int, agentJob) runOutcome) arrangement {
	return func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
		t.Helper()
		switch seq {
		case 1:
			arrangeProject(t, h, subject, s, "")
			setPolicy(t, h, subject, policy)
			if script != nil {
				s.venue.script(script(s))
			}
		case 2:
			awaitActivityState(t, h, subject, drivenActivity,
				delivery.ActivityViewDone, delivery.ActivityViewFailed, delivery.ActivityViewAwaitingHuman)
		default:
			// The operator steers what the screen shows settled: an activity that exited
			// is steered once the pump has finished its cascade, so the steer is not racing
			// a tick that already read the row; one held at a gate is decided at the gate.
			awaitPumpSettled(t, h, subject, drivenActivity)
		}
	}
}

// settleAnswerJob waits for the answer workflow of the question round the ask just seeded on
// main's architecture thread to close. The workflow is the composition's own (the delivery
// Manager names it by project, artifact, answer branch and round); waiting on it rather
// than on the thread is what makes the probes observe the use case's end — the replies
// landed, or the question still open after the dispatch miss — and not a race with it.
func settleAnswerJob(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, _ *stack, out any, err error) (any, error) {
	t.Helper()
	if err != nil {
		return out, err
	}
	id := planProject
	kind := delivery.KindSystem
	v, qerr := subject.QueryProjectView(callContext(t, h, ""), delivery.ProjectViewQuery{Kind: delivery.ProjectViewSession, ProjectID: &id, ArtifactKind: &kind})
	if qerr != nil {
		t.Fatalf("hooks: reading the architecture thread: %v", qerr)
	}
	if v.Session == nil || len(v.Session.ReviewThread) == 0 {
		t.Fatal("hooks: the ask seeded no question on main's architecture thread")
	}
	round := v.Session.ReviewThread[len(v.Session.ReviewThread)-1].Round
	name := projectstate.KindSystem.String()
	wfID := fmt.Sprintf("%s:answer:%s:answer/%s-r%d:r%d", planProject, name, name, round, round)
	ctx, cancel := context.WithTimeout(h.Context(t), walkWindow)
	defer cancel()
	if werr := h.TemporalClient().GetWorkflow(ctx, wfID, "").Get(ctx, nil); werr != nil {
		t.Fatalf("hooks: the answer workflow %s did not close cleanly: %v", wfID, werr)
	}
	return out, err
}

// awaitPumpIdle waits until the project's pump reports no open run.
func awaitPumpIdle(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager) {
	t.Helper()
	id := planProject
	eventually(t, walkWindow, "the pump going idle", func() (bool, string) {
		v, err := subject.QueryProjectView(callContext(t, h, ""), delivery.ProjectViewQuery{Kind: delivery.ProjectViewPump, ProjectID: &id})
		if err != nil {
			return false, err.Error()
		}
		return v.Pump != nil && !v.Pump.Open, "pump open"
	})
}

// settleActivityLeaves waits until the activity has moved off state and settled.
func settleActivityLeaves(state delivery.ActivityViewState) settler {
	return func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, _ *stack, out any, err error) (any, error) {
		t.Helper()
		if err != nil {
			return out, err
		}
		settled := []delivery.ActivityViewState{delivery.ActivityViewDone, delivery.ActivityViewFailed, delivery.ActivityViewAwaitingHuman}
		var keep []delivery.ActivityViewState
		for _, st := range settled {
			if st != state {
				keep = append(keep, st)
			}
		}
		awaitActivityState(t, h, subject, drivenActivity, keep...)
		return out, err
	}
}

// settleActivityRedispatched waits until the override's re-dispatch has run and the
// walk has settled again.
func settleActivityRedispatched(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, out any, err error) (any, error) {
	t.Helper()
	if err != nil {
		return out, err
	}
	before := len(s.venue.jobs())
	eventually(t, walkWindow, "the override's re-dispatch", func() (bool, string) {
		return len(s.venue.jobs()) > before, "no new job"
	})
	awaitActivityState(t, h, subject, drivenActivity,
		delivery.ActivityViewDone, delivery.ActivityViewFailed, delivery.ActivityViewAwaitingHuman)
	return out, err
}

func arrangeNothing(*testing.T, *scenariohost.Host, delivery.DeliveryManager, *stack, int) {}

// arrangeInFlight builds the in-flight project before the first step: design done, the
// project in construction, drivenActivity eligible and not started.
func arrangeInFlight(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
	t.Helper()
	if seq == 1 {
		arrangeProject(t, h, subject, s, "")
	}
}

// arrangeSeventhVersion is a project whose head state is at version 7, the version the
// state-log binding names: created, researched, its mission and glossary drafted and
// committed, and its review policy chosen — seven writes, each a public operation.
func arrangeSeventhVersion(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
	t.Helper()
	if seq != 1 {
		return
	}
	src := source(t)
	w := &writer{t: t, h: h, s: s}
	w.create()
	w.research(src.research)
	for _, kind := range []projectstate.ArtifactKind{projectstate.KindMission, projectstate.KindGlossary} {
		w.commit(kind, src.model(t, kind))
	}
	setPolicy(t, h, subject, delivery.ExecutionPolicyInput{Preset: "checkpoints"})
}

// arrangePausedProject puts the project's operator pause where the resume use case finds
// it. Settled: the operator paused the in-flight project through the subject's own op and
// the pause was applied (recorded). In flight: the pause is recorded, and a second pause
// request is still being applied — accepted while the subject's worker is not consuming
// its queue, so the request's supervision run is open and unapplied (the [no] arm of the
// pause-settled guard). That arrangement never serves the worker and so builds no design
// rows: it records the pause and the construction phase through projectStateAccess alone.
func arrangePausedProject(inFlight bool) arrangement {
	return func(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, s *stack, seq int) {
		t.Helper()
		if seq != 1 {
			return
		}
		if !inFlight {
			arrangeProject(t, h, subject, s, "")
			if err := subject.SetProjectRunState(callContext(t, h, ""), planProject, delivery.ProjectPaused, pauseReason); err != nil {
				t.Fatalf("hooks: pause the project: %v", err)
			}
			if _, err := settlePaused(t, h, subject, s, nil, nil); err != nil {
				t.Fatalf("hooks: %v", err)
			}
			return
		}
		src := source(t)
		w := &writer{t: t, h: h, s: s}
		w.create()
		w.research(src.research)
		for _, kind := range projectstate.AllArtifactKinds() {
			if kind != projectstate.KindStandardCheck {
				w.commit(kind, src.model(t, kind))
			}
		}
		w.advance()
		w.advance()
		v, err := s.ps.RecordOperatorPaused(h.ResourceAccessContext(t, w.key()), planProject, w.v, pauseReason, "dct-pause-recorded")
		w.ok("RecordOperatorPaused", v, err)
		if err := subject.SetProjectRunState(callContext(t, h, ""), planProject, delivery.ProjectPaused, pauseReason); err != nil {
			t.Fatalf("hooks: request the in-flight pause: %v", err)
		}
	}
}

// settlePaused waits until the pause the step requested is recorded on the project.
func settlePaused(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, _ *stack, out any, err error) (any, error) {
	t.Helper()
	if err != nil {
		return out, err
	}
	eventually(t, 60*time.Second, "the operator pause being recorded", func() (bool, string) {
		v, qerr := subject.QueryProjectView(callContext(t, h, ""), summaryQuery())
		if qerr != nil {
			return false, qerr.Error()
		}
		return v.Summary != nil && v.Summary.OperatorPaused, "not paused yet"
	})
	return out, err
}

// resolveCommentID turns the binding's symbolic comment id into the one the store
// minted, the way the reviewer's screen does: the reviewer resolves the comment they
// see. The binding names the n-th change request it filed as dct-cr-000n (its
// author cannot know the store's r<round>c<n> ids); the n-th change request on the
// artifact's session thread, in thread order, is that comment. Any other id passes
// through unchanged.
func resolveCommentID(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, kind delivery.ArtifactKind, id *string) *string {
	t.Helper()
	var n int
	if id == nil {
		return id
	}
	if _, err := fmt.Sscanf(*id, "dct-cr-%04d", &n); err != nil || n < 1 {
		return id
	}
	pid := planProject
	v, err := subject.QueryProjectView(callContext(t, h, ""), delivery.ProjectViewQuery{Kind: delivery.ProjectViewSession, ProjectID: &pid, ArtifactKind: &kind})
	if err != nil || v.Session == nil {
		t.Fatalf("hooks: read the thread to resolve %s: %v", *id, err)
	}
	seen := 0
	for _, c := range v.Session.ReviewThread {
		if c.Type == "question" {
			continue
		}
		seen++
		if seen == n {
			return &c.ID
		}
	}
	t.Fatalf("hooks: the thread holds %d change requests, the binding names %s", seen, *id)
	return id
}

// resolveEpisodeID turns the binding's symbolic episode id into the one the venue minted,
// the way the operator's screen does: the operator opens the episode of the attempt they
// see failed. The binding names the n-th construction attempt of the driven activity as
// dct-ep-<component>-000n (its author cannot know the venue's ids — the local venue's is
// the run's dedup token); the episode is the one the walk recorded against THAT attempt —
// its targetRef is the attempt key <activity>:construction:<n> — and never the n-th of a
// listing, which the parallel test-plan job's episodes interleave with (B12). Any other id
// passes through unchanged.
func resolveEpisodeID(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, id *string) *string {
	t.Helper()
	if id == nil {
		return id
	}
	var n int
	at := strings.LastIndex(*id, "-")
	if !strings.HasPrefix(*id, "dct-ep-") || at < 0 {
		return id
	}
	if _, err := fmt.Sscanf((*id)[at+1:], "%04d", &n); err != nil || n < 1 {
		return id
	}
	pid, activity := planProject, drivenActivity
	v, err := subject.QueryProjectView(callContext(t, h, ""), delivery.ProjectViewQuery{
		Kind: delivery.ProjectViewEpisodes, ProjectID: &pid, ActivityID: &activity})
	if err != nil {
		t.Fatalf("hooks: list the episodes to resolve %s: %v", *id, err)
	}
	attempt := fmt.Sprintf("%s:construction:%d", drivenActivity, n)
	var seen []string
	for _, e := range v.Episodes {
		if e.TargetRef == attempt {
			return &e.EpisodeID
		}
		seen = append(seen, e.TargetRef)
	}
	t.Fatalf("hooks: %s lists no episode for attempt %s (the binding names %s); listed: %v", activity, attempt, *id, seen)
	return id
}

func summaryQuery() delivery.ProjectViewQuery {
	id := planProject
	return delivery.ProjectViewQuery{Kind: delivery.ProjectViewSummary, ProjectID: &id}
}

func arrange(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, scenario string, seq int) {
	t.Helper()
	s := stackOf(t, subject)
	if s.arrangedStep(seq) {
		return
	}
	s.markArranged(seq)
	if scenario != "resume-paused-construction-P2" {
		s.serve()
	}
	fn, ok := arrangements[scenario]
	if !ok {
		fn = arrangeInFlight
	}
	fn(t, h, subject, s, seq)
	if _, settles := settlers[fmt.Sprintf("%s#%d", scenario, seq)]; settles {
		snapshotRevisions(t, h, subject, s)
	}
}

func settle(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, scenario string, seq int, out any, err error) (any, error) {
	t.Helper()
	fn, ok := settlers[fmt.Sprintf("%s#%d", scenario, seq)]
	if !ok {
		return out, err
	}
	return fn(t, h, subject, stackOf(t, subject), out, err)
}

func (s *stack) markArranged(seq int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.arranged[seq] = true
}

func (s *stack) arrangedStep(seq int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.arranged[seq]
}

// stepAddAUseCaseToAnInFlightProjectP1S1 — Add a Use Case to an In-Flight Project — [variation] · [no], step 1: DispatchActivityTask
// Projected stimulus (JSON): {"seq":1,"node":"reopen-slot","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"add-a-use-case-to-an-in-flight-project/reopen-slot/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"feedback","value":"{\"notes\":\"Amendment: add the use case 'Export the project audit trail' (variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 1)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 1, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP1S2 — Add a Use Case to an In-Flight Project — [variation] · [no], step 2: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":2,"node":"redraft-review","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"add-a-use-case-to-an-in-flight-project/redraft-review/4","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":[{"node":"amended-use-case-draft-approved","object":"Amended use-case draft","inState":"[staged and approved]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"amended-use-case-draft-approved","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4,\"stage\":1}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP1S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP1S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 2)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 2, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP1S3 — Add a Use Case to an In-Flight Project — [variation] · [no], step 3: AcknowledgeStaleBasis
// Projected stimulus (JSON): {"seq":3,"node":"reconcile","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"AcknowledgeStaleBasis","call":"add-a-use-case-to-an-in-flight-project/reconcile/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"AcknowledgeStaleBasis","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureDraft"},{"name":"note","value":"reviewed — unaffected"}],"expect":{"errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"stage\":1}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP1S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP1S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 3)
	out, err := any(nil), subject.AcknowledgeStaleBasis(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Note)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P1", 3, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP2S1 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 1: DispatchActivityTask
// Projected stimulus (JSON): {"seq":1,"node":"reopen-slot","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"add-a-use-case-to-an-in-flight-project/reopen-slot/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"feedback","value":"{\"notes\":\"Amendment: add the use case 'Export the project audit trail' (variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 1)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 1, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP2S2 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 2: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":2,"node":"redraft-review","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"add-a-use-case-to-an-in-flight-project/redraft-review/4","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":[{"node":"amended-use-case-draft-approved","object":"Amended use-case draft","inState":"[staged and approved]"}],"hints":null}
// Binding (JSON): {"seq":2,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"amended-use-case-draft-approved","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4,\"stage\":1}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP2S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP2S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 2)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 2, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP2S3 — Add a Use Case to an In-Flight Project — [variation] · [yes], step 3: AcknowledgeStaleBasis
// Projected stimulus (JSON): {"seq":3,"node":"reconcile","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"AcknowledgeStaleBasis","call":"add-a-use-case-to-an-in-flight-project/reconcile/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"AcknowledgeStaleBasis","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureDraft"},{"name":"note","value":"reviewed — unaffected"}],"expect":{"errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"stage\":1}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP2S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP2S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 3)
	out, err := any(nil), subject.AcknowledgeStaleBasis(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Note)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P2", 3, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP3S1 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"revalidate","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"add-a-use-case-to-an-in-flight-project/revalidate/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"designHealth\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"designHealth\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP3S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP3S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 1, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP3S2 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 2: DispatchActivityTask
// Projected stimulus (JSON): {"seq":2,"node":"reopen-slot","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"add-a-use-case-to-an-in-flight-project/reopen-slot/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"feedback","value":"{\"notes\":\"Amendment: add the use case 'Export the project audit trail' (variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP3S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP3S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 2)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 2, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP3S3 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 3: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":3,"node":"redraft-review","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"add-a-use-case-to-an-in-flight-project/redraft-review/4","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":[{"node":"amended-use-case-draft-approved","object":"Amended use-case draft","inState":"[staged and approved]"}],"hints":null}
// Binding (JSON): {"seq":3,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"amended-use-case-draft-approved","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4,\"stage\":1}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP3S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP3S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 3)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 3, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP3S4 — Add a Use Case to an In-Flight Project — [core: rare] · [no], step 4: AcknowledgeStaleBasis
// Projected stimulus (JSON): {"seq":4,"node":"reconcile","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"AcknowledgeStaleBasis","call":"add-a-use-case-to-an-in-flight-project/reconcile/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"AcknowledgeStaleBasis","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureDraft"},{"name":"note","value":"reviewed — unaffected"}],"expect":{"errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"stage\":1}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP3S4(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP3S4) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 4)
	out, err := any(nil), subject.AcknowledgeStaleBasis(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Note)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P3", 4, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP4S1 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"revalidate","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"add-a-use-case-to-an-in-flight-project/revalidate/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"designHealth\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"designHealth\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP4S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP4S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 1, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP4S2 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 2: DispatchActivityTask
// Projected stimulus (JSON): {"seq":2,"node":"reopen-slot","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"add-a-use-case-to-an-in-flight-project/reopen-slot/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesDraft"},{"name":"feedback","value":"{\"notes\":\"Amendment: add the use case 'Export the project audit trail' (variation of view-the-project-state-log): the owner downloads every state commit with its operation name as one archive.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"use-case-model-amendment-open","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP4S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP4S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 2)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 2, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP4S3 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 3: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":3,"node":"redraft-review","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"add-a-use-case-to-an-in-flight-project/redraft-review/4","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":[{"node":"amended-use-case-draft-approved","object":"Amended use-case draft","inState":"[staged and approved]"}],"hints":null}
// Binding (JSON): {"seq":3,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"requirements"},{"name":"taskID","value":"coreUseCasesReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"amended-use-case-draft-approved","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":4,\"stage\":1}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP4S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP4S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 3)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 3, out, err)
}

// stepAddAUseCaseToAnInFlightProjectP4S4 — Add a Use Case to an In-Flight Project — [core: rare] · [yes], step 4: AcknowledgeStaleBasis
// Projected stimulus (JSON): {"seq":4,"node":"reconcile","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"AcknowledgeStaleBasis","call":"add-a-use-case-to-an-in-flight-project/reconcile/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"AcknowledgeStaleBasis","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureDraft"},{"name":"note","value":"reviewed — unaffected"}],"expect":{"errorExpected":false},"probes":[{"for":"stale-artifacts-reconciled","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"stage\":1}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAddAUseCaseToAnInFlightProjectP4S4(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAddAUseCaseToAnInFlightProjectP4S4) (any, error) {
	t.Helper()
	arrange(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 4)
	out, err := any(nil), subject.AcknowledgeStaleBasis(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Note)
	return settle(t, h, subject, "add-a-use-case-to-an-in-flight-project-P4", 4, out, err)
}

// stepAskAClarifyingQuestionDuringReviewP1S1 — Ask a Clarifying Question During Review — [yes: session branch] · [yes], step 1: AskQuestions
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"AskQuestions","call":"ask-a-clarifying-question-during-review/write-questions/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"AskQuestions","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"addressee","value":"system-architect"},{"name":"questions","value":"[{\"jsonPath\":\"$.containers[2].responsibilities\",\"text\":\"Why does delivery-manager own the review roster rather than review-engine?\",\"anchorText\":\"review roster\",\"replyTo\":\"\"}]"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-open-on-session-branch","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"}],"expect":{"result":"{\"activityId\":\"architecture\",\"state\":\"awaitingHuman\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAskAClarifyingQuestionDuringReviewP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAskAClarifyingQuestionDuringReviewP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "ask-a-clarifying-question-during-review-P1", 1)
	out, err := any(nil), subject.AskQuestions(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Addressee, in.Questions)
	return settle(t, h, subject, "ask-a-clarifying-question-during-review-P1", 1, out, err)
}

// stepAskAClarifyingQuestionDuringReviewP2S1 — Ask a Clarifying Question During Review — [yes: session branch] · [no: dispatch miss], step 1: AskQuestions
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"AskQuestions","call":"ask-a-clarifying-question-during-review/write-questions/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"AskQuestions","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"addressee","value":"system-architect"},{"name":"questions","value":"[{\"jsonPath\":\"$.containers[2].responsibilities\",\"text\":\"Why does delivery-manager own the review roster rather than review-engine?\",\"anchorText\":\"review roster\",\"replyTo\":\"\"}]"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-open-on-session-branch","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"}],"expect":{"result":"{\"activityId\":\"architecture\",\"state\":\"awaitingHuman\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAskAClarifyingQuestionDuringReviewP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAskAClarifyingQuestionDuringReviewP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "ask-a-clarifying-question-during-review-P2", 1)
	out, err := any(nil), subject.AskQuestions(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Addressee, in.Questions)
	return settle(t, h, subject, "ask-a-clarifying-question-during-review-P2", 1, out, err)
}

// stepAskAClarifyingQuestionDuringReviewP3S1 — Ask a Clarifying Question During Review — [no: committed] · [yes], step 1: AskQuestions
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"AskQuestions","call":"ask-a-clarifying-question-during-review/write-questions/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"AskQuestions","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"addressee","value":"system-architect"},{"name":"questions","value":"[{\"jsonPath\":\"$.containers[2].responsibilities\",\"text\":\"Why does delivery-manager own the review roster rather than review-engine?\",\"anchorText\":\"review roster\",\"replyTo\":\"\"}]"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-seeded-on-main","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"}],"expect":{"result":"{\"activityId\":\"architecture\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAskAClarifyingQuestionDuringReviewP3S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAskAClarifyingQuestionDuringReviewP3S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "ask-a-clarifying-question-during-review-P3", 1)
	out, err := any(nil), subject.AskQuestions(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Addressee, in.Questions)
	return settle(t, h, subject, "ask-a-clarifying-question-during-review-P3", 1, out, err)
}

// stepAskAClarifyingQuestionDuringReviewP4S1 — Ask a Clarifying Question During Review — [no: committed] · [no: dispatch miss], step 1: AskQuestions
// Projected stimulus (JSON): {"seq":1,"node":"write-questions","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"AskQuestions","call":"ask-a-clarifying-question-during-review/write-questions/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"AskQuestions","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"addressee","value":"system-architect"},{"name":"questions","value":"[{\"jsonPath\":\"$.containers[2].responsibilities\",\"text\":\"Why does delivery-manager own the review roster rather than review-engine?\",\"anchorText\":\"review roster\",\"replyTo\":\"\"}]"}],"expect":{"errorExpected":false},"probes":[{"for":"thread-seeded-on-main","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"}],"expect":{"result":"{\"activityId\":\"architecture\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepAskAClarifyingQuestionDuringReviewP4S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputAskAClarifyingQuestionDuringReviewP4S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "ask-a-clarifying-question-during-review-P4", 1)
	out, err := any(nil), subject.AskQuestions(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Addressee, in.Questions)
	return settle(t, h, subject, "ask-a-clarifying-question-during-review-P4", 1, out, err)
}

// stepCommitToAProjectOptionP1S1 — Commit to a Project Option — [commit] · [no], step 1: DispatchActivityTask
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"commit-to-a-project-option/await-decision/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"present-sdp-review","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepCommitToAProjectOptionP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputCommitToAProjectOptionP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "commit-to-a-project-option-P1", 1)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "commit-to-a-project-option-P1", 1, out, err)
}

// stepCommitToAProjectOptionP1S2 — Commit to a Project Option — [commit] · [no], step 2: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":2,"node":"review-options","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"commit-to-a-project-option/review-options/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"decision","value":"{\"decision\":1,\"optionId\":\"opt-normal\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-review-committed","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":2,\"PhaseName\":\"construction\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepCommitToAProjectOptionP1S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputCommitToAProjectOptionP1S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "commit-to-a-project-option-P1", 2)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "commit-to-a-project-option-P1", 2, out, err)
}

// stepCommitToAProjectOptionP2S1 — Commit to a Project Option — [commit] · [yes], step 1: DispatchActivityTask
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"commit-to-a-project-option/await-decision/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"present-sdp-review","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepCommitToAProjectOptionP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputCommitToAProjectOptionP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "commit-to-a-project-option-P2", 1)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "commit-to-a-project-option-P2", 1, out, err)
}

// stepCommitToAProjectOptionP2S2 — Commit to a Project Option — [commit] · [yes], step 2: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":2,"node":"review-options","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"commit-to-a-project-option/review-options/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"decision","value":"{\"decision\":1,\"optionId\":\"opt-normal\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-review-committed","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":2,\"PhaseName\":\"construction\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepCommitToAProjectOptionP2S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputCommitToAProjectOptionP2S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "commit-to-a-project-option-P2", 2)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "commit-to-a-project-option-P2", 2, out, err)
}

// stepCommitToAProjectOptionP2S3 — Commit to a Project Option — [commit] · [yes], step 3: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":3,"node":"revoke","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"commit-to-a-project-option/revoke/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"decision","value":"{\"decision\":3}"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-review-revoked","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":1,\"PhaseName\":\"project-design\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepCommitToAProjectOptionP2S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputCommitToAProjectOptionP2S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "commit-to-a-project-option-P2", 3)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "commit-to-a-project-option-P2", 3, out, err)
}

// stepCommitToAProjectOptionP3S1 — Commit to a Project Option — [reject-all + feedback], step 1: DispatchActivityTask
// Projected stimulus (JSON): {"seq":1,"node":"await-decision","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"commit-to-a-project-option/await-decision/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"present-sdp-review","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepCommitToAProjectOptionP3S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputCommitToAProjectOptionP3S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "commit-to-a-project-option-P3", 1)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "commit-to-a-project-option-P3", 1, out, err)
}

// stepCommitToAProjectOptionP3S2 — Commit to a Project Option — [reject-all + feedback], step 2: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":2,"node":"review-options","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"commit-to-a-project-option/review-options/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"decision","value":"{\"decision\":2}"},{"name":"feedback","value":"{\"notes\":\"Reject all options: each overruns the 9-month window; re-plan with a 3-developer cap and the billing integration deferred.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-review-rejected","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":1,\"PhaseName\":\"project-design\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepCommitToAProjectOptionP3S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputCommitToAProjectOptionP3S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "commit-to-a-project-option-P3", 2)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "commit-to-a-project-option-P3", 2, out, err)
}

// stepDownloadGeneratedSourceCodeP1S1 — Download Generated Source Code — [clone or pull], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"open-repo","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"download-generated-source-code/open-repo/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"activityId\":\"C-review-engine\"}"}],"expect":{"result":"{\"kind\":\"session\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepDownloadGeneratedSourceCodeP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputDownloadGeneratedSourceCodeP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "download-generated-source-code-P1", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "download-generated-source-code-P1", 1, out, err)
}

// stepDownloadGeneratedSourceCodeP2S1 — Download Generated Source Code — [archive], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"open-repo","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"download-generated-source-code/open-repo/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"activityId\":\"C-review-engine\"}"}],"expect":{"result":"{\"kind\":\"session\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepDownloadGeneratedSourceCodeP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputDownloadGeneratedSourceCodeP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "download-generated-source-code-P2", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "download-generated-source-code-P2", 1, out, err)
}

// stepExecuteAProjectActivityP1S1 — Execute a Project Activity — dispatch · yes, step 1: ExecuteNextActivity
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"deliverymanager","op":"ExecuteNextActivity","call":"execute-a-project-activity/pump-fires/0","via":["scheduler-client","web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"tickID","value":"dct-dm-exec-P1-tick-0001"}],"expect":{"result":"{\"dispatched\":true}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P1", 1)
	out, err := subject.ExecuteNextActivity(callContext(t, h, ""), in.ProjectID, in.TickID)
	return settle(t, h, subject, "execute-a-project-activity-P1", 1, out, err)
}

// stepExecuteAProjectActivityP1S2 — Execute a Project Activity — dispatch · yes, step 2: QueryActivityView
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryActivityView","call":"execute-a-project-activity/activity-eligible/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"type\":\"service\"}","errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP1S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP1S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P1", 2)
	out, err := subject.QueryActivityView(callContext(t, h, ""), in.ProjectID, in.ActivityID)
	return settle(t, h, subject, "execute-a-project-activity-P1", 2, out, err)
}

// stepExecuteAProjectActivityP2S1 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 1: ExecuteNextActivity
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"deliverymanager","op":"ExecuteNextActivity","call":"execute-a-project-activity/pump-fires/0","via":["scheduler-client","web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"tickID","value":"dct-dm-exec-P2-tick-0001"}],"expect":{"result":"{\"dispatched\":true}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P2", 1)
	out, err := subject.ExecuteNextActivity(callContext(t, h, ""), in.ProjectID, in.TickID)
	return settle(t, h, subject, "execute-a-project-activity-P2", 1, out, err)
}

// stepExecuteAProjectActivityP2S2 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 2: QueryActivityView
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryActivityView","call":"execute-a-project-activity/activity-eligible/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"type\":\"service\"}","errorExpected":false},"probes":[{"for":"escalate-operator","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"failed\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP2S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP2S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P2", 2)
	out, err := subject.QueryActivityView(callContext(t, h, ""), in.ProjectID, in.ActivityID)
	return settle(t, h, subject, "execute-a-project-activity-P2", 2, out, err)
}

// stepExecuteAProjectActivityP2S3 — Execute a Project Activity — dispatch · no · no: exhausted · yes, step 3: OverrideActivity
// Projected stimulus (JSON): {"seq":3,"node":"operator-steers","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"OverrideActivity","call":"execute-a-project-activity/operator-steers/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"OverrideActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"},{"name":"override","value":"{\"kind\":2,\"notes\":\"Retry: the last attempt failed on a flaky CI runner, not on the code; rerun with the change unchanged.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP2S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP2S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P2", 3)
	out, err := any(nil), subject.OverrideActivity(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.Override)
	return settle(t, h, subject, "execute-a-project-activity-P2", 3, out, err)
}

// stepExecuteAProjectActivityP3S1 — Execute a Project Activity — review · agents only · approve, step 1: ExecuteNextActivity
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"deliverymanager","op":"ExecuteNextActivity","call":"execute-a-project-activity/pump-fires/0","via":["scheduler-client","web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"tickID","value":"dct-dm-exec-P3-tick-0001"}],"expect":{"result":"{\"dispatched\":true}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP3S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP3S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P3", 1)
	out, err := subject.ExecuteNextActivity(callContext(t, h, ""), in.ProjectID, in.TickID)
	return settle(t, h, subject, "execute-a-project-activity-P3", 1, out, err)
}

// stepExecuteAProjectActivityP3S2 — Execute a Project Activity — review · agents only · approve, step 2: QueryActivityView
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryActivityView","call":"execute-a-project-activity/activity-eligible/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"type\":\"service\"}","errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP3S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP3S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P3", 2)
	out, err := subject.QueryActivityView(callContext(t, h, ""), in.ProjectID, in.ActivityID)
	return settle(t, h, subject, "execute-a-project-activity-P3", 2, out, err)
}

// stepExecuteAProjectActivityP4S1 — Execute a Project Activity — review · human required · approve, step 1: ExecuteNextActivity
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"deliverymanager","op":"ExecuteNextActivity","call":"execute-a-project-activity/pump-fires/0","via":["scheduler-client","web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"tickID","value":"dct-dm-exec-P4-tick-0001"}],"expect":{"result":"{\"dispatched\":true}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP4S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP4S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P4", 1)
	out, err := subject.ExecuteNextActivity(callContext(t, h, ""), in.ProjectID, in.TickID)
	return settle(t, h, subject, "execute-a-project-activity-P4", 1, out, err)
}

// stepExecuteAProjectActivityP4S2 — Execute a Project Activity — review · human required · approve, step 2: QueryActivityView
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryActivityView","call":"execute-a-project-activity/activity-eligible/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"type\":\"service\"}","errorExpected":false},"probes":[{"for":"requires-human","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"awaitingHuman\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP4S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP4S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P4", 2)
	out, err := subject.QueryActivityView(callContext(t, h, ""), in.ProjectID, in.ActivityID)
	return settle(t, h, subject, "execute-a-project-activity-P4", 2, out, err)
}

// stepExecuteAProjectActivityP4S3 — Execute a Project Activity — review · human required · approve, step 3: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":3,"node":"human-decision","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"execute-a-project-activity/human-decision/3","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"},{"name":"taskID","value":"codeReview"},{"name":"decision","value":"{\"decision\":1}"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP4S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP4S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P4", 3)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "execute-a-project-activity-P4", 3, out, err)
}

// stepExecuteAProjectActivityP5S1 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 1: ExecuteNextActivity
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"deliverymanager","op":"ExecuteNextActivity","call":"execute-a-project-activity/pump-fires/0","via":["scheduler-client","web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"tickID","value":"dct-dm-exec-P5-tick-0001"}],"expect":{"result":"{\"dispatched\":true}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP5S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP5S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P5", 1)
	out, err := subject.ExecuteNextActivity(callContext(t, h, ""), in.ProjectID, in.TickID)
	return settle(t, h, subject, "execute-a-project-activity-P5", 1, out, err)
}

// stepExecuteAProjectActivityP5S2 — Execute a Project Activity — dispatch · no · yes: retry · yes, step 2: QueryActivityView
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryActivityView","call":"execute-a-project-activity/activity-eligible/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"type\":\"service\"}","errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP5S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP5S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P5", 2)
	out, err := subject.QueryActivityView(callContext(t, h, ""), in.ProjectID, in.ActivityID)
	return settle(t, h, subject, "execute-a-project-activity-P5", 2, out, err)
}

// stepExecuteAProjectActivityP6S1 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 1: ExecuteNextActivity
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"deliverymanager","op":"ExecuteNextActivity","call":"execute-a-project-activity/pump-fires/0","via":["scheduler-client","web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"tickID","value":"dct-dm-exec-P6-tick-0001"}],"expect":{"result":"{\"dispatched\":true}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP6S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP6S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P6", 1)
	out, err := subject.ExecuteNextActivity(callContext(t, h, ""), in.ProjectID, in.TickID)
	return settle(t, h, subject, "execute-a-project-activity-P6", 1, out, err)
}

// stepExecuteAProjectActivityP6S2 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 2: QueryActivityView
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryActivityView","call":"execute-a-project-activity/activity-eligible/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"type\":\"service\"}","errorExpected":false},"probes":[{"for":"escalate-operator","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"failed\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP6S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP6S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P6", 2)
	out, err := subject.QueryActivityView(callContext(t, h, ""), in.ProjectID, in.ActivityID)
	return settle(t, h, subject, "execute-a-project-activity-P6", 2, out, err)
}

// stepExecuteAProjectActivityP6S3 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 3: OverrideActivity
// Projected stimulus (JSON): {"seq":3,"node":"operator-steers","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"OverrideActivity","call":"execute-a-project-activity/operator-steers/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"OverrideActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"},{"name":"override","value":"{\"kind\":2,\"notes\":\"Retry: the last attempt failed on a flaky CI runner, not on the code; rerun with the change unchanged.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"escalate-operator","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"failed\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP6S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP6S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P6", 3)
	out, err := any(nil), subject.OverrideActivity(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.Override)
	return settle(t, h, subject, "execute-a-project-activity-P6", 3, out, err)
}

// stepExecuteAProjectActivityP6S4 — Execute a Project Activity — dispatch · no · no: exhausted · no · yes, step 4: OverrideActivity
// Projected stimulus (JSON): {"seq":4,"node":"operator-steers","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"OverrideActivity","call":"execute-a-project-activity/operator-steers/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"OverrideActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"},{"name":"override","value":"{\"kind\":2,\"notes\":\"Retry again: pin the integration test's Temporal namespace; the earlier failure was a namespace collision.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP6S4(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP6S4) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P6", 4)
	out, err := any(nil), subject.OverrideActivity(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.Override)
	return settle(t, h, subject, "execute-a-project-activity-P6", 4, out, err)
}

// stepExecuteAProjectActivityP7S1 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 1: ExecuteNextActivity
// Projected stimulus (JSON): {"seq":1,"node":"pump-fires","input":{"kind":"timeEvent","from":"scheduler-client","to":"deliverymanager","op":"ExecuteNextActivity","call":"execute-a-project-activity/pump-fires/0","via":["scheduler-client","web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"ExecuteNextActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"tickID","value":"dct-dm-exec-P7-tick-0001"}],"expect":{"result":"{\"dispatched\":true}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP7S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP7S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P7", 1)
	out, err := subject.ExecuteNextActivity(callContext(t, h, ""), in.ProjectID, in.TickID)
	return settle(t, h, subject, "execute-a-project-activity-P7", 1, out, err)
}

// stepExecuteAProjectActivityP7S2 — Execute a Project Activity — review · agents only · sendBack: reopens the judged task as revision n+1 · dispatch · yes, step 2: QueryActivityView
// Projected stimulus (JSON): {"seq":2,"node":"activity-eligible","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryActivityView","call":"execute-a-project-activity/activity-eligible/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"type\":\"service\"}","errorExpected":false},"probes":[{"for":"activity-exit-recorded","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"done\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepExecuteAProjectActivityP7S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputExecuteAProjectActivityP7S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "execute-a-project-activity-P7", 2)
	out, err := subject.QueryActivityView(callContext(t, h, ""), in.ProjectID, in.ActivityID)
	return settle(t, h, subject, "execute-a-project-activity-P7", 2, out, err)
}

// stepManageProjectsP1S1 — Manage Projects — [no: self-operated], step 1: StartProject
// Projected stimulus (JSON): {"seq":1,"node":"submit-create","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"StartProject","call":"manage-projects/submit-create/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StartProject","inputs":[{"name":"owner","value":"dct-owner"},{"name":"name","value":"dct-gtd-app"},{"name":"model","value":"selfOperated"},{"name":"start","value":"false"}],"expect":{"result":"{\"projectId\":\"dct-gtd-app\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepManageProjectsP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputManageProjectsP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "manage-projects-P1", 1)
	out, err := subject.StartProject(callContext(t, h, ""), in.Owner, in.Name, in.ProjectID, in.Model, in.Research, in.Start)
	return settle(t, h, subject, "manage-projects-P1", 1, out, err)
}

// stepManageProjectsP1S2 — Manage Projects — [no: self-operated], step 2: QueryProjectView
// Projected stimulus (JSON): {"seq":2,"node":"catalog-open","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"manage-projects/catalog-open/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-gtd-app\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-gtd-app\",\"Name\":\"dct-gtd-app\",\"Owner\":\"dct-owner\",\"Phase\":0,\"operatingModel\":\"selfOperated\"}}","errorExpected":false},"probes":[{"for":"obj-project-head-state-created","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"projects\",\"owner\":\"dct-owner\"}"}],"expect":{"result":"{\"kind\":\"projects\",\"projects\":[{\"ProjectID\":\"dct-gtd-app\",\"Name\":\"dct-gtd-app\",\"Owner\":\"dct-owner\"}]}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepManageProjectsP1S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputManageProjectsP1S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "manage-projects-P1", 2)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "manage-projects-P1", 2, out, err)
}

// stepManageProjectsP1S3 — Manage Projects — [no: self-operated], step 3: SetProjectExecutionPolicy
// Projected stimulus (JSON): {"seq":3,"node":"set-review-policy","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SetProjectExecutionPolicy","call":"manage-projects/set-review-policy/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"SetProjectExecutionPolicy","inputs":[{"name":"projectID","value":"dct-gtd-app"},{"name":"policy","value":"{\"preset\":\"checkpoints\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-review-policy-set","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-gtd-app\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"reviewPolicy\":{\"preset\":\"checkpoints\"}}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepManageProjectsP1S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputManageProjectsP1S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "manage-projects-P1", 3)
	out, err := any(nil), subject.SetProjectExecutionPolicy(callContext(t, h, ""), in.ProjectID, in.Policy)
	return settle(t, h, subject, "manage-projects-P1", 3, out, err)
}

// stepManageProjectsP1S4 — Manage Projects — [no: self-operated], step 4: StartProject
// Projected stimulus (JSON): {"seq":4,"node":"capture-research","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"StartProject","call":"manage-projects/capture-research/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"StartProject","inputs":[{"name":"owner","value":"dct-owner"},{"name":"name","value":"dct-gtd-app"},{"name":"projectID","value":"dct-gtd-app"},{"name":"research","value":"{\"sources\":[{\"title\":\"GTD method notes\",\"content\":\"Capture, clarify, organize, reflect, engage. Weekly review is mandatory.\"}]}"},{"name":"start","value":"true"}],"expect":{"result":"{\"projectId\":\"dct-gtd-app\"}","errorExpected":false},"probes":[{"for":"obj-research-input-captured","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-gtd-app\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-gtd-app\",\"Research\":{\"sources\":[{\"title\":\"GTD method notes\",\"contentBytes\":72}]}}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepManageProjectsP1S4(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputManageProjectsP1S4) (any, error) {
	t.Helper()
	arrange(t, h, subject, "manage-projects-P1", 4)
	out, err := subject.StartProject(callContext(t, h, ""), in.Owner, in.Name, in.ProjectID, in.Model, in.Research, in.Start)
	return settle(t, h, subject, "manage-projects-P1", 4, out, err)
}

// stepManageProjectsP2S1 — Manage Projects — [yes], step 1: StartProject
// Projected stimulus (JSON): {"seq":1,"node":"submit-create","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"StartProject","call":"manage-projects/submit-create/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"StartProject","inputs":[{"name":"owner","value":"dct-owner"},{"name":"name","value":"dct-gtd-app"},{"name":"model","value":"archistratorOperated"},{"name":"start","value":"false"}],"expect":{"result":"{\"projectId\":\"dct-gtd-app\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepManageProjectsP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputManageProjectsP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "manage-projects-P2", 1)
	out, err := subject.StartProject(callContext(t, h, ""), in.Owner, in.Name, in.ProjectID, in.Model, in.Research, in.Start)
	return settle(t, h, subject, "manage-projects-P2", 1, out, err)
}

// stepManageProjectsP2S2 — Manage Projects — [yes], step 2: QueryProjectView
// Projected stimulus (JSON): {"seq":2,"node":"catalog-open","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"manage-projects/catalog-open/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-gtd-app\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-gtd-app\",\"Name\":\"dct-gtd-app\",\"Owner\":\"dct-owner\",\"Phase\":0,\"operatingModel\":\"archistratorOperated\"}}","errorExpected":false},"probes":[{"for":"obj-project-head-state-created","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"projects\",\"owner\":\"dct-owner\"}"}],"expect":{"result":"{\"kind\":\"projects\",\"projects\":[{\"ProjectID\":\"dct-gtd-app\",\"Name\":\"dct-gtd-app\",\"Owner\":\"dct-owner\"}]}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepManageProjectsP2S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputManageProjectsP2S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "manage-projects-P2", 2)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "manage-projects-P2", 2, out, err)
}

// stepManageProjectsP2S3 — Manage Projects — [yes], step 3: SetProjectExecutionPolicy
// Projected stimulus (JSON): {"seq":3,"node":"set-review-policy","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SetProjectExecutionPolicy","call":"manage-projects/set-review-policy/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"SetProjectExecutionPolicy","inputs":[{"name":"projectID","value":"dct-gtd-app"},{"name":"policy","value":"{\"preset\":\"full\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-review-policy-set","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-gtd-app\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"reviewPolicy\":{\"preset\":\"full\"}}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepManageProjectsP2S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputManageProjectsP2S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "manage-projects-P2", 3)
	out, err := any(nil), subject.SetProjectExecutionPolicy(callContext(t, h, ""), in.ProjectID, in.Policy)
	return settle(t, h, subject, "manage-projects-P2", 3, out, err)
}

// stepManageProjectsP2S4 — Manage Projects — [yes], step 4: StartProject
// Projected stimulus (JSON): {"seq":4,"node":"capture-research","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"StartProject","call":"manage-projects/capture-research/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"StartProject","inputs":[{"name":"owner","value":"dct-owner"},{"name":"name","value":"dct-gtd-app"},{"name":"projectID","value":"dct-gtd-app"},{"name":"research","value":"{\"sources\":[{\"title\":\"GTD method notes\",\"content\":\"Capture, clarify, organize, reflect, engage. Weekly review is mandatory.\"}]}"},{"name":"start","value":"true"}],"expect":{"result":"{\"projectId\":\"dct-gtd-app\"}","errorExpected":false},"probes":[{"for":"obj-research-input-captured","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-gtd-app\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-gtd-app\",\"Research\":{\"sources\":[{\"title\":\"GTD method notes\",\"contentBytes\":72}]}}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepManageProjectsP2S4(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputManageProjectsP2S4) (any, error) {
	t.Helper()
	arrange(t, h, subject, "manage-projects-P2", 4)
	out, err := subject.StartProject(callContext(t, h, ""), in.Owner, in.Name, in.ProjectID, in.Model, in.Research, in.Start)
	return settle(t, h, subject, "manage-projects-P2", 4, out, err)
}

// stepPauseConstructionP1S1 — Pause Construction, step 1: SetProjectRunState
// Projected stimulus (JSON): {"seq":1,"node":"choose-pause","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SetProjectRunState","call":"pause-construction/choose-pause/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":["Pause is setProjectRunState(projectId, paused, reason): the same Manager entry as resume, with the literal 'paused'; reason must be non-empty (Go-side guard). The request is accepted at once and applied asynchronously, which is why Resume asks whether the pause is fully applied. MCP and web surfaces bind to the same Manager entry."]}
// Binding (JSON): {"seq":1,"operation":"SetProjectRunState","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"runState","value":"paused"},{"name":"reason","value":"Holding construction while the billing vendor contract is renegotiated."}],"expect":{"errorExpected":false},"probes":[{"for":"run-state-paused","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"operatorPaused\":true,\"pauseReason\":\"Holding construction while the billing vendor contract is renegotiated.\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepPauseConstructionP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputPauseConstructionP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "pause-construction-P1", 1)
	out, err := any(nil), subject.SetProjectRunState(callContext(t, h, ""), in.ProjectID, in.RunState, in.Reason)
	return settle(t, h, subject, "pause-construction-P1", 1, out, err)
}

// stepReplanUnderScopeChangeP1S1 — Replan Under Scope Change — [yes], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"snapshot","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"replan-under-scope-change/snapshot/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":2,\"PhaseName\":\"construction\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P1", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "replan-under-scope-change-P1", 1, out, err)
}

// stepReplanUnderScopeChangeP1S2 — Replan Under Scope Change — [yes], step 2: DispatchActivityTask
// Projected stimulus (JSON): {"seq":2,"node":"back-to-design","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"replan-under-scope-change/back-to-design/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureDraft"},{"name":"feedback","value":"{\"notes\":\"Architecture change: split billing into metering and invoicing components; this is not a scope change.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"system-amendment-open","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP1S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP1S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P1", 2)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "replan-under-scope-change-P1", 2, out, err)
}

// stepReplanUnderScopeChangeP2S1 — Replan Under Scope Change — [no: scope change] · [accept], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"snapshot","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"replan-under-scope-change/snapshot/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":2,\"PhaseName\":\"construction\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P2", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "replan-under-scope-change-P2", 1, out, err)
}

// stepReplanUnderScopeChangeP2S2 — Replan Under Scope Change — [no: scope change] · [accept], step 2: DispatchActivityTask
// Projected stimulus (JSON): {"seq":2,"node":"reenter","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"replan-under-scope-change/reenter/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"feedback","value":"{\"notes\":\"Scope change: add the audit-export use case; re-plan from the activity list against the current committed network.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP2S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP2S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P2", 2)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "replan-under-scope-change-P2", 2, out, err)
}

// stepReplanUnderScopeChangeP2S3 — Replan Under Scope Change — [no: scope change] · [accept], step 3: DispatchActivityTask
// Projected stimulus (JSON): {"seq":3,"node":"present","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"replan-under-scope-change/present/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-revision-staged","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP2S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP2S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P2", 3)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "replan-under-scope-change-P2", 3, out, err)
}

// stepReplanUnderScopeChangeP2S4 — Replan Under Scope Change — [no: scope change] · [accept], step 4: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":4,"node":"mgmt","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"replan-under-scope-change/mgmt/2","guard":"[accept]","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":["Guard-to-wire mapping for binding: [accept] = submitReviewDecision{decision: approve}; [reject] = {decision: reject}."]}
// Binding (JSON): {"seq":4,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"decision","value":"{\"decision\":1,\"optionId\":\"opt-normal\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-revision-adopted","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":2,\"PhaseName\":\"construction\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP2S4(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP2S4) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P2", 4)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "replan-under-scope-change-P2", 4, out, err)
}

// stepReplanUnderScopeChangeP3S1 — Replan Under Scope Change — [no: scope change] · [reject], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"snapshot","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"replan-under-scope-change/snapshot/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":2,\"PhaseName\":\"construction\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP3S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP3S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P3", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "replan-under-scope-change-P3", 1, out, err)
}

// stepReplanUnderScopeChangeP3S2 — Replan Under Scope Change — [no: scope change] · [reject], step 2: DispatchActivityTask
// Projected stimulus (JSON): {"seq":2,"node":"reenter","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"replan-under-scope-change/reenter/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"feedback","value":"{\"notes\":\"Scope change: add the audit-export use case; re-plan from the activity list against the current committed network.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP3S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP3S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P3", 2)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "replan-under-scope-change-P3", 2, out, err)
}

// stepReplanUnderScopeChangeP3S3 — Replan Under Scope Change — [no: scope change] · [reject], step 3: DispatchActivityTask
// Projected stimulus (JSON): {"seq":3,"node":"present","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"DispatchActivityTask","call":"replan-under-scope-change/present/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"DispatchActivityTask","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-revision-staged","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"}],"expect":{"result":"{\"activityId\":\"projectDesign\",\"state\":\"awaitingHuman\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP3S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP3S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P3", 3)
	out, err := subject.DispatchActivityTask(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Feedback)
	return settle(t, h, subject, "replan-under-scope-change-P3", 3, out, err)
}

// stepReplanUnderScopeChangeP3S4 — Replan Under Scope Change — [no: scope change] · [reject], step 4: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":4,"node":"mgmt","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"replan-under-scope-change/mgmt/2","guard":"[reject]","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":["Guard-to-wire mapping for binding: [accept] = submitReviewDecision{decision: approve}; [reject] = {decision: reject}."]}
// Binding (JSON): {"seq":4,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"projectDesign"},{"name":"taskID","value":"sdpReview"},{"name":"decision","value":"{\"decision\":2}"},{"name":"feedback","value":"{\"notes\":\"Reject the revision: the added cost exceeds the approved ceiling; the current commitment stands.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"sdp-revision-rejected","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"Phase\":2,\"PhaseName\":\"construction\"}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepReplanUnderScopeChangeP3S4(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputReplanUnderScopeChangeP3S4) (any, error) {
	t.Helper()
	arrange(t, h, subject, "replan-under-scope-change-P3", 4)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "replan-under-scope-change-P3", 4, out, err)
}

// stepRequeueAFailedConstructionActivityP1S1 — Requeue a Failed Construction Activity, step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"read-attempts","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"requeue-a-failed-construction-activity/read-attempts/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"episodes\",\"projectId\":\"dct-owner/dct-delivery\",\"activityId\":\"C-review-engine\"}"}],"expect":{"result":"{\"kind\":\"episodes\"}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepRequeueAFailedConstructionActivityP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputRequeueAFailedConstructionActivityP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "requeue-a-failed-construction-activity-P1", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "requeue-a-failed-construction-activity-P1", 1, out, err)
}

// stepRequeueAFailedConstructionActivityP1S2 — Requeue a Failed Construction Activity, step 2: QueryProjectView
// Projected stimulus (JSON): {"seq":2,"node":"read-failed-trace","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"requeue-a-failed-construction-activity/read-failed-trace/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"timeline\",\"projectId\":\"dct-owner/dct-delivery\",\"episodeId\":\"dct-ep-review-engine-0003\"}"}],"expect":{"result":"{\"kind\":\"timeline\",\"timeline\":{\"record\":{\"outcome\":1}}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepRequeueAFailedConstructionActivityP1S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputRequeueAFailedConstructionActivityP1S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "requeue-a-failed-construction-activity-P1", 2)
	in.Query.EpisodeID = resolveEpisodeID(t, h, subject, in.Query.EpisodeID)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "requeue-a-failed-construction-activity-P1", 2, out, err)
}

// stepRequeueAFailedConstructionActivityP1S3 — Requeue a Failed Construction Activity, step 3: OverrideActivity
// Projected stimulus (JSON): {"seq":3,"node":"write-change","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"OverrideActivity","call":"requeue-a-failed-construction-activity/write-change/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"OverrideActivity","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"},{"name":"override","value":"{\"kind\":2,\"notes\":\"Re-queue: the construction attempt failed because the contract's ReviewSet result changed; regenerate the fake and rerun from the first incomplete phase.\",\"comments\":null}"}],"expect":{"errorExpected":false},"probes":[{"for":"activity-resumed-state","component":"deliveryManager","operation":"QueryActivityView","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"C-review-engine"}],"expect":{"result":"{\"activityId\":\"C-review-engine\",\"state\":\"running\"}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepRequeueAFailedConstructionActivityP1S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputRequeueAFailedConstructionActivityP1S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "requeue-a-failed-construction-activity-P1", 3)
	out, err := any(nil), subject.OverrideActivity(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.Override)
	return settle(t, h, subject, "requeue-a-failed-construction-activity-P1", 3, out, err)
}

// stepResumePausedConstructionP1S1 — Resume Paused Construction — [yes], step 1: SetProjectRunState
// Projected stimulus (JSON): {"seq":1,"node":"choose-resume","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SetProjectRunState","call":"resume-paused-construction/choose-resume/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":["Resume is setProjectRunState(projectId, running, reason): the run-state value is the literal 'running' and reason must be non-empty (Go-side non-emptiness guard); MCP and web surfaces bind to the same Manager entry."]}
// Binding (JSON): {"seq":1,"operation":"SetProjectRunState","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"runState","value":"running"},{"name":"reason","value":"Vendor contract signed; resume construction."}],"expect":{"errorExpected":false},"probes":[{"for":"run-state-running","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"operatorPaused\":false}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepResumePausedConstructionP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputResumePausedConstructionP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "resume-paused-construction-P1", 1)
	out, err := any(nil), subject.SetProjectRunState(callContext(t, h, ""), in.ProjectID, in.RunState, in.Reason)
	return settle(t, h, subject, "resume-paused-construction-P1", 1, out, err)
}

// stepResumePausedConstructionP2S1 — Resume Paused Construction — [no], step 1: SetProjectRunState
// Projected stimulus (JSON): {"seq":1,"node":"choose-resume","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SetProjectRunState","call":"resume-paused-construction/choose-resume/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":["Resume is setProjectRunState(projectId, running, reason): the run-state value is the literal 'running' and reason must be non-empty (Go-side non-emptiness guard); MCP and web surfaces bind to the same Manager entry."]}
// Binding (JSON): {"seq":1,"operation":"SetProjectRunState","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"runState","value":"running"},{"name":"reason","value":"Vendor contract signed; resume construction."}],"expect":{"errorExpected":true,"errorCode":"FailedPrecondition"},"probes":[{"for":"run-state-still-paused","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"operatorPaused\":true}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepResumePausedConstructionP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputResumePausedConstructionP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "resume-paused-construction-P2", 1)
	out, err := any(nil), subject.SetProjectRunState(callContext(t, h, ""), in.ProjectID, in.RunState, in.Reason)
	return settle(t, h, subject, "resume-paused-construction-P2", 1, out, err)
}

// stepSendBackChangeRequestsForARedraftP1S1 — Send Back Change Requests for a Redraft — [yes], step 1: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":1,"node":"send-back","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"send-back-change-requests-for-a-redraft/send-back/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":[{"node":"obj-draft-sent-back","object":"Staged draft","inState":"[rejected: open change requests gate approve]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"decision","value":"{\"decision\":2}"},{"name":"feedback","value":"{\"notes\":\"Change requests before approval.\",\"comments\":[{\"jsonPath\":\"$.containers[3].name\",\"text\":\"Rename to the volatility it encapsulates.\",\"anchorText\":\"BillingThing\",\"replyTo\":\"\"}]}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-draft-sent-back","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"reviewThread\":[{\"anchor\":\"$.containers[3].name\",\"anchorText\":\"BillingThing\",\"status\":\"open\"}]}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepSendBackChangeRequestsForARedraftP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputSendBackChangeRequestsForARedraftP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "send-back-change-requests-for-a-redraft-P1", 1)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "send-back-change-requests-for-a-redraft-P1", 1, out, err)
}

// stepSendBackChangeRequestsForARedraftP1S2 — Send Back Change Requests for a Redraft — [yes], step 2: QueryProjectView
// Projected stimulus (JSON): {"seq":2,"node":"re-review","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"send-back-change-requests-for-a-redraft/re-review/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepSendBackChangeRequestsForARedraftP1S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputSendBackChangeRequestsForARedraftP1S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "send-back-change-requests-for-a-redraft-P1", 2)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "send-back-change-requests-for-a-redraft-P1", 2, out, err)
}

// stepSendBackChangeRequestsForARedraftP1S3 — Send Back Change Requests for a Redraft — [yes], step 3: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":3,"node":"close-comments","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"send-back-change-requests-for-a-redraft/close-comments/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":3,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"decision","value":"{\"decision\":5,\"commentId\":\"dct-cr-0001\",\"commentStatus\":\"resolved\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-thread-resolved","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"reviewThread\":[{\"anchor\":\"$.containers[3].name\",\"status\":\"resolved\"}]}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepSendBackChangeRequestsForARedraftP1S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputSendBackChangeRequestsForARedraftP1S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "send-back-change-requests-for-a-redraft-P1", 3)
	in.Decision.CommentID = resolveCommentID(t, h, subject, delivery.ArtifactKind(5), in.Decision.CommentID)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "send-back-change-requests-for-a-redraft-P1", 3, out, err)
}

// stepSendBackChangeRequestsForARedraftP2S1 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 1: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":1,"node":"send-back","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"send-back-change-requests-for-a-redraft/send-back/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":[{"node":"obj-draft-sent-back","object":"Staged draft","inState":"[rejected: open change requests gate approve]"}],"hints":null}
// Binding (JSON): {"seq":1,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"decision","value":"{\"decision\":2}"},{"name":"feedback","value":"{\"notes\":\"Change requests before approval.\",\"comments\":[{\"jsonPath\":\"$.containers[3].name\",\"text\":\"Rename to the volatility it encapsulates.\",\"anchorText\":\"BillingThing\",\"replyTo\":\"\"}]}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-draft-sent-back","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"reviewThread\":[{\"anchor\":\"$.containers[3].name\",\"anchorText\":\"BillingThing\",\"status\":\"open\"}]}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepSendBackChangeRequestsForARedraftP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputSendBackChangeRequestsForARedraftP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 1)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 1, out, err)
}

// stepSendBackChangeRequestsForARedraftP2S2 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 2: QueryProjectView
// Projected stimulus (JSON): {"seq":2,"node":"re-review","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"send-back-change-requests-for-a-redraft/re-review/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":2,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepSendBackChangeRequestsForARedraftP2S2(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputSendBackChangeRequestsForARedraftP2S2) (any, error) {
	t.Helper()
	arrange(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 2)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 2, out, err)
}

// stepSendBackChangeRequestsForARedraftP2S3 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 3: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":3,"node":"send-back","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"send-back-change-requests-for-a-redraft/send-back/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":[{"node":"obj-draft-sent-back","object":"Staged draft","inState":"[rejected: open change requests gate approve]"}],"hints":null}
// Binding (JSON): {"seq":3,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"decision","value":"{\"decision\":2}"},{"name":"feedback","value":"{\"notes\":\"Second round: one request is not resolved.\",\"comments\":[{\"jsonPath\":\"$.containers[3].responsibilities\",\"text\":\"The response renamed it but still lists invoicing; move invoicing out.\",\"anchorText\":\"invoicing\",\"replyTo\":\"\"}]}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-draft-sent-back","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"reviewThread\":[{\"anchor\":\"$.containers[3].name\"},{\"anchor\":\"$.containers[3].responsibilities\",\"anchorText\":\"invoicing\",\"status\":\"open\"}]}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepSendBackChangeRequestsForARedraftP2S3(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputSendBackChangeRequestsForARedraftP2S3) (any, error) {
	t.Helper()
	arrange(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 3)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 3, out, err)
}

// stepSendBackChangeRequestsForARedraftP2S4 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 4: QueryProjectView
// Projected stimulus (JSON): {"seq":4,"node":"re-review","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"send-back-change-requests-for-a-redraft/re-review/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":4,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepSendBackChangeRequestsForARedraftP2S4(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputSendBackChangeRequestsForARedraftP2S4) (any, error) {
	t.Helper()
	arrange(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 4)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 4, out, err)
}

// stepSendBackChangeRequestsForARedraftP2S5 — Send Back Change Requests for a Redraft — [no: iterate] · [yes], step 5: SubmitReviewDecision
// Projected stimulus (JSON): {"seq":5,"node":"close-comments","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"SubmitReviewDecision","call":"send-back-change-requests-for-a-redraft/close-comments/0","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":5,"operation":"SubmitReviewDecision","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"},{"name":"activityID","value":"architecture"},{"name":"taskID","value":"architectureReview"},{"name":"decision","value":"{\"decision\":5,\"commentId\":\"dct-cr-0002\",\"commentStatus\":\"resolved\"}"}],"expect":{"errorExpected":false},"probes":[{"for":"obj-thread-resolved","component":"deliveryManager","operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"session\",\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5}"}],"expect":{"result":"{\"kind\":\"session\",\"session\":{\"projectId\":\"dct-owner/dct-delivery\",\"artifactKind\":5,\"reviewThread\":[{\"anchor\":\"$.containers[3].name\"},{\"anchor\":\"$.containers[3].responsibilities\",\"status\":\"resolved\"}]}}","errorExpected":false}}],"unobservable":null,"hook":true}
func stepSendBackChangeRequestsForARedraftP2S5(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputSendBackChangeRequestsForARedraftP2S5) (any, error) {
	t.Helper()
	arrange(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 5)
	in.Decision.CommentID = resolveCommentID(t, h, subject, delivery.ArtifactKind(5), in.Decision.CommentID)
	out, err := any(nil), subject.SubmitReviewDecision(callContext(t, h, ""), in.ProjectID, in.ActivityID, in.TaskID, in.Decision, in.Feedback)
	return settle(t, h, subject, "send-back-change-requests-for-a-redraft-P2", 5, out, err)
}

// stepTrackWeeklyProjectProgressP1S1 — Track Weekly Project Progress — [no] · [yes], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"week-elapses","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"track-weekly-project-progress/week-elapses/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"PhaseName\":\"construction\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepTrackWeeklyProjectProgressP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputTrackWeeklyProjectProgressP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "track-weekly-project-progress-P1", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "track-weekly-project-progress-P1", 1, out, err)
}

// stepTrackWeeklyProjectProgressP2S1 — Track Weekly Project Progress — [no] · [no], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"week-elapses","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"track-weekly-project-progress/week-elapses/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"PhaseName\":\"construction\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepTrackWeeklyProjectProgressP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputTrackWeeklyProjectProgressP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "track-weekly-project-progress-P2", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "track-weekly-project-progress-P2", 1, out, err)
}

// stepTrackWeeklyProjectProgressP3S1 — Track Weekly Project Progress — [yes] · [yes], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"week-elapses","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"track-weekly-project-progress/week-elapses/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"PhaseName\":\"construction\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepTrackWeeklyProjectProgressP3S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputTrackWeeklyProjectProgressP3S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "track-weekly-project-progress-P3", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "track-weekly-project-progress-P3", 1, out, err)
}

// stepTrackWeeklyProjectProgressP4S1 — Track Weekly Project Progress — [yes] · [no], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"week-elapses","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"track-weekly-project-progress/week-elapses/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\",\"PhaseName\":\"construction\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepTrackWeeklyProjectProgressP4S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputTrackWeeklyProjectProgressP4S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "track-weekly-project-progress-P4", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "track-weekly-project-progress-P4", 1, out, err)
}

// stepViewTheProjectStateLogP1S1 — View the Project State Log — [yes], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"open-history","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"view-the-project-state-log/open-history/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\"}}","errorExpected":false},"probes":null,"unobservable":null,"hook":true}
func stepViewTheProjectStateLogP1S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputViewTheProjectStateLogP1S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "view-the-project-state-log-P1", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "view-the-project-state-log-P1", 1, out, err)
}

// stepViewTheProjectStateLogP2S1 — View the Project State Log — [no], step 1: QueryProjectView
// Projected stimulus (JSON): {"seq":1,"node":"open-history","input":{"kind":"call","from":"web-client","to":"deliverymanager","op":"QueryProjectView","call":"view-the-project-state-log/open-history/2","via":["web-client","mcp-client"]},"expectedOutputs":null,"expectedStates":null,"hints":null}
// Binding (JSON): {"seq":1,"operation":"QueryProjectView","inputs":[{"name":"query","value":"{\"kind\":\"summary\",\"projectId\":\"dct-owner/dct-delivery\"}"}],"expect":{"result":"{\"kind\":\"summary\",\"summary\":{\"ProjectID\":\"dct-owner/dct-delivery\"}}","errorExpected":false},"probes":[{"for":"project-state-unchanged","component":"projectStateAccess","operation":"ReadProjectVersion","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"}],"expect":{"result":"7","errorExpected":false}}],"unobservable":null,"hook":true}
func stepViewTheProjectStateLogP2S1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager, in inputViewTheProjectStateLogP2S1) (any, error) {
	t.Helper()
	arrange(t, h, subject, "view-the-project-state-log-P2", 1)
	out, err := subject.QueryProjectView(callContext(t, h, ""), in.Query)
	return settle(t, h, subject, "view-the-project-state-log-P2", 1, out, err)
}

// probeViewTheProjectStateLogP2S1N1 — View the Project State Log — [no], step 1, probe 1: projectStateAccess.ReadProjectVersion proves project-state-unchanged.
// Probe (JSON): {"for":"project-state-unchanged","component":"projectStateAccess","operation":"ReadProjectVersion","inputs":[{"name":"projectID","value":"dct-owner/dct-delivery"}],"expect":{"result":"7","errorExpected":false}}
func probeViewTheProjectStateLogP2S1N1(t *testing.T, h *scenariohost.Host, subject delivery.DeliveryManager) (any, error) {
	t.Helper()
	return stackOf(t, subject).ps.ReadProjectVersion(h.ResourceAccessContext(t, "probe-1.1"), planProject)
}

// ---------------------------------------------------------------------------
// The testing gate on the integration CheckRun (agent task spec, Task B5)
//
// These two tests are not projected from the test plan: the plan binds no scenario whose
// venue records a deficient run, and the gate's hold is workflow code no public op returns.
// They drive the subject exactly as the scenarios do — the arrangement, one pump tick,
// the scripted venue — and observe it through its own read ops.
// ---------------------------------------------------------------------------

// TestTestingGateHoldsOnARunMissingAProjectedScenario: the venue records an integration
// run that never ran one projected scenario, so the testing gate holds the round for a
// human with ONE system comment anchored at that run.
func TestTestingGateHoldsOnARunMissingAProjectedScenario(t *testing.T) {
	h := scenariohost.Start(t)
	endHeldWorkflows(t, h)
	subject := newSubject(t, h)
	s := stackOf(t, subject)
	s.venue.mu.Lock()
	s.venue.narrow = true
	s.venue.mu.Unlock()
	s.serve()
	arrangeProject(t, h, subject, s, "")
	setPolicy(t, h, subject, vibes)
	if _, err := subject.ExecuteNextActivity(callContext(t, h, ""), planProject, "dct-dm-gate-hold-0001"); err != nil {
		t.Fatalf("tick the pump: %v", err)
	}
	view := awaitActivityState(t, h, subject, drivenActivity, delivery.ActivityViewAwaitingHuman)
	s.venue.mu.Lock()
	dropped := s.venue.dropped
	s.venue.mu.Unlock()
	if dropped == "" {
		t.Fatal("the venue recorded no narrowed integration run")
	}
	hold, ok := testingHold(view)
	if !ok {
		t.Fatalf("the testing gate holds no system comment: %+v", taskOf(view, "testing"))
	}
	if !strings.HasPrefix(hold.Anchor, "$.testingState.checkRuns[id=local-") ||
		!strings.Contains(hold.Text, "never ran projected scenario(s)") || !strings.Contains(hold.Text, dropped) {
		t.Fatalf("hold comment %q at %q, want one at the run naming the never-run %s", hold.Text, hold.Anchor, dropped)
	}
}

// TestTestingGatePassCitesTheJudgedRun: a complete green integration run at the judged
// attempt passes the gate, the testing pass cites that ONE run (evidence checkRun), and
// the taskSpec view of integration revision 1 — which reads the activity branch, where
// the run is published — shows it, recorded at the judged attempt (R15).
func TestTestingGatePassCitesTheJudgedRun(t *testing.T) {
	h := scenariohost.Start(t)
	endHeldWorkflows(t, h)
	subject := newSubject(t, h)
	s := stackOf(t, subject)
	s.serve()
	arrangeProject(t, h, subject, s, "")
	setPolicy(t, h, subject, vibes)
	if _, err := subject.ExecuteNextActivity(callContext(t, h, ""), planProject, "dct-dm-gate-pass-0001"); err != nil {
		t.Fatalf("tick the pump: %v", err)
	}
	awaitActivityState(t, h, subject, drivenActivity, delivery.ActivityViewDone)
	assertTaskSpecEnvelopes(t, s.venue.jobs(), "construct")
	proj, err := s.ps.ReadProject(h.ResourceAccessContext(t, "gate-pass-read"), planProject)
	if err != nil {
		t.Fatal(err)
	}
	judged, cited := judgedAndCited(t, proj.ActivityExecution[drivenActivity])
	if cited == "" || strings.Contains(cited, ",") {
		t.Fatalf("the testing pass cites %q, want exactly one run", cited)
	}
	pid, act, task, rev := planProject, drivenActivity, "integration", 1
	v, err := subject.QueryProjectView(callContext(t, h, ""), delivery.ProjectViewQuery{
		Kind: delivery.ProjectViewTaskSpec, ProjectID: &pid, ActivityID: &act, TaskID: &task, Revision: &rev})
	if err != nil {
		t.Fatal(err)
	}
	if v.TaskSpec == nil || v.TaskSpec.CheckRun == nil {
		t.Fatalf("integration revision 1 shows no run, want the cited run %s", cited)
	}
	run := v.TaskSpec.CheckRun
	if run.ID != cited || run.Revision != judged || run.Task != "integration" || v.TaskSpec.SpecChanged {
		t.Fatalf("integration revision 1 shows run %s at %q (task %s, specChanged %v), want the cited run %s at the judged attempt %q",
			run.ID, run.Revision, run.Task, v.TaskSpec.SpecChanged, cited, judged)
	}
}

// TestEveryDesignDispatchCarriesItsTaskSpecEnvelope (R41): a design draft the subject
// dispatches carries the whole task-spec envelope — activity, task, spec hash and the
// attempt it runs under — so the local executor holds it to its spec rather than running
// it ungated. The construction arm is pinned by TestTestingGatePassCitesTheJudgedRun.
func TestEveryDesignDispatchCarriesItsTaskSpecEnvelope(t *testing.T) {
	h := scenariohost.Start(t)
	endHeldWorkflows(t, h, "architecture")
	subject := newSubject(t, h)
	s := stackOf(t, subject)
	s.serve()
	arrangeProject(t, h, subject, s, "architecture")
	assertTaskSpecEnvelopes(t, s.venue.jobs(), "draft")
}

// assertTaskSpecEnvelopes fails t unless jobs hold at least one job of mode, and every
// job but an answer job (ruling R30) was dispatched with the whole task-spec envelope.
func assertTaskSpecEnvelopes(t *testing.T, jobs []agentJob, mode string) {
	t.Helper()
	seen := false
	for _, j := range jobs {
		seen = seen || j.Mode == mode
		if j.Mode == "answer" {
			continue
		}
		hash := j.env["AIARCH_SPEC_HASH"]
		if j.env["AIARCH_ACTIVITY_ID"] == "" || j.env["AIARCH_TASK_ID"] == "" || j.env["AIARCH_REVISION"] == "" ||
			len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
			t.Errorf("the %s %s dispatch of %s carries envelope activity=%q task=%q hash=%q revision=%q, want all four",
				j.Mode, j.Command, j.Activity, j.env["AIARCH_ACTIVITY_ID"], j.env["AIARCH_TASK_ID"], hash, j.env["AIARCH_REVISION"])
		}
	}
	if !seen {
		t.Errorf("no %s job was dispatched among %d", mode, len(jobs))
	}
}

// judgedAndCited is the row's passed integration attempt and the run its passed testing
// attempt cites, failing t when the testing pass cites anything but a check run.
func judgedAndCited(t *testing.T, row projectstate.ActivityExecution) (judged, cited string) {
	t.Helper()
	for _, a := range row.Attempts {
		switch {
		case a.Task == projectstate.TaskIntegration && a.Outcome == projectstate.OutcomePassed:
			judged = a.AttemptID
		case a.Task == projectstate.TaskTesting && a.Outcome == projectstate.OutcomePassed:
			if a.Evidence.Kind != projectstate.EvidenceCheckRun {
				t.Fatalf("the testing pass cites %+v, want a check run", a.Evidence)
			}
			cited = a.Evidence.Ref
		}
	}
	return judged, cited
}

// taskOf is the view's task named id.
func taskOf(v delivery.ActivityView, id string) delivery.ActivityTaskView {
	for _, task := range v.Tasks {
		if task.ID == id {
			return task
		}
	}
	return delivery.ActivityTaskView{}
}

// testingHold is the system comment the testing task's latest revision carries.
func testingHold(v delivery.ActivityView) (delivery.ReviewThreadComment, bool) {
	revs := taskOf(v, "testing").Revisions
	if len(revs) == 0 {
		return delivery.ReviewThreadComment{}, false
	}
	for _, c := range revs[len(revs)-1].Thread {
		if strings.HasPrefix(c.Anchor, "$.testingState.checkRuns") {
			return c, true
		}
	}
	return delivery.ReviewThreadComment{}, false
}

// endHeldWorkflows terminates, when the test ends, the executions a held gate
// leaves running — the project's pump and the driven activity's walk — what RunScenario
// does for a projected scenario, so a gate the test left held cannot answer the next
// test's pump start for the same project. A terminate of a closed execution is a no-op.
//
// held names further activities whose walk the test leaves held (the driven activity's
// is always ended).
func endHeldWorkflows(t *testing.T, h *scenariohost.Host, held ...string) {
	t.Helper()
	ids := []string{planProject + ":nextActivity", planProject + ":activity:" + drivenActivity}
	for _, a := range held {
		ids = append(ids, planProject+":activity:"+a)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, id := range ids {
			_ = h.TemporalClient().TerminateWorkflow(ctx, id, "", "test ended")
		}
	})
}
