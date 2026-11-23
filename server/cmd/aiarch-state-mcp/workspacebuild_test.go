package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/server/internal/engine/review"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// checkLog is the log the runner kept for the one check under outDir.
func checkLog(t *testing.T, outDir string) string {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(outDir, "checks", "*.log"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, l := range logs {
		b, err := os.ReadFile(l) //nolint:gosec // G304: a log the test's runner wrote.
		if err != nil {
			t.Fatal(err)
		}
		all.Write(b)
	}
	return all.String()
}

// TestWorkspaceBuildCheckBuildsAgainstTheEdit executes the check the task spec of a component owning a
// workspace repo's directory carries (command:build-workspace, resolved from archistrator's own
// project): in a run laid out the way the venue lays it out (the project's checkout and the
// workspace repo's in unrelated directories), it builds the project's module against the
// workspace checkout's CURRENT content, so an API the edit removed turns it red, and one it
// provides turns it green.
func TestWorkspaceBuildCheckBuildsAgainstTheEdit(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	var anyProject projectstate.ProjectID
	proj, _, err := projectstate.DecodeProjectJSON(raw, anyProject)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := deriveTaskSpec(&proj, "C-design-health-engine", "construction")
	if err != nil {
		t.Fatal(err)
	}
	var build review.SpecCheck
	for _, c := range spec.Checks {
		if c.ID == "command:build-workspace" {
			build = c
		}
	}
	if build.ID == "" || build.Source.Dir == nil {
		t.Fatal("the spec carries no command:build-workspace check")
	}
	root, plat := t.TempDir(), t.TempDir()
	writeRepoFile(t, root, *build.Source.Dir+"/go.mod", []byte("module example.com/app\n\ngo 1.21\n\nrequire example.com/plat v0.0.0\n"))
	writeRepoFile(t, root, *build.Source.Dir+"/main.go", []byte("package main\n\nimport \"example.com/plat/lib\"\n\nfunc main() { lib.Hello() }\n"))
	writeRepoFile(t, plat, "go.mod", []byte("module example.com/plat\n\ngo 1.21\n"))
	writeRepoFile(t, plat, "lib/lib.go", []byte("package lib\n\nfunc Hello() {}\n"))
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "")
	out := t.TempDir()
	r := checkRunner{root: root, outDir: out, now: time.Now, workspaces: map[string]string{"archistrator-platform": plat}}
	if res := r.Run(context.Background(), []review.SpecCheck{build}); res[0].Status != checkPass {
		t.Fatalf("the workspace build over a satisfying edit: status %s\n%s", res[0].Status, checkLog(t, out))
	}
	writeRepoFile(t, plat, "lib/lib.go", []byte("package lib\n\nfunc Goodbye() {}\n"))
	res := r.Run(context.Background(), []review.SpecCheck{build})
	if res[0].Status == checkPass {
		t.Fatal("the workspace build passed over an edit that removed the API the project calls: it is not building against the edit")
	}
	if log := checkLog(t, out); !strings.Contains(log, "lib.Hello") {
		t.Fatalf("the build failed for the wrong reason:\n%s", log)
	}
	// A run with no workspace checkout cannot vouch for the build.
	r.workspaces = nil
	if res := r.Run(context.Background(), []review.SpecCheck{build}); res[0].Status == checkPass {
		t.Fatal("the workspace build passed in a run that has no workspace checkout")
	}
}
