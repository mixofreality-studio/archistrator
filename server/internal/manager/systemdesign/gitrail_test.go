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
	// openPRAuthFailsRemaining, when >0, makes OpenPullRequest return an fwra.Auth error
	// (the same rate-limit-403-as-Auth) and decrement — the QA F35 TWIN in the draft
	// round-trip. Set to railAuthRetryMaxAttempts (3) to exhaust the bounded retry so the
	// FIRST openPR faults and lands at the failed gate; the resume openPR then succeeds.
	openPRAuthFailsRemaining int
	// syncErr, when non-nil, makes SyncManagedScaffold fail terminally — exercises the
	// managed-scaffold-sync containment (dispatch BLOCKED, session lands at the failed
	// gate, NO design job submitted).
	syncErr error
	// syncChanged scripts the drift report (true ⇔ the seated scaffold drifted and the
	// sync "committed" a refresh).
	syncChanged bool
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

func (r *fakeRail) GetInstallationToken(_ fwra.Context, repo sourcecontrol.RepoRef) (sourcecontrol.RepoCredential, error) {
	r.record(railCall{verb: "GetInstallationToken", repo: sourcecontrol.RepoRefString(repo)})
	return sourcecontrol.RepoCredential{Bytes: []byte("tok"), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// CommitManagedFiles backs the managed-scaffold sync: sourcecontrol.SyncManagedScaffold
// (the free-function composition helper the custom SyncManagedScaffoldActivity wraps)
// reaches the rail through this verb for a non-managedFileSyncer fake. It records the sync
// call under the "SyncManagedScaffold" counter and honors the scripted syncErr.
func (r *fakeRail) CommitManagedFiles(_ fwra.Context, repo sourcecontrol.RepoRef, _ []sourcecontrol.ManagedFile, _ sourcecontrol.RepoCredential) (sourcecontrol.CommitRef, error) {
	r.record(railCall{verb: "SyncManagedScaffold", repo: sourcecontrol.RepoRefString(repo)})
	r.mu.Lock()
	err := r.syncErr
	r.mu.Unlock()
	if err != nil {
		return sourcecontrol.CommitRef(""), err
	}
	return sourcecontrol.CommitRef("scaffold-sync"), nil
}

func (r *fakeRail) OpenBranch(_ fwra.Context, repo sourcecontrol.RepoRef, branch sourcecontrol.BranchName, _ sourcecontrol.RepoCredential) (sourcecontrol.BranchRef, error) {
	r.record(railCall{verb: "OpenBranch", repo: sourcecontrol.RepoRefString(repo), branch: string(branch)})
	// The Manager discards the BranchRef (it only ensures the branch exists); a zero
	// ref is fine — the workflow never re-materializes a branch handle.
	return sourcecontrol.BranchRef(""), nil
}

func (r *fakeRail) OpenPullRequest(_ fwra.Context, repo sourcecontrol.RepoRef, spec sourcecontrol.PullRequestSpec, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestRef, error) {
	r.record(railCall{verb: "OpenPullRequest", repo: sourcecontrol.RepoRefString(repo), branch: string(spec.Head), prRef: "pr/" + string(spec.Head)})
	r.mu.Lock()
	fail := r.openPRAuthFailsRemaining > 0
	if fail {
		r.openPRAuthFailsRemaining--
	}
	r.mu.Unlock()
	if fail {
		// The observed F35-twin fault: OpenPullRequest hits a GitHub secondary rate-limit 403
		// the platform classifier reports as Auth. The shared bounded retry absorbs it; a
		// persistent one lands the draft round-trip at the failed gate (draft preserved).
		return sourcecontrol.PullRequestRefFromString(""), fwra.New(fwra.Auth, "openPullRequest: github auth/permission denied")
	}
	r.mu.Lock()
	r.openedPRs++
	prRef := "pr/" + string(spec.Head)
	r.mu.Unlock()
	return sourcecontrol.PullRequestRefFromString(prRef), nil
}

func (r *fakeRail) GetPullRequestStatus(_ fwra.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.PullRequestStatus, error) {
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

func (r *fakeRail) PostReview(_ fwra.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.ReviewSubmission, _ sourcecontrol.RepoCredential) error {
	r.record(railCall{verb: "PostReview", repo: sourcecontrol.RepoRefString(repo), prRef: sourcecontrol.PullRequestRefString(pr)})
	return nil
}

func (r *fakeRail) MergePullRequest(_ fwra.Context, repo sourcecontrol.RepoRef, pr sourcecontrol.PullRequestRef, _ sourcecontrol.RepoCredential) (sourcecontrol.MergeResult, error) {
	r.record(railCall{verb: "MergePullRequest", repo: sourcecontrol.RepoRefString(repo), prRef: sourcecontrol.PullRequestRefString(pr)})
	return sourcecontrol.MergeResult{Merged: true, Commit: "merged"}, nil
}

// The remaining SourceControlAccess ops are outside the design PR-rail lifecycle; the stub
// satisfies the full contract with inert implementations so it can back the GENERATED rail
// Activities registered via genActivities.
func (r *fakeRail) AdoptProjectRepo(_ fwra.Context, _ sourcecontrol.RepoAdoptionSpec) (sourcecontrol.RepoRef, error) {
	return sourcecontrol.RepoRef(""), nil
}

func (r *fakeRail) ConfigureBranchProtection(_ fwra.Context, _ sourcecontrol.RepoRef, _ sourcecontrol.RepoCredential) error {
	return nil
}

func (r *fakeRail) InstallAuthorizeApp(_ fwra.Context, _ sourcecontrol.AccountRef) (sourcecontrol.Installation, error) {
	return sourcecontrol.Installation(""), nil
}

var _ sourcecontrol.SourceControlAccess = (*fakeRail)(nil)

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
	// branchAdvancedModel, when non-nil, is the SystemDesign slot model the branch read-back
	// returns instead of main's — modeling an AMENDMENT whose Action actually CHANGED the
	// artifact (so sameArtifactModel(branch, main) is false and the F40 no-change guard does
	// NOT trip). Left nil, the branch read-back returns main's model verbatim (identical),
	// which trips the amendment no-change guard — the observed zero-new-commit scenario.
	branchAdvancedModel projectstate.ArtifactModel
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
	proj, err := f.ReadProject(fwra.Context{Context: ctx}, projectID)
	if err != nil {
		return projectstate.Project{}, err
	}
	f.mu.Lock()
	adv := f.branchAdvancedModel
	f.mu.Unlock()
	if adv != nil {
		// The amendment's Action changed the artifact on the branch — serve a branch model
		// distinct from main so the no-change guard sees advancement and proceeds.
		proj.SystemDesign = awaitingSlot(adv, "", "")
	}
	return proj, nil
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

func newRailWorkflows(ps projectstate.ProjectStateAccess, pipe *fakePipeline, rail sourcecontrol.SourceControlAccess) *workflows {
	_ = pipe // threaded to registerGenActivities, not stored on the struct.
	return &workflows{
		ProjectState: ps,
		Acts:         genInvokers{Opts: activityOptions()},
		Rail:         rail,
		Repo: func(ProjectID) (sourcecontrol.RepoRef, bool) {
			return sourcecontrol.RepoRefFromString("acct|owner/repo"), true
		},
	}
}

func registerRailCoAuthor(env *testsuite.TestWorkflowEnvironment, wf *workflows, pipe *fakePipeline) {
	env.RegisterWorkflowWithOptions(wf.CoAuthorArtifactWorkflow, workflow.RegisterOptions{Name: executionKindCoAuthor})
	env.RegisterActivity(wf.ReadProjectActivity)
	env.RegisterActivity(wf.ReadProjectOnBranchActivity)
	env.RegisterActivity(wf.StageArtifactForReviewActivity)
	env.RegisterActivity(wf.CommitArtifactActivity)
	env.RegisterActivity(wf.RejectArtifactActivity)
	env.RegisterActivity(wf.WithdrawArtifactActivity)
	env.RegisterActivity(wf.SeedReviewCommentsActivity)
	env.RegisterActivity(wf.SyncManagedScaffoldActivity)
	registerGenActivities(env, wf.ProjectState, pipe, wf.Rail)
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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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
	registerRailCoAuthor(env, wf, pipe)

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

// THE F38 AMENDMENT REGRESSION — reopening a COMMITTED artifact starts a fresh session on a
// …-amend-N branch and the draft prompt states it AMENDS the committed version. Driven by
// coAuthorInput.Amendment (which RequestArtifactDraft sets from the committed slot's
// Revisions). The reopening feedback rides coAuthorInput.Feedback.
func Test_CoAuthor_RailEnabled_Amendment_UsesAmendBranchAndPrompt(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)

	// Amendment 1 with anchored reopening feedback (the "why").
	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindSystem,
		Amendment:    1,
		Feedback:     &ReviewFeedback{Comments: []AnchoredComment{{JSONPath: "$.containers[0].name", Text: "rename this manager"}}},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("amendment session must not crash: %v", err)
	}
	if len(pipe.submits) == 0 {
		t.Fatal("amendment must dispatch a draft")
	}
	// The draft rode a fresh …-amend-1 branch (F38 + F40 stable within the amendment session).
	b := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if !strings.HasSuffix(b, "-amend-1") {
		t.Fatalf("amendment 1 must draft on a …-amend-1 branch, got %q", b)
	}
	// The prompt states it amends the committed version.
	if p := pipe.submits[0].dispatchInputs[dispatchInputDesignPrompt]; !strings.Contains(p, "AMENDMENT (revision 1)") {
		t.Fatalf("amendment prompt must state it amends the committed version; prompt:\n%s", p)
	}
}

// composeIdempotencyKey RUN-SCOPING (F40 root cause) — two sessions of the SAME workflow
// ID with the SAME ActivityID but DIFFERENT RunID must derive DISTINCT keys, so the
// constructionpipeline RA's run-name dedup (sha256(key)) never converges a fresh session
// onto a predecessor session's completed GitHub run. Pre-fix the key was "wfID:activityID"
// and these two COLLIDED (ActivityIDs restart from 1 on every new run of a deterministic
// workflow ID) → no new run dispatched → observe saw stale success → OpenPullRequest 422'd.
func Test_composeIdempotencyKey_RunScoped_DistinctPerRun(t *testing.T) {
	const wf = "gtdapp:1" // a deterministic workflow ID (note: itself contains a colon)
	const act = "5"       // ActivityIDs restart per run, so this repeats across sessions
	k1 := composeIdempotencyKey(wf, "run-aaaa", act)
	k2 := composeIdempotencyKey(wf, "run-bbbb", act)
	if k1 == k2 {
		t.Fatalf("distinct RunIDs must yield distinct idempotency keys (else the RA dedups a fresh session onto a predecessor's GitHub run); both = %q", k1)
	}
	// The RunID must actually appear in the key (it is the session-scoping segment).
	if !strings.Contains(string(k1), "run-aaaa") || !strings.Contains(string(k2), "run-bbbb") {
		t.Fatalf("the key must carry the RunID as its session scope; got %q / %q", k1, k2)
	}
	// A transient auto-retry of ONE invocation (same run, same ActivityID) must still
	// COLLAPSE to the same key so the RA/ledger dedups the retry.
	if composeIdempotencyKey(wf, "run-aaaa", act) != k1 {
		t.Fatal("same (workflowID, runID, activityID) must be stable so a transient retry dedups")
	}
}

// F40 dispatch key is RUN-SCOPED end-to-end — the DispatchDesignJobActivity composes the
// key from activity.GetInfo, which now includes the RunID. Asserted through the fake
// pipeline's captured key (the test env pins a fixed run id, so we assert its presence).
func Test_CoAuthor_DispatchKey_CarriesRunID(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline(pipelineFailed) // fail after the first dispatch so it lands quickly
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 30*time.Second)
	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if len(pipe.submits) == 0 {
		t.Fatal("expected a dispatch")
	}
	key := string(pipe.submits[0].idempotencyKey)
	// The run id must be a segment of the key (run-scoping) — not merely wfID:activityID.
	if !strings.Contains(key, ":default-test-run-id:") {
		t.Fatalf("dispatch idempotency key must be run-scoped (contain the RunID segment), got %q", key)
	}
}

// F40 AMENDMENT NO-CHANGE GUARD — an amendment session whose Action ran and "succeeded"
// but committed NOTHING that changed the artifact (branch read-back == committed main
// model) must land at the StageDraftFailed gate with the honest reason and open NO PR
// (opening one would 422 on the zero-new-commit branch). Withdraw ends clean.
func Test_CoAuthor_Rail_Amendment_NoChange_LandsFailedGate_NoPR(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	// branchAdvancedModel left nil ⇒ branch read-back == main ⇒ no advancement.
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // the design job "succeeds"
	rail := &fakeRail{checkGreen: true}
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
		ArtifactKind: KindSystem,
		Amendment:    1,
		Feedback:     &ReviewFeedback{Notes: "please tighten"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("no-change amendment must not crash: %v", err)
	}
	// The dispatch + read-back ran, but NO PR was opened (the branch never advanced).
	if pipe.submits[0].dispatchInputs[dispatchInputTargetBranch] == "" {
		t.Fatal("the amendment must still dispatch a draft")
	}
	if rail.verbCount("OpenPullRequest") != 0 {
		t.Fatalf("a no-change amendment must open NO PR (zero-new-commit branch), got %d", rail.verbCount("OpenPullRequest"))
	}
	if rail.verbCount("MergePullRequest") != 0 {
		t.Fatalf("a no-change amendment must NOT merge, got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 0 {
		t.Fatalf("a no-change amendment must commit nothing, got %v", base.committed)
	}
}

// F40 AMENDMENT POSITIVE CONTROL — an amendment whose Action DID change the artifact
// (branch read-back differs from committed main) must NOT trip the no-change guard: it
// opens the PR and, on approve, merges + commits. Proves the guard blocks only genuine
// no-ops, not every amendment.
func Test_CoAuthor_Rail_Amendment_Advanced_OpensPR_Merges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{
		fakeProjectState:    base,
		branchAdvancedModel: &projectstate.System{Components: []projectstate.Component{{}}}, // differs from main's empty System
	}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindSystem,
		Amendment:    1,
		Feedback:     &ReviewFeedback{Notes: "add a manager"},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("advanced amendment must not crash: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("an advanced amendment that is approved must merge+commit, got outcome %d", outcome)
	}
	if rail.verbCount("OpenPullRequest") == 0 {
		t.Fatal("an advanced amendment must OPEN a PR (the branch moved beyond main)")
	}
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("approve must merge the amendment PR once, got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 {
		t.Fatalf("approve must commit the amendment once, got %v", base.committed)
	}
}

// amendmentIndexFor — the pre-field fix rule. A COMMITTED slot yields an amendment index of
// max(1, Revisions): a slot committed BEFORE the Revisions field existed reads Revisions=0
// yet is still an amendment (index 1). Non-committed slots are the normal path (0).
func Test_amendmentIndexFor_Rule(t *testing.T) {
	// Pre-field committed slot (the observed gtdapp glossary case): Revisions 0 → index 1.
	if got := amendmentIndexFor(projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 0}); got != 1 {
		t.Fatalf("pre-field committed slot must yield amendment index 1, got %d", got)
	}
	// A committed slot with a real revision count returns it.
	if got := amendmentIndexFor(projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Revisions: 3}); got != 3 {
		t.Fatalf("committed slot at revision 3 must yield amendment index 3, got %d", got)
	}
	// Non-committed slots are NOT amendments regardless of any stray Revisions value.
	for _, st := range []projectstate.ArtifactReviewStatus{
		projectstate.ReviewNone, projectstate.ReviewAwaitingReview, projectstate.ReviewRejected, projectstate.ReviewWithdrawn,
	} {
		if got := amendmentIndexFor(projectstate.ArtifactSlot{Status: st, Revisions: 5}); got != 0 {
			t.Fatalf("non-committed slot (status %d) must yield amendment index 0, got %d", st, got)
		}
	}
}

