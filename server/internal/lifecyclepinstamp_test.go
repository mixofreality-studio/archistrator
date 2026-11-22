package internal_test

import (
	"fmt"
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
// fails on: a stamp that is not a call of lifecycleIdentity; a lifecycleIdentity whose
// json.Marshal argument is not the lifecycle bound by methodassets.LifecycleFor, whose
// Sum256 argument is not that marshalled value, or whose result does not use the sum; any
// projectstate.LifecyclePin composite literal in delivery outside lifecyclePinFor; any
// ActivityExecutionOpenActivity call whose pin is not lifecyclePinFor(...); and ANY use of
// methodassets.Version in the delivery package.
func TestLifecyclePinStampsDAGIdentity(t *testing.T) {
	funcs := deliveryFuncs(t, filepath.Join("manager", "delivery"))
	stamp, ident := funcs["lifecyclePinFor"], funcs["lifecycleIdentity"]
	if stamp == nil || ident == nil {
		t.Fatalf("delivery must declare lifecyclePinFor and lifecycleIdentity (found stamp=%v identity=%v)", stamp != nil, ident != nil)
	}
	for _, problem := range stampProblems(stamp) {
		t.Error(problem)
	}
	for _, problem := range identityProblems(ident) {
		t.Error(problem)
	}
	for _, problem := range pinSiteProblems(t, filepath.Join("manager", "delivery")) {
		t.Error(problem)
	}
}

// identityProblems checks lifecycleIdentity's data flow: lc from methodassets.LifecycleFor
// -> json.Marshal(lc) bound to raw -> sha256.Sum256(raw) bound to sum -> returned value
// built from sum.
func identityProblems(fn *ast.FuncDecl) []string {
	param := singleParam(fn)
	lcVar := ""
	if param != "" {
		lcVar = boundByCall(fn, "methodassets.LifecycleFor", param)
	}
	rawVar := boundByCall(fn, "json.Marshal", lcVar)
	sumVar := boundByCall(fn, "sha256.Sum256", rawVar)
	switch {
	case param == "":
		return []string{"lifecycleIdentity must take exactly one named parameter, the lifecycle type key"}
	case lcVar == "":
		return []string{"lifecycleIdentity must bind the lifecycle from methodassets.LifecycleFor(<its own parameter>): any other key identifies another type's DAG"}
	case rawVar == "":
		return []string{"lifecycleIdentity must json.Marshal the whole lifecycle bound by methodassets.LifecycleFor, not a part of it"}
	case sumVar == "":
		return []string{"lifecycleIdentity must sha256.Sum256 the marshalled lifecycle"}
	case !returnsDigestOf(fn, sumVar):
		return []string{`lifecycleIdentity must return "dag-sha256:" plus an encoding of the sum`}
	}
	return nil
}

// singleParam is the name of fn's only parameter, or "" when it has not exactly one.
func singleParam(fn *ast.FuncDecl) string {
	ps := fn.Type.Params.List
	if len(ps) != 1 || len(ps[0].Names) != 1 {
		return ""
	}
	return ps[0].Names[0].Name
}

// boundByCall is the first variable assigned the result of calling callee whose single
// argument is the identifier arg; an empty arg (the previous step failed) yields "".
func boundByCall(fn *ast.FuncDecl, callee, arg string) string {
	if arg == "" {
		return ""
	}
	out := ""
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || out != "" || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		lhs, _ := as.Lhs[0].(*ast.Ident)
		if !ok || lhs == nil || selName(call.Fun) != callee {
			return true
		}
		if len(call.Args) == 1 && isIdent(call.Args[0], arg) {
			out = lhs.Name
		}
		return true
	})
	return out
}

// returnsDigestOf reports whether fn returns an expression holding both the dag-sha256:
// prefix and the sum variable.
func returnsDigestOf(fn *ast.FuncDecl, sum string) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) == 1 {
			lit, uses := false, false
			ast.Inspect(ret.Results[0], func(m ast.Node) bool {
				if bl, ok := m.(*ast.BasicLit); ok && strings.Contains(bl.Value, "dag-sha256:") {
					lit = true
				}
				if id, ok := m.(*ast.Ident); ok && id.Name == sum {
					uses = true
				}
				return true
			})
			found = found || (lit && uses)
		}
		return true
	})
	return found
}

// pinSiteProblems scans every non-test delivery source for LifecyclePin literals outside
// lifecyclePinFor and for OpenActivity calls whose pin is not lifecyclePinFor(...).
func pinSiteProblems(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var problems []string
	openCalls := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, d := range f.Decls {
			ps, n := declPinProblems(fset, d)
			problems = append(problems, ps...)
			openCalls += n
		}
	}
	if openCalls == 0 {
		problems = append(problems, "no ActivityExecutionOpenActivity call found in delivery; the gate lost its target")
	}
	return problems
}

