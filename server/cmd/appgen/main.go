// Command appgen generates, in one project.json read: the per-manager Temporal
// layer (activities/invokers/worker), the archistrator-server container config,
// and the archistrator-server composition root — via framework-go-projectmodel +
// framework-go-app-generator's temporalgen/configgen/composegen. Byte-idempotent;
// run via `make gen-temporal`, `make gen-config`, or `make gen-main` (all invoke
// this same command; see the Makefile for their distinct diff scopes).
//
// Release-backed: framework-go-app-generator + framework-go-projectmodel are
// published platform modules pinned in server/go.mod, so this command builds
// under ordinary GOWORK=off server builds/tests — no build tag, no workspace.
// configgen was folded in from the former workspace-only cmd/configgen once
// framework-go-app-generator shipped it in a tag (v0.4.0).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/temporalgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

const (
	projectFile  = "../.aiarch/state/project.json"
	serverModule = "github.com/mixofreality-studio/archistrator/server"
	// configgen (archistrator-server container config) output location — folded
	// in from the former (workspace-only) cmd/configgen now that
	// framework-go-app-generator/configgen is published.
	containerKey  = "archistrator-server"
	envPrefix     = "ARCHISTRATOR"
	configPackage = "main"
	configOut     = "cmd/server/config.gen.go"
	// composegen (archistrator-server composition root) output location. Emitted
	// into the existing cmd/server package alongside config.gen.go whose *Config
	// it threads. framework-go-app-generator/composegen shipped in the v0.5.0 tag,
	// so this runs in the same GOWORK=off appgen pass as the other emitters.
	mainOut = "cmd/server/main.gen.go"
)

// projectstatePkg is the archistrator projectstate package import — the source of
// the GitHub projectStateAccess variant's composition-root port types
// (ProjectCatalog + CredentialMinter) that projectstate cannot construct itself
// (an RA→RA edge is forbidden inside the package). See generateMain's
// VariantHookArgs.
const projectstatePkg = serverModule + "/internal/resourceaccess/projectstate"

// githubInfraPkg is the framework-go-infrastructure-github import (package name
// `github`) — the source of the shared *github.AppClient the sourceControlAccess
// GitHub + agenticJobAccess GitHubActions variant-arg hooks build and
// spread into their EXISTING generated DI constructors. Threading the AppClient
// via a hook (not the raw github-app substrate strings) reuses the generated
// ctors verbatim: a naive positional re-fold would emit a SECOND
// New<Variant><Iface> with the same name and collide.
const githubInfraPkg = "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github"

// messagebusPkg is the archistrator messagebus package import — the source of
// the ExecutionKind/KindBinding types the messageBus Temporal variant-arg hook
// (Task 7c) returns; see generateMain's VariantHookArgs["messageBus/Temporal"].
const messagebusPkg = serverModule + "/internal/utility/messagebus"

// callerKeyedOps: ops whose idempotency is BUSINESS-stable, not run-scoped —
// the workflow supplies the key explicitly (billing money-moves; see
// gatewayIdempotencyKey in internal/manager/billing).
var callerKeyedOps = map[string]map[string][]string{
	"billingManager": {"merchantGateway": {"ChargeCustomer", "ValidateStoredInstrument"}},
}

// managers is the CODE-GENERATED set: temporalgen emits contract/activities/
// invokers/worker for each. It is the same three managers as the web-wired sets
// (exposedManagers in cmd/clientgen, WebExposedManagers below), but billingManager
// is web-mounted with its read-only GetBillingCycle only.
var managers = []string{"deliveryManager", "operationsManager", "billingManager"}

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

	generateConfig(m)
	generateMain(m)
}

