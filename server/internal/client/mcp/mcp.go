// Package mcp is the mcpClient component of the archistrator server's CLIENT
// layer — the agentic MCP entry surface (designs/aiarch; service-contract.json,
// FROZEN). It is an IN-PROCESS Go Client: per request it (1) resolves the
// principal from the MCP host's edge-trusted claims, (2) authorizes the intent
// via the security Utility (Authorize), (3) translates to one Manager's typed
// input, (4) invokes EXACTLY ONE Manager op (start/signal/query), and (5) shapes
// the reply as an MCP tool result.
//
// LAYER RULES ([[the-method-layers]]; service-contract §"Layer interaction notes"):
//   - The Client imports NO Temporal. The start/signal/query invocation is the
//     in-process MECHANISM of the "call one Manager" edge — realized here by
//     calling the Manager's typed Go ops directly (the Manager owns Temporal
//     internally). This package never touches go.temporal.io and the arch checker
//     (TestMethodLayering) stays green.
//   - The Client calls the security Utility (Authorize) before every Manager call
//     and rejects on Deny (fail-closed: security error → non-retryable tool error).
//   - The Client holds NO business logic, NO state, NO cross-Manager sequencing.
//
// THREE OPS (service-contract.json):
//   - ListTools      : pure read; projects frozen Manager OpenAPI vocabulary into
//     MCP tool descriptors; calls no Manager; idempotent.
//   - InvokeTool     : authorizes, resolves one target Manager op from the
//     projection table, relays the call; each invocation routes to exactly one Manager.
//   - ReadProjectState: agent continuity read; routes to one Manager read op by
//     view (getProject / getSessionState / queryCostProjection); mutates nothing.
//
// Transport (MCP stdio/HTTP) is hidden behind these three ops and is NOT part of
// this package — the caller (the composition root) owns the transport binding.
package mcp

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/settlement"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
)

// ProjectEntry is the NARROW port the mcpClient depends on for project catalog
// ops — the subset of projectManager's public surface the catalog tools relay to.
// Each method maps 1:1 to a frozen projectManager op.
type ProjectEntry interface {
	CreateProject(ctx context.Context, owner project.OwnerScope, name string) (project.ProjectID, error)
	ListProjects(ctx context.Context, owner project.OwnerScope) ([]project.ProjectSummary, error)
	GetProject(ctx context.Context, projectID project.ProjectID) (project.ProjectState, error)
}

var _ ProjectEntry = (*project.Manager)(nil)

// SystemDesignEntry is the NARROW port the mcpClient depends on for system design
// (Phase 1) ops — the subset of systemDesignManager's public surface the design
// tools relay to.
type SystemDesignEntry interface {
	StartSystemDesign(ctx context.Context, projectID systemdesign.ProjectID) (systemdesign.SessionRef, error)
	SetResearchInput(ctx context.Context, projectID systemdesign.ProjectID, research systemdesign.ResearchInput) (systemdesign.Version, error)
	RequestArtifactDraft(ctx context.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, feedback *systemdesign.ReviewFeedback) (systemdesign.SessionRef, error)
	SubmitReviewDecision(ctx context.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind, decision systemdesign.ReviewDecision, feedback *systemdesign.ReviewFeedback) error
	AdvancePhase(ctx context.Context, projectID systemdesign.ProjectID) (systemdesign.PhaseAdvanceResult, error)
	GetSessionState(ctx context.Context, projectID systemdesign.ProjectID, kind systemdesign.ArtifactKind) (systemdesign.SessionStateView, error)
}

var _ SystemDesignEntry = (*systemdesign.Manager)(nil)

