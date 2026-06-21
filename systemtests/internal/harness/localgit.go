package harness

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// LocalGitRepo is a throwaway bare git repo served over file:// — the on-disk
// substrate the server's LOCAL project-state-git profile
// (ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true) writes design artifacts to. The
// harness drives the server to commit into it and then inspects the resulting
// commits with the plain `git` CLI, staying fully black-box (it links no server
// code and no git library — it only speaks file:// + the published wire surface).
type LocalGitRepo struct {
	t      *testing.T
	bare   string // path to the bare repo (the file:// remote)
	branch string
}

// URL is the file:// clone URL the server is pointed at.
func (r LocalGitRepo) URL() string { return "file://" + r.bare }

// StartLocalGitRepo creates a bare git repo seeded with one empty commit so the
// branch exists as a real ref (the server's ref-CAS base), served over file://.
// It mirrors framework-go-infrastructure-github/testinfra.StartLocalGitRepo but
// is reimplemented here with the git CLI so the black-box harness module takes no
// new dependency on a framework test helper.
func StartLocalGitRepo(t *testing.T, branch string) LocalGitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping local-git project-state proof")
	}
	if branch == "" {
		branch = "main"
	}
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")

	gitRun(t, root, "git", "init", "--bare", "--initial-branch="+branch, bare)

	// Seed an initial commit through a throwaway working clone so `branch` exists.
	work := filepath.Join(root, "seed")
	gitRun(t, root, "git", "clone", bare, work)
	gitRun(t, work, "git", "-c", "init.defaultBranch="+branch, "checkout", "-B", branch)
	gitRun(t, work, "git", "config", "user.email", "seed@aiarch.local")
	gitRun(t, work, "git", "config", "user.name", "seed")
	gitRun(t, work, "git", "commit", "--allow-empty", "-m", "seed")
	gitRun(t, work, "git", "push", "origin", branch)

	return LocalGitRepo{t: t, bare: bare, branch: branch}
}

// CommitCount returns the number of commits on the repo's branch. The seed commit
// is included, so a freshly-seeded repo reports 1; each design-artifact commit the
// server pushes increments it.
func (r LocalGitRepo) CommitCount(ctx context.Context) int {
	r.t.Helper()
	out := gitOut(ctx, r.t, r.bare, "rev-list", "--count", r.branch)
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		r.t.Fatalf("parse commit count %q: %v", out, err)
	}
	return n
}

// ListFiles returns every tracked path on the repo's branch (recursive). A passing
// design commit lands its JSON under .aiarch/state/..., so this lets a test assert
// the artifact file actually exists in the committed tree (not merely that a
// commit was made).
func (r LocalGitRepo) ListFiles(ctx context.Context) []string {
	r.t.Helper()
	out := gitOut(ctx, r.t, r.bare, "ls-tree", "-r", "--name-only", r.branch)
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// LastCommitMessage returns the subject of the tip commit on the branch — useful
// for asserting the design-artifact commit (not just that the count grew).
func (r LocalGitRepo) LastCommitMessage(ctx context.Context) string {
	r.t.Helper()
	return strings.TrimSpace(gitOut(ctx, r.t, r.bare, "log", "-1", "--format=%s", r.branch))
}

func gitRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func gitOut(ctx context.Context, t *testing.T, bare string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", bare}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// GitLocalEnv is the env that switches the server's design project-state substrate
// to the LOCAL on-disk git profile, pointing the per-project repo at a file:// repo.
// Mirrors cmd/server buildDesignProjectState's LOCAL branch
// (ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL + the repo URL). The cross-project registry
// repo is GONE (founder ruling 2026-06-14): the catalog is discovered by scanning the
// on-disk project repo, so there is no second repo URL to wire.
func GitLocalEnv(projectRepoURL string) []string {
	return []string{
		"ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true",
		"ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL=" + projectRepoURL,
	}
}
