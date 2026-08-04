# SP3 — Analysis Engine Design

- **Date:** 2026-08-03
- **Author:** Claude (driven), founder (ratified)
- **Status:** Implemented (2026-08-03, `archistrator-bench` main — Tasks 1-8 of
  `docs/superpowers/plans/2026-08-03-sp3-analysis-engine.md`). See §13 for deviations discovered
  during implementation.
- **Repo:** `archistrator-bench` (the sibling bench repo; TS/ESM)
- **Supersedes:** refines §7 of `2026-08-02-self-improvement-pipeline-design.md` against the
  real SP1+SP2 data shapes and two founder rulings (2026-08-03): TS-native stats, all three goals.

## 1. Motivation

SP1 captures per-episode traces; SP2 archives every benchmark run immutably. SP3 is the
**analysis engine** that turns those archives into *falsifiable, human-ratified improvement
hypotheses* — the part the founder originally flagged as unknown ("how to analyse the data and
suggest improvements... as mathematical & scientific as possible; never deviate from hard
science to hand-wavy science"). Four CLI stages enforce the scientific method structurally:

```
extract <runId>   pure code    a run's evidence → metrics.json
detect <benchmark> statistics   many runs' metrics → findings.json (effect size + n, always)
hypothesize <b>    the ONE LLM  findings + trace excerpts → schema-enforced falsifiable hypotheses
experiment ...     the gate     HITL review → pre-registered experiment → declared test → verdict
```

**LLM proposes, math disposes.** The LLM may only emit falsifiable claims; it never computes or
touches a number. Every statistic is deterministic and reproducible.

## 2. Founder rulings (2026-08-03 brainstorm, after data-shape recon)

| Question | Ruling |
|---|---|
| Stats toolchain (§7 said Python+scipy; repos are all TS/Go) | **TS-native, golden-tested against scipy reference values.** Rigor preserved (the numbers ARE scipy's, pinned as fixtures); bench repo stays monolingual; CI stays simple. |
| Metric scope for v1 | **All three founder goals**: token/efficiency, estimation-accuracy (MAPE/bias), output-quality. Built + fixture-tested now; real numbers at AC iteration 0. |

## 3. Data reality (recon 2026-08-03 — what actually exists)

This design is grounded in what SP1+SP2 really produce, not §7's assumptions.

- **`runs/` is empty.** No real archived run exists yet. The detect stage and every cross-run
  metric are built and golden-tested against **synthesized archives + real trace fixtures**;
  first real numbers come from AC iteration 0 (the first real todomvc build). Same
  "built-now, validated-on-first-real-run" sequencing SP2 used.
- **Token/efficiency is real-shape:** the SP1 `EpisodeRecord` ledger (`traces/episodes.jsonl`) +
  real captured stream-json fixtures
  (`archistrator/server/internal/resourceaccess/agenticjob/testdata/streamjson/*.jsonl`) give
  the extractor genuine material for tokens/tool-calls/turns/cost/subagents/rework — real shape,
  synthetic volume until a real run.
- **Estimation-accuracy is half-real:** the *predicted* leg is fully derivable from the archived
  `project.json` (`slots[9].activities[].effortDays × slots[8].rateCard[workerClass].megatokens{In,Out}PerDay`,
  keyed by activity name); the *actual* leg comes from grouping the episode ledger by
  `Lineage.ActivityID` — which SP1 captures but only *populates during a real construction run*.
  MAPE/bias is computable by design, real only after a real run. (The 2026-07-20 calibration
  spec's per-activity accumulator / `.calibration` slot were never built and are **not needed** —
  SP3 groups the ledger directly, the analysis-owned path.)
- **Construction rework/gate-failure is real only post-run:** design-artifact rework
  (`slots.revisions` + review threads) is computable from real `project.json` today;
  construction rework needs the populated ledger. The extractor computes whatever's present and
  **labels coverage** rather than faking it.
- **`run.json` drops the per-run construction pass/fail tally** (`activitySummary` is not
  persisted — SP2 recon finding); extract re-derives it from `project.json.activityConstruction`.
- **No Python anywhere** (confirmed) — the TS-native ruling is greenfield, no conflict.

## 4. Placement & the evidence/analysis split

SP3 lives in `archistrator-bench`, extending the `bench` CLI. **Outputs go in a separate
`analysis/` tree, never inside the immutable `runs/`** — matching the evidence-vs-analysis
volatility split the whole pipeline is built on (audit-spec §4):

```
archistrator-bench/
  runs/<benchmark>/<runId>/       # SEALED evidence — SP3 only READS it, never writes
  analysis/
    <benchmark>/
      <runId>/metrics.json        # extract output — freely regenerable, NOT immutability-gated
      findings.json               # detect output (per epoch)
      hypotheses.json             # hypothesize output (schema-enforced)
      experiments/<expId>/
        preregistration.json      # frozen BEFORE any run
        verdict.json              # supported | refuted | inconclusive
  src/analysis/                   # the four stages
  src/stats/                      # TS-native vetted statistics (golden-tested vs scipy)
  test/stats/fixtures/            # scipy-computed reference values, pinned
```

`metrics.json` validates the existing `schema/metrics.schema.json` envelope (SP3 defines the
body). Analysis is deterministic and idempotent: re-running a stage over the same sealed inputs
yields byte-identical output.

## 5. Stage 1 — `extract <runId>` (pure code, zero LLM)

Reduces one archived run to a typed `metrics.json`. Inputs (all read-only from the sealed run):
`traces/episodes.jsonl` (`EpisodeRecord[]`) + per-episode `<id>.jsonl` (raw stream-json),
`project.json`, `acceptance/acceptance.json`, `run.json`.

**Token/efficiency** (per episode, rolled up per phase / episode-kind / worker-class / target,
plus whole-project totals): usage `{in,out,cacheRead,cacheCreate}` — **prefer the authoritative
terminal `Usage` over summed `StreamedUsage`** (the SP1 parser gotcha: a turn's `output_tokens`
is partial); record both + a divergence flag exactly as SP1's UI does. Plus `costUsd`,
`numTurns`, `toolCallCounts` distribution, subagent-span counts, durations (`endedAt−startedAt`).

**Rework & first-pass:** count `EpisodeKind ∈ {Review(2), Rework(3)}` grouped by `TargetRef` /
`Lineage.ActivityID` → rework ratio per activity; first-pass-success = activity with a single
succeeded construction episode and no rework.

**Gate/validation failures:** episode `Outcome=Failed` grouped by target; design-artifact
`slots.revisions > 1` + `reviewThread[]` status/round. Coverage-labeled (construction-blind for
real data until the ledger is populated).

**Estimation-accuracy:** predicted[activity] = `effortDays × rateCard[workerClass].MTok/day`
(join `slots[9].activities` + `slots[8].rateCard`, keyed by activity name); actual[activity] =
`Σ EpisodeRecord.Usage` over episodes with `Lineage.ActivityID == activity`. MAPE + signed bias
per worker-class and rollup. **Unpaired activities → explicit gaps** (predicted-but-no-episodes,
or episode-with-no-matching-activity), never silently dropped.

**Quality:** from `acceptance.json` — `buildOk`, `acceptancePassRate`, `testsPass/Failed`,
per-feature checklist; `soft.rubricScores` stays `null`, labeled soft (never in significance
tests).

**Re-derived:** per-run construction pass/fail from `project.json.activityConstruction` (since
`run.json` drops it).

Output: `analysis/<b>/<runId>/metrics.json`, schema-valid, with a coverage block naming which
metrics had real vs absent inputs.

## 6. Stage 2 — `detect <benchmark>` (deterministic statistics, zero LLM)

Over all runs of a benchmark **in the same epoch** (epoch = acceptance `suiteVersion` +
`operatorPolicyVersion`; cross-epoch comparisons refused — the detector errors rather than
mixing incomparable runs). Produces `analysis/<b>/findings.json`. Statistics (all in `src/stats/`,
each golden-tested against scipy reference values pinned in `test/stats/fixtures/`):

- **Concentration:** Pareto ranking of token/failure concentration by activity, gate, worker-class.
- **Outliers:** median-absolute-deviation robust flags.
- **Trend/regression across iterations:** CUSUM control charts (drift/regression the eye misses).
- **Config-vs-config** (archistrator commit A vs B): paired differences with runs as clusters,
  Wilcoxon signed-rank at small n, bootstrap confidence intervals.

Every finding carries **effect size and n**; insufficient-n findings are labeled exactly that —
never suppressed, never dressed up. Methodological basis: Anthropic's "Adding Error Bars to
Evals" (arXiv:2411.00640).

**TS-native stats + scipy golden tests:** each stat function (`wilcoxonSignedRank`, `bootstrapCI`,
`cusum`, `mad`, `mape`, `signedBias`, `paretoRank`, `pairedDiff`) is golden-tested against
reference values computed once with scipy offline and committed as JSON fixtures. A one-off
`scripts/gen-stats-fixtures.py` (run manually, its scipy output pinned; Python is NOT a runtime
or CI dependency) documents provenance. Rigor is scipy's; the toolchain stays TS.

## 7. Stage 3 — `hypothesize <benchmark>` (the only LLM stage)

Feeds the flagged findings + relevant trace excerpts to Claude via the **`claude` CLI**
(subscription auth, no API key — consistent with how the SP2 runner already uses it), and gets
back **schema-enforced structured hypotheses**:

```
Hypothesis {
  finding_ref, root_cause_narrative,
  proposed_change,        # file / prompt / skill-level, concrete
  metric, direction, min_effect,
  predicted_mechanism
}
```

Output is ajv-validated against the hypothesis schema; free-prose or malformed output is rejected
and retried (bounded). This cages GEPA's reflective insight (arXiv:2507.19457) to *proposing
falsifiable claims only* — the LLM never computes or touches a number. Output:
`analysis/<b>/hypotheses.json`. Future work: GEPA-proper evolution on micro-benchmarks — not v1.

## 8. Stage 4 — `experiment` (the scientific-method gate, zero LLM)

The founder reviews hypotheses (the HITL step). An accepted hypothesis becomes a
**pre-registered experiment**: `bench experiment register <hypothesisId>` writes
`analysis/<b>/experiments/<expId>/preregistration.json` freezing metric, direction, minimum
effect, K paired runs, test, and α **before any run happens**. The SP2 runner executes
baseline-vs-treatment (`bench experiment run <expId>` orchestrates the paired runs at the two
archistrator commits); `bench experiment verdict <expId>` runs the **declared** test over the
archived run metrics and writes `verdict.json`: **supported | refuted | inconclusive**. Refuted
hypotheses stay in the ledger forever — negative results are results.

**Operating mode** (founder ruling, carried from the pipeline spec): N=1 iterate is the default
(fast; the detector pools variance across accumulated runs and refuses significance claims it
can't back); **validation mode** runs the K-paired-run proof on demand.

## 9. CLI surface (extends SP2's `bench`)

```
bench extract <runId> [--benchmark todomvc]        # → analysis/<b>/<runId>/metrics.json
bench detect <benchmark> [--epoch auto]            # → analysis/<b>/findings.json
bench hypothesize <benchmark>                      # → analysis/<b>/hypotheses.json (claude CLI)
bench experiment register <hypothesisId>           # → preregistration.json (frozen)
bench experiment run <expId>                       # drives SP2 baseline-vs-treatment paired runs
bench experiment verdict <expId>                   # → verdict.json (declared test)
```

## 10. Testing

- **`src/stats/`**: golden tests against scipy-pinned reference values (Wilcoxon, bootstrap CIs,
  CUSUM on synthetic drift, MAD, MAPE/bias). The pinned fixtures ARE the hard-science guarantee.
- **`extract`**: golden-tested against the real captured stream-json fixtures + the real dogfood
  `project.json` (predicted-leg + design-rework paths); the divergence-flag, unpaired-gap, and
  coverage-labeling paths each covered.
- **`detect` / `experiment`**: tested against **synthesized archives** (multiple synthetic
  metrics.json sets with known statistical properties — e.g. a known drift the CUSUM must catch,
  a known paired difference the Wilcoxon must call at the right α). Cross-epoch refusal tested.
- **`hypothesize`**: schema-validation + rejection/retry tested with a faked claude-CLI transport
  (no live LLM in the default suite); a gated live test behind an env flag.
- First real end-to-end (run → traces → metrics → findings) validated at AC iteration 0.

## 11. Consent seam & non-goals

- **Consent filter** at the ingest boundary: `extract` honors `run.json.learningConsent` (bench
  runs = true); the one-line filter is the whole opt-in seam (per pipeline spec §3 — infra
  deferred).
- **Non-goals (v1):** GEPA-proper automated evolution (micro-benchmark future work); a Python
  runtime/CI dependency (fixtures only); soft-rubric LLM scoring in significance tests (recorded,
  never tested on); analysis over gtd/archistrator (defined-unrun benchmarks); auto-applying a
  supported hypothesis (the human ratifies and commits the archistrator change — the loop stays
  human-gated).

## 12. Open questions

1. **Trace-excerpt selection for hypothesize** — which slices of a (potentially large) trace to
   feed the LLM. Default: the flagged finding's episode(s) + their tool_use/result events,
   token-budgeted. Refine when real traces exist.
2. **Epoch auto-detection vs explicit** — `--epoch auto` groups by `(suiteVersion,
   operatorPolicyVersion)`; confirm the grouping key once real runs accumulate.
3. **Experiment `run` orchestration** — whether `bench experiment run` shells the SP2 `bench run`
   twice or shares process; a plan-time detail. **Resolved (Task 7):** `bench experiment run`
   drives SP2's real `runBenchmark` in-process against two already-resolved archistrator
   checkouts (`--baseline-archistrator`/`--treatment-archistrator`) — in-process
   commit-switching within one checkout was ruled out of scope for v1.

## 13. Implementation status & deviations (2026-08-03)

All 8 plan tasks landed on `archistrator-bench` main (277 tests, `npm test`/`typecheck`/`lint`
green), closed out by Task 8's hermetic end-to-end synthesized-pipeline smoke
(`test/integration/analysis-pipeline.test.ts`): 4 synthesized runs across 2 commits driven through
`extract` → `detect` → `hypothesize` (fake `claude`) → `experiment register`+`verdict`, landing a
coherent `analysis/` tree while `runs/**` stays byte-for-byte untouched — verified both by a
structural before/after snapshot and by running the real CI immutability classifier
(`classifyRunsChanges`) over a real `git diff` of the whole sequence (zero offenses). Real
end-to-end analysis against genuine archived runs remains deferred to AC iteration 0, per §11 —
unchanged.

Deviations found and ruled during implementation (none blocking; recorded here so a future reader
of this design doc isn't surprised by the code):

- **`episodes.jsonl` lives under `traces/`, not the run dir's top level.** §5 (and this doc's
  original tree sketch) didn't spell out the exact path; the real SP2 `harvest.ts` copies the
  gitignored `.aiarch/traces/` sidecar wholesale into `<runDir>/traces/`, so the episode ledger
  (`traces/episodes.jsonl`) sits alongside the per-episode raw traces
  (`traces/<episodeId>.jsonl`) as siblings, not at the run archive's root. A Task 6 cross-task
  bug (the reader and the test fixture both agreed on the wrong top-level path, so tests passed
  while a real run would have read zero episodes) was caught and fixed in both
  `readEpisodeLedger` and `test/support/synth-archive.ts` together — see `episode-record.ts`'s
  header for the full account.
- **The "gate" concentration dimension uses `featureChecklist` ids, not §5's
  "episode `Outcome=Failed` grouped by target" grouping.** That episode-level grouping was never
  built into the extractor (Tasks 3-4 only computed rework via `Kind ∈ {Review, Rework}` and
  repeat-target counts, not a Failed-outcome-by-target rollup) — `detect.ts`'s concentration
  finding instead pools acceptance-suite checklist failures. This is a real coverage gap for
  construction-time gate failures specifically (construction is Outcome-blind until a real run
  populates the ledger); the design-artifact signal (`slots.revisions > 1`) IS captured
  separately via extract's rework metrics. Triaged as accept-for-v1 rather than block: the
  featureChecklist dimension is genuinely useful on its own, and the deeper grouping needs real
  construction-run data to validate against anyway.
- **`MIN_PAIRS = 3`** is a fixed v1 constant in `experiment.ts` (below which `verdictExperiment`
  returns `inconclusive` rather than computing the declared test) — it is not itself part of the
  frozen `preregistration.json`. Documented as a v1 simplification, not pre-registered per-run.
- **CUSUM's target is a baseline-window mean (the first half of the chronologically-ordered
  series), not the whole series' mean**, which §6 left unspecified. Using the full series' mean
  would self-contaminate: a real sustained shift pulls the mean toward itself, dampening the
  alarm on the shifted points while risking a spurious alarm on the actually-stable baseline
  points. Ruled an improvement over the naive reading, not a deviation to walk back.
- **`hypothesize`'s trace-excerpt token budget (`excerptBudget`) is a character-count proxy**, not
  a real tokenizer — deterministic and dependency-free, the same "no external tokenizer" posture
  `src/stats/` takes toward scipy (rigor pinned via fixtures, not a runtime dependency). Open
  question 1 above ("refine when real traces exist") still applies.

### Final-review fix wave (2026-08-03)

Four small findings from a whole-repo review; all fixed except F5/F6, which are earmarked (see
their own notes). F1-F3 landed on `archistrator-bench` main in one commit, `npm
test`/`typecheck`/`lint` green.

- **F1 (fixed).** `verdictExperiment` conflated "not significant" with "refuted": the exact
  Wilcoxon's minimum achievable two-sided p at `m` nonzero paired differences is `2/2^m`, so at
  small `m` (e.g. the v1 `MIN_PAIRS=3` floor, where the floor is 0.25) `p<alpha` is structurally
  unreachable — every underpowered experiment was reported "refuted" regardless of data, dressing
  up insufficient n as a result. Fixed to a three-way outcome: `inconclusive` (reason
  "underpowered...") when `m < MIN_PAIRS` or `minAchievableP > alpha`; `supported` when
  significant AND the movement meets `minEffect` in the frozen direction; `refuted` only when
  adequately powered (`minAchievableP <= alpha`) AND not supported — a genuine negative result.
  `schema/verdict.schema.json`'s `outcome` description updated to match.
- **F2 (fixed).** `extractRun` never honored `run.json.learningConsent` (§11: "the one-line filter
  is the whole opt-in seam"). Fixed: `learningConsent === false` now writes a minimal
  consent-withheld `metrics.json` stub (envelope + one `coverage` entry) instead of computing the
  full body. Bench runs are always `true`, so the happy path is unchanged.
- **F3 (fixed).** The written `metrics.json` failed `schema/metrics.schema.json` (the envelope
  requires `runId`/`benchmark`/`schemaVersion`; `extractRun` wrote body-only). Fixed: `extractRun`
  now writes all three envelope fields (`schemaVersion: "v1"`) alongside the body, so the same
  document validates against both `metrics.schema.json` and `metrics-body.schema.json`.
- **F4 (already true, now documented).** `findings.json` (`detect.ts`) and `hypotheses.json`
  (`hypothesize.ts`) each carry a `generatedAt` wall-clock stamp (`new Date().toISOString()`), so
  those two outputs are NOT byte-identical on re-run even over identical inputs — deliberate,
  already noted inline at `detect.ts`'s `generatedAt` field ("byte-identical CIs (only
  `generatedAt`'s timestamp differs)"). `extract` and `verdictExperiment` remain deterministic
  (`verdictExperiment`'s `ranAt` is derived from the collected runs' own `endedAt`, never
  `Date.now()` — see `experiment.ts`'s module doc). No code change; recorded here so a future
  reader doesn't mistake the two timestamped outputs for a determinism bug.
- **F5 (earmarked, not built).** §9's `--epoch` flag was never implemented in
  `parseDetectArgs`/`cli.ts` — `detect`'s ambiguous-epoch refusal message ("scope detect to a
  single epoch to compare them") names a remedy the CLI does not yet expose. Earmark: add
  `--epoch` to `bench detect`, or reword the refusal message to point at an actionable remedy.
- **F6 (earmarked, folds into parked experiment-run hardening for AC iteration 0).** `bench
  experiment run` (`parseExperimentRunArgs` / `runExperiment`) does not cross-check the supplied
  `--baseline-archistrator`/`--treatment-archistrator` checkouts' actual commits against the
  frozen `preregistration.json`'s `baselineCommit`/`treatmentCommit` — a caller could point either
  flag at a checkout on the wrong commit and `runExperiment` would silently drive it anyway. Folds
  into the parked experiment-run hardening already deferred to AC iteration 0 (§10: "First real
  end-to-end ... validated at AC iteration 0").
