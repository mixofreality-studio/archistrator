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

// arch_bannedphase_test.go gates the 2026-09-09 naming ruling: four levels in
// the construction domain were all called "phase" and three of them collided.
//
//	Project lifecycle (1/2/3)   -> Project Phase   -> projectstate.Phase   (blessed)
//	The App-A five              -> Lifecycle Phase -> ActivityMethodPhase  (blessed)
//	The Figure A-1 twelve       -> Task            -> MethodTask
//	One execution of a task     -> Attempt         -> TaskAttempt
//
// The ruling bans the bare word "phase"/"Phase" as a NEW identifier and keeps
// the two blessed names. Every one of the ruling's own examples
// (ProjectPhase/LifecyclePhase/MethodTask/TaskAttempt) is a TYPE NAME, so this
// gate checks that same grain: a hand-written TOP-LEVEL type, const, or var
// declared with the bare name "phase" or "Phase" (case matters — Go
// identifiers are case-sensitive) — never a struct field, function parameter,
// or local variable that merely holds a value of the (already unambiguous)
// blessed type. That narrower grain is a deliberate scope decision, not an
// oversight: `phase ActivityMethodPhase` (constructactivity.go) and
// `Phase ActivityMethodPhase` / `Phase ActivityBuildStatus` struct fields
// (projectstateaccess.go, constructactivity.go) are the ordinary, idiomatic
// way Go code names a value of an already-unambiguous type, and are pervasive
// in pre-existing, correct, currently-shipping code — dozens of them, verified
// by hand across constructactivity.go/constructionmanager.go/
// projectstateaccess.go. Banning that grain too would demand an unplanned,
// out-of-scope rename sweep with real risk (some are wire-tag-adjacent field
// names) for no naming-collision benefit: none of them names a third,
// ambiguous concept — they are handles for one of the two already-blessed
// types. See task-13-report.md for the full inventory this decision rests on.
//
// Same posture as arch_activitynames_test.go: a pure, in-memory checker
// (table-tested both directions below) wired to a real-tree walk over the
// packages that own (projectstate) or dispatch (manager/construction) the
// Task/Attempt/Phase vocabulary. *.gen.go and *_test.go are excluded, exactly
// as every other hand-file gate in this package excludes them.
const bannedPhaseMsg = "%s: top-level %s %q reintroduces the banned bare 'phase'/'Phase' identifier — this level is MethodTask or TaskAttempt now (see arch_bannedphase_test.go)"

// bannedPhaseScopeDirs are the ModulePrefix-relative (internal/-relative)
// directories this gate walks for real enforcement.
var bannedPhaseScopeDirs = []string{
	"resourceaccess/projectstate",
	"manager/construction",
}

// bannedPhaseExempt reports whether a top-level declaration named exactly
// "phase" or "Phase" in package pkg is the one pre-existing, blessed type the
// ruling keeps: projectstate.Phase (the project 1/2/3 lifecycle). It is the
// ONLY entry this needs — ActivityMethodPhase (the App-A five) is a different
// identifier by construction and can never match the bare-name condition
// below, so it needs no allowlist entry to pass; TestFindBannedPhaseIdentifierViolations
// still proves it accepted, to pin that fact rather than leave it implicit.
func bannedPhaseExempt(pkg, name string) bool {
	return pkg == "projectstate" && name == "Phase"
}

// findBannedPhaseIdentifierViolations parses each (path -> source) entry and
// flags every top-level type/const/var declared with the bare name "phase" or
// "Phase", unless bannedPhaseExempt allows it. pkg is read off each file's own
// `package X` clause.
func findBannedPhaseIdentifierViolations(files map[string]string) []string {
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
		f, err := parser.ParseFile(fset, path, files[path], parser.ParseComments)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: parse error: %v", path, err))
			continue
		}
		violations = append(violations, bannedPhaseViolationsInFile(path, f)...)
	}
	return violations
}

// bannedPhaseViolationsInFile walks one parsed file's top-level declarations,
// delegating each GenDecl (type/const/var) to bannedPhaseViolationsInDecl.
func bannedPhaseViolationsInFile(path string, f *ast.File) []string {
	var violations []string
	pkg := f.Name.Name
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		violations = append(violations, bannedPhaseViolationsInDecl(path, pkg, gd)...)
	}
	return violations
}

