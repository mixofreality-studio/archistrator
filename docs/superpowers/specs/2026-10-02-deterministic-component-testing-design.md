# Deterministic Component Testing — Design

**Date:** 2026-10-02 · **Status:** founder-approved design (in chat); spec for review
**Source technique:** Kim, Kang, Baik, Ko — *Test Cases Generation from UML Activity Diagrams* (SNPD 2007): I/O-explicit activity diagram (IOAD), single-stimulus principle, elementary-path coverage.

## 1. Thesis

Every test an archistrator-built app runs is **derived deterministically from the use-case activity diagrams already committed in `project.json`**, bound to concrete values by an agent, generated into black-box integration tests, run in the construction venue, and rendered on the activity's testing task. There is no system test plan and no system testing activity: each component carries its own plan, and the union of passing component plans is the proof that the app works.

### Rulings (founder, 2026-10-02)

| # | Ruling |
|---|---|
| T1 | All test scenarios are deterministic, derived per the IOAD technique from the use cases. A component's plan contains every scenario in which some stimulus is an input to that component. |
| T2 | Those are the **only** tests permitted in archistrator-built code (managers, engines, resource access, clients, the UI app). Any white-box or hand-written test made during construction is removed before merge, and a check enforces it. The tests are black-box **integration** tests against the real downstream stack (Temporal test server, Postgres testcontainer, Gitea for GitHub, …). |
| T3 | Platform repos keep their own tests. The platform's layer/arch gates are exempt from T2 and **must run on every archistrator-built repo**. |
| T4 | Test results appear in archistrator on the construction activity's **testing** task (after integration). The task is ready for review only when every scenario test passed **and** its results render: Playwright video + trace for UI tests, stdout/stderr for server tests, fetched from the venue's run artifacts. |
| T5 | N-STP (System Test Plan) and N-IT (System Testing) are removed. |
| T6 | The plan is as declarative as possible (JSON in `project.json`) so archistrator can render every case and emit stubs for any language. Code is generated only for the parts an agent must fill. |
| T7 | Platform changes are released and pushed in this wave (framework-go, app-generator, method-assets, infrastructure-github). |
| T8 | Scenarios are **derived on read**, never stored; only bindings are committed (keyed by stable scenario id). |

### Stands

- The `test_plan` lifecycle step keeps its id and the test-engineer worker class; its content changes.
- The `testing` review task inside the `integration` phase keeps its id; its gate changes.
- N-PERF and N-QA are untouched.
- `cmd/` tooling tests and utility tests in archistrator are not component packages and are untouched.

## 2. Scenario derivation (deterministic)

Lives in **framework-go** as package `scenario` (`framework-go/scenario`), shared by the server (render), `methodcheck` (rules) and `app-generator` (emit). Pure function of `Project`.

### 2.1 Inputs

- `.coreUseCases[*].activity` — `ActivityDiagram{Nodes []ActivityNode, Edges []ActivityEdge}`; node kinds start/action/decision/merge/fork/join/end/swimLane/note/loop/switch/goto/interruptEdge/timeEvent/acceptEvent; edges controlFlow / guardedFlow with `Guard`.
- `.systemDesign` dynamic views — `DynamicView{UseCaseID, Steps []CallStep}`, `CallStep{ActivityNodeID, Calls []TraceCall{From,To,Mode,Label,Alt}}`.
- `.serviceContracts[component].interface` — operations with param/result JSON Schema.

### 2.2 IOAD reduction

1. Drop `note` and `swimLane` nodes (and `goto` after resolving it to its target edge).
2. An action node is **external** when some `CallStep` for that node has a `TraceCall` whose `From` is an actor (`LinkedActorID` set, or `From` not a component) → an **accept event** (input), or whose `To` is an actor / external resource → a **send signal** (output). All other action nodes are internal and collapse into the adjacent edge.
3. `acceptEvent` and `timeEvent` nodes are inputs by definition.
4. Decision/switch out-edges keep their `Guard`; loop nodes are unrolled **once** (elementary path: no repeated node).
5. Fork/join: by the single-stimulus principle, interleavings of outputs are ignored and inputs are serialised in diagram order. One path per join, not `n!`.

