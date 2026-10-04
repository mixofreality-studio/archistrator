# Deterministic Component Testing — Plan 4: Coverage (Amendment A)

> **For agentic workers:** Executed by the Workflow tool (founder: "do it"). Tasks carry the same implement → adversarial review → fix loop as Plans 1–2.

**Goal:** Make every component receive real scenarios (Amendment A1–A4), make every API reachable from a use case or delete it (A5), restore coverage by scenarios in all remaining packages, and bring `deterministic-component-testing` to `TestFileLayout = 0` with every scenario green.

**Architecture:** One framework-go release fixes the derivation; archistrator re-pins; a system-architect pass closes TP-OP-REACHED per contract (use case or deletion); then the Plan 3 bind → construct → verify → integrate pipeline re-runs over all 28 contracts.

**Spec:** `docs/superpowers/specs/2026-10-02-deterministic-component-testing-design.md` §11. Branch `deterministic-component-testing` in `../archistrator-dct` @ `2fb5657f` (Plan 2 merged, 7 Plan-3 units merged, report committed). Platform main @ `0421c78d`.

## Global Constraints

- Everything from Plans 1–3 Global Constraints (explicit-path staging; founder author identity on the platform repo; main tree read-only; never weaken a gate; no `t.Skip`).
- Breaking changes are allowed (no production usage). Delete, don't deprecate.
- Re-pin: `method-assets v0.10.1` (the branch is on v0.10.0), `framework-go v0.13.0` once Task 1 releases.
- A4 adds `Via []string` to `scenario.InputEv` (JSON `via`), A1 adds `Kind:"call"`; `methodcheck` and `testgen` accept both without change.
- The `start`-drop rule (A3) is implemented in `scenario.Derive`, not in adapters.

## Review Focus

1. A step with **three calls** (`manager→RA`, `manager→engine`, `RA→resource`) must yield exactly two stimuli (resource is not a component) in call order — pinned by Task 1's golden on `execute-a-project-activity` (`dispatch-task`: sourceControlAccess ×2 ops, agenticJobAccess ×1).
2. A label listing two ops `storeConstructionOutput / retrieveConstructionOutput — …` must yield two stimuli with ops resolved — Task 1 golden (`observe-validate`).
3. The `start` family of `execute-a-project-activity` (P1–P7 today) must disappear and `UC-START-REDUNDANT` must fire once — Task 1 + Task 2.
4. An `alt=s1/s2` pair (web vs MCP) must yield one deliveryManager stimulus with `via:["web-client","mcp-client"]` and one stimulus each for `webClient` and `mcpClient` — Task 1 golden (`human-decision`).
5. After Task 4, `TP-OP-REACHED` must be **0** on the branch; the integrator asserts it before merging anything.

---

### Task 1 (platform): `scenario` Amendment A + release framework-go v0.13.0

