# SP4 — Metrics Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the metrics dashboard in `archistrator-bench` — a build-time aggregator (`bench dashboard build`) that inlines the pipeline's `runs/` + `analysis/` outputs into one `data.json`, and a static Recharts/React SPA that fetches it and renders five views, never presenting a synthetic or underpowered number as confident signal.

**Architecture:** Two units with one data contract. The aggregator (pure Node, in `scripts/` + a `bench dashboard` CLI subcommand) walks `runs/**` + `analysis/**`, validates each source JSON against the pipeline's own `schema/*.json`, and emits a single typed `DashboardData` bundle. The SPA (`dashboard/`, Vite + React + Recharts) fetches that one bundle and renders — no server, no DB, no filesystem access, read-only over the pipeline's outputs.

**Tech Stack:** TypeScript/ESM (Node 22), Vite + React 18 + Recharts (new), Vitest 4 + React Testing Library (dashboard components), ajv (schema validation — already a dep), the existing `bench` CLI (tsx entry). Follow the `dataviz` skill for chart palette/accessibility.

## Global Constraints

- **Repo:** all work in `/Users/davidmarne/mixofrealitystudio/archistrator-bench` (on its `main`). Spec: `archistrator/docs/superpowers/specs/2026-08-05-sp4-dashboard-design.md`.
- **The dashboard performs NO analysis** — SP3 owns every number. SP4 aggregates, renders, and labels. It only READS `runs/**` + `analysis/**`; it never writes there and never re-runs the analysis.
- **Read-only over the pipeline outputs; no mutation** from the SPA. No server, DB, auth, or live ingest.
- **Determinism:** the aggregator emits no `Date.now`/`Math.random` in the DATA (a `builtAt` stamp is allowed but excluded from any equality assertion). Re-running the aggregator over the same `runs/`+`analysis/` yields byte-identical `data.json` except `builtAt`.
- **Schema safety:** the aggregator validates against the schemas that EXIST (ajv, `Ajv2020` named import + `createRequire` for `ajv-formats` — the established repo pattern; see `src/runner/harvest.ts`), failing the build loudly on drift: run.json → `run.schema.json`; metrics.json → `metrics.schema.json` AND `metrics-body.schema.json`; each `hypotheses[i]` element → `hypothesis.schema.json` (that schema validates ONE hypothesis, not the report); preregistration/verdict → their schemas. **`findings.json` and the findings/hypotheses REPORT envelopes have no pipeline schema** — apply a light structural check (required envelope keys present) instead; do NOT invent new pipeline schemas. `dashboard-data.schema.json` (Task 1) covers the composed bundle.
- **Honest surfacing (load-bearing UI requirement):** the dashboard visually distinguishes, with distinct treatment and never as confident signal: `coverage.hasRealInput===false` (synthetic/placeholder), `finding.insufficientN===true` (n too small), consent-withheld runs (metrics envelope only, no families), verdict `inconclusive` (esp. underpowered — surface the `reason`), and estimation `mape/bias===null` or unpaired gaps (not 0). The badges are tested explicitly — they are the point, not decoration.
- **Empty tree tolerance:** empty `runs/`/`analysis/` → a valid empty bundle → the SPA renders an empty state, never a crash.
- **No real data yet:** `runs/` and `analysis/` are empty on disk. Build + test against synthesized-pipeline fixtures — drive the REAL `buildSynthRun` → `extractRun`/`detect`/`hypothesize`/`experiment` into a tmp tree (byte-identical shapes to production), NOT hand-authored JSON. First real data at AC iteration 0.
- **`dashboard/public/data.json` is a build output — gitignore it** (regenerable from committed `runs/`+`analysis/`), consistent with `dist/`.
- Reuse repo conventions: TS ESM `.js`-suffixed imports for Node code (aggregator); Vite/React uses its own bundler resolution (no `.js` suffix needed in `dashboard/src`). Commit after every task; messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Gates before each commit: `npm test` (full suite green), `npm run typecheck`, `npm run lint`. Dashboard adds its own `dashboard:build` / `dashboard:dev` scripts; the SPA's tsc/vite build must also pass.

## Data shapes the aggregator reads (from recon — ground truth; validate against the cited schema)

