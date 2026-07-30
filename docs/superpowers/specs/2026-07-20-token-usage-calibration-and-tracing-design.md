# Token-Usage Calibration & Episode Tracing — Design

- **Date:** 2026-07-20
- **Author:** system-architect (driven), founder (ratified)
- **Status:** Approved design — ready for implementation planning
- **Phase target:** cross-cutting (spans ResourceAccess, Engine, Manager, Client, SPA)

## 1. Motivation

Project design predicts how many tokens a project will consume, but nothing ever checks
that prediction against reality. We want a closed feedback loop: **capture the actual token
usage of a project's construction, compare it against what project design predicted, diagnose
why they diverged, and propose how to make the next prediction more accurate — with the
architect ratifying the change.** Founder additionally wants a **full per-episode trace**
(LLM calls, tool calls, per-turn token usage) viewable on every design page and every
construction activity, so you can drill into exactly what a given Claude Code call did.

Chosen ambition (from brainstorming): **measure + diagnose + propose recalibration, human
ratifies** — i.e. option "B + A" (live mid-flight tracking *and* an end-of-project
cross-project calibration loop). Not full auto-amend.

## 2. Current terrain (reconnaissance findings)

Nothing captures actual token usage today, but the architecture already anticipates it — this
work is mostly **wiring pre-dug graves**, not inventing a subsystem.

**Prediction path.** Tokens exist *only* as a per-worker-class *rate* in
`.planningAssumptions.rateCard` (`WorkerRateSpec{ModelID, MegatokensInPerDay, MegatokensOutPerDay}`,
`server/internal/resourceaccess/projectstate/contract.gen.go:776`). At `rateForSpec`
(`server/internal/manager/projectdesign/assemblesdpreview.go:766`) the rate is immediately
collapsed into a `$/day` `Money` and **tokens never persist as a quantity**. A per-activity
predicted token total (`effortDays × rateCard MTok/day`) is therefore *derivable* but
materialized nowhere. The estimation engine
(`server/internal/engine/estimation/estimationengine.go`) works in effort-days × `$/day`.

**Actuals path.** Captured **nowhere**. The signal is nearly free on the local venue: we
already run `claude … --output-format json -p <prompt>` and buffer the full stdout envelope
(`claudeArgv`, `server/internal/resourceaccess/constructionpipeline/constructionpipelineaccess.go:2295`),
whose result envelope carries `usage` (in/out/cache tokens), `total_cost_usd`, `duration_ms`,
`num_turns`. But `claudeResultEnvelope` (`:1987`) decodes only `subtype/is_error/result/error`,
and `awaitCompletion` (`:1666`) mines stdout **only on failure** — on success it is discarded.
The GH venue (`.github/workflows/aiarch-construct.yml`, `claude-code-action@v1`) drops it too:
`PipelineObservation` (`constructionpipeline/contract.gen.go:18`) has no usage field and the
action's execution-file output is never fetched.

**Comparison path.** Earned-value/SPI exists but is **schedule-only**: `computeEVAtRead`
(`server/internal/manager/systemdesign/systemdesignmanager.go:2850`) weights binary
integration exits by *planned* effort — no actual-cost input at all.

**Pre-dug graves (reuse these):**
- `usageAccess` ledger reserves a `construction-token` unit — **zero construction writers**,
  and it is Postgres/cloud-only (unreachable in-episode / local-first).
- `ConstructionProgress.Points[].AcPct` is doctrinally *"actual cost as % of BAC — the
  AI-token cost basis"* (`projectstate/contract.gen.go:334`, tracking skill lines 51–81) —
  **no writer**; the SPA `EvTrackingChart` ignores it.
- `EstimateOverrun` exists in the variance enum (`intervention/contract.gen.go:156`) —
  **never raised**; only `WorkerMiss` is.
- The 5-minute variance sweep `ReplanSweepWorkflow.flagVariances` is a `nil` stub
  (`replansweep.go:45`).

## 3. Goals & non-goals

**Goals**
- Freeze a per-activity token prediction at SDP-commit as the comparison anchor.
- Capture per-episode actual usage on the local venue and roll it up per activity
  (whole-activity-lifecycle: design + construct + review + every rework/retry).
- Persist a full JSONL trace per episode and view it in the SPA on each design page and
  each activity.