**Files:** `P/framework-go/scenario/{derive.go,classify.go,paths.go,scenario.go}`, `derive_test.go`, `testdata/execute_activity.json` + golden (copy the real diagram + view from the branch's `project.json` — nodes/edges/view/contract op names only), `P/framework-go/methodcheck/rules_usecase_io.go` (+`UC-START-REDUNDANT`), `scenario_adapter.go` unchanged.

- Stimulus per `TraceCall` into a component on every path node (A1); `InputEv{Kind:"call", From:<component>}` for component callers; guard of decision nodes carried on that node's stimuli; `Via` (A4); `resolveOps(label) []string` (A2) with unit tests on the five label shapes in the golden; A3 drop + finding. Keep `ForComponent` semantics; `Seq` renumbered per projection.
- Golden assertions: for the use case, reviewEngine ≥ 1, interventionEngine ≥ 1, sourceControlAccess ≥ 1, agenticJobAccess ≥ 1, `projectStateAccess.readProject` resolved, no scenario with zero stimuli, deliveryManager `submitReviewDecision` present with `via` of length 2.
- Release: tag `framework-go/v0.13.0`, push, proxy-verify, CI green.

### Task 2 (archistrator): re-pin + re-measure

**Files:** `server/go.mod` (framework-go v0.13.0, method-assets v0.10.1), `go.sum`; `docs/bugs/2026-10-03-dct-coverage-baseline.md` (TP-OP-REACHED per contract, UC-START-REDUNDANT, scenarios per component from `ComponentScenarios`).
- `make gen && gen-*-check` clean; `go test ./... -short` green except `TestFileLayout` (13).

### Task 3 (archistrator): delete `constructionTransitionAccess` and the other dead-by-ruling contracts

**Files:** model edit (`$defs`/`serviceContracts`/system slot component + relationships + dynamic-view calls), `server/internal/resourceaccess/projectstate/*` (its ops/impl), `make gen`, any caller. Same for `revenueLedgerAccess` (permanent no-op) unless Task 2's baseline shows a dynamic view calling it. Commit "model(dct4): delete constructionTransitionAccess (+revenueLedgerAccess)".

### Task 4 (archistrator, fan-out): every API has a use case

Per contract with ≥1 unreached op after Task 3 (from the Task 2 baseline), a **system-architect** agent returns a patch (schema): for each op → either `{useCase: <existing id>, node: <existing or new node>, call: {from, to, label starting with the op name}}` (extending a dynamic view / diagram) or `{newUseCase: {id, name, actors, trigger, diagram, dynamicView}}` or `{delete: op, why}`. One **applier** writes all patches through `putDraftModel`/`publishDraft` (slot 4 + slot 5) and the model edit for deletions, runs `make method-check`, and loops until `TP-OP-REACHED == 0` (max 3 rounds; leftovers reported). It also retypes actor-lane output actions to `sendSignal` where the architect patch says so.

### Task 5 (archistrator): unbindables as design changes

From the migration report §2.5: `billingManager` gains a read-only query op (`GetBillingCycle`), `replanProject` moves onto `deliveryManager`'s interface (or the use case's call is corrected to the op that exists), `projectStateAccess` facet routing fixed in the adapter (calls land on the contract that owns the op — resolve by op name across the five projectstate contracts), `design-health-engine` gets a service contract (`EvaluateDesignHealth`), webClient `replan-P3` either gains a defer control in the UI design or the use case drops the branch. Model edit + `make gen` + stubs compile.

### Task 6 (archistrator, pipeline): bind → construct → verify → integrate, all 28 contracts

Same stages as Plan 3 §Task 2 with these changes: binding agents bind the **full** projected set (no empty plans accepted — an empty plan for a component with ≥1 scenario is a rejected result); construct units = Go packages + `client:webClient` + `client:mcpClient` (package `internal/client/mcp`, black-box over the MCP transport using the generated tools file); the integrator rejects any unit that purges tests while adding zero `TestScenario_*`; final gates: `TestFileLayout = 0`, `gen-tests-check` clean, `TP-OP-REACHED = 0`, all scenario tests green, `npm run test:scenarios` green, every component has a recorded `TestRun`.

### Task 7: report + merge checklist

`docs/bugs/2026-10-03-dct-plan4-report.md`; the branch is merge-ready when Task 6's gates hold. Merge to main stays manual (coordinate with session 6b).

### Task 8: Every API has a use case — permanent gate for every project (founder, 2026-10-04)

Plan 1 made `TP-OP-REACHED` a Warning, so `methodcheck.Check` only logs it and no other project is ever blocked. Make the A5 ruling enforced everywhere:

- **framework-go/methodcheck:** `TP-OP-REACHED` → `SeverityError`, with the message naming the contract, the op, and the fix ("add the op's call to a use case's dynamic view, or delete the op"). It runs in `ValidateProject`, so it is enforced (1) by `methodcheck.Check` in every app repo (scaffolded `aiarch_method_test.go` → `go-checks.yml` → `go test ./...`), (2) by the aiarch-state MCP write path, which validates with methodcheck: `recordServiceContract` / `putDraftModel` / `recordPhaseArtifact` reject a write that leaves an op unreached, and (3) by design health in the webApp. Tests: a fixture project with one unreached op → exactly one Error finding; with that op called from a dynamic view → none; `Check` fails the test on it.
- **Ordering edge:** a project whose slot 4 / slot 5 is not yet committed has no scenarios at all; the rule must not fire until both are committed (otherwise every contract written before architecture would be rejected). Test that case.
- **method-assets:** the `the-method-service-contract` skill and `service-detailed-design` command state the rule ("every operation you add must be called by a use case's dynamic view; add the call or don't add the op").
- Release `framework-go/v0.15.0` and `method-assets/v0.10.2`; after the Plan 4 workflow's integrate stage, bump archistrator's pins and confirm `make method-check` stays green (the branch is at 0 unreached).
