package main

// editscope.go — the `edit-scope` ONE-SHOT subcommand, the gate on what a task's agent
// committed:
//
//	aiarch-state-mcp edit-scope --spec FILE --base SHA --head REV (--main SHA | --no-main)
//
// It judges TREE DIFFS, never a commit walk (R23): a walk that skips merge commits
// misses a file added inside a merge commit and one merged in from a side branch. The
// changed set is the paths that differ between base and head AND between
// merge-base(main, head) and head. The first diff is everything the branch tip holds
// that base did not; intersecting it with the second drops what head shares with main,
// i.e. content a merge from main brought in. A path added and then removed nets out of
// both diffs. With --no-main the changed set is the plain base..head diff (strict).
//
// POSTURE (ruling R27): this gate is a DRIFT GUARDRAIL against an agent wandering off its
// task's scope, not a security boundary against a hostile agent. It runs on the runner
// (or machine) the agent just had, beside a spec file the agent could have reached, so
// an agent set on escaping it can. The hardening below closes the accidental and the
// cheap ways round it; real enforcement would be a separate gate job on a fresh runner
// that never shared a filesystem with the agent (earmarked, not built).
//
// THE AGENT OWNS THE CHECKOUT, so nothing the gate reads may come from it (R26):
//   - --base and --main are full 40-hex commit SHAs the venue captured BEFORE the agent
//     ran, never ref names an agent could move (`git branch -f main HEAD`). --head is
//     resolved once, to a SHA, and only the SHA is used after.
//   - every git call runs isolated (isolatedGit): no replace objects (`git replace`
//     would swap a dirty commit for a clean one), no grafts, no system or global
//     config, no inherited GIT_* environment, no external diff or textconv, every
//     submodule change shown, renames as a delete plus an add.
//   - -z output is parsed raw: a path is exactly its bytes, so " src/x", "go.mod " and
//     a name holding a newline are judged as the paths they are.
//
// A path is in scope when it lies under .aiarch/state/ (written through the state
// tools, always permitted) or, unless the spec is stateOnly, matches a component path
// glob or an extra glob in the edit-scope grammar (projectstate.ValidEditScopeGlob).
// Every path outside the scope is listed and the command exits 1; a bad flag, a git
// failure or a spec error exits 2.

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// fullSHA is a full commit id: the only form --base and --main accept.
var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// editScopeRange is what the gate diffs, every end a commit SHA. main "" is --no-main.
type editScopeRange struct {
	base, head, main string
}

// runEditScope gates the change from --base to --head; see the file comment.
func runEditScope(args []string) error {
	flags := flag.NewFlagSet("edit-scope", flag.ContinueOnError)
	spec := flags.String("spec", "", "the task spec file task-spec wrote")
	base := flags.String("base", "", "the full SHA of the branch tip before the agent ran")
	head := flags.String("head", "", "the branch tip the agent published (resolved once to a SHA)")
	mainSHA := flags.String("main", "", "the full SHA of main captured before the agent ran: content merged in from it is not the agent's")
	noMain := flags.Bool("no-main", false, "judge the plain base..head diff (no main to net merged-in content against)")
	if err := flags.Parse(args); err != nil {
		return exitError{2, err}
	}
	if strings.TrimSpace(*spec) == "" {
		return exitError{2, errors.New("--spec is required")}
	}
	wd, err := os.Getwd()
	if err != nil {
		return exitError{2, err}
	}
	rng, err := resolveEditScopeRange(wd, *base, *head, *mainSHA, *noMain)
	if err != nil {
		return exitError{2, err}
	}
	bad, err := editScopeViolations(wd, *spec, rng)
	if err != nil {
		return exitError{2, err}
	}
	if len(bad) > 0 {
		shown := make([]string, len(bad))
		for i, p := range bad {
			shown[i] = displayPath(p)
		}
		return exitError{1, fmt.Errorf("%d changed path(s) lie outside the task's edit scope:\n  %s", len(bad), strings.Join(shown, "\n  "))}
	}
	return nil
}

// resolveEditScopeRange holds the flags to the strict forms: base and main full SHAs
// naming commits, exactly one of --main and --no-main, and head resolved to its SHA.
func resolveEditScopeRange(root, base, head, mainSHA string, noMain bool) (editScopeRange, error) {
	switch {
	case !fullSHA.MatchString(base):
		return editScopeRange{}, fmt.Errorf("--base must be a full 40-hex commit SHA captured before the agent ran, got %q", base)
	case noMain && mainSHA != "":
		return editScopeRange{}, errors.New("--main and --no-main are mutually exclusive")
	case !noMain && !fullSHA.MatchString(mainSHA):
		return editScopeRange{}, fmt.Errorf("--main must be a full 40-hex commit SHA captured before the agent ran (or pass --no-main), got %q", mainSHA)
	case head == "" || strings.HasPrefix(head, "-"):
		return editScopeRange{}, fmt.Errorf("--head must name a commit, got %q", head)
	}
	rng := editScopeRange{base: base, main: mainSHA}
	for _, c := range []string{base, mainSHA} {
		if c == "" {
			continue
		}
		if _, err := commitSHA(root, c); err != nil {
			return editScopeRange{}, err
		}
	}
	h, err := commitSHA(root, head)
	if err != nil {
		return editScopeRange{}, err
	}
	rng.head = h
	return rng, nil
}

