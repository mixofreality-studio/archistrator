// Package web is the webClient component of the archistrator server's CLIENT
// layer — the human-operator web entry surface (designs/aiarch codename;
// implementation/contracts/webClient.md, FROZEN). It is an IN-PROCESS Go Client:
// per request it (1) accepts HTTP/JSON behind the auth middleware (which has
// already validated the access token into a principal on the context), (2)
// authorizes the intent via the security Utility (Authorize), (3) translates to
// the Manager's typed input, (4) invokes EXACTLY ONE Manager workflow op (or
// signal/query), and (5) shapes the reply.
//
// LAYER RULES ([[the-method-layers]]; webClient.md §0 / "Layer interaction
// notes"):
//   - The Client imports NO Temporal. The start/signal/query invocation is the
//     in-process MECHANISM of the "call one Manager" edge — realized here by
//     calling the systemDesignManager's typed Go ops directly (the Manager owns
//     Temporal internally). The Client depends on a NARROW port (SystemDesignEntry)
//     that is a subset of the Manager surface, so this package never touches
//     go.temporal.io and the arch checker (TestMethodLayering) stays green.
//   - The auth middleware validates the access token into a principal before any
//     handler runs; the Client then calls the security Utility (Authorize) before
//     every Manager call and rejects on Deny — webClient.md §0 "consequence for
//     the metrics" + "Layer interaction notes".
//   - The Client holds NO business logic, NO state, NO cross-Manager sequencing.
//
// UC1 SCOPE (this build): only the facet that routes to systemDesignManager is
// wired. The frozen IWebEntry contract has four activity facets
// (driveDesignPhase, superviseConstruction, operateDeliveredSystem,
// managePaymentLifecycle); the latter three route to Managers not yet built
// (constructionManager, operationsManager, settlementManager) and are NOT wired
// here. They are noted as follow-ups in the C-CW completion log.
//
// Transport is hidden behind business intents in the contract, but this is the
// HTTP binding: the JSON DTOs and route table live here (handlers.go, dto.go),
// the SSE-streaming-draft option and status-polling-vs-push are internal choices
// (webClient.md convention §4 "Transport is hidden").
package web

import (
	"context"

	fwm "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
)

// SystemDesignEntry is the NARROW port the webClient depends on for the UC1
// driveDesignPhase facet — the subset of systemDesignManager's public surface the
// Phase-1 web intents relay to (webClient.md §"Op → Manager fan-out map"). The
// concrete *systemdesign.Manager satisfies it; depending on the interface (not
// the concrete Manager) keeps the Client decoupled and is what lets the arch
// checker confirm the Client imports no Temporal — the Manager's Temporal client
// stays behind this seam.
//
// Each method maps 1:1 to a frozen systemDesignManager op (systemDesignManager.md
// §2): StartSystemDesign (Workflow), RequestArtifactDraft (Workflow),
// SubmitReviewDecision (Signal), AdvancePhase (Workflow), GetSessionState (Query).
type SystemDesignEntry interface {
	StartSystemDesign(ctx context.Context, projectID systemdesign.ProjectID) (systemdesign.SessionRef, error)
	SetResearchInput(ctx context.Context, projectID systemdesign.ProjectID, research systemdesign.ResearchInput) (systemdesign.Version, error)
	RequestArtifactDraft(ctx context.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, feedback *systemdesign.ReviewFeedback) (systemdesign.SessionRef, error)
	SubmitReviewDecision(ctx context.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, decision systemdesign.ReviewDecision, feedback *systemdesign.ReviewFeedback) error
	AdvancePhase(ctx context.Context, projectID systemdesign.ProjectID) (systemdesign.PhaseAdvanceResult, error)
	GetSessionState(ctx context.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind) (systemdesign.SessionStateView, error)
}

// compile-time proof the concrete Manager satisfies the narrow Client port. If
// the frozen Manager surface drifts, this breaks the build.
var _ SystemDesignEntry = (*systemdesign.Manager)(nil)

