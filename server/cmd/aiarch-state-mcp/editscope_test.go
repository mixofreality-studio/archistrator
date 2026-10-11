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

func newGitRepo(t *testing.T) gitRepo {
	t.Helper()
	r := gitRepo{t: t, root: t.TempDir()}
	r.git("init", "-q", "-b", "main")
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

// violations runs the gate in r and returns the paths it reports (nil when it passes).
func (r gitRepo) violations(spec, base, head string) []string {
	r.t.Helper()
	got, err := editScopeViolations(r.root, spec, base, head)
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
	r.git("checkout", "-q", "-b", "activity/C-x")
	base := r.git("rev-parse", "HEAD")
	r.commit("work", map[string]string{"server/internal/billing/billing.go": "package billing\n"})
	r.git("checkout", "-q", "main")
	r.commit("someone else", map[string]string{"webApp/src/app.ts": "x\n", ".github/workflows/ci.yml": "on: push\n"})
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
	r.git("checkout", "-q", "-b", "activity/C-x")
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

func TestScopeGlobMatch(t *testing.T) {
	for _, c := range []struct {
		glob, path string
		want       bool
	}{
		{"**", ".github/workflows/x.yml", true},
		{"server/internal/billing/**", "server/internal/billing/a/b.go", true},
		{"server/internal/billing/**", "server/internal/billingx/a.go", false},
		{"server/go.mod", "server/go.mod", true},
		{"server/*.go", "server/main.go", true},
		{"server/*.go", "server/a/main.go", false},
	} {
		if got := scopeGlobMatch(c.glob, c.path); got != c.want {
			t.Errorf("scopeGlobMatch(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}
