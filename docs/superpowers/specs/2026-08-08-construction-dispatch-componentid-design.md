# Design: authored `componentId` on the activity list, and a construction pump that fails loudly

**Date:** 2026-08-08 · **Status:** approved (founder) · architecture ruled (system-architect) ·
book fidelity ruled (product-manager, against `research/rightingsoftware/`)
**Fixes:** `docs/bugs/construction-dispatch-no-contract-key.md`
**Drives:** AC iteration 0 of the self-improvement pipeline — a todomvc benchmark run that
completes construction.

---

## 1. Problem

A real benchmark build of the todomvc reference project cleared all of Phase 1 and all of
Phase 2, advanced to `PhaseConstruction`, and then dispatched nothing. The serve log looped
`no service-contract key resolves for activity — skipping dispatch` 44 times; zero
construction agents ever spawned.

Three distinct defects sit behind that one log line, in
`server/internal/manager/construction/constructionmanager.go`:

1. **Chicken-and-egg.** `nextEligibleActivity` (:737) resolves an activity to a component
   *only* through `proj.ServiceContracts` (`resolveComponentID`, :822 — either a produced
   `service-contract` artifact title matched against the contract map, or a fuzzy normalized
   match of the activity title against the map's keys). On a fresh project that map is empty:
   detailed-design has not run, and detailed-design is an internal *phase* of the very coding
   activity the pump refuses to dispatch. Nothing ever populates the map, so nothing ever
   dispatches.
2. **The gate is applied to activities that have no component at all.** `N-*`, `I-*`, `G-SPA`
   and `R-TRS` are non-component work, and every construction slash command in
   `.claude/commands/` explicitly documents that `component_id` *may be empty*. The pump
   nonetheless demands a resolvable component for them.
3. **One unresolvable activity stalls the whole network.** On failure `nextEligibleActivity`
   returns `(zero, false)` for the entire sweep instead of considering the next candidate, and
   the pump reads that as "cascade quiescent".

Nothing downstream of dispatch has ever executed in a benchmark, so the fix must be validated
before it can be trusted.

## 2. Decisions

Founder-ratified, with one correction forced by the book itself (§2.5) — which withdrew part of
the first architecture ruling.

### 2.1 Component identity is authored, never derived

A component identity is an **authored field on the Phase-2 activity list**, not something the
runtime recovers by string matching. No fuzzy-match fallback ships in product code. The
existing benchmark state repo that predates the field gets a one-time authored backfill.

### 2.2 It marks structural coding work, and is self-declaring

`componentId` is present on **structural** coding activities — the ones derived from the
architecture, one per component in the base list — and absent on everything else. Its presence
*is* the structural marker; no predicate over other fields, and no name matching, decides it.

### 2.3 An unclaimed component or a bogus id is rejected at authoring time

The activity list draft is refused, with a message actionable enough that the drafting agent fixes
it in-loop, rather than the defect surfacing hours later as a stalled pump. The forcing rule is
component *coverage*, not per-activity presence — see §5.

### 2.4 An undispatchable activity fails loudly

Visible to the operator in the app. Not a warning in a server log — that is precisely how this
defect consumed an entire benchmark run undetected.

### 2.5 Correction: "a coding activity builds one component" is not the book's rule

The rule as first stated — `Coding == true ⇒ componentId required` — was checked against the
book (`research/rightingsoftware/OEBPS/xhtml/`) and is false.

The book's top-level split is binary, coding / noncoding, stated verbatim in ch.7 §5.3, ch.7
"Critical Path Analysis", ch.11 "The Mission", and App A "Activity Life Cycle" ("be it a service
or a noncoding activity"). Where Löwy refines it, he refines the **coding** side. Ch.13 §1:

> "The team classified the TradeMe activities into three categories: **Structural coding
> activities / Nonstructural coding activities / Noncoding activities**"

Structural = one per architecture component (Table 13-1). Nonstructural = "coding activities that
**did not map directly to the architecture**" — Table 13-2 in full: Abstract Manager (30d,
Developer), System Test Harness (25d, Test Engineer), Regression Test Harness (10d, Developer).
So the premise is contradicted by the book's own second worked example: Table 13-2 is a table of
coding activities that build no component.

The first draft of this design proposed a carve-out keyed on `isIntegrationActivity`
(`crossartifact.go:251` — `I-*` prefix / "integration" in the title). **That is withdrawn.** The
class of coding activities naming no component is a first-class Method category, not an exception
to be carved, and a prefix classifier misfires the moment an Abstract-Manager-style base service
or a coding-classed harness appears with an `I`-free name. Self-declaration replaces it:
`componentId` present = structural, absent = nonstructural.

`I-UC1..I-UC5` therefore keep `coding: true` and simply author no `componentId`. Nothing in
archistrator's committed plan is illegal under this rule.

Two further notes from the book reading, both out of scope here and tracked in §10:

- The book's own integration *activity* is not a multi-component span. Ch.11 "Compressing the
  Clients" splits one client into "a development activity against the simulators, and… an
  integration activity against the Managers" — the tail half of **one** component's lifecycle,
  priced at 5 days in ch.13, and present **only in the compressed solution**. Neither
  normal-solution worked example (ch.11 Table 11-1; ch.13 Tables 13-1/2/3) contains a single
  integration activity; in the normal plan integration is the 15% Integration *phase* inside every
  coding activity (App A Table A-1).
- Under that reading our `the-method-activity-list` skill's "one `I-UC*` per core use case in the
  base list" mandate is off-book, and the architect owns that defect.

## 3. Schema

One field on `projectstate.ActivityItem`
(`server/internal/resourceaccess/projectstate/projectstateaccess.go:3734`):

```go
// ComponentID names the committed System component (systemDesign Components[].id)
// this activity builds. AUTHORED at Phase-2 draft time — never derived by matching.
// Its PRESENCE declares the activity STRUCTURAL (ch.13 Table 13-1, one per
// architecture component); its absence declares it nonstructural (Table 13-2:
// harnesses, base services) or noncoding. When present it must resolve. A noncoding
// provisioning activity like R-TRS may name the Resource component it provisions.
ComponentID string `json:"componentId,omitempty"`
```

- **Bare string, not a typed alias.** `Component.ID` is itself a bare string; no
  `projectstate.ComponentID` type exists. The validation that matters is referential
  (`id ∈ committed Components`), which no Go type expresses, and an alias would ripple through
  clientgen, OAS, the webApp codecs and the construction manager's Temporal payloads for
  nothing.
- **`omitempty`.** Absence is a legitimate authored state; the stored document must not carry a
  misleading `componentId: ""` on `N-*`/`G-*`/`R-*` items.
- **`RequireModelFields` is deliberately NOT extended.** Read-back parity in `decodeSlotsMap`
  would make every pre-field committed state *undecodable* — CI `validate` would hard-fail at
  decode and the SPA could not render the project at all. The F81 rationale does not apply:
  absence is distinguishable post-decode (empty string is the honest "not authored").
- **No `layer` field.** Layer is fully derivable from the committed System component; storing it
  twice recreates the drift class F81 exists to kill.

**Cardinality rules.** Several activities may share one `componentId` — `U-SPA-CONSULT`,
`U-SPA-AMEND` and `U-SPA-EMBED` all build `todo-owner-client`, the ratified phase-split of one
client component. `ACT-COMPONENT-COVERAGE` therefore stays a **≥1** coverage check, never `== 1`.
The book supports this directly: ch.11's client split produces *two* activities — development
against simulators, then integration against the Managers — **both belonging to one Client
component**, and the compressed solution's D-splits add more.

`== 1` is not merely stricter, it is backwards. Consider the family slip where `U-SPA-AMEND` drops
its `componentId` while `U-SPA-CONSULT` keeps it: `== 1` *passes* that defective plan (one
claimant) and *rejects* the correct one (three claimants).

The "one coding activity per component" line in the skill is Table 13-1's base-list authoring
default, not a gate invariant. A noncoding activity *may* carry a `componentId`
(`R-TRS` → `todo-record-store`); when present it must resolve.

## 4. The pump

### 4.1 Selection

`nextEligibleActivity` no longer consults `ServiceContracts` at all. It reads
`item.ComponentID` and looks it up in the committed `.systemDesign` Components by exact id.
This dissolves the chicken-and-egg: dispatching a coding activity runs its own detailed-design
phase, which *produces* the contract.

Deleted: `resolveComponentID` (:822), `matchContractKey` (:846), `normalizeIdent` (:870) — and
the orphan check for any other caller of `normalizeIdent` in that file.

`hydrateConstructionActivity` (:914) grows the resolved component as a parameter and sets both
`ComponentID` and `Layer`. `constructionActivity.Layer` is populated nowhere today and prints as
an empty string into every PR body (`constructactivity.go:764`); the systemDesign lookup that
selection now performs makes hydrating it a single line.

### 4.2 Three-state verdict

`nextEligibleActivity` returns `(constructionActivity, pumpVerdict)` instead of a bool:

| Verdict | Meaning |
|---|---|
| `verdictDispatch` | eligible and resolvable — dispatch it |
| `verdictQuiescent` | nothing eligible — the network is drained or waiting on deps |
| `verdictBlocked{activityID, reason}` | eligible but undispatchable |

`verdictBlocked` fires on the pump's **defensive** re-check — never trust storage, even
gate-protected storage — on **three conditions**: a non-empty `ComponentID` naming no component in
the committed `.systemDesign` (`ComponentUnresolved`); an authored dependency id
(`network.dependencies[].dependsOn`) naming neither a known activity nor a known milestone
(`DependencyUnresolved`); or a milestone dependency cycle (`DependencyCycle`). The latter two were
not part of the original design — see §4.3a, added after the live drain surfaced milestone
dependencies as a second dispatch-blocking defect.

An empty `ComponentID` on a coding activity is **legal**: it declares the activity nonstructural
(§2.5), and it dispatches with an empty `component_id` exactly as a noncoding activity does today
— which every construction slash command already documents as supported.

### 4.3 Loud failure

`PumpNextActivityWorkflow` (`pumpnextactivity.go:38`) on `verdictBlocked`:

1. Calls `ConstructionTransitionRecordActivityFailed`
   (`invokers.gen.go:241`) with an empty `railCredEnvelope` — the pause path in
   `projectsupervision.go` establishes that the local store ignores an empty cred and the git
   adapter mints just-in-time.
2. Reason: **three new closed-enum values**, added to the six existing values
   (`projectstateaccess.go:6652` ff.): `ComponentUnresolved` (wire `"componentUnresolved"`, ordinal
   6) for the componentId case described here; `DependencyUnresolved` (wire
   `"dependencyUnresolved"`, ordinal 7) and `DependencyCycle` (wire `"dependencyCycle"`, ordinal 8)
   for the milestone-dependency defects added in §4.3a below. (An earlier draft of this design
   proposed a single, differently-named enum value covering only the componentId case — that name
   never shipped and appears nowhere in the codebase; disregard it.)
3. Detail: names the activity, the defect, and — since `RecordActivityFailed` is sticky and there
   is no reopen/retry verb in the store — that amending the plan alone will not restart it.
4. Logs at ERROR, sets `dispatch = {Decided:true, Dispatched:false}`, and returns **without**
   ContinueAsNew.

This is durable and app-visible: `ActivityConstructionStatus.FailureReason` / `FailureDetail` are
recorded on the head-state record, and `ActivityConstructionFailed` is sticky (`CoarsePhaseFor`
short-circuits). **This was NOT yet true when this section was first written** — the SPA had no
terminal-fail row state at all and folded `BuildFailed` into a generic `in-construction` look,
which is exactly why the webApp work in Stage 1 (`webApp/src/components/construction/status.tsx`,
`constructionAdapters.ts`) exists: it adds the `failed` `BuildStatus` member, its chip styling, and
the `FAILURE_REASON_LABEL` map, so the operator now sees a distinct red failed node carrying the
reason and detail text, not a lookalike in-progress one.

**Not** a returned workflow error — a failed Temporal execution is invisible in the app and buys
only retry noise. **Not** an interventionEngine escalation — that engine adjudicates variance for
work legitimately in flight; a plan-integrity defect is terminal until a human amends the plan,
and routing it through `DecideOnVariance` would launder a deterministic defect into a retry loop.

### 4.3a A second dispatch-blocking defect, found by the live drain

§8's dry-run drain (todomvc corpus) surfaced a **second, independent** defect behind the same
symptom class as §1: a milestone dependency (`network.dependencies[].dependsOn` naming a
`network.milestones` entry, not an activity) was never satisfied, because the original
`allDepsSatisfied` check only ever looked for a `Done` `ActivityConstruction` record — which a
milestone, being a zero-duration authored event node, never gets. Every activity gated behind a
milestone stalled exactly the way componentId-unresolvable activities did pre-fix: silently, behind
a quiescent verdict with no operator-visible cause.

The fix (`resolveDependencySatisfied` / `allDepsSatisfied`, `constructionmanager.go`) makes
milestone satisfaction **derived**: a milestone is satisfied iff every id in its own `dependsOn` is,
recursively, satisfied (empty `dependsOn` = satisfied). A dependency id naming neither a known
activity nor a known milestone, or a cycle in that recursion, is surfaced through the same loud
`verdictBlocked` path as `ComponentUnresolved` — its own `FailureReason` variant per class
(`DependencyUnresolved`, `DependencyCycle`), never folded into it. This closed under the drain: all
26 activities in the todomvc corpus reached `Done`.

**Advance-past semantics.** On later ticks the Failed activity is no longer `NotStarted`, so the
sweep considers other candidates: independent branches resume, and every dependent of the failed
node stays gated forever (`allDepsDone` requires `Done`; Failed is sticky). Nothing builds on the
hole, the failure is visible, and one bad non-critical branch does not dead-stop unrelated
critical-path work. Total project stop remains available through the existing operator pause
seam; auto-pausing here would over-brake.

## 5. The authoring gate

Three rules in `server/cmd/aiarch-state-mcp/crossartifact.go`, function
`activityCoverageFindings` (:155):

| Rule | Severity | Fires when |
|---|---|---|
| `ACT-UNKNOWN-COMPONENT` (Warning → **Error**) | Error | any activity with `ComponentID != ""` and no committed System component has that exact id |
| `ACT-COMPONENT-COVERAGE` (kept) | Error | a committed System component in a code layer (client / manager / engine / resourceAccess) is claimed by **≥1** coding activity's authored `ComponentID` — the load-bearing rule |
| `ACT-NONSTRUCTURAL-CODING` (new) | **Info** | enumerates coding activities with an empty `ComponentID`: "declared nonstructural (ch.13 Table 13-2 category) — confirm this is intentional". Never blocks |

`deriveActivityComponent` (:212), `isIntegrationActivity` (:251) and the duplicated
`normalizeIdent` are all deleted — the carve-out's only consumer was the coverage skip, which the
authored field obsoletes.

In `staleness.go`, `ACT-UNKNOWN-COMPONENT` and `ACT-COMPONENT-COVERAGE` stay in
`systemActivityListJoinRules` — a stale-basis downgrade during a System-amendment reconciliation
window is correct for join rules. The Info rule needs no policy entry.

### 5.1 What coverage catches, and what it cannot

Coverage is load-bearing because omission is self-punishing: drop `componentId` from `C-TLM` and
`todo-list-manager` has no claimant, so the draft is rejected with the component named. Every one
of the five `C-*` activities in the failing run is exactly this case. A typo'd id is caught by
`ACT-UNKNOWN-COMPONENT`; a claim stolen from a sibling leaves the original unclaimed and coverage
fires.

Two residuals are **not** machine-closable, and the design accepts them rather than pretending
otherwise:

- **The referentially-valid swap.** `C-TLM` claims `todo-record-access` and `C-TRA` claims
  `todo-list-manager`. Every component claimed, every claim resolves, coverage passes, both build
  the wrong thing. Catching this needs name matching strong enough to be the fuzzy resolver we
  just deleted wearing a validator costume. It belongs to the artifact review gate — which means
  the rendered activity table **must display the componentId column** so a reviewer can see it.
- **The family slip.** `U-SPA-AMEND` drops its `componentId` while `U-SPA-CONSULT` keeps it;
  coverage is satisfied by CONSULT and AMEND dispatches componentless. This is what the
  `ACT-NONSTRUCTURAL-CODING` Info rule exists for: the slipped activity appears on an explicit
  "declared nonstructural" list, where *"U-SPA-AMEND: declared nonstructural"* is visibly absurd
  to a reviewer. Promoting it to Error would re-outlaw Table 13-2.

**Why this is the hard reject.** `putDraftModel` (`state.go:36`) Gate 2 runs
`methodcheck.ValidateProjectJSON` + `applyGateSeverityPolicies` (which appends
`appendAppSideCrossArtifactFindings`) and **refuses the write** on any surviving SeverityError
attributed to the ambient slot. A draft that cannot be written cannot be staged, therefore
cannot be auto-approved — the vibes autogate sits *after* this gate, not before it. The drafting
agent receives the error string in-loop and self-corrects. The same finding set fails the CI
`validate` subcommand and `applyConstructionMutation` (`constructverbs.go:78`) by construction.

Rejected alternatives: the projectDesignManager commit path (business rule in an orchestrator,
and by commit time the drafting session is gone — a reject there strands the rail with nobody to
fix the draft); `projectStateAccess` `ContractMisuse` (cross-slot business rule in the storage
layer, and see §3 on `RequireModelFields`); the designHealth engine (right family, wrong
substrate — it reads the tolerant `framework-go-projectmodel` slices, which do not carry
`componentId`, so the rule would be gated on a platform release). Mirroring the rule into
designHealth so it also renders in the webApp Design Health view is an optional follow-up.

## 6. Authoring side

Two copies of the `the-method-activity-list` skill must change, and the platform copy is the
load-bearing one:

1. **`archistrator-platform` → `method-assets`** — mandatory. Benchmark and operated repos never
   read archistrator's local `.claude`; the design and construction jobs materialize their prompt
   surface at job start from the **pinned** module (`seatassets.go` / `methodassets.Materialize`,
   pin = `sourcecontrol.StateMcpModulePin`, plus the hardcoded `@v0.1.7` in
   `.github/workflows/server-checks.yml:145`). Without a method-assets release and pin bump,
   every future benchmark run drafts the field-less shape and bounces off the new gate forever.
2. **`.claude/skills/the-method-activity-list/SKILL.md`** — the self-hosted divergence copy,
   edited in lockstep. The committed drift check covers only `settings.json` and `grillme`, so
   nothing forces this; skipping it drifts the self-hosted rail.

Required instruction content, stated as a normative rule in the typed-model / Step-1 doctrine:

- `componentId` = the **exact `id`** of a component in the committed `.systemDesign` Components —
  not the display name, not an abbreviation.
- It marks a **structural** coding activity (ch.13 Table 13-1 — the one-per-component base-list
  default). **Nonstructural** coding activities legitimately omit it: harnesses, base services,
  and use-case integration slices (ch.13 Table 13-2). Teach the three-category taxonomy explicitly
  — the skill currently has only coding/noncoding.
- Multiple activities may share one `componentId` (ratified phase-split shapes: `U-SPA-*`,
  compression D-splits, the ch.11 client development/integration split). Optional but encouraged
  on noncoding provisioning activities targeting a named Resource component (`R-*`).
- State the enforcement plainly: *the coverage gate is what forces every code-layer component to
  be claimed — `putDraftModel` rejects the draft (`ACT-COMPONENT-COVERAGE` /
  `ACT-UNKNOWN-COMPONENT`) if a component goes unclaimed or a componentId resolves to no committed
  component.*
- Add the column to the Step-1 markdown render table.

`.claude/commands/activity-list-draft.md` needs **no change** — it already defers doctrine to the
skill and validation to `putDraftModel`'s fix-and-resubmit loop. The construction commands'
"`component_id` may be empty for non-component activities" text remains true.

## 7. Staging

Two independently shippable stages. Stage 1 adds no new rules, so CI stays green while the
backfills are still outstanding.

**Stage 1 — unblock construction (reaches the AC).**
Schema field · pump selection rewrite · three-state verdict · `ComponentUnresolved` /
`DependencyUnresolved` / `DependencyCycle` · webApp codec regen · authored backfill of the
benchmark state repo's 8 coding activities · dry-run validation · benchmark resume mode · the real
metrics run.

**Stage 2 — prevent recurrence.**
The two `ACT-*` Error rules and the `ACT-NONSTRUCTURAL-CODING` Info rule · authored backfill of
archistrator's own 68-activity list (structural coding activities only — `I-UC*` and the harnesses
stay as authored, per §2.5) · the `the-method-activity-list` skill change · method-assets release
and pin bumps.

