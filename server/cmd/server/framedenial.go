package main

// framedenial.go is the anti-clickjacking seam: every response this server
// writes — the embedded SPA shell and its assets (local profile), the REST API,
// /api/userinfo, /mcp, the health probes, and every 404 — carries
//
//	Content-Security-Policy: frame-ancestors 'none'
//	X-Frame-Options: DENY
//
// so no other site can frame archistrator and trick a signed-in user into
// clicking Approve, Begin, Pause or Override. X-Frame-Options is the legacy
// header; frame-ancestors is the modern one, and it wins wherever both are
// supported.
//
// Nothing here is relaxed for the SPA's own preview frames: a srcdoc iframe
// has an opaque origin and never loads a URL from this origin, so it does not
// need this origin to be frameable.
//
// Coverage is structural, not per-route: ExtraMounts (hooks.go) registers every
// route on an INNER mux, and denyFramingOnEveryPath binds that inner mux behind
// the wrap on the three root patterns that together match every clean path. The
// generated "/" catch-all (main.gen.go) stays registered on root but is never
// selected, so a route added to ExtraMounts later is covered without anyone
// having to remember to wrap it.
//
// Production: the Go server answers /api, /healthz and /readyz behind Envoy,
// which passes these upstream response headers through unchanged. The SPA
// itself is served by nginx there (webApp/nginx.conf), which sets the same two
// headers — see frame_denial_nginx_test.go.

import (
	"net/http"
	"strings"
)

const (
	cspHeader          = "Content-Security-Policy"
	frameOptionsHeader = "X-Frame-Options"
	frameAncestorsNone = "frame-ancestors 'none'"
	frameOptionsDeny   = "DENY"
)

// denyFramingOnEveryPath registers inner, wrapped in denyFraming, on the three
// root patterns that between them match every cleaned request path: the exact
// root, any single segment, and any deeper path. Each is strictly more specific
// than the generated "/" catch-all, so it coexists with it (and wins).
func denyFramingOnEveryPath(root *http.ServeMux, inner http.Handler) {
	h := denyFraming(inner)
	root.Handle("/{$}", h)
	root.Handle("/{first}", h)
	root.Handle("/{first}/{rest...}", h)
}

// denyFraming sets both anti-framing headers on every response next writes.
//
// The headers are applied before next runs, so a handler that never calls Write
// (net/http then writes an implicit 200 itself) is still covered. They are
// applied again when next writes its headers, so a Content-Security-Policy that
// next sets itself is merged with frame-ancestors 'none', not overwritten.
func denyFraming(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyFrameDenial(w.Header())
		next.ServeHTTP(&frameDenyingWriter{ResponseWriter: w}, r)
	})
}

// applyFrameDenial sets X-Frame-Options and merges frame-ancestors 'none' into
// every Content-Security-Policy value already present (or adds one). It is
// idempotent. Content-Security-Policy-Report-Only is left alone: it enforces
// nothing.
func applyFrameDenial(h http.Header) {
	h.Set(frameOptionsHeader, frameOptionsDeny)
	existing := h.Values(cspHeader)
	if len(existing) == 0 {
		h.Set(cspHeader, frameAncestorsNone)
		return
	}
	merged := make([]string, len(existing))
	for i, v := range existing {
		merged[i] = mergeFrameAncestors(v)
	}
	h[http.CanonicalHeaderKey(cspHeader)] = merged
}

// mergeFrameAncestors returns the header value csp with every policy in it
// carrying frame-ancestors 'none'. A comma separates policies inside one
// header value, and a semicolon separates directives inside a policy. The
// function keeps every other directive and replaces any existing
// frame-ancestors directive, because 'none' is the strictest value and the
// point of this wrap.
func mergeFrameAncestors(csp string) string {
	policies := strings.Split(csp, ",")
	for i, policy := range policies {
		kept := make([]string, 0, 4)
		for d := range strings.SplitSeq(policy, ";") {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			name, _, _ := strings.Cut(d, " ")
			if strings.EqualFold(name, "frame-ancestors") {
				continue
			}
			kept = append(kept, d)
		}
		policies[i] = strings.Join(append(kept, frameAncestorsNone), "; ")
	}
	return strings.Join(policies, ", ")
}

// frameDenyingWriter re-applies the frame denial at the moment the wrapped
// handler commits its headers, so a handler that sets its own
// Content-Security-Policy gets it merged, not dropped. Unwrap and Flush keep
// streaming responses (the /mcp SSE stream flushes via http.ResponseController)
// working through the wrap.
type frameDenyingWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *frameDenyingWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		applyFrameDenial(w.Header())
		// 1xx responses are informational; the final status is still to come.
		w.wroteHeader = code >= http.StatusOK
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *frameDenyingWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it can flush.
func (w *frameDenyingWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *frameDenyingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
