package main

// init.go implements `archistrator init` — scaffold-only
// (docs/superpowers/plans/2026-07-19-local-first-init-funnel.md, Task 5,
// amended 2026-07-19 "standalone serve, drop Serena pattern"): it produces
// the artifacts a subsequently, MANUALLY started `archistrator serve`
// (serve.go) needs, and starts NOTHING long-running itself — no exec, no
// daemon, no server.
//
// Produces, in the target directory:
//   - a git repo (created if absent, adopted if already present) configured
//     with receive.denyCurrentBranch=updateInstead — the local git-forward
//     projectstate substrate (projectstateaccess.go's NewGitLocal* ctors)
//     pushes design/construction commits directly to whatever branch is
//     checked out, which a bare `git push` into a checked-out branch refuses
//     by default; updateInstead makes that push update the working tree too.
//   - an empty `.aiarch/state/` directory — deliberately WITHOUT a
//     project.json. cmd/aiarch-state-mcp's `validate` subcommand documents
//     (validate.go) that a repo with NO committed .aiarch state has "nothing
//     to validate — a clean pass, never a red gate": the emptiest possible
//     shape that is guaranteed to pass the gate is no file at all, so this
//     scaffold writes none. The first design session (UC1) is what creates
//     project.json — via projectstateaccess.go's CreateProject, which is
//     therefore also where the review-policy sophistication dial's default
//     preset ("vibes") is seeded (Task 7); init deliberately does NOT invent
//     a reviewPolicy value here, for the same "write no file at all" reason.
//     See docs/superpowers/sdd/task-7-report.md.
//   - a .mcp.json registering archistrator as an HTTP MCP server pointed at
//     the standalone daemon's own /mcp mount
//     ({"type":"http","url":"http://127.0.0.1:8877/mcp"}) — unlike the
//     earlier stdio registration, this does NOT auto-start anything: the
//     user runs `archistrator serve` once, THEN opens Claude Code here.
//
// Idempotent: re-running never clobbers an existing git repo, an existing
// .aiarch/state/ tree (in particular an existing project.json), or entries
// already present in .mcp.json besides the archistrator one it owns.
import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	stateDirRel   = ".aiarch/state"
	mcpConfigFile = ".mcp.json"
	// finishedMessage is printed on success — the literal handoff line the
	// amended (2026-07-19) brief specifies: serve is now a manual, standalone
	// step, no longer auto-started by Claude Code.
	finishedMessage = "Run `archistrator serve` in this directory, then open Claude Code here."
)

// mcpServerEntry mirrors the one shape `.mcp.json` needs for an HTTP MCP
// server registration (Claude Code's own config schema carries more optional
// fields; only these two are needed here). Amendment 2026-07-19: this used
// to be a stdio {"command","args"} registration that auto-spawned
// `archistrator mcp`; it is now a plain HTTP client pointed at the
// standalone `archistrator serve` daemon's own /mcp mount — init writes no
// process-spawn instruction at all anymore.
type mcpServerEntry struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// defaultServeMCPURL is the /mcp endpoint `archistrator serve` exposes on its
// default port (defaultServePort, serve.go) — the value init.go writes into
// a fresh .mcp.json. An operator running serve on a non-default --port must
// hand-edit this entry; documenting that is out of scope for v1 (matches the
// existing singleton-guard "no proxy cleverness" scope note).
var defaultServeMCPURL = fmt.Sprintf("http://127.0.0.1:%d/mcp", defaultServePort)

// mcpConfigDoc is the (possibly pre-existing, possibly multi-server) shape of
// `.mcp.json`. mcpServers is decoded/re-encoded via a map of raw JSON so
// OTHER already-registered servers survive untouched byte-for-byte; any
// other top-level key a pre-existing file carries is intentionally NOT
// modeled — json.Unmarshal into this struct silently drops unknown fields on
// re-encode, which is acceptable for v1 (files this tool has reason to touch
// are Claude Code's own single-purpose .mcp.json).
type mcpConfigDoc struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// RunInit scaffolds dir per the package doc above, writing progress lines to
// out, and returns a non-nil error on the first unrecoverable step (each step
// names what it was doing, so the message stays actionable).
func RunInit(dir string, out io.Writer) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve target directory: %w", err)
	}

	adopted, err := ensureGitRepo(absDir)
	if err != nil {
		return fmt.Errorf("git repo: %w", err)
	}
	if adopted {
		fmt.Fprintln(out, "adopted existing git repo:", absDir)
	} else {
		fmt.Fprintln(out, "initialized git repo:", absDir)
	}

	if err := configureReceiveUpdateInstead(absDir); err != nil {
		return fmt.Errorf("git config receive.denyCurrentBranch: %w", err)
	}
	fmt.Fprintln(out, "git config receive.denyCurrentBranch=updateInstead")

	stateCreated, err := ensureStateDir(absDir)
	if err != nil {
		return fmt.Errorf(".aiarch/state: %w", err)
	}
	if stateCreated {
		fmt.Fprintln(out, "scaffolded .aiarch/state/ (empty — no project.json yet)")
	} else {
		fmt.Fprintln(out, ".aiarch/state/ already present — left untouched")
	}

	mcpChanged, err := ensureMCPConfig(absDir)
	if err != nil {
		return fmt.Errorf(".mcp.json: %w", err)
	}
	if mcpChanged {
		fmt.Fprintln(out, "registered archistrator in .mcp.json (http: "+defaultServeMCPURL+")")
	} else {
		fmt.Fprintln(out, ".mcp.json already registers archistrator — left untouched")
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, finishedMessage)
	return nil
}

