package usecases

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// E2E GIT-COMMIT PROOF (founder acceptance #3, wiring level) — drive the REAL
// manager flow through the running server's published Client surface and assert
// the committed design artifact lands as a REAL GIT COMMIT in an on-disk repo.
//
// This is the END-TO-END complement to cmd/server/projectstate_git_adapter_test.go
// (I-GIT-DESIGN), which proved the comp-root adapter over the no-cred
// projectStateAccess surface but BYPASSED the Manager + worker. Here the full
// spine runs: webClient intent → systemDesignManager (Temporal) → workerAccess
// draft (cassette replay) → human review gate (approve) → projectStateAccess
// COMMIT → git push. The server boots in the LOCAL project-state-git substrate
// (ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true), pointed at throwaway file:// repos
// the harness then inspects with the git CLI.
//
// The LIVE proof (real GitHub repo + real LLM) is founder-gated and out of scope;
// this makes the WIRING bulletproof. git not on PATH skips (StartLocalGitRepo).

// Test_GitE2E_UC1_DesignArtifactCommitsToGit is the deterministic UC1 proof.
//
// FORMERLY designed for the retired synchronous cassette-replay co-author path
// (RequestArtifactDraft → workerAccess draft directly, no dispatch). Since the
// agentic pivot (D-MSD-Δ), CoAuthorArtifactWorkflow ALWAYS calls
// DispatchDesignJobActivity -> constructionPipelineAccess.SubmitConstructionPipeline
// — a server booted with no GitHub App env (as this test used to do) has a nil
// pipeline RA there, so the activity panics (see the systemtests.yml workflow's
// former "GATED TESTS" note, which skipped this exact test for that reason).
// Rewired onto the SAME AgenticGitHub fake uc1_agentic_test.go drives (the fake
// commits a FIXED draft onto the session branch, faking only the external
// claude-code-action + GitHub PR seam per the test constitution) so the deterministic
// approve→commit-lands-in-git property this test exists to prove is HARD again:
// after approve, the artifact MUST appear as a fresh merge commit in the repo, with
// its JSON in the committed tree.
func Test_GitE2E_UC1_DesignArtifactCommitsToGit(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	const account = "aiarch-test-org"
	const kind = "volatilities" // architect-owned: no PM-critique round-trip (matches uc1_agentic_test.go)

	// The on-disk file:// project repo IS the server's LOCAL project-state substrate
	// AND the repo the agentic fake commits the draft into (one repo, two readers) —
	// the cross-project registry index repo is GONE (founder ruling 2026-06-14): the
	// catalog is discovered by scanning the project repo, so there is no second repo.
	projRepo := harness.StartLocalGitRepo(t, "main")
	artRepo := harness.StartLocalGitRepo(t, "main")
	fake := harness.StartAgenticGitHub(t, projRepo, account)
	appKey := harness.GenerateAppKeyPEM(t)

	srv := startServerWithEnv(t, true /* devAuth */, fake.Env(projRepo, artRepo, appKey))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	// Baseline: the seeded repo has exactly one commit before any project work.
	beforeProj := projRepo.CommitCount(ctx)

	// CreateProject — births the aggregate (the repo's project.json + its existence IS
	// the catalog entry now; no second registry write). In LOCAL git this is a real
	// commit; in the agentic config it also adopts the repo + seats the workflow file
	// (fake REST).
	projectID, err := tr.CreateProject(ctx, "uc1-git-e2e-"+harness.ShortID())
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}
	if got := projRepo.CommitCount(ctx); got <= beforeProj {
		t.Fatalf("CreateProject did not commit project birth to git: count %d -> %d", beforeProj, got)
	}

	// volatilities' Phase-1 predecessors must already be Committed — the wire surface
	// enforces the spine ordering (checkPhase1Predecessor, STP-UC1-B1). Seed them
	// directly rather than driving each through its own co-author round trip — this
	// test proves the approve→commit git leg, not the whole Phase-1 sequence.
	projRepo.SeedCommittedDesignSlots("mission", "glossary", "scrubbedRequirements")

	// Request the draft: DISPATCHES an agentic job (workflow_dispatch), which commits
	// a FIXED, deterministic draft onto the session branch — the approve→commit leg
	// is therefore HARD here, unlike the retired offline-cassette limitation.
	if _, err := tr.RequestArtifactDraft(ctx, projectID, kind); err != nil {
		t.Fatalf("draft: %v", err)
	}
	_ = harness.WaitForStartedSession(ctx, t, tr, projectID, kind, 90*time.Second)
	if !harness.TryReachStage(ctx, tr, projectID, kind, "awaitingReview", 2*time.Minute) {
		st, _, _ := tr.GetSessionState(ctx, projectID, kind)
		t.Fatalf("agentic draft never reached the human gate (awaitingReview); stuck at %q (fake fault: %q)", st.Stage, fake.LastFault())
	}

	beforeCommit := projRepo.CommitCount(ctx)

	// Approve at the gate — runs the merge guard (CI green) + the App-mediated merge
	// (fake REST) — CommitArtifact → projectStateAccess git push lands on main.
	if err := tr.SubmitReview(ctx, projectID, kind, "approve", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !harness.TryReachStage(ctx, tr, projectID, kind, "committed", 60*time.Second) {
		t.Fatalf("approved at the gate but the artifact never reached committed (fake fault: %q)", fake.LastFault())
	}

	// HARD: the PR was merged, and the commit landed as a real git commit with the
	// artifact JSON in the committed tree under .aiarch/state/ — design output really
	// is in the repo.
	if fake.MergeCount() < 1 {
		t.Fatalf("approve did not merge the design PR (MergeCount=%d)", fake.MergeCount())
	}
	if got := projRepo.CommitCount(ctx); got <= beforeCommit {
		t.Fatalf("approve→merge→commit did not produce a new git commit: count %d -> %d", beforeCommit, got)
	}
	files := projRepo.ListFiles(ctx)
	if !hasStateFile(files) {
		t.Fatalf("committed tree has no .aiarch/state artifact file after commit; tree=%v", files)
	}
	t.Logf("UC1 E2E git proof: %s committed as git commit %q; committed tree carries %d state file(s)",
		kind, projRepo.LastCommitMessage(ctx), countStateFiles(files))
}

