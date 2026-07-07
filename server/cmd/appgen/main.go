//go:build appgen

// Command appgen generates the per-manager Temporal layer (activities/
// invokers/worker) from project.json via framework-go-projectmodel +
// framework-go-app-generator/temporalgen. Byte-idempotent; run via
// `make gen-temporal`.
//
// Gated behind the appgen build tag: framework-go-app-generator is an
// unpublished platform module (no tagged version yet), so this command only
// builds in Go workspace mode (`go run -tags appgen ./cmd/appgen`, per the
// gen-temporal Makefile target) and is excluded from ordinary
// GOWORK=off server builds/tests. Task 14 drops the tag once the platform
// module is released and pinned in go.mod.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/temporalgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

const (
	projectFile  = "../.aiarch/state/project.json"
	serverModule = "github.com/mixofreality-studio/archistrator/server"
)

// callerKeyedOps: ops whose idempotency is BUSINESS-stable, not run-scoped —
// the workflow supplies the key explicitly (billing money-moves; see
// gatewayIdempotencyKey in internal/manager/billing).
var callerKeyedOps = map[string]map[string][]string{
	"billingManager": {"merchantGateway": {"PayoutCustomer", "ChargeCustomer", "CreateConnectedAccount", "ValidateStoredInstrument"}},
}

var managers = []string{"systemDesignManager", "projectDesignManager", "constructionManager", "operationsManager", "billingManager"}

func main() {
	path := projectFile
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	m, err := projectmodel.LoadFile(path)
	if err != nil {
		fatal(err)
	}
	for _, w := range m.Warnings {
		fmt.Fprintln(os.Stderr, "appgen warning:", w)
	}
	for _, key := range managers {
		files, err := temporalgen.Generate(m, temporalgen.Config{
			ModulePath: serverModule, ManagerKey: key, CallerKeyedOps: callerKeyedOps[key],
		})
		if err != nil {
			fatal(fmt.Errorf("%s: %w", key, err))
		}
		dir := m.Contracts[key].GoPackage
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
				fatal(err)
			}
		}
		fmt.Printf("appgen: %s → %s (%d files)\n", key, dir, len(files))
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "appgen:", err); os.Exit(1) }