// ledgerFakeProjectState is the branch-aware fake PLUS the review-ledger seam, so the
// amendment SEED (SeedReviewCommentsActivity → SeedReviewCommentsOnBranch) actually FIRES
// and is observable. It is a SEPARATE type (not the shared branchAwareFakeProjectState) so
// existing reject tests — which assert on the non-ledger RejectArtifactOnBranch recorder —
// are unaffected by the ledger routing.
type ledgerFakeProjectState struct {
	*branchAwareFakeProjectState
	seededRounds   []int64
	seededComments [][]projectstate.ReviewComment
}

var _ projectstate.LedgerProjectStateAccess = (*ledgerFakeProjectState)(nil)

func (f *ledgerFakeProjectState) SeedReviewCommentsOnBranch(_ context.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, _ string, _ projectstate.ArtifactKind, round int64, comments []projectstate.ReviewComment, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	f.seededRounds = append(f.seededRounds, round)
	f.seededComments = append(f.seededComments, comments)
	return expectedVersion, nil
}

func (f *ledgerFakeProjectState) RejectArtifactOnBranchWithComments(ctx context.Context, projectID projectstate.ProjectID, expectedVersion projectstate.Version, branch string, kind projectstate.ArtifactKind, notes string, _ int64, _ []projectstate.ReviewComment, key fwra.IdempotencyKey) (projectstate.Version, error) {
	return f.RejectArtifactOnBranch(ctx, projectID, expectedVersion, branch, kind, notes, key)
}

