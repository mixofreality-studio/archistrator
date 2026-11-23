package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// platRepos declares the workspace repo "plat", a sibling checkout of the project's repo.
const platRepos = `[{"id":"plat","relPath":"../plat"}]`

// workspaceState is the fixture project's state declaring repos (a JSON array) and, when owned is
// not empty, the ownedPaths (a JSON array) of the todo-list-access contract.
func workspaceState(t *testing.T, repos, owned string) []byte {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(freshState(t), &top); err != nil {
		t.Fatal(err)
	}
	top["workspaceRepos"] = json.RawMessage(repos)
	if owned != "" {
		var contracts map[string]map[string]json.RawMessage
		if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
			t.Fatal(err)
		}
		contracts[freshContract]["ownedPaths"] = json.RawMessage(owned)
		raw, err := json.Marshal(contracts)
		if err != nil {
			t.Fatal(err)
		}
		top["serviceContracts"] = raw
	}
	state, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// siblingRepo is a git repo named name beside the project's repo, accepting pushes to its
// checked-out main as `archistrator init` repos do, seeded with files.
func siblingRepo(t *testing.T, parent, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	inRepo(t, dir, "git", "init", "-q", "-b", "main")
	inRepo(t, dir, "git", "config", "receive.denyCurrentBranch", "updateInstead")
	for p, body := range files {
		writeRepoFile(t, dir, p, []byte(body))
	}
	inRepo(t, dir, "git", "add", "-A")
	inRepo(t, dir, "git", "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "seed")
	return dir
}

// workspaceMergeFixture is a born project repo declaring one workspace repo "plat" (a sibling
// checkout), both accepting pushes to their checked-out main as `archistrator init` repos do.
func workspaceMergeFixture(t *testing.T) (repo, plat string, access agenticjob.AgenticJobAccess) {
	t.Helper()
	repo, plat, _, access = workspaceFixtureOf(t, workspaceState(t, platRepos, ""))
	return repo, plat, access
}

// workspaceFixtureOf is a project repo born from state, beside a sibling "plat" repo, with the
// local executor over it. The project's repo is named app, so "../plat" is plat; repo and
// plat are siblings of each other and of parent, the directory returned.
func workspaceFixtureOf(t *testing.T, state []byte) (repo, plat, parent string, access agenticjob.AgenticJobAccess) {
	t.Helper()
	parent = t.TempDir()
	plat = siblingRepo(t, parent, "plat", map[string]string{
		"README.md": "plat\n", "go.mod": "module example.com/plat\n\ngo 1.21\n", "lib/lib.go": "package lib\n",
	})
	born := bornRepo(t, state)
	repo = filepath.Join(parent, "app")
	if err := os.Rename(born, repo); err != nil {
		t.Fatal(err)
	}
	inRepo(t, repo, "git", "config", "receive.denyCurrentBranch", "updateInstead")
	access, err := agenticjob.NewLocalExecAgenticJobAccess(repo, "todomvc", stateBinary(t), projectstate.StateToolVersion(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return repo, plat, parent, access
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
	return jobOf(t, access, activity, agenticjob.DispatchJobMerge)
}

var jobSeq atomic.Int64

// jobOf runs the synchronous git job (merge or refresh) of an activity to its terminal observation.
// Every call is its own submission: a retry is a new idempotency key.
func jobOf(t *testing.T, access agenticjob.AgenticJobAccess, activity, job string) agenticjob.PipelineObservation {
	t.Helper()
	key := fmt.Sprintf("ws-%s-%s-%s-%d", job, t.Name(), activity, jobSeq.Add(1))
	rc := fwra.Context{Context: context.Background(), IdempotencyKey: fwra.IdempotencyKey(key)}
	handle, err := access.SubmitAgenticJob(rc, agenticjob.PipelineSpec{
		ProjectID: "todomvc", ActivityID: agenticjob.ConstructionActivityID(activity), Steps: []agenticjob.PipelineStep{{Name: job}},
		DispatchInputs: map[string]string{agenticjob.DispatchInputJobKey: job},
	})
	if err != nil {
		t.Fatalf("submit %s: %v", job, err)
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
