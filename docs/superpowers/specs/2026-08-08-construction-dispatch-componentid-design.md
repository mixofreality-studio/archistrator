# Design: authored `componentId` on the activity list, and a construction pump that fails loudly

**Date:** 2026-08-08 · **Status:** approved (founder), architecture ruled (system-architect)
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

Founder-ratified, with one correction from the architecture ruling (§2.5).

### 2.1 Component identity is authored, never derived

A component identity is an **authored field on the Phase-2 activity list**, not something the
runtime recovers by string matching. No fuzzy-match fallback ships in product code. The
existing benchmark state repo that predates the field gets a one-time authored backfill.

### 2.2 It is required for component code, optional elsewhere

Required when the activity is coding work against a single named component. Not required for
testing, ui-design, documentation, infrastructure, or integration activities.

### 2.3 A missing or bogus value is rejected at authoring time

The activity list draft is refused, with a message actionable enough that the drafting agent
fixes it in-loop, rather than the defect surfacing hours later as a stalled pump.

### 2.4 An undispatchable activity fails loudly

Visible to the operator in the app. Not a warning in a server log — that is precisely how this
defect consumed an entire benchmark run undetected.

### 2.5 Correction: integration activities are coding but component-less

The rule as first stated — `Coding == true ⇒ componentId required` — rejects archistrator's own
ratified plan. `I-UC1..I-UC5` carry `coding: true` (integration is coding effort per the book)
but span components by definition. The predicate is therefore:

```
requiresComponent(item) := item.Coding && !isIntegrationActivity(item)
```

`isIntegrationActivity` is the deterministic classification already used in
`cmd/aiarch-state-mcp/crossartifact.go` (`I-*` id family / `integrate-*` name / "integration"
in the title). This is prefix classification, not fuzzy resolution — the same mechanism
`DeriveType` and `CommandFor` already depend on.

## 3. Schema

One field on `projectstate.ActivityItem`
(`server/internal/resourceaccess/projectstate/projectstateaccess.go:3734`):

```go
// ComponentID names the committed System component (systemDesign Components[].id)
// this activity builds. AUTHORED at Phase-2 draft time — never derived by matching.
// REQUIRED (non-empty, resolving) when Coding==true and the activity is not an
// integration activity; OPTIONAL on noncoding activities (a provisioning activity
// like R-TRS may name the Resource component it provisions).
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
`U-SPA-AMEND` and `U-SPA-EMBED` all build `todo-owner-client`, which is the ratified phase-split
of one client component. `ACT-COMPONENT-COVERAGE` therefore stays a **≥1** coverage check, never
`== 1`. The skill's "one coding activity per component" is the base-list default, not a gate
invariant. A noncoding activity *may* carry a `componentId` (`R-TRS` → `todo-record-store`); when
present it must resolve.

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
gate-protected storage: a coding non-integration activity with an empty `ComponentID`, or any
non-empty `ComponentID` naming no component in the committed `.systemDesign`.

### 4.3 Loud failure

`PumpNextActivityWorkflow` (`pumpnextactivity.go:38`) on `verdictBlocked`:

1. Calls `ConstructionTransitionRecordActivityFailed`
   (`invokers.gen.go:241`) with an empty `railCredEnvelope` — the pause path in
   `projectsupervision.go` establishes that the local store ignores an empty cred and the git
   adapter mints just-in-time.
2. Reason: a **new closed-enum value** `FailureReasonPlanUnresolvable`, wire name
   `"planUnresolvable"`, added to the six existing values
   (`projectstateaccess.go:6391`, `String()` at :6412).
3. Detail: names the activity, the defect, and the repair — e.g. *"coding activity C-TLM
   carries no componentId; amend the committed activityList to name the systemDesign component
   it builds"*.
4. Logs at ERROR, sets `dispatch = {Decided:true, Dispatched:false}`, and returns **without**
   ContinueAsNew.

This is durable and app-visible: `ActivityConstructionStatus.FailureReason` / `FailureDetail`
already render in the construction console, and `ActivityConstructionFailed` is sticky
(`CoarsePhaseFor` short-circuits). The operator sees a red failed node.

**Not** a returned workflow error — a failed Temporal execution is invisible in the app and buys
only retry noise. **Not** an interventionEngine escalation — that engine adjudicates variance for
work legitimately in flight; a plan-integrity defect is terminal until a human amends the plan,
and routing it through `DecideOnVariance` would launder a deterministic defect into a retry loop.

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
| `ACT-COMPONENT-REQUIRED` (new) | Error | `item.Coding && !isIntegrationActivity(item) && item.ComponentID == ""` |
| `ACT-UNKNOWN-COMPONENT` (Warning → **Error**) | Error | `item.ComponentID != ""` and no committed System component has that exact id |
| `ACT-COMPONENT-COVERAGE` (kept) | Error | unchanged semantics, computed over the authored field |

`deriveActivityComponent` (:212) and the duplicated `normalizeIdent` in that file are deleted.

In `staleness.go`, `ACT-UNKNOWN-COMPONENT` and `ACT-COMPONENT-COVERAGE` stay in
`systemActivityListJoinRules` — a stale-basis downgrade during a System-amendment reconciliation
window is correct for join rules. `ACT-COMPONENT-REQUIRED` must **not** be in that map: it is
single-slot and is never legitimately stale.

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

- Every activity entry carries `componentId` = the **exact `id`** of a component in the committed
  `.systemDesign` Components — not the display name, not an abbreviation.
- REQUIRED on every `coding: true` activity except integration (`I-*`). Multiple activities may
  share one `componentId` only for ratified phase-split shapes (`U-SPA-*`, compression D-splits).
  OPTIONAL but encouraged on noncoding provisioning activities targeting a named Resource
  component (`R-*`).
- State the enforcement plainly: *putDraftModel rejects the draft
  (`ACT-COMPONENT-REQUIRED` / `ACT-UNKNOWN-COMPONENT`) if a required componentId is missing or
  resolves to no committed component.*
- Add the column to the Step-1 markdown render table.

`.claude/commands/activity-list-draft.md` needs **no change** — it already defers doctrine to the
skill and validation to `putDraftModel`'s fix-and-resubmit loop. The construction commands'
"`component_id` may be empty for non-component activities" text remains true.

## 7. Staging

Two independently shippable stages. Stage 1 adds no new rules, so CI stays green while the
backfills are still outstanding.

**Stage 1 — unblock construction (reaches the AC).**
Schema field · pump selection rewrite · three-state verdict · `FailureReasonPlanUnresolvable` ·
webApp codec regen · authored backfill of the benchmark state repo's 8 coding activities ·
dry-run validation · benchmark resume mode · the real metrics run.

**Stage 2 — prevent recurrence.**
The three `ACT-*` Error rules · authored backfill of archistrator's own 68-activity list
(~44 coding) · the `the-method-activity-list` skill change · method-assets release and pin bumps.

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
3. **`crossartifact_test.go` / staleness tests** — `deriveActivityComponent` tests deleted, new
   rule tests, `systemActivityListJoinRules` membership assertions.
4. **`FailureReason` exhaustive switches** — the linter demands every switch handle
   `PlanUnresolvable`; the webApp must render the new wire value.
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

## 10. Out of scope

- Parallel dispatch in the pump (§8).
- Rate-limit-aware retry in the local executor (§8) — revisited with evidence after the real run.
- Mirroring the component rules into the designHealth engine for the Design Health view (§5).
- Any change to the construction slash commands (§6).
- Shrinking the todomvc benchmark network.
