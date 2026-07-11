package projectdesign

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	billing "github.com/mixofreality-studio/archistrator/server/internal/engine/billing"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// =============================================================================
// I-DESIGN-DISPATCH Part 3 — the projectdesign (Phase-2) TWIN of the wiring-level
// PROOF. Method product → NO BDD; regression-first, black-box at the WIRE seam. The
// rail is stubbed at the EXTERNAL sourceControlAccess boundary (a coherent SCRIPTED
// rail) + the read-back is served by a BRANCH-AWARE fake projectstate; the external
// agentic-job seam is the existing fakePipeline (workflow_test.go). The Manager under
// test is the REAL CoAuthorPhase2ArtifactWorkflow — no internal component is faked.
//
// Phase-2 has NO PM-critique round-trip (a single draft dispatch), and the SDP-assemble
// path keeps its three estimate Engines IN-WORKFLOW and gets NO rail — so this file
// drives ONLY the per-artifact draft path (the only path the rail rides). The
// AssembleSDPReviewWorkflow is deliberately untouched here (its in-process Engine join
// is proven in workflow_test.go).
//
// Proven here, mirroring the systemdesign twin:
//   1. happy round-trip + branch reconciliation (read-back branch == dispatch target_branch)
//   2. Approve: merge BEFORE commit-on-main + post-merge read on MAIN
//   3. Reject → redraft on a NEW session branch (attempt+1) + a NEW PR
//   4. PhaseFailed → StageDraftFailed with the rail wired (no approve-rail, no commit)
//   5. required-check RED → merge BLOCKED, no commit, recovers
// =============================================================================

// ---- seqLog: a shared ordered event log across the rail + projectstate fakes --

type seqLog struct {
	mu     sync.Mutex
	events []seqEvent
}

type seqEvent struct {
	op     string
	branch string
}

func (l *seqLog) add(op, branch string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, seqEvent{op: op, branch: branch})
}

func (l *seqLog) ops() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.events))
	for i, e := range l.events {
		out[i] = e.op
	}
	return out
}

func (l *seqLog) firstIndexOf(op string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, e := range l.events {
		if e.op == op {
			return i
		}
	}
	return -1
}

// ---- scriptedRail: the EXTERNAL PR-rail seam (per-attempt PR + ordered log) ----

type scriptedRail struct {
	mu  sync.Mutex
	log *seqLog

	checkGreen bool
	// statusAuthFailsRemaining, when >0, makes GetPullRequestStatus return an fwra.Auth
	// error (the platform's rate-limit-403-as-Auth) and decrement — exercises QA F35.
	statusAuthFailsRemaining int
	// syncErr, when non-nil, makes SyncManagedScaffold fail terminally — exercises the
	// managed-scaffold-sync containment (dispatch BLOCKED, session at the failed gate).
	syncErr error

	openedBranches []string
	openedPRHeads  []string
	mergedPRs      []string
	prByHead       map[string]string
	calls          map[string]int
}

func newScriptedRail(green bool, log *seqLog) *scriptedRail {
	return &scriptedRail{checkGreen: green, log: log, prByHead: map[string]string{}, calls: map[string]int{}}
}

func (r *scriptedRail) count(verb string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[verb]
}

// CommitManagedFiles backs the managed-scaffold sync: sourcecontrol.SyncManagedScaffold
// (the free-function composition helper the custom SyncManagedScaffoldActivity wraps)
// reaches the rail through this verb for a non-managedFileSyncer fake. It records the sync
// call under the "SyncManagedScaffold" counter and honors the scripted syncErr.
func (r *scriptedRail) CommitManagedFiles(_ fwra.Context, _ sourcecontrol.RepoRef, _ []sourcecontrol.ManagedFile, _ sourcecontrol.RepoCredential) (sourcecontrol.CommitRef, error) {
	r.mu.Lock()
	r.calls["SyncManagedScaffold"]++
	err := r.syncErr
	r.mu.Unlock()
	if err != nil {
		return sourcecontrol.CommitRef(""), err
	}
	return sourcecontrol.CommitRef("scaffold-sync"), nil
}

