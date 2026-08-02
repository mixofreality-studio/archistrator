# Self-Improvement Pipeline — Design

- **Date:** 2026-08-02
- **Author:** system-architect (driven), founder (ratified)
- **Status:** Approved design — ready for implementation planning
- **Phase target:** cross-cutting (archistrator capture + SPA, plus a new sibling bench repo)

## 1. Motivation

Archistrator has no feedback loop. It builds apps, predicts token/effort costs, and dispatches
agents — and nothing ever measures whether the apps are good, the predictions were accurate, or
the agents were efficient, let alone improves any of it. We want a **closed self-improvement
loop**:

1. **Observe** — trace every workflow / activity / agentic episode (lineage, durations, LLM
   calls, tool calls, subagent calls, tokens) with a first-class trace UI and export.
2. **Benchmark** — rebuild fixed-input reference apps repeatedly, archiving every built app
   forever, keyed to the archistrator commit that built it.
3. **Analyze** — scientifically: deterministic metrics, statistical detection, LLM-generated
   *falsifiable* hypotheses, pre-registered experimental validation. Never hand-wavy: the LLM
   may only propose; the math disposes.
4. **Improve** — human reviews and ratifies suggested changes; changes land as archistrator
   commits; the next benchmark run measures the delta.

Targets, in scope order: output-app quality, estimation accuracy, token/efficiency of agents.
Operations/billing phases are **out of scope** — the loop covers system design, project design,
and construction.

## 2. Relationship to existing specs

This is the **sibling spec** that `2026-08-01-system-audit-log-design.md` §4/§8 anticipated:
performance analysis / prompt optimization, sharing the *capture source* but not the audit
pipeline (volatility ruling: evidence vs analysis).

**Founder sequencing ruling (2026-08-02): capture seam only.** This project builds the shared
capture source (stream-json tee in `agenticjob`, GH `execution_file` pull) plus an
analysis-owned episode/trace store and trace UI. The audit spine (generated child workflows,
`auditAccess`, OCSF records, sealing) remains a separate later project consuming the same seam.
Nothing here may preclude it; the trust rule below is inherited from it.

From `2026-07-20-token-usage-calibration-and-tracing-design.md`: the episode-record shape and
lineage model survive here; its `recordEpisode` MCP verb stays withdrawn (agents never write
their own trail) and its committed-trace tier stays cut. Its calibration advisory
(rateCard deltas) is subsumed by this spec's estimation-accuracy metrics and hypothesis stages.

## 3. Founder decisions (2026-08-02 brainstorm)

| Question | Decision |
|---|---|
| Sequencing vs audit log | **Capture seam only**; audit spine decoupled, later |
| Bench archive location | **One new sibling repo** `archistrator-bench` |
| Run autonomy | **Fully autonomous** end-to-end (local venue, vibes autogate, zero human gates); HITL only at improvement review |
| Output-app quality metric | **Frozen external acceptance suite** (hard, drives statistics) + **LLM-judged rubric** (recorded, labeled soft, never in significance tests) |
| Dashboard | **Hand-built static Vite/React SPA** in the bench repo, no server/DB |
| Learning opt-in | **Deferred entirely**; keep a one-line consent-filter seam at the analysis ingest boundary |
| Rigor vs cost | **N=1 iterate + validate-on-demand**: iterations run once; a validation mode runs K paired repeats with a proper test when proof is wanted |
| Analysis engine | **Approach A — layered: LLM proposes, math disposes** |
| Plan review | Implementation plan is reviewed by the **system-architect agent** before founder review |

## 4. Goals and non-goals

**Goals**
- Per-episode capture on the local venue (supervisor-observed, never agent-self-reported),
  with full lineage: Temporal workflow/run → activity → episode → subagent calls.
- Trace UI in the archistrator SPA on every design-artifact page and construction-activity
  page, with JSON/CSV export via button and REST endpoint.
- A bench repo with three fixed-input benchmarks (todomvc / gtd / archistrator), a fully
  autonomous runner, and an immutable, never-deleted run archive keyed by archistrator commit.
- A four-stage analysis engine enforcing the scientific method structurally (§7).
- A standalone metrics dashboard for humans: run compare, trends, science ledger.
- AC: pipeline built; 3 improvement iterations on todomvc; the 3 built apps served in 3
  Chrome tabs.

**Non-goals (v1)**
- The audit spine (child workflows, sealed stores, OCSF export) — separate project.
- Learning opt-in / consent surfaces — deferred; seam noted at ingest.
- Operations/billing-phase coverage.
- Automated prompt evolution (GEPA proper) at full-build granularity — future work on
  micro-benchmarks (§7 stage 3 note).
- Running the gtd and archistrator benchmarks — defined, not run.
- In-place resume of failed runs; retention/pruning of the archive (append-only forever).

## 5. SP1 — Capture seam + trace UI (archistrator)

The only archistrator-side work.

**Capture.** `agenticjob` switches local invocations to
`claude --output-format stream-json --verbose` and tees the raw stream to
`.aiarch/traces/<episodeId>.jsonl` — **gitignored** (per the audit ruling: per-branch committed
trace files are the wrong shape; the bench archive is the forever store). On episode completion
(success and failure) the supervisor parses the terminal `result` event and the streamed
`tool_use`/`tool_result`/per-turn `usage` events, appending an **episode summary** to an episode
ledger:

