# MCP Apps compatibility — design

**Date:** 2026-07-13
**Status:** Draft for founder review
**Scope ruling:** Both surfaces eventually — archistrator's own server first as the learning vehicle, then replicate the pattern in app-generator so every generated app ships MCP-Apps-ready. This spec covers the archistrator slice; app-generator replication is a follow-on spec.

## 1. Goal

Every webApp screen becomes dual-hosted: rendered by the SPA as today, or rendered inside an MCP host's chat (Claude, Claude Desktop, VS Code Copilot, Goose, …) when the corresponding MCP tool fires. One React codebase, two mounts. The chat panel is omitted in MCP context — the host conversation *is* the chat; the model drives the workflow by calling the same manager tools the chat panel drives today.

MCP Apps background (the [apps extension](https://modelcontextprotocol.io/extensions/apps/overview)): the MCP server additionally serves HTML UI as MCP **resources** under a `ui://` URI; tools declare `_meta: { ui: { resourceUri } }`. When a host calls such a tool it fetches the HTML, renders it in a sandboxed iframe in the chat, pushes the tool result into it, and the iframe communicates with the host over postMessage JSON-RPC (`@modelcontextprotocol/ext-apps` `App` class): receive tool results (`ontoolresult`), call server tools (`callServerTool`). The iframe runs behind a deny-by-default CSP — it cannot call the REST API.

## 2. Current state (recon 2026-07-13)

- **Server A** (`cmd/server`, streamable HTTP at `/mcp`, official go-sdk v1.6.1) serves ~35 tools across systemDesign (13) / projectDesign (9) / construction (7) / operations (6), 100% generated from `project.json` `.serviceContracts` by `cmd/clientgen/internal/mcpemit`. Mounted in `cmd/server/mcp_mount.go` via the `hooks.go` `ExtraMounts` seam, behind `web.AuthMiddleware` (bearer validator, or dev-mode principal injection).
- **Server B** (`cmd/aiarch-state-mcp`, stdio, GH Actions) is out of scope — unchanged.
- **No MCP resources exist anywhere**; both servers are tools-only. `ui://` serving is greenfield.
- **webApp**: React 19 + Vite 7 + MUI 7 + @xyflow/react + TanStack Router/Query; data via `openapi-fetch` REST only (`src/api/client.ts`); layer DAG `routes → components → hooks → api` enforced by the reusable eslint-boundaries package. Deployed separately from the Go server (no static serving in Go today).
- Hand-editing `.gen.go` is gate-blocked, so all tool metadata must flow through the generator.

## 3. Architecture: prop-driven presentational core (ratified with founder)

The founder ruled for a **container/presentational redesign** rather than a transport swap under the hooks:

### 3.1 Components layer becomes presentational-pure

All screens and components are **prop-driven only**: data and callbacks arrive as props. No data hooks, no `api` imports, no TanStack Query, no fetches inside the `components` layer. This is what makes a screen mountable in either host.

### 3.2 The OpsClient abstraction (one clean client→server seam)

All client→server communication goes through a single **generated, transport-agnostic ops client** so the app works identically in browser or chat/agent/MCP-app context:

- **`OpsClient` interface** — generated from the service contracts (sibling to `gen:api`/`gen-enums` outputs): one typed method per operation (`getSessionState`, `submitReviewDecision`, …).
- **Two generated implementations:** `RestOpsClient` wrapping `openapi-fetch`, and `McpOpsClient` wrapping the ext-apps `app.callServerTool()` with the op→tool-name mapping baked in. Same contracts drive REST handlers, MCP tools, and both client impls — parity is mechanical, never hand-maintained.
- **Injected via React context.** TanStack Query runs in both hosts: the `hooks` layer (`useSessionState`, `useSubmitReviewDecision`, …) is written once against `OpsClient` — `queryFn`/`mutationFn` don't care whether bytes travel over HTTP or postMessage. **One shared container per screen**; only the root provider differs: the SPA wires `RestOpsClient`, the MCP shell wires `McpOpsClient` and seeds the query cache from the host-pushed initial tool result (`app.ontoolresult`). Mutations invalidate queries identically in both hosts.
- **Loading/pending UX:** hosts mediate app-initiated tool calls and may interpose consent prompts on mutations (host policy, uncontrollable from the server side), so click handlers use pending states, not optimistic flips — a direct continuation of the existing workflow loading-state patterns (§9 role-driven loading states), which work unchanged over either transport.

### 3.3 Lint enforcement, platform-wide

The reusable eslint-boundaries package (the TS counterpart to framework-go/arch) grows a new ruleset:

- DAG becomes `routes → containers → (components | hooks) → api`, with `contracts`/`utilities` as leaves.
- `components` may import only `components`, `contracts`, `utilities`. Data-hook calls (`useQuery`/`useMutation`) and `api` imports in `components` are violations.
- Direct `fetch`/network calls outside the `api` layer are violations (this is also what keeps screens CSP-safe in the iframe).

This ruleset ships in the app-generator webApp template so **every archistrator-generated app is MCP-Apps-ready by construction**, not by convention. Existing archistrator webApp screens migrate to comply (per-screen mechanical work; see rollout).

### 3.4 Shell app + bundling: CSP stub, assets from nginx (founder ruling)

A second Vite entry, `mcp-app.html`, builds the shell as a **normal asset bundle with fixed (unhashed) output names** (`mcp-app.js`, `mcp-app.css`) into the same `dist` that `PUSH-APP.sh` already publishes to nginx. The `ui://archistrator/shell.html` resource is a **~1–2 KB constant HTML stub** whose script/style tags point at the webApp origin, with `_meta.ui.csp` declaring that origin. Responsibility split: Go serves a config-templated constant string (no fetching, no caching, no asset awareness); nginx serves and cache-controls the assets (plus CORS for the sandbox origin); the browser HTTP-caches them normally; the MCP host caches the stub resource per conversation. Fixed names keep the stub constant; cache-busting via a version query param from config or short TTLs. No multi-MB blob travels through JSON-RPC, so bundle size is a non-issue.

Shell behavior: `App.connect()` → receive tool result → look up the screen in a static **view registry** (keyed by view id carried in tool metadata/result) → provide `McpOpsClient` + seeded query cache + theme → mount that screen's shared container, minus router shell and chat panel.

**Fallback** (only if a host's `_meta.ui.csp` support proves broken at pilot): single-file inlined bundle via `vite-plugin-singlefile` served as the resource body.

### 3.5 Go server: resources capability + tool metadata

- **MCP resources** (greenfield): register `ui://archistrator/shell.html` via go-sdk `AddResource`, returning the §3.4 constant stub with the MCP-Apps mimetype and `_meta.ui.csp`. The handler renders a config-templated string (webApp origin + version) — no file reads, no fetching, no caching in Go. nginx remains the single system of record for all built UI; a webApp deploy updates in-chat views with no server redeploy. Wiring beside `mcp_mount.go` in the hooks seam; one config value for the origin.
- **`_meta.ui.resourceUri` on tools**: stamped by `mcpemit` during codegen. Which operations have a view is declared **in `project.json`** — a small optional `ui` annotation on service-contract operations (schema-first, consistent with doctrine; exact slot shape settled during planning recon). Tools without a view stay plain tools.

## 4. Auth + testing

- **Host-mediated auth model**: the iframe never holds credentials. `app.callServerTool()` is postMessage to the host, which forwards it as an ordinary `tools/call` over its already-authenticated `/mcp` connection — same bearer token, same `AuthMiddleware` principal as model-initiated calls. Consequences: (a) the server cannot distinguish click-initiated from model-initiated calls (by design); (b) there is no per-request header/interceptor equivalent in `McpOpsClient` — anything the server needs must derive from the principal or be a tool argument (generated tools already comply); (c) hosts may apply consent policy to app-initiated mutations (see §3.2 pending-state rule).
- **Pilot**: dev-mode principal injection + `cloudflared` tunnel → Claude custom connector; plus the ext-apps `basic-host` (and/or MCPJam) locally for fast iteration against real local state (fits the run-locally/playwright/STOP-for-review UI loop).
- **Production auth is an explicit earmark, not in this slice**: hosts require MCP-spec OAuth (protected-resource metadata) in front of `/mcp`; the current bearer validator does not advertise that.

## 5. Rollout

1. Build the seam once: eslint ruleset, generated `OpsClient` (interface + REST + MCP impls), shell entry + view registry, Go resources capability (nginx byte-source), `mcpemit` `_meta.ui` stamping, project.json annotation.
2. Wire **two pilot screens** end-to-end: session state (read-mostly) and design review (write-heavy — exercises host consent prompts on `submitReviewDecision`). Each pilot screen is refactored to presentational-pure as part of its migration.
3. Migrate remaining screens mechanically (each screen: purify components → shared container over `OpsClient` hooks → registry entry → project.json annotation).
4. Follow-on spec: replicate the pattern in app-generator (template gains shell entry, ruleset, mapping codegen; generated Go server gains resources capability).

## 6. Risks / open items

- **Contract parity**: assumed 1:1 REST-op ↔ MCP-tool from shared contracts; planning recon must verify every webApp-consumed operation actually has a tool. Diagram render-on-read (Structurizr DSL) is the known suspect.
- **Screen/chat coupling**: how cleanly `DesignExperience` screens factor away from the chat panel is the main refactor unknown — recon item; sizes the redesign.
- **CSP**: nothing in the iframe may call REST; the lint rules are the guard.
- **go-sdk resource support**: verify v1.6.1 exposes what `AddResource` + `_meta` stamping need; upgrade if not.
- **Host `_meta.ui.csp` support is load-bearing** (§3.4): Claude documents it; pilot verifies against basic-host and Claude. Fallback = single-file inlined bundle as the resource body.
- **nginx CORS + cache headers** for the fixed-name shell assets, and per-environment origin config in the stub — small, but new surface; plan recon items.

## 7. Non-goals

- Server B (aiarch-state stdio) changes.
- Production OAuth for `/mcp` (earmarked).
- webApp acting as an MCP *host*.
- SSR / TanStack Start: explicitly deferred, explicitly unblocked. SSR cannot reach the MCP surface (the app is a static resource blob booting in a sandboxed iframe), so it carries no MCP payoff; if adopted later for SPA reasons, the prop-driven component purity helps it, and the §3.5 byte-source just points at the TS server instead of nginx.
- app-generator replication (follow-on spec after the pattern is proven).
