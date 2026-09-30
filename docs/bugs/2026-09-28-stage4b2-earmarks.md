# Stage 4b2 (the pump, the folds, the reads) — earmarks, carry-forwards, and one live defect outside this wave (2026-09-29)

Stage 4b2 shipped §5.1 of `docs/superpowers/specs/2026-09-20-unified-activity-experience-design.md`: **the react-by-signal pump** — `child.Get`'s blocking wait is gone, the pump parks on a selector, dispatches the whole eligible frontier in one pass and **grants a main-write lease** instead of serialising merges by blocking; plus the model edit that took revenue share out of the vocabulary and retired two kinds' draftability, one derived session view where two stood, the replan sweep's deletion, the guard census's verdicts, and the pump's first replay fixtures.

Branch `activity-experience-stage4b2` from `main` @`c5851e90`. 16 tasks planned, Task 8 dropped by the founder and Task 13 dropped as BLOCKED (its premise was false — §6). Code commits in landing order:

| Task | Commits |
|---|---|
| 1 — the pump guard census (the brief predicted 14 guards and 2 unarmed; the census landed 37 rows, later 36, with **15 armed by nothing**) | `78e1b458` |
| 2 — the steer precheck asks the ledger | `3f1746b3` |
| 3 — design questions file on main; the task gate stops asking which branch was entered last | `6ba40dd7` |
| 4 — the sweep's phase filter, G-S7, the meta-test re-key, the thread settlement | `a23b12f5`, `e4015c76`, `eb7177dd`, `4ee5c270`, `12c7655c` |
| 5 — one derived session view | `62e7106a` |
| 6 — the branch reconcile preserves a SET of slots | `635a7e7e` |
| 7 — THE ONE MODEL EDIT (revenue share out; two kinds undraftable; the session `$defs` fold; 12 ops → 11) | `ebfc1a42` |
| 9 — the Activity Experience mounts the three cross-slot providers; the MCP design gate is re-pointed at live state | `a574fac1` |
| 10 — a construction question says a person will answer it, and who | `eb3a9f78` |
| 11A — a joined gate attempt brings its sentence with it | `61df8899`, corrected `f549dede` |
| 11B — the replan sweep is deleted; the pump sweep is frozen | `f3673fb1` |
| 11C — an id nothing can select is an id nobody should read (153 orphan UI identifiers) | `05254eb5` |
| 12 — THE PUMP: the lease replaces the blocking child wait | `b84098eb`, fix round `aa977ae9` |
| 14 — the pump's replay fixtures, and the lease wire-form fix | `37b0e768` |
| 16 — the census's verdicts and its checked numbers | `cb244838` |
| 15 — the docs: the drain stops contradicting itself, and the spec says what shipped | `a883bcb1` |
| review fix C1 — a vanished execution is not a finished activity | `d3a4f898` |
| 17 — the measurement, the ledger reconcile, and the drain note rewritten as ONE procedure | this commit |

## Gates at ship — re-measured at `d3a4f898`, not transcribed

Every row below was re-run for Task 17 rather than carried from a report. Where an earlier document disagreed, the measurement is what stands and the stale claim is named in the last column.

| Gate | Value | Note |
|---|---|---|
| Registered Temporal names (golden) | **133** | The plan predicted **127**. It ends at 133 because Task 13 was BLOCKED and contributed nothing, and 133 = 134 − `ReplanSweepWorkflow` (`f3673fb1`) is the whole of 4b2's movement. Chain, measured from the golden literal across revisions: 231 (`4baed01a`) → 139 (4a) → 141 → **134** (`98e4a906`) → **133**. |
| Frozen workflow names | **15** | 22 → 15 at `98e4a906`; 15 → 15 at `f3673fb1` (`constructionReplanSweep` out, `constructionPumpSweep` in). |
| `Test_LifecycleShapes` | **11/11** | 11 subtests, all pass. |
| `Test_Replay_DeliveryHistories` | **8/8** | the pre-4b2 child histories, untouched by 4b2. |
| `Test_Replay_PumpHistories` | **6/6** | new at `37b0e768`. **14 replay fixtures in two families.** |
| Pump guard census | **36 rows**, 47/47 live pins, 3 meta-tests green | 22 + 7 + 7 = 36, and `Test_PumpGuardCensus_TheHeadlineCountsAreTrue` now fails if the doc lies about it. **`progress.md` and `task-1-report.md` said 37; both corrected by Task 17.** |
| `validate --root .. --slot System` | **43 advisory / 0 errors** | unmoved all wave. |
| `npm run check` (webApp) | **1234/1234** | |
| uitests preview | **57 tests, 57 passed**, over **23 fixture states** | See the reconciliation below — one reviewer counted 22, and both numbers are right about different things. |
| Hand-written `delivery` lines | **20,714** | See the line measurement below. |
| `make lint`, `fix-check`, 9× `gen-*-check`, `encapsulation-check`, `derived-plan-check`, `systemtests` build+vet | green at ship | re-run by the tasks that touched Go; Task 17 changed no Go. |

**The 22-vs-23 preview-fixture reconciliation, settled.** There are **23 fixture state files** under `uitests/preview-fixtures/web-client/**`. The preview specs contain **22 distinct `openState(page, '<screen>', '<state>')` literal pairs** — which is where the reviewer's 22 came from — but those two 22s are not the same set:

- 21 of the 22 literal pairs name a fixture that exists. The 22nd is `plan / no-such-state`, a **deliberate miss** with no fixture, asserting the honest error page.
- `activity-experience / service-fork-sent-back` is driven through the `SERVICE` **constant** (`activity-experience.spec.ts:44`), not a literal, so a literal-counting scan cannot see it. It is the most-exercised state in the suite (13 cases).
- So: 21 literal + 1 via the constant = **22 fixture states exercised**, plus **`deployment-linear.json`, which no preview spec drives at all** — it is validated only by `webApp/scripts/fixture-schema.test.mjs:266`. 22 + 1 = the 23 files.

**New earmark from that count: `deployment-linear.json` is schema-checked and browser-unexercised.** Either give it a case or delete it; a fixture nothing drives is a fixture whose truth nothing checks.

## The changed-path set at merge — **FIVE trees, not four** (final fix wave, F4)

Measured at the fix wave's HEAD with `git diff --name-only main...HEAD`: **123 files**, +24,654 / −5,124, across **six** top-level paths — and the fifth code tree is the one a reviewer of this branch is most likely to miss.

| path | files | what is in it |
|---|---|---|
| `server/` | 47 | the pump, the folds, the model edit's Go output |
| `webApp/` | 40 | the Activity Experience, the contracts, the UI identifiers |
| `uitests/` | 22 | the preview specs, the fixtures, `testids.ts` |
| **`systemtests/`** | **7** | **3 generated `internal/sdk/*_delivery.gen.go` + 4 hand-written `internal/harness/` files** |
| `docs/` | 6 | this file, the census, the spec, the drain procedure |
| `.aiarch/` | 1 | `state/project.json` |

