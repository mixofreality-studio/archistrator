package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// gitRepo is a throwaway git repository for the edit-scope gate.
type gitRepo struct {
	t    *testing.T
	root string
}

// newGitRepo is a repo whose trunk is main, seeded with one commit, checked out on the
// activity branch activity/C-x (as the venue's job is).
func newGitRepo(t *testing.T) gitRepo {
	t.Helper()
	r := newGitRepoOn(t, "main")
	r.git("checkout", "-q", "-b", "activity/C-x")
	return r
}

// newGitRepoOn is a repo seeded with one commit on trunk.
func newGitRepoOn(t *testing.T, trunk string) gitRepo {
	t.Helper()
	r := gitRepo{t: t, root: t.TempDir()}
	r.git("init", "-q", "-b", trunk)
	r.git("config", "user.name", "t")
	r.git("config", "user.email", "t@example.invalid")
	r.git("config", "commit.gpgsign", "false")
	r.commit("seed", map[string]string{"README.md": "seed\n"})
	return r
}

func (r gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit writes files (repo-relative path → content) and commits them; it returns the sha.
func (r gitRepo) commit(msg string, files map[string]string) string {
	r.t.Helper()
	for p, body := range files {
		full := filepath.Join(r.root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			r.t.Fatal(err)
		}
		r.git("add", "--", p)
	}
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// writeSpec writes a spec carrying scope to a temp file and returns its path.
func writeSpec(t *testing.T, scope projectstate.SpecEditScope) string {
	t.Helper()
	s := projectstate.TaskSpec{Activity: "C-x", Task: "construction", EditScope: scope}
	s.Hash = s.ComputeHash()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// violations runs the gate in r (main ref auto-resolved) and returns the paths it
// reports (nil when it passes).
func (r gitRepo) violations(spec, base, head string) []string {
	r.t.Helper()
	return r.violationsAgainst(spec, base, head, "")
}

func (r gitRepo) violationsAgainst(spec, base, head, mainRef string) []string {
	r.t.Helper()
	got, err := editScopeViolations(r.root, spec, base, head, mainRef)
	if err != nil {
		r.t.Fatalf("edit-scope: %v", err)
	}
	return got
}

var componentScope = projectstate.SpecEditScope{
	Components: []projectstate.SpecScopeComponent{{ID: "billing", Paths: []string{"server/internal/billing/**"}}},
	Extra:      []string{"server/go.mod"},
}

// TestEditScope_ReportsOnlyThePathsOutsideTheScope: a component path, an extra, and the
// state file pass; a path outside every glob (a workflow file included) is reported.
func TestEditScope_ReportsOnlyThePathsOutsideTheScope(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("work", map[string]string{
		"server/internal/billing/billing.go": "package billing\n",
		"server/go.mod":                      "module x\n",
		".aiarch/state/project.json":         "{}\n",
	})
	head := r.commit("stray", map[string]string{
		"server/internal/other/other.go": "package other\n",
		".github/workflows/x.yml":        "on: push\n",
	})
	got := r.violations(writeSpec(t, componentScope), base, head)
	want := []string{".github/workflows/x.yml", "server/internal/other/other.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("violations %v, want %v", got, want)
	}
}

// TestEditScope_StateOnly: a stateOnly task may change .aiarch/state/** alone.
func TestEditScope_StateOnly(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	head := r.commit("draft", map[string]string{
		".aiarch/state/project.json": "{}\n",
		"server/main.go":             "package main\n",
	})
	got := r.violations(writeSpec(t, projectstate.SpecEditScope{StateOnly: true, Extra: []string{"**"}}), base, head)
	if !slices.Equal(got, []string{"server/main.go"}) {
		t.Fatalf("violations %v", got)
	}
}

// TestEditScope_AnyPath: extra ["**"] admits every path, .github included.
func TestEditScope_AnyPath(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	head := r.commit("infra", map[string]string{".github/workflows/x.yml": "on: push\n", "deploy/chart.yaml": "x\n"})
	if got := r.violations(writeSpec(t, projectstate.SpecEditScope{Extra: []string{"**"}}), base, head); len(got) != 0 {
		t.Fatalf("violations %v", got)
	}
}

// TestEditScope_IgnoresMainMergedIn: content main brought in through a merge commit is
// not the task's edit (R13): only the branch's own first-parent commits are judged.
func TestEditScope_IgnoresMainMergedIn(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("work", map[string]string{"server/internal/billing/billing.go": "package billing\n"})
	r.git("checkout", "-q", "main")
	r.commit("someone else", map[string]string{"other/x.txt": "x\n", ".github/workflows/ci.yml": "on: push\n"})
	r.git("checkout", "-q", "activity/C-x")
	r.git("merge", "-q", "--no-edit", "main")
	head := r.commit("more work", map[string]string{"server/internal/billing/more.go": "package billing\n"})
	if got := r.violations(writeSpec(t, componentScope), base, head); len(got) != 0 {
		t.Fatalf("main's merged-in content must not count: %v", got)
	}
}

// TestEditScope_BaseAheadUsesMergeBase: the diff runs from merge-base(base, head), so a
// base that moved on (main advanced) does not count main's new commits against head.
func TestEditScope_BaseAheadUsesMergeBase(t *testing.T) {
	r := newGitRepo(t)
	head := r.commit("work", map[string]string{"server/internal/billing/billing.go": "package billing\n"})
	r.git("checkout", "-q", "main")
	base := r.commit("main moved", map[string]string{"elsewhere.txt": "x\n"})
	if got := r.violations(writeSpec(t, componentScope), base, head); len(got) != 0 {
		t.Fatalf("violations %v", got)
	}
}

// TestEditScope_CommandExitsOneListingViolations: the subcommand fails with exit 1 and
// names every violating path.
func TestEditScope_CommandExitsOneListingViolations(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	head := r.commit("stray", map[string]string{"a/x.go": "x\n", "b/y.go": "y\n"})
	t.Chdir(r.root)
	err := runEditScope([]string{"--spec", writeSpec(t, componentScope), "--base", base, "--head", head})
	var ee exitError
	if !errors.As(err, &ee) || ee.code != 1 || !strings.Contains(err.Error(), "a/x.go") || !strings.Contains(err.Error(), "b/y.go") {
		t.Fatalf("want exit 1 listing a/x.go and b/y.go, got %v", err)
	}
	if err := runEditScope([]string{"--spec", writeSpec(t, componentScope), "--base", base, "--head", base}); err != nil {
		t.Fatalf("no change is in scope: %v", err)
	}
}

// TestEditScope_NoFFSideBranchIsCaught (R23): an out-of-scope file committed on a side
// branch and merged --no-ff into the activity branch is the agent's edit.
func TestEditScope_NoFFSideBranchIsCaught(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.git("checkout", "-q", "-b", "side")
	r.commit("hide", map[string]string{"bad/x.txt": "x\n"})
	r.git("checkout", "-q", "activity/C-x")
	r.git("merge", "-q", "--no-ff", "--no-edit", "side")
	head := r.git("rev-parse", "HEAD")
	if got := r.violations(writeSpec(t, componentScope), base, head); !slices.Equal(got, []string{"bad/x.txt"}) {
		t.Fatalf("violations %v", got)
	}
}

// TestEditScope_FileAddedInsideAMergeIsCaught (R23): an evil merge — main merged in with
// --no-commit and an out-of-scope file added to the merge commit itself — reports that
// file and nothing main brought.
func TestEditScope_FileAddedInsideAMergeIsCaught(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.git("checkout", "-q", "main")
	r.commit("main moves", map[string]string{"other/x.txt": "x\n"})
	r.git("checkout", "-q", "activity/C-x")
	r.git("merge", "-q", "--no-ff", "--no-commit", "main")
	head := r.commit("merge main", map[string]string{"bad/evil.txt": "evil\n"})
	if got := r.violations(writeSpec(t, componentScope), base, head); !slices.Equal(got, []string{"bad/evil.txt"}) {
		t.Fatalf("violations %v", got)
	}
}

// TestEditScope_AddThenRemoveNetsOut: a file added and removed again changes no tree.
func TestEditScope_AddThenRemoveNetsOut(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("oops", map[string]string{"bad/tmp.txt": "x\n"})
	r.git("rm", "-q", "bad/tmp.txt")
	r.git("commit", "-q", "-m", "undo")
	head := r.git("rev-parse", "HEAD")
	if got := r.violations(writeSpec(t, componentScope), base, head); len(got) != 0 {
		t.Fatalf("violations %v", got)
	}
}

// TestEditScope_NoMainRefIsStrict: with no main ref to net against (no origin/main, no
// main), content merged in from the trunk counts; --main names the trunk explicitly.
func TestEditScope_NoMainRefIsStrict(t *testing.T) {
	r := newGitRepoOn(t, "trunk")
	r.git("checkout", "-q", "-b", "activity/C-x")
	base := r.git("rev-parse", "HEAD")
	r.git("checkout", "-q", "trunk")
	r.commit("trunk moves", map[string]string{"other/x.txt": "x\n"})
	r.git("checkout", "-q", "activity/C-x")
	r.git("merge", "-q", "--no-edit", "trunk")
	head := r.git("rev-parse", "HEAD")
	spec := writeSpec(t, componentScope)
	if got := r.violations(spec, base, head); !slices.Equal(got, []string{"other/x.txt"}) {
		t.Fatalf("strict fallback: violations %v", got)
	}
	if got := r.violationsAgainst(spec, base, head, "trunk"); len(got) != 0 {
		t.Fatalf("--main trunk nets trunk's content out: %v", got)
	}
}

// TestEditScope_RenameJudgesBothEnds: moving an in-scope file out of scope reports the
// new path.
func TestEditScope_RenameJudgesBothEnds(t *testing.T) {
	r := newGitRepo(t)
	r.commit("own", map[string]string{"server/internal/billing/a.go": "package billing\n"})
	base := r.git("rev-parse", "HEAD")
	r.git("mv", "server/internal/billing/a.go", "elsewhere.go")
	r.git("commit", "-q", "-m", "move")
	head := r.git("rev-parse", "HEAD")
	if got := r.violations(writeSpec(t, componentScope), base, head); !slices.Equal(got, []string{"elsewhere.go"}) {
		t.Fatalf("violations %v", got)
	}
}

// TestEditScope_RefusesAGlobOutsideTheGrammar: a spec glob that is not "**", "<dir>/**"
// or a literal path is a spec error (exit 2), never guessed at.
func TestEditScope_RefusesAGlobOutsideTheGrammar(t *testing.T) {
	r := newGitRepo(t)
	base := r.git("rev-parse", "HEAD")
	t.Chdir(r.root)
	err := runEditScope([]string{"--spec", writeSpec(t, projectstate.SpecEditScope{Extra: []string{"server/*.go"}}), "--base", base, "--head", base})
	var ee exitError
	if !errors.As(err, &ee) || ee.code != 2 || !strings.Contains(err.Error(), "server/*.go") {
		t.Fatalf("want exit 2 naming the glob, got %v", err)
	}
}

// TestScopeGlobMatch pins the edit-scope grammar: exactly "**", "<dir>/**" (a path
// boundary, so webApp/src/** does not reach webApp/srcx), or a literal path.
func TestScopeGlobMatch(t *testing.T) {
	for _, c := range []struct {
		glob, path string
		want       bool
	}{
		{"**", ".github/workflows/x.yml", true},
		{"server/internal/billing/**", "server/internal/billing/a/b.go", true},
		{"server/internal/billing/**", "server/internal/billingx/a.go", false},
		{"webApp/src/**", "webApp/src/a", true},
		{"webApp/src/**", "webApp/srcx/a", false},
		{"webApp/src/**", "webApp/src", false},
		{"server/go.mod", "server/go.mod", true},
		{"server/go.mod", "server/go.modx", false},
		// outside the grammar: matches nothing
		{"server/*.go", "server/main.go", false},
		{"server/**/x.go", "server/a/x.go", false},
		{"server/?.go", "server/a.go", false},
		{"/abs/**", "abs/x", false},
		{"", "", false},
	} {
		if got := scopeGlobMatch(c.glob, c.path); got != c.want {
			t.Errorf("scopeGlobMatch(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}