```
EpisodeSummary {
  episodeId, kind (design-artifact | construction | review | rework | ...),
  targetRef (artifactKind | activityId),
  lineage { workflowId, runId, activityId },
  workerClass, model,
  usage { in, out, cacheRead, cacheCreate }, costUsd, numTurns,
  toolCallCounts (by tool), subagentInvocations (count + per-call spans from Task tool_use),
  startedAt, endedAt, outcome, tracePath
}
```

**Trust rule (inherited):** the write path is supervisor-side in `agenticjob`; there is no MCP
verb an agent could call to write or suppress its own trail.

**GH venue (in scope if trivial, else seam-noted).** The workflow adds one
`actions/upload-artifact` step uploading the action's `execution_file` output (named by episode
id). The server — which already observes the run via `PipelineObservation` — **pulls** the
artifact through the GH artifacts API on completion and feeds it to the same parser as the
local tee (one code path). Pull, not push: a failing or compromised job cannot forge or omit
its trail. If the artifact expired or the run was deleted before the pull, an explicit
"trace unavailable + reason" entry is recorded — never a silent hole. Bench runs are
local-venue, so this path is not on the AC critical path.

**Trace UI.** In the archistrator SPA, on each design-artifact page and each
construction-activity page:
- **Episodes panel** — per episode: outcome, duration, model, worker class, token totals,
  turn/tool/subagent counts; lineage rendered as a tree (workflow → activity → episode →
  subagent spans).
- **Timeline viewer** — expansion of an episode: LLM turns with per-turn tokens, tool calls
  with metadata-only arguments, subagent spans, ordered and filterable by event type.
- **Export** — button + REST endpoint; **JSON** (full events) and **CSV** (flattened event
  rows) for one episode, one target, or the whole project.

Reads flow through a manager read op + trace-reading ResourceAccess + generated client op
(existing `clientgen` rail). The panel/timeline components are deliberately the same surface
shape as audit-spec §5.8 so the later audit spine reuses them.

## 6. SP2 — Bench repo (`archistrator-bench`)

```
archistrator-bench/
  benchmarks/
    todomvc/       # easy   — TodoMVC-equivalent; backend (web/mcp) + frontend (web/mcpapps)
    gtd/           # medium — full GTD per software/products/gtd docs incl. gtdinfo.txt
                   #          (capture/clarify/organize/reflect/tickler/horizons)
    archistrator/  # hard   — archistrator rebuilt from its own requirements corpus
      # each: pinned research corpus (the constant input), frozen acceptance suite,
      #       feature checklist, soft LLM rubric
  runner/          # CLI: provision project repo → seed pinned inputs → drive
                   # system-design → project-design → construction, fully autonomous
                   # (local venue, vibes autogate) → poll to completion → harvest
  runs/<benchmark>/<runId>/    # IMMUTABLE, append-only, kept forever
    app/           # full built-app snapshot
    project.json
    traces/        # copied from the run's .aiarch/traces
    acceptance/    # frozen-suite results
    metrics.json   # stage-1 extraction output
    run.json       # archistrator commit SHA, model IDs, suite version/epoch,
                   # timestamps, outcome (succeeded | failed)
  analysis/        # SP3 (Python + scipy/numpy)
  dashboard/       # SP4 (static Vite/React SPA)
  runs/index.json  # regenerated aggregate the dashboard reads
```

- **Configuration identity = archistrator commit SHA.** Every improvement is a commit; runs
  are keyed to what built them.
- **Failed runs are archived too**, marked `failed`, traces included — failures are data.
  Reruns get fresh runIds; no in-place resume.
- **Frozen suites:** versioned; never modified within an experiment series. A suite change
  starts a new comparability **epoch** recorded in `run.json`; cross-epoch comparisons are
  refused by the detector.
- **Immutability gate:** bench-repo CI fails any PR that modifies or deletes anything under
  `runs/**`.
- **Known risk:** a fully autonomous zero-gate end-to-end build has never been exercised;
  SP2 includes a hardening pass on that path (expected to be the real debugging cost of the AC).

## 7. SP3 — Analysis engine (approach A: LLM proposes, math disposes)

Four CLI stages in `analysis/`, all outputs committed to the bench repo. Python + scipy/numpy:
the statistics are the hard-science core and get vetted implementations, not hand-rolled ones.
(Consent-filter seam for future opt-in data lives at this ingest boundary.)

**Stage 1 — `extract <runId>` (pure code, zero LLM).** Reduces traces + `project.json` +
acceptance results to typed `metrics.json`: token totals and breakdowns (phase / activity /
worker class / episode kind), durations, turns, tool-call distributions, subagent counts,
retry & rework ratios, review-rejection counts, validation/gate-failure counts **by gate**,
first-pass success rate per activity type; quality block (build ok, app-tests pass rate,
frozen-acceptance pass rate, feature checklist) plus LLM-rubric scores labeled `soft: true`;
estimation block (predicted tokens/effort per activity vs actuals, MAPE + signed bias).
Predictions are **derived at extraction time** from the archived `project.json`
(`effortDays × rateCard MTok/day` for the SDP-chosen option) — the archive snapshot freezes
them, so no archistrator-side baseline materialization is required.

