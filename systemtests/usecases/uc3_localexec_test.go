package usecases

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator/systemtests/internal/harness"
)

// I-UC3-LOCALEXEC — local-first-init-funnel Task 6 (docs/superpowers/plans/2026-07-
// 19-local-first-init-funnel.md): the LOCAL construction executor. A local-profile
// server (ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true) with NO GitHub App creds AND
// ARCHISTRATOR_CONSTRUCTION_DRYRUN=false must dispatch construction via headless
// `claude` directly — no GitHub Actions rail, no dry-run stub. Unlike UC3's dry-run
// tests (uc3_construction_test.go), this test selects the REAL local executor
// (server/internal/resourceaccess/constructionpipeline's local-executor realisation,
// FinalizeConstructionPipelineAccess's third arm) and proves it end-to-end at the
// wire level: a fake `claude` shim on the server subprocess's PATH stands in for
// the real CLI (the SAME no-real-subscription-in-CI discipline
// framework-go-infrastructure-llm/claudecli_test.go and the RA's own
// localexec-section unit tests in constructionpipelineaccess_test.go use), while
// cmd/aiarch-state-mcp is built for REAL from source and its path is captured from
// the shim's own --mcp-config invocation, proving the rig is genuinely attached —
// not merely asserted from the RA's internal unit tests.
//
// SYNCHRONOUS DISPATCH OUTCOME: as with the dry-run UC3 tests, ExecuteNextActivity
// returns this tick's dispatch decision synchronously; the pump then drives the
// activity to completion in the background via the real subprocess.

// buildStateMCPBinaryOnce builds cmd/aiarch-state-mcp from source ONCE per test
// binary run (mirrors harness.BuildServerBinary's own build-from-source pattern),
// memoized so multiple test functions in this file share one build.
var (
	stateMCPBinOnce sync.Once
	stateMCPBinPath string
	stateMCPBinErr  error
)

func buildStateMCPBinary(t *testing.T) string {
	t.Helper()
	stateMCPBinOnce.Do(func() {
		root, err := usecasesModuleRoot()
		if err != nil {
			stateMCPBinErr = err
			return
		}
		serverDir := filepath.Join(root, "..", "server")
		bin := filepath.Join(os.TempDir(), fmt.Sprintf("aiarch-state-mcp-%d", os.Getpid()))
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/aiarch-state-mcp")
		cmd.Dir = serverDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			stateMCPBinErr = fmt.Errorf("build aiarch-state-mcp in %s: %w\n%s", serverDir, err, out)
			return
		}
		stateMCPBinPath = bin
	})
	if stateMCPBinErr != nil {
		t.Fatalf("buildStateMCPBinary: %v", stateMCPBinErr)
	}
	return stateMCPBinPath
}

// usecasesModuleRoot walks up from this source file to the systemtests module root
// (mirrors harness.moduleRoot's technique, reimplemented here since it is
// unexported in the harness package).
func usecasesModuleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot resolve caller for module root")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", file)
		}
		dir = parent
	}
}

