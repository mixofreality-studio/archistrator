package mcp

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// ListTools returns the static ToolCatalog projected from the frozen Manager
// OpenAPI vocabulary (service-contract §Op listTools). It calls no Manager and
// is idempotent. The principal is used to filter the catalog to tools this
// principal may see; in this build the full catalog is returned (per-tool
// visibility is enforced by the Authorize gate in InvokeTool).
//
// Errors: *ToolError only on authorization infrastructure failure (fail-closed).
func (c *Client) ListTools(_ context.Context, _ Principal) (ToolCatalog, *ToolError) {
	return staticCatalog, nil
}

// staticCatalog is the build-time projection of the frozen Manager OpenAPI
// vocabulary into MCP tool descriptors (service-contract §Op listTools, OQ-2
// "build-time codegen recommended"). Each ToolDescriptor maps 1:1 to one frozen
// Manager op. The schema strings are minimal JSON Schema documents; a future
// codegen pass can replace them with richer per-field schemas without a
// contract-surface change.
//
// Tool naming convention: <namespace>__<operation> where namespace is the Manager
// area (project, system_design, project_design, construction, operations, billing).
// Double-underscore separates namespace from operation, following MCP convention
// for namespaced tools.
var staticCatalog = ToolCatalog{
	Tools: []ToolDescriptor{
		// --- projectManager ops -----------------------------------------------
		{
			ToolName: "project__create",
			Title:    "Create a new project (adopt an existing repo into archistrator).",
			InputSchema: `{"type":"object","required":["name"],"properties":{` +
				`"name":{"type":"string","description":"The adopted repo name (equals the project identity)."}` +
				`}}`,
		},
		{
			ToolName: "project__list",
			Title:    "List projects owned by the authenticated principal.",
			InputSchema: `{"type":"object","properties":{` +
				`}}`,
		},
		{
			ToolName: "project__get",
			Title:    "Get the full typed head-state for one project.",
			InputSchema: `{"type":"object","required":["projectId"],"properties":{` +
				`"projectId":{"type":"string","description":"The project identity (adopted repo name)."}` +
				`}}`,
		},

		// --- systemDesignManager ops ------------------------------------------
		{
			ToolName: "system_design__set_research",
			Title:    "Record the Phase-1 research input for a project (prerequisite for start).",
			InputSchema: `{"type":"object","required":["projectId","sources"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"sources":{"type":"array","items":{"type":"object","required":["title","content"],"properties":{` +
				`"title":{"type":"string"},"content":{"type":"string"}}}}` +
				`}}`,
		},
		{
			ToolName: "system_design__start",
			Title:    "Start the Phase-1 (System Design) co-authoring workflow for a project.",
			InputSchema: `{"type":"object","required":["projectId"],"properties":{` +
				`"projectId":{"type":"string"}` +
				`}}`,
		},
		{
			ToolName: "system_design__request_draft",
			Title:    "Request a Phase-1 artifact draft (mission, glossary, volatilities, etc.).",
			InputSchema: `{"type":"object","required":["projectId","artifactKind"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"artifactKind":{"type":"string","enum":["mission","glossary","scrubbedRequirements","volatilities","coreUseCases","system","operationalConcepts","standardCheck"]},` +
				`"feedback":{"type":"string","description":"Optional feedback for a redraft."}` +
				`}}`,
		},
		{
			ToolName: "system_design__submit_review",
			Title:    "Submit the architect's review decision on a Phase-1 artifact.",
			InputSchema: `{"type":"object","required":["projectId","artifactKind","decision"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"artifactKind":{"type":"string"},` +
				`"decision":{"type":"string","enum":["approve","reject","withdraw"]},` +
				`"feedback":{"type":"string","description":"Required on reject."}` +
				`}}`,
		},
		{
			ToolName: "system_design__advance",
			Title:    "Attempt to advance the project past Phase 1 (all artifacts must be committed).",
			InputSchema: `{"type":"object","required":["projectId"],"properties":{` +
				`"projectId":{"type":"string"}` +
				`}}`,
		},
		{
			ToolName: "system_design__get_state",
			Title:    "Get the technical session state for one Phase-1 artifact (drafting / awaitingReview / committed / etc.).",
			InputSchema: `{"type":"object","required":["projectId","artifactKind"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"artifactKind":{"type":"string"}` +
				`}}`,
		},

		// --- projectDesignManager ops -----------------------------------------
		{
			ToolName: "project_design__request_draft",
			Title:    "Request a Phase-2 artifact draft (activityList, network, solutions, riskModel, etc.).",
			InputSchema: `{"type":"object","required":["projectId","artifactKind"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"artifactKind":{"type":"string","enum":["planningAssumptions","activityList","network","normalSolution","subcriticalSolution","compressedSolution","decompressedSolution","riskModel"]},` +
				`"feedback":{"type":"string","description":"Optional feedback for a redraft."}` +
				`}}`,
		},
		{
			ToolName: "project_design__submit_review",
			Title:    "Submit the architect's review decision on a Phase-2 artifact.",
			InputSchema: `{"type":"object","required":["projectId","artifactKind","decision"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"artifactKind":{"type":"string"},` +
				`"decision":{"type":"string","enum":["approve","reject","withdraw"]},` +
				`"feedback":{"type":"string","description":"Required on reject."}` +
				`}}`,
		},
		{
			ToolName: "project_design__request_sdp",
			Title:    "Assemble the Software Development Plan (SDP) for architect review.",
			InputSchema: `{"type":"object","required":["projectId"],"properties":{` +
				`"projectId":{"type":"string"}` +
				`}}`,
		},
		{
			ToolName: "project_design__submit_sdp",
			Title:    "Submit the architect's decision on the SDP (commit or rejectAll).",
			InputSchema: `{"type":"object","required":["projectId","decision"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"decision":{"type":"string","enum":["commit","rejectAll"]},` +
				`"optionId":{"type":"string","description":"Required on commit."},` +
				`"feedback":{"type":"string","description":"Required on rejectAll."}` +
				`}}`,
		},
		{
			ToolName: "project_design__advance",
			Title:    "Advance the project from Phase 2 to Phase 3 (Construction).",
			InputSchema: `{"type":"object","required":["projectId"],"properties":{` +
				`"projectId":{"type":"string"}` +
				`}}`,
		},
		{
			ToolName: "project_design__get_state",
			Title:    "Get the technical session state for one Phase-2 artifact.",
			InputSchema: `{"type":"object","required":["projectId","artifactKind"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"artifactKind":{"type":"string"}` +
				`}}`,
		},

		// --- constructionManager ops ------------------------------------------
		{
			ToolName: "construction__get_state",
			Title:    "Get the Phase-3 construction session state for a project (or specific activity).",
			InputSchema: `{"type":"object","required":["projectId"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"activityId":{"type":"string","description":"Optional: narrow to one activity."}` +
				`}}`,
		},
		{
			ToolName: "construction__execute_next",
			Title:    "Trigger one construction pump tick (executes the next eligible activity).",
			InputSchema: `{"type":"object","required":["projectId","tickId"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"tickId":{"type":"string","description":"Idempotency key for this pump tick."}` +
				`}}`,
		},
		{
			ToolName: "construction__pause",
			Title:    "Pause all construction activity for a project.",
			InputSchema: `{"type":"object","required":["projectId","reason"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"reason":{"type":"string","description":"Operator rationale for the pause."}` +
				`}}`,
		},
		{
			ToolName: "construction__override",
			Title:    "Apply an operator override to a specific construction activity (takeover / retry / skip / reassign).",
			InputSchema: `{"type":"object","required":["projectId","activityId","kind"],"properties":{` +
				`"projectId":{"type":"string"},` +
				`"activityId":{"type":"string"},` +
				`"kind":{"type":"string","enum":["takeover","retry","skip","reassign"]},` +
				`"notes":{"type":"string"}` +
				`}}`,
		},

		// --- operationsManager ops --------------------------------------------
		{
			ToolName: "operations__deploy",
			Title:    "Deploy or update an operated system (deploy / scale / update autoscaler policy).",
			InputSchema: `{"type":"object","required":["operatedAppId","changeId","patchKind"],"properties":{` +
				`"operatedAppId":{"type":"string","format":"uuid"},` +
				`"changeId":{"type":"string","description":"Operator-supplied idempotency token."},` +
				`"patchKind":{"type":"string","enum":["fullBundle","scale","policy"]},` +
				`"renderedDesiredState":{"type":"string","format":"base64","description":"The infrastructure-neutral rendered desired-state (opaque bytes)."}` +
				`}}`,
		},
		{
			ToolName: "operations__withdraw",
			Title:    "Withdraw (terminate) a delivered operated system.",
			InputSchema: `{"type":"object","required":["operatedAppId","changeId"],"properties":{` +
				`"operatedAppId":{"type":"string","format":"uuid"},` +
				`"changeId":{"type":"string"},` +
				`"notes":{"type":"string","description":"Optional withdrawal rationale."}` +
				`}}`,
		},
		{
			ToolName: "operations__cost_projection",
			Title:    "Query the cost projection (what-if curve) for an operated system.",
			InputSchema: `{"type":"object","required":["operatedAppId","requestId"],"properties":{` +
				`"operatedAppId":{"type":"string","format":"uuid"},` +
				`"requestId":{"type":"string","description":"Idempotency key for this read op."},` +
				`"scalePoints":{"type":"array","items":{"type":"object","properties":{"replicas":{"type":"integer"}}}}` +
				`}}`,
		},
		{
			ToolName: "operations__view",
			Title:    "Query the composing operator system view (health, SLOs, autoscaler, cost run-rate).",
			InputSchema: `{"type":"object","required":["operatedAppId","requestId"],"properties":{` +
				`"operatedAppId":{"type":"string","format":"uuid"},` +
				`"requestId":{"type":"string"}` +
				`}}`,
		},

		// --- billingManager (settlementManager) ops --------------------------
		{
			ToolName: "billing__onboard",
			Title:    "Onboard payment integration for a delivered operated system.",
			InputSchema: `{"type":"object","required":["deployedAppId"],"properties":{` +
				`"deployedAppId":{"type":"string","format":"uuid"}` +
				`}}`,
		},
	},
}

// toolsByName is a lookup map for the static catalog, keyed by ToolName. Built
// once at init time so InvokeTool's existence check is O(1).
var toolsByName = func() map[ToolName]struct{} {
	m := make(map[ToolName]struct{}, len(staticCatalog.Tools))
	for _, t := range staticCatalog.Tools {
		m[t.ToolName] = struct{}{}
	}
	return m
}()

// authorize runs the cross-cutting pre-step for every Manager call: read the
// principal, call Authorize on the security Utility, and fail-closed on Deny or
// error. Returns nil on Permit; *ToolError on Deny.
func (c *Client) authorize(ctx context.Context, principal Principal, verb, resourceKind, resourceID string) *ToolError {
	decision, err := c.security.Authorize(ctx, principal,
		security.Action{Verb: verb},
		security.ResourceRef{Kind: resourceKind, ID: resourceID})
	if err != nil || !decision.Permit {
		return permissionDeniedError()
	}
	return nil
}
