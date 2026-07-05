package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRig_FullDraftCycleOverStdio builds the binary and drives a full draft cycle over a
// real stdio MCP connection: initialize (the client handshake), tools/list, a rejected
// putDraftModel (bad enum), an accepted putDraftModel, and publishDraft — asserting the
// committed state on the session branch. It is the end-to-end proof the binary speaks MCP.
func TestRig_FullDraftCycleOverStdio(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	repo := initGitRepoWithProject(t, minimalProject(), "aiarch-design/rigproj/3")

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envArtifactKind+"=Volatilities",
		envJobMode+"=draft",
		envTargetBranch+"=aiarch-design/rigproj/3",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to MCP server: %v", err)
	}
	defer func() { _ = session.Close() }()

	// tools/list — the draft-mode set must include putDraftModel and NOT setCritiqueVerdict.
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"listResearchSources", "getCommittedSlot", "getDraftSlot", "getReviewThread", "putDraftModel", "respondToReviewComment", "publishDraft"} {
		if !names[want] {
			t.Fatalf("draft-mode tool %q missing from tools/list: %v", want, names)
		}
	}
	if names["setCritiqueVerdict"] {
		t.Fatalf("draft mode must NOT expose setCritiqueVerdict")
	}

	// A bad enum is rejected as an IsError tool result (self-correctable).
	res := callTool(t, ctx, session, "putDraftModel", map[string]any{
		"model": map[string]any{"items": []any{map[string]any{"name": "P", "rationale": "r", "axis": "changes a lot"}}},
	})
	if !res.IsError {
		t.Fatalf("expected IsError for a bad enum draft")
	}

	// A valid model is accepted.
	res = callTool(t, ctx, session, "putDraftModel", map[string]any{
		"model": map[string]any{"items": []any{map[string]any{"name": "P", "rationale": "r", "axis": "sameCustomerOverTime"}}},
	})
	if res.IsError {
		t.Fatalf("valid draft rejected: %s", contentText(res))
	}

	// publishDraft commits + pushes the session branch.
	res = callTool(t, ctx, session, "publishDraft", map[string]any{"message": "draft volatilities"})
	if res.IsError {
		t.Fatalf("publishDraft failed: %s", contentText(res))
	}

	// The pushed branch tip carries a valid, methodcheck-clean project.json with the draft.
	raw := gitShow(t, repo, "aiarch-design/rigproj/3", ".aiarch/state/project.json")
	proj, ok, derr := projectstate.DecodeProjectJSON(raw, "rigproj")
	if derr != nil || !ok {
		t.Fatalf("committed project.json does not decode: %v", derr)
	}
	if proj.Volatilities.Status != projectstate.ReviewCommitted || proj.Volatilities.Model == nil {
		t.Fatalf("committed volatilities slot not populated: %+v", proj.Volatilities)
	}
	findings, _ := methodcheck.ValidateProjectJSON(raw)
	if len(filterErrorFindings(findings)) != 0 {
		t.Fatalf("committed state has methodcheck errors: %v", findings)
	}
}

// TestRig_CritiqueModeToolSet proves critique mode omits putDraftModel and includes
// setCritiqueVerdict.
func TestRig_CritiqueModeToolSet(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin := buildBinary(t)
	p := minimalProject()
	p.Mission = projectstate.ArtifactSlot{Status: projectstate.ReviewCommitted, Model: &projectstate.MissionStatement{}}
	repo := initGitRepoWithProject(t, p, "aiarch-design/rigproj/0")

	cmd := exec.Command(bin)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(),
		envArtifactKind+"=Mission",
		envJobMode+"=critique",
		envProjectID+"=rigproj",
		envStateRoot+"="+repo,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "rig", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range tools.Tools {
		names[tl.Name] = true
	}
	if names["putDraftModel"] {
		t.Fatalf("critique mode must NOT expose putDraftModel")
	}
	if !names["setCritiqueVerdict"] {
		t.Fatalf("critique mode must expose setCritiqueVerdict")
	}
}

// ---- rig helpers ----

func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "aiarch-state-mcp")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v: %s", err, out)
	}
	return bin
}

func initGitRepoWithProject(t *testing.T, p projectstate.Project, branch string) string {
	t.Helper()
	repo := t.TempDir()
	bare := t.TempDir()
	mustGit(t, bare, "init", "--bare", "-b", "main")
	mustGit(t, repo, "init", "-b", "main")
	mustGit(t, repo, "config", "user.name", "rig")
	mustGit(t, repo, "config", "user.email", "rig@example.com")
	mustGit(t, repo, "remote", "add", "origin", bare)

	if err := os.MkdirAll(filepath.Join(repo, statePathPrefix), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := projectstate.EncodeProjectJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, statePathPrefix, projectFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "commit", "-m", "seed")
	mustGit(t, repo, "checkout", "-b", branch)
	mustGit(t, repo, "push", "-u", "origin", branch)
	return repo
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func gitShow(t *testing.T, repo, branch, path string) []byte {
	t.Helper()
	cmd := exec.Command("git", "show", branch+":"+path)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git show %s:%s: %v: %s", branch, path, err, out)
	}
	return out
}

func callTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

func contentText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
