package harness

// mcptransport.go is the MCP twin of httptransport.go — it drives the SAME
// published Client surface (server/internal/client/mcp/*), mounted at /mcp
// (streamable HTTP, official modelcontextprotocol/go-sdk server side —
// cmd/server/mcp_mount.go), instead of the REST routes. It is the R4
// cross-surface-equivalence transport: runUC1 (and any other transport-agnostic
// flow) runs unchanged against either.
//
// This is a HAND-ROLLED, stdlib-only streamable-HTTP JSON-RPC client — not an
// import of the SDK's client package. The systemtests module is deliberately
// stdlib-only (go.mod), so the smallest, most honest way to prove the wire
// surface is to speak the wire protocol directly with net/http +
// encoding/json, exactly as httpTransport speaks the REST wire directly with
// net/http. It implements only the small slice of MCP the server's streamable
// handler actually exercises for a single-session, single-request-per-POST
// client (negotiated protocol version below 2026-06-30, so the newer
// Mcp-Method/Mcp-Name standard headers are not required — see
// server's streamable_headers.go minVersionForStandardHeaders):
//
//   - POST initialize                    -> capture the Mcp-Session-Id response header
//   - POST notifications/initialized      -> a notification (no id); the server replies
//     202 Accepted with no body once the session is live
//   - POST tools/call                     -> the session id rides on every subsequent
//     request; the server always answers a call over Content-Type: text/event-stream
//     (mcp_mount.go passes nil StreamableHTTPOptions, so JSONResponse is false), one
//     "message" SSE event per response, matched back to the request's JSON-RPC id
//
// A tool error surfaces as an ORDINARY successful JSON-RPC response whose
// CallToolResult carries isError:true and a text content item
// "<fwmanager.Kind>: <Detail>" (mcp.AddTool's ToolHandlerFor packs a returned Go
// error into the result rather than a protocol-level error — see tool.go on the
// server side). mcpToolError parses that text back into the SAME transport
// sentinel errors (ErrBadRequest, ErrNotFound, ...) httpTransport maps HTTP
// status codes onto, so a step written once runs identically over both wires.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	mcpPath                  = "/mcp"
	mcpProtocolVersion       = "2025-06-18" // below streamable_headers.go's minVersionForStandardHeaders (2026-06-30)
	mcpSessionIDHeader       = "Mcp-Session-Id"
	mcpProtocolVersionHeader = "Mcp-Protocol-Version"
)

// mcpTransport drives the generated MCP tool surface over streamable HTTP. It
// knows ONLY the published tool names + JSON argument/result shapes (the same
// contract system-design_tools.gen.go / project-design_tools.gen.go /
// construction_tools.gen.go / operations_tools.gen.go publish) — never a
// server internal type.
type mcpTransport struct {
	baseURL string
	hc      *http.Client

	mu        sync.Mutex
	sessionID string

	nextID atomic.Int64
}

// NewMCPTransport binds a black-box MCP transport to a running server's base
// URL. The session is established lazily on the first call (initialize +
// notifications/initialized), mirroring NewHTTPTransport's zero-argument
// construction.
func NewMCPTransport(baseURL string) Transport {
	return &mcpTransport{baseURL: baseURL, hc: &http.Client{}}
}

func (t *mcpTransport) Name() string { return "mcp" }

func (t *mcpTransport) Close() error { return nil }

// --- session lifecycle -------------------------------------------------------

// ensureSession performs the initialize -> notifications/initialized handshake
// exactly once (guarded by sessionID being set). A failed attempt leaves the
// session unset so a subsequent call retries the handshake.
func (t *mcpTransport) ensureSession(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sessionID != "" {
		return nil
	}

	id := t.nextID.Add(1)
	initParams := map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "archistrator-systemtests", "version": "1.0.0"},
	}
	resp, sid, err := t.post(ctx, "", &id, "initialize", initParams)
	if err != nil {
		return fmt.Errorf("mcp initialize: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("mcp initialize: %s (code %d)", resp.Error.Message, resp.Error.Code)
	}
	if sid == "" {
		return fmt.Errorf("mcp initialize: server returned no %s header", mcpSessionIDHeader)
	}

	if _, _, err := t.post(ctx, sid, nil, "notifications/initialized", map[string]any{}); err != nil {
		return fmt.Errorf("mcp notifications/initialized: %w", err)
	}
	t.sessionID = sid
	return nil
}

// --- tools/call --------------------------------------------------------------

