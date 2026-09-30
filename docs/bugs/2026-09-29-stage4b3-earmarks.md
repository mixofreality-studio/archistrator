# Stage 4b3 (the gaps 4b2 left, and the wave that ends in the release) — earmarks and carry-forwards (2026-09-30)

Stage 4b3 is the **fourth** wave of stage 4 and the one the single drain and the one release ride. It carries what 4b2 deferred: the delinquency signal's shape, the signal-wire-form CONSUMER gate, the five discharged version fences, a per-task session view, a late-approve refusal, the zombie probe, the one model edit, the red node a broken merge tail was never drawing, the sweep that re-opens it, and the bound that stops that sweep from healing for ever.

Branch `activity-experience-stage4b3` from `main` @`00314580`. 13 tasks planned; 11 code/measurement commits landed, this docs commit is Task 12 and Task 13 is the drain/merge/tag/deploy. Commits in landing order:

| Task | Commit | What landed |
|---|---|---|
| 1 — the delinquency signal gets one flat shape and an action that can say "nobody said" | `bbbecc26` | the Manager-internal refusal; the cross-package wire pin |
| 2 — supervision reads a pause the way the bus can send one | `6ece6bae` | `pauseSignalReason`, shared by both consumers of `operatorPauseRequested` |
| 3 — the five version fences discharged with their census rows and their `DefaultVersion` tests | `94bdeb9c` | census **36 → 32** |
| 4 — **the signal-wire-form CONSUMER gate** | `12ff897b` | 7 files, all new, zero production code changed |
| 5 — the session state is keyed by task, because a fork holds two gates | `512cfc7d` | shapes 11 → 12; the `construction_gate_wait` mis-tagging found for free |
| 6 — a decision names the round it judged, and a superseded round is refused | `c8127ec8` | shapes 12 → 13 |
| 7 — a child that died writing no row is probed, not waited on for ever | `83622884` | shapes 13 → 14; census **32 → 33** |
| 8 — **THE ONE MODEL EDIT** | `f8bdba99` | 55 files, one indivisible commit; `awaitingTasks`, the per-task gates, `tailFailureDetail`, `DelinquencyAction` |
| 9 — an activity that completed its work and failed to land it gets a name | `dc0361eb` | the **fifth** write-once head fact, and write-once ENFORCED for the first time |
| 10 — the round sweep re-opens an activity that completed and did not land | `4f4655f7` | `sweepReopenNotLanded` |
| 11 — the heal stops when it stops helping; the named split is recorded as forbidden | `48fb184a` | the ping-pong bound; 82 symbols measured, zero moved |
| 12 — the record, the earmarks, the ceiling sentence and the measurement | this commit | docs only |

## Gates at ship — re-measured for this commit, not transcribed

Every row was re-run at `48fb184a` with `GOWORK=off`. Where a plan line or a task report disagreed, the measurement is what stands and the stale claim is named.

| Gate | Value | Note |
|---|---|---|
| Registered Temporal names (golden) | **133** | counted from the `registeredTemporalNamesGolden` literal, `TestRegisteredTemporalNamesGolden` PASS. Unmoved all wave — 4b3 registers no new workflow or activity name. |
| Frozen workflow names | **15** | counted from the `frozen` literal, `…_FrozenWorkflowNames` PASS. Unmoved. |
| `Test_LifecycleShapes` | **14/14** | 14 subtests, all PASS. **The plan's gate ledger said 15/15 and it is WRONG**: Task 9's new case is the standalone `Test_Walk_ABrokenMergeTailLeavesARedNode`, not a shape case. Chain: 11 → 12 (Task 5) → 13 (Task 6) → 14 (Task 7) → 14. |
| `Test_Replay_DeliveryHistories` | **8/8** | unmoved; the child fixtures survived the model edit's regen. |
| `Test_Replay_PumpHistories` | **6/6** | unmoved. **14 replay fixtures in two families**, exactly as 4b2 left them. |
| Pump guard census | **33 rows**, 3 meta-tests PASS | 19 + 7 + 7. 36 → 32 (Task 3 discharged four fence-bound rows) → 33 (Task 7's `G-P23`, the zombie probe — the first row this census has ever GAINED). **Earlier documents saying 36 or 37 are stale by one or two corrections; 33 is the number.** Pins: the machine census names exactly **33** `Test_*` functions, one per row, all distinct; the written twin names **48** distinct `Test_*` across its prose and its `PinnedBy` cells (the 4b2 ledger's "47 pins" was that doc-side figure at 36 rows — it has no code-side definition and should not be quoted as one). |
| Producer gate (`Test_DeliverSignal_TheWireFormProducersAreAClosedList`) | **PASS**, 4 producers | unmoved. |
| **Consumer gate** (`TestSignalWireFormConsumers` + `…_IsRedOnEveryKnownInstance`) | **PASS** — *"checked 12 signal channel(s) (6 bus-delivered) against 5 producer(s); classified 9 receive target(s)"* | NEW at `12ff897b`. **12 / 6 / 5 / 9.** The gate passes over production with **zero findings** and zero sanctioned exceptions. |
| `validate --root .. --slot System` | **43 advisory / 0 errors** | unmoved through 4a, 4b1, 4b2 and 4b3. `DH-CONTRACT-DEADOP` still ×2 (`AcknowledgeStaleBasis`, `RecordOperatorNote`) — both name collisions across `projectStateAccess` facets, not dead ops. |
| `npm run check` (webApp) | **1241 / 1241**, 0 fail | 1237 at the wave's base → 1240 (Task 8) → 1241 (Task 9) → 1241. **The plan's ledger said ≈1243 and it is wrong by two**: Task 9 added ONE (the SPA needed no code change — the red node arrives as `ActivityViewState="failed"`), and Tasks 10 and 11 added none. |
| uitests preview | **57 tests, 57 passed**, over **23 fixture state files** | unmoved. 22 exercised in a browser, plus `deployment-linear.json`, which no preview spec drives (schema-checked only). The 22-vs-23 reconciliation in the 4b2 earmarks still stands verbatim. |
| Hand-written `delivery` lines | **21,507** | see the line measurement below. |

**The changed-path set at merge — SIX trees, and `systemtests/` is again the one a reviewer drops.** `git diff --name-only main...HEAD`: **72 files**.

| path | files | what is in it |
|---|---|---|
| `server/` | 26 | every task's Go, plus the model edit's regenerated output |
| `uitests/` | 20 | the preview fixtures the model edit re-shaped |
| `webApp/` | 14 | the contracts, `constructionSession`, the per-task roster reads |
| **`systemtests/`** | **9** | 2 generated (`sdk/types_delivery.gen.go`, `sdk/types_operations.gen.go`) + `internal/generated/{manifest.json, stp_uc4_table.gen.go}` + **5 HAND-WRITTEN** — `internal/harness/{enums,transport,httptransport,mcptransport}.go` and `usecases/uc4_operations_test.go` |
| `docs/` | 2 (3 with this commit) | the census, the 4b2 earmarks, and this file |
| `.aiarch/` | 1 | `state/project.json`, touched by exactly one commit (`f8bdba99`) |

