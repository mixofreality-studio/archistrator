package systemdesign

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
	// statusAuthFailsRemaining, when >0, makes GetPullRequestStatus return an fwra.Auth
	// error (the platform's rate-limit-403-as-Auth) and decrement — used to exercise the
	// QA F35 bounded-retry + approve-window containment.
	statusAuthFailsRemaining int
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
	r.mu.Lock()
	fail := r.statusAuthFailsRemaining > 0
	if fail {
		r.statusAuthFailsRemaining--
	}
	r.mu.Unlock()
	if fail {
		// The observed F35 fault: GitHub secondary rate-limit 403 the platform classifier
		// reports as Auth. railApproveOpts retries it within a bounded budget.
		return sourcecontrol.PullRequestStatus{}, fwra.New(fwra.Auth, "getPullRequest: github auth/permission denied")
	}
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
	mu               sync.Mutex
	readBranches     []string
	stageBranches    []string
	rejectBranches   []string
	withdrawBranches []string
	// failRejectOnBranch, when true, makes RejectArtifactOnBranch fault terminally
	// (a ContractMisuse) — used to exercise the QA F28 crash-containment recovery gate.
	failRejectOnBranch bool
	// failWithdrawOnMain, when true, makes the MAIN-path WithdrawArtifact fault (models the
	// unpopulated-main-slot ContractMisuse of the PR rail). Opt-in so ONLY the F30
	// review-gate withdraw test arms it — the FAILED-gate withdraw tests (which legitimately
	// ride main) leave it false and are unperturbed.
	failWithdrawOnMain bool
	// failReadBackDecode, when true, makes the branch READ-BACK fault as a TERMINAL decode of
	// committed state (a ContractMisuse carrying the closed-enum wire-name diagnostic) — the
	// QA F36 scenario: the drafting agent committed free prose into the "trigger" closed enum,
	// CI validate went green, but the server codec rejects the value on read-back.
	failReadBackDecode bool
}

var _ projectstate.BranchAwareProjectStateAccess = (*branchAwareFakeProjectState)(nil)