func (r *scriptedRail) GetInstallationToken(_ fwra.Context, _ sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.mu.Lock()
	r.calls["GetInstallationToken"]++
	r.mu.Unlock()
	return sourcecontrol.RepoCredential{Bytes: []byte("tok"), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (r *scriptedRail) OpenBranch(_ fwra.Context, _ sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential) (sourcecontrol.BranchRef, error) {
	r.mu.Lock()
	r.calls["OpenBranch"]++
	r.openedBranches = append(r.openedBranches, string(branch))
	r.mu.Unlock()
	return sourcecontrol.BranchRef(""), nil
}

func (r *scriptedRail) OpenPullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestRef, error) {
	r.mu.Lock()
	r.calls["OpenPullRequest"]++
	head := string(spec.Head)
	pr, ok := r.prByHead[head]
	if !ok {
		pr = "pr/" + head
		r.prByHead[head] = pr
		r.openedPRHeads = append(r.openedPRHeads, head)
	}
	r.mu.Unlock()
	// Ordered event (F40 openPR-after-read-back proof): record WHEN the PR was opened
	// relative to the read-back so a test can assert the PR is never opened before a
	// committed model has been confirmed on the session branch.
	if r.log != nil {
		r.log.add("openPR", head)
	}
	return sourcecontrol.PullRequestRefFromString(pr), nil
}

func (r *scriptedRail) GetPullRequestStatus(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.mu.Lock()
	r.calls["GetPullRequestStatus"]++
	green := r.checkGreen
	fail := r.statusAuthFailsRemaining > 0
	if fail {
		r.statusAuthFailsRemaining--
	}
	r.mu.Unlock()
	if fail {
		// The observed F35 fault: GitHub secondary rate-limit 403 the platform classifier
		// reports as Auth. railWithAuthRetry retries it within a bounded budget.
		return sourcecontrol.PullRequestStatus{}, fwra.New(fwra.Auth, "getPullRequest: github auth/permission denied")
	}
	rollup := sourcecontrol.CheckFailure
	if green {
		rollup = sourcecontrol.CheckSuccess
	}
	return sourcecontrol.PullRequestStatus{CheckRollup: rollup, Mergeable: green}, nil
}

func (r *scriptedRail) PostReview(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential) error {
	r.mu.Lock()
	r.calls["PostReview"]++
	r.mu.Unlock()
	return nil
}

func (r *scriptedRail) MergePullRequest(_ fwra.Context, _ sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.MergeResult, error) {
	r.mu.Lock()
	r.calls["MergePullRequest"]++
	r.mergedPRs = append(r.mergedPRs, sourcecontrol.PullRequestRefString(pr))
	r.mu.Unlock()
	if r.log != nil {
		r.log.add("merge", sourcecontrol.PullRequestRefString(pr))
	}
	return sourcecontrol.MergeResult{Merged: true, Commit: "merged-" + sourcecontrol.PullRequestRefString(pr)}, nil
}

// The remaining SourceControlAccess ops are outside the design PR-rail lifecycle; the stub
// satisfies the full contract with inert implementations so it can back the GENERATED rail
// Activities registered via genActivities.
func (r *scriptedRail) AdoptProjectRepo(_ fwra.Context, _ sourcecontrol.RepoAdoptionSpec) (sourcecontrol.RepoRef, error) {
	return sourcecontrol.RepoRef(""), nil
}

func (r *scriptedRail) ConfigureBranchProtection(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) error {
	return nil
}

func (r *scriptedRail) InstallAuthorizeApp(_ fwra.Context, _ sourcecontrol.AccountRef) (sourcecontrol.Installation, error) {
	return sourcecontrol.Installation(""), nil
}

var _ sourcecontrol.SourceControlAccess = (*scriptedRail)(nil)

// ---- seqProjectState: branch-aware read-back + ordered commit/read events ------

type seqProjectState struct {
	*fakeProjectState
	log *seqLog

	mu            sync.Mutex
	readBranches  []string
	stageBranches []string
}

var _ projectstate.BranchAwareProjectStateAccess = (*seqProjectState)(nil)

func (f *seqProjectState) ReadProject(ctx fwra.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
	f.log.add("readMain", "")
	f.mu.Lock()
	f.readBranches = append(f.readBranches, "")
	f.mu.Unlock()
	return f.fakeProjectState.ReadProject(ctx, projectID)
}

func (f *seqProjectState) ReadProjectOnBranch(ctx context.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	f.log.add("readBranch", branch)
	f.mu.Lock()
	f.readBranches = append(f.readBranches, branch)
	f.mu.Unlock()
	return f.fakeProjectState.ReadProject(fwra.Context{Context: ctx}, projectID)
}

func (f *seqProjectState) StageArtifactForReviewOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, model projectstate.ArtifactModel, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.log.add("stageBranch", branch)
	f.mu.Lock()
	f.stageBranches = append(f.stageBranches, branch)
	f.mu.Unlock()
	return f.StageArtifactForReview(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, model)
}

func (f *seqProjectState) CommitArtifact(ctx fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind) (projectstate.Version, error) {
	f.log.add("commit", "")
	return f.fakeProjectState.CommitArtifact(ctx, projectID, expectedVersion, kind)
}

func (f *seqProjectState) RejectArtifactOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.log.add("rejectBranch", branch)
	return f.RejectArtifact(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

func (f *seqProjectState) WithdrawArtifactOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.log.add("withdrawBranch", branch)
	return f.WithdrawArtifact(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

func newRailWorkflows(ps projectstate.ProjectStateAccess, pipe *fakePipeline, rail sourcecontrol.SourceControlAccess) *workflows {
	return &workflows{
		Estimation:   estimation.NewEstimationEngine(),
		OperationEst: operationestimation.NewOperationEstimationEngine(),
		Settlement:   billing.NewBillingEngine(),
		ProjectState: ps,
		Acts:         genInvokers{Opts: activityOptions()},
		Rail:         rail,
		Repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo"), true
		},
	}
}

func registerRailCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *workflows, pipe *fakePipeline) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorPhase2ArtifactWorkflow, workflow.RegisterOptions{Name: executionKindCoAuthor})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectOnBranchActivity)
	env.RegisterActivity(wf.StageArtifactForReviewActivity)
	env.RegisterActivity(wf.CommitArtifactActivity)
	env.RegisterActivity(wf.RejectArtifactActivity)
	env.RegisterActivity(wf.WithdrawArtifactActivity)
	env.RegisterActivity(wf.SyncManagedScaffoldActivity)
	registerGenActivities(env, wf.ProjectState, pipe, wf.Rail)
}

// PROOF 1+2 — branch reconciliation + merge-before-commit + post-merge-read-on-main
// for the Phase-2 per-artifact draft path.
func Test_CoAuthorPhase2_Rail_BranchReconciliation_MergeBeforeCommit_PostMergeReadOnMain(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // dispatch observed Succeeded
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("rail reconciliation workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want coAuthorApproved, got %d", outcome)
	}

	if len(pipe.submits) != 1 {
		t.Fatalf("Phase-2 draft must be a single dispatch, got %d", len(pipe.submits))
	}
	dispatchBranch := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if dispatchBranch == "" {
		t.Fatal("dispatch must carry a non-empty target_branch")
	}

	// THE PER-PROJECT-DESIGN-DISPATCH ASSERTION (UC2 twin of the live-activation gap
	// fix): with the rail WIRED, the Phase-2 design dispatch must target the PER-PROJECT
	// repo (the rail's repoRef) + aiarch-design.yml — NOT the central construction repo +
	// aiarch-construct.yml.
	// The workflow-side dispatchDesignJob decodes the opaque RepoRef ("acct|owner/repo") to
	// the RA's RepoTarget{Owner:"owner", Name:"repo"} BEFORE the generated submit invoker, so
	// the fake records the decoded "owner/repo".
	if pipe.submits[0].targetRepo != "owner/repo" {
		t.Fatalf("design dispatch must target the per-project repo %q, got %q", "owner/repo", pipe.submits[0].targetRepo)
	}
	if pipe.submits[0].workflowFile != "aiarch-design.yml" {
		t.Fatalf("design dispatch must target aiarch-design.yml (NOT aiarch-construct.yml), got %q", pipe.submits[0].workflowFile)
	}

	if len(rail.openedBranches) != 1 || rail.openedBranches[0] != dispatchBranch {
		t.Fatalf("OpenBranch must address the dispatch session branch %q, got %v", dispatchBranch, rail.openedBranches)
	}
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != dispatchBranch {
		t.Fatalf("OpenPullRequest head must be the session branch %q, got %v", dispatchBranch, rail.openedPRHeads)
	}

	// THE LOAD-BEARING RECONCILIATION: read-back rode over the dispatch target_branch.
	sawReadBackOnSession := false
	for _, b := range ps.readBranches {
		if b == dispatchBranch {
			sawReadBackOnSession = true
		}
	}
	if !sawReadBackOnSession {
		t.Fatalf("read-back branch must equal the dispatch target_branch %q, got reads %v", dispatchBranch, ps.readBranches)
	}
	if len(ps.stageBranches) != 1 || ps.stageBranches[0] != dispatchBranch {
		t.Fatalf("stage must ride over the dispatch session branch %q, got %v", dispatchBranch, ps.stageBranches)
	}

	mergeIdx := log.firstIndexOf("merge")
	commitIdx := log.firstIndexOf("commit")
	if mergeIdx < 0 || commitIdx < 0 {
		t.Fatalf("a green approve must MERGE then COMMIT; ops=%v", log.ops())
	}
	if mergeIdx >= commitIdx {
		t.Fatalf("merge must precede commit-on-main; ops=%v", log.ops())
	}
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+dispatchBranch {
		t.Fatalf("merge must target the session-branch PR pr/%s, got %v", dispatchBranch, rail.mergedPRs)
	}

	postMergeReadOnMain := false
	for i := mergeIdx + 1; i < commitIdx; i++ {
		if log.events[i].op == "readMain" {
			postMergeReadOnMain = true
		}
	}
	if !postMergeReadOnMain {
		t.Fatalf("after merge the approve path must re-read on MAIN before commit; ops=%v", log.ops())
	}

	if len(base.committed) != 1 || base.committed[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one CommitArtifact(KindPlanningAssumptions) on main, got %v", base.committed)
	}
}

