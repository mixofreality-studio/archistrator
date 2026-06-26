package systemdesign

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"

	"github.com/google/uuid"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// =============================================================================
// I-DESIGN-DISPATCH Part 3 — the WIRING-LEVEL PROOF (test-engineer). This file
// EXTENDS gitrail_test.go (the senior's two rail wire-tests) with the load-bearing
// branch-reconciliation assertions the settled model (Part 1, the exact branch
// table) demands but the happy-path smoke test does not yet pin:
//
//   1. read-back branch == dispatch target_branch (the session branch) — the
//      reconciliation the whole §2a rail exists to make true.
//   2. Approve: MergePullRequest lands BEFORE commit-on-main, and the post-merge
//      commit reads/writes MAIN (the §2a branch table rows 7-8 vs 9a).
//   3. Reject → redraft on a NEW session branch (attempt+1), a NEW PR — the prior
//      PR is not reused.
//   4. Failure (PhaseFailed) → StageDraftFailed with the rail WIRED (the anti-wedge
//      path still holds; the rail's dispatch-time half ran, the approve-time half
//      did NOT).
//   5. Required-check RED → merge BLOCKED, no main-commit (the §2b merge guard) —
//      the senior covers this; we add the ORDERED-event variant proving the guard
//      fires before any merge/commit and the session recovers, not crashes.
//
// Real Manager under test (the REAL CoAuthorArtifactWorkflow + every Activity);
// FAKE ONLY the external agentic-job seam (constructionPipelineAccess, reusing
// fakePipeline from workflow_test.go) + the GitHub PR-rail seam (a coherent
// SCRIPTED fakeRail) + the branch-aware projectStateAccess read-back. NO internal
// Manager component is faked. Temporal in-memory test env, runs under -short.
// The on-disk-git equivalent (the Action's raw CommitSubtree to a branch, then the
// read-back on that branch) is already proven one layer down by
// projectstate.TestGitStore_ExternalActionDraftIsReadBack; here we prove the
// MANAGER SPINE reconciles the branch the rail addressed with the branch the
// read-back/commit ride over.
// =============================================================================

// ---- seqLog: a shared ordered event log across the rail + projectstate fakes --

// seqLog records, in call order, the load-bearing spine events so a test can assert
// the SEQUENCE (merge-before-commit) and the BRANCH each read/write rode over. Both
// the rail fake and the branch-aware projectstate fake append to the SAME log.
type seqLog struct {
	mu     sync.Mutex
	events []seqEvent
}

type seqEvent struct {
	op     string // "merge" | "commit" | "readMain" | "readBranch" | "stageBranch" | "stageMain"
	branch string // for read/stage events: the branch the op rode over ("" == main)
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

// firstIndexOf returns the index of the first event with op, or -1.
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

// ---- scriptedRail: the EXTERNAL PR-rail seam with a per-attempt PR + ordered log -

// scriptedRail is a coherent IPullRequestRail-subset fake that models the PR rail
// per attempt: OpenBranch ensures a (per-attempt) session branch, OpenPullRequest
// mints a DISTINCT PR per head branch (a merged/closed PR is never reused), the
// status reflects a scripted green/red check, PostReview is the +1, and Merge moves
// the draft from the session branch to main. It records the ordered merge event into
// the shared seqLog so a test can assert merge-before-commit and which PR was merged.
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
		// A distinct PR per head branch — a fresh attempt branch opens a fresh PR.
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
	// The merge moves the draft from the session branch onto main — model that by
	// flipping the projectstate fake to serve the draft on main for the post-merge read.
	if r.log != nil {
		r.log.add("merge", sourcecontrol.PullRequestRefString(pr))
	}
	return sourcecontrol.MergeResult{Merged: true, Commit: "merged-" + sourcecontrol.PullRequestRefString(pr)}, nil
}

var _ SourceControlRail = (*scriptedRail)(nil)

// ---- seqProjectState: branch-aware read-back + ordered commit/read events ------

