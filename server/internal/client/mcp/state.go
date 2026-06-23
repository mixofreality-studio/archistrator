package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
)

// ReadProjectState is the agent continuity read op (service-contract §Op
// readProjectState). It authorizes the principal for read scope on projectId,
// then routes to exactly one Manager read op selected by view:
//
//   - view == nil or Kind == StateViewKindFull  → projectManager.GetProject
//   - Kind == StateViewKindSystemDesign          → systemDesignManager.GetSessionState
//   - Kind == StateViewKindProjectDesign         → projectDesignManager.GetSessionState
//
// The result is the Manager's typed read reply marshaled as ProjectStateView.State
// (the same typed models the SPA renders — untransformed).
//
// Errors: *ToolError (see service-contract §DataContracts / §ErrorModel).
func (c *Client) ReadProjectState(ctx context.Context, principal Principal, projectID ProjectID, view *StateView) (ProjectStateView, *ToolError) {
	if toolErr := c.authorize(ctx, principal, "read-project", "project", projectID.String()); toolErr != nil {
		return ProjectStateView{}, toolErr
	}

	if view == nil || view.Kind == StateViewKindFull || view.Kind == "" {
		return c.readFullState(ctx, projectID)
	}

	switch view.Kind {
	case StateViewKindSystemDesign:
		return c.readSystemDesignState(ctx, projectID, view.ArtifactKind)
	case StateViewKindProjectDesign:
		return c.readProjectDesignState(ctx, projectID, view.ArtifactKind)
	default:
		return ProjectStateView{}, invalidArgumentError(fmt.Sprintf("unknown StateView.Kind %q; must be one of full|systemDesign|projectDesign", view.Kind))
	}
}

// readFullState routes to projectManager.GetProject and returns the full typed
// head-state as ProjectStateView.State.
func (c *Client) readFullState(ctx context.Context, projectID ProjectID) (ProjectStateView, *ToolError) {
	state, err := c.project.GetProject(ctx, projectID)
	if err != nil {
		return ProjectStateView{}, toolErrorFromManagerError(err)
	}
	b, mErr := json.Marshal(state)
	if mErr != nil {
		return ProjectStateView{}, &ToolError{Kind: ToolErrInternal, Detail: "marshal project state: " + mErr.Error()}
	}
	return ProjectStateView{State: b}, nil
}

// readSystemDesignState routes to systemDesignManager.GetSessionState for the
// Phase-1 artifact identified by artifactKind.
func (c *Client) readSystemDesignState(ctx context.Context, projectID ProjectID, artifactKindStr string) (ProjectStateView, *ToolError) {
	if artifactKindStr == "" {
		return ProjectStateView{}, invalidArgumentError("StateView.ArtifactKind is required for systemDesign view")
	}
	kind, err := artifactKindFromString(artifactKindStr)
	if err != nil {
		return ProjectStateView{}, invalidArgumentError(err.Error())
	}
	view, err := c.systemDesign.GetSessionState(ctx, systemdesign.ProjectID(projectID), kind)
	if err != nil {
		return ProjectStateView{}, toolErrorFromManagerError(err)
	}
	b, mErr := json.Marshal(view)
	if mErr != nil {
		return ProjectStateView{}, &ToolError{Kind: ToolErrInternal, Detail: "marshal session state: " + mErr.Error()}
	}
	return ProjectStateView{State: b}, nil
}

// readProjectDesignState routes to projectDesignManager.GetSessionState for the
// Phase-2 artifact identified by artifactKind.
func (c *Client) readProjectDesignState(ctx context.Context, projectID ProjectID, artifactKindStr string) (ProjectStateView, *ToolError) {
	if artifactKindStr == "" {
		return ProjectStateView{}, invalidArgumentError("StateView.ArtifactKind is required for projectDesign view")
	}
	kind, err := artifactKindFromString(artifactKindStr)
	if err != nil {
		return ProjectStateView{}, invalidArgumentError(err.Error())
	}
	view, err := c.projectDesign.GetSessionState(ctx, projectdesign.ProjectID(projectID), kind)
	if err != nil {
		return ProjectStateView{}, toolErrorFromManagerError(err)
	}
	b, mErr := json.Marshal(view)
	if mErr != nil {
		return ProjectStateView{}, &ToolError{Kind: ToolErrInternal, Detail: "marshal session state: " + mErr.Error()}
	}
	return ProjectStateView{State: b}, nil
}
