// MCP transport mount. This file is the composition-root glue that exposes the
// four web-wired managers over the Model Context Protocol, mirroring the REST
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

	constructionmcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/construction"
	operationsmcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/operations"
	projectdesignmcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/projectdesign"
	systemdesignmcp "github.com/mixofreality-studio/archistrator/server/internal/client/mcp/systemdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/construction"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/operations"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign"
	"github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// newMCPServer builds the single MCP server carrying every generated tool. Tool
// names are manager-namespaced (systemDesign*/projectDesign*/construction*/
// operations*), so registering all four Handlers on one server never collides.
// webAppOrigin/assetVersion configure the ui://archistrator/shell.html resource
// (mcp_apps.go) registered alongside the four generated tool Handlers.
func newMCPServer(
	sd systemdesign.SystemDesignManager,
	pd projectdesign.ProjectDesignManager,
	cons construction.ConstructionManager,
	ops operations.OperationsManager,
	webAppOrigin, assetVersion string,
) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "archistrator-server", Version: "1.0.0"}, nil)
	(&systemdesignmcp.Handler{Manager: sd}).Register(srv)
	(&projectdesignmcp.Handler{Manager: pd}).Register(srv)
	(&constructionmcp.Handler{Manager: cons}).Register(srv)
	(&operationsmcp.Handler{Manager: ops}).Register(srv)
	registerShellResource(srv, webAppOrigin, assetVersion)
	return srv
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
	sd systemdesign.SystemDesignManager,
	pd projectdesign.ProjectDesignManager,
	cons construction.ConstructionManager,
	ops operations.OperationsManager,
	webAppOrigin, assetVersion string,
) http.Handler {
	srv := newMCPServer(sd, pd, cons, ops, webAppOrigin, assetVersion)
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	return web.AuthMiddleware(dev, validator)(transport)
}
