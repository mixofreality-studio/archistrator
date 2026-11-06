# FPC Plan 5 — Delivery Surfaces and the Generated MCP Apps Client

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Step detail is filled from Plan 4's final signatures before execution.

**Goal:** A System that lacks a surface the brief committed to is refused at design time, and an MCP Apps UI is a Client the platform generates (resources, metadata, bridge), with the agent owning only the UI bundle.

**Architecture:** `Surface` is a closed sum/enum on Client components replacing `uiSurface`/`machineProtocol` (B10: delete, don't alias). `mission.deliverySurfaces` is a typed list the PM ratifies. Three rules refuse gaps at design. `mcpemit` derives `OpUI` and serves `ui://` resources from the model; webgen scaffolds the host-bridge entry for an `mcpApp` surface. **Founder ruling 4 is required** (and ruling 9 for the own-project waiver).

**Spec:** §W6; PM Q2/Q4. **Depends on:** Plan 4 (acceptance harness host for `mcpApp`).

## Global Constraints (plan-local)
- `Surface` values: `web | rest | mcpTools | mcpApp`. Every switch is exhaustive. `uiSurface` and `machineProtocol` are deleted from the schema, projectmodel, methodcheck, estimation and webApp in the same change (GC‑5: own `project.json` conformed).
- `GEN-SURFACE-SUPPORTED` reads a platform-declared list `appgen.SupportedSurfaces()`; the list grows only when a generator and a conformance fixture exist for the surface.

## Review Focus
1. **MCP Apps "covered" by the MCP tool server** (RUN-021): Task 5.2 `TestSYS_SurfaceRequired_KindMustMatch`.
2. **REST client dropped** (RUN-015/019): Task 5.2 `TestSYS_SurfaceRequired_FiresOnMissingRest`.
3. **mcpApp designed before the generator exists**: Task 5.2 `TestGEN_SurfaceSupported_RefusesUnsupported`.
4. **Core use case entered only through one surface when two are required**: Task 5.2 `TestSYS_SurfaceEntry_FiresWithoutAlt`.
5. **ui:// resource served with the wrong mimeType**: Task 5.4 `TestMCPApp_ResourceMime`.

---

### Task 5.1: `Surface` sum type and `deliverySurfaces` on the mission
**Files:** app `$defs` (`Surface` enum; `SystemComponent.surface` required for `kind: client`; `Mission.deliverySurfaces: [{kind: Surface, audience: human|machine, source: string(non-empty quote), objective: <objective id>}]`), `projectmodel/system.go` (`SystemComponent.Surface`), `methodcheck/project.go`, `estimationengine.go` (`clientAppActivityFor` keyed on `Surface ∈ {web, mcpApp}`; `codingActivityFor` MachineClient on `{rest, mcpTools}`), webApp enums regen, `the-method-business-alignment` + `mission-draft.md` (architect lists surfaces with brief quotes; PM ratifies in `mission-critique`). Data: own `project.json` conformed. Tests: decode/exhaustiveness, `TestDerivedPlan_SurfacesSelectActivityType`.

### Task 5.2: Design-time rules
**Files:** `methodcheck/rules_surfaces.go` (+test), `emitters.go`.
| id | statement |
|---|---|
| `SYS-SURFACE-REQUIRED` | every `deliverySurfaces[].kind` has ≥1 Client with that `surface`; a human-audience surface's Client must be `web` or `mcpApp`, never `mcpTools`/`rest` |
| `SYS-SURFACE-ENTRY` | each core use case's first actor step carries one call per required surface that actor uses, as an `alt` group (per-use-case waiver via the existing waiver mechanism) |
| `GEN-SURFACE-SUPPORTED` | every Client `surface` ∈ `appgen.SupportedSurfaces()` (methodcheck receives the list through `ProjectSpec.SupportedSurfaces`; the app passes `appgen.SupportedSurfaces()`) |

### Task 5.3: `mcpApp` generation — server side
**Files:** `clientgen/mcpemit/mcpemit.go` (derive `OpUI` for every tool the `mcpApp` Client fronts — the Manager ops reached from that Client in dynamic views; set `_meta.ui.resourceUri = ui://<app>/<surface>/shell.html`), new `mcpemit/emit_resources.go` (`resources/list` + `resources/read` serving the built bundle from `<PREFIX>_WEB_DIST/<surface>/` with mimeType `text/html;profile=mcp-app`, CSP header from a platform constant), `composegen` mounts them. Tests: `TestMCPApp_OpUIDerivedFromModel`, `TestMCPApp_ResourceMime`, `TestMCPApp_NoResourceWithoutMcpAppClient`.

### Task 5.4: `mcpApp` generation — web side
**Files:** `webgen/bootstrap.go` + templates (`<surface>.html` for `mcpApp` with the host-bridge bootstrap, `src/<surface>/main.tsx` using the `mcpOpsClient` transport from `ops.gen.ts`), `testgen/emit_playwright.go` (an `mcpApp` surface's specs run inside the harness host page: scenariohost gains `MCPAppHost(app)` serving the basic-host from the 2026-07-15 MCP Apps work), `appgen.SupportedSurfaces()` gains `mcpApp`. Tests: `TestWebgen_McpAppScaffold`, `TestPlaywright_McpAppRunsInHost`.

### Task 5.5: Release, pin, proof, own project
- Conformance: todomvc fixture gains `deliverySurfaces` (all four, with brief quotes) and a `todo-mcp-app` Client; chain + boot + acceptance green; bench's `frontend-mcpapps-surface` probe (initialize → tool with `_meta.ui.resourceUri` → `resources/read` mime) reproduced as `TestConformance_McpAppSurface`.
- Own project: mission `deliverySurfaces` (web, rest, mcpTools, mcpApp — archistrator already ships `ui://` in `cmd/server/mcp_apps.go`); migrate that hand-written shell to the generated path or waive `GEN-SURFACE-SUPPORTED` per ruling 9 with `expires`.

## Done when
A System missing a required surface is refused by `putDraftModel`; `TestConformance_McpAppSurface` green; own project conformed or waived.
