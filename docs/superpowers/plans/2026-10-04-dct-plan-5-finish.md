# Deterministic Component Testing — Plan 5: Finish

> Executed by the Workflow tool. Every task: implement → adversarial review → up to 2 fix rounds.

**Goal:** Finish the wave: `TestFileLayout = 0`, `TP-BOUND = 0`, `TP-OP-REACHED = 0` (now an Error, framework-go v0.15.0), `gen-tests-check` clean, every scenario test and generated UI test green, one recorded `TestRun` per component, on branch `deterministic-component-testing` (`../archistrator-dct` @ `6e458842`).

**Spec:** `docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md` §11 (Amendment A) and §12 (Amendment B — this plan's rulings). Input report: `../archistrator-dct/docs/bugs/2026-10-03-dct-plan4-report.md`.

## Global Constraints

- All Plan 1–4 constraints (explicit-path staging; platform commits authored as the founder; main tree read-only; never weaken a gate; no `t.Skip`, no `//nolint`, no lint-config exemptions).
- Breaking changes are fine; delete, don't deprecate; every schema change also conforms archistrator's own `project.json` (B11).
- macOS has no `timeout`: bound long commands with `perl -e 'alarm shift; exec @ARGV' 900 <cmd>`.
- Docker must answer `docker network ls` before any construct agent runs; the workflow preflights it.
- Already merged green on the branch and **not** redone: engine/autoscaler, engine/estimation, engine/operationestimation, engine/review.

## Tasks

**Platform (one release: framework-go v0.16.0, app-generator v0.12.0, scenariohost v0.2.0)**
- **P1 testgen lint-clean hook names (B3):** e.g. `stepUC3P2S1` / `probeUC3P2S1N2`; drift check and hooks-stub emitter use the same function; golden updated.
- **P2 scenariohost results keyed by `(component, scenario)`:** facets in one package (projectstate) no longer overwrite each other; `results.json` per component.
- **P3 scenariohost replay gate (B4):** the host records every workflow history started during a scenario and replays it with `worker.NewWorkflowReplayer` against the registered workflows at `Flush`; a non-determinism error marks the scenario `fail` with the history id. Generated `TestMain` for manager packages registers workflows with the host.
- **P4 hooks import rule (B2):** `arch.Spec` admits the module's own layer-leaf packages (public API only: no `internal/` sub-packages of a component, no `_test` helpers). Fixture tests for allowed / forbidden.
- **P5 client dialects (B1):** testgen emits Playwright for `webClient` ops (param `action`) and in-process MCP calls for `mcpClient` ops (one op per tool; transport = the generated tools server started in-process).

**Archistrator (model first, single writer)**
- **D1 re-pin + repair:** framework-go v0.16.0, app-generator v0.12.0, scenariohost v0.2.0, method-assets v0.10.2; `TestRepoStructureCmdIsClosed` (register or delete `server/cmd/record-testplan`); `make gen`.
- **D2 un-stub billingStateAccess + merchantGatewayAccess (B5):** Postgres stores, infra `["Postgres"]`, per-profile binding arms (local = same Postgres), migrations, `main.gen.go` regen; customer→project link for `ReadPersistentlyDelinquentCustomers`. Fixes the red billingstate merge forward.
- **D3 model edits:** B1 client ops (web actions named from the use cases + `UIIdentifiers`, MCP ops one per tool), A7-1..5 (B6, incl. attempt-suffixed idempotency key), D-1 deletions (B10), B9 revert of `in_flight=true`, B8 confirmed (no change). Conform own `project.json`; `make gen`; `make method-check` green (TP-OP-REACHED 0 as Error).
- **D4 SPA screens (B7):** build SPA-1..8 from the report against the UI design; `data-testid`s registered in `UIIdentifiers.ts`.

**Bind → construct → verify → integrate**
- Re-bind every component whose plan has TP-BOUND errors or is missing (binders read-only, parallel; one writer per component, sequential).
- Construct every unit except the 4 already green; Docker preflight first; verify adversarially (reject zero-scenario, nolint, white-box, gate-weakening); integrate serially; record one `TestRun` per component after integration.
- Final gates as in Goal; report `docs/bugs/2026-10-04-dct-plan5-report.md`. Merge to main stays manual (coordinate with session 6b).