// seqProjectState wraps fakeProjectState with the §2a branch-aware extension AND the
// shared ordered log: it records which BRANCH each read-back/stage rode over and
// appends "commit"/"readMain"/"readBranch" events so a test can assert
// merge-before-commit and post-merge-read-on-main.
type seqProjectState struct {
	*fakeProjectState
	log *seqLog

	mu            sync.Mutex
	readBranches  []string
	stageBranches []string
}

var _ projectstate.BranchAwareProjectStateAccess = (*seqProjectState)(nil)

func (f *seqProjectState) ReadProject(ctx context.Context, projectID projectstate.ProjectID) (projectstate.Project, error) {
	// main-path read (branch override "" ⇒ ReadProject): this is the priors read AND
	// the post-merge re-read the approve path does before commit-on-main.
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
	return f.fakeProjectState.ReadProject(ctx, projectID)
}

func (f *seqProjectState) StageArtifactForReviewOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, model projectstate.ArtifactModel, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.log.add("stageBranch", branch)
	f.mu.Lock()
	f.stageBranches = append(f.stageBranches, branch)
	f.mu.Unlock()
	return f.fakeProjectState.StageArtifactForReview(ctx, projectID, expectedVersion, model, key)
}

func (f *seqProjectState) CommitArtifact(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.log.add("commit", "")
	return f.fakeProjectState.CommitArtifact(ctx, projectID, expectedVersion, kind, key)
}

func newSeqRailWorkflows(ps projectstate.ProjectStateAccess, pipe *fakePipeline, rail SourceControlRail) *Workflows {
	return &Workflows{
		ProjectState: ps,
		Pipeline:     pipe,
		Rail:         rail,
		Repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo"), true
		},
	}
}

// PROOF 1+2 — branch reconciliation + merge-before-commit + post-merge-read-on-main.
// The load-bearing assertions the settled branch table prescribes:
//   - the read-back rode over EXACTLY the dispatch target_branch (the session branch).
//   - the AwaitingReview stage rode over that SAME session branch.
//   - on Approve: MergePullRequest landed BEFORE the commit; the commit was preceded by
//     a main-path read (the post-merge re-seed) — i.e. commit reflects MAIN, not the
//     session branch.
func Test_CoAuthor_Rail_BranchReconciliation_MergeBeforeCommit_PostMergeReadOnMain(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // dispatch observed Succeeded
	rail := newScriptedRail(true, log)
	wf := newSeqRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: projectstate.KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("rail reconciliation workflow error: %v", err)
	}
	var outcome CoAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != CoAuthorApproved {
		t.Fatalf("want CoAuthorApproved, got %d", outcome)
	}

	// The exact session branch the dispatch addressed (from DispatchInputs).
	if len(pipe.submits) != 1 {
		t.Fatalf("System draft must be a single dispatch, got %d", len(pipe.submits))
	}
	dispatchBranch := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if dispatchBranch == "" {
		t.Fatal("dispatch must carry a non-empty target_branch")
	}

	// THE PER-PROJECT-DESIGN-DISPATCH ASSERTION (the live-activation gap fix): with the
	// rail WIRED, the dispatch must target the PER-PROJECT repo (the rail's repoRef) +
	// aiarch-design.yml — NOT the central construction repo + aiarch-construct.yml. This
	// is exactly what the systemtests fake could not catch (it intercepted all GitHub
	// REST regardless of repo).
	if pipe.submits[0].targetRepo != "acct|owner/repo" {
		t.Fatalf("design dispatch must target the per-project repo %q, got %q", "acct|owner/repo", pipe.submits[0].targetRepo)
	}
	if pipe.submits[0].workflowFile != "aiarch-design.yml" {
		t.Fatalf("design dispatch must target aiarch-design.yml (NOT aiarch-construct.yml), got %q", pipe.submits[0].workflowFile)
	}

	// The rail opened EXACTLY that branch + a PR with that head.
	if len(rail.openedBranches) != 1 || rail.openedBranches[0] != dispatchBranch {
		t.Fatalf("OpenBranch must address the dispatch session branch %q, got %v", dispatchBranch, rail.openedBranches)
	}
	if len(rail.openedPRHeads) != 1 || rail.openedPRHeads[0] != dispatchBranch {
		t.Fatalf("OpenPullRequest head must be the session branch %q, got %v", dispatchBranch, rail.openedPRHeads)
	}

	// THE LOAD-BEARING RECONCILIATION: the read-back rode over the dispatch target_branch.
	if len(ps.readBranches) == 0 {
		t.Fatal("no read recorded")
	}
	sawReadBackOnSession := false
	for _, b := range ps.readBranches {
		if b == dispatchBranch {
			sawReadBackOnSession = true
		}
	}
	if !sawReadBackOnSession {
		t.Fatalf("read-back branch must equal the dispatch target_branch %q, got reads %v", dispatchBranch, ps.readBranches)
	}
	// The AwaitingReview stage rode over that same session branch.
	if len(ps.stageBranches) != 1 || ps.stageBranches[0] != dispatchBranch {
		t.Fatalf("stage must ride over the dispatch session branch %q, got %v", dispatchBranch, ps.stageBranches)
	}

	// Merge landed BEFORE commit (the §2a table: merge first, then commit-on-main).
	mergeIdx := log.firstIndexOf("merge")
	commitIdx := log.firstIndexOf("commit")
	if mergeIdx < 0 {
		t.Fatalf("a green approve must MERGE; ops=%v", log.ops())
	}
	if commitIdx < 0 {
		t.Fatalf("a green approve must COMMIT; ops=%v", log.ops())
	}
	if mergeIdx >= commitIdx {
		t.Fatalf("merge must precede commit-on-main; ops=%v", log.ops())
	}
	// The merged PR is the session-branch PR.
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+dispatchBranch {
		t.Fatalf("merge must target the session-branch PR pr/%s, got %v", dispatchBranch, rail.mergedPRs)
	}

	// POST-MERGE READ ON MAIN: between merge and commit there is a main-path read
	// (branch "") — the approve path re-seeds headVersion from MAIN before committing.
	postMergeReadOnMain := false
	for i := mergeIdx + 1; i < commitIdx; i++ {
		if log.events[i].op == "readMain" {
			postMergeReadOnMain = true
		}
	}
	if !postMergeReadOnMain {
		t.Fatalf("after merge the approve path must re-read on MAIN before commit; ops=%v", log.ops())
	}

	// Commit landed on main exactly once.
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one CommitArtifact(KindSystem) on main, got %v", base.committed)
	}
}