// ProjectDesignEntry is the NARROW port the webClient depends on for the UC2
// driveDesignPhase (Phase-2) facet — the subset of projectDesignManager's public
// surface the Phase-2 web intents relay to (projectDesignManager.md §2/§5.1). The
// concrete *projectdesign.Manager satisfies it; depending on the interface keeps
// the Client decoupled and Temporal-free (the Manager owns Temporal behind this
// seam). Each method maps 1:1 to a frozen projectDesignManager op:
// RequestArtifactDraft (Workflow), SubmitReviewDecision (per-artifact Signal,
// OQ-3), RequestSDPCommit (Workflow), SubmitSDPDecision (Signal),
// AdvanceToConstruction (Workflow), GetSessionState (Query).
type ProjectDesignEntry interface {
	RequestArtifactDraft(ctx context.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, feedback *projectdesign.ReviewFeedback) (projectdesign.SessionRef, error)
	SubmitReviewDecision(ctx context.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, decision projectdesign.ReviewDecision, feedback *projectdesign.ReviewFeedback) error
	RequestSDPCommit(ctx context.Context, projectID projectdesign.ProjectID) (projectdesign.SessionRef, error)
	SubmitSDPDecision(ctx context.Context, projectID projectdesign.ProjectID, decision projectdesign.SDPDecision, optionID *projectdesign.OptionID, feedback *projectdesign.ReviewFeedback) error
	AdvanceToConstruction(ctx context.Context, projectID projectdesign.ProjectID) (projectdesign.PhaseAdvanceResult, error)
	GetSessionState(ctx context.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind) (projectdesign.SessionStateView, error)
}

// compile-time proof the concrete Phase-2 Manager satisfies the narrow Client port.
var _ ProjectDesignEntry = (*projectdesign.Manager)(nil)

// ProjectEntry is the NARROW port the webClient depends on for the project CATALOG
// facet — the subset of projectManager's public surface the catalog routes relay
// to (projectManager.md; Task 2.4/2.5). Unlike the two phase ports above this one
// fronts a degenerate, Temporal-FREE Manager (the project head-state catalog), so
// the catalog handlers carry no co-authoring/workflow concerns. Each method maps
// 1:1 to a projectManager op: CreateProject (birth a project), ListProjects (the
// owner's landing-grid catalog), GetProject (the full typed head-state read).
//
// The owner-scoping rule lives here at the Client edge: CreateProject/ListProjects
// take the OwnerScope the Client derives from the authenticated principal, so a
// project is owned by its creator and the catalog is per-owner — the projectId is
// never an owner-supplied body field.
type ProjectEntry interface {
	CreateProject(ctx context.Context, owner project.OwnerScope, name string) (project.ProjectID, error)
	ListProjects(ctx context.Context, owner project.OwnerScope) ([]project.ProjectSummary, error)
	GetProject(ctx context.Context, projectID project.ProjectID) (project.ProjectState, error)
}

// compile-time proof the concrete projectManager satisfies the narrow Client port.
var _ ProjectEntry = (*project.Manager)(nil)

// ConstructionEntry is the NARROW port the webClient depends on for the UC3
// superviseConstruction facet — the subset of constructionManager's public surface
// the operations console relays to (constructionManager.md §2). The concrete
// *construction.Manager satisfies it; depending on the interface keeps the Client
// decoupled and Temporal-free (the Manager owns Temporal behind this seam). Each
// method maps 1:1 to a frozen constructionManager op:
//   - GetSessionState    (Query  sessionState)        — the tracker/interventions/artifacts read
//   - PauseProject       (Signal operatorPauseRequested)
//   - OverrideActivity   (Signal operatorOverride)
//   - ExecuteNextActivity (Workflow constructionPumpNextActivity) — the console's
//     manual "Begin construction" fires ONE pump tick, which self-cascades over the
//     committed network (ContinueAsNew). Until the schedulerClient lands (the 30s
//     Schedule seam gap), this hand-trigger is the only way to start the pump; the
//     console relays it fire-and-forget (the cascade drains asynchronously, the SPA
//     polls the project read for the per-activity status as it advances).
type ConstructionEntry interface {
	GetSessionState(rc fwm.Context, projectID construction.ProjectID, activityID *construction.ActivityID) (construction.ConstructionSessionView, error)
	PauseProject(rc fwm.Context, projectID construction.ProjectID, reason string) error
	OverrideActivity(rc fwm.Context, projectID construction.ProjectID, activityID construction.ActivityID, override construction.ActivityOverride) error
	ExecuteNextActivity(rc fwm.Context, projectID construction.ProjectID, tickID string) (construction.PumpResult, error)
}

// compile-time proof the concrete constructionManager satisfies the narrow Client port.
var _ ConstructionEntry = (*construction.Manager)(nil)