- `runs/<b>/<runId>/run.json` — RunRecord (`schema/run.schema.json`): `runId, benchmark, archistratorCommit, archistratorDirty ("true"|"false"|"unknown"), operatorPolicyVersion, epoch (int), suiteVersion, startedAt, endedAt, outcome ("succeeded"|"failed"), learningConsent (bool), operatorActions[] (open), gaps[] (open, {point,detail,at}), stackConfig (open), modelIds {claudeCliVersion, workerModelRoster, temporalCliVersion}`.
- `analysis/<b>/<runId>/metrics.json` — MetricsBody: `{runId, benchmark, schemaVersion, coverage[{metric,hasRealInput,note}], efficiency{perEpisode[], byKind, byWorkerClass, byTargetRef, totals}, rework{byTargetRef}, quality|null, estimation{pairs[], predictedNoActual[], actualNoPredicted[], mapeIn, mapeOut, biasIn, biasOut, byWorkerClass}}`. Consent-withheld: envelope + one coverage flag `{metric:"all",hasRealInput:false}`, NO efficiency/rework/quality/estimation. `quality` is `null` when no acceptance. `mape*/bias*` are `MapeResult|null`/`SignedBiasResult|null`. EfficiencyBucket = `{episodeCount, usageIn, usageOut, cacheRead, cacheCreate, streamedVsTerminalDivergence[], costUsd, numTurns, toolCallCounts, subagentCount, durationMs}`.
  - **TS typing caveat:** the exported `MetricsBody` type (`src/analysis/metrics-body.ts`) types only `coverage` + an open `[key:string]:unknown` index — `efficiency/rework/quality/estimation` are `unknown` on the type (detect casts them, `detect.ts:130-146`). The real family types are `EfficiencyMetrics`/`ReworkMetrics`/`QualityMetrics` (`src/analysis/extract-efficiency.ts`) + `EstimationMetrics` (`src/analysis/extract-estimation.ts`). The dashboard composes a **narrowed `RunMetrics` view type** from these and casts ONCE at load, not per-view.
- `analysis/<b>/findings.json` — `{benchmark, generatedAt, epochs[{epochKey, runCount, commits[]}], findings[{kind, subject, effectSize, n, insufficientN, detail}]}`. Trend `detail`: `{order[{runId,archistratorCommit,epochPosition,startedAt}], xs[], baselineSize, target, k, h, cusum{positive[],negative[],alarmsAt[]}, alarms[{index,runId,direction}]}` (verified `detect.ts:313-322`; `cusum` is a CusumResult OBJECT, not an array — the Trends view reads `alarms[]` for markers).
- `analysis/<b>/hypotheses.json` — `{benchmark, generatedAt, hypotheses[{finding_ref, root_cause_narrative, proposed_change, metric, direction, min_effect, predicted_mechanism}], gaps[{reason, attempts}]}`.
- `analysis/<b>/experiments/<expId>/{preregistration.json, runs.json, verdict.json}` — verdict `{expId, outcome ("supported"|"refuted"|"inconclusive"), statistic, pValue, effectSize, n, ranAt, metric, direction, alpha, test, reason?}`.
- Ordering: trend x-axis = `run.json.epoch` (int, monotonic), tiebreak `startedAt`, then `runId`.

---

### Task 1: The `DashboardData` type + schema + fixture-pipeline helper

**Files:**
- Create: `src/dashboard/types.ts` (the `DashboardData` shape shared by aggregator + SPA), `schema/dashboard-data.schema.json`
- Create: `test/support/synth-pipeline.ts` (drive the real pipeline over `buildSynthRun` → a tmp `runs/`+`analysis/` tree)
- Modify: `src/runner/harvest.ts` (EXPORT `RunRecord` + its `ModelIds` interface — they are currently module-private at `harvest.ts:168`; analysis exports only the `RunEpochFields` subset, which lacks `archistratorDirty/learningConsent/operatorActions/gaps/stackConfig/modelIds` the dashboard needs)
- Test: `test/dashboard-types.test.ts`, `test/support/synth-pipeline.test.ts`