func (f *ledgerFakeProjectState) SetReviewCommentStatusOnBranch(_ context.Context, _ projectstate.ProjectID, expectedVersion projectstate.Version, _ string, _ projectstate.ArtifactKind, _ string, _ string, _ fwra.IdempotencyKey) (projectstate.Version, error) {
	return expectedVersion, nil
}

// F38 PRE-FIELD AMENDMENT (the observed gtdapp glossary bug) — a slot COMMITTED before the
// Revisions field existed (Status=Committed, Revisions=0) must run the FULL amendment path:
// the index computes to 1 (amendmentIndexFor), the draft rides a …-amend-1 branch with the
// amendment prompt framing, AND the reopening feedback is SEEDED into the review ledger
// (round 0). Pre-fix it computed Amendment=0 → a normal draft on the canonical branch with
// NO seed.
func Test_CoAuthor_Rail_Amendment_PreFieldCommittedSlot_AmendBranch_Prompt_SeedFires(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	// A PRE-FIELD committed SystemDesign slot: committedSlot sets Status=Committed, Revisions=0.
	proj := systemReadBack(t, id)
	proj.SystemDesign = committedSlot(&projectstate.System{})
	base := &fakeProjectState{project: proj}

	// The index the manager WOULD compute for this pre-field slot must be 1 (not 0).
	if got := amendmentIndexFor(proj.SystemDesign); got != 1 {
		t.Fatalf("a pre-field committed slot must compute amendment index 1, got %d", got)
	}

	// The branch advances the artifact (so the no-change guard passes) and the ledger records seeds.
	ps := &ledgerFakeProjectState{
		branchAwareFakeProjectState: &branchAwareFakeProjectState{
			fakeProjectState:    base,
			branchAdvancedModel: &projectstate.System{Components: []projectstate.Component{{}}},
		},
	}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	// Drive the workflow with the COMPUTED index (1), as the manager would.
	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{
		ProjectID:    id,
		ArtifactKind: KindSystem,
		Amendment:    amendmentIndexFor(proj.SystemDesign),
		Feedback:     &ReviewFeedback{Comments: []AnchoredComment{{JSONPath: "$.components[0].name", Text: "rename this manager"}}},
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("pre-field amendment must not crash: %v", err)
	}
	if len(pipe.submits) == 0 {
		t.Fatal("the amendment must dispatch a draft")
	}
	// -amend-1 branch (NOT the canonical branch).
	b := pipe.submits[0].dispatchInputs[dispatchInputTargetBranch]
	if !strings.HasSuffix(b, "-amend-1") {
		t.Fatalf("a pre-field committed slot must draft on a …-amend-1 branch, got %q", b)
	}
	// Amendment prompt framing.
	if p := pipe.submits[0].dispatchInputs[dispatchInputDesignPrompt]; !strings.Contains(p, "AMENDMENT (revision 1)") {
		t.Fatalf("the amendment prompt must frame it as revision 1; prompt:\n%s", p)
	}
	// THE LOAD-BEARING FIX: the reopening feedback was SEEDED into the review ledger (round 0).
	if len(ps.seededRounds) == 0 {
		t.Fatal("the amendment SEED must fire for a pre-field committed slot (pre-fix it did not)")
	}
	if ps.seededRounds[0] != 0 {
		t.Fatalf("the reopening feedback must seed as round 0, got round %d", ps.seededRounds[0])
	}
	if len(ps.seededComments) == 0 || len(ps.seededComments[0]) == 0 {
		t.Fatal("the seed must carry the reopening comments as OPEN ledger entries")
	}
}