// bannedPhaseViolationsInDecl inspects one top-level type/const/var GenDecl
// for a declared name matching the bare "phase"/"Phase" identifier.
func bannedPhaseViolationsInDecl(path, pkg string, gd *ast.GenDecl) []string {
	var violations []string
	switch gd.Tok {
	case token.TYPE:
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			violations = appendBannedPhaseViolation(violations, path, pkg, "type", ts.Name)
		}
	case token.CONST, token.VAR:
		kind := "var"
		if gd.Tok == token.CONST {
			kind = "const"
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, id := range vs.Names {
				violations = appendBannedPhaseViolation(violations, path, pkg, kind, id)
			}
		}
	}
	return violations
}

func appendBannedPhaseViolation(violations []string, path, pkg, kind string, id *ast.Ident) []string {
	if id.Name != "phase" && id.Name != "Phase" {
		return violations
	}
	if bannedPhaseExempt(pkg, id.Name) {
		return violations
	}
	return append(violations, fmt.Sprintf(bannedPhaseMsg, path, kind, id.Name))
}

func TestFindBannedPhaseIdentifierViolations(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
		want int
	}{
		{
			name: "a new bare lowercase phase type is flagged",
			path: "seed.go",
			src:  "package construction\ntype phase string\n",
			want: 1,
		},
		{
			name: "a new bare uppercase Phase type is flagged",
			path: "seed.go",
			src:  "package construction\ntype Phase int\n",
			want: 1,
		},
		{
			name: "a new bare Phase const is flagged",
			path: "seed.go",
			src:  "package construction\nconst Phase = \"x\"\n",
			want: 1,
		},
		{
			name: "a new bare phase package-level var is flagged",
			path: "seed.go",
			src:  "package construction\nvar phase string\n",
			want: 1,
		},
		{
			name: "the blessed projectstate.Phase type is accepted",
			path: "seed.go",
			src:  "package projectstate\ntype Phase int\n",
			want: 0,
		},
		{
			name: "a bare Phase type OUTSIDE projectstate is still flagged — the exemption is package-scoped",
			path: "seed.go",
			src:  "package construction\ntype Phase int\n",
			want: 1,
		},
		{
			name: "the blessed ActivityMethodPhase type is accepted (a different identifier; never matches the bare-name check)",
			path: "seed.go",
			src:  "package projectstate\ntype ActivityMethodPhase string\n",
			want: 0,
		},
		{
			name: "multi-word identifiers carrying Phase as a suffix are not this gate's grain",
			path: "seed.go",
			src:  "package construction\ntype pipelinePhase int\ntype PhaseArtifactPayload struct{}\nconst MethodPhaseConstruction = \"construction\"\n",
			want: 0,
		},
		{
			name: "a struct field or function parameter named phase is NOT this gate's grain (see header)",
			path: "seed.go",
			src:  "package construction\ntype Foo struct{ Phase string }\nfunc Bar(phase string) {}\n",
			want: 0,
		},
		{
			name: "a local short variable declaration named phase is NOT this gate's grain",
			path: "seed.go",
			src:  "package construction\nfunc Bar() { phase := \"x\"; _ = phase }\n",
			want: 0,
		},
		{
			name: "generated files are skipped entirely",
			path: "contract.gen.go",
			src:  "package construction\ntype phase string\n",
			want: 0,
		},
		{
			name: "test files are skipped entirely",
			path: "foo_test.go",
			src:  "package construction\ntype phase string\n",
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := findBannedPhaseIdentifierViolations(map[string]string{c.path: c.src})
			if len(got) != c.want {
				t.Fatalf("findBannedPhaseIdentifierViolations() = %d violation(s), want %d: %v", len(got), c.want, got)
			}
		})
	}
}

// TestNoBannedPhaseIdentifier is the real gate: it walks every hand file under
// bannedPhaseScopeDirs (excluding *.gen.go/_test.go) and fails the moment one
// declares a new top-level type/const/var named exactly "phase" or "Phase".
func TestNoBannedPhaseIdentifier(t *testing.T) {
	files := map[string]string{}
	for _, dir := range bannedPhaseScopeDirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
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
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	if len(files) == 0 {
		t.Fatal("no hand Go files found under the banned-phase scope; this gate would pass vacuously")
	}

	violations := findBannedPhaseIdentifierViolations(files)
	if len(violations) > 0 {
		t.Fatalf(
			"the bare identifier 'phase'/'Phase' is banned outside projectstate.Phase (2026-09-09 naming ruling; ActivityMethodPhase is a distinct identifier and never trips this check); found %d violation(s):\n%s",
			len(violations), strings.Join(violations, "\n"))
	}
}
