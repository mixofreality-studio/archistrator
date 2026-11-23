package main

import (
	"os"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
)

// commitOnMain commits files straight onto main of dir (a repo accepting pushes to its checked-out main).
func commitOnMain(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		writeRepoFile(t, dir, p, []byte(body))
		inRepo(t, dir, "git", "add", "--", p)
	}
	inRepo(t, dir, "git", "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "main moves")
}

func refreshJob(t *testing.T, access agenticjob.AgenticJobAccess, activity string) agenticjob.PipelineObservation {
	t.Helper()
	return jobOf(t, access, activity, agenticjob.DispatchJobRefresh)
}

// TestLocalMergeOwnLegRefusingAfterAWorkspaceLegWasCleanLandsNothing: the workspace repo's branch
// merges cleanly and the project's own branch conflicts, so the dry run refuses the whole job and
// the workspace repo's main never moves (a merge that landed the workspace leg first would leave
// the platform half merged with the project's half refused).
func TestLocalMergeOwnLegRefusingAfterAWorkspaceLegWasCleanLandsNothing(t *testing.T) {
	repo, plat, access := workspaceMergeFixture(t)
	onBranch(t, repo, "activity/C-z", map[string]string{"README.md": "branch edit\n"})
	onBranch(t, plat, "activity/C-z", map[string]string{"framework-go/methodcheck/rule.go": "package methodcheck\n"})
	commitOnMain(t, repo, map[string]string{"README.md": "main edit\n"})
	platBefore := gitOut(t, plat, "rev-parse", "main")

	obs := mergeJob(t, access, "C-z")
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("the merge should fail on the project's own conflict: %+v", obs)
	}
	if got := gitOut(t, plat, "rev-parse", "main"); got != platBefore {
		t.Errorf("the workspace repo's main moved (%s -> %s) though the project's own leg conflicted", platBefore, got)
	}
	if got := gitOut(t, plat, "branch", "--list", "activity/C-z"); got == "" {
		t.Error("the workspace repo's activity branch was deleted though nothing landed")
	}
	if !strings.Contains(obs.Diagnostic, "nothing was landed in any repo") {
		t.Errorf("the diagnostic does not say nothing landed: %q", obs.Diagnostic)
	}
}

// TestLocalMergeFailsClosedWhenTheWorkspaceDeclarationCannotBeRead: a declared repo is not a repo
// the merge may skip. An unreadable declaration, or a declared checkout that is gone, stops the
// job before the project's own branch lands (it used to merge its half and report success).
func TestLocalMergeFailsClosedWhenTheWorkspaceDeclarationCannotBeRead(t *testing.T) {
	t.Run("unreadable state document on main", func(t *testing.T) {
		repo, _, access := workspaceMergeFixture(t)
		onBranch(t, repo, "activity/C-a", map[string]string{"own.txt": "own\n"})
		commitOnMain(t, repo, map[string]string{".aiarch/state/project.json": "{ not json"})
		before := gitOut(t, repo, "rev-parse", "main")
		obs := mergeJob(t, access, "C-a")
		if obs.Phase != agenticjob.PhaseFailed || gitOut(t, repo, "rev-parse", "main") != before {
			t.Fatalf("the merge must fail with main untouched: %+v", obs)
		}
	})
	t.Run("declared checkout is gone", func(t *testing.T) {
		repo, plat, access := workspaceMergeFixture(t)
		onBranch(t, repo, "activity/C-b", map[string]string{"own.txt": "own\n"})
		if err := os.RemoveAll(plat); err != nil {
			t.Fatal(err)
		}
		before := gitOut(t, repo, "rev-parse", "main")
		obs := mergeJob(t, access, "C-b")
		if obs.Phase != agenticjob.PhaseFailed || gitOut(t, repo, "rev-parse", "main") != before {
			t.Fatalf("the merge must fail with main untouched: %+v", obs)
		}
	})
	t.Run("a declaration that is not a sibling", func(t *testing.T) {
		for name, repos := range map[string]string{
			"the project's own repo": `[{"id":"plat","relPath":"../app"}]`,
			"unclean sibling":        `[{"id":"plat","relPath":"../plat/."}]`,
			"escapes upward":         `[{"id":"plat","relPath":"../../../../etc"}]`,
			"the dot id":             `[{"id":".","relPath":"../plat"}]`,
			"one repo twice":         `[{"id":"plat","relPath":"../plat"},{"id":"again","relPath":"../plat"}]`,
		} {
			t.Run(name, func(t *testing.T) {
				repo, _, _, access := workspaceFixtureOf(t, workspaceState(t, repos, ""))
				onBranch(t, repo, "activity/C-c", map[string]string{"own.txt": "own\n"})
				before := gitOut(t, repo, "rev-parse", "main")
				obs := mergeJob(t, access, "C-c")
				if obs.Phase != agenticjob.PhaseFailed || gitOut(t, repo, "rev-parse", "main") != before {
					t.Fatalf("the merge must refuse the declaration with main untouched: %+v", obs)
				}
			})
		}
	})
}

