// Command appgen generates, in one project.json read: the per-manager Temporal
// layer (activities/invokers/worker), the client SDK for the sibling
// systemtests module, and the archistrator-server container config + harness
// env-name consts — via framework-go-projectmodel + framework-go-app-generator's
// temporalgen/transportgen/configgen. Byte-idempotent; run via `make gen-temporal`,
// `make gen-sdk`, or `make gen-config` (all invoke this same command; see the
// Makefile for their distinct diff scopes).
//
// Release-backed: framework-go-app-generator + framework-go-projectmodel are
// published platform modules pinned in server/go.mod, so this command builds
// under ordinary GOWORK=off server builds/tests — no build tag, no workspace.
// configgen was folded in from the former workspace-only cmd/configgen once
// framework-go-app-generator shipped it in a tag (v0.4.0).
package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
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
	// configgen (archistrator-server container config + systemtests harness
	// env-name consts) output locations — folded in from the former
	// (workspace-only) cmd/configgen now that framework-go-app-generator/configgen
	// is published.
	containerKey  = "archistrator-server"
	envPrefix     = "ARCHISTRATOR"
	configPackage = "main"
	configOut     = "cmd/server/config.gen.go"
	envNamesOut   = "../systemtests/internal/harness/envnames.gen.go"
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
	generateConfig(m)
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

// generateConfig emits the archistrator-server container's env-loaded
// configuration file (cmd/server/config.gen.go) from project.json's deployment
// model via framework-go-app-generator/configgen, plus the systemtests-harness
// env-name const file (folded in from the former workspace-only cmd/configgen —
// framework-go-app-generator/configgen is now a published, tag-free dependency,
// so it runs in the same GOWORK=off appgen pass as the Temporal layer and SDK).
func generateConfig(m *projectmodel.Model) {
	files, err := configgen.Generate(m, configgen.Config{
		ContainerKey: containerKey,
		EnvPrefix:    envPrefix,
		PackageName:  configPackage,
	})
	if err != nil {
		fatal(fmt.Errorf("configgen: %w", err))
	}
	src, ok := files["config.gen.go"]
	if !ok {
		fatal(fmt.Errorf("configgen: emitter returned no config.gen.go"))
	}
	// #nosec G306 -- generated source, no secret content; standard 0644 like the other gen targets
	if err := os.WriteFile(configOut, src, 0o600); err != nil {
		fatal(fmt.Errorf("write %s: %w", configOut, err))
	}
	fmt.Printf("appgen: config → %s (%d bytes)\n", configOut, len(src))

	if err := generateEnvNames(m); err != nil {
		fatal(fmt.Errorf("envnames: %w", err))
	}
}

// harnessEnvName is one systemtests-harness env-name const: the Go const name and
// where its value is sourced from the deployment model (a setting name, or an
// infra "<key>.<INPUT>"). Sourcing the value from the model means a rename of the
// var in project.json fails the harness COMPILE (const gone / value moved), not at
// runtime.
type harnessEnvName struct {
	Const    string // emitted Go const name
	Setting  string // deployment setting name (mutually exclusive with Infra*)
	InfraKey string // deployment infra key
	InfraIn  string // deployment infra input token
}

// harnessEnvNames is the fixed set of env-var names the systemtests harness
// binds when booting a server. Each is resolved against the committed deployment
// declarations so the emitted const carries TODAY'S exact var name.
var harnessEnvNames = []harnessEnvName{
	{Const: "EnvPostgresURL", InfraKey: "postgres", InfraIn: "URL"},
	{Const: "EnvTemporalHostPort", InfraKey: "temporal", InfraIn: "HOSTPORT"},
	{Const: "EnvTemporalNamespace", InfraKey: "temporal", InfraIn: "NAMESPACE"},
	{Const: "EnvListenAddr", Setting: "listenAddr"},
	{Const: "EnvAuthDevMode", Setting: "authDevMode"},
	{Const: "EnvConstructionDryRun", Setting: "constructionDryRun"},
	{Const: "EnvOperationsDryRun", Setting: "operationsDryRun"},
	{Const: "EnvProjectStateGitLocal", Setting: "projectStateGitLocal"},
	{Const: "EnvProjectStateGitRepoURL", Setting: "projectStateGitRepoURL"},
}

// generateEnvNames emits envnames.gen.go — a package-harness const block mapping
// each harnessEnvName to its resolved env var, read directly off m.Deployment.
func generateEnvNames(m *projectmodel.Model) error {
	if m.Deployment == nil {
		return fmt.Errorf("model has no deployment")
	}
	settingEnv := map[string]string{}
	for _, s := range m.Deployment.Settings {
		settingEnv[s.Name] = s.Env
	}
	infraEnv := map[string]map[string]string{}
	for _, i := range m.Deployment.Infrastructure {
		infraEnv[i.Key] = i.Env
	}

	var b []byte
	// Header kept byte-identical to the former cmd/configgen's emission so the
	// fold produced zero diff in the committed generated file.
	b = append(b, []byte("// Code generated by configgen. DO NOT EDIT.\n//\n")...)
	b = append(b, []byte("// Env-var names the systemtests harness binds when booting a server,\n")...)
	b = append(b, []byte("// sourced from project.json's deployment declarations so a renamed var\n")...)
	b = append(b, []byte("// fails this harness COMPILE, not at runtime.\n")...)
	b = append(b, []byte("package harness\n\nconst (\n")...)
	for _, n := range harnessEnvNames {
		var val string
		switch {
		case n.Setting != "":
			v, ok := settingEnv[n.Setting]
			if !ok || v == "" {
				return fmt.Errorf("setting %q: no resolved env in deployment", n.Setting)
			}
			val = v
		default:
			m, ok := infraEnv[n.InfraKey]
			if !ok {
				return fmt.Errorf("infra %q: not declared in deployment", n.InfraKey)
			}
			v, ok := m[n.InfraIn]
			if !ok || v == "" {
				return fmt.Errorf("infra %q input %q: no resolved env override in deployment", n.InfraKey, n.InfraIn)
			}
			val = v
		}
		b = append(b, []byte(fmt.Sprintf("\t%s = %q\n", n.Const, val))...)
	}
	b = append(b, []byte(")\n")...)

	out, err := format.Source(b)
	if err != nil {
		return fmt.Errorf("gofmt: %w\n%s", err, b)
	}
	if err := os.WriteFile(envNamesOut, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", envNamesOut, err)
	}
	fmt.Printf("appgen: envnames → %s (%d bytes)\n", filepath.Base(envNamesOut), len(out))
	return nil
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "appgen:", err); os.Exit(1) }