### 2.3 Paths

Depth-first from `start` to each `end`; a path may not revisit a node. Each path is a **Scenario**:

```json
{
  "id": "UC3-P2",
  "useCase": "UC3",
  "title": "Order accepted → invoice paid",
  "path": ["n1","n4","n7","n9"],
  "guards": [{"at":"n2","guard":"[Order Accepted]"}],
  "stimuli": [
    {"seq":1,"node":"n1","input":{"from":"actor:customer","to":"orderManager","op":"PlaceOrder","call":"UC3/step-1/call-0"},
     "expectedOutputs":[{"to":"actor:customer","op":"","kind":"signal","label":"Send Invoice"}]}
  ]
}
```

`id` = `<useCaseId>-P<n>` where `n` is the index of the path under a canonical ordering (edges sorted by `to` node id, then guard). Renaming a node or edge therefore changes ids; adding a branch at the end does not shift earlier ones more than the ordering implies. Ids are shown to users and are the binding key.

### 2.4 Component projection

`scenario.ForComponent(project, componentId)` returns every scenario with ≥1 stimulus whose `input.to == componentId`, with `stimuli` filtered to those. The projected stimulus carries the contract op and its param/result schemas, so a binding editor and an emitter need no further lookup.

**Expected outputs are black-box observables only:** the op's result or error (`TestExpect`), plus optional **probes** — read-only contract ops of any component in the same repo invoked after the stimulus. Downstream calls are *not* asserted (the stack is real).

### 2.5 Determinism guarantees

- Same `project.json` → byte-identical scenario set (sorted, canonical JSON).
- No clock, no randomness, no file I/O in the package.
- The package ships a golden test in framework-go against a fixture project (platform test, exempt by T3).

## 3. The per-component test plan (what is committed)

`.phaseArtifacts.testPlan[<component>]` replaces the free-text `TestPlanRecord`:

```go
type TestPlanRecord struct {
    Component  string
    Bindings   []ScenarioBinding   // one per projected scenario id
    AuthoredAt *time.Time
}
type ScenarioBinding struct {
    Scenario string            // "UC3-P2"
    Steps    []StepBinding     // one per projected stimulus, by seq
    Skip     *SkipReason       // nil normally; see §3.1
}
type StepBinding struct {
    Seq     int
    Inputs  []TestArg          // existing {Name,Value,SchemaRef}
    Expect  TestExpect         // existing {Result,ErrorExpected,ErrorCode}
    Probes  []Probe            // {Component,Operation,Inputs,Expect}
    Hook    bool               // true ⇒ the generated step delegates arrange/assert to the hooks file
}
```