// PROOF 3 (F40) — Reject → redraft on the SAME persistent session branch + the SAME PR.
func Test_CoAuthorPhase2_Rail_RejectRedraftsOnSameSessionBranchAndSamePR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "rework the staffing assumptions"}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("reject-redraft workflow error: %v", err)
	}

	if len(pipe.submits) < 2 {
		t.Fatalf("reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
	b1 := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	b2 := pipe.submits[1].dispatchInputs[dispatchInputTargetBranch]
	if b1 == "" || b2 == "" {
		t.Fatalf("both dispatches must carry a target_branch, got %q / %q", b1, b2)
	}
	if b1 != b2 {
		t.Fatalf("a reject must redraft on the SAME session branch (F40 single-branch); got %q then %q", b1, b2)
	}
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != b1 {
		t.Fatalf("reject must reuse the ONE PR on the persistent branch, got PR heads %v", rail.openedPRHeads)
	}
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+b1 {
		t.Fatalf("the merged PR must be the persistent PR pr/%s, got %v", b1, rail.mergedPRs)
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit after redraft→approve, got %v", base.committed)
	}
}

// PROOF 4 — Failure with the rail WIRED: PhaseFailed lands in StageDraftFailed, the
// dispatch-time rail half ran but the approve-time half never does, nothing commits.
func Test_CoAuthorPhase2_Rail_PhaseFailed_LandsInStageDraftFailed_NoApproveRailNoCommit(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline(pipelineFailed)
	pipe.diagnostic = "aiarch-validate found 2 violations"
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage == StageDrafting {
			t.Fatal("a failed design job must NOT leave perpetual StageDrafting (the wedge), even with the rail wired")
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("want StageDraftFailed after a terminal failure phase, got %d", view.Stage)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT crash the rail-wired workflow: %v", err)
	}
	if rail.count("OpenBranch") == 0 {
		t.Fatal("the dispatch-time rail half (OpenBranch) must have run before the observe")
	}
	if rail.count("OpenPullRequest") != 0 {
		t.Fatalf("a failed draft must NOT open a PR, got %d", rail.count("OpenPullRequest"))
	}
	if rail.count("GetPullRequestStatus") != 0 || rail.count("MergePullRequest") != 0 {
		t.Fatalf("a failed draft must NOT reach the merge guard/merge, got status=%d merge=%d",
			rail.count("GetPullRequestStatus"), rail.count("MergePullRequest"))
	}
	if len(base.staged) != 0 || len(base.committed) != 0 {
		t.Fatalf("a failed draft must stage/commit nothing, got staged=%d committed=%v", len(base.staged), base.committed)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("withdraw from the draft-failed gate must call WithdrawArtifact once, got %d", len(base.withdrawn))
	}
}

// PROOF 5 — Required-check RED → merge BLOCKED, no commit, recovers (ordered).
func Test_CoAuthorPhase2_Rail_RequiredCheckRed_BlocksMerge_NoCommit_Recovers(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()           // draft Succeeds (the run was green) ...
	rail := newScriptedRail(false, log) // ... but the PR's required check is RED at merge time
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a not-green merge guard must not crash the workflow: %v", err)
	}
	if rail.count("GetPullRequestStatus") == 0 {
		t.Fatal("the approve path must consult the merge guard (GetPullRequestStatus)")
	}
	if rail.count("MergePullRequest") != 0 {
		t.Fatalf("a RED required check must BLOCK the merge, got %d merge calls", rail.count("MergePullRequest"))
	}
	if rail.count("PostReview") != 0 {
		t.Fatalf("a RED required check must NOT relay the +1, got %d PostReview calls", rail.count("PostReview"))
	}
	if log.firstIndexOf("merge") != -1 {
		t.Fatalf("a RED required check must produce NO merge event; ops=%v", log.ops())
	}
	if log.firstIndexOf("commit") != -1 {
		t.Fatalf("a RED required check must produce NO commit event; ops=%v", log.ops())
	}
	if len(base.committed) != 0 {
		t.Fatalf("a not-green merge guard must NEVER commit, got %v", base.committed)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("a blocked merge must route to the recovery gate; withdraw expected once, got %d", len(base.withdrawn))
	}
}

// ---- branchAwareRejectFake: faithful PR-rail reject substrate (QA F28) ---------

// branchAwareRejectFake models the PRODUCTION PR-rail reality that caused QA F28 for the
// Phase-2 CoAuthor spine: the draft is staged ONLY on the session branch, so main's slot
// is unpopulated and a MAIN-path reject is a ContractMisuse. Its RejectArtifactOnBranch
// records the session branch (and can fault-inject); its shadowing main-path RejectArtifact
// fails loudly, so a regression to rejecting on main crashes the workflow and the test.
type branchAwareRejectFake struct {
	*fakeProjectState
	mu               sync.Mutex
	rejectBranches   []string
	withdrawBranches []string
	// failRejectOnBranch makes RejectArtifactOnBranch fault terminally (ContractMisuse) —
	// exercises the crash-containment recovery gate.
	failRejectOnBranch bool
	// failWithdrawOnMain, when true, makes the MAIN-path WithdrawArtifact fault (models the
	// unpopulated-main-slot ContractMisuse of the PR rail). Opt-in so ONLY the F30 review-gate
	// withdraw test arms it.
	failWithdrawOnMain bool
}