// installLocalExecClaudeShim writes a `claude` shim into a fresh dir and returns
// that dir (for the caller to prepend to the server SUBPROCESS's PATH — NOT this
// test process's own PATH, since the shim must be resolvable by the server, which
// spawns it, not by this Go test binary). The shim answers TWO invocation shapes:
//
//   - `claude --version`      → the boot-time llm.PreflightClaudeCLI probe
//     (cmd/server/hooks.go's resolveWorkerProvider) — a plain success line.
//   - the construction dispatch shape (--dangerously-skip-permissions --settings
//     <path> --mcp-config <path> --strict-mcp-config --output-format stream-json --verbose -p
//     <prompt> — the Fix-subagent Task 6 sandboxed-by-default shape, THE
//     INVARIANT in constructionpipelineaccess.go's claudeArgv) → captures argv +
//     the --mcp-config file's content + the --settings (Tier-2 sandbox) file's
//     content into captureDir, makes one commit, exits 0.
//
// captureDir must be a path OUTSIDE the git working tree the shim itself commits
// into (it is — a sibling temp dir this test controls).
func installLocalExecClaudeShim(t *testing.T, captureDir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shim is a POSIX shell script; local-executor is not exercised on windows in CI")
	}
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir capture dir: %v", err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = '--version' ]; then echo 'Claude Code 1.0.0 (systemtest shim)'; exit 0; fi\n" +
		"CAPTURE='" + captureDir + "'\n" +
		"n=0\n" +
		"while [ -f \"$CAPTURE/call-$n.args\" ]; do n=$((n+1)); done\n" +
		"printf '%s\\n' \"$@\" > \"$CAPTURE/call-$n.args\"\n" +
		"prev=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = '--mcp-config' ]; then cp \"$a\" \"$CAPTURE/call-$n.mcpconfig.json\"; fi\n" +
		"  if [ \"$prev\" = '--settings' ]; then cp \"$a\" \"$CAPTURE/call-$n.settings.json\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"git config user.email shim@aiarch.local\n" +
		"git config user.name shim\n" +
		"echo \"phase $n\" >> SHIM_PROGRESS.txt\n" +
		"git add -A\n" +
		"git commit -m \"shim commit $n\" >/dev/null\n" +
		"echo '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"ok\"}'\n" +
		"exit 0\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test shim, deliberately executable
		t.Fatalf("write claude shim: %v", err)
	}
	return dir
}

