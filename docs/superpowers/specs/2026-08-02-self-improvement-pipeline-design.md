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
| Run autonomy | **Fully autonomous** end-to-end (local venue, vibes autogate); **the human does nothing at all** — a scripted Claude Code **operator agent** drives archistrator (MCP server preferred, Playwright MCP on the local SPA as fallback) and auto-approves every surviving gate; HITL only at improvement review |
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

**Capture — report/store split (system-architect amendment, 2026-08-02).** Observation and
storage are separate volatilities (audit spec §4); `agenticjob` observes, a new RA stores:

- **`agenticjob` (observe only).** Switches local invocations to
  `claude --output-format stream-json --verbose`, tees the raw stream to
  `.aiarch/traces/<episodeId>.jsonl` (to file with a bounded in-memory tail — never
  full-stream buffering for multi-hour episodes), parses the terminal `result` event and the
  streamed per-turn `usage` / `tool_use` / `tool_result` content blocks, and **reports** the
  parsed `EpisodeSummary` on the terminal `PipelineObservation` (generated-contract field
  addition). It stores nothing beyond the tee it uniquely owns. The RA mints `episodeId` at
  submit, deterministic against retries via the idempotency key it already receives.
- **`episodeAccess` (new RA, owns the store).** 3 ops: `appendEpisode` (gap and cancelled
  entries included via outcome), `listEpisodes`, `readTraceEvents`. Two substrate profiles per
  audit spec §5.5 shape: local gitignored sidecar now, deployed profile deferred.
- **The dispatching Manager persists.** The Manager workflow that already polls the job maps
  the reported summary into `episodeAccess.appendEpisode` through the generated activity rail —
  where Temporal durability is native, lineage comes from `workflow.GetInfo` (not parsed out of
  idempotency-key strings), and a run lost to a server restart still surfaces as a terminal
  observation the Manager persists as an explicit **gap episode**.

```
EpisodeSummary {
  episodeId, kind (design-artifact | construction | review | rework | ...),
  targetRef (artifactKind | activityId),
  lineage? { workflowId, runId, activityId },   // optional: answer-job episodes are
                                                // fire-and-forget, non-workflow → no lineage
  workerClass, model,
  usage { in, out, cacheRead, cacheCreate }, costUsd, numTurns,
  toolCallCounts (by tool), subagentInvocations (count + per-call spans via parent_tool_use_id),
  startedAt, endedAt,
  outcome (succeeded | failed | cancelled | gap),  // cancel path MUST write its entry too
  tracePath
}
```

**Ledger location (founder question, ruled):** the episode ledger lives in the **gitignored
`.aiarch/traces/` sidecar** (ledger + raw traces together), **never in `project.json`** — not
even tracePath pointers. Four independent reasons: (1) *trust rule* — `project.json` is tracked
in the agent's worktree and legitimately agent-committed, so a ledger slot there is an
agent-writable path to its own trail; the sidecar in the main checkout is physically outside
the agent worktree and its sandbox allowlist; (2) *merge behavior* — append-only arrays in
`project.json` conflict on every session-branch merge and evaporate on reject-on-branch;
untracked files are branch-independent; (3) *head-state size* — an unbounded ledger bloats the
64KB-capped dispatch aggregate; (4) *bench harvest* — the harness snapshots the working tree,
so one copy of `.aiarch/traces/` grabs ledger and traces together. Work items: gitignore
entries in archistrator's own `.gitignore` **and** the method-assets scaffold template. The GH
venue has no local checkout to hold a sidecar — that storage-substrate gap (not just artifact
pull) is why GH stays off the AC critical path.

**Trust rule (inherited):** episode writes are supervisor/Manager-side only; there is no MCP
verb an agent could call to write or suppress its own trail, and the export surface is
read-only behind the generated auth middleware.

**Placement: archistrator-local, deliberately.** The capture seam observes archistrator's own
agent dispatch; apps archistrator builds have no `agenticjob` RA and dispatch no agents, so
there is nothing for them to inherit — the audit spec's platform-placement constraint applies
to its generated emitters, not this seam. The stream parser stays package-internal to
`agenticjob`; its externally visible product is the generated-contract `EpisodeSummary` /
observation shape, which the later `auditEngine` can consume without re-homing a parser. No
platform release needed — only standard modelgen/clientgen regeneration.

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
- **Export** — button + REST endpoint for one episode, one target, or the whole project. The
  REST op serves **typed JSON only** (the clientgen rail is 100% generated JSON handlers);
  **CSV flattening happens client-side in the SPA** from the JSON export — no hand-written
  handler, no waiver.