func (f *branchAwareFakeProjectState) ReadProjectOnBranch(ctx context.Context, projectID projectstate.ProjectID, branch string) (projectstate.Project, error) {
	f.mu.Lock()
	f.readBranches = append(f.readBranches, branch)
	fail := f.failReadBackDecode
	f.mu.Unlock()
	if fail {
		// The QA F36 fault: the committed draft decodes MALFORMED (free prose in the "trigger"
		// closed enum). The real GitStore codec classifies this TERMINAL (ContractMisuse) and
		// carries the wire-name diagnostic; retry cannot fix the immutable committed bytes.
		return projectstate.Project{}, fwra.New(fwra.ContractMisuse,
			`projectstate: decode slots: decode slot CoreUseCases model: "A commitment of any size appears, however it arrives, and is still held only in the person's memory." is not a recognized Trigger wire name`)
	}
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

// WithdrawArtifactOnBranch records the Withdraw on the SESSION BRANCH — the correct PR-rail
// substrate, where the draft was staged and the branch version matches. It delegates to
// the embedded fake's bookkeeping (withdrawn), then records the branch so the test asserts
// the withdraw rode the session branch (non-empty), not main. NOTE: the main-path
// WithdrawArtifact is intentionally NOT shadowed to fail — the FAILED-gate withdraw
// legitimately rides main (branch==""), so a blanket-failing shadow would break the
// not-green recovery test. The F30 regression guard is the withdrawBranches assertion (a
// regression to main leaves it empty).
func (f *branchAwareFakeProjectState) WithdrawArtifactOnBranch(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, key fwra.IdempotencyKey) (projectstate.Version, error) {
	f.mu.Lock()
	f.withdrawBranches = append(f.withdrawBranches, branch)
	f.mu.Unlock()
	return f.fakeProjectState.WithdrawArtifact(fwra.Context{Context: ctx, IdempotencyKey: key}, projectID, expectedVersion, kind, notes)
}

// WithdrawArtifact (MAIN path) is the base behavior UNLESS failWithdrawOnMain is armed, in
// which case it models the PR-rail reality that caused QA F30: main's slot is unpopulated,
// so a main-path withdraw is a ContractMisuse. Armed only by the F30 review-gate test so a
// regression to withdrawing on main crashes the workflow and the test fails loudly.
func (f *branchAwareFakeProjectState) WithdrawArtifact(rc fwra.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, kind projectstate.ArtifactKind, notes string) (projectstate.Version, error) {
	if f.failWithdrawOnMain {
		return 0, fwra.New(fwra.ContractMisuse, "projectstate.WithdrawArtifact: slot "+kind.String()+" is unpopulated (stage a model first)")
	}
	return f.fakeProjectState.WithdrawArtifact(rc, projectID, expectedVersion, kind, notes)
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

// THE QA F36 REGRESSION — a MALFORMED committed draft read-back. The design job reports
// success and CI validate is GREEN (its Go mirror types the "trigger" field as a free
// string), but the server codec REJECTS the free-prose value in that closed enum on
// read-back (a TERMINAL ContractMisuse). Pre-fix the read-back Activity retried the same
// immutable committed bytes every ~100s FOREVER, leaving the session wedged at Drafting
// with no failure surface. After the fix the terminal decode fault lands the session at the
// human-visible StageDraftFailed gate carrying the DECODE DIAGNOSTIC as the FailureReason,
// and suspends awaiting Retry/Withdraw. Withdraw ends clean with nothing staged/committed.
func Test_CoAuthor_RailEnabled_MalformedReadBack_LandsInStageDraftFailed_WithDecodeReason(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base, failReadBackDecode: true}
	pipe := newFakePipeline() // dispatch observed Succeeded; CI validate is green
	rail := &fakeRail{checkGreen: true}
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
		// The load-bearing anti-wedge assertion: NOT stuck at Drafting (the F36 wedge).
		if view.Stage == StageDrafting {
			t.Fatal("a malformed read-back must NOT leave the session in perpetual StageDrafting (F36 wedge)")
		}
		if view.Stage != StageDraftFailed {
			t.Fatalf("a terminal decode read-back must land in StageDraftFailed, got stage %d", view.Stage)
		}
		// The FailureReason must carry the DECODE DIAGNOSTIC (the wire-name rejection) so the
		// human sees WHY — not a generic "job failed" message.
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("StageDraftFailed from a malformed read-back must carry a FailureReason")
		}
		if !strings.Contains(*view.FailureReason, "is not a recognized Trigger wire name") {
			t.Fatalf("FailureReason must carry the decode diagnostic; got %q", *view.FailureReason)
		}
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindCoreUseCases})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete after withdraw from the decode-failed gate")
	}
	// A terminal decode-of-committed-state is contained at the Manager (human gate), NOT a
	// workflow crash and NOT an infinite retry loop.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a terminal decode read-back must NOT fail the workflow, got: %v", err)
	}
	if len(ps.stageBranches) != 0 {
		t.Fatalf("a malformed read-back must stage nothing, got %v", ps.stageBranches)
	}
	if len(base.committed) != 0 {
		t.Fatalf("a malformed read-back must commit nothing, got %v", base.committed)
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

// ---- f29BranchFake: version-enforcing branch-aware substrate (QA F29) --------

// f29BranchFake models the production reality F29 exposed: MAIN and the SESSION BRANCH
// sit at DIFFERENT versions. A fresh CoAuthor workflow captures main's version (mainVer),
// but the Action's prior draft/critique commits left a REUSED session branch AHEAD
// (branchVer). The read-back reads the BRANCH (branchVer); a stage that expects the stale
// main version Conflicts. Unlike the version-ignoring base fake, this fake ENFORCES the
// branch version on stage-on-branch, so a stale expected version surfaces the real
// fwra.Conflict — the exact non-retryable stage crash of F29 — and the test proves the fix
// converges. stageFailsRemaining injects terminal stage faults for the containment test.
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
	p.Version = f.branchVer // the dirty session branch is AHEAD of main
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
		// The real GitStore's optimistic-concurrency guard — the F29 Conflict.
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
	// Terminal in the F29 tests; leave branchVer untouched so the stage-convergence
	// assertion stays about the stage, not this withdraw.
	return f.branchVer, nil
}

