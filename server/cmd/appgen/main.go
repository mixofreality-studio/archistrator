// Command appgen generates the per-manager Temporal layer (activities/
// invokers/worker) from project.json via framework-go-projectmodel +
// framework-go-app-generator/temporalgen. Byte-idempotent; run via
// `make gen-temporal`.
//
// Release-backed: framework-go-app-generator + framework-go-projectmodel are
// published platform modules pinned in server/go.mod, so this command builds
// under ordinary GOWORK=off server builds/tests — no build tag, no workspace.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/temporalgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/transportgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

const (
	projectFile  = "../.aiarch/state/project.json"
	serverModule = "github.com/mixofreality-studio/archistrator/server"
	// sdkDir is the systemtests-module package the generated client SDK
	// (transportgen: HTTP + MCP, stdlib-only) is written into. It lives in the
	// SIBLING systemtests module — the harness delegates its wire transports to
	// it — so it is emitted zero-import (see transportgen's package doc) and
	// never links server code (the test-authoring constitution R1/R3).
	sdkDir     = "../systemtests/internal/sdk"
	sdkPackage = "sdk"
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
			// #nosec G703 -- dir is a goPackage path from the committed project.json, name is a fixed generator filename; developer-run codegen, no trust boundary
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
				fatal(err)
			}
		}
		fmt.Printf("appgen: %s → %s (%d files)\n", key, dir, len(files))
	}

	generateSDK(m)
}

// generateSDK emits the self-contained client SDK (transportgen: HTTP + MCP,
// stdlib-only) for the 5 managers into ../systemtests/internal/sdk with
// prune-stale discipline (mirrors cmd/gen-systemtests): every *.gen.go not in
// the fresh output set is deleted, so a manager/op removed from project.json can
// never leave an orphan behind for the drift gate to trip on. UUIDAsString=true
// keeps the SDK zero-dependency (no google/uuid) so the systemtests module stays
// stdlib-only (depguard + the constitution test).
func generateSDK(m *projectmodel.Model) {
	files, err := transportgen.Generate(m, transportgen.Config{
		Managers: managers, PackageName: sdkPackage, UUIDAsString: true,
	})
	if err != nil {
		fatal(fmt.Errorf("sdk: %w", err))
	}
	if err := pruneStaleSDK(sdkDir, files); err != nil {
		fatal(fmt.Errorf("sdk prune: %w", err))
	}
	if err := os.MkdirAll(sdkDir, 0o755); err != nil { // #nosec G301 -- generator output dir, no trust boundary
		fatal(fmt.Errorf("sdk mkdir: %w", err))
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		// #nosec G304 G703 -- sdkDir is a fixed in-repo path, name is a fixed generator filename; developer-run codegen, no trust boundary
		if err := os.WriteFile(filepath.Join(sdkDir, name), files[name], 0o600); err != nil {
			fatal(fmt.Errorf("sdk write %s: %w", name, err))
		}
	}
	fmt.Printf("appgen: sdk → %s (%d files)\n", sdkDir, len(files))
}

// pruneStaleSDK deletes every previously-generated *.gen.go in dir that is not
// in the fresh output set — the same orphan-sweep discipline cmd/gen-systemtests
// applies to its *_table.gen.go files.
func pruneStaleSDK(dir string, files map[string][]byte) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*.gen.go"))
	if err != nil {
		return fmt.Errorf("glob %s: %w", dir, err)
	}
	for _, path := range matches {
		if _, keep := files[filepath.Base(path)]; keep {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("prune stale %s: %w", filepath.Base(path), err)
		}
		fmt.Printf("appgen: pruned stale sdk file %s\n", filepath.Base(path))
	}
	return nil
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "appgen:", err); os.Exit(1) }