**Read surface (AMENDED 2026-08-02, founder-ratified after system-architect consultation —
supersedes the earlier thin-`episodeManager` shape).** "View episode traces" is not a use case
in its own right; it is a **facet** of the existing use cases (as the EV chart is a facet of
tracking), and episode observability encapsulates **no Manager-layer volatility** — observation
varies in `agenticjob`, retention in `episodeAccess`, presentation in the Client. A dedicated
`episodeManager` would have existed only because clientgen requires a manager contract — a rail
constraint wearing an architecture costume. Therefore:

- Reads are **facet ops on the managers that already persist the episodes** (each already holds
  the `episodeAccess` dep for writes): `constructionManager.listEpisodesForActivity` +
  `getEpisodeTimeline` (activity pages, tracking family, beside the EV chart);
  `systemDesignManager.listEpisodesForArtifact` + `getEpisodeTimeline` and
  `projectDesignManager.listEpisodesForArtifact` + `getEpisodeTimeline` (session views, beside
  the dispatch observability they already carry). The two design-manager facets collapse into
  one automatically when the ratified 2026-07-10 DesignManager merge is eventually executed.
- **Manager roster stays at 5.** No cardinality waiver, no framework-go gate release, no
  weakened gate. The cardinality reckoning is explicitly deferred to the audit spine, whose
  customer-facing `auditManager` must re-argue its own case when it lands.
- **Export**: per-target JSON/CSV via the SPA export button, assembled **client-side** from the
  facet read ops. The whole-project REST export op is **cut from v1** — the bench harness reads
  the sidecar from the working tree and never calls REST; a dedicated export op returns if a
  real consumer appears.

The shared panel/timeline components are unchanged: pure, `EpisodeSummary`-shaped, with an
**optional badges slot** (assurance/completeness) so the audit spine later adds badges without
forking; `EpisodeSummary` stays free of OCSF/audit fields; gap-entry shape stays aligned with
the audit spec's gap records. Only the per-page containers differ in which manager's generated
ops they call.

### 5.1 SP1 implementation closeout (2026-08-02)

- SP1 implemented on branch `sp1-capture-seam` (commit range `0f4d961..HEAD`).
- Deviations ratified during implementation:
  - Fixture capture via `--allowedTools` (not `--dangerously-skip-permissions` — classifier-blocked).
  - Self-ignoring `.aiarch/traces/.gitignore` instead of a method-assets scaffold change (system-architect endorsed).
  - `NewLocalFSEpisodeAccess` single-return with lazy first-use validation (composegen no-infra constraint; precedent-faithful).
  - NoOp cloud variant binding (never a nil RA under an unbounded-retry activity).
  - Episode reads as facet ops per founder ruling (already amended above).
  - Episode append retry window bounded to 2m (bounded-latency ruling).
  - Answer-job episodes captured via a non-durable manager-side watcher (both design managers).
- Known limits/earmarks:
  - Venue detection via `RunURL` presence — dry-run and URL-less GH runs write gap records; the clean fix is a venue field on the `agenticJob` contract.
  - `WorkerClass` unset on records (unavailable at dispatch); SP3's extractor joins it from `project.json` via `activityID`.
  - Subagent tokens appear in NEITHER usage total — terminal usage is main-loop only; the UI labels accordingly.
  - Terminal-usage-vs-streamed divergence is recorded as both fields (`Usage`, `StreamedUsage`), not reconciled.
  - The `episodes-panel` uitest self-skips in CI until a dogfood-seeded job is added.
  - The two `uitests` project-state configs (fresh-empty vs. dogfood-seeded) are mutually exclusive.
  - `APPC-SVC-AVOID-12` cannot see contract op counts drop — `systemDesignManager` sits at 16 ops unflagged (waived pending the DesignManager merge).
  - Helper triplication across the three dispatching managers is parked pending the ratified DesignManager merge.

