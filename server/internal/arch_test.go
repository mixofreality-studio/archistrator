package internal_test

import (
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
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
	arch.Check(t, appArchSpec())
}

// appArchSpec is the app's Method layer model — the SINGLE source of truth shared by
// the standalone layer/encapsulation tests here AND the full methodcheck.Check gate
// in method_design_test.go (so the app validates its architecture ONE way, not two
// divergent ways). It is arch.MethodSpec plus the two app-specific relaxations: the
// durable-execution Temporal exemption and the operator-curated dependency allowlist.
func appArchSpec() arch.Spec {
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
	return spec
}

// TestGeneratedOnlyPublic — the ENCAPSULATION GATE — delegates to the framework
// checker arch.CheckGeneratedSurface, which was PORTED VERBATIM from the app's own
// former hand-rolled implementation. Delegating means the app and every downstream
// consumer run ONE encapsulation gate, not two divergent copies (fix-right per the
// v0.4.0 platform release). The founder invariant is unchanged: the only exported
// symbols a generated-contract package (one carrying a *.gen.go file) may carry are
// its generated contract surface, the transitive closure of types it traffics in,
// and the documented per-package allowlist below.
func TestGeneratedOnlyPublic(t *testing.T) {
	arch.CheckGeneratedSurface(t, appArchSpec(), encapAllowlist())
}

// encapAllowlist adapts encapsulationAllowlistData's self-documenting module-relative
// keys ("internal/engine/review") to the ModulePrefix-relative keys the framework
// checker expects ("engine/review") — appArchSpec's ModulePrefix already ends in
// "internal/". The SAME adapted map feeds both this test and method_design_test.go's
// full methodcheck.Check gate via ProjectSpec.EncapsulationAllowlist.
func encapAllowlist() map[string][]string {
	out := make(map[string][]string, len(encapsulationAllowlistData))
	for k, v := range encapsulationAllowlistData {
		out[strings.TrimPrefix(k, "internal/")] = v
	}
	return out
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
	"internal/manager/billing": {
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
		// PURE DERIVATION HELPERS over projectstate's own owned types (ActivityType,
		// TestingVariant, ActivityMethodPhase), shared downward with Managers per the
		// normal RA→Manager layer edge — not service-contract operations, since they
		// touch no resource. ClassifyType (corpusderive.go) is the classification rule a
		// Manager view-model needs to turn a Phase-2 activity row's coding/workerClass/
		// service-contract signals into a canonical ActivityType (systemdesign/catalog.go).
		// CommandFor + its supporting profileSlug (kept unexported, see commandfor.go) is
		// the (type, variant, phase) → .claude slash-command name mapping the construction
		// Manager needs to dispatch the right command for an activity (construction/
		// adapters.go). Both are total, side-effect-free functions of already-public
		// projectstate enum values; there is nothing to generate a contract op for.
		"ClassifyType",
		"CoarseBuildStatus",
		"CoarseBuildStatusFor",
		"CoarsePhase",
		"CoarsePhaseFor",
		"CommandFor",
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
		"DeployContainer",
		"DeploymentEnvironment",
		"DeploymentNode",
		"DeploymentProfile",
		"DeploymentProfile.MarshalJSON",
		"DeploymentProfile.UnmarshalJSON",
		"DeploymentTopology",
		"DeriveBuildStatus",
		"DeriveKind",
		"DeriveProduced",
		"DeriveType",
		"DeriveVariant",
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
		"InfrastructureNode",
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
		"Profile",
		"Profile.PhaseIDs",
		"ProfileCloud",
		"ProfileFor",
		"ProfileLocal",
		"ProfilePhase",
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
		// FACTORY FREE FUNCTION: ReviewPolicyFromGateIDs converts the webApp PolicyPanel's
		// ad-hoc gate-id vocabulary (e.g. "svc-contract") into the canonical ReviewPolicy
		// value stored in head-state. It is the client-facing constructor for ReviewPolicy
		// and must be exported for the client layer (cmd/server/construction_dryrun.go and
		// generated web handlers) to call. ReviewPolicy itself is contract surface via the
		// Project aggregate's ReviewPolicy field; only the constructor free-func needs
		// allowlisting.
		"ReviewPolicyFromGateIDs",
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
		"SoftwareSystemInstance",
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
	"internal/resourceaccess/usage": {
		"CustomerID",
		"Error",
		"OperatedAppID",
	},
}
