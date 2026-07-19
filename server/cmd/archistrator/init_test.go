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

	// .mcp.json registers archistrator exactly as specified.
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf(".mcp.json is not valid JSON: %v\n%s", err, raw)
	}
	entry, ok := doc.MCPServers["archistrator"]
	if !ok {
		t.Fatalf(".mcp.json has no archistrator entry: %s", raw)
	}
	if entry.Command != "archistrator" || len(entry.Args) != 1 || entry.Args[0] != "mcp" {
		t.Fatalf("archistrator entry = %+v, want command=archistrator args=[mcp]", entry)
	}

	if !strings.Contains(out.String(), "Start Claude Code in this directory and say: design my app.") {
		t.Fatalf("output missing the handoff line, got:\n%s", out.String())
	}
}

// TestRunInit_Idempotent_NeverClobbers covers Step-1(a)'s idempotency
// requirement: re-running init on an already-scaffolded directory adopts the
// existing repo and NEVER clobbers a committed project.json or a
// hand-edited .mcp.json entry for a different server.
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
	if err := os.WriteFile(stateFile, marker, 0o644); err != nil {
		t.Fatalf("seed project.json: %v", err)
	}

	mcpPath := filepath.Join(dir, ".mcp.json")
	raw, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal .mcp.json: %v", err)
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	servers["other-tool"] = map[string]any{"command": "other-tool", "args": []string{"serve"}}
	out2, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal .mcp.json: %v", err)
	}
	if err := os.WriteFile(mcpPath, out2, 0o644); err != nil {
		t.Fatalf("rewrite .mcp.json: %v", err)
	}

	// Re-run init.
	var second bytes.Buffer
	if err := RunInit(dir, &second); err != nil {
		t.Fatalf("second RunInit: %v", err)
	}

	// project.json byte-identical — never clobbered.
	gotMarker, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("re-read project.json: %v", err)
	}
	if !bytes.Equal(gotMarker, marker) {
		t.Fatalf("project.json was modified by re-running init: got %s, want %s", gotMarker, marker)
	}

	// .mcp.json still has BOTH entries.
	raw3, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("re-read .mcp.json: %v", err)
	}
	var doc3 struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw3, &doc3); err != nil {
		t.Fatalf("unmarshal .mcp.json after re-init: %v", err)
	}
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