var _ projectstate.BranchAwareProjectStateAccess = (*branchAwareRejectFake)(nil)

func (f *branchAwareRejectFake) ReadProjectOnBranch(ctx context.Context, projectID projectstate.ProjectID, _ string) (projectstate.Project, error) {
	return f.ReadProject(fwra.Context{Context: ctx}, projectID)
}

func (f *branchAwareRejectFake) StageArtifactForReviewOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, _ string, model projectstate.ArtifactModel, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.StageArtifactForReview(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, model)
}

func (f *branchAwareRejectFake) RejectArtifactOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.rejectBranches = append(f.rejectBranches, branch)
	fail := f.failRejectOnBranch
	f.mu.Unlock()
	if fail {
		// A terminal (non-retryable) write fault while recording the Reject — the crash
		// scenario QA F28 must contain instead of failing the whole workflow.
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.RejectArtifact: simulated terminal write fault")
	}
	return f.fakeProjectState.RejectArtifact(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

// RejectArtifact (MAIN path) models the PR-rail reality: main's slot is unpopulated, so a
// main-path reject is a ContractMisuse. Shadows the embedded fake so a regression to
// rejecting on main crashes the workflow and this test fails loudly.
func (f *branchAwareRejectFake) RejectArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind, _ string) (projectstate.Version, error) {
	return 0, fwra.New(fwra.ContractMisuse, "projectstate.RejectArtifact: slot "+kind.String()+" is unpopulated (stage a model first)")
}

// WithdrawArtifactOnBranch records the Withdraw on the SESSION BRANCH and delegates to the
// embedded fake's bookkeeping (withdrawn). The main-path WithdrawArtifact is intentionally
// NOT shadowed to fail (the FAILED-gate withdraw legitimately rides main); the F30
// regression guard is the withdrawBranches assertion.
func (f *branchAwareRejectFake) WithdrawArtifactOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.withdrawBranches = append(f.withdrawBranches, branch)
	f.mu.Unlock()
	return f.fakeProjectState.WithdrawArtifact(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

// WithdrawArtifact (MAIN path) is the base behavior UNLESS failWithdrawOnMain is armed, in
// which case it models the PR-rail reality that caused QA F30: main's slot is unpopulated,
// so a main-path withdraw is a ContractMisuse. Armed only by the F30 review-gate test so a
// regression to withdrawing on main crashes the workflow and the test fails loudly.
func (f *branchAwareRejectFake) WithdrawArtifact(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind, notes string) (projectstate.Version, error) {
	if f.failWithdrawOnMain {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.WithdrawArtifact: slot "+kind.String()+" is unpopulated (stage a model first)")
	}
	return f.fakeProjectState.WithdrawArtifact(rc, projectID, expectedVersion, kind, notes)
}

// THE QA F28 REGRESSION (Phase-2 twin) — Reject/"Send back" against the PR rail records
// the Rejected status ON THE SESSION BRANCH (not main, where the version mismatches AND
// the slot is unpopulated), the workflow survives, and the redraft dispatch carries the
// architect's notes woven into the design_prompt.
func Test_CoAuthorPhase2_Rail_Reject_RecordsOnSessionBranch_RedraftCarriesFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &branchAwareRejectFake{fakeProjectState: base}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	const rejectNotes = "rework the staffing assumptions"
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a PR-rail reject must not crash the workflow: %v", err)
	}
	if len(ps.rejectBranches) != 1 || ps.rejectBranches[0] == "" {
		t.Fatalf("reject must target the session branch, got %v", ps.rejectBranches)
	}
	if len(base.rejected) != 1 || base.rejected[0].kind != projectstate.KindPlanningAssumptions || base.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact(KindPlanningAssumptions, %q) on the session branch, got %v", rejectNotes, base.rejected)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("a reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
	redraftPrompt := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputDesignPrompt]
	if !strings.Contains(redraftPrompt, rejectNotes) {
		t.Fatalf("redraft design_prompt must carry the architect's feedback %q; prompt:\n%s", rejectNotes, redraftPrompt)
	}
}

// CRASH CONTAINMENT AT THE REVIEW GATE (Phase-2 twin, QA F28 item 2). An activity fault
// while RECORDING the Reject must not kill the workflow: the spine lands at the
// human-visible StageDraftFailed recovery gate KEEPING the feedback, so a Retry redrafts
// with the architect's notes rather than silently discarding the send-back.
func Test_CoAuthorPhase2_Rail_RejectWriteFaults_RecoversAtFailedGate_RetainsFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &branchAwareRejectFake{fakeProjectState: base, failRejectOnBranch: true}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	const rejectNotes = "rework the staffing assumptions"
	// First gate: REJECT (the write FAULTS terminally) → crash containment lands at the
	// StageDraftFailed gate carrying the feedback.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: rejectNotes}})
	}, 30*time.Second)
	// Assert the workflow landed at the recoverable failed gate (NOT crashed) with a reason.
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow at failed gate: %v", err)
		}
		var view SessionStateView
		if derr := enc.Get(&view); derr != nil {
			t.Fatalf("decode SessionStateView: %v", derr)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a faulted reject must land at StageDraftFailed, got stage %v", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("the failed gate must surface a human FailureReason")
		}
	}, 45*time.Second)
	// Retry via the redraft lever WITH NO NEW FEEDBACK: the retained feedback must drive
	// the redraft.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalRedraft, redraftSignal{})
	}, 60*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 100*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a faulted reject must not crash the workflow: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("the retry must issue a SECOND dispatch, got %d submits", len(pipe.submits))
	}
	redraftPrompt := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputDesignPrompt]
	if !strings.Contains(redraftPrompt, rejectNotes) {
		t.Fatalf("the retained feedback %q must survive the fault and drive the redraft; prompt:\n%s", rejectNotes, redraftPrompt)
	}
}

// ---- f29BranchFake (Phase-2 twin): version-enforcing branch-aware substrate --

