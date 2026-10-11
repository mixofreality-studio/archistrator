# Agent Task Spec — design

Date: 2026-10-11 · Status: founder-approved design (brainstorm), spec pending review

## 1. Problem

An agentic task in an activity shows three facts (worker class, command, exit
criterion), looked up client-side from `lifecycles.gen.ts`. Nothing tells a human
what the agent is actually asked, which model runs it, what it may call, what it
may touch, or what it must pass. Worse, several of those things are not even
decided deterministically today:

- **Model** — charter `model:` lines are inert on both rails
  (`agenticjobaccess.go:3364-3369`); cost uses a hand-copied `roleModel`
  (`deliverymanager.go:12265`) that has already drifted from the charters.
- **Prompt** — the construction prompt is `/<command> <component> <activity>`;
  what that expands to is invisible.
- **Tools** — deny-only (`--disallowedTools` / `permissions.deny` from the
  StepManifest); there is no allowlist a human can read.
- **Edit scope** — not enforced anywhere.
- **Checks** — the integration agent generates, runs and *records* its own
  scenario tests (`service-integration.md` steps 4/7/9); the workflow only `jq`s
  the agent-written `results.json`. Other tasks' checks live implicitly inside
  write tools.

## 2. Goal

One deterministic **AgentTaskSpec** per agentic task, computable with or without an
episode, that is (a) rendered in the task panel and (b) exactly what the venue
executes and gates on. Agents stay focused (fewer tools, scoped checks, smaller
context); humans see what each step does and needs to succeed.

## 3. Rulings (brainstorm 2026-10-11)

| # | Ruling |
|---|---|
| R1 | Checks are typed per task kind and all computed: rules (methodcheck / TP-* / contract rules), commands (build/vet/lint/arch gates scoped to edit scope), scenarios (`scenario.ForComponent`). B12 stands. |
| R2 | The deterministic half of integration (generate tests, run, record) leaves the prompt and moves into the workflow. Agents never record runs. `testing` stays a pure deterministic gate. |
| R3 | Edit scope is **enforced**: the workflow diffs the agent's commits against the spec's path globs and fails on violation. |
| R4 | Model = project `WorkerRateSpec.ModelID` override → charter `model:`. One resolver feeds dispatch (`--model`) and cost; `roleModel` is deleted. |
| R5 | The job **recomputes** the spec from the same code at the activity revision; the server passes `spec_hash`; mismatch refuses to start. |
| R6 | Scope: every dispatch **and agentic review** task in every lifecycle, including the design prefix (mission → glossary → volatilities → core use cases → architecture, drafts and critiques), on both venues (GH + local). |
| R7 | Tools become an explicit allowlist. |
| R8 | One agent tool, `run_checks(failedOnly?)`. No ids: the MCP server resolves the spec from its session context. Compact results; per-check timeout. |

## 4. AgentTaskSpec

### 4.1 Shape

```
AgentTaskSpec {
  activity, task            // ids
  identity  { command, agent (charter name), workerClass, exitCriterion, kind: dispatch|review }
  model     { id, source: projectOverride|charter, estimate { tokensIn, tokensOut, usd } }
  prompt    { text, tokens }                 // fully rendered: command body + charter + skills + arguments
  tools     { mcp: [verb...], builtin: [tool...] }
  editScope { components: [{ id, paths: [glob...] }], stateOnly: bool }
  checks    [{ id, kind: rule|command|scenario, title, source, timeoutSec }]
  hash                                       // over every field above (canonical JSON)
}
```

`check.source` is a typed reference: `{rule: <rule id>}`, `{scenario: <id>, useCase: <id>}`,
or `{command: <argv>}`.

### 4.2 Resolution

`ResolveTaskSpec(project, methodAssets, activityId, taskId) (AgentTaskSpec, error)` is
a **pure function** in shared platform code (method-assets, beside `stepmanifest`, or a
new sibling package if the import graph requires — scenariohost precedent). Server,
`aiarch-state-mcp` and the job all import it.

