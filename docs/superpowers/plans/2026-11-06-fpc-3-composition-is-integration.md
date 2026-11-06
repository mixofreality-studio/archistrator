# FPC Plan 3 — The Composed App Is the Unit of Integration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Task step detail is filled in from Plan 2's final signatures before execution (see index).

**Goal:** No activity reaches `integrated` unless the whole composed app boots through its generated run contract, and every machine-client scenario runs over the wire through the real composition-root policy.

**Architecture:** scenariohost owns the one substrate set and launches the generated `devstack` into it (spec §4b). The integration check set replaces compile-only composition checks with a boot smoke (`/readyz`, SPA index, `/mcp` initialize) and teardown. REST/MCP emitters stop serving handlers in-process and call the booted server with the principal from the plan. The composition-root authorization policy is generated from the model, so it has an owner and sits in generated code.

**Spec:** §W5; §4b "One substrate set"; SA-1 P4 ruling. **Depends on:** Plans 1–2.

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

### Task 3.1: scenariohost launches devstack into its substrates
**Files:** `framework-go-scenariohost/devstack.go` (new), `host.go`; `framework-go-app-generator/rungen/runtime.go` (env contract).
**Interfaces (Produces):**
```go
func (h *Host) LaunchDevstack(t *testing.T, moduleDir string) *App   // `go run ./cmd/devstack` with env: <PREFIX>_POSTGRES_URL=h.PostgresURL, <PREFIX>_TEMPORAL_HOSTPORT=h.TemporalHostPort, <PREFIX>_LISTEN=127.0.0.1:0 (port reported on stdout line "LISTENING <addr>"), <PREFIX>_DEV_PRINCIPAL from the plan principal; process group; Stop on t.Cleanup
type App struct { BaseURL string; MCPURL string; Stop func() error }
```
- rungen: when `<PREFIX>_POSTGRES_URL`/`_TEMPORAL_HOSTPORT` are set, devstack does **not** start its own containers; it prints `LISTENING <addr>` once `/readyz` is 200. Tests: `TestDevstackLaunch_UsesHostSubstrates`, `_ParallelIsolated` (two launches, distinct ports, same Postgres with distinct schemas via `<PREFIX>_SCHEMA=<scenario id>`), `_StopKillsProcessGroup`.

### Task 3.2: Boot smoke on every integration
**Files:** app `projectstateaccess.go:12896-12935` (`compositionChecks`), `cmd/aiarch-state-mcp/checks.go` (new check kind `smoke`); platform `scenariohost` `Smoke(app *App) error`.
**Interfaces:** check id `smoke:composition` of `Kind: "smoke"`: `go build ./cmd/server ./cmd/devstack` → `LaunchDevstack` → `GET /readyz` 200 → `GET /` contains `<html` → MCP `initialize` + `tools/list` with every generated tool name present (from `mcpemit`'s tool table) → `Stop` → `DockerLeftovers` empty. Replaces `command:build-server`/`build-devstack` (keep as sub-steps of smoke). Added to **every** lifecycle's integration task (service, frontend, machineClient, deployment). Tests: `TestSmoke_MCPListsEveryGeneratedTool`, `TestSmoke_FailsWhenSPAMissing`, `TestIntegrationChecks_IncludeSmokeForEveryType`.

### Task 3.3: Machine clients over the wire
**Files:** `testgen/emit_mcp.go`, `emit_rest.go`; `scenariohost` (`MCPSession(app)`, `RESTClient(app, principal)`).
**Interfaces:** generated client scenario = `app := h.LaunchDevstack(t, moduleDir)`; `subject := scenariohost.MCPSession(t, app, principal)` / `scenariohost.RESTClient(t, app, principal)`; steps call over the wire; probes over the wire; null-subject via `AIARCH_NULL_OPS` passed to the launched process (Plan 2 Task 2.9 middleware). Delete `toolServer`/`serveTools`/`permitAll` strings. Tests: `TestMCP_OverWire_UsesRealPolicy` (a principal without the role gets the contract's refusal code), `TestREST_OverWire_ErrorEnvelopeMatchesContract`, `TestEmitters_NoPermitAll` (`grep`-style assertion over all emitter sources).

### Task 3.4: Composition-root policy generated from the model
**Files:** `composegen/authz.go` (extend), new `composegen/emit_policy.go`; model: `operationalConcepts`/operating model gains `authorization: { ownerScopes: [{resource, ownerField}] }` typed (`$defs` in app, mirrored in projectmodel `Deployment`/`System`).
**Interfaces:** `policy.gen.go` implements `security.PolicyDecisionPoint` from `authzRows` + ownerScopes: permit when principal subject == resource owner for `ownerScope` resources, deny otherwise; `hooks.go` seam `PolicyDecisionPoint()` seed returns `generatedPolicy{}` and is no longer FILL. `TestPolicyAdmitsEveryGeneratedRequest` extends to **every principal role the plan declares** (`TestPolicyGen_AdmitsEveryGeneratedRequestForOwner`, `_DeniesNonOwner`). Edit scope: `cmd/server/policy.gen.go` is generated (in `compositionPaths`).

### Task 3.5: Arrange endpoint for web scenarios
**Files:** `composegen/emit_http.go` (local-profile-only mount `/__scenariohost/{advanceClock,failNext,seedResource}` forwarding to scenariohost via `<PREFIX>_SCENARIOHOST_URL`), `scenariohost/arrangeserver.go` (tiny HTTP server exposing Task 2.7 primitives), `testgen/tsruntime/runtime.ts` (replace the `test.skip` reasons from Plan 2 with real calls). Tests: `TestArrangeEndpoint_LocalOnly`, `TestPlaywright_ArrangeFailNextRoundTrip`.

### Task 3.6: App wiring, release, proof
- App: new check kind in `checks.go` dispatch; `compositionChecks` rewrite; `integration` tasks of every lifecycle in `lifecycles.json` keep ids (no graph change). Release scenariohost + app-generator + method-assets; pin.
- Proof (conformance, tag `conformance`): `TestConformance_ClientsOverWire` — todomvc fixture: all `todo-api` and `todo-mcp-server` scenarios pass over the wire under the generated policy; `DockerLeftovers` empty after the run; `TestConformance_Discriminate_Web` now runs (Plan 2's skipped half).

## Done when
Conformance green with over-the-wire clients; app `make test` green; `grep -rn permitAll archistrator-platform/framework-go-app-generator` empty.
