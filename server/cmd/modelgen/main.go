// cmd/modelgen generates each component's Go model structs + service-contract
// interface from project.json — step 2 of the contract pipeline. The
// `.serviceContracts` map in `.aiarch/state/project.json` is now the OWNER of the
// contracts: each built entry carries a contract document (`title`, `$defs`,
// `interface`) plus self-describing metadata (`component`, `layer`, `goPackage`).
// modelgen turns that document → Go, so project.json is the single source of
// truth and the Go model (`contract.gen.go`) is a generated output. (The former
// per-component `contract.schema.json` seed files have been retired — their
// content now lives in project.json.)
//
// This is a thin CLI shim over the platform library
// github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen,
// which owns the actual emitter (built on github.com/google/jsonschema-go — the
// same complete draft 2020-12 implementation used for reflection). The shim
// supplies the two archistrator-specific inputs (the server module path and the
// engine-impl allowlist) and writes each returned <goPackage>/contract.gen.go,
// then a second, symmetric pass writes each <goPackage>/fake/fake.gen.go
// (modelgen.GenerateFakes — one Fake<Iface> test double per contract that has
// an interface).
//
// For each `.serviceContracts` entry with a non-empty `goPackage` it writes
// `<goPackage>/contract.gen.go` (e.g. internal/resourceaccess/artifact/
// contract.gen.go), in a package named after the goPackage's last segment.
// Entries without a `goPackage` are skipped, and so is every Client-layer entry
// (modelDocument): a client contract is the test surface of a generated client
// (spec §12 B1 — the web client's user actions, the MCP client's tools), whose Go
// is cmd/clientgen's output, not a model. An entry flagged `"stub": true` is
// CONTRACTED-BUT-UNBUILT: it still carries a goPackage + $defs + interface, and
// modelgen emits a fully generated not-implemented impl for it.
//
// It also writes `<goPackage>/fake/fake.gen.go` for every contract with an
// interface (e.g. internal/resourceaccess/artifact/fake/fake.gen.go), in a
// package named `<base>fake`. These generated fakes replace hand-written test
// doubles. Contracts with no interface are skipped by the library.
//
// Usage:
//
//	cd server && make gen-models                       # all built contracts
//	cd server && go run ./cmd/modelgen ../.aiarch/state/project.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

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
	"interventionEngine",
	"settlementEngine",
	"billingEngine",
	"operationEstimationEngine",
	"autoscalerEngine",
	"estimationEngine",
	"designHealthEngine",
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

	raw, err = modelDocument(raw)
	if err != nil {
		fatal("modelgen: %v", err)
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

	// GenerateFakes emits one Fake<Iface> test double per contract that has an
	// interface, keyed by "<goPackage>/fake" (e.g.
	// internal/resourceaccess/artifact/fake). These generated fakes replace
	// hand-written test doubles; contracts with no interface are skipped by the
	// library. Written in a second, symmetric pass so a contract-emission
	// failure never leaves a half-written fake tree.
	fakes, err := modelgen.GenerateFakes(raw, modelgen.Config{
		ModulePath:          serverModulePath,
		EngineImplAllowlist: engineImplAllowlist,
	})
	if err != nil {
		fatal("modelgen: generate fakes: %v", err)
	}

	fakePackages := make([]string, 0, len(fakes))
	for fakePackage := range fakes {
		fakePackages = append(fakePackages, fakePackage)
	}
	sort.Strings(fakePackages)

	for _, fakePackage := range fakePackages {
		if err := os.MkdirAll(fakePackage, 0o750); err != nil { // #nosec G703 -- fakePackage is derived from project.json's own .serviceContracts goPackage keys, a trusted repo-local document, not an external trust boundary
			fatal("mkdir %s: %v", fakePackage, err)
		}
		dest := fakePackage + "/fake.gen.go"
		if err := os.WriteFile(dest, fakes[fakePackage], 0o600); err != nil { // #nosec G703 -- dest is derived from project.json's own .serviceContracts goPackage keys, a trusted repo-local document, not an external trust boundary
			fatal("write %s: %v", dest, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", dest)
	}
}

// modelDocument is the project document modelgen generates from: every
// .serviceContracts entry except the Client-layer ones. A client contract names its
// surface's ops (spec §12 B1) and may name a goPackage — the MCP client's scenario
// tests are generated into the tools package — but its Go is the client layer
// cmd/clientgen generates, so it has no model, interface or fake to emit. Every other
// entry passes through as its raw bytes, so its $defs and param order survive.
func modelDocument(raw []byte) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("parse project.json: %w", err)
	}
	var contracts map[string]json.RawMessage
	if err := json.Unmarshal(top["serviceContracts"], &contracts); err != nil {
		return nil, fmt.Errorf("parse .serviceContracts: %w", err)
	}
	for key, entry := range contracts {
		var meta struct {
			Layer string `json:"layer"`
		}
		if err := json.Unmarshal(entry, &meta); err != nil {
			return nil, fmt.Errorf("parse contract %q: %w", key, err)
		}
		if strings.EqualFold(meta.Layer, "client") {
			delete(contracts, key)
		}
	}
	kept, err := json.Marshal(contracts)
	if err != nil {
		return nil, err
	}
	top["serviceContracts"] = kept
	return json.Marshal(top)
}

func fatal(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}
