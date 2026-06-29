package internal_test

import (
	"go/ast"
	"go/types"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
	"golang.org/x/tools/go/packages"
)

// modulePrefix is the import-path prefix for this module's internal packages.
const modulePrefix = "github.com/mixofreality-studio/archistrator/server/internal/"

// TestMethodLayering enforces The Method's layer model on the archistrator server's
// internal packages: strictly closed downward imports, Temporal only in the
// manager layer, Engine/ResourceAccess ports named *Engine/*Access with error-
// returning methods, and the operator-curated dependency allowlist.
//
// Layer layout (top→bottom):
//   - manager/systemdesign                       — Manager façade; OWNS Temporal.
//     The Manager's SEQUENCE owns the Phase-1 prompt corpus (2026-05-29 re-cut);
//     drafting AND PM-critique are the GENERIC workerAccess.GenerateTypedData[T]
//     wrapped in a Manager-owned Activity, so the worker I/O stays out of replayed
//     workflow code.
//   - engine/{estimation,operationestimation,settlement}
//     — Engine ports. The estimate Engines are pure deterministic Phase-2 computation.
//     (The former systemdesign Engine was WITHDRAWN by the 2026-05-29 re-cut —
//     drafting a Method artifact via an LLM is the Manager's activity, not an
//     Engine. Server-side rendering was likewise removed — the client renders
//     typed models. The artifactValidation Engine was REMOVED 2026-06-16 — the
//     Method gate moved to framework-go/methodcheck, the seated `go test` in the
//     user repo; the Managers retained only the small Finding value type, relocated
//     locally to systemdesign/projectdesign.)
//   - resourceaccess/{projectstate,artifact,worker}
//     — ResourceAccess components, each fronting a single Resource. NO RA→RA
//     imports: worker is the GENERIC typed LLM worker (Generate raw JSON +
//     package-level GenerateTypedData[T] + Cancel) with NO Method-model knowledge —
//     it imports neither projectstate nor artifact; the typed Method models live
//     in projectstate and the construction value types live in artifact; only
//     Engines and the Manager import those.
//
// The Method has NO "domain" layer — only Clients/Managers/Engines/
// ResourceAccess/Resources/Utilities. Shared typed Method models are owned by
// the RA that fronts them (projectstate); downstream Engines and the Manager
// import the owning RA's package as a normal downward edge.
func TestMethodLayering(t *testing.T) {
	spec := arch.MethodSpec(
		"..", // module root relative to internal/
		modulePrefix,
	)
	// There is no sideways escape hatch to configure. The typed-models rework
	// eliminated every sideways RA→RA import: worker is the generic typed LLM
	// worker that imports neither projectstate nor artifact; the Engines'
	// projectstate import is a legal Engine→RA downward edge. MethodSpec
	// makes every layer NoSideways and requires every internal package to
	// classify into client/manager/engine/resourceaccess, so an out-of-band
	// package (e.g. a "domain" dir) fails the build outright.

	// Dependency allowlist — the FIXED, operator-curated infrastructure menu for
	// any system built with archistrator (the CustomerAppInfrastructure volatility made
	// executable). Production code may import only:
	//   - the Go standard library (auto-allowed; not listed),
	//   - the mixofreality-studio archistrator-platform framework family + this app's own module,
	//   - the sanctioned infrastructure drivers carried by the framework-go
	//     infrastructure modules: Postgres (pgx), Git/Gitea (go-git), and the
	//     durable-execution substrate (Temporal).
	// An unsanctioned dependency (e.g. a MongoDB driver) fails this test. Only an
	// archistrator operator may widen the menu by adding a prefix here; that is the one
	// and only place the menu is defined (no parallel doc to drift out of sync).
	// Test-only deps (testcontainers, the Gitea SDK) are NOT scanned — Check loads
	// with Tests:false — so they need no entry.
	// Temporal-isolation exemption — the single architecturally-sanctioned
	// exception to "Temporal lives only in the Manager layer". durableExecutionAccess
	// is the ResourceAccess whose fronted Resource IS the durable-execution substrate
	// (Temporal) itself — "the architecturally hardest case in the corpus"
	// (durableExecutionAccess.md §1). Its concrete adapter (temporal.go) MUST speak
	// the Temporal control-plane SDK, exactly as projectstate's adapter speaks pgx
	// and artifact's speaks go-git. The exemption relaxes ONLY the Temporal-isolation
	// rule for this one package; it remains subject to classification, downward-only
	// imports, no-sideways, the Access port + error returns, and the dependency
	// allowlist. The CONTRACT surface (the DurableExecutionAccess port + value types
	// in durableexecution.go) stays Temporal-free by the component's own design and
	// review — the Temporal SDK is confined to temporal.go. This list is the one and
	// only place the exception is granted; any OTHER RA/Engine/Utility importing
	// Temporal still fails the build.
	spec.TemporalExemptPackages = []string{"resourceaccess/durableexecution"}

	spec.AllowedImportPrefixes = []string{
		"github.com/mixofreality-studio/",        // archistrator-platform framework family + this app's own module
		"github.com/google/uuid",                 // sanctioned identity type (projectstate.ProjectID = uuid.UUID)
		"github.com/invopop/jsonschema",          // typed-output JSON Schema derivation for LLM prompts (manager/systemdesign/schema.go)
		"github.com/jackc/pgx",                   // sanctioned Postgres driver
		"github.com/go-git/",                     // sanctioned Git/Gitea client (go-git + go-billy)
		"go.temporal.io/",                        // sanctioned durable-execution substrate
		"github.com/modelcontextprotocol/go-sdk", // sanctioned MCP substrate for the generated mcpClient tool surface (internal/client/mcp/*, framework-go-mcp-generator output)
	}
	arch.Check(t, spec)
}

