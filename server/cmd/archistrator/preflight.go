package main

// preflight.go implements Task-5 Step 4: the `archistrator mcp` boot-time
// checks (git present, claude present, claude authenticated) with actionable,
// install-command-naming messages. The report is never fatal-and-silent: its
// Instructions() text is threaded into the local mcp.Server's
// ServerOptions.Instructions (mcpserve.go) so it rides the very first MCP
// `initialize` response the driver (Claude Code) sees, even when the finding
// is severe enough that the local stack cannot actually start (Fatal()).
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	llm "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-llm"
)

// claudeAuthProbeTimeout bounds the (network-touching, real-subscription)
// `claude -p "ok"` auth probe — generous enough for a cold CLI start, short
// enough that a hung/unreachable probe doesn't wedge server startup.
const claudeAuthProbeTimeout = 25 * time.Second

// preflightReport is the outcome of runPreflight — see its doc for how each
// field is populated and how Instructions() renders them.
type preflightReport struct {
	gitErr    error // nil ⇒ git is on PATH
	claudeErr error // nil ⇒ `claude --version` ran cleanly (llm.PreflightClaudeCLI)

	authChecked bool  // false when --skip-auth-check was passed, or claude itself is missing
	authErr     error // nil ⇒ the auth probe reported success (or was not run)
}

// Fatal reports whether the local stack cannot possibly start: git and claude
// are both HARD requirements (git backs the project-state substrate; claude
// backs the local worker provider cmd/server's own boot-time preflight,
// resolveWorkerProvider/hooks.go, ALSO requires). An auth-probe failure is
// deliberately NOT fatal — the user may still be able to fix it (`claude
// login`) from within the very Claude Code session asking, so mcpserve.go
// degrades to instructions-only rather than refusing to start at all.
func (r preflightReport) Fatal() bool {
	return r.gitErr != nil || r.claudeErr != nil
}

// Instructions renders the report as the MCP server Instructions text —
// empty findings produce a short all-clear line; any finding produces one
// actionable line per issue.
func (r preflightReport) Instructions() string {
	var issues []string
	if r.gitErr != nil {
		issues = append(issues, "git was not found on PATH — install it "+
			"(e.g. `brew install git`, `apt install git`, or https://git-scm.com/downloads) and restart Claude Code.")
	}
	if r.claudeErr != nil {
		issues = append(issues, r.claudeErr.Error())
	}
	if r.authChecked && r.authErr != nil {
		issues = append(issues, "claude does not appear to be authenticated ("+r.authErr.Error()+
			") — run `claude login` (or set ANTHROPIC_API_KEY to use the cloud provider instead) and restart.")
	}
	if len(issues) == 0 {
		return "archistrator: preflight OK (git + claude present" + authSuffix(r) + ")."
	}
	var b strings.Builder
	b.WriteString("archistrator preflight found issue(s) — the local stack may not be fully usable until these are fixed:\n")
	for _, i := range issues {
		b.WriteString("  - " + i + "\n")
	}
	return b.String()
}

func authSuffix(r preflightReport) string {
	switch {
	case !r.authChecked:
		return ", auth check skipped"
	default:
		return ", authenticated"
	}
}

// runPreflight performs every Step-4 check. skipAuthCheck corresponds to the
// CLI's --skip-auth-check flag (or tests that don't want a real `claude -p`
// network call): when set, the auth probe is not run at all (authChecked
// stays false, authErr stays nil — never treated as a failure).
func runPreflight(ctx context.Context, skipAuthCheck bool) preflightReport {
	var r preflightReport

	if _, err := exec.LookPath("git"); err != nil {
		r.gitErr = err
	}

	if err := llm.PreflightClaudeCLI(); err != nil {
		r.claudeErr = err
	}

	if r.claudeErr == nil && !skipAuthCheck {
		r.authChecked = true
		r.authErr = probeClaudeAuth(ctx)
	}

	return r
}

// probeClaudeAuth is the "worst case" 1-token probe the brief names: no
// cheaper way to verify actual AUTHENTICATION (as opposed to mere
// installation, which llm.PreflightClaudeCLI's `claude --version` already
// covers) is available — headless `claude -p` is the smallest real call that
// exercises the subscription OAuth path. Classification mirrors
// framework-go-infrastructure-llm/claudecli.go's classifyClaudeCLIFailureText
// vocabulary (kept local/minimal here: this is a boot-time yes/no probe, not
// a Generate call whose fault needs the full Terminal/Transient taxonomy).
func probeClaudeAuth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, claudeAuthProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", "ok", "--output-format", "json") //nolint:gosec // fixed trusted binary + fixed args
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("claude -p \"ok\" timed out after %s", claudeAuthProbeTimeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("claude -p \"ok\" failed: %s", msg)
	}

	lower := strings.ToLower(string(out))
	for _, needle := range []string{"invalid api key", "please run /login", "please run `claude login`", "not authenticated", "authentication_error"} {
		if strings.Contains(lower, needle) {
			return fmt.Errorf("claude reported an authentication problem: %s", strings.TrimSpace(string(out)))
		}
	}
	return nil
}
