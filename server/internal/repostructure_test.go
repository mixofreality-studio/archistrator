package internal_test

import (
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// repostructure_test.go is the CLOSED-STRUCTURE gate: the top-level repo tree and
// the server/cmd/ tool inventory are both allowlisted here, exhaustively. Anything
// tracked that isn't in one of the two sets below fails CI. This is the enforcement
// that prevents scratch directories and one-shot command-line tools from ever being
// merged again — it does NOT reimplement the component-package<->design conformance
// check (that's the methodcheck alignment pass); this gate only covers the tree
// shape and the cmd inventory, which alignment does not touch.
//
// Both allowlists are `var` sets (not consts) so that adding an intentional
// directory is a one-line, reviewable diff to THIS file.

// allowedTopLevel is the exhaustive set of tracked top-level path segments.
var allowedTopLevel = map[string]bool{
	".aiarch":     true,
	".claude":     true,
	".github":     true,
	".gitignore":  true,
	"docs":        true,
	"go.work":     true,
	"go.work.sum": true,
	"PUSH-APP.sh": true,
	"README.md":   true,
	"scripts":     true,
	"server":      true,
	"systemtests": true,
	"uitests":     true,
	"webApp":      true,
}

// allowedCmd is the exhaustive set of immediate subdirectory names under
// server/cmd/.
var allowedCmd = map[string]bool{
	"aiarch-state-mcp":     true,
	"appgen":               true,
	"clientgen":            true,
	"gen-systemtests":      true,
	"gen-uiprofiles":       true,
	"gen-uitests-fixtures": true,
	"internaltoolsgen":     true,
	"modelgen":             true,
	"server":               true,
}

// gitLsFiles runs `git -C <root> ls-files` and returns the tracked paths. It
// deliberately does NOT use os.ReadDir or reference a remote (origin/main may not
// exist in CI) — only the working-tree index, so gitignored scratch directories
// (research/, .superpowers/, .mcp-pilot/, .DS_Store, ...) are invisible to this
// gate exactly as they should be.
func gitLsFiles(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "ls-files")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	files := make([]string, 0, len(lines))
	for _, l := range lines {
		if l != "" {
			files = append(files, l)
		}
	}
	return files
}

// TestRepoStructureTopLevelIsClosed fails if any tracked top-level path segment is
// not in allowedTopLevel.
func TestRepoStructureTopLevelIsClosed(t *testing.T) {
	repoRoot := findRepoRootFromCwd(t)
	files := gitLsFiles(t, repoRoot)

	seen := map[string]bool{}
	for _, f := range files {
		seg := f
		if idx := strings.IndexByte(f, '/'); idx >= 0 {
			seg = f[:idx]
		}
		seen[seg] = true
	}

	segs := make([]string, 0, len(seen))
	for s := range seen {
		segs = append(segs, s)
	}
	sort.Strings(segs)

	for _, s := range segs {
		if !allowedTopLevel[s] {
			t.Errorf("unexpected top-level entry %q is tracked but not in allowedTopLevel (server/internal/repostructure_test.go) — "+
				"if this is intentional, add it to allowedTopLevel there; if it is a scratch or one-shot artifact, delete it instead of committing it", s)
		}
	}
}

// TestRepoStructureCmdIsClosed fails if any tracked immediate subdirectory of
// server/cmd/ is not in allowedCmd.
func TestRepoStructureCmdIsClosed(t *testing.T) {
	repoRoot := findRepoRootFromCwd(t)
	files := gitLsFiles(t, repoRoot)

	const prefix = "server/cmd/"
	seen := map[string]bool{}
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		rest := f[len(prefix):]
		idx := strings.IndexByte(rest, '/')
		if idx < 0 {
			// A tracked file directly under server/cmd/ (not inside a subdir) — not
			// expected, but not what this gate checks; skip rather than false-positive.
			continue
		}
		name := rest[:idx]
		seen[name] = true
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		if !allowedCmd[n] {
			t.Errorf("unexpected server/cmd/%s is tracked but not in allowedCmd (server/internal/repostructure_test.go) — "+
				"if this is an intentional new command, add it to allowedCmd there; if it is a scratch or one-shot tool, delete it instead of committing it", n)
		}
	}
}