// ensureGitRepo initializes a git repo at dir if none exists, or reports
// adopted=true for an already-initialized one. Uses `git rev-parse
// --is-inside-work-tree` (not a bare .git stat) so a dir that is itself
// INSIDE an existing repo (a parent's .git) is correctly treated as
// "no repo of its own here yet" and gets its own `git init`.
func ensureGitRepo(dir string) (adopted bool, err error) {
	if isGitRepoRoot(dir) {
		return true, nil
	}
	cmd := exec.Command("git", "init", "--quiet", dir) //nolint:gosec // fixed trusted binary, caller-controlled dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("git init: %w: %s", err, string(out))
	}
	return false, nil
}

// isGitRepoRoot reports whether dir is ITSELF a git repo's top level
// (a .git entry directly under dir — a directory for an ordinary repo, or a
// file for a worktree/submodule, both count as "already a repo here") —
// deliberately narrower than "is inside any git tree" so init in a
// subdirectory of an unrelated repo still gets its own nested repo rather
// than silently reusing the parent's.
func isGitRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// configureReceiveUpdateInstead sets receive.denyCurrentBranch=updateInstead
// on the repo at dir — required so a server-side push (the git-forward
// projectstate substrate) into the checked-out branch updates the working
// tree instead of being refused. Always (re)applied — cheap and idempotent.
func configureReceiveUpdateInstead(dir string) error {
	cmd := exec.Command("git", "-C", dir, "config", "receive.denyCurrentBranch", "updateInstead") //nolint:gosec // fixed trusted binary + args
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

// ensureStateDir creates .aiarch/state/ under dir if absent. Deliberately
// creates ONLY the directory, never a project.json — see the package doc's
// "clean pass" rationale. Reports created=false (and touches nothing) when
// the directory already exists, so an existing project.json inside it is
// never at risk.
func ensureStateDir(dir string) (created bool, err error) {
	full := filepath.Join(dir, stateDirRel)
	if info, err := os.Stat(full); err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("%s exists and is not a directory", full)
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(full, 0o755); err != nil { //nolint:gosec // scaffold directory, not a secret
		return false, err
	}
	return true, nil
}

// ensureMCPConfig writes (or merges into) dir/.mcp.json so it registers
// archistrator as an HTTP MCP server pointed at the standalone `archistrator
// serve` daemon's /mcp mount. Returns changed=false when an "archistrator"
// entry is already present (any pre-existing value is left exactly as the
// user configured it — init never overwrites a customization), so re-running
// init is a true no-op on this file once it is correctly registered.
func ensureMCPConfig(dir string) (changed bool, err error) {
	full := filepath.Join(dir, mcpConfigFile)

	doc := mcpConfigDoc{MCPServers: map[string]json.RawMessage{}}
	raw, readErr := os.ReadFile(full) //nolint:gosec // fixed scaffold filename under caller-controlled dir
	switch {
	case readErr == nil:
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &doc); err != nil {
				return false, fmt.Errorf("existing %s is not valid JSON: %w", mcpConfigFile, err)
			}
		}
		if doc.MCPServers == nil {
			doc.MCPServers = map[string]json.RawMessage{}
		}
	case errors.Is(readErr, os.ErrNotExist):
		// doc stays at its zero-ish default above; write a fresh file below.
	default:
		return false, readErr
	}

	if _, present := doc.MCPServers["archistrator"]; present {
		return false, nil
	}

	entry, err := json.Marshal(mcpServerEntry{Type: "http", URL: defaultServeMCPURL})
	if err != nil {
		return false, err
	}
	doc.MCPServers["archistrator"] = entry

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')
	if err := os.WriteFile(full, out, 0o644); err != nil { //nolint:gosec // project config, not a secret
		return false, err
	}
	return true, nil
}
