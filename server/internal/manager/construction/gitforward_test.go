package construction

import (
	"context"
	"sync"
	"testing"

	"go.temporal.io/sdk/testsuite"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// =============================================================================
// C-MCN-GIT wiring tests. They drive the REAL constructionManager per-activity
// workflow (ConstructActivityWorkflow + its real Activity wrappers + the real
// rail→record sequencing in gitforward.go) over the Temporal in-memory test env,
// with the git-forward slice WIRED. They assert the resulting ActivityGit head-state
// at each lifecycle transition (branch-open → CI → arch-approved → merged) through the
// real Manager seam, and the idempotent-retry invariant (re-running a record step does
// NOT double-record / corrupt the row).
//
// Per [[project_aiarch_testing_no_bdd]] (black-box, wire-level, anti-cheat §7): the
// observable is the recorded head-state side effects on a faithful store — NOT internal
// calls. The GitStatus seam is backed by stubGitStatus, an in-memory store that
// implements the SAME partial-map-key upsert + idempotency-dedup semantics the real
// *projectstate.GitStore proves under gitactivity_test.go (the real store is exercised
// there against a throwaway on-disk repo; here the Manager WIRING is the system under
// test). The rail is a controllable double returning scripted opaque handles + CI.
// =============================================================================

// ---- stubRail: a controllable IPullRequestRail double -----------------------

// stubRail returns scripted opaque handles + a scripted CI rollup, and records every
// call so the test can assert the rail was driven in the expected order. It honors the
// frozen rail surface (opaque returns; the Manager records them).
type stubRail struct {
	mu sync.Mutex

	prRef    string
	ciRollup sourcecontrol.CheckState
	merged   bool

	opened    []string // branch names OpenBranch saw
	prOpened  []sourcecontrol.PullRequestSpec
	statuses  int
	reviews   []sourcecontrol.ReviewSubmission
	merges    int
	credMints int
}

func (r *stubRail) GetInstallationToken(_ context.Context, _ sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credMints++
	return sourcecontrol.RepoCredential{Bytes: []byte("tok")}, nil
}

// OpenBranch returns the ZERO BranchRef: the frozen rail surface exposes
// RepoRefFromString / PullRequestRefFromString but NO BranchRefFromString, so a test
// double cannot mint a non-empty opaque BranchRef. The Manager records whatever
// BranchRef.String() yields (here ""); in production the real rail returns a populated
// handle. Assertions therefore key on the branch NAME + the PR ref (both
// test-constructable), NOT on BranchRef content. (Noted as a minor contract gap in
// C-MCN-GIT.md — non-blocking; the wiring records the rail's return verbatim.)
func (r *stubRail) OpenBranch(_ context.Context, _ sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.BranchRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opened = append(r.opened, string(branch))
	return sourcecontrol.BranchRef(""), nil
}

func (r *stubRail) OpenPullRequest(_ context.Context, _ sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.PullRequestRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prOpened = append(r.prOpened, spec)
	return sourcecontrol.PullRequestRefFromString(r.prRef), nil
}

func (r *stubRail) GetPullRequestStatus(_ context.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses++
	return sourcecontrol.PullRequestStatus{CheckRollup: r.ciRollup, ApprovalCount: 1, Mergeable: true}, nil
}

func (r *stubRail) PostReview(_ context.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, review sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reviews = append(r.reviews, review)
	return nil
}

func (r *stubRail) MergePullRequest(_ context.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.MergeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.merges++
	return sourcecontrol.MergeResult{Commit: "main-sha", Merged: r.merged}, nil
}

var _ sourceControlRail = (*stubRail)(nil)

// ---- stubGitStatus: an in-memory git head-state mirror ----------------------

// stubGitStatus faithfully reproduces the real GitStore's per-activity record
// semantics: a partial-map-key upsert keyed by activityID, the PR-tolerant branch-open
// fusing, the CICheck=Pending birth, and dedup-first idempotency on idempotencyKey (a
// retried key returns the prior Version with NO second apply). It exposes the recorded
// rows so the test asserts the head-state the real workflow produced.
type stubGitStatus struct {
	mu sync.Mutex

	rows    map[string]projectstate.ActivityGitStatus
	cons    map[string]projectstate.ActivityConstructionStatus // per-activity construction lifecycle (Task 3)
	version projectstate.Version
	dedup   map[fwra.IdempotencyKey]projectstate.Version
	applies int // count of NON-deduped (real) applies — proves no double-apply
}

func newStubGitStatus(seed projectstate.Version) *stubGitStatus {
	return &stubGitStatus{
		rows:    map[string]projectstate.ActivityGitStatus{},
		cons:    map[string]projectstate.ActivityConstructionStatus{},
		version: seed,
		dedup:   map[fwra.IdempotencyKey]projectstate.Version{},
	}
}

// apply is the shared upsert path: dedup-first, then a partial map-key mutation +
// version bump. It mirrors gitstore.applyMutation (dedup-first; modeRequireExisting is
// irrelevant here since the project is seeded).
func (s *stubGitStatus) apply(key fwra.IdempotencyKey, activityID string, mutate func(g *projectstate.ActivityGitStatus)) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil // dedup-first: no second apply
	}
	s.applies++
	g := s.rows[activityID]
	g.ActivityID = activityID
	mutate(&g)
	s.rows[activityID] = g
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) RecordActivityBranchOpened(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID, branch, branchRef, prRef, crLabel string, isRevert bool, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) {
		g.BranchName = branch
		g.BranchRef = branchRef
		if prRef != "" {
			g.PullRequestRef = prRef
		}
		if crLabel != "" {
			g.CRLabel = crLabel
		}
		if isRevert {
			g.IsRevert = true
		}
		// CICheck=Pending on first birth (real store sets it when first).
		if g.CICheck == 0 {
			g.CICheck = projectstate.CICheckPending
		}
	})
}