// TestLocalMergeReportsAPartialLanding: the dry run cannot know a refusal only the receiving repo
// makes, so when the project's own repo refuses the push after the workspace repo landed, the
// diagnostic says exactly that, names what moved, and the retry (the refusal lifted) lands the rest.
func TestLocalMergeReportsAPartialLanding(t *testing.T) {
	repo, plat, access := workspaceMergeFixture(t)
	onBranch(t, repo, "activity/C-p", map[string]string{"own.txt": "own\n"})
	onBranch(t, plat, "activity/C-p", map[string]string{"framework-go/methodcheck/rule.go": "package methodcheck\n"})
	inRepo(t, repo, "git", "config", "receive.denyCurrentBranch", "refuse")

	obs := mergeJob(t, access, "C-p")
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("the merge should fail when the project's repo refuses the push: %+v", obs)
	}
	if !strings.Contains(obs.Diagnostic, "PARTIAL LANDING") || !strings.Contains(obs.Diagnostic, "workspace repo plat") {
		t.Errorf("the diagnostic does not report the partial landing: %q", obs.Diagnostic)
	}
	if got := gitOut(t, plat, "show", "main:framework-go/methodcheck/rule.go"); !strings.Contains(got, "package methodcheck") {
		t.Errorf("the workspace repo's half did not land as the diagnostic says: %q", got)
	}
	inRepo(t, repo, "git", "config", "receive.denyCurrentBranch", "updateInstead")
	if obs := mergeJob(t, access, "C-p"); obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("the retry should land the project's half: %+v", obs)
	}
	if got := gitOut(t, repo, "show", "main:own.txt"); !strings.Contains(got, "own") {
		t.Errorf("the project's main lacks its half after the retry: %q", got)
	}
}

// TestLocalRefreshBringsMainIntoTheWorkspaceBranchToo: a workspace repo's activity branch behind
// that repo's main is refreshed (else its conflict could never be settled and the activity would
// wedge), and one that conflicts with main fails the refresh naming the repo, pushing nothing.
func TestLocalRefreshBringsMainIntoTheWorkspaceBranchToo(t *testing.T) {
	_, plat, access := workspaceMergeFixture(t)
	onBranch(t, plat, "activity/C-r", map[string]string{"framework-go/methodcheck/rule.go": "package methodcheck\n"})
	commitOnMain(t, plat, map[string]string{"landed.txt": "from main\n"})
	if obs := refreshJob(t, access, "C-r"); obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("the refresh should succeed: %+v", obs)
	}
	if got := gitOut(t, plat, "show", "activity/C-r:landed.txt"); !strings.Contains(got, "from main") {
		t.Fatalf("the workspace branch was not refreshed from its repo's main: %q", got)
	}

	onBranch(t, plat, "activity/C-s", map[string]string{"README.md": "branch edit\n"})
	commitOnMain(t, plat, map[string]string{"README.md": "main edit\n"})
	tip := gitOut(t, plat, "rev-parse", "activity/C-s")
	obs := refreshJob(t, access, "C-s")
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "workspace repo plat") {
		t.Fatalf("a conflicting workspace branch must fail the refresh naming the repo: %+v", obs)
	}
	if got := gitOut(t, plat, "rev-parse", "activity/C-s"); got != tip {
		t.Errorf("a refused refresh moved the branch (%s -> %s)", tip, got)
	}
}