**Stage 2 — `detect` (deterministic statistics, zero LLM).** Over all comparable runs of a
benchmark (same epoch): Pareto ranking of token/failure concentration by activity and gate;
robust outliers (median absolute deviation); rework-ratio ranking; CUSUM control charts across
iterations for drift/regression; for config-vs-config comparisons, paired differences with
runs as clusters, Wilcoxon signed-rank at small n, bootstrap confidence intervals. Every
finding carries its effect size and its n; insufficient-n findings are labeled exactly that —
never suppressed, never dressed up. Methodological basis: Anthropic's "Adding Error Bars to
Evals" (arXiv:2411.00640).

**Stage 3 — `hypothesize` (the only LLM stage).** A Claude agent reads flagged findings plus
the relevant trace excerpts and emits schema-enforced **structured hypotheses**:

```
Hypothesis {
  finding_ref, root_cause_narrative,
  proposed_change,      # file/prompt/skill-level, concrete
  metric, direction, min_effect,
  predicted_mechanism
}
```

Free-prose advice fails validation. This cages the GEPA insight (reflective analysis of
execution traces, arXiv:2507.19457) to proposing falsifiable claims only. Future work:
GEPA-proper automated evolution on **micro-benchmarks** (single-activity replays), never on
full builds in v1.

**Stage 4 — experiment protocol (the scientific-method gate, zero LLM).** Founder reviews
hypotheses (the HITL step). An accepted hypothesis becomes a **pre-registered experiment**:
`experiments/<id>/preregistration.json` freezes metric, direction, minimum effect, K paired
runs, test, and α **before any run**. The runner executes baseline vs treatment;
`verdict` runs the declared test and writes `verdict.json`: **supported / refuted /
inconclusive**. Refuted hypotheses stay in the ledger forever — negative results are results.

**Operating mode (founder ruling):** iterations run at **N=1** (fast, cheap); the detector
pools variance across accumulated runs and refuses significance claims it cannot back.
**Validation mode** is invoked on demand to prove a specific change via stage 4.

## 8. SP4 — Dashboard (standalone app)

Static Vite/React SPA in `dashboard/`, no server, no DB; reads `runs/index.json`. Views:

1. **Run list** — filter by benchmark / commit / outcome / epoch.
2. **Run detail** — phase/activity token & duration breakdowns, quality scores, gate failures.
3. **Run diff** — any two runs side-by-side, per-metric deltas, CIs where n allows.
4. **Trends** — metric-over-iteration charts (token cost per iteration, quality, estimation
   error) with control-chart bands.
5. **Science ledger** — hypotheses, experiments, pre-registrations, verdicts.

Built following the dataviz skill.

## 9. AC execution (SP5)

1. Build SP1 → SP4.
2. **Iteration 0:** full autonomous todomvc run (doubles as harness hardening) → archive →
   extract/detect/hypothesize → founder reviews → accepted changes land as archistrator
   commits.
3. **Iterations 1–2:** rerun → archive → analyze → improve. Yields 3 archived todomvc builds
   at 3 archistrator commits.
4. Boot the 3 built apps on 3 ports; open 3 Chrome tabs (claude-in-chrome) for founder review.
5. gtd and archistrator benchmarks ship **defined but unrun**.

## 10. Testing

- **Stats:** golden tests against published scipy reference values (Wilcoxon, bootstrap CIs,
  CUSUM behavior on synthetic drift).
- **Extractor:** golden fixtures from **real captured stream-json traces**, never
  hand-authored (audit-spec fixture ruling).
- **Capture:** unit tests on the stream parser (success, failure, malformed terminal event);
  an episode with a dead store/dir yields an explicit gap entry, not silence.
- **Runner:** smoke test via `CONSTRUCTION_DRYRUN` before real runs; mid-run failure archives
  a `failed` run with traces.
- **Archive:** CI immutability check on `runs/**`.
- **Trace UI:** Playwright on the existing `uitests` rail against real local state; standing
  stop-for-founder-review UI loop applies.
- **Dashboard:** Playwright against a fixture archive.

## 11. Open questions

1. **Autonomous-run gaps** — which design-rail steps still hard-require a human on the local
   venue (vibes autogate coverage) will be discovered in the SP2 hardening pass; each gets
   fixed or explicitly stubbed for bench runs.
2. **Subagent span fidelity** — how much per-subagent token/duration detail the stream-json
   exposes for Task tool calls; capture what the supervisor can observe, no agent self-report.
3. **Rubric stability** — the soft LLM judge should be pinned (model + prompt version) per
   epoch so soft scores are at least internally comparable.
4. **GH artifact wiring** — in scope if it stays a few workflow lines + one download call;
   otherwise seam-noted (not on the AC critical path).
5. **Micro-benchmark replay harness** — required before GEPA-style evolution; also what the
   audit spec called out as needed for causal reads. Future project.
