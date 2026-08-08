package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installShim writes an executable POSIX shell script named `name` into a
// fresh temp dir and prepends that dir to PATH for the test's duration — the
// same pattern framework-go-infrastructure-llm/claudecli_test.go uses to fake
// `claude` without touching a real subscription in CI.
func installShim(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shim is a POSIX shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test shim, deliberately executable
		t.Fatalf("write %s shim: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRunPreflight_AllGreen(t *testing.T) {
	installShim(t, "git", "#!/bin/sh\necho git-shim\nexit 0\n")
	installShim(t, "claude", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo '2.0.0 (Claude Code)'; exit 0; fi\n"+
		"echo '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"ok\"}'\nexit 0\n")

	r := runPreflight(context.Background(), false)
	if r.Fatal() {
		t.Fatalf("expected non-fatal report, got gitErr=%v claudeErr=%v", r.gitErr, r.claudeErr)
	}
	if !r.authChecked || r.authErr != nil {
		t.Fatalf("expected a clean auth check, got authChecked=%v authErr=%v", r.authChecked, r.authErr)
	}
	if strings.Contains(r.Instructions(), "issue") {
		t.Fatalf("expected an all-clear Instructions text, got:\n%s", r.Instructions())
	}
}

func TestRunPreflight_GitMissing_Fatal(t *testing.T) {
	// Empty PATH except a claude shim — no git anywhere.
	installShim(t, "claude", "#!/bin/sh\necho '2.0.0'\nexit 0\n")
	dir := filepath.Dir(mustLookPath(t, "claude"))
	t.Setenv("PATH", dir)

	r := runPreflight(context.Background(), true)
	if !r.Fatal() {
		t.Fatal("expected Fatal()=true when git is absent")
	}
	if r.gitErr == nil {
		t.Fatal("expected gitErr to be set")
	}
	if !strings.Contains(r.Instructions(), "git was not found") {
		t.Fatalf("Instructions() missing the git install message:\n%s", r.Instructions())
	}
}

func TestRunPreflight_ClaudeMissing_Fatal(t *testing.T) {
	installShim(t, "git", "#!/bin/sh\nexit 0\n")
	dir := filepath.Dir(mustLookPath(t, "git"))
	t.Setenv("PATH", dir)

	r := runPreflight(context.Background(), true)
	if !r.Fatal() {
		t.Fatal("expected Fatal()=true when claude is absent")
	}
	if r.claudeErr == nil {
		t.Fatal("expected claudeErr to be set")
	}
}

func TestRunPreflight_SkipAuthCheck_NotFatal(t *testing.T) {
	installShim(t, "git", "#!/bin/sh\nexit 0\n")
	installShim(t, "claude", "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then exit 0; fi\nexit 1\n") // -p would fail if ever invoked

	r := runPreflight(context.Background(), true)
	if r.Fatal() {
		t.Fatalf("expected non-fatal (git+claude present), got %+v", r)
	}
	if r.authChecked {
		t.Fatal("expected authChecked=false when --skip-auth-check is honored")
	}
}

func TestRunPreflight_AuthProbeFails_NotFatal_ButReported(t *testing.T) {
	installShim(t, "git", "#!/bin/sh\nexit 0\n")
	installShim(t, "claude", "#!/bin/sh\n"+
		"if [ \"$1\" = \"--version\" ]; then echo '2.0.0'; exit 0; fi\n"+
		"echo 'Invalid API key · Please run /login' >&2\nexit 1\n")

	r := runPreflight(context.Background(), false)
	if r.Fatal() {
		t.Fatalf("an auth-probe failure must NOT be Fatal, got %+v", r)
	}
	if !r.authChecked || r.authErr == nil {
		t.Fatalf("expected authChecked=true and authErr set, got %+v", r)
	}
	if !strings.Contains(r.Instructions(), "authenticated") {
		t.Fatalf("Instructions() missing the auth-failure message:\n%s", r.Instructions())
	}
}

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	for dir := range strings.SplitSeq(os.Getenv("PATH"), string(os.PathListSeparator)) {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	t.Fatalf("shim %q not found on PATH", name)
	return ""
}