// declPinProblems is one declaration's literal and OpenActivity-call findings, and how many
// OpenActivity calls it holds.
func declPinProblems(fset *token.FileSet, d ast.Decl) ([]string, int) {
	fd, isFn := d.(*ast.FuncDecl)
	inStamp := isFn && fd.Recv == nil && fd.Name.Name == "lifecyclePinFor"
	var problems []string
	calls := 0
	ast.Inspect(d, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CompositeLit); ok && selName(lit.Type) == "projectstate.LifecyclePin" && !inStamp {
			problems = append(problems, fmt.Sprintf("%s: a LifecyclePin literal outside lifecyclePinFor bypasses the DAG-identity stamp", fset.Position(lit.Pos())))
		}
		if call, ok := n.(*ast.CallExpr); ok && isOpenActivity(call) {
			calls++
			if !passesStamp(call) {
				problems = append(problems, fmt.Sprintf("%s: ActivityExecutionOpenActivity must be passed lifecyclePinFor(...)", fset.Position(call.Pos())))
			}
		}
		return true
	})
	return problems, calls
}

func isOpenActivity(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "ActivityExecutionOpenActivity"
}

func passesStamp(call *ast.CallExpr) bool {
	for _, a := range call.Args {
		if c, ok := a.(*ast.CallExpr); ok && isIdent(c.Fun, "lifecyclePinFor") {
			return true
		}
	}
	return false
}

// selName renders pkg.Name for a selector expression, else "".
func selName(e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			return id.Name + "." + sel.Sel.Name
		}
	}
	return ""
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
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

// stampProblems checks lifecyclePinFor's data flow: key := projectstate.LifecycleKeyFor(
// act.Type, act.Variant) (act being the function's parameter), and the returned LifecyclePin
// literal sets TypeKey: key and AssetsVersion: lifecycleIdentity(key), the one key for both,
// so the identity is of the very lifecycle the pin names.
func stampProblems(fn *ast.FuncDecl) []string {
	keyVar := boundLifecycleKey(fn)
	if keyVar == "" {
		return []string{"lifecyclePinFor must bind key := projectstate.LifecycleKeyFor(<act>.Type, <act>.Variant) from its own parameter"}
	}
	typeKeyOK, identityOK := false, false
	ast.Inspect(fn, func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			t, i := pinField(kv, keyVar)
			typeKeyOK, identityOK = typeKeyOK || t, identityOK || i
		}
		return true
	})
	var out []string
	if !typeKeyOK {
		out = append(out, "lifecyclePinFor must set LifecyclePin.TypeKey to the key bound from LifecycleKeyFor")
	}
	if !identityOK {
		out = append(out, "lifecyclePinFor must set LifecyclePin.AssetsVersion to lifecycleIdentity(<that same key>)")
	}
	return out
}

// boundLifecycleKey is the variable fn binds from projectstate.LifecycleKeyFor(act.Type,
// act.Variant), act being fn's own parameter, or "".
func boundLifecycleKey(fn *ast.FuncDecl) string {
	act := singleParam(fn)
	out := ""
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || out != "" || len(as.Rhs) != 1 || len(as.Lhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		lhs, _ := as.Lhs[0].(*ast.Ident)
		if ok && lhs != nil && act != "" && selName(call.Fun) == "projectstate.LifecycleKeyFor" && len(call.Args) == 2 &&
			isSel(call.Args[0], act, "Type") && isSel(call.Args[1], act, "Variant") {
			out = lhs.Name
		}
		return true
	})
	return out
}

// pinField reports whether kv is TypeKey: key, and whether it is AssetsVersion:
// lifecycleIdentity(key).
func pinField(kv *ast.KeyValueExpr, key string) (typeKey, identity bool) {
	k, _ := kv.Key.(*ast.Ident)
	if k == nil {
		return false, false
	}
	switch k.Name {
	case "TypeKey":
		return isIdent(kv.Value, key), false
	case "AssetsVersion":
		call, ok := kv.Value.(*ast.CallExpr)
		return false, ok && isIdent(call.Fun, "lifecycleIdentity") && len(call.Args) == 1 && isIdent(call.Args[0], key)
	}
	return false, false
}

// isSel reports whether e is the selector base.name.
func isSel(e ast.Expr, base, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == name && isIdent(sel.X, base)
}
