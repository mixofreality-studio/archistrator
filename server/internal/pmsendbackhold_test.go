package internal_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	agenticjobfake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
	messagebusfake "github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus/fake"
)

// The PM send-back hold, end to end (FPC Task 2.14, and the gap its review found: deleting the hold
// comment left every test green). Nothing here calls the hold's helpers. The REAL delivery Manager's
// worker runs the REAL activity walk of a requirements activity in Temporal's in-process test
// environment, over the REAL git-backed projectStateAccess facets in a throwaway repo, with the
// product manager's critique job the one double: it answers, as the project's committed critique
// carrier, that the core use cases need a revise. Every probe and every decision is a public
// operation of the Manager (QueryActivityView, SubmitReviewDecision) or a
// read-only contract read of the ledger.

const (
	holdProject  = "archistrator"
	holdActivity = "requirements"
	holdGateTask = "coreUseCasesReview"
)

// holdArrangement is a copy of archistrator's own committed project with ONE thing changed: the
// core use cases are a draft awaiting review, carrying the critique carrier verdict the scenario
// wants, under review policy preset `vibes`. Every earlier slot stays committed, so the walk
// begins at the core use cases and nothing else of the requirements lifecycle runs.
func holdArrangement(t *testing.T) []byte {
	t.Helper()
	return holdArrangementAt(t, projectstate.ReviewAwaitingReview)
}

// holdArrangementAt is holdArrangement with the core use cases' slot at the given review status
// (the Tier 1 rail test arranges a slot whose draft the walk has still to read back and stage).
func holdArrangementAt(t *testing.T, status projectstate.ArtifactReviewStatus) []byte {
	t.Helper()
	_, raw := ownProjectJSON(t)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	var slots map[string]json.RawMessage
	if err := json.Unmarshal(top["slots"], &slots); err != nil {
		t.Fatal(err)
	}
	var slot map[string]json.RawMessage
	key := string(rune('0' + int(projectstate.KindCoreUseCases)))
	if err := json.Unmarshal(slots[key], &slot); err != nil {
		t.Fatal(err)
	}
	set := func(m map[string]json.RawMessage, k string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		m[k] = b
	}
	set(slot, "status", int(status))
	set(slots, key, slot)
	set(top, "slots", slots)
	var rows map[string]json.RawMessage
	if err := json.Unmarshal(top["activityExecution"], &rows); err != nil {
		t.Fatal(err)
	}
	delete(rows, holdActivity)
	set(top, "activityExecution", rows)
	set(top, "reviewPolicy", map[string]any{"preset": projectstate.ReviewPresetVibes})
	set(top, "operatorPaused", false)
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// holdRepo is the project's repo: main carries the arranged project.json, and the working copy
// sits on another branch so pushes to main are accepted.
func holdRepo(t *testing.T, state []byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pmhold")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=seed", "-c", "user.email=seed@aiarch.local"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".aiarch", "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".aiarch", "state", "project.json"), state, 0o644); err != nil { //nolint:gosec // a repo file
		t.Fatal(err)
	}
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("commit", "-q", "-m", "arranged state")
	run("checkout", "-q", "-b", "checkout")
	return "file://" + dir
}