// ProjectDesignEntry is the NARROW port the mcpClient depends on for project
// design (Phase 2) ops — the subset of projectDesignManager's public surface the
// design tools relay to.
type ProjectDesignEntry interface {
	RequestArtifactDraft(ctx context.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, feedback *projectdesign.ReviewFeedback) (projectdesign.SessionRef, error)
	SubmitReviewDecision(ctx context.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind, decision projectdesign.ReviewDecision, feedback *projectdesign.ReviewFeedback) error
	RequestSDPCommit(ctx context.Context, projectID projectdesign.ProjectID) (projectdesign.SessionRef, error)
	SubmitSDPDecision(ctx context.Context, projectID projectdesign.ProjectID, decision projectdesign.SDPDecision, optionID *projectdesign.OptionID, feedback *projectdesign.ReviewFeedback) error
	AdvanceToConstruction(ctx context.Context, projectID projectdesign.ProjectID) (projectdesign.PhaseAdvanceResult, error)
	GetSessionState(ctx context.Context, projectID projectdesign.ProjectID, kind projectdesign.ArtifactKind) (projectdesign.SessionStateView, error)
}

var _ ProjectDesignEntry = (*projectdesign.Manager)(nil)

// ConstructionEntry is the NARROW port the mcpClient depends on for construction
// (Phase 3) ops — the subset of constructionManager's public surface the
// construction tools relay to.
type ConstructionEntry interface {
	GetSessionState(ctx context.Context, projectID construction.ProjectID, activityID *construction.ActivityID) (construction.ConstructionSessionView, error)
	PauseProject(ctx context.Context, projectID construction.ProjectID, reason string) error
	OverrideActivity(ctx context.Context, projectID construction.ProjectID, activityID construction.ActivityID, override construction.ActivityOverride) error
	ExecuteNextActivity(ctx context.Context, projectID construction.ProjectID, tickID string) (construction.PumpResult, error)
}

var _ ConstructionEntry = (*construction.Manager)(nil)

// OperationsEntry is the NARROW port the mcpClient depends on for operations (UC4)
// ops — the subset of operationsManager's public surface the operate tools relay to.
type OperationsEntry interface {
	DeployAfterConstruction(ctx context.Context, operatedAppID operations.OperatedAppID, change operations.DesiredStateChange) (operations.DeployResult, error)
	WithdrawSystem(ctx context.Context, operatedAppID operations.OperatedAppID, changeID string, reason operations.WithdrawReason) (operations.WithdrawResult, error)
	QueryCostProjection(ctx context.Context, operatedAppID operations.OperatedAppID, requestID string, points *operations.ScaleWhatIfPoints) (operations.CostProjection, error)
	QueryOperatedSystemView(ctx context.Context, operatedAppID operations.OperatedAppID, requestID string) (operations.OperatedSystemView, error)
}

var _ OperationsEntry = (*operations.Manager)(nil)

// BillingEntry is the NARROW port the mcpClient depends on for billing lifecycle
// ops — the subset of settlementManager's public surface the billing tools relay
// to. The concrete *settlement.Manager satisfies it structurally.
type BillingEntry interface {
	OnboardPaymentIntegration(ctx context.Context, deployedAppID settlement.DeployedAppID) (settlement.SettlementRef, error)
}

var _ BillingEntry = (*settlement.Manager)(nil)

// Client is the mcpClient — the in-process MCP entry surface for the agentic
// channel. It holds the six narrow Manager ports and the security Utility, nothing
// else (no state, no business logic, no transport). The three ops (ListTools,
// InvokeTool, ReadProjectState) bind to this and are called by the MCP server
// transport adapter (stdio/HTTP) in the composition root.
type Client struct {
	project       ProjectEntry
	systemDesign  SystemDesignEntry
	projectDesign ProjectDesignEntry
	construction  ConstructionEntry
	operations    OperationsEntry
	billing       BillingEntry
	security      security.Security
}

// NewClient constructs the mcpClient over the six Manager entry ports and the
// security Utility. The transport adapter (MCP stdio/HTTP, owned by the composition
// root) calls the three ops on this Client.
func NewClient(
	projectMgr ProjectEntry,
	systemDesignMgr SystemDesignEntry,
	projectDesignMgr ProjectDesignEntry,
	constructionMgr ConstructionEntry,
	operationsMgr OperationsEntry,
	billingMgr BillingEntry,
	sec security.Security,
) *Client {
	return &Client{
		project:       projectMgr,
		systemDesign:  systemDesignMgr,
		projectDesign: projectDesignMgr,
		construction:  constructionMgr,
		operations:    operationsMgr,
		billing:       billingMgr,
		security:      sec,
	}
}