Stage 2's ordering constraint is absolute: **the backfills land with or before the rules.** The
whole-document CI `validate` runs with no ambient slot, so no slot-scoped downgrade applies, and
every legacy corpus goes red the moment the rules ship.

## 8. Reaching the acceptance criteria

> Construction can be completed on the benchmark for a simple todomvc run, and the app can be
> opened showing every construction task complete.

1. **Land Stage 1's code.** Regenerate the codec chain; nothing new is enforced yet.
2. **Backfill the benchmark state repo.** `system-architect` authors `componentId` for the 8
   coding activities in
   `$TMPDIR/archistrator-bench-scratch/run-20260808T000116Z-1744cf81/state-repo`, whose
   `.aiarch/state/project.json` sits at `phase: 2` (construction), `reviewPolicy: vibes`,
   version 41, project id `todomvc-run-20260808T000116Z-1744cf81`, clean on `main`. The mapping
   is unambiguous from the committed systemDesign: `C-TLM`→`todo-list-manager`,
   `C-ACE`→`accounting-engine`, `C-AE`→`admitting-engine`, `C-TRA`→`todo-record-access`,
   `C-TAC`→`todo-agent-client`, `U-SPA-CONSULT`/`U-SPA-AMEND`/`U-SPA-EMBED`→`todo-owner-client`,
   and optionally `R-TRS`→`todo-record-store`. That reading is recorded here as the expected
   outcome, not as a pre-approval: the architect writes the state, and a departure from it is
   the architect's call to make and explain.