// THE QA F29 REGRESSION — a fresh workflow reusing a session branch already AHEAD of main
// stages against the ACTUAL branch version and CONVERGES, instead of Conflicting
// non-recoverably against the stale main-captured version and crashing the workflow.
func Test_CoAuthor_RailEnabled_StageAgainstDirtyBranch_Converges_NoCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// main at v2; the reused session branch already advanced to v4 by prior draft/critique
	// commits — the exact "have 4, expected 2" split from the F29 report.
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	// F29 crashed the workflow (non-retryable Conflict → MutateConflictExhausted). The fix
	// must let it complete.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("staging against a dirty session branch must converge, not crash: %v", err)
	}
	// A stage SUCCEEDED (the branch version advanced past 4) — it converged on the branch.
	if ps.branchVer != 5 {
		t.Fatalf("stage must converge and advance the branch version to 5, got %d (expecteds=%v)", ps.branchVer, ps.stageExpecteds)
	}
	// The successful stage expected the ACTUAL branch version (4), never the stale main 2.
	last := ps.stageExpecteds[len(ps.stageExpecteds)-1]
	if last != 4 {
		t.Fatalf("the converged stage must expect the branch version 4, got %d (expecteds=%v)", last, ps.stageExpecteds)
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one commit after the converged stage → approve, got %v", base.committed)
	}
}

// CRASH CONTAINMENT AT THE STAGE STEP (QA F29 item 2). A terminal stage-for-review fault
// must NOT kill the workflow: the spine lands at the human-visible StageDraftFailed
// recovery gate. A Retry redrafts and the second stage converges.
func Test_CoAuthor_RailEnabled_StageFaults_RecoversAtFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// The FIRST stage faults terminally; the retry's stage converges.
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4, stageFailsRemaining: 1}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	// After the stage fault the session is at StageDraftFailed — assert the recoverable
	// gate (not a crash), then Retry.
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
			t.Fatalf("a faulted stage must land at StageDraftFailed, got stage %v", view.Stage)
		}
		if view.FailureReason == nil || *view.FailureReason == "" {
			t.Fatal("the failed gate must surface a human FailureReason")
		}
	}, 30*time.Second)
	// Retry via the redraft lever → re-draft → the second stage converges.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 45*time.Second)
	// After the recovered stage reaches AwaitingReview, Withdraw to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a faulted stage must not crash the workflow: %v", err)
	}
	// The recovered retry staged successfully (branch advanced past 4).
	if ps.branchVer != 5 {
		t.Fatalf("the recovered retry must converge its stage (branch → 5), got %d", ps.branchVer)
	}
}

// THE QA F30 REGRESSION — Withdraw against the PR rail records the Withdrawn status ON THE
// SESSION BRANCH (not main, where the version mismatches AND the slot is unpopulated), the
// workflow survives, and ends withdrawn. failWithdrawOnMain arms the main-path guard so a
// regression to withdrawing on main crashes the workflow and this test fails loudly.
func Test_CoAuthor_RailEnabled_Withdraw_RecordsOnSessionBranch_NoCrash(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base, failWithdrawOnMain: true}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	// At the AwaitingReview gate, WITHDRAW the not-yet-merged draft.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw, Feedback: &ReviewFeedback{Notes: "abandon this draft"}})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	// A rail-flow withdraw must NOT crash (F30 was a main-path ContractMisuse on the
	// unpopulated main slot).
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a PR-rail withdraw must not crash the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorWithdrawn {
		t.Fatalf("want CoAuthorWithdrawn, got %d", outcome)
	}
	// The Withdraw rode the SESSION BRANCH, not main.
	if len(ps.withdrawBranches) != 1 || ps.withdrawBranches[0] == "" {
		t.Fatalf("withdraw must target the session branch, got %v", ps.withdrawBranches)
	}
	if len(base.withdrawn) != 1 || base.withdrawn[0] != projectstate.KindSystem {
		t.Fatalf("want one WithdrawArtifact(KindSystem) on the session branch, got %v", base.withdrawn)
	}
}