**`systemtests/` is benign and expected, and it must still be named.** All seven files come from **exactly one commit**, `ebfc1a42` (the wave's one model edit) — the same commit that is the only one to touch `.aiarch/state/project.json`. Three are regenerated SDK files; the four in `internal/harness/` (`enums.go`, `transport.go`, `httptransport.go`, `mcptransport.go`) are hand-written and were **compile-forced** by the regen, not edited for their own sake. A merge-safety read that names only `server/`, `webApp/`, `uitests/` and `docs/` silently drops a tree that holds hand-written changes, and `systemtests` is a SEPARATE Go module — `GOWORK=off go build ./... && go vet ./...` must be run in it as well as in `server/`, which is the standing rule the deadness-claim section states for the same reason.

## The line measurement — spec §9's acceptance still holds

Measured at `d3a4f898` with the plan's own recipe (`wc -l server/internal/manager/delivery/*.go`; hand-written = total − `manager_test.go` − the four `*.gen.go`), and **both baselines re-derived from git rather than read out of a document**:

| | Total | `manager_test.go` | the four `*.gen.go` | **Hand-written** | Non-test |
|---|---|---|---|---|---|
| Three predecessor packages @ `4baed01a` (`systemdesign` + `projectdesign` + `construction`, top-level `*.go`, `fake/` excluded as the recipe excludes it) | 61,576 | 31,798 | 4,135 | **25,643** | 29,778 |
| `delivery` @ wave start (`c5851e90` = `bae7681f`) | 46,857 | 25,066 | 2,218 | **19,573** | 21,791 |
| `delivery` @ `d3a4f898` (HEAD) | 51,560 | 28,675 | 2,171 | **20,714** | 22,885 |

**The recorded predecessor baselines are NOT stale: 25,643 hand-written and 29,778 non-test reproduce exactly from git.** (The floor that was stale is a different number — the plan's own end-of-wave *prediction*.)

**Spec §9's acceptance — "the delivery manager is smaller than the sum of its predecessors" — HOLDS: 20,714 < 25,643 (80.8 %), and 22,885 < 29,778 (76.9 %) on the non-test measure.**

**And the floor moved a long way this wave, which should be said plainly: +1,141 hand-written lines (19,573 → 20,714), where the plan budgeted a REDUCTION.** The wave's own accounting, each figure from the task that produced it: Task 5 was −203; Task 7 was net negative; **Task 12 alone was +851** (`pumpnextactivity.go` 424 → 1,142, at the ~49 % comment ratio the file already had, so ≈ +391 of code and the rest the *why* written at the site); Task 14 was +99 production lines, ~55 of them the wire-form doctrine comment; `d3a4f898` added ~55 more. A prior measurement put HEAD around **20,659** at `37b0e768` — that number was correct then and the last commit is the difference. The acceptance criterion is a ceiling, not a ratchet, and it is met; but nobody should read "§9 holds" as "the manager got smaller this wave". It did not.

Claims below carry the task and the commit that produced them.

---

## Deploy note — 4b2 does not deploy, and not alone

**The drain procedure is `docs/bugs/2026-09-24-stage3-rail-earmarks.md`, and its six steps were rewritten as ONE current sequence by Task 17** (every number re-measured at `d3a4f898`; the four waves' amendment blocks moved below a rule into an appendix marked as history). One drain covers stages **3 + 4a + 4b1 + 4b2**, once, before one release.

What 4b2 changes in it: the drain owes **THREE** `temporal schedule delete` calls (`delivery:replanSweep` joins the two abandoned `construction:*` ids) and step 6 confirms **TWO** sweep Schedules. No new workflow TYPE name, no new workflow id family, no state migration — but `constructionReplanSweep` becomes the **eighth** retired workflow TYPE that must be terminated by hand, and `{projectId}:replanSweep:{tickId}` / `:all:replanSweep:{tickId}` join the id families to sweep.

**Read the procedure, not this paragraph.** This is a summary; the steps are the instructions, and the one thing in them that is new and dangerous is that the main-write lease has never run in production (§*The lease RUNS FOR THE FIRST TIME* below, and the watch-list in the procedure's *After the release* section).

---

## THE WAVE'S OPEN ITEMS — one ordered list

Five things leave 4b2 unfinished. They are ordered by what they cost while they wait, and each one's full measurement is in its own section below.

1. **🔴 THE LIVE DELINQUENCY DEFECT — a founder decision, and it costs money every day it waits.** `deliverSignal` has a **fifth** production producer outside `delivery`: `billing/shortfallsweep.go:118` sends `applyDelinquencyPolicy` to `{customerId}:delinquency`, and `operations/delinquencyenforcement.go:51` receives it **into a concrete struct** — so the SDK drops it and **no app is ever paused or withdrawn for delinquency**. Worse, the two payload shapes do not match: the producer sends `deliverSignalPayload{CustomerID, PauseNotWithdraw}`, the consumer reads `applyDelinquencySignal{CustomerID, Context}`. **A wire-form fix alone would leave `Context` empty and start pausing or withdrawing on a default — which is worse than blocked.** The founder must say what `Context` is supposed to carry, and whether `PauseNotWithdraw` is that thing under a different name. Nothing else in this list can be fixed without knowing that. → §*THE FIFTH PRODUCER*.
2. **The ContinueAsNew boundary is entirely unfixtured and CANNOT be captured.** `pump-drain-pause-before-continue-as-new` v1's `GetVersion` fires only past `pumpHistoryBudget` (4000 events), so no history capture can ever reach it — and with it the drain, the carry and the replay of `Carried` are all uncovered by any fixture. **By the pump's own header those are the riskiest ten lines in the file.** One unit mutation covers the drain half; a deterministic harness that forces the budget low enough to cross the boundary is what would buy the rest. → §*The ContinueAsNew boundary*.
3. **The lease invariant's scope is per pump CHAIN, not per project.** `LeaseHolder` lives only in `pumpState`/`pumpInput`, so it survives a ContinueAsNew and nothing else. Any pump run ending while a lease is held exits with a holder; the 30 s sweep starts a fresh chain with none; two activities can then write main concurrently across that boundary, held only by the row CAS and the branch-file version guard. **"Fails open" is the NORMAL state after any pump restart, not a rare fault path.** The header says so now; the invariant is still narrower than the feature reads. → §*The lease invariant's true scope*.
4. **The no-failure-row gap: a broken merge tail leaves no red node.** The cascade is safe (the pump stops on the child's future, `aa977ae9`) but head state still reads `Running`, or `Completed` with uncommitted slots, for an activity whose tail broke — so **an operator sees no failed activity**. `finalizeWalk`'s tail errors bypass `failWalk`, so no terminal row is written, and `pumpReconcile`'s "terminal failure row, no finish" arm cannot fire for a tail that outlives its pump run. Routing it through `failWalk` would write `VarianceExhausted` over an already-recorded `Completed` — **a decision about the documented heal-by-re-open path, not a line to add.** → §*Deferred to 4b3*, item 1.
5. **The deferred contract deltas — four of them, all owed to the next model wave, none of which trips a gate.** `PumpResult.activityIds` (the field does not exist; the frontier rides the Manager-internal `queryPumpDispatch` payload instead, with the exact JSON recorded below); `ReplanSweepResult` as dead contract surface (`contract.gen.go`, `openapi.yaml`, the `systemtests` SDK and `schema.ts` all carry it with no Go caller); the `ActiveRole`/`ActiveStep` `$defs` cascade the provider sweep did not take; and the committed contract note whose stage-3 deferral reason has expired. → §*Deferred to 4b3* items 4 and 5, and §*Doc/model staleness*.

---

## 🔴 THE WIRE-FORM DEFECT — and it is NOT confined to delivery

### What happened here

**The whole stage-4b2 main-write lease was INERT in production until `37b0e768`.** All three lease messages ride `messageBus.deliverSignal`, which hands the Temporal client a bare `[]byte`; the default converter therefore tags the payload **binary/plain**, and `ByteSlicePayloadConverter` can assign that to nothing but a `*[]byte`. `pumpPark`, `drainForContinue` and `requestMainWriteLease` all received into the concrete STRUCT, so the SDK logged

> `Corrupted signal received on channel activityLeaseRequested … type *delivery.activityLeaseRequest: type is not *[]byte`

and **dropped every message**. Measured on a real Temporal dev server during Task 14's capture: no request reached the pump, no grant reached a child, no finish reached the pump; both children armed `NewTimer … Duration 2h0m0s` and the capture timed out. **Every merge tail armed its 2-hour budget and ran UNLEASED.** It failed OPEN — which is the documented, deliberate fail-open path — so nothing went red and no test caught it: **every existing lease test signals a STRUCT** (json/plain), a wire form production never produces. The file already knew: `pumpPauseRequested` decodes into `any` for exactly this reason and says so verbatim at the site. The lease channels were written against the same transport without inheriting the lesson.

The fix (`pumpReceiveSignal` / `pumpReceiveSignalBlocking` / `pumpDecodeSignal`, typed over a closed `pumpLeaseSignal` constraint, four call sites) receives into `any` and normalises both wire forms. It **emits no workflow command**, so no recorded sequence moved and the eight pre-4b2 child fixtures stayed 8/8. An undecodable LEASE message is DROPPED — deliberately the opposite of the pause rule, because a pause's channel NAME carries the operator's intent while a lease message whose activity id cannot be read names nobody.

### 🔴 THE FIFTH PRODUCER — A LIVE PRODUCTION DEFECT, PRE-EXISTING, OUTSIDE THIS WAVE, AND IT NEEDS A FOUNDER DECISION

Task 14 believed `deliverSignal` had three production call sites. **The review found FIVE.** The fifth is outside `delivery` entirely:

- **Producer:** `server/internal/manager/billing/shortfallsweep.go:118` sends `applyDelinquencyPolicy` to `{customerId}:delinquency`.
- **Consumer:** `server/internal/manager/operations/delinquencyenforcement.go:51` does `sigCh.Receive(ctx, &sig)` **into a concrete struct** — the identical defect.
- **And the payload shapes do not match.** The producer sends `deliverSignalPayload{CustomerID, PauseNotWithdraw}`; the consumer receives `applyDelinquencySignal{CustomerID, Context}`. **So fixing the wire form alone would leave `Context` empty** — the receiver would decode, and then act on a field nobody filled.

**Live consequence today: the delinquency-enforcement branch blocks FOREVER on `Receive`, and no app is ever paused or withdrawn for delinquency.** The SDK logs "Corrupted signal" and nothing goes red — the same silence that hid the lease for the whole of 4b2.

**This is not hygiene and it is not a gate item. It is a defect with a design question inside it, and the question is the founder's: what should `Context` carry, and is `PauseNotWithdraw` the thing it is meant to carry under a different name?** The shapes cannot be reconciled by a wire-form fix; somebody has to say what the enforcement branch is supposed to be told. Until that is answered, do not "fix the wire form" here — a decoding consumer acting on an empty `Context` is worse than a blocked one, because it would start pausing or withdrawing on a default.

### What is armed, and what is not

**Shipped (Task 16, `cb244838`): `Test_DeliverSignal_TheWireFormProducersAreAClosedList`.** It AST-reads every `MessageBusDeliverSignal` call site in the package's hand-written files, requires the name argument to be `messagebus.SignalName(<const>)` (a computed name is a channel nobody can check), and pins the producer set to exactly four: `signalOperatorPauseRequested`, `signalActivityLeaseRequested`, `signalActivityLeaseGranted`, `signalActivityFinished`. The failure message is the rule. It is a **producer** gate and it is package-scoped.

**Owed to 4b3: the CONSUMER gate, as a real arch gate.** The rule is *a signal delivered through `messageBus.deliverSignal` must never be received into a concrete struct*. A precise gate needs dataflow from `GetSignalChannel(ctx, name)` to a `Receive` target across a struct field (`pumpChannels`), a closure param (`AddReceive(ch, func(c …))`) and a helper func (`pumpReceiveSignal`) — go/types work beside `framework-go`'s other gates, not a package test. Two exceptions a real gate must answer, both measured:

1. **`pumpPausedAtRunStart`'s `DefaultVersion` arm** (`pumpnextactivity.go:1247`) receives into a struct **on purpose** — it is the pre-change body behind the `pump-pause-decode-any` fence. A gate that forbade it would force retiring that fence.
2. **`projectsupervision.go:48` is a LATENT instance.** `pauseCh.Receive(ctx, &sig)` into `operatorPauseSignal` is **correct today and wrong tomorrow**: its one producer is `client.SignalWithStartWorkflow` with a struct (json/plain), so nothing is dropped — but the signal NAME is the same `operatorPauseRequested` that `relayPauseToPump` delivers as raw bytes to a *different* execution id. The day anything relays a pause to `{p}:construction` through the bus, supervision drops it silently. **A name-keyed gate flags this; an id-keyed gate does not**, and choosing between those two rules is the gate author's first decision.

---

## The lease RUNS FOR THE FIRST TIME after this deploy

Because every lease message was dropped on the wire until `37b0e768`, the following were **dead code exercised only by tests using a wire form production never produces**:

- `activityLeaseGrantWaitBudget` — the **2-hour** grant budget a child waits out before giving up and running its merge tail unleased;
- the liveness probe and its reap path (`pumpCheckLease`);
- the lease **epoch** and its re-delivery rule;
- `pumpGrantLease`'s validation that the requester is a child of this run.

**The first real cascade after this release is the first time the admission queue has ever been in the path of a merge.** Watch it deliberately, not incidentally. Three things to measure on it: how long a tail actually holds the lease (the 2 h budget has to clear *other* activities' merge tails, each of which can hold a human approval gate); whether a busy project serialises behind the budget; and whether the grant→release→re-grant handshake behaves as the capture showed (`granted epoch 1 → released → re-granted epoch 2` in 7.4 s against the dev server).

---

## The lease invariant's true scope is per pump CHAIN, not per project

The pump's module header originally claimed PROJECT scope for "at most one activity holds the main-write lease". **It is per pump CHAIN**, and the header now says so (corrected in `aa977ae9`).

`LeaseHolder` lives only in `pumpState`/`pumpInput`, so **it survives a ContinueAsNew and nothing else**. Any pump run that ends while a lease is held — a pause while parked, a child failure stopping the cascade, any error return — exits WITH a holder. The 30 s sweep then starts a fresh chain with `LeaseHolder = nil` and an empty `Started` set; child A is still inside its merge tail (a human gate means minutes to hours); the new chain dispatches D, D asks for the lease and **is granted it**. A and D write main concurrently, and only the row-level CAS and the branch-file version guard prevent damage — i.e. exactly the pre-wave state.

Two consequences worth keeping:

- **"Fails open" is the NORMAL state after any pump restart, not a rare fault path.** A lease request from a pre-restart child is dropped by the new chain's `isStarted` validation, so that child runs its tail unleased by construction.
- **The lease sits ON TOP OF the row-level CAS; it never replaces it.** That ordering is why failing a completed activity because the admission queue is unreachable would be worse than the pre-wave behaviour, and it is why the child's lease request fails open by design.

---

## Fixtures, fences, and the measurement that must not be lost

### A replay fixture does NOT pin a `GetVersion` fence

Measured twice, by the implementer and independently by the reviewer:

- Deleting the `changeDesignActivitiesDispatchable` rung from `pumpEligibilityRule` left **all six** new pump fixtures **GREEN**. The SDK tolerates a recorded `Version` marker the replayed code never asks for.
- By contrast a real command change **does** bite: deleting the pace `Sleep` gives `[TMPRL1100] a matching Timer command was expected in history event position 34` on **2 of 6** (positions 34 and 33; the other four never reach a second loop iteration).

**Fixtures pin COMMANDS, not version rungs.** "Replay is 14/14 green" is therefore not evidence about a fence, and nobody may read it as licence to discharge one. The census's `…_DefaultVersion_…` tests are those rows' only pin.

### Every fence's `DefaultVersion` arm is unreachable by capture

`GetVersion` returns `maxSupported` on a new execution, so **no capture can ever produce a `DefaultVersion` history**. Those arms exist for executions already in flight. Four census rows hang off them, and if someone deletes one of those tests as "untestable in production" the fence goes unpinned and nothing else notices.

All five pump fences were deliberately KEPT at 4b2 (Task 16 refused to discharge them). The discharge is owed **after** the drain that covers 3 + 4a + 4b, as one edit deleting each fence, its census row's arm and its `…_DefaultVersion_…` test together — and the two measurements above must travel with that earmark as the counter-evidence to "the fixtures cover it now".

### The ContinueAsNew boundary is entirely unfixtured and CANNOT be captured

`pump-drain-pause-before-continue-as-new` v1's `GetVersion` is reached only once `pumpShouldContinueAsNew` is true — i.e. past `pumpHistoryBudget` (4000 events) — so **no capture can reach it**, and with it the whole ContinueAsNew boundary is unfixtured: the drain, the carry, and the replay of `Carried`. **By the pump's own header those are the riskiest ten lines in the file.** The one mutation that does cover the drain is a unit test (`continue-as-new-loses-no-signal`: deleting `st.drainForContinue` gives `SignalsDelivered = [], want [C-ONE:finish C-TWO:leaseRequest]`), which is a good pin and not a history.

**If one thing in this wave deserves a targeted non-replay guard, it is this.** A deterministic harness that forces the budget low enough to cross the boundary in a unit environment would buy what no capture can.

Also not covered, stated in code at `pumpReplayCases`: G-P12's pre-CAN row arm (needs a CAN), and a child FAILURE stopping the cascade.

---

## Deferred to 4b3

**1. The no-failure-row half of the broken merge tail.** The cascade is now SAFE — the pump stops on the child's future (F1, `aa977ae9`) — but head state still reads `Running`, or `Completed`-with-uncommitted-slots, for an activity whose tail broke, **so an operator sees no red node**. `pumpReconcile`'s "terminal failure row, no finish" arm cannot fire for a broken tail that outlives its pump run, because `finalizeWalk`'s tail errors bypass `failWalk` and no terminal row is written. Routing it through `failWalk` would write `VarianceExhausted` over an already-recorded `Completed` — **that is a decision about the documented heal-by-re-open path, not a line to add in a fix round.** Earmarked at the site.

**2. A child that dies without writing any row is invisible to the reconcile** and stays in `Started` forever. The lease HOLDER is probed and reaped; a non-holder zombie is not, because probing all N started activities every 30 s is N `deliverSignal` Activities per tick on a 30-activity plan. Wants either a real execution-status RA op (a contract change) or a staleness bound on `Started`.

**3. The per-wake-up whole-aggregate read is now STRUCTURAL — 1.17 MB every 30 s while parked.** The read-amplification win was DEFERRED, deliberately and by ruling: a long-lived lease pump must re-read on every wake-up because the CHILD writes head state and the frontier genuinely moves, so deriving from carried in-memory state is stale by construction. The real 4b2 win is the history churn of per-dispatch ContinueAsNew, not the read count — and a test asserts `reads >= 2` so no comment or commit text can claim otherwise. **This is the strongest argument yet for pulling the batched/narrowed plan read (`QueryProjectView(plan)`, R3/GAP-7) forward.**

**4. `PumpResult.activityIds` is an owed CONTRACT delta.** The field does not exist; Task 7's one model edit did not carry it and opening a second model edit + full self-amendment loop for one optional array was refused. Nothing was hand-edited. The whole frontier already rides the Manager-INTERNAL `queryPumpDispatch` payload (`pumpDispatch.ActivityIDs`, not a contract type), with `PumpResult.ActivityID` = the first and the deprecation stated at both sites. The exact delta, for the next model wave, on `deliveryManager.$defs.PumpResult`:

```json
"activityIds": { "type": "array", "items": { "$ref": "#/$defs/ActivityID" }, "x-go-name": "ActivityIDs" }
```

**5. `ReplanSweepResult` is DEAD CONTRACT SURFACE.** `ReplanSweepWorkflow` is gone (`f3673fb1`) but the model still defines the type, so `contract.gen.go`, `openapi.yaml`, the `systemtests` SDK and `schema.ts` all carry it with **no Go caller** (verified by the caller sweep, not by grep: two Go lines, both declarations). It trips no gate. Deleting it is a MODEL-level edit for the next model wave.

**6. Also 4b3, carried from earlier tasks in this wave:** the per-task session view (`constructState`'s single-valued view fields are clobbered on a fork, so a query sees the most recently entered gate — this is what an honest refusal of an early decision needs); the late-approve-applied-to-the-wrong-revision defect (pre-existing: `taskDecisionSignal` carries no round or revision identity, so a human who read revision *n* can have their approve applied to *n+1*; the fix shape is to put the round the human read in the signal payload — a stage check never could close it); the double `ReadActivityExecution` on the approve path (two git reads on the hottest write); the LIVE DOUBLE-WRITE where the child calls both `GitStatusRecordActivityStarted` and `ActivityExecutionOpenActivity` on the same row in the same walk with nobody having said which wins; and the artifact-as-of-revision read (R1/GAP-5), still owed — no op takes a ref.

---

## Written by stage 4b3 Task 11 — the correction to R23, and two model-wave carries

### 🔴 R23's remediation for `deliverymanager.go`'s size is FORBIDDEN BY THE STANDARD IT CITES

**R23's remediation row for the 13,535-line file — "split by op family under the existing
file-layout standard" — is FORBIDDEN BY that standard.** Rule 1 admits one
contract-implementation file and Rule 4 admits no others; the standard's own text calls large
files an accepted consequence and names `projectstateaccess.go` as the precedent. The only
sanctioned reduction is Rule 2's re-homing of workflow-exclusive helpers, which 4b3 did and
measured. **The real owners of the file-size finding are (a) a platform amendment to the
standard — a `framework-go` release, i.e. not this programme — or (b) the facet wave, which
removes RA-shaped code from the Manager rather than re-filing it.** Recording this as "done"
without saying so would leave the next reader believing a split is available.

(The size figure moves between documents and none of them is wrong: **13,234** at the wave's
base `8d9604b1`, **13,535** measured at Task 11's HEAD after Tasks 8-10 added to it. The plan's
acceptance for this task is `TestFileLayout` **green plus an honest number**, never a line
target — and the number went **up** during the wave, which is what "large files are an accepted
consequence" means in practice.)

`TestFileLayout` (`internal/arch_test.go:61`, `arch.CheckFileLayout`) has held that at **zero
waivers since 2026-07-12**, and the allowed set for this package is exactly:
`deliverymanager.go` (Rule 1) + one file per registered workflow — `deliveryactivity.go`,
`pumpnextactivity.go`, `pumpsweep.go`, `projectsupervision.go`, `roundsweep.go` (Rule 2) +
`manager_test.go` (Rule 3) + the four `*.gen.go` (exempt). **A second implementation file is
not an option**, and never weakening a gate to satisfy a remediation is the standing rule.

### And the measured Rule-2 re-homing is EMPTY — the honest number, with its method

The plan's first-cut scan found **107** candidate symbols by bare-word regex. That number is
inflated by prose: re-measured with a **go/ast walk** (comments discarded, string literals
excluded, selector `Sel` idents counted so a method call `wf.foo()` registers as a use), the
set of top-level symbols declared in `deliverymanager.go`, referenced by **exactly one** other
hand-written file, and referenced **nowhere inside `deliverymanager.go` itself**, is **82** of
**688** top-level declarations — **77** toward `deliveryactivity.go` and **5** toward
`roundsweep.go`. Each file declares exactly one *registered* workflow, so "one other file" and
"one workflow" coincide here.

**Zero of the 82 moved**, and the per-symbol reasons fall into four buckets, every one of them
the standard's own or the code's own rather than this task's taste:

1. **Rule 1's enumerated categories (≈40).** Rule 1's colon-list is verbatim Rule 4's
   forbidden-file list — error mapping, adapters, codecs, git rail/session mechanics, prompt
   corpora, strategy tables *and strategy implementations*, signal handlers, behavior
   free-functions — i.e. the code that used to live in `errors.go` / `gitrail.go` /
   `codec.go` / `strategy.go`. Rule 1's test is **"not specific to one workflow"**, which is
   semantic; a usage count of one is evidence, not proof. Covers `isConflict`,
   `isRailAuthFault`, `isRAContractMisuse`, `isDecodeFault`, `deriveFailureReason`,
   `encodeModel`, `mainBranch`, `activityBranchName`, `prTitle`/`prBody`, `crLabelHints`,
   `toRail`, the four `railAuthRetry*`/`railCredRenewSkew` constants, `mapCheckState`,
   `managerPipelinePhase`, `dispatchInputsFor`, `computeProjectPlanSlots`, and the note and
   round free-functions.
2. **Seven carry the doc line "Stage 4a: ONE copy now serves the systemDesign +
   projectDesign (+ construction) rails (byte-identical twins, collapsed by the package
   merge — `arch.CheckFileLayout` allows…)".** Their own text cites this standard as the
   reason they sit in the impl file. Moving them would make the code contradict its comment.
3. **A documented placement, made deliberately and citing the standard by name.**
   `strandedRounds` says, at its site: *"It is a PURE function of one row (no workflow
   context), so the file-layout standard puts it here beside `roundGateKey` rather than in
   `roundsweep.go`, and the sweep's tests can state the rule without a Temporal
   environment."* That block (`roundSweepDecidedBy`, `strandedRounds`, `sortedActivityIDs`,
   `deliverymanager.go:9386-9431`) was the single strongest move candidate and its own
   comment is the refusal.
4. **Splitting a declared FAMILY, or a symbol whose doc says "the Manager's…".**
   `roundSweepWorkflowID` is one of four adjacent workflow-id derivations (`:7405-7435`);
   the six `walkTask*` are an **iota** block under `walkTaskState`, whose ordinals are
   payload-visible across a ContinueAsNew and therefore under the never-renumber rule; the
   four `routedKind*` sit under `routedSignal`; the five `gateOutcome*` beside
   `takeoverGateKey`; `jobModeDraft`/`jobModeCritique` beside the `dispatchInput*` contract
   with `aiarch-design.yml`. And `csPipelineObservation`, `csPullRequestStatusView`,
   `proposeReviewSet`, `prTitle`/`prBody` and `managerPipelinePhase` each say *"the
   Manager's / Manager-local / Manager-neutral"* in their own first line — the brief's
   "if the Manager plausibly grows a caller, leave it", answered by the code.

**Two measurement caveats the next reader is owed.** (a) **The one-level set is not closed.**
`seedResumeFromLedger` is a candidate; `seedTaskCount`, which only it calls, is not — because
its one caller is inside `deliverymanager.go`. Moving the first makes the second a candidate,
so a maximal re-homing needs a transitive closure this task did not compute and no-one has
asked for. (b) **A move between files in one package moves no lines out of the package.** The
hand-written `delivery` total is unchanged by anything Task 11 did or could have done, and
§9's acceptance is a ceiling, not a ratchet.

### 🟡 Carry for the next MODEL wave: the requeue note cannot say who wrote it

`OperatorNoteInput` (`projectstate/contract.gen.go:558-564`) has **five** members —
`noteId`, `kind`, `gate`, `text`, `comments` — and **no author member**. The round half of the
sweep records `decidedBy: "platform-sweep"` as a *field* on the round; the re-open half's
provenance lives in the note's free **TEXT** (`sweepReopenNoteText`, `roundsweep.go`), so an
operator reading the notes list can tell a platform heal from their own and **no query can**.
There are exactly two writers of `NoteRequeue` — `reopenActivity`
(`deliverymanager.go:6189`) and `sweepReopenNotLanded` (`roundsweep.go`) — and nothing on the
row distinguishes them.

Not added here: a member on a generated contract type is a model edit, and R13 spends the
wave's one on Task 8. The exact delta, for the next model wave, on the SHARED
`OperatorNoteInput` `$def` (it is shared by `activityExecutionAccess` and the deprecated
`constructionTransitionAccess`, so it reaches both facets' tool schemas, `openapi.yaml`,
`schema.ts` and the `systemtests` SDK):

```json
"authoredBy": { "type": "string", "description": "Who filed the note: an operator identity, or the platform component that filed it on its own initiative (e.g. \"platform-sweep\", matching the round ledger's decidedBy). Empty on a note written before the member existed." }
```

**What it would buy, concretely:** the 4b3 heal bound
(`healWouldRepeatOneThatChangedNothing`, `roundsweep.go`) deliberately does **not**
distinguish a platform heal from an operator's, and the reasoning holds for both — a re-open
that resolved no task changed nothing whoever filed it. With the field, a future wave could
bound the two differently (e.g. let a human's explicit retry always through while the
platform's stays bounded) and could *count* platform heals per row, neither of which is
answerable today.

### 🟡 A pre-existing Rule-1 inversion, found by the same measurement and NOT fixed here

`readProject` is declared in **`pumpnextactivity.go:1538`** and is called by
`roundsweep.go` as well. The standard's Rule 2 says in as many words: *"Code shared by two or
more workflows moves up into the contract-implementation file (Rule 1)."* So its home is
`deliverymanager.go`. `TestFileLayout` does not catch it — the gate checks the FILE SET, not
which file a shared helper landed in — and the fix's direction (**into** the 13.4k file) is the
opposite of this task's, which is why it is recorded rather than taken. It belongs with (a) the
`framework-go` amendment or (b) the facet wave, the same two owners as the size finding itself.

---

## The deadness-claim lesson — this wave's dominant defect

**"No caller" from a grep was WRONG four times in this wave.** On record: `Solution.ClassRates` ("nothing reads it" — `SolutionView` does), the over-deleted exported `RegisterManagerWorker`/`RegisterSchedules`, the `statuses` channel "measured dead", and `detailPaneState.ts`. Nothing was actually deleted on a false premise — a dedicated verification sweep confirmed that — but only because the reviews caught each one.

**Why the grep lies: nine indirections reach a contract op and SIX are invisible to `\.Op(`.**

1. **The generated Temporal invoker** — and this is the one that defeated the original Task 13 recon: **the invoker method's prefix is the FIELD NAME in `genActivities`, not the contract name.** `GitActivityStatusAccess` is held in a field called `GitStatus`, so the method is `wf.Acts.GitStatusRecordActivityBranchOpened(…)` and `\.RecordActivityBranchOpened(` matches nothing. **Nothing in the repo states that map in one place.**
2. String-keyed worker registration.
3. The generated MCP tool catalog (`toolcatalog.gen.go`, served by `cmd/aiarch-state-mcp`) — **agent-reachable with zero Go call sites**.
4. Interface satisfaction.
5. Composition-root wiring.
6. The generated SDK / REST / webApp clients.

**The decisive check is: delete the member, then run `GOWORK=off go build ./... && go vet ./...` in BOTH `server/` and `systemtests/`** (vet type-checks tests, build does not). The converse hole is real too — 32 of `delivery`'s 70 invoker methods have no non-test call site, yet most are alive via direct synchronous calls.

### Adopted as standing rules

1. **Every deadness claim carries a COMPILE-PROBE TRANSCRIPT, never a bare grep.**
2. **`temporalgen` should EMIT the contract → invoker-prefix table.** The one fact that defeated the recon is written down nowhere.
3. **Extend `UIIdentifiers.test.ts` to assert the composed PREFIX of function-form ids.**
4. **Add a webApp orphan-module gate built on an import graph** (a ~15-line resolver was written during the sweep).
5. **Make "dead" a TWO-WORD VERDICT** — *uncalled* vs *unreachable* vs *unreferenced-in-Go-but-live-via-MCP*. Requiring the author to name which they measured would have caught `detailPaneState` on its own, because its author could not have named any of the three.

### 🔴 STRIKE THIS FROM ANY RETIREMENT LIST

**`webApp/src/components/construction/detail/detailPaneState.ts` has SEVEN production importers** — `activity/PlanList.tsx`, `activity/PlanTile.tsx`, `activity/planTiles.ts`, `construction/detail/bodies/taskBriefing.ts`, `construction/list/activityRowPresentation.ts`, `construction/list/activityTree.ts`, `construction/tasks/TasksLens.tsx` — and exports `taskDetailStateFill`, `TaskDetailState`, `RUN_NOT_WIRED_REASON`, `evidencePointerFor`, `selectedAttemptOf`, `taskDetailStateFor`, `outcomeStateOf`, `WIDE_PANE_SX`. Deleting it takes out the Plan tiles' fill, the task briefing and the Tasks lens. **It was listed as test-only; the claim came from a basename grep that hit prose comments.** The file still exists at HEAD — nothing was lost.

Verdicts from the same sweep, for anyone reading the 4b1/4b2 lists: `provenance.tsx` TRUE; `toolbarForLens` / `activityMeta` / `listEmptyState` / `searchExpansion` / `decisionRecords` TRUE; `observedOnly` TRUE **but six tests depend on it**; `decisionFlow` PARTLY FALSE (also imported by production `decisionRecords.ts` — retire the pair together); `scrollerGeometry.ts` FALSE in the safe direction (zero importers AND no test); `FocusRailContext` / `ScenarioLinkContext` / `inFocus` all TRUE. Four true orphans the wave never listed: `AwaitingPanel.tsx`, `ProjectArtifactRenderer.tsx`, `EpisodesPanelContainer.tsx`, `contracts/constructionRows.ts`.

### Task 13's premise, and why `gitActivityStatusAccess` survived

The recon said "zero production callers on all six ops". Corrected counts, excluding `_test.go` and `*.gen.go`: `BranchOpened` 1, `CIObserved` 2, `ArchApproved` 1, `Merged` 1, `Started` 1, `Completed` 1 = **7 live call sites; every one of the six ops has a caller.** All trace to the generic DAG child's spine (`openActivityRow`, `finalizeActivity`); no `GetVersion` retires them; the composition root binds the facet non-nil in BOTH profiles. `RecordActivityStarted`/`RecordActivityCompleted` are not git mirrors at all — they upsert the rows `AllDepsSatisfied` reads, and `Completed`'s own doc says it "unblocks its dependents", so **deleting them stalls the pump, this wave's whole subject.** `ActivityExecutionAccess` is at exactly 12/12 and cannot absorb a verb; the enabling move is the `projectCatalogAccess` split, in no row of this wave. `DH-CONTRACT-DEADOP ×2` never flagged these six — an independent signal that they have live callers.

One MODEL-level correction falls out of it and is owed to the next model wave: the committed contract note's stage-3 deferral states a reason that has EXPIRED (all nine fixtures recording these names now live under `testdata/replay-archive/`, which no test reads). The real blocker is the 12-op ceiling plus three unsuperseded verbs.

---

## Smaller carries

- **The `temporal` CLI is a HARD DEPENDENCY of both capture tools, stated only in a `t.Fatalf`** (`Test_Capture_DeliveryHistories`, `Test_Capture_PumpHistories`). It is in no README and no Makefile target, so a fresh checkout discovers it by failing. Version here: **1.7.0 (Server 1.31.0)**. `CONSTRUCT_HISTORY_CAPTURE_CASE=<name>` re-takes a single fixture and is documented only in the tool's own comment.
- **Three `.claude/commands/*.md` could not be deleted.** `.gitignore:29` ignores `/.claude/commands/`; the tree is MATERIALIZED from the pinned `method-assets` by `make claude-assets`, which `make test` runs, so deleting them is an untrackable no-op the next `make test` undoes. The retirement of `scrubbedRequirements` / `standardCheck` draftability is enforced by the EMPTY slug in `designKindSlugs` instead. **The file deletion rides the next method-assets release.**
- **`construction-round-withdrawn.json`'s three change requests still carry an addressee outside the generated enum.** Task 10 fixed one occurrence to `"pm"`; three remain at `:31408`, `:31428`, `:31519` carrying `"seniorDeveloper"`, which is outside `ReviewCommentAddressee` and is therefore DROPPED by the adapter. **Nothing gates preview fixtures against that enum** — one clean pass plus a fixture-vs-enum assertion closes it.
- **`AskQuestions` validates non-empty but not the vocabulary.** `deliverymanager.go:10625` refuses an EMPTY addressee and nothing else, so a value outside `{pm, architect}` can be persisted and then emitted on a wire whose enum forbids it. The server half of the item above.
- **The reachability sequel to the orphan guard is its own task (likely 4b3).** The UI-identifier guard proves an id is REFERENCED, not that the reference can ever RUN, and two clusters hide behind that: `Construction.PROVENANCE_RAIL`/`PROVENANCE_BADGE` are placed only by `components/construction/provenance.tsx`, which nothing imports; and `ServiceContract.OPEN_FOCUS`, `CANVAS_NEEDS_ROOM*`, `CODE_CANVAS*`, `CODE_INTERFACE_NODE`, `STRUCT_CARD` sit on `ServiceContractView`'s `inFocus` branches, **which can never be true** (nothing provides `FocusRailContext` and no caller passes `inFocus`). Both count as "referenced" and survive. Related: **ten construction modules are imported ONLY by their own tests** — green tests proving nothing about the app.
- **The MCP widget has NO preview surface at all.** `mcpShell` is not booted by `previewShell`, so Task 9's re-pointed design gate is covered by unit tests and the compiler only: a wrong `taskId` would be a silently-shut gate nothing in CI catches — **the same class of silence as the bug it fixed** (the widget's approve/reject gate had been dead since 4b1, because its derived data source stopped emitting the stages the branches read). The wiring is now pinned by a source-scan test; a real preview surface is the durable fix.
- **Two orphan React contexts**, recorded as a UX decision rather than a re-mount: `FocusRailContext` and `scenarioLink`'s `ScenarioLinkContext` have no `.Provider` anywhere in `src`. Their mounts died with the construction console (stage 5 Task 13). Neither is a data lens — one controls a collapsible panel this screen does not have, the other a `?sc=` URL parameter this route does not carry — so mounting either means adding the thing it controls. **Nothing gates a THIRD screen appearing without the three cross-slot providers.**
- **Two meta-test holes in the UI-identifier guard**, both from the review of `05254eb5`: any PROSE mention of `Namespace.key` in a non-excluded file vouches for that id (only three files are excluded, so a tombstone comment written elsewhere keeps a dead id green forever) — strip comments from the corpus; and `leafKeys` is INDENTATION-EXACT (`^ {2}\w+: \{$` / `^ {4}\w+:`), so a prettier width change or one nested object silently removes leaves from the scan with no signal — a count assertion against a known total catches it.
- **Doc/model staleness for the next model wave:** the `ActiveRole`/`ActiveStep` `$defs` cascade was not taken (it is a `project.json` edit plus a second regen of committed state, with `gen-enums.mjs`'s `OUTPUT_NAMES` and `enums.gen.ts`'s two blocks); the rewritten slot-8 notes state the hosting-fee sentence twice; `ReviewSubjectRef.kind`'s description still says `commit` is what "a future subject-by-sha writer will use" (stale since 4b1's `38fd7f9c`); `RevenueShareNone` is still the missing vocabulary member that `RevenueShareNegotiatedRate at 0%` stands in for.
- **Prose still naming things that moved:** five doc lines state revenue share as current (`billingengine.go:2` and `:20`, `billingmanager.go:3`, `projectstateaccess.go:4588`, `docs/later.md:58`); `deliverymanager.go:13165` says `querySessionState` returns a `SessionStateView` where the handlers return `ConstructionSessionView`; `internal/arch_test.go:943` calls `ReconcileBranchFromMain` "a required ProjectStateAccess op" (it is `DesignSessionAccess`-only); `ServiceContractView`'s prose describes a focus view that no longer exists; `homebase.spec.ts:47` still regexes `/^phase-card-/` — a raw literal asserting ABSENCE, so it passes and is now silently unfalsifiable.
- **`project-design-m0-defaulted` is a MISNOMER.** The fixture was minted for the retired "platform wrote slot 8" arm; it now means an AUTHORED slot 8 with no detail. Not renamed, recorded.
- **The `-defaulted` pair's meaning, so nobody reads it as broken plumbing:** on THIS repo the M0 cost-basis detail is EMPTY, and that is the PROOF the founder's revenue-share ruling closed the uncomputable-SDP finding at source — with revenue share gone, slot 8 is fully authored and `computeProjectPlanSlots` defaults nothing. **An absent detail is "nothing was assumed", never broken plumbing.** Said in the module header at the site.
- **Shape-rig artefact:** `shapeSDPActivity = "P-SDP"` is not a production activity id and mis-classifies (Documentation → no `sdpReview` task → kind nil), so two façade M0 tests and the `projectDesign` shape rig exercise a kind resolution production never takes. Rename to `projectDesign` (it touches several shape cases). The measurement that depends on it: **"`settleThreadsBeforeApprove` has never run at M0" is TRUE of the test rig and FALSE of production** — no downstream plan may assume M0 was ungated since 4b1.
- **`Test_LifecycleShapes` load-flake, for anyone reading a red run:** it does NOT reproduce serially (0/30 at base); it needs LOAD (3/40 at 8-way concurrency, 7.5%) and it hits `fork-join-service-design-first` as well as `stp-first`. Task 4 fixed the root cause for the declaration-order claim by moving it to `readyTasks` (pure, 100% instead of ~92%), but **a serial CI run understates the class; nobody should declare a future flake "gone" from a serial run.**
- **Two unrelated pre-existing hangs/flakes seen this wave:** `TestRegisterOperatedSystem_AlreadyRegistered_Conflict` hangs at 10 m under full non-short `./...` (not in `-short`, not touched here); and `golangci-lint cache clean` must run FIRST in any worktree — a stale cache printed 30 phantom issues pointing into a DELETED sibling worktree.
- **The census row count was wrong in two documents and both are now corrected.** It holds **36** (22 + 7 + 7), measured against `len(pumpGuardCensus())`, and `Test_PumpGuardCensus_TheHeadlineCountsAreTrue` fails if the doc lies about it. `progress.md` line 11 and `task-1-report.md` both said **37**; Task 17 corrected both in place with the correction marked, so the ledger still reads as a record rather than a rewrite.
- **`uitests/preview-fixtures/**/deployment-linear.json` is driven by no preview spec.** It is validated only by `webApp/scripts/fixture-schema.test.mjs:266`, so its schema is checked and its rendering is not. Every other one of the 23 fixture states is exercised in a browser. Give it a case or delete it.
- **🟠 NOTHING GATES THE CENSUS'S HEAD-LINE CITATIONS, and they drifted inside this wave (final fix wave, F1).** Task 16 wrote each pump row's HEAD line number into the `Verdict` column at `cb244838`; `d3a4f898` then added ~55 net lines to `pumpnextactivity.go` and updated **one** row, leaving **21 of 22 citations wrong** — G-P17 pointed at `r, _ := v["Reason"].(string)` while `ParentClosePolicy: PARENT_CLOSE_POLICY_ABANDON` had moved to `:1392`, so the row read as though the guard had been deleted. All 22 are re-measured now. **Three meta-tests stayed green throughout**, and they are right to: `Test_PumpGuardCensus_TheDocAndTheCodeAgree` compares the ID set and the `PinnedBy` cell, and the `Line` column it matches is `pumpGuard.Site` — the *pre-wave* provenance line, which by design does not move. **The assertion that is owed, and why it is not one line:** the `Verdict` cell is free text (`**RE-ASSERTED**, widened · `:769-783` + `:861-875``), so a test can extract the `` `:N-M` `` tokens, but an IN-RANGE check would not have caught this defect — `:1337` was in range in a 1,375-line file, and a gate that passes on the live failure is worse than none. The two designs that would work: (a) give every row an `AtHead` field naming a short literal the cited line must CONTAIN (36 rows × one string, and the field is then the thing that drifts); or (b) assert a recorded content hash of each cited file, which turns *any* edit to the pump into a red test demanding re-measurement — cheap, correct, and noisy by construction. **(b) is the honest one; it needs a founder's tolerance for a doc test that goes red on unrelated code edits.** Until then the citation is a human-navigation aid with no net, which is exactly what Task 16 said is worth more than its artifact.
- **NOTHING GATES AN UNUSED ALIAS in `uitests/tests/support/testids.ts` (final fix wave, F5).** The orphan guard (`webApp/src/utilities/constants/UIIdentifiers.test.ts`) iterates `UI_IDENTIFIERS` **leaves** and passes a leaf that is referenced anywhere in the corpus **or** through any of its aliases. It never iterates the alias map, so an alias for a leaf that production code places — every leaf with a `data-testid` — can sit in `testids.ts` forever with no spec naming it and nothing goes red. The Task 11C sweep removed 116 unused aliases **by hand**, which is the only mechanism there has ever been. The fix is the mirror of the existing loop: for each alias, require `TESTID.<alias>` in the specs corpus. **(The fix wave's F5 also nominated `activityCostBasis:413` as one such residue; that nomination is REFUTED — four live preview assertions use it, `activity-experience.spec.ts:671/694/705/714`, and deleting it fails `npm run typecheck` in `uitests` with four TS2339 errors. The guard hole is real; that instance is not.)**
- **`webApp/scripts/gen-enums.mjs`'s `NON_MECHANICAL.ProjectSessionStage` entry is dead configuration.** The enum it explains was removed by the model edit (`ebfc1a42`), so the key can never match a logical output name again. Its nine lines of reasoning are now history for an enum that does not exist. Harmless, ungated, and one line to delete in the next webApp pass.
- **`cmd/server/hooks_test.go:321-323`'s doc comment still names the Schedules it gates as "pump sweep / replan sweep".** The replan sweep is deleted and the second Schedule is the round sweep. The assertions are correct and pass; only the comment lies. (A code fix, so Task 17 left it — it is a docs-only task.)

---

## Founder decisions owed — and one that closed itself

1. **🔴 What should the delinquency signal's `Context` carry?** The live defect at the top of this file. Nothing is paused or withdrawn for delinquency today, and the two payload shapes cannot be reconciled without an answer. This is the one that costs money while it waits.
2. **GAP-4B-4's three orphan kinds**, carried from 4b1.
3. **`RevenueShareNone`** as a vocabulary member — `RevenueShareNegotiatedRate` at 0 % is standing in for it, which is truthful but is an encoding, not a member. (4b2 removed revenue share from the *model* entirely, so this is now a billing-vocabulary question, not an SDP one.)
4. **The assumed-planning-assumptions UX** — an operating-model screen, and a way to tell "assumed" from "accepted" once the founder has read them. The disclosure itself now works for the first time: the M0 attempt `Detail` was produced and dropped on every run until Task 7 persisted it.
5. **The construction answer command** — a construction question is recorded and rendered and nothing can answer it (`respondToReviewComment` is slot-scoped and unregistered in the construction job mode).

**CLOSED by measurement rather than by ruling: design questions are filed on MAIN.** 4b1 left "whether design questions belong on the activity branch or on main" as an open founder question; Task 3 (`6ba40dd7`) found `resolveQuestionBranch` was dead **by type** — it could only ever return `""` — so every design question has always been filed on main, and the resolver, `isLiveSessionStage`, `readProjectMaybeBranch` and `projectstate.DesignBranch` were deleted with it. The question was about a choice nothing was making.

---

## The lesson this wave is owed by name

**A fix that reads right, passes, and does not fire on the case it names is this programme's signature failure — and the controller produced one.** Two of the three M0 fixes in this wave looked correct and were INERT: `roundRevisions` copied a gate attempt's `Detail` only inside `attemptGateKey(a) == roundJoinKey(r)`, and because `roundJoinKey` includes the artifact kind while `attemptGateKey` is hardcoded kindless, at M0 the keys are `"sdpReview:sdpReview:1"` vs `"sdpReview:1"` and **can never match**. The commit message, the comment at the site and the test's doc were all false for the one gate they named, and the test's fourth assertion passed a round with no attempts supplied — so it CERTIFIED the broken shape as correct. Corrected at `f549dede` with `soleKindClaims`.

**The review loop caught both. Nothing else would have.** The same holds for the census: it named the rows and made the review a checklist, but the two guards lost *inside* this wave (G-P12, G-P10) were found by a human reading a new body against the old guard, with a machine-checked census in front of them the whole time. **A future wave that treats "the census is green" as coverage will repeat 4b1.**
