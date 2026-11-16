package internal_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/designhealth"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/intervention"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	reviewfake "github.com/mixofreality-studio/archistrator/server/internal/engine/review/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	agenticjobfake "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob/fake"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/episode"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus"
	messagebusfake "github.com/mixofreality-studio/archistrator/server/internal/utility/messagebus/fake"

	"go.temporal.io/sdk/testsuite"
)

// The server rail's Tier 1 admission (FPC Task 6.3), end to end. The REAL delivery Manager's worker
// runs the REAL activity walk of a requirements activity whose core use cases draft job has
// finished, in Temporal's in-process test environment over the real git-backed facets. The review
// Engine is the one wrapped double: every operation is the real Engine's except AdmitArtifact,
// whose answer the test sets. The probes are what the walk did with that answer: whether it staged
// the draft, and what it ended with.

// countingStages is the activity-execution facet with every StageTaskOutput counted: a staged
// draft is the one effect a refused draft must not have.
type countingStages struct {
	projectstate.ActivityExecutionAccess
	staged atomic.Int32
}

func (c *countingStages) StageTaskOutput(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version,
	expectedActivityVersion int64, activityID string, taskID string, branch string, model projectstate.ModelEnvelope,
	cred projectstate.RepoCredential, idempotencyKey fwra.IdempotencyKey,
) (projectstate.StagedRef, error) {
	c.staged.Add(1)
	return c.ActivityExecutionAccess.StageTaskOutput(rc, projectID, expectedVersion, expectedActivityVersion, activityID,
		taskID, branch, model, cred, idempotencyKey)
}

