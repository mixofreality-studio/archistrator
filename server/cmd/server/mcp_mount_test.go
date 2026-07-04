package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	handler := newMCPHandler(web.DevConfig{Enabled: true}, nil, nil, nil, nil, nil)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "archistrator-test-client", Version: "0.0.1"}, nil)
	// Connect performs the JSON-RPC initialize handshake (POST /mcp) and returns
	// the live session; a nil result here means the mount does not speak MCP.
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
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

	// One representative tool per web-wired manager: all four Handler sets must be
	// registered on the one server behind /mcp.
	want := []string{
		"systemDesignStartSystemDesign",
		"projectDesignRequestSDPCommit",
		"constructionExecuteNextActivity",
		"operationsDeployAfterConstruction",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tools/list missing %q; got %d tools: %s", name, len(res.Tools), strings.Join(toolNames(res.Tools), ", "))
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
