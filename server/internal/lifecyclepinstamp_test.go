package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLifecyclePinStampsDAGIdentity pins the delivery Manager's half of the reopen re-pin:
// the pin it stamps on OpenActivity carries the lifecycle DAG's CONTENT identity, never the
// method-assets module version. The version is build-dependent ((devel) in a workspace
// build, vX.Y.Z otherwise), so stamping it re-pins a requeued activity on every move
// between builds even when its DAG is byte-identical, and a real DAG change inside one
// version goes unseen. The RA's scenarios (activityExecutionAccess, OpenActivity re-pin)
// pin the decision on whatever pin it is handed; this gate pins what the Manager hands it.
//
// It is a source gate, not a unit test, because the stamp lives in the delivery Manager
// package, where TestFileLayout admits only generated scenario tests, and every
// deliveryManager scenario needs a full construction run (arranged state, ruling 8). It
// fails on: a stamp that is not a call of lifecycleIdentity; a lifecycleIdentity that does
// not hash the lifecycle resolved for its type key; and ANY use of methodassets.Version in
// the delivery package.
func TestLifecyclePinStampsDAGIdentity(t *testing.T) {
	funcs := deliveryFuncs(t, filepath.Join("manager", "delivery"))
	stamp, ident := funcs["lifecyclePinFor"], funcs["lifecycleIdentity"]
	if stamp == nil || ident == nil {
		t.Fatalf("delivery must declare lifecyclePinFor and lifecycleIdentity (found stamp=%v identity=%v)", stamp != nil, ident != nil)
	}
	if !stampsIdentity(stamp) {
		t.Error("lifecyclePinFor must set LifecyclePin.AssetsVersion to lifecycleIdentity(...)")
	}
	uses := identityInputs(ident)
	for _, want := range []string{"methodassets.LifecycleFor", "json.Marshal", "sha256.Sum256", "dag-sha256:"} {
		if !uses[want] {
			t.Errorf("lifecycleIdentity must derive the identity from the resolved lifecycle's content; missing %s", want)
		}
	}
}

// deliveryFuncs parses the package's non-test sources, reports every methodassets.Version
// use as an error, and returns the package-level functions by name.
func deliveryFuncs(t *testing.T, dir string) map[string]*ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	funcs := map[string]*ast.FuncDecl{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, pos := range moduleVersionUses(f) {
			t.Errorf("%s: the delivery Manager must not use methodassets.Version; a lifecycle pin is a DAG identity, not a module version", fset.Position(pos))
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
		}
	}
	return funcs
}

// moduleVersionUses is every methodassets.Version selector in f.
func moduleVersionUses(f *ast.File) []token.Pos {
	var out []token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "methodassets" && sel.Sel.Name == "Version" {
				out = append(out, sel.Pos())
			}
		}
		return true
	})
	return out
}

// stampsIdentity reports whether fn sets an AssetsVersion field to a lifecycleIdentity call.
func stampsIdentity(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "AssetsVersion" {
			if call, ok := kv.Value.(*ast.CallExpr); ok {
				if f, ok := call.Fun.(*ast.Ident); ok && f.Name == "lifecycleIdentity" {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// identityInputs is the set of qualified selectors and the dag-sha256 literal fn uses.
func identityInputs(fn *ast.FuncDecl) map[string]bool {
	uses := map[string]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				uses[id.Name+"."+x.Sel.Name] = true
			}
		case *ast.BasicLit:
			if strings.Contains(x.Value, "dag-sha256:") {
				uses["dag-sha256:"] = true
			}
		}
		return true
	})
	return uses
}
