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

// TestFileLayout enforces the 2026-07-11 layer file-layout standard (one impl
// file, one file per workflow, one test file, generated-only otherwise) —
// docs/superpowers/specs/2026-07-11-layer-file-layout-standard-design.md.
func TestFileLayout(t *testing.T) {
	arch.CheckFileLayout(t, appArchSpec())
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
		"github.com/google/jsonschema-go",        // the MCP SDK's own JSON Schema type; the generated mcpClient tools carry explicit InputSchema (enum values/meanings + REST-matching optionality) built from it (internal/client/mcp/*, cmd/clientgen mcpemit output)
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
		"RegisterManagerWorker",
		"RegisterWorker",
		"TaskQueue",
	},
	// Temporal registration entrypoints (see construction). RegisterSchedules registers the
	// operatedStateReconcile Schedule at startup.
	"internal/manager/operations": {
		"RegisterManagerWorker",
		"RegisterSchedules",
		"RegisterWorker",
		"TaskQueue",
	},
	// Temporal registration entrypoints (see construction).
	"internal/manager/projectdesign": {
		"RegisterManagerWorker",
		"RegisterWorker",
		"TaskQueue",
	},
	// Temporal registration entrypoints (see construction). RegisterSchedules registers the
	// shortfallSweep Schedule.
	"internal/manager/billing": {
		"RegisterManagerWorker",
		"RegisterSchedules",
		"RegisterWorker",
		"TaskQueue",
	},
	// Temporal registration entrypoints (see construction).
	"internal/manager/systemdesign": {
		"RegisterManagerWorker",
		"RegisterWorker",
		"TaskQueue",
	},
	// DEPLOYMENT-PROFILE VARIANT CONSTRUCTORS (step-8 fold): the composition-root policy
	// that used to live in cmd/server (buildArtifactAccess + artifact_auth.go + the dry-run
	// stub) folded into the owning package. Each assembles the generated GitArtifactAccess
	// over the satellite blob store + a profile-specific auth resolver; NewDryRunArtifactAccess
	// is the in-memory dogfood/demo stub. The auth-resolver glue itself stays unexported.
	"internal/resourceaccess/artifact": {
		"NewDryRunArtifactAccess",
		"NewGitHubCloudArtifactAccess",
		"NewGitLocalArtifactAccess",
	},
	// PERMANENT NO-OP CONSTRUCTOR (B5): revenueLedgerAccess has no infrastructure binding
	// (charge-only removed the ledger's persistence — see revenueledgeraccess.go's package
	// doc); NewRevenueLedgerAccess returns the sole, permanent no-op impl, same
	// VARIANT-CONSTRUCTOR category as artifact/agenticjob's dry-run constructors above/below.
	// The revenueLedgerAccess FACET re-folded into billingstate (Wave 1 reconciliation,
	// reversing the ea56a36 split) — one component, one Go package — so its no-op constructor
	// now lives in the billingStateAccess package alongside the generated billing surface.
	"internal/resourceaccess/billingstate": {
		"NewRevenueLedgerAccess",
	},
	// FREE-FUNCTION BEHAVIOUR over the contract's named-scalar handle/enum value types: the
	// schema-first rule keeps generated contract types method-free, so
	// String/Parse/Equal/IsZero/IsTerminal behaviour lives as free funcs. Plus the package Error
	// alias (= fwra.Error).
	"internal/resourceaccess/agenticjob": {
		// DRY-RUN VARIANT CONSTRUCTOR (step-8 fold): the in-memory dogfood/demo stub
		// folded out of cmd/server (construction_dryrun.go). The REAL GitHub-Actions
		// variant is the generated NewGitHubActionsAgenticJobAccess.
		"NewDryRunAgenticJobAccess",
		// LOCAL-EXECUTOR VARIANT CONSTRUCTOR (local-first-init-funnel Task 6, same
		// category as the dry-run stub above): headless-claude dispatch for a
		// local-profile boot with no GitHub creds. Selected by cmd/server/hooks.go's
		// FinalizeAgenticJobAccess alongside the generated GitHub-Actions
		// constructor and the dry-run stub.
		"NewLocalExecAgenticJobAccess",
		// DISPATCH-INPUT VOCABULARY (local-merge-and-policy Commit 1): the job key +
		// merge-job value the constructionManager stamps into the frozen Submit
		// surface's open DispatchInputs map to route the local-executor merge job
		// (merge activity/<id> into main + delete branch). Shared constants so the
		// Manager and this RA cannot drift on the wire strings.
		"DispatchInputJobKey",
		"DispatchJobMerge",
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
		// STEP-8 FOLD — deployment VARIANT CONSTRUCTORS + their ports (buildDesignProjectState
		// + projectstate_git_adapter.go folded in). NewGitLocal/GitHubProjectStateAccess build
		// the cred-binding adapter over *GitStore; CredentialMinter is the port the CLOUD
		// sourcecontrol-backed minter (kept at the composition root — NoSideways) satisfies;
		// GitConstructionPorts encapsulates the former composition-root private-field
		// type-assert to the git construction-transition + git-activity-status ports.
		"NewGitLocalProjectStateAccess",
		"NewGitHubProjectStateAccess",
		"CredentialMinter",
		"GitConstructionPorts",
		// designSessionAccess (B4): NewDesignSessionAccess wraps a base ProjectStateAccess,
		// running the SAME branch/ledger/provenance/reconcile capability-fallback chains the
		// design Managers' custom activities used to run inline — a facade, not a Manager-
		// consumed constructor yet, same VARIANT-CONSTRUCTOR category as the pair above.
		"NewDesignSessionAccess",
		"NewGitLocalConstructionTransitionAccess",
		"NewGitLocalGitActivityStatusAccess",
		"NewGitHubConstructionTransitionAccess",
		"NewGitHubGitActivityStatusAccess",
		"NewGitLocalDesignSessionAccess",
		"NewGitHubDesignSessionAccess",
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
		// DesignCommandFor (Plan-2 Task B1) + its DesignJobMode dispatch-shape enum: the
		// (kind, mode, addressee) → .claude slash-command name mapping the design Managers
		// need to dispatch draft/critique/answer jobs. Same category as CommandFor above —
		// a total, side-effect-free function of already-public projectstate enum values plus
		// the new DesignJobMode wire concept; its supporting designKindSlug/
		// designKindHasCritique stay unexported (see designcommand.go / commandfor.go
		// precedent).
		"DesignCommandFor",
		"DesignJobMode",
		"DesignJobModeAnswer",
		"DesignJobModeCritique",
		"DesignJobModeDraft",
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
		// PROMOTED ENVELOPE CODEC (designSessionAccess, B4): the Manager-Temporal-boundary
		// wire discipline for the sealed ArtifactModel sum + the head-state Project
		// aggregate, moved down from the near-duplicate codec.go the projectdesign and
		// systemdesign Managers each carried. Consumed downward by those two Managers
		// (a normal RA→Manager layer edge, same category as the typed Method model corpus
		// above) via type aliases + these exported constructors/methods.
		"EncodeModel",
		"EncodeProject",
		"EncodeProjectJSON",
		// PROMOTED CO-AUTHOR HELPERS (code-health-phase-bd task D3): the PURE reason/label
		// formatters, review-ledger predicate, and rail resolvers the two design Managers
		// (systemdesign, projectdesign) each carried as byte-identical duplicates, promoted
		// here per the SAME PROMOTED-CODEC precedent as EncodeModel above. The
		// git-rail/session/dispatch/recovery workflow ORCHESTRATION stays per-manager (Temporal
		// lives only in the Manager layer; workflow.Context funcs are forbidden outside
		// Manager) — only helpers with no Temporal SDK dependency, no RA→RA sideways
		// dependency, and no per-manager generated-type param were promotable. designRepoTarget
		// stays per-manager (it needs sibling RAs agenticjob/sourcecontrol — a
		// forbidden RA→RA sideways import); dispatchErrSummary and its 4 dependents stay
		// per-manager (they need go.temporal.io/sdk/temporal.ApplicationError); and
		// designArchApprovalBody/openReviewCommentViewIDs/checkCommentTransition/
		// reviewThreadToView stay per-manager (they traffic in each Manager's own generated
		// ArtifactKind/ReviewCommentView contract types). Consumed downward by both Managers
		// (a normal RA→Manager layer edge, same category as the codec above).
		"AmendmentIndexFor",
		"AmendmentNoChangeReason",
		"DesignBranch",
		"OpenReviewCommentIDs",
		"ReadBackDecodeFailedReason",
		"SameArtifactModel",
		"Error",
		"GitActivityStatusAccess",
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
		// RECONCILE (F80): the two deterministic single-writer-per-slot resolver free-funcs (a
		// diverged session branch vs main) behind ReconcileBranchFromMain (now a required
		// ProjectStateAccess op, C2 fold code-health-phase-a). Consumed by the
		// cmd/aiarch-state-mcp `reconcile` subcommand and the server git adapter.
		"ReconcileSlotOntoBase",
		"OverlaySlotFromBranchOntoMain",
		// F81 GATE 0: the raw-JSON required-field presence pass demanding every closed-enum /
		// identity field on a drafted model (component id/name/kind/layer, relationship
		// from/to/mode, dynamic-view useCaseId, …). Consumed by cmd/aiarch-state-mcp
		// (putDraftModel) AND the server read-back (decodeSlotsMap) so write ≡ read-back.
		"RequireModelFields",
		// CONSTRUCTION-VERB ROUTING CORE: the pure, I/O-free router of a phase-artifact /
		// testing-state payload into the Project aggregate — the shared core of the RA's
		// RecordPhaseArtifactProduced, exported so the cmd/aiarch-state-mcp construction verbs
		// (recordPhaseArtifact/recordTestingState) reuse the SAME routing (one source of truth
		// for which payload field lands in which slot). Same category as RequireModelFields
		// above (a pure helper the MCP binary shares with the server read/write path).
		"ApplyPhaseArtifactPayload",
		// INTERNAL MCP TOOL SURFACE (agentic-managers spec item 3): the generated
		// ResourceAccess/Engine tool catalog (toolcatalog.gen.go — NOT port-reachable, so it
		// needs an explicit entry like the System model types) + its hand-written accessors.
		// projectstate OWNS the contract corpus + System model this surface derives from, the
		// same category as the CommandFor/DeriveKind derivation helpers above.
		"InternalTool",
		"InternalToolCatalog",
		"AgentExposableTools",
		"InternalToolByName",
		"LocalRepoCredential",
		"MissionStatement",
		"MissionStatement.Kind",
		"ModelEnvelope",
		"ModelEnvelope.Decode",
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
		"ProjectEnvelope",
		"ProjectEnvelope.Decode",
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
		// REVIEW-LEDGER status vocabulary — the closed wire values of a durable ReviewComment's
		// status (the CritiqueVerdictApprove/Revise precedent above). Plain-string consts owned
		// here; the ReviewComment type itself is generated contract surface.
		"ReviewCommentAddressed",
		"ReviewCommentOpen",
		"ReviewCommentWaived",
		// QUESTION-COMMENTS vocabulary + behavior — the closed type/addressee wire values of a
		// durable ReviewComment plus the pure classification helpers over them. Plain-string
		// consts + funcs owned here (same category as the status vocabulary above); the
		// ReviewComment.Type/Addressee fields themselves are generated contract surface.
		// ReviewCommentID is exposed so the AskQuestions dispatch can predict the ids a fresh
		// append will mint (to name each question in the answer-job prompt).
		"ReviewCommentTypeChangeRequest",
		"ReviewCommentTypeQuestion",
		"ReviewCommentTypeStaleAck",
		"ReviewAddresseePM",
		"ReviewAddresseeArchitect",
		"ReviewCommentIsQuestion",
		"ReviewCommentBlocksApprove",
		"ReviewCommentID",
		// FACTORY FREE FUNCTION: ReviewPolicyFromGateIDs converts the webApp PolicyPanel's
		// ad-hoc gate-id vocabulary (e.g. "svc-contract") into the canonical ReviewPolicy
		// value stored in head-state. It is the client-facing constructor for ReviewPolicy
		// and must be exported for the client layer (cmd/server/construction_dryrun.go and
		// generated web handlers) to call. ReviewPolicy itself is contract surface via the
		// Project aggregate's ReviewPolicy field; only the constructor free-func needs
		// allowlisting.
		"ReviewPolicyFromGateIDs",
		// REVIEW-PRESET vocabulary + non-overridable-floor helpers (Task 7, local-first
		// sophistication dial). ReviewPresetVibes/Checkpoints/Full are the closed
		// ReviewPolicy.Preset wire values (same category as the ReviewComment status
		// vocabulary above — plain-string consts owned here; ReviewPolicy.Preset itself
		// is generated contract surface). ContractTouchesReviewFloor is the pure
		// classification helper the construction Manager's snapshot (constructactivity.go's
		// loadReviewSnapshot) calls to seed the floor — exported because it is invoked
		// from internal/manager/construction, a different package.
		"ReviewPresetVibes",
		"ReviewPresetCheckpoints",
		"ReviewPresetFull",
		"ContractTouchesReviewFloor",
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
		"SlotEnvelope",
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
	// DEPLOYMENT-PROFILE VARIANT CONSTRUCTORS (step-8 A2 composegen seam): the two
	// no-arg, no-error profile wrappers (Real/Local) the generated composition root
	// calls per the operatedRuntimeAccess binding. Thin wrappers over the generated
	// NewProfiledOperatedRuntimeAccess; the profiled ctor + its RuntimeProfile/
	// RuntimeConfig params are structurally reachable from the generated surface, so
	// only these two new free functions need listing.
	"internal/resourceaccess/operatedruntime": {
		"NewLocalOperatedRuntimeAccess",
		"NewRealOperatedRuntimeAccess",
	},
	// FREE-FUNCTION BEHAVIOUR over the repo/ref/handle scalars
	// (String/FromString/Equal/IsZero/OwnerRepo) + the MANAGED-REPO SCAFFOLD CONTRACT
	// (paths/versions/template files) + the FLAGGED HAND-WRITTEN CatalogAccess port
	// and ProjectRepoRef seam + Error alias.
	"internal/resourceaccess/sourcecontrol": {
		// VARIANT CONSTRUCTOR (step-8 fold): buildSourceControl folded out of cmd/server.
		// Returns both published surfaces (catalog + generated interface) over the shared
		// *fwgithub.AppClient satellite, folding the catalog type-assertion into the package.
		"NewGitHubSourceControl",
		// LOCAL-GIT VARIANT (F-R3 local design PR rail): the GitLocal realisation of the
		// SourceControlAccess contract + its pure project→RepoRef resolver the composition-root
		// Repo resolvers use. Same GitLocal-variant category as projectstate's
		// NewGitLocalDesignSessionAccess.
		"NewGitLocalSourceControlAccess",
		"GitLocalRepoRefForProject",
		"BranchRefIsZero",
		"BranchRefString",
		"CheckStateString",
		"CommitRefIsZero",
		"CommitRefString",
		// The managed-scaffold SYNC surface (sync-on-dispatch, 2026-07-06): the single
		// seat/sync rendering of the design workflow + the drift-converge helper the
		// design Managers run before every design-job dispatch.
		"DesignWorkflowFile",
		"DesignWorkflowPath",
		"Error",
		// (GoVersion / FrameworkGoVersion were deleted with the B4 methodassets
		// delegation — the seated go.mod pins are module-owned now.)
		"GoModPath",
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
		"RailAppSlug",
		"RepoCredentialIsZero",
		"RepoRefEqual",
		"RepoRefFromString",
		"RepoRefIsZero",
		"RepoRefOwnerRepo",
		"RepoRefString",
		"CatalogAccess",
		// The managed-scaffold pins for the local project-state MCP server the DESIGN
		// workflow `go install`s (agentic-managers spec §Construction application).
		"StateMcpModulePath",
		"StateMcpModulePin",
		// SyncManagedScaffold: the managed-scaffold sync entry point (see
		// DesignWorkflowFile above) — converge the seated design workflow onto the
		// current template rendering before a design-job dispatch.
		"SyncManagedScaffold",
	},
	// Cross-package identity value types (CustomerID, OperatedAppID) consumed by downstream
	// Managers, + Error alias. NewNoOpUsageAccess is the LOCAL-PROFILE VARIANT CONSTRUCTOR
	// (Task 2, local-first-init-funnel): the local deployment binding declares infra: []
	// for usageAccess (no Postgres in local mode — metering is a cloud-only concern), so
	// there is no generated New<Infra><Component> for it; same VARIANT-CONSTRUCTOR category
	// as artifact/agenticjob's dry-run constructors and revenueledger's permanent
	// no-op above.
	"internal/resourceaccess/usage": {
		"CustomerID",
		"Error",
		"NewNoOpUsageAccess",
		"OperatedAppID",
	},
	// NewNoOpOperatedSystemStateAccess is the LOCAL-PROFILE VARIANT CONSTRUCTOR
	// (local-first-init-funnel Task 2b): the local deployment binding declares
	// infra: [] for operatedSystemStateAccess (no Postgres in local mode — deploy/
	// operate is a cloud-only, paid-tier concern), so there is no generated
	// New<Infra><Component> for it; same VARIANT-CONSTRUCTOR category as
	// usage.NewNoOpUsageAccess above.
	"internal/resourceaccess/operatedsystemstate": {
		"NewNoOpOperatedSystemStateAccess",
	},
}
