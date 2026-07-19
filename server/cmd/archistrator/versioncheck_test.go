package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionSkewWarning_SameRevisionProducesNoWarning(t *testing.T) {
	own := buildIdentity{Version: "v0.1.0", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	child := buildIdentity{Version: "v0.1.0", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if got := versionSkewWarning(own, child, "/path/to/archistrator-server"); got != "" {
		t.Errorf("versionSkewWarning = %q, want empty for identical revisions", got)
	}
}

func TestVersionSkewWarning_DifferentRevisionProducesWarning(t *testing.T) {
	own := buildIdentity{Version: "v0.1.0", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	child := buildIdentity{Version: "v0.2.0", Revision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	got := versionSkewWarning(own, child, "/path/to/archistrator-server")
	if got == "" {
		t.Fatal("versionSkewWarning = empty, want a warning for differing revisions")
	}
	if !strings.Contains(got, "VERSION SKEW") {
		t.Errorf("warning missing VERSION SKEW marker: %q", got)
	}
	if !strings.Contains(got, "/path/to/archistrator-server") {
		t.Errorf("warning does not name the child binary path: %q", got)
	}
	if !strings.Contains(got, "aaaaaaaaaaaa") || !strings.Contains(got, "bbbbbbbbbbbb") {
		t.Errorf("warning does not name both short revisions: %q", got)
	}
}

func TestVersionSkewWarning_MissingRevisionSkipsCheck(t *testing.T) {
	withRev := buildIdentity{Version: "v0.1.0", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	empty := buildIdentity{}
	if got := versionSkewWarning(empty, withRev, "/path"); got != "" {
		t.Errorf("versionSkewWarning(own empty) = %q, want empty (cannot verify)", got)
	}
	if got := versionSkewWarning(withRev, empty, "/path"); got != "" {
		t.Errorf("versionSkewWarning(child empty) = %q, want empty (cannot verify)", got)
	}
}

func TestOwnBuildIdentity_ReadsThisTestBinary(_ *testing.T) {
	// go test builds a real binary with build info attached; ReadBuildInfo
	// should always succeed for it (even if the module isn't a VCS checkout,
	// in which case Revision is simply "").
	id := ownBuildIdentity()
	_ = id // ok is asserted inside ownBuildIdentity; just confirm no panic and a callable result.
}

// runGit runs `git <args...>` in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestChildBuildIdentity_DetectsMismatchAgainstRealBinary is the "fake
// mismatched binary" test the brief asks for: it builds a REAL, tiny Go
// binary in an isolated throwaway git repo (so its embedded vcs.revision is
// guaranteed to differ from whatever commit this archistrator module itself
// is checked out at), reads that binary's build identity via
// childBuildIdentity (debug/buildinfo.ReadFile — no execution), and confirms
// versionSkewWarning fires against a synthetic "own" identity with a
// different, fixed revision.
func TestChildBuildIdentity_DetectsMismatchAgainstRealBinary(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "test@example.invalid")
	runGit(t, dir, "config", "user.name", "test")

	mainGo := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fakechild\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "fake child binary fixture")

	binPath := filepath.Join(dir, "fakechild")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build fake child: %v\n%s", err, out)
	}

	child := childBuildIdentity(binPath)
	if child.Revision == "" {
		t.Fatal("expected the fake child binary to have an embedded vcs.revision (git repo + go build should stamp one)")
	}

	own := buildIdentity{Version: "v9.9.9", Revision: "ffffffffffffffffffffffffffffffffffffffff"}
	if own.Revision == child.Revision {
		t.Fatal("test fixture bug: own revision collided with the fake child's real revision")
	}

	warning := versionSkewWarning(own, child, binPath)
	if warning == "" {
		t.Fatal("expected a version-skew warning: own and the real fake-child binary have different revisions")
	}
	if !strings.Contains(warning, "VERSION SKEW") {
		t.Errorf("warning missing VERSION SKEW marker: %q", warning)
	}
	if !strings.Contains(warning, binPath) {
		t.Errorf("warning does not name the child binary path %q: %q", binPath, warning)
	}
}

// TestChildBuildIdentity_MissingFileIsEmpty covers the "cannot verify"
// degrade path: a nonexistent path must not panic or error out to the
// caller — it just yields an empty identity, which versionSkewWarning then
// treats as unverifiable (no false alarm).
func TestChildBuildIdentity_MissingFileIsEmpty(t *testing.T) {
	id := childBuildIdentity(filepath.Join(t.TempDir(), "does-not-exist"))
	if id.Revision != "" || id.Version != "" {
		t.Errorf("childBuildIdentity(missing) = %+v, want zero value", id)
	}
}
