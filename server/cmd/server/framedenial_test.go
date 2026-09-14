package main

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
)

// assertFrameDenied fails unless h carries both anti-framing headers.
func assertFrameDenied(t *testing.T, where string, h http.Header) {
	t.Helper()
	if got := h.Get(frameOptionsHeader); got != "DENY" {
		t.Errorf("%s: X-Frame-Options = %q, want %q", where, got, "DENY")
	}
	csp := strings.Join(h.Values(cspHeader), ", ")
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("%s: Content-Security-Policy = %q, want it to contain %q", where, csp, "frame-ancestors 'none'")
	}
}

// TestExtraMounts_DeniesFramingOnEveryResponse drives the real composed mux —
// the generated server bound at "/" exactly as main.gen.go does, then the real
// ExtraMounts — and proves an SPA route, an SPA asset, the generated API, the
// composition-root API routes, the health probes, /mcp and unknown paths all
// carry both headers. The body check proves each request reached the handler it
// was meant to, so a header present on some other handler's response cannot
// pass the test by accident.
func TestExtraMounts_DeniesFramingOnEveryResponse(t *testing.T) {
	type probe struct {
		method, path string
		wantStatus   int
		wantBody     string
	}
	common := []probe{
		// The generated API: a nil validator denies, proving the real generated
		// auth boundary answered.
		{http.MethodGet, "/api/v1/systems", http.StatusUnauthorized, "unauthorized"},
		// Composition-root API routes (dev mode injects a principal).
		{http.MethodGet, "/api/v1/capabilities", http.StatusOK, `"operations"`},
		{http.MethodGet, "/api/userinfo", http.StatusOK, ""},
		{http.MethodGet, "/healthz", http.StatusOK, `"status":"ok"`},
		{http.MethodGet, "/readyz", http.StatusOK, `"status":"ok"`},
		// A POST with no Content-Type is rejected by the MCP transport itself
		// before any manager runs, so nil managers are safe here.
		{http.MethodPost, "/mcp", http.StatusUnsupportedMediaType, "Content-Type"},
	}
	cases := []struct {
		name     string
		gitLocal bool
		probes   []probe
	}{
		{"local profile (embedded SPA)", true, append([]probe{
			{http.MethodGet, "/", http.StatusOK, "spa-shell"},
			{http.MethodGet, "/project/x/home", http.StatusOK, "spa-shell"},
			{http.MethodGet, "/assets/app.js", http.StatusOK, "spa-bundle"},
			{http.MethodGet, "/favicon.ico", http.StatusOK, "ico-bytes"},
			{http.MethodGet, "/api/v1/operations/query-operated-system-view/x", http.StatusNotFound, ""},
		}, common...)},
		{"cloud profile (API only)", false, append([]probe{
			{http.MethodGet, "/", http.StatusNotFound, ""},
			{http.MethodGet, "/no/such/route", http.StatusNotFound, ""},
		}, common...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := http.NewServeMux()
			root.Handle("/", web.NewServer(web.DevConfig{}, nil)) // main.gen.go's mount
			h := newTestAppHooks()
			h.embeddedSPA = func() (fs.FS, bool) { return spaTestFS(), true }
			h.ExtraMounts(root, &Config{ProjectStateGitLocal: tc.gitLocal}, web.DevConfig{Enabled: true, Principal: devPrincipal()}, nil, WebManagers{})

			ts := httptest.NewServer(root)
			defer ts.Close()
			for _, p := range tc.probes {
				where := p.method + " " + p.path
				req, err := http.NewRequest(p.method, ts.URL+p.path, nil)
				if err != nil {
					t.Fatalf("%s: %v", where, err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("%s: %v", where, err)
				}
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != p.wantStatus {
					t.Errorf("%s: status = %d, want %d (body %q)", where, resp.StatusCode, p.wantStatus, body)
				}
				if !strings.Contains(string(body), p.wantBody) {
					t.Errorf("%s: body = %q, want it to contain %q", where, body, p.wantBody)
				}
				assertFrameDenied(t, where, resp.Header)
			}
		})
	}
}

// A handler that sets its own Content-Security-Policy keeps its directives: the
// wrap merges frame-ancestors 'none' in, replacing a laxer frame-ancestors.
func TestDenyFraming_MergesIntoExistingCSP(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(cspHeader, "default-src 'self'; frame-ancestors *; img-src data:")
		_, _ = w.Write([]byte("ok"))
	})
	rec := httptest.NewRecorder()
	denyFraming(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rec.Header().Values(cspHeader)
	want := "default-src 'self'; img-src data:; frame-ancestors 'none'"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("Content-Security-Policy = %q, want exactly [%q]", got, want)
	}
	assertFrameDenied(t, "merged", rec.Header())
}

// A handler that never writes still gets both headers on the implicit 200.
func TestDenyFraming_HandlerThatNeverWrites(t *testing.T) {
	ts := httptest.NewServer(denyFraming(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	assertFrameDenied(t, "implicit 200", resp.Header)
}

// The /mcp SSE stream flushes through http.ResponseController; the wrap must
// not hide the underlying Flusher.
func TestDenyFraming_KeepsFlushWorking(t *testing.T) {
	var flushErr error
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("event"))
		flushErr = http.NewResponseController(w).Flush()
	})
	rec := httptest.NewRecorder()
	denyFraming(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if flushErr != nil {
		t.Fatalf("Flush through the wrap: %v", flushErr)
	}
	if !rec.Flushed {
		t.Fatal("underlying writer was not flushed")
	}
	assertFrameDenied(t, "streamed", rec.Header())
}

func TestMergeFrameAncestors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"default-src 'self'", "default-src 'self'; frame-ancestors 'none'"},
		{"default-src 'self';", "default-src 'self'; frame-ancestors 'none'"},
		{"FRAME-ANCESTORS https://evil.example", "frame-ancestors 'none'"},
		{"frame-ancestors 'none'", "frame-ancestors 'none'"},
		{"a b; frame-ancestors 'self', c d", "a b; frame-ancestors 'none', c d; frame-ancestors 'none'"},
	} {
		if got := mergeFrameAncestors(tc.in); got != tc.want {
			t.Errorf("mergeFrameAncestors(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