// f29BranchFake models the F29 reality for the Phase-2 spine: MAIN and the reused SESSION
// BRANCH sit at DIFFERENT versions. A fresh workflow captures main's version, but the
// Action's prior commits left the branch AHEAD. The read-back reads the branch version; a
// stage expecting the stale main version Conflicts. This fake ENFORCES the branch version
// on stage-on-branch (unlike the version-ignoring base fake), so the fix's convergence is
// proven.
type f29BranchFake struct {
	*fakeProjectState
	mu                  sync.Mutex
	mainVer             projectstate.Version
	branchVer           projectstate.Version
	stageExpecteds      []projectstate.Version
	stageFailsRemaining int
}

var _ projectstate.BranchAwareProjectStateAccess = (*f29BranchFake)(nil)

func (f *f29BranchFake) ReadProject(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.project
	p.Version = f.mainVer
	return p, nil
}

func (f *f29BranchFake) ReadProjectVersion(_ fwra.Context, _ projectstate.ProjectID) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mainVer, nil
}

func (f *f29BranchFake) ReadProjectOnBranch(_ context.Context, _ projectstate.ProjectID, _ string) (projectstate.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.project
	p.Version = f.branchVer
	return p, nil
}

func (f *f29BranchFake) StageArtifactForReviewOnBranch(_ context.Context, _ projectstate.ProjectID, expected projectstate.Version, _ string, model projectstate.ArtifactModel, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stageExpecteds = append(f.stageExpecteds, expected)
	if f.stageFailsRemaining > 0 {
		f.stageFailsRemaining--
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.StageArtifactForReview: simulated terminal stage fault")
	}
	if expected != f.branchVer {
		return 0, fwra.New(fwra.Conflict, fmt.Sprintf("projectstate.StageArtifactForReview: stale version: have %d, expected %d", f.branchVer, expected))
	}
	f.branchVer++
	f.staged = append(f.staged, model)
	return f.branchVer, nil
}

func (f *f29BranchFake) RejectArtifactOnBranch(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branchVer++
	return f.branchVer, nil
}

func (f *f29BranchFake) WithdrawArtifactOnBranch(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Terminal in the F29 tests; leave branchVer untouched.
	return f.branchVer, nil
}

// THE QA F29 REGRESSION (Phase-2 twin) — a fresh workflow reusing a session branch already
// AHEAD of main stages against the ACTUAL branch version and CONVERGES, instead of
// Conflicting non-recoverably against the stale main-captured version and crashing.
func Test_CoAuthorPhase2_Rail_StageAgainstDirtyBranch_Converges_NoCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("staging against a dirty session branch must converge, not crash: %v", err)
	}
	if ps.branchVer != 5 {
		t.Fatalf("stage must converge and advance the branch version to 5, got %d (expecteds=%v)", ps.branchVer, ps.stageExpecteds)
	}
	if last := ps.stageExpecteds[len(ps.stageExpecteds)-1]; last != 4 {
		t.Fatalf("the converged stage must expect the branch version 4, got %d (expecteds=%v)", last, ps.stageExpecteds)
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one commit after the converged stage → approve, got %v", base.committed)
	}
}

// THE QA F30 REGRESSION (Phase-2 twin) — Withdraw against the PR rail records the Withdrawn
// status ON THE SESSION BRANCH (not main, where the version mismatches AND the slot is
// unpopulated), the workflow survives, and ends withdrawn. failWithdrawOnMain arms the
// main-path guard so a regression to withdrawing on main crashes and this test fails loudly.
func Test_CoAuthorPhase2_Rail_Withdraw_RecordsOnSessionBranch_NoCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &branchAwareRejectFake{fakeProjectState: base, failWithdrawOnMain: true}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw, Feedback: &ReviewFeedback{Notes: "abandon this draft"}})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a PR-rail withdraw must not crash the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want coAuthorWithdrawn, got %d", outcome)
	}
	if len(ps.withdrawBranches) != 1 || ps.withdrawBranches[0] == "" {
		t.Fatalf("withdraw must target the session branch, got %v", ps.withdrawBranches)
	}
	if len(base.withdrawn) != 1 || base.withdrawn[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one WithdrawArtifact(KindPlanningAssumptions) on the session branch, got %v", base.withdrawn)
	}
}

// F40 (Phase-2 twin) — a Retry at the StageDraftFailed gate redrafts on the SAME persistent
// session branch (the F32 branch-per-retry topology is unwound; the template's refresh-from-
// main handles a stale base). Drives a stage fault → retry-via-reject (with feedback) and
// asserts the redraft dispatch targets the SAME branch AND still carries the retained feedback.
func Test_CoAuthorPhase2_Rail_RetryAtFailedGate_SameBranch_RetainsFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4, stageFailsRemaining: 1}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	const retryNotes = "fix the staffing assumptions before redrafting"
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: retryNotes}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed-gate retry must not crash the workflow: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("the retry must issue a SECOND draft dispatch, got %d submits", len(pipe.submits))
	}
	b0 := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	b1 := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputTargetBranch]
	if b0 == "" || b1 == "" {
		t.Fatalf("both dispatches must carry a target_branch, got %q / %q", b0, b1)
	}
	if b1 != b0 {
		t.Fatalf("a failed-gate retry must redraft on the SAME session branch (F40); got %q then %q", b0, b1)
	}
	if strings.Contains(b1, "-amend-") {
		t.Fatalf("the retry branch must be the stable session branch (no amendment suffix), got %q", b1)
	}
	if p := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputDesignPrompt]; !strings.Contains(p, retryNotes) {
		t.Fatalf("the retained feedback %q must drive the redraft; prompt:\n%s", retryNotes, p)
	}
}

// THE QA F35 REGRESSION (Phase-2 twin) — an approve-window fault (GetPullRequestStatus 403
// → Auth kind) must NOT kill the workflow. After the bounded retry is exhausted the session
// RETURNS to AwaitingReview with a queryable notice, and a re-approve merges. Not a redraft.
func Test_CoAuthorPhase2_Rail_ApproveStatusFault_ReturnsToAwaitingReview_ReapproveMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: &seqLog{}}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	rail.statusAuthFailsRemaining = 3 // the first approve exhausts the bounded budget

	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow after approve fault: %v", err)
		}
		var view SessionStateView
		if derr := enc.Get(&view); derr != nil {
			t.Fatalf("decode SessionStateView: %v", derr)
		}
		if view.Stage != StageAwaitingReview {
			t.Fatalf("an approve fault must return to AwaitingReview, got stage %v", view.Stage)
		}
		if view.FailureReason == nil || !strings.Contains(*view.FailureReason, "approve") {
			t.Fatalf("the returned session must carry a re-approve notice, got %v", view.FailureReason)
		}
	}, 70*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 90*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("an approve-window fault must not crash the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("the re-approve must commit, got outcome %d", outcome)
	}
	if len(rail.mergedPRs) != 1 {
		t.Fatalf("exactly one merge (on the successful re-approve), got %v", rail.mergedPRs)
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindPlanningAssumptions {
		t.Fatalf("want one commit after re-approve, got %v", base.committed)
	}
}

