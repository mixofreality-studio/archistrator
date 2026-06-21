package usecases

import (
	"context"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// I-UC2-AG — UC2 (project design / Phase-2) re-proven END-TO-END over the AGENTIC
// dispatch path, at the systemtests black-box level — the Phase-2 twin of
// uc1_agentic_test.go. The per-artifact Phase-2 co-authoring draft path DISPATCHES
// a claude-code-action DESIGN job (workflow_dispatch) over the FROZEN
// constructionPipelineAccess (D-MPD-Δ); the fake job commits the typed Phase-2
// draft to the session branch; the server observes + reads it back, reaches the
// human gate, the human approves through the route, the server merges the PR
// (fake), and the read-back from MAIN reflects the committed artifact.
//
// The SDP-assemble path's estimation Engines still run IN-PROCESS (no dispatch) —
// that leg is covered by the existing Test_UC2_SDPGate_Wiring; this file proves the
// AGENTIC per-artifact draft leg, which the offline-cassette UC2 wiring test could
// NOT (no Phase-2 cassettes). Here the fake commits a FIXED Phase-2 draft, so the
// dispatch→read-back→gate→approve→merge→commit legs are HARD, not best-effort.

func Test_UC2_Agentic_E2E_DispatchObserveGateMergeCommit(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	const account = "aiarch-test-org"
	// planningAssumptions is the FIRST draftable Phase-2 artifact (architect-owned,
	// no PM critique). Driving it proves the project-design dispatch path without a
	// sealed Phase 1.
	const kind = "planningAssumptions"

	projRepo := harness.StartLocalGitRepo(t, "main")
	fake := harness.StartAgenticGitHub(t, projRepo, account)
	appKey := harness.GenerateAppKeyPEM(t)

	srv := startServerWithEnv(t, true /* devAuth */, fake.Env(projRepo, appKey))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	// Unique per-run name — the systemtests Temporal dev server persists history, so a
	// fixed WorkflowID ({projectID}:{kind}) would collide with a stale run.
	projectID, err := tr.CreateProject(ctx, "uc2-agentic-"+harness.ShortID())
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}
	if got := projRepo.CommitCount(ctx); got < 1 {
		t.Fatalf("project birth did not commit to the project repo: count=%d", got)
	}

	dispatchesBefore := fake.DispatchCount()

	// Request a Phase-2 artifact draft. In the agentic path this DISPATCHES a job —
	// NOT a synchronous worker draft (which the old UC2 cassette path used and which
	// could never converge a Phase-2 artifact offline).
	sessionRef, err := tr.RequestProjectArtifactDraft(ctx, projectID, kind)
	if err != nil {
		t.Fatalf("requestProjectArtifactDraft: %v", err)
	}
	if sessionRef == "" {
		t.Fatalf("requestProjectArtifactDraft: empty sessionRef")
	}

	// HARD: the started Phase-2 session is observable through the published read route.
	st := harness.WaitForStartedProjectSession(ctx, t, tr, projectID, kind, 90*time.Second)
	if st.ArtifactKind != kind {
		t.Fatalf("session artifactKind = %q, want %q", st.ArtifactKind, kind)
	}

	// HARD: the AGENTIC path was taken — the projectDesignManager dispatched a job.
	if !waitDispatch(fake, dispatchesBefore, 30*time.Second) {
		t.Fatalf("expected an agentic workflow_dispatch for the Phase-2 draft (got %d, before %d) — the server took the OLD synchronous worker path", fake.DispatchCount(), dispatchesBefore)
	}

	// HARD (the per-project-design-dispatch proof — the live-activation gap fix): the
	// Phase-2 DESIGN dispatch must hit the PER-PROJECT repo (owner=account, repo=projectID)
	// + aiarch-design.yml — NOT the central construction repo + aiarch-construct.yml.
	fake.AssertDispatchedToPerProjectRepo(t, projectID)

	// HARD: the dispatched job committed the Phase-2 draft to the session branch, the
	// server observed + read it back, and the session reached the human gate. The fake
	// commits a FIXED Phase-2 draft, so this is deterministic (unlike the offline-
	// cassette UC2 wiring test, which had no Phase-2 cassettes).
	if !harness.TryReachProjectStage(ctx, tr, projectID, kind, "awaitingReview", 2*time.Minute) {
		st, _, _ := tr.GetProjectSessionState(ctx, projectID, kind)
		t.Fatalf("agentic Phase-2 draft never reached the human gate; stuck at stage %q (dispatch→observe→read-back round-trip did not complete; fake fault: %q)", st.Stage, fake.LastFault())
	}

	beforeCommit := projRepo.CommitCount(ctx)

	// Approve at the gate → merge GUARD (CI green) + the +1 relay + the App-mediated
	// merge of the session branch into main → commit on main.
	if err := tr.SubmitProjectReview(ctx, projectID, kind, "approve", ""); err != nil {
		t.Fatalf("project-design approve: %v", err)
	}
	if !harness.TryReachProjectStage(ctx, tr, projectID, kind, "committed", 60*time.Second) {
		t.Fatalf("approved at the gate but the Phase-2 artifact never reached committed (merge→commit-on-main leg failed)")
	}

	// HARD: the PR was merged and a new commit landed on main carrying the Phase-2
	// artifact JSON in .aiarch/state.
	if fake.MergeCount() < 1 {
		t.Fatalf("approve did not merge the Phase-2 design PR (MergeCount=%d)", fake.MergeCount())
	}
	if got := projRepo.CommitCount(ctx); got <= beforeCommit {
		t.Fatalf("approve→merge→commit produced no new commit on main: count %d -> %d", beforeCommit, got)
	}
	files := projRepo.ListFiles(ctx)
	if !hasStateFile(files) {
		t.Fatalf("committed tree on main has no .aiarch/state artifact file after commit; tree=%v", files)
	}
	t.Logf("UC2 agentic E2E proof: dispatches=%d, merged=%d, committed tip %q with %d state file(s) on main",
		fake.DispatchCount(), fake.MergeCount(), projRepo.LastCommitMessage(ctx), countStateFiles(files))
}
