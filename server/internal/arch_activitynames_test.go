package internal_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file extends the Method arch checker (arch_test.go) with the appgen
// migration's own invocation-convention gate: internal/manager/**/*.go hand
// files (custom Activities, workflow bodies, dispatch/gitsession/reviewledger
// helpers) must invoke Temporal Activities by METHOD VALUE
// (workflow.ExecuteActivity(ctx, wf.XActivity, args...)) and must never
// register Activities/Workflows directly — registration flows exclusively
// through the generated RegisterWorker(w, manifest) entrypoint
// (worker.gen.go). String-literal AND const-/var-name activity invocation, and
// direct Register*WithOptions calls, are RESERVED for the generated layer
// (invokers.gen.go / worker.gen.go), which the framework-go-app-generator
// temporalgen emitter owns; a hand file doing either would silently create a
// second, divergent way to call or register an activity that the generated
// surface's TaskQueue/manifest wiring doesn't know about.
//
// The gate is enforced on the go/ast — NOT a line regex — so it cannot be
// evaded by a name passed through a package-level string CONST
// (workflow.ExecuteActivity(ctx, actFoo, ...)) or spread across a multi-line
// call. The single legal shape for the activity argument (the 2nd positional
// arg, after ctx) is a selector expression — a method value like `wf.XActivity`
// or `wf.Acts.Y`. Anything else (a string literal, a bare const/var ident, a
// composite, a call) is a violation: Temporal would resolve it by a name the
// hand file chose rather than the one the generated manifest registered.

// executeActivityViolation is emitted when workflow.ExecuteActivity's activity
// argument is not a method value. selectorArg is the only permitted node.
const executeActivityMsg = "%s:%d: workflow.ExecuteActivity called with a %s activity argument — hand files must invoke activities by method value (workflow.ExecuteActivity(ctx, wf.XActivity, ...) / wf.Acts.Y); a string literal or const/var name is reserved for the generated internal/manager/*/invokers.gen.go: %s"

const registerWithOptionsMsg = "%s:%d: direct call to %s outside the generated worker.gen.go — hand files must register through the generated RegisterWorker(w, manifest) entrypoint: %s"

// findNoStringActivityViolations parses each (path -> source) entry with
// go/parser and walks its AST. It returns one human-readable violation message
// per offending call site. It is pure and I/O-free — table-tested directly
// below without touching the real filesystem — so
// TestNoHandTemporalStringActivityNames only has to wire it to the real
// internal/manager tree.
//
// Exclusions are keyed off the path so they are table-testable too: *.gen.go
// and *_test.go files are skipped entirely (generated / allowed to construct
// fake registrations). There is no remaining hand-file exemption for the
// Register*WithOptions rule: the workermanifest.go bridge that used to declare
// genRegisteredActivity Name+Fn pairs was folded into <pkg>manager.go during
// the framework-go v0.5.x migration, and the platform's own FileLayout +
// registration checks now cover hand registration too — so this app-level
// rule goes strictly tighter (no hand file may call Register*WithOptions at
// all) without going red; verified via `grep -rn
// "RegisterActivityWithOptions\|RegisterWorkflowWithOptions"` finding no hits
// outside *.gen.go before this exemption was pruned.
func findNoStringActivityViolations(files map[string]string) []string {
	var violations []string
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".gen.go") || strings.HasSuffix(base, "_test.go") {
			continue
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, files[path], 0)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: parse error: %v", path, err))
			continue
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if v := checkExecuteActivityCall(sel, call, files[path], fset, path); v != "" {
				violations = append(violations, v)
				return true
			}
			if v := checkRegisterCall(sel, call, files[path], fset, path); v != "" {
				violations = append(violations, v)
				return true
			}
			return true
		})
	}
	return violations
}

// checkExecuteActivityCall examines an ExecuteActivity call expression and
// returns a violation message if the activity argument is not a method value.
func checkExecuteActivityCall(sel *ast.SelectorExpr, call *ast.CallExpr, src string, fset *token.FileSet, path string) string {
	if sel.Sel.Name != "ExecuteActivity" {
		return ""
	}
	// Callee must be the workflow package's ExecuteActivity, not a
	// method named ExecuteActivity on some other receiver.
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "workflow" {
		return ""
	}
	// Args: [ctx, activity, args...]. The activity argument (index
	// 1) MUST be a method value — a selector expression. Anything
	// else is a violation.
	if len(call.Args) < 2 {
		return ""
	}
	arg := call.Args[1]
	if _, ok := arg.(*ast.SelectorExpr); ok {
		return "" // method value — the one legal shape
	}
	pos := fset.Position(arg.Pos())
	return fmt.Sprintf(executeActivityMsg,
		path, pos.Line, describeActivityArg(arg), snippet(src, fset, call))
}

// checkRegisterCall examines a Register*WithOptions call and returns a
// violation message if it's called outside generated files.
func checkRegisterCall(sel *ast.SelectorExpr, call *ast.CallExpr, src string, fset *token.FileSet, path string) string {
	switch sel.Sel.Name {
	case "RegisterActivityWithOptions", "RegisterWorkflowWithOptions":
		pos := fset.Position(sel.Sel.Pos())
		return fmt.Sprintf(registerWithOptionsMsg,
			path, pos.Line, sel.Sel.Name, snippet(src, fset, call))
	}
	return ""
}

