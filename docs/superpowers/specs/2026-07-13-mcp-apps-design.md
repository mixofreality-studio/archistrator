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

A second Vite entry, `mcp-app.html`, builds the shell as a **classic IIFE bundle with fixed (unhashed) output names** (`mcp-app.js`, `mcp-app.css`) into the same `dist` that `PUSH-APP.sh` already publishes to nginx. The `ui://archistrator/shell.html` resource is a **~1–2 KB constant HTML stub** referencing those assets with **plain `<script src>` / `<link rel="stylesheet">` tags — no `type="module"`, no `crossorigin` attribute**. Classic scripts and stylesheets are CORS-exempt by web-platform rules, so **no CORS configuration exists anywhere in production** (founder ruling 2026-07-14; supersedes the earlier ACAO-on-nginx design — ES-module scripts were the only thing that required it). `_meta.ui.csp.resourceDomains: ["https://<webapp-origin>"]` still declares the origin — that's CSP (host-side load restriction), not CORS. **`connectDomains` stays empty** — the app makes no fetch/XHR at all, which doubles as the exfiltration barrier (§8). Responsibility split: Go serves a config-templated constant string (no fetching, no caching, no asset awareness); nginx serves and cache-controls the assets; the browser HTTP-caches them normally; the MCP host caches the stub resource. Fixed names keep the stub constant; cache-busting via a version query param from config or short TTLs. No multi-MB blob travels through JSON-RPC, so bundle size is a non-issue. Trade-offs accepted: cross-origin classic scripts mute uncaught-error detail at `window.onerror` (mitigated by a top-level React error boundary + ext-apps `sendLog`), and IIFE forbids code-splitting (moot — the shell is one bundle by design).

**Lifecycle (spec-verified):** the iframe is instantiated **per tool call** and torn down via `ui/resource-teardown` — not one persistent app per conversation. Consequences: the shell must boot fast from HTTP cache (fixed-name assets make this a 304 after first load); the view registry keys off **`hostContext.toolInfo.tool` from the `ui/initialize` response** (the host tells the app which tool fired — no viewId smuggled through tool results); and SPA-style background polling (e.g. the 2s `useSessionState` poll) is wrong in MCP context — the MCP `OpsClient` provider disables polling intervals and refreshes on user action instead (hosts may rate-limit app-initiated calls; the spec flags resource consumption).

Shell behavior: `App.connect()` → `ui/initialize` handshake (declares `availableDisplayModes`, receives `toolInfo` + host `theme` + `displayMode`/`containerDimensions`) → registry lookup → provide `McpOpsClient` + seeded query cache (from `ui/notifications/tool-result`) + theme (mapped from host light/dark) → mount that screen's shared container, minus router shell and chat panel.

**Two-loop update model (ratified).** Loop A — interaction inside a widget (`app.callServerTool`): the same iframe updates in place (TanStack pending → invalidate → re-render); no new widget, no model turn. Loop B — anything through the conversation: a model turn that calls a UI-bearing tool renders a **new** widget inline at that point; the thread accumulates widget instances as a timeline of live snapshots. Old widgets revive on interaction (stale-while-revalidate refetch), so they never show rotted state to an active user.

**Commenting UX (ratified; supersedes the earlier "all comment UI SPA-only" ruling).** Comment *thread display* and the ChatRail remain SPA-only. Selection + comment *composition* are dual-hosted: in MCP context `SelectionPopover` stays, and its submit callback (an injected container prop) runs the two-call pattern — `app.updateModelContext(...)` with view state + anchor + comment text (ambient whiteboard, overwrite semantics: the MCP container keeps it continuously updated with "which artifact/element is displayed/selected" so bare chat references like "this step" resolve), then `app.sendMessage({role: "user", ...})` as the explicit trigger that starts the model turn. If the host rejects `sendMessage` (`isError`), the widget degrades to "shared with conversation — ask the agent to file it." The agent files the comment via the existing tools with the item-granular anchor from context.

**Display-mode adaptivity.** The spec's modes are `inline | fullscreen | pip`, host-negotiated (Microsoft's side-by-side is their rendering of the expanded surface, not a protocol standard — never assume it). Pure screens receive `displayMode`/`containerDimensions` as props and render density-adaptively: an **inline-compact variant** (glanceable summary, ≤2 actions, single-scroll) and the full screen for expanded modes; dense screens request expanded via `ui/request-display-mode` when `hostContext.availableDisplayModes` offers it. Inline-compact variants are per-screen work tracked in the migration checklist (pilot: session-state ships inline-compact; design-review requests expanded with a capable inline fallback). Every widget gets an "open in archistrator" affordance via `ui/open-link` to the corresponding SPA route.

