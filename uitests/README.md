# archistrator UI-test harness

The black-box, **browser-driven end-to-end** test harness for the archistrator
SPA — the UI sibling of [`../systemtests`](../systemtests/README.md) (which does
the same for the Go server at the wire). Like that package, this is a
**standalone package**, a *sibling* to `../webApp` (never under it), and it
drives the **real running SPA in a browser**, asserting the AC user flows.

## What makes it un-cheatable

This package links **zero** webApp source. It is not under `webApp/`, has its own
`package.json`, and its only real dependency is `@playwright/test`. It:

- drives the **real SPA** in a headless Chromium over HTTP — no React render
  harness, no component import, no API-client import;
- selects **only by published `data-testid`** (mirrored in `tests/support/testids.ts`
  from `webApp/src/constants/UIIdentifiers.ts` as black-box string literals — a
  renamed testid fails the matching assertion, exactly like a wire-format change);
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
tests/close-and-no-render.spec.ts  ✕ returns home; network log has NO /render request
tests/support/testids.ts    mirrored data-testid contract (no webApp import)
tests/support/gating.ts     infra gating — serverReachable / liveDrafting (the UI requireStack)
tests/support/flows.ts      reusable black-box flows (create project, enter design)
```

## Infra gating (mirrors systemtests)

Two tiers, the UI analogue of systemtests' "structural always-runs / wire-skips-
without-stack" split:

| Tier | Needs | Gate | Specs |
|------|-------|------|-------|
| **Pure UI / navigation** | SPA + dev-mode server with **Postgres** | self-skip when `/api/userinfo` ≠ 200 | `landing`, `homebase`, `close-and-no-render`, and the `structure` block of `design-experience` |
| **Live drafting** | + **Temporal + worker** (replay/Ollama/Anthropic) | opt-in `UITESTS_LIVE_DRAFTING` (see below) | the `co-author drafting` block of `design-experience`, plus `architecture-views` |

The SPA gates its whole tree on `GET /api/userinfo` returning 200. The pure-UI
specs probe that through the SPA proxy in `beforeEach` and **skip with a clear
reason** when the server is absent — never failing on a backend that was never
provisioned, matching systemtests. The live-drafting block additionally skips
unless `UITESTS_LIVE_DRAFTING` is non-`off`, the UI `requireStack`.

## Drafting modes (`UITESTS_LIVE_DRAFTING`)

The live-drafting specs need Postgres + Temporal + a worker. Bring the Go server up
yourself (this suite only drives the browser) and set the matching worker env. The
flag here only decides whether the live specs RUN; the server's mode is set
separately via `ARCHISTRATOR_WORKER_*` (a test-only `replay` provider).

| `UITESTS_LIVE_DRAFTING` | live specs | server worker env |
|---|---|---|
| `off` / unset | skipped | n/a |
| `replay` (CI default) | run | `ARCHISTRATOR_WORKER_PROVIDER=replay`, `ARCHISTRATOR_WORKER_REPLAY_MODE=strict`, `ARCHISTRATOR_WORKER_REPLAY_DIR=<uitests>/testdata/cassettes` |
| `WHEN_REQUIRED` | run | `…PROVIDER=replay`, `…REPLAY_MODE=record_on_miss`, `…REPLAY_DELEGATE=ollama` (or `anthropic`), plus that delegate's vars (`ARCHISTRATOR_OLLAMA_BASEURL=…` or `ARCHISTRATOR_ANTHROPIC_API_KEY=…`) |
| `live` (or legacy `1`) | run | `ARCHISTRATOR_WORKER_PROVIDER=ollama` (or `anthropic`) |

Server replay env vars (test-only):
- `ARCHISTRATOR_WORKER_REPLAY_DIR` — on-disk cassette directory (required for `replay`).
- `ARCHISTRATOR_WORKER_REPLAY_MODE` — `strict` (default; a cassette miss is a loud error) or `record_on_miss`.
- `ARCHISTRATOR_WORKER_REPLAY_DELEGATE` — `ollama` (default) or `anthropic`; serves misses in `record_on_miss`.

`replay` is offline and deterministic — no Ollama needed. Record cassettes once with
`WHEN_REQUIRED` (or `live`) and commit `testdata/cassettes/`.

## Environment variables

| Var | Default | Meaning |
|-----|---------|---------|
| `UITESTS_SPA_URL` | `http://localhost:5173` | Where the managed SPA dev server binds / baseURL falls back to. |
| `UITESTS_BASE_URL` | *(unset)* | Drive an **already-running** SPA (e.g. `vite preview` or a deployed origin). When set, the managed `webServer` is **skipped**. |
| `UITESTS_LIVE_DRAFTING` | *(unset → `off`)* | Four values — `off` / `replay` / `WHEN_REQUIRED` / `live` (legacy `1`/`true` ⇒ `live`) — decide whether the live drafting specs run. The server's matching worker mode is set separately via `ARCHISTRATOR_WORKER_*`. See [Drafting modes](#drafting-modes-uitests_live_drafting). |

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

Then run the dev-mode server bound to `:8888` (the port the SPA proxies to),
with Postgres and dev auth on. Pure-UI runs need only Postgres + a worker
selection (the server requires *some* provider to boot — Ollama avoids needing
an Anthropic key):

```bash
# from ../server
ARCHISTRATOR_LISTEN_ADDR=:8888 \
ARCHISTRATOR_AUTH_DEV_MODE=true \
ARCHISTRATOR_POSTGRES_URL=postgres://archistrator:archistrator@localhost:5432/archistrator?sslmode=disable \
ARCHISTRATOR_WORKER_PROVIDER=ollama \
ARCHISTRATOR_OLLAMA_BASEURL=http://localhost:11434 \
ARCHISTRATOR_TEMPORAL_HOSTPORT=localhost:7233 \
ARCHISTRATOR_TEMPORAL_NAMESPACE=aiarch-test \
go run ./cmd/server
```

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