3. **Dry-run the whole network.** Serve against that state repo with
   `ARCHISTRATOR_CONSTRUCTION_DRYRUN=true` — every submit succeeds instantly
   (`NewDryRunAgenticJobAccess`, `agenticjobaccess.go:1010`) while the Temporal orchestration,
   per-activity lifecycle and head-state writes stay real. The 26-activity network drains in
   minutes with zero agents. **This is where the next defects live** — nothing past dispatch has
   ever executed. Iterate until every activity reaches `Done` and the app renders them complete.
4. **Add a benchmark resume mode.** `provision.ts` has no reuse hook, but `driveConstruction`
   (`src/runner/drive.ts:531`) is cleanly separable from steps 1–4. A `--resume-from <runId>`
   flag provisions against the existing state repo, reads the project id from `project.json`,
   runs `driveConstruction` + `harvest`, and raises `constructionTicks`.
5. **Run it for real, babysat.**

**Honest cost of step 5.** Design was 24 episodes ≈ 2.5h of agent wall clock. Construction is 26
activities × 3–5 phases ≈ 100+ episodes, and the pump is **strictly sequential** — one activity
at a time, `child.Get()` then ContinueAsNew (`pumpnextactivity.go`). Expect 10–20h wall clock
against a `constructionTicks` budget currently set to ~6h. The 5-hour subscription window is a
hard wall and the executor has no rate-limit-aware retry: `ErrCapacity` maps to
`fwra.QuotaExhausted`, whose `DefaultRetryable()` is false. **Deliberately not designed for
now** — we take the real run, observe exactly how a usage-limit exhaustion surfaces, and fix that
specific failure with evidence. Parallel dispatch was considered and rejected for this iteration:
if the token window is the binding constraint, parallelism reaches the wall sooner without
reducing total tokens.