These types move into `$defs` of `projectStateAccess` so modelgen owns them (today's hand-written `TestArg`/`TestExpect`/`TestStep` in `projectstateaccess.go:5252-5370` are migrated; `TestCase`/`TestScenario`/`SystemTestPlan` are deleted).

For the UI app, `Inputs` are user actions on the design's screens: `{Name:"action", Value:{"kind":"click|fill|navigate", "target":"<ui-design element id>", "value":...}}` and `Expect.Result` is an observable (`{"visible":"<element id>"}` / `{"text":...}` / `{"url":...}`).

### 3.1 Skips

A scenario may be bound `Skip: {Reason, Until}` only when the stimulus's upstream (another activity) is not yet integrated. Skips are counted by the gate (§6) and a skipped scenario is **not** a pass. There is no other skip.

### 3.2 MCP verbs

- `recordPhaseArtifact` with kind `testPlan` accepts the typed record (codec-validated; `methodcheck` rules of §5.2 run).
- `recordTestingState` loses `systemTestPlan`; gains nothing (test runs are written by the venue, §6).
- A new read verb `listComponentScenarios(component)` returns `scenario.ForComponent` so the test-engineer agent never re-derives paths.

## 4. Generation

### 4.1 `testgen` (app-generator)

Input: `project.json`. For each hand-built component with a committed `testPlan`:

| Target | Generated (never edited) | Hand-filled hooks (committed) |
|---|---|---|
| Go component `<pkg>` | `<pkg>/<stereotype>_scenarios_test.gen.go`, `package <pkg>_test` | `<pkg>/<stereotype>_hooks_test.go`, `package <pkg>_test` |
| UI app `<client>` | `uitests/generated/<client>/<scenario>.spec.gen.ts` | `uitests/generated/<client>/hooks.ts` |

Generated Go file: one `TestScenario_<id>` per binding; a shared `TestMain` that boots the stack through a **platform-provided harness** (`framework-go/testinfra/scenariohost`: Temporal dev server, Postgres testcontainer, Gitea, the component's own compose profile) and resolves the component through its public constructor from `main.gen.go`'s wiring. Each step: marshal `Inputs` to the op's param type → call → assert `Expect` → run probes. When `Hook` is true the step calls `hooks.Step_<scenarioId>_<seq>(t, host, in) (out, err)`.

Generated hooks file (emitted **once**, then owned by the agent; `gen-check` only verifies that every referenced hook symbol exists): one stub per hooked step with a `// FILL:` comment and the step's JSON inline.

Generated Playwright spec: `test('<id>')` that logs in via the harness, performs actions by `data-testid` derived from the ui-design element id, asserts observables; `video: 'on'`, `trace: 'on'` in the generated config. Hooks file exposes `before(scenarioId)` / `step(scenarioId, seq, page)` for `Hook: true` steps.

Emitters are per-language functions over the **projected scenario + binding JSON**; nothing in the Go emitter is needed by the TS emitter.

### 4.2 Output of a run

The generated `TestMain` writes `test-results/<component>/results.json` = `[{scenario, status: pass|fail|skip, durationMs, stdoutRef, stderrRef}]` plus the raw `go test -json` stream. Playwright's reporter is configured to write `test-results/<client>/results.json` in the same shape with `videoRef` / `traceRef`. This shape is the **TestRun** contract (§6.1) and is the only thing the renderer needs.

### 4.3 Makefile / drift

`make gen-tests` and `make gen-tests-check` in each app repo's server and uitests modules (scaffolded by method-assets), on the pattern of `gen-models-check`.

## 5. Checks

### 5.1 Arch gate (framework-go/arch, exempt by T3)

New rule `scenario-tests-only`, applied to every layer leaf under `internal/{manager,engine,resourceaccess,client}` and to `webApp/src` + `uitests`:

- The only `_test.go` files allowed in a component package are `<stereotype>_scenarios_test.gen.go` and `<stereotype>_hooks_test.go`.
- Both must declare `package <pkg>_test` (black box). The hooks file may import only the component's own package, `scenariohost`, `testinfra`, the project's generated contract packages, and stdlib.
- `webApp/src/**/*.test.{ts,tsx}` and non-generated `uitests/**/*.spec.ts` are violations.
- Replaces the current `test-file-name` rule (`framework-go/arch/filelayout.go:14-24,189-213`) which mandates in-package `<stereotype>_test.go`.

This runs in `TestFileLayout`-style gate tests in archistrator (`server/internal/arch_test.go`) **and** in the method-assets `go-checks.yml.tmpl` so every app repo runs it on PR.

### 5.2 Methodcheck rules (framework-go/methodcheck, replacing `rules_testplan.go` STP-*)

| Rule | Statement |
|---|---|
| TP-BOUND | Every scenario in `ForComponent(c)` has exactly one binding; no binding references an unknown id (stale after a use-case edit). |
| TP-STEP | Binding steps match projected stimuli 1:1 by seq. |
| TP-ARG-NAME / TP-ARG-TYPE | Inputs name real params and validate against the param schema (carried over from STP-ARG-*). |
| TP-EXPECT | `Expect` matches the op's result schema or declares an error the contract allows. |
| TP-PROBE | Probes name real read-only ops. |
| TP-OP-REACHED | Every operation of every contract is the input of ≥1 scenario. Failure text: "dead operation or missing use case". |
| TP-SKIP | A skip names an activity that is genuinely not yet integrated. |

Design-health (render-on-read) also surfaces `UC-NO-IO`: a use case whose diagram yields zero external stimuli.

### 5.3 Drift

`gen-tests-check` fails on any diff in `*.gen.go` / `*.spec.gen.ts`, and on a hooks file that references a hook not present in the plan (orphans) or lacks one that is (missing).

## 6. Results on the testing task

### 6.1 Run record

```go
type TestRun struct {
    ID         string        // venue run id (GH run id / local uuid)
    Activity   string
    Component  string
    Revision   string        // the construction revision tested
    StartedAt, EndedAt time.Time
    Artifact   string        // GH artifact name or local path
    Scenarios  []ScenarioResult // from results.json
}
```

Stored at `.testingState.testRuns[]` (the existing slot, retyped). Written by the venue via the aiarch-state MCP verb `recordTestRun` (new; replaces the run half of `recordTestingState`).

### 6.2 Venue

- **GitHub (cloud):** `aiarch-construct.yml.tmpl` gains, for the `integration` phase, steps: `make gen-tests-check`, `make test-scenarios` (server) / `npm run test:scenarios` (uitests), `actions/upload-artifact` named `test-results-<activityId>-<revision>` containing `test-results/**` (results.json, go test -json, Playwright videos/traces), then `recordTestRun`. Tests fail the job but the run record is still written.
- **Local:** same make targets; `test-results/` is written under `.aiarch/test-results/<run>/` and `recordTestRun` points `Artifact` at it.

### 6.3 Fetch

`infrastructure-github` gains `listArtifacts(runId)` and `downloadArtifact(id) → zip stream` (REST `actions/runs/{id}/artifacts`, `actions/artifacts/{id}/zip`), authenticated with the existing installation token.

The **artifactAccess** resource access (already profile-split, Gitea testinfra) gains ops `GetTestRunIndex(run) → results.json` and `OpenTestRunFile(run, path) → stream` with cloud (GitHub zip, cached on disk per run) and local (directory) variants. DeliveryManager exposes `queryProjectView{kind:"testRun"}` and a streaming file endpoint `GET /api/v1/delivery/test-run/{run}/file?path=` for video/trace bytes.

### 6.4 Render

The `testing` task's `ReviewBody` gets `TaskArtifact` kind `testRun` (`taskArtifactFor.ts`) → `TestRunPanel`:

- Header: run id, revision, venue link, pass/fail/skip counts.
- Scenario list (CommentableList; item anchors `scenario:<id>`): id, title, guards, status, duration.
- Server component: per-scenario stdout/stderr tabs from the `go test -json` stream.
- UI app: `<video>` for the scenario's `videoRef`; "Open trace" opens the **self-hosted Playwright trace viewer** (`playwright-core/lib/vite/traceViewer`, copied into `webApp/public/trace-viewer/` by the build) with `?trace=<file endpoint URL>`.
- Plan lens: the existing `ScenarioBrowser` is repurposed to render `ForComponent(c)` + bindings for the `stp` task (test plan review) — same ids, same anchors.

### 6.5 Gate

In `runGate` (`deliveryactivity.go:2270`), for task `testing` the round cannot `passRound` unless:

1. a `TestRun` exists for the activity at the activity's current construction revision;
2. its `Scenarios` set equals `ForComponent(c)` ids (no missing, no unknown);
3. every status is `pass` (skips block; §3.1);
4. `GetTestRunIndex` succeeds and, for UI apps, every `videoRef`/`traceRef` is openable.

Failure holds the gate with a system comment naming the failing scenarios; reviewers never see a task that cannot render.

## 7. Removals

| Item | Where |
|---|---|
| N-STP, N-IT emission; `systemTestingActivity`; `addSinkEdges` sink rule → the network's sink becomes the project-end milestone with every terminal activity feeding it directly | `estimationengine.go:1428-1444,1866-1890` |
| `TestingVariant` Plan / SystemTest; `deriveVariant` rows | `projectstateaccess.go:8523-8540`, `contract.gen.go:951-959` |
| `testing:plan`, `testing:systemTest` lifecycles | `method-assets/lifecycles.json:86,134` |
| Commands `testing-plan-*`, `testing-systemtest-*`; agent `software-tester` | `method-assets/assets/claude/{commands,agents}` + materialized copies |
| `SystemTestPlan`, `TestCase`, `TestScenario`, `TestStep` types; `systemTestPlan` in `TestingState` | `projectstateaccess.go:5252-5370` |
| `rules_testplan.go` STP-*; `project.go:597-640` mirrors | framework-go/methodcheck |
| `cmd/gen-systemtests`, `systemtests/` module, `systemtests.yml` | archistrator |
| Hand-written `uitests/tests/*.spec.ts` (preview fixtures + `meta/use-case-coverage` included); `cmd/gen-uitests-fixtures`, `cmd/gen-uitests-episodes` | archistrator |
| Non-generated `_test.go` in every component package; all `webApp/src/**/*.test.ts` | archistrator (by the migration workflow) |
| `SystemTestRunView`; `artifactRenderers.tsx:44-45` rows | webApp |
| `the-method-testing` SKILL §N-STP/N-IT rows; `the-method-activity-list` N-STP/N-IT lines | method-assets skills |

Committed `.activityList` / `.network` / solution slots contain N-STP and N-IT today; the derivation drift gate will reject them, so the migration workflow re-derives project design (one review task, R7) as its first step.

## 8. Lifecycle changes (method-assets `lifecycles.json`)

- service/frontend `stp` task: `dependsOn: ["designReview"]` (was `srsReview`). Rationale: bindings need contract types. Its `exitCriterion`: "Every projected scenario is bound; TP-* pass."
- `testing` task `exitCriterion`: "A test run at the current revision covers every scenario, all pass, and results render."
- `test_plan` phase description and the `service-test-plan` / `frontend-test-plan` commands rewrite to: call `listComponentScenarios`, bind values, mark hooks, `recordPhaseArtifact`.
- `construction` command adds: run `make gen-tests`, fill hooks, delete any other test, `make test-scenarios` green before `publishDraft`.

## 9. Rollout

1. **Platform** (T7): framework-go `scenario` + `scenariohost` + arch rule + TP-* rules; app-generator `testgen`; infrastructure-github artifacts; method-assets lifecycles/commands/skills/templates/scaffold. Release + push each; bump pins in archistrator.
2. **Archistrator mechanism** on branch `deterministic-component-testing`: schema (`$defs`) + modelgen regen; MCP verbs; estimation removals; artifactAccess ops; DeliveryManager query + endpoint; gate; webApp panel; CI workflows.
3. **Migration workflow** (Claude Workflow tool; see the plan): re-derive project design → per component pipeline (bind → generate → fill hooks → purge non-generated tests → run green → review) → final gate sweep. Runs well above the 10-agent guideline, founder-approved.
4. Merge with `scenario-tests-only` at zero waivers; drain in-flight lifecycle workflows for removed activity types; deploy.

## 10. Open earmarks (not in this wave)

- Fork/join interleaving coverage beyond single-stimulus (paper §5 future work).
- Boundary-value generation from param schemas (`minimum`/`maxLength`) as additional deterministic cases.
- Other-language emitters beyond Go and TypeScript.
- Retention: GitHub artifacts expire (default 90 days); a run whose artifact is gone renders as "expired" and cannot re-pass the gate without a re-run.
