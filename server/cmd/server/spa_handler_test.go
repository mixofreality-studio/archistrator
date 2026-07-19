package main

// spa_handler_test.go exercises the local-profile embedded-SPA serving logic
// (spa_handler.go) against an in-memory testing/fstest.MapFS fixture — no real
// webApp build is required. It also exercises mountSPA's no-op arm: this
// package's default (non-`localdist`) test build resolves spaFS to
// spa_stub.go's (nil, false), so mountSPA must be a safe no-op.
import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mixofreality-studio/archistrator/server/internal/client/web"
)

func spaTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<html><body>spa-shell</body></html>")},
		"assets/app.js":  &fstest.MapFile{Data: []byte("console.log('spa-bundle')")},
		"assets/app.css": &fstest.MapFile{Data: []byte("body{color:red}")},
		"favicon.ico":    &fstest.MapFile{Data: []byte("ico-bytes")},
	}
}

func TestSPAHandlerServesIndexAtRoot(t *testing.T) {
	h := spaHandler(spaTestFS())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spa-shell") {
		t.Fatalf("body = %q, want to contain the index.html shell", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html prefix", ct)
	}
}

func TestSPAHandlerServesJSBundleWithContentType(t *testing.T) {
	h := spaHandler(spaTestFS())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "console.log('spa-bundle')" {
		t.Fatalf("body = %q, want the JS bundle bytes unchanged", rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type = %q, want a javascript MIME type", ct)
	}
}

func TestSPAHandlerFallsBackToIndexForClientSideRoute(t *testing.T) {
	h := spaHandler(spaTestFS())
	rec := httptest.NewRecorder()
	// TanStack Router client-side path — not a real embedded file.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/project/x/home", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (index.html fallback)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "spa-shell") {
		t.Fatalf("body = %q, want the index.html shell (SPA fallback)", rec.Body.String())
	}
}

func TestSPAHandlerFallbackDoesNotRewriteRequestPath(t *testing.T) {
	h := spaHandler(spaTestFS())
	req := httptest.NewRequest(http.MethodGet, "/project/x/home", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if req.URL.Path != "/project/x/home" {
		t.Fatalf("request URL.Path mutated to %q — fallback must serve index.html content without rewriting the path (breaks client-side routing / address bar)", req.URL.Path)
	}
}

func TestSPAHandlerSetsNoCacheOnIndexAtRoot(t *testing.T) {
	h := spaHandler(spaTestFS())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want %q (index.html must always be revalidated so a client picks up a redeployed SPA immediately)", cc, "no-cache")
	}
}

func TestSPAHandlerSetsNoCacheOnFallback(t *testing.T) {
	h := spaHandler(spaTestFS())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/project/x/home", nil))

	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want %q (SPA fallback serves index.html content and must carry the same no-cache contract as the direct-root path)", cc, "no-cache")
	}
}

func TestSPAHandlerHashedAssetKeepsDefaultCacheBehavior(t *testing.T) {
	h := spaHandler(spaTestFS())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))

	if cc := rec.Header().Get("Cache-Control"); cc != "" {
		t.Fatalf("Cache-Control = %q, want unset — only index.html forces no-cache; hashed assets keep FileServerFS's default behavior", cc)
	}
}

func TestSPAHandlerServesFaviconFromRoot(t *testing.T) {
	h := spaHandler(spaTestFS())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ico-bytes" {
		t.Fatalf("body = %q, want the favicon bytes unchanged (real file, not index.html fallback)", rec.Body.String())
	}
}

func TestMountSPANoOpWhenBuiltWithoutLocaldistTag(t *testing.T) {
	root := http.NewServeMux()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mountSPA(root, logger)

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (spaFS() unavailable in this non-localdist test build, so mountSPA must not mount anything)", rec.Code)
	}
}

// mountSPAWithFS is the test-only twin of mountSPA that mounts a given fs.FS
// directly, bypassing the build-tag-gated spaFS() seam — so this file's tests
// can pin the mux-registration coexistence (the tricky part of this feature)
// without needing a `localdist` build. Delegates to the SAME registerSPARoutes
// mountSPA itself calls, so a regression there (e.g. the /healthz-shadowing
// bug this test caught) is exercised identically to production.
func mountSPAWithFS(root *http.ServeMux, fsys fs.FS) {
	registerSPARoutes(root, fsys)
}

// Pins the mux-registration strategy documented in spa_handler.go: the
// generated composition root already binds the outer ServeMux's "/"
// catch-all to the generated REST server BEFORE the SPA mount runs, and the
// other composition-root-only routes (/api/v1/, /mcp, GET /api/userinfo) are
// registered alongside it — this must not panic on pattern conflict, and
// every path must resolve to the EXPECTED handler.
func TestMountSPACoexistsWithGeneratedAndExtraMounts(t *testing.T) {
	root := http.NewServeMux()

	// genServer is the REAL generated composition root
	// (internal/client/web/server.gen.go's NewServer), constructed with ZERO
	// registrars — reproducing the property this test cares about (the
	// literal top-level route set: GET /healthz, GET /readyz, /api/v1/ on an
	// INNER mux bound to root at "/") needs no manager wiring. Using the real
	// NewServer here (not a hand-rolled double) means a future httpgen regen
	// that changes that literal route set breaks THIS test instead of
	// silently shadowing it in production — the real bug shadowHealthLikeRoutes
	// exists to fix (see spa_handler.go's healthLikePaths comment). A nil
	// validator makes /api/v1/... deny-all (401 "unauthorized: ..."), which is
	// exactly what this test needs: a response that is unmistakably NOT the
	// SPA shell, proving the prefix wasn't shadowed.
	genServer := web.NewServer(web.DevConfig{}, nil)
	root.Handle("/", genServer) // main.gen.go's mount, before ExtraMounts/mountSPA run

	// hooks.go's ExtraMounts already shadows "/api/v1/" on the outer root
	// (the 5xx-logging wrap, http5xxlog.go) — reproduced here (minus the
	// logging wrap itself, irrelevant to mux-registration precedence) so this
	// test's mux matches the real one mountSPA runs against.
	if apiSurface, pat := root.Handler(httptest.NewRequest(http.MethodGet, "/api/v1/", nil)); pat != "" {
		root.Handle("/api/v1/", apiSurface)
	} else {
		t.Fatal("capture returned no handler for /api/v1/")
	}

	root.Handle("GET /api/userinfo", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("userinfo"))
	}))
	root.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mcp"))
	}))

	mountSPAWithFS(root, spaTestFS()) // must not panic

	cases := []struct {
		path       string
		wantStatus int
		want       string
	}{
		{"/", http.StatusOK, "spa-shell"},
		{"/project/x/home", http.StatusOK, "spa-shell"},
		{"/assets/app.js", http.StatusOK, "console.log('spa-bundle')"},
		// Reaches genServer's REAL auth boundary (security.Middleware, nil
		// validator denies) rather than the SPA fallback — proves /api/v1/ is
		// unshadowed, using the generated server's actual deny response.
		{"/api/v1/systems", http.StatusUnauthorized, "unauthorized"},
		{"/mcp", http.StatusOK, "mcp"},
		{"/api/userinfo", http.StatusOK, "userinfo"},
		// genServer's REAL handleHealth body (server.gen.go) — not a double.
		{"/healthz", http.StatusOK, `"status":"ok"`},
		{"/readyz", http.StatusOK, `"status":"ok"`},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		root.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.wantStatus {
			t.Errorf("path %q: status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("path %q: body = %q, want to contain %q", tc.path, rec.Body.String(), tc.want)
		}
	}
}
