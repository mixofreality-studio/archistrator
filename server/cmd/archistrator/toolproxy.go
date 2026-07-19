package main

// toolproxy.go bridges the stdio mcp.Server `archistrator mcp` exposes to
// Claude Code onto the SAME tool catalog the spawned archistrator-server
// child's own `/mcp` streamable-HTTP mount exposes (server/cmd/server/mcp_mount.go's
// newMCPServer — systemDesign/projectDesign/construction/operations, each
// namespaced, registered on ONE *mcp.Server there): connect an mcp.Client to
// the child, list its tools ONCE at startup, and register each one on the
// local server with a handler that forwards CallTool verbatim to the child's
// session. This reuses the REAL tool catalog byte-for-byte — no tool is
// redeclared or reimplemented here, only relayed — for exactly the same
// "cannot import package main" reason serverchild.go documents: relaying a
// live connection is how a SEPARATE binary shares another binary's exposed
// surface.
//
// MCP Apps (mcp_apps.go's ui://archistrator/shell.html resource) is
// deliberately NOT proxied: the plan doc is explicit that MCP Apps is not a
// surface here (Claude Code doesn't render ui://; the upstream ext-apps#671
// bug blocks claude.ai/Desktop rendering too) — only ListTools/CallTool are
// bridged.
import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpVersion is the Implementation.Version this binary reports over both the
// local stdio mcp.Server and (via toolproxy) the upstream client connection —
// bumped alongside archistrator releases; a literal for now (no release
// tooling wired to this cmd yet).
const mcpVersion = "0.1.0"

// connectUpstream opens an mcp.Client session to the archistrator-server
// child's streamable-HTTP /mcp mount at http://addr/mcp.
func connectUpstream(ctx context.Context, addr string) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "archistrator-cli", Version: mcpVersion}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: "http://" + addr + "/mcp"}
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to archistrator-server /mcp at %s: %w", addr, err)
	}
	return cs, nil
}

// mountProxiedTools lists upstream's tools once and registers each on local
// as a pure relay: CallTool(name, args) → upstream.CallTool(name, args),
// result returned verbatim. Returns the number of tools mounted.
func mountProxiedTools(ctx context.Context, local *mcp.Server, upstream *mcp.ClientSession) (int, error) {
	listed, err := upstream.ListTools(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("list upstream tools: %w", err)
	}
	for _, t := range listed.Tools {
		tool := t // capture
		local.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args any
			if req.Params != nil {
				args = req.Params.Arguments
			}
			res, err := upstream.CallTool(ctx, &mcp.CallToolParams{Name: tool.Name, Arguments: args})
			if err != nil {
				return nil, fmt.Errorf("archistrator-server: %s: %w", tool.Name, err)
			}
			return res, nil
		})
	}
	return len(listed.Tools), nil
}
