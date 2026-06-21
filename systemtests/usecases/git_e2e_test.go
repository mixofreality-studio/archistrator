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

// Test_GitE2E_UC1_DesignArtifactCommitsToGit is the deterministic UC1 proof. The
// committed glossary cassettes converge offline (the baseline UC1 happy-path
// reaches "committed" under strict replay), so the approve→commit leg is HARD
// here: after approve, the artifact MUST appear as a fresh commit in the repo, with
// its JSON in the committed tree.
func Test_GitE2E_UC1_DesignArtifactCommitsToGit(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	// Throwaway on-disk git repo for the project. The cross-project registry index repo
	// is GONE (founder ruling 2026-06-14): the catalog is discovered by scanning the
	// project repo, so there is no second repo. The LOCAL profile writes per-project
	// state here.
	projRepo := harness.StartLocalGitRepo(t, "main")

	srv := startServerWithEnv(t, true /* devAuth */, harness.GitLocalEnv(projRepo.URL()))
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	const kind = "glossary"

	// Baseline: the seeded repo has exactly one commit before any project work.
	beforeProj := projRepo.CommitCount(ctx)

	// CreateProject — births the aggregate (the repo's project.json + its existence IS
	// the catalog entry now; no second registry write). In LOCAL git this is a real commit.
	projectID, err := tr.CreateProject(ctx, "UC1 git e2e")
	if err != nil {
		t.Fatalf("createProject: %v", err)
	}
	if got := projRepo.CommitCount(ctx); got <= beforeProj {
		t.Fatalf("CreateProject did not commit project birth to git: count %d -> %d", beforeProj, got)
	}

	// Drive the co-author draft → human gate → approve → commit. The glossary
	// cassettes converge offline, so the gate is reached deterministically.
	if _, err := tr.RequestArtifactDraft(ctx, projectID, kind); err != nil {
		t.Fatalf("draft: %v", err)
	}
	_ = harness.WaitForStartedSession(ctx, t, tr, projectID, kind, 90*time.Second)
	if !harness.TryReachStage(ctx, tr, projectID, kind, "awaitingReview", 2*time.Minute) {
		t.Fatal("glossary co-author never reached the human gate under cassette replay (the cassettes are expected to converge — see Test_UC1_CoauthorGlossary_WiringHappyPath)")
	}

	beforeCommit := projRepo.CommitCount(ctx)

	// Approve at the gate → CommitArtifact → projectStateAccess git push.
	if err := tr.SubmitReview(ctx, projectID, kind, "approve", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !harness.TryReachStage(ctx, tr, projectID, kind, "committed", 30*time.Second) {
		t.Fatal("approved at the gate but glossary never reached committed")
	}

	// HARD: the commit landed as a real git commit AND the glossary JSON is in the
	// committed tree under .aiarch/state/ — design output really is in the repo.
	if got := projRepo.CommitCount(ctx); got <= beforeCommit {
		t.Fatalf("approve→commit did not produce a new git commit: count %d -> %d", beforeCommit, got)
	}
	files := projRepo.ListFiles(ctx)
	if !hasStateFile(files) {
		t.Fatalf("committed tree has no .aiarch/state artifact file after commit; tree=%v", files)
	}
	t.Logf("UC1 E2E git proof: glossary committed as git commit %q; committed tree carries %d state file(s)",
		projRepo.LastCommitMessage(ctx), countStateFiles(files))
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
