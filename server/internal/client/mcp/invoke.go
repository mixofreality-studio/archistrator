package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/settlement"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"
)

// InvokeTool authorizes the principal, resolves the named tool to exactly one
// Manager op from the static projection table, and relays the call
// (service-contract §Op invokeTool). Each invocation routes to EXACTLY one Manager.
//
// Errors: *ToolError with kind:
//   - permission_denied  — Authorize returned Deny or a security error.
//   - invalid_argument   — toolName unknown, or args fail schema validation.
//   - other kinds        — mapped from the Manager's *fwmanager.Error (see §ErrorModel).
func (c *Client) InvokeTool(ctx context.Context, principal Principal, toolName ToolName, args ToolArgs) (ToolResult, *ToolError) {
	if _, ok := toolsByName[toolName]; !ok {
		return ToolResult{}, invalidArgumentError(fmt.Sprintf("tool %q is not in the catalog", toolName))
	}
	handler, ok := dispatchTable[toolName]
	if !ok {
		return ToolResult{}, invalidArgumentError(fmt.Sprintf("tool %q has no registered handler", toolName))
	}
	return handler(ctx, c, principal, args)
}

// toolHandler is the function signature every dispatch-table entry implements.
// It decodes args, calls authorize, calls the Manager, and returns the JSON-
// encoded result as ToolResult.Content.
type toolHandler func(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError)

// dispatchTable maps every ToolName in the static catalog to its handler.
// It is the build-time "projection table" the contract names (§Op invokeTool).
var dispatchTable = map[ToolName]toolHandler{
	// projectManager
	"project__create": handleProjectCreate,
	"project__list":   handleProjectList,
	"project__get":    handleProjectGet,

	// systemDesignManager
	"system_design__set_research":  handleSystemDesignSetResearch,
	"system_design__start":         handleSystemDesignStart,
	"system_design__request_draft": handleSystemDesignRequestDraft,
	"system_design__submit_review": handleSystemDesignSubmitReview,
	"system_design__advance":       handleSystemDesignAdvance,
	"system_design__get_state":     handleSystemDesignGetState,

	// projectDesignManager
	"project_design__request_draft": handleProjectDesignRequestDraft,
	"project_design__submit_review": handleProjectDesignSubmitReview,
	"project_design__request_sdp":   handleProjectDesignRequestSDP,
	"project_design__submit_sdp":    handleProjectDesignSubmitSDP,
	"project_design__advance":       handleProjectDesignAdvance,
	"project_design__get_state":     handleProjectDesignGetState,

	// constructionManager
	"construction__get_state":    handleConstructionGetState,
	"construction__execute_next": handleConstructionExecuteNext,
	"construction__pause":        handleConstructionPause,
	"construction__override":     handleConstructionOverride,

	// operationsManager
	"operations__deploy":          handleOperationsDeploy,
	"operations__withdraw":        handleOperationsWithdraw,
	"operations__cost_projection": handleOperationsCostProjection,
	"operations__view":            handleOperationsView,

	// billingManager (settlementManager)
	"billing__onboard": handleBillingOnboard,
}

// ---------------------------------------------------------------------------
// result encoding helpers
// ---------------------------------------------------------------------------

func marshalResult(v any) (ToolResult, *ToolError) {
	b, err := json.Marshal(v)
	if err != nil {
		return ToolResult{}, &ToolError{Kind: ToolErrInternal, Detail: "marshal result: " + err.Error()}
	}
	return ToolResult{Content: b}, nil
}

// argString reads a required string field from args.
func argString(args ToolArgs, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%q is required", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%q must be a string", key)
	}
	return s, nil
}

// argStringOptional reads an optional string field from args; returns "" if absent.
func argStringOptional(args ToolArgs, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// argUUID reads a required UUID string field from args.
func argUUID(args ToolArgs, key string) (uuid.UUID, error) {
	s, err := argString(args, key)
	if err != nil {
		return uuid.UUID{}, err
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("%q must be a valid UUID: %w", key, err)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// projectManager handlers
// ---------------------------------------------------------------------------

func handleProjectCreate(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	name, err := argString(args, "name")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "create-project", "projectCatalog", string(ownerScopeFor(principal))); toolErr != nil {
		return ToolResult{}, toolErr
	}
	id, err := c.project.CreateProject(ctx, ownerScopeFor(principal), name)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]string{"projectId": id.String()})
}