// F40 — a Retry at the StageDraftFailed gate redrafts on the SAME persistent session
// branch (the F32 branch-per-retry topology is unwound; the stale-base problem is now
// handled by the workflow template's refresh-from-main git step, not a fresh branch). This
// drives a stage fault → retry-via-reject (with feedback) and asserts the redraft dispatch
// targets the SAME branch AND still carries the retained feedback.
func Test_CoAuthor_RailEnabled_RetryAtFailedGate_SameBranch_RetainsFeedback(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// The FIRST stage faults terminally → StageDraftFailed; the retry's stage converges.
	ps := &f29BranchFake{fakeProjectState: base, mainVer: 2, branchVer: 4, stageFailsRemaining: 1}
	pipe := newFakePipeline() // every dispatch Succeeds (the fault is at STAGE, not the job)
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	const retryNotes = "fix the layering violation before redrafting"
	// At the StageDraftFailed gate, Retry-via-Reject carrying feedback.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewReject, Feedback: &ReviewFeedback{Notes: retryNotes}})
	}, 30*time.Second)
	// After the recovered redraft reaches AwaitingReview, Withdraw to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

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
	// F40: the retry redrafts on the SAME persistent session branch (the template's
	// refresh-from-main handles a stale base; no per-attempt suffix).
	if b1 != b0 {
		t.Fatalf("a failed-gate retry must redraft on the SAME session branch (F40); got %q then %q", b0, b1)
	}
	if strings.Contains(b1, "-amend-") {
		t.Fatalf("the retry branch must be the stable session branch (no amendment suffix), got %q", b1)
	}
	// Retained feedback rides into the redraft prompt.
	if p := pipe.submits[len(pipe.submits)-1].dispatchInputs[dispatchInputDesignPrompt]; !strings.Contains(p, retryNotes) {
		t.Fatalf("the retained feedback %q must drive the redraft; prompt:\n%s", retryNotes, p)
	}
}

// THE QA F35 REGRESSION — an approve-window fault (GetPullRequestStatus 403 → Auth kind)
// must NOT kill the workflow. After the bounded retry budget is exhausted, the session
// RETURNS to AwaitingReview carrying a queryable notice (FailureReason), and a re-approve
// succeeds and merges. NOT a redraft (which would discard the approved-quality draft).
func Test_CoAuthor_RailEnabled_ApproveStatusFault_ReturnsToAwaitingReview_ReapproveMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	// The first approve's GetPullRequestStatus 403s on all 3 bounded attempts → contained;
	// after that the counter is 0 so the re-approve reads green and merges.
	rail := &fakeRail{checkGreen: true, statusAuthFailsRemaining: 3}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	// First approve → status 403s → contained → back to AwaitingReview with a notice.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)
	// Assert the session returned to AwaitingReview with a queryable re-approve notice.
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
	// Re-approve → now the status reads green → merge + commit.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 90*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

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
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("exactly one merge (on the successful re-approve), got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 || base.committed[0] != projectstate.KindSystem {
		t.Fatalf("want one commit after re-approve, got %v", base.committed)
	}
}

// BOUNDED RESILIENCE (QA F35). Two transient 403s then success: the bounded retry absorbs
// them and the merge completes on the FIRST approve — no return-to-review, no crash.
func Test_CoAuthor_RailEnabled_ApproveStatusTransient_RetriesThenMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true, statusAuthFailsRemaining: 2} // fail twice, 3rd succeeds
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

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
	// GetPullRequestStatus was attempted 3 times (2 faults + 1 success) within the budget.
	if n := rail.verbCount("GetPullRequestStatus"); n != 3 {
		t.Fatalf("want 3 bounded GetPullRequestStatus attempts (2 fault + 1 success), got %d", n)
	}
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("the merge must complete on the first approve, got %d merges", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 {
		t.Fatalf("want one commit on the first approve, got %v", base.committed)
	}
}