## 9. Second-order breakage

All of it is in scope for the stage that triggers it.

1. **Legacy corpora fail the Stage 2 rules** — archistrator's own
   `.aiarch/state/project.json` (68 activities, ~44 coding, none with `componentId`) and any
   surviving gtdapp / gtd-qa2 state repos. Backfills first. Note `ACT-COMPONENT-COVERAGE` is
   *already* Error and passes today only via fuzzy derivation; switching it to the authored field
   is what turns our own repo red.
2. **Construction manager tests** (`manager_test.go`) — `TestResolveComponentID` (:1426) deleted;
   the hardened-resolver tests (:1084, :1241–1288) reworked to authored-field semantics; any test
   asserting the whole-sweep `(zero, false)` behavior; `hydrateConstructionActivity` call sites
   (signature grows).
3. **`crossartifact_test.go` / staleness tests** — `deriveActivityComponent` and
   `isIntegrationActivity` tests deleted; new tests for the reworked coverage rule, the promoted
   `ACT-UNKNOWN-COMPONENT`, and the `ACT-NONSTRUCTURAL-CODING` Info rule;
   `systemActivityListJoinRules` membership assertions.
4. **`FailureReason` exhaustive switches** — the linter demands every switch handle
   `ComponentUnresolved`, `DependencyUnresolved`, and `DependencyCycle`; the webApp must render
   the new wire values.
