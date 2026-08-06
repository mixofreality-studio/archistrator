# SP4 — Metrics Dashboard Design

- **Date:** 2026-08-05
- **Author:** Claude (driven), founder (ratified)
- **Status:** Implemented (2026-08-05, `archistrator-bench` main — Tasks 1-8 of
  `docs/superpowers/plans/2026-08-05-sp4-dashboard.md`). See §10 for deviations discovered
  during implementation.
- **Repo:** `archistrator-bench` (the sibling bench repo)
- **Refines:** §8 of `2026-08-02-self-improvement-pipeline-design.md`, grounded in the real
  SP2 (`runs/`) + SP3 (`analysis/`) output shapes (recon 2026-08-05).

## 1. Motivation

The pipeline produces per-run metrics, cross-run findings, hypotheses, and experiment verdicts
as JSON under `runs/**` and `analysis/**`. SP4 is the **human UI over that data** — the thing the
founder opens to compare runs, watch trends across iterations ("token cost going down"), and read
the science ledger. It is the human-facing companion to what the analysis engine computes; it
performs **no analysis itself** (SP3 owns every number) — it aggregates, renders, and honestly
labels.

## 2. Founder rulings (2026-08-05 brainstorm, after data recon)

| Question | Ruling |
|---|---|
| Data feed (no run index exists; a static SPA can't glob files) | **One inlined bundle.** `bench dashboard build` walks `runs/` + `analysis/` and inlines everything (run list + each run's metrics + findings + hypotheses + verdicts) into a single `dashboard/public/data.json`; the SPA does one fetch. Dataset is small; regenerate to refresh. |
| Charting library | **Recharts** — declarative React charts; follow the `dataviz` skill for palette/accessibility. |

## 3. Data reality (recon 2026-08-05 — what the dashboard actually reads)

- **No aggregate index exists**, and a static SPA cannot walk the filesystem — SP4 must add a
  build-time aggregator (greenfield; `scripts/` currently holds only `gen-stats-fixtures.py` +
  `check-runs-immutable.mjs`).
- **No web tooling exists** — no Vite/React/Recharts; all greenfield. The repo is Node≥22 ESM,
  TypeScript 5.7, vitest 4, eslint, ajv. Vitest + Playwright are reusable for dashboard tests.
- **`runs/` and `analysis/` are empty on disk** — the pipeline has never run end-to-end here.
  The dashboard is built and tested against **synthesized-pipeline fixtures** (drive the REAL
  `extractRun`/`detect`/`hypothesize`/`experiment` over `buildSynthRun` output — byte-identical
  shapes to production). First real data at AC iteration 0.
- **The input shapes** (aggregator reads these; all validated against the pipeline's own
  `schema/*.json`):
  - `runs/<b>/<runId>/run.json` — RunRecord: `runId, benchmark, archistratorCommit,
    archistratorDirty, operatorPolicyVersion, epoch (int), suiteVersion, startedAt, endedAt,
    outcome, learningConsent, operatorActions[], gaps[], stackConfig, modelIds`. **The only
    source of the ordering/grouping axes** (epoch, timestamps, commit, suiteVersion, outcome).
  - `analysis/<b>/<runId>/metrics.json` — MetricsBody: envelope (`runId, benchmark,
    schemaVersion`) + `coverage[]{metric, hasRealInput, note}` + `efficiency{perEpisode[],
    byKind, byWorkerClass, byTargetRef, totals}` + `rework{byTargetRef}` + `quality|null` +
    `estimation{pairs[], predictedNoActual[], actualNoPredicted[], mapeIn/Out, biasIn/Out,
    byWorkerClass}`. Consent-withheld runs carry only the envelope + a single coverage flag and
    NO metric families — the dashboard must handle family-absent.
  - `analysis/<b>/findings.json` — `{benchmark, generatedAt, epochs[], findings[]}`; each
    Finding `{kind, subject, effectSize, n, insufficientN, detail}`. The `trend` finding's
    `detail.order[]` + `detail.xs[]` + `detail.alarms[]` give the trends view its exact series
    and CUSUM alarm bands — render from it, don't re-derive.
  - `analysis/<b>/hypotheses.json` — `{benchmark, generatedAt, hypotheses[], gaps[]}`.
  - `analysis/<b>/experiments/<expId>/{preregistration.json, runs.json, verdict.json}` — the
    frozen plan, the collected arm runs, and the verdict `{outcome:
    supported|refuted|inconclusive, statistic, pValue, effectSize, n, ranAt, reason?}`.
