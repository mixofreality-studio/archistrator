package internal_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// contract_defs_test.go is the HARD CHECK that the contract pipeline never strays
// back to an interface-only ("no models") contract. It is the executable guard that
// replaced the modelgen escape hatch (a built contract with an `interface` but no
// `$defs`): once projectstate migrated onto reflected $defs like every other
// component, an empty-$defs built contract has no legitimate producer, so both
// halves below must hold for EVERY built component:
//
//  1. .serviceContracts[c].$defs is NON-EMPTY — project.json, the contract owner,
//     carries the component's model schema.
//  2. <goPackage>/contract.gen.go declares AT LEAST ONE model type (a generated
//     top-level `type` beyond the service interface) — the generated Go surface
//     actually carries those models.
//
// "Built" = a contract entry with a non-empty goPackage (stubs and non-built
// entries have none and are skipped, matching cmd/modelgen's selection).

// contractEntry is the slice of a .serviceContracts entry this check reads.
type contractEntry struct {
	GoPackage string                     `json:"goPackage"`
	Defs      map[string]json.RawMessage `json:"$defs"`
	Interface *struct {
		Name string `json:"name"`
	} `json:"interface"`
}

func TestEveryBuiltContractHasModels(t *testing.T) {
	repoRoot := findRepoRootFromCwd(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".aiarch", "state", "project.json"))
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}
	var top struct {
		ServiceContracts map[string]contractEntry `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("parse project.json: %v", err)
	}
	if len(top.ServiceContracts) == 0 {
		t.Fatal("no .serviceContracts in project.json")
	}

	keys := make([]string, 0, len(top.ServiceContracts))
	for k := range top.ServiceContracts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	built := 0
	for _, k := range keys {
		e := top.ServiceContracts[k]
		if e.GoPackage == "" {
			continue // stub / non-built entry — no generated surface, like modelgen skips
		}
		built++

		// (1) project.json carries a non-empty model schema.
		if len(e.Defs) == 0 {
			t.Errorf("%s: built contract has empty $defs — a contract must declare its model types "+
				"(the interface-only escape hatch was removed; re-seed via cmd/schemagen)", k)
		}

		// (2) the generated Go surface declares at least one model type (beyond the
		// service interface itself).
		genPath := filepath.Join(repoRoot, "server", e.GoPackage, "contract.gen.go")
		ifaceName := ""
		if e.Interface != nil {
			ifaceName = e.Interface.Name
		}
		modelTypes, err := generatedModelTypes(genPath, ifaceName)
		if err != nil {
			t.Errorf("%s: %v", k, err)
			continue
		}
		if modelTypes == 0 {
			t.Errorf("%s: %s declares no model type (only the service interface) — "+
				"the generated file must carry the contract's model types", k, e.GoPackage)
		}
	}
	if built == 0 {
		t.Fatal("no built (goPackage) service contracts found — the check verified nothing")
	}
}

// generatedModelTypes parses a contract.gen.go and returns the number of top-level
// `type` declarations that are NOT the service interface (i.e. the model types:
// structs, enums/named scalars, and any sealed-sum interface). ifaceName is the
// generated service-contract interface's name, which is excluded from the count.
func generatedModelTypes(path, ifaceName string) (int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if ts.Name.Name == ifaceName {
				continue // the service interface is the port, not a model
			}
			n++
		}
	}
	return n, nil
}

// findRepoRootFromCwd ascends from the test's working directory to the directory
// holding `.aiarch/state/project.json` (the repo root). Self-contained so the check
// runs in the default (untagged) test build — the analogous helper in
// method_design_test.go is behind the `methoddesign` build tag.
func findRepoRootFromCwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".aiarch", "state", "project.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (.aiarch/state/project.json) ascending from %s", dir)
		}
		dir = parent
	}
}