// Test_UC3_LocalExec_DispatchesRealHeadlessClaude_AttachesStateMCP_ActivityCompletes
// is the local-first-init-funnel Task 6 acceptance slice (brief Step 2): begin
// construction on a seeded LOCAL project with NO GitHub creds and DRYRUN=false,
// pump, and assert (a) the fake-shim `claude` was actually invoked with the
// REAL, built-from-source cmd/aiarch-state-mcp attached via --mcp-config, carrying
// the correct AIARCH_* construct-mode envelope, and (b) the activity's phase
// transitions through to "exited" on completion — the local executor genuinely
// drives the SAME per-activity supervision spine the dry-run/cloud arms do.
func Test_UC3_LocalExec_DispatchesRealHeadlessClaude_AttachesStateMCP_ActivityCompletes(t *testing.T) {
	requireStack(t)
	ctx := context.Background()

	stateMCPBin := buildStateMCPBinary(t)

	const activityID = "C-LOCALEXEC"
	projectID := "uc3-localexec-" + harness.ShortID()

	seed := harness.ConstructionProjectJSON(projectID, []harness.SeedActivity{
		{ID: activityID, EffortDays: 5},
	})
	repo := harness.StartLocalGitRepoWithFiles(t, "main", map[string][]byte{
		".aiarch/state/project.json": seed,
	})

	capture := filepath.Join(t.TempDir(), "capture")
	shimDir := installLocalExecClaudeShim(t, capture)

	env := append(harness.GitLocalEnv(repo.URL()),
		"ARCHISTRATOR_CONSTRUCTION_DRYRUN=false",
		"ARCHISTRATOR_STATE_MCP_BIN="+stateMCPBin,
		// wins over the inherited PATH entry (exec.Cmd.Env: last duplicate key wins) —
		// the SERVER subprocess must resolve `claude` to the shim, not a real install.
		"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	srv := startServerWithEnv(t, true, env)
	tr := harness.NewHTTPTransport(srv.BaseURL())
	t.Cleanup(func() { _ = tr.Close() })

	dispatched, dispatchedID, err := tr.ExecuteNextActivity(ctx, projectID, "tick-localexec-1")
	if err != nil {
		t.Fatalf("executeNextActivity: %v", err)
	}
	if !dispatched || dispatchedID != activityID {
		t.Fatalf("expected sync dispatch of %q, got dispatched=%t activityId=%q", activityID, dispatched, dispatchedID)
	}

	// The activity runs its FULL 5-phase service profile (Requirements/Detailed
	// Design/Test Plan/Construction/Integration) via the local executor — real
	// subprocess dispatch + a git WORKTREE of the shared repo per phase (commits
	// advance the repo's refs directly; no clone, no push), unlike the
	// dry-run UC3 tests' instant stub. Per phase, if the FIRST observe poll lands
	// before the subprocess/git work finishes, the spine pays one extra
	// pipelinePollInterval=15s wait (constructactivity.go) before the next poll
	// catches it — a real, observed source of run-to-run variance: two isolated
	// runs during this task measured 78s (no phase paid the extra wait) and 152s
	// (every phase paid it: 5 * ~15s poll waits + subprocess/git overhead), i.e.
	// close to the theoretical worst case. 240s gives real headroom above that
	// worst case without masking a genuine hang.
	harness.TryReachConstructionStage(ctx, t, tr, projectID, activityID, "exited", 240*time.Second)

	// --- Dispatch-shape proof: the fake claude was genuinely invoked with the
	// state-mcp rig attached, exactly as aiarch-construct.yml's claude-code-action
	// step wires it. ---
	assertLocalExecClaudeInvocation(t, capture, stateMCPBin, activityID)

	// --- Vibes auto-merge proof (local-merge-and-policy Commit 1): the seed
	// carries reviewPolicy preset "vibes", so on completion the Manager's local
	// merge step must have landed the shim's commits on MAIN via a real --no-ff
	// merge commit and DELETED the activity branch — the branch no longer
	// exists, and the shim's SHIM_PROGRESS.txt is reachable from main. ---
	branch := "activity/" + activityID
	repoPath := strings.TrimPrefix(repo.URL(), "file://")
	if out, gitErr := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).CombinedOutput(); gitErr == nil {
		t.Fatalf("activity branch %s must be DELETED after the vibes auto-merge, but it still exists\n%s", branch, out)
	}
	progress, catErr := exec.CommandContext(ctx, "git", "-C", repoPath, "cat-file", "-p", "main:SHIM_PROGRESS.txt").CombinedOutput()
	if catErr != nil {
		t.Fatalf("main:SHIM_PROGRESS.txt not reachable after the auto-merge (was the branch merged?): %v\n%s", catErr, progress)
	}
	if !strings.Contains(string(progress), "phase") {
		t.Fatalf("main:SHIM_PROGRESS.txt = %q, want the shim's phase markers", progress)
	}
	mergeSubject, logErr := exec.CommandContext(ctx, "git", "-C", repoPath, "log", "--merges", "-1", "--format=%s", "main").CombinedOutput()
	if logErr != nil {
		t.Fatalf("git log --merges on main: %v\n%s", logErr, mergeSubject)
	}
	if !strings.Contains(string(mergeSubject), "aiarch: merge "+branch) {
		t.Fatalf("main's latest merge commit subject = %q, want %q", strings.TrimSpace(string(mergeSubject)), "aiarch: merge "+branch)
	}

	// --- Worktree-hygiene proof: every phase's throwaway worktree was removed;
	// only the repo's own primary entry remains registered. ---
	wtOut, wtErr := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "list", "--porcelain").CombinedOutput()
	if wtErr != nil {
		t.Fatalf("git worktree list: %v\n%s", wtErr, wtOut)
	}
	if n := strings.Count(string(wtOut), "worktree "); n != 1 {
		t.Fatalf("expected only the primary worktree entry after construction, got %d:\n%s", n, wtOut)
	}
}