func handleProjectList(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	if toolErr := c.authorize(ctx, principal, "list-projects", "projectCatalog", string(ownerScopeFor(principal))); toolErr != nil {
		return ToolResult{}, toolErr
	}
	summaries, err := c.project.ListProjects(ctx, ownerScopeFor(principal))
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(summaries)
}

func handleProjectGet(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := ProjectID(projectIDStr)
	if toolErr := c.authorize(ctx, principal, "read-project", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	state, err := c.project.GetProject(ctx, projectID)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(state)
}

// ---------------------------------------------------------------------------
// systemDesignManager handlers
// ---------------------------------------------------------------------------

func handleSystemDesignSetResearch(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := systemdesign.ProjectID(projectIDStr)
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	sourcesRaw, ok := args["sources"]
	if !ok {
		return ToolResult{}, invalidArgumentError(`"sources" is required`)
	}
	sourcesJSON, err := json.Marshal(sourcesRaw)
	if err != nil {
		return ToolResult{}, invalidArgumentError("sources: " + err.Error())
	}
	var sources []struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(sourcesJSON, &sources); err != nil {
		return ToolResult{}, invalidArgumentError("sources must be an array of {title, content}: " + err.Error())
	}
	researchSources := make([]systemdesign.ResearchSource, 0, len(sources))
	for i, s := range sources {
		if s.Title == "" {
			return ToolResult{}, invalidArgumentError(fmt.Sprintf("sources[%d].title is required", i))
		}
		if s.Content == "" {
			return ToolResult{}, invalidArgumentError(fmt.Sprintf("sources[%d].content is required", i))
		}
		researchSources = append(researchSources, systemdesign.ResearchSource{Title: s.Title, Content: s.Content})
	}
	_, err = c.systemDesign.SetResearchInput(ctx, projectID, systemdesign.ResearchInput{Sources: researchSources})
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]bool{"ok": true})
}

func handleSystemDesignStart(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := systemdesign.ProjectID(projectIDStr)
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	ref, err := c.systemDesign.StartSystemDesign(ctx, projectID)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]string{"sessionRef": ref.String()})
}

func handleSystemDesignRequestDraft(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := systemdesign.ProjectID(projectIDStr)
	kindStr, err := argString(args, "artifactKind")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	kind, err := artifactKindFromString(kindStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	var feedback *systemdesign.ReviewFeedback
	if notes := argStringOptional(args, "feedback"); notes != "" {
		feedback = &systemdesign.ReviewFeedback{Notes: notes}
	}
	ref, err := c.systemDesign.RequestArtifactDraft(ctx, projectID, kind, feedback)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]string{"sessionRef": ref.String()})
}

func handleSystemDesignSubmitReview(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := systemdesign.ProjectID(projectIDStr)
	kindStr, err := argString(args, "artifactKind")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	kind, err := artifactKindFromString(kindStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	decisionStr, err := argString(args, "decision")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	decision, err := parseSystemDesignReviewDecision(decisionStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "approve-artifact", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	var feedback *systemdesign.ReviewFeedback
	if notes := argStringOptional(args, "feedback"); notes != "" {
		feedback = &systemdesign.ReviewFeedback{Notes: notes}
	}
	if err := c.systemDesign.SubmitReviewDecision(ctx, projectID, kind, decision, feedback); err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]bool{"ok": true})
}

func handleSystemDesignAdvance(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := systemdesign.ProjectID(projectIDStr)
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	result, err := c.systemDesign.AdvancePhase(ctx, projectID)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	missing := make([]string, 0, len(result.MissingArtifacts))
	for _, k := range result.MissingArtifacts {
		missing = append(missing, k.String())
	}
	return marshalResult(map[string]any{"advanced": result.Advanced, "missingArtifacts": missing})
}

func handleSystemDesignGetState(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := systemdesign.ProjectID(projectIDStr)
	kindStr, err := argString(args, "artifactKind")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	kind, err := artifactKindFromString(kindStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	view, err := c.systemDesign.GetSessionState(ctx, projectID, kind)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(view)
}

// ---------------------------------------------------------------------------
// projectDesignManager handlers
// ---------------------------------------------------------------------------

func handleProjectDesignRequestDraft(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := projectdesign.ProjectID(projectIDStr)
	kindStr, err := argString(args, "artifactKind")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	kind, err := artifactKindFromString(kindStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	var feedback *projectdesign.ReviewFeedback
	if notes := argStringOptional(args, "feedback"); notes != "" {
		feedback = &projectdesign.ReviewFeedback{Notes: notes}
	}
	ref, err := c.projectDesign.RequestArtifactDraft(ctx, projectID, kind, feedback)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]string{"sessionRef": ref.String()})
}

