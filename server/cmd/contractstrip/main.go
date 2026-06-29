// cmd/contractstrip removes the hand-written contract surface from a component's
// source once that surface has been GENERATED into contract.gen.go — the
// automated form of the bootstrap's "remove hand-written declarations" step that
// breaks the contract chicken-and-egg (the seed reflection must run against a
// COMPILING package, so we generate first, then strip).
//
// It is registry-free: it reads each built component's owned type names ($defs
// keys + the interface name) from project.json `.serviceContracts` (the contract
// owner) and deletes exactly those top-level `type` declarations from the
// package's NON-generated, NON-test .go files.
//
// STEADY-STATE NOTE: the migrated components are already stripped (their
// hand-written contract surface was replaced by contract.gen.go long ago), so a
// re-run is a NO-OP. The tool is retained for re-bootstrapping a component.
//
// SAFETY (no silent behavior loss): if any of those types has a method (e.g. a
// `String()` on an enum), contractstrip REFUSES to strip it and reports it — that
// type carries behavior that is not pure data and must be handled deliberately
// (generate the behavior, or keep it out of the generated surface), never dropped.
//
// Usage:
//
//	cd server && go run ./cmd/contractstrip                       # all built contracts
//	cd server && go run ./cmd/contractstrip ../.aiarch/state/project.json
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/imports"
)

// projectFile is the default path (relative to the server module root, where the
// gen targets run) to the head-state document that owns the contracts.
const projectFile = "../.aiarch/state/project.json"

func main() {
	path := projectFile
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	raw, err := os.ReadFile(path) // #nosec G703 -- path is the CLI argument to a developer-run codegen tool, no trust boundary
	if err != nil {
		fatal("read %s: %v", path, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		fatal("parse %s: %v", path, err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		fatal("parse .serviceContracts in %s: %v", path, err)
	}
	keys := make([]string, 0, len(contracts))
	for k := range contracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		entry := contracts[k]
		// goPackage selects built entries (stubs have none) AND gives the dir whose
		// hand-written surface is stripped.
		var meta struct {
			GoPackage string `json:"goPackage"`
		}
		if err := json.Unmarshal(entry, &meta); err != nil {
			fatal("parse contract %q: %v", k, err)
		}
		if meta.GoPackage == "" {
			continue
		}
		if err := stripDir(meta.GoPackage, entry); err != nil {
			fatal("contractstrip %s: %v", k, err)
		}
	}
}

func stripDir(dir string, entry []byte) error {
	owned, err := ownedNames(entry)
	if err != nil {
		return err
	}
	if len(owned) == 0 {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, ".gen.go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if err := stripFile(filepath.Join(dir, name), owned); err != nil {
			return err
		}
	}
	return nil
}

// ownedNames returns the set of type names the generated surface provides:
// every $defs key plus the interface name, read from one project.json contract
// entry (raw JSON).
func ownedNames(entry []byte) (map[string]bool, error) {
	var doc struct {
		Defs      map[string]json.RawMessage `json:"$defs"`
		Interface *struct {
			Name string `json:"name"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(entry, &doc); err != nil {
		return nil, fmt.Errorf("parse contract entry: %w", err)
	}
	owned := map[string]bool{}
	for k := range doc.Defs {
		owned[k] = true
	}
	if doc.Interface != nil && doc.Interface.Name != "" {
		owned[doc.Interface.Name] = true
	}
	return owned, nil
}

func stripFile(path string, owned map[string]bool) error {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a developer-run codegen tool argument, no trust boundary
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, raw, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	// Safety: refuse to strip a type that has methods (behavior).
	var behavioral []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		if base := receiverType(fn.Recv.List[0].Type); owned[base] {
			behavioral = append(behavioral, base+"."+fn.Name.Name+"()")
		}
	}
	if len(behavioral) > 0 {
		sort.Strings(behavioral)
		return fmt.Errorf("refusing to strip types with behavior in %s: %s\n"+
			"  these carry logic, not pure data — generate the behavior or exclude them from the contract surface",
			path, strings.Join(behavioral, ", "))
	}

	var kept []ast.Decl
	changed := false
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			kept = append(kept, d)
			continue
		}
		// Const blocks binding members to an owned enum type are provided by the
		// generated file; drop them whole.
		if gd.Tok == token.CONST {
			if constBlockOwned(gd, owned) {
				changed = true
				continue
			}
			kept = append(kept, d)
			continue
		}
		if gd.Tok != token.TYPE {
			kept = append(kept, d)
			continue
		}
		var specs []ast.Spec
		for _, sp := range gd.Specs {
			ts := sp.(*ast.TypeSpec)
			if owned[ts.Name.Name] {
				changed = true
				continue
			}
			specs = append(specs, sp)
		}
		if len(specs) == 0 {
			continue // whole decl removed
		}
		gd.Specs = specs
		kept = append(kept, gd)
	}
	if !changed {
		return nil
	}
	f.Decls = kept

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return err
	}
	// Prune imports orphaned by the removed declarations and gofmt.
	out, err := imports.Process(path, buf.Bytes(), nil)
	if err != nil {
		return fmt.Errorf("imports.Process %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "stripped %s\n", path)
	return nil
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}

// constBlockOwned reports whether a const GenDecl binds members to an owned enum
// type — detected via the first value-spec that carries an explicit type (the
// `Name Type = iota` idiom; later bare specs inherit it).
func constBlockOwned(gd *ast.GenDecl, owned map[string]bool) bool {
	for _, sp := range gd.Specs {
		vs, ok := sp.(*ast.ValueSpec)
		if !ok || vs.Type == nil {
			continue
		}
		if id, ok := vs.Type.(*ast.Ident); ok {
			return owned[id.Name]
		}
		return false
	}
	return false
}

// receiverType returns the base type name of a method receiver (T or *T).
func receiverType(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverType(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}