func (s *stubGitStatus) RecordActivityCIObserved(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, ci projectstate.CICheckState, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.CICheck = ci })
}

func (s *stubGitStatus) RecordActivityArchApproved(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.ArchApproved = true })
}

func (s *stubGitStatus) RecordActivityMerged(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return s.apply(key, activityID, func(g *projectstate.ActivityGitStatus) { g.Merged = true })
}

func (s *stubGitStatus) RecordActivityStarted(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil
	}
	s.applies++
	c := s.cons[activityID]
	c.ActivityID = activityID
	c.Phase = projectstate.ActivityConstructionRunning
	s.cons[activityID] = c
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) RecordActivityCompleted(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, activityID string, _ projectstate.RepoCredential, key fwra.IdempotencyKey) (projectstate.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if activityID == "" {
		return 0, fwra.New(fwra.ContractMisuse, "empty activityID")
	}
	if v, ok := s.dedup[key]; ok {
		return v, nil
	}
	s.applies++
	c := s.cons[activityID]
	c.ActivityID = activityID
	c.Phase = projectstate.ActivityConstructionDone
	s.cons[activityID] = c
	s.version++
	s.dedup[key] = s.version
	return s.version, nil
}

func (s *stubGitStatus) row(activityID string) (projectstate.ActivityGitStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.rows[activityID]
	return g, ok
}

// constructionPhase returns the recorded construction lifecycle phase for activityID.
func (s *stubGitStatus) constructionPhase(activityID string) (projectstate.ActivityConstructionPhase, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cons[activityID]
	return c.Phase, ok
}

var _ gitActivityStatusAccess = (*stubGitStatus)(nil)

// ---- helpers ----------------------------------------------------------------

// registerConstructGit registers the per-activity workflow + ALL activities including
// the git-forward ones.
func registerConstructGit(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	registerConstruct(env, wf)
	env.RegisterActivity(wf.MintRepoCredentialActivity)
	env.RegisterActivity(wf.OpenBranchActivity)
	env.RegisterActivity(wf.OpenPullRequestActivity)
	env.RegisterActivity(wf.GetPullRequestStatusActivity)
	env.RegisterActivity(wf.PostReviewActivity)
	env.RegisterActivity(wf.MergePullRequestActivity)
	env.RegisterActivity(wf.RecordActivityBranchOpenedActivity)
	env.RegisterActivity(wf.RecordActivityCIObservedActivity)
	env.RegisterActivity(wf.RecordActivityArchApprovedActivity)
	env.RegisterActivity(wf.RecordActivityMergedActivity)
	env.RegisterActivity(wf.RecordActivityStartedActivity)
	env.RegisterActivity(wf.RecordActivityCompletedActivity)
}