// BOUNDED RESILIENCE (QA F35, Phase-2 twin). Two transient 403s then success: the bounded
// retry absorbs them and the merge completes on the FIRST approve.
func Test_CoAuthorPhase2_Rail_ApproveStatusTransient_RetriesThenMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: &seqLog{}}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, &seqLog{})
	rail.statusAuthFailsRemaining = 2 // fail twice, 3rd attempt succeeds

	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("bounded retry must absorb the transient 403s: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("the first approve must commit after the retries, got %d", outcome)
	}
	if n := rail.calls["GetPullRequestStatus"]; n != 3 {
		t.Fatalf("want 3 bounded GetPullRequestStatus attempts (2 fault + 1 success), got %d", n)
	}
	if len(rail.mergedPRs) != 1 {
		t.Fatalf("the merge must complete on the first approve, got %v", rail.mergedPRs)
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit on the first approve, got %v", base.committed)
	}
}

// PROOF 6 (F40 live-bug fix) — the PR is opened ONLY AFTER the read-back confirms a
// committed model on the session branch (branch has ≥1 commit beyond main), never at
// session start on a freshly-cut zero-commit branch (the observed gtdapp 422 "no commits
// between base and head"). Ordered assertion: the FIRST "openPR" strictly follows the
// FIRST "readBranch" (read-back). Reject → redraft reuses the SAME one PR; approve merges.
func Test_CoAuthorPhase2_Rail_OpenPR_OnlyAfterReadBack_ReuseThenMerge(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "tighten"}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("openPR-after-read-back workflow error: %v", err)
	}

	// THE LOAD-BEARING ORDERING (the reorder fix): OpenBranch, then a committed draft, then
	// the read-back confirms it — and ONLY THEN is the PR opened.
	firstReadBack := log.firstIndexOf("readBranch")
	firstOpenPR := log.firstIndexOf("openPR")
	if firstReadBack < 0 {
		t.Fatalf("expected a session-branch read-back; ops=%v", log.ops())
	}
	if firstOpenPR < 0 {
		t.Fatalf("a successful draft must open the PR; ops=%v", log.ops())
	}
	if firstOpenPR <= firstReadBack {
		t.Fatalf("the PR must be opened AFTER the first read-back (never on a zero-commit branch at session start); ops=%v", log.ops())
	}
	if rail.count("OpenBranch") == 0 {
		t.Fatalf("OpenBranch (dispatch-time half) must run so the Action has a branch to commit on; ops=%v", log.ops())
	}

	b1 := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != b1 {
		t.Fatalf("reject→redraft must reuse the ONE persistent PR, got heads %v", rail.openedPRHeads)
	}
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+b1 {
		t.Fatalf("approve must merge the one persistent PR pr/%s, got %v", b1, rail.mergedPRs)
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit-on-main after redraft→approve, got %v", base.committed)
	}
}

// composeIdempotencyKey RUN-SCOPING (F40 root cause) — two sessions of the SAME workflow
// ID with the SAME ActivityID but DIFFERENT RunID must derive DISTINCT keys, so the
// constructionpipeline RA's run-name dedup never converges a fresh session onto a
// predecessor session's completed GitHub run (the observed amendment 422).
func Test_composeIdempotencyKey_RunScoped_DistinctPerRun(t *testing.T) {
	const wf = "gtdapp:planningAssumptions"
	const act = "5"
	k1 := composeIdempotencyKey(wf, "run-aaaa", act)
	k2 := composeIdempotencyKey(wf, "run-bbbb", act)
	if k1 == k2 {
		t.Fatalf("distinct RunIDs must yield distinct idempotency keys; both = %q", k1)
	}
	if !strings.Contains(string(k1), "run-aaaa") || !strings.Contains(string(k2), "run-bbbb") {
		t.Fatalf("the key must carry the RunID as its session scope; got %q / %q", k1, k2)
	}
	if composeIdempotencyKey(wf, "run-aaaa", act) != k1 {
		t.Fatal("same (workflowID, runID, activityID) must be stable so a transient retry dedups")
	}
}

// F40 AMENDMENT NO-CHANGE GUARD — a Phase-2 amendment session whose Action "succeeded"
// but committed NOTHING that changed the artifact (branch read-back == committed main
// model) must land at StageDraftFailed with the honest reason and open NO PR. Withdraw
// ends clean.
func Test_CoAuthorPhase2_Rail_Amendment_NoChange_LandsFailedGate_NoPR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	// seqProjectState.ReadProjectOnBranch returns the SAME project as main ⇒ no advancement.
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // the design job "succeeds"
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a no-change amendment must land at StageDraftFailed, got stage %d", view.Stage)
		}
		reason := ""
		if view.FailureReason != nil {
			reason = *view.FailureReason
		}
		if !strings.Contains(reason, "no changes") {
			t.Fatalf("the failed gate must carry the honest no-change reason, got %q", reason)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindPlanningAssumptions,
		Amendment:    1,
		Feedback:     &ReviewFeedback{Notes: "please tighten"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("no-change amendment must not crash: %v", err)
	}
	if rail.count("OpenPullRequest") != 0 {
		t.Fatalf("a no-change amendment must open NO PR (zero-new-commit branch), got %d", rail.count("OpenPullRequest"))
	}
	if rail.count("MergePullRequest") != 0 {
		t.Fatalf("a no-change amendment must NOT merge, got %d", rail.count("MergePullRequest"))
	}
	if len(base.committed) != 0 {
		t.Fatalf("a no-change amendment must commit nothing, got %v", base.committed)
	}
}

// amendmentIndexFor — the pre-field fix rule (Phase-2 twin). A COMMITTED slot yields
// max(1, Revisions): a slot committed BEFORE the Revisions field existed reads Revisions=0
// yet is still an amendment (index 1). Non-committed slots are the normal path (0).
func Test_amendmentIndexFor_Rule(t *testing.T) {
	if got := amendmentIndexFor(projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 0}); got != 1 {
		t.Fatalf("pre-field committed slot must yield amendment index 1, got %d", got)
	}
	if got := amendmentIndexFor(projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 4}); got != 4 {
		t.Fatalf("committed slot at revision 4 must yield amendment index 4, got %d", got)
	}
	for _, st := range []projectstate.ArtifactReviewStatus{
		projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected, projectstate.ReviewWithdrawn,
	} {
		if got := amendmentIndexFor(projectstate.ArtifactSlot{Status: st, Revisions: 5}); got != 0 {
			t.Fatalf("non-committed slot (status %d) must yield amendment index 0, got %d", st, got)
		}
	}
}