- **Ordering axis:** trend x-axis = `run.json.epoch` (integer, monotonic lineage counter),
  tiebreak `startedAt` then `runId` — the same order `detect.ts` already computed. Note
  `run.json.epoch` (int) ≠ detect's grouping `epochKey` string (`suiteVersion::policyVersion`);
  trends are within one epochKey group, ordered by the int `epoch`.

## 4. Architecture

Two units, one data contract between them:

```
archistrator-bench/
  scripts/build-dashboard-data.mjs   # the aggregator (Node): walks runs/ + analysis/ →
                                     #   dashboard/public/data.json (validated vs schema/*.json)
  dashboard/
    index.html                       # Vite entry
    vite.config.ts
    public/data.json                 # the ONE bundle the SPA fetches (git-ignored build output)
    src/
      main.tsx, App.tsx, router
      data/                          # typed loaders over data.json + the DashboardData type
      views/                         # RunList, RunDetail, RunDiff, Trends, ScienceLedger
      components/                    # charts (Recharts), coverage/insufficientN/consent badges
      charts/                        # thin Recharts wrappers (palette per dataviz skill)
```

- **The aggregator** (`bench dashboard build`, added as a CLI subcommand + an npm script) is
  pure Node, deterministic (no `Date.now`/`Math.random` in the emitted data — a `builtAt` stamp
  is allowed but excluded from any equality test). It produces a single typed `DashboardData`:
  `{ builtAt, benchmarks: [{ name, runs: [RunSummary], findings, hypotheses, experiments }] }`,
  where `RunSummary` inlines `run.json` fields + that run's `metrics.json` body (or the
  consent/absent markers). It reads only what the dashboard needs — never `traces/`, `app/`, or
  `project.json` (large raw inputs the analysis already digested). Empty `runs/`/`analysis/` →
  an empty-but-valid bundle (the dashboard renders an empty state, not a crash).
- **The SPA** is a static Recharts/React app that fetches `data.json` once and renders. No
  server, no DB, no filesystem access, no network beyond that one fetch. `vite build` →
  static files opened in a browser (or `vite preview`).
- **Schema safety:** the aggregator validates each source JSON against the pipeline's own
  `schema/*.json` (ajv, already a dep) and fails the build loudly on drift — the dashboard never
  renders a shape it didn't expect.

## 5. Views (spec §8)

1. **Run list** — table of all runs, filter by benchmark / archistratorCommit / outcome / epoch;
   each row: runId, commit (short), outcome, epoch, key totals (tokens, cost, quality pass-rate),
   coverage/consent badges. Click → run detail.
2. **Run detail** — one run's breakdowns from `metrics.json`: token/cost/duration by kind /
   worker-class / target (bar charts), quality feature-checklist (pass/fail grid), rework ratios,
   estimation predicted-vs-actual (MAPE/bias, with unpaired-gap callouts), the run's gaps +
   modelIds. Every metric family carries its `coverage.hasRealInput` badge.