- Live mid-flight: render actual-cost (`AcPct`/CPI/EAC) on the EV chart; raise
  `EstimateOverrun` variance when an activity overshoots.
- End-of-project: emit a predicted-vs-actual report + a why-diagnosis + a proposed
  `rateCard` delta; architect ratifies at the next `/project-design`.

**Non-goals (v1)**
- GH-venue actuals/trace capture (noted seam; deferred).
- Dollars / spend accounting / settlement integration — comparand is **tokens**
  (throughput calibration). `$` is notional on subscription/OAuth runs anyway.
- Auto-amending the `rateCard` mid-construction (option C). Ratification is manual.
- Trace compression / externalization / retention enforcement (noted future work).

## 4. Design

### 4.1 The episode ledger (the spine)

Every Claude Code invocation — a design-phase draft *or* a construction phase/review/rework —
is an **episode**. The unifying record:

```
EpisodeRecord {
  episodeId       string          // stable id, minted at dispatch
  kind            enum            // design-artifact | construction | review | rework | ...
  targetRef       string          // artifactKind (design page) | activityId (construction)
  workerClass     string          // maps to rateCard class / model
  model           string
  usage           { in, out, cacheRead, cacheCreate: int64 }
  costUsd         float64         // notional; recorded, not calibrated on
  numTurns        int
  toolCallCounts  map[string]int  // by tool name
  startedAt, endedAt timestamp
  tracePath       string          // .aiarch/traces/<episodeId>.jsonl (pointer, never inlined)
  outcome         enum            // success | failure
}
```

Three storage additions in `project.json` (git-as-DB head-state — **not** the Postgres
`usageAccess` ledger, which is unreachable in-episode):

1. **Frozen prediction baseline.** At `sdpCommit` — where `assembleSdpReview`
   (`assemblesdpreview.go:108`) already re-runs the engines for the *chosen* option — we
   materialize, per activity, `predictedTokensIn/Out = effortDays × rateCard[workerClass].MTok/day`.
   This anchors the comparand so it does not drift as the rateCard/activityList are later amended.
2. **Per-activity actuals accumulator** on `.activityConstruction[activityId]`: a `usage`
   block that each episode *appends* to (whole-lifecycle sum), plus the list of that
   activity's `episodeId`s.
3. **A dedicated `.calibration` slot** holding: the frozen prediction baseline, the running
   rollup (per activity / per class / total), and the end-of-project advisory (§4.4). Kept
   separate from the Solution artifacts, which are about `$`/days and should not carry token
   vectors.

The **episode ledger** itself (list of `EpisodeRecord` summaries) is the join between design
pages, activities, actuals, and traces. Its `usage` summary *is* the actuals signal — capture
and tracing are one path.

### 4.2 Capture — the local-venue seam

Switch the local invocation from `--output-format json` to
`claude --output-format stream-json --verbose` and **tee** the event stream to
`.aiarch/traces/<episodeId>.jsonl`. That stream is literally the full trace (every LLM turn,
`tool_use`, `tool_result`, per-turn `usage`); its terminal `result` event carries the totals.
On episode completion (**success and failure** — whole lifecycle counts):

- Parse the terminal event for `usage` / `total_cost_usd` / `num_turns`; tally
  `toolCallCounts` from the streamed `tool_use` events.
- Write back via a new in-episode construct verb **`recordEpisode`**
  (sibling of `recordServiceContract` / `recordPhaseArtifact` in
  `server/cmd/aiarch-state-mcp/constructverbs.go`) that appends the `EpisodeRecord` to the
  ledger, appends `usage` to the target activity's accumulator, and records the `tracePath`.

Capture lives in `constructionpipelineaccess.go` (ResourceAccess owns the venue boundary).
**v1 is local-venue only.** GH-venue capture (fetch the action execution artifact, or an
in-episode self-report tool, or Claude Code OTLP telemetry) is a noted seam, not built here.

Design-phase episodes hook the same `recordEpisode` at the existing `recordPhaseArtifact`
write-back point, so design pages get episodes/traces without a separate path.

### 4.3 Live mid-flight tracking

- **AcPct/CPI/EAC writer.** Add the missing writer for `ConstructionProgress.Points[].AcPct`:
  `AcPct = Σ actual tokens / BAC`, `BAC = Σ frozen predicted tokens`. Compute `CPI =
  earned/actual` and `EAC` (formulas already specified in the tracking skill). Compute at read
  alongside the existing `computeEVAtRead` in the systemdesign manager. Render the `AcPct`
  line in the SPA `EvTrackingChart` (currently ignored).
