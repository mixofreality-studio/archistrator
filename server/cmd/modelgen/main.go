// cmd/modelgen generates each component's Go model structs + service-contract
// interface from project.json — step 2 of the contract pipeline. The
// `.serviceContracts` map in `.aiarch/state/project.json` is now the OWNER of the
// contracts: each built entry carries a contract document (`title`, `$defs`,
// `interface`) plus self-describing metadata (`component`, `layer`, `goPackage`).
// modelgen turns that document → Go, so project.json is the single source of
// truth and the Go model (`contract.gen.go`) is a generated output. (The former
// per-component `contract.schema.json` seed files have been retired — their
// content now lives in project.json; cmd/schemagen is a deprecated re-seed tool.)
//
// This is a thin CLI shim over the platform library
// github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen,
// which owns the actual emitter (built on github.com/google/jsonschema-go — the
// same complete draft 2020-12 implementation used for reflection). The shim
// supplies the two archistrator-specific inputs (the server module path and the
// engine-impl allowlist) and writes each returned <goPackage>/contract.gen.go.
//
// For each `.serviceContracts` entry with a non-empty `goPackage` it writes
// `<goPackage>/contract.gen.go` (e.g. internal/resourceaccess/artifact/
// contract.gen.go), in a package named after the goPackage's last segment.
// Entries without a `goPackage` are skipped. An entry flagged `"stub": true` is
// CONTRACTED-BUT-UNBUILT: it still carries a goPackage + $defs + interface, and
// modelgen emits a fully generated not-implemented impl for it.
//
// Usage:
//
//	cd server && make gen-models                       # all built contracts
//	cd server && go run ./cmd/modelgen ../.aiarch/state/project.json
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
)

// projectFile is the default path (relative to the server module root, where the
// gen targets run) to the head-state document that owns the contracts.
const projectFile = "../.aiarch/state/project.json"

// serverModulePath is the Go module path of the server tree — the prefix
// modelgen prepends to a dep's `goPackage` to form the full import path of the
// dep's published-interface package.
const serverModulePath = "github.com/mixofreality-studio/archistrator/server"

// engineImplAllowlist gates which Engine contracts gain a generated impl struct +
// constructor. Engines carry no `infra` field, so an explicit allowlist scopes the
// impl emission. All current engines are pure (field-less impl, no-arg constructor),
// so the full set is enrolled; replace this with a per-contract opt-in if a future
// engine ever needs a non-pure shape.
var engineImplAllowlist = []string{
	"reviewEngine",
	"handOffEngine",
	"interventionEngine",
	"settlementEngine",
	"billingEngine",
	"operationEstimationEngine",
	"autoscalerEngine",
	"estimationEngine",
}

func main() {
	path := projectFile
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	raw, err := os.ReadFile(path) // #nosec G703 -- path is the CLI argument to a developer-run codegen tool, no trust boundary
	if err != nil {
		fatal("read %s: %v", path, err)
	}

	out, err := modelgen.Generate(raw, modelgen.Config{
		ModulePath:          serverModulePath,
		EngineImplAllowlist: engineImplAllowlist,
	})
	if err != nil {
		fatal("modelgen: %v", err)
	}

	goPackages := make([]string, 0, len(out))
	for goPackage := range out {
		goPackages = append(goPackages, goPackage)
	}
	sort.Strings(goPackages)

	for _, goPackage := range goPackages {
		dest := goPackage + "/contract.gen.go"
		if err := os.WriteFile(dest, out[goPackage], 0o600); err != nil { // #nosec G703 -- dest is derived from project.json's own .serviceContracts goPackage keys, a trusted repo-local document, not an external trust boundary
			fatal("write %s: %v", dest, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", dest)
	}
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