// ---------------------------------------------------------------------------
// TestGeneratedOnlyPublic — the ENCAPSULATION GATE.
// ---------------------------------------------------------------------------
//
// Founder invariant: the ONLY exported (public) symbols a component may carry
// are its GENERATED CONTRACT SURFACE plus a small, DOCUMENTED allowlist of
// legitimate exceptions. No other public code may exist in the engine,
// resourceaccess, and manager layers. This is the executable form of "tests/rules
// that no other public code can exist beyond the generated contract."
//
// For each package under internal/{engine,resourceaccess,manager}/* the test
// computes two sets and fails on the difference:
//
//	GENERATED SURFACE =
//	  (a) every exported top-level identifier declared in the package's *.gen.go
//	      files (contract.gen.go: the Manager/Engine/Access interface, the
//	      generated impl struct, the constructor, and the contract value types),
//	      PLUS
//	  (b) the TRANSITIVE CLOSURE of in-package exported TYPES structurally
//	      reachable from (a) — i.e. a hand-written type that a generated
//	      operation traffics in (a field of a generated struct, a param/result
//	      of a generated interface method or constructor) IS part of the
//	      contract surface even though it is hand-declared. This is what makes
//	      the rule honest for components whose schema generates only the port
//	      (e.g. projectstate, whose ProjectStateAccess returns the hand-owned
//	      Project aggregate), without a blanket skip.
//	  A const/var whose declared TYPE is in the surface is itself surface (enum
//	  members of a contract enum). A method is surface when it implements a
//	  generated interface method OR its receiver type is in the surface (a
//	  contract type's own behaviour, e.g. MarshalJSON).
//
//	ACTUAL SURFACE = every exported top-level identifier (and exported method on
//	  an exported type) across the package's non-test, non-gen .go files.
//
// FAIL when an actual-exported symbol is neither generated surface NOR on the
// allowlist below. The allowlist is data (per-package symbol sets) grouped by
// the only categories that genuinely cannot be generated; see allowlist comments.
// Prefer UNEXPORTING over allowlisting: the manager Temporal worker plumbing was
// unexported wholesale (only RegisterWorker/RegisterSchedules/TaskQueue cross the
// package boundary), the Deps structs were unexported, and the engines carry zero
// non-generated public surface.
func TestGeneratedOnlyPublic(t *testing.T) {
	const modPrefix = "github.com/mixofreality-studio/archistrator/server/"
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedDeps | packages.NeedImports,
		Dir:   "..", // module root relative to internal/
		Tests: false,
		Env:   append(envOff(), "GOWORK=off"),
	}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		t.Fatalf("packages.Load: %v", err)
	}
	loadErr := false
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			t.Errorf("load error in %s: %v", p.PkgPath, e)
			loadErr = true
		}
	})
	if loadErr {
		t.FailNow()
	}

	// ENCAP_DUMP=1 prints the flagged symbols per package as a Go map literal
	// (the maintenance hook used to (re)generate encapsulationAllowlistData after a
	// contract change) instead of failing. Steady-state runs leave it unset.
	dump := os.Getenv("ENCAP_DUMP") != ""
	dumped := map[string][]string{}

	for _, p := range pkgs {
		rel := strings.TrimPrefix(p.PkgPath, modPrefix)
		if !isEncapsulationTarget(rel) {
			continue
		}
		allow := encapsulationAllowlist[rel]
		genNames, genIface, seeds := genSurface(p)
		closure := typeClosure(p, seeds)
		surface := func(name string) bool {
			return genNames[name] || closure[name] || allow[name]
		}
		flag := func(kind, sym string) {
			if dump {
				dumped[rel] = append(dumped[rel], sym)
				return
			}
			t.Errorf("%s: exported %s %s is neither generated contract surface nor allowlisted "+
				"(unexport it, or add it to encapsulationAllowlistData with a category justification)", rel, kind, sym)
		}

		for i, f := range p.Syntax {
			if strings.HasSuffix(p.CompiledGoFiles[i], ".gen.go") {
				continue
			}
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if !s.Name.IsExported() {
								continue
							}
							if !surface(s.Name.Name) {
								flag("type", s.Name.Name)
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if !n.IsExported() {
									continue
								}
								if surface(n.Name) {
									continue
								}
								// A const/var whose declared type is contract surface
								// is itself contract surface (enum members).
								if tn := valueTypeName(p, n.Name); tn != "" && surface(tn) {
									continue
								}
								flag(valueKeyword(d), n.Name)
							}
						}
					}
				case *ast.FuncDecl:
					if !d.Name.IsExported() {
						continue
					}
					if d.Recv == nil {
						if !surface(d.Name.Name) {
							flag("func", d.Name.Name)
						}
						continue
					}
					recv := recvTypeName(d.Recv)
					if recv == "" || !ast.IsExported(recv) {
						continue // method on an unexported type is invisible externally
					}
					// A method is surface when it implements a generated interface
					// method, its receiver is contract surface, or it is explicitly
					// allowlisted as "Recv.Method".
					if genIface[d.Name.Name] || surface(recv) || allow[recv+"."+d.Name.Name] {
						continue
					}
					flag("method", recv+"."+d.Name.Name)
				}
			}
		}
	}

	if dump {
		paths := make([]string, 0, len(dumped))
		for k := range dumped {
			paths = append(paths, k)
		}
		sort.Strings(paths)
		for _, k := range paths {
			syms := dumped[k]
			sort.Strings(syms)
			t.Logf("\t%q: {", k)
			for _, s := range syms {
				t.Logf("\t\t%q,", s)
			}
			t.Logf("\t},")
		}
	}
}