**Fallback** (only if a host's `_meta.ui.csp` support proves broken at pilot): single-file inlined bundle via `vite-plugin-singlefile` served as the resource body.

### 3.5 Go server: resources capability + tool metadata

- **MCP resources** (greenfield): register `ui://archistrator/shell.html` via go-sdk `AddResource`, returning the §3.4 constant stub with mimetype **`text/html;profile=mcp-app`** (spec-verified) and `_meta.ui.csp`. The handler renders a config-templated string (webApp origin + version) — no file reads, no fetching, no caching in Go. nginx remains the single system of record for all built UI; a webApp deploy updates in-chat views with no server redeploy. Wiring beside `mcp_mount.go` in the hooks seam; one config value for the origin.
- **`_meta.ui.resourceUri` on tools**: stamped by `mcpemit` during codegen. Which operations have a view is declared **in `project.json`** — a small optional `ui` annotation on service-contract operations (schema-first; exact slot shape settled during planning recon), e.g. `{"name": "getSessionState", "ui": {"view": "system-design-session"}}`. **Ruling: UI goes on state-read ops only** (~5–8 of ~35: the `getSessionState` family, `queryOperatedSystemView`, …); mutations stay text-only — the agent narrates outcomes and re-reads state, which is what renders the fresh widget. The same `view` ids drive the generated TS view registry, so the Go-side stamp and webApp-side registry are projections of one field and cannot drift. App-initiated `callServerTool` results never spawn widgets (they return to the calling app), so screens may freely call mutations internally.

## 4. Auth + testing

- **Host-mediated auth model**: the iframe never holds credentials. `app.callServerTool()` is postMessage to the host, which forwards it as an ordinary `tools/call` over its already-authenticated `/mcp` connection — same bearer token, same `AuthMiddleware` principal as model-initiated calls. Consequences: (a) the server cannot distinguish click-initiated from model-initiated calls (by design); (b) there is no per-request header/interceptor equivalent in `McpOpsClient` — anything the server needs must derive from the principal or be a tool argument (generated tools already comply); (c) hosts may apply consent policy to app-initiated mutations (see §3.2 pending-state rule).
- **Pilot**: dev-mode principal injection + `cloudflared` tunnel → Claude custom connector; plus the ext-apps `basic-host` (and/or MCPJam) locally for fast iteration against real local state (fits the run-locally/playwright/STOP-for-review UI loop). Note: `basic-host` is browser-based and calls `/mcp` cross-origin, so the Go server needs **dev-profile-only CORS headers on `/mcp`** (the build guide's example does the same via `cors()`); production hosts call server-to-server.
- **Production auth is an explicit earmark, not in this slice**: hosts require MCP-spec OAuth (protected-resource metadata) in front of `/mcp`; the current bearer validator does not advertise that.

## 5. Rollout

1. Build the seam once: eslint ruleset, generated `OpsClient` (interface + REST + MCP impls), shell entry + view registry, Go resources capability (nginx byte-source), `mcpemit` `_meta.ui` stamping, project.json annotation.
2. Wire **two pilot screens** end-to-end: session state (read-mostly) and design review (write-heavy — exercises host consent prompts on `submitReviewDecision`). Each pilot screen is refactored to presentational-pure as part of its migration.
3. Migrate remaining screens mechanically (each screen: purify components → shared container over `OpsClient` hooks → registry entry → project.json annotation).
4. Follow-on spec: replicate the pattern in app-generator (template gains shell entry, ruleset, mapping codegen; generated Go server gains resources capability).

## 6. Risks / open items — pilot-resolved status (2026-07-15)

Resolved during implementation + the basic-host pilot (details: branch `mcp-apps-pilot`, findings in `.superpowers/sdd/task-11-findings.md`):

- ✅ **Contract parity** — full: the webApp consumes only manager ops (diagrams are client-side xyflow; the render ops live in an internal Engine). `visibility:["app"]` escape hatch unused, still earmarked.
- ✅ **Screen/chat coupling** — resolved per §3.4; pilot renders the full committed-artifact view without chat/comment surfaces.
- ✅ **go-sdk v1.6.1** — `Tool.Meta`/`Resource.Meta`/`AddResource` all present; no upgrade.
- ✅ **Host CSP support** — basic-host builds its sandbox CSP from `_meta.ui.csp.resourceDomains` correctly; classic-tag assets load with zero CORS config. Claude-host verification pending (Task 11 remainder).
- ⚠️ **toolInfo is spec-optional and the reference host omits it** (F-T11-3): registry resolution falls back to the sole distinct view; whether Claude supplies `toolInfo` (and `_meta.ui.view` through it) is the key host-variance datum still to collect.
- ⚠️ **Transport-shape parity is a standing invariant, not a one-time fix** (F-T11-4): mcpemit's single-`result` envelope is unwrapped by `McpOpsClient`; any future divergence between REST and MCP result shapes silently breaks hooks. Owned by the testing earmark below.
- ⚠️ **Legacy-allowlist components break in the iframe**: `VolatilityMap`/`OperationalConceptsView` (un-purified, hooks-importing) error in MCP context — un-migrated `apiClient`-based hooks cannot fetch inside the sandbox (by design). Fix = their burn-down migration; the per-kind render matrix (testing earmark) pins it.

## 6b. Testing earmark (direction ratified 2026-07-15, deferred)

Three tiers: (1) protocol regression in `server/cmd/server` (seam test, no-boolean-schemas — SHIPPED); (2) **transport-parameterized systemtests** — founder ruling: NOT call-both-and-compare, but the same suite run twice against the two generated client impls (REST / MCP), the OpsClient abstraction mirrored at the test-suite level; (3) `mcptests` — Playwright over an in-repo instrumented host harness built on `@modelcontextprotocol/ext-apps/app-bridge` (records ui/* messages for assertions; simulates host variance incl. toolInfo on/off), with a per-artifact-kind × session-stage render matrix. Same use-case parameterization should eventually unify mcptests/uitests. Land tier 3 BEFORE the remaining-screens migration wave.

## 7. Non-goals

- Server B (aiarch-state stdio) changes.
- Production OAuth for `/mcp` (earmarked).
- webApp acting as an MCP *host*.
- SSR / TanStack Start: explicitly deferred, explicitly unblocked. SSR cannot reach the MCP surface (the app is a static resource blob booting in a sandboxed iframe), so it carries no MCP payoff; if adopted later for SPA reasons, the prop-driven component purity helps it, and the §3.5 byte-source just points at the TS server instead of nginx.
- app-generator replication (follow-on spec after the pattern is proven).

## 8. Security & spec-compliance review (2026-07-13, OWASP × ext-apps spec 2026-01-26)

### 8.1 Spec compliance (verified against `specification/2026-01-26/apps.mdx`)

| Requirement | Our design |
|---|---|
| Tool meta key `_meta.ui.resourceUri` (not the deprecated `ui/resourceUri`) | ✅ mcpemit stamps the nested form |
| Resource mimetype `text/html;profile=mcp-app` | ✅ §3.5 |
| CSP via `_meta.ui.csp.resourceDomains`; hosts MUST NOT allow undeclared domains | ✅ webApp origin only; `connectDomains`/`frameDomains`/`baseUriDomains` empty |
| Server MUST include meaningful fallback `content[]` even when UI renders | ⚠️ **rule for mcpemit**: tool results keep their full text/structured payloads; never "see the UI above" |
| Server SHOULD validate the client's `io.modelcontextprotocol/ui` extension capability before advertising UI tools | ⚠️ plan item: check capability at initialize if go-sdk exposes it; `_meta` is inert to non-supporting hosts, so graceful either way |
| View MUST use postMessage only, declare `availableDisplayModes` in `ui/initialize` | ✅ ext-apps `App` class handles |
| Per-tool-call iframe lifecycle + `ui/resource-teardown` | ✅ §3.4 (fast cached boot, no background polling) |
| `_meta.ui.visibility` (`model`/`app`) | Default (both) for all ops in this slice; no app-only tools |

### 8.2 OWASP findings

- **A03 XSS-in-iframe is the top threat, and it is not "contained by the sandbox":** script injected into the app can call `tools/call` with the user's principal (approve reviews, `executeNextActivity` → spends real money), poison the conversation via `ui/update-model-context`, and phish via `ui/open-link`. The app renders LLM/user-authored artifact content, so this is a live concern. Current state verified clean: `react-markdown` without `rehype-raw`, zero `dangerouslySetInnerHTML` in `webApp/src`. **Control: lint rules ban `dangerouslySetInnerHTML` and `rehype-raw` in the components layer** (ships in the platform eslint config). Empty `connectDomains` means even successful injection has no direct exfil channel — everything auditable rides host-mediated JSON-RPC.
- **A01 access control:** every tool must be safe to call directly by a hostile client holding the user's session — server-side per-operation authorization on the principal, never "the model wouldn't call this." Already the manager-op posture; named here as an invariant the MCP surface inherits. Host consent on app-initiated mutations is defense-in-depth, not the authz boundary.
- **A07 pilot exposure (highest operational risk):** dev-mode principal + cloudflared = **unauthenticated archistrator reachable from the public internet** while the tunnel is up. Controls: throwaway project state only, tunnel up only during active sessions, and **production OAuth graduates from earmark to release-blocker** before any connector touches real state.
- **A05 misconfiguration:** production carries **zero CORS configuration** (classic-script delivery, §3.4). The only CORS anywhere is the dev-profile-only `/mcp` allowance for the browser-based `basic-host` pilot harness, gated on the dev-mode flag and never shipped enabled.
- **A08 integrity:** assets over TLS from our origin; ext-apps package version pinned. SRI considered and deferred — integrity hashes would make the stub non-constant per deploy (revisit if assets ever move to a third-party CDN).
- **A10 SSRF:** eliminated by design — the CSP-stub ruling removed all server-side fetching.
- **Social engineering (spec-acknowledged):** the UI renders model-influenced content next to real action buttons; irreversible/spending ops (`advanceToConstruction`, `executeNextActivity`) keep an explicit in-UI confirm step rather than single-click, independent of host consent behavior.