// holdAnswerCritique is what the product manager's critique run leaves behind: its verdict and
// notes committed on the core use cases' slot of main (the dormant venue's design branch), as the
// state tool's setCritiqueVerdict does. A clone, an edit and a push, because main is never checked
// out in the project repo.
func holdAnswerCritique(url, verdict, notes string) error {
	dir, err := os.MkdirTemp("", "pmhold-critic-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	git := func(args ...string) error {
		cmd := exec.Command("git", append([]string{"-c", "user.name=critic", "-c", "user.email=critic@aiarch.local"}, args...)...)
		cmd.Dir = dir
		if out, gerr := cmd.CombinedOutput(); gerr != nil {
			return fmt.Errorf("git %v: %w\n%s", args, gerr, out)
		}
		return nil
	}
	if err := git("clone", "-q", "-b", "main", url, "."); err != nil {
		return err
	}
	path := filepath.Join(dir, ".aiarch", "state", "project.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return err
	}
	var slots map[string]json.RawMessage
	if err := json.Unmarshal(top["slots"], &slots); err != nil {
		return err
	}
	key := strconv.Itoa(int(projectstate.KindCoreUseCases))
	var slot map[string]json.RawMessage
	if err := json.Unmarshal(slots[key], &slot); err != nil {
		return err
	}
	if slot["critiqueVerdict"], err = json.Marshal(verdict); err != nil {
		return err
	}
	if slot["critiqueNotes"], err = json.Marshal(notes); err != nil {
		return err
	}
	if slots[key], err = json.Marshal(slot); err != nil {
		return err
	}
	if top["slots"], err = json.Marshal(slots); err != nil {
		return err
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil { //nolint:gosec // a repo file
		return err
	}
	if err := git("commit", "-q", "-am", "pm critique: "+verdict); err != nil {
		return err
	}
	return git("push", "-q", "origin", "HEAD:main")
}

// envClient is the Temporal client the Manager's public operations use, answered by the test
// environment's own workflow: a query is the live session read, a signal is the decision landing.
type envClient struct {
	client.Client
	env *testsuite.TestWorkflowEnvironment
}

func (c envClient) QueryWorkflow(_ context.Context, _, _, queryType string, args ...any) (converter.EncodedValue, error) {
	return c.env.QueryWorkflow(queryType, args...)
}

func (c envClient) SignalWorkflow(_ context.Context, _, _, name string, arg any) error {
	c.env.SignalWorkflow(name, arg)
	return nil
}

// envWorker serves the Manager's workflows and activities on the test environment.
type envWorker struct {
	worker.Worker
	env *testsuite.TestWorkflowEnvironment
}

func (w envWorker) RegisterWorkflowWithOptions(fn any, o workflow.RegisterOptions) {
	w.env.RegisterWorkflowWithOptions(fn, o)
}

func (w envWorker) RegisterActivityWithOptions(fn any, o activity.RegisterOptions) {
	w.env.RegisterActivityWithOptions(fn, o)
}

// holdRun is one arranged requirements activity, running.
type holdRun struct {
	env *testsuite.TestWorkflowEnvironment
	mgr delivery.DeliveryManager
	ae  projectstate.ActivityExecutionAccess
	pid delivery.ProjectID
	aid delivery.ActivityID
}

func newHoldRun(t *testing.T, critique string) *holdRun {
	t.Helper()
	url := holdRepo(t, holdArrangement(t))
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.SetTestTimeout(10 * time.Minute)
	ae := projectstate.NewGitLocalActivityExecutionAccess(url)
	// The agent jobs are the one double. A draft job finishes having drafted nothing new (the slot
	// already holds the draft); a critique job finishes having committed the PM's verdict on the
	// slot, which is the carrier the walk reads back, exactly as for a real run.
	jobs := &agenticjobfake.FakeAgenticJobAccess{
		SubmitAgenticJobFn: func(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
			if spec.DispatchInputs["job_mode"] == "critique" {
				if err := holdAnswerCritique(url, critique, "the abandon path reaches no Client and ends in nothing the actor sees"); err != nil {
					return "", err
				}
			}
			return agenticjob.PipelineHandle("job-1"), nil
		},
		ObserveAgenticJobFn: func(_ fwra.Context, h agenticjob.PipelineHandle) (agenticjob.PipelineObservation, error) {
			return agenticjob.PipelineObservation{Handle: h, Phase: agenticjob.PhaseSucceeded}, nil
		},
	}
	bus := &messagebusfake.FakeMessageBus{
		DeliverSignalFn: func(fwra.Context, messagebus.ExecutionID, messagebus.SignalName, messagebus.ExecutionPayload, *messagebus.ExecutionStart) error {
			return nil
		},
	}
	mgr := delivery.NewDeliveryManager(envClient{env: env}, projectstate.NewGitLocalProjectStateAccess(url), nil,
		intervention.NewInterventionEngine(), review.NewReviewEngine(), estimation.NewEstimationEngine(),
		operationestimation.NewOperationEstimationEngine(), billing.NewBillingEngine(), designhealth.NewDesignHealthEngine(),
		jobs, nil, projectstate.NewGitLocalGitActivityStatusAccess(url), projectstate.NewGitLocalDesignSessionAccess(url),
		ae, bus, episode.NewLocalFSEpisodeAccess(url), 0, "tiered")
	delivery.RegisterManagerWorker(envWorker{env: env}, mgr)
	return &holdRun{env: env, mgr: mgr, ae: ae, pid: holdProject, aid: holdActivity}
}

// start begins the activity's walk, as the pump does, and runs script when the walk first parks.
func (r *holdRun) start(t *testing.T, script func()) {
	t.Helper()
	typ, err := json.Marshal(projectstate.ActivityTypeRequirements)
	if err != nil {
		t.Fatal(err)
	}
	r.env.RegisterDelayedCallback(script, time.Minute)
	r.env.ExecuteWorkflow("deliveryActivity", map[string]any{
		"ProjectID":  holdProject,
		"ActivityID": holdActivity,
		"Activity":   map[string]any{"ActivityID": holdActivity, "Type": json.RawMessage(typ)},
	})
}

func (r *holdRun) ctx() context.Context { return context.Background() }

// gate reads the live session and says whether the core use cases' gate is held open for a decision.
func (r *holdRun) gate(t *testing.T) (delivery.ActivityTaskView, bool) {
	t.Helper()
	view, err := r.mgr.QueryActivityView(viewCtx(), r.pid, r.aid)
	if err != nil {
		return delivery.ActivityTaskView{}, false
	}
	for _, task := range view.Tasks {
		if task.ID == holdGateTask && task.State == delivery.ActivityTaskAwaitingHuman {
			return task, true
		}
	}
	return delivery.ActivityTaskView{}, false
}

// round is the ledger's round at the gate, read through the facet's contract read.
func (r *holdRun) round(t *testing.T) projectstate.ReviewRound {
	t.Helper()
	row, err := r.ae.ReadActivityExecution(fwra.Context{Context: r.ctx()}, holdProject, holdActivity)
	if err != nil {
		t.Fatalf("read the activity's ledger: %v", err)
	}
	var latest projectstate.ReviewRound
	for _, rd := range row.Reviews {
		if string(rd.TaskID) == holdGateTask && rd.Round >= latest.Round {
			latest = rd
		}
	}
	if latest.RoundID == "" {
		t.Fatalf("the ledger holds no round at %s: %+v", holdGateTask, row.Reviews)
	}
	return latest
}

func holdComments(rd projectstate.ReviewRound) []projectstate.ReviewComment {
	var out []projectstate.ReviewComment
	for _, c := range rd.Thread {
		if c.AuthorRole == "system" {
			out = append(out, c)
		}
	}
	return out
}

// The control: the same walk under the same `vibes` preset, the PM's critique an approve, closes the
// gate on its own and leaves no hold. It is what makes the send-back's human gate below a floor the
// PM's verdict imposes rather than the preset's ordinary behaviour.
func TestPMApprove_VibesCoreUseCasesReviewAutoApproves(t *testing.T) {
	r := newHoldRun(t, projectstate.CritiqueVerdictApprove)
	r.start(t, func() {})
	if !r.env.IsWorkflowCompleted() {
		t.Fatal("the walk did not finish")
	}
	rd := r.round(t)
	if rd.Outcome != projectstate.RoundPassed || rd.DecidedBy == "operator" {
		t.Errorf("an approving PM under vibes did not auto-approve: %+v", rd)
	}
	if hold := holdComments(rd); len(hold) != 0 {
		t.Errorf("an approving PM left a hold: %+v", hold)
	}
}

func TestPMSendBackHold_VibesCoreUseCasesReview(t *testing.T) {
	r := newHoldRun(t, projectstate.CritiqueVerdictRevise)
	var steps []string
	var poll func()
	poll = func() {
		if _, ok := r.gate(t); !ok {
			r.env.RegisterDelayedCallback(poll, time.Minute)
			return
		}
		steps = append(steps, "gate parked")
		r.script(t)
		steps = append(steps, "script done")
	}
	r.start(t, poll)
	if !r.env.IsWorkflowCompleted() {
		t.Fatal("the walk did not finish")
	}
	if len(steps) != 2 {
		t.Fatalf("the script did not run to its end: %v", steps)
	}
	if err := r.env.GetWorkflowError(); err != nil {
		t.Logf("the walk ended with %v (its tail is not under test)", err)
	}
	// (e) continued: the approve decided the round, as the operator.
	if rd := r.round(t); rd.Outcome != projectstate.RoundPassed || rd.DecidedBy != "operator" {
		t.Errorf("(e) the approve did not decide the round as the operator: %+v", rd)
	}
}

// held asserts (a) and (b) at the parked gate and returns the hold comment's id.
func (r *holdRun) held(t *testing.T) (string, bool) {
	t.Helper()
	g, _ := r.gate(t)
	rd := r.round(t)

	// (a) the gate does not auto-approve under vibes: the round is still pending, and the roster the
	// reviewer strip shows says a human is required, for the PM's send-back.
	if rd.Outcome != projectstate.RoundPending {
		t.Errorf("(a) the round was decided %q with nobody asked: %+v", rd.Outcome, rd)
	}
	if g.ReviewSet == nil || g.ReviewSet.RequiresHuman == nil || !*g.ReviewSet.RequiresHuman ||
		g.ReviewSet.Reason == nil || !strings.Contains(*g.ReviewSet.Reason, "pmCoverageSendBack") {
		t.Errorf("(a) the open gate's roster does not require a human for the PM's send-back: %+v", g.ReviewSet)
	}

	// (b) the send-back landed a blocking system comment on the round.
	hold := holdComments(rd)
	if len(hold) != 1 || !projectstate.PMCoverageHoldIn(hold) || !projectstate.ReviewCommentBlocksApprove(hold[0]) {
		t.Errorf("(b) the round carries no blocking system hold comment: %+v", rd.Thread)
		return "", false
	}
	return hold[0].ID, true
}

// script is what the operator does at the parked gate, steps (a) through (e).
func (r *holdRun) script(t *testing.T) {
	t.Helper()
	id, ok := r.held(t)
	if !ok {
		return
	}
	approve := func(notes string) error {
		return r.mgr.SubmitReviewDecision(viewCtx(), r.pid, r.aid, holdGateTask,
			delivery.ReviewDecisionInput{Decision: delivery.ReviewApprove}, &delivery.ReviewFeedback{Notes: notes})
	}
	stillPending := func(step string) {
		t.Helper()
		if rd := r.round(t); rd.Outcome != projectstate.RoundPending {
			t.Errorf("%s the refused approve still decided the round: %+v", step, rd)
		}
	}

	// (c) an approve over the unresolved hold is refused.
	if err := approve("the abandon path is Escape in the web client"); err == nil {
		t.Error("(c) an approve over the open hold was accepted")
	}
	stillPending("(c)")

	// (d) resolving the hold is not enough: an approve with no reason is refused.
	resolved := projectstate.ReviewCommentResolved
	if err := r.mgr.SubmitReviewDecision(viewCtx(), r.pid, r.aid, holdGateTask,
		delivery.ReviewDecisionInput{Decision: delivery.ReviewSetCommentStatus, CommentID: &id, CommentStatus: &resolved}, nil); err != nil {
		t.Errorf("resolve the hold: %v", err)
		return
	}
	for _, notes := range []string{"", "  \n"} {
		if err := approve(notes); err == nil || !strings.Contains(err.Error(), "reason") {
			t.Errorf("(d) an approve with notes %q over a resolved hold: %v, want a refusal demanding a reason", notes, err)
		}
	}
	stillPending("(d)")

	// (e) resolve plus a recorded reason approves; what the round then records is read after the walk.
	if err := approve("the abandon path is covered by the web client's own Escape test"); err != nil {
		t.Errorf("(e) an approve with the hold resolved and a reason was refused: %v", err)
	}
}
