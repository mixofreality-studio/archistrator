# FPC Plan 3 — The Composed App Is the Unit of Integration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Expanded to step detail 2026-11-08 from the code at platform `fpc/main` @f7f10dcc and app `worktree-first-pass-correctness-plan` @fc23f147 (Plans 1 and 2 landed and released: framework-go v0.22.0, projectmodel v0.5.0, app-generator v0.24.1, scenariohost v0.5.0, method-assets v0.24.1, framework-go-conformance v0.1.0, http-generator v0.5.0).

**Goal:** No activity reaches `integrated` unless the whole composed app boots through its generated run contract, and every machine-client scenario runs over the wire through the real composition-root policy.

**Architecture:** scenariohost owns the one substrate set and launches the generated `devstack` into it (spec §4b). The integration check set replaces compile-only composition checks with a boot smoke (`/readyz`, SPA index, `/mcp` initialize + `tools/list`) and teardown. REST/MCP emitters stop serving handlers in-process and call the booted server with the principal from the plan, injected per request by the generated local auth edge from a declared allowlist. The composition-root authorization policy is generated from the model, so it has an owner and sits in generated code. Web scenarios run under the scenario host too (`go tool webscenarios`), which serves the arrange endpoint the browser runtime calls.

**Spec:** §W5; §4b "One substrate set", "Per-request principals in the local profile"; SA-1 P4 ruling (Tier 1 smoke on every integration; Tier 2 machine clients over the wire; composition-root policy generated; no `permitAll`). **Depends on:** Plans 1–2.