**Interfaces:**
- Produces: `DashboardData = { builtAt: string, benchmarks: BenchmarkData[] }`; `BenchmarkData = { name: string, runs: RunSummary[], findings: FindingsReport | null, hypotheses: HypothesesReport | null, experiments: ExperimentView[] }`; `RunSummary = { run: RunRecord, metrics: RunMetrics | null }` where `RunMetrics` is the narrowed view type composing `EfficiencyMetrics|ReworkMetrics|QualityMetrics|null|EstimationMetrics` (all from `src/analysis/*`) atop the envelope + `coverage` — cast once here (as `detect.ts:130-146` does); `metrics` null if extract wasn't run for that run; `ExperimentView = { preregistration, verdict | null, runs: RunExperimentResult | null }`. Reuse the existing TS types (import `FindingsReport`/`HypothesesReport`/the experiment types from `src/analysis/*`, `EfficiencyMetrics`/`ReworkMetrics`/`QualityMetrics` from `extract-efficiency.ts`, `EstimationMetrics` from `extract-estimation.ts`, and `RunRecord`+`ModelIds` from the now-exported `src/runner/harvest.ts`) — do NOT redefine them.
- Produces: `buildSynthPipeline(tmpDir, spec): Promise<void>` — given a spec of benchmarks/runs (commits, epochs, metric overrides), calls `buildSynthRun` then the REAL pipeline: `extractRun` (each run) → `detect` → `hypothesize` (fake `invokeClaude` returning a STRICTLY schema-valid hypothesis: all 7 props, `additionalProperties:false`, a `finding_ref` the fixture knows, and a `metric` that is a real dot-path e.g. `"efficiency.totals.usageIn"`) → `registerExperiment` → `runExperiment` (fake `runBenchmark` that drives `buildSynthRun` per scheduled arm run and returns `{runId, runDir, outcome:"succeeded"}`) → `verdictExperiment`. **CRITICAL: `verdictExperiment` THROWS `NoCollectedRunsError` if `runExperiment` didn't write `runs.json` first — register→run→verdict, never register→verdict.** An INCONCLUSIVE verdict (for the honest-surfacing fixtures) is achieved with K<3 (→ "insufficient paired data") or K≤5 nonzero diffs at α=0.05 (→ underpowered). Note: `runExperiment`'s arm runs ALSO land under `runs/<b>/` + `analysis/<b>/<expId>-<arm>-<i>/` — so arm runs appear in the aggregated run list; that is the real layout, fixtures accept it. This is the ONLY fixture source for SP4 tests — no hand-authored dashboard JSON.