| Field | Source |
|---|---|
| identity | `lifecycles.json` task + phase (moved off the client; `taskFactsFor` deleted) |
| model | `planningAssumptions` `WorkerRateSpec.ModelID` for the worker class, else charter `model:`; estimate from the shared rate table (moved out of `deliverymanager.go`) |
| prompt | command `.md` + charter + seated skills, `$ARGUMENTS` substituted — the same materialization `seat-assets` performs |
| tools | `StepManifest` for the command, as an allowlist (MCP verbs + built-ins), plus `run_checks` |
| editScope | component id → package path(s) per the layer file-layout standard; integration adds the hooks file; design/requirements/test-plan/review tasks are `stateOnly` (write through MCP only) |
| checks | per task kind — see 4.3 |

### 4.3 Checks per task kind

| Task kind | Checks |
|---|---|
| Design drafts + critiques (Mission, Glossary, Volatilities, CoreUseCases, System) | methodcheck rules registered for the artifact kind, evaluated on the drafted artifact |
| Requirements (SRS), detailed design (contracts), frontend design | that artifact kind's rules (incl. Appendix-B contract rules) |
| Test plan | TP-* rules |
| Construction | `gofmt` check, `go build`, `go vet`, lint, arch gates — scoped to `editScope` packages (frontend: tsc/eslint-boundaries equivalents) |
| Integration | `gen-tests-check` + one `scenario` check per `scenario.ForComponent` scenario of each contract of the component |
| Deployment / documentation / testing-* variants | the command/rule checks their lifecycle already implies; a task with genuinely no checks says so explicitly (`checks: []`) and the UI shows "no checks" |

The rule-to-artifact-kind registry is data (generated, drift-gated), not a hand list.

### 4.4 Query

New Manager query `deliveryQueryTaskSpec {projectId, activityId, taskId, revision?}` →
`AgentTaskSpec` plus, when a CheckRun exists for that task at that revision, the
CheckRun (§5.4). Works for never-run tasks.

## 5. Execution

### 5.1 Dispatch

`dispatchInputsFor` adds `spec_hash` (and `task_id`). Design dispatch likewise.

### 5.2 Job sequence (GH `aiarch-construct.yml.tmpl`, `aiarch-design.yml.tmpl`; local `constructDispatchPlan` / design plan run the identical sequence)

1. `aiarch-state-mcp task-spec --activity --task --out spec.json`; compare to `spec_hash`; mismatch → fail before the agent.
2. Seat assets (unchanged) and write the MCP config.
3. Deterministic pre-steps by kind — integration: `make gen-tests` (seeds the hooks file).
4. Run the agent with: prompt = `spec.prompt.text`; `--model spec.model.id`; built-ins restricted to `spec.tools.builtin` (exact CLI mechanism pinned in the plan); MCP server registers only `spec.tools.mcp`. The `--disallowedTools` deny path is deleted.
5. **Edit-scope gate**: changed paths `base..HEAD` ⊆ `editScope` globs ∪ permitted generated paths (generated test files, `.aiarch/state/**` via state tools). Violation → fail.
6. **Check gate**: `aiarch-state-mcp run-checks --gate` — full set, same runner as the tool.
7. **Record** the CheckRun (red or green) and upload logs as the run artifact.
8. Job green iff every check passed and edit scope held.

### 5.3 `run_checks` tool