// F47 — the REDRAFT-SIGNAL feedback path (what RequestArtifactDraft now delivers via
// SignalWithStart, replacing the bare ExecuteWorkflow that DROPPED the feedback). A draft job
// fails → StageDraftFailed gate; the redraft signal carries the operator's fix notes; the NEXT
// draft dispatch's prompt must CONTAIN those notes.
func Test_CoAuthorPhase2_RedraftSignalFeedbackReachesPrompt(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: &seqLog{}}
	pipe := newFakePipeline(pipelineFailed) // the draft job fails → the StageDraftFailed gate
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	const notes = "resources must be plain strings, not objects"
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalRedraft, redraftSignal{Feedback: &ReviewFeedback{Notes: notes}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a redraft-signal retry must not crash: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("the redraft signal must trigger a SECOND draft dispatch, got %d", len(pipe.submits))
	}
	// THE LOAD-BEARING ASSERTION: the redraft-signal feedback reached the next draft prompt
	// (dropped live on gtdapp kind 8 because RequestArtifactDraft used a bare ExecuteWorkflow).
	if p := pipe.submits[1].dispatchInputs[dispatchInputDesignPrompt]; !strings.Contains(p, notes) {
		t.Fatalf("the redraft-signal feedback %q must reach the next draft prompt; prompt:\n%s", notes, p)
	}
}

// F48 — the Temporal activity-boundary codec MUST carry the durable review ledger. Without it,
// loadReviewThread (which reads the session branch through this projectEnvelope) returned [] even
// though the reject-with-comments append lives in the branch git — so the session-state query,
// the redraft prompt's writeReviewLedger block, and the approve gate all saw an empty thread.
func Test_projectEnvelope_PreservesReviewThread(t *testing.T) {
	var p projectstate.Project
	p.PlanningAssumptions = projectstate.ArtifactSlot{
		Status: projectstate.ReviewAwaitingReview,
		Model:  &projectstate.PlanningAssumptions{CalendarDaysPerWeek: 5},
		ReviewThread: []projectstate.ReviewComment{
			{ID: "r0c1", Text: "resources must be plain strings, not objects", AuthorRole: "architect", Round: 0, Status: projectstate.ReviewCommentOpen},
		},
	}
	env, err := encodeProject(p)
	if err != nil {
		t.Fatalf("encodeProject: %v", err)
	}
	got, err := env.Decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	thread := slotFor(got, projectstate.KindPlanningAssumptions).ReviewThread
	if len(thread) != 1 {
		t.Fatalf("the review thread must survive the Temporal codec round-trip, got %d comments: %+v", len(thread), thread)
	}
	if thread[0].ID != "r0c1" || thread[0].Status != projectstate.ReviewCommentOpen || thread[0].Text == "" {
		t.Fatalf("the codec must preserve the comment's id/status/text, got %+v", thread[0])
	}
}

// ledgerThreadFake stores a MUTABLE review thread and implements the review-ledger seam so a
// workflow-level test can drive reject-with-comments and read the thread back THROUGH the real
// Temporal codec (F48). ReadProjectOnBranch injects the live thread into the slot the read-back
// returns, modelling the git branch state the reject-append produced.
type ledgerThreadFake struct {
	*seqProjectState
	tmu    sync.Mutex
	thread []projectstate.ReviewComment
}

var (
	_ projectstate.BranchAwareProjectStateAccess = (*ledgerThreadFake)(nil)
	_ projectstate.LedgerProjectStateAccess      = (*ledgerThreadFake)(nil)
)

func (f *ledgerThreadFake) snapshot() []projectstate.ReviewComment {
	f.tmu.Lock()
	defer f.tmu.Unlock()
	return append([]projectstate.ReviewComment(nil), f.thread...)
}

func (f *ledgerThreadFake) ReadProjectOnBranch(ctx context.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	proj, err := f.seqProjectState.ReadProjectOnBranch(ctx, projectID, branch)
	if err != nil {
		return projectstate.Project{}, err
	}
	slot := proj.PlanningAssumptions
	slot.ReviewThread = f.snapshot()
	proj.PlanningAssumptions = slot
	return proj, nil
}

func (f *ledgerThreadFake) RejectArtifactOnBranchWithComments(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, round int64, comments []projectstate.ReviewComment, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.tmu.Lock()
	for i, c := range comments {
		f.thread = append(f.thread, projectstate.ReviewComment{
			ID: fmt.Sprintf("r%dc%d", round, i+1), Anchor: c.Anchor, AnchorText: c.AnchorText,
			Text: c.Text, AuthorRole: c.AuthorRole, Round: round, Status: projectstate.ReviewCommentOpen,
		})
	}
	f.tmu.Unlock()
	return f.RejectArtifactOnBranch(ctx, projectID, expectedVersion, branch, kind, notes, key)
}

func (f *ledgerThreadFake) SetReviewCommentStatusOnBranch(_ context.Context, _ projectstate.ProjectID, _ projectstate.Version, _ string, _ projectstate.ArtifactKind, commentID string, status string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.tmu.Lock()
	for i := range f.thread {
		if f.thread[i].ID == commentID {
			f.thread[i].Status = status
		}
	}
	f.tmu.Unlock()
	return f.bump(), nil
}

func (f *ledgerThreadFake) SeedReviewCommentsOnBranch(_ context.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, _ string, _ projectstate.ArtifactKind, round int64, comments []projectstate.ReviewComment, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.tmu.Lock()
	for i, c := range comments {
		f.thread = append(f.thread, projectstate.ReviewComment{ID: fmt.Sprintf("r%dc%d", round, i+1), Text: c.Text, AuthorRole: c.AuthorRole, Round: round, Status: projectstate.ReviewCommentOpen})
	}
	f.tmu.Unlock()
	return expectedVersion, nil
}

