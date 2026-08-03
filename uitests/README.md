# archistrator UI-test harness

The black-box, **browser-driven end-to-end** test harness for the archistrator
SPA — the UI sibling of [`../systemtests`](../systemtests/README.md) (which does
the same for the Go server at the wire). Like that package, this is a
**standalone package**, a *sibling* to `../webApp` (never under it), and it
drives the **real running SPA in a browser**, asserting the AC user flows.

## What makes it un-cheatable

This package links **zero webApp behavior** — no React render harness, no
component import, no hook import, no API-client import. It is not under
`webApp/`, has its own `package.json`, and its only real dependency is
`@playwright/test`. It:

- drives the **real SPA** in a headless Chromium over HTTP — no React render
  harness, no component import, no API-client import;
- selects **only by published `data-testid`**: `tests/support/testids.ts`
  IMPORTS the SPA's own id table
  (`webApp/src/utilities/constants/UIIdentifiers.ts`, plus a couple of
  ordered-list/type imports from `webApp/src/contracts/`) — a pure
  string-literal data module, not component code — so a renamed testid fails
  ONE import resolution here rather than silently drifting until the matching
  assertion happens to break (sharing beats a hand-copied mirror that can go
  stale);
- asserts a wire invariant of the rendering pivot directly from the **network log**
  (`close-and-no-render.spec.ts`: no request path matches `/render`).

## Topology

The SPA is a Vite app that proxies `/api/*` to the archistrator Go server, which
runs in **dev-mode auth** (it injects a dev principal when no edge headers are
present, so the SPA is locally runnable without an OIDC round-trip):

```
[chromium] → [SPA dev server :5173] ──/api──▶ [Go server :8888] → Postgres
                                                                 └▶ Temporal + worker   (only for drafting)
```

The Go server is **not** started by Playwright — it needs provisioned Postgres
and is brought up exactly as in `../systemtests` (docker-compose + the
`ARCHISTRATOR_*` env). This package owns only the SPA process.

## Layout

```
playwright.config.ts        baseURL + the managed SPA webServer; HTML reporter; traces/screenshots/video on failure
tests/landing.spec.ts       catalog: first-login empty state → create project → home base → project listed
tests/homebase.spec.ts      home base: phase card + artifact TOC + "Resume design" → design experience
tests/design-experience.spec.ts  spine + steps (pure UI); request-draft → generating → render → gate (LIVE)
tests/artifact-affordances.spec.ts  coreUseCases (LIVE): keyboard comment-arming on the review diagram; committed-paint chip not banner; Core/Variations picker grouping
tests/episodes-panel.spec.ts  SP1 capture-seam episodes panel on a design artifact (DOGFOOD-SEEDED state + episode ledger): rows, timeline tool events, GAP chip + reason, JSON/CSV export
tests/close-and-no-render.spec.ts  ✕ returns home; network log has NO /render request
tests/support/testids.ts    data-testid contract, imported from the SPA's own UI_IDENTIFIERS (no component/behavior import)
tests/support/gating.ts     infra gating — serverReachable / liveDrafting (the UI requireStack)
tests/support/flows.ts      reusable black-box flows (create project, enter design)
```

## Infra gating (mirrors systemtests)

Two tiers, the UI analogue of systemtests' "structural always-runs / wire-skips-
without-stack" split:

| Tier | Needs | Gate | Specs |
|------|-------|------|-------|
| **Pure UI / navigation** | SPA + dev-mode server with **Postgres** | self-skip when `/api/userinfo` ≠ 200 | `landing`, `homebase`, `close-and-no-render`, and the `structure` block of `design-experience` |
| **Live drafting** | a REAL GitHub App + repo (agentic dispatch — see the STALE NOTICE below) | opt-in `UITESTS_LIVE_DRAFTING` (see below) | the `co-author drafting` block of `design-experience`, plus `architecture-views` and `artifact-affordances` |

The SPA gates its whole tree on `GET /api/userinfo` returning 200. The pure-UI
specs probe that through the SPA proxy in `beforeEach` and **skip with a clear
reason** when the server is absent — never failing on a backend that was never
provisioned, matching systemtests. The live-drafting block additionally skips
unless `UITESTS_LIVE_DRAFTING` is non-`off`, the UI `requireStack`.

## Drafting modes (`UITESTS_LIVE_DRAFTING`)

