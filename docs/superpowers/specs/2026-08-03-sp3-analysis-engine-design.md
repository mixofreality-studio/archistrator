# SP3 — Analysis Engine Design

- **Date:** 2026-08-03
- **Author:** Claude (driven), founder (ratified)
- **Status:** Approved design — ready for implementation planning
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
   twice or shares process; a plan-time detail.