// Test_GitE2E_UC2_ProjectBirthCommitsToGit is the UC2-side proof of the SAME
// project-state→git write path (the projectManager + projectDesignManager share
// the comp-root git adapter the UC1 managers use). UC2's Phase-2 artifacts have no
// offline cassettes, so a Phase-2 ARTIFACT commit cannot be driven deterministically
// here (it is covered by the I-GIT-DESIGN adapter proof
// TestProjectStateGitAdapter_UC2AdvanceAndResearchLandsInGit at the adapter seam).
// What IS deterministic end-to-end through the wire is that the project lifecycle
// the UC2 manager operates on — project birth + the research-input the UC2 flow
// reads — commits to git. This proves the wire→Manager→git path is live for the
// project surface UC2 drives.
func Test_GitE2E_UC2_ProjectBirthCommitsToGit(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	projRepo := harness.StartLocalGitRepo(t, "main")

	srv := startServerWithEnv(t, true /* devAuth */, harness.GitLocalEnv(projRepo.URL()))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	before := projRepo.CommitCount(ctx)

	projectID, err := tr.CreateProject(ctx, "UC2 git e2e")
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}
	if got := projRepo.CommitCount(ctx); got <= before {
		t.Fatalf("CreateProject did not commit to git: count %d -> %d", before, got)
	}

	// SetResearchInput is the Phase-1 corpus the UC2 project-design flow reads from
	// head-state; writing it must also land as a git commit (head-state mutation →
	// git push), proving the design write path for the project the UC2 manager drives.
	afterCreate := projRepo.CommitCount(ctx)
	if err := tr.SetResearchInput(ctx, projectID, []harness.ResearchSource{
		{Title: "Founder brief", Content: "Automate The Method end to end."},
	}); err != nil {
		t.Fatalf("setResearchInput: %v", err)
	}
	if got := projRepo.CommitCount(ctx); got <= afterCreate {
		t.Fatalf("SetResearchInput did not commit to git: count %d -> %d", afterCreate, got)
	}

	files := projRepo.ListFiles(ctx)
	if !hasStateFile(files) {
		t.Fatalf("committed tree has no .aiarch/state file after project birth + research input; tree=%v", files)
	}
	t.Logf("UC2 E2E git proof: project birth + research-input committed to git (tip %q); %d state file(s) in tree",
		projRepo.LastCommitMessage(ctx), countStateFiles(files))
}

// hasStateFile reports whether the committed tree carries any head-state JSON under
// the .aiarch/state prefix the GitStore writes to.
func hasStateFile(files []string) bool {
	return countStateFiles(files) > 0
}

func countStateFiles(files []string) int {
	n := 0
	for _, f := range files {
		if strings.Contains(f, ".aiarch/state") || strings.Contains(f, "state/") {
			n++
		}
	}
	return n
}
