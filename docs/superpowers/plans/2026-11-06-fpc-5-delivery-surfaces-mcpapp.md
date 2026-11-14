# FPC Plan 5 — Delivery Surfaces and the Generated MCP Apps Client

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Expanded to step detail 2026-11-14 from the code at platform `fpc/main` @9d75a5cc and app `worktree-first-pass-correctness-plan` @843ffdbb (Plans 1–4 landed and released: framework-go v0.24.0, projectmodel v0.5.0, http-generator v0.6.0, app-generator v0.26.0, scenariohost v0.7.0, method-assets v0.26.0, framework-go-conformance v0.3.0).

**Goal:** A System that lacks a surface the brief committed to is refused at design time, and an MCP Apps UI is a Client the platform generates (resources, metadata, bridge), with the agent owning only the UI bundle.

**Architecture:** `Surface` is a closed sum/enum on Client components replacing `uiSurface`/`machineProtocol` (B10: delete, don't alias). `mission.deliverySurfaces` is a typed list the PM ratifies. Three rules refuse gaps at design. `mcpemit` derives `OpUI` and serves `ui://` resources from the model; webgen scaffolds the host-bridge entry for an `mcpApp` surface. **Founder ruling 4 is required** (and ruling 9 for the own-project waiver). Both are adopted per the spec's recommendations.

**Spec:** §W6; PM Q2/Q4; §6 rulings 4, 8, 9. **Depends on:** Plan 4 (acceptance harness host for `mcpApp`).

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

## Commands (every task)

- Platform: `P=/Users/davidmarne/mixofrealitystudio/fpcwt/archistrator-platform`. Each module tests standalone with `GOWORK=off` from its own directory; the sibling `replace` directives (`framework-go-app-generator/go.mod:57-61`, `framework-go-scenariohost/go.mod:122-…`) make an unreleased change in `framework-go` / `framework-go-projectmodel` / `framework-go-app-generator` visible to their dependents. Commit with `git -c user.name="David Marne" -c user.email="davemarne@gmail.com" commit` (the repo-local identity is "uitests"); stage explicit paths only.
- App: `A=/Users/davidmarne/mixofrealitystudio/archistrator/.claude/worktrees/first-pass-correctness-plan`; every go/make/npm command runs with `GOMODCACHE=/Users/davidmarne/.claude/jobs/a7cfb4f6/tmp/gomodcache-app6 ASDF_NODEJS_VERSION=25.2.1`. Until Task 5.5 pins the release, app packages run in workspace mode: `GOWORK=$A/go.work` (it `use`s `../archistrator-platform`, a symlink to `$P`). App edits are staged on the branch and **committed only in Task 5.5** (Plans 3–4 precedent). Never stage `server/cmd/aiarch-state-mcp/acceptancecheck_test.go` (a concurrent agent owns it).

## Detail-phase decisions (2026-11-14; each derived from the code named, the lead may override)

- **D1 — Surface's Go form.** The wire is a string enum (`"web" | "rest" | "mcpTools" | "mcpApp"`): a Client has one surface and no variant carries a payload, so modelgen's `{"kind","model"}` envelope would add nothing. To keep GC‑2's exhaustiveness checked by the linter already on (`gochecksumtype`, `$P/.golangci.yml:15-25`; there is no `exhaustive` linter), every Go mirror pairs `type Surface string` with a sealed `SurfaceKind` (`//sumtype:decl`, four zero-size variants) obtained by `ParseSurface`; code that branches on a surface switches over `SurfaceKind` with no `default`. Mirrors: `framework-go-projectmodel` (generators; dependency-free) and `framework-go/methodcheck` (rules; framework-go keeps its own structural mirror, `methodcheck/project.go:383-389`). The app's modelgen type (`projectstate.Surface`, a string enum with `x-enum-varnames` like `ActivityMethodPhase`; an optional `$ref` decodes to a pointer as `Actor.Kind *ActorKind` does, `projectstate/contract.gen.go:128-131`) is branched through `projectmodel.ParseSurface`. No TS code branches on a surface in this plan; `schema.ts` only regenerates.
- **D2 — the predicates that replace `uiSurface`/`machineProtocol`.** Every current reader maps onto one of these:

  | today | after | readers |
  |---|---|---|
  | `uiSurface: true` — hand-built in `webApp/src/<id>`, browser contract, UI design, Playwright, U-SPA activity | `surface ∈ {web, mcpApp}` — `IsBrowserSurface()` / `isBrowserSurface()` | rungen `web` (`rungen.go:136`), deployment `webDistDir` (`deployment.go:473`), conformance chain (`chain.go:160`), methodcheck (`align.go:267`, `rules_testplan.go:346`, `rules_testplan_v2.go:98`, `rules_statevalidation.go:223`), app `RecordBrowserContract` (`projectstateaccess.go:13590`) |
  | `uiSurface: false` — machine client | `surface ∈ {rest, mcpTools}` — `isMachineClient()` | `rules_statevalidation.go:134,199`; app `RecordMachineClientContracts` (`projectstateaccess.go:13775`) |
  | `machineProtocol: "rest"` | `surface == rest` — `isRESTClient()` | `align.go:267`, `rules_statevalidation.go:120`, app `machineClientContract` (`:13898`) |
  | `machineProtocol: "mcp"` | `surface == mcpTools` | app `machineClientContract` (`:13892`) |
  | — (new) a page served at `/` | `surface == web` — `IsWebSurface()` | composegen static handler (`surfaces.go:96-108`), devstack smoke `SPA` (`smoke.go:49`) |
  | `IsMCPClient()` — gets MCP tool tables | `surface ∈ {mcpTools, mcpApp}` — `ServesMCPTools()` (replaces `IsMCPClient`) | clientgen `machineClients` (`clientgen.go:218-229`), composegen `mcpClients` (`surfaces.go:80-91`), devstack `isMCPClient` (`smoke.go:93`) |

  Data migration (one throwaway script, deleted before its commit, as Plan 2's `cmd/tpmigrate`): `uiSurface: true` → `surface: "web"`; `uiSurface: false` + `machineProtocol: "rest"` → `"rest"`; + `"mcp"` → `"mcpTools"`; `uiSurface: false` with no protocol (only planned clients carry that) → no `surface`.
- **D3 — `SYS-CLIENT-SURFACE` replaces `SYS-CLIENT-UISURFACE` and `SYS-CLIENT-PROTOCOL`** (`rules_statevalidation.go:36-43,89-145`; neither id is in `emittedRuleIDs`, the new one is registered): a built Client states a surface; a stated surface parses; a non-Client states none; at most one `rest` Client (moved from SYS-CLIENT-PROTOCOL). A planned or external Client may omit it, as SYS-CLIENT-PROTOCOL already exempts one: the own `scheduler-client` (planned, timer-driven) is none of the four surfaces, and the enum is fixed by this plan's GC.
- **D4 — activity carriers.** The derived plan carries the Client's choice on two booleans (`projectstate.ActivityItem.UISurface/MachineClient`, `projectstateaccess.go:4914-4920`; estimation `DerivedActivity.uiSurface/machineClient` and `SystemComponent.uiSurface`, estimationEngine `$defs`). B10: each becomes one `surface`, and `ClassifyActivity(id, workerClass string, coding bool, surface Surface)` (`:9527`) maps `web|mcpApp` → Frontend, `rest|mcpTools` → MachineClient, `""` → the existing rules below it. Callers: `deliverymanager.go:4712,4738-4739,8400,8441,11142`, `cmd/backfill-attempts/main.go:337,1413,1419`, `projectstateaccess.go:9589,9631,12990`. The own slot 9 (`U-SPA-web-client`) and the fixture's slot 9 (`C-todo-api`, `C-todo-mcp-server`, `U-SPA-todo-web-app`) migrate with D2's script; nothing is re-derived (E23: no sanctioned slot-9 writer).
- **D5 — the `deliverySurfaces` record.** `MissionStatement.deliverySurfaces []DeliverySurface{kind Surface, audience Audience (human|machine), source string, objective int}`, `required` (presence). Non-emptiness lives in Go at the record verb (`ValidateModelIdentities` → `missionObjectiveProblems`, `projectstateaccess.go:5560-5585`, renamed `missionProblems`): at least one entry, a non-blank `source`, `objective` names exactly one objective `number`, a kind listed once, and `audience` agrees with the kind (`web|mcpApp` human, `rest|mcpTools` machine). The agreement is repeated in SYS-SURFACE-REQUIRED for CI, because the app attributes every `SYS-` finding to the System slot (`ruleSlotAttributionPrefixes`, `projectstateaccess.go:12198-12210`), so a mission session would only see it as a warning. The todomvc fixture's seven objectives all carry `number: 0`; Task 5.5 renumbers them 1..7 (the fixture has no slot 6, so no `justifyingObjective` refers to them).
- **D6 — validate options and where the supported list lives.** `methodcheck.ValidateProject(p, o ValidateOptions)` and `ValidateProjectJSON(raw, o ValidateOptions)` take `ValidateOptions{SupportedSurfaces []Surface}`; there is no default, so every caller chooses (`validate.go:15,196`; callers: `check.go:85`, conformance `fixtures_test.go:30`, `acceptance_test.go`, `run021_test.go`, methodcheck `decode_test.go`, `rules_testplan_test.go`, `rules_usecase_io_test.go`; app `cmd/aiarch-state-mcp/state.go:126` (putDraftModel), `constructverbs.go:93`, `validate.go:127`, `recordtestplan.go:82,95`, tests `ownproject_test.go`, `rig_test.go`, `run010_test.go`). An empty list fails closed: GEN-SURFACE-SUPPORTED fires on every Client that states a surface. `ProjectSpec.SupportedSurfaces` threads it through `Check` (`check.go:23-96`). `appgen.SupportedSurfaces()` is a new importable package `framework-go-app-generator/appgen` (`cmd/appgen` is `package main`); app-generator already imports methodcheck (`clientgen.go:35`), so it returns `[]methodcheck.Surface`.
- **D7 — SYS-SURFACE-ENTRY, exactly.** R = the mission's `deliverySurfaces` kinds. For an actor A, Uses(A) = { surface(C) : some dynamic-view call A→C, C a Client } ∩ R. It is data-driven, not by actor kind: the own `architect-user` (kind `human`) enters through `web-client` and `mcp-client` alike (25/25 alt-tagged calls), so "humans use human surfaces" would be wrong. For each core use case U and each actor A that calls a Client in U's view, A's entry step (the first step of the view holding a call A→Client) must hold, for each s ∈ Uses(A), a call A→C with surface(C) = s, and when |Uses(A)| > 1 those calls share one non-empty `alt` (`TraceCall.Alt`, `project.go:398-424`). The per-use-case waiver is the existing host-artifact waiver record: a `System.waivers[]` `CheckItem{section: <useCaseId>, guideline: "SYS-SURFACE-ENTRY", status: "waived", justification}` (the app's `System.waivers`, read today by `deliverymanager.go:1908`; methodcheck's `System` mirror gains `Waivers []CheckItem`).
- **D8 — OpUI is derived from the views.** The Manager operations an `mcpApp` Client reaches are what the app already computes for a machine client (`machineClientManagerOps`, `projectstateaccess.go:13829-13852`). It is hoisted to `scenario.ManagerOpsThrough(in, client)` (beside `ClientManagerOps`, `framework-go/scenario/clientops.go:286`) and the app calls the hoisted one. `appgen clients` decodes the project with methodcheck, builds `clientgen.Project.MCPApps`, and clientgen gives every tool table of a Manager the same `_meta.ui.resourceUri = ui://<app>/<surface>/shell.html` on the ops any mcpApp reaches (`<app>` = `clientgen.AppName(cfg)`: `Config.App`, else the module leaf, `clientgen.go:126-133`; the fixture's module `example.com/conformance/todomvc`, `chain.go:298`, gives `todomvc`). An op reached by two mcpApp Clients, or reached and also carrying the hand `ui.view` annotation (`projectmodel.OpUI`, `contractdoc.go:60-69`), is a generation error. The annotation path (`shellResourceURI = ui://<app>/shell.html`, `mcpemit.go:270-280`) stays only for the own hand shell (D12) — earmark E29.
- **D9 — where an mcpApp's server half lives.** An mcpApp Client is MCP-served: clientgen writes its fronted Managers' tool tables exactly as for an mcpTools Client (`internal/client/<leaf>/<pkg>`), and composegen registers each Manager once (the existing `seen`, `surfaces.go:43-62`). The resources are one generated file of the composition root, `cmd/server/mcpapps.gen.go` (package `main`), emitted by `mcpemit.GenerateResources` and written and called by composegen: `registerMCPAppResources(srv *mcp.Server, webDistDir string)` adds one resource per built mcpApp Client whose read handler reads `<webDistDir>/<surface>/shell.html` at read time. MIME `text/html;profile=mcp-app` (`mcpemit.ResourceMIMEType`; ext-apps' `RESOURCE_MIME_TYPE` and the own `shellMIMEType`, `cmd/server/mcp_apps.go:16`); `_meta.ui.csp = {"connectDomains": [], "resourceDomains": []}` (`mcpemit.ResourceCSP()`, the platform constant: the bundle is one self-contained file that calls only through the host bridge). The static page handler mounts only for a built `web` surface (an mcpApp writes no top-level `<surface>.html`, and `newSurfaceHandler` refuses a dist without one), while `webDistDir` stays declared for any browser surface.
- **D10 — the web half's build.** The shared `vite.config.ts` seat builds every root `*.html` as a page the server serves at `/`, and shared seats must be surface-neutral (FIX-036, `webgen/bootstrap.go:21-24`). So an mcpApp's entry is `<surface>.mcpapp.html` (not the sketch's `<surface>.html`); the page build ignores `*.mcpapp.html`; a shared seat `scripts/build-mcpapps.mjs` builds each `*.mcpapp.html` with Vite's `build()` API and a small inline plugin into one self-contained `dist/<surface>/shell.html` (script and CSS inlined); the bootstrap `package.json` build is `tsc -b && vite build && node scripts/build-mcpapps.mjs` (a no-op with no mcpApp entry). `webgen.Bootstrap` gains the surface kind (`cmd/webgen bootstrap -kind`), and the chain passes each Client's surface.
- **D11 — the harness host.** The 2026-07-15 basic-host was cloned to `/tmp/ext-apps` (`scripts/mcp-pilot-up.sh:22`) and nothing of it is in either repo (no MCP Apps E2E exists in the app's `uitests/`), so scenariohost carries a minimal host written fresh after ext-apps' `examples/basic-host`: `src/host.ts` connects an `@modelcontextprotocol/sdk@1.29.0` client to `/mcp` on the host's own origin (reverse-proxied to the launched app, so no dev CORS is involved), calls a tool whose `_meta.ui.resourceUri` names the surface, reads the resource, renders it in `<iframe id="mcp-app" sandbox="allow-scripts allow-forms">` via `srcdoc` with the requested route set as `location.hash` first, and bridges it with `@modelcontextprotocol/ext-apps@1.7.4`'s `AppBridge(client, hostInfo, capabilities)` over `PostMessageTransport(eventTarget, eventSource)` (`app-bridge.d.ts:262`, `message-transport.d.ts:62`; both versions are the ones `webApp/node_modules` holds). esbuild bundles it to `mcpapphost/dist/host.js`, committed and embedded; its first line records the sha256 of its sources, which a Go test re-checks with no node. Generated specs for an mcpApp plan open the app with `openApp(page, route)` for a `navigate` action and locate every element inside `appFrame(page)`; a `url` observable is a testgen error (the user sees no address bar). `go tool webscenarios` serves the host when the config's plan key resolves to an mcpApp Client and gives Playwright the host's URL.
- **D12 — the own project (ruling 9).** The own research corpus names two surfaces and no others: `.aiarch/state/research/01-founder-brief.txt:7` "host a UI portal" (web, human) and `00-mission.txt:18` "the user drives it with their own AI agent (over the remote MCP channel)" (mcpTools, machine). Nothing names a REST API or MCP Apps, and `source` must be the brief's words, so the own `deliverySurfaces` is `[web → objective 4, mcpTools → objective 8]` (the PM ratifies; the lead may override). GEN-SURFACE-SUPPORTED therefore cannot fire on the own project (it has no mcpApp Client) and no ruling-9 waiver is written. The hand `ui://archistrator/shell.html` stays on the generated `MCPMount` hook (`cmd/server/hooks.go:600-601`, `mcp_apps.go`) with the `ui.view` annotation on `deliveryManager.QueryProjectView`. Migrating it to the generated path is not feasible in this plan: the shell routes several views by `_meta.ui.view` (`webApp/src/mcpShell/resolveView.ts`) and loads `mcp-app.js` from the webApp origin (`webApp/vite.mcp.config.ts`), and the generated path needs an own mcpApp Client in the System (a design-rail amendment, a new derived `U-SPA-*` activity and a slot 9/10 re-materialization, which has no sanctioned writer, E23). Earmark E29. If the founder adds an MCP Apps line to the brief, its entry makes SYS-SURFACE-REQUIRED fire on the own System, and the ruling-8/9 fallback in Task 5.5 Step 6 applies.
- **D13 — the fixture's mcpApp.** `todo-mcp-app` reuses the harvested SPA: its `routes/App.tsx` re-exports `todo-web-app`'s `App`, its `main.tsx` is the generated host-bridge seat over `mcpOpsClient` (the SPA already calls only through `useOpsClient`, `impl/webApp/src/todo-web-app/hooks/useTodoList.ts:2`). Its UI design and test plan are `todo-web-app`'s with the surface key changed (no web binding uses a `url` observable). Its acceptance variants (`<id>@todoMcpApp`) are bound with a dated skip (earmark E28: acceptance through an MCP Apps host), and testgen refuses an unskipped acceptance variant whose target is an mcpApp, so none can silently run against the SPA.
- **D14 — Review Focus 5's task.** It names Task 5.4; the original task list (and this expansion) writes `TestMCPApp_ResourceMime` in Task 5.3, the server half that serves the resource.

## File map

Platform:
- Create `framework-go-projectmodel/surface.go` (+`surface_test.go`); modify `system.go:30-61`, `system_test.go`, `deployment_test.go`.
- Create `framework-go/methodcheck/surfaces.go`, `surfaces_test.go`, `rules_surfaces.go`, `rules_surfaces_test.go`, `testdata/surfaces_clean.json`; modify `project.go` (`MissionStatement` :171-175, `System` :294-298, `Component` :325-363, consts :860-865), `rules_statevalidation.go:34-145`, `rules.go:99-100`, `validate.go:15-35,196-205`, `check.go:23-96`, `emitters.go:33-37,97`; migrate `testdata/{project,project_clean,scenario_project,tp_clean,tp_drift,tp_legacy,tp2_clean,tp2_vacuous,atp_clean,atp_unbound,atp_badref}.json`, `testdata/fresh/run010.design.json`, and the inline fixtures of `rules_test.go`, `align_buildstatus_test.go`, `rules_clientops_test.go`, `rules_acceptance_test.go`, `rules_testplan_v2_test.go`.
- Modify `framework-go/scenario/clientops.go` (`ManagerOpsThrough`), `clientops_test.go`.
- Create `framework-go-app-generator/appgen/surfaces.go` (+test), `clientgen/mcpemit/emit_resources.go` (+`emit_resources_test.go`), `composegen/mcpapps.go`, `composegen/mcpapps_test.go`, `composegen/mcpapps_http_test.go`, `webgen/templates/bootstrap/{mcpapp.html.tmpl,mcpapp.main.tsx.tmpl,build-mcpapps.mjs.tmpl}`, `testgen/emit_mcpapp_test.go`; modify `clientgen/clientgen.go` (`Project`, `machineClients`, `mcpTools`, `AppName`), `clientgen/mcpemit/mcpemit.go` (`Options.UI`, `emitHandlerAndRegister` :267-312), `composegen/{composegen.go,surfaces.go}`, `devstack/smoke.go:47-52,93`, `cmd/appgen/main.go:373-395,410-429`, `webgen/{bootstrap.go,templates/bootstrap/vite.config.ts.tmpl,templates/bootstrap/package.json.tmpl,webgen_test.go}`, `cmd/webgen/main.go:130-145`, `testgen/{testgen.go,emit_playwright.go,emit_elements_ts.go,emit_surface_ts.go,emit_acceptance.go,tsruntime/runtime.ts}`; migrate the inline fixtures of `composegen/surfaces_test.go`, `clientgen/clientgen_test.go`, `rungen/{rungen_test.go,unseated_test.go}`, `cmd/appgen/main_test.go`, `modelgen/genproof_test.go`.
- Create `framework-go-scenariohost/mcpapphost.go`, `mcpapphost_test.go`, `mcpapphost/{package.json,package-lock.json,build.mjs,index.html,src/host.ts,dist/host.js,.gitignore}`; modify `cmd/webscenarios/main.go:223-281` (+`main_test.go`).
- Modify `method-assets/assets/claude/commands/{mission-draft.md,mission-critique.md,system-draft.md}`, `assets/claude/skills/{the-method-business-alignment,the-method-activity-list}/SKILL.md`, `testplan_prompts_test.go`.
- Modify `framework-go-conformance/{chain.go,fixtures_test.go,acceptance_test.go,run021_test.go,testdata/todomvc/project.json,testdata/todomvc/README.md}`; create `framework-go-conformance/mcpapp_test.go`, `testdata/todomvc/impl/webApp/src/todo-mcp-app/routes/App.tsx`; modify `.github/workflows/{platform-checks.yml,release-gate.yml}`.

App:
- `.aiarch/state/project.json`: projectStateAccess `$defs` (`Surface`, `Audience`, `DeliverySurface`; `MissionStatement.deliverySurfaces`; `Component.surface` replacing `uiSurface`/`machineProtocol`); estimationEngine `$defs` (`Surface`; `SystemComponent.surface`; `DerivedActivity.surface`); own data (slot 0 `deliverySurfaces`, slot 5 components, slot 9 activity items; `ruleWaivers` per Task 5.5).
- `server/internal/resourceaccess/projectstate/projectstateaccess.go` (D2, D4, D5, D8, `ruleSlotAttributionPrefixes`), `server/internal/engine/estimation/estimationengine.go:1636-1735`, `server/internal/manager/delivery/deliverymanager.go:4687-4741,8400,8441,11142`, `server/cmd/backfill-attempts/main.go:337,1413,1419` (+`main_test.go`), `server/cmd/aiarch-state-mcp/{state.go,constructverbs.go,validate.go,recordtestplan.go,rig_test.go,ownproject_test.go,run010_test.go,freshcomposition_test.go,testdata/fresh/run0{08,09,10,11,13,16}.project.json,testdata/fresh-systems/README.md}`, `server/internal/method_design_test.go:51`, `server/internal/derivedplan_test.go:19-20`, `server/go.mod`.
- Create `server/internal/surfaces_test.go`, `server/cmd/aiarch-state-mcp/surfaces_test.go`.
- Regenerate `contract.gen.go` (projectstate, estimation), fakes, `toolcatalog.gen.go`, `server/api/openapi.yaml`, `webApp/src/contracts/schema.ts`; re-seat `.claude/` from method-assets (`make claude-assets`).

---

### Task 5.1: `Surface` sum type and `deliverySurfaces` on the mission

**Files:**
- Create: `framework-go-projectmodel/surface.go`, `surface_test.go`; modify `system.go:30-61` and the inline JSON of `system_test.go`, `deployment_test.go`.
- Create: `framework-go/methodcheck/surfaces.go`, `surfaces_test.go`; modify `project.go:171-175,325-363,860-865`, `rules_statevalidation.go:34-45,89-145`, `rules.go:99-100`, `emitters.go:33-37` and every methodcheck fixture the file map names.
- Modify: `framework-go-app-generator/clientgen/clientgen.go:1-15,218-229`, `composegen/surfaces.go:80-108`, `devstack/smoke.go:47-52,93` and the app-generator inline fixtures the file map names.
- Modify: `method-assets/assets/claude/commands/{mission-draft.md,mission-critique.md,system-draft.md:18-19}`, `skills/the-method-business-alignment/SKILL.md` (Output :26-28, Step 5 :96-103, Draft-job doctrine :105-118), `skills/the-method-activity-list/SKILL.md:120,269`, `testplan_prompts_test.go`.
- App (staged): `$defs` and own data per the file map; `projectstateaccess.go`, `estimationengine.go`, `deliverymanager.go`, `backfill-attempts/main.go`, the app test fixtures; create `server/internal/surfaces_test.go`; amend `server/internal/derivedplan_test.go:19-20` (`UiSurface: true` → `Surface: "web"`).

**Interfaces (Produces):**
```go
// framework-go-projectmodel/surface.go — framework-go/methodcheck/surfaces.go mirrors Surface, SurfaceKind,
// the four variants, ParseSurface and Surfaces with the same names.
type Surface string // wire: "web" | "rest" | "mcpTools" | "mcpApp"
const (
	SurfaceWeb      Surface = "web"
	SurfaceREST     Surface = "rest"
	SurfaceMCPTools Surface = "mcpTools"
	SurfaceMCPApp   Surface = "mcpApp"
)
func Surfaces() []Surface // the closed set, in that order
//sumtype:decl
type SurfaceKind interface {
	isSurfaceKind()
	Surface() Surface
}
type WebSurface struct{}      // a browser page on the API's origin; hand-built UI
type RESTSurface struct{}     // the shared generated REST transport
type MCPToolsSurface struct{} // generated MCP tool tables
type MCPAppSurface struct{}   // a hand-built UI an MCP host embeds; its tool tables and ui:// resource are generated
func ParseSurface(s string) (SurfaceKind, error) // unknown or "" → error naming the four values

// projectmodel.SystemComponent: UISurface and MachineProtocol deleted; Surface Surface `json:"surface,omitempty"`
func (c SystemComponent) SurfaceKind() (SurfaceKind, bool) // false: not a Client, unstated, or unknown
func (c SystemComponent) IsBrowserSurface() bool           // web | mcpApp
func (c SystemComponent) IsWebSurface() bool               // web
func (c SystemComponent) ServesMCPTools() bool             // mcpTools | mcpApp (replaces IsMCPClient)
func (s *System) BuiltWebSurfaces() []SystemComponent      // built web Clients (BuiltBrowserSurfaces keeps web | mcpApp)
func (s *System) HasWebSurface() bool

// framework-go/methodcheck/surfaces.go
type Audience string
const (
	AudienceHuman   Audience = "human"
	AudienceMachine Audience = "machine"
)
func AudienceOf(k SurfaceKind) Audience // exhaustive: web, mcpApp → human; rest, mcpTools → machine
type DeliverySurface struct {
	Kind      Surface  `json:"kind"`
	Audience  Audience `json:"audience"`
	Source    string   `json:"source"`    // the brief's words, quoted
	Objective int      `json:"objective"` // an Objective.Number of the same mission
}
// MissionStatement gains DeliverySurfaces []DeliverySurface `json:"deliverySurfaces"`.
// Component: UISurface, MachineProtocol deleted; Surface Surface `json:"surface,omitempty"`;
// isBrowserSurface / isMachineClient / isRESTClient re-expressed per D2 through ParseSurface.
const ruleSysClientSurface RuleID = "SYS-CLIENT-SURFACE"
func clientSurfaceStated(s System) []Finding // D3; replaces clientSurfaceExplicit + clientProtocolStated
```
App `$defs` (projectStateAccess; estimationEngine carries its own `Surface` copy):
```json
"Surface": {"type": "string", "enum": ["web", "rest", "mcpTools", "mcpApp"], "x-enum-varnames": ["SurfaceWeb", "SurfaceREST", "SurfaceMCPTools", "SurfaceMCPApp"], "x-go-base": "string", "description": "How a Client reaches its users (FPC W6)."},
"Audience": {"type": "string", "enum": ["human", "machine"], "x-enum-varnames": ["AudienceHuman", "AudienceMachine"], "x-go-base": "string"},
"DeliverySurface": {"type": "object", "properties": {"kind": {"$ref": "#/$defs/Surface"}, "audience": {"$ref": "#/$defs/Audience"}, "source": {"type": "string"}, "objective": {"type": "integer", "x-go-type": "int"}}, "required": ["kind", "audience", "source", "objective"], "additionalProperties": false}
```
`MissionStatement.properties.deliverySurfaces = {"type": ["null", "array"], "items": {"$ref": "#/$defs/DeliverySurface"}}`, added to `required`. `Component.properties`: delete `uiSurface`, `machineProtocol`; add `"surface": {"$ref": "#/$defs/Surface"}` (optional → `Surface *Surface`). estimationEngine: `SystemComponent.surface` and `DerivedActivity.surface` (`{"type": "string"}`, a Surface value or `""`, required), replacing `uiSurface` and `uiSurface` + `machineClient`. App Go:
```go
// projectstate (hand code)
type ActivityItem struct { /* … */ Surface Surface `json:"surface,omitempty"` } // replaces UISurface, MachineClient
func ClassifyActivity(id, workerClass string, coding bool, surface Surface) (ActivityType, TestingVariant, error)
func ClassifyType(id, workerClass string, coding bool, surface Surface, hasServiceContract bool) (ActivityType, bool)
func missionProblems(m *MissionStatement) []string // replaces missionObjectiveProblems; D5
// estimation: clientAppActivityFor emits U-SPA-<id> for Surface ∈ {web, mcpApp} with Title "Build <Name> (SPA)" / "(MCP App)"
// and carries Surface; codingActivityFor skips {web, mcpApp} and carries Surface for {rest, mcpTools}
```

- [ ] **Step 1: Write the failing tests.**
```go
// framework-go-projectmodel/surface_test.go
func TestSurface_ParseIsClosed(t *testing.T)            // each of Surfaces() parses to its variant and .Surface() round-trips; "", "mcp", "Web", "rest " → error whose text names web, rest, mcpTools, mcpApp
func TestSystemComponent_SurfacePredicates(t *testing.T) // table over {web, rest, mcpTools, mcpApp, unstated, kind manager with surface web}: IsBrowserSurface = {T,F,F,T,F,F}; IsWebSurface = {T,F,F,F,F,F}; ServesMCPTools = {F,F,T,T,F,F}
func TestBuiltBrowserSurfacesKeepsMcpAppAndWebSurfacesDoNot(t *testing.T) // System with web, mcpApp, planned web: BuiltBrowserSurfaces ids == [web, mcpApp]; BuiltWebSurfaces ids == [web]
// framework-go/methodcheck/surfaces_test.go
func TestSurfaces_DecodeMission(t *testing.T)            // a mission slot with deliverySurfaces [{kind: mcpApp, audience: human, source: "plus an MCP Apps surface", objective: 7}] decodes into DeliverySurface{SurfaceMCPApp, AudienceHuman, "plus an MCP Apps surface", 7}
func TestSYS_ClientSurface_Fires(t *testing.T)           // four subtests, each exactly one SYS-CLIENT-SURFACE Error naming the component: a built client with no surface; surface "mcp"; a manager with surface "web"; two rest clients (the second named)
func TestSYS_ClientSurface_PlannedMayOmit(t *testing.T)  // a planned client with no surface → no finding
func TestSurfaceKind_SwitchIsExhaustive(t *testing.T)    // describe(k SurfaceKind) string switches over the four variants with no default (gochecksumtype); the test calls it for each
// method-assets/testplan_prompts_test.go
func TestPrompts_NoRetiredSurfaceFields(t *testing.T)    // no ClaudeFiles() entry mentions "uiSurface", "machineProtocol", "SYS-CLIENT-UISURFACE" or "SYS-CLIENT-PROTOCOL"
// TestPrompts_CiteTheRulesThatEnforceThem gains cmd("system-draft"): {"surface", "SYS-CLIENT-SURFACE", "mcpApp"},
// cmd("mission-draft"): {"deliverySurfaces", "verbatim"}, cmd("mission-critique"): {"deliverySurfaces", "audience"}
// app: server/internal/surfaces_test.go (package internal_test, as derivedplan_test.go)
func TestDerivedPlan_SurfacesSelectActivityType(t *testing.T) // DerivePlan over clients {w: web, a: mcpApp, r: rest, m: mcpTools, p: planned mcpApp} and one manager: U-SPA-w (Surface "web"), U-SPA-a (Surface "mcpApp", Title ends "(MCP App)"), C-r (Surface "rest"), C-m (Surface "mcpTools"), nothing for p; projectstate.ClassifyActivity over each: U-SPA-* → ActivityTypeFrontend, C-r and C-m → ActivityTypeMachineClient
func TestSurfaceSchema_DecodesStrictly(t *testing.T)   // DecodeProjectJSON of the own project with one component's "surface" set to "mcp" → error; with "uiSurface": true added → error naming uiSurface; the own project as committed → ok, web-client has Surface web, mcp-client mcpTools, scheduler-client none
func TestMissionDeliverySurfaces_RecordVerb(t *testing.T) // ValidateModelIdentities(&MissionStatement{…}): no entries; source "  "; objective 99; an objective number held by two objectives; a kind listed twice; {mcpTools, human} → each one problem naming it; a valid two-entry list → nil
```
- [ ] **Step 2: Run** `cd $P/framework-go-projectmodel && GOWORK=off go test ./ -run 'TestSurface_|TestSystemComponent_SurfacePredicates|TestBuiltBrowserSurfaces' -v`; `cd $P/framework-go && GOWORK=off go test ./methodcheck -run 'TestSurfaces_|TestSYS_ClientSurface|TestSurfaceKind_' -v`; `cd $P/method-assets && GOWORK=off go test ./ -run 'TestPrompts_NoRetiredSurfaceFields|TestPrompts_CiteTheRulesThatEnforceThem' -v`; `cd $A/server && GOWORK=$A/go.work go test ./internal/ -run 'TestDerivedPlan_SurfacesSelectActivityType|TestSurfaceSchema_|TestMissionDeliverySurfaces_' -v` — Expected: FAIL (`undefined: ParseSurface`, `DeliverySurface`, `ruleSysClientSurface`; prompts still say `uiSurface`; `estimation.SystemComponent` has no field `Surface`).
- [ ] **Step 3: Implement.**
  - projectmodel and methodcheck per Interfaces; delete `machineProtocolREST/MCP` (`methodcheck/project.go:861-865`); `rules.go:99-100` call `clientSurfaceStated(s)`; `emittedRuleIDs` gains `ruleSysClientSurface`.
  - app-generator: `IsMCPClient()` → `ServesMCPTools()` at `clientgen.go:222` (`if !c.ServesMCPTools() { continue }`), `composegen/surfaces.go:87`, `devstack/smoke.go:93`; `smoke.go:49` SPA → `HasWebSurface()`; `composegen/surfaces.go:96-108` keeps the `webDistDir` refusal for any browser surface and sets `r.serveWeb = declared && len(r.webMgrs) > 0 && m.System.HasWebSurface()`. Doc comments that say `uiSurface`/`machineProtocol` (`clientgen.go:5-10`, `methodcheck/align.go:241`, `project.go`) are rewritten in surface terms.
  - Migrate every platform fixture with the D2 script (`go run ./cmd/surfacemigrate <files>` under `framework-go`, deleted before the commit).
  - method-assets: `system-draft.md:18-19` become: "**Every built Client states its `surface`** — `web` (a page people use in a browser), `mcpApp` (a UI people use inside an MCP host; hand-built like `web`), `rest` (the shared generated HTTP API, at most one) or `mcpTools` (generated MCP tool tables). `putDraftModel` refuses a built Client without one (`SYS-CLIENT-SURFACE`)." followed by the remainder of today's :19 for a `rest`/`mcpTools` client (constructionProfile, `SYS-CLIENT-MACHINE-CALL`). `the-method-activity-list/SKILL.md:120,269`: "`uiSurface: true`" → "`surface` `web` or `mcpApp`". `mission-draft.md` Step 3 gains: "List `deliverySurfaces`: one entry per channel the brief commits the system to — `kind` (`web`, `mcpApp`, `rest`, `mcpTools`), `audience` (`human` for `web`/`mcpApp`, `machine` for `rest`/`mcpTools`), `source` (the brief's words, quoted verbatim) and the `objective` number it serves. A channel the brief does not name is not listed. `putDraftModel` refuses an empty list, a blank source, an objective that is not one of the mission's numbers, a kind listed twice and an audience that disagrees with the kind." `mission-critique.md` step 3 gains: "REVISE if a channel the brief names has no `deliverySurfaces` entry, an entry's `source` is not the brief's own words, or its `audience` is not who the brief says uses it." `the-method-business-alignment/SKILL.md` Output names `DeliverySurfaces []DeliverySurface` (the architect lists, the PM ratifies against the brief's wording; a channel constraint, never a behaviour list).
  - App: `$defs` per Interfaces; `make gen-models gen-fakes gen-internal-tools gen-client`; `cd webApp && npm run gen:api`. `projectstateaccess.go`: `ActivityItem.Surface`; `ClassifyActivity`/`ClassifyType` per D4 (the surface branch replaces rule 3: `switch kind.(type)` over `projectmodel.ParseSurface(string(surface))`, `""` skipping it); `:9631` passes `meta.Surface`; `:12990` `item.MachineClient` → the item's surface parses to `RESTSurface` or `MCPToolsSurface`; `RecordBrowserContract` (`:13590`) accepts `web|mcpApp`; `RecordMachineClientContracts` (`:13775`) accepts `rest|mcpTools`; `machineClientContract` (`:13892,13898`) switches over the kind (`mcpTools` wraps the result in `mcpOutputSchema`, `rest` files under `internal/client/web`, `web`/`mcpApp` return an error naming the component); delete `machineProtocolREST/MCP` (`:13855-13859`); `missionProblems` per D5. Estimation per Interfaces. `deliverymanager.go:4712` passes `derefSurface(c.Surface)` (delete `derefBool` if left unused), `:4738-4739` → `Surface: projectstate.Surface(a.Surface)`, `:8400,8441,11142` → `item.Surface`; `backfill-attempts/main.go:337,1413,1419` likewise.
  - Own `project.json` (GC‑5) and the app fixtures (`cmd/aiarch-state-mcp/testdata/fresh/run0{08,09,10,11,13,16}.project.json`; `rig_test.go:544-609` — `statingClientSurfaces` states `surface` and `TestRig_DraftRefusesAClientWithNoStatedSurface` expects `SYS-CLIENT-SURFACE`; `freshcomposition_test.go`; `backfill-attempts/main_test.go`; `testdata/fresh-systems/README.md`) through the D2 script; own slot 0 gains D12's `deliverySurfaces`; own slot 9 `U-SPA-web-client` gets `surface: "web"`.
- [ ] **Step 4: Run** `GOWORK=off go test ./...` in `framework-go-projectmodel`, `framework-go` (includes `TestDefaultCoverage_EveryAutomatedRuleHasEmitter`), `framework-go-app-generator`, `method-assets`; `golangci-lint run ./...` in each — 0 issues. App (workspace mode): `go test ./internal/ ./internal/resourceaccess/projectstate/... ./internal/engine/estimation/... ./internal/manager/delivery/... ./cmd/backfill-attempts ./cmd/aiarch-state-mcp`, `make gen-models-check gen-internal-tools-check gen-client-check`, `cd webApp && npm run typecheck` — PASS. `grep -rn "uiSurface\|machineProtocol\|UiSurface\|MachineProtocol\|IsMCPClient" $P --include='*.go' --include='*.md' --include='*.tmpl'` and the same over `$A/server $A/webApp/src $A/.aiarch/state/project.json` — no hit (the `framework-go-app-generator/testdata/webgen/archistrator*` OAS snapshots excepted: recorded inputs of an older archistrator, not schema).
- [ ] **Step 5: Commit** (platform, explicit paths) `git commit -m "feat(projectmodel,methodcheck): Surface sum type on Clients and deliverySurfaces on the mission; SYS-CLIENT-SURFACE replaces SYS-CLIENT-UISURFACE and SYS-CLIENT-PROTOCOL"`. App edits stay staged for Task 5.5.

---

### Task 5.2: Design-time rules

**Files:**
- Create: `framework-go/methodcheck/rules_surfaces.go`, `rules_surfaces_test.go`, `testdata/surfaces_clean.json` (a mission with D13's four `deliverySurfaces`; a System with `todo-web-app` web, `todo-mcp-app` mcpApp, `todo-mcp-server` mcpTools, `todo-api` rest and `list-manager`; core use case `add-a-todo` with actors `team-member` (human) and `team-agent` (agent) and a view whose entry step `s1` holds `team-member→todo-web-app`, `team-member→todo-mcp-app`, `team-agent→todo-mcp-server`, `team-agent→todo-api`, all `alt: "s1"`)
- Modify: `framework-go/methodcheck/project.go:294-298` (`System.Waivers`), `validate.go:15-35,196-205`, `check.go:23-96`, `emitters.go`, and every ValidateProject caller D6 lists
- Create: `framework-go-app-generator/appgen/surfaces.go`, `surfaces_test.go`
- Modify: `framework-go-conformance/{fixtures_test.go:30,acceptance_test.go,run021_test.go}` (pass `appgen.SupportedSurfaces()`)
- Modify: `method-assets/assets/claude/commands/{system-draft.md,mission-draft.md}`, `testplan_prompts_test.go`
- App (staged): `server/cmd/aiarch-state-mcp/{state.go:126,constructverbs.go:93,validate.go:127,recordtestplan.go:82,95}` and their tests, `server/internal/method_design_test.go:51`, `projectstateaccess.go:12198-12228` (`{"GEN-", KindSystem, AttribSlot}`); create `server/cmd/aiarch-state-mcp/surfaces_test.go`

| id | statement |
|---|---|
| `SYS-SURFACE-REQUIRED` | every `deliverySurfaces[].kind` has ≥1 Client with that `surface`; a human-audience surface's Client must be `web` or `mcpApp`, never `mcpTools`/`rest` |
| `SYS-SURFACE-ENTRY` | each core use case's first actor step carries one call per required surface that actor uses, as an `alt` group (per-use-case waiver via the existing waiver mechanism) |
| `GEN-SURFACE-SUPPORTED` | every Client `surface` ∈ `appgen.SupportedSurfaces()` (methodcheck receives the list through `ProjectSpec.SupportedSurfaces`; the app passes `appgen.SupportedSurfaces()`) |

All three are `SeverityError`. "Required surface", "uses" and the waiver are D7's.

**Interfaces (Produces):**
```go
// framework-go/methodcheck
type ValidateOptions struct {
	// SupportedSurfaces is what the platform's generators can build (appgen.SupportedSurfaces()).
	// Empty fails closed: GEN-SURFACE-SUPPORTED fires on every Client that states a surface.
	SupportedSurfaces []Surface
}
func ValidateProject(p Project, o ValidateOptions) ([]Finding, error)       // run list gains func(p Project) ([]Finding, error) { return surfaceFindings(p, o) }
func ValidateProjectJSON(raw []byte, o ValidateOptions) ([]Finding, error)
// ProjectSpec gains SupportedSurfaces []Surface; Check passes ValidateOptions{SupportedSurfaces: spec.SupportedSurfaces}.
// System gains Waivers []CheckItem `json:"waivers"`.
const (
	ruleSysSurfaceRequired  RuleID = "SYS-SURFACE-REQUIRED"
	ruleSysSurfaceEntry     RuleID = "SYS-SURFACE-ENTRY"
	ruleGenSurfaceSupported RuleID = "GEN-SURFACE-SUPPORTED"
)
func surfaceFindings(p Project, o ValidateOptions) ([]Finding, error) // a slot not committed skips the rules that read it
func surfaceRequired(m MissionStatement, s System) []Finding
func surfaceEntry(m MissionStatement, s System, c CoreUseCases) []Finding
func surfaceSupported(s System, supported []Surface) []Finding

// framework-go-app-generator/appgen/surfaces.go
// SupportedSurfaces is every Client surface the generators build end to end, each proven by a
// conformance fixture (framework-go-conformance TestSupportedSurfaces_EachHasAConformanceFixture).
func SupportedSurfaces() []methodcheck.Surface // [web, rest, mcpTools] until Task 5.4
```
Messages (the prefixes the tests assert): SYS-SURFACE-REQUIRED `the mission commits to a %s surface (%q) but no Client has surface %s` and `the mission lists %s for a %s audience, but %s serves a %s audience`; SYS-SURFACE-ENTRY `use case %q: %s enters at step %q without a call into a %s Client` and `use case %q: %s's calls into its surfaces at step %q are not one alt group`; GEN-SURFACE-SUPPORTED `Client %q states surface %s, which the platform cannot generate yet (supported: %s)`.

- [ ] **Step 1: Write the failing tests** (`rules_surfaces_test.go`, mutating `surfaces_clean.json` through small helpers `withSurfaces`, `withoutComponent(t, p, id)` (drops its relationships and view calls), `withCall(t, p, uc, step, from, to, alt)`, in the style of `binding(t,p,comp,scn)`):
```go
func TestSYS_SurfaceRequired_Passes(t *testing.T)              // surfaces_clean: ValidateProject(p, ValidateOptions{SupportedSurfaces: Surfaces()}) has no SYS-SURFACE-* or GEN-* finding
func TestSYS_SurfaceRequired_FiresOnMissingRest(t *testing.T)  // todo-api removed: exactly one SYS-SURFACE-REQUIRED Error whose message contains "rest" and the rest entry's source
func TestSYS_SurfaceRequired_KindMustMatch(t *testing.T)       // RUN-021: todo-mcp-app removed, the mcpApp entry kept → one SYS-SURFACE-REQUIRED Error naming mcpApp although todo-mcp-server (mcpTools) remains; an entry {kind: mcpTools, audience: human} → one Error containing "human audience"
func TestSYS_SurfaceEntry_FiresWithoutAlt(t *testing.T)        // team-member→todo-mcp-app removed from s1 → one SYS-SURFACE-ENTRY Error naming "add-a-todo", "team-member", "mcpApp"; the call kept with its alt cleared → one Error containing "not one alt group"
func TestSYS_SurfaceEntry_WaivedPerUseCase(t *testing.T)       // the first case plus System.waivers [{section: "add-a-todo", guideline: "SYS-SURFACE-ENTRY", status: "waived", justification: "x"}] → no finding; the same waiver with an empty justification → the finding stays
func TestSYS_SurfaceEntry_OnlyRequiredSurfaces(t *testing.T)   // deliverySurfaces without mcpTools: removing team-agent→todo-mcp-server yields no finding
func TestGEN_SurfaceSupported_RefusesUnsupported(t *testing.T) // SupportedSurfaces [web, rest, mcpTools] → one GEN-SURFACE-SUPPORTED Error naming "todo-mcp-app" and "mcpApp"; Surfaces() → none; nil → one per Client that states a surface (fail closed)
// framework-go-app-generator/appgen/surfaces_test.go
func TestSupportedSurfaces_ExcludesMcpAppUntilGenerated(t *testing.T) // == [web, rest, mcpTools]
// app: server/cmd/aiarch-state-mcp/surfaces_test.go
func TestPutDraftModel_RefusesSystemMissingRequiredSurface(t *testing.T) // seedProject(t, p, jobModeDraft, projectstate.KindSystem) over minimalProject() plus a committed mission whose deliverySurfaces is [{web, human, "an ordinary web app", 1}, {rest, machine, "reachable both as an ordinary web API", 1}]; putDraftModel of staleness_test.go:276's draft with c1 given "surface": "web" → error containing "SYS-SURFACE-REQUIRED" and "rest"; the same draft plus {"id":"api","name":"TodoApi","kind":"client","layer":"client","surface":"rest","encapsulates":"…"} and api→m1 → accepted
func TestAttributeRule_SurfaceRulesBelongToTheSystem(t *testing.T)     // projectstate.AttributeRule("GEN-SURFACE-SUPPORTED") and ("SYS-SURFACE-ENTRY") == (KindSystem, AttribSlot)
```
`TestPrompts_CiteTheRulesThatEnforceThem` gains `cmd("system-draft"): {"SYS-SURFACE-REQUIRED", "SYS-SURFACE-ENTRY", "GEN-SURFACE-SUPPORTED"}` and `cmd("mission-draft"): {"SYS-SURFACE-REQUIRED"}`.
- [ ] **Step 2: Run** `cd $P/framework-go && GOWORK=off go test ./methodcheck -run 'TestSYS_Surface|TestGEN_Surface' -v`; `cd $P/framework-go-app-generator && GOWORK=off go test ./appgen -v`; `cd $P/method-assets && GOWORK=off go test ./ -run TestPrompts_CiteTheRulesThatEnforceThem`; `cd $A/server && GOWORK=$A/go.work go test ./cmd/aiarch-state-mcp -run 'TestPutDraftModel_RefusesSystemMissingRequiredSurface|TestAttributeRule_SurfaceRules' -v` — Expected: FAIL (`undefined: ValidateOptions`, package `appgen` missing; no SYS-SURFACE-REQUIRED finding; `GEN-` unattributed).
- [ ] **Step 3: Implement** `rules_surfaces.go` (each rule a pure function; `surfaceEntry` builds Uses(A) from every view's calls into Clients whose `Surface` parses, then walks each view's steps for each actor's first call into a Client); thread `ValidateOptions` through `ValidateProject`/`ValidateProjectJSON`/`Check`; register the three ids in `emittedRuleIDs`; update every caller D6 lists (platform tests pass `ValidateOptions{SupportedSurfaces: Surfaces()}`; conformance passes `appgen.SupportedSurfaces()`). App: one helper in `cmd/aiarch-state-mcp/validate.go`, `func methodcheckOptions() methodcheck.ValidateOptions { return methodcheck.ValidateOptions{SupportedSurfaces: appgen.SupportedSurfaces()} }`, used at the five call sites; `method_design_test.go:51` sets `SupportedSurfaces: appgen.SupportedSurfaces()`; `ruleSlotAttributionPrefixes` gains `{"GEN-", KindSystem, AttribSlot}`. Prompts: `system-draft.md` adds "the mission's `deliverySurfaces` decide which Clients the System must have: each listed kind needs a Client with that `surface` (`SYS-SURFACE-REQUIRED`); in every use case an actor enters through a call into each required surface they use, drawn as one `alt` group at their first step (`SYS-SURFACE-ENTRY`); a surface the platform cannot generate yet is refused (`GEN-SURFACE-SUPPORTED`)"; `mission-draft.md` adds "the System will be refused unless it has a Client for each entry (`SYS-SURFACE-REQUIRED`)".
- [ ] **Step 4: Run** `GOWORK=off go test ./...` in `framework-go`, `framework-go-app-generator`, `method-assets`, and `cd $P/framework-go-conformance && GOWORK=off go test ./ -run TestFixtures_LoadUnderProjectmodelAndMethodcheck` (the fixture lists no `deliverySurfaces` yet, so the surface rules are silent on it) — PASS; `golangci-lint run ./...` in each — 0. App (workspace mode) `go test ./internal/ ./cmd/aiarch-state-mcp` — PASS, except that `TestMethodDesignArtifacts` and `TestAdmit_PassesOwnProject` may now report the own project's new surface findings: record their ids and count (the waivers land in Task 5.5, Step 5).
- [ ] **Step 5: Commit** (platform) `git commit -m "feat(methodcheck): SYS-SURFACE-REQUIRED, SYS-SURFACE-ENTRY, GEN-SURFACE-SUPPORTED over the mission's delivery surfaces; appgen.SupportedSurfaces"`.

---

### Task 5.3: `mcpApp` generation — server side

**Files:**
- Modify: `framework-go/scenario/clientops.go` (`ManagerOpsThrough`, hoisted from the app's `machineClientManagerOps`), `clientops_test.go`
- Modify: `framework-go-app-generator/clientgen/clientgen.go` (`Project.MCPApps`, `AppName`, `generateManager`, `mcpTools` passing `Options.UI`), `clientgen/mcpemit/mcpemit.go` (`Options.UI`, `emitHandlerAndRegister` :267-312), `clientgen/clientgen_test.go`
- Create: `framework-go-app-generator/clientgen/mcpemit/emit_resources.go`, `emit_resources_test.go`
- Create: `framework-go-app-generator/composegen/mcpapps.go`, `mcpapps_test.go`, `mcpapps_http_test.go`; modify `composegen/composegen.go` (`Config.App`, the `MCPAppsFile` output), `surfaces.go:111-145` (`writeRootMounts`)
- Modify: `framework-go-app-generator/cmd/appgen/main.go:373-395` (`composeConfig`), `:410-429` (`genClients`)
- App (staged): `projectstateaccess.go:13829-13852` deleted; `recordMachineClient` (`:13786`) calls `scenario.ManagerOpsThrough`

**Interfaces (Produces):**
```go
// framework-go/scenario/clientops.go
// ManagerOpsThrough is what the views reach through client, by Manager contract key: the client's calls
// into a Manager in every view someone calls the client in (the whole view, because the actor's request
// and the client's call into the Manager are drawn in one step as often as in two).
func ManagerOpsThrough(in Input, client string) map[string][]ManagerOp

// clientgen
type MCPApp struct {
	Component string              // the mcpApp Client's System id: webApp/src/<id>, ui://<app>/<id>/shell.html
	Ops       map[string][]string // Manager contract key → the operations the views reach through the Client
}
type Project struct {
	Contracts map[string]json.RawMessage
	System    *projectmodel.System
	MCPApps   []MCPApp
}
func AppName(cfg Config) string // cfg.App, else moduleLeaf(cfg.ModulePath)

// mcpemit
// Options gains UI map[string]string: op name → its derived ui:// resource URI.
const ResourceMIMEType = "text/html;profile=mcp-app"
func ResourceURI(app, surface string) string // "ui://" + app + "/" + surface + "/shell.html"
func ResourceCSP() map[string]any            // {"connectDomains": []string{}, "resourceDomains": []string{}}
type ResourceOptions struct {
	Package  string   // the composition root's package ("main")
	App      string
	Surfaces []string // the built mcpApp Client ids, sorted
}
// GenerateResources renders mcpapps.gen.go: func registerMCPAppResources(srv *mcp.Server, webDistDir string), one
// srv.AddResource per surface (URI ResourceURI(App, s), Name s, MIMEType ResourceMIMEType, Meta {"ui": {"csp": ResourceCSP()}})
// whose handler reads filepath.Join(webDistDir, s, "shell.html") at read time and answers one ResourceContents{URI,
// MIMEType, Text}; an empty webDistDir or a missing file answers mcp.ResourceNotFoundError(uri). gofmt'd, deterministic.
func GenerateResources(o ResourceOptions) ([]byte, error)

// composegen
// Config gains App string: the MCP Apps resource namespace (appgen passes clientgen.AppName).
const MCPAppsFile = "mcpapps.gen.go"
func (r *resolved) resolveMCPApps(m *projectmodel.Model) error // built mcpApp Clients; an error when none of their Managers is mounted on /mcp
```
The generated registration of a derived op (the exact form `TestMCPApp_OpUIDerivedFromModel` asserts):
```go
mcp.AddTool(srv, &mcp.Tool{Name: "listGreet", Description: "…", InputSchema: greetInputSchema(), OutputSchema: greetOutputSchema(), Meta: mcp.Meta{"ui": map[string]any{"resourceUri": "ui://<app>/todo-mcp-app/shell.html"}}}, h.handleGreet)
```
- [ ] **Step 1: Write the failing tests.**
```go
// framework-go/scenario/clientops_test.go
func TestManagerOpsThrough_WholeView(t *testing.T)          // a view whose step 1 is actor→client and step 2 client→manager "AddTodo": {"listManager": [{listManager ListManager AddTodo}]}; a view the client is not called in contributes nothing
// clientgen/clientgen_test.go — freshSurfacesProject's System plus {"id":"todo-mcp-app","name":"TodoMcpApp","kind":"client","surface":"mcpApp"} → list-manager
func TestMCPApp_OpUIDerivedFromModel(t *testing.T)          // MCPApps [{todo-mcp-app, {listManager: [Greet]}}]: every MCP-served client's table carries `"resourceUri": "ui://<leaf>/todo-mcp-app/shell.html"` on the listGreet tool and on no other; without MCPApps no table contains "resourceUri"
func TestMCPApp_OpReachedTwiceIsAnError(t *testing.T)       // two MCPApps reaching Greet → error naming both; one MCPApp reaching an op whose contract carries ui.view → error containing "ui annotation"
// mcpemit/emit_resources_test.go
func TestGenerateResources_OnePerSurface(t *testing.T)      // Surfaces [a, b]: the source contains ResourceURI(app, "a"), ResourceURI(app, "b"), `"text/html;profile=mcp-app"`, `"csp"`, `filepath.Join(webDistDir, "a", "shell.html")`, and parses
// composegen/mcpapps_test.go
func TestMCPApp_NoResourceWithoutMcpAppClient(t *testing.T) // freshSurfacesProject: Generate emits no MCPAppsFile and main.gen.go does not mention registerMCPAppResources
func TestMCPApp_ComposeRegistersResources(t *testing.T)     // with todo-mcp-app: MCPAppsFile present; main.gen.go calls `registerMCPAppResources(mcpServer, cfg.WebDistDir)` after the tool tables are registered and before root.Handle("/mcp", …); an mcpApp that fronts no mounted Manager → error containing "fronts no Manager"
// composegen/mcpapps_http_test.go — builds and boots the sandbox as surfaces_http_test.go:29-70 does
func TestMCPApp_ResourceMime(t *testing.T)                  // dist/todo-mcp-app/shell.html = "<!doctype html><div id=\"root\"></div>"; over /mcp: resources/list holds ui://…/todo-mcp-app/shell.html with _meta.ui.csp; resources/read returns exactly one content with mimeType "text/html;profile=mcp-app" and that text; with the file removed → resources/read is an error
```
- [ ] **Step 2: Run** `cd $P/framework-go && GOWORK=off go test ./scenario -run TestManagerOpsThrough -v`; `cd $P/framework-go-app-generator && GOWORK=off go test ./clientgen/... ./composegen -run 'TestMCPApp_|TestGenerateResources_' -v` — Expected: FAIL (`undefined: ManagerOpsThrough`, `MCPApps`, `GenerateResources`, `MCPAppsFile`).
- [ ] **Step 3: Implement.** `ManagerOpsThrough` is the body of the app's `machineClientManagerOps` verbatim. clientgen: `generateManager` builds, for contract key `key`, `ui := map[string]string{}` from `p.MCPApps` (`mcpemit.ResourceURI(AppName(cfg), app.Component)` for each op of `app.Ops[key]`; a second writer of one op is the error) and passes it as `mcpemit.Options.UI`; `emitHandlerAndRegister` emits the derived `Meta` for `opts.UI[op.Name]`, keeps the `op.UI` (annotation) arm, refuses both on one op, and emits `shellResourceURI` only for an annotated op. appgen `genClients`: `p, _, err := methodcheck.DecodeProject(raw)`; `in, err := methodcheck.ScenarioInput(p)`; for each built Client of `m.System` whose `SurfaceKind()` is `projectmodel.MCPAppSurface`, `clientgen.MCPApp{Component: c.ID, Ops: the op names of scenario.ManagerOpsThrough(in, c.ID) for keys in m.Contracts}`; `composeConfig` sets `App: clientgen.AppName(clientgen.Config{ModulePath: module, App: cfg.Clients.App})`. composegen: `resolveMCPApps` after `resolveMCP`; `Generate` writes `MCPAppsFile` from `mcpemit.GenerateResources(mcpemit.ResourceOptions{Package: cfg.PackageName, App: cfg.App, Surfaces: ids})`; `writeRootMounts` calls it with the `webDistDir` setting's field (`settingFieldName(projectmodel.WebDistDirSetting)`). App: delete `machineClientManagerOps`; `recordMachineClient` calls `scenario.ManagerOpsThrough(in, c.ID)`.
- [ ] **Step 4: Run** `GOWORK=off go test ./...` in `framework-go` and `framework-go-app-generator` (the sandbox tests build binaries; `-timeout 30m`) — PASS; `golangci-lint run ./...` in both — 0. App (workspace mode) `go test ./internal/ ./cmd/aiarch-state-mcp -run 'Machine|FreshComposition|Rig_'` — PASS.
- [ ] **Step 5: Commit** (platform) `git commit -m "feat(clientgen,composegen): MCP Apps server half generated from the model — derived _meta.ui.resourceUri, ui:// resources served from the web dist with the MCP Apps MIME type and a fixed CSP"`.

---

### Task 5.4: `mcpApp` generation — web side

**Files:**
- Modify: `framework-go-app-generator/webgen/bootstrap.go` (`Bootstrap(app, surface, kind)`, `bootstrapFiles`), `templates/bootstrap/{vite.config.ts.tmpl,package.json.tmpl}`; create `templates/bootstrap/{mcpapp.html.tmpl,mcpapp.main.tsx.tmpl,build-mcpapps.mjs.tmpl}`; modify `webgen/webgen_test.go:560-637`, `cmd/webgen/main.go:130-145`
- Modify: `framework-go/methodcheck/project.go` (`Project.ClientSurface`), `surfaces_test.go`
- Modify: `framework-go-app-generator/testgen/{testgen.go (componentPlan.Surface; planOf),emit_playwright.go (specWriter root; navigate; url refusal),emit_elements_ts.go:31-60,emit_surface_ts.go:19-46,emit_acceptance.go (mcpApp target refusal),tsruntime/runtime.ts}`; create `testgen/emit_mcpapp_test.go`
- Create: `framework-go-scenariohost/mcpapphost.go`, `mcpapphost_test.go`, `mcpapphost/{package.json,package-lock.json,build.mjs,index.html,src/host.ts,dist/host.js,.gitignore}`; modify `cmd/webscenarios/main.go:223-281` (+`main_test.go`)
- Modify: `framework-go-app-generator/appgen/surfaces.go` (+`mcpApp`), `surfaces_test.go`
- Modify: `framework-go-conformance/chain.go:160-168` (`-kind`)

**Interfaces (Produces):**
```go
// webgen
// web: today's surface files; mcpApp: <surface>.mcpapp.html, src/<surface>/main.tsx (the host-bridge seat),
// src/<surface>/routes/App.tsx; rest | mcpTools: error "webgen: bootstrap: %s is a %s surface, which has no web project".
// Shared seats, byte-identical for every surface and kind: vite.config.ts (pages = root *.html minus *.mcpapp.html),
// scripts/build-mcpapps.mjs, package.json ("build": "tsc -b && vite build && node scripts/build-mcpapps.mjs").
func Bootstrap(app, surface string, kind projectmodel.Surface) ([]File, error)
// cmd/webgen: bootstrap -surface <id> -kind web|mcpApp (required)

// methodcheck
func (p Project) ClientSurface(contractKey string) (componentID string, s Surface, ok bool) // a facet resolves to its parent component

// testgen: componentPlan gains Surface methodcheck.Surface (set by planOf from p.ClientSurface)

// scenariohost
// AppHost is the MCP Apps harness host an mcpApp surface's generated specs run in: a page that connects
// to the launched app's /mcp (proxied on the host's own origin), opens the surface's ui:// resource in a
// sandboxed iframe and bridges it, as a real MCP host does.
type AppHost struct {
	URL   string       // the host page's origin: Playwright's baseURL
	Close func() error // stops the host's listener; safe to call twice
}
func ServeMCPAppHost(ctx context.Context, app *App, surface string) (*AppHost, error)

// appgen
func SupportedSurfaces() []methodcheck.Surface // [web, rest, mcpTools, mcpApp]
```
The generated host-bridge seat (`mcpapp.main.tsx.tmpl`; the lines `TestWebgen_McpAppScaffold` asserts):
```tsx
import { App as McpApp } from '@modelcontextprotocol/ext-apps';
import { OpsClientProvider } from '../api/opsContext';
import { mcpOpsClient } from '../api/ops.gen';
import { App } from './routes/App';

const host = new McpApp({ name: '@@APP@@-@@SURFACE@@', version: '0.0.0' });
await host.connect();
// rootElement check as in main.tsx.tmpl, then:
// createRoot(rootElement).render(<StrictMode><OpsClientProvider value={{ ops: mcpOpsClient(host), transport: 'mcp' }}><App /></OpsClientProvider></StrictMode>);
```
The generated runtime additions (`tsruntime/runtime.ts`):
```ts
export const APP_FRAME = 'iframe#mcp-app';
export function appFrame(page: Page): FrameLocator; // page.frameLocator(APP_FRAME)
export async function openApp(page: Page, route: string): Promise<void>; // page.goto('/?route=' + encodeURIComponent(route)); await expect(appFrame(page).locator('#root')).toBeAttached()
```
`elements.gen.ts`: `export function locate(root: Page | FrameLocator, id: ElementId, nth?: number): Locator` (its `scope` becomes `Page | FrameLocator | Locator`). An mcpApp spec spells every target `locate(appFrame(page), '<id>')` and a `navigate` as `await openApp(page, '<route>')`; its `surface.spec.gen.ts` opens each screen the same way.

- [ ] **Step 1: Write the failing tests.**
```go
// webgen/webgen_test.go
func TestWebgen_McpAppScaffold(t *testing.T)                // Bootstrap("todomvc", "todo-mcp-app", projectmodel.SurfaceMCPApp) writes todo-mcp-app.mcpapp.html (script src "/src/todo-mcp-app/main.tsx"), src/todo-mcp-app/main.tsx containing "new McpApp(", "await host.connect()", "mcpOpsClient(host)", "transport: 'mcp'", and src/todo-mcp-app/routes/App.tsx; no todo-mcp-app.html; vite.config.ts contains ".mcpapp.html"; scripts/build-mcpapps.mjs contains "shell.html"; package.json's build contains "node scripts/build-mcpapps.mjs"
func TestWebgen_BootstrapRefusesMachineSurface(t *testing.T) // kind rest and mcpTools → error containing "has no web project"
// TestBootstrapSharedSeatsAreSurfaceNeutral (webgen_test.go:593) bootstraps todo-mcp-app as SurfaceMCPApp and todo-web-app as SurfaceWeb; the shared seats stay byte-identical
// framework-go/methodcheck/surfaces_test.go
func TestClientSurface_ResolvesFacetsToTheirComponent(t *testing.T) // surfaces_clean: "todoMcpApp" → ("todo-mcp-app", SurfaceMCPApp, true); an unknown key → ok false
// testgen/emit_mcpapp_test.go — testdata/project_tp.json's web plan with its component's surface set to mcpApp
func TestPlaywright_McpAppRunsInHost(t *testing.T)          // every scenario spec imports appFrame and openApp from './runtime.gen'; a navigate step is `await openApp(page, '/')`; every locate is `locate(appFrame(page), '…')`; elements.gen.ts declares `locate(root: Page | FrameLocator`; surface.spec.gen.ts opens screens with openApp; the same plan as web emits none of these
func TestPlaywright_McpAppRefusesURLObservable(t *testing.T) // a binding observing {"kind":"url"} on the mcpApp plan → error containing "has no address bar"
func TestAcceptance_McpAppTargetRefusedUntilHosted(t *testing.T) // an acceptance variant whose target client is mcpApp: skipped → test.skip emitted; unskipped → error containing "E28"
// TestRuntime_TypeChecks (emit_acceptance_test.go:19) stays green over the extended runtime.ts
// framework-go-scenariohost/mcpapphost_test.go
func TestMCPAppHost_ServesPageAndBundle(t *testing.T)       // stub app (httptest) as the App's BaseURL: GET host.URL+"/" → 200 text/html containing `<script src="/host.js">` and `data-surface="todo-mcp-app"`; GET /host.js → the embedded bundle
func TestMCPAppHost_ProxiesMCPToTheApp(t *testing.T)        // POST host.URL+"/mcp" with a JSON-RPC body reaches the stub's /mcp with the body and Mcp-Session-Id intact, and the stub's response comes back
func TestMCPAppHost_BundleMatchesSource(t *testing.T)       // dist/host.js's first line "// source-sha256: <hex>" equals sha256(src/host.ts ‖ index.html ‖ package-lock.json); needs no node
// framework-go-scenariohost/cmd/webscenarios/main_test.go
func TestWebscenarios_McpAppConfigServesTheHost(t *testing.T) // startWith with a fake launch and a fake host server: config generated/todoMcpApp/playwright.config.gen.ts over a module whose project makes todoMcpApp an mcpApp → the stack's BaseURL is the host's and Stop closes the host, then the app; a web surface's config → no host
// appgen/surfaces_test.go: TestSupportedSurfaces_ExcludesMcpAppUntilGenerated becomes TestSupportedSurfaces_IncludesMcpApp (== [web, rest, mcpTools, mcpApp])
```
- [ ] **Step 2: Run** `cd $P/framework-go-app-generator && GOWORK=off go test ./webgen ./testgen ./appgen -run 'TestWebgen_|TestBootstrap|TestPlaywright_McpApp|TestAcceptance_McpApp|TestRuntime_TypeChecks|TestSupportedSurfaces_' -v`; `cd $P/framework-go && GOWORK=off go test ./methodcheck -run TestClientSurface_`; `cd $P/framework-go-scenariohost && GOWORK=off go test ./ ./cmd/webscenarios -run 'TestMCPAppHost_|TestWebscenarios_McpApp' -v` — Expected: FAIL (`Bootstrap` takes two arguments; `appFrame` absent; `ServeMCPAppHost` undefined).
- [ ] **Step 3: Implement.**
  - webgen templates per Interfaces. `build-mcpapps.mjs` (surface-neutral): for each `*.mcpapp.html` beside it, `await build({ configFile: false, plugins: [react(), inlineAll()], build: { outDir: 'dist/' + surface, emptyOutDir: true, assetsInlineLimit: Infinity, cssCodeSplit: false, rollupOptions: { input: entry, output: { inlineDynamicImports: true } } } })` and rename the emitted html to `shell.html`; `inlineAll` replaces each `<script type="module" … src>` and `<link rel="stylesheet" … href>` of the bundle with the chunk's code in `generateBundle` and deletes the chunk, so `dist/<surface>/shell.html` references no file. `cmd/webgen` parses `-kind` with `projectmodel.ParseSurface`.
  - testgen: `planOf` sets `plan.Surface`; `specWriter` gains `root string` (`"page"` or `"appFrame(page)"`) used by `byElement`; `playwrightActionLine` takes the plan's kind and spells `UINavigate` as `openApp(page, …)` for an mcpApp (the switch stays exhaustive); `observable` refuses `UIURL` for an mcpApp; `elementsTS`/`surfaceSpecTS` take the kind; `emit_acceptance.go` refuses an unskipped variant whose target's `ClientSurface` is mcpApp: `testgen: acceptance variant %q runs through the MCP App %s, which no acceptance host drives yet (earmark E28): record it skipped`.
  - scenariohost: `mcpapphost/src/host.ts` per D11 (reads `data-surface` from its page and `route` from its query; picks the first `tools/list` entry whose `_meta.ui.resourceUri` contains `/<surface>/`; calls it; `resources/read`s the URI; prepends `<script>location.hash = <route as JSON>;</script>` to the resource's `<head>` and sets the result as the iframe's `srcdoc`; `new AppBridge(client, {name: 'aiarch-scenario-host', version: '0'}, {serverTools: {}, openLinks: {}})`, `await bridge.connect(new PostMessageTransport(iframe.contentWindow, iframe.contentWindow))`, then sends the tool's input and result to the app). `build.mjs` runs esbuild (`bundle: true, format: 'iife', platform: 'browser'`) and writes the hash line. `mcpapphost.go` embeds `mcpapphost/index.html` and `mcpapphost/dist/host.js`, serves `/` (index.html with `data-surface` filled), `/host.js`, and `/mcp` through `httputil.NewSingleHostReverseProxy(app.BaseURL)` on a `127.0.0.1:0` listener stopped by `Close` and by `ctx`. `cmd/webscenarios`: `launched` gains `appHost *scenariohost.AppHost`; `startWith(ctx, o, launch, serveHost)` resolves `methodcheck.DecodeProject` + `ClientSurface(<config's directory name>)` and, for an mcpApp, serves the host after the launch; `BaseURL()` returns the host's URL when there is one; `Stop` closes the host first.
  - `appgen.SupportedSurfaces()` gains `SurfaceMCPApp`. conformance `chain.go:162-163` appends `"-kind", string(s.Surface)`.
- [ ] **Step 4: Run** `GOWORK=off go test ./...` in `framework-go-app-generator`, `framework-go`, `framework-go-scenariohost` — PASS; `cd $P/framework-go-scenariohost/mcpapphost && npm ci && node build.mjs && git diff --exit-code dist/host.js` (the committed bundle is what its sources build); `golangci-lint run ./...` in the three modules — 0.
- [ ] **Step 5: Commit** (platform) `git commit -m "feat(webgen,testgen,scenariohost): MCP Apps web half — host-bridge seat and single-file shell build, specs that run inside a harness MCP host; appgen supports mcpApp"`.

---

### Task 5.5: Release, pin, proof, own project
- Conformance: todomvc fixture gains `deliverySurfaces` (all four, with brief quotes) and a `todo-mcp-app` Client; chain + boot + acceptance green; bench's `frontend-mcpapps-surface` probe (initialize → tool with `_meta.ui.resourceUri` → `resources/read` mime) reproduced as `TestConformance_McpAppSurface`.
- Own project: mission `deliverySurfaces` (web, rest, mcpTools, mcpApp — archistrator already ships `ui://` in `cmd/server/mcp_apps.go`); migrate that hand-written shell to the generated path or waive `GEN-SURFACE-SUPPORTED` per ruling 9 with `expires`. **Detail (D12):** the own brief quotes web and mcpTools only, so the own list is those two (written in Task 5.1); the shell stays (E29); the ruling-9 fallback is Step 6.

**Files (platform):**
- Modify: `framework-go-conformance/testdata/todomvc/project.json` (D5, D13; below), `testdata/todomvc/README.md` (§"Delivery surfaces and the MCP App (FPC plan 5)")
- Create: `framework-go-conformance/testdata/todomvc/impl/webApp/src/todo-mcp-app/routes/App.tsx` (`export { App } from '../../todo-web-app/routes/App';`)
- Create: `framework-go-conformance/mcpapp_test.go` (tag `conformance`); modify `fixtures_test.go` (`TestSupportedSurfaces_EachHasAConformanceFixture`, untagged)
- Modify: `.github/workflows/platform-checks.yml` (job `conformance-mcpapp`, modelled on `conformance-proof` :115-145, running `GOWORK=off go test -tags conformance ./ -run 'TestConformance_McpAppSurface$' -v -timeout 60m`), `.github/workflows/release-gate.yml:40-49` (requires `conformance-mcpapp` beside the four)

Fixture edits (`testdata/todomvc/project.json`):
- Slot 0: objectives renumbered 1..7 in order; `deliverySurfaces`, quoting `archistrator-bench/benchmarks/todomvc/research/00-founder-brief.txt:27-30`: `{rest, machine, "reachable both as an ordinary web API", 6}`, `{mcpTools, machine, "as an MCP surface, so our other agent tooling can drive it too", 6}`, `{web, human, "a real frontend on top (an ordinary web app", 7}`, `{mcpApp, human, "an MCP Apps surface so it can be embedded the same way our other MCP-hosted UIs are", 7}`.
- Slot 5: component `{"id": "todo-mcp-app", "name": "TodoMcpApp", "kind": "client", "layer": "client", "surface": "mcpApp", "contractKey": "todoMcpApp", "encapsulates": "Encapsulates Presentation-host volatility. MCP Apps surface for team members inside their MCP host; routes list actions to ListManager through the MCP tools.", "encapsulatesVolatilities": ["Presentation-host volatility"], "atomicBusinessVerbs": null}`; relationship `todo-mcp-app → list-manager` (sync); in every view where `team-member` enters through `todo-web-app`, the entry step gains `team-member → todo-mcp-app` with the web call's label and alt, and the step holding `todo-web-app → list-manager` gains `todo-mcp-app → list-manager` with the same label and alt. Then run `ValidateProject(p, ValidateOptions{SupportedSurfaces: appgen.SupportedSurfaces()})`: for each remaining SYS-SURFACE-ENTRY finding add the missing alt call; where a path is a gesture only the browser editor has (`reword-a-todo`, whose entry step holds two un-alted `todo-web-app` calls, Plan 2 Task 2.13), record the per-use-case System waiver with that justification instead; list each waiver in the README. The fixture ends with zero Error findings.
- `serviceContracts.todoMcpApp`: `todoWebApp`'s contract with `component: "todoMcpApp"` and its title and interface name changed (the app's `RecordBrowserContract` derives the same operation names from the same labels; `TestConformanceFixture_RecordsThroughVerbs` pins it).
- `phaseArtifacts.uiDesign["todo-mcp-app"]`: `todo-web-app`'s design with `surface: "todo-mcp-app"`; `phaseArtifacts.testPlan.todoMcpApp`: `todoWebApp`'s principals and the bindings of the scenarios `ForComponentAs(all, "todoMcpApp", true)` projects (the web plan's ids).
- `phaseArtifacts.acceptancePlan`: one binding per new residual variant `<id>@todoMcpApp`, `skip: {reason: "earmark E28: acceptance through an MCP Apps host — the acceptance emitter drives the SPA page, not the harness host", until: "2026-12-31"}`.
- Slots 9/10: `U-SPA-todo-mcp-app` (`surface: "mcpApp"`, as `DerivePlan` emits it), its dependencies as the derivation emits them (`U-SPA-todo-web-app`'s), and `M-ACCEPT` depending on it; the existing items migrated per D4 in Task 5.1.

**Files (app):** `server/go.mod` pins → framework-go v0.25.0, projectmodel v0.6.0, app-generator v0.27.0, scenariohost v0.8.0, method-assets v0.27.0, conformance v0.4.0; everything Tasks 5.1–5.3 staged; `.aiarch/state/project.json` `ruleWaivers` (Steps 5–6); `.claude/` re-seated.

**Proof (platform, `framework-go-conformance/mcpapp_test.go`, tag `conformance`):**
```go
// TestConformance_McpAppSurface is the bench's frontend-mcpapps-surface probe
// (archistrator-bench benchmarks/todomvc/acceptance/harness.ts:63-66) over the built fixture, then its scenarios in the host.
func TestConformance_McpAppSurface(t *testing.T) // launchBuilt (devstack_test.go:58) + LaunchDevstack; over app.MCPURL with devstack.RPC: initialize → tools/list: every tool of an op todo-mcp-app reaches (ManagerOpsThrough over the fixture) has _meta.ui.resourceUri == "ui://todomvc/todo-mcp-app/shell.html" and no other tool has one; resources/list holds that URI; resources/read returns one content with mimeType "text/html;profile=mcp-app" whose text contains `id="root"` and no `<script src=`; subtest "scenarios": `go tool webscenarios -C uitests -config generated/todoMcpApp/playwright.config.gen.ts` exits 0 with a results.json verdict per unskipped todoMcpApp binding (if the web surface's TP-BROKEN pins apply here too, pin the same list and record it in the README); DockerLeftovers empty
// fixtures_test.go (untagged)
func TestSupportedSurfaces_EachHasAConformanceFixture(t *testing.T) // for each s in appgen.SupportedSurfaces(): some embedded fixture has a built Client with surface s
```
The app's half: `TestConformanceFixture_RecordsThroughVerbs` records `todoMcpApp`'s UI design through the verb, and the contract that comes out equals the fixture's.

- [ ] **Step 1: Fixture and proof (platform).** Edit the fixture and impl as above; write the two tests. Run `cd $P/framework-go-conformance && GOWORK=off go test ./ -run 'TestFixtures_LoadUnderProjectmodelAndMethodcheck|TestSupportedSurfaces_'` — PASS; `GOWORK=off go test -tags conformance ./ -run 'TestConformance_McpAppSurface$' -v -timeout 60m` — PASS; then `GOWORK=off go test -tags conformance ./ -run 'TestConformance_(Chain|Boot|ClientsOverWire|Discriminate_Web)$|TestConformance_Acceptance$' -v -timeout 115m` — still PASS (re-pin `TestConformance_Discriminate_Web`'s broken list if the new surface adds entries; record the count in the README). Commit `git commit -m "test(conformance): todomvc lists its four delivery surfaces and gains the todo-mcp-app Client; TestConformance_McpAppSurface reproduces the bench's MCP Apps probe; CI job conformance-mcpapp"`.
- [ ] **Step 2: Release (literal release step), in dependency waves.** Each wave: tag locally at the current fpc/main commit, then `git push origin fpc/main --tags`; if the push is refused, record exactly that command under blockedActions and stop until the lead has pushed. Never let anything query the proxy for a version before its tag is pushed; never move a pushed tag.
  1. `framework-go-projectmodel/v0.6.0`, `framework-go/v0.25.0`.
  2. `framework-go-app-generator`: `go.mod` requires framework-go v0.25.0 and projectmodel v0.6.0; `GOWORK=off GOFLAGS=-mod=mod GOMODCACHE=$(mktemp -d) go mod tidy` against the real proxy; commit; tag `framework-go-app-generator/v0.27.0`.
  3. `framework-go-scenariohost`: requires app-generator v0.27.0, framework-go v0.25.0, projectmodel v0.6.0; tidy as above; commit; tag `framework-go-scenariohost/v0.8.0`.
  4. `method-assets`: `assets/scaffold/go.mod.tmpl` pins app-generator v0.27.0, scenariohost v0.8.0, framework-go v0.25.0, projectmodel v0.6.0; `go.sum.tmpl` regenerated against the real proxy with a fresh GOMODCACHE; `GOWORK=off go test ./`; commit; tag `method-assets/v0.27.0`.
  5. `framework-go-conformance`: requires method-assets v0.27.0 and the four above; tidy; commit; tag `framework-go-conformance/v0.4.0`. Release gate: the five conformance jobs green on that commit.
- [ ] **Step 3: Pin the app.** `cd $A/server && GOWORK=off go get` the six modules at those versions, `GOWORK=off go mod tidy` (env per Commands); `GOWORK=off go build ./... && GOWORK=off go vet ./...`; `make claude-assets`.
- [ ] **Step 4: App gates.** `make gen-models gen-fakes gen-client gen-internal-tools gen-tests`; `cd webApp && npm run gen:api`; `cd $A/server && GOWORK=off go test -count=1 -p 1 ./internal/ ./cmd/aiarch-state-mcp ./cmd/backfill-attempts` — PASS (`TestDerivedPlan_SurfacesSelectActivityType`, `TestSurfaceSchema_DecodesStrictly`, `TestMissionDeliverySurfaces_RecordVerb`, `TestPutDraftModel_RefusesSystemMissingRequiredSurface`, `TestAttributeRule_SurfaceRulesBelongToTheSystem`, `TestConformanceFixture_RecordsThroughVerbs`).
- [ ] **Step 5: Own project rules.** `make method-check`: list every Error from SYS-SURFACE-REQUIRED, SYS-SURFACE-ENTRY, GEN-SURFACE-SUPPORTED and SYS-CLIENT-SURFACE on the own project. SYS-CLIENT-SURFACE and GEN-SURFACE-SUPPORTED must have none (D3, D12). For each of the other two that fires, add `{rule, reason: "ruling 8: own-project surface rebinding scheduled", expires: "2026-12-31"}` to `ruleWaivers` (as Plans 2 and 4 did) and record the count in the commit body.
- [ ] **Step 6: Ruling 9.** The own mission lists no `mcpApp` (D12), so no GEN-SURFACE-SUPPORTED waiver is written and `cmd/server/mcp_apps.go` stays on the `MCPMount` hook; E29 is recorded. Fallback, only if the lead adds an MCP Apps quote to the own brief before this step: add the `mcpApp` entry to the own `deliverySurfaces` and `{rule: "SYS-SURFACE-REQUIRED", reason: "ruling 9: the own MCP Apps shell is hand-written until migrated to the generated path (E29)", expires: "2026-12-31"}` to `ruleWaivers`.
- [ ] **Step 7: Full suite once.** `cd $A/server && GOWORK=off go test -p 1 -timeout 180m ./...`; `make method-check gen-tests-check gen-models-check gen-lifecycles-check gen-client-check gen-internal-tools-check lint test`; `cd $A/webApp && npm run typecheck && npx eslint src` — all green; `TestAdmit_PassesOwnProject` green.
- [ ] **Step 8: Commit** (app, explicit paths — never `acceptancecheck_test.go`) `git commit -m "feat(app): delivery surfaces — Surface on Clients, deliverySurfaces on the mission, SYS-SURFACE-*/GEN-SURFACE-SUPPORTED at putDraftModel; pins Plan 5 release"`.

**Earmarks (not in this plan):** E28 acceptance variants through an MCP Apps host (the acceptance Playwright emitter drives the SPA page; Task 5.4 refuses an unskipped mcpApp variant). E29 the own hand-written MCP Apps shell (`cmd/server/mcp_apps.go`, `webApp/vite.mcp.config.ts`, the `ui.view` contract annotation and mcpemit's annotation arm) moves to the generated path once the own System has an mcpApp Client; `projectmodel.OpUI` is deleted then. E30 the frontend construction prompts say nothing about an mcpApp's host-bridge seat (the agent owns `routes/` and below); a prompt line and an edit-scope check belong to Plan 6's manifests.

## Done when
A System missing a required surface is refused by `putDraftModel`; `TestConformance_McpAppSurface` green; own project conformed or waived.