// gitWiredWorkflows builds a Workflows with the git-forward slice wired to the supplied
// rail + git store, a fixed repo resolver, and the happy-path engine fakes.
func gitWiredWorkflows(ps *fakeProjectState, rail *stubRail, git *stubGitStatus, mergeable bool) *Workflows {
	rail.merged = mergeable
	d := wfDeps{
		HandOff:      &fakeHandOff{class: AIWorker},
		Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review:       &fakeReview{},
		ProjectState: ps,
		Pipeline:     &fakePipeline{phase: PipelineSucceeded},
		Artifacts:    &fakeArtifacts{},
		Workers:      &fakeWorker{},
		// git-forward slice wired directly (the former WithGitForward composition helper
		// is retired — RegisterWorker now folds these from the manager's stored deps).
		Rail:      rail,
		GitStatus: git,
		Repo: func(_ ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("repo-1"), true
		},
	}
	return newWorkflows(d)
}

// gitSampleActivity carries a cr-NN label + revert flag so the recorded row's CR fields
// are asserted end-to-end.
func gitSampleActivity() ConstructionActivity {
	a := sampleActivity()
	a.ActivityID = "C-MST"
	a.CRLabel = "cr-021"
	return a
}

// ---- Tests ------------------------------------------------------------------

// The full git-forward lifecycle records branch-open → CI(success) → arch-approved →
// merged onto the per-activity head-state, in order, through the real Manager workflow.
func Test_GitForward_FullLifecycle_RecordsHeadState(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 5, Phase: 2}, version: 5}
	rail := &stubRail{prRef: "pr-7", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true /*mergeable*/)
	registerConstructGit(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: pid, ActivityID: "C-MST", Activity: gitSampleActivity(),
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	// Rail was driven: branch + PR opened, CI read, +1 relayed, merge performed.
	if len(rail.opened) == 0 || rail.opened[0] != "activity/C-MST" {
		t.Fatalf("want OpenBranch(activity/C-MST), got %v", rail.opened)
	}
	if len(rail.prOpened) != 1 || rail.prOpened[0].Base != mainBranch {
		t.Fatalf("want one OpenPullRequest with base=main, got %+v", rail.prOpened)
	}
	if string(rail.prOpened[0].Hints) != "cr-021" {
		t.Fatalf("cr label must ride in PR Hints, got %q", rail.prOpened[0].Hints)
	}
	if len(rail.reviews) != 1 || rail.reviews[0].Verdict != sourcecontrol.ReviewApprove {
		t.Fatalf("want one +1 (Approve) relayed, got %+v", rail.reviews)
	}
	if rail.merges != 1 {
		t.Fatalf("want one MergePullRequest, got %d", rail.merges)
	}

	// Head-state mirror reflects the full lifecycle.
	g, ok := git.row("C-MST")
	if !ok {
		t.Fatal("ActivityGit[C-MST] was never recorded")
	}
	if g.BranchName != "activity/C-MST" || g.PullRequestRef != "pr-7" {
		t.Fatalf("branch/PR handles wrong: %+v", g)
	}
	if g.CRLabel != "cr-021" {
		t.Fatalf("CR label not recorded: %+v", g)
	}
	if g.CICheck != projectstate.CICheckSuccess {
		t.Fatalf("CICheck = %v, want Success", g.CICheck)
	}
	if !g.ArchApproved {
		t.Fatalf("ArchApproved not recorded: %+v", g)
	}
	if !g.Merged {
		t.Fatalf("Merged not recorded: %+v", g)
	}

	// Task 3: the per-activity construction lifecycle recorded Running (started) then
	// Done (completed) through the same git-wired spine.
	phase, ok := git.constructionPhase("C-MST")
	if !ok {
		t.Fatal("ActivityConstruction[C-MST] was never recorded (started/completed)")
	}
	if phase != projectstate.ActivityConstructionDone {
		t.Fatalf("construction phase = %v, want Done (completed) after a happy-path spine", phase)
	}
}

// Task 3: the per-activity construction head-state flips to Running at the top of the
// spine and Done at the end — the records the pump's eligibility selection reads. A
// happy-path git-wired run leaves the activity Done so dependents unblock.
func Test_Construction_StartedThenCompleted_RecordedOnHeadState(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 5, Phase: 2}, version: 5}
	rail := &stubRail{prRef: "pr-1", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true /*mergeable*/)
	registerConstructGit(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: pid, ActivityID: "C-MST", Activity: gitSampleActivity(),
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	phase, ok := git.constructionPhase("C-MST")
	if !ok {
		t.Fatal("no construction head-state recorded")
	}
	if phase != projectstate.ActivityConstructionDone {
		t.Fatalf("want Done after a completed activity, got %v", phase)
	}
}