func isEncapsulationTarget(rel string) bool {
	for _, pre := range []string{"internal/engine/", "internal/resourceaccess/", "internal/manager/"} {
		if strings.HasPrefix(rel, pre) {
			return true
		}
	}
	return false
}

// genSurface returns the exported names declared in *.gen.go files, the set of
// interface method names declared there, and the seed types to expand into the
// transitive closure.
func genSurface(p *packages.Package) (names map[string]bool, ifaceMethods map[string]bool, seeds []types.Type) {
	names = map[string]bool{}
	ifaceMethods = map[string]bool{}
	for i, f := range p.Syntax {
		if !strings.HasSuffix(p.CompiledGoFiles[i], ".gen.go") {
			continue
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if !s.Name.IsExported() {
							continue
						}
						names[s.Name.Name] = true
						if obj := p.Types.Scope().Lookup(s.Name.Name); obj != nil {
							seeds = append(seeds, obj.Type())
						}
						if it, ok := s.Type.(*ast.InterfaceType); ok && it.Methods != nil {
							for _, m := range it.Methods.List {
								for _, nm := range m.Names {
									ifaceMethods[nm.Name] = true
								}
							}
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if !n.IsExported() {
								continue
							}
							names[n.Name] = true
							if obj := p.Types.Scope().Lookup(n.Name); obj != nil {
								seeds = append(seeds, obj.Type())
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil || !d.Name.IsExported() {
					continue
				}
				names[d.Name.Name] = true
				if obj := p.Types.Scope().Lookup(d.Name.Name); obj != nil {
					seeds = append(seeds, obj.Type())
				}
			}
		}
	}
	return names, ifaceMethods, seeds
}

// typeClosure returns the set of in-package exported type names structurally
// reachable from the seed types (the data the generated contract traffics in).
func typeClosure(p *packages.Package, seeds []types.Type) map[string]bool {
	out := map[string]bool{}
	visited := map[types.Type]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		if t == nil || visited[t] {
			return
		}
		visited[t] = true
		switch x := t.(type) {
		case *types.Named:
			if obj := x.Obj(); obj != nil && obj.Pkg() == p.Types && obj.Exported() {
				out[obj.Name()] = true
			}
			walk(x.Underlying())
			for i := 0; i < x.NumMethods(); i++ {
				walk(x.Method(i).Type())
			}
		case *types.Pointer:
			walk(x.Elem())
		case *types.Slice:
			walk(x.Elem())
		case *types.Array:
			walk(x.Elem())
		case *types.Map:
			walk(x.Key())
			walk(x.Elem())
		case *types.Chan:
			walk(x.Elem())
		case *types.Struct:
			for i := 0; i < x.NumFields(); i++ {
				walk(x.Field(i).Type())
			}
		case *types.Signature:
			if x.Params() != nil {
				for i := 0; i < x.Params().Len(); i++ {
					walk(x.Params().At(i).Type())
				}
			}
			if x.Results() != nil {
				for i := 0; i < x.Results().Len(); i++ {
					walk(x.Results().At(i).Type())
				}
			}
		case *types.Interface:
			for i := 0; i < x.NumMethods(); i++ {
				walk(x.Method(i).Type())
			}
		}
	}
	for _, t := range seeds {
		walk(t)
	}
	return out
}