## Global Constraints (plan-local)
- Teardown is owned by scenariohost/devstack. A scenario run may not leave a process, container or volume (Plan 1's `DockerLeftovers` is the oracle).
- `permitAll` must not exist anywhere in app-generator after this plan (`grep -rn permitAll` returns nothing).

## Review Focus
1. **A 403 from the generated policy on a verb the SPA uses** (RUN-015): Task 3.4 `TestPolicyGen_AdmitsEveryGeneratedRequestForOwner`.
2. **Devstack started twice from parallel integrations collide on ports/volumes**: Task 3.1 `TestDevstackLaunch_ParallelIsolated`.
3. **Orphaned server after a killed test**: Task 3.1 `TestDevstackLaunch_StopKillsProcessGroup`.
4. **MCP initialize succeeds but tools/list is empty** (FIX-050 variant): Task 3.2 `TestSmoke_MCPListsEveryGeneratedTool`.
5. **A client scenario that passes in-process but fails over the wire due to transport encoding**: Task 3.3 `TestREST_OverWire_ErrorEnvelopeMatchesContract`.

---

## Detail-phase decisions (2026-11-08; each derived from the code named, the lead may override)

- **D1 — where the launcher lives.** `arch.CheckScenarioComposeTestOnly` (`framework-go/arch/scenariocompose_testonly.go:74`, `isTestSubstrate`) refuses any production package of an app importing `framework-go-scenariohost` *or a package beneath it*, `cmd/` included; archistrator's own job binary `server/cmd/aiarch-state-mcp` (the `smoke` check, Task 3.2) and `cmd/testgen` (the web null run, Task 3.5) are production code. So the T-free launcher is a leaf package of the module that already owns the run contract: **`framework-go-app-generator/devstack`** (`Launch`, `App`, `Smoke`, `ToolNames`, `DockerInventory`, `DockerLeftovers`). scenariohost wraps it for tests (`Host.LaunchDevstack`) and therefore gains a `require` on app-generator (sibling `replace`, as its other requires); `framework-go-conformance/boot.go` keeps `Boot`/`Probe`/`DockerInventory`/`DockerLeftovers` as thin delegations so its tests and the app's `conformance.*` callers do not move. The plan sketch's `scenariohost.Smoke(app)` becomes `devstack.Smoke`.
- **D2 — devstack's ready line is kept.** `rungen/runtime.go:116-120` prints `{"devstack":"ready","url":"http://127.0.0.1:<port>"}` and `conformance.Boot` (`boot.go:40-47`, `readyScanner`) already parses it. The sketch's `LISTENING <addr>` line is not introduced; the JSON line is the contract.
- **D3 — isolation uses existing inputs.** `projectmodel.Substrate.Inputs()` (`deployment.go:382-398`) has no Postgres `SCHEMA` input, so the sketch's `<PREFIX>_SCHEMA` is dropped. A launch is isolated by a Postgres **database** per launch (the `URL` input names it; `Host.PostgresDatabase`) and a Temporal **namespace** per launch (the `NAMESPACE` input; two devstacks sharing one namespace would poll the same task queues and run each other's workflows). Ports: devstack already picks a free address (`runtime.go:401`); a preset `<PREFIX>_LISTEN_ADDR` is honoured (new).
- **D4 — principals over the wire.** The dev edge injects one `security.Principal{Subject}` for every request (`httpgen/wiring.go:128-134`, hook seed `composegen/hooks.go:258`), with no roles and no claims, so an agent's delegation (Task 3.0) and a second actor cannot reach the Manager over the wire. Spec §4b "Per-request principals in the local profile" is therefore part of Task 3.3 (Plan 2 Task 2.10 had deferred it to Plan 4): a declared, local-only `<PREFIX>_DEV_PRINCIPALS` allowlist (JSON) and an `X-Dev-Principal: <subject>` request header, honoured only when the subject is on the allowlist. This touches `framework-go-http-generator` (its `AuthMiddleware`), which joins the release set.
- **D5 — the generated policy needs no new model field.** Every generated request is already owner-scoped by construction: `httpgen.OpPlan.AuthzScope` is the principal's own `<manager>Catalog` and the `ResourceRef.ID` is always the principal's subject (`httpgen/plan_exported.go:52-60`, `composegen/authz.go:83-99`). The sketch's `authorization.ownerScopes` would have nothing to express; the generated `policy.gen.go` is the hoisted `edgePolicySource` of `composegen/scenariocompose_client.go:152-185` (permit iff `p.Subject != "" && r.ID == p.Subject && generatedAuthorizations[{verb, scope}]`). No schema change, no `$defs` mirror, GC-5 is trivially met. Resource-level ownership stays the Manager's (the fixture's `AccessGrantingEngine`).
- **D6 — the web run is owned by the scenario host.** Today nothing boots a server for a web scenario batch (`server/cmd/aiarch-state-mcp/checks.go:310-312` sets only `AIARCH_SCENARIO_TIMEOUT_MS`; `tsruntime/runtime.ts:88-91` fails with "no base URL"). Playwright's own `webServer` kills its process with SIGKILL, which would skip devstack's cleanups and leak the Postgres container (plan constraint 1). A new binary **`framework-go-scenariohost/cmd/webscenarios`** boots the host (T-free `scenariohost.Boot`), launches devstack into it, serves the arrange endpoint (Task 3.5) and runs `npx playwright test` with `PLAYWRIGHT_TEST_BASE_URL` and `AIARCH_NULL_OPS` passed through; the app's web scenario batch, `surface-check` and testgen's web null run all go through it.
- **D7 — `advanceClock` is not expressible over the wire yet.** The booted server's components read real time; only the in-process host clock moves (`scenariohost/arrange.go:60-66`). A web binding arranging `advanceClock` is refused at generation with the scenario named (testgen error), never skipped; `failNext` and `seedResource` round-trip (Task 3.5). Earmark E17: a model-declared clock for the composition root.
- **D8 — Task 3.0's claim shape.** `framework-go/utilities/security` defines no delegation claim (`grep -rn delegation framework-go/utilities/security` is empty; `Principal.Claims` is the escape hatch, `principal.go:27`). The shape is the consumer convention the conformance fixture's ListManager reads (`framework-go-conformance/testdata/todomvc/impl/internal/manager/list/listmanager.go:193-221`): `Claims["delegations"]` is a list of `{grantor, intents}`, and a grant with no intents has no reach (`accessgrantingengine.go:49-56`). `onBehalfOf` alone cannot say what the agent may do, so `TestPrincipal` gains an optional `intents: []string` (methodcheck + app `$defs` mirror + testgen principal table); the fixture's `team-agent` declares the seven intents its scenarios use.

## File map

Platform:
- Create `framework-go-app-generator/devstack/{devstack.go,smoke.go,docker.go,devstack_test.go}`; modify `rungen/runtime.go` (preset substrates and listen address honoured), `rungen/rungen.go` (framework inputs skipped), `configgen/{inputs.go,emit.go}` (`DevPrincipalsEnv`, `ScenariohostURLEnv`), `composegen/{emit_http.go,surfaces.go,authz.go,hooks.go,composegen.go,nullweb.go,scenariocompose_client.go}`, create `composegen/{emit_policy.go,emit_arrange_http.go,emit_faults.go}`, `nullgen/nullgen.go` (delete `permitAllPDP`), `testgen/{emit_mcp.go,emit_rest.go,emit_null.go,testgen.go,emit_playwright.go,tsruntime/runtime.ts}`, `cmd/testgen/{main.go,discriminate.go}`.
- Modify `framework-go-http-generator/httpgen/wiring.go` (`DevConfig.Principals`, `DevPrincipalHeader`).
- Modify `framework-go-scenariohost/{callcontext.go,host.go,input.go,arrange.go,go.mod}`; create `devstack.go`, `postgres.go`, `arrangeserver.go`, `cmd/webscenarios/main.go` and their tests.
- Modify `framework-go/methodcheck/{project.go,bindings.go}` (`TestPrincipal.Intents`), `framework-go/arch/scenariocompose_testonly.go` unchanged (D1 avoids it).
- Modify `framework-go-conformance/{boot.go,go.mod}`; create `devstack_test.go`, `wire_test.go`; modify `testdata/todomvc/{project.json,README.md,impl/cmd/server/hooks.go}`.
- Modify `method-assets/assets/scaffold/{Makefile.scenarios.mk.tmpl,go.mod.tmpl}`, `assets/claude/commands/{service,frontend}-integration.md`.

App:
- Modify `server/internal/resourceaccess/projectstate/projectstateaccess.go` (`CheckKindSmoke`, `compositionChecks`, `frontendScopeAndChecks`), `server/cmd/aiarch-state-mcp/checks.go` (`runSmoke`, web batch via `webscenarios`), `server/cmd/aiarch-state-mcp/freshcomposition_test.go` (lift `requireDelegationClaim`), `.aiarch/state/project.json` (`$defs.TestPrincipal.intents`), `server/go.mod`, `Makefile.scenarios.mk`.

---

### Task 3.0: scenariohost delegation claim

**Basis:** Plan 2 earmark E15 (fpc-2 Task 2.11h). `framework-go-scenariohost/callcontext.go:31` writes `Claims[ClaimOnBehalfOf]` (`"onBehalfOf"`); the fixture's ListManager reads `Claims["delegations"]` as `[]any` of `map[string]any{"grantor": string, "intents": []any|[]string}` (`listmanager.go:194-221`) and its engine permits an agent only an intent inside a grant whose grantor is the owner (`accessgrantingengine.go:49-56`). The app's `requireDelegationClaim` (`server/cmd/aiarch-state-mcp/freshcomposition_test.go:499-512`) skips the machine clients' verdicts until the pinned scenariohost's non-test sources contain the string `"delegations"` (`scenariohostMapsDelegations`, :469-493), and fails after 2026-12-31.

**Files:**
- Modify: `framework-go/methodcheck/project.go:755-761` (`TestPrincipal`), `framework-go/methodcheck/bindings_test.go`
- Modify: `framework-go-scenariohost/callcontext.go:17-50`; test `framework-go-scenariohost/principal_test.go`
- Modify: `framework-go-app-generator/testgen/emit_go.go:805-826` (`writePrincipalTable`); test `testgen/emit_go_v2_test.go`
- Modify: `framework-go-conformance/testdata/todomvc/project.json` (`phaseArtifacts.testPlan.{todoApi,todoMcpServer}.principals["team-agent"]`), `testdata/todomvc/README.md` §"Rebinding for the strong proof"
- App (Task 3.6 carries it): `.aiarch/state/project.json` `serviceContracts.projectStateAccess.$defs.TestPrincipal` gains `intents`.

**Interfaces (Produces):**
```go
// methodcheck
type TestPrincipal struct {
	Subject    string   `json:"subject"`
	Roles      []string `json:"roles"`
	OnBehalfOf string   `json:"onBehalfOf,omitempty"`
	Intents    []string `json:"intents,omitempty"` // what the delegating human lets this agent do, in the app's own vocabulary; read only when OnBehalfOf is set
}
// scenariohost
type Principal struct { Subject string; Roles []string; OnBehalfOf string; Intents []string }
const ClaimDelegations = "delegations"                       // replaces ClaimOnBehalfOf (deleted)
type Delegation struct { Grantor string `json:"grantor"`; Intents []string `json:"intents"` }
func DelegationsOf(p security.Principal) []Delegation       // the one reader: decodes Claims[ClaimDelegations] from []any/map[string]any or []Delegation
// securityPrincipal: OnBehalfOf != "" → Kind = PrincipalApplication, Claims = map[string]any{ClaimDelegations: []any{map[string]any{"grantor": p.OnBehalfOf, "intents": toAny(p.Intents)}}}; Intents with no OnBehalfOf → t.Fatalf("scenariohost: Principal %q declares intents but no OnBehalfOf")
```
- `writePrincipalTable` emits `, OnBehalfOf: "alice", Intents: []string{"read", "add"}` for a principal that declares them (order kept).
- A `TestPrincipal` with `intents` and no `onBehalfOf` is a methodcheck Error from the existing `TP-PRINCIPAL-DELEGATION` emitter (`rules_testplan_v2.go:530-533` gains the second arm: "intents without onBehalfOf").

- [ ] **Step 1: Write the failing tests.**
```go
// framework-go-scenariohost/principal_test.go
func TestPrincipalContext_DelegationClaimShape(t *testing.T) {
	h := Start(t)
	p := Principal{Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice", Intents: []string{"read", "add"}}
	got, _ := security.PrincipalFrom(h.PrincipalContext(t, p))
	if got.Kind != security.PrincipalApplication { t.Errorf("kind = %q, want application", got.Kind) }
	if _, stale := got.Claims["onBehalfOf"]; stale { t.Error(`Claims["onBehalfOf"] must no longer be written`) }
	raw, ok := got.Claims[ClaimDelegations].([]any)
	if !ok || len(raw) != 1 { t.Fatalf("Claims[%q] = %#v, want one grant", ClaimDelegations, got.Claims[ClaimDelegations]) }
	grant := raw[0].(map[string]any)
	if grant["grantor"] != "alice" { t.Errorf("grantor = %v", grant["grantor"]) }
	if intents, _ := grant["intents"].([]any); len(intents) != 2 || intents[0] != "read" || intents[1] != "add" { t.Errorf("intents = %v", grant["intents"]) }
	ds := DelegationsOf(got)
	if len(ds) != 1 || ds[0].Grantor != "alice" || !slices.Equal(ds[0].Intents, []string{"read", "add"}) { t.Errorf("DelegationsOf = %+v", ds) }
	if _, has := security.PrincipalFrom(h.PrincipalContext(t, Principal{Subject: "alice"})); !has { t.Fatal("human principal lost") }
	if human, _ := security.PrincipalFrom(h.PrincipalContext(t, Principal{Subject: "alice"})); human.Claims != nil { t.Errorf("a human carries no claims, got %v", human.Claims) }
}
// framework-go/methodcheck/bindings_test.go
func TestBindings_PrincipalIntentsDecode(t *testing.T)          // `{"subject":"a","roles":["agent"],"onBehalfOf":"alice","intents":["read"]}` → Intents == ["read"]; DisallowUnknownFields still refuses "hook"
func TestTP_PrincipalDelegation_IntentsNeedOnBehalfOf(t *testing.T) // fixture tp2_intents_orphan.json: a principal with intents and no onBehalfOf → one TP-PRINCIPAL-DELEGATION Error naming the principal
// framework-go-app-generator/testgen/emit_go_v2_test.go
func TestGo_PrincipalTableCarriesIntents(t *testing.T)           // generated source contains `"team-agent": {Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice", Intents: []string{"read", "add"}},`
```
- [ ] **Step 2: Run** `cd /Users/davidmarne/mixofrealitystudio/fpcwt/archistrator-platform/framework-go-scenariohost && GOWORK=off go test ./ -run TestPrincipalContext_DelegationClaimShape -v`; `cd ../framework-go && GOWORK=off go test ./methodcheck -run 'TestBindings_PrincipalIntentsDecode|TestTP_PrincipalDelegation_IntentsNeedOnBehalfOf'`; `cd ../framework-go-app-generator && GOWORK=off go test ./testgen -run TestGo_PrincipalTableCarriesIntents` — Expected: FAIL (undefined `Intents`, `ClaimDelegations`, `DelegationsOf`).
- [ ] **Step 3: Implement** `TestPrincipal.Intents`; `scenariohost.Principal.Intents`, `ClaimDelegations`, `Delegation`, `DelegationsOf`, the `securityPrincipal` body; delete `ClaimOnBehalfOf`; `writePrincipalTable`; the rule arm. Fixture: `todoApi` and `todoMcpServer` `principals["team-agent"]` gain `"intents": ["read","add","reword","discard","mark","toggleAll","clearCompleted"]` (the fixture's `Intent` enum, `project.json` `$defs.Intent`); README records that the delegation claim is now produced by the host in the shape `impl/` reads and that `onBehalfOf` without intents has no reach.
- [ ] **Step 4: Run** the three packages above — PASS; `cd framework-go-conformance && GOWORK=off go test ./ -run TestFixtures_LoadUnderProjectmodelAndMethodcheck` — PASS; `GOWORK=off go test -tags conformance ./ -run 'TestConformance_RUN021Proof/(derive|methodcheck|testgen)' -timeout 20m` — PASS.
- [ ] **Step 5: Commit** (platform) `git commit -m "feat(scenariohost,methodcheck): delegation claim written as Claims[\"delegations\"] grants with intents; TestPrincipal.intents"`. Note for Task 3.6: the app's `requireDelegationClaim` skip lifts by itself once `server/go.mod` pins a scenariohost whose non-test sources contain `"delegations"` (`callcontext.go` now does); the app's `$defs.TestPrincipal` must gain `intents` in the same change or the pinned methodcheck's strict decode is fine but the app's codec drops the field.

---

### Task 3.1: scenariohost launches devstack into its substrates

**Files:**
- Create: `framework-go-app-generator/devstack/devstack.go`, `devstack/docker.go` (moved from `framework-go-conformance/boot.go:279-315`), `devstack/devstack_test.go`
- Modify: `framework-go-app-generator/rungen/runtime.go:63-125` (`run`), `:175-204` (`provision`); `rungen/rungen.go:104-128` (`Derive` skips framework inputs); test `rungen/rungen_test.go`
- Modify: `framework-go-conformance/boot.go` (`Boot`/`DockerInventory`/`DockerLeftovers` delegate; `readyScanner`, `tailBuffer`, `terminate` move to devstack), `go.mod` (add `framework-go-scenariohost` require + replace); create `framework-go-conformance/devstack_test.go` (tag `conformance`)
- Create: `framework-go-scenariohost/devstack.go`, `postgres.go`; modify `host.go:57-87` (`Host` gains `launches int`, `databases []string`), `temporal.go` (namespace per launch), `go.mod` (require `framework-go-app-generator`, sibling replace)

**Interfaces (Produces):**
```go
// framework-go-app-generator/devstack
type App struct {
	BaseURL string          // "http://127.0.0.1:<port>" from the ready line
	MCPURL  string          // BaseURL + "/mcp"
	Stop    func() error    // SIGTERM to the launcher's process group, wait ≤ StopGrace (90s), then SIGKILL; error when the group outlived StopGrace
	Log     func() string   // the launcher's combined output tail (40 lines), for failure messages
}
const ReadyTimeout = 10 * time.Minute // launcher up to its ready line (it may build the SPA and the server)
func Launch(ctx context.Context, moduleDir string, env []string, log io.Writer) (*App, error) // `go run ./cmd/devstack` in moduleDir, cmd.Env = os.Environ()+GOWORK=off+env (later wins), Setpgid; waits for the {"devstack":"ready","url":…} line (conformance.readyScanner moved here) then GET /readyz == 200; an exit before the line is an error carrying the log tail
func DockerInventory(ctx context.Context) ([]string, error)          // moved verbatim from conformance/boot.go:279
func DockerLeftovers(_ context.Context, before, after []string) []string // moved verbatim from conformance/boot.go:302

// framework-go-scenariohost
func (h *Host) LaunchDevstack(t *testing.T, moduleDir string, principals ...Principal) *App // moduleDir "" = the host's module root (moduleRoot(), host.go:345); reads <moduleDir>/.aiarch/state/project.json (projectmodel.LoadFile) and <moduleDir>/appgen.json ({"envPrefix","container"}; defaults "APP" and the deployment's first container, as cmd/appgen/main.go:49,144); env = h.Env(t, prefix, m.EffectiveDeployment(container)) with the postgres URL replaced by h.PostgresDatabase(t) and the temporal NAMESPACE by h.TemporalNamespaceFor(t); + configgen.DevPrincipalEnv(prefix)=principals[0].Subject ("scenario" when none) + configgen.DevPrincipalsEnv(prefix)=JSON of every principal (Task 3.3; written only when len(principals) > 0); t.Cleanup(app.Stop) and t.Fatalf on a Stop error naming the leftover
type App = devstack.App
func (h *Host) PostgresDatabase(t *testing.T) string   // CREATE DATABASE "devstack_<n>" on h.PostgresURL (pgx, template0), returns the URL with that database; DROP DATABASE ... WITH (FORCE) on t.Cleanup; t.Skip under -short as Input does
func (h *Host) TemporalNamespaceFor(t *testing.T) string // RegisterNamespace("devstack-<n>", retention 1 day) through h.temporal's WorkflowService(); returned once registered (poll DescribeNamespace ≤ 30s); a dev server keeps it for the process, which is acceptable: names are per process and per launch
```
- rungen `runtime.go`: `provision` starts a substrate only when some variable it provides is unset in the environment: Temporal when any `temporal.*`-sourced env is empty, Postgres when the `postgres.url`-sourced env is empty, a git repo per key when its env is empty; a preset variable is kept (`env[p.Env] = os.Getenv(p.Env)` when set). `run` keeps a preset `plan.ListenEnv` instead of `freeAddr()`. Log line on skip: `devstack: <substrate> from the environment (<ENV>)`.
- rungen `Derive`: the framework's own local inputs (`configgen.DevPrincipalEnv`, `DevPrincipalsEnv`, `ScenariohostURLEnv` — Task 3.3/3.5 add the latter two) are neither provisioned nor required (`:112-115` pattern extended to a set).

- [ ] **Step 1: Write the failing tests.**
```go
// framework-go-app-generator/rungen/rungen_test.go
func TestLauncherHonoursPresetSubstratesAndListenAddr(t *testing.T) // rendered launcher contains `if os.Getenv(p.Env) != ""` guard text `from the environment` and `if preset := os.Getenv(plan.ListenEnv); preset != ""`; TestLauncherCoversTheLocalConfigBothWays stays green (the framework inputs are not in Required)
// framework-go-app-generator/devstack/devstack_test.go (unit, no docker)
func TestLaunch_ExitBeforeReadyCarriesTheLog(t *testing.T)   // moduleDir with a cmd/devstack/main.go that prints "boom" to stderr and exits 3 → error contains "boom" and "exit status 3" (moved from conformance TestBoot_ExitBeforeReadyCarriesTheLauncherLog)
func TestLaunch_ReadyLineThenReadyz(t *testing.T)            // a fake cmd/devstack that serves /readyz on 127.0.0.1:0 and prints the ready line → App.BaseURL set, MCPURL = BaseURL+"/mcp"; Stop returns nil and the port refuses connections afterwards
func TestDockerLeftovers(t *testing.T)                        // pure: before/after sets
// framework-go-conformance/devstack_test.go  //go:build conformance
func TestDevstackLaunch_UsesHostSubstrates(t *testing.T)      // buildFixture(t, dir); h := scenariohost.Start(t); before := DockerInventory; app := h.LaunchDevstack(t, dir); log has no "devstack: postgres on" and no "temporal dev server on"; DockerInventory during the run == before (no new container); Probe(ctx, Booted{BaseURL: app.BaseURL}) == nil
func TestDevstackLaunch_ParallelIsolated(t *testing.T)        // two LaunchDevstack of the same built dir: distinct BaseURL ports; the postgres URLs the two were launched with name distinct databases (h.PostgresDatabase called twice → "devstack_1", "devstack_2" present in pg_database); a listAddTodo through app A's /mcp (Task 3.3 MCPSession, or raw rpc until then) is not visible through app B's listOpenList; both Stop() nil
func TestDevstackLaunch_StopKillsProcessGroup(t *testing.T)   // after Stop: `pgrep -g <pgid>` is empty (pgid read from the launcher's pid), GET BaseURL/readyz fails to connect, DockerLeftovers(before, after) empty
```
- [ ] **Step 2: Run** `cd framework-go-app-generator && GOWORK=off go test ./rungen ./devstack` — FAIL (package missing, launcher text missing); `cd ../framework-go-conformance && GOWORK=off go test -tags conformance ./ -run 'TestDevstackLaunch_' -timeout 40m` — FAIL (undefined `LaunchDevstack`).
- [ ] **Step 3: Implement** `devstack` (move `readyScanner`, `tailBuffer`, `terminate`, docker helpers from `conformance/boot.go`; `conformance.Boot` becomes `app, err := devstack.Launch(ctx, dir, nil, &log); return Booted{BaseURL: app.BaseURL, Stop: app.Stop}`), rungen runtime/Derive, scenariohost `devstack.go` + `postgres.go` + namespace registration, `go.mod` requires (`GOWORK=off go mod tidy` in scenariohost and conformance).
- [ ] **Step 4: Run** `GOWORK=off go test ./rungen ./devstack ./configgen` (app-generator), `GOWORK=off go test ./` (scenariohost, `-short` and full), `GOWORK=off go test ./ && GOWORK=off go test -tags conformance ./ -run 'TestDevstackLaunch_|TestConformance_Boot' -timeout 50m` (conformance) — PASS. `golangci-lint run ./...` in the three modules — 0 issues.
- [ ] **Step 5: Commit** `git commit -m "feat(devstack,scenariohost): T-free devstack launcher; host launches devstack into its substrates with a database and namespace per launch; preset substrates honoured"`.

---

### Task 3.2: Boot smoke on every integration

**Files:**
- Create: `framework-go-app-generator/devstack/smoke.go`; test `devstack/smoke_test.go`
- Modify: `framework-go-conformance/boot.go:161-211` (`Probe` → `devstack.Smoke` with `SmokeSpec{SPA: true}`), `conformance_test.go` (`TestConformance_Boot` asserts the full tool list)
- App: `server/internal/resourceaccess/projectstate/projectstateaccess.go:12169-12173` (`CheckKindSmoke`), `:12999-13024` (`compositionChecks`), `:13114-13170` (`frontendScopeAndChecks` integration arm), `:13225` (`checkKindOrder`); `server/cmd/aiarch-state-mcp/checks.go:81-122` (`Run` dispatch), new `runSmoke`; tests `projectstateaccess_taskspec_test.go` (or the file holding the spec tests; locate with `grep -ln "compositionChecks\|build-devstack" server/internal/resourceaccess/projectstate/*_test.go`), `cmd/aiarch-state-mcp/checks_test.go`, `freshcomposition_test.go:289`

**Interfaces (Produces):**
```go
// framework-go-app-generator/devstack
type SmokeSpec struct {
	SPA   bool     // GET / must be 200 and contain "<html" (the model has a built browser surface)
	Tools []string // every name tools/list must contain; empty with MCP mounted = error "smoke: no tool expected; derive the list with ToolNames"
}
func ToolNames(m *projectmodel.Model, exposed map[string][]string) []string // for every web-exposed Manager an MCP client fronts (composegen's mcpMounts rule): projectmodel.LowerFirst(doc.ManagerBase()) + op.Name per served op — the same expression mcpgen genTools uses (mcpgen.go:84,97) and testgen's toolName (emit_mcp.go:toolName); sorted
func Smoke(ctx context.Context, app *App, spec SmokeSpec) error // GET /readyz 200 → SPA check when spec.SPA → MCP initialize (protocol "2025-06-18"), notifications/initialized, tools/list ⊇ spec.Tools (missing names listed in the error) ; the rpc helpers move here from conformance/boot.go:177-277
```
- App check: `CheckKindSmoke = "smoke"`; `compositionChecks` returns `build-server` and `build-devstack` (kept, as sub-steps the smoke needs green first) plus `SpecCheck{ID: "smoke:composition", Kind: CheckKindSmoke, Title: "The composed app boots through its run contract and answers /readyz, the SPA and /mcp", Source: SpecCheckSource{Dir: dir}, TimeoutSec: smokeCheckTimeoutSec /* 900 */}`; `checkKindOrder` ranks `smoke` after `command`, before `scenario`. `frontendScopeAndChecks(integration=true)` and the machine client's `serviceScopeAndChecks` already reach `compositionChecks` through `composesServer`; the browser surface's integration gains it explicitly (`if integration { checks = append(checks, compositionChecks(p, p.ModuleDir)...) }`), and `deployment` integration (`ms.Command == "deployment-integration"`, resolved at `:12743-12746`'s switch) gains the same three.
- `runSmoke(ctx, c)`: `before := devstack.DockerInventory` → `app, err := devstack.Launch(ctx, dir, nil, &log)` → `devstack.Smoke(ctx, app, SmokeSpec{SPA: proj has a built browser surface, Tools: devstack.ToolNames(model, exposedOps)})` → `app.Stop()` → `DockerLeftovers(before, after)` must be empty (else fail naming them). Status `pass|fail|timeout`, log = launcher tail + smoke error. Env: `commandEnv("go")` (GOWORK=off).

- [ ] **Step 1: Write the failing tests.**
```go
// framework-go-app-generator/devstack/smoke_test.go
func TestToolNames_MatchesTheGeneratedTables(t *testing.T)   // over the todomvc fixture (read through the conformance module's testdata by relative path, as composegen tests do): ["listAddTodo", "listClearCompleted", "listDiscardTodo", "listMarkTodo", "listOpenList", "listRewordTodo", "listToggleAll"] (derive the exact list from the fixture's listManager interface and the clients' exposed ops; pin it)
func TestSmoke_MCPListsEveryGeneratedTool(t *testing.T)       // httptest server: /readyz 200, / "<html>", /mcp a streamable MCP server (mcp.NewStreamableHTTPHandler) registering tools A and B; Smoke with Tools{A,B} nil; with Tools{A,B,C} error mentions "C"
func TestSmoke_FailsWhenSPAMissing(t *testing.T)              // / answers 404 → error "GET / = 404"; SPA:false passes
func TestSmoke_FailsWhenToolsEmpty(t *testing.T)              // /mcp lists no tools → error "tools/list returned no tools"
// app server/internal/resourceaccess/projectstate
func TestIntegrationChecks_IncludeSmokeForEveryType(t *testing.T) // TaskSpecFor over the conformance fixture (conformance.Fixture("todomvc")) for the integration task of: the Manager (service), todo-web-app (frontend), todoMcpServer and todoApi (machineClient); each has a check with ID "smoke:composition", Kind "smoke", after every "command:" check and before every "scenario:" check; TaskSpecSchemaVersion bumped to 5
// app server/cmd/aiarch-state-mcp/checks_test.go
func TestRunSmokeFailsOnLeftovers(t *testing.T)               // checkRunner with exec stubbed: a fake launcher that leaves "container:x" in the inventory → status fail, log names "container:x"
```
- [ ] **Step 2: Run** `cd framework-go-app-generator && GOWORK=off go test ./devstack -run 'TestToolNames|TestSmoke'` — FAIL; `cd <app>/server && GOWORK=off go test ./internal/resourceaccess/projectstate -run TestIntegrationChecks_IncludeSmokeForEveryType && GOWORK=off go test ./cmd/aiarch-state-mcp -run TestRunSmokeFailsOnLeftovers` — FAIL.
- [ ] **Step 3: Implement** `smoke.go` (+ `conformance.Probe` delegating with `SmokeSpec{SPA: true, Tools: ToolNames(...)}`), the app check kind, dispatch and `runSmoke`. App edits land on the app branch but are **not committed until Task 3.6** pins the released platform (the app builds with `GOWORK=off` against pins; until then run the app tests with `GOWORK=<worktree go.work>` as Plan 2 did).
- [ ] **Step 4: Run** platform: `GOWORK=off go test ./devstack ./rungen` (app-generator), `GOWORK=off go test -tags conformance ./ -run 'TestConformance_Boot$' -timeout 40m` (conformance) — PASS. App (workspace mode): the two packages above — PASS; `TestFreshManagerIntegrationComposesTheServer` (`freshcomposition_test.go:289` wants list gains `"smoke:composition"`) — PASS (needs docker + temporal CLI).
- [ ] **Step 5: Commit** (platform) `git commit -m "feat(devstack): Smoke — readyz, SPA, MCP tools/list against the model's tool names; conformance Probe delegates"`. App commit deferred to Task 3.6.

---

### Task 3.3: Machine clients over the wire

**Files:**
- Modify: `framework-go-http-generator/httpgen/wiring.go:105-140` (`middlewareBody`); test `httpgen/wiring_test.go`
- Modify: `framework-go-app-generator/configgen/inputs.go:55-64`, `emit.go:200-224`; `composegen/emit_http.go:29-40` (`writeSecurity`); tests `configgen/*_test.go`, `composegen/composegen_test.go`
- Modify: `framework-go-app-generator/testgen/emit_mcp.go` (delete `toolsHelpers`, `serveTools`, `toolSession`, `sessionVar`, `serveSessions`), `emit_rest.go` (delete `restHelpers`; keep `restRouteTable`, `restWireCodes`/`restErrorHasCode`), `testgen.go:235-238` (no `reasonClient` skip), `emit_null.go:53-54`; tests `testgen/emit_mcp_test.go`, `emit_go_v2_test.go`
- Create: `framework-go-scenariohost/wire.go`; test `wire_test.go`
- Delete: `framework-go-app-generator/composegen/scenariocompose_client.go:109-128` `emitClients` constructors (`New<ClientIface>`), keep `clientAuthzRows` for Task 3.4; test `composegen/scenariocompose_test.go:181` (`TestScenarioCompose_EmitsMachineClientConstructors` inverts to `_EmitsNoClientConstructors`)
- Create: `framework-go-conformance/wire_test.go` (tag `conformance`)

**Interfaces (Produces):**
```go
// httpgen (generated into internal/client/web/middleware.gen.go of every app)
const DevPrincipalHeader = "X-Dev-Principal"
type DevConfig struct {
	Enabled    bool
	Principal  security.Principal
	Principals map[string]security.Principal // local-only allowlist by subject; a request carrying DevPrincipalHeader naming one is served as it, any other value is 401 "dev principal not allowed"; absent header → Principal
}
// configgen (config.gen.go)
func DevPrincipalsEnv(prefix string) string               // prefix + "_DEV_PRINCIPALS": JSON `[{"subject":"","roles":[],"claims":{}}]`
func (c *Config) LocalDevPrincipals(profile string) (map[string]security.Principal, error) // "" → nil; profile != "local" → error naming the variable (same text shape as LocalDevPrincipal); bad JSON → error
// composegen writeSecurity emits after `dev := hooks.DevConfig(cfg)`:
//   if dev.Principals, err = cfg.LocalDevPrincipals(hooks.ResolveProfile(cfg)); err != nil { return err }
// scenariohost
type Session interface { Call(ctx context.Context, name, args string) (any, error) } // the generated file's subject; the same shape testgen's toolSession had
func MCPSession(t *testing.T, app *App, p Principal) Session   // mcp.NewClient(...).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: app.MCPURL, HTTPClient: &http.Client{Transport: devPrincipalTransport{subject: p.Subject}}}, nil); Call = CallTool; isError → error carrying the tool text; closed on t.Cleanup
func RESTClient(t *testing.T, app *App, p Principal, routes map[string]Route) Session // Route{Verb, Path}; Call spells args onto the route as testgen's restRequest did (path holes, GET query, JSON body) with header X-Dev-Principal: p.Subject; status ≥ 400 → error "<name>: <status> <body>"
type Route struct{ Verb, Path string }
```
- Generated client scenario (both dialects): `app := h.LaunchDevstack(t, "", principalsOf(plan)...)` once per test (every principal the plan declares is allowlisted; the first is the process principal); `subjectAs<Actor> := scenariohost.MCPSession(t, app, principal("<actor>"))` / `scenariohost.RESTClient(t, app, principal("<actor>"), clientRoutes)`; steps and same-component probes call `Call` over the wire (unchanged emitted lines); `clientRoutes` becomes `map[string]scenariohost.Route`. The in-process `handlers := scenariocompose.New<Client>(…)` line and the `scenariocompose` import go; a null run inherits `AIARCH_NULL_OPS` into the launched process (the composed server's `applyNullOps`, `composegen/nullweb.go`). `Discriminate` lists client scenarios (`testgen.go:238` skip removed; `reasonClient` deleted); the per-run cost (a devstack boot per `go test`) is budgeted by the app's `discriminateTimeoutSec`, which now counts client scenarios too (Task 3.6).
- `scenariocompose` no longer emits `New<Client>`/`edgePolicySource`; `clientAuthzRows` moves to `authz.go` for Task 3.4.

- [ ] **Step 1: Write the failing tests.**
```go
// framework-go-http-generator/httpgen/wiring_test.go
func TestAuthMiddleware_DevPrincipalHeaderFromAllowlist(t *testing.T) // compile the generated middleware in the package's sample (internal/sample/wiring) and drive it: no header → Principal; header "alice" on allowlist → alice (roles and claims as listed); header "mallory" → 401 body "dev principal not allowed"; Enabled=false ignores the header
// framework-go-app-generator/configgen
func TestConfig_LocalDevPrincipals(t *testing.T)                   // generated config.gen.go contains `func (c *Config) LocalDevPrincipals(profile string)` and reads APP_DEV_PRINCIPALS; TestLauncherCoversTheLocalConfigBothWays still green (rungen skips it)
// framework-go-app-generator/composegen
func TestCompose_WiresDevPrincipalsUnderLocal(t *testing.T)        // main.gen.go contains `dev.Principals, err = cfg.LocalDevPrincipals(hooks.ResolveProfile(cfg))`
func TestScenarioCompose_EmitsNoClientConstructors(t *testing.T)   // todomvc fixture: compose.gen.go has no `func NewTodoApiList`, no `generatedPolicy`, no `edgeSecurity`
// framework-go-app-generator/testgen
func TestMCP_OverWire_SubjectIsASession(t *testing.T)               // generated file contains `app := h.LaunchDevstack(t, "", principal("team-agent"))` and `scenariohost.MCPSession(t, app, principal("team-agent"))`; contains neither `serveTools` nor `scenariocompose.` nor `mcp.NewInMemoryTransports`
func TestREST_OverWire_UsesRoutesTable(t *testing.T)                // `scenariohost.RESTClient(t, app, principal("team-agent"), clientRoutes)` and `var clientRoutes = map[string]scenariohost.Route{`
func TestEmitters_NoPermitAll(t *testing.T)                         // walks every non-test .go under testgen/, composegen/, nullgen/ and asserts none contains "permitAll" (red until Task 3.4 deletes nullgen's)
func TestDiscriminate_ListsClientScenarios(t *testing.T)            // Output.Discriminate has an entry per (todoApi scenario, op); Output.Skipped has no reasonClient entry
// framework-go-conformance/wire_test.go  //go:build conformance
func TestMCP_OverWire_UsesRealPolicy(t *testing.T)                  // built fixture (Task 3.4's generated policy): MCPSession as principal{Subject:"alice"} → listOpenList ok; a session as principal{Subject:"mallory"} calling listOpenList with owner alice's list → the Manager's refusal (accessgranting notOwner, error text contains "notOwner"); a request whose header names a subject not on the allowlist → 401
func TestREST_OverWire_ErrorEnvelopeMatchesContract(t *testing.T)   // RESTClient as alice: listDiscardTodo of an unknown id → error text contains "not_found" (restWireCodes["NotFound"]) and status 404; the same op over MCPSession → error text contains "NotFound": both satisfy errorHasCode for the plan's `NotFound`
```
- [ ] **Step 2: Run** `cd framework-go-http-generator && GOWORK=off go test ./httpgen -run TestAuthMiddleware_DevPrincipalHeaderFromAllowlist`; `cd ../framework-go-app-generator && GOWORK=off go test ./configgen ./composegen ./testgen -run 'TestConfig_LocalDevPrincipals|TestCompose_WiresDevPrincipalsUnderLocal|TestScenarioCompose_EmitsNoClientConstructors|TestMCP_OverWire|TestREST_OverWire|TestEmitters_NoPermitAll|TestDiscriminate_ListsClientScenarios'` — FAIL; the conformance pair FAILS until Task 3.4 (no policy yet) — run them in Task 3.4 Step 4.
- [ ] **Step 3: Implement** httpgen middleware; configgen input + reader; composegen `writeSecurity`; scenariohost `wire.go`; testgen emitters (the generated REST file keeps `restWireCodes`/`errorHasCode`; the MCP file keeps `toolsErrorHasCode`; both drop their transport helpers and the `security` import, keep `expectHelpers`, `refHelpers`, `matchHelpers`); delete `emitClients`' constructors; rungen `Derive` already skips `DevPrincipalsEnv` (Task 3.1).
- [ ] **Step 4: Run** `GOWORK=off go test ./httpgen` (http-generator), `GOWORK=off go test ./configgen ./composegen ./testgen ./cmd/testgen ./rungen` (app-generator; `TestEmitters_NoPermitAll` stays red until 3.4 — note it), `GOWORK=off go test ./ -short` (scenariohost; `wire_test.go` unit half: `RESTClient` against an `httptest.Server` that echoes the header) — PASS. `golangci-lint run ./...` — 0 in each.
- [ ] **Step 5: Commit** `git commit -m "feat(testgen,scenariohost,httpgen): machine clients run over the wire against the launched devstack as the plan's principals; local allowlisted X-Dev-Principal; in-process servers deleted"`.

---

### Task 3.4: Composition-root policy generated from the model

**Files:**
- Create: `framework-go-app-generator/composegen/emit_policy.go`; test `composegen/policy_test.go`
- Modify: `composegen/authz.go:59-81` (`emitAuthz` test body), `:100-130` (`authzTestBody`), `composegen/hooks.go:43-63` (`deriveHooks` drops `hookPDP`), `:217-231` (delete `hookPDP`), `composegen/emit_http.go:106-116` (`writeEdgeSecurity` → `edgeSecurity()` over `generatedPolicy{}`), `composegen/composegen.go:134-140,144-172` (`PolicyFile = "policy.gen.go"`, emitted when `len(r.webMgrs) > 0`), `composegen/scenariocompose_client.go` (`clientAuthzRows` → `authz.go`, rest deleted), `nullgen/nullgen.go:100-160` (delete `pdpHook` branch and `permitAllPDP`); tests `composegen/authz_test.go`, `nullgen/nullgen_test.go:104` (`TestNullgen_HooksCoverEverySeam` runs `TestPolicyAdmitsEveryGeneratedRequest` unchanged, now against the generated policy), `composegen/hooklist_test.go`
- Modify: `framework-go-conformance/testdata/todomvc/impl/cmd/server/hooks.go:41-60` (delete `PolicyDecisionPoint`, `listVerbs`, `listPolicy`; README notes it), `README.md`
- App (Task 3.6): `server/cmd/server/hooks.go:502-507` keeps `PolicyDecisionPoint()` as a plain method the hand code at `:551` and `:734` still calls (the interface no longer declares it); `cmd/server/policy.gen.go` lands in `compositionPaths` by the existing `cmd/server/**/*.gen.*` glob.

**Interfaces (Produces):**
```go
// generated cmd/server/policy.gen.go (package main)
type authzKey struct{ verb, scope string }
var generatedPolicyRows = map[authzKey]bool{ /* one entry per distinct (Verb, Scope) of generatedAuthorizations, sorted */ }
// generatedPolicy is the composition root's authorization policy, derived from the model: it permits an authenticated principal exactly the requests the generated transports send, on the principal's own scope (ResourceRef.ID == Subject), and denies everything else. Resource ownership inside an operation is the Manager's to decide.
type generatedPolicy struct{}
func (generatedPolicy) Decide(_ context.Context, p security.Principal, a security.Action, r security.ResourceRef) (bool, error) {
	return p.Subject != "" && r.ID == p.Subject && generatedPolicyRows[authzKey{a.Verb, r.Kind}], nil
}
// main.gen.go
func edgeSecurity() security.Security { return security.New(security.WithPolicyDecisionPoint(generatedPolicy{})) } // writeEdgeSecurity; callers `edgeSecurity(hooks)` → `edgeSecurity()`
// authz.gen_test.go (generated, replaces authzTestBody)
func TestPolicyAdmitsEveryGeneratedRequest(t *testing.T)            // kept name: for every row, Principal{Subject:"owner-a"} on ResourceRef{Kind: row.Scope, ID: "owner-a"} permitted
func TestPolicyGen_AdmitsEveryGeneratedRequestForOwner(t *testing.T) // for every row and for each Kind in {user, application}: permitted on the own ID; with roles [] and with roles ["agent"] (the policy reads no roles)
func TestPolicyGen_DeniesNonOwner(t *testing.T)                      // every row: ID "owner-b" denied; Subject "" denied; an unknown verb on a known scope denied; a known verb on an unknown scope denied
```
- The `Hooks` interface loses `PolicyDecisionPoint()`: `hooks.go` seeds no PDP, `hookDevConfig` stays. An app that still declares the method keeps compiling (an extra method on `appHooks`); `TestPolicyAdmitsEveryGeneratedRequest` no longer needs `DevConfig`'s principal.
- nullgen: `nullBody`'s `pdpHook` arm and `permitAllPDP` go; `TestEmitters_NoPermitAll` (Task 3.3) turns green here.

- [ ] **Step 1: Write the failing tests.**
```go
// composegen/policy_test.go
func TestPolicyGen_RowsFromAuthzTable(t *testing.T)        // todomvc fixture: policy.gen.go contains one `{verb: "add-todo", scope: "listCatalog"}: true,` per distinct row of authz.gen.go (compare the two emitted files' row sets)
func TestPolicyGen_HooksInterfaceHasNoPDP(t *testing.T)   // HookList(...) has no Hook named "PolicyDecisionPoint"; main.gen.go contains `edgeSecurity()` and not `hooks.PolicyDecisionPoint`
func TestPolicyGen_TestFileHoldsOwnerAndNonOwner(t *testing.T) // authz.gen_test.go contains `func TestPolicyGen_AdmitsEveryGeneratedRequestForOwner` and `func TestPolicyGen_DeniesNonOwner`
func TestPolicyGen_CompilesAndPasses(t *testing.T)        // sandbox (as composegen/sandbox_test.go does): go vet + `go test -run 'TestPolicy' ./cmd/server` over the greenfield case → PASS
// nullgen/nullgen_test.go
func TestNullgen_SeedsNoPolicy(t *testing.T)              // emitted hooks.go contains neither "permitAll" nor "PolicyDecisionPoint"
```
- [ ] **Step 2: Run** `GOWORK=off go test ./composegen ./nullgen -run 'TestPolicyGen|TestNullgen_SeedsNoPolicy|TestEmitters_NoPermitAll'` — FAIL.
- [ ] **Step 3: Implement** `emit_policy.go`, the test body, `deriveHooks`, `writeEdgeSecurity`, `Generate`'s `out[PolicyFile]`, nullgen deletions, the fixture hooks.go edit. `grep -rn permitAll framework-go-app-generator` — Expected: nothing.
- [ ] **Step 4: Run** `GOWORK=off go test ./...` in app-generator (includes `TestNullgen_HooksCoverEverySeam` sandbox and `TestEmitters_NoPermitAll`) — PASS; `cd ../framework-go-conformance && GOWORK=off go test -tags conformance ./ -run 'TestConformance_RUN021Proof/(chain|vet)|TestMCP_OverWire_UsesRealPolicy|TestREST_OverWire_ErrorEnvelopeMatchesContract' -timeout 60m` — PASS (Task 3.3's two wire tests go green here).
- [ ] **Step 5: Commit** `git commit -m "feat(composegen): composition-root policy generated from the authorization table (policy.gen.go); PolicyDecisionPoint hook and nullgen permitAll deleted"`.

---

### Task 3.5: Arrange endpoint for web scenarios

**Files:**
- Modify: `framework-go-app-generator/configgen/inputs.go` (`ScenariohostURLEnv`), `emit.go` (`LocalScenariohostURL(profile)`); `composegen/emit_http.go:42-55` (`writeWebManagersBundle` adds `applyFaults`), `composegen/surfaces.go:113-132` (`writeRootMounts` mounts `/__scenariohost/`), create `composegen/emit_arrange_http.go`, `composegen/emit_faults.go` (`FaultsFile = "faults.gen.go"`, the per-Manager fault wrapper beside `null.gen.go`); tests `composegen/arrange_http_test.go`, `faults_test.go`
- Modify: `framework-go-app-generator/testgen/emit_playwright.go:159-205` (arrange: real calls; `advanceClock` refused), `tsruntime/runtime.ts:178-188` (`skipUntilScenarioHost` deleted; `arrange` performs the three host kinds via `POST /__scenariohost/<kind>`); `cmd/testgen/discriminate.go` (web runs through `webscenarios`); tests `testgen/emit_playwright_v2_test.go`, `cmd/testgen/discriminate_test.go`
- Create: `framework-go-scenariohost/arrangeserver.go`, `arrangeserver_test.go`, `cmd/webscenarios/main.go`, `cmd/webscenarios/main_test.go`; modify `host.go:108-180` (export `Boot(ctx) (*Host, error)` and `(*Host).Close()` over `boot`/`stopAll`), `arrange.go` (T-free cores `advanceClock(d) (undo func())`, `failNext(component, op, code) (undo func(), error)`, `seedResource` exists at `:145`)
- Modify: `method-assets/assets/scaffold/go.mod.tmpl:76-83` (tool `framework-go-scenariohost/cmd/webscenarios`), `Makefile.scenarios.mk.tmpl` (`surface-check` and a new `test-web` through `go tool webscenarios`)
- Create: `framework-go-conformance/arrange_test.go` (tag `conformance`)

**Interfaces (Produces):**
```go
// configgen
func ScenariohostURLEnv(prefix string) string                      // prefix + "_SCENARIOHOST_URL"
func (c *Config) LocalScenariohostURL(profile string) (string, error) // "" when unset; set under a profile other than "local" → error naming the variable
// composegen main.gen.go (local profile, URL set): root.Handle("/__scenariohost/", arrangeForwarder(cfg.ScenariohostURL)) — a reverse proxy of POST only, same path, 2 s timeout; absent URL → nothing mounted (405 as today)
// faults.gen.go: type fault<Iface> struct{ inner <pkg>.<Iface>; take func(component, op string) (string, bool) }; every op asks take("<contractKey>", "<Op>") first and returns the Manager's error kind for the code (the same error-code→kind table restWireCodes uses, inverted; unknown code → fwm.Infrastructure) — applyFaults(profile, url, managers) wraps each web-exposed Manager when url != "" (local only, like applyNullOps); take = GET <url>/faults/take?component=&op= → 200 {"errorCode"} | 204
// scenariohost
type ArrangeServer struct{ URL string; Close func() }
func (h *Host) ServeArrange(ctx context.Context) (*ArrangeServer, error) // POST /advanceClock {"duration"} → h.advanceClock; POST /failNext {"component","operation","errorCode"} → h.failNext; POST /seedResource {"resource","fixture"} → h.seedResource; GET /faults/take → h.TakeFault; 400 on a bad body, 500 carrying the error text
func Boot(ctx context.Context) (*Host, error)   // the T-free host (host.go boot), for cmd/webscenarios
func (h *Host) Close()                           // stopAll + Flush
// cmd/webscenarios: go tool webscenarios -C uitests -config generated/<key>/playwright.config.gen.ts [-grep <re>] [-project surface] [-module <dir>] [-- extra playwright args]
//   Boot → h.LaunchDevstack-equivalent over the T-free API (devstack.Launch with h.Env + ScenariohostURLEnv=arrange.URL + DevPrincipalEnv/DevPrincipalsEnv from the plan's principals of the web contract) → npx playwright test … with PLAYWRIGHT_TEST_BASE_URL=app.BaseURL, AIARCH_NULL_OPS and AIARCH_SCENARIO_TIMEOUT_MS passed through → Stop → Close; exit = playwright's exit; a leftover (DockerLeftovers) → exit 3 naming it
```
- `tsruntime/runtime.ts` `arrange`: `advanceClock`/`failNext`/`seedResource` → `fetch(baseURL + '/__scenariohost/' + call.kind, {method: 'POST', body: JSON.stringify(call)})`, status ≥ 400 → `expect.fail`; `skipUntilScenarioHost` and the emitted `test.skip` line (`emit_playwright.go:165-167`, `scenarioHostSkipReason`) are deleted; a binding arranging `advanceClock` makes `emitPlaywright` return `fmt.Errorf("testgen: %s scenario %q arranges advanceClock, which a booted server cannot honour yet (E17); arrange state through a contract call or seedResource instead", key, id)`.
- testgen `-discriminate` web half: for each web scenario and stimulus op, `go tool webscenarios -C <uitests> -config generated/<key>/playwright.config.gen.ts -grep '^<id>$'` with `AIARCH_NULL_OPS=<Iface>.<Op>`; a pass is a survivor, as for Go; `reasonWeb` deleted.

- [ ] **Step 1: Write the failing tests.**
```go
// composegen
func TestArrangeEndpoint_LocalOnly(t *testing.T)            // main.gen.go: the mount is inside `if url, err := cfg.LocalScenariohostURL(profile); …` and config.gen.go refuses the variable under "cloud" (text "honoured only under the local profile")
func TestFaults_WrapEveryWebManagerOp(t *testing.T)         // faults.gen.go has one method per op of every web-exposed Manager, each calling take(<key>, <Op>) first
// scenariohost
func TestArrangeServer_RoundTrip(t *testing.T)              // ServeArrange; POST /failNext then GET /faults/take → {"errorCode"}; second take → 204; POST /seedResource {"resource":"postgres","fixture":{"sql":"select 1"}} → 200 (skip under -short); bad JSON → 400
func TestWebscenarios_ParsesFlagsAndPassesEnv(t *testing.T) // cmd/webscenarios with a stubbed runner: argv/env contain PLAYWRIGHT_TEST_BASE_URL, APP_SCENARIOHOST_URL, AIARCH_NULL_OPS passthrough
// testgen
func TestPlaywright_ArrangeHostKindsCallTheEndpoint(t *testing.T) // spec has `await arrange(testInfo, { kind: 'failNext', … })` and no `test.skip(true, 'arrange`; runtime.gen.ts has no `skipUntilScenarioHost`
func TestPlaywright_AdvanceClockRefused(t *testing.T)        // Generate error names the scenario and "advanceClock"
// cmd/testgen
func TestDiscriminate_WebRunsThroughWebscenarios(t *testing.T) // the runner stub sees `go tool webscenarios … -grep ^<id>$` with AIARCH_NULL_OPS set
// conformance  //go:build conformance
func TestPlaywright_ArrangeFailNextRoundTrip(t *testing.T)   // built fixture + a one-off web binding (in-test copy of the fixture: arrange failNext{listManager, ListAddTodo, Infrastructure} before add-a-todo-P1) run through `go tool webscenarios -grep add-a-todo-P1`: the scenario FAILS at the add step with the Infrastructure refusal visible (proves the fault reached the booted server), and the unmodified fixture's add-a-todo-P1 PASSES
```
- [ ] **Step 2: Run** `GOWORK=off go test ./composegen ./testgen ./cmd/testgen -run 'TestArrangeEndpoint|TestFaults_|TestPlaywright_Arrange|TestPlaywright_AdvanceClockRefused|TestDiscriminate_WebRuns'` (app-generator), `GOWORK=off go test ./ ./cmd/webscenarios -run 'TestArrangeServer|TestWebscenarios'` (scenariohost) — FAIL.
- [ ] **Step 3: Implement** in the order configgen → composegen (forwarder, faults) → scenariohost (T-free cores, `ServeArrange`, `Boot`/`Close`, `cmd/webscenarios`) → testgen/runtime.ts → cmd/testgen → method-assets templates (`go.mod.tmpl` tool line; `surface-check: go tool webscenarios -C uitests -config … -project surface` per config; `test-web`). `cd framework-web && npx tsc --noEmit -p ../framework-go-app-generator/testgen/tsruntime` (or the tsconfig the Plan 2 Task 2.10 step used) — PASS.
- [ ] **Step 4: Run** the units above — PASS; `cd framework-go-conformance && GOWORK=off go test -tags conformance ./ -run 'TestPlaywright_ArrangeFailNextRoundTrip' -timeout 40m` — PASS (needs npm, chromium, docker, temporal CLI); `golangci-lint run ./...` in app-generator and scenariohost — 0.
- [ ] **Step 5: Commit** `git commit -m "feat(scenariohost,composegen,testgen): web scenarios run under the scenario host (webscenarios); local /__scenariohost arrange endpoint with fault wrapper; advanceClock refused for web"`.

---

### Task 3.6: App wiring, release, proof

**Platform release (literal release step):** at the fpc/main commit holding 3.0–3.5 tag `framework-go/v0.23.0`, `framework-go-http-generator/v0.6.0`, `framework-go-app-generator/v0.25.0`, `framework-go-scenariohost/v0.6.0`, `method-assets/v0.25.0` (its `go.mod.tmpl`/`go.sum.tmpl` pin app-generator v0.25.0 and scenariohost v0.6.0 first; re-tag at that commit as 2.11i did), `framework-go-conformance/v0.2.0`; `git push origin fpc/main --tags`. If the push is refused record `git push origin fpc/main --tags` under blockedActions; the lead pushes. Release gate: Platform Checks green including `TestConformance_(Chain|Boot|BootTwice_NoOrphans)` and `TestConformance_RUN021Proof` plus the new conformance tests; add `TestDevstackLaunch_|TestMCP_OverWire|TestREST_OverWire|TestPlaywright_ArrangeFailNextRoundTrip|TestConformance_ClientsOverWire` to `.github/workflows/platform-checks.yml:102` run regex.

**Files (app):**
- `server/go.mod`: pins → framework-go v0.23.0, framework-go-app-generator v0.25.0, framework-go-scenariohost v0.6.0, method-assets v0.25.0, framework-go-conformance v0.2.0, http-generator v0.6.0 (indirect, by tidy); `tool` directive adds `framework-go-scenariohost/cmd/webscenarios`; `GOWORK=off go mod tidy`.
- `.aiarch/state/project.json`: `$defs.TestPrincipal.properties.intents` (`{"type":"array","items":{"type":"string"},"description":"…read only with onBehalfOf"}`); `make gen-models` regenerates `contract.gen.go`.
- `server/internal/resourceaccess/projectstate/projectstateaccess.go`: Task 3.2's `CheckKindSmoke`, `compositionChecks`, `frontendScopeAndChecks`, deployment integration, `checkKindOrder`; `discriminateTimeoutSec` (`:13184-13203`) counts client contracts too (drop the `client` `continue`) and adds one `DevstackBootSec` (120) per client run; `TaskSpecSchemaVersion` = 5.
- `server/cmd/aiarch-state-mcp/checks.go`: `runSmoke` (Task 3.2); `newScenarioBatch` web arm (`:309-313`) → `argv = ["go", "tool", "webscenarios", "-C", "uitests", "-config", "generated/<contract>/playwright.config.gen.ts", "-grep", webScenarioGrep(ids)]`, `dir = proj.ModuleDir`, env `commandEnv("go", "AIARCH_SCENARIO_TIMEOUT_MS=…")`; `surface-check` command (`projectstateaccess.go:13150-13154`) → `["go", "tool", "webscenarios", "-C", "uitests", "-config", …, "-project", "surface"]` in `p.ModuleDir`.
- `Makefile.scenarios.mk` (app root): re-seated from the released template (`make claude-assets` or the seat verb; `TestSeatedWorkflowsEqualTheTemplates`-style test for the Makefile if one exists — `grep -rn "Makefile.scenarios" server/internal/*_test.go`).
- `server/cmd/aiarch-state-mcp/freshcomposition_test.go`: `requireDelegationClaim`/`scenariohostMapsDelegations`/`delegationClaimOwedUntil` (`:462-512`) deleted; `requireClientScenarioVerdicts` runs unconditionally; `:289` wants list adds `"smoke:composition"`; `requireMachineClientGate` asserts the scenario verdicts pass over the wire (the generated file contains `scenariohost.MCPSession(` / `RESTClient(`).
- `method-assets` prompts (platform, in the release above): `service-integration.md:23` and `frontend-integration.md:24` drop "announces web and machine-client scenarios as skipped until the booted-server run lands"; state that the integration gate boots the composed app (`smoke:composition`) and that client scenarios run over the wire as the plan's principals `[TP-PRINCIPAL]`.
- `lifecycles.json`: unchanged — every lifecycle's `integration` task keeps its id and edges (`service`, `frontend`, `machineClient`, `deployment`, `integration`); the check set is the app's derivation, not the graph.

**Proof (platform, `framework-go-conformance/run021_test.go` + new subtests, tag `conformance`):**
```go
func TestConformance_ClientsOverWire(t *testing.T) // built fixture with impl (FixtureImpl): for every todoApi (8) and todoMcpServer (20) scenario, `go test -run ^TestScenario_<id>$ ./internal/client/...` passes (results.json verdicts pass, keyed under the contract); `go tool testgen -discriminate` over the built dir reports 0 survivors with client scenarios counted (scenarios == 78 + 28); DockerLeftovers(before, after) empty after the whole run
func TestConformance_Discriminate_Web(t *testing.T) // the web half of -discriminate over todo-web-app runs (summary line counts its scenarios; "skipped … plan 3" lines absent) and reports 0 survivors
```

- [ ] **Step 1: Release** the platform as above (tags local; push or record). `cd framework-go-conformance && GOWORK=off go test -tags conformance ./ -run 'TestConformance_ClientsOverWire|TestConformance_Discriminate_Web' -timeout 100m` — PASS before tagging.
- [ ] **Step 2: Bump pins + tidy;** `cd <app>/server && GOWORK=off go build ./... && GOWORK=off go vet ./...`.
- [ ] **Step 3: App edits** (schema → `make gen-models`; checks; batch argv; Makefile re-seat; tests). `make gen-tests` regenerates the own machine-client scenario files (`mcpClient`'s `client_scenarios.gen_test.go` now launches devstack: the own project's local profile must boot — if it does not, the scenario is a dated skip naming the earmark, never a silent pass; record the count in the commit body).
- [ ] **Step 4: Run** `GOWORK=off go test -count=1 -p 1 ./internal/resourceaccess/projectstate ./cmd/aiarch-state-mcp` — PASS (`TestFreshManagerIntegrationComposesTheServer`, `TestMachineClientsIntegrateOverTheManagersRealShapes` with `requireClientScenarioVerdicts` unconditional, `TestIntegrationChecks_IncludeSmokeForEveryType`, `TestRunSmokeFailsOnLeftovers`); then the plan's full suite once: `cd server && GOWORK=off go test -p 1 -timeout 120m ./...`, `make method-check`, `make gen-tests-check`, `make gen-models-check`, `make test` — all green; `grep -rn permitAll /Users/davidmarne/mixofrealitystudio/fpcwt/archistrator-platform/framework-go-app-generator` — empty.
- [ ] **Step 5: Commit** (app) `git commit -m "feat(app): smoke:composition on every integration; web scenarios and surface-check run under the scenario host; client verdicts over the wire; pins Plan 3 release"`.

**Earmarks (not in this plan):** E17 a model-declared clock for the composition root so `advanceClock` reaches a booted server (D7). E18 one devstack boot shared by a package's client scenarios (today one per `go test` run, budgeted by `DevstackBootSec`); together with Plan 2's E16 null-run speed-up. E19 the own project's `mcpClient` scenarios need the own local profile to boot under the host (E3's agenticJobAccess arm). E20 `scenariocompose` still emits nothing for clients; a registry of unused generated symbols belongs to Plan 6's manifests.

## Done when
Conformance green with over-the-wire clients; app `make test` green; `grep -rn permitAll archistrator-platform/framework-go-app-generator` empty.