// PROOF 3 — Reject → redraft on a NEW session branch (attempt+1), a NEW PR. The
// rejected PR is never reused: the second dispatch's target_branch differs from the
// first, the rail opened a second distinct branch + PR, and the eventual merge is of
// the SECOND (fresh) PR.
func Test_CoAuthor_Rail_RejectRedraftsOnNewSessionBranchAndNewPR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := newScriptedRail(true, log)
	wf := newSeqRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	// First gate: REJECT → fresh attempt branch + PR. Second gate: APPROVE → merge.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: "rework decomposition"}})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 70*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: projectstate.KindSystem})

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
	// THE LOAD-BEARING ASSERTION: the redraft is on a DIFFERENT (attempt+1) session branch.
	if b1 == b2 {
		t.Fatalf("a fresh REJECT must redraft on a NEW session branch (attempt+1); both were %q", b1)
	}
	// The rail opened two distinct branches + two distinct PRs (the prior PR not reused).
	if len(rail.openedPRHeads) != 2 {
		t.Fatalf("reject must open a NEW PR on the fresh branch (prior PR not reused), got PR heads %v", rail.openedPRHeads)
	}
	if rail.openedPRHeads[0] != b1 || rail.openedPRHeads[1] != b2 {
		t.Fatalf("PR heads must track the two session branches %q then %q, got %v", b1, b2, rail.openedPRHeads)
	}
	// The merge is of the SECOND (fresh) PR — the rejected one is never merged.
	if len(rail.mergedPRs) != 1 || rail.mergedPRs[0] != "pr/"+b2 {
		t.Fatalf("the merged PR must be the fresh attempt's PR pr/%s, got %v", b2, rail.mergedPRs)
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit after redraft→approve, got %v", base.committed)
	}
}