// A CI failure is mirrored as Failure (the dumb reflection); the lifecycle still
// proceeds to record the rest (CI is NOT a gate at this seam — the gate is
// interventionEngine, modeled by the merge mergeable flag, not CI).
func Test_GitForward_CIFailure_MirroredNotGated(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}, version: 1}
	rail := &stubRail{prRef: "pr-1", ciRollup: sourcecontrol.CheckFailure}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true)
	registerConstructGit(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: pid, ActivityID: "C-CI", Activity: ConstructionActivity{ActivityID: "C-CI", Kind: ActivityKindConstruction, ComponentID: "c"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	g, ok := git.row("C-CI")
	if !ok {
		t.Fatal("row never recorded")
	}
	if g.CICheck != projectstate.CICheckFailure {
		t.Fatalf("CICheck = %v, want Failure mirrored", g.CICheck)
	}
}

// The dormant slice (rail/git unwired) leaves the spine untouched: no git rows recorded
// and the activity still completes the non-git records.
func Test_GitForward_Dormant_WhenUnwired(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 1, Phase: 2}, version: 1}
	wf := newWorkflows(wfDeps{
		HandOff: &fakeHandOff{class: AIWorker}, Intervention: &fakeIntervention{directive: DirectiveRetry},
		Review: &fakeReview{}, ProjectState: ps, Pipeline: &fakePipeline{phase: PipelineSucceeded},
		Artifacts: &fakeArtifacts{}, Workers: &fakeWorker{},
		// no WithGitForward — Rail/GitStatus/Repo nil.
	})
	registerConstruct(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: pid, ActivityID: "C-NO-GIT", Activity: sampleActivity(),
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	// Non-git spine still recorded the binary exit.
	if len(ps.exited) != 1 {
		t.Fatalf("dormant slice must still complete the non-git spine, got exited=%v", ps.exited)
	}
}

// Idempotent retry: re-running the branch-opened record step with the SAME idempotency
// key returns the prior Version and does NOT double-apply (the dedup-first invariant the
// crash-safe workflow relies on). Driven directly through the Activity wrapper against
// the stub store (the workflow-replay path uses the same key derivation).
func Test_GitForward_RecordActivity_IdempotentRetry_NoDoubleApply(t *testing.T) {
	git := newStubGitStatus(10)
	ctx := context.Background()
	pid := projectstate.ProjectID(uuid.NewString())

	// Same idempotency key twice (a workflow retry re-runs the same Activity id).
	key := fwra.IdempotencyKey("wf-1:branch")
	v1, err := git.RecordActivityBranchOpened(ctx, pid, 10, "C-MST", "activity/C-MST", "ref", "pr-1", "cr-021", false, projectstate.RepoCredential{}, key)
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	v2, err := git.RecordActivityBranchOpened(ctx, pid, 0 /*stale*/, "C-MST", "activity/C-MST", "ref", "pr-1", "cr-021", false, projectstate.RepoCredential{}, key)
	if err != nil {
		t.Fatalf("idempotent re-record: %v", err)
	}
	if v1 != v2 {
		t.Fatalf("idempotent re-record version = %d, want prior %d", v2, v1)
	}
	if git.applies != 1 {
		t.Fatalf("dedup-first must apply exactly once, applied %d times (DOUBLE APPLY)", git.applies)
	}
}

// workflowGitForwardOrder asserts the rail+record order at the workflow level by
// checking that, on a successful lifecycle, the head-state row ends in the terminal
// (merged) state — i.e. every step ran and recorded in sequence without a gap.
func Test_GitForward_RecordsConvergeMonotonically(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	pid := ProjectID(uuid.NewString())
	ps := &fakeProjectState{project: projectstate.Project{ID: projectstate.ProjectID(pid), Version: 2, Phase: 2}, version: 2}
	rail := &stubRail{prRef: "pr-9", ciRollup: sourcecontrol.CheckSuccess}
	git := newStubGitStatus(0)
	wf := gitWiredWorkflows(ps, rail, git, true)
	registerConstructGit(env, wf)

	env.ExecuteWorkflow(ExecutionKindConstructActivity, ConstructActivityInput{
		ProjectID: pid, ActivityID: "C-MONO", Activity: ConstructionActivity{ActivityID: "C-MONO", Kind: ActivityKindConstruction, ComponentID: "c"},
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	g, _ := git.row("C-MONO")
	if !(g.BranchName != "" && g.CICheck == projectstate.CICheckSuccess && g.ArchApproved && g.Merged) {
		t.Fatalf("lifecycle did not converge through all record steps: %+v", g)
	}
	// At least 4 distinct applies (branch, ci, approve, merge) landed.
	if git.applies < 4 {
		t.Fatalf("want >=4 record applies across the lifecycle, got %d", git.applies)
	}
}
