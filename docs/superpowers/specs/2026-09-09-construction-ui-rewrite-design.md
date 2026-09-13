# Construction UI rewrite — three lenses over one honest activity model

Branch `construction-ui-rewrite`, cut from `main` @ `cbaeff9`.
Team: founder (decides), system-architect (domain rulings), product-manager (customer proxy),
ui-designer (UX concept), Claude (orchestration).

## 1. Problem

The construction console asserts things it does not know.

Its lifecycle strip is fabricated: `lifecycleTemplates.activeIdxFor` reverse-engineers an
"active phase" from the coarse 8-member client-side `BuildStatus`, because the real
`PhaseCompletion` data exists for exactly **1 of 69** activity records. Its graph lays 40
activities plus 4 milestones out by CPM position, which renders as illegible confetti at
fit-view with a dead half-canvas. Its intervention queue is derived from head-state
`status === 'in-review'` while the actual gate is the workflow stage `StageAwaitingApproval`,
which the console finds via a `.find()` that can only ever return one activity. Sixty of its
sixty-nine construction records are keyed to activity ids the derived plan no longer contains,
and the classifier silently types them all as `Deployment`.

The rewrite's purpose is not prettier views. It is: **a screen that never looks authoritative
about something it does not know**, over a data model whose join key survives the later
capture wave without a migration.

## 2. Founder decisions on record (2026-09-09)

| # | Decision |
|---|---|
| D1 | Clean slate from `main`. The unmerged `ux/mario-map-activity-graph` work (2 specs, ~2.9k lines of React Flow) is **not** built on. |
| D2 | UI-first. Backfill/synthesize the gaps with a provenance stamp; real capture is a later wave. |
| D3 | `@mui/x-tree-view` (MIT) plus hand-laid columns. **No** `@mui/x-data-grid` — tree data is a Pro/commercial feature and `@mui/x-*` is not installed at all today. |
| D4 | Graph work is **render only**. `DerivePlan` and its CI drift gate are untouched. |
| D5 | One route, a lens segmented-control in a shared toolbar, and one persistent shared detail pane. Not three tabs. |
| D6 | Three tiers: activity › phase › task. |
| D7 | All three lenses ship this wave, with the designer's cut list (§10). |
| D8 | The 60 legacy records are **segregated now** — read-only context while the 40 derived activities are brought into good shape — and **deleted once that is done**. Deletion is a separate, explicit commit. |

## 3. Book ground truth (Löwy, *Righting Software*)

Authoritative; extracted verbatim from `research/rightingsoftware/`.

**Figure A-1** — 12 tasks, and it is **not** a linear chain:

```
SRS → SRS Review ─┬─→ STP → STP Review ──────────────────────────────┐
                  │                                                   ├→ Testing
                  └─→ Some Construction → Detailed Design →           │
                      Design Review → Construction ⇄ Test Client →    │
                      Code Review → Integration ────────────────────-─┘
```

**Figure A-2** groups those 12 into 5 phases, each with a **binary** exit criterion —
*"the Construction phase is complete once you have had the code review, not simply when the
code is checked in."*

| Lifecycle phase | Tasks | Gate | Weight (Table A-1) |
|---|---|---|---|
| Requirements | SRS, SRS Review | SRS Review | 15 |
| Test Plan | STP, STP Review | STP Review | 10 |
| Detailed Design | Some Construction, Detailed Design, Design Review | Design Review | 20 |
| Construction | Construction, Test Client, Code Review | Code Review | 40 |
| Integration | Integration, Testing | Testing | 15 |

Activity progress = Σ weights of **completed** phases. Project progress = Σ(Eᵢ × Aᵢ(t)) / Σ Eᵢ,
which *is* actual earned value.

**Retry rule, verbatim:** *"each review task in the diagram must complete successfully. A failing
review causes the developer to repeat the preceding internal task. For clarity, Figure A-1 does
not show these retries."*

**Table 11-1 derivation** (already implemented — see §5): one coding activity per architecture
component; dependencies from the call chains counted exactly once; implicit edges added
(all-but-Resources → Logging; Clients and Managers → Security); **inherited/transitive edges
collapsed**; noncoding activities appended; M0 forced so no construction starts before SDP review.

## 4. Verified state of the world

Measured against `.aiarch/state/project.json` and the code, 2026-09-09.

| Fact | Value |
|---|---|
| Slot 9 `activityList` (derived, drift-gated) | **40** activities |
| `.activityConstruction` (legacy head-state) | **69** records |
| Intersection of the two id sets | **9** |
| Activities with `componentId` | 31 of 40 — resolving to **26 distinct components** (not injective: each of the 5 managers carries `C-*` **and** `U-SPA-*`) |
| Components with **no** activity | 11 (`web-client`, `logging`, `security`, `message-bus`, `project-git-repo`, …) |
| Records carrying `phases[]` | **1** (`G-SPA`) — and it is **non-monotonic**: `requirements` incomplete while all later phases are complete, at `buildStatus: integrated` |
| `.phaseArtifacts` | 1 record across 9 declared maps |
| `.activityGit` | absent entirely |
| `reviewPolicy` | `{}` — `PolicyPanel` silently substitutes a hardcoded 3-rule constant |
| `SupervisionCap` | **3** |
| Episodes | gitignored JSONL sidecar, `TargetRef` = bare activityId, **no phase field**; live endpoint returns `[]` for every activity probed |
| Headline "ACTIVITIES 44 in network" | mislabelled — 44 = 40 activities + 4 milestones |