// assertLocalExecClaudeInvocation asserts the captured first claude invocation
// (a) carries the fixed dispatch flags, and (b) attaches the REAL, built-from-
// source cmd/aiarch-state-mcp binary via --mcp-config, carrying the correct
// AIARCH_* construct-mode envelope — proving the rig is genuinely attached, not
// merely asserted from the RA's own internal unit tests.
// assertLocalExecSandboxPosture reads the captured Tier-2 --settings envelope and
// proves the REAL local executor (not just the RA's own unit tests) always
// dispatches with an ACTIVE OS sandbox: enabled + fail-closed + no per-command
// unsandboxed escape.
func assertLocalExecSandboxPosture(t *testing.T, captureDir string) {
	t.Helper()
	settingsRaw, err := os.ReadFile(filepath.Join(captureDir, "call-0.settings.json"))
	if err != nil {
		t.Fatalf("read captured --settings file: %v", err)
	}
	var sandboxCfg struct {
		Sandbox struct {
			Enabled                  bool `json:"enabled"`
			FailIfUnavailable        bool `json:"failIfUnavailable"`
			AllowUnsandboxedCommands bool `json:"allowUnsandboxedCommands"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal(settingsRaw, &sandboxCfg); err != nil {
		t.Fatalf("decode captured --settings: %v\n%s", err, settingsRaw)
	}
	if !sandboxCfg.Sandbox.Enabled || !sandboxCfg.Sandbox.FailIfUnavailable || sandboxCfg.Sandbox.AllowUnsandboxedCommands {
		t.Fatalf("unexpected sandbox posture in captured --settings: %+v", sandboxCfg.Sandbox)
	}
}

func assertLocalExecClaudeInvocation(t *testing.T, captureDir, wantStateMCPBin, activityID string) {
	t.Helper()
	argsRaw, err := os.ReadFile(filepath.Join(captureDir, "call-0.args"))
	if err != nil {
		t.Fatalf("read captured claude invocation args (was claude ever invoked?): %v", err)
	}
	args := strings.TrimRight(string(argsRaw), "\n")
	// --settings and --strict-mcp-config are the Fix-subagent Task 6 hardening
	// additions (sandboxed-by-default): --settings is UNCONDITIONALLY paired
	// with --dangerously-skip-permissions per THE INVARIANT
	// (constructionpipelineaccess.go's claudeArgv doc comment); --strict-mcp-
	// config ensures only the ONE attached aiarch-state server loads, never
	// ambient user/project MCP config.
	for _, want := range []string{"--dangerously-skip-permissions", "--settings", "--mcp-config", "--strict-mcp-config", "--output-format\nstream-json", "--verbose", "-p\n/"} {
		if !strings.Contains(args, want) {
			t.Fatalf("captured claude args %q missing %q", args, want)
		}
	}

	assertLocalExecSandboxPosture(t, captureDir)

	mcpRaw, err := os.ReadFile(filepath.Join(captureDir, "call-0.mcpconfig.json"))
	if err != nil {
		t.Fatalf("read captured --mcp-config file: %v", err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpRaw, &cfg); err != nil {
		t.Fatalf("decode captured --mcp-config: %v\n%s", err, mcpRaw)
	}
	stateSrv, ok := cfg.MCPServers["aiarch-state"]
	if !ok {
		t.Fatalf("--mcp-config missing the aiarch-state server: %s", mcpRaw)
	}
	// The command IS the real, built-from-source binary this test compiled —
	// proving the rig attached is the genuine construct-verb server, not a stub.
	if stateSrv.Command != wantStateMCPBin {
		t.Fatalf("--mcp-config aiarch-state command = %q, want the built binary %q", stateSrv.Command, wantStateMCPBin)
	}
	wantEnv := map[string]string{
		"AIARCH_JOB_MODE":      "construct",
		"AIARCH_ACTIVITY_ID":   activityID,
		"AIARCH_TARGET_BRANCH": "activity/" + activityID,
	}
	for k, want := range wantEnv {
		if got := stateSrv.Env[k]; got != want {
			t.Fatalf("mcp config env[%s] = %q, want %q", k, got, want)
		}
	}
	if stateSrv.Env["AIARCH_STATE_ROOT"] == "" {
		t.Fatal("AIARCH_STATE_ROOT is empty")
	}
}