// holdCommitDraft is what the core use cases draft job leaves behind: the draft committed on the
// slot of main (the dormant venue's design branch), as the state tool's putDraftModel does. A clone,
// a write and a push, because main is never checked out in the project repo.
func holdCommitDraft(url string, slot json.RawMessage) error {
	dir, err := os.MkdirTemp("", "tier1-draft-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	git := func(args ...string) error {
		cmd := exec.Command("git", append([]string{"-c", "user.name=drafter", "-c", "user.email=drafter@aiarch.local"}, args...)...)
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
	slots[key] = slot
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
	if err := git("commit", "-q", "-am", "draft the core use cases"); err != nil {
		return err
	}
	return git("push", "-q", "origin", "HEAD:main")
}

// coreUseCasesSlot is the core use cases' slot of a project document: the draft, as committed.
func coreUseCasesSlot(t *testing.T, doc []byte) json.RawMessage {
	t.Helper()
	var top struct {
		Slots map[string]json.RawMessage `json:"slots"`
	}
	if err := json.Unmarshal(doc, &top); err != nil {
		t.Fatal(err)
	}
	slot, ok := top.Slots[strconv.Itoa(int(projectstate.KindCoreUseCases))]
	if !ok {
		t.Fatal("the arranged project holds no core use cases slot")
	}
	return slot
}

// admissionRun is one requirements activity whose core use cases draft is committed and not yet
// read back, with the Tier 1 answer the review Engine gives.
type admissionRun struct {
	*holdRun
	stages *countingStages
	asked  atomic.Int32
	kind   atomic.Value
}

func newAdmissionRun(t *testing.T, answer []review.AdmissionProblem) *admissionRun {
	t.Helper()
	url := holdRepo(t, holdArrangementAt(t, projectstate.ReviewNone))
	draftSlot := coreUseCasesSlot(t, holdArrangementAt(t, projectstate.ReviewAwaitingReview))
	env := newHoldEnv()
	stages := &countingStages{ActivityExecutionAccess: projectstate.NewGitLocalActivityExecutionAccess(url)}
	jobs := &agenticjobfake.FakeAgenticJobAccess{
		// A draft job commits its model on the slot; a critique job approves, so a draft that is
		// staged runs to a closed gate on its own under vibes.
		SubmitAgenticJobFn: func(_ fwra.Context, spec agenticjob.PipelineSpec) (agenticjob.PipelineHandle, error) {
			if spec.DispatchInputs["job_mode"] == "draft" {
				if err := holdCommitDraft(url, draftSlot); err != nil {
					return "", err
				}
			}
			if spec.DispatchInputs["job_mode"] == "critique" {
				if err := holdAnswerCritique(url, projectstate.CritiqueVerdictApprove, "covers the use cases"); err != nil {
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
	r := &admissionRun{stages: stages}
	real := review.NewReviewEngine()
	engine := &reviewfake.FakeReviewEngine{
		ProposeReviewsFn: real.ProposeReviews,
		DeriveTaskSpecFn: real.DeriveTaskSpec,
		TaskSpecHashFn:   real.TaskSpecHash,
		AdmitArtifactFn: func(_ fweng.Context, _ string, kind string) ([]review.AdmissionProblem, error) {
			r.asked.Add(1)
			r.kind.Store(kind)
			return answer, nil
		},
	}
	mgr := delivery.NewDeliveryManager(envClient{env: env}, projectstate.NewGitLocalProjectStateAccess(url), nil,
		intervention.NewInterventionEngine(), engine, estimation.NewEstimationEngine(),
		operationestimation.NewOperationEstimationEngine(), billing.NewBillingEngine(), designhealth.NewDesignHealthEngine(),
		jobs, nil, projectstate.NewGitLocalGitActivityStatusAccess(url), projectstate.NewGitLocalDesignSessionAccess(url),
		stages, bus, episode.NewLocalFSEpisodeAccess(url), 0, "tiered")
	delivery.RegisterManagerWorker(envWorker{env: env}, mgr)
	r.holdRun = &holdRun{env: env, mgr: mgr, ae: stages, pid: holdProject, aid: holdActivity}
	return r
}

func newHoldEnv() *testsuite.TestWorkflowEnvironment {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.SetTestTimeout(10 * time.Minute)
	return env
}

// A local problem refuses the draft at its read-back: the walk ends with the non-retryable
// AdmissionRefused naming the problem, and nothing was staged.
func TestTier1AdmissionRail_LocalProblemRefusesTheDraftBeforeStaging(t *testing.T) {
	r := newAdmissionRun(t, []review.AdmissionProblem{
		{Consumer: "testgen", Path: "testPlan.todoListAccess", Message: "the scenario names an operation the contract lacks",
			Severity: review.AdmissionLocal, Tier: review.AdmissionTier1},
		{Consumer: "appgen clients", Path: "system", Message: "waiting on a contract",
			Severity: review.AdmissionIncomplete, Tier: review.AdmissionTier1},
	})
	r.start(t, func() {})
	if !r.env.IsWorkflowCompleted() {
		t.Fatal("the walk did not finish")
	}
	if r.asked.Load() == 0 {
		t.Fatalf("the rail never asked the review Engine to admit the draft: %v", r.env.GetWorkflowError())
	}
	if got, _ := r.kind.Load().(string); got != projectstate.KindCoreUseCases.WireName() {
		t.Errorf("admitted kind = %q, want %q", got, projectstate.KindCoreUseCases.WireName())
	}
	err := r.env.GetWorkflowError()
	if err == nil {
		t.Fatal("the walk ended clean: a draft with a local problem was admitted")
	}
	for _, want := range []string{"AdmissionRefused", "refused by Tier 1 admission", "testgen", "testPlan.todoListAccess",
		"the scenario names an operation the contract lacks"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the walk's error lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "waiting on a contract") {
		t.Errorf("an incomplete problem was reported as a refusal: %v", err)
	}
	if n := r.stages.staged.Load(); n != 0 {
		t.Errorf("a refused draft was staged %d times", n)
	}
}

// An incomplete-only answer is not a refusal: the draft is staged and the walk runs on to its
// closed gate.
func TestTier1AdmissionRail_IncompleteOnlyStagesNormally(t *testing.T) {
	r := newAdmissionRun(t, []review.AdmissionProblem{
		{Consumer: "appgen clients", Path: "system", Message: "waiting on a contract",
			Severity: review.AdmissionIncomplete, Tier: review.AdmissionTier1},
	})
	r.start(t, func() {})
	if !r.env.IsWorkflowCompleted() {
		t.Fatal("the walk did not finish")
	}
	if r.asked.Load() == 0 {
		t.Fatal("the rail never asked the review Engine to admit the draft")
	}
	if err := r.env.GetWorkflowError(); err != nil && strings.Contains(err.Error(), "AdmissionRefused") {
		t.Fatalf("an incomplete-only answer refused the draft: %v", err)
	}
	if n := r.stages.staged.Load(); n == 0 {
		t.Error("the admitted draft was never staged")
	}
}
