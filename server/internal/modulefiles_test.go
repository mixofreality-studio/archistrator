package internal_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/sourcecontrol"
)

// The rail reads a module's appgen.json and go.mod so Tier 1 can hold a draft to the module it is
// built in, and that reach is READ-ONLY: the managed-file write verb stays on its own allowlist.
// These live here, not beside the component, for the reason projectrepo_test.go gives: the
// sourcecontrol package holds only its generated scenarios file (TestFileLayout).

// moduleFilesRepo is a repo whose main carries a module under server/ and files that ride the
// same base names where no module lives.
func moduleFilesRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "modrepo")
	files := map[string]string{
		"server/go.mod":                  "module example.com/app\n",
		"server/appgen.json":             `{"container":"x"}`,
		"vendor/x/go.mod":                "module vendored\n",
		"webApp/node_modules/y/go.mod":   "module nodemod\n",
		"README.md":                      "not managed\n",
		"server/internal/notmanaged.txt": "not managed\n",
	}
	for p, body := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil { //nolint:gosec // a repo file
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=seed", "-c", "user.email=seed@aiarch.local"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("commit", "-q", "-m", "a module under server")
	run("checkout", "-q", "-b", "checkout")
	return "file://" + dir
}

func TestManagedFileRead_ServesAModulesAppgenConfigAndGoMod(t *testing.T) {
	scm := sourcecontrol.NewGitLocalSourceControlAccess(moduleFilesRepo(t))
	repo := sourcecontrol.GitLocalRepoRefForProject("p")
	rc := fwra.Context{}
	for path, want := range map[string]string{"server/go.mod": "module example.com/app\n", "server/appgen.json": `{"container":"x"}`} {
		got, err := scm.ReadManagedFile(rc, repo, path, sourcecontrol.RepoCredential{})
		if err != nil || !got.Found || string(got.Content) != want {
			t.Errorf("ReadManagedFile(%q) = %q found=%v err=%v, want %q", path, got.Content, got.Found, err, want)
		}
	}
	missing, err := scm.ReadManagedFile(rc, repo, "other/go.mod", sourcecontrol.RepoCredential{})
	if err != nil || missing.Found {
		t.Errorf("a module file that is not there: found=%v err=%v, want a clean miss", missing.Found, err)
	}
}

func TestManagedFileRead_RefusesPathsOutsideTheReadAllowlist(t *testing.T) {
	scm := sourcecontrol.NewGitLocalSourceControlAccess(moduleFilesRepo(t))
	repo := sourcecontrol.GitLocalRepoRefForProject("p")
	for _, path := range []string{
		"../go.mod", "server/../go.mod", "/server/go.mod", "server//go.mod", ".git/go.mod",
		"vendor/x/go.mod", "webApp/node_modules/y/go.mod", "README.md", "server/internal/notmanaged.txt", "server/go.sum",
	} {
		if _, err := scm.ReadManagedFile(fwra.Context{}, repo, path, sourcecontrol.RepoCredential{}); err == nil {
			t.Errorf("ReadManagedFile(%q) was allowed", path)
		}
	}
}

// The write verb does not follow the read allowlist: a go.mod or appgen.json below the root is
// refused, and nothing is pushed.
func TestCommitManagedFiles_RefusesAModulesFilesThatReadsAllow(t *testing.T) {
	scm := sourcecontrol.NewGitLocalSourceControlAccess(moduleFilesRepo(t))
	repo := sourcecontrol.GitLocalRepoRefForProject("p")
	for _, path := range []string{"server/go.mod", "server/appgen.json", "vendor/x/go.mod", "webApp/node_modules/y/go.mod", ".git/go.mod"} {
		_, err := scm.CommitManagedFiles(fwra.Context{}, repo, []sourcecontrol.ManagedFile{{Path: path, Content: []byte("x")}}, sourcecontrol.RepoCredential{})
		if err == nil {
			t.Errorf("CommitManagedFiles seated %q", path)
		}
	}
}