**`systemtests` is a separate Go module the server's own gates never compile.** `GOWORK=off go build ./... && go vet ./...` must be run in it as well as in `server/`. A merge-safety read that lists only `server/`, `webApp/`, `uitests/` and `docs/` drops a tree holding five hand-written changes, and it dropped one at 4b2 too.

## The line measurement — spec §9's acceptance holds, and the floor rose again

Recipe, verbatim: `wc -l server/internal/manager/delivery/*.go`; **hand-written = total − `manager_test.go` − the four `*.gen.go`** (`activities`, `contract`, `invokers`, `worker`).

| | Total | `manager_test.go` | the four `*.gen.go` | **Hand-written** | Non-test |
|---|---:|---:|---:|---:|---:|
| Three predecessor packages @ `4baed01a` | 61,576 | 31,798 | 4,135 | **25,643** | 29,778 |
| `delivery` @ 4b1 head | 44,665 | 23,464 | 2,218 | **18,983** | 21,201 |
| `delivery` @ 4b2 ship (`d3a4f898`) | 51,560 | 28,675 | 2,171 | **20,714** | 22,885 |
| **`delivery` @ 4b3 head (`48fb184a`)** | **53,648** | **29,981** | **2,160** | **21,507** | **23,667** |

**§9's acceptance HOLDS: 21,507 < 25,643 — 83.9 % of the sum of predecessors, −4,136 lines (−16.1 %).** On the non-test measure, 23,667 < 29,778 (79.5 %).

**And the floor rose again: +793 hand-written lines (20,714 → 21,507), in a wave that budgeted none.** Where it went, by commit rather than by estimate: Task 6 alone was +249 (20,714 → 20,963), Tasks 8-10 carried the rest, and `deliverymanager.go` is **13,535** lines at this HEAD, **UP from 13,234** at the wave's base. **The acceptance is a CEILING, not a ratchet** — it reads green while the number grows, and it has now grown in two consecutive waves. **The floor the next wave measures against is 21,507.** A wave that meets the ceiling while raising the floor should say so in its own words rather than quote the green tick.

Claims below carry the task and the commit that produced them.

---

## Deploy note — 4b3 IS the wave the one drain and the one release ride

4b2's earmark file says "4b2 does not deploy, and not alone". That is still true, and the sentence that replaces it is: **stages 3 + 4a + 4b1 + 4b2 + 4b3 merge together, drain once, release once, at the end of 4b3.** The procedure is `docs/bugs/2026-09-24-stage3-rail-earmarks.md` and it is executable top to bottom.

**What 4b3 adds to that procedure is NOTHING BUT THE CONFIRMATION, and that is the finding.** Measured, not assumed: golden **133** and frozen **15** are unmoved, so 4b3 registers no workflow or activity name and retires none; it creates **no new workflow TYPE, no new workflow id family, no new task queue owner and no new Schedule**; `temporal schedule list` must still show **two**. Task 10's re-open sweep is a second STEP inside `RoundSweepWorkflow`'s existing single-project arm, so `{projectId}:roundSweep:{tickId}` was already in the drain's scope. There is **no state migration**: `tailFailureDetail` is an `omitempty` addition and every pre-existing row reads as absent.

The one thing in the drain that is new and dangerous is unchanged from 4b2: **the main-write lease runs for the first time after this release.** 4b3 adds a second first-run to watch beside it — **the heal bound** (below) has never executed against production data either.

---

## THE WAVE'S OPEN ITEMS — one ordered list

Eleven things leave 4b3 unfinished, ordered by what they cost while they wait.

1. **🟡 THE HEAL BOUND RESTS ON ONE PRODUCTION CLAIM, AND IF THAT CLAIM IS WRONG IT FAILS OPEN AND SILENT.** → §*The ping-pong bound*.
2. **🔴 The late-approve defect is HALF closed, and the open half is the one a human hits.** → §*The late approve*.
3. **`reviewSetError` now has a producer and NO shape case driving a refusing engine.** → §*The three arms with no oracle*, item 1.
4. **`recordTailFailure`'s `finalizeActivity` arm has no direct oracle** — the most likely place for a subtle error in Task 9's commit. → same section, item 2.
5. **`TailFailureDetail` stores a verbatim ~250-character nested Temporal error into the git-as-DB document with NO truncation, because nobody has decided a limit.** → same section, item 3.
6. **The wire contract note in `project.json` still says FOUR write-once head facts. There are FIVE, and as of Task 9 the promise is enforced.** → §*Owed to the next MODEL wave*, item 1.
7. **`OperatorNoteInput` has no author member**, so a query cannot tell a sweep-heal from an operator's re-open. → same section, item 2.
8. **The façade's `pauseNotWithdraw` upstream half is still a bool** — `billingStateAccess.CustomerSummary.PauseNotWithdraw` is what the sweep READS to decide what to send, and Task 8's enum swap does not reach it. → same section, item 3.
9. **The artifact-as-of-revision read is still owed**, now with a measured reason rather than a slip. → §*The revision read*.
10. **The consumer gate has three stated blind spots** and the probe cannot see a child that is alive-and-wedged. → §*What the new gates cannot see*.
11. **`readProject` lives in the wrong file and `TestFileLayout` is narrower than the standard it enforces.** → §*The file-size finding's real owners*.

---

## 🟡 The ping-pong bound — and the one way it can be wrong

**Shipped (Task 11, `48fb184a`): `healWouldRepeatOneThatChangedNothing` (`roundsweep.go`).** A deterministically broken tail heals once and is then left alone. Without it, Task 10's sweep would heal → the pump re-runs the activity → the tail breaks the same way → `RecordActivityOutcome` writes a NEW `CompletedAt` → `reopenNoteID` mints a NEW id the store's dedup never sees → heal, **every 300 s for ever**, appending an `OperatorNote` to a row inside `project.json`, which nothing prunes. Each cycle is legitimate in isolation, which is why nothing stopped it.

The rule: refuse when the row already carries a `NoteRequeue` at the `reopen` gate **and no attempt has RESOLVED since that note**. Net: **at most one heal per unit of measurable progress.** Two other progress measures were rejected at the site — `CompletedAt` because it changes every cycle **by construction** (that is what MAKES the ping-pong), and `TailFailureDetail` harder still, because comparing it would make the bound depend on a Temporal error string staying byte-identical across runs, and its run-id noise would read as "something changed" — **a bound that fails open on its own target case**.

**🟡 THE ONE WAY IT CAN BE WRONG, and it is the first thing to check against a real run.** The bound rests on a claim about PRODUCTION behaviour: **that a re-run of an already-passed walk records no new *resolved* attempt.** It is sound on the code as read — `seedResumeFromLedger` seeds every already-passed task FROM the ledger rather than redoing it, and Task 10 pinned "every design task drafted exactly ONCE across both runs". But if **some** path records a fresh resolved attempt for an already-passed task, **the bound becomes a no-op on exactly the case it exists for, and it fails OPEN and SILENT** — no red test, no log line saying the bound declined to fire, just the ping-pong back. Nothing gates the claim.