// callTool invokes one MCP tool by name, decoding a successful result's
// structuredContent into out (a pointer; nil for a tool with no bindable
// result). A tool-level error (isError:true) is mapped to the SAME transport
// sentinel errors httpTransport produces from HTTP status codes.
func (t *mcpTransport) callTool(ctx context.Context, name string, args any, out any) error {
	if err := t.ensureSession(ctx); err != nil {
		return err
	}
	t.mu.Lock()
	sid := t.sessionID
	t.mu.Unlock()

	id := t.nextID.Add(1)
	params := map[string]any{"name": name, "arguments": args}
	resp, _, err := t.post(ctx, sid, &id, "tools/call", params)
	if err != nil {
		return err
	}
	if resp.Error != nil {
		// A protocol-level error (malformed arguments against the tool's input
		// schema, unknown tool, ...) — a transport-edge bad request, never a Manager
		// outcome.
		return fmt.Errorf("%w: mcp %s: %s", ErrBadRequest, name, resp.Error.Message)
	}
	var result mcpCallToolResultWire
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("mcp: decode tools/call result for %s: %w", name, err)
	}
	if result.IsError {
		return mcpToolError(name, result)
	}
	if out == nil || len(result.StructuredContent) == 0 || string(result.StructuredContent) == "null" {
		return nil
	}
	if err := json.Unmarshal(result.StructuredContent, out); err != nil {
		return fmt.Errorf("mcp: decode structuredContent for %s: %w", name, err)
	}
	return nil
}

// callToolResult is callTool for the common "{result: T}" output shape shared
// by every non-void generated MCP op — mirrors mgr.<Op>Output{Result T} on the
// server side.
func callToolResult[T any](t *mcpTransport, ctx context.Context, name string, args any) (T, error) {
	var wrap struct {
		Result T `json:"result"`
	}
	err := t.callTool(ctx, name, args, &wrap)
	return wrap.Result, err
}

// mcpContentItemWire is one entry of CallToolResult.Content (mcp.TextContent on
// the wire: {"type":"text","text":"..."}).
type mcpContentItemWire struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// mcpCallToolResultWire is the wire shape of mcp.CallToolResult — only the
// fields the harness reads (Content for the error text, StructuredContent for
// the typed op result, IsError to discriminate).
type mcpCallToolResultWire struct {
	Content           []mcpContentItemWire `json:"content"`
	StructuredContent json.RawMessage      `json:"structuredContent"`
	IsError           bool                 `json:"isError"`
}

// mcpToolError parses a failed CallToolResult's text content — "<Kind>:
// <Detail>", the exact format mapManagerError formats on the server (see e.g.
// systemdesign's generated mcp Handler) — back into the transport-agnostic
// sentinel the equivalent HTTP status maps to (statusForKind's mirror).
func mcpToolError(tool string, result mcpCallToolResultWire) error {
	msg := ""
	if len(result.Content) > 0 {
		msg = result.Content[0].Text
	}
	kind, detail, found := strings.Cut(msg, ": ")
	if !found {
		detail = msg
	}
	switch kind {
	case "ContractMisuse":
		return fmt.Errorf("%w: mcp %s: %s", ErrBadRequest, tool, detail)
	case "NotFound":
		return fmt.Errorf("%w: mcp %s: %s", ErrNotFound, tool, detail)
	case "Unauthorized":
		return fmt.Errorf("%w: mcp %s: %s", ErrForbidden, tool, detail)
	case "FailedPrecondition":
		return fmt.Errorf("%w: mcp %s: %s", ErrConflict, tool, detail)
	case "Infrastructure":
		return fmt.Errorf("%w: mcp %s: %s", ErrUnavailable, tool, detail)
	default:
		return fmt.Errorf("mcp %s: %s", tool, msg)
	}
}

// --- raw JSON-RPC over streamable HTTP ---------------------------------------

type jsonrpcRequestWire struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcErrorWire struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcResponseWire struct {
	JSONRPC string            `json:"jsonrpc"`
	ID      *int64            `json:"id"`
	Result  json.RawMessage   `json:"result"`
	Error   *jsonrpcErrorWire `json:"error"`
}

