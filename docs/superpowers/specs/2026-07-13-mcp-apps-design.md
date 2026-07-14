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

### 3.2 Two container flavors

- **webApp containers** (SPA): wrap each screen; call TanStack Query hooks → REST `api` layer; map responses to the screen's props; wire callbacks (approve, reject, advance, dispatch, …) to REST mutations. Live in the new `containers` layer element (§3.3).
- **MCP shell containers**: same screens, other data path. Initial data arrives as the host-pushed tool result (`app.ontoolresult`); callbacks and refreshes go through `app.callServerTool()`. Both directions use a **generated op→tool mapping module** (TS, sibling to `gen:api`/`gen-enums` outputs) so tool names, argument shapes, and result unwrapping are typed and mechanical — REST handlers and MCP tools are generated from the same service contracts, so the mapping is derivable, never hand-maintained.

### 3.3 Lint enforcement, platform-wide

The reusable eslint-boundaries package (the TS counterpart to framework-go/arch) grows a new ruleset:

- DAG becomes `routes → containers → (components | hooks) → api`, with `contracts`/`utilities` as leaves.
- `components` may import only `components`, `contracts`, `utilities`. Data-hook calls (`useQuery`/`useMutation`) and `api` imports in `components` are violations.
- Direct `fetch`/network calls outside the `api` layer are violations (this is also what keeps screens CSP-safe in the iframe).

This ruleset ships in the app-generator webApp template so **every archistrator-generated app is MCP-Apps-ready by construction**, not by convention. Existing archistrator webApp screens migrate to comply (per-screen mechanical work; see rollout).

### 3.4 Shell app + bundling

A second Vite entry, `mcp-app.html`, builds a **single-file shell bundle** (`vite-plugin-singlefile`): one `ui://` resource shared by all views so the host preloads it once per conversation and MUI/xyflow are not duplicated per view. The shell: `App.connect()` → receive tool result → look up the screen in a static **view registry** (keyed by view id carried in tool metadata/result) → mount the MCP container for that screen with theme, minus router shell and chat panel. Expected bundle 2–4 MB inlined; measure at the pilot; split into per-manager shells (systemdesign / projectdesign / construction / operations) only if a host chokes on size.

### 3.5 Go server: resources capability + tool metadata

- **MCP resources** (greenfield): register `ui://archistrator/shell.html` via go-sdk `AddResource`, serving the built shell HTML with the MCP-Apps mimetype. The bundle ships as a runtime asset in the server container (config-pathed file, **no `go:embed`** — the Go build must not grow a Node dependency). Wiring beside `mcp_mount.go` in the hooks seam.
- **`_meta.ui.resourceUri` on tools**: stamped by `mcpemit` during codegen. Which operations have a view is declared **in `project.json`** — a small optional `ui` annotation on service-contract operations (schema-first, consistent with doctrine; exact slot shape settled during planning recon). Tools without a view stay plain tools.

## 4. Auth + testing

- **Pilot**: dev-mode principal injection + `cloudflared` tunnel → Claude custom connector; plus the ext-apps `basic-host` (and/or MCPJam) locally for fast iteration against real local state (fits the run-locally/playwright/STOP-for-review UI loop).
- **Production auth is an explicit earmark, not in this slice**: hosts require MCP-spec OAuth (protected-resource metadata) in front of `/mcp`; the current bearer validator does not advertise that.

## 5. Rollout

1. Build the seam once: eslint ruleset, generated op→tool mapping, shell entry + view registry, Go resources capability, `mcpemit` `_meta.ui` stamping, project.json annotation.
2. Wire **two pilot screens** end-to-end: session state (read-mostly) and design review (write-heavy — exercises host consent prompts on `submitReviewDecision`). Each pilot screen is refactored to presentational-pure as part of its migration.
3. Migrate remaining screens mechanically (each screen: purify → webApp container → MCP container → registry entry → project.json annotation).
4. Follow-on spec: replicate the pattern in app-generator (template gains shell entry, ruleset, mapping codegen; generated Go server gains resources capability).

## 6. Risks / open items

- **Contract parity**: assumed 1:1 REST-op ↔ MCP-tool from shared contracts; planning recon must verify every webApp-consumed operation actually has a tool. Diagram render-on-read (Structurizr DSL) is the known suspect.
- **Screen/chat coupling**: how cleanly `DesignExperience` screens factor away from the chat panel is the main refactor unknown — recon item; sizes the redesign.
- **CSP**: nothing in the iframe may call REST; the lint rules are the guard.
- **go-sdk resource support**: verify v1.6.1 exposes what `AddResource` + `_meta` stamping need; upgrade if not.
- **Bundle size**: measured at pilot; per-manager shells are the fallback.

## 7. Non-goals

- Server B (aiarch-state stdio) changes.
- Production OAuth for `/mcp` (earmarked).
- webApp acting as an MCP *host*.
- app-generator replication (follow-on spec after the pattern is proven).
