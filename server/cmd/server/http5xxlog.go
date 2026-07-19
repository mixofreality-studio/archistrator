// http5xxlog.go is the composition-root HTTP observability seam over the generated
// /api/v1 surface (F-QA2-46). The generated handlers' writeManagerError (see
// internal/client/web/*/*_handlers.gen.go) writes every manager-fault 5xx — e.g.
// the Infrastructure-kind 503 a lost Temporal response surfaces — with ZERO
// server-side logging, so a founder-visible fault could leave no server trace.
// The manager logging wrap (managerlog.go) covers Infrastructure-kind
// *manager.Error values, but not the generated fallback 500 ("internal error")
// nor any other path that writes a 5xx status directly.
//
// log5xxResponses closes that gap at the transport: one WARN per >= 500 response
// with method, path, status, and a bounded snippet of the response body (the
// manager error Detail JSON). It is mounted from the hand ExtraMounts hook
// (hooks.go) — the generated files stay untouched, per the client-layer codegen
// convention. Like managerlog.go, this file lives OUTSIDE internal/, pure
// composition-root wiring glue.
package main

import (
	"log/slog"
	"net/http"
)

// fault5xxBodyCap bounds the response-body snippet logged for a 5xx. The manager
// error envelope is a small JSON object; the cap only guards the log line against
// a pathological body.
const fault5xxBodyCap = 1024

// log5xxResponses wraps next so every response with status >= 500 is logged WARN
// with method, path, status, and a bounded body snippet. Sub-500 responses pass
// through with no logging and no body buffering.
func log5xxResponses(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := &statusCapturingWriter{ResponseWriter: w}
		next.ServeHTTP(cw, r)
		if cw.status >= http.StatusInternalServerError {
			logger.Warn("5xx response on web API surface",
				"method", r.Method,
				"path", r.URL.Path,
				"status", cw.status,
				"body", string(cw.snippet))
		}
	})
}

// statusCapturingWriter is the minimal ResponseWriter wrapper backing
// log5xxResponses: it records the response status and tees a bounded snippet of
// the body ONLY once a 5xx status is known. The /api/v1 surface is plain JSON
// request/response (no hijack/flush streaming), so no optional interfaces are
// forwarded.
type statusCapturingWriter struct {
	http.ResponseWriter
	status  int
	snippet []byte
}

func (w *statusCapturingWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCapturingWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		// First Write without an explicit WriteHeader is an implicit 200.
		w.status = http.StatusOK
	}
	if w.status >= http.StatusInternalServerError && len(w.snippet) < fault5xxBodyCap {
		room := min(fault5xxBodyCap-len(w.snippet), len(b))
		w.snippet = append(w.snippet, b[:room]...)
	}
	return w.ResponseWriter.Write(b)
}
