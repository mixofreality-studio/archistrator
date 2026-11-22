package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// workspaceMergeFixture is a born project repo declaring one workspace repo "plat" (a sibling
// checkout), both accepting pushes to their checked-out main as `archistrator init` repos do.
func workspaceMergeFixture(t *testing.T) (repo, plat string, access agenticjob.AgenticJobAccess) {
	t.Helper()
	parent := t.TempDir()
	plat = filepath.Join(parent, "plat")
	if err := os.Mkdir(plat, 0o750); err != nil {
		t.Fatal(err)
	}
	inRepo(t, plat, "git", "init", "-q", "-b", "main")
	inRepo(t, plat, "git", "config", "receive.denyCurrentBranch", "updateInstead")
	writeRepoFile(t, plat, "README.md", []byte("plat\n"))
	inRepo(t, plat, "git", "add", "-A")
	inRepo(t, plat, "git", "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "seed")

	var top map[string]json.RawMessage
	if err := json.Unmarshal(freshState(t), &top); err != nil {
		t.Fatal(err)
	}
	// The project repo lives beside plat, so the relative path is ../plat.
	repo = filepath.Join(parent, "app")
	if err := os.Mkdir(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	top["workspaceRepos"] = json.RawMessage(`[{"id":"plat","relPath":"../plat"}]`)
	state, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	born := bornRepo(t, state)
	if err := os.Remove(repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(born, repo); err != nil {
		t.Fatal(err)
	}
	inRepo(t, repo, "git", "config", "receive.denyCurrentBranch", "updateInstead")
	access, err = agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return repo, plat, access
}

// onBranch commits files on a new branch off main in dir and returns to main.
func onBranch(t *testing.T, dir, branch string, files map[string]string) {
	t.Helper()
	inRepo(t, dir, "git", "checkout", "-q", "-b", branch)
	for p, body := range files {
		writeRepoFile(t, dir, p, []byte(body))
		inRepo(t, dir, "git", "add", "--", p)
	}
	inRepo(t, dir, "git", "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "work")
	inRepo(t, dir, "git", "checkout", "-q", "main")
}

func mergeJob(t *testing.T, access agenticjob.AgenticJobAccess, activity string) agenticjob.PipelineObservation {
	t.Helper()
	rc := fwra.Context{Context: context.Background(), IdempotencyKey: fwra.IdempotencyKey("ws-merge-" + t.Name() + activity)}
	handle, err := access.SubmitAgenticJob(rc, agenticjob.PipelineSpec{
		ProjectID: "todomvc", ActivityID: agenticjob.ConstructionActivityID(activity), Steps: []agenticjob.PipelineStep{{Name: "merge"}},
		DispatchInputs: map[string]string{agenticjob.DispatchInputJobKey: agenticjob.DispatchJobMerge},
	})
	if err != nil {
		t.Fatalf("submit merge: %v", err)
	}
	return observeToTerminal(t, access, rc, handle)
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(out))
}

// TestLocalMergeLandsTheWorkspaceRepoBranchWithTheProjects: an activity branch present in a
// declared workspace repo is merged into that repo's main and deleted, with the project's own.
func TestLocalMergeLandsTheWorkspaceRepoBranchWithTheProjects(t *testing.T) {
	repo, plat, access := workspaceMergeFixture(t)
	onBranch(t, repo, "activity/C-x", map[string]string{"own.txt": "own\n"})
	onBranch(t, plat, "activity/C-x", map[string]string{"framework-go/methodcheck/rule.go": "package methodcheck\n"})

	obs := mergeJob(t, access, "C-x")
	if obs.Phase != agenticjob.PhaseSucceeded {
		t.Fatalf("merge did not succeed: %+v", obs)
	}
	for dir, file := range map[string]string{repo: "own.txt", plat: "framework-go/methodcheck/rule.go"} {
		if got := gitOut(t, dir, "show", "main:"+file); got == "" || strings.Contains(got, "fatal") {
			t.Errorf("%s: main lacks %s: %q", dir, file, got)
		}
		if got := gitOut(t, dir, "branch", "--list", "activity/C-x"); got != "" {
			t.Errorf("%s: the activity branch was not deleted: %q", dir, got)
		}
	}
}

// TestLocalMergeConflictInAWorkspaceRepoLandsNothing: a conflict in the workspace repo fails the
// merge before any repo's main moves, and the project's own branch stays for the retry.
func TestLocalMergeConflictInAWorkspaceRepoLandsNothing(t *testing.T) {
	repo, plat, access := workspaceMergeFixture(t)
	onBranch(t, repo, "activity/C-y", map[string]string{"own.txt": "own\n"})
	onBranch(t, plat, "activity/C-y", map[string]string{"README.md": "branch edit\n"})
	// main moves under the same file in the workspace repo, so the branch conflicts with it.
	writeRepoFile(t, plat, "README.md", []byte("main edit\n"))
	inRepo(t, plat, "git", "add", "-A")
	inRepo(t, plat, "git", "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "main edit")
	ownBefore := gitOut(t, repo, "rev-parse", "main")

	obs := mergeJob(t, access, "C-y")
	if obs.Phase != agenticjob.PhaseFailed {
		t.Fatalf("merge should fail on the workspace conflict: %+v", obs)
	}
	if got := gitOut(t, repo, "rev-parse", "main"); got != ownBefore {
		t.Errorf("the project's main moved (%s -> %s) though a workspace repo conflicted", ownBefore, got)
	}
	if got := gitOut(t, repo, "branch", "--list", "activity/C-y"); got == "" {
		t.Error("the project's activity branch was deleted though the merge failed")
	}
}