// PROOF 4 — Failure with the rail WIRED. A PhaseFailed draft lands the session in
// StageDraftFailed (NOT perpetual Drafting, NOT a crash) even with the rail enabled:
// the dispatch-time rail half ran (mint + OpenBranch), but the approve-time half
// (status guard / +1 / merge) NEVER runs and NOTHING commits. Withdraw ends clean.
// This is the rail-aware variant of the existing anti-wedge test.
func Test_CoAuthor_Rail_PhaseFailed_LandsInStageDraftFailed_NoApproveRailNoCommit(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline(PipelineFailed)
	pipe.diagnostic = "aiarch-validate found 2 violations"
	rail := newScriptedRail(true, log)
	wf := newSeqRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		enc, err := env.QueryWorkflow(QuerySessionState)
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
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: projectstate.KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal job failure must NOT crash the rail-wired workflow: %v", err)
	}
	// Dispatch-time rail half ran (the failure is observed AFTER OpenBranch).
	if rail.count("OpenBranch") == 0 {
		t.Fatalf("the dispatch-time rail half (OpenBranch) must have run before the observe")
	}
	// The approve-time rail half NEVER ran on a failed draft.
	if rail.count("OpenPullRequest") != 0 {
		t.Fatalf("a failed draft must NOT open a PR, got %d", rail.count("OpenPullRequest"))
	}
	if rail.count("GetPullRequestStatus") != 0 || rail.count("MergePullRequest") != 0 {
		t.Fatalf("a failed draft must NOT reach the merge guard/merge, got status=%d merge=%d",
			rail.count("GetPullRequestStatus"), rail.count("MergePullRequest"))
	}
	// Nothing staged, nothing committed; withdraw recorded once.
	if len(base.staged) != 0 || len(base.committed) != 0 {
		t.Fatalf("a failed draft must stage/commit nothing, got staged=%d committed=%v", len(base.staged), base.committed)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("withdraw from the draft-failed gate must call WithdrawArtifact once, got %d", len(base.withdrawn))
	}
}

// PROOF 5 — Required-check RED → merge BLOCKED, no commit-on-main, ORDERED. The status
// guard (GetPullRequestStatus) fires; because the rollup is red the spine does NOT
// PostReview, does NOT MergePullRequest, and does NOT commit. It routes to the
// StageDraftFailed recovery gate; Withdraw ends clean. (Complements the senior's
// count-only guard test with an ordered-event + recovery assertion.)
func Test_CoAuthor_Rail_RequiredCheckRed_BlocksMerge_NoCommit_Recovers(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	log := &seqLog{}
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &seqProjectState{fakeProjectState: base, log: log}
	pipe := newFakePipeline()           // draft Succeeds (the run was green) ...
	rail := newScriptedRail(false, log) // ... but the PR's required check is RED at merge time
	wf := newSeqRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalReviewDecision, ReviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(ExecutionKindCoAuthor, CoAuthorInput{ProjectID: id, ArtifactKind: projectstate.KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a not-green merge guard must not crash the workflow: %v", err)
	}
	// The guard read happened; the merge + the +1 did NOT.
	if rail.count("GetPullRequestStatus") == 0 {
		t.Fatal("the approve path must consult the merge guard (GetPullRequestStatus)")
	}
	if rail.count("MergePullRequest") != 0 {
		t.Fatalf("a RED required check must BLOCK the merge, got %d merge calls", rail.count("MergePullRequest"))
	}
	if rail.count("PostReview") != 0 {
		t.Fatalf("a RED required check must NOT relay the +1, got %d PostReview calls", rail.count("PostReview"))
	}
	// No merge, no main-commit.
	if log.firstIndexOf("merge") != -1 {
		t.Fatalf("a RED required check must produce NO merge event; ops=%v", log.ops())
	}
	if log.firstIndexOf("commit") != -1 {
		t.Fatalf("a RED required check must produce NO commit event; ops=%v", log.ops())
	}
	if len(base.committed) != 0 {
		t.Fatalf("a not-green merge guard must NEVER commit, got %v", base.committed)
	}
	// It RECOVERED (withdraw from the StageDraftFailed gate), it did not wedge or crash.
	if len(base.withdrawn) != 1 {
		t.Fatalf("a blocked merge must route to the recovery gate; withdraw expected once, got %d", len(base.withdrawn))
	}
}