5. **webApp codec regen** — `ModelActivityItem` (`webApp/src/contracts/schema.ts:825`) lacks
   `componentId`; the chain is clientgen → OAS → `schema.ts`/`types.ts`. Skipping the regen
   renders activity screens empty.
6. **Generated-layer drift gates** (`server-checks.yml`) — clientgen, toolcatalog /
   internaltoolsgen, and modelgen outputs regenerated in the same change or CI reds on drift.
7. **Platform releases and pins** (Stage 2) — method-assets release, `StateMcpModulePin` bump,
   the `@v0.1.7` literal in `server-checks.yml:145`; `framework-go-projectmodel` only if the
   designHealth mirror is pursued.
8. **Dead-code lint** — deleting the resolvers orphans `normalizeIdent` in
   `constructionmanager.go` and its twin in `crossartifact.go`; remove both or `unused` fails.
9. **Fixture generators** — `gen-systemtests`, `gen-uitests-fixtures` and the seed-construction
   corpora emit activity lists that must carry the field or Stage 2's gate rejects their output.
10. **Temporal in-flight compatibility** — `constructionActivity` rides child-workflow payloads.
    The additions are additive JSON, but per standing doctrine: **drain in-flight construction
    workflows before deploying** the changed pump selection logic.

## 10. Follow-up workstreams this design uncovered