func handleProjectDesignSubmitReview(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := projectdesign.ProjectID(projectIDStr)
	kindStr, err := argString(args, "artifactKind")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	kind, err := artifactKindFromString(kindStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	decisionStr, err := argString(args, "decision")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	decision, err := parseProjectDesignReviewDecision(decisionStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "approve-artifact", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	var feedback *projectdesign.ReviewFeedback
	if notes := argStringOptional(args, "feedback"); notes != "" {
		feedback = &projectdesign.ReviewFeedback{Notes: notes}
	}
	if err := c.projectDesign.SubmitReviewDecision(ctx, projectID, kind, decision, feedback); err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]bool{"ok": true})
}

func handleProjectDesignRequestSDP(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := projectdesign.ProjectID(projectIDStr)
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	ref, err := c.projectDesign.RequestSDPCommit(ctx, projectID)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]string{"sessionRef": ref.String()})
}

func handleProjectDesignSubmitSDP(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := projectdesign.ProjectID(projectIDStr)
	decisionStr, err := argString(args, "decision")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	sdpDecision, err := parseSDPDecision(decisionStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "approve-artifact", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	var optionID *projectdesign.OptionID
	if optStr := argStringOptional(args, "optionId"); optStr != "" {
		oid := projectdesign.OptionID(optStr)
		optionID = &oid
	}
	var feedback *projectdesign.ReviewFeedback
	if notes := argStringOptional(args, "feedback"); notes != "" {
		feedback = &projectdesign.ReviewFeedback{Notes: notes}
	}
	if err := c.projectDesign.SubmitSDPDecision(ctx, projectID, sdpDecision, optionID, feedback); err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]bool{"ok": true})
}

func handleProjectDesignAdvance(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := projectdesign.ProjectID(projectIDStr)
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	result, err := c.projectDesign.AdvanceToConstruction(ctx, projectID)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]bool{"advanced": result.Advanced})
}

func handleProjectDesignGetState(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := projectdesign.ProjectID(projectIDStr)
	kindStr, err := argString(args, "artifactKind")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	kind, err := artifactKindFromString(kindStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	view, err := c.projectDesign.GetSessionState(ctx, projectID, kind)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(view)
}

// ---------------------------------------------------------------------------
// constructionManager handlers
// ---------------------------------------------------------------------------

func handleConstructionGetState(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := construction.ProjectID(projectIDStr)
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	var activityID *construction.ActivityID
	if actStr := argStringOptional(args, "activityId"); actStr != "" {
		activityID = &actStr
	}
	view, err := c.construction.GetSessionState(ctx, projectID, activityID)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(view)
}

func handleConstructionExecuteNext(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := construction.ProjectID(projectIDStr)
	tickID, err := argString(args, "tickId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	result, err := c.construction.ExecuteNextActivity(ctx, projectID, tickID)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(result)
}

func handleConstructionPause(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := construction.ProjectID(projectIDStr)
	reason, err := argString(args, "reason")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	if err := c.construction.PauseProject(ctx, projectID, reason); err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]bool{"ok": true})
}

