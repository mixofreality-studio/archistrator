//go:build localdist

// spa_embed.go is the local-profile single-binary SPA embed
// (local-first-init-funnel Task 4,
// docs/superpowers/plans/2026-07-19-local-first-init-funnel.md): a go:embed of
// the BUILT webApp `dist/` tree, gated behind the `localdist` build tag so
// cloud images — built WITHOUT this tag (see the Dockerfile) — never carry it
// and keep serving the SPA via nginx exactly as before (spa_stub.go supplies
// the no-op arm those builds get instead).
//
// This is a DIFFERENT surface from the earlier MCP-Apps pilot's "No go:embed
// of web assets" constraint: that constraint blocked embedding a
// ui://-scheme resource stub INSIDE the MCP transport itself (Claude Code
// does not render ui://; see mcp_apps.go). This is a build-tag-gated,
// local-only embed of the whole SPA, authorized by THIS plan's Task 4
// specifically for `archistrator init`'s single local binary (server + SPA +
// /api + /mcp, all one process). The two are not in tension.
//
// webappdist/ is a build-time-only artifact staged by scripts/build-local.sh
// (`cp -R ../webApp/dist webappdist` immediately before
// `go build -tags localdist`) — never checked into git (see .gitignore). A
// `localdist` build without that directory present FAILS AT COMPILE TIME
// (go:embed requires the named files to exist) — the desired fail-fast: this
// tag can never silently ship a binary with a missing or stale SPA.
package main

import (
	"embed"
	"io/fs"
)

//go:embed all:webappdist
var spaDistEmbedded embed.FS

// spaDistRoot is the directory inside spaDistEmbedded holding the staged
// dist/ tree (go:embed patterns must name a real, existing directory relative
// to this file — see the doc comment above).
const spaDistRoot = "webappdist"

// spaFS returns the embedded webApp dist tree, rooted so its paths match the
// real dist/ layout (e.g. "index.html", "assets/app-xxxx.js") rather than
// being prefixed with "webappdist/".
func spaFS() (fs.FS, bool) {
	sub, err := fs.Sub(spaDistEmbedded, spaDistRoot)
	if err != nil {
		return nil, false
	}
	return sub, true
}
