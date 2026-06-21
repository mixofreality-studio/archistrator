package usecases

import (
	"context"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// I-UC1-AG — UC1 (system design) re-proven END-TO-END over the AGENTIC dispatch
// path, at the systemtests black-box level. The REAL server binary is booted into
// the agentic design path (sourceControlAccess + constructionPipelineAccess wired
// against the AgenticGitHub fake; the project-state on the LOCAL file:// repo) and
// driven through the PUBLISHED HTTP routes only. The external agentic-job + GitHub
// PR seam is FAKED at the systemtests boundary (the test constitution: real server,
// fake only the external claude-code-action + GitHub seam — no internal-component
// faking).
//
// This is the agentic twin of usecases/git_e2e_test.go's
// Test_GitE2E_UC1_DesignArtifactCommitsToGit, which proved the OLD synchronous
// (cassette) worker path. Here the design process DISPATCHES an agentic job (proven
// by the fake's DispatchCount), the fake job commits the typed draft to the session
// branch, the server observes + reads it back, the session reaches AwaitingReview
// with the read-back draft, the human approves through the route, the server merges
// the PR (fake), and the read-back from MAIN reflects the committed artifact in
// .aiarch/state/project.json — the approve→commit leg is HARD here (the fake commits
// a FIXED draft, unlike the offline-cassette limitation of the old tests).

// Test_UC1_Agentic_E2E_DispatchObserveGateMergeCommit drives UC1 end to end over
// the agentic path for an ARCHITECT-OWNED kind (volatilities — no PM critique),
// asserting the AGENTIC path (not the old worker path) at every leg.
func Test_UC1_Agentic_E2E_DispatchObserveGateMergeCommit(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	const account = "aiarch-test-org"
	const kind = "volatilities" // architect-owned: no PM-critique round-trip

	// The on-disk file:// project repo IS the server's LOCAL project-state substrate
	// AND the repo the agentic fake commits the draft into (one repo, two readers).
	projRepo := harness.StartLocalGitRepo(t, "main")
	fake := harness.StartAgenticGitHub(t, projRepo, account)
	appKey := harness.GenerateAppKeyPEM(t)

	srv := startServerWithEnv(t, true /* devAuth */, fake.Env(projRepo, appKey))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	// createProject — name-as-identity: the project id IS the repo name. In the
	// agentic config this adopts the repo (fake REST) + seats the workflow file (fake
	// REST) + commits project birth to the LOCAL file:// repo.
	// Unique per-run project name: the WorkflowID is {projectID}:{kind}, and the
	// systemtests Temporal dev server PERSISTS history across runs — a fixed name would
	// collide with a stale workflow from an earlier run still retrying against a closed
	// fake. A random suffix isolates each run.
	projectID, err := tr.CreateProject(ctx, "uc1-agentic-"+harness.ShortID())
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}
	if got := projRepo.CommitCount(ctx); got < 1 {
		t.Fatalf("project birth did not commit to the project repo: count=%d", got)
	}

	dispatchesBefore := fake.DispatchCount()

	// Request a system-design artifact draft. In the agentic path this DISPATCHES a
	// claude-code-action job (workflow_dispatch) — NOT a synchronous worker draft.
	if _, err := tr.RequestArtifactDraft(ctx, projectID, kind); err != nil {
		t.Fatalf("requestArtifactDraft: %v", err)
	}

	// HARD: the session became observable through the published read route.
	_ = harness.WaitForStartedSession(ctx, t, tr, projectID, kind, 90*time.Second)

	// HARD: the AGENTIC path was taken — the design Manager dispatched a job. The old
	// synchronous worker path issues ZERO workflow_dispatch calls; the agentic path
	// issues at least one. Poll (the dispatch fires just after the session first
	// becomes observable at "drafting"). This is the load-bearing "not the old worker
	// path" assertion.
	if !waitDispatch(fake, dispatchesBefore, 30*time.Second) {
		t.Fatalf("expected an agentic workflow_dispatch (got %d, before %d) — the server took the OLD synchronous worker path, not the agentic dispatch path", fake.DispatchCount(), dispatchesBefore)
	}

	// HARD (the per-project-design-dispatch proof — the live-activation gap fix): the
	// DESIGN dispatch must hit the PER-PROJECT repo (owner=account, repo=projectID) +
	// aiarch-design.yml — NOT the central construction repo + aiarch-construct.yml. Before
	// the fix the dispatch targeted the fixed construction repo, and this assertion (the
	// one the fake was previously missing) is exactly what catches that regression.
	fake.AssertDispatchedToPerProjectRepo(t, projectID)

	// HARD: the dispatched job committed the draft to the session branch, the server
	// observed + read it back, and the session reached the human gate (AwaitingReview)
	// carrying the read-back draft. Deterministic — the fake commits a FIXED draft.
	if !harness.TryReachStage(ctx, tr, projectID, kind, "awaitingReview", 2*time.Minute) {
		st, _, _ := tr.GetSessionState(ctx, projectID, kind)
		t.Fatalf("agentic draft never reached the human gate (awaitingReview); stuck at %q — dispatch→observe→read-back round-trip did not complete (fake fault: %q)", st.Stage, fake.LastFault())
	}

	beforeCommit := projRepo.CommitCount(ctx)

	// Approve at the gate. In the agentic path this runs the merge GUARD (CI green) +
	// the architecture +1 relay + the App-mediated merge (all fake REST), then commits
	// on MAIN. The fake merges the session branch into main of the file:// repo, so the
	// post-merge ReadProject (main) reflects the committed artifact.
	if err := tr.SubmitReview(ctx, projectID, kind, "approve", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !harness.TryReachStage(ctx, tr, projectID, kind, "committed", 60*time.Second) {
		t.Fatalf("approved at the gate but the artifact never reached committed (merge→commit-on-main leg failed; fake fault: %q)", fake.LastFault())
	}

	// HARD: the PR was merged (the App-mediated merge), and a new commit landed on
	// main with the artifact JSON in the committed tree under .aiarch/state.
	if fake.MergeCount() < 1 {
		t.Fatalf("approve did not merge the design PR (MergeCount=%d) — the App-mediated merge did not run", fake.MergeCount())
	}
	if got := projRepo.CommitCount(ctx); got <= beforeCommit {
		t.Fatalf("approve→merge→commit produced no new commit on main: count %d -> %d", beforeCommit, got)
	}
	files := projRepo.ListFiles(ctx)
	if !hasStateFile(files) {
		t.Fatalf("committed tree on main has no .aiarch/state artifact file after commit; tree=%v", files)
	}
	t.Logf("UC1 agentic E2E proof: dispatches=%d, merged=%d, committed tip %q with %d state file(s) on main",
		fake.DispatchCount(), fake.MergeCount(), projRepo.LastCommitMessage(ctx), countStateFiles(files))
}

// Test_UC1_Agentic_PhaseFailed_EntersStageDraftFailed drives the anti-wedge path:
// a DISPATCHED job that terminates as a FAILED run (PhaseFailed) must land the
// session in StageDraftFailed (the route shows the failed stage, NOT a perpetual
// GENERATING/drafting) — never a workflow crash, never a silent empty draft.
func Test_UC1_Agentic_PhaseFailed_EntersStageDraftFailed(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	const account = "aiarch-test-org"
	const kind = "volatilities"

	projRepo := harness.StartLocalGitRepo(t, "main")
	fake := harness.StartAgenticGitHub(t, projRepo, account)
	appKey := harness.GenerateAppKeyPEM(t)

	srv := startServerWithEnv(t, true, fake.Env(projRepo, appKey))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	projectID, err := tr.CreateProject(ctx, "uc1-agentic-fail-"+harness.ShortID())
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}

	// Script EVERY dispatched job to terminate as a FAILED run (no draft committed).
	// (requestArtifactDraft's SignalWithStart buffers a redraft signal that auto-redrafts
	// once after the first failure, so a one-shot fail would let the auto-redraft succeed
	// and escape the gate — fail every attempt to pin the session at draftFailed.)
	fake.FailDispatches(true)

	if _, err := tr.RequestArtifactDraft(ctx, projectID, kind); err != nil {
		t.Fatalf("requestArtifactDraft: %v", err)
	}
	_ = harness.WaitForStartedSession(ctx, t, tr, projectID, kind, 90*time.Second)

	// HARD: a job was still dispatched (the agentic path ran) ...
	if !waitDispatch(fake, 0, 30*time.Second) {
		t.Fatalf("no workflow_dispatch on the failed-draft path — the agentic dispatch did not run")
	}
	// ... and the session landed in the human-visible StageDraftFailed (the anti-wedge
	// rule), NOT a perpetual drafting/generating stage.
	if !harness.TryReachStage(ctx, tr, projectID, kind, "draftFailed", 90*time.Second) {
		st, _, _ := tr.GetSessionState(ctx, projectID, kind)
		t.Fatalf("PhaseFailed job did not route to draftFailed; session stuck at stage %q (anti-wedge rule violated)", st.Stage)
	}
	t.Logf("UC1 agentic anti-wedge proof: failed dispatch routed to draftFailed (dispatches=%d)", fake.DispatchCount())
}

// waitDispatch polls until the fake has seen more than `before` workflow_dispatch
// calls (the agentic-path proof) or the timeout elapses. The dispatch fires just
// after the session first becomes observable, so a short poll avoids racing it.
func waitDispatch(fake *harness.AgenticGitHub, before int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fake.DispatchCount() > before {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