// F35 TWIN (the draft-round-trip openPR fault) — a GREEN draft + successful read-back, then
// OpenPullRequest persistently Auth-faults (secondary-rate-limit-403-as-Auth) past the shared
// bounded retry. The whole CoAuthor workflow must NOT die (as it did live on gtdapp kind 5):
// it CONTAINS the fault at the StageDraftFailed gate, and on Retry it RESUMES from the
// read-back — WITHOUT a second dispatch (which would burn another 20+ min draft and red the
// no-commit guard) — re-opens the PR, and Approve merges.
func Test_CoAuthor_Rail_OpenPRAuthFault_ContainsAtGate_RetryResumesNoRedispatch_ThenMerges(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline() // the draft job succeeds (a green 20+ min draft)
	// OpenPullRequest Auth-faults for all railAuthRetryMaxAttempts of the FIRST openPR, so the
	// bounded retry exhausts and the round-trip lands at the failed gate; the resume openPR succeeds.
	rail := &fakeRail{checkGreen: true, openPRAuthFailsRemaining: railAuthRetryMaxAttempts}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	// At the failed gate: assert StageDraftFailed with the honest openPR reason, then RETRY.
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
			t.Fatalf("a persistent openPR Auth fault must CONTAIN at StageDraftFailed, got stage %d", view.Stage)
		}
		reason := ""
		if view.FailureReason != nil {
			reason = *view.FailureReason
		}
		if !strings.Contains(reason, "pull request") {
			t.Fatalf("the failed gate must name the openPR step honestly, got %q", reason)
		}
		// Only ONE dispatch so far — the draft is preserved on the branch.
		if len(pipe.submits) != 1 {
			t.Fatalf("before retry there must be exactly ONE dispatch, got %d", len(pipe.submits))
		}
		env.SignalWorkflow(lSignalRedraft, redraftSignal{})
	}, 40*time.Second)

	// After the resume re-stages, Approve → merge.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 90*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("an openPR Auth fault must NOT kill the workflow: %v", err)
	}
	var outcome coAuthorOutcome
	if err := env.GetWorkflowResult(&outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	if outcome != coAuthorApproved {
		t.Fatalf("after resume + approve the session must be Approved, got %d", outcome)
	}

	// THE LOAD-BEARING ASSERTION: the Retry RESUMED from read-back — NO second dispatch.
	if len(pipe.submits) != 1 {
		t.Fatalf("the retry must NOT re-dispatch (resume from read-back); got %d dispatches", len(pipe.submits))
	}
	// OpenPullRequest was attempted railAuthRetryMaxAttempts times (all faulting) in the first
	// round + once more on the resume (success) = maxAttempts+1.
	if got, want := rail.verbCount("OpenPullRequest"), railAuthRetryMaxAttempts+1; got != want {
		t.Fatalf("OpenPullRequest attempts: got %d, want %d (%d bounded-retry faults + 1 resume success)", got, want, railAuthRetryMaxAttempts)
	}
	// Exactly one PR was actually opened (the resume success), then merged, then committed once.
	if rail.openedPRs != 1 {
		t.Fatalf("exactly one PR must actually open (on the resume), got %d", rail.openedPRs)
	}
	if rail.verbCount("MergePullRequest") != 1 {
		t.Fatalf("approve must merge once, got %d", rail.verbCount("MergePullRequest"))
	}
	if len(base.committed) != 1 {
		t.Fatalf("approve must commit once, got %v", base.committed)
	}
}

