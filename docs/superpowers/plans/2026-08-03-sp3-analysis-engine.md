# SP3 — Analysis Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the four-stage analysis engine (`extract` → `detect` → `hypothesize` → `experiment`) in the `archistrator-bench` repo — turning immutable run archives into falsifiable, human-ratified improvement hypotheses, with all statistics TS-native and golden-tested against scipy.

**Architecture:** Four CLI stages extending SP2's `bench` command, plus a shared TS-native `stats/` module. All SP3 output lands in a regenerable `analysis/` tree kept strictly separate from the immutable `runs/` archive (evidence vs analysis split). `extract` aggregates the SP1 `EpisodeRecord` ledger + `project.json` + `acceptance.json` into `metrics.json`; `detect` runs deterministic statistics over comparable runs; `hypothesize` is the only LLM stage (schema-caged); `experiment` enforces pre-registration → declared test → verdict.

**Tech Stack:** TypeScript/ESM (Node 22), Vitest, ajv (schemas), the `claude` CLI (hypothesize stage, subscription auth). Statistics are hand-written TS golden-tested against scipy reference values generated once offline (Python is NOT a runtime or CI dependency).

## Global Constraints

- **Repo:** all work is in `/Users/davidmarne/mixofrealitystudio/archistrator-bench` (the sibling bench repo, on its `main`). The spec being implemented is `archistrator/docs/superpowers/specs/2026-08-03-sp3-analysis-engine-design.md`.
- **Evidence is immutable:** SP3 code **only reads** `runs/**`; it never writes, modifies, or adds files there. All SP3 output goes under `analysis/**`. The immutability CI gate must stay green.
- **`analysis/` is regenerable & deterministic:** re-running any stage over the same sealed inputs yields byte-identical output. No `Date.now`/`Math.random` in any extract/detect/verdict computation (timestamps that must be recorded come from the run's own archived data or are passed in).
- **Hard science:** every statistic is golden-tested against a scipy-computed reference value pinned as a fixture. A stat with no scipy golden test is not done. Findings always carry effect size AND n; insufficient-n is labeled, never suppressed.
- **LLM proposes, math disposes:** the LLM (hypothesize stage only) emits schema-validated falsifiable claims; it never computes or touches a number. Malformed/free-prose output is rejected and retried (bounded).
- **Reuse SP2 patterns:** TS ESM with `.js`-suffixed relative imports (NodeNext); ajv via `Ajv2020` named import + `createRequire` for `ajv-formats` (the SP1/SP2 ESM gotcha); Vitest; the `bench` CLI entry runs via `tsx` (`npm run bench`). Read an existing SP2 module (e.g. `src/runner/index.ts`, `src/mcp/ops.ts`) before writing to match style.
- **Data reality (recon 2026-08-03):** `runs/` is empty; `.aiarch/traces/` doesn't exist in the dogfood repo. Real inputs available TODAY: the dogfood `project.json` (`archistrator/.aiarch/state/project.json`) and the real stream-json fixtures (`archistrator/server/internal/resourceaccess/agenticjob/testdata/streamjson/*.jsonl`). Everything else (episode ledgers, archived runs) must be **synthesized** for tests, matching the real shapes derived from Go source. First real end-to-end is AC iteration 0.
- Commit after every task; messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Gates before each commit: `npm test` (full suite green), `npm run typecheck`, `npm run lint`.

## Data shapes (derived from recon — treat as ground truth; verify casing against the cited source before parsing)

- **`EpisodeRecord`** (one JSON object per line in `runs/<b>/<runId>/traces/episodes.jsonl`) — the Go contract at `archistrator/server/internal/resourceaccess/episode/contract.gen.go`. Fields (VERIFY the exact JSON key casing against that file's json tags before writing the parser — the RA contract uses PascalCase props): `EpisodeID string`, `Kind int` (0 Design, 1 Construction, 2 Review, 3 Rework, 4 Answer), `TargetRef string` (artifactKind for design, activityId for construction), `Lineage {WorkflowID, RunID string, ActivityID *string}`, `WorkerClass *string`, `Model *string`, `Usage {In, Out, CacheRead, CacheCreate int64}`, `StreamedUsage *{...same...}`, `CostUSD *number`, `NumTurns *int64`, `ToolCallCounts map[string]int64`, `SubagentSpans [{ToolUseID string, StartedAt, EndedAt *string}]`, `StartedAt, EndedAt string(RFC3339)`, `Outcome int` (0 Succeeded, 1 Failed, 2 Cancelled, 3 Gap), `GapReason *string`, `TracePath *string`.
- **`project.json`** (`runs/<b>/<runId>/project.json`) — `.slots` is a map keyed `"0".."16"`; each slot `{status, kind, model, revisions int, reviewThread?[]}`. Predicted-estimation inputs: `slots["8"].model.rateCard` (per workerClass `{modelId, megatokensInPerDay, megatokensOutPerDay}`) and `slots["9"].model.activities[]` (`{name, effortDays, workerClass, coding, riskBucket}`). Design rework: `slots[k].revisions` + `slots[k].reviewThread[].{round, status, type}`. Construction state: `.activityConstruction[activityId] {phase, buildStatus, produced[]}` (no usage/token data — actuals come from the episode ledger, not here).
- **`AcceptanceResult`** (`runs/<b>/<runId>/acceptance/acceptance.json`) — `{buildOk bool, reason?, testsPass int, testsFailed int, acceptancePassRate number(0..1), featureChecklist [{id, pass}], soft {rubricScores: null}}`.
- **`RunRecord`** (`runs/<b>/<runId>/run.json`) — validates `schema/run.schema.json`; SP3 reads `outcome, archistratorCommit, archistratorDirty, epoch, suiteVersion, operatorPolicyVersion, modelIds, learningConsent, gaps[], operatorActions[]`.

---

### Task 1: `analysis/` conventions, metrics.json body schema, synthesized-archive test fixtures

**Files:**
- Create: `schema/metrics-body.schema.json` (the SP3-defined body; the existing `schema/metrics.schema.json` envelope stays)
- Create: `src/analysis/paths.ts` (path helpers for the `analysis/` tree)
- Create: `test/support/synth-archive.ts` (build a synthetic `runs/<b>/<runId>/` under a tmpdir for tests)
- Test: `test/analysis-paths.test.ts`, `test/support/synth-archive.test.ts`

**Interfaces:**
- Produces: `analysisDir(root, benchmark, runId)`, `metricsPath(...)`, `findingsPath(root, benchmark)`, `hypothesesPath(...)`, `experimentDir(root, benchmark, expId)` — all returning paths **under `<root>/analysis/`, never `<root>/runs/`**. Plus `SynthArchive` builder: `buildSynthRun(dir, {runId, benchmark, episodes, projectJson, acceptance, runRecord})` writing a valid `runs/<b>/<runId>/` (episodes.jsonl, project.json, acceptance/acceptance.json, run.json) for downstream tests. The `MetricsBody` TS type (fields defined incrementally by Tasks 3-4; Task 1 defines the envelope + a `coverage` block `{metric: string, hasRealInput: boolean, note: string}[]`).

- [ ] **Step 1:** Read `src/runner/harvest.ts` + `schema/metrics.schema.json` + `schema/run.schema.json` to confirm the archive layout and envelope. Read `scripts/check-runs-immutable.mjs` to confirm `analysis/**` is NOT under the gate (it isn't — the gate only covers `runs/**`; verify).
- [ ] **Step 2: Failing test** `analysis-paths.test.ts`: `metricsPath("/x","todomvc","r1")` === `/x/analysis/todomvc/r1/metrics.json`; assert NO path helper ever returns a string containing `/runs/`.
- [ ] **Step 3:** Implement `paths.ts`. Run to green.
- [ ] **Step 4: Failing test** `synth-archive.test.ts`: `buildSynthRun` in a `tmpdir` produces `runs/todomvc/r1/{episodes.jsonl, project.json, acceptance/acceptance.json, run.json}`; episodes.jsonl has one `EpisodeRecord`-shaped line per episode; run.json validates `schema/run.schema.json`.
- [ ] **Step 5:** Implement `synth-archive.ts` (episodes serialized with the exact `EpisodeRecord` key casing from `episode/contract.gen.go` — open that file and match its json tags). Author `metrics-body.schema.json` (envelope + coverage block; `additionalProperties:true` on metric sub-objects so Tasks 3-4 extend freely).
- [ ] **Step 6:** Gates green. **Commit** (`feat(analysis): analysis-tree paths + metrics body schema + synth-archive fixtures`).

---

### Task 2: `src/stats/` — TS-native statistics, golden-tested against scipy

**Files:**
- Create: `src/stats/index.ts` (+ one file per function if they grow: `wilcoxon.ts`, `bootstrap.ts`, `cusum.ts`, `descriptive.ts`)
- Create: `scripts/gen-stats-fixtures.py` (one-off, run manually; scipy output pinned — NOT a CI/runtime dep)
- Create: `test/stats/fixtures/*.json` (scipy reference values)
- Test: `test/stats/stats.test.ts`

**Interfaces:**
- Produces (pure functions, deterministic): `mean(xs)`, `median(xs)`, `mad(xs)` (median absolute deviation, scaled ×1.4826), `mape(pred, actual): number` (mean absolute percentage error over paired arrays, skipping pairs where actual==0 with a reported skip count), `signedBias(pred, actual): number` (mean signed relative error), `pairedDiff(a, b): number[]` (elementwise a−b for cluster inputs), `wilcoxonSignedRank(a, b): {statistic, pValue}` (exact for small n, normal-approx with continuity correction for larger), `bootstrapCI(xs, {statistic, iters, alpha, seed}): {lo, hi}` (percentile bootstrap with a SEEDED PRNG so it's deterministic — pass the seed in, no `Math.random`), `cusum(xs, {target, k}): {positive: number[], negative: number[], alarmsAt: number[]}` (tabular CUSUM), `paretoRank(items: {key, value}[]): {key, value, cumPct}[]` (descending, cumulative %).

- [ ] **Step 1: Write `scripts/gen-stats-fixtures.py`** — for a handful of pinned input arrays, compute the reference outputs with scipy (`scipy.stats.wilcoxon`, `scipy.stats.bootstrap`, `numpy` for mean/median/MAD/mape) and write `test/stats/fixtures/<fn>.json` = `[{input, expected}]`. Run it once locally (scipy in an ephemeral venv or `pip install --user`), commit the JSON fixtures. Document in the file header: "run manually to regenerate; scipy is not a project dependency."
- [ ] **Step 2: Failing tests** `stats.test.ts`: for each function, load its fixture and assert the TS output matches scipy's `expected` within a tight tolerance (`1e-9` for exact stats; `1e-2` relative for bootstrap CI given PRNG differences — bootstrap is asserted for *coverage/width sanity* against scipy, plus a determinism test: same seed → identical output twice). Include: `wilcoxon` on a fixture with a known scipy p-value; `mad` on `[1,1,2,2,4,6,9]`; `mape`/`signedBias` on a pred/actual pair with a hand-checkable value + a zero-actual skip case; `cusum` on a series with an injected step-change asserting `alarmsAt` matches; `paretoRank` cumulative-% correctness.
- [ ] **Step 3:** Implement each function. The golden fixture IS the correctness spec — match scipy.
- [ ] **Step 4:** Tests green; add a determinism test (each fn called twice → identical output; bootstrap with a fixed seed → identical).
- [ ] **Step 5:** Gates green. **Commit** (`feat(stats): TS-native statistics golden-tested against scipy`).

---

### Task 3: `extract` — episode/efficiency/rework/quality metrics

**Files:**
- Create: `src/analysis/episode-record.ts` (the TS `EpisodeRecord` type + `readEpisodeLedger(runDir)`), `src/analysis/extract-efficiency.ts`
- Test: `test/extract-efficiency.test.ts`

**Interfaces:**
- Consumes: Task 1 `synth-archive`, `paths`.
- Produces: `EpisodeRecord` TS interface (mirrors the Go contract — VERIFY casing against `episode/contract.gen.go`); `readEpisodeLedger(runDir): EpisodeRecord[]` (parse episodes.jsonl, tolerant of a blank/absent file → `[]`); `extractEfficiency(episodes): EfficiencyMetrics` = per-episode + rollups by `phase|kind|workerClass|targetRef` + whole-project totals of `{usageIn, usageOut, cacheRead, cacheCreate, streamedVsTerminalDivergence: {episodeId, delta}[], costUsd, numTurns, toolCallCounts, subagentCount, durationMs}`; `extractRework(episodes): ReworkMetrics` = per-target `{reworkEpisodes, reviewEpisodes, totalEpisodes, reworkRatio, firstPassSuccess: boolean}`; `extractQuality(acceptance): QualityMetrics` = `{buildOk, acceptancePassRate, testsPass, testsFailed, featureChecklist, soft: null}`.

- [ ] **Step 1: Failing test** for `readEpisodeLedger`: a synth episodes.jsonl with 3 records parses to 3 typed records; an absent file → `[]`; a file with one blank line + 2 records → 2 records.
- [ ] **Step 2:** Implement `episode-record.ts`. Green.
- [ ] **Step 3: Failing test** for `extractEfficiency`: build episodes with known usage/tool/turn values; assert per-workerClass and total rollups; assert **terminal `Usage` is used** (not `StreamedUsage`) and that when they differ the divergence is recorded (a synth episode with `Usage.Out=171`, `StreamedUsage.Out=2` → divergence entry, total uses 171). Assert `durationMs` from `endedAt-startedAt`.
- [ ] **Step 4:** Implement `extract-efficiency.ts` efficiency + rework + quality. Green.
- [ ] **Step 5: Failing test** for rework: episodes with `Kind` 1×Construction + 2×Rework + 1×Review on one `TargetRef` → `reworkRatio=3/4`, `firstPassSuccess=false`; a single succeeded Construction episode → `firstPassSuccess=true`. Quality: an `acceptance.json` with `passRate=0.8` maps through, `soft` stays null.
- [ ] **Step 6:** Implement to green. **Commit** (`feat(extract): efficiency + rework + quality metrics`).

---

### Task 4: `extract` — estimation-accuracy join + coverage + `bench extract` CLI

**Files:**
- Create: `src/analysis/extract-estimation.ts`, `src/analysis/extract.ts` (orchestrates all extract sub-metrics → metrics.json), `src/analysis/project-json.ts` (typed readers for the slots)
- Modify: `src/runner/cli.ts` (add the `extract` subcommand)
- Test: `test/extract-estimation.test.ts`, `test/extract.test.ts`

**Interfaces:**
- Consumes: Task 2 `mape`/`signedBias`, Task 3 extractors, Task 1 paths/schema.
- Produces: `readPredictedTokens(projectJson): {activity, workerClass, effortDays, predIn, predOut}[]` (join `slots[9].activities` × `slots[8].rateCard`; `predIn = effortDays × rateCard[wc].megatokensInPerDay × 1e6`); `extractEstimation(projectJson, episodes): EstimationMetrics` = pair predicted[activity] with actual (`Σ Usage` over episodes whose `Lineage.ActivityID===activity` OR `TargetRef===activity`), compute per-class + rollup `mape`/`signedBias`, and emit **unpaired gaps** (`predictedNoActual[]`, `actualNoPredicted[]`); `extractRun(runDir): MetricsBody` (runs all sub-extractors + a `coverage` block flagging which had real inputs) → writes `analysis/<b>/<runId>/metrics.json` (schema-valid); `bench extract <runId> --benchmark <b> [--out .]`.

- [ ] **Step 1: Failing test** for `readPredictedTokens` against the REAL dogfood `project.json` (`archistrator/.aiarch/state/project.json`): assert a known activity (e.g. from `slots[9]`) joins to its workerClass rateCard and computes `predIn = effortDays × megatokensInPerDay × 1e6`. (Copy a trimmed real slice into a fixture if reading the 876KB file in-test is slow; keep the values real.)
- [ ] **Step 2:** Implement `project-json.ts` + `readPredictedTokens`. Green.
- [ ] **Step 3: Failing test** for `extractEstimation`: synth predicted (2 activities) + synth episodes (actuals for 1) → MAPE/bias over the 1 paired activity, `predictedNoActual` names the other, `actualNoPredicted` empty; an episode with an activityId matching no prediction → `actualNoPredicted`. Assert MAPE uses Task 2's `mape` (not a reimplementation).
- [ ] **Step 4:** Implement `extract-estimation.ts`. Green.
- [ ] **Step 5: Failing test** for `extractRun` end-to-end on a `buildSynthRun` fixture: produces a schema-valid metrics.json under `analysis/`, NOT under `runs/`; the coverage block flags estimation as real-input=true when episodes+project present, false when episodes absent. CLI test: `bench extract` arg parse + it writes the file.
- [ ] **Step 6:** Implement `extract.ts` + wire the CLI subcommand. Gates green (incl. immutability gate — assert the test wrote nothing under `runs/`). **Commit** (`feat(extract): estimation-accuracy join + metrics.json + bench extract CLI`).

---

### Task 5: `detect` — statistics over comparable runs + `bench detect` CLI

**Files:**
- Create: `src/analysis/detect.ts`, `src/analysis/epoch.ts`, `src/analysis/finding.ts`
- Modify: `src/runner/cli.ts` (add `detect`)
- Test: `test/detect.test.ts`, `test/epoch.test.ts`

**Interfaces:**
- Consumes: Task 2 stats, Task 1 paths, extracted metrics.json files.
- Produces: `epochKey(runRecord): string` (=`${suiteVersion}::${operatorPolicyVersion}`); `groupByEpoch(metricsList): Map<epochKey, MetricsBody[]>`; `detect(benchmark, root): FindingsReport` reading all `analysis/<b>/*/metrics.json` + their runs' run.json for epoch, **refusing to compare across epochs** (each finding scoped to one epoch; a config-vs-config across epochs → an error/skip, not a silent mix). Findings: `concentration` (Pareto over token/failure by activity/gate/workerClass), `outliers` (MAD flags), `trend` (CUSUM across runs ordered by archistratorCommit/epoch position → drift/regression alarms), `configCompare` (when ≥2 commits present in an epoch: paired diff + Wilcoxon + bootstrap CI, runs as clusters). Every `Finding` carries `{kind, subject, effectSize, n, insufficientN: boolean, detail}`. Writes `analysis/<b>/findings.json`.

- [ ] **Step 1: Failing test** `epoch.test.ts`: two metrics with same suiteVersion+policyVersion group together; differing → separate; `detect` refuses a configCompare spanning two epochs (returns an error finding, not a merged stat).
- [ ] **Step 2:** Implement `epoch.ts`. Green.
- [ ] **Step 3: Failing test** for concentration + outliers: synth metrics where activity A holds 80% of tokens → Pareto ranks A first with cumPct; an outlier run flagged by MAD. n and effectSize present on each.
- [ ] **Step 4:** Implement those in `detect.ts` using Task 2 stats. Green.
- [ ] **Step 5: Failing test** for trend + configCompare: a synthesized series of runs with an injected downward token trend → CUSUM `trend` finding with an alarm; two commits × K runs each with a known mean difference → `configCompare` with the right Wilcoxon call and a bootstrap CI, `insufficientN:true` when K is tiny (e.g. K=2) and labeled. Assert small-n is LABELED, not suppressed.
- [ ] **Step 6:** Implement trend/configCompare + `findings.json` write + CLI `bench detect`. Gates green. **Commit** (`feat(detect): epoch-scoped statistics over runs + bench detect CLI`).

---

### Task 6: `hypothesize` — the LLM stage (schema-caged, claude CLI)

**Files:**
- Create: `src/analysis/hypothesize.ts`, `src/analysis/claude-cli.ts` (injectable claude invocation), `schema/hypothesis.schema.json`
- Modify: `src/runner/cli.ts` (add `hypothesize`)
- Test: `test/hypothesize.test.ts`

**Interfaces:**
- Consumes: Task 5 findings.json, the runs' traces for excerpts.
- Produces: `hypothesisSchema` (ajv) enforcing `{finding_ref, root_cause_narrative, proposed_change, metric, direction ("increase"|"decrease"), min_effect (number), predicted_mechanism}`; `selectTraceExcerpts(finding, runDir, budget): string` (the finding's episode(s) + their tool_use/result events from the per-episode `<id>.jsonl`, token-budgeted); `invokeClaude(prompt, {claudeBin, timeoutMs}): Promise<string>` (shells the `claude` CLI, `--output-format json`, injectable for tests); `hypothesize(benchmark, root, deps): HypothesesReport` — builds the prompt (findings + excerpts + the strict output contract), calls claude, **ajv-validates each returned hypothesis, rejects+retries (max 3) on malformed/free-prose**, writes `analysis/<b>/hypotheses.json`. On persistent failure → a recorded gap, not a crash.

- [ ] **Step 1: Failing test** with a FAKE `invokeClaude` (returns canned JSON): a well-formed response → validated hypotheses written; a free-prose response → rejected, retried, and on 3 failures a gap is recorded (no crash, no unvalidated output written).
- [ ] **Step 2:** Author `hypothesis.schema.json` + implement validation/retry loop with the injected fake. Green.
- [ ] **Step 3: Failing test** for `selectTraceExcerpts`: given a finding referencing an episodeId and a synth per-episode trace, the excerpt contains that episode's tool_use/result events and respects the budget (truncates, never exceeds).
- [ ] **Step 4:** Implement excerpt selection + `claude-cli.ts` (real invocation, but the default test uses the fake; a gated live test behind `SP3_LIVE_CLAUDE=1`). Wire CLI `bench hypothesize`. Green.
- [ ] **Step 5:** Gates green. **Commit** (`feat(hypothesize): schema-caged LLM hypothesis stage via claude CLI`).

---

### Task 7: `experiment` — pre-registration → declared test → verdict

**Files:**
- Create: `src/analysis/experiment.ts`, `schema/preregistration.schema.json`, `schema/verdict.schema.json`
- Modify: `src/runner/cli.ts` (add `experiment register|run|verdict`)
- Test: `test/experiment.test.ts`

**Interfaces:**
- Consumes: Task 6 hypotheses, Task 2 stats, SP2 `runBenchmark` (for `run`).
- Produces: `registerExperiment(hypothesisId, {K, test, alpha}, root): expId` → writes `analysis/<b>/experiments/<expId>/preregistration.json` (freezes `{hypothesis, metric, direction, minEffect, K, test, alpha, baselineCommit, treatmentCommit}`) — **frozen: `register` refuses to overwrite an existing preregistration** (like the harvest seal); `runExperiment(expId, deps)` drives SP2 `runBenchmark` K times at baseline and K at treatment (or records that the founder runs them), collecting their metrics.json; `verdictExperiment(expId, root): Verdict` reads the pre-registered plan + the collected run metrics, runs the **declared** test (e.g. Wilcoxon at the frozen α on the frozen metric/direction), writes `verdict.json` = `{outcome: "supported"|"refuted"|"inconclusive", statistic, pValue, effectSize, n, ranAt}`. A refuted/inconclusive verdict is written and KEPT (negative results are results).

- [ ] **Step 1: Failing test** for `registerExperiment`: writes a schema-valid preregistration freezing metric/direction/minEffect/K/test/alpha BEFORE any run; a second register of the same expId THROWS (frozen). Assert it's under `analysis/`, not `runs/`.
- [ ] **Step 2:** Author the two schemas + implement `registerExperiment`. Green.
- [ ] **Step 3: Failing test** for `verdictExperiment` with synth collected metrics: a treatment set with a clear improvement on the frozen metric at the frozen α → `supported`; a null difference → `refuted`/`inconclusive` (per the test + n); assert the verdict uses ONLY the pre-registered metric/direction/test/α (feeding a different metric changes nothing — no post-hoc fishing). Assert the declared Wilcoxon (Task 2) is what runs.
- [ ] **Step 4:** Implement `verdictExperiment`. Green.
- [ ] **Step 5: Failing test** for `runExperiment` with a FAKE `runBenchmark` (records call args): schedules K baseline + K treatment runs at the two commits, collects their metrics; a run failure still records what it can. Wire CLI `bench experiment register|run|verdict`.
- [ ] **Step 6:** Implement + wire CLI. Gates green. **Commit** (`feat(experiment): pre-registration + declared-test verdict + bench experiment CLI`).

---

### Task 8: End-to-end synthesized pipeline smoke + closeout

**Files:**
- Create: `test/integration/analysis-pipeline.test.ts`
- Modify: `README.md` (the SP3 CLI stages + the analysis/ tree), `archistrator/docs/superpowers/specs/2026-08-03-sp3-analysis-engine-design.md` (status closeout — the ONE sanctioned archistrator-repo edit, docs-only)

**Interfaces:**
- Consumes: all prior tasks.

- [ ] **Step 1: Failing integration test** (hermetic, no real archistrator/claude): synthesize 4 archived runs across 2 commits in a tmpdir; run `extract` on each → `detect` → `hypothesize` (fake claude) → `experiment register`+`verdict` (synth collected metrics); assert a coherent `analysis/` tree results, nothing was written under `runs/`, and the immutability classifier (`classifyRunsChanges`) reports zero offenses for the whole sequence.
- [ ] **Step 2:** Fix any integration seams surfaced. Green.
- [ ] **Step 3:** Full gates: `npm test`, `npm run typecheck`, `npm run lint`; confirm `npm run bench -- --help` (or a usage path) lists the new subcommands and loads (no ERR_MODULE_NOT_FOUND). Update README.
- [ ] **Step 4:** Amend the SP3 spec's status to "implemented"; record any deviations discovered. Commit the spec amendment in the **archistrator** repo (docs-only, the one sanctioned cross-repo edit) with the trailer.
- [ ] **Step 5:** Commit the bench repo (`chore: SP3 analysis engine complete — end-to-end synthesized pipeline green`).

---

## Explicitly deferred (per spec §11)

- Real end-to-end analysis (needs real archived runs) — AC iteration 0.
- GEPA-proper automated evolution on micro-benchmarks.
- Auto-applying a supported hypothesis — the human ratifies and commits the archistrator change.
- Soft-rubric LLM scoring in significance tests (recorded, never tested on).
- Analysis over gtd/archistrator (defined-unrun benchmarks).
- A Python runtime/CI dependency (scipy fixtures only, generated offline).
