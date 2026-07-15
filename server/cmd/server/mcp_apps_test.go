package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
)

func TestShellStubHTML(t *testing.T) {
	got := shellStubHTML("https://app.example.com", "42")
	if strings.Contains(got, "module") || strings.Contains(got, "crossorigin") {
		t.Error("stub must use classic CORS-exempt tags (no module/crossorigin)")
	}
	for _, want := range []string{
		`<!DOCTYPE html>`,
		// CLASSIC tags — no type="module", no crossorigin: CORS-exempt (spec §3.4)
		`<script src="https://app.example.com/mcp-app.js?v=42"></script>`,
		`<link rel="stylesheet" href="https://app.example.com/mcp-app.css?v=42">`,
		`<div id="root"></div>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stub missing %q\n---\n%s", want, got)
		}
	}
}

func TestDevCORSPreflight(t *testing.T) {
	h := devCORS(true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("OPTIONS", "/mcp", nil)
	req.Header.Set("Origin", "http://localhost:8080")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("missing ACAO header: %v", rr.Header())
	}
	// DELETE tears down a streamable-HTTP session (Mcp-Session-Id) — must be in the
	// allow-list or a browser host's teardown DELETE never reaches the transport.
	if methods := rr.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(methods, "DELETE") {
		t.Fatalf("Allow-Methods missing DELETE (session teardown): %q", methods)
	}
	// prod profile: no header
	h = devCORS(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("prod must not set CORS headers")
	}
}

// TestMCPAppsSeam proves the full MCP-Apps wiring end to end over a real (in
// memory) MCP session, following TestMCPMountInitializeAndListTools's pattern
// (server/cmd/server/mcp_mount_test.go): (1) initialize + tools/list finds
// systemDesignGetSessionState and asserts its _meta ui.resourceUri/ui.view match
// what mcpemit stamps (mcpemit.go) and what project.json declares for this
// artifact's view (ui.view: "system-design-session" — see the ratified spec,
// "the same view ids drive the registry"); (2) resources/read that URI returns
// the shell stub with the MCP-Apps mimetype and the fixed asset names the webApp
// build emits (mcp-app.js/mcp-app.css — see mcp_apps.go's shellStubHTML).
// Managers are nil — neither step invokes a manager, so no backing infra is
// required (mirrors TestMCPMountInitializeAndListTools).
func TestMCPAppsSeam(t *testing.T) {
	const webAppOrigin = "https://app.example.com"
	const assetVersion = "42"
	handler := newMCPHandler(web.DevConfig{Enabled: true}, nil, nil, nil, nil, nil, webAppOrigin, assetVersion)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "archistrator-test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("MCP initialize against /mcp failed: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list against /mcp failed: %v", err)
	}
	var tool *mcp.Tool
	for _, tl := range res.Tools {
		if tl.Name == "systemDesignGetSessionState" {
			tool = tl
			break
		}
	}
	if tool == nil {
		t.Fatalf("tools/list missing systemDesignGetSessionState; got: %s", strings.Join(toolNames(res.Tools), ", "))
	}
	ui, ok := tool.Meta["ui"].(map[string]any)
	if !ok {
		t.Fatalf("systemDesignGetSessionState._meta.ui missing or wrong shape: %#v", tool.Meta["ui"])
	}
	if got := ui["resourceUri"]; got != shellResourceURI {
		t.Errorf("_meta.ui.resourceUri = %v, want %q", got, shellResourceURI)
	}
	if got := ui["view"]; got != "system-design-session" {
		t.Errorf("_meta.ui.view = %v, want %q", got, "system-design-session")
	}

	rres, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: shellResourceURI})
	if err != nil {
		t.Fatalf("resources/read %q failed: %v", shellResourceURI, err)
	}
	if len(rres.Contents) != 1 {
		t.Fatalf("resources/read %q returned %d contents, want 1", shellResourceURI, len(rres.Contents))
	}
	content := rres.Contents[0]
	if content.MIMEType != shellMIMEType {
		t.Errorf("resource mimetype = %q, want %q", content.MIMEType, shellMIMEType)
	}
	for _, want := range []string{"mcp-app.js", "mcp-app.css"} {
		if !strings.Contains(content.Text, want) {
			t.Errorf("resource body missing fixed asset name %q:\n%s", want, content.Text)
		}
	}
}

// mustListTools spins up the same in-memory MCP mount as TestMCPAppsSeam /
// TestMCPMountInitializeAndListTools and returns the live tools/list result.
// Managers are nil — tools/list never invokes a manager, so no backing infra
// is required.
func mustListTools(t *testing.T) []*mcp.Tool {
	t.Helper()
	handler := newMCPHandler(web.DevConfig{Enabled: true}, nil, nil, nil, nil, nil, "http://localhost:5173", "dev")
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "archistrator-test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("MCP initialize against /mcp failed: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list against /mcp failed: %v", err)
	}
	return res.Tools
}

// TestMCPToolsListUUIDParamSchema proves the generator's fixUUIDStrings fix
// (T11 finding F-T11-1): a uuid.UUID input param must be typed as a JSON
// string with format "uuid" — matching what the value actually marshals as on
// the wire — instead of the SDK's structural [16]byte-array inference, which
// the prior relaxRawJSON pass then blanked to bare JSON `true` (rejected by
// the TS MCP SDK's Zod validator, which requires an object).
//
// mcp.Tool.InputSchema/OutputSchema are declared `any` on the wire type (not
// *jsonschema.Schema), so the client-decoded value is generic JSON
// (map[string]any / bool) — this asserts against that shape, exactly what a
// real TS SDK host sees.
func TestMCPToolsListUUIDParamSchema(t *testing.T) {
	const toolName = "operationsApplyDelinquencyPolicy"
	var tool *mcp.Tool
	for _, tl := range mustListTools(t) {
		if tl.Name == toolName {
			tool = tl
			break
		}
	}
	if tool == nil {
		t.Fatalf("tools/list missing %q", toolName)
	}
	inSchema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("%s inputSchema is not a JSON object: %#v", toolName, tool.InputSchema)
	}
	props, ok := inSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s inputSchema.properties is not a JSON object: %#v", toolName, inSchema["properties"])
	}
	prop, ok := props["customerID"]
	if !ok {
		t.Fatalf("%s inputSchema.properties missing customerID", toolName)
	}
	got, err := json.Marshal(prop)
	if err != nil {
		t.Fatalf("marshal customerID schema: %v", err)
	}
	var gotMap map[string]any
	if err := json.Unmarshal(got, &gotMap); err != nil {
		t.Fatalf("customerID schema %s did not unmarshal as a JSON object: %v", got, err)
	}
	want := map[string]any{"format": "uuid", "type": "string"}
	if len(gotMap) != len(want) || gotMap["format"] != want["format"] || gotMap["type"] != want["type"] {
		t.Errorf("customerID schema = %s, want %v", got, want)
	}
}

// TestMCPToolsListNoBooleanPropertySchemas is the regression test for the Zod
// failure class behind F-T11-1: the TS MCP SDK rejects a tools/list response
// where any property schema is the JSON boolean `true` (the zero-value
// jsonschema.Schema{} marshaled by the library) — `true` is valid JSON
// Schema, but Zod requires every property schema to be an object. Walk every
// tool's input and output schema tree, as decoded into generic JSON (matching
// what a real client sees), and fail on any node reachable via `properties`
// that is a bare boolean.
func TestMCPToolsListNoBooleanPropertySchemas(t *testing.T) {
	for _, tool := range mustListTools(t) {
		walkSchemaProperties(t, tool.Name+".inputSchema", tool.InputSchema)
		walkSchemaProperties(t, tool.Name+".outputSchema", tool.OutputSchema)
	}
}

// walkSchemaProperties walks a generic-JSON-decoded schema node (map[string]any
// for an object schema, bool for `true`/`false`) and fails on any property
// value that is a bare boolean.
func walkSchemaProperties(t *testing.T, path string, node any) {
	t.Helper()
	if node == nil {
		return
	}
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for name, p := range props {
			propPath := fmt.Sprintf("%s.properties.%s", path, name)
			if _, isBool := p.(bool); isBool {
				t.Errorf("%s is JSON boolean %v, not an object (TS SDK Zod validator rejects this)", propPath, p)
				continue
			}
			walkSchemaProperties(t, propPath, p)
		}
	}
	walkSchemaProperties(t, path+".items", m["items"])
	walkSchemaProperties(t, path+".additionalProperties", m["additionalProperties"])
	if prefix, ok := m["prefixItems"].([]any); ok {
		for i, p := range prefix {
			walkSchemaProperties(t, fmt.Sprintf("%s.prefixItems[%d]", path, i), p)
		}
	}
}