// valueTypeName returns the simple name of the in-package named type of a
// top-level const/var, or "" if it has none in this package.
func valueTypeName(p *packages.Package, name string) string {
	obj := p.Types.Scope().Lookup(name)
	if obj == nil {
		return ""
	}
	if named, ok := obj.Type().(*types.Named); ok {
		if named.Obj() != nil && named.Obj().Pkg() == p.Types {
			return named.Obj().Name()
		}
	}
	return ""
}

func valueKeyword(d *ast.GenDecl) string {
	if d.Tok.String() == "const" {
		return "const"
	}
	return "var"
}

func recvTypeName(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	t := fl.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func envOff() []string { return os.Environ() }

// encapsulationAllowlist is the DOCUMENTED, per-package set of exported symbols
// that are legitimately public despite not being part of the generated contract
// surface. Keys are module-relative package paths; values are the allowed
// identifier names (or "Recv.Method" for a specific method). Allowlisting a TYPE
// name implicitly allows its methods. Every entry falls into one of the
// categories documented inline. Prefer UNEXPORTING over adding entries here.
var encapsulationAllowlist = map[string]map[string]bool{}

func init() {
	for pkg, names := range encapsulationAllowlistData {
		set := make(map[string]bool, len(names))
		for _, n := range names {
			set[n] = true
		}
		encapsulationAllowlist[pkg] = set
	}
}

var encapsulationAllowlistData = map[string][]string{
	// Temporal registration entrypoints. The composition root (cmd/server/main.go) calls
	// RegisterWorker(w, m) and worker.New(tc, TaskQueue); RegisterSchedules is the startup
	// Schedule hook. The entire Temporal worker plumbing (the Workflows facade + its
	// workflow/activity methods, every *Input/*Args/*Signal payload struct, the consumer-mirror
	// seam/enum types, the workflow/signal name consts) was UNEXPORTED — only these registration
	// entrypoints cross the package boundary.
	"internal/manager/construction": {
		"RegisterWorker",
		"TaskQueue",
	},
	// Temporal registration entrypoints (see construction). RegisterSchedules registers the
	// operatedStateReconcile Schedule at startup.
	"internal/manager/operations": {
		"RegisterSchedules",
		"RegisterWorker",
		"TaskQueue",
	},
	// Temporal registration entrypoints (see construction).
	"internal/manager/projectdesign": {
		"RegisterWorker",
		"TaskQueue",
	},
	// Temporal registration entrypoints (see construction). RegisterSchedules registers the
	// shortfallSweep Schedule.
	"internal/manager/settlement": {
		"RegisterSchedules",
		"RegisterWorker",
		"TaskQueue",
	},
	// Temporal registration entrypoints (see construction).
	"internal/manager/systemdesign": {
		"RegisterWorker",
		"TaskQueue",
	},
	// FREE-FUNCTION BEHAVIOUR over the contract's named-scalar handle/enum value types: the
	// schema-first rule keeps generated contract types method-free, so
	// String/Parse/Equal/IsZero/IsTerminal behaviour lives as free funcs. Plus the package Error
	// alias (= fwra.Error).
	"internal/resourceaccess/constructionpipeline": {
		"Error",
		"ParsePipelineHandle",
		"PipelineHandleEqual",
		"PipelineHandleIsZero",
		"PipelineHandleString",
		"PipelinePhaseIsTerminal",
		"PipelinePhaseString",
		"RepoTargetIsZero",
		"StepOutcomeString",
	},
	// FREE-FUNCTION BEHAVIOUR over the ExecutionHandle/ExecutionStatus value types
	// (String/Parse/Equal/IsZero) + the package Error alias.
	"internal/resourceaccess/durableexecution": {
		"Error",
		"ExecutionHandleEqual",
		"ExecutionHandleIsZero",
		"ExecutionHandleString",
		"ExecutionStatusString",
		"ParseExecutionHandle",
	},
	// TYPED METHOD-MODEL CORPUS + GIT INFRASTRUCTURE. projectstate is the OWNER of the shared,
	// hand-written typed Method models (Glossary, System, Network, Solution, ...); its schema
	// generates only the ProjectStateAccess port, and Project stores artifact payloads as opaque
	// slots, so the models are NOT structurally reachable from the port. They are consumed
	// downward by Engines and Managers as a normal layer edge (see TestMethodLayering). This set
	// also carries the FLAGGED HAND-WRITTEN INTERFACES the spec documents: the git-backed GitStore
	// + its access ports, the JSON codec (EncodeProjectJSON/DecodeProjectJSON), the repo-locator
	// options (RepoLocator/BranchRepoLocator/ProjectCatalog/ProjectCatalogRef/LocalRepoCredential/
	// NewGitStore + GitStore.WithCatalog/WithClock), and the model derivation/factory helpers
	// (New*, Derive*, Coarse*, ...). Error = fwra.Error. This is the one package with a large
	// legitimate non-generated public surface.
	"internal/resourceaccess/projectstate": {
		"ActivityDiagram",
		"ActivityEdge",
		"ActivityItem",
		"ActivityKind",
		"ActivityList",
		"ActivityList.Kind",
		"ActivityNetwork",
		"ActivityNode",
		"ActivityNodeKind",
		"ActivityNodeKind.BookEnumerated",
		"ActivityNodeKind.MarshalJSON",
		"ActivityNodeKind.UnmarshalJSON",
		"ActivityOutcome",
		"ActivityOutcome.String",
		"ActivityOutcomeCompleted",
		"ActivityOutcomeSkipped",
		"ActivityOutcomeTakenOver",
		"ActivityOutcomeUnknown",
		"ActivityProgress",
		"Actor",
		"AllArtifactKinds",
		"ArtifactKindFromWireName",
		"Axis",
		"Axis.MarshalJSON",
		"Axis.UnmarshalJSON",
		"AxisAllCustomersAtOneTime",
		"AxisSameCustomerOverTime",
		"BranchAwareProjectStateAccess",
		"BranchRepoLocator",
		"CallEventPubSub",
		"CallMode",
		"CallMode.MarshalJSON",
		"CallMode.UnmarshalJSON",
		"CallQueued",
		"CallSync",
		"CanonicalLayer",
		"CheckFail",
		"CheckItem",
		"CheckPass",
		"CheckStatus",
		"CheckStatus.MarshalJSON",
		"CheckStatus.UnmarshalJSON",
		"CheckWaived",
		"ClassCore",
		"ClassNonCore",
		"Classification",
		"Classification.MarshalJSON",
		"Classification.UnmarshalJSON",
		"CoarseBuildStatus",
		"CoarseBuildStatusFor",
		"CoarsePhase",
		"CoarsePhaseFor",
		"CompClient",
		"CompEngine",
		"CompManager",
		"CompResource",
		"CompResourceAccess",
		"CompUtility",
		"Component",
		"ComponentID",
		"ComponentKind",
		"ComponentKind.MarshalJSON",
		"ComponentKind.UnmarshalJSON",
		"ComputeCostFlatMarkup",
		"ComputeCostKind",
		"ComputeCostTieredFloors",
		"ComputeCostUnknown",
		"ConstructionTransitionAccess",
		"ContainerInstance",
		"CoreUseCases",
		"CoreUseCases.Kind",
		"CorpusPresence",
		"CritiqueVerdictApprove",
		"CritiqueVerdictRevise",
		"DecodeProjectJSON",
		"DeliveryStyle",
		"DeliveryStyle.MarshalJSON",
		"DeliveryStyle.UnmarshalJSON",
		"DeploymentEnvironment",
		"DeploymentNode",
		"DeploymentProfile",
		"DeploymentProfile.MarshalJSON",
		"DeploymentProfile.UnmarshalJSON",
		"DeploymentTopology",
		"DeriveBuildStatus",
		"DeriveKind",
		"DeriveProduced",
		"DynamicView",
		"EdgeControlFlow",
		"EdgeGuardedFlow",
		"EdgeKind",
		"EdgeKind.MarshalJSON",
		"EdgeKind.UnmarshalJSON",
		"EncodeProjectJSON",
		"Error",
		"GitActivityConstructionAccess",
		"GitActivityStatusAccess",
		"GitConstructionTransitionAccess",
		"GitProjectStateAccess",
		"GitStore",
		"GitStore.ReadProjectOnBranch",
		"GitStore.RecordActivityArchApproved",
		"GitStore.RecordActivityBranchOpened",
		"GitStore.RecordActivityCIObserved",
		"GitStore.RecordActivityCompleted",
		"GitStore.RecordActivityExited",
		"GitStore.RecordActivityFailed",
		"GitStore.RecordActivityMerged",
		"GitStore.RecordActivityStarted",
		"GitStore.RecordChangeReviewed",
		"GitStore.RecordOperatorPaused",
		"GitStore.RecordPhaseArtifactProduced",
		"GitStore.RecordPhaseCompleted",
		"GitStore.RecordPhaseStarted",
		"GitStore.RecordServiceContractProduced",
		"GitStore.StageArtifactForReviewOnBranch",
		"GitStore.WithCatalog",
		"GitStore.WithClock",
		"Glossary",
		"Glossary.Kind",
		"GlossaryItem",
		"InfrastructureKind",
		"InfrastructureKindGoTemporalPostgres",
		"InfrastructureKindUnknown",
		"Layer",
		"Layer.MarshalJSON",
		"Layer.Rank",
		"Layer.UnmarshalJSON",
		"LayerClient",
		"LayerEngine",
		"LayerManager",
		"LayerResource",
		"LayerResourceAccess",
		"LayerUtility",
		"LocalRepoCredential",
		"MissionStatement",
		"MissionStatement.Kind",
		"Money",
		"Network",
		"Network.Kind",
		"NetworkDependency",
		"NetworkMilestone",
		"NetworkNodeCompute",
		"NetworkSummary",
		"NewGitStore",
		"NewGlossary",
		"NewMissionStatement",
		"NewModelForKind",
		"NewSolution",
		"NewSystem",
		"NewUseCase",
		"NodeAction",
		"NodeDecision",
		"NodeEnd",
		"NodeFork",
		"NodeGoto",
		"NodeInterruptEdge",
		"NodeJoin",
		"NodeLoop",
		"NodeMerge",
		"NodeNote",
		"NodeStart",
		"NodeSwimLane",
		"NodeSwitch",
		"Objective",
		"OperationalConcepts",
		"OperationalConcepts.Kind",
		"OperationalDecision",
		"OptionActivity",
		"OptionID",
		"Phase1RequiredKinds",
		"Phase2RequiredKinds",
		"PhaseArtifactPayload",
		"PlanningAssumptions",
		"PlanningAssumptions.Kind",
		"ProfileCloud",
		"ProfileLocal",
		"ProfileTest",
		"ProjectCatalog",
		"ProjectCatalogRef",
		"ProjectEarnedValue",
		"ProjectOption",
		"Relationship",
		"RepoCredential",
		"RepoCredential.IsZero",
		"RepoLocator",
		"Requirement",
		"RevenueShareKind",
		"RevenueShareLaunchFlat10",
		"RevenueShareNegotiatedRate",
		"RevenueShareUnknown",
		"RiskModel",
		"RiskModel.Kind",
		"RiskRow",
		"ScheduleDaily",
		"ScheduleKind",
		"ScheduleMonthly",
		"ScheduleUnknown",
		"ScheduleWeekly",
		"ScrubbedRequirements",
		"ScrubbedRequirements.Kind",
		"SdpOptionRow",
		"SdpReview",
		"SdpReview.Kind",
		"SettlementTerms",
		"Slug",
		"Solution",
		"Solution.Kind",
		"SolutionKinds",
		"StandardCheck",
		"StandardCheck.Kind",
		"StyleBoth",
		"StyleCloud",
		"StyleLocal",
		"System",
		"System.Kind",
		"Trigger",
		"Trigger.MarshalJSON",
		"Trigger.UnmarshalJSON",
		"TriggerBusMessage",
		"TriggerClientAction",
		"TriggerTimer",
		"UsageAssumption",
		"UseCase",
		"UseCaseDecision",
		"UseCaseID",
		"Volatilities",
		"Volatilities.Kind",
		"Volatility",
		"WorkerMix",
	},
	// FREE-FUNCTION BEHAVIOUR over the repo/ref/handle scalars
	// (String/FromString/Equal/IsZero/OwnerRepo) + the MANAGED-REPO SCAFFOLD CONTRACT
	// (paths/versions/template files) + the FLAGGED HAND-WRITTEN SourceControlCatalogAccess port
	// and ProjectRepoRef seam + Error alias.
	"internal/resourceaccess/sourcecontrol": {
		"BranchRefIsZero",
		"BranchRefString",
		"CheckStateString",
		"CommitRefIsZero",
		"CommitRefString",
		"DesignWorkflowPath",
		"Error",
		"FrameworkGoVersion",
		"GoModPath",
		"GoVersion",
		"InstallationIsZero",
		"InstallationString",
		"ManagedCommitMessage",
		"ManagedScaffoldFiles",
		"MethodTestPath",
		"ProjectRepoRef",
		"ProjectRepoRef.ProjectID",
		"PullRequestRefEqual",
		"PullRequestRefFromString",
		"PullRequestRefIsZero",
		"PullRequestRefString",
		"RepoCredentialIsZero",
		"RepoRefEqual",
		"RepoRefFromString",
		"RepoRefIsZero",
		"RepoRefOwnerRepo",
		"RepoRefString",
		"SourceControlCatalogAccess",
	},
	// Cross-package identity value types (CustomerID, OperatedAppID) consumed by downstream
	// Managers, + Error alias.
	"internal/resourceaccess/usagelog": {
		"CustomerID",
		"Error",
		"OperatedAppID",
	},
	// The generic typed-data PUBLIC API a Go interface cannot express: GenerateTypedData[T] is a
	// package-level generic function (generic methods are illegal on the WorkerAccess contract
	// interface), plus the distinct UnmarshalError it returns. + Error alias. (ReplayMode and its
	// members are already contract surface via the generated NewReplayWorkerAccess constructor.)
	"internal/resourceaccess/worker": {
		"Error",
		"GenerateTypedData",
		"UnmarshalError",
		"UnmarshalError.Error",
		"UnmarshalError.Unwrap",
	},
}
