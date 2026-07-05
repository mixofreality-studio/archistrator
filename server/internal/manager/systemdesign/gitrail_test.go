package systemdesign

import (
	"context"
	"strings"
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
// drives the merge guard. It satisfies the design Manager's sourceControlRail.
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

var _ sourceControlRail = (*fakeRail)(nil)

// ---- branchAwareFakeProjectState: read-back/stage capture by branch ----------

// branchAwareFakeProjectState extends fakeProjectState behavior with the §2a
// branch-aware extension so the rail-enabled spine's read-back + stage land on the
// session branch. It records the branch each read/stage targeted so the test can
// assert the session-branch routing.
type branchAwareFakeProjectState struct {
	*fakeProjectState
	mu             sync.Mutex
	readBranches   []string
	stageBranches  []string
	rejectBranches []string
	// failRejectOnBranch, when true, makes RejectArtifactOnBranch fault terminally
	// (a ContractMisuse) — used to exercise the QA F28 crash-containment recovery gate.
	failRejectOnBranch bool
}

var _ projectstate.BranchAwareProjectStateAccess = (*branchAwareFakeProjectState)(nil)

func (f *branchAwareFakeProjectState) ReadProjectOnBranch(ctx context.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	f.mu.Lock()
	f.readBranches = append(f.readBranches, branch)
	f.mu.Unlock()
	return f.ReadProject(fwra.Context{Context: ctx}, projectID)
}

func (f *branchAwareFakeProjectState) StageArtifactForReviewOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, model projectstate.ArtifactModel, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.stageBranches = append(f.stageBranches, branch)
	f.mu.Unlock()
	return f.StageArtifactForReview(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, model)
}

// RejectArtifactOnBranch records the Reject on the SESSION BRANCH — the correct PR-rail
// substrate, where the draft was staged and the branch version matches. It delegates to
// the embedded fake's bookkeeping (rejected + Notes), then records the branch so the test
// asserts the reject rode the session branch (non-empty), not main.
func (f *branchAwareFakeProjectState) RejectArtifactOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
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

// RejectArtifact (MAIN path) models the PRODUCTION PR-rail reality that caused QA F28: in
// the rail flow the draft is staged ONLY on the session branch, so main's slot is
// unpopulated and a main-path reject is a ContractMisuse ("stage a model first"), exactly
// as the real GitStore's statusTransition raises. This shadows the embedded fake so that
// if the Manager ever regresses to rejecting on main, the workflow crashes here and the
// regression test fails loudly.
func (f *branchAwareFakeProjectState) RejectArtifact(_ fwra.Context, _ projectstate.ProjectID, _ projectstate.Version, kind projectstate.ArtifactKind, _ string) (projectstate.Version, error) {
	return 0, fwra.New(fwra.ContractMisuse, "projectstate.RejectArtifact: slot "+kind.String()+" is unpopulated (stage a model first)")
}

func newRailWorkflows(ps projectstate.ProjectStateAccess, pipe *fakePipeline, rail sourceControlRail) *workflows {
	return &workflows{
		ProjectState: ps,
		Pipeline:     pipe,
		Rail:         rail,
		Repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo"), true
		},
	}
}

func registerRailCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *workflows) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: executionKindCoAuthor})
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
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("rail happy path workflow error: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
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
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 60*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

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

// THE QA F28 REGRESSION — Reject/"Send back" against the PR rail. In the rail flow the
// draft + its AwaitingReview status live ONLY on the session branch (main's slot is
// unpopulated until an approved draft merges). The architect's Reject must therefore
// record the Rejected status ON THE SESSION BRANCH — NOT on main, where the version
// mismatches AND the slot is unpopulated (the ContractMisuse crash that ended the
// CoAuthor workflow FAILED and silently discarded the review comments). After the fix the
// Reject lands on the session branch, the workflow survives, and the redraft dispatch
// carries the architect's anchored comments + notes woven into the design_prompt.
func Test_CoAuthor_RailEnabled_Reject_RecordsOnSessionBranch_RedraftCarriesFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	const (
		rejectNotes = "rework the decomposition"
		commentPath = "$.containers[0].name"
		commentText = "this manager name violates the layering rule"
	)
	feedback := &ReviewFeedback{
		Notes:    rejectNotes,
		Comments: []AnchoredComment{{JSONPath: commentPath, Text: commentText}},
	}

	// First gate: REJECT with anchored feedback. Second gate (after the redraft reaches
	// AwaitingReview again): WITHDRAW to end the session cleanly.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: feedback})
	}, 30*time.Second)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 70*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	// The reject must NOT crash the workflow (QA F28 was a non-retryable ContractMisuse
	// that ended it FAILED).
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a PR-rail reject must not crash the workflow: %v", err)
	}
	// The Reject rode the SESSION BRANCH, not main (main's slot is unpopulated — the
	// branchAwareFakeProjectState's main-path RejectArtifact would ContractMisuse).
	if len(ps.rejectBranches) != 1 || ps.rejectBranches[0] == "" {
		t.Fatalf("reject must target the session branch, got %v", ps.rejectBranches)
	}
	if len(base.rejected) != 1 || base.rejected[0].kind != projectstate.KindSystem || base.rejected[0].notes != rejectNotes {
		t.Fatalf("want one RejectArtifact(KindSystem, %q) on the session branch, got %v", rejectNotes, base.rejected)
	}
	// The reject looped to a FRESH redraft dispatch that WEAVES IN the feedback: both the
	// free-text notes AND the JSONPath-anchored comment text (writeFeedback in prompts.go).
	if len(pipe.submits) < 2 {
		t.Fatalf("a reject must re-dispatch a fresh draft, got %d submits", len(pipe.submits))
	}
	redraftPrompt := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputDesignPrompt]
	for _, want := range []string{rejectNotes, commentPath, commentText} {
		if !strings.Contains(redraftPrompt, want) {
			t.Fatalf("redraft design_prompt must carry the architect's feedback %q; prompt:\n%s", want, redraftPrompt)
		}
	}
}

// CRASH CONTAINMENT AT THE REVIEW GATE (QA F28 item 2). An activity fault while RECORDING
// the Reject must not kill the workflow. The spine lands at the human-visible
// StageDraftFailed recovery gate KEEPING the received feedback, so a Retry redrafts with
// the architect's comments woven in rather than silently discarding the send-back.
func Test_CoAuthor_RailEnabled_RejectWriteFaults_RecoversAtFailedGate_RetainsFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base, failRejectOnBranch: true}
	pipe := newFakePipeline() // every dispatch Succeeds
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	const (
		rejectNotes = "rework the decomposition"
		commentPath = "$.containers[0].name"
		commentText = "this manager name violates the layering rule"
	)
	feedback := &ReviewFeedback{
		Notes:    rejectNotes,
		Comments: []AnchoredComment{{JSONPath: commentPath, Text: commentText}},
	}

	// First gate: REJECT (the write FAULTS terminally) → crash containment lands at the
	// StageDraftFailed gate carrying the feedback.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: feedback})
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
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 60*time.Second)
	// After the redraft reaches AwaitingReview, WITHDRAW to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 100*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a faulted reject must not crash the workflow: %v", err)
	}
	// The retry redrafted, and the RETAINED feedback (set before the faulted write) rode
	// into the redraft prompt even though the Retry signal carried none.
	if len(pipe.submits) < 2 {
		t.Fatalf("the retry must issue a SECOND dispatch, got %d submits", len(pipe.submits))
	}
	redraftPrompt := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputDesignPrompt]
	for _, want := range []string{rejectNotes, commentPath, commentText} {
		if !strings.Contains(redraftPrompt, want) {
			t.Fatalf("the retained feedback %q must survive the fault and drive the redraft; prompt:\n%s", want, redraftPrompt)
		}
	}
}
