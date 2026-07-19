package main

// spa_handler.go implements the local-profile embedded-SPA serving logic
// (local-first-init-funnel Task 4,
// docs/superpowers/plans/2026-07-19-local-first-init-funnel.md): the "single
// binary" `archistrator init` boot serves the built webApp SPA at "/" from the
// SAME Go server that already serves /api + /mcp, with an index.html fallback
// for client-side routing (TanStack Router paths like /project/x/home).
//
// Deliberately split from the go:embed itself (spa_embed.go, //go:build
// localdist / spa_stub.go, //go:build !localdist) so THIS file carries no
// build tag and its routing/content-type/fallback logic is exercised directly
// by spa_handler_test.go against an in-memory testing/fstest.MapFS fixture —
// no real webApp build is required to run the unit tests or to build the
// default (cloud) binary.
//
// Mux registration strategy: the generated composition root (main.gen.go)
// already binds the OUTER *http.ServeMux's "/" catch-all pattern to the
// generated REST server (genServer) BEFORE ExtraMounts runs, and net/http's
// ServeMux panics on re-registering an EQUAL pattern (confirmed empirically:
// registering a second "/"-equivalent catch-all, e.g. "/{rest...}", panics
// with "matches the same requests as /"). So mountSPA instead registers THREE
// patterns that are each a strict SUBSET of "/" — Go's ServeMux allows a more
// specific pattern to coexist with (and take precedence over) a broader one:
//
//   - "/{$}"              — the exact root path.
//   - "/{first}"           — any single path segment (e.g. /favicon.ico,
//     /systems).
//   - "/{first}/{rest...}" — any multi-segment path (covers both static
//     assets like /assets/app-xxxx.js AND client-side
//     routes like /project/x/home) — coexists with the
//     literal prefix "/api/v1/" the same way (literal
//     prefix wins over the wildcard for /api/v1/... paths).
//
// GOTCHA (caught by a real local boot, not just the unit tests — /healthz
// returned the SPA shell instead of {"status":"ok"} before this fix): the
// OUTER mux only compares patterns registered DIRECTLY ON ITSELF when
// choosing the most specific match — it has no visibility into genServer's
// OWN inner routing table (server.gen.go's "GET /healthz"/"GET /readyz",
// registered on genServer's private inner mux, not on root). So the plain
// outer "/{first}" IS more specific than the outer "/" and would silently
// steal /healthz and /readyz from genServer. shadowHealthLikeRoutes fixes
// this with the SAME capture-and-shadow technique hooks.go's ExtraMounts
// already uses for /api/v1/ (http5xxlog.go): before registering the
// wildcards, each health-like path's currently-resolved (genServer) handler
// is captured and re-registered as its own literal pattern directly on root,
// which — being literal — continues to win over "/{first}".
import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// healthLikePaths are genServer's (server.gen.go) exact-match, single-segment
// GET routes that mountSPA's "/{first}" wildcard would otherwise shadow — see
// the GOTCHA above.
var healthLikePaths = []string{"/healthz", "/readyz"}

// mountSPA registers the embedded local-profile SPA on root at the patterns
// described above when this binary was built with the `localdist` tag
// (spaFS, spa_embed.go, returns ok=true); it is a no-op otherwise (the
// default/cloud build, spa_stub.go), so ExtraMounts (hooks.go) can call it
// unconditionally and let the build tag decide.
func mountSPA(root *http.ServeMux, logger *slog.Logger) {
	fsys, ok := spaFS()
	if !ok {
		logger.Info("embedded SPA not mounted — not a localdist build")
		return
	}
	registerSPARoutes(root, fsys)
	logger.Info("embedded SPA mounted at /")
}

// registerSPARoutes performs the actual mux registration mountSPA describes
// above; split out so tests can exercise it directly against an in-memory
// fs.FS without depending on the `localdist` build tag.
func registerSPARoutes(root *http.ServeMux, fsys fs.FS) {
	shadowHealthLikeRoutes(root)
	h := spaHandler(fsys)
	root.Handle("/{$}", h)
	root.Handle("/{first}", h)
	root.Handle("/{first}/{rest...}", h)
}

// shadowHealthLikeRoutes re-registers each of healthLikePaths as its own
// literal "GET <path>" pattern directly on root, backed by whatever handler
// currently resolves that path (genServer's real handler in production; a
// test double in spa_handler_test.go) — see the GOTCHA above.
func shadowHealthLikeRoutes(root *http.ServeMux) {
	for _, p := range healthLikePaths {
		req := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: p}}
		if h, pat := root.Handler(req); pat != "" {
			root.Handle("GET "+p, h)
		}
	}
}

// spaHandler serves fsys (the built webApp `dist/` tree) at the request path.
// A request that resolves to a real file (e.g. /assets/app-xxxx.js) is served
// with its content-type set from the file extension; any other request (the
// exact root, or a path with no matching file — a client-side route) serves
// index.html WITHOUT rewriting the request URL, so the browser's address bar
// (and therefore the client router) keeps the original path.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := requestedFile(r.URL.Path)

		// The exact root resolves to "index.html" by directory-index
		// convention — let FileServerFS serve it directly (its request path
		// is "/", which does NOT literally end in "/index.html", so its
		// index.html→"/" redirect special-case does not fire here).
		if name == "index.html" {
			fileServer.ServeHTTP(w, r)
			return
		}

		if info, err := fs.Stat(fsys, name); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		// No matching file (or it's a directory) — SPA fallback: serve
		// index.html's content for THIS request, leaving r.URL.Path (and so
		// the browser's address bar) untouched.
		serveIndexFallback(w, r, fsys)
	})
}

// requestedFile maps a request path to the fs.FS-relative name spaHandler
// looks up: leading slash trimmed, "." and ".." segments collapsed via
// path.Clean, empty/root normalized to "index.html".
func requestedFile(urlPath string) string {
	clean := path.Clean(urlPath)
	name := strings.TrimPrefix(clean, "/")
	if name == "" || name == "." {
		return "index.html"
	}
	return name
}

// serveIndexFallback writes fsys's index.html as the response body for r
// without touching r.URL.Path — the SPA client-side-routing contract.
func serveIndexFallback(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	f, err := fsys.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "index.html unavailable", http.StatusInternalServerError)
		return
	}

	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "index.html unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", stat.ModTime(), rs)
}
