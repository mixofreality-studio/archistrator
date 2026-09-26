// MCP transport mount. This file is the composition-root glue that exposes the
// two web-wired managers over the Model Context Protocol, mirroring the REST
// wiring in main.go (web.NewServer): it constructs ONE mcp.Server, registers
// every generated per-manager tool Handler against the SAME manager instances
// the REST Handlers use, and returns the SDK's streamable-HTTP transport wrapped
// in the SAME auth middleware as /api/v1 (web.AuthMiddleware — dev mode injects a
// principal with no token; prod validates the bearer token, nil validator denies).
//
// Like main.go this lives OUTSIDE internal/, so it is not scanned by the Method
// arch checker and may freely import the MCP SDK transport. The generated tool
// Handlers (internal/client/mcp/*) are the only things the arch checker allows to
// import the SDK; this root mounts them.
package main

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	deliverymcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/delivery"
	operationsmcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/delivery"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// newMCPServer builds the single MCP server carrying every generated tool. Tool
// names are manager-namespaced (delivery*/operations*), so registering both
// Handlers on one server never collides. webAppOrigin/assetVersion configure the
// ui://archistrator/shell.html resource (mcp_apps.go) registered alongside the
// two generated tool Handlers.
func newMCPServer(
	del delivery.DeliveryManager,
	ops operations.OperationsManager,
	webAppOrigin, assetVersion string,
) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "archistrator-server", Version: "1.0.0"}, nil)
	(&deliverymcp.Handler{Manager: del}).Register(srv)
	(&operationsmcp.Handler{Manager: ops}).Register(srv)
	registerShellResource(srv, webAppOrigin, assetVersion)
	return srv
}

// devCORS permits browser-based MCP hosts (the ext-apps basic-host pilot
// harness) to call /mcp cross-origin. DEV ONLY: production hosts call
// server-to-server; enabled strictly rides the dev-mode auth flag.
func devCORS(enabled bool, next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// DELETE tears down a streamable-HTTP session (Mcp-Session-Id) — without it
		// in the allow-list, a browser host's session-teardown DELETE is blocked by
		// the CORS preflight before it ever reaches the transport.
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id, Mcp-Protocol-Version")
		w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// newMCPHandler returns the http.Handler mounted at /mcp: the SDK streamable-HTTP
// transport in front of the shared MCP server, wrapped by the SAME auth boundary
// as the REST surface. The dev-mode principal (or the validated bearer principal)
// is stashed on the request context by AuthMiddleware and flows into each tool
// handler's ctx, exactly as it does for the REST handlers (they both read it via
// security.PrincipalFrom).
func newMCPHandler(
	dev web.DevConfig,
	validator security.Validator,
	del delivery.DeliveryManager,
	ops operations.OperationsManager,
	webAppOrigin, assetVersion string,
) http.Handler {
	srv := newMCPServer(del, ops, webAppOrigin, assetVersion)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	return devCORS(dev.Enabled, web.AuthMiddleware(dev, validator)(transport))
}