// F48 END-TO-END — reject-with-comments must flow through the whole review-ledger loop now that
// the Temporal codec carries the thread: (a) the session-state query shows the open entry, (b)
// the redraft prompt contains the writeReviewLedger block, and (c) Approve is BLOCKED until the
// open comment is waived. All three read the workflow's in-memory reviewThread, which is
// reloaded from the branch read-back — the read that silently dropped the thread pre-F48.
func Test_CoAuthorPhase2_RejectWithComments_ThreadRefreshes_QueryPromptAndApproveGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &ledgerThreadFake{seqProjectState: &seqProjectState{fakeProjectState: base, log: &seqLog{}}}
	pipe := newFakePipeline() // every dispatch succeeds
	rail := newScriptedRail(true, &seqLog{})
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)
	// The review-ledger activities the base helper does not register (only ledger-flow tests use them).
	env.RegisterActivity(wf.SetReviewCommentStatusActivity)
	env.RegisterActivity(wf.SeedReviewCommentsActivity)

	const commentText = "resources must be plain strings, not objects"

	// t=30s: at the first AwaitingReview, REJECT with an anchored comment.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{
			Decision: ReviewReject,
			Feedback: &ReviewFeedback{Comments: []AnchoredComment{{JSONPath: "$.resources", Text: commentText}}},
		})
	}, 30*time.Second)

	// t=80s: at the second AwaitingReview, the query must SHOW the open comment; then Approve
	// (which must be BLOCKED because the comment is still open).
	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if len(view.ReviewThread) != 1 || view.ReviewThread[0].Status != projectstate.ReviewCommentOpen {
			t.Fatalf("(a) the session-state query must show the OPEN reject comment, got %+v", view.ReviewThread)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 80*time.Second)

	// t=130s: Approve must have been BLOCKED (still AwaitingReview, nothing committed). Then waive.
	env.RegisterDelayedCallback(func() {
		enc, _ := env.QueryWorkflow(querySessionState)
		var view SessionStateView
		_ = enc.Get(&view)
		if view.Stage != StageAwaitingReview {
			t.Fatalf("(c) Approve must be BLOCKED while a comment is open — want AwaitingReview, got stage %d", view.Stage)
		}
		if len(base.committed) != 0 {
			t.Fatalf("(c) nothing may commit while a comment is open, got %v", base.committed)
		}
		env.SignalWorkflow(signalSetCommentStatus, setCommentStatusSignal{CommentID: "r0c1", Status: projectstate.ReviewCommentWaived})
	}, 130*time.Second)

	// t=170s: with the comment waived, Approve now merges.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 170*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the review-ledger loop must not crash: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("after waive + approve the session must be Approved, got %d", outcome)
	}
	if len(base.committed) != 1 {
		t.Fatalf("approve-after-waive must commit once, got %v", base.committed)
	}
	// (b) the REDRAFT prompt (the second dispatch, after the reject) carried the ledger block.
	if len(pipe.submits) < 2 {
		t.Fatalf("the reject must trigger a redraft dispatch, got %d", len(pipe.submits))
	}
	if p := pipe.submits[1].dispatchInputs[dispatchInputDesignPrompt]; !strings.Contains(p, commentText) {
		t.Fatalf("(b) the redraft prompt must carry the open review-ledger comment; prompt:\n%s", p)
	}
}

// MANAGED-SCAFFOLD SYNC GATE (sync-on-dispatch, 2026-07-06; UC2 twin of the
// systemdesign proof). A managed-scaffold sync failure BLOCKS the Phase-2 design
// dispatch: NO design job is submitted, NO session branch is opened, and the session
// lands at the human-visible StageDraftFailed gate (contained, never a crash).
func Test_CoAuthorPhase2_Rail_ScaffoldSyncFailure_BlocksDispatch_LandsAtFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, log)
	rail.syncErr = fwra.New(fwra.ContractMisuse, "seated workflow could not be refreshed")
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(querySessionState)
		if err != nil {
			t.Fatalf("QueryWorkflow: %v", err)
		}
		var view SessionStateView
		if err := enc.Get(&view); err != nil {
			t.Fatalf("decode SessionStateView: %v", err)
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a failed managed-scaffold sync must land at StageDraftFailed (dispatch blocked), got %d", view.Stage)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed managed-scaffold sync must be CONTAINED at the failed gate, not crash: %v", err)
	}
	if rail.count("SyncManagedScaffold") == 0 {
		t.Fatal("the managed-scaffold sync must have been attempted")
	}
	if len(pipe.submits) != 0 {
		t.Fatalf("a failed sync must BLOCK the design-job dispatch, got %d submits", len(pipe.submits))
	}
	if rail.count("OpenBranch") != 0 || rail.count("OpenPullRequest") != 0 {
		t.Fatalf("a failed sync must not open a branch/PR, got openBranch=%d openPR=%d",
			rail.count("OpenBranch"), rail.count("OpenPullRequest"))
	}
	if len(base.staged) != 0 || len(base.committed) != 0 {
		t.Fatalf("a blocked dispatch must stage/commit nothing, got staged=%d committed=%v", len(base.staged), base.committed)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("withdraw from the failed gate must call WithdrawArtifact once, got %d", len(base.withdrawn))
	}
}

// THE VERSION GATE (UC2 twin of the systemdesign proof; live regression gtdapp:5).
// A Phase-2 design execution already in flight when the managed-scaffold sync
// deployed has no history event for the sync activity — GetVersion pins it
// (DefaultVersion) to the OLD command sequence, so it must complete its full happy
// path with ZERO SyncManagedScaffold calls even with syncErr armed.
func Test_CoAuthorPhase2_Rail_ScaffoldSync_VersionGate_PreFeatureExecutionSkipsSync(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, log)
	// syncErr armed: an UN-GATED sync would derail the pre-feature run at the failed gate.
	rail.syncErr = fwra.New(fwra.ContractMisuse, "sync must not run for a pre-feature execution")
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	// Simulate a PRE-FEATURE in-flight execution: GetVersion resolves DefaultVersion.
	env.OnGetVersion("managed-scaffold-sync", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindPlanningAssumptions})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a pre-feature execution must run the OLD command sequence cleanly: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("want coAuthorApproved on the pre-feature path, got %d", outcome)
	}
	if got := rail.count("SyncManagedScaffold"); got != 0 {
		t.Fatalf("a pre-feature (DefaultVersion) execution must NEVER call SyncManagedScaffold, got %d", got)
	}
	if len(pipe.submits) != 1 || len(base.committed) != 1 {
		t.Fatalf("the pre-feature spine must dispatch and commit exactly once, got submits=%d committed=%v", len(pipe.submits), base.committed)
	}
}
