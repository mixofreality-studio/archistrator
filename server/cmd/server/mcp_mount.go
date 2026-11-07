// MCP transport wrap. The generated composition root (main.gen.go) owns the "/mcp"
// mount: it builds ONE mcp.Server, registers every generated per-manager tool table
// against the SAME manager instances the REST handlers use, and mounts the SDK's
// streamable-HTTP transport behind the SAME auth boundary as /api/v1. The hand half of
// the surface (the ui://archistrator/shell.html resource, the dev-only CORS, the
// anti-framing headers) rides the generated MCPMount hook (appHooks.MCPMount in hooks.go),
// which is why this file holds only the wrap helper it calls.
//
// Like main.go this lives OUTSIDE internal/, so it is not scanned by the Method
// arch checker.
package main

import (
	"net/http"
)

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
