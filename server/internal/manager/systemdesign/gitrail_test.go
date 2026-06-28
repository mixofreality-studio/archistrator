package systemdesign

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// =============================================================================
// I-DESIGN-DISPATCH §2b/§2c WIRE-LEVEL regression — the PR-rail-enabled CoAuthor
// spine. Method product → NO BDD; regression-first, black-box at the WIRE seam.
// The rail is stubbed at the EXTERNAL sourceControlAccess boundary (a FAKE rail
// recording every verb) and the read-back is served by a BRANCH-AWARE fake
// projectstate. The Manager under test is NOT faked; the workflow drives the REAL
// mint → OpenBranch → dispatch → observe → OpenPullRequest → branch-aware read-back
// → stage(on-branch) → human gate → status-guard → +1 → merge → commit(on-main)
// sequence over the Temporal in-memory test env (runs under -short).
// =============================================================================

// ---- fakeRail: the EXTERNAL PR-rail seam (IPullRequestRail subset) -----------

type railCall struct {
	verb   string
	repo   string
	branch string
	prRef  string
}

// fakeRail records every PR-rail verb and serves a scripted PR status. checkGreen
// drives the merge guard. It satisfies the design Manager's SourceControlRail.
type fakeRail struct {
	mu         sync.Mutex
	calls      []railCall
	checkGreen bool
	openedPRs  int
}

func (r *fakeRail) record(c railCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, c)
}

func (r *fakeRail) verbCount(verb string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c.verb == verb {
			n++
		}
	}
	return n
}

func (r *fakeRail) GetInstallationToken(_ context.Context, repo sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.record(railCall{verb: "GetInstallationToken", repo: sourcecontrol.RepoRefString(repo)})
	return sourcecontrol.RepoCredential{Bytes: []byte("tok"), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (r *fakeRail) OpenBranch(_ context.Context, repo sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.BranchRef, error) {
	r.record(railCall{verb: "OpenBranch", repo: sourcecontrol.RepoRefString(repo), branch: string(branch)})
	// The Manager discards the BranchRef (it only ensures the branch exists); a zero
	// ref is fine — the workflow never re-materializes a branch handle.
	return sourcecontrol.BranchRef(""), nil
}

func (r *fakeRail) OpenPullRequest(_ context.Context, repo sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.PullRequestRef, error) {
	r.mu.Lock()
	r.openedPRs++
	prRef := "pr/" + string(spec.Head)
	r.mu.Unlock()
	r.record(railCall{verb: "OpenPullRequest", repo: sourcecontrol.RepoRefString(repo), branch: string(spec.Head), prRef: prRef})
	return sourcecontrol.PullRequestRefFromString(prRef), nil
}

func (r *fakeRail) GetPullRequestStatus(_ context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
	r.record(railCall{verb: "GetPullRequestStatus", repo: sourcecontrol.RepoRefString(repo), prRef: sourcecontrol.PullRequestRefString(pr)})
	rollup := sourcecontrol.CheckFailure
	if r.checkGreen {
		rollup = sourcecontrol.CheckSuccess
	}
	return sourcecontrol.PullRequestStatus{CheckRollup: rollup, Mergeable: r.checkGreen}, nil
}

func (r *fakeRail) PostReview(_ context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) error {
	r.record(railCall{verb: "PostReview", repo: sourcecontrol.RepoRefString(repo), prRef: sourcecontrol.PullRequestRefString(pr)})
	return nil
}

func (r *fakeRail) MergePullRequest(_ context.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential, _ fwra.IdempotencyKey) (sourcecontrol.MergeResult, error) {
	r.record(railCall{verb: "MergePullRequest", repo: sourcecontrol.RepoRefString(repo), prRef: sourcecontrol.PullRequestRefString(pr)})
	return sourcecontrol.MergeResult{Merged: true, Commit: "merged"}, nil
}

var _ SourceControlRail = (*fakeRail)(nil)

// ---- branchAwareFakeProjectState: read-back/stage capture by branch ----------

// branchAwareFakeProjectState extends fakeProjectState behavior with the §2a
// branch-aware extension so the rail-enabled spine's read-back + stage land on the
// session branch. It records the branch each read/stage targeted so the test can
// assert the session-branch routing.
type branchAwareFakeProjectState struct {
	*fakeProjectState
	mu            sync.Mutex
	readBranches  []string
	stageBranches []string
}

var _ projectstate.BranchAwareProjectStateAccess = (*branchAwareFakeProjectState)(nil)

func (f *branchAwareFakeProjectState) ReadProjectOnBranch(ctx context.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	f.mu.Lock()
	f.readBranches = append(f.readBranches, branch)
	f.mu.Unlock()
	return f.fakeProjectState.ReadProject(fwra.Context{Context: ctx}, projectID)
}

func (f *branchAwareFakeProjectState) StageArtifactForReviewOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, model projectstate.ArtifactModel, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.stageBranches = append(f.stageBranches, branch)
	f.mu.Unlock()
	return f.fakeProjectState.StageArtifactForReview(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, model)
}

func newRailWorkflows(ps projectstate.ProjectStateAccess, pipe *fakePipeline, rail SourceControlRail) *Workflows {
	return &Workflows{
		ProjectState: ps,
		Pipeline:     pipe,
		Rail:         rail,
		Repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo"), true
		},
	}
}

func registerRailCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *Workflows) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: ExecutionKindCoAuthor})
	env.RegisterActivity(wf.ReadProjectActivity)
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

// THE RAIL HAPPY PATH (§2b/§2c). With the rail wired, the System draft (architect-
// owned, single dispatch) runs the full settled flow: OpenBranch(sessionBranch) →
// dispatch → OpenPullRequest(head=sessionBranch) → read-back ON the session branch →
// stage ON the session branch → AwaitingReview → Approve → status guard (green) → +1 →
// merge → commit on main.
func Test_CoAuthor_RailEnabled_BranchPRReadBackPlusOneMerge_HappyPath(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // dispatch observed Succeeded
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("rail happy path workflow error: %v", err)
	}
	var outcome CoAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != CoAuthorApproved {
		t.Fatalf("want CoAuthorApproved, got %d", outcome)
	}
	// The rail ran the full settled sequence exactly once each.
	for _, verb := range []string{"GetInstallationToken", "OpenBranch", "OpenPullRequest", "GetPullRequestStatus", "PostReview", "MergePullRequest"} {
		if rail.verbCount(verb) != 1 {
			t.Fatalf("want exactly one %s rail call, got %d (calls: %+v)", verb, rail.verbCount(verb), rail.calls)
		}
	}
	// The read-back + stage rode over the SESSION BRANCH (non-empty), not main.
	if len(ps.readBranches) == 0 || ps.readBranches[0] == "" {
		t.Fatalf("read-back must target the session branch, got %v", ps.readBranches)
	}
	if len(ps.stageBranches) != 1 || ps.stageBranches[0] == "" {
		t.Fatalf("stage must target the session branch, got %v", ps.stageBranches)
	}
	// Commit landed on main (the canonical head) exactly once.
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one CommitArtifact(KindSystem) on main, got %v", base.committed)
	}
}

// THE MERGE GUARD (§2b). At Approve the required CI check is NOT green: the rail must
// NOT merge and the spine must NOT commit — it routes to the StageDraftFailed recovery
// gate. Withdraw ends clean with nothing committed.
func Test_CoAuthor_RailEnabled_ApproveButPRNotGreen_DoesNotMerge_Recovers(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: false} // the merge guard is RED
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	// First Approve hits the not-green guard → StageDraftFailed; then Withdraw ends it.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a not-green merge guard must not crash the workflow: %v", err)
	}
	if rail.verbCount("MergePullRequest") != 0 {
		t.Fatalf("a not-green PR must NOT be merged, got %d merge calls", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 0 {
		t.Fatalf("a not-green merge guard must NEVER commit, got %v", base.committed)
	}
}
