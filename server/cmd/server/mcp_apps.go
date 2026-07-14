// MCP Apps support: serves the single ui:// shell resource. Composition-root
// glue like mcp_mount.go — lives outside internal/, may import the SDK.
// The stub is a constant, config-templated string: assets live on the webApp
// origin (nginx); this server does no asset fetching or caching (spec §3.4/§3.5).
package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	shellResourceURI = "ui://archistrator/shell.html"
	shellMIMEType    = "text/html;profile=mcp-app"
)

// shellStubHTML renders the MCP-Apps shell stub: CLASSIC (non-module,
// non-crossorigin) <script>/<link> tags pointing at the webApp origin, so the
// asset requests are CORS-exempt (spec §3.4). webAppOrigin/assetVersion are
// config, not fetched or cached here — this server is a pure protocol shim.
func shellStubHTML(webAppOrigin, assetVersion string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>archistrator</title>
<link rel="stylesheet" href="%[1]s/mcp-app.css?v=%[2]s">
</head>
<body>
<div id="root"></div>
<script src="%[1]s/mcp-app.js?v=%[2]s"></script>
</body>
</html>`, webAppOrigin, assetVersion)
}

// registerShellResource registers the ui://archistrator/shell.html resource on
// srv. The handler returns the constant, config-templated stub — no file reads,
// no fetching, no caching.
func registerShellResource(srv *mcp.Server, webAppOrigin, assetVersion string) {
	srv.AddResource(&mcp.Resource{
		URI:      shellResourceURI,
		Name:     "archistrator-shell",
		MIMEType: shellMIMEType,
		Meta: mcp.Meta{"ui": map[string]any{
			"csp":           map[string]any{"resourceDomains": []string{webAppOrigin}},
			"prefersBorder": true,
		}},
	}, func(_ context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI:      shellResourceURI,
			MIMEType: shellMIMEType,
			Text:     shellStubHTML(webAppOrigin, assetVersion),
		}}}, nil
	})
}