- [ ] **Step 1:** Read `src/analysis/extract.ts`, `detect.ts`, `hypothesize.ts`, `experiment.ts`, `finding.ts`, `episode-record.ts`, and `test/support/synth-archive.ts` to learn the exact exported types + function signatures. `DashboardData` imports and composes them.
- [ ] **Step 2: Failing test** `dashboard-types.test.ts`: a hand-built minimal `DashboardData` object validates against `schema/dashboard-data.schema.json` (ajv); a `DashboardData` missing `benchmarks` fails.
- [ ] **Step 3:** Author `types.ts` (composing the analysis types) + `dashboard-data.schema.json` (`additionalProperties:true` on the composed sub-objects so it doesn't fight the analysis schemas; require `builtAt` + `benchmarks`). Green.
- [ ] **Step 4: Failing test** `synth-pipeline.test.ts`: `buildSynthPipeline(tmp, {benchmarks:[{name:"todomvc", runs:[{commit:"aaa", epoch:0}, {commit:"bbb", epoch:1}]}]})` produces `tmp/runs/todomvc/*/run.json` AND `tmp/analysis/todomvc/*/metrics.json` + `findings.json`, all real-pipeline output (assert metrics.json validates `schema/metrics.schema.json`).
- [ ] **Step 5:** Implement `synth-pipeline.ts` (compose the real functions; fake `invokeClaude` for hypothesize returns a schema-valid hypothesis). Green.
- [ ] **Step 6:** Gates green. **Commit** (`feat(dashboard): DashboardData type + schema + synth-pipeline fixture helper`).

---

### Task 2: The aggregator — walk runs/ + analysis/ → data.json

**Files:**
- Create: `src/dashboard/aggregate.ts`, `scripts/build-dashboard-data.mjs` (thin CLI shim over aggregate), 
- Modify: `src/runner/cli.ts` (add `dashboard build` subcommand), `.gitignore` (add `dashboard/public/data.json`)
- Test: `test/dashboard-aggregate.test.ts`

**Interfaces:**
- Consumes: Task 1 `DashboardData`/types, `buildSynthPipeline`; the pipeline's `schema/*.json`.
- Produces: `aggregate(root: string, { now?: string }): Promise<DashboardData>` — enumerates benchmarks from `<root>/runs/*` (each benchmark dir), reads each run's `run.json` + paired `analysis/<b>/<runId>/metrics.json` (null if absent), each benchmark's `findings.json`/`hypotheses.json` (null if absent), and `analysis/<b>/experiments/*/` triples; VALIDATES each source against the schema that exists (per the schema-safety constraint); returns a `DashboardData`. **A run is keyed by its `runs/<b>/<runId>/run.json` (the run list = runs that exist in `runs/`, mirroring detect's exclusion of metrics-without-run); an `analysis/<b>/<runId>/` with no matching `run.json` is an "orphan analysis" — skip it (it can't be placed without run identity), do not crash.** `writeDashboardData(root): Promise<string>` — `aggregate` then write `<root>/dashboard/public/data.json`, return the path. `bench dashboard build [--root .]` CLI wraps it. NEVER reads `traces/`, `app/`, `project.json`. `now` is injectable so tests pin `builtAt`. **Determinism: sort every enumerated name (benchmarks, runIds, expIds) lexicographically before emitting, so the bundle is byte-stable across filesystems.** **Missing-dir tolerance: `<root>/analysis/` does not exist on disk at all today (only `runs/` exists, empty) — a missing `runs/` OR `analysis/` dir yields an empty/partial bundle, never a throw.**

- [ ] **Step 1: Failing test** `dashboard-aggregate.test.ts`: `buildSynthPipeline` a 2-benchmark, 3-run tree in tmp → `aggregate(tmp, {now:"2026-01-01T00:00:00Z"})` returns a `DashboardData` whose `benchmarks` names + `runs[].run.runId` + `runs[].metrics.efficiency.totals` match what the pipeline wrote; findings/hypotheses/experiments inlined; `builtAt` == the injected now.
- [ ] **Step 2:** Implement `aggregate.ts` (readdir + read + ajv-validate each source). Green.
- [ ] **Step 3: Failing tests** for the edges: MISSING `runs/`+`analysis/` dirs (not just empty) → `{builtAt, benchmarks:[]}` (valid, no crash); a run with `run.json` but no `metrics.json` → `metrics:null`; an `analysis/<b>/<runId>/` with no matching `run.json` → skipped (orphan, not in output, no crash); a consent-withheld metrics.json → inlined with its envelope-only body; a schema-DRIFTED source (write a metrics.json missing `schemaVersion`) → `aggregate` THROWS a clear validation error naming the file; two runs enumerated in non-sorted readdir order → output `runs[]` is lexicographically sorted (byte-stable).
- [ ] **Step 4:** Implement the edge handling + `writeDashboardData` + the `dashboard build` CLI subcommand + gitignore. Assert the CLI writes `dashboard/public/data.json` and it validates `dashboard-data.schema.json`. Green.
- [ ] **Step 5:** Gates green. **Commit** (`feat(dashboard): aggregator → data.json + bench dashboard build`).

---

### Task 3: Vite/React scaffold + typed data loader + empty state

**Files:**
- Create: `dashboard/index.html`, `dashboard/vite.config.ts`, `dashboard/tsconfig.json`, `dashboard/src/main.tsx`, `dashboard/src/App.tsx`, `dashboard/src/data/load.ts`, `dashboard/src/data/useDashboardData.ts`
- Modify: `package.json` (add react/react-dom/recharts/@vitejs/plugin-react/vite deps + `dashboard:dev`/`dashboard:build`/`dashboard:preview` scripts; add @testing-library/react + @testing-library/jest-dom + jsdom devDeps), `vitest.config.ts` (convert to vitest-4 `test.projects` — see Step 1), `eslint.config.js` (ensure it covers `dashboard/**/*.tsx` without erroring)
- Test: `dashboard/src/data/load.test.ts`, `dashboard/src/App.test.tsx`

**Interfaces:**
- Consumes: Task 1 `DashboardData` type, Task 2's `data.json` at `dashboard/public/data.json` (fetched at `/data.json`).
- Produces: `loadDashboardData(json: unknown): DashboardData` (validates against `dashboard-data.schema.json` in-browser via ajv, throws on invalid); `useDashboardData()` hook (fetch `/data.json` → loading/error/data state); `<App>` rendering a router shell over the five views (stubs for now) + an **empty state** when `benchmarks` is empty. Vite `root` = `dashboard/`, `base: "./"` (so the built SPA opens from file:// or any path).

- [ ] **Step 1:** `npm install` the deps. Configure `dashboard/vite.config.ts` (react plugin, `root: "dashboard"`, `base: "./"`). **Convert `vitest.config.ts` to vitest-4 `test.projects`** (the current config is `include: ["test/**/*.test.ts"], environment: "node"` — the dashboard `.test.tsx` files would SILENTLY never run): project **"node"** (`test/**/*.test.ts`, environment node) + project **"dashboard"** (`dashboard/src/**/*.test.{ts,tsx}`, environment jsdom, `plugins: [react()]`, a setup file importing `@testing-library/jest-dom`). Verify `npm test` runs BOTH projects — the run output must list both project names (assert this in the report; if only "node" appears, the dashboard tests are dead).
- [ ] **Step 2: Failing test** `load.test.ts`: `loadDashboardData(validBundle)` returns it typed; `loadDashboardData({})` throws (missing benchmarks). Use a `buildSynthPipeline`+`aggregate` fixture bundle (real shape), not hand-authored.
- [ ] **Step 3:** Implement `load.ts` + `useDashboardData.ts`. Green.
- [ ] **Step 4: Failing test** `App.test.tsx` (RTL + jsdom): `<App>` given an empty-benchmarks bundle renders an "no runs yet" empty state; given a 1-benchmark bundle renders the run-list view container (testid). Mock `useDashboardData` to return the fixture.
- [ ] **Step 5:** Implement `App.tsx` + router shell + view stubs + empty state. `npm run dashboard:build` succeeds (tsc + vite build). Green.
- [ ] **Step 6:** Gates green (incl. the SPA build). **Commit** (`feat(dashboard): Vite/React scaffold + typed loader + empty state`).

---

### Task 4: Honest-surfacing badges + shared chart wrappers

**Files:**
- Create: `dashboard/src/components/badges.tsx` (CoverageBadge, InsufficientNBadge, ConsentWithheldBadge, InconclusiveBadge, NotComputableBadge), `dashboard/src/charts/` (BarBreakdown.tsx, TrendLine.tsx, DiffBars.tsx — thin Recharts wrappers), `dashboard/src/theme.ts` (palette per dataviz skill)
- Test: `dashboard/src/components/badges.test.tsx`, `dashboard/src/charts/charts.test.tsx`

**Interfaces:**
- Consumes: the metrics/finding/verdict types.
- Produces: badge components that take the relevant flag/shape and render a distinct, labelled marker: `<CoverageBadge coverage={coverageItem} />` (shows "synthetic/placeholder — no real input" when `hasRealInput===false`, else nothing or a subtle "real" marker); `<InsufficientNBadge finding={f} />` ("n=<n> — too small for significance" when `insufficientN`); `<ConsentWithheldBadge />` (its detection predicate = `run.learningConsent === false` from run.json — the required field, NOT sniffing the coverage stub, which is only corroborating); `<InconclusiveBadge verdict={v} />` (surfaces `reason`); `<NotComputableBadge />` (for null mape/bias / unpaired gaps). Plus chart wrappers: `<BarBreakdown data labelKey valueKey />`, `<TrendLine points alarms />` (line + CUSUM alarm bands), `<DiffBars a b metrics />`. FOLLOW the `dataviz` skill for palette/accessibility.

- [ ] **Step 1:** Invoke the `dataviz` skill (it's a Skill-tool skill — call it via the Skill tool, not a filesystem read) for palette + chart conventions; set `theme.ts` accordingly. **Fallback if unavailable:** apply its two load-bearing rules directly — (a) a colorblind-safe categorical palette; (b) never encode meaning by color ALONE, so the CUSUM alarm markers and every honest-surfacing badge carry a shape + text label, not just a color.
- [ ] **Step 2: Failing tests** `badges.test.tsx` (RTL): CoverageBadge with `hasRealInput:false` renders the "synthetic" text + a `data-testid="badge-synthetic"`; with `true` renders no warning; InsufficientNBadge with `insufficientN:true, n:2` renders "n=2" + testid; InconclusiveBadge renders the verdict `reason`; NotComputableBadge renders "not computable". These are the load-bearing honest-surfacing assertions.
- [ ] **Step 3:** Implement `badges.tsx`. Green.
- [ ] **Step 4: Failing test** `charts.test.tsx`: each chart renders without throwing given a fixture series (render-smoke; assert an SVG/role present). TrendLine given alarm indices renders the alarm markers.
- [ ] **Step 5:** Implement the three chart wrappers. Green.
- [ ] **Step 6:** Gates green. **Commit** (`feat(dashboard): honest-surfacing badges + Recharts wrappers`).

---

### Task 5: Run list + run detail views

**Files:**
- Create: `dashboard/src/views/RunList.tsx`, `dashboard/src/views/RunDetail.tsx`
- Test: `dashboard/src/views/RunList.test.tsx`, `dashboard/src/views/RunDetail.test.tsx`

**Interfaces:**
- Consumes: `DashboardData`, badges (Task 4), charts (Task 4).
- Produces: `<RunList data />` — a filterable table (by benchmark/commit/outcome/epoch) of all runs across benchmarks; each row: runId, short commit, outcome, epoch, total tokens (in+out), cost, quality pass-rate, and a CoverageBadge/ConsentWithheldBadge where applicable; row click routes to detail. `<RunDetail run={RunSummary} />` — the run's `metrics.json` breakdowns: token/cost/duration by kind/workerClass/target (BarBreakdown), quality feature-checklist pass/fail grid, rework ratios, estimation predicted-vs-actual (MAPE/bias with NotComputableBadge on nulls + unpaired-gap callouts), the run's gaps + modelIds. Each metric family shows its coverage badge.

- [ ] **Step 1: Failing test** `RunList.test.tsx` (RTL, over a `buildSynthPipeline`+`aggregate` fixture with a real + a consent-withheld + a failed run): renders one row per run; filtering by outcome="failed" shows only the failed run; the consent-withheld run shows the ConsentWithheldBadge; a synthetic-coverage run shows the synthetic badge.
- [ ] **Step 2:** Implement `RunList.tsx`. Green.
- [ ] **Step 3: Failing test** `RunDetail.test.tsx`: given a run with real efficiency + a null estimation mape → renders the token-by-kind breakdown (values present) AND the NotComputableBadge on the estimation MAPE; a run with `quality:null` renders a "no acceptance" state, not a crash; unpaired estimation gaps render the callout; a **consent-withheld run** (metrics families ABSENT, `run.learningConsent===false`) renders the withheld state, not a crash (the family-absent path).
- [ ] **Step 4:** Implement `RunDetail.tsx`. Green.
- [ ] **Step 5:** Gates green. **Commit** (`feat(dashboard): run list + run detail views`).

---

### Task 6: Run diff + trends views

**Files:**
- Create: `dashboard/src/views/RunDiff.tsx`, `dashboard/src/views/Trends.tsx`
- Test: `dashboard/src/views/RunDiff.test.tsx`, `dashboard/src/views/Trends.test.tsx`

**Interfaces:**
- Consumes: `DashboardData`, DiffBars/TrendLine (Task 4).
- Produces: `<RunDiff data />` — pick two runs (by runId), show per-metric deltas side-by-side (total tokens, cost, quality passRate, estimation MAPE) with each run's archistrator commit labelled and delta direction; NO significance claim (descriptive only — a note says "for a significance test, register an experiment"). `<Trends data benchmark />` — for a chosen benchmark + epoch group, render metric-over-epoch line charts FROM `findings.json` trend findings (`detail.order[]` + `detail.xs[]` + `detail.alarms[]`) — token cost, quality, estimation error — with CUSUM alarm bands; if no trend finding exists (too few runs), render an "insufficient runs for a trend" state.

- [ ] **Step 1: Failing test** `RunDiff.test.tsx`: given two runs with known token totals, selecting both renders the delta (b−a) with correct sign and both commits labelled; a metric that's null on one side renders "n/a", not NaN.
- [ ] **Step 2:** Implement `RunDiff.tsx`. Green.
- [ ] **Step 3: Failing test** `Trends.test.tsx` (fixture via `buildSynthPipeline` with an injected downward token trend across ≥4 epochs so `detect` emits a trend finding): renders the TrendLine with the series from `findings.json` and the CUSUM alarm marker; a benchmark with too few runs (no trend finding) renders the "insufficient runs" state.
- [ ] **Step 4:** Implement `Trends.tsx`. Green.
- [ ] **Step 5:** Gates green. **Commit** (`feat(dashboard): run diff + trends views`).

---

### Task 7: Science ledger view

**Files:**
- Create: `dashboard/src/views/ScienceLedger.tsx`
- Test: `dashboard/src/views/ScienceLedger.test.tsx`

**Interfaces:**
- Consumes: `DashboardData` (findings + hypotheses + experiments), badges (Task 4).
- Produces: `<ScienceLedger data benchmark />` — the chain per benchmark: findings (kind, subject, effectSize, n, InsufficientNBadge) → hypotheses (finding_ref linked to its finding, root_cause_narrative, proposed_change, metric+direction+min_effect) → experiments (frozen preregistration summary → verdict with a distinct treatment per outcome: supported / refuted / **inconclusive** with its `reason` via InconclusiveBadge). Refuted + inconclusive are first-class (negative results are results). A hypothesis whose `finding_ref` matches no finding is flagged ("orphan hypothesis").

- [ ] **Step 1: Failing test** `ScienceLedger.test.tsx` (fixture with a finding + a hypothesis referencing it + an experiment with an INCONCLUSIVE underpowered verdict): renders the finding→hypothesis→verdict chain; the inconclusive verdict shows its `reason` (underpowered) via InconclusiveBadge and is visually distinct from supported/refuted; an insufficientN finding shows its badge.
- [ ] **Step 2:** Implement `ScienceLedger.tsx`. Green.
- [ ] **Step 3: Failing test:** a hypothesis with a `finding_ref` matching no finding → renders the "orphan hypothesis" flag.
- [ ] **Step 4:** Implement the orphan handling. Green.
- [ ] **Step 5:** Gates green. **Commit** (`feat(dashboard): science ledger view`).

---

### Task 8: Wire router + end-to-end build smoke + closeout

**Files:**
- Modify: `dashboard/src/App.tsx` (wire the five real views into the router), `README.md` (the dashboard: build+open flow)
- Create: `dashboard/src/App.integration.test.tsx`, (optional) `test/integration/dashboard-build.test.ts`
- Modify: `archistrator/docs/superpowers/specs/2026-08-05-sp4-dashboard-design.md` (status closeout — the one sanctioned archistrator-repo docs edit)

**Interfaces:**
- Consumes: all views.

- [ ] **Step 1:** Wire RunList/RunDetail/RunDiff/Trends/ScienceLedger into `App.tsx`'s router (replace the stubs). Navigation testids.
- [ ] **Step 2: Failing integration test** `App.integration.test.tsx` (RTL over a full `buildSynthPipeline`+`aggregate` bundle: 2 benchmarks, a downward trend, a consent-withheld run, an inconclusive experiment): mount `<App>`, assert you can navigate run-list → a run's detail → diff → trends → science ledger, and that each honest-surfacing badge appears where its fixture demands (synthetic, insufficientN, consent, inconclusive).
- [ ] **Step 3:** Fix any wiring seams. Green.
- [ ] **Step 4: Build smoke:** `bench dashboard build` over a `buildSynthPipeline` tmp tree writes a valid `data.json`; `npm run dashboard:build` (tsc + vite) succeeds and emits `dashboard/dist/`. Capture in the report. (Optionally a gated Playwright test loads the built SPA — behind an env flag, not the default suite.)
- [ ] **Step 5:** Full gates: `npm test`, `npm run typecheck`, `npm run lint`, `npm run dashboard:build`. Update README (how to `bench dashboard build` then `npm run dashboard:dev`/`preview` to view). Amend the SP4 spec status to "implemented"; commit that spec amendment in the ARCHISTRATOR repo (docs-only). **Commit** the bench work (`chore: SP4 dashboard complete — five views over the pipeline, end-to-end build smoke green`).

---

## Explicitly deferred (per spec §8)

- Real end-to-end (real archived runs → dashboard) — AC iteration 0.
- Visual-regression testing (render-smoke only in v1).
- Dashboard hosting/publishing beyond local `vite build`/`preview`.
- Any server/DB/auth/live-ingest.
- An index+lazy-fetch data feed (revisit only if a benchmark accumulates hundreds of runs).
