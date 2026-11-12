package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	billingmcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/billing"
	deliverymcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/delivery"
	operationsmcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
)

// TestMCPMountInitializeAndListTools proves the /mcp transport answers a real MCP
// session in dev mode (tokenless): the SDK client performs the initialize
// handshake over streamable HTTP against the mounted handler, then tools/list
// returns the union of every generated per-manager tool. Managers are nil — the
// initialize + tools/list path enumerates the registered tools without invoking
// any manager, so no backing infra (Temporal/Postgres) is required.
func TestMCPMountInitializeAndListTools(t *testing.T) {
	// Dev mode: AuthMiddleware injects a principal and validates no token, so a
	// nil validator is fine (mirrors the local ARCHISTRATOR_AUTH_DEV_MODE=true run).
	handler := composedMCPRoot(t, "http://localhost:5173", "dev")
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "archistrator-test-client", Version: "0.0.1"}, nil)
	// Connect performs the JSON-RPC initialize handshake (POST /mcp) and returns
	// the live session; a nil result here means the mount does not speak MCP.
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("MCP initialize against /mcp failed: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list against /mcp failed: %v", err)
	}

	got := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}

	// One representative tool per web-wired manager: all three Handler sets must be
	// registered on the one server behind /mcp.
	want := []string{
		"billingGetBillingCycle",
		"billingOnboardPaymentIntegration",
		"billingRegisterCustomer",
		"deliveryStartProject",
		"deliveryDispatchActivityTask",
		"deliveryExecuteNextActivity",
		"operationsDeployAfterConstruction",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tools/list missing %q; got %d tools: %s", name, len(res.Tools), strings.Join(toolNames(res.Tools), ", "))
		}
	}
	// billingManager reaches MCP with the ops a use case draws from a client — the
	// cycle read and onboarding's two entries (spec §12 B7) — and nothing else: the
	// scheduler-driven ops stay on its Temporal worker.
	for _, name := range []string{"billingCloseBillingCycle", "billingRunShortfallSweep"} {
		if got[name] {
			t.Errorf("tools/list exposes the scheduler-driven %q", name)
		}
	}
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

// testSecurity is the edge security the composition root serves with (the generated
// edgeSecurity over the generated policy), for tests that mount the transports directly.
func testSecurity() security.Security { return edgeSecurity() }

// mountGeneratedMCP reproduces, line for line, the /mcp mount main.gen.go builds before it
// calls hooks.ExtraMounts: every generated tool table on one mcp.Server, the transport behind
// the auth boundary, and the whole thing handed through the MCPMount hook. Tests that mount
// the transports directly must go through it, so they see the composition root's real order
// (the generated literal "/mcp" pattern first, the hand routes after).
func mountGeneratedMCP(root *http.ServeMux, hooks Hooks, dev web.DevConfig, validator security.Validator, sec security.Security, m WebManagers) {
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: serviceName, Version: "1.0.0"}, nil)
	(&billingmcp.Handler{Manager: m.BillingManager, Security: sec}).Register(mcpServer)
	(&deliverymcp.Handler{Manager: m.DeliveryManager, Security: sec}).Register(mcpServer)
	(&operationsmcp.Handler{Manager: m.OperationsManager, Security: sec}).Register(mcpServer)
	root.Handle("/mcp", hooks.MCPMount(mcpServer, dev, web.AuthMiddleware(dev, validator)(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, nil))))
}

// composedMCPRoot is the real composed mux in main.gen.go's order: the generated server at "/",
// the generated /mcp mount, then the hand routes (ExtraMounts) — in dev mode, with nil Managers
// (tools/list and resources/read never invoke one). The shell resource is keyed by the two
// Config values (webappOrigin / webappAssetVersion), set here.
func composedMCPRoot(t *testing.T, webAppOrigin, assetVersion string) http.Handler {
	t.Helper()
	dev := web.DevConfig{Enabled: true, Principal: devPrincipal()}
	root := http.NewServeMux()
	root.Handle("/", web.NewServer(dev, nil))
	h := newTestAppHooks()
	h.config = &Config{WebappOrigin: webAppOrigin, WebappAssetVersion: assetVersion}
	mountGeneratedMCP(root, h, dev, nil, testSecurity(), WebManagers{})
	h.ExtraMounts(root, &Config{}, dev, nil, testSecurity(), WebManagers{})
	return root
}

// TestComposedMCPMountCarriesTheHandSeams is the regression for the dead hand mount: the
// generated "/mcp" is a literal pattern, so a second "/mcp" the hand routes registered on the
// inner mux (behind the wildcard frame-denial binding) was never selected, and the served /mcp
// silently lost the shell resource, the dev CORS and the frame denial. Driving the real order
// asserts all three reach the served /mcp.
func TestComposedMCPMountCarriesTheHandSeams(t *testing.T) {
	ts := httptest.NewServer(composedMCPRoot(t, "https://app.example.com", "42"))
	defer ts.Close()

	// Dev CORS: the preflight is answered before auth and carries the browser-host headers.
	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://localhost:8080")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS /mcp status = %d, want %d (the hand CORS preflight)", resp.StatusCode, http.StatusNoContent)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("OPTIONS /mcp Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
	assertFrameDenied(t, "OPTIONS /mcp", resp.Header)

	// Frame denial on the transport's own responses too (a POST with no Content-Type is
	// rejected by the SDK transport itself).
	resp, err = http.Post(ts.URL+"/mcp", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("POST /mcp status = %d, want %d (the generated transport answered)", resp.StatusCode, http.StatusUnsupportedMediaType)
	}
	assertFrameDenied(t, "POST /mcp", resp.Header)

	// The shell resource, keyed by the composition-root env values, is on the served server.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "archistrator-test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("MCP initialize against /mcp failed: %v", err)
	}
	defer func() { _ = session.Close() }()
	rres, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: shellResourceURI})
	if err != nil {
		t.Fatalf("resources/read %q failed: %v", shellResourceURI, err)
	}
	if len(rres.Contents) != 1 || !strings.Contains(rres.Contents[0].Text, "https://app.example.com/mcp-app.js?v=42") {
		t.Errorf("the served shell resource is not keyed by the composition-root origin/version: %+v", rres.Contents)
	}
}