**RELEASE NOTE:** **DRAIN in-flight construction/coauthor workflows before deploying** — this
release inserts a new episode-append activity command into existing workflow bodies, with no
`GetVersion` guard (drain ruling, same standing convention as the callchain and layer-layout
releases). In-flight executions replaying against the old command sequence must be allowed to
finish before the new binary is deployed.

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
  runner/          # deterministic CLI shell + the operator agent (see below)
  runs/<benchmark>/<runId>/    # IMMUTABLE, append-only, kept forever
    app/           # full built-app snapshot
    project.json
    traces/        # copied from the run's .aiarch/traces
    acceptance/    # frozen-suite results
    metrics.json   # stage-1 extraction output
    run.json       # archistrator commit SHA, model IDs, suite version/epoch,
                   # operator prompt hash + operator model, pinned-fallback occurrences,
                   # timestamps, outcome (succeeded | failed)
    operator/      # operator agent transcript — bench overhead, excluded from metrics.json
  analysis/        # SP3 (Python + scipy/numpy)
  dashboard/       # SP4 (static Vite/React SPA)
  runs/index.json  # regenerated aggregate the dashboard reads
```

**The runner is two layers (founder ruling, 2026-08-02).**

1. **Deterministic CLI shell** (plain code): provision the project repo, seed the pinned
   research corpus, boot the archistrator local stack (server + Temporal + SPA), launch the
   operator agent, poll to completion, harvest, archive. No judgment, no LLM.
2. **The operator agent** — a Claude Code session acting as a scripted live QA operator that
   drives archistrator exactly as a user would, end-to-end through system design → project
   design → construction. **No human does anything at any point.** Tool surface, in preference
   order: **archistrator's own MCP server** (the generated manager MCP tools) for every step
   that exposes a verb; **Playwright MCP against the locally running SPA** for any step that
   is UI-only — including clicking approve on any approval gate that survives the vibes
   autogate policy. Doing it all via MCP is the desired end-state; the browser fallback is
   acceptable wherever a verb is missing (each such gap gets noted for later MCP exposure).

**Operator skew control.** The operator must not be a confound in the measurements:
   - Its prompt is **frozen and versioned** in `runner/`: enter the pinned benchmark prompt
     **verbatim** (never paraphrase, never add context), approve everything, never author
     content, never answer a design question with its own ideas — if archistrator asks a
     question the pinned inputs don't answer, the operator gives a pinned fallback response
     ("proceed with your recommendation"), and the occurrence is recorded in `run.json`.
   - `run.json` records the operator prompt hash + operator model ID; a change to either
     starts a new comparability **epoch**, same as an acceptance-suite change.
   - The operator's own session (its tokens, turns, transcript) is **bench overhead, not run
     data**: archived alongside the run for debugging, but excluded from `metrics.json` — the
     measured system is archistrator's agents, never the operator.

- **Configuration identity = archistrator commit SHA.** Every improvement is a commit; runs
  are keyed to what built them. (Bench-side identity — operator prompt, acceptance suite —
  is tracked by the epoch fields in `run.json`.)
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

1. **Autonomous-run gaps** — which design-rail steps still require an approval or answer on
   the local venue (vibes autogate coverage) will be discovered in the SP2 hardening pass;
   each is handled by the operator agent (MCP verb if exposed, Playwright click if UI-only),
   and each UI-only gap is noted as a candidate for MCP exposure.
2. **Subagent span fidelity** — how much per-subagent token/duration detail the stream-json
   exposes for Task tool calls (sidechain events carry `parent_tool_use_id`); capture what the
   supervisor can observe, no agent self-report. Known discrepancy: the terminal
   `result.usage` has historically not equaled the sum of streamed per-turn usage once
   subagents are involved (cost is total; usage is main-loop) — the parser tallies from
   streamed events and records **both** totals, treating divergence explicitly.
3. **Rubric stability** — the soft LLM judge should be pinned (model + prompt version) per
   epoch so soft scores are at least internally comparable.
4. **GH artifact wiring** — in scope if it stays a few workflow lines + one download call;
   otherwise seam-noted (not on the AC critical path).
5. **Micro-benchmark replay harness** — required before GEPA-style evolution; also what the
   audit spec called out as needed for causal reads. Future project.