Three structural findings:

- **`CoarsePhaseFor` / `CoarseBuildStatusFor` are dead code** (`projectstateaccess.go:7058`, `:7069`).
  Documented as the compute-at-read entry points; the read path passes stored values straight
  through (`systemdesignmanager.go:3369-3370`). Every stored/derived contradiction is silent.
- **M0 gates nothing.** `deriveMilestones` emits it with no fan-in (`estimationengine.go:1831`),
  `resolveDependencySatisfied` treats a milestone with no `DependsOn` as satisfied
  (`constructionmanager.go:1024-1027`), and no committed dependency names it.
- **Slot 10 is not materialized.** `materializePhase2Draft` discards two of four return values —
  `list, _, _, err := ...` (`projectdesignmanager.go:3181`). The network is still agent-authored,
  held honest only by the CI drift gate.

## 5. Domain rulings (system-architect)

### R1 — Model all 12 Figure A-1 tasks. No review folds.

Folding a review into its preceding task destroys the exact distinction the list view is built
on: an AI task opens an episode, a review task opens the artifact under review. The
AI-task/review-task alternation **is** Figure A-1's structure, and the retry rule has no
*preceding* to name without it.

Emission is one rule, not a per-type table:

1. A task exists iff its lifecycle phase is in the activity's `ProfileFor` subset
   (`projectstateaccess.go:7322`). `ProfileFor` is and remains the authority.
2. Nine of the twelve are invariant — present whenever their phase is.
3. `someConstruction` and `testClient` are **conditional-emit**: rendered only when an attempt
   record exists. Emitting a row for work that never happened is the "renders a lie" failure.
4. **Words are profile data; the task set is not.** The twelve task keys, which of them a profile emits (rule 1), each phase's gate, the weights and every attempt id are profile-invariant and are never tabulated per type. What a profile *calls* each phase's work task and gate task, and that phase's binary exit criterion, are per-profile display copy. They sit on the same row as the phase's `Label` and `Weight`, in the one table `ProfileFor` projects from, and never in a second switch that restates the phase subset. Service reads Figure A-1 verbatim. Every other profile obeys one vocabulary rule: **the phase `Label` is the name of one of its own tasks** — the work task, the gate task, or `<work> & <gate>` — so a phase and its tasks never carry near-synonyms. Hence `testing` reads "Doc Review" for Documentation and "Convergence Verification" for Deployment, exactly as the phase does. The two conditional tasks keep the book's name on every profile. Where a profile's label differs from the book's, the book's name renders beside it as secondary text and stays searchable. An exit criterion is the gate's success stated in the profile's own terms; no exit criterion is authored in the SPA.

Resulting counts: service 12 · frontend 12 · deployment 8 · documentation 8 · uiDesign 5 ·
integration 2 · testing 5 profile-dependent variants.

(Deployment and Documentation were published here as 9 apiece. They are **8**: both profiles
carry three phases — DetailedDesign 3 + Construction 3 + Integration 2. Corrected against
`ProfileFor`/`TasksForPhase`, and pinned by `TestTasksForProfile_PerTypeTaskSets`, which now
asserts the whole task SET rather than a count a wrong set would also satisfy.)