func handleConstructionOverride(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	projectIDStr, err := argString(args, "projectId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	projectID := construction.ProjectID(projectIDStr)
	activityIDStr, err := argString(args, "activityId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	kindStr, err := argString(args, "kind")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	overrideKind, err := parseOverrideKind(kindStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "drive-phase", "project", projectID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	override := construction.ActivityOverride{
		Kind:  overrideKind,
		Notes: argStringOptional(args, "notes"),
	}
	if err := c.construction.OverrideActivity(ctx, projectID, activityIDStr, override); err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// operationsManager handlers
// ---------------------------------------------------------------------------

func handleOperationsDeploy(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	appID, err := argUUID(args, "operatedAppId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	changeID, err := argString(args, "changeId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	patchKindStr, err := argString(args, "patchKind")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	patchKind, err := parsePatchKind(patchKindStr)
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "operate-system", "operatedApp", appID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	var renderedState []byte
	if stateStr := argStringOptional(args, "renderedDesiredState"); stateStr != "" {
		renderedState = []byte(stateStr)
	}
	change := operations.DesiredStateChange{
		Reason:               operations.ReasonDeployAfterConstruction,
		PatchKind:            patchKind,
		ChangeID:             changeID,
		RenderedDesiredState: renderedState,
	}
	result, err := c.operations.DeployAfterConstruction(ctx, appID, change)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(result)
}

func handleOperationsWithdraw(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	appID, err := argUUID(args, "operatedAppId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	changeID, err := argString(args, "changeId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "operate-system", "operatedApp", appID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	reason := operations.WithdrawReason{Notes: argStringOptional(args, "notes")}
	result, err := c.operations.WithdrawSystem(ctx, appID, changeID, reason)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(result)
}

func handleOperationsCostProjection(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	appID, err := argUUID(args, "operatedAppId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	requestID, err := argString(args, "requestId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "read-operated-system", "operatedApp", appID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	projection, err := c.operations.QueryCostProjection(ctx, appID, requestID, nil)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(projection)
}

func handleOperationsView(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	appID, err := argUUID(args, "operatedAppId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	requestID, err := argString(args, "requestId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "read-operated-system", "operatedApp", appID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	view, err := c.operations.QueryOperatedSystemView(ctx, appID, requestID)
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(view)
}

// ---------------------------------------------------------------------------
// billingManager (settlementManager) handlers
// ---------------------------------------------------------------------------

func handleBillingOnboard(ctx context.Context, c *Client, principal Principal, args ToolArgs) (ToolResult, *ToolError) {
	appID, err := argUUID(args, "deployedAppId")
	if err != nil {
		return ToolResult{}, invalidArgumentError(err.Error())
	}
	if toolErr := c.authorize(ctx, principal, "operate-system", "operatedApp", appID.String()); toolErr != nil {
		return ToolResult{}, toolErr
	}
	ref, err := c.billing.OnboardPaymentIntegration(ctx, settlement.DeployedAppID(appID))
	if err != nil {
		return ToolResult{}, toolErrorFromManagerError(err)
	}
	return marshalResult(map[string]string{"customerId": ref.CustomerID.String()})
}

// ---------------------------------------------------------------------------
// Enum parsers
// ---------------------------------------------------------------------------

func parseSystemDesignReviewDecision(s string) (systemdesign.ReviewDecision, error) {
	switch s {
	case "approve":
		return systemdesign.ReviewApprove, nil
	case "reject":
		return systemdesign.ReviewReject, nil
	case "withdraw":
		return systemdesign.ReviewWithdraw, nil
	default:
		return systemdesign.ReviewDecisionUnknown, fmt.Errorf("decision %q must be one of approve|reject|withdraw", s)
	}
}

func parseProjectDesignReviewDecision(s string) (projectdesign.ReviewDecision, error) {
	switch s {
	case "approve":
		return projectdesign.ReviewApprove, nil
	case "reject":
		return projectdesign.ReviewReject, nil
	case "withdraw":
		return projectdesign.ReviewWithdraw, nil
	default:
		return projectdesign.ReviewDecisionUnknown, fmt.Errorf("decision %q must be one of approve|reject|withdraw", s)
	}
}

func parseSDPDecision(s string) (projectdesign.SDPDecision, error) {
	switch s {
	case "commit":
		return projectdesign.SDPCommit, nil
	case "rejectAll":
		return projectdesign.SDPRejectAll, nil
	default:
		return projectdesign.SDPDecisionUnknown, fmt.Errorf("decision %q must be one of commit|rejectAll", s)
	}
}

func parseOverrideKind(s string) (construction.OverrideKind, error) {
	switch s {
	case "takeover":
		return construction.OverrideTakeover, nil
	case "retry":
		return construction.OverrideRetry, nil
	case "skip":
		return construction.OverrideSkip, nil
	case "reassign":
		return construction.OverrideReassign, nil
	default:
		return construction.OverrideUnknown, fmt.Errorf("kind %q must be one of takeover|retry|skip|reassign", s)
	}
}

func parsePatchKind(s string) (operations.PatchKind, error) {
	switch s {
	case "fullBundle":
		return operations.PatchFullBundle, nil
	case "scale":
		return operations.PatchScale, nil
	case "policy":
		return operations.PatchPolicy, nil
	default:
		return operations.PatchKindUnknown, fmt.Errorf("patchKind %q must be one of fullBundle|scale|policy", s)
	}
}
