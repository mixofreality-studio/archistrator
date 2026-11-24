package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// BLK-APP-LINT-CACHE-STALE-WORKTREE: golangci-lint's default cache is shared across jobs and
// stores issues by absolute path, so a later job linting identical content replayed issues
// into an earlier job's DELETED checkout and lost their //nolint. Every job now gets its own
// cache (lintCacheDir(parentDir)), set as GOLANGCI_LINT_CACHE in the gate subprocess env and
// the agent's env, and written into the sandbox allowlist.
//
// THIS IS A SOURCE PRESENCE GATE, NOT A BEHAVIORAL PIN. The agenticJobAccess package admits
// only generated scenario tests (TestFileLayout) and every scenario bound to it is skipped
// under ruling 8, so the behavioral pin (same content linted from a second checkout, first
// checkout deleted, //nolint still honoured) waits for the ruling-8 scenario rebinding. This
// gate fails only when a seam is removed outright; it does not check that lintCacheDir returns
// a per-job path or that golangci-lint honours the variable.
func TestJobLintCacheSeams(t *testing.T) {
	agentic := filepath.Join("resourceaccess", "agenticjob")

	gate := findFunc(t, agentic, "gateEnv")
	if !appendsLintCacheAfter(gate, "allowlistedEnv") {
		t.Error(`agenticjob.gateEnv must append "GOLANGCI_LINT_CACHE="+lintCacheDir(parentDir) after allowlistedEnv(), so the host's shared cache is never the one read`)
	}

	dispatch := findMethod(t, agentic, "localExecAccess", "dispatch")
	if !assignsEnvWithLintCache(dispatch) {
		t.Error(`agenticjob dispatch must put "GOLANGCI_LINT_CACHE="+lintCacheDir(parentDir) into cmd.Env`)
	}
	if !allowWritePathsHasLintCache(dispatch) {
		t.Error("agenticjob dispatch must list lintCacheDir(parentDir) in claudeSettings.allowWritePaths, or the sandboxed agent cannot write its cache")
	}
}

// hasLintCacheEntry: n holds both the "GOLANGCI_LINT_CACHE=" literal and a lintCacheDir call.
func hasLintCacheEntry(n ast.Node) bool {
	lit, call := false, false
	ast.Inspect(n, func(x ast.Node) bool {
		if l, ok := x.(*ast.BasicLit); ok {
			if s, err := strconv.Unquote(l.Value); err == nil && s == "GOLANGCI_LINT_CACHE=" {
				lit = true
			}
		}
		if c, ok := x.(*ast.CallExpr); ok && calleeName(c) == "lintCacheDir" {
			call = true
		}
		return true
	})
	return lit && call
}

// appendsLintCacheAfter: an append whose first argument is a call of base and whose other
// arguments carry the lint cache entry.
func appendsLintCacheAfter(fn *ast.FuncDecl, base string) bool {
	hit := false
	ast.Inspect(fn, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || calleeName(c) != "append" || len(c.Args) < 2 {
			return true
		}
		first, ok := c.Args[0].(*ast.CallExpr)
		if ok && calleeName(first) == base {
			for _, a := range c.Args[1:] {
				if hasLintCacheEntry(a) {
					hit = true
				}
			}
		}
		return true
	})
	return hit
}

// assignsEnvWithLintCache: an assignment to a selector named Env whose right side carries the entry.
func assignsEnvWithLintCache(fn *ast.FuncDecl) bool {
	hit := false
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			if s, ok := lhs.(*ast.SelectorExpr); ok && s.Sel.Name == "Env" && i < len(as.Rhs) && hasLintCacheEntry(as.Rhs[i]) {
				hit = true
			}
		}
		return true
	})
	return hit
}

// allowWritePathsHasLintCache: a composite-literal field allowWritePaths whose value calls lintCacheDir.
func allowWritePathsHasLintCache(fn *ast.FuncDecl) bool {
	hit := false
	ast.Inspect(fn, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "allowWritePaths" {
			ast.Inspect(kv.Value, func(x ast.Node) bool {
				if c, ok := x.(*ast.CallExpr); ok && calleeName(c) == "lintCacheDir" {
					hit = true
				}
				return true
			})
		}
		return true
	})
	return hit
}

// findMethod is findFunc for a method of the named receiver type (dispatch exists on two).
func findMethod(t *testing.T, dir, recv, name string) *ast.FuncDecl {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name != name || fd.Recv == nil || len(fd.Recv.List) != 1 {
				continue
			}
			rt := fd.Recv.List[0].Type
			if st, ok := rt.(*ast.StarExpr); ok {
				rt = st.X
			}
			if id, ok := rt.(*ast.Ident); ok && id.Name == recv {
				return fd
			}
		}
	}
	t.Fatalf("%s must declare (%s).%s", dir, recv, name)
	return nil
}