- Composed MCP verb, mode-implicit for every spec'd task (added to stepmanifest's verb registry; sync test covers it).
- `run_checks()` resolves the spec from session env (`AIARCH_ACTIVITY_ID`, task, revision) and runs every check. `run_checks({failedOnly: true})` re-runs only the checks that failed on this session's previous call (server-tracked).
- Returns `{passed, failed, results: [failures only: {id, title, tail}], logRef}` — output tails trimmed; full logs to a file.
- Every check has a timeout (default from kind; scenarios get a per-scenario cap — the 6.3h webClient hang).
- Scenario subset runs must **merge** into per-contract `results.json`, never overwrite (today's `scenariohost.Flush` rewrites the index — fixed here).
- Agent-initiated runs are not recorded. Prompts say: "call `run_checks()` until green; finish with a full run".

### 5.4 CheckRun (replaces TestRun)

```
CheckRun { id, activity, task, revision, specHash, startedAt, endedAt, artifact,
           results [{ checkId, kind, status: pass|fail|skip|timeout, durationMs,
                      outputRef, traceRef?, videoRef? }] }
```

Stored under `.testingState.checkRuns` (TestRun removed; no migration code per B19 —
our own project.json conformed by a data edit). Written only by the workflow/local
gate step (`aiarch-state-mcp record-check-run`). `scenarioTestingGate` reads the
integration task's CheckRun scenario results; its completeness/green/readable rules are
unchanged. `record-test-run` and `recordTestingState`'s TestRun refusal path are deleted.

### 5.5 Prompt changes (method-assets commands)

Integration commands drop generate/run/record steps (service steps 4, 7, 9; frontend
equivalents) in favour of "fill hooks, wire, `run_checks()` until green". Construction
and design commands gain the `run_checks()` instruction. Charters' `model:` become live
(drift-gated against the resolver).

## 6. UI

`TaskHeader` facts strip → **TaskSpecPanel**, rendered on every agentic task above the
episode timeline, run or not:

- **Header row**: agent avatar + worker class · `/command` · model chip · cost (estimate; actual beside it once run) · exit criterion.
- **Prompt**: collapsed by default with token count; expands to the rendered text, monospace.
- **Tools**: chips grouped MCP / built-in.
- **May edit**: component chips (link to component); expand for path globs; `stateOnly` shows "writes through state tools only".
- **Checks**: grouped by kind; each row title + source link (rule id; scenario → use case/diagram; command). Before a run: "not run". After: status, duration, expandable output from the CheckRun at the viewed revision; scenario rows reuse `TestRunPanel` row pieces (trace, video). Run `specHash` ≠ current hash → "spec changed since this run" badge.
- Every check row is a comment anchor.

`testing` review node keeps `TestRunPanel`, fed by the integration CheckRun. Human review
tasks show reviewer roster + gate rule. Existing review aids (`TestRunPanel`, scenario
diagrams, episode timeline) are kept, not rewritten.

## 7. Testing

- **Baseline first**: record the set of currently failing tests on main (Go: server, systemtests, platform modules; webApp) before any change. Some are red today; the bar is **no regression of any currently passing test**, and every new test passes.
- Resolver: golden spec per lifecycle task (all lifecycles); hash stability; drift gate — charter `model:` ↔ resolver, stepmanifest ↔ `spec.tools`, rule registry ↔ methodcheck.
- Gates: out-of-scope edit fails; red check fails; spec-hash mismatch refuses to start; subset run merges rather than overwrites.
- `run_checks`: no-arg runs full set from session; `failedOnly` re-runs last failures; timeout yields `timeout`.
- UI: run locally on real state; Playwright pass over the panel (never-run task, run task green, run task red, spec-changed badge).

## 8. Rollout

1. Platform: method-assets (resolver, stepmanifest, commands, workflow templates), scenariohost (merge-on-flush, timeouts) — implemented, CI green except pre-existing reds.
2. Archistrator server + webApp against `replace`/workspace pins.
3. Conform archistrator's own project.json (TestRun → CheckRun, data edit).
4. **Serve the app locally for founder review and approval. No release before approval.**
5. After approval: tag + push platform releases, bump pins, re-seat workflow templates, release server + webapp.
6. Drain in-flight construct/design jobs in Temporal before deploying.

## 9. Out of scope

- Agent-initiated runs as recorded evidence.
- Changing review policy / reviewer routing.
- Phase-2 project-design commands beyond those already in the activity lifecycles.