**Two more things the bound does not do, each deliberate:**
- **It does not distinguish a platform heal from an operator's.** The reasoning holds for both — a re-open that resolved nothing changed nothing whoever filed it — and the operator's button is never inhibited, so it remains the documented way past the bound. But **an operator who pressed the button once, whose re-run resolved nothing, and who then waits for the platform, waits for ever.** Distinguishing them needs the `authoredBy` field below.
- **It has no visible counter.** An operator sees a node that stays red and a single note; they do NOT see "the platform tried once and stopped". The log line says it and nothing renders it.

**Its test coverage is honest and partial:** the FIRST heal is production-shaped (driven end to end through the sweep); the RE-BREAK is hand-built (`reBreakTheTail` writes the row) rather than driven through a second real walk with `commitFailKinds` still set. Same class of gap the wave's own R11 warned about.

---

## 🔴 The late approve — half closed, and the open half is the one a human hits

**Shipped (Task 6, `c8127ec8`):** `taskDecisionSignal` gains a `Round int` (appended, Manager-internal); `requireOpenRound` returns the round it validated — **the first pending in ledger order, never a recomputed maximum**, and 0 on every error path; `decideTaskGate` refuses `sig.Round != 0 && sig.Round != gate.number`.

**DO NOT READ THIS AS "the late approve is refused".** The stamp is the round the **Manager** resolved at submit time, not the one the **BROWSER** rendered. A redraft between the render and the click still slips through: the Manager resolves n+1, stamps n+1, and the gate agrees with itself. **"The late-approve refusal shipped" ≠ "the late approve is refused".** The half that is closed is the CHILD side — a signal that names a round can no longer be applied to a different one. The half that is open is the only one a human can actually trip.

**And the refusal is a LOG LINE, not a typed error.** Fire-and-forget means the child has no channel back, so to the SPA a refused decision looks like a gate that did not move — better than applying it, worse than saying so, and the same argument the deferred half rests on.

**The deferred half, costed (owner: STAGE 6's model edit, explicitly NOT Task 8's).** `SubmitTaskDecision` grows a `round` parameter — **8 surfaces**:

1. the op's `$defs` in `.aiarch/state/project.json` (`deliveryManager`, via `ReviewDecisionInput`);
2. `server/api/openapi.yaml`;
3. `webApp/src/contracts/schema.ts`;
4. `webApp/src/contracts/ops.gen.ts`;
5. the generated MCP tool schema;
6. the `systemtests` SDK;
7. `webApp/src/.../useDeliveryMutations.ts`;
8. `SubmitBar`, which passes `TaskRevisionView.round`;

plus the server-side refusal: the façade compares the client's round against the ledger's and answers a typed `FailedPrecondition` naming the current revision — **the refusal a human can act on, as against a log line**. **`TaskRevisionView.round` is ALREADY on the wire**, so the client half is one line; the cost is the eight-surface regen, not the logic.

**The zero arm has a live producer and it is not the one the brief predicted.** Dropping it reddens 19 test functions **including 3 of the 8 child replay fixtures** — so `Round: 0` means "a decision recorded BEFORE the field existed", not "the merge hold". `holdForMergeApproval` consumes a merge decision inline and never routes through `decideTaskGate` at all.

---

## The three arms with no oracle

1. **🔴 `reviewSetError` now has a PRODUCER and no shape case driving a refusing engine.** Before Task 8 the member was on the wire, in the MCP tool text and RENDERED BY THE SPA while **every write of it in the package was `""`** — `proposeReviewSet`'s error failed the task instead, throwing away an activity's completed work because nobody could be asked. Task 8 chose "a site sets it" over deletion (deleting breaks preview 57/23 and drops a rendered review aid): `runGate`'s error arm now logs `delivery.gate.reviewSetRefused`, **opens the round with an EMPTY roster**, calls `surfaceReviewSetRefusal` and awaits a human **with no autogate** — which is what the contract's own description always said. What is pinned: the wire half (`constructionSession.test.ts`) and the render (`review-set-error.json`). What is NOT: **no `Test_LifecycleShapes` case drives a genuinely refusing engine through a real walk.** `wf.Review` is an interface, so the arm IS fakeable and the case is writable; the wave's shape budget was spent. **This is the item most likely to be inert in production, and it is the one to test next.**
2. **`recordTailFailure`'s `finalizeActivity` arm has no direct oracle.** Task 9 (`dc0361eb`) routes a broken merge tail to a named failure. Only the `commitDesignArtifacts` break is reachable through an existing double; the `finalizeActivity` arm is exercised transitively and asserted nowhere of its own. **The most likely place for a subtle error in that commit.**
3. **`TailFailureDetail` stores a VERBATIM ~250-character nested Temporal error into `project.json`, with NO truncation.** Not an oversight — **nobody has decided a limit.** Every neighbouring free-text member on a row (`sweepReopenNoteText`, the operator notes) truncates at `maxOperatorNoteRunes`; this one does not, and the string it holds carries run ids, scheduled-event ids and an identity, so it is both long and noisy. Deciding the limit is a one-line change **once somebody decides the number**; until then every not-landed activity adds a quarter-kilobyte of Temporal internals to a git-as-DB document that nothing prunes.

---

## Owed to the next MODEL wave — three deltas, none of which trips a gate

**This wave spent its one model edit on Task 8 (`f8bdba99`).** Each item below is a contract-type change and is therefore recorded, not made.

### 1. 🔴 The wire contract note says FOUR write-once head facts. There are FIVE, and the promise is now ENFORCED.

`.aiarch/state/project.json`'s `activityExecutionAccess` note reads:

> *"…`StartedAt`/`CompletedAt`/`FailureReason`/`FailureDetail` are write-once … `RecordOperatorNote` of kind REQUEUE now re-arms such a row: **it clears exactly those four head facts**…"*

**Both halves of that sentence are now false, in opposite directions.**

- **There are FIVE.** Task 9 added `tailFailureDetail` as a separately-named THIRD head fact — never a re-meaning of `failureDetail` — and Task 8's `reopenTerminalRow` clears it **with the other four** (`projectstateaccess.go:10883`, `:10908-10913`; `assertRequeuedRow` / `assertReArmed` raised from four cleared facts to five).
- **"Are write-once" was a CONTRACT-NOTE CLAIM THAT WAS NOT ENFORCED.** Only `CompletedAt` was guarded; `RecordActivityOutcome` assigned `FailureReason`/`FailureDetail` unconditionally. **Task 9 made the note true**: a failure over a recorded COMPLETION becomes `TailFailureDetail` (itself write-once); a failure over a recorded FAILURE keeps the FIRST cause. "Refused" means the second value does not land while the verb still returns success — deliberate, because every caller is a Temporal workflow whose retry must not be made fatal.

**The edit is to the note's prose, on the `activityExecutionAccess` contract: say five, name `tailFailureDetail`, and drop "are write-once" as a claim in favour of stating the two rules that now enforce it.** Recording it rather than making it is the wave's model-edit budget, not a judgement about its importance.

### 2. 🟡 `OperatorNoteInput` has no author member — the requeue note cannot say who wrote it