3. **Run diff** — pick two runs, per-metric deltas side-by-side (tokens, cost, quality, MAPE),
   with the archistrator commit of each labeled. Deltas shown with direction; no significance
   claim here (that's the experiment view) — this is descriptive comparison.
4. **Trends** — metric-over-epoch line charts within an epoch group (token cost, quality,
   estimation error), rendered from `findings.json` trend series + CUSUM alarm bands. The "watch
   token cost fall across iterations" view.
5. **Science ledger** — findings (with effect size + n + insufficientN badges) → hypotheses
   (root cause + proposed change) → experiments (frozen pre-registration → verdict:
   supported/refuted/inconclusive, with the reason). Refuted and inconclusive shown as
   first-class (negative results are results).

## 6. Honest surfacing (load-bearing, matches the hard-science bent)

The dashboard **never presents a number as real when it isn't.** Distinct visual treatment for:
- `coverage.hasRealInput === false` → "no real input yet (synthetic/placeholder)" badge.
- `finding.insufficientN === true` → "n too small for significance" badge; the number is shown
  but never styled as a confident result.
- consent-withheld runs (metrics envelope only, no families) → "learning consent withheld" state.
- verdict `inconclusive` (esp. underpowered) → visually distinct from `supported`/`refuted`; the
  `reason` (e.g. "underpowered — n too small to reach α") is surfaced.
- estimation `mape/bias === null` or unpaired gaps → shown as "not computable / unpaired," not 0.

This is a UI requirement, not decoration: the whole pipeline's value is trustworthy signal, and
the dashboard is where a human could be misled by a confident-looking synthetic or underpowered
number.

## 7. Testing

- **Aggregator:** unit-tested — drive the real `buildSynthRun` → `extractRun`/`detect`/
  `hypothesize`/`experiment` pipeline into a tmp `runs/`+`analysis/`, run the aggregator, assert
  the emitted `data.json` validates a `DashboardData` schema and inlines the right runs/findings/
  verdicts; an empty tree → valid empty bundle; a schema-drifted source → build fails loudly.
- **SPA components:** vitest + React Testing Library — each view renders from a fixture
  `DashboardData`; the honest-surfacing badges assert on synthetic/insufficientN/consent/
  inconclusive fixtures (the badges are the point — test them explicitly).
- **Charts:** render-smoke each chart against a fixture series (no visual regression in v1).
- **E2E (optional, Playwright — already available):** build the SPA against a fixture bundle,
  load it, click through the five views — gated, not in the default unit suite.
- First real end-to-end (real archived runs → aggregator → dashboard) at AC iteration 0.

## 8. Non-goals (v1)

- No server, DB, auth, or live ingest (static bundle only).
- No visual-regression testing (render-smoke only).
- No editing/mutation from the dashboard — it is read-only over the pipeline's outputs.
- gtd/archistrator benchmark data (defined-unrun; the dashboard renders whatever benchmarks have
  data, so it works for them automatically once run).
- The aggregator does not re-run the analysis — it reads SP3's committed outputs. Regenerating
  analysis is SP3's `bench detect/hypothesize/...`, not the dashboard build.

## 9. Open questions

1. **Bundle size ceiling** — one inlined `data.json` is fine for a handful of runs; if a
   benchmark ever accumulates hundreds of runs the bundle grows. Revisit (index+lazy) if it bites;
   not a v1 concern.
2. **Dashboard hosting for review** — v1 is `vite build` + open locally / `vite preview`. Whether
   to ever publish it (e.g. an artifact) is a later question; the founder opens it locally for AC.
3. **`data.json` git status** — a build output; gitignore it (regenerable from committed
   `runs/`+`analysis/`), consistent with `dist/`. Confirm at plan time.

## 10. Implementation status & deviations (2026-08-05)

All 8 plan tasks landed on `archistrator-bench` main (355 tests across the `node` + `dashboard`
vitest projects, `npm test`/`typecheck`/`lint`/`dashboard:build` all green), closed out by Task 8:
`App.tsx`'s router wired to the five real views (RunList/RunDetail/RunDiff/Trends/ScienceLedger)
with `useState`-only selection state (`activeView`/`selectedRunId`/`selectedBenchmark`, no router
library — open question 2's "opens it locally" posture didn't call for one), an end-to-end RTL
integration test (`dashboard/src/App.integration.test.tsx`) over a REAL `buildSynthPipeline` +
`aggregate()` bundle proving run-list → run-detail → diff → trends → science-ledger navigation and
every honest-surfacing badge (synthetic, insufficientN, consent-withheld, inconclusive), and a
build smoke: `bench dashboard build` over a synthesized tree writes a schema-valid `data.json`
(already covered by `test/dashboard-aggregate.test.ts`'s `main(["dashboard","build",...])` suite,
re-verified manually for this closeout), and `npm run dashboard:build` (tsc + `vite build`) emits
`dashboard/dist/` (`index.html` + a single ~711 kB / ~206 kB gzip JS bundle — Recharts pulls in the
bulk of it; open question 1's bundle-size concern is about `data.json` growth, not this asset
bundle, and remains unaddressed by design, v1 scope).

Deviations found and ruled during implementation (none blocking):

- **Open question 3 resolved: `dashboard/public/data.json` is gitignored**, consistent with
  `dashboard/dist/` — both are regenerable build outputs (`bench dashboard build` / `vite build`
  respectively), never committed.
- **Open question 2 resolved as scoped: no hosting was added.** `npm run dashboard:dev` (live dev
  server) and `npm run dashboard:build` + `npm run dashboard:preview` (production build + static
  preview) are the only supported ways to view it, per §8's non-goals — the README's new
  "Dashboard (SP4)" section documents both paths.
- **No gated Playwright test was added.** The brief for the closeout task allowed this
  explicitly as optional ("do not add it unless trivial"); the RTL integration test over a real
  aggregated bundle already exercises the full view graph + badge wiring, so a second,
  slower browser-level smoke was judged non-essential for v1 and skipped.
- **The benchmark selector (`<select data-testid="benchmark-select">`) only renders on the
  Trends/Science-Ledger views**, not globally in the shell nav — those are the only two views
  that take a `benchmark` prop (RunList/RunDiff are cross-benchmark by design, RunDetail is
  per-run); showing it elsewhere would imply it does something it doesn't.