// generateMain emits the archistrator-server composition root
// (cmd/server/main.gen.go) from project.json's deployment model + service
// contracts via framework-go-app-generator/composegen: the ordered boot walk
// (signal ctx → telemetry → Temporal dial → Postgres pool → per-binding RA
// construction → Engines → security Utility → Managers via their DI ctors → one
// Worker per Manager → generated web Handlers + NewServer + ExtraMounts → serve +
// shutdown), driving a hand cmd/server/hooks.go for the genuinely-compositional
// policy. Four driver knobs the deployment model cannot express:
//
//   - WebExposedManagers = clientgen's exposedManagers (the 3 web-wired
//     managers). billingManager is among them for its read-only GetBillingCycle
//     (view-a-billing-cycle); clientgen narrows its generated
//     internal/client/web/billing Handler to that op, so the money-moving ops stay
//     unexposed.
//   - VariantHookArgs for the variant ctors whose args the model cannot supply
//     (G3): projectstate GitHub needs the sourcecontrol-backed catalog + minter
//     PORTS (an RA→RA edge forbidden inside projectstate); artifact GitHubCloud
//     needs the repoURL/owner strings + a typed int64 installationID; messageBus
//     Temporal (Task 7c) needs the cross-manager ExecutionKind→KindBinding table
//     (composegen threads its bound temporal infra, `tc`, automatically as an
//     extra positional constructor arg — see the entry's own comment below).
//   - VariantConstructorNoError overrides the infra-implies-error-return
//     heuristic for messageBus/Temporal (Task 7c): messagebus.
//     NewTemporalMessageBus is single-return despite consuming infra, unlike
//     every other infra-consuming variant above.
//   - ScheduleRegistrarComponent (fix round 1, Task 7c live-firing review)
//     names "messageBus" as the component whose Manager dependents get a
//     startup RegisterSchedules call emitted after their Worker starts.
func generateMain(m *projectmodel.Model) {
	files, err := composegen.Generate(m, composegen.Config{
		ContainerKey: containerKey,
		ModulePath:   serverModule,
		PackageName:  configPackage,
		EnvPrefix:    envPrefix,
		WebExposedManagers: []string{
			"billingManager",
			"deliveryManager",
			"operationsManager",
		},
		VariantHookArgs: map[string][]composegen.HookArgType{
			"projectStateAccess/GitHub": {
				{GoType: "string"}, // webHost
				{GoType: "string"}, // account
				{GoType: "projectstate.ProjectCatalog", GoImport: projectstatePkg},
				{GoType: "projectstate.CredentialMinter", GoImport: projectstatePkg},
			},
			// gitActivityStatusAccess / designSessionAccess (B6) are secondary contracts
			// sharing projectStateAccess's own goPackage and git substrate — each variant
			// constructor builds its OWN second GitStore over the SAME repoURL (GitLocal)
			// or the SAME shared catalog/minter ports (GitHub, reusing the identical
			// 4-arg tuple as projectStateAccess/GitHub above), never raw github-app
			// credentials directly. The GitLocal arm also needs a hook (not a plain
			// positional settings arg) purely to REUSE cfg.ProjectStateGitRepoURL rather
			// than declare a duplicate binding-scoped setting for the same env var.
			"gitActivityStatusAccess/GitLocal": {
				{GoType: "string"}, // repoURL
			},
			"gitActivityStatusAccess/GitHub": {
				{GoType: "string"}, // webHost
				{GoType: "string"}, // account
				{GoType: "projectstate.ProjectCatalog", GoImport: projectstatePkg},
				{GoType: "projectstate.CredentialMinter", GoImport: projectstatePkg},
			},
			"designSessionAccess/GitLocal": {
				{GoType: "string"}, // repoURL
			},
			// activityExecutionAccess (stage 3) is the fourth facet over the same one
			// component, package and git substrate — so it takes the identical tuples as
			// its siblings above, for the identical reason.
			"activityExecutionAccess/GitLocal": {
				{GoType: "string"}, // repoURL
			},
			"activityExecutionAccess/GitHub": {
				{GoType: "string"}, // webHost
				{GoType: "string"}, // account
				{GoType: "projectstate.ProjectCatalog", GoImport: projectstatePkg},
				{GoType: "projectstate.CredentialMinter", GoImport: projectstatePkg},
			},
			// episodeAccess/LocalFS (Task 8) reuses the SAME shared-repo repoURL
			// setting (cfg.ProjectStateGitRepoURL) the GitActivityStatusAccess/
			// DesignSessionAccess/ActivityExecutionAccess GitLocal arms above already
			// hook-arg through — one repo per server config, name-as-identity — purely
			// to avoid a duplicate env var for the same value. The cloud profile builds
			// the NoOp variant (no infra, no hook arg needed).
			"episodeAccess/LocalFS": {
				{GoType: "string"}, // repoURL
			},
			"designSessionAccess/GitHub": {
				{GoType: "string"}, // webHost
				{GoType: "string"}, // account
				{GoType: "projectstate.ProjectCatalog", GoImport: projectstatePkg},
				{GoType: "projectstate.CredentialMinter", GoImport: projectstatePkg},
			},
			// artifactAccess GitHubCloud threads the SAME shared *github.AppClient
			// as sourceControlAccess/agenticJobAccess below (the test-run store
			// lists/downloads Actions run artifacts through it, and the git auth
			// resolver mints its installation token from it) instead of re-deriving
			// a private client from the raw github-app cfg strings.
			"artifactAccess/GitHubCloud": {
				{GoType: "*github.AppClient", GoImport: githubInfraPkg},
				{GoType: "string"}, // repoURL
				{GoType: "string"}, // owner
				{GoType: "int64"},  // installationID
			},
			// sourceControlAccess GitHub + agenticJobAccess GitHubActions
			// thread the shared *github.AppClient (built in the hook from the
			// github-app cfg strings) + the variant's own settings, reusing their
			// EXISTING generated DI ctors verbatim. The AppClient cannot be a
			// substrate string, and a raw positional re-fold would collide with the
			// generated ctor name — so these are hook-args cases too.
			"sourceControlAccess/GitHub": {
				{GoType: "*github.AppClient", GoImport: githubInfraPkg},
				{GoType: "string"}, // defaultAccount
				{GoType: "string"}, // appSlug
				{GoType: "bool"},   // repoPrivate
			},
			// sourceControlAccess GitLocal (the local PR rail): branch/PR/merge over the
			// local file:// repo. A hook-args case for the same reason as the other
			// GitLocal arms above — it REUSES cfg.ProjectStateGitRepoURL rather than
			// declare a duplicate binding-scoped setting for the same env var.
			"sourceControlAccess/GitLocal": {
				{GoType: "string"}, // repoURL
			},
			"agenticJobAccess/GitHubActions": {
				{GoType: "*github.AppClient", GoImport: githubInfraPkg},
				{GoType: "string"}, // owner
				{GoType: "string"}, // repo
				{GoType: "string"}, // workflowFile
				{GoType: "string"}, // ref
				{GoType: "int64"},  // installationID
			},
			// messageBus Temporal (Task 7c): the deployment model can supply `tc`
			// positionally (its bound "temporal" infra — composegen threads it as an
			// extra POSITIONAL ARG on the surrounding NewTemporalMessageBus(tc, ...)
			// call automatically, since the substrate resolves to an
			// already-constructed local, not a cfg field; the hook itself stays
			// cfg-only), but the KindBinding table (ExecutionKind -> registered
			// workflow-type name + owning Manager's TaskQueue, across
			// billing/operations/construction's RegisterSchedule call sites) is
			// cross-manager composition-root knowledge the model has no notion of at
			// all (see hooks.go's MessageBusTemporalArgs).
			"messageBus/Temporal": {
				{GoType: "map[messagebus.ExecutionKind]messagebus.KindBinding", GoImport: messagebusPkg},
			},
		},
		// messagebus.NewTemporalMessageBus(cl, table) is single-return despite
		// consuming the temporal infra (Task 7c) — every OTHER infra-consuming
		// variant above happens to return an error; this one does not.
		VariantConstructorNoError: map[string]bool{
			"messageBus/Temporal": true,
		},
		// ScheduleRegistrarComponent (fix round 1, Task 7c live-firing review,
		// FINDING 4): names "messageBus" as the component whose Manager
		// dependents (billing/operations/construction) get a startup
		// RegisterSchedules(ctx, messageBus) call emitted right after their
		// embedded Worker starts. Replaces composegen's earlier hard-coded
		// "messageBus" constant with this driver-supplied value.
		ScheduleRegistrarComponent: "messageBus",
	})
	if err != nil {
		fatal(fmt.Errorf("composegen: %w", err))
	}
	src, ok := files["main.gen.go"]
	if !ok {
		fatal(fmt.Errorf("composegen: emitter returned no main.gen.go"))
	}
	// #nosec G306 G703 -- generated source, no secret content; mainOut is a fixed constant path, no trust boundary
	if err := os.WriteFile(mainOut, src, 0o600); err != nil {
		fatal(fmt.Errorf("write %s: %w", mainOut, err))
	}
	fmt.Printf("appgen: main → %s (%d bytes)\n", mainOut, len(src))
}

// generateConfig emits the archistrator-server container's env-loaded
// configuration file (cmd/server/config.gen.go) from project.json's deployment
// model via framework-go-app-generator/configgen (folded in from the former
// workspace-only cmd/configgen — framework-go-app-generator/configgen is now a
// published, tag-free dependency, so it runs in the same GOWORK=off appgen pass
// as the Temporal layer).
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
	// #nosec G306 G703 -- generated source, no secret content; configOut is a fixed constant path, no trust boundary
	if err := os.WriteFile(configOut, src, 0o600); err != nil {
		fatal(fmt.Errorf("write %s: %w", configOut, err))
	}
	fmt.Printf("appgen: config → %s (%d bytes)\n", configOut, len(src))
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "appgen:", err); os.Exit(1) }
