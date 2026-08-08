package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// TestRunInit_EmptyDir_ProducesArtifacts is Task-5 Step-1(a): init in an empty
// temp dir produces every artifact the brief names.
func TestRunInit_EmptyDir_ProducesArtifacts(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()

	var out bytes.Buffer
	if err := RunInit(dir, &out); err != nil {
		t.Fatalf("RunInit: %v", err)
	}

	// git repo present.
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git missing: %v", err)
	}

	// receive.denyCurrentBranch=updateInstead set.
	got := gitConfigGet(t, dir, "receive.denyCurrentBranch")
	if got != "updateInstead" {
		t.Fatalf("receive.denyCurrentBranch = %q, want updateInstead", got)
	}

	// .aiarch/state/ exists, and — the empty-project shape that passes
	// cmd/aiarch-state-mcp validate — carries NO project.json (validate.go's
	// documented "no committed .aiarch state ⇒ clean pass" case).
	stateInfo, err := os.Stat(filepath.Join(dir, ".aiarch", "state"))
	if err != nil || !stateInfo.IsDir() {
		t.Fatalf(".aiarch/state not a directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".aiarch", "state", "project.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no project.json in a fresh scaffold, stat err = %v", err)
	}

	// .mcp.json registers archistrator as an HTTP MCP server pointed at the
	// standalone `archistrator serve` daemon's own /mcp mount (amendment
	// 2026-07-19 — no more stdio auto-start).
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf(".mcp.json is not valid JSON: %v\n%s", err, raw)
	}
	entry, ok := doc.MCPServers["archistrator"]
	if !ok {
		t.Fatalf(".mcp.json has no archistrator entry: %s", raw)
	}
	if entry.Type != "http" || entry.URL != "http://127.0.0.1:8877/mcp" {
		t.Fatalf("archistrator entry = %+v, want type=http url=http://127.0.0.1:8877/mcp", entry)
	}

	if !strings.Contains(out.String(), "Run `archistrator serve` in this directory, then open Claude Code here.") {
		t.Fatalf("output missing the handoff line, got:\n%s", out.String())
	}
}

// TestRunInit_Idempotent_NeverClobbers covers Step-1(a)'s idempotency
// requirement: re-running init on an already-scaffolded directory adopts the
// existing repo and NEVER clobbers a committed project.json or a
// hand-edited .mcp.json entry for a different server.
// The must* helpers below carry the "an IO/JSON failure in test SETUP is a fatal,
// not an assertion" boilerplate that otherwise doubles the length — and the
// cyclomatic complexity — of every fixture-heavy test in this package.

// mustRead reads a file the test has already created; a failure is setup, not a
// finding.
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// mustWrite writes a fixture file.
func mustWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustUnmarshal decodes JSON the test itself produced or just read back.
func mustUnmarshal(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

// mustMarshal encodes a fixture document.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestRunInit_Idempotent_NeverClobbers(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()

	var first bytes.Buffer
	if err := RunInit(dir, &first); err != nil {
		t.Fatalf("first RunInit: %v", err)
	}

	// Simulate a real design session: commit a project.json into
	// .aiarch/state/, and hand-add an unrelated MCP server entry.
	stateFile := filepath.Join(dir, ".aiarch", "state", "project.json")
	marker := []byte(`{"id":"deadbeef","version":1}`)
	mustWrite(t, stateFile, marker)

	mcpPath := filepath.Join(dir, ".mcp.json")
	var doc map[string]any
	mustUnmarshal(t, mustRead(t, mcpPath), &doc)
	servers, _ := doc["mcpServers"].(map[string]any)
	servers["other-tool"] = map[string]any{"command": "other-tool", "args": []string{"serve"}}
	mustWrite(t, mcpPath, mustMarshal(t, doc))

	// Re-run init.
	var second bytes.Buffer
	if err := RunInit(dir, &second); err != nil {
		t.Fatalf("second RunInit: %v", err)
	}

	// project.json byte-identical — never clobbered.
	gotMarker := mustRead(t, stateFile)
	if !bytes.Equal(gotMarker, marker) {
		t.Fatalf("project.json was modified by re-running init: got %s, want %s", gotMarker, marker)
	}

	// .mcp.json still has BOTH entries.
	raw3 := mustRead(t, mcpPath)
	var doc3 struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	mustUnmarshal(t, raw3, &doc3)
	if _, ok := doc3.MCPServers["archistrator"]; !ok {
		t.Fatalf(".mcp.json lost the archistrator entry after re-init: %s", raw3)
	}
	if _, ok := doc3.MCPServers["other-tool"]; !ok {
		t.Fatalf(".mcp.json lost the hand-added other-tool entry after re-init: %s", raw3)
	}

	// Still receive.denyCurrentBranch=updateInstead (idempotent re-apply).
	got := gitConfigGet(t, dir, "receive.denyCurrentBranch")
	if got != "updateInstead" {
		t.Fatalf("receive.denyCurrentBranch = %q after re-init, want updateInstead", got)
	}

	// Still exactly ONE .git directory (adopted, not re-initialized into a
	// nested repo).
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git missing after re-init: %v", err)
	}
}

// TestRunInit_AdoptsExistingGitRepo covers "git init if absent" the other
// direction: a directory that is ALREADY a git repo (e.g. the user ran
// `git init`/cloned before trying archistrator) is adopted, not re-initialized
// or rejected.
func TestRunInit_AdoptsExistingGitRepo(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()

	initCmd := exec.Command("git", "init", "--quiet", dir)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("pre-seed git init: %v: %s", err, out)
	}
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("seed README: %v", err)
	}
	commitCmd := exec.Command("git", "-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t",
		"add", "-A")
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	commit2 := exec.Command("git", "-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t",
		"commit", "-q", "-m", "seed")
	if out, err := commit2.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}

	var out bytes.Buffer
	if err := RunInit(dir, &out); err != nil {
		t.Fatalf("RunInit on existing repo: %v", err)
	}

	if !strings.Contains(out.String(), "adopted existing git repo") {
		t.Fatalf("expected 'adopted existing git repo' in output, got:\n%s", out.String())
	}
	if _, err := os.Stat(readmePath); err != nil {
		t.Fatalf("README.md lost: %v", err)
	}
	got := gitConfigGet(t, dir, "receive.denyCurrentBranch")
	if got != "updateInstead" {
		t.Fatalf("receive.denyCurrentBranch = %q, want updateInstead", got)
	}
}

func gitConfigGet(t *testing.T, dir, key string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "config", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git config --get %s: %v", key, err)
	}
	return strings.TrimSpace(string(out))
}
