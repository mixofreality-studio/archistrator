package main

import (
	"context"
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