// post sends one JSON-RPC message. id == nil sends a NOTIFICATION (no "id"
// field): the server always answers 202 Accepted with no body (§2.1.4 of the
// spec — "If the server accepts the input... MUST return HTTP status code 202
// Accepted with no body"), so the returned *jsonrpcResponseWire is nil. id !=
// nil sends a CALL: the server answers over text/event-stream (mcp_mount.go
// selects the default JSONResponse=false), one "message" SSE event carrying
// the JSON-RPC response matching id. Returns the response (nil for a
// notification), the Mcp-Session-Id response header (set only on a successful
// initialize), and any transport-level error mapped through the SAME
// statusError sentinel table httpTransport uses.
func (t *mcpTransport) post(ctx context.Context, sessionID string, id *int64, method string, params any) (*jsonrpcResponseWire, string, error) {
	body, err := json.Marshal(jsonrpcRequestWire{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, "", fmt.Errorf("marshal mcp request %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+mcpPath, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// The streamable HTTP handler requires Accept to carry BOTH media types on
	// every POST (StreamableHTTPHandler.ServeHTTP's streamableAccepts check).
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(mcpProtocolVersionHeader, mcpProtocolVersion)
	if sessionID != "" {
		req.Header.Set(mcpSessionIDHeader, sessionID)
	}

	resp, err := t.hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("mcp %s: %w", method, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	newSID := resp.Header.Get(mcpSessionIDHeader)

	if resp.StatusCode == http.StatusAccepted {
		// A notification's expected ack — no body to decode.
		return nil, newSID, nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("%w: mcp %s: status %d: %s", statusError(resp.StatusCode), method, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if id == nil {
		// A notification answered 200 with no id to match (defensive — the spec
		// promises 202, but nothing here depends on that promise).
		return nil, newSID, nil
	}

	var msg *jsonrpcResponseWire
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
		msg, err = scanSSEForResponse(resp.Body, *id)
	} else {
		msg = &jsonrpcResponseWire{}
		err = json.NewDecoder(resp.Body).Decode(msg)
	}
	if err != nil {
		return nil, newSID, fmt.Errorf("mcp %s: %w", method, err)
	}
	return msg, newSID, nil
}

// scanSSEForResponse reads Server-Sent Events off r (the SSE wire format:
// "data: <line>" fields, records terminated by a blank line — see the server's
// event.go) until it finds one whose JSON-RPC id matches want, or the stream
// ends. Only "data" fields are read; "event"/"id"/"retry" fields (and
// stream-priming events without a matching id, e.g. an interleaved server
// notification) are skipped, exactly as a spec-compliant client would.
func scanSSEForResponse(r io.Reader, want int64) (*jsonrpcResponseWire, error) {
	br := bufio.NewReader(r)
	var data []string
	flush := func() (*jsonrpcResponseWire, bool) {
		if len(data) == 0 {
			return nil, false
		}
		payload := strings.Join(data, "\n")
		data = nil
		var msg jsonrpcResponseWire
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			return nil, false // not a JSON-RPC message we recognize; keep scanning
		}
		if msg.ID != nil && *msg.ID == want {
			return &msg, true
		}
		return nil, false
	}
	for {
		line, err := br.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(trimmed, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
		case trimmed == "":
			if msg, ok := flush(); ok {
				return msg, nil
			}
		}
		if err != nil {
			if err == io.EOF {
				if msg, ok := flush(); ok {
					return msg, nil
				}
				return nil, fmt.Errorf("no matching response for request id %d in event stream", want)
			}
			return nil, err
		}
	}
}

// --- Transport implementation: UC1 (system-design / Phase-1) ----------------

func (t *mcpTransport) CreateProject(ctx context.Context, name string) (string, error) {
	return callToolResult[string](t, ctx, "systemDesignCreateProject", map[string]any{"owner": testOwner, "name": name})
}

func (t *mcpTransport) ListProjects(ctx context.Context, owner string) ([]ProjectSummary, error) {
	out, err := callToolResult[[]projectSummaryWire](t, ctx, "systemDesignListProjects", map[string]any{"owner": owner})
	if err != nil {
		return nil, err
	}
	summaries := make([]ProjectSummary, 0, len(out))
	for _, s := range out {
		summaries = append(summaries, ProjectSummary(s))
	}
	return summaries, nil
}

func (t *mcpTransport) SetResearchInput(ctx context.Context, projectID string, sources []ResearchSource) error {
	return t.callTool(ctx, "systemDesignSetResearchInput", map[string]any{
		"projectID": projectID,
		"research":  map[string]any{"sources": sources},
	}, nil)
}

func (t *mcpTransport) StartDesign(ctx context.Context, projectID string) (string, error) {
	return callToolResult[string](t, ctx, "systemDesignStartSystemDesign", map[string]any{"projectID": projectID})
}

func (t *mcpTransport) RequestArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	return callToolResult[string](t, ctx, "systemDesignRequestArtifactDraft", map[string]any{
		"projectID": projectID, "kind": artifactKindOrdinal(kind),
	})
}

func (t *mcpTransport) GetSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	out, err := callToolResult[sessionStateWire](t, ctx, "systemDesignGetSessionState", map[string]any{
		"projectID": projectID, "kind": artifactKindOrdinal(kind),
	})
	if err != nil {
		return SessionState{}, false, err
	}
	return SessionState{
		ProjectID:     out.ProjectID,
		ArtifactKind:  artifactKindName(out.ArtifactKind),
		Stage:         stageName(systemSessionStageByOrdinal, out.Stage),
		FailureReason: strPtrVal(out.FailureReason),
	}, true, nil
}

func (t *mcpTransport) SubmitReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	args := map[string]any{
		"projectID": projectID,
		"kind":      artifactKindOrdinal(kind),
		"decision":  reviewDecisionToOrdinal[decision],
	}
	if feedback != "" {
		args["feedback"] = reviewFeedbackBody{Notes: feedback}
	}
	return t.callTool(ctx, "systemDesignSubmitReviewDecision", args, nil)
}

func (t *mcpTransport) AdvancePhase(ctx context.Context, projectID string) (bool, []string, error) {
	out, err := callToolResult[phaseAdvanceWire](t, ctx, "systemDesignAdvancePhase", map[string]any{
		"projectID": projectID, "acknowledgeStale": false,
	})
	return out.Advanced, decodeMissingArtifacts(out.MissingArtifacts), err
}

// --- Transport implementation: UC2 (project-design / Phase-2) ---------------

func (t *mcpTransport) RequestProjectArtifactDraft(ctx context.Context, projectID, kind string) (string, error) {
	return callToolResult[string](t, ctx, "projectDesignRequestArtifactDraft", map[string]any{
		"projectID": projectID, "kind": artifactKindOrdinal(kind),
	})
}

func (t *mcpTransport) GetProjectSessionState(ctx context.Context, projectID, kind string) (SessionState, bool, error) {
	out, err := callToolResult[sessionStateWire](t, ctx, "projectDesignGetSessionState", map[string]any{
		"projectID": projectID, "kind": artifactKindOrdinal(kind),
	})
	if err != nil {
		return SessionState{}, false, err
	}
	return SessionState{
		ProjectID:     out.ProjectID,
		ArtifactKind:  artifactKindName(out.ArtifactKind),
		Stage:         stageName(projectSessionStageByOrdinal, out.Stage),
		FailureReason: strPtrVal(out.FailureReason),
	}, true, nil
}

func (t *mcpTransport) SubmitProjectReview(ctx context.Context, projectID, kind, decision, feedback string) error {
	args := map[string]any{
		"projectID": projectID,
		"kind":      artifactKindOrdinal(kind),
		"decision":  reviewDecisionToOrdinal[decision],
	}
	if feedback != "" {
		args["feedback"] = reviewFeedbackBody{Notes: feedback}
	}
	return t.callTool(ctx, "projectDesignSubmitReviewDecision", args, nil)
}

func (t *mcpTransport) RequestSDPCommit(ctx context.Context, projectID string) (string, error) {
	return callToolResult[string](t, ctx, "projectDesignRequestSDPCommit", map[string]any{"projectID": projectID})
}

func (t *mcpTransport) SubmitSDPDecision(ctx context.Context, projectID, decision, optionID, feedback string) error {
	args := map[string]any{
		"projectID": projectID,
		"decision":  sdpDecisionToOrdinal[decision],
	}
	if optionID != "" {
		args["optionID"] = optionID
	}
	if feedback != "" {
		args["feedback"] = reviewFeedbackBody{Notes: feedback}
	}
	return t.callTool(ctx, "projectDesignSubmitSDPDecision", args, nil)
}

func (t *mcpTransport) AdvanceToConstruction(ctx context.Context, projectID string) (bool, []string, error) {
	out, err := callToolResult[phaseAdvanceWire](t, ctx, "projectDesignAdvanceToConstruction", map[string]any{
		"projectID": projectID, "acknowledgeStale": false,
	})
	return out.Advanced, decodeMissingArtifacts(out.MissingArtifacts), err
}

// --- Transport implementation: UC3 (construction / Phase-3) -----------------

func (t *mcpTransport) ExecuteNextActivity(ctx context.Context, projectID, tickID string) (bool, string, error) {
	out, err := callToolResult[pumpResultWire](t, ctx, "constructionExecuteNextActivity", map[string]any{
		"projectID": projectID, "tickID": tickID,
	})
	activityID := ""
	if out.ActivityID != nil {
		activityID = *out.ActivityID
	}
	return out.Dispatched, activityID, err
}

func (t *mcpTransport) GetConstructionSessionState(ctx context.Context, projectID, activityID string) (ConstructionSessionState, error) {
	out, err := callToolResult[constructionSessionViewWire](t, ctx, "constructionGetSessionState", map[string]any{
		"projectID": projectID, "activityID": activityID,
	})
	if err != nil {
		return ConstructionSessionState{}, err
	}
	state := ConstructionSessionState{ProjectID: out.ProjectID, ActivityID: out.ActivityID, Stage: stageName(constructionStageByOrdinal, out.Stage)}
	if out.PipelinePhase != nil {
		state.PipelinePhase = stageName(pipelinePhaseByOrdinal, *out.PipelinePhase)
	}
	return state, nil
}

func (t *mcpTransport) SubmitPhaseDecision(ctx context.Context, projectID, activityID, phase, decision, feedback string) error {
	args := map[string]any{
		"projectID":  projectID,
		"activityID": activityID,
		"phase":      phase,
		"decision":   phaseDecisionToOrdinal[decision],
	}
	if feedback != "" {
		args["feedback"] = reviewFeedbackBody{Notes: feedback}
	}
	return t.callTool(ctx, "constructionSubmitPhaseDecision", args, nil)
}

func (t *mcpTransport) UpdateReviewPolicy(ctx context.Context, projectID string, gatedPhasesByType map[string][]string) error {
	return t.callTool(ctx, "constructionUpdateReviewPolicy", map[string]any{
		"projectID": projectID,
		"policy":    map[string]any{"gatedPhasesByType": gatedPhasesByType},
	}, nil)
}

// --- Transport implementation: UC4 (operations / Phase-4) -------------------

type mcpDeployResultWire struct {
	Published bool    `json:"published"`
	Revision  *string `json:"revision,omitempty"`
}

type mcpReconcileResultWire struct {
	Observed    int64 `json:"observed"`
	Transitions int64 `json:"transitions"`
	Republished int64 `json:"republished"`
}

type mcpWithdrawResultWire struct {
	Withdrawn bool `json:"withdrawn"`
}

func (t *mcpTransport) DeployAfterConstruction(ctx context.Context, operatedAppID string, change DesiredStateChange) (bool, string, error) {
	out, err := callToolResult[mcpDeployResultWire](t, ctx, "operationsDeployAfterConstruction", map[string]any{
		"operatedAppID": operatedAppID,
		"change": map[string]any{
			"reason":               desiredStateReasonToOrdinal[change.Reason],
			"patchKind":            patchKindToOrdinal[change.PatchKind],
			"changeId":             change.ChangeID,
			"renderedDesiredState": change.RenderedDesiredState,
		},
	})
	revision := ""
	if out.Revision != nil {
		revision = *out.Revision
	}
	return out.Published, revision, err
}

func (t *mcpTransport) ReconcileOperatedState(ctx context.Context, tickID string, appIDs []string) (int64, int64, int64, error) {
	args := map[string]any{"tickID": tickID}
	if appIDs != nil {
		args["scope"] = map[string]any{"appIds": appIDs}
	}
	out, err := callToolResult[mcpReconcileResultWire](t, ctx, "operationsReconcileOperatedState", args)
	return out.Observed, out.Transitions, out.Republished, err
}

func (t *mcpTransport) QueryOperatedSystemView(ctx context.Context, operatedAppID, requestID string) (OperatedSystemView, error) {
	out, err := callToolResult[operatedSystemViewWire](t, ctx, "operationsQueryOperatedSystemView", map[string]any{
		"operatedAppID": operatedAppID, "requestID": requestID,
	})
	if err != nil {
		return OperatedSystemView{}, err
	}
	return OperatedSystemView{
		OperatedAppID: out.OperatedAppID,
		Phase:         stageName(runtimeStatusByOrdinal, out.Phase),
		InFlight:      out.InFlight,
	}, nil
}

func (t *mcpTransport) ApplyDelinquencyPolicy(ctx context.Context, customerID string, pauseNotWithdraw bool) error {
	return t.callTool(ctx, "operationsApplyDelinquencyPolicy", map[string]any{
		"customerID":         customerID,
		"delinquencyContext": map[string]any{"pauseNotWithdraw": pauseNotWithdraw},
	}, nil)
}

func (t *mcpTransport) WithdrawSystem(ctx context.Context, operatedAppID, changeID, notes string) (bool, error) {
	out, err := callToolResult[mcpWithdrawResultWire](t, ctx, "operationsWithdrawSystem", map[string]any{
		"operatedAppID": operatedAppID,
		"changeID":      changeID,
		"reason":        map[string]any{"notes": notes},
	})
	return out.Withdrawn, err
}