// F47 — the REDRAFT-SIGNAL feedback path (what RequestArtifactDraft delivers via
// SignalWithStart). A draft job fails → StageDraftFailed gate; the redraft signal carries the
// operator's fix notes; the NEXT draft dispatch's prompt must CONTAIN those notes (they were
// dropped live on the retry-at-failed-gate path). Complements the retry-via-reject test.
func Test_CoAuthor_RailEnabled_RedraftSignalFeedbackReachesPrompt(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline(pipelineFailed) // the draft job fails → the StageDraftFailed gate
	rail := &fakeRail{checkGreen: true}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	const notes = "resources must be plain strings, not objects"
	// At the failed gate, deliver the REDRAFT signal (the RequestArtifactDraft path) with notes.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(lSignalRedraft, redraftSignal{Feedback: &ReviewFeedback{Notes: notes}})
	}, 30*time.Second)
	// Back at the gate after the second (also-failed) dispatch, withdraw to end.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewWithdraw})
	}, 80*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a redraft-signal retry must not crash: %v", err)
	}
	if len(pipe.submits) < 2 {
		t.Fatalf("the redraft signal must trigger a SECOND draft dispatch, got %d", len(pipe.submits))
	}
	// THE LOAD-BEARING ASSERTION: the redraft-signal feedback reached the next draft prompt.
	if p := pipe.submits[1].dispatchInputs[dispatchInputDesignPrompt]; !strings.Contains(p, notes) {
		t.Fatalf("the redraft-signal feedback %q must reach the next draft prompt; prompt:\n%s", notes, p)
	}
}