// commitSHA resolves rev to the full SHA of the commit it names, isolated.
func commitSHA(root, rev string) (string, error) {
	out, err := isolatedGit(root, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%q does not name a commit: %w", rev, err)
	}
	sha := strings.TrimSuffix(out, "\n")
	if !fullSHA.MatchString(sha) {
		return "", fmt.Errorf("%q resolved to %q, not a commit SHA", rev, sha)
	}
	return sha, nil
}

// editScopeViolations returns the sorted paths changed over rng (net of main's
// merged-in content, see the file comment) outside specPath's edit scope, in repo root.
func editScopeViolations(root, specPath string, rng editScopeRange) ([]string, error) {
	spec, err := readSpecFile(specPath)
	if err != nil {
		return nil, err
	}
	scope := spec.EditScope
	var globs []string
	if !scope.StateOnly {
		for _, c := range scope.Components {
			globs = append(globs, c.Paths...)
		}
		globs = append(globs, scope.Extra...)
	}
	for _, g := range globs {
		if !projectstate.ValidEditScopeGlob(g) {
			return nil, fmt.Errorf("the spec's edit-scope glob %q is not \"**\", \"<dir>/**\" or a literal path", g)
		}
	}
	changed, err := branchChangedPaths(root, rng)
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, p := range changed {
		if strings.HasPrefix(p, statePathPrefix+"/") {
			continue
		}
		if !slices.ContainsFunc(globs, func(g string) bool { return scopeGlobMatch(g, p) }) {
			bad = append(bad, p)
		}
	}
	return bad, nil
}

// branchChangedPaths is the sorted set of paths that differ base..head and, unless
// main is "", also merge-base(main, head)..head.
func branchChangedPaths(root string, rng editScopeRange) ([]string, error) {
	changed, err := treeDiffNames(root, rng.base, rng.head)
	if err != nil {
		return nil, err
	}
	if rng.main == "" {
		return sortedKeys(changed), nil
	}
	mbOut, err := isolatedGit(root, "merge-base", "--end-of-options", rng.main, rng.head)
	if err != nil {
		return nil, err
	}
	mb := strings.TrimSuffix(mbOut, "\n")
	if !fullSHA.MatchString(mb) {
		return nil, fmt.Errorf("merge-base of main and head is %q, not one commit SHA", mb)
	}
	ownOfMain, err := treeDiffNames(root, mb, rng.head)
	if err != nil {
		return nil, err
	}
	for p := range changed {
		if !ownOfMain[p] {
			delete(changed, p)
		}
	}
	return sortedKeys(changed), nil
}

// treeDiffNames is the set of paths whose entry differs between the trees of the
// commits from and to, parsed raw from -z output: each NUL-terminated field is a path,
// byte for byte, and only the trailing empty field is dropped.
func treeDiffNames(root, from, to string) (map[string]bool, error) {
	out, err := isolatedGit(root, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", "--no-textconv",
		"--no-relative", "--ignore-submodules=none", "--end-of-options", from, to, "--")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	if fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	names := make(map[string]bool, len(fields))
	for _, p := range fields {
		names[p] = true
	}
	return names, nil
}

// isolatedGit runs git in root with nothing the agent could have planted steering it:
// the environment is the process's minus every GIT_* variable, plus no replace
// objects, no grafts and no system/global config or attributes; the repo-local config
// that matters to a diff is overridden on the command line. It returns stdout RAW
// (never trimmed; stderr is kept apart so a warning cannot become a path).
func isolatedGit(root string, args ...string) (string, error) {
	full := append([]string{"-c", "core.quotepath=off", "-c", "diff.relative=false", "-c", "diff.ignoreSubmodules=none",
		"-c", "core.fsmonitor=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = root
	cmd.Env = isolatedGitEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// isolatedGitEnv is environ without any GIT_* variable, plus the isolation switches.
func isolatedGitEnv(environ []string) []string {
	env := make([]string, 0, len(environ)+5)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_GRAFT_FILE=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
	)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// displayPath is p as the violation list shows it: quoted when it holds a space or a
// non-printing character, so " src/x" and "go.mod\n" are visible for what they are.
func displayPath(p string) string {
	if strings.IndexFunc(p, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) }) >= 0 {
		return strconv.Quote(p)
	}
	return p
}

// scopeGlobMatch reports whether the repo-relative path p matches an edit-scope glob in
// the grammar projectstate.ValidEditScopeGlob admits: "**" is every path, "<dir>/**"
// every path under dir, "<dir>/**/*.gen.*" every generated file under dir, and a literal path
// only itself. Any other glob matches nothing.
func scopeGlobMatch(glob, p string) bool {
	return projectstate.EditScopeGlobMatch(glob, p)
}
