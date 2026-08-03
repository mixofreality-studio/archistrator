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
- **`analysis/**` is COMMITTED to the bench repo** (regenerable but versioned — `preregistration.json`'s freeze and "refuted hypotheses stay forever" are only tamper-evident under version control; register-refuses-overwrite alone doesn't survive a delete-and-rewrite, git history does). Do NOT gitignore `analysis/`.
- Commit after every task; messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Gates before each commit: `npm test` (full suite green), `npm run typecheck`, `npm run lint`.

## Data shapes (derived from recon — treat as ground truth; verify casing against the cited source before parsing)

- **Ledger line = `storedEpisode` WRAPPER, not a bare record** (verified: `episodeaccess.go:146-149` marshals `{ProjectID string, Record EpisodeRecord}` at `:202`; harvest copies `traces/` verbatim, `harvest.ts:305-310`). So each `runs/<b>/<runId>/traces/episodes.jsonl` line is `{"ProjectID":"...","Record":{...}}`. `readEpisodeLedger` MUST unwrap `.Record`, dedupe by `Record.EpisodeID` last-wins, and skip blank/unparseable lines (mirroring `ListEpisodes`/`readLedgerLocked` tolerance, `episodeaccess.go:231-271,356-385`). **`buildSynthRun` serializes the SAME wrapper** or every downstream test builds on the wrong shape. `EpisodeRecord` fields (PascalCase, json tags per `episode/contract.gen.go:22-75` — casing confirmed correct; it's the nesting that matters): `EpisodeID string`, `Kind int` (0 Design, 1 Construction, 2 Review, 3 Rework, 4 Answer), `TargetRef string` (artifactKind for design, **Method activity id** e.g. `"C-CW"` for construction), `Lineage {WorkflowID, RunID string, ActivityID *string}`, `WorkerClass *string` (**unset on construction episodes** — `constructactivity.go:315-317`), `Model *string`, `Usage {In, Out, CacheRead, CacheCreate int64}` (already-computed summary — extract does NOT re-parse raw stream-json), `StreamedUsage *{...same...}`, `CostUSD *number`, `NumTurns *int64`, `ToolCallCounts map[string]int64`, `SubagentSpans [{ToolUseID string, StartedAt, EndedAt *string}]`, `StartedAt, EndedAt string(RFC3339)`, `Outcome int` (0 Succeeded, 1 Failed, 2 Cancelled, 3 Gap), `GapReason *string`, `TracePath *string`.
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
- Produces (pure functions, deterministic): `mean(xs)`, `median(xs)`, `mad(xs)` (median absolute deviation, scaled ×1.4826), `mape(pred, actual): {value, skipped}` (mean absolute percentage error over paired arrays, skipping pairs where actual==0 with the skip count reported), `signedBias(pred, actual): number` (mean signed relative error), `pairedDiff(a, b): number[]` (elementwise a−b), `wilcoxonSignedRank(a, b, {zeroMethod:"wilcox", alternative:"two-sided"}): {statistic, pValue}` (the PAIRED test — exact for small n, normal-approx w/ continuity correction larger; **zeroMethod + alternative are pinned and must match the fixture generator's scipy call exactly**, since scipy's defaults on zeros/ties change the result), `mannWhitneyU(a, b, {alternative:"two-sided"}): {statistic, pValue}` (the UNPAIRED rank-sum test, for run-level comparisons with no pairing unit), `bootstrapCI(xs, {statistic, iters, alpha, seed}): {lo, hi}` (percentile bootstrap, SEEDED PRNG — pass the seed in, no `Math.random`), `clusterBootstrapCI(clusters: number[][], {statistic, iters, alpha, seed}): {lo, hi}` (resamples whole CLUSTERS/runs, seeded — this is the "runs as clusters" CI), `cusum(xs, {target, k, h}): {positive: number[], negative: number[], alarmsAt: number[]}` (tabular CUSUM), `paretoRank(items: {key, value}[]): {key, value, cumPct}[]` (descending, cumulative %).

- [ ] **Step 1: Write `scripts/gen-stats-fixtures.py`** — for a handful of pinned input arrays, compute reference outputs. **scipy** for the tests it actually has: `scipy.stats.wilcoxon(a, b, zero_method="wilcox", alternative="two-sided")`, `scipy.stats.mannwhitneyu(a, b, alternative="two-sided")`, `scipy.stats.bootstrap`, `numpy` for mean/median/MAD/mape/signedBias. **`cusum` and `paretoRank` have NO scipy equivalent** — generate their references with a small independent numpy implementation in the same script and label them in the fixture header as `"reference: hand-derived numpy (no scipy equivalent)"`. Write `test/stats/fixtures/<fn>.json` = `[{input, params, expected}]` (params records `zero_method`/`alternative` so the TS call provably matches). Run once locally (scipy in an ephemeral venv), commit the JSON. File header: "run manually to regenerate; Python/scipy is NOT a project or CI dependency."
- [ ] **Step 2: Failing tests** `stats.test.ts`: for each function, load its fixture and assert the TS output matches `expected` within tolerance (`1e-9` exact stats; `1e-2` relative for bootstrap CIs given PRNG differences — asserted for coverage/width sanity + a determinism test: same seed → identical twice). Include: `wilcoxon` with the pinned `zeroMethod/alternative` matching the fixture's `params`; `mannWhitneyU` on a known scipy p-value; `mad` on `[1,1,2,2,4,6,9]`; `mape`/`signedBias` with a hand-checkable value + a zero-actual skip case (skip count asserted); `cusum` on an injected step-change asserting `alarmsAt`; `paretoRank` cumulative-%; `clusterBootstrapCI` determinism + that resampling clusters (not points) widens the CI vs a naive point bootstrap on the same flattened data.
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
- Produces: `EpisodeRecord` + `StoredEpisode` TS interfaces (mirror the Go contract — the ledger line is `StoredEpisode = {ProjectID, Record: EpisodeRecord}`, see Data shapes); `readEpisodeLedger(runDir): EpisodeRecord[]` (parse episodes.jsonl, **unwrap `.Record`**, dedupe by `EpisodeID` last-wins, tolerant of blank/absent file → `[]`); `extractEfficiency(episodes): EfficiencyMetrics` = per-episode + rollups by `kind|workerClass|targetRef` + whole-project totals of `{usageIn, usageOut, cacheRead, cacheCreate, streamedVsTerminalDivergence: {episodeId, delta}[], costUsd, numTurns, toolCallCounts, subagentCount, durationMs}` — **`workerClass` is null on construction episodes; bucket those under a `"(unset)"` key and label it, never drop them**; `extractRework(episodes): ReworkMetrics` = per-target both signals: `kindRework` (count `Kind∈{Review(2),Rework(3)}` — design targets only, since the construction rail stamps every episode `Construction`, `constructactivity.go:298-301`) AND `repeatRework` (episodes-per-`TargetRef` > 1 = the construction rework signal), plus `totalEpisodes, reworkRatio (repeat-based for construction), firstPassSuccess: boolean`; `extractQuality(acceptance): QualityMetrics` = `{buildOk, acceptancePassRate, testsPass, testsFailed, featureChecklist, soft: null}`.

- [ ] **Step 1: Failing test** for `readEpisodeLedger`: a synth episodes.jsonl with 3 **`{ProjectID, Record}` wrapper** lines parses+unwraps to 3 typed `EpisodeRecord`s; an absent file → `[]`; a blank line + 2 records → 2; a duplicate `EpisodeID` → last-wins (1). (If a test line were a bare record without the wrapper, unwrap must yield undefined and be skipped — assert the wrapper is required.)
- [ ] **Step 2:** Implement `episode-record.ts`. Green.
- [ ] **Step 3: Failing test** for `extractEfficiency`: build episodes with known usage/tool/turn values; assert per-workerClass and total rollups; assert **terminal `Usage` is used** (not `StreamedUsage`) and that when they differ the divergence is recorded (a synth episode with `Usage.Out=171`, `StreamedUsage.Out=2` → divergence entry, total uses 171). Assert `durationMs` from `endedAt-startedAt`.
- [ ] **Step 4:** Implement `extract-efficiency.ts` efficiency + rework + quality. Green.
- [ ] **Step 5: Failing test** for rework — TWO signals: (design target) episodes `Kind` 2×Rework + 1×Review + a Draft on one artifact `TargetRef` → `kindRework=3`; (construction target) 3 `Construction` episodes on one activity `TargetRef` → `repeatRework` true, `reworkRatio=2/3` (repeats beyond the first), `firstPassSuccess=false`; a single succeeded Construction episode → `firstPassSuccess=true`, `repeatRework` false. Assert construction `kindRework` is structurally 0 (the rail never emits Rework/Review kinds). Quality: `acceptance.json` `passRate=0.8` maps through, `soft` stays null.
- [ ] **Step 6:** Implement to green. **Commit** (`feat(extract): efficiency + rework + quality metrics`).

---

### Task 4: `extract` — estimation-accuracy join + coverage + `bench extract` CLI

**Files:**
- Create: `src/analysis/extract-estimation.ts`, `src/analysis/extract.ts` (orchestrates all extract sub-metrics → metrics.json), `src/analysis/project-json.ts` (typed readers for the slots)
- Modify: `src/runner/cli.ts` (add the `extract` subcommand)
- Test: `test/extract-estimation.test.ts`, `test/extract.test.ts`

**Interfaces:**
- Consumes: Task 2 `mape`/`signedBias`, Task 3 extractors, Task 1 paths/schema.
- Produces: `readPredictedTokens(projectJson): {activity, workerClass, effortDays, predIn, predOut}[]` (join `slots[9].activities` × `slots[8].rateCard`; `predIn = effortDays × rateCard[wc].megatokensInPerDay × 1e6`; `activity` = `activities[].name`, the join key); `extractEstimation(projectJson, episodes): EstimationMetrics` = pair predicted[activity] with actual, where **the actual leg is scoped to `Kind===1 (Construction)` episodes** (design/answer episodes are outside the estimation universe — NOT `actualNoPredicted` noise; filter them out), summing `Usage` over construction episodes whose `Lineage.ActivityID===activity` (fallback `TargetRef===activity`). **Per-class MAPE/bias takes the workerClass from the PREDICTED leg** (`activities[].workerClass`), never from the episode (episodes leave it unset). Emit unpaired gaps: `predictedNoActual[]` (activity predicted but no construction episodes) and `actualNoPredicted[]` (a construction episode whose activity matches no prediction). Use Task 2's `mape`/`signedBias` — do not reimplement. `extractRun(runDir): MetricsBody` (all sub-extractors + a `coverage` block flagging real-vs-absent inputs) → writes `analysis/<b>/<runId>/metrics.json` (schema-valid); `bench extract <runId> --benchmark <b> [--out .]`.
- **CLI restructure (do this FIRST, before adding the subcommand):** `src/runner/cli.ts`'s `parseArgs` currently THROWS on any command other than `run` (`cli.ts:63-68`). Refactor `parseArgs`/`main` into a subcommand dispatch (`run` | `extract`, extended by Tasks 5-7) before wiring `extract`. Keep the existing `run` behavior byte-identical (its tests must stay green).

- [ ] **Step 1: Failing test** for `readPredictedTokens` against a **trimmed REAL slice** copied into `test/fixtures/project-json-slice.json` (the primary path — a few real `slots[9].activities` entries + the `slots[8].rateCard`, real values from `archistrator/.aiarch/state/project.json`; do NOT read the 876KB sibling file in the default suite — a sibling-path read may exist only behind an env-gated test since it breaks bench-alone CI checkout). Assert a known activity (e.g. `C-CW`) joins to its workerClass rateCard and `predIn = effortDays × megatokensInPerDay × 1e6`.
- [ ] **Step 2:** Implement `project-json.ts` + `readPredictedTokens`. Green.
- [ ] **Step 3: Failing test** for `extractEstimation`: synth predicted (2 activities) + synth **construction** episodes (actuals for 1) + a synth **design** episode (Kind=0, TargetRef=an artifactKind) → MAPE/bias over the 1 paired activity, `predictedNoActual` names the other, `actualNoPredicted` EMPTY (the design episode is filtered out, not counted as unpaired); a construction episode with an activityId matching no prediction → `actualNoPredicted`. Assert per-class uses the predicted-leg workerClass. Assert MAPE uses Task 2's `mape`.
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
- Produces: `epochKey(runRecord): string` (=`${suiteVersion}::${operatorPolicyVersion}`); `groupByEpoch(metricsList): Map<epochKey, MetricsBody[]>`; `detect(benchmark, root): FindingsReport` reading all `analysis/<b>/*/metrics.json` + their runs' run.json for epoch, **refusing to compare across epochs** (each finding scoped to one epoch; a config-vs-config across epochs → an error/skip, not a silent mix). Findings: `concentration` (Pareto over token/failure by activity/gate/workerClass), `outliers` (MAD flags), `trend` (CUSUM across runs ordered by archistratorCommit/epoch position → drift/regression alarms), `configCompare` (when ≥2 commits present in an epoch). Every `Finding` carries `{kind, subject, effectSize, n, insufficientN: boolean, detail}`. Writes `analysis/<b>/findings.json`.

**`configCompare` statistical design (corrected — the pairing unit is the ACTIVITY, not the run).** Independent replicate runs have no pairing key, so index-pairing run *i* of config A with run *i* of config B is invalid pseudo-pairing. Correct design (matches the spec's "paired differences with runs as clusters"): the **paired unit is the Method activity** (or the feature-checklist item, for quality metrics) — `diff(activity) = metric_A(activity) − metric_B(activity)`, where `metric_X(activity)` aggregates that activity's value across config X's runs; run `wilcoxonSignedRank` over the per-activity differences; compute the CI via `clusterBootstrapCI` resampling whole RUNS (seeded). If a metric has NO natural pairing unit (a single run-level scalar per config, e.g. total tokens), use the UNPAIRED `mannWhitneyU` instead — never index-paired signed-rank. The finding records which test/pairing-unit was used.

- [ ] **Step 1: Failing test** `epoch.test.ts`: two metrics with same suiteVersion+policyVersion group together; differing → separate; `detect` refuses a configCompare spanning two epochs (returns an error finding, not a merged stat).
- [ ] **Step 2:** Implement `epoch.ts`. Green.
- [ ] **Step 3: Failing test** for concentration + outliers: synth metrics where activity A holds 80% of tokens → Pareto ranks A first with cumPct; an outlier run flagged by MAD. n and effectSize present on each.
- [ ] **Step 4:** Implement those in `detect.ts` using Task 2 stats. Green.
- [ ] **Step 5: Failing test** for trend + configCompare: a synthesized series of runs with an injected downward token trend → CUSUM `trend` finding with an alarm; two commits, each with K runs over a shared set of activities with a known per-activity mean difference → `configCompare` running `wilcoxonSignedRank` over the **per-activity differences** (paired unit = activity, NOT run-index) + `clusterBootstrapCI` over runs, `insufficientN:true` when the activity count / K is tiny and LABELED (not suppressed). Add a run-level-scalar metric case → asserts `mannWhitneyU` (unpaired) is used, not signed-rank.
- [ ] **Step 6:** Implement trend/configCompare + `findings.json` write + CLI `bench detect`. Gates green. **Commit** (`feat(detect): epoch-scoped statistics over runs + bench detect CLI`).

---

### Task 6: `hypothesize` — the LLM stage (schema-caged, claude CLI)

**Files:**
- Create: `src/analysis/hypothesize.ts`, `src/analysis/claude-cli.ts` (injectable claude invocation), `schema/hypothesis.schema.json`
- Modify: `src/runner/cli.ts` (add `hypothesize`)
- Test: `test/hypothesize.test.ts`

**Interfaces:**
- Consumes: Task 5 findings.json, the runs' traces for excerpts.
- Produces: `hypothesisSchema` (ajv) enforcing `{finding_ref, root_cause_narrative, proposed_change, metric, direction ("increase"|"decrease"), min_effect (number), predicted_mechanism}`; `selectTraceExcerpts(finding, runDir, budget): string` (the finding's episode(s) + their tool_use/result events from the per-episode `<id>.jsonl`, token-budgeted); `invokeClaude(prompt, {claudeBin, timeoutMs}): Promise<string>` (shells the `claude` CLI in **print mode**: `claude -p <prompt> --output-format json` — the `-p` flag is what makes it a one-shot non-interactive invocation; ambient-subscription auth, no API key, same as the SP2 runner's tool assumption; injectable for tests); `hypothesize(benchmark, root, deps): HypothesesReport` — builds the prompt (findings + excerpts + the strict output contract), calls claude, **ajv-validates each returned hypothesis, rejects+retries (max 3) on malformed/free-prose**, writes `analysis/<b>/hypotheses.json`. On persistent failure → a recorded gap, not a crash.

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
