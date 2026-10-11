package main

// editscope.go — the `edit-scope` ONE-SHOT subcommand, the gate on what a task's agent
// committed:
//
//	aiarch-state-mcp edit-scope --spec FILE --base REV --head REV
//
// It judges the paths the branch's OWN commits changed: the first-parent, non-merge
// commits in merge-base(base, head)..head, run in the checkout (cwd). Content a merge
// brought in from main is someone else's edit and is not judged (R13). A path is in
// scope when it lies under .aiarch/state/ (written through the state tools, always
// permitted) or, unless the spec is stateOnly, matches a component path glob or an
// extra glob. Every path outside the scope is listed and the command exits 1; a git
// failure exits 2.

import (
	"flag"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
)

// runEditScope gates the commits between --base and --head; see the file comment.
func runEditScope(args []string) error {
	flags := flag.NewFlagSet("edit-scope", flag.ContinueOnError)
	spec := flags.String("spec", "", "the task spec file task-spec wrote")
	base := flags.String("base", "", "the branch tip before the agent ran")
	head := flags.String("head", "", "the branch tip the agent published")
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
	bad, err := editScopeViolations(wd, *spec, strings.TrimSpace(*base), strings.TrimSpace(*head))
	if err != nil {
		return exitError{2, err}
	}
	if len(bad) > 0 {
		return exitError{1, fmt.Errorf("%d changed path(s) lie outside the task's edit scope:\n  %s", len(bad), strings.Join(bad, "\n  "))}
	}
	return nil
}

// editScopeViolations returns the sorted paths the branch's own commits in
// merge-base(base, head)..head changed outside specPath's edit scope, in repo root.
func editScopeViolations(root, specPath, base, head string) ([]string, error) {
	spec, err := readSpecFile(specPath)
	if err != nil {
		return nil, err
	}
	changed, err := branchChangedPaths(root, base, head)
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

// branchChangedPaths is the sorted, de-duplicated set of paths changed by the
// first-parent, non-merge commits in merge-base(base, head)..head. Renames are listed
// as their delete and add, so both ends are judged.
func branchChangedPaths(root, base, head string) ([]string, error) {
	mb, err := runGit(root, "merge-base", base, head)
	if err != nil {
		return nil, err
	}
	revs, err := runGit(root, "rev-list", "--first-parent", "--no-merges", mb+".."+head)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for rev := range strings.FieldsSeq(revs) {
		out, err := runGit(root, "diff-tree", "--root", "--no-commit-id", "--name-only", "--no-renames", "-r", "-z", rev)
		if err != nil {
			return nil, err
		}
		for p := range strings.SplitSeq(out, "\x00") {
			if p = strings.TrimSpace(p); p != "" {
				seen[p] = true
			}
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths, nil
}

// scopeGlobMatch reports whether the repo-relative path p matches an edit-scope glob:
// "**" is every path, "<dir>/**" every path under dir, and anything else path.Match.
func scopeGlobMatch(glob, p string) bool {
	if glob == "**" {
		return true
	}
	if dir, ok := strings.CutSuffix(glob, "/**"); ok {
		return strings.HasPrefix(p, dir+"/")
	}
	ok, err := path.Match(glob, p)
	return err == nil && ok
}