// OperationsEntry is the NARROW port the webClient depends on for the UC4
// operateDeliveredSystem facet — the subset of operationsManager's public surface
// the operate intents relay to (webClient.md §"Op → Manager fan-out map", UC4 row;
// operationsManager.md §2). The concrete *operations.Manager satisfies it; depending
// on the interface keeps the Client decoupled and Temporal-free (the Manager owns
// Temporal behind this seam). The five operate intents fold onto THREE Manager ops:
//   - DeployAfterConstruction (Workflow) — serves Deploy + Scale + UpdateAutoscalerPolicy
//     (a desired-state republish parameterized by patch; webClient.md §"Factoring
//     decisions", operationsManager.md §2.1/§2.6)
//   - WithdrawSystem          (Workflow) — terminal withdraw
//   - QueryCostProjection     (Workflow) — read-only, side-effect-free
//
// ReconcileOperatedState (Schedule-triggered) and ApplyDelinquencyPolicy
// (cross-Manager Signal) are NOT on this Client port — reconcile enters via the
// scheduler, delinquency via settlementManager, neither via the human web console.
//
// CONTRACT 2 — IWebReadModel (operationsRead-ruling.md §C). The operations-console
// READ facet adds QueryOperatedSystemView (op readOperatedSystemView) — the composing
// read view that backs U-SPA-4. It is the FIRST op of the second webClient contract
// (IWebReadModel); IWebEntry stays frozen at 4 ops. The narrow port carries it as one
// more 1:1 method.
type OperationsEntry interface {
	DeployAfterConstruction(rc fwm.Context, operatedAppID operations.OperatedAppID, change operations.DesiredStateChange) (operations.DeployResult, error)
	WithdrawSystem(rc fwm.Context, operatedAppID operations.OperatedAppID, changeID string, reason operations.WithdrawReason) (operations.WithdrawResult, error)
	QueryCostProjection(rc fwm.Context, operatedAppID operations.OperatedAppID, requestID string, points *operations.ScaleWhatIfPoints) (operations.CostProjection, error)
	QueryOperatedSystemView(rc fwm.Context, operatedAppID operations.OperatedAppID, requestID string) (operations.OperatedSystemView, error)
}

// compile-time proof the concrete operationsManager satisfies the narrow Client port.
var _ OperationsEntry = (*operations.Manager)(nil)

// Client is the webClient — the in-process HTTP entry edge for the design phases,
// the UC3 construction console, and the UC4 operations console. It holds the narrow
// systemDesignManager + projectDesignManager + projectManager + constructionManager +
// operationsManager ports and the security Utility, nothing else (no state, no
// business logic). The HTTP handlers (handlers.go, projectdesign.go, construction.go,
// operations.go) bind to this.
type Client struct {
	systemDesign  SystemDesignEntry
	projectDesign ProjectDesignEntry
	project       ProjectEntry
	construction  ConstructionEntry
	operations    OperationsEntry
	security      security.Security

	// repoBase is the project-wide construction-repo base (<host>/<owner>/<repo>)
	// composed ONCE at the composition root from the construction-repo config the
	// server already holds (cmd/server/main.go: cfg.ConstructionRepoOwner /
	// cfg.ConstructionRepoName + host). It is used ONLY by the git-row read projection
	// to compose each row's clickable prUrl as <repoBase>/pull/<opaqueRef>
	// (D-PA-GIT-PRURL-ruling R1). It is a Client-held CONFIG value — NOT a
	// Client→ResourceAccess call, NOT a read of the projectStateAccess store, NOT an
	// aggregate field; the durable git head-state stays provider-opaque. Empty when the
	// construction repo is unconfigured (the nil-pipeline empty-session state), in which
	// case prUrl is simply omitted (no fabricated host).
	repoBase string
}

// NewClient constructs the webClient over the systemDesignManager (Phase 1) +
// projectDesignManager (Phase 2) + projectManager (catalog) + constructionManager
// (Phase 3 / UC3) + operationsManager (UC4) entry ports and the security Utility.
//
// repoBase is the project-wide construction-repo base the git-row read projection
// composes prUrl from (D-PA-GIT-PRURL-ruling). Pass "" when the construction repo is
// unconfigured — the projection then omits prUrl (additive, no behavior change for any
// other surface).
func NewClient(systemDesign SystemDesignEntry, projectDesign ProjectDesignEntry, projectCatalog ProjectEntry, constructionMgr ConstructionEntry, operationsMgr OperationsEntry, sec security.Security, repoBase string) *Client {
	return &Client{
		systemDesign:  systemDesign,
		projectDesign: projectDesign,
		project:       projectCatalog,
		construction:  constructionMgr,
		operations:    operationsMgr,
		security:      sec,
		repoBase:      repoBase,
	}
}
