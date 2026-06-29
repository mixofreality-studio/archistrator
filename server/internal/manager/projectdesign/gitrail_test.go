package projectdesign

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/estimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/operationestimation"
	"github.com/mixofreality-studio/archistrator/server/internal/engine/settlement"
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

func (r *scriptedRail) GetInstallationToken(_ context.Context, _ sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.mu.Lock()
	r.calls["GetInstallationToken"]++
	r.mu.Unlock()
	return sourcecontrol.RepoCredential{Bytes: []byte("tok"), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (r *scriptedRail) OpenBranch(_ context.Context, _ sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.BranchRef, error) {
	r.mu.Lock()
	r.calls["OpenBranch"]++
	r.openedBranches = append(r.openedBranches, string(branch))
	r.mu.Unlock()
	return sourcecontrol.BranchRef(""), nil
}

func (r *scriptedRail) OpenPullRequest(_ context.Context, _ sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.PullRequestRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls["OpenPullRequest"]++
	head := string(spec.Head)
	pr, ok := r.prByHead[head]
	if !ok {
		pr = "pr/" + head
		r.prByHead[head] = pr
		r.openedPRHeads = append(r.openedPRHeads, head)
	}
	return sourcecontrol.PullRequestRefFromString(pr), nil
}

func (r *scriptedRail) GetPullRequestStatus(_ context.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.mu.Lock()
	r.calls["GetPullRequestStatus"]++
	green := r.checkGreen
	r.mu.Unlock()
	rollup := sourcecontrol.CheckFailure
	if green {
		rollup = sourcecontrol.CheckSuccess
	}
	return sourcecontrol.PullRequestStatus{CheckRollup: rollup, Mergeable: green}, nil
}

func (r *scriptedRail) PostReview(_ context.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.PullRequestRef, _ sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) error {
	r.mu.Lock()
	r.calls["PostReview"]++
	r.mu.Unlock()
	return nil
}

func (r *scriptedRail) MergePullRequest(_ context.Context, _ sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.MergeResult, error) {
	r.mu.Lock()
	r.calls["MergePullRequest"]++
	r.mergedPRs = append(r.mergedPRs, sourcecontrol.PullRequestRefString(pr))
	r.mu.Unlock()
	if r.log != nil {
		r.log.add("merge", sourcecontrol.PullRequestRefString(pr))
	}
	return sourcecontrol.MergeResult{Merged: true, Commit: "merged-" + sourcecontrol.PullRequestRefString(pr)}, nil
}

var _ sourceControlRail = (*scriptedRail)(nil)

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

func newRailWorkflows(ps projectstate.ProjectStateAccess, pipe *fakePipeline, rail sourceControlRail) *workflows {
	return &workflows{
		Estimation:   estimation.NewEstimationEngine(),
		OperationEst: operationestimation.NewOperationEstimationEngine(),
		Settlement:   settlement.NewSettlementEngine(),
		ProjectState: ps,
		Pipeline:     pipe,
		Rail:         rail,
		Repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo"), true
		},
	}
}

func registerRailCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorPhase2ArtifactWorkflow, workflow.RegisterOptions{Name: executionKindCoAuthor})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectVersionActivity)
	env.RegisterActivity(wf.ReadProjectOnBranchActivity)
	env.RegisterActivity(wf.DispatchDesignJobActivity)
	env.RegisterActivity(wf.ObserveDesignJobActivity)
	env.RegisterActivity(wf.StageArtifactForReviewActivity)
	env.RegisterActivity(wf.CommitArtifactActivity)
	env.RegisterActivity(wf.RejectArtifactActivity)
	env.RegisterActivity(wf.WithdrawArtifactActivity)
	env.RegisterActivity(wf.MintRepoCredentialActivity)
	env.RegisterActivity(wf.OpenBranchActivity)
	env.RegisterActivity(wf.OpenPullRequestActivity)
	env.RegisterActivity(wf.GetPullRequestStatusActivity)
	env.RegisterActivity(wf.PostReviewActivity)
	env.RegisterActivity(wf.MergePullRequestActivity)
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
	registerRailCoAuthor(env, wf)

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
	if pipe.submits[0].targetRepo != "acct|owner/repo" {
		t.Fatalf("design dispatch must target the per-project repo %q, got %q", "acct|owner/repo", pipe.submits[0].targetRepo)
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

// PROOF 3 — Reject → redraft on a NEW session branch (attempt+1) + a NEW PR.
func Test_CoAuthorPhase2_Rail_RejectRedraftsOnNewSessionBranchAndNewPR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: planningAssumptionsReadBack(projectstate.ProjectID(id))}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()
	rail := newScriptedRail(true, log)
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

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
	if b1 == b2 {
		t.Fatalf("a fresh REJECT must redraft on a NEW session branch (attempt+1); both were %q", b1)
	}
	if len(rail.openedPRHeads) != 2 {
		t.Fatalf("reject must open a NEW PR on the fresh branch (prior PR not reused), got PR heads %v", rail.openedPRHeads)
	}
	if rail.openedPRHeads[0] != b1 || rail.openedPRHeads[1] != b2 {
		t.Fatalf("PR heads must track the two session branches %q then %q, got %v", b1, b2, rail.openedPRHeads)
	}
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+b2 {
		t.Fatalf("the merged PR must be the fresh attempt's PR pr/%s, got %v", b2, rail.mergedPRs)
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
	registerRailCoAuthor(env, wf)

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
	registerRailCoAuthor(env, wf)

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
