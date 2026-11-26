package main

// jobgenerated_test.go pins that the local job OWNS its generated output (ISS-031-2): after the
// generation succeeds it commits whatever generation changed onto the activity branch, authored
// aiarch, before the agent runs, and the gates' base is the tip after that commit. The agent never
// commits generated output, so a run that commits nothing is still a run that produced no commits,
// and the reset the check gate does keeps the job's commit. The harness is workspacedispatch_test.go's.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/agenticjob"
)

const (
	// jobGenFile is the file the job's generation (the make stub's gen-models) writes inside the
	// component's own package; jobHandWritten is the component's one hand-written file.
	jobGenFile     = freshPackage + "/job.gen.go"
	jobHandWritten = freshPackage + "/todolistaccess.go"
)

// jobGeneratesFile makes the fixture's make stub write jobGenFile (a package-level function G) when
// the job runs gen-models, instead of skipping it; every other target still runs the real make. It
// replaces the stub installAgent put on PATH, so what the other tests see is unchanged.
func jobGeneratesFile(t *testing.T, ag agent) {
	t.Helper()
	realMake := "make"
	if p, err := exec.LookPath("make"); err == nil {
		realMake = p
	}
	stub := "#!/bin/sh\nif [ \"$1\" = gen-models ]; then\n" +
		"  mkdir -p " + freshPackage + "; printf 'package todolist\\n\\n// G is what the job generated.\\nfunc G() {}\\n' > " + jobGenFile + "\n  exit 0\nfi\n" +
		"exec " + realMake + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(ag.bodyPath), "make"), []byte(stub), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
}

// jobFixture is the construction fixture whose job generates jobGenFile, with main's tip as it was
// before any dispatch.
func jobFixture(t *testing.T) (repo, mainTip string, access agenticjob.AgenticJobAccess, spec review.TaskSpec, ag agent) {
	t.Helper()
	repo, _, _, access, spec, ag = workspaceConstruction(t)
	jobGeneratesFile(t, ag)
	return repo, gitOut(t, repo, "rev-parse", "main"), access, spec, ag
}

// jobCommits is the commits main..branch, oldest first, each as "<sha> <author>".
func jobCommits(t *testing.T, repo, mainTip string) []string {
	t.Helper()
	out := gitOut(t, repo, "log", "--reverse", "--format=%H %an", mainTip+"..activity/"+freshActivity)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// filesOf is the paths a commit changes.
func filesOf(t *testing.T, repo, sha string) []string {
	t.Helper()
	out := gitOut(t, repo, "show", "--name-only", "--format=", sha)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// jobCommitOf is the sha of the one commit authored aiarch on the branch, failing unless there is
// exactly one.
func jobCommitOf(t *testing.T, repo, mainTip string) string {
	t.Helper()
	var shas []string
	for _, c := range jobCommits(t, repo, mainTip) {
		if sha, author, _ := strings.Cut(c, " "); author == "aiarch" {
			shas = append(shas, sha)
		}
	}
	if len(shas) != 1 {
		t.Fatalf("the branch holds %d commits authored aiarch, want 1: %v", len(shas), jobCommits(t, repo, mainTip))
	}
	return shas[0]
}

// The job's generated file is committed on the activity branch before the agent runs: one commit
// authored aiarch holding exactly that file, whose parent is the tip the branch had before the
// dispatch (main, the branch being born by it). Against the old behaviour (nothing committed by the
// job; the agent had to commit generated output) the branch holds no aiarch commit and this fails.
func TestJobGenerated_TheJobCommitsItsOutputOnTheBranchBeforeTheAgent(t *testing.T) {
	repo, mainTip, access, spec, ag := jobFixture(t)
	ag.does(t, `true`)
	_ = constructionOf(t, access, spec, 1)
	sha := jobCommitOf(t, repo, mainTip)
	if got := filesOf(t, repo, sha); len(got) != 1 || got[0] != jobGenFile {
		t.Errorf("the job's commit holds %v, want exactly [%s]", got, jobGenFile)
	}
	if got := gitOut(t, repo, "rev-parse", sha+"^"); got != mainTip {
		t.Errorf("the job's commit has parent %s, want the tip before the dispatch %s", got, mainTip)
	}
}

// An agent that commits an in-scope edit and leaves the generated file alone passes the edit-scope
// gate (the job's commit is the gate's base, not the agent's diff), and the tree the checks ran
// over still holds the file: the agent's file calls G, which only the job's file defines, so the
// build check passes only if the check gate's reset kept the job's commit. Against the old
// behaviour the file was uncommitted when the agent left it alone, the reset discarded it, and
// the build check fails (undefined: G).
func TestJobGenerated_AnInScopeEditPassesTheScopeGateAndTheChecksSeeTheJobsFile(t *testing.T) {
	repo, _, access, spec, ag := jobFixture(t)
	ag.does(t, `printf 'package todolist\n\nfunc use() { G() }\n' > `+jobHandWritten+`; git add -- `+jobHandWritten+`; commit`)
	obs := constructionOf(t, access, spec, 1)
	if strings.Contains(obs.Diagnostic, "edit scope") || strings.Contains(obs.Diagnostic, "produced no commits") {
		t.Fatalf("the in-scope edit was refused: %s", obs.Diagnostic)
	}
	if got := resultOn(t, repo, "command:build"); got != "pass" {
		t.Errorf("build over the tree the checks ran on: status %q, want pass (the job's file is part of it) (run: %s)", got, obs.Diagnostic)
	}
	if got := gitOut(t, repo, "show", "activity/"+freshActivity+":"+jobGenFile); !strings.Contains(got, "func G()") {
		t.Errorf("the branch does not hold the job's file: %q", got)
	}
}

// An agent that commits nothing is a run that produced no commits, although the job committed: the
// job's commit is the gate's base, so it is not the agent's progress. Against the old behaviour the
// branch holds no job commit, so the "although the job committed" half fails.
func TestJobGenerated_AnAgentThatCommitsNothingProducedNoCommitsAlthoughTheJobCommitted(t *testing.T) {
	repo, mainTip, access, spec, ag := jobFixture(t)
	ag.does(t, `true`)
	obs := constructionOf(t, access, spec, 1)
	if obs.Phase != agenticjob.PhaseFailed || !strings.Contains(obs.Diagnostic, "produced no commits") {
		t.Fatalf("a run whose agent committed nothing did not fail as no commits: %+v", obs)
	}
	_ = jobCommitOf(t, repo, mainTip)
}

// A second dispatch over the same branch regenerates the same bytes and adds no second job commit.
// Against the old behaviour there is no job commit at all, so the count of one fails.
func TestJobGenerated_ASecondDispatchAddsNoSecondJobCommit(t *testing.T) {
	repo, mainTip, access, spec, ag := jobFixture(t)
	ag.does(t, `true`)
	_ = constructionOf(t, access, spec, 1)
	first := jobCommitOf(t, repo, mainTip)
	_ = constructionOf(t, access, spec, 2)
	if second := jobCommitOf(t, repo, mainTip); second != first {
		t.Errorf("the second dispatch changed the job's commit: %s -> %s", first, second)
	}
}
