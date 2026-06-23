package mcp

import (
	"errors"
	"fmt"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go/manager"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/project"
	"github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"
)

// ---------------------------------------------------------------------------
// Principal — the resolved edge-trusted identity (service-contract §DataContracts)
// ---------------------------------------------------------------------------

// Principal is the resolved, edge-trusted identity from the MCP host claims.
// It is an alias for the framework security principal; the Client never parses
// token bytes — the MCP transport adapter resolves the principal before calling
// the three ops.
type Principal = security.SecurityPrincipal

// ---------------------------------------------------------------------------
// Tool catalog contracts (service-contract §DataContracts)
// ---------------------------------------------------------------------------

// ToolName is a generated MCP tool name projected from the frozen Manager OpenAPI
// specs (service-contract §DataContracts). A ToolName must be in the principal's
// ToolCatalog for InvokeTool to accept it; an unknown name is InvalidArgument.
type ToolName string

// ToolArgs is the schema-validated, typed request payload for the named tool.
// Each tool's handler decodes the map into the target Manager's typed input type.
// An InvokeTool call that fails schema validation returns InvalidArgument.
type ToolArgs = map[string]any

// ToolDescriptor is one MCP tool descriptor in the ToolCatalog
// (service-contract §DataContracts). It mirrors the MCP protocol's tool schema
// shape: a stable name, a human title, and JSON Schema definitions for input and
// output.
type ToolDescriptor struct {
	// ToolName is the generated, stable MCP tool name (projection of one frozen
	// Manager OpenAPI op).
	ToolName ToolName `json:"toolName"`
	// Title is the human-readable description the agent uses for tool selection.
	Title string `json:"title"`
	// InputSchema is the JSON Schema (as a JSON string) for the tool's args.
	InputSchema string `json:"inputSchema,omitempty"`
	// OutputSchema is the JSON Schema (as a JSON string) for the tool's result.
	OutputSchema string `json:"outputSchema,omitempty"`
}

// ToolCatalog is the set of MCP tool descriptors returned by ListTools
// (service-contract §DataContracts).
type ToolCatalog struct {
	Tools []ToolDescriptor `json:"tools"`
}

// ToolResult is the typed Manager reply marshaled as an MCP tool result
// (service-contract §DataContracts). Content is the JSON-encoded Manager response.
type ToolResult struct {
	// Content is the typed Manager reply marshaled as MCP tool result content.
	Content []byte `json:"content"`
}

// ---------------------------------------------------------------------------
// ToolError — structured error model (service-contract §ErrorModel)
// ---------------------------------------------------------------------------

// ToolErrorKind classifies a ToolError. The kinds map framework-go error kinds
// to the MCP tool surface (service-contract §DataContracts / §ErrorModel).
type ToolErrorKind string

const (
	// ToolErrPermissionDenied maps Unauthorized/Deny (non-retryable).
	ToolErrPermissionDenied ToolErrorKind = "permission_denied"
	// ToolErrInvalidArgument maps ContractMisuse / schema validation failure.
	ToolErrInvalidArgument ToolErrorKind = "invalid_argument"
	// ToolErrFailedPrecondition maps FailedPrecondition (a workflow gate not met).
	ToolErrFailedPrecondition ToolErrorKind = "failed_precondition"
	// ToolErrNotFound maps NotFound.
	ToolErrNotFound ToolErrorKind = "not_found"
	// ToolErrConflict maps an idempotency conflict — retryable by the agent.
	ToolErrConflict ToolErrorKind = "conflict"
	// ToolErrUnavailable maps Infrastructure (transient) — retryable by the agent.
	ToolErrUnavailable ToolErrorKind = "unavailable"
	// ToolErrInternal maps ContractMisuse/ContentPolicy — terminal (non-retryable).
	ToolErrInternal ToolErrorKind = "internal"
)

// ToolError is the structured error the mcpClient surface returns to the MCP
// host. It carries the framework-go error kind mapped to the MCP error vocabulary
// (service-contract §DataContracts / §ErrorModel). Retryable is true only for
// Conflict and Unavailable kinds; all other kinds are terminal at the agent.
type ToolError struct {
	Kind      ToolErrorKind
	Detail    string
	Retryable bool
}

func (e *ToolError) Error() string {
	return fmt.Sprintf("mcp: %s: %s", e.Kind, e.Detail)
}

// toolErrorFromManagerError maps a Manager error (framework-go *fwmanager.Error)
// to a *ToolError, per the service-contract §ErrorModel. Any unmapped or nil error
// maps to ToolErrInternal (fail-closed).
func toolErrorFromManagerError(err error) *ToolError {
	if err == nil {
		return &ToolError{Kind: ToolErrInternal, Detail: "nil error passed to error mapper"}
	}
	var me *fwmanager.Error
	if errors.As(err, &me) {
		switch me.Kind {
		case fwmanager.Unauthorized:
			return &ToolError{Kind: ToolErrPermissionDenied, Detail: me.Detail, Retryable: false}
		case fwmanager.ContractMisuse:
			return &ToolError{Kind: ToolErrInvalidArgument, Detail: me.Detail, Retryable: false}
		case fwmanager.FailedPrecondition:
			return &ToolError{Kind: ToolErrFailedPrecondition, Detail: me.Detail, Retryable: false}
		case fwmanager.NotFound:
			return &ToolError{Kind: ToolErrNotFound, Detail: me.Detail, Retryable: false}
		case fwmanager.Infrastructure:
			return &ToolError{Kind: ToolErrUnavailable, Detail: me.Detail, Retryable: true}
		default:
			return &ToolError{Kind: ToolErrInternal, Detail: me.Detail, Retryable: false}
		}
	}
	return &ToolError{Kind: ToolErrInternal, Detail: err.Error(), Retryable: false}
}