// F48 — the Temporal activity-boundary codec MUST carry the durable review ledger (audited from
// projectdesign; systemdesign had the same hole). Without it, loadReviewThread reads the session
// branch through this projectEnvelope and returns [] despite the reject-append living in git.
func Test_projectEnvelope_PreservesReviewThread(t *testing.T) {
	id := ProjectID(uuid.NewString())
	p := systemReadBack(t, id)
	p.SystemDesign = projectstate.ArtifactSlot{
		Status: projectstate.ReviewAwaitingReview,
		Model:  &projectstate.System{},
		ReviewThread: []projectstate.ReviewComment{
			{ID: "r0c1", Text: "split this Manager per volatility", AuthorRole: "architect", Round: 0, Status: projectstate.ReviewCommentOpen},
		},
	}
	env, err := encodeProject(p)
	if err != nil {
		t.Fatalf("encodeProject: %v", err)
	}
	got, err := env.decode()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	thread := slotFor(got, KindSystem).ReviewThread
	if len(thread) != 1 {
		t.Fatalf("the review thread must survive the Temporal codec round-trip, got %d comments: %+v", len(thread), thread)
	}
	if thread[0].ID != "r0c1" || thread[0].Status != projectstate.ReviewCommentOpen || thread[0].Text == "" {
		t.Fatalf("the codec must preserve the comment's id/status/text, got %+v", thread[0])
	}
}

