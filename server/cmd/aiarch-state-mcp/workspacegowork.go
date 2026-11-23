package main

// workspacegowork.go builds the go.work a project's workspace build check runs under. The
// project's committed go.work spells its sibling repos as relative paths from the repo root
// ("../archistrator-platform/..."), but a run lays the checkouts out in its own temporary
// directories, so no committed path reaches them. The check therefore names its workspace by
// a marker (GOWORK=@workspace) and the runner writes the file from what the venue handed it:
// the check's own module plus every module of every workspace repo checkout
// (AIARCH_WORKSPACE_ROOTS, absolute), so the build sees exactly the edit under judgment.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// workspaceGoWorkMarker is the GOWORK assignment a command check writes in its `env` argv to
// build against the run's workspace checkouts (review's serviceScopeAndChecks emits it).
const workspaceGoWorkMarker = "GOWORK=@workspace"

// usesWorkspaceGoWork reports whether argv is an `env` invocation carrying the marker.
func usesWorkspaceGoWork(argv []string) bool {
	return len(argv) > 1 && argv[0] == "env" && slices.Contains(argv[1:], workspaceGoWorkMarker)
}

// withWorkspaceGoWork returns argv with the marker replaced by a go.work written for module
// dir dir and the workspace repos' checkouts, and the cleanup that removes it.
func (r checkRunner) withWorkspaceGoWork(argv []string, dir string) ([]string, func(), error) {
	if len(r.workspaces) == 0 {
		return nil, nil, fmt.Errorf("the check builds against the workspace repos, and this run has no checkout of any")
	}
	holder, err := os.MkdirTemp("", "aiarch-gowork-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(holder) }
	file, err := r.writeGoWork(holder, dir)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	out := slices.Clone(argv)
	for i, a := range out {
		if a == workspaceGoWorkMarker {
			out[i] = "GOWORK=" + file
		}
	}
	return out, cleanup, nil
}

// writeGoWork writes holder/go.work using module dir and every module of the workspace
// checkouts, with the project's go.work.sum beside it, and returns its path.
func (r checkRunner) writeGoWork(holder, dir string) (string, error) {
	modules := []string{dir}
	for _, id := range slices.Sorted(maps.Keys(r.workspaces)) {
		found, err := modulesOf(r.workspaces[id])
		if err != nil {
			return "", fmt.Errorf("workspace repo %q: %w", id, err)
		}
		modules = append(modules, found...)
	}
	// The go command resolves its working directory's symlinks, so a module spelled through one
	// (macOS's /var -> /private/var) would not be recognised as the directory it runs in.
	for i, m := range modules {
		realDir, err := filepath.EvalSymlinks(m)
		if err != nil {
			return "", err
		}
		modules[i] = realDir
	}
	goVersion := "1.21"
	for _, m := range modules {
		if v := goDirective(filepath.Join(m, "go.mod")); v != "" && compareGoVersion(v, goVersion) > 0 {
			goVersion = v
		}
	}
	var b strings.Builder
	b.WriteString("go " + goVersion + "\n\nuse (\n")
	for _, m := range modules {
		b.WriteString("\t" + strconv.Quote(m) + "\n")
	}
	b.WriteString(")\n")
	file := filepath.Join(holder, "go.work")
	if err := os.WriteFile(file, []byte(b.String()), 0o600); err != nil {
		return "", err
	}
	if sum, err := os.ReadFile(filepath.Join(r.root, "go.work.sum")); err == nil { //nolint:gosec // G304: the project's own checkout.
		if err := os.WriteFile(filepath.Join(holder, "go.work.sum"), sum, 0o600); err != nil {
			return "", err
		}
	}
	return file, nil
}

// modulesOf is the Go modules of one workspace checkout: the root itself when it holds a go.mod,
// and each directory directly under it that does (the platform's modules sit side by side).
func modulesOf(root string) ([]string, error) {
	var out []string
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		out = append(out, root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "go.mod")); err == nil {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s holds no Go module", root)
	}
	return out, nil
}

var goDirectiveRE = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)

// goDirective is the go line of the go.mod at path, or "".
func goDirective(path string) string {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: a module of the run's own checkouts.
	if err != nil {
		return ""
	}
	if m := goDirectiveRE.FindSubmatch(raw); m != nil {
		return string(m[1])
	}
	return ""
}

// compareGoVersion orders two go directive versions ("1.25", "1.25.4"), numerically.
func compareGoVersion(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range 3 {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