// permissionDeniedError returns the canonical permission_denied ToolError used
// when security.Authorize returns Deny (or a security error — fail-closed).
func permissionDeniedError() *ToolError {
	return &ToolError{Kind: ToolErrPermissionDenied, Detail: "not permitted", Retryable: false}
}

// invalidArgumentError wraps a validation failure as an invalid_argument ToolError.
func invalidArgumentError(detail string) *ToolError {
	return &ToolError{Kind: ToolErrInvalidArgument, Detail: detail, Retryable: false}
}

// ---------------------------------------------------------------------------
// Project identity (service-contract §DataContracts)
// ---------------------------------------------------------------------------

// ProjectID is the project aggregate identity (name-shaped business key;
// name-as-identity, C-PM-Δ). Re-exported from the manager/project surface so
// callers depend on the Manager surface, not the ResourceAccess directly.
type ProjectID = project.ProjectID

// ---------------------------------------------------------------------------
// StateView — selector for ReadProjectState (service-contract §DataContracts)
// ---------------------------------------------------------------------------

// StateViewKind selects the target Manager read op for ReadProjectState.
type StateViewKind string

const (
	// StateViewKindFull selects projectManager.GetProject — the full typed
	// head-state (the default when StateView is nil).
	StateViewKindFull StateViewKind = "full"
	// StateViewKindSystemDesign selects systemDesignManager.GetSessionState — one
	// Phase-1 artifact slice.
	StateViewKindSystemDesign StateViewKind = "systemDesign"
	// StateViewKindProjectDesign selects projectDesignManager.GetSessionState —
	// one Phase-2 artifact slice.
	StateViewKindProjectDesign StateViewKind = "projectDesign"
)

// StateView is the optional selector that routes ReadProjectState to the
// appropriate Manager read op (service-contract §DataContracts).
// A nil StateView defaults to StateViewKindFull (full project head-state).
type StateView struct {
	// Kind selects the target Manager read op.
	Kind StateViewKind `json:"kind"`
	// ArtifactKind selects the specific artifact slot for session views
	// (StateViewKindSystemDesign / StateViewKindProjectDesign). It must be a valid
	// ArtifactKind string; the Client maps it to the Manager's typed ArtifactKind.
	ArtifactKind string `json:"artifactKind,omitempty"`
}

// ---------------------------------------------------------------------------
// ProjectStateView — ReadProjectState output (service-contract §DataContracts)
// ---------------------------------------------------------------------------

// ProjectStateView is the typed Method head-state JSON returned by
// ReadProjectState (service-contract §DataContracts). State is the
// JSON-marshaled Manager read result — the same typed models the SPA renders,
// untransformed.
type ProjectStateView struct {
	// State is the JSON-marshaled Manager read result.
	State []byte `json:"state"`
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// ownerScopeFor derives the project catalog OwnerScope from the authenticated
// principal. The stable opaque Subject ("sub") is the owner key; it never
// changes across token refreshes or email/username edits. Mirrors the webClient
// convention (web/handlers.go ownerScopeFor).
func ownerScopeFor(p Principal) project.OwnerScope {
	if p.Subject != "" {
		return project.OwnerScope(p.Subject)
	}
	return project.OwnerScope(p.Email)
}

// artifactKindFromString maps a wire artifact-kind name to the typed
// projectstate.ArtifactKind. Returns an error for unknown names.
func artifactKindFromString(s string) (projectstate.ArtifactKind, error) {
	kind, ok := artifactKindByName[s]
	if !ok {
		return 0, fmt.Errorf("artifactKind %q is not a recognized kind", s)
	}
	return kind, nil
}

// artifactKindByName maps wire kind names to typed ArtifactKind values.
// Covers all Phase-1 and Phase-2 kinds (canonical names from openapi.yaml).
var artifactKindByName = map[string]projectstate.ArtifactKind{
	// Phase 1
	"mission":              projectstate.KindMission,
	"glossary":             projectstate.KindGlossary,
	"scrubbedRequirements": projectstate.KindScrubbedRequirements,
	"volatilities":         projectstate.KindVolatilities,
	"coreUseCases":         projectstate.KindCoreUseCases,
	"system":               projectstate.KindSystem,
	"operationalConcepts":  projectstate.KindOperationalConcepts,
	"standardCheck":        projectstate.KindStandardCheck,
	// Phase 2
	"planningAssumptions":  projectstate.KindPlanningAssumptions,
	"activityList":         projectstate.KindActivityList,
	"network":              projectstate.KindNetwork,
	"normalSolution":       projectstate.KindNormalSolution,
	"subcriticalSolution":  projectstate.KindSubcriticalSolution,
	"compressedSolution":   projectstate.KindCompressedSolution,
	"decompressedSolution": projectstate.KindDecompressedSolution,
	"riskModel":            projectstate.KindRiskModel,
	"sdpReview":            projectstate.KindSdpReview,
}