// ---------------------------------------------------------------------------
// MANAGED-SCAFFOLD SYNC (sync-on-dispatch, 2026-07-06). The seated aiarch-design.yml
// is converged onto the CURRENT template rendering BEFORE any design job is
// dispatched; a sync failure BLOCKS the dispatch (never run a design job against a
// scaffold the server could not prove current — the gtdapp stale-pin / F81 incident).
// ---------------------------------------------------------------------------

// firstCallIndex returns the index of the first recorded rail call with verb, or -1.
func (r *fakeRail) firstCallIndex(verb string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, c := range r.calls {
		if c.verb == verb {
			return i
		}
	}
	return -1
}

// THE SYNC ORDER. The managed-scaffold sync runs in the dispatch-time rail half,
// BEFORE OpenBranch and therefore before any design job is dispatched; a drifted
// scaffold (syncChanged=true) refreshes and the spine proceeds normally to Approve.
func Test_CoAuthor_Rail_ScaffoldSync_RunsBeforeDispatch(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true, syncChanged: true} // the seated scaffold DRIFTED
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a drifted-then-refreshed scaffold must not perturb the spine: %v", err)
	}
	if rail.verbCount("SyncManagedScaffold") != 1 {
		t.Fatalf("want exactly one managed-scaffold sync per dispatch-time session begin, got %d", rail.verbCount("SyncManagedScaffold"))
	}
	iSync, iBranch := rail.firstCallIndex("SyncManagedScaffold"), rail.firstCallIndex("OpenBranch")
	if iSync < 0 || iBranch < 0 || iSync > iBranch {
		t.Fatalf("the managed-scaffold sync must run BEFORE OpenBranch (pre-dispatch), got sync=%d openBranch=%d (calls: %+v)", iSync, iBranch, rail.calls)
	}
	if len(pipe.submits) != 1 {
		t.Fatalf("the design job must still dispatch exactly once after a successful sync, got %d", len(pipe.submits))
	}
	if len(base.committed) != 1 {
		t.Fatalf("the approved spine must commit once, got %v", base.committed)
	}
}