(Rule 4 amended 2026-09-12. As published it said task labels "inherit" the phase `Label`, which does not say which of a phase's two or three tasks inherits it, and it was silent on exits. That silence left the SPA with five generic exit sentences that were false for ten of eleven profiles — N-STP "code-complete", N-IT's Smoke Pass "requirement captured". "No per-type task tables" meant the task *set*; it never forbade per-profile words. Implemented in 8beec0c; consolidated into `ProfileFor`'s own table, and conformed to the phase `Label`s, by the follow-up.)

**Two `gen-uiprofiles` defects are in scope**, because that generator is the seam carrying this
vocabulary to TS: its header and the generated file cite
`internal/resourceaccess/projectstate/activityprofile.go`, **which does not exist**; and it emits
only `TestVariantPlan` though the server carries five variants with materially different weights
(testing is 7 of 40 activities).

### R2 — Retries are an append-only `TaskAttempt` ledger

Not a task-with-attempts-array. The tasks that did *not* happen must still render, and their row
set comes from R1's profile-derived vocabulary, not from storage — a stored skeleton would
duplicate `ProfileFor` in the data and let the two drift, which has already happened once and
been cleaned up. Append-only is also safe under Temporal retry, and `ArtifactSlot.reviewThread`
is existing precedent for a round-numbered append-only ledger.

```
ActivityConstructionStatus.attempts []TaskAttempt   // ordered, append-only

TaskAttempt {
  attemptId   string          // "<activityId>:<task>:<n>" — deterministic join key
  task        MethodTask      // one of the 12
  phase       LifecyclePhase  // denormalized for query
  attempt     int             // 1-based, per (activity, task)
  actor       TaskActor       // agent | human | system
  startedAt   time
  endedAt     *time
  outcome     TaskOutcome     // pending | passed | rejected | failed | skipped
  evidence    EvidenceRef     // { kind: episode|artifact|contract|git|none, ref }
  feedback    *ReviewFeedback // gate tasks only
  provenance  Provenance      // REQUIRED — see R7
}
```

Click behaviour falls straight out of `evidence.kind`; no second lookup table.

Consequences accepted with this shape:

- `PhaseCompletion.Completed` becomes **derived**, not written: a phase is complete iff its gate
  task's latest attempt is `passed`. That is App A's binary exit verbatim.
- `PhaseCompletion.ArtifactRef` is **superseded and deleted**, not fixed. It is the gate attempt's
  `evidence`.
- A rejected design review renders as four rows under one phase:
  `detailedDesign#1 passed → designReview#1 rejected → detailedDesign#2 passed → designReview#2 passed`.
- One new write verb: `RecordTaskAttempt` (append, upsert-by-`attemptId` for `pending → terminal`).

Author camelCase on the wire. `ProducedArtifact`'s PascalCase keys inside an otherwise-camelCase
record are a contract-authoring defect — cosmetic, carries no lie, **earmarked** — but do not add
a second one.

### R3 — Qualify the four levels; ban the bare word "phase"

| Level | Canonical name | Go | Wire | TS | UI copy |
|---|---|---|---|---|---|
| Project lifecycle (1/2/3) | **Project Phase** | `projectstate.Phase` | `Phase` | `ProjectPhase` | "Phase 1 — System Design" |
| App-A grouping (5) | **Lifecycle Phase** | `ActivityMethodPhase` | `phase` | `LifecyclePhase` (was `CanonicalPhase`) | "Detailed Design" |
| Figure A-1 unit (12) | **Task** | `MethodTask` | `task` | `MethodTask` | "task" |
| One execution | **Attempt** | `TaskAttempt` | `attempts[]` | `TaskAttempt` | "attempt 2 of 2" |
| ch11 grouping of activities | *(does not exist)* | — | — | — | **banned** |

Two renames required: `ConstructionRow.phase` (`types.ts:658`, which holds a Lifecycle Phase under
the most collision-prone name in the codebase) → `currentLifecyclePhase`; and `CanonicalPhase` →
`LifecyclePhase` in the same `gen-uiprofiles` touch.

**Enforcement:** add bare `phase`/`Phase` to the construction-view arch gate's banned-string list
with the two blessed types exempted. Precedent: the paramguard gate already holds 74 strings and
is mutation-tested. A naming ruling with no gate is a suggestion.

### R4 — Weights stay in `ProfileFor`, per-type, with no project override

Löwy explicitly permits per-type tuning and the table is already built and correct in shape.

- **Not `project.json`.** A stored weight edit silently restates history — yesterday's 85% becomes
  today's 60% with no diff anyone reads. That is an invisible earned-value integrity violation.
- **Not method-assets.** These weights are executable arithmetic; splitting the formula from the
  code that computes it re-creates the hand-mirror defect `lifecycleTemplates.ts` already suffered.
- **Override authority: nobody, this wave.** If ever justified, the only legal channel is the one
  the activity list already uses — a justified, reviewed delta applied render-on-read
  (`applyOverrides`, `estimationengine.go:1875`, which rejects any override lacking a written
  justification). Never a hand edit to a stored number. Earmarked.
- **No per-task weights.** Table A-1 weights *phases*. A phase is worth its full weight when its
  gate passes and zero before. Fractional credit for a passed non-gate task is precisely the
  anti-pattern Löwy names.

### R5 — Layer projection: one new server-side field pair

`DerivePlan` already performs the Table 11-1 derivation including the transitive reduction
(`estimationengine.go:1315`, `:1610`). No changes to it or the drift gate.

The layer render needs exactly one new projection, and it must live on the **server** so the same
rule holds for the Structurizr render-on-read and the MCP tool output:

| Activity | Layer |
|---|---|
| `C-<id>` | `component.layer` |
| `R-<id>` | `resource` |
| `U-SPA-<id>` | `client` — **regardless of the referenced component's layer** |
| `U-SPA-S`, `G-SPA`, `N-*` | no layer — project-wide band |

The third row is the trap: `managerSPAActivityFor` sets `ComponentID = <manager>.ID`, so the naive
`componentId → component.layer` join places a Client-layer surface on the Manager row. **The layer
of an activity is not the layer of its component.**

**Edges: render them, and make upward edges alarm.** Correct Method layering yields zero backward
edges (measured across all 58 in prior work). In a layer-positioned render, any upward or sideways
edge is therefore a **design-defect indicator**. Draw it in an alarm channel; never hide it or
route around it. This is what earns the view its keep — it becomes a live App C layering check
that runs whenever anyone looks at it.

**Deliverable: `layer` + `layerBand` on the activity read model. Nothing else.**

**Remaining role for AI in the graph: none.** No LLM output may determine a node's existence,
position, layer, edge, or lifecycle state.

**`activityListOverrides` gap: confirmed OUT, with a tripwire.** An override may only replace
`effortDays`/`riskBucket` on an already-derived activity, so the 25 overrides cannot add or remove
a node — the rendered activity set is byte-identical with or without them. **Tripwire:** this must
be fixed before any view surfaces effort, cost, float, or critical path from a re-materialization,
i.e. before the graph gets a CPM overlay. Ship the layer render without numbers and the gap stays
inert.

### R6 — Minimum honest defect set

Test: *does leaving it unfixed make a view state something false, or merely leave it unknown?*
False is disqualifying; unknown is shippable.

**MUST fix:**

- **D-classify — 60 of 69 mistyped** (not ~25). Slot 9 uses `C-agentic-job-access`,
  `.activityConstruction` uses `C-AA`; `activityMetaByID` (`systemdesignmanager.go:3396`) misses 60
  rows, which get a zero `ActivityItem`, fall to the no-rule arm of `ClassifyActivity`
  (`projectstateaccess.go:7607`), and `ClassifyType` returns `Deployment` (`:7632`).
  **Ruling: kill the lenient fallback, do not patch the ids.** `ClassifyType` returns
  `(ActivityType, bool)`; on failure the row renders **Unclassified with no lifecycle sub-rows at
  all**. Patching ids would restore a false lifecycle for rows whose classification is still a guess.
- **D-phases-dropped** — `mapConstructionRow` (`wire.ts:370-379`) never reads `w.Phases` though
  `schema.ts:1624` carries it. Also restore `Label` in `phasesToContract`
  (`systemdesignmanager.go:3407`), without which a Frontend activity renders Service labels.
- **D-gspa — "Integrated at 85%" is three bugs:** (a) the row is seeded with the Service five-phase
  profile though slot 9 says `workerClass: ui-designer, coding: false` → `uiDesign`, a two-phase
  profile; (b) the read path trusts stored `phase`/`buildStatus` verbatim — **wire the two dead
  `Coarse*For` functions**; (c) `CoarseBuildStatus` returns `Integrated` on Integration-done alone
  without checking earlier phases (`:7108`) — require **all** profile phases complete, which makes
  85%-and-Integrated impossible rather than merely unlikely.
- **Provenance stamp** (R7) and the **layer projection** (R5).

**DEFER:** verdict persistence (land the *shape* — type, store verb, wire field, SPA reader — but
do not touch `awaitPhaseDecision`); `ArtifactRef` (defer, then delete — superseded);
`.phaseArtifacts` / `.activityGit` emptiness (renders as an honest "no artifact captured").

**OVERRULED INTO SCOPE — the one thing that cannot wait.** `episodeRecordFor` stamps
`TargetRef = string(in.ActivityID)` (`constructactivity.go:264`). Its caller `runPipeline` already
has `phase` in hand and the redraft loop already has the attempt count. Change `TargetRef` to carry
the **attemptId** now, even though nothing reads it yet — roughly three lines. Every episode
written between now and the capture wave is otherwise **permanently unjoinable**, because the
information that would join it exists only at write time and cannot be backfilled.

**Status vocabularies — in scope, narrowly.** Split the client union: `BuildStatus`
(server-owned, 4) × `Readiness` (client-derived, 3: `blocked`/`eligible`/`not-started` are
network-position facts, not build facts). Delete `in-detailed-design` — a Lifecycle Phase fact
wearing a status costume; the task ledger says it better. `ActivityConstructionPhase` becomes a
always-derived progress state, never stored-and-trusted.

**40-vs-69 — slot 9 is authoritative for what exists.** Three explicit buckets, nothing hidden or
merged: 9 matched (real state) · 31 planned-no-record ("not started / no record", **never** Done) ·
60 superseded (segregated, collapsed, read-only). Per D8 the third bucket is deleted once the 40
are in good shape. Side effect while it stands: it makes `.constructionProgress`'s
"100.0% earned, 1070/1070 effort-days" visibly untrustworthy, which it is.

### R7 — The provenance contract

Required, closed-enum field on every `TaskAttempt`. Not optional, not `omitempty`.

```
RecordOrigin:
  "synthesized"  // ZERO VALUE — fabricated for this wave; no event backs it
  "backfilled"   // reconstructed from real evidence recorded elsewhere
  "observed"     // written by the running system from a real event
```

**The zero value is `synthesized`.** That is the whole design: a missing or dropped stamp must fail
*suspicious*, never *blessed*. Decoding a `TaskAttempt` with no origin is a hard error.

**Three values, not two.** Some rows genuinely can be reconstructed from real artifacts — a frozen
contract in `.serviceContracts`, a merged commit. Those are not lies and must not be tarred as
fakes; they were also not observed.

Five rules:

1. Zero value is `synthesized` (above).
2. Carry the reason: `{ origin, generator, generatedAt, basis }` — `generator` names the producing
   tool and sha, `basis` names what it derived from or is empty for pure fiction. Do **not**
   overload the existing `Provenance` type (`projectstateaccess.go:2341`); it answers a different
   question.
3. **Contagion.** A value derived from any synthesized input is itself synthesized.
   `PhaseCompletion.Completed`, activity %, and project earned value inherit the **worst** origin
   among their inputs; the read model carries per-activity and per-project `worstOrigin`. Without
   this, rows are honestly badged while the header launders a fabricated 93.7% — the single worst
   lie available to this wave.
4. **Synthesis is a committed artifact, never a render-time behaviour.** It lands as a reviewable
   diff, called out in the commit message, revertible in one commit. **No server code path may
   fabricate a row at request time.**
5. Never merge synthesized state to `main`'s `project.json` without an explicit founder decision
   recorded in the commit.

## 6. Product rulings (product-manager)

**The Tasks row is one decision: `(activityId, gate, attempt)` — not an activity.** An activity
enters the queue three, four, five times; on attempt 2+ the only question that matters is *"did
they fix what I asked for?"*, which an activity-keyed row cannot express. Rounds must thread to
their original.

**`SupervisionCap: 3` inverts the brief.** The realistic queue is 1–3 items, each holding a third
of all throughput. Do not build bulk-triage ergonomics; build depth and speed per item, and make
the row scream how much is idling behind it. Headline: **"3 of 3 workers idle, waiting on you."**

**Seven fields to triage without opening:** the ask as a verb sentence about the artifact (not the
machinery) · blast radius (`N activities blocked`) · waiting time in human scale · attempt/round ·
size and shape of what you're about to read (`Contract · 12 ops, 3 changed`) · machine verdict
(red CI means *don't read the diff yet* — the highest time-saving field on the row) · whether it is
a non-overridable risk-floor gate.

**Failed activities belong in the same table**, with a reason column. "The machine is waiting for a
decision" and "the machine stopped" are the same job to the human: *the pipeline is stopped and I'm
the reason it can restart.* Two screens means one gets ignored.

**Wave-1 actions: Approve · Send back with a required note · Open in GitHub.** Free text suffices
*provided the feedback reaches the redraft*. Three things must hold or send-back fails the customer:
the human's words persist visibly against the activity forever (today `reset()` discards them);
the human is told when the redraft budget is exhausted (today `redraftExhausted` sets and the
workflow **silently keeps waiting**, so the button becomes a no-op with no signal); and send-back
can target a different phase when the gate isn't the problem.

**In-app vs GitHub — the line is intent vs mechanics.** In-app: service contract / detailed design
(non-negotiable — a contract read as a diff is a contract read badly, and construction may not
begin against an unfrozen contract), SRS, test plan, UI design concept, and always the ask,
reviewer set, prior feedback and the decision itself. GitHub: code diff, deployment change, CI
investigation. **The rule that makes the split honest:** *the human must never have to leave the
app to discover whether they need to leave the app* — so GitHub-bound rows still carry CI status,
file count, ±lines, branch, and what changed relative to the frozen contract. The decision comes
back to the app; do not build a GitHub-approval round trip.

**Default sort: risk-floor gates · blast radius desc · age desc.** Not float — with a cap of 3
nearly everything in flight is effectively critical, so a float column is a near-constant.
**Age must not be primary**: a four-day-old doc review outranking a contract freeze with two agents
idle behind it is the exact failure being eliminated. Age earns a separate *staleness* flag, because
a long wait usually means "I don't understand this one" — a signal to change policy or take over.

**On decide:** the agent resumes automatically (any "now press start" kills the north star), and
confirmation is **evidence of the resume, not acknowledgement of the click** — the row transitions
in place to "resumed — now in Construction", lingers ~30s, then leaves. Failures must be loud: a
signal that lands nowhere currently produces a silent no-op, and a swallowed approval is worse than
no button, because the human walks away believing they unblocked the system. Then hand them the
next item.

**The conflict in the brief, resolved.** "Review exactly what you need to" (more gates) and "keep
the agents moving" (fewer gates) are mutually exclusive as stated. Resolution: **the queue is a
policy-tuning surface, not just a work list.** Every row offers "stop asking me about this class of
thing", and the view reports what got auto-approved so trust can be calibrated in both directions.
Success is not a fast queue — it is **a queue that converges toward empty as trust is earned**, with
the risk floor as the part that never converges.

**Acceptance metric for the wave:** time from an agent stopping at a human gate to that agent
resuming, and the share of it that is human wall-clock. If that number does not measurably shrink,
the wave shipped views, not value.

## 7. UX concept (ui-designer)

### 7.1 The inversion that makes "unknown" calm

> The tier-2 and tier-3 rows **always exist**, because Fig A-1 × the activity's profile is
> deterministic. Only their **state** is unknown.

An activity with zero history still expands into a complete, correct, quiet Method skeleton — 5
phases, ~12 tasks, weights, exit criteria — each dashed and stateless. Nothing missing, nothing
fabricated. It reads as *"here is the work, none of it has happened"* (true) instead of *"empty
container"* (useless) or *"12 green rows"* (a lie).

This is also why the founder's literal *"a complete activity should have at least one row per
lifecycle item"* cannot be honored as stated: 68 of 69 records have no per-phase history, and the
one that does completed **out of order**. Honoring it literally would fabricate ~12 green rows ×
68 activities and assert a false ordering.

### 7.2 Channels — magnitudes get geometry, states get one chip

Honoring the recorded diagnosis *"every magnitude gets a geometric channel, nothing with a geometry
gets a chip"*. States are categories, not magnitudes, so a chip is legitimate — but only one per
row, at the tier that owns it.

| Magnitude | Channel |
|---|---|
| effort days | node width (graph) / left bar length (list) |
| float | left rail band (reuse `bandTokens`) **+ always-visible numeral** (WCAG 1.4.1) |
| on critical path | border weight 2→3px + full-bleed rail — never a "CRITICAL" chip |
| phase weight | segment width in the 5-segment lifecycle spine |
| % complete | fill extent of that spine |
| retry count | stroke doubling + `×N` numeral |

| State | Channel | Chip? |
|---|---|---|
| `unknown` | hairline dashed outline, muted, no fill | **no** — chip-less *is* the signal |
| `absent` (not in this kind's profile) | gap in the spine, 40% opacity, struck label | no |
| `notStarted` | hollow `RadioButtonUnchecked` | no |
| `running` | teal dot — the **only** animated element on screen | yes (xs) |
| `awaitingHuman` | `awaitingBg`/`awaitingFg` + 3px accent left edge — loudest thing on screen | yes |
| `passed` | olive check | yes (xs) |
| `failed` | `dangerFg` on `awaitingBg` **+ inline `↻ Retry`** | yes |
| `superseded` | 55% opacity + `↻n` index | no |

**Failure is never terminal.** Every `failed` row renders `↻ Retry` inline, and the detail action
bar carries `↻ Run this task` in **every** state — including `passed` (re-run) and `unknown` (run
it for the first time). `failed` is amber-backed with a red foreground: "needs you", not "dead".

### 7.3 Provenance is an orthogonal axis

`recorded` → no mark (default is the absence of a mark). `reconstructed` → 3px hatched left rail
using the existing `scan()` texture helper, with **one** `≈ RECONSTRUCTED` badge at the *group*
header, never per row. `unknown` → dashed outline, no rail.

Two rules make this survivable at 528 rows: **stamp the group, not the row** (a screen of 300
chips reads as damage; a consistent hatch on a left rail reads as a *material*), and **sub-grade in
the tooltip** — `backfilled` and `inferred` share the hatch, and the tooltip names which, with
inference citing the input it guessed from.

New `ReconstructedBadge` joins `ComputedBadge`/`AuthoredBadge` in `project/computed.tsx` — a third
member of an existing family, not a new language.

**Explicit ruling: stop using `activeIdxFor`.** Anything derived that way is `reconstructed` and
hatched, or it is `unknown`.

### 7.4 Shell

One route. `▤ LIST │ ⬡ GRAPH │ ⚑ TASKS ③` segmented control inside a shared toolbar with
search / scope / kind / layer / sort, all persisting across lenses. One resizable right detail pane
the canvas lays out beside — **not** the current overlay drawer, which covers the graph you clicked
from. Default 520px, collapsible, width remembered; below 1200px it degrades to the existing
overlay `Drawer` (keep that code path).

Deep links: `?lens=list|graph|tasks&a=<activityId>&p=<phase>&k=<task>&n=<attempt>`. Tasks carries a
count badge — the only lens that asserts something is owed.

A persistent coverage strip sits above the content:

```
COVERAGE  40 derived · 31 mapped · 9 cross-cutting ‖ 69 legacy · 9 reconcile · 60 orphaned ⚠
```

### 7.5 Lens 1 — Activity list

Three tiers. **Tier 1 (activity)**: `▸ | float rail + CP weight | id + title + KindBadge | effort
bar | StatusChip | prov | % | last event | RoleAvatar | ⋯`. **Tier 2 (phase)**: a *rule*, not a
card — `┌ PHASE NAME ····· wt N · exit: <criterion>` with a binary `PASS / PENDING / —`; `absent`
phases render struck and dimmed. **Tier 3 (task)**: the clickable unit —
`▫ <task> | state | prov | when | who | ↻N`.

**Attempts collapse into the `↻N` counter**; the row shows the *latest* attempt's state, and
clicking the counter expands attempts newest-first. A 3-retry task stays 1 row.

At rest: 40 collapsed tier-1 rows and nothing else.

Navigability without a DataGrid (no free sort/filter/virtualization):

- **Never offer "Expand all"** — that is the 528-row trap. Offer **"Expand to current phase"**,
  which opens only the in-flight phases (today: 1–3 rows).
- Scope chips: `All · Critical path · Near-critical · Awaiting me · In flight · Has retries ·
  Reconstructed only · Unknown only`.
- Search matches id, title, componentId; a tier-3 match auto-expands its ancestors.
- **Sort applies to tier 1 only** — tiers 2 and 3 are a *sequence*; sorting them is nonsense and is
  not offered (say so in the menu's helper text).
- **No virtualization this wave.** Resting DOM is 40 rows; worst realistic expansion ~90. Earmark
  windowing at >150 tier-1 activities.

Implementation: `RichTreeView` + `slots.item` with a custom `TreeItem` wrapping our own CSS grid
(the tree is *derived*, so a data-driven `items` prop is the right shape). `apiRef` drives
search-reveal and expand-to-current-phase. Selection is controlled and mirrored into URL search
params, so the detail pane never owns selection state.

### 7.6 Lens 2 — Activity graph

Nodes positioned by **Method layer**, top-down: `CLIENTS / MANAGERS / ENGINES / RESOURCE ACCESS /
RESOURCES / SYSTEM-WIDE`, with utilities in a side bar and **no lines** (ratified convention).
Milestones leave the canvas and become a **gate ribbon** across the top. Edges are the architecture
call-chain relationships from `.systemDesign.relationships`, not CPM dependencies — a DAG that
already lays out cleanly, versus the CPM spaghetti staircase.

**A component node hosts 1..n activity lanes**, because `componentId` is not injective. Each lane
body is the 5-segment lifecycle spine: segment width = Table A-1 weight, fill = state. That is the
mini-lifecycle, rendered as geometry rather than as a graph.

`G-SPA` and `U-SPA-S` join **Clients**; the 7 `N-*` join a wide **System-wide** band.

**Three levels of detail.** Nesting a full 12-node Fig A-1 sub-flow inside every node does not
work: 26 × 12 = 312 nodes and ~350 edges; a legible 12-node fork needs ≥240×160px, pushing the
canvas past 1600×2400 with nothing readable at fit; and 44 *simple* nodes are already confetti.

- **LOD-0** (fit, or >12 nodes in viewport): component card + lane spines. Geometry and fill only.
- **LOD-1** (zoom >0.8 or hover): spine segments gain phase labels and per-task tick marks, doubled
  stroke where a task has retries.
- **LOD-2** (node **selected**): that one node expands in place to ~1000×220 and renders the real
  nested React Flow sub-flow with `parentId` — Fig A-1's fork and rejoin intact. Every other node
  drops to LOD-0 and dims to 35%. *(Cut from this wave — see §10.)*

Two LOD-2 simplifications, both defensible: `Construction ⇄ Test Client` renders as one
double-height tandem cell with an internal two-headed edge; and **retries are not extra nodes** —
a `↺N` badge and doubled border. Löwy himself omits retries from Fig A-1 for clarity; the graph
follows him, the list carries the truth.

**Coverage as a first-class signal:** the 11 components with no activity render **hollow**, labelled
`no activity`. `ACT-COMPONENT-COVERAGE` is a real gate, and a hollow node in the layer stack is the
most legible possible rendering of "the architecture has a component the plan never builds".

**Mandatory carry-over:** copy the `signatureOf()` + module-level `selectionStore` pattern from
`NetworkView.tsx:106-128`. The console polls at 1.5s while cascading; without it the operator's
selection and viewport are wiped mid-glance. This is a recorded, previously-fixed bug.

### 7.7 Lens 3 — Tasks

Columns: WHAT (activity › phase › task, carrying the float rail and CP weight) · WHY (which policy
rule opened the gate) · WHO (reviewer set / role) · WAITING (age, loud past SLA) · BLAST (`↓N` +
float + CP flag) · ACTION.

Header: **"3 tasks are blocking 11 downstream activities · 18 days of critical path stalled."**

`[Review]` opens the detail pane in place with the artifact and inline Approve / Send-back
(`PhaseGatePanel` + `CommentProvider` anchors + `toWire()` — that path already works end to end).
`[GH ↗]` is the escape hatch, deliberately secondary.

**Permanent degraded banner until `reviewPolicy` is populated:** *"No review policy recorded for
this project. Showing the 3 default gates; tasks are derived from activity status only."* Silently
presenting a hardcoded constant as project policy is exactly what this rewrite exists to stop.

**Empty state is not a dead end.** Replace *"No interventions pending"* with **"Nothing needs you."**
+ `17 eligible · 3 in flight · 14 blocked` + `[ Resume construction ]`.

### 7.8 The shared detail surface

Invariant header — breadcrumb · state chip · provenance chip · attempt selector · exit criterion +
weight. Invariant action bar — `[✓ Approve] [↩ Send back] [↻ Run this task] [⋯]`, with
`↻ Run this task` present **and enabled in every state**. That makes the founder's "the app always
lets u retry" ruling structural rather than conditional.

Four bodies:

- **Agentic episode** — reuse `EpisodesPanel` + `EpisodeTimeline` verbatim (outcome, duration,
  model, workerClass, the 4-way token split with its "tokens (main loop)" caption, turns, cost,
  tool counts, subagent spans, lineage tree). Add a subagent-span gantt strip. **Required honesty
  caption:** `EPISODES FOR THIS ACTIVITY · N — not attributable to a specific lifecycle task (no
  phase on the episode record)`.
- **Review task** — artifact above, verdict below. Artifact pane armed with `CommentableList`-style
  anchors into `CommentProvider` so send-back carries item-granular comments. Verdict = per-reviewer
  rows. Where the only surviving verdict text is prose in `produced[].Note`, render it stamped
  `≈ reconstructed from the produced-record note` — never as a structured verdict.
- **Design artifact**, dispatched on the existing `classify(row)`: `service` → `ServiceContractView`
  / `ContractCodeFlow` / `ContractComponentFlow` (the code-level diagram) · `uiDesign` →
  `FrontendArtifactView` (the UI spec) · `testing:*` → `TestPlanView` / `ScenarioBrowser` /
  `DynamicViewFlow` (the test dynamic diagrams). All three the founder named already have renderers.
- **Unknown** — the majority body, designed as a feature: what the task is, its exit criterion, its
  weight, the retry rule, and one enabled `↻ Run this task`. Calm, no error tone, no red, no
  spinner. A distinct sibling body for `absent`: *"Deployment activities carry no Test Plan phase.
  This is by design, not missing data."*

## 8. Reuse

Tokens via `useTokens()` from `utilities/theme/themes.ts` — no hardcoded colour anywhere; all five
themes must keep working. The `scan()` texture helper becomes the reconstructed hatch.

Components: `construction/status.tsx` (extend the union, invent no colours) · `KindBadge` ·
`lifecycleTemplates{,.gen}.ts` for tier 2 entirely (**stop using `activeIdxFor`**) ·
`primitives/RecordTable`, `StatTile` · `project/computed.tsx` (+`ReconstructedBadge`) ·
`project/bandTokens.ts` · `NetworkView.tsx:106-128` (mandatory) · `ContractCodeFlow` /
`ContractComponentFlow` for xyflow visual language · `components/flow/*` (`ArchitectureFlow`,
`flowLayout`, `C4Node`, `DynamicViewFlow`, `DeploymentFlow`) — the layer-band layout already exists ·
`EpisodesPanel` / `EpisodeTimeline` verbatim · `PhaseGatePanel` / `InterventionQueue` / `PolicyPanel` ·
`renderers/*` + `artifactClassification.classify` · `CommentProvider` / `setAnchor` / `toWire` ·
`ExperienceChrome` / `ChatRail` unchanged · `GitRowMeta` / `RoleAvatar` ·
`UIIdentifiers.ts` for new testids.

## 9. Acceptance criteria

1. **The evidence toggle ("Observed only").** With "Observed only" **on**, all three lenses render
   without crashing and keep **every** activity: slot 9 decides what exists (R6), so the toggle
   never removes a row. It strips every attempt whose origin is not `observed`, together with the
   phase completions, status and current phase the server derived from them, so an activity known
   only from reconstructed evidence reads as not started, with no hatch and no badge. Stripped is
   not unrecorded: where the toggle hid a recorded row's evidence, the surface says how many
   reconstructed attempts it hid, and "unrecorded" appears only where nothing is recorded at all.
   Any lifecycle progress that remains with the toggle on was observed. If not, something is
   fabricating.

   (Amended 2026-09-12. As published this criterion called the control "hide synthesized" and
   expected "approximately one activity with lifecycle data", which read as a row filter. The
   designer UX pass (P1-11) found that filter dropped 23 of 29 real activities; the ruling kept
   every row, stripped the untrusted evidence instead, and renamed the control "Observed only"
   (0ef0c9b). The designer re-check (B1) then found the pane calling a stripped row
   "unrecorded"; the hidden-attempt count and the recorded/unrecorded split are from that pass.)
2. **No laundered aggregate.** The project EV/progress header shows **"—"** whenever
   `worstOrigin != observed` — not a badged number, not a footnote.
3. **Retry is never absent.** `↻ Run this task` is present and enabled in every state, in every
   lens's detail pane.
4. **Unclassified is visible.** Activities whose type cannot be resolved render as Unclassified with
   **zero** lifecycle sub-rows.
5. **The join key holds.** Every `TaskAttempt` and every newly written `EpisodeRecord.TargetRef`
   carries `<activityId>:<task>:<n>`.
6. **Selection survives the poll.** Selection and viewport are stable across the 1.5s cascade poll
   in the graph lens.
7. **Playwright-verified per view.** Each lens is driven in a real browser and reviewed by the
   ux-reviewer before the next is started (per the founder's standing UI review loop).
8. **Throughput metric captured** — time from agent-stop-at-gate to agent-resume (PM's acceptance
   metric), even if only instrumented.

## 10. Cut list

Cut, in this order: **LOD-2 nested sub-flow** (selecting a node opens the detail pane instead;
highest effort, lowest frequency, and the list carries the same information losslessly) ·
**attempt sub-rows** (ship the `↻N` counter + attempt selector; there is no retry data to expand
yet) · **per-task episode attribution** (blocked on the missing field) · **artifact bodies for
deployment / documentation / integration** (fall back to the unknown body; ship the three the
founder named) · **sort options beyond network-order and float-asc** · **a separate graph filter
bar** (it shares the toolbar — that is the point of the lens model) · **editing review policy from
Tasks** (read-only + link) · **virtualization**.

**Do not cut** — these are the wave's reason to exist: the provenance hatch, the unknown body, the
coverage strip, the always-enabled retry.

## 11. Earmarks (explicitly deferred, recorded so they are not lost)

- Verdict persistence — wire `sig.Feedback` through `awaitPhaseDecision` into the redraft.
- `redraftExhausted` silently continuing to wait — must signal the operator.
- `phase` (and ideally `task`) on `EpisodeRecord`; durable, non-gitignored episode storage.
- `.activityConstruction` re-keying, then **deletion of the 60 legacy records** once the 40 derived
  activities are in good shape (founder D8).
- `activityListOverrides` unreachable — **tripwire: fix before any CPM overlay on the graph.**
- Slot 10 not materialized (`list, _, _, err :=`).
- M0 gates nothing.
- `ProducedArtifact` PascalCase keys.
- Per-project weight override via a justified render-on-read delta, if ever needed.
- `ACTIVITIES 44 in network` → `NODES 44 · 40 activities + 4 milestones`.