`OperatorNoteInput` (`projectstate/contract.gen.go:558-564`) has five members — `noteId`, `kind`, `gate`, `text`, `comments` — and **no author member**. The round half of the sweep records `decidedBy: "platform-sweep"` as a **field** on the round; the re-open half's provenance lives in the note's free **TEXT** (`sweepReopenNoteText`), so an operator reading the list can tell a platform heal from their own and **a query cannot**. Exactly two writers of `NoteRequeue` exist — `reopenActivity` (`deliverymanager.go:6189`) and `sweepReopenNotLanded` (`roundsweep.go`) — and nothing on the row distinguishes them.

The exact delta, on the **SHARED** `OperatorNoteInput` `$def` (shared by `activityExecutionAccess` and the deprecated `constructionTransitionAccess`, so it reaches both facets' tool schemas, `openapi.yaml`, `schema.ts` and the `systemtests` SDK):

```json
"authoredBy": { "type": "string", "description": "Who filed the note: an operator identity, or the platform component that filed it on its own initiative (e.g. \"platform-sweep\", matching the round ledger's decidedBy). Empty on a note written before the member existed." }
```

**What it would buy, concretely:** the heal bound could then treat the two differently — let a human's explicit retry always through while the platform's stays bounded — and platform heals per row would become **countable**, which is the missing counter named above. Neither is answerable today. It does **not** block the bound.

### 3. `billingStateAccess.CustomerSummary.PauseNotWithdraw` is still a bool

Task 8's enum swap replaced `operationsManager.$defs.DelinquencyContext.pauseNotWithdraw:boolean` with `action: {$ref: DelinquencyAction}`, closing the façade hazard (the generated REST handler does a plain `decodeJSON` with NO required-presence validation, so an omitted non-pointer bool decoded as the Go zero — **WITHDRAW**). **The swap does not reach the upstream half.** `billingStateAccess.CustomerSummary.PauseNotWithdraw` is what the shortfall sweep READS to decide what to send: different contract, different op, and it is also baked into `toolcatalog.gen.go:311` as an embedded output schema. Named so nobody reads the façade fix as the whole job.

---

## 🟡 The revision read — still owed, now with a measured reason

*"revision read returns artifact-as-of-`stagedRef`"* (§9's fourth bullet, R1/GAP-5) is **still owed after 4b3**, and this is the first wave that can say WHY rather than that it slipped. Four measurements:

1. **The handle is not ref-shaped.** `stagedRefString` drops `StagedRef.Branch`; `designSubjectRef` was deleted in `98e4a906`; and the live state holds exactly ONE kinded round whose `SubjectRef` is `{artifact, "operationalConcepts"}`, backfilled — **no `@v` ref exists anywhere on this repo**, so the `<branch>@v<version>` shape 4b1 "ratified" has never been produced by anything.
2. **Server-side resolution is preferred over a format change.** `QueryActivityView` taking a revision and returning the artifact as of its `stagedRef` is one op signature; re-shaping the stored handle is a migration of committed state.
3. **The client half already ships and says so honestly.** The read-only banner, Back to latest, that revision's threads and verdicts, no submit bar, and the CURRENT artifact under `Activity.HISTORY_CAPTION` saying it is the current one — not the one this round judged.
4. **It rides STAGE 6's model edit**, with the late-approve `round` parameter, because both are op-signature changes on the same contract.

---

## What the new gates cannot see

### The consumer gate's three blind spots (Task 4, written into the file's own header)

The gate is module-scoped over `server/internal`, non-test, non-generated, and it PASSES over production with zero findings and **zero sanctioned exceptions** — which is stronger than the one exception the plan predicted, because Task 3's fence discharge removed the only live instance. `R4`'s sanctioned-exception SHAPE and the `sanctionedDecoders` allowlist were **amended away and the amendment ratified**: an exception mechanism nothing exercises is an untested escape hatch in the one gate whose failure mode is SILENCE (any struct receive could be quieted by wrapping it in a `GetVersion`).

1. **Codegen emitting a signal receive would be invisible.** Measured at this commit: zero `GetSignalChannel` calls in generated code and zero outside `internal/`, so the exclusions cost nothing today — **and there is no seed for the reachability net to catch a future one**.
2. **A channel read off a receiver whose type is not syntactically declared** — arriving through an interface, a map value, or a type the gate cannot name. The seed-reachability rule is the net: such a channel reaches no sink and is REPORTED, which is how mutation 6 fires.
3. **Methods are resolved by NAME within a package, with no types loaded**, so two same-named methods both take the taint. That over-approximates toward RED and never away from it — but a finding could name a second, innocent site.

Known imprecision, not a defect: `pumpPark`'s three callbacks share the parameter name `c`, so the three pump channels' seeds blur inside that one function. Sink classification is unaffected.

**Five numbers now gate anyone touching a signal channel or a bus producer: 12 channels / 6 bus-delivered / 5 producers / the five producer names / the 7-entry vacuity floor.** Minting a sixth signal name moves three of them.

### The probe cannot see a child that is ALIVE AND WEDGED

Task 7's zombie probe (`83622884`) asks one question — **does the execution exist** — and reaps a child that died writing no row. **It cannot see a child that is alive and stuck**, because there is no per-activity wall clock anywhere in the pump. Pre-existing and unchanged by this wave; it becomes a real gap the day the operator surface wants to say "in flight for six hours". Two more properties worth keeping:

- **The "fully probed within 30 ticks" bound is PER RUN, not per chain.** `probeCursor` is run-local, so every `ContinueAsNew` restarts the rotation at the head. It cannot starve; the figure is simply not a chain guarantee — the same scope error the lease invariant had.
- **The ignore-epoch arm the probe depends on had NEVER been exercised.** It is enforced at exactly one site (`deliveryactivity.go:3498`) and there were **zero `pumpLivenessProbeEpoch` and zero `Epoch: 0`** in the package's tests before Task 7 (every lease test signalled `Epoch 7`). Measured, not argued: **the same envelope delivered at `Epoch 1` is taken as a GRANT after 1 ms of workflow time**, so the epoch is the only thing between a probe and a stolen lease.

---

## 🔴 The file-size finding's real owners — and a gate narrower than its standard

**Quoted from the correction Task 11 wrote into `docs/bugs/2026-09-28-stage4b2-earmarks.md` ("Written by stage 4b3 Task 11"), because it is the record and this file does not re-derive it:**

> **R23's remediation row for the 13,535-line file — "split by op family under the existing file-layout standard" — is FORBIDDEN BY that standard.** Rule 1 admits one contract-implementation file and Rule 4 admits no others; the standard's own text calls large files an accepted consequence and names `projectstateaccess.go` as the precedent. The only sanctioned reduction is Rule 2's re-homing of workflow-exclusive helpers, which 4b3 did and measured. **The real owners of the file-size finding are (a) a platform amendment to the standard — a `framework-go` release, i.e. not this programme — or (b) the facet wave, which removes RA-shaped code from the Manager rather than re-filing it.**

The measurement behind it, restated with its two numbers: **82 of 688** top-level declarations in `deliverymanager.go` are referenced by exactly one other hand-written file and by nothing inside `deliverymanager.go` itself (77 → `deliveryactivity.go`, 5 → `roundsweep.go`) — measured with a `go/ast` walk, not the plan's bare-word regex, which said 107 and was inflated by prose. **Zero moved**, each failing a test that is the standard's own or the code's own; **the cleanest candidate of all refuses itself in writing** (`strandedRounds`: *"the file-layout standard puts it here beside `roundGateKey` rather than in `roundsweep.go`"*). `TestFileLayout` was PASS before and after, which is the stated acceptance.

**And a move between files in ONE package moves no lines out of the package.** `deliverymanager.go` is **13,535** at this HEAD, **UP from 13,234** at the wave's base. Nobody may read the absence of a move as a regression, and nobody may read a move as a reduction.

**Three of R22/R4's supporting figures drifted and are re-measured here, each with its recipe** (the spec's §10 carries the same corrections):

| Claim | R22/R4 said | Measured at `48fb184a` | Recipe |
|---|---|---|---|
| `deliveryManager`'s share of hand-written Manager code | 81.0 % | **81.2 %** | 21,507 / 26,491 (`delivery` + `operations` 3,192 + `billing` 1,792), non-test non-generated |
| "6.7× operations" | 6.7× | **6.7× — but of LINES, not ops** | 21,507 / 3,192 (the next-largest Manager). The op counts are 11 / 8 / 6, a ratio of **1.8×**. The multiplier was right and its label was wrong. |
| direct collaborators | 15 of 35 | **17 of 35** | 14 outbound + 3 inbound (`mcp-client`, `scheduler-client`, `web-client`) over slot 5's 56 relationships |
| `projectStateAccess`'s size | 5 contracts / 47 ops / 10,864 lines | **5 contracts / 47 ops / 10,988 lines** | contracts and ops from the committed slot; lines from `projectstateaccess.go`, which has grown since R4 |

**The two largest `projectStateAccess` facets are at exactly 12/12** (`activityExecutionAccess`, `constructionTransitionAccess`), which is why neither can absorb a verb and why every fold in this programme has had to go somewhere else.

### 🟡 `readProject` is in the wrong file, and the gate does not catch it

`readProject` is declared in **`pumpnextactivity.go:1538`** and called from **`roundsweep.go:200`** and from **five sites in `deliveryactivity.go`** as well as once in its own file — i.e. shared by **three** registered workflows, not two. The standard's Rule 2 says in as many words: *"Code shared by two or more workflows moves up into the contract-implementation file (Rule 1)."* **So its home is `deliverymanager.go`.**

**`TestFileLayout` DOES NOT CATCH IT: the gate checks the FILE SET, not which file a shared helper landed in — so the gate is NARROWER THAN THE STANDARD'S TEXT.** That is the same class of finding as this wave's census lesson (a green meta-test that reads the ID set and never a number). Not fixed here because the fix's direction is **into** the 13.5k file, the opposite of Task 11's. **Same two owners as the size finding** — a `framework-go` amendment could close both in one release.

---

## 🟡 The ~7.5 % fork flake, and what a serial run does not prove

`Test_LifecycleShapes` is **14/14** and every task in this wave that ran it ran it **alone and serially**. **That is not evidence the flake is gone.** The measured class (4b2): it does NOT reproduce serially (0/30 at base) and needs LOAD — **3/40 at 8-way concurrency, ~7.5 %** — hitting `fork-join-service-design-first` as well as `stp-first`. A serial CI run understates the class by construction.

**One data point this wave adds, and it should not be over-read either:** Task 6 saw **one unexplained red in ~14 whole-package runs**, not reproduced, with no assertion captured, against 13 later whole-package runs and 30 clean serial shape runs. It is consistent with the known class and it is not proof of it.

**Nobody may declare this flake gone from a serial run.** Closing it needs a deliberate concurrent re-run at 8-way, and the class is a `readyTasks` ordering question, not a test-authoring one.

---

## Two recon corrections to the record

1. **Signal channels are TWELVE, not thirteen.** The consumer gate asserts 12 and passes at 12. The thirteenth was **prose** at `deliveryactivity.go:3729`, counted by a reader who included a comment. (C5.)
2. **The pump's fence header said "five over four change ids" where it is FIVE CALL SITES OVER SIX IDS.** Fixed in Task 3 (`94bdeb9c`) as part of the discharge. Noted because the header was the document anyone counting fences would have counted from.

---

## The census's head-line citations — the standing decision, recorded rather than re-taken

**`docs/bugs/2026-09-28-pump-guard-census.md`'s ~30 `pumpnextactivity.go` Verdict-column line citations are STALE — by ±7 and worse (G-P16 cites `:1391`; the line is at `:1398`) — and NOTHING gates them. Tasks 3, 7 and 11 each declined to re-measure them by hand, and that declining is now a decision rather than three omissions.** The reason and the two failed gate designs are written into the census itself; the short form is that an in-range check **PASSED on the live failure**, and a gate that passes on the live failure is worse than none. **Do not re-measure them a fourth time without the gate that would keep them true.**

---

## Smaller carries — carried forward unchanged, and three new ones

**Carried verbatim from 4b2, because 4b3 did not close them:**

- **The `ContinueAsNew` boundary is entirely unfixtured and CANNOT be captured**, and both counter-measurements travel with it: (a) **a replay fixture does NOT pin a `GetVersion` fence** — deleting the `changeDesignActivitiesDispatchable` rung left all six pump fixtures GREEN, while deleting the pace `Sleep` gives `[TMPRL1100] a matching Timer command was expected in history event position 34` on 2 of 6, so **fixtures pin COMMANDS, not version rungs**; and (b) **no capture can ever produce a `DefaultVersion` history** (`GetVersion` returns `maxSupported` on a new execution). `pump-drain-pause-before-continue-as-new`'s fence fires only past the 4000-event budget, so the drain, the carry and the replay of `Carried` are uncovered by any fixture — **by the pump's own header, the riskiest ten lines in the file.** A deterministic harness that forces the budget low enough to cross the boundary in a unit environment is what would buy it. **Task 3 discharged the five pump fences, so the `…_DefaultVersion_…` tests that were those rows' only pin are gone with their subjects — the boundary's own fence is what remains.**
- **The lease's exclusivity is per pump CHAIN, not per project**, and **"fails open" is the NORMAL state after any pump restart, not a rare fault path**. Unchanged by 4b3.
- **THE LEASE RUNS FOR THE FIRST TIME AFTER THIS RELEASE.** The one watch-item that matters. Every lease message was dropped on the wire until 4b2's `37b0e768`, so the 2 h grant budget, the liveness probe, the epoch and `pumpGrantLease`'s requester validation were dead code exercised only by tests using a wire form production never produces. **The first real cascade after this release is the first time the admission queue has ever been in the path of a merge.** Three things to measure on it: how long a tail actually holds the lease, whether a busy project serialises behind the 2 h budget, and whether grant → release → re-grant behaves as the dev-server capture showed.
- **The double `ReadActivityExecution` on the approve path** — two git reads on the hottest write. Untouched by Task 6, which added a parameter to the inner resolver and not a read. → the facet wave.
- **The LIVE DOUBLE-WRITE: the child calls both `GitStatusRecordActivityStarted` and `ActivityExecutionOpenActivity` on the same row in the same walk, and nobody has said which wins.** → stage 6 / the facet wave.
- **`deployment-linear.json` is still browser-unexercised** — schema-checked by `webApp/scripts/fixture-schema.test.mjs:266` and driven by no preview spec. Give it a case or delete it.
- **`webApp/scripts/gen-enums.mjs`'s `NON_MECHANICAL.ProjectSessionStage` is still dead configuration** — the enum it explains was removed by 4b2's model edit, so the key can never match a logical output name again. One line to delete.
- **`cmd/server/hooks_test.go:321-323`'s doc comment still names the Schedules as "pump sweep / replan sweep".** The replan sweep is deleted and the second Schedule is the round sweep. The assertions are correct; only the comment lies. (A code fix, so a docs task cannot take it.)
- **`homebase.spec.ts:47`'s `/^phase-card-/` regex is a RAW LITERAL asserting ABSENCE** — it passes and is **silently unfalsifiable**, because nothing ties the literal to a live identifier. Point it at `UI_IDENTIFIERS` or delete it.
- **`shapeSDPActivity = "P-SDP"` is a shape-rig artefact that MIS-CLASSIFIES** — not a production activity id, so it resolves as Documentation → no `sdpReview` task → kind nil, and two façade M0 tests plus the `projectDesign` shape rig exercise a kind resolution production never takes. Rename to `projectDesign`. The measurement that hangs off it: **"`settleThreadsBeforeApprove` has never run at M0" is TRUE of the test rig and FALSE of production.**
- **The reachability pass over the UI identifiers** (referenced ≠ reachable) and the unused-alias hole in `uitests/tests/support/testids.ts`.
- **`construction-round-withdrawn.json`'s out-of-enum addressees** — partly discharged: Task 8's rider found **FIVE** `seniorDeveloper` values (not three; the plan missed `service-fork-sent-back.json`) and made them **`""` not `"pm"`**, because all five are CHANGE REQUESTS and the contract says a change request carries no addressee. **One in-enum-but-wrong case is CARRIED, not fixed**: `architecture-round.json` `r2c3` carries `architect` on a change request — green because it is in the enum, and outside what the contract says.
- **`AskQuestions` validates non-empty but not the vocabulary** (`deliverymanager.go:10625`) — the server half of the same item.
- **The `temporal` CLI is a HARD DEPENDENCY of both capture tools, stated only in a `t.Fatalf`**, in no README and no Makefile target. Version here: 1.7.0 (Server 1.31.0).
- **`golangci-lint cache clean` must run FIRST in any worktree** — a stale cache printed 30 phantom issues pointing into a DELETED sibling worktree — and **`golangci-lint` needs `GOWORK=off` even from `server/`**.

**New from this wave:**

- **🟡 `operationalConcepts` is the ONE orphan artifact kind left, and the founder has DECLINED a drafting step for it (R17).** That is a ruling and it stands; **the cost is recorded here as a deliberate choice, not an oversight.** With no lifecycle naming the kind, **`ReviewRound.artifactKind` stays DEFENSIVE rather than load-bearing** — it is a member every round may carry and no rule consumes — and the one kinded round this repo actually holds (`architecture:architectureReview:operationalConcepts:2`) judges a kind no lifecycle names. **The honest category is NINE, not one:** `operationalConcepts` plus the **eight surviving Phase-2 draft slugs** (`planning-assumptions`, `activity-list`, `network`, `normal-solution`, `subcritical-solution`, `compressed-solution`, `decompressed-solution`, `risk-model`), which are dead-but-green for the same structural reason — **they are dispatched BY NAME FROM OUTSIDE GO, so the Go call graph cannot settle their deadness.** All nine are **a method-assets release**, not a server change.
- **`Test_DeliveryActivityOptions_EveryInvokedActivityIsTuned` earned its keep two waves after it was written.** Task 10's sweep gave `activityExecutionAccess.recordOperatorNote` its FIRST WORKFLOW caller (every prior writer is the Manager, which does not consult the options hook), so it would silently have inherited the generated 15 s default. The gate went red and the preset was added beside its six execution-ledger siblings. **Keep this gate.**
- **🔴 A SUM-TYPE LINTER COVERS `switch`, NOT `==`.** Task 9 found two phase comparisons `gochecksumtype` could not name because they test with `if` rather than `switch`, **and BOTH WERE WRONG**: `OpenActivity`'s resurrection refusal (a birth would have RE-OPENED AN EXITED ROW IN PLACE) and the Manager's `reopenActivity` precheck, which **refused the one state Task 10's heal exists for**. Both fixed; all six phase-comparison sites audited. The blind spot is structural and remains.
- **The live-gate derivation defect Task 8 found and fixed, and what it means for the first real run.** The derivation compared the gate TASK id the child writes against a lifecycle PHASE id, so `evidenceState`'s rule 1 and the `session.awaitingGate` pending attempt **had never fired in production** — invisible because every test fixture supplied a phase. Leaving it would have shipped Tasks 5/6/7's per-task join INERT. Every existing expectation is unchanged, but **the first real run after this release will show task states the previous image could not produce. That is the fix working, not a regression.**
- **The `PREVIEW_FIXTURE_CONTRACTS_WRITE` tool regenerates ONLY `ServiceContracts`.** Task 8's view-shape fixture migration was a one-off script and is **NOT reproducible from the tree**. If the fixtures are re-captured from a live server the new shape comes free; if they are hand-edited again, the OAS schema clause is the net.
- **`awaitingTasks` lists only tasks AWAITING a human, so `redraftExhausted` is unavailable for a task not at a gate**, and `ActivityView` no longer carries an activity-level roster at all — `owedWork.ts` reads `awaitingTasks[0]`, which on a fork shows one of two. Both match the old derived scalars and both now look like choices rather than accidents.

**Added by the fix round (2026-09-30), after the Tasks 5–8 and 9–12 reviews:**

- **🟡 THE HEAL BOUND IS "ONE AUTOMATIC HEAL PER ROW, EVER" — stronger than Task 11 claimed, and the stated rationale was WRONG twice.** `healWouldRepeatOneThatChangedNothing` keys on a *resolved attempt recorded after the newest requeue note*. **Three writers DO record one on a re-run**, with fresh ids: `recordTaskAttempt` (`deliveryactivity.go:3309`), `resolveWorkAttempt` (`:5027`), `passGateAttempt` (`:5150`) — the fix round's own first correction claimed otherwise and was over-broad. **The property holds by REACHABILITY:** in the state this sweep heals, every task already passed and only the main-writing tail broke; `seedWalkFromLedger` (`:3189`) seeds every such task as passed and `finalizeWalk`'s precondition (`:3377`) requires all of them, so the re-run runs **no task** and all three writers are unreachable. What is left is the tail — a merge plus N slot commits — which records **zero attempts by construction**. So the predicate returns true on the second sweep **whether or not the re-run made progress**. Concretely: a `requirements` activity holding four slots; run 2 commits `mission` and `glossary` and fails on `volatilities` — real progress, invisible, and the sweep never heals that row again. **It fails CLOSED** (the red node and the operator's button both survive, and the operator is never inhibited), so the fix round corrected the statements — `task-11-report.md` §1.2 and the comment at `roundsweep.go` — and **left the behaviour alone**.
  - **The meta-lesson, which cost two rounds:** the first correction replaced a wrong reason with a wider wrong reason, because "nothing writes X" is a claim about the whole call graph and "X is unreachable in state S" is a claim about one state. Prefer the narrow one; it is the one that can be checked.
  - **The measure that would actually work: `uncommittedSlotsOf` SHRINKING between ticks.** Not built, because Task 11 rejected the slot set as the TRIGGER's key ("the honest MID-FLIGHT shape of a design activity") and re-opening that choice for the BOUND is a design decision, not a fix-round edit. Whoever takes it must decide the trigger question and the bound question together.
  - **`Test_RoundSweep_ATailThatResolvedATaskSinceTheHealIsHealedAgain` pins the progress arm with a HAND-BUILT resolved attempt the production path cannot produce in that state.** It is a true statement about the predicate and not about any reachable run. Do not read it as coverage of the progress case.
- **🟡 A STALE REVIEW DECISION IS REFUSED AND THE REVIEWER IS NEVER TOLD — the open half of Task 6.** `deliveryactivity.go` Warns and returns `walkTaskFailed, false, nil`, so the gate keeps awaiting and the reviewer sees only that their submission did not land: no note, no view field, no notification. The refusal itself is correct and pinned; the **operator-facing half is open** and needs a surfaced fact (an operator note on the row, or a view member the gate card can render) before a human meets this path in anger. The task-6 report's Concerns 1 and 2 name both halves; the code comment that claimed the reviewer "is told" was corrected by the fix round.
- **🟡 TWO PRE-EXISTING COMMENTS CITE TESTS THAT DO NOT EXIST**, found by the sweep a review ordered after the fix round's own citation (`Test_LiveApprovalGates_TheMergeHoldIsNotAPhaseGate`, a name one word off the real one) turned out dangling. Neither name resolves anywhere in `server/`, `systemtests/` or `uitests/`, under that spelling or any near one: `Test_CoAuthor_ResolveComment_IsMirroredOntoTheRound` (`deliverymanager.go:9329`) and `Test_ConstructionCommands_MatchTheLifecycleData` (`deliverymanager.go:11890`, `deliveryactivity.go:1060`). **Left as found** — repairing them needs to know which test was meant, and guessing would replace a visibly dangling citation with an invisibly wrong one. **A cited test name is an assertion that nothing checks**, and three instances in one file is enough to want a gate: a grep over `// …Test[A-Za-z_]+` resolving against `func \1(` is a few lines and would have caught all three.
- **🟡 `liveApprovalGates`' merge-hold exclusion never worked, and three places said it did.** The filter was `if g.Gate != g.TaskID { continue }`, but `mergeGateTaskID == mergeGateKey == "merge"`, so Gate and TaskID are EQUAL for the merge hold and the shape filter never skipped it — an independent probe returned `{designReview, merge}`. Inert today only because no lifecycle task is named `merge`; name one and `evidenceState` rule 1 fires for the merge hold. The fix round excluded the two holds **by name** and pinned it. **The general lesson is the earmark:** a filter that keys on two fields being DIFFERENT is silently dead wherever they are equal, and nothing in the type system or the linters can see it.

---

## Discharged by 4b3 — what leaves the 4b2 list

Recorded in full, by name and commit, in `docs/bugs/2026-09-28-stage4b2-earmarks.md`'s own "Discharged by stage 4b3" section. In summary: the CONSUMER gate (Task 4), the delinquency founder decision (Task 1 — **and the correction of the record: it was reported LIVE twice and it is LATENT**), the no-failure-row gap (Task 9), the `Started`-forever zombie (Task 7), `PumpResult.activityIds` and `ReplanSweepResult` (Task 8), the `ActiveRole`/`ActiveStep` cascade (Task 8), the per-task session view (Tasks 5 + 8), the late approve's child-side half (Task 6, with the other half re-earmarked above), the re-open sweep (Task 10) and the five version fences (Task 3).

**Four items are DROPPED by ruling rather than done, and each is marked DROPPED with its reason** — never silently absent: the batched plan read (its stated argument was measured FALSE), the pushed job-completion signal, the census citation gate (both candidate designs fail, and the killer is measured), and the reachability pass as framed.

---

## Founder decisions owed

1. **GAP-4B-4's orphan kinds** — now measured as **nine** (see above), and the founder has already ruled on `operationalConcepts`. What is left is the method-assets release that drops the eight command files.
2. **`RevenueShareNone`** as a vocabulary member — a billing-vocabulary question since 4b2 took revenue share out of the model.
3. **The assumed-planning-assumptions UX** — an operating-model screen, and a way to tell "assumed" from "accepted" once the founder has read them.
4. **The construction answer command** — a construction question is recorded and rendered and nothing can answer it.
5. **A limit for `TailFailureDetail`** — new this wave, and the cheapest decision on this list.

**CLOSED by measurement rather than by ruling: the delinquency defect is LATENT, not live.** The founder was told twice that nothing is paused or withdrawn for delinquency **today** and that it costs money every day it waits. Neither claim is true. `billingStateAccess` is the **arm-less, not-implemented stub in every profile** (`NewBillingStateAccess()` returns `stubBillingStateAccess`; `FinalizeBillingStateAccess` is identity), so `ReadPersistentlyDelinquentCustomers` answers `not implemented` and the hourly sweep no-ops **before it ever sends**. Nothing was being dropped because nothing was being sent. The design question is still real and Task 1 answered it — but it was never costing money, and the record is corrected in the 4b2 file where the claim was written.

---

## The lesson this wave is owed by name

**The measurement that contradicts the plan is the deliverable, and this wave produced five.** The census was 33 and two documents said 36 or 37. The shapes end at 14/14 where the gate ledger said 15/15, and npm at 1241 where it said 1243. The GAP-7 argument — that the pump's per-wake-up read is the strongest case for the batched plan read — is **false**: the pump reads through `designSessionAccess.ReadProjectOnBranch` **inside a workflow** and touches `QueryProjectView` **zero times**, so an eighth `ProjectViewKind` buys it nothing. The delinquency defect was reported live and is latent. And the file that R23 said to split **grew 301 lines** while a task measured 82 candidate symbols and correctly moved none.

**None of those was found by a gate. Every one was found by somebody re-running the number instead of transcribing it** — which is the discipline 4b2's Task 17 adopted after the 37-row miss, and it has now paid five times in one wave.

---

## The release — Task 13 Step 11, recorded

**Cut 2026-09-30. `main` is at `0ff0ca33`; the wave's last engineering commit is `ed22471e`, merged forward as `d637e73d`.**

| | |
|---|---|
| Server tag | **`archistrator-server-v0.8.112`** (`server/VERSION` 0.8.112) |
| webApp tag | **`archistrator-webapp-v0.6.94`** (`webApp/package.json` 0.6.94) |
| Images | `ghcr.io/mixofreality-studio/archistrator-server:0.8.112`, `ghcr.io/mixofreality-studio/archistrator-webapp:0.6.94` |
| Release run | `release.yml` **36699164010**, success in 1m53s |
| Merge shape | `origin/main` merged INTO the branch (bringing the 0.8.111 / 0.6.93 bump-backs), then `main` fast-forwarded — the same shape stage 4b2 used |

**The plan predicted 0.8.111 / 0.6.93 and was one release behind**, because the 4b2 merge's own release run had already consumed those numbers. `release.yml` resolves `max(last-tag-patch + 1, source-file-version)`, so the arithmetic self-corrected; nothing was hand-tagged, which matters because the next release reads the last tag.

### What the drain actually did, step by step

**Steps 3 and 5 were NOT run from the release session, and this is the honest record of why.** That session had **no cluster and no production Temporal namespace**: `~/.kube/` holds no config file at all (`kubectl config view` → `cannot locate context`), and the only reachable Temporal is a local dev server on `127.0.0.1:7233` in the `default` namespace. A local server proves nothing about Schedules in either direction — `dryRunConstructionScheduleGate` skips every `delivery`-queue `RegisterSchedule` under `CONSTRUCTION_DRYRUN=true`, so no Schedule is created to inspect.

There is also **no chart in this repository** — no `Chart.yaml`, no `values*.yaml`, no pinned image tag — so rolling the images is outside this repo by construction.

**Therefore, outstanding and owed to whoever has the namespace, IN THIS ORDER:**

1. **Step 3 — pause, then drain.** Optional cleanup under the founder's no-users ruling *except* where noted below. Sweep the `delivery:` prefix **and** `*:nextActivity:*` **and** `{customerId}:delinquency` (see step 2's fifth bullet — that one is on the `operations` queue, which is why a `delivery:`-prefix sweep never reached it).
2. **Step 5 — the three `temporal schedule delete` calls, BEFORE the new image runs.** This is the one step the no-users ruling does not excuse: `RegisterSchedule` **adopts** a same-id Schedule rather than replacing it (`messagebus.go:167-217`), so an unregistered Schedule is not a deleted one and keeps firing into a workflow type no worker serves. Delete `construction:pumpSweep`, `construction:replanSweep`, `delivery:replanSweep`.
3. **The delinquency route must stay CLOSED across the whole rollout.** Not merely drained. The destructive direction is a rolling window: an **old** worker receiving the **new** payload decodes `Context` absent → zero → `PauseNotWithdraw == false` → **withdraw**, irreversible, per app. Nothing in code enforces this; a `GetVersion` fence on that decode would have made it a non-issue and was out of scope.
4. **Step 8 — deploy, then confirm by ID and never by count** (the corrected step above), then unpause with `SetProjectRunState` `runState: "running"`.
5. **Step 9 — watch the first cascade deliberately.** The main-write lease has **never run in production**: every lease message was dropped on the wire until `37b0e768`, so the 2 h grant budget, the liveness probe, the epoch and `pumpGrantLease`'s requester validation have only ever been exercised by tests using a wire form production never produced. Measure the three things Step 9 names, plus this wave's own two firsts — Task 7's liveness probe (a `deliverSignal` at epoch 0 no child has ever received in production) and Task 10's re-open sweep (which writes a requeue note on a 300 s schedule). **A probe storm or an unexpected re-open is a rollback trigger, not a curiosity.**
6. **Step 10 — rollback order is not the obvious one:** pause → restore `project.json` to its pre-migration commit → roll the image → unpause. Rolling the image alone re-dispatches every activity as `NotStarted`.

### One operational consequence of the fix itself, expected and not a regression

The live-gate derivation was comparing a gate **task** id against a lifecycle **phase** id, so two rules had never fired in production. **The first run on this image will show `awaitingHuman` task states the previous image could not produce.** That is the fix working.

---

## The cutover actually ran — 2026-09-30. What it proved, and the one thing it found

**Deployed.** `ghcr.io/mixofreality-studio/archistrator-server:0.8.114` + `archistrator-webapp:0.6.96`, pinned in `davidmarne/aiarchmultiplatform` @ `d0acf14a` (`k8s/argocd/applications/archistrator-{server,webapp}.yaml`) and synced by ArgoCD. Production had been on **0.8.88 / 0.6.66 since 2026-08-11** — stages 3, 4a, 4b1, 4b2 and 4b3 all undeployed at once.

**Production was already broken when we arrived, and the cause is worth keeping.** The state repo IS `mixofreality-studio/archistrator`, so stage 3's `.activityConstruction` → `.activityExecution` rename landed in production's data the moment stage 3 merged — while production ran an image with no reader for it. 0.8.88 therefore saw **every activity as `NotStarted`** and re-dispatched; `archistrator:requirements` was wedged retrying `gitActivityStatusAccess.recordActivityStarted` at attempt 14 with `MaximumAttempts 0`. **The rollback note's hazard had been live in the forward direction for days, and nobody was watching.** Deploying was the fix, not the risk.

### The drain, as run
1. `operatorPaused: true` committed to the state repo, so the new image came up paused.
2. **Five stranded executions terminated** — `constructionConstructActivity`, `constructionPumpNextActivity`, `systemDesignPhase`, two `systemDesignCoAuthor` (two had already self-completed). Every one a type 4b1 retired.
3. **`construction:pumpSweep` and `construction:replanSweep` deleted by id.** `delivery:replanSweep` did not exist to delete — 4a never reached production, which is why the plan's "three deletions" was two in practice.
4. Tags bumped, ArgoCD synced, both rollouts completed.

### Step 8, checked by id — and the corrected step was right
`delivery:pumpSweep` (30 s) and `delivery:roundSweep` (300 s) **present**; `construction:*` and `delivery:replanSweep` **absent**; `operations:operatedStateReconcile` and `shortfallSweep` **untouched**. **Four schedules on a healthy deploy** — the old "expect exactly TWO, three is wrong" would have read this correct state as broken, one step after deleting Schedules by id.

**While paused, the round sweep ran and healed nothing** — Task 10's paused-project guard, working in production on its first outing, and the implementer extended that scope on its own authority.

### 🔴 THE FINDING: the state-repo clone cannot carry the parallel pump

On unpause the pump dispatched **three `deliveryActivity` children at once** — 4b2's parallel pump doing exactly what it was built to do, in production, for the first time. All three then wedged on the same activity:

```
activityExecutionAccess.openActivity — attempt 9, MaximumAttempts 0
resourceaccess: github.GitStore.clone: Post
  https://github.com/mixofreality-studio/archistrator.git/git-upload-pack:
  context deadline exceeded
```

A **full clone of the state repo per activity, three concurrently**, over the activity's StartToClose budget. This is precisely the earmark the 2026-08-11 OOM fix left open: `listProjects` went depth-1, **`GitBlobStore` did not**, and the repo-growth driver was never addressed. The cost was always there; **the parallel pump is simply the first caller to ask for three clones at once**, so a latent per-clone cost became a wedge the moment concurrency arrived.

Retries are unbounded and each one re-clones, so waiting compounds it. **Re-paused and the four executions terminated.** Not a rollback: the image is correct, the state is correct, and the containment is the pause.

**This is a serialisation-vs-clone-cost question, not a bug in the cutover.** The candidate fixes, cheapest first: a shallow/partial clone in `GitStore.clone` (what `listProjects` already got); a per-pod clone cache reused across activities; a StartToClose budget that matches a real clone of this repo; or admitting fewer children concurrently. The first is almost certainly right, and it is the same fix the OOM wave deferred.

**Production is stable, on the new image, paused, with no in-flight work.** Unpausing again without a clone fix reproduces the wedge.
