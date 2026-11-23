package main

// workspacegrants_test.go pins the workspace boundary's two halves the review found open. WHAT A
// TASK MAY OPEN is what main's committed contracts own, not what its activity branch's contract
// claims (the spec is resolved over the branch, whose state document the agent may edit). WHAT
// THE MERGE LANDS is what the gates judged: every declared repo already holding the activity
// branch is gated, under a scope that admits none of it when the spec names none of its
// directories, and the merge itself refuses a leg that holds anything outside main's grants.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
)

// amendBranchState commits state onto the EXISTING activity branch of the fixture project.
func amendBranchState(t *testing.T, repo string, state []byte) {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt")
	inRepo(t, repo, "git", "worktree", "add", "--quiet", wt, "activity/"+freshActivity)
	writeRepoFile(t, wt, ".aiarch/state/project.json", state)
	inRepo(t, wt, "git", "add", "-A")
	inRepo(t, wt, "git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "state revised on the branch")
	inRepo(t, repo, "git", "worktree", "remove", "--force", wt)
}

// An attempt cannot grant the next one write access: main declares plat but no contract owns
// anything in it, and the activity branch's own contract claims @plat/lib. The spec the branch
// resolves names plat, and the dispatch is refused before a worktree, a sandbox path or a branch
// exists in plat, and before the agent runs.
func TestWorkspaceDispatch_AnAgentCannotGrantItselfAWorkspaceDirectory(t *testing.T) {
	ag := installAgent(t)
	repo, plat, _, access := workspaceFixtureOf(t, workspaceState(t, platRepos, ""))
	branchState(t, repo, workspaceState(t, platRepos, wsOwned))
	spec := stateSpecOnBranch(t, repo)
	if !slices.Contains(globsOf(spec), "@plat/lib/**") {
		t.Fatalf("the branch's spec does not name the claimed directory: %v", globsOf(spec))
	}
	ran := filepath.Join(t.TempDir(), "ran")
	ag.does(t, `touch `+ran)
	obs := constructionOf(t, access, spec, 1)
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "grants") {
		t.Fatalf("a directory only the branch's contract owns was opened: %+v", obs)
	}
	if got := gitOut(t, plat, "branch", "--list", "activity/*"); got != "" {
		t.Errorf("plat got an activity branch: %q", got)
	}
	if _, err := os.Lstat(ran); err == nil {
		t.Error("the agent ran although its scope was refused")
	}
}

// The first shape of the merge hole: attempt 1 owned @plat/lib and committed a file outside it; the
// contract was then revised so the next spec names no plat. Plat still holds the activity branch, so
// attempt 2's gates judge it (judge-only) and name the stray; the merge refuses the leg either way.
func TestWorkspaceDispatch_ALeftoverInARepoTheNextSpecNoLongerNamesIsJudgedAndNotMerged(t *testing.T) {
	repo, plat, _, access, spec, ag := workspaceConstruction(t)
	ag.does(t, `mkdir -p "$PLAT/other"; printf 'package other\n' > "$PLAT/other/y.go"; git -C "$PLAT" add other/y.go; (cd "$PLAT" && commit)`)
	first := constructionOf(t, access, spec, 1)
	if first.Phase != agenticjob.PhaseFailed || !strings.Contains(first.Diagnostic, "@plat/other/y.go") {
		t.Fatalf("attempt 1 was not red on the stray: %+v", first)
	}
	amendBranchState(t, repo, workspaceState(t, platRepos, ""))
	second := constructionOf(t, access, stateSpecOnBranch(t, repo), 2)
	if second.Phase != agenticjob.PhaseFailed || !strings.Contains(second.Diagnostic, "@plat/other/y.go") {
		t.Fatalf("attempt 2 did not judge the repo its spec no longer names: %+v", second)
	}
	platMain := gitOut(t, plat, "rev-parse", "main")
	if obs := mergeJob(t, access, freshActivity); obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("the merge landed the stray: %+v", obs)
	}
	if got := gitOut(t, plat, "rev-parse", "main"); got != platMain {
		t.Errorf("plat main moved: %s -> %s", platMain, got)
	}
}

// The second shape: an out-of-scope commit planted on another activity's branch in plat. That
// activity's spec never names plat, yet its gates go red on it, and the merge refuses it.
func TestWorkspaceDispatch_ABranchPlantedInAnUnnamedRepoIsJudgedAndNotMerged(t *testing.T) {
	ag := installAgent(t)
	repo, plat, _, access := workspaceFixtureOf(t, workspaceState(t, platRepos, ""))
	onBranch(t, plat, "activity/"+freshActivity, map[string]string{"evil/x.go": "package evil\n"})
	ag.does(t, `true`)
	obs := constructionOf(t, access, activitySpec(t, repo, freshActivity, "construction"), 1)
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "@plat/evil/x.go") {
		t.Fatalf("the planted branch was not judged: %+v", obs)
	}
	platMain := gitOut(t, plat, "rev-parse", "main")
	if m := mergeJob(t, access, freshActivity); m.Phase != agenticjob.PhaseFailed {
		t.Fatalf("the merge landed the planted commit: %+v", m)
	}
	if got := gitOut(t, plat, "rev-parse", "main"); got != platMain {
		t.Errorf("plat main moved: %s -> %s", platMain, got)
	}
}

// Whatever judged (or never judged) a branch, a workspace leg lands only content inside the
// directories main's contracts own: the merge job alone, with no dispatch before it.
func TestLocalMergeRefusesAWorkspaceLegOutsideMainsGrants(t *testing.T) {
	repo, plat, access := workspaceMergeFixture(t)
	onBranch(t, repo, "activity/C-g", map[string]string{"own.txt": "own\n"})
	onBranch(t, plat, "activity/C-g", map[string]string{"lib/ok.go": "package lib\n", "hooks/pre.sh": "x\n"})
	ownBefore, platBefore := gitOut(t, repo, "rev-parse", "main"), gitOut(t, plat, "rev-parse", "main")
	obs := mergeJob(t, access, "C-g")
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "@plat/hooks/pre.sh") {
		t.Fatalf("the leg outside the grants was not refused by name: %+v", obs)
	}
	if strings.Contains(obs.Diagnostic, "@plat/lib/ok.go") {
		t.Errorf("a granted path was reported as a stray: %s", obs.Diagnostic)
	}
	if gitOut(t, repo, "rev-parse", "main") != ownBefore || gitOut(t, plat, "rev-parse", "main") != platBefore {
		t.Error("a main moved though the merge was refused")
	}
}