**STALE NOTICE (2026-07-06):** the table this section used to carry described a
`ARCHISTRATOR_WORKER_PROVIDER` (`replay` / `ollama` / `anthropic`) knob on the Go
server. That config was **removed** in the agentic-design pivot (see
`server/internal/manager/systemdesign/dispatch.go`): the server holds no
server-side LLM worker at all. Drafting a Phase-1/Phase-2 artifact now DISPATCHES
a real `claude-code-action` GitHub Actions job (workflow_dispatch) in the
project's own repo, OBSERVES it to a terminal phase, and READS BACK the typed
draft the Action committed — the "generating scene" carries a `ci-job-notice`
because it is, literally, waiting on the user's CI, not a local model call.

There is currently **no offline/replay path this package can drive**: the only
offline simulation of that GitHub Actions + PR seam is
`systemtests/internal/harness/agentic_github.go`, an in-process Go
`httptest.Server` fake that exists only inside `systemtests`' `*_test.go` files
(compiled by `go test`, not runnable as a standalone dev-mode server) — wiring
uitests to it would mean adding new server-side test infrastructure, which this
package (black-box, zero server code) deliberately does not do.

Practically: running the `co-author drafting` / `architecture-views` /
`artifact-affordances` specs against a REAL model today means configuring a real
GitHub App identity (`ARCHISTRATOR_GITHUB_APP_ID` /
`ARCHISTRATOR_GITHUB_APP_PRIVATE_KEY_PEM`) and a real target repo with
`aiarch-design.yml` wired to `claude-code-action`, and setting
`UITESTS_LIVE_DRAFTING=live` (the *specs'* gate — the flag name is legacy but
still the correct switch). Locally, without that repo/App configured, these
specs are —correctly— gated off (`UITESTS_LIVE_DRAFTING` unset ⇒ `off`); this is
an honest skip, not a broken test. `ARCHISTRATOR_CONSTRUCTION_DRYRUN=true` does
**not** unblock this path — that flag only stubs the *construction* (UC3)
pipeline, not the design-dispatch one the `systemdesign`/`projectdesign`
Managers use. With no GitHub App configured, their
`constructionPipelineAccess` dependency is a `nil` interface (see
`cmd/server/main.go`'s `buildConstructionPipeline`), so a "Request draft" click
fails the dispatch activity (a nil-interface call) rather than silently
no-opping — clicking it against a dry-run-only server like the one this
package's own smoke rig boots is a dead end, which is exactly why the pure-UI
`structure` specs never click it.

## Environment variables

| Var | Default | Meaning |
|-----|---------|---------|
| `UITESTS_SPA_URL` | `http://localhost:5173` | Where the managed SPA dev server binds / baseURL falls back to. |
| `UITESTS_BASE_URL` | *(unset)* | Drive an **already-running** SPA (e.g. `vite preview` or a deployed origin). When set, the managed `webServer` is **skipped**. |
| `UITESTS_LIVE_DRAFTING` | *(unset → `off`)* | Four values — `off` / `replay` / `WHEN_REQUIRED` / `live` (legacy `1`/`true` ⇒ `live`) — decide whether the live drafting specs run. See the STALE NOTICE under [Drafting modes](#drafting-modes-uitests_live_drafting): only `live`, against a REAL GitHub App + repo, is actually wired today. |
| `ARCHISTRATOR_API_PROXY_TARGET` | *(unset → the SPA's own `http://localhost:8888` default)* | Passed through to the MANAGED SPA dev server (see `../webApp/vite.config.ts`) so its `/api` proxy targets a specific Go server instance instead of whatever happens to already be on `:8888`. Combine with a distinct `UITESTS_SPA_URL` port so the managed SPA doesn't collide with an unrelated dev server someone else already has running on `:5173`. |

## Running

### 0. Install

```bash
npm install
npm run install:browsers     # playwright install --with-deps chromium
```

### 1. Bring up the Go server in dev mode (behind the /api proxy)

Provision Postgres (and, for drafting, Temporal + a worker) the same way as the
system tests — reuse its `docker-compose.yaml`:

```bash
# from ../systemtests
make up          # postgres (+ ollama) containers + the local Temporal dev server
```

Then run the dev-mode server bound to `:8888` (the port the SPA proxies to by
default — see `ARCHISTRATOR_API_PROXY_TARGET` below to point it elsewhere),
with Postgres and dev auth on. Pure-UI runs need only Postgres — there is no
server-side LLM worker to select (see the "Drafting modes" STALE NOTICE above).
The LOCAL project-state git profile below needs its bare repo seeded with ONE
commit first (`git init --bare --initial-branch=main <path> && git clone
<path> <tmp> && cd <tmp> && git commit --allow-empty -m seed && git push`) —
`projectStateAccess.CreateProject` resolves against an existing `main` ref, not
a wholly-empty repo (mirrors `systemtests/internal/harness/localgit.go`'s
`StartLocalGitRepo`):

```bash
# from ../server
ARCHISTRATOR_LISTEN_ADDR=:8888 \
ARCHISTRATOR_AUTH_DEV_MODE=true \
ARCHISTRATOR_POSTGRES_URL=postgres://archistrator:archistrator@localhost:5432/archistrator?sslmode=disable \
ARCHISTRATOR_CONSTRUCTION_DRYRUN=true \
ARCHISTRATOR_PROJECT_STATE_GIT_LOCAL=true \
ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL=file:///path/to/a/seeded/bare/repo.git \
ARCHISTRATOR_TEMPORAL_HOSTPORT=localhost:7233 \
ARCHISTRATOR_TEMPORAL_NAMESPACE=aiarch-test \
go run ./cmd/server
```

To point the managed SPA's `/api` proxy at a server on a DIFFERENT port
(instead of restarting/sharing whatever is already on `:8888`), set
`ARCHISTRATOR_API_PROXY_TARGET=http://localhost:<port>` alongside
`UITESTS_SPA_URL=http://localhost:<a-free-port>` when invoking `npm test` — see
[Environment variables](#environment-variables).

### 1b. (episodes-panel only) seed the episode ledger

`tests/episodes-panel.spec.ts` asserts against REAL captured episode-ledger
content on the well-known `archistrator` dogfood project, so it needs the
**dogfood-seeded** project-state repo (same requirement as
`artifact-systemtest.spec.ts` — see `skipUnlessConstructionArtifacts`), PLUS an
episode ledger under `<repoRoot>/.aiarch/traces/`. Without both it self-skips.

```bash
# a) a repo seeded from THIS checkout's committed project.json
REPO=/tmp/uitests-episodes-repo
git init --initial-branch=main "$REPO"
git -C "$REPO" config receive.denyCurrentBranch updateInstead
mkdir -p "$REPO/.aiarch/state"
cp .aiarch/state/project.json "$REPO/.aiarch/state/project.json"
git -C "$REPO" add -A && git -C "$REPO" commit -m "seed: dogfood project state"

# b) the episode ledger, written through the real episodeAccess RA and derived
#    from the committed capture fixtures (see the tool's own doc comment for
#    why this is seeded rather than produced by a live dispatch)
cd ../server && GOWORK=off go run ./cmd/gen-uitests-episodes -repo "$REPO"
```

Then point the server at that repo
(`ARCHISTRATOR_PROJECT_STATE_GIT_REPO_URL=file:///tmp/uitests-episodes-repo`).

> **The two project-state configurations are mutually exclusive** — a
> long-standing property of this harness, not new. The project-CREATION specs
> (`landing`, `homebase`, `close-and-no-render`, `design-experience`'s
> `structure` block, `gate-sendback-fault`) require the FRESH EMPTY repo CI
> provisions; `artifact-systemtest`, `episodes-panel` and the
> `meta/use-case-coverage` check require the dogfood-seeded one. Whichever half
> is not configured self-skips. See `.github/workflows/uitests.yml`'s "Project
> state" note.

### 2. Run the UI tests

The SPA is started for you by Playwright's `webServer` (`npm run dev` in
`../webApp`, port 5173). Just run:

```bash
# Pure-UI / navigation specs (server with Postgres up):
npm test

# Everything, including the live co-author drafting flow.
# Offline + deterministic against committed cassettes (CI default):
UITESTS_LIVE_DRAFTING=replay npm test
# Or hit a real model (records misses / ignores cache — see "Drafting modes"):
UITESTS_LIVE_DRAFTING=live npm test

# Drive an already-running SPA instead of the managed dev server:
UITESTS_BASE_URL=http://localhost:4173 npm test     # e.g. `vite preview`

# Headed / interactive:
npm run test:ui
```

### Enumerate specs without a backend

`playwright test --list` enumerates every spec with no server and no browser
download — the structural gate for this package (the run-green-against-a-stack
step is the integration stage, not this one):

```bash
npm run list      # playwright test --list
```

## Artifacts

On failure, Playwright writes a trace, a screenshot, and a video under
`test-results/`, and an HTML report under `playwright-report/` — the same
"artifacts on failure" convention the other test packages follow.

```bash
npm run test:report     # open the HTML report
```
