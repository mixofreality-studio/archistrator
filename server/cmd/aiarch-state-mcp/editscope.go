package main

// editscope.go — the `edit-scope` ONE-SHOT subcommand, the gate on what a task's agent
// committed:
//
//	aiarch-state-mcp edit-scope --spec FILE --base REV --head REV [--main REF]
//
// It judges TREE DIFFS, never a commit walk (R23): a walk that skips merge commits
// misses a file added inside a merge commit and one merged in from a side branch. The
// changed set is the paths that differ between base and head AND between
// merge-base(main, head) and head. The first diff is everything the branch tip holds
// that base did not; intersecting it with the second drops what head shares with main,
// i.e. content a merge from main brought in. A path added and then removed nets out of
// both diffs. --main defaults to origin/main, then main; with no main ref at all the
// changed set is the plain base..head diff (strict). Renames are judged at both ends.
//
// A path is in scope when it lies under .aiarch/state/ (written through the state
// tools, always permitted) or, unless the spec is stateOnly, matches a component path
// glob or an extra glob in the edit-scope grammar (projectstate.ValidEditScopeGlob).
// Every path outside the scope is listed and the command exits 1; a git or spec
// failure exits 2.

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// runEditScope gates the change from --base to --head; see the file comment.
func runEditScope(args []string) error {
	flags := flag.NewFlagSet("edit-scope", flag.ContinueOnError)
	spec := flags.String("spec", "", "the task spec file task-spec wrote")
	base := flags.String("base", "", "the branch tip before the agent ran")
	head := flags.String("head", "", "the branch tip the agent published")
	mainRef := flags.String("main", "", "the main line merges may bring content from (default: origin/main, else main)")
	if err := flags.Parse(args); err != nil {
		return exitError{2, err}
	}
	for _, f := range []struct{ name, value string }{{"spec", *spec}, {"base", *base}, {"head", *head}} {
		if strings.TrimSpace(f.value) == "" {
			return exitError{2, fmt.Errorf("--%s is required", f.name)}
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return exitError{2, err}
	}
	bad, err := editScopeViolations(wd, *spec, strings.TrimSpace(*base), strings.TrimSpace(*head), strings.TrimSpace(*mainRef))
	if err != nil {
		return exitError{2, err}
	}
	if len(bad) > 0 {
		return exitError{1, fmt.Errorf("%d changed path(s) lie outside the task's edit scope:\n  %s", len(bad), strings.Join(bad, "\n  "))}
	}
	return nil
}

// editScopeViolations returns the sorted paths changed from base to head (net of
// main's merged-in content, see the file comment) outside specPath's edit scope, in
// repo root. mainRef "" tries origin/main, then main.
func editScopeViolations(root, specPath, base, head, mainRef string) ([]string, error) {
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
	changed, err := branchChangedPaths(root, base, head, mainRef)
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

// branchChangedPaths is the sorted set of paths that differ base..head and, when a main
// ref resolves, also merge-base(main, head)..head.
func branchChangedPaths(root, base, head, mainRef string) ([]string, error) {
	changed, err := treeDiffNames(root, base, head)
	if err != nil {
		return nil, err
	}
	ref, ok := resolveMainRef(root, mainRef)
	if !ok {
		return sortedKeys(changed), nil // strict: nothing to net against
	}
	mb, err := runGit(root, "merge-base", ref, head)
	if err != nil {
		return nil, err
	}
	ownOfMain, err := treeDiffNames(root, mb, head)
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

// resolveMainRef is the main line to net merged-in content against: ref when given,
// else origin/main, else main; ok=false when none resolves to a commit (the caller
// then judges the plain base..head diff).
func resolveMainRef(root, ref string) (string, bool) {
	candidates := []string{"origin/main", "main"}
	if ref != "" {
		candidates = []string{ref}
	}
	for _, c := range candidates {
		if _, err := runGit(root, "rev-parse", "--verify", "--quiet", c+"^{commit}"); err == nil {
			return c, true
		}
	}
	return "", false
}

// treeDiffNames is the set of paths whose content differs between the trees of from
// and to. Renames are a delete plus an add, so both ends are listed.
func treeDiffNames(root, from, to string) (map[string]bool, error) {
	out, err := runGit(root, "diff", "--name-only", "--no-renames", "-z", from, to, "--")
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for p := range strings.SplitSeq(out, "\x00") {
		if p = strings.TrimSpace(p); p != "" {
			names[p] = true
		}
	}
	return names, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// scopeGlobMatch reports whether the repo-relative path p matches an edit-scope glob in
// the grammar projectstate.ValidEditScopeGlob admits: "**" is every path, "<dir>/**"
// every path under dir, and a literal path only itself. Any other glob matches nothing.
func scopeGlobMatch(glob, p string) bool {
	if !projectstate.ValidEditScopeGlob(glob) {
		return false
	}
	if glob == "**" {
		return true
	}
	if dir, ok := strings.CutSuffix(glob, "/**"); ok {
		return strings.HasPrefix(p, dir+"/")
	}
	return p == glob
}