// THE SYNC GATE. A managed-scaffold sync failure BLOCKS the dispatch: NO design job is
// submitted, NO session branch is opened, and the session lands at the human-visible
// StageDraftFailed gate (contained, never a crash). Withdraw ends clean.
func Test_CoAuthor_Rail_ScaffoldSyncFailure_BlocksDispatch_LandsAtFailedGate(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	rail := &fakeRail{checkGreen: true, syncErr: fwra.New(fwra.ContractMisuse, "seated workflow could not be refreshed")}
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

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed managed-scaffold sync must be CONTAINED at the failed gate, not crash: %v", err)
	}
	if got := rail.verbCount("SyncManagedScaffold"); got == 0 {
		t.Fatal("the managed-scaffold sync must have been attempted")
	}
	// The dispatch was BLOCKED: no design job submitted, no session branch, no PR.
	if len(pipe.submits) != 0 {
		t.Fatalf("a failed sync must BLOCK the design-job dispatch, got %d submits", len(pipe.submits))
	}
	if rail.verbCount("OpenBranch") != 0 || rail.verbCount("OpenPullRequest") != 0 {
		t.Fatalf("a failed sync must not open a branch/PR, got openBranch=%d openPR=%d",
			rail.verbCount("OpenBranch"), rail.verbCount("OpenPullRequest"))
	}
	// Nothing staged/committed; withdraw from the failed gate recorded once.
	if len(base.staged) != 0 || len(base.committed) != 0 {
		t.Fatalf("a blocked dispatch must stage/commit nothing, got staged=%d committed=%v", len(base.staged), base.committed)
	}
	if len(base.withdrawn) != 1 {
		t.Fatalf("withdraw from the failed gate must call WithdrawArtifact once, got %d", len(base.withdrawn))
	}
}

// THE VERSION GATE (live regression, gtdapp:5). The sync activity was added to
// beginSession AFTER CoAuthor sessions shipped; an execution already in flight at
// deploy time has no history event for it, so it must NEVER issue the sync command —
// workflow.GetVersion("managed-scaffold-sync") pins such executions (DefaultVersion)
// to the OLD command sequence. This test simulates a pre-feature execution via the
// testsuite's GetVersion mock, arms syncErr so an UN-GATED sync would derail the run
// at the failed gate, and proves the spine completes the full pre-feature happy path
// with ZERO SyncManagedScaffold calls. If the gate is ever removed (or its changeID
// renamed), the armed syncErr fails this test loudly.
func Test_CoAuthor_Rail_ScaffoldSync_VersionGate_PreFeatureExecutionSkipsSync(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	id := ProjectID(uuid.NewString())
	base := &fakeProjectState{project: systemReadBack(t, id)}
	ps := &branchAwareFakeProjectState{fakeProjectState: base}
	pipe := newFakePipeline()
	// syncErr armed: if the sync ran despite DefaultVersion, the session would land at
	// the failed gate and the approve below would never commit — a loud failure.
	rail := &fakeRail{checkGreen: true, syncErr: fwra.New(fwra.ContractMisuse, "sync must not run for a pre-feature execution")}
	wf := newRailWorkflows(ps, pipe, rail)
	registerRailCoAuthor(env, wf, pipe)

	// Simulate a PRE-FEATURE in-flight execution: GetVersion resolves DefaultVersion
	// (no version marker in the replayed history).
	env.OnGetVersion("managed-scaffold-sync", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(signalReviewDecision, reviewDecisionSignal{Decision: ReviewApprove})
	}, 30*time.Second)

	env.ExecuteWorkflow(executionKindCoAuthor, coAuthorInput{ProjectID: id, ArtifactKind: KindSystem})

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
	if got := rail.verbCount("SyncManagedScaffold"); got != 0 {
		t.Fatalf("a pre-feature (DefaultVersion) execution must NEVER call SyncManagedScaffold, got %d", got)
	}
	if len(pipe.submits) != 1 || len(base.committed) != 1 {
		t.Fatalf("the pre-feature spine must dispatch and commit exactly once, got submits=%d committed=%v", len(pipe.submits), base.committed)
	}
}