Both came out of the book reading. Both are ruled **separate workstreams** — neither blocks the
dispatch fix, which is shape-agnostic: it corrects resolution and gating for whatever plan is
committed.

**(a) `N-STH` / `N-RTH` are marked `coding: false`.** Ch.13 Table 13-2 files System Test Harness
(25d, Test Engineer) and Regression Test Harness (10d, Developer) under *nonstructural coding*.
Our skill's inventory and our committed plan both call them noncoding. This is not inert — `Coding`
selects the activity's phase profile and construction routing (App A lifecycle vs the noncoding
profile), so it needs its own ruling on downstream effects plus a skill edit and a state amendment.
It *may* ride the method-assets release Stage 2 already forces, if ruled in time; that is an
efficiency, not a default.

**(b) The `I-UC*` base-list shape.** The skill mandates one integration activity per core use
case in the **base** list. Our network has five, each depending on 9–11 components, all converging
on `N-IT`, with `I-UC3` on the critical path. Against App C §Integration ("Avoid mass integration
points. Avoid integration at the end of the project.") and ch.13's stated reason the compressed
TradeMe was rejected — "The multiple, parallel integrations occurring near the end of the project
offered no leeway" — this is the anti-pattern, and it is plausibly double-counted against the 15%
Integration phase already inside every `C-*` estimate. The architect's own read is that these are
mislabeled **system-test slices** (the book files end-to-end use-case proving under noncoding
System Testing), not integration activities. Reshaping is a base-list doctrine amendment (skill +
method-assets) plus a scope-change re-run of Phase 2 on affected plans.

**Sequencing condition on (b), which matters for what the benchmark is for.** The benchmark today
measures a plan shape the architect no longer endorses. That does not block the fix, but the
doctrine amendment **should land before benchmark baselines are locked or compared across runs** —
otherwise the self-improvement loop is calibrated against a network we intend to repudiate, and
every subsequent delta is polluted by the reshape. Fix first, reshape second, baseline third.

## 11. Out of scope

- Parallel dispatch in the pump (§8).
- Rate-limit-aware retry in the local executor (§8) — revisited with evidence after the real run.
- Mirroring the component rules into the designHealth engine for the Design Health view (§5).
- Any change to the construction slash commands (§6).
- Shrinking the todomvc benchmark network.