- **Variance.** Raise `EstimateOverrun` when an activity's cumulative actual exceeds
  `predicted × threshold` (**default 1.5×, configurable**), wired into the currently-`nil`
  `flagVariances` sweep and/or at `recordEpisode` write-back, routed through
  `interventionEngine.DecideOnVariance`. Surfaces divergence in-flight, not just post-mortem.

### 4.4 End-of-project calibration (the "why + suggestion")

A new **estimation-engine method** (so server and Phase-2 agents share the math via the
existing `rawexec` rail) computes, per worker class:

- **Observed throughput** = `Σ actual tokens for that class's activities ÷ Σ their effort-days`
  (in and out separately), compared to the rateCard's `MTok/day`.
- **Predicted-vs-actual report**: per activity, per class, and rollup, with divergence %.
- **Diagnosis of *why*** — attribution across (a) worker class, (b) outlier activities,
  (c) **rework ratio** (episodes/attempts per activity — often the real driver).
- **Proposed `rateCard` delta per class**.

Output lands in the `.calibration` advisory as an artifact the architect reviews at the next
`/project-design`. **The founder ratifies**; ratification updates
`.planningAssumptions.rateCard`. A thin skill drives the review/ratify step. No auto-amend.
The loop closes *across* projects — each new project starts better-calibrated.

### 4.5 Trace storage

`.aiarch/traces/<episodeId>.jsonl` — **a plain JSONL file committed to the git repo, never
inlined into `project.json`, no compression.** Rationale: git already zlib-compresses blobs in packfiles, so on-disk size
is close to gzip; plain files stay greppable / diffable / `cat`-able; and git can delta-compress
structurally-similar traces (opaque gzip blobs defeat that). `project.json` holds only the
`EpisodeRecord` summary + `tracePath` (respects the git-as-DB 64KB dispatch cap and keeps
head-state small).

**Future work (triggered):** a retention/pruning policy (prune or externalize traces past
N projects) if `.git` growth becomes noticeable. Not enforced in v1.

### 4.6 SPA — episode & trace viewer

On each design-artifact page (mission, glossary, volatilities, systemDesign, project-design
artifacts) and each construction activity: an **"Episodes" panel** listing the episode(s) for
that `targetRef` with a token / cost / tool-call summary, each expandable into a **full trace
viewer** rendered from the JSONL (LLM turns + tool calls + per-turn usage). Requires:

- a manager read op: *list episodes for a `targetRef`* and *get trace by `episodeId`*;
- a trace-reading ResourceAccess (reads the JSONL file);
- a generated client op + OAS;
- the SPA viewer + per-page episodes panel component.

## 5. Layering (Method decomposition)

| Layer | Additions |
|---|---|
| **ResourceAccess** | constructionpipeline: stream-json tee + terminal-event parse + `recordEpisode`. projectstate: frozen-prediction baseline, per-activity `usage` accumulator, `.calibration` slot, episode ledger, trace-file reader. |
| **Engine** | estimation: predicted-token materialization, AcPct/CPI/EAC, calibration/throughput computation + proposed deltas. intervention: `EstimateOverrun` raise logic. |
| **Manager** | construction: episode write-back + variance raise. systemdesign: AcPct/CPI/EAC at read. projectdesign: freeze prediction at `sdpCommit`, surface calibration advisory at `/project-design`. |
| **Client/SPA** | list-episodes / get-trace ops; EV chart `AcPct` line; episodes panel + trace viewer. |

## 6. Open questions / future work

1. **GH-venue capture** — deferred; the seam is fetching the action's execution artifact or an
   in-episode self-report tool. v1 is local-only.
2. **Trace retention** — plain committed JSONL is fine at low volume; define a pruning /
   externalization trigger before `.git` bloat bites.
3. **Auto-amend (option C)** — a future rail could drive the scope-change flow to amend the
   rateCard mid-construction; explicitly out of scope now ("human accepts").
4. **Cache-token calibration** — decide whether the rateCard should model cache-read/create
   separately or fold them into effective input for throughput calibration. Default: fold in.
5. **Variance threshold default (1.5×)** — confirm the floor and whether it is per-class.