// describeActivityArg names the offending node kind for a clear message.
func describeActivityArg(arg ast.Expr) string {
	switch arg.(type) {
	case *ast.BasicLit:
		return "string-literal"
	case *ast.Ident:
		return "const-/var-name"
	default:
		return "non-method-value"
	}
}

// snippet returns the first line of a call expression's source for the message.
func snippet(src string, fset *token.FileSet, call *ast.CallExpr) string {
	start := fset.Position(call.Pos())
	lines := strings.Split(src, "\n")
	if start.Line-1 < 0 || start.Line-1 >= len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[start.Line-1])
}

// TestFindNoStringActivityViolations table-tests the pure checker in memory.
// Both flag directions are proven: the legal method-value shape (single-line,
// multi-line, and nested selector) must pass, and every banned shape (string
// literal, package-level const/var ident, multi-line literal, direct
// Register*WithOptions) must be caught — so the checker cannot vacuously pass.
func TestFindNoStringActivityViolations(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
		want int
	}{
		{
			name: "method-value invocation is legal",
			path: "seed.go",
			src:  "package p\nfunc x() { e := workflow.ExecuteActivity(ctx, wf.FooActivity, arg); _ = e }\n",
			want: 0,
		},
		{
			name: "method-value invocation across multiple lines is legal",
			path: "seed.go",
			src: `package p
func x() {
	workflow.ExecuteActivity(
		ctx,
		wf.RecordPhaseCompletedActivity,
		recordPhaseCompletedArgs{},
	)
}
`,
			want: 0,
		},
		{
			name: "nested selector method value (wf.Acts.X) is legal",
			path: "seed.go",
			src:  "package p\nfunc x() { workflow.ExecuteActivity(ctx, wf.Acts.ReadRange, q) }\n",
			want: 0,
		},
		{
			name: "string-literal activity name is flagged",
			path: "seed.go",
			src:  "package p\nfunc x() { workflow.ExecuteActivity(ctx, \"fooAccess.bar\", arg) }\n",
			want: 1,
		},
		{
			name: "package-level const name is flagged (the billing regression regex missed)",
			path: "seed.go",
			src: `package p
const actReadRevenueRange = "ReadRevenueRangeActivity"
func x() { workflow.ExecuteActivity(ctx, actReadRevenueRange, args{}) }
`,
			want: 1,
		},
		{
			name: "multi-line string-literal call is flagged (the line-regex blind spot)",
			path: "seed.go",
			src: `package p
func x() {
	workflow.ExecuteActivity(
		ctx,
		"fooAccess.bar",
		arg,
	)
}
`,
			want: 1,
		},
		{
			name: "RegisterActivityWithOptions outside generated/manifest file is flagged",
			path: "worker.go",
			src:  "package p\nfunc x() { w.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: \"x\"}) }\n",
			want: 1,
		},
		{
			name: "RegisterWorkflowWithOptions outside generated/manifest file is flagged",
			path: "worker.go",
			src:  "package p\nfunc x() { w.RegisterWorkflowWithOptions(fn, workflow.RegisterOptions{Name: \"x\"}) }\n",
			want: 1,
		},
		{
			name: "generated file is skipped entirely",
			path: "invokers.gen.go",
			src:  "package p\nfunc x() { workflow.ExecuteActivity(ctx, \"fooAccess.bar\", arg) }\n",
			want: 0,
		},
		{
			name: "both violations in one file are both flagged",
			path: "worker.go",
			src: `package p
func x() {
	workflow.ExecuteActivity(ctx, "fooAccess.bar", arg)
	w.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: "x"})
}
`,
			want: 2,
		},
		{
			name: "ExecuteActivity on a non-workflow receiver is ignored",
			path: "seed.go",
			src:  "package p\nfunc x() { other.ExecuteActivity(ctx, \"name\", arg) }\n",
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := findNoStringActivityViolations(map[string]string{c.path: c.src})
			if len(got) != c.want {
				t.Fatalf("findNoStringActivityViolations() = %d violation(s), want %d: %v", len(got), c.want, got)
			}
		})
	}
}

// TestNoHandTemporalStringActivityNames is the real gate: it walks
// internal/manager/**/*.go (excluding *.gen.go and *_test.go, which are
// generated/allowed to construct fake registrations respectively) and fails the
// build the moment a hand file invokes an activity by anything other than a
// method value, or registers one directly. Every hand file is in scope,
// including the folded <pkg>manager.go files that build the genWorkerManifest
// by NAME+method-value pairs (genRegisteredActivity{Name: ..., Fn:
// wf.XActivity}) — that construction does not itself call ExecuteActivity or
// Register*WithOptions, so it passes this checker without needing an
// exemption.
func TestNoHandTemporalStringActivityNames(t *testing.T) {
	root := filepath.Join("manager")
	files := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, ".gen.go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Fatalf("no hand Go files found under %s — checker is not scanning anything", root)
	}

	violations := findNoStringActivityViolations(files)
	if len(violations) > 0 {
		t.Fatalf(
			"internal/manager hand files must invoke activities by method value and register only through the generated RegisterWorker entrypoint; found %d violation(s):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
